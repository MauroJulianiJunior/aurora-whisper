package tray

import "time"

// # Por que o ícone tenta de novo
//
// Shell_NotifyIconW entrega o pedido à barra de tarefas com
// SendMessageTimeout e desiste se o Explorer estiver ocupado — o que é
// justamente o estado dele no logon, quando o início automático sobe o
// Agent. A primeira versão tratava essa primeira falha como definitiva:
// sem console, o Agent saía, e o início automático falhava em silêncio
// pela sessão inteira (revisão de 28-set-2026 do commit 73de148).
//
// Agora a janela fica viva, e o ícone é tentado de novo a cada
// IntervaloEntreTentativas, até MaxTentativasDoIcone. O Agent só desiste
// se a barra de tarefas não aceitar o ícone em todo esse tempo.
//
// # Depois da subida, não há desistência
//
// Quando o Explorer reinicia, ele manda TaskbarCreated e recebe o NIM_ADD
// de todos os programas da bandeja ao mesmo tempo — ocupado de novo. Até
// 29-set-2026 esse caso tentava UMA vez: se falhasse, o ícone sumia pelo
// resto da sessão, com o Agent ditando e sem jeito de sair a não ser pelo
// Gerenciador de Tarefas (revisão de 29-set-2026).
//
// Depois da subida, desistir não serve para nada: Iniciar já voltou, e
// não há a quem devolver o erro. Então a série continua pelo mesmo
// timer, sem limite, até o ícone entrar.
const (
	MaxTentativasDoIcone     = 10
	IntervaloEntreTentativas = time.Second
)

// Passo é o que fazer depois de uma tentativa de pôr o ícone.
type Passo uint8

const (
	PassoPronto Passo = iota
	PassoTentarDeNovo
	PassoDesistir
)

// DepoisDaTentativa decide o próximo passo. tentativas conta a que acabou
// de acontecer na série corrente (a primeira é 1). jaSubiu diz se Iniciar
// já voltou — aí a série é a do TaskbarCreated, e não desiste nunca.
func DepoisDaTentativa(tentativas int, conseguiu, jaSubiu bool) Passo {
	switch {
	case conseguiu:
		return PassoPronto
	case jaSubiu:
		return PassoTentarDeNovo
	case tentativas >= MaxTentativasDoIcone:
		return PassoDesistir
	default:
		return PassoTentarDeNovo
	}
}
