package main

import "encoding/binary"

// Parâmetros do PCM16 mono 16 kHz que o Windows Agent grava (ver
// internal/benchmark/dataset/wav.go, que lê/escreve exatamente este
// formato). transportbench não depende desse pacote — a lógica de
// cabeçalho é pequena o bastante para ficar autocontida aqui, como
// "lógica pura, sem build tag" testável em qualquer SO.
const (
	wavHeaderBytes   = 44
	wavSampleRate    = 16000
	wavChannels      = 1
	wavBitsPerSample = 16
)

// wavPCM16MonoBytes calcula o tamanho total em bytes (cabeçalho WAV
// canônico de 44 bytes + dados PCM) de um áudio de duração seconds, no
// formato PCM16 mono 16 kHz. É o caminho inverso de
// internal/benchmark/dataset.Duration (que vai de tamanho para duração):
// aqui vai de duração para tamanho, para derivar os três tamanhos do
// benchmark de transporte a partir de segundos de fala reais, não de
// números escolhidos a dedo.
//
//	wavPCM16MonoBytes(1)  == 32044
//	wavPCM16MonoBytes(3)  == 96044
//	wavPCM16MonoBytes(10) == 320044
func wavPCM16MonoBytes(seconds float64) int {
	bytesPerSecond := wavSampleRate * wavChannels * wavBitsPerSample / 8
	return wavHeaderBytes + int(seconds*float64(bytesPerSecond))
}

// buildWAVPayload monta, em memória, um arquivo WAV PCM16 mono 16 kHz de
// duração seconds: cabeçalho RIFF/WAVE canônico (o mesmo layout de
// internal/benchmark/dataset.WriteWAV) seguido de dados com um padrão
// determinístico não-constante.
//
// O padrão importa: um payload todo-zero deixaria passar um transporte
// que trunca ou zera o final sem que o teste de round-trip byte a byte
// percebesse (o zero "esperado" seria indistinguível do zero "corrompido
// por bug"). Um padrão que varia byte a byte não tem esse ponto cego.
func buildWAVPayload(seconds float64) []byte {
	total := wavPCM16MonoBytes(seconds)
	dataSize := total - wavHeaderBytes
	buf := make([]byte, total)

	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")

	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // tamanho do chunk fmt PCM
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // audio format PCM
	binary.LittleEndian.PutUint16(buf[22:24], wavChannels)
	binary.LittleEndian.PutUint32(buf[24:28], wavSampleRate)
	byteRate := wavSampleRate * wavChannels * wavBitsPerSample / 8
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	blockAlign := wavChannels * wavBitsPerSample / 8
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], wavBitsPerSample)

	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))

	// Padrão pseudo-aleatório determinístico (mesma seed sempre — não
	// precisa ser criptograficamente aleatório, só variar byte a byte).
	for i := wavHeaderBytes; i < total; i++ {
		buf[i] = byte((i*2654435761 + 1) >> 3)
	}

	return buf
}
