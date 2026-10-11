package nuvem

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// seg monta um segmento do verbose_json.
func seg(inicio, fim float64, texto string, logprob, semFala float64) string {
	return fmt.Sprintf(`{"id":0,"start":%v,"end":%v,"text":%q,"avg_logprob":%v,"no_speech_prob":%v,"temperature":0}`,
		inicio, fim, texto, logprob, semFala)
}

func transcrever(t *testing.T, resposta string, modelo string) (asr.ASRResult, error) {
	t.Helper()
	srv := novoProvedorFalso(t, responder(200, resposta))
	p := providerDe(srv, Groq, modelo)
	return p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
}

// Texto vazio vira ErrSemFala (e nada mais): nenhum ditado é criado.
func TestTranscribe_TextoVazioViraSemFala(t *testing.T) {
	for nome, resposta := range map[string]string{
		"vazio":              `{"text":""}`,
		"só espaços":         `{"text":"   "}`,
		"só quebras":         `{"text":"\n\t \r\n"}`,
		"vazio com segmento": `{"text":"","segments":[` + seg(0, 1, "", -0.2, 0.01) + `]}`,
	} {
		t.Run(nome, func(t *testing.T) {
			res, err := transcrever(t, resposta, "whisper-large-v3-turbo")
			if !errors.Is(err, ErrSemFala) {
				t.Fatalf("err = %v, quero ErrSemFala", err)
			}
			var e *Erro
			if errors.As(err, &e) {
				t.Errorf("SEM_FALA virou falha do provedor: %v", e)
			}
			if !reflect.DeepEqual(res, asr.ASRResult{}) {
				t.Errorf("resultado = %+v, quero zero", res)
			}
		})
	}
}

// Só silêncio: todos os segmentos saem, e o texto vazio vira SEM_FALA.
func TestTranscribe_SoSilencioViraSemFala(t *testing.T) {
	resposta := `{"text":" Obrigado por assistir.","segments":[` +
		seg(0, 2, " Obrigado por assistir.", -1.6, 0.93) + `,` +
		seg(2, 4, " Legendas.", -1.2, 0.81) + `]}`
	_, err := transcrever(t, resposta, "whisper-large-v3-turbo")
	if !errors.Is(err, ErrSemFala) {
		t.Fatalf("err = %v, quero ErrSemFala", err)
	}
}

// O segmento de silêncio sai: no_speech_prob acima de 0,6 E avg_logprob abaixo
// de -1,0. O texto é refeito dos que ficaram, e o topo (que ainda traz o
// descartado) não vale mais.
func TestTranscribe_SegmentoDeSilencioSai(t *testing.T) {
	resposta := `{"text":" Bom dia. Obrigado por assistir. Vamos começar.","language":"portuguese","segments":[` +
		seg(0, 1.5, " Bom dia.", -0.21, 0.02) + `,` +
		seg(1.5, 3, " Obrigado por assistir.", -1.4, 0.88) + `,` +
		seg(3, 5.25, " Vamos começar.", -0.35, 0.05) + `]}`
	res, err := transcrever(t, resposta, "whisper-large-v3-turbo")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Bom dia. Vamos começar." {
		t.Errorf("Text = %q, quero o texto sem o segmento de silêncio", res.Text)
	}
	want := []asr.Span{
		{Text: "Bom dia.", Start: 0, End: 1500 * time.Millisecond},
		{Text: "Vamos começar.", Start: 3 * time.Second, End: 5250 * time.Millisecond},
	}
	if !reflect.DeepEqual(res.Spans, want) {
		t.Errorf("Spans = %+v\nquero    %+v", res.Spans, want)
	}
}

// Os limiares são estritos, e as DUAS condições valem juntas.
func TestSegmento_LimiaresDoSilencio(t *testing.T) {
	if LimiteSemFala != 0.6 || LimiteLogprob != -1.0 {
		t.Fatalf("os limiares mudaram: %v e %v (ADR-021, escolha menor 7)", LimiteSemFala, LimiteLogprob)
	}
	f := func(v float64) *float64 { return &v }
	casos := []struct {
		nome     string
		semFala  *float64
		logprob  *float64
		silencio bool
	}{
		{"os dois passam", f(0.61), f(-1.01), true},
		{"muito acima e muito abaixo", f(0.99), f(-3), true},
		{"no_speech_prob exatamente 0,6 fica", f(0.6), f(-2), false},
		{"avg_logprob exatamente -1,0 fica", f(0.99), f(-1.0), false},
		{"só o no_speech_prob passa", f(0.95), f(-0.5), false},
		{"só o avg_logprob passa", f(0.1), f(-2), false},
		{"nenhum passa", f(0.1), f(-0.2), false},
		{"sem no_speech_prob", nil, f(-2), false},
		{"sem avg_logprob", f(0.99), nil, false},
		{"sem nenhum dos dois", nil, nil, false},
	}
	for _, c := range casos {
		s := segmento{NoSpeechProb: c.semFala, AvgLogprob: c.logprob}
		if got := s.ehSilencio(); got != c.silencio {
			t.Errorf("%s: ehSilencio() = %v, quero %v", c.nome, got, c.silencio)
		}
	}

	// De ponta a ponta: o segmento que passa só numa das condições fica no
	// texto.
	resposta := `{"text":"a b c","segments":[` +
		seg(0, 1, " a", -0.5, 0.95) + `,` + // só no_speech alto
		seg(1, 2, " b", -2.0, 0.10) + `,` + // só logprob baixo
		seg(2, 3, " c", -1.0, 0.60) + `]}` // nos limites
	res, err := transcrever(t, resposta, "whisper-large-v3-turbo")
	if err != nil || res.Text != "a b c" {
		t.Errorf("= (%q, %v), quero a b c", res.Text, err)
	}
}

// Os modelos gpt-*-transcribe não devolvem segmentos: vale o texto, e só a
// medição do app (ADR-021, "Detecção de fala").
func TestTranscribe_RespostaSemSegmentos(t *testing.T) {
	resposta := `{"text":"Anota aí: reunião às 15 horas.","usage":{"type":"duration","seconds":4}}`
	srv := novoProvedorFalso(t, responder(200, resposta))
	p := providerDe(srv, OpenAI, "gpt-transcribe")
	res, err := p.Transcribe(context.Background(), bufferWebm(), asr.ASROptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Anota aí: reunião às 15 horas." {
		t.Errorf("Text = %q", res.Text)
	}
	if len(res.Spans) != 0 {
		t.Errorf("Spans = %+v, quero nenhum", res.Spans)
	}
}

// Sem nenhum segmento descartado, o texto é o do provedor, sem refazer.
func TestTranscribe_SemDescarteUsaOTextoDoProvedor(t *testing.T) {
	resposta := `{"text":" Olá,  mundo!\nTudo bem? ","segments":[` +
		seg(0, 1, " Olá,", -0.3, 0.01) + `,` + seg(1, 2, "  mundo! Tudo bem?", -0.4, 0.02) + `]}`
	res, err := transcrever(t, resposta, "whisper-large-v3-turbo")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Olá,  mundo!\nTudo bem?" {
		t.Errorf("Text = %q, quero o do provedor (só sem as bordas)", res.Text)
	}
	if len(res.Spans) != 2 {
		t.Errorf("Spans = %+v", res.Spans)
	}
}

// Sem o texto do topo, mas com segmentos: o texto sai dos segmentos.
func TestTranscribe_SoSegmentos(t *testing.T) {
	resposta := `{"segments":[` + seg(0, 1, " um", -0.3, 0.01) + `,` + seg(1, 2, " dois", -0.3, 0.01) + `]}`
	res, err := transcrever(t, resposta, "whisper-large-v3-turbo")
	if err != nil || res.Text != "um dois" {
		t.Errorf("= (%q, %v), quero um dois", res.Text, err)
	}
}

// Resposta que não se lê é resposta inesperada, nunca texto vazio nem pânico.
func TestTranscribe_RespostaIlegivel(t *testing.T) {
	for nome, corpo := range map[string]string{
		"vazia":          ``,
		"não é JSON":     `<html>erro</html>`,
		"JSON truncado":  `{"text":"ola`,
		"objeto vazio":   `{}`,
		"lista":          `[]`,
		"texto numérico": `{"text":42}`,
		"nulo":           `null`,
		"segmentos mal":  `{"text":"x","segments":"nada"}`,
	} {
		t.Run(nome, func(t *testing.T) {
			_, err := transcrever(t, corpo, "whisper-large-v3-turbo")
			var e *Erro
			if !errors.As(err, &e) || e.Motivo != MotivoRespostaInesperada {
				t.Errorf("err = %v, quero *Erro com MotivoRespostaInesperada", err)
			}
			if errors.Is(err, ErrSemFala) {
				t.Error("resposta ilegível foi tomada por SEM_FALA")
			}
		})
	}
}

// Um corpo gigante não é lido inteiro: o teto da resposta vale.
func TestTranscribe_RespostaGrandeDemais(t *testing.T) {
	corpo := `{"text":"` + strings.Repeat("a", limiteResposta+10) + `"}`
	_, err := transcrever(t, corpo, "whisper-large-v3-turbo")
	var e *Erro
	if !errors.As(err, &e) || e.Motivo != MotivoRespostaInesperada {
		t.Errorf("err = %v, quero *Erro com MotivoRespostaInesperada", err)
	}
}
