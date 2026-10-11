package audio

import (
	"encoding/binary"
	"fmt"
)

// Este arquivo decodifica o mix format que o WASAPI devolve, e fica
// DELIBERADAMENTE fora de //go:build windows.
//
// A versão anterior fazia essa decodificação dentro de wasapi_windows.go,
// com uma struct Go que embutia waveFormatEx. O Go alinha essa struct em
// 20 bytes; a WAVEFORMATEX nativa tem 18. O SubFormat passou a ser lido no
// offset 28 em vez de 24, isFloat virou false para todo dispositivo
// float32, e a captura gravou o padrão de bits IEEE-754 de cada amostra no
// lugar do áudio — sem erro, sem log, sem teste vermelho. Foi assim que as
// 46 gravações do dataset de benchmark saíram sem voz.
//
// O trecho que quebrou era o único do caminho de áudio sem cobertura, e
// não por acaso: estava atrás de uma build tag, onde nenhum teste de
// `go test ./...` no Linux alcança. Movê-lo para cá é a correção
// estrutural; a correção do offset, sozinha, deixaria a próxima armadilha
// no mesmo lugar.
//
// Nada aqui chama Windows: é parsing de bytes. Roda e é testado em
// qualquer SO.

// wFormatTag da WAVEFORMATEX.
const (
	waveFormatPCM           = 0x0001
	waveFormatIEEEFloat     = 0x0003
	waveFormatTagExtensible = 0xFFFE
)

// Offsets e tamanhos do layout NATIVO (mmreg.h). Escritos como constantes
// explícitas justamente para não depender de regra de alinhamento do Go.
const (
	offFormatTag     = 0
	offChannels      = 2
	offSamplesPerSec = 4
	offBitsPerSample = 14
	offCbSize        = 16

	// waveFormatExNativeSize é o tamanho da WAVEFORMATEX em C: 18 bytes,
	// não os 20 que uma struct Go equivalente ocupa.
	waveFormatExNativeSize = 18

	// subFormatOffset é onde o GUID SubFormat começa em
	// WAVEFORMATEXTENSIBLE: 18 (WAVEFORMATEX) + 2 (Samples) + 4
	// (ChannelMask).
	subFormatOffset = 24

	// extensibleCbSize é o cbSize obrigatório de WAVEFORMATEXTENSIBLE:
	// 2 + 4 + 16.
	extensibleCbSize = 22

	guidSize = 16
)

// mixFormat é o que o resto do pacote precisa saber sobre o formato
// negociado.
type mixFormat struct {
	Channels      int
	SampleRate    int
	BitsPerSample int
	IsFloat       bool
}

// ksDataFormatSuffix é o sufixo compartilhado por toda a família
// KSDATAFORMAT_SUBTYPE_*: {xxxxxxxx-0000-0010-8000-00AA00389B71}. Só os
// 4 primeiros bytes (Data1) distinguem PCM de float.
var ksDataFormatSuffix = [12]byte{
	0x00, 0x00, // Data2
	0x10, 0x00, // Data3
	0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71, // Data4
}

// classifySubFormat decide se o subtipo é float ou PCM inteiro, e se é um
// subtipo que sequer reconhecemos.
//
// known=false existe para que o desconhecido vire ERRO em vez de virar PCM
// por omissão. A versão anterior só perguntava "Data1 == 3?" e respondia
// false para qualquer outra coisa — inclusive para um GUID lido do offset
// errado. "Não é float" virava "então é PCM", e seguia em frente. Esse
// "seguia em frente" é o defeito inteiro.
func classifySubFormat(guid []byte) (isFloat, known bool) {
	if len(guid) < guidSize {
		return false, false
	}
	var suffix [12]byte
	copy(suffix[:], guid[4:16])
	if suffix != ksDataFormatSuffix {
		return false, false
	}
	switch binary.LittleEndian.Uint32(guid[0:4]) {
	case waveFormatIEEEFloat:
		return true, true
	case waveFormatPCM:
		return false, true
	default:
		return false, false
	}
}

// parseMixFormat decodifica o bloco devolvido por IAudioClient::GetMixFormat.
//
// Recusa em vez de supor: bloco curto, cbSize incoerente ou SubFormat
// desconhecido devolvem erro. Um formato que não sabemos ler tem de
// impedir a captura de começar, nunca produzir áudio silenciosamente
// errado.
func parseMixFormat(blob []byte) (mixFormat, error) {
	if len(blob) < waveFormatExNativeSize {
		return mixFormat{}, fmt.Errorf("audio: mix format tem %d bytes, mínimo é %d", len(blob), waveFormatExNativeSize)
	}

	mf := mixFormat{
		Channels:      int(binary.LittleEndian.Uint16(blob[offChannels:])),
		SampleRate:    int(binary.LittleEndian.Uint32(blob[offSamplesPerSec:])),
		BitsPerSample: int(binary.LittleEndian.Uint16(blob[offBitsPerSample:])),
	}
	tag := binary.LittleEndian.Uint16(blob[offFormatTag:])
	cbSize := int(binary.LittleEndian.Uint16(blob[offCbSize:]))

	switch tag {
	case waveFormatIEEEFloat:
		mf.IsFloat = true
	case waveFormatPCM:
		mf.IsFloat = false
	case waveFormatTagExtensible:
		if cbSize < extensibleCbSize {
			return mixFormat{}, fmt.Errorf(
				"audio: formato diz WAVEFORMATEXTENSIBLE mas cbSize=%d (esperado >= %d); "+
					"ler o SubFormat passaria do fim do bloco", cbSize, extensibleCbSize)
		}
		if len(blob) < subFormatOffset+guidSize {
			return mixFormat{}, fmt.Errorf(
				"audio: WAVEFORMATEXTENSIBLE truncado: %d bytes, precisa de %d",
				len(blob), subFormatOffset+guidSize)
		}
		isFloat, known := classifySubFormat(blob[subFormatOffset : subFormatOffset+guidSize])
		if !known {
			return mixFormat{}, fmt.Errorf(
				"audio: SubFormat não reconhecido (% x); recusando em vez de supor PCM",
				blob[subFormatOffset:subFormatOffset+guidSize])
		}
		mf.IsFloat = isFloat
	default:
		return mixFormat{}, fmt.Errorf("audio: wFormatTag 0x%04X não suportado", tag)
	}

	if mf.Channels < 1 {
		return mixFormat{}, fmt.Errorf("audio: mix format com %d canais", mf.Channels)
	}
	if mf.SampleRate < 1 {
		return mixFormat{}, fmt.Errorf("audio: mix format com taxa de %d Hz", mf.SampleRate)
	}
	if !formatSupported(mf.IsFloat, mf.BitsPerSample) {
		return mixFormat{}, fmt.Errorf(
			"audio: mix format não suportado (bitsPerSample=%d, float=%v); suportados: float32, PCM 16/24/32",
			mf.BitsPerSample, mf.IsFloat)
	}
	return mf, nil
}
