package resolver

import "testing"

// Teste do Mauro de 01-out-2026, comparando raw_text e final_text no
// histórico: ele ditou "Não, não, não, isso não está certo.", o ASR devolveu
// exatamente isso, e o Smart entregou "não, isso não está certo." O segundo
// "não" era lido como marcador de autocorreção, o primeiro como a oração
// errada e o terceiro como a correção. A negação enfática repetida é fala
// comum, e não correção nenhuma.

// TestAutocorrecao_NegacaoEnfaticaNaoDispara: um marcador isolado não dispara
// quando a oração que ele corrigiria, ou a de reposição, é ela mesma só um
// marcador. O texto sai como foi dito.
func TestAutocorrecao_NegacaoEnfaticaNaoDispara(t *testing.T) {
	casos := []struct{ nome, fala string }{
		{"o ditado do Mauro", "Não, não, não, isso não está certo."},
		{"dois não e o resto", "Não, não, isso não."},
		{"sem vírgula", "Não não não."},
		{"inglês", "No, no, no, that's wrong."},
		{"sem acento, misturado", "não, nao, não, tá errado"},
		{"só os marcadores", "Não, não."},
		{"ponto depois do terceiro", "Não, não, não. Isso não está certo."},
		// O ASR pontua a mesma fala de outro jeito: a oração anterior ao
		// segundo "não" passa a ser "Não. Não", e o que encosta no marcador é
		// o "Não" depois do ponto.
		{"ponto depois do primeiro", "Não. Não, não, isso não está certo."},
		{"resposta depois de pergunta", "Você vai amanhã? Não, não, eu fico."},
		// A reticência de UM caractere (U+2026) é fim de frase como os três
		// pontos: a mutação que a tirava de frasesDe sobrevivia.
		{"reticência de um caractere", "Não… não, não, isso não está certo."},
		// Ponto de exclamação entre os "não", antes e depois do marcador.
		{"exclamação antes do marcador", "Não! Não, não, isso não está certo."},
		{"exclamação depois do marcador", "Vou na terça, não, não! Vou na quarta."},
		// O ASR às vezes põe ponto e vírgula, dois-pontos ou travessão entre os
		// "não" no lugar do ponto.
		{"ponto e vírgula", "Não; não, não, isso não está certo."},
		{"travessão", "Não — não, não, isso não está certo."},
		{"meia-risca", "Não – não, não, isso não está certo."},
		{"dois-pontos", "Não: não, não, isso não está certo."},
		{"dois-pontos antes da reposição", "Ele disse: não, não, isso não está certo."},
		{"ponto e vírgula depois do marcador", "Vou na terça, não, não; vou na quarta."},
		// Fala entre aspas: a aspa encosta no primeiro "não".
		{"entre aspas", `"Não, não, isso não está certo", ele disse.`},
		// Reticências antes do terceiro "não": a oração de reposição começa
		// com um fim de frase, e o pedaço vazio antes dele não conta.
		{"reticências antes do marcador", "Isso está certo, não, ... não."},
		// A reposição seria só "não.": antes, "Isso está certo" virava "não.".
		{"reposição só com o marcador e o ponto", "Isso está certo, não, não."},
		{"reposição que começa com o marcador e o ponto", "Vou na terça, não, não. Vou na quarta."},
		// O preço da regra mínima: a correção com o "não" repetido também
		// não dispara. Falha FECHADO, sem perder palavra.
		{"correção com o não repetido fica como foi dita", "Manda para a Rosângela, não, não, manda para o Thiago."},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := applySelfCorrection(c.fala)
			if got.Triggered || got.Text != c.fala {
				t.Errorf("applySelfCorrection(%q) = %q (Triggered=%v), quero o texto intocado",
					c.fala, got.Text, got.Triggered)
			}
		})
	}
}

// TestAutocorrecao_RealContinuaComARegraDaEnfase: a regra da ênfase não pode
// desligar a autocorreção de verdade — a do dataset (corr-01 a corr-04, como
// o ASR as escreve) e a dos testes de sempre.
func TestAutocorrecao_RealContinuaComARegraDaEnfase(t *testing.T) {
	casos := []struct{ nome, fala, quero string }{
		{"não e na verdade", "Manda para a Rosângela, não, na verdade manda para o Thiago.", "manda para o Thiago."},
		{"corr-01, reticências", "Manda para a Rosângela... não, na verdade manda para o Thiago.", "manda para o Thiago."},
		{"não sozinho", "Manda para a Rosângela, não, manda para o Thiago.", "manda para o Thiago."},
		{"corr-02, desculpa", "O vencimento é dia dez, desculpa, dia doze.", "O vencimento é dia doze."},
		{"corr-03, quer dizer", "Sobe no Supabase, quer dizer, no PostgreSQL local mesmo.", "Sobe no PostgreSQL local mesmo."},
		{"corr-04, valor", "São 3 mil reais, não, 3 mil e quinhentos.", "São 3 mil e quinhentos reais."},
		{"prazo", "Prazo de 30 dias, não, de 45.", "Prazo de 45 dias."},
		{"não e quero dizer", "vou amanhã, não, quero dizer terça", "terça"},
		{"inglês", "I will go monday, no, I mean Tuesday", "Tuesday"},
		// Os separadores novos de frasesDe (; : — –) só valem para o pedaço que
		// encosta no marcador: a correção depois deles continua.
		{"dois-pontos antes da oração corrigida", "Atenção: o prazo é dia dez, não, dia doze.", "Atenção: o prazo é dia doze."},
		{"travessão antes da oração corrigida", "Olha — o prazo é dia dez, não, dia doze.", "Olha — o prazo é dia doze."},
		// A ênfase no começo fica, e a correção de depois acontece: a regra
		// pula só o marcador repetido, não desliga o resto da frase.
		{"ênfase antes de uma correção", "Não, não, não, manda para a Rosângela, não, manda para o Thiago.",
			"Não, não, não, manda para o Thiago."},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := applySelfCorrection(c.fala)
			if !got.Triggered || got.Text != c.quero {
				t.Errorf("applySelfCorrection(%q) = %q (Triggered=%v), quero %q",
					c.fala, got.Text, got.Triggered, c.quero)
			}
		})
	}
}

// TestSmart_NegacaoEnfaticaChegaInteira: o ditado do Mauro pelo Smart inteiro,
// com o dicionário versionado. O Polish recebe este texto, então o modo com IA
// também deixa de perder os dois "não".
func TestSmart_NegacaoEnfaticaChegaInteira(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, sementeVersionada(t))
	for _, fala := range []string{
		"Não, não, não, isso não está certo.",
		"Não, não, isso não.",
		"No, no, no, that's wrong.",
	} {
		if got := resolverTexto(t, sr, user, fala); got != fala {
			t.Errorf("Smart(%q) = %q, quero o texto como foi dito", fala, got)
		}
	}
}
