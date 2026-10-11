package dictionary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// O "Adicionar termo" (ADR-020), no lado puro e sobre o arquivo. Tudo em
// memória ou em t.TempDir(): nenhum teste toca o bin\dicionario.json.

// sementeDeTermos tem um termo de antes no topo do prompt (DCTFWeb, 100), dois
// abaixo, um termo que o painel criou e um desligado. Está no formato que
// Gravar escreve.
const sementeDeTermos = `{
  "usuario": "mauro",
  "alias_origem": {
    "DAAS": "cont-04",
    "ista": "painel"
  },
  "entradas": [
    {
      "canonica": "DCTFWeb",
      "prioridade": 100
    },
    {
      "canonica": "eSocial",
      "prioridade": 95
    },
    {
      "canonica": "DAS",
      "prioridade": 90,
      "alias": [
        "DAAS"
      ]
    },
    {
      "canonica": "esta",
      "prioridade": 50,
      "alias": [
        "ista"
      ],
      "origem": "painel"
    },
    {
      "canonica": "Desligado",
      "prioridade": 80,
      "inativa": true
    }
  ]
}
`

func sementeT(t *testing.T) Semente {
	t.Helper()
	s, err := CarregarSemente(strings.NewReader(sementeDeTermos))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func adicionar(t *testing.T, s Semente, p PedidoTermo) (Semente, MudancaDeTermos) {
	t.Helper()
	novo, m, err := s.AdicionarTermo(p, nil)
	if err != nil {
		t.Fatalf("AdicionarTermo(%+v): %v", p, err)
	}
	return novo, m
}

func entradaT(t *testing.T, s Semente, termo string) EntradaSemente {
	t.Helper()
	for _, e := range s.Entradas {
		if e.Canonica == termo {
			return e
		}
	}
	t.Fatalf("o termo %q não está na semente", termo)
	return EntradaSemente{}
}

func prompt(s Semente) string { return Vocabulario(s.ParaEntradas(), OrcamentoRunasPadrao) }

func TestAdicionarTermo_NovoComDica(t *testing.T) {
	s := sementeT(t)
	antes := s.copiaProfunda()
	novo, m := adicionar(t, s, PedidoTermo{Termo: "  RAG ", Variantes: []string{"rague", "HACI", "hage"}, Dica: true})

	if !reflect.DeepEqual(s, antes) {
		t.Error("AdicionarTermo mexeu na semente de entrada")
	}
	e := entradaT(t, novo, "RAG")
	if e.Origem != OrigemAdicionado || e.Prioridade != PrioridadeDica {
		t.Errorf("entrada = %+v, quero origem adicionado e prioridade %d", e, PrioridadeDica)
	}
	if want := []string{"HACI", "hage", "rague"}; !reflect.DeepEqual(e.Alias, want) {
		t.Errorf("alias = %v, quero %v (em ordem, como o dictlearn deixa)", e.Alias, want)
	}
	for _, v := range []string{"rague", "HACI", "hage"} {
		if novo.AliasOrigem[v] != OrigemAdicionado {
			t.Errorf("alias_origem[%q] = %q", v, novo.AliasOrigem[v])
		}
	}
	if novo.AliasOrigem["DAAS"] != "cont-04" || novo.AliasOrigem["ista"] != OrigemPainel {
		t.Errorf("as outras origens mudaram: %v", novo.AliasOrigem)
	}
	if m.Termo != "RAG" || m.JaExistia || len(m.Novas) != 3 || len(m.SairamDasDicas) != 0 {
		t.Errorf("mudança = %+v", m)
	}
	// A dica dela vem antes de tudo no prompt.
	if got := prompt(novo); !strings.HasPrefix(got, "RAG, DCTFWeb") {
		t.Errorf("prompt = %q, quero começando por RAG", got)
	}
	if got := m.Lista.Dicas; got != (Dicas{Usadas: 3, Orcamento: OrcamentoRunasPadrao}) {
		t.Errorf("dicas = %+v", got)
	}
	if want := []TermoAdicionado{{Termo: "RAG", Variantes: []string{"HACI", "hage", "rague"}, Dica: true}}; !reflect.DeepEqual(m.Lista.Itens, want) {
		t.Errorf("lista = %+v", m.Lista.Itens)
	}
}

// Sem a dica, o termo nunca vai ao prompt, nem num dicionário com espaço
// sobrando: "sem dica" tem de valer.
func TestAdicionarTermo_SemDicaNuncaVaiAoPrompt(t *testing.T) {
	novo, m := adicionar(t, sementeT(t), PedidoTermo{Termo: "RAG"})
	e := entradaT(t, novo, "RAG")
	if e.Prioridade != PrioridadeTermoAprendido || e.Origem != OrigemAdicionado {
		t.Errorf("entrada = %+v", e)
	}
	if got := prompt(novo); strings.Contains(got, "RAG") {
		t.Errorf("o termo sem dica entrou no prompt: %q", got)
	}
	if m.Lista.Dicas.Usadas != 0 || m.Lista.Itens[0].Dica {
		t.Errorf("lista = %+v", m.Lista)
	}
	// E é termo de verdade: não é AliasOnly, e a marca chega à entrada.
	for _, d := range novo.ParaEntradas() {
		if d.CanonicalForm == "RAG" && (d.AliasOnly || !d.Adicionado) {
			t.Errorf("entrada do repositório = %+v", d)
		}
	}
}

// As dicas dela vêm na ordem do arquivo, antes das de antes, mesmo com
// prioridade igual à de um termo de antes (DCTFWeb, 100) e com ordem
// alfabética contrária.
func TestVocabulario_DicasDaPessoaPrimeiroNaOrdemDoArquivo(t *testing.T) {
	s := sementeT(t)
	s, _ = adicionar(t, s, PedidoTermo{Termo: "Zeta", Dica: true})
	s, _ = adicionar(t, s, PedidoTermo{Termo: "Alfa", Dica: true})
	s, _ = adicionar(t, s, PedidoTermo{Termo: "Sem", Variantes: []string{"semdica"}})
	if got, want := prompt(s), "Zeta, Alfa, DCTFWeb, eSocial, DAS"; got != want {
		t.Errorf("prompt = %q, quero %q", got, want)
	}
}

func TestDisputaOPrompt(t *testing.T) {
	casos := []struct {
		nome    string
		e       DictionaryEntry
		disputa bool
		dica    bool
	}{
		{"termo comum", DictionaryEntry{CanonicalForm: "DAS", Active: true, Priority: 10}, true, false},
		{"inativo", DictionaryEntry{CanonicalForm: "DAS", Priority: 100}, false, false},
		{"do painel", DictionaryEntry{CanonicalForm: "esta", Active: true, AliasOnly: true, Priority: 100}, false, false},
		{"sem forma", DictionaryEntry{CanonicalForm: " ", Active: true}, false, false},
		{"adicionado sem dica", DictionaryEntry{CanonicalForm: "RAG", Active: true, Adicionado: true, Priority: PrioridadeDica - 1}, false, false},
		{"adicionado com dica", DictionaryEntry{CanonicalForm: "RAG", Active: true, Adicionado: true, Priority: PrioridadeDica}, true, true},
		{"adicionado com dica e desligado", DictionaryEntry{CanonicalForm: "RAG", Adicionado: true, Priority: PrioridadeDica}, false, false},
	}
	for _, c := range casos {
		if got := DisputaOPrompt(c.e); got != c.disputa {
			t.Errorf("%s: DisputaOPrompt = %v", c.nome, got)
		}
		if got := EDicaDaPessoa(c.e); got != c.dica {
			t.Errorf("%s: EDicaDaPessoa = %v", c.nome, got)
		}
	}
}

// O orçamento das dicas dela: cabe até o último caractere, e a que não cabe é
// recusada, dizendo quanto sobra. Uma dica nova nunca empurra outra dela.
func TestAdicionarTermo_DicaQueNaoCabeERecusada(t *testing.T) {
	s := sementeT(t)
	for _, letra := range []string{"a", "b", "c"} {
		s, _ = adicionar(t, s, PedidoTermo{Termo: strings.Repeat(letra, 40), Dica: true})
	}
	// 40 + 2 + 40 + 2 + 40 = 124: sobram 16.
	if got := s.TermosAdicionados().Dicas.Usadas; got != 124 {
		t.Fatalf("usadas = %d, quero 124", got)
	}
	_, _, err := s.AdicionarTermo(PedidoTermo{Termo: strings.Repeat("d", 15), Dica: true}, nil)
	var semEspaco *ErroSemEspaco
	if !errors.As(err, &semEspaco) || semEspaco.Sobram != 16 || !errors.Is(err, ErrSemEspaco) {
		t.Fatalf("err = %v (%+v), quero ErroSemEspaco com 16", err, semEspaco)
	}
	// 2 + 14 = 16: cabe, exatamente.
	cheio, m := adicionar(t, s, PedidoTermo{Termo: strings.Repeat("d", 14), Dica: true})
	if got := cheio.TermosAdicionados().Dicas.Usadas; got != OrcamentoRunasPadrao {
		t.Errorf("usadas = %d, quero o orçamento inteiro", got)
	}
	// As dela ocupam tudo: os termos de antes saíram do prompt, e a resposta diz.
	if want := []string{"DCTFWeb", "DAS"}; !reflect.DeepEqual(m.SairamDasDicas, want) {
		t.Errorf("saíram = %v, quero %v", m.SairamDasDicas, want)
	}
	// Sem dica, o mesmo termo entra: só a dica disputa o orçamento.
	if _, _, err := cheio.AdicionarTermo(PedidoTermo{Termo: "outro"}, nil); err != nil {
		t.Errorf("sem dica: %v", err)
	}
}

// Quando cabe, a dica empurra para fora termos de antes, e a resposta diz
// quais.
func TestAdicionarTermo_DizQuemSaiuDasDicas(t *testing.T) {
	s := sementeT(t)
	s, _ = adicionar(t, s, PedidoTermo{Termo: strings.Repeat("a", 40), Dica: true})
	s, _ = adicionar(t, s, PedidoTermo{Termo: strings.Repeat("b", 40), Dica: true})
	_, m := adicionar(t, s, PedidoTermo{Termo: strings.Repeat("c", 40), Dica: true})
	// 124 das dela; DCTFWeb (9) cabe, eSocial (9) não, DAS (5) cabe.
	if want := []string{"eSocial"}; !reflect.DeepEqual(m.SairamDasDicas, want) {
		t.Errorf("saíram = %v, quero %v", m.SairamDasDicas, want)
	}
}

func TestAdicionarTermo_TermoQueJaEDela(t *testing.T) {
	s, _ := adicionar(t, sementeT(t), PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}, Dica: true})

	// Grafias novas entram; a dica segue o pedido.
	s2, m := adicionar(t, s, PedidoTermo{Termo: "rag", Variantes: []string{"hage"}, Dica: false})
	e := entradaT(t, s2, "RAG")
	if !reflect.DeepEqual(e.Alias, []string{"hage", "rague"}) || e.Prioridade != PrioridadeTermoAprendido {
		t.Errorf("entrada = %+v", e)
	}
	if !m.JaExistia || m.Termo != "RAG" {
		t.Errorf("mudança = %+v", m)
	}
	// Só a dica: vale.
	s3, _ := adicionar(t, s2, PedidoTermo{Termo: "RAG", Dica: true})
	if entradaT(t, s3, "RAG").Prioridade != PrioridadeDica {
		t.Error("a dica não voltou")
	}
	// Nada novo: recusado.
	if _, _, err := s3.AdicionarTermo(PedidoTermo{Termo: "RAG", Dica: true}, nil); !errors.Is(err, ErrNadaAMudar) {
		t.Errorf("err = %v, quero ErrNadaAMudar", err)
	}
	// A grafia que já é dele: recusada, dizendo qual.
	_, _, err := s3.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"Rague"}, Dica: true}, nil)
	var daVariante *ErroDaVariante
	if !errors.Is(err, ErrJaExiste) || !errors.As(err, &daVariante) || daVariante.Variante != "Rague" {
		t.Errorf("err = %v, quero ErrJaExiste da grafia Rague", err)
	}
}

// O termo que o painel criou é adotado: vira dela, com a dica do pedido, e as
// variantes do painel continuam do painel (saem pelo Desfazer de lá).
func TestAdicionarTermo_AdotaOTermoDoPainel(t *testing.T) {
	novo, m := adicionar(t, sementeT(t), PedidoTermo{Termo: "Esta", Variantes: []string{"estta"}, Dica: true})
	e := entradaT(t, novo, "esta")
	if e.Origem != OrigemAdicionado || e.Prioridade != PrioridadeDica {
		t.Errorf("entrada = %+v", e)
	}
	if novo.AliasOrigem["ista"] != OrigemPainel || novo.AliasOrigem["estta"] != OrigemAdicionado {
		t.Errorf("origens = %v", novo.AliasOrigem)
	}
	if !m.JaExistia {
		t.Error("ja_existia falso para o termo do painel")
	}
	if got := novo.VariantesDoPainel(); len(got) != 1 || got[0].Variante != "ista" {
		t.Errorf("o aprendido no painel mudou: %v", got)
	}
	if want := []TermoAdicionado{{Termo: "esta", Variantes: []string{"estta"}, Dica: true}}; !reflect.DeepEqual(m.Lista.Itens, want) {
		t.Errorf("lista = %+v", m.Lista.Itens)
	}
}

// No termo de antes, as grafias entram e a dica não muda (decisão 3).
func TestAdicionarTermo_TermoDeAntesSoGanhaAsGrafias(t *testing.T) {
	novo, m := adicionar(t, sementeT(t), PedidoTermo{Termo: "das", Variantes: []string{"daz"}, Dica: false})
	e := entradaT(t, novo, "DAS")
	if e.Prioridade != 90 || e.Origem != "" || !reflect.DeepEqual(e.Alias, []string{"DAAS", "daz"}) {
		t.Errorf("entrada = %+v", e)
	}
	if want := []TermoAdicionado{{Termo: "DAS", Variantes: []string{"daz"}, DoDicionario: true}}; !reflect.DeepEqual(m.Lista.Itens, want) {
		t.Errorf("lista = %+v", m.Lista.Itens)
	}
	if m.DicaIgnorada {
		t.Error("sem a dica no pedido, a mudança diz que ignorou a dica")
	}
	// Com a dica, as grafias entram, a prioridade fica, e a mudança diz que a
	// dica não mudou.
	novo, m = adicionar(t, sementeT(t), PedidoTermo{Termo: "eSocial", Variantes: []string{"isocial"}, Dica: true})
	if e := entradaT(t, novo, "eSocial"); e.Prioridade != 95 || e.Origem != "" || !m.DicaIgnorada {
		t.Errorf("entrada = %+v, dica ignorada = %v", e, m.DicaIgnorada)
	}
	// Só com a dica, o pedido é recusado dizendo por quê, e não "já está assim".
	if _, _, err := sementeT(t).AdicionarTermo(PedidoTermo{Termo: "eSocial", Dica: true}, nil); !errors.Is(err, ErrDicaDoDicionario) {
		t.Errorf("err = %v, quero ErrDicaDoDicionario", err)
	}
	if _, _, err := sementeT(t).AdicionarTermo(PedidoTermo{Termo: "eSocial"}, nil); !errors.Is(err, ErrNadaAMudar) {
		t.Errorf("sem a dica: err = %v, quero ErrNadaAMudar", err)
	}
	// O termo dela e o do painel seguem a dica, e não a ignoram.
	_, m = adicionar(t, sementeT(t), PedidoTermo{Termo: "esta", Dica: true})
	if m.DicaIgnorada {
		t.Error("o termo do painel adotado diz que ignorou a dica")
	}
	novo, _ = adicionar(t, sementeT(t), PedidoTermo{Termo: "RAG"})
	if _, m = adicionar(t, novo, PedidoTermo{Termo: "RAG", Dica: true}); m.DicaIgnorada {
		t.Error("o termo dela diz que ignorou a dica")
	}
}

func TestAdicionarTermo_Recusas(t *testing.T) {
	s := sementeT(t)
	comum := func(texto string, _ bool) string {
		if texto == "de" || texto == "2025" {
			return "palavra_de_ligacao"
		}
		return ""
	}
	casos := []struct {
		nome string
		p    PedidoTermo
		quer error
	}{
		{"vazio", PedidoTermo{Termo: " ?! "}, ErrVazio},
		{"quebra de linha", PedidoTermo{Termo: "RAG\nX"}, ErrInvalido},
		{"tab", PedidoTermo{Termo: "R\tAG"}, ErrInvalido},
		{"cinco palavras", PedidoTermo{Termo: "um dois três quatro cinco"}, ErrInvalido},
		{"41 caracteres", PedidoTermo{Termo: strings.Repeat("a", 41)}, ErrInvalido},
		{"grafia de uma letra", PedidoTermo{Termo: "RAG", Variantes: []string{"r"}}, ErrInvalido},
		{"grafia com quebra", PedidoTermo{Termo: "RAG", Variantes: []string{"ra\ngue"}}, ErrInvalido},
		{"grafia que é o termo", PedidoTermo{Termo: "RAG", Variantes: []string{"rág"}}, ErrSemNovidade},
		{"grafia de outro termo", PedidoTermo{Termo: "RAG", Variantes: []string{"daas"}}, ErrVarianteDeOutroTermo},
		{"grafia que é outro termo", PedidoTermo{Termo: "RAG", Variantes: []string{"e social"}}, ErrVarianteDeOutroTermo},
		{"termo que é variante de outro", PedidoTermo{Termo: "Daas"}, ErrTermoEVarianteDeOutro},
		{"termo desligado", PedidoTermo{Termo: "desligado", Variantes: []string{"deslig"}}, ErrTermoInativo},
		{"palavra comum", PedidoTermo{Termo: "de"}, ErrPalavraComum},
		{"grafia comum", PedidoTermo{Termo: "RAG", Variantes: []string{"2025"}}, ErrPalavraComum},
		{"seis grafias", PedidoTermo{Termo: "RAG", Variantes: []string{"aa", "bb", "cc", "dd", "ee", "ff"}}, ErrLimite},
	}
	for _, c := range casos {
		novo, m, err := s.AdicionarTermo(c.p, comum)
		if !errors.Is(err, c.quer) {
			t.Errorf("%s: err = %v, quero %v", c.nome, err, c.quer)
		}
		if !reflect.DeepEqual(novo, s) || !reflect.DeepEqual(m, MudancaDeTermos{}) {
			t.Errorf("%s: a recusa devolveu outra semente ou uma mudança", c.nome)
		}
	}
	// A palavra comum diz qual texto, e o motivo de quem conferiu.
	_, _, err := s.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"ok", "2025"}}, comum)
	var pc *ErroPalavraComum
	if !errors.As(err, &pc) || pc.Texto != "2025" || pc.Motivo != "palavra_de_ligacao" {
		t.Errorf("err = %v (%+v)", err, pc)
	}
	// O termo que já era do dicionário não é conferido como palavra comum: só
	// as grafias novas.
	if _, _, err := s.AdicionarTermo(PedidoTermo{Termo: "DAS", Variantes: []string{"daz"}}, func(x string, _ bool) string {
		if x == "DAS" {
			return "palavra_de_ligacao"
		}
		return ""
	}); err != nil {
		t.Errorf("o termo de antes foi conferido como palavra comum: %v", err)
	}
	// A grafia que é de outro termo diz qual grafia, e de quem.
	_, _, err = s.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"bom", "daas"}}, nil)
	var dv *ErroDaVariante
	var outro *ErroVarianteDeOutroTermo
	if !errors.As(err, &dv) || dv.Variante != "daas" || !errors.As(err, &outro) || outro.Dono != "DAS" {
		t.Errorf("err = %v", err)
	}
	// O termo desligado é citado como está no dicionário.
	_, _, err = s.AdicionarTermo(PedidoTermo{Termo: "desligado", Variantes: []string{"deslig"}}, nil)
	var inativo *ErroTermoInativoDe
	if !errors.As(err, &inativo) || inativo.Termo != "Desligado" {
		t.Errorf("err = %v", err)
	}
}

func TestAdicionarTermo_GrafiasEmBrancoERepetidasSaem(t *testing.T) {
	novo, m := adicionar(t, sementeT(t), PedidoTermo{Termo: "RAG", Variantes: []string{" rague ", "", "  ", "RAGUE", "ha  ge"}})
	if want := []string{"ha ge", "rague"}; !reflect.DeepEqual(entradaT(t, novo, "RAG").Alias, want) {
		t.Errorf("alias = %v, quero %v", entradaT(t, novo, "RAG").Alias, want)
	}
	if len(m.Novas) != 2 {
		t.Errorf("novas = %v", m.Novas)
	}
}

func TestAdicionarTermo_Tetos(t *testing.T) {
	s := sementeT(t)
	// 10 grafias por termo, as dela.
	s, _ = adicionar(t, s, PedidoTermo{Termo: "RAG", Variantes: []string{"aa", "bb", "cc", "dd", "ee"}})
	s, _ = adicionar(t, s, PedidoTermo{Termo: "RAG", Variantes: []string{"ff", "gg", "hh", "ii", "jj"}})
	_, _, err := s.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"kk"}}, nil)
	var lim *ErroLimite
	if !errors.As(err, &lim) || lim.Do != LimiteVariantesDoTermo || lim.Maximo != MaxVariantesPorTermo {
		t.Errorf("err = %v (%+v)", err, lim)
	}

	// 50 termos dela.
	s = sementeT(t)
	for i := 0; i < MaxTermosAdicionados; i++ {
		s, _ = adicionar(t, s, PedidoTermo{Termo: fmt.Sprintf("termo%02d", i)})
	}
	_, _, err = s.AdicionarTermo(PedidoTermo{Termo: "mais um"}, nil)
	if !errors.As(err, &lim) || lim.Do != LimiteTermos {
		t.Errorf("err = %v", err)
	}
	// O teto é de termos dela: grafia num termo que já é dela ainda entra.
	if _, _, err := s.AdicionarTermo(PedidoTermo{Termo: "termo07", Variantes: []string{"termu"}}, nil); err != nil {
		t.Errorf("grafia num termo dela, no teto: %v", err)
	}
}

func TestDicaDoTermo(t *testing.T) {
	s, _ := adicionar(t, sementeT(t), PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}, Dica: true})

	desligada, m, err := s.DicaDoTermo("rag", false)
	if err != nil {
		t.Fatal(err)
	}
	if entradaT(t, desligada, "RAG").Prioridade != PrioridadeTermoAprendido || strings.Contains(prompt(desligada), "RAG") {
		t.Error("a dica não desligou")
	}
	if m.Termo != "RAG" || m.Lista.Itens[0].Dica || len(m.SairamDasDicas) != 0 {
		t.Errorf("mudança = %+v", m)
	}
	ligada, _, err := desligada.DicaDoTermo("RAG", true)
	if err != nil || !strings.HasPrefix(prompt(ligada), "RAG") {
		t.Errorf("a dica não voltou: %v", err)
	}
	// Pedir o que já está não é erro, e não muda nada.
	igual, _, err := ligada.DicaDoTermo("RAG", true)
	if err != nil || !reflect.DeepEqual(igual, ligada) {
		t.Errorf("ligar de novo: %v", err)
	}

	for _, termo := range []string{"DAS", "esta", "nenhum"} {
		if _, _, err := s.DicaDoTermo(termo, false); !errors.Is(err, ErrNaoAdicionado) {
			t.Errorf("%s: err = %v, quero ErrNaoAdicionado", termo, err)
		}
	}
	if _, _, err := s.DicaDoTermo(" ", true); !errors.Is(err, ErrVazio) {
		t.Errorf("vazio: %v", err)
	}
}

func TestDicaDoTermo_LigarSemEspaco(t *testing.T) {
	s := sementeT(t)
	for _, letra := range []string{"a", "b", "c"} {
		s, _ = adicionar(t, s, PedidoTermo{Termo: strings.Repeat(letra, 40), Dica: true})
	}
	s, _ = adicionar(t, s, PedidoTermo{Termo: strings.Repeat("d", 15)})
	_, _, err := s.DicaDoTermo(strings.Repeat("d", 15), true)
	var se *ErroSemEspaco
	if !errors.As(err, &se) || se.Sobram != 16 {
		t.Errorf("err = %v (%+v)", err, se)
	}
}

// Adicionar e Remover, sem mais nada ter mudado, devolve a semente de antes.
func TestRemoverTermo_VoltaAoQueEra(t *testing.T) {
	s := sementeT(t)
	com, _ := adicionar(t, s, PedidoTermo{Termo: "RAG", Variantes: []string{"rague", "hage"}, Dica: true})
	sem, m, err := com.RemoverTermo("rag")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sem, s) {
		t.Errorf("Remover não voltou ao que era:\n%#v\n%#v", sem, s)
	}
	if m.Termo != "RAG" || len(m.Lista.Itens) != 0 {
		t.Errorf("mudança = %+v", m)
	}
}

func TestRemoverTermo_TermoDeAntesPerdeSoAsGrafiasDela(t *testing.T) {
	s := sementeT(t)
	com, _ := adicionar(t, s, PedidoTermo{Termo: "DAS", Variantes: []string{"daz"}})
	sem, _, err := com.RemoverTermo("DAS")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sem, s) {
		t.Errorf("o termo de antes não voltou ao que era: %+v", entradaT(t, sem, "DAS"))
	}
	// Sem grafia dela, não há o que remover.
	if _, _, err := s.RemoverTermo("DAS"); !errors.Is(err, ErrNaoAdicionado) {
		t.Errorf("err = %v", err)
	}
}

// O termo do painel adotado volta ao painel: as variantes de lá ficam, com a
// prioridade de termo aprendido, e o Desfazer de lá continua funcionando.
func TestRemoverTermo_OAdotadoVoltaAoPainel(t *testing.T) {
	s := sementeT(t)
	com, _ := adicionar(t, s, PedidoTermo{Termo: "esta", Variantes: []string{"estta"}, Dica: true})
	sem, _, err := com.RemoverTermo("esta")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sem, s) {
		t.Errorf("não voltou ao painel: %+v", entradaT(t, sem, "esta"))
	}
	// Sem a origem dela, o termo do painel não sai por aqui.
	if _, _, err := s.RemoverTermo("esta"); !errors.Is(err, ErrNaoAdicionado) {
		t.Errorf("err = %v", err)
	}
	if _, _, err := s.RemoverTermo("nenhum"); !errors.Is(err, ErrNaoAdicionado) {
		t.Errorf("err = %v", err)
	}
}

// chavesDaEntradaEm41c30be são os campos de uma entrada que o Agent de
// 41c30be conhece (EntradaSemente naquele commit). O carregamento recusa
// campo desconhecido: nada fora daqui pode aparecer no arquivo.
var chavesDaEntradaEm41c30be = map[string]bool{
	"canonica": true, "prioridade": true, "dominio": true, "idioma": true,
	"alias": true, "alias_restrito": true, "restrito": true, "inativa": true, "origem": true,
}

var chavesDaSementeEm41c30be = map[string]bool{
	"usuario": true, "dominios": true, "alias_origem": true, "entradas": true,
}

// Gravar não escreve campo novo: o Agent de antes carrega o que o painel
// gravou.
func TestArquivo_TermoAdicionadoNaoPoeCampoNovoNoJSON(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDeTermos)
	if _, _, err := a.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}, Dica: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.AdicionarTermo(PedidoTermo{Termo: "Outro"}); err != nil {
		t.Fatal(err)
	}
	dados := lerBytes(t, caminho)

	var bruto struct {
		Entradas []map[string]json.RawMessage `json:"entradas"`
	}
	var topo map[string]json.RawMessage
	if err := json.Unmarshal(dados, &topo); err != nil {
		t.Fatal(err)
	}
	for k := range topo {
		if !chavesDaSementeEm41c30be[k] {
			t.Errorf("campo novo no topo do arquivo: %q", k)
		}
	}
	if err := json.Unmarshal(dados, &bruto); err != nil {
		t.Fatal(err)
	}
	for _, e := range bruto.Entradas {
		for k := range e {
			if !chavesDaEntradaEm41c30be[k] {
				t.Errorf("campo novo numa entrada: %q", k)
			}
		}
	}
	if !bytes.Contains(dados, []byte(`"origem": "adicionado"`)) || !bytes.Contains(dados, []byte(`"rague": "adicionado"`)) {
		t.Errorf("o arquivo não marca o que é dela:\n%s", dados)
	}
	// E o arquivo carrega como sempre, com o termo sendo termo de verdade.
	s, err := CarregarSemente(bytes.NewReader(dados))
	if err != nil {
		t.Fatal(err)
	}
	if e := entradaT(t, s, "RAG"); e.Prioridade != PrioridadeDica {
		t.Errorf("entrada = %+v", e)
	}
}

// Adicionar e remover, num arquivo no formato que Gravar escreve, devolve os
// mesmos bytes.
func TestArquivo_AdicionarERemoverDeixaOArquivoIdentico(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDeTermos)
	antes := lerBytes(t, caminho)
	if _, _, err := a.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}, Dica: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.AdicionarTermo(PedidoTermo{Termo: "DAS", Variantes: []string{"daz"}}); err != nil {
		t.Fatal(err)
	}
	for _, termo := range []string{"RAG", "DAS"} {
		if _, _, err := a.RemoverTermo(termo); err != nil {
			t.Fatal(err)
		}
	}
	if depois := lerBytes(t, caminho); !bytes.Equal(antes, depois) {
		t.Errorf("o arquivo não voltou ao que era:\n%s", depois)
	}
	exigirSoOArquivo(t, caminho)
}

// ConferirMudanca vê o antes, o depois e as grafias novas; o que ela recusa
// não chega ao disco, e chega a quem chama como está.
func TestArquivo_ConferirMudanca(t *testing.T) {
	t.Run("vê antes, depois e as novas", func(t *testing.T) {
		a, _ := arquivoDeTeste(t, sementeDeTermos)
		var visto struct {
			antes, depois int
			novas         []Aprendida
		}
		a.ConferirMudanca = func(antes, depois Semente, novas []Aprendida) error {
			visto.antes, visto.depois, visto.novas = len(antes.Entradas), len(depois.Entradas), novas
			return nil
		}
		if _, _, err := a.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}}); err != nil {
			t.Fatal(err)
		}
		if visto.antes != 5 || visto.depois != 6 || !reflect.DeepEqual(visto.novas, []Aprendida{{Termo: "RAG", Variante: "rague"}}) {
			t.Errorf("visto = %+v", visto)
		}
	})
	for _, c := range []struct {
		nome string
		err  error
		quer error
	}{
		{"não agiria", &ErroNaoAgiria{Variante: "rague"}, ErrNaoAgiria},
		{"quebraria", &ErroQuebraria{Variante: "sf", Termo: "ECF"}, ErrQuebraria},
		{"outro erro", errors.New("o Smart não montou com RAG"), ErrNaoCarrega},
	} {
		t.Run(c.nome, func(t *testing.T) {
			a, caminho := arquivoDeTeste(t, sementeDeTermos)
			antes := lerBytes(t, caminho)
			a.ConferirMudanca = func(Semente, Semente, []Aprendida) error { return c.err }
			for _, op := range []func() error{
				func() error {
					_, _, err := a.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}})
					return err
				},
			} {
				err := op()
				if !errors.Is(err, c.quer) {
					t.Errorf("err = %v, quero %v", err, c.quer)
				}
				if strings.Contains(err.Error(), "RAG") || strings.Contains(err.Error(), "rague") {
					t.Errorf("o texto do erro traz o dicionário: %q", err.Error())
				}
			}
			if !bytes.Equal(lerBytes(t, caminho), antes) {
				t.Error("a recusa gravou")
			}
			exigirSoOArquivo(t, caminho)
		})
	}
	// O erro tipado chega como está, e não embrulhado como "não carrega": é
	// uma recusa de regra (409 na tela), e não um defeito (500).
	t.Run("o erro tipado chega como está", func(t *testing.T) {
		a, _ := arquivoDeTeste(t, sementeDeTermos)
		a.ConferirMudanca = func(Semente, Semente, []Aprendida) error {
			return &ErroQuebraria{Variante: "sf", Termo: "ECF"}
		}
		_, _, err := a.AdicionarTermo(PedidoTermo{Termo: "RAG"})
		var q *ErroQuebraria
		if !errors.As(err, &q) || q.Variante != "sf" || q.Termo != "ECF" || errors.Is(err, ErrNaoCarrega) {
			t.Errorf("err = %v", err)
		}
		a.ConferirMudanca = func(Semente, Semente, []Aprendida) error {
			return &ErroNaoAgiria{Variante: "rague"}
		}
		_, _, err = a.AdicionarTermo(PedidoTermo{Termo: "RAG"})
		var na *ErroNaoAgiria
		if !errors.As(err, &na) || na.Variante != "rague" || errors.Is(err, ErrNaoCarrega) {
			t.Errorf("err = %v", err)
		}
	})
}

// Dica e Remover também passam por Validar e por ConferirMudanca.
func TestArquivo_DicaERemoverPassamPelasConferencias(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDeTermos)
	if _, _, err := a.AdicionarTermo(PedidoTermo{Termo: "RAG", Dica: true}); err != nil {
		t.Fatal(err)
	}
	gravado := lerBytes(t, caminho)
	conferidas := 0
	a.ConferirMudanca = func(Semente, Semente, []Aprendida) error {
		conferidas++
		return &ErroQuebraria{Variante: "x", Termo: "Y"}
	}
	if _, _, err := a.DicaDoTermo("RAG", false); !errors.Is(err, ErrQuebraria) {
		t.Errorf("dica: %v", err)
	}
	if _, _, err := a.RemoverTermo("RAG"); !errors.Is(err, ErrQuebraria) {
		t.Errorf("remover: %v", err)
	}
	a.ConferirMudanca = nil
	a.Validar = func(Semente) error { return errors.New("não carrega") }
	if _, _, err := a.RemoverTermo("RAG"); !errors.Is(err, ErrNaoCarrega) {
		t.Errorf("remover com Validar: %v", err)
	}
	if conferidas != 2 || !bytes.Equal(lerBytes(t, caminho), gravado) {
		t.Errorf("conferidas = %d, ou o arquivo mudou", conferidas)
	}
}

func TestArquivo_ListarTermosLeODisco(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDeTermos)
	if l, err := a.ListarTermos(); err != nil || len(l.Itens) != 0 || l.Itens == nil || l.Dicas.Orcamento != OrcamentoRunasPadrao {
		t.Fatalf("lista = %+v, %v", l, err)
	}
	// Outro processo grava: a lista de agora já vê.
	s := sementeT(t)
	s, _ = adicionar(t, s, PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}, Dica: true})
	if err := s.Gravar(caminho); err != nil {
		t.Fatal(err)
	}
	l, err := a.ListarTermos()
	if err != nil || len(l.Itens) != 1 || l.Itens[0].Termo != "RAG" || l.Dicas.Usadas != 3 {
		t.Errorf("lista = %+v, %v", l, err)
	}
	// O arquivo que não é nosso não é lido como lista vazia.
	if err := os.WriteFile(caminho, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListarTermos(); !errors.Is(err, ErrArquivoInvalido) {
		t.Errorf("err = %v", err)
	}
}

// A palavra certa é conferida com grafia falso, e cada grafia com grafia
// verdadeiro: a regra da grafia é a mais estrita (a polaridade em qualquer
// palavra). "Nada Consta" passa como termo, e a mesma regra recusa "não rague"
// como grafia.
func TestAdicionarTermo_PalavraComumDistingueTermoEGrafia(t *testing.T) {
	var vistos []string
	comum := func(x string, grafia bool) string {
		vistos = append(vistos, fmt.Sprintf("%s/%v", x, grafia))
		if grafia && strings.HasPrefix(x, "não ") {
			return "muda_sentido"
		}
		return ""
	}
	if _, _, err := sementeT(t).AdicionarTermo(PedidoTermo{Termo: "Nada Consta", Variantes: []string{"nada costa"}}, comum); err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"Nada Consta/false", "nada costa/true"}; !reflect.DeepEqual(vistos, want) {
		t.Errorf("conferidos = %v, quero %v", vistos, want)
	}
	_, _, err := sementeT(t).AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"rague", "não rague"}}, comum)
	var pc *ErroPalavraComum
	if !errors.As(err, &pc) || pc.Texto != "não rague" || pc.Motivo != "muda_sentido" {
		t.Errorf("err = %v (%+v)", err, pc)
	}
}

// A palavra comum que o Agent passa chega à conferência do pedido.
func TestArquivo_PalavraComumChegaAoPedido(t *testing.T) {
	a, caminho := arquivoDeTeste(t, sementeDeTermos)
	antes := lerBytes(t, caminho)
	a.PalavraComum = func(x string, grafia bool) string {
		if x == "das" && grafia {
			return "palavra_de_ligacao"
		}
		return ""
	}
	if _, _, err := a.AdicionarTermo(PedidoTermo{Termo: "RAG", Variantes: []string{"das"}}); !errors.Is(err, ErrPalavraComum) {
		t.Errorf("err = %v", err)
	}
	if !bytes.Equal(lerBytes(t, caminho), antes) {
		t.Error("a recusa gravou")
	}
}
