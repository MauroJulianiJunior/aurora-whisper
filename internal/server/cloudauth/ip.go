package cloudauth

import (
	"errors"
	"net/netip"
	"strings"
)

// ProxyConfiavel é PROXY_CONFIAVEL: o endereço (ou a rede, em CIDR) do proxy
// que termina o HTTPS na frente do servidor. Só de um pedido vindo dele o
// X-Forwarded-For vale; de qualquer outro, quem pede é o RemoteAddr. O valor
// zero não confia em ninguém.
type ProxyConfiavel struct {
	rede netip.Prefix
	ok   bool
}

// ParseProxyConfiavel lê PROXY_CONFIAVEL: vazio (nenhum proxy), um IP
// ("127.0.0.1", "::1") ou uma rede ("172.18.0.0/16").
func ParseProxyConfiavel(s string) (ProxyConfiavel, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ProxyConfiavel{}, nil
	}
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return ProxyConfiavel{}, errors.New("não é um IP nem uma rede CIDR")
		}
		return ProxyConfiavel{rede: p.Masked(), ok: true}, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return ProxyConfiavel{}, errors.New("não é um IP nem uma rede CIDR")
	}
	a = a.Unmap()
	return ProxyConfiavel{rede: netip.PrefixFrom(a, a.BitLen()), ok: true}, nil
}

// Ligado diz se há um proxy configurado (PROXY_CONFIAVEL não vazia). Vai à
// linha "servindo" e ao aviso do X-Forwarded-For (F11c), que só dizem isso.
func (p ProxyConfiavel) Ligado() bool { return p.ok }

// Confia diz se o RemoteAddr é o proxy confiável (então o X-Forwarded-For
// dele vale).
func (p ProxyConfiavel) Confia(remoteAddr string) bool {
	addr, ok := endereco(remoteAddr)
	return ok && p.ok && p.rede.Contains(addr)
}

// IPDoCliente devolve o IP de quem pede, como texto, para servir de chave do
// limite de tentativas (nunca vai a log). É o RemoteAddr, exceto quando o
// RemoteAddr é o proxy confiável: aí é o ÚLTIMO endereço do X-Forwarded-For,
// o que o próprio proxy acrescentou (os anteriores vêm do cliente, que pode
// escrever o que quiser).
func (p ProxyConfiavel) IPDoCliente(remoteAddr string, xff []string) string {
	addr, ok := endereco(remoteAddr)
	if !ok {
		return remoteAddr
	}
	if p.Confia(remoteAddr) {
		if ultimo, ok := ultimoSalto(xff); ok {
			return ultimo.String()
		}
	}
	return addr.String()
}

// ChaveDoLimite é a chave do limite de tentativas para o IP que
// IPDoCliente devolve: o IPv4 inteiro, e o IPv6 agrupado por /64. Quem tem
// um /64 (o normal de uma casa, de um celular ou de uma VPS) gira endereços
// dentro dele à vontade e escaparia de um limite por endereço. Um texto que
// não é IP volta como está.
func ChaveDoLimite(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	a = a.Unmap().WithZone("")
	if a.Is4() {
		return a.String()
	}
	return netip.PrefixFrom(a, 64).Masked().String()
}

func endereco(remoteAddr string) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return ap.Addr().Unmap(), true
	}
	if a, err := netip.ParseAddr(remoteAddr); err == nil {
		return a.Unmap(), true
	}
	return netip.Addr{}, false
}

func ultimoSalto(xff []string) (netip.Addr, bool) {
	var last string
	for _, h := range xff {
		for _, part := range strings.Split(h, ",") {
			if part = strings.TrimSpace(part); part != "" {
				last = part
			}
		}
	}
	if last == "" {
		return netip.Addr{}, false
	}
	return endereco(last)
}
