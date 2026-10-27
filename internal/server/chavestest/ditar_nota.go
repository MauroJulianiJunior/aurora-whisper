package chavestest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Os ditados que vão para uma nota (ADR-022, N8): o "Ditar para nota" do
// celular, com a regra do título do Ctrl+Win+Alt do PC, o Desfazer dele, e o
// microfone de dentro da nota aberta. Em nenhum dos casos o ditado aparece no
// histórico, em aparelho nenhum.

// notaDoDitado é a nota da resposta do ditado (só o que os casos olham).
type notaDoDitado struct {
	ID             string  `json:"id"`
	Titulo         string  `json:"titulo"`
	Corpo          string  `json:"corpo"`
	Version        int64   `json:"version"`
	DitadoDeOrigem *string `json:"ditado_de_origem"`
}

// ditadoNaNota é a resposta do ditado que foi para uma nota.
type ditadoNaNota struct {
	ID        string        `json:"id"`
	FinalText string        `json:"final_text"`
	VirouNota *string       `json:"virou_nota"`
	Nota      *notaDoDitado `json:"nota"`
	Destino   *struct {
		NotaID     string  `json:"nota_id"`
		Nova       bool    `json:"nova"`
		Linhas     int     `json:"linhas"`
		CorpoAntes *string `json:"corpo_antes"`
	} `json:"destino"`
}

func lerDitadoNaNota(t *testing.T, raw []byte) ditadoNaNota {
	t.Helper()
	var d ditadoNaNota
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("a resposta do ditado: %v\n%s", err, raw)
	}
	return d
}

// criarNotaDoCelular cria uma nota pela sessão da conta A.
func (m *mundo) criarNotaDoCelular(titulo, corpo string) notaDoDitado {
	m.t.Helper()
	rec := m.pedir(celA, http.MethodPost, "/api/v1/notes", jsonDe(m.t, map[string]any{"titulo": titulo, "corpo": corpo}))
	m.exigirStatus(rec, http.StatusCreated, "POST /notes")
	var n notaDoDitado
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil {
		m.t.Fatal(err)
	}
	return n
}

// noHistorico diz se o ditado está na lista do histórico do celular.
func (m *mundo) noHistorico(id string) bool {
	m.t.Helper()
	rec := m.pedir(celA, http.MethodGet, "/api/v1/transcriptions", "")
	m.exigirStatus(rec, http.StatusOK, "GET /transcriptions")
	return strings.Contains(rec.Body.String(), id)
}

// ditadoNoPull é o ditado como o PC o recebe, com o virou_nota.
func (m *mundo) ditadoNoPull(id string) (virouNota *string, achou bool) {
	m.t.Helper()
	rec := m.pedir(pcA, http.MethodGet, "/api/v1/sync/pull?after_seq=0", "")
	m.exigirStatus(rec, http.StatusOK, "pull")
	var p struct {
		Changes []struct {
			EntityID string          `json:"entity_id"`
			Payload  json.RawMessage `json:"payload"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		m.t.Fatal(err)
	}
	for _, c := range p.Changes {
		if c.EntityID == id {
			var it struct {
				VirouNota *string `json:"virou_nota"`
			}
			_ = json.Unmarshal(c.Payload, &it)
			return it.VirouNota, true
		}
	}
	return nil, false
}

// "lista do sábado: queijo, presunto e manteiga" entra no fim da lista de marcar da nota
// "Lista do sábado", como itens, antes dos marcados; o ditado fica ligado a ela
// e fora do histórico, aqui e no PC. O reenvio responde o mesmo destino; o
// Desfazer devolve o corpo de antes e o ditado ao histórico.
func testDitarParaNotaAcrescenta(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	lista := m.criarNotaDoCelular("Lista do sábado", "- [ ] Leite\n- [x] Pão")
	m.criarNotaDoCelular("Lista", "- [ ] outra")
	m.voz.responder("lista do sábado: queijo, presunto e manteiga", 0)
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm(), comCabecalho("Ditado-Nota", "para-nota"))
	m.exigirStatus(rec, http.StatusCreated, "ditar para nota")
	d := lerDitadoNaNota(t, rec.Body.Bytes())
	switch {
	case d.VirouNota == nil || *d.VirouNota != lista.ID:
		t.Fatalf("virou_nota = %v, quero a lista", d.VirouNota)
	case d.Destino == nil || d.Destino.NotaID != lista.ID || d.Destino.Nova || d.Destino.Linhas != 3 ||
		d.Destino.CorpoAntes == nil || *d.Destino.CorpoAntes != lista.Corpo:
		t.Fatalf("o destino = %+v", d.Destino)
	case d.Nota == nil || d.Nota.Corpo != "- [ ] Leite\n- [ ] Queijo\n- [ ] Presunto\n- [ ] Manteiga\n- [x] Pão" || d.Nota.Version != 2:
		t.Fatalf("a nota = %+v", d.Nota)
	}
	if m.noHistorico(id) {
		t.Fatal("o ditado para nota apareceu no histórico do celular")
	}
	if v, ok := m.ditadoNoPull(id); !ok || v == nil || *v != lista.ID {
		t.Fatalf("o PC recebeu o ditado com virou_nota %v (%v), quero a lista", v, ok)
	}
	// O reenvio (a resposta que se perdeu): o mesmo ditado e o mesmo destino.
	rec, _ = m.ditar(celA, id, webm(), comCabecalho("Ditado-Nota", "para-nota"))
	m.exigirStatus(rec, http.StatusOK, "o reenvio")
	if r := lerDitadoNaNota(t, rec.Body.Bytes()); r.Destino == nil || r.Destino.NotaID != lista.ID || r.Nota == nil || r.Nota.Version != 2 {
		t.Fatalf("o reenvio = %+v", r)
	}
	if n := m.voz.quantas(); n != 1 {
		t.Fatalf("%d chamadas ao provedor, quero 1 (o reenvio não paga de novo)", n)
	}

	// O Desfazer: o corpo de antes volta e o ditado entra no histórico.
	rec = m.pedir(celA, http.MethodPost, "/api/v1/transcriptions/"+id+"/undo-note",
		jsonDe(t, map[string]any{"versao": d.Nota.Version, "corpo_antes": *d.Destino.CorpoAntes}))
	m.exigirStatus(rec, http.StatusOK, "o Desfazer")
	var u struct {
		Ditado struct {
			VirouNota *string `json:"virou_nota"`
		} `json:"ditado"`
		NotaApagada bool `json:"nota_apagada"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil || u.NotaApagada || u.Ditado.VirouNota != nil {
		t.Fatalf("o Desfazer = %s", rec.Body.String())
	}
	n, err := m.b.Store.GetNote(context.Background(), m.userA, lista.ID)
	if err != nil || n.Corpo != lista.Corpo {
		t.Fatalf("a lista depois do Desfazer = %q (%v), quero %q", n.Corpo, err, lista.Corpo)
	}
	if !m.noHistorico(id) {
		t.Fatal("o ditado não voltou ao histórico")
	}
	if v, _ := m.ditadoNoPull(id); v != nil {
		t.Fatalf("o PC ainda vê o ditado na nota %v", *v)
	}
	// De novo: o ditado já não está numa nota.
	rec = m.pedir(celA, http.MethodPost, "/api/v1/transcriptions/"+id+"/undo-note", `{}`)
	m.exigirStatus(rec, http.StatusConflict, "o segundo Desfazer")
}

// Sem título no começo, nasce uma nota nova com o ditado inteiro, sem título,
// com o ditado de origem, no topo; o Desfazer a apaga (ainda na versão 1) e
// devolve o ditado ao histórico. Mexida depois, a nota fica.
func testDitarParaNotaNotaNova(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	m.criarNotaDoCelular("Mercado", "- [ ] leite")
	m.voz.responder("ligar para o contador amanhã cedo", 0)
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm(), comCabecalho("Ditado-Nota", "para-nota"))
	m.exigirStatus(rec, http.StatusCreated, "ditar para nota")
	d := lerDitadoNaNota(t, rec.Body.Bytes())
	if d.Destino == nil || !d.Destino.Nova || d.Destino.CorpoAntes != nil || d.Nota == nil || d.Nota.Titulo != "" ||
		d.Nota.Corpo != d.FinalText || d.Nota.DitadoDeOrigem == nil || *d.Nota.DitadoDeOrigem != id || d.Nota.Version != 1 {
		t.Fatalf("a nota nova = %+v %+v", d.Destino, d.Nota)
	}
	if m.noHistorico(id) {
		t.Fatal("o ditado para nota apareceu no histórico")
	}
	rec = m.pedir(celA, http.MethodPost, "/api/v1/transcriptions/"+id+"/undo-note", jsonDe(t, map[string]any{"versao": 1}))
	m.exigirStatus(rec, http.StatusOK, "o Desfazer")
	if !strings.Contains(rec.Body.String(), `"nota_apagada":true`) {
		t.Fatalf("o Desfazer não apagou a nota nova: %s", rec.Body.String())
	}
	if _, err := m.b.Store.GetNote(context.Background(), m.userA, d.Nota.ID); err == nil {
		t.Fatal("a nota nova ficou")
	}
	if !m.noHistorico(id) {
		t.Fatal("o ditado não voltou ao histórico")
	}

	// A nota nova editada depois do ditado fica no Desfazer.
	id2 := cloudstore.NewUUIDv7()
	rec, _ = m.ditar(celA, id2, webm(), comCabecalho("Ditado-Nota", "para-nota"))
	m.exigirStatus(rec, http.StatusCreated, "ditar para nota")
	d2 := lerDitadoNaNota(t, rec.Body.Bytes())
	m.exigirStatus(m.pedir(celA, http.MethodPatch, "/api/v1/notes/"+d2.Nota.ID, jsonDe(t, map[string]any{"base_version": 1, "corpo": "editada"})),
		http.StatusOK, "editar a nota nova")
	rec = m.pedir(celA, http.MethodPost, "/api/v1/transcriptions/"+id2+"/undo-note", jsonDe(t, map[string]any{"versao": 1}))
	m.exigirStatus(rec, http.StatusOK, "o Desfazer da nota editada")
	if !strings.Contains(rec.Body.String(), `"nota_apagada":false`) {
		t.Fatalf("o Desfazer apagou a nota editada: %s", rec.Body.String())
	}
	if n, err := m.b.Store.GetNote(context.Background(), m.userA, d2.Nota.ID); err != nil || n.Corpo != "editada" {
		t.Fatalf("a nota editada = %+v %v", n, err)
	}
}

// O microfone de dentro da nota aberta: o ditado fica ligado à nota (o app
// escreve o texto no cursor) e nasce fora do histórico; a nota não muda no
// servidor. O Ditado-Nota que não é "para-nota" nem um id é 400, antes do
// provedor.
func testDitarNaNotaAberta(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	n := m.criarNotaDoCelular("Reunião", "pauta")
	id := cloudstore.NewUUIDv7()
	rec, _ := m.ditar(celA, id, webm(), comCabecalho("Ditado-Nota", n.ID))
	m.exigirStatus(rec, http.StatusCreated, "ditar na nota")
	d := lerDitadoNaNota(t, rec.Body.Bytes())
	if d.VirouNota == nil || *d.VirouNota != n.ID || d.Nota == nil || d.Nota.Corpo != "pauta" || d.Nota.Version != 1 {
		t.Fatalf("o ditado na nota = %+v %+v", d, d.Nota)
	}
	if m.noHistorico(id) {
		t.Fatal("o ditado feito dentro da nota apareceu no histórico")
	}
	if v, ok := m.ditadoNoPull(id); !ok || v == nil || *v != n.ID {
		t.Fatalf("o PC recebeu o ditado com virou_nota %v", v)
	}
	antes := m.voz.quantas()
	for _, ruim := range []string{"outra-coisa", "para nota", "123"} {
		rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm(), comCabecalho("Ditado-Nota", ruim))
		m.exigirStatus(rec, http.StatusBadRequest, "Ditado-Nota "+ruim)
		if !corpo.naoLido() {
			t.Fatalf("Ditado-Nota %q: o corpo foi lido", ruim)
		}
	}
	if m.voz.quantas() != antes {
		t.Fatal("o Ditado-Nota inválido chamou o provedor")
	}
}
