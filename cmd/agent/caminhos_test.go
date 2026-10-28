package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Estes testes rodam em pastas temporárias, com o ambiente das pastas
// INJETADO: nenhum toca no LOCALAPPDATA de verdade nem no bin\ de quem
// desenvolve.

// modoDeTeste monta as pastas num ambiente falso. prog e local são
// subpastas de t.TempDir(); marcador e banco dizem o que existe ao lado do
// executável.
type modoDeTeste struct {
	marcador   bool // cria o arquivo portatil
	banco      bool // cria um historico.db ao lado do executável
	dicionario bool // cria um dicionario.json ao lado do executável
}

func pastasDeTeste(t *testing.T, m modoDeTeste) (pastas.Pastas, string, string) {
	t.Helper()
	raiz := t.TempDir()
	prog := filepath.Join(raiz, "Aurora Whisper")
	local := filepath.Join(raiz, "AppData", "Local")
	if err := os.MkdirAll(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	if m.marcador {
		escreverArquivo(t, filepath.Join(prog, pastas.Marcador))
	}
	if m.banco {
		escreverArquivo(t, filepath.Join(prog, pastas.BancoDoHistorico))
	}
	if m.dicionario {
		escreverArquivo(t, filepath.Join(prog, nomeDicionario))
	}
	p, err := pastas.Resolver(pastas.Ambiente{
		Executavel: func() (string, error) { return filepath.Join(prog, "aurora-whisper-agent.exe"), nil },
		PastaLocal: func() (string, error) { return local, nil },
	})
	if err != nil {
		t.Fatalf("pastas.Resolver: %v", err)
	}
	return p, prog, local
}

func escreverArquivo(t *testing.T, caminho string) {
	t.Helper()
	if err := os.WriteFile(caminho, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// trazOCaminho diz se a saída do log leva o caminho, em qualquer das formas em
// que um log o escreve: crua, com a barra dobrada (o slog a dobra quando o
// valor vai entre aspas, o que acontece com a mensagem e com qualquer caminho
// com espaço) e com barra normal. Conferir só a forma crua deixava passar um
// caminho escrito dentro de uma mensagem.
func trazOCaminho(saida, caminho string) (string, bool) {
	for _, forma := range []string{
		caminho,
		strings.ReplaceAll(caminho, `\`, `\\`),
		strings.ReplaceAll(caminho, `\`, `/`),
	} {
		if strings.Contains(saida, forma) {
			return forma, true
		}
	}
	return "", false
}

func dentroDe(caminho, pasta string) bool {
	rel, err := filepath.Rel(pasta, caminho)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// TestCaminhos_DadosEmDadosEProgramaEmPrograma é a regra da ADR-018: o que o
// Agent GRAVA (o histórico, o dicionário, os dois logs) vai para Dados(), e o
// que só LÊ (o modelo, o servidor, o painel) vem de Programa(). Numa
// instalação normal as duas são pastas diferentes, e nenhum dado cai na
// pasta do programa.
func TestCaminhos_DadosEmDadosEProgramaEmPrograma(t *testing.T) {
	p, prog, local := pastasDeTeste(t, modoDeTeste{})
	c := caminhosDe(p)
	dados := filepath.Join(local, pastas.NomeDaPastaDeDados)

	daqui := map[string]string{
		"historico.db":       c.Historico,
		"dicionario.json":    c.Dicionario,
		"agent.log":          c.LogDoAgent,
		"whisper-server.log": c.LogDoServidor,
		"notas-imagens":      c.ImagensDasNotas,
	}
	for nome, caminho := range daqui {
		if !dentroDe(caminho, dados) || dentroDe(caminho, prog) {
			t.Errorf("%s = %q: é dado, devia estar em %q e fora de %q", nome, caminho, dados, prog)
		}
		if filepath.Base(caminho) != nome {
			t.Errorf("%s: o arquivo se chama %q", nome, filepath.Base(caminho))
		}
	}
	doPrograma := map[string]string{
		"whisper-server.exe": c.Servidor,
		"painel":             c.Painel,
		"pasta do modelo":    c.Programa,
	}
	for nome, caminho := range doPrograma {
		if caminho != prog && !dentroDe(caminho, prog) {
			t.Errorf("%s = %q: é do programa, devia estar em %q", nome, caminho, prog)
		}
		if dentroDe(caminho, dados) {
			t.Errorf("%s = %q está na pasta de dados %q", nome, caminho, dados)
		}
	}
	if c.Programa != prog {
		t.Errorf("a pasta do modelo = %q, quero a do programa %q", c.Programa, prog)
	}
}

// No modo portátil (o bin\ de desenvolvimento) tudo fica na pasta do
// executável, como sempre: os dados dele, e o roteiro manual, continuam
// valendo. E no legado também.
func TestCaminhos_PortatilJuntaTudoNaPastaDoExecutavel(t *testing.T) {
	for nome, m := range map[string]modoDeTeste{
		"marcador":           {marcador: true},
		"banco sem marcador": {banco: true},
	} {
		t.Run(nome, func(t *testing.T) {
			p, prog, local := pastasDeTeste(t, m)
			c := caminhosDe(p)
			for _, caminho := range []string{c.Historico, c.Dicionario, c.LogDoAgent, c.LogDoServidor, c.Servidor, c.Painel} {
				if !dentroDe(caminho, prog) {
					t.Errorf("%q está fora da pasta do executável %q", caminho, prog)
				}
			}
			if _, err := os.Stat(local); err == nil {
				t.Error("o modo portátil criou a pasta local")
			}
		})
	}
}

// TestNenhumConsumidorJuntaComODiretorioDoExecutavel trava a causa de tudo:
// cinco lugares repetiam filepath.Dir(os.Executable()) e gravavam dado ao
// lado do .exe. Hoje só caminhosDe junta pasta e nome; ninguém em cmd/agent
// deve voltar a calcular o diretório do executável por conta própria.
func TestNenhumConsumidorJuntaComODiretorioDoExecutavel(t *testing.T) {
	proibido := regexp.MustCompile(`filepath\.Dir\(\s*(exe\b|os\.Executable)`)
	arquivos, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range arquivos {
		if strings.HasSuffix(a, "_test.go") {
			continue
		}
		b, err := os.ReadFile(a)
		if err != nil {
			t.Fatal(err)
		}
		if loc := proibido.FindIndex(b); loc != nil {
			t.Errorf("%s calcula o diretório do executável por conta própria (%q): use caminhosDe", a, b[loc[0]:loc[1]])
		}
	}
}

// TestLogNaoTemChaveDeCaminho: nenhuma linha do log do Agent carrega um
// campo de caminho (ADR-018). O caminho completo leva o nome de usuário do
// Windows, e o log é o que a pessoa cola num issue. Confere o código-fonte:
// nenhum literal de chave "caminho", "pasta", "exe"... em cmd/agent.
func TestLogNaoTemChaveDeCaminho(t *testing.T) {
	chaves := map[string]bool{
		"caminho": true, "pasta": true, "procurado_em": true, "exe": true,
		"dir": true, "path": true, "destino": true, "programa": true,
	}
	arquivos, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, a := range arquivos {
		if strings.HasSuffix(a, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, a, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && chaves[s] {
				t.Errorf("%s: a chave de log %s leva caminho (e o caminho leva o nome de usuário): logue só o nome ou o modo",
					fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}

// TestRegistrarPastas_SoONomeDoModo: o log diz o modo, e nunca o caminho.
func TestRegistrarPastas_SoONomeDoModo(t *testing.T) {
	casos := []struct {
		nome    string
		m       modoDeTeste
		quer    string
		querAvi bool
	}{
		{"instalado", modoDeTeste{}, "dados=localappdata", false},
		{"marcador", modoDeTeste{marcador: true}, "dados=portatil", false},
		{"legado", modoDeTeste{banco: true}, "dados=portatil-legado", true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			p, prog, local := pastasDeTeste(t, c.m)
			var buf bytes.Buffer
			registrarPastas(slog.New(slog.NewTextHandler(&buf, nil)), p)
			saida := buf.String()

			if !strings.Contains(saida, c.quer+"\n") && !strings.Contains(saida, c.quer+" ") {
				t.Errorf("o log não diz %q:\n%s", c.quer, saida)
			}
			for _, caminho := range []string{prog, local, filepath.Dir(prog)} {
				if forma, traz := trazOCaminho(saida, caminho); traz {
					t.Errorf("o log traz o caminho %q:\n%s", forma, saida)
				}
			}
			if temAviso := strings.Contains(saida, "level=WARN"); temAviso != c.querAvi {
				t.Errorf("aviso = %v, quero %v:\n%s", temAviso, c.querAvi, saida)
			}
			if c.querAvi {
				for _, quer := range []string{pastas.Marcador, pastas.BancoDoHistorico, "asrstage"} {
					if !strings.Contains(saida, quer) {
						t.Errorf("o aviso do modo legado não diz %q:\n%s", quer, saida)
					}
				}
			}
		})
	}
}

// TestAbrirHistoricoEm_LogSemCaminho: a linha "banco pronto" dizia o
// caminho inteiro.
func TestAbrirHistoricoEm_LogSemCaminho(t *testing.T) {
	pasta := t.TempDir()
	caminho := filepath.Join(pasta, "historico.db")
	var buf bytes.Buffer
	_, _, _, fechar, err := abrirHistoricoEm(slog.New(slog.NewTextHandler(&buf, nil)), caminho, "mauro", clock.NewFake(agoraDaSubida))
	if err != nil {
		t.Fatalf("abrirHistoricoEm: %v", err)
	}
	defer fechar()
	if strings.Contains(buf.String(), pasta) {
		t.Errorf("o log do histórico traz o caminho %q:\n%s", pasta, buf.String())
	}
	if !strings.Contains(buf.String(), "banco pronto") {
		t.Errorf("a linha de 'banco pronto' sumiu:\n%s", buf.String())
	}
}

// TestAbrirDicionarioEm_SemArquivoNaoDizOCaminho: a linha "nenhum
// encontrado" dizia onde procurou, com o caminho inteiro.
func TestAbrirDicionarioEm_SemArquivoNaoDizOCaminho(t *testing.T) {
	pasta := t.TempDir()
	var buf bytes.Buffer
	_, _, _, _, err := abrirDicionarioEm(slog.New(slog.NewTextHandler(&buf, nil)), filepath.Join(pasta, nomeDicionario))
	if err != nil {
		t.Fatalf("abrirDicionarioEm: %v", err)
	}
	if strings.Contains(buf.String(), pasta) {
		t.Errorf("o log do dicionário traz o caminho %q:\n%s", pasta, buf.String())
	}
	if !strings.Contains(buf.String(), "nenhum encontrado") {
		t.Errorf("a linha 'nenhum encontrado' sumiu:\n%s", buf.String())
	}
}

// TestCamposDaMigracao_CopiaSoComONome: a linha da migração dizia onde ficou
// a cópia, com o caminho inteiro. O nome basta: ela fica ao lado do banco.
func TestCamposDaMigracao_CopiaSoComONome(t *testing.T) {
	pasta := t.TempDir()
	nome := "historico.db.antes-do-dono-20260929-101500.db"
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info(msgMigrou,
		camposDaMigracao(storage.DonoFixo{Dono: "d", Migrou: true, Copia: filepath.Join(pasta, nome)})...)
	if strings.Contains(buf.String(), pasta) {
		t.Errorf("o log da migração traz o caminho %q:\n%s", pasta, buf.String())
	}
	if !strings.Contains(buf.String(), nome) {
		t.Errorf("o log da migração não diz o nome da cópia:\n%s", buf.String())
	}
}

// TestMensagemDeErro_DizOndeEstaOLogSemCaminhoFixo: o aviso dizia "na pasta
// do programa", que deixa de ser verdade instalado; e não pode levar o
// caminho de ninguém.
func TestMensagemDeErro_DizOndeEstaOLogSemCaminhoFixo(t *testing.T) {
	msg := mensagemDeErro(errors.New("x"))
	for _, quer := range []string{"agent.log", "pasta de dados", `%LOCALAPPDATA%\AuroraWhisper`, "modo portátil"} {
		if !strings.Contains(msg, quer) {
			t.Errorf("a mensagem não traz %q: %q", quer, msg)
		}
	}
	for _, proibido := range []string{`C:\`, "Desenvolvimento", "na pasta do programa"} {
		if strings.Contains(msg, proibido) {
			t.Errorf("a mensagem traz %q: %q", proibido, msg)
		}
	}
}

// TestConfigurarLog_NaoGravaOCaminhoCompleto é o caminho de ponta a ponta:
// configurarLog abre o agent.log na pasta de DADOS e o que um erro do sistema
// leva de caminho sai trocado por rótulo. Mexe no estado global do pacote log
// (destino e o destino do pânico), então restaura os dois.
func TestConfigurarLog_NaoGravaOCaminhoCompleto(t *testing.T) {
	p, prog, local := pastasDeTeste(t, modoDeTeste{})
	perfil := filepath.Dir(filepath.Dir(local)) // o "perfil" deste ambiente

	antes := log.Writer()
	t.Cleanup(func() {
		log.SetOutput(antes)
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	})

	fechar := configurarLog(p, perfil)
	erro := &os.PathError{Op: "open", Path: filepath.Join(p.Dados(), "historico.db"), Err: errors.New("Acesso negado.")}
	log.Printf("historico: não abriu: %v (programa em %s)", erro, prog)
	fechar()
	log.SetOutput(antes)

	arquivo := filepath.Join(p.Dados(), nomeLogDoAgent)
	b, err := os.ReadFile(arquivo)
	if err != nil {
		t.Fatalf("o log não foi gravado em %s: %v", arquivo, err)
	}
	texto := string(b)
	for _, proibido := range []string{p.Dados(), prog, perfil} {
		if strings.Contains(texto, proibido) {
			t.Errorf("o agent.log traz o caminho %q:\n%s", proibido, texto)
		}
	}
	for _, quer := range []string{"<dados>", "<programa>", "Acesso negado."} {
		if !strings.Contains(texto, quer) {
			t.Errorf("o agent.log não traz %q:\n%s", quer, texto)
		}
	}
	// O log mora na pasta de dados, e não ao lado do executável.
	if _, err := os.Stat(filepath.Join(prog, nomeLogDoAgent)); err == nil {
		t.Error("o agent.log foi gravado na pasta do programa")
	}
}

// TestConfigurarLog_GiraNaPastaDeDados: o giro de 5 MB acontece na pasta de
// DADOS, onde o agent.log mora. Sem este teste, girar o log na pasta do
// programa passava em tudo: instalado, o agent.log em %LOCALAPPDATA% nunca
// giraria e cresceria sem limite, e o programa, que é só leitura, receberia
// um agent.log.1.
func TestConfigurarLog_GiraNaPastaDeDados(t *testing.T) {
	antes := log.Writer()
	t.Cleanup(func() {
		log.SetOutput(antes)
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	})

	// cheio cria o agent.log do tamanho dado, sem escrever 5 MB de verdade.
	cheio := func(t *testing.T, p pastas.Pastas, tamanho int64) {
		t.Helper()
		f, err := os.Create(filepath.Join(p.Dados(), nomeLogDoAgent))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := f.Truncate(tamanho); err != nil {
			t.Fatal(err)
		}
	}
	// abrirEFechar configura o log e o fecha em seguida, soltando também o destino
	// do pânico: o runtime guarda uma cópia do arquivo, e no Windows ela impediria
	// de apagar a pasta temporária do subteste.
	abrirEFechar := func(p pastas.Pastas) {
		configurarLog(p, "")()
		log.SetOutput(antes)
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	}
	tamanho := func(t *testing.T, caminho string) int64 {
		t.Helper()
		info, err := os.Stat(caminho)
		if err != nil {
			t.Fatalf("esperava %s: %v", caminho, err)
		}
		return info.Size()
	}

	t.Run("no limite gira, em dados", func(t *testing.T) {
		p, prog, _ := pastasDeTeste(t, modoDeTeste{})
		cheio(t, p, limiteDoLog)
		abrirEFechar(p)

		anterior := filepath.Join(p.Dados(), nomeLogDoAgent+".1")
		if got := tamanho(t, anterior); got != limiteDoLog {
			t.Errorf("agent.log.1 tem %d bytes, quero os %d do log girado", got, limiteDoLog)
		}
		if got := tamanho(t, filepath.Join(p.Dados(), nomeLogDoAgent)); got >= limiteDoLog {
			t.Errorf("o agent.log novo tem %d bytes: não foi girado", got)
		}
		// Nada foi criado nem movido na pasta do programa.
		restos, err := filepath.Glob(filepath.Join(prog, "agent.log*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(restos) != 0 {
			t.Errorf("a pasta do programa ganhou %v", restos)
		}
	})

	t.Run("abaixo do limite não gira", func(t *testing.T) {
		p, _, _ := pastasDeTeste(t, modoDeTeste{})
		cheio(t, p, limiteDoLog-1)
		abrirEFechar(p)

		if _, err := os.Stat(filepath.Join(p.Dados(), nomeLogDoAgent+".1")); err == nil {
			t.Error("girou um log abaixo do limite")
		}
		if got := tamanho(t, filepath.Join(p.Dados(), nomeLogDoAgent)); got != limiteDoLog-1 {
			t.Errorf("o agent.log tem %d bytes, quero os %d de antes", got, limiteDoLog-1)
		}
	})

	t.Run("modo portátil: gira ao lado do executável, que é a pasta de dados", func(t *testing.T) {
		p, prog, _ := pastasDeTeste(t, modoDeTeste{marcador: true})
		cheio(t, p, limiteDoLog)
		abrirEFechar(p)

		if got := tamanho(t, filepath.Join(prog, nomeLogDoAgent+".1")); got != limiteDoLog {
			t.Errorf("agent.log.1 tem %d bytes, quero %d", got, limiteDoLog)
		}
	})
}

// TestConfigurarLog_PerfilVaiParaORotulo: o perfil que main passa
// (perfilDoUsuario) é anonimizado também. Se main passasse "", só os caminhos
// da pasta de dados e do programa seriam trocados, e o nome do usuário do
// Windows escaparia numa mensagem de erro sobre outra pasta do perfil.
func TestConfigurarLog_PerfilVaiParaORotulo(t *testing.T) {
	p, _, local := pastasDeTeste(t, modoDeTeste{})
	perfil := filepath.Dir(filepath.Dir(local)) // contém a pasta do programa e a de dados
	fora := filepath.Join(perfil, "Documents", "relatorio.txt")

	antes := log.Writer()
	t.Cleanup(func() {
		log.SetOutput(antes)
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	})
	fechar := configurarLog(p, perfil)
	log.Printf("x: %v", &os.PathError{Op: "open", Path: fora, Err: errors.New("Acesso negado.")})
	fechar()
	log.SetOutput(antes)

	b, err := os.ReadFile(filepath.Join(p.Dados(), nomeLogDoAgent))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), perfil) || !strings.Contains(string(b), "<perfil>") {
		t.Errorf("o perfil não foi trocado por <perfil>:\n%s", b)
	}
}

// TestRegistrarPastas_AvisaDoDicionarioAoLado: instalado, um dicionario.json
// ao lado do executável não é usado, e o log só dizia INFO "nenhum
// encontrado", sem dizer que há um arquivo logo ao lado. Agora avisa, sem o
// caminho. Nos outros modos o arquivo VALE (o portátil o lê dali), e uma pasta
// chamada dicionario.json não é um dicionário.
func TestRegistrarPastas_AvisaDoDicionarioAoLado(t *testing.T) {
	casos := []struct {
		nome         string
		m            modoDeTeste
		pastaNoLugar bool // cria uma PASTA chamada dicionario.json, e não um arquivo
		querAviso    bool
	}{
		{"instalado com o arquivo ao lado do exe", modoDeTeste{dicionario: true}, false, true},
		{"instalado sem nada ao lado do exe", modoDeTeste{}, false, false},
		{"instalado com uma pasta de mesmo nome", modoDeTeste{}, true, false},
		{"portátil usa o arquivo", modoDeTeste{marcador: true, dicionario: true}, false, false},
		{"legado usa o arquivo, só o aviso do legado", modoDeTeste{banco: true, dicionario: true}, false, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			p, prog, local := pastasDeTeste(t, c.m)
			if c.pastaNoLugar {
				if err := os.Mkdir(filepath.Join(prog, nomeDicionario), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var buf bytes.Buffer
			registrarPastas(slog.New(slog.NewTextHandler(&buf, nil)), p)
			saida := buf.String()

			temAviso := strings.Contains(saida, nomeDicionario)
			if temAviso != c.querAviso {
				t.Errorf("aviso do dicionário = %v, quero %v:\n%s", temAviso, c.querAviso, saida)
			}
			if c.querAviso {
				if !strings.Contains(saida, "level=WARN") {
					t.Errorf("o aviso não é WARN:\n%s", saida)
				}
				for _, quer := range []string{"pasta de dados", `%LOCALAPPDATA%`} {
					if !strings.Contains(saida, quer) {
						t.Errorf("o aviso não diz %q:\n%s", quer, saida)
					}
				}
			}
			for _, caminho := range []string{prog, local} {
				if forma, traz := trazOCaminho(saida, caminho); traz {
					t.Errorf("o log traz o caminho %q:\n%s", forma, saida)
				}
			}
		})
	}
}

// TestResolverPastas_ErroNaoMandaProAgentLog: quando as pastas não resolvem,
// não há log em lugar nenhum, e o aviso não pode dizer "os detalhes ficam no
// agent.log". O texto do próprio erro tem de aparecer: é ele que diz o que
// faltou.
func TestResolverPastas_ErroNaoMandaProAgentLog(t *testing.T) {
	_, err := resolverPastas(pastas.Ambiente{
		Executavel: func() (string, error) { return filepath.Join(t.TempDir(), "x.exe"), nil },
		PastaLocal: func() (string, error) { return "", nil }, // LOCALAPPDATA vazio
	})
	if err == nil {
		t.Fatal("LOCALAPPDATA vazio devia falhar")
	}
	if !errors.Is(err, pastas.ErrSemPastaLocal) {
		t.Errorf("o erro perdeu a causa: %v", err)
	}
	msg := mensagemDeErro(err)
	if strings.Contains(msg, "agent.log, na pasta de dados") || strings.Contains(msg, "Os detalhes ficam") {
		t.Errorf("o aviso manda a pessoa a um log que não existe: %q", msg)
	}
	if !strings.Contains(msg, "LOCALAPPDATA") || !strings.Contains(msg, "não há agent.log") {
		t.Errorf("o aviso não diz o que faltou nem que não há log: %q", msg)
	}

	// Um erro que NÃO é das pastas continua mandando para o log.
	if msg := mensagemDeErro(errors.New("modelo faltando")); !strings.Contains(msg, "Os detalhes ficam no agent.log") {
		t.Errorf("o erro comum perdeu a indicação do log: %q", msg)
	}

	// Sem erro, as pastas voltam sem marca de erro.
	p, _, _ := pastasDeTeste(t, modoDeTeste{})
	if _, err := resolverPastas(pastas.Ambiente{
		Executavel: func() (string, error) { return filepath.Join(p.Programa(), "x.exe"), nil },
		PastaLocal: func() (string, error) { return t.TempDir(), nil },
	}); err != nil {
		t.Errorf("resolverPastas com ambiente bom: %v", err)
	}
}

// TestRun_LigaCadaConsumidorAoCaminhoCerto: run() sobe o hook, o microfone e
// a bandeja, então não roda num teste; mas a ligação de cada consumidor ao
// caminho da tabela (caminhosDe) é conferida no código de main.go. Sem isto,
// um consumidor que passasse a abrir o histórico em Programa() (em vez de
// Dados()) não falharia em teste nenhum.
//
// Três travas, nesta ordem de importância:
//  1. cada chamada recebe exatamente o argumento da tabela;
//  2. a tabela (cam) nunca é reatribuída: nem cam.Historico = ..., nem
//     cam = ..., que desviariam um consumidor sem mudar o argumento que a
//     trava 1 lê;
//  3. as pastas resolvem em main com resolverPastas(pastas.Sistema()), o log se
//     configura com as MESMAS pastas e o perfil, e a subida registra o modo.
func TestRun_LigaCadaConsumidorAoCaminhoCerto(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Cada chamada, com o argumento que tem de ser o caminho da tabela.
	quer := []struct {
		chamada string // como o código a escreve: pacote.Funcao ou Funcao
		arg     int
		expr    string
	}{
		{"abrirDicionarioEm", 1, "cam.Dicionario"},
		{"novoAprendizado", 0, "cam.Dicionario"},
		// O histórico com o ciclo do expurgo (F10d): é o que run chama; o
		// abrirHistoricoEm é o mesmo sem devolver o ciclo, e só os testes o usam.
		{"abrirHistoricoComRetencao", 1, "cam.Historico"},
		// As imagens das notas (N10): a pasta da tabela, na pasta de dados.
		{"abrirHistoricoComRetencao", 2, "cam.ImagensDasNotas"},
		{"subirPainel", 2, "cam.Painel"},
		// A nuvem (F6): o modo das MESMAS pastas decide a D7 (o build de
		// desenvolvimento só conecta no portátil) e o alvo do token no
		// Gerenciador; e a instalação é a do historico.db da tabela.
		{"montarNuvem", 0, "prefs.Instalacao()"},
		{"montarNuvem", 2, "pp.Modo()"},
		// O envio inicial (F7c): a cópia fica ao lado do historico.db da
		// tabela, o mesmo que abrirHistoricoEm abre, e a fila é a do mesmo
		// histórico e dono.
		{"montarNuvem", 1, "historicoDaNuvem(hist, dono, cam.Historico)"},
		{"historicoDaNuvem", 2, "cam.Historico"},
		{"subirPainel", 1, "nuvemDoPC.Retorno()"},
		{"model.Resolve", 0, "cam.Programa"},
		// O log do servidor: o arquivo da tabela, com os caminhos de quem usa
		// trocados (o mesmo pp e o mesmo perfil do agent.log). Era um os.Create
		// direto, que gravava a pasta do programa instalado, com o nome do
		// usuário, na primeira linha do whisper-server.log.
		{"abrirLogDoServidor", 0, "cam.LogDoServidor"},
		{"abrirLogDoServidor", 1, "pp"},
		{"abrirLogDoServidor", 2, "perfil"},
		{"abrirASRLocal", 0, "cam"},
		{"abrirASRLocal", 1, "pp"},
		{"abrirASRLocal", 2, "perfilDoUsuario()"},
		// As pastas e o log (trava 3).
		{"resolverPastas", 0, "pastas.Sistema()"},
		{"configurarLog", 0, "pp"},
		{"configurarLog", 1, "perfilDoUsuario()"},
		{"registrarPastas", 1, "pp"},
		{"caminhosDe", 0, "pp"},
	}
	chamadas := map[string]int{} // quantas vezes main.go chama cada uma
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		nome := types.ExprString(c.Fun)
		for _, q := range quer {
			if nome == q.chamada && q.arg < len(c.Args) && types.ExprString(c.Args[q.arg]) != q.expr {
				t.Errorf("%s: main.go chama %s com o argumento %d diferente de %s (é %s)",
					fset.Position(c.Pos()), nome, q.arg, q.expr, types.ExprString(c.Args[q.arg]))
			}
			if nome == q.chamada && q.arg >= len(c.Args) {
				t.Errorf("%s: main.go chama %s sem o argumento %d", fset.Position(c.Pos()), nome, q.arg)
			}
		}
		chamadas[nome]++
		return true
	})
	for _, q := range quer {
		if chamadas[q.chamada] != 1 {
			t.Errorf("main.go tem %d chamada(s) de %s; o teste espera exatamente 1", chamadas[q.chamada], q.chamada)
		}
	}

	// Trava 2: ninguém reatribui a tabela, nem um campo dela. A única
	// definição é cam := caminhosDe(pp).
	definiu := 0
	ast.Inspect(f, func(n ast.Node) bool {
		a, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, l := range a.Lhs {
			if raiz := raizDaExpressao(l); raiz != "cam" {
				continue
			}
			if a.Tok == token.DEFINE && i < len(a.Rhs) && len(a.Lhs) == len(a.Rhs) &&
				types.ExprString(l) == "cam" && types.ExprString(a.Rhs[i]) == "caminhosDe(pp)" {
				definiu++
				continue
			}
			t.Errorf("%s: main.go reatribui a tabela de caminhos (%s): um consumidor pode estar sendo desviado sem mudar o argumento que o teste lê",
				fset.Position(a.Pos()), types.ExprString(l))
		}
		return true
	})
	if definiu != 1 {
		t.Errorf("main.go tem %d definição(ões) de `cam := caminhosDe(pp)`; o teste espera exatamente 1", definiu)
	}

	// O servidor de ASR: o campo ServerPath da config é o da tabela.
	servidor := false
	ast.Inspect(f, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok && types.ExprString(kv.Key) == "ServerPath" {
			servidor = true
			if got := types.ExprString(kv.Value); got != "cam.Servidor" {
				t.Errorf("ServerPath = %s, quero cam.Servidor", got)
			}
		}
		return true
	})
	if !servidor {
		t.Error("main.go não monta o ServerPath")
	}
}

// raizDaExpressao é o identificador mais à esquerda de a, a.b, a.b.c, a[i]...:
// "cam" em cam.Historico. Vazio se a expressão não começa por um nome.
func raizDaExpressao(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		default:
			return ""
		}
	}
}

// TestPerfilDoUsuario: main passa o perfil do Windows a configurarLog para
// que o log o troque por <perfil>. Se perfilDoUsuario devolvesse sempre vazio,
// a troca ficaria desligada sem nenhum outro teste notar (o de configurarLog
// recebe o perfil pronto). Só lê a variável de ambiente, que o teste injeta.
func TestPerfilDoUsuario(t *testing.T) {
	casa := t.TempDir()
	t.Setenv("USERPROFILE", casa) // Windows
	t.Setenv("HOME", casa)        // os outros sistemas
	if got := perfilDoUsuario(); got != casa {
		t.Errorf("perfilDoUsuario() = %q, quero %q", got, casa)
	}

	t.Setenv("USERPROFILE", "")
	t.Setenv("HOME", "")
	if got := perfilDoUsuario(); got != "" {
		t.Errorf("sem perfil, perfilDoUsuario() = %q, quero vazio", got)
	}
}
