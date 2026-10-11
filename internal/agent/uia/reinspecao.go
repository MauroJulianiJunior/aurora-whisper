package uia

// Este arquivo não tem build tag: é a política de nova tentativa depois de
// uma inspeção em dúvida — quando, quantas vezes, em que casos. Quem
// agenda de verdade é tracker_windows.go (um timer da própria thread
// dedicada); aqui só se decide.

import "time"

// esperasDeReinspecao são as esperas antes de cada reinspeção, na ordem:
// a primeira sai 100 ms depois da inspeção original, a segunda 300 ms
// depois da primeira, a terceira 1 s depois da segunda. Não há quarta.
var esperasDeReinspecao = [...]time.Duration{
	100 * time.Millisecond,
	300 * time.Millisecond,
	time.Second,
}

// MaxReinspecoes é quantas reinspeções, no máximo, seguem uma mesma troca
// de foco.
const MaxReinspecoes = len(esperasDeReinspecao)

// TetoParaReinspecionar: uma inspeção que levou mais que isto não é
// reinspecionada — salvo a leitura completa acima de TetoLeituraAtrasada,
// que é reinspecionada já. Ver ProximaReinspecao, "O teto de duração".
//
// # Revisto em 29-set-2026, com o teto de 3 s: fica em 2 s
//
// O item 2 das decisões do Mauro pôs um teto por chamada
// (TetoChamadaUIA, 3 s) e pediu para rever esta regra. Ela fica, em 2 s —
// ABAIXO do teto de propósito:
//
//   - Com o CUIAutomation8, uma chamada que o provedor não responde volta
//     como UIA_E_TIMEOUT perto de 3 s. Como 3 s > 2 s, essa inspeção não é
//     reinspecionada: o provedor acabou de bater no teto. A única
//     evidência que há (29-set-2026) são travas de um minuto;
//     reinspecionar em 100 ms, 300 ms e 1 s voltaria a um provedor que
//     muito provavelmente ainda está travado, e cada volta prende a thread
//     até mais 3 s sem processar troca de foco — o vigia deixa isso
//     seguro, mas uma troca para um campo sadio nesse meio-tempo ficaria
//     recusada até a thread voltar.
//   - Sem reinspeção, o custo agora é pequeno: a fila de eventos anda em
//     no máximo ~3 s (e não mais um minuto), e trocar de janela e voltar
//     inspeciona de novo.
//   - A leitura COMPLETA acima de 2 s é outra coisa: o provedor respondeu
//     tudo, está lento e não travado. Ela ganha a reinspeção imediata do
//     item 3, sem este teto.
//
// Se o CUIAutomation8 não existir (Automacao, ObjetoCUIAutomation), não
// há teto por chamada, e esta regra vale como na fase 1: é o que impede
// uma chamada de um minuto de se repetir três vezes.
//
// Não afrouxa a proteção em caso nenhum: um valor maior ou menor só muda
// quantas vezes se pergunta de novo.
const TetoParaReinspecionar = 2 * time.Second

// ProximaReinspecao decide se, depois de uma inspeção que terminou em s e
// levou duracao, vale perguntar de novo quem tem o foco, e depois de
// quanto tempo. tentativa é quantas reinspeções já foram feitas para esta
// troca de foco (0 para a inspeção original).
//
// A reinspeção lê o foco ATUAL (GetFocusedElement), não "o mesmo
// elemento": sem um evento de troca de foco no meio, é em geral o mesmo
// campo, mas nada garante. Uma troca para outro processo dispara o
// próprio EVENT_OBJECT_FOCUS e cancela a reinspeção (ver
// cicloFoco.disparar).
//
// # Por que existe
//
// Até 29-set-2026 a primeira falha virava protegido e ficava: a única
// atualização seguinte do cache era o próximo EVENT_OBJECT_FOCUS de OUTRO
// processo. Quem continuava no mesmo campo depois de um WARN não
// conseguia ditar. E algumas dessas falhas são passageiras por definição:
// a página do GetFocusedElement no Microsoft Learn manda tentar de novo
// em UIA_E_ELEMENTNOTAVAILABLE (0x80040201), que sai quando o elemento
// com foco some durante a chamada.
//
// # Em que casos
//
// Reinspeciona quando a inspeção terminou em dúvida e não passou do teto
// de duração (abaixo):
//
//   - outcome=failed, com qualquer chamada e qualquer HRESULT — inclusive
//     UIA_E_ELEMENTNOTAVAILABLE, UIA_E_TIMEOUT (0x80131505) e E_FAIL;
//   - outcome=nil_element;
//   - outcome=timeout do orçamento, a menos que a leitura atrasada tenha
//     visto sinal de senha: aí ela já aponta para um campo de senha, e
//     reinspecionar só confirmaria o bloqueio (se o foco mudou, o evento
//     da troca reinspeciona de qualquer forma).
//
// Um outcome=timeout de leitura COMPLETA (EstiloLido) — que só existe
// acima de TetoLeituraAtrasada, porque abaixo dele a leitura vale — é
// reinspecionado já (esperaReinspecaoImediata), sem o teto de duração
// abaixo: é o item 3 das decisões do Mauro de 29-set-2026. O provedor
// respondeu todas as chamadas; está lento, não travado, e com o
// CUIAutomation8 cada chamada tem teto próprio (TetoChamadaUIA).
//
// NÃO reinspeciona outcome=ok — nem livre nem senha: é resposta, não
// dúvida (inclusive a leitura atrasada que valeu).
//
// # O teto de duração
//
// NÃO reinspeciona uma inspeção em dúvida que levou mais que
// TetoParaReinspecionar — a exceção é a leitura completa acima de
// TetoLeituraAtrasada, acima. A thread dedicada é uma só: enquanto uma
// chamada UIA está presa, os eventos de foco esperam na fila. Em
// 29-set-2026 duas chamadas ficaram presas um minuto cada. Reinspecionar
// 100 ms depois voltaria ao provedor que acabou de travar; se ele travasse
// de novo, seriam até três minutos a mais com a thread cega por troca de
// foco — e uma troca para um campo sadio, feita nesse meio-tempo, só
// seria inspecionada no fim, com o ditado recusado até lá (revisão da
// fase 1). Sem a reinspeção, essa dúvida fica como ficava antes dela:
// protegida até a próxima troca de foco.
//
// O teto separa o timeout que passa um pouco do orçamento de 500 ms (515
// ms observado, que vale perguntar de novo) da chamada presa (60 s com o
// CUIAutomation antigo; ~3 s, UIA_E_TIMEOUT, com o CUIAutomation8 e
// TetoChamadaUIA). Ele não impede que a própria reinspeção trave — a
// original falha rápido, a reinspeção prende —, mas encerra a escada ali:
// no máximo uma trava a mais por troca de foco.
//
// # O que NÃO muda
//
// "Na dúvida, protege" (ADR-011, decisão 5) continua valendo: o cache
// fica protegido enquanto as reinspeções não respondem. Reinspecionar só
// dá à dúvida a chance de virar resposta sem a pessoa ter de trocar de
// janela e voltar.
//
// O máximo é MaxReinspecoes. Uma falha que não é passageira — o E_FAIL
// em rajada do Gerenciador de Tarefas, que roda elevado e que o Agent,
// sem uiAccess, não consegue inspecionar — custa três inspeções a mais,
// não um laço.
func ProximaReinspecao(s Snapshot, duracao time.Duration, tentativa int) (espera time.Duration, ok bool) {
	if tentativa < 0 || tentativa >= MaxReinspecoes {
		return 0, false
	}
	switch {
	case s.Outcome == InspectOK:
		return 0, false
	case s.Outcome == InspectTimeout && (s.IsPassword || s.StyleHasPassword):
		return 0, false
	case s.Outcome == InspectTimeout && s.EstiloLido:
		// Leitura completa que passou de TetoLeituraAtrasada (abaixo dele
		// ela teria valido: classificarLeitura). Decisão do Mauro de
		// 29-set-2026: protegida como antes, e uma leitura NOVA já — sem o
		// teto de duração, porque o provedor respondeu tudo: está lento,
		// não travado.
		return esperaReinspecaoImediata, true
	}
	if duracao > TetoParaReinspecionar {
		return 0, false
	}
	// Todo o resto é dúvida — o mesmo critério de Snapshot.Protected, que
	// trata qualquer Outcome diferente de InspectOK como protegido.
	return esperasDeReinspecao[tentativa], true
}
