package chavestest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Os prazos da rota de ditar num servidor HTTP de verdade (com conexão, o
// ResponseController e os prazos do http.Server), e o canário de áudio e de
// texto (ADR-021, "Prazos" e "Memória, disco e log"; C5b).

// servidorDeVerdade sobe a nuvem do mundo num httptest.Server, com o
// writeTimeout dado (zero: sem prazo).
func (m *mundo) servidorDeVerdade(writeTimeout time.Duration) *httptest.Server {
	m.t.Helper()
	srv := httptest.NewUnstartedServer(m.h)
	srv.Config.WriteTimeout = writeTimeout
	srv.Start()
	m.t.Cleanup(srv.Close)
	return srv
}

// pedidoDeVerdade é o POST /dictations da sessão, para o servidor de verdade.
func (m *mundo) pedidoDeVerdade(srv *httptest.Server, quem, id string, corpo io.Reader, tamanho int64) *http.Request {
	m.t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+rotaDitar, corpo)
	if err != nil {
		m.t.Fatal(err)
	}
	req.ContentLength = tamanho
	req.Host = contracttest.HostNuvem
	req.Header.Set("Content-Type", "audio/webm")
	req.Header.Set("Ditado-Id", id)
	req.Header.Set("Ditado-Duracao-Ms", "4000")
	req.Header.Set("Ditado-Fala-Ms", "2000")
	req.Header.Set("Cookie", "__Host-sessao="+m.creds[quem].valor)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return req
}

// corpoLento entrega o áudio em pedaços, com a pausa dada entre eles.
type corpoLento struct {
	pedacos [][]byte
	pausa   time.Duration
	i       int
}

func (c *corpoLento) Read(p []byte) (int, error) {
	if c.i >= len(c.pedacos) {
		return 0, io.EOF
	}
	if c.i > 0 {
		time.Sleep(c.pausa)
	}
	n := copy(p, c.pedacos[c.i])
	c.pedacos[c.i] = c.pedacos[c.i][n:]
	if len(c.pedacos[c.i]) == 0 {
		c.i++
	}
	return n, nil
}

func emPedacos(b []byte, n int) [][]byte {
	var out [][]byte
	tam := (len(b) + n - 1) / n
	for len(b) > 0 {
		k := min(tam, len(b))
		out = append(out, b[:k])
		b = b[k:]
	}
	return out
}

// O corpo lento dentro do prazo recebe a resposta inteira; o corpo além do
// prazo é cortado antes do provedor, e a vaga volta.
func testDitarCorpoLento(t *testing.T, m *mundo) {
	m.prazos = server.PrazosDoDitado{Corpo: 1500 * time.Millisecond}
	m.prepararDitado(celA, "openai", "", false)
	srv := m.servidorDeVerdade(0)
	audio := webm(strings.Repeat("b", 32<<10))

	id := cloudstore.NewUUIDv7()
	resp, err := srv.Client().Do(m.pedidoDeVerdade(srv, celA, id, &corpoLento{pedacos: emPedacos(audio, 5), pausa: 120 * time.Millisecond}, int64(len(audio))))
	if err != nil {
		t.Fatalf("o corpo lento dentro do prazo: %v", err)
	}
	corpo, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var d ditadoResp
	if resp.StatusCode != http.StatusCreated || json.Unmarshal(corpo, &d) != nil || d.ID != id {
		t.Fatalf("o corpo lento dentro do prazo: %d %s", resp.StatusCode, corpo)
	}

	m.prazos = server.PrazosDoDitado{Corpo: 300 * time.Millisecond}
	m.montar(ChaveDeCifraA)
	srv = m.servidorDeVerdade(0)
	antes := m.voz.quantas()
	id = cloudstore.NewUUIDv7()
	resp, err = srv.Client().Do(m.pedidoDeVerdade(srv, celA, id, &corpoLento{pedacos: emPedacos(audio, 2), pausa: 1200 * time.Millisecond}, int64(len(audio))))
	if err == nil {
		corpo, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
			t.Fatalf("o corpo além do prazo foi aceito: %d %s", resp.StatusCode, corpo)
		}
	}
	if m.voz.quantas() != antes || m.situacao(celA, id).Estado != cloudstore.IdNada {
		t.Fatalf("o corpo além do prazo chegou ao provedor (%d chamadas) ou criou um ditado", m.voz.quantas()-antes)
	}
	// A vaga voltou: o ditado seguinte passa.
	resp, err = srv.Client().Do(m.pedidoDeVerdade(srv, celA, cloudstore.NewUUIDv7(), bytes.NewReader(audio), int64(len(audio))))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("depois do corte, o ditado seguinte: %d", resp.StatusCode)
	}
}

// Quem espera o mesmo id estende o próprio prazo de escrita: com o writeTimeout
// do servidor bem menor que a demora do provedor, os dois pedidos recebem a
// resposta inteira (o primeiro 201, o reenvio 200), com uma chamada só.
func testDitarQuemEsperaPassaDoWriteTimeout(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	srv := m.servidorDeVerdade(300 * time.Millisecond)
	entrou := make(chan struct{}, 1)
	m.voz.enquantoResponde(t, func() {
		select {
		case entrou <- struct{}{}:
		default:
		}
		time.Sleep(1200 * time.Millisecond)
	})
	audio := webm()
	id := cloudstore.NewUUIDv7()
	type resultado struct {
		status int
		corpo  []byte
		err    error
	}
	pedir := func() resultado {
		resp, err := srv.Client().Do(m.pedidoDeVerdade(srv, celA, id, bytes.NewReader(audio), int64(len(audio))))
		if err != nil {
			return resultado{err: err}
		}
		defer resp.Body.Close()
		c, err := io.ReadAll(resp.Body)
		return resultado{status: resp.StatusCode, corpo: c, err: err}
	}
	primeiro := make(chan resultado, 1)
	go func() { primeiro <- pedir() }()
	<-entrou
	segundo := pedir()
	p := <-primeiro
	for nome, r := range map[string]resultado{"o primeiro": p, "o reenvio": segundo} {
		var d ditadoResp
		if r.err != nil || json.Unmarshal(r.corpo, &d) != nil || d.ID != id {
			t.Fatalf("%s não recebeu a resposta inteira: %v %d %s", nome, r.err, r.status, r.corpo)
		}
	}
	if p.status != http.StatusCreated || segundo.status != http.StatusOK || m.voz.quantas() != 1 {
		t.Fatalf("status %d e %d, %d chamadas", p.status, segundo.status, m.voz.quantas())
	}
}

// O canário: o áudio nunca aparece no log, em resposta nenhuma, no banco nem no
// diretório temporário; o texto ditado nunca aparece no log nem numa resposta de
// erro; a chave e o corpo de erro do provedor, em lugar nenhum. Passa por todos
// os desfechos: o ditado criado, com e sem o Polish, a falha do provedor, o sem
// fala, o formato errado, o corpo grande demais e a gravação que falha.
func testDitarCanario(t *testing.T, m *mundo) {
	temp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP", "TEMPDIR"} {
		t.Setenv(v, temp)
	}
	logsPadrao := capturarLogs(t)
	canarioAudio := "CANARIO-DO-AUDIO-" + cloudstore.NewUUIDv7()
	canarioTexto := "canariodotexto" + strings.ReplaceAll(cloudstore.NewUUIDv7()[:8], "-", "")
	falado := "hoje eu falei o " + canarioTexto + " para o cliente da empresa"
	audio := webm(strings.Repeat(canarioAudio, 64))

	m.prepararDitado(celA, "openai", "openai", false)
	falha := m.comGravacaoQueFalha(0)
	var erros, sucessos []string
	guardar := func(rec *httptest.ResponseRecorder) {
		if rec.Code >= 400 {
			erros = append(erros, rec.Body.String())
		} else {
			sucessos = append(sucessos, rec.Body.String())
		}
	}
	m.voz.responder(falado, 0)
	m.pol.responder(0, nil)
	r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), audio)
	if r.Code != http.StatusCreated || !strings.Contains(r.Body.String(), canarioTexto) {
		t.Fatalf("o ditado do canário: %d %s", r.Code, r.Body.String())
	}
	guardar(r)
	m.pol.responder(http.StatusInternalServerError, nil)
	r, _ = m.ditar(celA, cloudstore.NewUUIDv7(), audio)
	guardar(r)
	m.pol.responder(0, nil)
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway} {
		m.voz.responder(falado, status)
		r, _ = m.ditar(celA, cloudstore.NewUUIDv7(), audio)
		guardar(r)
	}
	m.voz.responder("", 0)
	r, _ = m.ditar(celA, cloudstore.NewUUIDv7(), audio)
	guardar(r)
	m.voz.responder(falado, 0)
	falha.mu.Lock()
	falha.falhas = 1
	falha.mu.Unlock()
	r, _ = m.ditar(celA, cloudstore.NewUUIDv7(), audio)
	guardar(r)
	r, _ = m.ditar(celA, cloudstore.NewUUIDv7(), []byte(strings.Repeat(canarioAudio, 64)))
	guardar(r)
	r, _ = m.ditar(celA, cloudstore.NewUUIDv7(), webm(strings.Repeat(canarioAudio, server.TetoDoCorpoDoDitado/len(canarioAudio)+1)),
		func(r *http.Request) { r.ContentLength = -1 })
	guardar(r)
	if len(erros) < 6 || len(sucessos) < 2 {
		t.Fatalf("o canário não passou por todos os desfechos: %d erros, %d sucessos", len(erros), len(sucessos))
	}

	pedacosDoAudio := []string{canarioAudio, hex.EncodeToString([]byte(canarioAudio))}
	logs := m.logs.String() + logsPadrao.String()
	despejo := m.b.Despejo(t)
	for _, p := range pedacosDoAudio {
		if strings.Contains(logs, p) {
			t.Error("o áudio aparece no log")
		}
		if strings.Contains(despejo, p) {
			t.Error("o áudio aparece no banco")
		}
		for _, c := range append(append([]string{}, erros...), sucessos...) {
			if strings.Contains(c, p) {
				t.Error("o áudio aparece numa resposta")
			}
		}
	}
	if strings.Contains(logs, canarioTexto) {
		t.Error("o texto ditado aparece no log")
	}
	for _, c := range erros {
		if strings.Contains(c, canarioTexto) {
			t.Errorf("o texto ditado aparece numa resposta de erro: %s", c)
		}
		semCanario(t, "uma resposta de erro", c, chaveDe("openai"))
	}
	semCanario(t, "o log", logs, chaveDe("openai"))
	if strings.Contains(logs, "Incorrect API key") {
		t.Error("o corpo de erro do provedor aparece no log")
	}
	var novos []string
	_ = filepath.WalkDir(temp, func(p string, d os.DirEntry, err error) error {
		if err == nil && p != temp {
			novos = append(novos, p)
		}
		return nil
	})
	if len(novos) != 0 {
		t.Errorf("o diretório temporário ganhou arquivos: %v", novos)
	}
	// A linha "ditado" saiu, com só chaves da lista.
	if !strings.Contains(m.logs.String(), "msg=ditado ") {
		t.Fatal("o log não tem a linha \"ditado\"")
	}
}
