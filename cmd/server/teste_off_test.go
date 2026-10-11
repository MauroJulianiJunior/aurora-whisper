//go:build !logindeteste

package main

import (
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sem a build tag logindeteste, o servidor de teste não existe: "teste" é um
// comando desconhecido, e a ajuda nem o cita.
func TestTeste_NaoExisteSemATag(t *testing.T) {
	out, errOut, code := runCmd(t, map[string]string{envPainelDir: painelDir(t), envHTTPAddr: "127.0.0.1:0"}, "teste")
	if code != 2 || !strings.Contains(errOut, `comando desconhecido "teste"`) || out != "" {
		t.Fatalf("teste sem a tag: %d %q %q", code, out, errOut)
	}
	help, _, _ := runCmd(t, nil, "help")
	if strings.Contains(help, "teste") {
		t.Errorf("a ajuda cita o comando teste sem a tag: %s", help)
	}
}

// O cofre por processo e o provedor de modelos de mentira do servidor de teste
// (teste.go) só existem com a build tag logindeteste: nenhum arquivo que entra
// no build normal declara um deles, e o único ModelosDoProvedor do build normal é
// o que fala com a rede (server.ModelosPelaRede). O teste lê o código-fonte de
// cmd/server e de internal/server (sem os _test.go, e sem depender da tag) e
// confere a restrição de cada arquivo que declara um deles.
//
// Se alguém copiasse o provedor de mentira ou o cofre aleatório para um arquivo
// sem a tag, o serve de produção poderia montá-los: isto falha antes.
func TestTeste_OCofreEOProvedorDeMentiraSoExistemComATag(t *testing.T) {
	// O que só pode ser declarado em arquivo que só compila com a tag.
	vigiados := map[string]bool{
		"modelosDeTeste": true, "modelosDeMentira": true, "novoCofreDeTeste": true,
		"chaveQueRecusa": true, "chaveSemCredito": true, "chaveQueCai": true,
	}
	achados := map[string]string{}
	fset := token.NewFileSet()
	for _, dir := range []string{".", filepath.Join("..", "..", "internal", "server")} {
		entradas, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entradas {
			nome := e.Name()
			if e.IsDir() || !strings.HasSuffix(nome, ".go") || strings.HasSuffix(nome, "_test.go") {
				continue
			}
			caminho := filepath.Join(dir, nome)
			f, err := parser.ParseFile(fset, caminho, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("%s: %v", caminho, err)
			}
			soComATag := soCompilaComATag(f)
			declara := func(id string) {
				if !vigiados[id] {
					return
				}
				achados[id] = caminho
				if !soComATag {
					t.Errorf("%s declara %s e entra no build normal: só pode existir com a build tag logindeteste", caminho, id)
				}
			}
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil {
						declara(d.Name.Name)
						continue
					}
					// Todo método ListarModelos do build normal é o da rede.
					if d.Name.Name == "ListarModelos" && !soComATag && tipoDoReceptor(d) != "ModelosPelaRede" {
						t.Errorf("%s declara %s.ListarModelos e entra no build normal: o único provedor de modelos dele é o da rede",
							caminho, tipoDoReceptor(d))
					}
				case *ast.GenDecl:
					for _, s := range d.Specs {
						switch s := s.(type) {
						case *ast.TypeSpec:
							declara(s.Name.Name)
						case *ast.ValueSpec:
							for _, n := range s.Names {
								declara(n.Name)
							}
						}
					}
				}
			}
		}
	}
	// O teste precisa de dentes: se um renomear sumisse com os identificadores,
	// ele passaria sem provar nada.
	for id := range vigiados {
		if arquivo, ok := achados[id]; !ok || filepath.Base(arquivo) != "teste.go" {
			t.Errorf("%s não foi achado em teste.go (achado em %q): atualize a lista deste teste", id, arquivo)
		}
	}
}

// soCompilaComATag diz se o arquivo só entra no build com a tag logindeteste: a
// linha //go:build dele é verdadeira com ela e falsa sem ela.
func soCompilaComATag(f *ast.File) bool {
	for _, g := range f.Comments {
		if g.Pos() >= f.Package {
			break
		}
		for _, c := range g.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			expr, err := constraint.Parse(c.Text)
			if err != nil {
				return false
			}
			com := expr.Eval(func(tag string) bool { return tag == "logindeteste" })
			sem := expr.Eval(func(string) bool { return false })
			return com && !sem
		}
	}
	return false
}

// tipoDoReceptor é o nome do tipo de um método (sem o * nem os parâmetros).
func tipoDoReceptor(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	switch t := d.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}
