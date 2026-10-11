package localapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // driver puro Go, só para teste (ADR-008).

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// chaveQueNaoPodeVazar tem letras fora do hexadecimal, maiúsculas e
// minúsculas misturadas: nenhum pedaço dela aparece por acaso num UUID de
// request_id nem em JSON comum.
const chaveQueNaoPodeVazar = "sk-proj-QwErTyVaZaMeNtOzZ-XyXyKpKpWvWv-kLmNoPqR-fImJ"

// pedacosDaChave são todas as janelas de 8 caracteres da chave. Achar
// qualquer uma é vazamento: toda janela de 8 tem pelo menos 4 caracteres
// fora do "final" que a API mostra de propósito.
func pedacosDaChave() []string {
	var p []string
	for i := 0; i+8 <= len(chaveQueNaoPodeVazar); i++ {
		p = append(p, chaveQueNaoPodeVazar[i:i+8])
	}
	return p
}

func procurarChave(t *testing.T, onde, texto string) {
	t.Helper()
	for _, p := range pedacosDaChave() {
		if strings.Contains(texto, p) {
			t.Errorf("pedaço da chave (%q) em %s", p, onde)
			return
		}
	}
}

// TestChave_NuncaSaiDoCofre passa a chave por TODAS as rotas que tocam
// nela — guardar, consultar, validar o Polish, testar contra um provedor
// que a devolve no corpo e num cabeçalho (como o 401 da OpenAI), apagar —
// e procura a chave em tudo o que sai: corpo e cabeçalhos de cada
// resposta, o log da API, o log padrão do processo, e cada coluna de cada
// tabela do SQLite de verdade por trás do histórico e das preferências.
func TestChave_NuncaSaiDoCofre(t *testing.T) {
	ctx := context.Background()

	// O log padrão do processo, capturado: nada deste caminho pode
	// escrever a chave em lugar nenhum, nem por engano pelo slog.Default.
	var logPadrao bytes.Buffer
	antesSlog, antesLog := slog.Default(), log.Writer()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logPadrao, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&logPadrao)
	t.Cleanup(func() { slog.SetDefault(antesSlog); log.SetOutput(antesLog) })

	var logAPI bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logAPI, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// O provedor falso ecoa a chave, como a OpenAI faz no 401. Se a chave
	// passar do provedor para a resposta, o teste vê.
	chavesVistas := 0
	provedor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebida := r.Header.Get("x-api-key") + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if recebida == chaveQueNaoPodeVazar {
			chavesVistas++
		}
		w.Header().Set("X-Eco", recebida)
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":{"message":"Incorrect API key provided: %s"}}`, recebida)
			return
		}
		w.Write([]byte(`{"data":[{"id":"modelo-1"}]}`))
	}))
	defer provedor.Close()

	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := storage.NewMigrator().Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cofre := chaves.NovaMemoria()
	pref := prefsDeTeste()
	pref.Chaves = cofre
	pref.Modelos = &ia.Cliente{BaseOpenAI: provedor.URL, BaseAnthropic: provedor.URL}
	pref.Log = logger
	pref.Aplicar = func(s storage.Settings) []string {
		logger.Info("aplicado", "preferencias", fmt.Sprintf("%+v", s))
		return nil
	}
	h := localapi.New(testUserID, storage.NewSQLiteTranscriptionRepository(db), storage.NewSQLiteSettingsRepository(db), pref).Handler()

	passos := []struct {
		metodo, url string
		corpo       any
		status      int
	}{
		{http.MethodPut, "/api/ia/chave", map[string]string{"provedor": "openai", "chave": "  " + chaveQueNaoPodeVazar + "\n"}, 204},
		{http.MethodPut, "/api/ia/chave", map[string]string{"provedor": "anthropic", "chave": chaveQueNaoPodeVazar}, 204},
		{http.MethodGet, "/api/ia/chave?provedor=openai", nil, 200},
		{http.MethodPut, "/api/ia/chave", map[string]string{"provedor": "openai", "chave": chaveQueNaoPodeVazar + " x"}, 400},
		{http.MethodPut, "/api/ia/chave", `{"provedor":"openai","chave":"` + chaveQueNaoPodeVazar, 400},
		{http.MethodPut, "/api/ia/chave", map[string]string{"provedor": chaveQueNaoPodeVazar, "chave": chaveQueNaoPodeVazar}, 400},
		{http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "text_mode": "polish", "ai_provider": "openai", "ai_model": "m"}, 200},
		{http.MethodPut, "/api/settings", map[string]any{"base_version": 1, "ai_provider": "google"}, 400},
		{http.MethodGet, "/api/settings", nil, 200},
		{http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "openai"}, 200},
		{http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "anthropic"}, 200},
		{http.MethodDelete, "/api/ia/chave?provedor=anthropic", nil, 204},
		{http.MethodPost, "/api/ia/testar", map[string]string{"provedor": "anthropic"}, 200},
		{http.MethodGet, "/api/history", nil, 200},
	}
	for _, p := range passos {
		w := fazer(t, h, p.metodo, p.url, p.corpo)
		if w.Code != p.status {
			t.Fatalf("%s %s: status = %d, quero %d; body = %s", p.metodo, p.url, w.Code, p.status, w.Body.String())
		}
		procurarChave(t, fmt.Sprintf("resposta de %s %s", p.metodo, p.url), w.Body.String())
		for nome, valores := range w.Header() {
			procurarChave(t, fmt.Sprintf("cabeçalho %s de %s %s", nome, p.metodo, p.url), strings.Join(valores, " "))
		}
	}

	// Com dentes: a chave de fato circulou — o provedor a recebeu nos
	// dois testes com chave, e ela continua no cofre (onde deve estar).
	if chavesVistas != 2 {
		t.Fatalf("o provedor recebeu a chave %d vezes, quero 2: o teste não exercitou o caminho", chavesVistas)
	}
	if guardada, err := cofre.Ler("openai"); err != nil || guardada != chaveQueNaoPodeVazar {
		t.Fatalf("a chave não está no cofre: %v", err)
	}
	if logAPI.Len() == 0 {
		t.Fatal("o log da API ficou vazio: o teste não está vendo o que se registra")
	}

	procurarChave(t, "log da API", logAPI.String())
	procurarChave(t, "log padrão do processo", logPadrao.String())
	procurarChave(t, "banco SQLite", despejarBanco(t, db))
}

// despejarBanco devolve o conteúdo de todas as colunas de todas as
// linhas de todas as tabelas, como texto.
func despejarBanco(t *testing.T, db *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	tabelas, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("listar tabelas: %v", err)
	}
	var nomes []string
	for tabelas.Next() {
		var n string
		tabelas.Scan(&n)
		nomes = append(nomes, n)
	}
	tabelas.Close()

	var tudo strings.Builder
	linhas := 0
	for _, tabela := range nomes {
		rows, err := db.QueryContext(ctx, `SELECT * FROM "`+tabela+`"`)
		if err != nil {
			t.Fatalf("ler %s: %v", tabela, err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			valores := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range valores {
				ptrs[i] = &valores[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("scan %s: %v", tabela, err)
			}
			linhas++
			for _, v := range valores {
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				fmt.Fprintf(&tudo, "%s|%v\n", tabela, v)
			}
		}
		rows.Close()
	}
	// local_settings tem a linha gravada pelo PUT; schema_migrations, as
	// versões. Sem linhas, a varredura não estaria vendo nada.
	if !strings.Contains(tudo.String(), "local_settings|polish") || linhas < 3 {
		t.Fatalf("o despejo do banco não viu as preferências gravadas (%d linhas)", linhas)
	}
	return tudo.String()
}
