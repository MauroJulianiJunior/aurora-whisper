package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// TestAbrirDicionario_TermoDoPainelNaoContaComoCortadoDoPrompt: o termo que o
// painel criou fica fora do vocabulário do ASR por desenho, e por isso não
// disputou o orçamento. Contá-lo entre as ativas fazia o log dizer "o
// orçamento cortou termos", ou "NENHUM termo coube", sem ser verdade.
func TestAbrirDicionario_TermoDoPainelNaoContaComoCortadoDoPrompt(t *testing.T) {
	abrir := func(t *testing.T, conteudo string) string {
		t.Helper()
		caminho := filepath.Join(t.TempDir(), nomeDicionario)
		if err := os.WriteFile(caminho, []byte(conteudo), 0o600); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, _, _, _, err := abrirDicionarioEm(slog.New(slog.NewTextHandler(&buf, nil)), caminho); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	t.Run("só termos do painel", func(t *testing.T) {
		log := abrir(t, `{"usuario":"mauro","alias_origem":{"ista":"painel"},"entradas":[`+
			`{"canonica":"esta","prioridade":50,"alias":["ista"],"origem":"painel"}]}`)
		if strings.Contains(log, "NENHUM termo coube") {
			t.Errorf("o log avisou que nada coube no prompt, e não havia termo para caber:\n%s", log)
		}
	})
	t.Run("um termo comum e um do painel", func(t *testing.T) {
		log := abrir(t, `{"usuario":"mauro","alias_origem":{"ista":"painel"},"entradas":[`+
			`{"canonica":"DCTFWeb","prioridade":100},`+
			`{"canonica":"esta","prioridade":50,"alias":["ista"],"origem":"painel"}]}`)
		if strings.Contains(log, "cortou") {
			t.Errorf("o log diz que o orçamento cortou um termo, e o do painel nunca disputou:\n%s", log)
		}
		if !strings.Contains(log, "termos_no_prompt=1") {
			t.Errorf("o controle falhou: o termo comum devia estar no prompt:\n%s", log)
		}
	})
	// O termo que a pessoa adicionou sem a dica também não disputou (ADR-020);
	// o com a dica disputou, e entrou.
	t.Run("adicionados com e sem a dica", func(t *testing.T) {
		log := abrir(t, `{"usuario":"mauro","entradas":[`+
			`{"canonica":"RAG","prioridade":100,"origem":"adicionado"},`+
			`{"canonica":"Sem","prioridade":50,"origem":"adicionado"}]}`)
		if strings.Contains(log, "cortou") {
			t.Errorf("o log diz que o orçamento cortou um termo, e o sem dica nunca disputou:\n%s", log)
		}
		if !strings.Contains(log, "termos_no_prompt=1") {
			t.Errorf("a dica devia estar no prompt:\n%s", log)
		}
	})
}

// TestAlcancadas: o filtro da conferência deixa passar a variante que casa com
// a entrada que mudou, e só ela; sem mudança, nenhuma.
func TestAlcancadas(t *testing.T) {
	antes := mustSemente(t, dicionarioComViti)
	if alcancadas(antes, antes)("Viti") {
		t.Error("sem mudança, uma variante passou pelo filtro")
	}
	depois, _, err := antes.AdicionarTermo(dictionary.PedidoTermo{Termo: "Vite", Dica: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	alcanca := alcancadas(antes, depois)
	if !alcanca("Viti") || !alcanca("o viti de lá") {
		t.Error("a variante que casa por fonética com o termo novo não passou")
	}
	if alcanca("ECF") {
		t.Error("uma variante sem nada com o termo novo passou")
	}
	// A versão de antes da entrada que mudou também conta: desligar a dica
	// (ou remover) pode tirar da busca o que casava com ela.
	sem, _, err := depois.RemoverTermo("Vite")
	if err != nil {
		t.Fatal(err)
	}
	if !alcancadas(depois, sem)("Viti") {
		t.Error("a entrada que saiu não conta no filtro")
	}
}
