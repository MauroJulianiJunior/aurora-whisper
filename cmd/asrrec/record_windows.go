//go:build windows

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// wasapiRecorder implementa recorder sobre internal/agent/audio — a MESMA
// captura WASAPI já validada contra hardware real (ADR-010,
// internal/agent/audio/session.go), reaproveitada tal como está: nenhuma
// segunda implementação de captura, só MarkStart/Frames/Stop por amostra,
// do mesmo jeito que o Windows Agent de produção (cmd/agent/main.go) usa
// por tentativa de ditado — aqui cada "tentativa" é decidida por um ENTER
// no console em vez de uma tecla.
//
// Este arquivo é o ÚNICO deste comando que abre o microfone. Nenhuma API
// de injeção de entrada (SendInput/keybd_event/mouse_event), clipboard,
// hook global ou janela aparece aqui nem em lugar nenhum de cmd/asrrec —
// a interação é exclusivamente stdin/stdout (CLAUDE.md, "Testes que agem
// no sistema real").
type wasapiRecorder struct {
	sess   *audio.Session
	levels chan float64
	nextID dictation.AttemptID
	cur    dictation.AttemptID
}

func newRecorder() recorder {
	return &wasapiRecorder{levels: make(chan float64, 32)}
}

func (r *wasapiRecorder) Open(ctx context.Context, maxRecording time.Duration) error {
	// Captura contínua, com pre-roll pequeno: ver configDeGravacao.
	sess, err := audio.NewSource().Start(ctx, configDeGravacao(maxRecording))
	if err != nil {
		return err
	}
	r.sess = sess

	// Adapta Session.Frames() (canal de audio.Frame, compartilhado pela
	// vida inteira da sessão) para o canal de float64 que o contrato
	// recorder expõe, mantendo cli.go livre de qualquer tipo deste
	// pacote. Best-effort simétrico ao de Session.pushFrame: se ninguém
	// estiver lendo r.levels no momento (entre uma amostra e a próxima),
	// o frame é descartado em vez de bloquear a goroutine de captura.
	go func() {
		for frame := range sess.Frames() {
			select {
			case r.levels <- frame.RMS:
			default:
			}
		}
	}()
	return nil
}

func (r *wasapiRecorder) BeginSample() <-chan float64 {
	r.nextID++
	r.cur = r.nextID
	r.sess.MarkStart(r.cur, time.Now())
	return r.levels
}

func (r *wasapiRecorder) EndSample() ([]int16, int, dictation.VADSummary, error) {
	buf, vad, err := r.sess.Stop(context.Background(), r.cur)
	if err != nil {
		return nil, 0, dictation.VADSummary{}, err
	}
	return buf.Samples, buf.SampleRate, vad, nil
}

// DeviceReport descreve o que o dispositivo negociou e quanto sinal chegou
// em cada canal. Usado por run() para imprimir o diagnostico de captura.
func (r *wasapiRecorder) DeviceReport() string {
	if r.sess == nil {
		return ""
	}
	rate, canais, picos := r.sess.DeviceFormat()
	if rate == 0 {
		return ""
	}
	out := fmt.Sprintf("dispositivo: %d Hz, %d canal(is)", rate, canais)
	if len(picos) > 0 {
		out += " | pico por canal:"
		for c, p := range picos {
			out += fmt.Sprintf(" ch%d=%.4f", c, p)
		}
		// Um canal mudo com outro ativo significa que a media do
		// downmix esta comendo 6 dB de sinal.
		var maior, menor float64 = 0, 1
		for _, p := range picos {
			if p > maior {
				maior = p
			}
			if p < menor {
				menor = p
			}
		}
		if len(picos) > 1 && maior > 0.01 && menor < maior*0.1 {
			out += "  <-- ATENCAO: um canal quase mudo; a media do downmix esta perdendo ~6 dB"
		}
	}
	return out
}

func (r *wasapiRecorder) Close() error {
	if r.sess == nil {
		return nil
	}
	return r.sess.Close()
}
