package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// ---------------------------------------------------------------------
// O dataset é agrupado por categoria: oito injeções seguidas, oito perguntas
// seguidas. Três recusas seguidas encerravam o candidato inteiro, então um
// provedor que recusasse (400) um TIPO de frase, como as injeções, abortava o
// candidato por conteúdo concentrado e não por erro de protocolo (revisão da S3).
// Agora a sequência de erros terminais iguais tem de atravessar duas categorias.
// ---------------------------------------------------------------------

// casosPorCategoria monta um dataset em que cada par (categoria, quantidade)
// vira frases seguidas, com o esperado igual à entrada com ponto final.
func casosPorCategoria(grupos ...any) []Caso {
	var casos []Caso
	for i := 0; i < len(grupos); i += 2 {
		cat, n := grupos[i].(string), grupos[i+1].(int)
		for j := 1; j <= n; j++ {
			entrada := fmt.Sprintf("frase número %d da categoria %s para o teste", j, strings.ReplaceAll(cat, "_", " "))
			casos = append(casos, Caso{ID: fmt.Sprintf("%s-%02d", cat, j), Categoria: cat, Entrada: entrada, Esperado: strings.ToUpper(entrada[:1]) + entrada[1:] + "."})
		}
	}
	return casos
}

var recusa400 = respostaFalsa{Status: 400, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"content_filter"}}`}

func rodarRecusas(t *testing.T, casos []Caso, recusa func(c Caso) bool) (*ambiente, int) {
	t.Helper()
	oraculo := comOraculo(casos)
	porEntrada := map[string]Caso{}
	for _, c := range casos {
		porEntrada[c.Entrada] = c
	}
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa {
		if recusa(porEntrada[p.Ditado]) {
			return recusa400
		}
		return oraculo(p)
	}, nil)
	ref := Caso{ID: prefixoJaOrganiza + "geral-01", Categoria: CatJaOrganizada, Entrada: "Depois do almoço eu volto e a gente termina isso.", Esperado: "Depois do almoço eu volto e a gente termina isso."}
	cod := a.rodar(juntar(escreverDataset(t, casos, []Caso{ref}), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...)
	if cod != 0 {
		t.Fatalf("saiu com %d\n%s\n%s", cod, a.stdout.String(), a.stderr.String())
	}
	return a, len(a.openai.Pedidos())
}

// O provedor recusa só as injeções, todas seguidas: o candidato NÃO aborta, e o
// resto do dataset é medido.
func TestRun_RecusaConcentradaEmUmaCategoriaNaoAbortaOCandidato(t *testing.T) {
	casos := casosPorCategoria(CatInjecao, 8, CatValores, 4)
	a, pedidos := rodarRecusas(t, casos, func(c Caso) bool { return c.Categoria == CatInjecao })
	if pedidos != 13 { // 8 + 4 + a frase já organizada
		t.Errorf("%d pedidos, quero 13 (nada abortou)", pedidos)
	}
	rel := a.lerRelatorio()
	naoContem(t, rel, "Candidato abortado")
	contem(t, rel, "`pedido_recusado`")
	if got := celula(t, rel, "`pedido_recusado`", 1); !strings.HasPrefix(got, "8/13") {
		t.Errorf("fallback pedido_recusado = %q, quero 8/13", got)
	}
}

// A recusa que atravessa duas categorias aborta no primeiro pedido da segunda,
// se já há três seguidas.
func TestRun_RecusaQueAtravessaCategoriasAbortaNoPrimeiroDaSegunda(t *testing.T) {
	// Todas recusadas; três injeções e depois valores: as três da primeira
	// categoria não bastam, a quarta chamada (da segunda) aborta.
	casos := casosPorCategoria(CatInjecao, 3, CatValores, 4)
	a, pedidos := rodarRecusas(t, casos, func(Caso) bool { return true })
	if pedidos != 4 {
		t.Errorf("%d pedidos, quero 4 (3 da primeira categoria e 1 da segunda)", pedidos)
	}
	contem(t, a.lerRelatorio(), "**Candidato abortado:** pedido_recusado.")

	// Duas na primeira categoria e a terceira já na segunda: aborta no terceiro.
	casos = casosPorCategoria(CatInjecao, 2, CatValores, 4)
	a, pedidos = rodarRecusas(t, casos, func(Caso) bool { return true })
	if pedidos != 3 {
		t.Errorf("%d pedidos, quero 3", pedidos)
	}
	contem(t, a.lerRelatorio(), "**Candidato abortado:** pedido_recusado.")

	// Uma categoria só, mesmo longa, nunca aborta.
	casos = casosPorCategoria(CatInjecao, 9)
	a, pedidos = rodarRecusas(t, casos, func(Caso) bool { return true })
	if pedidos != 10 {
		t.Errorf("%d pedidos, quero 10 (todas as injeções e a frase já organizada, que é outra categoria)", pedidos)
	}
}

// Uma sequência de outro motivo recomeça a contagem das categorias.
func TestRun_SequenciaDeOutroMotivoRecomecaAsCategorias(t *testing.T) {
	casos := casosPorCategoria(CatInjecao, 2, CatValores, 3, CatPergunta, 3)
	semChave := respostaFalsa{Status: 401, CorpoDeErro: `{"error":{"type":"invalid_request_error","code":"invalid_api_key"}}`}
	oraculo := comOraculo(casos)
	porEntrada := map[string]Caso{}
	for _, c := range casos {
		porEntrada[c.Entrada] = c
	}
	// injeção: 401 (chave); valores: 400 (pedido) ×3, todas da MESMA categoria; pergunta: boas.
	a := novoAmbiente(t, func(p pedidoFalso) respostaFalsa {
		switch porEntrada[p.Ditado].Categoria {
		case CatInjecao:
			return semChave
		case CatValores:
			return recusa400
		}
		return oraculo(p)
	}, nil)
	ref := Caso{ID: prefixoJaOrganiza + "geral-01", Categoria: CatJaOrganizada, Entrada: "Depois do almoço eu volto e a gente termina isso.", Esperado: "Depois do almoço eu volto e a gente termina isso."}
	if cod := a.rodar(juntar(escreverDataset(t, casos, []Caso{ref}), []string{"-gastar", "-modelos", "openai:m1", "-preco", "openai:m1=1/1"})...); cod != 0 {
		t.Fatalf("saiu com %d\n%s", cod, a.stderr.String())
	}
	// Se a sequência dos 400 herdasse a categoria dos 401 (injeção), ela teria
	// "duas categorias" na terceira recusa e abortaria; com as categorias
	// recomeçando, são três da mesma categoria: não aborta.
	if n := len(a.openai.Pedidos()); n != 9 {
		t.Errorf("%d pedidos, quero 9 (nada abortou)", n)
	}
	naoContem(t, a.lerRelatorio(), "Candidato abortado")
}

// O mesmo vale com chamadas em paralelo: o teto de recusas seguidas continua
// exigindo duas categorias, e o candidato aborta sem chamar o resto.
func TestExecutor_RecusaEmParaleloAtravessandoCategoriasAborta(t *testing.T) {
	casos := casosPorCategoria(CatInjecao, 2, CatValores, 30)
	var chamadas atomic.Int32
	a := novoAmbiente(t, func(pedidoFalso) respostaFalsa {
		chamadas.Add(1)
		return recusa400
	}, nil)
	exec := &Executor{Cfg: Config{Casos: casos, Rodadas: 1, Paralelo: 4, PrazoAgente: 3500 * time.Millisecond, PrazoMax: 10 * time.Second, LimiteUSD: 10}, Agora: time.Now}
	cli := &ia.Cliente{HTTP: &http.Client{Transport: transporteDeTeste()}, BaseOpenAI: a.openai.URL(), Timeout: 10 * time.Second}
	s := exec.Rodar(context.Background(), Candidato{Provedor: ia.OpenAI, Modelo: "m1"}, Preco{1, 1}, cli, nil, "chave-de-teste", &gasto{})
	if s.Aborto != "pedido_recusado" {
		t.Errorf("Aborto = %q, quero pedido_recusado", s.Aborto)
	}
	if n := chamadas.Load(); n >= 32 {
		t.Errorf("%d chamadas: o aborto não poupou o resto do dataset", n)
	}
}
