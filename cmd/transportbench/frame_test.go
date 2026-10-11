package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestWriteReadFrame_RoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, 32044, 96044, 320044} {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}

		var buf bytes.Buffer
		if err := writeFrame(&buf, payload); err != nil {
			t.Fatalf("writeFrame(size=%d): %v", size, err)
		}
		got, err := readFrame(&buf)
		if err != nil {
			t.Fatalf("readFrame(size=%d): %v", size, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("readFrame(size=%d): payload nao bate byte a byte", size)
		}
	}
}

func TestReadFrame_EOFAtFrameBoundary(t *testing.T) {
	// Reader vazio: fim limpo entre frames, o sinal que echoPipeLoop usa
	// para encerrar sem erro.
	var buf bytes.Buffer
	_, err := readFrame(&buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestReadFrame_TruncatedBody(t *testing.T) {
	var buf bytes.Buffer
	// Cabeçalho promete 10 bytes, só escreve 3: frame truncado no meio,
	// nunca deve virar um payload parcial silencioso.
	_ = writeFrame(&buf, make([]byte, 10))
	truncated := buf.Bytes()[:4+3]

	_, err := readFrame(bytes.NewReader(truncated))
	if err == nil {
		t.Fatal("readFrame: want erro para frame truncado, got nil")
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("readFrame: frame truncado nao deve ser reportado como io.EOF limpo")
	}
}
