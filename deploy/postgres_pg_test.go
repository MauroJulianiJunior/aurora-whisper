package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// O SQL do init do Postgres (deploy/postgres/init/10-papel-da-aplicacao.sh)
// e as consultas do conferir-postgres.sh, rodados num Postgres 18 de verdade:
// o de teste, só quando o AURORA_TEST_POSTGRES_URL existe (pgtest). O teste
// tira o SQL do próprio script, troca as variáveis por nomes únicos (os papéis
// são do servidor inteiro, e o teste os apaga no fim) e roda comando por
// comando, pelo superusuário de teste, como a imagem oficial roda o init.

// sqlDoInit devolve os comandos do heredoc do init, sem os meta-comandos do
// psql (\set e \unset), com :"nome" trocado pelo identificador e :'nome' pelo
// literal de vars.
func sqlDoInit(t *testing.T, vars map[string]string) []string {
	t.Helper()
	texto := ler(t, initDoPapel)
	_, depois, ok := strings.Cut(texto, "<<'SQL'\n")
	if !ok {
		t.Fatal("o init não tem o heredoc <<'SQL'")
	}
	corpo, _, ok := strings.Cut(depois, "\nSQL\n")
	if !ok {
		t.Fatal("o heredoc do init não termina em SQL")
	}
	var cmds []string
	for _, l := range strings.Split(corpo, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, `\`) {
			continue
		}
		for k, v := range vars {
			l = strings.ReplaceAll(l, `:"`+k+`"`, pgx.Identifier{v}.Sanitize())
			l = strings.ReplaceAll(l, `:'`+k+`'`, "'"+strings.ReplaceAll(v, "'", "''")+"'")
		}
		if regexp.MustCompile(`:["'][a-z_]+["']`).MatchString(l) {
			t.Fatalf("variável do psql sem valor no teste: %s", l)
		}
		cmds = append(cmds, l)
	}
	return cmds
}

// consultaDoConferir devolve a consulta do conferir-postgres.sh que lê os
// poderes de um papel (a que termina em "WHERE rolname = '$<variavel>'"),
// com as variáveis do bash trocadas.
func consultaDoConferir(t *testing.T, variavel string, vars map[string]string) string {
	t.Helper()
	re := regexp.MustCompile(`consultar "(SELECT concat_ws\([^"]*WHERE rolname = '\$` + variavel + `')"`)
	m := re.FindStringSubmatch(ler(t, conferirPostgre))
	if m == nil {
		t.Fatalf("não achei no conferir-postgres.sh a consulta do papel $%s", variavel)
	}
	q := m[1]
	for k, v := range vars {
		q = strings.ReplaceAll(q, "$"+k+"'", v+"'")
	}
	if strings.Contains(q, "$") {
		t.Fatalf("variável do bash sem valor no teste: %s", q)
	}
	return q
}

func hexAleatorio(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// conectarComo abre uma conexão ao banco, como o papel e a senha dados, no
// mesmo servidor de adminURL.
func conectarComo(ctx context.Context, t *testing.T, adminURL, papel, senha, banco string) (*pgx.Conn, error) {
	t.Helper()
	cfg, err := pgx.ParseConfig(adminURL)
	if err != nil {
		t.Fatal("a URL do Postgres de teste não é válida")
	}
	cfg.User, cfg.Password, cfg.Database = papel, senha, banco
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err == nil {
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
	}
	return conn, err
}

// semPermissao diz se o erro é o "permission denied" ou o "must be owner" do
// Postgres (SQLSTATE 42501).
func semPermissao(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42501"
}

func TestPostgres_InitDeVerdade(t *testing.T) {
	adminURL := pgtest.NewDB(t) // pula sem o AURORA_TEST_POSTGRES_URL
	admin := pgtest.Connect(t, adminURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sufixo := hexAleatorio(t, 6)
	papel := "aw_test_papel_" + sufixo
	backup := "aw_test_backup_" + sufixo
	banco := "aw_test_init_" + sufixo
	senha := hexAleatorio(t, 32)
	senhaBackup := hexAleatorio(t, 32)
	// Roda antes do fecho da conexão do admin (as limpezas vão da última
	// para a primeira).
	t.Cleanup(func() {
		c, cc := context.WithTimeout(context.Background(), 30*time.Second)
		defer cc()
		if _, err := admin.Exec(c, "DROP DATABASE IF EXISTS "+pgx.Identifier{banco}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("apagar o banco do teste: %v", err)
		}
		for _, p := range []string{backup, papel} {
			if _, err := admin.Exec(c, "DROP ROLE IF EXISTS "+pgx.Identifier{p}.Sanitize()); err != nil {
				t.Errorf("apagar o papel %s do teste: %v", p, err)
			}
		}
	})

	cmds := sqlDoInit(t, map[string]string{"papel": papel, "papel_backup": backup, "banco": banco,
		"senha": senha, "senha_backup": senhaBackup})
	for i, cmd := range cmds {
		if _, err := admin.Exec(ctx, cmd); err != nil {
			t.Fatalf("o comando %d do init falhou no Postgres de teste: %v", i+1, err)
		}
	}
	// A sessão do init ficou sem o log de comando (o superusuário pode).
	var lmes string
	if err := admin.QueryRow(ctx, "SHOW log_min_error_statement").Scan(&lmes); err != nil || lmes != "panic" {
		t.Errorf("log_min_error_statement da sessão do init = %q (%v), quero panic", lmes, err)
	}

	// As consultas do conferir-postgres.sh, no Postgres de verdade, dão o que
	// o script espera.
	nomes := map[string]string{"PAPEL": papel, "PAPEL_BACKUP": backup, "BANCO": banco}
	for variavel, quero := range map[string]string{
		"PAPEL":        "false false false false false",
		"PAPEL_BACKUP": "false false false false false true false true",
	} {
		var achado string
		if err := admin.QueryRow(ctx, consultaDoConferir(t, variavel, nomes)).Scan(&achado); err != nil {
			t.Fatalf("a consulta do conferir para $%s falhou: %v", variavel, err)
		}
		if achado != quero {
			t.Errorf("o conferir, para $%s: %q, quero %q", variavel, achado, quero)
		}
	}
	var dono string
	var publicoConecta bool
	if err := admin.QueryRow(ctx, `SELECT pg_get_userbyid(datdba), has_database_privilege('public', datname, 'CONNECT')
		FROM pg_database WHERE datname = $1`, banco).Scan(&dono, &publicoConecta); err != nil {
		t.Fatal(err)
	}
	if dono != papel || publicoConecta {
		t.Errorf("o banco: dono %q (quero %q), PUBLIC conecta = %v (quero false)", dono, papel, publicoConecta)
	}

	// O papel da aplicação, com a senha do arquivo, entra e aplica as
	// migrations sem ser superusuário.
	app, err := conectarComo(ctx, t, adminURL, papel, senha, banco)
	if err != nil {
		t.Fatalf("o papel da aplicação não entrou no banco: %v", err)
	}
	ms, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pgmigrate.Run(ctx, app, ms); err != nil {
		t.Fatalf("as migrations não rodaram pelo papel da aplicação: %v", err)
	}

	// O papel do backup entra, lê e trava (ACCESS SHARE, como o pg_dump) cada
	// tabela e cada sequência das migrations, e não escreve nada.
	bk, err := conectarComo(ctx, t, adminURL, backup, senhaBackup, banco)
	if err != nil {
		t.Fatalf("o papel do backup não entrou no banco: %v", err)
	}
	var tabelas, sequencias []string
	rows, err := app.Query(ctx, `SELECT c.relname, c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'S') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var nome, tipo string
		if err := rows.Scan(&nome, &tipo); err != nil {
			t.Fatal(err)
		}
		if tipo == "r" {
			tabelas = append(tabelas, nome)
		} else {
			sequencias = append(sequencias, nome)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(tabelas) < 10 {
		t.Fatalf("as migrations deveriam criar as tabelas: %v", tabelas)
	}
	for _, tb := range tabelas {
		id := pgx.Identifier{"public", tb}.Sanitize()
		var n int64
		if err := bk.QueryRow(ctx, "SELECT count(*) FROM "+id).Scan(&n); err != nil {
			t.Errorf("o backup não leu %s: %v", tb, err)
		}
		tx, err := bk.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "LOCK TABLE "+id+" IN ACCESS SHARE MODE"); err != nil {
			t.Errorf("o backup não travou %s para ler (o pg_dump trava): %v", tb, err)
		}
		_ = tx.Rollback(ctx)
	}
	for _, sq := range sequencias {
		var v int64
		if err := bk.QueryRow(ctx, "SELECT last_value FROM "+pgx.Identifier{"public", sq}.Sanitize()).Scan(&v); err != nil {
			t.Errorf("o backup não leu a sequência %s: %v", sq, err)
		}
	}
	for _, sql := range []string{
		"DELETE FROM public.users",
		"UPDATE public.users SET email = email",
		"TRUNCATE public.transcriptions",
		"INSERT INTO public.schema_migrations SELECT * FROM public.schema_migrations LIMIT 0",
		"DROP TABLE public.users",
		"CREATE TABLE public.aw_test_intrusa (i int)",
		"ALTER TABLE public.users ADD COLUMN aw_test_intrusa int",
	} {
		if _, err := bk.Exec(ctx, sql); !semPermissao(err) {
			t.Errorf("o papel do backup deveria ser recusado (42501) em %q: %v", sql, err)
		}
	}

	// Sem o GRANT CONNECT, o pg_read_all_data não dá a entrada no banco: é
	// por isso que o restaurar.sh dá o CONNECT ao banco novo.
	if _, err := admin.Exec(ctx, "REVOKE CONNECT ON DATABASE "+pgx.Identifier{banco}.Sanitize()+" FROM "+pgx.Identifier{backup}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := conectarComo(ctx, t, adminURL, backup, senhaBackup, banco); err == nil {
		t.Error("sem o CONNECT, o papel do backup ainda entrou no banco")
	}
}
