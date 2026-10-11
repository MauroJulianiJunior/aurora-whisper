package winruntime

// O prompt do ASR troca com o Agent rodando desde o "Adicionar termo"
// (ADR-020): a dica que a pessoa liga vale no próximo ditado. Estes testes
// provam as regras da troca do prompt, as mesmas do resto do dicionário (vale
// no próximo keydown, nunca no meio de um ditado), e que o ditado que já está
// no ASR segue com o prompt com que foi. Nada aqui abre áudio, digita ou toca a
// área de transferência: o ASR é um dublê.

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

func comPrompt(d Dicionario, prompt string) Dicionario {
	d.Vocabulario = &prompt
	return d
}

func TestTrocarDicionario_TrocaOPromptNoProximoKeydown(t *testing.T) {
	rt := runtimeComDicionario(t, 1)
	rt.UsarVocabulario("ECD, DCTFWeb")
	rt.TrocarDicionario(comPrompt(dicionarioDaGeracao(2), "RAG, ECD, DCTFWeb"))

	if rt.vocabulario != "ECD, DCTFWeb" {
		t.Fatalf("o prompt mudou antes do keydown: %q", rt.vocabulario)
	}
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if rt.vocabulario != "RAG, ECD, DCTFWeb" {
		t.Fatalf("depois do keydown o prompt é %q, quero o novo", rt.vocabulario)
	}
	if g := geracaoDoRuntime(t, rt); g != 2 {
		t.Fatalf("geração = %d", g)
	}
}

// Um prompt vazio é um prompt: esvazia (o dicionário ficou sem termo no
// prompt). Só o nil mantém o que valia.
func TestTrocarDicionario_PromptVazioEsvazia(t *testing.T) {
	rt := runtimeComDicionario(t, 1)
	rt.UsarVocabulario("ECD")
	rt.TrocarDicionario(comPrompt(dicionarioDaGeracao(2), ""))
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if rt.vocabulario != "" {
		t.Fatalf("prompt = %q, quero vazio", rt.vocabulario)
	}
}

// O prompt que chega com um ditado em curso espera o próximo, como o resto do
// dicionário.
func TestTrocarDicionario_PromptNoMeioDeUmDitadoEsperaOProximo(t *testing.T) {
	rt := runtimeComDicionario(t, 1)
	rt.UsarVocabulario("velho")
	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // ditado 1
	rt.TrocarDicionario(comPrompt(dicionarioDaGeracao(2), "novo"))
	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // repetição da tecla no meio
	if rt.vocabulario != "velho" {
		t.Fatalf("o prompt mudou no meio do ditado: %q", rt.vocabulario)
	}
	if !holdVenceu(t, rt, 700*time.Millisecond) || rt.machine.CurrentAttempt() != dictation.NoAttempt {
		t.Fatal("o ditado 1 não terminou")
	}
	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // ditado 2
	if rt.vocabulario != "novo" {
		t.Fatalf("o ditado 2 ficou com o prompt %q", rt.vocabulario)
	}
}

// asrQueAnotaOPrompt guarda o prompt que cada transcrição recebeu, pelo
// tamanho do áudio (que o teste faz diferente a cada ditado).
type asrQueAnotaOPrompt struct {
	mu      sync.Mutex
	prompts map[int]string
}

func (a *asrQueAnotaOPrompt) Transcribe(_ context.Context, buf asr.AudioBuffer, opts asr.ASROptions) (asr.ASRResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prompts[len(buf.PCM)] = opts.Vocabulary
	return asr.ASRResult{Text: "x", Model: "modelo-de-teste", ProcessingTime: time.Millisecond}, nil
}

// TestTranscrever_DitadoEmVooSegueComOPromptDeAntes: o prompt de um ditado é o
// que valia quando a goroutine de Run mandou transcrevê-lo. Se a goroutine do
// ASR o lesse sozinha, mais tarde, leria o prompt que o painel trocou no
// intervalo (e, com o detector de corrida, seria uma corrida com a escrita de
// aplicarDicionario).
//
// Com um processador só, a goroutine do ASR só roda quando a de Run para de
// trabalhar: exatamente depois de o prompt ser trocado. É o pior caso, e é
// determinístico. Rodar também com -race onde houver cgo.
func TestTranscrever_DitadoEmVooSegueComOPromptDeAntes(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	prov := &asrQueAnotaOPrompt{prompts: map[int]string{}}
	rt := runtimeMinimo(prov)

	const ditados = 50
	for i := 1; i <= ditados; i++ {
		rt.vocabulario = fmt.Sprintf("prompt %d", i)
		rt.guardarAudio(dictation.AttemptID(i), audioFalso(1000+i))
		rt.transcrever(dictation.TranscribeEffect{Attempt: dictation.AttemptID(i)})
		// O painel troca o prompt logo depois (na goroutine de Run, como
		// aplicarDicionario), com a transcrição ainda no ar.
		rt.vocabulario = fmt.Sprintf("trocado depois de %d", i)
	}
	for i := 0; i < ditados; i++ {
		select {
		case <-rt.pending:
		case <-time.After(5 * time.Second):
			t.Fatalf("só %d de %d transcrições voltaram", i, ditados)
		}
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	for i := 1; i <= ditados; i++ {
		if got, want := prov.prompts[1000+i], fmt.Sprintf("prompt %d", i); got != want {
			t.Errorf("o ditado %d foi ao ASR com o prompt %q, quero %q (o de quando foi mandado)", i, got, want)
		}
	}
}

// TestTrocarDicionario_PromptConcorrente: o painel troca o dicionário com o
// prompt sem parar, de várias goroutines, enquanto a de Run abre ditados e os
// manda ao ASR. Cada ditado vai ao ASR com o prompt que valia no keydown dele.
// Este ambiente não tem cgo para o detector de corrida; onde houver, rodar com
// -race -count=20.
func TestTrocarDicionario_PromptConcorrente(t *testing.T) {
	rt := runtimeComDicionario(t, 0)
	prov := &asrQueAnotaOPrompt{prompts: map[int]string{}}
	rt.asr = prov

	parar := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-parar:
					return
				default:
				}
				rt.TrocarDicionario(comPrompt(dicionarioDaGeracao(w*1_000_000+i), fmt.Sprintf("p%d-%d", w, i)))
				runtime.Gosched()
			}
		}(w)
	}
	defer func() { close(parar); wg.Wait() }()

	quer := map[int]string{}
	for rodada := 1; rodada <= 100; rodada++ {
		// Espera um pedido novo chegar: com um processador só, as goroutines do
		// painel só andam quando esta cede a vez.
		for rt.dicionario.Load() == nil {
			runtime.Gosched()
		}
		rt.handleRaw(dictation.RawKeyEvent{Down: true}) // aplica o pedido
		quer[rodada] = rt.vocabulario
		rt.guardarAudio(rt.machine.CurrentAttempt(), audioFalso(2000+rodada))
		rt.transcrever(dictation.TranscribeEffect{Attempt: rt.machine.CurrentAttempt()})
		select {
		case <-rt.pending:
		case <-time.After(5 * time.Second):
			t.Fatalf("rodada %d: a transcrição não voltou", rodada)
		}
		if !holdVenceu(t, rt, 700*time.Millisecond) || rt.machine.CurrentAttempt() != dictation.NoAttempt {
			t.Fatalf("rodada %d: o ditado não terminou", rodada)
		}
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	diferentes := map[string]bool{}
	for rodada, want := range quer {
		if got := prov.prompts[2000+rodada]; got != want {
			t.Errorf("rodada %d: o ASR recebeu %q, o prompt do keydown era %q", rodada, got, want)
		}
		diferentes[want] = true
	}
	if len(diferentes) < 10 {
		t.Fatalf("em 100 ditados só %d prompts diferentes: a troca não está acontecendo", len(diferentes))
	}
}
