package audio

// A recuperação do microfone (revisão adversarial de 29-set-2026, M1): uma
// leitura que falha com o microfone ligado não mata a sessão. Tudo contra o
// fakeDevice, o relógio falso e um temporizador de recuo que o teste
// controla — nada aqui abre microfone nem espera de verdade.

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// logSeguro é o destino do log da sessão: a goroutine de áudio escreve, o
// teste lê.
type logSeguro struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logSeguro) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logSeguro) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *logSeguro) contar(trecho string) int { return strings.Count(l.String(), trecho) }

// linha devolve a primeira linha do log com o trecho.
func (l *logSeguro) linha(t *testing.T, trecho string) string {
	t.Helper()
	for _, s := range strings.Split(l.String(), "\n") {
		if strings.Contains(s, trecho) {
			return s
		}
	}
	t.Fatalf("nenhuma linha com %q no log:\n%s", trecho, l.String())
	return ""
}

// sessaoComLog sobe uma sessão cujo log o teste lê.
func sessaoComLog(t *testing.T, dev *fakeDevice, cfg Config) (*Session, *logSeguro) {
	t.Helper()
	l := &logSeguro{}
	cfg.Log = slog.New(slog.NewTextHandler(l, nil))
	return startTestSession(t, dev, cfg), l
}

// recuosFalsos faz o papel do time.After do recuo: registra cada espera
// pedida e a vence na hora — ou nunca, com nunca ligado.
type recuosFalsos struct {
	mu      sync.Mutex
	pedidos []time.Duration
	nunca   bool
	// relogio, se não for nil, anda a espera inteira antes de vencê-la, como
	// o tempo passa de verdade em produção. Nil: o recuo vence sem o relógio
	// da sessão sair do lugar.
	relogio *relogioFalso
}

// maxRecuosNoTeste é o teto de recuos que o dublê vence. Passado dele, o
// recuo não vence mais: um mutante que nunca desiste para ali, e o teste
// falha pela lista de recuos, e não por timeout.
const maxRecuosNoTeste = 20

func (r *recuosFalsos) temporizador(d time.Duration) <-chan time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pedidos = append(r.pedidos, d)
	ch := make(chan time.Time, 1)
	if r.nunca || len(r.pedidos) > maxRecuosNoTeste {
		return ch
	}
	if r.relogio != nil {
		r.relogio.avancar(d)
	}
	ch <- time.Time{}
	return ch
}

func (r *recuosFalsos) lista() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.pedidos...)
}

// comRecuosFalsos troca o temporizador da sessão. Antes de qualquer falha:
// a goroutine de áudio só o lê depois de uma, e a falha passa pelo mutex
// do dispositivo falso.
func comRecuosFalsos(sess *Session, nunca bool) *recuosFalsos {
	r := &recuosFalsos{nunca: nunca}
	sess.temporizador = r.temporizador
	return r
}

// terminou diz se a goroutine de áudio saiu — a sessão morreu (ou fechou).
func terminou(sess *Session) bool {
	select {
	case <-sess.done:
		return true
	default:
		return false
	}
}

var errInvalidado = errors.New("fakeDevice: AUDCLNT_E_DEVICE_INVALIDATED")

const (
	linhaLeituraFalhou = "audio: a leitura do microfone falhou"
	linhaReabriu       = "audio: o microfone reabriu depois de uma falha"
	linhaNaoReligou    = "audio: o microfone não religou"
	linhaNaoLigou      = "audio: o microfone não ligou; o próximo ditado tenta de novo"
	linhaMorreu        = "audio: o microfone parou de responder"
	linhaParou         = "audio: o microfone parou de entregar áudio sem erro"
	linhaNaoComecou    = "audio: o microfone ligou e não entregou áudio nenhum"
)

// entregar faz o microfone falso entregar d de silêncio, com o relógio
// andando junto: é o microfone saudável por d. No sempre pronto, o relógio
// andar sem pacote nenhum é outra coisa — o microfone que nunca começou
// (VigiaDoPrimeiroPacote) ou que parou (VigiaDeParada).
func entregar(t *testing.T, sess *Session, dev *fakeDevice, d time.Duration) {
	t.Helper()
	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	n := amostrasDe(d)
	dev.packets <- encher(n, 0)
	waitForCursor(t, sess, base+int64(n)-1)
}

// recuosAteDesistir são as esperas de um incidente que nunca se resolve:
// a primeira religação é imediata, e a oitava falha desiste.
var recuosAteDesistir = []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 30 * time.Second}

// TestSemprePronto_LeituraQueFalhaEmRepousoReabreSozinha: suspender e
// retomar o notebook com o sempre pronto ligado. Antes, a sessão morria
// calada; agora o microfone reabre sozinho, sem esperar keydown, e o
// próximo ditado sai bem — sem nada de antes da falha.
func TestSemprePronto_LeituraQueFalhaEmRepousoReabreSozinha(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, dev, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, false)

	fala := amostrasDe(500 * time.Millisecond)
	dev.packets <- encher(fala, 0.9) // de antes da falha: não pode entrar em ditado nenhum
	waitForCursor(t, sess, int64(fala-1))

	dev.falharLeitura(errInvalidado)
	esperarAte(t, "o microfone reabrir sozinho", func() bool { return dev.contar("reabrir") == 1 && dev.ligado() })
	if err := sess.Falha(); err != nil {
		t.Fatalf("a sessão morreu numa leitura que falhou: %v", err)
	}

	// 100 ms da janela nova antes da marca viram pre-roll; a fala de antes
	// da falha, não — o pre-roll nunca atravessa a falha.
	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	silencio := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(silencio, 0)
	waitForCursor(t, sess, base+int64(silencio)-1)
	sess.MarkStart(1, time.Now())
	if !sess.MicrofoneEntregando(1) {
		t.Error("o microfone reaberto não entrega para o ditado novo: o bipe não sairia")
	}
	dev.packets <- encher(silencio, 0)
	waitForCursor(t, sess, base+int64(2*silencio)-1)

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop depois da reabertura: %v", err)
	}
	for i, s := range buf.Samples {
		if s != 0 {
			t.Fatalf("amostra %d = %d: áudio de antes da falha entrou no ditado", i, s)
		}
	}
	if j := buf.Janela; j.Modo != ModoSemprePronto || j.PreRollEntregue < 90*time.Millisecond || j.PreRollEntregue > 100*time.Millisecond {
		t.Errorf("janela = %+v, quero sempre pronto com o pre-roll só da janela nova (~100 ms)", j)
	}
	if r := recuos.lista(); len(r) != 0 {
		t.Errorf("a primeira religação esperou recuo: %v", r)
	}

	// Uma linha WARN da falha, só números, booleanos e o modo — nem o erro;
	// uma INFO da reabertura; nenhum ERROR.
	l := log.linha(t, linhaLeituraFalhou)
	for _, campo := range []string{"level=WARN", "modo=sempre_pronto", "tentativas_perdidas=0", "falhas_de_leitura=1", "reabre_sozinho=true", "falhas_seguidas=1", "recuo_ms=0"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha da falha não tem %q: %s", campo, l)
		}
	}
	if strings.Contains(l, "err=") || strings.Contains(l, "INVALIDATED") {
		t.Errorf("a linha da falha leva o erro; deveria ter só números, booleanos e o modo: %s", l)
	}
	if n := log.contar(linhaReabriu); n != 1 {
		t.Errorf("%d linhas de reabertura, quero 1:\n%s", n, log.String())
	}
	if log.contar("level=ERROR") != 0 {
		t.Errorf("ERROR numa falha de que o microfone se recuperou:\n%s", log.String())
	}
	semViolacoes(t, dev)
}

// TestSemprePronto_MicrofoneQueParaSemErroEReaberto: o Windows pode pausar o
// stream ao suspender sem invalidar o cliente, e aí nenhuma leitura falha —
// ela só volta vazia, para sempre. Passado o VigiaDeParada sem pacote, a
// sessão trata isso como uma leitura que falhou: solta e reabre. Antes, o
// sempre pronto ficava "ligado" e mudo, o bipe saía sem microfone e o
// pre-roll do ditado seguinte trazia a fala de antes da parada.
func TestSemprePronto_MicrofoneQueParaSemErroEReaberto(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, dev, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	fala := amostrasDe(500 * time.Millisecond)
	dev.packets <- encher(fala, 0.9) // de antes da parada
	waitForCursor(t, sess, int64(fala-1))

	// Um período antes do vigia, nada: consultas vazias são normais.
	dev.relogio.avancar(VigiaDeParada - periodoPadrao)
	for leu := dev.totalDeLeituras(); dev.totalDeLeituras() < leu+5; {
		time.Sleep(time.Millisecond)
	}
	if n := dev.contar("liberar"); n != 0 {
		t.Fatalf("o microfone foi solto %s depois do último pacote, antes do vigia", VigiaDeParada-periodoPadrao)
	}

	dev.relogio.avancar(periodoPadrao)
	esperarAte(t, "o microfone parado ser solto e reaberto", func() bool { return dev.contar("reabrir") == 1 && dev.ligado() })
	l := log.linha(t, linhaParou)
	for _, campo := range []string{"level=WARN", "modo=sempre_pronto", "sem_pacote_ms=500"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha do vigia não tem %q: %s", campo, l)
		}
	}
	if n := log.contar(linhaLeituraFalhou); n != 1 {
		t.Errorf("%d linhas de leitura que falhou, quero 1:\n%s", n, log.String())
	}

	// O ditado seguinte não traz a fala de antes da parada.
	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	sess.MarkStart(1, time.Now())
	silencio := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(silencio, 0)
	waitForCursor(t, sess, base+int64(silencio)-1)
	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop depois da reabertura: %v", err)
	}
	for i, s := range buf.Samples {
		if s != 0 {
			t.Fatalf("amostra %d = %d: a fala de antes da parada entrou no ditado", i, s)
		}
	}
	semViolacoes(t, dev)
}

// TestLeituraQueFalhaNoMeioDoDitado_EsseErraOSeguinteFunciona: a
// tentativa em curso falha com o erro do dispositivo — o áudio de depois da
// reabertura não é dela — e a seguinte funciona. No sob demanda, nada
// religa antes do próximo keydown: é o comportamento de sempre com o
// microfone estacionado.
func TestLeituraQueFalhaNoMeioDoDitado_EsseErraOSeguinteFunciona(t *testing.T) {
	for _, semprePronto := range []bool{false, true} {
		t.Run(nomeDoModo(semprePronto), func(t *testing.T) {
			dev := newFakeDevice(SampleRate, 1)
			sess, log := sessaoComLog(t, dev, Config{SemprePronto: semprePronto})
			comRecuosFalsos(sess, false)

			sess.MarkStart(1, time.Now())
			esperarMicrofone(t, dev, true)
			n := amostrasDe(200 * time.Millisecond)
			dev.packets <- encher(n, 0.9)
			waitForCursor(t, sess, int64(n-1))

			dev.falharLeitura(errInvalidado)
			esperarAte(t, "a falha soltar o microfone", func() bool { return dev.contar("liberar") == 1 })
			if semprePronto {
				// Reabre sozinho, e a janela nova segue gravando com a tecla
				// ainda segurada — nada disso pode ir para a tentativa 1.
				esperarAte(t, "o microfone reabrir", func() bool { return dev.contar("reabrir") == 1 && dev.ligado() })
				sess.mu.Lock()
				base := sess.ring.Cursor()
				sess.mu.Unlock()
				dev.packets <- encher(n, 0.5)
				waitForCursor(t, sess, base+int64(n)-1)
			} else {
				// A tecla ainda segurada de um ditado que falhou não religa.
				time.Sleep(20 * time.Millisecond)
				if dev.ligado() || dev.contar("reabrir") != 0 {
					t.Fatalf("no sob demanda o microfone religou sem keydown: %v", dev.historico())
				}
			}
			if sess.MicrofoneEntregando(1) {
				t.Error("o microfone 'entrega' para uma tentativa que perdeu a janela: o bipe mentiria")
			}

			buf, _, err := sess.Stop(ctxFundo, 1)
			if !errors.Is(err, ErrDeviceLost) {
				t.Fatalf("Stop do ditado da falha: err = %v, quero ErrDeviceLost", err)
			}
			if len(buf.Samples) != 0 || buf.Janela.Modo != nomeDoModo(semprePronto) {
				t.Errorf("devolveu %d amostras e a janela %+v; quero nenhuma amostra e a janela medida", len(buf.Samples), buf.Janela)
			}

			b2 := ditar(t, sess, dev, 2, 100*time.Millisecond, 0.5)
			if !semprePronto && !b2.Janela.Reabriu {
				t.Error("o keydown seguinte não reabriu o microfone solto")
			}
			if err := sess.Falha(); err != nil {
				t.Errorf("a sessão morreu: %v", err)
			}
			l := log.linha(t, linhaLeituraFalhou)
			if !strings.Contains(l, "tentativas_perdidas=1") || !strings.Contains(l, "modo="+nomeDoModo(semprePronto)) {
				t.Errorf("a linha da falha não diz o ditado perdido: %s", l)
			}
			semViolacoes(t, dev)
		})
	}
}

// TestLeituraQueFalha_AJanelaDoDitadoEAQueFalhou: a linha "audio: janela do
// microfone" de um ditado que perdeu a janela numa leitura mede ESSA janela
// — e não a que a falha zerou (no sob demanda, o primeiro pacote sumia) nem
// a que a reabertura abriu depois (no sempre pronto, com o "reabriu" e sem
// pacote nenhum dela).
func TestLeituraQueFalha_AJanelaDoDitadoEAQueFalhou(t *testing.T) {
	const latencia = 30 * time.Millisecond
	for _, semprePronto := range []bool{false, true} {
		t.Run(nomeDoModo(semprePronto), func(t *testing.T) {
			dev := newFakeDevice(SampleRate, 1)
			dev.latenciaLigar = latencia
			sess := startTestSession(t, dev, Config{SemprePronto: semprePronto})
			comRecuosFalsos(sess, false)

			sess.MarkStart(1, time.Now())
			esperarMicrofone(t, dev, true)
			n := amostrasDe(100 * time.Millisecond)
			dev.packets <- encher(n, 0.5)
			waitForCursor(t, sess, int64(n-1))
			dev.falharLeitura(errInvalidado)
			esperarAte(t, "a falha soltar o microfone", func() bool { return dev.contar("liberar") == 1 })
			if semprePronto {
				esperarAte(t, "o microfone reabrir", func() bool { return dev.contar("reabrir") == 1 && dev.ligado() })
			}

			buf, _, err := sess.Stop(ctxFundo, 1)
			if !errors.Is(err, ErrDeviceLost) {
				t.Fatalf("err = %v, quero ErrDeviceLost", err)
			}
			j := buf.Janela
			// O primeiro frame foi gravado latencia depois da marca e chegou
			// com o pacote inteiro, 100 ms depois.
			if j.InicioRelMarca != latencia || j.MarcaAtePrimeiroPacote != latencia+100*time.Millisecond {
				t.Errorf("inicio_rel_marca = %s, marca_ate_primeiro_pacote = %s; quero %s e %s, os da janela que falhou",
					j.InicioRelMarca, j.MarcaAtePrimeiroPacote, latencia, latencia+100*time.Millisecond)
			}
			if j.Reabriu {
				t.Error("Reabriu = true: a janela deste ditado não reabriu o endpoint; quem reabriu foi a seguinte")
			}
			semViolacoes(t, dev)
		})
	}
}

// TestSobDemanda_DitadoQueNaoLigouNaoHerdaAJanelaAnterior: a linha de um
// ditado cujo microfone não ligou não traz os números da janela de antes —
// que aqui ligou reabrindo o endpoint, e faria o log dizer que este também
// ligou assim.
func TestSobDemanda_DitadoQueNaoLigouNaoHerdaAJanelaAnterior(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.falhasAoLigar = 1
	sess, log := sessaoComLog(t, dev, Config{})
	if b := ditar(t, sess, dev, 1, 100*time.Millisecond, 0.5); !b.Janela.ReabriuPorFalha {
		t.Fatalf("janela 1 = %+v: o teste precisa de uma janela anterior que reabriu por falha", b.Janela)
	}
	esperarMicrofone(t, dev, false)

	dev.mu.Lock()
	dev.falhasAoLigar = 2
	dev.mu.Unlock()
	sess.MarkStart(2, time.Now())
	esperarAte(t, "o microfone não ligar", func() bool { return log.contar(linhaNaoLigou) == 1 })
	buf, _, err := sess.Stop(ctxFundo, 2)
	if !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("err = %v, quero ErrDeviceLost", err)
	}
	if j := buf.Janela; j.Reabriu || j.ReabriuPorFalha || j.Modo != ModoSobDemanda {
		t.Errorf("janela 2 = %+v: um microfone que não ligou não 'ligou reabrindo'", j)
	}
	semViolacoes(t, dev)
}

// TestLeituraQueFalhaDuranteAParada: a tentativa já está "parando" —
// esperando a última sílaba calar — quando a leitura falha. Ela falha
// também: o corte é fixado no mesmo lock que confere a falha.
func TestLeituraQueFalhaDuranteAParada(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})
	sess.MarkStart(1, time.Now())
	n := amostrasDe(400 * time.Millisecond)
	dev.packets <- encher(n, 0.9) // fala até o fim: a cauda vai esperar
	waitForCursor(t, sess, int64(n-1))

	falhou := false
	sess.dormir = func(time.Duration) {
		if !falhou {
			falhou = true
			dev.falharLeitura(errInvalidado)
			esperarAte(t, "a falha soltar o microfone", func() bool { return dev.contar("liberar") == 1 })
		}
	}
	buf, _, err := sess.Stop(ctxFundo, 1)
	if !falhou {
		t.Fatal("a cauda não esperou: o teste não provou nada")
	}
	if !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("err = %v, quero ErrDeviceLost: a leitura falhou antes do corte", err)
	}
	if len(buf.Samples) != 0 || buf.Janela.CaudaEsperou == 0 {
		t.Errorf("devolveu %d amostras, cauda esperou %s; quero nenhuma amostra e a espera medida", len(buf.Samples), buf.Janela.CaudaEsperou)
	}
	ditar(t, sess, dev, 2, 100*time.Millisecond, 0.5)
	semViolacoes(t, dev)
}

// TestLeituraQueFalhaEsperandoOAudioDoKeyup: a tecla subiu à frente do
// áudio, e o Stop espera o pacote do keyup (esperarOMicrofone) quando a
// leitura falha. O erro é o do dispositivo, na volta seguinte — e não "o
// microfone não abriu", depois do teto inteiro de 1 s esperando uma janela
// que já não é deste ditado.
func TestLeituraQueFalhaEsperandoOAudioDoKeyup(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})
	sess.MarkStart(1, time.Now())
	n := amostrasDe(100 * time.Millisecond)
	dev.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))
	dev.relogio.avancar(50 * time.Millisecond) // a tecla sobe 50 ms à frente do último pacote

	falhou := false
	sess.dormir = func(d time.Duration) {
		sess.dormiuNoTeste += d
		if !falhou {
			falhou = true
			dev.falharLeitura(errInvalidado)
			esperarAte(t, "a falha soltar o microfone", func() bool { return dev.contar("liberar") == 1 })
		}
	}
	buf, _, err := sess.Stop(ctxFundo, 1)
	if !falhou {
		t.Fatal("o Stop não esperou o microfone: o teste não provou nada")
	}
	if !errors.Is(err, ErrDeviceLost) || errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrDeviceLost", err)
	}
	if got := buf.Janela.EsperouMicrofone; got != PassoDoMicrofone {
		t.Errorf("esperou o microfone %s, quero um passo (%s): o erro sai na volta seguinte à falha", got, PassoDoMicrofone)
	}
	ditar(t, sess, dev, 2, 100*time.Millisecond, 0.5)
	semViolacoes(t, dev)
}

// TestSemprePronto_LigarComOMicrofoneForaAvisaQuandoReabre: o incidente
// começa por um Start que falha — ligar o sempre pronto com o microfone fora
// —, sem leitura nenhuma que falhe. A religação que dá certo fecha a falha
// no log, como a de uma leitura que falhou.
func TestSemprePronto_LigarComOMicrofoneForaAvisaQuandoReabre(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.falhasAoLigar = 2 // o Start e a reabertura; a religação seguinte liga
	sess, log := sessaoComLog(t, dev, Config{})
	recuos := comRecuosFalsos(sess, false)

	sess.DefinirSemprePronto(true)
	esperarAte(t, "o microfone religar", func() bool { return log.contar(linhaReabriu) == 1 })
	if !dev.ligado() {
		t.Errorf("o microfone não ficou ligado: %v", dev.historico())
	}
	if l := log.linha(t, linhaNaoReligou); !strings.Contains(l, "falhas_seguidas=1") || !strings.Contains(l, "recuo_ms=0") {
		t.Errorf("a linha do Start que falhou: %s", l)
	}
	if l := log.linha(t, linhaReabriu); !strings.Contains(l, "modo=sempre_pronto") || !strings.Contains(l, "falhas_seguidas=1") {
		t.Errorf("a linha da reabertura: %s", l)
	}
	if n := log.contar(linhaLeituraFalhou); n != 0 {
		t.Errorf("%d leituras que falharam: o teste devia começar o incidente pelo Start", n)
	}
	if r := recuos.lista(); len(r) != 0 {
		t.Errorf("a primeira religação do incidente esperou recuo: %v", r)
	}
	semViolacoes(t, dev)
}

// TestSemprePronto_ReligacaoQueFalhaRecuaEDesiste: o microfone não religa
// mais. Sem laço quente: 1, 2, 5, 10, 30 e 30 s entre as tentativas, e na
// oitava falha seguida a sessão morre — com ERROR, e o canal de frames
// fecha, que é o que o runtime vigia.
func TestSemprePronto_ReligacaoQueFalhaRecuaEDesiste(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, dev, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, false)
	dev.mu.Lock()
	dev.falhasAoLigar = 1000 // depois da subida, que liga normalmente
	dev.mu.Unlock()

	dev.falharLeitura(errInvalidado)
	esperarAte(t, "a sessão desistir", func() bool { return terminou(sess) })

	if err := sess.Falha(); !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("Falha() = %v, quero ErrDeviceLost", err)
	}
	if got := recuos.lista(); !slices.Equal(got, recuosAteDesistir) {
		t.Errorf("recuos = %v, quero %v", got, recuosAteDesistir)
	}
	// A falha de leitura, e depois sete religações que não vingaram — o
	// Start e a reabertura, em cada uma.
	if n := dev.contar("ligar falhou"); n != 2*(MaxFalhasSeguidas-1) {
		t.Errorf("%d Start que falharam, quero %d", n, 2*(MaxFalhasSeguidas-1))
	}
	if n := log.contar(linhaNaoReligou); n != MaxFalhasSeguidas-2 {
		t.Errorf("%d linhas de religação que falhou, quero %d:\n%s", n, MaxFalhasSeguidas-2, log.String())
	}
	if l := log.linha(t, linhaMorreu); !strings.Contains(l, "level=ERROR") || !strings.Contains(l, "falhas_seguidas=8") {
		t.Errorf("a linha da desistência: %s", l)
	}
	if n := log.contar("level=ERROR"); n != 1 {
		t.Errorf("%d linhas ERROR, quero 1:\n%s", n, log.String())
	}

	// Morta, um ditado novo falha na hora, e o canal de frames fechou.
	sess.MarkStart(1, time.Now())
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrDeviceLost) {
		t.Errorf("Stop numa sessão morta: err = %v, quero ErrDeviceLost", err)
	}
	if _, aberto := <-sess.Frames(); aberto {
		t.Error("o canal de frames seguiu aberto com a sessão morta")
	}
	semViolacoes(t, dev)
}

// TestSemprePronto_MicrofoneQueReligaENaoSeSustentaRecua: o Start dá certo,
// mas a primeira leitura depois de cada reabertura falha. Cada falha não
// pode ser "a primeira" — seria religar na hora, para sempre: um laço
// quente. É o mesmo incidente, e o recuo cresce igual.
func TestSemprePronto_MicrofoneQueReligaENaoSeSustentaRecua(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, dev, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, false)
	dev.mu.Lock()
	dev.leituraFalhaDepoisDeReabrir = true
	dev.mu.Unlock()

	dev.falharLeitura(errInvalidado)
	esperarAte(t, "a sessão desistir", func() bool { return terminou(sess) })

	if got := recuos.lista(); !slices.Equal(got, recuosAteDesistir) {
		t.Errorf("recuos = %v, quero %v", got, recuosAteDesistir)
	}
	if n := log.contar(linhaLeituraFalhou); n != MaxFalhasSeguidas {
		t.Errorf("%d leituras que falharam, quero %d", n, MaxFalhasSeguidas)
	}
	if n := log.contar("level=ERROR"); n != 1 {
		t.Errorf("%d linhas ERROR, quero 1:\n%s", n, log.String())
	}
	semViolacoes(t, dev)
}

// TestSemprePronto_AEsperaDoRecuoNaoESaude: aqui o relógio anda o recuo
// inteiro, como em produção — nos testes acima ele fica parado. Um recuo de
// 10 s não pode contar como os 10 s sem falhar (JanelaSaudavel) que encerram
// o incidente: se contasse, o microfone que nunca volta recomeçaria o ciclo
// 0, 1, 2, 5, 10 s para sempre, religando na hora a cada ~18 s, e o sempre
// pronto nunca desistiria. Vale para os dois jeitos de o incidente seguir.
func TestSemprePronto_AEsperaDoRecuoNaoESaude(t *testing.T) {
	for _, c := range []struct {
		nome     string
		preparar func(*fakeDevice)
	}{
		{"a religação não vinga", func(d *fakeDevice) { d.falhasAoLigar = 1000 }},
		{"religa e falha na primeira leitura", func(d *fakeDevice) { d.leituraFalhaDepoisDeReabrir = true }},
	} {
		t.Run(c.nome, func(t *testing.T) {
			dev := newFakeDevice(SampleRate, 1)
			sess := startTestSession(t, dev, Config{SemprePronto: true})
			recuos := comRecuosFalsos(sess, false)
			recuos.relogio = dev.relogio // antes da falha, como o temporizador
			dev.mu.Lock()
			c.preparar(dev)
			dev.mu.Unlock()
			inicio := dev.relogio.agora()

			dev.falharLeitura(errInvalidado)
			esperarAte(t, "a sessão desistir, ou os recuos passarem do teto", func() bool {
				return terminou(sess) || len(recuos.lista()) > maxRecuosNoTeste
			})
			if got := recuos.lista(); !slices.Equal(got, recuosAteDesistir) {
				t.Errorf("recuos = %v, quero %v", got, recuosAteDesistir)
			}
			if !terminou(sess) {
				t.Fatal("o sempre pronto nunca desistiu de um microfone que não volta")
			}
			// A desistência vem depois dos ~78 s de recuo, no relógio da sessão.
			if passou := duracaoDeTicks(dev.relogio.agora() - inicio); passou != 78*time.Second {
				t.Errorf("desistiu depois de %s no relógio da sessão, quero 78s", passou)
			}
			semViolacoes(t, dev)
		})
	}
}

// TestSemprePronto_FalhaDepoisDeJanelaSaudavelReligaNaHora: o incidente
// acaba quando o microfone fica JanelaSaudavel sem falhar. A falha
// seguinte é "a primeira" de novo: religa na hora, e o contador não
// acumula falhas de dias atrás até desistir.
func TestSemprePronto_FalhaDepoisDeJanelaSaudavelReligaNaHora(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, dev, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, false)

	falhar := func(reaberturas int) {
		t.Helper()
		dev.falharLeitura(errInvalidado)
		esperarAte(t, "o microfone reabrir", func() bool { return dev.contar("reabrir") == reaberturas && dev.ligado() })
	}
	falhar(1) // incidente novo: religa na hora
	falhar(2) // logo depois: mesmo incidente, 1 s de recuo
	if got := recuos.lista(); !slices.Equal(got, []time.Duration{time.Second}) {
		t.Fatalf("recuos = %v, quero [1s]", got)
	}

	// O microfone fica JanelaSaudavel sem falhar, contando do fim do recuo.
	entregar(t, sess, dev, time.Second+JanelaSaudavel)
	falhar(3)
	if got := recuos.lista(); !slices.Equal(got, []time.Duration{time.Second}) {
		t.Errorf("recuos = %v: a falha depois de uma janela saudável esperou recuo", got)
	}
	primeiras := 0
	for _, l := range strings.Split(log.String(), "\n") {
		if strings.Contains(l, linhaLeituraFalhou) && strings.Contains(l, "falhas_seguidas=1 ") {
			primeiras++
		}
	}
	if primeiras != 2 {
		t.Errorf("%d falhas marcadas como a primeira do incidente, quero 2:\n%s", primeiras, log.String())
	}
	semViolacoes(t, dev)
}

// TestSemprePronto_RecuoNaoTravaAOpcaoNemOClose: o recuo é uma espera de
// até 30 s. Desligar a opção tira a goroutine dele na hora — um keydown no
// sob demanda religa sem esperar —, e o Close não espera o teto.
func TestSemprePronto_RecuoNaoTravaAOpcaoNemOClose(t *testing.T) {
	entrarNoRecuo := func(t *testing.T) (*Session, *fakeDevice) {
		t.Helper()
		dev := newFakeDevice(SampleRate, 1)
		sess := startTestSession(t, dev, Config{SemprePronto: true})
		recuos := comRecuosFalsos(sess, true) // o recuo nunca vence sozinho
		dev.mu.Lock()
		dev.falhasAoLigar = 2 // a religação imediata falha: 1 s de recuo
		dev.mu.Unlock()
		dev.falharLeitura(errInvalidado)
		esperarAte(t, "o recuo começar", func() bool { return len(recuos.lista()) == 1 })
		return sess, dev
	}

	t.Run("desligar a opção", func(t *testing.T) {
		sess, dev := entrarNoRecuo(t)
		voltou := make(chan struct{})
		go func() {
			sess.DefinirSemprePronto(false)
			close(voltou)
		}()
		select {
		case <-voltou:
		case <-time.After(2 * time.Second):
			t.Fatal("DefinirSemprePronto travou no recuo")
		}
		// Já no sob demanda: o keydown religa na hora, sem esperar o recuo.
		sess.MarkStart(1, time.Now())
		esperarMicrofone(t, dev, true)
		sess.Abort(1)
		esperarMicrofone(t, dev, false)
		if err := sess.Falha(); err != nil {
			t.Errorf("a sessão morreu: %v", err)
		}
		semViolacoes(t, dev)
	})

	t.Run("Close", func(t *testing.T) {
		sess, _ := entrarNoRecuo(t)
		sess.esperaAoFechar = 500 * time.Millisecond
		fechou := make(chan error, 1)
		go func() { fechou <- sess.Close() }()
		select {
		case err := <-fechou:
			if err != nil {
				t.Errorf("Close = %v: a goroutine de áudio ficou presa no recuo até o teto", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Close travou no recuo")
		}
	})
}
