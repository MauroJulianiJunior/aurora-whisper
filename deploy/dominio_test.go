package deploy

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O repositório é público e o projeto não tem servidor central: a nuvem é de
// quem a sobe. Os roteiros usam um domínio de EXEMPLO (aurora.exemplo.com.br)
// e nenhum arquivo traz o endereço da instância do mantenedor. Estes testes só
// LEEM os arquivos do repositório; nada é executado.

const dominioDeExemplo = "aurora.exemplo.com.br"

// dominioDoMantenedor é montado em duas metades para o próprio teste não ser
// um arquivo que traz o endereço (a busca por ele no repositório dá zero).
var dominioDoMantenedor = "mj" + "j.net.br"

func TestRoteiros_UsamODominioDeExemplo(t *testing.T) {
	for _, arq := range []string{roteiroDeploy, "docs/teste-final-f11.md", "docs/restauracao-f11.md", "deploy/README.md"} {
		texto := ler(t, arq)
		exigirContem(t, arq, texto, dominioDeExemplo)
		exigirNaoContem(t, arq, strings.ToLower(texto), dominioDoMantenedor)
	}
	// O comando do roteiro que leva o domínio vai com o de exemplo.
	exigirContem(t, roteiroDeploy, ler(t, roteiroDeploy), `-Dominio `+dominioDeExemplo)
}

func TestRepositorio_NenhumArquivoTrazODominioDoMantenedor(t *testing.T) {
	// Pastas que não são do repositório (cópias de trabalho, dependências e
	// saídas de build) ficam de fora.
	pula := map[string]bool{".git": true, ".claude": true, "node_modules": true, "dist": true, "bin": true, "vendor": true}
	vistos := 0
	err := filepath.WalkDir(raiz, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if pula[d.Name()] && p != raiz {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 4<<20 {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		vistos++
		if strings.Contains(strings.ToLower(string(data)), dominioDoMantenedor) {
			t.Errorf("%s traz o endereço da instância do mantenedor: use %s", filepath.ToSlash(p), dominioDeExemplo)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if vistos < 100 {
		t.Errorf("olhei só %d arquivos: o teste não está olhando o repositório", vistos)
	}
}
