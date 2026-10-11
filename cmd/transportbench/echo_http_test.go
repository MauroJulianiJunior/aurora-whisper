package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEchoHTTPHandler_RoundTrip_ByteExact sobe um httptest.Server (stdlib,
// loopback do próprio teste, sem build tag) e prova eco byte a byte via
// POST multipart para os três tamanhos, reaproveitando o mesmo
// *http.Client entre eles (o que o modo warm faz de verdade — ver
// httpWarmSession), passando pela função de produção postMultipart em
// vez de reimplementar a chamada no teste.
func TestEchoHTTPHandler_RoundTrip_ByteExact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echoHTTPHandler))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	client := srv.Client()

	for _, seconds := range []float64{1, 3, 10} {
		payload := buildWAVPayload(seconds)

		got, err := postMultipart(client, port, payload)
		if err != nil {
			t.Fatalf("postMultipart(%vs): %v", seconds, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("eco de %vs nao bate byte a byte (len enviado=%d, len recebido=%d)",
				seconds, len(payload), len(got))
		}
	}
}

func TestEchoHTTPHandler_RejectsNonPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(echoHTTPHandler))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}
