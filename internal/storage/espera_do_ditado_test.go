package storage_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O Agent abre o historico.db com UMA conexão (SetMaxOpenConns(1)), e o Put do
// ditado roda na goroutine do Run: enquanto o laço do sync segura a conexão
// numa transação, o Put do PRÓXIMO ditado espera (o texto do ditado atual já
// foi inserido no campo; o histórico grava depois). Estes testes medem essa
// espera nas duas transações mais longas do sync, e falham se ela crescer
// muito (o teto é largo: é guarda de regressão, não benchmark). Os números
// medidos estão na ADR-019 ("Decisões da F7a/b", "A espera do ditado").
//
//   - o Aplicar de uma página cheia do pull (500 ditados novos), a cada
//     rodada que traz muita coisa;
//   - o Recomecar sobre um histórico grande, que depois da revisão de
//     06-out-2026 só roda no epoch novo (um backup restaurado) e na primeira
//     rodada da instalação: a reconexão e o 410 não passam por ele.

// Put durante o Aplicar de uma página cheia: o Put começa quando o Aplicar
// está no primeiro ditado, e espera o resto da página.
func TestEspera_PutDuranteOAplicarDeUmaPaginaCheia(t *testing.T) {
	if testing.Short() {
		t.Skip("mede tempo")
	}
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	f.conectar(t)
	preparado(t, f)
	const pagina = 500
	ids := make([]string, pagina)
	for i := range ids {
		ids[i] = fmt.Sprintf("0199b000-0000-7000-8000-%012d", i)
	}
	agora := time.Now().UTC()
	dentro := make(chan struct{})
	espera := make(chan time.Duration, 1)
	primeiro := true
	inicio := time.Now()
	err := f.fila.Aplicar(ctx, "a1", "e1", ids, nil, func(e storage.EstadoDoItem) storage.AcaoLocal {
		if primeiro {
			primeiro = false
			go func() {
				close(dentro)
				t0 := time.Now()
				if err := f.repo.Put(newTranscription("0199b000-0000-7000-8000-ffffffffffff", donoDaFila, agora)); err != nil {
					t.Error(err)
				}
				espera <- time.Since(t0)
			}()
			<-dentro
		}
		g := newTranscription(e.ID, donoDaFila, agora)
		v := int64(1)
		return storage.AcaoLocal{Gravar: &g, ServerVersion: &v}
	})
	if err != nil {
		t.Fatal(err)
	}
	aplicar := time.Since(inicio)
	put := <-espera
	t.Logf("o Aplicar de %d ditados levou %v; o Put esperou %v", pagina, aplicar, put)
	if put > 2*time.Second {
		t.Errorf("o Put esperou %v durante o Aplicar de uma página", put)
	}
}

// O Recomecar sobre 7 mil ditados de tamanho comum (todos com a versão da
// nuvem): a duração da transação é a espera máxima de um Put que caia nela.
func TestEspera_RecomecarSobreUmHistoricoGrande(t *testing.T) {
	if testing.Short() {
		t.Skip("mede tempo")
	}
	ctx := context.Background()
	f := novaFilaDeTeste(t)
	preparado(t, f)
	const n = 7000
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	agora := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	texto := strings.Repeat("palavra ", 160) // 1,25 KiB, um ditado de uns 40 s
	for i := 0; i < n; i++ {
		if _, err := tx.Exec(`INSERT INTO local_transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
			asr_provider, status, created_at, updated_at, version, sync_state, server_version)
			VALUES (?, ?, 'windows', ?, ?, 'smart', 'local', 'done', ?, ?, 1, 'pending', 3)`,
			fmt.Sprintf("0199b000-0000-7000-8000-%012d", i), donoDaFila, texto, texto, agora, agora); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	inicio := time.Now()
	if err := f.fila.Recomecar(ctx, "a1", "p1", time.Now()); err != nil {
		t.Fatal(err)
	}
	d := time.Since(inicio)
	t.Logf("o Recomecar de %d ditados segurou a conexão por %v", n, d)
	if c, _ := f.fila.Contar(ctx); c != n {
		t.Errorf("a fila depois do recomeço: %d; quero %d", c, n)
	}
	if d > 5*time.Second {
		t.Errorf("o Recomecar de %d ditados levou %v", n, d)
	}
}
