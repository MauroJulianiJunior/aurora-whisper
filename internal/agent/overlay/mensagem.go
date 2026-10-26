package overlay

import (
	"math"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// Este arquivo não tem build tag: é a COMPOSIÇÃO da pílula de mensagem —
// onde quebrar a linha, que largura e altura a pílula tem, onde ficam o
// ponto de estado, cada linha e a linha de base dela. Decidida inteira
// antes de desenhar, e testável sem Windows (mensagem_test.go).
//
// # Por que a mensagem mudou em 29-set-2026
//
// Até ali a pílula de mensagem era fixa em 300x56, com a fonte de bitmap
// na escala 2, e o texto quebrado de forma gulosa: enche a primeira linha
// e joga o resto na próxima. Olhando a pílula compacta aprovada (128x20)
// ao lado das mensagens, Mauro: "as mensagens ainda estão muito grandes.
// Pode distribuir melhor a largura delas e diminuir uns 30% do tamanho pra
// ficar mais compatível com a pílula."
//
// Três coisas mudaram juntas:
//
//  1. A fonte é a IBM Plex Mono do design system, a 13 px (tipografia.go):
//     ~35% menor que o bitmap, e legível nesse tamanho.
//  2. A pílula tem o tamanho do TEXTO, entre MensagemLarguraMin e
//     MensagemLarguraMax: "Inserido" é uma cápsula curta, e a mensagem
//     longa não arrasta mais as curtas para a largura dela.
//  3. A quebra é EQUILIBRADA (QuebraEquilibrada): as duas linhas saem com
//     larguras parecidas, em vez de uma cheia e outra com uma palavra
//     ("Não consegui inserir o" / "texto."). Isso também é o que deixa a
//     pílula de duas linhas mais estreita: a largura é a da linha maior,
//     e o equilíbrio é o que a faz a menor possível.
const (
	// PillRadius é o raio da pílula de mensagem: o radius-lg do design
	// system (os raios dele são 6, 10, 16, 24 e pílula). Com uma linha, a
	// pílula tem 32 px de altura e o raio de 16 é a cápsula inteira; com
	// duas, é um retângulo de cantos redondos com o mesmo raio.
	PillRadius = 16

	// MensagemAlturaUmaLinha é a altura da pílula de uma linha, e
	// LinhaPasso o quanto cada linha a mais acrescenta — a distância entre
	// as linhas de base. Uma linha: 32 px; duas: 48. A de antes tinha 56
	// para qualquer mensagem.
	//
	// O passo de 16 px é 1,23 vezes o corpo de 13: o "ç" e o "g" de uma
	// linha e o acento da de baixo não se encostam
	// (TestComporMensagem_LinhasNaoSeTocam).
	MensagemAlturaUmaLinha = 32
	LinhaPasso             = 16

	// MaxLinhas é em quantas linhas uma mensagem pode ser quebrada. Eram
	// três na fonte de bitmap — e a terceira linha foi a que Mauro achou
	// grande demais. Com a Plex Mono, todas cabem em duas.
	MaxLinhas = 2

	// MensagemMargemX é o respiro, em cada lado, entre a borda da pílula e
	// o bloco (ponto + texto). Com 12, o ponto de 8 px fica com o centro a
	// 16 px da borda — o centro da curva da cápsula: o ponto é concêntrico
	// com a ponta da pílula de uma linha.
	MensagemMargemX = 12

	// MensagemLarguraMin é a largura mínima: a da pílula compacta. A
	// pílula que diz "Inserido" sai com a mesma largura da onda que ela
	// substitui e só cresce na altura.
	//
	// MensagemLarguraMax é a máxima, e decide o que cabe numa linha só: a
	// 8 px por letra, 30 letras (ver larguraUtil). O que passa disso vai
	// para duas linhas equilibradas. É menor que os 300 px de antes.
	MensagemLarguraMin = CompactWidth
	MensagemLarguraMax = 280

	// SinalDiametro/SinalVao são o ponto de estado que abre toda mensagem
	// (ver sinalPara, em paleta.go) e o respiro entre ele e o texto — o
	// componente "status" do design system, que usa ponto de 8 px ao lado
	// de texto de 11 px. O vão caiu de 8 para 6 junto com a fonte.
	SinalDiametro = 8
	SinalVao      = 6
)

// Mensagem é a pílula de mensagem composta: o tamanho dela e onde vai cada
// coisa, em coordenadas da PÍLULA (o canto superior esquerdo dela é 0,0).
type Mensagem struct {
	Dim    Dimensoes
	Linhas []string

	// TextoX é onde a caneta começa cada linha, e Bases é a linha de base
	// de cada uma. As linhas são alinhadas à esquerda, como no .alert do
	// design system.
	TextoX int
	Bases  []int

	// Sinal é a cor do ponto de estado, e SinalX/SinalY o canto superior
	// esquerdo dele. TemSinal é falso só nas fases sem desfecho.
	Sinal          Cor
	TemSinal       bool
	SinalX, SinalY int
}

// alturaMensagem é a altura da pílula com n linhas (pelo menos uma).
func alturaMensagem(n int) int {
	return MensagemAlturaUmaLinha + (max(n, 1)-1)*LinhaPasso
}

// larguraUtil é quanto de texto cabe numa linha da pílula mais larga: a
// largura máxima menos as margens e, se houver, o ponto e o vão dele.
func larguraUtil(temSinal bool) int {
	w := MensagemLarguraMax - 2*MensagemMargemX
	if temSinal {
		w -= SinalDiametro + SinalVao
	}
	return w
}

// ComporMensagem decide a pílula de mensagem de phase: quebra o texto
// (Label) em até MaxLinhas linhas equilibradas, dimensiona a pílula pelo
// bloco de ponto + texto e posiciona cada parte.
//
// # A vertical
//
// A linha de base é posta para que a ALTURA ÓPTICA do texto (a média das
// alturas de maiúscula e minúscula, ver tipografia.alturaOptica) fique
// centrada na pílula de uma linha. Com duas linhas, a pílula cresce
// exatamente um LinhaPasso e o bloco continua centrado: a primeira base
// fica no mesmo lugar, e a segunda um passo abaixo.
//
// O ponto de estado é centrado na altura óptica da PRIMEIRA linha — como
// o sinal do .alert do design system, que abre o texto em cima. Na pílula
// de uma linha isso é o centro da pílula.
//
// # A horizontal
//
// O bloco (ponto + vão + linha mais larga) é centrado na pílula. Quando a
// pílula está na largura do bloco, as margens são MensagemMargemX; quando
// a largura mínima manda, sobra dos dois lados por igual.
func ComporMensagem(phase dictation.OverlayPhase) Mensagem {
	return ComporMensagemComTexto(phase, "")
}

// ComporMensagemComTexto é ComporMensagem com o texto dado no lugar do Label da
// fase — o "Lista de compras +3" do ditar para nota (FaseNaNota). Texto vazio é
// o Label. O texto passa pela mesma quebra e pelo mesmo corte (até MaxLinhas,
// com "…"), então nenhum texto sai da pílula.
func ComporMensagemComTexto(phase dictation.OverlayPhase, texto string) Mensagem {
	if texto == "" {
		texto = Label(phase)
	}
	t := tipo()
	sinal, temSinal := sinalPara(phase)
	linhas := QuebraEquilibrada(texto, t.largura, larguraUtil(temSinal))

	larguraTexto := 0
	for _, l := range linhas {
		larguraTexto = max(larguraTexto, t.largura(l))
	}
	bloco := larguraTexto
	if temSinal {
		bloco += SinalDiametro + SinalVao
	}
	// Não passa de MensagemLarguraMax: cada linha cabe em larguraUtil, que
	// já é a máxima menos as margens e o ponto.
	w := max(MensagemLarguraMin, bloco+2*MensagemMargemX)
	h := alturaMensagem(len(linhas))

	optica := t.alturaOptica()
	base := int(math.Round(float64(MensagemAlturaUmaLinha)/2 + optica/2))

	m := Mensagem{
		Dim:      Dimensoes{W: w, H: h, Radius: PillRadius},
		Linhas:   linhas,
		TextoX:   (w - bloco) / 2,
		Sinal:    sinal,
		TemSinal: temSinal,
	}
	for i := range linhas {
		m.Bases = append(m.Bases, base+i*LinhaPasso)
	}
	if temSinal {
		m.SinalX = m.TextoX
		m.SinalY = int(math.Round(float64(base) - optica/2 - float64(SinalDiametro)/2))
		m.TextoX += SinalDiametro + SinalVao
	}
	return m
}

// QuebraEquilibrada quebra texto em até MaxLinhas linhas de no máximo
// maxLargura pixels cada, medidas por largura, sempre em espaço — nunca no
// meio de uma palavra.
//
// Cabendo numa linha, fica numa linha. Se não, entre todas as quebras em
// que as duas linhas cabem, escolhe a que deixa a MAIOR linha o mais
// curta possível: as duas saem parecidas, e a pílula, que tem a largura da
// maior, sai a mais estreita possível. Em empate, a de primeira linha mais
// longa, para o bloco não parecer uma pirâmide de ponta-cabeça sob o
// ponto de estado.
//
// Empate só acontece entre uma quebra com a maior linha em cima e outra
// com ela embaixo — com a largura crescendo a cada palavra, duas quebras
// não têm a mesma primeira linha nem a mesma segunda —, e aí as duas
// diferenças são iguais. Por isso não há desempate por "mais parecidas":
// ele nunca decidiria nada.
//
// A quebra gulosa de antes enchia a primeira linha e jogava o resto na
// segunda: "Não consegui inserir o" / "texto.".
//
// Se nem em duas linhas couber — nenhuma mensagem da spec chega perto
// disso, e TestComporMensagem_TodaMensagemCabeInteira existe para nunca
// chegar —, a primeira linha leva as palavras que couberem e a segunda o
// resto, cortado com "…" (truncaNaLargura). Perder a cauda é ruim, mas
// menos ruim que o texto sair pela borda da pílula em silêncio.
func QuebraEquilibrada(texto string, largura func(string) int, maxLargura int) []string {
	palavras := strings.Fields(texto)
	if len(palavras) == 0 || maxLargura <= 0 {
		return nil
	}
	inteiro := strings.Join(palavras, " ")
	if largura(inteiro) <= maxLargura {
		return []string{inteiro}
	}
	if len(palavras) == 1 {
		return []string{truncaNaLargura(inteiro, largura, maxLargura)}
	}

	var melhor []string
	melhorMaior, melhorPrimeira := 0, 0
	for k := 1; k < len(palavras); k++ {
		a := strings.Join(palavras[:k], " ")
		b := strings.Join(palavras[k:], " ")
		la, lb := largura(a), largura(b)
		if la > maxLargura || lb > maxLargura {
			continue
		}
		if maior := max(la, lb); melhor == nil || maior < melhorMaior || (maior == melhorMaior && la > melhorPrimeira) {
			melhor = []string{a, b}
			melhorMaior, melhorPrimeira = maior, la
		}
	}
	if melhor != nil {
		return melhor
	}

	// Não cabe em duas: a primeira leva o que couber, a segunda o resto.
	k := 1
	for k < len(palavras) && largura(strings.Join(palavras[:k+1], " ")) <= maxLargura {
		k++
	}
	return []string{
		truncaNaLargura(strings.Join(palavras[:k], " "), largura, maxLargura),
		truncaNaLargura(strings.Join(palavras[k:], " "), largura, maxLargura),
	}
}

// truncaNaLargura encurta s até caber em maxLargura pixels, trocando a
// cauda por um único "…". Conta runas, não bytes: "ã" não é cortado ao
// meio.
func truncaNaLargura(s string, largura func(string) int, maxLargura int) string {
	if largura(s) <= maxLargura {
		return s
	}
	r := []rune(s)
	for n := len(r) - 1; n > 0; n-- {
		if c := strings.TrimRight(string(r[:n]), " ") + "…"; largura(c) <= maxLargura {
			return c
		}
	}
	if largura("…") <= maxLargura {
		return "…"
	}
	return ""
}
