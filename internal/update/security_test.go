package update

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenIdentFragments são fragmentos de NOME de identificador que
// indicariam, se aparecessem em um campo de struct, const ou var deste
// pacote, uma credencial de provider vazando para o desktop (AC-040,
// THR-006: "provider secret no client" → mitigação "gateway"). A checagem
// é sobre o NOME do identificador, não sobre valores/comentários — sha256
// e signature são dados públicos do release, não segredos, e não batem
// aqui.
var forbiddenIdentFragments = []string{
	"token",
	"apikey",
	"clientsecret",
	"githubtoken",
	"providerkey",
	"providersecret",
	"accesskey",
	"privatekey",
	"secretkey",
}

// TestPackageNeverDeclaresProviderSecrets varre todo .go não-teste deste
// pacote (via go/ast) e falha se algum nome de campo, const, var ou tipo
// contiver um fragmento de "token/chave/secret de provider" (AC-040: "o
// cliente de update não carrega token do GitHub nem chave de provider —
// ele só fala com o endpoint cloud").
func TestPackageNeverDeclaresProviderSecrets(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	fset := token.NewFileSet()
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++

		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch decl := n.(type) {
			case *ast.Field:
				for _, id := range decl.Names {
					checkIdentNoSecret(t, name, id.Name)
				}
			case *ast.ValueSpec:
				for _, id := range decl.Names {
					checkIdentNoSecret(t, name, id.Name)
				}
			case *ast.TypeSpec:
				checkIdentNoSecret(t, name, decl.Name.Name)
			}
			return true
		})
	}

	if scanned == 0 {
		t.Fatal("nenhum arquivo .go não-teste encontrado em internal/update — teste não varreu nada")
	}
}

func checkIdentNoSecret(t *testing.T, file, ident string) {
	t.Helper()
	lower := strings.ToLower(ident)
	for _, bad := range forbiddenIdentFragments {
		if strings.Contains(lower, bad) {
			t.Errorf("%s: identificador %q parece carregar segredo de provider (AC-040): contém %q", file, ident, bad)
		}
	}
}
