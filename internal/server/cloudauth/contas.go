package cloudauth

import (
	"errors"
	"strconv"
	"strings"
)

// Contas é a lista CONTAS_PERMITIDAS (ADR-019, decisão 5): os e-mails que
// podem entrar na nuvem. Qualquer outra conta Google é recusada sem criar
// nada. O valor zero não permite ninguém.
type Contas struct {
	emails map[string]struct{}
	// primeira é o primeiro e-mail da lista, na ordem em que foi escrita: a
	// conta que o login de teste oferece (build tag logindeteste).
	primeira string
}

// ErrContasVazia: a lista não tem nenhum e-mail. Na subida é erro, porque um
// servidor em que ninguém entra é configuração errada, não segurança.
var ErrContasVazia = errors.New("nenhum e-mail na lista")

// ParseContas lê CONTAS_PERMITIDAS: e-mails separados por vírgula, com
// espaços em volta ignorados. Guarda em minúsculas, como o banco (a 0001 exige
// email = lower(email)). Uma entrada que não parece e-mail é erro, e o erro
// diz a posição, nunca o valor.
func ParseContas(s string) (Contas, error) {
	c := Contas{emails: make(map[string]struct{})}
	for i, part := range strings.Split(s, ",") {
		e := NormalizarEmail(part)
		if e == "" {
			continue
		}
		if !pareceEmail(e) {
			return Contas{}, errors.New("a entrada " + strconv.Itoa(i+1) + " não é um e-mail")
		}
		c.emails[e] = struct{}{}
		if c.primeira == "" {
			c.primeira = e
		}
	}
	if len(c.emails) == 0 {
		return Contas{}, ErrContasVazia
	}
	return c, nil
}

// NormalizarEmail é a forma em que o e-mail é comparado e guardado: sem
// espaços em volta e em minúsculas.
func NormalizarEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Permite diz se o e-mail está na lista. Compara a forma normalizada.
func (c Contas) Permite(email string) bool {
	if c.emails == nil {
		return false
	}
	_, ok := c.emails[NormalizarEmail(email)]
	return ok
}

// Len é quantos e-mails a lista tem.
func (c Contas) Len() int { return len(c.emails) }

// Primeira é o primeiro e-mail da lista, como foi escrita (normalizado); ""
// na lista vazia.
func (c Contas) Primeira() string { return c.primeira }

// pareceEmail é uma checagem de formato, não de RFC 5322: um "@" só, as duas
// partes não vazias, um ponto no domínio, nada de espaço, controle ou
// caractere que o endereço de uma conta Google não tem.
func pareceEmail(e string) bool {
	if len(e) > 254 {
		return false
	}
	at := strings.IndexByte(e, '@')
	if at <= 0 || at != strings.LastIndexByte(e, '@') {
		return false
	}
	domain := e[at+1:]
	if domain == "" || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") ||
		strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return false
	}
	for _, r := range e {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`<>()[]\,;:"`, r) {
			return false
		}
	}
	return true
}
