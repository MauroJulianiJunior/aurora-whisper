package deploy

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// O SQL do tirar-cadastro.sh num Postgres de verdade (o de teste, só quando o
// AURORA_TEST_POSTGRES_URL existe, como o postgres_pg_test.go): o teste tira os
// blocos <<'SQL_...' do próprio script, troca :'email' e :'uid' pelo literal
// (é o que o psql faz com o -v) e roda sobre as migrations de verdade.

// sqlDoCadastro devolve o bloco de SQL do script com as variáveis do psql
// trocadas pelos literais.
func sqlDoCadastro(t *testing.T, bloco string, vars map[string]string) string {
	t.Helper()
	// A linha do heredoc termina em "|| true" (o read -d '' sai com 1 no fim do
	// texto, e o set -e pararia): o resto da linha fica de fora do bloco.
	re := regexp.MustCompile(`(?s)<< '` + bloco + `'[^\n]*\n(.*?)\n` + bloco + `\n`)
	m := re.FindStringSubmatch(ler(t, tirarCadastro))
	if m == nil {
		t.Fatalf("o tirar-cadastro.sh não tem o bloco %s", bloco)
	}
	sql := m[1]
	for k, v := range vars {
		sql = strings.ReplaceAll(sql, `:'`+k+`'`, "'"+strings.ReplaceAll(v, "'", "''")+"'")
	}
	if regexp.MustCompile(`:'[a-z_]+'`).MatchString(sql) {
		t.Fatalf("variável do psql sem valor no teste: %s", sql)
	}
	return sql
}

type bancoDoCadastro struct {
	t    *testing.T
	ctx  context.Context
	conn *pgx.Conn
}

func novoBancoDoCadastro(t *testing.T) *bancoDoCadastro {
	t.Helper()
	url := pgtest.NewDB(t) // pula sem o AURORA_TEST_POSTGRES_URL
	conn := pgtest.Connect(t, url)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	ms, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pgmigrate.Run(ctx, conn, ms); err != nil {
		t.Fatalf("as migrations não rodaram: %v", err)
	}
	return &bancoDoCadastro{t: t, ctx: ctx, conn: conn}
}

func (b *bancoDoCadastro) exec(sql string, args ...any) {
	b.t.Helper()
	if _, err := b.conn.Exec(b.ctx, sql, args...); err != nil {
		b.t.Fatalf("%s: %v", sql, err)
	}
}

// pessoa cria o cadastro como o que o "Apagar meus dados" deixa: a linha em
// users, o contador de sincronização e uma sessão.
func (b *bancoDoCadastro) pessoa(id, email string) {
	b.t.Helper()
	b.exec(`INSERT INTO users (id, google_sub, email, display_name) VALUES ($1, $2, $3, 'Fulana')`, id, "sub-"+id, email)
	b.exec(`INSERT INTO user_sync_counters (user_id, sync_epoch) VALUES ($1, gen_random_uuid())`, id)
	// O token_hmac é único na tabela: cada pessoa ganha o seu (o sha256 do id).
	b.exec(`INSERT INTO web_sessions (id, user_id, token_hmac, label, expires_at)
	        VALUES (gen_random_uuid(), $1::uuid, sha256(convert_to($1::uuid::text, 'UTF8')), 'navegador', now() + interval '1 day')`, id)
}

func (b *bancoDoCadastro) contar(sql string, args ...any) int {
	b.t.Helper()
	var n int
	if err := b.conn.QueryRow(b.ctx, sql, args...).Scan(&n); err != nil {
		b.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// restos roda a consulta do script e devolve tabela -> contagem.
func (b *bancoDoCadastro) restos(id string) map[string]string {
	b.t.Helper()
	sql := sqlDoCadastro(b.t, "SQL_RESTOS", map[string]string{"uid": id})
	rows, err := b.conn.Query(b.ctx, sql)
	if err != nil {
		b.t.Fatalf("a consulta de restos falhou: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var tabela, n string
		if err := rows.Scan(&tabela, &n); err != nil {
			b.t.Fatal(err)
		}
		out[strings.TrimPrefix(tabela, "public.")] = n
	}
	if err := rows.Err(); err != nil {
		b.t.Fatal(err)
	}
	return out
}

const (
	idDaPessoa = "0b5f2a4e-7d1c-4e0a-9a53-6c1f0d2b8e91"
	idDeOutra  = "7c3e9a10-2b4d-4f6a-8e15-93a0b7d4c2f8"
)

func TestTirarCadastro_Postgres_AchaPeloEmailSemMaiusculas(t *testing.T) {
	b := novoBancoDoCadastro(t)
	b.pessoa(idDaPessoa, "pessoa@exemplo.com")
	sql := sqlDoCadastro(t, "SQL_ACHAR", map[string]string{"email": "Pessoa@Exemplo.COM"})
	var id, email, criado, nome string
	if err := b.conn.QueryRow(b.ctx, sql).Scan(&id, &email, &criado, &nome); err != nil {
		t.Fatalf("não achou o cadastro: %v", err)
	}
	if id != idDaPessoa || email != "pessoa@exemplo.com" || nome != "Fulana" || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(criado) {
		t.Errorf("achou %q %q %q %q", id, email, criado, nome)
	}
	// Um e-mail que não existe não acha nada, e um com aspas não quebra o SQL.
	for _, e := range []string{"outra@exemplo.com", "x'; DROP TABLE users; --@exemplo.com"} {
		sql := sqlDoCadastro(t, "SQL_ACHAR", map[string]string{"email": e})
		if err := b.conn.QueryRow(b.ctx, sql).Scan(&id, &email, &criado, &nome); err != pgx.ErrNoRows {
			t.Errorf("%q: esperava nenhuma linha, veio %v", e, err)
		}
	}
	if b.contar(`SELECT count(*) FROM users`) != 1 {
		t.Error("a tabela users mudou")
	}
}

// A consulta de restos lê as chaves estrangeiras do próprio banco: tem de
// listar toda tabela das migrations que aponta para users, contar certo, e
// ver o dado que a pessoa ainda tem.
func TestTirarCadastro_Postgres_RestosVeTodaTabelaQueApontaParaUsers(t *testing.T) {
	b := novoBancoDoCadastro(t)
	b.pessoa(idDaPessoa, "pessoa@exemplo.com")
	b.pessoa(idDeOutra, "outra@exemplo.com")

	r := b.restos(idDaPessoa)
	// as tabelas que o "Apagar meus dados" apaga e as duas que ele deixa
	for _, tabela := range strings.Fields("devices transcriptions dictionary_entries corrections user_settings sync_events " +
		"applied_mutations user_sync_counters web_sessions pairing_requests tombstones learned_pairs ai_provider_keys " +
		"dictation_settings dictation_consents labels notes note_labels note_tombstones note_sync_events " +
		"transcription_note_links note_images dictionary_copies") {
		if _, ok := r[tabela]; !ok {
			t.Errorf("a consulta não lista a tabela %s", tabela)
		}
	}
	for tabela, n := range r {
		quer := "0"
		if tabela == "user_sync_counters" || tabela == "web_sessions" {
			quer = "1"
		}
		if n != quer {
			t.Errorf("%s: contou %s, quero %s", tabela, n, quer)
		}
	}

	// Com um aparelho cadastrado, a pessoa ainda tem dado; a outra, não.
	b.exec(`INSERT INTO devices (id, user_id, install_id, name, platform) VALUES (gen_random_uuid(), $1, 'inst-1', 'PC', 'windows')`, idDaPessoa)
	if got := b.restos(idDaPessoa)["devices"]; got != "1" {
		t.Errorf("devices da pessoa: %s, quero 1", got)
	}
	if got := b.restos(idDeOutra)["devices"]; got != "0" {
		t.Errorf("devices da outra: %s, quero 0", got)
	}
}

// Só as duas tabelas que o "Apagar meus dados" deixa podem ter linha na hora de
// tirar o cadastro: o script as trata como sobra. Se uma migration nova criar
// outra tabela com a FK para users, ela entra na contagem (acima) e, com dado,
// faz o script recusar. Aqui, que o SQL de tirar leva o cadastro e SÓ o dele.
func TestTirarCadastro_Postgres_TiraOCadastroENadaDeOutraPessoa(t *testing.T) {
	b := novoBancoDoCadastro(t)
	b.pessoa(idDaPessoa, "pessoa@exemplo.com")
	b.pessoa(idDeOutra, "outra@exemplo.com")

	sql := sqlDoCadastro(t, "SQL_TIRAR", map[string]string{"uid": idDaPessoa})
	tx, err := b.conn.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range strings.Split(sql, ";\n") {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if _, err := tx.Exec(b.ctx, cmd); err != nil {
			_ = tx.Rollback(b.ctx)
			t.Fatalf("%s: %v", cmd, err)
		}
	}
	if err := tx.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}

	for _, tabela := range []string{"users", "user_sync_counters", "web_sessions"} {
		col := "user_id"
		if tabela == "users" {
			col = "id"
		}
		if n := b.contar(`SELECT count(*) FROM `+tabela+` WHERE `+col+` = $1`, idDaPessoa); n != 0 {
			t.Errorf("%s: ficou %d linha(s) da pessoa", tabela, n)
		}
		if n := b.contar(`SELECT count(*) FROM `+tabela+` WHERE `+col+` = $1`, idDeOutra); n != 1 {
			t.Errorf("%s: a outra pessoa tem %d linha(s), quero 1", tabela, n)
		}
	}
}

// Se a pessoa gravou algo entre a conferência e o apagar, a chave estrangeira
// recusa e a transação desfaz tudo: o contador e a sessão continuam.
func TestTirarCadastro_Postgres_DadoNovoFazATransacaoDesfazer(t *testing.T) {
	b := novoBancoDoCadastro(t)
	b.pessoa(idDaPessoa, "pessoa@exemplo.com")
	b.exec(`INSERT INTO devices (id, user_id, install_id, name, platform) VALUES (gen_random_uuid(), $1, 'inst-1', 'PC', 'windows')`, idDaPessoa)

	sql := sqlDoCadastro(t, "SQL_TIRAR", map[string]string{"uid": idDaPessoa})
	tx, err := b.conn.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var falhou bool
	for _, cmd := range strings.Split(sql, ";\n") {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if _, err := tx.Exec(b.ctx, cmd); err != nil {
			falhou = true
			break
		}
	}
	_ = tx.Rollback(b.ctx)
	if !falhou {
		t.Fatal("o DELETE de users deveria ser recusado pela chave estrangeira de devices")
	}
	for _, tabela := range []string{"users", "user_sync_counters", "web_sessions"} {
		col := "user_id"
		if tabela == "users" {
			col = "id"
		}
		if n := b.contar(`SELECT count(*) FROM `+tabela+` WHERE `+col+` = $1`, idDaPessoa); n != 1 {
			t.Errorf("%s: tem %d linha(s) da pessoa depois do desfazer, quero 1", tabela, n)
		}
	}
}
