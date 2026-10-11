package uia

// Este arquivo não tem build tag: é a decisão de quando uma leitura que
// passou do orçamento ainda vale. Quem lê e cronometra é
// automation_windows.go; aqui só se decide.

import "time"

// TetoLeituraAtrasada é até quando uma leitura COMPLETA que passou do
// orçamento (Config.InspectBudget) ainda vale. Ver classificarLeitura.
const TetoLeituraAtrasada = 2 * time.Second

// esperaReinspecaoImediata é a espera da reinspeção de uma leitura
// completa que passou de TetoLeituraAtrasada: o menor intervalo que o
// SetTimer do Windows aceita (USER_TIMER_MINIMUM, 10 ms, winuser.h) —
// pedir menos dá o mesmo. "Imediata" no sentido de não esperar a escada:
// o WM_TIMER só sai quando a fila da thread não tem outra mensagem, então
// um evento de foco que já esteja na fila roda antes dela, e aí a
// reinspeção nem dispara (cicloFoco.disparar).
const esperaReinspecaoImediata = 10 * time.Millisecond

// classificarLeitura decide o Outcome de uma leitura que chegou ao fim:
// GetFocusedElement devolveu um elemento, e ControlType e IsPassword — as
// duas chamadas essenciais — responderam. estiloLido diz se o sinal
// ES_PASSWORD rodou: get_CurrentNativeWindowHandle respondeu (hwnd 0 vale,
// como "não se aplica"). duracao é o tempo da leitura inteira; orcamento
// é Config.InspectBudget, e <= 0 desliga a checagem (só em teste).
//
// # A decisão do Mauro (29-set-2026, item 3)
//
// Até aqui, qualquer leitura acima do orçamento virava InspectTimeout —
// protegido — mesmo tendo lido tudo. Em 29-set-2026 isso gerou três dos
// 26 avisos do dia: a de 515 ms e as duas de um minuto, todas com
// is_password=false lido de verdade.
//
// Agora uma leitura acima do orçamento VALE (InspectOK, atrasada=true) se:
//
//	(a) GetFocusedElement, ControlType e IsPassword responderam — é o que
//	    faz a leitura chegar aqui;
//	(b) o sinal ES_PASSWORD rodou (estiloLido) — sem ele, um Edit clássico
//	    com ES_PASSWORD e IsPassword errado passaria como livre, que é
//	    justamente o caso que o segundo sinal existe para cobrir
//	    (contestação da investigação de 29-set-2026);
//	(c) a leitura inteira levou até TetoLeituraAtrasada.
//
// Acima de 2 s continua InspectTimeout — protegido como antes — e
// ProximaReinspecao pede uma reinspeção imediata: uma leitura NOVA,
// nunca a velha.
//
// # Só é seguro junto com o vigia
//
// Uma leitura atrasada descreve o elemento que tinha o foco quando ela
// COMEÇOU. Se o foco foi para um campo de senha no meio, o evento dessa
// troca está na fila da thread, esperando esta leitura acabar — e a guarda
// por geração não descarta nada sem reentrância. É o vigia que cobre esse
// intervalo: passado o orçamento, o cache está protegido; esta leitura o
// devolve a livre, e o evento da fila inspeciona o campo de senha logo em
// seguida — e o vigia dessa inspeção o protege de novo se ela passar do
// orçamento. O que sobra é o intervalo entre esta publicação e o fim da
// inspeção seguinte (no máximo um orçamento, 500 ms), aceito pelo Mauro
// junto com o teto de 2 s.
func classificarLeitura(estiloLido bool, duracao, orcamento time.Duration) (o InspectOutcome, atrasada bool) {
	switch {
	case orcamento <= 0 || duracao <= orcamento:
		return InspectOK, false
	case estiloLido && duracao <= TetoLeituraAtrasada:
		return InspectOK, true
	default:
		return InspectTimeout, false
	}
}
