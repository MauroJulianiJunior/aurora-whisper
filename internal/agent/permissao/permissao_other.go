//go:build !windows

package permissao

// Consultar só lê o registro do Windows; fora dele não há o que perceber.
func Consultar() Motivo { return Desconhecido }
