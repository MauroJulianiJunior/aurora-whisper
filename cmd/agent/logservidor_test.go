package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
)

// A saída do whisper-server leva o caminho da pasta do programa (a linha da
// biblioteca do processador), e o programa instalado mora numa pasta com o nome
// do usuário do Windows. Estes testes escrevem no whisper-server.log como o
// servidor escreveria, em pastas temporárias: nenhum servidor sobe.

// pastasComUsuario monta pastas cujo perfil tem espaço e acento ("José Teste"),
// que é o caso difícil, e devolve o perfil.
func pastasComUsuario(t *testing.T) (p pastas.Pastas, perfil string) {
	t.Helper()
	perfil = filepath.Join(t.TempDir(), "José Teste")
	prog := filepath.Join(perfil, "AppData", "Local", "Programs", "Aurora Whisper")
	p, err := pastas.Resolver(pastas.Ambiente{
		Executavel: func() (string, error) { return filepath.Join(prog, "aurora-whisper-agent.exe"), nil },
		PastaLocal: func() (string, error) { return filepath.Join(perfil, "AppData", "Local"), nil },
	})
	if err != nil {
		t.Fatalf("pastas.Resolver: %v", err)
	}
	return p, perfil
}

// latin1 escreve s com um byte por caractere, como o servidor o escreveria na
// página de código do Windows.
func latin1(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return string(b)
}

// formasDe devolve o caminho como um log pode escrevê-lo: cru, com a barra
// dobrada, com barra normal, e cada uma em UTF-8 e em Latin-1.
func formasDe(caminho string) []string {
	var formas []string
	for _, c := range []string{caminho, latin1(caminho)} {
		formas = append(formas, c,
			strings.ReplaceAll(c, `\`, `\\`),
			strings.ReplaceAll(c, `\`, `/`))
	}
	return formas
}

// TestAbrirLogDoServidor_NaoLevaOCaminhoDoUsuario é o defeito da revisão: o
// log instalado trazia "C:\Users\<nome>\...\ggml-cpu-*.dll" na primeira linha.
// O servidor escreve em pedaços (o pipe corta onde quer), então o teste também
// parte a linha no meio do nome do usuário.
func TestAbrirLogDoServidor_NaoLevaOCaminhoDoUsuario(t *testing.T) {
	p, perfil := pastasComUsuario(t)
	caminho := filepath.Join(p.Dados(), nomeLogDoServidor)
	sep := string(filepath.Separator)

	for _, codificacao := range []struct {
		nome string
		em   func(string) string
	}{
		{"UTF-8", func(s string) string { return s }},
		{"ANSI (Latin-1)", latin1},
	} {
		t.Run(codificacao.nome, func(t *testing.T) {
			saida, fechar := abrirLogDoServidor(caminho, p, perfil)
			if saida == nil {
				t.Fatal("abrirLogDoServidor não devolveu a saída")
			}
			linha := "load_backend: loaded CPU backend from " + codificacao.em(p.Programa()) + sep + "ggml-cpu-alderlake.dll\n"
			corte := strings.Index(linha, "Jo") + len("Jo") // no meio do nome do usuário
			saida.Write([]byte(linha[:corte]))
			saida.Write([]byte(linha[corte:]))
			saida.Write([]byte("whisper_init: loading model from 'ggml-small-q4_0.bin'\n"))
			// Um caminho do perfil que não é do programa nem dos dados: só o
			// perfil que main passa o troca.
			saida.Write([]byte("aviso: " + codificacao.em(filepath.Join(perfil, "Documents", "a.wav")) + "\n"))
			saida.Write([]byte("ultima linha, sem quebra"))
			fechar()

			b, err := os.ReadFile(caminho)
			if err != nil {
				t.Fatal(err)
			}
			log := string(b)

			for _, c := range []string{perfil, p.Programa(), p.Dados()} {
				for _, forma := range formasDe(c) {
					if strings.Contains(log, forma) {
						t.Errorf("o log ainda tem o caminho %q:\n%s", forma, log)
					}
				}
			}
			if strings.Contains(log, "José") || strings.Contains(log, "Jos\xe9") || strings.Contains(log, "Teste") {
				t.Errorf("o log ainda tem o nome do usuário:\n%s", log)
			}
			quer := "load_backend: loaded CPU backend from <programa>" + sep + "ggml-cpu-alderlake.dll\n" +
				"whisper_init: loading model from 'ggml-small-q4_0.bin'\n" +
				"aviso: <perfil>" + sep + "Documents" + sep + "a.wav\n" +
				"ultima linha, sem quebra"
			if log != quer {
				t.Errorf("o log ficou\n%q\nquero\n%q", log, quer)
			}
		})
	}
}

// O arquivo é recriado a cada subida: o log de uma subida não se soma ao da
// anterior, e o que sobrou do arquivo antigo não aparece.
func TestAbrirLogDoServidor_SobrescreveOArquivo(t *testing.T) {
	p, perfil := pastasComUsuario(t)
	caminho := filepath.Join(p.Dados(), nomeLogDoServidor)
	if err := os.WriteFile(caminho, []byte("a subida anterior, bem mais longa do que esta\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	saida, fechar := abrirLogDoServidor(caminho, p, perfil)
	saida.Write([]byte("nova\n"))
	fechar()

	b, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "nova\n" {
		t.Errorf("o log ficou %q, quero só o desta subida", b)
	}
}

// Sem poder criar o arquivo, o Agent sobe do mesmo jeito: devolve uma saída
// nula (e o servidor cai no stderr, como antes) e um fechar que não quebra.
func TestAbrirLogDoServidor_SemPoderCriarDevolveNulo(t *testing.T) {
	p, perfil := pastasComUsuario(t)
	caminho := filepath.Join(p.Dados(), "nao-existe", nomeLogDoServidor)

	saida, fechar := abrirLogDoServidor(caminho, p, perfil)
	if saida != nil {
		t.Errorf("a saída devia ser nula (interface nula, e não um ponteiro nulo dentro dela), veio %T", saida)
	}
	if fechar == nil {
		t.Fatal("a função de fechar é nula")
	}
	fechar() // não pode entrar em pânico
}

// TestAbrirASRLocal_LigaOLogFiltradoAoServidor: abrirASRLocal liga a saída do
// servidor ao que abrirLogDoServidor devolve, e não a um arquivo aberto à mão
// (que gravaria o caminho do usuário). Sem esta trava, trocar a ligação por um
// os.Create passaria por todos os testes acima.
func TestAbrirASRLocal_LigaOLogFiltradoAoServidor(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var achou bool
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "abrirASRLocal" {
			return true
		}
		achou = true
		var saida string // o nome que recebe o primeiro resultado de abrirLogDoServidor
		ligou := false   // cfg.SaidaDoServidor = <esse nome>
		ast.Inspect(fn.Body, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.CallExpr:
				switch nome := types.ExprString(x.Fun); nome {
				case "os.Create", "os.OpenFile", "os.NewFile":
					t.Errorf("%s: abrirASRLocal abre arquivo à mão (%s); a saída do servidor passa por abrirLogDoServidor, que troca os caminhos", fset.Position(x.Pos()), nome)
				}
			case *ast.AssignStmt:
				if len(x.Lhs) == 2 && len(x.Rhs) == 1 {
					if c, ok := x.Rhs[0].(*ast.CallExpr); ok && types.ExprString(c.Fun) == "abrirLogDoServidor" {
						saida = types.ExprString(x.Lhs[0])
					}
				}
				if len(x.Lhs) == 1 && len(x.Rhs) == 1 && types.ExprString(x.Lhs[0]) == "cfg.SaidaDoServidor" {
					if got := types.ExprString(x.Rhs[0]); saida != "" && got == saida {
						ligou = true
					} else {
						t.Errorf("%s: cfg.SaidaDoServidor = %s; quero o resultado de abrirLogDoServidor", fset.Position(x.Pos()), got)
					}
				}
			}
			return true
		})
		if saida == "" {
			t.Error("abrirASRLocal não chama abrirLogDoServidor")
		}
		if !ligou {
			t.Error("abrirASRLocal não liga cfg.SaidaDoServidor ao resultado de abrirLogDoServidor")
		}
		return false
	})
	if !achou {
		t.Fatal("não achei abrirASRLocal em main.go")
	}
}
