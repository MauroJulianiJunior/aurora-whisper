package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O ditado feito dentro de uma nota (ADR-022, "Ditado feito dentro da nota"),
// contra um SQLite de verdade: a linha nasce com virou_nota (o id da nota ou
// "nova:<token>"), o token vira o id da nota na mesma transação em que ela nasce,
// e a caixa que fecha vazia devolve os ditados ao histórico.

// ditadoEscondido grava um ditado que já nasce com virou_nota = valor, como o
// Runtime faz (Put com VirouNota).
func (v virarDeTeste) ditadoEscondido(t *testing.T, id, valor string, criado time.Time) {
	t.Helper()
	tr := newTranscription(id, v.dono, criado)
	tr.VirouNota = &valor
	if err := v.hist.Put(tr); err != nil {
		t.Fatal(err)
	}
}

func TestDitadoDentroDaNota_PutGravaOVirouNotaESaiDoHistorico(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditado(t, "comum", "b", "f")
	v.ditadoEscondido(t, "escondido", "nota-1", inicioDasNotas.Add(-time.Hour))

	if got := v.virouNota(t, "escondido"); !got.Valid || got.String != "nota-1" {
		t.Errorf("virou_nota = %v", got)
	}
	if ids := v.noHistorico(t, false); len(ids) != 1 || ids[0] != "comum" {
		t.Errorf("o histórico = %v; quero só o comum", ids)
	}
	// O "Aprender" e a auditoria leem pelo id e acham o bruto, com o valor.
	tr, err := v.hist.Get(ctx, v.dono, "escondido")
	if err != nil || tr.RawText == "" || tr.VirouNota == nil || *tr.VirouNota != "nota-1" {
		t.Errorf("Get = %+v, %v", tr, err)
	}
	// Nada na fila do sync, nada de versão nova.
	if n := v.contar(t, `SELECT COUNT(*) FROM sync_outbox`); n != 0 {
		t.Errorf("sync_outbox tem %d linhas", n)
	}
}

func TestCriarComToken_LigaOsDitadosDaCaixaANota(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditadoEscondido(t, "d1", storage.PrefixoDaNotaNova+"tk-1", inicioDasNotas.Add(-2*time.Hour))
	v.ditadoEscondido(t, "d2", storage.PrefixoDaNotaNova+"tk-1", inicioDasNotas.Add(-time.Hour))
	v.ditadoEscondido(t, "outra-caixa", storage.PrefixoDaNotaNova+"tk-2", inicioDasNotas.Add(-time.Hour))
	v.ditado(t, "comum", "b", "f")

	n, err := v.notas.Criar(ctx, storage.NovaNota{UserID: v.dono, Corpo: "- leite", Token: "tk-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"d1", "d2"} {
		if got := v.virouNota(t, id); !got.Valid || got.String != n.ID {
			t.Errorf("%s: virou_nota = %v; quero %s", id, got, n.ID)
		}
	}
	// A caixa de outro token e o ditado comum ficam como estavam.
	if got := v.virouNota(t, "outra-caixa"); got.String != "nova:tk-2" {
		t.Errorf("outra caixa: %v", got)
	}
	if got := v.virouNota(t, "comum"); got.Valid {
		t.Errorf("o comum foi marcado: %v", got)
	}
	// Estes ditados NÃO são a origem da nota: isso é só do Virar nota.
	if n.DitadoDeOrigem != nil {
		t.Errorf("DitadoDeOrigem = %q; quero nil", *n.DitadoDeOrigem)
	}
	if ids := v.noHistorico(t, false); len(ids) != 1 || ids[0] != "comum" {
		t.Errorf("o histórico = %v", ids)
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM sync_outbox`); n != 0 {
		t.Errorf("sync_outbox tem %d linhas", n)
	}
}

func TestCriarComToken_SemDitadoNaoFazNada(t *testing.T) {
	v := novoVirarDeTeste(t)
	if _, err := v.notas.Criar(context.Background(), storage.NovaNota{UserID: v.dono, Corpo: "x", Token: "tk-vazio"}); err != nil {
		t.Fatalf("token sem ditado: %v", err)
	}
}

func TestCriarComToken_TokenInvalidoERecusado(t *testing.T) {
	v := novoVirarDeTeste(t)
	for _, tk := range []string{"com espaço", "a:b", "%", "x/y"} {
		_, err := v.notas.Criar(context.Background(), storage.NovaNota{UserID: v.dono, Corpo: "x", Token: tk})
		if !errors.Is(err, storage.ErrTokenInvalido) {
			t.Errorf("token %q: %v; quero ErrTokenInvalido", tk, err)
		}
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM local_notes`); n != 0 {
		t.Errorf("nasceram %d notas apesar do token inválido", n)
	}
}

// Tudo ou nada: se o vínculo falha, a nota não nasce.
func TestCriarComToken_TudoOuNada(t *testing.T) {
	v := novoVirarDeTeste(t)
	v.ditadoEscondido(t, "d1", storage.PrefixoDaNotaNova+"tk-1", inicioDasNotas.Add(-time.Hour))
	if _, err := v.db.Exec(`CREATE TRIGGER falha_vinculo BEFORE UPDATE OF virou_nota ON local_transcriptions
		BEGIN SELECT RAISE(ABORT, 'falha de teste'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := v.notas.Criar(context.Background(), storage.NovaNota{UserID: v.dono, Corpo: "x", Token: "tk-1"}); err == nil {
		t.Fatal("o vínculo falhou e a nota nasceu")
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM local_notes`); n != 0 {
		t.Errorf("a nota ficou (%d) sem o vínculo", n)
	}
	if got := v.virouNota(t, "d1"); got.String != "nova:tk-1" {
		t.Errorf("virou_nota = %v", got)
	}
}

func TestSoltarDitadosDaNotaNova_DevolveAoHistorico(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditadoEscondido(t, "d1", storage.PrefixoDaNotaNova+"tk-1", inicioDasNotas.Add(-2*time.Hour))
	v.ditadoEscondido(t, "d2", storage.PrefixoDaNotaNova+"tk-1", inicioDasNotas.Add(-time.Hour))
	v.ditadoEscondido(t, "outra", storage.PrefixoDaNotaNova+"tk-2", inicioDasNotas.Add(-time.Hour))
	v.ditadoEscondido(t, "de-nota", "nota-7", inicioDasNotas.Add(-time.Hour))

	n, err := v.notas.SoltarDitadosDaNotaNova(ctx, v.dono, "tk-1")
	if err != nil || n != 2 {
		t.Fatalf("soltou %d, %v; quero 2", n, err)
	}
	if ids := v.noHistorico(t, false); len(ids) != 2 {
		t.Errorf("o histórico = %v; quero d1 e d2 de volta", ids)
	}
	if got := v.virouNota(t, "outra"); got.String != "nova:tk-2" {
		t.Errorf("a caixa de outro token foi solta: %v", got)
	}
	if got := v.virouNota(t, "de-nota"); got.String != "nota-7" {
		t.Errorf("o ditado de uma nota foi solto: %v", got)
	}
	// Repetir não acha mais nada; token inválido e dono vazio são recusados.
	if n, err := v.notas.SoltarDitadosDaNotaNova(ctx, v.dono, "tk-1"); err != nil || n != 0 {
		t.Errorf("repetir: %d, %v", n, err)
	}
	if _, err := v.notas.SoltarDitadosDaNotaNova(ctx, v.dono, "a:b"); !errors.Is(err, storage.ErrTokenInvalido) {
		t.Errorf("token inválido: %v", err)
	}
	if _, err := v.notas.SoltarDitadosDaNotaNova(ctx, "", "tk-1"); err == nil {
		t.Error("dono vazio aceito")
	}
	// De outro dono, nada.
	if n, err := v.notas.SoltarDitadosDaNotaNova(ctx, "outro-dono", "tk-2"); err != nil || n != 0 {
		t.Errorf("outro dono soltou %d, %v", n, err)
	}
}

func TestLiberarDitadosDeNotasNovas_SoOsAbandonados(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	agora := inicioDasNotas
	v.ditadoEscondido(t, "velho", storage.PrefixoDaNotaNova+"tk-velho", agora.Add(-48*time.Hour))
	v.ditadoEscondido(t, "recente", storage.PrefixoDaNotaNova+"tk-novo", agora.Add(-time.Hour))
	v.ditadoEscondido(t, "de-nota", "nota-7", agora.Add(-48*time.Hour))

	n, err := v.notas.LiberarDitadosDeNotasNovas(ctx, agora.Add(-storage.CarenciaDaNotaNova))
	if err != nil || n != 1 {
		t.Fatalf("liberou %d, %v; quero 1", n, err)
	}
	if got := v.virouNota(t, "velho"); got.Valid {
		t.Errorf("o abandonado continua escondido: %v", got)
	}
	if got := v.virouNota(t, "recente"); got.String != "nova:tk-novo" {
		t.Errorf("o recente foi solto: %v", got)
	}
	if got := v.virouNota(t, "de-nota"); got.String != "nota-7" {
		t.Errorf("o ditado de uma nota foi solto: %v", got)
	}
}

// O expurgo das notas carrega a limpeza das caixas abandonadas, com a carência.
func TestPurgeExpired_LiberaAsCaixasAbandonadas(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditadoEscondido(t, "velho", storage.PrefixoDaNotaNova+"tk-velho", inicioDasNotas.Add(-time.Hour))

	if _, err := v.notas.PurgeExpired(ctx, inicioDasNotas); err != nil {
		t.Fatal(err)
	}
	if got := v.virouNota(t, "velho"); !got.Valid {
		t.Error("liberou antes da carência")
	}
	if _, err := v.notas.PurgeExpired(ctx, inicioDasNotas.Add(storage.CarenciaDaNotaNova)); err != nil {
		t.Fatal(err)
	}
	if got := v.virouNota(t, "velho"); got.Valid {
		t.Errorf("passou a carência e continua escondido: %v", got)
	}
}

func TestDitadoDentroDaNota_ExpurgosNaoAlcancam(t *testing.T) {
	v := novoVirarDeTeste(t)
	ctx := context.Background()
	v.ditadoEscondido(t, "d1", "nota-1", inicioDasNotas.AddDate(-2, 0, 0))
	if _, err := v.hist.PurgeExpired(ctx, inicioDasNotas.AddDate(2, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if n := v.contar(t, `SELECT COUNT(*) FROM local_transcriptions WHERE id = 'd1'`); n != 1 {
		t.Error("o expurgo da lixeira apagou o ditado escondido")
	}
}
