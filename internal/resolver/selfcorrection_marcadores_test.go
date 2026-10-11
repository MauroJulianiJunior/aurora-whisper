package resolver

import "testing"

// Os marcadores "quer dizer" e "desculpa" são as formas MAIS comuns de
// se corrigir falando em PT-BR — e exatamente por serem comuns, também
// aparecem sem nenhuma intenção corretiva. Estes testes travam os dois
// lados dessa moeda.

func TestAutocorrecao_MarcadorIsoladoCorrige(t *testing.T) {
	casos := []struct{ entrada, quer string }{
		{"O vencimento é dia dez, desculpa, dia doze.", "O vencimento é dia doze."},
		{"Sobe no Supabase, quer dizer, no PostgreSQL local mesmo.", "Sobe no PostgreSQL local mesmo."},
		{"Manda para a Rosângela... não, na verdade manda para o Thiago.", "manda para o Thiago."},
	}
	for _, c := range casos {
		if got := applySelfCorrection(c.entrada).Text; got != c.quer {
			t.Errorf("%q\n  ->   %q\n  want %q", c.entrada, got, c.quer)
		}
	}
}

// TestAutocorrecao_MarcadorComConteudoNaoCorrige é a trava que impede
// destruição de texto.
//
// Medido em 27-set-2026: tratando "quer dizer" e "desculpa" como
// marcadores de prefixo comuns, "O prazo venceu, quer dizer que vai ter
// multa" virava "que vai ter multa". A frase inteira desaparecia.
//
// O discriminador é a oração ser EXATAMENTE o marcador: ", quer dizer,"
// corrige; "quer dizer que ..." explica.
func TestAutocorrecao_MarcadorComConteudoNaoCorrige(t *testing.T) {
	intocadas := []string{
		"O prazo venceu, quer dizer que vai ter multa.",
		"Manda o balancete, desculpa o incomodo.",
		"Isso quer dizer que o DAS venceu.",
		"Desculpa o atraso, mando hoje.",
	}
	for _, s := range intocadas {
		if got := applySelfCorrection(s).Text; got != s {
			t.Errorf("texto não corretivo foi alterado:\n  %q\n  -> %q", s, got)
		}
	}
}

// TestAutocorrecao_MarcadorIsoladoExigeOracaoAnterior: sem nada antes
// para corrigir, não há correção — só alguém começando a frase com
// "desculpa".
func TestAutocorrecao_MarcadorIsoladoExigeOracaoAnterior(t *testing.T) {
	for _, s := range []string{"Desculpa, mando hoje.", "Quer dizer, mando hoje."} {
		if got := applySelfCorrection(s).Text; got != s {
			t.Errorf("%q virou %q — não havia oração anterior para corrigir", s, got)
		}
	}
}

// TestMesclarCorrecao_PreservaOQueVeioAntes trava o segundo defeito que
// os marcadores novos expuseram.
//
// A correção substituía a ORAÇÃO INTEIRA, e isso só não aparecia porque
// o único marcador que disparava na prática vinha com uma oração de
// reposição completa. Perder o sujeito da frase é pior que não corrigir:
// a pessoa precisa reescrever o que o produto apagou.
func TestMesclarCorrecao_PreservaOQueVeioAntes(t *testing.T) {
	casos := []struct{ corrigida, correcao, quer string }{
		{"O vencimento é dia dez", "dia doze", "O vencimento é dia doze"},
		{"Sobe no Supabase", "no PostgreSQL local", "Sobe no PostgreSQL local"},
		// Até 28-set-2026 este caso esperava "São três mil e quinhentos" —
		// sem "reais". O teste existe para travar o SUJEITO preservado, e
		// afirmava a saída inteira; levou junto, por acidente, a perda da
		// unidade. Mauro confirmou que a intenção é "três mil e quinhentos
		// reais" (dataset corr-04). Ver devolverUnidade.
		{"São três mil reais", "três mil e quinhentos", "São três mil e quinhentos reais"},
		// Âncora na primeira palavra: substitui tudo, comportamento histórico.
		{"Manda para a Rosângela", "manda para o Thiago", "manda para o Thiago"},
		// Sem âncora: não há como saber onde encaixar; devolve a correção.
		{"O prazo venceu", "que vai ter multa", "que vai ter multa"},
	}
	for _, c := range casos {
		if got := mesclarCorrecao(c.corrigida, c.correcao); got != c.quer {
			t.Errorf("mesclarCorrecao(%q, %q) = %q, want %q", c.corrigida, c.correcao, got, c.quer)
		}
	}
}

// TestMesclarCorrecao_IgnoraCaixa: o ASR capitaliza o começo da frase e
// a correção vem no meio dela.
func TestMesclarCorrecao_IgnoraCaixa(t *testing.T) {
	if got := mesclarCorrecao("Manda para a Rosângela", "manda para o Thiago"); got != "manda para o Thiago" {
		t.Errorf("= %q", got)
	}
}
