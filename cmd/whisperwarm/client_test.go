package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTempAudio cria um arquivo com conteúdo arbitrário (o fake
// servidor HTTP não decodifica áudio de verdade — só precisa de bytes
// para provar que o caminho inteiro do adaptador funciona sem
// whisper-server nem modelo real, como pedido na tarefa).
func writeTempAudio(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "amostra.wav")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("escrevendo áudio de teste: %v", err)
	}
	return path
}

func TestClient_Transcribe_HappyPath(t *testing.T) {
	var gotMethod, gotPath, gotFieldFilename string
	var gotFileBytes []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path

		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("servidor falso: ParseMultipartForm: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("servidor falso: FormFile(\"file\"): %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		gotFieldFilename = header.Filename
		buf := make([]byte, header.Size)
		_, _ = file.Read(buf)
		gotFileBytes = buf

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":" Já mandei o arquivo.\n"}`)
	}))
	defer srv.Close()

	audioPath := writeTempAudio(t, "conteudo-de-audio-fake")
	c := newClient(srv.URL)

	text, err := c.Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "Já mandei o arquivo." {
		t.Fatalf("text = %q, want %q", text, "Já mandei o arquivo.")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != inferencePath {
		t.Errorf("path = %q, want %q", gotPath, inferencePath)
	}
	if gotFieldFilename != filepath.Base(audioPath) {
		t.Errorf("nome do arquivo enviado = %q, want %q", gotFieldFilename, filepath.Base(audioPath))
	}
	if string(gotFileBytes) != "conteudo-de-audio-fake" {
		t.Errorf("bytes recebidos pelo servidor = %q, want o conteúdo do arquivo original", gotFileBytes)
	}
}

func TestClient_Transcribe_SanitizesEmbeddedNewlines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":" duas\nlinhas \n"}`)
	}))
	defer srv.Close()

	audioPath := writeTempAudio(t, "x")
	c := newClient(srv.URL)

	text, err := c.Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "duas linhas" {
		t.Fatalf("text = %q, want %q (uma linha só, sem \\n embutido)", text, "duas linhas")
	}
}

func TestClient_Transcribe_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "deu ruim no servidor", http.StatusInternalServerError)
	}))
	defer srv.Close()

	audioPath := writeTempAudio(t, "x")
	c := newClient(srv.URL)

	text, err := c.Transcribe(context.Background(), audioPath)
	if err == nil {
		t.Fatalf("Transcribe: err = nil, want erro (status 500); text = %q", text)
	}
	if text != "" {
		t.Fatalf("text = %q, want vazio quando há erro", text)
	}
}

func TestClient_Transcribe_MissingAudioFileNeverCallsServer(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	_, err := c.Transcribe(context.Background(), filepath.Join(t.TempDir(), "nao-existe.wav"))
	if err == nil {
		t.Fatal("Transcribe: err = nil, want erro para arquivo inexistente")
	}
	if called {
		t.Fatal("Transcribe chamou o servidor mesmo sem conseguir ler o arquivo de áudio")
	}
}

// TestClient_Transcribe_ChunkedInPieces_StillAssemblesFullText prova que
// o adaptador lê a resposta inteira mesmo quando o servidor a entrega em
// vários Write()+Flush() pequenos, em vez de um write só — o cenário que
// uma implementação ingênua (uma única chamada de Read, por exemplo)
// truncaria silenciosamente.
func TestClient_Transcribe_ChunkedInPieces_StillAssemblesFullText(t *testing.T) {
	full := `{"text":"esta transcricao longa chega em varios pedacos pequenos pelo servidor falso para provar que o cliente monta o corpo inteiro antes de decodificar"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter não suporta Flusher neste ambiente de teste")
		}
		w.Header().Set("Content-Type", "application/json")
		for i := 0; i < len(full); i += 7 {
			end := i + 7
			if end > len(full) {
				end = len(full)
			}
			_, _ = w.Write([]byte(full[i:end]))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	audioPath := writeTempAudio(t, "x")
	c := newClient(srv.URL)

	text, err := c.Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := "esta transcricao longa chega em varios pedacos pequenos pelo servidor falso para provar que o cliente monta o corpo inteiro antes de decodificar"
	if text != want {
		t.Fatalf("text = %q, want %q (resposta em pedaços não foi remontada corretamente)", text, want)
	}
}

// TestClient_Transcribe_TruncatedResponse_ReturnsErrorNeverPartialText é
// "o teste que importa" pedido na tarefa: um servidor que promete mais
// bytes (Content-Length) do que realmente envia, e fecha a conexão no
// meio — exatamente o defeito documentado em
// docs/benchmarks/transporte-2026-09-17.md ("áudio truncado em silêncio,
// transcrição pela metade, sem erro nenhum"). O adaptador tem que
// detectar isso e nunca devolver o texto parcial como se fosse
// completo.
func TestClient_Transcribe_TruncatedResponse_ReturnsErrorNeverPartialText(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	serveOne := make(chan struct{})
	go func() {
		defer close(serveOne)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Consome a requisição inteira (método, headers e corpo
		// multipart) para o cliente não falhar do lado de escrita —
		// só nos importa quebrar a RESPOSTA aqui.
		reader := bufio.NewReader(conn)
		req, err := http.ReadRequest(reader)
		if err == nil && req.Body != nil {
			buf := make([]byte, 64*1024)
			for {
				_, err := req.Body.Read(buf)
				if err != nil {
					break
				}
			}
		}

		// Declara um Content-Length bem maior do que o que realmente
		// será escrito, e fecha a conexão no meio — o cliente HTTP da
		// stdlib detecta isso como io.ErrUnexpectedEOF ao ler o corpo.
		fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 5000\r\n\r\n")
		fmt.Fprint(conn, `{"text":"comec`) // corta no meio da string JSON, bem menos que os 5000 anunciados
		// conn.Close() via defer encerra a conexão aqui, sem completar
		// os 5000 bytes prometidos.
	}()

	audioPath := writeTempAudio(t, "x")
	c := newClient("http://" + ln.Addr().String())

	text, err := c.Transcribe(context.Background(), audioPath)

	select {
	case <-serveOne:
	case <-time.After(5 * time.Second):
		t.Fatal("servidor falso nunca terminou de atender a requisição")
	}

	if err == nil {
		t.Fatalf("Transcribe: err = nil para resposta truncada, want erro; text devolvido = %q", text)
	}
	if text != "" {
		t.Fatalf("Transcribe devolveu texto (%q) para uma resposta truncada — deveria devolver vazio + erro, nunca transcrição parcial disfarçada de completa", text)
	}
}
