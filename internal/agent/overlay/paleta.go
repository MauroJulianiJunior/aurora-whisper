package overlay

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

// Este arquivo não tem build tag: é dado (cores) e uma decisão pura (qual
// sinal cada fase mostra), testáveis sem Windows — ver paleta_test.go.

// # A paleta é a do projeto
//
// Decisão de 29-set-2026: a paleta do projeto (chamada de DS, o design
// system, nos comentários deste pacote) vale para tudo — cores, fontes,
// detalhes visuais —, e não inclui logo nem nome pessoal de ninguém. Até
// então a pílula era cinza sobre quase-preto, paleta provisória do
// ADR-010, D2 que nunca tinha sido trocada.
//
// As regras do DS que decidem cada escolha abaixo:
//
//   - verde (#00E676) não é decoração: sinaliza capacidade, ação ou
//     estado válido;
//   - vermelho (#FF3B3B) só para risco, erro ou revisão humana, nunca
//     decorativo, e nunca sozinho — sempre com texto junto;
//   - off-white (#F5F7F5) no lugar de branco puro;
//   - cinza (#9AA4A0) é o "process" do DS: neutro, em curso.
//
// Cor é sRGB opaca, na ordem que se lê num hex. O Canvas escreve B,G,R
// (canvas.go); a troca de ordem mora só em bgr, para ninguém inverter
// canal à mão.
type Cor struct{ R, G, B byte }

func (c Cor) bgr() (b, g, r byte) { return c.B, c.G, c.R }

var (
	// corCorpo é o corpo da pílula: o voidBlack do DS (#070A09), com o
	// contorno de 1 px em borderDark (corBorda) que o DS põe em toda
	// superfície que flutua sobre outra.
	//
	// A alternativa era o surfaceRaised (#111916). Decidiu a conta de
	// contraste sobre o que fica ATRÁS da pílula, que é o papel de parede
	// ou o aplicativo de quem usa — medida em
	// TestPaleta_CorpoEscolhidoPeloContraste:
	//
	//   - sobre fundo claro não decide: os dois passam de 15:1;
	//   - sobre o cinza-escuro do modo escuro do Windows e dos aplicativos
	//     (#1F1F1F a #2B2B2B), o voidBlack recorta mais: 1,21 a 1,40:1,
	//     contra 1,08 a 1,26:1 do surfaceRaised;
	//   - sobre preto puro nenhum corpo aparece (1,06 e 1,17:1), e quem
	//     recorta é o contorno, 1,57:1 nos dois casos;
	//   - o contorno se lê mais como contorno sobre o corpo mais escuro
	//     (1,49:1 contra 1,34:1), e o texto e as barras ganham um pouco.
	//
	// Nenhum desses números é alto: fundo escuro atrás de pílula escura é
	// o caso difícil, e a pílula quase preta aprovada antes (#202020) sumia
	// do mesmo jeito. O que tem de ser lido — onda, ponto e texto — tem
	// contraste de sobra contra o corpo (TestPaleta_ContrasteSobreAPilula).
	corCorpo = Cor{0x07, 0x0A, 0x09}
	corBorda = Cor{0x24, 0x32, 0x2D}

	// corTexto é o off-white do DS. Branco puro saiu da pílula.
	corTexto = Cor{0xF5, 0xF7, 0xF5}

	// corVerde é o systemGreen: a barra que recebe sinal medido e o ponto
	// de "Inserido".
	corVerde = Cor{0x00, 0xE6, 0x76}

	// corCinza é o steelGray: a barra em repouso, a onda parada do
	// processamento e o ponto da área de transferência.
	//
	// O repouso poderia ser o verde profundo (#0F3D2E), que deixaria a
	// onda inteira num tom só. Não é, por conta: sobre corCorpo ele dá
	// 1,6:1, abaixo dos 3:1 que o WCAG 1.4.11 pede para elemento gráfico
	// que informa. A onda em silêncio viraria uma pílula vazia — e pílula
	// vazia se lê como defeito, não como "estou ouvindo e não há som". O
	// cinza dá 7,7:1.
	//
	// E o cinza diz a coisa certa: a barra só fica verde quando há sinal
	// MEDIDO chegando. Silêncio é neutro. Verde vira informação, que é o
	// que o DS exige dele — e é o princípio do produto de não afirmar o
	// que não foi medido.
	corCinza = Cor{0x9A, 0xA4, 0xA0}

	// corVermelho é o machineRed, só nos desfechos em que algo deu errado
	// ou foi recusado (ver sinalPara). Sempre ponto, nunca fundo nem
	// texto: o texto continua off-white e é ele que diz o que houve.
	corVermelho = Cor{0xFF, 0x3B, 0x3B}
)

// sinalPara devolve a cor do ponto de estado que acompanha a mensagem de
// phase, e false quando a fase não tem mensagem (gravação, processamento
// e hidden).
//
// É o componente "status" do DS — um ponto e o texto do lado —, e o
// ponto nunca está sozinho: toda fase com sinal tem texto (Label), e é o
// texto que se lê. A cor é a segunda codificação, para quem bate o olho.
//
//   - Inserido: verde, estado válido.
//   - Área de transferência: cinza. O ditado não se perdeu, então não é
//     erro; mas também NÃO foi inserido, e verde ali diria que foi — a
//     pessoa bateria o olho no verde e não colaria.
//   - Texto que chegou depois do prazo e ficou no histórico
//     (FaseTextoNoHistorico): cinza, pelo mesmo motivo.
//   - Repetição removida (FaseRepeticaoRemovida): verde. O texto entrou;
//     a mensagem pede para conferir.
//   - Erro, campo protegido, janela de administrador, falha de inserção:
//     vermelho. É o uso que o DS reserva para ele: algo falhou ou foi
//     recusado e pede atenção.
//
// A janela de administrador (FaseJanelaElevada) é um bloqueio, e tem o
// ponto do bloqueio. Ela ficou sem ponto nenhum de 29-set-2026 até a
// mudança das mensagens, no mesmo dia: a fase e o ponto de estado
// entraram em frentes paralelas, e os testes do ponto percorriam só as
// fases do núcleo (todasAsFases), de onde ela está fora de propósito. Os
// testes de mensagem percorrem fasesDoOverlay desde então.
func sinalPara(phase dictation.OverlayPhase) (Cor, bool) {
	switch phase {
	case dictation.OverlaySuccess, FaseRepeticaoRemovida, FaseNaNota, FaseNotaNova:
		return corVerde, true
	case dictation.OverlayClipboard, FaseTextoNoHistorico, FaseNotaNaoSalvou:
		return corCinza, true
	case dictation.OverlayError, dictation.OverlayBlocked, dictation.OverlayInsertFailed, FaseJanelaElevada,
		FaseMicrofoneBloqueado:
		return corVermelho, true
	default:
		return Cor{}, false
	}
}
