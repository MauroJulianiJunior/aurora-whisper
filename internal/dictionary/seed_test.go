package dictionary

import (
	"context"
	"strings"
	"testing"
)

const sementeExemplo = `{
  "usuario": "mauro",
  "entradas": [
    {"canonica": "DCTFWeb", "prioridade": 100, "dominio": "contabil", "alias": ["DCTF Web"]},
    {"canonica": "Supabase", "prioridade": 90, "dominio": "dev"},
    {"canonica": "antigo", "prioridade": 10, "inativa": true}
  ]
}`

func carregar(t *testing.T, texto string) Semente {
	t.Helper()
	s, err := CarregarSemente(strings.NewReader(texto))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	return s
}

func TestCarregarSemente_LeOsCampos(t *testing.T) {
	s := carregar(t, sementeExemplo)
	if s.Usuario != "mauro" {
		t.Errorf("Usuario = %q", s.Usuario)
	}
	if len(s.Entradas) != 3 {
		t.Fatalf("len(Entradas) = %d, want 3", len(s.Entradas))
	}
	if s.Entradas[0].Alias[0] != "DCTF Web" {
		t.Errorf("alias = %q", s.Entradas[0].Alias)
	}
	if !s.Entradas[2].Inativa {
		t.Error("a terceira entrada deveria estar inativa")
	}
}

// TestCarregarSemente_CampoDesconhecidoEhErro.
//
// Um campo escrito errado ("prioriade") seria ignorado em silêncio pelo
// json padrão, e o termo entraria com prioridade zero — indo para o fim
// da fila do vocabulário sem ninguém entender por quê.
func TestCarregarSemente_CampoDesconhecidoEhErro(t *testing.T) {
	_, err := CarregarSemente(strings.NewReader(
		`{"usuario":"m","entradas":[{"canonica":"X","prioriade":10}]}`))
	if err == nil {
		t.Fatal("campo com nome errado passou em silêncio")
	}
}

// TestCarregarSemente_SemUsuarioEhErro: o dicionário é pessoal e Search
// filtra por UserID. Uma semente sem usuário popularia um dicionário que
// nenhuma busca encontraria.
func TestCarregarSemente_SemUsuarioEhErro(t *testing.T) {
	for _, corpo := range []string{
		`{"entradas":[{"canonica":"X"}]}`,
		`{"usuario":"   ","entradas":[{"canonica":"X"}]}`,
	} {
		if _, err := CarregarSemente(strings.NewReader(corpo)); err == nil {
			t.Errorf("semente sem usuário passou: %s", corpo)
		}
	}
}

// TestPopular_EhIdempotente é o que torna a medição do portão
// reproduzível: rodar a mesma semente duas vezes tem de produzir o mesmo
// dicionário, não dois termos duplicados com IDs diferentes.
func TestPopular_EhIdempotente(t *testing.T) {
	s := carregar(t, sementeExemplo)
	repo := NewInMemoryDictionaryRepository(nil)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := s.Popular(ctx, repo); err != nil {
			t.Fatalf("Popular (rodada %d): %v", i, err)
		}
	}

	got, err := repo.Search(ctx, DictionaryQuery{UserID: "mauro", Term: "DCTF Web", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Search devolveu %d candidatos depois de 3 populações, want 1: %+v", len(got), got)
	}
	if got[0].CanonicalForm != "DCTFWeb" {
		t.Errorf("CanonicalForm = %q, want DCTFWeb", got[0].CanonicalForm)
	}
}

// TestPopular_AliasViraMatchAutoAplicavel é o teste que justifica a
// Fatia 3 inteira.
//
// SmartResolver auto-aplica match `exact` e `alias`; match FONÉTICO
// sozinho é evidência fraca e só é aplicado se o contexto corroborar — e
// a captura de contexto não existe (TASK-009 é só contrato). Ou seja: em
// produção, alias é o ÚNICO caminho pelo qual o dicionário corrige
// alguma coisa.
//
// Se Popular gravasse os alias errado, tudo continuaria compilando, o
// Search acharia o termo por fonética, e a correção nunca seria
// aplicada.
func TestPopular_AliasViraMatchAutoAplicavel(t *testing.T) {
	s := carregar(t, sementeExemplo)
	repo := NewInMemoryDictionaryRepository(nil)
	ctx := context.Background()
	if err := s.Popular(ctx, repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}

	got, err := repo.Search(ctx, DictionaryQuery{UserID: "mauro", Term: "DCTF Web", Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("o alias não foi encontrado")
	}
	if got[0].Reason == MatchReasonPhonetic {
		t.Errorf("match saiu como FONÉTICO — o SmartResolver não auto-aplica isso sem contexto, "+
			"e contexto não existe. O alias não foi gravado como alias. (%+v)", got[0])
	}
}

// TestPopular_AliasIgualAFormaCanonicaEhDescartado: o match exato já
// resolve esse caso; guardar o alias só gastaria espaço e confundiria
// quem fosse ler o dicionário.
func TestPopular_AliasIgualAFormaCanonicaEhDescartado(t *testing.T) {
	s := carregar(t, `{"usuario":"m","entradas":[
		{"canonica":"DCTFWeb","alias":["dctfweb","DCTFWeb","  DCTFWeb  "]}]}`)
	repo := NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	got, err := repo.Search(context.Background(), DictionaryQuery{UserID: "m", Term: "DCTFWeb", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Reason != MatchReasonExact {
		t.Errorf("busca pela forma canônica devolveu %+v, want um único match exato", got)
	}
}

// TestPopular_ErroDizQualTermo: "ErrInvalidEntry" num arquivo de 43
// termos não diz qual deles quebrou.
func TestPopular_ErroDizQualTermo(t *testing.T) {
	s := Semente{Usuario: "m", Entradas: []EntradaSemente{
		{Canonica: "boa"},
		{Canonica: "   "},
	}}
	err := s.Popular(context.Background(), NewInMemoryDictionaryRepository(nil))
	if err == nil {
		t.Fatal("entrada sem forma canônica passou")
	}
	if !strings.Contains(err.Error(), "canônica") {
		t.Errorf("erro não explica o problema: %v", err)
	}
}

// TestParaEntradas_AlimentaOVocabulario liga as fatias 2 e 3: a semente
// precisa produzir exatamente o que Vocabulario consome.
func TestParaEntradas_AlimentaOVocabulario(t *testing.T) {
	s := carregar(t, sementeExemplo)
	vocab := Vocabulario(s.ParaEntradas(), OrcamentoRunasPadrao)

	if !strings.Contains(vocab, "DCTFWeb") {
		t.Errorf("vocabulário não contém o termo de maior prioridade: %q", vocab)
	}
	if strings.Contains(vocab, "antigo") {
		t.Errorf("termo INATIVO entrou no vocabulário: %q", vocab)
	}
	// Prioridade 100 antes de 90.
	if i, j := strings.Index(vocab, "DCTFWeb"), strings.Index(vocab, "Supabase"); i > j {
		t.Errorf("ordem de prioridade não foi respeitada: %q", vocab)
	}
}

// TestParaEntradas_IdBateComPopular: os dois caminhos têm de gerar o
// mesmo ID para a mesma entrada, senão o vocabulário e o repositório
// falariam de termos diferentes com o mesmo nome.
func TestParaEntradas_IdBateComPopular(t *testing.T) {
	s := carregar(t, sementeExemplo)
	repo := NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	got, err := repo.Search(context.Background(), DictionaryQuery{UserID: "mauro", Term: "Supabase", Limit: 1})
	if err != nil || len(got) == 0 {
		t.Fatalf("Search: %v / %d", err, len(got))
	}

	var esperado string
	for _, e := range s.ParaEntradas() {
		if e.CanonicalForm == "Supabase" {
			esperado = e.ID
		}
	}
	if got[0].EntryID != esperado {
		t.Errorf("EntryID = %q, mas ParaEntradas gerou %q", got[0].EntryID, esperado)
	}
}
