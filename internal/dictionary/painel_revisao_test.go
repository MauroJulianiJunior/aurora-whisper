package dictionary

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Testes da revisão de 30-set-2026 sobre o aprendizado do painel.

// TestAprenderVariante_RecusaTermoQueEVarianteDeOutro: o termo que o painel
// CRIARIA já é variante de outro termo. Criá-lo sombrearia a regra
// anterior (o termo novo casa exato, e leva a melhor quando a prioridade é
// maior), que deixaria de valer sem aviso. Vale para as duas listas de
// alias, para termo inativo e para qualquer caixa.
func TestAprenderVariante_RecusaTermoQueEVarianteDeOutro(t *testing.T) {
	s := sementeDoPainel(t)
	antes := emJSON(t, s)
	casos := []struct{ nome, termo, dono string }{
		{"alias comum", "ECFX", "ECF"},
		{"alias comum, outra caixa", "ecfx", "ECF"},
		{"alias de dataset", "DAAS", "ECD"},
		{"alias restrito", "comite", "commit"},
		{"alias de termo inativo", "deslig", "Desligado"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, ap, err := s.AprenderVariante(c.termo, "outra coisa")
			if !errors.Is(err, ErrTermoEVarianteDeOutro) {
				t.Fatalf("erro = %v, quero ErrTermoEVarianteDeOutro", err)
			}
			var e *ErroTermoEVarianteDeOutro
			if !errors.As(err, &e) || e.Dono != c.dono {
				t.Errorf("Dono = %#v, quero %q", err, c.dono)
			}
			if strings.Contains(err.Error(), c.dono) || strings.Contains(err.Error(), c.termo) {
				t.Errorf("o texto do erro traz o conteúdo do dicionário: %q", err.Error())
			}
			if ap != (Aprendida{}) || emJSON(t, got) != antes {
				t.Error("devolveu Aprendida ou mudou a semente junto com o erro")
			}
		})
	}
}

// TestAprenderVariante_TermoVarianteDeOutro_AindaValeParaTermoQueExiste: a
// recusa é só de termo NOVO. Um termo que o dicionário já tem segue
// recebendo variante, mesmo que ele também seja alias de outro (o arquivo é
// da pessoa).
func TestAprenderVariante_TermoVarianteDeOutro_AindaValeParaTermoQueExiste(t *testing.T) {
	s := Semente{Usuario: "m", Entradas: []EntradaSemente{
		{Canonica: "ECF", Prioridade: 90, Alias: []string{"SF"}},
		{Canonica: "SF", Prioridade: 40},
	}}
	if _, _, err := s.AprenderVariante("SF", "esse efe"); err != nil {
		t.Errorf("termo que já existia foi recusado: %v", err)
	}
}

// TestAprenderVariante_PrioridadesDoTermoNovoNaoImportam: o caso da revisão.
// O termo já ser variante de outro é recusado seja qual for a prioridade do
// dono (40 e 50 ficam abaixo ou no nível do termo novo; 90 acima).
func TestAprenderVariante_PrioridadesDoTermoNovoNaoImportam(t *testing.T) {
	for _, prio := range []int{40, 50, 90} {
		s := Semente{Usuario: "m", Entradas: []EntradaSemente{
			{Canonica: "commit", Prioridade: prio, Dominio: "dev", AliasRestrito: []string{"comite"}, Restrito: true},
			{Canonica: "ECF", Prioridade: prio, Alias: []string{"SF"}},
		}}
		for _, termo := range []string{"comite", "SF"} {
			if _, _, err := s.AprenderVariante(termo, "cometi"); !errors.Is(err, ErrTermoEVarianteDeOutro) {
				t.Errorf("prioridade %d, termo %q: erro = %v", prio, termo, err)
			}
		}
	}
}

// TestAprenderVariante_RecusaVarianteDeOutroTermoRestrito: a variante que já
// é alias RESTRITO de outro termo também é dele (a lista restrita não é
// terra de ninguém). Pega a mutação de tirar a checagem de AliasRestrito da
// recusa.
func TestAprenderVariante_RecusaVarianteDeOutroTermoRestrito(t *testing.T) {
	s := sementeDoPainel(t)
	for _, variante := range []string{"comite", "COMITE", "co mi te"} {
		_, _, err := s.AprenderVariante("ECF", variante)
		var e *ErroVarianteDeOutroTermo
		if !errors.Is(err, ErrVarianteDeOutroTermo) || !errors.As(err, &e) || e.Dono != "commit" || e.ETermo {
			t.Errorf("variante %q: erro = %#v, quero ErrVarianteDeOutroTermo de commit", variante, err)
		}
	}
}

// TestAprenderVariante_RecusaVarianteQueOSmartNuncaProcura: o Smart só
// procura frases de até MaxPalavrasVariante palavras. Uma variante maior
// seria gravada e nunca agiria, e "aprendido" seria mentira.
func TestAprenderVariante_RecusaVarianteQueOSmartNuncaProcura(t *testing.T) {
	s := sementeDoPainel(t)
	antes := emJSON(t, s)
	for _, variante := range []string{
		"um dois tres quatro cinco",
		"um dois tres quatro cinco seis",
		"um-dois tres quatro cinco",     // o hífen parte a palavra, como o Smart
		"a1 b2 c3 d4 e5",                // dígitos contam
		"um, dois, tres, quatro, cinco", // pontuação não junta palavras
	} {
		if _, _, err := s.AprenderVariante("ECF", variante); !errors.Is(err, ErrInvalido) {
			t.Errorf("variante %q (%d palavras): erro = %v, quero ErrInvalido", variante, contarPalavras(variante), err)
		}
	}
	for _, variante := range []string{"um dois tres quatro", "um-dois tres quatro", "a1 b2 c3 d4"} {
		if _, _, err := s.AprenderVariante("ECF", variante); err != nil {
			t.Errorf("variante %q (%d palavras) no limite foi recusada: %v", variante, contarPalavras(variante), err)
		}
	}
	if emJSON(t, s) != antes {
		t.Error("a semente original mudou")
	}
	// O termo é a SAÍDA, e pode ter quantas palavras forem.
	if _, _, err := s.AprenderVariante("um dois tres quatro cinco seis", "ume"); err != nil {
		t.Errorf("termo longo recusado: %v", err)
	}
}

func TestContarPalavras(t *testing.T) {
	for entrada, quer := range map[string]int{
		"":              0,
		"  ":            0,
		"!!!":           0,
		"SF":            1,
		"n oito n":      3,
		"n8n":           1,
		"DCTF Web":      2,
		"x-y z":         3,
		"a,b;c":         3,
		"ação  não é":   3,
		"un deux trois": 3, // espaço sem quebra
	} {
		if got := contarPalavras(entrada); got != quer {
			t.Errorf("contarPalavras(%q) = %d, quero %d", entrada, got, quer)
		}
	}
}

// TestDesfazerVariante_VarianteQueAPessoaMoveuParaAliasRestrito: depois de
// aprender, a pessoa moveu a variante para a lista restrita (porque ela
// colide com palavra comum). O Desfazer ainda a acha. Pega a mutação de
// tirar o tratamento de AliasRestrito do Desfazer.
func TestDesfazerVariante_VarianteQueAPessoaMoveuParaAliasRestrito(t *testing.T) {
	s, _, err := sementeDoPainel(t).AprenderVariante("commit", "cometi")
	if err != nil {
		t.Fatal(err)
	}
	i := len(s.Entradas)
	for j := range s.Entradas {
		if s.Entradas[j].Canonica == "commit" {
			i = j
		}
	}
	e := s.Entradas[i]
	e.Alias = nil
	e.AliasRestrito = append(e.AliasRestrito, "cometi")
	s.Entradas[i] = e

	volta, ap, err := s.DesfazerVariante("cometi")
	if err != nil {
		t.Fatalf("DesfazerVariante: %v", err)
	}
	if ap != (Aprendida{Termo: "commit", Variante: "cometi"}) {
		t.Errorf("Aprendida = %+v", ap)
	}
	if got := entradaDe(t, volta, "commit"); strings.Join(got.AliasRestrito, ",") != "comite" {
		t.Errorf("AliasRestrito = %v, quero só o que já estava lá", got.AliasRestrito)
	}
	if _, achou := volta.AliasOrigem["cometi"]; achou {
		t.Error("sobrou a origem da variante desfeita")
	}
	if l := s.VariantesDoPainel(); len(l) != 1 || l[0].Variante != "cometi" {
		t.Errorf("VariantesDoPainel não lista a variante que está na lista restrita: %+v", l)
	}
}

// TestAprenderVariante_OrigemOrfaEhSobrescrita documenta a ÚNICA exceção à
// volta idêntica do Desfazer: uma chave de alias_origem que aponta para um
// alias que já não está em termo nenhum (uma origem órfã) é sobrescrita por
// "painel", e o Desfazer a apaga. A procedência antiga se perde. O estado é
// raro (só sobra por edição à mão) e recusar o clique por causa de um metadado
// órfão seria pior; fica documentado em AprenderVariante.
func TestAprenderVariante_OrigemOrfaEhSobrescrita(t *testing.T) {
	s := sementeDoPainel(t)
	s.AliasOrigem["SF"] = "cont-09" // órfã: nenhum termo tem o alias SF

	a, _, err := s.AprenderVariante("ECF", "SF")
	if err != nil {
		t.Fatal(err)
	}
	if a.AliasOrigem["SF"] != OrigemPainel {
		t.Errorf("a origem órfã não foi sobrescrita: %v", a.AliasOrigem)
	}
	volta, _, err := a.DesfazerVariante("SF")
	if err != nil {
		t.Fatal(err)
	}
	if _, achou := volta.AliasOrigem["SF"]; achou {
		t.Error("o Desfazer deixou uma origem para a variante que saiu")
	}
	if s.AliasOrigem["SF"] != "cont-09" {
		t.Error("a semente original foi mexida")
	}
}

func TestAusentes(t *testing.T) {
	base := Semente{Usuario: "m", Entradas: []EntradaSemente{
		{Canonica: "ECF", Prioridade: 90, Alias: []string{"SF"}},
		{Canonica: "DCTFWeb", Prioridade: 90},
	}}
	padrao := Semente{Usuario: "m", Entradas: []EntradaSemente{
		{Canonica: "ecf", Prioridade: 90, Alias: []string{"sf", "esse efe"}, AliasRestrito: []string{"efe"}}, // 2 variantes a mais
		{Canonica: "DCTF Web", Prioridade: 90},                               // o mesmo termo
		{Canonica: "Supabase", Prioridade: 90, Alias: []string{"Supabaese"}}, // termo novo (a variante dele não se conta à parte)
		{Canonica: "SPED", Prioridade: 90},                                   // termo novo
	}}
	if t2, v := padrao.Ausentes(base); t2 != 2 || v != 2 {
		t.Errorf("Ausentes = (%d termos, %d variantes), quero (2, 2)", t2, v)
	}
	if t2, v := base.Ausentes(base); t2 != 0 || v != 0 {
		t.Errorf("uma semente contra ela mesma: (%d, %d)", t2, v)
	}
	if t2, v := (Semente{Usuario: "m"}).Ausentes(base); t2 != 0 || v != 0 {
		t.Errorf("uma semente vazia: (%d, %d)", t2, v)
	}
}

// TestPopular_MarcaAEntradaDoPainelComoAliasOnly: Origem "painel" vira
// DictionaryEntry.AliasOnly, tanto pelo repositório quanto por
// ParaEntradas (que alimenta o prompt do ASR).
func TestPopular_MarcaAEntradaDoPainelComoAliasOnly(t *testing.T) {
	s := sementeDoPainel(t)
	s, _, err := s.AprenderVariante("Wispr Flow", "Whisperflow")
	if err != nil {
		t.Fatal(err)
	}
	s, _, err = s.AprenderVariante("ECF", "SF") // termo que já existia: NÃO é AliasOnly
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range s.ParaEntradas() {
		if quer := e.CanonicalForm == "Wispr Flow"; e.AliasOnly != quer {
			t.Errorf("ParaEntradas: %q AliasOnly = %v, quero %v", e.CanonicalForm, e.AliasOnly, quer)
		}
	}

	repo := NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	casos := map[string]struct {
		termo string
		razao MatchReason
		so    bool
	}{
		"whisperflow": {"Wispr Flow", MatchReasonAlias, true},
		"wispr flow":  {"Wispr Flow", MatchReasonExact, true},
		"sf":          {"ECF", MatchReasonAlias, false},
		"ecf":         {"ECF", MatchReasonExact, false},
	}
	for busca, c := range casos {
		cs, err := repo.Search(context.Background(), DictionaryQuery{UserID: "mauro", Term: busca})
		if err != nil || len(cs) == 0 {
			t.Fatalf("Search(%q) = %+v, %v", busca, cs, err)
		}
		if cs[0].CanonicalForm != c.termo || cs[0].Reason != c.razao || cs[0].AliasOnly != c.so {
			t.Errorf("Search(%q) = %+v, quero %s/%s/AliasOnly=%v", busca, cs[0], c.termo, c.razao, c.so)
		}
	}
}

// TestSearch_EntradaDoPainelNaoCasaPorFonetica: a entrada do painel só casa
// exato e por alias. A entrada igual sem a marca (controle) casa por
// fonética.
func TestSearch_EntradaDoPainelNaoCasaPorFonetica(t *testing.T) {
	busca := func(origem string) []DictionaryCandidate {
		s := Semente{Usuario: "m", Entradas: []EntradaSemente{{Canonica: "Comercial", Prioridade: 50, Alias: []string{"comerciau"}, Origem: origem}}}
		repo := NewInMemoryDictionaryRepository(nil)
		if err := s.Popular(context.Background(), repo); err != nil {
			t.Fatal(err)
		}
		cs, err := repo.Search(context.Background(), DictionaryQuery{UserID: "m", Term: "comersial"})
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	if cs := busca(""); len(cs) != 1 || cs[0].Reason != MatchReasonPhonetic {
		t.Fatalf("controle: a entrada da pessoa devia casar por fonética: %+v", cs)
	}
	if cs := busca(OrigemPainel); len(cs) != 0 {
		t.Errorf("a entrada do painel casou por fonética: %+v", cs)
	}
}

// TestVocabulario_NaoLevaOTermoDoPainel: a regra "o prompt do ASR só muda por
// decisão da pessoa" não pode depender de o dicionário ser grande. Num que
// cabe inteiro no orçamento, um termo de prioridade 50 entraria no prompt ao
// reiniciar. O mesmo termo SEM a origem (a pessoa o adotou) entra.
func TestVocabulario_NaoLevaOTermoDoPainel(t *testing.T) {
	vocabulario := func(origem string) string {
		s := Semente{Usuario: "m", Entradas: []EntradaSemente{
			{Canonica: "Alfa", Prioridade: 90},
			{Canonica: "Beta", Prioridade: 50, Alias: []string{"bt"}, Origem: origem},
			{Canonica: "Gama", Prioridade: 50},
		}}
		return Vocabulario(s.ParaEntradas(), OrcamentoRunasPadrao)
	}
	if got := vocabulario(OrigemPainel); got != "Alfa, Gama" {
		t.Errorf("com o termo do painel: %q, quero %q", got, "Alfa, Gama")
	}
	if got := vocabulario(""); got != "Alfa, Beta, Gama" {
		t.Errorf("controle, com o termo adotado: %q, quero %q", got, "Alfa, Beta, Gama")
	}
}
