package nuvem

import (
	"net/url"
	"strconv"
	"strings"
)

// Politica é o que o build e o modo permitem (ADR-019, D7).
type Politica struct {
	// Dev: o build de desenvolvimento (sem -X main.version). Só conecta no
	// modo portátil (o bin\), com a conta de teste, para nunca ligar os
	// dados reais à nuvem por engano.
	Dev bool
	// Portatil: o Agent roda no modo portátil (portatil ou portatil-legado:
	// os dados ao lado do executável).
	Portatil bool
}

// As frases das recusas (08-ui-ux-spec.md, "Nuvem (no PC)").
const (
	MsgUseHTTPS           = "Use um endereço https://."
	MsgHTTPSOuLocalhost   = "Use https:// ou http://localhost."
	MsgSoPortatil         = `Este build de teste só conecta da pasta bin\ (modo portátil).`
	MsgSemNuvem           = "Não deu para falar com a nuvem. Confira o endereço."
	MsgOutraConta         = "Este PC já está ligado a outra conta."
	MsgJaConectado        = "Este PC já está conectado. Desconecte antes de conectar de novo."
	MsgCofre              = "Não consegui usar o Gerenciador de Credenciais do Windows."
	MsgInstalacao         = "Não consegui ler ou gravar os dados desta instalação."
	MsgPainelNaoSubiu     = "O painel local ainda não está pronto. Abra o painel de novo pela bandeja."
	maxURLDaNuvem         = 256
	portaPadraoHTTPS      = "443"
	portaPadraoHTTP       = "80"
	hostDeDesenvolvimento = "localhost"
)

// NormalizarURL confere o endereço da nuvem e o devolve como origem:
// esquema://host[:porta], em minúsculas, sem a porta padrão do esquema. Recusa
// usuário e senha, caminho além de "/", busca, fragmento, host fora do ASCII
// e mais de 256 caracteres. O erro é um *Erro com a frase para a tela.
//
// A política: o build de release só aceita https, em qualquer modo; o de
// desenvolvimento só conecta no modo portátil e aceita https ou
// http://localhost. Tudo isso antes de qualquer rede.
func NormalizarURL(bruta string, pol Politica) (string, error) {
	if pol.Dev && !pol.Portatil {
		return "", recusa(MsgSoPortatil)
	}
	esquema := MsgUseHTTPS
	if pol.Dev {
		esquema = MsgHTTPSOuLocalhost
	}
	bruta = strings.TrimSpace(bruta)
	if bruta == "" || len(bruta) > maxURLDaNuvem || strings.ContainsAny(bruta, "?#\\ \t\r\n") {
		return "", recusa(esquema)
	}
	u, err := url.Parse(bruta)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", recusa(esquema)
	}
	host := strings.ToLower(u.Hostname())
	porta := u.Port()
	if !hostASCII(host) || (porta != "" && !portaValida(porta)) || (porta == "" && strings.HasSuffix(u.Host, ":")) {
		return "", recusa(esquema)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if porta == portaPadraoHTTPS {
			porta = ""
		}
		return origem("https", host, porta), nil
	case "http":
		if !pol.Dev || host != hostDeDesenvolvimento {
			return "", recusa(esquema)
		}
		if porta == portaPadraoHTTP {
			porta = ""
		}
		return origem("http", host, porta), nil
	}
	return "", recusa(esquema)
}

func origem(esquema, host, porta string) string {
	if strings.Contains(host, ":") { // IPv6
		host = "[" + host + "]"
	}
	if porta != "" {
		host += ":" + porta
	}
	return esquema + "://" + host
}

// hostASCII: um nome de host em ASCII (letras, dígitos, ponto, hífen) ou um
// IPv6 entre colchetes já sem eles (dígitos hexadecimais, ":" e "."). Um
// nome com acento ou em punycode visual fica de fora: quem quiser usa o nome
// já convertido (xn--).
func hostASCII(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == ':') {
			return false
		}
	}
	return !strings.HasPrefix(h, ".") && !strings.HasPrefix(h, "-")
}

func portaValida(p string) bool {
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == p
}
