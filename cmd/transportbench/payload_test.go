package main

import (
	"bytes"
	"testing"
)

func TestWavPCM16MonoBytes_MatchesSpecSizes(t *testing.T) {
	// Tamanhos exigidos por docs/benchmarks/candidatos-2026-09-17.md:
	// derivados de PCM16 mono 16 kHz com cabeçalho WAV de 44 bytes.
	cases := []struct {
		seconds float64
		want    int
	}{
		{1, 32044},
		{3, 96044},
		{10, 320044},
	}
	for _, c := range cases {
		got := wavPCM16MonoBytes(c.seconds)
		if got != c.want {
			t.Errorf("wavPCM16MonoBytes(%v) = %d, want %d", c.seconds, got, c.want)
		}
	}
}

func TestBuildWAVPayload_SizeMatchesFormula(t *testing.T) {
	for _, seconds := range []float64{1, 3, 10} {
		buf := buildWAVPayload(seconds)
		want := wavPCM16MonoBytes(seconds)
		if len(buf) != want {
			t.Fatalf("buildWAVPayload(%v): len = %d, want %d", seconds, len(buf), want)
		}
	}
}

func TestBuildWAVPayload_HeaderFields(t *testing.T) {
	buf := buildWAVPayload(1)

	if !bytes.Equal(buf[0:4], []byte("RIFF")) {
		t.Fatalf("magic RIFF ausente: %q", buf[0:4])
	}
	if !bytes.Equal(buf[8:12], []byte("WAVE")) {
		t.Fatalf("magic WAVE ausente: %q", buf[8:12])
	}
	if !bytes.Equal(buf[12:16], []byte("fmt ")) {
		t.Fatalf("chunk fmt ausente: %q", buf[12:16])
	}
	if !bytes.Equal(buf[36:40], []byte("data")) {
		t.Fatalf("chunk data ausente: %q", buf[36:40])
	}
}

func TestBuildWAVPayload_DataIsNotConstant(t *testing.T) {
	// Um payload de dados todo-zero (ou todo-igual) deixaria um teste de
	// round-trip cego a truncamento/zeragem no final — ver comentário em
	// payload.go. Este teste garante que o padrão realmente varia.
	buf := buildWAVPayload(1)
	data := buf[wavHeaderBytes:]

	allSame := true
	for _, b := range data {
		if b != data[0] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Fatal("dados do payload são todos o mesmo byte — não detecta truncamento/zeragem")
	}
}

func TestBuildWAVPayload_Deterministic(t *testing.T) {
	a := buildWAVPayload(3)
	b := buildWAVPayload(3)
	if !bytes.Equal(a, b) {
		t.Fatal("buildWAVPayload não é determinístico entre chamadas com o mesmo seconds")
	}
}
