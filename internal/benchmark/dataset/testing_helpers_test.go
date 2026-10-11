package dataset

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeTestWAV escreve um WAV PCM16 mono canônico e minúsculo com
// numSamples amostras de silêncio em sampleRateHz, e devolve o caminho.
// Serve só para os testes deste pacote — nenhum arquivo de áudio real é
// versionado (AC-028), e t.TempDir() garante que nada disso sobrevive ao
// teste.
func writeTestWAV(t *testing.T, dir, name string, sampleRateHz, numSamples int) string {
	t.Helper()

	const (
		numChannels   = 1
		bitsPerSample = 16
	)
	blockAlign := numChannels * bitsPerSample / 8
	byteRate := sampleRateHz * blockAlign
	dataSize := numSamples * blockAlign

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("criando WAV de teste: %v", err)
	}
	defer f.Close()

	write := func(v any) {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatalf("escrevendo WAV de teste: %v", err)
		}
	}

	f.WriteString("RIFF")
	write(uint32(36 + dataSize))
	f.WriteString("WAVE")

	f.WriteString("fmt ")
	write(uint32(16)) // tamanho do chunk fmt PCM
	write(uint16(1))  // PCM
	write(uint16(numChannels))
	write(uint32(sampleRateHz))
	write(uint32(byteRate))
	write(uint16(blockAlign))
	write(uint16(bitsPerSample))

	f.WriteString("data")
	write(uint32(dataSize))
	if dataSize > 0 {
		if _, err := f.Write(make([]byte, dataSize)); err != nil {
			t.Fatalf("escrevendo dados PCM de teste: %v", err)
		}
	}

	return path
}
