package audio

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// O vigia de parada (VigiaDeParada) na segunda revisão de 29-set-2026: ele
// derrubava o ditado inteiro por um soluço que antes só cortava um pedaço,
// disparava com o microfone perfeito quando a goroutine de áudio acordava
// atrasada, e fazia o sempre pronto desistir de um microfone que só tinha
// soluços. Os cenários vieram do revisor, provados numa cópia.

// devAtrasado embrulha o fakeDevice e faz a goroutine de áudio "acordar
// atrasada" depois de uma consulta vazia — o sono de um período dentro do
// wasapiDevice.Read, com a CPU saturada: a consulta viu 0 frames, a
// goroutine volta atraso depois, e o motor seguiu capturando — o pacote
// acumulado já está no buffer na consulta seguinte.
type devAtrasado struct {
	*fakeDevice
	mu        sync.Mutex
	atraso    time.Duration
	acumulado []float32
	aplicados int
}

func (d *devAtrasado) atrasarProximaVazia(a time.Duration, acumulado []float32) {
	d.mu.Lock()
	d.atraso, d.acumulado = a, acumulado
	d.mu.Unlock()
}

func (d *devAtrasado) nAplicados() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.aplicados
}

func (d *devAtrasado) Read(ctx context.Context) (pacote, error) {
	p, err := d.fakeDevice.Read(ctx)
	if err == nil && len(p.amostras) == 0 {
		d.mu.Lock()
		a, acc := d.atraso, d.acumulado
		d.atraso, d.acumulado = 0, nil
		if a > 0 {
			d.aplicados++
		}
		d.mu.Unlock()
		if a > 0 {
			d.fakeDevice.relogio.avancar(a)
			if acc != nil {
				d.fakeDevice.packets <- acc
			}
		}
	}
	return p, err
}

// sessaoSobre sobe uma sessão sobre um dispositivo que embrulha o
// fakeDevice, com o log à vista.
func sessaoSobre(t *testing.T, dev device, relogio *relogioFalso, cfg Config) (*Session, *logSeguro) {
	t.Helper()
	return sessaoComRelogio(t, dev, relogio.agora, cfg)
}

// sessaoComRelogio é sessaoSobre com o relógio da sessão dado pelo teste.
func sessaoComRelogio(t *testing.T, dev device, agora func() int64, cfg Config) (*Session, *logSeguro) {
	t.Helper()
	l := &logSeguro{}
	cfg.Log = slog.New(slog.NewTextHandler(l, nil))
	sess, err := startSession(context.Background(), cfg, func(context.Context) (device, error) { return dev, nil }, agora)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	sess.dormir = func(d time.Duration) { sess.dormiuNoTeste += d }
	return sess, l
}

// TestVigia_GoroutineAtrasadaNaoDerrubaODitado: dispositivo saudável, só a
// goroutine de áudio acordou 600 ms atrasada depois de uma consulta vazia.
// O relógio da consulta é lido antes do Read, e o ditado sai inteiro.
func TestVigia_GoroutineAtrasadaNaoDerrubaODitado(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	dw := &devAtrasado{fakeDevice: fd}
	sess, log := sessaoSobre(t, dw, fd.relogio, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true)
	n := amostrasDe(300 * time.Millisecond)
	fd.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))

	acc := amostrasDe(600 * time.Millisecond)
	dw.atrasarProximaVazia(600*time.Millisecond, encher(acc, 0.5))
	esperarAte(t, "a consulta atrasada", func() bool { return dw.nAplicados() == 1 })
	waitForCursor(t, sess, int64(n+acc-1))

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("ditado perdido com o dispositivo saudável: %v\n%s", err, log.String())
	}
	if len(buf.Samples) < n+acc-2 {
		t.Fatalf("amostras = %d, quero ~%d", len(buf.Samples), n+acc)
	}
	if fd.contar("liberar") != 0 || log.contar(linhaParou) != 0 {
		t.Fatalf("o vigia disparou com o microfone perfeito:\n%s", log.String())
	}
}

// TestVigia_SolucoNoDitadoCortaSoOPedaco: sob demanda, a frase inteira já
// está no ring e o microfone fica 600 ms sem entregar antes do keyup. O
// Stop devolve a frase, com FaltouNoFim dizendo o que faltou — o ditado não
// vira ErrDeviceLost por um soluço.
func TestVigia_SolucoNoDitadoCortaSoOPedaco(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true)
	n := amostrasDe(1 * time.Second)
	fd.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))

	fd.relogio.avancar(600 * time.Millisecond)
	// Um vigia que soltasse o microfone pararia as leituras: a condição
	// de saída olha as duas coisas, para o teste falhar em vez de travar.
	for leu := fd.totalDeLeituras(); fd.totalDeLeituras() < leu+5 && fd.contar("liberar") == 0; {
		time.Sleep(time.Millisecond)
	}
	if fd.contar("liberar") != 0 {
		t.Fatalf("o vigia soltou o microfone com um ditado vivo:\n%s", log.String())
	}

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("a frase inteira estava no ring e o ditado virou erro: %v\n%s", err, log.String())
	}
	if len(buf.Samples) < n-2 {
		t.Fatalf("amostras = %d, quero a frase inteira (~%d)", len(buf.Samples), n)
	}
	if buf.Janela.FaltouNoFim <= 0 {
		t.Errorf("FaltouNoFim = %s, quero o soluço registrado", buf.Janela.FaltouNoFim)
	}
	// Faltou VigiaDeParada ou mais: o ESTACIONAMENTO solta o cliente (parar
	// e liberar, sem Reset), e o próximo keydown reabre — a recuperação do
	// sob demanda. Não o vigia: ele não age no sob demanda.
	esperarMicrofone(t, fd, false)
	esperarAte(t, "o cliente solto no estacionamento", func() bool { return fd.contar("liberar") == 1 })
	if h := fd.historico(); len(h) < 2 || h[len(h)-2] != "parar" || h[len(h)-1] != "liberar" || fd.contar("zerar") != 0 {
		t.Fatalf("histórico = %v, quero o estacionamento com a política liberar (parar, liberar)", h)
	}
	if log.contar(linhaParou) != 0 {
		t.Fatalf("o vigia agiu no sob demanda:\n%s", log.String())
	}
}

// TestSemprePronto_IncidenteNovoZeraACota: falhas reais de incidentes
// diferentes, separados por mais que JanelaSaudavel, não se somam para a
// desistência.
func TestSemprePronto_IncidenteNovoZeraACota(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	falhar := func(total int) {
		t.Helper()
		esperarAte(t, "ligado", func() bool { return fd.ligado() || terminou(sess) })
		fd.falharLeitura(errInvalidado)
		esperarAte(t, "a falha ser tratada", func() bool { return fd.contar("liberar") == total || terminou(sess) })
	}
	for i := 1; i < MaxFalhasSeguidas; i++ {
		falhar(i)
	}
	// Saudável bem mais que JanelaSaudavel depois do último recuo (30 s).
	esperarAte(t, "religar", func() bool { return fd.ligado() || terminou(sess) })
	entregar(t, sess, fd, time.Minute)
	for i := 1; i < MaxFalhasSeguidas; i++ {
		falhar(MaxFalhasSeguidas - 1 + i)
	}
	esperarAte(t, "religar depois do segundo incidente", func() bool { return fd.ligado() || terminou(sess) })
	if terminou(sess) || log.contar(linhaMorreu) != 0 {
		t.Fatalf("dois incidentes separados somaram a cota da desistência:\n%s", log.String())
	}
}

// TestVigia_SemprePronto_SolucoNoDitadoNaoDerruba: no sempre pronto, onde o
// vigia vale, um ditado vivo o cala — o soluço corta só o pedaço, e o Stop
// devolve a frase.
func TestVigia_SemprePronto_SolucoNoDitadoNaoDerruba(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	sess.MarkStart(1, time.Now())
	n := amostrasDe(1 * time.Second)
	fd.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))
	fd.relogio.avancar(600 * time.Millisecond)
	for leu := fd.totalDeLeituras(); fd.totalDeLeituras() < leu+5 && fd.contar("liberar") == 0; {
		time.Sleep(time.Millisecond)
	}
	if fd.contar("liberar") != 0 {
		t.Fatalf("o vigia soltou o microfone com um ditado vivo no sempre pronto:\n%s", log.String())
	}
	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("a frase estava no ring e o ditado virou erro: %v\n%s", err, log.String())
	}
	if len(buf.Samples) < n-2 {
		t.Fatalf("amostras = %d, quero a frase inteira (~%d)", len(buf.Samples), n)
	}
}

// TestSobDemanda_FaltaPequenaNoFimNaoSoltaOCliente: abaixo de VigiaDeParada
// (um carimbo atrasado, um soluço curto), o estacionamento é o de sempre —
// parar e zerar —, sem cobrar a reabertura do próximo ditado.
func TestSobDemanda_FaltaPequenaNoFimNaoSoltaOCliente(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true)
	n := amostrasDe(1 * time.Second)
	fd.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))
	fd.relogio.avancar(200 * time.Millisecond)

	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("Stop: %v\n%s", err, log.String())
	}
	if buf.Janela.FaltouNoFim <= 0 || buf.Janela.FaltouNoFim >= VigiaDeParada {
		t.Fatalf("FaltouNoFim = %s, quero entre 0 e %s para este cenário", buf.Janela.FaltouNoFim, VigiaDeParada)
	}
	esperarMicrofone(t, fd, false)
	esperarAte(t, "o Reset do estacionamento", func() bool { return fd.contar("zerar") == 1 })
	if fd.contar("liberar") != 0 {
		t.Fatalf("uma falta de %s soltou o cliente: %v", buf.Janela.FaltouNoFim, fd.historico())
	}
}

// TestSobDemanda_MicrofoneLentoNaoESolto: um microfone que entrega, mas só
// depois de a tecla subir, falha aquele ditado curto (ErrMicrofoneNaoAbriu)
// e NÃO perde o cliente: soltá-lo cobraria a reabertura inteira de cada
// ditado curto, e ele ficaria ainda mais lento.
func TestSobDemanda_MicrofoneLentoNaoESolto(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	fd.latenciaLigar = 400 * time.Millisecond
	sess, log := sessaoComLog(t, fd, Config{})
	// O primeiro pacote chega durante a espera do Stop, carimbado depois
	// do keyup.
	sess.dormir = func(d time.Duration) {
		sess.dormiuNoTeste += d
		sess.mu.Lock()
		vazio := sess.ring.Cursor() == 0
		sess.mu.Unlock()
		if vazio {
			fd.packets <- encher(amostrasDe(100*time.Millisecond), 0.5)
			for i := 0; i < 2000; i++ {
				sess.mu.Lock()
				chegou := sess.ring.Cursor() > 0
				sess.mu.Unlock()
				if chegou {
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
	}
	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true)
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu (a captura começou depois do keyup)\n%s", err, log.String())
	}
	esperarMicrofone(t, fd, false)
	esperarAte(t, "o Reset do estacionamento", func() bool { return fd.contar("zerar") == 1 })
	if fd.contar("liberar") != 0 {
		t.Fatalf("um microfone que entregou (só tarde) teve o cliente solto: %v", fd.historico())
	}
}

// TestSobDemanda_LeituraQueFalhaNaoDeixaMarcaVelha: a leitura que falha no
// meio do ditado já solta o cliente; o Stop dela (ErrDeviceLost) não deixa
// marca para soltar de novo o cliente saudável do ditado seguinte.
func TestSobDemanda_LeituraQueFalhaNaoDeixaMarcaVelha(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true)
	n := amostrasDe(200 * time.Millisecond)
	fd.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "a leitura que falhou soltar o cliente", func() bool { return fd.contar("liberar") == 1 })
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("err = %v, quero ErrDeviceLost", err)
	}

	ditar(t, sess, fd, 2, 100*time.Millisecond, 0.5)
	esperarMicrofone(t, fd, false)
	esperarAte(t, "o Reset do ditado que voltou", func() bool { return fd.contar("zerar") >= 1 })
	if fd.contar("liberar") != 1 {
		t.Fatalf("o cliente saudável do ditado seguinte foi solto de novo: %v\n%s", fd.historico(), log.String())
	}
}

// TestSemprePronto_MarcaVelhaNaoSoltaClienteSaudavel: o microfone para no
// meio de um ditado do sempre pronto (a marca liga), o vigia solta e reabre
// no repouso — e a marca não sobra para soltar, horas depois, o cliente
// saudável ao desligar o sempre pronto.
func TestSemprePronto_MarcaVelhaNaoSoltaClienteSaudavel(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	sess.MarkStart(1, time.Now())
	n := amostrasDe(300 * time.Millisecond)
	fd.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))
	fd.relogio.avancar(700 * time.Millisecond)
	if _, _, err := sess.Stop(ctxFundo, 1); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	esperarAte(t, "o vigia soltar no repouso", func() bool { return fd.contar("liberar") == 1 })
	esperarAte(t, "religar", func() bool { return fd.ligado() })
	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	k := amostrasDe(50 * time.Millisecond)
	fd.packets <- encher(k, 0.1)
	waitForCursor(t, sess, base+int64(k)-1)

	sess.DefinirSemprePronto(false)
	esperarMicrofone(t, fd, false)
	esperarAte(t, "o Reset de desligar o sempre pronto", func() bool { return fd.contar("zerar") >= 1 })
	if fd.contar("liberar") != 1 {
		t.Fatalf("desligar o sempre pronto soltou um cliente saudável: %v\n%s", fd.historico(), log.String())
	}
}

// TestSemprePronto_ParadasNaoGastamACotaDaDesistencia: depois de
// MaxFalhasSeguidas paradas sem erro no mesmo incidente, UMA leitura com
// erro (o USB saiu e voltou) não mata a sessão: a cota da desistência é das
// falhas reais.
func TestSemprePronto_ParadasNaoGastamACotaDaDesistencia(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	for i := 1; i <= MaxFalhasSeguidas; i++ {
		esperarAte(t, "ligado", func() bool { return fd.ligado() })
		sess.mu.Lock()
		base := sess.ring.Cursor()
		sess.mu.Unlock()
		k := amostrasDe(50 * time.Millisecond)
		fd.packets <- encher(k, 0.1)
		waitForCursor(t, sess, base+int64(k)-1)
		fd.relogio.avancar(VigiaDeParada)
		esperarAte(t, "o vigia soltar", func() bool { return fd.contar("liberar") == i })
	}
	esperarAte(t, "ligado", func() bool { return fd.ligado() })
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "a falha real ser tratada", func() bool { return fd.contar("liberar") == MaxFalhasSeguidas+1 || terminou(sess) })
	esperarAte(t, "religar depois da falha real", func() bool { return fd.ligado() || terminou(sess) })
	if terminou(sess) || log.contar(linhaMorreu) != 0 {
		t.Fatalf("uma leitura com erro matou o sempre pronto porque as paradas gastaram a cota:\n%s", log.String())
	}
}

// TestVigia_TentativaQueFalhouNaoCalaOVigia: uma tentativa que já falhou (a
// leitura falhou no meio dela) não é "ditado em curso" para o vigia. A
// janela reaberta que parar antes do Stop dessa tentativa é solta.
func TestVigia_TentativaQueFalhouNaoCalaOVigia(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	sess.MarkStart(1, time.Now())
	n := amostrasDe(100 * time.Millisecond)
	fd.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "a leitura que falhou soltar o cliente", func() bool { return fd.contar("liberar") == 1 })
	esperarAte(t, "religar", func() bool { return fd.ligado() })

	sess.mu.Lock()
	base := sess.ring.Cursor()
	sess.mu.Unlock()
	k := amostrasDe(50 * time.Millisecond)
	fd.packets <- encher(k, 0.1)
	waitForCursor(t, sess, base+int64(k)-1)
	fd.relogio.avancar(VigiaDeParada)
	esperarAte(t, "o vigia soltar a janela reaberta", func() bool { return fd.contar("liberar") == 2 })

	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("err = %v, quero ErrDeviceLost para a tentativa que falhou\n%s", err, log.String())
	}
}

// devQueNaoEntrega liga (Start ok) e nunca entrega — o cliente parado que
// volta da suspensão sem ser invalidado — até ser solto: reabrir o endpoint
// cura.
type devQueNaoEntrega struct {
	*fakeDevice
	mu     sync.Mutex
	parado bool
}

func (d *devQueNaoEntrega) Read(ctx context.Context) (pacote, error) {
	d.mu.Lock()
	parado := d.parado
	d.mu.Unlock()
	if parado {
		d.fakeDevice.mu.Lock()
		d.fakeDevice.leituras++
		d.fakeDevice.mu.Unlock()
		time.Sleep(time.Millisecond)
		return pacote{}, nil
	}
	return d.fakeDevice.Read(ctx)
}

func (d *devQueNaoEntrega) Liberar() {
	d.mu.Lock()
	d.parado = false
	d.mu.Unlock()
	d.fakeDevice.Liberar()
}

// TestSobDemanda_ClienteQueLigaENaoEntregaESoltoEReaberto: um ditado que não
// recebeu áudio (ErrMicrofoneNaoAbriu) solta o cliente no estacionamento, e
// o ditado seguinte reabre o microfone e funciona. Antes, a PoliticaParar
// reaproveitava o mesmo cliente para sempre.
func TestSobDemanda_ClienteQueLigaENaoEntregaESoltoEReaberto(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	dz := &devQueNaoEntrega{fakeDevice: fd, parado: true}
	sess, log := sessaoSobre(t, dz, fd.relogio, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true)
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}
	esperarMicrofone(t, fd, false)
	esperarAte(t, "o cliente solto no estacionamento", func() bool { return fd.contar("liberar") == 1 })

	buf := ditar(t, sess, fd, 2, 100*time.Millisecond, 0.5)
	if len(buf.Samples) == 0 {
		t.Fatalf("o ditado depois da reabertura veio vazio:\n%s", log.String())
	}
	semViolacoes(t, fd)
}

// TestVigia_AtrasosDaGoroutineNaoMatamOSemprePronto: no repouso do sempre
// pronto, a goroutine acordando atrasada várias vezes, com o microfone
// perfeito, não é parada — nem uma, nem MaxFalhasSeguidas delas.
func TestVigia_AtrasosDaGoroutineNaoMatamOSemprePronto(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	dw := &devAtrasado{fakeDevice: fd}
	sess, log := sessaoSobre(t, dw, fd.relogio, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	for i := 1; i <= MaxFalhasSeguidas; i++ {
		sess.mu.Lock()
		base := sess.ring.Cursor()
		sess.mu.Unlock()
		k := amostrasDe(50 * time.Millisecond)
		fd.packets <- encher(k, 0.1)
		waitForCursor(t, sess, base+int64(k)-1)
		acc := amostrasDe(600 * time.Millisecond)
		dw.atrasarProximaVazia(600*time.Millisecond, encher(acc, 0.1))
		esperarAte(t, "a consulta atrasada", func() bool { return dw.nAplicados() == i })
		waitForCursor(t, sess, base+int64(k+acc)-1)
	}
	if terminou(sess) || fd.contar("liberar") != 0 || log.contar(linhaParou) != 0 {
		t.Fatalf("atrasos da goroutine contaram como parada (liberar=%d, morreu=%v):\n%s", fd.contar("liberar"), terminou(sess), log.String())
	}
}

// TestVigia_ParadaRepetidaSoRecuaNuncaDesiste: um microfone de verdade que
// para sem erro e volta a cada reabertura, mais de MaxFalhasSeguidas vezes
// no mesmo incidente, só recua: o sempre pronto não troca "funciona com
// soluços" por "mudo até reiniciar o Agent".
func TestVigia_ParadaRepetidaSoRecuaNuncaDesiste(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, false)

	for i := 1; i <= MaxFalhasSeguidas+2; i++ {
		esperarAte(t, "ligado", func() bool { return fd.ligado() || terminou(sess) })
		if terminou(sess) {
			t.Fatalf("o sempre pronto desistiu na parada %d:\n%s", i, log.String())
		}
		sess.mu.Lock()
		base := sess.ring.Cursor()
		sess.mu.Unlock()
		k := amostrasDe(50 * time.Millisecond)
		fd.packets <- encher(k, 0.1)
		waitForCursor(t, sess, base+int64(k)-1)
		fd.relogio.avancar(VigiaDeParada)
		esperarAte(t, "o vigia soltar", func() bool { return fd.contar("liberar") == i || terminou(sess) })
	}
	esperarAte(t, "religar depois da última parada", func() bool { return fd.ligado() || terminou(sess) })
	if terminou(sess) || log.contar(linhaMorreu) != 0 {
		t.Fatalf("o sempre pronto morreu por paradas sem erro:\n%s", log.String())
	}
	// Recuou de verdade, e chegou ao teto: é o mesmo incidente.
	if p := recuos.lista(); len(p) == 0 || p[len(p)-1] != 30*time.Second {
		t.Errorf("recuos = %v, quero crescendo até 30 s", p)
	}
}

// A terceira rodada da revisão de 29-set-2026 tirou as duas limitações
// conhecidas do sempre pronto: o keydown no meio do recuo, que não ligava o
// microfone, e a janela que liga e nunca entrega o primeiro pacote, que não
// se recuperava sozinha. Os dois cenários vieram do revisor (R4 e R1),
// provados numa cópia.

// sessaoLigada diz se a sessão já se sabe ligada — com a janela, e o instante
// do Start, gravados. fakeDevice.ligado vira antes disso, dentro do Ligar.
func sessaoLigada(sess *Session) bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.ligado
}

// algumasLeituras espera a goroutine de áudio fazer mais algumas consultas —
// ou soltar o microfone, que para as consultas: o teste falha em vez de
// travar.
func algumasLeituras(fd *fakeDevice) {
	leu, soltou := fd.totalDeLeituras(), fd.contar("liberar")
	for fim := time.Now().Add(2 * time.Second); time.Now().Before(fim); time.Sleep(time.Millisecond) {
		if fd.totalDeLeituras() >= leu+5 || fd.contar("liberar") != soltou {
			return
		}
	}
}

// TestSemprePronto_KeydownNoRecuoLigaNaHora: depois de duas paradas
// seguidas, o sempre pronto está num recuo de 1 s quando a tecla desce. O
// keydown corta o recuo, o microfone liga na hora, e o ditado sai — antes,
// ele falhava com "nenhum pacote em 1 s", e no teto do recuo (30 s) falhava
// todo ditado no meio dele. A religação do keydown não deixa rastro no
// incidente: a do recuo seguinte ainda conta.
func TestSemprePronto_KeydownNoRecuoLigaNaHora(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, false)
	segurarRecuos := func(v bool) {
		recuos.mu.Lock()
		recuos.nunca = v
		recuos.mu.Unlock()
	}

	for i := 1; i <= 2; i++ {
		esperarAte(t, "ligado", func() bool { return sessaoLigada(sess) })
		entregar(t, sess, fd, 50*time.Millisecond)
		if i == 2 {
			segurarRecuos(true) // o recuo de 1 s da segunda parada não vence sozinho
		}
		fd.relogio.avancar(VigiaDeParada)
		esperarAte(t, "o vigia soltar", func() bool { return fd.contar("liberar") == i })
	}
	esperarAte(t, "o recuo começar", func() bool { return len(recuos.lista()) == 1 })
	if fd.ligado() {
		t.Fatal("o microfone religou no meio do recuo: o teste não provaria nada")
	}

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true) // antes, só quando o recuo vencesse
	entregar(t, sess, fd, 200*time.Millisecond)
	buf, _, err := sess.Stop(ctxFundo, 1)
	if err != nil {
		t.Fatalf("o ditado no meio do recuo falhou: %v\n%s", err, log.String())
	}
	if j := buf.Janela; len(buf.Samples) < amostrasDe(200*time.Millisecond)-2 || j.JaLigado || !j.Reabriu {
		t.Errorf("amostras = %d, janela = %+v; quero os 200 ms numa janela que o keydown reabriu", len(buf.Samples), j)
	}
	// A primeira parada religou na hora; a segunda, pelo keydown.
	if n := log.contar(linhaReabriu); n != 2 {
		t.Errorf("%d linhas de reabertura, quero 2:\n%s", n, log.String())
	}

	// O microfone cai de novo, no mesmo incidente, e a religação do recuo não
	// vinga: ela conta, como se o keydown não tivesse existido.
	fd.mu.Lock()
	fd.falhasAoLigar = 2 // o Start e a reabertura
	fd.mu.Unlock()
	segurarRecuos(false)
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "a religação do recuo não vingar", func() bool { return log.contar(linhaNaoReligou) == 1 })
	if l := log.linha(t, linhaNaoReligou); !strings.Contains(l, "falhas_seguidas=4") {
		t.Errorf("a religação do recuo depois do keydown não contou no incidente: %s", l)
	}
	esperarAte(t, "religar", func() bool { return fd.ligado() })
	if n := log.contar(linhaNaoLigou); n != 0 {
		t.Errorf("%d religações tratadas como de keydown, sem keydown nenhum:\n%s", n, log.String())
	}
	semViolacoes(t, fd)
}

// recuoSegurado segura todo recuo até o teste soltar; daí em diante, cada
// recuo vence na hora. Registra as esperas pedidas.
type recuoSegurado struct {
	mu       sync.Mutex
	pedidos  []time.Duration
	pendente chan time.Time
	solto    bool
}

func (r *recuoSegurado) temporizador(d time.Duration) <-chan time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pedidos = append(r.pedidos, d)
	ch := make(chan time.Time, 1)
	switch {
	case !r.solto:
		r.pendente = ch
	case len(r.pedidos) <= 100: // um mutante que nunca desiste para aqui
		ch <- time.Time{}
	}
	return ch
}

// soltar vence o recuo em espera e todos os seguintes.
func (r *recuoSegurado) soltar() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.solto = true
	if r.pendente != nil {
		r.pendente <- time.Time{}
	}
}

func (r *recuoSegurado) lista() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.pedidos...)
}

// TestSemprePronto_KeydownsNumApagaoNaoGastamACota: o microfone sumiu de
// vez, e o sempre pronto está no recuo. Cada keydown tenta ligar na hora e
// não consegue: perde só aquele ditado, na hora e com o erro do dispositivo,
// e não conta para a desistência nem mexe no recuo. Mais keydowns que
// MaxFalhasSeguidas não matam a sessão; e, quando o recuo volta a correr, ela
// desiste na oitava falha do recuo, como se não houvesse keydown nenhum.
func TestSemprePronto_KeydownsNumApagaoNaoGastamACota(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	recuos := &recuoSegurado{}
	sess.temporizador = recuos.temporizador // antes de qualquer falha
	fd.mu.Lock()
	fd.falhasAoLigar = 1000 // depois da subida, que liga normalmente
	fd.mu.Unlock()

	// A leitura falha, a religação imediata não vinga: 1 s de recuo, segurado.
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "o recuo começar", func() bool { return len(recuos.lista()) == 1 })
	incidente := func() [4]int64 {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return [4]int64{sess.recuoAte, int64(sess.falhasSeguidas), int64(sess.falhasReais), sess.recuoCortado}
	}
	antes := incidente()

	const keydowns = MaxFalhasSeguidas + 2
	for i := 1; i <= keydowns; i++ {
		id := dictation.AttemptID(i)
		sess.MarkStart(id, time.Now())
		esperarAte(t, "o keydown tentar ligar", func() bool { return log.contar(linhaNaoLigou) == i || terminou(sess) })
		if terminou(sess) {
			t.Fatalf("a sessão morreu no keydown %d:\n%s", i, log.String())
		}
		if sess.MicrofoneEntregando(id) {
			t.Errorf("o microfone 'entrega' para o ditado %d, cujo Start falhou: o bipe mentiria", i)
		}
		buf, _, err := sess.Stop(ctxFundo, id)
		if !errors.Is(err, ErrDeviceLost) || errors.Is(err, ErrMicrofoneNaoAbriu) {
			t.Fatalf("Stop %d: err = %v, quero ErrDeviceLost", i, err)
		}
		if buf.Janela.EsperouMicrofone != 0 {
			t.Errorf("Stop %d esperou o microfone %s: o Start já tinha falhado, o erro sai na hora", i, buf.Janela.EsperouMicrofone)
		}
	}
	// recuoAte, falhasSeguidas, falhasReais e recuoCortado: os mesmos de antes.
	if depois := incidente(); depois != antes {
		t.Fatalf("o incidente depois dos keydowns = %v, quero %v: o keydown mexeu no recuo ou na cota", depois, antes)
	}
	l := log.linha(t, linhaNaoLigou)
	for _, campo := range []string{"level=WARN", "modo=sempre_pronto", "tentativas_perdidas=1", "recuo_restante_ms=1000"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha do keydown que não ligou não tem %q: %s", campo, l)
		}
	}

	// O recuo volta a correr de onde estava.
	recuos.soltar()
	esperarAte(t, "a sessão desistir", func() bool { return terminou(sess) })
	if l := log.linha(t, linhaMorreu); !strings.Contains(l, "falhas_seguidas=8") {
		t.Errorf("a linha da desistência: %s", l)
	}
	// As religações do recuo que não vingaram são as de um incidente sem
	// keydown nenhum (TestSemprePronto_ReligacaoQueFalhaRecuaEDesiste).
	if n := log.contar(linhaNaoReligou); n != MaxFalhasSeguidas-2 {
		t.Errorf("%d religações do recuo que falharam, quero %d:\n%s", n, MaxFalhasSeguidas-2, log.String())
	}
	if n := log.contar(linhaNaoLigou); n != keydowns {
		t.Errorf("%d religações de keydown, quero %d", n, keydowns)
	}
	if got := recuos.lista(); len(got) < len(recuosAteDesistir) || !slices.Equal(got[len(got)-len(recuosAteDesistir)+1:], recuosAteDesistir[1:]) {
		t.Errorf("recuos = %v, quero terminar em %v", got, recuosAteDesistir[1:])
	}
	semViolacoes(t, fd)
}

// TestSemprePronto_KeydownDuranteAReligacaoDoRecuo: a tecla desce com a
// religação do recuo já em curso (o Start preso no driver) — não há recuo a
// cortar, e o ditado vai nessa religação. Ela não vinga: conta para o
// incidente, como sempre, e o ditado falha na hora com o erro do
// dispositivo, em vez de esperar o teto do Stop e sair como "o microfone não
// abriu a tempo".
func TestSemprePronto_KeydownDuranteAReligacaoDoRecuo(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	comRecuosFalsos(sess, true)
	// Depois da subida, que liga normalmente; antes da falha, que passa pelo
	// mutex do dispositivo falso.
	fd.ligarPreso = make(chan struct{})
	fd.noLigar = make(chan struct{}, 2) // o Start e a reabertura
	soltar := sync.OnceFunc(func() { close(fd.ligarPreso) })
	t.Cleanup(soltar)
	fd.mu.Lock()
	fd.falhasAoLigar = 2
	fd.mu.Unlock()

	fd.falharLeitura(errInvalidado)
	select {
	case <-fd.noLigar: // a religação imediata está dentro do Start
	case <-time.After(2 * time.Second):
		t.Fatal("a religação nunca chamou o Start")
	}
	sess.MarkStart(1, time.Now())
	soltar()
	esperarAte(t, "a religação não vingar", func() bool { return log.contar(linhaNaoReligou) == 1 })

	buf, _, err := sess.Stop(ctxFundo, 1)
	if !errors.Is(err, ErrDeviceLost) || errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrDeviceLost", err)
	}
	if buf.Janela.EsperouMicrofone != 0 {
		t.Errorf("o Stop esperou o microfone %s: a religação já tinha falhado, o erro sai na hora", buf.Janela.EsperouMicrofone)
	}
	l := log.linha(t, linhaNaoReligou)
	for _, campo := range []string{"tentativas_perdidas=1", "falhas_seguidas=2", "recuo_ms=1000"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha da religação que não vingou não tem %q: %s", campo, l)
		}
	}
	if n := log.contar(linhaNaoLigou); n != 0 {
		t.Errorf("a religação do recuo saiu como a de um keydown:\n%s", log.String())
	}
	semViolacoes(t, fd)
}

// TestSemprePronto_KeydownComORecuoVencido: o recuo já venceu no relógio, e a
// goroutine ainda não acordou para religar — o keydown chega junto com o fim
// do recuo. Não há recuo a cortar: o ditado vai na religação do recuo, que
// conta, e o microfone não tenta duas vezes.
func TestSemprePronto_KeydownComORecuoVencido(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, true) // o temporizador não acorda a goroutine
	fd.mu.Lock()
	fd.falhasAoLigar = 4 // a religação imediata e a do recuo: o Start e a reabertura de cada
	fd.mu.Unlock()

	fd.falharLeitura(errInvalidado)
	esperarAte(t, "o recuo começar", func() bool { return len(recuos.lista()) == 1 })
	fd.relogio.avancar(2 * time.Second) // o recuo de 1 s venceu
	sess.MarkStart(1, time.Now())
	esperarAte(t, "a religação do recuo não vingar", func() bool { return log.contar(linhaNaoReligou) == 2 })

	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("err = %v, quero ErrDeviceLost", err)
	}
	if n := log.contar("tentativas_perdidas=1 falhas_seguidas=3 recuo_ms=2000"); n != 1 {
		t.Errorf("quero o ditado perdido na religação do recuo, que conta:\n%s", log.String())
	}
	if n := log.contar(linhaNaoLigou); n != 0 {
		t.Errorf("o keydown cortou um recuo que já tinha vencido:\n%s", log.String())
	}
	if n := fd.contar("ligar falhou"); n != 4 {
		t.Errorf("%d Start que falharam, quero 4 (duas religações): %v", n, fd.historico())
	}
	semViolacoes(t, fd)
}

// TestSemprePronto_JanelaSemPrimeiroPacoteESoltaNoRepouso: o cliente parado
// volta da suspensão ligando sem erro e sem entregar nada. Com a tecla
// segurada, quem decide é o Stop — o ditado falha com ErrMicrofoneNaoAbriu —;
// no repouso, a janela ligada há VigiaDoPrimeiroPacote sem o primeiro pacote
// é solta e reaberta, e o ditado seguinte funciona. Antes, o vigia de parada
// exigia o primeiro pacote, o sempre pronto nunca estacionava para soltar o
// cliente, e todo ditado falhava até desmarcar e marcar a opção.
func TestSemprePronto_JanelaSemPrimeiroPacoteESoltaNoRepouso(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	dz := &devQueNaoEntrega{fakeDevice: fd, parado: true}
	sess, log := sessaoSobre(t, dz, fd.relogio, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)

	// A tecla segurada além do vigia: nada — quem decide é o Stop.
	sess.MarkStart(1, time.Now())
	fd.relogio.avancar(VigiaDoPrimeiroPacote + time.Second)
	algumasLeituras(fd)
	if fd.contar("liberar") != 0 {
		t.Fatalf("o vigia do primeiro pacote soltou o microfone com um ditado vivo:\n%s", log.String())
	}
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}

	// No repouso, a janela que nunca entregou é solta e reaberta.
	esperarAte(t, "a janela muda ser solta e reaberta", func() bool { return fd.contar("reabrir") == 1 && sessaoLigada(sess) })
	l := log.linha(t, linhaNaoComecou)
	for _, campo := range []string{"level=WARN", "modo=sempre_pronto", "ligado_ha_ms=3000"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha do vigia do primeiro pacote não tem %q: %s", campo, l)
		}
	}
	l = log.linha(t, linhaLeituraFalhou)
	for _, campo := range []string{"tentativas_perdidas=0", "falhas_seguidas=1", "recuo_ms=0"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha da falha não tem %q: %s", campo, l)
		}
	}
	if n := log.contar(linhaParou); n != 0 {
		t.Errorf("o vigia de parada levou a culpa por uma janela que nunca entregou:\n%s", log.String())
	}

	// A janela reaberta também não entrega (o microfone falso, sem pacote):
	// um período antes do vigia, nada — um Start frio não é parada —; no
	// vigia, é solta de novo.
	fd.relogio.avancar(VigiaDoPrimeiroPacote - periodoPadrao)
	algumasLeituras(fd)
	if n := fd.contar("liberar"); n != 1 {
		t.Fatalf("a janela foi solta %s depois do Start, antes do vigia", VigiaDoPrimeiroPacote-periodoPadrao)
	}
	fd.relogio.avancar(periodoPadrao)
	esperarAte(t, "a segunda janela muda ser solta e reaberta", func() bool { return fd.contar("reabrir") == 2 && sessaoLigada(sess) })
	if n := log.contar("ligado_ha_ms=2000"); n != 1 {
		t.Errorf("quero a segunda janela solta no vigia, com ligado_ha_ms=2000:\n%s", log.String())
	}

	// O microfone entrega, e o ditado seguinte sai.
	if b := ditar(t, sess, fd, 2, 100*time.Millisecond, 0.5); len(b.Samples) == 0 {
		t.Fatalf("o ditado depois da reabertura veio vazio:\n%s", log.String())
	}
	if terminou(sess) || log.contar("level=ERROR") != 0 {
		t.Errorf("ERROR numa janela de que o microfone se recuperou:\n%s", log.String())
	}
	semViolacoes(t, fd)
}

// TestSemprePronto_JanelaMudaRepetidaSoRecua: um microfone que liga e nunca
// entrega, reaberto a cada vigia, mais de MaxFalhasSeguidas vezes no mesmo
// incidente: só recua, e o sempre pronto não desiste — como a parada. E essas
// voltas não gastam a cota: uma leitura com erro depois delas não mata a
// sessão.
func TestSemprePronto_JanelaMudaRepetidaSoRecua(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1) // sem pacote nenhum: nunca entrega
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	recuos := comRecuosFalsos(sess, false)

	for i := 1; i <= MaxFalhasSeguidas+2; i++ {
		esperarAte(t, "ligado", func() bool { return sessaoLigada(sess) || terminou(sess) })
		if terminou(sess) {
			t.Fatalf("o sempre pronto desistiu na janela muda %d:\n%s", i, log.String())
		}
		fd.relogio.avancar(VigiaDoPrimeiroPacote)
		esperarAte(t, "o vigia soltar", func() bool { return fd.contar("liberar") == i || terminou(sess) })
	}
	esperarAte(t, "ligado", func() bool { return sessaoLigada(sess) || terminou(sess) })
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "a falha real ser tratada", func() bool { return fd.contar("liberar") == MaxFalhasSeguidas+3 || terminou(sess) })
	esperarAte(t, "religar depois da falha real", func() bool { return fd.ligado() || terminou(sess) })
	if terminou(sess) || log.contar(linhaMorreu) != 0 {
		t.Fatalf("o sempre pronto morreu por janelas mudas:\n%s", log.String())
	}
	// Recuou de verdade, e chegou ao teto: é o mesmo incidente.
	if p := recuos.lista(); len(p) == 0 || p[len(p)-1] != 30*time.Second {
		t.Errorf("recuos = %v, quero crescendo até 30 s", p)
	}
	semViolacoes(t, fd)
}

// devQueSeguraAConsulta é o cliente que liga e não entrega, com uma consulta
// que o teste segura: a goroutine de áudio fica dentro do Read, e só volta
// dele — para os vigias — quando o teste soltar, com o estado já mudado.
type devQueSeguraAConsulta struct {
	*devQueNaoEntrega
	mu      sync.Mutex
	segurar chan struct{}
	dentro  chan struct{}
}

// segurarProxima segura a próxima consulta: dentro fecha quando a goroutine
// de áudio entra nela, e soltar a devolve.
func (d *devQueSeguraAConsulta) segurarProxima() (dentro <-chan struct{}, soltar func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.segurar, d.dentro = make(chan struct{}), make(chan struct{})
	seg := d.segurar
	return d.dentro, sync.OnceFunc(func() { close(seg) })
}

func (d *devQueSeguraAConsulta) Read(ctx context.Context) (pacote, error) {
	d.mu.Lock()
	seg, den := d.segurar, d.dentro
	d.segurar, d.dentro = nil, nil
	d.mu.Unlock()
	if seg != nil {
		close(den)
		select {
		case <-seg:
		case <-ctx.Done():
		}
	}
	return d.devQueNaoEntrega.Read(ctx)
}

// TestSobDemanda_JanelaSemPrimeiroPacoteNaoTemVigia: no sob demanda, um
// ditado segurado além de VigiaDoPrimeiroPacote com um cliente que liga e não
// entrega. A goroutine de áudio volta da consulta já sem o ditado — o
// instante entre o Stop e o estacionamento —, e o vigia do primeiro pacote
// não age: quem solta o cliente é o estacionamento (liberarNoEstacionar), e
// nenhuma linha de vigia sai. Um mutante que deixava esse vigia agir no sob
// demanda sobrevivia à suíte (M6 da quarta rodada da revisão, 30-set-2026).
func TestSobDemanda_JanelaSemPrimeiroPacoteNaoTemVigia(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	ds := &devQueSeguraAConsulta{devQueNaoEntrega: &devQueNaoEntrega{fakeDevice: fd, parado: true}}
	sess, log := sessaoSobre(t, ds, fd.relogio, Config{})

	sess.MarkStart(1, time.Now())
	esperarMicrofone(t, fd, true)
	fd.relogio.avancar(VigiaDoPrimeiroPacote + time.Second)
	dentro, soltar := ds.segurarProxima()
	t.Cleanup(soltar)
	select {
	case <-dentro:
	case <-time.After(2 * time.Second):
		t.Fatal("a goroutine de áudio não voltou a consultar")
	}
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}
	soltar() // a consulta volta vazia, já sem ditado nenhum

	esperarAte(t, "o cliente solto", func() bool { return fd.contar("liberar") == 1 })
	if h := fd.historico(); len(h) < 2 || h[len(h)-2] != "parar" || h[len(h)-1] != "liberar" {
		t.Errorf("histórico = %v, quero o estacionamento com a política liberar (parar, liberar)", h)
	}
	if log.contar(linhaNaoComecou) != 0 || log.contar(linhaLeituraFalhou) != 0 {
		t.Errorf("o vigia do primeiro pacote agiu no sob demanda:\n%s", log.String())
	}
	semViolacoes(t, fd)
}

// TestSemprePronto_StartLentoNaoEJanelaMuda: a religação do sempre pronto
// fica 1,5 s presa dentro do Start, e a janela passa 1 s sem pacote depois
// de ele voltar — 2,5 s desde que o Start começou. O vigia do primeiro pacote
// conta de quando o Start voltou: um Start lento não é janela muda. Um
// mutante que contava do começo do Start sobrevivia à suíte (M8 da quarta
// rodada da revisão, 30-set-2026).
func TestSemprePronto_StartLentoNaoEJanelaMuda(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	comRecuosFalsos(sess, false)
	// Depois da subida, que liga normalmente; antes da falha, que passa pelo
	// mutex do dispositivo falso.
	fd.ligarPreso = make(chan struct{})
	fd.noLigar = make(chan struct{}, 1)
	soltar := sync.OnceFunc(func() { close(fd.ligarPreso) })
	t.Cleanup(soltar)

	fd.falharLeitura(errInvalidado) // a religação imediata fica presa no Start
	select {
	case <-fd.noLigar:
	case <-time.After(2 * time.Second):
		t.Fatal("a religação nunca chamou o Start")
	}
	fd.relogio.avancar(1500 * time.Millisecond) // o Start leva 1,5 s
	soltar()
	esperarAte(t, "ligado", func() bool { return sessaoLigada(sess) })
	fd.relogio.avancar(time.Second)
	algumasLeituras(fd)
	if fd.contar("liberar") != 1 || log.contar(linhaNaoComecou) != 0 {
		t.Fatalf("um Start lento virou janela muda (liberar=%d):\n%s", fd.contar("liberar"), log.String())
	}
	semViolacoes(t, fd)
}

// A quarta rodada da revisão (30-set-2026) achou dois furos no corte do recuo
// pelo keydown, provados numa cópia (R1 e R2 do revisor): o Start do keydown
// que vinga com a janela caindo logo na primeira leitura, e o incidente que
// não zerava no tempo certo depois de o keydown religar.

// ultimaLinha devolve a última linha do log com o trecho — vazia, se não houver.
func ultimaLinha(l *logSeguro, trecho string) string {
	var ultima string
	for _, s := range strings.Split(l.String(), "\n") {
		if strings.Contains(s, trecho) {
			ultima = s
		}
	}
	return ultima
}

// TestSemprePronto_KeydownsQueReligamENaoSeSustentamNaoGastamACota: o
// microfone religa e não se sustenta — o Start dá certo, e a primeira leitura
// depois da reabertura falha (o cenário de
// TestSemprePronto_MicrofoneQueReligaENaoSeSustentaRecua). No meio do recuo,
// cada keydown liga, e a janela dele cai antes do primeiro pacote: é como o
// Start que não vinga, e não conta. Antes, cada um gastava uma falha real e
// escalava o recuo, e seis keydowns matavam a sessão sem nenhum recuo vencer.
func TestSemprePronto_KeydownsQueReligamENaoSeSustentamNaoGastamACota(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	recuos := &recuoSegurado{} // nenhum recuo vence sozinho
	sess.temporizador = recuos.temporizador
	fd.mu.Lock()
	fd.leituraFalhaDepoisDeReabrir = true
	fd.mu.Unlock()

	// A leitura falha e a religação imediata reabre e cai na primeira
	// leitura: duas falhas reais, e 1 s de recuo, segurado.
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "o recuo começar", func() bool { return len(recuos.lista()) == 1 })
	incidente := func() [4]int64 {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return [4]int64{sess.recuoAte, int64(sess.falhasSeguidas), int64(sess.falhasReais), sess.recuoCortado}
	}
	antes := incidente()

	const keydowns = MaxFalhasSeguidas + 2
	for i := 1; i <= keydowns; i++ {
		id := dictation.AttemptID(i)
		sess.MarkStart(id, time.Now())
		esperarAte(t, "a janela do keydown cair", func() bool { return log.contar(linhaLeituraFalhou) == i+2 || terminou(sess) })
		if terminou(sess) {
			t.Fatalf("a sessão morreu no keydown %d, sem nenhum recuo vencer:\n%s", i, log.String())
		}
		if _, _, err := sess.Stop(ctxFundo, id); !errors.Is(err, ErrDeviceLost) {
			t.Fatalf("Stop %d: err = %v, quero ErrDeviceLost (a janela do ditado caiu)", i, err)
		}
	}
	if n := fd.contar("reabrir"); n != keydowns+1 {
		t.Errorf("%d reaberturas, quero %d: uma por keydown, e a religação imediata", n, keydowns+1)
	}
	// recuoAte, falhasSeguidas, falhasReais e recuoCortado: os mesmos de antes.
	if depois := incidente(); depois != antes {
		t.Fatalf("o incidente depois dos keydowns = %v, quero %v: a janela do keydown mexeu no recuo ou na cota", depois, antes)
	}
	l := ultimaLinha(log, linhaLeituraFalhou)
	for _, campo := range []string{"tentativas_perdidas=1", "reabre_sozinho=true", "recuo_restante_ms=1000"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a linha da janela do keydown que caiu não tem %q: %s", campo, l)
		}
	}
	if strings.Contains(l, "falhas_seguidas") {
		t.Errorf("a linha da janela do keydown fala do incidente, em que ela não conta: %s", l)
	}

	// O recuo volta a correr de onde estava: a sessão desiste na oitava falha
	// do recuo, como se não houvesse keydown nenhum.
	recuos.soltar()
	esperarAte(t, "a sessão desistir", func() bool { return terminou(sess) })
	if l := log.linha(t, linhaMorreu); !strings.Contains(l, "falhas_seguidas=8") {
		t.Errorf("a linha da desistência: %s", l)
	}
	if got := recuos.lista(); len(got) < len(recuosAteDesistir) || !slices.Equal(got[len(got)-len(recuosAteDesistir)+1:], recuosAteDesistir[1:]) {
		t.Errorf("recuos = %v, quero terminar em %v", got, recuosAteDesistir[1:])
	}
	semViolacoes(t, fd)
}

// TestSemprePronto_KeydownQueReligaZeraOIncidenteNoTempoCerto: o keydown corta
// o recuo de 30 s, e o microfone volta. Ele funciona 15 s — mais que
// JanelaSaudavel desde o primeiro pacote — e cai: é um incidente novo, com a
// religação imediata. Antes, o incidente contava do fim do recuo cortado, 30 s
// adiante, e a falha entrava no incidente velho, com o recuo no teto.
func TestSemprePronto_KeydownQueReligaZeraOIncidenteNoTempoCerto(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	var mu sync.Mutex
	var pedidos []time.Duration
	sess.temporizador = func(d time.Duration) <-chan time.Time {
		mu.Lock()
		defer mu.Unlock()
		pedidos = append(pedidos, d)
		ch := make(chan time.Time, 1)
		if d < 30*time.Second {
			ch <- time.Time{} // 1, 2, 5 e 10 s vencem na hora; o de 30 s fica
		}
		return ch
	}
	fd.mu.Lock()
	fd.falhasAoLigar = 2 * 5 // cinco religações que não vingam: o recuo chega a 30 s
	fd.mu.Unlock()

	fd.falharLeitura(errInvalidado)
	esperarAte(t, "o recuo de 30 s", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(pedidos) > 0 && pedidos[len(pedidos)-1] == 30*time.Second
	})

	sess.MarkStart(1, time.Now()) // corta o recuo de 30 s
	esperarMicrofone(t, fd, true)
	entregar(t, sess, fd, 200*time.Millisecond)
	if _, _, err := sess.Stop(ctxFundo, 1); err != nil {
		t.Fatalf("o ditado do keydown falhou: %v\n%s", err, log.String())
	}
	entregar(t, sess, fd, 15*time.Second) // saudável 15 s depois do primeiro pacote

	fd.falharLeitura(errInvalidado)
	esperarAte(t, "a falha", func() bool { return log.contar(linhaLeituraFalhou) == 2 })
	l := ultimaLinha(log, linhaLeituraFalhou)
	for _, campo := range []string{"falhas_seguidas=1 ", "recuo_ms=0"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a falha 15 s depois de o keydown religar entrou no incidente velho (quero %q): %s", campo, l)
		}
	}
	esperarAte(t, "religar na hora", func() bool { return sessaoLigada(sess) })
	semViolacoes(t, fd)
}

// TestSemprePronto_JanelaDoKeydownQueEstacionaNaoGuardaOCorte: o keydown corta
// o recuo e liga, e o sempre pronto é desligado antes de a janela entregar ou
// cair — o ditado sai sem áudio, e o microfone estaciona. O corte some com a
// janela: religado o sempre pronto, a primeira falha conta como qualquer
// outra, e não passa pela janela de um keydown que já acabou.
func TestSemprePronto_JanelaDoKeydownQueEstacionaNaoGuardaOCorte(t *testing.T) {
	fd := newFakeDevice(SampleRate, 1)
	sess, log := sessaoComLog(t, fd, Config{SemprePronto: true})
	recuos := &recuoSegurado{}
	sess.temporizador = recuos.temporizador
	fd.mu.Lock()
	fd.falhasAoLigar = 2 // a religação imediata não vinga: 1 s de recuo, segurado
	fd.mu.Unlock()

	fd.falharLeitura(errInvalidado)
	esperarAte(t, "o recuo começar", func() bool { return len(recuos.lista()) == 1 })

	sess.MarkStart(1, time.Now()) // corta o recuo e liga
	esperarAte(t, "a janela do keydown", func() bool { return sessaoLigada(sess) })
	sess.DefinirSemprePronto(false)
	if _, _, err := sess.Stop(ctxFundo, 1); !errors.Is(err, ErrMicrofoneNaoAbriu) {
		t.Fatalf("err = %v, quero ErrMicrofoneNaoAbriu", err)
	}
	esperarMicrofone(t, fd, false)

	sess.DefinirSemprePronto(true)
	esperarAte(t, "religar", func() bool { return sessaoLigada(sess) })
	fd.falharLeitura(errInvalidado)
	esperarAte(t, "a falha", func() bool { return log.contar(linhaLeituraFalhou) == 2 })
	// A terceira falha do incidente — a leitura, a religação que não vingou e
	// esta —, com o recuo de 2 s.
	l := ultimaLinha(log, linhaLeituraFalhou)
	for _, campo := range []string{"falhas_seguidas=3 ", "recuo_ms=2000"} {
		if !strings.Contains(l, campo) {
			t.Errorf("a falha depois de religar o sempre pronto não contou (quero %q): %s", campo, l)
		}
	}
	semViolacoes(t, fd)
}

// relogioComGancho é o relógio da sessão com um gancho que roda uma vez, na
// goroutine de áudio, entre a decisão de um vigia e a ação dele: na segunda
// leitura do relógio que já vê o instante limite. A primeira é a consulta
// vazia em que o vigia decide (run, antes do Read); a segunda, a de
// leituraFalhou, antes do lock em que ele age — entre as duas a goroutine de
// áudio não lê o relógio, e o teste não o lê enquanto espera.
type relogioComGancho struct {
	*relogioFalso
	mu     sync.Mutex
	limite int64
	vistas int
	gancho func()
	rodou  bool
}

func (r *relogioComGancho) agora() int64 {
	v := r.relogioFalso.agora()
	r.mu.Lock()
	var g func()
	if r.gancho != nil && v >= r.limite {
		if r.vistas++; r.vistas == 2 {
			g, r.gancho = r.gancho, nil
		}
	}
	r.mu.Unlock()
	if g != nil {
		g() // fora do mutex: o gancho lê o relógio de novo
		r.mu.Lock()
		r.rodou = true
		r.mu.Unlock()
	}
	return v
}

// armar põe o gancho para rodar quando o relógio chegar a limite.
func (r *relogioComGancho) armar(limite int64, g func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limite, r.vistas, r.gancho = limite, 0, g
}

func (r *relogioComGancho) jaRodou() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rodou
}

// TestSemprePronto_KeydownEntreOVigiaEAFalhaNaoPerdeODitado: o vigia decide
// com o microfone no repouso, e a tecla desce antes de ele agir. O keydown
// tirou o microfone do repouso: o vigia não age, o ditado sai, e o log não
// diz que o microfone foi solto. Antes, o repouso era decidido num lock e a
// ação vinha noutro — o ditado falhava com a causa do vigia (R3 do revisor,
// quarta rodada, 30-set-2026).
func TestSemprePronto_KeydownEntreOVigiaEAFalhaNaoPerdeODitado(t *testing.T) {
	for _, c := range []struct {
		nome     string
		preparar func(*testing.T, *Session, *fakeDevice)
		vigia    time.Duration
	}{
		{"primeiro pacote", func(*testing.T, *Session, *fakeDevice) {}, VigiaDoPrimeiroPacote},
		{"parada", func(t *testing.T, s *Session, fd *fakeDevice) { entregar(t, s, fd, 50*time.Millisecond) }, VigiaDeParada},
	} {
		t.Run(c.nome, func(t *testing.T) {
			fd := newFakeDevice(SampleRate, 1)
			rg := &relogioComGancho{relogioFalso: fd.relogio}
			sess, log := sessaoComRelogio(t, fd, rg.agora, Config{SemprePronto: true})
			comRecuosFalsos(sess, true)
			esperarAte(t, "ligado", func() bool { return sessaoLigada(sess) })
			c.preparar(t, sess, fd)

			rg.armar(fd.relogio.agora()+ticksDe(c.vigia), func() { sess.MarkStart(7, time.Now()) })
			fd.relogio.avancar(c.vigia)
			esperarAte(t, "o keydown entre o vigia e a falha", rg.jaRodou)
			algumasLeituras(fd)
			if fd.contar("liberar") != 0 || log.contar(linhaLeituraFalhou) != 0 {
				t.Fatalf("o vigia soltou o microfone com o keydown já marcado:\n%s", log.String())
			}
			if n := log.contar(linhaParou) + log.contar(linhaNaoComecou); n != 0 {
				t.Errorf("a linha do vigia saiu sem o vigia agir:\n%s", log.String())
			}
			entregar(t, sess, fd, 100*time.Millisecond)
			if _, _, err := sess.Stop(ctxFundo, 7); err != nil {
				t.Errorf("o ditado que começou junto com o vigia falhou: %v\n%s", err, log.String())
			}
			semViolacoes(t, fd)
		})
	}
}
