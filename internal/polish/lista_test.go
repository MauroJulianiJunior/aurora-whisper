package polish

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// TestClienteDaLista_SeparaComOPromptDaLista: a separação em itens das notas
// (ADR-022, N5) usa o mesmo cliente do Polish (provedor, modelo, chave de
// quem chama), com o prompt e o nome do schema da lista, contra o provedor
// falso em loopback; e sem provedor válido não há cliente nem leitura de chave.
func TestClienteDaLista_SeparaComOPromptDaLista(t *testing.T) {
	f := novoProvedorFalso(t, func(_ pedidoVisto, w http.ResponseWriter) {
		saida, _ := json.Marshal(map[string]any{"itens": []string{"leite", "pão", "café"}})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "completed",
			"output": []any{map[string]any{
				"type":    "message",
				"content": []any{map[string]any{"type": "output_text", "text": string(saida)}},
			}},
		})
	})
	m := montagemDeTeste(f, "openai", "modelo-x", NovaPoliticaEsforco(nil))
	res := resolver.SepararItens(context.Background(), m.ClienteDaLista(), "leite pão e café")
	if res.Como != resolver.SeparouComIA || strings.Join(res.Itens, "|") != "Leite|Pão|Café" {
		t.Fatalf("resultado = %+v", res)
	}
	vistos := f.vistos()
	if len(vistos) != 1 || vistos[0].chave != chaveDeTeste || vistos[0].caminho != "/v1/responses" {
		t.Fatalf("pedidos = %d", len(vistos))
	}
	texto, _ := vistos[0].corpo["text"].(map[string]any)
	formato, _ := texto["format"].(map[string]any)
	if formato["name"] != resolver.NotasListaSchemaNome || vistos[0].corpo["instructions"] != resolver.NotasListaPrompt {
		t.Fatalf("o pedido não é o da lista: name=%v", formato["name"])
	}

	leu := false
	semProvedor := Montagem{Cliente: f.cliente(), Esforco: NovaPoliticaEsforco(nil), Provedor: "", Modelo: "x",
		Chave: func(context.Context) (string, error) { leu = true; return chaveDeTeste, nil }}
	if c := semProvedor.ClienteDaLista(); c != nil || leu {
		t.Fatalf("sem provedor: cliente %v, leu a chave %v", c, leu)
	}
}
