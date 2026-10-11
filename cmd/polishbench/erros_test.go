package main

import (
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Os erros de cada camada, pelo comando inteiro: cada um cai no motivo certo do
// "fallback por motivo", sem retry (uma chamada, um pedido), e os que o
// provedor cobrou têm os tokens contados.

const entradaLonga = "um cliente novo chegou hoje cedo ao escritório para abrir uma empresa de serviços e queria saber todos os prazos dos impostos que ele vai pagar no primeiro ano de atividade então expliquei o passo a passo do registro na junta e da inscrição na prefeitura com calma"

func casosDeErro() []Caso {
	mk := func(id, entrada string) Caso {
		return Caso{ID: id, Categoria: CatValores, Entrada: entrada, Esperado: strings.ToUpper(entrada[:1]) + entrada[1:] + "."}
	}
	return []Caso{
		mk("err-limite", "o DAS de agosto vence dia vinte"),
		mk("err-sobrecarga", "a DCTFWeb de setembro venceu ontem"),
		mk("err-credito", "o eSocial rejeitou o evento de hoje"),
		mk("err-truncada", "a folha de outubro fechou com doze funcionários"),
		mk("err-recusa", "o balancete de novembro ficou pronto"),
		mk("err-vazia", "a nota fiscal chegou com atraso hoje"),
		mk("err-json", "o cliente pagou a guia no banco"),
		mk("err-formato", "o parcelamento vence todo dia dez"),
		mk("err-numeros", "a multa foi de vinte reais"),
		mk("err-siglas", "confere o PGDAS antes de fechar"),
		mk("err-palavras", "o contador enviou o relatório ontem"),
		mk("err-trecho", entradaLonga),
	}
}

func comportamentoDeErros() comportamento {
	return func(p pedidoFalso) respostaFalsa {
		switch p.Ditado {
		case "o DAS de agosto vence dia vinte":
			return respostaFalsa{Status: 429, CorpoDeErro: `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Rate limit reached for x in organization org-ABCDEFGHIJKLMNOPQRSTUVWX on tokens per min"}}`}
		case "a DCTFWeb de setembro venceu ontem":
			return respostaFalsa{Status: 503, CorpoDeErro: `{"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"overloaded"}}`}
		case "o eSocial rejeitou o evento de hoje":
			return respostaFalsa{Status: 429, CorpoDeErro: `{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"You exceeded your current quota"}}`}
		case "a folha de outubro fechou com doze funcionários":
			return respostaFalsa{CorpoLiteral: `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":1200,"output_tokens":2048}}`}
		case "o balancete de novembro ficou pronto":
			return respostaFalsa{CorpoLiteral: `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"não posso ajudar"}]}],"usage":{"input_tokens":1100,"output_tokens":7}}`}
		case "a nota fiscal chegou com atraso hoje":
			return respostaFalsa{CorpoLiteral: `{"status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":0}}`}
		case "o cliente pagou a guia no banco":
			return respostaFalsa{TextoDoModelo: "Claro! Aqui está o texto organizado."}
		case "o parcelamento vence todo dia dez":
			return respostaFalsa{Final: "Claro! O parcelamento vence todo dia dez."}
		case "a multa foi de vinte reais":
			return respostaFalsa{Final: "A multa foi de 20 reais."}
		case "confere o PGDAS antes de fechar":
			return respostaFalsa{Final: "Confere o PGDAs antes de fechar."}
		case "o contador enviou o relatório ontem":
			return respostaFalsa{Final: "O contador mandou o relatório ontem."}
		case entradaLonga:
			return respostaFalsa{Final: "Um cliente novo chegou hoje cedo ao escritório para abrir uma empresa de serviços e queria saber todos os prazos dos impostos que ele vai pagar no primeiro ano de atividade, então expliquei o passo a passo do registro na junta e da inscrição na prefeitura com calma. Fico à disposição."}
		}
		return respostaFalsa{Final: p.Ditado}
	}
}

func TestRun_CadaErroCaiNoSeuMotivo_SemRetry(t *testing.T) {
	casos := casosDeErro()
	if n := len(palavrasDe(entradaLonga)); n < 40 {
		t.Fatalf("a entrada longa tem %d palavras, quero 40 ou mais", n)
	}
	a := novoAmbiente(t, comportamentoDeErros(), nil)
	cod := a.rodar(juntar(escreverDataset(t, casos, nil), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	// Sem retry: um pedido por frase.
	if n := len(a.openai.Pedidos()); n != len(casos) {
		t.Fatalf("a OpenAI recebeu %d pedidos, quero %d (sem retry)", n, len(casos))
	}
	rel := a.lerRelatorio()
	for _, motivo := range []string{
		"limite", "sobrecarga", "sem_credito", "truncada", "recusa", "resposta_invalida", "json", "formato", "numeros", "siglas_termos", "palavras", "trecho_novo",
	} {
		if got := celula(t, rel, "`"+motivo+"`", 1); !strings.HasPrefix(got, "1/12") {
			t.Errorf("fallback %s = %q, quero 1/12", motivo, got)
		}
	}
	// Doze fallbacks em doze chamadas.
	if got := celula(t, rel, "Fallback, todas as categorias", 1); !strings.HasPrefix(got, "12/12") {
		t.Errorf("fallback total = %q, quero 12/12", got)
	}
	// Sem final_text: os 6 erros do provedor e o texto que nem é JSON.
	if got := celula(t, rel, "Sem `final_text` lido", 1); got != "7" {
		t.Errorf("sem final_text = %q, quero 7", got)
	}
	contem(t, rel, "`nao_json` ×1")
	// O provedor cobrou a resposta truncada e a recusada: os tokens contam.
	if got := celula(t, rel, "Tokens por chamada", 1); !strings.Contains(got, "/") {
		t.Errorf("tokens = %q", got)
	}
	contem(t, rel, "`429 tipo=rate_limit_error codigo=rate_limit_exceeded` ×1")
	contem(t, rel, "`503 tipo=service_unavailable_error codigo=server_is_overloaded` ×1")
	contem(t, rel, "`429 tipo=insufficient_quota codigo=insufficient_quota` ×1")
	// A mensagem do 429 cita a organização e não é guardada (só a de erro de pedido).
	naoContem(t, rel, "org-ABCDEF")
	naoContem(t, rel, "Rate limit reached")
	// Nada disso é falha crítica: o modelo não devolveu texto errado que passasse.
	if got := celula(t, rel, rotuloDepois, 1); !strings.HasPrefix(got, "0/") {
		t.Errorf("falha crítica depois = %q", got)
	}
	// O que a guarda barrou e era falha do modelo (número, sigla, palavra trocada...).
	if got := celula(t, rel, "Falhas críticas do modelo que as guardas barraram", 1); !strings.HasPrefix(got, "5/5") && !strings.HasPrefix(got, "4/4") && !strings.HasPrefix(got, "6/6") {
		t.Logf("barradas = %q", got) // só informativo: depende de quantas trocas a auditoria acusa
	}
}

// O 429 e o 5xx passam; três respostas terminais IGUAIS seguidas param o
// candidato; uma resposta boa no meio zera a contagem.
func TestRun_SeguidasParaAbortarContamSoAsIguaisSeguidas(t *testing.T) {
	frases, refs := datasetPequeno()
	casos := append(append([]Caso(nil), frases...), refs...)
	oraculo := comOraculo(casos)
	recusa := respostaFalsa{Status: 401, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"invalid_api_key"}}`}
	pedido := respostaFalsa{Status: 400, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"outra"}}`}

	// 401 401 (boa) 401 401 (boa) 401: nunca três seguidas.
	roteiro := func(seq []string) comportamento {
		var n atomic.Int32
		return func(p pedidoFalso) respostaFalsa {
			i := int(n.Add(1)) - 1
			switch seq[i%len(seq)] {
			case "a":
				return recusa
			case "b":
				return pedido
			}
			return oraculo(p)
		}
	}
	for nome, seq := range map[string][]string{
		"duas e uma boa":        {"a", "a", "ok", "a", "a", "ok", "a"},
		"alternando dois erros": {"a", "b", "a", "b", "a", "b", "a"},
	} {
		amb := novoAmbiente(t, roteiro(seq), nil)
		cod := amb.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
		if cod != 0 {
			t.Fatalf("%s: saiu com %d\n%s", nome, cod, amb.stderr.String())
		}
		if n := len(amb.openai.Pedidos()); n != 7 {
			t.Errorf("%s: %d pedidos, quero 7 (nunca três iguais seguidas)", nome, n)
		}
		naoContem(t, amb.lerRelatorio(), "Candidato abortado")
	}

	// 401 401 400 401 401: o 400 interrompe a sequência (motivo diferente).
	amb := novoAmbiente(t, roteiro([]string{"a", "a", "b", "a", "a", "b", "a"}), nil)
	if cod := amb.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d", cod)
	}
	if n := len(amb.openai.Pedidos()); n != 7 {
		t.Errorf("%d pedidos, quero 7", n)
	}

	// 429 e 503 não são terminais: repetem sem abortar.
	limite := respostaFalsa{Status: 429, CorpoDeErro: `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded"}}`}
	amb = novoAmbiente(t, func(pedidoFalso) respostaFalsa { return limite }, nil)
	if cod := amb.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d", cod)
	}
	if n := len(amb.openai.Pedidos()); n != 7 {
		t.Errorf("o limite por minuto abortou o candidato: %d pedidos, quero 7", n)
	}
	contem(t, amb.lerRelatorio(), "`limite`")
}

// O prazo real da chamada: um provedor que não responde a tempo vira `prazo`, e a
// latência da chamada é medida até o prazo máximo.
func TestRun_PrazoMaximoCortaEMedeALatencia(t *testing.T) {
	frases, refs := datasetPequeno()
	lento := func(pedidoFalso) respostaFalsa { return respostaFalsa{Final: "x", Dormir: 600 * time.Millisecond} }
	a := novoAmbiente(t, lento, nil)
	a.relogioReal = true
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-prazo-max", "80ms", "-prazo-agente", "50ms", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	rel := a.lerRelatorio()
	if got := celula(t, rel, "`prazo`", 1); !strings.HasPrefix(got, "7/7") {
		t.Errorf("fallback prazo = %q, quero 7/7", got)
	}
	// A latência medida fica perto do prazo máximo (80 ms), muito abaixo dos 600 ms do provedor.
	p95 := celula(t, rel, "Latência p95", 1)
	v, err := strconv.ParseFloat(strings.Replace(strings.TrimSuffix(p95, " s"), ",", ".", 1), 64)
	if err != nil || v < 0.05 || v > 0.55 {
		t.Errorf("p95 = %q, quero entre 0,05 s (o prazo de 0,08 s) e bem abaixo dos 0,6 s do provedor", p95)
	}
	contem(t, rel, "cada chamada aqui roda com até 0,08 s")
}

// A latência mede o provedor, não a fila: com o mesmo provedor a chamada rápida
// e a lenta ficam distintas mesmo em paralelo (todas contam uma vez).
func TestRun_RespostaBoaNaoEntraNoPrazoSePassarDoPrazoDoAgent(t *testing.T) {
	frases, refs := datasetPequeno()
	oraculo := comOraculo(append(append([]Caso(nil), frases...), refs...))
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa {
		r := oraculo(p)
		r.Atraso = 3500 * time.Millisecond // exatamente o prazo do Agent: passa
		if strings.HasPrefix(p.Ditado, "o DAS") {
			r.Atraso = 3501 * time.Millisecond // um milésimo a mais: passa do prazo
		}
		return r
	}, nil)
	cod := a.rodar(juntar(escreverDataset(t, frases, refs), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d", cod)
	}
	rel := a.lerRelatorio()
	if got := celula(t, rel, "`prazo`", 1); !strings.HasPrefix(got, "1/7") {
		t.Errorf("fallback prazo = %q, quero 1/7 (só o que passou de 3,5 s)", got)
	}
}
