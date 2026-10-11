package pgstore

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Os testes do aceite e das preferências do ditado no celular (ADR-021, C3b)
// que só um Postgres de verdade prova: os CHECKs da 0018 e da 0019, o índice
// único do aceite em aberto, a atomicidade da revogação e o "apagar meus
// dados" nas três tabelas. A suíte dos casos de uso (TestChaves_Postgres) roda
// em chaves_ia_pg_test.go.

func sqlstateDe(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// Os CHECKs da 0018 e da 0019 no banco de verdade: o que o Go confere antes
// (ConferirPreferencias e ConferirAceite) o banco também recusa, para uma linha
// posta à mão ou um caminho futuro que esqueça a conferência.
func TestPostgres_AsPreferenciasEOAceiteNoBanco(t *testing.T) {
	e := newPG(t)
	u := e.newUser(t)

	type prefs struct {
		asrProv, asrModel string
		hint, polish      bool
		polishProv        string
		polishModel       string
	}
	boa := prefs{"openai", "gpt-4o-mini-transcribe", false, false, "anthropic", ""}
	inserir := func(user string, p prefs, criada, atualizada string) error {
		_, err := e.pool.Exec(e.ctx, `INSERT INTO dictation_settings
			(user_id, asr_provider, asr_model, use_hint, polish_enabled, polish_provider, polish_model, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, `+criada+`, `+atualizada+`)`,
			user, p.asrProv, p.asrModel, p.hint, p.polish, p.polishProv, p.polishModel)
		return err
	}
	mudar := func(f func(p *prefs)) prefs {
		p := boa
		f(&p)
		return p
	}
	for nome, c := range map[string]struct {
		p      prefs
		quero  string
		antes  string
		depois string
	}{
		"transcrição pela anthropic":  {mudar(func(p *prefs) { p.asrProv = "anthropic" }), "23514", "now()", "now()"},
		"transcrição em maiúsculas":   {mudar(func(p *prefs) { p.asrProv = "OpenAI" }), "23514", "now()", "now()"},
		"Polish pela groq":            {mudar(func(p *prefs) { p.polishProv = "groq" }), "23514", "now()", "now()"},
		"modelo vazio":                {mudar(func(p *prefs) { p.asrModel = "" }), "23514", "now()", "now()"},
		"modelo com espaço":           {mudar(func(p *prefs) { p.asrModel = "gpt 4" }), "23514", "now()", "now()"},
		"modelo com quebra no fim":    {mudar(func(p *prefs) { p.asrModel = "gpt-4\n" }), "23514", "now()", "now()"},
		"modelo com quebra no meio":   {mudar(func(p *prefs) { p.asrModel = "gpt\n4" }), "23514", "now()", "now()"},
		"modelo com acento":           {mudar(func(p *prefs) { p.asrModel = "modelo-é" }), "23514", "now()", "now()"},
		"modelo de 101 caracteres":    {mudar(func(p *prefs) { p.asrModel = strings.Repeat("m", 101) }), "23514", "now()", "now()"},
		"modelo do Polish com espaço": {mudar(func(p *prefs) { p.polishModel = "a b" }), "23514", "now()", "now()"},
		"Polish ligado sem modelo":    {mudar(func(p *prefs) { p.polish = true }), "23514", "now()", "now()"},
		"troca antes da criação":      {boa, "23514", "now()", "now() - interval '1 second'"},
	} {
		if got := sqlstateDe(inserir(u, c.p, c.antes, c.depois)); got != c.quero {
			t.Errorf("%s: SQLSTATE %q, quero %s", nome, got, c.quero)
		}
	}
	for _, ok := range []prefs{
		boa,
		mudar(func(p *prefs) { p.asrModel = strings.Repeat("m", 100); p.polishModel = strings.Repeat("p", 100) }),
		mudar(func(p *prefs) {
			p.asrModel = "openai/gpt-oss-20b"
			p.polish = true
			p.polishModel = "ft:gpt-4o-mini:org::id"
		}),
	} {
		other := e.newUser(t)
		if err := inserir(other, ok, "now()", "now()"); err != nil {
			t.Errorf("a linha %+v devia valer: %v", ok, err)
		}
	}
	if err := inserir(u, boa, "now()", "now()"); err != nil {
		t.Fatalf("a linha boa: %v", err)
	}
	if got := sqlstateDe(inserir(u, boa, "now()", "now()")); got != "23505" {
		t.Errorf("duas linhas de preferências da mesma conta: SQLSTATE %q, quero 23505 (a PK)", got)
	}
	if got := sqlstateDe(inserir(contracttest.NewID(), boa, "now()", "now()")); got != "23503" {
		t.Errorf("preferências de um usuário que não existe: SQLSTATE %q, quero 23503", got)
	}
	_, err := e.pool.Exec(e.ctx, `INSERT INTO dictation_settings (user_id, asr_provider, asr_model, polish_provider, polish_model, created_at, updated_at)
		VALUES ($1, 'openai', 'm', 'openai', '', now(), now())`, e.newUser(t))
	if got := sqlstateDe(err); got != "23502" {
		t.Errorf("preferências sem as flags (sem DEFAULT): SQLSTATE %q, quero 23502", got)
	}

	// O aceite.
	aceite := func(user, id string, versao int, asr, polish, aceitoEm, fim, motivo string) error {
		fimSQL, motivoSQL := "NULL", "NULL"
		if fim != "" {
			fimSQL = fim
		}
		if motivo != "" {
			motivoSQL = "'" + motivo + "'"
		}
		var polishArg any
		if polish != "" {
			polishArg = polish
		}
		_, err := e.pool.Exec(e.ctx, `INSERT INTO dictation_consents
			(id, user_id, notice_version, asr_provider, polish_provider, accepted_at, ended_at, end_reason)
			VALUES ($1, $2, $3, $4, $5, `+aceitoEm+`, `+fimSQL+`, `+motivoSQL+`)`,
			id, user, versao, asr, polishArg)
		return err
	}
	novo := func() string { return cloudstore.NewUUIDv7() }
	for nome, c := range map[string]struct {
		err   error
		quero string
	}{
		"versão zero":                  {aceite(u, novo(), 0, "openai", "", "now()", "", ""), "23514"},
		"transcrição pela anthropic":   {aceite(u, novo(), 1, "anthropic", "", "now()", "", ""), "23514"},
		"transcrição em maiúsculas":    {aceite(u, novo(), 1, "OpenAI", "", "now()", "", ""), "23514"},
		"Polish pela groq":             {aceite(u, novo(), 1, "openai", "groq", "now()", "", ""), "23514"},
		"fim sem motivo":               {aceite(u, novo(), 1, "openai", "", "now() - interval '1 hour'", "now()", ""), "23514"},
		"motivo sem fim":               {aceite(u, novo(), 1, "openai", "", "now()", "", "revogado"), "23514"},
		"motivo fora da lista":         {aceite(u, novo(), 1, "openai", "", "now() - interval '1 hour'", "now()", "expirado"), "23514"},
		"fim antes do começo":          {aceite(u, novo(), 1, "openai", "", "now()", "now() - interval '1 hour'", "revogado"), "23514"},
		"usuário que não existe":       {aceite(contracttest.NewID(), novo(), 1, "openai", "", "now()", "", ""), "23503"},
		"o mesmo id duas vezes (a PK)": {mesmoIDDuasVezes(e, aceite), "23505"},
	} {
		if got := sqlstateDe(c.err); got != c.quero {
			t.Errorf("%s: SQLSTATE %q, quero %s", nome, got, c.quero)
		}
	}

	// Um em aberto por conta (o índice único parcial); os que terminaram não
	// contam, e outra conta tem o dela.
	b := e.newUser(t)
	if err := aceite(b, novo(), 1, "openai", "openai", "now() - interval '2 hour'", "now() - interval '1 hour'", "substituido"); err != nil {
		t.Fatalf("um aceite que terminou: %v", err)
	}
	if err := aceite(b, novo(), 1, "groq", "", "now() - interval '2 hour'", "now() - interval '1 hour'", "revogado"); err != nil {
		t.Fatalf("outro aceite que terminou (vários valem): %v", err)
	}
	if err := aceite(b, novo(), 1, "openai", "", "now()", "", ""); err != nil {
		t.Fatalf("o aceite em aberto: %v", err)
	}
	if got := sqlstateDe(aceite(b, novo(), 1, "groq", "", "now()", "", "")); got != "23505" {
		t.Errorf("dois aceites em aberto: SQLSTATE %q, quero 23505 (o índice único parcial)", got)
	}
	if err := aceite(e.newUser(t), novo(), 1, "groq", "", "now()", "", ""); err != nil {
		t.Errorf("o aceite em aberto de outra conta: %v", err)
	}
	// Terminar o aberto abre vaga para outro.
	if _, err := e.pool.Exec(e.ctx, `UPDATE dictation_consents SET ended_at = now(), end_reason = 'revogado' WHERE user_id = $1 AND ended_at IS NULL`, b); err != nil {
		t.Fatal(err)
	}
	if err := aceite(b, novo(), 1, "groq", "", "now()", "", ""); err != nil {
		t.Errorf("um aceite novo depois de terminar o aberto: %v", err)
	}
}

// mesmoIDDuasVezes monta o caso "o mesmo id duas vezes": a segunda inserção, de
// outro usuário, com o id da primeira, bate na PK.
func mesmoIDDuasVezes(e *pgEnv, aceite func(user, id string, versao int, asr, polish, aceitoEm, fim, motivo string) error) error {
	id := cloudstore.NewUUIDv7()
	if err := aceite(e.newUser(e.t), id, 1, "openai", "", "now()", "", ""); err != nil {
		e.t.Fatalf("a primeira linha do id: %v", err)
	}
	return aceite(e.newUser(e.t), id, 1, "openai", "", "now()", "", "")
}

// "Apagar meus dados" deixa as TRÊS tabelas do ditado no celular vazias para a
// conta (as chaves, as preferências e os aceites, os que terminaram também), de
// verdade no banco, e não toca na outra conta.
func TestPostgres_ApagarMeusDadosDeixaAsTresTabelasVazias(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	a, b := e.newUser(t), e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for _, u := range []string{a, b} {
		for _, prov := range []string{"openai", "groq"} {
			if _, err := s.GravarChaveDeIA(e.ctx, u, cloudstore.ChaveDeIA{
				Provedor: prov, IDDaCifra: make([]byte, 8), Nonce: make([]byte, 12), Cifrado: make([]byte, 40), Final: "WXYZ",
			}, agora); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.GravarPreferenciasDoDitado(e.ctx, u, cloudstore.PreferenciasPadrao(), agora); err != nil {
			t.Fatal(err)
		}
		// Dois aceites: um terminado e um em aberto.
		for _, prov := range []string{"openai", "groq"} {
			if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: 1, Transcricao: prov}, agora); err != nil {
				t.Fatal(err)
			}
		}
	}
	contar := func(tabela, user string) (n int) {
		e.scalar(&n, "SELECT count(*) FROM "+tabela+" WHERE user_id = $1", user)
		return n
	}
	for _, tabela := range []string{"ai_provider_keys", "dictation_settings", "dictation_consents"} {
		if contar(tabela, a) == 0 || contar(tabela, b) == 0 {
			t.Fatalf("a %s não tem linhas antes de apagar", tabela)
		}
	}
	if contar("dictation_consents", a) != 2 {
		t.Fatalf("a conta tem %d aceites, quero 2 (um terminado, um em aberto)", contar("dictation_consents", a))
	}

	if _, err := s.ApagarDadosDaConta(e.ctx, a, contracttest.NewID(), agora); err != nil {
		t.Fatalf("ApagarDadosDaConta: %v", err)
	}
	for _, tabela := range []string{"ai_provider_keys", "dictation_settings", "dictation_consents"} {
		if n := contar(tabela, a); n != 0 {
			t.Errorf("depois de apagar, a %s ainda tem %d linhas da conta", tabela, n)
		}
		if n := contar(tabela, b); n == 0 {
			t.Errorf("apagar a conta A levou as linhas da %s da conta B", tabela)
		}
	}
	if contar("dictation_consents", b) != 2 {
		t.Errorf("a conta B tem %d aceites, quero 2", contar("dictation_consents", b))
	}
}

// Revogar o aceite e apagar as chaves é uma transação só: se apagar as chaves
// falha, o aceite continua em aberto (e as chaves, guardadas). Sem isso, uma
// falha no meio deixaria o aceite revogado com a chave ainda no banco, ou a
// chave apagada com o aceite em pé.
func TestPostgres_RevogarOAceiteEApagarAsChavesSaoUmaTransacaoSo(t *testing.T) {
	e := newPG(t)
	s := New(e.pool)
	u := e.newUser(t)
	agora := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if _, err := s.GravarAceite(e.ctx, u, cloudstore.NovoAceite{Versao: 1, Transcricao: "openai"}, agora); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GravarChaveDeIA(e.ctx, u, cloudstore.ChaveDeIA{
		Provedor: "openai", IDDaCifra: make([]byte, 8), Nonce: make([]byte, 12), Cifrado: make([]byte, 40), Final: "WXYZ",
	}, agora); err != nil {
		t.Fatal(err)
	}

	// Um gatilho que recusa apagar chaves, só neste banco de teste.
	e.exec(`CREATE FUNCTION recusa_apagar_chave() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'recusado de propósito'; END $$`)
	e.exec(`CREATE TRIGGER recusa_apagar_chave BEFORE DELETE ON ai_provider_keys FOR EACH ROW EXECUTE FUNCTION recusa_apagar_chave()`)

	if r, err := s.RevogarAceite(e.ctx, u, agora.Add(time.Hour)); err == nil {
		t.Fatalf("RevogarAceite com o apagar das chaves falhando devolveu %+v, quero erro", r)
	}
	aberto, err := s.LerAceiteAberto(e.ctx, u)
	if err != nil || !aberto.Aberto() || !aberto.EncerradoEm.IsZero() {
		t.Fatalf("o aceite depois da revogação que falhou: %+v, %v; quero ainda em aberto", aberto, err)
	}
	if _, err := s.LerChaveDeIA(e.ctx, u, "openai"); err != nil {
		t.Fatalf("a chave depois da revogação que falhou: %v", err)
	}

	// Sem o gatilho, revoga e apaga.
	e.exec(`DROP TRIGGER recusa_apagar_chave ON ai_provider_keys`)
	r, err := s.RevogarAceite(e.ctx, u, agora.Add(time.Hour))
	if err != nil || !r.Revogou || r.ChavesApagadas != 1 {
		t.Fatalf("RevogarAceite = %+v, %v", r, err)
	}
	if _, err := s.LerChaveDeIA(e.ctx, u, "openai"); !errors.Is(err, cloudstore.ErrChaveDeIAAusente) {
		t.Errorf("a chave depois de revogar: %v", err)
	}
}
