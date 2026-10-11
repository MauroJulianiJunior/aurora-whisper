package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// A chegada mais rápida no PC (ADR-021, S), pela montagem do Agent: o ditado que
// a nuvem traz faz subir o contador que a API local entrega ao painel, e a
// lista do histórico, relida, já o mostra. Nenhum teste aqui roda o Agent,
// toca no Gerenciador de Credenciais ou fala com a rede: a nuvem e a conexão
// são dublês, o banco é uma pasta temporária.

// nuvemComUmDitado é uma nuvem que, na primeira busca, traz um ditado feito no
// celular (sem aparelho, pela web).
type nuvemComUmDitado struct{ ditado syncengine.Item }

func (n *nuvemComUmDitado) Push(context.Context, []syncengine.Mutation) (syncengine.PushResult, error) {
	return syncengine.PushResult{Epoch: "epoch-1"}, nil
}

func (n *nuvemComUmDitado) Pull(_ context.Context, after int64, _ int) (syncengine.ChangePage, error) {
	if after > 0 {
		return syncengine.ChangePage{LastSeq: after, Epoch: "epoch-1"}, nil
	}
	return syncengine.ChangePage{LastSeq: 1, Epoch: "epoch-1", Changes: []syncengine.Change{{
		Seq: 1, EntityType: storage.EntidadeDitado, EntityID: n.ditado.ID, Operation: syncengine.OpUpsert,
		EntityVersion: n.ditado.Version, Payload: &n.ditado,
	}}}, nil
}

func TestSync_OContadorDoPainelSobeComODitadoDoCelular(t *testing.T) {
	ctx := context.Background()
	hist, dono := historicoVazio(t)
	logger := slog.New(slog.NewTextHandler(&logTravado{}, nil))
	criado := time.Now().UTC().Add(-time.Minute)
	nv := &nuvemComUmDitado{ditado: syncengine.Item{
		ID: storage.NewID(), SourcePlatform: "web", RawText: "ditado no celular", FinalText: "Ditado no celular.", Mode: "smart",
		ASRProvider: "cloud", Status: "done", CreatedAt: criado, UpdatedAt: criado, Version: 1,
	}}
	cfg := configDoSync(hist.Fila(dono), nil, nil, logger)
	cfg.Conexao = conexaoConectada{}
	cfg.Cliente = func(string, string) syncengine.SyncClient { return nv }
	laco := syncengine.NovoLaco(cfg)

	api := localapi.New(dono, hist, storage.NewInMemorySettingsRepository(),
		ligarMudancasAoPainel(localapi.Preferencias{Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{}}, laco)).Handler()
	contador := func() uint64 {
		t.Helper()
		w := chamar(t, api, http.MethodGet, "/api/nuvem/mudancas", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/nuvem/mudancas = %d %s", w.Code, w.Body)
		}
		var r map[string]uint64
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || len(r) != 1 {
			t.Fatalf("a resposta não é só o contador: %s (%v)", w.Body, err)
		}
		return r["contador"]
	}
	lista := func() string {
		t.Helper()
		return chamar(t, api, http.MethodGet, "/api/history", nil).Body.String()
	}

	if got := contador(); got != 0 {
		t.Fatalf("antes de qualquer rodada, o contador é %d", got)
	}
	if strings.Contains(lista(), "Ditado no celular.") {
		t.Fatal("o ditado já estava na lista")
	}

	if err := laco.RodadaAgora(ctx); err != nil {
		t.Fatal(err)
	}
	if got := contador(); got != 1 {
		t.Errorf("depois de a rodada trazer o ditado do celular, o contador é %d; quero 1", got)
	}
	if !strings.Contains(lista(), "Ditado no celular.") {
		t.Errorf("a lista, relida, não tem o ditado do celular: %s", lista())
	}

	// Uma rodada que não traz nada não mexe no contador: o painel não relê à toa.
	if err := laco.RodadaAgora(ctx); err != nil {
		t.Fatal(err)
	}
	if got := contador(); got != 1 {
		t.Errorf("uma rodada sem nada novo mexeu no contador: %d", got)
	}
}

// Sem o laço (nunca acontece no Agent com histórico, que é o único que monta a
// API), a rota responde 503 em vez de um contador que não existe.
func TestSync_SemOLacoOContadorNaoExiste(t *testing.T) {
	hist, dono := historicoVazio(t)
	api := localapi.New(dono, hist, storage.NewInMemorySettingsRepository(),
		localapi.Preferencias{Chaves: chaves.NovaMemoria(), Inicio: inicioParado{}, Modelos: modelosParados{}}).Handler()
	if w := chamar(t, api, http.MethodGet, "/api/nuvem/mudancas", nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("sem o laço: %d %s", w.Code, w.Body)
	}
}

// O laço é o contador que a API local pede (a assinatura é conferida em
// tempo de compilação).
var _ localapi.MudancasDoSync = (*syncengine.Laco)(nil)
