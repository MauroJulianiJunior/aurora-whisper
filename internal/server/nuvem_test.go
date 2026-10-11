package server_test

import (
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// painelEnorme tem mais arquivos do que um build do painel tem.
func painelEnorme() fstest.MapFS {
	p := contracttest.PainelDeTeste()
	for i := range 600 {
		p["assets/a"+strconv.Itoa(i)+".js"] = &fstest.MapFile{Data: []byte("x")}
	}
	return p
}

// NewCloud recusa uma configuração incompleta ou perigosa, sem mostrar
// valor nenhum. O serve confere o mesmo antes (cmd/server/config.go); esta é
// a segunda guarda, para quem montar o Cloud de outro jeito.
func TestNewCloud_RecusaConfiguracaoErrada(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	const segredo = "SEGREDO-DE-SESSAO-DO-TESTE-0123456789abcdef"
	const pepper = "PEPPER-DO-TOKEN-DO-TESTE-0123456789abcdefgh"
	base := "http://localhost:8080"
	if server.LoginDeTesteCompilado {
		t.Skip("com o login de teste, a base https já é recusada por outro motivo; ver login_de_teste_test.go")
	}
	boa := func() server.CloudConfig {
		return server.CloudConfig{
			Store: mem, Accounts: mem, BaseURL: base,
			SessionSecret: []byte(segredo), DeviceTokenPepper: []byte(pepper), Contas: contas,
			Google: cloudauth.NovoGoogle("cliente", "secret-do-client", base+"/auth/google/callback"),
			Painel: contracttest.PainelDeTeste(),
		}
	}
	// comPainel troca o build do painel por um com um arquivo a mais (ou a
	// menos, com nil).
	comPainel := func(nome string, f *fstest.MapFile) func(c *server.CloudConfig) {
		return func(c *server.CloudConfig) {
			p := contracttest.PainelDeTeste()
			if f == nil {
				delete(p, nome)
			} else {
				p[nome] = f
			}
			c.Painel = p
		}
	}
	arquivo := &fstest.MapFile{Data: []byte("x")}
	if _, err := server.NewCloud(boa()); err != nil {
		t.Fatalf("a configuração boa foi recusada: %v", err)
	}
	comBase := func(b string) func(c *server.CloudConfig) {
		return func(c *server.CloudConfig) {
			c.BaseURL = b
			c.Google.RedirectURL = strings.TrimSuffix(b, "/") + "/auth/google/callback"
		}
	}
	cases := map[string]func(c *server.CloudConfig){
		"sem Store":    func(c *server.CloudConfig) { c.Store = nil },
		"sem Accounts": func(c *server.CloudConfig) { c.Accounts = nil },
		// Nas bases, o redirect acompanha a base: só a regra da base pode recusar.
		"base vazia":                comBase(""),
		"http fora de localhost":    comBase("http://nuvem.exemplo.test"),
		"http em 192.168":           comBase("http://192.168.0.10:8080"),
		"base ftp":                  comBase("ftp://localhost"),
		"base com caminho":          comBase("https://nuvem.exemplo.test/app"),
		"base com query":            comBase("https://nuvem.exemplo.test?x=1"),
		"base com usuário":          comBase("https://u:" + segredo + "@nuvem.exemplo.test"),
		"segredo curto":             func(c *server.CloudConfig) { c.SessionSecret = []byte("curto") },
		"pepper curto":              func(c *server.CloudConfig) { c.DeviceTokenPepper = []byte("curto") },
		"segredos iguais":           func(c *server.CloudConfig) { c.DeviceTokenPepper = []byte(segredo) },
		"lista vazia":               func(c *server.CloudConfig) { c.Contas = cloudauth.Contas{} },
		"sem Google":                func(c *server.CloudConfig) { c.Google = nil },
		"Google sem secret":         func(c *server.CloudConfig) { c.Google.ClientSecret = "" },
		"redirect de outra origem":  func(c *server.CloudConfig) { c.Google.RedirectURL = "http://localhost:9999/auth/google/callback" },
		"redirect de outro caminho": func(c *server.CloudConfig) { c.Google.RedirectURL = base + "/callback" },
		// O build do painel (F4): obrigatório, com o index.html, e só com o
		// que o servidor sabe servir.
		"sem o painel":               func(c *server.CloudConfig) { c.Painel = nil },
		"painel sem index.html":      comPainel("index.html", nil),
		"painel com outra pasta":     comPainel("icones/a.png", arquivo),
		"assets com subpasta":        comPainel("assets/sub/a.js", arquivo),
		"extensão desconhecida":      comPainel("assets/a.map", arquivo),
		"arquivo sem extensão":       comPainel("LEIAME", arquivo),
		"nome que não é do build":    comPainel("a b.js", arquivo),
		"nome que a nuvem já usa":    comPainel("pagina.css", arquivo),
		"arquivo grande demais":      comPainel("assets/grande.js", &fstest.MapFile{Data: make([]byte, 8<<20+1)}),
		"index.html que é uma pasta": func(c *server.CloudConfig) { c.Painel = fstest.MapFS{"index.html/a.txt": arquivo} },
		"painel com arquivos demais": func(c *server.CloudConfig) { c.Painel = painelEnorme() },
	}
	for name, mudar := range cases {
		c := boa()
		mudar(&c)
		_, err := server.NewCloud(c)
		if err == nil {
			t.Errorf("%s: NewCloud aceitou", name)
			continue
		}
		if strings.Contains(err.Error(), segredo) || strings.Contains(err.Error(), "secret-do-client") {
			t.Errorf("%s: o erro mostra um valor: %v", name, err)
		}
	}
	// https em qualquer servidor vale.
	c := boa()
	c.BaseURL = "https://nuvem.exemplo.test"
	c.Google.RedirectURL = "https://nuvem.exemplo.test/auth/google/callback"
	if _, err := server.NewCloud(c); err != nil {
		t.Errorf("https: %v", err)
	}
}
