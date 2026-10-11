package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Teste do Mauro de 01-out-2026: o ASR devolveu "ESOCIAL" e "ESOcial", e o
// Smart deixou assim, com "eSocial" no dicionário. O termo de grafia própria
// passa a sair na caixa do dicionário; o resto da caixa continua de quem
// ditou. Ver precisaCanonicalizar.

func TestGrafiaPropria(t *testing.T) {
	casos := map[string]bool{
		// Caixa mista que não é só a inicial.
		"eSocial": true, "DCTFWeb": true, "PostgreSQL": true, "TypeScript": true,
		"EFD-Reinf": true, "iPhone": true, "macOS": true, "GitHub Actions": true,
		// Sigla toda maiúscula: muitas são palavra comum ("das", "mei").
		"DAS": false, "MEI": false, "SPED": false, "PGDAS": false, "CI": false, "C#": false,
		// Só a inicial maiúscula, em cada palavra: a caixa é da frase.
		"Aurora": false, "Supabase": false, "Docker": false, "Thiago": false,
		"Simples Nacional": false, "Claude Code": false, "Rosângela Bittencourt": false,
		// Nenhuma maiúscula.
		"n8n": false, "log": false, "commit": false, "worker": false, "deploy": false,
		// A sigla e a inicial em palavras separadas: "das simples" é português.
		"SQL Server": false, "DAS Simples": false,
		// A primeira LETRA é a que conta como inicial, não o primeiro caractere.
		"99Freelas": false, "#Aurora": false,
		"": false,
	}
	for canonica, quero := range casos {
		if got := grafiaPropria(canonica); got != quero {
			t.Errorf("grafiaPropria(%q) = %v, quero %v", canonica, got, quero)
		}
	}
}

func TestPrecisaCanonicalizar(t *testing.T) {
	casos := []struct {
		escrito, canonica string
		colado            bool // o trecho encosta em separador de código no texto
		quero             bool
	}{
		// Grafia própria com maiúscula escrita: a caixa conta.
		{"ESOCIAL", "eSocial", false, true},
		{"ESOcial", "eSocial", false, true},
		{"Esocial", "eSocial", false, true}, // começo de frase: eSocial não vira ESocial
		{"Typescript", "TypeScript", false, true},
		{"Postgresql", "PostgreSQL", false, true},
		{"DCTFWEB", "DCTFWeb", false, true},
		{"eSocial", "eSocial", false, false}, // já está certo
		// Tudo minúsculo é a grafia correta de muito código e endereço
		// ("npm install typescript", "gov.br/esocial"): fica como veio.
		{"esocial", "eSocial", false, false},
		{"typescript", "TypeScript", false, false},
		{"postgresql", "PostgreSQL", false, false},
		{"dctfweb", "DCTFWeb", false, false},
		// Colado em separador de código: fica como veio, mesmo com maiúscula.
		{"Esocial", "eSocial", true, false},
		{"ESOCIAL", "eSocial", true, false},
		// Sem grafia própria: a caixa não conta.
		{"das", "DAS", false, false},
		{"Das", "DAS", false, false},
		{"mei", "MEI", false, false},
		{"aurora", "Aurora", false, false},
		{"SUPABASE", "Supabase", false, false},
		{"Log", "log", false, false},
		{"COMMIT", "commit", false, false},
		{"N8N", "n8n", false, false},
		// Diferença além da caixa: sempre, como antes — minúsculo e colado
		// também, porque a regra de caixa não é a que decide aqui.
		{"DCTF Web", "DCTFWeb", false, true},
		{"e social", "eSocial", false, true},
		{"e social", "eSocial", true, true},
	}
	for _, c := range casos {
		if got := precisaCanonicalizar(c.escrito, c.canonica, c.colado); got != c.quero {
			t.Errorf("precisaCanonicalizar(%q, %q, colado=%v) = %v, quero %v", c.escrito, c.canonica, c.colado, got, c.quero)
		}
	}
}

func resolvaNoDominio(t *testing.T, sr *SmartResolver, user, dominio, texto string) string {
	t.Helper()
	var dom *string
	if dominio != "" {
		dom = &dominio
	}
	out, err := sr.Resolve(context.Background(), ResolveRequest{
		UserID: user, Domain: dom,
		ASRResult: asr.ASRResult{Text: texto, Language: "pt"},
	})
	if err != nil {
		t.Fatalf("Resolve(%q): %v", texto, err)
	}
	return out.FinalText
}

// TestSmart_GrafiaPropriaComODicionarioVersionado: os casos do Mauro e os
// que não podem mudar, pelo Smart inteiro, com o dicionário de medição.
func TestSmart_GrafiaPropriaComODicionarioVersionado(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, sementeVersionada(t))
	casos := []struct{ nome, dominio, entra, quero string }{
		// O que muda.
		{"o do Mauro", "", "O ESOCIAL caiu de novo.", "O eSocial caiu de novo."},
		{"o outro do Mauro", "", "Manda o ESOcial hoje.", "Manda o eSocial hoje."},
		{"começo de frase, tudo maiúsculo", "", "ESOCIAL caiu de novo.", "eSocial caiu de novo."},
		{"começo de frase, inicial do ASR", "", "Esocial caiu de novo.", "eSocial caiu de novo."},
		{"TypeScript", "", "Vê se o Typescript reclama.", "Vê se o TypeScript reclama."},
		{"PostgreSQL", "", "Confere se o Postgresql aceitou.", "Confere se o PostgreSQL aceitou."},
		{"PostgreSQL no começo", "", "Postgresql caiu.", "PostgreSQL caiu."},
		{"DCTFWeb", "", "Transmiti a DCTFWEB hoje.", "Transmiti a DCTFWeb hoje."},
		{"no domínio dele", "contabil", "O ESOCIAL caiu.", "O eSocial caiu."},

		// O que não muda: sigla toda maiúscula, que também é palavra comum.
		{"das, preposição", "", "O boleto das empresas venceu.", "O boleto das empresas venceu."},
		{"Das no começo da frase", "", "Das empresas, só uma pagou.", "Das empresas, só uma pagou."},
		{"das no domínio da sigla", "contabil", "a maioria das empresas", "a maioria das empresas"},
		// Grafia própria escrita TODA em minúscula: é a grafia certa de muito
		// código e endereço, e a frase de prosa também fica como veio (o preço
		// da regra; ver precisaCanonicalizar).
		{"minúsculo", "", "transmiti o esocial", "transmiti o esocial"},
		{"mei e das minúsculos", "", "Ele é mei e paga o das.", "Ele é mei e paga o das."},
		{"sped minúsculo", "", "o sped já foi", "o sped já foi"},
		// Só a inicial maiúscula: a caixa é da frase.
		{"aurora", "", "A aurora estava linda.", "A aurora estava linda."},
		{"supabase", "", "Sobe no supabase.", "Sobe no supabase."},
		{"DOCKER", "", "O DOCKER caiu.", "O DOCKER caiu."},
		{"thiago", "", "falei com o thiago", "falei com o thiago"},
		// Palavra comum minúscula.
		{"Log no começo", "", "Log do backend cheio.", "Log do backend cheio."},
		{"COMMIT", "", "Faz o COMMIT agora.", "Faz o COMMIT agora."},
		{"Worker", "", "O Worker caiu.", "O Worker caiu."},
		{"N8N", "", "O N8N travou.", "O N8N travou."},
		// Fora do domínio do termo, o dicionário não o acha: nada muda.
		{"eSocial no domínio dev", "dev", "O ESOCIAL caiu.", "O ESOCIAL caiu."},
		{"TypeScript no domínio contábil", "contabil", "Vê se o Typescript reclama.", "Vê se o Typescript reclama."},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := resolvaNoDominio(t, sr, user, c.dominio, c.entra); got != c.quero {
				t.Errorf("%q (domínio %q) -> %q, quero %q", c.entra, c.dominio, got, c.quero)
			}
		})
	}
}

// TestSmart_GrafiaPropriaEmTermoDeDuasPalavras: o casamento de frase (2 a 4
// palavras) segue a mesma regra que o de palavra única.
func TestSmart_GrafiaPropriaEmTermoDeDuasPalavras(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, dictionary.Semente{
		Usuario: "m",
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "GitHub Actions", Prioridade: 90},
			{Canonica: "Simples Nacional", Prioridade: 90},
		},
	})
	for entra, quero := range map[string]string{
		"roda no Github actions":    "roda no GitHub Actions",
		"roda no GITHUB ACTIONS":    "roda no GitHub Actions",
		"roda no GitHub Actions":    "roda no GitHub Actions",
		"roda no github actions":    "roda no github actions",    // minúsculo: fica
		"o github/actions/checkout": "o github/actions/checkout", // caminho de repositório
		// Termo de duas palavras encostado em separador de código, de cada lado.
		"roda no Github Actions@v4":  "roda no Github Actions@v4",
		"usa x/Github Actions":       "usa x/Github Actions",
		"roda no Github Actions, ok": "roda no GitHub Actions, ok",
		"roda no Github Actions.":    "roda no GitHub Actions.",
		"o simples nacional vence":   "o simples nacional vence",
		"o SIMPLES NACIONAL vence":   "o SIMPLES NACIONAL vence",
	} {
		if got := resolverTexto(t, sr, user, entra); got != quero {
			t.Errorf("%q -> %q, quero %q", entra, got, quero)
		}
	}
}

// TestSmart_GrafiaPropriaRespeitaRestricao: a entrada restrita só vale no
// domínio dela, e a caixa também.
func TestSmart_GrafiaPropriaRespeitaRestricao(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, dictionary.Semente{
		Usuario: "m",
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "eSocial", Prioridade: 95, Dominio: "contabil", Restrito: true},
		},
	})
	for _, c := range []struct{ dominio, quero string }{
		{"", "O ESOCIAL caiu."},
		{"dev", "O ESOCIAL caiu."},
		{"contabil", "O eSocial caiu."},
	} {
		if got := resolvaNoDominio(t, sr, user, c.dominio, "O ESOCIAL caiu."); got != c.quero {
			t.Errorf("domínio %q: saiu %q, quero %q", c.dominio, got, c.quero)
		}
	}
}

// TestSmart_GrafiaPropriaDoPainelAgeSoPelaVariante: o termo que o painel
// inventou age só pela variante que a pessoa confirmou (searchTop descarta o
// casamento exato dele), e a caixa não abre exceção.
func TestSmart_GrafiaPropriaDoPainelAgeSoPelaVariante(t *testing.T) {
	s := comTermoDoPainel(t, dictionary.Semente{Usuario: "m"}, "iFood", "aifude")
	sr, user := smartDaSementeParaTeste(t, s)
	for entra, quero := range map[string]string{
		"o IFOOD atrasou":  "o IFOOD atrasou",
		"o Ifood atrasou":  "o Ifood atrasou",
		"o aifude atrasou": "o iFood atrasou",
	} {
		if got := resolverTexto(t, sr, user, entra); got != quero {
			t.Errorf("%q -> %q, quero %q", entra, got, quero)
		}
	}
}

// TestSmart_GrafiaPropriaNaoMexeEmCodigoNemEndereco: a grafia minúscula de
// URL, caminho, e-mail, pacote, comando e hashtag é a certa, e a caixa do
// dicionário não vale ali. Antes desta regra o casamento exato reescrevia
// "gov.br/esocial" e "npm install typescript" (o npm recusa maiúscula no nome
// do pacote; o caminho de URL e o nome de unit do systemctl distinguem
// maiúscula). A medida do dataset não vê isso: as referências dela têm o
// termo sempre na caixa canônica e nenhuma tem URL nem comando — este teste
// é quem cobre o contexto em que a minúscula é a certa.
func TestSmart_GrafiaPropriaNaoMexeEmCodigoNemEndereco(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, sementeVersionada(t))
	// Cada entrada vale nos domínios onde o termo existe no dicionário e num
	// domínio de fora, onde o dicionário não o acha (nada muda de qualquer jeito).
	casos := []struct{ nome, dominio, texto string }{
		// eSocial (domínio contábil).
		{"URL do portal", "contabil", "https://www.gov.br/esocial/pt-br"},
		{"domínio do portal", "contabil", "esocial.gov.br"},
		{"e-mail", "contabil", "esocial@empresa.com.br"},
		{"hashtag", "contabil", "#esocial"},
		{"URL do portal sem domínio", "", "https://www.gov.br/esocial/pt-br"},
		// TypeScript e PostgreSQL (domínio dev).
		{"npm install", "dev", "npm install typescript"},
		{"versão do pacote", "dev", "typescript@5.4"},
		{"systemctl", "dev", "systemctl restart postgresql"},
		{"apt install", "dev", "apt install postgresql"},
		{"string de conexão", "dev", "postgresql://localhost:5432/app"},
		{"caminho de módulo", "dev", "node_modules/typescript/lib"},
		{"arquivo de configuração", "dev", "typescript.config.json"},
		{"pacote com hífen", "dev", "typescript-eslint"},
		// Com maiúscula, mas colado no separador: a caixa também fica.
		{"e-mail, inicial maiúscula", "contabil", "Esocial@empresa.com.br"},
		{"domínio, inicial maiúscula", "contabil", "Esocial.gov.br"},
		{"caminho de URL, inicial maiúscula", "contabil", "https://www.gov.br/Esocial/pt-br"},
		{"hashtag, inicial maiúscula", "contabil", "#Esocial"},
		{"versão do pacote, inicial maiúscula", "dev", "Typescript@5.4"},
		{"caminho de módulo, inicial maiúscula", "dev", "node_modules/Typescript/lib"},
		{"pacote com hífen, inicial maiúscula", "dev", "Typescript-eslint"},
		{"unit com sublinhado", "dev", "Postgresql_16"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := resolvaNoDominio(t, sr, user, c.dominio, c.texto); got != c.texto {
				t.Errorf("%q (domínio %q) -> %q, quero o texto como veio", c.texto, c.dominio, got)
			}
		})
	}
}

// TestSmart_GrafiaPropriaComPontuacaoDeFraseContinua: a pontuação de uma
// frase não é separador de código. O mesmo termo, com maiúscula, encostado em
// vírgula, ponto final, dois-pontos, aspas e parênteses, sai na caixa do
// dicionário.
func TestSmart_GrafiaPropriaComPontuacaoDeFraseContinua(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, sementeVersionada(t))
	casos := []struct{ entra, quero string }{
		{"O ESOCIAL.", "O eSocial."},
		{"ESOCIAL: caiu de novo.", "eSocial: caiu de novo."},
		{"Typescript, Postgresql e DCTFWEB.", "TypeScript, PostgreSQL e DCTFWeb."},
		{"Veja (Typescript) e \"Postgresql\".", "Veja (TypeScript) e \"PostgreSQL\"."},
		{"Caiu o ESOCIAL - de novo.", "Caiu o eSocial - de novo."},
		{"Falei do ESOCIAL!", "Falei do eSocial!"},
	}
	for _, c := range casos {
		if got := resolverTexto(t, sr, user, c.entra); got != c.quero {
			t.Errorf("%q -> %q, quero %q", c.entra, got, c.quero)
		}
	}
}

func TestColaEmCodigo(t *testing.T) {
	// A palavra é a que está entre os colchetes; os colchetes saem do texto.
	casos := []struct {
		texto string
		quero bool
	}{
		// Separadores que colam sempre, de qualquer lado.
		{"gov.br/[Esocial]/pt-br", true},
		{"[Esocial]/pt-br", true},
		{"https://x.br/[Esocial]", true},
		{`C:\[Esocial]\x`, true},
		{`[Esocial]\x`, true},
		{"[Esocial]@empresa.com", true},
		{"fulano@[Esocial].com", true},
		{"#[Esocial]", true},
		{"[Esocial]#1", true},
		// Separadores que só colam com letra ou dígito do outro lado.
		{"www.[Esocial]", true},
		{"[Esocial].gov.br", true},
		{"foo-[Esocial]", true},
		{"[Esocial]-eslint", true},
		{"foo_[Esocial]", true},
		{"[Esocial]_v2", true},
		{"a:[Esocial]", true},
		{"[Esocial]:5432", true},
		// Pontuação de frase: não cola.
		{"o [Esocial].", false},
		{"o [Esocial]. Depois", false},
		{"[Esocial]: caiu", false},
		{"o [Esocial], o outro", false},
		{"o [Esocial] - de novo", false},
		{"o [Esocial]-", false},
		{"(o [Esocial])", false},
		{"\"[Esocial]\"", false},
		{"[Esocial]", false},
		{"o [Esocial] caiu", false},
		{"... [Esocial]", false},
		{"o [Esocial]...", false},
		// O separador ambíguo sem letra ou dígito do outro lado não cola: reticência
		// sem espaço, travessão de lista, dois-pontos solto, no começo do texto também.
		{"...[Esocial]", false},
		{"-[Esocial]", false},
		{"o -[Esocial]", false},
		{":[Esocial]", false},
		{"[Esocial]...", false},
	}
	for _, c := range casos {
		ini := strings.IndexRune(c.texto, '[')
		texto := strings.Replace(strings.Replace(c.texto, "[", "", 1), "]", "", 1)
		runes := []rune(texto)
		start := len([]rune(c.texto[:ini]))
		end := start + len([]rune("Esocial"))
		if got := colaEmCodigo(runes, start, end); got != c.quero {
			t.Errorf("colaEmCodigo(%q) = %v, quero %v", c.texto, got, c.quero)
		}
	}
}
