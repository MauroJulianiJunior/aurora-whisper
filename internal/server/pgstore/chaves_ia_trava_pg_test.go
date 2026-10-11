package pgstore

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As travas da gravação conferida da chave da IA (ADR-021, C3-correções) no
// Postgres de verdade. A suíte dos casos de uso (TestChaves_Postgres) prova que
// a chave não é gravada depois de um apagar ou de um revogar; estes provam o que
// ela não alcança: que a gravação ESPERA a transação que segura a conta, e
// confere a condição só depois, olhando o que aquela transação confirmou.
//
// Cada caso faz o papel do outro lado com os comandos de verdade (os mesmos
// SQL do "apagar meus dados", do revogar e do aceite novo), numa transação que
// ele segura aberta. Dispara a gravação, espera o Postgres dizer que ela está
// bloqueada numa trava (pg_stat_activity), só então deixa o outro lado
// terminar, e confere o resultado. Sem a trava da gravação (o caso real da
// corrida), a gravação passaria direto, antes de o outro lado terminar, e o
// teste falha nessa espera.

// esperarUmBloqueado espera algum comando deste banco ficar parado numa trava.
func (e *pgEnv) esperarUmBloqueado(t *testing.T) {
	t.Helper()
	limite := time.Now().Add(20 * time.Second)
	for {
		var n int
		e.scalar(&n, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`)
		if n > 0 {
			return
		}
		if time.Now().After(limite) {
			t.Fatal("a gravação não ficou esperando a trava em 20 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gravacaoEmEspera segura uma trava com abrir (numa transação), dispara a
// gravação conferida da chave, espera ela bloquear, deixa o outro lado terminar
// com acabar, confirma (ou desfaz) a transação dele e devolve o que a gravação
// respondeu. A gravação NÃO pode ter respondido antes de acabar.
func (e *pgEnv) gravacaoEmEspera(t *testing.T, s *Store, u string, cond cloudstore.CondicaoDaChave, k cloudstore.ChaveDeIA,
	abrir func(exec func(sql string, args ...any)), acabar func(exec func(sql string, args ...any)), confirmar bool) error {
	t.Helper()
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(e.ctx) }()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(e.ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	abrir(exec)

	resposta := make(chan error, 1)
	go func() {
		_, err := s.GravarChaveDeIAConferindo(e.ctx, u, cond, k, time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
		resposta <- err
	}()
	e.esperarUmBloqueado(t)
	select {
	case err := <-resposta:
		t.Fatalf("a gravação respondeu (%v) sem esperar a transação que segura a conta", err)
	default:
	}

	acabar(exec)
	if confirmar {
		if err := tx.Commit(e.ctx); err != nil {
			t.Fatal(err)
		}
	} else if err := tx.Rollback(e.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-resposta:
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("a gravação não terminou depois de a trava sair")
		return nil
	}
}

func (e *pgEnv) chavesDaConta(u string) int {
	e.t.Helper()
	var n int
	e.scalar(&n, `SELECT count(*) FROM ai_provider_keys WHERE user_id = $1`, u)
	return n
}

func linhaDeChave(provedor string, b byte) cloudstore.ChaveDeIA {
	return cloudstore.ChaveDeIA{
		Provedor: provedor, IDDaCifra: bytes.Repeat([]byte{b}, 8), Nonce: bytes.Repeat([]byte{b + 1}, 12),
		Cifrado: bytes.Repeat([]byte{b + 2}, 40), Final: "WXYZ",
	}
}

// Um "apagar meus dados" que já pegou o contador (o primeiro comando dele) faz a
// gravação esperar. Quando ele termina, o epoch é outro: a gravação recusa
// (ErrEpochChanged) e a chave não existe. É a corrida que a revisão da C3a
// apontou, sem a janela: o epoch lido no começo do PUT e o INSERT agora estão
// sob a mesma trava.
func TestPostgres_GravarChaveEsperaOApagarQueTemOContador(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"}, agora); err != nil {
		t.Fatal(err)
	}
	epoch, err := s.EpochDaConta(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}

	err = e.gravacaoEmEspera(t, s, u, cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: true}, linhaDeChave("openai", 1),
		func(exec func(string, ...any)) { exec(sqlNovoEpoch, u, cloudstore.NewUUIDv7()) },
		func(exec func(string, ...any)) {
			for _, passo := range apagarDaConta {
				exec(passo.sql, u)
			}
		}, true)
	if !errors.Is(err, cloudstore.ErrEpochChanged) {
		t.Fatalf("a gravação depois do apagar: %v, quero ErrEpochChanged", err)
	}
	if n := e.chavesDaConta(u); n != 0 {
		t.Fatalf("o apagar deixou %d chaves", n)
	}
	var aceites int
	e.scalar(&aceites, `SELECT count(*) FROM dictation_consents WHERE user_id = $1`, u)
	if aceites != 0 {
		t.Fatalf("o apagar deixou %d aceites", aceites)
	}
}

// Um revogar que já pegou a linha do usuário (o primeiro comando dele) faz a
// gravação esperar. Quando ele termina, o aceite está encerrado e as chaves
// saíram: a gravação recusa (ErrSemAceite) e a chave não existe. Sem a trava da
// linha do usuário, a gravação leria o aceite ainda em aberto, gravaria, e o
// revogar já teria passado pelas chaves.
func TestPostgres_GravarChaveEsperaORevogarQueTemAConta(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"}, agora); err != nil {
		t.Fatal(err)
	}
	epoch, err := s.EpochDaConta(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}

	err = e.gravacaoEmEspera(t, s, u, cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: true}, linhaDeChave("openai", 1),
		func(exec func(string, ...any)) { exec(sqlTravarUsuario, u) },
		func(exec func(string, ...any)) {
			exec(sqlRevogarAceite, u, agora.Add(time.Second))
			exec(sqlApagarChavesDaConta, u)
		}, true)
	if !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Fatalf("a gravação depois do revogar: %v, quero ErrSemAceite", err)
	}
	if n := e.chavesDaConta(u); n != 0 {
		t.Fatalf("o revogar deixou %d chaves", n)
	}
	aceites, err := s.ListarAceites(e.ctx, u)
	if err != nil || len(aceites) != 1 || aceites[0].Aberto() || aceites[0].Motivo != cloudstore.FimRevogado {
		t.Fatalf("aceites = %+v, %v; quero um só, revogado", aceites, err)
	}
}

// Um aceite novo que já pegou a linha do usuário e troca o aceite por um que não
// cobre o provedor da chave: a gravação espera e recusa.
func TestPostgres_GravarChaveEsperaOAceiteNovoQueNaoCobreOProvedor(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"}, agora); err != nil {
		t.Fatal(err)
	}
	epoch, err := s.EpochDaConta(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}

	err = e.gravacaoEmEspera(t, s, u, cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: true}, linhaDeChave("openai", 1),
		func(exec func(string, ...any)) { exec(sqlTravarUsuario, u) },
		func(exec func(string, ...any)) {
			exec(sqlSubstituirAceite, u, agora.Add(time.Second))
			exec(sqlGravarAceite, cloudstore.NewUUIDv7(), u, cloudstore.VersaoDoAviso, "groq", nil, agora.Add(time.Second))
		}, true)
	if !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Fatalf("a gravação depois do aceite novo da Groq: %v, quero ErrSemAceite", err)
	}
	if n := e.chavesDaConta(u); n != 0 {
		t.Fatalf("ficaram %d chaves", n)
	}
}

// O controle: a mesma espera, mas o outro lado DESFAZ (a transação dele não
// muda nada). A gravação continua depois que a trava sai, passa nas duas
// conferências e grava. E gravar uma chave não é uma escrita do histórico: o
// contador de sync não anda (nem seq, nem evento).
func TestPostgres_GravarChaveContinuaQuandoATravaSaiSemMudarNada(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"}, agora); err != nil {
		t.Fatal(err)
	}
	epoch, err := s.EpochDaConta(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	seqAntes := e.lastSeq(u)

	for _, trava := range []struct {
		nome string
		sql  string
	}{
		{"o contador", `UPDATE user_sync_counters SET last_seq = last_seq WHERE user_id = $1`},
		{"a linha do usuário", sqlTravarUsuario},
	} {
		err = e.gravacaoEmEspera(t, s, u, cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: true}, linhaDeChave("openai", 1),
			func(exec func(string, ...any)) { exec(trava.sql, u) },
			func(func(string, ...any)) {}, false)
		if err != nil {
			t.Fatalf("segurando %s e desfazendo: %v", trava.nome, err)
		}
		if n := e.chavesDaConta(u); n != 1 {
			t.Fatalf("segurando %s: %d chaves, quero 1", trava.nome, n)
		}
		if _, err := s.ApagarChaveDeIA(e.ctx, u, "openai"); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.lastSeq(u); got != seqAntes {
		t.Errorf("gravar a chave mexeu no contador: last_seq %d -> %d", seqAntes, got)
	}
	var eventos int
	e.scalar(&eventos, `SELECT count(*) FROM sync_events WHERE user_id = $1`, u)
	if eventos != 0 {
		t.Errorf("gravar a chave gerou %d eventos de sync", eventos)
	}
}

// O revogar e o "apagar meus dados" da mesma conta, com um aceite em aberto e
// uma chave guardada, não se travam um no outro. O revogar trava o aceite e
// depois apaga as chaves; se o apagar apagasse as chaves primeiro e esperasse o
// aceite depois, cada um esperaria o outro (SQLSTATE 40P01, e o Postgres aborta
// um deles, que podia ser o apagar). Aqui o revogar é segurado entre as duas
// coisas, e o apagar é lançado nesse meio: com a ordem do apagarDaConta (o
// aceite antes das chaves), ele espera, o revogar termina, e o apagar também.
func TestPostgres_RevogarEApagarNaoSeTravam(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"}, agora); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GravarChaveDeIA(e.ctx, u, linhaDeChave("groq", 50), agora); err != nil {
		t.Fatal(err)
	}

	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(e.ctx) }()
	// Os dois primeiros comandos do revogar: a linha do usuário e o aceite.
	if _, err := tx.Exec(e.ctx, sqlTravarUsuario, u); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(e.ctx, sqlRevogarAceite, u, agora); err != nil {
		t.Fatal(err)
	}
	apagar := make(chan error, 1)
	go func() {
		_, err := s.ApagarDadosDaConta(e.ctx, u, cloudstore.NewUUIDv7(), agora)
		apagar <- err
	}()
	e.esperarUmBloqueado(t)
	// O resto do revogar: as chaves. Com a ordem errada, é aqui que o Postgres
	// acusava o deadlock.
	if _, err := tx.Exec(e.ctx, sqlApagarChavesDaConta, u); err != nil {
		t.Fatalf("o revogar, apagando as chaves, com o apagar esperando: %v", err)
	}
	if err := tx.Commit(e.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-apagar:
		if err != nil {
			t.Fatalf("o apagar meus dados: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("o apagar meus dados não terminou em 20 s")
	}
	var restos int
	e.scalar(&restos, `SELECT (SELECT count(*) FROM ai_provider_keys WHERE user_id = $1)
		+ (SELECT count(*) FROM dictation_consents WHERE user_id = $1)
		+ (SELECT count(*) FROM dictation_settings WHERE user_id = $1)`, u)
	if restos != 0 {
		t.Fatalf("ficaram %d linhas do ditado no celular", restos)
	}
}

// Dois lados da conta que não se cruzam: uma gravação conferida e um "apagar"
// (ou um revogar) lançados juntos, muitas vezes, nunca travam um no outro. O
// deadlock apareceria aqui como um teste que não termina.
func TestPostgres_GravarChaveEOutrosLadosNaoSeTravamUmNoOutro(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for i := range 30 {
		if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"}, agora); err != nil {
			t.Fatal(err)
		}
		// Uma chave já guardada: o revogar e o apagar têm o que apagar dos dois lados.
		if _, err := s.GravarChaveDeIA(e.ctx, u, linhaDeChave("groq", 50), agora); err != nil {
			t.Fatal(err)
		}
		epoch, err := s.EpochDaConta(e.ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		fim := make(chan error, 3)
		go func() {
			_, err := s.GravarChaveDeIAConferindo(e.ctx, u, cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: true}, linhaDeChave("openai", byte(i)), agora)
			if errors.Is(err, cloudstore.ErrEpochChanged) || errors.Is(err, cloudstore.ErrSemAceite) {
				err = nil
			}
			fim <- err
		}()
		go func() {
			_, err := s.RevogarAceite(e.ctx, u, agora)
			fim <- err
		}()
		go func() {
			// O epoch trocado e as tabelas apagadas, como o "apagar meus dados".
			tx, err := e.pool.Begin(e.ctx)
			if err != nil {
				fim <- err
				return
			}
			defer func() { _ = tx.Rollback(e.ctx) }()
			if _, err := tx.Exec(e.ctx, sqlNovoEpoch, u, cloudstore.NewUUIDv7()); err != nil {
				fim <- err
				return
			}
			for _, passo := range apagarDaConta {
				if _, err := tx.Exec(e.ctx, passo.sql, u); err != nil {
					fim <- err
					return
				}
			}
			fim <- tx.Commit(e.ctx)
		}()
		for range 3 {
			select {
			case err := <-fim:
				if err != nil {
					t.Fatalf("rodada %d: %v", i, err)
				}
			case <-time.After(30 * time.Second):
				t.Fatalf("rodada %d: um lado não terminou em 30 s (deadlock?)", i)
			}
		}
		if n := e.chavesDaConta(u); n != 0 {
			t.Fatalf("rodada %d: ficaram %d chaves depois do apagar e do revogar", i, n)
		}
	}
}
