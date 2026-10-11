package dataset

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"
)

// Duration lê o cabeçalho de um arquivo WAV (RIFF/WAVE canônico) e devolve
// a duração do áudio, calculada a partir do chunk "fmt " (byte rate) e do
// tamanho do chunk "data" — sem decodificar nenhuma amostra.
//
// É matemática/parsing puro (sem chamada de plataforma), por isso este
// arquivo não tem build tag: roda e é testado em qualquer SO. RTF
// (candidate/runner.go, via harness) precisa desta duração para
// normalizar o tempo de processamento pela duração real do áudio.
//
// Formatos não suportados (WAV com chunks extension, extensible format,
// compressão) devolvem erro claro em vez de um valor inventado — fora de
// escopo deste sprint porque o dataset de voz é gravado em PCM simples.
func Duration(path string) (time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("dataset: abrindo %s: %w", path, err)
	}
	defer f.Close()

	var riffHeader [12]byte
	if _, err := io.ReadFull(f, riffHeader[:]); err != nil {
		return 0, fmt.Errorf("dataset: %s: cabeçalho RIFF incompleto: %w", path, err)
	}
	if string(riffHeader[0:4]) != "RIFF" || string(riffHeader[8:12]) != "WAVE" {
		return 0, fmt.Errorf("dataset: %s: não é um arquivo WAV (RIFF/WAVE) reconhecível", path)
	}

	var byteRate uint32
	var haveFmt bool
	var dataSize uint32
	var haveData bool

	for !haveData {
		var chunkHeader [8]byte
		n, err := f.Read(chunkHeader[:])
		if n < 8 || err != nil {
			break // fim do arquivo sem chunk "data": tratado abaixo como erro
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		switch chunkID {
		case "fmt ":
			body := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, body); err != nil {
				return 0, fmt.Errorf("dataset: %s: chunk fmt incompleto: %w", path, err)
			}
			if len(body) < 16 {
				return 0, fmt.Errorf("dataset: %s: chunk fmt menor que o mínimo PCM (16 bytes)", path)
			}
			byteRate = binary.LittleEndian.Uint32(body[8:12])
			haveFmt = true
		case "data":
			dataSize = chunkSize
			haveData = true
			// Não lê o corpo do chunk "data": só a duração importa aqui.
		default:
			// Chunk desconhecido (ex.: "LIST", "fact"): pula o corpo.
			if _, err := f.Seek(int64(chunkSize), 1); err != nil {
				return 0, fmt.Errorf("dataset: %s: pulando chunk %q: %w", path, chunkID, err)
			}
		}
		// Chunks WAV são alinhados em 2 bytes: um chunkSize ímpar tem 1
		// byte de padding depois do corpo.
		if chunkSize%2 == 1 && chunkID != "data" {
			if _, err := f.Seek(1, 1); err != nil {
				break
			}
		}
	}

	if !haveFmt {
		return 0, fmt.Errorf("dataset: %s: sem chunk fmt", path)
	}
	if !haveData {
		return 0, fmt.Errorf("dataset: %s: sem chunk data", path)
	}
	if byteRate == 0 {
		return 0, fmt.Errorf("dataset: %s: byte rate zero no chunk fmt", path)
	}

	seconds := float64(dataSize) / float64(byteRate)
	return time.Duration(seconds * float64(time.Second)), nil
}

// WriteWAV escreve samples (PCM int16 mono) em path como um arquivo WAV
// RIFF/WAVE canônico: o mesmo layout que writeTestWAV (testing_helpers_test.go,
// deste pacote), writeWAV (internal/benchmark/harness/harness_test.go) e
// writeE2EWAV (cmd/asrbench/main_test.go) já escrevem à mão nos testes —
// chunk "fmt " de 16 bytes PCM seguido do chunk "data" com as amostras
// cruas little-endian, sem chunks extras. É o formato que Duration lê de
// volta: WriteWAV seguido de Duration devolve a duração original.
//
// cmd/asrrec é o único chamador de produção: grava cada amostra do
// dataset de voz do benchmark assim que o dono do produto aceita uma
// tomada. path é sobrescrito se já existir — é o que permite a flag
// -redo regravar uma amostra específica.
func WriteWAV(path string, sampleRate int, samples []int16) (err error) {
	if sampleRate <= 0 {
		return fmt.Errorf("dataset: WriteWAV: sample rate inválido: %d", sampleRate)
	}

	const (
		numChannels   = 1
		bitsPerSample = 16
	)
	blockAlign := numChannels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	dataSize := len(samples) * blockAlign

	f, createErr := os.Create(path)
	if createErr != nil {
		return fmt.Errorf("dataset: WriteWAV: criando %s: %w", path, createErr)
	}
	// err é o retorno NOMEADO de propósito: só assim este defer consegue
	// promover um erro de Close (disco cheio no flush final, por
	// exemplo) para o valor devolvido quando o corpo da função não
	// encontrou nenhum erro antes — um defer que só reatribuísse uma
	// variável local não nomeada seria descartado silenciosamente no
	// `return` que já tivesse rodado.
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	// write escreve v e, ao primeiro erro, para de escrever qualquer
	// coisa depois — evita 14 blocos de "if err != nil" repetidos para
	// cada campo do cabeçalho, sem esconder o erro real (ele ainda é
	// devolvido no fim, com o path).
	write := func(v any) {
		if err != nil {
			return
		}
		err = binary.Write(f, binary.LittleEndian, v)
	}
	writeString := func(s string) {
		if err != nil {
			return
		}
		_, err = f.WriteString(s)
	}

	writeString("RIFF")
	write(uint32(36 + dataSize))
	writeString("WAVE")

	writeString("fmt ")
	write(uint32(16)) // tamanho do chunk fmt PCM
	write(uint16(1))  // PCM
	write(uint16(numChannels))
	write(uint32(sampleRate))
	write(uint32(byteRate))
	write(uint16(blockAlign))
	write(uint16(bitsPerSample))

	writeString("data")
	write(uint32(dataSize))
	write(samples)

	if err != nil {
		return fmt.Errorf("dataset: WriteWAV: escrevendo %s: %w", path, err)
	}
	return nil
}

// ReadWAV lê um arquivo WAV RIFF/WAVE canônico PCM mono de 16 bits — o
// mesmo formato que WriteWAV escreve — e devolve a taxa de amostragem e
// as amostras int16 do chunk "data".
//
// Só entende PCM mono de 16 bits: qualquer outra combinação (estéreo,
// float, WAVEFORMATEXTENSIBLE, outra profundidade de bits) devolve erro
// explícito em vez de decodificar errado silenciosamente — o mesmo
// princípio de Duration acima. cmd/asrfix (o primeiro chamador) só
// processa gravações do próprio dataset de voz, que WriteWAV sempre
// escreve neste formato exato.
func ReadWAV(path string) (sampleRate int, samples []int16, err error) {
	f, openErr := os.Open(path)
	if openErr != nil {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: abrindo %s: %w", path, openErr)
	}
	defer f.Close()

	var riffHeader [12]byte
	if _, err := io.ReadFull(f, riffHeader[:]); err != nil {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: cabeçalho RIFF incompleto: %w", path, err)
	}
	if string(riffHeader[0:4]) != "RIFF" || string(riffHeader[8:12]) != "WAVE" {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: não é um arquivo WAV (RIFF/WAVE) reconhecível", path)
	}

	var haveFmt, haveData bool
	var numChannels, bitsPerSample uint16
	var data []byte

	for !haveData {
		var chunkHeader [8]byte
		n, readErr := f.Read(chunkHeader[:])
		if n < 8 || readErr != nil {
			break // fim do arquivo sem chunk "data": tratado abaixo como erro
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])

		switch chunkID {
		case "fmt ":
			body := make([]byte, chunkSize)
			if _, err := io.ReadFull(f, body); err != nil {
				return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: chunk fmt incompleto: %w", path, err)
			}
			if len(body) < 16 {
				return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: chunk fmt menor que o mínimo PCM (16 bytes)", path)
			}
			audioFormat := binary.LittleEndian.Uint16(body[0:2])
			numChannels = binary.LittleEndian.Uint16(body[2:4])
			sampleRate = int(binary.LittleEndian.Uint32(body[4:8]))
			bitsPerSample = binary.LittleEndian.Uint16(body[14:16])
			if audioFormat != 1 {
				return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: audio format %d não suportado (só PCM=1)", path, audioFormat)
			}
			haveFmt = true
		case "data":
			data = make([]byte, chunkSize)
			if _, err := io.ReadFull(f, data); err != nil {
				return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: chunk data incompleto: %w", path, err)
			}
			haveData = true
		default:
			if _, err := f.Seek(int64(chunkSize), 1); err != nil {
				return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: pulando chunk %q: %w", path, chunkID, err)
			}
		}
		if chunkSize%2 == 1 && chunkID != "data" {
			if _, err := f.Seek(1, 1); err != nil {
				break
			}
		}
	}

	if !haveFmt {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: sem chunk fmt", path)
	}
	if !haveData {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: sem chunk data", path)
	}
	if numChannels != 1 {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: %d canais não suportado (só mono)", path, numChannels)
	}
	if bitsPerSample != 16 {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: %d bits por amostra não suportado (só 16)", path, bitsPerSample)
	}
	if len(data)%2 != 0 {
		return 0, nil, fmt.Errorf("dataset: ReadWAV: %s: chunk data com tamanho ímpar (%d bytes), incompatível com amostras de 16 bits", path, len(data))
	}

	samples = make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}
	return sampleRate, samples, nil
}
