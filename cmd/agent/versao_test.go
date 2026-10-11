package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// -X main.version só age sobre variável: numa const o build de release
// (scripts/montar-instalador.ps1) ficaria com a versão de desenvolvimento, sem
// erro nenhum.
func TestVersion_EVariavelParaOLdflagsX(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	achou := false
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, s := range g.Specs {
			v, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range v.Names {
				if n.Name != "version" {
					continue
				}
				achou = true
				if g.Tok != token.VAR {
					t.Errorf("version é declarada com %s; o -ldflags -X só funciona com var", g.Tok)
				}
				// Inicializada com um literal (ou sem nada): -X não troca o
				// que é calculado por chamada de função.
				for _, val := range v.Values {
					if _, lit := val.(*ast.BasicLit); !lit {
						t.Errorf("version é inicializada por %s; o -X só troca a que vem de um literal", types.ExprString(val))
					}
				}
			}
		}
	}
	if !achou {
		t.Error("main.go não declara version")
	}
}
