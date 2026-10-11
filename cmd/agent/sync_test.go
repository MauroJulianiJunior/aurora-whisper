package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

type logTravado struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logTravado) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logTravado) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A montagem de verdade do sync (montarSync, sobre o montarNuvem do Agent),
// sob go test: o token só sai do Gerenciador de Credenciais pelo portão
// sysinput, que recusa sob go test (e fora do Windows não há Gerenciador).
// Então, mesmo com a instalação dizendo "conectado", nenhum pedido chega à
// nuvem, nada é gravado no Gerenciador, o ditado grava e põe o marcador na
// fila na hora, e a bandeja diz "sem conexão".
func TestSync_MontagemDoAgentNaoPassaDoPortao(t *testing.T) {
	var pedidos atomic.Int32
	nuvemFalsa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pedidos.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer nuvemFalsa.Close()

	log := &logTravado{}
	logger := slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hist, prefs, dono, fechar, err := abrirHistoricoEm(logger, filepath.Join(t.TempDir(), "historico.db"), "", clock.NewSystem())
	if err != nil {
		t.Fatal(err)
	}
	defer fechar()
	if err := prefs.Instalacao().Gravar(context.Background(), map[string]string{
		"nuvem_url": nuvemFalsa.URL, "nuvem_aparelho": "0192f0a1-0000-7000-8000-0000000a9a1e",
		"nuvem_conectado_em": "2026-10-06T12:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	n := montarNuvem(prefs.Instalacao(), nil, pastas.ModoPortatil, logger)
	laco := montarSync(hist, dono, n, nil, logger)
	parar := iniciarSync(laco, logger)
	defer parar()

	limite := time.Now().Add(10 * time.Second)
	for laco.Resumo() != "Nuvem: sem conexão" {
		if time.Now().After(limite) {
			t.Fatalf("a primeira rodada não aconteceu: %q\n%s", laco.Resumo(), log.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// O ditado grava na hora, com o marcador, e o laço não o segura.
	agora := time.Now().UTC()
	inicio := time.Now()
	if err := hist.Put(storage.Transcription{
		ID: storage.NewID(), UserID: dono, SourcePlatform: "windows", RawText: "bruto", FinalText: "final",
		Mode: "smart", ASRProvider: "whisper-local", Status: "done", CreatedAt: agora, UpdatedAt: agora,
		Version: 1, SyncState: storage.SituacaoNormal,
	}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(inicio); d > time.Second {
		t.Errorf("o Put levou %v", d)
	}
	if c, err := hist.Fila(dono).Contar(context.Background()); err != nil || c != 1 {
		t.Errorf("a fila tem %d (%v); quero o marcador do ditado", c, err)
	}
	parar()
	if pedidos.Load() != 0 {
		t.Errorf("%d pedido(s) chegaram à nuvem sem passar pelo portão", pedidos.Load())
	}
	if strings.Contains(log.String(), nuvemFalsa.URL) {
		t.Error("o log tem a URL da nuvem")
	}
}
