package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/local"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
)

// F2 da ADR-018: o whisper-server roda com a pasta de trabalho na pasta do
// PROGRAMA e recebe o modelo por um caminho que não depende da pasta atual do
// Agent. Estes testes só montam a configuração, em pastas temporárias: nenhum
// servidor sobe, nenhum modelo é lido.

// O servidor é o do programa, e roda NA pasta do programa; o modelo é o que
// model.Resolve devolveu, dentro dela.
func TestConfigDoServidor_RodaNaPastaDoPrograma(t *testing.T) {
	p, prog, _ := pastasDeTeste(t, modoDeTeste{})
	cam := caminhosDe(p)
	modelo := filepath.Join(prog, model.SmallQ4_0.Arquivo)

	cfg := configDoServidor(cam, modelo)

	if cfg.Dir != prog {
		t.Errorf("Dir = %q, quero a pasta do programa %q: sem ela o servidor herda a pasta atual do Agent (atalho, início automático)", cfg.Dir, prog)
	}
	if cfg.ServerPath != cam.Servidor || !dentroDe(cfg.ServerPath, prog) {
		t.Errorf("ServerPath = %q, quero o servidor do programa %q", cfg.ServerPath, cam.Servidor)
	}
	if cfg.ModelPath != modelo {
		t.Errorf("ModelPath = %q, quero o que model.Resolve devolveu, %q", cfg.ModelPath, modelo)
	}
	if !dentroDe(cfg.ModelPath, cfg.Dir) {
		t.Errorf("o modelo %q não está dentro da pasta de trabalho %q: não daria para passá-lo pelo nome relativo", cfg.ModelPath, cfg.Dir)
	}
	if cfg.Language != "pt" || cfg.Threads < 1 {
		t.Errorf("a afinação medida mudou: idioma %q, threads %d", cfg.Language, cfg.Threads)
	}
	// A janela do encoder é por requisição (incidente de 01-out-2026): nada
	// de -ac na linha de comando, que valeria também para o ditado longo.
	if cfg.AudioCtx != 0 || cfg.Janela != local.JanelaRecomendada {
		t.Errorf("audio-ctx %d e janela %+v; quero -ac nenhum e a janela por duração %+v",
			cfg.AudioCtx, cfg.Janela, local.JanelaRecomendada)
	}
	// Sem flash attention: com ela, a janela alternando fazia o ditado longo
	// ler o K/V do curto anterior (docs/benchmarks/asr-longo-2026-10-01-nfa.md).
	if !cfg.SemFlashAttention {
		t.Error("o servidor do Agent tem de subir com -nfa (SemFlashAttention)")
	}
	if cfg.Decodificacao != (local.Decodificacao{}) {
		t.Errorf("Decodificacao = %+v: os ajustes do decodificador ainda não foram medidos (cmd/asrlongo)", cfg.Decodificacao)
	}
}

// No portátil (o bin\ de desenvolvimento) a pasta do programa é a do
// executável, e o servidor roda nela, como o resto sempre esteve.
func TestConfigDoServidor_NoPortatilEATambemAPastaDoExecutavel(t *testing.T) {
	p, prog, _ := pastasDeTeste(t, modoDeTeste{marcador: true})
	cfg := configDoServidor(caminhosDe(p), filepath.Join(prog, model.SmallQ4_0.Arquivo))
	if cfg.Dir != prog {
		t.Errorf("Dir = %q, quero a pasta do executável %q", cfg.Dir, prog)
	}
}

// A saída do servidor é dado e vai para a pasta de DADOS, nunca para a do
// programa (que o Setup troca e que numa instalação de máquina é só leitura).
// A ligação de abrirASRLocal a esse caminho é conferida no código de main.go,
// como as das demais pastas (TestRun_LigaCadaConsumidorAoCaminhoCerto); aqui,
// que o caminho da tabela está certo e que a configuração não o leva (o
// servidor recebe a saída por um io.Writer, e não por um caminho).
func TestConfigDoServidor_LogDoServidorEDeDadosEForaDaConfig(t *testing.T) {
	p, prog, local := pastasDeTeste(t, modoDeTeste{})
	cam := caminhosDe(p)
	dados := filepath.Join(local, "AuroraWhisper")

	if !dentroDe(cam.LogDoServidor, dados) || dentroDe(cam.LogDoServidor, prog) {
		t.Errorf("whisper-server.log = %q: devia estar em %q, e não em %q", cam.LogDoServidor, dados, prog)
	}
	if cfg := configDoServidor(cam, filepath.Join(prog, model.SmallQ4_0.Arquivo)); cfg.SaidaDoServidor != nil {
		t.Error("configDoServidor já traz a saída do servidor: quem abre o log é abrirASRLocal, em Dados()")
	}
}

// TestAbrirASRLocal_UsaConfigDoServidor: abrirASRLocal monta a configuração por
// configDoServidor, com a tabela de caminhos e o modelo que Resolve conferiu.
// Sem esta trava, uma Config escrita à mão em abrirASRLocal perderia o Dir sem
// que o teste da configuração percebesse.
func TestAbrirASRLocal_UsaConfigDoServidor(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var chamadas int
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "abrirASRLocal" {
			return true
		}
		ast.Inspect(fn.Body, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.CallExpr:
				if types.ExprString(x.Fun) == "configDoServidor" {
					chamadas++
					if len(x.Args) != 2 || types.ExprString(x.Args[0]) != "cam" || types.ExprString(x.Args[1]) != "caminhoModelo" {
						t.Errorf("configDoServidor chamada com (%s), quero (cam, caminhoModelo)", exprs(x.Args))
					}
				}
			case *ast.CompositeLit:
				if types.ExprString(x.Type) == "local.Config" {
					t.Errorf("%s: abrirASRLocal monta local.Config à mão; use configDoServidor, que leva a pasta de trabalho do servidor", fset.Position(x.Pos()))
				}
			}
			return true
		})
		return false
	})
	if chamadas != 1 {
		t.Errorf("abrirASRLocal chama configDoServidor %d vez(es); o teste espera exatamente 1", chamadas)
	}
}

func exprs(args []ast.Expr) string {
	s := ""
	for i, a := range args {
		if i > 0 {
			s += ", "
		}
		s += types.ExprString(a)
	}
	return s
}
