package overlay

import (
	"image"
	"math"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// --- a quebra, com uma régua de mentira --------------------------------------
//
// QuebraEquilibrada recebe a função que mede. Com uma régua de 10 px por
// runa, os testes de lógica não dependem da fonte: o que se trava aqui é
// a DECISÃO de onde quebrar.

func regua10(s string) int { return 10 * len([]rune(s)) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestQuebraEquilibrada_CabendoFicaNumaLinha(t *testing.T) {
	if got := QuebraEquilibrada("Inserido", regua10, 80); len(got) != 1 || got[0] != "Inserido" {
		t.Fatalf("QuebraEquilibrada(\"Inserido\", 80 px) = %q, want [\"Inserido\"]", got)
	}
	// Espaços repetidos não viram largura nem linha em branco.
	if got := QuebraEquilibrada("  Inserido  ", regua10, 80); len(got) != 1 || got[0] != "Inserido" {
		t.Fatalf("com espaços nas pontas = %q, want [\"Inserido\"]", got)
	}
	// Na largura exata ainda cabe: "ab cd" tem 50 px em 50.
	if got := QuebraEquilibrada("ab cd", regua10, 50); len(got) != 1 || got[0] != "ab cd" {
		t.Fatalf("QuebraEquilibrada(\"ab cd\", 50 px) = %q, want [\"ab cd\"] — cabia na largura exata", got)
	}
}

// TestQuebraEquilibrada_NaoEhGulosa é o defeito que Mauro viu: a quebra
// gulosa enchia a primeira linha e deixava uma palavra na segunda — "Não
// consegui inserir o" / "texto.". A equilibrada põe as duas linhas
// parecidas.
func TestQuebraEquilibrada_NaoEhGulosa(t *testing.T) {
	got := QuebraEquilibrada("Não consegui inserir o texto.", regua10, 220)
	want := []string{"Não consegui", "inserir o texto."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q (a gulosa daria [\"Não consegui inserir o\" \"texto.\"])", got, want)
	}
}

// TestQuebraEquilibrada_MenorLinhaMaiorPossivel confere, contra força
// bruta, que a quebra escolhida é a de MENOR linha mais longa — que é o
// que deixa a pílula a mais estreita possível. A força bruta confere
// também a diferença entre as linhas: em empate da maior, as diferenças
// são sempre iguais (ver QuebraEquilibrada), e isto acusa se deixarem de
// ser.
func TestQuebraEquilibrada_MenorLinhaMaiorPossivel(t *testing.T) {
	textos := []string{
		"Ditado desativado em campo protegido.",
		"Não consegui transcrever. Tente novamente.",
		"Texto na área de transferência: Ctrl+V.",
		"O Windows não deixa ditar em janela de administrador.",
		"aa bb cc dd",       // empate: "aa bb"/"cc dd" é o único melhor
		"aaa bb cc dd",      // "aaa bb"/"cc dd" (6/5) contra "aaa"/"bb cc dd" (3/8)
		"a bb ccc dddd eee", // várias quebras válidas
	}
	for _, texto := range textos {
		palavras := strings.Fields(texto)
		got := QuebraEquilibrada(texto, regua10, regua10(texto)-10)
		if len(got) != 2 {
			t.Fatalf("%q: %d linhas, want 2", texto, len(got))
		}
		melhorMaior, melhorDif := math.MaxInt, math.MaxInt
		for k := 1; k < len(palavras); k++ {
			a, b := regua10(strings.Join(palavras[:k], " ")), regua10(strings.Join(palavras[k:], " "))
			if max(a, b) < melhorMaior || (max(a, b) == melhorMaior && abs(a-b) < melhorDif) {
				melhorMaior, melhorDif = max(a, b), abs(a-b)
			}
		}
		la, lb := regua10(got[0]), regua10(got[1])
		if max(la, lb) != melhorMaior || abs(la-lb) != melhorDif {
			t.Errorf("%q: %q tem linha maior %d e diferença %d; o melhor é %d e %d",
				texto, got, max(la, lb), abs(la-lb), melhorMaior, melhorDif)
		}
		if strings.Join(got, " ") != strings.Join(palavras, " ") {
			t.Errorf("%q: a quebra alterou o texto: %q", texto, got)
		}
	}
}

// TestQuebraEquilibrada_EmpatePrefereAPrimeiraMaisLonga: "ab cd ef" em
// largura 50 quebra em "ab cd"/"ef" (5/2) ou "ab"/"cd ef" (2/5) — mesmo
// maior, mesma diferença. Fica a primeira mais longa.
func TestQuebraEquilibrada_EmpatePrefereAPrimeiraMaisLonga(t *testing.T) {
	got := QuebraEquilibrada("ab cd ef", regua10, 50)
	if len(got) != 2 || got[0] != "ab cd" || got[1] != "ef" {
		t.Fatalf("got %q, want [\"ab cd\" \"ef\"]", got)
	}
}

// TestQuebraEquilibrada_RespeitaALargura: a quebra nunca escolhe uma
// linha mais larga que o limite, mesmo que ela equilibrasse melhor.
func TestQuebraEquilibrada_RespeitaALargura(t *testing.T) {
	// "aaaaaaa bb" em 70: "aaaaaaa"/"bb" é a única que cabe.
	got := QuebraEquilibrada("aaaaaaa bb", regua10, 70)
	if len(got) != 2 || got[0] != "aaaaaaa" || got[1] != "bb" {
		t.Fatalf("got %q, want [\"aaaaaaa\" \"bb\"]", got)
	}
}

// TestQuebraEquilibrada_TextoLongoDemaisTrunca: se nem em duas linhas
// couber, a primeira leva o que couber e a segunda sai cortada com "…",
// cada uma dentro do limite. Nenhuma mensagem da spec chega aqui
// (TestComporMensagem_TodaMensagemCabeInteira).
func TestQuebraEquilibrada_TextoLongoDemaisTrunca(t *testing.T) {
	longo := strings.Repeat("palavra ", 40)
	got := QuebraEquilibrada(longo, regua10, 200)
	if len(got) != 2 {
		t.Fatalf("got %d linhas, want 2: %q", len(got), got)
	}
	for i, l := range got {
		if regua10(l) > 200 {
			t.Errorf("linha %d %q tem %d px, limite 200", i, l, regua10(l))
		}
	}
	if !strings.HasSuffix(got[1], "…") || strings.HasSuffix(got[0], "…") {
		t.Errorf("só a segunda linha termina em \"…\": %q", got)
	}
	if !strings.HasPrefix(longo, got[0]) {
		t.Errorf("a primeira linha não é o começo do texto: %q", got[0])
	}
}

// TestQuebraEquilibrada_PalavraUnicaLongaDemaisTrunca: quebrar a palavra
// ao meio produziria "transcre" + "ver", que se lê pior que "transcre…".
func TestQuebraEquilibrada_PalavraUnicaLongaDemaisTrunca(t *testing.T) {
	got := QuebraEquilibrada("supercalifragilistico", regua10, 100)
	if len(got) != 1 || got[0] != "supercali…" {
		t.Fatalf("got %q, want [\"supercali…\"]", got)
	}
}

func TestQuebraEquilibrada_VazioELimiteZeroNaoCriamLinha(t *testing.T) {
	if got := QuebraEquilibrada("", regua10, 100); got != nil {
		t.Errorf("texto vazio = %q, want nil", got)
	}
	if got := QuebraEquilibrada("qualquer coisa", regua10, 0); got != nil {
		t.Errorf("limite 0 = %q, want nil", got)
	}
}

func TestTruncaNaLargura(t *testing.T) {
	for _, c := range []struct {
		s    string
		max  int
		want string
	}{
		{"cabe", 40, "cabe"},
		{"ããããã", 30, "ãã…"}, // conta runas: "ã" não é cortado ao meio
		{"ab cd", 40, "ab…"}, // o espaço antes do "…" sai
		{"qualquer", 10, "…"},
		{"qualquer", 5, ""},
	} {
		if got := truncaNaLargura(c.s, regua10, c.max); got != c.want {
			t.Errorf("truncaNaLargura(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
}

// --- a composição das mensagens de verdade -----------------------------------

// fasesComMensagem são as fases que mostram texto.
func fasesComMensagem() []dictation.OverlayPhase {
	var fs []dictation.OverlayPhase
	for _, p := range fasesDoOverlay() {
		if Label(p) != "" && !DimensoesPara(p).ComOnda {
			fs = append(fs, p)
		}
	}
	return fs
}

// TestComporMensagem_TodaMensagemCabeInteira garante que nenhuma mensagem
// de estado aparece truncada na tela. Em uso real, uma já apareceu como
// "Ditado desativado em campo prot…" — mensagem de erro pela metade não
// informa. Além de caber, o texto quebrado tem de conter TUDO que a
// mensagem original dizia.
func TestComporMensagem_TodaMensagemCabeInteira(t *testing.T) {
	fases := fasesComMensagem()
	if len(fases) < 6 {
		t.Fatalf("só %d fases com mensagem — a lista perdeu alguma", len(fases))
	}
	for _, p := range fases {
		label := Label(p)
		m := ComporMensagem(p)
		if len(m.Linhas) == 0 || len(m.Linhas) > MaxLinhas {
			t.Errorf("%s: %q em %d linhas, cabem %d", nomeDaFase(p), label, len(m.Linhas), MaxLinhas)
			continue
		}
		for i, l := range m.Linhas {
			if strings.Contains(l, "…") {
				t.Errorf("%s: a linha %d saiu truncada: %q", nomeDaFase(p), i, l)
			}
			if w := tipo().largura(l); w > larguraUtil(m.TemSinal) {
				t.Errorf("%s: a linha %q tem %d px, cabem %d", nomeDaFase(p), l, w, larguraUtil(m.TemSinal))
			}
		}
		if got := strings.Join(m.Linhas, " "); got != strings.Join(strings.Fields(label), " ") {
			t.Errorf("%s: a quebra perdeu ou alterou texto:\n  original: %q\n  quebrado: %q", nomeDaFase(p), label, got)
		}
		if len(m.Bases) != len(m.Linhas) {
			t.Errorf("%s: %d linhas e %d linhas de base", nomeDaFase(p), len(m.Linhas), len(m.Bases))
		}
	}
}

// TestComporMensagem_QuebrasDasMensagens trava a quebra de cada mensagem
// da spec, como Mauro as vê. Uma régua ou uma quebra diferente muda isto
// — e aí é olhar o PNG de novo (render_png_test.go), não só atualizar.
func TestComporMensagem_QuebrasDasMensagens(t *testing.T) {
	for p, want := range map[dictation.OverlayPhase][]string{
		dictation.OverlaySuccess:      {"Inserido"},
		dictation.OverlayInsertFailed: {"Não consegui inserir o texto."},
		dictation.OverlayBlocked:      {"Ditado desativado", "em campo protegido."},
		dictation.OverlayError:        {"Não consegui transcrever.", "Tente novamente."},
		dictation.OverlayClipboard:    {"Texto na área de", "transferência: Ctrl+V."},
		FaseJanelaElevada:             {"O Windows não deixa ditar", "em janela de administrador."},
		FaseTextoNoHistorico:          {"O texto demorou e", "ficou no histórico."},
		FaseRepeticaoRemovida:         {"Removi uma repetição.", "Confira o texto."},
		FaseMicrofoneBloqueado:        {"O Windows bloqueou o", "microfone. Veja o painel."},
	} {
		if got := ComporMensagem(p).Linhas; strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: %q, want %q", nomeDaFase(p), got, want)
		}
	}
}

// TestComporMensagem_LinhasEquilibradas: nas mensagens de duas linhas, a
// mais curta tem pelo menos 60% da mais longa. A quebra gulosa de antes
// deixava 6 runas contra 22 ("texto." sob "Não consegui inserir o").
func TestComporMensagem_LinhasEquilibradas(t *testing.T) {
	duas := 0
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		if len(m.Linhas) != 2 {
			continue
		}
		duas++
		a, b := tipo().largura(m.Linhas[0]), tipo().largura(m.Linhas[1])
		if float64(min(a, b)) < 0.6*float64(max(a, b)) {
			t.Errorf("%s: linhas de %d e %d px (%q) — desequilibradas", nomeDaFase(p), a, b, m.Linhas)
		}
	}
	if duas == 0 {
		t.Fatal("nenhuma mensagem em duas linhas — o teste não prova nada")
	}
}

// TestComporMensagem_LarguraPeloTexto: a pílula tem a largura do bloco
// (ponto + vão + linha mais larga) mais as margens — nem mais, nem menos
// —, limitada por baixo pela largura da pílula compacta. "Inserido" fica
// na mínima; as outras, cada uma na sua.
func TestComporMensagem_LarguraPeloTexto(t *testing.T) {
	larguras := map[int]bool{}
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		bloco := 0
		for _, l := range m.Linhas {
			bloco = max(bloco, tipo().largura(l))
		}
		if m.TemSinal {
			bloco += SinalDiametro + SinalVao
		}
		if want := max(MensagemLarguraMin, bloco+2*MensagemMargemX); m.Dim.W != want {
			t.Errorf("%s: pílula com %d px de largura, want %d (bloco de %d + margens)", nomeDaFase(p), m.Dim.W, want, bloco)
		}
		if m.Dim.W < MensagemLarguraMin || m.Dim.W > MensagemLarguraMax {
			t.Errorf("%s: largura %d fora de [%d, %d]", nomeDaFase(p), m.Dim.W, MensagemLarguraMin, MensagemLarguraMax)
		}
		if want := alturaMensagem(len(m.Linhas)); m.Dim.H != want {
			t.Errorf("%s: altura %d com %d linha(s), want %d", nomeDaFase(p), m.Dim.H, len(m.Linhas), want)
		}
		larguras[m.Dim.W] = true
	}
	if d := DimensoesPara(dictation.OverlaySuccess); d.W != MensagemLarguraMin || d.H != MensagemAlturaUmaLinha {
		t.Errorf("\"Inserido\" = %dx%d, want a pílula curta %dx%d", d.W, d.H, MensagemLarguraMin, MensagemAlturaUmaLinha)
	}
	if len(larguras) < 4 {
		t.Errorf("só %d larguras diferentes entre as mensagens — a largura não acompanha o texto", len(larguras))
	}
}

// TestComporMensagem_MedidasDoDesign trava em NÚMEROS o que os PNGs
// mostraram e o relatório citou. Os testes acima calculam o esperado a
// partir das próprias constantes — MensagemMargemX, SinalVao,
// SinalDiametro, LinhaPasso —, então uma troca acidental de qualquer uma
// passava calada: mudava o PNG e todo o resto continuava verde. Aqui, cada
// uma muda pelo menos uma linha da tabela.
//
// Mudou de propósito? Olhe a folha de PNGs de novo (render_png_test.go) e
// só então atualize a tabela.
func TestComporMensagem_MedidasDoDesign(t *testing.T) {
	for p, want := range map[dictation.OverlayPhase]image.Point{
		dictation.OverlaySuccess:      {128, 32}, // a largura da compacta
		dictation.OverlayInsertFailed: {270, 32},
		dictation.OverlayBlocked:      {190, 48},
		dictation.OverlayError:        {238, 48},
		dictation.OverlayClipboard:    {214, 48},
		FaseJanelaElevada:             {254, 48},
		FaseTextoNoHistorico:          {190, 48}, // folha de PNGs de 01-out-2026
		FaseRepeticaoRemovida:         {206, 48}, // idem
		FaseMicrofoneBloqueado:        {238, 48}, // duas linhas, como a de erro
	} {
		if d := ComporMensagem(p).Dim; d.W != want.X || d.H != want.Y || d.Radius != 16 {
			t.Errorf("%s: pílula %dx%d raio %d, want %dx%d raio 16", nomeDaFase(p), d.W, d.H, d.Radius, want.X, want.Y)
		}
	}

	concentricas := 0
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		if !m.TemSinal {
			continue
		}
		// O ponto do componente "status" do design system: 8 px, com 6 px de
		// vão até o texto.
		if k := caixaDoPonto(m); k.Dx() != 8 || k.Dy() != 8 {
			t.Errorf("%s: ponto de %dx%d, want 8x8", nomeDaFase(p), k.Dx(), k.Dy())
		}
		if vao := m.TextoX - (m.SinalX + SinalDiametro); vao != 6 {
			t.Errorf("%s: %d px entre o ponto e o texto, want 6", nomeDaFase(p), vao)
		}
		// Na pílula de uma linha que tem a largura do bloco (não a mínima),
		// o ponto é concêntrico com a ponta esquerda da cápsula: o centro
		// dele é o centro do semicírculo, a 16 px da borda de cima e da
		// esquerda.
		if len(m.Linhas) == 1 && m.Dim.W > MensagemLarguraMin {
			concentricas++
			cx := float64(m.SinalX) + float64(SinalDiametro)/2
			cy := float64(m.SinalY) + float64(SinalDiametro)/2
			if cx != 16 || cy != 16 {
				t.Errorf("%s: ponto com o centro em (%.1f, %.1f), want (16, 16) — concêntrico com a ponta da cápsula",
					nomeDaFase(p), cx, cy)
			}
		}
	}
	if concentricas == 0 {
		t.Error("nenhuma mensagem de uma linha mais larga que a mínima — a checagem do ponto concêntrico não rodou")
	}
}

// TestComporMensagem_UnsTrintaPorCentoMenor trava o pedido de Mauro de
// 29-set-2026 — "diminuir uns 30% do tamanho" — contra a pílula de antes,
// 300x56 para qualquer mensagem:
//
//   - a de uma linha fica com 40 px de altura ou menos;
//   - nenhuma chega à altura nem à largura de antes;
//   - toda mensagem ocupa no máximo 75% da área de antes.
func TestComporMensagem_UnsTrintaPorCentoMenor(t *testing.T) {
	const antesW, antesH = 300, 56
	if MensagemAlturaUmaLinha > 40 {
		t.Errorf("pílula de uma linha com %d px de altura, want <= 40", MensagemAlturaUmaLinha)
	}
	// Vale para mensagem que ainda não existe: nenhuma pode chegar à largura
	// de antes, nem numa linha só.
	if MensagemLarguraMax > 0.95*antesW {
		t.Errorf("MensagemLarguraMax = %d: uma mensagem nova poderia sair tão larga quanto a pílula de antes (%d)",
			MensagemLarguraMax, antesW)
	}
	for _, p := range fasesComMensagem() {
		d := DimensoesPara(p)
		if d.W >= antesW || d.H >= antesH {
			t.Errorf("%s: %dx%d, não menor que os %dx%d de antes", nomeDaFase(p), d.W, d.H, antesW, antesH)
		}
		if area := float64(d.W*d.H) / (antesW * antesH); area > 0.75 {
			t.Errorf("%s: %dx%d é %.0f%% da área de antes, want <= 75%%", nomeDaFase(p), d.W, d.H, 100*area)
		}
	}
}

// TestLarguraUtil_FechaComALarguraMaxima: a linha mais larga que a quebra
// admite, somada ao ponto, ao vão e às duas margens, dá exatamente a
// largura máxima da pílula — nem passa (a pílula estouraria a superfície
// da janela), nem sobra (a quebra jogaria para duas linhas o que cabia
// numa).
func TestLarguraUtil_FechaComALarguraMaxima(t *testing.T) {
	if got := larguraUtil(true) + SinalDiametro + SinalVao + 2*MensagemMargemX; got != MensagemLarguraMax {
		t.Errorf("com ponto: %d px, want %d", got, MensagemLarguraMax)
	}
	if got := larguraUtil(false) + 2*MensagemMargemX; got != MensagemLarguraMax {
		t.Errorf("sem ponto: %d px, want %d", got, MensagemLarguraMax)
	}
}

// miolo reporta se o pixel (x,y), em coordenadas da pílula d, está no
// corpo com pelo menos 1 px de folga até o contorno: profundidade (régua
// independente, paleta_test.go) de BordaPilula + 1.
func miolo(d Dimensoes, x, y int) bool {
	return profundidade(x, y, d.W, d.H, d.Radius) >= BordaPilula+1
}

// caixaDaLinha é o retângulo da tinta da linha i de m, em coordenadas da
// pílula.
func caixaDaLinha(m Mensagem, i int) image.Rectangle {
	return tipo().tinta(m.Linhas[i]).Add(image.Pt(m.TextoX, m.Bases[i]))
}

func caixaDoPonto(m Mensagem) image.Rectangle {
	return image.Rect(m.SinalX, m.SinalY, m.SinalX+SinalDiametro, m.SinalY+SinalDiametro)
}

// TestComporMensagem_NadaFicaNaBorda: a tinta de cada linha — com acento
// em cima e perna do "ç" e do "g" embaixo — e o ponto de estado ficam no
// corpo da pílula, com folga até o contorno. A pílula é convexa: se os
// quatro cantos de uma caixa estão dentro, a caixa inteira está.
func TestComporMensagem_NadaFicaNaBorda(t *testing.T) {
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		caixas := map[string]image.Rectangle{}
		for i := range m.Linhas {
			caixas["linha "+m.Linhas[i]] = caixaDaLinha(m, i)
		}
		if m.TemSinal {
			caixas["ponto"] = caixaDoPonto(m)
		}
		for nome, k := range caixas {
			if k.Empty() {
				t.Errorf("%s: %s sem tinta", nomeDaFase(p), nome)
				continue
			}
			for _, q := range []image.Point{k.Min, {k.Max.X - 1, k.Min.Y}, {k.Min.X, k.Max.Y - 1}, k.Max.Sub(image.Pt(1, 1))} {
				if !miolo(m.Dim, q.X, q.Y) {
					t.Errorf("%s: %s ocupa %v e o canto (%d,%d) cai no contorno ou fora da pílula %dx%d",
						nomeDaFase(p), nome, k, q.X, q.Y, m.Dim.W, m.Dim.H)
				}
			}
		}
	}
}

// TestComporMensagem_LinhasNaoSeTocam: a perna do "ç"/"g"/"p" de uma linha
// e o acento da de baixo ficam separados por pelo menos 1 px.
func TestComporMensagem_LinhasNaoSeTocam(t *testing.T) {
	duas := 0
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		for i := 0; i+1 < len(m.Linhas); i++ {
			duas++
			de, ate := caixaDaLinha(m, i), caixaDaLinha(m, i+1)
			if de.Max.Y >= ate.Min.Y {
				t.Errorf("%s: a linha %q desce até y=%d e a %q começa em y=%d", nomeDaFase(p),
					m.Linhas[i], de.Max.Y-1, m.Linhas[i+1], ate.Min.Y)
			}
		}
	}
	if duas == 0 {
		t.Fatal("nenhuma mensagem de duas linhas — o teste não prova nada")
	}
}

// TestComporMensagem_CentradaEPontoAbreAPrimeiraLinha trava a composição:
//
//  1. o bloco (ponto + vão + linha mais larga) é centrado na horizontal;
//  2. a altura óptica do bloco — do meio da maiúscula/minúscula da
//     primeira linha até a base da última — é centrada na vertical;
//  3. o ponto de estado é centrado na altura óptica da PRIMEIRA linha,
//     como o sinal do .alert do design system.
//
// Meio pixel de tolerância: as posições são inteiras.
func TestComporMensagem_CentradaEPontoAbreAPrimeiraLinha(t *testing.T) {
	optica := tipo().alturaOptica()
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		esquerda := m.TextoX
		if m.TemSinal {
			esquerda = m.SinalX
		}
		larguraTexto := 0
		for _, l := range m.Linhas {
			larguraTexto = max(larguraTexto, tipo().largura(l))
		}
		if dir := m.Dim.W - (m.TextoX + larguraTexto); abs(esquerda-dir) > 1 {
			t.Errorf("%s: %d px de margem à esquerda e %d à direita", nomeDaFase(p), esquerda, dir)
		}

		topo := float64(m.Bases[0]) - optica
		base := float64(m.Bases[len(m.Bases)-1])
		if meio := (topo + base) / 2; math.Abs(meio-float64(m.Dim.H)/2) > 0.5 {
			t.Errorf("%s: o texto tem o centro óptico em y=%.2f, a pílula em %.1f", nomeDaFase(p), meio, float64(m.Dim.H)/2)
		}

		if m.TemSinal {
			centro := float64(m.SinalY) + float64(SinalDiametro)/2
			if linha := float64(m.Bases[0]) - optica/2; math.Abs(centro-linha) > 0.5 {
				t.Errorf("%s: o ponto tem o centro em y=%.2f e a primeira linha em y=%.2f", nomeDaFase(p), centro, linha)
			}
			if vao := m.TextoX - (m.SinalX + SinalDiametro); vao != SinalVao {
				t.Errorf("%s: %d px entre o ponto e o texto, want %d", nomeDaFase(p), vao, SinalVao)
			}
		}
	}
}

// --- o desenho, ligado à composição ----------------------------------------

// fracaoDeTinta diz se cor é o corpo da pílula misturado com o off-white
// do texto — é o que o rasterizador produz, pixel a pixel, na fração que a
// letra cobre — e em que fração. Tolera 2 por canal, o arredondamento da
// mistura.
func fracaoDeTinta(cor Cor) (float64, bool) {
	f := (float64(cor.G) - float64(corCorpo.G)) / (float64(corTexto.G) - float64(corCorpo.G))
	if f <= 0 || f > 1.01 {
		return 0, false
	}
	perto := func(v, de, ate byte) bool {
		return math.Abs(float64(v)-(float64(de)+f*(float64(ate)-float64(de)))) <= 2
	}
	return f, perto(cor.R, corCorpo.R, corTexto.R) && perto(cor.B, corCorpo.B, corTexto.B)
}

// ehCorDeTinta: tinta de verdade, com pelo menos metade do off-white — as
// bordas de pouca cobertura ficam de fora, para a borda suavizada do ponto
// cinza não passar por texto.
func ehCorDeTinta(cor Cor) bool {
	f, ok := fracaoDeTinta(cor)
	return ok && f >= 0.5
}

// contaTinta conta os pixels opacos de c que são tinta de texto.
func contaTinta(c *Canvas) int {
	n := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if b, g, r, a := c.At(x, y); a == 255 && ehCorDeTinta(Cor{R: r, G: g, B: b}) {
				n++
			}
		}
	}
	return n
}

// soAPilula é o frame de p sem a mensagem: só o corpo e o contorno, no
// mesmo lugar. A diferença entre ele e o frame inteiro é exatamente o que
// desenhaMensagem pintou.
func soAPilula(p dictation.OverlayPhase) *Canvas {
	c := canvasDeTeste()
	d := DimensoesPara(p)
	px, py := cantoNoCanvasDeTeste(d)
	desenhaPilula(c, px, py, d)
	return c
}

// TestRender_MensagemFicaOndeAComposicaoDiz liga o desenho à composição:
// todo pixel que a mensagem pinta cai dentro da caixa do ponto ou da
// tinta de uma das linhas (ComporMensagem), e no corpo da pílula, com
// folga até o contorno. É o "nada de texto cortado" medido no frame — o
// canvas dos testes tem folga em volta da pílula, então uma letra que
// saísse dela apareceria ali em vez de ser recortada.
//
// E cada parte pintou alguma coisa: toda linha tem tinta, e o ponto tem a
// cor exata do sinal.
func TestRender_MensagemFicaOndeAComposicaoDiz(t *testing.T) {
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		px, py := cantoNoCanvasDeTeste(m.Dim)
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p})
		fundo := soAPilula(p)

		caixas := []image.Rectangle{}
		for i := range m.Linhas {
			caixas = append(caixas, caixaDaLinha(m, i))
		}
		ponto := caixaDoPonto(m)
		pintados := make([]int, len(caixas))
		extensao := make([]image.Rectangle, len(caixas))
		sinalExato := 0
		for y := 0; y < c.H; y++ {
			for x := 0; x < c.W; x++ {
				ib := (y*c.W + x) * 4
				if string(c.Pix[ib:ib+4]) == string(fundo.Pix[ib:ib+4]) {
					continue
				}
				q := image.Pt(x-px, y-py)
				if !miolo(m.Dim, q.X, q.Y) {
					t.Fatalf("%s: a mensagem pintou (%d,%d), fora do corpo da pílula", nomeDaFase(p), q.X, q.Y)
				}
				dentro := m.TemSinal && q.In(ponto)
				if dentro {
					if b, g, r, _ := c.At(x, y); (Cor{R: r, G: g, B: b}) == m.Sinal {
						sinalExato++
					}
				}
				for i, k := range caixas {
					if q.In(k) {
						pintados[i]++
						extensao[i] = extensao[i].Union(image.Rect(q.X, q.Y, q.X+1, q.Y+1))
						dentro = true
					}
				}
				if !dentro {
					t.Fatalf("%s: a mensagem pintou (%d,%d), fora do ponto %v e das linhas %v",
						nomeDaFase(p), q.X, q.Y, ponto, caixas)
				}
			}
		}
		for i, n := range pintados {
			if n == 0 {
				t.Errorf("%s: a linha %q não pintou nada", nomeDaFase(p), m.Linhas[i])
				continue
			}
			// E pintou a caixa inteira, não um canto dela: a tinta desenhada
			// vai de ponta a ponta da caixa, com 1 px de tolerância em cada
			// lado (a borda da caixa é arredondada para fora). Letras
			// empilhadas no mesmo lugar, ou uma linha cortada no meio, ficam
			// muito aquém.
			k, e := caixas[i], extensao[i]
			if e.Min.X-k.Min.X > 1 || k.Max.X-e.Max.X > 1 || e.Min.Y-k.Min.Y > 1 || k.Max.Y-e.Max.Y > 1 {
				t.Errorf("%s: a linha %q pintou %v, a caixa da tinta dela é %v", nomeDaFase(p), m.Linhas[i], e, k)
			}
		}
		if m.TemSinal && sinalExato == 0 {
			t.Errorf("%s: o ponto não tem nenhum pixel na cor %v", nomeDaFase(p), m.Sinal)
		}
	}
}

// TestRender_TextoEhOffWhiteSobreOCorpo: todo pixel que o texto pinta é o
// corpo misturado com o off-white do design system — nada de outra cor —,
// o traço chega ao off-white cheio em algum lugar de cada linha, e a
// borda da letra é suavizada: há pixels no meio do caminho. Um texto
// desenhado a meia opacidade, na cor errada ou em serrilhado binário falha
// aqui.
func TestRender_TextoEhOffWhiteSobreOCorpo(t *testing.T) {
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		px, py := cantoNoCanvasDeTeste(m.Dim)
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p})
		ponto := caixaDoPonto(m)
		for i := range m.Linhas {
			k := caixaDaLinha(m, i)
			maisForte, suaves := 0.0, 0
			for y := k.Min.Y; y < k.Max.Y; y++ {
				for x := k.Min.X; x < k.Max.X; x++ {
					if m.TemSinal && image.Pt(x, y).In(ponto) {
						continue
					}
					b, g, r, a := c.At(px+x, py+y)
					cor := Cor{R: r, G: g, B: b}
					if cor == corCorpo {
						continue
					}
					f, ok := fracaoDeTinta(cor)
					if a != 255 || !ok {
						t.Fatalf("%s: (%d,%d) na linha %q = %v alpha %d — não é off-white sobre o corpo",
							nomeDaFase(p), x, y, m.Linhas[i], cor, a)
					}
					maisForte = max(maisForte, f)
					if f > 0.1 && f < 0.9 {
						suaves++
					}
				}
			}
			if maisForte < 0.95 {
				t.Errorf("%s: a linha %q chega a só %.0f%% do off-white", nomeDaFase(p), m.Linhas[i], 100*maisForte)
			}
			if suaves == 0 {
				t.Errorf("%s: a linha %q não tem borda suavizada", nomeDaFase(p), m.Linhas[i])
			}
		}
	}
}

// TestRender_PontoDeEstadoEhRedondo: o ponto de 8 px tem borda suavizada
// sobre o corpo — binário, ele é um octógono. Ocupa exatamente o quadrado
// SinalDiametro da composição, com o miolo na cor exata do sinal e a borda
// misturada com o corpo.
func TestRender_PontoDeEstadoEhRedondo(t *testing.T) {
	for _, p := range fasesComMensagem() {
		m := ComporMensagem(p)
		if !m.TemSinal {
			continue
		}
		px, py := cantoNoCanvasDeTeste(m.Dim)
		c := canvasDeTeste()
		Render(c, FrameState{Phase: p})

		// O que não é corpo em volta do ponto, com 2 px de sobra para cada
		// lado (menos do que o vão até o texto).
		ocupa := image.Rectangle{}
		exatos, misturados := 0, 0
		for y := m.SinalY - 2; y < m.SinalY+SinalDiametro+2; y++ {
			for x := m.SinalX - 2; x < m.SinalX+SinalDiametro+2; x++ {
				b, g, r, _ := c.At(px+x, py+y)
				switch cor := (Cor{R: r, G: g, B: b}); cor {
				case corCorpo:
					continue
				case m.Sinal:
					exatos++
				default:
					misturados++
				}
				ocupa = ocupa.Union(image.Rect(x, y, x+1, y+1))
			}
		}
		if ocupa != caixaDoPonto(m) {
			t.Errorf("%s: o ponto ocupa %v, want %v", nomeDaFase(p), ocupa, caixaDoPonto(m))
		}
		if exatos == 0 || misturados == 0 {
			t.Errorf("%s: ponto com %d pixels na cor exata e %d misturados — want os dois", nomeDaFase(p), exatos, misturados)
		}
	}
}
