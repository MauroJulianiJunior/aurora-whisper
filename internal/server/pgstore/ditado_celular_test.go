package pgstore

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// A lista que um CHECK "<coluna> IN ('a', 'b')" aceita, lida do SQL.
func listaDoCheck(t *testing.T, sql, coluna string) []string {
	t.Helper()
	m := regexp.MustCompile(coluna + ` IN \(([^)]*)\)`).FindStringSubmatch(sql)
	if m == nil {
		t.Fatalf("a lista de %s não foi achada", coluna)
	}
	var out []string
	for _, p := range strings.Split(m[1], ",") {
		out = append(out, strings.Trim(strings.TrimSpace(p), "'"))
	}
	return out
}

func lerMigration(t *testing.T, arquivo string) string {
	t.Helper()
	raw, err := fs.ReadFile(migrations.FS, arquivo)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// As preferências e o aceite do ditado no celular (ADR-021, C3b), sem Postgres:
// o SQL do pacote lê e grava só as colunas da 0018 e da 0019, e as listas de
// provedores e a forma do modelo dos CHECKs são as do Go (o cloudstore). Uma
// mudança de um lado sem o outro quebra aqui, e não no primeiro PUT em
// produção.
func TestSQL_AsPreferenciasEOAceiteConferemComAs0018E0019(t *testing.T) {
	// A 0018: o que o SELECT lê e o INSERT grava são as colunas dela.
	cols := createTableColumns(t, "0018_dictation_settings_table.sql")
	lidas := listOf(colunasDasPreferencias)
	if len(lidas) != len(cols)-1 {
		t.Errorf("a leitura tem %d colunas, a 0018 tem %d (com o user_id)", len(lidas), len(cols))
	}
	for _, c := range lidas {
		if !cols[c] || c == "user_id" {
			t.Errorf("colunasDasPreferencias lê %q, que não é coluna da 0018", c)
		}
	}
	m := regexp.MustCompile(`INSERT INTO dictation_settings \(([^)]*)\)`).FindStringSubmatch(sqlGravarPreferencias)
	if m == nil {
		t.Fatal("o INSERT das preferências não foi achado")
	}
	if gravadas := listOf(m[1]); len(gravadas) != len(cols) {
		t.Errorf("o INSERT grava %v, a 0018 tem %d colunas", gravadas, len(cols))
	} else {
		for _, c := range gravadas {
			if !cols[c] {
				t.Errorf("o INSERT das preferências grava %q, que não é coluna da 0018", c)
			}
		}
	}

	// A 0019: o INSERT do aceite grava as colunas dela (o fim só no UPDATE).
	cols = createTableColumns(t, "0019_dictation_consents_table.sql")
	m = regexp.MustCompile(`INSERT INTO dictation_consents \(([^)]*)\)`).FindStringSubmatch(sqlGravarAceite)
	if m == nil {
		t.Fatal("o INSERT do aceite não foi achado")
	}
	gravadas := listOf(m[1])
	for _, c := range gravadas {
		if !cols[c] {
			t.Errorf("o INSERT do aceite grava %q, que não é coluna da 0019", c)
		}
	}
	if len(gravadas) != len(cols)-2 || slices.Contains(gravadas, "ended_at") || slices.Contains(gravadas, "end_reason") {
		t.Errorf("o INSERT do aceite grava %v: o aceite nasce em aberto, sem ended_at nem end_reason", gravadas)
	}
	// O SELECT do aceite lê as colunas dela (com o COALESCE dos nulos).
	for _, c := range []string{"id", "notice_version", "asr_provider", "polish_provider", "accepted_at", "ended_at", "end_reason"} {
		if !cols[c] || !strings.Contains(colunasDoAceite, c) {
			t.Errorf("colunasDoAceite não lê %q, ou ela não é coluna da 0019", c)
		}
	}

	// As listas de provedores dos CHECKs são as do Go, cada papel a sua.
	s18, s19 := lerMigration(t, "0018_dictation_settings_table.sql"), lerMigration(t, "0019_dictation_consents_table.sql")
	for nome, sql := range map[string]string{"0018": s18, "0019": s19} {
		if got := listaDoCheck(t, sql, "asr_provider"); !slices.Equal(got, cloudstore.ProvedoresDeTranscricao) {
			t.Errorf("a %s aceita %v na transcrição, o cloudstore %v", nome, got, cloudstore.ProvedoresDeTranscricao)
		}
		if got := listaDoCheck(t, sql, "polish_provider"); !slices.Equal(got, cloudstore.ProvedoresDePolish) {
			t.Errorf("a %s aceita %v no Polish, o cloudstore %v", nome, got, cloudstore.ProvedoresDePolish)
		}
	}
	if got := listaDoCheck(t, s19, "end_reason"); !slices.Equal(got, []string{cloudstore.FimRevogado, cloudstore.FimSubstituido}) {
		t.Errorf("a 0019 aceita %v como motivo do fim, o cloudstore %q e %q", got, cloudstore.FimRevogado, cloudstore.FimSubstituido)
	}
	// E os fins que o SQL do pacote grava são esses.
	if !strings.Contains(sqlSubstituirAceite, "'"+cloudstore.FimSubstituido+"'") || !strings.Contains(sqlRevogarAceite, "'"+cloudstore.FimRevogado+"'") {
		t.Error("o SQL do fim do aceite não grava os motivos do cloudstore")
	}
}

// A forma do nome do modelo é a mesma no CHECK da 0018 (a ARE do Postgres) e em
// cloudstore.ModeloValido (a RE2 do Go): a expressão do SQL, aplicada pelo Go a
// exemplos, concorda com a função. O Postgres a confere de verdade em
// TestPostgres_AsPreferenciasEOAceiteNoBanco.
func TestSQL_AFormaDoModeloEIgualNoSQLENoGo(t *testing.T) {
	sql := lerMigration(t, "0018_dictation_settings_table.sql")
	asr := regexp.MustCompile(`asr_model ~ '([^']+)'`).FindStringSubmatch(sql)
	polish := regexp.MustCompile(`polish_model ~ '([^']+)'`).FindStringSubmatch(sql)
	if asr == nil || polish == nil {
		t.Fatal("as expressões do modelo não foram achadas na 0018")
	}
	reAsr, err := regexp.Compile(asr[1])
	if err != nil {
		t.Fatalf("a expressão do asr_model (%s) não compila: %v", asr[1], err)
	}
	rePolish, err := regexp.Compile(polish[1])
	if err != nil {
		t.Fatalf("a expressão do polish_model (%s) não compila: %v", polish[1], err)
	}
	if cloudstore.TamanhoMaximoDoModelo != 100 || !strings.Contains(asr[1], "{1,100}") || !strings.Contains(polish[1], "{0,100}") {
		t.Error("o teto do modelo não é 100 nos dois lados")
	}
	for _, m := range []string{
		"gpt-4o-mini-transcribe", "whisper-large-v3-turbo", "openai/gpt-oss-20b", "ft:gpt-4o-mini:org::id", "a.b_c-d@e",
		strings.Repeat("m", 100), "A", "1",
	} {
		if !cloudstore.ModeloValido(m) || !reAsr.MatchString(m) || !rePolish.MatchString(m) {
			t.Errorf("o modelo %q devia valer nos dois lados", m)
		}
	}
	for _, m := range []string{
		"", "gpt 4", "gpt\n4", "gpt\t4", "modelo-é", "a;b", `a\b`, "a'b", `a"b`, "a<b", "a#b", "a?b", "a,b", "a$b",
		strings.Repeat("m", 101), " gpt", "gpt ", "gpt\n",
	} {
		if cloudstore.ModeloValido(m) || reAsr.MatchString(m) {
			t.Errorf("o modelo %q devia ser recusado nos dois lados", m)
		}
		// O Polish aceita o vazio, e só ele a mais.
		if m != "" && rePolish.MatchString(m) {
			t.Errorf("o modelo do Polish %q devia ser recusado", m)
		}
	}
	if !rePolish.MatchString("") || reAsr.MatchString("") {
		t.Error("o modelo vazio vale só no Polish (desligado)")
	}
}

// O "apagar meus dados" apaga toda tabela que tem user_id, fora as duas que ele
// trata à parte: o contador (que fica, com o epoch novo) e as sessões (que ficam
// as de quem pediu). Uma tabela nova de dados da conta, sem o DELETE aqui,
// quebra esta leitura, e não fica para trás sem ninguém ver. As três do ditado
// no celular, em particular.
func TestSQL_ApagarMeusDadosCobreToda_TabelaComUserID(t *testing.T) {
	apagadas := map[string]bool{}
	for _, passo := range apagarDaConta {
		m := regexp.MustCompile(`^DELETE FROM ([a-z_]+) WHERE`).FindStringSubmatch(passo.sql)
		if m == nil {
			t.Errorf("o passo %q não é um DELETE FROM <tabela> WHERE", passo.op)
			continue
		}
		apagadas[m[1]] = true
	}
	trataAparte := map[string]string{
		"user_sync_counters": "o contador fica, com o epoch novo",
		"web_sessions":       "as outras sessões saem, e a de quem pediu fica",
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	comUserID := regexp.MustCompile(`(?m)^\s*user_id\s+UUID\b`)
	tabela := regexp.MustCompile(`(?m)^CREATE TABLE ([a-z_]+) \(`)
	vistas := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		sql := lerMigration(t, e.Name())
		m := tabela.FindStringSubmatch(sql)
		if m == nil || !comUserID.MatchString(sql) {
			continue
		}
		vistas++
		if _, aparte := trataAparte[m[1]]; aparte {
			continue
		}
		if !apagadas[m[1]] {
			t.Errorf("a tabela %s tem user_id e o \"apagar meus dados\" não a apaga (apagarDaConta)", m[1])
		}
	}
	if vistas < 12 {
		t.Errorf("a leitura viu só %d tabelas com user_id", vistas)
	}
	for _, t3 := range []string{"ai_provider_keys", "dictation_settings", "dictation_consents"} {
		if !apagadas[t3] {
			t.Errorf("o \"apagar meus dados\" não apaga a %s", t3)
		}
	}
}
