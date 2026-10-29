package imagemdanota

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// As imagens dos testes nascem aqui, pelo codificador do Go: nada de arquivo
// de foto de verdade no repositório (uma foto de celular traria a localização
// de quem a tirou).

// jpegDe codifica uma imagem w x h, com um degradê (para os dados comprimidos não
// serem triviais).
func jpegDe(t testing.TB, w, h int, cinza bool) []byte {
	t.Helper()
	var img image.Image
	if cinza {
		g := image.NewGray(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				g.SetGray(x, y, color.Gray{Y: uint8((x*7 + y*3) % 256)})
			}
		}
		img = g
	} else {
		c := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 3), B: uint8((x + y) * 2), A: 255})
			}
		}
		img = c
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// segmento monta um segmento com comprimento: FF, o marcador, os dois bytes do
// comprimento e o conteúdo.
func segmento(m byte, conteudo []byte) []byte {
	n := len(conteudo) + 2
	return append([]byte{0xFF, m, byte(n >> 8), byte(n)}, conteudo...)
}

// depoisDoSOI põe os segmentos logo depois do SOI.
func depoisDoSOI(b []byte, segs ...[]byte) []byte {
	out := append([]byte{}, b[:2]...)
	for _, s := range segs {
		out = append(out, s...)
	}
	return append(out, b[2:]...)
}

// O EXIF de mentira, com a localização: o que a foto do celular carrega. Os
// textos servem para procurar nos bytes limpos.
var (
	exifComGPS = append([]byte("Exif\x00\x00MM\x00*GPSLatitude=-23.5505;GPSLongitude=-46.6333;"), bytes.Repeat([]byte{0x42}, 64)...)
	jfif       = []byte("JFIF\x00\x01\x02\x00\x00\x01\x00\x01\x00\x00")
	xmp        = []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>Câmera do Mauro</x:xmpmeta>")
	icc        = append([]byte("ICC_PROFILE\x00\x01\x01"), bytes.Repeat([]byte{0x11}, 40)...)
	comentario = []byte("tirada na casa do cliente")
)

// fotoDeCelular é o JPEG com tudo o que a foto de um celular leva: o JFIF, o
// EXIF com a localização, o XMP, o perfil de cor, um comentário e, depois do fim
// da imagem, o vídeo da "foto em movimento".
func fotoDeCelular(t testing.TB) []byte {
	b := depoisDoSOI(jpegDe(t, 64, 48, false),
		segmento(0xE0, jfif), segmento(0xE1, exifComGPS), segmento(0xE1, xmp), segmento(0xE2, icc), segmento(0xFE, comentario))
	return append(b, []byte("ftypmp42 o vídeo da foto em movimento")...)
}

// marcadoresDe lista os marcadores fora dos dados comprimidos: uma leitura à
// parte da de semMetadados, para conferir uma com a outra.
func marcadoresDe(t testing.TB, b []byte) []byte {
	t.Helper()
	var ms []byte
	i := 2
	for i < len(b) {
		for i < len(b) && b[i] == 0xFF {
			i++
		}
		if i >= len(b) {
			t.Fatalf("o JPEG acaba num marcador")
		}
		m := b[i]
		i++
		ms = append(ms, m)
		if m == 0xD9 {
			if i != len(b) {
				t.Fatalf("%d bytes depois do EOI", len(b)-i)
			}
			return ms
		}
		n := int(b[i])<<8 | int(b[i+1])
		i += n
		if m == 0xDA {
			for i < len(b) && !(b[i] == 0xFF && b[i+1] != 0x00 && (b[i+1] < 0xD0 || b[i+1] > 0xD7)) {
				i++
			}
		}
	}
	t.Fatal("o JPEG acaba sem o EOI")
	return nil
}

func conferirOuFalha(t testing.TB, b []byte) Limpa {
	t.Helper()
	l, err := Conferir(b)
	if err != nil {
		t.Fatalf("Conferir: %v", err)
	}
	return l
}

// TestConferir_AFotoSaiSemOEXIFNemALocalizacao é a prova do que a ADR promete: a
// foto com EXIF (e a localização dentro dele), XMP, perfil de cor, comentário e o
// vídeo pendurado no fim sai só com a imagem. Os pixels são os mesmos: nada foi
// recomprimido.
func TestConferir_AFotoSaiSemOEXIFNemALocalizacao(t *testing.T) {
	original := fotoDeCelular(t)
	l := conferirOuFalha(t, original)

	for _, proibido := range [][]byte{[]byte("Exif"), []byte("GPS"), []byte("-23.5505"), []byte("xmpmeta"),
		[]byte("ICC_PROFILE"), comentario, []byte("ftypmp42")} {
		if bytes.Contains(l.Bytes, proibido) {
			t.Errorf("a imagem limpa ainda tem %q", proibido)
		}
	}
	for _, m := range marcadoresDe(t, l.Bytes) {
		if (m >= 0xE1 && m <= 0xEF) || m == 0xFE {
			t.Errorf("a imagem limpa ainda tem o segmento FF %02X", m)
		}
	}
	// O APP0 (JFIF) fica: não é metadado da foto, é o formato.
	if !bytes.Contains(l.Bytes, []byte("JFIF")) {
		t.Error("o APP0 (JFIF) sumiu")
	}
	if l.Largura != 64 || l.Altura != 48 {
		t.Errorf("tamanho = %dx%d, quero 64x48", l.Largura, l.Altura)
	}
	soma := sha256.Sum256(l.Bytes)
	if l.SHA256 != hex.EncodeToString(soma[:]) {
		t.Errorf("SHA256 = %s, que não é o dos bytes limpos", l.SHA256)
	}

	// Os mesmos pixels da original (os dados comprimidos não mudaram).
	a, err := jpeg.Decode(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	d, err := jpeg.Decode(bytes.NewReader(l.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	ya, yd := a.(*image.YCbCr), d.(*image.YCbCr)
	if !bytes.Equal(ya.Y, yd.Y) || !bytes.Equal(ya.Cb, yd.Cb) || !bytes.Equal(ya.Cr, yd.Cr) {
		t.Error("os pixels mudaram: a imagem foi recomprimida ou estragada")
	}

	// Conferir de novo não muda nada (é o que o servidor faz com o que o PC manda).
	if de2 := conferirOuFalha(t, l.Bytes); !bytes.Equal(de2.Bytes, l.Bytes) {
		t.Error("conferir a imagem limpa a mudou")
	}
}

func TestConferir_Tabela(t *testing.T) {
	comum := jpegDe(t, 40, 30, false)
	cinza := jpegDe(t, 30, 20, true)

	// O SOF0 do JPEG do Go: FF C0, comprimento, precisão, altura e largura.
	sof := bytes.Index(comum, []byte{0xFF, 0xC0})
	if sof < 0 {
		t.Fatal("o JPEG de teste não tem SOF0")
	}
	comLado := func(w, h int) []byte {
		b := append([]byte{}, comum...)
		b[sof+5], b[sof+6] = byte(h>>8), byte(h)
		b[sof+7], b[sof+8] = byte(w>>8), byte(w)
		return b
	}
	sos := bytes.Index(comum, []byte{0xFF, 0xDA})
	eoi := len(comum) - 2
	// A varredura inteira (o SOS e os dados), para repetir.
	varredura := comum[sos:eoi]
	muitasVarreduras := append(append([]byte{}, comum[:sos]...), bytes.Repeat(varredura, maxVarreduras+1)...)
	muitasVarreduras = append(muitasVarreduras, 0xFF, 0xD9)
	algumasVarreduras := append(append([]byte{}, comum[:sos]...), bytes.Repeat(varredura, 3)...)
	algumasVarreduras = append(algumasVarreduras, 0xFF, 0xD9)
	// O APP14 da Adobe com transform 0: as cores são RGB, e só o metadado diz isso.
	adobeRGB := depoisDoSOI(comum, segmento(0xEE, []byte("Adobe\x00\x64\x00\x00\x00\x00\x00")))
	lixoEntreSegmentos := append(append(append([]byte{}, comum[:sos]...), 0x00, 0x13), comum[sos:]...)
	comprimentoErrado := depoisDoSOI(comum, []byte{0xFF, 0xE1, 0xFF, 0xF0, 'E', 'x'})

	casos := []struct {
		nome string
		b    []byte
		erro error
		w, h int
	}{
		{"o JPEG comum", comum, nil, 40, 30},
		{"o JPEG em cinza", cinza, nil, 30, 20},
		{"a foto de celular", fotoDeCelular(t), nil, 64, 48},
		{"três varreduras", algumasVarreduras, nil, 40, 30},
		{"no limite do lado", jpegDe(t, MaxLado, 8, false), nil, MaxLado, 8},
		{"vazio", nil, ErrNaoEJPEG, 0, 0},
		{"curto", []byte{0xFF, 0xD8}, ErrNaoEJPEG, 0, 0},
		{"um PNG", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), ErrNaoEJPEG, 0, 0},
		{"um GIF", []byte("GIF89a\x01\x00\x01\x00"), ErrNaoEJPEG, 0, 0},
		{"texto", []byte("isto não é uma imagem"), ErrNaoEJPEG, 0, 0},
		{"grande demais", append(append([]byte{}, comum...), make([]byte, MaxBytes)...), ErrGrandeDemais, 0, 0},
		{"lado de 2.001 px", jpegDe(t, MaxLado+1, 8, false), ErrLadoGrande, 0, 0},
		{"alto de 2.001 px", jpegDe(t, 8, MaxLado+1, true), ErrLadoGrande, 0, 0},
		// A bomba: o cabeçalho diz 60.000 x 60.000 (3,6 bilhões de pixels) num
		// arquivo de poucos KB. DecodeConfig a recusa antes de alocar a imagem.
		{"a bomba de descompressão", comLado(60000, 60000), ErrLadoGrande, 0, 0},
		{"lado zero", comLado(0, 30), ErrLadoGrande, 0, 0},
		{"cortado no meio dos dados", comum[:sos+40], ErrInvalida, 0, 0},
		{"sem o EOI", comum[:eoi], ErrInvalida, 0, 0},
		{"só o cabeçalho", comum[:sos], ErrInvalida, 0, 0},
		{"lixo entre os segmentos", lixoEntreSegmentos, ErrInvalida, 0, 0},
		{"comprimento que passa do fim", comprimentoErrado, ErrInvalida, 0, 0},
		{"varreduras demais", muitasVarreduras, ErrInvalida, 0, 0},
		{"as cores RGB da Adobe", adobeRGB, ErrInvalida, 0, 0},
		{"um segundo SOI", depoisDoSOI(comum, []byte{0xFF, 0xD8}), ErrInvalida, 0, 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			l, err := Conferir(c.b)
			if !errors.Is(err, c.erro) {
				t.Fatalf("erro = %v, quero %v", err, c.erro)
			}
			if c.erro != nil {
				if l.Bytes != nil {
					t.Error("recusou, mas devolveu bytes")
				}
				return
			}
			if l.Largura != c.w || l.Altura != c.h {
				t.Errorf("tamanho = %dx%d, quero %dx%d", l.Largura, l.Altura, c.w, c.h)
			}
		})
	}
}

// TestConferir_NaoMexeNaOriginal: a cópia limpa é outra fatia; os bytes de quem
// chamou ficam como estavam.
func TestConferir_NaoMexeNaOriginal(t *testing.T) {
	original := fotoDeCelular(t)
	copia := append([]byte{}, original...)
	l := conferirOuFalha(t, original)
	if !bytes.Equal(original, copia) {
		t.Error("Conferir mexeu nos bytes de quem chamou")
	}
	l.Bytes[len(l.Bytes)-3] ^= 0xFF
	if !bytes.Equal(original, copia) {
		t.Error("a imagem limpa divide a memória com a original")
	}
}

func TestFraseDoTeto(t *testing.T) {
	if TetoDoTotal != 400<<20 || TetoEmMB() != 400 {
		t.Fatalf("o teto = %d bytes (%d MB); a decisão do Mauro é 400 MB", TetoDoTotal, TetoEmMB())
	}
	const quero = "As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras."
	if FraseDoTeto() != quero {
		t.Errorf("FraseDoTeto() = %q", FraseDoTeto())
	}
}

// FuzzConferir procura entrada que derrube o Conferir ou que passe sem estar
// limpa. Rode com go test -fuzz=FuzzConferir ./internal/imagemdanota; o
// go test comum roda só as sementes.
func FuzzConferir(f *testing.F) {
	f.Add(jpegDe(f, 16, 16, false))
	f.Add(jpegDe(f, 9, 7, true))
	f.Add(fotoDeCelular(f))
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xD9})
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x02, 0xFF, 0xD9})
	f.Add([]byte("\x89PNG\r\n\x1a\n"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		l, err := Conferir(b)
		if err != nil {
			if !errors.Is(err, ErrNaoEJPEG) && !errors.Is(err, ErrGrandeDemais) && !errors.Is(err, ErrLadoGrande) && !errors.Is(err, ErrInvalida) {
				t.Fatalf("erro fora dos quatro: %v", err)
			}
			return
		}
		if len(l.Bytes) > MaxBytes || len(l.Bytes) > len(b) {
			t.Fatalf("a imagem limpa tem %d bytes (a original, %d)", len(l.Bytes), len(b))
		}
		if l.Largura <= 0 || l.Altura <= 0 || l.Largura > MaxLado || l.Altura > MaxLado {
			t.Fatalf("tamanho %dx%d passou", l.Largura, l.Altura)
		}
		for _, m := range marcadoresDe(t, l.Bytes) {
			if (m >= 0xE1 && m <= 0xEF) || m == 0xFE {
				t.Fatalf("o segmento FF %02X passou", m)
			}
		}
		img, err := jpeg.Decode(bytes.NewReader(l.Bytes))
		if err != nil {
			t.Fatalf("a imagem limpa não abre: %v", err)
		}
		if img.Bounds().Dx() != l.Largura || img.Bounds().Dy() != l.Altura {
			t.Fatalf("a imagem limpa tem outro tamanho")
		}
		soma := sha256.Sum256(l.Bytes)
		if l.SHA256 != hex.EncodeToString(soma[:]) {
			t.Fatal("o SHA256 não é o dos bytes limpos")
		}
		de2, err := Conferir(l.Bytes)
		if err != nil || !bytes.Equal(de2.Bytes, l.Bytes) {
			t.Fatalf("conferir a limpa de novo: %v", err)
		}
	})
}

// TestConferir_ALimpaPassaIgual: conferir de novo a cópia limpa devolve os
// mesmos bytes e o mesmo SHA-256. O sync das imagens (N10b) depende disso: o
// servidor guarda a cópia que o Conferir dele limpou, e o PC que a baixa a
// confere de novo e exige o SHA-256 da linha (storage.GuardarBaixada).
func TestConferir_ALimpaPassaIgual(t *testing.T) {
	for _, b := range [][]byte{fotoDeCelular(t), jpegDe(t, 64, 48, true), jpegDe(t, 2000, 10, false)} {
		uma := conferirOuFalha(t, b)
		duas := conferirOuFalha(t, uma.Bytes)
		if !bytes.Equal(uma.Bytes, duas.Bytes) || uma.SHA256 != duas.SHA256 || uma.Largura != duas.Largura || uma.Altura != duas.Altura {
			t.Errorf("a cópia limpa mudou ao ser conferida de novo (%d -> %d bytes)", len(uma.Bytes), len(duas.Bytes))
		}
	}
}
