//go:build caudareal

package audio

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// TestCaudaReal confere as DUAS metades da promessa da margem de cauda
// contra as 46 gravações reais do dataset, em vez de contra tom
// sintético.
//
// A margem promete custar ZERO para quem termina de falar antes de
// soltar, e disparar para quem se corta no meio da palavra. As duas
// metades são medíveis aqui, e a segunda é a que quase passou
// despercebida.
//
// # O proxy que enganou
//
// A primeira versão deste teste usou audio-aparado como "fala até o
// fim", e deu 0/46 — o que sugeria que a margem NUNCA dispararia. Era o
// proxy, não a margem: cmd/asrtrim corta justamente onde a energia cai,
// então a cauda de um arquivo aparado é sub-limiar por construção.
//
// O caso real de quem solta a tecla cedo é o corte no MEIO da palavra,
// com energia alta — e aí a margem dispara em 74% a 93% das gravações.
//
// Rodar:
//
//	go test -tags caudareal -run TestCaudaReal -v ./internal/agent/audio/
func TestCaudaReal(t *testing.T) {
	raiz, _ := filepath.Abs("../../..")
	janela := int64(JanelaDeCauda.Seconds() * float64(SampleRate))

	temFalaNaCauda := func(pcm []int16) bool {
		if int64(len(pcm)) < janela {
			return false
		}
		return ClassifyVAD(pcm[int64(len(pcm))-janela:], SampleRate, VADConfig{}).HasSpeech
	}

	ler := func(dir string) [][]int16 {
		padrao := filepath.Join(raiz, "testdata", "asr-dataset", dir, "*.wav")
		arquivos, _ := filepath.Glob(padrao)
		var out [][]int16
		for _, f := range arquivos {
			if _, pcm, err := dataset.ReadWAV(f); err == nil {
				out = append(out, pcm)
			}
		}
		return out
	}

	gravacoes := ler("audio-normalizado")
	if len(gravacoes) == 0 {
		t.Skip("dataset não encontrado")
	}

	contar := func(corte float64) (int, int) {
		n := 0
		for _, pcm := range gravacoes {
			fim := len(pcm)
			if corte > 0 {
				fim = int(float64(fim) * corte)
			}
			if temFalaNaCauda(pcm[:fim]) {
				n++
			}
		}
		return n, len(gravacoes)
	}

	fmt.Println()
	fmt.Println("=== quem TERMINA de falar antes de soltar (custo tem de ser zero) ===")
	comFala, total := contar(0)
	fmt.Printf("  fim natural: %d/%d disparam (%.0f%%)\n", comFala, total, 100*float64(comFala)/float64(total))
	if comFala > total/10 {
		t.Errorf("a margem dispara em %d de %d gravações que terminaram normalmente — "+
			"virou post-roll fixo, e todo ditado passa a pagar latência que não usa", comFala, total)
	}

	fmt.Println()
	fmt.Println("=== quem se CORTA no meio da palavra (a margem tem de disparar) ===")
	piorCaso := 100.0
	for _, corte := range []float64{0.5, 0.6, 0.7, 0.8} {
		comFala, total := contar(corte)
		pct := 100 * float64(comFala) / float64(total)
		if pct < piorCaso {
			piorCaso = pct
		}
		fmt.Printf("  corte em %.0f%% da fala: %d/%d disparam (%.0f%%)\n", 100*corte, comFala, total, pct)
	}
	if piorCaso < 50 {
		t.Errorf("no pior recorte a margem só dispara em %.0f%% dos cortes — "+
			"ela não estaria salvando o caso para o qual existe", piorCaso)
	}

	// Os que não disparam são cortes que caíram numa pausa entre palavras
	// ou num fonema de baixa energia. Esses não são salvos, e isso é
	// limitação declarada, não defeito: um limiar baixo o bastante para
	// pegá-los pegaria também a respiração de quem terminou.
	fmt.Println()
}
