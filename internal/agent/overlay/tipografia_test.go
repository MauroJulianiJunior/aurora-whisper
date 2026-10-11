package overlay

import (
	"image"
	"math"
	"os"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// TestTipografia_EhAPlexMonoMediumComALicenca trava a origem do arquivo
// embutido: IBM Plex Mono, peso Medium — a fonte que o design system
// reserva para status e microcopy técnica. E a licença (SIL OFL 1.1) tem
// de continuar ao lado dele: a OFL exige que ela acompanhe a fonte.
func TestTipografia_EhAPlexMonoMediumComALicenca(t *testing.T) {
	f, err := opentype.Parse(fonteTTF)
	if err != nil {
		t.Fatalf("a fonte embutida não abre: %v", err)
	}
	for _, c := range []struct {
		id   sfnt.NameID
		want string
	}{
		{sfnt.NameIDFamily, "IBM Plex Mono"},
		{sfnt.NameIDFull, "IBM Plex Mono Medium"},
	} {
		// Algumas fontes guardam a família tipográfica no ID 16 e deixam o
		// peso no nome da família do ID 1 ("IBM Plex Mono Medium").
		got, err := f.Name(nil, c.id)
		if err != nil || !strings.HasPrefix(got, c.want) {
			t.Errorf("nome %d = %q (%v), want começando com %q", c.id, got, err, c.want)
		}
	}
	licenca, err := os.ReadFile("fontes/OFL.txt")
	if err != nil {
		t.Fatalf("a licença da fonte sumiu: %v", err)
	}
	if !strings.Contains(string(licenca), "SIL Open Font License") {
		t.Error("fontes/OFL.txt não é a SIL Open Font License")
	}
}

// TestTipografia_CobreTodasAsMensagens: todo caractere de toda mensagem
// que a pílula mostra tem desenho próprio na fonte. Sem ele, sai o .notdef
// — um retângulo vazio no meio da palavra.
//
// Percorre fasesDoOverlay. A fonte de bitmap de antes tinha um teste
// igual, que percorria só as fases do núcleo: a mensagem de janela de
// administrador passou sem os glifos de "O", "W" e "j", e a pílula dizia
// "indo s não deixa ditar em anela de administrador.".
//
// Confere também o resto do português — acentos, til e cedilha, em
// maiúscula e minúscula —, para uma mensagem nova não precisar de fonte
// nova, e o "…" do corte (truncaNaLargura).
func TestTipografia_CobreTodasAsMensagens(t *testing.T) {
	textos := []string{
		"áàâãéêíóôõúüç ÁÀÂÃÉÊÍÓÔÕÚÜÇ",
		"abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789",
		".,:;!?+-()/%…",
	}
	for _, p := range fasesDoOverlay() {
		textos = append(textos, Label(p))
	}
	for _, texto := range textos {
		for _, r := range texto {
			if r != ' ' && !tipo().temGlifo(r) {
				t.Errorf("%q (de %q) não tem glifo na fonte embutida", r, texto)
			}
		}
	}
}

// TestTipografia_TemGlifoRecusaOQueNaoTem é a guarda do teste acima: o
// arquivo é só o subconjunto latin, e um caractere de fora dele tem de
// dar falso — senão o teste de cobertura aprovaria qualquer coisa.
func TestTipografia_TemGlifoRecusaOQueNaoTem(t *testing.T) {
	for _, r := range []rune{'ā', 'ő', 'Ж', '中'} {
		if tipo().temGlifo(r) {
			t.Errorf("temGlifo(%q) = true, mas o subconjunto latin não tem esse caractere", r)
		}
	}
}

// TestTipografia_TamanhoEAvanco trava o corpo pedido — "fonte ~12-13 px",
// uns 30% menor que o bitmap — e a régua monoespaçada: todo caractere
// avança o mesmo tanto, em pixel inteiro (8 px a 13 px de corpo).
//
// A altura de maiúscula do bitmap era 14 px (7 linhas na escala 2); a da
// Plex Mono a 13 px é ~9, 35% menor.
func TestTipografia_TamanhoEAvanco(t *testing.T) {
	if FonteTamanho < 12 || FonteTamanho > 13 {
		t.Errorf("FonteTamanho = %d, o pedido foi de 12 a 13 px", FonteTamanho)
	}
	const capAntes = 14.0
	if reducao := 1 - tipo().capAltura/capAntes; reducao < 0.25 || reducao > 0.45 {
		t.Errorf("maiúscula com %.1f px, %.0f%% menor que os %v de antes — o pedido foi uns 30%%",
			tipo().capAltura, 100*reducao, capAntes)
	}
	for _, p := range fasesDoOverlay() {
		for _, r := range Label(p) {
			if w := tipo().largura(string(r)); w != 8 {
				t.Errorf("%q avança %d px, want 8 (monoespaçada, avanço arredondado)", r, w)
			}
		}
	}
	if w := tipo().largura("Inserido"); w != 64 {
		t.Errorf("largura(\"Inserido\") = %d, want 64", w)
	}
}

// TestTipografia_AlturaOptica: as alturas de maiúscula e minúscula são as
// da fonte (OS/2 da Plex Mono: 698 e 516 por 1000 de corpo), sem o
// arredondamento para cima que o hinting faria, e a altura óptica — que
// é o que se centra na pílula — é a média das duas.
func TestTipografia_AlturaOptica(t *testing.T) {
	maiuscula, minuscula := tipo().capAltura, tipo().xAltura
	if want := 0.698 * FonteTamanho; math.Abs(maiuscula-want) > 0.05 {
		t.Errorf("capAltura = %.3f, want %.3f", maiuscula, want)
	}
	if want := 0.516 * FonteTamanho; math.Abs(minuscula-want) > 0.05 {
		t.Errorf("xAltura = %.3f, want %.3f", minuscula, want)
	}
	if got, want := tipo().alturaOptica(), (maiuscula+minuscula)/2; got != want {
		t.Errorf("alturaOptica = %.3f, want a média %.3f", got, want)
	}
}

// comKerning é uma face com kerning de k em todo par de letras. A Plex
// Mono não tem kerning nenhum, então é com ela que se prova que desenha
// anda a caneta como largura e tinta medem.
type comKerning struct {
	font.Face
	k fixed.Int26_6
}

func (f comKerning) Kern(r0, r1 rune) fixed.Int26_6 { return f.k }

// TestTipografia_DesenhaSomaOKerning: largura e tinta medem com
// font.MeasureString e font.BoundString, que somam o kerning de cada par.
// Se desenha só avançasse pelo avanço da letra, o texto desenhado sairia
// da caixa medida — mais curto com kerning positivo, pela borda com
// negativo — e a composição (ComporMensagem) centraria uma coisa e o
// desenho pintaria outra.
func TestTipografia_DesenhaSomaOKerning(t *testing.T) {
	for _, k := range []int{5, -3} {
		tp, err := novaTipografia(fonteTTF, FonteTamanho)
		if err != nil {
			t.Fatal(err)
		}
		tp.face = comKerning{Face: tp.face, k: fixed.I(k)}

		const s = "IIII"
		c := NewCanvas(120, 40)
		c.FillRect(0, 0, c.W, c.H, corCorpo.B, corCorpo.G, corCorpo.R)
		const x, base = 20, 25
		tp.desenha(c, x, base, s, corTexto)
		caixa := tp.tinta(s).Add(image.Pt(x, base))

		pintou := image.Rectangle{}
		for y := 0; y < c.H; y++ {
			for xx := 0; xx < c.W; xx++ {
				if b, g, r, _ := c.At(xx, y); (Cor{R: r, G: g, B: b}) != corCorpo {
					pintou = pintou.Union(image.Rect(xx, y, xx+1, y+1))
				}
			}
		}
		// A caixa é arredondada para fora: 1 px de folga em cada ponta.
		if pintou.Min.X < caixa.Min.X || pintou.Max.X > caixa.Max.X || caixa.Max.X-pintou.Max.X > 1 || pintou.Min.X-caixa.Min.X > 1 {
			t.Errorf("kerning de %d px: %q pintou x em [%d, %d), a caixa medida é [%d, %d)",
				k, s, pintou.Min.X, pintou.Max.X, caixa.Min.X, caixa.Max.X)
		}
	}
}

// TestTipografia_DesenhaDentroDaTinta: o que desenha pinta só dentro da
// caixa que tinta diz, e pinta alguma coisa. É o que os testes de
// composição (mensagem_test.go) usam para saber onde está cada linha sem
// renderizar.
func TestTipografia_DesenhaDentroDaTinta(t *testing.T) {
	for _, s := range []string{"Não", "transferência: Ctrl+V.", "ç", "Ã"} {
		c := NewCanvas(260, 40)
		c.FillRect(0, 0, c.W, c.H, corCorpo.B, corCorpo.G, corCorpo.R)
		const x, base = 10, 25
		tipo().desenha(c, x, base, s, corTexto)
		k := tipo().tinta(s).Add(image.Pt(x, base))

		pintados := 0
		for y := 0; y < c.H; y++ {
			for xx := 0; xx < c.W; xx++ {
				b, g, r, _ := c.At(xx, y)
				if (Cor{R: r, G: g, B: b}) == corCorpo {
					continue
				}
				pintados++
				if !image.Pt(xx, y).In(k) {
					t.Fatalf("%q pintou (%d,%d), fora da tinta %v", s, xx, y, k)
				}
			}
		}
		if pintados == 0 {
			t.Errorf("%q não pintou nada", s)
		}
	}
	// O acento de "Ã" fica acima da maiúscula, e a perna de "ç" abaixo da
	// base: a caixa acompanha os dois.
	if a, n := tipo().tinta("Ã"), tipo().tinta("A"); a.Min.Y >= n.Min.Y {
		t.Errorf("tinta(\"Ã\") começa em %d e tinta(\"A\") em %d — o til não entrou na caixa", a.Min.Y, n.Min.Y)
	}
	if k := tipo().tinta("ç"); k.Max.Y <= 0 {
		t.Errorf("tinta(\"ç\") termina em %d, na base ou acima — a cedilha não entrou na caixa", k.Max.Y)
	}
}
