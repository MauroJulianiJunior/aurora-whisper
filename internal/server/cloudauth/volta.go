package cloudauth

import "strings"

// VoltaPadrao é para onde o login leva quando não há volta: o app.
const VoltaPadrao = "/"

// maxVolta é o teto do banco (pending_logins.return_to).
const maxVolta = 256

// ValidarVolta confere a "volta" do POST /auth/start: o CAMINHO da própria
// origem para onde o callback leva a pessoa no fim do login (ADR-019,
// "Login"; o caso é a aprovação do pareamento, que exige sessão). Vazia é a
// VoltaPadrao.
//
// A regra é a mesma do CHECK de pending_logins.return_to (a 0012), que é a
// segunda guarda: começa com uma barra e não com duas (que o navegador leria
// como outro site), até 256 caracteres, e só caracteres de caminho e de query
// (letras e dígitos sem acento e . _ ~ / ? & = % : @ + -), sem barra
// invertida (que alguns navegadores leem como barra), espaço, quebra de linha
// ou "#". Assim o Location do 303 nunca sai da origem.
func ValidarVolta(s string) (string, bool) {
	if s == "" {
		return VoltaPadrao, true
	}
	if len(s) > maxVolta || s[0] != '/' || strings.HasPrefix(s, "//") {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			strings.IndexByte("._~/?&=%:@+-", c) >= 0
		if !ok {
			return "", false
		}
	}
	return s, true
}
