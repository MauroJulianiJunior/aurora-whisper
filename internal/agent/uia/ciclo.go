package uia

// Este arquivo não tem build tag: é a lógica de decisão da thread
// dedicada do Tracker — o que publicar no cache e o que contar no log —
// sem nenhuma chamada de sistema. tracker_windows.go faz as chamadas; o
// que fazer com o resultado delas é decidido aqui e testado contra
// dublês em ciclo_test.go (CLAUDE.md, "Testes que agem no sistema real").

import "time"

// cicloFoco é o estado de decisão da thread dedicada. Só ela o toca — o
// callback do hook e o do timer de reinspeção rodam nela —, por isso não
// tem lock; os testes o exercitam de uma goroutine só.
type cicloFoco struct {
	// geracao sobe a cada inspeção que começa: a cada EVENT_OBJECT_FOCUS,
	// na inspeção do Start e em cada reinspeção. Ver comecar.
	geracao uint64

	// publicouProtegido é o Protected da última publicação no cache, e
	// protegidoDesde, o instante em que ele passou a true.
	publicouProtegido bool
	protegidoDesde    time.Time

	// A reinspeção agendada, se houver: a geração da inspeção em dúvida
	// que a pediu, e qual tentativa ela será. Só vale enquanto nenhuma
	// outra inspeção começar (ver disparar).
	pendente          bool
	pendenteGeracao   uint64
	pendenteTentativa int

	// pidProprio é o PID do próprio Agent, escrito uma vez em NewTracker.
	// Ver concluir, "Reinspeção que cai no próprio Agent".
	pidProprio uint32
}

// desfecho é o que a thread dedicada fez com uma inspeção, além do
// Snapshot em si. Vai para o DiagnosticoFoco.
type desfecho struct {
	// Descartada: uma inspeção mais nova começou depois desta, e esta não
	// publicou nada (ver devePublicar).
	Descartada bool
	// NoProprioAgent: esta reinspeção leu um elemento do próprio Agent e
	// não publicou nada (ver concluir). O cache ficou com a dúvida que a
	// pediu.
	NoProprioAgent bool
	// Liberou: esta publicação devolveu o cache de protegido para livre,
	// depois de ProtegidoPor protegido.
	Liberou      bool
	ProtegidoPor time.Duration
	// Tentativa: 0 para a inspeção de uma troca de foco (ou a do Start);
	// de 1 a MaxReinspecoes para as reinspeções que a seguiram.
	Tentativa int
	// ReinspecaoEm: maior que zero quando esta inspeção agendou a
	// seguinte, para daqui a ReinspecaoEm (ver ProximaReinspecao).
	ReinspecaoEm time.Duration
	// Vigia: o vigia desta inspeção protegeu o cache enquanto ela rodava
	// (ver Tracker.inspecionarEPublicar). Preenchido por quem chamou
	// concluir, que é quem sabe.
	Vigia bool
}

// publica diz se o resultado desta inspeção vai para o cache.
func (d desfecho) publica() bool {
	return !d.Descartada && !d.NoProprioAgent
}

// doProprioAgent diz se o elemento lido em s pertence ao processo
// pidProprio. ProcessID zero — a leitura dele falhou, ou não houve
// elemento — nunca é.
func doProprioAgent(s Snapshot, pidProprio uint32) bool {
	return s.ProcessID != 0 && s.ProcessID == pidProprio
}

// comecar abre uma inspeção e devolve a geração dela.
func (c *cicloFoco) comecar() uint64 {
	c.geracao++
	return c.geracao
}

// devePublicar diz se a inspeção que começou na geração inicio ainda pode
// publicar, sendo atual a geração mais nova já começada: só se nenhuma
// inspeção mais nova começou depois dela.
//
// # Contra o quê isto protege — e contra o quê NÃO
//
// A thread dedicada é uma só, e o callback do hook roda nela. Sem
// reentrância, os eventos de foco ficam na fila e são processados EM
// ORDEM: o evento seguinte só roda — e só sobe a geração — depois que a
// inspeção em curso já publicou. Nesse caso esta guarda nunca descarta
// nada, e uma inspeção presa (60 s observados em 29-set-2026) publica o
// que leu quando volta, valendo até a inspeção seguinte terminar.
// (Contestação da investigação de 29-set-2026, item 4.)
//
// Ela vale para o caso REENTRANTE: a Microsoft documenta que um hook que
// faz chamada COM entre processos pode ser reentrado ("Guarding Against
// Reentrancy in Hook Functions") — a espera da chamada bombeia mensagens,
// e o evento de foco seguinte roda DENTRO da inspeção anterior. Aí a mais
// nova termina primeiro e publica, e a mais antiga, que volta depois,
// sobrescreveria o cache com um elemento que já não tem o foco — nos dois
// sentidos: um timeout velho sobre um "livre" novo, ou um "livre" velho
// sobre um campo de senha novo. Esta guarda descarta a mais antiga.
//
// Se a reentrância acontece de fato nesta thread não foi verificado; o
// log diz quando a guarda age (descartada=true).
func devePublicar(inicio, atual uint64) bool {
	return inicio == atual
}

// concluir decide o que fazer com a inspeção número tentativa (ver
// desfecho.Tentativa), que começou na geração inicio, levou duracao e
// terminou em agora com o resultado s: ou ela é descartada (uma mais nova
// começou depois), ou é uma reinspeção que caiu no próprio Agent (abaixo)
// e também não publica, ou publica — e aí a volta a livre é contada (ver
// publicar) e, se s for dúvida, uma reinspeção fica pendente (ver
// ProximaReinspecao, que também olha duracao). Quem agenda de fato é quem
// chamou, com desfecho.ReinspecaoEm; se não conseguir, chama
// cancelarReinspecao.
//
// Uma inspeção descartada não mexe na reinspeção pendente: a mais nova,
// que publicou, já decidiu por ela. Uma resposta certa também não precisa
// apagar a pendência de uma dúvida anterior: ela começou numa geração
// mais nova, e disparar já recusa a pendência de geração velha.
//
// # Reinspeção que cai no próprio Agent
//
// O hook usa WINEVENT_SKIPOWNPROCESS: um foco que vai para uma janela do
// próprio Agent nunca é inspecionado. A reinspeção, porém, pergunta à UIA
// pelo foco ATUAL, e pode ler justamente uma dessas janelas — o menu da
// bandeja faz SetForegroundWindow na janela oculta do Agent antes de
// abrir (tray_windows.go). Publicar esse "livre" seria a única volta a
// livre sem um evento de foco de outro aplicativo: o log contaria uma
// liberação que não aconteceu no aplicativo, e um ditado nesse intervalo
// iria para a janela oculta, com o texto perdido (revisão da fase 1).
//
// Então a reinspeção que lê um elemento do próprio Agent não publica e
// encerra a escada. O cache fica com a dúvida que pediu a reinspeção —
// protegido: a reinspeção só dispara se nenhuma inspeção começou desde
// essa dúvida (disparar), e esta passou por devePublicar, então nenhuma
// outra publicou no meio. Voltar ao outro aplicativo dispara
// EVENT_OBJECT_FOCUS, que inspeciona de novo.
//
// Vale só para a reinspeção (tentativa > 0): é ela que esta fase
// acrescentou. A inspeção de uma troca de foco segue como antes.
func (c *cicloFoco) concluir(inicio uint64, tentativa int, s Snapshot, duracao time.Duration, agora time.Time) desfecho {
	if !devePublicar(inicio, c.geracao) {
		return desfecho{Descartada: true, Tentativa: tentativa}
	}
	if tentativa > 0 && doProprioAgent(s, c.pidProprio) {
		return desfecho{NoProprioAgent: true, Tentativa: tentativa}
	}
	d := desfecho{Tentativa: tentativa}
	d.Liberou, d.ProtegidoPor = c.publicar(s.Protected(), agora)

	if espera, ok := ProximaReinspecao(s, duracao, tentativa); ok {
		c.pendente, c.pendenteGeracao, c.pendenteTentativa = true, inicio, tentativa+1
		d.ReinspecaoEm = espera
	}
	return d
}

// anotarVigia registra que o vigia da inspeção que começou na geração
// inicio publicou protegido no instante em — para que a volta a livre
// conte esse intervalo em ProtegidoPor, como conta o de uma dúvida.
//
// Só vale se essa inspeção ainda é a atual: se uma mais nova começou
// depois (reentrância), foi ela que publicou por último, e o protegido
// deste vigia já não é o que está no cache.
//
// Roda na thread dedicada, depois de o vigia ter terminado — nunca na
// goroutine do vigia, que não toca o cicloFoco.
func (c *cicloFoco) anotarVigia(inicio uint64, em time.Time) {
	if !devePublicar(inicio, c.geracao) {
		return
	}
	c.publicar(true, em)
}

// cancelarReinspecao desiste da reinspeção pendente — quando o timer não
// pôde ser criado. O cache continua como está: protegido, se a última
// inspeção foi dúvida.
func (c *cicloFoco) cancelarReinspecao() {
	c.pendente = false
}

// disparar é chamado quando o timer da reinspeção vence. Devolve ok ==
// true, e qual tentativa fazer, só se a reinspeção pendente ainda vale:
// nenhuma outra inspeção começou desde a que a pediu. Se o foco mudou
// nesse meio-tempo, a inspeção da troca já respondeu por ele, e
// reinspecionar seria inspecionar de novo quem já foi inspecionado — ou,
// com reentrância, correr com ela.
//
// A pendência é consumida em qualquer caso: um timer vencido nunca
// dispara duas reinspeções.
func (c *cicloFoco) disparar() (tentativa int, ok bool) {
	if !c.pendente {
		return 0, false
	}
	c.pendente = false
	if c.pendenteGeracao != c.geracao {
		return 0, false
	}
	return c.pendenteTentativa, true
}

// publicar anota que o cache recebeu um alvo com Protected == protegido
// no instante agora. Devolve liberou == true só quando esta publicação
// devolveu o cache de protegido para livre, e por quanto tempo ele ficou
// protegido — contado da PRIMEIRA publicação protegida da sequência, não
// da última: é quanto tempo quem estava ali não conseguia ditar.
//
// O valor inicial do cache (Protected: true, em NewTracker) não conta:
// ele não veio de inspeção nenhuma, e a inspeção do Start o substitui na
// subida. Contá-lo só poria no log uma "volta a livre" de milissegundos
// a cada vez que o Agent sobe.
func (c *cicloFoco) publicar(protegido bool, agora time.Time) (liberou bool, protegidoPor time.Duration) {
	switch {
	case protegido && !c.publicouProtegido:
		c.protegidoDesde = agora
	case !protegido && c.publicouProtegido:
		liberou, protegidoPor = true, agora.Sub(c.protegidoDesde)
	}
	c.publicouProtegido = protegido
	return liberou, protegidoPor
}
