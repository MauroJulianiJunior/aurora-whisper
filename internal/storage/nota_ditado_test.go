package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O "Virar nota" e o Desfazer (ADR-022, N3), contra um SQLite de verdade: as
// duas tabelas mudam juntas ou nenhuma muda, o ditado marcado sai das listas
// sem ir para a lixeira, não é alcançado pelos expurgos, e nada vai para a
// fila do sync.

type virarDeTeste struct {
	db    *sql.DB
	notas *storage.SQLiteNotaRepository
	hist  *storage.SQLiteTranscriptionRepository
	dono  string
	rel   *relogioDeNotas
}

func novoVirarDeTeste(t *testing.T) virarDeTeste {
	t.Helper()
	_, db := bancoEmArquivo(t)
	r, err := storage.GarantirDonoFixo(context.Background(), db, "")
	if err != nil {
		t.Fatal(err)
	}
	notas := storage.NewSQLiteNotaRepository(db)
	rel := &relogioDeNotas{t: inicioDasNotas}
	notas.UsarRelogio(rel.Now)
	return virarDeTeste{db: db, notas: notas, hist: storage.NewSQLiteTranscriptionRepository(db), dono: r.Dono, rel: rel}
}

// ditado grava um ditado do dono, com o texto final e o bruto dados.
func (v virarDeTeste) ditado(t *testing.T, id, bruto, final string) {
	t.Helper()
	tr := newTranscription(id, v.dono, inicioDasNotas.Add(-time.Hour))
	tr.RawText, tr.FinalText = bruto, final
	if err := v.hist.Put(tr); err != nil {
		t.Fatal(err)
	}
}

func (v virarDeTeste) noHistorico(t *testing.T, trash bool) []string {
	t.Helper()
	p, err := v.hist.List(context.Background(), storage.TranscriptionListFilter{UserID: v.dono, Trash: trash})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func (v virarDeTeste) contar(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := v.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (v virarDeTeste) virouNota(t *testing.T, id string) sql.NullString {
	t.Helper()
	var s sql.NullString
	if err := v.db.QueryRow(`SELECT virou_nota FROM local_transcriptions WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVirarNota_CriaANotaEEscondeODitado(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "bruto um", "- leite\n- pão")
	v.ditado(t, "d2", "bruto dois", "outro")

	n, err := v.notas.VirarNota(ctx, v.dono, "d1")
	if err != nil {
		t.Fatalf("VirarNota: %v", err)
	}
	if n.Corpo != "- leite\n- pão" || n.Titulo != "" || n.Cor != nil {
		t.Errorf("a nota = %+v; quero o final_text no corpo, sem título nem cor", n)
	}
	if n.DitadoDeOrigem == nil || *n.DitadoDeOrigem != "d1" || n.Version != 1 || n.Fixada || n.Arquivada {
		t.Errorf("a nota = %+v; quero ditado_de_origem d1, versão 1, nem fixada nem arquivada", n)
	}
	if got := v.virouNota(t, "d1"); !got.Valid || got.String != n.ID {
		t.Errorf("virou_nota = %v; quero %s", got, n.ID)
	}
	if got := v.virouNota(t, "d2"); got.Valid {
		t.Errorf("o outro ditado também foi marcado: %v", got)
	}
	// Sai do histórico, não vai para a lixeira.
	if ids := v.noHistorico(t, false); len(ids) != 1 || ids[0] != "d2" {
		t.Errorf("o histórico = %v; quero só d2", ids)
	}
	if ids := v.noHistorico(t, true); len(ids) != 0 {
		t.Errorf("a lixeira = %v; quero vazia", ids)
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM local_transcriptions WHERE id = 'd1' AND deleted_at IS NULL AND purge_at IS NULL`); n != 1 {
		t.Error("o ditado ganhou deleted_at ou purge_at")
	}
	// O Get (do Aprender e da auditoria) ainda acha, com o raw_text.
	tr, err := v.hist.Get(ctx, v.dono, "d1")
	if err != nil || tr.RawText != "bruto um" {
		t.Errorf("Get = %+v, %v; quero o ditado de volta, com o raw_text", tr, err)
	}
}

func TestVirarNota_BuscaNoHistoricoNaoAchaODitadoQueVirouNota(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "reunião de segunda", "reunião de segunda")
	v.ditado(t, "d2", "reunião de terça", "reunião de terça")
	if _, err := v.notas.VirarNota(ctx, v.dono, "d1"); err != nil {
		t.Fatal(err)
	}
	p, err := v.hist.List(ctx, storage.TranscriptionListFilter{UserID: v.dono, Query: "reunião"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].ID != "d2" {
		t.Errorf("a busca achou %d; quero só d2", len(p.Items))
	}
}

func TestVirarNota_SemTextoFinalUsaOBruto(t *testing.T) {
	v := novoVirarDeTeste(t)
	v.ditado(t, "d1", "só o bruto", "   ")
	n, err := v.notas.VirarNota(context.Background(), v.dono, "d1")
	if err != nil || n.Corpo != "só o bruto" {
		t.Fatalf("VirarNota = %+v, %v; quero o bruto no corpo", n, err)
	}
}

func TestVirarNota_Recusas(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d-lixeira", "b", "f")
	v.ditado(t, "d-vazio", "", "")
	v.ditado(t, "d-ok", "b", "f")
	if err := v.hist.SoftDelete(ctx, v.dono, "d-lixeira", inicioDasNotas); err != nil {
		t.Fatal(err)
	}
	if _, err := v.notas.VirarNota(ctx, v.dono, "d-ok"); err != nil {
		t.Fatal(err)
	}

	casos := []struct {
		nome, usuario, id string
		quero             error
	}{
		{"inexistente", v.dono, "nao-existe", storage.ErrNotFound},
		{"de outro usuário", "outro", "d-vazio", storage.ErrNotFound},
		{"na lixeira", v.dono, "d-lixeira", storage.ErrDitadoNaLixeira},
		{"já virou nota", v.dono, "d-ok", storage.ErrDitadoJaVirouNota},
		{"sem texto nenhum", v.dono, "d-vazio", storage.ErrNotaVazia},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			notasAntes := v.contar(t, `SELECT COUNT(*) FROM local_notes`)
			if _, err := v.notas.VirarNota(ctx, c.usuario, c.id); !errors.Is(err, c.quero) {
				t.Fatalf("erro = %v; quero %v", err, c.quero)
			}
			if got := v.contar(t, `SELECT COUNT(*) FROM local_notes`); got != notasAntes {
				t.Errorf("a recusa deixou %d nota(s) a mais", got-notasAntes)
			}
		})
	}
	// A recusa não marcou nenhum dos recusados.
	if got := v.contar(t, `SELECT COUNT(*) FROM local_transcriptions WHERE virou_nota IS NOT NULL`); got != 1 {
		t.Errorf("%d ditados marcados; quero só d-ok", got)
	}
}

func TestVirarNota_DuasVezesNaoCriaDuasNotas(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "b", "f")
	if _, err := v.notas.VirarNota(ctx, v.dono, "d1"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.notas.VirarNota(ctx, v.dono, "d1"); !errors.Is(err, storage.ErrDitadoJaVirouNota) {
		t.Fatalf("segunda vez = %v; quero ErrDitadoJaVirouNota", err)
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM local_notes`); n != 1 {
		t.Errorf("%d notas; quero 1", n)
	}
}

// Tudo ou nada: se marcar o ditado falhar depois de a nota ser gravada, a nota
// some junto. O gatilho faz o UPDATE do ditado falhar de verdade, no SQLite.
func TestVirarNota_TudoOuNada(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "b", "f")
	executar(t, v.db, `CREATE TRIGGER recusa_virou_nota BEFORE UPDATE OF virou_nota ON local_transcriptions
		BEGIN SELECT RAISE(ABORT, 'recusado de propósito'); END`)

	if _, err := v.notas.VirarNota(ctx, v.dono, "d1"); err == nil {
		t.Fatal("VirarNota passou com o UPDATE do ditado falhando")
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM local_notes`); n != 0 {
		t.Errorf("a falha deixou %d nota(s): a nota nasceu sem o ditado marcado", n)
	}
	if got := v.virouNota(t, "d1"); got.Valid {
		t.Errorf("virou_nota = %v; quero nulo", got)
	}
	if ids := v.noHistorico(t, false); len(ids) != 1 {
		t.Errorf("o histórico = %v; o ditado tem de continuar lá", ids)
	}
}

func TestDesfazerVirarNota_NotaIntactaSai(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "b", "f")
	n, err := v.notas.VirarNota(ctx, v.dono, "d1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := v.notas.DesfazerVirarNota(ctx, v.dono, n.ID)
	if err != nil {
		t.Fatalf("DesfazerVirarNota: %v", err)
	}
	if out.DitadoID != "d1" || !out.NotaApagada {
		t.Errorf("resultado = %+v; quero d1 e a nota apagada", out)
	}
	if _, err := v.notas.Get(ctx, v.dono, n.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a nota intacta continua lá: %v", err)
	}
	if got := v.virouNota(t, "d1"); got.Valid {
		t.Errorf("virou_nota = %v; quero nulo", got)
	}
	if ids := v.noHistorico(t, false); len(ids) != 1 || ids[0] != "d1" {
		t.Errorf("o histórico = %v; quero d1 de volta", ids)
	}
	// Dá para virar nota de novo.
	if _, err := v.notas.VirarNota(ctx, v.dono, "d1"); err != nil {
		t.Errorf("virar de novo depois do Desfazer: %v", err)
	}
}

func TestDesfazerVirarNota_NotaEditadaFica(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	mudancas := map[string]func(n storage.Nota) error{
		"corpo": func(n storage.Nota) error {
			c := "editado"
			_, err := v.notas.Atualizar(ctx, v.dono, n.ID, n.Version, storage.MudancaDaNota{Corpo: &c})
			return err
		},
		"fixar": func(n storage.Nota) error {
			f := true
			_, err := v.notas.Atualizar(ctx, v.dono, n.ID, n.Version, storage.MudancaDaNota{Fixada: &f})
			return err
		},
		"marcador": func(n storage.Nota) error {
			m, err := v.notas.CriarMarcador(ctx, v.dono, "x")
			if err != nil {
				return err
			}
			_, err = v.notas.Marcar(ctx, v.dono, n.ID, m.ID)
			return err
		},
		"lixeira": func(n storage.Nota) error {
			return v.notas.SoftDelete(ctx, v.dono, n.ID, inicioDasNotas)
		},
	}
	for nome, mexer := range mudancas {
		t.Run(nome, func(t *testing.T) {
			id := "d-" + nome
			v.ditado(t, id, "b", "f")
			n, err := v.notas.VirarNota(ctx, v.dono, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := mexer(n); err != nil {
				t.Fatal(err)
			}
			out, err := v.notas.DesfazerVirarNota(ctx, v.dono, n.ID)
			if err != nil {
				t.Fatalf("DesfazerVirarNota: %v", err)
			}
			if out.DitadoID != id || out.NotaApagada {
				t.Errorf("resultado = %+v; quero o ditado de volta e a nota mantida", out)
			}
			if _, err := v.notas.Get(ctx, v.dono, n.ID); err != nil {
				t.Errorf("a nota editada sumiu: %v", err)
			}
			if got := v.virouNota(t, id); got.Valid {
				t.Errorf("virou_nota = %v; o ditado devia ter voltado", got)
			}
		})
	}
}

func TestDesfazerVirarNota_Recusas(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "b", "f")
	n, err := v.notas.VirarNota(ctx, v.dono, "d1")
	if err != nil {
		t.Fatal(err)
	}
	solta := criarNotaDe(t, v, "uma nota qualquer")
	if _, err := v.notas.DesfazerVirarNota(ctx, v.dono, "nao-existe"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("inexistente = %v; quero ErrNotFound", err)
	}
	if _, err := v.notas.DesfazerVirarNota(ctx, "outro", n.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("de outro usuário = %v; quero ErrNotFound", err)
	}
	if _, err := v.notas.DesfazerVirarNota(ctx, v.dono, solta.ID); !errors.Is(err, storage.ErrNotaNaoVeioDeDitado) {
		t.Errorf("nota que não veio de ditado = %v; quero ErrNotaNaoVeioDeDitado", err)
	}
	if _, err := v.notas.Get(ctx, v.dono, solta.ID); err != nil {
		t.Errorf("a recusa apagou a nota solta: %v", err)
	}
	// O Desfazer repetido: a primeira vez devolve, a segunda não acha o que desfazer.
	c := "editada"
	if _, err := v.notas.Atualizar(ctx, v.dono, n.ID, n.Version, storage.MudancaDaNota{Corpo: &c}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.notas.DesfazerVirarNota(ctx, v.dono, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.notas.DesfazerVirarNota(ctx, v.dono, n.ID); !errors.Is(err, storage.ErrNotaNaoVeioDeDitado) {
		t.Errorf("Desfazer repetido = %v; quero ErrNotaNaoVeioDeDitado", err)
	}
	// E não apagou a nota editada.
	if _, err := v.notas.Get(ctx, v.dono, n.ID); err != nil {
		t.Errorf("o Desfazer repetido apagou a nota editada: %v", err)
	}
}

func criarNotaDe(t *testing.T, v virarDeTeste, corpo string) storage.Nota {
	t.Helper()
	n, err := v.notas.Criar(context.Background(), storage.NovaNota{UserID: v.dono, Corpo: corpo})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Apagar a nota de vez (depois de ir para a lixeira) não devolve o ditado ao
// histórico: ele fica escondido, com o id solto em virou_nota (sem FK).
func TestVirarNota_ApagarANotaDeVezNaoDevolveODitado(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "b", "f")
	n, err := v.notas.VirarNota(ctx, v.dono, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.notas.SoftDelete(ctx, v.dono, n.ID, inicioDasNotas); err != nil {
		t.Fatal(err)
	}
	if err := v.notas.ApagarDeVez(ctx, v.dono, n.ID); err != nil {
		t.Fatalf("ApagarDeVez: %v", err)
	}
	if ids := v.noHistorico(t, false); len(ids) != 0 {
		t.Errorf("o histórico = %v; o ditado não volta quando a nota é apagada de vez", ids)
	}
	if got := v.virouNota(t, "d1"); !got.Valid || got.String != n.ID {
		t.Errorf("virou_nota = %v; quero o id solto %s", got, n.ID)
	}
	// E o expurgo da lixeira das notas também deixa o ditado em paz.
	if _, err := v.notas.PurgeExpired(ctx, inicioDasNotas.AddDate(1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM local_transcriptions WHERE id = 'd1'`); n != 1 {
		t.Error("o expurgo das notas apagou o ditado")
	}
}

// O expurgo da lixeira dos ditados (30 dias) não alcança o ditado que virou
// nota: ele nunca foi para a lixeira.
func TestVirarNota_ExpurgoDaLixeiraNaoApagaODitado(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "d1", "b", "f")
	v.ditado(t, "d2", "b", "f")
	if _, err := v.notas.VirarNota(ctx, v.dono, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := v.hist.SoftDelete(ctx, v.dono, "d2", inicioDasNotas); err != nil {
		t.Fatal(err)
	}
	// Dois anos adiante: o d2 (na lixeira) sai, o d1 (que virou nota) fica.
	if _, err := v.hist.PurgeExpired(ctx, inicioDasNotas.AddDate(2, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if v.contar(t, `SELECT COUNT(*) FROM local_transcriptions WHERE id = 'd1'`) != 1 {
		t.Error("o expurgo da lixeira apagou o ditado que virou nota")
	}
	if v.contar(t, `SELECT COUNT(*) FROM local_transcriptions WHERE id = 'd2'`) != 0 {
		t.Error("o expurgo da lixeira não apagou o ditado da lixeira (o teste não prova nada)")
	}
}

// A retenção de 6 meses não alcança o ditado que virou nota, nem na prévia.
func TestVirarNota_SeisMesesNaoAlcancaODitado(t *testing.T) {
	s := novosSeisMesesDeTeste(t)
	notas := storage.NewSQLiteNotaRepository(s.db)
	ctx := context.Background()
	s.ditado(t, idSM(1), agoraSM.AddDate(0, -8, 0))
	s.ditado(t, idSM(2), agoraSM.AddDate(0, -8, 0))
	s.ditado(t, idSM(3), agoraSM.AddDate(0, -1, 0))
	if _, err := notas.VirarNota(ctx, s.dono, idSM(1)); err != nil {
		t.Fatal(err)
	}
	s.ligar(t)
	if c := s.previa(t, agoraSM); c.Total != 1 {
		t.Errorf("a prévia contou %d; quero só o idSM(2): o que virou nota fica de fora", c.Total)
	}
	if n := s.apagar(t, agoraSM); n != 1 {
		t.Errorf("apagou %d; quero 1", n)
	}
	if !s.tem(t, idSM(1)) {
		t.Error("a retenção de 6 meses apagou o ditado que virou nota")
	}
	if s.tem(t, idSM(2)) {
		t.Error("a retenção não apagou o ditado vencido comum (o teste não prova nada)")
	}
}

// A fila do sync (N7): com a nuvem conectada e o ditado que ela já tem, o
// "Virar nota" põe a nota nova (campos 0) e o virou_nota do ditado
// (CampoVirouNota) na mesma transação; o Desfazer põe o virou_nota de novo e,
// como a nota intacta sai de vez sem nunca ter subido, a lápide dela não fica.
// O ditado não sobe a versão local nem a hora (a base do sync não mexe).
func TestVirarNota_PoeANotaEOVirouNotaNaFila(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	executar(t, v.db, `INSERT INTO instalacao (nome, valor) VALUES (?, 'aparelho-1')`, storage.ChaveNuvemAparelho)
	v.ditado(t, "d1", "b", "f")
	executar(t, v.db, `UPDATE local_transcriptions SET server_version = 3 WHERE id = 'd1'`)
	executar(t, v.db, `DELETE FROM sync_outbox`)

	var versaoAntes int64
	var atualizadoAntes string
	if err := v.db.QueryRow(`SELECT version, updated_at FROM local_transcriptions WHERE id = 'd1'`).Scan(&versaoAntes, &atualizadoAntes); err != nil {
		t.Fatal(err)
	}

	n, err := v.notas.VirarNota(ctx, v.dono, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if c := v.contar(t, `SELECT COUNT(*) FROM sync_outbox WHERE entity_type = 'note' AND entity_id = '`+n.ID+`' AND campos = 0`); c != 1 {
		t.Errorf("a nota nova na fila: %d", c)
	}
	if c := v.contar(t, `SELECT COUNT(*) FROM sync_outbox WHERE entity_type = 'transcription' AND entity_id = 'd1' AND campos = 4`); c != 1 {
		t.Errorf("o virou_nota do ditado na fila: %d", c)
	}
	executar(t, v.db, `DELETE FROM sync_outbox WHERE entity_type = 'transcription'`)
	if _, err := v.notas.DesfazerVirarNota(ctx, v.dono, n.ID); err != nil {
		t.Fatal(err)
	}
	if c := v.contar(t, `SELECT COUNT(*) FROM sync_outbox WHERE entity_type = 'note'`); c != 0 {
		t.Errorf("a nota que nunca subiu deixou %d marcador(es) depois de sair de vez", c)
	}
	if c := v.contar(t, `SELECT COUNT(*) FROM sync_outbox WHERE entity_type = 'transcription' AND entity_id = 'd1' AND campos = 4`); c != 1 {
		t.Errorf("o Desfazer não pôs o virou_nota do ditado na fila: %d", c)
	}
	var versaoDepois int64
	var atualizadoDepois string
	if err := v.db.QueryRow(`SELECT version, updated_at FROM local_transcriptions WHERE id = 'd1'`).Scan(&versaoDepois, &atualizadoDepois); err != nil {
		t.Fatal(err)
	}
	if versaoDepois != versaoAntes || atualizadoDepois != atualizadoAntes {
		t.Errorf("o ditado mudou de versão (%d -> %d) ou de hora: a base do sync não pode mexer", versaoAntes, versaoDepois)
	}
}

func TestVirarNota_CorpoGrandeERecusado(t *testing.T) {
	v := novoVirarDeTeste(t)
	v.ditado(t, "d1", "b", strings.Repeat("a", storage.MaxCorpoDaNotaBytes))
	if _, err := v.notas.VirarNota(context.Background(), v.dono, "d1"); err != nil {
		t.Fatalf("no teto exato: %v", err)
	}
}
