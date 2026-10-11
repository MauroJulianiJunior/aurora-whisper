// Command asrstage monta o diretório de execução do Agent: junta o
// binário, o servidor de ASR, as DLLs que ele precisa e o modelo, com os
// nomes que o Agent procura.
//
// # Por que um comando, e não um script
//
// O Agent resolve o modelo e o servidor pelo caminho do PRÓPRIO
// executável (cmd/agent, abrirASRLocal), e não pelo diretório de trabalho —
// porque ele abre por atalho, por autostart ou pelo terminal, e o
// diretório de trabalho não é confiável para achar arquivo de instalação.
//
// Isso significa que rodar o Agent a partir de `go run` ou de `bin/` sem
// preparar o diretório dá um erro de modelo ausente que parece bug e é só
// montagem faltando. Este comando existe para essa montagem ser um passo
// explícito, verificável e igual para todo mundo.
//
// Em Go, e não em .ps1 ou .sh, por dois motivos: roda igual em qualquer
// shell (o terminal aqui é PowerShell, onde `&&` nem existe), e a
// verificação de checksum do modelo é a mesma função que o Agent usa em
// produção — se o artefato montado não for o que foi medido no benchmark,
// isso aparece aqui e não na máquina de quem instalou.
//
// # O marcador do modo portátil
//
// O Agent instalado guarda os dados (histórico, dicionário, logs) em
// %LOCALAPPDATA%\AuroraWhisper, e só lê o programa da pasta do executável
// (ADR-018, internal/agent/pastas). O bin\ montado aqui é o ambiente de
// DESENVOLVIMENTO e continua igual: guarda os dados ao lado do executável,
// e para isso este comando cria, como primeiro passo, o arquivo "portatil"
// em bin\. Sem ele, um Agent de desenvolvimento montado aqui abriria o
// histórico real de quem usa o programa instalado. A garantia é só desta
// montagem: um "go run ./cmd/agent", ou um executável compilado para uma
// pasta sem o marcador, vai para %LOCALAPPDATA%\AuroraWhisper de verdade.
//
// # Duas montagens: o bin\ de desenvolvimento e a pasta do pacote
//
// Sem argumentos, monta o bin\ de desenvolvimento, como sempre foi: com o
// marcador do modo portátil e com o dicionário padrão do repositório.
//
// Com -destino <pasta>, monta OUTRA pasta, e ela é a do pacote do instalador
// (ADR-018, scripts/montar-instalador.ps1): SEM o marcador. O programa
// instalado guarda os dados em %LOCALAPPDATA%, e um marcador na pasta que o
// Setup troca a cada atualização desviaria os dados para dentro dela. Por isso
// o marcador não depende de uma terceira opção: qualquer -destino é o pacote.
// Para uma segunda pasta de desenvolvimento, crie o arquivo "portatil" à mão.
//
// A pasta do pacote NUNCA leva dicionário: -destino implica -sem-dicionario, e
// não há como pedir o contrário. O que está no repositório é o dicionário
// pessoal de quem desenvolve, e não vai para terceiros; e o Agent instalado nem
// o leria dali (os dados dele ficam em %LOCALAPPDATA%), então a cópia só abriria
// a chance de vazar. O Agent instalado cria o seu, vazio, na pasta de dados
// (cmd/agent, garantirDicionario).
//
// Sem -destino, -sem-dicionario ainda vale para o bin\ de desenvolvimento: não
// copia nem confere nenhum dicionário.
//
// A pasta do pacote tem de estar limpa de dados: a montagem recusa uma que já
// tenha o marcador, um historico.db (ou as cópias dele), os logs ou um
// dicionario.json. Apagar o que o Setup nunca deveria levar seria decisão de
// quem chama; recusar é o que a montagem faz.
//
// As origens (thirdparty\whisper\Release e models\quant) continuam relativas
// ao diretório de trabalho, que tem de ser a raiz do repositório.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// dllsNecessarias é o conjunto MÍNIMO que o whisper-server precisa,
// determinado empiricamente: copiando só estas e rodando o servidor, ele
// sobe. SDL2.dll (whisper-stream), llama.dll (whisper-talk-llama) e
// parakeet.dll ficam de fora — pertencem a outras ferramentas do mesmo zip
// e só inflariam o instalador.
//
// As ggml-cpu-*.dll são variantes por microarquitetura, escolhidas em
// runtime: nesta máquina o servidor carregou a alderlake. Todas vão juntas
// porque o instalador não sabe em que CPU vai rodar, e são poucos MB.
var dllsNecessarias = []string{
	"whisper.dll",
	"ggml.dll",
	"ggml-base.dll",
	"ggml-cpu-alderlake.dll",
	"ggml-cpu-cannonlake.dll",
	"ggml-cpu-cascadelake.dll",
	"ggml-cpu-haswell.dll",
	"ggml-cpu-icelake.dll",
	"ggml-cpu-sandybridge.dll",
	"ggml-cpu-skylakex.dll",
	"ggml-cpu-sse42.dll",
	"ggml-cpu-x64.dll",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "asrstage:", err)
		os.Exit(1)
	}
}

func run() error {
	m, err := montagemDeArgumentos(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil // o uso já foi impresso
	}
	if err != nil {
		return err
	}
	return montar(m)
}

// pastaPadrao é o destino sem -destino: o bin\ de desenvolvimento.
const pastaPadrao = "bin"

// montagemDeArgumentos lê a linha de comando (sem o nome do programa) e
// devolve a montagem pedida: o bin\ de desenvolvimento, ou a pasta do pacote
// se há -destino (ver a documentação do pacote).
//
// usoSaida recebe o texto de uso (-h) e a mensagem de um argumento inválido.
// Argumento sobrando é erro: "asrstage bin2" parecia montar bin2 e montava bin\.
func montagemDeArgumentos(args []string, usoSaida io.Writer) (montagem, error) {
	fs := flag.NewFlagSet("asrstage", flag.ContinueOnError)
	fs.SetOutput(usoSaida)
	destino := fs.String("destino", pastaPadrao,
		"pasta a montar. Sem esta opcao, e o bin de desenvolvimento (com o marcador do modo portatil e o dicionario padrao). "+
			"Com ela, e a pasta do pacote do instalador: SEM marcador e SEM dicionario (implica -sem-dicionario), e a pasta tem de estar limpa de dados")
	semDicionario := fs.Bool("sem-dicionario", false,
		"nao copia nem confere dicionario nenhum (o dicionario do repositorio e pessoal e nao vai para terceiros). Com -destino ja e sempre assim")
	if err := fs.Parse(args); err != nil {
		return montagem{}, err
	}
	if fs.NArg() > 0 {
		return montagem{}, fmt.Errorf("argumento inesperado %q: use -destino <pasta> e -sem-dicionario", fs.Arg(0))
	}

	m := montagemDeDesenvolvimento()
	m.semDicionario = *semDicionario

	explicito := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "destino" {
			explicito = true
		}
	})
	if explicito {
		pasta := strings.TrimSpace(*destino)
		if pasta == "" {
			return montagem{}, errors.New("-destino sem pasta")
		}
		m.destino = pasta
		// A pasta do pacote nunca leva o marcador: ver a documentação do pacote.
		m.portatil = false
	}
	return m, nil
}

// montagemDeDesenvolvimento é o bin\ de quem desenvolve: relativo à raiz do
// repositório, com o modelo conferido pela função que o Agent usa.
//
// Esta é a montagem de DESENVOLVIMENTO: o bin\ guarda o programa e os dados
// no mesmo lugar (ADR-018), então leva o marcador do modo portátil. A do
// pacote do instalador (-destino) não o leva.
func montagemDeDesenvolvimento() montagem {
	return montagem{
		origemWhisper:    "thirdparty/whisper/Release",
		origemModelo:     "models/quant/small-q4_0.bin",
		origemDicionario: "testdata/asr-dataset/dicionario.json",
		destino:          pastaPadrao,
		portatil:         true,
		conferirModelo: func(dir string) error {
			_, err := model.Resolve(dir, model.SmallQ4_0)
			return err
		},
		saida: os.Stdout,
	}
}

// montagem é o que montar precisa. Existe para os testes montarem em pastas
// temporárias, com arquivos de mentira, em vez do bin\ de quem desenvolve.
type montagem struct {
	origemWhisper, origemModelo, origemDicionario, destino string

	// portatil cria o marcador do modo portátil em destino (criarMarcador). É a
	// montagem de desenvolvimento; sem ele, destino é a pasta do pacote, que
	// precisa estar limpa de dados (conferirPastaDoPacote).
	portatil bool

	// semDicionario não copia nem confere nenhum dicionário (-sem-dicionario).
	// Na pasta do pacote (portatil falso) vale sempre, qualquer que seja o valor
	// aqui: montar o força (ver a documentação do pacote).
	semDicionario bool

	// conferirModelo confere o modelo montado em destino. Em produção é
	// model.Resolve; os testes não têm os 139 MB.
	conferirModelo func(destino string) error

	saida io.Writer
}

func montar(m montagem) error {
	destino := m.destino
	if err := os.MkdirAll(destino, 0o755); err != nil {
		return fmt.Errorf("criando %s: %w", destino, err)
	}

	// A pasta do pacote não pode ter dado de ninguém, nem o marcador, e nunca
	// leva o dicionário pessoal: confere ANTES de copiar os 139 MB do modelo.
	if !m.portatil {
		m.semDicionario = true
		if err := conferirPastaDoPacote(destino); err != nil {
			return err
		}
	}

	// O marcador vem ANTES de copiar qualquer coisa. Com o Agent aberto, o
	// executável e as DLLs estão travados e a cópia falha — e um bin\ que já
	// tem dados do Agent ficaria sem o marcador. Criá-lo primeiro é o que
	// garante o modo portátil mesmo quando o resto da montagem não termina.
	if m.portatil {
		msg, err := criarMarcador(destino)
		if err != nil {
			return err
		}
		fmt.Fprintln(m.saida, msg)
	}

	// O servidor e as DLLs.
	arquivos := append([]string{"whisper-server.exe"}, dllsNecessarias...)
	var total int64
	for _, nome := range arquivos {
		n, err := copiar(filepath.Join(m.origemWhisper, nome), filepath.Join(destino, nome))
		if err != nil {
			return err
		}
		total += n
	}
	fmt.Fprintf(m.saida, "servidor + %d DLLs: %.1f MB\n", len(dllsNecessarias), float64(total)/(1<<20))

	// O modelo, com o NOME que o Agent procura — que é diferente do nome
	// local, de propósito: o local diz de onde veio, o instalado diz o que
	// é.
	nModelo, err := copiar(m.origemModelo, filepath.Join(destino, model.SmallQ4_0.Arquivo))
	if err != nil {
		return err
	}
	fmt.Fprintf(m.saida, "modelo %s: %.1f MB\n", model.SmallQ4_0.Arquivo, float64(nModelo)/(1<<20))

	// A verificação que dá sentido ao resto: o artefato montado é o MESMO
	// que o benchmark mediu?
	//
	// Usa a função que o Agent usa em produção. Se o checksum não bater
	// aqui, o problema aparece agora e não na máquina de quem instalou —
	// onde o sintoma seria transcrição ruim, e a causa pareceria ser o
	// modelo em vez da montagem.
	if err := m.conferirModelo(destino); err != nil {
		return fmt.Errorf("o modelo montado não confere: %w", err)
	}
	fmt.Fprintf(m.saida, "checksum confere: %s\n", model.SmallQ4_0.SHA256[:16]+"...")

	if m.semDicionario {
		fmt.Fprintln(m.saida, "dicionario: sem dicionario (a pasta do pacote nunca leva o pessoal; no bin, -sem-dicionario); nao copiei nem conferi nenhum. O Agent instalado cria o seu, vazio, na pasta de dados")
	} else {
		msg, err := prepararDicionario(m.origemDicionario, filepath.Join(destino, nomeDoDicionario))
		if err != nil {
			return err
		}
		fmt.Fprintln(m.saida, msg)
	}

	if m.portatil {
		fmt.Fprint(m.saida, proximosPassos(diretorioAtual(), destino))
	} else {
		fmt.Fprintf(m.saida, "\npronto: pasta do pacote em %s (sem o marcador do modo portatil). "+
			"Falta compilar o Agent e construir o painel: scripts/montar-instalador.ps1 faz tudo isso\n", destino)
	}
	return nil
}

// conteudoDoMarcador é o texto do arquivo portatil: quem o encontra ao lado
// do executável e se pergunta o que é, lê aqui. Sem acento, como as
// mensagens deste comando.
const conteudoDoMarcador = `Este arquivo liga o MODO PORTATIL do Aurora Whisper (ADR-018).

Com ele ao lado do executavel, o Agent guarda o historico (historico.db), o
dicionario (dicionario.json) e os logs NESTA pasta, e nao em
%LOCALAPPDATA%\AuroraWhisper, que e onde ficam os dados de um programa
instalado. E o que o asrstage cria em bin\, para o Agent montado aqui (o de
desenvolvimento) nao mexer nos dados de quem usa o programa instalado. Um
executavel fora de uma pasta com este arquivo ou com um historico.db usa
%LOCALAPPDATA%\AuroraWhisper, que sao os dados reais.

Nao apague este arquivo sem copiar historico.db e dicionario.json para
%LOCALAPPDATA%\AuroraWhisper: sem ele, e sem um historico.db aqui, o Agent
passa a usar aquela pasta e abre um historico novo.
`

// criarMarcador cria o arquivo portatil em destino e devolve a linha que diz
// o que fez. Se ja existe, nao o reescreve.
func criarMarcador(destino string) (string, error) {
	caminho := filepath.Join(destino, pastas.Marcador)
	if info, err := os.Stat(caminho); err == nil && !info.IsDir() {
		return fmt.Sprintf("modo portatil: %s ja existe; os dados do Agent ficam em %s", pastas.Marcador, destino), nil
	}
	if err := os.WriteFile(caminho, []byte(conteudoDoMarcador), 0o644); err != nil {
		return "", fmt.Errorf("criando o marcador %s: %w", caminho, err)
	}
	return fmt.Sprintf("modo portatil: criei %s; os dados do Agent (historico, dicionario e logs) ficam em %s, e nao em %%LOCALAPPDATA%%", pastas.Marcador, destino), nil
}

// nomeDoDicionario é o arquivo do dicionário pessoal (cmd/agent, nomeDicionario).
const nomeDoDicionario = "dicionario.json"

// nomeDoExeDoAgent é o executável do Agent (cmd/agent, appName + ".exe"): o que
// a montagem de desenvolvimento manda compilar, e o que o instalador
// (installer/aurora-whisper.iss) e scripts/montar-instalador.ps1 chamam pelo
// mesmo nome. Um teste confere os quatro lugares.
const nomeDoExeDoAgent = "aurora-whisper-agent.exe"

// conferirPastaDoPacote recusa uma pasta de pacote que já tenha dado de alguém,
// um dicionário ou o marcador do modo portátil: o Setup levaria isso para a
// máquina de quem instala. Nunca apaga nada.
//
// O filepath.Glob olha o nome, e não o destino do link: um link simbólico
// quebrado com um desses nomes também é achado.
func conferirPastaDoPacote(destino string) error {
	proibidos := []string{
		pastas.Marcador,
		pastas.BancoDoHistorico + "*", // o banco, o journal e as cópias antes-do-dono
		"agent.log*",
		"whisper-server.log",
		nomeDoDicionario,
	}
	for _, padrao := range proibidos {
		achados, err := filepath.Glob(filepath.Join(destino, padrao))
		if err != nil {
			return fmt.Errorf("olhando %s: %w", destino, err)
		}
		if len(achados) > 0 {
			return fmt.Errorf("a pasta do pacote (%s) ja tem %s: o pacote nao leva dados, logs nem o marcador do modo portatil. "+
				"Nada foi apagado; use uma pasta nova ou apague isso e monte de novo", destino, filepath.Base(achados[0]))
		}
	}
	return nil
}

// prepararDicionario põe o dicionário pessoal em destino e devolve a
// linha que diz o que fez.
//
// Opcional de propósito: o Agent sobe sem ele e só deixa de corrigir termo
// (cmd/agent, abrirDicionario). Mas se existe no repositório e NÃO for
// copiado, o sintoma é pior que ausência — é "configurei o dicionário e ele
// não faz nada".
//
// # O que já está em destino NÃO é sobrescrito
//
// O painel grava ali o que a pessoa ensina (etapa 3, dictionary.Arquivo).
// Copiar o padrão do repositório por cima apagaria tudo o que foi aprendido
// desde a última montagem, em silêncio. O padrão só entra quando não há
// dicionário nenhum. Para voltar ao padrão, a pessoa apaga o arquivo e roda
// de novo.
//
// # Mas o repositório pode ter o que o destino não tem
//
// O dictlearn grava, por padrão, no dicionário do REPOSITÓRIO (a origem
// daqui), e não em bin/. Com o destino preservado, o que ele ensinou lá
// nunca chegaria ao Agent, e nada diria isso. Por isso a montagem compara os
// dois e, se a origem tem termo ou variante que o destino não tem, avisa
// quantos (só contagens) e como trazê-los. Não une os dois sozinha: qual
// dos dicionários vale é decisão de quem usa.
//
// # Destino inválido
//
// Um bin/dicionario.json vazio ou quebrado NÃO é preservado em silêncio: o
// Agent recusa subir com ele. A montagem não o sobrescreve (podia ter algo
// que a pessoa quer salvar), mas falha, e diz como seguir.
func prepararDicionario(origem, destino string) (string, error) {
	if _, err := os.Stat(destino); err == nil {
		return dicionarioQueJaExiste(origem, destino)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("olhando %s: %w", destino, err)
	}

	if _, err := os.Stat(origem); err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("dicionario: nenhum em %s — o Agent vai rodar sem correcao de termos", origem), nil
		}
		return "", fmt.Errorf("olhando %s: %w", origem, err)
	}
	n, err := copiar(origem, destino)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("dicionario: %d bytes (o padrao de %s)", n, origem), nil
}

// dicionarioQueJaExiste é o ramo de prepararDicionario em que o destino já
// existe: confere que ele carrega, o mantém, e avisa do que o padrão tem a
// mais.
func dicionarioQueJaExiste(origem, destino string) (string, error) {
	atual, err := carregarDicionario(destino)
	if err != nil {
		return "", fmt.Errorf("%s existe e NAO carrega (%v); o Agent recusa subir com ele. Nao o sobrescrevi: corrija-o, ou apague-o e rode de novo para voltar ao padrao de %s", destino, err, origem)
	}
	msg := fmt.Sprintf("dicionario: %s ja existe e foi MANTIDO (o que o painel aprendeu fica); para voltar ao padrao de %s, apague-o e rode de novo", destino, origem)

	padrao, err := carregarDicionario(origem)
	if err != nil {
		return msg, nil // sem padrão legível não há o que comparar
	}
	termos, variantes := padrao.Ausentes(atual)
	if termos+variantes > 0 {
		msg += fmt.Sprintf("\n  AVISO: %s tem %d termo(s) novo(s) e %d variante(s) que %s nao tem, e o Agent usa o de bin. "+
			"O dictlearn grava no de %s por padrao. Para o Agent usar o que ele ensinou, ensine de novo com -dict %s, "+
			"ou apague %s (perde o que o painel aprendeu) e rode de novo",
			origem, termos, variantes, destino, origem, destino, destino)
	}
	return msg, nil
}

// carregarDicionario lê o dicionário e o popula num repositório em memória: é
// o que o Agent faz na subida (smartDaSemente, em cmd/agent). Só ler o JSON
// deixava passar arquivo que o Agent recusa, como uma entrada com forma
// canônica vazia.
func carregarDicionario(caminho string) (dictionary.Semente, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return dictionary.Semente{}, err
	}
	defer f.Close()
	s, err := dictionary.CarregarSemente(f)
	if err != nil {
		return dictionary.Semente{}, err
	}
	if err := s.Popular(context.Background(), dictionary.NewInMemoryDictionaryRepository(nil)); err != nil {
		return dictionary.Semente{}, err
	}
	return s, nil
}

// proximosPassos é o que falta depois da montagem: compilar o Agent e
// construir o painel, um comando por linha.
//
// O painel entrou aqui na revisão de 29-set-2026. A mensagem terminava no
// go build, e quem a seguia ao pé da letra subia um Agent cujo "Abrir
// Aurora Whisper" mostrava a página "O painel não foi construído". Um
// comando por linha, e não encadeados com &&, porque o terminal aqui é o
// PowerShell 5.1, onde && nem existe.
func proximosPassos(repo, destino string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\npronto. Falta compilar o Agent e construir o painel em %s/:\n", destino)
	// -H=windowsgui: o Agent é aplicativo de bandeja. Sem isso sai uma
	// build de console, e se o início automático apontar para ela o
	// Windows abre uma janela preta a cada logon — que, fechada, mata o
	// Agent.
	fmt.Fprintf(&b, "  go -C %s build -ldflags \"-H=windowsgui\" -o %s/%s ./cmd/agent\n", repo, destino, nomeDoExeDoAgent)
	// O build do painel vai para bin/painel (web/vite.config.ts), a pasta
	// que o Agent serve ao lado do executável.
	fmt.Fprintf(&b, "  pnpm -C %s/web install\n", repo)
	fmt.Fprintf(&b, "  pnpm -C %s/web build\n", repo)
	return b.String()
}

func diretorioAtual() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return strings.ReplaceAll(wd, `\`, "/")
}

// copiar copia origem para destino, devolvendo quantos bytes foram
// escritos. Erro de origem ausente é reportado com o caminho, porque a
// causa mais provável é o passo de download não ter sido feito.
func copiar(origem, destino string) (int64, error) {
	in, err := os.Open(origem)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%s não existe — falta baixar o whisper.cpp ou gerar o modelo? Ver docs/benchmarks/asr-decisao-2026-09-18.md", origem)
		}
		return 0, fmt.Errorf("abrindo %s: %w", origem, err)
	}
	defer in.Close()

	out, err := os.Create(destino)
	if err != nil {
		return 0, fmt.Errorf("criando %s: %w", destino, err)
	}
	defer out.Close()

	n, err := io.Copy(out, in)
	if err != nil {
		return 0, fmt.Errorf("copiando para %s: %w", destino, err)
	}
	return n, out.Close()
}
