package localapi_test

import (
	"net/http"
	"testing"
)

// A ordem manual pela API (ADR-022, N2b): POST /api/notas/{id}/mover.

func titulosDaLista(t *testing.T, h http.Handler, url string) []string {
	t.Helper()
	var out []string
	for _, n := range lerLista(t, fazer(t, h, "GET", url, nil)) {
		out = append(out, n.Titulo)
	}
	return out
}

func iguais(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNotasAPI_Mover(t *testing.T) {
	h, _ := servidorDeNotas(t)
	a := criarNotaAPI(t, h, map[string]any{"titulo": "a", "corpo": "x"})
	b := criarNotaAPI(t, h, map[string]any{"titulo": "b", "corpo": "x"})
	c := criarNotaAPI(t, h, map[string]any{"titulo": "c", "corpo": "x"})
	if a.Posicao == "" || !(c.Posicao < b.Posicao && b.Posicao < a.Posicao) {
		t.Fatalf("a nota nova devia ficar acima: a=%q b=%q c=%q", a.Posicao, b.Posicao, c.Posicao)
	}
	if got := titulosDaLista(t, h, "/api/notas"); !iguais(got, []string{"c", "b", "a"}) {
		t.Fatalf("lista inicial: %v", got)
	}

	// a para o topo: antes null, depois = c. Devolve a nota, com a versão nova.
	w := fazer(t, h, "POST", "/api/notas/"+a.ID+"/mover", map[string]any{"antes": nil, "depois": c.ID})
	esperar(t, w, 200)
	movida := lerNotaJSON(t, w)
	if movida.ID != a.ID || movida.Version != 2 || movida.UpdatedAt != a.UpdatedAt || !(movida.Posicao < c.Posicao) {
		t.Errorf("nota movida: %+v", movida)
	}
	if got := titulosDaLista(t, h, "/api/notas"); !iguais(got, []string{"a", "c", "b"}) {
		t.Errorf("depois de mover para o topo: %v", got)
	}

	// c para o fim: "antes" é b, "depois" ausente.
	esperar(t, fazer(t, h, "POST", "/api/notas/"+c.ID+"/mover", `{"antes":"`+b.ID+`"}`), 200)
	if got := titulosDaLista(t, h, "/api/notas"); !iguais(got, []string{"a", "b", "c"}) {
		t.Errorf("depois de mover para o fim: %v", got)
	}

	// Mover não pede base_version e não troca o corpo da nota.
	if lida := lerNotaJSON(t, fazer(t, h, "GET", "/api/notas/"+c.ID, nil)); lida.Corpo != "x" || lida.Titulo != "c" {
		t.Errorf("mover mexeu no texto: %+v", lida)
	}

	// Editar depois de mover usa a versão que o mover devolveu, e não move.
	novaVersao := lerNotaJSON(t, fazer(t, h, "GET", "/api/notas/"+c.ID, nil)).Version
	esperar(t, fazer(t, h, "PATCH", "/api/notas/"+c.ID, map[string]any{"base_version": novaVersao, "corpo": "y"}), 200)
	if got := titulosDaLista(t, h, "/api/notas"); !iguais(got, []string{"a", "b", "c"}) {
		t.Errorf("editar moveu: %v", got)
	}
}

func TestNotasAPI_MoverRecusa(t *testing.T) {
	h, _ := servidorDeNotas(t)
	a := criarNotaAPI(t, h, map[string]any{"titulo": "a", "corpo": "x"})
	b := criarNotaAPI(t, h, map[string]any{"titulo": "b", "corpo": "x"})
	fixa := criarNotaAPI(t, h, map[string]any{"titulo": "fixa", "corpo": "x", "fixada": true})
	arq := criarNotaAPI(t, h, map[string]any{"titulo": "arq", "corpo": "x", "arquivada": true})
	lix := criarNotaAPI(t, h, map[string]any{"titulo": "lix", "corpo": "x"})
	esperar(t, fazer(t, h, "DELETE", "/api/notas/"+lix.ID, nil), 204)

	casos := []struct {
		nome   string
		id     string
		corpo  any
		status int
		code   string
	}{
		{"vizinha de outro grupo (fixada)", a.ID, map[string]any{"antes": fixa.ID}, 409, "CONFLICT"},
		{"vizinha no Arquivo", a.ID, map[string]any{"depois": arq.ID}, 409, "CONFLICT"},
		{"vizinha na lixeira", a.ID, map[string]any{"antes": lix.ID}, 409, "CONFLICT"},
		{"vizinha que não existe", a.ID, map[string]any{"antes": "nao-existe"}, 409, "CONFLICT"},
		{"a própria nota", a.ID, map[string]any{"antes": a.ID}, 409, "CONFLICT"},
		{"sem vizinha", a.ID, map[string]any{}, 400, "BAD_REQUEST"},
		{"as duas nulas", a.ID, map[string]any{"antes": nil, "depois": nil}, 400, "BAD_REQUEST"},
		{"campo desconhecido", a.ID, `{"depois":"` + b.ID + `","base_version":1}`, 400, "BAD_REQUEST"},
		{"json inválido", a.ID, "[", 400, "BAD_REQUEST"},
		{"a nota não existe", "nao-existe", map[string]any{"depois": b.ID}, 404, "NOT_FOUND"},
		{"a nota está na lixeira", lix.ID, map[string]any{"depois": b.ID}, 409, "CONFLICT"},
	}
	for _, c := range casos {
		esperarErro(t, fazer(t, h, "POST", "/api/notas/"+c.id+"/mover", c.corpo), c.status, c.code)
	}
	// Nada mudou.
	if got := titulosDaLista(t, h, "/api/notas"); !iguais(got, []string{"fixa", "b", "a"}) {
		t.Errorf("as recusas mexeram na lista: %v", got)
	}
}
