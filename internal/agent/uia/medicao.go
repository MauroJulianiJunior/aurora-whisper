package uia

// Este arquivo não tem build tag: é só a contabilidade de QUAL chamada de
// uma inspeção foi a mais lenta. Quem cronometra cada chamada de verdade é
// automation_windows.go; aqui só se decide o que guardar.

import "time"

// ChamadaUIA nomeia uma das chamadas que uma inspeção de foco faz, para o
// diagnóstico dizer qual delas foi a mais lenta.
//
// # Por que existe
//
// Em 29-set-2026 o log mostrou duas inspeções de um minuto cada
// (duracao=1m0.03s e 1m0.10s), terminadas em outcome=timeout. Só que
// timeout só sai DEPOIS de GetFocusedElement, ControlType e IsPassword
// terem respondido — então o minuto pode ter ido em qualquer uma delas ou
// nas leituras de melhor esforço que vêm depois, e o código não media
// nenhuma. Sem saber qual, não há como escolher entre enxugar a inspeção
// e mudar de mecanismo.
//
// # Por que um tipo, e não uma string
//
// Mesmo motivo de FalhaEm: um valor enumerado não consegue carregar o
// nome da janela nem por acidente. O conjunto inteiro de nomes possíveis
// está escrito em String().
type ChamadaUIA uint8

const (
	// ChamadaNenhuma: nenhuma chamada foi cronometrada. Não sai de uma
	// inspeção de verdade — GetFocusedElement é sempre chamada —, mas é o
	// zero value.
	ChamadaNenhuma ChamadaUIA = iota
	// ChamadaGetFocusedElement: IUIAutomation::GetFocusedElement.
	ChamadaGetFocusedElement
	// ChamadaControlType: get_CurrentControlType.
	ChamadaControlType
	// ChamadaIsPassword: get_CurrentIsPassword.
	ChamadaIsPassword
	// ChamadaProcessID: get_CurrentProcessId.
	ChamadaProcessID
	// ChamadaNomeDoProcesso: OpenProcess + QueryFullProcessImageNameW. É
	// Win32, não UIA, e não espera a thread de interface de outro processo
	// — está aqui para que isso deixe de ser suposição.
	ChamadaNomeDoProcesso
	// ChamadaNativeWindowHandle: get_CurrentNativeWindowHandle.
	ChamadaNativeWindowHandle
	// ChamadaEstiloDaJanela: GetWindowLongW(GWL_STYLE), o fallback
	// ES_PASSWORD. Win32, pelo mesmo motivo de ChamadaNomeDoProcesso.
	ChamadaEstiloDaJanela
)

// String nomeia a chamada — só para diagnóstico. Como FalhaEm.String,
// nunca inclui conteúdo do elemento inspecionado.
func (c ChamadaUIA) String() string {
	switch c {
	case ChamadaNenhuma:
		return "nenhuma"
	case ChamadaGetFocusedElement:
		return "get_focused_element"
	case ChamadaControlType:
		return "control_type"
	case ChamadaIsPassword:
		return "is_password"
	case ChamadaProcessID:
		return "process_id"
	case ChamadaNomeDoProcesso:
		return "process_image_name"
	case ChamadaNativeWindowHandle:
		return "native_window_handle"
	case ChamadaEstiloDaJanela:
		return "window_style"
	default:
		return "desconhecida"
	}
}

// maisLenta acumula, chamada a chamada, a mais lenta de uma inspeção.
// O zero value está pronto para uso.
type maisLenta struct {
	chamada ChamadaUIA
	duracao time.Duration
}

// anotar registra que a chamada c levou d. Fica com a mais lenta; num
// empate, com a que foi anotada primeiro — a ordem das chamadas é fixa,
// então o empate não muda de lado de uma inspeção para outra.
func (m *maisLenta) anotar(c ChamadaUIA, d time.Duration) {
	if m.chamada == ChamadaNenhuma || d > m.duracao {
		m.chamada, m.duracao = c, d
	}
}
