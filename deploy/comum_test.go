// Package deploy guarda só testes: os que provam, sem rodar Docker, ssh nem
// nada de verdade, as promessas dos arquivos de deploy da nuvem (ADR-019,
// "Decisões da F11"): a imagem do servidor e o .dockerignore (F11e), o init e
// a conferência do Postgres (F11e), os scripts da VPS e a imagem do backup
// (F11f), os scripts do PC (F11g) e a varredura de segredos que faz as vezes
// do gitleaks enquanto não há push (F11h).
//
// Como na prova de scripts/postgres-de-teste.ps1
// (internal/server/pgtest/script_test.go), a maior parte dos testes LÊ o texto
// dos arquivos e confere o que ele promete. Os scripts em bash também rodam de
// verdade, mas com programas de mentira (docker, psql, pg_dump, age, rclone...)
// num PATH só deles, numa pasta temporária e sem rede (bash_test.go): nenhum
// teste chama o Docker, o ssh, o B2 nem o servidor.
package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// raiz é a raiz do repositório, vista da pasta deste pacote (deploy/).
const raiz = ".."

// caminho junta partes relativas à raiz do repositório.
func caminho(partes ...string) string {
	return filepath.Join(append([]string{raiz}, partes...)...)
}

// caminhoAbsoluto devolve o caminho absoluto de um arquivo do repositório
// (relativo à raiz, com barras).
func caminhoAbsoluto(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(caminho(filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// ler devolve o texto de um arquivo do repositório (caminho relativo à raiz,
// com barras).
func ler(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(caminho(filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ler %s: %v", rel, err)
	}
	return string(data)
}

// exigirASCII falha no primeiro caractere fora do ASCII. O PowerShell 5.1 lê o
// .ps1 na página de código do sistema, e os scripts da VPS rodam em qualquer
// locale.
func exigirASCII(t *testing.T, nome, texto string) {
	t.Helper()
	for i, r := range texto {
		if r > 127 {
			t.Fatalf("%s, byte %d: caractere não ASCII %q", nome, i, r)
		}
	}
}

// exigirLF: os scripts do bash têm de ter fim de linha LF (um \r no fim da
// linha quebra o bash). O .gitattributes garante no checkout; isto pega o
// arquivo escrito com CRLF antes do commit.
func exigirLF(t *testing.T, nome, texto string) {
	t.Helper()
	if strings.Contains(texto, "\r") {
		t.Errorf("%s tem \\r: os scripts do bash são LF", nome)
	}
	if !strings.HasSuffix(texto, "\n") {
		t.Errorf("%s não termina com fim de linha", nome)
	}
}

// exigirCRLF: os .ps1 são CRLF (.gitattributes), como os outros scripts do PC.
func exigirCRLF(t *testing.T, nome, texto string) {
	t.Helper()
	sem := strings.ReplaceAll(texto, "\r\n", "")
	if strings.Contains(sem, "\n") || strings.Contains(sem, "\r") {
		t.Errorf("%s tem fim de linha que não é CRLF", nome)
	}
}

// linhasDeCodigo devolve as linhas que não são comentário (no bash e no
// PowerShell, # até o fim da linha quando a linha começa com #), já sem os
// espaços das pontas, com o número da linha. Os blocos <# ... #> do PowerShell
// também ficam de fora.
func linhasDeCodigo(texto string) []linha {
	var out []linha
	emBloco := false
	for i, l := range strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(l)
		if emBloco {
			if strings.Contains(s, "#>") {
				emBloco = false
			}
			continue
		}
		if strings.HasPrefix(s, "<#") {
			emBloco = !strings.Contains(s, "#>")
			continue
		}
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, linha{i + 1, s})
	}
	return out
}

type linha struct {
	n     int
	texto string
}

// codigo é o texto sem as linhas de comentário, para procurar o que o script
// FAZ sem tropeçar no que os comentários explicam.
func codigo(texto string) string {
	var b strings.Builder
	for _, l := range linhasDeCodigo(texto) {
		b.WriteString(l.texto)
		b.WriteByte('\n')
	}
	return b.String()
}

// exigirContem confere que cada trecho aparece no texto.
func exigirContem(t *testing.T, nome, texto string, trechos ...string) {
	t.Helper()
	for _, s := range trechos {
		if !strings.Contains(texto, s) {
			t.Errorf("%s deveria conter %q", nome, s)
		}
	}
}

// exigirNaoContem confere que nenhum trecho aparece no texto.
func exigirNaoContem(t *testing.T, nome, texto string, trechos ...string) {
	t.Helper()
	for _, s := range trechos {
		if strings.Contains(texto, s) {
			t.Errorf("%s não pode conter %q", nome, s)
		}
	}
}

// exigirCasa confere que a expressão casa com o texto.
func exigirCasa(t *testing.T, nome, texto, expr string) {
	t.Helper()
	if !regexp.MustCompile(expr).MatchString(texto) {
		t.Errorf("%s deveria casar com %s", nome, expr)
	}
}
