package dictionary

import (
	"context"
	"strings"
	"testing"
)

const sementeRestrita = `{
  "usuario": "m",
  "dominios": {"dev": ["Code.exe", "devenv.exe"], "contabil": ["SistemaContabil.exe"]},
  "entradas": [
    {"canonica": "commit", "dominio": "dev", "alias": ["comite"], "restrito": true},
    {"canonica": "DCTFWeb", "prioridade": 100, "alias": ["DCTF Web"]}
  ]
}`

func repoRestrito(t *testing.T) DictionaryRepository {
	t.Helper()
	s, err := CarregarSemente(strings.NewReader(sementeRestrita))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	repo := NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	return repo
}

func buscar(t *testing.T, repo DictionaryRepository, termo string, dominio *string) []DictionaryCandidate {
	t.Helper()
	got, err := repo.Search(context.Background(),
		DictionaryQuery{UserID: "m", Term: termo, Domain: dominio, Limit: 5})
	if err != nil {
		t.Fatalf("Search(%q): %v", termo, err)
	}
	return got
}

func ptr(s string) *string { return &s }

// rotulo rende um *string legível em mensagem de erro: "%v" num ponteiro
// imprime endereço, que não diz nada a quem está lendo a falha.
func rotulo(p *string) string {
	if p == nil {
		return "desconhecido"
	}
	return *p
}

// TestRestrito_DominioDesconhecidoNaoLibera é a trava central desta
// funcionalidade.
//
// "comite" -> "commit" é correção certa num editor de código e ERRO num
// e-mail sobre o comitê. O filtro normal de Domain não serve aqui: ele
// trata Domain nil como "sem restrição", que é o certo para LOOKUP e
// exatamente o contrário do que segurança pede.
//
// Por construção: quanto MENOS se sabe sobre onde a pessoa está
// digitando, MENOS o dicionário mexe no texto.
func TestRestrito_DominioDesconhecidoNaoLibera(t *testing.T) {
	repo := repoRestrito(t)

	if got := buscar(t, repo, "comite", nil); len(got) != 0 {
		t.Errorf("domínio DESCONHECIDO devolveu %+v — o alias perigoso dispararia "+
			"num e-mail sobre o comitê", got)
	}
	if got := buscar(t, repo, "comite", ptr("contabil")); len(got) != 0 {
		t.Errorf("domínio ERRADO devolveu %+v", got)
	}
	got := buscar(t, repo, "comite", ptr("dev"))
	if len(got) == 0 {
		t.Fatal("no domínio CERTO o alias não foi encontrado — a restrição bloqueou até onde devia valer")
	}
	if got[0].CanonicalForm != "commit" {
		t.Errorf("CanonicalForm = %q, want commit", got[0].CanonicalForm)
	}
}

// TestRestrito_NaoAfetaEntradaComum: a restrição é opt-in. Termo sem
// ela continua sendo corrigido em qualquer lugar — DCTFWeb não colide
// com nada e não deve depender de saber onde a pessoa está.
func TestRestrito_NaoAfetaEntradaComum(t *testing.T) {
	repo := repoRestrito(t)
	for _, dom := range []*string{nil, ptr("dev"), ptr("contabil")} {
		if got := buscar(t, repo, "DCTF Web", dom); len(got) == 0 {
			t.Errorf("entrada comum não foi encontrada com domínio %v", dom)
		}
	}
}

// TestDominioDoApp_TraduzOExecutavel.
func TestDominioDoApp_TraduzOExecutavel(t *testing.T) {
	s, err := CarregarSemente(strings.NewReader(sementeRestrita))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	casos := []struct {
		app  string
		quer *string
	}{
		{"Code.exe", ptr("dev")},
		{"code.exe", ptr("dev")},     // Windows não distingue caixa em nome de arquivo
		{"  Code.exe  ", ptr("dev")}, // espaço não pode mudar a resposta
		{"devenv.exe", ptr("dev")},
		{"SistemaContabil.exe", ptr("contabil")},
		{"chrome.exe", nil}, // navegador não está em domínio nenhum, de propósito
		{"", nil},
		{"desconhecido.exe", nil},
	}
	for _, c := range casos {
		got := s.DominioDoApp(c.app)
		switch {
		case c.quer == nil && got != nil:
			t.Errorf("DominioDoApp(%q) = %q, want nil", c.app, *got)
		case c.quer != nil && got == nil:
			t.Errorf("DominioDoApp(%q) = nil, want %q", c.app, *c.quer)
		case c.quer != nil && got != nil && *got != *c.quer:
			t.Errorf("DominioDoApp(%q) = %q, want %q", c.app, *got, *c.quer)
		}
	}
}

// TestDominioDoApp_EhDeterministico: um app listado em dois domínios
// daria resposta diferente a cada execução se dependesse da ordem do
// mapa, e o mesmo ditado corrigiria ou não conforme a sorte.
func TestDominioDoApp_EhDeterministico(t *testing.T) {
	s, err := CarregarSemente(strings.NewReader(
		`{"usuario":"m","dominios":{"zeta":["X.exe"],"alfa":["X.exe"]},"entradas":[]}`))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	primeiro := s.DominioDoApp("X.exe")
	if primeiro == nil {
		t.Fatal("DominioDoApp devolveu nil para app listado")
	}
	for i := 0; i < 50; i++ {
		got := s.DominioDoApp("X.exe")
		if got == nil || *got != *primeiro {
			t.Fatalf("execução %d deu %v, primeira deu %q", i, got, *primeiro)
		}
	}
}

// TestRestrito_PorAliasNaoPorEntrada trava a correção de granularidade
// de 27-set-2026.
//
// O perigo é do ALIAS, não da entrada: "postgres" -> "PostgreSQL"
// precisa de domínio (postgres é termo legítimo), mas "Postgre" ->
// "PostgreSQL" não precisa de nada. Marcar a ENTRADA como restrita
// trancava os dois juntos — e custou uma entidade na medição, porque o
// alias seguro parou de valer em amostra sem domínio conhecido.
func TestRestrito_PorAliasNaoPorEntrada(t *testing.T) {
	s, err := CarregarSemente(strings.NewReader(`{
	  "usuario":"m",
	  "entradas":[{"canonica":"PostgreSQL","dominio":"dev",
	               "alias":["Postgre"],"alias_restrito":["postgres"]}]}`))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	repo := NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}

	// O alias SEGURO vale onde a ENTRADA vale — inclusive com domínio
	// desconhecido, que é o caso que a restrição por entrada quebrava.
	//
	// Domínio "contabil" fica de fora de propósito: aí é o filtro normal
	// de Domain excluindo uma entrada "dev", que é comportamento certo e
	// não tem a ver com restrição.
	for _, dom := range []*string{nil, ptr("dev")} {
		if got := buscar(t, repo, "Postgre", dom); len(got) == 0 {
			t.Errorf("alias seguro não valeu com domínio %v — proteger o perigoso "+
				"não pode trancar o seguro da mesma entrada", rotulo(dom))
		}
	}

	// O alias PERIGOSO só vale no domínio certo.
	//
	// O que importa é a FORÇA do match, não a existência de candidato:
	// alias é auto-aplicado pelo resolver, fonético não é. "postgres" e
	// "PostgreSQL" são foneticamente parecidos de verdade, então um
	// candidato fonético continua aparecendo fora do domínio — e é
	// inofensivo, porque vira span incerto e nada é substituído.
	for _, dom := range []*string{nil, ptr("contabil")} {
		for _, c := range buscar(t, repo, "postgres", dom) {
			if c.Reason == MatchReasonAlias {
				t.Errorf("domínio %s: alias perigoso casou como ALIAS (%+v) — seria "+
					"auto-aplicado, e 'postgres' é termo legítimo", rotulo(dom), c)
			}
		}
	}
	got := buscar(t, repo, "postgres", ptr("dev"))
	if len(got) == 0 || got[0].Reason != MatchReasonAlias {
		t.Errorf("alias perigoso não valeu no domínio certo: %+v", got)
	}
}

// TestSemAliasDe_RemoveSoOsDaMetadeDeTeste é o que torna a medição de
// generalização honesta.
//
// Aprender alias das mesmas amostras usadas para medir faz o número
// validar a si mesmo — mede a capacidade de reproduzir erro já visto,
// que é pergunta diferente e muito mais fácil.
func TestSemAliasDe_RemoveSoOsDaMetadeDeTeste(t *testing.T) {
	s, err := CarregarSemente(strings.NewReader(`{
	  "usuario":"m",
	  "alias_origem":{"DAAS":"cont-04","Supabaese":"dev-02","comite":"cs-04"},
	  "entradas":[
	    {"canonica":"DAS","alias":["DAAS"]},
	    {"canonica":"Supabase","alias":["Supabaese"]},
	    {"canonica":"commit","dominio":"dev","alias_restrito":["comite"]},
	    {"canonica":"DCTFWeb","alias":["DCTF Web"]}
	  ]}`))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}

	recortada := s.SemAliasDe(map[string]bool{"dev-02": true, "cs-04": true})

	por := map[string]EntradaSemente{}
	for _, e := range recortada.Entradas {
		por[e.Canonica] = e
	}
	if len(por["Supabase"].Alias) != 0 {
		t.Errorf("alias de dev-02 sobreviveu: %v", por["Supabase"].Alias)
	}
	if len(por["commit"].AliasRestrito) != 0 {
		t.Errorf("alias restrito de cs-04 sobreviveu: %v", por["commit"].AliasRestrito)
	}
	if len(por["DAS"].Alias) != 1 {
		t.Errorf("alias de cont-04 (fora do teste) foi removido: %v", por["DAS"].Alias)
	}
	// Alias SEM procedência sobrevive: não há como saber que ele veio do
	// lado de teste, e remover por via das dúvidas mediria outra coisa.
	if len(por["DCTFWeb"].Alias) != 1 {
		t.Errorf("alias sem procedência foi removido: %v", por["DCTFWeb"].Alias)
	}
	// A forma CANÔNICA nunca sai: ela vem da pessoa, não da medição.
	if len(recortada.Entradas) != len(s.Entradas) {
		t.Errorf("entradas foram removidas: %d, want %d", len(recortada.Entradas), len(s.Entradas))
	}
}

// TestSemAliasDe_NaoMutaAOriginal: a semente recortada é uma cópia. Se
// mutasse a original, rodar holdout e depois a medição normal daria
// números errados na segunda.
func TestSemAliasDe_NaoMutaAOriginal(t *testing.T) {
	s, _ := CarregarSemente(strings.NewReader(
		`{"usuario":"m","alias_origem":{"DAAS":"cont-04"},
		  "entradas":[{"canonica":"DAS","alias":["DAAS"]}]}`))

	_ = s.SemAliasDe(map[string]bool{"cont-04": true})

	if len(s.Entradas[0].Alias) != 1 {
		t.Errorf("a semente original foi mutada: %v", s.Entradas[0].Alias)
	}
}
