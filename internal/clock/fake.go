package clock

import "time"

// Fake é o Clock controlado usado nos testes da Fase 1. Now() só avança
// quando o teste chama Set ou Advance; AfterFunc não cria goroutine nem
// usa tempo real — apenas registra o callback.
//
// Fake não é seguro para uso concorrente. Isso é intencional: o núcleo
// (Machine.Handle) já é documentado como não thread-safe por projeto
// (um único consumidor serializado), e o Fake segue a mesma regra.
type Fake struct {
	now    time.Time
	timers []*fakeTimer
	seq    uint64 // contador monotônico: desempate FIFO entre timers com o mesmo horário
}

// fakeTimer é o Timer devolvido por Fake.AfterFunc.
type fakeTimer struct {
	at      time.Time
	seq     uint64
	f       func()
	fired   bool
	stopped bool
}

// Stop cancela o timer. Retorna false se ele já disparou ou já havia
// sido parado antes.
func (t *fakeTimer) Stop() bool {
	if t.fired || t.stopped {
		return false
	}
	t.stopped = true
	return true
}

// NewFake cria um Fake com o relógio posicionado em start.
func NewFake(start time.Time) *Fake {
	return &Fake{now: start}
}

// Now retorna o instante atual do relógio falso.
func (c *Fake) Now() time.Time {
	return c.now
}

// Set posiciona o relógio em t, sem disparar nenhum timer. Usado para
// preparar o instante inicial de um teste.
func (c *Fake) Set(t time.Time) {
	c.now = t
}

// AfterFunc registra f para disparar quando o relógio alcançar
// Now()+d. Não cria goroutine; o disparo só acontece dentro de uma
// chamada a Advance.
func (c *Fake) AfterFunc(d time.Duration, f func()) Timer {
	c.seq++
	t := &fakeTimer{
		at:  c.now.Add(d),
		seq: c.seq,
		f:   f,
	}
	c.timers = append(c.timers, t)
	return t
}

// Advance avança o relógio em d e dispara, sincronamente e dentro
// desta chamada, todos os callbacks vencidos (at <= novo Now), em
// ordem cronológica; empate de horário é resolvido por ordem de
// agendamento (FIFO).
//
// Um callback disparado durante o Advance pode agendar um novo timer
// via AfterFunc; se o novo timer também vencer dentro da mesma janela,
// ele dispara ainda dentro deste Advance, antes de retornar.
//
// Quando Advance retorna, todo timer vencido até o novo Now já
// disparou.
func (c *Fake) Advance(d time.Duration) {
	target := c.now.Add(d)

	for {
		next := c.nextDue(target)
		if next == nil {
			break
		}
		// O relógio avança até o instante do disparo antes de chamar
		// o callback, para que um Now() lido de dentro dele reflita
		// o horário do próprio timer, não o alvo final do Advance.
		c.now = next.at
		next.fired = true
		next.f()
	}

	c.now = target
}

// nextDue encontra, entre os timers ainda pendentes com vencimento até
// target, o mais antigo cronologicamente (empate: menor seq/FIFO).
// Retorna nil se nenhum estiver vencido.
func (c *Fake) nextDue(target time.Time) *fakeTimer {
	var next *fakeTimer
	for _, t := range c.timers {
		if t.fired || t.stopped {
			continue
		}
		if t.at.After(target) {
			continue
		}
		if next == nil || t.at.Before(next.at) || (t.at.Equal(next.at) && t.seq < next.seq) {
			next = t
		}
	}
	return next
}
