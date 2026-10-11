//go:build !windows

package uia

// Stubs para GOOS != windows (ver doc.go). Nenhuma destas funções instala
// hook nem consulta UI Automation de verdade — elas existem só para que
// este pacote, e tudo que o importa, continue compilando e com a parte
// pura testável (types.go, tracker.go) executável no runner Linux do CI.

// Start sempre devolve ErrNotSupported fora do Windows.
func (t *Tracker) Start() error {
	return ErrNotSupported
}

// Stop é um no-op fora do Windows: não há hook instalado para encerrar.
func (t *Tracker) Stop() error {
	return nil
}
