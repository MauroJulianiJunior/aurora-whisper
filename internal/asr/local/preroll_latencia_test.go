//go:build asrlatencia

package local_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/local"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// TestPreRoll mediu as duas perguntas que decidiram o tamanho do
// pre-roll em 28-set-2026, e o resultado contrariou a expectativa nas
// duas.
//
//  1. SILÊNCIO NO COMEÇO CUSTA TEMPO DE ASR? Não. Medido: 0,0% sobre 5
//     amostras, delta de 1 ms na soma. A suposição era "sim,
//     proporcional" — RTF 0,1368 vezes 2 s daria ~274 ms — e está errada,
//     porque o whisper.cpp paga o encoder numa JANELA FIXA (audio_ctx):
//     um clipe de 4 s e um de 6 s custam o mesmo. O decoder custa por
//     token, e silêncio gera poucos.
//
//     Consequência: cortar o pre-roll NÃO era ganho de latência, e a
//     fatia inteira teria sido feita pelo motivo errado.
//
//  2. FALA ANTERIOR NO PRE-ROLL VAZA PARA A TRANSCRIÇÃO? Sim, inteira. Com
//     os 2 s anteriores contendo o fim de outra frase, a transcrição
//     cresceu 39 e 31 runas — texto que a pessoa não ditou agora,
//     inserido no campo. É o que de fato justificou o corte.
//
// O teste fica atrás de build tag porque precisa do whisper-server e do
// modelo em bin/, e leva ~30 s de CPU.
//
// Rodar (com bin/ montado):
//
//	go test -tags asrlatencia -run TestPreRoll -v -timeout 20m ./internal/asr/local/
func TestPreRoll(t *testing.T) {
	raiz, _ := filepath.Abs("../../..")
	bin := filepath.Join(raiz, "bin")
	caminhoModelo, err := model.Resolve(bin, model.SmallQ4_0)
	if err != nil {
		t.Skipf("modelo não encontrado em %s: %v", bin, err)
	}
	p := local.New(local.Config{
		ServerPath: filepath.Join(bin, "whisper-server.exe"),
		ModelPath:  caminhoModelo,
		Language:   "pt",
		Threads:    local.ThreadsRecomendadas(),
		AudioCtx:   750,
	}, model.SmallQ4_0.Nome)
	ctx, cancel := context.WithTimeout(context.Background(), local.DefaultReadyTimeout)
	defer cancel()
	if err := p.Warmup(ctx); err != nil {
		t.Skipf("whisper-server não subiu: %v", err)
	}
	defer p.Close()

	// Amostras aparadas: o começo já está sem silêncio, então o que eu
	// acrescentar é a única variável.
	amostras := []string{"cont-01", "cont-02", "dev-02", "nome-01", "rapida-01"}
	const repeticoes = 3

	mediana := func(ds []time.Duration) time.Duration {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		return ds[len(ds)/2]
	}
	medir := func(buf asr.AudioBuffer) (time.Duration, string) {
		var ds []time.Duration
		var texto string
		for i := 0; i < repeticoes; i++ {
			res, err := p.Transcribe(context.Background(), buf, asr.ASROptions{Language: "pt-BR"})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			ds = append(ds, res.ProcessingTime)
			texto = res.Text
		}
		return mediana(ds), texto
	}

	silencio := func(d time.Duration, taxa int) []int16 {
		return make([]int16, int(d.Seconds()*float64(taxa)))
	}

	// caudaDe devolve os ultimos d de OUTRA amostra: e o que o pre-roll
	// captura de verdade quando a pessoa dita duas vezes seguidas.
	caudaDe := func(nome string, d time.Duration) []int16 {
		caminho := filepath.Join(raiz, "testdata", "asr-dataset", "audio-aparado", nome+".wav")
		taxa, pcm, err := dataset.ReadWAV(caminho)
		if err != nil {
			t.Fatalf("cauda %s: %v", nome, err)
		}
		n := int(d.Seconds() * float64(taxa))
		if n > len(pcm) {
			n = len(pcm)
		}
		_ = taxa
		return pcm[len(pcm)-n:]
	}

	fmt.Println()
	fmt.Println("amostra      dur(s)   sem pre-roll   +0.5s      +2.0s     delta 2s")
	fmt.Println("--------------------------------------------------------------------")
	var somaSem, soma2s time.Duration
	for _, nome := range amostras {
		caminho := filepath.Join(raiz, "testdata", "asr-dataset", "audio-aparado", nome+".wav")
		if _, err := os.Stat(caminho); err != nil {
			continue
		}
		taxa, pcm, err := dataset.ReadWAV(caminho)
		if err != nil {
			t.Fatalf("%s: %v", nome, err)
		}
		base := asr.AudioBuffer{PCM: pcm, SampleRateHz: taxa}
		meio := asr.AudioBuffer{PCM: append(silencio(500*time.Millisecond, taxa), pcm...), SampleRateHz: taxa}
		cheio := asr.AudioBuffer{PCM: append(silencio(2*time.Second, taxa), pcm...), SampleRateHz: taxa}

		tSem, txtSem := medir(base)
		tMeio, _ := medir(meio)
		tCheio, txtCheio := medir(cheio)
		somaSem += tSem
		soma2s += tCheio

		fmt.Printf("%-12s %6.2f   %10s   %8s   %8s   %+8s\n",
			nome, float64(len(pcm))/float64(taxa),
			tSem.Round(time.Millisecond), tMeio.Round(time.Millisecond),
			tCheio.Round(time.Millisecond), (tCheio - tSem).Round(time.Millisecond))
		if txtSem != txtCheio {
			fmt.Printf("             TEXTO MUDOU com o silêncio:\n               sem: %q\n              2s:  %q\n", txtSem, txtCheio)
		}
	}
	fmt.Println("--------------------------------------------------------------------")

	// A pergunta que importa: fala anterior no pre-roll VAZA para a
	// transcricao?
	fmt.Println()
	fmt.Println("=== 2s de FALA anterior no pre-roll (ditados seguidos) ===")
	for _, par := range [][2]string{{"cont-01", "nome-01"}, {"dev-02", "cont-02"}} {
		alvo, anterior := par[0], par[1]
		caminho := filepath.Join(raiz, "testdata", "asr-dataset", "audio-aparado", alvo+".wav")
		taxa, pcm, err := dataset.ReadWAV(caminho)
		if err != nil {
			continue
		}
		cauda := caudaDe(anterior, 2*time.Second)
		_, txtLimpo := medir(asr.AudioBuffer{PCM: pcm, SampleRateHz: taxa})
		_, txtSujo := medir(asr.AudioBuffer{PCM: append(append([]int16(nil), cauda...), pcm...), SampleRateHz: taxa})
		fmt.Printf("\n%s com a cauda de %s na frente:\n", alvo, anterior)
		fmt.Printf("  so o alvo: %q\n", txtLimpo)
		fmt.Printf("  com cauda: %q\n", txtSujo)
		if n := len([]rune(txtSujo)) - len([]rune(txtLimpo)); n > 10 {
			fmt.Printf("  >>> VAZOU: a transcricao cresceu %d runas\n", n)
		}
	}
	fmt.Println()

	fmt.Printf("soma sem pre-roll %s | soma com 2s %s | delta %s (%.1f%%)\n",
		somaSem.Round(time.Millisecond), soma2s.Round(time.Millisecond),
		(soma2s - somaSem).Round(time.Millisecond),
		100*float64(soma2s-somaSem)/float64(somaSem))
}
