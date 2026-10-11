package pgstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O aprendido no Postgres (F8c; ADR-019, "Decisões da F8"): o que só o banco
// prova. A tabela da nuvem, as pré-checagens, o duplicate que confere a
// operação e o pull fundido rodam pela suíte de contrato (TestContrato_Postgres);
// aqui ficam o snapshot das três leituras do pull, as colunas gravadas (as
// datas do relógio de quem chama, a chave, a operação da applied_mutation), e
// que nenhum push chega ao erro do banco (23505, 23514, 22021), cujo DETAIL
// poria a variante no log do Postgres. Como os outros testes deste pacote, só
// rodam com AURORA_TEST_POSTGRES_URL.

// learnedPayload é o payload do upsert do aprendido, como o PC o manda.
func learnedPayload(t *testing.T, variant, term string, restricted bool, domain *string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"variant": variant, "term": term, "restricted": restricted, "domain": domain})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func learnMut(t *testing.T, variant, term string, base int64) cloudstore.Mutation {
	return cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: cloudstore.EntityLearned,
		EntityID: dictionary.IDDoPar(variant), Operation: cloudstore.OpUpsert, BaseVersion: base,
		Payload: learnedPayload(t, variant, term, false, nil)}
}

func undoMut(variant string, base int64) cloudstore.Mutation {
	return cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: cloudstore.EntityLearned,
		EntityID: dictionary.IDDoPar(variant), Operation: cloudstore.OpDelete, BaseVersion: base}
}

// push1 manda uma mutação e devolve o resultado; um erro do Store para o
// teste.
func (e *pgEnv) push1(t *testing.T, s *Store, a cloudstore.Actor, m cloudstore.Mutation, now time.Time) cloudstore.Result {
	t.Helper()
	resp, err := s.Push(e.ctx, a, []cloudstore.Mutation{m}, now)
	if err != nil {
		t.Fatalf("push %s %s: %v", m.EntityType, m.Operation, err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("push: %d resultados, quero 1", len(resp.Results))
	}
	return resp.Results[0]
}

func (e *pgEnv) mustApply(t *testing.T, s *Store, a cloudstore.Actor, m cloudstore.Mutation, now time.Time, version int64) {
	t.Helper()
	if r := e.push1(t, s, a, m, now); r.Status != cloudstore.StatusApplied || r.EntityVersion != version {
		t.Fatalf("push %s %s (base %d) = %+v, quero applied v%d", m.EntityType, m.Operation, m.BaseVersion, r, version)
	}
}

// learnedRow lê a linha do aprendido direto da tabela.
func (e *pgEnv) learnedRow(t *testing.T, userID, variant string) cloudstore.Learned {
	t.Helper()
	l, err := scanLearned(e.pool.QueryRow(e.ctx, sqlSelectLearned, userID, dictionary.IDDoPar(variant)))
	if err != nil {
		t.Fatalf("ler a linha do aprendido: %v", err)
	}
	return l
}

// Um aprendido gravado entre as leituras do pull não faz o cursor pular. O
// gancho grava, depois da leitura dos ditados, uma edição do celular (seq 4)
// e um par novo (seq 5), e, depois da leitura das lápides, o desfazer de um
// par (seq 6), por outras conexões. Se a leitura do aprendido fosse de outro
// snapshot, o pull veria o 5 e o 6 sem o 4, o cursor iria para o 6, e a
// edição nunca chegaria ao PC.
func TestPostgres_PullComOAprendidoNumSnapshotSo(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	dev := e.newDevice(t, user)
	pc := cloudstore.Actor{UserID: user, DeviceID: dev}
	cel := cloudstore.Actor{UserID: user}
	d := contracttest.NewID()
	e.mustApply(t, s, pc, createMut(t, d, "um ditado"), now0, 1)  // seq 1
	e.mustApply(t, s, pc, learnMut(t, "sf", "ECF", 0), now0, 1)   // seq 2
	e.mustApply(t, s, pc, learnMut(t, "nfe", "NF-e", 0), now0, 1) // seq 3
	const cursor = int64(3)

	var stages []string
	betweenPullQueries = func(done string) {
		stages = append(stages, done)
		switch done {
		case pullReadTranscriptions:
			if _, err := s.Edit(e.ctx, cel, d, 1, "editado no meio do pull", now0); err != nil { // seq 4
				t.Errorf("Edit no meio: %v", err)
			}
			if r := e.push1(t, s, pc, learnMut(t, "ecd", "ECD", 0), now0); r.Status != cloudstore.StatusApplied { // seq 5
				t.Errorf("aprender no meio = %+v", r)
			}
		case pullReadTombstones:
			if r := e.push1(t, s, pc, undoMut("sf", 1), now0); r.Status != cloudstore.StatusApplied { // seq 6
				t.Errorf("desfazer no meio = %+v", r)
			}
		}
	}
	t.Cleanup(func() { betweenPullQueries = nil })

	p, err := s.Pull(e.ctx, user, cursor, 0)
	betweenPullQueries = nil
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(stages) != fmt.Sprint([]string{pullReadTranscriptions, pullReadTombstones}) {
		t.Fatalf("o gancho rodou em %v, quero depois dos ditados e depois das lápides", stages)
	}
	if len(p.Changes) != 0 || p.LastSeq != cursor {
		t.Fatalf("pull com gravações no meio = %+v, quero nada (as três gravações são de depois do snapshot)", p)
	}
	if got := e.lastSeq(user); got != 6 {
		t.Fatalf("contador = %d, quero 6 (as três gravações do meio confirmaram)", got)
	}

	p, err = s.Pull(e.ctx, user, p.LastSeq, 0)
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		seq        int64
		entityType string
		id         string
	}
	wants := []want{
		{4, cloudstore.EntityTranscription, d},
		{5, cloudstore.EntityLearned, dictionary.IDDoPar("ecd")},
		{6, cloudstore.EntityLearned, dictionary.IDDoPar("sf")},
	}
	if len(p.Changes) != len(wants) || p.LastSeq != 6 || p.HasMore {
		t.Fatalf("pull seguinte = %+v, quero a edição (4), o par novo (5) e o desfazer (6)", p)
	}
	for i, w := range wants {
		c := p.Changes[i]
		if c.Seq != w.seq || c.EntityType != w.entityType || c.EntityID != w.id || c.Operation != cloudstore.OpUpsert {
			t.Errorf("mudança %d = %+v, quero %s %s no seq %d (upsert)", i, c, w.entityType, w.id, w.seq)
		}
	}
	if c := p.Changes[0]; c.Item == nil || c.Item.FinalText != "editado no meio do pull" || c.DeviceID != nil || c.Learned != nil {
		t.Errorf("a edição = %+v, quero o texto do celular, sem aparelho", c)
	}
	if c := p.Changes[1]; c.Learned == nil || c.Learned.Term != "ECD" || c.Learned.UndoneAt != nil || c.EntityVersion != 1 ||
		c.DeviceID == nil || *c.DeviceID != dev || c.Item != nil {
		t.Errorf("o par novo = %+v, quero ECD v1, do PC", c)
	}
	if c := p.Changes[2]; c.Learned == nil || c.Learned.UndoneAt == nil || !c.Learned.UndoneAt.Equal(now0) || c.EntityVersion != 2 {
		t.Errorf("o desfazer = %+v, quero o par desfeito (v2) com o undone_at do relógio", c)
	}
}

// As colunas que o ramo do aprendido grava: as datas vêm do relógio de quem
// chama (nunca do DEFAULT now() do banco); normalized_variant é a chave da
// variante, calculada pelo servidor, e não muda quando a grafia muda; o
// UPDATE não toca created_at; o evento é learned upsert, o desfazer
// inclusive; e a applied_mutation guarda a operação.
func TestPostgres_AprendidoGravaAsColunasComORelogioInjetado(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	dev := e.newDevice(t, user)
	pc := cloudstore.Actor{UserID: user, DeviceID: dev}
	t1, t2, t3, t4 := now0, now0.Add(time.Hour), now0.Add(2*time.Hour), now0.Add(3*time.Hour)
	fiscal := "fiscal"

	e.mustApply(t, s, pc, learnMut(t, "DCTF Web", "DCTFWeb", 0), t1, 1)
	l := e.learnedRow(t, user, "DCTF Web")
	if l.ID != dictionary.IDDoPar("DCTF Web") || l.UserID != user || l.Variant != "DCTF Web" ||
		l.NormalizedVariant != dictionary.ChaveDoPar("DCTF Web") || l.NormalizedVariant != "dctfweb" ||
		l.Term != "DCTFWeb" || l.Restricted || l.Domain != nil {
		t.Fatalf("a linha nova = %+v", l)
	}
	if !l.CreatedAt.Equal(t1) || !l.UpdatedAt.Equal(t1) || l.UndoneAt != nil || l.Version != 1 || l.LastSeq != 1 {
		t.Fatalf("a linha nova = %+v, quero created_at e updated_at no relógio (%v), v1 no seq 1", l, t1)
	}

	// Outra grafia da mesma variante (o mesmo id), com o domínio e a
	// restrição: a mesma linha, com a grafia nova; a chave e o created_at
	// ficam.
	m := learnMut(t, "dctf-web", "DCTFWeb", 1)
	m.Payload = learnedPayload(t, "dctf-web", "DCTFWeb", true, &fiscal)
	if m.EntityID != dictionary.IDDoPar("DCTF Web") {
		t.Fatal("as duas grafias deveriam ter o mesmo id")
	}
	e.mustApply(t, s, pc, m, t2, 2)
	l = e.learnedRow(t, user, "DCTF Web")
	if l.Variant != "dctf-web" || l.NormalizedVariant != "dctfweb" || !l.Restricted || l.Domain == nil || *l.Domain != fiscal ||
		!l.CreatedAt.Equal(t1) || !l.UpdatedAt.Equal(t2) || l.Version != 2 || l.LastSeq != 2 {
		t.Fatalf("depois da troca = %+v, quero a grafia nova, restrito ao fiscal, created_at %v, updated_at %v, v2", l, t1, t2)
	}

	e.mustApply(t, s, pc, undoMut("DCTFWeb", 2), t3, 3)
	l = e.learnedRow(t, user, "DCTF Web")
	if l.UndoneAt == nil || !l.UndoneAt.Equal(t3) || !l.UpdatedAt.Equal(t3) || !l.CreatedAt.Equal(t1) || l.Version != 3 ||
		l.Term != "DCTFWeb" || l.Variant != "dctf-web" {
		t.Fatalf("depois do desfazer = %+v, quero undone_at e updated_at %v, o texto mantido, v3", l, t3)
	}

	e.mustApply(t, s, pc, learnMut(t, "DCTF Web", "DCTFWeb", 3), t4, 4)
	l = e.learnedRow(t, user, "DCTF Web")
	if l.UndoneAt != nil || !l.UpdatedAt.Equal(t4) || !l.CreatedAt.Equal(t1) || l.Version != 4 || l.LastSeq != 4 ||
		l.Variant != "DCTF Web" || l.Restricted || l.Domain != nil {
		t.Fatalf("depois de reaprender = %+v, quero undone_at limpo, updated_at %v, v4, sem domínio", l, t4)
	}
	var rows int
	e.scalar(&rows, "SELECT count(*) FROM learned_pairs WHERE user_id = $1", user)
	if rows != 1 {
		t.Fatalf("%d linhas do aprendido, quero 1 (a mesma variante, quatro mudanças)", rows)
	}

	evs := e.events(t, user)
	if len(evs) != 4 {
		t.Fatalf("eventos = %+v, quero 4", evs)
	}
	for i, ev := range evs {
		if ev.Seq != int64(i+1) || ev.EntityType != cloudstore.EntityLearned || ev.EntityID != l.ID ||
			ev.Operation != cloudstore.OpUpsert || ev.EntityVersion != int64(i+1) || ev.DeviceID == nil || *ev.DeviceID != dev {
			t.Errorf("evento %d = %+v, quero learned upsert v%d no seq %d, do PC", i, ev, i+1, i+1)
		}
	}
	var changedAt time.Time
	e.scalar(&changedAt, "SELECT changed_at FROM sync_events WHERE user_id = $1 AND seq = 3", user)
	if !changedAt.Equal(t3) {
		t.Errorf("changed_at do desfazer = %v, quero o relógio (%v)", changedAt, t3)
	}

	got := e.operations(t, user)
	if fmt.Sprint(got) != fmt.Sprint([]string{"upsert", "upsert", "delete", "upsert"}) {
		t.Fatalf("a operação gravada em applied_mutations = %v, quero upsert, upsert, delete, upsert", got)
	}
}

// operations lê a coluna operation das mutações do usuário, na ordem em que
// foram aplicadas ("<nula>" para a nula).
func (e *pgEnv) operations(t *testing.T, userID string) []string {
	t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT operation FROM applied_mutations WHERE user_id = $1 ORDER BY applied_at, mutation_id`, userID)
	if err != nil {
		t.Fatalf("ler applied_mutations: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var op *string
		if err := rows.Scan(&op); err != nil {
			t.Fatalf("ler applied_mutations: %v", err)
		}
		if op == nil {
			out = append(out, "<nula>")
		} else {
			out = append(out, *op)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("ler applied_mutations: %v", err)
	}
	return out
}

// A operação da primeira vez sai da coluna da 0009, e o reenvio a compara:
// a mesma é duplicate; outra é mutacao_reusada. A operação fora da lista fica
// nula (o banco nunca guarda o que o cliente mandou) e compara igual a ela
// mesma; o delete de um ditado (recusado) fica como delete.
func TestPostgres_OReenvioComparaAOperacaoGravada(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	at := func(i int) time.Time { return now0.Add(time.Duration(i) * time.Minute) }

	learn := learnMut(t, "sf", "ECF", 0)
	e.mustApply(t, s, pc, learn, at(1), 1)
	undo := undoMut("sf", 1)
	e.mustApply(t, s, pc, undo, at(2), 2)
	patch := learnMut(t, "nfe", "NF-e", 0)
	patch.Operation = "patch"
	if r := e.push1(t, s, pc, patch, at(3)); r.Status != cloudstore.StatusRejected || r.Code != cloudstore.CodeInvalidOperation {
		t.Fatalf("operação fora da lista = %+v, quero rejected operacao_invalida", r)
	}
	del := cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: cloudstore.EntityTranscription,
		EntityID: contracttest.NewID(), Operation: cloudstore.OpDelete}
	if r := e.push1(t, s, pc, del, at(4)); r.Status != cloudstore.StatusRejected || r.Code != cloudstore.CodeInvalidOperation {
		t.Fatalf("delete de ditado = %+v, quero rejected operacao_invalida", r)
	}
	if got := e.operations(t, user); fmt.Sprint(got) != fmt.Sprint([]string{"upsert", "delete", "<nula>", "delete"}) {
		t.Fatalf("operações gravadas = %v, quero upsert, delete, <nula>, delete", got)
	}

	resend := func(name string, m cloudstore.Mutation, op string, wantStatus cloudstore.Status, wantCode string) {
		t.Helper()
		m.Operation = op
		if op == cloudstore.OpDelete {
			m.Payload = nil
		}
		r := e.push1(t, s, pc, m, at(9))
		if r.Status != wantStatus || r.Code != wantCode {
			t.Errorf("%s = %+v, quero %s %q", name, r, wantStatus, wantCode)
		}
	}
	resend("o aprender reenviado", learn, cloudstore.OpUpsert, cloudstore.StatusDuplicate, "")
	resend("o aprender reenviado como desfazer", learn, cloudstore.OpDelete, cloudstore.StatusRejected, cloudstore.CodeReusedMutation)
	resend("o desfazer reenviado", undo, cloudstore.OpDelete, cloudstore.StatusDuplicate, "")
	resend("o desfazer reenviado como reaprender", undo, cloudstore.OpUpsert, cloudstore.StatusRejected, cloudstore.CodeReusedMutation)
	resend("a operação fora da lista reenviada", patch, "patch", cloudstore.StatusDuplicate, cloudstore.CodeInvalidOperation)
	resend("a operação fora da lista reenviada com outra fora da lista", patch, "merge", cloudstore.StatusDuplicate, cloudstore.CodeInvalidOperation)
	resend("a operação fora da lista reenviada como upsert", patch, cloudstore.OpUpsert, cloudstore.StatusRejected, cloudstore.CodeReusedMutation)
	resend("o delete de ditado reenviado como upsert", del, cloudstore.OpUpsert, cloudstore.StatusRejected, cloudstore.CodeReusedMutation)

	// Nada disso gravou: o par continua desfeito, na v2, e os seq são 1 e 2.
	if l := e.learnedRow(t, user, "sf"); l.UndoneAt == nil || l.Version != 2 {
		t.Fatalf("o par depois dos reenvios = %+v, quero desfeito na v2", l)
	}
	if n := e.lastSeq(user); n != 2 {
		t.Fatalf("contador = %d, quero 2", n)
	}
}

// Nenhum push do aprendido chega ao erro do banco: o UNIQUE (23505), os CHECKs
// da 0016 (23514) e o byte nulo (22021). As grafias da mesma variante caem na
// mesma linha pelo id; as pré-checagens recusam antes do banco; e a chave já
// tomada por outro id (só um nome do v5 trocado, o "v2", ou uma linha posta à
// mão a produziria) é conferida antes do INSERT. Cada recusa fica em
// applied_mutations sem gastar seq. E o
// erro cru do Postgres, que traz a variante no DETAIL, sai sem ela.
func TestPostgres_AprendidoNuncaChegaAoErroDoBanco(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc1 := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	pc2 := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}

	// As grafias da mesma variante: a mesma linha (o id), nunca o UNIQUE.
	e.mustApply(t, s, pc1, learnMut(t, "DCTF Web", "DCTFWeb", 0), now0, 1)
	if r := e.push1(t, s, pc2, learnMut(t, "dctf-web", "DCTFWeb", 0), now0); r.Status != cloudstore.StatusApplied || r.EntityVersion != 1 {
		t.Fatalf("a mesma troca com outra grafia = %+v, quero applied v1 (o mesmo conteúdo, sem gravar)", r)
	}
	if r := e.push1(t, s, pc2, learnMut(t, "DCTFWeb", "DCTF-Web", 0), now0); r.Status != cloudstore.StatusConflict || r.LearnedState == nil ||
		r.LearnedState.Term != "DCTFWeb" || r.LearnedState.LastSeq != 1 {
		t.Fatalf("outro termo com base 0 = %+v, quero conflict com o state da linha", r)
	}

	// As pré-checagens, com o payload cru (o nulo pelo escape do JSON).
	long := strings.Repeat("é", dictionary.MaxRunasPar+1)
	raw := func(variant, payload string) cloudstore.Mutation {
		return cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: cloudstore.EntityLearned,
			EntityID: dictionary.IDDoPar(variant), Operation: cloudstore.OpUpsert, Payload: json.RawMessage(payload)}
	}
	for _, c := range []struct {
		name string
		m    cloudstore.Mutation
	}{
		{"o nulo na variante", raw("sf", `{"variant":"s\u0000f","term":"ECF","restricted":false,"domain":null}`)},
		{"o nulo no termo", raw("sf", `{"variant":"sf","term":"E\u0000CF","restricted":false,"domain":null}`)},
		{"o nulo no domínio", raw("sf", `{"variant":"sf","term":"ECF","restricted":true,"domain":"fis\u0000cal"}`)},
		{"a variante com 81 runas", raw(long, `{"variant":"`+long+`","term":"ECF","restricted":false,"domain":null}`)},
		{"o termo com 81 runas", raw("sf", `{"variant":"sf","term":"`+long+`","restricted":false,"domain":null}`)},
		{"o domínio com 81 runas", raw("sf", `{"variant":"sf","term":"ECF","restricted":false,"domain":"`+long+`"}`)},
		{"o domínio vazio", raw("sf", `{"variant":"sf","term":"ECF","restricted":false,"domain":""}`)},
		{"o termo vazio", raw("sf", `{"variant":"sf","term":"","restricted":false,"domain":null}`)},
		{"a variante sem letra nem dígito", raw("!!!", `{"variant":"!!!","term":"ECF","restricted":false,"domain":null}`)},
		{"o restrito sem domínio", raw("sf", `{"variant":"sf","term":"ECF","restricted":true,"domain":null}`)},
		{"o id de outra variante", raw("sfe", `{"variant":"sf","term":"ECF","restricted":false,"domain":null}`)},
	} {
		if r := e.push1(t, s, pc1, c.m, now0); r.Status != cloudstore.StatusRejected || r.Code != cloudstore.CodeInvalidField {
			t.Errorf("%s = %+v, quero rejected campo_invalido", c.name, r)
		}
	}
	if n := e.lastSeq(user); n != 1 {
		t.Fatalf("contador = %d depois das recusas, quero 1 (nenhuma gastou seq)", n)
	}

	// A chave já tomada por outro id: posta direto no banco, como um nome do
	// v5 de antes (o "v1" visto por um servidor "v2") a teria gravado: a mesma
	// chave, com um id que não vem dela pelo nome de hoje. (Uma regra de chave
	// mudada daria outra chave, e não outro id: esta guarda não a pega.)
	const secret = "VARIANTESECRETA"
	key := dictionary.ChaveDoPar(secret)
	e.exec("UPDATE user_sync_counters SET last_seq = last_seq + 1 WHERE user_id = $1", user)
	e.exec(`INSERT INTO learned_pairs (id, user_id, variant, normalized_variant, term, last_seq)
		VALUES ($1, $2, $3, $4, 'antigo', 2)`, contracttest.NewID(), user, secret, key)
	m := learnMut(t, secret, "novo", 0)
	if r := e.push1(t, s, pc1, m, now0); r.Status != cloudstore.StatusRejected || r.Code != cloudstore.CodeInvalidField {
		t.Fatalf("a chave tomada por outro id = %+v, quero rejected campo_invalido (e não o 23505)", r)
	}
	if r := e.push1(t, s, pc1, m, now0); r.Status != cloudstore.StatusDuplicate || r.Code != cloudstore.CodeInvalidField {
		t.Fatalf("o reenvio = %+v, quero duplicate do campo_invalido", r)
	}
	var withKey int
	e.scalar(&withKey, "SELECT count(*) FROM learned_pairs WHERE user_id = $1 AND normalized_variant = $2", user, key)
	if withKey != 1 || e.lastSeq(user) != 2 {
		t.Fatalf("linhas com a chave = %d, contador = %d; quero 1 e 2 (a recusa não grava nem gasta seq)", withKey, e.lastSeq(user))
	}

	// O erro cru do Postgres para a mesma chave traz a variante no DETAIL; o
	// DBError, não.
	_, err := e.pool.Exec(e.ctx, `INSERT INTO learned_pairs (id, user_id, variant, normalized_variant, term, last_seq)
		VALUES ($1, $2, $3, $4, 'outro', 99)`, contracttest.NewID(), user, secret, key)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || !strings.Contains(pgErr.Detail, key) {
		t.Fatalf("esperava o 23505 cru com a chave no DETAIL (é o que o teste prova que some): %v", err)
	}
	clean := sanitize("gravar o aprendido", err)
	for _, txt := range []string{secret, key} {
		if strings.Contains(clean.Error(), txt) {
			t.Fatalf("o DBError carrega a variante: %v", clean)
		}
	}
	var dbErr *DBError
	if !errors.As(clean, &dbErr) || dbErr.Code != "23505" || dbErr.Constraint == "" || errors.Is(clean, err) {
		t.Fatalf("DBError = %+v, quero o SQLSTATE 23505 e a constraint, sem embrulhar o original", dbErr)
	}
}
