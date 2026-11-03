package deploy

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// As imagens de fora vão pela tag E pelo digest (03-nov-2026): o Docker usa o
// digest, e o build e o pull de amanhã baixam os mesmos bytes que o teste do PC
// provou, mesmo que a tag ande no registro. O comentário do topo do
// deploy/Dockerfile diz por que e como trocar.

// comDigest é o sufixo de uma referência fixada, para os regexes dos outros
// testes do pacote.
const comDigest = `@sha256:[0-9a-f]{64}`

// imagemFixada: nome:tag@sha256:<64 hex>. A tag fica para quem lê.
var imagemFixada = regexp.MustCompile(`^([a-z0-9][a-z0-9._/-]*):([A-Za-z0-9][A-Za-z0-9._-]*)` + comDigest + `$`)

// imagensNossas são as que o PC monta e manda por ssh (pull_policy: never,
// sem registro): não têm digest de registro.
var imagensNossas = map[string]bool{
	"aurora-whisper-server:producao": true,
	"aurora-whisper-backup:producao": true,
}

type usoDeImagem struct {
	arquivo string // relativo à raiz, com barras
	ref     string
}

// imagensDeFora varre os Dockerfiles e os YAML do deploy/ e do .github/ (os
// compose e os workflows do CI) e devolve cada imagem de registro que eles
// usam: o FROM que não é um estágio anterior nem o scratch, e o "image:".
func imagensDeFora(t *testing.T) []usoDeImagem {
	t.Helper()
	var usos []usoDeImagem
	imagemDoYAML := regexp.MustCompile(`(?m)^\s*(?:-\s+)?image:\s*["']?([^"'\s#]+)`)
	for _, pasta := range []string{"deploy", ".github"} {
		err := filepath.WalkDir(caminho(pasta), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(raiz, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			nome := d.Name()
			switch {
			case nome == "Dockerfile" || strings.HasPrefix(nome, "Dockerfile.") && !strings.HasSuffix(nome, ".dockerignore"):
				estagiosAntes := map[string]bool{"scratch": true}
				for _, i := range instrucoes(ler(t, rel)) {
					if i.cmd != "FROM" {
						continue
					}
					campos := strings.Fields(i.args)
					var ref string
					for _, c := range campos {
						if !strings.HasPrefix(c, "--") { // --platform=...
							ref = c
							break
						}
					}
					if !estagiosAntes[strings.ToLower(ref)] {
						usos = append(usos, usoDeImagem{rel, ref})
					}
					if n := len(campos); n >= 3 && strings.EqualFold(campos[n-2], "AS") {
						estagiosAntes[strings.ToLower(campos[n-1])] = true
					}
				}
			case strings.HasSuffix(nome, ".yml") || strings.HasSuffix(nome, ".yaml"):
				for _, m := range imagemDoYAML.FindAllStringSubmatch(ler(t, rel), -1) {
					if !imagensNossas[m[1]] {
						usos = append(usos, usoDeImagem{rel, m[1]})
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return usos
}

func TestDigests_TodaImagemDeForaFixada(t *testing.T) {
	usos := imagensDeFora(t)
	porArquivo := map[string]int{}
	for _, u := range usos {
		porArquivo[u.arquivo]++
		if !imagemFixada.MatchString(u.ref) {
			t.Errorf("%s: a imagem %q não está fixada (nome:tag@sha256:<64 hex>; o comentário do deploy/Dockerfile diz como)", u.arquivo, u.ref)
		}
	}
	// A varredura tem de achar as imagens que já se sabe que existem: um
	// arquivo que some da varredura (renomeado, mudado de pasta) não passa calado.
	for _, a := range []string{"deploy/Dockerfile", "deploy/backup/Dockerfile", "deploy/compose.yml", "deploy/compose.teste.yml"} {
		if porArquivo[a] == 0 {
			t.Errorf("a varredura não achou imagem de fora em %s", a)
		}
	}
	if porArquivo["deploy/Dockerfile"] != 3 || porArquivo["deploy/backup/Dockerfile"] != 3 {
		t.Errorf("os dois Dockerfiles têm 3 imagens de base cada, a varredura achou %v", porArquivo)
	}
}

// A mesma tag tem o mesmo digest em todo lugar: o golang dos dois Dockerfiles,
// e o Postgres do compose e o do CI, se houver.
func TestDigests_AMesmaTagComOMesmoDigest(t *testing.T) {
	digestDe := map[string]map[string][]string{}
	for _, u := range imagensDeFora(t) {
		nomeTag, digest, ok := strings.Cut(u.ref, "@")
		if !ok {
			continue // o teste de cima já reclama
		}
		if digestDe[nomeTag] == nil {
			digestDe[nomeTag] = map[string][]string{}
		}
		digestDe[nomeTag][digest] = append(digestDe[nomeTag][digest], u.arquivo)
	}
	for nomeTag, ds := range digestDe {
		if len(ds) > 1 {
			var lista []string
			for d, arqs := range ds {
				lista = append(lista, d+" em "+strings.Join(arqs, ", "))
			}
			sort.Strings(lista)
			t.Errorf("%s com digests diferentes: %s", nomeTag, strings.Join(lista, "; "))
		}
	}
}

// O próprio regex: o que passa e o que não passa.
func TestDigests_ORegexDaReferencia(t *testing.T) {
	d := strings.Repeat("ab", 32)
	for _, c := range []struct {
		ref  string
		quer bool
	}{
		{"postgres:18.6-trixie@sha256:" + d, true},
		{"gcr.io/distroless/static-debian12:nonroot@sha256:" + d, true},
		{"rclone/rclone:1.75.2@sha256:" + d, true},
		{"postgres:18.6-trixie", false},                  // sem digest
		{"postgres@sha256:" + d, false},                  // sem tag: quem lê não sabe a versão
		{"postgres:18.6-trixie@sha256:" + d[:63], false}, // digest curto
		{"postgres:18.6-trixie@sha256:" + strings.ToUpper(d), false},
		{"postgres:latest@sha256:" + d + "x", false},
	} {
		if got := imagemFixada.MatchString(c.ref); got != c.quer {
			t.Errorf("%q: fixada=%v, queria %v", c.ref, got, c.quer)
		}
	}
}
