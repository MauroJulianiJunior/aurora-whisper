package migrations_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// Testes do SQL das migrations CONTRA UM POSTGRES DE VERDADE: aplicam as
// migrations num banco descartável e provam o isolamento por usuário, as
// constraints e o contador de seq. Só rodam com AURORA_TEST_POSTGRES_URL
// definida (pgtest.NewDB pula o teste sem ela): o "go test ./..." normal não
// toca no Docker. Para ligar, veja scripts/README.md e migrations/README.md.
//
// schema_test.go é a metade estática (lê o SQL, roda sempre); este arquivo é a
// que prova que o Postgres aceita o SQL e que ele se comporta como a ADR-019
// descreve.

// Códigos SQLSTATE do Postgres que os testes esperam.
const (
	notNullViolation    = "23502"
	foreignKeyViolation = "23503"
	uniqueViolation     = "23505"
	checkViolation      = "23514"
	undefinedColumn     = "42703"
)

// pg é um banco descartável com todas as migrations aplicadas.
type pg struct {
	t    *testing.T
	ctx  context.Context
	conn *pgx.Conn
	url  string
}

func newPG(t *testing.T) *pg {
	t.Helper()
	url := pgtest.NewDB(t) // pula o teste sem AURORA_TEST_POSTGRES_URL
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load(migrations.FS): %v", err)
	}
	conn := pgtest.Connect(t, url)
	if _, err := pgmigrate.Run(ctx, conn, migs); err != nil {
		t.Fatalf("aplicar as migrations: %v", err)
	}
	return &pg{t: t, ctx: ctx, conn: conn, url: url}
}

func (p *pg) exec(sql string, args ...any) {
	p.t.Helper()
	if _, err := p.conn.Exec(p.ctx, sql, args...); err != nil {
		p.t.Fatalf("%s\n  erro: %v", sql, err)
	}
}

func (p *pg) try(sql string, args ...any) error {
	_, err := p.conn.Exec(p.ctx, sql, args...)
	return err
}

func (p *pg) scan(dest any, sql string, args ...any) {
	p.t.Helper()
	if err := p.conn.QueryRow(p.ctx, sql, args...).Scan(dest); err != nil {
		p.t.Fatalf("%s\n  erro: %v", sql, err)
	}
}

func (p *pg) newID() string {
	p.t.Helper()
	var id string
	p.scan(&id, "SELECT gen_random_uuid()::text")
	return id
}

// newUser cria um usuário e o contador dele, como a F3 fará no primeiro login.
func (p *pg) newUser() string {
	p.t.Helper()
	id := p.newID()
	p.exec("INSERT INTO users (id, google_sub, email) VALUES ($1, $2, $3)", id, "sub-"+id, id+"@exemplo.test")
	p.exec("INSERT INTO user_sync_counters (user_id, sync_epoch) VALUES ($1, gen_random_uuid())", id)
	return id
}

func (p *pg) newDevice(user string) string {
	p.t.Helper()
	id := p.newID()
	p.exec("INSERT INTO devices (id, user_id, install_id, name, platform) VALUES ($1, $2, $3, 'PC de teste', 'windows')", id, user, "install-"+id)
	return id
}

// sqlState devolve o SQLSTATE de um erro do Postgres, ou "" se não for um.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func (p *pg) accept(what string, r *row) {
	p.t.Helper()
	if err := p.insert(r); err != nil {
		p.t.Errorf("%s: deveria ser aceito, mas: %v", what, err)
	}
}

func (p *pg) reject(code, what string, r *row) {
	p.t.Helper()
	err := p.insert(r)
	switch got := sqlState(err); {
	case err == nil:
		p.t.Errorf("%s: deveria ser recusado (SQLSTATE %s), mas foi aceito", what, code)
	case got != code:
		p.t.Errorf("%s: SQLSTATE %q, quero %q (erro: %v)", what, got, code, err)
	}
}

func (p *pg) insert(r *row) error {
	sql, args := r.sql()
	return p.try(sql, args...)
}

// row monta um INSERT: cada coluna é um trecho de SQL (lit) ou um parâmetro
// (val). Os testes partem de uma linha válida e mudam o que querem quebrar.
type row struct {
	table string
	cols  []string
	lits  map[string]string
	vals  map[string]any
}

func newRow(table string) *row {
	return &row{table: table, lits: map[string]string{}, vals: map[string]any{}}
}

func (r *row) forget(col string) {
	delete(r.lits, col)
	delete(r.vals, col)
}

func (r *row) lit(col, expr string) *row {
	r.drop(col)
	r.cols = append(r.cols, col)
	r.lits[col] = expr
	return r
}

func (r *row) val(col string, v any) *row {
	r.drop(col)
	r.cols = append(r.cols, col)
	r.vals[col] = v
	return r
}

func (r *row) drop(col string) *row {
	for i, c := range r.cols {
		if c == col {
			r.cols = append(r.cols[:i], r.cols[i+1:]...)
			break
		}
	}
	r.forget(col)
	return r
}

func (r *row) sql() (string, []any) {
	var values []string
	var args []any
	for _, c := range r.cols {
		if expr, ok := r.lits[c]; ok {
			values = append(values, expr)
			continue
		}
		args = append(args, r.vals[c])
		values = append(values, fmt.Sprintf("$%d", len(args)))
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", r.table, strings.Join(r.cols, ", "), strings.Join(values, ", ")), args
}

// TestRowBuilder NÃO precisa de Postgres: confere o montador de INSERT que os
// testes abaixo usam, porque um erro nele faria um teste de constraint passar
// (ou falhar) pelo motivo errado.
func TestRowBuilder(t *testing.T) {
	r := newRow("t").lit("a", "now()").val("b", 7).val("c", nil).lit("d", "'x'")
	sql, args := r.sql()
	if want := "INSERT INTO t (a, b, c, d) VALUES (now(), $1, $2, 'x')"; sql != want {
		t.Errorf("sql = %q, quero %q", sql, want)
	}
	if len(args) != 2 || args[0] != 7 || args[1] != nil {
		t.Errorf("args = %v, quero [7 <nil>]", args)
	}

	// Trocar uma coluna a move para o fim e troca a forma (literal por parâmetro).
	r.val("a", "novo").drop("d")
	sql, args = r.sql()
	if want := "INSERT INTO t (b, c, a) VALUES ($1, $2, $3)"; sql != want {
		t.Errorf("depois de val e drop: sql = %q, quero %q", sql, want)
	}
	if len(args) != 3 || args[2] != "novo" {
		t.Errorf("args = %v", args)
	}

	// As linhas-base dos testes mencionam as colunas certas e nunca target_app.
	for _, base := range []*row{tRow("u", 1), eventRow("u", 1), mutationRow("u", "d")} {
		sql, _ := base.sql()
		if strings.Contains(sql, "target_app") {
			t.Errorf("linha-base com target_app: %s", sql)
		}
	}
	if got := tok("x"); got != "sha256(convert_to('x', 'UTF8'))" {
		t.Errorf("tok = %q", got)
	}
}

// tok devolve um bytea de 32 bytes (um SHA-256) para a semente; shortTok, um
// de 31 bytes, que os CHECKs de HMAC e de hash têm de recusar.
func tok(seed string) string { return fmt.Sprintf("sha256(convert_to('%s', 'UTF8'))", seed) }
func shortTok(seed string) string {
	return fmt.Sprintf("substr(sha256(convert_to('%s', 'UTF8')), 1, 31)", seed)
}

// ---------------------------------------------------------------------------
// O esquema
// ---------------------------------------------------------------------------

func TestPostgres_AsTabelasSaoExatamenteAsDoModelo(t *testing.T) {
	p := newPG(t)
	rows, err := p.conn.Query(p.ctx, `SELECT table_name::text FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []string{"schema_migrations"}
	for _, spec := range wantTables {
		want = append(want, spec.table)
	}
	// Os dois lados ordenados pelo Go: a ordem do ORDER BY depende do collation
	// do banco (o "_" some na comparação de alguns), e isto não é sobre ordem.
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tabelas no banco:\n  %s\nquero:\n  %s", strings.Join(got, ", "), strings.Join(want, ", "))
	}
}

func TestPostgres_NenhumaColunaDeAudioContextoOuTargetApp(t *testing.T) {
	p := newPG(t)
	rows, err := p.conn.Query(p.ctx, `SELECT table_name::text, column_name::text FROM information_schema.columns
		WHERE table_schema = 'public'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		n++
		for _, pattern := range forbiddenColumnPatterns {
			if strings.Contains(strings.ToLower(column), pattern) {
				t.Errorf("%s.%s: contém %q (áudio, contexto e target_app não vão para o banco)", table, column, pattern)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 50 {
		t.Errorf("li só %d colunas: a consulta ao information_schema não está vendo o esquema", n)
	}
}

func TestPostgres_SemSequenceNenhuma(t *testing.T) {
	// O seq é por usuário, tirado do contador. Uma sequence global (o
	// BIGSERIAL da 0008 antiga) não pode existir.
	p := newPG(t)
	var sequences int
	p.scan(&sequences, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'S' AND n.nspname = 'public'`)
	if sequences != 0 {
		t.Errorf("%d sequences no esquema; quero 0", sequences)
	}
	var seqDefault *string
	p.scan(&seqDefault, `SELECT column_default::text FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sync_events' AND column_name = 'seq'`)
	if seqDefault != nil {
		t.Errorf("sync_events.seq tem DEFAULT %q; o número vem do contador do usuário", *seqDefault)
	}
}

// ---------------------------------------------------------------------------
// Isolamento por usuário
// ---------------------------------------------------------------------------

func eventRow(user string, seq int64) *row {
	return newRow("sync_events").
		val("user_id", user).
		val("seq", seq).
		lit("entity_type", "'transcription'").
		lit("entity_id", "gen_random_uuid()").
		lit("operation", "'upsert'").
		lit("entity_version", "1")
}

func TestPostgres_SeqEhUnicoPorUsuario(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()

	p.accept("seq 1 do usuário A", eventRow(a, 1))
	p.accept("o MESMO seq 1 no usuário B", eventRow(b, 1))
	p.reject(uniqueViolation, "seq 1 repetido no mesmo usuário A", eventRow(a, 1))
	p.accept("seq 2 do usuário A", eventRow(a, 2))

	p.reject(checkViolation, "seq 0", eventRow(a, 0))
	p.reject(checkViolation, "entity_type fora da lista", eventRow(a, 3).lit("entity_type", "'dictionary_entry'"))
	p.reject(checkViolation, "operation fora da lista", eventRow(a, 3).lit("operation", "'delete'"))
	p.accept("operation tombstone", eventRow(a, 3).lit("operation", "'tombstone'"))
}

// mutationRow é uma mutação aplicada válida: do aparelho device (que tem de ser
// do usuário user), sobre um ditado qualquer.
func mutationRow(user, device string) *row {
	return newRow("applied_mutations").
		val("user_id", user).
		val("device_id", device).
		lit("entity_type", "'transcription'").
		lit("entity_id", "gen_random_uuid()").
		lit("status", "'applied'").
		lit("result_version", "1")
}

func TestPostgres_MutationIDEhPorUsuario(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()
	deviceA, deviceB := p.newDevice(a), p.newDevice(b)
	mutation := p.newID()

	p.accept("mutation_id do usuário A", mutationRow(a, deviceA).val("mutation_id", mutation))
	p.accept("o MESMO mutation_id no usuário B", mutationRow(b, deviceB).val("mutation_id", mutation))
	p.reject(uniqueViolation, "mutation_id repetido no mesmo usuário A (o reenvio)",
		mutationRow(a, deviceA).val("mutation_id", mutation))

	p.reject(checkViolation, "status duplicate nunca é gravado",
		mutationRow(a, deviceA).val("mutation_id", p.newID()).lit("status", "'duplicate'"))
	p.reject(checkViolation, "merged só vale para applied",
		mutationRow(a, deviceA).val("mutation_id", p.newID()).lit("status", "'conflict'").lit("merged", "true"))
	p.accept("applied com merged",
		mutationRow(a, deviceA).val("mutation_id", p.newID()).lit("merged", "true"))
	p.accept("gone com motivo",
		mutationRow(a, deviceA).val("mutation_id", p.newID()).lit("status", "'gone'").lit("result_code", "'apagado'").drop("result_version"))
}

// A mutação grava a entidade e o aparelho, e nenhum dos três pode faltar: sem
// a entidade, um mutation_id reusado para OUTRA entidade viraria "duplicate" e
// a mudança se perderia em silêncio (a F2 compara a entidade do reenvio com a
// gravada).
func TestPostgres_MutacaoGuardaEntidadeEAparelho(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	device := p.newDevice(user)
	mutation := p.newID()
	entity := p.newID()

	p.accept("mutação com a entidade e o aparelho",
		mutationRow(user, device).val("mutation_id", mutation).val("entity_id", entity))

	// O reenvio do mesmo mutation_id colide, e a linha gravada diz qual era a
	// entidade: é com ela que o servidor decide entre "duplicate" e "rejected".
	p.reject(uniqueViolation, "o mesmo mutation_id para OUTRA entidade",
		mutationRow(user, device).val("mutation_id", mutation).val("entity_id", p.newID()))
	var gotType, gotEntity string
	if err := p.conn.QueryRow(p.ctx, "SELECT entity_type, entity_id::text FROM applied_mutations WHERE user_id = $1 AND mutation_id = $2",
		user, mutation).Scan(&gotType, &gotEntity); err != nil {
		t.Fatal(err)
	}
	if gotType != "transcription" || gotEntity != entity {
		t.Errorf("a linha gravada diz %s/%s, quero transcription/%s", gotType, gotEntity, entity)
	}

	p.reject(notNullViolation, "mutação sem aparelho",
		mutationRow(user, device).val("mutation_id", p.newID()).val("device_id", nil))
	p.reject(notNullViolation, "mutação sem entity_type",
		mutationRow(user, device).val("mutation_id", p.newID()).drop("entity_type"))
	p.reject(notNullViolation, "mutação sem entity_id",
		mutationRow(user, device).val("mutation_id", p.newID()).drop("entity_id"))
}

// A operação da primeira vez (ADR-019, "Decisões da F8"; a 0009 corrigida no
// lugar pela F8c): upsert ou delete, e nula quando a mandada não era da lista
// (a recusa operacao_invalida). Fora disso, o banco recusa: ele nunca guarda
// texto livre do cliente nesta tabela. Sem DEFAULT: a linha sem a coluna fica
// nula, e não "upsert".
func TestPostgres_MutacaoGuardaAOperacao(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	device := p.newDevice(user)

	p.accept("o aprender (upsert)", mutationRow(user, device).val("mutation_id", p.newID()).
		lit("entity_type", "'learned'").lit("operation", "'upsert'"))
	p.accept("o desfazer (delete)", mutationRow(user, device).val("mutation_id", p.newID()).
		lit("entity_type", "'learned'").lit("operation", "'delete'"))
	p.accept("a recusa de uma operação fora da lista (nula)", mutationRow(user, device).val("mutation_id", p.newID()).
		lit("status", "'rejected'").lit("result_code", "'operacao_invalida'").drop("result_version").val("operation", nil))
	p.reject(checkViolation, "tombstone (só da nuvem, nunca de uma mutação)",
		mutationRow(user, device).val("mutation_id", p.newID()).lit("operation", "'tombstone'"))
	p.reject(checkViolation, "texto livre do cliente",
		mutationRow(user, device).val("mutation_id", p.newID()).val("operation", "apagar tudo"))
	p.reject(checkViolation, "a caixa conta (o servidor grava a forma da lista)",
		mutationRow(user, device).val("mutation_id", p.newID()).lit("operation", "'DELETE'"))

	mutation := p.newID()
	p.accept("sem a coluna", mutationRow(user, device).val("mutation_id", mutation))
	var op *string
	p.scan(&op, "SELECT operation FROM applied_mutations WHERE user_id = $1 AND mutation_id = $2", user, mutation)
	if op != nil {
		t.Errorf("a linha sem a coluna operation ficou com %q, quero nula (sem DEFAULT)", *op)
	}
}

func TestPostgres_AparelhoDeOutroUsuarioEhRecusado(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()
	deviceA, deviceB := p.newDevice(a), p.newDevice(b)

	// Do próprio usuário, ou sem aparelho (o celular): aceito.
	p.accept("ditado de A com o aparelho de A", tRow(a, 1).val("device_id", deviceA))
	p.accept("ditado de A sem aparelho", tRow(a, 2).val("device_id", nil))
	p.accept("texto e lixeira de A mudados por último pelo aparelho de A", tRow(a, 7).val("text_device_id", deviceA).val("trash_device_id", deviceA))
	p.accept("evento de A com o aparelho de A", eventRow(a, 1).val("device_id", deviceA))
	p.accept("mutação de A com o aparelho de A", mutationRow(a, deviceA).val("mutation_id", p.newID()))

	// Do outro usuário: recusado pelas chaves estrangeiras compostas.
	p.reject(foreignKeyViolation, "ditado de A com o aparelho de B", tRow(a, 3).val("device_id", deviceB))
	p.reject(foreignKeyViolation, "selo de conflito de A com o aparelho de B",
		tRow(a, 4).lit("conflict_final_text", "'perdeu'").lit("conflict_at", "now()").val("conflict_device_id", deviceB))
	p.reject(foreignKeyViolation, "texto de A mudado por último pelo aparelho de B", tRow(a, 5).val("text_device_id", deviceB))
	p.reject(foreignKeyViolation, "lixeira de A mudada por último pelo aparelho de B", tRow(a, 6).val("trash_device_id", deviceB))
	p.reject(foreignKeyViolation, "evento de A com o aparelho de B", eventRow(a, 2).val("device_id", deviceB))
	p.reject(foreignKeyViolation, "mutação de A com o aparelho de B", mutationRow(a, deviceB).val("mutation_id", p.newID()))
}

// ---------------------------------------------------------------------------
// O contador por usuário (UPDATE ... RETURNING)
// ---------------------------------------------------------------------------

// nextSeq é o primeiro comando de toda transação de mudança (ADR-019,
// "Gravação única").
func nextSeq(ctx context.Context, tx pgx.Tx, user string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx,
		"UPDATE user_sync_counters SET last_seq = last_seq + 1 WHERE user_id = $1 RETURNING last_seq", user).Scan(&seq)
	return seq, err
}

func insertEvent(ctx context.Context, tx pgx.Tx, user string, seq int64) error {
	sql, args := eventRow(user, seq).sql()
	_, err := tx.Exec(ctx, sql, args...)
	return err
}

func (p *pg) lastSeq(user string) int64 {
	p.t.Helper()
	var seq int64
	p.scan(&seq, "SELECT last_seq FROM user_sync_counters WHERE user_id = $1", user)
	return seq
}

func TestPostgres_ContadorNaoPulaNumeroComRollback(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()

	step := func(user string, commit bool) int64 {
		t.Helper()
		tx, err := p.conn.Begin(p.ctx)
		if err != nil {
			t.Fatal(err)
		}
		seq, err := nextSeq(p.ctx, tx, user)
		if err != nil {
			_ = tx.Rollback(p.ctx)
			t.Fatalf("nextSeq: %v", err)
		}
		if !commit {
			if err := tx.Rollback(p.ctx); err != nil {
				t.Fatal(err)
			}
			return seq
		}
		if err := insertEvent(p.ctx, tx, user, seq); err != nil {
			_ = tx.Rollback(p.ctx)
			t.Fatalf("insertEvent: %v", err)
		}
		if err := tx.Commit(p.ctx); err != nil {
			t.Fatal(err)
		}
		return seq
	}

	if got := step(a, false); got != 1 {
		t.Fatalf("primeira transação (com rollback) recebeu seq %d, quero 1", got)
	}
	if got := p.lastSeq(a); got != 0 {
		t.Errorf("depois do rollback o contador está em %d, quero 0 (o rollback devolve o número)", got)
	}
	if got := step(a, true); got != 1 {
		t.Errorf("a transação seguinte recebeu seq %d, quero 1 de novo", got)
	}
	if got := step(a, true); got != 2 {
		t.Errorf("a seguinte recebeu seq %d, quero 2", got)
	}
	if got := step(a, false); got != 3 {
		t.Errorf("uma com rollback recebeu seq %d, quero 3", got)
	}
	if got := step(a, true); got != 3 {
		t.Errorf("depois dela, seq %d, quero 3 (sem buraco)", got)
	}

	// O contador de B é independente do de A.
	if got := step(b, true); got != 1 {
		t.Errorf("o primeiro seq do usuário B é %d, quero 1", got)
	}
	if got := p.lastSeq(a); got != 3 {
		t.Errorf("contador de A = %d, quero 3", got)
	}
}

func TestPostgres_ContadorSerializaDoisEscritores(t *testing.T) {
	pgtest.Require(t) // sem Postgres, o teste inteiro aparece como pulado
	for _, rollback := range []bool{false, true} {
		name := "A confirma"
		if rollback {
			name = "A desfaz"
		}
		t.Run(name, func(t *testing.T) {
			p := newPG(t)
			user := p.newUser()
			connB := pgtest.Connect(t, p.url)

			// A pega o contador e segura a trava até o fim da transação.
			txA, err := p.conn.Begin(p.ctx)
			if err != nil {
				t.Fatal(err)
			}
			seqA, err := nextSeq(p.ctx, txA, user)
			if err != nil || seqA != 1 {
				t.Fatalf("A: seq %d, erro %v; quero 1", seqA, err)
			}

			type result struct {
				seq int64
				err error
			}
			done := make(chan result, 1)
			go func() {
				txB, err := connB.Begin(p.ctx)
				if err != nil {
					done <- result{0, err}
					return
				}
				seq, err := nextSeq(p.ctx, txB, user)
				if err == nil {
					err = insertEvent(p.ctx, txB, user, seq)
				}
				if err != nil {
					_ = txB.Rollback(p.ctx)
					done <- result{seq, err}
					return
				}
				done <- result{seq, txB.Commit(p.ctx)}
			}()

			// B tem de ficar esperando enquanto A não termina.
			select {
			case r := <-done:
				t.Fatalf("B terminou (seq %d, erro %v) com a transação de A ainda aberta: o contador não serializou", r.seq, r.err)
			case <-time.After(500 * time.Millisecond):
			}
			// E não é só demora: o Postgres tem de mostrar uma sessão esperando
			// uma trava (B esperando a transação de A). Uma terceira conexão
			// olha, porque a de A está dentro da transação.
			monitor := pgtest.Connect(t, p.url)
			waiting := 0
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
				if err := monitor.QueryRow(p.ctx, `SELECT count(*) FROM pg_stat_activity
					WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting > 0 {
					break
				}
			}
			if waiting == 0 {
				t.Error("nenhuma sessão está esperando uma trava: B não está bloqueado pelo contador de A")
			}

			if rollback {
				if err := txA.Rollback(p.ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := insertEvent(p.ctx, txA, user, seqA); err != nil {
					t.Fatal(err)
				}
				if err := txA.Commit(p.ctx); err != nil {
					t.Fatal(err)
				}
			}

			var r result
			select {
			case r = <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("B não terminou depois de A liberar o contador")
			}
			if r.err != nil {
				t.Fatalf("B: %v", r.err)
			}

			wantSeqB, wantEvents := int64(2), 2
			if rollback {
				// O rollback de A devolveu o número: B recebe o 1, sem buraco.
				wantSeqB, wantEvents = 1, 1
			}
			if r.seq != wantSeqB {
				t.Errorf("B recebeu seq %d, quero %d", r.seq, wantSeqB)
			}
			var events int
			p.scan(&events, "SELECT count(*) FROM sync_events WHERE user_id = $1", user)
			if events != wantEvents {
				t.Errorf("%d eventos, quero %d", events, wantEvents)
			}
			if got := p.lastSeq(user); got != int64(wantEvents) {
				t.Errorf("contador = %d, quero %d", got, wantEvents)
			}
		})
	}
}

func TestPostgres_EscritoresConcorrentesNaoDeixamBuracoNemRepetem(t *testing.T) {
	p := newPG(t)
	user := p.newUser()

	const workers, perWorker = 8, 12
	conns := make([]*pgx.Conn, workers)
	for i := range conns {
		conns[i] = pgtest.Connect(t, p.url)
	}

	var committed atomic.Int64
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				tx, err := conns[w].Begin(p.ctx)
				if err != nil {
					errs <- err
					return
				}
				seq, err := nextSeq(p.ctx, tx, user)
				if err == nil {
					err = insertEvent(p.ctx, tx, user, seq)
				}
				if err != nil {
					_ = tx.Rollback(p.ctx)
					errs <- err
					return
				}
				if (w+i)%3 == 0 {
					// Um terço das transações desiste: o número tem de voltar.
					err = tx.Rollback(p.ctx)
				} else if err = tx.Commit(p.ctx); err == nil {
					committed.Add(1)
				}
				if err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("escritor: %v", err)
	}

	n := committed.Load()
	if n == 0 {
		t.Fatal("nenhuma transação confirmou")
	}
	var count, minSeq, maxSeq int64
	if err := p.conn.QueryRow(p.ctx,
		"SELECT count(*), min(seq), max(seq) FROM sync_events WHERE user_id = $1", user).Scan(&count, &minSeq, &maxSeq); err != nil {
		t.Fatal(err)
	}
	if count != n || minSeq != 1 || maxSeq != n {
		t.Errorf("%d eventos, seq de %d a %d; quero %d eventos de 1 a %d, sem buraco e sem repetição", count, minSeq, maxSeq, n, n)
	}
	if got := p.lastSeq(user); got != n {
		t.Errorf("contador = %d, quero %d", got, n)
	}
}

func TestPostgres_ContadorRestricoes(t *testing.T) {
	p := newPG(t)
	user := p.newUser()

	if err := p.try("UPDATE user_sync_counters SET tombstone_watermark = 5 WHERE user_id = $1", user); sqlState(err) != checkViolation {
		t.Errorf("marca d'água acima do last_seq: erro %v, quero SQLSTATE %s", err, checkViolation)
	}
	if err := p.try("UPDATE user_sync_counters SET last_seq = -1 WHERE user_id = $1", user); sqlState(err) != checkViolation {
		t.Errorf("last_seq negativo: erro %v, quero SQLSTATE %s", err, checkViolation)
	}
	// O epoch é gerado pela aplicação (UUID v7), sem DEFAULT: faltar é erro de
	// NOT NULL (que o Postgres confere antes de olhar a chave estrangeira).
	if err := p.try("INSERT INTO user_sync_counters (user_id) VALUES ($1)", p.newID()); sqlState(err) != notNullViolation {
		t.Errorf("contador sem sync_epoch: erro %v, quero SQLSTATE %s", err, notNullViolation)
	}
	if err := p.try("INSERT INTO user_sync_counters (user_id, sync_epoch) VALUES ($1, gen_random_uuid())", user); sqlState(err) != uniqueViolation {
		t.Errorf("segundo contador do mesmo usuário: erro %v, quero SQLSTATE %s", err, uniqueViolation)
	}

	// A marca d'água sobe com GREATEST e nunca desce por esse caminho.
	p.exec("UPDATE user_sync_counters SET last_seq = 10 WHERE user_id = $1", user)
	for _, seq := range []int64{7, 3, 9} {
		p.exec("UPDATE user_sync_counters SET tombstone_watermark = GREATEST(tombstone_watermark, $2) WHERE user_id = $1", user, seq)
	}
	var watermark int64
	p.scan(&watermark, "SELECT tombstone_watermark FROM user_sync_counters WHERE user_id = $1", user)
	if watermark != 9 {
		t.Errorf("marca d'água = %d depois de 7, 3 e 9; quero 9 (o maior)", watermark)
	}
}

// ---------------------------------------------------------------------------
// As constraints de cada tabela
// ---------------------------------------------------------------------------

func tRow(user string, seq int64) *row {
	return newRow("transcriptions").
		lit("id", "gen_random_uuid()").
		val("user_id", user).
		lit("source_platform", "'windows'").
		lit("raw_text", "'texto bruto'").
		lit("final_text", "'texto final'").
		lit("mode", "'smart'").
		lit("asr_provider", "'whisper-local'").
		lit("status", "'done'").
		lit("created_at", "now()").
		val("last_seq", seq)
}

func TestPostgres_TranscricaoRestricoes(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	device := p.newDevice(user)
	var seq int64
	next := func() int64 { seq++; return seq }

	p.accept("ditado válido", tRow(user, next()))
	p.accept("ditado válido com todos os campos opcionais", tRow(user, next()).
		val("device_id", device).lit("language", "'pt-BR'").lit("asr_model", "'small-q4_0'").
		lit("duration_ms", "3200").lit("processing_ms", "410").lit("confidence", "0.91"))
	p.accept("ditado na lixeira (deleted_at e purge_at)", tRow(user, next()).
		lit("deleted_at", "now()").lit("purge_at", "now() + interval '30 days'"))
	p.accept("ditado com o selo de conflito", tRow(user, next()).
		lit("conflict_final_text", "'versão que perdeu'").lit("conflict_at", "now()").val("conflict_device_id", device))
	p.accept("selo de conflito vindo do celular (sem aparelho)", tRow(user, next()).
		lit("conflict_final_text", "'versão que perdeu'").lit("conflict_at", "now()"))

	p.reject(checkViolation, "status completed (o servidor em memória gravava isso)", tRow(user, next()).lit("status", "'completed'"))
	p.accept("status error", tRow(user, next()).lit("status", "'error'"))
	p.reject(checkViolation, "mode fora da lista", tRow(user, next()).lit("mode", "'turbo'"))
	p.reject(checkViolation, "last_seq zero", tRow(user, 0))
	p.reject(checkViolation, "version zero", tRow(user, next()).val("version", int64(0)))
	p.reject(checkViolation, "confidence acima de 1", tRow(user, next()).lit("confidence", "1.5"))
	p.reject(checkViolation, "duration_ms negativo", tRow(user, next()).lit("duration_ms", "-1"))
	p.reject(checkViolation, "purge_at sem deleted_at", tRow(user, next()).lit("purge_at", "now()"))
	p.reject(checkViolation, "conflict_final_text sem conflict_at", tRow(user, next()).lit("conflict_final_text", "'x'"))
	p.reject(checkViolation, "conflict_at sem conflict_final_text", tRow(user, next()).lit("conflict_at", "now()"))
	p.reject(checkViolation, "conflict_device_id sem conflict_final_text", tRow(user, next()).val("conflict_device_id", device))

	// F2: a versão em que o texto e a lixeira mudaram por último nunca passa da
	// versão da linha, e começa em 1.
	p.accept("text_version e trash_version até a versão", tRow(user, next()).
		val("version", int64(5)).val("text_version", int64(5)).val("trash_version", int64(3)))
	p.reject(checkViolation, "text_version maior que a versão", tRow(user, next()).val("text_version", int64(2)))
	p.reject(checkViolation, "trash_version maior que a versão", tRow(user, next()).val("trash_version", int64(2)))
	p.reject(checkViolation, "text_version zero", tRow(user, next()).val("text_version", int64(0)))
	p.reject(checkViolation, "trash_version zero", tRow(user, next()).val("trash_version", int64(0)))

	// created_at é a data do ditado, a do PC: sem DEFAULT, não pode faltar.
	p.reject(notNullViolation, "ditado sem created_at (a nuvem não pode inventar a data)", tRow(user, next()).drop("created_at"))

	// last_seq único por usuário.
	dup := next()
	p.accept("last_seq novo", tRow(user, dup))
	p.reject(uniqueViolation, "last_seq repetido no mesmo usuário", tRow(user, dup))
	p.accept("o mesmo last_seq em outro usuário", tRow(p.newUser(), dup))

	// target_app saiu: a coluna não existe.
	p.reject(undefinedColumn, "target_app (contexto não vai para a nuvem)", tRow(user, next()).lit("target_app", "'chrome'"))
}

// O id do ditado vem do cliente (UUID v7 do PC), então a PK é por usuário: o
// mesmo id em duas contas são duas linhas, e o push de uma conta nunca colide
// com a linha de outra nem revela que o id existe lá (ADR-019, "Decisões da
// F1"). Dentro do mesmo usuário o id continua único.
func TestPostgres_TranscricaoIDEhPorUsuario(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()
	shared := p.newID()

	p.accept("ditado do usuário A", tRow(a, 1).val("id", shared))
	p.accept("o MESMO id de ditado no usuário B", tRow(b, 1).val("id", shared))
	p.reject(uniqueViolation, "o mesmo id repetido no mesmo usuário A", tRow(a, 2).val("id", shared))

	// O upsert do push (F2) é por (user_id, id): o do usuário B não toca a
	// linha do A, mesmo com o mesmo id.
	p.exec(`UPDATE transcriptions SET final_text = 'só do B' WHERE user_id = $1 AND id = $2`, b, shared)
	var textA, textB string
	p.scan(&textA, "SELECT final_text FROM transcriptions WHERE user_id = $1 AND id = $2", a, shared)
	p.scan(&textB, "SELECT final_text FROM transcriptions WHERE user_id = $1 AND id = $2", b, shared)
	if textA != "texto final" || textB != "só do B" {
		t.Errorf("final_text de A = %q e de B = %q; quero %q e %q", textA, textB, "texto final", "só do B")
	}
	tag, err := p.conn.Exec(p.ctx, `INSERT INTO transcriptions
		(id, user_id, source_platform, raw_text, final_text, mode, asr_provider, status, created_at, last_seq)
		VALUES ($2, $1, 'windows', 'x', 'x', 'smart', 'whisper-local', 'done', now(), 9)
		ON CONFLICT (user_id, id) DO NOTHING`, a, shared)
	if err != nil || tag.RowsAffected() != 0 {
		t.Errorf("ON CONFLICT (user_id, id) no id repetido: %d linhas, erro %v; quero 0 linhas e nenhum erro", tag.RowsAffected(), err)
	}
}

func TestPostgres_CorrecaoNaoApagaComATranscricao(t *testing.T) {
	// A 0006 fica sem uso na etapa 5, mas o ON DELETE SET NULL dela tem de
	// funcionar de verdade (a leitura estática só vê o texto). A chave é
	// composta (user_id, transcription_id), e o SET NULL leva a lista de
	// colunas: só o transcription_id é zerado, o user_id fica.
	p := newPG(t)
	user, other := p.newUser(), p.newUser()
	tid := p.newID()
	p.accept("ditado", tRow(user, 1).val("id", tid))
	p.exec(`INSERT INTO corrections (id, user_id, transcription_id, from_text, to_text, normalized_from, normalized_to)
		VALUES (gen_random_uuid(), $1, $2, 'n oito n', 'n8n', 'n oito n', 'n8n')`, user, tid)

	// A correção de outro usuário não aponta para o ditado deste.
	_, err := p.conn.Exec(p.ctx, `INSERT INTO corrections (id, user_id, transcription_id, from_text, to_text, normalized_from, normalized_to)
		VALUES (gen_random_uuid(), $1, $2, 'x', 'y', 'x', 'y')`, other, tid)
	if sqlState(err) != foreignKeyViolation {
		t.Errorf("correção do usuário B apontando para o ditado do A: erro %v, quero SQLSTATE %s", err, foreignKeyViolation)
	}

	p.exec("DELETE FROM transcriptions WHERE user_id = $1 AND id = $2", user, tid)
	var orphan, kept bool
	p.scan(&orphan, "SELECT transcription_id IS NULL FROM corrections WHERE user_id = $1", user)
	p.scan(&kept, "SELECT count(*) = 1 FROM corrections WHERE user_id = $1", user)
	if !orphan {
		t.Error("apagar o ditado não zerou corrections.transcription_id (ON DELETE SET NULL)")
	}
	if !kept {
		t.Error("apagar o ditado apagou a correção (era para só perder a referência)")
	}
}

// O usuário (ADR-019, F3): o sub é a chave e fica fixado; o e-mail é guardado
// em minúsculas, como a lista o compara.
func TestPostgres_Usuario(t *testing.T) {
	p := newPG(t)
	base := func(seed string) *row {
		return newRow("users").
			lit("id", "gen_random_uuid()").
			val("google_sub", "sub-"+seed).
			val("email", seed+"@exemplo.test")
	}

	p.accept("usuário válido", base("u1"))
	p.reject(uniqueViolation, "o mesmo sub em outra linha", base("u2").val("google_sub", "sub-u1"))
	p.reject(uniqueViolation, "o mesmo e-mail com outro sub (D7)", base("u3").val("email", "u1@exemplo.test"))
	p.reject(checkViolation, "e-mail com maiúscula", base("u4").val("email", "Mauro@exemplo.test"))
	p.reject(checkViolation, "sub vazio", base("u5").val("google_sub", ""))
	p.reject(checkViolation, "sub de 256 caracteres", base("u6").lit("google_sub", "repeat('9', 256)"))
	p.accept("sub de 255 caracteres", base("u7").lit("google_sub", "repeat('9', 255)"))
}

func TestPostgres_SessaoWeb(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	base := func(seed string) *row {
		return newRow("web_sessions").
			lit("id", "gen_random_uuid()").
			val("user_id", user).
			lit("token_hmac", tok(seed)).
			lit("label", "'Android · Chrome'").
			lit("expires_at", "now() + interval '30 days'")
	}

	p.accept("sessão válida (o rótulo tem acento e ponto médio)", base("s1"))
	p.reject(uniqueViolation, "o mesmo HMAC em duas sessões", base("s1"))
	p.reject(checkViolation, "HMAC de 31 bytes", base("s2").lit("token_hmac", shortTok("s2")))
	p.reject(checkViolation, "rótulo vazio", base("s3").lit("label", "''"))
	p.reject(checkViolation, "rótulo de 65 caracteres (o user-agent inteiro não cabe)", base("s4").lit("label", "repeat('x', 65)"))
	p.accept("rótulo de 64 caracteres", base("s5").lit("label", "repeat('x', 64)"))
	p.reject(checkViolation, "sessão que já nasce vencida", base("s6").lit("expires_at", "now() - interval '1 day'"))
	p.reject(foreignKeyViolation, "sessão de usuário que não existe", base("s7").val("user_id", p.newID()))
}

func TestPostgres_LoginPendente(t *testing.T) {
	p := newPG(t)
	base := func(seed string) *row {
		return newRow("pending_logins").
			lit("state_hash", tok("state-"+seed)).
			lit("pre_hash", tok("pre-"+seed)).
			lit("code_verifier", "'verificador'").
			lit("nonce", "'nonce'").
			lit("expires_at", "now() + interval '5 minutes'")
	}

	p.accept("login pendente válido", base("1"))
	p.reject(uniqueViolation, "o mesmo state duas vezes", base("1"))
	p.reject(checkViolation, "state_hash de 31 bytes", base("2").lit("state_hash", shortTok("state-2")))
	p.reject(checkViolation, "pre_hash de 31 bytes", base("3").lit("pre_hash", shortTok("pre-3")))
	p.reject(checkViolation, "login que já nasce vencido", base("4").lit("expires_at", "now() - interval '1 minute'"))
	p.reject(notNullViolation, "login sem nonce", base("5").drop("nonce"))

	// return_to: para onde o callback leva a pessoa depois do login. Um caminho
	// da própria origem, nunca uma URL (sem open redirect).
	var def string
	p.scan(&def, "SELECT return_to FROM pending_logins WHERE state_hash = "+tok("state-1"))
	if def != "/" {
		t.Errorf("return_to sem valor = %q, quero \"/\" (o app)", def)
	}
	p.accept("return_to com o caminho e a query da aprovação do pareamento",
		base("6").val("return_to", "/device/authorize?pedido=0190b3a0-0000-7000-8000-00000000f001"))
	for i, bad := range []string{
		"https://exemplo.test/",        // outra origem
		"//exemplo.test/",              // o navegador lê como outro site
		"/\\exemplo.test",              // a barra invertida vira barra no navegador
		"device/authorize",             // não começa com barra
		"",                             // vazio
		"/a b",                         // espaço
		"/a\r\nSet-Cookie: x=1",        // quebra de linha
		"/" + strings.Repeat("a", 256), // 257 caracteres
	} {
		p.reject(checkViolation, fmt.Sprintf("return_to inválido %q", bad),
			base(fmt.Sprintf("r%d", i)).val("return_to", bad))
	}
	p.accept("return_to de 256 caracteres", base("7").val("return_to", "/"+strings.Repeat("a", 255)))

	// Uso único: marcar used_at só acontece uma vez, porque o UPDATE filtra por
	// used_at IS NULL.
	claim := "UPDATE pending_logins SET used_at = now() WHERE state_hash = " + tok("state-1") +
		" AND used_at IS NULL AND expires_at > now()"
	tag, err := p.conn.Exec(p.ctx, claim)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("primeiro uso: %v linhas, erro %v; quero 1", tag.RowsAffected(), err)
	}
	if tag, err = p.conn.Exec(p.ctx, claim); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("segundo uso do mesmo state: %v linhas, erro %v; quero 0", tag.RowsAffected(), err)
	}
}

func TestPostgres_PedidoDePareamento(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	base := func() *row {
		return newRow("pairing_requests").
			lit("id", "gen_random_uuid()").
			lit("code_challenge", "'E9Melhoeoz-desafio-pkce-em-base64url'").
			lit("install_id", "'install-1'").
			lit("device_name", "'Dell G15'").
			lit("platform", "'windows'").
			lit("app_version", "'0.1.0'").
			lit("redirect_port", "49152").
			lit("confirmation_code", "'K7QX'").
			lit("expires_at", "now() + interval '10 minutes'")
	}
	approved := func(seed string) *row {
		return base().val("approved_by", user).lit("approved_at", "now()").lit("code_hash", tok("code-"+seed))
	}

	p.accept("pedido novo, sem aprovação", base())
	p.accept("pedido aprovado, com o hash do code", approved("1"))
	p.accept("pedido aprovado e já usado", approved("2").lit("used_at", "now()"))
	p.reject(uniqueViolation, "o mesmo code_hash em dois pedidos", approved("1"))

	p.reject(checkViolation, "porta privilegiada (80)", base().lit("redirect_port", "80"))
	p.reject(checkViolation, "porta acima de 65535", base().lit("redirect_port", "70000"))
	p.reject(checkViolation, "nome do PC vazio", base().lit("device_name", "''"))
	p.reject(checkViolation, "nome do PC de 81 caracteres", base().lit("device_name", "repeat('x', 81)"))
	p.reject(checkViolation, "código de conferência vazio", base().lit("confirmation_code", "''"))
	p.reject(checkViolation, "pedido que já nasce vencido", base().lit("expires_at", "now() - interval '1 minute'"))
	p.reject(checkViolation, "aprovado por alguém, sem aprovado_em nem hash", base().val("approved_by", user))
	p.reject(checkViolation, "code_hash sem aprovação", base().lit("code_hash", tok("code-3")))
	p.reject(checkViolation, "usado sem ter sido aprovado", base().lit("used_at", "now()"))
	p.reject(checkViolation, "code_hash de 31 bytes", approved("4").lit("code_hash", shortTok("code-4")))
	p.reject(foreignKeyViolation, "aprovado por usuário que não existe", approved("5").val("approved_by", p.newID()))
}

func TestPostgres_TokenDeAparelho(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	device, other := p.newDevice(user), p.newDevice(user)

	p.accept("token válido", newRow("device_tokens").lit("token_hmac", tok("t1")).val("device_id", device))
	p.reject(uniqueViolation, "segundo token para o mesmo aparelho (parear de novo substitui)",
		newRow("device_tokens").lit("token_hmac", tok("t2")).val("device_id", device))
	p.reject(uniqueViolation, "o mesmo HMAC em outro aparelho",
		newRow("device_tokens").lit("token_hmac", tok("t1")).val("device_id", other))
	p.reject(checkViolation, "HMAC de 31 bytes",
		newRow("device_tokens").lit("token_hmac", shortTok("t3")).val("device_id", other))
	p.reject(foreignKeyViolation, "token de aparelho que não existe",
		newRow("device_tokens").lit("token_hmac", tok("t4")).val("device_id", p.newID()))

	p.exec("DELETE FROM devices WHERE id = $1", device)
	var tokens int
	p.scan(&tokens, "SELECT count(*) FROM device_tokens WHERE device_id = $1", device)
	if tokens != 0 {
		t.Errorf("apagar o aparelho deixou %d token(s) para trás (ON DELETE CASCADE)", tokens)
	}
}

func TestPostgres_Lapides(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()
	base := func(user string, entity string, seq int64) *row {
		return newRow("tombstones").
			val("user_id", user).
			lit("entity_type", "'transcription'").
			val("entity_id", entity).
			lit("reason", "'apagado'").
			lit("dictated_at", "now() - interval '10 days'").
			val("seq", seq)
	}
	first := p.newID()

	p.accept("lápide válida", base(a, first, 1))
	p.reject(uniqueViolation, "segunda lápide da mesma entidade", base(a, first, 2))
	p.reject(uniqueViolation, "o mesmo seq para outra entidade do mesmo usuário", base(a, p.newID(), 1))
	p.accept("o mesmo seq em outro usuário", base(b, p.newID(), 1))
	p.accept("a mesma entidade em outro usuário", base(b, first, 2))
	p.reject(checkViolation, "seq zero", base(a, p.newID(), 0))
	p.reject(checkViolation, "lápide de aprendido (ele é desfeito por undone_at)", base(a, p.newID(), 3).lit("entity_type", "'learned'"))
	p.reject(checkViolation, "motivo fora da lista", base(a, p.newID(), 4).lit("reason", "'expirado'"))
	p.reject(notNullViolation, "lápide sem a data do ditado", base(a, p.newID(), 5).drop("dictated_at"))
}

func TestPostgres_Aprendido(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()
	const sharedID = "0190b3a0-0000-7000-8000-00000000f001" // o id derivado da variante: o mesmo em todo PC e usuário
	base := func(user string, seq int64, variant string) *row {
		return newRow("learned_pairs").
			lit("id", "gen_random_uuid()").
			val("user_id", user).
			val("variant", variant).
			val("normalized_variant", strings.ToLower(variant)).
			lit("term", "'ECF'").
			val("last_seq", seq)
	}

	p.accept("par válido", base(a, 1, "SF").val("id", sharedID))
	p.accept("o MESMO id no usuário B (a PK é por usuário)", base(b, 1, "SF").val("id", sharedID))
	p.reject(uniqueViolation, "o mesmo id no mesmo usuário", base(a, 2, "outra").val("id", sharedID))
	p.reject(uniqueViolation, "a mesma variante normalizada com outro id", base(a, 2, "sf"))
	p.reject(uniqueViolation, "last_seq repetido no mesmo usuário", base(a, 1, "esse efe"))

	p.accept("restrito, com o nome do domínio", base(a, 2, "comite").lit("restricted", "true").lit("domain", "'dev'").lit("term", "'commit'"))
	p.reject(checkViolation, "restrito sem domínio (nunca age)", base(a, 3, "comite 2").lit("restricted", "true"))
	p.reject(checkViolation, "domínio vazio", base(a, 3, "comite 3").lit("domain", "''"))
	p.reject(checkViolation, "variante de 81 caracteres", base(a, 3, strings.Repeat("x", 81)))
	p.reject(checkViolation, "termo vazio", base(a, 3, "vazio").lit("term", "''"))
	p.reject(checkViolation, "versão zero", base(a, 3, "versao").val("version", int64(0)))
	p.accept("desfeito (a linha continua, com undone_at)", base(a, 3, "desfeito").lit("undone_at", "now()"))
}

// Os índices da arrumação do job de retenção (ADR-019, "Decisões da F9"):
// o Postgres os criou com as colunas certas (o do sync_event órfão é por
// usuário e idade, a 0008 corrigida no lugar pela F9c).
func TestPostgres_IndicesDaRetencao(t *testing.T) {
	p := newPG(t)
	for name, want := range map[string][2]string{
		"idx_sync_events_changed_at":       {"sync_events", "USING btree (user_id, changed_at)"},
		"idx_applied_mutations_applied_at": {"applied_mutations", "USING btree (applied_at)"},
		"idx_tombstones_dictated_at":       {"tombstones", "USING btree (dictated_at)"},
		"idx_note_sync_events_changed_at":  {"note_sync_events", "USING btree (user_id, changed_at)"},
	} {
		var table, def string
		p.scan(&table, `SELECT tablename::text FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`, name)
		p.scan(&def, `SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`, name)
		if table != want[0] || !strings.HasSuffix(def, want[1]) {
			t.Errorf("%s: tabela %q, %q; quero %q e %q", name, table, def, want[0], want[1])
		}
	}
}

// ---------------------------------------------------------------------------
// O ditado no celular (ADR-021, C3b): a 0018 e a 0019
// ---------------------------------------------------------------------------

// As migrations do ditado no celular são SÓ ADITIVAS (ADR-021, "As
// migrations"): aplicadas sobre um banco que já está em produção, com dados nas
// tabelas de antes, criam as duas tabelas novas, vazias, e não mudam uma linha
// das que existiam. O banco de antes é o das 0001 a 0017 (o da C3a, o último
// que o Mauro tem no ar), com uma conta, um PC, ditados, eventos, uma lápide, um
// aprendido e uma chave da IA cifrada.
func TestPostgres_AsMigrationsDoDitadoNoCelularAplicamSobreUmBancoComDados(t *testing.T) {
	url := pgtest.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load(migrations.FS): %v", err)
	}
	const emProducao = 17
	if len(migs) < emProducao+2 || migs[emProducao].File != "0018_dictation_settings_table.sql" || migs[emProducao+1].File != "0019_dictation_consents_table.sql" {
		t.Fatalf("a 0018 e a 0019 não são as migrations depois da 0017: %d migrations", len(migs))
	}
	conn := pgtest.Connect(t, url)
	if res, err := pgmigrate.Run(ctx, conn, migs[:emProducao]); err != nil || len(res.Applied) != emProducao {
		t.Fatalf("aplicar as 0001 a 0017: %+v, %v", res, err)
	}
	p := &pg{t: t, ctx: ctx, conn: conn, url: url}

	// Os dados de antes.
	a, b := p.newUser(), p.newUser()
	device := p.newDevice(a)
	p.accept("ditado 1", tRow(a, 1))
	p.accept("ditado 2", tRow(a, 2))
	p.accept("ditado do outro", tRow(b, 1))
	p.accept("evento", eventRow(a, 3))
	p.accept("lápide", newRow("tombstones").val("user_id", a).lit("entity_type", "'transcription'").lit("entity_id", "gen_random_uuid()").
		lit("reason", "'apagado'").lit("dictated_at", "now() - interval '10 days'").val("seq", int64(4)))
	p.accept("aprendido", newRow("learned_pairs").lit("id", "gen_random_uuid()").val("user_id", a).val("variant", "SF").
		val("normalized_variant", "sf").lit("term", "'ECF'").val("last_seq", int64(5)))
	p.exec(`INSERT INTO ai_provider_keys (user_id, provider, cipher_key_id, nonce, ciphertext, key_suffix, created_at, updated_at)
		VALUES ($1, 'openai', decode('0102030405060708', 'hex'), decode('010203040506070809101112', 'hex'), decode(repeat('ab', 40), 'hex'), 'WXYZ', now(), now())`, a)

	// A foto do que existe: por tabela, a contagem e um resumo de todas as linhas.
	tabelas := func() []string {
		rows, err := p.conn.Query(p.ctx, `SELECT table_name::text FROM information_schema.tables
			WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			out = append(out, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	foto := func() map[string]string {
		out := map[string]string{}
		for _, tabela := range tabelas() {
			if tabela == "schema_migrations" {
				continue // ganha as duas linhas novas, de propósito
			}
			var n int
			var resumo *string
			p.scan(&n, "SELECT count(*) FROM "+pgx.Identifier{tabela}.Sanitize())
			p.scan(&resumo, "SELECT md5(string_agg(t::text, '|' ORDER BY t::text)) FROM "+pgx.Identifier{tabela}.Sanitize()+" t")
			out[tabela] = fmt.Sprintf("%d:%v", n, resumo != nil)
			if resumo != nil {
				out[tabela] += ":" + *resumo
			}
		}
		return out
	}
	antes := foto()
	if len(antes) != emProducao {
		t.Fatalf("a foto viu %d tabelas, quero %d (uma por migration): %v", len(antes), emProducao, antes)
	}
	if !strings.HasPrefix(antes["ai_provider_keys"], "1:true") || !strings.HasPrefix(antes["transcriptions"], "3:true") {
		t.Fatalf("o banco de antes não tem os dados que o teste pôs: %v", antes)
	}

	// Agora as duas migrations novas.
	res, err := pgmigrate.Run(ctx, conn, migs)
	if err != nil {
		t.Fatalf("aplicar a 0018 e a 0019 sobre o banco com dados: %v", err)
	}
	if len(res.Applied) != len(migs)-emProducao || res.AlreadyApplied != emProducao {
		t.Fatalf("aplicou %d e já havia %d; quero %d e %d", len(res.Applied), res.AlreadyApplied, len(migs)-emProducao, emProducao)
	}
	if res.Applied[0].File != "0018_dictation_settings_table.sql" || res.Applied[1].File != "0019_dictation_consents_table.sql" {
		t.Errorf("as aplicadas: %s, %s", res.Applied[0].File, res.Applied[1].File)
	}

	// Nenhuma linha de antes mudou.
	depois := foto()
	for tabela, quero := range antes {
		if depois[tabela] != quero {
			t.Errorf("a tabela %s mudou com as migrations novas: %s -> %s", tabela, quero, depois[tabela])
		}
	}
	// As duas tabelas novas existem, vazias, e aceitam linhas das contas de antes.
	for _, tabela := range []string{"dictation_settings", "dictation_consents"} {
		if depois[tabela] != "0:false" {
			t.Errorf("a %s nasceu com %s, quero vazia", tabela, depois[tabela])
		}
	}
	var versoes int
	p.scan(&versoes, "SELECT count(*) FROM schema_migrations WHERE version IN (18, 19)")
	if versoes != 2 {
		t.Errorf("schema_migrations tem %d das duas novas", versoes)
	}
	p.exec(`INSERT INTO dictation_settings (user_id, asr_provider, asr_model, use_hint, polish_enabled, polish_provider, polish_model, created_at, updated_at)
		VALUES ($1, 'openai', 'gpt-4o-mini-transcribe', false, false, 'openai', '', now(), now())`, a)
	p.exec(`INSERT INTO dictation_consents (id, user_id, notice_version, asr_provider, polish_provider, accepted_at)
		VALUES ($1, $2, 1, 'openai', NULL, now())`, p.newID(), a)
	// A chave de antes continua legível, e o PC dela também.
	var final string
	p.scan(&final, "SELECT key_suffix FROM ai_provider_keys WHERE user_id = $1", a)
	if final != "WXYZ" {
		t.Errorf("a chave de antes: %q", final)
	}
	var devices int
	p.scan(&devices, "SELECT count(*) FROM devices WHERE id = $1", device)
	if devices != 1 {
		t.Error("o PC de antes sumiu")
	}

	// Rodar de novo não faz nada.
	if res, err := pgmigrate.Run(ctx, conn, migs); err != nil || len(res.Applied) != 0 || res.AlreadyApplied != len(migs) {
		t.Errorf("rodar de novo: %+v, %v", res, err)
	}
}

// Os CHECKs, a PK e o índice único das duas tabelas novas, no Postgres de
// verdade. (A suíte do ditado, em internal/server/pgstore, prova o
// comportamento; aqui, o esquema.)
func TestPostgres_PreferenciasEAceiteDoDitado(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()
	prefs := func(user string) *row {
		return newRow("dictation_settings").
			val("user_id", user).
			lit("asr_provider", "'groq'").
			lit("asr_model", "'whisper-large-v3-turbo'").
			lit("use_hint", "false").
			lit("polish_enabled", "false").
			lit("polish_provider", "'anthropic'").
			lit("polish_model", "''").
			lit("created_at", "now()").
			lit("updated_at", "now()")
	}
	p.accept("preferências válidas", prefs(a))
	p.reject(uniqueViolation, "duas linhas de preferências da mesma conta", prefs(a))
	p.accept("as preferências de outra conta", prefs(b))
	p.reject(foreignKeyViolation, "preferências de um usuário que não existe", prefs(p.newID()))
	p.reject(checkViolation, "transcrição pela anthropic", prefs(p.newUser()).lit("asr_provider", "'anthropic'"))
	p.reject(checkViolation, "Polish pela groq", prefs(p.newUser()).lit("polish_provider", "'groq'"))
	p.reject(checkViolation, "modelo vazio", prefs(p.newUser()).lit("asr_model", "''"))
	p.reject(checkViolation, "modelo com espaço", prefs(p.newUser()).lit("asr_model", "'a b'"))
	p.reject(checkViolation, "modelo com quebra de linha no fim", prefs(p.newUser()).lit("asr_model", "E'gpt\\n'"))
	p.reject(checkViolation, "modelo de 101 caracteres", prefs(p.newUser()).lit("asr_model", "repeat('m', 101)"))
	p.accept("modelo de 100 caracteres", prefs(p.newUser()).lit("asr_model", "repeat('m', 100)"))
	p.reject(checkViolation, "Polish ligado sem modelo", prefs(p.newUser()).lit("polish_enabled", "true"))
	p.accept("Polish ligado com modelo", prefs(p.newUser()).lit("polish_enabled", "true").lit("polish_model", "'claude-x'"))
	p.reject(checkViolation, "a troca antes da criação", prefs(p.newUser()).lit("updated_at", "now() - interval '1 second'"))
	p.reject(notNullViolation, "sem a flag da dica (sem DEFAULT)", prefs(p.newUser()).drop("use_hint"))
	p.reject(notNullViolation, "sem a data de criação (sem DEFAULT)", prefs(p.newUser()).drop("created_at"))

	consent := func(user string) *row {
		return newRow("dictation_consents").
			val("id", p.newID()).
			val("user_id", user).
			lit("notice_version", "1").
			lit("asr_provider", "'openai'").
			lit("accepted_at", "now()")
	}
	c := p.newUser()
	p.accept("aceite válido, sem Polish", consent(c))
	p.reject(uniqueViolation, "um segundo aceite em aberto da mesma conta (o índice único parcial)", consent(c).lit("asr_provider", "'groq'"))
	p.accept("o aceite em aberto de outra conta", consent(p.newUser()))
	p.reject(foreignKeyViolation, "aceite de um usuário que não existe", consent(p.newID()))
	p.reject(checkViolation, "versão zero", consent(p.newUser()).lit("notice_version", "0"))
	p.reject(checkViolation, "transcrição pela anthropic", consent(p.newUser()).lit("asr_provider", "'anthropic'"))
	p.reject(checkViolation, "Polish pela groq", consent(p.newUser()).lit("polish_provider", "'groq'"))
	p.accept("Polish pela anthropic", consent(p.newUser()).lit("polish_provider", "'anthropic'"))
	p.reject(notNullViolation, "sem a data do aceite", consent(p.newUser()).drop("accepted_at"))
	terminado := func(user string) *row {
		return consent(user).lit("accepted_at", "now() - interval '2 hour'").lit("ended_at", "now() - interval '1 hour'").lit("end_reason", "'revogado'")
	}
	d := p.newUser()
	p.accept("aceite revogado", terminado(d))
	p.accept("outro aceite terminado da mesma conta (só um em aberto, vários terminados)", terminado(d).lit("end_reason", "'substituido'"))
	p.accept("e um em aberto ao lado dos terminados", consent(d))
	p.reject(checkViolation, "fim sem motivo", consent(p.newUser()).lit("accepted_at", "now() - interval '2 hour'").lit("ended_at", "now()"))
	p.reject(checkViolation, "motivo sem fim", consent(p.newUser()).lit("end_reason", "'revogado'"))
	p.reject(checkViolation, "motivo fora da lista", terminado(p.newUser()).lit("end_reason", "'expirado'"))
	p.reject(checkViolation, "fim antes do começo", terminado(p.newUser()).lit("ended_at", "now() - interval '3 hour'"))
	p.reject(uniqueViolation, "o mesmo id de aceite duas vezes", consent(p.newUser()).val("id", p.newID()).lit("id", "(SELECT id FROM dictation_consents LIMIT 1)"))
}

// ---------------------------------------------------------------------------
// As notas (ADR-022, N7): a 0020 a 0025
// ---------------------------------------------------------------------------

// noteRow é uma nota válida, sem aparelho nem selo.
func noteRow(user string, seq int64) *row {
	return newRow("notes").
		lit("id", "gen_random_uuid()").
		val("user_id", user).
		lit("titulo", "'Mercado'").
		lit("corpo", "'- [ ] leite'").
		lit("fixada", "false").
		lit("arquivada", "false").
		lit("posicao", "'V'").
		lit("created_at", "now()").
		lit("updated_at", "now()").
		lit("version", "1").
		lit("content_version", "1").
		lit("trash_version", "1").
		val("last_seq", seq)
}

func labelRow(user string, seq int64) *row {
	return newRow("labels").
		lit("id", "gen_random_uuid()").
		val("user_id", user).
		lit("nome", "'Mercado'").
		lit("created_at", "now()").
		lit("updated_at", "now()").
		lit("version", "1").
		val("last_seq", seq)
}

func TestPostgres_Notas(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	device := p.newDevice(user)
	other := p.newDevice(p.newUser())
	var seq int64
	next := func() int64 { seq++; return seq }

	p.accept("nota válida", noteRow(user, next()))
	p.accept("nota com tudo: cor, aparelhos, ditado de origem, lixeira e selo", noteRow(user, next()).
		lit("cor", "'verde'").val("content_device_id", device).val("trash_device_id", device).
		lit("ditado_de_origem", "gen_random_uuid()").lit("deleted_at", "now()").lit("purge_at", "now() + interval '30 days'").
		lit("version", "3").lit("content_version", "2").lit("trash_version", "3").
		lit("conflict_titulo", "''").lit("conflict_corpo", "'o que perdeu'").lit("conflict_at", "now()").val("conflict_device_id", device))
	p.accept("título e corpo vazios", noteRow(user, next()).lit("titulo", "''").lit("corpo", "''"))
	p.accept("título de 1024 bytes", noteRow(user, next()).lit("titulo", "repeat('é', 512)"))
	p.reject(checkViolation, "título de 1025 bytes", noteRow(user, next()).lit("titulo", "repeat('é', 512) || 'x'"))
	p.accept("corpo de 64 KiB", noteRow(user, next()).lit("corpo", "repeat('x', 65536)"))
	p.reject(checkViolation, "corpo de 64 KiB e um byte", noteRow(user, next()).lit("corpo", "repeat('x', 65537)"))
	p.reject(checkViolation, "cor fora da lista", noteRow(user, next()).lit("cor", "'#ff0000'"))
	p.reject(notNullViolation, "título nulo", noteRow(user, next()).lit("titulo", "NULL"))
	p.reject(notNullViolation, "fixada sem valor (sem DEFAULT)", noteRow(user, next()).drop("fixada"))

	for _, bad := range []string{"", "a0", "a-b", "é", strings.Repeat("a", 129)} {
		p.reject(checkViolation, fmt.Sprintf("posição %q", bad), noteRow(user, next()).val("posicao", bad))
	}
	p.accept("posição de 128 caracteres", noteRow(user, next()).val("posicao", strings.Repeat("z", 128)))
	p.accept("posição com zero no meio", noteRow(user, next()).val("posicao", "a0b"))

	p.reject(checkViolation, "purge_at sem deleted_at", noteRow(user, next()).lit("purge_at", "now()"))
	p.reject(checkViolation, "content_version maior que a versão", noteRow(user, next()).lit("content_version", "2"))
	p.reject(checkViolation, "trash_version zero", noteRow(user, next()).lit("trash_version", "0"))
	p.reject(checkViolation, "selo sem o corpo", noteRow(user, next()).lit("conflict_titulo", "'x'").lit("conflict_at", "now()"))
	p.reject(checkViolation, "selo sem a data", noteRow(user, next()).lit("conflict_titulo", "'x'").lit("conflict_corpo", "'y'"))
	p.reject(checkViolation, "aparelho do selo sem o selo", noteRow(user, next()).val("conflict_device_id", device))
	p.reject(foreignKeyViolation, "aparelho de outro usuário", noteRow(user, next()).val("content_device_id", other))

	dup := next()
	p.accept("last_seq novo", noteRow(user, dup))
	p.reject(uniqueViolation, "last_seq repetido", noteRow(user, dup))
	shared := p.newID()
	p.accept("nota com id escolhido", noteRow(user, next()).val("id", shared))
	p.accept("o mesmo id em outra conta", noteRow(p.newUser(), 1).val("id", shared))
	p.reject(uniqueViolation, "o mesmo id na mesma conta", noteRow(user, next()).val("id", shared))

	// A posição é COLLATE "C": a ordem dos bytes ("B" < "a"), a mesma do Go e
	// do PC, qualquer que seja o collation do banco.
	u := p.newUser()
	p.accept("posição a", noteRow(u, 1).val("posicao", "a"))
	p.accept("posição B", noteRow(u, 2).val("posicao", "B"))
	var first string
	p.scan(&first, "SELECT posicao FROM notes WHERE user_id = $1 ORDER BY posicao LIMIT 1", u)
	if first != "B" {
		t.Errorf("ORDER BY posicao começou por %q, quero \"B\" (a ordem dos bytes)", first)
	}
}

func TestPostgres_MarcadoresDeNota(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	p.accept("marcador válido", labelRow(user, 1))
	p.accept("o mesmo nome de novo (a nuvem não impõe nome único)", labelRow(user, 2))
	p.accept("nome de 50 caracteres", labelRow(user, 3).lit("nome", "repeat('é', 50)"))
	p.reject(checkViolation, "nome de 51 caracteres", labelRow(user, 4).lit("nome", "repeat('é', 51)"))
	p.reject(checkViolation, "nome vazio", labelRow(user, 4).lit("nome", "''"))
	p.reject(checkViolation, "versão zero", labelRow(user, 4).lit("version", "0"))
	p.reject(uniqueViolation, "last_seq repetido", labelRow(user, 1))
	p.reject(notNullViolation, "sem a data (sem DEFAULT)", labelRow(user, 4).drop("created_at"))

	// As ligações: saem com a nota, e não apontam para labels.
	note, label := p.newID(), p.newID()
	p.accept("nota", noteRow(user, 10).val("id", note))
	link := func(u, n, l string) *row {
		return newRow("note_labels").val("user_id", u).val("note_id", n).val("label_id", l)
	}
	p.accept("ligação com um marcador que ainda não chegou", link(user, note, label))
	p.reject(uniqueViolation, "a mesma ligação duas vezes", link(user, note, label))
	p.reject(foreignKeyViolation, "ligação de uma nota que não existe", link(user, p.newID(), label))
	p.reject(foreignKeyViolation, "ligação com a nota de outro usuário", link(p.newUser(), note, label))
	p.exec("DELETE FROM notes WHERE user_id = $1 AND id = $2", user, note)
	var links int
	p.scan(&links, "SELECT count(*) FROM note_labels WHERE user_id = $1", user)
	if links != 0 {
		t.Errorf("apagar a nota deixou %d ligação(ões) (ON DELETE CASCADE)", links)
	}
}

func TestPostgres_LapidesEEventosDasNotas(t *testing.T) {
	p := newPG(t)
	a, b := p.newUser(), p.newUser()
	device, other := p.newDevice(a), p.newDevice(b)
	tomb := func(user, entityType, id string, seq int64) *row {
		return newRow("note_tombstones").val("user_id", user).val("entity_type", entityType).val("entity_id", id).val("seq", seq)
	}
	id := p.newID()
	p.accept("lápide de nota", tomb(a, "note", id, 1))
	p.accept("lápide de marcador com o mesmo id (outro tipo)", tomb(a, "label", id, 2))
	p.reject(uniqueViolation, "segunda lápide da mesma nota", tomb(a, "note", id, 3))
	p.reject(uniqueViolation, "o mesmo seq", tomb(a, "note", p.newID(), 1))
	p.accept("o mesmo seq em outra conta", tomb(b, "note", id, 1))
	p.reject(checkViolation, "lápide de ditado (essa é a da 0015)", tomb(a, "transcription", p.newID(), 4))
	p.reject(checkViolation, "seq zero", tomb(a, "note", p.newID(), 0))

	ev := func(user string, seq int64) *row {
		return newRow("note_sync_events").val("user_id", user).val("seq", seq).
			lit("entity_type", "'note'").lit("entity_id", "gen_random_uuid()").
			lit("operation", "'upsert'").lit("entity_version", "1").lit("changed_at", "now()")
	}
	p.accept("evento de nota", ev(a, 1).val("device_id", device))
	p.accept("evento de marcador, lápide", ev(a, 2).lit("entity_type", "'label'").lit("operation", "'tombstone'"))
	p.reject(uniqueViolation, "o mesmo seq", ev(a, 1))
	p.reject(checkViolation, "tipo de ditado (esse é o da 0008)", ev(a, 3).lit("entity_type", "'transcription'"))
	p.reject(checkViolation, "operação delete (o evento é upsert ou tombstone)", ev(a, 3).lit("operation", "'delete'"))
	p.reject(checkViolation, "versão zero", ev(a, 3).lit("entity_version", "0"))
	p.reject(foreignKeyViolation, "aparelho de outro usuário", ev(a, 3).val("device_id", other))
	p.reject(notNullViolation, "sem changed_at (sem DEFAULT)", ev(a, 3).drop("changed_at"))
}

func TestPostgres_LigacaoDoDitadoComANota(t *testing.T) {
	p := newPG(t)
	user := p.newUser()
	tid := p.newID()
	p.accept("ditado", tRow(user, 1).val("id", tid))
	link := func(u, id string) *row {
		return newRow("transcription_note_links").val("user_id", u).val("transcription_id", id).lit("note_id", "gen_random_uuid()")
	}
	p.accept("ligação com uma nota que não existe (ela pode chegar depois)", link(user, tid))
	p.reject(uniqueViolation, "uma segunda nota para o mesmo ditado", link(user, tid))
	p.reject(foreignKeyViolation, "ligação de um ditado que não existe", link(user, p.newID()))
	p.reject(foreignKeyViolation, "ligação com o ditado de outro usuário", link(p.newUser(), tid))
	p.exec("DELETE FROM transcriptions WHERE user_id = $1 AND id = $2", user, tid)
	var links int
	p.scan(&links, "SELECT count(*) FROM transcription_note_links WHERE user_id = $1", user)
	if links != 0 {
		t.Errorf("apagar o ditado (a retenção) deixou %d ligação(ões) (ON DELETE CASCADE)", links)
	}
}

// As migrations das notas são SÓ ADITIVAS (ADR-021, "As migrations"):
// aplicadas sobre o banco de antes (as 0001 a 0019, o último que o Mauro tem no
// ar), com dados em todas as tabelas que a nuvem usa, criam as seis tabelas
// novas, vazias, e não mudam uma linha das que existiam.
func TestPostgres_AsMigrationsDasNotasAplicamSobreUmBancoComDados(t *testing.T) {
	url := pgtest.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load(migrations.FS): %v", err)
	}
	const emProducao = 19
	novas := []string{
		"0020_labels_table.sql", "0021_notes_table.sql", "0022_note_labels_table.sql",
		"0023_note_tombstones_table.sql", "0024_note_sync_events_table.sql", "0025_transcription_note_links_table.sql",
	}
	if len(migs) != emProducao+len(novas) {
		t.Fatalf("%d migrations, quero %d (as 19 de antes e as 6 das notas)", len(migs), emProducao+len(novas))
	}
	for i, f := range novas {
		if migs[emProducao+i].File != f {
			t.Fatalf("a migration %d é %s, quero %s", emProducao+i+1, migs[emProducao+i].File, f)
		}
	}
	conn := pgtest.Connect(t, url)
	if res, err := pgmigrate.Run(ctx, conn, migs[:emProducao]); err != nil || len(res.Applied) != emProducao {
		t.Fatalf("aplicar as 0001 a 0019: %+v, %v", res, err)
	}
	p := &pg{t: t, ctx: ctx, conn: conn, url: url}

	a, b := p.newUser(), p.newUser()
	device := p.newDevice(a)
	tid := p.newID()
	p.accept("ditado", tRow(a, 1).val("id", tid).val("device_id", device))
	p.accept("ditado do outro", tRow(b, 1))
	p.accept("evento", eventRow(a, 2))
	p.accept("lápide", newRow("tombstones").val("user_id", a).lit("entity_type", "'transcription'").lit("entity_id", "gen_random_uuid()").
		lit("reason", "'apagado'").lit("dictated_at", "now() - interval '10 days'").val("seq", int64(3)))
	p.accept("aprendido", newRow("learned_pairs").lit("id", "gen_random_uuid()").val("user_id", a).val("variant", "SF").
		val("normalized_variant", "sf").lit("term", "'ECF'").val("last_seq", int64(4)))
	p.accept("mutação", mutationRow(a, device))
	p.exec(`INSERT INTO dictation_settings (user_id, asr_provider, asr_model, use_hint, polish_enabled, polish_provider, polish_model, created_at, updated_at)
		VALUES ($1, 'openai', 'gpt-4o-mini-transcribe', false, false, 'openai', '', now(), now())`, a)

	tabelas := func() []string {
		rows, err := p.conn.Query(p.ctx, `SELECT table_name::text FROM information_schema.tables
			WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			out = append(out, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	foto := func() map[string]string {
		out := map[string]string{}
		for _, tabela := range tabelas() {
			if tabela == "schema_migrations" {
				continue // ganha as linhas novas, de propósito
			}
			var n int
			var resumo *string
			p.scan(&n, "SELECT count(*) FROM "+pgx.Identifier{tabela}.Sanitize())
			p.scan(&resumo, "SELECT md5(string_agg(t::text, '|' ORDER BY t::text)) FROM "+pgx.Identifier{tabela}.Sanitize()+" t")
			out[tabela] = fmt.Sprintf("%d:%v", n, resumo != nil)
			if resumo != nil {
				out[tabela] += ":" + *resumo
			}
		}
		return out
	}
	antes := foto()
	if len(antes) != emProducao {
		t.Fatalf("a foto viu %d tabelas, quero %d: %v", len(antes), emProducao, antes)
	}
	if !strings.HasPrefix(antes["transcriptions"], "2:true") || !strings.HasPrefix(antes["learned_pairs"], "1:true") {
		t.Fatalf("o banco de antes não tem os dados que o teste pôs: %v", antes)
	}

	res, err := pgmigrate.Run(ctx, conn, migs)
	if err != nil {
		t.Fatalf("aplicar as migrations das notas sobre o banco com dados: %v", err)
	}
	if len(res.Applied) != len(novas) || res.AlreadyApplied != emProducao {
		t.Fatalf("aplicou %d e já havia %d; quero %d e %d", len(res.Applied), res.AlreadyApplied, len(novas), emProducao)
	}
	depois := foto()
	for tabela, quero := range antes {
		if depois[tabela] != quero {
			t.Errorf("a tabela %s mudou com as migrations das notas: %s -> %s", tabela, quero, depois[tabela])
		}
	}
	for _, tabela := range []string{"labels", "notes", "note_labels", "note_tombstones", "note_sync_events", "transcription_note_links"} {
		if depois[tabela] != "0:false" {
			t.Errorf("a %s nasceu com %s, quero vazia", tabela, depois[tabela])
		}
	}
	// As tabelas novas aceitam linhas das contas e dos ditados de antes.
	p.accept("nota da conta de antes", noteRow(a, 10).val("content_device_id", device))
	p.accept("ligação do ditado de antes", newRow("transcription_note_links").val("user_id", a).val("transcription_id", tid).lit("note_id", "gen_random_uuid()"))

	if res, err := pgmigrate.Run(ctx, conn, migs); err != nil || len(res.Applied) != 0 || res.AlreadyApplied != len(migs) {
		t.Errorf("rodar de novo: %+v, %v", res, err)
	}
}
