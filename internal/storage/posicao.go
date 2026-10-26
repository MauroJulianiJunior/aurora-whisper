package storage

import (
	"errors"
	"strings"
)

// Este arquivo é a ordem manual das notas (ADR-022, "Ordem manual (N2b)"): a
// posição de cada nota é um índice fracionário, uma chave de texto que se
// ordena pela comparação de bytes, e arrastar uma nota grava UMA chave nova, a
// que cai entre as duas vizinhas. Nenhuma outra nota é renumerada, o que é o
// ponto: o sync (N7) leva só a nota que mudou.
//
// # A chave
//
// Texto no alfabeto 0-9 A-Z a-z (a ordem dos bytes ASCII é a ordem do
// alfabeto), que NÃO termina em '0'. Sem o '0' do fim, sempre existe uma chave
// entre duas chaves quaisquer e uma antes da primeira ("0V" < "1" e nada fica
// preso em "0"). Uma chave é lida como uma fração em base 62 depois da vírgula,
// e o '0' do fim seria um zero à direita da mesma fração.
//
// # De onde sai cada chave
//
//   - entre duas vizinhas: o ponto médio (o algoritmo clássico do índice
//     fracionário, sem a parte inteira);
//   - antes da primeira (a nota nova, a que se fixa): o dígito de antes, não o
//     meio. A nota nova entra sempre no topo, e o meio entre "" e a primeira
//     encolheria o espaço pela metade a cada nota, com chave de um caractere a
//     mais a cada cinco notas; o dígito de antes gasta um caractere a cada ~30;
//   - depois da última: o dígito de depois, pelo mesmo motivo.

// alfabetoDaPosicao tem os 62 dígitos, em ordem de byte.
const alfabetoDaPosicao = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// MaxPosicao é o teto de uma chave. O uso normal fica abaixo de 10
// caracteres; o teto só impede que um pedido torto grave um texto enorme.
const MaxPosicao = 128

var (
	// ErrPosicaoInvalida: uma chave fora do alfabeto, vazia, grande demais ou
	// terminada em '0'.
	ErrPosicaoInvalida = errors.New("storage: posição inválida")
	// ErrPosicaoInvertida: pediu uma chave entre duas que não estão em ordem
	// (antes >= depois). Quem chama tem a lista desatualizada.
	ErrPosicaoInvertida = errors.New("storage: as vizinhas estão fora de ordem")
	// ErrPosicaoSemEspaco: o teto de MaxPosicao não deixa gerar a chave. Na
	// prática, nunca.
	ErrPosicaoSemEspaco = errors.New("storage: sem espaço na ordem das notas")
)

// PosicaoValida diz se s serve de posição.
func PosicaoValida(s string) bool {
	if s == "" || len(s) > MaxPosicao || s[len(s)-1] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(alfabetoDaPosicao, s[i]) < 0 {
			return false
		}
	}
	return true
}

func digitoDe(c byte) int { return strings.IndexByte(alfabetoDaPosicao, c) }

// PosicaoEntre devolve uma chave estritamente entre antes e depois, na ordem
// de bytes. "" no lugar de antes é "nada antes" (a chave cai no topo) e "" no
// lugar de depois é "nada depois" (cai no fim). As duas vazias dão a chave do
// meio, a da primeira nota.
func PosicaoEntre(antes, depois string) (string, error) {
	if antes != "" && !PosicaoValida(antes) || depois != "" && !PosicaoValida(depois) {
		return "", ErrPosicaoInvalida
	}
	if antes != "" && depois != "" && antes >= depois {
		return "", ErrPosicaoInvertida
	}
	var k string
	switch {
	case antes == "" && depois == "":
		k = string(alfabetoDaPosicao[len(alfabetoDaPosicao)/2])
	case antes == "":
		k = posicaoAntesDe(depois)
	case depois == "":
		k = posicaoDepoisDe(antes)
	default:
		k = posicaoMeio(antes, depois)
	}
	if len(k) > MaxPosicao {
		return "", ErrPosicaoSemEspaco
	}
	return k, nil
}

// posicaoAntesDe devolve uma chave menor que b (válida, não vazia).
func posicaoAntesDe(b string) string {
	d := digitoDe(b[0])
	switch {
	case d >= 2:
		// Um dígito menor no começo basta: "5…" < "6…", e o dígito não é '0'.
		return alfabetoDaPosicao[d-1 : d]
	case d == 1:
		// Nada é menor que "1…" com um dígito só além do '0': "0" e o meio.
		return "0" + string(alfabetoDaPosicao[len(alfabetoDaPosicao)/2])
	default:
		// b começa em '0' (e, por não terminar em '0', tem mais um dígito).
		return "0" + posicaoAntesDe(b[1:])
	}
}

// posicaoDepoisDe devolve uma chave maior que a (válida).
func posicaoDepoisDe(a string) string {
	if a == "" {
		return string(alfabetoDaPosicao[len(alfabetoDaPosicao)/2])
	}
	d := digitoDe(a[0])
	if d < len(alfabetoDaPosicao)-1 {
		return alfabetoDaPosicao[d+1 : d+2]
	}
	// a começa no último dígito: fica o dígito e a conta segue no resto.
	return a[:1] + posicaoDepoisDe(a[1:])
}

// posicaoMeio devolve uma chave entre a e b, com a < b, b não vazia. a pode
// ser vazia (lida como "000…").
func posicaoMeio(a, b string) string {
	// O começo igual (a vazia tem zeros à direita) fica, e a conta segue no resto.
	n := 0
	for n < len(b) {
		var ca byte = '0'
		if n < len(a) {
			ca = a[n]
		}
		if ca != b[n] {
			break
		}
		n++
	}
	if n > 0 {
		if n >= len(b) { // a == b: quem chama já recusou
			return b
		}
		resto := ""
		if n < len(a) {
			resto = a[n:]
		}
		return b[:n] + posicaoMeio(resto, b[n:])
	}
	da := 0
	if a != "" {
		da = digitoDe(a[0])
	}
	db := digitoDe(b[0])
	if db-da > 1 {
		return alfabetoDaPosicao[(da+db+1)/2 : (da+db+1)/2+1]
	}
	// Dígitos vizinhos. Se b tem mais dígitos, o primeiro dele já fica entre
	// as duas (é menor que b e maior que a).
	if len(b) > 1 {
		return b[:1]
	}
	// b é um dígito só, vizinho do de a: fica o dígito de a e a conta segue
	// depois de a.
	resto := ""
	if len(a) > 1 {
		resto = a[1:]
	}
	return alfabetoDaPosicao[da:da+1] + posicaoDepoisDe(resto)
}
