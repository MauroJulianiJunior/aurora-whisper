package nuvem

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// ---------------------------------------------------------------------
// O corpo de tamanho conhecido (ADR-021, decisão D)
// ---------------------------------------------------------------------

// O pedido chega com Content-Length e sem chunked, e o servidor lê
// exatamente tantos bytes quanto o cabeçalho disse.
func TestTranscribe_ContentLengthSemChunked(t *testing.T) {
	for _, provedor := range []string{OpenAI, Groq} {
		srv := novoProvedorFalso(t, responder(200, respostaOK))
		p := providerDe(srv, provedor, "")
		if _, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{Vocabulary: canarioDica}); err != nil {
			t.Fatalf("%s: %v", provedor, err)
		}
		v := srv.pedidos()[0]
		if len(v.Transfer) != 0 {
			t.Errorf("%s: Transfer-Encoding = %v: o corpo foi chunked", provedor, v.Transfer)
		}
		if v.ContentLength <= int64(len(audioWebm().Bytes)) {
			t.Errorf("%s: Content-Length = %d, quero o tamanho do corpo inteiro (mais que só o áudio)", provedor, v.ContentLength)
		}
		if int64(v.TamanhoLido) != v.ContentLength {
			t.Errorf("%s: o servidor leu %d bytes mas o Content-Length diz %d", provedor, v.TamanhoLido, v.ContentLength)
		}
		if got := v.Cab.Get("Content-Length"); got != strconv.FormatInt(v.ContentLength, 10) {
			t.Errorf("%s: cabeçalho Content-Length = %q", provedor, got)
		}
	}
}

// O mesmo, NO FIO: um servidor TCP cru lê a cabeça do pedido como ela saiu da
// máquina, sem a normalização do net/http do servidor.
func TestTranscribe_NoFioTemContentLengthENaoTemChunked(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type visto struct {
		cabeca []string
		corpo  int
		err    error
	}
	saida := make(chan visto, 1)
	go func() {
		var v visto
		defer func() { saida <- v }()
		c, err := ln.Accept()
		if err != nil {
			v.err = err
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(10 * time.Second))
		br := bufio.NewReader(c)
		for {
			linha, err := br.ReadString('\n')
			if err != nil {
				v.err = err
				return
			}
			if linha == "\r\n" {
				break
			}
			v.cabeca = append(v.cabeca, strings.TrimRight(linha, "\r\n"))
		}
		tamanho := 0
		for _, h := range v.cabeca {
			if n, ok := strings.CutPrefix(strings.ToLower(h), "content-length: "); ok {
				tamanho, _ = strconv.Atoi(n)
			}
		}
		n, err := io.CopyN(io.Discard, br, int64(tamanho))
		v.corpo = int(n)
		if err != nil {
			v.err = err
			return
		}
		corpo := respostaOK
		io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\nContent-Length: "+
			strconv.Itoa(len(corpo))+"\r\n\r\n"+corpo)
	}()

	cli := &Cliente{BaseGroq: "http://" + ln.Addr().String() + "/openai/v1"}
	res, err := cli.Transcrever(context.Background(), Chamada{Provedor: Groq}, chaveDeTeste, audioWebm())
	if err != nil {
		t.Fatalf("Transcrever: %v", err)
	}
	if res.Text == "" {
		t.Error("sem texto")
	}

	v := <-saida
	if v.err != nil {
		t.Fatalf("o servidor cru: %v", v.err)
	}
	var tem, chunked bool
	for _, h := range v.cabeca {
		baixa := strings.ToLower(h)
		if strings.HasPrefix(baixa, "content-length: ") {
			tem = true
			if n, _ := strconv.Atoi(strings.TrimPrefix(baixa, "content-length: ")); n != v.corpo {
				t.Errorf("Content-Length = %d, mas o corpo tinha %d bytes", n, v.corpo)
			}
		}
		if strings.HasPrefix(baixa, "transfer-encoding:") || strings.Contains(baixa, "chunked") {
			chunked = true
		}
	}
	if !tem {
		t.Errorf("o pedido saiu sem Content-Length:\n%s", strings.Join(v.cabeca, "\n"))
	}
	if chunked {
		t.Errorf("o pedido saiu chunked:\n%s", strings.Join(v.cabeca, "\n"))
	}
	if v.cabeca[0] != "POST /openai/v1/audio/transcriptions HTTP/1.1" {
		t.Errorf("linha do pedido = %q", v.cabeca[0])
	}
}

// montarCorpo: o corpo que sai é um multipart válido, o tamanho calculado é o
// dos bytes lidos, e o áudio atravessa inteiro.
func TestMontarCorpo_TamanhoCalculadoEConteudo(t *testing.T) {
	audio := audioWebm().Bytes
	campos := []campo{{"model", "m"}, {"language", "pt"}, {"prompt", "termo, outro termo"}}
	c, err := montarCorpo(campos, `dit"ado.webm`, "audio/webm", audio)
	if err != nil {
		t.Fatal(err)
	}
	todo, err := io.ReadAll(c.leitor)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(todo)) != c.tamanho {
		t.Fatalf("tamanho calculado %d, bytes lidos %d", c.tamanho, len(todo))
	}

	tipo, params, err := mime.ParseMediaType(c.contentType)
	if err != nil || tipo != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("Content-Type = %q (%v)", c.contentType, err)
	}
	mr := multipart.NewReader(bytes.NewReader(todo), params["boundary"])
	var nomes []string
	var arquivo []byte
	var nomeArquivo string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("multipart inválido: %v", err)
		}
		dados, _ := io.ReadAll(p)
		if p.FileName() != "" {
			arquivo, nomeArquivo = dados, p.FileName()
			if got := p.Header.Get("Content-Type"); got != "audio/webm" {
				t.Errorf("Content-Type do arquivo = %q", got)
			}
			continue
		}
		nomes = append(nomes, p.FormName()+"="+string(dados))
	}
	want := []string{"model=m", "language=pt", "prompt=termo, outro termo"}
	if strings.Join(nomes, "|") != strings.Join(want, "|") {
		t.Errorf("campos = %v, quero %v", nomes, want)
	}
	if !bytes.Equal(arquivo, audio) {
		t.Error("o áudio que saiu do multipart não é o que entrou")
	}
	if nomeArquivo != `dit"ado.webm` {
		t.Errorf("nome do arquivo = %q (o escape é do mime/multipart)", nomeArquivo)
	}
}

// Sem segunda cópia do áudio: montar o corpo e lê-lo todo aloca bem menos que o
// próprio áudio, que é só referenciado.
func TestMontarCorpo_NaoCopiaOAudio(t *testing.T) {
	audio := make([]byte, 8<<20)
	for i := range audio {
		audio[i] = byte(i)
	}
	campos := []campo{{"model", "m"}, {"language", "pt"}, {"temperature", "0"}}

	var antes, depois runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&antes)
	c, err := montarCorpo(campos, "ditado.webm", "audio/webm", audio)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, c.leitor)
	if err != nil || n != c.tamanho {
		t.Fatalf("lido %d (%v), tamanho %d", n, err, c.tamanho)
	}
	runtime.ReadMemStats(&depois)

	// io.Copy para io.Discard usa um buffer de 8 KiB; o resto é o cabeçalho.
	// Uma cópia do áudio custaria 8 MiB.
	if alocado := depois.TotalAlloc - antes.TotalAlloc; alocado > 256<<10 {
		t.Errorf("montar e ler o corpo alocou %d bytes para um áudio de %d: parece haver uma cópia", alocado, len(audio))
	}
	runtime.KeepAlive(audio)
}

// ---------------------------------------------------------------------
// Só HTTP/1.1, e os endereços oficiais
// ---------------------------------------------------------------------

type protocolos struct {
	mu sync.Mutex
	v  []int
}

func (p *protocolos) add(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.v = append(p.v, n)
}

func (p *protocolos) tira() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.v
	p.v = nil
	return v
}

// O provedor de teste ACEITA HTTP/2; o Cliente não pode pedir. Com HTTP/2, a
// chave e o começo do corpo iriam ao log padrão sob GODEBUG=http2debug, e o
// Authorization ficaria na tabela HPACK da conexão.
func TestTransportePadrao_NaoFalaHTTP2(t *testing.T) {
	var protos protocolos
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		protos.add(r.ProtoMajor)
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"whisper-1","created":1}]}`)
			return
		}
		io.WriteString(w, respostaOK)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	// Controle: um cliente comum negocia HTTP/2 com este servidor. Sem isto,
	// "chegou HTTP/1.1" poderia ser só o servidor que não oferece HTTP/2.
	resp, err := srv.Client().Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := protos.tira(); len(v) != 1 || v[0] != 2 {
		t.Fatalf("controle: o cliente comum falou HTTP/%v, quero HTTP/2 (o servidor de teste devia oferecer)", v)
	}

	// O transporte do pacote, confiando no certificado do servidor de teste.
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr := novoTransporteHTTP1()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	defer tr.CloseIdleConnections()
	antes := transporteHTTP1
	transporteHTTP1 = tr
	t.Cleanup(func() { transporteHTTP1 = antes })

	cli := &Cliente{BaseOpenAI: srv.URL + "/v1", BaseGroq: srv.URL + "/openai/v1"}
	for _, prov := range []string{OpenAI, Groq} {
		if _, err := cli.Transcrever(context.Background(), Chamada{Provedor: prov}, chaveDeTeste, audioWebm()); err != nil {
			t.Fatalf("Transcrever %s: %v", prov, err)
		}
		if _, err := cli.ListarModelos(context.Background(), prov, chaveDeTeste); err != nil {
			t.Fatalf("ListarModelos %s: %v", prov, err)
		}
	}
	// Um HTTP injetado que não traz Transport também fica em HTTP/1.1.
	cli.HTTP = &http.Client{}
	if _, err := cli.Transcrever(context.Background(), Chamada{Provedor: OpenAI}, chaveDeTeste, audioWebm()); err != nil {
		t.Fatal(err)
	}

	v := protos.tira()
	if len(v) != 5 {
		t.Fatalf("o provedor recebeu %d pedidos, quero 5", len(v))
	}
	for i, p := range v {
		if p != 1 {
			t.Errorf("pedido %d chegou em HTTP/%d, quero HTTP/1", i, p)
		}
	}
}

func TestNovoTransporteHTTP1_SoHTTP1EComProxyDoAmbiente(t *testing.T) {
	tr := novoTransporteHTTP1()
	if tr.Protocols == nil || !tr.Protocols.HTTP1() || tr.Protocols.HTTP2() || tr.Protocols.UnencryptedHTTP2() {
		t.Fatalf("Protocols = %v, quero só HTTP/1.1", tr.Protocols)
	}
	if tr.ForceAttemptHTTP2 || len(tr.TLSNextProto) != 0 {
		t.Error("o transporte tem caminho para o HTTP/2")
	}
	if tr.Proxy == nil || tr.TLSHandshakeTimeout <= 0 || tr.IdleConnTimeout <= 0 || tr.DialContext == nil {
		t.Error("o transporte não tem o proxy do ambiente e os prazos do transporte padrão")
	}
}

// O pedido não pode ser reenviado sozinho pelo transporte: sem GetBody e sem
// Idempotency-Key, o net/http não repete um POST numa conexão velha, e um
// ditado nunca é cobrado duas vezes por obra dele. O Content-Length é o do
// corpo todo.
func TestTranscribe_PedidoNaoSeReenviaSozinho(t *testing.T) {
	var visto *http.Request
	cli := &Cliente{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		visto = r
		io.Copy(io.Discard, r.Body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(respostaOK)), Header: http.Header{}}, nil
	})}}
	if _, err := cli.Transcrever(context.Background(), Chamada{Provedor: OpenAI}, chaveDeTeste, audioWebm()); err != nil {
		t.Fatal(err)
	}
	if visto.GetBody != nil {
		t.Error("o pedido tem GetBody: o transporte poderia reenviá-lo sozinho")
	}
	if visto.Header.Get("Idempotency-Key") != "" {
		t.Error("o pedido tem Idempotency-Key")
	}
	if visto.ContentLength <= int64(len(audioWebm().Bytes)) {
		t.Errorf("ContentLength = %d, quero o tamanho do corpo todo", visto.ContentLength)
	}
}

// Os endereços oficiais, sem rede nenhuma: um RoundTripper que só anota o
// pedido e devolve um 200.
func TestEnderecosOficiais(t *testing.T) {
	var urls, auth []string
	cli := &Cliente{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.Method+" "+r.URL.String())
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Body != nil {
			io.Copy(io.Discard, r.Body)
		}
		corpo := respostaOK
		if strings.HasSuffix(r.URL.Path, "/models") {
			corpo = `{"data":[]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(corpo)), Header: http.Header{}}, nil
	})}}

	for _, prov := range []string{OpenAI, Groq} {
		if _, err := cli.Transcrever(context.Background(), Chamada{Provedor: prov}, chaveDeTeste, audioWebm()); err != nil {
			t.Fatal(err)
		}
		if _, err := cli.ListarModelos(context.Background(), prov, chaveDeTeste); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"POST https://api.openai.com/v1/audio/transcriptions",
		"GET https://api.openai.com/v1/models",
		"POST https://api.groq.com/openai/v1/audio/transcriptions",
		"GET https://api.groq.com/openai/v1/models",
	}
	if strings.Join(urls, "\n") != strings.Join(want, "\n") {
		t.Errorf("endereços:\n%s\nquero:\n%s", strings.Join(urls, "\n"), strings.Join(want, "\n"))
	}
	for i, a := range auth {
		if a != "Bearer "+chaveDeTeste {
			t.Errorf("pedido %d: Authorization = %q", i, a)
		}
	}
}
