package dictionary

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Os testes do PC que recebe o aprendido da nuvem (F8a): a tabela, os casos
// de borda, o lote e o arquivo. Tudo em memória ou em t.TempDir(): nenhum
// teste toca o bin\dicionario.json.

// pcQueRecebe é o dicionário do PC que recebe: um termo com domínio no mapa
// (ECF, ECD), um restrito a um domínio (commit), um sem domínio (SPED), um
// restrito sem domínio (Fantasma), um com domínio fora do mapa (Folha, "rh") e
// um desligado.
func pcQueRecebe() Semente {
	return Semente{
		Usuario:     "mauro",
		Dominios:    map[string][]string{"contabil": {"SistemaContabil.exe"}, "dev": {"Code.exe"}},
		AliasOrigem: map[string]string{"DAAS": "cont-04"},
		Entradas: []EntradaSemente{
			{Canonica: "ECF", Prioridade: 90, Dominio: "contabil", Alias: []string{"ECFX"}},
			{Canonica: "ECD", Prioridade: 90, Dominio: "contabil", Alias: []string{"DAAS"}},
			{Canonica: "commit", Prioridade: 75, Dominio: "dev", Restrito: true, AliasRestrito: []string{"comite"}},
			{Canonica: "SPED", Prioridade: 95},
			{Canonica: "Fantasma", Prioridade: 60, Restrito: true},
			{Canonica: "Folha", Prioridade: 80, Dominio: "rh"},
			{Canonica: "Desligado", Prioridade: 10, Alias: []string{"deslig"}, Inativa: true},
		},
	}
}

// comPainel devolve s com a variante v, de origem "painel", na lista dada do
// termo (como se este PC a tivesse aprendido, ou recebido, antes).
func comPainel(t *testing.T, s Semente, termo, v string, emRestrito bool) Semente {
	t.Helper()
	c := s.copiaProfunda()
	i := c.indiceDoTermo(termo)
	if i < 0 {
		t.Fatalf("sem o termo %q", termo)
	}
	if emRestrito {
		c.Entradas[i].AliasRestrito = append(c.Entradas[i].AliasRestrito, v)
	} else {
		c.Entradas[i].Alias = append(c.Entradas[i].Alias, v)
	}
	if c.AliasOrigem == nil {
		c.AliasOrigem = map[string]string{}
	}
	c.AliasOrigem[v] = OrigemPainel
	return c
}

// comAMao devolve s com a variante v no alias do termo, sem origem (posta à
// mão).
func comAMao(t *testing.T, s Semente, termo, v string) Semente {
	t.Helper()
	c := s.copiaProfunda()
	i := c.indiceDoTermo(termo)
	if i < 0 {
		t.Fatalf("sem o termo %q", termo)
	}
	c.Entradas[i].Alias = append(c.Entradas[i].Alias, v)
	return c
}

// aplicarUmPar roda um lote de um par só.
func aplicarUmPar(t *testing.T, s Semente, p ParDaNuvem, c ConferenciasDoLote) (Semente, Lote) {
	t.Helper()
	antes := emJSON(t, s)
	depois, lote, err := s.AplicarPares([]ParDaNuvem{p}, c)
	if err != nil {
		t.Fatalf("AplicarPares: %v", err)
	}
	if emJSON(t, s) != antes {
		t.Fatal("AplicarPares mudou a semente de entrada")
	}
	if len(lote.Resultados) != 1 {
		t.Fatalf("%d resultados para um par", len(lote.Resultados))
	}
	if got := lote.Resultados[0].ID; got != IDDoPar(p.Variante) {
		t.Errorf("ID = %s, want IDDoPar(variante)", got)
	}
	return depois, lote
}

// ondeEsta diz em que lista de que termo a variante está ("" se em nenhum).
func ondeEsta(s Semente, v string) string {
	var out []string
	for _, e := range s.Entradas {
		for _, a := range e.Alias {
			if a == v {
				out = append(out, e.Canonica+".alias")
			}
		}
		for _, a := range e.AliasRestrito {
			if a == v {
				out = append(out, e.Canonica+".alias_restrito")
			}
		}
	}
	return strings.Join(out, ",")
}

// ondeEstaAChave é ondeEsta para qualquer grafia da mesma variante (a mesma
// chave): o arquivo guarda a grafia daqui.
func ondeEstaAChave(s Semente, chave string) string {
	var out []string
	for _, e := range s.Entradas {
		for _, a := range e.Alias {
			if ChaveDoPar(a) == chave {
				out = append(out, e.Canonica+".alias")
			}
		}
		for _, a := range e.AliasRestrito {
			if ChaveDoPar(a) == chave {
				out = append(out, e.Canonica+".alias_restrito")
			}
		}
	}
	return strings.Join(out, ",")
}

// TestAplicarPares_TabelaDoPCQueRecebe é a tabela "As regras do PC que
// recebe" da ADR-019, linha a linha, e as checagens de depois dela.
func TestAplicarPares_TabelaDoPCQueRecebe(t *testing.T) {
	casos := []struct {
		nome   string
		par    ParDaNuvem
		motivo MotivoDoPar // "" é aplicado
		onde   string      // aplicado: onde a variante ficou
		proj   ParDoPainel // aplicado: a projeção, sem ID nem Variante
	}{
		// Linha 1: o mapa dominios vem primeiro, mesmo com o termo local no domínio X.
		{"X fora do mapa, com o termo daqui no domínio X", ParDaNuvem{Variante: "fl", Termo: "Folha", Dominio: "rh"}, MotivoDominioAusente, "", ParDoPainel{}},
		{"X fora do mapa, restrito, sem o termo", ParDaNuvem{Variante: "zz", Termo: "Novo", Restrito: true, Dominio: "rh"}, MotivoDominioAusente, "", ParDoPainel{}},
		// As linhas 1 e 2 cruzadas: X fora do mapa E o termo daqui restrito
		// sem domínio. As duas não gravam, mas o motivo na tela muda: a
		// primeira que casa decide, e o mapa dominios vem primeiro.
		{"X fora do mapa, com o termo daqui restrito sem domínio", ParDaNuvem{Variante: "fant", Termo: "Fantasma", Dominio: "rh"}, MotivoDominioAusente, "", ParDoPainel{}},
		// Linha 2: o termo daqui restrito sem domínio nunca age.
		{"o termo daqui restrito sem domínio, par sem domínio", ParDaNuvem{Variante: "fant", Termo: "Fantasma"}, MotivoNaoAgiria, "", ParDoPainel{}},
		{"o termo daqui restrito sem domínio, par com domínio do mapa", ParDaNuvem{Variante: "fant", Termo: "Fantasma", Dominio: "dev"}, MotivoNaoAgiria, "", ParDoPainel{}},
		// Linha 3: sem domínio, a palavra não existe: a D2 a cria.
		{"sem domínio, a palavra não existe: cria (D2)", ParDaNuvem{Variante: "n oito n", Termo: "n8n"}, "", "n8n.alias", ParDoPainel{Termo: "n8n"}},
		// Linha 4: sem domínio, a palavra existe: em alias, onde o termo daqui age.
		{"sem domínio, a palavra existe sem domínio", ParDaNuvem{Variante: "espede", Termo: "sped"}, "", "SPED.alias", ParDoPainel{Termo: "SPED"}},
		{"sem domínio, a palavra existe com domínio", ParDaNuvem{Variante: "sf", Termo: "ECF"}, "", "ECF.alias", ParDoPainel{Termo: "ECF", Dominio: "contabil"}},
		{"sem domínio, a palavra existe restrita a um domínio", ParDaNuvem{Variante: "cmt", Termo: "commit"}, "", "commit.alias", ParDoPainel{Termo: "commit", Restrito: true, Dominio: "dev"}},
		// Linha 5: X sem restrição, o termo daqui no domínio X.
		{"X sem restrição, o termo daqui em X sem restrição", ParDaNuvem{Variante: "sf", Termo: "ECF", Dominio: "contabil"}, "", "ECF.alias", ParDoPainel{Termo: "ECF", Dominio: "contabil"}},
		{"X sem restrição, o termo daqui restrito a X", ParDaNuvem{Variante: "cmt", Termo: "commit", Dominio: "dev"}, "", "commit.alias", ParDoPainel{Termo: "commit", Restrito: true, Dominio: "dev"}},
		// Linha 6: X restrita, o termo daqui restrito a X.
		{"X restrita, o termo daqui restrito a X", ParDaNuvem{Variante: "cmt", Termo: "commit", Restrito: true, Dominio: "dev"}, "", "commit.alias", ParDoPainel{Termo: "commit", Restrito: true, Dominio: "dev"}},
		// Linha 7: X restrita, o termo daqui em X sem restrição: alias_restrito.
		{"X restrita, o termo daqui em X sem restrição", ParDaNuvem{Variante: "efe", Termo: "ECF", Restrito: true, Dominio: "contabil"}, "", "ECF.alias_restrito", ParDoPainel{Termo: "ECF", Restrito: true, Dominio: "contabil", EmAliasRestrito: true}},
		// Linha 8: X, a palavra não existe: não cria (D2, escolha menor 6).
		{"X, a palavra não existe", ParDaNuvem{Variante: "xx", Termo: "Novo", Dominio: "dev"}, MotivoPalavraAusenteComDominio, "", ParDoPainel{}},
		{"X restrita, a palavra não existe", ParDaNuvem{Variante: "xx", Termo: "Novo", Restrito: true, Dominio: "dev"}, MotivoPalavraAusenteComDominio, "", ParDoPainel{}},
		// Linha 9: X, o termo daqui sem domínio, ou com outro.
		{"X, o termo daqui sem domínio", ParDaNuvem{Variante: "spd", Termo: "SPED", Dominio: "contabil"}, MotivoRestricaoNaoCabe, "", ParDoPainel{}},
		{"X, o termo daqui com outro domínio", ParDaNuvem{Variante: "sf", Termo: "ECF", Dominio: "dev"}, MotivoRestricaoNaoCabe, "", ParDoPainel{}},
		{"X restrita, o termo daqui com outro domínio", ParDaNuvem{Variante: "sf", Termo: "commit", Restrito: true, Dominio: "contabil"}, MotivoRestricaoNaoCabe, "", ParDoPainel{}},

		// Depois da tabela, só nas linhas que gravam: as integridades do Aprender.
		{"a variante é de outro termo aqui, sem a origem", ParDaNuvem{Variante: "DAAS", Termo: "ECF"}, MotivoVarianteDeOutroTermo, "", ParDoPainel{}},
		{"a variante é outro termo aqui", ParDaNuvem{Variante: "sped", Termo: "ECF"}, MotivoVarianteDeOutroTermo, "", ParDoPainel{}},
		{"o termo novo já é variante de outro", ParDaNuvem{Variante: "xyz", Termo: "ECFX"}, MotivoVarianteDeOutroTermo, "", ParDoPainel{}},
		{"o termo daqui está desligado", ParDaNuvem{Variante: "dslg", Termo: "Desligado"}, MotivoTermoDesligado, "", ParDoPainel{}},
		{"a variante é o próprio termo (o Aprender recusaria)", ParDaNuvem{Variante: "ecf", Termo: "ECF"}, MotivoNaoAgiria, "", ParDoPainel{}},
		{"a variante de cinco palavras (o Smart nunca a procura)", ParDaNuvem{Variante: "um dois tres quatro cinco", Termo: "ECF"}, MotivoNaoAgiria, "", ParDoPainel{}},
		{"o termo de 81 runas (a nuvem recusaria)", ParDaNuvem{Variante: "sf", Termo: strings.Repeat("a", 81)}, MotivoNaoAgiria, "", ParDoPainel{}},
		// O restrito sem domínio, sem a pré-checagem, passaria na linha "sem
		// domínio" e valeria em todo lugar: alargaria.
		{"restrito sem domínio (a nuvem recusaria)", ParDaNuvem{Variante: "comite2", Termo: "commit", Restrito: true}, MotivoNaoAgiria, "", ParDoPainel{}},
	}
	vistos := map[MotivoDoPar]bool{}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s := pcQueRecebe()
			depois, lote := aplicarUmPar(t, s, c.par, ConferenciasDoLote{})
			r := lote.Resultados[0]
			if c.motivo != "" {
				vistos[c.motivo] = true
				if r.Situacao != SituacaoNaoAplicado || r.Motivo != c.motivo {
					t.Fatalf("resultado = %+v, want nao_aplicado %s", r, c.motivo)
				}
				if lote.Mudou || emJSON(t, depois) != emJSON(t, s) {
					t.Errorf("o par não aplicado mudou o dicionário:\n%s", emJSON(t, depois))
				}
				if r.Projecao != nil {
					t.Errorf("Projecao = %+v, want nil", r.Projecao)
				}
				return
			}
			if r.Situacao != SituacaoAplicado || r.Motivo != "" {
				t.Fatalf("resultado = %+v, want aplicado", r)
			}
			if !lote.Mudou {
				t.Error("aplicado sem Mudou")
			}
			if got := ondeEsta(depois, c.par.Variante); got != c.onde {
				t.Errorf("a variante está em %q, want %q", got, c.onde)
			}
			if depois.AliasOrigem[c.par.Variante] != OrigemPainel {
				t.Error("o par recebido não entrou com a origem painel (o Desfazer não valeria nele)")
			}
			want := c.proj
			want.ID, want.Variante = IDDoPar(c.par.Variante), c.par.Variante
			if r.Projecao == nil || *r.Projecao != want {
				t.Errorf("Projecao = %+v, want %+v", r.Projecao, want)
			}
		})
	}
	for _, m := range MotivosDoPar() {
		if !vistos[m] && m != MotivoArquivoIndisponivel && m != MotivoPalavraComum {
			t.Errorf("nenhuma linha da tabela prova o motivo %s", m)
		}
	}
}

// TestAplicarPares_CriaAPalavraComoOAprender: a palavra que a D2 cria é a do
// Aprender: prioridade 50, origem "painel" (AliasOnly: só age pela troca e não
// vai ao prompt), sem domínio.
func TestAplicarPares_CriaAPalavraComoOAprender(t *testing.T) {
	s := pcQueRecebe()
	depois, _ := aplicarUmPar(t, s, ParDaNuvem{Variante: "n oito n", Termo: "n8n"}, ConferenciasDoLote{})
	doAprender, _, err := s.AprenderVariante("n8n", "n oito n")
	if err != nil {
		t.Fatal(err)
	}
	if emJSON(t, depois) != emJSON(t, doAprender) {
		t.Fatalf("o lote criou a palavra diferente do Aprender:\n%s\nwant:\n%s", emJSON(t, depois), emJSON(t, doAprender))
	}
	e := entradaDe(t, depois, "n8n")
	if e.Prioridade != PrioridadeTermoAprendido || e.Origem != OrigemPainel || e.Dominio != "" || e.Restrito {
		t.Errorf("entrada criada = %+v", e)
	}
	for _, d := range depois.ParaEntradas() {
		if d.CanonicalForm == "n8n" && !d.AliasOnly {
			t.Error("a palavra criada não é AliasOnly")
		}
	}
}

// TestAplicarPares_AsOutrasChecagens: a palavra comum (como defesa, só sobre a
// variante, com grafia verdadeiro) e o "agiria" do Agent, nas linhas que gravam.
func TestAplicarPares_AsOutrasChecagens(t *testing.T) {
	var pedidos []string
	c := ConferenciasDoLote{
		PalavraComum: func(texto string, grafia bool) string {
			pedidos = append(pedidos, texto)
			if !grafia {
				t.Errorf("PalavraComum(%q) sem grafia: a defesa do PC que recebe é a GrafiaArriscada", texto)
			}
			if texto == "de" {
				return "palavra_de_ligacao"
			}
			return ""
		},
		Agiria: func(s Semente, p ParDoPainel) error {
			if p.Variante == "sf" {
				return ErrNaoAgiria
			}
			return nil
		},
	}
	s := pcQueRecebe()

	_, lote := aplicarUmPar(t, s, ParDaNuvem{Variante: "de", Termo: "ECF"}, c)
	if r := lote.Resultados[0]; r.Situacao != SituacaoNaoAplicado || r.Motivo != MotivoPalavraComum || lote.Mudou {
		t.Errorf("palavra comum: %+v, Mudou %v", r, lote.Mudou)
	}

	_, lote = aplicarUmPar(t, s, ParDaNuvem{Variante: "sf", Termo: "ECF"}, c)
	if r := lote.Resultados[0]; r.Situacao != SituacaoNaoAplicado || r.Motivo != MotivoNaoAgiria || lote.Mudou {
		t.Errorf("não agiria: %+v, Mudou %v", r, lote.Mudou)
	}

	// As linhas que não gravam não chegam às checagens.
	pedidos = nil
	aplicarUmPar(t, s, ParDaNuvem{Variante: "de", Termo: "Folha", Dominio: "rh"}, c)
	if len(pedidos) != 0 {
		t.Errorf("a palavra comum foi conferida numa linha que não grava: %v", pedidos)
	}
}

// TestAplicarPares_AgiriaRecebeARestricaoEfetiva: o "agiria" do Agent confere
// cada domínio onde o par vai agir AQUI, e para isso recebe a projeção com a
// adaptação deste PC (o alias_restrito, a restrição do termo daqui).
func TestAplicarPares_AgiriaRecebeARestricaoEfetiva(t *testing.T) {
	var vistos []ParDoPainel
	c := ConferenciasDoLote{Agiria: func(s Semente, p ParDoPainel) error {
		if ondeEsta(s, p.Variante) == "" {
			t.Errorf("Agiria recebeu a semente sem a variante %q", p.Variante)
		}
		vistos = append(vistos, p)
		return nil
	}}
	aplicarUmPar(t, pcQueRecebe(), ParDaNuvem{Variante: "efe", Termo: "ECF", Restrito: true, Dominio: "contabil"}, c)
	want := ParDoPainel{ID: IDDoPar("efe"), Variante: "efe", Termo: "ECF", Restrito: true, Dominio: "contabil", EmAliasRestrito: true}
	if len(vistos) == 0 || vistos[0] != want {
		t.Fatalf("Agiria recebeu %+v, want %+v", vistos, want)
	}
}

// TestAplicarPares_AgiriaComOutroErroInterrompe: um erro do "agiria" que não é
// ErrNaoAgiria (o dicionário não carrega) não é um motivo: o lote para, e nada
// é gravado.
func TestAplicarPares_AgiriaComOutroErroInterrompe(t *testing.T) {
	quebra := errors.New("não carrega")
	_, _, err := pcQueRecebe().AplicarPares([]ParDaNuvem{{Variante: "sf", Termo: "ECF"}},
		ConferenciasDoLote{Agiria: func(Semente, ParDoPainel) error { return quebra }})
	if !errors.Is(err, quebra) {
		t.Fatalf("err = %v, want o erro do agiria", err)
	}
}

// TestAplicarPares_CasosDeBorda são os "Os casos de borda" da ADR-019.
func TestAplicarPares_CasosDeBorda(t *testing.T) {
	base := pcQueRecebe()
	casos := []struct {
		nome     string
		s        Semente
		par      ParDaNuvem
		situacao SituacaoDoPar
		motivo   MotivoDoPar
		mudou    bool
		onde     string // onde a variante do par fica depois
	}{
		// O convergente não grava.
		{"o mesmo par já vale aqui, de origem painel", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "sf", Termo: "ECF", Dominio: "contabil"}, SituacaoConvergente, "", false, "ECF.alias"},
		{"o mesmo par, escrito de outro jeito e com o termo em outra caixa", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "S.F.", Termo: "ecf", Dominio: "contabil"}, SituacaoConvergente, "", false, "ECF.alias"},
		{"convergente com a adaptação: o termo daqui é restrito", comPainel(t, base, "commit", "cmt", false),
			ParDaNuvem{Variante: "cmt", Termo: "commit", Dominio: "dev"}, SituacaoConvergente, "", false, "commit.alias"},
		{"convergente com a adaptação: em alias_restrito", comPainel(t, base, "ECF", "efe", true),
			ParDaNuvem{Variante: "efe", Termo: "ECF", Restrito: true, Dominio: "contabil"}, SituacaoConvergente, "", false, "ECF.alias_restrito"},
		// O convergente vem antes da tabela: o par que este PC projeta igual
		// ao que chega não passa pelo mapa dominios nem pela lista que a
		// tabela escolheria para um par novo (o recomeço na mesma conta).
		{"o mesmo par já vale aqui, num termo cujo domínio não está no mapa", comPainel(t, base, "Folha", "fl", false),
			ParDaNuvem{Variante: "fl", Termo: "Folha", Dominio: "rh"}, SituacaoConvergente, "", false, "Folha.alias"},
		{"o mesmo par já vale aqui, em alias_restrito de um termo restrito", comPainel(t, base, "commit", "cmr", true),
			ParDaNuvem{Variante: "cmr", Termo: "commit", Restrito: true, Dominio: "dev"}, SituacaoConvergente, "", false, "commit.alias_restrito"},

		// O posto à mão: "já vale aqui", sem gravar nem adotar, e o desfazer não o tira.
		{"o mesmo par posto à mão", base,
			ParDaNuvem{Variante: "ECFX", Termo: "ECF"}, SituacaoJaValeAqui, "", false, "ECF.alias"},
		{"o mesmo par posto à mão, e a nuvem o tem restrito", base,
			ParDaNuvem{Variante: "ECFX", Termo: "ECF", Restrito: true, Dominio: "contabil"}, SituacaoJaValeAqui, "", false, "ECF.alias"},
		{"o mesmo par posto à mão, num termo cujo domínio não está no mapa", comAMao(t, base, "Folha", "fl"),
			ParDaNuvem{Variante: "fl", Termo: "Folha", Dominio: "rh"}, SituacaoJaValeAqui, "", false, "Folha.alias"},
		{"o desfazer que chega não tira o posto à mão", base,
			ParDaNuvem{Variante: "ECFX", Termo: "ECF", Desfeito: true}, SituacaoJaValeAqui, "", false, "ECF.alias"},

		// O desfazer.
		{"o par desfeito na nuvem sai daqui", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "sf", Termo: "ECF", Desfeito: true}, SituacaoDesfeito, "", true, ""},
		{"o desfazer é pela variante, qualquer que seja o termo", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "S F", Termo: "SPED", Desfeito: true}, SituacaoDesfeito, "", true, ""},
		{"o desfeito que não está aqui", base,
			ParDaNuvem{Variante: "sf", Termo: "ECF", Desfeito: true}, SituacaoAusente, "", false, ""},
		{"o desfeito que é de outro termo, à mão", base,
			ParDaNuvem{Variante: "DAAS", Termo: "ECF", Desfeito: true}, SituacaoAusente, "", false, "ECD.alias"},

		// A troca: o de antes sai, o novo entra pelas regras.
		{"a nuvem trocou o termo", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "sf", Termo: "SPED"}, SituacaoAplicado, "", true, "SPED.alias"},
		{"a nuvem tornou o par restrito, e o PC consegue honrar", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "sf", Termo: "ECF", Restrito: true, Dominio: "contabil"}, SituacaoAplicado, "", true, "ECF.alias_restrito"},
		{"a nuvem tirou a restrição", comPainel(t, base, "ECF", "efe", true),
			ParDaNuvem{Variante: "efe", Termo: "ECF", Dominio: "contabil"}, SituacaoAplicado, "", true, "ECF.alias"},
		{"a nuvem tornou o par mais restrito, e o PC não consegue honrar: o de antes sai", comPainel(t, base, "SPED", "sf", false),
			ParDaNuvem{Variante: "sf", Termo: "SPED", Restrito: true, Dominio: "contabil"}, SituacaoNaoAplicado, MotivoRestricaoNaoCabe, true, ""},
		{"a nuvem trocou o domínio, com o mesmo termo e a mesma restrição: o de antes sai", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "sf", Termo: "ECF", Dominio: "dev"}, SituacaoNaoAplicado, MotivoRestricaoNaoCabe, true, ""},
		{"a nuvem trocou para um termo que não vale aqui: o de antes sai", comPainel(t, base, "ECF", "sf", false),
			ParDaNuvem{Variante: "sf", Termo: "Folha", Dominio: "rh"}, SituacaoNaoAplicado, MotivoDominioAusente, true, ""},
		{"a nuvem trocou para um termo de que a variante já é, à mão", comPainel(t, base, "SPED", "ECFX ", false),
			ParDaNuvem{Variante: "ECFX", Termo: "ECF"}, SituacaoJaValeAqui, "", true, "ECF.alias"},

		// A variante com a origem em dois lugares: o PC não sabe qual é o par.
		{"a variante com a origem em dois lugares", comPainel(t, comPainel(t, base, "ECF", "sf", false), "SPED", "S F", false),
			ParDaNuvem{Variante: "sf", Termo: "ECF"}, SituacaoNaoAplicado, MotivoVarianteDeOutroTermo, false, "ECF.alias,SPED.alias"},
		{"a variante com a origem em dois lugares, desfeita", comPainel(t, comPainel(t, base, "ECF", "sf", false), "SPED", "S F", false),
			ParDaNuvem{Variante: "sf", Termo: "ECF", Desfeito: true}, SituacaoNaoAplicado, MotivoVarianteDeOutroTermo, false, "ECF.alias,SPED.alias"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			depois, lote := aplicarUmPar(t, c.s, c.par, ConferenciasDoLote{})
			r := lote.Resultados[0]
			if r.Situacao != c.situacao || r.Motivo != c.motivo {
				t.Fatalf("resultado = %+v, want %s %s", r, c.situacao, c.motivo)
			}
			if lote.Mudou != c.mudou {
				t.Errorf("Mudou = %v, want %v", lote.Mudou, c.mudou)
			}
			if !c.mudou && emJSON(t, depois) != emJSON(t, c.s) {
				t.Errorf("sem Mudou, mas a semente mudou:\n%s", emJSON(t, depois))
			}
			if got := ondeEstaAChave(depois, ChaveDoPar(c.par.Variante)); got != c.onde {
				t.Errorf("a variante está em %q, want %q", got, c.onde)
			}
			if c.onde == "" {
				if p := depois.projecaoDe(IDDoPar(c.par.Variante)); p != nil {
					t.Errorf("o par continua na projeção: %+v", p)
				}
			}
			if c.situacao == SituacaoJaValeAqui && depois.AliasOrigem[c.par.Variante] == OrigemPainel {
				t.Error("o posto à mão foi adotado (ganhou a origem painel)")
			}
		})
	}
}

// --- O lote ---

// agiriaComVeto imita o veto do Smart ("pedro das", com o termo DAS): a
// variante não age quando uma palavra dela é a forma canônica de outro termo
// ativo do dicionário.
func agiriaComVeto(s Semente, p ParDoPainel) error {
	for _, w := range strings.Fields(Normalize(p.Variante)) {
		for _, e := range s.Entradas {
			if !e.Inativa && !mesmaCoisa(e.Canonica, p.Termo) && ChaveDoPar(e.Canonica) == w {
				return ErrNaoAgiria
			}
		}
	}
	return nil
}

// TestAplicarPares_UmParQueVetaOutroEhPego, nas duas ordens: "pedro das" deixa
// de agir quando o lote cria o termo DAS. Na ordem do lote, a conferência do
// segundo par sobre a semente acumulada o pega; na contrária, a conferência
// final. O fim é o mesmo.
func TestAplicarPares_UmParQueVetaOutroEhPego(t *testing.T) {
	pedro := ParDaNuvem{Variante: "pedro das", Termo: "Pedro Dias"}
	das := ParDaNuvem{Variante: "dass", Termo: "DAS"}
	c := ConferenciasDoLote{Agiria: agiriaComVeto}
	var finais []string
	for _, ordem := range [][]ParDaNuvem{{das, pedro}, {pedro, das}} {
		depois, lote, err := pcQueRecebe().AplicarPares(ordem, c)
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range ordem {
			r := lote.Resultados[i]
			want := SituacaoAplicado
			if p == pedro {
				want = SituacaoNaoAplicado
			}
			if r.Situacao != want || (want == SituacaoNaoAplicado && r.Motivo != MotivoNaoAgiria) {
				t.Errorf("ordem %v: %q = %+v, want %s", ordem, p.Variante, r, want)
			}
		}
		if ondeEsta(depois, "pedro das") != "" || depois.indiceDoTermo("Pedro Dias") >= 0 {
			t.Errorf("o par vetado ficou no dicionário:\n%s", emJSON(t, depois))
		}
		for _, p := range depois.ParesDoPainel() {
			if agiriaComVeto(depois, p) != nil {
				t.Errorf("ficou um par que não age: %+v", p)
			}
		}
		finais = append(finais, emJSON(t, depois))
	}
	if finais[0] != finais[1] {
		t.Errorf("as duas ordens terminam diferentes:\n%s\n%s", finais[0], finais[1])
	}
}

// TestAplicarPares_OParQueNaoAgeNaoSeguraOsOutros: o par que não age sai na
// hora (a conferência par a par), e não fica no meio do lote barrando o
// seguinte: aqui, o termo novo "xyz" seria variante do par que não age.
func TestAplicarPares_OParQueNaoAgeNaoSeguraOsOutros(t *testing.T) {
	c := ConferenciasDoLote{Agiria: func(s Semente, p ParDoPainel) error {
		if p.Variante == "xyz" {
			return ErrNaoAgiria
		}
		return nil
	}}
	depois, lote, err := pcQueRecebe().AplicarPares([]ParDaNuvem{
		{Variante: "xyz", Termo: "ECF"},
		{Variante: "abc", Termo: "xyz"},
	}, c)
	if err != nil {
		t.Fatal(err)
	}
	if r := lote.Resultados[0]; r.Situacao != SituacaoNaoAplicado || r.Motivo != MotivoNaoAgiria {
		t.Errorf("o par que não age: %+v", r)
	}
	if r := lote.Resultados[1]; r.Situacao != SituacaoAplicado {
		t.Errorf("o par seguinte foi barrado pelo que não age: %+v", r)
	}
	if ondeEsta(depois, "xyz") != "" || ondeEsta(depois, "abc") != "xyz.alias" {
		t.Errorf("dicionário:\n%s", emJSON(t, depois))
	}
}

// TestAplicarPares_ForaDaProjecaoNaoAplica: o par que ficaria no arquivo fora
// da projeção (aqui, o termo daqui escrito com mais de 80 runas) nunca subiria,
// e a varredura o leria como desfeito: não é gravado.
func TestAplicarPares_ForaDaProjecaoNaoAplica(t *testing.T) {
	longo := "A" + strings.Repeat("-", 80) + "B" // o mesmo termo que "A-B" para o Search
	s := semente(nil, EntradaSemente{Canonica: longo, Prioridade: 70})
	chamado := false
	depois, lote := aplicarUmPar(t, s, ParDaNuvem{Variante: "sf", Termo: "A-B"},
		ConferenciasDoLote{Agiria: func(Semente, ParDoPainel) error { chamado = true; return nil }})
	if r := lote.Resultados[0]; r.Situacao != SituacaoNaoAplicado || r.Motivo != MotivoNaoAgiria || lote.Mudou {
		t.Fatalf("resultado = %+v, Mudou %v", r, lote.Mudou)
	}
	if ondeEsta(depois, "sf") != "" || chamado {
		t.Errorf("gravou fora da projeção (agiria chamado: %v)", chamado)
	}
}

// TestAplicarPares_OVetoPegaOParRepetido: o par aplicado e repetido no lote
// (o segundo é convergente) também é pego pela conferência final, e os dois
// resultados dizem o mesmo.
func TestAplicarPares_OVetoPegaOParRepetido(t *testing.T) {
	pedro := ParDaNuvem{Variante: "pedro das", Termo: "Pedro Dias"}
	das := ParDaNuvem{Variante: "dass", Termo: "DAS"}
	depois, lote, err := pcQueRecebe().AplicarPares([]ParDaNuvem{pedro, pedro, das}, ConferenciasDoLote{Agiria: agiriaComVeto})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []SituacaoDoPar{SituacaoNaoAplicado, SituacaoNaoAplicado, SituacaoAplicado} {
		if r := lote.Resultados[i]; r.Situacao != want {
			t.Errorf("par %d: %+v, want %s", i, r, want)
		}
	}
	if ondeEsta(depois, "pedro das") != "" {
		t.Errorf("o par vetado ficou:\n%s", emJSON(t, depois))
	}
}

// TestAplicarPares_ParesDoMesmoIdNoLote: o segundo par do mesmo id decide sobre
// o que o primeiro deixou (aplica e depois desfaz: nada fica).
func TestAplicarPares_ParesDoMesmoIdNoLote(t *testing.T) {
	s := pcQueRecebe()
	depois, lote, err := s.AplicarPares([]ParDaNuvem{
		{Variante: "sf", Termo: "ECF"},
		{Variante: "sf", Termo: "ECF", Desfeito: true},
	}, ConferenciasDoLote{Agiria: agiriaComVeto})
	if err != nil {
		t.Fatal(err)
	}
	if lote.Resultados[0].Situacao != SituacaoAplicado || lote.Resultados[1].Situacao != SituacaoDesfeito {
		t.Errorf("resultados = %+v", lote.Resultados)
	}
	if lote.Mudou || emJSON(t, depois) != emJSON(t, s) {
		t.Errorf("aplicar e desfazer no mesmo lote mudou o dicionário (Mudou %v)", lote.Mudou)
	}
	if lote.Resultados[0].Projecao != nil {
		t.Error("o par desfeito no fim do lote tem projeção")
	}
}

// TestAplicarPares_NuncaAlarga confere, para toda combinação do par (sem
// domínio, X sem restrição, X restrito; X no mapa ou não) com o termo daqui
// (ausente, sem domínio, restrito sem domínio, em X, em outro domínio, fora do
// mapa, desligado), que o par aplicado nunca age em mais lugares do que na
// origem.
func TestAplicarPares_NuncaAlarga(t *testing.T) {
	type escopo struct {
		restrito bool
		dominio  string
	}
	// cabe diz se onde o par age aqui está contido em onde ele age na origem.
	cabe := func(aqui, origem escopo) bool {
		switch {
		case origem.dominio == "": // em todo lugar
			return true
		case !origem.restrito: // no desconhecido e em X
			return aqui.dominio == origem.dominio
		default: // só em X
			return aqui.dominio == origem.dominio && aqui.restrito
		}
	}
	pares := []escopo{{false, ""}, {false, "contabil"}, {true, "contabil"}, {false, "rh"}, {true, "rh"}}
	termos := map[string]*EntradaSemente{
		"ausente":                 nil,
		"sem domínio":             {Canonica: "Alvo", Prioridade: 70},
		"restrito sem domínio":    {Canonica: "Alvo", Prioridade: 70, Restrito: true},
		"em X":                    {Canonica: "Alvo", Prioridade: 70, Dominio: "contabil"},
		"restrito a X":            {Canonica: "Alvo", Prioridade: 70, Dominio: "contabil", Restrito: true},
		"em outro":                {Canonica: "Alvo", Prioridade: 70, Dominio: "dev"},
		"restrito a outro":        {Canonica: "Alvo", Prioridade: 70, Dominio: "dev", Restrito: true},
		"fora do mapa":            {Canonica: "Alvo", Prioridade: 70, Dominio: "rh"},
		"restrito, fora do mapa":  {Canonica: "Alvo", Prioridade: 70, Dominio: "rh", Restrito: true},
		"desligado, sem domínio":  {Canonica: "Alvo", Prioridade: 70, Inativa: true},
		"desligado, restrito a X": {Canonica: "Alvo", Prioridade: 70, Dominio: "contabil", Restrito: true, Inativa: true},
	}
	aplicados := 0
	for nome, e := range termos {
		for _, origem := range pares {
			s := semente(nil)
			if e != nil {
				s.Entradas = append(s.Entradas, *e)
			}
			p := ParDaNuvem{Variante: "vv", Termo: "Alvo", Restrito: origem.restrito, Dominio: origem.dominio}
			depois, lote, err := s.AplicarPares([]ParDaNuvem{p}, ConferenciasDoLote{})
			if err != nil {
				t.Fatal(err)
			}
			r := lote.Resultados[0]
			if r.Situacao != SituacaoAplicado {
				if lote.Mudou {
					t.Errorf("%s, par %+v: não aplicado, mas mudou o dicionário", nome, origem)
				}
				continue
			}
			aplicados++
			proj := depois.projecaoDe(IDDoPar("vv"))
			if proj == nil {
				t.Fatalf("%s, par %+v: aplicado fora da projeção", nome, origem)
			}
			if aqui := (escopo{proj.Restrito, proj.Dominio}); !cabe(aqui, origem) {
				t.Errorf("%s: o par %+v age aqui em %+v: alargou", nome, origem, aqui)
			}
		}
	}
	if aplicados < 8 {
		t.Errorf("só %d combinações aplicadas: o teste não prova muita coisa", aplicados)
	}
}

// TestAplicarPares_IdaEVolta: o que um PC projeta chega a outro igual (o mesmo
// id, a mesma restrição e o mesmo domínio), e um segundo lote igual só
// converge.
func TestAplicarPares_IdaEVolta(t *testing.T) {
	pc1 := pcQueRecebe()
	var err error
	for _, a := range [][2]string{{"ECF", "sf"}, {"n8n", "n oito n"}, {"SPED", "espede"}} {
		if pc1, _, err = pc1.AprenderVariante(a[0], a[1]); err != nil {
			t.Fatal(err)
		}
	}
	pc1 = comPainel(t, pc1, "ECF", "efe", true)
	pc1 = comPainel(t, pc1, "commit", "cmt", false)

	var pull []ParDaNuvem
	for _, p := range pc1.ParesDoPainel() {
		pull = append(pull, p.ParaNuvem())
	}
	pc2, lote, err := pcQueRecebe().AplicarPares(pull, ConferenciasDoLote{Agiria: agiriaComVeto})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range lote.Resultados {
		if r.Situacao != SituacaoAplicado {
			t.Errorf("%+v", r)
		}
	}
	um, dois := pc1.ParesDoPainel(), pc2.ParesDoPainel()
	if !reflect.DeepEqual(um, dois) {
		t.Errorf("a projeção do PC 2 não é a do PC 1:\n%+v\n%+v", um, dois)
	}

	_, lote, err = pc2.AplicarPares(pull, ConferenciasDoLote{Agiria: agiriaComVeto})
	if err != nil {
		t.Fatal(err)
	}
	if lote.Mudou {
		t.Error("o mesmo lote de novo mudou o dicionário")
	}
	for _, r := range lote.Resultados {
		if r.Situacao != SituacaoConvergente || r.Projecao == nil {
			t.Errorf("de novo: %+v", r)
		}
	}

	// O PC 1 de volta à mesma conta (o pull do cursor 0 traz o que ele
	// subiu), com uma variante num termo cujo domínio não está no mapa
	// (Folha, "rh") e uma em alias_restrito de um termo restrito: só
	// converge, e nada sai do arquivo.
	pc1 = comPainel(t, pc1, "Folha", "fl", false)
	pc1 = comPainel(t, pc1, "commit", "cmr", true)
	var proprio []ParDaNuvem
	for _, p := range pc1.ParesDoPainel() {
		proprio = append(proprio, p.ParaNuvem())
	}
	volta, lote, err := pc1.AplicarPares(proprio, ConferenciasDoLote{Agiria: agiriaComVeto})
	if err != nil {
		t.Fatal(err)
	}
	if lote.Mudou || emJSON(t, volta) != emJSON(t, pc1) {
		t.Errorf("a própria projeção mudou o dicionário:\n%s", emJSON(t, volta))
	}
	for _, r := range lote.Resultados {
		if r.Situacao != SituacaoConvergente || r.Projecao == nil {
			t.Errorf("a própria projeção: %+v", r)
		}
	}
}

// TestAplicarPares_APropriaProjecaoSoConverge: o PC que recebe de volta o que
// ele mesmo projetou (o "Esquecer esta nuvem" e a volta à mesma conta, que
// traz tudo do cursor 0) só converge, em toda combinação do termo daqui (sem
// domínio, com o domínio no mapa, fora dele, ou no mapa em outra caixa;
// restrito ou não; desligado ou não) e da lista da variante (alias ou
// alias_restrito). Nada é gravado, e nenhum par sai do arquivo.
func TestAplicarPares_APropriaProjecaoSoConverge(t *testing.T) {
	conferidos := 0
	for _, dominio := range []string{"", "contabil", "rh", "Dev"} {
		for _, restrito := range []bool{false, true} {
			for _, inativa := range []bool{false, true} {
				for _, emRestrito := range []bool{false, true} {
					e := EntradaSemente{Canonica: "Alvo", Prioridade: 70, Dominio: dominio, Restrito: restrito, Inativa: inativa}
					if emRestrito {
						e.AliasRestrito = []string{"vv"}
					} else {
						e.Alias = []string{"vv"}
					}
					s := semente(painel("vv"), e)
					proj := s.ParesDoPainel()
					if len(proj) == 0 {
						// Não sobe (nunca age aqui). O mesmo texto vindo de
						// outro PC, sem domínio, não é convergente: o
						// convergente sempre tem a projeção (a base do
						// espelho precisa dela).
						_, lote, err := s.AplicarPares([]ParDaNuvem{{Variante: "vv", Termo: "Alvo"}}, ConferenciasDoLote{})
						if err != nil {
							t.Fatal(err)
						}
						if r := lote.Resultados[0]; r.Situacao == SituacaoConvergente && r.Projecao == nil {
							t.Errorf("convergente sem a projeção em\n%s", emJSON(t, s))
						}
						continue
					}
					conferidos++
					nome := emJSON(t, s)
					depois, lote, err := s.AplicarPares([]ParDaNuvem{proj[0].ParaNuvem()}, ConferenciasDoLote{})
					if err != nil {
						t.Fatalf("%s: %v", nome, err)
					}
					if r := lote.Resultados[0]; r.Situacao != SituacaoConvergente || r.Projecao == nil {
						t.Errorf("a própria projeção %+v deu %+v em\n%s", proj[0], r, nome)
					}
					if lote.Mudou || emJSON(t, depois) != nome {
						t.Errorf("a própria projeção %+v mudou o arquivo:\n%s", proj[0], emJSON(t, depois))
					}
				}
			}
		}
	}
	if conferidos < 16 {
		t.Errorf("só %d combinações sobem: o teste não prova muita coisa", conferidos)
	}
}

func TestMotivosDoPar_ListaFechada(t *testing.T) {
	ms := MotivosDoPar()
	if len(ms) != 8 {
		t.Fatalf("%d motivos, a ADR lista 8", len(ms))
	}
	vistos := map[MotivoDoPar]bool{}
	for _, m := range ms {
		if vistos[m] || m == "" || strings.ToLower(string(m)) != string(m) || strings.ContainsAny(string(m), " -") {
			t.Errorf("motivo %q repetido, vazio ou fora do formato", m)
		}
		vistos[m] = true
	}
}

// --- Sobre o arquivo ---

// contarGravacoes troca o renomear de Gravar por um que conta, e o devolve.
func contarGravacoes(t *testing.T) *int {
	t.Helper()
	antes := renomear
	t.Cleanup(func() { renomear = antes })
	n := new(int)
	renomear = func(de, para string) error {
		*n++
		return antes(de, para)
	}
	return n
}

func arquivoQueRecebe(t *testing.T, s Semente) (*Arquivo, string) {
	t.Helper()
	return arquivoDeTeste(t, emJSON(t, s))
}

func todos(pares ...ParDaNuvem) func(Semente) []ParDaNuvem {
	return func(Semente) []ParDaNuvem { return pares }
}

// loteMisto aplica cada linha que grava: cria (D2), acrescenta, adapta em
// alias_restrito, e um que não vale aqui.
var loteMisto = []ParDaNuvem{
	{Variante: "n oito n", Termo: "n8n"},
	{Variante: "sf", Termo: "ECF", Dominio: "contabil"},
	{Variante: "efe", Termo: "ECF", Restrito: true, Dominio: "contabil"},
	{Variante: "cmt", Termo: "commit", Dominio: "dev"},
	{Variante: "fl", Termo: "Folha", Dominio: "rh"},
}

// TestArquivo_AplicarPares_UmaGravacaoPorLote: uma leitura, a conferência par a
// par e UMA gravação, com o lote inteiro.
func TestArquivo_AplicarPares_UmaGravacaoPorLote(t *testing.T) {
	a, caminho := arquivoQueRecebe(t, pcQueRecebe())
	gravacoes := contarGravacoes(t)
	var conferidos int
	a.ValidarPar = func(Semente, ParDoPainel) error { conferidos++; return nil }
	var validados int
	a.Validar = func(Semente) error { validados++; return nil }

	gravada, lote, err := a.AplicarPares(todos(loteMisto...), nil)
	if err != nil {
		t.Fatal(err)
	}
	if *gravacoes != 1 || validados != 1 {
		t.Errorf("%d gravações e %d Validar para um lote, want 1 e 1", *gravacoes, validados)
	}
	if conferidos < 4 {
		t.Errorf("ValidarPar chamado %d vezes para 4 pares aplicados", conferidos)
	}
	if !lote.Mudou {
		t.Error("lote aplicado sem Mudou")
	}
	noDisco, err := a.Atual()
	if err != nil {
		t.Fatal(err)
	}
	if emJSON(t, noDisco) != emJSON(t, gravada) {
		t.Error("a semente devolvida não é a gravada")
	}
	if got := len(noDisco.ParesDoPainel()); got != 4 {
		t.Errorf("%d pares no disco, want 4", got)
	}
	exigirSoOArquivo(t, caminho)
}

// TestArquivo_AplicarPares_ConvergenteNaoGrava: o convergente e o "já vale
// aqui" não gravam (nem chamam o antesDeGravar: não há intenção a guardar).
func TestArquivo_AplicarPares_ConvergenteNaoGrava(t *testing.T) {
	s := comPainel(t, pcQueRecebe(), "ECF", "sf", false)
	a, caminho := arquivoQueRecebe(t, s)
	antes := lerBytes(t, caminho)
	gravacoes := contarGravacoes(t)

	_, lote, err := a.AplicarPares(todos(
		ParDaNuvem{Variante: "sf", Termo: "ECF", Dominio: "contabil"}, // convergente
		ParDaNuvem{Variante: "ECFX", Termo: "ECF"},                    // já vale aqui
		ParDaNuvem{Variante: "ECFX", Termo: "ECF", Desfeito: true},    // o desfazer não tira o posto à mão
		ParDaNuvem{Variante: "zz", Termo: "ECF", Desfeito: true},      // ausente
		ParDaNuvem{Variante: "fl", Termo: "Folha", Dominio: "rh"},     // não aplicado
	), func(Lote) error {
		t.Error("antesDeGravar chamado sem nada a gravar")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if lote.Mudou || *gravacoes != 0 {
		t.Errorf("Mudou %v, %d gravações, want nenhuma", lote.Mudou, *gravacoes)
	}
	if !bytes.Equal(lerBytes(t, caminho), antes) {
		t.Error("o arquivo mudou")
	}
	want := []SituacaoDoPar{SituacaoConvergente, SituacaoJaValeAqui, SituacaoJaValeAqui, SituacaoAusente, SituacaoNaoAplicado}
	for i, r := range lote.Resultados {
		if r.Situacao != want[i] {
			t.Errorf("par %d: %s, want %s", i, r.Situacao, want[i])
		}
	}
	if lote.Resultados[0].Projecao == nil {
		t.Error("o convergente sem a projeção (a base do espelho precisa dela)")
	}
}

// TestArquivo_AplicarPares_EscolherEAntesDeGravar: escolher vê o arquivo
// relido; antesDeGravar vê o lote antes do arquivo mudar (a intenção do
// espelho, write-ahead), e o erro dele cancela a gravação.
func TestArquivo_AplicarPares_EscolherEAntesDeGravar(t *testing.T) {
	s := pcQueRecebe()
	a, caminho := arquivoQueRecebe(t, s)

	// Alguém (o dictlearn, a mão) muda o arquivo antes do lote: escolher o vê.
	mudado := comPainel(t, s, "SPED", "espede", false)
	if err := mudado.Gravar(caminho); err != nil {
		t.Fatal(err)
	}
	antes := lerBytes(t, caminho)

	recusa := errors.New("o espelho não gravou")
	var visto Lote
	_, _, err := a.AplicarPares(func(atual Semente) []ParDaNuvem {
		if ondeEsta(atual, "espede") == "" {
			t.Error("escolher não recebeu o arquivo relido")
		}
		return []ParDaNuvem{{Variante: "sf", Termo: "ECF"}}
	}, func(l Lote) error {
		visto = l
		if !bytes.Equal(lerBytes(t, caminho), antes) {
			t.Error("antesDeGravar chamado depois de o arquivo mudar")
		}
		return recusa
	})
	if !errors.Is(err, recusa) {
		t.Fatalf("err = %v, want o do antesDeGravar", err)
	}
	if len(visto.Resultados) != 1 || visto.Resultados[0].Situacao != SituacaoAplicado || visto.Resultados[0].Projecao == nil {
		t.Errorf("antesDeGravar recebeu %+v", visto)
	}
	if !bytes.Equal(lerBytes(t, caminho), antes) {
		t.Error("o erro do antesDeGravar não cancelou a gravação")
	}
	exigirSoOArquivo(t, caminho)
}

// TestArquivo_AplicarPares_RecusasNaoTocam: o Validar que recusa, e o
// "agiria" que falha por outro motivo, não gravam nada, e o erro não traz o
// texto.
func TestArquivo_AplicarPares_RecusasNaoTocam(t *testing.T) {
	for nome, prepara := range map[string]func(*Arquivo){
		"Validar recusa":        func(a *Arquivo) { a.Validar = func(Semente) error { return errors.New("ECF sf") } },
		"agiria com outro erro": func(a *Arquivo) { a.ValidarPar = func(Semente, ParDoPainel) error { return errors.New("ECF sf") } },
	} {
		t.Run(nome, func(t *testing.T) {
			a, caminho := arquivoQueRecebe(t, pcQueRecebe())
			antes := lerBytes(t, caminho)
			prepara(a)
			_, _, err := a.AplicarPares(todos(ParDaNuvem{Variante: "sf", Termo: "ECF"}), nil)
			if !errors.Is(err, ErrNaoCarrega) {
				t.Fatalf("err = %v, want ErrNaoCarrega", err)
			}
			if strings.Contains(err.Error(), "sf") || strings.Contains(err.Error(), "ECF") {
				t.Errorf("o erro cita o dicionário: %q", err)
			}
			if !bytes.Equal(lerBytes(t, caminho), antes) {
				t.Error("o arquivo mudou")
			}
		})
	}
}

// TestArquivo_AplicarPares_ArquivoIndisponivel: o arquivo ausente, inválido,
// de outro usuário ou preso na troca de nome nunca é tocado, e o erro é o do
// motivo arquivo_indisponivel.
func TestArquivo_AplicarPares_ArquivoIndisponivel(t *testing.T) {
	t.Run("ausente", func(t *testing.T) {
		a := &Arquivo{Caminho: filepath.Join(t.TempDir(), "dicionario.json")}
		_, _, err := a.AplicarPares(todos(ParDaNuvem{Variante: "sf", Termo: "ECF"}), nil)
		if !errors.Is(err, ErrSemArquivo) || !ArquivoIndisponivel(err) {
			t.Fatalf("err = %v", err)
		}
		if _, err := os.Stat(a.Caminho); !errors.Is(err, fs.ErrNotExist) {
			t.Error("o lote criou o arquivo")
		}
	})
	for nome, conteudo := range map[string]string{
		"inválido":      "{\"usuario\": \"mauro\", \"entradas\": [",
		"campo novo":    "{\"usuario\": \"mauro\", \"entradas\": [], \"novo\": 1}",
		"de outro dono": emJSON(t, Semente{Usuario: "outra", Entradas: []EntradaSemente{}}),
	} {
		t.Run(nome, func(t *testing.T) {
			a, caminho := arquivoDeTeste(t, conteudo)
			a.Usuario = "mauro"
			_, _, err := a.AplicarPares(todos(ParDaNuvem{Variante: "sf", Termo: "ECF"}), nil)
			if !ArquivoIndisponivel(err) {
				t.Fatalf("err = %v, want arquivo indisponível", err)
			}
			if got := string(lerBytes(t, caminho)); got != conteudo {
				t.Error("o arquivo foi tocado")
			}
		})
	}
	t.Run("preso na troca de nome", func(t *testing.T) {
		a, caminho := arquivoQueRecebe(t, pcQueRecebe())
		antes := lerBytes(t, caminho)
		antesFn, antesPausas := renomear, pausasEntreRenames
		t.Cleanup(func() { renomear, pausasEntreRenames = antesFn, antesPausas })
		pausasEntreRenames = nil
		renomear = func(de, para string) error {
			return &os.LinkError{Op: "rename", Old: de, New: para, Err: fs.ErrPermission}
		}
		_, _, err := a.AplicarPares(todos(ParDaNuvem{Variante: "sf", Termo: "ECF"}), nil)
		if !ArquivoIndisponivel(err) {
			t.Fatalf("err = %v, want arquivo indisponível", err)
		}
		if !bytes.Equal(lerBytes(t, caminho), antes) {
			t.Error("o arquivo mudou")
		}
		exigirSoOArquivo(t, caminho)
	})
	if ArquivoIndisponivel(nil) || ArquivoIndisponivel(errors.New("outro")) || ArquivoIndisponivel(ErrNaoCarrega) {
		t.Error("ArquivoIndisponivel diz sim para o que não é do arquivo")
	}
}

// TestArquivo_AplicarPares_DesfazerOAplicadoVoltaByteAByte: o lote que desfaz
// o que outro lote aplicou (criado pela D2, acrescentado, em alias_restrito)
// deixa o arquivo idêntico, byte a byte, ao de antes.
func TestArquivo_AplicarPares_DesfazerOAplicadoVoltaByteAByte(t *testing.T) {
	conteudos := map[string]string{
		"o PC que recebe":     emJSON(t, pcQueRecebe()),
		"o do painel":         sementeDoPainelJSON,
		"sem alias_origem":    "{\n  \"usuario\": \"mauro\",\n  \"dominios\": {\n    \"contabil\": [\n      \"SistemaContabil.exe\"\n    ]\n  },\n  \"entradas\": [\n    {\n      \"canonica\": \"ECF\",\n      \"prioridade\": 90,\n      \"dominio\": \"contabil\"\n    }\n  ]\n}\n",
		"sem entrada nenhuma": "{\n  \"usuario\": \"mauro\",\n  \"dominios\": {\n    \"contabil\": []\n  },\n  \"entradas\": []\n}\n",
	}
	if versionado, err := os.ReadFile(filepath.Join("..", "..", "testdata", "asr-dataset", "dicionario.json")); err == nil {
		conteudos["o dicionário versionado"] = string(versionado)
	}
	pares := []ParDaNuvem{
		{Variante: "n oito n", Termo: "n8n"},
		{Variante: "sf", Termo: "ECF"},
		{Variante: "efe", Termo: "ECF", Restrito: true, Dominio: "contabil"},
	}
	for nome, conteudo := range conteudos {
		t.Run(nome, func(t *testing.T) {
			a, caminho := arquivoDeTeste(t, conteudo)
			antes := lerBytes(t, caminho)
			s, err := a.Atual()
			if err != nil {
				t.Fatal(err)
			}
			if emJSON(t, s) != conteudo {
				t.Skip("o arquivo não está no formato de Gravar")
			}
			_, lote, err := a.AplicarPares(todos(pares...), nil)
			if err != nil {
				t.Fatal(err)
			}
			aplicados := 0
			var desfazer []ParDaNuvem
			for i, r := range lote.Resultados {
				if r.Situacao == SituacaoAplicado {
					aplicados++
					p := pares[i]
					p.Desfeito = true
					desfazer = append(desfazer, p)
				}
			}
			if aplicados < 2 {
				t.Fatalf("só %d pares aplicados: %+v", aplicados, lote.Resultados)
			}
			if bytes.Equal(lerBytes(t, caminho), antes) {
				t.Fatal("o lote não mudou o arquivo")
			}
			_, lote, err = a.AplicarPares(todos(desfazer...), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range lote.Resultados {
				if r.Situacao != SituacaoDesfeito {
					t.Errorf("desfazer: %+v", r)
				}
			}
			if got := lerBytes(t, caminho); !bytes.Equal(got, antes) {
				t.Errorf("desfazer o que foi aplicado não voltou o arquivo.\nantes:\n%s\ndepois:\n%s", antes, got)
			}
			exigirSoOArquivo(t, caminho)
		})
	}
}

// --- O formato do dicionario.json não muda ---

// camposDoArquivo são os campos do dicionario.json, congelados. Um Agent
// antigo recusa campo novo (CarregarSemente, DisallowUnknownFields): a F8 não
// acrescenta nenhum (ADR-019, "Nada novo entra no dicionario.json").
var camposDoArquivo = map[string][]string{
	"semente": {"usuario", "dominios", "alias_origem", "entradas"},
	"entrada": {"canonica", "prioridade", "dominio", "idioma", "alias", "alias_restrito", "restrito", "inativa", "origem"},
}

func nomesJSON(t *testing.T, tipo reflect.Type) []string {
	t.Helper()
	var out []string
	for i := 0; i < tipo.NumField(); i++ {
		nome, _, _ := strings.Cut(tipo.Field(i).Tag.Get("json"), ",")
		out = append(out, nome)
	}
	return out
}

// TestGravar_NaoEscreveCampoNovo: os campos das structs são os congelados, e o
// arquivo que o lote grava só tem eles.
func TestGravar_NaoEscreveCampoNovo(t *testing.T) {
	if got, want := nomesJSON(t, reflect.TypeOf(Semente{})), camposDoArquivo["semente"]; !reflect.DeepEqual(got, want) {
		t.Errorf("os campos de Semente mudaram: %v, want %v", got, want)
	}
	if got, want := nomesJSON(t, reflect.TypeOf(EntradaSemente{})), camposDoArquivo["entrada"]; !reflect.DeepEqual(got, want) {
		t.Errorf("os campos de EntradaSemente mudaram: %v, want %v", got, want)
	}

	a, caminho := arquivoQueRecebe(t, comPainel(t, pcQueRecebe(), "SPED", "spd", false))
	if _, _, err := a.AplicarPares(todos(append(append([]ParDaNuvem{}, loteMisto...),
		ParDaNuvem{Variante: "spd", Termo: "ECF"})...), nil); err != nil {
		t.Fatal(err)
	}
	var arquivo struct {
		Raiz     map[string]json.RawMessage
		Entradas []map[string]json.RawMessage `json:"entradas"`
	}
	b := lerBytes(t, caminho)
	if err := json.Unmarshal(b, &arquivo.Raiz); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &arquivo); err != nil {
		t.Fatal(err)
	}
	permitidos := func(lista []string) map[string]bool {
		m := map[string]bool{}
		for _, c := range lista {
			m[c] = true
		}
		return m
	}
	var fora []string
	for c := range arquivo.Raiz {
		if !permitidos(camposDoArquivo["semente"])[c] {
			fora = append(fora, c)
		}
	}
	for _, e := range arquivo.Entradas {
		for c := range e {
			if !permitidos(camposDoArquivo["entrada"])[c] {
				fora = append(fora, c)
			}
		}
	}
	sort.Strings(fora)
	if len(fora) > 0 {
		t.Errorf("o arquivo gravado tem campos novos: %v", fora)
	}
	if _, err := CarregarSemente(bytes.NewReader(b)); err != nil {
		t.Errorf("o arquivo gravado não carrega com DisallowUnknownFields: %v", err)
	}
}
