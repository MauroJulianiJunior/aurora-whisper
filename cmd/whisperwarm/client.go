package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// inferencePath é o endpoint de transcrição do whisper-server. Não é
// configurável por flag (a spec da tarefa só pede -server, -model,
// -lang, -ready-timeout) — é o default do próprio whisper-server
// (--inference-path, default "/inference").
const inferencePath = "/inference"

// httpClientTimeout limita cada requisição individual a /inference. É
// generoso o bastante para um cold-load acidental (ex.: primeira
// requisição contra um whisper-server que carregou o modelo de forma
// preguiçosa), mas garante que uma conexão travada nunca prende o
// protocolo de linha para sempre — ver doc.go, "sem travar".
const httpClientTimeout = 120 * time.Second

// inferenceResponse é o formato de resposta do whisper-server em
// /inference (confirmado contra o binário real em
// thirdparty/whisper/Release/whisper-server.exe: POST multipart com
// campo "file" devolve {"text": "..."}).
type inferenceResponse struct {
	Text string `json:"text"`
}

// client fala com um whisper-server já pronto em baseURL.
type client struct {
	httpClient *http.Client
	baseURL    string
}

func newClient(baseURL string) *client {
	return &client{
		httpClient: &http.Client{Timeout: httpClientTimeout},
		baseURL:    baseURL,
	}
}

// Transcribe lê audioPath, envia como POST multipart para
// baseURL+/inference e devolve a transcrição como uma única linha
// (sem quebras de linha embutidas — ver sanitizeLine).
//
// Nunca devolve texto em caso de erro: qualquer falha — arquivo
// ilegível, requisição que não completa, status não-200, corpo que não
// bate com o Content-Length declarado (a armadilha de truncamento
// documentada em docs/benchmarks/transporte-2026-09-17.md — "áudio
// truncado em silêncio, transcrição pela metade, sem erro nenhum"), ou
// JSON inválido — devolve ("", err), nunca um texto parcial disfarçado
// de transcrição completa.
func (c *client) Transcribe(ctx context.Context, audioPath string) (string, error) {
	data, err := readWholeFile(audioPath)
	if err != nil {
		return "", err
	}

	body, contentType, err := buildMultipartBody(data, filepath.Base(audioPath))
	if err != nil {
		return "", fmt.Errorf("whisperwarm: %s: %w", audioPath, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+inferencePath, body)
	if err != nil {
		return "", fmt.Errorf("whisperwarm: montando requisição para %s: %w", c.baseURL, err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisperwarm: requisição a %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		// Isto é exatamente o sintoma da armadilha: o servidor prometeu
		// mais bytes do que entregou e fechou a conexão. io.ReadAll
		// devolve erro (tipicamente io.ErrUnexpectedEOF) em vez de um
		// []byte curto silencioso — propagamos o erro, nunca o corpo
		// parcial.
		return "", fmt.Errorf("whisperwarm: lendo resposta de %s (áudio pode ter sido truncado): %w", c.baseURL, readErr)
	}
	// Segunda camada de defesa, redundante com o erro acima na maioria
	// dos casos, mas direta: se o servidor declarou Content-Length e o
	// que chegou não bate, é truncamento, mesmo que io.ReadAll não tenha
	// erro (ex.: conexão fechada exatamente no limite de um chunk).
	if resp.ContentLength >= 0 && int64(len(respBody)) != resp.ContentLength {
		return "", fmt.Errorf("whisperwarm: resposta truncada de %s: recebeu %d bytes, Content-Length declarava %d",
			c.baseURL, len(respBody), resp.ContentLength)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisperwarm: whisper-server (%s) respondeu %s: %s",
			c.baseURL, resp.Status, sanitizeLine(string(respBody)))
	}

	var parsed inferenceResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("whisperwarm: resposta de %s não é JSON válido (%d bytes recebidos): %w",
			c.baseURL, len(respBody), err)
	}

	return sanitizeLine(parsed.Text), nil
}

// readWholeFile lê audioPath inteiro e confere que o número de bytes
// lidos bate com o tamanho que os.Stat reportou — a checagem do lado
// CLIENTE equivalente à que o pacote pede: "confira que o corpo enviado
// tem o tamanho que você espera", aplicada antes de qualquer byte ir
// para a rede.
func readWholeFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("whisperwarm: %s: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("whisperwarm: lendo %s: %w", path, err)
	}
	if int64(len(data)) != fi.Size() {
		return nil, fmt.Errorf("whisperwarm: %s: leu %d bytes, os.Stat informava %d (arquivo mudou durante a leitura?)",
			path, len(data), fi.Size())
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("whisperwarm: %s: arquivo de áudio vazio", path)
	}
	return data, nil
}

// buildMultipartBody monta o corpo multipart com data no campo "file"
// (nome confirmado contra o whisper-server real — outro nome de campo
// devolve HTTP 400 "Invalid request") e confere que a quantidade de
// bytes escrita no part bate com len(data), antes de devolver o corpo
// pronto para virar o Body da requisição HTTP.
func buildMultipartBody(data []byte, filename string) (io.Reader, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", fmt.Errorf("montando multipart: %w", err)
	}
	n, err := part.Write(data)
	if err != nil {
		return nil, "", fmt.Errorf("escrevendo multipart: %w", err)
	}
	if n != len(data) {
		return nil, "", fmt.Errorf("escreveu %d de %d bytes esperados no corpo multipart", n, len(data))
	}
	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("fechando multipart: %w", err)
	}

	return &buf, mw.FormDataContentType(), nil
}

// sanitizeLine deixa s seguro para virar UMA linha do protocolo:
// remove espaço de sobra e substitui qualquer quebra de linha embutida
// por espaço (o whisper-server devolve "text" com um "\n" no final, e
// nada impede um JSON malicioso/malformado de embutir mais quebras no
// meio — se isso vazasse para stdout sem tratamento, uma transcrição
// viraria duas ou mais linhas e dessincronizaria o protocolo do
// harness, que espera exatamente uma linha de resposta por requisição).
func sanitizeLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}
