package main

import (
	"strings"
	"testing"
)

// O comando pos-restauracao sem Postgres (F11d): sem -confirmo não faz nada,
// nem conecta; o uso errado é 2; a falta da URL é 2; o banco fora do ar é 1;
// nada mostra a senha. O caminho feliz está em posrestauracao_pg_test.go.

func TestPosRestauracao_SemConfirmoNaoFazNada(t *testing.T) {
	// A URL aponta para uma porta onde ninguém escuta: se o comando tentasse
	// conectar, a mensagem seria a da conexão, e o código, 1.
	env := map[string]string{envDatabaseURL: "postgres://aurora:" + testDBPassword + "@127.0.0.1:1/aurora?connect_timeout=2"}
	for _, args := range [][]string{nil, {"-confirmo=false"}} {
		out, errOut, code := runCmd(t, env, append([]string{"pos-restauracao"}, args...)...)
		if code != 2 || out != "" || !strings.Contains(errOut, "nada foi feito") || !strings.Contains(errOut, "-confirmo") {
			t.Errorf("%v: %d %q %q; quero 2 e o aviso", args, code, out, errOut)
		}
		if strings.Contains(errOut, "não consegui") || strings.Contains(out+errOut, testDBPassword) {
			t.Errorf("%v: tentou conectar ou mostrou a senha: %q", args, errOut)
		}
	}
	// Sem -confirmo, nem a falta da URL é lida: o aviso vem antes.
	if _, errOut, code := runCmd(t, nil, "pos-restauracao"); code != 2 || !strings.Contains(errOut, "nada foi feito") {
		t.Errorf("sem nada: %d %q", code, errOut)
	}
}

func TestPosRestauracao_UsoErradoEConfiguracao(t *testing.T) {
	for nome, args := range map[string][]string{
		"argumento solto":    {"pos-restauracao", "-confirmo", "agora"},
		"opção desconhecida": {"pos-restauracao", "-confirmo", "-tudo"},
	} {
		if _, errOut, code := runCmd(t, nil, args...); code != 2 {
			t.Errorf("%s: %d %q; quero 2", nome, code, errOut)
		}
	}
	if _, errOut, code := runCmd(t, nil, "pos-restauracao", "-confirmo"); code != 2 || !strings.HasPrefix(errOut, "pos-restauracao: "+envDatabaseURL+": ") {
		t.Errorf("sem a URL: %d %q", code, errOut)
	}
	juntas := map[string]string{envDatabaseURL: "postgres://x", envDatabaseURL + sufixoArquivo: arquivoDeSegredo(t, "postgres://y")}
	if _, errOut, code := runCmd(t, juntas, "pos-restauracao", "-confirmo"); code != 2 || !strings.Contains(errOut, sufixoArquivo) {
		t.Errorf("com as duas: %d %q", code, errOut)
	}
	invalida := map[string]string{envDatabaseURL: "postgres://u:" + testDBPassword + "@127.0.0.1:porta/x"}
	if out, errOut, code := runCmd(t, invalida, "pos-restauracao", "-confirmo"); code != 2 || strings.Contains(out+errOut, testDBPassword) {
		t.Errorf("a URL inválida: %d %q", code, errOut)
	}
}

func TestPosRestauracao_BancoForaDoAr(t *testing.T) {
	env := map[string]string{envDatabaseURL + sufixoArquivo: arquivoDeSegredo(t,
		"postgres://aurora:"+testDBPassword+"@127.0.0.1:1/aurora?connect_timeout=2\n")}
	out, errOut, code := runCmd(t, env, "pos-restauracao", "-confirmo")
	if code != 1 || !strings.Contains(errOut, "não consegui") {
		t.Fatalf("banco fora do ar: %d %q; quero 1", code, errOut)
	}
	if strings.Contains(out+errOut, testDBPassword) || strings.Contains(out+errOut, "PASTA-SECRETA") || out != "" {
		t.Errorf("a saída traz a senha, o caminho, ou disse que fez: %q %q", out, errOut)
	}
}
