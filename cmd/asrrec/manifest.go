package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// manifestFile é o mesmo formato que dataset.LoadManifest lê
// (internal/benchmark/dataset/dataset.go): um objeto com a chave
// "samples", cada uma um dataset.Sample.
type manifestFile struct {
	Samples []dataset.Sample `json:"samples"`
}

// loadManifestForRecording lê o manifesto no MESMO formato e com a MESMA
// resolução de caminho relativo que dataset.LoadManifest (relativo ao
// diretório do próprio manifesto) — mas DELIBERADAMENTE não chama
// LoadManifest.
//
// LoadManifest recusa qualquer amostra cujo arquivo de áudio ainda não
// exista no disco ("áudio inacessível", dataset.go) — correto para o
// asrbench, que só deveria rodar sobre um dataset já gravado. Mas é
// exatamente o estado inicial de QUALQUER dataset ainda sendo gravado por
// este comando: rodar asrrec contra testdata/asr-dataset/manifest.json
// pela primeira vez, hoje, veria as 46 amostras do manifesto sem nenhum
// arquivo audio/*.wav no disco ainda. Usar LoadManifest aqui faria o
// comando falhar de cara, sempre, até a última amostra estar gravada —
// o oposto do que ele existe para fazer.
//
// O restante da validação — JSON bem formado, ao menos uma amostra, ID
// derivado do índice quando ausente, ID único, referência não vazia,
// categoria conhecida (dataset.Categories), caminho de áudio não vazio —
// é o mesmo critério de LoadManifest, só sem a checagem de existência.
func loadManifestForRecording(path string) ([]dataset.Sample, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("asrrec: lendo manifesto %s: %w", path, err)
	}

	var mf manifestFile
	if err := json.Unmarshal(raw, &mf); err != nil {
		return nil, fmt.Errorf("asrrec: manifesto %s não é JSON válido: %w", path, err)
	}
	if len(mf.Samples) == 0 {
		return nil, fmt.Errorf("asrrec: manifesto %s não declara nenhuma amostra", path)
	}

	baseDir := filepath.Dir(path)
	validCategories := make(map[dataset.Category]bool, len(dataset.Categories))
	for _, c := range dataset.Categories {
		validCategories[c] = true
	}
	seenIDs := make(map[string]bool, len(mf.Samples))

	samples := make([]dataset.Sample, len(mf.Samples))
	for i, s := range mf.Samples {
		label := s.ID
		if label == "" {
			label = fmt.Sprintf("amostra[%d]", i)
			s.ID = label
		}

		if seenIDs[s.ID] {
			return nil, fmt.Errorf("asrrec: manifesto %s: ID de amostra duplicado %q", path, s.ID)
		}
		seenIDs[s.ID] = true

		if strings.TrimSpace(s.Reference) == "" {
			return nil, fmt.Errorf("asrrec: manifesto %s: %s tem referência vazia", path, label)
		}

		if !validCategories[s.Category] {
			return nil, fmt.Errorf("asrrec: manifesto %s: %s tem categoria desconhecida %q (válidas: %v)",
				path, label, s.Category, dataset.Categories)
		}

		if s.AudioPath == "" {
			return nil, fmt.Errorf("asrrec: manifesto %s: %s não declara caminho de áudio", path, label)
		}
		if !filepath.IsAbs(s.AudioPath) {
			s.AudioPath = filepath.Join(baseDir, s.AudioPath)
		}

		samples[i] = s
	}

	return samples, nil
}

// fileExists é o `exists` real usado por run() — os.Stat. pendingSamples
// (abaixo) recebe isso injetado em vez de chamar os.Stat direto, para que
// a seleção da amostra pendente seja testável sem tocar em disco nenhum.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pendingSamples devolve, na ordem do manifesto, as amostras que a sessão
// interativa deve oferecer para gravação: toda amostra cujo áudio ainda
// não existe segundo exists, MAIS a amostra cujo ID bate com redoID (se
// redoID não for vazio) — mesmo que o arquivo dela já exista. É o que
// torna -redo capaz de regravar uma amostra já aceita, e torna o comando
// retomável por construção: rodar de novo sem -redo produz exatamente as
// amostras que ainda faltam.
func pendingSamples(samples []dataset.Sample, exists func(path string) bool, redoID string) []dataset.Sample {
	var pending []dataset.Sample
	for _, s := range samples {
		if redoID != "" && s.ID == redoID {
			pending = append(pending, s)
			continue
		}
		if !exists(s.AudioPath) {
			pending = append(pending, s)
		}
	}
	return pending
}

// sampleIDExists reporta se algum elemento de samples tem este ID — usado
// por run() para recusar -redo com um ID que não existe no manifesto em
// vez de silenciosamente não fazer nada.
func sampleIDExists(samples []dataset.Sample, id string) bool {
	for _, s := range samples {
		if s.ID == id {
			return true
		}
	}
	return false
}
