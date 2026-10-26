package localapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// POST /api/notas/separar-itens (ADR-022, N5): a IA só com o Polish ligado
// nas preferências; a regra no resto; o trecho nunca no log.

// iaDaListaFalsa é o cliente do provedor: devolve a resposta dada e guarda o
// que recebeu. Nenhuma rede.
type iaDaListaFalsa struct {
	mu       sync.Mutex
	resposta []byte
	err      error
	pedidos  []resolver.PolishRequest
	abertos  [][2]string
}

func (f *iaDaListaFalsa) Polish(_ context.Context, req resolver.PolishRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pedidos = append(f.pedidos, req)
	return f.resposta, f.err
}

func (f *iaDaListaFalsa) abrir(provedor, modelo string) resolver.PolishClient {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abertos = append(f.abertos, [2]string{provedor, modelo})
	return f
}

type itensView struct {
	Itens []string `json:"itens"`
	Como  string   `json:"como"`
}

// servidorDaLista monta a API com as preferências gravadas e a IA falsa.
func servidorDaLista(t *testing.T, gravadas *storage.Settings, ia *iaDaListaFalsa, log *slog.Logger) http.Handler {
	t.Helper()
	settings := storage.NewInMemorySettingsRepository()
	if gravadas != nil {
		if _, err := settings.Put(context.Background(), *gravadas, 0); err != nil {
			t.Fatal(err)
		}
	}
	pref := prefsDeTeste()
	pref.Log = log
	if ia != nil {
		pref.ClienteDaLista = ia.abrir
	}
	return localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), settings, pref).Handler()
}

func comPolish(provedor, modelo string) *storage.Settings {
	s := padraoDeTeste()
	s.TextMode, s.AIProvider, s.AIModel = "polish", provedor, modelo
	return &s
}

func separar(t *testing.T, h http.Handler, trecho string) itensView {
	t.Helper()
	w := fazer(t, h, http.MethodPost, "/api/notas/separar-itens", map[string]string{"trecho": trecho})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	return decodeJSON[itensView](t, w)
}

func respostaDaIA(itens ...string) []byte {
	b, _ := json.Marshal(map[string]any{"itens": itens})
	return b
}

func TestSepararItens_ComOPolishLigadoVaiAIA(t *testing.T) {
	ia := &iaDaListaFalsa{resposta: respostaDaIA("leite", "pão", "café", "duas dúzias de ovos")}
	h := servidorDaLista(t, comPolish("openai", "modelo-x"), ia, nil)
	got := separar(t, h, "leite pão café e duas dúzias de ovos")
	if got.Como != "ia" || strings.Join(got.Itens, "|") != "Leite|Pão|Café|Duas dúzias de ovos" {
		t.Fatalf("got %+v", got)
	}
	if len(ia.abertos) != 1 || ia.abertos[0] != [2]string{"openai", "modelo-x"} {
		t.Fatalf("cliente aberto com %v, quero o provedor e o modelo das preferências", ia.abertos)
	}
	if len(ia.pedidos) != 1 || ia.pedidos[0].PromptRef != resolver.NotasListaPromptRef {
		t.Fatalf("pedidos = %v", ia.pedidos)
	}
}

func TestSepararItens_RecusadaFicaARegra(t *testing.T) {
	ia := &iaDaListaFalsa{resposta: respostaDaIA("leite", "pão francês", "café")}
	h := servidorDaLista(t, comPolish("anthropic", "modelo-a"), ia, nil)
	if got := separar(t, h, "leite, pão e café"); got.Como != "regra" || strings.Join(got.Itens, "|") != "Leite|Pão|Café" {
		t.Fatalf("got %+v", got)
	}
	// Sem vírgula, nem a regra separa: nenhum, com a lista vazia (nunca null).
	w := fazer(t, h, http.MethodPost, "/api/notas/separar-itens", map[string]string{"trecho": "leite pão café"})
	if !strings.Contains(w.Body.String(), `"itens":[]`) || decodeJSON[itensView](t, w).Como != "nenhum" {
		t.Fatalf("got %s", w.Body.String())
	}
}

// TestSepararItens_SemPolishNemChamaAIA: modo smart, sem provedor, sem modelo
// ou sem o cliente montado: a regra, sem abrir cliente nenhum.
func TestSepararItens_SemPolishNemChamaAIA(t *testing.T) {
	smart := padraoDeTeste()
	semModelo := comPolish("openai", " ")
	semProvedor := comPolish("", "modelo-x")
	for nome, gravadas := range map[string]*storage.Settings{
		"nada gravado": nil, "modo smart": &smart, "sem modelo": semModelo, "sem provedor": semProvedor,
	} {
		t.Run(nome, func(t *testing.T) {
			ia := &iaDaListaFalsa{resposta: respostaDaIA("a", "b")}
			h := servidorDaLista(t, gravadas, ia, nil)
			if got := separar(t, h, "arroz, feijão e batata"); got.Como != "regra" || len(got.Itens) != 3 {
				t.Fatalf("got %+v", got)
			}
			if len(ia.abertos) != 0 || len(ia.pedidos) != 0 {
				t.Fatalf("a IA foi aberta (%v) ou chamada (%d)", ia.abertos, len(ia.pedidos))
			}
		})
	}
	t.Run("sem cliente montado", func(t *testing.T) {
		h := servidorDaLista(t, comPolish("openai", "modelo-x"), nil, nil)
		if got := separar(t, h, "arroz, feijão e batata"); got.Como != "regra" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestSepararItens_PedidoInvalido(t *testing.T) {
	h := servidorDaLista(t, nil, nil, nil)
	for nome, corpo := range map[string]any{
		"JSON quebrado":  `{"trecho":`,
		"vazio":          map[string]string{"trecho": "   "},
		"várias linhas":  map[string]string{"trecho": "leite\npão"},
		"longo demais":   map[string]string{"trecho": strings.Repeat("palavra ", 300)},
		"corpo gigante":  map[string]string{"trecho": strings.Repeat("x", 20<<10)},
		"tipo diferente": map[string]any{"trecho": 3},
		"campo a mais":   map[string]any{"trecho": "leite, pão", "nota": "n1"},
	} {
		t.Run(nome, func(t *testing.T) {
			w := fazer(t, h, http.MethodPost, "/api/notas/separar-itens", corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, quero 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestSepararItens_OTrechoNuncaVaiAoLog: com a IA recusada (o caminho que
// loga), o log leva como, motivo e o prompt, e nada do trecho nem da resposta.
func TestSepararItens_OTrechoNuncaVaiAoLog(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ia := &iaDaListaFalsa{resposta: respostaDaIA("Zephyrion", "Quillabar inventado")}
	h := servidorDaLista(t, comPolish("openai", "modelo-x"), ia, log)
	separar(t, h, "Zephyrion, Quillabar e Xantolho")
	saida := buf.String()
	if !strings.Contains(saida, "motivo=palavras") || !strings.Contains(saida, "como=regra") {
		t.Fatalf("o log não diz o motivo: %s", saida)
	}
	for _, segredo := range []string{"Zephyrion", "Quillabar", "Xantolho", "inventado"} {
		if strings.Contains(saida, segredo) {
			t.Fatalf("o log levou texto da nota (%q):\n%s", segredo, saida)
		}
	}
}
