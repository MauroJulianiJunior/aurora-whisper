package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Os segredos por arquivo (F11c; ADR-019, "Decisões da F11"):
// DATABASE_URL_FILE, SESSION_SECRET_FILE e DEVICE_TOKEN_PEPPER_FILE, com a
// variável comum ainda valendo e as duas juntas dando erro. Nenhuma mensagem
// traz o valor nem o caminho.

// arquivoDeSegredo grava conteudo num arquivo de uma pasta temporária cujo
// nome é reconhecível (para o teste conferir que o caminho não vaza).
func arquivoDeSegredo(t *testing.T, conteudo string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "PASTA-SECRETA-DOS-SEGREDOS")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "segredo")
	if err := os.WriteFile(path, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// lerDe lê um segredo com um ambiente falso.
func lerDe(env map[string]string, nome string) (string, error) {
	return lerSegredo(func(k string) string { return env[k] }, nome)
}

func TestLerSegredo_DoArquivo(t *testing.T) {
	const valor = "VALOR-DO-SEGREDO-0123456789abcdefghij"
	for nome, conteudo := range map[string]string{
		"sem fim de linha":  valor,
		"com \\n no fim":    valor + "\n",
		"com \\r\\n no fim": valor + "\r\n",
	} {
		got, err := lerDe(map[string]string{"X_FILE": arquivoDeSegredo(t, conteudo)}, "X")
		if err != nil || got != valor {
			t.Errorf("%s: %q, %v; quero o valor sem o fim de linha", nome, got, err)
		}
	}
	// Só UM fim de linha sai, e o espaço é do valor (o arquivo é a fonte).
	if got, err := lerDe(map[string]string{"X_FILE": arquivoDeSegredo(t, " "+valor+" \n")}, "X"); err != nil || got != " "+valor+" " {
		t.Errorf("com espaços: %q, %v", got, err)
	}
	// A variável comum continua valendo, e sem nenhuma das duas é "" sem erro.
	if got, err := lerDe(map[string]string{"X": valor}, "X"); err != nil || got != valor {
		t.Errorf("a variável comum: %q, %v", got, err)
	}
	if got, err := lerDe(map[string]string{}, "X"); err != nil || got != "" {
		t.Errorf("sem nada: %q, %v", got, err)
	}
}

func TestLerSegredo_Erros(t *testing.T) {
	const valor = "VALOR-DO-SEGREDO-0123456789abcdefghij"
	dirComoArquivo := filepath.Join(t.TempDir(), "PASTA-SECRETA-DOS-SEGREDOS")
	if err := os.MkdirAll(dirComoArquivo, 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		nome, variavel string
		env            map[string]string
	}{
		{"as duas juntas", "X", map[string]string{"X": valor, "X_FILE": arquivoDeSegredo(t, valor)}},
		{"arquivo que não existe", "X_FILE", map[string]string{"X_FILE": filepath.Join(t.TempDir(), "PASTA-SECRETA-DOS-SEGREDOS", "nao-existe")}},
		{"vazio", "X_FILE", map[string]string{"X_FILE": arquivoDeSegredo(t, "")}},
		{"só o fim de linha", "X_FILE", map[string]string{"X_FILE": arquivoDeSegredo(t, "\n")}},
		{"só o \\r\\n", "X_FILE", map[string]string{"X_FILE": arquivoDeSegredo(t, "\r\n")}},
		{"duas linhas", "X_FILE", map[string]string{"X_FILE": arquivoDeSegredo(t, valor+"\n"+valor+"\n")}},
		{"dois fins de linha", "X_FILE", map[string]string{"X_FILE": arquivoDeSegredo(t, valor+"\n\n")}},
		{"um \\r no meio", "X_FILE", map[string]string{"X_FILE": arquivoDeSegredo(t, valor+"\r"+valor)}},
		{"grande demais", "X_FILE", map[string]string{"X_FILE": arquivoDeSegredo(t, strings.Repeat("a", maxArquivoSegredo+1))}},
		{"uma pasta", "X_FILE", map[string]string{"X_FILE": dirComoArquivo}},
	}
	for _, c := range cases {
		got, err := lerDe(c.env, "X")
		var ce *configError
		if !errors.As(err, &ce) || ce.variavel != c.variavel {
			t.Errorf("%s: %q, %v; quero o erro de configuração de %s", c.nome, got, err, c.variavel)
			continue
		}
		if got != "" {
			t.Errorf("%s: devolveu um valor com o erro", c.nome)
		}
		if msg := err.Error(); strings.Contains(msg, valor) || strings.Contains(msg, "PASTA-SECRETA") {
			t.Errorf("%s: a mensagem traz o valor ou o caminho: %q", c.nome, msg)
		}
	}
	// No teto exato, vale.
	if got, err := lerDe(map[string]string{"X_FILE": arquivoDeSegredo(t, strings.Repeat("a", maxArquivoSegredo))}, "X"); err != nil || len(got) != maxArquivoSegredo {
		t.Errorf("no teto: %d, %v", len(got), err)
	}
}

// O serve com os três segredos em arquivo: lê os três (o banco fora do ar é a
// prova de que a DATABASE_URL do arquivo valeu), e as regras de depois da
// leitura continuam (32 caracteres, diferentes). Cada erro diz o nome da
// variável, nunca o valor nem o caminho.
func TestServe_SegredosPorArquivo(t *testing.T) {
	env := envBom(t)
	porArquivo := map[string]string{}
	for k, v := range env {
		porArquivo[k] = v
	}
	for _, nome := range []string{envDatabaseURL, envSessionSecret, envDeviceTokenPep} {
		delete(porArquivo, nome)
		porArquivo[nome+sufixoArquivo] = arquivoDeSegredo(t, env[nome]+"\n")
	}
	cfg, err := loadConfig(func(k string) string { return porArquivo[k] })
	if err != nil {
		t.Fatalf("loadConfig com os três por arquivo: %v", err)
	}
	if cfg.databaseURL != env[envDatabaseURL] || string(cfg.sessionSecret) != testSessionSecret || string(cfg.pepper) != testPepper {
		t.Fatal("os valores lidos dos arquivos não são os gravados")
	}
	out, errOut, code := runCmd(t, porArquivo, "serve")
	if code != 1 || !strings.Contains(errOut, "não consegui conectar ao Postgres") {
		t.Fatalf("serve com a URL do arquivo e o banco fora do ar: %d %q", code, errOut)
	}
	if strings.Contains(out+errOut, testDBPassword) || strings.Contains(out+errOut, "PASTA-SECRETA") {
		t.Errorf("a saída traz a senha ou o caminho: %q", errOut)
	}

	cases := []struct {
		nome, variavel string
		env            map[string]string
	}{
		{"DATABASE_URL e o _FILE juntas", envDatabaseURL, with(porArquivo, envDatabaseURL, env[envDatabaseURL])},
		{"SESSION_SECRET e o _FILE juntas", envSessionSecret, with(porArquivo, envSessionSecret, testSessionSecret)},
		{"DEVICE_TOKEN_PEPPER e o _FILE juntas", envDeviceTokenPep, with(porArquivo, envDeviceTokenPep, testPepper)},
		{"DATABASE_URL_FILE vazio", envDatabaseURL + sufixoArquivo, with(porArquivo, envDatabaseURL+sufixoArquivo, arquivoDeSegredo(t, ""))},
		{"SESSION_SECRET_FILE que não existe", envSessionSecret + sufixoArquivo,
			with(porArquivo, envSessionSecret+sufixoArquivo, filepath.Join(t.TempDir(), "PASTA-SECRETA-DOS-SEGREDOS", "x"))},
		{"DEVICE_TOKEN_PEPPER_FILE com duas linhas", envDeviceTokenPep + sufixoArquivo,
			with(porArquivo, envDeviceTokenPep+sufixoArquivo, arquivoDeSegredo(t, testPepper+"\n"+testPepper))},
		// As regras de depois da leitura valem para o que veio do arquivo.
		{"SESSION_SECRET_FILE curto", envSessionSecret, with(porArquivo, envSessionSecret+sufixoArquivo, arquivoDeSegredo(t, "CURTO-DEMAIS-123\n"))},
		{"os dois arquivos iguais", envSessionSecret, with(porArquivo, envDeviceTokenPep+sufixoArquivo, arquivoDeSegredo(t, testSessionSecret))},
		{"nem a variável nem o arquivo", envSessionSecret, with(porArquivo, envSessionSecret+sufixoArquivo, "")},
		{"nem a URL nem o arquivo", envDatabaseURL, with(porArquivo, envDatabaseURL+sufixoArquivo, "")},
	}
	for _, c := range cases {
		out, errOut, code := runCmd(t, c.env, "serve")
		if code != 2 || !strings.HasPrefix(errOut, "serve: "+c.variavel+": ") {
			t.Errorf("%s: %d %q; quero 2 e a mensagem de %s", c.nome, code, errOut, c.variavel)
			continue
		}
		for _, segredo := range []string{testSessionSecret, testPepper, testDBPassword, "CURTO-DEMAIS", "PASTA-SECRETA"} {
			if strings.Contains(out+errOut, segredo) {
				t.Errorf("%s: a saída mostra %q: %q", c.nome, segredo, errOut)
			}
		}
	}
}

// O migrate lê a URL pela mesma regra: do arquivo vale (a URL inválida do
// arquivo falha ao ler a URL, com 1, e não por falta da variável, com 2), as
// duas juntas são erro de uso, e nada mostra a senha nem o caminho.
func TestMigrate_DatabaseURLPorArquivo(t *testing.T) {
	const secret = "SENHA_SECRETA_DO_ARQUIVO_456"
	urlInvalida := "postgres://aurora:" + secret + "@127.0.0.1:porta/aurora"
	env := map[string]string{envDatabaseURL + sufixoArquivo: arquivoDeSegredo(t, urlInvalida+"\n")}
	out, errOut, code := runCmd(t, env, "migrate")
	if code != 1 {
		t.Fatalf("migrate com a URL inválida no arquivo: %d %q; quero 1 (a URL do arquivo foi lida)", code, errOut)
	}
	if strings.Contains(out+errOut, secret) || strings.Contains(out+errOut, "PASTA-SECRETA") {
		t.Errorf("a saída traz a senha ou o caminho: %q", out+errOut)
	}
	juntas := map[string]string{envDatabaseURL: urlInvalida, envDatabaseURL + sufixoArquivo: env[envDatabaseURL+sufixoArquivo]}
	if out, errOut, code := runCmd(t, juntas, "migrate", "-status"); code != 2 || !strings.HasPrefix(errOut, "migrate: "+envDatabaseURL+": ") ||
		strings.Contains(out+errOut, secret) {
		t.Errorf("migrate com as duas: %d %q", code, errOut)
	}
	vazio := map[string]string{envDatabaseURL + sufixoArquivo: arquivoDeSegredo(t, "")}
	if _, errOut, code := runCmd(t, vazio, "migrate"); code != 2 || !strings.Contains(errOut, envDatabaseURL+sufixoArquivo) {
		t.Errorf("migrate com o arquivo vazio: %d %q", code, errOut)
	}
}
