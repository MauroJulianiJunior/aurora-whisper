package pgstore

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// As contas no Postgres (ADR-019, F3): o que só o banco prova (a transação, a
// concorrência e o SQL). O comportamento pela API está na suíte
// internal/server/authtest, que roda contra o Postgres em TestAuth_Postgres.

var politica = cloudstore.PoliticaDeSessao{SemUso: 30 * 24 * time.Hour, Teto: 90 * 24 * time.Hour}

func (e *pgEnv) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	e.scalar(&n, sql, args...)
	return n
}

// O primeiro login cria o usuário e o contador, com o epoch UUID v7; o
// segundo acha o mesmo usuário pelo sub.
func TestPostgres_EntrarComGoogle_UsuarioEContadorJuntos(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	id := cloudstore.Identidade{Sub: "sub-1", Email: "mauro@exemplo.test", Nome: "Mauro"}
	u, err := s.EntrarComGoogle(e.ctx, id, now0)
	if err != nil {
		t.Fatalf("EntrarComGoogle: %v", err)
	}
	if v, ok := cloudstore.CanonicalUUID(u.ID); !ok || v[14] != '7' {
		t.Errorf("id do usuário = %q, quero UUID v7", u.ID)
	}
	var epoch string
	e.scalar(&epoch, "SELECT sync_epoch::text FROM user_sync_counters WHERE user_id = $1 AND last_seq = 0", u.ID)
	if epoch[14] != '7' {
		t.Errorf("sync_epoch = %q, quero UUID v7", epoch)
	}
	again, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-1", Email: "mauro@exemplo.test", Nome: "Mauro J."}, now0)
	if err != nil || again.ID != u.ID || again.Nome != "Mauro J." {
		t.Fatalf("segundo login = %+v, %v; quero o mesmo usuário com o nome novo", again, err)
	}
	if n := e.count("SELECT count(*) FROM users"); n != 1 {
		t.Fatalf("%d usuários, quero 1", n)
	}

	// D7: o mesmo e-mail com outro sub não cria nada.
	if _, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-2", Email: "mauro@exemplo.test"}, now0); !errors.Is(err, cloudstore.ErrSubDiferente) {
		t.Fatalf("outro sub com o mesmo e-mail: %v, quero ErrSubDiferente", err)
	}
	// O e-mail mudou no Google: atualiza; mas não para o de outro usuário.
	other, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-3", Email: "outra@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-1", Email: "outra@exemplo.test"}, now0); !errors.Is(err, cloudstore.ErrEmailEmUso) {
		t.Fatalf("e-mail de outro usuário: %v, quero ErrEmailEmUso", err)
	}
	if moved, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-1", Email: "mauro.novo@exemplo.test"}, now0); err != nil || moved.ID != u.ID {
		t.Fatalf("e-mail novo: %+v, %v", moved, err)
	}
	var email string
	e.scalar(&email, "SELECT email FROM users WHERE id = $1", u.ID)
	if email != "mauro.novo@exemplo.test" {
		t.Errorf("users.email = %q", email)
	}
	// Conta desativada.
	e.exec("UPDATE users SET disabled_at = now() WHERE id = $1", other.ID)
	if _, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-3", Email: "outra@exemplo.test"}, now0); !errors.Is(err, cloudstore.ErrContaDesativada) {
		t.Fatalf("conta desativada: %v", err)
	}
	if n := e.count("SELECT count(*) FROM users"); n != 2 {
		t.Fatalf("%d usuários, quero 2", n)
	}
	if n := e.count("SELECT count(*) FROM user_sync_counters"); n != 2 {
		t.Fatalf("%d contadores, quero 2 (um por usuário)", n)
	}
}

// Oito primeiros logins ao mesmo tempo com o mesmo sub: um usuário só, um
// contador só, e todos recebem o mesmo id. Oito com o mesmo e-mail e subs
// diferentes: um entra, os outros são recusados (D7).
func TestPostgres_EntrarComGoogle_Concorrente(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	const n = 8
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-x", Email: "x@exemplo.test"}, now0)
			ids[i], errs[i] = u.ID, err
		})
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("login %d = %q, %v; quero o mesmo usuário, sem erro", i, ids[i], errs[i])
		}
	}
	for i := range n {
		wg.Go(func() {
			_, errs[i] = s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "sub-y" + string(rune('a'+i)), Email: "y@exemplo.test"}, now0)
		})
	}
	wg.Wait()
	ok := 0
	for i := range n {
		switch {
		case errs[i] == nil:
			ok++
		case !errors.Is(errs[i], cloudstore.ErrSubDiferente):
			t.Errorf("login %d: %v, quero ErrSubDiferente", i, errs[i])
		}
	}
	if ok != 1 {
		t.Fatalf("%d entraram com o mesmo e-mail, quero 1", ok)
	}
	if u, c := e.count("SELECT count(*) FROM users"), e.count("SELECT count(*) FROM user_sync_counters"); u != 2 || c != 2 {
		t.Fatalf("%d usuários e %d contadores, quero 2 e 2", u, c)
	}
}

func pendente(seed string) cloudstore.LoginPendente {
	return cloudstore.LoginPendente{
		StateHash: cloudauth.Hash("state-" + seed), PreHash: cloudauth.Hash("pre-" + seed),
		CodeVerifier: "verificador-" + seed, Nonce: "nonce-" + seed, ReturnTo: "/volta",
		CriadoEm: now0, ExpiraEm: now0.Add(5 * time.Minute),
	}
}

// O login pendente vale uma vez só, mesmo com oito callbacks ao mesmo tempo,
// e não vale vencido.
func TestPostgres_LoginPendente_UsoUnico(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	if err := s.CriarLoginPendente(e.ctx, pendente("a")); err != nil {
		t.Fatal(err)
	}
	// O state certo com o pre errado (ou sem pre) não acha a linha e não a
	// consome: o dono do login ainda entra depois.
	for _, pre := range [][]byte{cloudauth.Hash("pre-b"), nil} {
		if _, err := s.ConsumirLoginPendente(e.ctx, cloudauth.Hash("state-a"), pre, now0.Add(time.Minute)); !errors.Is(err, cloudstore.ErrLoginPendente) {
			t.Fatalf("pre errado (%x): %v, quero ErrLoginPendente", pre, err)
		}
	}
	got, err := s.ConsumirLoginPendente(e.ctx, cloudauth.Hash("state-a"), cloudauth.Hash("pre-a"), now0.Add(time.Minute))
	if err != nil || got.CodeVerifier != "verificador-a" || got.Nonce != "nonce-a" || got.ReturnTo != "/volta" ||
		!bytes.Equal(got.PreHash, cloudauth.Hash("pre-a")) {
		t.Fatalf("consumir = %+v, %v", got, err)
	}
	if _, err := s.ConsumirLoginPendente(e.ctx, cloudauth.Hash("state-a"), cloudauth.Hash("pre-a"), now0.Add(time.Minute)); !errors.Is(err, cloudstore.ErrLoginPendente) {
		t.Fatalf("segundo uso: %v, quero ErrLoginPendente", err)
	}
	if err := s.CriarLoginPendente(e.ctx, pendente("b")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumirLoginPendente(e.ctx, cloudauth.Hash("state-b"), cloudauth.Hash("pre-b"), now0.Add(5*time.Minute)); !errors.Is(err, cloudstore.ErrLoginPendente) {
		t.Fatalf("vencido: %v, quero ErrLoginPendente", err)
	}
	// O volta padrão é "/".
	p := pendente("c")
	p.ReturnTo = ""
	if err := s.CriarLoginPendente(e.ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ConsumirLoginPendente(e.ctx, cloudauth.Hash("state-c"), cloudauth.Hash("pre-c"), now0); got.ReturnTo != "/" {
		t.Fatalf("volta padrão = %q", got.ReturnTo)
	}

	if err := s.CriarLoginPendente(e.ctx, pendente("d")); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for range n {
		wg.Go(func() {
			if _, err := s.ConsumirLoginPendente(e.ctx, cloudauth.Hash("state-d"), cloudauth.Hash("pre-d"), now0); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d callbacks consumiram o mesmo state, quero 1", wins)
	}

	// A arrumação apaga os vencidos.
	a, err := s.Arrumar(e.ctx, now0.Add(6*time.Minute))
	if err != nil || a.LoginsPendentes != 4 {
		t.Fatalf("Arrumar = %+v, %v; quero 4 logins pendentes", a, err)
	}
}

// A sessão anda o vencimento a cada uso, sem passar do teto, e o UPDATE é um
// só: a sessão vencida não volta a valer.
func TestPostgres_Sessao_SemUsoETeto(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "s", Email: "s@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	h := cloudauth.HMAC([]byte("chave"), "token")
	sess, err := s.CriarSessao(e.ctx, cloudstore.NovaSessao{UserID: u.ID, TokenHMAC: h, Rotulo: "Android · Chrome",
		Agora: now0, ExpiraEm: now0.Add(politica.SemUso)})
	if err != nil {
		t.Fatal(err)
	}
	at := now0
	for range 3 { // usa a cada 29 dias: chega a 87 dias
		at = at.Add(29 * 24 * time.Hour)
		r, err := s.ResolverSessao(e.ctx, h, at, politica)
		if err != nil {
			t.Fatalf("resolver em %v: %v", at, err)
		}
		if r.ID != sess.ID || r.Email != "s@exemplo.test" || !r.UltimoUso.Equal(at) {
			t.Fatalf("resolvida = %+v", r)
		}
		want := politica.ExpiracaoDaSessao(now0, at)
		if !r.ExpiraEm.Equal(want) {
			t.Fatalf("vence em %v, quero %v", r.ExpiraEm, want)
		}
	}
	// 90 dias desde a criação: acabou, mesmo com uso há 3 dias.
	if _, err := s.ResolverSessao(e.ctx, h, now0.Add(90*24*time.Hour), politica); !errors.Is(err, cloudstore.ErrCredencial) {
		t.Fatalf("depois do teto: %v, quero ErrCredencial", err)
	}

	h2 := cloudauth.HMAC([]byte("chave"), "token-2")
	if _, err := s.CriarSessao(e.ctx, cloudstore.NovaSessao{UserID: u.ID, TokenHMAC: h2, Rotulo: "x",
		Agora: now0, ExpiraEm: now0.Add(politica.SemUso)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolverSessao(e.ctx, h2, now0.Add(politica.SemUso), politica); !errors.Is(err, cloudstore.ErrCredencial) {
		t.Fatalf("30 dias sem uso: %v, quero ErrCredencial", err)
	}
	// O teto vale pela política da hora: encurtá-lo corta as sessões que já
	// existem, mesmo com o vencimento anotado mais longe.
	h3 := cloudauth.HMAC([]byte("chave"), "token-3")
	if _, err := s.CriarSessao(e.ctx, cloudstore.NovaSessao{UserID: u.ID, TokenHMAC: h3, Rotulo: "x",
		Agora: now0, ExpiraEm: now0.Add(politica.SemUso)}); err != nil {
		t.Fatal(err)
	}
	curta := cloudstore.PoliticaDeSessao{SemUso: politica.SemUso, Teto: 10 * 24 * time.Hour}
	if _, err := s.ResolverSessao(e.ctx, h3, now0.Add(20*24*time.Hour), curta); !errors.Is(err, cloudstore.ErrCredencial) {
		t.Fatalf("teto encurtado: %v, quero ErrCredencial", err)
	}
	a, err := s.Arrumar(e.ctx, now0.Add(91*24*time.Hour))
	if err != nil || a.Sessoes != 3 {
		t.Fatalf("Arrumar = %+v, %v; quero 3 sessões", a, err)
	}
}

// O "apagar meus dados" com um push do mesmo usuário ao mesmo tempo: no fim,
// nenhum ditado nem evento da conta fica, o aparelho e o token somem, e o seq
// continua de onde estava (nunca volta).
func TestPostgres_ApagarDadosComPushConcorrente(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u, err := s.EntrarComGoogle(e.ctx, cloudstore.Identidade{Sub: "a", Email: "a@exemplo.test"}, now0)
	if err != nil {
		t.Fatal(err)
	}
	other := e.newUser(t)
	otherDev := e.newDevice(t, other)
	dev := e.newDevice(t, u.ID)
	e.exec("INSERT INTO device_tokens (token_hmac, device_id) VALUES ($1, $2)", cloudauth.HMAC([]byte("p"), "t"), dev)
	h := cloudauth.HMAC([]byte("chave"), "sessao")
	keep, err := s.CriarSessao(e.ctx, cloudstore.NovaSessao{UserID: u.ID, TokenHMAC: h, Rotulo: "x", Agora: now0, ExpiraEm: now0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CriarSessao(e.ctx, cloudstore.NovaSessao{UserID: u.ID, TokenHMAC: cloudauth.HMAC([]byte("chave"), "outra"),
		Rotulo: "y", Agora: now0, ExpiraEm: now0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	pc := cloudstore.Actor{UserID: u.ID, DeviceID: dev}
	for i := range 5 {
		if _, err := s.Push(e.ctx, pc, []cloudstore.Mutation{createMut(t, contracttest.NewID(), "antes "+string(rune('a'+i)))}, now0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Push(e.ctx, cloudstore.Actor{UserID: other, DeviceID: otherDev}, []cloudstore.Mutation{createMut(t, contracttest.NewID(), "de B")}, now0); err != nil {
		t.Fatal(err)
	}
	// Um pedido de pareamento aprovado por A e um por B (o pareamento é da F6).
	for i, by := range []string{u.ID, other} {
		e.exec(`INSERT INTO pairing_requests (id, code_challenge, install_id, device_name, platform, redirect_port,
			confirmation_code, expires_at, approved_by, approved_at, code_hash)
			VALUES (gen_random_uuid(), 'desafio', $1, 'PC', 'windows', 50000, 'AB12', now() + interval '10 minutes',
			$2, now(), sha256(convert_to($1, 'UTF8')))`, "install-"+string(rune('a'+i)), by)
	}
	var epochBefore string
	e.scalar(&epochBefore, "SELECT sync_epoch::text FROM user_sync_counters WHERE user_id = $1", u.ID)

	var wg sync.WaitGroup
	wg.Go(func() {
		for range 20 {
			_, _ = s.Push(e.ctx, pc, []cloudstore.Mutation{createMut(t, contracttest.NewID(), "durante")}, now0)
		}
	})
	epoch, err := s.ApagarDadosDaConta(e.ctx, u.ID, keep.ID, now0)
	wg.Wait()
	if err != nil {
		t.Fatalf("ApagarDadosDaConta: %v", err)
	}
	if epoch == epochBefore || epoch[14] != '7' {
		t.Errorf("epoch novo = %q (antes %q), quero outro UUID v7", epoch, epochBefore)
	}
	for _, q := range []string{
		"SELECT count(*) FROM transcriptions WHERE user_id = $1",
		"SELECT count(*) FROM sync_events WHERE user_id = $1",
		"SELECT count(*) FROM applied_mutations WHERE user_id = $1",
		"SELECT count(*) FROM devices WHERE user_id = $1",
		"SELECT count(*) FROM pairing_requests WHERE approved_by = $1",
	} {
		if n := e.count(q, u.ID); n != 0 {
			t.Errorf("%s = %d, quero 0", q, n)
		}
	}
	if n := e.count("SELECT count(*) FROM device_tokens"); n != 0 {
		t.Errorf("%d tokens, quero 0 (o aparelho levou o token)", n)
	}
	if n := e.count("SELECT count(*) FROM web_sessions WHERE user_id = $1", u.ID); n != 1 {
		t.Errorf("%d sessões, quero 1 (a atual)", n)
	}
	if _, err := s.ResolverSessao(e.ctx, h, now0, politica); err != nil {
		t.Errorf("a sessão atual deixou de valer: %v", err)
	}
	var lastSeq, watermark int64
	e.scalar(&lastSeq, "SELECT last_seq FROM user_sync_counters WHERE user_id = $1", u.ID)
	e.scalar(&watermark, "SELECT tombstone_watermark FROM user_sync_counters WHERE user_id = $1", u.ID)
	if lastSeq < 5 || watermark != lastSeq {
		t.Errorf("last_seq %d e marca d'água %d: quero o seq que continua (>= 5) e a marca no last_seq", lastSeq, watermark)
	}
	// B intacto.
	if n := e.count("SELECT count(*) FROM transcriptions WHERE user_id = $1", other); n != 1 {
		t.Errorf("B tem %d ditados, quero 1", n)
	}
	if n := e.count("SELECT count(*) FROM pairing_requests WHERE approved_by = $1", other); n != 1 {
		t.Errorf("B tem %d pedidos de pareamento, quero 1", n)
	}
	// Um push depois do apagar não acha o aparelho.
	if _, err := s.Push(e.ctx, pc, []cloudstore.Mutation{createMut(t, contracttest.NewID(), "depois")}, now0); err == nil {
		t.Error("push com o aparelho apagado passou")
	}
}
