//go:build !windows

package inserter

import (
	"errors"
	"log/slog"
)

// errNotSupported é devolvido por toda função dos backends stub quando o
// GOOS não é windows — mesmo padrão de internal/agent/win32.ErrNotSupported
// e internal/agent/clipboard.errNotSupported. Existe só para que este
// pacote, e tudo que o importa, continue compilando e com a estratégia
// pura testável (strategy_test.go) no runner Linux do CI; nenhuma chamada
// real acontece aqui.
var errNotSupported = errors.New("inserter: não suportado fora do Windows")

type stubUnicodeBackend struct{}

func newUnicodeBackend() unicodeInjector { return stubUnicodeBackend{} }

func (stubUnicodeBackend) InjectUnicode([]uint16) (int, error) { return 0, errNotSupported }

type stubClipboardBackend struct{}

func newClipboardBackend(*slog.Logger) clipboardInserter { return stubClipboardBackend{} }

func (stubClipboardBackend) InsertViaClipboard(string) (bool, error) {
	return false, errNotSupported
}
