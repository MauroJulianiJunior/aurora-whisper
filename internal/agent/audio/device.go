package audio

import (
	"context"
	"errors"
	"time"
)

// ErrNotSupported é devolvido por openDevice em qualquer SO que não seja
// Windows (wasapi_other.go) — existe só para que este pacote continue
// compilando em CI (ubuntu-latest); nenhum caminho de produção chama isto
// fora do Windows.
var ErrNotSupported = errors.New("audio: captura WASAPI só é suportada no Windows")

// ErrDeviceLost é a causa raiz sempre que o dispositivo de captura falha
// no meio de uma sessão — tipicamente o microfone padrão foi removido ou
// desabilitado, ou o Start do keydown falhou mesmo reabrindo o endpoint
// (ADR-015). Uma leitura que falha com o microfone ligado o devolve no Stop
// da tentativa em curso, e a sessão segue (Session.leituraFalhou). ADR-010 D1:
// "microfone removido no meio -> erro de GetBuffer vira um erro claro que
// o runtime transforma em AudioFailedEvent (o núcleo já sabe tratar)". O
// runtime mapeia isto para dictation.ErrorReasonAudioCaptureFailed.
var ErrDeviceLost = errors.New("audio: dispositivo de captura falhou ou foi removido")

// ErrMicrofoneNaoAbriu é a falha de Stop quando o microfone, ligado no
// keydown (ADR-015), não entregou áudio que cubra o tempo em que a tecla
// ficou segurada: nenhum pacote chegou até o teto de espera, ou o primeiro
// só chegou depois de a tecla subir. É distinta de ErrDeviceLost de
// propósito — "o microfone não abriu a tempo" pede outra ação que "o
// microfone sumiu" — e existe para que isso nunca vire um buffer vazio
// silencioso, que o núcleo leria como "nada reconhecido como fala".
var ErrMicrofoneNaoAbriu = errors.New("audio: o microfone não abriu a tempo")

// device é a porta de baixo nível, específica de plataforma. Seu único
// trabalho é entregar o que quer que o mix format realmente seja —
// reamostrar para 16 kHz mono int16 acontece inteiramente na camada de
// cima (Resampler), em Go puro, para que essa conversão seja testável sem
// hardware nenhum.
//
// Implementado por wasapi_windows.go (real) e wasapi_other.go (stub que
// devolve ErrNotSupported). openDevice e todos os métodos abaixo são
// chamados sempre a partir da MESMA goroutine com runtime.LockOSThread()
// — ver doc.go e session.go.
//
// # Ciclo de vida (ADR-015)
//
// openDevice abre e PREPARA o microfone (IAudioClient::Initialize), sem
// ligar: preparado e parado, o Windows não o conta como em uso. Ligar dá o
// Start; Parar+Zerar (ou Liberar) estacionam. A Session garante a ordem —
// Ligar só a partir de estacionado, Parar só depois de Ligar, Read só com
// o microfone ligado — e os testes usam um dublê que RECUSA a ordem
// errada.
type device interface {
	// MixFormat devolve a taxa de amostragem e a contagem de canais do
	// formato compartilhado que o dispositivo negociou. Pode mudar depois
	// de um Ligar que reabriu o endpoint (política Liberar).
	MixFormat() (sampleRate, channels int)
	// Periodo é o período padrão do motor de áudio para este dispositivo
	// (IAudioClient::GetDevicePeriod). Zero se desconhecido.
	Periodo() time.Duration
	// Ligar começa a captura (IAudioClient::Start). Depois de Liberar,
	// reabre o endpoint padrão antes.
	Ligar() error
	// Parar para a captura (IAudioClient::Stop), mantendo o cliente.
	Parar() error
	// Zerar descarta os pacotes pendentes e zera a posição do stream
	// (IAudioClient::Reset). Só vale com o stream parado.
	Zerar() error
	// Liberar para (se preciso) e solta o cliente de captura, mantendo o
	// que for preciso para reabrir no próximo Ligar.
	Liberar()
	// Read devolve o próximo pacote, ou um pacote VAZIO quando a consulta
	// não achou nada — depois de esperar no máximo um intervalo de
	// consulta. Voltar a cada consulta vazia é o que deixa a Session
	// estacionar o microfone mesmo quando ele não entrega nada (um Start
	// frio, um driver lento). Falha do dispositivo vira ErrDeviceLost.
	Read(ctx context.Context) (pacote, error)
	// Close libera o dispositivo e qualquer objeto COM que ele detenha.
	Close() error
}

// pacote é um pacote de captura, já copiado para fora do buffer nativo.
type pacote struct {
	// amostras são float32 intercaladas, na faixa [-1, 1]. Vazio numa
	// consulta que não achou nada.
	amostras []float32
	// qpc é o instante, em 100 ns do contador de performance, em que o
	// dispositivo gravou o primeiro frame (pu64QPCPosition). Zero se o
	// dispositivo não informou.
	qpc int64
	// posicao é a posição do primeiro frame no stream, em frames
	// (pu64DevicePosition). Perto de 0 depois de um Reset.
	posicao uint64
	// erroTimestamp é AUDCLNT_BUFFERFLAGS_TIMESTAMP_ERROR: qpc não vale.
	erroTimestamp bool
}

// Frame é uma amostra de áudio com carimbo de tempo e amplitude RMS
// normalizada (0..1), usada só para alimentar a onda do overlay
// (Session.Frames — best-effort, ADR-007 seção 4). O núcleo
// (dictation.Machine) nunca vê este tipo.
type Frame struct {
	At  time.Time
	RMS float64
}

// AudioBuffer é o áudio bruto de uma tentativa concluída: PCM 16 kHz mono
// int16, pronto para internal/asr. Nunca persistido em disco
// (09-security-privacy.md).
type AudioBuffer struct {
	SampleRate int
	Samples    []int16
	// Janela são os números do microfone desta tentativa, para o log. Só
	// números: nenhuma amostra, nenhum texto (11-observability.md).
	Janela Janela
}

// Modos do microfone (ADR-015), como aparecem no log.
const (
	ModoSobDemanda   = "sob_demanda"
	ModoSemprePronto = "sempre_pronto"
)

// LimiteDeInicio é o L do AC-003 no modo sob demanda: a captura tem de
// começar até este tanto depois da marca do keydown.
//
// Provisório, por JULGAMENTO e não medição: coerente com a medição da
// cauda de 28-set-2026, em que cortar 100 ms do fim mudou só uma vírgula.
// Troque por um número medido quando existir (ADR-015, "Revisão").
const LimiteDeInicio = 100 * time.Millisecond

// Janela descreve, com números, o microfone de uma tentativa que chegou a
// Stop. O runtime a registra numa linha por tentativa ("audio: janela do
// microfone") e decide os alertas; o pacote de áudio só mede.
//
// Todas as durações "desde a marca" contam da MARCA do keydown
// (MarkStart), medida no mesmo relógio QPC do carimbo dos pacotes.
type Janela struct {
	// Modo é ModoSobDemanda ou ModoSemprePronto, o que valia no keydown.
	Modo string
	// JaLigado diz se o microfone já entregava áudio quando a tecla desceu
	// (sempre pronto, ou um ditado anterior ainda terminando).
	JaLigado bool
	// Reabriu diz se ligar precisou reabrir o endpoint (o estacionamento
	// anterior foi pela política Liberar, ou o Start falhou).
	Reabriu bool
	// ReabriuPorFalha diz que o Start do keydown falhou e a janela só abriu
	// soltando o cliente e reabrindo o endpoint padrão — tipicamente um
	// cliente invalidado com o Agent parado (suspensão, microfone USB
	// retirado).
	ReabriuPorFalha bool
	// LigarLevou é quanto a chamada de Ligar demorou (reabertura inclusa).
	// Zero se JaLigado.
	LigarLevou time.Duration
	// MarcaAtePrimeiroPacote é da marca até a CHEGADA do primeiro pacote.
	// Zero se JaLigado.
	MarcaAtePrimeiroPacote time.Duration
	// InicioRelMarca é o número-chave do AC-003, com sinal: o instante da
	// primeira amostra entregue menos a marca. Negativo no sempre pronto
	// (é o pre-roll); no sob demanda, é quanto o microfone levou para
	// começar a gravar. Inclui PerdeuInicio, quando houver.
	InicioRelMarca time.Duration
	// PreRollEntregue é quanto áudio anterior à marca entrou no buffer.
	// Zero no sob demanda, sempre.
	PreRollEntregue time.Duration
	// PerdeuInicio é quanto do começo pedido já não estava no ring buffer
	// quando o corte foi fixado: o ditado passou do que o ring guarda. Tem
	// de ser sempre zero — o ring é dimensionado pelo pior caso
	// (Config.capacitySamples). Acima de zero, o COMEÇO do ditado ficou de
	// fora.
	PerdeuInicio time.Duration
	// EsperouMicrofone é quanto Stop esperou por áudio que cobrisse o
	// instante do keyup.
	EsperouMicrofone time.Duration
	// FaltouNoFim é quanto do tempo com a tecla segurada ficou sem áudio no
	// fim: o último pacote acabou antes do keyup e a espera pelo microfone
	// venceu o teto. Zero no caso normal. Acima de zero, o fim da fala pode
	// ter ficado de fora — um carimbo do dispositivo atrasado de forma
	// sistemática (e aí cada ditado pagaria também o teto da espera), ou um
	// microfone que parou de entregar no meio do ditado. O vigia de parada
	// não age com ditado vivo (VigiaDeParada): o Stop devolve o que chegou,
	// e este campo diz quanto faltou. De VigiaDeParada para cima, o
	// estacionamento seguinte solta o cliente (liberarNoEstacionar).
	FaltouNoFim time.Duration
	// CaudaEsperou é quanto Stop esperou a última sílaba calar.
	CaudaEsperou time.Duration
	// FalaNoInicio diz se os primeiros JanelaDeCauda do buffer já são
	// fala — o espelho de caudaTemFala no começo. No sob demanda, sugere
	// que a pessoa começou a falar antes de o microfone abrir.
	FalaNoInicio bool
	// PacoteAnteriorAoEstacionamento é o detector de contaminação: o
	// primeiro pacote desta janela tem carimbo anterior ao estacionamento
	// ANTERIOR (com tolerância de um período do dispositivo) — áudio do
	// ditado passado entrou neste. Só vale para o ditado cujo corte começa
	// no início da janela: é ele que carrega aquele primeiro pacote.
	PacoteAnteriorAoEstacionamento bool
	// ErroTimestamp diz que o carimbo do primeiro pacote não valia; os
	// tempos desta janela são estimados pela chegada.
	ErroTimestamp bool
	// PosicaoPrimeiroPacote é a posição do primeiro pacote no stream, em
	// frames. Perto de 0 depois de um Reset.
	PosicaoPrimeiroPacote uint64
	// DesligouAntesDoCorte diz que o microfone já não estava ligado quando
	// o corte foi fixado. Tem de ser sempre falso: é a armadilha da cauda.
	DesligouAntesDoCorte bool
	// Aberturas e Fechamentos contam, desde a subida, quantas vezes o
	// microfone ligou e estacionou.
	Aberturas, Fechamentos int
}
