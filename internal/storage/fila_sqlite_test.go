package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A fila do sync no SQLite (ADR-019, F7a): a emissão do marcador em cada
// gravação local, na mesma transação, e o que a fila executa das decisões do
// sync (internal/sync). Cada teste roda num historico.db de verdade, num
// arquivo da pasta temporária.

const donoDaFila = "0199a000-0000-7000-8000-00000000d0d0"

// filaDeTeste é um banco migrado, o repositório, a fila e o contador de
// avisos.
type filaDeTeste struct {
	db     *sql.DB
	repo   *storage.SQLiteTranscriptionRepository
	fila   *storage.Fila
	avisos *atomic.Int64
}

func novaFilaDeTeste(t *testing.T) filaDeTeste {
	t.Helper()
	_, db := bancoEmArquivo(t)
	repo := storage.NewSQLiteTranscriptionRepository(db)
	var avisos atomic.Int64
	repo.AoEnfileirar(func() { avisos.Add(1) })
	return filaDeTeste{db: db, repo: repo, fila: repo.Fila(donoDaFila), avisos: &avisos}
}

// conectar grava o aparelho na instalação: a nuvem está conectada para a fila.
func (f filaDeTeste) conectar(t *testing.T) {
	t.Helper()
	executar(t, f.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', 'a1'), ('nuvem_conectado_em', 'p1')
		ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`)
}

func (f filaDeTeste) desconectar(t *testing.T) {
	t.Helper()
	executar(t, f.db, `DELETE FROM instalacao WHERE nome = 'nuvem_aparelho'`)
}

func (f filaDeTeste) marcadores(t *testing.T) map[string]storage.Marcador {
	t.Helper()
	ms, err := f.fila.Marcadores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]storage.Marcador{}
	for _, m := range ms {
		out[m.EntityID] = m
	}
	return out
}

func (f filaDeTeste) ditar(t *testing.T, id string, criado time.Time) {
	t.Helper()
	if err := f.repo.Put(newTranscription(id, donoDaFila, criado)); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func (f filaDeTeste) linha(t *testing.T, id string) storage.Transcription {
	t.Helper()
	tr, err := f.repo.Get(context.Background(), donoDaFila, id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return tr
}

var agoraDaFila = time.Now().UTC().Truncate(time.Second)

const (
	idA = "0199a000-0000-7000-8000-00000000000a"
	idB = "0199a000-0000-7000-8000-00000000000b"
	idC = "0199a000-0000-7000-8000-00000000000c"
	idD = "0199a000-0000-7000-8000-00000000000d"
	idE = "0199a000-0000-7000-8000-00000000000e"
	idF = "0199a000-0000-7000-8000-00000000000f"
)

// Os cinco caminhos que gravam o histórico emitem o marcador: o ditado novo e
// o texto que chega atrasado (os dois Put, sem campo), a edição (o texto), a
// lixeira e o restaurar (a lixeira), estes três num ditado que a nuvem já
// tem. O expurgo não leva a lixeira que a nuvem ainda não soube (a D3 da F10;
// lixeira_nuvem_test.go tem o resto). O aprendido não passa por aqui (F8).
func TestEmissao_OsCincoCaminhos(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)

	f.ditar(t, idA, agoraDaFila)                     // ditado novo
	f.ditar(t, idB, agoraDaFila.Add(-time.Minute))   // texto atrasado (outra linha, outro id)
	f.ditar(t, idC, agoraDaFila.Add(-2*time.Minute)) // vai ser restaurado
	// O A e o C subiram (a nuvem respondeu); o B ainda não.
	executar(t, f.db, `DELETE FROM sync_outbox WHERE entity_id IN (?, ?)`, idA, idC)
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 1 WHERE id IN (?, ?)`, idA, idC)
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idA, 1, "editado"); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SoftDelete(ctx, donoDaFila, idA, agoraDaFila); err != nil {
		t.Fatal(err)
	}
	// O restaurar de um que entrou na lixeira desconectado: só o restaurar
	// conta, e ele põe o marcador de novo.
	executar(t, f.db, `UPDATE local_transcriptions SET deleted_at = ?, purge_at = ? WHERE id = ?`,
		"2026-10-01T00:00:00.000000000Z", "2026-10-31T00:00:00.000000000Z", idC)
	if err := f.repo.Restore(ctx, donoDaFila, idC); err != nil {
		t.Fatal(err)
	}

	ms := f.marcadores(t)
	if len(ms) != 3 {
		t.Fatalf("marcadores: %v", ms)
	}
	if ms[idA].Campos != storage.CampoTexto|storage.CampoLixeira {
		t.Errorf("o ditado editado e apagado tem os campos %d; quero texto e lixeira", ms[idA].Campos)
	}
	if ms[idB].Campos != 0 {
		t.Errorf("o ditado novo tem os campos %d; quero nenhum", ms[idB].Campos)
	}
	if ms[idC].Campos != storage.CampoLixeira {
		t.Errorf("o restaurado tem os campos %d; quero a lixeira", ms[idC].Campos)
	}
	for id, m := range ms {
		if m.EmVoo() || m.VersaoEnviada != 0 || m.EntityType != storage.EntidadeDitado {
			t.Errorf("%s: %+v", id, m)
		}
	}
	if n := f.avisos.Load(); n != 6 {
		t.Errorf("%d avisos; quero 6 (três Put, a edição, a lixeira e o restaurar)", n)
	}

	// O expurgo, com a guarda da D3 (ADR-019, "Decisões da F10"; até a F10
	// este teste exigia o contrário, de propósito): o A está na lixeira
	// vencida, mas a nuvem tem o ditado (server_version) e ainda não soube da
	// lixeira (o marcador com texto e lixeira está na fila). Ele fica, com o
	// marcador, até o push confirmar.
	r, err := f.repo.PurgeExpired(ctx, agoraDaFila.AddDate(0, 0, 31))
	if err != nil || r.Apagados != 0 || r.EsperandoANuvem != 1 {
		t.Fatalf("PurgeExpired = %+v, %v; quero nada apagado e o A esperando a nuvem", r, err)
	}
	if l := f.linha(t, idA); l.DeletedAt == nil {
		t.Errorf("o A saiu da lixeira: %+v", l)
	}
	if m, ok := f.marcadores(t)[idA]; !ok || m.Campos != storage.CampoTexto|storage.CampoLixeira {
		t.Errorf("o marcador do A mudou: %+v %v", m, ok)
	}
	if c, _ := f.fila.Contar(ctx); c != 3 {
		t.Errorf("Contar = %d; quero 3", c)
	}
}

// Desconectado, o ditado que a nuvem nunca teve não entra na fila (o envio
// inicial, F7c, cobre o que veio antes), nem as mudanças nele, e nada avisa o
// laço. E a gravação vale do mesmo jeito.
func TestEmissao_DesconectadoNadaEntra(t *testing.T) {
	ctx := context.Background()
	for _, caso := range []struct {
		nome    string
		preparo func(t *testing.T, f filaDeTeste)
	}{
		{"nunca conectou", func(*testing.T, filaDeTeste) {}},
		{"desconectou", func(t *testing.T, f filaDeTeste) { f.conectar(t); f.desconectar(t) }},
		{"aparelho vazio", func(t *testing.T, f filaDeTeste) {
			executar(t, f.db, `INSERT INTO instalacao (nome, valor) VALUES ('nuvem_aparelho', '')`)
		}},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			f := novaFilaDeTeste(t)
			caso.preparo(t, f)
			f.ditar(t, idA, agoraDaFila)
			if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idA, 1, "editado"); err != nil {
				t.Fatal(err)
			}
			if err := f.repo.SoftDelete(ctx, donoDaFila, idA, agoraDaFila); err != nil {
				t.Fatal(err)
			}
			if err := f.repo.Restore(ctx, donoDaFila, idA); err != nil {
				t.Fatal(err)
			}
			if ms := f.marcadores(t); len(ms) != 0 {
				t.Errorf("entrou na fila desconectado: %v", ms)
			}
			if n := f.avisos.Load(); n != 0 {
				t.Errorf("%d avisos desconectado", n)
			}
			if l := f.linha(t, idA); l.FinalText != "editado" || l.Version != 4 {
				t.Errorf("a gravação não valeu: %+v", l)
			}
		})
	}
}

// Desconectado, a mudança num ditado que a nuvem JÁ tem (server_version) entra
// na fila, com os campos: ela sobe na volta com a base certa, e a nuvem
// compara campo a campo (ADR-019, "Decisões da F7a/b", decisão 2). O ditado
// que a nuvem pôs fora da janela continua de fora, e o que ela nunca teve
// também.
func TestEmissao_DesconectadoMarcaOQueANuvemTem(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.ditar(t, idA, agoraDaFila)                     // a nuvem tem
	f.ditar(t, idB, agoraDaFila.Add(-time.Minute))   // a nuvem nunca teve
	f.ditar(t, idC, agoraDaFila.Add(-2*time.Minute)) // a nuvem tem, mas pôs fora da janela
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 3 WHERE id IN (?, ?)`, idA, idC)
	executar(t, f.db, `UPDATE local_transcriptions SET sync_state = ? WHERE id = ?`, storage.SituacaoForaDaJanela, idC)

	for _, id := range []string{idA, idB, idC} {
		if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, id, 1, "editado fora"); err != nil {
			t.Fatal(err)
		}
		if err := f.repo.SoftDelete(ctx, donoDaFila, id, agoraDaFila); err != nil {
			t.Fatal(err)
		}
	}
	ms := f.marcadores(t)
	if len(ms) != 1 || ms[idA].Campos != storage.CampoTexto|storage.CampoLixeira || ms[idA].EmVoo() {
		t.Fatalf("marcadores desconectado: %v; quero só o A, com o texto e a lixeira", ms)
	}
	if n := f.avisos.Load(); n != 2 {
		t.Errorf("%d avisos; quero 2 (a edição e a lixeira do A)", n)
	}
	// O restaurar também conta, e não duplica o marcador.
	if err := f.repo.Restore(ctx, donoDaFila, idA); err != nil {
		t.Fatal(err)
	}
	if ms := f.marcadores(t); len(ms) != 1 || ms[idA].Campos != storage.CampoTexto|storage.CampoLixeira {
		t.Errorf("depois do restaurar: %v", ms)
	}
}

// Conectado, num ditado que a nuvem NÃO tem (server_version nulo), só o Put do
// ditado novo põe o marcador (revisão da F7c): a edição, a lixeira e o
// restaurar de um ditado do histórico não põem, para ele não subir antes do
// "Enviar o histórico" e da cópia (D1), e a lixeira nunca subir (resposta 3).
// A lixeira tira da fila o marcador de criar que ainda não saiu (o do ditado
// novo e o do envio inicial); o em voo fica (a nuvem pode já ter o ditado), e
// a edição não mexe em nenhum deles (o ditado sobe inteiro, lido na hora).
func TestEmissao_SemAVersaoDaNuvemSoODitadoNovo(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.ditar(t, idA, agoraDaFila) // do histórico: ditado antes de conectar
	f.conectar(t)
	f.ditar(t, idB, agoraDaFila) // novo: entra
	f.ditar(t, idC, agoraDaFila) // novo, vai estar em voo
	f.ditar(t, idD, agoraDaFila) // do histórico, com o marcador do envio inicial
	executar(t, f.db, `UPDATE sync_outbox SET inicial = 1 WHERE entity_id = ?`, idD)
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeDitado, EntityID: idC, MutationID: "m-1", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	if ms := f.marcadores(t); len(ms) != 3 {
		t.Fatalf("os ditados novos: %v", ms)
	}
	f.avisos.Store(0)

	// A edição: nenhum marcador novo, e os que existem ficam como estão.
	for _, id := range []string{idA, idB, idC, idD} {
		if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, id, 1, "editado"); err != nil {
			t.Fatal(err)
		}
	}
	ms := f.marcadores(t)
	if _, ok := ms[idA]; ok || len(ms) != 3 {
		t.Fatalf("a edição do histórico entrou na fila: %v", ms)
	}
	for id, m := range ms {
		if m.Campos != 0 {
			t.Errorf("a edição mexeu nos campos do marcador de criar de %s: %+v", id, m)
		}
	}
	// A lixeira: sai o marcador parado (o novo e o do envio inicial), fica o
	// em voo; o do histórico continua de fora.
	for _, id := range []string{idA, idB, idC, idD} {
		if err := f.repo.SoftDelete(ctx, donoDaFila, id, agoraDaFila); err != nil {
			t.Fatal(err)
		}
	}
	ms = f.marcadores(t)
	if len(ms) != 1 || !ms[idC].EmVoo() || ms[idC].Campos != 0 {
		t.Fatalf("depois da lixeira: %v; quero só o C, em voo", ms)
	}
	// O restaurar: nada entra (a completação do envio leva o ditado, depois
	// do aceite).
	for _, id := range []string{idA, idB, idD} {
		if err := f.repo.Restore(ctx, donoDaFila, id); err != nil {
			t.Fatal(err)
		}
	}
	if ms := f.marcadores(t); len(ms) != 1 {
		t.Errorf("o restaurar entrou na fila: %v", ms)
	}
	if n := f.avisos.Load(); n != 0 {
		t.Errorf("%d avisos; quero nenhum", n)
	}
	for _, id := range []string{idA, idB, idD} {
		if l := f.linha(t, id); l.FinalText != "editado" || l.DeletedAt != nil || l.Version != 4 {
			t.Errorf("a gravação de %s não valeu: %+v", id, l)
		}
	}
	// O Put de um ditado de outro PC (device_id) que a nuvem não tem
	// também não entra (D2).
	deOutro := newTranscription(idE, donoDaFila, agoraDaFila)
	outroPC := "0199a000-0000-7000-8000-0000000000c2"
	deOutro.DeviceID = &outroPC
	if err := f.repo.Put(deOutro); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.marcadores(t)[idE]; ok {
		t.Error("o Put de um ditado de outro PC, sem a versão da nuvem, entrou na fila")
	}
}

// O que não emite, conectado: o ditado que a nuvem pôs fora da janela, a
// lixeira repetida e o restaurar de quem não está na lixeira (a linha não
// muda), e a gravação que falha.
func TestEmissao_SoQuandoALinhaMuda(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraDaFila)
	executar(t, f.db, `DELETE FROM sync_outbox`)
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 1 WHERE id = ?`, idA) // subiu
	f.avisos.Store(0)

	if err := f.repo.Restore(ctx, donoDaFila, idA); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SoftDelete(ctx, donoDaFila, idA, agoraDaFila); err != nil {
		t.Fatal(err)
	}
	executar(t, f.db, `DELETE FROM sync_outbox`)
	if err := f.repo.SoftDelete(ctx, donoDaFila, idA, agoraDaFila.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ms := f.marcadores(t); len(ms) != 0 {
		t.Errorf("a lixeira repetida (ou o restaurar sem efeito) emitiu: %v", ms)
	}
	if n := f.avisos.Load(); n != 1 {
		t.Errorf("%d avisos; quero 1 (só a primeira lixeira)", n)
	}
	// A edição com a base velha é recusada e não emite.
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idA, 1, "velho"); !errors.Is(err, storage.ErrVersionConflict) {
		t.Fatalf("edição com base velha: %v", err)
	}
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idB, 1, "x"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("edição de quem não existe: %v", err)
	}
	if ms := f.marcadores(t); len(ms) != 0 {
		t.Errorf("a edição recusada emitiu: %v", ms)
	}

	// Fora da janela: o ditado fica, e nenhuma mudança dele sobe mais.
	executar(t, f.db, `UPDATE local_transcriptions SET sync_state = ? WHERE id = ?`, storage.SituacaoForaDaJanela, idA)
	if err := f.repo.Restore(ctx, donoDaFila, idA); err != nil {
		t.Fatal(err)
	}
	l := f.linha(t, idA)
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idA, l.Version, "depois"); err != nil {
		t.Fatal(err)
	}
	if ms := f.marcadores(t); len(ms) != 0 {
		t.Errorf("o ditado fora da janela emitiu: %v", ms)
	}
}

// Duas edições seguidas viram um marcador só, e a segunda, durante o voo da
// primeira, não troca o envio em curso: junta os campos.
func TestEmissao_DuasEdicoesUmMarcador(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraDaFila)
	executar(t, f.db, `DELETE FROM sync_outbox`)
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 1 WHERE id = ?`, idA) // subiu
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idA, 1, "um"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idA, 2, "dois"); err != nil {
		t.Fatal(err)
	}
	if ms := f.marcadores(t); len(ms) != 1 || ms[idA].Campos != storage.CampoTexto {
		t.Fatalf("duas edições: %v", ms)
	}
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeDitado, EntityID: idA, MutationID: "m-1", Versao: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SoftDelete(ctx, donoDaFila, idA, agoraDaFila); err != nil {
		t.Fatal(err)
	}
	m := f.marcadores(t)[idA]
	if m.MutationID != "m-1" || m.VersaoEnviada != 3 || m.Campos != storage.CampoTexto|storage.CampoLixeira {
		t.Errorf("a gravação durante o voo mexeu no envio: %+v", m)
	}
}

// O marcador e a linha são uma coisa só: se o marcador não grava, a linha
// também não (e o ditado não some calado: o erro volta a quem gravou).
func TestEmissao_FalhaNoMarcadorDesfazALinha(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraDaFila)
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 1 WHERE id = ?`, idA) // subiu
	executar(t, f.db, `CREATE TRIGGER falha_na_fila BEFORE INSERT ON sync_outbox BEGIN SELECT RAISE(ABORT, 'disco cheio'); END`)
	executar(t, f.db, `DELETE FROM sync_outbox`)

	if err := f.repo.Put(newTranscription(idB, donoDaFila, agoraDaFila)); err == nil {
		t.Fatal("o Put passou com a fila falhando")
	}
	if _, err := f.repo.Get(ctx, donoDaFila, idB); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a linha ficou sem o marcador: %v", err)
	}
	if _, err := f.repo.UpdateFinalText(ctx, donoDaFila, idA, 1, "perdido"); err == nil {
		t.Fatal("a edição passou com a fila falhando")
	}
	if err := f.repo.SoftDelete(ctx, donoDaFila, idA, agoraDaFila); err == nil {
		t.Fatal("a lixeira passou com a fila falhando")
	}
	if l := f.linha(t, idA); l.FinalText != "final "+idA || l.Version != 1 || l.DeletedAt != nil {
		t.Errorf("a linha mudou sem o marcador: %+v", l)
	}
}

// ─── O recomeço e o cursor ───

// Preparar: sem estado, recomeça; o mesmo pareamento continua; outro
// pareamento da mesma instalação (o mesmo aparelho reconectado, ou outro
// aparelho) CONTINUA do cursor, com o epoch, as versões da nuvem e a fila,
// e só esquece os envios em curso (ADR-019, "Decisões da F7a/b", decisão 2).
func TestPreparar_ReconexaoContinuaDoCursor(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	est, inicio, err := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila)
	if err != nil || inicio != storage.InicioRecomeco || est.Cursor != 0 || est.Epoch != "" || est.PareadoEm != "p1" {
		t.Fatalf("sem estado: %+v, %v, %v", est, inicio, err)
	}
	executar(t, f.db, `UPDATE sync_state SET last_server_seq = 9, sync_epoch = 'e1'`)
	est, inicio, err = f.fila.Preparar(ctx, "a1", "p1", agoraDaFila)
	if err != nil || inicio != storage.InicioMesmo || est.Cursor != 9 || est.Epoch != "e1" {
		t.Fatalf("o mesmo pareamento: %+v, %v, %v", est, inicio, err)
	}

	// O que a reconexão não pode tocar: a versão da nuvem de uma linha e um
	// marcador (em voo, que perde só o envio).
	f.ditar(t, idA, agoraDaFila)
	f.ditar(t, idB, agoraDaFila)
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 4 WHERE id = ?`, idA)
	executar(t, f.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, mutation_id, versao_enviada, criado_em)
		VALUES ('transcription', ?, 1, 'm-velho', 1, '2026-10-01T00:00:00.000000000Z')`, idB)
	conferir := func(t *testing.T, onde string) {
		t.Helper()
		if sv := f.linha(t, idA).ServerVersion; sv == nil || *sv != 4 {
			t.Errorf("%s: a versão da nuvem: %v; quero 4", onde, sv)
		}
		if m, ok := f.marcadores(t)[idB]; !ok || m.EmVoo() || m.VersaoEnviada != 0 || m.Campos != storage.CampoTexto {
			t.Errorf("%s: o marcador: %+v, %v; quero os campos, sem o envio em curso", onde, m, ok)
		}
		if ms := f.marcadores(t); len(ms) != 1 {
			t.Errorf("%s: a reconexão pôs marcadores: %v", onde, ms)
		}
		var n int
		if err := f.db.QueryRow(`SELECT COUNT(*) FROM sync_state`).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d cursores; quero 1", onde, n)
		}
	}

	// Reconectou: o mesmo aparelho, outro nuvem_conectado_em.
	est, inicio, err = f.fila.Preparar(ctx, "a1", "p2", agoraDaFila)
	if err != nil || inicio != storage.InicioReconexao || est.Cursor != 9 || est.Epoch != "e1" || est.PareadoEm != "p2" {
		t.Fatalf("reconectou: %+v, %v, %v", est, inicio, err)
	}
	conferir(t, "reconectou")
	executar(t, f.db, `UPDATE sync_outbox SET mutation_id = 'm-2', versao_enviada = 2`)

	// Outro aparelho (o pareamento novo depois de revogar): o cursor passa.
	est, inicio, err = f.fila.Preparar(ctx, "a2", "p3", agoraDaFila)
	if err != nil || inicio != storage.InicioReconexao || est.Cursor != 9 || est.Epoch != "e1" || est.PareadoEm != "p3" {
		t.Fatalf("outro aparelho: %+v, %v, %v", est, inicio, err)
	}
	conferir(t, "outro aparelho")
	if est, inicio, err = f.fila.Preparar(ctx, "a2", "p3", agoraDaFila); err != nil || inicio != storage.InicioMesmo || est.Cursor != 9 {
		t.Fatalf("o cursor não ficou com o aparelho novo: %+v, %v, %v", est, inicio, err)
	}

	// Dois cursores (só num banco mexido à mão): não se sabe qual vale, e o PC
	// recomeça.
	executar(t, f.db, `INSERT INTO sync_state (user_id, device_id, last_server_seq) VALUES ('outro', 'a0', 3)`)
	if est, inicio, err = f.fila.Preparar(ctx, "a3", "p4", agoraDaFila); err != nil || inicio != storage.InicioRecomeco || est.Cursor != 0 {
		t.Fatalf("dois cursores: %+v, %v, %v", est, inicio, err)
	}
}

// ZerarCursor (o 410 no mesmo epoch): só o cursor volta a 0. O epoch, as
// versões da nuvem e a fila, com o envio em curso, ficam.
func TestZerarCursor(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	preparado(t, f)
	executar(t, f.db, `UPDATE sync_state SET last_server_seq = 9, sync_epoch = 'e1'`)
	f.ditar(t, idA, agoraDaFila)
	executar(t, f.db, `UPDATE local_transcriptions SET server_version = 4 WHERE id = ?`, idA)
	executar(t, f.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, mutation_id, versao_enviada, criado_em)
		VALUES ('transcription', ?, 1, 'm-1', 1, '2026-10-01T00:00:00.000000000Z')`, idA)

	if err := f.fila.ZerarCursor(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	est, inicio, err := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila)
	if err != nil || inicio != storage.InicioMesmo || est.Cursor != 0 || est.Epoch != "e1" {
		t.Fatalf("depois do 410: %+v, %v, %v", est, inicio, err)
	}
	if sv := f.linha(t, idA).ServerVersion; sv == nil || *sv != 4 {
		t.Errorf("a versão da nuvem: %v; quero 4", sv)
	}
	if m := f.marcadores(t)[idA]; m.MutationID != "m-1" || m.VersaoEnviada != 1 {
		t.Errorf("o envio em curso: %+v", m)
	}
}

// O recomeço (o epoch novo): cursor 0 e epoch nulo; os envios em curso
// esquecidos; um marcador para cada ditado que a nuvem já tinha, na janela
// de 6 meses, fora da lixeira e não posto fora dela; e server_version nulo
// em todos. O que nunca subiu fica de fora (F7c), e a lixeira também (a ADR;
// a revisão da F2 deixou para a F7c o risco do backup).
func TestRecomecar(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	if _, _, err := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila); err != nil {
		t.Fatal(err)
	}
	executar(t, f.db, `UPDATE sync_state SET last_server_seq = 9, sync_epoch = 'e1'`)
	executar(t, f.db, `INSERT INTO sync_state (user_id, device_id, last_server_seq) VALUES ('outro', 'a0', 3)`)
	for _, c := range []struct {
		id     string
		criado time.Time
		sv     any
		extra  string
	}{
		{idA, agoraDaFila.AddDate(0, -1, 0), 3, ""},                           // na janela: entra
		{idB, agoraDaFila.AddDate(0, -1, 0), 2, "lixeira"},                    // na lixeira, já subiu: não entra
		{idC, agoraDaFila.AddDate(0, -7, 0), 1, ""},                           // fora da janela: não entra
		{idD, agoraDaFila.AddDate(0, -1, 0), nil, ""},                         // nunca subiu: não entra (F7c)
		{idE, agoraDaFila.AddDate(0, -1, 0), 5, storage.SituacaoForaDaJanela}, // a nuvem pôs fora: não entra
		{idF, agoraDaFila.AddDate(0, -1, 0), 4, "marcador"},                   // já tinha marcador em voo
	} {
		f.ditar(t, c.id, c.criado)
		executar(t, f.db, `UPDATE local_transcriptions SET server_version = ? WHERE id = ?`, c.sv, c.id)
		switch c.extra {
		case "lixeira":
			executar(t, f.db, `UPDATE local_transcriptions SET deleted_at = created_at WHERE id = ?`, c.id)
		case storage.SituacaoForaDaJanela:
			executar(t, f.db, `UPDATE local_transcriptions SET sync_state = ? WHERE id = ?`, c.extra, c.id)
		case "marcador":
			executar(t, f.db, `INSERT INTO sync_outbox (entity_type, entity_id, campos, mutation_id, versao_enviada, criado_em)
				VALUES ('transcription', ?, 1, 'm-velho', 1, '2026-10-01T00:00:00.000000000Z')`, c.id)
		}
	}

	if err := f.fila.Recomecar(ctx, "a1", "p1", agoraDaFila); err != nil {
		t.Fatal(err)
	}
	ms := f.marcadores(t)
	if len(ms) != 2 {
		t.Fatalf("marcadores depois do recomeço: %v", ms)
	}
	if m, ok := ms[idA]; !ok || m.Campos != 0 || m.EmVoo() {
		t.Errorf("%s: %+v, %v", idA, m, ok)
	}
	if m := ms[idF]; m.Campos != storage.CampoTexto || m.EmVoo() || m.VersaoEnviada != 0 {
		t.Errorf("o marcador que já existia: %+v; quero os campos e sem o envio em curso", m)
	}
	var comVersao int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions WHERE server_version IS NOT NULL`).Scan(&comVersao); err != nil || comVersao != 0 {
		t.Errorf("%d ditados ainda com server_version", comVersao)
	}
	est, inicio, err := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila)
	if err != nil || inicio != storage.InicioMesmo || est.Cursor != 0 || est.Epoch != "" {
		t.Errorf("o cursor depois do recomeço: %+v, %v, %v", est, inicio, err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM sync_state`).Scan(&n); err != nil || n != 1 {
		t.Errorf("%d cursores; quero 1", n)
	}
}

// ─── O push ───

// Pendentes: os em voo primeiro, depois na ordem em que entraram; o marcador
// sem linha (ou de um ditado posto fora da janela) sai da fila ali mesmo.
func TestPendentes(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)
	for i, id := range []string{idA, idB, idC, idD} {
		f.ditar(t, id, agoraDaFila.Add(time.Duration(i)*time.Second))
		executar(t, f.db, `UPDATE sync_outbox SET criado_em = ? WHERE entity_id = ?`,
			agoraDaFila.Add(time.Duration(i)*time.Second).Format("2006-01-02T15:04:05.000000000Z"), id)
	}
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeDitado, EntityID: idC, MutationID: "m-c", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	executar(t, f.db, `DELETE FROM local_transcriptions WHERE id = ?`, idB) // sumiu sem passar pelo repositório
	executar(t, f.db, `UPDATE local_transcriptions SET sync_state = ? WHERE id = ?`, storage.SituacaoForaDaJanela, idD)

	itens, err := f.fila.Pendentes(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range itens {
		ids = append(ids, it.Marcador.EntityID)
		if it.Linha.ID != it.Marcador.EntityID || it.Linha.UserID != donoDaFila {
			t.Errorf("a linha não é a do marcador: %+v", it)
		}
	}
	if len(ids) != 2 || ids[0] != idC || ids[1] != idA {
		t.Errorf("ordem = %v; quero o em voo (C) e depois A", ids)
	}
	if ms := f.marcadores(t); len(ms) != 2 {
		t.Errorf("os órfãos ficaram na fila: %v", ms)
	}
	if itens, _ := f.fila.Pendentes(ctx, 1); len(itens) != 1 || itens[0].Marcador.EntityID != idC {
		t.Errorf("o limite: %v", itens)
	}
}

// MarcarEnvio só grava o id em quem não está em voo: o em voo repete o mesmo.
func TestMarcarEnvio_SoForaDeVoo(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)
	f.ditar(t, idA, agoraDaFila)
	envio := storage.Envio{EntityType: storage.EntidadeDitado, EntityID: idA, MutationID: "m-1", Versao: 1}
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{envio}); err != nil {
		t.Fatal(err)
	}
	envio.MutationID, envio.Versao = "m-2", 5
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{envio}); err != nil {
		t.Fatal(err)
	}
	if m := f.marcadores(t)[idA]; m.MutationID != "m-1" || m.VersaoEnviada != 1 {
		t.Errorf("o envio em curso foi trocado: %+v", m)
	}
}

// ─── A resposta da nuvem ───

func preparado(t *testing.T, f filaDeTeste) {
	t.Helper()
	if _, _, err := f.fila.Preparar(context.Background(), "a1", "p1", agoraDaFila); err != nil {
		t.Fatal(err)
	}
}

// O epoch: o guardado nulo adota o da resposta; o mesmo segue; outro devolve
// ErrEpochDiferente sem aplicar nada (nem o cursor). Sem o estado do
// pareamento, também.
func TestAplicar_Epoch(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.ditar(t, idA, agoraDaFila)
	apagar := func(storage.EstadoDoItem) storage.AcaoLocal { return storage.AcaoLocal{ApagarLinha: true} }
	nada := func(storage.EstadoDoItem) storage.AcaoLocal { return storage.AcaoLocal{} }

	if err := f.fila.Aplicar(ctx, "a1", "e1", nil, nil, nada); !errors.Is(err, storage.ErrPareamentoMudou) {
		t.Fatalf("sem o estado do pareamento: %v", err)
	}
	preparado(t, f)
	cinco := int64(5)
	if err := f.fila.Aplicar(ctx, "a1", "e1", nil, &cinco, nada); err != nil {
		t.Fatalf("adotar: %v", err)
	}
	est, _, _ := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila)
	if est.Epoch != "e1" || est.Cursor != 5 {
		t.Fatalf("depois de adotar: %+v", est)
	}
	nove := int64(9)
	if err := f.fila.Aplicar(ctx, "a1", "e2", []string{idA}, &nove, apagar); !errors.Is(err, storage.ErrEpochDiferente) {
		t.Fatalf("outro epoch: %v", err)
	}
	if err := f.fila.Aplicar(ctx, "a1", "", []string{idA}, &nove, apagar); !errors.Is(err, storage.ErrEpochDiferente) {
		t.Fatalf("epoch vazio: %v", err)
	}
	f.linha(t, idA) // não foi apagado
	if est, _, _ := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila); est.Cursor != 5 {
		t.Errorf("o cursor andou com o epoch errado: %+v", est)
	}
}

// Cada pedaço da AcaoLocal, executado. E aplicar nunca ecoa: com a nuvem
// conectada, gravar o estado remoto não põe marcador nem avisa o laço.
func TestAplicar_CadaAcao(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)
	preparado(t, f)
	f.ditar(t, idA, agoraDaFila)
	f.ditar(t, idB, agoraDaFila)
	f.ditar(t, idC, agoraDaFila)
	executar(t, f.db, `UPDATE local_transcriptions SET user_id = 'outro' WHERE id = ?`, idC)
	f.avisos.Store(0)

	criado := agoraDaFila.Add(-time.Hour)
	apagado := agoraDaFila.Add(-30 * time.Minute)
	pc := "0199a000-0000-7000-8000-0000000000c1"
	remoto := storage.Transcription{
		DeviceID: &pc, SourcePlatform: "windows", RawText: "bruto remoto", FinalText: "final remoto", Mode: "smart",
		ASRProvider: "whisper-local", Status: "done", CreatedAt: criado, UpdatedAt: agoraDaFila, DeletedAt: &apagado,
		Conflito:  &storage.Conflito{FinalText: "o que perdeu", DeviceID: &pc, Em: agoraDaFila},
		TargetApp: func() *string { s := "chrome"; return &s }(),
	}
	v7, v3 := int64(7), int64(3)
	acoes := map[string]storage.AcaoLocal{
		idD: {Gravar: &remoto, ServerVersion: &v7},                                                             // novo aqui
		idA: {Gravar: &remoto, ServerVersion: &v3, Situacao: storage.SituacaoRecusado, Erro: "campo_invalido"}, // muda o que existe
		idB: {ApagarLinha: true},
		idC: {ApagarLinha: true, Gravar: &remoto}, // de outro dono: intocado
	}
	ids := []string{idD, idA, idB, idC}
	if err := f.fila.Aplicar(ctx, "a1", "e1", ids, nil, func(e storage.EstadoDoItem) storage.AcaoLocal {
		if e.ID == idC && !e.DeOutroDono {
			t.Error("a linha de outro dono não foi reconhecida")
		}
		return acoes[e.ID]
	}); err != nil {
		t.Fatal(err)
	}

	novo := f.linha(t, idD)
	if novo.UserID != donoDaFila || novo.Version != 1 || novo.ServerVersion == nil || *novo.ServerVersion != 7 ||
		novo.FinalText != "final remoto" || novo.RawText != "bruto remoto" || novo.TargetApp != nil ||
		novo.DeviceID == nil || *novo.DeviceID != pc || novo.SyncState != storage.SituacaoNormal {
		t.Errorf("o novo: %+v", novo)
	}
	if novo.DeletedAt == nil || !novo.DeletedAt.Equal(apagado) || novo.PurgeAt == nil || !novo.PurgeAt.Equal(apagado.Add(storage.PrazoDaLixeira)) {
		t.Errorf("a lixeira do novo: %v, %v", novo.DeletedAt, novo.PurgeAt)
	}
	if novo.Conflito == nil || novo.Conflito.FinalText != "o que perdeu" || *novo.Conflito.DeviceID != pc || !novo.Conflito.Em.Equal(agoraDaFila) {
		t.Errorf("o selo do novo: %+v", novo.Conflito)
	}
	mudado := f.linha(t, idA)
	if mudado.Version != 2 || mudado.FinalText != "final remoto" || mudado.RawText != "raw "+idA ||
		*mudado.ServerVersion != 3 || mudado.DeletedAt == nil || mudado.Conflito == nil ||
		mudado.SyncState != storage.SituacaoRecusado || mudado.LastSyncError == nil || *mudado.LastSyncError != "campo_invalido" {
		t.Errorf("o mudado: %+v", mudado)
	}
	if _, err := f.repo.Get(ctx, donoDaFila, idB); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("o apagado ficou: %v", err)
	}
	var deOutro int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM local_transcriptions WHERE id = ? AND user_id = 'outro'`, idC).Scan(&deOutro); err != nil || deOutro != 1 {
		t.Errorf("a linha de outro dono foi tocada: %d", deOutro)
	}
	// Aplicar não ecoa.
	if ms := f.marcadores(t); len(ms) != 2 || ms[idD].EntityID != "" {
		// os marcadores de A e C (dos Put) continuam; D não ganhou; B saiu com a linha
		t.Errorf("marcadores depois de aplicar: %v", ms)
	}
	if n := f.avisos.Load(); n != 0 {
		t.Errorf("aplicar avisou o laço %d vez(es)", n)
	}

	// O selo limpo, a lixeira tirada, a situação de volta ao normal, a
	// versão esquecida, e os dois destinos do marcador.
	limpo := remoto
	limpo.DeletedAt, limpo.Conflito, limpo.FinalText = nil, nil, "limpo"
	if err := f.fila.Aplicar(ctx, "a1", "e1", []string{idA}, nil, func(storage.EstadoDoItem) storage.AcaoLocal {
		return storage.AcaoLocal{Gravar: &limpo, ZerarServerVersion: true, Situacao: storage.SituacaoNormal, Marcador: storage.MarcadorSai}
	}); err != nil {
		t.Fatal(err)
	}
	l := f.linha(t, idA)
	if l.Conflito != nil || l.DeletedAt != nil || l.PurgeAt != nil || l.ServerVersion != nil || l.LastSyncError != nil ||
		l.SyncState != storage.SituacaoNormal || l.FinalText != "limpo" || l.Version != 3 {
		t.Errorf("depois de limpar: %+v", l)
	}
	if _, ok := f.marcadores(t)[idA]; ok {
		t.Error("MarcadorSai não tirou o marcador")
	}
	f.ditar(t, idE, agoraDaFila)
	if err := f.fila.MarcarEnvio(ctx, []storage.Envio{{EntityType: storage.EntidadeDitado, EntityID: idE, MutationID: "m-e", Versao: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := f.fila.Aplicar(ctx, "a1", "e1", []string{idE}, nil, func(storage.EstadoDoItem) storage.AcaoLocal {
		return storage.AcaoLocal{Marcador: storage.MarcadorReenviar}
	}); err != nil {
		t.Fatal(err)
	}
	if m := f.marcadores(t)[idE]; m.EmVoo() || m.VersaoEnviada != 0 || m.EntityID != idE {
		t.Errorf("MarcadorReenviar: %+v", m)
	}
}

// Um erro de E/S no meio da página desfaz a página inteira, e o cursor não
// anda: a próxima rodada lê a mesma página de novo.
func TestAplicar_ErroDeESDesfazTudo(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	preparado(t, f)
	f.ditar(t, idA, agoraDaFila)
	f.ditar(t, idB, agoraDaFila)
	executar(t, f.db, `CREATE TRIGGER falha_no_b BEFORE DELETE ON local_transcriptions WHEN old.id = '`+idB+`'
		BEGIN SELECT RAISE(ABORT, 'disco'); END`)
	dez := int64(10)
	err := f.fila.Aplicar(ctx, "a1", "e1", []string{idA, idB}, &dez, func(storage.EstadoDoItem) storage.AcaoLocal {
		return storage.AcaoLocal{ApagarLinha: true}
	})
	if err == nil {
		t.Fatal("a falha de E/S não voltou")
	}
	f.linha(t, idA) // o A não foi apagado: a página voltou inteira
	if est, _, _ := f.fila.Preparar(ctx, "a1", "p1", agoraDaFila); est.Cursor != 0 || est.Epoch != "" {
		t.Errorf("o cursor (ou o epoch) andou com a falha: %+v", est)
	}
}

// RegistrarRodada anota a falha (só quando o código muda) e o sucesso (só
// quando a rodada fez algo, a anterior falhou, ou a última anotação passou de
// AnotacaoOciosa): o PC conectado e parado não escreve no banco a cada busca.
func TestRegistrarRodada(t *testing.T) {
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	preparado(t, f)
	ler := func() (sql.NullString, sql.NullString) {
		t.Helper()
		var erro, sucesso sql.NullString
		if err := f.db.QueryRow(`SELECT ultimo_erro, last_success_at FROM sync_state`).Scan(&erro, &sucesso); err != nil {
			t.Fatal(err)
		}
		return erro, sucesso
	}
	escritas := func() int64 {
		t.Helper()
		var n int64
		if err := f.db.QueryRow(`SELECT total_changes()`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	registrar := func(agora time.Time, erro string, teve bool) {
		t.Helper()
		if err := f.fila.RegistrarRodada(ctx, "a1", agora, erro, teve); err != nil {
			t.Fatal(err)
		}
	}

	registrar(agoraDaFila, "rede", false)
	if erro, sucesso := ler(); erro.String != "rede" || sucesso.Valid {
		t.Errorf("depois da falha: %v, %v", erro, sucesso)
	}
	antes := escritas()
	registrar(agoraDaFila, "rede", false)
	if escritas() != antes {
		t.Error("a mesma falha de novo escreveu no banco")
	}
	registrar(agoraDaFila, "prazo", false)
	if erro, _ := ler(); erro.String != "prazo" {
		t.Errorf("a falha nova: %v", erro)
	}

	// O sucesso depois da falha sempre anota (limpa o erro).
	registrar(agoraDaFila, "", false)
	if erro, sucesso := ler(); erro.Valid || sucesso.String != agoraDaFila.UTC().Format("2006-01-02T15:04:05.000000000Z") {
		t.Errorf("depois do sucesso: %v, %v", erro, sucesso)
	}
	// A rodada ociosa, logo depois: nada escrito.
	antes = escritas()
	registrar(agoraDaFila.Add(time.Minute), "", false)
	registrar(agoraDaFila.Add(storage.AnotacaoOciosa-time.Second), "", false)
	if escritas() != antes {
		t.Error("a rodada ociosa escreveu no banco antes do intervalo")
	}
	// A rodada que fez algo anota; e a ociosa, passado o intervalo, também.
	registrar(agoraDaFila.Add(2*time.Minute), "", true)
	if _, sucesso := ler(); sucesso.String != agoraDaFila.Add(2*time.Minute).UTC().Format("2006-01-02T15:04:05.000000000Z") {
		t.Errorf("a rodada que fez algo não anotou: %v", sucesso)
	}
	registrar(agoraDaFila.Add(2*time.Minute+storage.AnotacaoOciosa), "", false)
	if _, sucesso := ler(); sucesso.String != agoraDaFila.Add(2*time.Minute+storage.AnotacaoOciosa).UTC().Format("2006-01-02T15:04:05.000000000Z") {
		t.Errorf("a rodada ociosa, passado o intervalo, não anotou: %v", sucesso)
	}
	// A volta de uma falha, logo depois de um sucesso anotado, limpa o erro
	// mesmo ociosa.
	depois := agoraDaFila.Add(2*time.Minute + storage.AnotacaoOciosa + time.Minute)
	registrar(depois, "rede", false)
	registrar(depois.Add(time.Second), "", false)
	if erro, sucesso := ler(); erro.Valid || sucesso.String != depois.Add(time.Second).UTC().Format("2006-01-02T15:04:05.000000000Z") {
		t.Errorf("a volta da falha não anotou: %v, %v", erro, sucesso)
	}
}
