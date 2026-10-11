package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// A montagem do envio inicial e do esquecer no Agent (ADR-019, F7c): o
// histórico de verdade (o historico.db numa pasta temporária, com o caminho
// para a cópia), sobre o montarNuvem do Agent. Sob go test, o Gerenciador de
// Credenciais está atrás do portão (e fora do Windows não existe): o envio
// para ao conferir que o PC está conectado, e o esquecer, ao tirar o token,
// os dois ANTES de tocar no banco. Nada é copiado, marcado nem esquecido.
func TestEnvio_MontagemDoAgentNaoPassaDoPortao(t *testing.T) {
	ctx := context.Background()
	caminho := filepath.Join(t.TempDir(), "historico.db")
	log := &logTravado{}
	logger := slog.New(slog.NewTextHandler(log, nil))
	hist, prefs, dono, fechar, err := abrirHistoricoEm(logger, caminho, "", clock.NewSystem())
	if err != nil {
		t.Fatal(err)
	}
	defer fechar()
	agora := time.Now().UTC()
	if err := hist.Put(storage.Transcription{
		ID: storage.NewID(), UserID: dono, SourcePlatform: "windows", RawText: "bruto", FinalText: "de antes",
		Mode: "smart", ASRProvider: "whisper-local", Status: "done", CreatedAt: agora, UpdatedAt: agora,
		Version: 1, SyncState: storage.SituacaoNormal,
	}); err != nil {
		t.Fatal(err)
	}
	inst := prefs.Instalacao()
	if err := inst.Gravar(ctx, map[string]string{
		"nuvem_url": "http://localhost:8080", "nuvem_aparelho": "0192f0a1-0000-7000-8000-0000000a9a1e",
		"nuvem_conectado_em": "2026-10-06T12:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	h := storage.HistoricoDaNuvem{Fila: hist.Fila(dono), CaminhoDoBanco: caminho}
	n := montarNuvem(inst, h, pastas.ModoPortatil, logger)

	var ne *nuvem.Erro
	if _, err := n.EnviarHistorico(ctx); !errors.As(err, &ne) || ne.Status != http.StatusInternalServerError || ne.Mensagem != nuvem.MsgCofre {
		t.Fatalf("EnviarHistorico sob go test: %v; quero a recusa do Gerenciador", err)
	}
	if _, err := n.Esquecer(ctx); !errors.As(err, &ne) || ne.Mensagem != nuvem.MsgCofre {
		// Sem o token (o Ler recusado), o PC não conta como conectado só se
		// o Gerenciador disser "ausente"; o portão diz outra coisa.
		t.Fatalf("Esquecer sob go test: %v; quero a recusa do Gerenciador", err)
	}
	copias, _ := filepath.Glob(caminho + ".antes-da-nuvem-*.db")
	if len(copias) != 0 {
		t.Errorf("houve cópia: %v", copias)
	}
	r, err := h.Resumo(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.Previa != 1 || r.Aceito || r.Pendentes != 0 {
		t.Errorf("o banco mudou: %+v", r)
	}
	if v, ok, _ := inst.Ler(ctx, "nuvem_url"); !ok || v == "" {
		t.Error("a nuvem foi esquecida")
	}
	// O caminho da cópia é ao lado do banco de verdade (o do Agent).
	if got := storage.CaminhoDaCopiaDoEnvio(caminho, time.Date(2026, 10, 6, 15, 30, 0, 0, time.Local)); filepath.Dir(got) != filepath.Dir(caminho) {
		t.Errorf("a cópia iria para %s", got)
	}
}
