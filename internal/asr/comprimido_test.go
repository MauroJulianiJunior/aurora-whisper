package asr_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

func TestComprimido_Presente(t *testing.T) {
	casos := []struct {
		nome string
		c    asr.Comprimido
		want bool
	}{
		{"valor zero é PCM", asr.Comprimido{}, false},
		{"bytes sem tipo", asr.Comprimido{Bytes: []byte{1}}, true},
		{"tipo sem bytes", asr.Comprimido{Tipo: "audio/webm"}, true},
		{"completo", asr.Comprimido{Bytes: []byte{1}, Tipo: "audio/webm"}, true},
	}
	for _, c := range casos {
		if got := c.c.Presente(); got != c.want {
			t.Errorf("%s: Presente() = %v, want %v", c.nome, got, c.want)
		}
	}
}

// PCM ou comprimido, nunca os dois (ADR-021, decisão C).
func TestAudioBuffer_Validar(t *testing.T) {
	pcm := []int16{1, 2, 3}
	webm := asr.Comprimido{Bytes: []byte{0x1a, 0x45, 0xdf, 0xa3}, Tipo: "audio/webm"}

	casos := []struct {
		nome    string
		buf     asr.AudioBuffer
		invalid bool
	}{
		{"só PCM", asr.AudioBuffer{PCM: pcm, SampleRateHz: 16000}, false},
		{"PCM vazio continua válido (quem decide é o provider)", asr.AudioBuffer{}, false},
		{"só comprimido", asr.AudioBuffer{Comprimido: webm}, false},
		{"tipo com parâmetros", asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: webm.Bytes, Tipo: "Audio/WebM; codecs=opus"}}, false},
		{"os dois", asr.AudioBuffer{PCM: pcm, SampleRateHz: 16000, Comprimido: webm}, true},
		{"comprimido sem bytes", asr.AudioBuffer{Comprimido: asr.Comprimido{Tipo: "audio/webm"}}, true},
		{"comprimido sem tipo", asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: webm.Bytes}}, true},
		{"comprimido só com parâmetros no tipo", asr.AudioBuffer{Comprimido: asr.Comprimido{Bytes: webm.Bytes, Tipo: " ;codecs=opus"}}, true},
	}
	for _, c := range casos {
		err := c.buf.Validar()
		if c.invalid {
			if !errors.Is(err, asr.ErrAudioInvalido) {
				t.Errorf("%s: Validar() = %v, want ErrAudioInvalido", c.nome, err)
			}
		} else if err != nil {
			t.Errorf("%s: Validar() = %v, want nil", c.nome, err)
		}
	}
}

// O erro não carrega o áudio: mensagem fixa.
func TestAudioBuffer_Validar_ErroNaoLevaOAudio(t *testing.T) {
	const canario = "CANARIO-DE-AUDIO-DO-VALIDAR"
	buf := asr.AudioBuffer{
		PCM:        []int16{1},
		Comprimido: asr.Comprimido{Bytes: []byte(canario), Tipo: "audio/webm"},
	}
	err := buf.Validar()
	if err == nil {
		t.Fatal("Validar() = nil")
	}
	if got := err.Error(); strings.Contains(got, canario) {
		t.Errorf("o erro traz o áudio: %q", got)
	}
}

func TestNormalizarTipo(t *testing.T) {
	casos := map[string]string{
		"audio/webm":                 "audio/webm",
		"Audio/WebM":                 "audio/webm",
		"audio/webm;codecs=opus":     "audio/webm",
		"  audio/webm ; codecs=opus": "audio/webm",
		"":                           "",
		" ; codecs=opus":             "",
	}
	for in, want := range casos {
		if got := asr.NormalizarTipo(in); got != want {
			t.Errorf("NormalizarTipo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapabilities_AceitaFormato(t *testing.T) {
	caps := asr.ASRCapabilities{Formatos: []string{"audio/webm", "Audio/Ogg"}}
	for tipo, want := range map[string]bool{
		"audio/webm":             true,
		"audio/webm;codecs=opus": true,
		"AUDIO/WEBM":             true,
		"audio/ogg":              true,
		"audio/wav":              false,
		"":                       false,
		"audio":                  false,
		"audio/webmx":            false,
	} {
		if got := caps.AceitaFormato(tipo); got != want {
			t.Errorf("AceitaFormato(%q) = %v, want %v", tipo, got, want)
		}
	}
	// Sem Formatos: só PCM, e nada comprimido é aceito.
	if (asr.ASRCapabilities{}).AceitaFormato("audio/webm") {
		t.Error("ASRCapabilities sem Formatos aceitou audio/webm")
	}
	if (asr.ASRCapabilities{Formatos: []string{""}}).AceitaFormato("") {
		t.Error("tipo vazio nunca é aceito")
	}
}

// formatoProvider é um provider que só prova o contrato: aceita o que declara
// e recusa o resto com ErrComprimidoNaoSuportado, como o local faz.
type formatoProvider struct {
	id       string
	formatos []string
	chamadas int
}

func (p *formatoProvider) ID() string { return p.id }
func (p *formatoProvider) Capabilities() asr.ASRCapabilities {
	return asr.ASRCapabilities{Formatos: p.formatos}
}
func (p *formatoProvider) Warmup(context.Context) error { return nil }
func (p *formatoProvider) Transcribe(_ context.Context, audio asr.AudioBuffer, _ asr.ASROptions) (asr.ASRResult, error) {
	p.chamadas++
	if audio.Comprimido.Presente() && !p.Capabilities().AceitaFormato(audio.Comprimido.Tipo) {
		return asr.ASRResult{}, asr.ErrComprimidoNaoSuportado
	}
	return asr.ASRResult{Text: "ok", Model: p.id}, nil
}

// O Engine não muda: um buffer comprimido atravessa a policy
// local_then_cloud como o PCM atravessa, o local o recusa, o cloud (que
// declara o formato) o aceita, e o Release roda uma vez só, no fim.
func TestEngine_AudioComprimido_AtravessaAPolicy(t *testing.T) {
	releases := 0
	buf := asr.AudioBuffer{
		Comprimido: asr.Comprimido{Bytes: []byte{1, 2, 3}, Tipo: "audio/webm"},
		Release:    func() { releases++ },
	}
	local := &formatoProvider{id: "local-so-pcm"}
	nuvem := &formatoProvider{id: "nuvem", formatos: []string{"audio/webm"}}
	e := &asr.Engine{
		Local:           local,
		Cloud:           nuvem,
		CloudCredential: asr.CloudCredential{Token: "x"},
	}

	res, err := e.Transcribe(context.Background(), asr.PolicyLocalThenCloud, buf, asr.ASROptions{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Model != "nuvem" {
		t.Errorf("Model = %q, want nuvem (o local recusa o comprimido)", res.Model)
	}
	if local.chamadas != 1 || nuvem.chamadas != 1 {
		t.Errorf("chamadas local=%d nuvem=%d, want 1 e 1", local.chamadas, nuvem.chamadas)
	}
	if releases != 1 {
		t.Errorf("Release rodou %d vezes, want 1", releases)
	}

	// Só local: o erro do formato volta, e o Release roda mesmo assim.
	releases = 0
	_, err = e.Transcribe(context.Background(), asr.PolicyLocalOnly, buf, asr.ASROptions{})
	if !errors.Is(err, asr.ErrComprimidoNaoSuportado) {
		t.Errorf("local_only com comprimido: err = %v, want ErrComprimidoNaoSuportado", err)
	}
	if releases != 1 {
		t.Errorf("Release rodou %d vezes, want 1", releases)
	}
}
