package audio

// Testes do microfone só enquanto dita (ADR-015). Todos contra o
// fakeDevice e o relógio falso: nada aqui abre microfone, e tudo roda no
// Linux e no Windows. O que só o hardware responde — o ícone de microfone
// do Windows apagar, a latência real do Start — está no roteiro manual
// (docs/teste-manual-bandeja-e-painel.md, "Parte 6 — Microfone só enquanto
// dita (ADR-015)").

import (
	"context"
	"errors"
	"math/big"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

var ctxFundo = context.Background()

// --- subida e repouso ---------------------------------------------------

// TestSobDemanda_NaoLigaNaSubida: no padrão, a subida prepara o microfone
// e não liga — é o que apaga o ícone do Windows em repouso.
func TestSobDemanda_NaoLigaNaSubida(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	time.Sleep(20 * time.Millisecond)
	if h := dev.historico(); len(h) != 0 {
		t.Errorf("o microfone foi mexido na subida: %v", h)
	}
	if n := dev.totalDeLeituras(); n != 0 {
		t.Errorf("%d leituras na subida, sem ditado nenhum", n)
	}
	if got := sess.Modo(); got != ModoSobDemanda {
		t.Errorf("Modo() = %q, quero %q", got, ModoSobDemanda)
	}
}

// TestSobDemanda_MarkStartLigaEAbortEstaciona: o toque curto liga e
// estaciona — com Reset —, mesmo sem o microfone ter entregue nada. É
// também a prova de que a leitura volta a cada consulta vazia: se ela só
// voltasse com pacote, o Abort nunca estacionaria.
func TestSobDemanda_MarkStartLigaEAbortEstaciona(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, dev, true)
	sess.Abort(1)
	esperarMicrofone(t, dev, false)

	esperarAte(t, "o Reset depois do Stop", func() bool { return dev.contar("zerar") == 1 })
	if h, want := dev.historico(), []string{"ligar", "parar", "zerar"}; !slices.Equal(h, want) {
		t.Errorf("chamadas = %v, quero %v", h, want)
	}
	semViolacoes(t, dev)
}

// TestSobDemanda_EstacionadoNaoConsultaODispositivo: estacionada, a
// goroutine de áudio fica bloqueada — nenhuma consulta a cada 10 ms. Sem
// isto o ícone apagaria, mas a CPU de repouso não cairia.
func TestSobDemanda_EstacionadoNaoConsultaODispositivo(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	time.Sleep(20 * time.Millisecond)
	if n := dev.totalDeLeituras(); n != 0 {
		t.Fatalf("%d leituras em repouso desde a subida", n)
	}

	sess.MarkStart(1, time.Now())
	esperarAte(t, "a primeira leitura", func() bool { return dev.totalDeLeituras() > 0 })
	sess.Abort(1)
	esperarMicrofone(t, dev, false)

	antes := dev.totalDeLeituras()
	time.Sleep(30 * time.Millisecond)
	if depois := dev.totalDeLeituras(); depois != antes {
		t.Errorf("%d leituras com o microfone estacionado", depois-antes)
	}
	semViolacoes(t, dev)
}

// --- AC-003 no sob demanda ----------------------------------------------

// TestSobDemanda_AC003_CapturaComecaAteLDepoisDaMarca é o AC-003 do modo
// padrão (ADR-015) no que o código controla: a MEDIÇÃO do início
// (InicioRelMarca acompanha o microfone, rápido ou lento) e nada perdido
// entre o primeiro frame e o corte.
//
// O "até L" em si é propriedade do microfone, não do código: nenhum
// teste com dublê o impõe. Quem o vigia é o alerta do runtime
// (winruntime, TestJanela_Alertas) e o uso real (ADR-015, "Revisão"); o
// valor de L está preso em TestLimiteDeInicio.
//
// O microfone falso demora para gravar o primeiro frame, como o de
// verdade. Com um Ligar instantâneo o teste provaria a medição à toa: o
// trecho entre a marca e o primeiro frame é sempre perdido no sob demanda,
// e é justamente ele que InicioRelMarca mede. Uma latência abaixo de L e
// uma acima: a medição não pode ser grampeada em nenhum dos dois lados.
func TestSobDemanda_AC003_CapturaComecaAteLDepoisDaMarca(t *testing.T) {
	for _, latencia := range []time.Duration{40 * time.Millisecond, 150 * time.Millisecond} {
		t.Run(latencia.String(), func(t *testing.T) {
			dev := newFakeDevice(SampleRate, 1)
			dev.latenciaLigar = latencia
			sess := startTestSession(t, dev, Config{})

			sess.MarkStart(1, time.Now())
			fala := encher(amostrasDe(300*time.Millisecond), 0.5)
			silencio := encher(amostrasDe(100*time.Millisecond), 0)
			dev.packets <- fala
			dev.packets <- silencio
			total := len(fala) + len(silencio)
			waitForCursor(t, sess, int64(total-1))

			buf, _, err := sess.Stop(ctxFundo, 1)
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			j := buf.Janela
			if j.InicioRelMarca != latencia {
				t.Errorf("InicioRelMarca = %s, quero %s — a medição do início não acompanha o microfone", j.InicioRelMarca, latencia)
			}
			// Nada se perde entre o primeiro frame e o corte (±1 amostra
			// que o Resampler segura).
			if diff := abs(len(buf.Samples) - total); diff > 1 {
				t.Errorf("len(buf.Samples) = %d, quero ~%d: áudio perdido entre o primeiro frame e o corte", len(buf.Samples), total)
			}
			if j.PreRollEntregue != 0 || j.JaLigado || j.Modo != ModoSobDemanda {
				t.Errorf("janela = %+v, quero sob demanda, sem pre-roll, microfone ligado pela marca", j)
			}
			// A fala já vinha no primeiro pacote: é o sinal de que a pessoa
			// começou a falar antes de o microfone abrir.
			if !j.FalaNoInicio {
				t.Error("FalaNoInicio = false com fala desde o primeiro pacote")
			}
			semViolacoes(t, dev)
		})
	}
}

// TestLimiteDeInicio é uma tripwire, como a do DefaultPreRoll: o L do
// AC-003 no sob demanda é provisório, por julgamento (ADR-015), e está
// escrito em três lugares que precisam andar juntos — esta constante, o
// texto da ADR-015 e o M4 do roteiro manual ("o critério é até 100 ms").
// Trocar o número exige passar por aqui e atualizar os outros dois.
func TestLimiteDeInicio(t *testing.T) {
	if LimiteDeInicio != 100*time.Millisecond {
		t.Errorf("LimiteDeInicio = %s, quero 100ms.\n"+
			"Se o L mudou — de preferência por um número medido, ADR-015 \"Revisão\" —, "+
			"atualize a ADR-015 e o M4 de docs/teste-manual-bandeja-e-painel.md, e então este teste.",
			LimiteDeInicio)
	}
}

// TestSobDemanda_PreRollZeroMesmoComOMicrofoneJaLigado: no sob demanda o
// pre-roll é zero sempre — também quando o microfone já roda por causa de
// outra tentativa. Senão, os 300 ms antes da marca seriam o ditado dela.
func TestSobDemanda_PreRollZeroMesmoComOMicrofoneJaLigado(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	sess.MarkStart(1, time.Now())
	alto := amostrasDe(300 * time.Millisecond)
	dev.packets <- encher(alto, 0.9)
	waitForCursor(t, sess, int64(alto-1))

	sess.MarkStart(2, time.Now()) // o microfone já está ligado pela tentativa 1
	baixo := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(baixo, 0)
	waitForCursor(t, sess, int64(alto+baixo-1))
	sess.Abort(1)

	buf, _, err := sess.Stop(ctxFundo, 2)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// A primeira amostra é a que o Resampler segurava do pacote anterior
	// (a defasagem de 1 amostra, 62,5 µs, descrita em
	// TestSession_StopIncludesPreRoll). Daí em diante, nada da tentativa 1.
	for i, s := range buf.Samples[1:] {
		if s != 0 {
			t.Fatalf("amostra %d = %d: o áudio da tentativa 1 entrou na 2", i+1, s)
		}
	}
	if !buf.Janela.JaLigado || buf.Janela.PreRollEntregue != 0 {
		t.Errorf("janela = %+v, quero JaLigado e pre-roll zero", buf.Janela)
	}
}

// --- a cauda com o microfone ligado ---------------------------------------

// TestSobDemanda_MicrofoneLigadoDuranteTodaACauda: a armadilha da cauda.
// Se o microfone estacionasse quando "não há mais marca", ele desligaria
// no meio da espera pela última sílaba — o "fator R" que virou "fator E"
// de volta. Conferido de dentro do dormir injetado.
func TestSobDemanda_MicrofoneLigadoDuranteTodaACauda(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	sess.MarkStart(1, time.Now())
	n := amostrasDe(400 * time.Millisecond)
	dev.packets <- encher(n, 0.5) // fala até o fim: a cauda vai esperar
	waitForCursor(t, sess, int64(n-1))

	esperas, desligou := 0, false
	sess.dormir = func(time.Duration) {
		esperas++
		// Tempo de sobra para a goroutine de áudio estacionar, se fosse.
		ate := time.Now().Add(20 * time.Millisecond)
		for time.Now().Before(ate) {
			if !dev.ligado() {
				desligou = true
			}
			time.Sleep(time.Millisecond)
		}
	}
	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if esperas == 0 {
		t.Fatal("a cauda não esperou: o teste não provou nada")
	}
	if desligou {
		t.Error("o microfone estacionou durante a espera da cauda")
	}
	if buf.Janela.DesligouAntesDoCorte {
		t.Error("Janela.DesligouAntesDoCorte = true")
	}
	esperarMicrofone(t, dev, false) // e estaciona depois do corte
	semViolacoes(t, dev)
}

// TestSobDemanda_DuasParadasSobrepostas: o watchdog do núcleo (5 s a 9,5 s,
// pelo texto) pode liberar um keydown novo com o Stop anterior ainda em
// curso. O microfone só estaciona depois da ÚLTIMA parada.
func TestSobDemanda_DuasParadasSobrepostas(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	// Uma espera de cauda por Stop: cada parada entra no dormir uma vez.
	sess := startTestSession(t, dev, Config{MargemDeCauda: PassoDeCauda})

	sess.MarkStart(1, time.Now())
	n := amostrasDe(200 * time.Millisecond)
	dev.packets <- encher(n, 0.9)
	waitForCursor(t, sess, int64(n-1))
	sess.MarkStart(2, time.Now())

	entrou, soltar := make(chan struct{}, 2), make(chan struct{})
	sess.dormir = func(time.Duration) {
		entrou <- struct{}{}
		<-soltar
	}
	fim := make(chan error, 2)
	parar := func(id dictation.AttemptID) {
		_, _, err := sess.Stop(ctxFundo, id)
		fim <- err
	}
	go parar(1)
	<-entrou
	go parar(2)
	<-entrou

	soltar <- struct{}{}
	if err := <-fim; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if !dev.ligado() {
		t.Error("o microfone estacionou com a outra parada ainda esperando a cauda")
	}

	soltar <- struct{}{}
	if err := <-fim; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	esperarMicrofone(t, dev, false)
	semViolacoes(t, dev)
}

// TestSobDemanda_AbortDuranteAParadaNaoDesligaAntesDoCorte: o Abort do
// watchdog pode chegar com o Stop ainda esperando a cauda. Quem tira uma
// tentativa "parando" é o Stop dela, depois do corte — senão o microfone
// desligaria no meio da última sílaba.
func TestSobDemanda_AbortDuranteAParadaNaoDesligaAntesDoCorte(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{MargemDeCauda: PassoDeCauda})

	sess.MarkStart(1, time.Now())
	n := amostrasDe(200 * time.Millisecond)
	dev.packets <- encher(n, 0.9)
	waitForCursor(t, sess, int64(n-1))

	entrou, soltar := make(chan struct{}, 1), make(chan struct{})
	sess.dormir = func(time.Duration) {
		entrou <- struct{}{}
		<-soltar
	}
	fim := make(chan AudioBuffer, 1)
	go func() {
		buf, _, _ := sess.Stop(ctxFundo, 1)
		fim <- buf
	}()
	<-entrou
	sess.Abort(1)
	time.Sleep(20 * time.Millisecond)
	if !dev.ligado() {
		t.Error("o Abort estacionou o microfone com o Stop ainda esperando a cauda")
	}
	close(soltar)
	if buf := <-fim; buf.Janela.DesligouAntesDoCorte {
		t.Error("Janela.DesligouAntesDoCorte = true")
	}
	esperarMicrofone(t, dev, false)
	semViolacoes(t, dev)
}

// --- Stop espera o microfone (ajuste A da contestação) ---------------------

// TestSobDemanda_StopEsperaOAudioDoInstanteDoKeyup: a tecla sobe antes de
// o pacote que cobre esse instante chegar. Cortar na hora perderia o fim.
func TestSobDemanda_StopEsperaOAudioDoInstanteDoKeyup(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{MargemDeCauda: -1}) // sem cauda: isola a espera do microfone

	sess.MarkStart(1, time.Now())
	antes := amostrasDe(200 * time.Millisecond)
	dev.packets <- encher(antes, 0.5)
	waitForCursor(t, sess, int64(antes-1))

	// O keyup acontece 50 ms depois do último áudio já lido.
	dev.relogio.avancar(50 * time.Millisecond)
	tarde := amostrasDe(100 * time.Millisecond)
	entregou := false
	sess.dormir = func(d time.Duration) {
		if !entregou {
			entregou = true
			dev.packets <- encher(tarde, 0.5)
			waitForCursor(t, sess, int64(antes+tarde-1))
		}
	}

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !entregou {
		t.Fatal("Stop não esperou o microfone")
	}
	if diff := abs(len(buf.Samples) - (antes + tarde)); diff > 1 {
		t.Errorf("len(buf.Samples) = %d, quero ~%d: o áudio do instante do keyup ficou de fora", len(buf.Samples), antes+tarde)
	}
	if buf.Janela.EsperouMicrofone <= 0 {
		t.Errorf("Janela.EsperouMicrofone = %s, quero > 0", buf.Janela.EsperouMicrofone)
	}
	if buf.Janela.FaltouNoFim != 0 {
		t.Errorf("Janela.FaltouNoFim = %s com o áudio do keyup entregue", buf.Janela.FaltouNoFim)
	}
}

// TestSobDemanda_AudioQueAcabaAntesDoKeyupDizQuantoFaltou: o microfone
// entregou o começo e parou de entregar, sem erro. Vencido o teto, Stop
// segue com o que há — mas a janela diz quanto do fim ficou de fora, em vez
// de só esperou_mic_ms=1000 e nenhum aviso.
func TestSobDemanda_AudioQueAcabaAntesDoKeyupDizQuantoFaltou(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{MargemDeCauda: -1})

	sess.MarkStart(1, time.Now())
	n := amostrasDe(200 * time.Millisecond)
	dev.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))
	dev.relogio.avancar(300 * time.Millisecond) // a tecla sobe 300 ms depois do último áudio

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop: %v — com áudio desde a marca, é para seguir com o que há", err)
	}
	if buf.Janela.EsperouMicrofone != DefaultEsperaPeloMicrofone {
		t.Errorf("esperou %s, quero o teto de %s", buf.Janela.EsperouMicrofone, DefaultEsperaPeloMicrofone)
	}
	if buf.Janela.FaltouNoFim != 300*time.Millisecond {
		t.Errorf("Janela.FaltouNoFim = %s, quero 300ms", buf.Janela.FaltouNoFim)
	}
	if diff := abs(len(buf.Samples) - n); diff > 1 {
		t.Errorf("len(buf.Samples) = %d, quero ~%d", len(buf.Samples), n)
	}
}

// TestSobDemanda_MicrofoneQueNaoAbreDaErroProprio: sem nenhum pacote até o
// teto, o erro diz que o microfone não abriu — nunca um buffer vazio, que
// o núcleo leria como "nada reconhecido como fala".
func TestSobDemanda_MicrofoneQueNaoAbreDaErroProprio(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, dev, true) // ligou, mas não entrega nada

	buf, _, err := sess.Stop(ctxFundo, 1)
	if !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}
	if errors.Is(err, ErrDeviceLost) {
		t.Errorf("err = %v: não abrir a tempo não é perder o dispositivo", err)
	}
	if len(buf.Samples) != 0 {
		t.Errorf("devolveu %d amostras junto com o erro", len(buf.Samples))
	}
	if buf.Janela.EsperouMicrofone != DefaultEsperaPeloMicrofone {
		t.Errorf("esperou %s, quero o teto de %s", buf.Janela.EsperouMicrofone, DefaultEsperaPeloMicrofone)
	}
	esperarMicrofone(t, dev, false) // e estaciona mesmo assim
	semViolacoes(t, dev)
}

// TestSobDemanda_StartPresoNoDriverDaErroProprio: a tecla sobe com o Start
// ainda preso no driver. O erro é "não abriu a tempo" — e não "desligou
// antes do corte", que mandaria procurar o defeito no lugar errado. Quando
// o Start enfim volta, o microfone estaciona, porque ninguém mais precisa
// dele.
func TestSobDemanda_StartPresoNoDriverDaErroProprio(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.ligarPreso = make(chan struct{})
	dev.noLigar = make(chan struct{}, 1)
	sess := startTestSession(t, dev, Config{})
	// Solta o Start antes do Close da sessão mesmo se o teste falhar no
	// meio: com ele preso, o Close esperaria para sempre.
	soltar := sync.OnceFunc(func() { close(dev.ligarPreso) })
	t.Cleanup(soltar)

	sess.MarkStart(1, time.Now())
	select {
	case <-dev.noLigar: // a goroutine de áudio está dentro do Start
	case <-time.After(2 * time.Second):
		t.Fatal("o Start nunca foi chamado")
	}
	buf, _, err := sess.Stop(ctxFundo, 1)
	if !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}
	if buf.Janela.DesligouAntesDoCorte {
		t.Error("Janela.DesligouAntesDoCorte = true para um microfone que nem ligou")
	}

	soltar()
	esperarAte(t, "o Start voltar e o microfone estacionar", func() bool { return dev.contar("parar") == 1 })
	esperarMicrofone(t, dev, false)
	semViolacoes(t, dev)
}

// TestSobDemanda_CloseNaoTravaComOStartPreso: com o Start preso no driver,
// a goroutine de áudio não volta. Close desiste depois do teto e diz por
// quê — sem isso, o "Sair" da bandeja travaria o Agent.
func TestSobDemanda_CloseNaoTravaComOStartPreso(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.ligarPreso = make(chan struct{})
	dev.noLigar = make(chan struct{}, 1)
	sess := startTestSession(t, dev, Config{})
	soltar := sync.OnceFunc(func() { close(dev.ligarPreso) })
	t.Cleanup(soltar)
	sess.esperaAoFechar = 50 * time.Millisecond

	sess.MarkStart(1, time.Now())
	select {
	case <-dev.noLigar:
	case <-time.After(2 * time.Second):
		t.Fatal("o Start nunca foi chamado")
	}
	fechou := make(chan error, 1)
	go func() { fechou <- sess.Close() }()
	select {
	case err := <-fechou:
		if err == nil {
			t.Error("Close devolveu nil com a goroutine de áudio ainda presa no Start")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close travou com o Start preso no driver")
	}
	soltar()
	esperarAte(t, "a goroutine de áudio terminar depois de o Start voltar", dev.fechado)
}

// TestSobDemanda_StartQueFalhaReabreOEndpoint: o cliente preparado na
// subida pode ficar inválido com o Agent parado (suspender e retomar, o
// microfone USB retirado), e o Start do keydown falha. Soltar o cliente e
// reabrir o endpoint padrão salva o ditado — sem isso, o áudio morreria
// até reiniciar o Agent.
func TestSobDemanda_StartQueFalhaReabreOEndpoint(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.falhasAoLigar = 1
	sess := startTestSession(t, dev, Config{})

	buf := ditar(t, sess, dev, 1, 100*time.Millisecond, 0.5)
	if h, want := dev.historico(), []string{"ligar falhou", "liberar", "reabrir", "ligar"}; len(h) < 4 || !slices.Equal(h[:4], want) {
		t.Errorf("chamadas = %v, quero começar por %v", h, want)
	}
	if !buf.Janela.Reabriu || !buf.Janela.ReabriuPorFalha {
		t.Errorf("janela = %+v, quero Reabriu e ReabriuPorFalha", buf.Janela)
	}
	esperarMicrofone(t, dev, false)

	// O ditado seguinte liga normalmente, sem a marca de falha.
	buf = ditar(t, sess, dev, 2, 100*time.Millisecond, 0.5)
	if buf.Janela.ReabriuPorFalha {
		t.Error("ReabriuPorFalha ficou grudado no ditado seguinte")
	}
	semViolacoes(t, dev)
}

// TestSobDemanda_StartQueFalhaDeNovoPerdeSoEsseDitado: o microfone USB saiu
// no meio de um ditado, e a pessoa tenta de novo antes de pô-lo de volta —
// nem o Start nem a reabertura ligam. Falha esse ditado, na hora e com o erro
// do dispositivo, e a sessão segue viva: posto o USB de volta, o keydown
// seguinte dita. Até a revisão de 29-set-2026 (segunda rodada), a sessão
// morria aqui, e só reiniciar o Agent trazia o microfone de volta.
func TestSobDemanda_StartQueFalhaDeNovoPerdeSoEsseDitado(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, dev, Config{})
	recuos := comRecuosFalsos(sess, false)

	// Ditado 1: a leitura falha no meio (o USB saiu).
	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, dev, true)
	dev.falharLeitura(errInvalidado)
	esperarAte(t, "a falha soltar o microfone", func() bool { return dev.contar("liberar") == 1 })
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("Stop 1: err = %v, quero ErrDeviceLost", err)
	}

	// Ditado 2, ainda sem o USB: o Start e a reabertura falham.
	dev.mu.Lock()
	dev.falhasAoLigar = 2
	dev.mu.Unlock()
	sess.MarkStart(2, time.Now())
	esperarAte(t, "o aviso de que o microfone não ligou", func() bool { return log.contar(linhaNaoLigou) == 1 })
	if n := dev.contar("ligar falhou"); n != 2 {
		t.Errorf("%d Start que falharam, quero 2 (o do keydown e o da reabertura): %v", n, dev.historico())
	}
	if sess.MicrofoneEntregando(2) {
		t.Error("o microfone 'entrega' para um ditado cujo Start falhou: o bipe mentiria")
	}
	buf, _, err := sess.Stop(ctxFundo, 2)
	if !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("Stop 2: err = %v, quero ErrDeviceLost", err)
	}
	if buf.Janela.EsperouMicrofone != 0 {
		t.Errorf("Stop 2 esperou o microfone %s: o Start já tinha falhado, o erro sai na hora", buf.Janela.EsperouMicrofone)
	}
	if err := sess.Falha(); err != nil || terminou(sess) {
		t.Fatalf("a sessão morreu num keydown sem microfone: Falha() = %v", err)
	}

	// Ditado 3, com o USB de volta: dita, reabrindo o endpoint.
	b3 := ditar(t, sess, dev, 3, 100*time.Millisecond, 0.5)
	if !b3.Janela.Reabriu {
		t.Error("o keydown com o microfone de volta não reabriu o endpoint")
	}

	if r := recuos.lista(); len(r) != 0 {
		t.Errorf("o sob demanda entrou em recuo: %v", r)
	}
	l := log.linha(t, linhaNaoLigou)
	for _, campo := range []string{"level=WARN", "modo=sob_demanda", "tentativas_perdidas=1"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha do Start que falhou não tem %q: %s", campo, l)
		}
	}
	if strings.Contains(l, "err=") || strings.Contains(l, "INVALIDATED") {
		t.Errorf("a linha do Start que falhou leva o erro; deveria ter só números e o modo: %s", l)
	}
	// Uma linha de reabertura por falha fechada: a leitura do ditado 1 e o
	// Start do ditado 2 são a mesma, fechada pelo ditado 3.
	if n := log.contar(linhaReabriu); n != 1 {
		t.Errorf("%d linhas de reabertura, quero 1:\n%s", n, log.String())
	}
	if n := log.contar("level=ERROR"); n != 0 {
		t.Errorf("ERROR num microfone que voltou:\n%s", log.String())
	}
	semViolacoes(t, dev)
}

// TestSobDemanda_KeydownsSemMicrofoneNaoViramLaco: com o microfone fora,
// cada Ctrl+Win+seta é um keydown que tenta ligar e falha. Cada um tenta
// duas vezes (o Start e a reabertura) e só: nada tenta sem keydown, nada lê
// o dispositivo, e a sessão não morre.
func TestSobDemanda_KeydownsSemMicrofoneNaoViramLaco(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.falhasAoLigar = 1000
	sess, log := sessaoComLog(t, dev, Config{})
	comRecuosFalsos(sess, false)

	const atalhos = 5
	for i := 1; i <= atalhos; i++ {
		id := dictation.AttemptID(i)
		sess.MarkStart(id, time.Now())
		esperarAte(t, "o keydown tentar ligar", func() bool { return log.contar(linhaNaoLigou) == i })
		sess.Abort(id)
	}
	time.Sleep(30 * time.Millisecond) // um laço já teria tentado de novo
	if n := dev.contar("ligar falhou"); n != 2*atalhos {
		t.Errorf("%d Start que falharam em %d keydowns, quero %d: %v", n, atalhos, 2*atalhos, dev.historico())
	}
	if n := dev.totalDeLeituras(); n != 0 {
		t.Errorf("%d leituras sem o microfone ligar", n)
	}
	if err := sess.Falha(); err != nil || terminou(sess) {
		t.Errorf("a sessão morreu: Falha() = %v", err)
	}

	// O microfone volta: o keydown seguinte dita, e o log fecha a falha —
	// que aqui começou por um Start, sem leitura nenhuma que falhasse.
	dev.mu.Lock()
	dev.falhasAoLigar = 0
	dev.mu.Unlock()
	ditar(t, sess, dev, atalhos+1, 100*time.Millisecond, 0.5)
	if n := log.contar(linhaReabriu); n != 1 {
		t.Errorf("%d linhas de reabertura, quero 1:\n%s", n, log.String())
	}
	semViolacoes(t, dev)
}

// TestSobDemanda_MicrofoneQueAbreDepoisDeSoltarDaErroProprio: o primeiro
// frame só foi gravado depois de a tecla subir. Nada do que foi dito com a
// tecla segurada foi capturado.
func TestSobDemanda_MicrofoneQueAbreDepoisDeSoltarDaErroProprio(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.latenciaLigar = 300 * time.Millisecond
	sess := startTestSession(t, dev, Config{MargemDeCauda: -1})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, dev, true)
	dev.relogio.avancar(200 * time.Millisecond) // a tecla sobe aos 200 ms

	entregou := false
	n := amostrasDe(100 * time.Millisecond)
	sess.dormir = func(time.Duration) {
		if !entregou {
			entregou = true
			dev.packets <- encher(n, 0.5)
			waitForCursor(t, sess, int64(n-1))
		}
	}
	_, _, err := sess.Stop(ctxFundo, 1)
	if !entregou {
		t.Fatal("o pacote tardio nunca foi entregue: o teste não provou nada")
	}
	if !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}
}

// --- estacionar: Stop + Reset, e o que fazer quando falha ----------------

// TestSobDemanda_SegundoDitadoNaoTemNadaDoPrimeiro: o WASAPI guarda
// pacotes pendentes depois do Stop, e o Start seguinte os entregaria — o
// fim do ditado anterior no começo do próximo. O Reset descarta.
func TestSobDemanda_SegundoDitadoNaoTemNadaDoPrimeiro(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.sobraAoParar = encher(amostrasDe(50*time.Millisecond), 0.9)
	dev.idadeDaSobra = 50 * time.Millisecond
	sess := startTestSession(t, dev, Config{})

	sess.MarkStart(1, time.Now())
	n1 := amostrasDe(300 * time.Millisecond)
	dev.packets <- encher(n1, 0.9)
	waitForCursor(t, sess, int64(n1-1))
	if _, _, err := sess.Stop(ctxFundo, 1); err != nil {
		t.Fatalf("Stop 1: %v", err)
	}
	esperarAte(t, "o Reset", func() bool { return dev.contar("zerar") == 1 })

	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	sess.MarkStart(2, time.Now())
	n2 := amostrasDe(200 * time.Millisecond)
	dev.packets <- encher(n2, 0)
	waitForCursor(t, sess, base+int64(n2-1))

	buf, _, err := sess.Stop(ctxFundo, 2)
	if err != nil {
		t.Fatalf("Stop 2: %v", err)
	}
	for i, s := range buf.Samples {
		if s != 0 {
			t.Fatalf("amostra %d = %d: o ditado 2 tem áudio do ditado 1", i, s)
		}
	}
	if buf.Janela.PacoteAnteriorAoEstacionamento {
		t.Error("detector de contaminação disparou numa janela limpa")
	}
	if buf.Janela.FalaNoInicio {
		t.Error("FalaNoInicio = true num ditado em silêncio")
	}
	semViolacoes(t, dev)
}

// TestSobDemanda_CaudaNaoOlhaODitadoAnterior: num ditado curtíssimo, os
// últimos 80 ms do ring ainda seriam o fim do ditado anterior. A cauda só
// olha a janela atual — senão esperaria à toa por uma fala que já acabou.
func TestSobDemanda_CaudaNaoOlhaODitadoAnterior(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	ditar(t, sess, dev, 1, 300*time.Millisecond, 0.9) // termina falando
	esperarMicrofone(t, dev, false)
	antes := esperaDeCauda(sess)

	ditar(t, sess, dev, 2, 20*time.Millisecond, 0) // 20 ms de silêncio
	if got := esperaDeCauda(sess) - antes; got != 0 {
		t.Errorf("esperou %s pela cauda de um ditado em silêncio: olhou o ditado anterior", got)
	}
}

// TestSobDemanda_ResetQueNaoLimpaEDetectado: se o Reset não limpar (driver
// com defeito), o detector aponta — o log é o que avisa, porque o texto
// errado sai no campo de quem usa.
func TestSobDemanda_ResetQueNaoLimpaEDetectado(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.sobraAoParar = encher(amostrasDe(50*time.Millisecond), 0.9)
	dev.idadeDaSobra = 50 * time.Millisecond
	dev.zerarNaoLimpa = true
	sess := startTestSession(t, dev, Config{})

	ditar(t, sess, dev, 1, 300*time.Millisecond, 0.9)
	esperarMicrofone(t, dev, false)

	sess.MarkStart(2, time.Now())
	esperarAte(t, "a sobra ser entregue", func() bool {
		return sess.MicrofoneEntregando(2)
	})
	buf, _, err := sess.Stop(ctxFundo, 2)
	if err != nil {
		t.Fatalf("Stop 2: %v", err)
	}
	if !buf.Janela.PacoteAnteriorAoEstacionamento {
		t.Error("áudio do ditado anterior entrou e o detector não disparou")
	}
}

// TestSemprePronto_ContaminacaoSoNoDitadoQuePegaOInicioDaJanela: o
// detector olha o primeiro pacote da janela, e numa janela longa do sempre
// pronto só o ditado cujo corte começa no início dela carrega esse pacote.
// Os seguintes não podem herdar o alerta.
func TestSemprePronto_ContaminacaoSoNoDitadoQuePegaOInicioDaJanela(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.sobraAoParar = encher(amostrasDe(50*time.Millisecond), 0.9)
	dev.idadeDaSobra = 50 * time.Millisecond
	dev.zerarNaoLimpa = true
	sess := startTestSession(t, dev, Config{})

	ditar(t, sess, dev, 1, 300*time.Millisecond, 0.9)
	esperarMicrofone(t, dev, false)
	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()

	// A janela do sempre pronto abre com a sobra do ditado 1: contaminada.
	sess.DefinirSemprePronto(true)
	sobra := int64(len(dev.sobraAoParar))
	waitForCursor(t, sess, base+sobra-1)

	// O ditado 2 começa em cima: o corte inclui o início da janela.
	sess.MarkStart(2, time.Now())
	curto := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(curto, 0)
	waitForCursor(t, sess, base+sobra+int64(curto)-1)
	buf, _, err := sess.Stop(ctxFundo, 2)
	if err != nil {
		t.Fatalf("Stop 2: %v", err)
	}
	if !buf.Janela.PacoteAnteriorAoEstacionamento {
		t.Fatal("o ditado que pega o início da janela contaminada não foi marcado: o teste não provou nada")
	}

	// O ditado 3 começa 500 ms depois: o pre-roll de 300 ms não chega ao
	// início da janela.
	longo := amostrasDe(500 * time.Millisecond)
	dev.packets <- encher(longo, 0)
	waitForCursor(t, sess, base+sobra+int64(curto+longo)-1)
	sess.MarkStart(3, time.Now())
	dev.packets <- encher(curto, 0)
	waitForCursor(t, sess, base+sobra+int64(2*curto+longo)-1)
	buf, _, err = sess.Stop(ctxFundo, 3)
	if err != nil {
		t.Fatalf("Stop 3: %v", err)
	}
	if buf.Janela.PreRollEntregue != DefaultPreRoll {
		t.Fatalf("PreRollEntregue = %s, quero %s: o corte do ditado 3 teria de parar antes do início da janela", buf.Janela.PreRollEntregue, DefaultPreRoll)
	}
	if buf.Janela.PacoteAnteriorAoEstacionamento {
		t.Error("o ditado 3 herdou o alerta de contaminação do primeiro pacote da janela, que não está no corte dele")
	}
	semViolacoes(t, dev)
}

// TestSobDemanda_PacoteDentroDeUmPeriodoNaoEContaminacao: o motor carimba
// o primeiro frame até um período antes do Start. Num Start logo depois do
// estacionamento, isso não é áudio do ditado passado.
func TestSobDemanda_PacoteDentroDeUmPeriodoNaoEContaminacao(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.latenciaLigar = -5 * time.Millisecond
	sess := startTestSession(t, dev, Config{})

	ditar(t, sess, dev, 1, 100*time.Millisecond, 0.5)
	esperarMicrofone(t, dev, false)
	buf := ditar(t, sess, dev, 2, 100*time.Millisecond, 0.5)
	if buf.Janela.PacoteAnteriorAoEstacionamento {
		t.Error("um pacote carimbado 5 ms antes do Start virou contaminação: falta a tolerância de um período")
	}
}

func TestPacoteAnteriorAoEstacionamento(t *testing.T) {
	const ms = int64(10_000) // 1 ms em 100 ns
	for _, c := range []struct {
		nome                 string
		primeiro, estacionou int64
		periodo              time.Duration
		contaminado          bool
	}{
		{"nunca estacionou", 100 * ms, 0, 10 * time.Millisecond, false},
		{"depois do estacionamento", 1000 * ms, 900 * ms, 10 * time.Millisecond, false},
		{"dentro de um período antes", 895 * ms, 900 * ms, 10 * time.Millisecond, false},
		{"exatamente um período antes", 890 * ms, 900 * ms, 10 * time.Millisecond, false},
		{"mais de um período antes", 850 * ms, 900 * ms, 10 * time.Millisecond, true},
	} {
		if got := pacoteAnteriorAoEstacionamento(c.primeiro, c.estacionou, c.periodo); got != c.contaminado {
			t.Errorf("%s: = %v, quero %v", c.nome, got, c.contaminado)
		}
	}
}

// TestEstacionar_ResetQueFalhaLiberaOCliente: sem Reset, o cliente não
// pode ficar preparado. Liberar e reabrir no próximo ditado é o caminho.
func TestEstacionar_ResetQueFalhaLiberaOCliente(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.falharZerar = errors.New("AUDCLNT_E_BUFFER_OPERATION_PENDING")
	sess := startTestSession(t, dev, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, dev, true)
	sess.Abort(1)
	esperarAte(t, "o cliente ser liberado", func() bool { return dev.contar("liberar") == 1 })
	if h, want := dev.historico(), []string{"ligar", "parar", "zerar", "liberar"}; !slices.Equal(h, want) {
		t.Errorf("chamadas = %v, quero %v", h, want)
	}

	dev.falharZerar = nil
	buf := ditar(t, sess, dev, 2, 100*time.Millisecond, 0.5)
	if dev.contar("reabrir") != 1 {
		t.Errorf("chamadas = %v: o ditado seguinte não reabriu o cliente", dev.historico())
	}
	if !buf.Janela.Reabriu {
		t.Error("Janela.Reabriu = false depois de um estacionamento que liberou")
	}
	semViolacoes(t, dev)
}

// TestEstacionar_PoliticaLiberarNaoFazReset: com a política Liberar (o
// plano B do roteiro manual, M7), estacionar solta o cliente direto.
func TestEstacionar_PoliticaLiberarNaoFazReset(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{Politica: PoliticaLiberar})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, dev, true)
	sess.Abort(1)
	esperarAte(t, "o cliente ser liberado", func() bool { return dev.contar("liberar") == 1 })
	if h, want := dev.historico(), []string{"ligar", "parar", "liberar"}; !slices.Equal(h, want) {
		t.Errorf("chamadas = %v, quero %v", h, want)
	}
	semViolacoes(t, dev)
}

func TestPoliticaDeEstacionamento(t *testing.T) {
	falha := errors.New("falhou")
	for _, c := range []struct {
		nome               string
		padrao             Politica
		errParar, errZerar error
		quero              Politica
	}{
		{"parar deu certo", PoliticaParar, nil, nil, PoliticaParar},
		{"Stop falhou", PoliticaParar, falha, nil, PoliticaLiberar},
		{"Reset falhou", PoliticaParar, nil, falha, PoliticaLiberar},
		{"padrão liberar", PoliticaLiberar, nil, nil, PoliticaLiberar},
	} {
		if got := politicaDeEstacionamento(c.padrao, c.errParar, c.errZerar); got != c.quero {
			t.Errorf("%s: = %v, quero %v", c.nome, got, c.quero)
		}
	}
}

// TestLigarFalhou: os números vêm de audioclient.h, escritos à mão aqui e
// não da constante de produção — senão um valor errado lá passaria aqui
// também, como já passou.
func TestLigarFalhou(t *testing.T) {
	for _, c := range []struct {
		nome   string
		hr     uint32
		falhou bool
	}{
		{"S_OK", 0, false},
		{"S_FALSE", 1, false},
		{"AUDCLNT_E_NOT_STOPPED: já rodando", 0x88890005, false},
		{"AUDCLNT_E_ALREADY_INITIALIZED", 0x88890002, true},
		{"AUDCLNT_E_NOT_INITIALIZED", 0x88890001, true},
		{"AUDCLNT_E_DEVICE_INVALIDATED", 0x88890004, true},
		{"E_FAIL", 0x80004005, true},
	} {
		if got := ligarFalhou(int32(c.hr)); got != c.falhou {
			t.Errorf("%s: ligarFalhou = %v, quero %v", c.nome, got, c.falhou)
		}
	}
}

// --- a ordem do liga-desliga (ajuste C da contestação) ---------------------

// TestSobDemanda_LigarSoAPartirDeEstacionado: toque seguido de keydown, em
// rajada — o caso em que a goroutine acorda com o microfone ainda rodando.
// Um Start duplo daria AUDCLNT_E_NOT_STOPPED e mataria o áudio até
// reiniciar o Agent. O dublê recusa.
func TestSobDemanda_LigarSoAPartirDeEstacionado(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	for i := 1; i <= 50; i++ {
		id := dictation.AttemptID(i)
		sess.MarkStart(id, time.Now())
		if i%7 == 0 {
			time.Sleep(time.Millisecond)
		}
		sess.Abort(id)
	}
	sess.MarkStart(100, time.Now())
	esperarMicrofone(t, dev, true)
	sess.Abort(100)
	esperarMicrofone(t, dev, false)

	semViolacoes(t, dev)
	esperarAte(t, "cada Start ter o seu Stop", func() bool { return dev.contar("ligar") == dev.contar("parar") })
}

// TestSobDemanda_MarcaDuranteOEstacionamentoReligaNumaJanelaNova: uma marca
// que chega com o Stop+Reset em curso não pode se achar na janela que está
// acabando. O Reset descarta o que veio depois dela, e o microfone religa
// numa janela nova — a linha do log tem de dizer isso: não "já ligado", e
// com o tempo até o primeiro pacote, que é o que dispara o alerta de
// microfone lento.
func TestSobDemanda_MarcaDuranteOEstacionamentoReligaNumaJanelaNova(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.latenciaLigar = 150 * time.Millisecond
	var sess *Session
	marcou := false // só a goroutine de áudio lê e escreve
	dev.aoParar = func() {
		if !marcou {
			marcou = true
			sess.MarkStart(2, time.Now())
		}
	}
	sess = startTestSession(t, dev, Config{MargemDeCauda: -1})

	ditar(t, sess, dev, 1, 100*time.Millisecond, 0.5)
	esperarAte(t, "o microfone religar para a marca 2", func() bool { return dev.contar("ligar") == 2 })
	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	n := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(n, 0.5)
	waitForCursor(t, sess, base+int64(n-1))

	buf, _, err := sess.Stop(ctxFundo, 2)
	if err != nil {
		t.Fatalf("Stop 2: %v", err)
	}
	j := buf.Janela
	if j.JaLigado {
		t.Error("Janela.JaLigado = true para uma marca feita com o microfone estacionando")
	}
	if j.MarcaAtePrimeiroPacote <= 0 {
		t.Errorf("Janela.MarcaAtePrimeiroPacote = %s: o tempo até o primeiro pacote sumiu da linha", j.MarcaAtePrimeiroPacote)
	}
	if j.InicioRelMarca != dev.latenciaLigar {
		t.Errorf("Janela.InicioRelMarca = %s, quero %s", j.InicioRelMarca, dev.latenciaLigar)
	}
	if h, want := dev.historico(), []string{"ligar", "parar", "zerar", "ligar"}; !slices.Equal(h[:4], want) {
		t.Errorf("chamadas = %v, quero começar por %v", h, want)
	}
	semViolacoes(t, dev)
}

// TestProximoPasso: a decisão do laço de áudio. Decidir estacionar já
// deixa o estado em "desligado" — no mesmo lock, sem fresta para uma marca
// cair entre a decisão e o estacionamento.
func TestProximoPasso(t *testing.T) {
	for _, c := range []struct {
		nome                 string
		semprePronto, ligado bool
		tentativas           int
		quero                passo
		ligadoDepois         bool
	}{
		{"estacionado, ninguém precisa", false, false, 0, passoEsperar, false},
		{"estacionado, com marca", false, false, 1, passoLigar, false},
		{"estacionado, sempre pronto", true, false, 0, passoLigar, false},
		{"ligado, com marca", false, true, 1, passoLer, true},
		{"ligado, sempre pronto", true, true, 0, passoLer, true},
		{"ligado, ninguém mais precisa", false, true, 0, passoEstacionar, false},
	} {
		s := &Session{
			semprePronto: c.semprePronto,
			ligado:       c.ligado,
			tentativas:   make(map[dictation.AttemptID]*tentativa),
			janela:       janelaAtual{temPacote: c.ligado},
		}
		for i := 0; i < c.tentativas; i++ {
			s.tentativas[dictation.AttemptID(i+1)] = &tentativa{}
		}
		if got := s.proximoPasso(); got != c.quero {
			t.Errorf("%s: proximoPasso = %d, quero %d", c.nome, got, c.quero)
		}
		if s.ligado != c.ligadoDepois || (!s.ligado && s.janela.temPacote) {
			t.Errorf("%s: depois da decisão ligado=%v temPacote=%v, quero ligado=%v e sem pacote se desligado",
				c.nome, s.ligado, s.janela.temPacote, c.ligadoDepois)
		}
	}
}

// TestSobDemanda_LigarEPararPareadosEmTodoCaminho: sucesso, toque e falha
// terminam com o microfone estacionado, um Stop para cada Start.
func TestSobDemanda_LigarEPararPareadosEmTodoCaminho(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	// Sucesso.
	ditar(t, sess, dev, 1, 100*time.Millisecond, 0.5)
	esperarMicrofone(t, dev, false)
	// Toque.
	sess.MarkStart(2, time.Now())
	esperarMicrofone(t, dev, true)
	sess.Abort(2)
	esperarMicrofone(t, dev, false)
	// Falha: o microfone não entrega nada.
	sess.MarkStart(3, time.Now())
	esperarMicrofone(t, dev, true)
	if _, _, err := sess.Stop(ctxFundo, 3); !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}
	esperarMicrofone(t, dev, false)

	// O ditado que não recebeu áudio solta o cliente ao estacionar
	// (liberarNoEstacionar): Stop sem Reset, e Liberar.
	esperarAte(t, "o cliente solto", func() bool { return dev.contar("liberar") == 1 })
	if l, p, z := dev.contar("ligar"), dev.contar("parar"), dev.contar("zerar"); l != 3 || p != 3 || z != 2 {
		t.Errorf("ligar=%d parar=%d zerar=%d, quero 3, 3 e 2", l, p, z)
	}
	// E o ditado seguinte reabre o microfone e funciona.
	ditar(t, sess, dev, 4, 100*time.Millisecond, 0.5)
	esperarMicrofone(t, dev, false)
	esperarAte(t, "o Reset do ditado que voltou", func() bool { return dev.contar("zerar") == 3 })
	if l, p := dev.contar("ligar"), dev.contar("parar"); l != 4 || p != 4 {
		t.Errorf("ligar=%d parar=%d, quero 4 e 4", l, p)
	}
	semViolacoes(t, dev)
}

// --- sempre pronto --------------------------------------------------------

// TestSemprePronto_LigaNaSubidaNuncaEstacionaEEntrega300ms: o modo
// opcional é o comportamento de antes da ADR-015, intacto.
func TestSemprePronto_LigaNaSubidaNuncaEstacionaEEntrega300ms(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{SemprePronto: true})
	if !dev.ligado() {
		t.Fatal("no sempre pronto o microfone liga na subida")
	}
	if got := sess.Modo(); got != ModoSemprePronto {
		t.Errorf("Modo() = %q, quero %q", got, ModoSemprePronto)
	}

	antes := amostrasDe(500 * time.Millisecond)
	dev.packets <- encher(antes, 0.5)
	waitForCursor(t, sess, int64(antes-1))
	sess.MarkStart(1, time.Now())
	vivo := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(vivo, 0.5)
	waitForCursor(t, sess, int64(antes+vivo-1))

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if buf.Janela.PreRollEntregue != DefaultPreRoll {
		t.Errorf("PreRollEntregue = %s, quero %s", buf.Janela.PreRollEntregue, DefaultPreRoll)
	}
	if buf.Janela.InicioRelMarca != -DefaultPreRoll {
		t.Errorf("InicioRelMarca = %s, quero %s", buf.Janela.InicioRelMarca, -DefaultPreRoll)
	}
	if want := amostrasDe(DefaultPreRoll) + vivo; abs(len(buf.Samples)-want) > 1 {
		t.Errorf("len(buf.Samples) = %d, quero ~%d", len(buf.Samples), want)
	}
	if buf.Janela.PerdeuInicio != 0 {
		t.Errorf("PerdeuInicio = %s num ditado que cabe no ring", buf.Janela.PerdeuInicio)
	}

	time.Sleep(20 * time.Millisecond)
	if !dev.ligado() || dev.contar("parar") != 0 {
		t.Errorf("o sempre pronto estacionou: %v", dev.historico())
	}
	semViolacoes(t, dev)
}

// TestSemprePronto_PreRollNaoAtravessaAParada: religar o sempre pronto não
// traz áudio velho do ring. Antes do início da janela, o ring guarda o que
// foi dito antes da parada.
func TestSemprePronto_PreRollNaoAtravessaAParada(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{SemprePronto: true})

	n := amostrasDe(500 * time.Millisecond)
	dev.packets <- encher(n, 0.9) // fala de antes da parada
	waitForCursor(t, sess, int64(n-1))

	sess.DefinirSemprePronto(false)
	esperarMicrofone(t, dev, false)
	sess.DefinirSemprePronto(true)
	esperarMicrofone(t, dev, true)

	sess.MarkStart(1, time.Now())
	vivo := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(vivo, 0)
	waitForCursor(t, sess, int64(n-1+vivo-1))

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	for i, s := range buf.Samples {
		if s != 0 {
			t.Fatalf("amostra %d = %d: o pre-roll atravessou a parada do microfone", i, s)
		}
	}
	semViolacoes(t, dev)
}

// TestSession_DefinirSemprePronto: a opção muda com o Agent rodando, e
// desligá-la com um ditado em curso não corta o ditado.
func TestSession_DefinirSemprePronto(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	sess.DefinirSemprePronto(true)
	esperarMicrofone(t, dev, true)
	if got := sess.Modo(); got != ModoSemprePronto {
		t.Errorf("Modo() = %q depois de ligar a opção", got)
	}

	sess.MarkStart(1, time.Now())
	sess.DefinirSemprePronto(false)
	time.Sleep(20 * time.Millisecond)
	if !dev.ligado() {
		t.Fatal("desligar a opção estacionou o microfone no meio de um ditado")
	}
	sess.Abort(1)
	esperarMicrofone(t, dev, false)
	if got := sess.Modo(); got != ModoSobDemanda {
		t.Errorf("Modo() = %q depois de desligar a opção", got)
	}
	semViolacoes(t, dev)
}

// TestSession_DitadoTerminaNoModoDoKeydown: mudar a opção com um ditado em
// curso não muda aquele ditado — o pre-roll e o modo da linha do log são os
// do keydown, nos dois sentidos (ADR-015, decisão 7).
func TestSession_DitadoTerminaNoModoDoKeydown(t *testing.T) {
	t.Run("keydown no sempre pronto, desligado antes do Stop", func(t *testing.T) {
		dev := newFakeDevice(SampleRate, 1)
		sess := startTestSession(t, dev, Config{SemprePronto: true})
		antes := amostrasDe(500 * time.Millisecond)
		dev.packets <- encher(antes, 0.5)
		waitForCursor(t, sess, int64(antes-1))

		sess.MarkStart(1, time.Now())
		sess.DefinirSemprePronto(false)
		vivo := amostrasDe(100 * time.Millisecond)
		dev.packets <- encher(vivo, 0.5)
		waitForCursor(t, sess, int64(antes+vivo-1))

		buf, _, err := sess.Stop(ctxFundo, 1)
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if j := buf.Janela; j.PreRollEntregue != DefaultPreRoll || j.Modo != ModoSemprePronto {
			t.Errorf("PreRollEntregue = %s, modo = %s; quero %s e %s, os do keydown", j.PreRollEntregue, j.Modo, DefaultPreRoll, ModoSemprePronto)
		}
		esperarMicrofone(t, dev, false) // e estaciona depois do corte, já sob demanda
		semViolacoes(t, dev)
	})

	t.Run("keydown no sob demanda com o microfone já ligado, sempre pronto ligado antes do Stop", func(t *testing.T) {
		dev := newFakeDevice(SampleRate, 1)
		sess := startTestSession(t, dev, Config{})
		sess.MarkStart(1, time.Now())
		antes := amostrasDe(500 * time.Millisecond)
		dev.packets <- encher(antes, 0.9)
		waitForCursor(t, sess, int64(antes-1))

		sess.MarkStart(2, time.Now()) // o microfone já roda pela tentativa 1
		sess.DefinirSemprePronto(true)
		vivo := amostrasDe(100 * time.Millisecond)
		dev.packets <- encher(vivo, 0)
		waitForCursor(t, sess, int64(antes+vivo-1))
		sess.Abort(1)

		buf, _, err := sess.Stop(ctxFundo, 2)
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if j := buf.Janela; j.PreRollEntregue != 0 || j.Modo != ModoSobDemanda || !j.JaLigado {
			t.Errorf("PreRollEntregue = %s, modo = %s, já ligado = %v; quero 0, %s e true, os do keydown",
				j.PreRollEntregue, j.Modo, j.JaLigado, ModoSobDemanda)
		}
		semViolacoes(t, dev)
	})
}

// TestSession_MicrofoneEntregando: só depois do primeiro pacote — é o que
// segura o bipe e a pílula "Gravando" no runtime.
func TestSession_MicrofoneEntregando(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	if sess.MicrofoneEntregando(1) {
		t.Fatal("entregando para uma tentativa que nem foi marcada")
	}
	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, dev, true)
	if sess.MicrofoneEntregando(1) {
		t.Fatal("entregando com o microfone ligado mas sem pacote nenhum")
	}
	dev.packets <- encher(amostrasDe(20*time.Millisecond), 0)
	esperarAte(t, "o primeiro pacote", func() bool { return sess.MicrofoneEntregando(1) })
	sess.Abort(1)
	if sess.MicrofoneEntregando(1) {
		t.Error("entregando para uma tentativa já abortada")
	}
}

// --- o ring cabe o pior caso (revisão de 29-set-2026, M2) -------------------

// TestConfig_RingCabeOPiorCaso prende a conta do ring. A marca é no
// KEYDOWN, o limite de gravação do núcleo só começa quando o limiar vence,
// e o Stop do limite ainda espera o microfone e a cauda com o microfone
// ligado. Com PreRoll + MaxRecording só, uma fala de 120 s perdia o COMEÇO
// em silêncio.
func TestConfig_RingCabeOPiorCaso(t *testing.T) {
	for _, c := range []struct {
		nome     string
		cfg      Config
		segundos float64
	}{
		// 0,3 pre-roll + 2 limiar máximo + 120 gravação + 1 espera pelo
		// microfone + 0,2 cauda.
		{"padrões", Config{}, 123.5},
		// Sem cauda (margem negativa), a espera dela não entra.
		{"sem cauda", Config{MargemDeCauda: -1}, 123.3},
		{"explícito", Config{PreRoll: 100 * time.Millisecond, MaxRecording: 10 * time.Second, MaxHoldThreshold: 500 * time.Millisecond, MargemDeCauda: 100 * time.Millisecond}, 11.7},
	} {
		want := int(c.segundos * SampleRate)
		if got := c.cfg.withDefaults().capacitySamples(); abs(got-want) > 1 {
			t.Errorf("%s: o ring guarda %d amostras (%.3f s), quero %d (%.1f s)", c.nome, got, float64(got)/SampleRate, want, c.segundos)
		}
	}
}

// TestDefaultMaxHoldThreshold_EspelhaONucleo: o padrão do ring usa o maior
// limiar que o núcleo aceita. Se o núcleo aceitar mais, o ring tem de
// crescer junto.
func TestDefaultMaxHoldThreshold_EspelhaONucleo(t *testing.T) {
	if DefaultMaxHoldThreshold != dictation.MaxHoldThreshold {
		t.Errorf("DefaultMaxHoldThreshold = %s, e o núcleo aceita até %s", DefaultMaxHoldThreshold, dictation.MaxHoldThreshold)
	}
}

// TestSession_CorteMaisAntigoQueORingDizQuantoPerdeu: se o ditado passar
// do que o ring guarda, o começo se perde — e a janela diz quanto, em vez
// de o buffer simplesmente vir mais curto.
func TestSession_CorteMaisAntigoQueORingDizQuantoPerdeu(t *testing.T) {
	for _, c := range []struct {
		nome string
		// antes é quanto áudio o microfone já tinha entregue na marca.
		semprePronto bool
		antes        time.Duration
	}{
		{"sob demanda", false, 0},
		// O pre-roll pedido começa no início da janela, 200 ms antes da
		// marca, e também se perde.
		{"sempre pronto", true, 200 * time.Millisecond},
	} {
		t.Run(c.nome, func(t *testing.T) {
			dev := newFakeDevice(SampleRate, 1)
			// Ring de 1,5 s: 0,3 pre-roll + 0,1 limiar + 0,1 gravação + 1 espera.
			sess := startTestSession(t, dev, Config{
				SemprePronto: c.semprePronto,
				MaxRecording: 100 * time.Millisecond, MaxHoldThreshold: 100 * time.Millisecond, MargemDeCauda: -1,
			})
			if c.antes > 0 {
				dev.packets <- encher(amostrasDe(c.antes), 0.5)
				waitForCursor(t, sess, int64(amostrasDe(c.antes)-1))
			}
			sess.mu.Lock()
			marca := sess.ring.Cursor()
			sess.mu.Unlock()

			sess.MarkStart(1, time.Now())
			n := amostrasDe(1700 * time.Millisecond)
			dev.packets <- encher(n, 0.5)
			waitForCursor(t, sess, marca+int64(n-1))

			buf, _, err := sess.Stop(ctxFundo, 1)
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			sess.mu.Lock()
			retido := sess.ring.Cursor() - int64(sess.ring.Cap())
			capacidade := sess.ring.Cap()
			sess.mu.Unlock()
			// O corte pedido começava no início da janela (cursor 0): a
			// marca no sob demanda, o começo do pre-roll no sempre pronto.
			j := buf.Janela
			if quero := duracaoDeAmostras(retido, SampleRate); j.PerdeuInicio != quero || quero < 190*time.Millisecond {
				t.Errorf("PerdeuInicio = %s, quero %s", j.PerdeuInicio, quero)
			}
			if len(buf.Samples) != capacidade {
				t.Errorf("len(buf.Samples) = %d, quero o ring inteiro, %d", len(buf.Samples), capacidade)
			}
			// A primeira amostra entregue é a que sobrou no ring, depois da
			// marca: nenhum pre-roll, e o início conta a partir dela.
			if quero := duracaoDeAmostras(retido-marca, SampleRate); j.InicioRelMarca != quero || j.PreRollEntregue != 0 {
				t.Errorf("InicioRelMarca = %s e PreRollEntregue = %s, quero %s e 0: a primeira amostra entregue é a que sobrou",
					j.InicioRelMarca, j.PreRollEntregue, quero)
			}
			semViolacoes(t, dev)
		})
	}
}

// --- relógio ----------------------------------------------------------------

func TestQpcPara100ns(t *testing.T) {
	// A frequência do QPC é 10 MHz no Windows 10+, mas já foi 3,579545 MHz
	// e há máquinas com outras. O contador passa de 10^14 com semanas de
	// máquina ligada: multiplicar antes de dividir estouraria.
	for _, c := range []struct{ contador, frequencia int64 }{
		{10_000_000, 10_000_000},
		{3, 3_579_545},
		{123_456_789_012_345, 3_579_545},
		{900_000_000_000_000, 24_000_000},
	} {
		want := new(big.Int).Div(new(big.Int).Mul(big.NewInt(c.contador), big.NewInt(10_000_000)), big.NewInt(c.frequencia))
		if got := qpcPara100ns(c.contador, c.frequencia); got != want.Int64() {
			t.Errorf("qpcPara100ns(%d, %d) = %d, quero %s", c.contador, c.frequencia, got, want)
		}
	}
	if got := qpcPara100ns(123, 0); got != 0 {
		t.Errorf("frequência zero deu %d, quero 0", got)
	}
}

// ditar faz um ditado completo — marca, áudio, Stop — e devolve o buffer.
func ditar(t *testing.T, sess *Session, dev *fakeDevice, id dictation.AttemptID, d time.Duration, amp float32) AudioBuffer {
	t.Helper()
	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	sess.MarkStart(id, time.Now())
	n := amostrasDe(d)
	dev.packets <- encher(n, amp)
	waitForCursor(t, sess, base+int64(n-1))
	buf, _, err := sess.Stop(ctxFundo, id)
	if err != nil {
		t.Fatalf("Stop %d: %v", id, err)
	}
	return buf
}
