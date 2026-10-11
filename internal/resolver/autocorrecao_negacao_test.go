package resolver

import "testing"

// Decisão do Mauro, 01-out-2026: quando a oração que o marcador corrigiria JÁ
// CONTÉM negação, o marcador não dispara e o texto sai como foi dito. Começou no
// "não"/"no" isolado, que na dupla negação pós-verbal do português ("Eu não sei,
// não, cara.") virava "cara." e apagava o que a pessoa disse, e passou a valer
// para TODOS os marcadores: na verdade, corrigindo, quero dizer, melhor, quer
// dizer, desculpa, desculpe, actually, i mean, rather.
//
// Custo aceito por ele: "Eu não vou na segunda, não, vou na terça" também sai
// como foi dito (nada se perde). Antes saía "Eu não vou na terça", com o sentido
// invertido, e os outros marcadores ("Eu não vou na segunda, na verdade vou na
// terça") faziam o mesmo, em silêncio.

// TestAutocorrecao_OracaoQueJaNegaNaoDispara: o texto sai intocado.
func TestAutocorrecao_OracaoQueJaNegaNaoDispara(t *testing.T) {
	casos := []struct{ nome, fala string }{
		// Os cinco do Mauro.
		{"dupla negação pós-verbal", "Eu não sei, não, cara."},
		{"dupla negação, tá?", "Eu não vou, não, tá?"},
		{"negação no começo da oração", "Não gostei, não, sério."},
		{"negação no fim da oração", "Claro que não, não, isso nunca."},
		{"negação no meio da oração", "Eu acho que não, não, deixa pra lá."},
		// O custo aceito: a correção cuja oração corrigida nega.
		{"custo aceito", "Eu não vou na segunda, não, vou na terça"},
		// Cada palavra da lista (negacoes) tem a sua linha, sem outra negação na
		// oração: tirar uma da lista faz o caso disparar.
		{"nunca", "Eu nunca vou na segunda, não, vou na terça."},
		{"jamais", "Eu jamais vou na segunda, não, vou na terça."},
		{"nem", "Nem eu vou na segunda, não, vou na terça."},
		{"tampouco", "Tampouco vou na segunda, não, vou na terça."},
		{"nada", "Nada disso vale hoje, não, vale amanhã."},
		{"ninguém", "Ninguém vai na segunda, não, vai na terça."},
		{"nenhum", "Nenhum deles vai na segunda, não, vai na terça."},
		{"nenhuma", "Nenhuma delas vai na segunda, não, vai na terça."},
		{"not", "I will not go Monday, no, I will go Tuesday."},
		{"never", "I never go Monday, no, I go Tuesday."},
		{"cannot", "I cannot go Monday, no, I go Tuesday."},
		{"nothing", "Nothing works Monday, no, it works Tuesday."},
		{"nobody", "Nobody goes Monday, no, somebody goes Tuesday."},
		{"none", "None of us goes Monday, no, all of us go Tuesday."},
		{"nor", "Nor will I go Monday, no, I will go Tuesday."},
		{"neither", "Neither of us goes Monday, no, both of us go Tuesday."},
		{"contração n't", "I don't go Monday, no, I go Tuesday."},
		{"contração n't, apóstrofo curvo", "I can’t go Monday, no, I go Tuesday."},
		// Caixa, acento e pontuação em volta não escondem a negação.
		{"maiúsculas", "EU NÃO VOU NA SEGUNDA, NÃO, VOU NA TERÇA."},
		{"sem acento", "Eu nao vou na segunda, nao, vou na terca."},
		{"ninguém sem acento", "Ninguem vai na segunda, nao, vai na terca."},
		{"entre aspas", `Eu disse "não" na segunda, não, na terça.`},
		{"entre aspas simples", "Eu disse 'não' na segunda, não, na terça."},
		{"entre parênteses", "Eu (não) vou na segunda, não, vou na terça."},
		// O corte é a oração (entre vírgulas), não a frase: a negação de uma
		// frase anterior da MESMA oração também bloqueia. Falha fechado; cortar
		// no ponto erraria para o lado aberto com "Sr. Silva", "3.5" e "12:30".
		{"negação em outra frase da mesma oração", "Ele não veio. O prazo é dia dez, não, dia doze."},
		{"resposta curta antes", "Não. Manda para a Rosângela, não, manda para o Thiago."},
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

// TestAutocorrecao_TodoMarcadorRespeitaANegacao: a mesma regra nos três tipos de
// marcador (isolado "não"/"no", de prefixo, "só isolado"), cada um com a sua
// linha. O texto sai intocado. A lista de negação é a mesma do teste de cima:
// aqui só se prova que nenhum tipo escapa dela.
func TestAutocorrecao_TodoMarcadorRespeitaANegacao(t *testing.T) {
	casos := []struct{ nome, fala string }{
		// Marcadores de prefixo, com a correção na mesma oração ou na seguinte.
		{"na verdade", "Eu não vou na segunda, na verdade vou na terça"},
		{"corrigindo", "Eu não vou na segunda, corrigindo, vou na terça"},
		{"quero dizer, com a correção na mesma oração", "Eu não vou na segunda, quero dizer vou na terça"},
		{"quero dizer, em oração própria", "Eu não vou na segunda, quero dizer, vou na terça"},
		{"melhor", "Eu não vou na segunda, melhor terça"},
		{"actually", "I don't want Monday, actually Tuesday"},
		{"i mean", "I don't want Monday, I mean Tuesday"},
		{"rather", "I don't want Monday, rather Tuesday"},
		// Marcadores que só valem isolados.
		{"quer dizer", "Eu não vou na segunda, quer dizer, vou na terça"},
		{"desculpa", "Eu não vou na segunda, desculpa, vou na terça"},
		{"desculpe", "Eu não vou na segunda, desculpe, vou na terça"},
		// O "não" solto que acompanha o marcador não esconde a oração que nega:
		// quem olha é a oração de antes dele. Antes saía "Eu não vou na segunda,
		// vou na terça", com a negação e o "na verdade" sumidos.
		{"não, na verdade", "Eu não vou na segunda, não, na verdade vou na terça"},
		{"não, quero dizer", "Eu não vou na segunda, não, quero dizer terça"},
		{"não, quero dizer, em oração própria", "Eu não vou na segunda, não, quero dizer, vou na terça"},
		{"no, actually", "I don't want Monday, no, actually Tuesday"},
		{"dois não, na verdade", "Eu não vou na segunda, não, não, na verdade vou na terça"},
		// Dois marcadores em fila, de qualquer tipo: o segundo não corrige o
		// primeiro, e o texto não perde os dois marcadores com a negação ficando.
		{"desculpa, quer dizer", "Eu não vou na segunda, desculpa, quer dizer, vou na terça"},
		{"quero dizer, na verdade", "Eu não vou na segunda, quero dizer, na verdade vou na terça"},
		{"quero dizer, melhor", "Eu não vou na segunda, quero dizer, melhor terça"},
		{"i mean, actually", "I don't want Monday, I mean, actually Tuesday"},
		// O par do caso "dois marcadores soltos no fim da oração" (continua
		// corrigindo, em SemNegacaoNaOracaoContinuaCorrigindo): os marcadores soltos
		// saem de cena, mas a negação que vem antes deles na oração continua valendo.
		{"dois marcadores soltos no fim, depois de uma negação", "Eu não vou. Não. Desculpa, na verdade vou na terça."},
		// Só o "não" do fim sai de cena, e não a oração toda: a negação que vem
		// antes dele, na mesma oração, continua valendo.
		{"não colado por reticência, depois de uma negação", "Eu não vou na segunda... não, na verdade vou na terça."},
		{"não colado por ponto, depois de uma negação", "Ele não veio. Não, na verdade ele veio."},
		// Caixa, acento e fim de frase valem como na regra do "não".
		{"maiúsculas", "EU NÃO VOU NA SEGUNDA, NA VERDADE VOU NA TERÇA."},
		{"sem acento", "Eu nao vou na segunda, na verdade vou na terca."},
		{"ninguém", "Ninguém vai na segunda, na verdade vai na terça."},
		{"contração n't, apóstrofo curvo", "I can’t go Monday, actually Tuesday."},
		// O corte é a oração (entre vírgulas), não a frase, para todo marcador.
		{"negação em outra frase da mesma oração", "Ele não veio. O prazo é dia dez, na verdade dia doze."},
		// O custo maior do corte na oração é o ditado longo, que o ASR pontua pouco:
		// uma negação sem relação com o trecho retratado, na frase de antes, também
		// bloqueia a correção legítima. Aceito pelo Mauro (falha fechado, sem inverter
		// o sentido); a saída é o texto como foi dito, e não a oração apagada.
		{"ditado longo, negação de outro assunto", "O cliente não pagou a fatura de março e o vencimento é dia dez, na verdade dia doze"},
		{"negação na frase de antes, desculpa", "Hoje não deu. Amanhã envio, desculpa, depois de amanhã."},
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

// TestAutocorrecao_SemNegacaoNaOracaoContinuaCorrigindo: a regra não pode
// desligar a autocorreção de verdade. A do dataset (corr-01 a corr-04) e a que
// tem uma palavra PARECIDA com negação na oração corrigida.
func TestAutocorrecao_SemNegacaoNaOracaoContinuaCorrigindo(t *testing.T) {
	casos := []struct{ nome, fala, quero string }{
		{"corr-01", "Manda para a Rosângela... não, na verdade manda para o Thiago.", "manda para o Thiago."},
		{"corr-02", "O vencimento é dia dez, desculpa, dia doze.", "O vencimento é dia doze."},
		{"corr-03", "Sobe no Supabase, quer dizer, no PostgreSQL local mesmo.", "Sobe no PostgreSQL local mesmo."},
		{"corr-04", "São 3 mil reais, não, 3 mil e quinhentos.", "São 3 mil e quinhentos reais."},
		{"não sozinho", "Manda para a Rosângela, não, manda para o Thiago.", "manda para o Thiago."},
		{"dois-pontos antes", "Atenção: o prazo é dia dez, não, dia doze.", "Atenção: o prazo é dia doze."},
		// "no" é a contração de "em + o": está na oração corrigida e não nega nada.
		{"no é contração", "Sobe no Supabase, não, no PostgreSQL.", "Sobe no PostgreSQL."},
		{"no em inglês", "I will go Monday, no, I mean Tuesday", "Tuesday"},
		// "sem" é preposição, não negação: está fora da lista de propósito.
		{"sem não é negação", "Manda sem o anexo, não, com o anexo.", "com o anexo."},
		// Palavra inteira: o que CONTÉM uma negação não é uma negação.
		{"cinema não tem nem", "Abre o cinema, não, o teatro.", "Abre o teatro."},
		{"nota não tem not", "Manda a nota fiscal, não, a nota de débito.", "Manda a nota de débito."},
		{"norte não tem nor", "Vai para o norte, não, para o sul.", "Vai para o sul."},
		{"nadador não tem nada", "Chama o nadador, não, o goleiro.", "Chama o goleiro."},
		// Só a oração ANTERIOR conta.
		{"negação numa oração mais atrás", "Eu não gosto, mas manda para a Rosângela, não, manda para o Thiago.",
			"Eu não gosto, mas manda para o Thiago."},
		{"negação só na oração de reposição", "Manda para a Rosângela, não, não manda para o Thiago.",
			"não manda para o Thiago."},
		// A oração de reposição de um marcador bloqueado não é "só um marcador":
		// o segundo marcador a corrige, e a negação de antes dela não conta.
		{"correção da reposição de um marcador bloqueado", "Eu não vou, na verdade vou na segunda, desculpe, vou na terça.",
			"Eu não vou, na verdade vou na terça."},
		// Os outros marcadores, com a oração corrigida SEM negação, seguem como
		// sempre (a decisão não desliga a autocorreção de verdade).
		{"quer dizer", "Sobe no Supabase, quer dizer, no PostgreSQL.", "Sobe no PostgreSQL."},
		{"desculpa", "O vencimento é dia dez, desculpa, dia doze.", "O vencimento é dia doze."},
		{"desculpe", "Eu vou na segunda, desculpe, vou na terça.", "Eu vou na terça."},
		{"não, na verdade", "Manda para a Rosângela, não, na verdade manda para o Thiago.", "manda para o Thiago."},
		{"na verdade", "Manda para a Rosângela, na verdade manda para o Thiago.", "manda para o Thiago."},
		{"corrigindo", "Eu vou na segunda, corrigindo, vou na terça.", "Eu vou na terça."},
		{"quero dizer", "Manda para a Rosângela, quero dizer, manda para o Thiago.", "manda para o Thiago."},
		{"melhor", "Eu vou na segunda, melhor terça.", "terça."},
		{"actually", "I will go Monday, actually Tuesday.", "Tuesday."},
		{"i mean", "I will go Monday, I mean Tuesday.", "Tuesday."},
		{"rather", "I will go Monday, rather Tuesday.", "Tuesday."},
		// O "não" que o ASR cola no fim da oração por uma reticência ou um ponto é
		// o marcador da retratação, não negação da oração (corr-01 é assim).
		{"não colado por ponto", "Manda para a Rosângela. Não, na verdade manda para o Thiago.", "manda para o Thiago."},
		// Dois marcadores soltos no fim da MESMA oração, em frases separadas: os
		// dois saem de cena (oracaoCorrigida tira o último marcador solto em laço, e
		// não só um), e a oração que o marcador retratava é "Manda para a Rosângela".
		// Com um "if" no lugar do laço o "Não" que sobra bloquearia a correção.
		{"dois marcadores soltos no fim da oração", "Manda para a Rosângela. Não. Desculpa, na verdade manda para o Thiago.",
			"manda para o Thiago."},
		// Um "não" sozinho no começo é só o marcador: não há oração para negar.
		{"não, na verdade, no começo", "Não, na verdade vou na terça", "vou na terça"},
		// Palavra inteira também nos outros marcadores.
		{"cinema, na verdade", "Abre o cinema, na verdade o teatro.", "Abre o teatro."},
		// Só a oração ANTERIOR conta, também nos outros marcadores.
		{"negação mais atrás, na verdade", "Eu não gosto, mas manda para a Rosângela, na verdade manda para o Thiago.",
			"Eu não gosto, mas manda para o Thiago."},
		{"negação só na reposição, na verdade", "Manda para a Rosângela, na verdade não manda para o Thiago.",
			"não manda para o Thiago."},
		// Marcador no começo do texto: sem oração anterior, segue como sempre.
		{"na verdade, no começo", "Na verdade, não vou na segunda, vou na terça", "não vou na segunda, vou na terça"},
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

// TestAutocorrecao_LimitacoesRegistradas fixa o que a regra NÃO cobre, para a
// spec (16-ai-behavior-spec.md, "Self-correction") dizer a verdade. Não é o
// comportamento desejado: se alguém consertar um destes, o teste quebra de
// propósito e a spec tem de ser atualizada junto. Decisão do Mauro (01-out-
// 2026): ficam registrados, sem regra nova.
//
// Os outros marcadores já não estão aqui: "Eu não vou na segunda, na verdade vou
// na terça" e as variantes com desculpa e quer dizer deixaram de inverter o
// sentido, e saem como foram ditas (TestAutocorrecao_TodoMarcadorRespeitaANegacao).
func TestAutocorrecao_LimitacoesRegistradas(t *testing.T) {
	casos := []struct{ nome, fala, hoje string }{
		{"vocativo entre os não", "Não, Mauro, não, isso não está certo.", "Não, isso não está certo."},
		{"pergunta e resposta", "Quer café, não, não quero", "não quero"},
		// O "não" colado no fim da oração por um ponto é lido como o marcador, e
		// não como a resposta: a pergunta some, como já sumia. Contá-lo como
		// negação quebraria corr-01, em que a reticência cola o "não" do mesmo jeito.
		{"resposta depois da pergunta", "Você vai amanhã? Não, na verdade vou na terça.", "vou na terça."},
		// O "não" não acha correção (a oração seguinte é só o "quero dizer") e o
		// "quero dizer" passa a corrigir o próprio "não": a oração retratada fica
		// ao lado da correção. É o comportamento de sempre.
		{"não e quero dizer em orações separadas", "Manda para a Rosângela, não, quero dizer, manda para o Thiago",
			"Manda para a Rosângela, manda para o Thiago"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := applySelfCorrection(c.fala).Text; got != c.hoje {
				t.Errorf("applySelfCorrection(%q) = %q, a limitação registrada é %q", c.fala, got, c.hoje)
			}
		})
	}
}

// TestSmart_NegacaoNaOracaoChegaInteira: os cinco do Mauro, e a decisão para os
// outros marcadores, pelo Smart inteiro, com o dicionário versionado. O Polish
// recebe este texto, então o modo com IA também deixa de perder o que foi dito.
// E as correções que não têm negação na oração, com "no" na anterior ou com os
// outros marcadores, continuam acontecendo.
func TestSmart_NegacaoNaOracaoChegaInteira(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, sementeVersionada(t))
	for _, fala := range []string{
		"Eu não sei, não, cara.",
		"Eu não vou, não, tá?",
		"Não gostei, não, sério.",
		"Claro que não, não, isso nunca.",
		"Eu acho que não, não, deixa pra lá.",
		"Eu não vou na segunda, não, vou na terça",
		"Eu não vou na segunda, na verdade vou na terça",
		"Eu não vou na segunda, não, na verdade vou na terça",
		"Eu não vou na segunda, desculpa, vou na terça",
		"Eu não vou na segunda, quer dizer, vou na terça",
		"Eu não vou na segunda, quero dizer, vou na terça",
		"Eu não vou na segunda, melhor terça",
		"I don't want Monday, actually Tuesday",
	} {
		if got := resolverTexto(t, sr, user, fala); got != fala {
			t.Errorf("Smart(%q) = %q, quero o texto como foi dito", fala, got)
		}
	}
	for _, c := range []struct{ fala, quero string }{
		{"Sobe no Supabase, não, no PostgreSQL.", "Sobe no PostgreSQL."},
		{"Sobe no Supabase, quer dizer, no PostgreSQL.", "Sobe no PostgreSQL."},
		{"O vencimento é dia dez, desculpa, dia doze.", "O vencimento é dia doze."},
		{"Manda para a Rosângela, não, na verdade manda para o Thiago.", "manda para o Thiago."},
	} {
		if got := resolverTexto(t, sr, user, c.fala); got != c.quero {
			t.Errorf("Smart(%q) = %q, quero %q", c.fala, got, c.quero)
		}
	}
}
