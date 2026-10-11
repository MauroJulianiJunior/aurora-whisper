package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// O Postgres da nuvem (F11e): o init que cria o papel da aplicação e a
// conferência dos cinco ajustes de log. Os dois rodam aqui com o psql e o
// docker de mentira.

const (
	initDoPapel     = "deploy/postgres/init/10-papel-da-aplicacao.sh"
	conferirPostgre = "deploy/postgres/conferir-postgres.sh"
)

func TestPostgres_ScriptsSoASCIIELF(t *testing.T) {
	for _, s := range []string{initDoPapel, conferirPostgre} {
		texto := ler(t, s)
		exigirASCII(t, s, texto)
		exigirLF(t, s, texto)
		if !strings.HasPrefix(texto, "#!/usr/bin/env bash\n") {
			t.Errorf("%s deveria começar com #!/usr/bin/env bash", s)
		}
		exigirContem(t, s, codigo(texto), "set -euo pipefail\n")
		exigirNaoContem(t, s, codigo(texto), "set -x", "/usr/bin/docker", "/usr/bin/psql", "sudo ")
	}
}

func TestPostgres_InitCriaOPapelSemPoderEDonoDoBanco(t *testing.T) {
	texto := ler(t, initDoPapel)
	c := codigo(texto)
	exigirContem(t, initDoPapel, c,
		`CREATE ROLE :"papel" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD :'senha';`,
		`CREATE DATABASE :"banco" OWNER :"papel";`,
		`REVOKE ALL ON DATABASE :"banco" FROM PUBLIC;`,
		"PAPEL='aurora'", "BANCO='aurora'",
		"-v ON_ERROR_STOP=1",
		// A senha sai do arquivo dentro do psql: nunca pela linha de comando.
		`\set senha `+"`cat :'arquivo'`",
		`--set=arquivo="$ARQUIVO_DA_SENHA"`,
		`\unset senha`,
		// O papel do backup: sem poder, só de leitura, com a entrada no banco
		// e a senha dele, de outro arquivo.
		"PAPEL_BACKUP='aurora_backup'",
		`CREATE ROLE :"papel_backup" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD :'senha_backup';`,
		`GRANT pg_read_all_data TO :"papel_backup";`,
		`GRANT CONNECT ON DATABASE :"banco" TO :"papel_backup";`,
		`\set senha_backup `+"`cat :'arquivo_backup'`",
		`--set=arquivo_backup="$ARQUIVO_DA_SENHA_BACKUP"`,
		`\unset senha_backup`,
		`ARQUIVO_DA_SENHA_BACKUP="${AURORA_BACKUP_SENHA_FILE:-/run/secrets/postgres-backup-senha}"`,
	)
	// Tudo num subshell, para o source da imagem não herdar o set -u nem o
	// exit.
	if !regexp.MustCompile(`(?s)\n\(\nset -euo pipefail\n.*\n\)\n$`).MatchString(texto) {
		t.Error("o corpo do init deveria estar todo num subshell: ( set -euo pipefail ... )")
	}
	if regexp.MustCompile(`(?i)PASSWORD\s+'`).MatchString(c) {
		t.Error("senha literal no init")
	}
	// O log de comando sai da sessão ANTES de a senha entrar num comando: um
	// CREATE ROLE que falha não deixa a linha STATEMENT, com a senha, no log.
	senha := strings.Index(c, `\set senha`)
	for _, set := range []string{"SET log_statement = none;\n", "SET log_min_duration_statement = -1;\n", "SET log_min_error_statement = panic;\n"} {
		if i := strings.Index(c, set); i < 0 || senha < 0 || i > senha {
			t.Errorf("o init deveria abrir a sessão do psql com %q antes do \\set senha", strings.TrimSpace(set))
		}
	}
	// O papel do backup nunca escreve: nada de pg_write_all_data, de dono,
	// de ALL nem de outro GRANT.
	for _, l := range linhasDeCodigo(texto) {
		if strings.HasPrefix(l.texto, "GRANT ") && l.texto != `GRANT pg_read_all_data TO :"papel_backup";` &&
			l.texto != `GRANT CONNECT ON DATABASE :"banco" TO :"papel_backup";` {
			t.Errorf("linha %d: GRANT inesperado no init: %s", l.n, l.texto)
		}
	}
	exigirNaoContem(t, initDoPapel, c, "SUPERUSER LOGIN", "CREATEDB LOGIN", "echo \"$senha", "cat \"$ARQUIVO_DA_SENHA",
		"pg_write_all_data", `OWNER :"papel_backup"`)
}

// O init de verdade, com o psql de mentira: o que ele recebe pela linha de
// comando e pela entrada.
func psqlQueAnota(a *ambienteBash) {
	a.falso("psql", `cat > "$REGISTRO.entrada"`)
}

// As duas senhas de teste, distintas por padrão: a do papel e a do backup.
var (
	senhaAuroraDeTeste = strings.Repeat("ab12", 16)
	senhaBackupDeTeste = strings.Repeat("cd34", 16)
)

func senhaDeTesteEm(t *testing.T, a *ambienteBash, nome, conteudo string) string {
	t.Helper()
	p := filepath.Join(a.dir, nome)
	if err := os.WriteFile(p, []byte(conteudo), 0o444); err != nil {
		t.Fatal(err)
	}
	return p
}

// senhasDoInit grava os dois arquivos de senha e aponta o init para eles.
func senhasDoInit(t *testing.T, a *ambienteBash, aurora, backup string) {
	t.Helper()
	a.env["AURORA_SENHA_FILE"] = posix(senhaDeTesteEm(t, a, "senha-aurora", aurora))
	a.env["AURORA_BACKUP_SENHA_FILE"] = posix(senhaDeTesteEm(t, a, "senha-backup", backup))
}

func TestPostgres_InitComOPsqlDeMentira(t *testing.T) {
	a := novoAmbienteBash(t)
	psqlQueAnota(a)
	senhasDoInit(t, a, senhaAuroraDeTeste+"\n", senhaBackupDeTeste+"\n")
	a.env["POSTGRES_USER"] = "postgres"
	r := a.rodar(initDoPapel, nil)
	if r.codigo != 0 {
		t.Fatalf("o init falhou (%d): %s", r.codigo, r.tudo())
	}
	if !strings.Contains(r.saida, "OK: papel aurora criado") || !strings.Contains(r.saida, "papel aurora_backup so de leitura") {
		t.Errorf("faltou o OK: %q", r.saida)
	}
	ch := a.chamadas()
	if len(ch) != 1 || !strings.HasPrefix(ch[0], "psql ") {
		t.Fatalf("o init deveria chamar o psql uma vez: %q", ch)
	}
	for _, want := range []string{"[-v] [ON_ERROR_STOP=1]", "[--username] [postgres]", "[--dbname] [postgres]", "[--set=papel=aurora]",
		"[--set=papel_backup=aurora_backup]", "[--set=banco=aurora]"} {
		if !strings.Contains(ch[0], want) {
			t.Errorf("o psql deveria receber %s: %s", want, ch[0])
		}
	}
	entrada, err := os.ReadFile(a.registro + ".entrada")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entrada), "NOSUPERUSER") || !strings.Contains(string(entrada), "OWNER :\"papel\"") ||
		!strings.Contains(string(entrada), "GRANT pg_read_all_data") {
		t.Errorf("a entrada do psql não tem o SQL: %q", entrada)
	}
	if !strings.HasPrefix(string(entrada), "SET log_statement = none;\nSET log_min_duration_statement = -1;\nSET log_min_error_statement = panic;\n") {
		t.Errorf("a sessão do psql deveria começar desligando o log de comando: %q", entrada)
	}
	for nome, texto := range map[string]string{"a saída": r.tudo(), "a linha de comando": ch[0], "a entrada do psql": string(entrada)} {
		if strings.Contains(texto, senhaAuroraDeTeste) || strings.Contains(texto, senhaBackupDeTeste) {
			t.Errorf("uma senha aparece n%s", nome)
		}
	}
}

func TestPostgres_InitRecusaSenhaRuimSemChamarOPsql(t *testing.T) {
	boa := senhaAuroraDeTeste + "\n"
	outraBoa := senhaBackupDeTeste + "\n"
	for nome, conteudo := range map[string]string{
		"vazia":         "",
		"curta":         "abc123\n",
		"duas linhas":   strings.Repeat("ab12", 16) + "\n" + strings.Repeat("cd34", 16) + "\n",
		"não hex":       strings.Repeat("zz12", 16) + "\n",
		"com maiúscula": strings.Repeat("AB12", 16) + "\n",
	} {
		for _, qual := range []string{"aurora", "backup"} {
			t.Run(qual+" "+nome, func(t *testing.T) {
				a := novoAmbienteBash(t)
				psqlQueAnota(a)
				if qual == "aurora" {
					senhasDoInit(t, a, conteudo, outraBoa)
				} else {
					senhasDoInit(t, a, boa, conteudo)
				}
				r := a.rodar(initDoPapel, nil)
				if r.codigo == 0 || !strings.Contains(r.erro, "FALHOU") {
					t.Fatalf("deveria falhar: %d %q", r.codigo, r.tudo())
				}
				if a.chamou("psql") {
					t.Error("chamou o psql com a senha ruim")
				}
			})
		}
	}
	t.Run("as duas senhas iguais", func(t *testing.T) {
		a := novoAmbienteBash(t)
		psqlQueAnota(a)
		senhasDoInit(t, a, boa, boa)
		r := a.rodar(initDoPapel, nil)
		if r.codigo == 0 || a.chamou("psql") || !strings.Contains(r.erro, "e a mesma do aurora") {
			t.Fatalf("deveria falhar sem chamar o psql: %d %q", r.codigo, r.tudo())
		}
	})
	for _, env := range []string{"AURORA_SENHA_FILE", "AURORA_BACKUP_SENHA_FILE"} {
		t.Run("sem o arquivo de "+env, func(t *testing.T) {
			a := novoAmbienteBash(t)
			psqlQueAnota(a)
			senhasDoInit(t, a, boa, outraBoa)
			a.env[env] = posix(filepath.Join(a.dir, "nao-existe"))
			r := a.rodar(initDoPapel, nil)
			if r.codigo == 0 || a.chamou("psql") || !strings.Contains(r.erro, "("+env+")") {
				t.Fatalf("deveria falhar sem chamar o psql: %d %q", r.codigo, r.tudo())
			}
		})
	}
	for _, c := range []struct{ env, valor string }{
		{"POSTGRES_DB", "aurora"},
		// O superusuário com o nome de um dos papéis: o CREATE ROLE falharia
		// (e, sem o SET da sessão, deixaria a senha no log).
		{"POSTGRES_USER", "aurora"},
		{"POSTGRES_USER", "aurora_backup"},
	} {
		t.Run(c.env+"="+c.valor, func(t *testing.T) {
			a := novoAmbienteBash(t)
			psqlQueAnota(a)
			senhasDoInit(t, a, boa, outraBoa)
			a.env[c.env] = c.valor
			r := a.rodar(initDoPapel, nil)
			if r.codigo == 0 || a.chamou("psql") || !strings.Contains(r.erro, c.env+" nao pode ser "+c.valor+":") {
				t.Fatalf("deveria falhar sem chamar o psql: %d %q", r.codigo, r.tudo())
			}
		})
	}
}

// A imagem oficial faz "." (source) do init que não é executável, dentro do
// script dela, que roda com set -Eeo pipefail. O set -u e o exit do init não
// podem vazar; a falha, sim, tem de parar a inicialização.
func TestPostgres_InitPorSourceNaoVazaOsAjustes(t *testing.T) {
	abs, err := filepath.Abs(caminho(filepath.FromSlash(initDoPapel)))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		nome, senha, quero string
		codigo             int
	}{
		{"senha boa", senhaAuroraDeTeste + "\n", "DEPOIS sem-u", 0},
		{"senha ruim", "x\n", "", 1},
	} {
		t.Run(c.nome, func(t *testing.T) {
			a := novoAmbienteBash(t)
			psqlQueAnota(a)
			senhasDoInit(t, a, c.senha, senhaBackupDeTeste+"\n")
			imagem := filepath.Join(a.dir, "entrypoint.sh")
			texto := "#!/usr/bin/env bash\nset -Eeo pipefail\n. \"$1\"\n" +
				"case $- in *u*) echo 'DEPOIS com-u' ;; *) echo 'DEPOIS sem-u' ;; esac\n"
			if err := os.WriteFile(imagem, []byte(texto), 0o755); err != nil {
				t.Fatal(err)
			}
			r := a.rodar(imagem, nil, posix(abs))
			if r.codigo != c.codigo {
				t.Fatalf("código %d, quero %d: %q", r.codigo, c.codigo, r.tudo())
			}
			if c.quero != "" && !strings.Contains(r.saida, c.quero) {
				t.Errorf("quero %q na saída: %q", c.quero, r.saida)
			}
			if c.codigo != 0 && strings.Contains(r.saida, "DEPOIS") {
				t.Error("a falha do init não parou o script da imagem")
			}
		})
	}
}

// --- conferir-postgres.sh -----------------------------------------------------

// dockerDoPostgres é o docker de mentira do conferir: responde ao pg_isready e
// ao psql -c pelos valores das variáveis FALSO_*.
const dockerDoPostgres = `
[ "$1" = exec ] || { echo "docker de mentira: so exec" >&2; exit 99; }
if [ "${FALSO_PG_FORA:-}" = 1 ]; then echo "Error: No such container: $2" >&2; exit 1; fi
[ "$3" = pg_isready ] && exit 0
sql="${@: -1}"
case "$sql" in
  "SHOW log_error_verbosity") printf '%s\n' "${FALSO_log_error_verbosity-terse}" ;;
  "SHOW log_statement") printf '%s\n' "${FALSO_log_statement-none}" ;;
  "SHOW log_min_duration_statement") printf '%s\n' "${FALSO_log_min_duration_statement--1}" ;;
  "SHOW log_parameter_max_length_on_error") printf '%s\n' "${FALSO_log_parameter_max_length_on_error-0}" ;;
  "SHOW shared_preload_libraries") printf '%s\n' "${FALSO_shared_preload_libraries-}" ;;
  "SHOW session_preload_libraries") printf '%s\n' "${FALSO_session_preload_libraries-}" ;;
  *"rolname = 'aurora_backup'"*) printf '%s\n' "${FALSO_PAPEL_BACKUP-false false false false false true false true}" ;;
  *pg_roles*) printf '%s\n' "${FALSO_PAPEL-false false false false false}" ;;
  *pg_database*) printf '%s\n' "${FALSO_DONO-aurora}" ;;
  *) echo "psql de mentira: consulta inesperada" >&2; exit 98 ;;
esac
`

func TestPostgres_ConferirTudoCerto(t *testing.T) {
	a := novoAmbienteBash(t)
	a.falso("docker", dockerDoPostgres)
	r := a.rodar(conferirPostgre, nil)
	if r.codigo != 0 {
		t.Fatalf("deveria passar: %d %q", r.codigo, r.tudo())
	}
	exigirContem(t, "a saída", r.saida,
		"OK: log_error_verbosity = terse", "OK: log_statement = none",
		"OK: log_min_duration_statement = -1", "OK: log_parameter_max_length_on_error = 0",
		"OK: shared_preload_libraries sem o auto_explain", "OK: session_preload_libraries sem o auto_explain",
		"OK: o papel aurora nao e superusuario", "OK: o banco aurora e do papel aurora",
		"OK: o papel aurora_backup so le (pg_read_all_data) e entra no banco aurora",
		"OK: os cinco ajustes de log")
	if strings.Contains(r.tudo(), "FALHOU") {
		t.Errorf("FALHOU com tudo certo: %q", r.tudo())
	}
	// Só lê: toda consulta é SHOW ou SELECT.
	for _, c := range a.chamadas() {
		if strings.Contains(c, "[-c]") && !regexp.MustCompile(`\[-c\] \[(SHOW|SELECT) `).MatchString(c) {
			t.Errorf("consulta que não é SHOW nem SELECT: %s", c)
		}
	}
}

func TestPostgres_ConferirAcusaCadaDesvio(t *testing.T) {
	for _, c := range []struct {
		env, valor, quero string
	}{
		{"FALSO_log_error_verbosity", "default", "FALHOU: log_error_verbosity = default (o esperado e terse)"},
		{"FALSO_log_statement", "ddl", "FALHOU: log_statement = ddl (o esperado e none)"},
		{"FALSO_log_min_duration_statement", "0", "FALHOU: log_min_duration_statement = 0 (o esperado e -1)"},
		{"FALSO_log_parameter_max_length_on_error", "-1", "FALHOU: log_parameter_max_length_on_error = -1"},
		{"FALSO_shared_preload_libraries", "pg_stat_statements,auto_explain", "FALHOU: shared_preload_libraries carrega o auto_explain"},
		{"FALSO_session_preload_libraries", "auto_explain", "FALHOU: session_preload_libraries carrega o auto_explain"},
		{"FALSO_PAPEL", "true false false false false", "FALHOU: o papel aurora tem poder demais"},
		{"FALSO_PAPEL", "false true false false false", "FALHOU: o papel aurora tem poder demais"},
		{"FALSO_PAPEL", "false false true false false", "FALHOU: o papel aurora tem poder demais"},
		{"FALSO_PAPEL", "false false false true false", "FALHOU: o papel aurora tem poder demais"},
		{"FALSO_PAPEL", "false false false false true", "FALHOU: o papel aurora tem poder demais"},
		{"FALSO_PAPEL", "", "FALHOU: o papel aurora nao existe"},
		{"FALSO_PAPEL_BACKUP", "", "FALHOU: o papel aurora_backup nao existe"},
		{"FALSO_PAPEL_BACKUP", "true false false false false true false true", "FALHOU: o papel aurora_backup nao esta como deveria"},
		{"FALSO_PAPEL_BACKUP", "false false false false true true false true", "FALHOU: o papel aurora_backup nao esta como deveria"},
		{"FALSO_PAPEL_BACKUP", "false false false false false false false true", "FALHOU: o papel aurora_backup nao esta como deveria"},
		{"FALSO_PAPEL_BACKUP", "false false false false false true true true", "FALHOU: o papel aurora_backup nao esta como deveria"},
		{"FALSO_PAPEL_BACKUP", "false false false false false true false false", "FALHOU: o papel aurora_backup nao esta como deveria"},
		// O banco não existe: a entrada vem nula e some do concat_ws.
		{"FALSO_PAPEL_BACKUP", "false false false false false true false", "FALHOU: o papel aurora_backup nao esta como deveria"},
		{"FALSO_DONO", "postgres", "FALHOU: o banco aurora e de postgres"},
		{"FALSO_DONO", "", "FALHOU: o banco aurora nao existe"},
	} {
		t.Run(c.env+"="+c.valor, func(t *testing.T) {
			a := novoAmbienteBash(t)
			a.falso("docker", dockerDoPostgres)
			a.env[c.env] = c.valor
			r := a.rodar(conferirPostgre, nil)
			if r.codigo != 1 {
				t.Errorf("deveria sair com 1, saiu com %d", r.codigo)
			}
			if !strings.Contains(r.saida, c.quero) {
				t.Errorf("quero %q na saída:\n%s", c.quero, r.saida)
			}
			if !strings.Contains(r.saida, "FALHOU: 1 item(ns) fora do esperado") {
				t.Errorf("faltou o resumo com 1 item:\n%s", r.saida)
			}
		})
	}
}

func TestPostgres_ConferirSemOConteiner(t *testing.T) {
	a := novoAmbienteBash(t)
	a.falso("docker", dockerDoPostgres)
	a.env["FALSO_PG_FORA"] = "1"
	r := a.rodar(conferirPostgre, nil)
	if r.codigo != 1 || !strings.Contains(r.saida, "FALHOU: o conteiner aurora-whisper-postgres nao responde") {
		t.Fatalf("deveria falhar dizendo que o conteiner não responde: %d %q", r.codigo, r.tudo())
	}
	// Para ali: nenhuma consulta depois de saber que o Postgres não responde.
	for _, c := range a.chamadas() {
		if strings.Contains(c, "[-c]") {
			t.Errorf("consultou com o Postgres fora: %s", c)
		}
	}
}
