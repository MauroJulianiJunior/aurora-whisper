package contracttest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Os casos das escritas do celular nas notas e nos marcadores (ADR-022, N8):
// pela sessão, com as regras da N7 (a mescla da lista de marcar, o selo, a
// lápide que não vence), o evento que leva a mudança ao PC pelo pull e o
// expurgo da lixeira das notas aos 30 dias, contra a memória e o Postgres.

// notaGravadaJSON é a nota que volta do PATCH, com a gravação.
type notaGravadaJSON struct {
	notaJSON
	Gravacao string `json:"gravacao"`
}

// celNota faz um pedido do celular A nas notas e exige o status.
func (w *world) celNota(method, path string, body any, status int) []byte {
	w.t.Helper()
	rec := w.do(method, path, celA, body)
	if rec.Code != status {
		w.t.Fatalf("%s %s: %d %s, quero %d", method, path, rec.Code, rec.Body.String(), status)
	}
	return rec.Body.Bytes()
}

// celCriaNota cria uma nota pelo celular A e a devolve.
func (w *world) celCriaNota(body map[string]any) notaJSON {
	w.t.Helper()
	return lerNota(w.t, w.celNota(http.MethodPost, "/api/v1/notes", body, http.StatusCreated))
}

// celEdita é o PATCH do celular A, lido com a gravação.
func (w *world) celEdita(id string, body map[string]any) notaGravadaJSON {
	w.t.Helper()
	raw := w.celNota(http.MethodPatch, "/api/v1/notes/"+id, body, http.StatusOK)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		w.t.Fatalf("PATCH: %v", err)
	}
	var g notaGravadaJSON
	if raw, ok := obj["gravacao"]; ok {
		_ = json.Unmarshal(raw, &g.Gravacao)
		delete(obj, "gravacao")
	}
	sem, _ := json.Marshal(obj)
	g.notaJSON = lerNota(w.t, sem)
	return g
}

// O celular cria e edita uma nota pela sessão; o PC a recebe pelo pull, com o
// evento sem aparelho. O token de um PC não escreve por aqui (403), e o texto
// nunca vai ao log.
func testNotasCelularCriaEditaEOPCRecebe(t *testing.T, w *world) {
	n := w.celCriaNota(map[string]any{"titulo": "Lista do sábado", "corpo": "- [ ] leite\n- [ ] pão", "cor": "amarelo"})
	if n.Version != 1 || n.Titulo != "Lista do sábado" || n.Cor == nil || *n.Cor != "amarelo" || n.Posicao == "" || n.DeletedAt != nil {
		t.Fatalf("a nota criada = %+v", n)
	}
	if got := w.notaNoPull(pcA1, n.ID); got.Corpo != n.Corpo || got.Version != 1 {
		t.Fatalf("o PC recebeu %+v", got)
	}
	if c, _ := mudancaDe(w.pullNotas(pcA1, 0, 0), n.ID); c.DeviceID != nil {
		t.Fatalf("a nota do celular chegou ao PC com aparelho %q", *c.DeviceID)
	}
	// A segunda nota nasce no topo das outras.
	m := w.celCriaNota(map[string]any{"titulo": "Ideias", "corpo": ""})
	if !(m.Posicao < n.Posicao) {
		t.Fatalf("a nota nova não foi para o topo: %q, a de antes %q", m.Posicao, n.Posicao)
	}
	if ns, _ := w.listarNotas(celA, "ativas"); len(ns) != 2 || ns[0].ID != m.ID {
		t.Fatalf("a lista = %v, quero a nova primeiro", idsDe(ns))
	}

	// Só o corpo: o título fica o de agora.
	g := w.celEdita(n.ID, map[string]any{"base_version": 1, "corpo": "- [ ] leite\n- [x] pão", "corpo_base": n.Corpo})
	if g.Version != 2 || g.Titulo != "Lista do sábado" || g.Corpo != "- [ ] leite\n- [x] pão" || g.Gravacao != "" {
		t.Fatalf("a edição = %+v", g)
	}
	if got := w.notaNoPull(pcA2, n.ID); got.Corpo != g.Corpo || got.Version != 2 {
		t.Fatalf("o PC recebeu a edição %+v", got)
	}
	// Fixar leva ao topo das fixadas; a cor é o último que escreve.
	g = w.celEdita(n.ID, map[string]any{"base_version": 2, "fixada": true, "cor": nil})
	if !g.Fixada || g.Cor != nil || g.Version != 3 {
		t.Fatalf("fixar e tirar a cor = %+v", g)
	}
	if got := lerNota(t, w.celNota(http.MethodGet, "/api/v1/notes/"+n.ID, nil, http.StatusOK)); got.Version != 3 || !got.Fixada {
		t.Fatalf("o GET da nota = %+v", got)
	}

	// Nada a mudar, base que o servidor nunca deu, nota vazia, campo estranho.
	w.celNota(http.MethodPatch, "/api/v1/notes/"+n.ID, map[string]any{"base_version": 3}, http.StatusBadRequest)
	w.celNota(http.MethodPatch, "/api/v1/notes/"+n.ID, map[string]any{"base_version": 9, "corpo": "x"}, http.StatusConflict)
	w.celNota(http.MethodPost, "/api/v1/notes", map[string]any{"titulo": " ", "corpo": ""}, http.StatusBadRequest)
	w.celNota(http.MethodPatch, "/api/v1/notes/"+n.ID, map[string]any{"base_version": 3, "corpos": "x"}, http.StatusBadRequest)
	w.celNota(http.MethodGet, "/api/v1/notes/"+newID(), nil, http.StatusNotFound)
	// A conta B não vê nem mexe na nota da A.
	if rec := w.do(http.MethodGet, "/api/v1/notes/"+n.ID, celB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a conta B leu a nota da A: %d", rec.Code)
	}
	if rec := w.do(http.MethodPatch, "/api/v1/notes/"+n.ID, celB, map[string]any{"base_version": 3, "corpo": "x"}); rec.Code != http.StatusNotFound {
		t.Fatalf("a conta B mexeu na nota da A: %d", rec.Code)
	}
	// O token do PC não escreve pela sessão.
	for _, rota := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/notes"}, {http.MethodPatch, "/api/v1/notes/" + n.ID},
		{http.MethodDelete, "/api/v1/notes/" + n.ID}, {http.MethodPost, "/api/v1/labels"},
	} {
		if rec := w.do(rota.method, rota.path, pcA1, map[string]any{"titulo": "x"}); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s com o token do PC: %d, quero 403", rota.method, rota.path, rec.Code)
		}
	}
	if strings.Contains(w.logs.String(), "leite") || strings.Contains(w.logs.String(), "sábado") {
		t.Fatal("o texto da nota foi ao log")
	}
}

// A volta da rede (E4): o celular marca sobre uma base velha com o corpo da
// base, e o PC marcou outro item: junta, sem selo, e a resposta diz que
// mesclou. Os dois mudando o texto: o do celular vai para o selo, e a escolha
// do selo pelo celular limpa.
func testNotasCelularMesclaESelo(t *testing.T, w *world) {
	n := newID()
	base := "- [ ] leite\n- [ ] pão\n- [ ] ovos"
	w.mustNota(pcA1, criarNota(n, map[string]any{"corpo": base}), 1, false)
	// O PC marca o leite.
	w.mustNota(pcA1, mudarNota(n, 1, map[string]any{"titulo": "Mercado", "corpo": "- [ ] pão\n- [ ] ovos\n- [x] leite", "corpo_base": base}), 2, false)
	// O celular, sem ter visto (base 1), marca o pão.
	g := w.celEdita(n, map[string]any{"base_version": 1, "titulo": "Mercado", "corpo": "- [ ] leite\n- [ ] ovos\n- [x] pão", "corpo_base": base})
	if g.Gravacao != "mesclada" || g.Version != 3 || g.VersaoEmConflito != nil || !strings.Contains(g.Corpo, "- [x] leite") || !strings.Contains(g.Corpo, "- [x] pão") {
		t.Fatalf("a mescla = %+v", g)
	}
	if got := w.notaNoPull(pcA1, n); got.Corpo != g.Corpo {
		t.Fatalf("o PC recebeu %q, quero o mesclado %q", got.Corpo, g.Corpo)
	}

	// O celular acrescenta um item sobre a v3; o PC troca um texto sobre a v3
	// antes: os dois mudaram o texto, e o do celular vai para o selo.
	w.mustNota(pcA1, mudarNota(n, 3, map[string]any{"titulo": "Mercado", "corpo": strings.Replace(g.Corpo, "ovos", "ovos caipira", 1), "corpo_base": g.Corpo}), 4, false)
	meu := g.Corpo + "\n- [ ] manteiga"
	s := w.celEdita(n, map[string]any{"base_version": 3, "titulo": "Mercado", "corpo": meu, "corpo_base": g.Corpo})
	if s.Gravacao != "no_selo" || s.VersaoEmConflito == nil || s.VersaoEmConflito.Corpo != meu || s.VersaoEmConflito.DeviceID != nil ||
		!strings.Contains(s.Corpo, "ovos caipira") || s.Version != 5 {
		t.Fatalf("o selo = %+v %+v", s, s.VersaoEmConflito)
	}
	if got := w.notaNoPull(pcA1, n); got.VersaoEmConflito == nil || got.VersaoEmConflito.Corpo != meu {
		t.Fatalf("o PC não recebeu o selo: %+v", got)
	}
	// Sem corpo_base, sem mescla: só marcar sobre a base velha também é selo.
	// A escolha sobre outra versão é 409; sobre a de agora, limpa.
	w.celNota(http.MethodPatch, "/api/v1/notes/"+n, map[string]any{"base_version": 4, "resolver_conflito": "trocar"}, http.StatusConflict)
	w.celNota(http.MethodPatch, "/api/v1/notes/"+n, map[string]any{"base_version": 5, "resolver_conflito": "outra"}, http.StatusBadRequest)
	esc := w.celEdita(n, map[string]any{"base_version": 5, "resolver_conflito": "trocar"})
	if esc.VersaoEmConflito != nil || esc.Corpo != meu || esc.Version != 6 {
		t.Fatalf("depois de trocar = %+v", esc)
	}
	w.celNota(http.MethodPatch, "/api/v1/notes/"+n, map[string]any{"base_version": 6, "resolver_conflito": "manter"}, http.StatusConflict)
	if got := w.notaNoPull(pcA2, n); got.VersaoEmConflito != nil || got.Corpo != meu {
		t.Fatalf("o PC depois da escolha = %+v", got)
	}

	// Reenviar a mesma mudança (a resposta que se perdeu) não vira selo: o
	// conteúdo já é o mandado.
	if again := w.celEdita(n, map[string]any{"base_version": 5, "titulo": "Mercado", "corpo": meu, "corpo_base": g.Corpo}); again.VersaoEmConflito != nil || again.Gravacao != "" {
		t.Fatalf("o reenvio virou selo: %+v", again)
	}
}

// A lixeira, restaurar (no topo do grupo), apagar de vez (só da lixeira, com a
// lápide que o PC recebe e que nunca vence) e o expurgo de 30 dias.
func testNotasCelularLixeira(t *testing.T, w *world) {
	a := w.celCriaNota(map[string]any{"titulo": "A", "corpo": "um"})
	b := w.celCriaNota(map[string]any{"titulo": "B", "corpo": "dois"})
	w.celNota(http.MethodDelete, "/api/v1/notes/"+a.ID, nil, http.StatusNoContent)
	w.celNota(http.MethodDelete, "/api/v1/notes/"+a.ID, nil, http.StatusNoContent) // de novo: nada muda
	if got := w.notaNoPull(pcA1, a.ID); got.DeletedAt == nil || got.Version != 2 {
		t.Fatalf("a lixeira no PC = %+v", got)
	}
	if ns, _ := w.listarNotas(celA, "lixeira"); len(ns) != 1 || ns[0].ID != a.ID {
		t.Fatalf("a lixeira = %v", idsDe(ns))
	}
	// Mexer na nota da lixeira: marcar e mover recusam.
	w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/move", map[string]any{"antes": nil, "depois": b.ID}, http.StatusConflict)
	r := lerNota(t, w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/restore", nil, http.StatusOK))
	if r.DeletedAt != nil || !(r.Posicao < b.Posicao) {
		t.Fatalf("restaurar = %+v, quero fora da lixeira e no topo (antes de %q)", r, b.Posicao)
	}
	// Apagar de vez só da lixeira.
	w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/purge", nil, http.StatusConflict)
	w.celNota(http.MethodDelete, "/api/v1/notes/"+a.ID, nil, http.StatusNoContent)
	w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/purge", nil, http.StatusNoContent)
	if c, ok := mudancaDe(w.pullNotas(pcA1, 0, 0), a.ID); !ok || c.Operation != "tombstone" {
		t.Fatalf("a lápide no pull = %+v %v", c, ok)
	}
	w.celNota(http.MethodGet, "/api/v1/notes/"+a.ID, nil, http.StatusNotFound)
	// O PC que mandar a nota de novo recebe gone: a lápide não vence.
	if res := w.pushNota(pcA2, criarNota(a.ID, nil)); res.Status != "gone" {
		t.Fatalf("o create atrasado = %+v, quero gone", res)
	}

	// O expurgo: a nota na lixeira há 30 dias sai de vez, com a lápide e sem
	// aparelho; a que voltou da lixeira fica.
	w.celNota(http.MethodDelete, "/api/v1/notes/"+b.ID, nil, http.StatusNoContent)
	c := w.celCriaNota(map[string]any{"titulo": "C", "corpo": "três"})
	w.celNota(http.MethodDelete, "/api/v1/notes/"+c.ID, nil, http.StatusNoContent)
	w.celNota(http.MethodPost, "/api/v1/notes/"+c.ID+"/restore", nil, http.StatusOK)
	w.wantRetido(w.userA, cloudstore.Retido{})
	w.andar(w.clk.Now().AddDate(0, 0, 30).Add(time.Minute))
	got := w.reter(w.userA)
	if got.Notas != 1 {
		t.Fatalf("o expurgo = %+v, quero 1 nota", got)
	}
	if c, ok := mudancaDe(w.pullNotas(pcA1, 0, 0), b.ID); !ok || c.Operation != "tombstone" {
		t.Fatalf("a lápide do expurgo = %+v %v", c, ok)
	}
	// A sessão do celular venceu nos 30 dias sem uso: quem confere é o Store.
	if _, err := w.b.Store.GetNote(context.Background(), w.userA, c.ID); err != nil {
		t.Fatalf("a nota restaurada saiu no expurgo: %v", err)
	}
	if _, err := w.b.Store.GetNote(context.Background(), w.userA, b.ID); err == nil {
		t.Fatal("a nota vencida ficou")
	}
	// De novo: nada.
	if again := w.reter(w.userA); again.Notas != 0 {
		t.Fatalf("o segundo expurgo apagou %d notas", again.Notas)
	}
}

// O mover (a ordem manual) e os marcadores pelo celular.
func testNotasCelularMoverEMarcadores(t *testing.T, w *world) {
	c := w.celCriaNota(map[string]any{"titulo": "C", "corpo": "x"})
	b := w.celCriaNota(map[string]any{"titulo": "B", "corpo": "x"})
	a := w.celCriaNota(map[string]any{"titulo": "A", "corpo": "x"})
	// A ordem é A, B, C. A vai para depois de C.
	m := lerNota(t, w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/move", map[string]any{"antes": c.ID, "depois": nil}, http.StatusOK))
	if !(m.Posicao > c.Posicao) || m.Version != 2 {
		t.Fatalf("mover = %+v", m)
	}
	if ns, _ := w.listarNotas(celA, "ativas"); strings.Join(idsDe(ns), ",") != strings.Join([]string{b.ID, c.ID, a.ID}, ",") {
		t.Fatal("a ordem depois de mover não é B, C, A")
	}
	if got := w.notaNoPull(pcA1, a.ID); got.Posicao != m.Posicao {
		t.Fatalf("o PC recebeu a posição %q, quero %q", got.Posicao, m.Posicao)
	}
	// Já está entre as duas: nada muda.
	if again := lerNota(t, w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/move", map[string]any{"antes": c.ID}, http.StatusOK)); again.Version != 2 {
		t.Fatalf("mover para o mesmo lugar gravou: %+v", again)
	}
	// Sem vizinha, a própria nota, fora de ordem, de outro grupo.
	w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/move", map[string]any{}, http.StatusBadRequest)
	w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/move", map[string]any{"antes": a.ID}, http.StatusConflict)
	w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/move", map[string]any{"antes": c.ID, "depois": b.ID}, http.StatusConflict)
	w.celEdita(c.ID, map[string]any{"base_version": 1, "fixada": true})
	w.celNota(http.MethodPost, "/api/v1/notes/"+a.ID+"/move", map[string]any{"depois": c.ID}, http.StatusConflict)

	// Os marcadores: criar (nome único, sem caixa nem acento), marcar, renomear e apagar.
	l := lerMarcador(t, w.celNota(http.MethodPost, "/api/v1/labels", map[string]any{"nome": "  Mercado  "}, http.StatusCreated))
	if l.Nome != "Mercado" || l.Version != 1 {
		t.Fatalf("o marcador = %+v", l)
	}
	rec := w.do(http.MethodPost, "/api/v1/labels", celA, map[string]any{"nome": "mércado"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "NAME_IN_USE") {
		t.Fatalf("o nome repetido: %d %s", rec.Code, rec.Body.String())
	}
	w.celNota(http.MethodPost, "/api/v1/labels", map[string]any{"nome": ""}, http.StatusBadRequest)
	marcada := lerNota(t, w.celNota(http.MethodPut, "/api/v1/notes/"+b.ID+"/labels/"+l.ID, nil, http.StatusOK))
	if len(marcada.Marcadores) != 1 || marcada.Marcadores[0] != l.ID {
		t.Fatalf("marcar = %+v", marcada)
	}
	if again := lerNota(t, w.celNota(http.MethodPut, "/api/v1/notes/"+b.ID+"/labels/"+l.ID, nil, http.StatusOK)); again.Version != marcada.Version {
		t.Fatal("marcar de novo gravou")
	}
	w.celNota(http.MethodPut, "/api/v1/notes/"+b.ID+"/labels/"+newID(), nil, http.StatusNotFound)
	if got := w.notaNoPull(pcA1, b.ID); len(got.Marcadores) != 1 {
		t.Fatalf("o PC recebeu a nota sem o marcador: %+v", got)
	}
	ren := lerMarcador(t, w.celNota(http.MethodPatch, "/api/v1/labels/"+l.ID, map[string]any{"base_version": 1, "nome": "Compras"}, http.StatusOK))
	if ren.Nome != "Compras" || ren.Version != 2 {
		t.Fatalf("renomear = %+v", ren)
	}
	outro := lerMarcador(t, w.celNota(http.MethodPost, "/api/v1/labels", map[string]any{"nome": "Casa"}, http.StatusCreated))
	if rec := w.do(http.MethodPatch, "/api/v1/labels/"+outro.ID, celA, map[string]any{"base_version": 1, "nome": "COMPRAS"}); rec.Code != http.StatusConflict {
		t.Fatalf("renomear para o nome de outro: %d", rec.Code)
	}
	desm := lerNota(t, w.celNota(http.MethodDelete, "/api/v1/notes/"+b.ID+"/labels/"+l.ID, nil, http.StatusOK))
	if len(desm.Marcadores) != 0 {
		t.Fatalf("desmarcar = %+v", desm)
	}
	w.celNota(http.MethodPut, "/api/v1/notes/"+b.ID+"/labels/"+l.ID, nil, http.StatusOK)
	w.celNota(http.MethodDelete, "/api/v1/labels/"+l.ID, nil, http.StatusNoContent)
	w.celNota(http.MethodDelete, "/api/v1/labels/"+l.ID, nil, http.StatusNotFound)
	if got := lerNota(t, w.celNota(http.MethodGet, "/api/v1/notes/"+b.ID, nil, http.StatusOK)); len(got.Marcadores) != 0 {
		t.Fatalf("o marcador apagado ficou na nota: %+v", got)
	}
	if c, ok := mudancaDe(w.pullNotas(pcA1, 0, 0), l.ID); !ok || c.Operation != "tombstone" || c.EntityType != "label" {
		t.Fatalf("a lápide do marcador no pull = %+v %v", c, ok)
	}
	if ms, _ := w.listarMarcadores(celA); len(ms) != 1 || ms[0].ID != outro.ID {
		t.Fatalf("os marcadores = %+v", ms)
	}
	// Os eventos: todos sem aparelho (o celular), e nenhum seq repetido.
	vistos := map[int64]bool{}
	for _, e := range w.b.NoteEvents(t, w.userA) {
		if vistos[e.Seq] {
			t.Fatalf("o seq %d repetiu", e.Seq)
		}
		vistos[e.Seq] = true
		if e.DeviceID != nil {
			t.Fatalf("um evento do celular com aparelho: %+v", e)
		}
	}
}
