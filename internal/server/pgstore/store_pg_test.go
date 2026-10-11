package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// Testes contra um Postgres de verdade. Só rodam com AURORA_TEST_POSTGRES_URL
// (pgtest.NewDB pula o teste sem ela): o "go test ./..." normal não toca no
// Docker. Cada teste cria o próprio banco aw_test_<hex>, com as migrations, e
// o apaga no fim.

var now0 = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

type pgEnv struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
}

func newPG(t *testing.T) *pgEnv {
	t.Helper()
	url := pgtest.NewDB(t) // pula sem AURORA_TEST_POSTGRES_URL
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load(migrations.FS): %v", err)
	}
	if _, err := pgmigrate.Run(ctx, pgtest.Connect(t, url), migs); err != nil {
		t.Fatalf("aplicar as migrations: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("pgxpool.ParseConfig falhou (a URL não é impressa)")
	}
	cfg.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("abrir o pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return &pgEnv{t: t, ctx: ctx, pool: pool}
}

func (e *pgEnv) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(e.ctx, sql, args...); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

func (e *pgEnv) scalar(dest any, sql string, args ...any) {
	e.t.Helper()
	if err := e.pool.QueryRow(e.ctx, sql, args...).Scan(dest); err != nil {
		e.t.Fatalf("%s: %v", sql, err)
	}
}

// newUser cria o usuário e o contador dele, como a F3 fará no primeiro login.
func (e *pgEnv) newUser(t *testing.T) string {
	t.Helper()
	id := contracttest.NewID()
	e.exec("INSERT INTO users (id, google_sub, email) VALUES ($1, $2, $3)", id, "sub-"+id, id+"@exemplo.test")
	e.exec("INSERT INTO user_sync_counters (user_id, sync_epoch) VALUES ($1, gen_random_uuid())", id)
	return id
}

func (e *pgEnv) newDevice(t *testing.T, userID string) string {
	t.Helper()
	id := contracttest.NewID()
	e.exec("INSERT INTO devices (id, user_id, install_id, name, platform) VALUES ($1, $2, $3, 'PC de teste', 'windows')",
		id, userID, "install-"+id)
	return id
}

func (e *pgEnv) events(t *testing.T, userID string) []cloudstore.Event {
	t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT seq, entity_type, entity_id, operation, entity_version, device_id, changed_at
		FROM sync_events WHERE user_id = $1 ORDER BY seq`, userID)
	if err != nil {
		t.Fatalf("ler sync_events: %v", err)
	}
	defer rows.Close()
	var out []cloudstore.Event
	for rows.Next() {
		var ev cloudstore.Event
		if err := rows.Scan(&ev.Seq, &ev.EntityType, &ev.EntityID, &ev.Operation, &ev.EntityVersion, &ev.DeviceID, &ev.ChangedAt); err != nil {
			t.Fatalf("ler sync_events: %v", err)
		}
		ev.ChangedAt = ev.ChangedAt.UTC()
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("ler sync_events: %v", err)
	}
	return out
}

func (e *pgEnv) lastSeq(userID string) int64 {
	e.t.Helper()
	var n int64
	e.scalar(&n, "SELECT last_seq FROM user_sync_counters WHERE user_id = $1", userID)
	return n
}

// TestContrato_Postgres roda a mesma suíte de contrato do Store em memória
// (internal/server/store_contract_test.go) contra o Postgres, o aprendido
// inclusive (F8c): a tabela da nuvem linha a linha, as pré-checagens, o
// duplicate que confere a operação, o pull fundido por seq, o "apagar meus
// dados" e o log sem texto.
func TestContrato_Postgres(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	contracttest.Run(t, contracttest.Backend{
		Store:     s,
		Accounts:  s,
		NewDevice: e.newDeviceWithToken,
		Events:    e.events,
		AntesDaTrava: func(t *testing.T, f func(id string)) {
			antesDaTravaDaRetencao = f
			t.Cleanup(func() { antesDaTravaDaRetencao = nil })
		},
		Fotografar: e.fotografar,
	})
}

// newDeviceWithToken cria o aparelho e grava o HMAC do token dele (o
// pareamento é da F6).
func (e *pgEnv) newDeviceWithToken(t *testing.T, userID string, tokenHMAC []byte) string {
	t.Helper()
	id := e.newDevice(t, userID)
	e.exec("INSERT INTO device_tokens (token_hmac, device_id) VALUES ($1, $2)", tokenHMAC, id)
	return id
}

func dictation(t *testing.T, text string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"source_platform": "windows", "raw_text": "bruto: " + text, "final_text": text,
		"mode": "smart", "asr_provider": "whisper-local", "status": "done",
		"created_at": now0.Add(-time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func createMut(t *testing.T, id, text string) cloudstore.Mutation {
	return cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: "transcription", EntityID: id,
		Operation: "upsert", Payload: dictation(t, text)}
}

// Uma gravação que falha DEPOIS de subir o contador é desfeita inteira: o
// número volta, e a próxima gravação pega o seguinte, sem buraco. A falha é
// a chave estrangeira composta: o aparelho do usuário B num evento do A.
func TestPostgres_RollbackDevolveONumero(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	userA, userB := e.newUser(t), e.newUser(t)
	devA, devB := e.newDevice(t, userA), e.newDevice(t, userB)
	pcA := cloudstore.Actor{UserID: userA, DeviceID: devA}
	id := contracttest.NewID()

	if _, err := s.Push(e.ctx, pcA, []cloudstore.Mutation{createMut(t, id, "um")}, now0); err != nil {
		t.Fatalf("push: %v", err)
	}
	_, err := s.Edit(e.ctx, cloudstore.Actor{UserID: userA, DeviceID: devB}, id, 1, "com o aparelho de B", now0)
	var dbErr *DBError
	if !errors.As(err, &dbErr) || dbErr.Code != "23503" {
		t.Fatalf("Edit com o aparelho de outro usuário: %v, quero a chave estrangeira (23503)", err)
	}
	if got := e.lastSeq(userA); got != 1 {
		t.Fatalf("contador = %d depois da falha, quero 1 (o rollback devolve o número)", got)
	}
	if _, err := s.Edit(e.ctx, cloudstore.Actor{UserID: userA}, id, 1, "pelo celular", now0); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	evs := e.events(t, userA)
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Seq != 2 || evs[1].EntityVersion != 2 || evs[1].DeviceID != nil {
		t.Fatalf("eventos = %+v, quero o seq 1 e o 2 (a edição do celular, sem aparelho)", evs)
	}
	var row struct {
		version, lastSeq int64
		text             string
	}
	if err := e.pool.QueryRow(e.ctx, "SELECT version, last_seq, final_text FROM transcriptions WHERE user_id = $1 AND id = $2",
		userA, id).Scan(&row.version, &row.lastSeq, &row.text); err != nil {
		t.Fatal(err)
	}
	if row.version != 2 || row.lastSeq != 2 || row.text != "pelo celular" {
		t.Fatalf("linha = %+v, quero v2 no seq 2 com o texto do celular", row)
	}
}

// Uma decisão que não grava entidade (reenvio, recusa, gone, lixeira que já
// estava lá) não gasta seq: o contador fica onde estava, e as recusas e os
// gone ficam em applied_mutations para o reenvio.
func TestPostgres_DecisaoSemGravacaoNaoGastaSeq(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	id := contracttest.NewID()
	m := createMut(t, id, "um")
	if _, err := s.Push(e.ctx, pc, []cloudstore.Mutation{m}, now0); err != nil {
		t.Fatal(err)
	}
	bad := createMut(t, contracttest.NewID(), "recusado")
	bad.Payload = json.RawMessage(`{"target_app":"x"}`)
	old := createMut(t, contracttest.NewID(), "antigo")
	old.Payload = json.RawMessage(strings.Replace(string(old.Payload), now0.Add(-time.Hour).Format(time.RFC3339),
		now0.AddDate(-1, 0, 0).Format(time.RFC3339), 1))
	resp, err := s.Push(e.ctx, pc, []cloudstore.Mutation{m, bad, old}, now0)
	if err != nil {
		t.Fatal(err)
	}
	got := []cloudstore.Status{resp.Results[0].Status, resp.Results[1].Status, resp.Results[2].Status}
	if got[0] != cloudstore.StatusDuplicate || got[1] != cloudstore.StatusRejected || got[2] != cloudstore.StatusGone {
		t.Fatalf("resultados = %v, quero duplicate, rejected, gone", got)
	}
	if _, err := s.Restore(e.ctx, cloudstore.Actor{UserID: user}, id, now0); err != nil {
		t.Fatal(err)
	}
	if n := e.lastSeq(user); n != 1 {
		t.Fatalf("contador = %d, quero 1 (nada disso gravou entidade)", n)
	}
	var recorded int
	e.scalar(&recorded, "SELECT count(*) FROM applied_mutations WHERE user_id = $1", user)
	if recorded != 3 {
		t.Fatalf("applied_mutations = %d, quero 3 (o applied, o rejected e o gone)", recorded)
	}
}

// O pull lê as entidades e as lápides do mesmo snapshot. O gancho grava, entre
// as duas leituras, uma edição (seq N+1) e uma exclusão definitiva (seq N+2)
// por outra conexão. Se as leituras fossem de snapshots diferentes, o pull
// veria a lápide (N+2) sem a edição (N+1), o cursor iria para N+2, e a edição
// nunca chegaria ao PC.
func TestPostgres_PullNumSnapshotSo(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	cel := cloudstore.Actor{UserID: user}
	edited, purged := contracttest.NewID(), contracttest.NewID()
	if _, err := s.Push(e.ctx, pc, []cloudstore.Mutation{createMut(t, edited, "a"), createMut(t, purged, "b")}, now0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Trash(e.ctx, cel, purged, now0); err != nil {
		t.Fatal(err)
	}
	cursor := int64(3)

	fired := false
	betweenPullQueries = func(done string) {
		if fired || done != pullReadTranscriptions {
			return
		}
		fired = true
		if _, err := s.Edit(e.ctx, cel, edited, 1, "editado no meio do pull", now0); err != nil {
			t.Errorf("Edit no meio: %v", err)
		}
		if err := s.Purge(e.ctx, cel, purged, now0); err != nil {
			t.Errorf("Purge no meio: %v", err)
		}
	}
	t.Cleanup(func() { betweenPullQueries = nil })

	p, err := s.Pull(e.ctx, user, cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !fired {
		t.Fatal("o gancho não rodou")
	}
	if len(p.Changes) != 0 || p.LastSeq != cursor {
		t.Fatalf("pull com gravação no meio = %+v, quero nada (as duas gravações são de depois do snapshot)", p)
	}
	p, err = s.Pull(e.ctx, user, p.LastSeq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 2 || p.Changes[0].Seq != 4 || p.Changes[0].EntityID != edited ||
		p.Changes[1].Seq != 5 || p.Changes[1].Operation != cloudstore.OpTombstone {
		t.Fatalf("pull seguinte = %+v, quero a edição (seq 4) e a lápide (seq 5)", p)
	}
}

// A trava do contador, provada sem depender de sorte: o escritor A sobe o
// contador e para no meio da transação; o escritor B (outra conexão, outro
// ditado) espera a trava em vez de tomar o seq seguinte; um pull no meio não
// vê nada; A confirma com o seq N+1, e só então B grava, com N+2.
func TestPostgres_SegundoEscritorEsperaATrava(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	cel := cloudstore.Actor{UserID: user}
	existing := contracttest.NewID()
	if _, err := s.Push(e.ctx, pc, []cloudstore.Mutation{createMut(t, existing, "já existia")}, now0); err != nil {
		t.Fatal(err)
	}

	holding, release := make(chan int64), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	// Se o teste falhar com A parado, solta A antes de fechar o pool: senão
	// o pool.Close espera para sempre a conexão de A.
	t.Cleanup(unblock)
	aDone := make(chan error, 1)
	go func() {
		newID := contracttest.NewID()
		aDone <- s.gravar(e.ctx, user, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
			out := cloudstore.DecidePush(createMut(t, newID, "do escritor A"), nil, false, pc, now0)
			if err := put(e.ctx, tx, out.Next, true, 0, seq, pc, now0); err != nil {
				return false, err
			}
			holding <- seq
			<-release
			return true, nil
		})
	}()
	var seqA int64
	select {
	case seqA = <-holding:
	case err := <-aDone:
		t.Fatalf("A falhou antes de segurar a trava: %v", err)
	}

	bDone := make(chan cloudstore.Transcription, 1)
	go func() {
		got, err := s.Edit(e.ctx, cel, existing, 1, "do escritor B", now0)
		if err != nil {
			t.Errorf("Edit de B: %v", err)
		}
		bDone <- got
	}()
	select {
	case got := <-bDone:
		t.Fatalf("B gravou (seq %d) enquanto A segurava a trava do contador", got.LastSeq)
	case <-time.After(300 * time.Millisecond):
	}
	if p, err := s.Pull(e.ctx, user, 1, 0); err != nil || len(p.Changes) != 0 {
		t.Fatalf("pull no meio = %+v, %v; quero nada (A não confirmou, B espera)", p, err)
	}

	unblock()
	if err := <-aDone; err != nil {
		t.Fatalf("A: %v", err)
	}
	b := <-bDone
	if seqA != 2 || b.LastSeq != 3 {
		t.Fatalf("seq de A = %d, de B = %d; quero 2 e 3", seqA, b.LastSeq)
	}
}

// O mesmo id em duas contas são duas linhas (a PK é por usuário), e o mesmo
// mutation_id também.
func TestPostgres_OMesmoIDEmDuasContasSaoDuasLinhas(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	userA, userB := e.newUser(t), e.newUser(t)
	pcA := cloudstore.Actor{UserID: userA, DeviceID: e.newDevice(t, userA)}
	pcB := cloudstore.Actor{UserID: userB, DeviceID: e.newDevice(t, userB)}
	id := contracttest.NewID()
	ma, mb := createMut(t, id, "de A"), createMut(t, id, "de B")
	mb.MutationID = ma.MutationID
	for _, c := range []struct {
		a cloudstore.Actor
		m cloudstore.Mutation
	}{{pcA, ma}, {pcB, mb}} {
		resp, err := s.Push(e.ctx, c.a, []cloudstore.Mutation{c.m}, now0)
		if err != nil || resp.Results[0].Status != cloudstore.StatusApplied {
			t.Fatalf("push: %+v %v", resp, err)
		}
	}
	var rows, muts int
	e.scalar(&rows, "SELECT count(*) FROM transcriptions WHERE id = $1", id)
	e.scalar(&muts, "SELECT count(*) FROM applied_mutations WHERE mutation_id = $1", ma.MutationID)
	if rows != 2 || muts != 2 {
		t.Fatalf("linhas = %d, mutações = %d; quero 2 e 2", rows, muts)
	}
	for user, want := range map[string]string{userA: "de A", userB: "de B"} {
		got, err := s.Get(e.ctx, user, id)
		if err != nil || got.FinalText != want || got.UserID != user {
			t.Errorf("Get(%s) = %+v, %v; quero %q", user, got, err, want)
		}
	}
}

// O erro do Postgres traz os valores da linha (o texto ditado) no Detail; o
// DBError, não.
func TestPostgres_ErroSemTextoDitado(t *testing.T) {
	e := newPG(t)
	user := e.newUser(t)
	const secret = "TEXTO-DITADO-SIGILOSO-4410"
	_, err := e.pool.Exec(e.ctx, `INSERT INTO transcriptions (id, user_id, source_platform, raw_text, final_text, mode,
		asr_provider, status, created_at, last_seq) VALUES ($1, $2, 'windows', $3, $3, 'magia', 'x', 'done', now(), 1)`,
		contracttest.NewID(), user, secret)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || !strings.Contains(pgErr.Detail, secret) {
		t.Fatalf("esperava o erro cru do Postgres com o texto no Detail (é o que o teste prova que some): %v", err)
	}
	clean := sanitize("gravar o ditado", err)
	if strings.Contains(clean.Error(), secret) {
		t.Fatalf("o DBError carrega o texto ditado: %v", clean)
	}
	var dbErr *DBError
	if !errors.As(clean, &dbErr) || dbErr.Code != "23514" || dbErr.Constraint == "" {
		t.Fatalf("DBError = %+v, quero o SQLSTATE 23514 e a constraint", dbErr)
	}
	if errors.Is(clean, err) {
		t.Fatal("o DBError embrulha o erro original (e com ele o texto)")
	}
}

// A leitura do pull e da listagem para no orçamento de texto, e não só a
// resposta: com ditados no teto, o Store lê do banco só a página mais uma
// linha (a que diz que há mais), e não os limit+1 que a consulta permite. É
// isso que segura a memória de um pedido (a resposta já é cortada pela suíte
// de contrato).
func TestPostgres_LeituraParaNoOrcamentoDeTexto(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	teto := strings.Repeat("a", cloudstore.MaxTextBytes)
	perItem := 2 * cloudstore.MaxTextBytes
	perPage := (cloudstore.PageTextBudget + perItem - 1) / perItem
	total := perPage + 5
	for i := 0; i < total; i++ {
		raw, err := json.Marshal(map[string]any{
			"source_platform": "windows", "raw_text": teto, "final_text": teto,
			"mode": "smart", "asr_provider": "whisper-local", "status": "done",
			"created_at": now0.Add(-time.Hour).Format(time.RFC3339),
		})
		if err != nil {
			t.Fatal(err)
		}
		m := cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: "transcription",
			EntityID: contracttest.NewID(), Operation: "upsert", Payload: raw}
		if resp, err := s.Push(e.ctx, pc, []cloudstore.Mutation{m}, now0); err != nil || resp.Results[0].Status != cloudstore.StatusApplied {
			t.Fatalf("push %d: %+v %v", i, resp, err)
		}
	}
	read := map[string]int{}
	rowsRead = func(where string, n int) { read[where] = n }
	t.Cleanup(func() { rowsRead = nil })

	p, err := s.Pull(e.ctx, user, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != perPage || !p.HasMore {
		t.Fatalf("pull = %d mudanças, has_more %v; quero %d e has_more", len(p.Changes), p.HasMore, perPage)
	}
	page, err := s.List(e.ctx, cloudstore.ListFilter{UserID: user, Limit: cloudstore.MaxListLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != perPage || page.NextCursor == "" {
		t.Fatalf("listagem = %d itens, cursor %q; quero %d e um cursor", len(page.Items), page.NextCursor, perPage)
	}
	if read["pull"] != perPage+1 || read["list"] != perPage+1 {
		t.Fatalf("linhas lidas = %v, quero %d no pull e na listagem (a página e mais uma), de %d", read, perPage+1, total)
	}
}
