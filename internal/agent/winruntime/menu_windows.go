//go:build windows

package winruntime

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"

// menuEmPrimeiroPlano reporta se a thread da janela em primeiro plano está
// num menu do Win32: o de contexto, o da bandeja, a barra de menu (ADR-025,
// risco 5). É o sinal que não depende de um item do menu ter o foco: num
// menu aberto pelo clique, nenhum item está destacado ainda, e as teclas já
// vão para o menu.
//
// Só leitura (GetGUIThreadInfo): não move foco nem muda nada. Só a bandeira
// do modo de menu é lida; as janelas que a chamada devolve junto não saem
// daqui. Falhar responde "sem menu", e quem decide fica com o tipo do
// controle em foco (focoEhMenu).
func menuEmPrimeiroPlano() bool {
	var info win32.GUITHREADINFO
	if err := win32.GetGUIThreadInfo(0, &info); err != nil {
		return false
	}
	return info.Flags&(win32.GUIInMenuMode|win32.GUISystemMenuMode|win32.GUIPopupMenuMode) != 0
}
