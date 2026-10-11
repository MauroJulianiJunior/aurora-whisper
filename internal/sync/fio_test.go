package syncengine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// O fio por tipo (ADR-019, "Decisões da F8", F8f1): o payload do pull e o
// state do conflict lidos pelo entity_type. O ditado lê exatamente como na
// F7 (a leitura direta, o json.Unmarshal no ChangePage e no PushResult); o
// aprendido lê no Aprendido. Nada aqui toca banco ou sistema: a nuvem, quando
// há, é um httptest.

const (
	idDoPar      = "6f1b2c3d-4e5f-5a6b-8c7d-9e0f1a2b3c4d"
	idDoOutroPar = "6f1b2c3d-4e5f-5a6b-8c7d-9e0f1a2b3c5e"
	mutacao1     = "0199a000-0000-7000-8000-000000000001"
	mutacao2     = "0199a000-0000-7000-8000-000000000002"
	mutacao3     = "0199a000-0000-7000-8000-000000000003"
)

// itemDoFio é um ditado inteiro, como o servidor o escreve (todos os campos,
// com o selo).
const itemDoFio = `{"id":"` + idDoTeste + `","device_id":"0199a000-0000-7000-8000-0000000000c1",` +
	`"source_platform":"windows","raw_text":"bruto","final_text":"venceu","mode":"smart","language":"pt",` +
	`"asr_provider":"whisper-local","asr_model":"m","duration_ms":1200,"processing_ms":300,"confidence":0.9,` +
	`"status":"done","created_at":"2026-10-06T09:00:00Z","updated_at":"2026-10-06T10:00:00.123456Z",` +
	`"deleted_at":null,"version":4,"versao_em_conflito":{"final_text":"perdeu","device_id":null,` +
	`"conflict_at":"2026-10-06T10:00:00Z"}}`

// parDoFio é um aprendido ativo, preso a um domínio e restrito, como o
// servidor o escreve (o learnedDTO: as seis chaves, domain e undone_at
// sempre presentes).
const parDoFio = `{"id":"` + idDoPar + `","variant":"nfe","term":"NF-e","restricted":true,"domain":"fiscal","undone_at":null}`

// parDesfeitoDoFio é um aprendido desfeito, sem domínio.
const parDesfeitoDoFio = `{"id":"` + idDoOutroPar + `","variant":"sf","term":"ECF","restricted":false,"domain":null,` +
	`"undone_at":"2026-10-07T12:00:00.5Z"}`

func paginaDireta(b []byte) (ChangePage, error) {
	var p ChangePage
	err := json.Unmarshal(b, &p)
	return p, err
}

func paginaPeloTipo(b []byte) (ChangePage, error) {
	var w paginaDoFio
	if err := json.Unmarshal(b, &w); err != nil {
		return ChangePage{}, err
	}
	return w.pagina()
}

func respostaDireta(b []byte) (PushResult, error) {
	var r PushResult
	err := json.Unmarshal(b, &r)
	return r, err
}

func respostaPeloTipo(b []byte, muts []Mutation) (PushResult, error) {
	var w respostaDoPush
	if err := json.Unmarshal(b, &w); err != nil {
		return PushResult{}, err
	}
	return w.resultado(muts)
}

// mudanca é uma linha do pull com o resto dado.
func mudanca(resto string) string {
	return `{"seq":7,"entity_type":"transcription","entity_id":"` + idDoTeste + `","operation":"upsert",` +
		`"entity_version":4,"device_id":"0199a000-0000-7000-8000-0000000000c1"` + resto + `}`
}

// pagina é uma página com as mudanças dadas.
func pagina(mudancas ...string) string {
	return `{"changes":[` + strings.Join(mudancas, ",") + `],"last_seq":9,"has_more":true,"sync_epoch":"e1"}`
}

// O pull sem aprendido lê pelo tipo exatamente como a F7 o lia direto, para
// qualquer JSON: o ditado, a lápide, o tipo que o PC não conhece (que fica no
// Item, como na F7), o payload null, vazio, repetido (a chave repetida
// decodifica por cima da anterior, no mesmo Item), a página repetida, as
// chaves em outra caixa, e os erros (os dois lados erram juntos).
func TestLeituraPeloTipo_OPullDoDitadoComoNaF7(t *testing.T) {
	casos := map[string]string{
		"ditado inteiro": pagina(mudanca(`,"payload":` + itemDoFio)),
		"lápide": pagina(`{"seq":8,"entity_type":"transcription","entity_id":"` + idDoTeste +
			`","operation":"tombstone"}`),
		"payload null":  pagina(mudanca(`,"payload":null`)),
		"payload vazio": pagina(mudanca(`,"payload":{}`)),
		"sem payload":   pagina(mudanca(``)),
		"payload repetido, que se junta": pagina(mudanca(`,"payload":{"id":"` + idDoTeste +
			`","final_text":"um"},"payload":{"raw_text":"dois","version":5}`)),
		"payload repetido, com null no meio": pagina(mudanca(`,"payload":{"final_text":"um"},"payload":null,` +
			`"payload":{"raw_text":"dois"}`)),
		"payload repetido, com null no fim": pagina(mudanca(`,"payload":{"final_text":"um"},"payload":null`)),
		"tipo desconhecido, no Item como na F7": pagina(`{"seq":9,"entity_type":"settings","entity_id":"` + idDoTeste +
			`","operation":"upsert","entity_version":1,"payload":{"id":"x","final_text":"y","tema":"escuro"}}`),
		"Learned em outra caixa não é o aprendido": pagina(`{"seq":9,"entity_type":"Learned","entity_id":"` + idDoPar +
			`","operation":"upsert","entity_version":1,"payload":` + parDoFio + `}`),
		"entity_type repetido, vale o último": pagina(`{"entity_type":"learned","seq":3,"entity_type":"transcription",` +
			`"payload":` + itemDoFio + `}`),
		"chaves em outra caixa": pagina(`{"Seq":3,"Entity_Type":"transcription","ENTITY_ID":"` + idDoTeste +
			`","PAYLOAD":` + itemDoFio + `}`),
		"campo a mais":    pagina(mudanca(`,"x":1,"payload":` + itemDoFio)),
		"mudança null":    pagina(`null`, mudanca(`,"payload":`+itemDoFio)),
		"várias":          pagina(mudanca(`,"payload":`+itemDoFio), mudanca(`,"payload":null`), mudanca(``)),
		"changes null":    `{"changes":null,"last_seq":0,"sync_epoch":"e1"}`,
		"changes vazio":   `{"changes":[],"last_seq":0,"sync_epoch":"e1"}`,
		"sem changes":     `{"last_seq":0,"sync_epoch":"e1"}`,
		"a resposta null": `null`,
		"changes repetido, que se junta": `{"changes":[` + mudanca(`,"payload":`+itemDoFio) + `,` + mudanca(``) +
			`],"changes":[{"seq":12,"payload":{"raw_text":"outro"}}],"sync_epoch":"e1"}`,
		// Os erros.
		"payload com tipo errado":      pagina(mudanca(`,"payload":{"version":"quatro"}`)),
		"payload que não é objeto":     pagina(mudanca(`,"payload":"x"`)),
		"payload lista":                pagina(mudanca(`,"payload":[1]`)),
		"tipo desconhecido que colide": pagina(`{"entity_type":"settings","payload":{"status":5}}`),
		"seq com tipo errado":          pagina(`{"seq":"1"}`),
		"JSON quebrado":                `{"changes":[`,
		"o erro depois de um ok":       pagina(mudanca(`,"payload":`+itemDoFio), mudanca(`,"payload":{"version":"x"}`)),
	}
	erros := 0
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			direta, errDireta := paginaDireta([]byte(corpo))
			peloTipo, errPeloTipo := paginaPeloTipo([]byte(corpo))
			if (errDireta == nil) != (errPeloTipo == nil) {
				t.Fatalf("a F7 errou %v; pelo tipo, %v", errDireta, errPeloTipo)
			}
			if errDireta != nil {
				erros++
				return
			}
			if !reflect.DeepEqual(direta, peloTipo) {
				t.Errorf("pelo tipo é outro:\nF7:       %+v\npelo tipo: %+v", direta, peloTipo)
			}
			for _, c := range peloTipo.Changes {
				if c.PayloadAprendido != nil {
					t.Errorf("o ditado ganhou um aprendido: %+v", c)
				}
			}
		})
	}
	// Os casos de erro erraram de verdade (senão a comparação não prova nada).
	if erros != 7 {
		t.Errorf("%d casos de erro; quero 7", erros)
	}
	// E a junção da chave repetida aconteceu (o corpus prova o que diz).
	p, _ := paginaPeloTipo([]byte(casos["payload repetido, que se junta"]))
	if it := p.Changes[0].Payload; it == nil || it.FinalText != "um" || it.RawText != "dois" || it.Version != 5 {
		t.Errorf("a chave repetida não se juntou: %+v", it)
	}
}

// muts são mutações do tipo dado, uma por id, na ordem.
func muts(tipos ...string) []Mutation {
	ids := []string{mutacao1, mutacao2, mutacao3}
	out := make([]Mutation, len(tipos))
	for i, tipo := range tipos {
		out[i] = Mutation{MutationID: ids[i], EntityType: tipo, EntityID: idDoTeste, Operation: OpUpsert}
	}
	return out
}

// resposta é uma resposta do push com os resultados dados.
func resposta(resultados ...string) string {
	return `{"results":[` + strings.Join(resultados, ",") + `],"sync_epoch":"e1"}`
}

// O push só de ditados lê pelo tipo exatamente como a F7 lia direto, para
// qualquer JSON: o state do conflict, null, repetido, o resultado a mais que
// o lote (fica no Item, como na F7), e os erros.
func TestLeituraPeloTipo_OPushDoDitadoComoNaF7(t *testing.T) {
	dois := muts(storage.EntidadeDitado, storage.EntidadeDitado)
	conflito := `{"mutation_id":"` + mutacao1 + `","status":"conflict","entity_version":5,"state":` + itemDoFio + `}`
	casos := map[string]string{
		"conflict e applied": resposta(conflito, `{"mutation_id":"`+mutacao2+`","status":"applied","entity_version":2,"mesclado":true}`),
		"duplicate e rejected": resposta(`{"mutation_id":"`+mutacao1+`","status":"duplicate","original":"conflict","entity_version":5}`,
			`{"mutation_id":"`+mutacao2+`","status":"rejected","code":"campo_invalido"}`),
		"gone":                         resposta(`{"mutation_id":"` + mutacao1 + `","status":"gone","motivo":"apagado"}`),
		"state null":                   resposta(`{"mutation_id":"` + mutacao1 + `","status":"conflict","state":null}`),
		"state vazio":                  resposta(`{"mutation_id":"` + mutacao1 + `","status":"conflict","state":{}}`),
		"state repetido, que se junta": resposta(`{"mutation_id":"` + mutacao1 + `","state":{"final_text":"um"},"state":{"version":7}}`),
		"state repetido, null no fim":  resposta(`{"mutation_id":"` + mutacao1 + `","state":{"final_text":"um"},"state":null}`),
		"resultado a mais que o lote":  resposta(conflito, conflito, conflito),
		"results null":                 `{"results":null,"sync_epoch":"e1"}`,
		"results vazio":                `{"results":[],"sync_epoch":"e1"}`,
		"sem results":                  `{"sync_epoch":"e1"}`,
		"results repetido, que se junta": `{"results":[` + conflito + `],"results":[{"state":{"raw_text":"novo"}}],` +
			`"sync_epoch":"e1"}`,
		// Os erros.
		"state com tipo errado":  resposta(`{"mutation_id":"` + mutacao1 + `","state":{"version":"x"}}`),
		"state que não é objeto": resposta(`{"mutation_id":"` + mutacao1 + `","state":"x"}`),
		"status com tipo errado": resposta(`{"mutation_id":"` + mutacao1 + `","status":3}`),
		"JSON quebrado":          `{"results":[`,
	}
	erros := 0
	for nome, corpo := range casos {
		t.Run(nome, func(t *testing.T) {
			direta, errDireta := respostaDireta([]byte(corpo))
			peloTipo, errPeloTipo := respostaPeloTipo([]byte(corpo), dois)
			if (errDireta == nil) != (errPeloTipo == nil) {
				t.Fatalf("a F7 errou %v; pelo tipo, %v", errDireta, errPeloTipo)
			}
			if errDireta != nil {
				erros++
				return
			}
			if !reflect.DeepEqual(direta, peloTipo) {
				t.Errorf("pelo tipo é outro:\nF7:       %+v\npelo tipo: %+v", direta, peloTipo)
			}
			for _, r := range peloTipo.Results {
				if r.StateAprendido != nil {
					t.Errorf("o ditado ganhou um aprendido: %+v", r)
				}
			}
		})
	}
	if erros != 4 {
		t.Errorf("%d casos de erro; quero 4", erros)
	}
	r, _ := respostaPeloTipo([]byte(casos["state repetido, que se junta"]), dois)
	if st := r.Results[0].State; st == nil || st.FinalText != "um" || st.Version != 7 {
		t.Errorf("a chave repetida não se juntou: %+v", st)
	}
}

func texto(s string) *string { return &s }

// O aprendido do pull lê no Aprendido (o ativo, com o domínio e a restrição,
// e o desfeito, sem domínio), sem passar pelo Item; o ditado da mesma página
// lê como sempre; e o motor continua contando o aprendido como "não
// aplicado" (a F8f2 é que decide o que fazer com ele).
func TestLeituraPeloTipo_OAprendidoDoPull(t *testing.T) {
	ditado := mudanca(`,"payload":` + itemDoFio)
	corpo := pagina(ditado,
		`{"seq":8,"entity_type":"learned","entity_id":"`+idDoPar+`","operation":"upsert","entity_version":1,`+
			`"device_id":"0199a000-0000-7000-8000-0000000000c1","payload":`+parDoFio+`}`,
		`{"seq":9,"entity_type":"learned","entity_id":"`+idDoOutroPar+`","operation":"upsert","entity_version":3,`+
			`"payload":`+parDesfeitoDoFio+`}`)
	p, err := paginaPeloTipo([]byte(corpo))
	if err != nil || len(p.Changes) != 3 || p.LastSeq != 9 || !p.HasMore || p.Epoch != "e1" {
		t.Fatalf("a página: %+v, %v", p, err)
	}
	soDitado, _ := paginaDireta([]byte(pagina(ditado)))
	if !reflect.DeepEqual(p.Changes[0], soDitado.Changes[0]) {
		t.Errorf("o ditado ao lado do aprendido: %+v", p.Changes[0])
	}
	desfeito := time.Date(2026, 10, 7, 12, 0, 0, 500_000_000, time.UTC)
	quero := []Aprendido{
		{ID: idDoPar, Variant: "nfe", Term: "NF-e", Restricted: true, Domain: texto("fiscal")},
		{ID: idDoOutroPar, Variant: "sf", Term: "ECF", UndoneAt: &desfeito},
	}
	for i, q := range quero {
		c := p.Changes[i+1]
		if c.Payload != nil || c.PayloadAprendido == nil {
			t.Fatalf("o aprendido %d: Payload %+v, PayloadAprendido %+v", i, c.Payload, c.PayloadAprendido)
		}
		got := *c.PayloadAprendido
		if got.UndoneAt != nil && q.UndoneAt != nil && got.UndoneAt.Equal(*q.UndoneAt) {
			got.UndoneAt = q.UndoneAt
		}
		if !reflect.DeepEqual(got, q) {
			t.Errorf("o aprendido %d = %+v; quero %+v", i, got, q)
		}
		if d := DecidirMudanca(storage.EstadoDoItem{ID: c.EntityID}, c); d.Tipo != TipoNaoAplicado || !reflect.DeepEqual(d.Acao, storage.AcaoLocal{}) {
			t.Errorf("o motor decidiu %+v para o aprendido %d", d, i)
		}
	}

	// Pelo tipo, e não pelo Item: o aprendido com um campo que o Item não lê
	// (version como texto) passa, e a leitura da F7 erraria.
	estranho := pagina(`{"seq":8,"entity_type":"learned","entity_id":"` + idDoPar + `","operation":"upsert",` +
		`"entity_version":1,"payload":{"id":"` + idDoPar + `","variant":"nfe","term":"NF-e","restricted":false,` +
		`"domain":null,"undone_at":null,"version":"x"}}`)
	if _, err := paginaDireta([]byte(estranho)); err == nil {
		t.Fatal("o caso não prova nada: a leitura da F7 não errou")
	}
	if p, err := paginaPeloTipo([]byte(estranho)); err != nil || p.Changes[0].PayloadAprendido == nil {
		t.Errorf("o aprendido passou pelo Item: %+v, %v", p, err)
	}

	// O aprendido ilegível no tipo dele é erro, como o ditado ilegível; o
	// null é nil.
	for _, payload := range []string{`{"restricted":"sim"}`, `{"undone_at":"ontem"}`, `"nfe"`} {
		ruim := pagina(`{"entity_type":"learned","entity_id":"` + idDoPar + `","payload":` + payload + `}`)
		if _, err := paginaPeloTipo([]byte(ruim)); err == nil {
			t.Errorf("o payload %s foi lido", payload)
		}
	}
	p, err = paginaPeloTipo([]byte(pagina(`{"entity_type":"learned","entity_id":"` + idDoPar + `","payload":null}`)))
	if err != nil || p.Changes[0].Payload != nil || p.Changes[0].PayloadAprendido != nil {
		t.Errorf("o payload null: %+v, %v", p, err)
	}
}

// O state do push lê no tipo da mutação do mesmo lugar no lote: o do ditado
// no Item, o do aprendido no Aprendido, num lote misturado; o resultado sem
// state fica sem os dois.
func TestLeituraPeloTipo_OStateDoPush(t *testing.T) {
	lote := muts(storage.EntidadeDitado, storage.EntidadeAprendido, storage.EntidadeAprendido)
	lote[2].Operation = OpDelete
	corpo := resposta(
		`{"mutation_id":"`+mutacao1+`","status":"conflict","entity_version":5,"state":`+itemDoFio+`}`,
		`{"mutation_id":"`+mutacao2+`","status":"conflict","entity_version":2,"state":`+parDoFio+`}`,
		`{"mutation_id":"`+mutacao3+`","status":"rejected","code":"nada_a_desfazer"}`)
	r, err := respostaPeloTipo([]byte(corpo), lote)
	if err != nil || len(r.Results) != 3 || r.Epoch != "e1" {
		t.Fatalf("a resposta: %+v, %v", r, err)
	}
	soDitado, _ := respostaDireta([]byte(resposta(`{"mutation_id":"` + mutacao1 + `","status":"conflict","entity_version":5,"state":` + itemDoFio + `}`)))
	if !reflect.DeepEqual(r.Results[0], soDitado.Results[0]) {
		t.Errorf("o ditado: %+v", r.Results[0])
	}
	quero := Aprendido{ID: idDoPar, Variant: "nfe", Term: "NF-e", Restricted: true, Domain: texto("fiscal")}
	if res := r.Results[1]; res.State != nil || res.StateAprendido == nil || !reflect.DeepEqual(*res.StateAprendido, quero) ||
		res.Status != StatusConflict || res.EntityVersion != 2 {
		t.Errorf("o aprendido: %+v (state %+v)", res, res.StateAprendido)
	}
	if res := r.Results[2]; res.State != nil || res.StateAprendido != nil || res.Code != CodigoNadaADesfazer {
		t.Errorf("o desfazer sem nada a desfazer: %+v", res)
	}

	// O state do aprendido não passa pelo Item; ilegível no tipo dele, erra.
	estranho := resposta(`{"mutation_id":"` + mutacao1 + `","status":"conflict","state":{"id":"` + idDoPar + `","version":"x"}}`)
	if _, err := respostaDireta([]byte(estranho)); err == nil {
		t.Fatal("o caso não prova nada: a leitura da F7 não errou")
	}
	if r, err := respostaPeloTipo([]byte(estranho), muts(storage.EntidadeAprendido)); err != nil || r.Results[0].StateAprendido == nil {
		t.Errorf("o state do aprendido passou pelo Item: %+v, %v", r, err)
	}
	ruim := resposta(`{"mutation_id":"` + mutacao1 + `","status":"conflict","state":{"restricted":"sim"}}`)
	if _, err := respostaPeloTipo([]byte(ruim), muts(storage.EntidadeAprendido)); err == nil {
		t.Error("o state ilegível do aprendido foi lido")
	}
}

// O delete do aprendido vai sem payload: o null do fio, que não pesa no lote.
func TestFio_ODeleteSemPayload(t *testing.T) {
	m := Mutation{MutationID: mutacao1, EntityType: storage.EntidadeAprendido, EntityID: idDoPar,
		Operation: OpDelete, BaseVersion: 3}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	quero := `{"mutation_id":"` + mutacao1 + `","entity_type":"learned","entity_id":"` + idDoPar +
		`","operation":"delete","base_version":3,"payload":null}`
	if string(raw) != quero {
		t.Errorf("o delete no fio:\n%s\nquero\n%s", raw, quero)
	}
	if n := CortarLote([]Mutation{m, m}); n != 2 {
		t.Errorf("CortarLote = %d", n)
	}
}

// O Cliente lê pelo tipo: o delete sai com o payload null; o state do
// aprendido vem no Aprendido e o do ditado no Item, no mesmo lote; e o pull
// também.
func TestCliente_FioPorTipo(t *testing.T) {
	var corpo []byte
	srv := nuvemHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/push" {
			corpo, _ = io.ReadAll(r.Body)
			_, _ = io.WriteString(w, resposta(
				`{"mutation_id":"`+mutacao1+`","status":"conflict","entity_version":5,"state":`+itemDoFio+`}`,
				`{"mutation_id":"`+mutacao2+`","status":"conflict","entity_version":2,"state":`+parDoFio+`}`,
				`{"mutation_id":"`+mutacao3+`","status":"applied","entity_version":4}`))
			return
		}
		_, _ = io.WriteString(w, pagina(
			`{"seq":8,"entity_type":"learned","entity_id":"`+idDoOutroPar+`","operation":"upsert",`+
				`"entity_version":4,"payload":`+parDesfeitoDoFio+`}`))
	})
	c := NovoCliente(srv.URL, tokenDeMentira, nil)
	lote := muts(storage.EntidadeDitado, storage.EntidadeAprendido, storage.EntidadeAprendido)
	lote[0].Payload = json.RawMessage(`{"final_text":"x"}`)
	lote[1].Payload = json.RawMessage(`{"variant":"nfe","term":"Outro","restricted":false,"domain":null}`)
	lote[2].Operation, lote[2].EntityID = OpDelete, idDoOutroPar
	res, err := c.Push(context.Background(), lote)
	if err != nil {
		t.Fatal(err)
	}
	var enviado struct {
		Mutations []map[string]json.RawMessage `json:"mutations"`
	}
	if err := json.Unmarshal(corpo, &enviado); err != nil || len(enviado.Mutations) != 3 {
		t.Fatalf("o corpo do push: %s, %v", corpo, err)
	}
	if d := enviado.Mutations[2]; string(d["operation"]) != `"delete"` || string(d["payload"]) != `null` {
		t.Errorf("o delete enviado: %s", corpo)
	}
	if res.Results[0].State == nil || res.Results[0].State.FinalText != "venceu" || res.Results[0].StateAprendido != nil {
		t.Errorf("o state do ditado: %+v", res.Results[0])
	}
	if res.Results[1].State != nil || res.Results[1].StateAprendido == nil || res.Results[1].StateAprendido.Term != "NF-e" {
		t.Errorf("o state do aprendido: %+v", res.Results[1])
	}
	if res.Results[2].State != nil || res.Results[2].StateAprendido != nil || res.Results[2].EntityVersion != 4 {
		t.Errorf("o applied do desfazer: %+v", res.Results[2])
	}

	p, err := c.Pull(context.Background(), 0, LimiteDoPull)
	if err != nil || len(p.Changes) != 1 {
		t.Fatalf("o pull: %+v, %v", p, err)
	}
	if ch := p.Changes[0]; ch.Payload != nil || ch.PayloadAprendido == nil || ch.PayloadAprendido.UndoneAt == nil ||
		ch.PayloadAprendido.Domain != nil || ch.PayloadAprendido.Variant != "sf" {
		t.Errorf("o aprendido do pull: %+v (%+v)", ch, ch.PayloadAprendido)
	}
}

// O payload ou o state ilegível no tipo é CodigoJSON, como na F7 (que o lia
// junto com o resto da resposta): antes das conferências do lote e do cursor,
// que dariam outro código.
func TestCliente_IlegivelNoTipoECodigoJSON(t *testing.T) {
	pushes := map[string]struct {
		corpo string
		lote  []Mutation
	}{
		"o state do ditado": {resposta(`{"mutation_id":"` + mutacao1 + `","status":"conflict","state":{"version":"x"}}`),
			muts(storage.EntidadeDitado)},
		"o state do ditado, com o lote que não bate": {`{"results":[{"mutation_id":"outro","state":"x"},{}]}`,
			muts(storage.EntidadeDitado)},
		"o state do aprendido": {resposta(`{"mutation_id":"` + mutacao1 + `","status":"conflict","state":{"restricted":"sim"}}`),
			muts(storage.EntidadeAprendido)},
	}
	for nome, c := range pushes {
		t.Run("push: "+nome, func(t *testing.T) {
			srv := nuvemHTTP(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, c.corpo) })
			_, err := NovoCliente(srv.URL, tokenDeMentira, nil).Push(context.Background(), c.lote)
			if !errors.Is(err, ErrContrato) || CodigoDoErro(err) != CodigoJSON {
				t.Errorf("err = %v (%s); quero ErrContrato, %s", err, CodigoDoErro(err), CodigoJSON)
			}
		})
	}
	for nome, corpo := range map[string]string{
		"o payload do ditado":                         pagina(mudanca(`,"payload":{"version":"x"}`)),
		"o payload do ditado, com o cursor que volta": `{"changes":[` + mudanca(`,"payload":"x"`) + `],"last_seq":0}`,
		"o payload do aprendido": pagina(`{"entity_type":"learned","entity_id":"` + idDoPar +
			`","payload":{"restricted":"sim"}}`),
	} {
		t.Run("pull: "+nome, func(t *testing.T) {
			srv := nuvemHTTP(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, corpo) })
			_, err := NovoCliente(srv.URL, tokenDeMentira, nil).Pull(context.Background(), 7, LimiteDoPull)
			if !errors.Is(err, ErrContrato) || CodigoDoErro(err) != CodigoJSON {
				t.Errorf("err = %v (%s); quero ErrContrato, %s", err, CodigoDoErro(err), CodigoJSON)
			}
		})
	}
}
