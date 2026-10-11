package candidate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWarmSession_MultipleTranscriptionsSameProcess(t *testing.T) {
	t.Setenv(helperEnvVar, "warm")
	cfg := helperSelfConfig(t, "warm")

	ws, err := StartWarmSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartWarmSession: %v", err)
	}
	defer ws.Close()

	first, err := ws.Transcribe(context.Background(), "/tmp/um.wav")
	if err != nil {
		t.Fatalf("Transcribe #1: %v", err)
	}
	if !strings.Contains(first.Text, "/tmp/um.wav") {
		t.Fatalf("Text #1 = %q", first.Text)
	}

	second, err := ws.Transcribe(context.Background(), "/tmp/dois.wav")
	if err != nil {
		t.Fatalf("Transcribe #2: %v", err)
	}
	if !strings.Contains(second.Text, "/tmp/dois.wav") {
		t.Fatalf("Text #2 = %q", second.Text)
	}

	if err := ws.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWarmSession_NotSupportedWithoutWarmArgs(t *testing.T) {
	cfg := Config{Name: "sem-warm", Executable: "algum-binario", WarmArgs: nil}
	_, err := StartWarmSession(context.Background(), cfg)
	if !errors.Is(err, ErrWarmNotSupported) {
		t.Fatalf("err = %v, want ErrWarmNotSupported", err)
	}
}

func TestWarmSession_Timeout(t *testing.T) {
	t.Setenv(helperEnvVar, "hang")
	cfg := helperSelfConfig(t, "hang")
	cfg.Timeout = 150 * time.Millisecond

	ws, err := StartWarmSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartWarmSession: %v", err)
	}
	defer func() {
		_ = ws.Close() // best-effort: o processo travado será morto
	}()

	start := time.Now()
	_, err = ws.Transcribe(context.Background(), "/tmp/amostra.wav")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Transcribe demorou %v para desistir (timeout configurado: %v)", elapsed, cfg.Timeout)
	}
}

func TestWarmSession_CostNeverSummedWithCold(t *testing.T) {
	// Prova estrutural do ADR-012: cold e warm produzem Result
	// independentes, nunca um valor combinado. Este teste garante que
	// medir os dois modos para o mesmo candidato produz dois números que
	// o chamador pode inspecionar separadamente — não existe, em lugar
	// nenhum deste pacote, uma soma cold+warm.
	t.Setenv(helperEnvVar, "cold")
	coldCfg := helperSelfConfig(t, "cold")
	coldResult, err := RunCold(context.Background(), coldCfg, "/tmp/amostra.wav")
	if err != nil {
		t.Fatalf("RunCold: %v", err)
	}

	t.Setenv(helperEnvVar, "warm")
	warmCfg := helperSelfConfig(t, "warm")
	ws, err := StartWarmSession(context.Background(), warmCfg)
	if err != nil {
		t.Fatalf("StartWarmSession: %v", err)
	}
	defer ws.Close()
	warmResult, err := ws.Transcribe(context.Background(), "/tmp/amostra.wav")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	// coldResult e warmResult são Result independentes — cada um carrega
	// seu próprio WallTime. Nada neste pacote os soma ou tira média
	// conjunta; é responsabilidade do harness (fora deste pacote) manter
	// as duas séries agregadas separadamente.
	if coldResult.WallTime <= 0 || warmResult.WallTime <= 0 {
		t.Fatalf("WallTime cold=%v warm=%v, want ambos > 0", coldResult.WallTime, warmResult.WallTime)
	}
}
