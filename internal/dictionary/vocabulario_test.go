package dictionary

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func entrada(canonica string, prioridade int) DictionaryEntry {
	return DictionaryEntry{CanonicalForm: canonica, Priority: prioridade, Active: true}
}

// TestVocabulario_RespeitaOOrcamento é a razão de esta função existir.
//
// Sem limite, um dicionário pessoal — que cresce a cada termo aprendido —
// deixaria o ditado mais lento para sempre, de forma gradual e sem causa
// visível. O orçamento é o que impede isso.
func TestVocabulario_RespeitaOOrcamento(t *testing.T) {
	entradas := []DictionaryEntry{
		entrada("DCTFWeb", 10), entrada("PGDAS", 10), entrada("eSocial", 10),
		entrada("EFD-Reinf", 10), entrada("Supabase", 10), entrada("PostgreSQL", 10),
		entrada("Metalúrgica Toffanetti", 10), entrada("Rosângela Bittencourt", 10),
	}
	for _, orcamento := range []int{10, 30, 60, 140, 1000} {
		got := Vocabulario(entradas, orcamento)
		if n := utf8.RuneCountInString(got); n > orcamento {
			t.Errorf("orçamento %d: vocabulário tem %d runas: %q", orcamento, n, got)
		}
	}
}

// TestVocabulario_CortaEmTermoInteiro: meio termo enviesaria o
// decodificador para uma palavra que não existe — pior que não enviesar.
func TestVocabulario_CortaEmTermoInteiro(t *testing.T) {
	// "PostgreSQL" (10) + ", " (2) + "Supabase" (8) = 20. Orçamento 15
	// cabe só o primeiro.
	got := Vocabulario([]DictionaryEntry{
		entrada("PostgreSQL", 10), entrada("Supabase", 5),
	}, 15)

	if got != "PostgreSQL" {
		t.Errorf("Vocabulario = %q, want %q (o segundo não cabia inteiro)", got, "PostgreSQL")
	}
	for _, parcial := range []string{"Supab", "Supabas", "Supa"} {
		if strings.Contains(got, parcial) {
			t.Errorf("vocabulário contém termo cortado ao meio (%q): %q", parcial, got)
		}
	}
}

// TestVocabulario_TermoLongoNaoBloqueiaOsSeguintes.
//
// Se a função parasse no primeiro termo que não cabe, um único termo
// longo no topo da prioridade gastaria o orçamento inteiro sem entrar —
// e os termos curtos logo abaixo, que caberiam, ficariam de fora.
func TestVocabulario_TermoLongoNaoBloqueiaOsSeguintes(t *testing.T) {
	got := Vocabulario([]DictionaryEntry{
		entrada("Metalúrgica Toffanetti Indústria e Comércio Limitada", 100),
		entrada("DAS", 90),
		entrada("MEI", 80),
	}, 12)

	if !strings.Contains(got, "DAS") || !strings.Contains(got, "MEI") {
		t.Errorf("Vocabulario = %q — os termos curtos deveriam ter entrado no espaço que sobrou", got)
	}
}

// TestVocabulario_OrdenaPorPrioridade: o que importa mais para quem usa
// entra primeiro, e é o que sobrevive quando o orçamento aperta.
func TestVocabulario_OrdenaPorPrioridade(t *testing.T) {
	got := Vocabulario([]DictionaryEntry{
		entrada("baixa", 1), entrada("alta", 100), entrada("media", 50),
	}, 140)

	if got != "alta, media, baixa" {
		t.Errorf("Vocabulario = %q, want %q", got, "alta, media, baixa")
	}
}

// TestVocabulario_EhDeterministico: o mesmo dicionário precisa produzir
// sempre o mesmo prompt, senão a medição do portão não é reproduzível —
// duas execuções dariam números diferentes sem nada ter mudado.
func TestVocabulario_EhDeterministico(t *testing.T) {
	entradas := []DictionaryEntry{
		entrada("zebra", 10), entrada("alfa", 10), entrada("meio", 10), entrada("beta", 10),
	}
	primeiro := Vocabulario(entradas, 140)
	for i := 0; i < 20; i++ {
		if got := Vocabulario(entradas, 140); got != primeiro {
			t.Fatalf("execução %d deu resultado diferente:\n  %q\n  %q", i, primeiro, got)
		}
	}
	// Empate de prioridade resolve alfabeticamente.
	if primeiro != "alfa, beta, meio, zebra" {
		t.Errorf("empate de prioridade não resolveu alfabeticamente: %q", primeiro)
	}
}

// TestVocabulario_IgnoraInativa: desativar um termo tem de tirá-lo também
// do viés do ASR. Senão "desativado" não significaria nada — o termo
// continuaria influenciando a transcrição.
func TestVocabulario_IgnoraInativa(t *testing.T) {
	got := Vocabulario([]DictionaryEntry{
		{CanonicalForm: "ativa", Priority: 10, Active: true},
		{CanonicalForm: "desativada", Priority: 100, Active: false},
	}, 140)

	if strings.Contains(got, "desativada") {
		t.Errorf("termo inativo entrou no vocabulário: %q", got)
	}
	if got != "ativa" {
		t.Errorf("Vocabulario = %q, want %q", got, "ativa")
	}
}

// TestVocabulario_OrcamentoZeroOuNegativoDevolveVazio: "sem orçamento" é
// "sem vocabulário", nunca "vocabulário ilimitado". O erro oposto faria o
// caminho de desligar o recurso ligá-lo no máximo.
func TestVocabulario_OrcamentoZeroOuNegativoDevolveVazio(t *testing.T) {
	entradas := []DictionaryEntry{entrada("DCTFWeb", 10)}
	for _, orcamento := range []int{0, -1, -1000} {
		if got := Vocabulario(entradas, orcamento); got != "" {
			t.Errorf("orçamento %d devolveu %q, want vazio", orcamento, got)
		}
	}
}

// TestVocabulario_SemEntradasDevolveVazio: dicionário vazio não pode
// gerar prompt, nem um separador solto.
func TestVocabulario_SemEntradasDevolveVazio(t *testing.T) {
	if got := Vocabulario(nil, 140); got != "" {
		t.Errorf("Vocabulario(nil) = %q, want vazio", got)
	}
	if got := Vocabulario([]DictionaryEntry{}, 140); got != "" {
		t.Errorf("Vocabulario([]) = %q, want vazio", got)
	}
}

// TestVocabulario_IgnoraFormaCanonicaEmBranco: entrada malformada não
// pode virar ", , " no prompt.
func TestVocabulario_IgnoraFormaCanonicaEmBranco(t *testing.T) {
	got := Vocabulario([]DictionaryEntry{
		{CanonicalForm: "   ", Priority: 100, Active: true},
		{CanonicalForm: "", Priority: 90, Active: true},
		entrada("DAS", 10),
	}, 140)

	if got != "DAS" {
		t.Errorf("Vocabulario = %q, want %q", got, "DAS")
	}
}

// TestVocabulario_ContaRunasNaoBytes: "Metalúrgica" tem acento, e contar
// bytes daria um orçamento efetivamente menor em português que em inglês
// — justamente no idioma principal do produto.
func TestVocabulario_ContaRunasNaoBytes(t *testing.T) {
	const termo = "Metalúrgica Toffanetti" // 22 runas, 23 bytes (ú tem 2)
	got := Vocabulario([]DictionaryEntry{entrada(termo, 10)}, 22)
	if got != termo {
		t.Errorf("Vocabulario = %q, want %q — 22 runas cabem em orçamento 22", got, termo)
	}
}

// TestOrcamentoRunasPadrao_CabeOPromptMedido ancora a constante na
// medição que a justifica: o prompt de 13 termos que levou a Entity
// Accuracy a 73,5% por +40 ms.
func TestOrcamentoRunasPadrao_CabeOPromptMedido(t *testing.T) {
	medido := []DictionaryEntry{
		entrada("DCTFWeb", 10), entrada("EFD-Reinf", 10), entrada("SPED", 10),
		entrada("PGDAS", 10), entrada("Supabase", 10), entrada("PostgreSQL", 10),
		entrada("Vite", 10), entrada("commit", 10), entrada("endpoint", 10),
		entrada("timeout", 10), entrada("Toffanetti", 10),
	}
	got := Vocabulario(medido, OrcamentoRunasPadrao)
	for _, termo := range medido {
		if !strings.Contains(got, termo.CanonicalForm) {
			t.Errorf("o orçamento padrão não coube %q — ele foi calibrado para o prompt medido:\n%q",
				termo.CanonicalForm, got)
		}
	}
	if n := utf8.RuneCountInString(got); n > OrcamentoRunasPadrao {
		t.Errorf("vocabulário tem %d runas, acima do próprio padrão (%d)", n, OrcamentoRunasPadrao)
	}
}

// TestVocabularioTermos_NaoDivergeDeVocabulario trava as duas contra
// deriva: se alguém mexer no corte de uma e não da outra, o Agent passa a
// logar um número que não descreve a string que ele mandou — que é
// exatamente o tipo de mentira que VocabularioTermos existe para acabar.
func TestVocabularioTermos_NaoDivergeDeVocabulario(t *testing.T) {
	entradas := []DictionaryEntry{
		entrada("DCTFWeb", 90), entrada("PostgreSQL", 80), entrada("Toffanetti", 70),
		entrada("eSocial", 60), entrada("Wispr", 50), entrada("MEI", 40),
		{CanonicalForm: "Inativa", Priority: 100, Active: false},
	}
	for _, orcamento := range []int{0, 1, 7, 8, 20, 35, 140, 1000} {
		termos := VocabularioTermos(entradas, orcamento)
		nomes := make([]string, 0, len(termos))
		for _, e := range termos {
			nomes = append(nomes, e.CanonicalForm)
		}
		quero := strings.Join(nomes, ", ")
		if got := Vocabulario(entradas, orcamento); got != quero {
			t.Errorf("orçamento %d: Vocabulario = %q, mas VocabularioTermos monta %q",
				orcamento, got, quero)
		}
	}
}

// TestVocabularioTermos_TornaOCorteVisivel é o teste do defeito de
// 28-set-2026.
//
// O dicionário de Mauro tinha 44 termos e só 17 chegavam ao ASR. O log
// dizia "termos=44" e nada mais, então "meu termo não chegou ao modelo" e
// "chegou e o modelo ignorou" eram indistinguíveis — e são problemas
// opostos. Descobrir qual dos dois era custou uma medição.
//
// O que este teste trava não é o número 17: é que a função CONSIGA
// responder "quantos entraram", que era a pergunta sem resposta.
func TestVocabularioTermos_TornaOCorteVisivel(t *testing.T) {
	entradas := []DictionaryEntry{
		entrada("PostgreSQL", 90), // 10 runas
		entrada("Toffanetti", 80), // +2+10 = 22
		entrada("Wispr", 50),      // +2+5  = 29
		entrada("MEI", 40),        // +2+3  = 34 -> não cabe em 30
	}
	termos := VocabularioTermos(entradas, 30)
	if len(termos) != 3 {
		t.Fatalf("entraram %d termos, esperava 3: %v", len(termos), termos)
	}
	if cortados := len(entradas) - len(termos); cortados != 1 {
		t.Errorf("cortados = %d, esperava 1", cortados)
	}
	// O último é o piso de prioridade que o Agent loga. Se a ordem
	// mudasse, o número logado deixaria de ser acionável.
	if piso := termos[len(termos)-1].Priority; piso != 50 {
		t.Errorf("menor prioridade no prompt = %d, esperava 50", piso)
	}
}

// TestVocabularioTermos_OrcamentoQueNaoCabeNadaDevolveVazio: o Agent
// indexa termos[len-1] para logar o piso, e só pode fazer isso porque
// "nenhum termo coube" é uma lista vazia e não um panic.
func TestVocabularioTermos_OrcamentoQueNaoCabeNadaDevolveVazio(t *testing.T) {
	entradas := []DictionaryEntry{entrada("PostgreSQL", 90), entrada("Toffanetti", 80)}
	for _, orcamento := range []int{-1, 0, 1, 9} {
		if got := VocabularioTermos(entradas, orcamento); len(got) != 0 {
			t.Errorf("orçamento %d devolveu %d termos, esperava 0", orcamento, len(got))
		}
	}
}
