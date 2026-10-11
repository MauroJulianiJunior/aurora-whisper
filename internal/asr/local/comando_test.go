package local

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Estes testes cobrem a F2 da ADR-018: o whisper-server roda com a pasta de
// trabalho na pasta do PROGRAMA e recebe o modelo pelo nome relativo, para o
// Agent instalado funcionar de qualquer pasta atual (atalho do Menu Iniciar,
// início automático, terminal) e para o whisper-server.log não levar o nome do
// usuário do Windows.
//
// Montam o comando e o *exec.Cmd e NÃO executam nada: caminhos de pastas
// temporárias, nenhum processo, nenhum servidor.

// pastaDoPrograma devolve uma pasta absoluta, com espaço e acento no meio e
// o nome de um usuário que não pode vazar para o log, sem criá-la.
func pastaDoPrograma(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "Users", "João Çedilha", "AppData", "Local", "Programs", "Aurora Whisper")
}

func TestCaminhoDoModelo(t *testing.T) {
	prog := pastaDoPrograma(t)
	fora := filepath.Join(filepath.Dir(prog), "Outro Programa")
	const arquivo = "ggml-small-q4_0.bin"

	casos := []struct {
		nome string
		cfg  Config
		quer string
	}{
		{
			"modelo ao lado do servidor vira o nome do arquivo",
			Config{Dir: prog, ModelPath: filepath.Join(prog, arquivo)},
			arquivo,
		},
		{
			"modelo numa subpasta vira o caminho relativo a ela",
			Config{Dir: prog, ModelPath: filepath.Join(prog, "modelos", arquivo)},
			filepath.Join("modelos", arquivo),
		},
		{
			"pasta com barra no fim",
			Config{Dir: prog + string(filepath.Separator), ModelPath: filepath.Join(prog, arquivo)},
			arquivo,
		},
		{
			"sem Dir, o caminho é o que veio",
			Config{ModelPath: filepath.Join(prog, arquivo)},
			filepath.Join(prog, arquivo),
		},
		{
			"modelo fora da pasta fica absoluto: nunca sobe um nível",
			Config{Dir: prog, ModelPath: filepath.Join(fora, arquivo)},
			filepath.Join(fora, arquivo),
		},
		{
			"pasta irmã com o mesmo prefixo de nome não conta como dentro",
			Config{Dir: prog, ModelPath: filepath.Join(prog+" 2", arquivo)},
			filepath.Join(prog+" 2", arquivo),
		},
		{
			"o modelo é a própria pasta",
			Config{Dir: prog, ModelPath: prog},
			prog,
		},
		{
			"o modelo é a pasta de cima: não sobe um nível",
			Config{Dir: prog, ModelPath: filepath.Dir(prog)},
			filepath.Dir(prog),
		},
		{
			"caminho relativo já é relativo a Dir: não mexe",
			Config{Dir: prog, ModelPath: arquivo},
			arquivo,
		},
		{
			// Rel("programa", "programa/x.bin") daria "x.bin", e o servidor,
			// já em "programa", procuraria programa/x.bin dentro dele.
			"Dir e modelo relativos: o modelo já é relativo a Dir, não se olha de novo",
			Config{Dir: "programa", ModelPath: filepath.Join("programa", arquivo)},
			filepath.Join("programa", arquivo),
		},
		{
			"Dir relativa: não dá para saber a que ela se refere",
			Config{Dir: "programa", ModelPath: filepath.Join(prog, arquivo)},
			filepath.Join(prog, arquivo),
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := caminhoDoModelo(c.cfg); got != c.quer {
				t.Errorf("caminhoDoModelo = %q, quero %q", got, c.quer)
			}
		})
	}
}

// TestServerArgs_ModeloRelativoNaoLevaOPerfil: o argv do servidor, com a pasta
// do programa em Dir, não traz o caminho do programa nem o nome do usuário em
// lugar nenhum. É o que impede o whisper-server.log de repeti-los.
func TestServerArgs_ModeloRelativoNaoLevaOPerfil(t *testing.T) {
	prog := pastaDoPrograma(t)
	cfg := Config{
		ServerPath: filepath.Join(prog, "whisper-server.exe"),
		Dir:        prog,
		ModelPath:  filepath.Join(prog, "ggml-small-q4_0.bin"),
		Language:   "pt", Threads: 8, AudioCtx: 750,
	}
	args := serverArgs(cfg, "127.0.0.1", 53124)

	i := indice(args, "-m")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("argv = %q, falta o -m com valor", args)
	}
	if args[i+1] != "ggml-small-q4_0.bin" {
		t.Errorf("-m = %q, quero o nome do arquivo, sem pasta", args[i+1])
	}
	for _, a := range args {
		if strings.Contains(a, "João") || strings.Contains(a, "Users") || strings.Contains(a, "AppData") {
			t.Errorf("o argumento %q leva o caminho do perfil do usuário", a)
		}
	}
}

func indice(args []string, quer string) int {
	for i, a := range args {
		if a == quer {
			return i
		}
	}
	return -1
}

func TestMontarComando_PastaDeTrabalhoEDoPrograma(t *testing.T) {
	prog := pastaDoPrograma(t)
	cfg := Config{
		ServerPath: filepath.Join(prog, "whisper-server.exe"),
		Dir:        prog,
		ModelPath:  filepath.Join(prog, "ggml-small-q4_0.bin"),
		Language:   "pt",
	}
	c := montarComando(cfg, "127.0.0.1", 41000)

	if c.Caminho != cfg.ServerPath {
		t.Errorf("Caminho = %q, quero o servidor %q", c.Caminho, cfg.ServerPath)
	}
	if c.Dir != prog {
		t.Errorf("Dir = %q, quero a pasta do programa %q", c.Dir, prog)
	}
	if got, quer := strings.Join(c.Args, " "), "--host 127.0.0.1 --port 41000 -m ggml-small-q4_0.bin -l pt"; got != quer {
		t.Errorf("Args = %q, quero %q", got, quer)
	}
}

// TestMontarComando_SemDirMantemOComportamentoDeAntes: quem não preenche Dir
// (os testes de latência, que passam o caminho absoluto do bin\) segue
// recebendo o modelo inteiro e herdando a pasta atual.
func TestMontarComando_SemDirMantemOComportamentoDeAntes(t *testing.T) {
	prog := pastaDoPrograma(t)
	modelo := filepath.Join(prog, "ggml-small-q4_0.bin")
	c := montarComando(Config{ServerPath: "whisper-server.exe", ModelPath: modelo}, "127.0.0.1", 1)

	if c.Dir != "" {
		t.Errorf("Dir = %q, quero vazio: o servidor herda a pasta do Agent", c.Dir)
	}
	if i := indice(c.Args, "-m"); i < 0 || c.Args[i+1] != modelo {
		t.Errorf("Args = %q, quero o -m com o caminho inteiro %q", c.Args, modelo)
	}
}

// TestExecCmd_LigaAPastaEASaida: o *exec.Cmd sai com Dir e com a saída
// ligadas, e NINGUÉM o inicia aqui.
func TestExecCmd_LigaAPastaEASaida(t *testing.T) {
	prog := pastaDoPrograma(t)
	cfg := Config{
		ServerPath: filepath.Join(prog, "whisper-server.exe"),
		Dir:        prog,
		ModelPath:  filepath.Join(prog, "ggml-small-q4_0.bin"),
	}
	var saida bytes.Buffer
	cmd := montarComando(cfg, "127.0.0.1", 41000).execCmd(&saida)

	if cmd.Dir != prog {
		t.Errorf("cmd.Dir = %q, quero a pasta do programa %q", cmd.Dir, prog)
	}
	if cmd.Path != cfg.ServerPath {
		t.Errorf("cmd.Path = %q, quero %q", cmd.Path, cfg.ServerPath)
	}
	if len(cmd.Args) < 2 || cmd.Args[0] != cfg.ServerPath {
		t.Errorf("cmd.Args = %q, quero o servidor primeiro, depois os argumentos", cmd.Args)
	}
	if cmd.Stdout != &saida || cmd.Stderr != &saida {
		t.Error("stdout e stderr do servidor não vão para a saída pedida (o whisper-server.log)")
	}
	if cmd.Process != nil {
		t.Error("execCmd iniciou o processo: ele só monta o comando")
	}
}

func TestExecCmd_SaidaNilVaiParaOStderrDoAgent(t *testing.T) {
	cmd := montarComando(Config{ServerPath: filepath.Join(pastaDoPrograma(t), "whisper-server.exe")}, "127.0.0.1", 1).execCmd(nil)
	if cmd.Stdout != os.Stderr || cmd.Stderr != os.Stderr {
		t.Error("sem saída pedida, o servidor tem de escrever no stderr do Agent, como antes")
	}
}

// TestServerArgs_SemFlashAttention: o -nfa só vai quando pedido, e sozinho
// (sem valor). A flash attention é o padrão do b5130 (server.cpp:106).
func TestServerArgs_SemFlashAttention(t *testing.T) {
	com := serverArgs(Config{ServerPath: "s", ModelPath: "m"}, "127.0.0.1", 1)
	if indice(com, "-nfa") >= 0 {
		t.Errorf("sem pedir, o argv levou -nfa: %q", com)
	}
	sem := serverArgs(Config{ServerPath: "s", ModelPath: "m", SemFlashAttention: true}, "127.0.0.1", 1)
	i := indice(sem, "-nfa")
	if i < 0 {
		t.Fatalf("SemFlashAttention e o argv não levou -nfa: %q", sem)
	}
	if len(sem) != len(com)+1 {
		t.Errorf("o -nfa tem de ir sem valor: %q contra %q", sem, com)
	}
}
