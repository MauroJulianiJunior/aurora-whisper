// Package pastas decide onde o Windows Agent LÊ o que é do programa e onde
// GRAVA o que é de quem usa.
//
// # Duas pastas, dois papéis (ADR-018)
//
// Programa() é a pasta do executável: o modelo, o whisper-server e as DLLs,
// o painel. Só se lê dali. Dados() é onde o Agent grava: o histórico (com as
// preferências e o dono fixo), o dicionário, os logs.
//
// Instalado, o programa fica em %LOCALAPPDATA%\Programs\Aurora Whisper e os
// dados em %LOCALAPPDATA%\AuroraWhisper, uma pasta que só a conta de quem usa
// e os administradores leem. Até aqui o Agent assumia tudo ao lado do .exe,
// para ler e para gravar, e cinco lugares repetiam filepath.Dir(os.Executable()):
// isso só funciona numa pasta onde quem usa escreve (o bin\ de desenvolvimento)
// e deixa o histórico legível por qualquer conta local.
//
// # O modo portátil
//
// O bin\ de desenvolvimento continua igual: programa e dados no mesmo lugar.
// Dados() é a própria pasta do executável quando:
//
//  1. existe o arquivo Marcador ("portatil") ao lado do executável — quem o cria
//     é quem monta (cmd/asrstage) ou a pessoa, nunca o Agent; ou
//  2. não existe o marcador, mas existe um historico.db ao lado do executável
//     (ModoPortatilLegado). É a rede de segurança de quem já tem dados em bin\ e
//     roda um Agent novo sem ter rodado o asrstage: sem ela o Agent abriria um
//     banco novo em %LOCALAPPDATA%, com OUTRO dono fixo (o dono mora dentro do
//     banco) e o painel vazio, e o histórico real ficaria parado em bin\.
//
// Só o historico.db serve de gatilho, e não o dicionario.json: o banco só existe
// ali se um Agent rodou ali; o dicionário uma pessoa pode pôr à mão numa pasta
// instalada, e aí os dados cairiam dentro da pasta que o instalador troca na
// atualização. Ver a ADR-018, "Modo portátil".
//
// # O que este pacote NÃO faz
//
// Não escolhe nomes de arquivo (cmd/agent junta o caminho de cada um) e não
// cria o marcador. Não lê o ambiente do processo sozinho: tudo o que pede ao
// sistema entra por Ambiente, e não há valor padrão. Um teste que esquecesse de
// injetar a pasta local gravaria no LOCALAPPDATA de verdade, então a falta de
// uma função é erro, e não "use o real". Só Sistema() liga o real, e só o
// binário do Agent a chama.
package pastas

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// Marcador é o nome do arquivo que liga o modo portátil quando está ao
	// lado do executável. Sem extensão, de propósito: não é um arquivo de
	// ninguém, é uma chave.
	Marcador = "portatil"

	// NomeDaPastaDeDados é a pasta dentro do LOCALAPPDATA.
	NomeDaPastaDeDados = "AuroraWhisper"

	// BancoDoHistorico é o arquivo do histórico. Mora aqui, e não só em
	// cmd/agent, porque a presença dele ao lado do executável é o gatilho do
	// modo portátil legado.
	BancoDoHistorico = "historico.db"
)

// Modo diz por que Dados() é onde é. É o que o log registra (`dados=...`),
// no lugar do caminho: o caminho leva o nome de usuário do Windows.
type Modo string

const (
	// ModoPortatil: há o arquivo Marcador ao lado do executável.
	ModoPortatil Modo = "portatil"
	// ModoPortatilLegado: não há o marcador, mas há um historico.db ao lado
	// do executável. O Agent o trata como portátil e avisa, para nunca
	// "perder" os dados de quem já usa.
	ModoPortatilLegado Modo = "portatil-legado"
	// ModoLocalAppData: a instalação normal.
	ModoLocalAppData Modo = "localappdata"
)

var (
	// ErrSemExecutavel: o sistema não disse onde o executável está.
	ErrSemExecutavel = errors.New("pastas: não localizei o executável")
	// ErrSemPastaLocal: sem o LOCALAPPDATA não há onde gravar os dados (e o
	// modo portátil não está ligado). Não cai para a pasta do programa em
	// silêncio: num Program Files ela nem grava, e numa pasta gravável
	// dividiria os dados em dois lugares quando o LOCALAPPDATA voltasse.
	ErrSemPastaLocal = errors.New("pastas: não localizei a pasta de dados do usuário (LOCALAPPDATA)")
	// ErrAmbienteIncompleto: faltou injetar uma das funções do Ambiente.
	ErrAmbienteIncompleto = errors.New("pastas: Ambiente sem Executavel ou PastaLocal")
)

// Ambiente é o que o pacote pede ao sistema.
type Ambiente struct {
	// Executavel devolve o caminho do executável (os.Executable).
	Executavel func() (string, error)
	// PastaLocal devolve a pasta que contém os dados de cada app do usuário:
	// no Windows, o LOCALAPPDATA (os.UserCacheDir). Só é chamada no modo
	// localappdata: um executável portátil roda sem ela.
	PastaLocal func() (string, error)
}

// Sistema é o Ambiente de verdade. Só o binário do Agent o usa.
func Sistema() Ambiente {
	return Ambiente{Executavel: os.Executable, PastaLocal: os.UserCacheDir}
}

// Pastas é o resultado de Resolver. O valor zero não serve para nada: sempre
// vem de Resolver.
type Pastas struct {
	programa string
	dados    string
	modo     Modo
}

// Programa é a pasta do executável: onde estão o modelo, o servidor de ASR,
// as DLLs e o painel. Só leitura.
func (p Pastas) Programa() string { return p.programa }

// Dados é onde o Agent grava: o histórico, o dicionário e os logs. Já existe
// (Resolver a cria).
func (p Pastas) Dados() string { return p.dados }

// Modo diz por que Dados() é onde é.
func (p Pastas) Modo() Modo { return p.modo }

// Resolver decide as duas pastas e garante que a de dados existe
// (os.MkdirAll; no modo portátil ela é a do executável e já existe).
//
// Só pede a pasta local ao sistema quando vai usá-la.
func Resolver(amb Ambiente) (Pastas, error) {
	if amb.Executavel == nil || amb.PastaLocal == nil {
		return Pastas{}, ErrAmbienteIncompleto
	}
	exe, err := amb.Executavel()
	if err != nil {
		return Pastas{}, fmt.Errorf("%w: %v", ErrSemExecutavel, err)
	}
	if !filepath.IsAbs(exe) {
		return Pastas{}, fmt.Errorf("%w: o caminho não é absoluto", ErrSemExecutavel)
	}
	programa := filepath.Dir(exe)

	modo := ModoLocalAppData
	switch {
	case existeArquivo(filepath.Join(programa, Marcador)):
		modo = ModoPortatil
	case existeArquivo(filepath.Join(programa, BancoDoHistorico)):
		modo = ModoPortatilLegado
	}

	dados := programa
	if modo == ModoLocalAppData {
		local, err := amb.PastaLocal()
		if err != nil {
			return Pastas{}, fmt.Errorf("%w: %v", ErrSemPastaLocal, err)
		}
		// Vazio ou relativo depende do diretório de trabalho de quem abriu o
		// Agent (um atalho, o autostart): gravaria o histórico em lugares
		// diferentes a cada abertura.
		if local == "" || !filepath.IsAbs(local) {
			return Pastas{}, fmt.Errorf("%w: valor vazio ou relativo", ErrSemPastaLocal)
		}
		dados = filepath.Join(local, NomeDaPastaDeDados)
	}

	if err := os.MkdirAll(dados, 0o700); err != nil {
		return Pastas{}, fmt.Errorf("pastas: criando a pasta de dados: %w", err)
	}
	return Pastas{programa: programa, dados: dados, modo: modo}, nil
}

// existeArquivo: existe e não é pasta. Uma pasta chamada "portatil" ou
// "historico.db" não liga nada.
func existeArquivo(caminho string) bool {
	info, err := os.Stat(caminho)
	return err == nil && !info.IsDir()
}

// Anonimizar devolve um io.Writer que, em cada escrita, troca por rótulos os
// caminhos que levam o nome de quem usa, antes de repassar a w: a pasta de
// dados por <dados>, a do programa por <programa> e a do perfil do Windows
// (perfil, normalmente %USERPROFILE%) por <perfil>.
//
// Existe porque o log é o que a pessoa cola num issue, e uma mensagem de erro
// do sistema traz o caminho inteiro ("open C:\Users\maria\...: acesso
// negado"). Tirar o caminho das linhas que nós escrevemos não basta: o
// caminho também chega dentro do erro.
//
// Cada caminho é trocado na forma com barra invertida, na forma com a barra
// dobrada (é assim que o log/slog entre aspas escreve) e na forma com barra
// normal. Quando o caminho tem acento (e nada fora do Latin-1), também na
// codificação ANSI (Latin-1 e Windows-1252 coincidem nessa faixa): um processo
// que escreve na página de código do Windows, e não em UTF-8, escreve "José"
// com um byte só, e a forma em UTF-8 não o acharia. A comparação é exata,
// inclusive na caixa das letras.
//
// Vale para quem escreve uma linha inteira por chamada de Write, como o pacote
// log: um caminho partido entre duas escritas não é achado. Para a saída de
// outro processo, que chega em pedaços quaisquer, use AnonimizarLinhas.
func Anonimizar(w io.Writer, p Pastas, perfil string) io.Writer {
	// A ordem importa: na mesma posição, vence o que vier primeiro, e o perfil
	// é prefixo das outras duas pastas.
	var pares []string
	visto := map[string]bool{}
	for _, c := range []struct{ caminho, rotulo string }{
		{p.dados, "<dados>"},
		{p.programa, "<programa>"},
		{perfil, "<perfil>"},
	} {
		// Menos de 4 caracteres ("C:\") trocaria texto que não é caminho.
		if len(c.caminho) < 4 {
			continue
		}
		for _, forma := range formasDoCaminho(c.caminho) {
			if !visto[forma] {
				visto[forma] = true
				pares = append(pares, forma, c.rotulo)
			}
		}
	}
	if len(pares) == 0 {
		return w
	}
	return &anonimo{w: w, r: strings.NewReplacer(pares...)}
}

func formasDoCaminho(c string) []string {
	formas := []string{
		c,
		strings.ReplaceAll(c, `\`, `\\`),
		strings.ReplaceAll(c, `\`, `/`),
	}
	// Só com o que o UTF-8 e o ANSI escrevem de jeitos diferentes: um caminho
	// só de ASCII é o mesmo nos dois, e duplicá-lo é ruído.
	if ansi, ok := emLatin1(c); ok && ansi != c {
		formas = append(formas,
			ansi,
			strings.ReplaceAll(ansi, `\`, `\\`),
			strings.ReplaceAll(ansi, `\`, `/`),
		)
	}
	return formas
}

// emLatin1 reescreve s com um byte por caractere (ISO-8859-1, que na faixa
// U+00A0 a U+00FF é igual ao Windows-1252). Falso se s tem algo que não cabe
// ali, ou que o Windows-1252 escreve diferente do Latin-1 (U+0080 a U+009F:
// o euro, as aspas curvas): melhor não gerar uma forma do que gerar errada.
func emLatin1(s string) (string, bool) {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF || (r >= 0x80 && r < 0xA0) {
			return "", false
		}
		b = append(b, byte(r))
	}
	return string(b), true
}

type anonimo struct {
	w io.Writer
	r *strings.Replacer
}

func (a *anonimo) Write(b []byte) (int, error) {
	if _, err := io.WriteString(a.w, a.r.Replace(string(b))); err != nil {
		return 0, err
	}
	// O tamanho que o chamador passou, e não o que foi escrito: o contrato do
	// io.Writer é sobre o que entrou.
	return len(b), nil
}

// maxLinha é quanto AnonimizarLinhas guarda à espera de uma quebra de linha.
// Passou disso, entrega o que tem: uma saída sem quebra nenhuma não pode
// crescer sem limite na memória.
const maxLinha = 64 << 10

// AnonimizarLinhas é o Anonimizar para a saída de OUTRO processo (o
// whisper-server): ela chega em pedaços quaisquer, e um caminho pode vir
// partido entre dois Write. Junta o que recebe até a quebra de linha e só
// então entrega ao Anonimizar, que sempre vê linhas inteiras.
//
// Por que existe. A linha que o whisper.cpp imprime com a pasta de onde
// carregou a biblioteca (load_backend: loaded CPU backend from <pasta do
// executável>\ggml-cpu-*.dll) é do servidor, e a pasta do programa instalado
// leva o nome de usuário do Windows. Passar o modelo pelo nome (ADR-018, F2)
// não a muda: quem a imprime é o servidor, de onde ele mesmo está.
//
// O Close entrega o resto que ficou sem quebra de linha. NÃO fecha w: quem o
// abriu o fecha, depois. Depois do Close, Write devolve os.ErrClosed. É seguro
// para escritas concorrentes (o os/exec escreve o stdout e o stderr de um filho
// com um único Writer, mas só serializa se os dois forem o mesmo valor).
//
// Passada a maxLinha sem quebra de linha, entrega tudo o que tem, e um caminho
// partido nesse ponto não é achado: um limite assim só se atinge com uma saída
// que não é de linhas, e o whisper-server não escreve uma.
func AnonimizarLinhas(w io.Writer, p Pastas, perfil string) io.WriteCloser {
	return &porLinha{w: Anonimizar(w, p, perfil)}
}

type porLinha struct {
	mu       sync.Mutex
	w        io.Writer
	pendente []byte
	fechado  bool
}

func (l *porLinha) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fechado {
		return 0, os.ErrClosed
	}
	l.pendente = append(l.pendente, b...)
	// Entrega até a ÚLTIMA quebra; o que vem depois espera a próxima escrita.
	if i := bytes.LastIndexByte(l.pendente, '\n'); i >= 0 {
		if err := l.entregar(i + 1); err != nil {
			return 0, err
		}
	}
	if len(l.pendente) >= maxLinha {
		if err := l.entregar(len(l.pendente)); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// entregar passa ao Anonimizar os n primeiros bytes pendentes e os tira da
// fila, mesmo se a escrita falhar: repeti-los na próxima escrita duplicaria a
// linha no arquivo.
func (l *porLinha) entregar(n int) error {
	// Escreve ANTES de deslocar o resto: o deslocamento reaproveita o mesmo
	// vetor e sobrescreveria o bloco.
	_, err := l.w.Write(l.pendente[:n])
	l.pendente = append(l.pendente[:0], l.pendente[n:]...)
	return err
}

func (l *porLinha) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Idempotente sem teste à parte: o primeiro Close esvazia a fila (mesmo se a
	// escrita falhar), e o segundo não tem o que entregar.
	l.fechado = true
	if len(l.pendente) == 0 {
		return nil
	}
	return l.entregar(len(l.pendente))
}
