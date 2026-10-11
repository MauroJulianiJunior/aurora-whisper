package cloudauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// TamanhoSegredo é o tamanho de um segredo de NovoSegredo: 32 bytes em
// base64url sem preenchimento.
const TamanhoSegredo = 43

// NovoSegredo devolve 256 bits aleatórios em base64url, sem preenchimento:
// o id da sessão, o state, o cookie __Host-pre, o verificador do PKCE e o
// nonce. O crypto/rand da biblioteca padrão nunca devolve erro (Go 1.24 em
// diante: sem fonte de entropia, o programa para).
func NovoSegredo() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// PareceSegredo diz se s tem o formato de um segredo de NovoSegredo. Serve
// para recusar lixo (um cookie forjado de 1 MiB, um Bearer qualquer) antes de
// ir ao banco; não prova nada além do formato.
func PareceSegredo(s string) bool {
	if len(s) != TamanhoSegredo {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// Hash é o SHA-256 de s: o que o banco guarda do state e do cookie
// __Host-pre (pending_logins), que só vivem cinco minutos.
func Hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// HMAC é o HMAC-SHA256 de s com key: o que o banco guarda do id da sessão
// (com SESSION_SECRET) e do token do aparelho (com DEVICE_TOKEN_PEPPER). Quem
// lê o banco sem a chave não ganha sessão nem token.
func HMAC(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

// AlfabetoDaConferencia são os caracteres do código de conferência do
// pareamento (ADR-019, F6): dígitos e maiúsculas sem 0, 1, I, L e O, que se
// confundem na leitura. 31 símbolos.
const AlfabetoDaConferencia = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// TamanhoDaConferencia é quantos caracteres o código tem.
const TamanhoDaConferencia = 4

// NovaConferencia devolve um código de conferência por crypto/rand, sem viés
// (o byte que passaria do último múltiplo de 31 é descartado). O código NÃO
// protege nada sozinho: é só para a pessoa conferir que a página e o painel
// do PC falam do mesmo pedido.
func NovaConferencia() string {
	const n = len(AlfabetoDaConferencia)
	const teto = 256 - 256%n // 248: os bytes de 0 a 247 caem iguais nos 31
	out := make([]byte, 0, TamanhoDaConferencia)
	var b [16]byte
	for len(out) < TamanhoDaConferencia {
		_, _ = rand.Read(b[:])
		for _, x := range b {
			if int(x) < teto && len(out) < TamanhoDaConferencia {
				out = append(out, AlfabetoDaConferencia[int(x)%n])
			}
		}
	}
	return string(out)
}

// PareceConferencia diz se s tem o formato de um código de NovaConferencia.
func PareceConferencia(s string) bool {
	if len(s) != TamanhoDaConferencia {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(AlfabetoDaConferencia, rune(s[i])) {
			return false
		}
	}
	return true
}

// DesafioS256 é o code_challenge do PKCE (RFC 7636, 4.2): o SHA-256 do
// verificador, em base64url sem preenchimento.
func DesafioS256(verificador string) string {
	h := sha256.Sum256([]byte(verificador))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
