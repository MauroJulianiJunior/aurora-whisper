package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
)

// Nomes dos arquivos do Agent. O que é do PROGRAMA se lê da pasta do
// programa; o que o Agent GRAVA mora na pasta de dados (ADR-018).
const (
	nomeServidor      = "whisper-server.exe" // programa
	nomePainel        = "painel"             // programa
	nomeLogDoAgent    = "agent.log"          // dados
	nomeLogDoServidor = "whisper-server.log" // dados
)

// caminhosDoAgent é onde cada arquivo do Agent mora. É o ÚNICO lugar que
// junta um nome de arquivo a uma pasta: quem abre o histórico, o dicionário,
// o modelo ou o painel recebe o caminho daqui. Assim o que vai para o
// programa e o que vai para os dados fica numa tabela só, que um teste lê
// (caminhos_test.go).
//
// Antes, cinco lugares repetiam a conta do diretório do executável e gravavam
// o histórico, o dicionário e os logs ao lado do .exe — o que não funciona
// numa pasta só de leitura, e deixava o histórico legível por qualquer conta
// local (ADR-018).
type caminhosDoAgent struct {
	// Do PROGRAMA: só leitura.
	Programa string // a pasta; o modelo é procurado nela (model.Resolve)
	Servidor string // whisper-server.exe; as DLLs ficam ao lado dele
	Painel   string // a pasta painel\

	// Dos DADOS: o que o Agent grava.
	Historico     string // historico.db; as cópias antes-do-dono ficam ao lado
	Dicionario    string // dicionario.json, inclusive o aprendido no painel
	LogDoAgent    string
	LogDoServidor string
}

func caminhosDe(p pastas.Pastas) caminhosDoAgent {
	return caminhosDoAgent{
		Programa: p.Programa(),
		Servidor: filepath.Join(p.Programa(), nomeServidor),
		Painel:   filepath.Join(p.Programa(), nomePainel),

		Historico:     filepath.Join(p.Dados(), pastas.BancoDoHistorico),
		Dicionario:    filepath.Join(p.Dados(), nomeDicionario),
		LogDoAgent:    filepath.Join(p.Dados(), nomeLogDoAgent),
		LogDoServidor: filepath.Join(p.Dados(), nomeLogDoServidor),
	}
}

// errDePastas marca o erro de resolver as pastas (resolverPastas). Esse erro
// vem ANTES do log: não há agent.log em lugar nenhum, e o aviso que a pessoa vê
// não pode mandá-la procurar um arquivo que não existe (mensagemDeErro).
type errDePastas struct{ err error }

func (e errDePastas) Error() string { return e.err.Error() }
func (e errDePastas) Unwrap() error { return e.err }

// resolverPastas é pastas.Resolver com o erro marcado. Só main a chama, com
// pastas.Sistema(); o teste passa um ambiente falso.
func resolverPastas(amb pastas.Ambiente) (pastas.Pastas, error) {
	p, err := pastas.Resolver(amb)
	if err != nil {
		return pastas.Pastas{}, errDePastas{err}
	}
	return p, nil
}

// registrarPastas diz no log em que modo os dados estão. Só o NOME do modo:
// o caminho levaria o nome de usuário do Windows para o log, que é o que se
// cola num issue.
//
// No modo legado (um historico.db ao lado do executável, sem o arquivo
// portatil) avisa em WARN, a cada subida: o Agent nunca cria o marcador
// sozinho (o programa é só leitura), e quem precisa saber é quem montou.
//
// No modo localappdata, um dicionario.json ao lado do executável NÃO é usado
// (o dicionário vale na pasta de dados), e isso era calado: o INFO "nenhum
// encontrado" não diz que há um arquivo logo ao lado. Acontece com quem seguiu
// o README antigo, que mandava pôr o dicionário ali, ou com um bin\ montado
// pelo asrstage antigo e nunca rodado. O aviso não diz o caminho.
func registrarPastas(log *slog.Logger, p pastas.Pastas) {
	log.Info("pastas: pasta de dados resolvida", "dados", string(p.Modo()))
	if p.Modo() == pastas.ModoPortatilLegado {
		log.Warn("pastas: há um " + pastas.BancoDoHistorico + " ao lado do executável e não há o arquivo " +
			pastas.Marcador + "; tratando como modo portátil, para não perder os dados. " +
			"Crie o arquivo " + pastas.Marcador + " ao lado do executável (ou rode o asrstage) para tornar isso explícito")
	}
	if p.Modo() == pastas.ModoLocalAppData && ehArquivo(filepath.Join(p.Programa(), nomeDicionario)) {
		log.Warn("pastas: há um " + nomeDicionario + " ao lado do executável que não é usado; o dicionário vale na pasta de dados " +
			"(%LOCALAPPDATA%\\" + pastas.NomeDaPastaDeDados + "). Copie o arquivo para lá e abra o Agent de novo")
	}
}

// ehArquivo: existe e não é pasta.
func ehArquivo(caminho string) bool {
	info, err := os.Stat(caminho)
	return err == nil && !info.IsDir()
}

// perfilDoUsuario é a pasta do perfil do Windows (%USERPROFILE%), que o log
// troca por um rótulo (pastas.Anonimizar). Vazio se o sistema não disser.
func perfilDoUsuario() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
}
