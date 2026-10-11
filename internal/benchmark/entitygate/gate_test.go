package entitygate

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
)

// TestMedir_ClassificaOsQuatroDesfechos é o coração deste pacote: os
// quatro destinos possíveis de uma entidade precisam ser distinguíveis,
// porque três deles pedem ações OPOSTAS.
//
// "continua_errada" é trabalho que falta. "regrediu" é dano causado pelo
// próprio dicionário. Um agregado que some os dois esconde exatamente a
// informação que decide se o dicionário pode ser liberado.
func TestMedir_ClassificaOsQuatroDesfechos(t *testing.T) {
	r := Medir([]Amostra{
		{
			ID: "ja-certa", Entidades: []string{"DCTFWeb"},
			Bruto: "transmitir a DCTFWeb hoje", Resolvido: "transmitir a DCTFWeb hoje",
		},
		{
			ID: "corrigida", Entidades: []string{"Supabase"},
			Bruto: "subir no Supabaese", Resolvido: "subir no Supabase",
		},
		{
			ID: "continua-errada", Entidades: []string{"PostgreSQL"},
			Bruto: "seu post de grias", Resolvido: "seu post de grias",
		},
		{
			ID: "regrediu", Entidades: []string{"DRE"},
			Bruto: "manda a DRE do mes", Resolvido: "manda a DAS do mes",
		},
	})

	querido := map[string]StatusEntidade{
		"ja-certa":        JaEstavaCerta,
		"corrigida":       Corrigida,
		"continua-errada": ContinuaErrada,
		"regrediu":        Regrediu,
	}
	if len(r.Ocorrencias) != len(querido) {
		t.Fatalf("len(Ocorrencias) = %d, want %d", len(r.Ocorrencias), len(querido))
	}
	for _, oc := range r.Ocorrencias {
		if got, want := oc.Status, querido[oc.AmostraID]; got != want {
			t.Errorf("amostra %s: status = %v, want %v", oc.AmostraID, got, want)
		}
	}
	if r.Ganhos != 1 {
		t.Errorf("Ganhos = %d, want 1", r.Ganhos)
	}
	if r.Regressoes != 1 {
		t.Errorf("Regressoes = %d, want 1", r.Regressoes)
	}
}

// TestMedir_AgregadoBateComAsOcorrencias trava a sincronia entre as duas
// contagens que o relatório mostra lado a lado.
//
// Se MatchResult.Total e len(Ocorrencias) divergirem, o relatório
// apresenta dois totais diferentes para a mesma coisa e quem lê não tem
// como saber qual acreditar.
func TestMedir_AgregadoBateComAsOcorrencias(t *testing.T) {
	r := Medir([]Amostra{
		{ID: "a", Entidades: []string{"DAS", "MEI"}, Bruto: "o DAS e o MEI", Resolvido: "o DAS e o MEI"},
		{ID: "b", Entidades: []string{"SPED"}, Bruto: "o ESPEDI foi", Resolvido: "o SPED foi"},
		{ID: "c", Entidades: []string{"", "  "}, Bruto: "nada", Resolvido: "nada"},
	})

	if r.Bruto.Total != len(r.Ocorrencias) {
		t.Errorf("Bruto.Total = %d mas há %d ocorrências — os dois contam a mesma coisa",
			r.Bruto.Total, len(r.Ocorrencias))
	}
	if r.Resolvido.Total != len(r.Ocorrencias) {
		t.Errorf("Resolvido.Total = %d mas há %d ocorrências",
			r.Resolvido.Total, len(r.Ocorrencias))
	}
}

// TestMedir_EntidadeVaziaEhIgnoradaDosDoisLados: metrics.EntityAccuracy
// ignora entidade vazia. Se este pacote NÃO ignorasse, cada string vazia
// viraria uma ocorrência "continua_errada" fantasma.
func TestMedir_EntidadeVaziaEhIgnoradaDosDoisLados(t *testing.T) {
	r := Medir([]Amostra{
		{ID: "a", Entidades: []string{"", "DAS", "   "}, Bruto: "o DAS", Resolvido: "o DAS"},
	})
	if len(r.Ocorrencias) != 1 {
		t.Fatalf("len(Ocorrencias) = %d, want 1 (as vazias não contam)", len(r.Ocorrencias))
	}
	if r.Bruto.Total != 1 {
		t.Errorf("Bruto.Total = %d, want 1", r.Bruto.Total)
	}
}

// TestTaxaCorrecaoFalsa_DenominadorEhOQueEstavaCerto.
//
// O risco que o portão mede é "o resolver estraga o que já funcionava".
// Usar o TOTAL como denominador diluiria a taxa com entidades que nunca
// estiveram certas e que, por definição, não podem regredir.
func TestTaxaCorrecaoFalsa_DenominadorEhOQueEstavaCerto(t *testing.T) {
	// 4 certas antes; 1 regride. Taxa = 1/4 = 0,25.
	// Se o denominador fosse o total (5), daria 0,20 — mais brando.
	r := Resultado{
		Bruto:      matchResultDe(4, 5),
		Regressoes: 1,
	}
	if got := r.TaxaCorrecaoFalsa(); got != 0.25 {
		t.Errorf("TaxaCorrecaoFalsa = %v, want 0.25 (1 de 4 que estavam certas)", got)
	}
}

// TestTaxaCorrecaoFalsa_SemCertasAntesNaoInventaTaxa: dividir por zero
// ou devolver 1.0 ("100% de correção falsa") seria pior que dizer nada.
func TestTaxaCorrecaoFalsa_SemCertasAntesNaoInventaTaxa(t *testing.T) {
	r := Resultado{Bruto: matchResultDe(0, 5), Regressoes: 0}
	if got := r.TaxaCorrecaoFalsa(); got != 0 {
		t.Errorf("TaxaCorrecaoFalsa sem base = %v, want 0", got)
	}
}

// TestPassouNoPortao_ExigeOsDois é a trava mais importante do pacote.
//
// Um dicionário agressivo o bastante para chegar a 100% de Entity
// Accuracy quase certamente estraga texto que já estava certo. Se o
// portão olhasse só a acurácia, esse dicionário passaria — e o dano
// apareceria em uso real, em texto que a pessoa não estava conferindo.
func TestPassouNoPortao_ExigeOsDois(t *testing.T) {
	casos := []struct {
		nome         string
		resolvido    int
		total        int
		certasBefore int
		regressoes   int
		querPassar   bool
	}{
		{"bate os dois", 47, 49, 30, 0, true},
		{"acurácia no limite exato", 47, 49, 47, 0, true},
		{"acurácia alta, correção falsa alta", 49, 49, 30, 5, false},
		{"acurácia baixa, sem regressão", 40, 49, 40, 0, false},
		{"os dois ruins", 30, 49, 30, 9, false},
	}
	for _, c := range casos {
		r := Resultado{
			Bruto:      matchResultDe(c.certasBefore, c.total),
			Resolvido:  matchResultDe(c.resolvido, c.total),
			Regressoes: c.regressoes,
		}
		if got := r.PassouNoPortao(); got != c.querPassar {
			t.Errorf("%s: PassouNoPortao = %v, want %v (acurácia %.3f, correção falsa %.3f)",
				c.nome, got, c.querPassar, r.Resolvido.Score(), r.TaxaCorrecaoFalsa())
		}
	}
}

// TestMedir_ResolverQueNaoMexeNaoGeraGanhoNemRegressao: o caso de
// controle. Se o resolver não estiver ligado (ou não achar nada), os
// dois números têm de ser idênticos — qualquer diferença aqui seria a
// própria medição mentindo.
func TestMedir_ResolverQueNaoMexeNaoGeraGanhoNemRegressao(t *testing.T) {
	const texto = "precisa transmitir a DCTF Web antes do dia quinze"
	r := Medir([]Amostra{
		{ID: "a", Entidades: []string{"DCTFWeb"}, Bruto: texto, Resolvido: texto},
	})
	if r.Ganhos != 0 || r.Regressoes != 0 {
		t.Errorf("resolver inerte gerou Ganhos=%d Regressoes=%d, want 0 e 0", r.Ganhos, r.Regressoes)
	}
	if r.Bruto != r.Resolvido {
		t.Errorf("resolver inerte deu números diferentes: bruto=%+v resolvido=%+v", r.Bruto, r.Resolvido)
	}
}

// TestMedir_SensivelAAcentoEDigito documenta, aqui, a semântica herdada
// de metrics.EntityAccuracy — porque é ela que define o que o dicionário
// precisa produzir, e é contraintuitiva: maiúscula NÃO importa, acento e
// dígito importam.
func TestMedir_SensivelAAcentoEDigito(t *testing.T) {
	r := Medir([]Amostra{
		{ID: "acento", Entidades: []string{"Rosângela"}, Bruto: "a Rosangela pediu", Resolvido: "a Rosângela pediu"},
		{ID: "caixa", Entidades: []string{"eSocial"}, Bruto: "o ESocial foi", Resolvido: "o ESocial foi"},
		{ID: "digito", Entidades: []string{"n8n"}, Bruto: "o n 8 n caiu", Resolvido: "o n8n caiu"},
	})
	porID := map[string]StatusEntidade{}
	for _, oc := range r.Ocorrencias {
		porID[oc.AmostraID] = oc.Status
	}
	if porID["acento"] != Corrigida {
		t.Errorf("acento: %v, want Corrigida — acento conta", porID["acento"])
	}
	if porID["caixa"] != JaEstavaCerta {
		t.Errorf("caixa: %v, want JaEstavaCerta — maiúscula NÃO conta", porID["caixa"])
	}
	if porID["digito"] != Corrigida {
		t.Errorf("dígito: %v, want Corrigida — 'n 8 n' são três tokens, 'n8n' é um", porID["digito"])
	}
}

func TestStatusEntidade_String(t *testing.T) {
	for _, s := range []StatusEntidade{JaEstavaCerta, Corrigida, ContinuaErrada, Regrediu} {
		if s.String() == "" || s.String() == "desconhecido" {
			t.Errorf("StatusEntidade(%d).String() = %q", int(s), s.String())
		}
	}
	if got := StatusEntidade(99).String(); got != "desconhecido" {
		t.Errorf("status fora da faixa = %q, want \"desconhecido\"", got)
	}
}

// matchResultDe monta um metrics.MatchResult para os testes de portão,
// que exercitam a aritmética de PassouNoPortao/TaxaCorrecaoFalsa sem
// precisar de texto real.
func matchResultDe(certas, total int) metrics.MatchResult {
	return metrics.MatchResult{Correct: certas, Total: total}
}

// TestMedir_NaoEnxergaPontuacaoApagada registra um ponto CEGO do portão,
// e existe para que ninguém confie nele onde ele não vê.
//
// Em 28-set-2026 o SmartResolver foi flagrado apagando pontuação no meio
// de um termo canônico de duas palavras:
//
//	entra: "O Claude, Code e o Aurora resolvem coisas diferentes."
//	sai:   "O Claude Code e o Aurora resolvem coisas diferentes."
//
// Uma lista de TRÊS itens virou uma de dois — correção falsa, a categoria
// que este pacote existe para vigiar. A explicação natural era "falta uma
// amostra com esse formato no dataset". Está errada: nem com a amostra
// perfeita o portão acusaria.
//
// O motivo é metrics.normalizeTokens, que parte por espaço e apara
// pontuação de BORDA antes de contar entidade E antes de calcular
// WER/CER. "claude," e "claude" viram o mesmo token, então os dois textos
// acima são INDISTINGUÍVEIS para todos os números que Medir produz: zero
// regressão, zero correção falsa, WER zero, portão passa.
//
// A cegueira é ESTRUTURAL, não amostral. Gravar áudio novo não a
// remove — só daria falsa segurança. Quem protege contra este defeito é
// internal/resolver (ver TestSmart_PontuacaoDentroDoSpanNaoEhEngolida),
// onde a verificação é exata e roda em `go test ./...`.
//
// Endurecer normalizeTokens para enxergar pontuação NÃO é a correção
// óbvia: a autocorreção de fala remove vírgula legitimamente ("Sobe no
// Supabase, quer dizer, no Postgre local mesmo." -> "Sobe no PostgreSQL
// local mesmo."), e um portão que reclamasse disso reprovaria
// comportamento correto. Se este teste quebrar, é porque alguém mudou a
// normalização — e a pergunta a fazer antes de "consertar" o teste é o
// que acontece com a autocorreção.
func TestMedir_NaoEnxergaPontuacaoApagada(t *testing.T) {
	const intacto = "O Claude, Code e o Aurora resolvem coisas diferentes."
	const estragado = "O Claude Code e o Aurora resolvem coisas diferentes."

	r := Medir([]Amostra{{
		ID: "armadilha", Entidades: []string{"Claude Code"},
		Bruto: intacto, Resolvido: estragado, Referencia: intacto,
	}})

	if r.Regressoes != 0 || r.TaxaCorrecaoFalsa() != 0 || r.QualidadeResolvida.WER != 0 {
		t.Fatalf("o portão passou a ENXERGAR pontuação apagada "+
			"(regressões=%d, correção falsa=%.4f, WER=%.3f).\n"+
			"Isso é uma mudança de comportamento, não uma falha: confira o que "+
			"acontece com a autocorreção de fala, que remove vírgula de propósito.",
			r.Regressoes, r.TaxaCorrecaoFalsa(), r.QualidadeResolvida.WER)
	}
}

// TestMedir_ListaOndeOResolverPiorouOTexto: a correção falsa só enxerga
// palavras marcadas como entidade. Uma palavra comum trocada pelo resolver
// não move aquele número — mas move o WER, e o WER não entra no portão.
//
// PiorouTexto é o que torna isso visível. Em 28-set-2026 ele explicou,
// na primeira execução, um CER que subia com o portão marcando PASSOU:
// as três amostras eram de autocorreção de fala, funcionando como
// projetado contra uma referência literal.
func TestMedir_ListaOndeOResolverPiorouOTexto(t *testing.T) {
	r := Medir([]Amostra{
		{
			// O resolver trocou uma palavra que não é entidade: a correção
			// falsa não vê, o WER vê.
			ID: "piorou", Entidades: []string{"DCTFWeb"},
			Referencia: "manda a DCTFWeb hoje",
			Bruto:      "manda a DCTFWeb hoje",
			Resolvido:  "manda a DCTFWeb ontem",
		},
		{
			ID: "melhorou", Entidades: []string{"PostgreSQL"},
			Referencia: "confere o PostgreSQL",
			Bruto:      "confere o postgres",
			Resolvido:  "confere o PostgreSQL",
		},
		{
			ID: "igual", Entidades: []string{"eSocial"},
			Referencia: "envia o eSocial",
			Bruto:      "envia o eSocial",
			Resolvido:  "envia o eSocial",
		},
	})

	if r.TaxaCorrecaoFalsa() != 0 {
		t.Fatalf("pré-condição: a troca de palavra comum não devia contar como correção falsa, deu %.3f",
			r.TaxaCorrecaoFalsa())
	}
	if len(r.PiorouTexto) != 1 || r.PiorouTexto[0].ID != "piorou" {
		t.Fatalf("PiorouTexto = %+v, quero só a amostra \"piorou\"", r.PiorouTexto)
	}
	if p := r.PiorouTexto[0]; p.WERResolvido <= p.WERBruto {
		t.Errorf("WER %.3f -> %.3f não é piora", p.WERBruto, p.WERResolvido)
	}
}

// TestPortao_ReprovaPalavraComumTrocada é o motivo do terceiro critério.
//
// Entity Accuracy perfeita, correção falsa zero — e o resolver trocou
// "hoje" por "ontem". Até 28-set-2026 o portão aprovava isto.
func TestPortao_ReprovaPalavraComumTrocada(t *testing.T) {
	r := Medir([]Amostra{{
		ID: "troca", Entidades: []string{"DCTFWeb"},
		Referencia: "manda a DCTFWeb hoje",
		Bruto:      "manda a DCTFWeb hoje",
		Resolvido:  "manda a DCTFWeb ontem",
	}})
	if r.Resolvido.Score() < MetaEntityAccuracy || r.TaxaCorrecaoFalsa() > MetaCorrecaoFalsa {
		t.Fatal("pré-condição: os dois critérios antigos deviam passar")
	}
	if r.PassouNoPortao() {
		t.Error("o portão aprovou um resolver que trocou uma palavra comum")
	}
}

// TestPortao_AutocorrecaoCertaNaoEhPiora: contra a fala literal, a
// autocorreção que ACERTA parecia piora. Contra a intenção, não.
func TestPortao_AutocorrecaoCertaNaoEhPiora(t *testing.T) {
	a := Amostra{
		ID: "corr", Entidades: []string{"Thiago"},
		Referencia: "manda para a Rosângela não na verdade manda para o Thiago",
		Bruto:      "manda para a Rosângela não na verdade manda para o Thiago",
		Resolvido:  "manda para o Thiago",
	}

	semIntencao := Medir([]Amostra{a})
	if len(semIntencao.PiorouTexto) != 1 {
		t.Fatalf("pré-condição: sem intenção, a autocorreção devia aparecer como piora (%d)",
			len(semIntencao.PiorouTexto))
	}

	a.Intencao = "manda para o Thiago"
	comIntencao := Medir([]Amostra{a})
	if len(comIntencao.PiorouTexto) != 0 {
		t.Errorf("com a intenção, a autocorreção certa ainda aparece como piora: %+v", comIntencao.PiorouTexto)
	}
	if !comIntencao.PassouNoPortao() {
		t.Error("o portão reprovou uma autocorreção correta")
	}
	if comIntencao.DistanciaDaIntencao.WER != 0 {
		t.Errorf("distância até a intenção = %.3f, want 0", comIntencao.DistanciaDaIntencao.WER)
	}
}
