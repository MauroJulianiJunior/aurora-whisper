package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotSupported é devolvido fora do Windows, onde não há servidor de ASR
// para iniciar. Não é "não implementado ainda": o Agent é um produto
// Windows (ADR-010), e este pacote existe para ele.
var ErrNotSupported = errors.New("asr/local: processo de ASR só é iniciado no Windows")

// ErrServerDead é devolvido quando o servidor residente morreu e ainda não
// foi possível recolocá-lo de pé. Quem chama deve tratar como falha de
// transcrição — NUNCA devolver texto vazio no lugar, que o histórico
// registraria como uma transcrição que simplesmente não entendeu nada.
var ErrServerDead = errors.New("asr/local: servidor de ASR não está de pé")

// serverProcess é a costura entre a decisão pura deste pacote e o processo
// de verdade. Existe para que todo o resto — backoff, montagem do WAV,
// leitura da resposta, política de repetição — seja testável com um dublê,
// sem subir binário nenhum e sem depender de Windows.
type serverProcess interface {
	// Start sobe o servidor e espera até ele aceitar requisição, ou até
	// ctx terminar. Devolve o endereço (a base, ex.: "http://127.0.0.1:53124",
	// e o caminho secreto desta subida) para quem for falar HTTP com ele.
	//
	// O endereço é SEMPRE loopback. Um servidor de ASR escutando em
	// 0.0.0.0 exporia transcrição do que a pessoa fala para a rede local.
	Start(ctx context.Context) (endereco, error)

	// Alive reporta se o processo ainda está de pé. Barato o bastante para
	// ser chamado antes de cada transcrição.
	Alive() bool

	// Stop encerra o servidor e espera ele sair.
	Stop() error
}

// newServerProcess devolve a implementação de plataforma. Em Windows,
// process_windows.go; em qualquer outro SO, process_other.go devolve um
// processo que falha ao iniciar com ErrNotSupported.
//
// A separação existe porque o corpo de Start é a única parte deste pacote
// que não roda no CI do Linux — e quanto menor esse corpo, menor a
// superfície sem cobertura.
func newServerProcess(cfg Config) serverProcess { return newPlatformProcess(cfg) }

// Config descreve o servidor a subir e como configurá-lo.
//
// Os valores de desempenho NÃO têm default esperto aqui de propósito: quem
// monta a Config decide, e a decisão está registrada em
// docs/benchmarks/asr-decisao-2026-09-18.md com o número que a justifica.
// Um default escondido neste pacote seria mais uma medição que ninguém
// refez quando o hardware mudou.
type Config struct {
	// ServerPath é o caminho do whisper-server.exe. Absoluto, na prática: com
	// Dir preenchida, um caminho relativo seria lido a partir de Dir.
	ServerPath string
	// ModelPath é o caminho do .bin do modelo.
	//
	// Com Dir preenchida, o servidor recebe o modelo pelo caminho RELATIVO a
	// Dir quando ele está dentro dela (ver caminhoDoModelo): "ggml-small-q4_0.bin"
	// e não "C:\Users\<nome>\AppData\Local\Programs\...\ggml-small-q4_0.bin".
	// Um caminho relativo aqui já é relativo a Dir.
	ModelPath string
	// Dir é a pasta de trabalho do servidor (cmd.Dir): a pasta do PROGRAMA, a
	// mesma do whisper-server.exe, das DLLs e do modelo (ADR-018, F2). Vazio
	// mantém o de antes: o servidor herda a pasta atual do Agent.
	//
	// Por que importa instalado. O Agent abre por atalho, por início
	// automático e por terminal, cada um com uma pasta atual diferente, e nada
	// do servidor pode depender dela. E o caminho do modelo é uma das linhas do
	// whisper-server.log, que a pessoa cola num issue, com o nome do usuário do
	// Windows dentro (a linha é do whisper.cpp; só dá para tirá-lo do que lhe
	// entregamos). Também tira o servidor dos caminhos com acento ou fora do
	// cp1252, que o perfil do Windows pode ter: o nome do arquivo não os leva.
	//
	// Não é o bastante para o log: a linha da biblioteca do processador
	// (load_backend: loaded CPU backend from <pasta do executável>) é do
	// servidor e traz a pasta dele, que Dir não muda. Quem a limpa é o filtro
	// de caminhos de SaidaDoServidor (cmd/agent, abrirLogDoServidor).
	Dir string
	// Language é a dica de idioma passada ao servidor (ex.: "pt").
	Language string
	// Threads é quantas threads o servidor usa. Medido: o default do
	// binário é 4, e numa máquina de 20 núcleos lógicos 12 ganha 22%.
	// Zero deixa o servidor decidir (isto é, usar o default dele).
	Threads int
	// AudioCtx reduz a janela do encoder para TODA requisição (o -ac da
	// linha de comando). Medido em fala curta: 750 corta 40% do tempo sem
	// estragar a transcrição; abaixo disso a saída começa a errar, e em 150
	// o tempo EXPLODE para 15 s. Zero usa a janela inteira.
	//
	// Acima de 15 s de áudio o 750 fica frágil (incidente de 01-out-2026):
	// o Agent não o usa mais na linha de comando, e escolhe por requisição
	// (Janela). Fica para o benchmark medir o comportamento antigo.
	AudioCtx int
	// Janela escolhe o audio_ctx de cada requisição pela duração do áudio
	// (ver JanelaPorDuracao). Zero não manda o campo: vale AudioCtx.
	Janela JanelaPorDuracao
	// Decodificacao são ajustes do decodificador por requisição; zero não
	// manda nada. Só o benchmark os usa (ver Decodificacao).
	Decodificacao Decodificacao
	// SemFlashAttention sobe o servidor com -nfa. Com a flash attention, o
	// decodificador lê o K/V cruzado arredondado para múltiplos de 256 sem
	// máscara, e as linhas que sobram guardam o que a requisição anterior
	// escreveu: com a janela alternando entre 768 e a inteira, o texto do
	// ditado longo passou a depender do ditado curto anterior (6 de 6 áudios
	// em docs/benchmarks/asr-longo-2026-10-01.md). Sem ela, a atenção usa o
	// tamanho exato.
	SemFlashAttention bool
	// ReadyTimeout é quanto esperar o servidor aceitar a primeira
	// requisição. Zero usa DefaultReadyTimeout.
	ReadyTimeout time.Duration

	// SaidaDoServidor recebe o que o whisper-server escreve em stdout e
	// stderr. Nil mantém o comportamento de antes: o stderr do Agent.
	//
	// Precisa ser configurável desde 28-set-2026, quando o Agent passou a
	// rodar como aplicativo de bandeja, SEM console. Num processo sem
	// console, o stderr herdado é um handle inválido — o filho escreveria
	// no vazio, e as centenas de linhas de carregamento do modelo, que são
	// a única pista quando o servidor não sobe, se perderiam.
	SaidaDoServidor io.Writer
}

// DefaultReadyTimeout cobre o carregamento do modelo do disco na primeira
// subida, que com `small` em q4_0 (139 MB) leva poucos segundos em disco
// quente e bem mais em disco frio.
const DefaultReadyTimeout = 90 * time.Second

func (c Config) readyTimeout() time.Duration {
	if c.ReadyTimeout <= 0 {
		return DefaultReadyTimeout
	}
	return c.ReadyTimeout
}

// # O caminho secreto de cada subida
//
// O servidor escuta só em 127.0.0.1, mas o loopback não basta: o whisper-server
// aceita qualquer Host e qualquer Origin, responde com
// Access-Control-Allow-Origin: *, e tem a rota /load, que troca o modelo por um
// arquivo do disco. Uma página da web aberta no navegador de quem usa, que
// achasse a porta, derrubaria o servidor e descobriria se um arquivo existe
// (auditoria de rede do PC, 13-out-2026).
//
// O b5130 tem a opção --request-path, que prefixa TODAS as rotas que ele
// registra: GET /, POST e OPTIONS /inference, POST /load e GET /health
// (examples/server/server.cpp da tag). Cada subida ganha um caminho novo, de
// 128 bits do crypto/rand, e o Agent só fala com o servidor por ele; sem o
// caminho, a página recebe 404 e não alcança rota nenhuma. O que sobra fora
// dele é a pasta estática (--public, set_base_dir), que o servidor só monta se
// a pasta existir: o padrão é examples/server/public, relativo à pasta de
// trabalho (a do programa, Config.Dir), e o Setup não a cria.
//
// O caminho é segredo: nunca vai a log nem a mensagem de erro (ver endereco e
// Provider.pedir). O servidor também não o escreve: a linha "listening at"
// traz só o host e a porta. O único lugar em que ele o imprime é o texto de
// uso, que sai num argumento desconhecido ou inválido, e com o processo saindo
// logo em seguida (exit): o caminho de um servidor que já morreu não abre nada.
// Mesmo assim ele vai por último no argv (serverArgs), para que um argumento
// desconhecido antes dele imprima o uso com o caminho ainda vazio.

// bytesDoCaminho é o tamanho do caminho secreto: 128 bits.
const bytesDoCaminho = 16

// novoCaminhoSecreto devolve "/" seguido de 32 dígitos hexadecimais, de
// 128 bits do crypto/rand. Um novo a cada chamada: é o de UMA subida.
func novoCaminhoSecreto() string {
	var b [bytesDoCaminho]byte
	// Desde o Go 1.24, rand.Read nunca devolve erro: se o sistema não der
	// aleatoriedade, o processo morre antes. Mesma forma de novoSegredo, em
	// internal/agent/nuvem.
	_, _ = rand.Read(b[:])
	return "/" + hex.EncodeToString(b[:])
}

// endereco é onde um servidor que subiu atende: a base (esquema, host e porta)
// e o caminho secreto que prefixa cada rota dele.
//
// Só url junta os dois. String (e com ela %v, %s e %+v) e GoString (%#v)
// mostram só a base: quem escrever o endereço num erro ou num log não leva o
// caminho junto.
type endereco struct {
	base    string // "http://127.0.0.1:53124": pode ir a erro e a log
	caminho string // "/<32 hex>": NUNCA vai a erro nem a log
}

// url é o endereço da rota (ex.: "/inference") neste servidor, com o caminho.
func (e endereco) url(rota string) string { return e.base + e.caminho + rota }

func (e endereco) String() string { return e.base }

func (e endereco) GoString() string { return "local.endereco{" + e.base + "}" }

// semCaminho troca o caminho secreto em s por um rótulo. Serve ao corpo de uma
// resposta de erro, que o whisper-server pode ecoar: num erro que não seja 400
// nem 500, o tratador de erro dele responde "File Not Found (<caminho da
// requisição>)".
func (e endereco) semCaminho(s string) string {
	if e.caminho == "" {
		return s
	}
	return strings.ReplaceAll(s, e.caminho, "/<caminho>")
}

// serverArgs monta o argv do whisper-server. Puro, para que a regra de
// loopback, o caminho secreto e o repasse dos flags de desempenho sejam
// verificáveis sem subir processo nenhum.
//
// host, port e caminho entram por parâmetro, e não por Config, porque são de
// cada subida: quem escolhe a porta é a implementação de plataforma (pede uma
// livre ao SO), e o caminho é novo a cada uma (novaSubida).
//
// O que NUNCA entra aqui: -pr (--print-realtime), -debug (--debug-mode) e
// --convert. Com -pr, o texto de cada ditado iria para o whisper-server.log;
// -debug despeja dados do áudio (o log_mel) para diagnóstico; --convert grava
// o áudio recebido num arquivo temporário e chama o ffmpeg pelo shell, com a
// saída dele no log (áudio em disco, contra o AC-028). Um teste trava isso
// (TestServerArgs_NuncaLevaFlagQueExpoeODitado).
func serverArgs(cfg Config, host string, port int, caminho string) []string {
	args := []string{
		"--host", host,
		"--port", itoa(port),
		"-m", caminhoDoModelo(cfg),
	}
	if cfg.Language != "" {
		args = append(args, "-l", cfg.Language)
	}
	if cfg.Threads > 0 {
		args = append(args, "-t", itoa(cfg.Threads))
	}
	if cfg.AudioCtx > 0 {
		args = append(args, "-ac", itoa(cfg.AudioCtx))
	}
	if cfg.SemFlashAttention {
		args = append(args, "-nfa")
	}
	// Por último, de propósito: ver "O caminho secreto de cada subida".
	return append(args, "--request-path", caminho)
}

// caminhoDoModelo é o que vai depois do -m: o modelo pelo caminho relativo à
// pasta de trabalho do servidor (Dir), quando ele está dentro dela, e o
// caminho que a Config trouxe em qualquer outro caso.
//
// Puro, para a regra ser verificável sem subir processo: é ela que decide se
// o whisper-server.log (e o carregador de arquivos do whisper.cpp) vê o nome
// do arquivo ou um caminho com o perfil do usuário.
//
// Só encurta quando dá para ter certeza do que "relativo" quer dizer: Dir e
// ModelPath absolutos, e o modelo dentro de Dir. Sem Dir, com um caminho
// relativo (já é relativo a Dir; olhá-lo contra Dir o leria duas vezes), com
// Dir relativa (filepath.Rel recusa: não há como saber a que ela se refere), ou
// com o modelo noutro disco ou fora da pasta, devolve o que recebeu. Nunca sobe
// um nível ("..\"): sairia da pasta que o servidor considera dele.
func caminhoDoModelo(cfg Config) string {
	if cfg.Dir == "" || !filepath.IsAbs(cfg.ModelPath) {
		return cfg.ModelPath
	}
	rel, err := filepath.Rel(cfg.Dir, cfg.ModelPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return cfg.ModelPath
	}
	return rel
}

// comandoDoServidor é o que se executa para subir o servidor: o executável, os
// argumentos e a pasta de trabalho. Existe para que a montagem seja um valor
// que o teste confere, sem executar nada (montarComando é pura; execCmd só
// monta o *exec.Cmd, que ninguém inicia aqui).
type comandoDoServidor struct {
	Caminho string
	Args    []string
	Dir     string // pasta de trabalho; vazia, o servidor herda a do Agent
}

// montarComando monta o comando do servidor para a porta e o caminho secreto
// pedidos. host, port e caminho entram por parâmetro pelo mesmo motivo de
// serverArgs.
func montarComando(cfg Config, host string, port int, caminho string) comandoDoServidor {
	return comandoDoServidor{
		Caminho: cfg.ServerPath,
		Args:    serverArgs(cfg, host, port, caminho),
		Dir:     cfg.Dir,
	}
}

// novaSubida é o que uma subida do servidor precisa: o comando, com um caminho
// secreto novo, e o endereço em que o servidor vai atender, com o MESMO
// caminho. É a única fonte do caminho: a implementação de plataforma a chama a
// cada processo que sobe (inclusive a cada porta tentada), e o caminho do
// comando e o do endereço nunca se desencontram.
func novaSubida(cfg Config, host string, port int) (comandoDoServidor, endereco) {
	caminho := novoCaminhoSecreto()
	return montarComando(cfg, host, port, caminho),
		endereco{base: "http://" + host + ":" + itoa(port), caminho: caminho}
}

// execCmd é o *exec.Cmd do comando, com a pasta de trabalho e a saída ligadas,
// e SEM iniciar nada. saida nil manda o que o servidor escreve para o stderr do
// Agent, como sempre foi. Os atributos de processo de cada sistema (no Windows,
// CREATE_SUSPENDED e CREATE_NO_WINDOW) são de quem inicia.
func (c comandoDoServidor) execCmd(saida io.Writer) *exec.Cmd {
	cmd := exec.Command(c.Caminho, c.Args...)
	cmd.Dir = c.Dir
	if saida == nil {
		saida = os.Stderr
	}
	cmd.Stdout = saida
	cmd.Stderr = saida
	return cmd
}
