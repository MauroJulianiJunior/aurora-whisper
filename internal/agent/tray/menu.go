// Package tray é o ícone do Agent na área de notificação do Windows e o
// menu dele (TASK-021; 08-ui-ux-spec.md, "Tray").
//
// Existe porque, até 28-set-2026, usar o produto exigia abrir um terminal
// e rodar um executável — e o Wispr Flow, que é a referência deste
// projeto, fica na bandeja e sobe sozinho com o Windows.
//
// # O que o menu tem, e o que ainda não
//
// A spec lista: Abrir, status, provider ativo, pausar, reconectar,
// configurações, sair — e a TASK-021 acrescenta o toggle de início
// automático. "Abrir" entrou com o painel local (6f546e0) e fica
// desabilitado quando o painel não subiu. "Configurações" espera a tela
// dela no painel, e "reconectar" depende da nuvem; entram quando essas
// coisas existirem. Um item de menu que não faz nada seria pior que a
// ausência dele.
//
// Este arquivo e icone.go não têm build tag: o QUE o menu mostra e COMO o
// ícone é desenhado são decisões testáveis no runner Linux. Só a entrega
// ao Windows fica em tray_windows.go.
package tray

import "errors"

// ErrNaoSuportado é devolvido fora do Windows.
var ErrNaoSuportado = errors.New("tray: só existe no Windows")

// Estado é o que o menu mostra. É lido de novo a cada abertura do menu
// (Acoes.Estado), nunca guardado: a fonte da verdade da pausa é o hook e
// a do início automático é o registro, e uma cópia aqui divergiria delas.
type Estado struct {
	Pausado           bool
	IniciarComWindows bool
	// InicioIndisponivel, quando não vazio, diz por que "Iniciar com o
	// Windows" não pode ser usado agora: o item fica desabilitado, e o
	// motivo aparece entre parênteses — desabilitado sem motivo parece
	// defeito. Desmarcado e clicável seria pior nos dois casos que existem:
	//
	//   - registro ilegível ("não consegui ler"): desmarcado afirmaria
	//     "desligado" sem ter medido, e o clique gravaria o contrário do
	//     que talvez já esteja lá;
	//   - build de console ("só na versão de bandeja"): o início automático
	//     é comparado com o PRÓPRIO executável, então a build de console,
	//     com outro nome, lia "desligado" com ele ligado para a de uso
	//     diário — e o clique gravava a build de console no Run, que abre
	//     uma janela preta a cada logon, e fechá-la mata o Agent (revisão
	//     de 29-set-2026).
	//
	// Quem preenche decide o texto; a bandeja só o mostra.
	InicioIndisponivel string
	// Provider descreve o ASR ativo, como a spec pede ("provider ativo").
	Provider string
	// PainelDisponivel diz se o painel local subiu. Se não subiu — porta
	// ocupada, histórico indisponível —, "Abrir Aurora Whisper" aparece
	// desabilitado em vez de abrir uma página que não responde.
	PainelDisponivel bool
	// Nuvem é a linha do sync (ADR-019, F7b; decisão 3 da F7b): "Nuvem: em
	// dia", "Nuvem: 3 para enviar", "Nuvem: sem conexão", "Nuvem:
	// aguardando", "Nuvem: resposta inesperada" ou "Nuvem: desconectada"
	// (syncengine.Laco.Resumo). Informação, desabilitada, sem ícone nem
	// aviso. Vazia (quem nunca ligou o PC a uma nuvem), a linha não aparece.
	Nuvem string
}

// ID identifica um item do menu no WM_COMMAND. Zero é reservado: é o que
// TrackPopupMenu devolve quando o menu é fechado sem escolha.
type ID int

const (
	IDNenhum ID = iota
	IDStatus
	IDProvider
	IDPausar
	IDIniciarComWindows
	IDSair
	// IDAbrir entrou depois, no fim da lista de propósito: os valores são
	// só identificadores de comando, e acrescentar no fim não renumera os
	// outros.
	IDAbrir
	// IDNuvem é a linha do sync (informação, como o status e o provider).
	IDNuvem
)

// Item é uma linha do menu.
type Item struct {
	ID         ID
	Texto      string
	Habilitado bool
	Marcado    bool
	Separador  bool
}

// Menu monta o menu para o estado e.
//
// Status e provider aparecem desabilitados: são informação, não ação. O
// Windows os desenha em cinza, que é a convenção para "isto é um
// rótulo", e clicar neles não faz nada.
func Menu(e Estado) []Item {
	status := "Ditado ativo"
	if e.Pausado {
		status = "Ditado pausado"
	}
	provider := e.Provider
	if provider == "" {
		provider = "desconhecido"
	}
	inicio := Item{ID: IDIniciarComWindows, Texto: "Iniciar com o Windows", Habilitado: true, Marcado: e.IniciarComWindows}
	if e.InicioIndisponivel != "" {
		inicio = Item{ID: IDIniciarComWindows, Texto: "Iniciar com o Windows (" + e.InicioIndisponivel + ")"}
	}
	itens := []Item{
		// Primeiro, como a spec lista: é a ação mais comum depois de
		// ditar, e o primeiro item do menu é onde o olho cai.
		{ID: IDAbrir, Texto: "Abrir Aurora Whisper", Habilitado: e.PainelDisponivel},
		{Separador: true},
		{ID: IDStatus, Texto: status},
		{ID: IDProvider, Texto: "ASR: " + provider},
	}
	if e.Nuvem != "" {
		itens = append(itens, Item{ID: IDNuvem, Texto: e.Nuvem})
	}
	return append(itens,
		Item{Separador: true},
		Item{ID: IDPausar, Texto: "Pausar ditado", Habilitado: true, Marcado: e.Pausado},
		inicio,
		Item{Separador: true},
		Item{ID: IDSair, Texto: "Sair", Habilitado: true},
	)
}

// maxDica é o tamanho do campo szTip de NOTIFYICONDATAW, em unidades
// UTF-16, contando o terminador. Texto maior seria cortado pelo Windows
// no meio de um caractere.
const maxDica = 128

// Dica é o texto que aparece ao parar o mouse sobre o ícone.
func Dica(e Estado) string {
	if e.Pausado {
		return "Aurora Whisper — ditado pausado"
	}
	return "Aurora Whisper"
}
