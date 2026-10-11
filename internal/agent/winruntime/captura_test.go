package winruntime

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// capturaFalsa prende o Stop até ser liberado, para o teste observar o que
// acontece ENQUANTO a captura espera a cauda calar.
type capturaFalsa struct {
	entrou  chan struct{}
	liberar chan struct{}
	frames  chan audio.Frame
	vad     dictation.VADSummary
	err     error
}

func novaCapturaFalsa() *capturaFalsa {
	return &capturaFalsa{
		entrou:  make(chan struct{}, 4),
		liberar: make(chan struct{}),
		frames:  make(chan audio.Frame),
		vad:     dictation.VADSummary{HasSpeech: true, SpeechDuration: 900 * time.Millisecond},
	}
}

func (f *capturaFalsa) Frames() <-chan audio.Frame                   { return f.frames }
func (f *capturaFalsa) MarkStart(dictation.AttemptID, time.Time)     {}
func (f *capturaFalsa) Abort(dictation.AttemptID)                    {}
func (f *capturaFalsa) MicrofoneEntregando(dictation.AttemptID) bool { return true }
func (f *capturaFalsa) Stop(_ context.Context, _ dictation.AttemptID) (audio.AudioBuffer, dictation.VADSummary, error) {
	f.entrou <- struct{}{}
	<-f.liberar
	if f.err != nil {
		return audio.AudioBuffer{}, dictation.VADSummary{}, f.err
	}
	return audio.AudioBuffer{Samples: make([]int16, 16000), SampleRate: 16000}, f.vad, nil
}

func runtimeComAudio(a capturador) *Runtime {
	return &Runtime{
		audio:         a,
		pending:       make(chan dictation.Event, pendingBuffer),
		capturas:      make(chan capturaPronta, 1),
		pararCapturas: make(chan struct{}),
		log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestCaptura_NaoBloqueiaOLacoDeEventos é o que justifica a parada de
// áudio ser assíncrona.
//
// audio.Session.Stop deixou de devolver na hora em 28-set-2026: ele
// espera a fala calar quando a tecla sobe no meio de uma palavra, até
// 200 ms. Esperar isso na goroutine de Run congelaria o laço que consome
// hook, áudio e pending — o mesmo defeito que acabou de ser corrigido na
// inserção, reintroduzido por outra porta.
func TestCaptura_NaoBloqueiaOLacoDeEventos(t *testing.T) {
	a := novaCapturaFalsa()
	rt := runtimeComAudio(a)

	voltou := make(chan struct{})
	go func() {
		rt.pedirCaptura(7)
		close(voltou)
	}()

	select {
	case <-voltou:
	case <-time.After(2 * time.Second):
		t.Fatal("pedirCaptura() não voltou com o Stop preso — o laço de eventos ficaria travado")
	}
	select {
	case <-a.entrou:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop nunca foi chamado")
	}
	close(a.liberar)
}

// TestCaptura_AudioVoltaPeloCanalProprio: o buffer não pode voltar por
// rt.pending. rt.som é escrito por guardarAudio e lido por transcrever,
// ambos na goroutine de Run — entregar por um canal próprio é o que
// mantém esse campo com um dono só.
func TestCaptura_AudioVoltaPeloCanalProprio(t *testing.T) {
	a := novaCapturaFalsa()
	close(a.liberar)
	rt := runtimeComAudio(a)

	rt.pedirCaptura(9)

	select {
	case c := <-rt.capturas:
		if c.attempt != 9 {
			t.Errorf("attempt = %d, want 9", c.attempt)
		}
		if len(c.buf.Samples) == 0 {
			t.Error("buffer vazio")
		}
		// E o Event só nasce quando Run conclui — é lá que o áudio entra
		// no slot.
		ev := rt.concluirCaptura(c)
		if _, ok := ev.(dictation.AudioCapturedEvent); !ok {
			t.Fatalf("concluirCaptura devolveu %T, want AudioCapturedEvent", ev)
		}
		if !rt.som.ocupado || rt.som.attempt != 9 {
			t.Error("o áudio não entrou no slot: transcrever não teria o que transcrever")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a captura nunca chegou em rt.capturas")
	}

	select {
	case ev := <-rt.pending:
		t.Fatalf("a captura vazou para rt.pending (%#v) — rt.som passaria a ter dois donos", ev)
	default:
	}
}

// TestCaptura_GoroutinePresaNaoVazaQuandoRunSai: sem o canal de parada, a
// goroutine ficaria pendurada para sempre tentando entregar resultado
// para um Run que já acabou.
func TestCaptura_GoroutinePresaNaoVazaQuandoRunSai(t *testing.T) {
	a := novaCapturaFalsa()
	close(a.liberar)
	rt := runtimeComAudio(a)
	rt.capturas = make(chan capturaPronta) // sem buffer: a entrega bloqueia

	entregou := make(chan struct{})
	go func() {
		rt.pedirCaptura(1)
		// pedirCaptura devolve na hora; a goroutine interna é que fica
		// presa na entrega. Damos tempo a ela de chegar lá.
		time.Sleep(50 * time.Millisecond)
		close(rt.pararCapturas)
		close(entregou)
	}()

	select {
	case <-entregou:
	case <-time.After(2 * time.Second):
		t.Fatal("o fechamento de pararCapturas travou")
	}
	// Se a goroutine não tivesse saída, o -leak apareceria aqui como
	// canal com escritor eterno; o que dá para afirmar sem detector é
	// que ninguém entregou nada depois.
	select {
	case c := <-rt.capturas:
		t.Fatalf("entregou %#v depois de Run ter saído", c)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestCaptura_DuracaoDoAudioVaiAoNucleo: o núcleo dá à transcrição um prazo
// do tamanho do áudio (dictation.PrazoDeTranscricao), e quem sabe o tamanho
// é o runtime, pelas amostras. Sem a duração no evento, todo ditado voltaria
// ao prazo fixo de 10 s, e o de 37 s do incidente de 01-out-2026 perderia o
// texto de novo.
func TestCaptura_DuracaoDoAudioVaiAoNucleo(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{})
	// 48000 amostras a 16 kHz: 3 s.
	ev := rt.concluirCaptura(capturaPronta{
		attempt: 4,
		buf:     audioFalso(48000),
		vad:     dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second},
	})
	ac, ok := ev.(dictation.AudioCapturedEvent)
	if !ok {
		t.Fatalf("concluirCaptura devolveu %T, want AudioCapturedEvent", ev)
	}
	if ac.Duration != 3*time.Second {
		t.Errorf("Duration = %v, want 3s: a duração do áudio capturado, e não a da fala detectada (%v)",
			ac.Duration, time.Second)
	}
}
