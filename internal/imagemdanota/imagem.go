// Package imagemdanota confere e limpa a imagem de uma nota (ADR-022, "As
// imagens (N10)"): o painel reduz a foto no navegador (no máximo 2.000 px no
// lado maior, em JPEG) e manda só essa cópia; quem guarda (o Agent no PC, e o
// servidor na nuvem, na segunda metade da N10) passa os bytes por Conferir antes
// de gravar.
//
// Conferir aceita só JPEG, dentro do tamanho e do lado, que decodifica inteiro, e
// devolve os bytes SEM os segmentos de metadados (APP1 a APP15 e COM: o EXIF, com
// a localização da foto, o XMP, o perfil de cor, o comentário) e sem nada depois
// do fim da imagem (o vídeo que a "foto em movimento" do celular pendura ali). A
// imagem não é recomprimida: os dados comprimidos saem como entraram, byte a byte.
//
// Só a biblioteca padrão (image/jpeg), para o servidor reusar sem dependência
// nova. Nada toca disco, rede nem log: quem chama loga só o tamanho e o id, nunca
// os bytes nem o nome do arquivo.
package imagemdanota

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
)

// Os limites das imagens das notas. Valem no PC e na nuvem: são uma constante só,
// aqui, para os dois lados recusarem a mesma coisa.
const (
	// MaxBytes é o teto de bytes da imagem JÁ REDUZIDA. Uma foto de 2.000 px em
	// JPEG fica entre 200 e 600 KB (a prévia da N10); 4 MB só barra o que não veio
	// da redução do painel. O original de até 10 MB é conferido no navegador, que
	// nunca o manda.
	MaxBytes = 4 << 20
	// MaxLado é o lado maior, em pixels, da cópia reduzida.
	MaxLado = 2000
	// MaxPorNota é quantas imagens uma nota pode ter (decisão do Mauro,
	// 28-out-2026).
	MaxPorNota = 10
	// TetoDoTotal é o teto do total de imagens da conta, somadas as de todas as
	// notas (as da lixeira também: elas voltam com a nota). Decisão do Mauro em
	// 28-out-2026: 400 MB. Por quê: na nuvem as imagens ficam no banco, e a
	// restauração abre o dump decifrado em /run/user/<uid> da VPS
	// (deploy/scripts/restaurar.sh), um tmpfs de 795 MB; as imagens quase não
	// comprimem, e com 400 MB delas o dump fica em uns 450 MB, pouco mais da
	// metade, com folga para o resto do banco crescer. O PC usa o mesmo número,
	// para não guardar o que a nuvem recusaria.
	TetoDoTotal = 400 << 20

	// maxVarreduras é quantas varreduras (SOS) o JPEG pode ter. O canvas do
	// navegador grava uma (o JPEG comum); o progressivo, umas dez. Muitas mais são
	// trabalho de propósito para o decodificador.
	maxVarreduras = 100
)

// Os erros de Conferir. Quem chama compara com errors.Is.
var (
	// ErrNaoEJPEG: os bytes não começam como um JPEG.
	ErrNaoEJPEG = errors.New("imagemdanota: não é um JPEG")
	// ErrGrandeDemais: mais de MaxBytes.
	ErrGrandeDemais = errors.New("imagemdanota: a imagem passa do tamanho máximo")
	// ErrLadoGrande: um lado passa de MaxLado (ou é zero).
	ErrLadoGrande = errors.New("imagemdanota: a imagem passa de 2.000 px no lado maior")
	// ErrInvalida: o JPEG não decodifica, tem estrutura que não se reconhece, ou
	// guarda as cores de um jeito que só os metadados explicam (CMYK, o RGB da
	// Adobe): tirar os metadados mudaria as cores.
	ErrInvalida = errors.New("imagemdanota: o JPEG não se abre")
)

// Limpa é a imagem conferida: os bytes sem os metadados, o tamanho em pixels e o
// SHA-256 dos bytes (em hexadecimal, minúsculo), que acompanha a imagem para o
// sync conferir o que chegou.
type Limpa struct {
	Bytes   []byte
	Largura int
	Altura  int
	SHA256  string
}

// TetoEmMB é o TetoDoTotal em MB, como as frases o mostram.
func TetoEmMB() int { return TetoDoTotal >> 20 }

// FraseDoTeto é o aviso de quando o total de imagens chegou ao teto (a cena 7 da
// prévia), com o número da constante.
func FraseDoTeto() string {
	return fmt.Sprintf("As imagens das notas chegaram ao limite de %d MB. Apague algumas para anexar outras.", TetoEmMB())
}

// Conferir confere b e devolve a imagem limpa. A ordem importa: o tamanho dos
// bytes, depois o cabeçalho (image/jpeg.DecodeConfig, que lê só até o tamanho da
// imagem e recusa a "bomba" de descompressão antes de qualquer alocação grande),
// depois a estrutura dos segmentos (sem os metadados), e só então a decodificação
// inteira, da original e da limpa.
func Conferir(b []byte) (Limpa, error) {
	if len(b) > MaxBytes {
		return Limpa{}, ErrGrandeDemais
	}
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 || b[2] != 0xFF {
		return Limpa{}, ErrNaoEJPEG
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return Limpa{}, ErrInvalida
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxLado || cfg.Height > MaxLado {
		return Limpa{}, ErrLadoGrande
	}
	limpo, err := semMetadados(b)
	if err != nil {
		return Limpa{}, err
	}
	// As duas decodificações: a original diz que as cores não dependem dos
	// metadados (só YCbCr e cinza, o que o canvas do navegador grava), e a limpa,
	// que o que vai ser guardado abre inteiro, do mesmo tamanho e do mesmo tipo.
	antes, err := decodificar(b)
	if err != nil {
		return Limpa{}, err
	}
	depois, err := decodificar(limpo)
	if err != nil {
		return Limpa{}, err
	}
	if fmt.Sprintf("%T", antes) != fmt.Sprintf("%T", depois) || antes.Bounds() != depois.Bounds() {
		return Limpa{}, ErrInvalida
	}
	if d := depois.Bounds(); d.Dx() != cfg.Width || d.Dy() != cfg.Height {
		return Limpa{}, ErrInvalida
	}
	soma := sha256.Sum256(limpo)
	return Limpa{Bytes: limpo, Largura: cfg.Width, Altura: cfg.Height, SHA256: hex.EncodeToString(soma[:])}, nil
}

// decodificar abre o JPEG inteiro e aceita só as cores que não dependem de
// metadados: YCbCr (a foto comum) e cinza.
func decodificar(b []byte) (image.Image, error) {
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, ErrInvalida
	}
	switch img.(type) {
	case *image.YCbCr, *image.Gray:
		return img, nil
	}
	return nil, ErrInvalida
}

// semMetadados percorre os segmentos do JPEG e devolve uma cópia sem APP1 a
// APP15 e sem COM, e sem nada depois do EOI. Os dados comprimidos de cada
// varredura (depois do SOS) são copiados sem mudança, até o próximo marcador que
// não seja o "FF 00" de dentro dos dados nem um RSTn. O resto (APP0, as tabelas,
// o SOF, o DRI, o SOS) fica como estava.
//
// É estrito de propósito: o que o canvas do navegador grava é sempre bem
// formado, e um byte solto entre segmentos é recusado (o decodificador do Go o
// pularia calado).
func semMetadados(b []byte) ([]byte, error) {
	out := make([]byte, 0, len(b))
	out = append(out, 0xFF, 0xD8)
	i := 2
	varreduras := 0
	for {
		// O marcador: um ou mais 0xFF (os de antes do último são enchimento) e o
		// código.
		if i >= len(b) || b[i] != 0xFF {
			return nil, ErrInvalida
		}
		for i < len(b) && b[i] == 0xFF {
			i++
		}
		if i >= len(b) {
			return nil, ErrInvalida
		}
		m := b[i]
		i++
		switch {
		case m == 0xD9: // EOI: o fim. O que vier depois não é imagem.
			return append(out, 0xFF, 0xD9), nil
		case m == 0x00, m == 0x01, m >= 0xD0 && m <= 0xD8:
			// "FF 00" e RSTn só existem dentro dos dados de uma varredura; TEM e
			// um segundo SOI não fazem sentido aqui.
			return nil, ErrInvalida
		}
		// Os outros marcadores têm comprimento (dois bytes, que se contam).
		if i+2 > len(b) {
			return nil, ErrInvalida
		}
		n := int(b[i])<<8 | int(b[i+1])
		if n < 2 || i+n > len(b) {
			return nil, ErrInvalida
		}
		segmento := b[i : i+n]
		i += n
		metadado := (m >= 0xE1 && m <= 0xEF) || m == 0xFE
		if !metadado {
			out = append(out, 0xFF, m)
			out = append(out, segmento...)
		}
		if m != 0xDA {
			continue
		}
		// SOS: os dados comprimidos vêm logo depois do cabeçalho da varredura.
		varreduras++
		if varreduras > maxVarreduras {
			return nil, ErrInvalida
		}
		inicio := i
		for {
			if i >= len(b) {
				return nil, ErrInvalida
			}
			if b[i] != 0xFF {
				i++
				continue
			}
			if i+1 >= len(b) {
				return nil, ErrInvalida
			}
			if prox := b[i+1]; prox == 0x00 || (prox >= 0xD0 && prox <= 0xD7) {
				i += 2
				continue
			}
			break // um marcador de verdade (ou enchimento antes dele)
		}
		out = append(out, b[inicio:i]...)
	}
}
