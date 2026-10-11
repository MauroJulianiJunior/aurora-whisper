package syncengine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const tokenDeMentira = "tokentokentokentokentokentokentokentokenAAA"

// nuvemHTTP é um servidor que responde o que o teste manda e anota o que
// recebeu.
func nuvemHTTP(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func umaMutacao() []Mutation {
	return []Mutation{{MutationID: "0199a000-0000-7000-8000-000000000001", EntityType: "transcription",
		EntityID: idDoTeste, Operation: OpUpsert, Payload: json.RawMessage(`{"final_text":"x"}`)}}
}

// O push manda o Bearer e o lote, e confere a resposta.
func TestCliente_Push(t *testing.T) {
	var recebido struct {
		Mutations []Mutation `json:"mutations"`
	}
	var auth, tipo, metodo, caminho string
	srv := nuvemHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		auth, tipo, metodo, caminho = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&recebido)
		_, _ = io.WriteString(w, `{"results":[{"mutation_id":"0199A000-0000-7000-8000-000000000001","status":"applied","entity_version":2,"mesclado":true}],"sync_epoch":"e1"}`)
	})
	c := NovoCliente(srv.URL+"/", tokenDeMentira, nil)
	res, err := c.Push(context.Background(), umaMutacao())
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer "+tokenDeMentira || tipo != "application/json" || metodo != http.MethodPost || caminho != "/api/v1/sync/push" {
		t.Errorf("pedido: %q %q %q %q", auth, tipo, metodo, caminho)
	}
	if len(recebido.Mutations) != 1 || recebido.Mutations[0].EntityID != idDoTeste {
		t.Errorf("lote recebido: %+v", recebido)
	}
	if res.Epoch != "e1" || !res.Results[0].Mesclado || res.Results[0].EntityVersion != 2 {
		t.Errorf("resposta: %+v", res)
	}
}

// A resposta que não bate com o lote é ErrContrato: o lote fica em voo.
func TestCliente_PushForaDoContrato(t *testing.T) {
	for nome, corpo := range map[string]string{
		"sem epoch":        `{"results":[{"mutation_id":"0199a000-0000-7000-8000-000000000001","status":"applied"}]}`,
		"menos resultados": `{"results":[],"sync_epoch":"e1"}`,
		"outro id":         `{"results":[{"mutation_id":"0199a000-0000-7000-8000-000000000009","status":"applied"}],"sync_epoch":"e1"}`,
		"não é JSON":       `<html>`,
	} {
		t.Run(nome, func(t *testing.T) {
			srv := nuvemHTTP(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, corpo) })
			if _, err := NovoCliente(srv.URL, tokenDeMentira, nil).Push(context.Background(), umaMutacao()); !errors.Is(err, ErrContrato) {
				t.Errorf("err = %v; quero ErrContrato", err)
			}
		})
	}
}

// Os status viram os erros tipados.
func TestCliente_Status(t *testing.T) {
	for _, c := range []struct {
		status int
		quero  error
		codigo string
	}{
		{401, ErrNaoAutorizado, "nao_autorizado"},
		{410, ErrRecomeco, "recomeco"},
		{500, ErrTransitorio, CodigoServidor},
		{503, ErrTransitorio, CodigoServidor},
		{400, ErrContrato, CodigoStatus},
		{403, ErrContrato, CodigoStatus},
		{404, ErrContrato, CodigoStatus},
	} {
		srv := nuvemHTTP(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, `{"error":"x"}`)
		})
		cli := NovoCliente(srv.URL, tokenDeMentira, nil)
		_, err := cli.Push(context.Background(), umaMutacao())
		if !errors.Is(err, c.quero) || CodigoDoErro(err) != c.codigo {
			t.Errorf("%d no push: %v (%s)", c.status, err, CodigoDoErro(err))
		}
		_, err = cli.Pull(context.Background(), 0, 10)
		if !errors.Is(err, c.quero) {
			t.Errorf("%d no pull: %v", c.status, err)
		}
	}
}

// O 429 traz a espera do Retry-After, com teto.
func TestCliente_429(t *testing.T) {
	for retry, quero := range map[string]time.Duration{
		"7":                    7 * time.Second,
		"0":                    time.Second,
		"999999":               MaxEsperaPedida,
		"":                     EsperaPadraoDo429,
		"amanhã":               EsperaPadraoDo429,
		"-3":                   EsperaPadraoDo429,
		"99999999999999999999": EsperaPadraoDo429,
	} {
		srv := nuvemHTTP(t, func(w http.ResponseWriter, _ *http.Request) {
			if retry != "" {
				w.Header().Set("Retry-After", retry)
			}
			w.WriteHeader(http.StatusTooManyRequests)
		})
		_, err := NovoCliente(srv.URL, tokenDeMentira, nil).Push(context.Background(), umaMutacao())
		var e *ErrEspere
		if !errors.As(err, &e) || e.Espera != quero {
			t.Errorf("Retry-After %q: %v; quero %v", retry, err, quero)
		}
	}
	// Uma data HTTP também vale, com o mesmo teto, e uma data passada é 1 s.
	agora := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for quando, quero := range map[time.Time]time.Duration{
		agora.Add(90 * time.Second): 90 * time.Second,
		agora.Add(24 * time.Hour):   MaxEsperaPedida,
		agora.Add(-time.Hour):       time.Second,
	} {
		if d := esperaPedida(quando.Format(http.TimeFormat), agora); d != quero {
			t.Errorf("data HTTP %v: %v; quero %v", quando, d, quero)
		}
	}
}

// O redirecionamento não é seguido: o token nunca vai a outro endereço.
func TestCliente_NaoSegueRedirecionamento(t *testing.T) {
	var outro atomic.Int32
	destino := nuvemHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		outro.Add(1)
		_, _ = io.WriteString(w, `{"results":[],"sync_epoch":"e1"}`)
	})
	srv := nuvemHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destino.URL+r.URL.Path, http.StatusTemporaryRedirect)
	})
	_, err := NovoCliente(srv.URL, tokenDeMentira, nil).Push(context.Background(), umaMutacao())
	if !errors.Is(err, ErrTransitorio) || CodigoDoErro(err) != CodigoRedirecionamento {
		t.Errorf("err = %v", err)
	}
	if _, err := NovoCliente(srv.URL, tokenDeMentira, nil).Pull(context.Background(), 0, 10); !errors.Is(err, ErrTransitorio) {
		t.Errorf("pull: %v", err)
	}
	if outro.Load() != 0 {
		t.Error("o redirecionamento foi seguido")
	}
}

// A resposta grande demais é falha de rede (o reenvio dá duplicate).
func TestCliente_RespostaGrandeDemais(t *testing.T) {
	srv := nuvemHTTP(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"changes":[],"last_seq":0,"sync_epoch":"`)
		_, _ = w.Write([]byte(strings.Repeat("a", MaxCorpoDaResposta)))
		_, _ = io.WriteString(w, `"}`)
	})
	_, err := NovoCliente(srv.URL, tokenDeMentira, nil).Pull(context.Background(), 0, 10)
	if !errors.Is(err, ErrTransitorio) || CodigoDoErro(err) != CodigoCorpoGrande {
		t.Errorf("err = %v", err)
	}
}

// O pedido grande demais nem sai.
func TestCliente_PedidoGrandeDemaisNaoSai(t *testing.T) {
	var pedidos atomic.Int32
	srv := nuvemHTTP(t, func(http.ResponseWriter, *http.Request) { pedidos.Add(1) })
	muts := umaMutacao()
	muts[0].Payload = json.RawMessage(`"` + strings.Repeat("a", MaxCorpoDoPedido) + `"`)
	if _, err := NovoCliente(srv.URL, tokenDeMentira, nil).Push(context.Background(), muts); !errors.Is(err, ErrContrato) {
		t.Errorf("err = %v", err)
	}
	if pedidos.Load() != 0 {
		t.Error("o pedido grande saiu")
	}
}

// O prazo: um servidor que não responde é falha transitória, e o erro não
// leva a URL nem o token.
func TestCliente_PrazoEErroSemURL(t *testing.T) {
	solta := make(chan struct{})
	srv := nuvemHTTP(t, func(http.ResponseWriter, *http.Request) { <-solta })
	defer close(solta)
	h := NovoHTTP()
	h.Timeout = 50 * time.Millisecond
	_, err := NovoCliente(srv.URL, tokenDeMentira, h).Pull(context.Background(), 0, 10)
	if !errors.Is(err, ErrTransitorio) || CodigoDoErro(err) != CodigoPrazo {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), tokenDeMentira) {
		t.Errorf("o erro leva a URL ou o token: %v", err)
	}
	// A rede fora (nada escutando).
	_, err = NovoCliente("http://127.0.0.1:1", tokenDeMentira, nil).Pull(context.Background(), 0, 10)
	if !errors.Is(err, ErrTransitorio) || CodigoDoErro(err) != CodigoRede || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("rede fora: %v", err)
	}
	// O contexto cancelado (o Agent saindo) não é falha da nuvem.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NovoCliente(srv.URL, tokenDeMentira, nil).Pull(ctx, 0, 10); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelado: %v", err)
	}
}

// O pull: o pedido, o cursor e o epoch conferidos.
func TestCliente_Pull(t *testing.T) {
	var consulta string
	srv := nuvemHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		consulta = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"changes":[{"seq":8,"entity_type":"transcription","entity_id":"`+idDoTeste+
			`","operation":"tombstone"}],"last_seq":8,"has_more":false,"sync_epoch":"e1"}`)
	})
	p, err := NovoCliente(srv.URL, tokenDeMentira, nil).Pull(context.Background(), 7, 500)
	if err != nil || consulta != "after_seq=7&limit=500" || p.LastSeq != 8 || len(p.Changes) != 1 || p.Changes[0].Operation != OpTombstone {
		t.Errorf("pull: %+v, %v, %q", p, err, consulta)
	}
	// O cursor que volta, ou sem epoch: fora do contrato.
	for _, corpo := range []string{`{"changes":[],"last_seq":3,"sync_epoch":"e1"}`, `{"changes":[],"last_seq":7}`} {
		srv := nuvemHTTP(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, corpo) })
		if _, err := NovoCliente(srv.URL, tokenDeMentira, nil).Pull(context.Background(), 7, 500); !errors.Is(err, ErrContrato) {
			t.Errorf("%s: %v", corpo, err)
		}
	}
}

// A espera depois de cada desfecho (pura).
func TestEsperaDepois(t *testing.T) {
	zero := BackoffPolicy{Base: 2 * time.Second, Max: 5 * time.Minute, Rand: func() float64 { return 0 }}
	um := BackoffPolicy{Base: 2 * time.Second, Max: 5 * time.Minute, Rand: func() float64 { return 0.999999 }}
	intervalo := time.Minute
	for _, c := range []struct {
		nome     string
		err      error
		falhas   int
		p        BackoffPolicy
		min, max time.Duration
	}{
		{"sucesso", nil, 0, zero, intervalo, intervalo},
		{"desconectado", errDesconectado, 0, zero, intervalo, intervalo},
		{"401", ErrNaoAutorizado, 0, zero, intervalo, intervalo},
		{"rede, 1ª", transitorio(CodigoRede), 1, zero, time.Second, time.Second},
		{"rede, 1ª, jitter alto", transitorio(CodigoRede), 1, um, 1999 * time.Millisecond, 2 * time.Second},
		{"rede, 3ª", transitorio(CodigoRede), 3, zero, 4 * time.Second, 4 * time.Second},
		{"rede, no teto", transitorio(CodigoRede), 40, um, 299 * time.Second, 5 * time.Minute},
		{"contrato: espera longa", contrato(CodigoJSON), 1, zero, 150 * time.Second, 150 * time.Second},
		{"410 de novo: espera longa", ErrRecomeco, 1, um, 299 * time.Second, 5 * time.Minute},
		{"429: o pedido, nunca menos", &ErrEspere{Espera: 40 * time.Second}, 1, zero, 40 * time.Second, 40 * time.Second},
		{"429 com jitter", &ErrEspere{Espera: 40 * time.Second}, 1, um, 49 * time.Second, 50 * time.Second},
		{"429 longo: jitter de no máximo 30 s", &ErrEspere{Espera: MaxEsperaPedida}, 1, um, MaxEsperaPedida + 29*time.Second, MaxEsperaPedida + 30*time.Second},
	} {
		got := EsperaDepois(c.err, c.falhas, intervalo, c.p)
		if got < c.min || got > c.max {
			t.Errorf("%s: %v; quero entre %v e %v", c.nome, got, c.min, c.max)
		}
	}
}
