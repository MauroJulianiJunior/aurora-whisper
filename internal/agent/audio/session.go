package audio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// DefaultPreRoll é o quanto de áudio anterior a MarkStart fica retido e
// disponível para Stop quando Config.PreRoll é zero — SÓ no modo "sempre
// pronto" (Config.SemprePronto). No padrão, sob demanda, o microfone liga
// na marca e o pre-roll é zero por construção (ADR-015).
//
// # Por que 300 ms, e não os 2 s de antes
//
// O pre-roll existe para o AC-003 — "o início da fala não é perdido" —
// cobrindo quem começa a falar em cima da tecla. MarkStart acontece no
// KEYDOWN (dictation.Machine.onKeyDown), não na expiração do hold: os
// 700 ms de HoldThreshold já estão DENTRO do trecho capturado, e o
// pre-roll nunca precisou cobri-los.
//
// 2 s era generoso e custava caro. Medido em 28-set-2026, com o áudio de
// OUTRA fala nos 2 s anteriores à marca:
//
//	só o alvo: "Precisa transmitir a DCTF web da competência de agosto..."
//	com cauda: "na net e confirma o balanço de firme. Precisa transmitir..."
//
// 39 runas da frase anterior entraram na transcrição — e daí no campo de
// quem usa. Ditar duas vezes em menos de 2 s bastava. É o mesmo sintoma
// que custou uma noite de investigação em 27-set (texto que a pessoa não
// ditou agora), com a diferença de que aquele vinha de fora e este é
// nosso.
//
// Não foi economia de latência: silêncio no começo do clipe custa ZERO
// tempo de ASR, medido em 0,0% sobre 5 amostras (o whisper.cpp paga o
// encoder numa janela fixa, audio_ctx). Ver o teste com a tag
// asrlatencia em internal/asr/local.
//
// O projeto já tinha chegado a este número por este caminho: cmd/asrrec
// baixou o pre-roll da GRAVAÇÃO para 300 ms porque os 2 s contaminavam
// as amostras com o final da frase anterior. A lição não tinha sido
// aplicada ao ditado.
//
// Continua sendo JULGAMENTO, não medição: 300 ms é a margem para quem
// começa a falar em cima da tecla. Se alguém perder sílaba inicial no
// sempre pronto, aumente — e o sinal disso é a transcrição começar no
// meio da primeira palavra, não vir vazia. No sob demanda esta constante
// não tem efeito; ali a alavanca é o próprio "sempre pronto".
const DefaultPreRoll = 300 * time.Millisecond

// Margem de cauda: quanto Stop espera quando a tecla sobe no meio de
// uma palavra.
//
// # O problema
//
// A proteção do ditado era ASSIMÉTRICA. O começo tem DefaultPreRoll; o
// fim não tinha nada — Stop lia o ring até o cursor do instante em que
// rodava, então o áudio acabava exatamente quando a tecla subia. Quem
// solta em cima da última sílaba perde a última sílaba.
//
// Mauro produziu o caso testando o pre-roll em 28-set-2026: soltou um
// pouco antes em "Faça a simulação do fator R" e recebeu "fator E".
// Palavra final de uma letra é o pior caso, e é comum no domínio dele —
// fator R, Anexo I, DAS.
//
// # Por que adaptativo, e não um post-roll fixo
//
// Um post-roll fixo cobraria a margem em TODO ditado, inclusive nos que
// terminaram direito — latência que só se paga, nunca se usa. Medido
// sobre 6 amostras, cortar o fim custa: -100 ms muda 1 (e só uma
// vírgula), -200 ms muda 3, -400 ms muda 3 com palavra truncada (ver o
// teste da tag asrlatencia em internal/asr/local).
//
// A margem adaptativa olha os últimos JanelaDeCauda de áudio. Se já é
// silêncio, a pessoa terminou e Stop corta na hora — custo ZERO, que é o
// caso normal. Se ainda é fala, ela se cortou, e aí vale esperar.
//
// # O microfone fica ligado durante a espera
//
// Com o microfone só enquanto dita (ADR-015), a espera só serve se o
// microfone continuar gravando até o corte. Stop marca a tentativa como
// "parando" antes de esperar, e o microfone só estaciona depois que o
// corte foi fixado — nunca "quando não há mais marca".
const (
	// DefaultMargemDeCauda é o teto da espera. Dentro da faixa em que a
	// medição mostra dano (100–200 ms), escolhido no limite superior
	// porque só é gasto quando há fala de fato para salvar.
	DefaultMargemDeCauda = 200 * time.Millisecond
	// JanelaDeCauda é quanto do fim se examina para decidir se a pessoa
	// ainda estava falando.
	JanelaDeCauda = 80 * time.Millisecond
	// PassoDeCauda é o intervalo entre duas checagens. Menor devolve o
	// controle mais cedo quando a fala acaba; maior gasta menos CPU.
	PassoDeCauda = 40 * time.Millisecond
)

// Espera pelo microfone no Stop (ADR-015, ajuste A da contestação).
//
// Com o microfone ligado só no keydown, pode acontecer de a tecla subir
// antes de o microfone ter entregue áudio que cubra esse instante — um
// Start lento, um driver frio. Cortar na hora devolveria um buffer vazio
// ou curto, e o núcleo diria "nada reconhecido como fala", que é a causa
// errada. Stop espera até ter áudio com carimbo no instante do keyup ou
// depois; sem nenhum pacote até o teto, devolve ErrMicrofoneNaoAbriu.
const (
	// DefaultEsperaPeloMicrofone é o teto. Fica bem abaixo dos 5 s do
	// dictation.DefaultAudioStopTimeout, somada a margem de cauda: Stop
	// precisa voltar antes de o núcleo desistir dele.
	DefaultEsperaPeloMicrofone = 1 * time.Second
	// PassoDoMicrofone é o intervalo entre duas checagens: o período
	// típico do motor de áudio compartilhado.
	PassoDoMicrofone = 10 * time.Millisecond
)

// periodoPadrao é o período usado quando o dispositivo não informa o
// dele: o do motor compartilhado do Windows.
const periodoPadrao = 10 * time.Millisecond

// DefaultEsperaAoFechar é quanto Session.Close espera a goroutine de áudio
// terminar. Folgado para um Stop e os Release; curto o bastante para não
// segurar o "Sair" da bandeja se um Start ficar preso no driver.
const DefaultEsperaAoFechar = 2 * time.Second

// DefaultMaxRecording espelha dictation.DefaultMaxRecording (120s), usado
// quando Config.MaxRecording é zero. Não importamos a constante do pacote
// dictation de propósito — este pacote não deve depender de detalhes do
// núcleo além dos tipos de dado que já cruzam a borda (AttemptID,
// VADSummary etc.); o chamador real (o runtime do Sprint 1) é quem deve
// passar dictation.Config.MaxRecording aqui, mantendo as duas em sincronia
// por construção em vez de por essa constante coincidir.
const DefaultMaxRecording = 120 * time.Second

// DefaultMaxHoldThreshold espelha dictation.MaxHoldThreshold (2 s, o maior
// limiar de hold que o núcleo aceita e que a API local valida), usado
// quando Config.MaxHoldThreshold é zero. Pelo mesmo motivo de
// DefaultMaxRecording, a constante do núcleo não é importada aqui: quem
// passa o valor é o chamador (cmd/agent, configDeAudio). O teste
// TestDefaultMaxHoldThreshold_EspelhaONucleo prende os dois juntos.
const DefaultMaxHoldThreshold = 2 * time.Second

// Config são os parâmetros de uma sessão de captura.
type Config struct {
	// SemprePronto mantém o microfone ligado desde a subida, com pre-roll
	// — o comportamento de antes da ADR-015. O valor zero, falso, é o
	// padrão: o microfone só fica ligado enquanto há ditado. Quem quer
	// captura contínua (cmd/asrrec) pede explicitamente. Muda em tempo de
	// execução por Session.DefinirSemprePronto.
	SemprePronto bool
	// Politica é como estacionar o microfone. O valor zero é
	// PoliticaParar.
	Politica Politica

	// PreRoll é quanto de áudio anterior a cada MarkStart fica disponível
	// no Stop correspondente, SÓ no sempre pronto. Zero usa
	// DefaultPreRoll. Nunca atravessa uma parada do microfone.
	PreRoll time.Duration
	// MaxRecording entra no tamanho do ring buffer (ADR-010, "Ring buffer
	// de pre-roll"; a conta inteira está em capacitySamples). Zero usa
	// DefaultMaxRecording. Deve ser o mesmo valor de
	// dictation.Config.MaxRecording usado pelo Machine.
	MaxRecording time.Duration
	// MaxHoldThreshold é o maior limiar de hold que o núcleo aceita. Entra
	// no tamanho do ring porque a marca é no KEYDOWN e o limite de gravação
	// do núcleo só começa a contar quando o limiar vence. Zero usa
	// DefaultMaxHoldThreshold. Deve ser dictation.MaxHoldThreshold — o
	// máximo, e não o limiar configurado, que muda com o Agent rodando.
	MaxHoldThreshold time.Duration
	// VAD configura o limiar de energia usado para classificar o áudio
	// devolvido por Stop. Zero value usa VADConfig{}.withDefaults().
	VAD VADConfig

	// MargemDeCauda é quanto Stop pode ESPERAR, no máximo, quando a
	// pessoa solta a tecla no meio de uma palavra. Zero usa
	// DefaultMargemDeCauda; negativo desliga a espera.
	MargemDeCauda time.Duration

	// Log recebe as linhas da recuperação do microfone (uma leitura que
	// falhou, a reabertura, a desistência). Só números, booleanos e o modo
	// — nunca áudio. Nil usa slog.Default().
	Log *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.PreRoll <= 0 {
		c.PreRoll = DefaultPreRoll
	}
	if c.MaxRecording <= 0 {
		c.MaxRecording = DefaultMaxRecording
	}
	if c.MaxHoldThreshold <= 0 {
		c.MaxHoldThreshold = DefaultMaxHoldThreshold
	}
	if c.MargemDeCauda == 0 {
		c.MargemDeCauda = DefaultMargemDeCauda
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	return c
}

// capacitySamples dimensiona o ring pelo pior caso do trecho que um Stop
// pode pedir, da marca menos o pre-roll até o corte.
//
// Até 29-set-2026 era só PreRoll + MaxRecording, e não cabia: a marca é no
// KEYDOWN, o TimerMaxRecording do núcleo só é armado quando o limiar do
// hold vence (até dictation.MaxHoldThreshold), e o Stop do limite ainda
// espera o microfone e a cauda com o microfone ligado. Numa fala de 120 s,
// o Range subia o from e descartava o COMEÇO do ditado sem sinal nenhum.
// Agora o pior caso inteiro cabe:
//
//	PreRoll + MaxHoldThreshold + MaxRecording + DefaultEsperaPeloMicrofone + MargemDeCauda
//
// A espera pelo microfone entra inteira por folga: com o microfone
// atrasado, ela não traz 1 s de áudio a mais, mas cobre o atraso de
// entrega que já havia na marca. Com os padrões são 123,5 s — cerca de
// 3,95 MB de int16 a 16 kHz, 3,2 s a mais que antes. Se ainda assim o corte
// pedido for mais antigo que o ring, Stop diz quanto perdeu
// (Janela.PerdeuInicio). TestConfig_RingCabeOPiorCaso prende a conta.
func (c Config) capacitySamples() int {
	total := c.PreRoll + c.MaxHoldThreshold + c.MaxRecording + DefaultEsperaPeloMicrofone + max(c.MargemDeCauda, 0)
	return int(total.Seconds() * float64(SampleRate))
}

func (c Config) preRollSamples() int64 {
	return int64(c.PreRoll.Seconds() * float64(SampleRate))
}

// Source abre o dispositivo de captura padrão — o equivalente real ao
// AudioSource descrito em ADR-007, seção 4 (emendada pela ADR-015), e
// docs/ai-spec/18-tool-contract.md.
type Source struct{}

// NewSource cria um Source pronto para Start.
func NewSource() *Source { return &Source{} }

// Start abre e PREPARA o dispositivo padrão na inicialização do Agent. No
// padrão (ADR-015) o microfone fica parado até o primeiro MarkStart — o
// Windows não o mostra em uso; com Config.SemprePronto, liga já aqui e
// captura continuamente, com pre-roll.
//
// Abrir continua na subida, e não no keydown, para que um microfone que
// não abre apareça na subida e o keydown pague só o Start.
//
// Toda chamada COM subsequente acontece numa única goroutine interna,
// travada à sua thread do SO com runtime.LockOSThread(); Start bloqueia só
// até essa goroutine terminar de abrir o dispositivo (ou falhar), não até
// a sessão acabar.
func (s *Source) Start(ctx context.Context, cfg Config) (*Session, error) {
	return startSession(ctx, cfg, openDevice, agoraQPC)
}

// startSession é a implementação real de Start, parametrizada pela função
// que abre o dispositivo de baixo nível e pelo relógio. Em produção isso é
// sempre openDevice (windows: WASAPI real; outro SO: ErrNotSupported) e
// agoraQPC; nos testes deste pacote (session_test.go, mesmo pacote, sem
// build tag) é um dispositivo falso e um relógio que o teste controla, o
// que permite testar toda a máquina de MarkStart/Stop/Abort/pre-roll e o
// liga-desliga sem hardware nenhum e em qualquer SO.
func startSession(ctx context.Context, cfg Config, open func(context.Context) (device, error), relogio func() int64) (*Session, error) {
	cfg = cfg.withDefaults()

	runCtx, cancel := context.WithCancel(context.Background())
	sess := &Session{
		ring:            NewRingBuffer(cfg.capacitySamples()),
		preRollSamples:  cfg.preRollSamples(),
		vadCfg:          cfg.VAD,
		politica:        cfg.Politica,
		semprePronto:    cfg.SemprePronto,
		margemCauda:     cfg.MargemDeCauda,
		esperaMicrofone: DefaultEsperaPeloMicrofone,
		esperaAoFechar:  DefaultEsperaAoFechar,
		dormir:          time.Sleep,
		temporizador:    time.After,
		relogio:         relogio,
		log:             cfg.Log,
		tentativas:      make(map[dictation.AttemptID]*tentativa),
		acordar:         make(chan struct{}, 1),
		frames:          make(chan Frame, 32),
		cancel:          cancel,
		done:            make(chan struct{}),
	}

	ready := make(chan error, 1)
	go sess.run(runCtx, ready, open)

	select {
	case err := <-ready:
		if err != nil {
			cancel()
			<-sess.done
			return nil, err
		}
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
	return sess, nil
}

// Session é a captura de áudio compartilhada por todas as tentativas de
// ditado. Corresponde a AudioSession do ADR-007, seção 4.
//
// É dona do liga-desliga do microfone (ADR-015). A regra é uma só: o
// microfone roda enquanto valer ao menos uma destas — (1) modo sempre
// pronto; (2) há tentativa marcada; (3) há tentativa parando. Uma tentativa
// que perdeu a janela numa leitura que falhou não conta: o áudio de depois
// nunca será dela. O núcleo, os Effects e o runtime não sabem que o
// microfone liga e desliga.
//
// É dona também da recuperação do microfone (revisão de 29-set-2026, M1):
// ver leituraFalhou e naoLigou.
//
// MarkStart, Abort e Stop só mexem no estado sob o mutex e acordam a
// goroutine de áudio; nunca esperam o dispositivo — MarkStart roda no laço
// que consome o hook de teclado.
type Session struct {
	mu             sync.Mutex
	ring           *RingBuffer
	devRate        int
	devChannels    int
	chanPeak       []float64
	preRollSamples int64
	vadCfg         VADConfig
	politica       Politica
	semprePronto   bool

	// margemCauda, esperaMicrofone e dormir governam as esperas de Stop.
	// dormir é injetável para que o teste não precise esperar de verdade.
	margemCauda     time.Duration
	esperaMicrofone time.Duration
	dormir          func(time.Duration)
	// esperaAoFechar é o teto de Close. Injetável pelo teste.
	esperaAoFechar time.Duration
	// dormiuNoTeste acumula a espera simulada. Só o teste escreve em
	// dormir, e só ele lê isto — em produção dormir é time.Sleep e este
	// campo fica em zero.
	dormiuNoTeste time.Duration
	// relogio é o instante atual em 100 ns do QPC (relogio.go).
	relogio func() int64
	// temporizador é o time.After do recuo da reabertura. Injetável: o
	// teste decide quando o recuo vence, sem esperar de verdade.
	temporizador func(time.Duration) <-chan time.Time
	log          *slog.Logger

	tentativas map[dictation.AttemptID]*tentativa
	// failed é a falha definitiva: o microfone não liga mais até reiniciar
	// o Agent. Ver Falha.
	failed error

	// Recuperação do microfone (M1). Só a goroutine de áudio escreve — menos
	// recuoAte e recuoCortado, que o MarkStart também escreve, quando o
	// keydown corta o recuo —; todos leem e escrevem sob mu.
	//
	// falhasSeguidas conta as falhas do incidente em curso — leituras que
	// falharam e religações do recuo que não vingaram, não as da janela de um
	// keydown que cortou o recuo (recuoCortado) — no sempre pronto. É o que
	// escala o recuo.
	falhasSeguidas int
	// falhasReais conta, do mesmo incidente, só as falhas com erro — sem as
	// dos vigias. É o que decide a desistência (MaxFalhasSeguidas):
	// paradas sem erro não gastam a cota de um microfone que depois some
	// de verdade (segunda revisão de 29-set-2026).
	falhasReais int
	// fimDoUltimoRecuo é o instante em que a última falha deixou o microfone
	// tentar de novo — ou, se um keydown cortou o recuo, o do primeiro pacote
	// da janela que ele abriu (receber). Uma falha JanelaSaudavel depois disso
	// abre um incidente novo.
	fimDoUltimoRecuo int64
	// recuoAte é até quando a religação espera. Zero: não espera.
	recuoAte int64
	// recuoCortado é o recuoAte que um keydown cortou (MarkStart): a janela
	// que vem é a do keydown, e não a do recuo, e fica em teste até o
	// primeiro pacote (receber). Se ela não vingar — o Start falha
	// (naoLigou), ou a janela cai antes do primeiro pacote (leituraFalhou) —,
	// o recuo volta a valer daqui, e a falha não conta para o incidente. Se
	// a janela acabar sem veredito (estacionar), o corte some com ela. Zero:
	// nenhum keydown cortou, ou a janela dele já entregou.
	recuoCortado int64
	// religarDepoisDaFalha diz que a próxima religação que der certo fecha
	// uma falha, e merece a linha do log.
	religarDepoisDaFalha bool
	// falhasDeLeitura conta, desde a subida, as leituras que falharam com o
	// microfone ligado.
	falhasDeLeitura int

	// Estado do microfone. Só a goroutine de áudio escreve; todos leem
	// sob mu.
	ligado bool
	// liberado diz que o último estacionamento soltou o cliente: o
	// próximo Ligar reabre.
	liberado bool
	janela   janelaAtual
	// qpcEstacionou é quando o microfone estacionou pela última vez. Zero
	// se nunca estacionou. É a referência do detector de contaminação.
	qpcEstacionou int64
	// liberarNoEstacionar: o último Stop não recebeu o áudio do ditado; o
	// próximo estacionamento solta o cliente, e o keydown seguinte reabre.
	liberarNoEstacionar    bool
	aberturas, fechamentos int

	acordar chan struct{}
	frames  chan Frame

	cancel context.CancelFunc
	done   chan struct{}
}

// tentativa é o que a Session sabe de uma tentativa em curso.
type tentativa struct {
	// marca é o cursor do ring no keydown.
	marca int64
	// qpcMarca é o instante da marca.
	qpcMarca int64
	// preRoll é o pre-roll em amostras do modo que valia no keydown.
	preRoll int64
	// semprePronto é o modo que valia no keydown.
	semprePronto bool
	// jaLigado diz se o microfone já entregava áudio na marca.
	jaLigado bool
	// parando: Stop já começou. Abort não a remove mais; quem remove é o
	// Stop, depois de fixar o corte — é o que mantém o microfone ligado
	// durante a espera da cauda.
	parando bool
	// falha é o erro da leitura que derrubou a janela desta tentativa com o
	// microfone ligado (M1), ou o do Start que não ligou para ela, no sob
	// demanda. O áudio de depois da reabertura nunca é dela: Stop devolve
	// este erro, e ela deixa de segurar o microfone ligado.
	falha error
	// janelaNaFalha é a janela do microfone como estava quando a tentativa
	// falhou — antes de a falha zerá-la, e antes de uma reabertura abrir
	// outra. É dela a linha do log deste ditado. Vazia se o Start não ligou.
	janelaNaFalha *janelaAtual
}

// janelaAtual descreve o trecho em que o microfone ficou ligado.
type janelaAtual struct {
	// inicio é o cursor do ring quando o microfone ligou. O corte nunca
	// começa antes disto: o pre-roll não atravessa uma parada.
	inicio int64
	// qpcLigou é quando o Start voltou: de onde o vigia do primeiro pacote
	// conta (VigiaDoPrimeiroPacote).
	qpcLigou   int64
	ligarLevou time.Duration
	reabriu    bool
	// reabriuPorFalha: o primeiro Start falhou e a janela só abriu
	// reabrindo o endpoint (ligarOuReabrir).
	reabriuPorFalha bool
	periodo         time.Duration

	temPacote       bool
	qpcPrimeiro     int64
	chegadaPrimeiro int64
	posicaoPrimeiro uint64
	erroTimestamp   bool
	contaminada     bool
	// fimUltimo é o instante do fim do último pacote recebido.
	fimUltimo int64
	// chegadaUltimo é quando o último pacote chegou: o que o vigia de
	// parada olha (VigiaDeParada).
	chegadaUltimo int64
}

// deveRodar é a regra única do liga-desliga. Chamar com mu.
//
// Uma tentativa que perdeu a janela numa leitura que falhou não segura o
// microfone: no sob demanda, é o próximo keydown que religa, e não a tecla
// ainda segurada de um ditado que já falhou.
func (s *Session) deveRodar() bool {
	if s.semprePronto {
		return true
	}
	for _, t := range s.tentativas {
		if t.falha == nil {
			return true
		}
	}
	return false
}

// passo é o que a goroutine de áudio faz na próxima volta do laço.
type passo int

const (
	// passoEsperar: estacionado e ninguém precisa do microfone — bloqueia
	// até um aviso.
	passoEsperar passo = iota
	// passoLigar: estacionado e alguém precisa — Start.
	passoLigar
	// passoEstacionar: ligado e ninguém mais precisa — Stop+Reset.
	passoEstacionar
	// passoLer: ligado e alguém precisa — lê o próximo pacote.
	passoLer
)

// proximoPasso decide a próxima volta do laço de áudio. Chamar com mu.
//
// Decidir estacionar já põe o estado em "desligado", no MESMO lock da
// decisão. Se fossem dois locks, um MarkStart que caísse entre eles veria
// o microfone ligado e com pacote, e gravaria "já ligado" — mas o
// microfone estaciona mesmo assim, o Reset descarta o que veio depois da
// marca, e ele religa numa janela nova. O áudio sairia certo; a linha do
// log, não (ja_ligado=true, ligar_levou_ms=0, e o alerta de microfone
// lento suprimido). Assim, uma marca que chega antes da decisão impede o
// estacionamento, e uma que chega depois se sabe numa janela nova.
func (s *Session) proximoPasso() passo {
	quer := s.deveRodar()
	switch {
	case !s.ligado && !quer:
		return passoEsperar
	case !s.ligado && quer:
		return passoLigar
	case s.ligado && !quer:
		s.ligado = false
		s.janela.temPacote = false
		return passoEstacionar
	default:
		return passoLer
	}
}

// acordarThread avisa a goroutine de áudio que o estado mudou. Nunca
// bloqueia: um aviso pendente já basta, porque a goroutine relê o estado
// inteiro a cada volta.
func (s *Session) acordarThread() {
	select {
	case s.acordar <- struct{}{}:
	default:
	}
}

// run é a única goroutine que toca o dispositivo do início ao fim: abre,
// liga, lê, estaciona, fecha. runtime.LockOSThread() aqui é o que garante
// que toda chamada COM sai da mesma thread do SO que inicializou o
// apartment — sem isso, o runtime do Go pode migrar a goroutine para outra
// thread entre duas chamadas e o comportamento fica indefinido (ADR-010,
// "COM em Go puro").
//
// Estacionada, ela fica BLOQUEADA esperando um aviso: nenhuma consulta ao
// dispositivo, nenhum pacote acordando o laço do runtime. Ligada, relê o
// estado a cada volta de Read — que volta até numa consulta vazia —, e é
// assim que um toque curto estaciona o microfone mesmo que ele ainda não
// tenha entregue nada.
func (s *Session) run(ctx context.Context, ready chan<- error, open func(context.Context) (device, error)) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.done)
	defer close(s.frames)

	dev, err := open(ctx)
	if err != nil {
		ready <- err
		return
	}
	defer dev.Close()

	s.mu.Lock()
	quer := s.deveRodar()
	s.mu.Unlock()

	var resampler *Resampler
	if quer {
		// Sempre pronto: liga já na subida, e uma falha aparece em Start,
		// como antes da ADR-015.
		if resampler, err = s.ligar(dev); err != nil {
			ready <- err
			return
		}
	}
	ready <- nil

	for {
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		p := s.proximoPasso()
		s.mu.Unlock()

		switch p {
		case passoEsperar:
			select {
			case <-ctx.Done():
				return
			case <-s.acordar:
			}

		case passoLigar:
			// Depois de uma falha no sempre pronto, o recuo segura a
			// próxima tentativa: sem laço quente. Qualquer aviso — a opção
			// desligada, um keydown, o Close — acorda a espera para
			// reavaliar; o keydown, além disso, corta o recuo (MarkStart).
			if espera := s.esperaDoRecuo(); espera > 0 {
				if !s.aguardarRecuo(ctx, espera) {
					return
				}
				continue
			}
			// Só a partir de estacionado: nunca um Start duplo.
			r, err := s.ligarOuReabrir(dev)
			if err != nil {
				if !s.naoLigou(err) {
					return
				}
				continue
			}
			resampler = r
			s.religou()

		case passoEstacionar:
			s.estacionar(dev)

		default:
			// O relógio da consulta é lido ANTES do Read: um Read vazio
			// dorme um período antes de voltar, e uma goroutine que acorda
			// atrasada desse sono (CPU saturada, paginação) não pode fazer
			// o microfone parecer parado com os pacotes já esperando no
			// buffer (segunda revisão de 29-set-2026).
			consulta := s.relogio()
			p, err := dev.Read(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if !s.leituraFalhou(dev, err, 0) {
					return
				}
				continue
			}
			if len(p.amostras) == 0 {
				// Consulta vazia: volta a olhar o estado — e os vigias, que só
				// uma consulta vazia pode disparar.
				if causa, ha := s.vigiar(consulta); causa != nil {
					if !s.leituraFalhou(dev, causa, ha) {
						return
					}
				}
				continue
			}
			s.receber(p, resampler)
		}
	}
}

// ligar começa uma janela: um Resampler novo (o estado da interpolação não
// atravessa uma parada, e o formato pode ter mudado numa reabertura), o
// início da janela no cursor atual e o Start. Só a goroutine de áudio
// chama, e só com o microfone estacionado.
func (s *Session) ligar(dev device) (*Resampler, error) {
	s.mu.Lock()
	inicio := s.ring.Cursor()
	reabriu := s.liberado
	s.mu.Unlock()

	t0 := s.relogio()
	err := dev.Ligar()
	t1 := s.relogio()
	if err != nil {
		return nil, err
	}

	rate, channels := dev.MixFormat()
	periodo := dev.Periodo()
	if periodo <= 0 {
		periodo = periodoPadrao
	}

	s.mu.Lock()
	s.devRate, s.devChannels = rate, channels
	if len(s.chanPeak) != channels {
		s.chanPeak = make([]float64, channels)
	}
	s.ligado = true
	s.liberado = false
	s.aberturas++
	s.janela = janelaAtual{
		inicio:     inicio,
		qpcLigou:   t1,
		ligarLevou: duracaoDeTicks(t1 - t0),
		reabriu:    reabriu,
		periodo:    periodo,
	}
	s.mu.Unlock()
	return NewResampler(rate, channels), nil
}

// ligarOuReabrir liga o microfone no keydown e, se o Start falhar, solta
// o cliente e tenta UMA vez de novo, reabrindo o endpoint padrão.
//
// É o caso de um cliente que ficou inválido com o Agent parado —
// suspender e retomar, o microfone USB retirado: o Start dá
// AUDCLNT_E_DEVICE_INVALIDATED. Com o microfone ligado só no keydown, cada
// keydown é um Start (inclusive Ctrl+Win+setas), e sem esta segunda
// tentativa o ditado falharia com o microfone já de volta. Se a reabertura
// também falhar, quem decide é naoLigou: no sob demanda, e no sempre pronto
// quando a religação é a de um keydown que cortou o recuo, falha o ditado, e
// o próximo keydown tenta de novo; na religação do recuo, o recuo.
func (s *Session) ligarOuReabrir(dev device) (*Resampler, error) {
	r, err := s.ligar(dev)
	if err == nil {
		return r, nil
	}
	dev.Liberar()
	s.mu.Lock()
	s.liberado = true
	s.mu.Unlock()
	r, err2 := s.ligar(dev)
	if err2 != nil {
		return nil, fmt.Errorf("%v; e reabrindo o endpoint: %v", err, err2)
	}
	s.mu.Lock()
	s.janela.reabriuPorFalha = true
	s.mu.Unlock()
	return r, nil
}

// estacionar desliga o microfone pela política configurada. O estado já
// está em "desligado" ANTES das chamadas ao dispositivo — proximoPasso o
// muda no mesmo lock em que decide estacionar —, para que um MarkStart no
// meio do estacionamento não se ache numa janela que está acabando.
func (s *Session) estacionar(dev device) {
	s.mu.Lock()
	padrao := s.politica
	if s.liberarNoEstacionar {
		// O último ditado não recebeu áudio (ver Stop): o cliente parado
		// pode ter voltado da suspensão ligando sem entregar, e a
		// PoliticaParar o reaproveitaria para sempre. Soltá-lo faz o
		// próximo keydown reabrir o microfone padrão.
		padrao, s.liberarNoEstacionar = PoliticaLiberar, false
	}
	s.mu.Unlock()

	usada := estacionarDispositivo(dev, padrao)
	agora := s.relogio()

	s.mu.Lock()
	s.qpcEstacionou = agora
	s.fechamentos++
	s.liberado = usada == PoliticaLiberar
	// A janela acabou sem veredito. Se era a de um keydown que cortou o
	// recuo, ela não entregou nem falhou: o sempre pronto foi desligado no
	// meio dela (só fora dele o microfone estaciona). O corte some com ela —
	// fora do sempre pronto não há recuo —, e a primeira falha do sempre
	// pronto religado conta como qualquer outra.
	s.recuoCortado = 0
	s.mu.Unlock()
}

// Recuperação do microfone (revisão de 29-set-2026, M1).
//
// Com o sempre pronto ligado, o microfone roda em repouso. Se o cliente for
// invalidado — suspender e retomar o notebook, tirar o microfone USB —, o
// erro chega pela LEITURA, e não pelo Start. Até aqui isso matava a sessão
// em silêncio: nenhum ditado funcionava, o bipe nunca saía, desligar a
// opção não resolvia, e só reiniciar o Agent trazia o microfone de volta.
//
// Agora uma leitura que falha solta o cliente e a sessão volta a poder
// ligar. No sempre pronto ela religa sozinha, com recuo se não vingar; no
// sob demanda, o próximo keydown religa, como já fazia com o microfone
// estacionado (ligarOuReabrir) — e, se não religar, o seguinte tenta de
// novo (naoLigou). Só o sempre pronto desiste.
const (
	// JanelaSaudavel é quanto o microfone precisa ficar sem falhar para a
	// próxima falha abrir um incidente novo, com a primeira religação
	// imediata. Menos que isso é o mesmo incidente, e o recuo cresce.
	JanelaSaudavel = 10 * time.Second
	// MaxFalhasSeguidas é quando o sempre pronto desiste — o sob demanda
	// nunca desiste, porque só tenta a cada keydown: na oitava falha
	// seguida, depois de ~78 s de recuos (0 + 1 + 2 + 5 + 10 + 30 + 30 s).
	// Um microfone que não volta nesse tempo não é suspensão, é hardware
	// que sumiu — e tentar para sempre a cada 30 s só encheria o log. A
	// sessão morre com ERROR, e o microfone só volta reiniciando o Agent.
	MaxFalhasSeguidas = 8
)

// VigiaDeParada é quanto o microfone ligado pode ficar sem entregar pacote
// nenhum, depois de já ter entregue na janela, antes de a sessão tratar isso
// como uma leitura que falhou (revisão de 29-set-2026, segunda rodada).
//
// O motor compartilhado entrega um pacote a cada período (~10 ms), com fala
// ou silêncio: 500 ms são 50 períodos sem nada. Um microfone que para assim,
// sem erro — o Windows pausando o stream ao suspender, sem invalidar o
// cliente —, deixaria o sempre pronto "ligado" e mudo para sempre: o bipe
// sairia sem microfone, e o pre-roll do ditado seguinte traria os últimos
// 300 ms de antes da parada. Tratado como falha, ele é solto e reaberto como
// qualquer outro (leituraFalhou), e o pre-roll não atravessa a falha.
//
// Só vale depois do primeiro pacote da janela: antes dele, quem vigia é o
// VigiaDoPrimeiroPacote, com mais folga, porque um Start lento não é parada.
// E conta pela chegada dos pacotes, não pelo carimbo do dispositivo: um
// carimbo atrasado de forma sistemática não é parada.
//
// E só vale no repouso do sempre pronto: no sempre pronto, e SEM ditado em
// curso (segunda revisão de 29-set-2026). Com um ditado vivo, quem decide é o
// Stop, que já tem teto e FaltouNoFim: um soluço de 600 ms no meio de uma
// fala de 60 s corta aquele pedaço, e derrubar o ditado inteiro por ele
// seria trocar um parcial por nada. Um ditado que não recebeu áudio nenhum
// solta o cliente no estacionamento seguinte (liberarNoEstacionar), e é
// por aí que o sob demanda se recupera de um cliente que liga e não
// entrega. O sempre pronto nunca estaciona: quem faz isso por ele é o
// VigiaDoPrimeiroPacote.
const VigiaDeParada = 500 * time.Millisecond

// VigiaDoPrimeiroPacote é quanto o microfone ligado pode ficar sem entregar o
// PRIMEIRO pacote da janela, contado do Start, antes de a sessão tratar isso
// como uma parada (terceira rodada da revisão de 29-set-2026).
//
// É o cliente parado que volta da suspensão ligando sem erro e sem entregar
// nada. O VigiaDeParada não o pega — conta da chegada do último pacote, e não
// houve nenhum —, e o sempre pronto nunca estaciona para soltá-lo pela marca
// do Stop (liberarNoEstacionar), que é como o sob demanda se recupera. Sem
// este vigia, todo ditado do sempre pronto falhava com ErrMicrofoneNaoAbriu
// até desmarcar e marcar de novo a opção nas Configurações.
//
// Julgamento, como o VigiaDeParada: bem acima do primeiro pacote de um Start
// frio (algumas dezenas de ms, pela estimativa da ADR-015), e o dobro do teto
// do Stop (DefaultEsperaPeloMicrofone). Vale nas mesmas condições do
// VigiaDeParada — no repouso do sempre pronto —, e como a parada só recua,
// sem gastar a cota da desistência.
//
// Um microfone cujo primeiro pacote legítimo chegue depois disso (um fone
// Bluetooth abrindo o perfil de chamada) nunca se firma no repouso: cicla
// entre o recuo de 30 s e 2 s ligado, até um keydown segurado até o
// primeiro pacote. Risco aceito, sem medição (ADR-015, "Riscos",
// Bluetooth).
const VigiaDoPrimeiroPacote = 2 * time.Second

var (
	// errParouDeEntregar é a causa que o vigia de parada dá à falha.
	errParouDeEntregar = errors.New("audio: o microfone parou de entregar áudio, sem erro")
	// errNaoComecouAEntregar é a causa que o vigia do primeiro pacote dá à
	// falha.
	errNaoComecouAEntregar = errors.New("audio: o microfone ligou e não entregou áudio nenhum, sem erro")
)

// doVigia diz se a falha veio de um vigia — o microfone parou, ou nunca
// começou, sem erro nenhum do dispositivo.
func doVigia(err error) bool {
	return errors.Is(err, errParouDeEntregar) || errors.Is(err, errNaoComecouAEntregar)
}

// emRepouso diz se o microfone está no repouso do sempre pronto — ligado, no
// sempre pronto e sem ditado em curso —, o único estado em que os vigias
// agem. Chamar com mu.
//
// Só no sempre pronto: no sob demanda o microfone ligado sem ditado é só o
// instante entre o Stop e o estacionamento, e ali o vigia soltaria o cliente
// pelo caminho errado, com um aviso falso de parada — a recuperação do sob
// demanda é a marca do Stop (liberarNoEstacionar). E sem ditado em curso:
// com um ditado vivo, quem decide é o Stop.
func (s *Session) emRepouso() bool {
	if !s.semprePronto || !s.ligado {
		return false
	}
	for _, t := range s.tentativas {
		if t.falha == nil {
			return false
		}
	}
	return true
}

// vigiar diz se, numa consulta vazia no instante agora, o microfone ligado
// deve ser tratado como uma leitura que falhou: parou de entregar
// (VigiaDeParada, contado da chegada do último pacote) ou nunca começou
// (VigiaDoPrimeiroPacote, contado do Start). Devolve a causa — nil para
// seguir lendo — e há quanto tempo. Só no repouso (emRepouso), que
// leituraFalhou confere de novo antes de agir.
func (s *Session) vigiar(agora int64) (causa error, ha time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.emRepouso() {
		return nil, 0
	}
	if s.janela.temPacote {
		if ha = duracaoDeTicks(agora - s.janela.chegadaUltimo); ha >= VigiaDeParada {
			return errParouDeEntregar, ha
		}
		return nil, ha
	}
	if ha = duracaoDeTicks(agora - s.janela.qpcLigou); ha >= VigiaDoPrimeiroPacote {
		return errNaoComecouAEntregar, ha
	}
	return nil, ha
}

// recuosDaReligacao é a espera depois da n-ésima falha seguida, antes de
// religar: a primeira religação de um incidente é imediata; depois, 1, 2, 5,
// 10 e 30 s, e 30 s daí em diante.
var recuosDaReligacao = [...]time.Duration{0, 1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

func recuoDepoisDe(n int) time.Duration {
	if n < 1 {
		return 0
	}
	if n > len(recuosDaReligacao) {
		n = len(recuosDaReligacao)
	}
	return recuosDaReligacao[n-1]
}

// contarFalha põe uma falha no incidente em curso e marca o recuo. Chamar
// com mu, só no sempre pronto.
//
// Duas falhas são do mesmo incidente se o microfone não ficou
// JanelaSaudavel sem falhar entre elas, contando do fim do recuo — a espera
// não é saúde. É o que segura um microfone que religa e falha logo na
// primeira leitura: sem isso, cada falha seria "a primeira" e a religação
// seria imediata, num laço quente.
//
// real diz se a falha teve erro — tudo, menos o que vem dos vigias (doVigia).
// n escala o recuo; reais decide a desistência.
func (s *Session) contarFalha(agora int64, real bool) (n, reais int, recuo time.Duration) {
	if s.falhasSeguidas > 0 && agora-s.fimDoUltimoRecuo >= ticksDe(JanelaSaudavel) {
		s.falhasSeguidas, s.falhasReais = 0, 0
	}
	s.falhasSeguidas++
	if real {
		s.falhasReais++
	}
	recuo = recuoDepoisDe(s.falhasSeguidas)
	s.recuoAte = 0
	if recuo > 0 {
		s.recuoAte = agora + ticksDe(recuo)
	}
	s.fimDoUltimoRecuo = agora + ticksDe(recuo)
	return s.falhasSeguidas, s.falhasReais, recuo
}

// leituraFalhou trata uma leitura que falhou com o microfone ligado — ou um
// microfone que parou de entregar sem erro (VigiaDeParada), ou que ligou e
// nunca começou (VigiaDoPrimeiroPacote); para os vigias, ha é há quanto tempo
// (vigiar), e zero numa leitura com erro. Devolve false quando a sessão
// morreu.
//
// O vigia decide num lock (vigiar) e age neste. Um keydown que chegue entre
// os dois tira o microfone do repouso, e com ele o vigia não age: por isso o
// repouso é conferido de novo aqui, no lock que age, e a linha do vigia só
// sai depois de agir. Antes, esse keydown perdia o ditado com a causa do
// vigia, e o log dizia que o microfone foi solto (quarta rodada da revisão,
// 30-set-2026).
//
// A tentativa em curso, marcada ou parando, falha com o erro do
// dispositivo: o áudio de depois da reabertura não é dela, e o pre-roll
// nunca atravessa a falha — a janela seguinte é nova. O cliente é solto,
// como na PoliticaLiberar, e o microfone conta como estacionado.
//
// No sempre pronto, a falha conta para o incidente e marca o recuo — menos a
// da janela que um keydown abriu cortando o recuo (MarkStart), que cai antes
// do primeiro pacote: é como o Start dele que não vinga (naoLigou). Não
// conta, e o recuo cortado volta a valer de onde estava. Sem isso, um
// microfone que religa e não se sustenta fazia cada keydown no recuo gastar
// uma falha real e escalar o recuo, e seis keydowns seguidos matavam a sessão
// sem que nenhum recuo tivesse vencido (quarta rodada da revisão, 30-set-2026).
func (s *Session) leituraFalhou(dev device, err error, ha time.Duration) bool {
	agora := s.relogio()
	s.mu.Lock()
	if doVigia(err) && !s.emRepouso() {
		// Saiu do repouso depois de o vigia decidir: quem decide agora é o
		// Stop. Nada a fazer, e nada a dizer.
		s.mu.Unlock()
		return true
	}
	janela := s.janela
	perdidas := s.falharTentativas(err, &janela)
	// No mesmo lock da falha das tentativas, como no estacionamento: uma
	// marca que chegue daqui em diante se sabe numa janela nova.
	s.ligado = false
	s.janela.temPacote = false
	s.falhasDeLeitura++
	sempre := s.semprePronto
	cortado := s.recuoCortado
	if cortado != 0 {
		s.recuoAte = cortado
	}
	s.recuoCortado = 0
	doRecuo := sempre && cortado == 0
	var n, reais int
	var recuo time.Duration
	if doRecuo {
		n, reais, recuo = s.contarFalha(agora, !doVigia(err))
	}
	leituras := s.falhasDeLeitura
	s.mu.Unlock()

	dev.Liberar()

	s.mu.Lock()
	s.qpcEstacionou = s.relogio()
	s.fechamentos++
	s.liberado = true
	// O cliente acabou de ser solto: uma marca de "soltar no próximo
	// estacionamento" de um Stop anterior já está cumprida, e soltaria à
	// toa, horas depois, um cliente saudável.
	s.liberarNoEstacionar = false
	s.religarDepoisDaFalha = true
	s.mu.Unlock()

	// A linha do vigia vem logo antes da linha da falha, e só depois de agir.
	switch {
	case errors.Is(err, errParouDeEntregar):
		s.log.Warn("audio: o microfone parou de entregar áudio sem erro; tratado como leitura que falhou",
			"modo", nomeDoModo(sempre),
			"sem_pacote_ms", ha.Milliseconds())
	case errors.Is(err, errNaoComecouAEntregar):
		s.log.Warn("audio: o microfone ligou e não entregou áudio nenhum; tratado como leitura que falhou",
			"modo", nomeDoModo(sempre),
			"ligado_ha_ms", ha.Milliseconds())
	}
	// reabre_sozinho: no sempre pronto, a sessão religa sem keydown (depois
	// de recuo_ms); no sob demanda, quem religa é o próximo ditado.
	args := []any{"modo", nomeDoModo(sempre),
		"tentativas_perdidas", perdidas,
		"falhas_de_leitura", leituras,
		"reabre_sozinho", sempre}
	if sempre && !doRecuo {
		// A janela do keydown, que não conta: como na linha do keydown que
		// não ligou (naoLigou), quanto falta do recuo que ele cortou.
		args = append(args, "recuo_restante_ms", max(duracaoDeTicks(cortado-agora), 0).Milliseconds())
	} else {
		args = append(args, "falhas_seguidas", n, "recuo_ms", recuo.Milliseconds())
	}
	s.log.Warn("audio: a leitura do microfone falhou; o microfone foi solto", args...)
	// A parada sem erro (os vigias) só recua, e nunca faz o sempre pronto
	// desistir nem gasta a cota: é um microfone que volta a entregar a cada
	// reabertura. Desistir dele trocaria "funciona com soluços" por "mudo até
	// reiniciar o Agent" (segunda revisão de 29-set-2026).
	if doRecuo && reais >= MaxFalhasSeguidas {
		s.morrer(err, n)
		return false
	}
	return true
}

// falharTentativas faz as tentativas em curso, marcadas ou parando, falharem
// com err: o áudio que o microfone entregar daqui em diante não é delas.
// janela é a janela em que falharam, para a linha do log. Devolve quantas.
// Chamar com mu.
func (s *Session) falharTentativas(err error, janela *janelaAtual) int {
	n := 0
	for _, t := range s.tentativas {
		if t.falha == nil {
			t.falha, t.janelaNaFalha = err, janela
			n++
		}
	}
	return n
}

// naoLigou decide o que fazer quando ligar falhou mesmo reabrindo o
// endpoint (ligarOuReabrir). Devolve false quando a sessão morreu.
//
// No sob demanda, falham só as tentativas que pediam o microfone, com o erro
// do dispositivo, e a sessão segue viva: o próximo keydown tenta de novo,
// reabrindo o endpoint. Não vira laço: uma tentativa que falhou não segura o
// microfone (deveRodar), e a goroutine volta a esperar um aviso — cada nova
// tentativa de ligar precisa de um keydown novo. Até 29-set-2026 a sessão
// morria aqui: um keydown com o microfone fora (o USB retirado, o endpoint
// ainda voltando depois de retomar a suspensão) deixava o ditado morto até
// reiniciar o Agent, mesmo com o microfone de volta logo depois.
//
// No sempre pronto, a religação do recuo — em repouso, sem keydown nenhum
// para tentar de novo — conta para o incidente, entra no recuo, e só desiste
// em MaxFalhasSeguidas. A de um keydown que cortou o recuo (MarkStart) é
// como a do sob demanda: não conta, e o recuo que ele cortou volta a valer
// de onde estava (terceira rodada da revisão de 29-set-2026). O mesmo vale
// quando o Start do keydown vinga e a janela cai antes do primeiro pacote
// (leituraFalhou): keydowns no recuo não matam a sessão mais cedo, nem adiam
// a religação seguinte do recuo, com o microfone fora ou caindo antes do
// primeiro pacote (quarta rodada, 30-set-2026). Uma janela que entrega um
// pacote e só depois cai já saiu do teste e conta — risco aceito na ADR-015.
//
// Nos dois modos, e nas duas religações, as tentativas que pediam o microfone
// falham agora, com o erro do dispositivo — e não um teto depois, no Stop,
// como "o microfone não abriu a tempo".
func (s *Session) naoLigou(err error) bool {
	agora := s.relogio()
	s.mu.Lock()
	sempre := s.semprePronto
	// A próxima religação que der certo fecha esta falha, nos dois modos.
	s.religarDepoisDaFalha = true
	// Nenhuma janela abriu: a linha do ditado sai sem os números de uma
	// janela anterior.
	perdidas := s.falharTentativas(err, &janelaAtual{inicio: s.ring.Cursor()})
	cortado := s.recuoCortado
	if cortado != 0 {
		s.recuoAte = cortado
	}
	s.recuoCortado = 0
	doRecuo := sempre && cortado == 0
	var n, reais int
	var recuo time.Duration
	if doRecuo {
		n, reais, recuo = s.contarFalha(agora, true)
	}
	s.mu.Unlock()

	if !doRecuo {
		args := []any{"modo", nomeDoModo(sempre), "tentativas_perdidas", perdidas}
		if sempre {
			// Quanto falta do recuo que o keydown cortou: é quando a sessão
			// tenta de novo sozinha.
			args = append(args, "recuo_restante_ms", max(duracaoDeTicks(cortado-agora), 0).Milliseconds())
		}
		s.log.Warn("audio: o microfone não ligou; o próximo ditado tenta de novo", args...)
		return true
	}
	if reais >= MaxFalhasSeguidas {
		s.morrer(fmt.Errorf("%w: %v", ErrDeviceLost, err), n)
		return false
	}
	s.log.Warn("audio: o microfone não religou; tento de novo depois do recuo",
		"modo", ModoSemprePronto,
		"tentativas_perdidas", perdidas,
		"falhas_seguidas", n,
		"recuo_ms", recuo.Milliseconds())
	return true
}

// religou fecha a falha, se a religação que acabou de dar certo veio de
// uma: uma linha no log, com quanto custou. O contador do incidente só
// zera depois de JanelaSaudavel (contarFalha): religar não prova que o
// microfone voltou.
func (s *Session) religou() {
	s.mu.Lock()
	depoisDaFalha := s.religarDepoisDaFalha
	s.religarDepoisDaFalha = false
	// Um recuoCortado fica: o Start do keydown vingou, mas a janela dele só
	// sai do teste no primeiro pacote (receber). Ligar não prova que o
	// microfone voltou.
	n, levou, modo := s.falhasSeguidas, s.janela.ligarLevou, nomeDoModo(s.semprePronto)
	s.mu.Unlock()
	if depoisDaFalha {
		s.log.Info("audio: o microfone reabriu depois de uma falha",
			"modo", modo,
			"falhas_seguidas", n,
			"ligar_levou_ms", levou.Milliseconds())
	}
}

// esperaDoRecuo é quanto falta do recuo antes de religar. Zero fora do
// sempre pronto: no sob demanda quem religa é um keydown, e ele não espera.
// No sempre pronto, um keydown corta o recuo (MarkStart).
func (s *Session) esperaDoRecuo() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.semprePronto || s.recuoAte == 0 {
		return 0
	}
	if falta := duracaoDeTicks(s.recuoAte - s.relogio()); falta > 0 {
		return falta
	}
	s.recuoAte = 0
	return 0
}

// aguardarRecuo espera o recuo sem travar ninguém: Close (ctx) sai na hora,
// e qualquer aviso — DefinirSemprePronto, um keydown — acorda a goroutine
// para reavaliar; o que faltar do recuo é recalculado pelo relógio. Devolve
// false com o contexto cancelado.
func (s *Session) aguardarRecuo(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-s.acordar:
	case <-s.temporizador(d):
		s.mu.Lock()
		s.recuoAte = 0
		s.mu.Unlock()
	}
	return true
}

// morrer é a falha definitiva: o microfone não liga mais até reiniciar o
// Agent. Todo Stop daqui em diante devolve ErrDeviceLost.
func (s *Session) morrer(err error, falhasSeguidas int) {
	s.markFailed(err)
	s.mu.Lock()
	modo := nomeDoModo(s.semprePronto)
	s.mu.Unlock()
	s.log.Error("audio: o microfone parou de responder; o ditado não funciona até reiniciar o Agent",
		"modo", modo,
		"falhas_seguidas", falhasSeguidas,
		"err", err)
}

// receber põe um pacote no ring e atualiza os números da janela.
func (s *Session) receber(p pacote, resampler *Resampler) {
	chegada := s.relogio()

	s.mu.Lock()
	rate, channels := s.devRate, s.devChannels
	s.mu.Unlock()

	// Pico por canal ANTES do downmix. O Resampler faz a media dos
	// canais, entao um microfone que alimente so um canal perde 6 dB
	// e some do diagnostico depois deste ponto — e "gravou baixo com
	// o ganho no alto" vira misterio insoluvel. Custa uma passada
	// pelo buffer e responde a pergunta de uma vez.
	s.observeChannels(p.amostras, channels)

	mono := resampler.Process(p.amostras)

	frames := int64(0)
	if channels > 0 {
		frames = int64(len(p.amostras) / channels)
	}
	dur := ticksDe(duracaoDeAmostras(frames, rate))
	inicio, erroTs := p.qpc, p.erroTimestamp || p.qpc <= 0
	if erroTs {
		// Sem carimbo que valha, a melhor estimativa é que o pacote
		// acabou de ser gravado quando chegou.
		inicio = chegada - dur
	}

	s.mu.Lock()
	if !s.janela.temPacote {
		s.janela.temPacote = true
		s.janela.qpcPrimeiro = inicio
		s.janela.chegadaPrimeiro = chegada
		s.janela.posicaoPrimeiro = p.posicao
		s.janela.erroTimestamp = erroTs
		s.janela.contaminada = !erroTs && pacoteAnteriorAoEstacionamento(inicio, s.qpcEstacionou, s.janela.periodo)
		if s.recuoCortado != 0 {
			// O primeiro pacote da janela de um keydown que cortou o recuo: o
			// microfone voltou, e não há recuo a devolver. O recuo cortado
			// acabou aqui, e não no fim que ele teria — é daqui que a
			// JanelaSaudavel conta (contarFalha). Sem isso, 15 s saudáveis
			// depois de um keydown que cortou um recuo de 30 s ainda eram o
			// mesmo incidente.
			s.recuoCortado = 0
			s.fimDoUltimoRecuo = min(s.fimDoUltimoRecuo, chegada)
		}
	}
	s.janela.fimUltimo = inicio + dur
	s.janela.chegadaUltimo = chegada
	if len(mono) > 0 {
		s.ring.Write(mono)
	}
	s.mu.Unlock()

	if len(mono) > 0 {
		s.pushFrame(mono)
	}
}

// pacoteAnteriorAoEstacionamento é o detector de contaminação (ADR-015,
// ajuste D da contestação): o primeiro pacote de uma janela nova com
// carimbo ANTERIOR ao estacionamento passado é áudio do ditado que já
// acabou — o Reset não limpou.
//
// Compara com o estacionamento anterior, e não com o Start desta janela: o
// motor compartilhado trabalha em períodos, e o primeiro frame de um
// pacote legítimo pode vir carimbado até um período antes do Start. A
// mesma tolerância de um período vale aqui, para um Start logo depois do
// estacionamento.
func pacoteAnteriorAoEstacionamento(qpcPrimeiro, qpcEstacionou int64, periodo time.Duration) bool {
	if qpcEstacionou <= 0 {
		return false // nunca estacionou: não há ditado anterior a vazar
	}
	return qpcPrimeiro+ticksDe(periodo) < qpcEstacionou
}

func (s *Session) markFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = err
	}
}

func (s *Session) pushFrame(mono []int16) {
	f := Frame{At: time.Now(), RMS: rmsInt16(mono)}
	select {
	case s.frames <- f:
	default:
		// Best-effort (ADR-007, seção 4): sob carga, perder um frame de
		// onda é aceitável; o buffer autoritativo de Stop nunca passa
		// por aqui.
	}
}

// MarkStart marca o início da tentativa id e liga o microfone, se ele
// estiver estacionado (ADR-015). Não espera o microfone: só grava a marca
// e acorda a goroutine de áudio — roda no laço que consome o hook.
//
// No sempre pronto, com o microfone fora no meio de um recuo, corta o recuo:
// a goroutine tenta ligar na hora, como no sob demanda. Sem isso, um ditado
// no meio de um recuo de até 30 s falhava com "nenhum pacote em 1 s". A
// janela que ele abre fica em teste até o primeiro pacote: se o Start não
// vingar (naoLigou), ou se ela cair antes do primeiro pacote
// (leituraFalhou), falha só este ditado, e o recuo volta a valer de onde
// estava, sem contar para o incidente nem para a desistência.
func (s *Session) MarkStart(id dictation.AttemptID, at time.Time) {
	agora := s.relogio()
	s.mu.Lock()
	t := &tentativa{
		marca:        s.ring.Cursor(),
		qpcMarca:     agora,
		semprePronto: s.semprePronto,
		jaLigado:     s.ligado && s.janela.temPacote,
	}
	if s.semprePronto {
		t.preRoll = s.preRollSamples
		if !s.ligado && s.recuoAte > agora {
			s.recuoCortado, s.recuoAte = s.recuoAte, 0
		}
	}
	s.tentativas[id] = t
	s.mu.Unlock()
	s.acordarThread()
}

// MicrofoneEntregando diz se o microfone já entrega áudio para a tentativa
// id: ela existe, o microfone está ligado e a janela dele já recebeu o
// primeiro pacote.
//
// É o que o runtime consulta para só tocar o bipe e mostrar "Gravando"
// quando o microfone está de fato captando (ADR-015, ajuste A).
func (s *Session) MicrofoneEntregando(id dictation.AttemptID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tentativas[id]
	// Uma tentativa que perdeu a janela numa falha de leitura não recebe o
	// áudio da reabertura: "pode falar" seria mentira.
	return ok && t.falha == nil && s.failed == nil && s.ligado && s.janela.temPacote
}

// Falha é a falha definitiva da sessão: nil enquanto o microfone ainda
// pode ligar — inclusive durante uma recuperação (M1). Diferente de nil, o
// microfone não liga mais até reiniciar o Agent, e todo Stop devolve
// ErrDeviceLost. Seguro de qualquer goroutine.
func (s *Session) Falha() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

// DefinirSemprePronto liga ou desliga o modo "microfone sempre pronto" em
// tempo de execução. Seguro de qualquer goroutine: só muda o estado e
// acorda a goroutine de áudio. Ligar abre uma janela nova (o pre-roll não
// traz áudio de antes dela); desligar sem ditado em curso estaciona o
// microfone na volta seguinte do laço de áudio.
func (s *Session) DefinirSemprePronto(v bool) {
	s.mu.Lock()
	s.semprePronto = v
	s.mu.Unlock()
	s.acordarThread()
}

// Modo é o modo atual do microfone, como aparece no log.
func (s *Session) Modo() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return nomeDoModo(s.semprePronto)
}

func nomeDoModo(semprePronto bool) string {
	if semprePronto {
		return ModoSemprePronto
	}
	return ModoSobDemanda
}

// Politica é a política de estacionamento configurada.
func (s *Session) Politica() Politica {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.politica
}

// Frames expõe a onda ao vivo, best-effort — alimenta a amplitude do
// overlay durante StateRecording. Pode perder frame sob carga; nunca é a
// fonte do áudio realmente transcrito (isso é Stop). Continua aberto com o
// microfone estacionado: só não chega nada.
func (s *Session) Frames() <-chan Frame { return s.frames }

// observeChannels atualiza o pico de cada canal do dispositivo.
func (s *Session) observeChannels(raw []float32, channels int) {
	if channels <= 0 || len(raw) < channels {
		return
	}
	picos := make([]float64, channels)
	for i := 0; i+channels <= len(raw); i += channels {
		for c := 0; c < channels; c++ {
			if v := math.Abs(float64(raw[i+c])); v > picos[c] {
				picos[c] = v
			}
		}
	}
	s.mu.Lock()
	for c := range picos {
		if c < len(s.chanPeak) && picos[c] > s.chanPeak[c] {
			s.chanPeak[c] = picos[c]
		}
	}
	s.mu.Unlock()
}

// DeviceFormat devolve a taxa e a contagem de canais que o dispositivo de
// fato negociou, e o pico observado em cada canal desde a abertura.
//
// Existe para diagnostico de captura: distingue "o microfone gravou baixo"
// de "o microfone alimenta um canal so e a media com o canal mudo comeu
// 6 dB", que sao problemas diferentes com correcoes diferentes.
func (s *Session) DeviceFormat() (sampleRate, channels int, channelPeaks []float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.devRate, s.devChannels, append([]float64(nil), s.chanPeak...)
}

// Stop é a parada autoritativa e sem perdas: devolve todo o áudio da
// tentativa id, do início da captura (incluindo o PreRoll anterior à
// marca, no sempre pronto) até o corte (ADR-007, seção 4; ADR-015).
//
// A tentativa passa a "parando" e o microfone SEGUE ligado: Stop espera o
// áudio do instante do keyup chegar, depois a última sílaba calar, fixa o
// corte, e só então libera o microfone para estacionar.
//
// Depois de qualquer chamada a Stop ou Abort, id não é mais reconhecido —
// chamar Stop de novo para o mesmo id é erro.
func (s *Session) Stop(ctx context.Context, id dictation.AttemptID) (AudioBuffer, dictation.VADSummary, error) {
	corte := s.relogio()

	s.mu.Lock()
	t, ok := s.tentativas[id]
	if ok && t.parando {
		ok = false // um segundo Stop do mesmo id
	}
	if ok {
		t.parando = true
	}
	failed := s.failed
	if failed == nil && ok {
		failed = t.falha
	}
	s.mu.Unlock()

	if !ok {
		return AudioBuffer{}, dictation.VADSummary{}, fmt.Errorf("audio: Stop: nenhum MarkStart pendente para attempt %d", id)
	}
	// Em todo caminho, a tentativa sai DEPOIS do corte — e só então o
	// microfone pode estacionar.
	defer s.encerrar(id)

	if failed != nil {
		// A janela vai junto também aqui: a linha do log por tentativa
		// vale justamente depois que o microfone morreu.
		return AudioBuffer{SampleRate: SampleRate, Janela: s.janelaDaFalha(t)}, dictation.VADSummary{}, fmt.Errorf("%w: %v", ErrDeviceLost, failed)
	}

	esperouMic, faltou, err := s.esperarOMicrofone(ctx, t, corte)
	// O ditado não recebeu o áudio que devia: a janela não entregou pacote
	// nenhum, ou parou por VigiaDeParada ou mais antes do keyup. Soltar o
	// cliente no estacionamento (o encerrar, adiado acima, vem depois
	// disto) é a recuperação do sob demanda, no lugar do vigia de parada.
	// Um microfone só lento — entregou, mas depois de a tecla subir — não
	// entra: soltá-lo cobraria a reabertura de cada ditado curto.
	s.mu.Lock()
	semPacote := !s.janela.temPacote
	if (errors.Is(err, ErrMicrofoneNaoAbriu) && semPacote) || faltou >= VigiaDeParada {
		s.liberarNoEstacionar = true
	}
	s.mu.Unlock()
	if err != nil {
		j := s.janelaDaFalha(t)
		j.EsperouMicrofone = esperouMic
		return AudioBuffer{SampleRate: SampleRate, Janela: j}, dictation.VADSummary{}, err
	}

	// Espera a fala calar ANTES de fixar o corte: o microfone continua
	// ligado (a tentativa está "parando"), então cada milissegundo
	// esperado é um milissegundo a mais de áudio. Ver a margem de cauda,
	// acima.
	esperouCauda := s.esperarACaudaCalar(ctx)

	s.mu.Lock()
	if falha := t.falha; falha != nil {
		// A leitura falhou durante a espera. O que o ring tem depois disso é
		// de outra janela: esta tentativa falha, no mesmo lock do corte.
		s.mu.Unlock()
		j := s.janelaDaFalha(t)
		j.EsperouMicrofone, j.CaudaEsperou = esperouMic, esperouCauda
		return AudioBuffer{SampleRate: SampleRate, Janela: j}, dictation.VADSummary{}, fmt.Errorf("%w: %v", ErrDeviceLost, falha)
	}
	from := t.marca - t.preRoll
	if from < s.janela.inicio {
		// O pre-roll nunca atravessa uma parada: antes do início da
		// janela, o ring guarda o fim do ditado ANTERIOR.
		from = s.janela.inicio
	}
	// O ring guarda Cap() amostras. Um corte pedido mais antigo que isso
	// perdeu o começo — o Range devolveria o que sobrou sem avisar. O ring
	// é dimensionado para que isto não aconteça (capacitySamples); se
	// acontecer, a janela diz quanto se perdeu.
	to := s.ring.Cursor()
	var perdeu int64
	if retido := to - int64(s.ring.Cap()); from < retido {
		perdeu, from = retido-from, retido
	}
	samples := s.ring.Range(from, to)
	j := s.medir(t, from)
	s.mu.Unlock()

	j.PerdeuInicio = duracaoDeAmostras(perdeu, SampleRate)
	j.EsperouMicrofone, j.CaudaEsperou, j.FaltouNoFim = esperouMic, esperouCauda, faltou
	j.FalaNoInicio = falaNoInicio(samples, s.vadCfg)
	summary := ClassifyVAD(samples, SampleRate, s.vadCfg)
	return AudioBuffer{SampleRate: SampleRate, Samples: samples, Janela: j}, summary, nil
}

// janelaDaFalha mede a janela de uma tentativa cujo Stop falhou antes de
// fixar o corte. Não houve corte: um microfone que nem chegou a ligar, ou
// que morreu, não "desligou antes do corte" — o alerta certo é o erro.
func (s *Session) janelaDaFalha(t *tentativa) Janela {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Sem corte, nenhuma amostra foi entregue: o "início" é a própria marca.
	j := s.medir(t, t.marca)
	j.DesligouAntesDoCorte = false
	return j
}

// encerrar tira a tentativa e acorda a goroutine de áudio, que estaciona
// o microfone se ninguém mais precisar dele.
func (s *Session) encerrar(id dictation.AttemptID) {
	s.mu.Lock()
	delete(s.tentativas, id)
	s.mu.Unlock()
	s.acordarThread()
}

// medir monta os números da janela para a tentativa t, com o corte
// começando em from — a primeira amostra de fato entregue. Chamar com mu.
//
// A janela é a atual, a não ser que a tentativa tenha falhado: aí é a
// janela em que ela falhou (tentativa.janelaNaFalha), e não a que a falha
// zerou ou a que a reabertura abriu depois.
func (s *Session) medir(t *tentativa, from int64) Janela {
	jan := s.janela
	if t.janelaNaFalha != nil {
		jan = *t.janelaNaFalha
	}
	j := Janela{
		Modo:            nomeDoModo(t.semprePronto),
		JaLigado:        t.jaLigado,
		Reabriu:         jan.reabriu && !t.jaLigado,
		ReabriuPorFalha: jan.reabriuPorFalha && !t.jaLigado,
		// A contaminação é do PRIMEIRO pacote da janela: só entra no
		// ditado cujo corte começa no início dela. Numa janela longa do
		// sempre pronto, os ditados seguintes não o têm.
		PacoteAnteriorAoEstacionamento: jan.contaminada && from <= jan.inicio,
		ErroTimestamp:                  jan.erroTimestamp,
		PosicaoPrimeiroPacote:          jan.posicaoPrimeiro,
		DesligouAntesDoCorte:           !s.ligado,
		Aberturas:                      s.aberturas,
		Fechamentos:                    s.fechamentos,
	}
	if from < t.marca {
		j.PreRollEntregue = duracaoDeAmostras(t.marca-from, SampleRate)
	}
	if !t.jaLigado {
		j.LigarLevou = jan.ligarLevou
		if jan.temPacote {
			j.MarcaAtePrimeiroPacote = duracaoDeTicks(jan.chegadaPrimeiro - t.qpcMarca)
		}
	}
	// A primeira amostra entregue fica from-marca amostras depois da marca:
	// negativo é o pre-roll; positivo, só quando o ring perdeu o começo
	// (Janela.PerdeuInicio).
	switch {
	case t.marca == jan.inicio && jan.temPacote:
		// Nada gravado na janela até a marca: a amostra da marca é a do
		// primeiro pacote.
		j.InicioRelMarca = duracaoDeTicks(jan.qpcPrimeiro-t.qpcMarca) + duracaoDeAmostras(from-t.marca, SampleRate)
	default:
		j.InicioRelMarca = duracaoDeAmostras(from-t.marca, SampleRate)
	}
	return j
}

// falaNoInicio classifica os primeiros JanelaDeCauda do buffer com o
// MESMO ClassifyVAD da cauda e do AC-010.
func falaNoInicio(samples []int16, cfg VADConfig) bool {
	n := int(JanelaDeCauda.Seconds() * float64(SampleRate))
	if len(samples) < n {
		n = len(samples)
	}
	return ClassifyVAD(samples[:n], SampleRate, cfg).HasSpeech
}

// Abort descarta o rastreamento da tentativa id sem produzir buffer — o
// caminho de tap, watchdog ou falha (AbortAudioCaptureEffect). Se ninguém
// mais precisar do microfone, ele estaciona.
//
// Uma tentativa que já está "parando" não é tocada: é o Stop dela que a
// tira, depois do corte.
func (s *Session) Abort(id dictation.AttemptID) {
	s.mu.Lock()
	if t, ok := s.tentativas[id]; ok && !t.parando {
		delete(s.tentativas, id)
	}
	s.mu.Unlock()
	s.acordarThread()
}

// Close para a captura e libera o dispositivo. Depois de Close, a Session
// não deve mais ser usada.
//
// Espera a goroutine de áudio terminar até esperaAoFechar. Com o microfone
// ligado a cada keydown (ADR-015), um Start preso no driver prende essa
// goroutine — e um Close sem teto travaria o encerramento do Agent. Antes,
// o único Start ficava sob o audioStartTimeout da subida. Vencido o teto,
// Close devolve erro e deixa a goroutine para trás: o processo está saindo.
func (s *Session) Close() error {
	s.cancel()
	select {
	case <-s.done:
		return nil
	case <-time.After(s.esperaAoFechar):
		return fmt.Errorf("audio: Close: a goroutine de áudio não terminou em %s (um Start preso no driver?)", s.esperaAoFechar)
	}
}

// esperarOMicrofone segura Stop até a janela ter áudio que cubra o
// instante corte (o keyup), até o teto de esperaMicrofone. Devolve quanto
// esperou e, se o teto venceu com o áudio acabando antes do keyup, quanto
// faltou até ele.
//
// Devolve na hora no caso normal: com o microfone ligado há mais de um
// período, o pacote do keyup chega em até ~10 ms. A espera só existe
// quando o microfone ainda não entregou o que precisa — e aí ela é a
// diferença entre "o microfone não abriu" e "nada reconhecido como fala".
func (s *Session) esperarOMicrofone(ctx context.Context, t *tentativa, corte int64) (esperado, faltou time.Duration, err error) {
	for {
		s.mu.Lock()
		tem := s.ligado && s.janela.temPacote
		primeiro, fim, failed := s.janela.qpcPrimeiro, s.janela.fimUltimo, s.failed
		if failed == nil {
			// Uma leitura que falhou derruba a tentativa: a janela que o
			// microfone abrir depois não é dela.
			failed = t.falha
		}
		s.mu.Unlock()

		if failed != nil {
			return esperado, 0, fmt.Errorf("%w: %v", ErrDeviceLost, failed)
		}
		cobriu := tem && fim >= corte
		if cobriu || esperado >= s.esperaMicrofone || s.dormir == nil || (ctx != nil && ctx.Err() != nil) {
			switch {
			case !tem:
				return esperado, 0, fmt.Errorf("%w: nenhum pacote em %s", ErrMicrofoneNaoAbriu, esperado)
			case primeiro >= corte:
				return esperado, 0, fmt.Errorf("%w: a captura começou depois de a tecla subir", ErrMicrofoneNaoAbriu)
			}
			if !cobriu {
				// Teto vencido com o fim ainda antes do keyup: segue com o
				// que há, mas diz quanto faltou — o fim da fala pode ter
				// ficado de fora (Janela.FaltouNoFim).
				faltou = duracaoDeTicks(corte - fim)
			}
			return esperado, faltou, nil
		}
		s.dormir(PassoDoMicrofone)
		esperado += PassoDoMicrofone
	}
}

// esperarACaudaCalar segura Stop enquanto os últimos JanelaDeCauda de
// áudio ainda soarem como fala, até o teto de margemCauda, e devolve
// quanto esperou.
//
// Devolve na hora no caso normal — quem terminou de falar já tem
// silêncio no fim. A espera só existe para quem soltou a tecla no meio
// de uma palavra, e é exatamente aí que ela paga (ver a margem de cauda,
// no topo deste arquivo).
//
// NÃO pode ser chamada da goroutine que consome eventos do Agent: ela
// dorme. Quem chama Stop é responsável por estar fora do laço quente —
// ver winruntime.pedirCaptura.
func (s *Session) esperarACaudaCalar(ctx context.Context) time.Duration {
	if s.margemCauda <= 0 || s.dormir == nil {
		return 0
	}
	var esperado time.Duration
	for ; esperado < s.margemCauda; esperado += PassoDeCauda {
		if !s.caudaTemFala() {
			return esperado
		}
		if ctx != nil && ctx.Err() != nil {
			return esperado
		}
		s.dormir(PassoDeCauda)
	}
	return esperado
}

// caudaTemFala classifica os últimos JanelaDeCauda do ring buffer — sem
// passar do início da janela do microfone, que antes dele é outro ditado.
//
// Usa o MESMO ClassifyVAD e a mesma VADConfig que decidem se a tentativa
// teve fala (AC-010). Um segundo limiar aqui faria "tem fala para o
// AC-010" e "tem fala para a cauda" divergirem sem ninguém notar.
func (s *Session) caudaTemFala() bool {
	janela := int64(JanelaDeCauda.Seconds() * float64(SampleRate))
	s.mu.Lock()
	to := s.ring.Cursor()
	from := to - janela
	if from < s.janela.inicio {
		from = s.janela.inicio
	}
	amostras := s.ring.Range(from, to)
	s.mu.Unlock()
	if len(amostras) == 0 {
		return false
	}
	return ClassifyVAD(amostras, SampleRate, s.vadCfg).HasSpeech
}
