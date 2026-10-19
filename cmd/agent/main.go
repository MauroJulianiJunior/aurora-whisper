// Command agent é o ponto de entrada do Windows Agent do Aurora Whisper.
//
// Sprint 1 (ADR-010): instala o hook global de teclado, abre a captura de
// áudio (desde a ADR-015, por padrão o microfone só liga enquanto dita), cria o
// overlay nativo e roda o runtime de
// produção (internal/agent/winruntime) que liga tudo isso ao núcleo puro
// do ditado (internal/agent/dictation, ADR-007). Encerra limpo em Ctrl+C:
// desinstala o hook, para a captura de áudio, destrói a janela do
// overlay.
//
// Sprint 2 (ADR-011) acrescenta o rastreador de foco (internal/agent/uia)
// — sobe junto com hook/áudio/overlay e é encerrado do mesmo jeito — e o
// portão de segurança (internal/agent/sysinput): sysinput.Enable() é
// chamado UMA ÚNICA VEZ aqui, na inicialização, e só este binário chama —
// é o que libera a segunda camada do portão que winruntime/insert.go
// (via internal/agent/inserter) e o rastreador de foco (via
// uia.Tracker.Start) checam antes de agir no sistema real (ADR-011,
// decisão 6). Sob "go test", Guard recusa sempre, então nada nesta
// função corre risco de executar fora de um binário real — ver
// internal/agent/sysinput/doc.go.
//
// No Sprint 1, ASR e resolução eram stub (ADR-010, "Escopo do Sprint 1"):
// o objetivo era provar o núcleo real alimentado por Win32 real. Hoje
// sobem aqui também o ASR local (abrirASRLocal), o dicionário
// (abrirDicionarioEm), o histórico com o painel local (abrirHistoricoComRetencao,
// subirPainel) e a bandeja (abrirBandeja). Onde cada arquivo mora, programa
// de um lado e dados do outro, decide o pacote internal/agent/pastas
// (caminhosDe, em caminhos.go).
//
// Fora de Windows este binário compila normalmente (é exigência do CI,
// que roda em matriz ubuntu-latest + windows-latest — CLAUDE.md,
// "compilação multiplataforma") e, ao rodar, imprime uma mensagem clara e
// sai sem erro em vez de falhar tentando instalar um hook que não existe
// fora do Win32: run() detecta isso pelo primeiro erro Win32 real que
// aparece (hook.Start(), que devolve win32.ErrNotSupported em qualquer SO
// que não seja Windows), sem precisar de nenhum arquivo com build tag
// próprio neste pacote — hook, audio, overlay e uia.Tracker já resolvem
// essa diferença sozinhos.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/local"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/model"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
	"log"
	"log/slog"
	"maps"
	_ "modernc.org/sqlite"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/autostart"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/conflito"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/expurgo"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/guard"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/hook"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/instancia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/painel"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/pastas"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/permissao"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/tray"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/uia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
)

// appName identifica o binário nas mensagens de diagnóstico.
const appName = "aurora-whisper-agent"

// version é a versão do Windows Agent. É uma VARIÁVEL, e não uma constante,
// para o build de release poder injetá-la (ADR-018, scripts/montar-instalador.ps1):
//
//	go build -trimpath -ldflags "-H=windowsgui -X main.version=<versão>" ...
//
// O -X só troca variável string inicializada com um literal (ou sem
// inicializador); numa const ele é ignorado em silêncio, e numa variável
// inicializada por chamada de função também. O valor abaixo é o de um build de
// desenvolvimento, sem -X.
var version = "0.1.0-sprint2"

// audioStartTimeout limita quanto tempo run() espera o dispositivo de
// áudio abrir antes de desistir (ADR-010, D1: só o dispositivo padrão, em
// modo compartilhado — abrir não deveria demorar; um timeout evita que um
// driver travado prenda o Agent inteiro na inicialização).
const audioStartTimeout = 10 * time.Second

// defaultConfig é a configuração com que o Machine nasce, e o padrão da
// tela de Configurações (preferenciasPadrao). O limiar e o beep gravados
// pela tela chegam depois, por Runtime.Ajustar (carregarPreferencias); a
// tecla continua fixa.
func defaultConfig() dictation.Config {
	return dictation.Config{
		// Gatilho por COMBINAÇÃO de modificadores, não por tecla de letra
		// (ADR-013). Com Ctrl+Win não há caractere para engolir e devolver,
		// e o defeito que fazia "freio" sair "refio" deixa de existir.
		//
		// Ctrl+Win é também a convenção do Wispr Flow — se ele estiver
		// instalado, os dois disparam no mesmo atalho.
		Key:           dictation.KeyUnknown,
		Modifiers:     dictation.ModControl | dictation.ModMeta,
		HoldThreshold: 700 * time.Millisecond,
		BeepEnabled:   true,
		// Fixado explicitamente (em vez de deixado em zero) para que
		// audio.Config.MaxRecording, logo abaixo em run(), use o MESMO
		// valor por construção — não por coincidência de dois defaults
		// que por acaso empatam. Ver o comentário de
		// audio.Config.MaxRecording (internal/agent/audio/session.go).
		MaxRecording: dictation.DefaultMaxRecording,
	}
}

// configDeAudio é a captura que o Agent pede.
//
// O microfone só enquanto dita (ADR-015) é o padrão, e está escrito aqui
// por extenso em vez de depender do valor zero de audio.Config: uma troca
// de padrão lá, ou alguém ligando SemprePronto aqui, deixaria o microfone
// aceso o tempo todo sem nenhum outro sinal — e o teste deste pacote é o
// que pega. "Microfone sempre pronto" é opção, desligada por padrão: a
// escolha gravada na tela de Configurações chega depois da subida, por
// aplicarPreferencias.
//
// PoliticaParar é o estacionamento padrão. Se o roteiro manual mostrar que
// o ícone de microfone não apaga com o cliente parado (M0/M1), troque por
// audio.PoliticaLiberar aqui.
//
// O ring de áudio cabe o pior caso do núcleo: o limite de gravação e o
// MAIOR limiar de hold que ele aceita — não o configurado, que a tela muda
// com o Agent rodando (ver audio.Config.MaxHoldThreshold).
func configDeAudio(cfg dictation.Config) audio.Config {
	return audio.Config{
		SemprePronto:     false,
		Politica:         audio.PoliticaParar,
		MaxRecording:     cfg.MaxRecording,
		MaxHoldThreshold: dictation.MaxHoldThreshold,
	}
}

func main() {
	// Libera a segunda camada do portão de segurança (ADR-011, decisão 6)
	// ANTES de qualquer coisa que possa checá-la (uia.Tracker.Start, o
	// inserter, a bandeja, a janela de aviso). Só este binário chama
	// Enable, uma vez. Sob "go test" isto não tem efeito nenhum — a
	// primeira camada do portão recusa sempre, incondicionalmente (ver
	// internal/agent/sysinput/doc.go) — por isso é seguro chamar aqui
	// incondicionalmente, sem nenhum "if" de ambiente.
	sysinput.Enable()
	defer sysinput.Disable()

	// Um Agent por sessão, e verificado antes de TUDO — inclusive do log.
	// O segundo precisa sair sem ter tocado em nada: nem no whisper-server,
	// nem no hook, nem no agent.log. Antes, ele abria o log e o girava na
	// subida, apagando o agent.log.1 do Agent que já estava rodando.
	//
	// Com o início automático, abrir o executável na mão com um Agent já
	// na bandeja seria a coisa mais fácil do mundo — e dois hooks na
	// mesma tecla duplicam o texto. Foi exatamente o sintoma da noite de
	// 27-set-2026, quando o outro programa era o Wispr Flow; desta vez a
	// causa seria nossa. Ver internal/agent/instancia.
	liberarInstancia, errInstancia := instancia.Garantir(instancia.NomeDoAgent)
	if errors.Is(errInstancia, instancia.ErrJaRodando) {
		// Sair não é erro: é o comportamento certo, código 0. Mas sair
		// calado deixava o duplo clique sem resposta nenhuma — e o ícone
		// costuma estar escondido na setinha da bandeja.
		avisar("O Aurora Whisper já está rodando.\n\nO ícone dele fica na bandeja do Windows, perto do relógio. Se não aparecer, clique na setinha ^ ao lado dos ícones.", false)
		return
	}

	// O log vai para arquivo antes do resto, e fecha só DEPOIS de
	// registrar o erro de run: se fosse fechado dentro de run, o erro que
	// derrubou o Agent seria escrito num arquivo já fechado — e sem
	// console, esse erro é a única pista que sobra.
	//
	// As pastas se resolvem aqui, depois da instância e antes do log: é
	// onde o log mora, e resolver cria a pasta de dados (MkdirAll). O
	// segundo Agent, que sai acima, não cria nada. Se não resolvem, o log
	// fica como estava (sem arquivo) e o erro sai pelo aviso, abaixo.
	pp, errPastas := resolverPastas(pastas.Sistema())
	fecharLog := func() {}
	if errPastas == nil {
		fecharLog = configurarLog(pp, perfilDoUsuario())
	}
	switch {
	case errInstancia == nil:
	case errors.Is(errInstancia, instancia.ErrNaoSuportado):
		// Fora do Windows; run() diz isso a quem rodou.
	default:
		// Falha do mecanismo, não "já rodando". Raríssimo; ditar sem a
		// garantia é melhor que não ditar, mas não pode ser silencioso.
		slog.Default().Warn("instancia: não consegui garantir instância única; seguindo sem ela", "err", errInstancia)
	}

	err := errPastas
	if err == nil {
		err = run(pp)
	}
	if err != nil {
		encerrarComErro(err, fecharLog, liberarInstancia, avisar)
		os.Exit(1) // depois de fecharLog: os.Exit não roda defers
	}
	fecharLog()
	if liberarInstancia != nil {
		liberarInstancia()
	}
}

// encerrarComErro é o fim de main quando run falhou: registra o erro,
// fecha o log, solta a instância e só então avisa quem usa.
//
// # Por que a instância é solta ANTES do aviso
//
// Sem console, o aviso é uma MessageBox que bloqueia até alguém clicar OK
// — e que, com a trava de primeiro plano do logon, pode nem aparecer na
// frente. Enquanto ela ficava aberta, o mutex continuava preso: quem
// reabria o Agent ouvia "já está rodando, o ícone fica na bandeja", sem
// Agent nenhum rodando e sem ícone (revisão de 29-set-2026). Quando run
// volta, os defers dela já desmontaram tudo — hook, áudio, ASR, painel,
// bandeja —, e a instância não protege mais nada.
//
// O log fecha antes pelo mesmo motivo: um Agent novo, aberto com o aviso
// ainda na tela, abre (e pode girar) o mesmo agent.log.
func encerrarComErro(err error, fecharLog, liberarInstancia func(), avisar func(texto string, ehErro bool)) {
	log.Printf("%s: %v", appName, err)
	fecharLog()
	if liberarInstancia != nil {
		liberarInstancia()
	}
	avisar(mensagemDeErro(err), true)
}

// mensagemDeErro é o texto do aviso de erro fatal.
//
// Neutro de propósito: o mesmo aviso sai para o erro da subida (modelo
// faltando, ícone recusado pela barra de tarefas) e para o que derruba o
// Agent depois de rodar (o runtime). "Não conseguiu iniciar" era falso no
// segundo caso (revisão de 29-set-2026). Dizer que dá para abrir de novo
// só é verdade porque encerrarComErro solta a instância antes do aviso.
//
// O erro das pastas (errDePastas) é o único que vem antes do log: o agent.log
// não existe em lugar nenhum, e mandar a pessoa procurá-lo seria mandá-la
// procurar um arquivo que não há. O próprio erro já diz o que faltou.
func mensagemDeErro(err error) string {
	var dasPastas errDePastas
	if errors.As(err, &dasPastas) {
		return fmt.Sprintf("O Aurora Whisper foi encerrado por um erro.\n\n%v\n\nO programa não conseguiu nem preparar a pasta dos seus dados, então desta vez não há agent.log. Para tentar de novo, é só abrir o programa outra vez.", err)
	}
	return fmt.Sprintf("O Aurora Whisper foi encerrado por um erro.\n\n%v\n\nOs detalhes ficam no agent.log, na pasta de dados do programa (%%LOCALAPPDATA%%\\AuroraWhisper; no modo portátil, ao lado do executável). Para tentar de novo, é só abrir o programa outra vez.", err)
}

// run sobe o Agent e só volta quando ele encerra. pp são as pastas já
// resolvidas em main (a de dados já foi criada): run não chama
// os.Executable para achar arquivo nenhum, só para o início automático.
func run(pp pastas.Pastas) error {
	// Sem isto o log não tem resolução abaixo de UM SEGUNDO, e nenhuma
	// linha pode ser subtraída de outra.
	//
	// slog.Default(), quando ninguém chama slog.SetDefault, escreve pelo
	// pacote log — que sai com Ldate|Ltime. O resultado é que a sessão
	// inteira de 28-set-2026 carimbou "01:39:45" em quatro etapas
	// diferentes do mesmo ditado, e a única forma de saber para onde ia o
	// tempo era ler o código e reconstruir. Os únicos números sub-segundo
	// que existiam vinham de estágios que se cronometram por dentro.
	//
	// Lmicroseconds é o mínimo que resolve, e mantém o formato legível
	// que já estava ali: a linha ganha ".975758", não vira key=value.
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Onde cada arquivo mora (ADR-018). O que o Agent lê do programa e o que
	// ele grava vêm de pastas diferentes; ver caminhosDe.
	registrarPastas(slog.Default(), pp)
	cam := caminhosDe(pp)

	cfg := defaultConfig()
	machine, err := dictation.NewMachine(cfg)
	if err != nil {
		return fmt.Errorf("config invalida: %w", err)
	}

	// O portão (sysinput.Enable) e a instância única são tomados em main,
	// antes do log: ver lá.

	hk := hook.New(hook.Config{Key: cfg.Key, Modifiers: cfg.Modifiers})
	if err := hk.Start(); err != nil {
		if errors.Is(err, win32.ErrNotSupported) {
			fmt.Printf("%s %s: suportado apenas em Windows 10/11 x64; nada a fazer nesta plataforma.\n", appName, version)
			return nil
		}
		return fmt.Errorf("hook: %w", err)
	}
	defer func() {
		if stopErr := hk.Stop(); stopErr != nil {
			// Pelo log, não por stderr: sem console, stderr não vai a lugar
			// nenhum, e erro de encerramento é justamente o que se procura
			// depois. O mesmo nos três abaixo.
			slog.Default().Warn("hook: Stop falhou", "err", stopErr)
		}
	}()

	// O microfone é ABERTO e preparado aqui, sempre sob demanda
	// (configDeAudio): só liga no keydown e estaciona quando o ditado
	// termina (ADR-015), e em repouso o Windows não o mostra em uso. Se o
	// "microfone sempre pronto" estiver gravado nas Configurações,
	// carregarPreferencias o liga logo depois, ainda na subida. Abrir na
	// subida, e não no primeiro ditado, faz um microfone que não abre
	// aparecer já aqui.
	startCtx, cancelStart := context.WithTimeout(context.Background(), audioStartTimeout)
	sess, err := audio.NewSource().Start(startCtx, configDeAudio(cfg))
	cancelStart()
	if err != nil {
		return fmt.Errorf("audio: %w", err)
	}
	slog.Default().Info("audio: microfone preparado", "modo", sess.Modo(), "politica", sess.Politica().String())
	defer func() {
		if closeErr := sess.Close(); closeErr != nil {
			slog.Default().Warn("audio: Close falhou", "err", closeErr)
		}
	}()

	ov, err := overlay.New()
	if err != nil {
		return fmt.Errorf("overlay: %w", err)
	}
	// Apresentar um frame é best-effort e nunca derruba o ditado, mas
	// falhar em silêncio deixava "a janela não apareceu" indistinguível
	// de "apareceu e você não viu" — ver Overlay.reportarFalhaDeDesenho.
	ov.AoFalharDesenho(func(err error) {
		slog.Default().Error("overlay: nao consegui apresentar o frame; a pilula nao vai aparecer na tela", "err", err)
	})
	// Onde a pílula foi posta. GetSystemMetrics só enxerga a tela
	// PRIMÁRIA: com dois monitores, a pílula vai para o primário mesmo
	// que a pessoa esteja trabalhando no outro — e isso é invisível em
	// qualquer outro log.
	{
		x, y, w, h, telaW, telaH := ov.Geometria()
		slog.Default().Info("overlay: janela criada",
			"x", x, "y", y, "largura", w, "altura", h,
			"tela_primaria", fmt.Sprintf("%dx%d", telaW, telaH))
	}

	avisarConflitoDeDitado(slog.Default())
	defer func() {
		if closeErr := ov.Close(); closeErr != nil {
			slog.Default().Warn("overlay: Close falhou", "err", closeErr)
		}
	}()

	// O rastreador de foco (ADR-011, decisões 2 e 5): thread dedicada que
	// consulta UI Automation fora do caminho do teclado a cada troca de
	// foco, e um cache que handleRaw (winruntime) lê em O(1). Sobe e desce
	// junto com hook/áudio/overlay.
	focus := uia.NewTracker(uia.Config{})
	// Um ditado recusado por "campo protegido" tem quatro causas
	// possíveis e só uma delas está certa — ver uia.DiagnosticoFoco.
	// Nada aqui diz o que há na janela; do aplicativo, só o nome-base do
	// executável, e só num WARN (ver registroDoFoco).
	focus.AoInspecionar(func(d uia.DiagnosticoFoco) {
		nivel, args := registroDoFoco(d)
		slog.Default().Log(context.Background(), nivel, "uia: foco inspecionado", args...)
	})
	if err := focus.Start(); err != nil {
		return fmt.Errorf("uia tracker: %w", err)
	}
	{
		nivel, args := registroDaAutomacao(focus.Automacao())
		slog.Default().Log(context.Background(), nivel, "uia: automação", args...)
	}
	defer func() {
		if stopErr := focus.Stop(); stopErr != nil {
			slog.Default().Warn("uia: Tracker.Stop falhou", "err", stopErr)
		}
	}()

	// O ASR local sobe ANTES do runtime, e falha alto se não puder subir.
	//
	// Falhar aqui é melhor que subir sem ASR: o Agent ficaria capturando
	// áudio e inserindo erro em todo ditado, e a pessoa só descobriria o
	// motivo falando com o programa. Modelo faltando e modelo corrompido
	// dão mensagens diferentes de propósito — pedem ações diferentes
	// (internal/asr/model).
	asrProvider, fecharASR, err := abrirASRLocal(cam, pp, perfilDoUsuario())
	if err != nil {
		return err
	}
	defer fecharASR()

	rt := winruntime.New(machine, clock.NewSystem(), hk, sess, ov, focus, asrProvider, slog.Default())

	// A privacidade do Windows pode bloquear o microfone: quem instala assim
	// não vê nada. O Agent percebe (leitura do registro, nada é gravado),
	// registra na subida e, quando um ditado falha por isso, a pílula manda
	// olhar o painel (permissao; overlay.FaseMicrofoneBloqueado).
	avisarMicrofoneBloqueado(slog.Default(), permissao.Consultar)
	rt.ComConsultaDoMicrofone(permissao.Consultar)

	// O dicionário pessoal: corrige termo técnico depois do ASR, e
	// enviesa o próprio ASR antes. Sem ele o ditado funciona igual, só
	// sem correção de termos — ver abrirDicionarioEm.
	//
	// O resolver é montado SEMPRE (ADR-016, decisão 13): sem
	// dicionario.json, o Smart roda sem dicionário (só a autocorreção), e
	// o Polish, que parte do texto do Smart, continua alcançável.
	//
	// Antes de abrir, garante que há um dicionario.json: se nada existe, cria
	// um VAZIO (e só então), para o aprender do painel funcionar desde o
	// primeiro dia num Agent instalado, que não vem com dicionário
	// (garantirDicionario; ADR-018). Tem de vir ANTES: o usuario do arquivo
	// entra no runtime e no aprendizado logo abaixo. O arquivo criado agora é
	// outro arquivo para o sync do aprendido (prepararOAprendido, abaixo).
	criouDicionario := garantirDicionario(slog.Default(), cam.Dicionario)
	smart, vocab, usuario, dominioDoApp, err := abrirDicionarioEm(slog.Default(), cam.Dicionario)
	if err != nil {
		return err
	}
	rt.UsarResolver(smart, usuario)
	rt.UsarVocabulario(vocab)
	rt.UsarDominioDoApp(dominioDoApp)

	// O cofre da chave e o cliente dos provedores de IA servem à tela (a
	// chave, o "Testar") e ao Polish. O Polish lê a chave a cada ditado,
	// pelo provedor do keydown — ver polishDoAgent.
	cofre := chaves.NovoGerenciador()
	clienteIA := &ia.Cliente{}
	polish := novoPolishDoAgent(smart, cofre, clienteIA, slog.Default())
	rt.UsarPolish(polish.abrir)

	// O aprendizado pelo painel (etapa 3): grava a variante no mesmo
	// dicionario.json e troca o dicionário do runtime na hora. Sem
	// dicionário na subida não há o que aprender (usuario vazio). A API local
	// do painel o usa: sugere ao salvar uma edição e grava só com o clique.
	aprendizado := novoAprendizado(cam.Dicionario, usuario, rt, polish, slog.Default())

	// Histórico e painel sobem juntos, e nessa ordem: o painel lê o mesmo
	// banco. Os defers rodam ao contrário — o painel para ANTES de o
	// banco fechar, e nenhuma requisição chega a um banco já fechado.
	//
	// Um dono só para os dois: o dono fixo desta instalação, gravado no
	// próprio banco (escolherDono). O usuario do dicionário serve só ao
	// dicionário, acima — e só entra aqui quando a migração para o dono
	// fixo não pôde rodar.
	//
	// Degradação aceitável: sem histórico o ditado funciona igual, só não
	// fica registrado — e o laço de aprendizado volta a depender da
	// memória de quem ditou.
	//
	// As preferências gravadas pela tela de Configurações vêm do mesmo
	// banco; sem ele, valem as de defaultConfig e não há tela para mudá-las.
	//
	// O caminho do executável só serve ao início automático, que grava no
	// Run o caminho absoluto do .exe (autostart.Ligar): é o executável, e não
	// a pasta do programa, e nada disso muda com as pastas de ADR-018. Sem
	// ele também se dita: o início automático fica indisponível
	// (inicioComWindows com exe vazio).
	exe, err := os.Executable()
	if err != nil {
		slog.Default().Warn("agent: não localizei o executável; seguindo sem início automático", "err", err)
		exe = ""
	}
	inicio := inicioComWindows{exe: exe, console: temConsole(), log: slog.Default()}
	var abrirPainel func() error
	// resumoDaNuvem é a linha da nuvem na bandeja; sem histórico, não há sync.
	resumoDaNuvem := func() string { return "" }
	if h, err := abrirHistoricoComRetencao(slog.Default(), cam.Historico, usuario, clock.NewSystem()); err != nil {
		slog.Default().Warn("historico: seguindo SEM registro; o laço de aprendizado fica sem o texto bruto", "err", err)
	} else {
		hist, prefs, dono, fecharHist := h.hist, h.prefs, h.dono, h.fechar
		defer fecharHist()
		rt.UsarHistorico(hist, asrProvider.ID(), model.SmallQ4_0.Nome, dono)
		aplicar := aplicarPreferencias(rt.Ajustar, sess, slog.Default())
		carregarPreferencias(prefs, aplicar, slog.Default(), time.Sleep)

		// A nuvem (ADR-019, F6): começa desligada e só fala com a rede
		// depois que a pessoa clica em Conectar à nuvem nas Configurações.
		// Mora no mesmo banco (a tabela instalacao), e o retorno da
		// aprovação chega pelo servidor do painel.
		// O lado do histórico (F7c): a prévia, o "Enviar o histórico" (a
		// cópia fica ao lado do historico.db da tabela) e o esquecer.
		nuvemDoPC := montarNuvem(prefs.Instalacao(), historicoDaNuvem(hist, dono, cam.Historico), pp.Modo(), slog.Default())

		// O sync do histórico (ADR-019, F7b): um laço em segundo plano, que o
		// ditado só acorda. Para antes de o banco fechar (os defers rodam ao
		// contrário). O aprendido do painel (F8g) vai junto: o laço aplica o
		// que chega da nuvem pelo mesmo aprendizado (a mesma trava), e o
		// Aprender e o Desfazer do painel o acordam. Antes, o dicionário é
		// conferido contra o espelho do sync: o recriado vazio, ou o de outro
		// usuário, não pode valer como "a pessoa desfez tudo" (o espelho do
		// anterior é solto); sem a conferência, o laço desta execução o vê
		// como indisponível.
		if !prepararOAprendido(context.Background(), hist.Fila(dono), prefs.Instalacao(), usuario, criouDicionario,
			time.Now(), slog.Default()) {
			aprendizado.semConferir = true
		}
		laco := montarSync(hist, dono, nuvemDoPC, aprendizado, slog.Default())
		defer iniciarSync(laco, slog.Default())()
		resumoDaNuvem = laco.Resumo

		// O contador de mudanças do sync (ADR-021, S) vai junto: o painel o lê
		// para saber quando reler a lista, e a leitura avisa o laço de que ele
		// está aberto.
		api := localapi.New(dono, hist, prefs, ligarMudancasAoPainel(ligarAprendidoAoPainel(localapi.Preferencias{
			Padrao:       preferenciasPadrao(),
			Chaves:       cofre,
			Inicio:       inicio,
			Modelos:      clienteIA,
			Aplicar:      aplicarNoRuntime(aplicar),
			UltimoPolish: ultimoPolishParaATela(rt.UltimoPolish),
			Nuvem:        nuvemDoPC,
			// Os 6 meses e as cópias do banco (F10d e F10e): o mesmo ciclo do
			// expurgo, dono do mutex do Ligar, do Desligar e do apagar das
			// cópias. Sem o OK gravado nas Configurações, nada dos 6 meses é
			// apagado.
			Retencao:  h.retencao,
			Microfone: novoMicrofoneDoAgent(),
			Log:       slog.Default(),
		}, hist, dono, aprendizado), laco))
		abrir, porta, pararPainel, err := subirPainel(api.Handler(), nuvemDoPC.Retorno(), cam.Painel, slog.Default())
		if err != nil {
			slog.Default().Warn("painel: não subiu; o item Abrir da bandeja fica desabilitado", "err", err)
		} else {
			defer pararPainel()
			abrirPainel = abrir
			nuvemDoPC.UsarPorta(porta)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// O "taskkill" sem /F manda um WM_CLOSE só, e ele cai na pílula do
	// overlay, não na bandeja — ver overlay.AoPedirFechar. Ele pede o mesmo
	// que o item Sair. Um pedido que chegou durante a subida é entregue
	// aqui mesmo, no registro; aí subir a bandeja só para derrubá-la em
	// seguida não serve a ninguém.
	ov.AoPedirFechar(func() {
		slog.Default().Info("overlay: pedido de fechar vindo de fora (taskkill sem /F); encerrando")
		stop()
	})
	if ctx.Err() != nil {
		return nil
	}

	// A bandeja. Sair por ela cancela o mesmo contexto do Ctrl+C: um
	// caminho de encerramento só, com os mesmos defers.
	bandeja, err := abrirBandeja(hk, asrProvider.ID()+" · "+model.SmallQ4_0.Nome, inicio, stop, abrirPainel, resumoDaNuvem)
	if err == nil {
		defer bandeja.Parar()
	} else {
		switch semBandeja(ctx.Err() != nil, temConsole()) {
		case semBandejaSairCalado:
			slog.Default().Info("tray: o encerramento foi pedido durante a subida da bandeja; saindo sem aviso", "err", err)
			return nil
		case semBandejaSeguir:
			slog.Default().Warn("tray: seguindo SEM ícone na bandeja", "err", err)
		default:
			return fmt.Errorf("sem console e sem bandeja, não haveria como encerrar o Agent: %w", err)
		}
	}

	// O hook está instalado desde o começo de run, e a subida leva de
	// segundos (warmup do ASR) a bem mais (tentativas do ícone com o
	// Explorer ocupado). O que foi apertado nesse tempo ficou na fila, e
	// rt.Run o consumiria agora, atrasado — ver descartarPendentes.
	if n := descartarPendentes(hk.Events()); n > 0 {
		slog.Default().Info("hook: descartei as teclas apertadas durante a subida", "eventos", n)
	}

	slog.Default().Info("agent: rodando; segure a tecla de ditado para gravar",
		"versao", version, "encerrar", "menu da bandeja ou Ctrl+C")
	if err := rt.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("runtime: %w", err)
	}
	return nil
}

// passoSemBandeja é o que run faz quando a bandeja não subiu.
type passoSemBandeja int

const (
	// semBandejaFalhar: sem console e sem bandeja, o Agent ficaria
	// rodando invisível, com a tecla de ditado capturada e nenhum jeito de
	// encerrar a não ser pelo Gerenciador de Tarefas. Não subir é melhor.
	// É o valor zero de propósito.
	semBandejaFalhar passoSemBandeja = iota
	// semBandejaSeguir: desenvolvimento, rodado de um terminal. O Ctrl+C
	// continua funcionando, e seguir sem ícone é aceitável.
	semBandejaSeguir
	// semBandejaSairCalado: o encerramento já foi pedido.
	semBandejaSairCalado
)

// semBandeja decide o passo. encerrando diz se o contexto de run já foi
// cancelado quando a bandeja falhou.
//
// # Por que o encerramento pedido vem antes de tudo
//
// Durante as tentativas do ícone, um "taskkill" sem /F cancela o
// contexto: o WM_CLOSE dele cai na pílula do overlay, que o repassa ao
// mesmo encerramento do Sair (overlay.AoPedirFechar), e um WM_CLOSE
// postado direto na janela da bandeja vira Sair do mesmo jeito. Mas run
// continua esperando a bandeja, e se o ícone nunca entrava, o caminho era
// o de falhar: quem tinha pedido para fechar recebia uma janela de erro
// (revisão de 29-set-2026). Com o encerramento pedido, a bandeja que
// falhou não importa mais.
func semBandeja(encerrando, console bool) passoSemBandeja {
	switch {
	case encerrando:
		return semBandejaSairCalado
	case console:
		return semBandejaSeguir
	default:
		return semBandejaFalhar
	}
}

// descartarPendentes esvazia o que já está na fila de ch, sem esperar por
// mais, e diz quantos eventos descartou.
//
// # Por que run descarta as teclas da subida
//
// O hook é instalado no começo de run, e rt.Run só começa a consumir a
// fila no fim — depois do warmup do ASR e das tentativas do ícone. Um
// Ctrl+Win apertado nesse meio tempo não dava sinal nenhum, e era
// reprocessado atrasado quando rt.Run começava: um toque completo virava
// um "tap" que abortava a captura, e uma tecla ainda segurada marcava o
// início do áudio AGORA, cortando o começo do que foi dito sem aviso
// (revisão de 29-set-2026). Tecla apertada antes de o Agent estar pronto
// nunca vira tentativa.
//
// Se a tecla ainda estiver segurada, o keyup dela chega depois, sem
// tentativa em curso, e o runtime já o descarta (winruntime.handleRaw).
func descartarPendentes[T any](ch <-chan T) int {
	n := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return n // fechado: nada mais vai chegar
			}
			n++
		default:
			return n
		}
	}
}

// registroDoFoco decide em que nível, e com que campos, uma inspeção de
// foco vai para o log ("uia: foco inspecionado"). Nada aqui diz o que há
// na janela: DiagnosticoFoco não carrega texto (ver
// uia.TestDiagnosticoFoco_SoTemCamposQueNaoCarregamTexto), salvo o
// nome-base do executável, que só sai na linha WARN (exceção autorizada
// pelo Mauro em 29-set-2026).
//
// Todo bloqueio precisa ser visível, não só o falso positivo: se um
// bloqueio legítimo saísse em Debug (que o handler padrão do slog nem
// imprime), "não consigo ditar" voltaria a ser uma tela sem log nenhum —
// o problema que este observador existe para resolver.
//
// A volta de protegido para livre também sai em Info, com quanto tempo o
// cache ficou protegido (protegido_por). Até 29-set-2026 ela saía em
// Debug como qualquer inspeção OK, e o log não dizia se um WARN tinha
// recusado o ditado por um segundo ou por dez minutos.
func registroDoFoco(d uia.DiagnosticoFoco) (slog.Level, []any) {
	nivel := slog.LevelDebug
	switch {
	case d.Descartada:
		// Não mexeu no cache, então não é bloqueio — mas é a única prova
		// de que o callback foi reentrado, e isso nunca foi visto.
		nivel = slog.LevelInfo
	case d.NoProprioAgent:
		// Também não mexeu no cache: a reinspeção leu o próprio Agent (o
		// menu da bandeja) e parou ali. O protegido e o outcome desta
		// linha são do elemento do Agent, não do cache — que continua com
		// o WARN anterior. Sai em Info para explicar por que aquele WARN
		// não teve volta a livre.
		nivel = slog.LevelInfo
	case d.Protected && d.Outcome != uia.InspectOK:
		nivel = slog.LevelWarn // bloqueio por dúvida: falso positivo
	case d.Protected:
		nivel = slog.LevelInfo // campo de senha de verdade
	case d.Liberou:
		nivel = slog.LevelInfo // o cache voltou a livre
	case d.Atrasada:
		// Leitura completa acima do orçamento que valeu (decisão de
		// 29-set-2026): não é bloqueio, mas até ali seria um WARN de
		// timeout. Info, para o log dizer quantas vezes isso acontece.
		nivel = slog.LevelInfo
	}
	args := []any{
		"outcome", d.Outcome.String(),
		"protegido", d.Protected,
		"is_password", d.IsPassword,
		"style_senha", d.StyleSenha,
		"duracao", d.Duracao,
		"lenta_em", d.LentaEm.String(),
		"lenta_duracao", d.LentaDuracao,
		"inicial", d.Inicial,
	}
	// falha/hresult só aparecem quando há o que dizer. Num outcome=ok ou
	// timeout eles sairiam como "nenhuma 0x00000000" e mandariam quem lê o
	// log procurar um código que não existe.
	if d.Falha != uia.FalhaNenhuma {
		args = append(args,
			"falha_em", d.Falha.String(),
			"hresult", fmt.Sprintf("0x%08X", uint32(d.HRESULT)))
	}
	if d.Liberou {
		args = append(args, "protegido_por", d.ProtegidoPor)
	}
	if d.Descartada {
		args = append(args, "descartada", true)
	}
	if d.NoProprioAgent {
		args = append(args, "no_proprio_agent", true)
	}
	// O vigia: esta inspeção passou do orçamento sem terminar, e o cache
	// ficou protegido enquanto ela rodava (uia.Tracker, "O vigia"). Numa
	// volta a livre, protegido_por conta esse intervalo.
	if d.Vigia {
		args = append(args, "vigia", true)
	}
	if d.Atrasada {
		args = append(args, "leitura_atrasada", true)
	}
	// Só depois de uma inspeção sem elemento se olha a janela em primeiro
	// plano (uia.Elevacao): sim, nao, ou desconhecida — esta última quando o
	// token dela não abriu, e a pílula fica com "campo protegido".
	if d.Elevacao != uia.ElevacaoNaoVerificada {
		args = append(args, "janela_elevada", d.Elevacao.String())
	}
	// O nome-base do executável em foco (ex.: brave.exe), SÓ na linha WARN.
	// Exceção à regra do CLAUDE.md de não registrar contexto de janela,
	// autorizada pelo Mauro em 29-set-2026: sem ela, o log de um
	// travamento nunca diz qual aplicativo travou. Nunca título, classe,
	// caminho ou texto — o uia.DiagnosticoFoco só traz o nome-base, e só
	// num aviso —, e nunca em Info/Debug, mesmo que o campo venha
	// preenchido (uma inspeção descartada, por exemplo, sai em Info).
	if nivel == slog.LevelWarn && d.Executavel != "" {
		args = append(args, "executavel", d.Executavel)
	}
	// A reinspeção depois de uma dúvida: qual tentativa esta foi, e para
	// quando a próxima ficou agendada. Um WARN sem reinspeciona_em não será
	// reinspecionado — foi a última tentativa, ou a inspeção passou de
	// uia.TetoParaReinspecionar (a duracao diz) — e o cache fica protegido
	// até a próxima troca de foco.
	if d.Tentativa > 0 {
		args = append(args, "reinspecao", d.Tentativa)
	}
	if d.ReinspecaoEm > 0 {
		args = append(args, "reinspeciona_em", d.ReinspecaoEm)
	}
	return nivel, args
}

// registroDaAutomacao decide como o objeto de UI Automation criado no
// Start vai para o log ("uia: automação"): qual objeto, e com que teto por
// chamada (uia.TetoChamadaUIA, decisão de 29-set-2026).
//
// Sai em WARN quando o teto não está inteiro — o CUIAutomation antigo, sem
// teto nenhum, ou um put_ que falhou —, porque aí uma chamada presa volta a
// poder durar o que durava antes (um minuto em 29-set-2026), e quem lê o
// log de um travamento precisa saber disso primeiro.
func registroDaAutomacao(a uia.Automacao) (slog.Level, []any) {
	nivel := slog.LevelInfo
	if !a.ComTeto() {
		nivel = slog.LevelWarn
	}
	args := []any{
		"objeto", a.Objeto.String(),
		"teto_conexao", a.TetoConexao,
		"teto_transacao", a.TetoTransacao,
	}
	if a.HRESULT8 != 0 {
		args = append(args, "hresult_cuiautomation8", fmt.Sprintf("0x%08X", uint32(a.HRESULT8)))
	}
	if a.HRESULTConexao != 0 {
		args = append(args, "hresult_teto_conexao", fmt.Sprintf("0x%08X", uint32(a.HRESULTConexao)))
	}
	if a.HRESULTTransacao != 0 {
		args = append(args, "hresult_teto_transacao", fmt.Sprintf("0x%08X", uint32(a.HRESULTTransacao)))
	}
	return nivel, args
}

// escolherDono decide o dono do histórico e do painel, e o registra no
// log.
//
// # O dono fixo
//
// Decisão do Mauro, 29-set-2026: "dono local fixo, migrando tudo pra esse
// dono e fazer a cópia antes de migrar". Até ali o dono era o "usuario" do
// dicionário, ou "local" sem ele — e trocar o dicionário escondia do
// painel todo o histórico anterior, porque toda listagem é escopada pelo
// dono. Com o projeto aberto, quem instalasse sem o dicionário do Mauro
// cairia nisso ao criar o seu.
//
// Agora o dono é gerado uma vez por instalação e gravado no próprio
// historico.db (storage.GarantirDonoFixo). Na primeira subida com esta
// versão, todos os ditados antigos — "mauro", "local", "" — migram para
// ele, depois de uma cópia do banco feita ao lado dele. A linha INFO da
// migração diz quantas linhas cada tabela teve, e onde ficou a cópia;
// nunca texto.
//
// # Quando a migração não roda
//
// A cópia falhou, ou a transação (storage.ErrSemDonoFixo): o banco fica
// como estava, SEM dono, e NESTA execução vale a regra antiga
// (donoPelaRegraAntiga). Usar um dono novo sem as linhas migradas
// mostraria o painel vazio — exatamente o que a decisão veio evitar. A
// próxima subida tenta de novo.
//
// Qualquer outra falha — o banco tem um dono gravado que não se lê, ou a
// leitura dele deu erro — devolve erro, e o Agent sobe SEM histórico. A
// regra antiga ali gravaria ditados com um dono que o painel, na subida
// seguinte, não mostraria nunca mais; e a migração não roda de novo sobre
// um banco com dono.
//
// # Linhas de outro dono
//
// Com o dono gravado, nada migra de novo. Uma linha gravada depois com
// outro user_id — um Agent anterior ao dono fixo, rodando sobre o mesmo
// banco — fica fora do painel. Toda subida as conta e, se houver, avisa em
// WARN (só contagens). Migrá-las é decisão do Mauro, com cópia antes.
func escolherDono(ctx context.Context, log *slog.Logger, db *sql.DB, caminhoBanco, usuarioDoDicionario string, agora time.Time) (string, error) {
	r, err := storage.GarantirDonoFixo(ctx, db, storage.CaminhoDaCopia(caminhoBanco, agora))
	if errors.Is(err, storage.ErrSemDonoFixo) {
		log.Warn("historico: não migrei para o dono fixo; nesta execução vale a regra antiga, e a próxima subida tenta de novo", "err", err)
		return donoPelaRegraAntiga(log, usuarioDoDicionario), nil
	}
	if err != nil {
		return "", fmt.Errorf("lendo o dono fixo gravado no banco (a regra antiga gravaria ditados que o painel não mostraria): %w", err)
	}
	if r.Migrou {
		log.Info("historico: ditados migrados para o dono fixo", camposDaMigracao(r)...)
	}
	log.Info("historico: dono dos ditados e do painel", "usuario", r.Dono, "origem", "o dono fixo desta instalação, gravado no historico.db")
	avisarLinhasDeOutroDono(ctx, log, db, r.Dono)
	return r.Dono, nil
}

// avisarLinhasDeOutroDono registra em WARN quantas linhas, por tabela, não
// são do dono fixo — as que o painel não mostra (ver escolherDono). Nada
// quando não há nenhuma. Nunca texto.
func avisarLinhasDeOutroDono(ctx context.Context, log *slog.Logger, db *sql.DB, dono string) {
	fora, err := storage.LinhasDeOutroDono(ctx, db, dono)
	if err != nil {
		log.Warn("historico: não consegui conferir se há linhas de outro dono no banco", "err", err)
		return
	}
	if len(fora) == 0 {
		return
	}
	log.Warn("historico: há linhas de outro dono, fora do painel; nada as migra sozinho",
		"causa_provavel", "um Agent anterior ao dono fixo rodou sobre este historico.db",
		porTabela("linhas", fora))
}

// camposDaMigracao são os campos da linha INFO da migração: o dono, a
// cópia, e as linhas migradas por tabela, em ordem de nome.
func camposDaMigracao(r storage.DonoFixo) []any {
	// Só o nome da cópia, que fica ao lado do banco: o caminho completo leva
	// o nome de usuário do Windows para o log.
	copia := filepath.Base(r.Copia)
	if r.Copia == "" {
		copia = "nenhuma: o banco não tinha linha a migrar"
	}
	return []any{"dono", r.Dono, "copia", copia, porTabela("linhas", r.Linhas)}
}

// porTabela põe uma contagem por tabela num grupo do log, em ordem de
// nome.
func porTabela(nome string, n map[string]int64) slog.Attr {
	var campos []any
	for _, t := range slices.Sorted(maps.Keys(n)) {
		campos = append(campos, t, n[t])
	}
	return slog.Group(nome, campos...)
}

// donoPelaRegraAntiga é o dono de antes do dono fixo — o usuário do
// dicionário, ou "local" sem ele —, que só vale quando a migração para o
// dono fixo não pôde rodar (ver escolherDono). Registra no log com qual
// dono o Agent subiu, e por quê.
//
// "local" e não vazio: toda listagem do histórico é escopada por usuário
// e recusa o vazio, e localapi.New entra em pânico com ele. Num Agent sem
// console, pânico é um fechamento silencioso.
func donoPelaRegraAntiga(log *slog.Logger, usuarioDoDicionario string) string {
	dono, origem := usuarioDoDicionario, "o usuario do "+nomeDicionario+" (regra antiga)"
	if dono == "" {
		dono, origem = usuarioDoDicionarioNovo, "o padrão, sem usuario no "+nomeDicionario+" (regra antiga)"
	}
	log.Info("historico: dono dos ditados e do painel", "usuario", dono, "origem", origem)
	return dono
}

// abrirASRLocal resolve o modelo na pasta do programa, sobe o servidor
// residente e devolve o provider pronto.
//
// # Por que na pasta do programa
//
// Decisão de 18-set-2026: o instalador embarca o q4_0 de 139 MB pronto. O
// caminho é relativo ao .exe, e não ao diretório de trabalho, porque o
// Agent roda a partir de qualquer lugar (atalho, autostart, terminal) e o
// diretório de trabalho não é confiável para achar arquivo de instalação.
// O modelo, o servidor e as DLLs são do PROGRAMA; só a saída do servidor
// (whisper-server.log) é gravada, e vai para a pasta de DADOS (caminhosDe).
//
// # O log do servidor não leva o nome do usuário
//
// Duas medidas, e nenhuma basta sozinha. O servidor roda com a pasta de
// trabalho no programa e o modelo pelo nome relativo (configDoServidor), o que
// tira o caminho da linha do modelo. Mas a linha da biblioteca do processador
// (load_backend: loaded CPU backend from <pasta do executável>) é do servidor, e
// a pasta dele é a do programa instalado, com o nome do usuário dentro: essa
// quem tira é o filtro de caminhos da saída (abrirLogDoServidor, com pp e o
// perfil). Os dois são conferidos em teste, e o log de um servidor de verdade
// ainda não foi lido depois da mudança (roteiro manual do instalador, I5).
func abrirASRLocal(cam caminhosDoAgent, pp pastas.Pastas, perfil string) (*local.Provider, func(), error) {
	caminhoModelo, err := model.Resolve(cam.Programa, model.SmallQ4_0)
	if err != nil {
		return nil, nil, fmt.Errorf("modelo de ASR: %w", err)
	}

	cfg := configDoServidor(cam, caminhoModelo)

	// A saída do whisper-server vai para um arquivo PRÓPRIO, sobrescrito
	// a cada subida. Sem console ela iria para um handle inválido — e ela
	// é a única pista quando o servidor não sobe. Separada do agent.log
	// porque são centenas de linhas de carregamento do modelo por subida,
	// que afogariam o log do Agent. Com os caminhos de quem usa trocados por
	// rótulos, como no agent.log: ver abrirLogDoServidor.
	saidaServidor, fecharSaida := abrirLogDoServidor(cam.LogDoServidor, pp, perfil)
	// Nula se o arquivo não abriu (uma interface nula de verdade): o servidor
	// cai no stderr do Agent, como antes.
	cfg.SaidaDoServidor = saidaServidor

	p := local.New(cfg, model.SmallQ4_0.Nome)

	// Warmup agora, e não no primeiro ditado: carregar o modelo custa
	// segundos, e pagá-los no primeiro uso faria a primeira fala da sessão
	// parecer travada.
	ctx, cancel := context.WithTimeout(context.Background(), local.DefaultReadyTimeout)
	defer cancel()
	if err := p.Warmup(ctx); err != nil {
		fecharSaida()
		return nil, nil, fmt.Errorf("subindo o ASR local (detalhes em whisper-server.log): %w", err)
	}

	fechar := func() {
		if err := p.Close(); err != nil {
			slog.Default().Warn("asr: Close falhou", "err", err)
		}
		fecharSaida()
	}
	return p, fechar, nil
}

// configDoServidor monta a configuração do whisper-server. Pura (só junta o
// que a tabela de caminhos e o modelo já resolvido dizem), para o teste
// conferir o que o servidor recebe sem subi-lo.
//
// # A pasta de trabalho é a do programa (ADR-018, F2)
//
// O servidor roda com cmd.Dir na pasta do PROGRAMA (Dir: cam.Programa), a
// mesma do executável, das DLLs e do modelo, e recebe o modelo pelo nome
// relativo a ela (local.Config.ModelPath/Dir). Assim o Agent instalado funciona
// de qualquer pasta atual (atalho do Menu Iniciar, início automático,
// terminal), e a linha do whisper-server.log que trazia o caminho do modelo —
// com o nome do usuário do Windows — passa a trazer só o nome do arquivo. É só
// UMA das duas linhas com caminho: a da biblioteca do processador traz a pasta
// do executável do servidor, que a pasta de trabalho não muda; essa sai pelo
// filtro da saída (abrirLogDoServidor). A saída do servidor, essa sim gravável,
// vai para a pasta de DADOS (cam.LogDoServidor, em abrirASRLocal).
func configDoServidor(cam caminhosDoAgent, caminhoModelo string) local.Config {
	return local.Config{
		ServerPath: cam.Servidor,
		Dir:        cam.Programa,
		ModelPath:  caminhoModelo,
		Language:   "pt",
		Threads:    local.ThreadsRecomendadas(),
		// A janela do encoder é escolhida por requisição, pela duração do
		// áudio: reduzida (768) na fala curta, inteira acima de 12 s. O
		// -ac 750 fixo na linha de comando foi medido só em fala curta
		// (docs/benchmarks/asr-2026-09-18-cpu-otimizado.md) e quebrava o
		// ditado longo (docs/defeito-ditado-longo.md). Sem -ac, quem não
		// mandar o campo cai na janela inteira, a segura.
		Janela: local.JanelaRecomendada,
		// Sem flash attention (-nfa): com ela, alternar a janela entre 768 e
		// a inteira fazia o texto do ditado longo depender do ditado curto
		// anterior (6 de 6 áudios). Sem ela, 6 de 6 iguais e o menor erro
		// medido; custa a fala curta passar de 0,59 s para 0,76 s (p50).
		// Decisão do Mauro, 01-out-2026: docs/benchmarks/asr-longo-2026-10-01-nfa.md.
		SemFlashAttention: true,
	}
}

// nomeDicionario é o arquivo do dicionário pessoal. Mora na pasta de DADOS
// (caminhosDe), e não na do programa: o painel grava nele o que a pessoa
// ensina (etapa 3), então precisa de pasta gravável. O Agent o acha pelo
// caminho das pastas, e não pelo diretório de trabalho, que não é confiável:
// ele abre por atalho, autostart ou terminal.
const nomeDicionario = "dicionario.json"

// abrirDicionarioEm carrega o dicionário pessoal de caminho e devolve o
// resolver Smart, o vocabulário que vai enviesar o ASR, e o usuário dono do
// dicionário. O caminho vem de caminhosDe; os testes passam uma pasta
// temporária, nunca o dicionario.json de quem usa.
//
// Ausência de dicionário NÃO é erro: o ditado funciona sem ele, só sem
// correção de termo. Fazer o Agent recusar subir por falta de um arquivo
// opcional trocaria uma degradação suave por uma falha dura. (Na subida do
// Agent, quem chega aqui sem arquivo é porque garantirDicionario não conseguiu
// criar o vazio; ver dicionario_vazio.go.)
//
// Sem dicionário, o Smart volta mesmo assim, com o dicionário NULO (ADR-016,
// decisão 13): a autocorreção roda, e o Polish, que parte do texto do
// Smart, fica alcançável. Nulo, e não um repositório vazio: a busca num
// repositório exige usuário, e sem arquivo não há usuário — cada consulta
// das guardas do Polish falharia, e o Polish cairia sempre no Smart com o
// motivo dicionario. Com o nulo, o Smart grava um aviso fixo no resultado
// ("sem DictionaryRepository"), e a linha "resolucao aplicada" sai a cada
// ditado com ele — sem texto.
//
// Arquivo PRESENTE mas inválido é erro, e deliberadamente: significa que
// alguém tentou configurar um dicionário e ele não vai funcionar —
// exatamente o caso em que falhar em silêncio é pior.
//
// A carga é a da subida. Depois dela, o que o painel aprende ou adiciona é
// gravado no mesmo arquivo, e o dicionário em memória e o vocabulário do ASR
// são trocados na hora (aprendizadoDoAgent, em aprendizado.go; ADR-020).
func abrirDicionarioEm(log *slog.Logger, caminho string) (*resolver.SmartResolver, string, string, func(string) *string, error) {
	f, err := os.Open(caminho)
	if err != nil {
		if os.IsNotExist(err) {
			// Sem o caminho: ele leva o nome de usuário do Windows para o log.
			log.Info("dicionario: nenhum encontrado na pasta de dados; o Smart segue só com a autocorreção, sem correção de termos",
				"arquivo", nomeDicionario)
			return resolver.NewSmartResolver(nil), "", "", nil, nil
		}
		return nil, "", "", nil, fmt.Errorf("abrindo %s: %w", caminho, err)
	}
	defer f.Close()

	semente, err := dictionary.CarregarSemente(f)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("lendo %s: %w", caminho, err)
	}
	smart, err := smartconta.SmartDaSemente(semente)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("carregando %s: %w", caminho, err)
	}

	entradas := semente.ParaEntradas()
	noPrompt := dictionary.VocabularioTermos(entradas, dictionary.OrcamentoRunasPadrao)
	vocab := dictionary.Vocabulario(entradas, dictionary.OrcamentoRunasPadrao)
	log.Info("dicionario: carregado",
		"termos", len(semente.Entradas),
		"termos_no_prompt", len(noPrompt),
		"usuario", semente.Usuario,
		"vocabulario_runas", len([]rune(vocab)),
		"orcamento_runas", dictionary.OrcamentoRunasPadrao,
		"dominios", len(semente.Dominios))

	// O corte por orçamento é normal e projetado, mas era SILENCIOSO: a
	// linha acima sozinha se lia como "mandei os 44", quando foram 17.
	// Um termo que nunca chega ao ASR e um termo que chega e o modelo
	// ignora são problemas opostos, e o log não os distinguia.
	//
	// menor_prioridade_no_prompt é o número acionável: é o piso que um
	// termo precisa passar para entrar no prompt deste dicionário.
	//
	// O denominador são as entradas ATIVAS, não todas: uma entrada
	// desativada não disputou o orçamento, e contá-la como "cortada"
	// culparia o orçamento por uma decisão de quem usa. O termo que o painel
	// criou (AliasOnly) também não disputou: o vocabulário o deixa de fora
	// por desenho, e um dicionário só com termos do painel daria "NENHUM termo
	// coube" sem ser verdade. Nem o termo que a pessoa adicionou sem a dica
	// (ADR-020). A regra é uma só, a do vocabulário: dictionary.DisputaOPrompt.
	ativas := 0
	for _, e := range entradas {
		if dictionary.DisputaOPrompt(e) {
			ativas++
		}
	}
	if cortados := ativas - len(noPrompt); cortados > 0 && len(noPrompt) > 0 {
		log.Info("dicionario: o orçamento cortou termos do prompt do ASR",
			"cortados", cortados,
			"menor_prioridade_no_prompt", noPrompt[len(noPrompt)-1].Priority)
	} else if len(noPrompt) == 0 && ativas > 0 {
		log.Warn("dicionario: NENHUM termo coube no prompt do ASR",
			"ativas", ativas, "orcamento_runas", dictionary.OrcamentoRunasPadrao)
	}

	return smart, vocab, semente.Usuario, semente.DominioDoApp, nil
}

// avisarConflitoDeDitado procura outros programas de ditado em execução
// e registra um aviso para cada um.
//
// Nasceu de um episódio real: em 27-set-2026 o Wispr Flow ficou rodando
// esquecido depois de um teste, usando o MESMO atalho. Os dois gravavam
// e os dois inseriam, e o campo recebia o texto duas vezes.
//
// O sintoma é péssimo de diagnosticar justamente porque tudo do lado de
// cá fica correto — o ASR devolvia 20 runas, o inserter injetava 20, o
// histórico registrava 20. Sem este aviso, a única pista é o texto
// duplicado, e não há nada no log ligando uma coisa à outra.
//
// Nunca impede a subida: dois programas de ditado no mesmo atalho podem
// ser intencional (comparar os dois lado a lado é exatamente o que
// estava sendo feito quando isso aconteceu). A decisão é de quem usa; o
// trabalho aqui é que ela seja informada.
//
// Falha na enumeração é ignorada em silêncio: não saber se há outro
// programa de ditado é bem menos grave que não ter ditado.
func avisarConflitoDeDitado(log *slog.Logger) {
	processos, err := conflito.Listar()
	if err != nil {
		log.Debug("conflito: não consegui listar processos", "err", err)
		return
	}
	for _, p := range conflito.Detectar(processos) {
		log.Warn("conflito: " + p.Aviso())
	}
}

// historicoAberto é o histórico pronto que abrirHistoricoComRetencao devolve:
// o repositório, as preferências, o dono, o ciclo do expurgo (o dono do Ligar,
// do Desligar e do apagar das cópias dos 6 meses, que compartilham o mutex da
// rodada; a Local API o recebe em Preferencias.Retencao) e a função de fechar.
type historicoAberto struct {
	hist     *storage.SQLiteTranscriptionRepository
	prefs    *storage.SQLiteSettingsRepository
	dono     string
	retencao *expurgo.Ciclo
	fechar   func()
}

// abrirHistoricoComRetencao abre (ou cria) o banco local do histórico em
// caminho — na pasta de DADOS (caminhosDe) — e devolve o histórico pronto
// (historicoAberto). Os testes passam bancos temporários, nunca o historico.db
// de quem usa. A hora da subida (nome da cópia de segurança do dono fixo) e o
// ciclo do expurgo saem do mesmo relógio. É a única que run chama
// (caminhos_test.go confere que o caminho é cam.Historico); os testes que só
// querem o repositório usam abrirHistoricoEm (historico_helper_test.go).
//
// # Por que SQLite, e não a Local API
//
// O cmd/dictlearn precisa do texto BRUTO do ASR para aprender da forma
// que a máquina de fato produziu, em vez da que a pessoa lembra. O
// caminho aparentemente óbvio — dictlearn falando com a Local API do
// Agent — está fechado: a API exige token, e o token só existe em
// memória, nunca em disco nem em log (09-security-privacy.md, e o
// comentário de agent/guard.NewToken). Publicá-lo num arquivo para o
// dictlearn ler violaria a decisão de segurança.
//
// O banco resolve sem abrir superfície nova: os dois processos leem o
// mesmo arquivo, protegido pela permissão do sistema de arquivos. E é o
// alvo da arquitetura de qualquer forma — o AC-011 pede histórico local.
//
// Falhar aqui NÃO derruba o Agent: ditar sem histórico é degradação;
// não ditar é perda.
//
// Devolve também o dono dos ditados e do painel (escolherDono), que mora
// no próprio banco. usuarioDoDicionario só é usado se a migração para o
// dono fixo não puder rodar.
//
// As preferências da tela de Configurações moram no mesmo banco
// (local_settings) e voltam junto. A chave de API, não: essa mora no
// Gerenciador de Credenciais (internal/agent/chaves).
//
// Abrir o histórico já liga o expurgo (internal/agent/expurgo): a lixeira, e
// os 6 meses depois do OK de quem usa (ADR-019, "Decisões da F10"). Quem abre
// o banco não tem como esquecer dele, e a função de fechar devolvida
// interrompe o expurgo ANTES de fechar o banco.
func abrirHistoricoComRetencao(log *slog.Logger, caminho, usuarioDoDicionario string, relogio clock.Clock) (historicoAberto, error) {
	db, err := abrirBancoDoHistorico(caminho)
	if err != nil {
		return historicoAberto{}, err
	}

	if err := storage.NewMigrator().Migrate(context.Background(), db); err != nil {
		db.Close()
		return historicoAberto{}, fmt.Errorf("migrando %s: %w", caminho, err)
	}
	// Sem o caminho: ele leva o nome de usuário do Windows para o log.
	log.Info("historico: banco pronto", "arquivo", filepath.Base(caminho))
	dono, err := escolherDono(context.Background(), log, db, caminho, usuarioDoDicionario, relogio.Now())
	if err != nil {
		db.Close()
		return historicoAberto{}, err
	}
	hist := storage.NewSQLiteTranscriptionRepository(db)
	// O expurgo liga só depois do dono: se a migração para o dono fixo
	// tirou cópia do banco, ela foi tirada antes de qualquer linha sair. Os
	// 6 meses leem o dono fixo eles mesmos: sem ele (a regra antiga do
	// escolherDono), o passo pula.
	ciclo := expurgo.Iniciar(relogio, hist, hist.SeisMeses(caminho), log)
	return historicoAberto{
		hist: hist, prefs: storage.NewSQLiteSettingsRepository(db), dono: dono, retencao: ciclo,
		fechar: func() { ciclo.Parar(); db.Close() },
	}, nil
}

// abrirBancoDoHistorico abre o historico.db em caminho com uma conexão só e
// o secure_delete ligado.
//
// Uma conexão só: o PRAGMA foreign_keys que o Migrator liga vale POR
// CONEXÃO, e com pool maior uma escrita poderia sair por uma conexão que não
// o tem. O Agent escreve uma linha por ditado — não há o que ganhar com
// concorrência aqui.
//
// O texto apagado sai de verdade do arquivo (ADR-019, "Decisões da F10";
// escolha menor 6): sem o secure_delete, o DELETE deixa o texto nas páginas
// livres do historico.db (o auto_vacuum está desligado e o arquivo não
// encolhe), e nem a lixeira nem os 6 meses valeriam no disco.
//
// Os dois pragmas vão no DSN, e não num Exec depois de abrir: valem por
// conexão, e "uma conexão só" não quer dizer "a mesma conexão para sempre". O
// modernc descarta a conexão que teve uma consulta interrompida por contexto
// cancelado (a busca do histórico que o painel aborta, a aba fechada no meio de
// um pedido), e o database/sql abre outra pelo DSN; com o Exec, ela viria sem
// os dois até o Agent reiniciar (TestRetencao_OsPragmasValemDepoisDeUmaConsultaCancelada).
//
// O caminho vai como nome de arquivo, sem "file:" (como sempre foi): o modernc
// corta o DSN no primeiro '?', que um caminho do Windows não pode ter.
func abrirBancoDoHistorico(caminho string) (*sql.DB, error) {
	if strings.ContainsRune(caminho, '?') {
		return nil, fmt.Errorf("abrindo %s: o caminho do banco não pode ter '?'", filepath.Base(caminho))
	}
	db, err := sql.Open("sqlite", caminho+"?_pragma=secure_delete(1)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("abrindo %s: %w", caminho, err)
	}
	db.SetMaxOpenConns(1)
	// Abre a primeira conexão agora: um pragma que falha aparece aqui, e não
	// na migração.
	var ligado int
	if err := db.QueryRow(`PRAGMA secure_delete`).Scan(&ligado); err != nil || ligado != 1 {
		db.Close()
		if err == nil {
			err = fmt.Errorf("secure_delete = %d", ligado)
		}
		return nil, fmt.Errorf("ligando o secure_delete em %s: %w", filepath.Base(caminho), err)
	}
	return db, nil
}

// esperaParaReler é quanto carregarPreferencias espera antes de ler de novo
// quando a primeira leitura falhou.
const esperaParaReler = 200 * time.Millisecond

// carregarPreferencias aplica, na subida, o que foi gravado pela tela de
// Configurações (aplicarPreferencias). Sem nada gravado, valem as de
// defaultConfig, com que o Machine já nasceu, e o microfone sob demanda
// de configDeAudio.
//
// Uma leitura que falha tenta de novo UMA vez, depois de esperaParaReler.
// Desistir no primeiro erro passageiro (o banco ocupado na subida) deixava
// o Agent com as padrão — o microfone sob demanda — enquanto o banco dizia
// "sempre pronto", e nada na tela indicava a diferença. dormir é
// time.Sleep; o teste troca.
func carregarPreferencias(repo storage.SettingsRepository, aplicar func(storage.Settings), log *slog.Logger, dormir func(time.Duration)) {
	salvas, err := repo.Get(context.Background())
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		log.Warn("preferencias: não consegui ler; tento de novo", "err", err, "espera_ms", esperaParaReler.Milliseconds())
		dormir(esperaParaReler)
		salvas, err = repo.Get(context.Background())
	}
	switch {
	case errors.Is(err, storage.ErrNotFound):
		log.Info("preferencias: nenhuma gravada; valem as padrão")
	case err != nil:
		log.Warn("preferencias: não consegui ler nem tentando de novo; valem as padrão, com o microfone sob demanda", "err", err)
	default:
		aplicar(salvas)
		log.Info("preferencias: carregadas",
			"hold_threshold_ms", salvas.HoldThresholdMs, "beep", salvas.BeepEnabled,
			"microfone_sempre_pronto", salvas.MicrophoneAlwaysReady,
			"modo", salvas.TextMode, "provedor_ia", salvas.AIProvider, "version", salvas.Version)
	}
}

// modoDoMicrofone é o que as preferências mudam na captura: o "microfone
// sempre pronto" (ADR-015). *audio.Session o cumpre.
type modoDoMicrofone interface {
	DefinirSemprePronto(bool)
	Modo() string
	// Falha diz se a sessão de áudio morreu de vez (audio.Session.Falha).
	Falha() error
}

// aplicarPreferencias leva o que foi gravado a quem usa, na subida e a
// cada Salvar do painel.
//
// O runtime aplica no próximo ditado (Runtime.Ajustar). O modo do
// microfone vale NA HORA: quem liga o "sempre pronto" espera o microfone
// aceso ao salvar, e quem desliga, apagado. Não precisa esperar como o
// runtime: a Session é segura de qualquer goroutine, e um ditado em curso
// termina no modo do keydown dele — desligar no meio só estaciona depois
// do corte.
//
// É a exceção ao "tudo ou nada" do runtime (winruntime, aplicarAjuste):
// se o núcleo recusar o limiar no próximo keydown, o limiar, o beep e o
// modo de texto ficam como estavam, mas o microfone já mudou aqui. Aceito
// porque a recusa só existe como defeito — a API local valida o limiar e o
// modo de texto antes de gravar (localapi.validar), e o "sempre pronto" é
// um booleano que não tem o que validar.
//
// "audio: modo do microfone mudou" só sai quando a mudança valeu: com a
// sessão de áudio morta, o modo fica gravado, mas nenhum microfone liga
// até reiniciar o Agent — e o log diz isso, em vez de "mudou". Um Start
// que falhe DEPOIS do Salvar, com a sessão viva, é da própria sessão: ela
// entra no recuo e registra cada tentativa (audio.Session, M1).
func aplicarPreferencias(ajustar func(winruntime.Preferencias), mic modoDoMicrofone, log *slog.Logger) func(storage.Settings) {
	return func(s storage.Settings) {
		ajustar(preferenciasDoRuntime(s))
		antes := mic.Modo()
		mic.DefinirSemprePronto(s.MicrophoneAlwaysReady)
		agora := mic.Modo()
		if agora == antes {
			return
		}
		if err := mic.Falha(); err != nil {
			log.Warn("audio: o modo do microfone foi gravado, mas o microfone não responde; vale depois de reiniciar o Agent",
				"modo", agora, "err", err)
			return
		}
		log.Info("audio: modo do microfone mudou", "modo", agora)
	}
}

// abrirBandeja põe o Agent na área de notificação, com o menu da spec
// (08-ui-ux-spec.md, "Tray") menos o que ainda não existe:
// "configurações" espera a tela dela no painel, "reconectar" espera a
// nuvem. "Abrir" abre o painel — desabilitado quando ele não subiu
// (abrirPainel nil).
//
// A fonte da verdade de cada item fica onde ela já estava — a pausa no
// hook, o início automático no registro —, e a bandeja lê as duas a cada
// abertura do menu, em vez de guardar uma cópia que divergiria.
func abrirBandeja(hk *hook.Hook, provider string, inicio inicioComWindows, sair func(), abrirPainel func() error, nuvem func() string) (*tray.Tray, error) {
	log := slog.Default()
	return tray.Iniciar(tray.Acoes{
		Estado: func() tray.Estado {
			ligado, indisponivel := inicio.Estado()
			return tray.Estado{
				Pausado:            hk.Pausado(),
				IniciarComWindows:  ligado,
				InicioIndisponivel: indisponivel,
				Provider:           provider,
				PainelDisponivel:   abrirPainel != nil,
				// Só memória: o laço de sync guarda a linha; o menu não
				// espera banco nem rede para abrir.
				Nuvem: nuvem(),
			}
		},
		Pausar: func(p bool) {
			hk.Pausar(p)
			log.Info("tray: ditado pausado", "pausado", p)
		},
		IniciarComWindows: inicio.Definir,
		Sair: func() {
			log.Info("tray: encerrando a pedido de quem usa")
			sair()
		},
		Abrir: abrirPainel,
		// Não só o menu: o ícone recusado depois de o Explorer reiniciar
		// também chega aqui (ver tray.Acoes.AoFalhar).
		AoFalhar: func(err error) { log.Warn("tray: falhou", "err", err) },
		// A outra metade do aviso acima, quando ele foi o ícone recusado:
		// sem ela, o log parava em "tentando de novo".
		AoVoltar: func() { log.Info("tray: o ícone voltou à bandeja") },
	})
}

// Motivos que desabilitam "Iniciar com o Windows" — ver
// tray.Estado.InicioIndisponivel.
const (
	motivoSoNaBandeja      = "só na versão de bandeja"
	motivoRegistroIlegivel = "não consegui ler"
	motivoSemExecutavel    = "não achei o programa"
)

// inicioIndisponivel diz por que o item "Iniciar com o Windows" não pode
// ser usado, ou "" se pode.
//
// # Por que a build de console não mexe no início automático
//
// O roteiro manual manda gerar uma build de console, com outro nome, na
// mesma pasta — e ela também sobe a bandeja. O início automático é
// comparado com o PRÓPRIO executável (autostart.Aponta), então nela o
// item aparecia desmarcado com o início automático ligado para a build de
// uso diário; e o clique gravava a build de console no Run. No logon
// seguinte abria uma janela preta, e fechá-la matava o Agent (revisão de
// 29-set-2026). O Run só deve apontar para a build de bandeja.
//
// A build de console vem antes do registro ilegível: nela o registro nem
// é lido.
func inicioIndisponivel(console bool, errRegistro error) string {
	switch {
	case console:
		return motivoSoNaBandeja
	case errRegistro != nil:
		return motivoRegistroIlegivel
	default:
		return ""
	}
}

// inicioComWindows é o "Iniciar com o Windows" da bandeja E da tela de
// Configurações do painel: a mesma regra nos dois, num lugar só. A fonte
// da verdade é o registro, lido a cada pergunta — nenhum dos dois guarda
// cópia que divergiria.
type inicioComWindows struct {
	exe     string
	console bool
	log     *slog.Logger
}

// recusa diz por que nem se chega ao registro: a build de console (ver
// inicioIndisponivel), ou o caminho do executável que os.Executable não
// deu — sem ele não há o que comparar nem o que gravar no Run. "" = pode.
func (i inicioComWindows) recusa() string {
	if motivo := inicioIndisponivel(i.console, nil); motivo != "" {
		return motivo
	}
	if i.exe == "" {
		return motivoSemExecutavel
	}
	return ""
}

// Estado diz se o início automático está ligado para este executável e,
// quando não dá para mudar, por quê (ver inicioIndisponivel).
func (i inicioComWindows) Estado() (ligado bool, indisponivel string) {
	// A build de console nem lê: o que ela leria é sobre o executável
	// DELA, e não interessa — ver inicioIndisponivel.
	if motivo := i.recusa(); motivo != "" {
		return false, motivo
	}
	ligado, errLeitura := autostart.Ligado(i.exe)
	if errLeitura != nil {
		i.log.Warn("autostart: não consegui ler o registro", "err", errLeitura)
	}
	return ligado, inicioIndisponivel(false, errLeitura)
}

// Definir liga ou desliga. A bandeja e o painel já não oferecem a troca
// quando ela está indisponível; isto garante a regra mesmo que algum
// caminho chegue aqui sem passar por eles.
func (i inicioComWindows) Definir(ligar bool) error {
	if motivo := i.recusa(); motivo != "" {
		return fmt.Errorf("autostart: recusado (%s): %s", motivo, i.exe)
	}
	if ligar {
		if err := autostart.Ligar(i.exe); err != nil {
			return err
		}
		// Sem o caminho do executável (ele leva o nome de usuário do Windows
		// para o log): quem quer ver onde o Run aponta abre o Gerenciador de
		// Tarefas, em Inicializar.
		i.log.Info("autostart: ligado")
		return nil
	}
	if err := autostart.Desligar(); err != nil {
		return err
	}
	i.log.Info("autostart: desligado")
	return nil
}

// preferenciasPadrao é o que vale antes da primeira gravação pela tela de
// Configurações: os valores de defaultConfig, e o modo Smart (REQ-022).
// Os campos que a tela não mostra levam o que o Agent de fato faz.
func preferenciasPadrao() storage.Settings {
	cfg := defaultConfig()
	return storage.Settings{
		Hotkey:               teclaDeDitado,
		HoldThresholdMs:      cfg.HoldThreshold.Milliseconds(),
		BeepEnabled:          cfg.BeepEnabled,
		ASRPolicy:            "local_only",
		ProtectedFieldPolicy: "block",
		TextMode:             localapi.ModoSmart,
	}
}

// teclaDeDitado é como a tela mostra o gatilho de defaultConfig. Só
// leitura nesta etapa; TestPreferenciasPadrao_BatemComODefaultConfig
// confere que os dois dizem a mesma coisa.
const teclaDeDitado = "ctrl+win"

// aplicarNoRuntime é o localapi.Preferencias.Aplicar do Agent: aplica o
// que foi gravado (aplicarPreferencias), e nada fica para o próximo
// início — o que a tela muda vale a partir do próximo ditado, e o modo do
// microfone, na hora.
func aplicarNoRuntime(aplicar func(storage.Settings)) func(storage.Settings) []string {
	return func(s storage.Settings) []string {
		aplicar(s)
		return nil
	}
}

// preferenciasDoRuntime traduz o que foi gravado no que o runtime aplica:
// o modo (text_mode) e a escolha de IA (ai_provider, ai_model). A chave
// não passa por aqui: o Polish a lê do Gerenciador a cada ditado.
func preferenciasDoRuntime(s storage.Settings) winruntime.Preferencias {
	return winruntime.Preferencias{
		HoldThreshold: time.Duration(s.HoldThresholdMs) * time.Millisecond,
		BeepEnabled:   s.BeepEnabled,
		Literal:       s.TextMode == localapi.ModoLiteral,
		Polish:        s.TextMode == localapi.ModoPolish,
		Provedor:      s.AIProvider,
		Modelo:        s.AIModel,
	}
}

// subirPainel põe no ar a Local API e o painel, e devolve a função que
// abre o painel no navegador.
//
// Porta aleatória a cada subida (guard.Listen(0)), só em loopback: sem
// porta fixa não há conflito com outro programa, nem um alvo previsível
// para quem quisesse varrer a máquina. O token vive só nesta função e no
// handler — é passado ao navegador pelo bilhete (internal/agent/painel),
// e nunca vai para log.
//
// api é o handler da Local API (localapi.Server.Handler), montado em run.
// retorno é o GET /nuvem/retorno da nuvem (nuvem.Servico.Retorno): a volta
// da aprovação chega por esta mesma porta, sem ouvinte a mais.
// dir é a pasta painel\ do programa (caminhosDe.Painel): o Agent só a lê.
// Devolve também a porta, que a nuvem manda no pedido de pareamento.
func subirPainel(api, retorno http.Handler, dir string, log *slog.Logger) (func() error, int, func(), error) {
	lis, err := guard.Listen(0)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("abrindo porta local: %w", err)
	}
	token, err := guard.NewToken()
	if err != nil {
		lis.Close()
		return nil, 0, nil, fmt.Errorf("gerando token: %w", err)
	}
	porta := lis.Addr().(*net.TCPAddr).Port

	bilhetes := painel.NovosBilhetes()
	srv := &http.Server{
		Handler: painel.Handler(painel.Config{
			Porta: porta, Token: token, Bilhetes: bilhetes, API: api, DirPainel: dir, Retorno: retorno,
		}),
		// Sem isto, uma conexão que abre e nunca manda o cabeçalho segura
		// uma goroutine para sempre.
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn("painel: servidor parou", "err", err)
		}
	}()
	log.Info("painel: no ar", "porta", porta)

	abrir := func() error {
		bilhete, err := bilhetes.Emitir()
		if err != nil {
			return err
		}
		// A URL carrega o bilhete: vai para o navegador e para nenhum
		// outro lugar. Nem o log registra ela.
		if err := painel.AbrirNoNavegador(fmt.Sprintf("http://127.0.0.1:%d/?t=%s", porta, bilhete)); err != nil {
			return err
		}
		log.Info("painel: aberto no navegador")
		return nil
	}
	parar := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return abrir, porta, parar, nil
}
