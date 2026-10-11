package main

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgmigrate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/pgtest"
	"github.com/MauroJulianiJunior/aurora-whisper/migrations"
)

// Testes do comando "migrate" CONTRA UM POSTGRES DE VERDADE. Pulam sem
// AURORA_TEST_POSTGRES_URL (pgtest.NewDB); veja scripts/README.md.

func TestMigrate_Postgres_DoZeroDeNovoEStatus(t *testing.T) {
	dbURL := pgtest.NewDB(t)
	env := map[string]string{envDatabaseURL: dbURL}
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()

	migs, err := pgmigrate.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	total := len(migs)

	// 1. -status num banco vazio: tudo pendente, e nada é escrito.
	out, errOut, code := runCmd(t, env, "migrate", "-status")
	if code != 0 || errOut != "" {
		t.Fatalf("-status: código %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "pendente  "+migs[0].File) ||
		!strings.Contains(out, fmt.Sprintf("migrate: 0 aplicadas, %d pendentes", total)) {
		t.Errorf("-status num banco vazio:\n%s", out)
	}

	// 2. migrate do zero aplica tudo, na ordem.
	out, errOut, code = runCmd(t, env, "migrate")
	if code != 0 || errOut != "" {
		t.Fatalf("migrate: código %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "migrate: aplicada "+migs[0].File) ||
		!strings.Contains(out, "migrate: aplicada "+migs[total-1].File) ||
		!strings.Contains(out, fmt.Sprintf("migrate: %d aplicadas agora, 0 já estavam aplicadas", total)) {
		t.Errorf("migrate do zero:\n%s", out)
	}
	if strings.Index(out, migs[0].File) > strings.Index(out, migs[total-1].File) {
		t.Errorf("as migrations não foram aplicadas na ordem:\n%s", out)
	}

	// 3. de novo: não faz nada.
	out, errOut, code = runCmd(t, env, "migrate")
	if code != 0 || errOut != "" {
		t.Fatalf("migrate de novo: código %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, fmt.Sprintf("migrate: nada a aplicar (%d já estavam aplicadas)", total)) {
		t.Errorf("migrate de novo:\n%s", out)
	}

	// 4. -status: tudo aplicado.
	out, errOut, code = runCmd(t, env, "migrate", "-status")
	if code != 0 || errOut != "" {
		t.Fatalf("-status depois: código %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, fmt.Sprintf("migrate: %d aplicadas, 0 pendentes", total)) || strings.Contains(out, "pendente  ") {
		t.Errorf("-status depois do migrate:\n%s", out)
	}

	// Nada do que o comando imprimiu pode trazer a senha nem a URL.
	if password != "" && strings.Contains(out+errOut, password) {
		t.Error("a saída do comando traz a senha do banco")
	}
}

func TestMigrate_Postgres_SenhaErradaNaoVazaASenha(t *testing.T) {
	dbURL := pgtest.NewDB(t)
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	const wrong = "SENHA_ERRADA_QUE_NAO_PODE_APARECER"
	u.User = url.UserPassword(u.User.Username(), wrong)

	out, errOut, code := runCmd(t, map[string]string{envDatabaseURL: u.String()}, "migrate")
	if code == 0 {
		t.Skip("o Postgres de teste aceitou uma senha errada (autenticação trust): nada a provar aqui")
	}
	if code != 1 {
		t.Errorf("código %d, quero 1", code)
	}
	if strings.Contains(out+errOut, wrong) {
		t.Errorf("a saída traz a senha: stdout %q, stderr %q", out, errOut)
	}
	if !strings.Contains(errOut, "não consegui conectar") {
		t.Errorf("stderr não diz que a conexão falhou: %q", errOut)
	}
}
