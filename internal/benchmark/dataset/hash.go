package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
)

// Hash calcula um hash determinístico do dataset inteiro — conteúdo dos
// arquivos de áudio MAIS os metadados do manifesto (referência, categoria,
// entidades) — exigido pelo relatório de benchmark
// (docs/ai-spec/19-eval-plan.md, "Relatório"). Ele é o que permite a
// alguém, lendo um relatório antigo, saber se o dataset mudou desde então,
// mesmo sem o áudio estar no git (AC-028): o hash muda se qualquer byte de
// qualquer áudio mudar, ou se referência/categoria/entidades mudarem.
//
// Determinístico independentemente da ordem de samples: ordena por ID
// antes de combinar, então embaralhar a ordem das amostras no manifesto
// não muda o hash.
//
// Hash de cada amostra = sha256(ID || 0x00 || Category || 0x00 ||
// Reference || 0x00 || Entities-join-0x00 || 0x00 || conteúdo do áudio).
// O hash final é sha256 da concatenação, em ordem de ID, dos hashes de
// amostra em hex — assim uma colisão exigiria quebrar sha256 duas vezes,
// e o cálculo não precisa manter todos os áudios em memória ao mesmo
// tempo (processa um arquivo por vez).
func Hash(samples []Sample) (string, error) {
	sorted := make([]Sample, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	final := sha256.New()
	for _, s := range sorted {
		h, err := sampleHash(s)
		if err != nil {
			return "", err
		}
		io.WriteString(final, h)
	}
	return hex.EncodeToString(final.Sum(nil)), nil
}

func sampleHash(s Sample) (string, error) {
	h := sha256.New()
	io.WriteString(h, s.ID)
	h.Write([]byte{0})
	io.WriteString(h, string(s.Category))
	h.Write([]byte{0})
	io.WriteString(h, s.Reference)
	h.Write([]byte{0})
	io.WriteString(h, strconv.Itoa(len(s.Entities)))
	for _, e := range s.Entities {
		h.Write([]byte{0})
		io.WriteString(h, e)
	}
	h.Write([]byte{0})

	f, err := os.Open(s.AudioPath)
	if err != nil {
		return "", fmt.Errorf("dataset: hash: abrindo %s (amostra %s): %w", s.AudioPath, s.ID, err)
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("dataset: hash: lendo %s (amostra %s): %w", s.AudioPath, s.ID, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
