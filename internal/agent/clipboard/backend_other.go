//go:build !windows

package clipboard

import "errors"

// errNotSupported é devolvido por toda função de stubBackend quando o
// GOOS não é windows. Existe só para que este pacote continue
// compilando e testável no runner Linux do CI (mesmo padrão de
// internal/agent/win32.ErrNotSupported); nenhuma chamada real acontece
// aqui.
var errNotSupported = errors.New("clipboard: não suportado fora do Windows")

// stubBackend é a implementação de backend usada fora do Windows.
type stubBackend struct{}

func newBackend() backend { return stubBackend{} }

func (stubBackend) open() error                        { return errNotSupported }
func (stubBackend) close()                             {}
func (stubBackend) empty() error                       { return errNotSupported }
func (stubBackend) unicodeText() (string, bool, error) { return "", false, errNotSupported }
func (stubBackend) setUnicodeText(string) error        { return errNotSupported }
func (stubBackend) sequenceNumber() (uint32, error)    { return 0, errNotSupported }
