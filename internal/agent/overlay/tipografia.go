package overlay

import (
	_ "embed"
	"fmt"
	"image"
	"math"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Este arquivo não tem build tag: é o texto das mensagens do overlay,
// rasterizado em Go puro sobre o Canvas — nenhuma chamada de GDI (ver
// canvas.go e render.go para o porquê) e testável no runner Linux.
//
// # A fonte é a IBM Plex Mono do design system, desde 29-set-2026
//
// Até ali o texto era uma fonte de bitmap 5x7 desenhada à mão e escalada
// 2x, sem suavização. Ela prendia a mensagem num tamanho só: na escala 1
// ficava ilegível, e na 2 cada letra tinha 14 px de altura e 12 de
// avanço, o que obrigava a pílula de mensagem a 300x56. Mauro, olhando a
// pílula compacta já aprovada ao lado das mensagens: "as mensagens ainda
// estão muito grandes. Pode distribuir melhor a largura delas e diminuir
// uns 30% do tamanho pra ficar mais compatível com a pílula."
//
// A fonte de bitmap tinha outro defeito, que só apareceu desenhando: o
// vocabulário dela era fechado, e a mensagem de janela de administrador
// ("O Windows não deixa ditar...") entrou depois sem os glifos de 'O',
// 'W' e 'j'. A pílula dizia "indo s não deixa ditar em anela de
// administrador." — o teste de cobertura da fonte percorria só as fases do
// núcleo, e aquela é do overlay (FaseJanelaElevada).
//
// A IBM Plex Mono é a fonte que o design system do projeto reserva para
// "status, dados, índices e microcopy técnica" — que é o que a pílula é.
// Vem com suavização de verdade, e com todo o Latin-1: qualquer mensagem
// em português cabe sem desenhar glifo nenhum à mão.
//
// # De onde vem o arquivo
//
// fontes/IBMPlexMono-Medium-latin.ttf é o subconjunto "latin" do peso 500
// (Medium) do pacote @fontsource/ibm-plex-mono 5.3.0, o mesmo que o
// painel já usa (web/). O pacote distribui WOFF; o WOFF 1.0 é só as
// tabelas do TTF comprimidas com zlib, e foi convertido uma vez, fora do
// repositório, descomprimindo as tabelas e remontando o diretório sfnt
// (checksums conferidos contra os do WOFF). Nenhuma tabela foi alterada.
// A licença, SIL Open Font License 1.1, está ao lado: fontes/OFL.txt. Ela
// tem de acompanhar o executável distribuído, porque dentro dele ninguém a
// vê — THIRD_PARTY_NOTICES.md, na raiz, registra isso e os avisos BSD do
// x/image e do x/text, que esta fonte trouxe para o Agent.
//
// Só o subconjunto "latin" (U+0000-00FF, mais pontuação geral, onde mora
// o "…"): é nele que estão todas as letras do português, acentos e
// cedilha inclusive. O "latin-ext" (U+0100 em diante) não tem nada que
// uma mensagem em português use, e juntar dois subconjuntos num arquivo
// só exigiria refazer as tabelas de glifos. TestTipografia_CobreTodasAs
// Mensagens é quem acusa se um dia uma mensagem precisar dele.
//
// Peso 500, e não o 400 do corpo de texto: a pílula é texto claro sobre
// quase-preto a 13 px, e o regular sai fino demais nesse tamanho — os
// três pesos foram desenhados lado a lado antes da escolha.
//
// # O rasterizador
//
// golang.org/x/image/font/opentype, do time do Go. Go puro, sem CGO, e
// é a dependência nova desta mudança (justificada no commit que a
// trouxe). O que se usa dele é pouco: ler o TTF, medir o avanço de cada
// letra e rasterizar o contorno em máscara de cobertura.
//
//go:embed fontes/IBMPlexMono-Medium-latin.ttf
var fonteTTF []byte

// FonteTamanho é o corpo do texto das mensagens, em pixels (o "em" da
// fonte). A fonte de bitmap tinha 14 px de altura de maiúscula; a 13 px a
// Plex Mono tem 9 — os ~30% a menos que Mauro pediu, na faixa de 12 a
// 13 px que a pílula comporta legível. A 12 px o avanço arredondado cai
// para 7 px e as letras se apertam; a 13, ele é 8.
const FonteTamanho = 13

// tipografia é a fonte já preparada no tamanho das mensagens.
//
// A face do x/image reaproveita buffers entre chamadas e não pode ser
// usada por duas goroutines ao mesmo tempo. Em produção só a thread da
// janela desenha, mas os testes e o cálculo de layout também medem, então
// todo acesso passa por mu.
type tipografia struct {
	mu   sync.Mutex
	face font.Face

	// capAltura/xAltura são as alturas de maiúscula e de minúscula em
	// pixels, sem arredondar: é com elas que a linha de base é posta (ver
	// alturaOptica).
	capAltura, xAltura float64
}

// tipo devolve a tipografia do overlay, preparada na primeira chamada.
//
// O arquivo é embutido no binário: se ele não abrir, é o build que está
// quebrado, não a máquina de quem usa — daí o pânico, que qualquer teste
// deste pacote dispara na hora.
var tipo = sync.OnceValue(func() *tipografia {
	t, err := novaTipografia(fonteTTF, FonteTamanho)
	if err != nil {
		panic(fmt.Sprintf("overlay: a fonte embutida não abriu: %v", err))
	}
	return t
})

func novaTipografia(ttf []byte, tamanho float64) (*tipografia, error) {
	f, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	// HintingFull arredonda o AVANÇO de cada letra para pixel inteiro. O
	// contorno não é ajustado à grade (o x/image não interpreta as
	// instruções de hinting do TrueType) — só a posição de cada letra. Numa
	// fonte monoespaçada, isso põe toda letra começando em pixel inteiro, e
	// letras iguais saem iguais.
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: tamanho, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	m, err := f.Metrics(nil, fixed.Int26_6(math.Round(tamanho*64)), font.HintingNone)
	if err != nil {
		return nil, err
	}
	return &tipografia{
		face:      face,
		capAltura: float64(m.CapHeight) / 64,
		xAltura:   float64(m.XHeight) / 64,
	}, nil
}

// alturaOptica é a altura que se centra na pílula: a média entre a da
// maiúscula e a da minúscula.
//
// Centrar a altura de maiúscula deixa o texto baixo — as mensagens são
// quase só minúsculas, e o olho pesa o corpo delas. Centrar a de
// minúscula deixa as maiúsculas altas. A média é o meio-termo de sempre
// para texto em caixa mista.
func (t *tipografia) alturaOptica() float64 { return (t.capAltura + t.xAltura) / 2 }

// largura é quanto s avança, em pixels inteiros.
func (t *tipografia) largura(s string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return font.MeasureString(t.face, s).Ceil()
}

// temGlifo reporta se a fonte tem desenho próprio para r. Sem ele, o
// rasterizador desenha o glifo .notdef — um retângulo vazio — no lugar.
func (t *tipografia) temGlifo(r rune) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.face.GlyphAdvance(r)
	return ok
}

// tinta devolve o retângulo que a tinta de s ocupa, em pixels, com a
// caneta em (0,0) na linha de base: Min.Y é negativo (acima da base).
// Arredondado para fora, então cobre todo pixel que a máscara pode acender.
func (t *tipografia) tinta(s string) image.Rectangle {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, _ := font.BoundString(t.face, s)
	return image.Rect(b.Min.X.Floor(), b.Min.Y.Floor(), b.Max.X.Ceil(), b.Max.Y.Ceil())
}

// desenha escreve s em c com a caneta começando em x e a linha de base em
// base, na cor opaca cor. Cada pixel recebe a cor na fração que a letra
// cobre dele, composta por cima do que já está lá (Canvas.compoeCobertura)
// — sobre o corpo da pílula, o pixel continua opaco; só a cor mistura.
//
// A caneta anda como em largura e tinta (font.MeasureString e
// font.BoundString): avanço de cada letra MAIS o kerning do par. A Plex
// Mono é monoespaçada e o kerning dá zero em todo par, mas com outra fonte
// o texto desenhado sairia da caixa medida se só um lado o somasse
// (TestTipografia_DesenhaSomaOKerning).
func (t *tipografia) desenha(c *Canvas, x, base int, s string, cor Cor) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, g, r := cor.bgr()
	caneta := fixed.P(x, base)
	anterior := rune(-1)
	for _, ch := range s {
		if anterior >= 0 {
			caneta.X += t.face.Kern(anterior, ch)
		}
		anterior = ch
		dr, mask, mp, avanco, _ := t.face.Glyph(caneta, ch)
		if alpha, ok := mask.(*image.Alpha); ok {
			for y := dr.Min.Y; y < dr.Max.Y; y++ {
				for x := dr.Min.X; x < dr.Max.X; x++ {
					cob := alpha.AlphaAt(mp.X+x-dr.Min.X, mp.Y+y-dr.Min.Y).A
					if cob != 0 {
						c.compoeCobertura(x, y, b, g, r, cob)
					}
				}
			}
		}
		caneta.X += avanco
	}
}
