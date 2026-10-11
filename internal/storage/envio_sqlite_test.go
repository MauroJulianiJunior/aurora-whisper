package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O envio inicial (ADR-019, resposta 3 e "Decisões da F7c"): o filtro único
// (a prévia é o que o clique marca), a cópia antes de marcar, os blocos, o
// aceite, a completação a cada rodada, a ordem da fila e o progresso. Cada
// teste roda num historico.db de verdade, numa pasta temporária.

// envioDeTeste é a fila de teste com o caminho do banco (a cópia fica ao lado
// dele).
type envioDeTeste struct {
	filaDeTeste
	caminho string
}

func novoEnvioDeTeste(t *testing.T) envioDeTeste {
	t.Helper()
	caminho, db := bancoEmArquivo(t)
	repo := storage.NewSQLiteTranscriptionRepository(db)
	var avisos atomic.Int64
	repo.AoEnfileirar(func() { avisos.Add(1) })
	return envioDeTeste{filaDeTeste{db: db, repo: repo, fila: repo.Fila(donoDaFila), avisos: &avisos}, caminho}
}

// agoraDoEnvio é o "agora" dos testes do envio.
var agoraDoEnvio = time.Date(2026, 10, 6, 15, 30, 0, 0, time.UTC)

// semear grava uma linha direto no banco, com os campos que o filtro olha.
type semente struct {
	id        string
	dono      string
	criado    time.Time
	lixeira   bool
	servidor  *int64
	aparelho  string
	situacao  string
	comMarcas bool
}

func (e envioDeTeste) semear(t *testing.T, s semente) {
	t.Helper()
	if s.dono == "" {
		s.dono = donoDaFila
	}
	if s.situacao == "" {
		s.situacao = storage.SituacaoNormal
	}
	tr := newTranscription(s.id, s.dono, s.criado)
	tr.FinalText = "texto de " + s.id + " com acento: ação"
	tr.SyncState = s.situacao
	tr.ServerVersion = s.servidor
	if s.aparelho != "" {
		a := s.aparelho
		tr.DeviceID = &a
	}
	if s.lixeira {
		d := s.criado.Add(time.Minute)
		p := d.AddDate(0, 0, 30)
		tr.DeletedAt, tr.PurgeAt = &d, &p
	}
	// A semente é o banco como ele está: sem marcador, a não ser comMarcas (o
	// Put põe o dele numa linha com server_version, e ele sai).
	if err := e.repo.Put(tr); err != nil {
		t.Fatal(err)
	}
	executar(t, e.db, `DELETE FROM sync_outbox WHERE entity_id = ?`, s.id)
	if s.comMarcas {
		executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES ('transcription', ?, 1, ?)`,
			s.id, "2026-10-06T00:00:00.000000000Z")
	}
}

func id(n int) string { return fmt.Sprintf("0199c000-0000-7000-8000-%012d", n) }

func versao(v int64) *int64 { return &v }

// marcadosInicial devolve os ids dos marcadores do envio inicial.
func (e envioDeTeste) marcadosInicial(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range e.marcadores(t) {
		if m.Inicial {
			out = append(out, m.EntityID)
		}
	}
	sort.Strings(out)
	return out
}

func (e envioDeTeste) chave(t *testing.T, nome string) (string, bool) {
	t.Helper()
	v, ok, err := storage.NovaInstalacao(e.db).Ler(context.Background(), nome)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

func copiasDoEnvio(t *testing.T, caminho string) []string {
	t.Helper()
	achadas, err := filepath.Glob(caminho + ".antes-da-nuvem-*.db")
	if err != nil {
		t.Fatal(err)
	}
	return achadas
}

// Cada filtro da elegibilidade, um ditado para cada, e a prova de que a
// prévia é exatamente o que o clique marca.
func TestEnvio_CadaFiltroEPreviaIgualAoQueMarca(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	seisMeses := agoraDoEnvio.AddDate(0, -6, 0)
	sementes := []semente{
		{id: id(1), criado: agoraDoEnvio.Add(-time.Hour)},                                         // sobe
		{id: id(2), criado: seisMeses.Add(time.Second)},                                           // sobe: um segundo dentro da janela
		{id: id(3), criado: seisMeses},                                                            // fica: no limite, fora (a conta da nuvem)
		{id: id(4), criado: seisMeses.Add(-48 * time.Hour)},                                       // fica: mais de 6 meses
		{id: id(5), criado: agoraDoEnvio.Add(-time.Hour), lixeira: true},                          // fica: lixeira
		{id: id(6), criado: agoraDoEnvio.Add(-time.Hour), servidor: versao(3)},                    // fica: a nuvem já tem
		{id: id(7), criado: agoraDoEnvio.Add(-time.Hour), aparelho: id(900)},                      // fica: veio de outro PC
		{id: id(8), criado: agoraDoEnvio.Add(-time.Hour), situacao: storage.SituacaoRecusado},     // fica: recusado
		{id: id(9), criado: agoraDoEnvio.Add(-time.Hour), situacao: storage.SituacaoForaDaJanela}, // fica: fora da janela
		{id: id(10), criado: agoraDoEnvio.Add(-time.Hour), comMarcas: true},                       // fica: já está na fila
		{id: id(11), criado: agoraDoEnvio.Add(-time.Hour), dono: "outro-dono"},                    // fica: outro dono
		{id: id(12), criado: agoraDoEnvio.Add(-24 * time.Hour * 100)},                             // sobe: cem dias
	}
	for _, s := range sementes {
		e.semear(t, s)
	}

	previa, err := e.fila.Previa(ctx, agoraDoEnvio)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio)
	if err != nil {
		t.Fatal(err)
	}
	quero := []string{id(1), id(2), id(12)}
	sort.Strings(quero)
	if got := e.marcadosInicial(t); strings.Join(got, ",") != strings.Join(quero, ",") {
		t.Fatalf("marcados %v; quero %v", got, quero)
	}
	if previa != len(quero) || r.Marcados != len(quero) {
		t.Fatalf("prévia %d, marcados %d; quero %d nos dois", previa, r.Marcados, len(quero))
	}
	// Os marcadores são de criar (sem campo), fora de voo; o que já estava
	// na fila ficou como estava.
	ms := e.marcadores(t)
	for _, q := range quero {
		if m := ms[q]; m.Campos != 0 || m.EmVoo() {
			t.Errorf("%s: %+v", q, m)
		}
	}
	if m := ms[id(10)]; m.Inicial || m.Campos != 1 {
		t.Errorf("o marcador que já existia mudou: %+v", m)
	}
	if n, _ := e.fila.Previa(ctx, agoraDoEnvio); n != 0 {
		t.Errorf("a prévia depois do envio: %d", n)
	}
	if v, ok := e.chave(t, storage.ChaveNuvemEnvioTotal); !ok || v != "3" {
		t.Errorf("o total do envio: %q, %v", v, ok)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEnvioAceitoEm); !ok {
		t.Error("o aceite não foi gravado")
	}
}

// A cópia vem antes de marcar: o arquivo existe ao lado do banco, abre e tem
// tudo (as mesmas linhas, tabela por tabela, e nenhum marcador do envio), e
// fica registrado onde ela está. Nenhuma linha do histórico muda.
func TestEnvio_CopiaAntesDeMarcar(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	for i := 1; i <= 20; i++ {
		e.semear(t, semente{id: id(i), criado: agoraDoEnvio.Add(-time.Duration(i) * time.Hour), lixeira: i%5 == 0})
	}
	antes := e.todasAsLinhas(t)
	r, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio)
	if err != nil {
		t.Fatal(err)
	}
	if r.Copia != storage.CaminhoDaCopiaDoEnvio(e.caminho, agoraDoEnvio) || r.Marcados != 16 {
		t.Fatalf("resultado: %+v", r)
	}
	if v, _ := e.chave(t, storage.ChaveNuvemCopia); v != r.Copia {
		t.Errorf("a cópia registrada: %q", v)
	}
	c, err := storage.AbrirSomenteLeitura(r.Copia)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var n, marcas int
	if err := c.QueryRow(`SELECT COUNT(*) FROM local_transcriptions`).Scan(&n); err != nil || n != 20 {
		t.Fatalf("a cópia tem %d ditados (%v); quero 20", n, err)
	}
	if err := c.QueryRow(`SELECT COUNT(*) FROM sync_outbox`).Scan(&marcas); err != nil || marcas != 0 {
		t.Errorf("a cópia tem %d marcadores (%v): ela é de antes de marcar", marcas, err)
	}
	if depois := e.todasAsLinhas(t); !iguais(antes, depois) {
		t.Errorf("o envio mudou o histórico:\nantes  %v\ndepois %v", antes, depois)
	}
}

// A cópia que falha não deixa marcador nenhum, nem o aceite, nem o registro:
// com MaxCopias cópias do envio já ao lado (ErrCopiasDemais) e com o destino
// ocupado. As cópias de antes do dono são de outra marca e não contam.
func TestEnvio_CopiaQueFalhaNaoMarcaNada(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-time.Hour)})
	pasta := filepath.Dir(e.caminho)
	escrever := func(nome string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pasta, nome), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	nenhum := func(onde string) {
		t.Helper()
		if ms := e.marcadores(t); len(ms) != 0 {
			t.Errorf("%s: %d marcadores", onde, len(ms))
		}
		for _, k := range []string{storage.ChaveNuvemEnvioAceitoEm, storage.ChaveNuvemCopia, storage.ChaveNuvemEnvioTotal} {
			if _, ok := e.chave(t, k); ok {
				t.Errorf("%s: a chave %s foi gravada", onde, k)
			}
		}
	}

	// O destino ocupado (o mesmo segundo): nunca é sobrescrito.
	escrever(filepath.Base(storage.CaminhoDaCopiaDoEnvio(e.caminho, agoraDoEnvio)))
	_, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio)
	var ec *storage.ErroDaCopia
	if !errors.As(err, &ec) || !errors.Is(err, storage.ErrCopiaDoEnvio) || errors.Is(err, storage.ErrCopiasDemais) {
		t.Fatalf("o destino ocupado: %v", err)
	}
	if strings.Contains(err.Error(), pasta) {
		t.Errorf("o erro leva o caminho: %v", err)
	}
	nenhum("destino ocupado")

	// Três do dono não contam; três do envio, sim.
	for i := 1; i <= storage.MaxCopias; i++ {
		escrever(fmt.Sprintf("historico.db.antes-do-dono-2026090%d-101500.db", i))
	}
	if _, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio.Add(time.Second)); err != nil {
		t.Fatalf("as cópias do dono barraram o envio: %v", err)
	}
	executar(t, e.db, `DELETE FROM sync_outbox`)
	executar(t, e.db, `DELETE FROM instalacao WHERE nome LIKE 'nuvem_%'`)
	// Agora há duas do envio (a ocupada e a feita); com mais uma, são três.
	escrever("historico.db.antes-da-nuvem-20261001-000000.db")
	_, err = e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio.Add(2*time.Second))
	if !errors.Is(err, storage.ErrCopiasDemais) || !errors.Is(err, storage.ErrCopiaDoEnvio) {
		t.Fatalf("com %d cópias do envio: %v", storage.MaxCopias, err)
	}
	nenhum("cópias demais")
	if n := len(copiasDoEnvio(t, e.caminho)); n != storage.MaxCopias {
		t.Errorf("%d cópias do envio; quero %d (nenhuma nova)", n, storage.MaxCopias)
	}
}

// Sem nada elegível, não há cópia: só o aceite.
func TestEnvio_NadaElegivelSemCopia(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-time.Hour), lixeira: true})
	r, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio)
	if err != nil || r.Marcados != 0 || r.Copia != "" {
		t.Fatalf("%+v, %v", r, err)
	}
	if len(copiasDoEnvio(t, e.caminho)) != 0 {
		t.Error("fez cópia sem nada a enviar")
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEnvioAceitoEm); !ok {
		t.Error("sem o aceite")
	}
}

// Os blocos e a queda no meio: o segundo bloco falha (um gatilho temporário
// na conexão do teste recusa um id dele); o primeiro fica, com o total dele e
// a cópia registrada, e sem o aceite. O clique seguinte continua de onde
// parou, sem outra cópia, e o total fecha. Clicar de novo depois não duplica
// nada.
func TestEnvio_BlocosQuedaNoMeioEDoisCliques(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	const n = 1200
	tx, err := e.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if _, err := tx.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
			asr_provider, status, created_at, updated_at, version, sync_state)
			VALUES (?, ?, 'windows', 'b', 'f', 'smart', 'local', 'done', ?, ?, 1, 'pending')`,
			id(i), donoDaFila, emTextoDeTeste(agoraDoEnvio.Add(-time.Duration(i)*time.Minute)),
			emTextoDeTeste(agoraDoEnvio)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Os elegíveis vão do mais antigo ao mais novo: id(1200) é o primeiro,
	// e o segundo bloco começa no 500º.
	executar(t, e.db, `CREATE TEMP TRIGGER falha_no_segundo BEFORE INSERT ON sync_outbox
		WHEN NEW.entity_id = '`+id(n-storage.BlocoDoEnvio-10)+`' BEGIN SELECT RAISE(ABORT, 'falha de teste'); END`)

	if _, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio); err == nil {
		t.Fatal("o segundo bloco não falhou")
	}
	if got := len(e.marcadosInicial(t)); got != storage.BlocoDoEnvio {
		t.Fatalf("depois da queda: %d marcadores; quero o primeiro bloco (%d)", got, storage.BlocoDoEnvio)
	}
	if v, _ := e.chave(t, storage.ChaveNuvemEnvioTotal); v != fmt.Sprint(storage.BlocoDoEnvio) {
		t.Errorf("o total depois da queda: %q", v)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEnvioAceitoEm); ok {
		t.Error("o aceite foi gravado com o envio pela metade")
	}
	copia, ok := e.chave(t, storage.ChaveNuvemCopia)
	if !ok || len(copiasDoEnvio(t, e.caminho)) != 1 {
		t.Fatalf("a cópia: %q, %v, %v", copia, ok, copiasDoEnvio(t, e.caminho))
	}

	executar(t, e.db, `DROP TRIGGER falha_no_segundo`)
	r, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if r.Copia != "" || len(copiasDoEnvio(t, e.caminho)) != 1 || r.Marcados != n-storage.BlocoDoEnvio {
		t.Fatalf("o segundo clique: %+v, cópias %v", r, copiasDoEnvio(t, e.caminho))
	}
	if got := len(e.marcadosInicial(t)); got != n {
		t.Fatalf("%d marcadores; quero %d", got, n)
	}
	feitos, total, err := e.fila.Progresso(ctx)
	if err != nil || feitos != 0 || total != n {
		t.Fatalf("o progresso: %d de %d (%v)", feitos, total, err)
	}

	r, err = e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio.Add(2*time.Minute))
	if err != nil || !r.JaAceito || r.Marcados != 0 {
		t.Fatalf("o terceiro clique: %+v, %v", r, err)
	}
	if got := len(e.marcadores(t)); got != n {
		t.Errorf("o terceiro clique mexeu na fila: %d", got)
	}
}

// A completação a cada rodada: sem o aceite e com algo elegível, nada; sem o
// aceite e sem nada elegível, grava o aceite; com o aceite, marca o que ficou
// de fora fora do progresso (inicial 0), e apaga o total quando o envio
// inicial acabou.
func TestEnvio_CompletarSoComAceite(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-time.Hour)})
	n, err := e.fila.CompletarEnvio(ctx, agoraDoEnvio, 0)
	if err != nil || n != 0 || len(e.marcadores(t)) != 0 {
		t.Fatalf("sem o aceite: %d, %v, %v", n, err, e.marcadores(t))
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEnvioAceitoEm); ok {
		t.Fatal("o aceite foi gravado com algo a enviar")
	}

	if _, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	// Um ditado novo feito desconectado e um restaurado desconectado.
	e.semear(t, semente{id: id(2), criado: agoraDoEnvio.Add(-time.Minute)})
	e.semear(t, semente{id: id(3), criado: agoraDoEnvio.Add(-2 * time.Minute), lixeira: true})
	if err := e.repo.Restore(ctx, donoDaFila, id(3)); err != nil {
		t.Fatal(err)
	}
	if len(e.marcadores(t)) != 1 {
		t.Fatalf("desconectado, os dois entraram na fila: %v", e.marcadores(t))
	}
	n, err = e.fila.CompletarEnvio(ctx, agoraDoEnvio, 0)
	if err != nil || n != 2 {
		t.Fatalf("a completação: %d, %v", n, err)
	}
	ms := e.marcadores(t)
	if ms[id(2)].Inicial || ms[id(3)].Inicial || !ms[id(1)].Inicial {
		t.Errorf("os marcadores: %+v", ms)
	}
	if feitos, total, _ := e.fila.Progresso(ctx); feitos != 0 || total != 1 {
		t.Errorf("a completação entrou no progresso: %d de %d", feitos, total)
	}
	// O envio inicial acaba (o ditado sobe: ganha a versão, e o marcador
	// sai): o total é apagado na rodada seguinte.
	executar(t, e.db, `UPDATE local_transcriptions SET server_version = 1 WHERE id = ?`, id(1))
	executar(t, e.db, `DELETE FROM sync_outbox WHERE inicial = 1`)
	if _, err := e.fila.CompletarEnvio(ctx, agoraDoEnvio, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEnvioTotal); ok {
		t.Error("o total ficou depois do fim")
	}

	// Um banco sem nada a enviar: a rodada grava o aceite sozinha.
	e2 := novoEnvioDeTeste(t)
	e2.semear(t, semente{id: id(9), criado: agoraDoEnvio.Add(-time.Hour), lixeira: true})
	if _, err := e2.fila.CompletarEnvio(ctx, agoraDoEnvio, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := e2.chave(t, storage.ChaveNuvemEnvioAceitoEm); !ok {
		t.Error("sem nada a enviar, o aceite não foi gravado")
	}
	if len(copiasDoEnvio(t, e2.caminho)) != 0 || len(e2.marcadores(t)) != 0 {
		t.Error("o aceite sozinho fez cópia ou marcou")
	}
}

// A ordem da fila: o que está em voo, depois o ditado novo e as edições (na
// ordem em que entraram), e por último o histórico.
func TestEnvio_OrdemDosPendentes(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	for i := 1; i <= 3; i++ {
		e.semear(t, semente{id: id(i), criado: agoraDoEnvio.Add(-time.Duration(10+i) * time.Hour)})
	}
	if _, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	e.conectar(t)
	e.ditar(t, id(10), agoraDoEnvio) // o ditado novo, durante o envio
	if err := e.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeDitado, EntityID: id(2), MutationID: "m", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	itens, err := e.fila.Pendentes(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var ordem []string
	for _, it := range itens {
		ordem = append(ordem, it.Marcador.EntityID)
	}
	// O em voo, o novo, e os do histórico (marcados juntos, na ordem do id:
	// com UUID v7, a ordem em que foram ditados).
	quero := []string{id(2), id(10), id(1), id(3)}
	if strings.Join(ordem, ",") != strings.Join(quero, ",") {
		t.Errorf("a ordem: %v; quero %v", ordem, quero)
	}
}

// O progresso: feitos = total menos os que faltam; uma recusa (o marcador
// sai) conta como feito; sem nenhum, não há envio.
func TestEnvio_Progresso(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	for i := 1; i <= 5; i++ {
		e.semear(t, semente{id: id(i), criado: agoraDoEnvio.Add(-time.Duration(i) * time.Hour)})
	}
	if _, err := e.fila.EnviarHistorico(ctx, e.caminho, agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	if f, tot, _ := e.fila.Progresso(ctx); f != 0 || tot != 5 {
		t.Fatalf("no começo: %d de %d", f, tot)
	}
	// Dois sobem (a versão da nuvem, e o marcador sai); um é recusado.
	executar(t, e.db, `UPDATE local_transcriptions SET server_version = 1 WHERE id IN (?, ?)`, id(1), id(2))
	executar(t, e.db, `DELETE FROM sync_outbox WHERE entity_id IN (?, ?)`, id(1), id(2))
	if err := e.fila.Recusar(ctx, []string{id(3)}, "texto_grande"); err != nil {
		t.Fatal(err)
	}
	if f, tot, _ := e.fila.Progresso(ctx); f != 3 || tot != 5 {
		t.Fatalf("no meio: %d de %d", f, tot)
	}
	r, err := e.fila.Resumo(ctx, agoraDoEnvio)
	if err != nil {
		t.Fatal(err)
	}
	if r.Feitos != 3 || r.Total != 5 || r.NaoSubiram != 1 || r.Pendentes != 2 || !r.Aceito || r.Previa != 0 {
		t.Errorf("o resumo: %+v", r)
	}
	executar(t, e.db, `DELETE FROM sync_outbox`)
	if f, tot, _ := e.fila.Progresso(ctx); f != 0 || tot != 0 {
		t.Errorf("no fim: %d de %d", f, tot)
	}
}

// O resumo antes do envio e com a rodada anotada.
func TestEnvio_Resumo(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-time.Hour)})
	e.semear(t, semente{id: id(2), criado: agoraDoEnvio.Add(-2 * time.Hour)})
	r, err := e.fila.Resumo(ctx, agoraDoEnvio)
	if err != nil {
		t.Fatal(err)
	}
	if r.Previa != 2 || r.Aceito || r.Total != 0 || r.ConferidoEm != nil || r.Falhou || r.OutraNuvem {
		t.Fatalf("antes: %+v", r)
	}
	preparado(t, e.filaDeTeste)
	if err := e.fila.RegistrarRodada(ctx, "a1", agoraDoEnvio, "", true); err != nil {
		t.Fatal(err)
	}
	r, _ = e.fila.Resumo(ctx, agoraDoEnvio)
	if r.ConferidoEm == nil || !r.ConferidoEm.Equal(agoraDoEnvio) || r.Falhou {
		t.Errorf("depois de uma rodada: %+v", r)
	}
	if err := e.fila.RegistrarRodada(ctx, "a1", agoraDoEnvio, "rede", false); err != nil {
		t.Fatal(err)
	}
	if r, _ = e.fila.Resumo(ctx, agoraDoEnvio); !r.Falhou {
		t.Errorf("depois de uma falha: %+v", r)
	}
}

// As mudanças da fila (reconferência da F7c), o número da pergunta do
// esquecer: a edição e a escolha do selo num ditado que a nuvem tem, a
// reconciliação e a criação que ficou em voo e voltou com a versão da nuvem
// (editada durante o voo) contam; o histórico do envio inicial e o ditado
// novo que ainda não subiram, não. Os Pendentes contam todos.
func TestEnvio_ResumoContaSoAsMudancas(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	for i := 1; i <= 7; i++ {
		e.semear(t, semente{id: id(i), criado: agoraDoEnvio.Add(-time.Duration(i) * time.Hour)})
	}
	executar(t, e.db, `UPDATE local_transcriptions SET server_version = 3 WHERE id IN (?, ?, ?)`, id(4), id(5), id(7))
	marcar := func(x string, campos, inicial int, escolha any) {
		var em any
		if escolha != nil {
			em = emTextoDeTeste(agoraDoEnvio)
		}
		executar(t, e.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, inicial, selo_escolha, selo_em)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, storage.EntidadeDitado, x, campos, emTextoDeTeste(agoraDoEnvio), inicial, escolha, em)
	}
	marcar(id(1), 0, 1, nil)                                       // o histórico do envio inicial
	marcar(id(2), 0, 1, nil)                                       // idem
	marcar(id(3), 0, 0, nil)                                       // o ditado novo
	marcar(id(4), storage.CampoTexto, 0, nil)                      // a edição
	marcar(id(5), 0, 0, storage.EscolhaManter)                     // a escolha do selo
	marcar(id(6), storage.CampoTexto|storage.CampoLixeira, 0, nil) // a reconciliação
	marcar(id(7), 0, 1, nil)                                       // o histórico, de volta do voo com a versão
	r, err := e.fila.Resumo(ctx, agoraDoEnvio)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pendentes != 7 || r.Mudancas != 4 {
		t.Errorf("pendentes %d, mudanças %d; quero 7 e 4", r.Pendentes, r.Mudancas)
	}
}

// A guarda do emitir (D2): o ditado que veio de outro PC e que a nuvem de
// agora não tem (depois do esquecer) não entra na fila, mesmo conectado; com
// a versão da nuvem, entra.
func TestEmissao_DeOutroPCSemVersaoNaoSobe(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.conectar(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio, aparelho: id(900)})
	e.semear(t, semente{id: id(2), criado: agoraDoEnvio, aparelho: id(900), servidor: versao(4)})
	executar(t, e.db, `DELETE FROM sync_outbox`)
	for _, x := range []string{id(1), id(2)} {
		if _, err := e.repo.UpdateFinalText(ctx, donoDaFila, x, 1, "editado aqui"); err != nil {
			t.Fatal(err)
		}
		if err := e.repo.SoftDelete(ctx, donoDaFila, x, agoraDoEnvio); err != nil {
			t.Fatal(err)
		}
	}
	ms := e.marcadores(t)
	if _, ok := ms[id(1)]; ok {
		t.Error("o ditado de outro PC, sem a versão desta nuvem, entrou na fila")
	}
	if m, ok := ms[id(2)]; !ok || m.Campos != storage.CampoTexto|storage.CampoLixeira {
		t.Errorf("o ditado de outro PC com a versão: %+v, %v", m, ok)
	}
}

// todasAsLinhas é o histórico inteiro do banco, cada linha como coluna ->
// valor, em ordem de id.
func (e envioDeTeste) todasAsLinhas(t *testing.T) []map[string]any {
	t.Helper()
	return linhasDaTabela(t, e.db, "local_transcriptions")
}

func linhasDaTabela(t *testing.T, db *sql.DB, tabela string) []map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM "` + tabela + `" ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out
}

func iguais(a, b []map[string]any) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func emTextoDeTeste(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// A referência da reconciliação (D3): sem esquecer, toda linha sem a versão
// da nuvem conta como mudada aqui; depois do esquecer, só a que mudou depois
// dele, ou a que ele anotou (sync_mudou_aqui); o recomeço de um epoch novo
// tira a referência e o anotado. A linha com a versão da nuvem nunca conta.
func TestAplicar_MudouAquiPelaReferencia(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio.Add(-2 * time.Hour)}) // updated_at = criado
	e.semear(t, semente{id: id(2), criado: agoraDoEnvio.Add(-time.Minute)})
	e.semear(t, semente{id: id(3), criado: agoraDoEnvio.Add(-2 * time.Hour), servidor: versao(4)})
	preparado(t, e.filaDeTeste)
	vistos := map[string]bool{}
	ver := func(st storage.EstadoDoItem) storage.AcaoLocal {
		vistos[st.ID] = st.MudouAqui
		return storage.AcaoLocal{}
	}
	ids := []string{id(1), id(2), id(3)}
	if err := e.fila.Aplicar(ctx, "a1", "e1", ids, nil, ver); err != nil {
		t.Fatal(err)
	}
	if !vistos[id(1)] || !vistos[id(2)] {
		t.Errorf("sem esquecer, a linha sem versão não contou como mudada aqui: %v", vistos)
	}
	// O esquecer foi há uma hora: a linha 1 é de antes (velha), a 2, depois.
	executar(t, e.db, `UPDATE local_transcriptions SET server_version = 4 WHERE id = ?`, id(3))
	executar(t, e.db, `INSERT INTO instalacao (nome, valor) VALUES (?, ?)`, storage.ChaveNuvemEsquecidaEm,
		emTextoDeTeste(agoraDoEnvio.Add(-time.Hour)))
	if err := e.fila.Aplicar(ctx, "a1", "e1", ids, nil, ver); err != nil {
		t.Fatal(err)
	}
	if vistos[id(1)] || !vistos[id(2)] || vistos[id(3)] {
		t.Errorf("com o esquecer: %v; quero só a 2", vistos)
	}
	// A linha 1 mudou antes do esquecer sem a nuvem receber: o esquecer a
	// anotou, e ela conta (a 3, com a versão da nuvem, nunca).
	executar(t, e.db, `INSERT INTO sync_mudou_aqui (entity_type, entity_id) VALUES (?, ?), (?, ?)`,
		storage.EntidadeDitado, id(1), storage.EntidadeDitado, id(3))
	if err := e.fila.Aplicar(ctx, "a1", "e1", ids, nil, ver); err != nil {
		t.Fatal(err)
	}
	if !vistos[id(1)] || !vistos[id(2)] || vistos[id(3)] {
		t.Errorf("com o anotado: %v; quero a 1 e a 2", vistos)
	}
	// A primeira rodada de um pareamento novo (sem cursor) também recomeça
	// pelo Preparar, e MANTÉM a referência: é nela que o PC que voltou à
	// mesma conta reconcilia.
	executar(t, e.db, `DELETE FROM sync_state`)
	if _, inicio, err := e.fila.Preparar(ctx, "a2", "p2", agoraDoEnvio); err != nil || inicio != storage.InicioRecomeco {
		t.Fatalf("Preparar: %v, %v", inicio, err)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEsquecidaEm); !ok {
		t.Fatal("a primeira rodada do pareamento tirou a referência do esquecer")
	}
	if n := len(linhasDaTabela(t, e.db, "sync_mudou_aqui")); n != 2 {
		t.Fatalf("a primeira rodada do pareamento deixou %d anotados; quero os 2", n)
	}
	if err := e.fila.Recomecar(ctx, "a1", "p1", agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.chave(t, storage.ChaveNuvemEsquecidaEm); ok {
		t.Error("o recomeço não tirou a referência do esquecer")
	}
	if n := len(linhasDaTabela(t, e.db, "sync_mudou_aqui")); n != 0 {
		t.Errorf("o recomeço deixou %d anotados do esquecer", n)
	}
}

// O MarcadorCriar da reconciliação (D3): põe o marcador de criar no ditado
// que existe aqui, sem tocar na linha, com os dois campos (a nuvem tem o
// ditado: a lixeira feita antes do envio não o tira da fila); o marcador que
// já existe fica como está; sem a linha, nada.
func TestAplicar_MarcadorCriar(t *testing.T) {
	ctx := context.Background()
	e := novoEnvioDeTeste(t)
	e.semear(t, semente{id: id(1), criado: agoraDoEnvio})
	e.semear(t, semente{id: id(2), criado: agoraDoEnvio, comMarcas: true})
	preparado(t, e.filaDeTeste)
	antes := e.todasAsLinhas(t)
	criar := func(storage.EstadoDoItem) storage.AcaoLocal {
		return storage.AcaoLocal{Marcador: storage.MarcadorCriar}
	}
	if err := e.fila.Aplicar(ctx, "a1", "e1", []string{id(1), id(2), id(3)}, nil, criar); err != nil {
		t.Fatal(err)
	}
	ms := e.marcadores(t)
	if len(ms) != 2 || ms[id(1)].Campos != storage.CampoTexto|storage.CampoLixeira || ms[id(1)].Inicial || ms[id(1)].EmVoo() ||
		ms[id(2)].Campos != 1 {
		t.Errorf("os marcadores: %+v", ms)
	}
	if !iguais(antes, e.todasAsLinhas(t)) {
		t.Error("a reconciliação mexeu na linha")
	}
	// A lixeira feita aqui antes do envio: o marcador da reconciliação fica
	// (e a lixeira sobe com ele).
	if err := e.repo.SoftDelete(ctx, donoDaFila, id(1), agoraDoEnvio); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.marcadores(t)[id(1)]; !ok {
		t.Error("a lixeira tirou da fila o ditado a reconciliar")
	}
}
