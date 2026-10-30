package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
)

// A política de privacidade da instância (POLITICA_FILE): opcional. Sem a
// variável, o servidor sobe como sempre e a rota /privacidade não existe; com
// ela, o arquivo é conferido na subida e, se não presta, a subida falha (saída
// 2) com o nome da variável e o problema, nunca o caminho nem o conteúdo.

const marcaDoConteudo = "CONTEUDO-DA-POLITICA-DE-TESTE"

func arquivoDaPolitica(t *testing.T, conteudo []byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "PASTA-DA-INSTANCIA")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "politica-de-privacidade.md")
	if err := os.WriteFile(path, conteudo, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_SemAPoliticaNadaMuda(t *testing.T) {
	env := envBom(t)
	cfg, err := loadConfig(func(k string) string { return env[k] })
	if err != nil || cfg.politica != nil {
		t.Fatalf("sem POLITICA_FILE: política %v, %v; quero nil sem erro", cfg.politica, err)
	}
}

func TestLoadConfig_APoliticaDoArquivo(t *testing.T) {
	for nome, conteudo := range map[string]string{
		"o arquivo":       "# Política\n\n" + marcaDoConteudo + "\n",
		"com BOM e CRLF":  "\xEF\xBB\xBF# Política\r\n\r\n" + marcaDoConteudo + "\r\n",
		"sem o fim":       "# Política\n\n" + marcaDoConteudo,
		"no limite exato": marcaDoConteudo + strings.Repeat("a", server.MaxPolitica-len(marcaDoConteudo)),
	} {
		env := with(envBom(t), envPoliticaFile, arquivoDaPolitica(t, []byte(conteudo)))
		cfg, err := loadConfig(func(k string) string { return env[k] })
		if err != nil || cfg.politica == nil {
			t.Errorf("%s: %v, política %v; quero a política carregada", nome, err, cfg.politica)
		}
	}
}

// Qualquer problema no arquivo impede a subida, e a mensagem não traz o
// caminho nem o conteúdo.
func TestServe_APoliticaQueNaoPrestaNaoSobe(t *testing.T) {
	cases := []struct {
		nome     string
		conteudo []byte
		quero    string
	}{
		{"vazio", nil, "o arquivo está vazio"},
		{"só espaços", []byte(" \n\n\t\n"), "o arquivo está vazio"},
		{"passa do limite", []byte(marcaDoConteudo + strings.Repeat("a", server.MaxPolitica)), "o arquivo passa de 96 KiB"},
		{"Latin-1", []byte(marcaDoConteudo + " a\xe7\xe3o\n"), "o arquivo não é um texto UTF-8 válido"},
		{"binário", []byte(marcaDoConteudo + "\x00\x01\x02\n"), "o arquivo tem caracteres de controle"},
		{"o modelo sem preencher", []byte(marcaDoConteudo + "\n**[A PREENCHER: nome do responsável]**\n"), "o arquivo ainda tem campos [A PREENCHER...] do modelo"},
		{"o modelo, campo \"se for o caso\"", []byte(marcaDoConteudo + "\n**[A PREENCHER, se for o caso: nome do responsável]**\n"), "o arquivo ainda tem campos [A PREENCHER...] do modelo"},
		{"a nota do topo do modelo", []byte(marcaDoConteudo + "\nPreencha os campos **[A PREENCHER]**.\n"), "o arquivo ainda tem campos [A PREENCHER...] do modelo"},
	}
	for _, c := range cases {
		env := with(envBom(t), envPoliticaFile, arquivoDaPolitica(t, c.conteudo))
		out, errOut, code := runCmd(t, env, "serve")
		if code != 2 || !strings.HasPrefix(errOut, "serve: "+envPoliticaFile+": "+c.quero) {
			t.Errorf("%s: %d %q; quero 2 e a mensagem de %s (%s)", c.nome, code, errOut, envPoliticaFile, c.quero)
			continue
		}
		tudo := out + errOut
		for _, vaza := range []string{marcaDoConteudo, "PASTA-DA-INSTANCIA", "politica-de-privacidade.md", "nome do responsável", testSessionSecret, testPepper} {
			if strings.Contains(tudo, vaza) {
				t.Errorf("%s: a saída mostra %q: %q", c.nome, vaza, errOut)
			}
		}
	}

	t.Run("é uma pasta", func(t *testing.T) {
		env := with(envBom(t), envPoliticaFile, t.TempDir())
		_, errOut, code := runCmd(t, env, "serve")
		if code != 2 || !strings.HasPrefix(errOut, "serve: "+envPoliticaFile+": ") {
			t.Errorf("%d %q", code, errOut)
		}
	})
}

// O arquivo que não existe não é erro: o compose sempre define POLITICA_FILE, e
// quem ainda não tem política sobe sem a página (o serve avisa no log; o
// TestServe_Postgres_PoliticaAusenteSobeSemARota confere a subida inteira).
func TestLoadConfig_PoliticaAusenteNaoEErro(t *testing.T) {
	for nome, caminho := range map[string]string{
		"o arquivo":    filepath.Join(t.TempDir(), "politica-de-privacidade.md"),
		"a pasta toda": filepath.Join(t.TempDir(), "nao-existe", "politica-de-privacidade.md"),
	} {
		env := with(envBom(t), envPoliticaFile, caminho)
		cfg, err := loadConfig(func(k string) string { return env[k] })
		if err != nil || cfg.politica != nil || !cfg.politicaAusente {
			t.Errorf("%s: %v, política %v, ausente %v; quero nil, sem erro, e ausente", nome, err, cfg.politica, cfg.politicaAusente)
		}
	}
	env := envBom(t)
	if cfg, err := loadConfig(func(k string) string { return env[k] }); err != nil || cfg.politicaAusente {
		t.Errorf("sem a variável: %v, ausente %v; quero que não conte como ausente", err, cfg.politicaAusente)
	}
}
