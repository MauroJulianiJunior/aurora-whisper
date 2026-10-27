package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// A retenção no Postgres (F9b; ADR-019, "Decisões da F9"): o que só o banco
// prova. O comportamento pela API (o ativo, a lixeira, as lápides, o PC
// parado, o restaurado no meio pelo gancho, o ctx cancelado) roda pela suíte
// de contrato (TestContrato_Postgres); aqui ficam a corrida com a trava de
// verdade, o push e o "apagar meus dados" junto com o job, o fuso trocado e os
// lotes por cursor. Só rodam com AURORA_TEST_POSTGRES_URL.

// dictationAt é o payload de um ditado novo com a data do PC escolhida.
func dictationAt(t *testing.T, text string, created time.Time) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"source_platform": "windows", "raw_text": "bruto: " + text, "final_text": text,
		"mode": "smart", "asr_provider": "whisper-local", "status": "done",
		"created_at": created.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func trashMut(t *testing.T, id string, base int64, at time.Time) cloudstore.Mutation {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"deleted_at": at.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	return cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: "transcription", EntityID: id,
		Operation: "upsert", BaseVersion: base, Payload: raw}
}

// pushAll manda as mutações em lotes de até 500 e confere que todas valeram.
func (e *pgEnv) pushAll(t *testing.T, s *Store, a cloudstore.Actor, muts []cloudstore.Mutation, now time.Time) {
	t.Helper()
	for len(muts) > 0 {
		n := min(len(muts), cloudstore.MaxPushMutations)
		resp, err := s.Push(e.ctx, a, muts[:n], now)
		if err != nil {
			t.Fatalf("push: %v", err)
		}
		for _, r := range resp.Results {
			if r.Status != cloudstore.StatusApplied {
				t.Fatalf("push: %+v, quero applied", r)
			}
		}
		muts = muts[n:]
	}
}

// requireSeqs confere que os eventos do usuário são 1..n e o contador está em
// n: sem buraco nem repetição.
func (e *pgEnv) requireSeqs(t *testing.T, userID string, n int64) {
	t.Helper()
	evs := e.events(t, userID)
	if int64(len(evs)) != n || e.lastSeq(userID) != n {
		t.Fatalf("%d eventos e o contador em %d, quero %d", len(evs), e.lastSeq(userID), n)
	}
	for i, ev := range evs {
		if ev.Seq != int64(i+1) {
			t.Fatalf("seq %d na posição %d (buraco ou repetição)", ev.Seq, i)
		}
	}
}

// A corrida restaurar × reter com a trava de verdade: o restaurar está no
// meio da transação (segura o contador e a linha) quando o Reter, que já
// escolheu o ditado sem trava, chega a ele. O Reter espera, decide de novo
// sobre a linha travada e deixa o ditado (AC-018).
func TestPostgres_Retencao_RestaurarNoMeioGanhaDaTrava(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	id := contracttest.NewID()
	e.pushAll(t, s, pc, []cloudstore.Mutation{createMut(t, id, "restaurado no meio")}, now0)
	e.pushAll(t, s, pc, []cloudstore.Mutation{trashMut(t, id, 1, now0)}, now0)
	later := now0.AddDate(0, 0, 30)

	holding, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	restored := make(chan error, 1)
	antesDaTravaDaRetencao = func(got string) {
		if got != id {
			return
		}
		go func() {
			var signalOnce sync.Once
			signal := func() { signalOnce.Do(func() { close(holding) }) }
			defer signal() // num erro, solta o gancho também
			restored <- s.gravar(e.ctx, user, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
				cur, err := readForUpdate(e.ctx, tx, user, id)
				if err != nil || cur == nil {
					return false, fmt.Errorf("ler para restaurar: %v", err)
				}
				next, _ := cloudstore.Restore(*cur, nil, later)
				if err := put(e.ctx, tx, next, false, cur.Version, seq, cloudstore.Actor{UserID: user}, later); err != nil {
					return false, err
				}
				signal()
				<-release
				return true, nil
			})
		}()
		<-holding
		// O Reter segue e vai esperar a trava; solta o restaurar logo depois.
		time.AfterFunc(300*time.Millisecond, unblock)
	}
	t.Cleanup(func() { antesDaTravaDaRetencao = nil })

	r, err := s.Reter(e.ctx, user, later)
	if err != nil {
		t.Fatalf("Reter: %v", err)
	}
	if err := <-restored; err != nil {
		t.Fatalf("restaurar: %v", err)
	}
	if r != (cloudstore.Retido{}) {
		t.Fatalf("Reter = %+v, quero nada (o ditado foi restaurado antes da trava)", r)
	}
	got, err := s.Get(e.ctx, user, id)
	if err != nil || got.DeletedAt != nil || got.Version != 3 {
		t.Fatalf("o ditado = %+v, %v; quero fora da lixeira, v3", got, err)
	}
	e.requireSeqs(t, user, 3)
}

// O push do mesmo usuário durante o Reter, nas mesmas linhas: sem deadlock
// (a ordem das travas é a mesma, contador, entidade, evento), cada mudança é
// applied (antes do job) ou gone apagado (depois), e o seq fica sem buraco.
func TestPostgres_Retencao_PushDuranteOReterSemDeadlock(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	const n = 40
	ids := make([]string, n)
	var muts []cloudstore.Mutation
	for i := range ids {
		ids[i] = contracttest.NewID()
		muts = append(muts, createMut(t, ids[i], fmt.Sprintf("ditado %d", i)))
	}
	e.pushAll(t, s, pc, muts, now0)
	muts = nil
	for _, id := range ids {
		muts = append(muts, trashMut(t, id, 1, now0))
	}
	e.pushAll(t, s, pc, muts, now0)
	later := now0.AddDate(0, 0, 30)

	ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	var retido cloudstore.Retido
	var reterErr error
	wg.Go(func() { retido, reterErr = s.Reter(ctx, user, later) })
	results := make(chan cloudstore.Result, 2*n)
	errs := make(chan error, 2*n)
	for g := range 2 {
		wg.Go(func() {
			for i := g; i < n; i += 2 {
				raw, _ := json.Marshal(map[string]any{"final_text": "mudado durante o job"})
				resp, err := s.Push(ctx, pc, []cloudstore.Mutation{{MutationID: contracttest.NewID(), EntityType: "transcription",
					EntityID: ids[i], Operation: "upsert", BaseVersion: 2, Payload: raw}}, later)
				if err != nil {
					errs <- err
					continue
				}
				results <- resp.Results[0]
			}
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("push durante o Reter: %v", err)
	}
	if reterErr != nil || retido.Lixeira != n || retido.LapidesCriadas != n {
		t.Fatalf("Reter = %+v, %v; quero os %d da lixeira com lápide", retido, reterErr, n)
	}
	applied := int64(0)
	for r := range results {
		switch {
		case r.Status == cloudstore.StatusApplied:
			applied++
		case r.Status == cloudstore.StatusGone && r.Reason == cloudstore.ReasonDeleted:
		default:
			t.Errorf("mudança durante o Reter = %+v, quero applied ou gone apagado", r)
		}
	}
	// n criados, n na lixeira, as mudanças que chegaram antes e n lápides.
	e.requireSeqs(t, user, 3*n+applied)
	if c := e.count("SELECT count(*) FROM transcriptions WHERE user_id = $1", user); c != 0 {
		t.Errorf("%d ditados ficaram, quero 0", c)
	}
}

// O "apagar meus dados" correndo com o job: os dois terminam sem erro, nada
// fica, o seq não volta e o CHECK (tombstone_watermark <= last_seq) vale (o
// job não mexe na marca d'água; D2).
func TestPostgres_Retencao_ApagarMeusDadosJuntoComOJob(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "retencao", Email: "retencao@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := s.CriarSessao(e.ctx, cloudstore.NovaSessao{UserID: u.ID, TokenHMAC: make32("sessao"), Rotulo: "x",
		Agora: now0, ExpiraEm: now0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	pc := cloudstore.Actor{UserID: u.ID, DeviceID: e.newDevice(t, u.ID)}
	const n = 30
	var creates, trashes []cloudstore.Mutation
	for i := range n {
		id := contracttest.NewID()
		creates = append(creates, createMut(t, id, fmt.Sprintf("ditado %d", i)))
		trashes = append(trashes, trashMut(t, id, 1, now0))
	}
	e.pushAll(t, s, pc, creates, now0)
	e.pushAll(t, s, pc, trashes, now0)
	later := now0.AddDate(0, 0, 30)
	before := e.lastSeq(u.ID)

	var wg sync.WaitGroup
	var reterErr error
	wg.Go(func() { _, reterErr = s.Reter(e.ctx, u.ID, later) })
	_, apagarErr := s.ApagarDadosDaConta(e.ctx, u.ID, keep.ID, later)
	wg.Wait()
	if reterErr != nil || apagarErr != nil {
		t.Fatalf("Reter: %v; apagar: %v", reterErr, apagarErr)
	}
	for _, q := range []string{
		"SELECT count(*) FROM transcriptions WHERE user_id = $1",
		"SELECT count(*) FROM tombstones WHERE user_id = $1",
		"SELECT count(*) FROM sync_events WHERE user_id = $1",
	} {
		if c := e.count(q, u.ID); c != 0 {
			t.Errorf("%s = %d, quero 0", q, c)
		}
	}
	var last, watermark int64
	e.scalar(&last, "SELECT last_seq FROM user_sync_counters WHERE user_id = $1", u.ID)
	e.scalar(&watermark, "SELECT tombstone_watermark FROM user_sync_counters WHERE user_id = $1", u.ID)
	if last < before || watermark > last {
		t.Fatalf("last_seq %d (antes %d), marca d'água %d", last, before, watermark)
	}
	if r, err := s.Reter(e.ctx, u.ID, later); err != nil || r != (cloudstore.Retido{}) {
		t.Fatalf("o Reter depois do apagar = %+v, %v; quero nada", r, err)
	}
}

func make32(seed string) []byte {
	b := make([]byte, 32)
	copy(b, seed)
	return b
}

// O fuso trocado: o pgx devolve timestamptz no fuso local, e o AddDate soma
// meses no relógio de parede dele. Com time.Local em UTC+14, o 31/ago 12:00Z
// lido do banco é 1/set 02:00 local; contado no fuso local, ele venceria em
// 28/fev 12:00Z (e a lápide em 1/mar). O Reter decide em UTC: o ditado vence
// em 3/mar e a lápide em 4/mar.
func TestPostgres_Retencao_FusoTrocado(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("UTC+14", 14*60*60)
	t.Cleanup(func() { time.Local = old })

	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	ago31 := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	ativo, apagado := contracttest.NewID(), contracttest.NewID()
	created := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{ativo, apagado} {
		e.pushAll(t, s, pc, []cloudstore.Mutation{{MutationID: contracttest.NewID(), EntityType: "transcription",
			EntityID: id, Operation: "upsert", Payload: dictationAt(t, "fim do mês", ago31)}}, created)
	}
	cel := cloudstore.Actor{UserID: user}
	if _, err := s.Trash(e.ctx, cel, apagado, created); err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(e.ctx, cel, apagado, created); err != nil {
		t.Fatal(err)
	}
	// Na primeira rodada, a arrumação (F9c) leva os três eventos órfãos de
	// 15/set (a criação, a lixeira e a lápide do apagado); o da criação do
	// ativo é o last_seq dele e só sai junto com ele.
	steps := []struct {
		now  time.Time
		want cloudstore.Retido
	}{
		{time.Date(2027, 2, 28, 12, 0, 0, 0, time.UTC), cloudstore.Retido{Eventos: 3}},
		{time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC), cloudstore.Retido{}},
		{time.Date(2027, 3, 3, 12, 0, 0, 0, time.UTC).Add(-time.Microsecond), cloudstore.Retido{}},
		{time.Date(2027, 3, 3, 12, 0, 0, 0, time.UTC), cloudstore.Retido{Ativos: 1, Eventos: 1}},
		{time.Date(2027, 3, 4, 12, 0, 0, 0, time.UTC).Add(-time.Microsecond), cloudstore.Retido{}},
		{time.Date(2027, 3, 4, 12, 0, 0, 0, time.UTC), cloudstore.Retido{LapidesVencidas: 1}},
	}
	for _, st := range steps {
		if r, err := s.Reter(e.ctx, user, st.now.In(time.Local)); err != nil || r != st.want {
			t.Fatalf("Reter em %s = %+v, %v; quero %+v", st.now, r, err, st.want)
		}
	}
}

// Mais de um lote: os candidatos e as lápides são lidos por cursor, e todos
// saem, cada um na sua transação.
func TestPostgres_Retencao_MaisDeUmLote(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	n := cloudstore.LoteDaRetencao + 7
	var creates, trashes []cloudstore.Mutation
	for i := range n {
		id := contracttest.NewID()
		// Datas diferentes, e algumas iguais: o cursor é (created_at, id).
		at := now0.Add(-time.Hour - time.Duration(i/3)*time.Second)
		creates = append(creates, cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: "transcription",
			EntityID: id, Operation: "upsert", Payload: dictationAt(t, fmt.Sprintf("ditado %d", i), at)})
		trashes = append(trashes, trashMut(t, id, 1, now0))
	}
	e.pushAll(t, s, pc, creates, now0)
	e.pushAll(t, s, pc, trashes, now0)

	r, err := s.Reter(e.ctx, user, now0.AddDate(0, 0, 30))
	if err != nil || r != (cloudstore.Retido{Lixeira: int64(n), LapidesCriadas: int64(n)}) {
		t.Fatalf("Reter = %+v, %v; quero os %d com lápide", r, err, n)
	}
	e.requireSeqs(t, user, int64(3*n))
	// Um ano depois: as lápides vencem, e a arrumação leva os 3n eventos
	// (todos órfãos) e as 2n respostas guardadas.
	r, err = s.Reter(e.ctx, user, now0.AddDate(1, 0, 0))
	want := cloudstore.Retido{LapidesVencidas: int64(n), Eventos: int64(3 * n), Mutacoes: int64(2 * n)}
	if err != nil || r != want {
		t.Fatalf("Reter um ano depois = %+v, %v; quero %+v", r, err, want)
	}
	for _, q := range []string{
		"SELECT count(*) FROM tombstones WHERE user_id = $1",
		"SELECT count(*) FROM sync_events WHERE user_id = $1",
		"SELECT count(*) FROM applied_mutations WHERE user_id = $1",
	} {
		if c := e.count(q, user); c != 0 {
			t.Fatalf("%s = %d, quero 0", q, c)
		}
	}
	if last := e.lastSeq(user); last != int64(3*n) {
		t.Fatalf("o contador = %d, quero %d (a arrumação não mexe nele)", last, 3*n)
	}
}

// Os que ficam não voltam na mesma rodada (o fechamento da F9): o cursor
// (created_at, id) avança a cada lote e é estritamente maior que o último
// lido. O MaisDeUmLote não prova isso, porque apaga todo candidato que lê: um
// cursor que não avança, ou que relê o último lido (>= no lugar de >), passa
// nele. Aqui há mais de um lote de candidatos que o pré-filtro do SQL traz e
// que DecideRetencao deixa (o ativo do fim de agosto: em 1/mar ele está antes
// do corte de 1/set, mas só vence em 3/mar), com um vencido na frente.
//
//   - Rodada 1: o gancho do vencido põe na lixeira, já vencida, o último lido
//     do primeiro lote (k; o deleted_at de 1 minuto depois do ditado, o PC que
//     ficou sem internet da D4, chegado à nuvem 24 h antes de now, então o
//     piso da chegada também já passou). Lido como ativo, k fica, e o cursor
//     passa dele: só o vencido sai. Relido, k sairia nesta rodada.
//   - Rodada 2: k sai, com lápide.
//   - Rodada 3: só os que ficam, mais de um lote, e nada sai. Um cursor que não
//     avança leria o primeiro lote para sempre (o ctx de 30 s corta).
func TestPostgres_Retencao_OsQueFicamNaoVoltamNaMesmaRodada(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	now := time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC)
	// O push é 80 dias antes: todos dentro da janela, e nenhum evento órfão
	// (90 dias) nem resposta guardada (190) vence em now; a arrumação não entra
	// nas contas.
	pushed := now.AddDate(0, 0, -80)
	vencido := contracttest.NewID()
	muts := []cloudstore.Mutation{{MutationID: contracttest.NewID(), EntityType: "transcription", EntityID: vencido,
		Operation: "upsert", Payload: dictationAt(t, "vencido", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))}}
	n := cloudstore.LoteDaRetencao + 2
	fimDoMes := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	for i := range n {
		at := fimDoMes.Add(time.Duration(i) * time.Second) // datas distintas: a ordem é a do created_at
		if at.After(cloudstore.CorteDoAtivo(now)) || cloudstore.DecideRetencao(at, nil, nil, now) != cloudstore.RetencaoFica {
			t.Fatalf("o ditado de %s não é candidato que fica em %s", at, now)
		}
		muts = append(muts, cloudstore.Mutation{MutationID: contracttest.NewID(), EntityType: "transcription",
			EntityID: contracttest.NewID(), Operation: "upsert", Payload: dictationAt(t, fmt.Sprintf("fica %d", i), at)})
	}
	e.pushAll(t, s, pc, muts, pushed)

	// k é o último do primeiro lote, na ordem do cursor: o vencido, e depois os
	// LoteDaRetencao-1 primeiros do fim do mês.
	var k string
	var kCreated time.Time
	if err := e.pool.QueryRow(e.ctx, `SELECT id, created_at FROM transcriptions WHERE user_id = $1
		ORDER BY created_at, id OFFSET $2 LIMIT 1`, user, cloudstore.LoteDaRetencao-1).Scan(&k, &kCreated); err != nil {
		t.Fatalf("achar o último do primeiro lote: %v", err)
	}
	if want := fimDoMes.Add(time.Duration(cloudstore.LoteDaRetencao-2) * time.Second); !kCreated.Equal(want) {
		t.Fatalf("o último do primeiro lote é de %s, quero %s", kCreated.UTC(), want)
	}

	var vistos []string
	antesDaTravaDaRetencao = func(id string) {
		vistos = append(vistos, id)
		if id == vencido {
			// Roda no goroutine do teste, fora de transação: o Reter não segura
			// trava nenhuma aqui. A lixeira chega à nuvem 24 h antes de now:
			// o piso da chegada (a decisão do Mauro de 08-out-2026) cai em now,
			// e ela já está vencida, como o caso pede.
			e.pushAll(t, s, pc, []cloudstore.Mutation{trashMut(t, k, 1, kCreated.Add(time.Minute))},
				now.Add(-cloudstore.FolgaDaLapide))
		}
	}
	t.Cleanup(func() { antesDaTravaDaRetencao = nil })
	reter := func(rodada string, want cloudstore.Retido, wantVistos ...string) {
		t.Helper()
		vistos = nil
		ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
		defer cancel()
		r, err := s.Reter(ctx, user, now)
		if err != nil || r != want {
			t.Fatalf("%s: Reter = %+v, %v; quero %+v", rodada, r, err, want)
		}
		if !slices.Equal(vistos, wantVistos) {
			t.Fatalf("%s: os candidatos que chegaram à trava = %v, quero %v", rodada, vistos, wantVistos)
		}
	}

	reter("rodada 1 (k, lido como ativo, não é relido)", cloudstore.Retido{Ativos: 1}, vencido)
	if c := e.count(`SELECT count(*) FROM transcriptions WHERE user_id = $1 AND id = $2
		AND deleted_at IS NOT NULL AND purge_at <= $3`, user, k, now); c != 1 {
		t.Fatalf("k na lixeira vencida: %d, quero 1 (o gancho não o pôs lá?)", c)
	}
	reter("rodada 2 (k sai)", cloudstore.Retido{Lixeira: 1, LapidesCriadas: 1}, k)
	if c := e.count("SELECT count(*) FROM tombstones WHERE user_id = $1 AND entity_id = $2", user, k); c != 1 {
		t.Fatalf("a lápide de k: %d, quero 1", c)
	}
	reter("rodada 3 (só os que ficam)", cloudstore.Retido{})
	if c := e.count("SELECT count(*) FROM transcriptions WHERE user_id = $1", user); c != n-1 {
		t.Fatalf("%d ditados, quero os %d que ficam", c, n-1)
	}
}

// A arrumação em lotes curtos (a revisão da F9): as lápides, os eventos órfãos
// e as respostas guardadas saem em lotes, cada um numa transação que trava o
// contador e confirma; entre um lote e outro a trava está solta, e o push e o
// CRUD do mesmo usuário passam. Antes, as lápides e os dois DELETE sem limite
// iam numa transação só, que segurava a trava até apagar tudo o que venceu
// desde a rodada anterior.
func TestPostgres_Retencao_ArrumacaoEmLotesSoltaATrava(t *testing.T) {
	oldL, oldA := loteDasLapides, loteDaArrumacao
	loteDasLapides, loteDaArrumacao = 2, 2
	t.Cleanup(func() {
		loteDasLapides, loteDaArrumacao = oldL, oldA
		depoisDeUmLoteDaArrumacao = nil
	})
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	const n = 5
	var creates, trashes []cloudstore.Mutation
	for i := range n {
		id := contracttest.NewID()
		creates = append(creates, createMut(t, id, fmt.Sprintf("ditado %d", i)))
		trashes = append(trashes, trashMut(t, id, 1, now0))
	}
	e.pushAll(t, s, pc, creates, now0)
	e.pushAll(t, s, pc, trashes, now0)
	if r, err := s.Reter(e.ctx, user, now0.AddDate(0, 0, 30)); err != nil || r != (cloudstore.Retido{Lixeira: n, LapidesCriadas: n}) {
		t.Fatalf("Reter = %+v, %v; quero os %d com lápide", r, err, n)
	}

	lotes := 0
	depoisDeUmLoteDaArrumacao = func() {
		lotes++
		tx, err := e.pool.Begin(e.ctx)
		if err != nil {
			t.Errorf("abrir a transação de fora: %v", err)
			return
		}
		defer func() { _ = tx.Rollback(e.ctx) }()
		var last int64
		if err := tx.QueryRow(e.ctx, "SELECT last_seq FROM user_sync_counters WHERE user_id = $1 FOR UPDATE NOWAIT", user).Scan(&last); err != nil {
			t.Errorf("o contador continua travado entre os lotes da arrumação (lote %d): %v", lotes, err)
		}
	}
	// Um ano depois: as 5 lápides (lotes de 2: 3 transações), os 15 eventos
	// órfãos (8), os eventos das notas (1, vazio) e as 10 respostas (5, mais a
	// que acha menos que o lote).
	r, err := s.Reter(e.ctx, user, now0.AddDate(1, 0, 0))
	want := cloudstore.Retido{LapidesVencidas: n, Eventos: 3 * n, Mutacoes: 2 * n}
	if err != nil || r != want {
		t.Fatalf("Reter um ano depois = %+v, %v; quero %+v", r, err, want)
	}
	if lotes != 3+8+1+6 {
		t.Errorf("%d lotes, quero %d (cada lote é uma transação)", lotes, 3+8+1+6)
	}
	for _, q := range []string{
		"SELECT count(*) FROM tombstones WHERE user_id = $1",
		"SELECT count(*) FROM sync_events WHERE user_id = $1",
		"SELECT count(*) FROM applied_mutations WHERE user_id = $1",
	} {
		if c := e.count(q, user); c != 0 {
			t.Errorf("%s = %d, quero 0", q, c)
		}
	}
	// Idempotente: a rodada seguinte não acha nada.
	if r, err := s.Reter(e.ctx, user, now0.AddDate(1, 0, 0)); err != nil || r != (cloudstore.Retido{}) {
		t.Fatalf("a segunda rodada = %+v, %v; quero nada", r, err)
	}
}

// A arrumação com o ctx cancelado entre um lote e outro: para, devolve o que
// já confirmou, com o erro, e a rodada seguinte termina.
func TestPostgres_Retencao_ArrumacaoCanceladaEntreLotes(t *testing.T) {
	oldL, oldA := loteDasLapides, loteDaArrumacao
	loteDasLapides, loteDaArrumacao = 2, 2
	t.Cleanup(func() {
		loteDasLapides, loteDaArrumacao = oldL, oldA
		depoisDeUmLoteDaArrumacao = nil
	})
	e := newPG(t)
	s := New(e.pool)
	user := e.newUser(t)
	pc := cloudstore.Actor{UserID: user, DeviceID: e.newDevice(t, user)}
	const n = 5
	var creates []cloudstore.Mutation
	for i := range n {
		creates = append(creates, createMut(t, contracttest.NewID(), fmt.Sprintf("ditado %d", i)))
	}
	e.pushAll(t, s, pc, creates, now0)
	// Um ano depois os 5 ditados saem pelos 6 meses (sem lápide), e os 5
	// eventos e as 5 respostas vencem. O cancelamento vem depois do primeiro
	// lote de eventos (o das lápides, vazio, é o lote 1).
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	lotes := 0
	depoisDeUmLoteDaArrumacao = func() {
		lotes++
		if lotes == 2 {
			cancel()
		}
	}
	r, err := s.Reter(ctx, user, now0.AddDate(1, 0, 0))
	if !errors.Is(err, context.Canceled) || r != (cloudstore.Retido{Ativos: n, Eventos: 2}) {
		t.Fatalf("Reter cancelado entre os lotes = %+v, %v; quero os %d ativos, 2 eventos e context.Canceled", r, err, n)
	}
	depoisDeUmLoteDaArrumacao = nil
	if r, err := s.Reter(e.ctx, user, now0.AddDate(1, 0, 0)); err != nil || r != (cloudstore.Retido{Eventos: n - 2, Mutacoes: n}) {
		t.Fatalf("a rodada seguinte = %+v, %v; quero o resto da arrumação", r, err)
	}
}

// Um usuário sem contador é ErrUnknownUser, e UsuariosDaRetencao lista quem
// tem contador.
func TestPostgres_Retencao_Usuarios(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a, b := e.newUser(t), e.newUser(t)
	users, err := s.UsuariosDaRetencao(e.ctx)
	if err != nil || len(users) != 2 || !(users[0] == a && users[1] == b || users[0] == b && users[1] == a) {
		t.Fatalf("UsuariosDaRetencao = %v, %v; quero os dois", users, err)
	}
	if _, err := s.Reter(e.ctx, contracttest.NewID(), now0); !errors.Is(err, cloudstore.ErrUnknownUser) {
		t.Fatalf("Reter de quem não tem contador: %v, quero ErrUnknownUser", err)
	}
}
