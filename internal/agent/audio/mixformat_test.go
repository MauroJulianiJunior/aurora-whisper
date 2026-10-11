package audio

import (
	"encoding/binary"
	"strings"
	"testing"
)

// waveFormatExtensibleBytes monta os bytes EXATOS que o Windows devolve em
// IAudioClient::GetMixFormat para um WAVEFORMATEXTENSIBLE — 40 bytes, com
// a WAVEFORMATEX ocupando 18 e não 20.
//
// Montar os bytes à mão, em vez de serializar uma struct Go, é o ponto
// deste helper: é justamente a discordância entre o layout nativo e o
// layout que o Go dá a uma struct equivalente que corrompeu o dataset de
// 17-set-2026. Um helper que usasse a struct reproduziria o mesmo erro dos
// dois lados e o teste passaria com o código quebrado.
func waveFormatExtensibleBytes(channels, rate, bits int, subFormatData1 uint32) []byte {
	b := make([]byte, 40)
	binary.LittleEndian.PutUint16(b[0:], waveFormatTagExtensible)
	binary.LittleEndian.PutUint16(b[2:], uint16(channels))
	binary.LittleEndian.PutUint32(b[4:], uint32(rate))
	binary.LittleEndian.PutUint32(b[8:], uint32(rate*channels*bits/8)) // AvgBytesPerSec
	binary.LittleEndian.PutUint16(b[12:], uint16(channels*bits/8))     // BlockAlign
	binary.LittleEndian.PutUint16(b[14:], uint16(bits))
	binary.LittleEndian.PutUint16(b[16:], extensibleCbSize)
	binary.LittleEndian.PutUint16(b[18:], uint16(bits)) // wValidBitsPerSample
	binary.LittleEndian.PutUint32(b[20:], 0x3)          // dwChannelMask
	// SubFormat: {Data1-0000-0010-8000-00AA00389B71}, começando no byte 24.
	binary.LittleEndian.PutUint32(b[24:], subFormatData1)
	copy(b[28:], []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71})
	return b
}

// TestParseMixFormat_ExtensibleIEEEFloat é o teste que faltava.
//
// Este é EXATAMENTE o formato que o microfone da máquina de referência
// reporta, e foi ele que passou despercebido: o SubFormat era lido no
// offset 28 em vez de 24, IsFloat vinha false, convertPCM caía no ramo de
// PCM 32 bits e gravava o padrão de bits IEEE-754 no lugar do áudio. As 46
// gravações do dataset saíram assim, e nenhum dos 871 testes ficou
// vermelho — porque nenhum olhava para estes 40 bytes.
//
// Não precisa de Windows, não precisa de microfone, roda em milissegundos.
func TestParseMixFormat_ExtensibleIEEEFloat(t *testing.T) {
	blob := waveFormatExtensibleBytes(2, 48000, 32, waveFormatIEEEFloat)

	mf, err := parseMixFormat(blob)
	if err != nil {
		t.Fatalf("parseMixFormat: %v", err)
	}
	if !mf.IsFloat {
		t.Errorf("IsFloat = false, want true.\n"+
			"É ISTO que corrompeu o dataset: o SubFormat IEEE_FLOAT está no offset %d "+
			"(bytes % x), e lê-lo de qualquer outro offset devolve false em silêncio.",
			subFormatOffset, blob[subFormatOffset:subFormatOffset+4])
	}
	if mf.Channels != 2 || mf.SampleRate != 48000 || mf.BitsPerSample != 32 {
		t.Errorf("mf = %+v, want {Channels:2 SampleRate:48000 BitsPerSample:32}", mf)
	}
}

// TestParseMixFormat_SubFormatNaoPodeSairDoOffset24 falha se alguém
// reintroduzir uma struct Go embutida para ler o bloco. Uma struct assim
// desloca o SubFormat em 2 bytes; aqui, plantamos o GUID de float SÓ no
// offset 24 e enchemos o resto de lixo — quem ler de outro lugar não acha.
func TestParseMixFormat_SubFormatNaoPodeSairDoOffset24(t *testing.T) {
	blob := waveFormatExtensibleBytes(2, 48000, 32, waveFormatIEEEFloat)
	// Envenena os 2 bytes seguintes ao GUID: se alguém ler a partir do 28,
	// vai pegar parte deste lixo em vez do GUID legítimo.
	blob = append(blob, 0xFF, 0xFF, 0xFF, 0xFF)

	mf, err := parseMixFormat(blob)
	if err != nil || !mf.IsFloat {
		t.Fatalf("parseMixFormat = (%+v, %v); o GUID válido está no offset 24 e só ali", mf, err)
	}
}

func TestParseMixFormat_ExtensiblePCM(t *testing.T) {
	mf, err := parseMixFormat(waveFormatExtensibleBytes(1, 44100, 16, waveFormatPCM))
	if err != nil {
		t.Fatalf("parseMixFormat: %v", err)
	}
	if mf.IsFloat {
		t.Error("IsFloat = true para SubFormat PCM")
	}
	if mf.SampleRate != 44100 || mf.Channels != 1 || mf.BitsPerSample != 16 {
		t.Errorf("mf = %+v", mf)
	}
}

// TestParseMixFormat_SubFormatDesconhecidoEhErro trava a regra que
// transformou uma falha em corrupção: o subtipo que não conhecemos tem de
// PARAR a captura, nunca virar PCM por omissão.
func TestParseMixFormat_SubFormatDesconhecidoEhErro(t *testing.T) {
	casos := map[string][]byte{
		"Data1 desconhecido": waveFormatExtensibleBytes(2, 48000, 32, 0x0092),
		"sufixo KS diferente": func() []byte {
			b := waveFormatExtensibleBytes(2, 48000, 32, waveFormatIEEEFloat)
			b[30] = 0x77 // quebra o sufixo {...-0000-0010-8000-00AA00389B71}
			return b
		}(),
	}
	for nome, blob := range casos {
		t.Run(nome, func(t *testing.T) {
			mf, err := parseMixFormat(blob)
			if err == nil {
				t.Fatalf("parseMixFormat = (%+v, nil), want erro — supor PCM no desconhecido "+
					"é exatamente o que gravou 46 arquivos sem voz", mf)
			}
			if !strings.Contains(err.Error(), "supor") {
				t.Logf("erro: %v", err)
			}
		})
	}
}

// TestParseMixFormat_CbSizeMentiroso: um bloco que se declara extensible
// mas não tem espaço para o SubFormat não pode ser lido — seria leitura
// além do buffer devolvido por GetMixFormat.
func TestParseMixFormat_CbSizeMentiroso(t *testing.T) {
	blob := waveFormatExtensibleBytes(2, 48000, 32, waveFormatIEEEFloat)
	binary.LittleEndian.PutUint16(blob[offCbSize:], 0)

	if mf, err := parseMixFormat(blob); err == nil {
		t.Fatalf("parseMixFormat = (%+v, nil), want erro de cbSize", mf)
	}
}

func TestParseMixFormat_BlocoTruncado(t *testing.T) {
	blob := waveFormatExtensibleBytes(2, 48000, 32, waveFormatIEEEFloat)
	if mf, err := parseMixFormat(blob[:30]); err == nil {
		t.Fatalf("parseMixFormat(truncado) = (%+v, nil), want erro", mf)
	}
	if mf, err := parseMixFormat(blob[:10]); err == nil {
		t.Fatalf("parseMixFormat(10 bytes) = (%+v, nil), want erro", mf)
	}
}

// TestParseMixFormat_WaveFormatExSimples cobre o driver que devolve
// WAVEFORMATEX puro, de 18 bytes, sem nenhum SubFormat.
func TestParseMixFormat_WaveFormatExSimples(t *testing.T) {
	b := make([]byte, waveFormatExNativeSize)
	binary.LittleEndian.PutUint16(b[0:], waveFormatIEEEFloat)
	binary.LittleEndian.PutUint16(b[2:], 1)
	binary.LittleEndian.PutUint32(b[4:], 16000)
	binary.LittleEndian.PutUint16(b[14:], 32)

	mf, err := parseMixFormat(b)
	if err != nil {
		t.Fatalf("parseMixFormat: %v", err)
	}
	if !mf.IsFloat || mf.SampleRate != 16000 || mf.Channels != 1 {
		t.Errorf("mf = %+v", mf)
	}
}

// TestParseMixFormat_FormatoNaoSuportadoEhErro garante que o formato que
// não sabemos converter para de vez, em vez de seguir e produzir áudio
// errado.
func TestParseMixFormat_FormatoNaoSuportadoEhErro(t *testing.T) {
	blob := waveFormatExtensibleBytes(2, 48000, 8, waveFormatPCM) // PCM 8 bits
	if mf, err := parseMixFormat(blob); err == nil {
		t.Fatalf("parseMixFormat = (%+v, nil), want erro para PCM de 8 bits", mf)
	}
}
