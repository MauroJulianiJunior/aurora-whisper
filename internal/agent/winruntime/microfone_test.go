package winruntime

// O bipe e a pílula "Gravando" só saem com o microfone entregando áudio
// (ADR-015, ajuste A), e cada tentativa deixa uma linha de números do
// microfone no log. Tudo com o relógio falso, um microfone falso e um bipe
// falso: nada aqui abre microfone nem toca som.

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/hook"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// microfoneFalso faz o papel da audio.Session. pronto diz se o microfone
// já entrega áudio. roteiro, quando não vazio, responde às próximas
// consultas, uma por chamada, antes de pronto voltar a valer: é o microfone
// que muda de ideia entre duas consultas seguidas. consultas guarda de que
// tentativa cada consulta perguntou.
type microfoneFalso struct {
	pronto    bool
	roteiro   []bool
	frames    chan audio.Frame
	consultas []dictation.AttemptID
}

func (m *microfoneFalso) Frames() <-chan audio.Frame               { return m.frames }
func (m *microfoneFalso) MarkStart(dictation.AttemptID, time.Time) {}
func (m *microfoneFalso) Abort(dictation.AttemptID)                {}
func (m *microfoneFalso) MicrofoneEntregando(id dictation.AttemptID) bool {
	m.consultas = append(m.consultas, id)
	if len(m.roteiro) > 0 {
		resposta := m.roteiro[0]
		m.roteiro = m.roteiro[1:]
		return resposta
	}
	return m.pronto
}
func (m *microfoneFalso) Stop(context.Context, dictation.AttemptID) (audio.AudioBuffer, dictation.VADSummary, error) {
	return audio.AudioBuffer{Samples: make([]int16, 16000), SampleRate: 16000}, dictation.VADSummary{HasSpeech: true}, nil
}

type bancada struct {
	rt     *Runtime
	mic    *microfoneFalso
	pilula *pilulaFalsa
	log    *bytes.Buffer
	bipes  int
}

func novaBancada(t *testing.T, pronto bool) *bancada {
	t.Helper()
	machine, err := dictation.NewMachine(dictation.Config{
		Key:           dictation.KeyF,
		HoldThreshold: 700 * time.Millisecond,
		MaxRecording:  dictation.DefaultMaxRecording,
		BeepEnabled:   true,
	})
	if err != nil {
		t.Fatalf("dictation.NewMachine: %v", err)
	}
	b := &bancada{mic: &microfoneFalso{pronto: pronto}, pilula: &pilulaFalsa{}, log: &bytes.Buffer{}}
	b.rt = &Runtime{
		machine:       machine,
		clk:           clock.NewFake(time.Now()),
		focus:         fakeFocus{},
		audio:         b.mic,
		overlay:       b.pilula,
		log:           slog.New(slog.NewTextHandler(b.log, nil)),
		timers:        make(map[timerKey]clock.Timer),
		pending:       make(chan dictation.Event, pendingBuffer),
		capturas:      make(chan capturaPronta, 1),
		pararCapturas: make(chan struct{}),
	}
	b.rt.bipar = func() error { b.bipes++; return nil }
	t.Cleanup(func() { close(b.rt.pararCapturas) })
	return b
}

// segurar aperta a tecla e deixa o hold vencer.
func (b *bancada) segurar(t *testing.T) {
	t.Helper()
	b.rt.handleRaw(dictation.RawKeyEvent{Down: true})
	b.rt.clk.(*clock.Fake).Advance(700 * time.Millisecond)
	select {
	case ev := <-b.rt.pending:
		b.rt.dispatch(ev)
	default:
		t.Fatal("o timer do hold não venceu")
	}
	if got := b.rt.machine.State(); got != dictation.StateRecording {
		t.Fatalf("State = %v depois do hold, quero StateRecording", got)
	}
}

func (b *bancada) mostrouGravando() bool {
	for _, f := range b.pilula.fases {
		if f == dictation.OverlayRecording {
			return true
		}
	}
	return false
}

// TestGravando_EsperaOMicrofone: o hold venceu, mas o microfone ainda não
// entrega áudio. Nem bipe nem "Gravando" — quem espera o bipe para falar
// perderia o começo. Saem quando o primeiro frame chega.
func TestGravando_EsperaOMicrofone(t *testing.T) {
	b := novaBancada(t, false)
	b.segurar(t)

	if b.mostrouGravando() || b.bipes != 0 {
		t.Fatalf("com o microfone ainda sem áudio: fases=%v bipes=%d", b.pilula.fases, b.bipes)
	}

	// Um frame que chega antes de o microfone entregar áudio para ESTA
	// tentativa — a sobra do ditado anterior no canal, por exemplo — não
	// solta nada: quem solta é o microfone entregando, não o frame. Nem
	// conta como espera terminada: a espera segue contando desde o hold.
	b.rt.clk.(*clock.Fake).Advance(30 * time.Millisecond)
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.1})
	if b.mostrouGravando() || b.bipes != 0 || !b.rt.retido.ativo() || b.rt.sinal.saiu ||
		strings.Contains(b.log.String(), "esperaram por ele") {
		t.Fatalf("um frame sem o microfone entregando soltou o gravando: fases=%v bipes=%d retido=%+v sinal=%+v\n%s",
			b.pilula.fases, b.bipes, b.rt.retido, b.rt.sinal, b.log.String())
	}

	b.rt.clk.(*clock.Fake).Advance(90 * time.Millisecond)
	b.mic.pronto = true
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.1})

	if !b.mostrouGravando() || b.bipes != 1 {
		t.Fatalf("com o microfone entregando: fases=%v bipes=%d, quero Gravando e um bipe", b.pilula.fases, b.bipes)
	}
	if !strings.Contains(b.log.String(), "esperaram por ele") || !strings.Contains(b.log.String(), "espera_ms=120") {
		t.Errorf("a espera pelo microfone não foi registrada:\n%s", b.log.String())
	}

	// Mais frames não repetem nada.
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.1})
	if b.bipes != 1 {
		t.Errorf("bipes = %d depois de outro frame, quero 1", b.bipes)
	}
}

// TestGravando_SemBipeNaoTocaAoLiberar: com o bipe desligado nas
// Configurações, a pílula retida sai quando o microfone entrega, e nenhum
// bipe sai junto — quem desligou o bipe não o ouve por causa de um
// microfone lento.
func TestGravando_SemBipeNaoTocaAoLiberar(t *testing.T) {
	b := novaBancada(t, false)
	if err := b.rt.machine.AjustarPreferencias(700*time.Millisecond, false); err != nil {
		t.Fatalf("AjustarPreferencias: %v", err)
	}
	b.segurar(t)
	if b.mostrouGravando() {
		t.Fatalf("a pílula saiu com o microfone sem áudio: fases=%v", b.pilula.fases)
	}

	b.mic.pronto = true
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.1})

	if !b.mostrouGravando() || b.bipes != 0 {
		t.Errorf("fases=%v bipes=%d, quero a pílula e nenhum bipe (bipe desligado)", b.pilula.fases, b.bipes)
	}
}

// TestGravando_MicrofoneJaProntoSaiNaHora: o caso normal — o microfone
// abriu nas dezenas de ms e o hold leva 700.
func TestGravando_MicrofoneJaProntoSaiNaHora(t *testing.T) {
	b := novaBancada(t, true)
	b.segurar(t)
	if !b.mostrouGravando() || b.bipes != 1 {
		t.Fatalf("fases=%v bipes=%d, quero Gravando e um bipe no hold", b.pilula.fases, b.bipes)
	}
	if strings.Contains(b.log.String(), "esperam o microfone") {
		t.Errorf("o sinal foi retido com o microfone pronto:\n%s", b.log.String())
	}
}

// TestGravando_TeclaSobeAntesDoMicrofone: a tecla subiu antes de o
// microfone entregar áudio. O "gravando" não sai depois — o momento passou.
func TestGravando_TeclaSobeAntesDoMicrofone(t *testing.T) {
	b := novaBancada(t, false)
	b.segurar(t)
	b.rt.handleRaw(dictation.RawKeyEvent{Down: false})

	if got := b.pilula.ultima(t); got != dictation.OverlayProcessing {
		t.Fatalf("a pílula mostra %v depois do keyup, quero Processing", got)
	}
	b.mic.pronto = true
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.1})
	if b.mostrouGravando() || b.bipes != 0 {
		t.Errorf("o gravando saiu depois do keyup: fases=%v bipes=%d", b.pilula.fases, b.bipes)
	}
	if !strings.Contains(b.log.String(), "a tecla subiu antes de o microfone entregar") {
		t.Errorf("o log não diz que o bipe não saiu:\n%s", b.log.String())
	}
}

// TestGravando_BipeRetidoSemAPilula: o microfone entregava quando a pílula
// foi decidida e parou de entregar antes do bipe, na mesma leva de efeitos
// do hold (o roteiro do dublê responde "entregando" e depois "parou"). A
// pílula sai; o bipe — "pode falar" — fica retido sozinho, e só sai se o
// microfone voltar. Se a tecla subir antes, não sai.
func TestGravando_BipeRetidoSemAPilula(t *testing.T) {
	gravandos := func(b *bancada) int {
		n := 0
		for _, f := range b.pilula.fases {
			if f == dictation.OverlayRecording {
				n++
			}
		}
		return n
	}
	// segurarComMicrofoneQueParou aperta e deixa o hold vencer, com o
	// microfone respondendo à pílula e ao bipe de modo diferente.
	segurarComMicrofoneQueParou := func(t *testing.T) *bancada {
		t.Helper()
		b := novaBancada(t, false)
		b.mic.roteiro = []bool{true, false}
		b.segurar(t)
		if gravandos(b) != 1 || b.bipes != 0 {
			t.Fatalf("pílula Recording = %d, bipes = %d; quero a pílula e nenhum bipe", gravandos(b), b.bipes)
		}
		if !b.rt.retido.bip || b.rt.retido.overlay {
			t.Fatalf("retido = %+v; quero só o bipe retido", b.rt.retido)
		}
		if !b.rt.sinal.esperouMic || b.rt.sinal.saiu {
			t.Fatalf("sinal = %+v; quero esperouMic e o bipe ainda sem sair", b.rt.sinal)
		}
		return b
	}

	t.Run("o microfone volta", func(t *testing.T) {
		b := segurarComMicrofoneQueParou(t)
		b.rt.clk.(*clock.Fake).Advance(40 * time.Millisecond)
		b.mic.pronto = true
		b.rt.aoReceberFrame(audio.Frame{RMS: 0.1})

		if b.bipes != 1 {
			t.Errorf("bipes = %d depois de o microfone voltar, quero 1", b.bipes)
		}
		if gravandos(b) != 1 {
			t.Errorf("a pílula Recording saiu %d vezes, quero 1 (só o bipe estava retido)", gravandos(b))
		}
		if !strings.Contains(b.log.String(), "espera_ms=40") {
			t.Errorf("a espera do bipe não foi registrada:\n%s", b.log.String())
		}
		if !b.rt.sinal.saiu {
			t.Errorf("sinal = %+v; quero o bipe marcado como saído", b.rt.sinal)
		}
	})

	t.Run("a tecla sobe antes", func(t *testing.T) {
		b := segurarComMicrofoneQueParou(t)
		b.rt.handleRaw(dictation.RawKeyEvent{Down: false})
		b.mic.pronto = true
		b.rt.aoReceberFrame(audio.Frame{RMS: 0.1})

		if b.bipes != 0 {
			t.Errorf("o bipe saiu depois do keyup: bipes = %d", b.bipes)
		}
		if !strings.Contains(b.log.String(), "a tecla subiu antes de o microfone entregar") {
			t.Errorf("o log não diz que o bipe não saiu:\n%s", b.log.String())
		}
	})
}

// TestOnda_SoAndaComOMicrofoneEntregandoParaODitado: no sempre pronto, uma
// leitura que falha no meio do ditado faz o microfone reabrir sozinho, e os
// frames voltam — mas o áudio deles não é deste ditado, que o Stop vai
// devolver com erro. A onda para, como no sob demanda: reagindo à voz, ela
// diria que grava o que já se perdeu.
func TestOnda_SoAndaComOMicrofoneEntregandoParaODitado(t *testing.T) {
	b := novaBancada(t, true)
	b.segurar(t)
	b.mic.consultas = nil
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.3})
	if len(b.pilula.amps) != 1 {
		t.Fatalf("amplitudes = %v com o microfone entregando, quero uma", b.pilula.amps)
	}
	if atual := b.rt.machine.CurrentAttempt(); len(b.mic.consultas) != 1 || b.mic.consultas[0] != atual {
		t.Errorf("o frame perguntou pelas tentativas %v, quero só a do ditado em curso (%d)", b.mic.consultas, atual)
	}

	// A leitura falhou: a sessão diz que o microfone não entrega mais para
	// esta tentativa, e os frames da janela reaberta seguem chegando.
	b.mic.pronto = false
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.4})
	b.rt.aoReceberFrame(audio.Frame{RMS: 0.5})
	if len(b.pilula.amps) != 1 {
		t.Errorf("amplitudes = %v: a onda andou com frames que não são do ditado", b.pilula.amps)
	}
}

// escritaSegura é o destino do log quando quem escreve é a goroutine de Run
// e quem lê é o teste.
type escritaSegura struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (e *escritaSegura) Write(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.b.Write(p)
}

func (e *escritaSegura) String() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.b.String()
}

// TestRun_AudioQueParaLogaErro: o canal de frames só fecha com a sessão de
// áudio morta de vez. Com o Agent rodando, isso é ERROR — antes, Run só
// parava de ler o canal, e nenhum ditado funcionava sem uma linha no log
// (revisão de 29-set-2026, M1).
func TestRun_AudioQueParaLogaErro(t *testing.T) {
	frames := make(chan audio.Frame)
	close(frames)
	var saida escritaSegura
	rt := testRuntime(t, fakeFocus{})
	rt.log = slog.New(slog.NewTextHandler(&saida, nil))
	rt.hook = hook.New(hook.Config{})
	rt.audio = &microfoneFalso{frames: frames}
	rt.pararCapturas = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	fim := make(chan error, 1)
	go func() { fim <- rt.Run(ctx) }()
	const parou = "winruntime: o áudio parou"
	for prazo := time.Now().Add(2 * time.Second); !strings.Contains(saida.String(), parou) && time.Now().Before(prazo); {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-fim

	l := saida.String()
	if strings.Count(l, parou) != 1 || !strings.Contains(l, "level=ERROR") {
		t.Fatalf("quero uma linha ERROR dizendo que o áudio parou:\n%s", l)
	}
}

// linhaDaJanela devolve a linha "audio: janela do microfone".
func linhaDaJanela(t *testing.T, log string) string {
	t.Helper()
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, "audio: janela do microfone") {
			return l
		}
	}
	t.Fatalf("sem a linha da janela do microfone:\n%s", log)
	return ""
}

// concluir entrega ao runtime o resultado de um Stop com a janela j.
func (b *bancada) concluir(j audio.Janela, err error) dictation.Event {
	return b.rt.concluirCaptura(capturaPronta{
		attempt: b.rt.machine.CurrentAttempt(),
		buf:     audio.AudioBuffer{Samples: make([]int16, 1600), SampleRate: 16000, Janela: j},
		vad:     dictation.VADSummary{HasSpeech: true},
		err:     err,
	})
}

// TestJanela_LinhaPorTentativa: o tempo do keydown ao primeiro pacote e se
// o bipe esperou o microfone, numa linha por tentativa, só números.
func TestJanela_LinhaPorTentativa(t *testing.T) {
	b := novaBancada(t, false)
	b.segurar(t)
	b.rt.clk.(*clock.Fake).Advance(80 * time.Millisecond)
	b.mic.pronto = true
	b.rt.aoReceberFrame(audio.Frame{})
	b.rt.handleRaw(dictation.RawKeyEvent{Down: false})

	b.concluir(audio.Janela{
		Modo:                   audio.ModoSobDemanda,
		MarcaAtePrimeiroPacote: 780 * time.Millisecond,
		InicioRelMarca:         60 * time.Millisecond,
		Aberturas:              3, Fechamentos: 2,
	}, nil)

	l := linhaDaJanela(t, b.log.String())
	for _, campo := range []string{
		"modo=sob_demanda", "marca_ate_primeiro_pacote_ms=780", "inicio_rel_marca_ms=60",
		"bip_esperou_mic=true", "bip_espera_ms=80", "bip_saiu=true", "aberturas=3", "fechamentos=2",
		"faltou_no_fim_ms=0", "perdeu_inicio_ms=0",
	} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha não tem %q: %s", campo, l)
		}
	}
	if strings.Contains(b.log.String(), "level=WARN msg=\"audio:") {
		t.Errorf("alerta de áudio numa tentativa normal:\n%s", b.log.String())
	}
}

// TestJanela_LinhaSaiTambemQuandoOMicrofoneNaoAbre: os números importam
// mais justamente na falha.
func TestJanela_LinhaSaiTambemQuandoOMicrofoneNaoAbre(t *testing.T) {
	b := novaBancada(t, false)
	b.segurar(t)
	b.rt.handleRaw(dictation.RawKeyEvent{Down: false})

	ev := b.concluir(audio.Janela{Modo: audio.ModoSobDemanda, EsperouMicrofone: time.Second}, audio.ErrMicrofoneNaoAbriu)
	if _, ok := ev.(dictation.AudioFailedEvent); !ok {
		t.Fatalf("concluirCaptura devolveu %T, quero AudioFailedEvent", ev)
	}
	l := linhaDaJanela(t, b.log.String())
	if !strings.Contains(l, "esperou_mic_ms=1000") || !strings.Contains(l, "bip_saiu=false") {
		t.Errorf("a linha da falha não diz o que houve: %s", l)
	}
}

// TestJanela_Alertas: cada alerta dispara no caso dele, e só nele.
func TestJanela_Alertas(t *testing.T) {
	const (
		abriuTarde = "o microfone abriu depois de a fala começar"
		contaminou = "áudio de antes desta tentativa entrou no buffer"
		cortou     = "o microfone desligou antes do corte"
		acabou     = "o áudio acabou antes de a tecla subir"
		reabriu    = "o Start do microfone falhou"
		perdeu     = "o ditado passou do que o buffer guarda"
	)
	for _, c := range []struct {
		nome   string
		janela audio.Janela
		alerta string // "" = nenhum
	}{
		{"sob demanda rápido", audio.Janela{Modo: audio.ModoSobDemanda, InicioRelMarca: 40 * time.Millisecond}, ""},
		{"sob demanda no limite", audio.Janela{Modo: audio.ModoSobDemanda, InicioRelMarca: audio.LimiteDeInicio}, ""},
		{"sob demanda lento", audio.Janela{Modo: audio.ModoSobDemanda, InicioRelMarca: 150 * time.Millisecond}, abriuTarde},
		{"sob demanda com fala no início", audio.Janela{Modo: audio.ModoSobDemanda, InicioRelMarca: 20 * time.Millisecond, FalaNoInicio: true}, abriuTarde},
		{"microfone já ligado", audio.Janela{Modo: audio.ModoSobDemanda, JaLigado: true, FalaNoInicio: true}, ""},
		{"sempre pronto com pre-roll", audio.Janela{Modo: audio.ModoSemprePronto, InicioRelMarca: -300 * time.Millisecond, PreRollEntregue: 300 * time.Millisecond, FalaNoInicio: true}, ""},
		{"pre-roll no sob demanda", audio.Janela{Modo: audio.ModoSobDemanda, JaLigado: true, PreRollEntregue: 300 * time.Millisecond}, contaminou},
		{"pacote do ditado anterior", audio.Janela{Modo: audio.ModoSobDemanda, PacoteAnteriorAoEstacionamento: true}, contaminou},
		{"desligou antes do corte", audio.Janela{Modo: audio.ModoSobDemanda, DesligouAntesDoCorte: true}, cortou},
		{"o áudio acabou antes do keyup", audio.Janela{Modo: audio.ModoSobDemanda, EsperouMicrofone: audio.DefaultEsperaPeloMicrofone, FaltouNoFim: 300 * time.Millisecond}, acabou},
		// O teto da espera numa falha ("não abriu a tempo") já sai como
		// erro; não é o fim da fala que faltou.
		{"esperou o teto e não abriu", audio.Janela{Modo: audio.ModoSobDemanda, EsperouMicrofone: audio.DefaultEsperaPeloMicrofone}, ""},
		{"Start falhou e reabriu", audio.Janela{Modo: audio.ModoSobDemanda, Reabriu: true, ReabriuPorFalha: true}, reabriu},
		// Reabrir pela política "liberar" é o caminho normal dela.
		{"reabriu pela política", audio.Janela{Modo: audio.ModoSobDemanda, Reabriu: true}, ""},
		// O começo perdido no buffer empurra o início e deixa fala logo no
		// começo; o alerta é o do buffer, não o de microfone lento.
		{"o começo não coube no buffer", audio.Janela{Modo: audio.ModoSobDemanda, InicioRelMarca: 250 * time.Millisecond, PerdeuInicio: 200 * time.Millisecond, FalaNoInicio: true}, perdeu},
		{"o começo não coube, no sempre pronto", audio.Janela{Modo: audio.ModoSemprePronto, JaLigado: true, PerdeuInicio: 200 * time.Millisecond}, perdeu},
	} {
		t.Run(c.nome, func(t *testing.T) {
			b := novaBancada(t, true)
			b.rt.registrarJanela(1, c.janela)
			var alertas []string
			for _, l := range strings.Split(b.log.String(), "\n") {
				if strings.Contains(l, "level=WARN") {
					alertas = append(alertas, l)
				}
			}
			switch {
			case c.alerta == "" && len(alertas) > 0:
				t.Errorf("alerta sem motivo: %v", alertas)
			case c.alerta != "" && (len(alertas) != 1 || !strings.Contains(alertas[0], c.alerta)):
				t.Errorf("alertas = %v, quero só %q", alertas, c.alerta)
			}
		})
	}
}
