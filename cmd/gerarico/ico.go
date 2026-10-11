package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
)

// tamanhosDoIcone são as camadas do .ico: 16, 24, 32 e 48 px são os que o
// Windows pede a 100%, 150%, 200% e 300% de escala nas listas e na barra de
// tarefas; 64 para os mosaicos médios do Explorador; 256 para os grandes.
var tamanhosDoIcone = []int{16, 24, 32, 48, 64, 256}

// imagem é uma camada já codificada: exatamente os bytes que vão no .ico e,
// iguais, no recurso RT_ICON do executável.
type imagem struct {
	Tam   int
	Dados []byte
	// PNG diz se Dados é um PNG, e não um bitmap (DIB). Só a camada de 256 px é
	// PNG: é o que os criadores de ícone fazem desde o Windows Vista, e mantém o
	// arquivo pequeno. As menores seguem em bitmap, que todo leitor de ícone
	// (inclusive o do instalador) entende.
	PNG bool
}

const pngAPartirDe = 256

// imagensDoIcone desenha e codifica todas as camadas.
func imagensDoIcone() ([]imagem, error) {
	var out []imagem
	for _, tam := range tamanhosDoIcone {
		pix := pixelsDoIcone(tam)
		if tam >= pngAPartirDe {
			dados, err := codificarPNG(tam, pix)
			if err != nil {
				return nil, fmt.Errorf("camada de %d px: %w", tam, err)
			}
			out = append(out, imagem{Tam: tam, Dados: dados, PNG: true})
			continue
		}
		out = append(out, imagem{Tam: tam, Dados: codificarDIB(tam, pix)})
	}
	return out, nil
}

// codificarPNG codifica pix (BGRA, alfa direto) como PNG RGBA de 8 bits.
func codificarPNG(tam int, pix []byte) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, tam, tam))
	for i := 0; i < tam*tam*4; i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pix[i+2], pix[i+1], pix[i], pix[i+3]
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// codificarDIB codifica pix (BGRA, alfa direto) como o bitmap de um ícone: o
// BITMAPINFOHEADER com a altura DOBRADA (a metade de baixo é a máscara AND),
// as linhas de baixo para cima, e a máscara de 1 bit por pixel (1 onde o pixel
// é transparente). O alfa de 32 bits é o que o Windows usa; a máscara fica
// coerente com ele para os leitores antigos.
func codificarDIB(tam int, pix []byte) []byte {
	var b escritor
	b.u32(40) // biSize
	b.u32(uint32(tam))
	b.u32(uint32(tam * 2)) // altura: XOR + AND
	b.u16(1)               // biPlanes
	b.u16(32)              // biBitCount
	b.u32(0)               // biCompression: BI_RGB
	linhaMascara := ((tam + 31) / 32) * 4
	b.u32(uint32(tam*tam*4 + linhaMascara*tam)) // biSizeImage
	b.u32(0)                                    // biXPelsPerMeter
	b.u32(0)                                    // biYPelsPerMeter
	b.u32(0)                                    // biClrUsed
	b.u32(0)                                    // biClrImportant

	for y := tam - 1; y >= 0; y-- { // XOR: BGRA, de baixo para cima
		b.Write(pix[y*tam*4 : (y+1)*tam*4])
	}
	for y := tam - 1; y >= 0; y-- { // AND: 1 = transparente
		linha := make([]byte, linhaMascara)
		for x := 0; x < tam; x++ {
			if pix[(y*tam+x)*4+3] == 0 {
				linha[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(linha)
	}
	return b.Bytes()
}

// montarICO monta o arquivo .ico: o ICONDIR, uma ICONDIRENTRY por camada (em
// ordem de tamanho, como chegam) e os dados de cada uma.
func montarICO(imgs []imagem) []byte {
	var b escritor
	b.u16(0)                 // idReserved
	b.u16(1)                 // idType: ícone
	b.u16(uint16(len(imgs))) // idCount

	desloc := 6 + 16*len(imgs)
	for _, im := range imgs {
		b.u8(ladoNoIco(im.Tam)) // bWidth (0 quer dizer 256)
		b.u8(ladoNoIco(im.Tam)) // bHeight
		b.u8(0)                 // bColorCount: sem paleta
		b.u8(0)                 // bReserved
		b.u16(1)                // wPlanes
		b.u16(32)               // wBitCount
		b.u32(uint32(len(im.Dados)))
		b.u32(uint32(desloc))
		desloc += len(im.Dados)
	}
	for _, im := range imgs {
		b.Write(im.Dados)
	}
	return b.Bytes()
}

// ladoNoIco é o byte de largura/altura de um ICONDIRENTRY: 0 para 256.
func ladoNoIco(tam int) byte {
	if tam >= 256 {
		return 0
	}
	return byte(tam)
}

// escritor é um bytes.Buffer com as escritas little-endian que os formatos
// (.ico, COFF) pedem.
type escritor struct{ bytes.Buffer }

func (e *escritor) u8(v byte) { e.WriteByte(v) }
func (e *escritor) u16(v uint16) {
	var t [2]byte
	binary.LittleEndian.PutUint16(t[:], v)
	e.Write(t[:])
}
func (e *escritor) u32(v uint32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	e.Write(t[:])
}
