package pgmigrate_test

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// Testes PUROS do leitor (Load) e do planejador (Plan): rodam sempre, sem
// Postgres. As regras que importam moram aqui; run_test.go prova só que
// Run as aplica contra um banco de verdade.

const secretMarker = "SEGREDO_NO_CORPO_DO_SQL"

func mapFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

const okSQL = "CREATE TABLE t (id INTEGER);\n"

func TestLoad_AsMigrationsDoProjeto(t *testing.T) {
	got, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load(migrations.FS): %v", err)
	}
	if len(got) == 0 {
		t.Fatal("nenhuma migration embutida")
	}

	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]string{}
	for i, m := range got {
		if m.Version != i+1 {
			t.Errorf("posição %d: versão %d, quero %d", i, m.Version, i+1)
		}
		if want := strings.TrimSuffix(m.File, ".sql"); !strings.HasSuffix(want, "_"+m.Name) {
			t.Errorf("%s: o nome %q não bate com o arquivo", m.File, m.Name)
		}
		if !hex64.MatchString(m.Checksum) {
			t.Errorf("%s: checksum %q não é um SHA-256 em hexadecimal", m.File, m.Checksum)
		}
		if other, dup := seen[m.Checksum]; dup {
			t.Errorf("%s e %s têm o mesmo checksum", m.File, other)
		}
		seen[m.Checksum] = m.File
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("%s: SQL vazio", m.File)
		}
	}
	if got[0].File != "0001_users_table.sql" {
		t.Errorf("a primeira migration é %s, quero 0001_users_table.sql", got[0].File)
	}
}

// O que está embutido no binário é o que está em migrations/ no disco: o
// go:embed lê os mesmos arquivos, e este teste pega o dia em que alguém
// trocar o embed por outra coisa.
func TestLoad_EmbutidoIgualAoDisco(t *testing.T) {
	embedded, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := pgmigrate.Load(os.DirFS("../../../migrations"))
	if err != nil {
		t.Fatalf("Load(disco): %v", err)
	}
	if len(embedded) != len(onDisk) {
		t.Fatalf("embutidas %d, em disco %d", len(embedded), len(onDisk))
	}
	for i := range embedded {
		if embedded[i].Checksum != onDisk[i].Checksum || embedded[i].File != onDisk[i].File {
			t.Errorf("%s difere entre o embed e o disco", embedded[i].File)
		}
	}
}

func TestLoad_ChecksumNaoDependeDoFimDeLinha(t *testing.T) {
	lf, err := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": "CREATE TABLE a (\n    id INTEGER\n);\n"}))
	if err != nil {
		t.Fatal(err)
	}
	crlf, err := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": "CREATE TABLE a (\r\n    id INTEGER\r\n);\r\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if lf[0].Checksum != crlf[0].Checksum {
		t.Error("um checkout com CRLF mudou o checksum (um Windows e um Linux discordariam sobre a mesma migration)")
	}
	if strings.Contains(crlf[0].SQL, "\r") {
		t.Error("o SQL guardado ainda tem \\r")
	}
}

func TestLoad_MudarUmByteMudaOChecksum(t *testing.T) {
	a, _ := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": okSQL}))
	b, _ := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": okSQL + "-- x\n"}))
	if len(a) != 1 || len(b) != 1 || a[0].Checksum == b[0].Checksum {
		t.Error("um comentário a mais não mudou o checksum")
	}
}

func TestLoad_IgnoraOQueNaoESQL(t *testing.T) {
	got, err := pgmigrate.Load(mapFS(map[string]string{
		"0001_a.sql":        okSQL,
		"README.md":         "# nada",
		"schema_test.go":    "package x",
		"sub/0002_b.sql":    okSQL, // subdiretório: fora
		"0002_b.sql.bak":    okSQL, // não termina em .sql
		"Leiame sem sufixo": "x",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].File != "0001_a.sql" {
		t.Errorf("Load devolveu %+v, quero só 0001_a.sql", got)
	}
}

func TestLoad_Ordena(t *testing.T) {
	got, err := pgmigrate.Load(mapFS(map[string]string{
		"0003_c.sql": okSQL, "0001_a.sql": okSQL, "0002_b.sql": okSQL,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"0001_a.sql", "0002_b.sql", "0003_c.sql"} {
		if got[i].File != want {
			t.Errorf("posição %d: %s, quero %s", i, got[i].File, want)
		}
	}
}

func TestLoad_Recusa(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  error
	}{
		{"nome sem quatro dígitos", map[string]string{"01_a.sql": okSQL}, pgmigrate.ErrBadFileName},
		{"nome com maiúscula", map[string]string{"0001_Users.sql": okSQL}, pgmigrate.ErrBadFileName},
		{"nome com hífen", map[string]string{"0001-users.sql": okSQL}, pgmigrate.ErrBadFileName},
		{"nome com espaço", map[string]string{"0001_a b.sql": okSQL}, pgmigrate.ErrBadFileName},
		{"nome sem descrição", map[string]string{"0001_.sql": okSQL}, pgmigrate.ErrBadFileName},
		{"versão zero", map[string]string{"0000_a.sql": okSQL}, pgmigrate.ErrBadFileName},
		{"mesma versão duas vezes", map[string]string{"0001_a.sql": okSQL, "0001_b.sql": okSQL}, pgmigrate.ErrDuplicateVersion},
		{"buraco no meio", map[string]string{"0001_a.sql": okSQL, "0003_c.sql": okSQL}, pgmigrate.ErrGap},
		{"não começa em 0001", map[string]string{"0002_b.sql": okSQL}, pgmigrate.ErrGap},
		{"arquivo vazio", map[string]string{"0001_a.sql": ""}, pgmigrate.ErrEmpty},
		{"só comentário", map[string]string{"0001_a.sql": "-- nada\n/* nem isto */\n"}, pgmigrate.ErrEmpty},
		{"BEGIN", map[string]string{"0001_a.sql": "BEGIN;\n" + okSQL}, pgmigrate.ErrTransactionControl},
		{"COMMIT em minúscula", map[string]string{"0001_a.sql": okSQL + "commit;\n"}, pgmigrate.ErrTransactionControl},
		{"ROLLBACK", map[string]string{"0001_a.sql": okSQL + "\n  ROLLBACK;"}, pgmigrate.ErrTransactionControl},
		{"START TRANSACTION", map[string]string{"0001_a.sql": "START   TRANSACTION;\n" + okSQL}, pgmigrate.ErrTransactionControl},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := pgmigrate.Load(mapFS(c.files))
			if !errors.Is(err, c.want) {
				t.Fatalf("erro = %v, quero %v", err, c.want)
			}
		})
	}
}

func TestLoad_NaoConfundeTransacaoComTextoParecido(t *testing.T) {
	sql := `-- BEGIN; COMMIT; num comentário não conta
/* nem ROLLBACK; num bloco */
CREATE TABLE beginning_log (id INTEGER, commit_hash TEXT);
CREATE FUNCTION f() RETURNS integer AS $$ BEGIN RETURN 1; END $$ LANGUAGE plpgsql;
`
	if _, err := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": sql})); err != nil {
		t.Fatalf("Load recusou SQL válido: %v", err)
	}
}

// O lint olha só o código: o que está num corpo de função (com DECLARE, BEGIN,
// END e ";" dentro), num literal, num identificador entre aspas ou num
// comentário aninhado não é um comando do arquivo, mesmo que comece por
// BEGIN ou COMMIT depois de cortar o texto em ";".
func TestLoad_CorpoDeFuncaoLiteralEComentarioNaoSaoComandos(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"função PL/pgSQL com DECLARE", `CREATE FUNCTION f() RETURNS integer AS $$
DECLARE
    x integer;
BEGIN
    x := 1;
    RETURN x;
END
$$ LANGUAGE plpgsql;
`},
		{"bloco DO com DECLARE", "DO $$ DECLARE v integer; BEGIN v := 1; END $$;\n"},
		{"delimitador com tag", "CREATE FUNCTION f() RETURNS void AS $corpo$ SELECT 1; COMMIT; $corpo$ LANGUAGE sql;\n"},
		{"dois delimitadores no mesmo arquivo", "DO $a$ BEGIN PERFORM 1; END $a$;\nDO $b$ DECLARE y integer; BEGIN y := 2; END $b$;\n"},
		{"literal com ; e BEGIN", "INSERT INTO t VALUES ('x; BEGIN; y');\n"},
		{"literal com aspa dobrada", "INSERT INTO t VALUES ('it''s; COMMIT; ok');\n"},
		{"literal E com aspa escapada", `INSERT INTO t VALUES (E'it\'s; COMMIT;');` + "\n"},
		{"identificador com ; e COMMIT", `CREATE TABLE "a;COMMIT" (id INTEGER);` + "\n"},
		{"comentário de bloco aninhado", "/* a /* b */ COMMIT; */\n" + okSQL},
		{"-- dentro de literal não abre comentário", "INSERT INTO t VALUES ('a--b');\n" + okSQL},
		{"parâmetro $1 não abre corpo", "SELECT $1;\n" + okSQL},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": c.sql})); err != nil {
				t.Fatalf("Load recusou SQL válido: %v", err)
			}
		})
	}
}

// E o que continua sendo controle de transação, mesmo parecendo texto: um
// COMMIT depois do corpo, ou depois de um literal com "--", é um comando do
// arquivo.
func TestLoad_ControleDeTransacaoForaDoCorpoContinuaRecusado(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"COMMIT depois do corpo de função", "CREATE FUNCTION f() RETURNS integer AS $$ DECLARE x integer; BEGIN RETURN 1; END $$ LANGUAGE plpgsql;\nCOMMIT;\n"},
		{"BEGIN antes de uma função com DECLARE", "BEGIN;\nDO $$ DECLARE v integer; BEGIN v := 1; END $$;\n"},
		{"COMMIT depois de literal com -- na mesma linha", "INSERT INTO t VALUES ('a--b'); COMMIT;\n"},
		{"COMMIT depois de literal com ;", "INSERT INTO t VALUES ('x;y'); COMMIT;\n"},
		{"COMMIT depois de literal E com aspa escapada", `INSERT INTO t VALUES (E'it\'s'); COMMIT;` + "\n"},
		{"COMMIT depois de identificador entre aspas", `CREATE TABLE "a;b" (id INTEGER); COMMIT;` + "\n"},
		{"COMMIT depois de comentário aninhado", "/* a /* b */ c */ COMMIT;\n"},
		// O "$" no meio de um identificador não abre corpo: se abrisse, o COMMIT
		// ficaria escondido entre os dois "$b$".
		{"COMMIT entre identificadores com $", "CREATE TABLE a$b$ (id INTEGER); COMMIT; CREATE TABLE c$b$ (id INTEGER);\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := pgmigrate.Load(mapFS(map[string]string{"0001_a.sql": c.sql}))
			if !errors.Is(err, pgmigrate.ErrTransactionControl) {
				t.Fatalf("erro = %v, quero ErrTransactionControl", err)
			}
		})
	}
}

func TestLoad_ErroNuncaTrazOCorpoDoSQL(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"vazio":      {"0001_a.sql": "-- " + secretMarker + "\n"},
		"transação":  {"0001_a.sql": "BEGIN; -- " + secretMarker + "\n"},
		"nome ruim":  {"0001_A.sql": secretMarker},
		"duplicada":  {"0001_a.sql": secretMarker, "0001_b.sql": secretMarker},
		"com buraco": {"0002_a.sql": secretMarker},
	} {
		_, err := pgmigrate.Load(mapFS(files))
		if err == nil {
			t.Errorf("%s: esperava erro", name)
			continue
		}
		if strings.Contains(err.Error(), secretMarker) {
			t.Errorf("%s: o erro traz o corpo do SQL: %v", name, err)
		}
	}
}

// ---- Plan ----

func migs(names ...string) []pgmigrate.Migration {
	out := make([]pgmigrate.Migration, len(names))
	for i, n := range names {
		out[i] = pgmigrate.Migration{
			Version:  i + 1,
			Name:     n,
			File:     "000" + string(rune('1'+i)) + "_" + n + ".sql",
			Checksum: "sum-" + n,
		}
	}
	return out
}

func applied(m ...pgmigrate.Migration) []pgmigrate.Applied {
	out := make([]pgmigrate.Applied, len(m))
	for i, x := range m {
		out[i] = pgmigrate.Applied{Version: x.Version, Name: x.Name, Checksum: x.Checksum}
	}
	return out
}

func files(m []pgmigrate.Migration) string {
	var names []string
	for _, x := range m {
		names = append(names, x.File)
	}
	return strings.Join(names, ",")
}

func TestPlan_BancoVazioAplicaTudo(t *testing.T) {
	all := migs("a", "b", "c")
	for _, empty := range [][]pgmigrate.Applied{nil, {}} {
		got, err := pgmigrate.Plan(all, empty)
		if err != nil {
			t.Fatal(err)
		}
		if files(got) != files(all) {
			t.Errorf("pendentes = %s, quero todas", files(got))
		}
	}
}

func TestPlan_TudoAplicadoNaoFazNada(t *testing.T) {
	all := migs("a", "b", "c")
	got, err := pgmigrate.Plan(all, applied(all...))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("pendentes = %s, quero nenhuma", files(got))
	}
}

func TestPlan_AplicaSoOQueFalta(t *testing.T) {
	all := migs("a", "b", "c", "d")
	got, err := pgmigrate.Plan(all, applied(all[:2]...))
	if err != nil {
		t.Fatal(err)
	}
	if files(got) != files(all[2:]) {
		t.Errorf("pendentes = %s, quero %s", files(got), files(all[2:]))
	}
}

func TestPlan_AceitaOBancoEmQualquerOrdem(t *testing.T) {
	all := migs("a", "b", "c")
	rows := applied(all...)
	rows[0], rows[2] = rows[2], rows[0]
	got, err := pgmigrate.Plan(all, rows)
	if err != nil || len(got) != 0 {
		t.Errorf("Plan = %v, %v; quero nenhuma pendente e sem erro", files(got), err)
	}
}

func TestPlan_ArquivoAlteradoDepoisDeAplicadoEhRecusado(t *testing.T) {
	all := migs("a", "b", "c")
	rows := applied(all[:2]...)
	all[1].Checksum = "sum-b-editada"
	got, err := pgmigrate.Plan(all, rows)
	if !errors.Is(err, pgmigrate.ErrChecksum) {
		t.Fatalf("erro = %v, quero ErrChecksum", err)
	}
	if got != nil {
		t.Errorf("com migration alterada nada pode ser planejado, veio %s", files(got))
	}
	if !strings.Contains(err.Error(), all[1].File) {
		t.Errorf("o erro não diz qual arquivo foi alterado: %v", err)
	}
}

func TestPlan_AlteradaVenceAPendente(t *testing.T) {
	// Mesmo com migration nova esperando, a alterada barra tudo: não se aplica
	// nada em cima de uma história que não confere.
	all := migs("a", "b", "c")
	rows := applied(all[:1]...)
	all[0].Checksum = "outra"
	if _, err := pgmigrate.Plan(all, rows); !errors.Is(err, pgmigrate.ErrChecksum) {
		t.Fatalf("erro = %v, quero ErrChecksum", err)
	}
}

func TestPlan_AplicadaQueNaoExisteNoBinario(t *testing.T) {
	all := migs("a", "b", "c")
	rows := applied(all...)
	_, err := pgmigrate.Plan(all[:2], rows) // o binário só conhece a e b
	if !errors.Is(err, pgmigrate.ErrMissingFile) {
		t.Fatalf("erro = %v, quero ErrMissingFile", err)
	}
	if !strings.Contains(err.Error(), "0003_c") {
		t.Errorf("o erro não diz qual migration sumiu: %v", err)
	}
}

func TestPlan_NomeMudouDepoisDeAplicado(t *testing.T) {
	all := migs("a", "b")
	rows := applied(all...)
	all[1].Name = "b_renomeada"
	if _, err := pgmigrate.Plan(all, rows); !errors.Is(err, pgmigrate.ErrNameChanged) {
		t.Fatalf("erro = %v, quero ErrNameChanged", err)
	}
}

func TestPlan_PendenteMaisAntigaQueUmaAplicada(t *testing.T) {
	all := migs("a", "b", "c", "d")
	rows := applied(all[0], all[1], all[3]) // 1, 2 e 4 aplicadas; a 3 ficou de fora
	_, err := pgmigrate.Plan(all, rows)
	if !errors.Is(err, pgmigrate.ErrOutOfOrder) {
		t.Fatalf("erro = %v, quero ErrOutOfOrder", err)
	}
	if !strings.Contains(err.Error(), all[2].File) {
		t.Errorf("o erro não diz qual migration está fora de ordem: %v", err)
	}
}

func TestPlan_NaoAlteraAsEntradas(t *testing.T) {
	all := migs("a", "b", "c")
	rows := applied(all[:2]...)
	rows[0], rows[1] = rows[1], rows[0]
	before := rows[0]
	if _, err := pgmigrate.Plan(all, rows); err != nil {
		t.Fatal(err)
	}
	if rows[0] != before {
		t.Error("Plan reordenou o slice do chamador")
	}
}

func TestPlan_ErroNaoTrazChecksumInteiro(t *testing.T) {
	all := migs("a")
	rows := applied(all...)
	all[0].Checksum = strings.Repeat("f", 64)
	rows[0].Checksum = strings.Repeat("e", 64)
	_, err := pgmigrate.Plan(all, rows)
	if err == nil {
		t.Fatal("esperava erro")
	}
	if strings.Contains(err.Error(), strings.Repeat("f", 64)) || strings.Contains(err.Error(), strings.Repeat("e", 64)) {
		t.Errorf("o erro traz o checksum inteiro: %v", err)
	}
}
