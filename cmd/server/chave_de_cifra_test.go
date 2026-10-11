package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
)

// A chave de cifra das chaves da IA do celular (ADR-021, C3a):
// AI_KEYS_SECRET_FILE (ou AI_KEYS_SECRET), opcional. Sem ela, o servidor sobe
// sem as rotas do ditado no celular; com ela malformada, a subida falha com a
// mensagem do nome da variável, nunca o conteúdo nem o caminho.

// chaveDeCifraDeTeste é de mentira: 64 caracteres hexadecimais.
var chaveDeCifraDeTeste = strings.Repeat("c0ffee12", 8)

func TestLoadConfig_SemAChaveDeCifraSobeSemAsRotas(t *testing.T) {
	env := envBom(t)
	cfg, err := loadConfig(func(k string) string { return env[k] })
	if err != nil || cfg.cofre != nil {
		t.Fatalf("sem AI_KEYS_SECRET: cofre %v, %v; quero nil sem erro", cfg.cofre, err)
	}
}

func TestLoadConfig_AChaveDeCifraDoArquivo(t *testing.T) {
	quero, err := cofre.Novo(chaveDeCifraDeTeste)
	if err != nil {
		t.Fatal(err)
	}
	for nome, env := range map[string]map[string]string{
		"o arquivo":            with(envBom(t), envAIKeysSecret+sufixoArquivo, arquivoDeSegredo(t, chaveDeCifraDeTeste+"\n")),
		"o arquivo com \\r\\n": with(envBom(t), envAIKeysSecret+sufixoArquivo, arquivoDeSegredo(t, chaveDeCifraDeTeste+"\r\n")),
		"em maiúsculas":        with(envBom(t), envAIKeysSecret+sufixoArquivo, arquivoDeSegredo(t, strings.ToUpper(chaveDeCifraDeTeste))),
		"a variável comum":     with(envBom(t), envAIKeysSecret, chaveDeCifraDeTeste),
	} {
		cfg, err := loadConfig(func(k string) string { return env[k] })
		if err != nil || cfg.cofre == nil || !bytes.Equal(cfg.cofre.ID(), quero.ID()) {
			t.Errorf("%s: %v; quero o cofre desta chave de cifra", nome, err)
		}
	}
}

// O arquivo malformado impede a subida (saída 2), e a mensagem diz o nome da
// variável e o que fazer, nunca o conteúdo nem o caminho do arquivo.
func TestServe_AChaveDeCifraMalformadaNaoSobe(t *testing.T) {
	const marca = "fedcba98"
	cases := []struct {
		nome, variavel, conteudo string
		porVariavel              bool
	}{
		{"63 caracteres", envAIKeysSecret + sufixoArquivo, chaveDeCifraDeTeste[:56] + marca[:7], false},
		{"65 caracteres", envAIKeysSecret + sufixoArquivo, chaveDeCifraDeTeste + "f", false},
		{"128 caracteres", envAIKeysSecret + sufixoArquivo, chaveDeCifraDeTeste + strings.Repeat(marca, 8), false},
		{"não hexadecimal", envAIKeysSecret + sufixoArquivo, strings.Repeat("ZQ"+marca[:6], 8), false},
		{"base64", envAIKeysSecret + sufixoArquivo, "QUJD" + marca + "REVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5Kys=", false},
		{"espaço no meio", envAIKeysSecret + sufixoArquivo, chaveDeCifraDeTeste[:32] + " " + chaveDeCifraDeTeste[33:], false},
		{"duas linhas", envAIKeysSecret + sufixoArquivo, chaveDeCifraDeTeste + "\n" + chaveDeCifraDeTeste, false},
		{"vazio", envAIKeysSecret + sufixoArquivo, "", false},
		{"pela variável, curta", envAIKeysSecret, marca + marca, true},
	}
	for _, c := range cases {
		env := envBom(t)
		if c.porVariavel {
			env[envAIKeysSecret] = c.conteudo
		} else {
			env[envAIKeysSecret+sufixoArquivo] = arquivoDeSegredo(t, c.conteudo)
		}
		out, errOut, code := runCmd(t, env, "serve")
		if code != 2 || !strings.HasPrefix(errOut, "serve: "+c.variavel+": ") {
			t.Errorf("%s: %d %q; quero 2 e a mensagem de %s", c.nome, code, errOut, c.variavel)
			continue
		}
		tudo := out + errOut
		for _, segredo := range []string{marca, chaveDeCifraDeTeste[:16], "PASTA-SECRETA", testSessionSecret, testPepper} {
			if strings.Contains(tudo, segredo) {
				t.Errorf("%s: a saída mostra %q: %q", c.nome, segredo, errOut)
			}
		}
	}
	// As duas juntas, e a chave de cifra igual a um dos segredos do HMAC.
	juntas := envBom(t)
	juntas[envAIKeysSecret] = chaveDeCifraDeTeste
	juntas[envAIKeysSecret+sufixoArquivo] = arquivoDeSegredo(t, chaveDeCifraDeTeste)
	if _, errOut, code := runCmd(t, juntas, "serve"); code != 2 || !strings.HasPrefix(errOut, "serve: "+envAIKeysSecret+": ") ||
		strings.Contains(errOut, chaveDeCifraDeTeste[:16]) {
		t.Errorf("as duas juntas: %d %q", code, errOut)
	}
	for _, igual := range []string{envSessionSecret, envDeviceTokenPep} {
		env := envBom(t)
		env[igual] = strings.ToUpper(chaveDeCifraDeTeste)
		env[envAIKeysSecret+sufixoArquivo] = arquivoDeSegredo(t, chaveDeCifraDeTeste)
		_, errOut, code := runCmd(t, env, "serve")
		if code != 2 || !strings.HasPrefix(errOut, "serve: "+envAIKeysSecret+sufixoArquivo+": tem de ser diferente") ||
			strings.Contains(strings.ToLower(errOut), chaveDeCifraDeTeste[:16]) {
			t.Errorf("igual a %s: %d %q", igual, code, errOut)
		}
	}
}
