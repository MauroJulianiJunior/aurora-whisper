//go:build !windows

package conflito

// Listar fora do Windows não enumera nada: o produto é Windows-only, e
// devolver vazio mantém o pacote compilável e testável no runner Linux
// do CI (ver CLAUDE.md, "compilação multiplataforma").
func Listar() ([]string, error) { return nil, nil }
