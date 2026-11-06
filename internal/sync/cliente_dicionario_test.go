package syncengine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// O cliente do dicionário contra um servidor de verdade (httptest): o método, o
// caminho, o token e os dois cabeçalhos do PUT, o estado e o motivo lidos, e o
// que cada erro devolve (sem a URL, o token nem o corpo).

const tokenDoTeste = "token-que-nao-pode-vazar-0123456789abcdefghijk"

func servidorDoDicionario(t *testing.T, h http.HandlerFunc) (*Cliente, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NovoCliente(srv.URL, tokenDoTeste, nil), srv
}

func TestClienteDoDicionario_PUTLevaOsCabecalhosEOCorpo(t *testing.T) {
	var metodo, caminho, auth, tipo, sha, fonte string
	var corpo []byte
	cli, _ := servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) {
		metodo, caminho, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		tipo, sha, fonte = r.Header.Get("Content-Type"), r.Header.Get("Dicionario-SHA256"), r.Header.Get("Dicionario-Fonte")
		corpo, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"fonte":"este","aparelho":"PC-DO-MAURO","termos":49,"sha256":"abc","atualizado_em":"2026-11-05T14:02:00Z"}`)
	})
	for _, modo := range []ModoDoEnvio{EnvioTomar, EnvioManter} {
		r, err := cli.SubirDicionario(context.Background(), []byte(`{"usuario":"copia"}`), "abc123", modo)
		if err != nil {
			t.Fatal(err)
		}
		if metodo != http.MethodPut || caminho != "/api/v1/sync/dictionary" || auth != "Bearer "+tokenDoTeste ||
			tipo != "application/json" || sha != "abc123" || fonte != string(modo) || string(corpo) != `{"usuario":"copia"}` {
			t.Errorf("o pedido: %s %s auth %v tipo %q sha %q fonte %q corpo %q", metodo, caminho, auth != "", tipo, sha, fonte, corpo)
		}
		want := time.Date(2026, 11, 5, 14, 2, 0, 0, time.UTC)
		if r.Status != 200 || r.Estado == nil || r.Estado.Fonte != FonteEste || r.Estado.Aparelho != "PC-DO-MAURO" ||
			r.Estado.Termos != 49 || r.Estado.SHA256 != "abc" || !r.Estado.AtualizadoEm.Equal(want) {
			t.Errorf("a resposta: %+v %+v", r, r.Estado)
		}
	}
}

func TestClienteDoDicionario_GETEDELETE(t *testing.T) {
	var metodo, auth string
	cli, _ := servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) {
		metodo, auth = r.Method, r.Header.Get("Authorization")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, `{"fonte":"nenhuma"}`)
	})
	r, err := cli.EstadoDoDicionario(context.Background())
	if err != nil || metodo != http.MethodGet || auth != "Bearer "+tokenDoTeste || r.Status != 200 ||
		r.Estado == nil || r.Estado.Fonte != FonteNenhuma || r.Estado.Aparelho != "" || r.Estado.Termos != 0 {
		t.Fatalf("GET: %v, %+v, método %s", err, r, metodo)
	}
	r, err = cli.ApagarDicionario(context.Background())
	if err != nil || metodo != http.MethodDelete || r.Status != 204 || r.Estado != nil {
		t.Fatalf("DELETE: %v, %+v, método %s", err, r, metodo)
	}
}

func TestClienteDoDicionario_AsRespostasDeErro(t *testing.T) {
	casos := []struct {
		nome   string
		status int
		corpo  string
		quer   RespostaDoDicionario
	}{
		{"409 com o estado no topo", 409,
			`{"error":{"code":"nao_e_a_fonte","message":"x","request_id":"r"},"fonte":"outro","aparelho":"NOTEBOOK","termos":3,"sha256":"s","atualizado_em":"2026-11-05T14:02:00Z"}`,
			RespostaDoDicionario{Status: 409, Codigo: CodigoNaoEAFonte, Estado: &EstadoDoDicionario{Fonte: FonteOutro, Aparelho: "NOTEBOOK", Termos: 3, SHA256: "s",
				AtualizadoEm: time.Date(2026, 11, 5, 14, 2, 0, 0, time.UTC)}}},
		{"409 com o estado em estado", 409,
			`{"error":{"code":"nao_e_a_fonte"},"estado":{"fonte":"nenhuma"}}`,
			RespostaDoDicionario{Status: 409, Codigo: CodigoNaoEAFonte, Estado: &EstadoDoDicionario{Fonte: FonteNenhuma}}},
		{"409 com code no topo", 409, `{"code":"nao_e_a_fonte","fonte":"outro","aparelho":"N"}`,
			RespostaDoDicionario{Status: 409, Codigo: CodigoNaoEAFonte, Estado: &EstadoDoDicionario{Fonte: FonteOutro, Aparelho: "N"}}},
		{"422 com o motivo em error", 422, `{"error":{"code":"dicionario_invalido","motivo":"termos_demais"}}`,
			RespostaDoDicionario{Status: 422, Codigo: CodigoDicionarioInvalid, Motivo: "termos_demais"}},
		{"422 com o motivo no topo", 422, `{"code":"dicionario_invalido","motivo":"grande_demais"}`,
			RespostaDoDicionario{Status: 422, Codigo: CodigoDicionarioInvalid, Motivo: "grande_demais"}},
		{"404 sem corpo", 404, ``, RespostaDoDicionario{Status: 404}},
		{"400", 400, `{"error":{"code":"BAD_REQUEST","message":"x"}}`, RespostaDoDicionario{Status: 400, Codigo: "BAD_REQUEST"}},
		{"500 com lixo", 500, `<html>`, RespostaDoDicionario{Status: 500}},
		{"fonte desconhecida não vira estado", 200, `{"fonte":"qualquer"}`, RespostaDoDicionario{Status: 200}},
	}
	for _, c := range casos {
		cli, _ := servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.corpo)
		})
		got, err := cli.SubirDicionario(context.Background(), []byte("{}"), "s", EnvioManter)
		if err != nil {
			t.Errorf("%s: erro %v", c.nome, err)
			continue
		}
		if got.Status != c.quer.Status || got.Codigo != c.quer.Codigo || got.Motivo != c.quer.Motivo {
			t.Errorf("%s: %+v, quero %+v", c.nome, got, c.quer)
		}
		switch {
		case (got.Estado == nil) != (c.quer.Estado == nil):
			t.Errorf("%s: estado %+v, quero %+v", c.nome, got.Estado, c.quer.Estado)
		case got.Estado != nil && (got.Estado.Fonte != c.quer.Estado.Fonte || got.Estado.Aparelho != c.quer.Estado.Aparelho ||
			got.Estado.Termos != c.quer.Estado.Termos || got.Estado.SHA256 != c.quer.Estado.SHA256 ||
			!got.Estado.AtualizadoEm.Equal(c.quer.Estado.AtualizadoEm)):
			t.Errorf("%s: estado %+v, quero %+v", c.nome, got.Estado, c.quer.Estado)
		}
	}
}

func TestClienteDoDicionario_OsErrosQueParamAFase(t *testing.T) {
	// 401.
	cli, _ := servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	if _, err := cli.SubirDicionario(context.Background(), []byte("{}"), "s", EnvioManter); !errors.Is(err, ErrNaoAutorizado) {
		t.Errorf("401: %v", err)
	}
	// 429 com Retry-After.
	cli, _ = servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var esp *ErrEspere
	if _, err := cli.EstadoDoDicionario(context.Background()); !errors.As(err, &esp) || esp.Espera != 7*time.Second {
		t.Errorf("429: %v", err)
	}
	// Redirecionamento: não é seguido, e o token não vai ao outro endereço.
	var visitado bool
	alvo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { visitado = true }))
	defer alvo.Close()
	cli, _ = servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, alvo.URL+"/outro", http.StatusTemporaryRedirect)
	})
	_, err := cli.SubirDicionario(context.Background(), []byte("{}"), "s", EnvioManter)
	var f *Falha
	if !errors.As(err, &f) || f.Codigo != CodigoRedirecionamento || visitado {
		t.Errorf("redirecionamento: %v (seguiu %v)", err, visitado)
	}
	// A rede: o servidor fechado. O erro não leva a URL nem o token.
	cli, srv := servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) {})
	srv.Close()
	_, err = cli.SubirDicionario(context.Background(), []byte(`{"termo":"segredo-do-corpo"}`), "s", EnvioManter)
	if !errors.Is(err, ErrTransitorio) {
		t.Fatalf("rede: %v", err)
	}
	for _, proibido := range []string{srv.URL, "127.0.0.1", tokenDoTeste, "segredo-do-corpo"} {
		if strings.Contains(err.Error(), proibido) {
			t.Errorf("o erro de rede leva %q: %v", proibido, err)
		}
	}
	// O contexto cancelado volta como contexto cancelado.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cli, _ = servidorDoDicionario(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := cli.EstadoDoDicionario(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelado: %v", err)
	}
}
