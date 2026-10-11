package localapi_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

type chaveView struct {
	Provedor    string `json:"provedor"`
	Configurada bool   `json:"configurada"`
	Final       string `json:"final"`
}

type testarView struct {
	OK      bool     `json:"ok"`
	Modelos []string `json:"modelos"`
	Erro    string   `json:"erro"`
}

const chaveDeTeste = "sk-teste-0123456789abcdefghijklmnopqrstuvWXYZ"

func servidorDeIA(t *testing.T, cofre chaves.Cofre, modelos localapi.ListadorDeModelos, log *slog.Logger) http.Handler {
	t.Helper()
	pref := prefsDeTeste()
	pref.Chaves, pref.Modelos, pref.Log = cofre, modelos, log
	return localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
}

func TestChave_GuardaMostraSoOFinalEApaga(t *testing.T) {
	h := servidorDeIA(t, chaves.NovaMemoria(), &modelosFalsos{}, nil)

	w := fazer(t, h, http.MethodGet, "/api/ia/chave?provedor=openai", nil)
	if got := decodeJSON[chaveView](t, w); w.Code != 200 || got != (chaveView{Provedor: "openai"}) {
		t.Fatalf("antes de guardar: %d %+v", w.Code, got)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("a rota da chave pode ficar em cache")
	}

	w = fazer(t, h, http.MethodPut, "/api/ia/chave", map[string]string{"provedor": "openai", "chave": " " + chaveDeTeste + "\r\n"})
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("PUT: %d %q; quero 204 sem corpo", w.Code, w.Body.String())
	}

	w = fazer(t, h, http.MethodGet, "/api/ia/chave?provedor=openai", nil)
	if got := decodeJSON[chaveView](t, w); got != (chaveView{Provedor: "openai", Configurada: true, Final: "WXYZ"}) {
		t.Fatalf("depois de guardar: %+v", got)
	}
	// Cada provedor tem a sua.
	if got := decodeJSON[chaveView](t, fazer(t, h, http.MethodGet, "/api/ia/chave?provedor=anthropic", nil)); got.Configurada {
		t.Fatal("a chave da OpenAI apareceu na Anthropic")
	}

	if w = fazer(t, h, http.MethodDelete, "/api/ia/chave?provedor=openai", nil); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[chaveView](t, fazer(t, h, http.MethodGet, "/api/ia/chave?provedor=openai", nil)); got.Configurada {
		t.Fatal("a chave continua lá depois do DELETE")
	}
	if w = fazer(t, h, http.MethodDelete, "/api/ia/chave?provedor=openai", nil); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE sem chave: %d; apagar o que não existe não é erro", w.Code)
	}
}

func TestChave_ProvedorDesconhecidoDa400EmTodaRota(t *testing.T) {
	h := servidorDeIA(t, chaves.NovaMemoria(), &modelosFalsos{}, nil)
	for _, c := range []struct {
		metodo, url string
		corpo       any
	}{
		{http.MethodGet, "/api/ia/chave?provedor=google", nil},
		{http.MethodGet, "/api/ia/chave", nil},
		{http.MethodPut, "/api/ia/chave", map[string]string{"provedor": "OpenAI", "chave": chaveDeTeste}},
		{http.MethodDelete, "/api/ia/chave?provedor=../x", nil},
		{http.MethodPost, "/api/ia/testar", map[string]string{"provedor": ""}},
	} {
		w := fazer(t, h, c.metodo, c.url, c.corpo)
		if w.Code != http.StatusBadRequest || !strings.Contains(decodeJSON[erroView](t, w).Error.Message, "openai ou anthropic") {
			t.Errorf("%s %s: %d %s", c.metodo, c.url, w.Code, w.Body.String())
		}
	}
}

func TestChave_InvalidaDa400ComMensagemLegivel(t *testing.T) {
	cofre := chaves.NovaMemoria()
	h := servidorDeIA(t, cofre, &modelosFalsos{}, nil)
	for chave, trecho := range map[string]string{
		"":                        "Cole a chave",
		"  \n ":                   "Cole a chave",
		"sk-abc def":              "Copie de novo",
		"sk-abc\r\nX-Injetado: 1": "Copie de novo",
		strings.Repeat("k", 2561): "longo demais",
	} {
		w := fazer(t, h, http.MethodPut, "/api/ia/chave", map[string]string{"provedor": "openai", "chave": chave})
		if w.Code != http.StatusBadRequest || !strings.Contains(decodeJSON[erroView](t, w).Error.Message, trecho) {
			t.Errorf("chave %.20q: %d %s", chave, w.Code, w.Body.String())
		}
	}
	if _, err := cofre.Ler("openai"); !errors.Is(err, chaves.ErrAusente) {
		t.Fatal("uma chave inválida foi guardada")
	}
	if w := fazer(t, h, http.MethodPut, "/api/ia/chave", `{"provedor":"openai","chave":`); w.Code != http.StatusBadRequest {
		t.Fatalf("JSON quebrado: %d", w.Code)
	}
}

// cofreQuebrado é o Gerenciador de Credenciais falhando.
type cofreQuebrado struct{}

var errDoCofre = errors.New("CredWriteW: Acesso negado.")

func (cofreQuebrado) Gravar(string, string) error { return errDoCofre }
func (cofreQuebrado) Ler(string) (string, error)  { return "", errDoCofre }
func (cofreQuebrado) Apagar(string) error         { return errDoCofre }

// TestChave_FalhaDoCofreDa500ELogaSoOErro: a resposta é genérica; o erro
// do Windows vai para o log, para quem diagnosticar — sem a chave.
func TestChave_FalhaDoCofreDa500ELogaSoOErro(t *testing.T) {
	var log bytes.Buffer
	h := servidorDeIA(t, cofreQuebrado{}, &modelosFalsos{}, slog.New(slog.NewTextHandler(&log, nil)))
	for _, c := range []struct {
		metodo, url string
		corpo       any
	}{
		{http.MethodGet, "/api/ia/chave?provedor=openai", nil},
		{http.MethodPut, "/api/ia/chave", map[string]string{"provedor": "openai", "chave": chaveDeTeste}},
		{http.MethodDelete, "/api/ia/chave?provedor=openai", nil},
		{http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "openai"}},
	} {
		w := fazer(t, h, c.metodo, c.url, c.corpo)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Gerenciador de Credenciais") {
			t.Errorf("%s %s: %d %s", c.metodo, c.url, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "Acesso negado") {
			t.Errorf("%s %s: o erro interno vazou na resposta", c.metodo, c.url)
		}
	}
	if strings.Count(log.String(), "Acesso negado") != 4 {
		t.Errorf("o log não registrou as quatro falhas:\n%s", log.String())
	}
	if strings.Contains(log.String(), chaveDeTeste) {
		t.Error("a chave foi para o log")
	}
}

func TestTestar_UsaAChaveGuardadaETrazOsModelos(t *testing.T) {
	cofre := chaves.NovaMemoria()
	provedor := &modelosFalsos{modelos: []string{"modelo-2", "modelo-1"}}
	h := servidorDeIA(t, cofre, provedor, nil)
	if err := cofre.Gravar("anthropic", chaveDeTeste); err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	w := fazer(t, h, http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "anthropic"})
	got := decodeJSON[testarView](t, w)
	if w.Code != 200 || !got.OK || !reflect.DeepEqual(got.Modelos, []string{"modelo-2", "modelo-1"}) || got.Erro != "" {
		t.Fatalf("testar: %d %+v", w.Code, got)
	}
	if provedor.chaveVista != chaveDeTeste {
		t.Fatal("o teste não usou a chave guardada")
	}
	if strings.Contains(w.Body.String(), chaveDeTeste) {
		t.Fatal("a chave voltou na resposta do teste")
	}
}

func TestTestar_ListaVaziaEhOKComListaVazia(t *testing.T) {
	cofre := chaves.NovaMemoria()
	cofre.Gravar("openai", chaveDeTeste)
	w := fazer(t, servidorDeIA(t, cofre, &modelosFalsos{}, nil), http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "openai"})
	if !strings.Contains(w.Body.String(), `"modelos":[]`) {
		t.Fatalf("body = %s; quero modelos: []", w.Body.String())
	}
}

func TestTestar_SemChaveNemChamaOProvedor(t *testing.T) {
	provedor := &modelosFalsos{}
	w := fazer(t, servidorDeIA(t, chaves.NovaMemoria(), provedor, nil), http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "openai"})
	got := decodeJSON[testarView](t, w)
	if w.Code != 200 || got.OK || !strings.Contains(got.Erro, "Nenhuma chave da OpenAI") {
		t.Fatalf("testar sem chave: %d %+v", w.Code, got)
	}
	if provedor.chamadas != 0 {
		t.Fatal("chamou o provedor sem chave")
	}
}

// TestTestar_FalhaDoProvedorEhOKFalseComAMensagemTraduzida: a mensagem
// de ia.Erro vai inteira; um erro sem tradução vira mensagem genérica —
// o texto dele não se sabe o que carrega.
func TestTestar_FalhaDoProvedorEhOKFalseComAMensagemTraduzida(t *testing.T) {
	for nome, c := range map[string]struct {
		err   error
		quero string
	}{
		"traduzido":    {&ia.Erro{Motivo: ia.MotivoChaveRecusada, Mensagem: "A OpenAI recusou a chave."}, "A OpenAI recusou a chave."},
		"sem tradução": {errors.New("dial tcp: " + chaveDeTeste), "Não consegui testar a chave da OpenAI."},
	} {
		t.Run(nome, func(t *testing.T) {
			cofre := chaves.NovaMemoria()
			cofre.Gravar("openai", chaveDeTeste)
			var log bytes.Buffer
			h := servidorDeIA(t, cofre, &modelosFalsos{err: c.err}, slog.New(slog.NewTextHandler(&log, nil)))
			w := fazer(t, h, http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "openai"})
			got := decodeJSON[testarView](t, w)
			if w.Code != 200 || got.OK || got.Erro != c.quero || got.Modelos != nil {
				t.Fatalf("%d %+v", w.Code, got)
			}
			if strings.Contains(w.Body.String()+log.String(), chaveDeTeste) {
				t.Fatal("a chave escapou pelo erro")
			}
		})
	}
}
