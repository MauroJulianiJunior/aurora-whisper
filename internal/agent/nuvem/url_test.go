package nuvem_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
)

// A URL da nuvem × a política (ADR-019, D7): o build de release só https; o
// de desenvolvimento só no modo portátil, com https ou http://localhost.
func TestNormalizarURL(t *testing.T) {
	release := nuvem.Politica{}
	releasePortatil := nuvem.Politica{Portatil: true}
	dev := nuvem.Politica{Dev: true, Portatil: true}
	devInstalado := nuvem.Politica{Dev: true}

	boas := []struct {
		bruta string
		pol   nuvem.Politica
		quero string
	}{
		{"https://nuvem.exemplo.com", release, "https://nuvem.exemplo.com"},
		{"  HTTPS://Nuvem.Exemplo.COM/  ", release, "https://nuvem.exemplo.com"},
		{"https://nuvem.exemplo.com:443", release, "https://nuvem.exemplo.com"},
		{"https://nuvem.exemplo.com:8443/", releasePortatil, "https://nuvem.exemplo.com:8443"},
		{"https://xn--nuvm-7oa.exemplo.com", release, "https://xn--nuvm-7oa.exemplo.com"},
		{"https://192.0.2.10", release, "https://192.0.2.10"},
		{"https://[2001:db8::1]:8443", release, "https://[2001:db8::1]:8443"},
		{"http://localhost:8080", dev, "http://localhost:8080"},
		{"http://LOCALHOST:8080/", dev, "http://localhost:8080"},
		{"http://localhost:80", dev, "http://localhost"},
		{"https://nuvem.exemplo.com", dev, "https://nuvem.exemplo.com"},
	}
	for _, c := range boas {
		got, err := nuvem.NormalizarURL(c.bruta, c.pol)
		if err != nil || got != c.quero {
			t.Errorf("NormalizarURL(%q, %+v) = %q, %v; quero %q", c.bruta, c.pol, got, err, c.quero)
		}
	}

	ruins := []struct {
		bruta string
		pol   nuvem.Politica
		frase string
	}{
		// Release: só https, em qualquer modo.
		{"http://localhost:8080", release, nuvem.MsgUseHTTPS},
		{"http://localhost:8080", releasePortatil, nuvem.MsgUseHTTPS},
		{"http://nuvem.exemplo.com", release, nuvem.MsgUseHTTPS},
		// Desenvolvimento fora do portátil: nada, nem https.
		{"https://nuvem.exemplo.com", devInstalado, nuvem.MsgSoPortatil},
		{"http://localhost:8080", devInstalado, nuvem.MsgSoPortatil},
		// Desenvolvimento: http só no localhost.
		{"http://127.0.0.1:8080", dev, nuvem.MsgHTTPSOuLocalhost},
		{"http://nuvem.exemplo.com", dev, nuvem.MsgHTTPSOuLocalhost},
		{"http://localhost.exemplo.com:8080", dev, nuvem.MsgHTTPSOuLocalhost},
		{"ftp://localhost", dev, nuvem.MsgHTTPSOuLocalhost},
		// O que não é uma origem.
		{"", release, nuvem.MsgUseHTTPS},
		{"nuvem.exemplo.com", release, nuvem.MsgUseHTTPS},
		{"//nuvem.exemplo.com", release, nuvem.MsgUseHTTPS},
		{"https://", release, nuvem.MsgUseHTTPS},
		{"https:nuvem.exemplo.com", release, nuvem.MsgUseHTTPS},
		{"https://eu:senha@nuvem.exemplo.com", release, nuvem.MsgUseHTTPS},
		{"https://eu@nuvem.exemplo.com", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com/app", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com/?x=1", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com?", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com#x", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com#", release, nuvem.MsgUseHTTPS},
		{"https://nuvém.exemplo.com", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com\\@outro.com", release, nuvem.MsgUseHTTPS},
		{"https://nuvem exemplo.com", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com:0", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com:65536", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com:08443", release, nuvem.MsgUseHTTPS},
		{"https://nuvem.exemplo.com:", release, nuvem.MsgUseHTTPS},
		{"https://-nuvem.exemplo.com", release, nuvem.MsgUseHTTPS},
		{"https://" + strings.Repeat("a", 250) + ".com", release, nuvem.MsgUseHTTPS},
	}
	for _, c := range ruins {
		got, err := nuvem.NormalizarURL(c.bruta, c.pol)
		var e *nuvem.Erro
		if !errors.As(err, &e) || e.Mensagem != c.frase || e.Status != http.StatusBadRequest || got != "" {
			t.Errorf("NormalizarURL(%q, %+v) = %q, %v; quero a recusa %q", c.bruta, c.pol, got, err, c.frase)
		}
	}
}

func TestNomeNoCofre(t *testing.T) {
	if got := nuvem.NomeNoCofre(false); got != "nuvem" {
		t.Errorf("instalado: %q", got)
	}
	if got := nuvem.NomeNoCofre(true); got != "nuvem-portatil" {
		t.Errorf("portátil: %q", got)
	}
}

func TestNomeDoPC(t *testing.T) {
	for bruto, quero := range map[string]string{
		"DELL-G15":                     "DELL-G15",
		"  PC  ":                       "PC",
		"":                             "PC",
		"\x00\x01":                     "PC",
		"PC\nda\tSala":                 "PCdaSala",
		strings.Repeat("é", 90):        strings.Repeat("é", 80),
		"PC\xffquebrado":               "PCquebrado",
		strings.Repeat("a", 79) + " b": strings.Repeat("a", 79),
	} {
		if got := nuvem.NomeDoPC(bruto); got != quero {
			t.Errorf("NomeDoPC(%q) = %q, quero %q", bruto, got, quero)
		}
	}
}
