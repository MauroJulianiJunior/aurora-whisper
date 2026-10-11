package nuvem

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestEhModeloDeTranscricao(t *testing.T) {
	casos := map[string]bool{
		"gpt-transcribe":                true,
		"gpt-4o-transcribe":             true,
		"gpt-4o-mini-transcribe":        true,
		"gpt-4o-mini-transcribe-2025":   true,
		"whisper-1":                     true,
		"whisper-large-v3":              true,
		"whisper-large-v3-turbo":        true,
		"GPT-4O-TRANSCRIBE":             true,
		"gpt-4o-transcribe-diarize":     false, // separa quem falou; outro formato de resposta
		"distil-whisper-large-v3-en":    false, // só inglês
		"whisper-base.en":               false,
		"gpt-4o":                        false,
		"gpt-4o-mini-tts":               false,
		"tts-1":                         false,
		"text-embedding-3-small":        false,
		"dall-e-3":                      false,
		"llama-3.3-70b-versatile":       false,
		"":                              false,
		"transcribe-sem-prefixo-sufixo": true,
	}
	for id, want := range casos {
		if got := EhModeloDeTranscricao(id); got != want {
			t.Errorf("EhModeloDeTranscricao(%q) = %v, quero %v", id, got, want)
		}
	}
}

// A lista do provedor vem filtrada aos modelos de transcrição, do mais novo ao
// mais antigo.
func TestListarModelos_FiltraETranscricaoOrdena(t *testing.T) {
	const lista = `{"object":"list","data":[
		{"id":"gpt-4o","created":100},
		{"id":"whisper-1","created":50},
		{"id":"gpt-4o-mini-transcribe","created":300},
		{"id":"text-embedding-3-small","created":400},
		{"id":"gpt-transcribe","created":500},
		{"id":"gpt-4o-transcribe","created":300},
		{"id":"tts-1","created":10},
		{"id":"gpt-4o-transcribe-diarize","created":600},
		{"id":"gpt-transcribe","created":500},
		{"id":"","created":700}
	]}`
	for _, prov := range []string{OpenAI, Groq} {
		srv := novoProvedorFalso(t, responder(200, lista))
		ids, err := clienteDe(srv).ListarModelos(context.Background(), prov, chaveDeTeste)
		if err != nil {
			t.Fatalf("%s: %v", prov, err)
		}
		want := []string{"gpt-transcribe", "gpt-4o-mini-transcribe", "gpt-4o-transcribe", "whisper-1"}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("%s: modelos = %v, quero %v", prov, ids, want)
		}

		v := srv.pedidos()[0]
		caminho := "/v1/models"
		if prov == Groq {
			caminho = "/openai/v1/models"
		}
		if v.Metodo != http.MethodGet || v.Caminho != caminho {
			t.Errorf("%s: pedido %s %s, quero GET %s", prov, v.Metodo, v.Caminho, caminho)
		}
		if got := v.Cab.Get("Authorization"); got != "Bearer "+chaveDeTeste {
			t.Errorf("%s: Authorization = %q", prov, got)
		}
	}
}

func TestListarModelos_ListaVaziaNaoENula(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, `{"data":[{"id":"gpt-4o","created":1}]}`))
	ids, err := clienteDe(srv).ListarModelos(context.Background(), Groq, chaveDeTeste)
	if err != nil {
		t.Fatal(err)
	}
	if ids == nil || len(ids) != 0 {
		t.Errorf("modelos = %#v, quero uma lista vazia não nula", ids)
	}
}

// Os mesmos motivos da transcrição, sem o corpo, e a chave nunca na mensagem.
func TestListarModelos_StatusViraMotivo(t *testing.T) {
	casos := []struct {
		status int
		motivo Motivo
	}{
		{401, MotivoChaveRecusada},
		{403, MotivoSemPermissao},
		{429, MotivoLimite},
		{500, MotivoIndisponivel},
		{503, MotivoIndisponivel},
		{404, MotivoRespostaInesperada},
		{302, MotivoRespostaInesperada},
	}
	for _, c := range casos {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			destino := novoProvedorFalso(t, responder(200, `{"data":[]}`))
			srv := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
				if c.status == 302 {
					w.Header().Set("Location", destino.URL+"/models")
				}
				w.WriteHeader(c.status)
				w.Write([]byte(corpoDeErroDoProvedor()))
			})
			_, err := clienteDe(srv).ListarModelos(context.Background(), OpenAI, chaveDeTeste)
			var e *Erro
			if !errors.As(err, &e) || e.Motivo != c.motivo {
				t.Fatalf("err = %v, quero *Erro com motivo %q", err, c.motivo)
			}
			semRastro(t, "o erro", err.Error(), chaveDeTeste, canarioCorpo, "Incorrect API key", srv.URL)
			if n := len(destino.pedidos()); n != 0 {
				t.Errorf("o redirecionamento foi seguido (%d pedidos no destino)", n)
			}
		})
	}
}

func TestListarModelos_RespostaIlegivel(t *testing.T) {
	for nome, corpo := range map[string]string{
		"não é JSON": `<html/>`,
		"sem data":   `{"object":"list"}`,
		"data nulo":  `{"data":null}`,
		"lista":      `[]`,
		"vazia":      ``,
	} {
		srv := novoProvedorFalso(t, responder(200, corpo))
		_, err := clienteDe(srv).ListarModelos(context.Background(), Groq, chaveDeTeste)
		var e *Erro
		if !errors.As(err, &e) || e.Motivo != MotivoRespostaInesperada {
			t.Errorf("%s: err = %v, quero MotivoRespostaInesperada", nome, err)
		}
	}
}

func TestListarModelos_ChaveEProvedor(t *testing.T) {
	srv := novoProvedorFalso(t, responder(200, `{"data":[]}`))
	cli := clienteDe(srv)

	if _, err := cli.ListarModelos(context.Background(), "anthropic", chaveDeTeste); err == nil {
		t.Error("ListarModelos com provedor desconhecido = nil")
	}
	_, err := cli.ListarModelos(context.Background(), OpenAI, "")
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoSemChave {
		t.Errorf("sem chave: err = %v, quero MotivoSemChave", err)
	}
	_, err = cli.ListarModelos(context.Background(), OpenAI, "com espaço")
	if !errors.As(err, &e) || e.Motivo != MotivoChaveRecusada {
		t.Errorf("chave mal formada: err = %v, quero MotivoChaveRecusada", err)
	}
	if n := len(srv.pedidos()); n != 0 {
		t.Errorf("o provedor recebeu %d pedidos", n)
	}
}

func TestListarModelos_TempoEsgotado(t *testing.T) {
	srv := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	cli := clienteDe(srv)
	cli.TimeoutLista = 80 * time.Millisecond
	_, err := cli.ListarModelos(context.Background(), OpenAI, chaveDeTeste)
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoTempoEsgotado {
		t.Fatalf("err = %v, quero MotivoTempoEsgotado", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("o erro do prazo não embrulha context.DeadlineExceeded")
	}
}
