package pgstore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Depois de restaurar um backup (F11d; restauracao.go), contra o Postgres de
// verdade. Pulam sem AURORA_TEST_POSTGRES_URL. O cenário do backup mais velho
// roda na suíte de contrato (contracttest, Restauracao_*), com o fotografar
// abaixo como o pg_dump e o pg_restore.

// fotografar copia as tabelas do esquema public (menos a das migrations) para
// um esquema à parte, e devolve a restauração NO LUGAR: o TRUNCATE de todas e
// o INSERT de volta, com os gatilhos das chaves estrangeiras desligados na
// transação (session_replication_role = replica, o que o pg_restore
// --disable-triggers faz; o usuário do Postgres de teste é superusuário). O
// pool do Store continua apontando para o mesmo banco.
func (e *pgEnv) fotografar(t *testing.T) func(t *testing.T) {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	esquema := "foto_" + hex.EncodeToString(b[:])
	rows, err := e.pool.Query(e.ctx, `SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'schema_migrations' ORDER BY tablename`)
	if err != nil {
		t.Fatalf("listar as tabelas: %v", err)
	}
	tabelas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(tabelas) < 10 {
		t.Fatalf("listar as tabelas: %v (%d)", err, len(tabelas))
	}
	e.exec("CREATE SCHEMA " + pgx.Identifier{esquema}.Sanitize())
	for _, tab := range tabelas {
		e.exec("CREATE TABLE " + pgx.Identifier{esquema, tab}.Sanitize() + " AS TABLE " + pgx.Identifier{"public", tab}.Sanitize())
	}
	return func(t *testing.T) {
		t.Helper()
		tx, err := e.pool.Begin(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(e.ctx) }()
		var todas []string
		for _, tab := range tabelas {
			todas = append(todas, pgx.Identifier{"public", tab}.Sanitize())
		}
		for _, sql := range append([]string{
			"SET LOCAL session_replication_role = replica",
			"TRUNCATE " + strings.Join(todas, ", "),
		}, func() []string {
			var ins []string
			for _, tab := range tabelas {
				ins = append(ins, "INSERT INTO "+pgx.Identifier{"public", tab}.Sanitize()+
					" SELECT * FROM "+pgx.Identifier{esquema, tab}.Sanitize())
			}
			return ins
		}()...) {
			if _, err := tx.Exec(e.ctx, sql); err != nil {
				t.Fatalf("restaurar a foto (%s): %v", strings.SplitN(sql, " ", 3)[0], err)
			}
		}
		if err := tx.Commit(e.ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// O DepoisDeRestaurar num banco só deste teste, com números exatos: cada conta
// ganha um epoch novo (e a marca d'água sobe ao last_seq), todas as sessões
// saem, os aparelhos ainda não revogados são revogados (revoked_at e a versão
// mais um), o já revogado fica como estava, e os tokens saem; os ditados não
// mudam. A segunda vez só troca o epoch de novo.
func TestDepoisDeRestaurar_Postgres(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a, b := e.newUser(t), e.newUser(t)
	devA1, devA2, devB := e.newDeviceWithToken(t, a, hmac32("hmac-a1")), e.newDeviceWithToken(t, a, hmac32("hmac-a2")), e.newDeviceWithToken(t, b, hmac32("hmac-b"))
	revogado := e.newDevice(t, a)
	e.exec("UPDATE devices SET revoked_at = $2, version = 4 WHERE id = $1", revogado, now0.Add(-48*time.Hour))
	for _, u := range []string{a, b} {
		for i := range 2 {
			e.exec(`INSERT INTO web_sessions (id, user_id, token_hmac, label, created_at, last_seen_at, expires_at)
				VALUES (gen_random_uuid(), $1, $2, 'Android', $3, $3, $4)`, u, hmac32(u+strconv.Itoa(i)), now0, now0.Add(24*time.Hour))
		}
	}
	id := cloudstore.NewUUIDv7()
	if _, err := s.Push(e.ctx, cloudstore.Actor{UserID: a, DeviceID: devA1}, []cloudstore.Mutation{createMut(t, id, "fica como está")}, now0); err != nil {
		t.Fatal(err)
	}
	type contador struct {
		epoch     string
		lastSeq   int64
		watermark int64
	}
	ler := func(u string) contador {
		var c contador
		if err := e.pool.QueryRow(e.ctx, `SELECT sync_epoch::text, last_seq, tombstone_watermark FROM user_sync_counters WHERE user_id = $1`, u).
			Scan(&c.epoch, &c.lastSeq, &c.watermark); err != nil {
			t.Fatal(err)
		}
		return c
	}
	antesA, antesB := ler(a), ler(b)
	if antesA.lastSeq != 1 || antesA.watermark != 0 {
		t.Fatalf("o contador de A antes = %+v", antesA)
	}

	r, err := s.DepoisDeRestaurar(e.ctx, now0)
	if err != nil {
		t.Fatalf("DepoisDeRestaurar: %v", err)
	}
	if r != (cloudstore.Restauracao{Contas: 2, Sessoes: 4, Aparelhos: 3}) {
		t.Fatalf("as contagens = %+v; quero 2 contas, 4 sessões e 3 aparelhos", r)
	}
	depoisA, depoisB := ler(a), ler(b)
	if depoisA.epoch == antesA.epoch || depoisB.epoch == antesB.epoch || depoisA.epoch == depoisB.epoch {
		t.Errorf("os epochs: A %s -> %s, B %s -> %s", antesA.epoch, depoisA.epoch, antesB.epoch, depoisB.epoch)
	}
	if !strings.HasPrefix(depoisA.epoch[14:15], "7") {
		t.Errorf("o epoch novo não é um UUID v7: %s", depoisA.epoch)
	}
	if depoisA.lastSeq != 1 || depoisA.watermark != 1 {
		t.Errorf("o contador de A depois = %+v; quero o seq igual e a marca d'água nele", depoisA)
	}
	var sessoes, tokens int
	e.scalar(&sessoes, "SELECT count(*) FROM web_sessions")
	e.scalar(&tokens, "SELECT count(*) FROM device_tokens")
	if sessoes != 0 || tokens != 0 {
		t.Errorf("sobraram %d sessões e %d tokens", sessoes, tokens)
	}
	for _, d := range []string{devA1, devA2, devB} {
		var versao int64
		var revogadoEm *time.Time
		if err := e.pool.QueryRow(e.ctx, "SELECT version, revoked_at FROM devices WHERE id = $1", d).Scan(&versao, &revogadoEm); err != nil {
			t.Fatal(err)
		}
		if versao != 2 || revogadoEm == nil || !revogadoEm.Equal(now0) {
			t.Errorf("o aparelho %s: versão %d, revoked_at %v; quero 2 e now", d, versao, revogadoEm)
		}
	}
	var versaoDoRevogado int64
	e.scalar(&versaoDoRevogado, "SELECT version FROM devices WHERE id = $1", revogado)
	if versaoDoRevogado != 4 {
		t.Errorf("o aparelho já revogado mudou: versão %d", versaoDoRevogado)
	}
	if _, err := s.ResolverTokenDeAparelho(e.ctx, hmac32("hmac-a1"), now0); !errors.Is(err, cloudstore.ErrCredencial) {
		t.Errorf("o token de A1 depois: %v, quero ErrCredencial", err)
	}
	if got, err := s.Get(e.ctx, a, id); err != nil || got.FinalText != "fica como está" || got.Version != 1 {
		t.Errorf("o ditado depois: %+v, %v", got, err)
	}
	if p, err := s.Pull(e.ctx, a, 0, 10); err != nil || p.Epoch != depoisA.epoch || len(p.Changes) != 1 {
		t.Errorf("o pull de A depois: epoch %s (quero %s), %d mudanças, %v", p.Epoch, depoisA.epoch, len(p.Changes), err)
	}

	r2, err := s.DepoisDeRestaurar(e.ctx, now0)
	if err != nil || r2 != (cloudstore.Restauracao{Contas: 2}) {
		t.Errorf("a segunda vez = %+v, %v; quero só as 2 contas", r2, err)
	}
	if ler(a).epoch == depoisA.epoch {
		t.Error("a segunda vez não trocou o epoch")
	}
}

// hmac32 é um HMAC de mentira com o tamanho do de verdade (32 bytes, o CHECK
// das tabelas).
func hmac32(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
