package chavestest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// Os provedores falsos da rota de ditar (ADR-021, C5b): a transcrição da OpenAI
// e da Groq, e o Polish da OpenAI e da Anthropic, cada um num httptest.Server do
// loopback, que conta as chamadas e anota o que chegou. Nenhum teste chama um
// provedor de verdade. O multipart é lido em memória (MultipartReader), nunca
// com ParseMultipartForm, que gravaria em disco.

// pedidoDeVoz é o que um pedido de transcrição trouxe.
type pedidoDeVoz struct {
	caminho       string
	contentLength int64
	chunked       bool
	auth          string
	campos        map[string]string
	arquivo       []byte
	tipoArquivo   string
	nomeArquivo   string
}

// vozFalsa é o provedor de transcrição falso.
type vozFalsa struct {
	srv *httptest.Server

	mu      sync.Mutex
	pedidos []pedidoDeVoz
	// texto é o que o próximo pedido devolve; status, se não for 0 nem 200, é a
	// falha.
	texto  string
	status int
	// durante, se não for nil, roda dentro de cada chamada antes da resposta
	// (fora do mutex): o que a pessoa faz em outro aparelho enquanto o provedor
	// demora, ou uma espera.
	durante func()
	// emVoo e pico são as chamadas ao mesmo tempo, agora e no máximo.
	emVoo, pico int
}

func novaVozFalsa(t *testing.T) *vozFalsa {
	v := &vozFalsa{texto: "hoje eu entreguei o relatório para o cliente da empresa"}
	v.srv = httptest.NewServer(http.HandlerFunc(v.servir))
	t.Cleanup(v.srv.Close)
	return v
}

// cliente é o cliente da transcrição apontado para o falso.
func (v *vozFalsa) cliente() *nuvem.Cliente {
	return &nuvem.Cliente{BaseOpenAI: v.srv.URL + "/v1", BaseGroq: v.srv.URL + "/openai/v1"}
}

func (v *vozFalsa) servir(w http.ResponseWriter, r *http.Request) {
	p := pedidoDeVoz{
		caminho: r.URL.Path, contentLength: r.ContentLength, chunked: len(r.TransferEncoding) > 0,
		auth: r.Header.Get("Authorization"), campos: map[string]string{},
	}
	if mr, err := r.MultipartReader(); err == nil {
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			dados, _ := io.ReadAll(part)
			if part.FormName() == "file" {
				p.arquivo, p.tipoArquivo, p.nomeArquivo = dados, part.Header.Get("Content-Type"), part.FileName()
			} else {
				p.campos[part.FormName()] = string(dados)
			}
		}
	}
	v.mu.Lock()
	v.pedidos = append(v.pedidos, p)
	v.emVoo++
	v.pico = max(v.pico, v.emVoo)
	durante, texto, status := v.durante, v.texto, v.status
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.emVoo--
		v.mu.Unlock()
	}()
	if durante != nil {
		durante()
	}
	if status != 0 && status != http.StatusOK {
		// O corpo de erro traz um pedaço da chave, como o 401 da OpenAI: o
		// servidor nunca o repassa.
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided: `+p.auth+`"}}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"text": texto})
}

func (v *vozFalsa) quantas() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.pedidos)
}

func (v *vozFalsa) ultimo() pedidoDeVoz {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.pedidos) == 0 {
		return pedidoDeVoz{}
	}
	return v.pedidos[len(v.pedidos)-1]
}

func (v *vozFalsa) responder(texto string, status int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.texto, v.status = texto, status
}

// enquantoResponde põe f para rodar dentro de cada chamada, e o tira ao fim do
// caso.
func (v *vozFalsa) enquantoResponde(t *testing.T, f func()) {
	t.Helper()
	v.mu.Lock()
	v.durante = f
	v.mu.Unlock()
	t.Cleanup(func() {
		v.mu.Lock()
		v.durante = nil
		v.mu.Unlock()
	})
}

func (v *vozFalsa) picoEmVoo() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.pico
}

// polishFalso é o provedor do Polish falso: devolve o texto que recebeu (ou o
// que o caso mandar), no envelope da OpenAI (/v1/responses) ou da Anthropic
// (/v1/messages).
type polishFalso struct {
	srv *httptest.Server

	mu        sync.Mutex
	recebidos []string
	caminhos  []string
	status    int
	devolver  func(entrada string) string
}

func novoPolishFalso(t *testing.T) *polishFalso {
	p := &polishFalso{}
	p.srv = httptest.NewServer(http.HandlerFunc(p.servir))
	t.Cleanup(p.srv.Close)
	return p
}

// cliente é o cliente do Polish apontado para o falso.
func (p *polishFalso) cliente() *ia.Cliente {
	return &ia.Cliente{BaseOpenAI: p.srv.URL, BaseAnthropic: p.srv.URL}
}

func (p *polishFalso) servir(w http.ResponseWriter, r *http.Request) {
	var corpo struct {
		Input []struct {
			Content string `json:"content"`
		} `json:"input"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&corpo)
	msg := ""
	switch {
	case len(corpo.Input) > 0:
		msg = corpo.Input[0].Content
	case len(corpo.Messages) > 0:
		msg = corpo.Messages[0].Content
	}
	var ditado struct {
		Ditado string `json:"ditado"`
	}
	_ = json.Unmarshal([]byte(msg), &ditado)
	p.mu.Lock()
	p.recebidos = append(p.recebidos, ditado.Ditado)
	p.caminhos = append(p.caminhos, r.URL.Path)
	status, devolver := p.status, p.devolver
	p.mu.Unlock()
	if status != 0 && status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	final := ditado.Ditado
	if devolver != nil {
		final = devolver(final)
	}
	interno, _ := json.Marshal(map[string]string{"final_text": final})
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(r.URL.Path, "/v1/messages") {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stop_reason": "end_turn",
			"content":     []map[string]string{{"type": "text", "text": string(interno)}},
			"usage":       map[string]int{"input_tokens": 12, "output_tokens": 7},
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "completed",
		"output": []map[string]any{{"type": "message", "content": []map[string]string{{"type": "output_text", "text": string(interno)}}}},
		"usage":  map[string]int{"input_tokens": 12, "output_tokens": 7},
	})
}

func (p *polishFalso) quantas() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.recebidos)
}

func (p *polishFalso) ultimo() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.recebidos) == 0 {
		return ""
	}
	return p.recebidos[len(p.recebidos)-1]
}

func (p *polishFalso) responder(status int, devolver func(string) string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status, p.devolver = status, devolver
}
