package nuvem

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// Os canários: valores que não existem em lugar nenhum além dos testes, para
// provar que áudio e chave não aparecem onde não devem. Nada aqui é uma
// credencial de verdade.
const (
	chaveDeTeste   = "chave-FALSA-de-teste-nao-vale-nada-0001"
	canarioAudio   = "CANARIO-DE-AUDIO-q8w2e7r1"
	canarioCorpo   = "CANARIO-DO-CORPO-DE-ERRO-z4x6c9"
	canarioDica    = "CANARIO-DA-DICA-m3n5b8"
	cabecalhoWebm  = "\x1a\x45\xdf\xa3" // o início de um EBML (webm)
	respostaOK     = `{"text":" Já mandei o arquivo. "}`
	respostaOKJSON = "application/json"
)

// audioWebm é um "áudio" de teste: o cabeçalho EBML, o canário e mais alguns
// bytes. O pacote não decodifica nada, então basta que os bytes sejam
// reconhecíveis do outro lado.
func audioWebm() asr.Comprimido {
	return asr.Comprimido{
		Bytes: []byte(cabecalhoWebm + canarioAudio + "\x00\x01\x02\xff\xfe-fim"),
		Tipo:  "audio/webm",
	}
}

// pedidoVisto é o que o provedor falso viu de um pedido.
type pedidoVisto struct {
	Metodo, Caminho string
	Cab             http.Header
	ContentLength   int64
	Transfer        []string
	ProtoMajor      int
	// TamanhoLido é quantos bytes de corpo o servidor leu de fato.
	TamanhoLido int
	// Campos são os campos de texto do multipart, na ordem em que vieram.
	Campos []parCampo
	// Arquivo é a parte "file", se houve.
	TemArquivo   bool
	NomeArquivo  string
	TipoArquivo  string
	BytesArquivo []byte
	// Defeito é o erro de leitura do multipart, se houve.
	Defeito string
}

type parCampo struct{ Nome, Valor string }

// campo devolve o valor do primeiro campo com o nome dado.
func (p pedidoVisto) campo(nome string) (string, bool) {
	for _, c := range p.Campos {
		if c.Nome == nome {
			return c.Valor, true
		}
	}
	return "", false
}

func (p pedidoVisto) nomes() []string {
	var ns []string
	for _, c := range p.Campos {
		ns = append(ns, c.Nome)
	}
	return ns
}

// provedorFalso é um servidor de transcrição de mentira, em loopback. Ele lê o
// pedido INTEIRO e o interpreta sem tocar o disco (multipart.Reader sobre os
// bytes, nunca ParseMultipartForm), registra o que viu e chama h para
// responder.
type provedorFalso struct {
	*httptest.Server
	mu     sync.Mutex
	vistos []pedidoVisto
}

func (p *provedorFalso) pedidos() []pedidoVisto {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]pedidoVisto(nil), p.vistos...)
}

func novoProvedorFalso(t *testing.T, h http.HandlerFunc) *provedorFalso {
	t.Helper()
	p := &provedorFalso{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := lerPedido(r)
		p.mu.Lock()
		p.vistos = append(p.vistos, v)
		p.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(p.Close)
	return p
}

func lerPedido(r *http.Request) pedidoVisto {
	v := pedidoVisto{
		Metodo:        r.Method,
		Caminho:       r.URL.Path,
		Cab:           r.Header.Clone(),
		ContentLength: r.ContentLength,
		Transfer:      append([]string(nil), r.TransferEncoding...),
		ProtoMajor:    r.ProtoMajor,
	}
	corpo, err := io.ReadAll(r.Body)
	v.TamanhoLido = len(corpo)
	if err != nil {
		v.Defeito = "leitura do corpo: " + err.Error()
		return v
	}
	tipo, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(tipo, "multipart/") {
		return v
	}
	mr := multipart.NewReader(bytes.NewReader(corpo), params["boundary"])
	for {
		parte, err := mr.NextPart()
		if err == io.EOF {
			return v
		}
		if err != nil {
			v.Defeito = "multipart: " + err.Error()
			return v
		}
		dados, err := io.ReadAll(parte)
		if err != nil {
			v.Defeito = "parte: " + err.Error()
			return v
		}
		if parte.FileName() != "" {
			v.TemArquivo = true
			v.NomeArquivo = parte.FileName()
			v.TipoArquivo = parte.Header.Get("Content-Type")
			v.BytesArquivo = dados
			continue
		}
		v.Campos = append(v.Campos, parCampo{parte.FormName(), string(dados)})
	}
}

// responder devolve um handler que responde com o status e o corpo dados.
func responder(status int, corpo string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", respostaOKJSON)
		w.WriteHeader(status)
		io.WriteString(w, corpo)
	}
}

// clienteDe aponta um Cliente para o provedor falso, para os dois provedores.
func clienteDe(srv *provedorFalso) *Cliente {
	return &Cliente{BaseOpenAI: srv.URL + "/v1", BaseGroq: srv.URL + "/openai/v1"}
}

// providerDe monta um Provider de teste com a chave de teste.
func providerDe(srv *provedorFalso, provedor, modelo string) *Provider {
	return &Provider{
		Cliente:  clienteDe(srv),
		Provedor: provedor,
		Modelo:   modelo,
		Chave:    func(context.Context) (string, error) { return chaveDeTeste, nil },
	}
}

func bufferWebm() asr.AudioBuffer { return asr.AudioBuffer{Comprimido: audioWebm()} }

// semRastro falha se algum dos proibidos aparece em texto.
func semRastro(t *testing.T, onde, texto string, proibidos ...string) {
	t.Helper()
	for _, p := range proibidos {
		if p != "" && strings.Contains(texto, p) {
			t.Errorf("%s traz %q:\n%s", onde, p, texto)
		}
	}
}
