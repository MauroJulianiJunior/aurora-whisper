package uia

// Este arquivo não tem build tag: é lógica pura de decisão sobre o
// resultado de uma inspeção UIA, sem nenhuma chamada de sistema — testável
// em qualquer SO (ver doc.go e ADR-011, decisão 5).

import (
	"fmt"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// InspectOutcome classifica como uma consulta UIA ao elemento com foco
// terminou. ADR-011, decisão 5, exige tratar cada ramo explicitamente:
// inspeção ok e não-senha, ok e senha, falhou, timeout — mais
// InspectNilElement (elemento nulo: nenhum controle tem foco de teclado
// agora, por exemplo durante uma troca de janela).
type InspectOutcome int

const (
	// InspectOK: a consulta terminou dentro do orçamento (Config.
	// InspectBudget) e devolveu um elemento válido. Os campos de Snapshot
	// refletem o que a UIA reportou de fato.
	InspectOK InspectOutcome = iota
	// InspectFailed: uma chamada COM essencial (ControlType ou IsPassword)
	// devolveu HRESULT de falha.
	InspectFailed
	// InspectTimeout: a consulta excedeu Config.InspectBudget e não se
	// qualificou como leitura atrasada que ainda vale — ou passou de
	// TetoLeituraAtrasada, ou o sinal ES_PASSWORD não rodou (ver
	// classificarLeitura). Até 29-set-2026, qualquer consulta acima do
	// orçamento caía aqui, mesmo completa.
	InspectTimeout
	// InspectNilElement: GetFocusedElement devolveu sucesso, mas nenhum
	// elemento (ponteiro nulo) — não há foco de teclado no momento.
	InspectNilElement
)

// String nomeia o Outcome — só para diagnóstico (nunca inclui conteúdo do
// elemento inspecionado, ver NFR-010 e CLAUDE.md, "Safety rules").
func (o InspectOutcome) String() string {
	switch o {
	case InspectOK:
		return "ok"
	case InspectFailed:
		return "failed"
	case InspectTimeout:
		return "timeout"
	case InspectNilElement:
		return "nil_element"
	default:
		return "unknown"
	}
}

// FalhaEm nomeia QUAL chamada COM falhou numa inspeção, quando
// Outcome == InspectFailed.
//
// # Por que existe
//
// Em 28-set-2026 Mauro viu três "campo protegido" seguidos num campo de
// texto comum, e desistiu de usar o Agent. O log dizia outcome=failed e
// mais nada — mas HÁ DOIS caminhos que produzem esse outcome, e eles
// significam coisas opostas:
//
//	get_focused_element  a UIA não conseguiu sequer dizer QUEM tem o foco
//	control_type/is_password  o elemento veio, uma propriedade não leu
//
// O primeiro aponta para a janela em foco estar fora de alcance (processo
// elevado, app que não responde, RPC caindo). O segundo, para o elemento
// existir e ser exótico. As correções não se parecem.
//
// # Por que um tipo, e não uma string
//
// Porque uma string de diagnóstico neste pacote é uma via de vazamento
// esperando acontecer: CLAUDE.md proíbe registrar contexto de janela, e
// DiagnosticoFoco tem um teste dedicado a provar que não o faz. Um valor
// enumerado não consegue carregar o nome da janela nem por acidente nem
// por refatoração distraída — o mesmo motivo de learning.Confirmation
// guardar um bool não exportado em vez de confiar em disciplina.
type FalhaEm uint8

const (
	// FalhaNenhuma: não houve falha de chamada COM. É o valor de
	// InspectOK, InspectTimeout e InspectNilElement — timeout e elemento
	// nulo bloqueiam o ditado, mas nenhum dos dois é uma chamada que
	// falhou.
	FalhaNenhuma FalhaEm = iota
	// FalhaGetFocusedElement: IUIAutomation::GetFocusedElement devolveu
	// HRESULT de falha. Não chegamos a ter elemento nenhum.
	FalhaGetFocusedElement
	// FalhaControlType: get_CurrentControlType falhou no elemento já
	// obtido.
	FalhaControlType
	// FalhaIsPassword: get_CurrentIsPassword falhou. É o sinal primário
	// de segurança — falhar aqui é o único caso em que "na dúvida,
	// protege" está protegendo de algo concreto.
	FalhaIsPassword
)

// String nomeia a falha — só para diagnóstico. Como InspectOutcome.String,
// nunca inclui conteúdo do elemento inspecionado: o conjunto de valores
// possíveis está inteiro escrito aqui embaixo.
func (f FalhaEm) String() string {
	switch f {
	case FalhaNenhuma:
		return "nenhuma"
	case FalhaGetFocusedElement:
		return "get_focused_element"
	case FalhaControlType:
		return "get_CurrentControlType"
	case FalhaIsPassword:
		return "get_CurrentIsPassword"
	default:
		return "desconhecida"
	}
}

// Snapshot é o resultado bruto de uma consulta UIA ao elemento com foco,
// antes de virar o dictation.FocusTarget que o núcleo consome (ADR-007).
// Carrega mais dado do que dictation.FocusTarget — ProcessID, os sinais
// de senha crus, o diagnóstico da falha; só ProcessName, ControlType e o
// resultado de Protected() atravessam a fronteira (ToFocusTarget).
//
// Não tem Name nem ClassName: eram lidos e nunca usados, e saíram em
// 29-set-2026 (ver readElement em automation_windows.go).
type Snapshot struct {
	Outcome InspectOutcome

	ProcessID   uint32
	ProcessName string
	ControlType string

	// IsPassword é o sinal primário da UIA
	// (IUIAutomationElement::get_CurrentIsPassword). Só significativo
	// quando Outcome == InspectOK.
	IsPassword bool
	// StyleHasPassword é o sinal de fallback (ADR-011, decisão 5):
	// ES_PASSWORD no estilo de janela de um controle Edit clássico, para
	// quando a UIA não responde IsPassword corretamente. Só significativo
	// quando Outcome == InspectOK; fica false quando o elemento não tem um
	// HWND nativo próprio (controles modernos/navegador) — nesse caso o
	// fallback simplesmente não se aplica, e o sinal da UIA basta.
	StyleHasPassword bool
	// EstiloLido diz se o sinal de StyleHasPassword RODOU:
	// get_CurrentNativeWindowHandle respondeu (hwnd 0 conta, como "não se
	// aplica"). Com false, StyleHasPassword=false não quer dizer nada — a
	// leitura do hwnd falhou. É a condição (b) de classificarLeitura.
	EstiloLido bool
	// Atrasada marca a leitura completa que passou do orçamento e valeu
	// mesmo assim, por estar dentro de TetoLeituraAtrasada (ver
	// classificarLeitura). Só com Outcome == InspectOK.
	Atrasada bool

	// Elevacao diz se a janela em primeiro plano é de administrador. Só é
	// verificada depois de uma inspeção que não trouxe elemento (ver
	// deveOlharPrimeiroPlano); nas demais fica ElevacaoNaoVerificada. Não
	// entra em Protected(): muda só a mensagem, nunca a decisão.
	Elevacao Elevacao
	// ExecutavelDoPrimeiroPlano é o nome-base do executável da janela em
	// primeiro plano, lido junto com Elevacao e nas mesmas condições. Só
	// serve ao aviso (executavelDoAviso); não atravessa para o núcleo.
	ExecutavelDoPrimeiroPlano string

	// Falha e HRESULT descrevem a chamada COM que falhou. Só
	// significativos quando Outcome == InspectFailed; nos demais outcomes
	// ficam no zero value (FalhaNenhuma, 0).
	//
	// HRESULT é o código cru que a UIA devolveu, guardado para o log em
	// hexa. É um int32 e não o tipo hresult de automation_windows.go
	// porque Snapshot compila em qualquer plataforma e aquele tipo não —
	// e porque aqui ele não é operado, só reportado.
	Falha   FalhaEm
	HRESULT int32

	// LentaEm e LentaDuracao dizem qual chamada da inspeção demorou mais,
	// e quanto. Valem em qualquer Outcome: numa falha, a mais lenta pode
	// ser justamente a que falhou. Ver ChamadaUIA.
	LentaEm      ChamadaUIA
	LentaDuracao time.Duration
}

// Protected decide se o ditado deve ser bloqueado neste alvo (ADR-011,
// decisão 5: "na dúvida, proteger"). Qualquer Outcome diferente de
// InspectOK — falhou, deu timeout, elemento nulo — já É a dúvida: o custo
// de proteger à toa é um ditado que não acontece; o custo do erro oposto é
// voz capturada num campo de senha. Com Outcome == InspectOK, basta um dos
// dois sinais (IsPassword da UIA, ou StyleHasPassword do fallback
// ES_PASSWORD) ser verdadeiro.
func (s Snapshot) Protected() bool {
	if s.Outcome != InspectOK {
		return true
	}
	return s.IsPassword || s.StyleHasPassword
}

// ToFocusTarget traduz o Snapshot para o DTO que dictation.Machine
// consome (ADR-007, "FocusTarget... é dado já pronto entregue pelo
// runtime"). Só o que o núcleo de fato contrata atravessa a fronteira —
// ProcessID e os sinais de senha crus ficam só no Snapshot, para não
// vazar mais forma do runtime Win32 para dentro do núcleo do que ele já
// contrata (ADR-007, "O que foi rejeitado": nenhuma interface nova para
// foco).
func (s Snapshot) ToFocusTarget() dictation.FocusTarget {
	return dictation.FocusTarget{
		ProcessName: s.ProcessName,
		ControlType: s.ControlType,
		Protected:   s.Protected(),
	}
}

// controlTypeNames mapeia os UIA_*ControlTypeId documentados em
// UIAutomationClient.h para um nome legível. Um ID fora deste mapa (ex.:
// um controle customizado exótico, ou zero quando a UIA não informou
// nada) cai no fallback numérico de controlTypeName — nunca um erro:
// ControlType é só descritivo em dictation.FocusTarget, e nenhuma decisão
// deste pacote depende do seu valor além de aparecer em Snapshot/logs.
var controlTypeNames = map[int32]string{
	50000: "Button",
	50001: "Calendar",
	50002: "CheckBox",
	50003: "ComboBox",
	50004: "Edit",
	50005: "Hyperlink",
	50006: "Image",
	50007: "ListItem",
	50008: "List",
	50009: "Menu",
	50010: "MenuBar",
	50011: "MenuItem",
	50012: "ProgressBar",
	50013: "RadioButton",
	50014: "ScrollBar",
	50015: "Slider",
	50016: "Spinner",
	50017: "StatusBar",
	50018: "Tab",
	50019: "TabItem",
	50020: "Text",
	50021: "ToolBar",
	50022: "ToolTip",
	50023: "Tree",
	50024: "TreeItem",
	50025: "Custom",
	50026: "Group",
	50027: "Thumb",
	50028: "DataGrid",
	50029: "DataItem",
	50030: "Document",
	50031: "SplitButton",
	50032: "Window",
	50033: "Pane",
	50034: "Header",
	50035: "HeaderItem",
	50036: "Table",
	50037: "TitleBar",
	50038: "Separator",
	50039: "SemanticZoom",
	50040: "AppBar",
}

// controlTypeName traduz um UIA_*ControlTypeId para um nome legível.
func controlTypeName(id int32) string {
	if name, ok := controlTypeNames[id]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", id)
}
