package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// O dicionário para o celular (ADR-023, D3) na montagem do Agent: a fase do sync
// lê o MESMO dicionario.json do aprendizado, ou o arquivo indisponível quando a
// subida não o conferiu. Tudo em t.TempDir(), sem rede.

func TestCopiaDoSync_LeODicionarioDoAprendizado(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	n := montarNuvem(instalacaoVazia{}, nil, pastas.ModoPortatil, nil)

	c := copiaDoSync(n, m.a)
	if c == nil || c.Fonte == nil || c.Opcao == nil {
		t.Fatalf("a fase do dicionário não foi montada: %+v", c)
	}
	semente, err := c.Fonte.Dicionario(context.Background())
	if err != nil || semente.Usuario != "mauro" || len(semente.Entradas) == 0 {
		t.Fatalf("o dicionário da fase: %+v, %v", semente.Usuario, err)
	}
	// E a cópia dele sai pela função de sempre (sem o usuário local).
	copia, sha, err := semente.BytesDaCopia()
	if err != nil || len(sha) != 64 || len(copia) == 0 {
		t.Fatalf("BytesDaCopia: %v", err)
	}
	// Sem a opção ligada (a coluna nasce 0, e a nuvem não tem a opção montada),
	// a fase não manda nada.
	if l, err := c.Opcao.Ligada(context.Background()); err != nil || l {
		t.Errorf("a opção sem a montagem do Agent deveria estar desligada: %v, %v", l, err)
	}

	// Sem conferir o arquivo (prepararOAprendido falhou), a cópia também não o lê.
	m.a.semConferir = true
	c = copiaDoSync(n, m.a)
	if _, err := c.Fonte.Dicionario(context.Background()); err != dictionary.ErrSemArquivo {
		t.Errorf("sem conferir, o dicionário da fase: %v; quero o arquivo indisponível", err)
	}

	// Sem o serviço ou sem o aprendizado, a fase fica de fora.
	if copiaDoSync(nil, m.a) != nil || copiaDoSync(n, nil) != nil {
		t.Error("a fase deveria ficar de fora sem o serviço ou sem o aprendizado")
	}
	if cfg := configDoSync(nil, nil, m.a, nil); cfg.Copia != nil {
		t.Error("configDoSync (sem o montarSync) não monta a fase do dicionário")
	}
}

// run() não roda em teste, então a ligação é conferida no código de main.go: a
// opção e a fonte vão ao serviço DEPOIS de conferir o dicionário (a fonte lê o
// semConferir) e ANTES de montar o laço, e a API local recebe o serviço.
func TestRun_DicionarioDoCelularLigadoNaOrdemCerta(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string][]token.Pos{}
	var campoNaAPI bool
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			nome := types.ExprString(n.Fun)
			if nome == "prepararOAprendido" || nome == "montarSync" || nome == "nuvemDoPC.UsarDicionario" {
				pos[nome] = append(pos[nome], n.Pos())
			}
			if nome == "nuvemDoPC.UsarDicionario" && len(n.Args) == 2 &&
				(types.ExprString(n.Args[1]) != "aprendizado.fonteDaCopia()" ||
					types.ExprString(n.Args[0]) != "storage.NovaOpcaoDaCopiaDoDicionario(prefs, preferenciasPadrao())") {
				t.Errorf("UsarDicionario com os argumentos errados: %s", fset.Position(n.Pos()))
			}
		case *ast.KeyValueExpr:
			if types.ExprString(n.Key) == "CopiaDoDicionario" && types.ExprString(n.Value) == "nuvemDoPC" {
				campoNaAPI = true
			}
		}
		return true
	})
	for _, nome := range []string{"prepararOAprendido", "nuvemDoPC.UsarDicionario", "montarSync"} {
		if len(pos[nome]) != 1 {
			t.Fatalf("main.go chama %s %d vez(es); espero exatamente 1", nome, len(pos[nome]))
		}
	}
	if !(pos["prepararOAprendido"][0] < pos["nuvemDoPC.UsarDicionario"][0] && pos["nuvemDoPC.UsarDicionario"][0] < pos["montarSync"][0]) {
		t.Error("main.go liga o dicionário do celular fora da ordem: conferir o dicionário, UsarDicionario, montarSync")
	}
	if !campoNaAPI {
		t.Error("main.go não passa o serviço da nuvem como CopiaDoDicionario da API local")
	}
}
