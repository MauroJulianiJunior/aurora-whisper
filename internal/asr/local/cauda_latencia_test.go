//go:build asrlatencia

package local_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/local"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// TestCaudaCortada mede quanto custa soltar a tecla cedo, e existe
// porque a proteção do ditado é ASSIMÉTRICA.
//
// O começo tem 300 ms de pre-roll, justificados pelo AC-003. O fim não
// tem nada: Stop lê o ring buffer até o cursor do instante em que roda
// (session.go), então o áudio acaba exatamente quando a tecla sobe.
//
// Medido em 28-set-2026 sobre 6 amostras, cortando o FIM:
//
//	-100 ms  1 de 6 muda (e só ganha uma vírgula)
//	-200 ms  3 de 6 mudam: "vem em sexta" -> "vem em 6º"
//	-400 ms  3 de 6, com palavra truncada: "vem se ser"
//
// Mauro produziu o caso real testando: soltou a tecla um pouco antes em
// "Faça a simulação do fator R" e recebeu "fator E". Palavra final de
// uma letra é o pior caso, e é comum no domínio dele (fator R, Anexo I,
// DAS).
//
// O pre-roll dá 300 ms de margem ao COMEÇO da fala. O fim não tem
// margem nenhuma: Stop lê o buffer até o cursor do instante em que roda.
// Este teste corta os últimos N ms de amostras reais e compara a
// transcrição com a íntegra.
func TestCaudaCortada(t *testing.T) {
	raiz, _ := filepath.Abs("../../..")
	bin := filepath.Join(raiz, "bin")
	caminhoModelo, err := model.Resolve(bin, model.SmallQ4_0)
	if err != nil {
		t.Skipf("modelo: %v", err)
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
		t.Skipf("servidor: %v", err)
	}
	defer p.Close()

	texto := func(pcm []int16, taxa int) string {
		res, err := p.Transcribe(context.Background(), asr.AudioBuffer{PCM: pcm, SampleRateHz: taxa},
			asr.ASROptions{Language: "pt-BR"})
		if err != nil {
			t.Fatalf("Transcribe: %v", err)
		}
		return res.Text
	}

	cortes := []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	amostras := []string{"cont-01", "cont-02", "dev-02", "nome-01", "rapida-01", "cs-04"}

	fmt.Println()
	for _, nome := range amostras {
		caminho := filepath.Join(raiz, "testdata", "asr-dataset", "audio-aparado", nome+".wav")
		taxa, pcm, err := dataset.ReadWAV(caminho)
		if err != nil {
			continue
		}
		fmt.Printf("=== %s ===\n", nome)
		var integra string
		for _, corte := range cortes {
			n := len(pcm) - int(corte.Seconds()*float64(taxa))
			if n < taxa/2 {
				continue
			}
			got := texto(pcm[:n], taxa)
			marca := ""
			if corte == 0 {
				integra = got
			} else if got != integra {
				marca = "   <<< MUDOU"
			}
			fmt.Printf("  -%-5s %q%s\n", corte, got, marca)
		}
		fmt.Println()
	}
}
