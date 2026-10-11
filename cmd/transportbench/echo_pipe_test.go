package main

import (
	"bytes"
	"os"
	"testing"
)

// TestEchoPipeLoop_RoundTrip_ByteExact exercita echoPipeLoop com pipes de
// verdade (os.Pipe, syscall real, loopback do próprio teste — sem build
// tag, sem tocar em nada global) para os três tamanhos exigidos por
// docs/benchmarks/candidatos-2026-09-17.md, e prova reuso (a mesma
// goroutine de eco atende os três frames, como o processo faria em modo
// warm) além do encerramento limpo ao fechar a escrita (modo cold).
func TestEchoPipeLoop_RoundTrip_ByteExact(t *testing.T) {
	reqR, reqW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (requisicao): %v", err)
	}
	respR, respW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (resposta): %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- echoPipeLoop(reqR, respW) }()

	for _, seconds := range []float64{1, 3, 10} {
		payload := buildWAVPayload(seconds)

		if err := writeFrame(reqW, payload); err != nil {
			t.Fatalf("writeFrame(%vs): %v", seconds, err)
		}
		got, err := readFrame(respR)
		if err != nil {
			t.Fatalf("readFrame(%vs): %v", seconds, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("eco de %vs nao bate byte a byte (len enviado=%d, len recebido=%d)",
				seconds, len(payload), len(got))
		}
	}

	if err := reqW.Close(); err != nil {
		t.Fatalf("fechando escrita da requisicao: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("echoPipeLoop nao encerrou limpo ao fechar stdin: %v", err)
	}

	_ = respW.Close()
	_ = reqR.Close()
	_ = respR.Close()
}
