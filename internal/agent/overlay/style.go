package overlay

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"

// Este arquivo não tem build tag de propósito.
//
// O estilo da janela é DADO — uma máscara de bits —, não chamada de
// sistema. Deixá-lo dentro de window_windows.go colocava a decisão
// inteira atrás de `//go:build windows`, onde nenhum teste da suíte
// chega. E foi exatamente ali que o defeito de 26-set-2026 morou: a
// pílula era criada, desenhada e mostrada, e ficava ATRÁS da janela em
// foco porque faltava um bit.
//
// É a mesma lição do CLAUDE.md sobre testes que agem no sistema real:
// "a lógica de decisão é pura e vai sem build tag".

// overlayExStyle é o conjunto de WS_EX_* da janela do overlay.
//
// Cada bit está aqui por um motivo:
//
//   - Layered: exigido por UpdateLayeredWindow, que é o que permite
//     alpha por pixel (cantos arredondados de verdade sobre o que
//     estiver embaixo). SetLayeredWindowAttributes não faria isso.
//   - Transparent: cliques atravessam a pílula. Ela cobre o canto
//     inferior da tela durante o ditado e não pode roubar um clique.
//   - ToolWindow: fora do Alt+Tab e da barra de tarefas. É um HUD, não
//     uma janela de aplicativo.
//   - NoActivate: nunca rouba o foco do teclado. Isto é crítico, não
//     cosmético — o ditado insere texto no campo que TEM o foco, e um
//     overlay que ativasse a si mesmo destruiria o alvo da inserção.
//   - Topmost: fica acima da janela em foco.
//
// Topmost entrou em 26-set-2026. Sem ele, NoActivate + SW_SHOWNOACTIVATE
// garantem que a janela nunca suba, então a pílula era desenhada atrás
// de qualquer coisa que cobrisse o centro inferior da tela. Funcionava
// quando a área estava descoberta, o que fez o defeito passar por dois
// roteiros de teste manual antes de Mauro reportar "eu ouço o som mas
// não vejo nada na tela".
//
// O bit sozinho não garante o topo: dentro da faixa das janelas topmost
// a ordem é de quem subiu por último, e a pílula é criada uma vez, na
// subida do Agent. Por isso, a cada vez que é mostrada, ela reafirma o
// topo (30-set-2026; ver comoMostrar).
const overlayExStyle = win32.WSExLayered |
	win32.WSExTransparent |
	win32.WSExToolWindow |
	win32.WSExNoActivate |
	win32.WSExTopmost

const (
	// SW_HIDE e SW_SHOWNOACTIVATE (winuser.h): win32.ShowWindow recebe o
	// cmdShow cru. SW_SHOWNOACTIVATE mostra a janela no tamanho e na
	// posição atuais SEM ativá-la; SW_SHOW e SW_SHOWNORMAL a ativariam.
	swHide           = 0
	swShowNoActivate = 4
)

// aoMostrar é o que se pede ao Windows para mostrar a pílula.
type aoMostrar struct {
	// CmdShow é o cmdShow de ShowWindow.
	CmdShow int32
	// InsertAfter e Flags são os de SetWindowPos, chamada com x, y, cx e cy
	// zerados (as flags de Move e Size os ignoram).
	InsertAfter win32.HWND
	Flags       uint32
}

// comoMostrar é a decisão de mostrar a pílula: sem ativar, e reafirmando o
// topo.
//
// # Por que reafirmar
//
// WS_EX_TOPMOST põe a janela na faixa das topmost, mas DENTRO da faixa o
// que vale é quem subiu por último. A pílula nasce uma vez, na subida do
// Agent, e uma janela topmost que apareça depois (a de qualquer programa
// comum com "sempre visível") fica acima dela, mesmo com o bit.
//
// O Gerenciador de Tarefas NÃO é exemplo disto, e não serve de teste: com
// "Sempre visível" ele fica numa faixa própria do Windows, acima das topmost
// comuns (descrito por terceiros, não pela Microsoft; não verificado aqui), e
// uma janela de programa comum não passa dele com SetWindowPos. Para o teste
// manual use uma janela topmost comum (docs/teste-manual-instalador.md, I6).
//
// Mauro relatou em 30-set-2026 a pílula não aparecendo sobre uma janela em
// primeiro plano, num build antigo; no build novo apareceu. A causa daquele
// relato não foi estabelecida; esta chamada é a defesa barata contra a
// hipótese (outra janela topmost que subiu depois): além de ShowWindow, ao
// mostrar, SetWindowPos(HWND_TOPMOST), que leva a pílula ao alto da faixa.
//
// É uma chamada, sem ativar nada e sem mexer em posição nem em tamanho
// (quem decide os dois é o UpdateLayeredWindow, no mesmo quadro).
// SetVisible(true) corre a cada quadro da gravação, e a reafirmação vai
// junto: sobre uma pílula que já está no alto ela não muda nada, e sobre
// uma que ficou atrás de outra topmost ela a recoloca.
//
// # O que não pode ceder
//
// Esta decisão é pura e sem build tag, como o overlayExStyle, porque é
// onde mora o risco: qualquer flag a menos aqui ou rouba o foco do campo
// de texto (SWPNoActivate, e o SW_SHOWNOACTIVATE do ShowWindow), ou deixa
// a chamada sem efeito (SWPNoZOrder, que faz o SetWindowPos ignorar o
// HWND_TOPMOST), ou move a pílula de onde o UpdateLayeredWindow a pôs
// (SWPNoMove e SWPNoSize). O ditado insere no campo que TEM o foco: uma
// pílula que o ativasse levaria o foco embora.
func comoMostrar() aoMostrar {
	return aoMostrar{
		CmdShow:     swShowNoActivate,
		InsertAfter: win32.HWNDTopmost,
		Flags: win32.SWPNoMove |
			win32.SWPNoSize |
			win32.SWPNoActivate |
			win32.SWPNoOwnerZOrder |
			win32.SWPShowWindow,
	}
}
