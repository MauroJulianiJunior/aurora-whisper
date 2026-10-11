package pgmigrate_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// Testes de Run e Inspect CONTRA UM POSTGRES DE VERDADE. Cada um pede um banco
// descartável a pgtest.NewDB, que pula o teste quando AURORA_TEST_POSTGRES_URL
// não está definida: o "go test ./..." normal nunca chega ao Docker. Para
// ligar, veja scripts/README.md.

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func realMigrations(t *testing.T) []pgmigrate.Migration {
	t.Helper()
	m, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load(migrations.FS): %v", err)
	}
	return m
}

// alteredCopy devolve as migrations reais, com um comentário a mais no fim do
// arquivo file: o conteúdo muda, a tabela que ele cria não.
func alteredCopy(t *testing.T, file string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		data, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == file {
			data = append(data, []byte("\n-- alterada depois de aplicada\n")...)
			found = true
		}
		out[e.Name()] = &fstest.MapFile{Data: data}
	}
	if !found {
		t.Fatalf("migration %s não existe", file)
	}
	return out
}

func appliedCount(t *testing.T, ctx context.Context, conn *pgx.Conn) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("contar schema_migrations: %v", err)
	}
	return n
}

func tableExists(t *testing.T, ctx context.Context, conn *pgx.Conn, name string) bool {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatalf("procurar a tabela %s: %v", name, err)
	}
	return exists
}

// lockIsFree diz se outra conexão consegue pegar a trava do migrate agora (e
// a solta em seguida).
func lockIsFree(t *testing.T, ctx context.Context, conn *pgx.Conn) bool {
	t.Helper()
	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", pgmigrate.AdvisoryLockKey).Scan(&got); err != nil {
		t.Fatalf("pg_try_advisory_lock: %v", err)
	}
	if got {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", pgmigrate.AdvisoryLockKey); err != nil {
			t.Fatalf("pg_advisory_unlock: %v", err)
		}
	}
	return got
}

func TestRun_DoZeroAplicaTudo(t *testing.T) {
	ctx := testCtx(t)
	migs := realMigrations(t)
	conn := pgtest.Connect(t, pgtest.NewDB(t))

	res, err := pgmigrate.Run(ctx, conn, migs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Applied) != len(migs) || res.AlreadyApplied != 0 {
		t.Errorf("aplicou %d e já havia %d; quero %d e 0", len(res.Applied), res.AlreadyApplied, len(migs))
	}

	rows, err := conn.Query(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("ler schema_migrations: %v", err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var version int
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			t.Fatal(err)
		}
		if i >= len(migs) {
			t.Fatalf("schema_migrations tem linha a mais: versão %d", version)
		}
		if version != migs[i].Version || name != migs[i].Name || checksum != migs[i].Checksum {
			t.Errorf("linha %d de schema_migrations = (%d, %s, %.8s...), quero (%d, %s, %.8s...)",
				i, version, name, checksum, migs[i].Version, migs[i].Name, migs[i].Checksum)
		}
		i++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if i != len(migs) {
		t.Errorf("schema_migrations tem %d linhas, quero %d", i, len(migs))
	}
	rows.Close()

	for _, table := range []string{"users", "transcriptions", "sync_events", "applied_mutations", "user_sync_counters", "learned_pairs"} {
		if !tableExists(t, ctx, conn, table) {
			t.Errorf("a tabela %s não existe depois do migrate", table)
		}
	}
}

func TestRun_DeNovoNaoFazNada(t *testing.T) {
	ctx := testCtx(t)
	migs := realMigrations(t)
	conn := pgtest.Connect(t, pgtest.NewDB(t))

	if _, err := pgmigrate.Run(ctx, conn, migs); err != nil {
		t.Fatalf("primeiro Run: %v", err)
	}
	var before time.Time
	if err := conn.QueryRow(ctx, "SELECT max(applied_at) FROM schema_migrations").Scan(&before); err != nil {
		t.Fatal(err)
	}

	res, err := pgmigrate.Run(ctx, conn, migs)
	if err != nil {
		t.Fatalf("segundo Run: %v", err)
	}
	if len(res.Applied) != 0 || res.AlreadyApplied != len(migs) {
		t.Errorf("segundo Run aplicou %d e viu %d já aplicadas; quero 0 e %d", len(res.Applied), res.AlreadyApplied, len(migs))
	}
	var after time.Time
	if err := conn.QueryRow(ctx, "SELECT max(applied_at) FROM schema_migrations").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before) {
		t.Errorf("o segundo Run mexeu em schema_migrations (applied_at %v -> %v)", before, after)
	}
	if n := appliedCount(t, ctx, conn); n != len(migs) {
		t.Errorf("schema_migrations tem %d linhas, quero %d", n, len(migs))
	}
}

func TestRun_ArquivoAlteradoDepoisDeAplicadoEhRecusado(t *testing.T) {
	ctx := testCtx(t)
	migs := realMigrations(t)
	conn := pgtest.Connect(t, pgtest.NewDB(t))

	// Aplica só as cinco primeiras, como se o binário antigo só as conhecesse.
	if _, err := pgmigrate.Run(ctx, conn, migs[:5]); err != nil {
		t.Fatalf("Run das cinco primeiras: %v", err)
	}

	altered, err := pgmigrate.Load(alteredCopy(t, "0002_devices_table.sql"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := pgmigrate.Run(ctx, conn, altered)
	if !errors.Is(err, pgmigrate.ErrChecksum) {
		t.Fatalf("erro = %v, quero ErrChecksum", err)
	}
	if !strings.Contains(err.Error(), "0002_devices_table.sql") {
		t.Errorf("o erro não diz qual arquivo foi alterado: %v", err)
	}
	if len(res.Applied) != 0 {
		t.Errorf("aplicou %d migrations em cima de uma história que não confere", len(res.Applied))
	}
	if n := appliedCount(t, ctx, conn); n != 5 {
		t.Errorf("schema_migrations tem %d linhas, quero 5 (nada novo pode entrar)", n)
	}
	if tableExists(t, ctx, conn, "web_sessions") {
		t.Error("a 0011 foi aplicada mesmo com uma migration alterada")
	}
}

func TestRun_DoisMigrateSimultaneosNaoAplicamDuasVezes(t *testing.T) {
	ctx := testCtx(t)
	migs := realMigrations(t)
	url := pgtest.NewDB(t)

	const runners = 4
	conns := make([]*pgx.Conn, runners)
	for i := range conns {
		conns[i] = pgtest.Connect(t, url)
	}
	results := make([]pgmigrate.Result, runners)
	errs := make([]error, runners)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < runners; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = pgmigrate.Run(ctx, conns[i], migs)
		}(i)
	}
	close(start)
	wg.Wait()

	total, workers := 0, 0
	for i := 0; i < runners; i++ {
		if errs[i] != nil {
			t.Errorf("Run %d: %v", i, errs[i])
			continue
		}
		total += len(results[i].Applied)
		if len(results[i].Applied) > 0 {
			workers++
		}
	}
	if total != len(migs) {
		t.Errorf("os %d Run juntos aplicaram %d migrations, quero exatamente %d", runners, total, len(migs))
	}
	if workers != 1 {
		t.Errorf("%d Run aplicaram algo; quero um só (os outros esperam a trava e veem tudo aplicado)", workers)
	}
	if n := appliedCount(t, ctx, conns[0]); n != len(migs) {
		t.Errorf("schema_migrations tem %d linhas, quero %d", n, len(migs))
	}
}

// A prova determinística de que Run usa a trava: outra conexão a segura, e Run
// espera, sem criar nada, até ela soltar.
func TestRun_EsperaATravaDeOutraConexao(t *testing.T) {
	ctx := testCtx(t)
	migs := realMigrations(t)
	url := pgtest.NewDB(t)
	holder := pgtest.Connect(t, url)
	runner := pgtest.Connect(t, url)

	if _, err := holder.Exec(ctx, "SELECT pg_advisory_lock($1)", pgmigrate.AdvisoryLockKey); err != nil {
		t.Fatalf("pegar a trava: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := pgmigrate.Run(ctx, runner, migs)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Run terminou (erro: %v) com a trava segurada por outra conexão", err)
	case <-time.After(750 * time.Millisecond):
	}
	if tableExists(t, ctx, holder, "schema_migrations") {
		t.Error("Run já criou schema_migrations antes de pegar a trava")
	}

	if _, err := holder.Exec(ctx, "SELECT pg_advisory_unlock($1)", pgmigrate.AdvisoryLockKey); err != nil {
		t.Fatalf("soltar a trava: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run depois da trava solta: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Run não terminou depois de a trava ser solta")
	}
	if n := appliedCount(t, ctx, holder); n != len(migs) {
		t.Errorf("schema_migrations tem %d linhas, quero %d", n, len(migs))
	}
}

func TestRun_FalhaNoMeioDesfazOArquivoQueFalhouEParaAli(t *testing.T) {
	ctx := testCtx(t)
	migs, err := pgmigrate.Load(mapFS(map[string]string{
		"0001_a.sql": "CREATE TABLE a (id INTEGER);\n",
		// O segundo CREATE TABLE b falha (a tabela já existe). O primeiro, que
		// já tinha rodado dentro da transação do arquivo, tem de ser desfeito.
		"0002_b.sql": "CREATE TABLE b (id INTEGER);\nCREATE TABLE b (id INTEGER);\n",
		"0003_c.sql": "CREATE TABLE c (id INTEGER);\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	conn := pgtest.Connect(t, pgtest.NewDB(t))

	res, err := pgmigrate.Run(ctx, conn, migs)
	if err == nil {
		t.Fatal("esperava erro na 0002")
	}
	if !strings.Contains(err.Error(), "0002_b.sql") {
		t.Errorf("o erro não diz qual arquivo falhou: %v", err)
	}
	if len(res.Applied) != 1 || res.Applied[0].File != "0001_a.sql" {
		t.Errorf("Result.Applied = %v, quero só a 0001", res.Applied)
	}
	if !tableExists(t, ctx, conn, "a") {
		t.Error("a tabela a (0001) deveria existir")
	}
	if tableExists(t, ctx, conn, "b") {
		t.Error("a tabela b existe: o arquivo que falhou não foi desfeito por inteiro")
	}
	if tableExists(t, ctx, conn, "c") {
		t.Error("a 0003 foi aplicada depois de a 0002 falhar")
	}
	if n := appliedCount(t, ctx, conn); n != 1 {
		t.Errorf("schema_migrations tem %d linhas, quero 1", n)
	}
}

func TestRun_SoltaATravaNoFimComSucessoOuErro(t *testing.T) {
	ctx := testCtx(t)
	url := pgtest.NewDB(t)
	runner := pgtest.Connect(t, url)
	other := pgtest.Connect(t, url)

	good, err := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": "CREATE TABLE a (id INTEGER);\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pgmigrate.Run(ctx, runner, good); err != nil {
		t.Fatal(err)
	}
	if !lockIsFree(t, ctx, other) {
		t.Error("a trava continuou presa depois de um Run que deu certo")
	}

	bad, err := pgmigrate.Load(mapFS(map[string]string{
		"0001_a.sql": "CREATE TABLE a (id INTEGER);\n",
		"0002_b.sql": "CREATE TABLE tabela_com_erro_de_sintaxe (;\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pgmigrate.Run(ctx, runner, bad); err == nil {
		t.Fatal("esperava erro na 0002")
	}
	if !lockIsFree(t, ctx, other) {
		t.Error("a trava continuou presa depois de um Run que falhou")
	}
}

func TestInspect_NaoEscreveEDizOQueFalta(t *testing.T) {
	ctx := testCtx(t)
	migs := realMigrations(t)
	conn := pgtest.Connect(t, pgtest.NewDB(t))

	state, err := pgmigrate.Inspect(ctx, conn, migs)
	if err != nil {
		t.Fatalf("Inspect em banco vazio: %v", err)
	}
	if len(state.Applied) != 0 || len(state.Pending) != len(migs) {
		t.Errorf("banco vazio: %d aplicadas e %d pendentes; quero 0 e %d", len(state.Applied), len(state.Pending), len(migs))
	}
	if tableExists(t, ctx, conn, "schema_migrations") {
		t.Error("Inspect criou schema_migrations: ele só pode ler")
	}

	if _, err := pgmigrate.Run(ctx, conn, migs[:3]); err != nil {
		t.Fatal(err)
	}
	state, err = pgmigrate.Inspect(ctx, conn, migs)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Applied) != 3 || len(state.Pending) != len(migs)-3 {
		t.Errorf("com três aplicadas: %d aplicadas e %d pendentes; quero 3 e %d", len(state.Applied), len(state.Pending), len(migs)-3)
	}
	if n := appliedCount(t, ctx, conn); n != 3 {
		t.Errorf("Inspect mudou schema_migrations (%d linhas, quero 3)", n)
	}

	altered, err := pgmigrate.Load(alteredCopy(t, "0001_users_table.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pgmigrate.Inspect(ctx, conn, altered); !errors.Is(err, pgmigrate.ErrChecksum) {
		t.Errorf("Inspect com arquivo alterado: erro = %v, quero ErrChecksum", err)
	}
}
