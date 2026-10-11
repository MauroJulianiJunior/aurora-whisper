package localapi_test

// O que a tela sabe do Polish (ADR-016, decisões 10 e 14): polish_disponivel,
// polish_ultimo_motivo (frase pronta, só o motivo, nunca texto ditado) e o
// modelo sugerido. Nada aqui chama a API de um provedor.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

const sufixo = " o último ditado saiu sem o Polish."

// servidorComPolish monta o Server com o ponto de injeção do último Polish.
func servidorComPolish(t *testing.T, ultimo func() (localapi.DesfechoDoPolish, bool)) http.Handler {
	t.Helper()
	pref := prefsDeTeste()
	pref.UltimoPolish = ultimo
	return localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
}

func motivoDaTela(t *testing.T, d localapi.DesfechoDoPolish) string {
	t.Helper()
	h := servidorComPolish(t, func() (localapi.DesfechoDoPolish, bool) { return d, true })
	w := fazer(t, h, http.MethodGet, "/api/settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", w.Code, w.Body.String())
	}
	return decodeJSON[settingsView](t, w).PolishUltimoMotivo
}

// TestPolish_FraseDeCadaMotivo é a tabela: cada MotivoFallback do resolver
// vira uma frase em português simples, com o nome do provedor onde ele
// faz sentido. "curto" e "" não são motivo a mostrar.
func TestPolish_FraseDeCadaMotivo(t *testing.T) {
	casos := []struct{ motivo, quero string }{
		// Não são para mostrar.
		{"", ""},
		{"curto", ""}, // esperado: menos de 4 palavras nem vai ao provedor

		// Do transporte e do provedor: com o nome dele.
		{"sem_cliente", "O Aurora Whisper não conseguiu chamar a IA:" + sufixo},
		{"sem_chave", "Sem chave de API da OpenAI guardada:" + sufixo},
		{"transporte", "Sem conexão com a OpenAI:" + sufixo},
		{"prazo", "A OpenAI demorou mais de 3,5 s:" + sufixo},
		{"chave_recusada", "A OpenAI recusou a chave:" + sufixo},
		{"sem_credito", "Sem crédito na OpenAI:" + sufixo},
		{"limite", "Limite de uso da OpenAI atingido por agora:" + sufixo},
		{"modelo_inexistente", "A OpenAI não achou o modelo escolhido:" + sufixo},
		{"modelo_incompativel", "Este modelo da OpenAI não aceita o pedido do Polish:" + sufixo},
		{"sobrecarga", "A OpenAI está fora do ar ou sobrecarregada:" + sufixo},
		{"pedido_recusado", "A OpenAI rejeitou o pedido do Polish:" + sufixo},
		{"recusa", "O modelo da OpenAI recusou responder:" + sufixo},
		{"truncada", "A resposta da OpenAI veio cortada:" + sufixo},
		{"resposta_invalida", "A OpenAI respondeu de um jeito inesperado:" + sufixo},

		// Das guardas: a IA respondeu e o texto foi recusado; sem provedor.
		{"json", "A resposta da IA veio fora do formato:" + sufixo},
		{"formato", "A resposta da IA trouxe algo além do texto:" + sufixo},
		{"numeros", "A resposta da IA mudou os números:" + sufixo},
		{"siglas_termos", "A resposta da IA mudou siglas, termos ou nomes:" + sufixo},
		{"palavras", "A resposta da IA mudou o texto:" + sufixo},
		{"trecho_novo", "A resposta da IA acrescentou palavras:" + sufixo},
		{"dicionario", "Não consegui ler o dicionário para conferir o texto:" + sufixo},
	}
	for _, c := range casos {
		t.Run("motivo="+c.motivo, func(t *testing.T) {
			got := motivoDaTela(t, localapi.DesfechoDoPolish{Motivo: c.motivo, Provedor: "openai", Quando: time.Now()})
			if got != c.quero {
				t.Fatalf("polish_ultimo_motivo = %q\nquero %q", got, c.quero)
			}
		})
	}
}

// TestPolish_NomeDoProvedor: a frase leva o provedor da TENTATIVA (não o
// gravado agora), e sem provedor conhecido diz "IA".
func TestPolish_NomeDoProvedor(t *testing.T) {
	casos := []struct{ provedor, quero string }{
		{"openai", "Sem crédito na OpenAI:" + sufixo},
		{"anthropic", "Sem crédito na Anthropic:" + sufixo},
		{"", "Sem crédito na IA:" + sufixo},
		{"outro", "Sem crédito na IA:" + sufixo},
	}
	for _, c := range casos {
		if got := motivoDaTela(t, localapi.DesfechoDoPolish{Motivo: "sem_credito", Provedor: c.provedor}); got != c.quero {
			t.Errorf("provedor %q: %q, quero %q", c.provedor, got, c.quero)
		}
	}
	if got := motivoDaTela(t, localapi.DesfechoDoPolish{Motivo: "prazo", Provedor: "anthropic"}); got != "A Anthropic demorou mais de 3,5 s:"+sufixo {
		t.Errorf("prazo na Anthropic: %q", got)
	}
}

// TestPolish_MotivoDesconhecidoNaoVazaONomeDele: um valor fora do enum vira
// a frase genérica — o texto dele, que podia ser qualquer coisa, nunca
// chega à tela.
func TestPolish_MotivoDesconhecidoNaoVazaONomeDele(t *testing.T) {
	const canario = "TEXTO-DITADO-QUE-NAO-PODE-APARECER"
	got := motivoDaTela(t, localapi.DesfechoDoPolish{Motivo: canario, Provedor: "openai"})
	if got != "Algo impediu o Polish:"+sufixo {
		t.Fatalf("polish_ultimo_motivo = %q, quero a frase genérica", got)
	}
	if strings.Contains(got, canario) {
		t.Fatalf("o motivo desconhecido vazou para a tela: %q", got)
	}
}

// TestPolish_SemInformacaoDoAgentNaoMostraNada cobre o ponto de injeção
// ausente e o "ainda não houve Polish".
func TestPolish_SemInformacaoDoAgentNaoMostraNada(t *testing.T) {
	semPonto := servidorComPolish(t, nil)
	if got := decodeJSON[settingsView](t, fazer(t, semPonto, http.MethodGet, "/api/settings", nil)); got.PolishUltimoMotivo != "" {
		t.Fatalf("UltimoPolish nil: polish_ultimo_motivo = %q, quero \"\"", got.PolishUltimoMotivo)
	}
	semTentativa := servidorComPolish(t, func() (localapi.DesfechoDoPolish, bool) {
		// Mesmo com um motivo preenchido, ok=false manda: não houve tentativa.
		return localapi.DesfechoDoPolish{Motivo: "sem_credito", Provedor: "openai"}, false
	})
	if got := decodeJSON[settingsView](t, fazer(t, semTentativa, http.MethodGet, "/api/settings", nil)); got.PolishUltimoMotivo != "" {
		t.Fatalf("ok=false: polish_ultimo_motivo = %q, quero \"\"", got.PolishUltimoMotivo)
	}
}

// TestPolish_SoMostraMotivoRecente: "o último ditado" de anteontem, que a
// pessoa já resolveu, só confunde. Sem hora (zero), vale como recente.
func TestPolish_SoMostraMotivoRecente(t *testing.T) {
	agora := time.Now()
	casos := []struct {
		nome   string
		quando time.Time
		mostra bool
	}{
		{"agora", agora, true},
		{"há 1 minuto", agora.Add(-time.Minute), true},
		{"há 23 horas", agora.Add(-23 * time.Hour), true},
		{"há 25 horas", agora.Add(-25 * time.Hour), false},
		{"há 3 dias", agora.Add(-72 * time.Hour), false},
		{"sem hora (zero)", time.Time{}, true},
		{"no futuro (relógio mudou)", agora.Add(time.Hour), true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := motivoDaTela(t, localapi.DesfechoDoPolish{Motivo: "sem_credito", Provedor: "openai", Quando: c.quando})
			if (got != "") != c.mostra {
				t.Fatalf("polish_ultimo_motivo = %q, mostra esperado = %v", got, c.mostra)
			}
		})
	}
}

// TestPolish_UltimoMotivoMudaComOAgent: o campo é lido a cada GET, não
// guardado. Um Polish que agiu depois do fallback apaga a frase.
func TestPolish_UltimoMotivoMudaComOAgent(t *testing.T) {
	atual := localapi.DesfechoDoPolish{Motivo: "sem_credito", Provedor: "openai", Quando: time.Now()}
	h := servidorComPolish(t, func() (localapi.DesfechoDoPolish, bool) { return atual, true })
	pegar := func() string {
		return decodeJSON[settingsView](t, fazer(t, h, http.MethodGet, "/api/settings", nil)).PolishUltimoMotivo
	}
	if pegar() == "" {
		t.Fatal("com sem_credito a frase devia aparecer")
	}
	atual = localapi.DesfechoDoPolish{Motivo: "", Provedor: "openai", Quando: time.Now()} // o Polish agiu
	if got := pegar(); got != "" {
		t.Fatalf("depois de um Polish que agiu, polish_ultimo_motivo = %q, quero \"\"", got)
	}
}

// TestPolish_PutDevolveOMesmoCampoQueOGet: o PUT devolve o corpo do GET.
func TestPolish_PutDevolveOMesmoCampoQueOGet(t *testing.T) {
	h := servidorComPolish(t, func() (localapi.DesfechoDoPolish, bool) {
		return localapi.DesfechoDoPolish{Motivo: "prazo", Provedor: "openai", Quando: time.Now()}, true
	})
	w := fazer(t, h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "beep_enabled": false})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[settingsView](t, w).PolishUltimoMotivo; got != "A OpenAI demorou mais de 3,5 s:"+sufixo {
		t.Fatalf("PUT polish_ultimo_motivo = %q", got)
	}
}

// TestPolish_TodoMotivoDoResolverTemFrase lê os MotivoFallback direto do
// código do resolver e exige que cada um ou seja um dos dois que não se
// mostram ("" e "curto") ou tenha frase própria — não a genérica. Um motivo
// novo no resolver (a caixa alta, por exemplo) sem frase aqui quebra este
// teste, em vez de chegar à tela como "Algo impediu o Polish".
func TestPolish_TodoMotivoDoResolverTemFrase(t *testing.T) {
	motivos := motivosDoResolver(t)
	if len(motivos) < 20 {
		t.Fatalf("achei só %d motivos no resolver (%v): o teste não está lendo o código certo", len(motivos), motivos)
	}
	generica := motivoDaTela(t, localapi.DesfechoDoPolish{Motivo: "motivo-que-nao-existe", Provedor: "openai"})
	for _, m := range motivos {
		got := motivoDaTela(t, localapi.DesfechoDoPolish{Motivo: m, Provedor: "openai"})
		if m == "" || m == "curto" {
			if got != "" {
				t.Errorf("motivo %q: %q, quero \"\" (não é para mostrar)", m, got)
			}
			continue
		}
		if got == "" || got == generica {
			t.Errorf("motivo %q do resolver não tem frase própria (saiu %q): acrescente em frasesDoMotivo", m, got)
		}
	}
}

// motivosDoResolver acha os valores das constantes do tipo MotivoFallback
// nos fontes de internal/resolver.
func motivosDoResolver(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "resolver")
	arquivos, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(arquivos) == 0 {
		t.Fatalf("não achei os fontes do resolver em %s: %v", dir, err)
	}
	var valores []string
	for _, arq := range arquivos {
		if strings.HasSuffix(arq, "_test.go") {
			continue
		}
		src, err := os.ReadFile(arq)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), arq, src, 0)
		if err != nil {
			t.Fatalf("lendo %s: %v", arq, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "MotivoFallback" {
				return true
			}
			for _, v := range vs.Values {
				if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						valores = append(valores, s)
					}
				}
			}
			return true
		})
	}
	return valores
}

// TestPolish_ModeloSugeridoNoTestar: POST /api/ia/testar devolve o modelo
// que o projeto sugere para o provedor (o vencedor do benchmark de
// 30-set-2026): gpt-6-luna na OpenAI, nenhum na Anthropic. É sugestão —
// vem junto da lista da conta, e a tela confere se ele consta nela.
func TestPolish_ModeloSugeridoNoTestar(t *testing.T) {
	type testarComSugestao struct {
		OK             bool     `json:"ok"`
		Modelos        []string `json:"modelos"`
		ModeloSugerido *string  `json:"modelo_sugerido"`
		Erro           string   `json:"erro"`
	}
	casos := []struct{ provedor, quero string }{
		{"openai", "gpt-6-luna"},
		{"anthropic", ""},
	}
	for _, c := range casos {
		t.Run(c.provedor, func(t *testing.T) {
			cofre := chaves.NovaMemoria()
			if err := cofre.Gravar(c.provedor, chaveDeTeste); err != nil {
				t.Fatal(err)
			}
			h := servidorDeIA(t, cofre, &modelosFalsos{modelos: []string{"um-modelo", "outro-modelo"}}, nil)
			w := fazer(t, h, http.MethodPost, "/api/ia/testar", map[string]string{"provedor": c.provedor})
			got := decodeJSON[testarComSugestao](t, w)
			if w.Code != http.StatusOK || !got.OK {
				t.Fatalf("testar: %d %+v", w.Code, got)
			}
			if got.ModeloSugerido == nil {
				t.Fatal("modelo_sugerido ausente: o campo tem de vir sempre, \"\" quando não há sugestão")
			}
			if *got.ModeloSugerido != c.quero {
				t.Fatalf("modelo_sugerido = %q, quero %q", *got.ModeloSugerido, c.quero)
			}
			// A sugestão não filtra a lista: o que o provedor listou vai inteiro,
			// mesmo sem o sugerido.
			if len(got.Modelos) != 2 {
				t.Fatalf("modelos = %v, quero os 2 que o provedor listou", got.Modelos)
			}
		})
	}
}

// TestPolish_SugestaoNuncaEExigida: nada valida ai_model contra a sugestão.
// Qualquer nome de modelo é aceito no PUT, com o Polish inclusive.
func TestPolish_SugestaoNuncaEExigida(t *testing.T) {
	s := novoServidorDeSettings(t)
	if err := s.cofre.Gravar(ia.OpenAI, chaveDeTeste); err != nil {
		t.Fatal(err)
	}
	for _, modelo := range []string{"gpt-5-nano", "um-modelo-que-nao-existe", "gpt-6-luna"} {
		got := s.get(t)
		w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{
			"base_version": got.Version, "text_mode": "polish", "ai_provider": "openai", "ai_model": modelo,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("modelo %q: PUT status = %d, body = %s", modelo, w.Code, w.Body.String())
		}
		if v := decodeJSON[settingsView](t, w); v.AIModel != modelo {
			t.Fatalf("ai_model = %q, quero %q", v.AIModel, modelo)
		}
	}
}
