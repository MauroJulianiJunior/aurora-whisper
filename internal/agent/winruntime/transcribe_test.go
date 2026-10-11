package winruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// asrFalso é o dublê do provider. Permite exercitar o caminho de
// transcrição sem servidor, sem modelo e sem Windows.
type asrFalso struct {
	texto   string
	err     error
	entrou  chan struct{} // fechado quando Transcribe começa
	liberar chan struct{} // Transcribe só retorna quando isto fecha
	recebeu asr.AudioBuffer
	// processamento é o tempo que o dublê "mede". Zero reporta 1 ms,
	// que era o valor fixo antes de o histórico passar a gravá-lo.
	processamento time.Duration
}

func (f *asrFalso) Transcribe(ctx context.Context, a asr.AudioBuffer, _ asr.ASROptions) (asr.ASRResult, error) {
	f.recebeu = a
	if f.entrou != nil {
		close(f.entrou)
	}
	if f.liberar != nil {
		<-f.liberar
	}
	if f.err != nil {
		return asr.ASRResult{}, f.err
	}
	processamento := f.processamento
	if processamento == 0 {
		processamento = time.Millisecond
	}
	return asr.ASRResult{Text: f.texto, Model: "modelo-de-teste", ProcessingTime: processamento}, nil
}

// runtimeMinimo monta só o que transcrever() usa. Construir o struct
// direto, em vez de New(), evita precisar de hook, overlay e sessão de
// áudio reais — que exigiriam Windows e um microfone.
func runtimeMinimo(prov transcriber) *Runtime {
	return &Runtime{
		asr:     prov,
		pending: make(chan dictation.Event, 8),
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func audioFalso(n int) audio.AudioBuffer {
	pcm := make([]int16, n)
	for i := range pcm {
		pcm[i] = int16(i % 997)
	}
	return audio.AudioBuffer{Samples: pcm, SampleRate: 16000}
}

// TestTranscrever_NaoBloqueiaOLacoDeEventos é o teste que justifica a
// transcrição ser assíncrona, e é a razão inteira do desenho.
//
// A goroutine de Run é a mesma que consome hook.Events(). A transcrição
// medida custa p50 de 565 ms. Se transcrever() chamasse o ASR direto, as
// teclas digitadas nesse meio segundo ficariam paradas na fila — e o
// callback do hook, que tem orçamento de ~300 ms, poderia ser removido
// pelo Windows em silêncio, matando o ditado sem nenhum erro visível.
//
// O teste prende o ASR e exige que transcrever() RETORNE mesmo assim.
func TestTranscrever_NaoBloqueiaOLacoDeEventos(t *testing.T) {
	prov := &asrFalso{
		texto:   "não deveria chegar ainda",
		entrou:  make(chan struct{}),
		liberar: make(chan struct{}),
	}
	rt := runtimeMinimo(prov)
	rt.guardarAudio(7, audioFalso(16000))

	voltou := make(chan struct{})
	go func() {
		rt.transcrever(dictation.TranscribeEffect{Attempt: 7})
		close(voltou)
	}()

	select {
	case <-voltou:
	case <-time.After(2 * time.Second):
		t.Fatal("transcrever() não voltou com o ASR preso — o laço de eventos ficaria travado")
	}

	// E o ASR realmente começou, ou seja: voltar não foi por ter pulado o
	// trabalho.
	select {
	case <-prov.entrou:
	case <-time.After(2 * time.Second):
		t.Fatal("o ASR nunca foi chamado")
	}
	close(prov.liberar)
}

func TestTranscrever_ResultadoVoltaPorPending(t *testing.T) {
	prov := &asrFalso{texto: "precisa transmitir a DCTFWeb"}
	rt := runtimeMinimo(prov)
	rt.guardarAudio(3, audioFalso(8000))

	rt.transcrever(dictation.TranscribeEffect{Attempt: 3})

	select {
	case ev := <-rt.pending:
		te, ok := ev.(dictation.TranscribeResultEvent)
		if !ok {
			t.Fatalf("evento = %T, want TranscribeResultEvent", ev)
		}
		if te.Attempt != 3 {
			t.Errorf("Attempt = %d, want 3", te.Attempt)
		}
		if te.Reason != dictation.ErrorReasonNone {
			t.Errorf("Reason = %v, want None", te.Reason)
		}
		if te.Result.Text != "precisa transmitir a DCTFWeb" {
			t.Errorf("Text = %q", te.Result.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nenhum evento chegou em rt.pending")
	}
}

// TestTranscrever_NuncaFabricaConfidence: o DTO do núcleo não sabe
// representar "ausente", e o provider local não fornece confidence. Um
// valor inventado aqui alimentaria a policy de fallback com um número que
// ninguém mediu.
func TestTranscrever_NuncaFabricaConfidence(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "texto"})
	rt.guardarAudio(1, audioFalso(4000))
	rt.transcrever(dictation.TranscribeEffect{Attempt: 1})

	ev := <-rt.pending
	te := ev.(dictation.TranscribeResultEvent)
	if te.Result.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 — o provider local não fornece confidence", te.Result.Confidence)
	}
}

func TestTranscrever_FalhaDoASRViraFalhaDaTentativa(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{err: errors.New("servidor caiu")})
	rt.guardarAudio(9, audioFalso(4000))

	rt.transcrever(dictation.TranscribeEffect{Attempt: 9})

	ev := <-rt.pending
	te, ok := ev.(dictation.TranscribeResultEvent)
	if !ok {
		t.Fatalf("evento = %T", ev)
	}
	if te.Reason != dictation.ErrorReasonTranscribeFailed {
		t.Errorf("Reason = %v, want TranscribeFailed", te.Reason)
	}
	if te.Result.Text != "" {
		t.Errorf("Text = %q, want vazio — falha não pode virar texto inserido no campo", te.Result.Text)
	}
}

// TestTranscrever_SemAudioRetido_FalhaEmVezDeTranscreverSilencio.
//
// Sem áudio, mandar um buffer vazio ao ASR devolveria texto vazio, e o
// ditado apareceria como "não entendi nada" — escondendo que a captura é
// que falhou.
func TestTranscrever_SemAudioRetido_FalhaEmVezDeTranscreverSilencio(t *testing.T) {
	prov := &asrFalso{texto: "não devia ser chamado"}
	rt := runtimeMinimo(prov)
	// nenhum guardarAudio

	rt.transcrever(dictation.TranscribeEffect{Attempt: 5})

	ev := <-rt.pending
	te := ev.(dictation.TranscribeResultEvent)
	if te.Reason != dictation.ErrorReasonTranscribeFailed {
		t.Errorf("Reason = %v, want TranscribeFailed", te.Reason)
	}
	if prov.recebeu.PCM != nil {
		t.Error("o ASR foi chamado sem áudio retido")
	}
}

// TestTranscrever_AudioDeOutraTentativaNaoServe: o slot guarda uma
// tentativa só. Transcrever o áudio da tentativa anterior colocaria no
// campo o que a pessoa falou ANTES — pior que falhar.
func TestTranscrever_AudioDeOutraTentativaNaoServe(t *testing.T) {
	prov := &asrFalso{texto: "fala antiga"}
	rt := runtimeMinimo(prov)
	rt.guardarAudio(1, audioFalso(4000))

	rt.transcrever(dictation.TranscribeEffect{Attempt: 2}) // outra tentativa

	ev := <-rt.pending
	te := ev.(dictation.TranscribeResultEvent)
	if te.Reason != dictation.ErrorReasonTranscribeFailed {
		t.Errorf("Reason = %v, want TranscribeFailed", te.Reason)
	}
	if te.Result.Text == "fala antiga" {
		t.Error("transcreveu o áudio da tentativa ERRADA — isso inseriria no campo o que a pessoa falou antes")
	}
}

// TestTranscrever_LiberaOAudioDoSlot: o áudio sai do runtime assim que a
// transcrição começa. Reter amostras além do necessário contraria o
// 09-security-privacy.md, e com slot único o próximo ditado sobrescreveria
// — mas o teste trava a intenção, não o acidente.
func TestTranscrever_LiberaOAudioDoSlot(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "ok"})
	rt.guardarAudio(4, audioFalso(4000))
	if !rt.som.ocupado {
		t.Fatal("guardarAudio não ocupou o slot")
	}

	rt.transcrever(dictation.TranscribeEffect{Attempt: 4})

	if rt.som.ocupado {
		t.Error("o slot continua ocupado depois de transcrever — o áudio ficaria retido no runtime")
	}
	<-rt.pending
}

// TestGuardarAudio_SubstituiOAnterior: sem isso, um ditado abandonado
// deixaria o áudio dele retido para sempre.
func TestGuardarAudio_SubstituiOAnterior(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{})
	rt.guardarAudio(1, audioFalso(16000))
	primeiro := rt.som.buf.PCM

	rt.guardarAudio(2, audioFalso(8000))

	if rt.som.attempt != 2 {
		t.Errorf("attempt no slot = %d, want 2", rt.som.attempt)
	}
	if len(rt.som.buf.PCM) == len(primeiro) {
		t.Error("o slot ainda tem o áudio da tentativa anterior")
	}
}

// TestGuardarAudio_PassaAsAmostrasIntactas: se o PCM chegasse alterado ao
// provider, a transcrição erraria e a causa pareceria ser o modelo.
func TestGuardarAudio_PassaAsAmostrasIntactas(t *testing.T) {
	prov := &asrFalso{texto: "ok"}
	rt := runtimeMinimo(prov)
	origem := audioFalso(1000)
	rt.guardarAudio(1, origem)

	rt.transcrever(dictation.TranscribeEffect{Attempt: 1})
	<-rt.pending

	if prov.recebeu.SampleRateHz != origem.SampleRate {
		t.Errorf("sample rate = %d, want %d", prov.recebeu.SampleRateHz, origem.SampleRate)
	}
	if len(prov.recebeu.PCM) != len(origem.Samples) {
		t.Fatalf("len(PCM) = %d, want %d", len(prov.recebeu.PCM), len(origem.Samples))
	}
	for i := range origem.Samples {
		if prov.recebeu.PCM[i] != origem.Samples[i] {
			t.Fatalf("amostra %d = %d, want %d", i, prov.recebeu.PCM[i], origem.Samples[i])
		}
	}
}

// TestTranscrever_LogDizADuracaoDoAudio: a linha da transcrição traz a
// duração do áudio ao lado do tempo do ASR. No incidente de 01-out-2026 foi
// preciso casar o agent.log com o whisper-server.log para saber de que
// tamanho era cada ditado.
func TestTranscrever_LogDizADuracaoDoAudio(t *testing.T) {
	log := &escritaSegura{}
	rt := runtimeMinimo(&asrFalso{texto: "ok", processamento: 700 * time.Millisecond})
	rt.log = slog.New(slog.NewTextHandler(log, nil))
	rt.guardarAudio(2, audioFalso(48000)) // 3 s

	rt.transcrever(dictation.TranscribeEffect{Attempt: 2})
	<-rt.pending

	l := log.String()
	if !strings.Contains(l, "transcrição concluída") || !strings.Contains(l, "audio=3s") || !strings.Contains(l, "duracao=700ms") {
		t.Errorf("a linha da transcrição não traz a duração do áudio e o tempo do ASR:\n%s", l)
	}
}
