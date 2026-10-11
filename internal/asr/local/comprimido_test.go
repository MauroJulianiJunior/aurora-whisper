package local

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// O provider local só fala PCM (ADR-021, decisão C): não declara nenhum
// formato comprimido e recusa o comprimido com o erro próprio.
func TestCapabilities_SemFormatosComprimidos(t *testing.T) {
	p := providerCom(t, &procFalso{})
	if fs := p.Capabilities().Formatos; len(fs) != 0 {
		t.Errorf("Formatos = %v, want vazio (o local só entende PCM)", fs)
	}
	if p.Capabilities().AceitaFormato("audio/webm") {
		t.Error("o provider local declarou aceitar audio/webm")
	}
}

func TestTranscribe_RecusaAudioComprimido(t *testing.T) {
	srv := servidorFalso(t, "não devia chegar aqui")
	proc := &procFalso{baseURL: srv.URL}
	p := providerCom(t, proc)

	liberado := 0
	audio := asr.AudioBuffer{
		Comprimido: asr.Comprimido{Bytes: []byte{0x1a, 0x45, 0xdf, 0xa3, 1, 2, 3}, Tipo: "audio/webm"},
		Release:    func() { liberado++ },
	}
	res, err := p.Transcribe(context.Background(), audio, asr.ASROptions{})
	if !errors.Is(err, asr.ErrComprimidoNaoSuportado) {
		t.Fatalf("Transcribe = (%+v, %v), want asr.ErrComprimidoNaoSuportado", res, err)
	}
	if res.Text != "" {
		t.Errorf("Text = %q, want vazio", res.Text)
	}
	// A recusa vem ANTES de subir o servidor: o modelo não é carregado para
	// dizer que o formato está errado.
	if got := atomic.LoadInt32(&proc.starts); got != 0 {
		t.Errorf("o servidor subiu %d vez(es) para recusar um formato", got)
	}
	// O provider nunca libera o buffer: quem libera é o dono (asr.AudioBuffer).
	if liberado != 0 {
		t.Errorf("Release foi chamado %d vez(es) pelo provider", liberado)
	}
}

// O comprimido é recusado também quando vem sem tipo ou sem bytes, e mesmo
// junto de PCM: o local nunca toma o comprimido por PCM vazio.
func TestTranscribe_RecusaComprimidoIncompleto(t *testing.T) {
	proc := &procFalso{}
	p := providerCom(t, proc)
	for nome, audio := range map[string]asr.AudioBuffer{
		"só o tipo":         {Comprimido: asr.Comprimido{Tipo: "audio/webm"}},
		"só os bytes":       {Comprimido: asr.Comprimido{Bytes: []byte{1}}},
		"PCM e webm":        {PCM: []int16{1, 2}, SampleRateHz: 16000, Comprimido: asr.Comprimido{Bytes: []byte{1}, Tipo: "audio/webm"}},
		"tipo desconhecido": {Comprimido: asr.Comprimido{Bytes: []byte{1}, Tipo: "audio/x-nada"}},
	} {
		if _, err := p.Transcribe(context.Background(), audio, asr.ASROptions{}); !errors.Is(err, asr.ErrComprimidoNaoSuportado) {
			t.Errorf("%s: err = %v, want asr.ErrComprimidoNaoSuportado", nome, err)
		}
	}
	if got := atomic.LoadInt32(&proc.starts); got != 0 {
		t.Errorf("o servidor subiu %d vez(es)", got)
	}
}

// O PCM continua passando como antes (o Agent não muda).
func TestTranscribe_PCMContinuaFuncionando(t *testing.T) {
	srv := servidorFalso(t, "segue igual")
	p := providerCom(t, &procFalso{baseURL: srv.URL})
	res, err := p.Transcribe(context.Background(), audioDeTeste(8000), asr.ASROptions{})
	if err != nil || res.Text != "segue igual" {
		t.Fatalf("Transcribe = (%+v, %v)", res, err)
	}
}
