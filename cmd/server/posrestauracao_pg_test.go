package main

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
)

// O pos-restauracao CONTRA UM POSTGRES DE VERDADE (F11d). Pulam sem
// AURORA_TEST_POSTGRES_URL (pgtest.NewDB).

func hmacDeTeste(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// Um banco sem as migrations: recusa, sem fazer nada.
func TestPosRestauracao_Postgres_RecusaBancoNaoMigrado(t *testing.T) {
	dbURL := pgtest.NewDB(t)
	out, errOut, code := runCmd(t, map[string]string{envDatabaseURL: dbURL}, "pos-restauracao", "-confirmo")
	if code != 1 || !strings.Contains(errOut, "migrations pendentes") || !strings.Contains(errOut, "nada foi feito") || out != "" {
		t.Fatalf("banco não migrado: %d %q %q", code, out, errOut)
	}
	if pw := senhaDe(t, dbURL); pw != "" && strings.Contains(out+errOut, pw) {
		t.Error("a saída traz a senha do banco")
	}
}

// Migrado e com dados: sem -confirmo, nada muda; com ele, o epoch de cada
// conta troca, as sessões saem, os aparelhos são revogados e os tokens saem,
// e a saída tem só as contagens (nenhum id, e-mail nem nome).
func TestPosRestauracao_Postgres_TrocaOEpochERevoga(t *testing.T) {
	dbURL := pgtest.NewDB(t)
	env := map[string]string{envDatabaseURL + sufixoArquivo: arquivoDeSegredo(t, dbURL+"\n")}
	if _, errOut, code := runCmd(t, env, "migrate"); code != 0 {
		t.Fatalf("migrate: %d %q", code, errOut)
	}
	conn := pgtest.Connect(t, dbURL)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	type conta struct{ id, email string }
	contas := []conta{
		{"00000000-0000-7000-8000-0000000000a1", "conta-sigilosa-a@exemplo.test"},
		{"00000000-0000-7000-8000-0000000000b2", "conta-sigilosa-b@exemplo.test"},
	}
	for i, c := range contas {
		exec("INSERT INTO users (id, google_sub, email, display_name) VALUES ($1, $2, $3, 'NOME-SIGILOSO')", c.id, "sub-"+c.id, c.email)
		exec("INSERT INTO user_sync_counters (user_id, sync_epoch) VALUES ($1, gen_random_uuid())", c.id)
		dev := "00000000-0000-7000-8000-00000000d00" + string(rune('1'+i))
		exec("INSERT INTO devices (id, user_id, install_id, name, platform) VALUES ($1, $2, $3, 'PC-SIGILOSO', 'windows')", dev, c.id, "install-"+dev)
		exec("INSERT INTO device_tokens (token_hmac, device_id) VALUES ($1, $2)", hmacDeTeste("token-"+dev), dev)
		exec(`INSERT INTO web_sessions (id, user_id, token_hmac, label, created_at, last_seen_at, expires_at)
			VALUES (gen_random_uuid(), $1, $2, 'Android', now(), now(), now() + interval '1 day')`, c.id, hmacDeTeste("sessao-"+c.id))
	}
	epochs := func() map[string]string {
		t.Helper()
		rows, err := conn.Query(ctx, "SELECT user_id::text, sync_epoch::text FROM user_sync_counters")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, r := range must(pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ U, E string }])) {
			out[r.U] = r.E
		}
		return out
	}
	contar := func(sql string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	antes := epochs()

	// Sem -confirmo: nada muda.
	if _, errOut, code := runCmd(t, env, "pos-restauracao"); code != 2 || !strings.Contains(errOut, "nada foi feito") {
		t.Fatalf("sem -confirmo: %d %q", code, errOut)
	}
	if got := epochs(); got[contas[0].id] != antes[contas[0].id] || contar("SELECT count(*) FROM web_sessions") != 2 ||
		contar("SELECT count(*) FROM device_tokens") != 2 {
		t.Fatal("sem -confirmo, o banco mudou")
	}

	out, errOut, code := runCmd(t, env, "pos-restauracao", "-confirmo")
	if code != 0 || errOut != "" {
		t.Fatalf("pos-restauracao -confirmo: %d %q %q", code, out, errOut)
	}
	want := "pos-restauracao: 2 conta(s) com o epoch novo, 2 sessão(ões) derrubada(s), 2 aparelho(s) revogado(s)\n" +
		"pos-restauracao: todo PC precisa conectar de novo, e o celular, entrar de novo\n"
	if out != want {
		t.Errorf("a saída = %q, quero %q", out, want)
	}
	for _, proibido := range []string{"sigilosa", "SIGILOS", "0000000000a1", "0000000000b2", "d001", "d002", senhaDe(t, dbURL)} {
		if proibido != "" && strings.Contains(out+errOut, proibido) {
			t.Errorf("a saída traz %q", proibido)
		}
	}
	depois := epochs()
	for _, c := range contas {
		if depois[c.id] == antes[c.id] || depois[c.id] == "" {
			t.Errorf("o epoch de %s não mudou", c.id)
		}
	}
	if n := contar("SELECT count(*) FROM web_sessions"); n != 0 {
		t.Errorf("sobraram %d sessões", n)
	}
	if n := contar("SELECT count(*) FROM device_tokens"); n != 0 {
		t.Errorf("sobraram %d tokens", n)
	}
	if n := contar("SELECT count(*) FROM devices WHERE revoked_at IS NULL OR version <> 2"); n != 0 {
		t.Errorf("%d aparelhos sem revogar (ou sem a versão mais um)", n)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
