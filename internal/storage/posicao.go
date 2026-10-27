package storage

import "github.com/MauroJulianiJunior/aurora-whisper/internal/notas"

// A ordem manual das notas (ADR-022, "Ordem manual (N2b)") mora em
// internal/notas desde a N8: a nuvem calcula a mesma chave (a nota nova no
// topo, o mover do celular) sem importar o armazenamento do PC. Estes nomes
// ficam para o resto do pacote e para quem já os usava.

// MaxPosicao é o teto de uma chave (notas.MaxPosicao).
const MaxPosicao = notas.MaxPosicao

var (
	// ErrPosicaoInvalida: uma chave fora do alfabeto, vazia, grande demais ou
	// terminada em '0'.
	ErrPosicaoInvalida = notas.ErrPosicaoInvalida
	// ErrPosicaoInvertida: pediu uma chave entre duas que não estão em ordem.
	ErrPosicaoInvertida = notas.ErrPosicaoInvertida
	// ErrPosicaoSemEspaco: o teto de MaxPosicao não deixa gerar a chave.
	ErrPosicaoSemEspaco = notas.ErrPosicaoSemEspaco
)

// PosicaoValida diz se s serve de posição (notas.PosicaoValida).
func PosicaoValida(s string) bool { return notas.PosicaoValida(s) }

// PosicaoEntre devolve uma chave estritamente entre antes e depois
// (notas.PosicaoEntre).
func PosicaoEntre(antes, depois string) (string, error) { return notas.PosicaoEntre(antes, depois) }
