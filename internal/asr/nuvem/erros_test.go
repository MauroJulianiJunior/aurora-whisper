package nuvem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// corpoDeErroDoProvedor imita o que a OpenAI devolve num 401: um pedaço da
// chave recusada, e mais o que o provedor achar útil dizer.
func corpoDeErroDoProvedor() string {
	return `{"error":{"message":"Incorrect API key provided: ` + chaveDeTeste + `. ` + canarioCorpo +
		`","type":"invalid_request_error","code":"invalid_api_key"}}`
}

// Cada status vira um motivo da lista fechada (ADR-021, decisão D), sem o
// corpo da resposta.
func TestTranscribe_StatusViraMotivo(t *testing.T) {
	casos := []struct {
		status int
		motivo Motivo
	}{
		{401, MotivoChaveRecusada},
		{403, MotivoSemPermissao},
		{413, MotivoArquivoGrande},
		{429, MotivoLimite},
		{500, MotivoIndisponivel},
		{502, MotivoIndisponivel},
		{503, MotivoIndisponivel},
		{504, MotivoIndisponivel},
		{599, MotivoIndisponivel},
		{400, MotivoRespostaInesperada},
		{404, MotivoRespostaInesperada},
		{415, MotivoRespostaInesperada},
		{422, MotivoRespostaInesperada},
		{204, MotivoRespostaInesperada}, // sucesso sem texto não é um 200
		{301, MotivoRespostaInesperada},
		{302, MotivoRespostaInesperada},
		{303, MotivoRespostaInesperada},
		{307, MotivoRespostaInesperada},
		{308, MotivoRespostaInesperada},
	}
	for _, provedor := range []string{OpenAI, Groq} {
		for _, c := range casos {
			t.Run(fmt.Sprintf("%s/%d", provedor, c.status), func(t *testing.T) {
				srv := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if c.status >= 300 && c.status < 400 {
						w.Header().Set("Location", "http://127.0.0.1:1/destino")
					}
					w.WriteHeader(c.status)
					if c.status != 204 {
						w.Write([]byte(corpoDeErroDoProvedor()))
					}
				})
				p := providerDe(srv, provedor, "")
				res, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})

				var e *Erro
				if !errors.As(err, &e) {
					t.Fatalf("err = %v (%T), quero *Erro", err, err)
				}
				if e.Motivo != c.motivo {
					t.Errorf("Motivo = %q, quero %q", e.Motivo, c.motivo)
				}
				if e.Provedor != provedor {
					t.Errorf("Provedor = %q", e.Provedor)
				}
				if res.Text != "" {
					t.Errorf("Text = %q numa falha", res.Text)
				}
				// Sem o corpo, sem a chave, sem o áudio, sem a URL.
				semRastro(t, "a mensagem do erro", err.Error(),
					chaveDeTeste, canarioCorpo, "Incorrect API key", canarioAudio, srv.URL, "127.0.0.1")
				if errors.Unwrap(err) != nil {
					t.Errorf("o erro embrulha %v", errors.Unwrap(err))
				}
				if n := len(srv.pedidos()); n != 1 {
					t.Errorf("o provedor recebeu %d pedidos, quero 1 (sem nova tentativa)", n)
				}
			})
		}
	}
}

// Redirecionamento nunca é seguido: o Authorization não vai a outro lugar.
func TestTranscribe_NaoSegueRedirecionamento(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			destino := novoProvedorFalso(t, responder(200, respostaOK))
			origem := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destino.URL+"/roubado")
				w.WriteHeader(status)
			})
			p := providerDe(origem, OpenAI, "")

			_, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
			var e *Erro
			if !errors.As(err, &e) || e.Motivo != MotivoRespostaInesperada {
				t.Fatalf("err = %v, quero *Erro com MotivoRespostaInesperada", err)
			}
			if n := len(origem.pedidos()); n != 1 {
				t.Errorf("a origem recebeu %d pedidos, quero 1", n)
			}
			if n := len(destino.pedidos()); n != 0 {
				t.Errorf("o destino do redirecionamento recebeu %d pedidos: a chave e o áudio foram seguidos para lá", n)
			}
			semRastro(t, "o erro", err.Error(), destino.URL, "roubado")
		})
	}

	// O mesmo vale mesmo com um cliente HTTP injetado que seguiria sozinho.
	t.Run("cliente injetado", func(t *testing.T) {
		destino := novoProvedorFalso(t, responder(200, respostaOK))
		origem := novoProvedorFalso(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destino.URL+"/roubado", http.StatusTemporaryRedirect)
		})
		p := providerDe(origem, Groq, "")
		p.Cliente.HTTP = &http.Client{} // o padrão do net/http segue até 10 redirecionamentos
		if _, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{}); err == nil {
			t.Fatal("Transcribe = nil depois de um redirecionamento")
		}
		if n := len(destino.pedidos()); n != 0 {
			t.Errorf("o destino recebeu %d pedidos", n)
		}
	})
}

// O 413 do provedor (o áudio passou do teto dele) e o 429 (limite ou falta de
// crédito) têm cada um a sua frase para a tela.
func TestErro_MensagensSaoFixasEEmPortugues(t *testing.T) {
	for _, status := range []int{401, 403, 413, 429, 500, 404} {
		e := falhaHTTP(OpenAI, status).(*Erro)
		if e.Mensagem == "" || e.Error() != e.Mensagem {
			t.Errorf("%d: mensagem vazia ou diferente de Error(): %q", status, e.Mensagem)
		}
	}
	if Nome(OpenAI) != "OpenAI" || Nome(Groq) != "Groq" || Nome("x") != "x" {
		t.Error("Nome")
	}
	if !Valido(OpenAI) || !Valido(Groq) || Valido("anthropic") || Valido("") {
		t.Error("Valido")
	}
}

// O CANÁRIO: áudio e chave fora de erro e de log, em todo desfecho. O
// provedor falso devolve a chave e o canário no corpo do erro (como a OpenAI
// faz no 401), e o log padrão e o slog são capturados.
func TestCanario_AudioEChaveForaDeErroEDeLog(t *testing.T) {
	var saida bytes.Buffer
	logAntes, flagsAntes := log.Writer(), log.Flags()
	slogAntes := slog.Default()
	log.SetOutput(&saida)
	slog.SetDefault(slog.New(slog.NewTextHandler(&saida, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		log.SetOutput(logAntes)
		log.SetFlags(flagsAntes)
		slog.SetDefault(slogAntes)
	})

	desfechos := []struct {
		nome   string
		status int
		corpo  string
	}{
		{"200 com texto", 200, respostaOK},
		{"200 sem texto", 200, `{"text":""}`},
		{"200 ilegível", 200, "{isto não é json " + chaveDeTeste},
		{"401", 401, corpoDeErroDoProvedor()},
		{"403", 403, corpoDeErroDoProvedor()},
		{"413", 413, corpoDeErroDoProvedor()},
		{"429", 429, corpoDeErroDoProvedor()},
		{"500", 500, corpoDeErroDoProvedor()},
		{"302", 302, corpoDeErroDoProvedor()},
	}
	var erros []string
	for _, d := range desfechos {
		srv := novoProvedorFalso(t, responder(d.status, d.corpo))
		p := providerDe(srv, OpenAI, "")
		res, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{Vocabulary: canarioDica})
		if err != nil {
			erros = append(erros,
				err.Error(),
				fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Sprintf("%q", err),
				fmt.Sprintf("%v", res), fmt.Sprintf("%+v", res))
		}
		if _, ok := err.(*Erro); ok {
			erros = append(erros, fmt.Sprintf("%+v", *err.(*Erro)), fmt.Sprintf("%#v", *err.(*Erro)))
		}
	}

	// Sem rede, sem chave, formato errado e prazo estourado.
	morto := novoProvedorFalso(t, responder(200, respostaOK))
	pMorto := providerDe(morto, OpenAI, "")
	morto.Close()
	for _, fn := range []func() error{
		func() error {
			_, err := pMorto.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
			return err
		},
		func() error {
			p := providerDe(morto, OpenAI, "")
			p.Chave = func(context.Context) (string, error) { return "", fmt.Errorf("falhou com %s", chaveDeTeste) }
			_, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
			return err
		},
		func() error {
			_, err := pMorto.Transcribe(context.Background(),
				asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: audioWebm().Bytes, Tipo: "audio/x-nada"}}, asr.ASROptions{})
			return err
		},
		func() error {
			_, err := pMorto.Transcribe(context.Background(),
				asr.AudioBuffer{PCM: []int16{1}, Comprimido: audioWebm()}, asr.ASROptions{})
			return err
		},
	} {
		if err := fn(); err != nil {
			erros = append(erros, err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err))
		}
	}
	if len(erros) == 0 {
		t.Fatal("nenhum erro foi coletado: o teste não exercitou nada")
	}
	for i, e := range erros {
		semRastro(t, fmt.Sprintf("o erro #%d", i), e, chaveDeTeste, canarioAudio, canarioCorpo, canarioDica, "Incorrect API key")
	}
	semRastro(t, "o log", saida.String(), chaveDeTeste, canarioAudio, canarioCorpo, canarioDica, "Bearer")
	if saida.Len() != 0 {
		t.Errorf("o pacote escreveu %d bytes no log: não devia escrever nada", saida.Len())
	}
}
