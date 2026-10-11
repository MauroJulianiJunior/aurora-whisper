package clipboard

// backend abstrai as chamadas Win32 de baixo nível para permitir dublê em
// teste. A implementação real (backend_windows.go) chama OpenClipboard/
// GetClipboardData/SetClipboardData/GetClipboardSequenceNumber de
// verdade; fora do Windows (backend_other.go), toda função devolve
// errNotSupported só para manter o pacote compilável e testável no CI
// Linux (mesmo padrão de internal/agent/win32, ver seu errors.go).
//
// Este arquivo não tem build tag: é só a interface, sem nenhuma chamada
// de sistema.
type backend interface {
	// open abre o clipboard para o processo corrente (hwnd NULL — este
	// pacote não tem, e não precisa de, uma janela própria).
	open() error

	// close fecha o clipboard aberto por open. Todo chamador de open
	// bem-sucedido DEVE chamar close via defer, sempre — inclusive nos
	// caminhos de erro — porque um clipboard aberto e não fechado trava
	// o recurso para o sistema inteiro.
	close()

	// empty esvazia o clipboard (também assume a posse dele para este
	// processo, exigido pelo Win32 antes de SetClipboardData).
	empty() error

	// unicodeText lê o CF_UNICODETEXT atual. ok=false (sem erro) quando
	// esse formato simplesmente não está presente — que é o caso comum
	// de "clipboard tinha outra coisa" ou "clipboard vazio", não uma
	// falha.
	unicodeText() (text string, ok bool, err error)

	// setUnicodeText substitui o conteúdo do clipboard por text como
	// CF_UNICODETEXT. Chama empty() internamente.
	setUnicodeText(text string) error

	// sequenceNumber lê o contador que o Win32 incrementa a cada vez que
	// o conteúdo do clipboard muda (nunca quando só é lido — ver doc.go
	// do pacote).
	sequenceNumber() (uint32, error)
}
