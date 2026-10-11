package pastas

import (
	"bytes"
	"errors"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nenhum teste aqui toca no LOCALAPPDATA de verdade: o Ambiente é injetado
// e toda pasta sai de t.TempDir().

// ambiente monta um Ambiente falso. O contador de chamadas da pasta local
// prova quando o pacote a pediu ao sistema.
type ambiente struct {
	exe        string
	local      string
	errExe     error
	errLocal   error
	chamadasLo int
}

func (a *ambiente) Ambiente() Ambiente {
	return Ambiente{
		Executavel: func() (string, error) { return a.exe, a.errExe },
		PastaLocal: func() (string, error) { a.chamadasLo++; return a.local, a.errLocal },
	}
}

// novo devolve um ambiente com a pasta do executável criada (e o "exe" nela)
// e uma pasta local vazia, as duas em t.TempDir().
func novo(t *testing.T) (*ambiente, string) {
	t.Helper()
	raiz := t.TempDir()
	prog := filepath.Join(raiz, "programa")
	if err := os.MkdirAll(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	return &ambiente{
		exe:   filepath.Join(prog, "aurora-whisper-agent.exe"),
		local: filepath.Join(raiz, "local"),
	}, prog
}

func criar(t *testing.T, caminho string) {
	t.Helper()
	if err := os.WriteFile(caminho, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func existeDir(caminho string) bool {
	info, err := os.Stat(caminho)
	return err == nil && info.IsDir()
}

// Sem marcador e sem banco: a instalação normal. Os dados vão para a pasta
// local, numa pasta própria, que o pacote cria; o programa continua onde o
// executável está.
func TestResolver_SemMarcadorUsaAPastaLocal(t *testing.T) {
	a, prog := novo(t)

	p, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatalf("Resolver: %v", err)
	}
	if p.Modo() != ModoLocalAppData {
		t.Errorf("Modo = %q, quero %q", p.Modo(), ModoLocalAppData)
	}
	if p.Programa() != prog {
		t.Errorf("Programa = %q, quero %q", p.Programa(), prog)
	}
	if quer := filepath.Join(a.local, NomeDaPastaDeDados); p.Dados() != quer {
		t.Errorf("Dados = %q, quero %q", p.Dados(), quer)
	}
	if !existeDir(p.Dados()) {
		t.Errorf("a pasta de dados %q não foi criada", p.Dados())
	}
	if p.Dados() == p.Programa() {
		t.Error("programa e dados são a mesma pasta fora do modo portátil")
	}
}

// O marcador liga o modo portátil: os dados são a pasta do executável, e a
// pasta local nem é pedida ao sistema (um executável portátil roda sem
// LOCALAPPDATA, e nada é criado nela).
func TestResolver_MarcadorLigaOModoPortatil(t *testing.T) {
	a, prog := novo(t)
	criar(t, filepath.Join(prog, Marcador))

	p, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatalf("Resolver: %v", err)
	}
	if p.Modo() != ModoPortatil {
		t.Errorf("Modo = %q, quero %q", p.Modo(), ModoPortatil)
	}
	if p.Dados() != prog || p.Programa() != prog {
		t.Errorf("Dados = %q, Programa = %q; os dois deviam ser %q", p.Dados(), p.Programa(), prog)
	}
	if a.chamadasLo != 0 {
		t.Errorf("a pasta local foi pedida %d vez(es) com o marcador ligado", a.chamadasLo)
	}
	if existeDir(a.local) {
		t.Error("o modo portátil criou a pasta local")
	}
}

// O marcador vence, mesmo com um historico.db ao lado: o modo é o explícito.
func TestResolver_MarcadorVenceOBancoLegado(t *testing.T) {
	a, prog := novo(t)
	criar(t, filepath.Join(prog, Marcador))
	criar(t, filepath.Join(prog, BancoDoHistorico))

	p, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatal(err)
	}
	if p.Modo() != ModoPortatil {
		t.Errorf("Modo = %q, quero %q (o marcador é o modo explícito)", p.Modo(), ModoPortatil)
	}
}

// A regra de segurança: quem já tem um historico.db ao lado do executável e
// roda um Agent novo sem o marcador NÃO pode "perder" os dados. Sem esta
// regra o Agent abriria um banco novo na pasta local, com outro dono fixo e
// o painel vazio.
func TestResolver_BancoAoLadoSemMarcadorVaiParaOPortatilLegado(t *testing.T) {
	a, prog := novo(t)
	criar(t, filepath.Join(prog, BancoDoHistorico))

	p, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatalf("Resolver: %v", err)
	}
	if p.Modo() != ModoPortatilLegado {
		t.Errorf("Modo = %q, quero %q", p.Modo(), ModoPortatilLegado)
	}
	if p.Dados() != prog {
		t.Errorf("Dados = %q; os dados de quem já usa ficam em %q", p.Dados(), prog)
	}
	if a.chamadasLo != 0 || existeDir(a.local) {
		t.Error("o modo legado tocou na pasta local: abriria um banco novo ao lado do antigo")
	}
}

// Só o historico.db é gatilho. O dicionário uma pessoa pode pôr à mão numa
// pasta instalada, e aí os dados cairiam dentro da pasta que o instalador
// troca na atualização.
func TestResolver_DicionarioSozinhoNaoLigaOPortatil(t *testing.T) {
	a, prog := novo(t)
	criar(t, filepath.Join(prog, "dicionario.json"))

	p, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatal(err)
	}
	if p.Modo() != ModoLocalAppData {
		t.Errorf("Modo = %q: um dicionario.json ao lado do executável não pode desviar os dados", p.Modo())
	}
}

// Uma PASTA chamada "portatil" (ou "historico.db") não é o marcador nem o
// banco.
func TestResolver_PastaComONomeDoMarcadorNaoLiga(t *testing.T) {
	for _, nome := range []string{Marcador, BancoDoHistorico} {
		t.Run(nome, func(t *testing.T) {
			a, prog := novo(t)
			if err := os.Mkdir(filepath.Join(prog, nome), 0o755); err != nil {
				t.Fatal(err)
			}
			p, err := Resolver(a.Ambiente())
			if err != nil {
				t.Fatal(err)
			}
			if p.Modo() != ModoLocalAppData {
				t.Errorf("uma pasta %q ligou o modo %q", nome, p.Modo())
			}
		})
	}
}

// Caminho com acento e com espaço: o nome do perfil no Windows costuma ter
// um dos dois. A pasta local ainda não existe, e o MkdirAll a cria inteira.
func TestResolver_CaminhoComAcentoEEspaco(t *testing.T) {
	raiz := t.TempDir()
	prog := filepath.Join(raiz, "José da Conceição", "Aurora Whisper")
	local := filepath.Join(raiz, "Usuário Ângelo", "AppData", "Local")
	if err := os.MkdirAll(prog, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &ambiente{exe: filepath.Join(prog, "aurora-whisper-agent.exe"), local: local}

	p, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatalf("Resolver: %v", err)
	}
	if p.Programa() != prog {
		t.Errorf("Programa = %q, quero %q", p.Programa(), prog)
	}
	if quer := filepath.Join(local, NomeDaPastaDeDados); p.Dados() != quer || !existeDir(quer) {
		t.Errorf("Dados = %q (existe: %v), quero %q criada", p.Dados(), existeDir(p.Dados()), quer)
	}
}

// Resolver de novo devolve o mesmo, e não falha por a pasta já existir; o
// que já está nela não é tocado.
func TestResolver_Idempotente(t *testing.T) {
	a, _ := novo(t)
	p1, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatal(err)
	}
	dado := filepath.Join(p1.Dados(), BancoDoHistorico)
	criar(t, dado)

	p2, err := Resolver(a.Ambiente())
	if err != nil {
		t.Fatalf("segunda chamada: %v", err)
	}
	if p1 != p2 {
		t.Errorf("duas chamadas deram %+v e %+v", p1, p2)
	}
	if b, err := os.ReadFile(dado); err != nil || string(b) != "x" {
		t.Errorf("o dado que estava na pasta mudou: %q, %v", b, err)
	}
}

// LOCALAPPDATA vazio, relativo ou com erro: sem ele não há onde gravar, e o
// pacote não inventa outro lugar (a pasta do programa pode nem gravar). A
// mensagem é a do erro próprio.
func TestResolver_SemPastaLocal(t *testing.T) {
	casos := []struct {
		nome  string
		local string
		err   error
	}{
		{"vazio", "", nil},
		{"relativo", filepath.Join("AppData", "Local"), nil},
		{"erro do sistema", "", errors.New("%LocalAppData% is not defined")},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			a, prog := novo(t)
			a.local, a.errLocal = c.local, c.err

			_, err := Resolver(a.Ambiente())
			if !errors.Is(err, ErrSemPastaLocal) {
				t.Fatalf("err = %v, quero ErrSemPastaLocal", err)
			}
			// E nada foi criado ao lado do executável no lugar dela.
			if existeDir(filepath.Join(prog, NomeDaPastaDeDados)) {
				t.Error("criou a pasta de dados ao lado do executável")
			}
		})
	}
}

// Com o modo portátil, a falta do LOCALAPPDATA não atrapalha: a pasta local
// nem é usada.
func TestResolver_PortatilFuncionaSemPastaLocal(t *testing.T) {
	for _, arquivo := range []string{Marcador, BancoDoHistorico} {
		t.Run(arquivo, func(t *testing.T) {
			a, prog := novo(t)
			criar(t, filepath.Join(prog, arquivo))
			a.local, a.errLocal = "", errors.New("%LocalAppData% is not defined")

			p, err := Resolver(a.Ambiente())
			if err != nil {
				t.Fatalf("Resolver: %v", err)
			}
			if p.Dados() != prog {
				t.Errorf("Dados = %q, quero %q", p.Dados(), prog)
			}
		})
	}
}

func TestResolver_SemExecutavel(t *testing.T) {
	t.Run("erro do sistema", func(t *testing.T) {
		a, _ := novo(t)
		a.errExe = errors.New("boom")
		if _, err := Resolver(a.Ambiente()); !errors.Is(err, ErrSemExecutavel) {
			t.Fatalf("err = %v, quero ErrSemExecutavel", err)
		}
	})
	t.Run("caminho relativo", func(t *testing.T) {
		a, _ := novo(t)
		a.exe = "aurora-whisper-agent.exe" // dependeria do diretório de trabalho
		if _, err := Resolver(a.Ambiente()); !errors.Is(err, ErrSemExecutavel) {
			t.Fatalf("err = %v, quero ErrSemExecutavel", err)
		}
	})
}

// O MkdirAll que falha (aqui: já existe um ARQUIVO com o nome da pasta de
// dados) vira erro, e não pânico nem pasta em outro lugar.
func TestResolver_MkdirAllQueFalha(t *testing.T) {
	a, _ := novo(t)
	if err := os.MkdirAll(a.local, 0o755); err != nil {
		t.Fatal(err)
	}
	criar(t, filepath.Join(a.local, NomeDaPastaDeDados))

	if _, err := Resolver(a.Ambiente()); err == nil {
		t.Fatal("Resolver não falhou com um arquivo no lugar da pasta de dados")
	}
}

// Sem valor padrão: um Ambiente com função faltando é erro, e nunca "use o
// real". Um teste que esquecesse a pasta local gravaria no LOCALAPPDATA de
// quem roda a suíte.
func TestResolver_AmbienteIncompletoNaoCaiNoSistema(t *testing.T) {
	a, _ := novo(t)
	amb := a.Ambiente()

	semLocal := amb
	semLocal.PastaLocal = nil
	if _, err := Resolver(semLocal); !errors.Is(err, ErrAmbienteIncompleto) {
		t.Errorf("sem PastaLocal: err = %v", err)
	}
	semExe := amb
	semExe.Executavel = nil
	if _, err := Resolver(semExe); !errors.Is(err, ErrAmbienteIncompleto) {
		t.Errorf("sem Executavel: err = %v", err)
	}
	if _, err := Resolver(Ambiente{}); !errors.Is(err, ErrAmbienteIncompleto) {
		t.Errorf("Ambiente vazio: err = %v", err)
	}
}

// Sistema liga as funções reais, mas não as chama aqui.
func TestSistema_LigaOsDoisPontos(t *testing.T) {
	s := Sistema()
	if s.Executavel == nil || s.PastaLocal == nil {
		t.Fatal("Sistema() deixou uma função nula")
	}
}

// Anonimizar troca, em cada linha, os caminhos de quem usa. O teste usa
// caminhos no formato do Windows escritos à mão (e não filepath.Join), para
// valer igual no Linux.
func TestAnonimizar(t *testing.T) {
	const (
		dados    = `C:\Users\José Maria\AppData\Local\AuroraWhisper`
		programa = `C:\Users\José Maria\AppData\Local\Programs\Aurora Whisper`
		perfil   = `C:\Users\José Maria`
	)
	p := Pastas{programa: programa, dados: dados, modo: ModoLocalAppData}

	casos := []struct{ nome, entra, quer string }{
		{"dados", `abri ` + dados + `\historico.db`, `abri <dados>\historico.db`},
		{"programa", `modelo em ` + programa + `\ggml.bin`, `modelo em <programa>\ggml.bin`},
		{"perfil", `home ` + perfil + `\Documents`, `home <perfil>\Documents`},
		{"a mais específica vence o perfil", dados + ` e ` + perfil, `<dados> e <perfil>`},
		{"erro do sistema", `open ` + dados + `\agent.log: Acesso negado.`, `open <dados>\agent.log: Acesso negado.`},
		{"barra dobrada, como o slog entre aspas escreve", `arquivo="` + strings.ReplaceAll(dados, `\`, `\\`) + `\\x"`, `arquivo="<dados>\\x"`},
		{"barra normal", `arquivo=` + strings.ReplaceAll(programa, `\`, `/`) + `/x`, `arquivo=<programa>/x`},
		{"sem caminho nenhum", `historico: banco pronto dados=localappdata`, `historico: banco pronto dados=localappdata`},
		{"outro usuário não é trocado", `C:\Users\Outra Pessoa\x`, `C:\Users\Outra Pessoa\x`},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var saida bytes.Buffer
			w := Anonimizar(&saida, p, perfil)
			n, err := w.Write([]byte(c.entra))
			if err != nil {
				t.Fatal(err)
			}
			if n != len(c.entra) {
				t.Errorf("Write devolveu %d; o contrato é o tamanho que entrou (%d)", n, len(c.entra))
			}
			if saida.String() != c.quer {
				t.Errorf("saiu %q, quero %q", saida.String(), c.quer)
			}
		})
	}
}

// No modo portátil programa e dados são a mesma pasta: o rótulo é um só, e
// a troca não quebra.
func TestAnonimizar_PortatilTemPastasIguais(t *testing.T) {
	const pasta = `C:\Users\Maria\dev\bin`
	p := Pastas{programa: pasta, dados: pasta, modo: ModoPortatil}
	var saida bytes.Buffer
	if _, err := Anonimizar(&saida, p, `C:\Users\Maria`).Write([]byte(pasta + `\agent.log`)); err != nil {
		t.Fatal(err)
	}
	if got := saida.String(); got != `<dados>\agent.log` {
		t.Errorf("saiu %q", got)
	}
}

// Caminho curto demais não vira regra: "C:\" trocaria texto que não é caminho.
func TestAnonimizar_CaminhoCurtoNaoTroca(t *testing.T) {
	p := Pastas{programa: `C:\`, dados: "", modo: ModoPortatil}
	var saida bytes.Buffer
	w := Anonimizar(&saida, p, "")
	if w != &saida {
		t.Fatal("sem caminho utilizável, devia devolver o próprio writer")
	}
}

// Pelo log/slog de verdade: o que o Agent usa é o pacote log, que recebe o
// slog.Default() — e um erro com caminho passa pelo Anonimizar.
func TestAnonimizar_PeloLogEPeloSlog(t *testing.T) {
	const (
		dados  = `C:\Users\Maria\AppData\Local\AuroraWhisper`
		perfil = `C:\Users\Maria`
	)
	p := Pastas{programa: `C:\Users\Maria\AppData\Local\Programs\Aurora Whisper`, dados: dados, modo: ModoLocalAppData}
	erro := errors.New(`open ` + dados + `\historico.db: Acesso negado.`)

	var saida bytes.Buffer
	w := Anonimizar(&saida, p, perfil)

	log.New(w, "", 0).Printf("agent: %v", erro)
	slog.New(slog.NewTextHandler(w, nil)).Warn("historico: não abriu", "err", erro, "arquivo", dados+`\x`)

	texto := saida.String()
	for _, proibido := range []string{`Maria`, `C:\Users`, `C:\\Users`} {
		if strings.Contains(texto, proibido) {
			t.Errorf("a saída ainda tem %q:\n%s", proibido, texto)
		}
	}
	if !strings.Contains(texto, "<dados>") {
		t.Errorf("a saída não tem o rótulo:\n%s", texto)
	}
}
