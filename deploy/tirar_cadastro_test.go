package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O tirar-cadastro.sh (docs/deploy-privacidade.md): a política promete tirar o
// cadastro (a linha em users) a pedido, e este é o comando do responsável. Aqui
// o bash roda com um docker/psql de mentira (a lógica: recusas, SIM, o e-mail
// que vai por -v e nunca no texto do SQL); o SQL de verdade roda no Postgres de
// teste, em tirar_cadastro_pg_test.go.

const (
	idDeTeste    = "0b5f2a4e-7d1c-4e0a-9a53-6c1f0d2b8e91"
	linhaAchada  = idDeTeste + "|pessoa@exemplo.com|2026-05-01|Fulana de Tal"
	restosLimpos = "public.devices|0\npublic.transcriptions|0\npublic.notes|0\npublic.user_sync_counters|1\npublic.web_sessions|2\n"
)

// dockerDoTirarCadastro é o docker de mentira: responde ao info, ao pg_isready,
// ao inspect (o ambiente da API) e ao psql, que escolhe a resposta pelo texto do
// SQL que chega na entrada. O SQL e os -v ficam em FALSO_SQL_LOG.
const dockerDoTirarCadastro = `
case "$1" in
  info) [ -z "${FALSO_SEM_DOCKER:-}" ]; exit $? ;;
  inspect) printf 'PATH=/usr/bin\nCONTAS_PERMITIDAS=%s\n' "${FALSO_CONTAS_API:-}"; exit 0 ;;
  exec)
    shift
    [ "$1" = "-i" ] && shift
    shift
    case "$1" in
      pg_isready) exit 0 ;;
      psql)
        sql="$(cat)"
        { printf 'ARGS:%s\n' "$*"; printf '%s\n' "$sql"; printf '%s\n' '----'; } >> "$FALSO_SQL_LOG"
        case "$sql" in
          *"DELETE FROM users"*)
            [ -z "${FALSO_DELETE_FALHA:-}" ] || exit 1
            : > "$FALSO_APAGADO"; exit 0 ;;
          *pg_constraint*) cat "$FALSO_RESTOS"; exit 0 ;;
          *"FROM users"*) [ -e "$FALSO_APAGADO" ] || cat "$FALSO_ACHADO"; exit 0 ;;
        esac
        exit 0 ;;
    esac ;;
esac
exit 0
`

type cadastroDeTeste struct {
	a      *ambienteBash
	base   string
	sqlLog string
}

func novoCadastro(t *testing.T) *cadastroDeTeste {
	t.Helper()
	a := novoAmbienteBash(t)
	c := &cadastroDeTeste{a: a, base: filepath.Join(a.dir, "vps", "aurora-whisper"), sqlLog: filepath.Join(a.dir, "sql-log.txt")}
	if err := os.MkdirAll(c.base, 0o755); err != nil {
		t.Fatal(err)
	}
	c.config("CONTAS_PERMITIDAS=outra@exemplo.com,mais.uma@exemplo.com\nDOMINIO=aurora.exemplo.com.br\n")
	c.achado(linhaAchada + "\n")
	c.restos(restosLimpos)
	if err := os.WriteFile(c.sqlLog, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.env["AW_BASE"] = posix(c.base)
	a.env["FALSO_SQL_LOG"] = posix(c.sqlLog)
	a.env["FALSO_APAGADO"] = posix(filepath.Join(a.dir, "apagado"))
	a.falso("docker", dockerDoTirarCadastro)
	return c
}

func (c *cadastroDeTeste) config(texto string) {
	c.a.t.Helper()
	if err := os.WriteFile(filepath.Join(c.base, "config.env"), []byte(texto), 0o600); err != nil {
		c.a.t.Fatal(err)
	}
}

func (c *cadastroDeTeste) achado(texto string) {
	c.a.t.Helper()
	p := filepath.Join(c.a.dir, "achado.txt")
	if err := os.WriteFile(p, []byte(texto), 0o644); err != nil {
		c.a.t.Fatal(err)
	}
	c.a.env["FALSO_ACHADO"] = posix(p)
}

func (c *cadastroDeTeste) restos(texto string) {
	c.a.t.Helper()
	p := filepath.Join(c.a.dir, "restos.txt")
	if err := os.WriteFile(p, []byte(texto), 0o644); err != nil {
		c.a.t.Fatal(err)
	}
	c.a.env["FALSO_RESTOS"] = posix(p)
}

func (c *cadastroDeTeste) rodar(entrada string, args ...string) resultado {
	return c.a.rodar(tirarCadastro, []byte(entrada), args...)
}

func (c *cadastroDeTeste) log() string {
	data, err := os.ReadFile(c.sqlLog)
	if err != nil {
		c.a.t.Fatal(err)
	}
	return string(data)
}

func (c *cadastroDeTeste) apagou() bool {
	_, err := os.Stat(filepath.Join(c.a.dir, "apagado"))
	return err == nil
}

func TestTirarCadastro_NoTexto(t *testing.T) {
	c := codigo(ler(t, tirarCadastro))
	exigirContem(t, tirarCadastro, c,
		// o e-mail e o id vão ao psql como variável, e o SQL os lê com :'nome'
		`-v "email=$email"`, `-v "uid=$uid"`, `lower(:'email')`, `= :'uid'`,
		// as tabelas que apontam para users saem do próprio banco
		"pg_constraint", "c.confrelid = 'public.users'::regclass",
		// o que o "Apagar meus dados" deixa, e só isso, sai com o cadastro
		"TABELAS_QUE_SOBRAM='user_sync_counters web_sessions'",
		"DELETE FROM web_sessions", "DELETE FROM user_sync_counters", "DELETE FROM users",
		// numa transação só, depois do SIM
		"-1 -v", `[ "$resposta" = SIM ]`,
		// recusa se o e-mail ainda pode entrar
		"CONTAS_PERMITIDAS", "docker inspect",
	)
	// O e-mail nunca entra no texto do SQL nem numa linha de comando de outro
	// programa: só como valor do -v.
	for _, l := range linhasDeCodigo(ler(t, tirarCadastro)) {
		if strings.Contains(l.texto, "$email") && !strings.Contains(l.texto, `-v "email=$email"`) &&
			!strings.HasPrefix(l.texto, "email=") && !strings.HasPrefix(l.texto, "[[") &&
			!strings.HasPrefix(l.texto, "[ ") && !strings.HasPrefix(l.texto, "printf ") &&
			!strings.Contains(l.texto, `[ "$item" = "$email" ]`) && !strings.HasPrefix(l.texto, "falhou ") && !strings.HasPrefix(l.texto, "na_lista()") {
			t.Errorf("linha %d usa o e-mail fora do -v: %s", l.n, l.texto)
		}
	}
}

func TestTirarCadastro_TiraOCadastroDepoisDoSIM(t *testing.T) {
	c := novoCadastro(t)
	r := c.rodar("SIM\n", "Pessoa@Exemplo.com")
	if r.codigo != 0 {
		t.Fatalf("falhou (%d): %s", r.codigo, r.tudo())
	}
	if !c.apagou() {
		t.Errorf("deveria ter apagado: %s", r.tudo())
	}
	for _, quer := range []string{"pessoa@exemplo.com", "Fulana de Tal", "2026-05-01", "OK: o cadastro saiu do banco", "30 dias"} {
		if !strings.Contains(r.saida, quer) {
			t.Errorf("a saída deveria conter %q:\n%s", quer, r.saida)
		}
	}
	log := c.log()
	// o e-mail (em minúsculas) e o id vão por -v; nunca dentro do SQL
	for _, quer := range []string{"email=pessoa@exemplo.com", "uid=" + idDeTeste, "-1"} {
		if !strings.Contains(log, quer) {
			t.Errorf("o psql deveria receber %q:\n%s", quer, log)
		}
	}
	for _, sql := range strings.Split(log, "----") {
		var corpo []string
		for _, l := range strings.Split(sql, "\n") {
			if !strings.HasPrefix(l, "ARGS:") {
				corpo = append(corpo, l)
			}
		}
		texto := strings.Join(corpo, "\n")
		if strings.Contains(texto, "pessoa@exemplo.com") || strings.Contains(texto, idDeTeste) {
			t.Errorf("o e-mail ou o id foi parar dentro do texto do SQL:\n%s", sql)
		}
	}
}

func TestTirarCadastro_SemOSIMNadaMuda(t *testing.T) {
	for _, entrada := range []string{"", "nao\n", "sim\n", "SIM, tire\n"} {
		c := novoCadastro(t)
		r := c.rodar(entrada, "pessoa@exemplo.com")
		if r.codigo == 0 || c.apagou() {
			t.Errorf("entrada %q: deveria parar sem apagar (codigo %d): %s", entrada, r.codigo, r.tudo())
		}
		if !strings.Contains(r.erro, "PAROU") {
			t.Errorf("entrada %q: deveria dizer PAROU: %s", entrada, r.erro)
		}
	}
}

func TestTirarCadastro_RecusaEmailAindaPermitido(t *testing.T) {
	cenarios := map[string]func(c *cadastroDeTeste){
		"no config.env": func(c *cadastroDeTeste) {
			c.config("CONTAS_PERMITIDAS=outra@exemplo.com, Pessoa@Exemplo.com\n")
		},
		"na API que está de pé": func(c *cadastroDeTeste) {
			c.a.env["FALSO_CONTAS_API"] = "outra@exemplo.com,PESSOA@exemplo.com"
		},
	}
	for nome, prepara := range cenarios {
		t.Run(nome, func(t *testing.T) {
			c := novoCadastro(t)
			prepara(c)
			r := c.rodar("SIM\n", "pessoa@exemplo.com")
			if r.codigo == 0 || c.apagou() {
				t.Fatalf("deveria recusar sem apagar (codigo %d): %s", r.codigo, r.tudo())
			}
			if !strings.Contains(r.erro, "FALHOU") || !strings.Contains(r.erro, "CONTAS_PERMITIDAS") {
				t.Errorf("deveria apontar o CONTAS_PERMITIDAS: %s", r.erro)
			}
			if strings.Contains(c.log(), "DELETE") {
				t.Errorf("nenhum DELETE deveria ter sido enviado:\n%s", c.log())
			}
		})
	}
}

func TestTirarCadastro_RecusaQuemAindaTemDados(t *testing.T) {
	c := novoCadastro(t)
	c.restos("public.devices|3\npublic.notes|0\npublic.note_images|12\npublic.user_sync_counters|1\npublic.web_sessions|1\n")
	r := c.rodar("SIM\n", "pessoa@exemplo.com")
	if r.codigo == 0 || c.apagou() {
		t.Fatalf("deveria recusar sem apagar (codigo %d): %s", r.codigo, r.tudo())
	}
	for _, quer := range []string{"devices: 3 linha(s)", "note_images: 12 linha(s)", "Apagar meus dados"} {
		if !strings.Contains(r.erro, quer) {
			t.Errorf("deveria dizer %q: %s", quer, r.erro)
		}
	}
	if strings.Contains(r.erro, "notes:") || strings.Contains(r.erro, "web_sessions:") {
		t.Errorf("não deveria listar tabela vazia nem as que sobram de propósito: %s", r.erro)
	}
	if strings.Contains(c.log(), "DELETE") {
		t.Errorf("nenhum DELETE deveria ter sido enviado:\n%s", c.log())
	}
}

func TestTirarCadastro_Recusas(t *testing.T) {
	casos := []struct {
		nome   string
		args   []string
		ajeita func(c *cadastroDeTeste)
		quer   string
	}{
		{"sem argumento", nil, nil, "use:"},
		{"dois argumentos", []string{"a@exemplo.com", "b@exemplo.com"}, nil, "use:"},
		{"não é e-mail", []string{"fulana"}, nil, "e-mail"},
		{"aspas no e-mail", []string{"x'; DROP TABLE users; --@exemplo.com"}, nil, "e-mail"},
		{"espaço no e-mail", []string{"a b@exemplo.com"}, nil, "e-mail"},
		{"sem Docker", []string{"pessoa@exemplo.com"}, func(c *cadastroDeTeste) { c.a.env["FALSO_SEM_DOCKER"] = "1" }, "Docker"},
		{"sem cadastro", []string{"pessoa@exemplo.com"}, func(c *cadastroDeTeste) { c.achado("") }, "nao ha cadastro"},
		{"dois cadastros", []string{"pessoa@exemplo.com"}, func(c *cadastroDeTeste) { c.achado(linhaAchada + "\n" + linhaAchada + "\n") }, "mais de um"},
		{"id que não é UUID", []string{"pessoa@exemplo.com"}, func(c *cadastroDeTeste) { c.achado("1; DROP|pessoa@exemplo.com|2026-05-01|x\n") }, "UUID"},
		{"banco que não é o da nuvem", []string{"pessoa@exemplo.com"}, func(c *cadastroDeTeste) { c.restos("") }, "nenhuma tabela"},
		{"contagem que não é número", []string{"pessoa@exemplo.com"}, func(c *cadastroDeTeste) { c.restos("public.devices|x\n") }, "numero"},
	}
	for _, k := range casos {
		t.Run(k.nome, func(t *testing.T) {
			c := novoCadastro(t)
			if k.ajeita != nil {
				k.ajeita(c)
			}
			r := c.rodar("SIM\n", k.args...)
			if r.codigo == 0 || c.apagou() {
				t.Fatalf("deveria falhar sem apagar (codigo %d): %s", r.codigo, r.tudo())
			}
			if !strings.Contains(r.erro, "FALHOU") || !strings.Contains(r.erro, k.quer) {
				t.Errorf("deveria dizer FALHOU e %q: %s", k.quer, r.erro)
			}
		})
	}
}

func TestTirarCadastro_BancoQueRecusaNaoDizOK(t *testing.T) {
	c := novoCadastro(t)
	c.a.env["FALSO_DELETE_FALHA"] = "1"
	r := c.rodar("SIM\n", "pessoa@exemplo.com")
	if r.codigo == 0 || c.apagou() {
		t.Fatalf("deveria falhar (codigo %d): %s", r.codigo, r.tudo())
	}
	if strings.Contains(r.saida, "o cadastro saiu do banco") || !strings.Contains(r.erro, "nada foi tirado") {
		t.Errorf("não deveria dizer que tirou:\n%s\n%s", r.saida, r.erro)
	}
}
