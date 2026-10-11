package candidate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunCold_Success(t *testing.T) {
	t.Setenv(helperEnvVar, "cold")
	cfg := helperSelfConfig(t, "cold")

	got, err := RunCold(context.Background(), cfg, "/tmp/amostra.wav")
	if err != nil {
		t.Fatalf("RunCold: %v", err)
	}
	if !strings.Contains(got.Text, "/tmp/amostra.wav") {
		t.Fatalf("Text = %q, want conter o caminho do áudio", got.Text)
	}
	if got.WallTime <= 0 {
		t.Fatalf("WallTime = %v, want > 0", got.WallTime)
	}
}

func TestRunCold_ProcessFails(t *testing.T) {
	t.Setenv(helperEnvVar, "fail")
	cfg := helperSelfConfig(t, "fail")

	_, err := RunCold(context.Background(), cfg, "/tmp/amostra.wav")
	if err == nil {
		t.Fatal("RunCold: want erro para processo que sai com código != 0, got nil")
	}
	if !strings.Contains(err.Error(), "falha proposital") {
		t.Fatalf("erro não carrega o stderr do candidato: %v", err)
	}
}

func TestRunCold_Timeout(t *testing.T) {
	t.Setenv(helperEnvVar, "hang")
	cfg := helperSelfConfig(t, "hang")
	cfg.Timeout = 150 * time.Millisecond

	start := time.Now()
	_, err := RunCold(context.Background(), cfg, "/tmp/amostra.wav")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("RunCold demorou %v para desistir de um candidato travado (timeout configurado: %v)",
			elapsed, cfg.Timeout)
	}
}

func TestRunCold_MissingPlaceholder(t *testing.T) {
	cfg := Config{Name: "sem-marcador", Executable: "algum-binario", ColdArgs: []string{"--flag"}}
	if _, err := RunCold(context.Background(), cfg, "/tmp/a.wav"); !errors.Is(err, ErrMissingPlaceholder) {
		t.Fatalf("err = %v, want ErrMissingPlaceholder", err)
	}
}

func TestRunCold_EmptyExecutable(t *testing.T) {
	cfg := Config{ColdArgs: []string{AudioPlaceholder}}
	if _, err := RunCold(context.Background(), cfg, "/tmp/a.wav"); !errors.Is(err, ErrEmptyExecutable) {
		t.Fatalf("err = %v, want ErrEmptyExecutable", err)
	}
}

func TestRunCold_MeasuresRSSOrHonestlyDoesNot(t *testing.T) {
	// O helper roda tempo suficiente para não garantir uma amostra de
	// RSS. O contrato importante é que Result nunca finge medição: se
	// RSSMeasured é false, PeakRSSBytes deve ser 0.
	t.Setenv(helperEnvVar, "cold")
	cfg := helperSelfConfig(t, "cold")

	got, err := RunCold(context.Background(), cfg, "/tmp/amostra.wav")
	if err != nil {
		t.Fatalf("RunCold: %v", err)
	}
	if !got.RSSMeasured && got.PeakRSSBytes != 0 {
		t.Fatalf("RSSMeasured=false mas PeakRSSBytes=%d (deveria ser 0 quando não medido)", got.PeakRSSBytes)
	}
}
