package main

import (
	"bytes"
	"net"
	"testing"
)

// TestEchoTCPServe_RoundTrip_ByteExact sobe um listener TCP de verdade em
// loopback (net.Listen("tcp", "127.0.0.1:0"), do próprio teste — sem
// build tag, sem tocar em nada global) e prova eco byte a byte para os
// três tamanhos, reaproveitando a MESMA conexão entre eles (o que o modo
// warm faz de verdade — ver tcpWarmSession).
func TestEchoTCPServe_RoundTrip_ByteExact(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	go echoTCPServe(ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial: %v", err)
	}
	defer conn.Close()

	for _, seconds := range []float64{1, 3, 10} {
		payload := buildWAVPayload(seconds)

		if err := writeFrame(conn, payload); err != nil {
			t.Fatalf("writeFrame(%vs): %v", seconds, err)
		}
		got, err := readFrame(conn)
		if err != nil {
			t.Fatalf("readFrame(%vs): %v", seconds, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("eco de %vs nao bate byte a byte (len enviado=%d, len recebido=%d)",
				seconds, len(payload), len(got))
		}
	}
}

// TestEchoTCPServe_MultipleConnections prova que cada conexão é
// independente (o que o modo cold precisa: abrir, usar, fechar, sem
// deixar estado vazando para a próxima).
func TestEchoTCPServe_MultipleConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	go echoTCPServe(ln)

	payload := buildWAVPayload(1)
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("net.Dial (conexao %d): %v", i, err)
		}
		if err := writeFrame(conn, payload); err != nil {
			t.Fatalf("writeFrame (conexao %d): %v", i, err)
		}
		got, err := readFrame(conn)
		if err != nil {
			t.Fatalf("readFrame (conexao %d): %v", i, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("conexao %d: eco nao bate byte a byte", i)
		}
		_ = conn.Close()
	}
}
