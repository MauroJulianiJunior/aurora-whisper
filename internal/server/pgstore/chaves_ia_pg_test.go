package pgstore

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/chavestest"
)

// TestChaves_Postgres roda a suíte das chaves da IA (C3a) e do aceite e das
// preferências do ditado no celular (C3b) contra o Postgres: a mesma de
// internal/server (TestChaves_Memoria). O canário varre o texto de TODAS as
// tabelas do banco (o bytea em hex), a ai_provider_keys inclusive.
func TestChaves_Postgres(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	chavestest.Run(t, chavestest.Backend{
		Store: s, Accounts: s, Chaves: s, Ditado: s,
		NewDevice: e.newDeviceWithToken,
		Despejo:   e.despejoDeTudo,
	})
}

// despejoDeTudo devolve o texto de todas as linhas de todas as tabelas do
// esquema public (o bytea sai em hex, \x...).
func (e *pgEnv) despejoDeTudo(t *testing.T) string {
	t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`)
	if err != nil {
		t.Fatalf("listar as tabelas: %v", err)
	}
	tabelas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(tabelas) < 17 {
		t.Fatalf("listar as tabelas: %v (%d)", err, len(tabelas))
	}
	var b strings.Builder
	for _, tab := range tabelas {
		var dump *string
		e.scalar(&dump, "SELECT string_agg(t::text, E'\\n') FROM "+pgx.Identifier{"public", tab}.Sanitize()+" t")
		if dump != nil {
			b.WriteString(tab + ":\n" + *dump + "\n")
		}
	}
	if !strings.Contains(b.String(), "ai_provider_keys:") {
		t.Fatal("o despejo não leu a ai_provider_keys")
	}
	return b.String()
}

// Os CHECKs da 0017 no banco de verdade: o que o Go confere antes
// (ConferirChaveDeIA) o banco também recusa, para uma linha posta à mão ou um
// caminho futuro que esqueça a conferência.
func TestPostgres_AsChavesDaIANoBanco(t *testing.T) {
	e := newPG(t)
	u := e.newUser(t)
	inserir := func(provider, id, nonce, cifrado, final string) error {
		_, err := e.pool.Exec(e.ctx, `INSERT INTO ai_provider_keys
			(user_id, provider, cipher_key_id, nonce, ciphertext, key_suffix, created_at, updated_at)
			VALUES ($1, $2, decode($3, 'hex'), decode($4, 'hex'), decode($5, 'hex'), $6, now(), now())`,
			u, provider, id, nonce, cifrado, final)
		return err
	}
	id8, nonce12, cifrado17 := strings.Repeat("ab", 8), strings.Repeat("cd", 12), strings.Repeat("ef", 17)
	codigo := func(err error) string {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return pg.Code
		}
		return ""
	}
	for nome, c := range map[string]struct {
		provider, id, nonce, cifrado, final, quero string
	}{
		"provedor fora da lista":    {"xai", id8, nonce12, cifrado17, "WXYZ", "23514"},
		"provedor em maiúsculas":    {"OpenAI", id8, nonce12, cifrado17, "WXYZ", "23514"},
		"id de 7 bytes":             {"openai", id8[:14], nonce12, cifrado17, "WXYZ", "23514"},
		"nonce de 11 bytes":         {"openai", id8, nonce12[:22], cifrado17, "WXYZ", "23514"},
		"cifrado só da etiqueta":    {"openai", id8, nonce12, cifrado17[:32], "WXYZ", "23514"},
		"cifrado acima do teto":     {"openai", id8, nonce12, strings.Repeat("ef", 2577), "WXYZ", "23514"},
		"fim de 5 caracteres":       {"openai", id8, nonce12, cifrado17, "ABCDE", "23514"},
		"fim com espaço":            {"openai", id8, nonce12, cifrado17, "a b", "23514"},
		"fim com caractere de fora": {"openai", id8, nonce12, cifrado17, "é", "23514"},
	} {
		if got := codigo(inserir(c.provider, c.id, c.nonce, c.cifrado, c.final)); got != c.quero {
			t.Errorf("%s: SQLSTATE %q, quero %s", nome, got, c.quero)
		}
	}
	if err := inserir("openai", id8, nonce12, cifrado17, ""); err != nil {
		t.Fatalf("a linha boa (com o fim vazio): %v", err)
	}
	if got := codigo(inserir("openai", id8, nonce12, cifrado17, "WXYZ")); got != "23505" {
		t.Errorf("duas chaves do mesmo provedor: SQLSTATE %q, quero 23505 (a PK)", got)
	}
	_, err := e.pool.Exec(e.ctx, `UPDATE ai_provider_keys SET updated_at = created_at - interval '1 second' WHERE user_id = $1`, u)
	if got := codigo(err); got != "23514" {
		t.Errorf("a troca antes da criação: SQLSTATE %q, quero 23514", got)
	}
}
