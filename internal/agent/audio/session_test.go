package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// relogioFalso é o relógio QPC dos testes, em 100 ns. Parado, a não ser
// que o teste avance — ou que o dispositivo falso entregue áudio, que
// avança o relógio até o fim do que foi "gravado".
type relogioFalso struct {
	mu sync.Mutex
	t  int64
}

// novoRelogioFalso começa em 1000 s: longe do zero, que para a Session é
// "carimbo ausente".
func novoRelogioFalso() *relogioFalso { return &relogioFalso{t: ticksDe(1000 * time.Second)} }

func (r *relogioFalso) agora() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogioFalso) avancar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t += ticksDe(d)
}

// ateAo avança o relógio até t, se ele ainda estiver antes.
func (r *relogioFalso) ateAo(t int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t > r.t {
		r.t = t
	}
}

// fakeDevice é um device de teste, sem nenhuma dependência de Win32: só um
// canal que o teste alimenta com pacotes, exatamente como o loop de
// GetBuffer/ReleaseBuffer real alimentaria. Existe para provar que Source/
// Session ligam RingBuffer, Resampler e VAD corretamente, sem precisar de
// hardware nem do Windows (ADR-010: só a captura em si — mmdevapi — é
// específica de plataforma; a máquina em volta dela não é).
//
// Desde a ADR-015 ele também faz o papel do IAudioClient no liga-desliga,
// e RECUSA a ordem errada, como o WASAPI recusaria: Ligar com o microfone
// já ligado, Parar sem ter ligado, Reset com o stream rodando e Read com o
// microfone estacionado viram violações que os testes conferem.
//
// O relógio: Ligar marca o instante do primeiro frame (agora +
// latenciaLigar); cada pacote lido é carimbado em sequência a partir dali,
// e o relógio anda até o fim do que foi entregue — o áudio só existe
// depois de gravado.
type fakeDevice struct {
	sampleRate, channels int
	packets              chan []float32
	failAfter            error // se setado, Read devolve isto depois que packets fecha
	relogio              *relogioFalso

	// Configuráveis antes de a sessão começar.

	// latenciaLigar é quanto depois do Start o primeiro frame é gravado.
	// Negativa simula o motor carimbando o primeiro frame um pouco antes
	// do Start, dentro de um período.
	latenciaLigar time.Duration
	// falharZerar faz o Reset falhar (ex.: AUDCLNT_E_BUFFER_OPERATION_PENDING).
	falharZerar error
	// sobraAoParar é o pacote que fica pendente no Stop, como os que o
	// WASAPI guarda até um Reset; idadeDaSobra é quanto antes do Stop ele
	// foi gravado.
	sobraAoParar []float32
	idadeDaSobra time.Duration
	// zerarNaoLimpa simula um Reset que não descarta os pendentes (um
	// driver com defeito): é o caso que o detector de contaminação pega.
	zerarNaoLimpa bool
	// ligarPreso, se não for nil, segura o Ligar até ser fechado: um
	// Start que demora no driver. noLigar avisa que o Ligar começou.
	ligarPreso chan struct{}
	noLigar    chan struct{}
	// aoParar, se não for nil, roda no começo de cada Parar, na goroutine
	// de áudio: é o instante em que o microfone está estacionando.
	aoParar func()
	// falhasAoLigar faz os próximos N Ligar falharem, como um Start num
	// cliente invalidado (AUDCLNT_E_DEVICE_INVALIDATED).
	falhasAoLigar int
	// erroDeLeitura, se não for nil, é devolvido pela próxima leitura: o
	// cliente invalidado com o microfone ligado. Ver falharLeitura.
	erroDeLeitura error
	// leituraFalhaDepoisDeReabrir faz a PRIMEIRA leitura depois de cada
	// reabertura falhar: um microfone que religa e não se sustenta.
	leituraFalhaDepoisDeReabrir bool

	mu        sync.Mutex
	closed    bool
	rodando   bool
	liberado  bool
	chamadas  []string
	leituras  int
	violacoes []string
	posQPC    int64
	pendentes []pacote
}

func newFakeDevice(sampleRate, channels int) *fakeDevice {
	return &fakeDevice{
		sampleRate: sampleRate, channels: channels,
		packets: make(chan []float32, 16),
		relogio: novoRelogioFalso(),
	}
}

func (d *fakeDevice) MixFormat() (int, int)  { return d.sampleRate, d.channels }
func (d *fakeDevice) Periodo() time.Duration { return 10 * time.Millisecond }

func (d *fakeDevice) violou(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	d.violacoes = append(d.violacoes, msg)
	return errors.New("fakeDevice: " + msg)
}

func (d *fakeDevice) Ligar() error {
	if d.ligarPreso != nil {
		if d.noLigar != nil {
			d.noLigar <- struct{}{}
		}
		<-d.ligarPreso
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rodando {
		return d.violou("Ligar com o microfone já ligado (AUDCLNT_E_NOT_STOPPED)")
	}
	if d.liberado {
		d.chamadas = append(d.chamadas, "reabrir")
		d.liberado = false
	}
	if d.falhasAoLigar > 0 {
		d.falhasAoLigar--
		d.chamadas = append(d.chamadas, "ligar falhou")
		return errors.New("fakeDevice: AUDCLNT_E_DEVICE_INVALIDATED")
	}
	d.chamadas = append(d.chamadas, "ligar")
	d.rodando = true
	d.posQPC = d.relogio.agora() + ticksDe(d.latenciaLigar)
	if d.leituraFalhaDepoisDeReabrir && len(d.chamadas) >= 2 && d.chamadas[len(d.chamadas)-2] == "reabrir" {
		d.erroDeLeitura = errors.New("fakeDevice: AUDCLNT_E_DEVICE_INVALIDATED na primeira leitura")
	}
	return nil
}

// falharLeitura faz a próxima leitura falhar com err, como um cliente
// invalidado com o microfone ligado (suspender e retomar, o microfone USB
// retirado).
func (d *fakeDevice) falharLeitura(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.erroDeLeitura = err
}

func (d *fakeDevice) Parar() error {
	if d.aoParar != nil {
		d.aoParar()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.rodando {
		return d.violou("Parar sem Ligar")
	}
	d.chamadas = append(d.chamadas, "parar")
	d.rodando = false
	if d.sobraAoParar != nil {
		d.pendentes = append(d.pendentes, pacote{
			amostras: append([]float32(nil), d.sobraAoParar...),
			qpc:      d.relogio.agora() - ticksDe(d.idadeDaSobra),
		})
	}
	return nil
}

func (d *fakeDevice) Zerar() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rodando {
		return d.violou("Reset com o stream rodando")
	}
	d.chamadas = append(d.chamadas, "zerar")
	if d.falharZerar != nil {
		return d.falharZerar
	}
	if !d.zerarNaoLimpa {
		d.pendentes = nil
	}
	return nil
}

func (d *fakeDevice) Liberar() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.chamadas = append(d.chamadas, "liberar")
	d.rodando = false
	d.liberado = true
	d.pendentes = nil
}

func (d *fakeDevice) Read(ctx context.Context) (pacote, error) {
	d.mu.Lock()
	d.leituras++
	if !d.rodando {
		d.violou("Read com o microfone estacionado")
	}
	if err := d.erroDeLeitura; err != nil {
		d.erroDeLeitura = nil
		d.chamadas = append(d.chamadas, "leitura falhou")
		d.mu.Unlock()
		return pacote{}, err
	}
	if len(d.pendentes) > 0 {
		p := d.pendentes[0]
		d.pendentes = d.pendentes[1:]
		d.mu.Unlock()
		return p, nil
	}
	d.mu.Unlock()

	select {
	case p, ok := <-d.packets:
		if !ok {
			if d.failAfter != nil {
				return pacote{}, d.failAfter
			}
			return pacote{}, errors.New("fakeDevice: packets fechado sem falha configurada")
		}
		frames := int64(len(p) / d.channels)
		d.mu.Lock()
		qpc := d.posQPC
		d.posQPC += ticksDe(duracaoDeAmostras(frames, d.sampleRate))
		fim := d.posQPC
		d.mu.Unlock()
		d.relogio.ateAo(fim)
		return pacote{amostras: p, qpc: qpc}, nil
	case <-ctx.Done():
		return pacote{}, ctx.Err()
	case <-time.After(time.Millisecond):
		return pacote{}, nil // consulta vazia
	}
}

func (d *fakeDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// ligado diz se o microfone falso está rodando agora.
func (d *fakeDevice) ligado() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rodando
}

func (d *fakeDevice) fechado() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func (d *fakeDevice) totalDeLeituras() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.leituras
}

func (d *fakeDevice) historico() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.chamadas...)
}

// contar diz quantas vezes a chamada aparece no histórico.
func (d *fakeDevice) contar(chamada string) int {
	n := 0
	for _, c := range d.historico() {
		if c == chamada {
			n++
		}
	}
	return n
}

// semViolacoes falha o teste se a Session chamou o dispositivo na ordem
// errada.
func semViolacoes(t *testing.T, d *fakeDevice) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, v := range d.violacoes {
		t.Errorf("a Session violou o ciclo do microfone: %s", v)
	}
}

// esperarAte espera cond valer, com teto.
func esperarAte(t *testing.T, oque string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout esperando %s", oque)
}

// esperarMicrofone espera o microfone falso ficar ligado (ou estacionado).
func esperarMicrofone(t *testing.T, d *fakeDevice, ligado bool) {
	t.Helper()
	oque := "o microfone ligar"
	if !ligado {
		oque = "o microfone estacionar"
	}
	esperarAte(t, oque, func() bool { return d.ligado() == ligado })
}

func waitForCursor(t *testing.T, sess *Session, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess.mu.Lock()
		c := sess.ring.Cursor()
		sess.mu.Unlock()
		if c >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout esperando cursor do ring buffer alcançar %d amostras", want)
}

func startTestSession(t *testing.T, dev *fakeDevice, cfg Config) *Session {
	t.Helper()
	sess, err := startSession(context.Background(), cfg, func(context.Context) (device, error) {
		return dev, nil
	}, dev.relogio.agora)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	// A espera de cauda não pode depender do relógio real num teste: o
	// dublê registra quanto teria dormido e devolve na hora. Quem quiser
	// observar a espera usa esperaDeCauda.
	sess.dormir = func(d time.Duration) { sess.dormiuNoTeste += d }
	return sess
}

// esperaDeCauda é quanto Stop teria esperado pela cauda, em tempo
// simulado.
func esperaDeCauda(sess *Session) time.Duration { return sess.dormiuNoTeste }

// TestSession_StopIncludesPreRoll descreve o modo "sempre pronto"
// (ADR-015): o microfone já grava antes da tecla descer, e o buffer inclui
// o pre-roll. No padrão, sob demanda, o pre-roll é zero — ver
// microfone_test.go.
func TestSession_StopIncludesPreRoll(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1) // ratio 1:1, mono — sem reamostragem
	cfg := Config{SemprePronto: true, PreRoll: 100 * time.Millisecond, MaxRecording: 2 * time.Second}
	sess := startTestSession(t, dev, cfg)

	attempt := dictation.AttemptID(1)

	// Pre-roll: 200ms de "fala" (tom alto) antes da marca — mais do que
	// o PreRoll configurado (100ms), para provar que só os últimos 100ms
	// sobrevivem no buffer devolvido.
	preRollSamples := int(0.2 * float64(SampleRate))
	preRoll := make([]float32, preRollSamples)
	for i := range preRoll {
		preRoll[i] = 0.5
	}
	dev.packets <- preRoll
	// O Resampler é stateful e sempre segura a última amostra de cada
	// Process() para interpolar com o próximo pacote (session.go /
	// resampler.go) — por isso o cursor fica até 1 amostra atrás do que
	// foi enviado enquanto não chega mais dado. Isso é irrelevante para
	// captura contínua de verdade (sempre chega mais um pacote) e some
	// assim que o próximo Write acontece; o teste só precisa tolerar essa
	// defasagem de no máximo 1 amostra por pacote.
	waitForCursor(t, sess, int64(preRollSamples-1))

	sess.MarkStart(attempt, time.Now())

	live := make([]float32, int(0.05*float64(SampleRate))) // 50ms ao vivo
	for i := range live {
		live[i] = 0.5
	}
	dev.packets <- live
	waitForCursor(t, sess, int64(preRollSamples+len(live)-1))

	buf, vad, err := sess.Stop(context.Background(), attempt)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	wantSamples := int(0.1*float64(SampleRate)) + len(live) // 100ms de preroll + 50ms ao vivo
	if diff := abs(len(buf.Samples) - wantSamples); diff > 1 {
		t.Fatalf("len(buf.Samples) = %d, want ~%d (±1, preroll deveria ter sido recortado para Config.PreRoll)", len(buf.Samples), wantSamples)
	}
	if buf.SampleRate != SampleRate {
		t.Fatalf("buf.SampleRate = %d, want %d", buf.SampleRate, SampleRate)
	}
	if !vad.HasSpeech {
		t.Fatalf("vad.HasSpeech = false, want true (tom bem acima do limiar)")
	}
}

func TestSession_AbortDiscardsMark(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	attempt := dictation.AttemptID(7)
	sess.MarkStart(attempt, time.Now())
	sess.Abort(attempt)

	if _, _, err := sess.Stop(context.Background(), attempt); err == nil {
		t.Fatalf("Stop após Abort deveria falhar, mas não falhou")
	}
}

func TestSession_StopWithoutMarkStartFails(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{})

	if _, _, err := sess.Stop(context.Background(), dictation.AttemptID(99)); err == nil {
		t.Fatalf("Stop sem MarkStart deveria falhar, mas não falhou")
	}
}

func TestSession_DeviceFailureSurfacesOnStop(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	dev.failAfter = ErrDeviceLost
	sess := startTestSession(t, dev, Config{})

	attempt := dictation.AttemptID(3)
	sess.MarkStart(attempt, time.Now())

	dev.packets <- []float32{0.1, 0.1}
	waitForCursor(t, sess, 1) // ver comentário em TestSession_StopIncludesPreRoll sobre a defasagem de até 1 amostra
	close(dev.packets)        // simula GetBuffer falhando (mic removido)

	// Dá tempo do loop de captura observar a falha — e soltar o cliente —
	// antes de chamar Stop. A sessão NÃO morre por isso (M1): só a
	// tentativa em curso falha.
	esperarAte(t, "a leitura que falhou soltar o microfone", func() bool { return dev.contar("liberar") == 1 })

	buf, _, err := sess.Stop(context.Background(), attempt)
	if err == nil {
		t.Fatalf("Stop deveria falhar após o dispositivo cair")
	}
	if !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("err = %v, want wrapping ErrDeviceLost", err)
	}
	// A linha "audio: janela do microfone" vale justamente depois que o
	// microfone morreu: o runtime descarta a janela sem Modo.
	if buf.Janela.Modo != ModoSobDemanda || buf.Janela.Aberturas != 1 {
		t.Errorf("janela = %+v, quero medida (modo sob demanda, uma abertura)", buf.Janela)
	}
	if len(buf.Samples) != 0 {
		t.Errorf("devolveu %d amostras junto com o erro", len(buf.Samples))
	}
}

func TestSession_CloseReleasesDevice(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess, err := startSession(context.Background(), Config{}, func(context.Context) (device, error) {
		return dev, nil
	}, dev.relogio.agora)
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !dev.fechado() {
		t.Fatalf("device.Close() não foi chamado")
	}
}

func TestSource_StartPropagatesOpenError(t *testing.T) {
	wantErr := errors.New("dispositivo padrão indisponível")
	_, err := startSession(context.Background(), Config{}, func(context.Context) (device, error) {
		return nil, wantErr
	}, novoRelogioFalso().agora)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestSession_ResamplesNonNativeMixFormat(t *testing.T) {
	// 48 kHz estéreo, como o mix format real tipicamente é (ADR-010 D1).
	dev := newFakeDevice(48000, 2)
	cfg := Config{PreRoll: 0, MaxRecording: 2 * time.Second}
	sess := startTestSession(t, dev, cfg)

	attempt := dictation.AttemptID(1)
	sess.MarkStart(attempt, time.Now())

	// 480000 frames estéreo a 48kHz = 10s -> mas mandamos só 48000 frames
	// (1s) para o teste ser rápido; espera 16000 amostras de saída (1s a
	// 16kHz).
	frames := 48000
	packet := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		packet[i*2] = 0.4
		packet[i*2+1] = 0.4
	}
	dev.packets <- packet
	waitForCursor(t, sess, 15900) // ~1s a 16kHz, com folga para arredondamento

	buf, _, err := sess.Stop(context.Background(), attempt)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if diff := abs(len(buf.Samples) - 16000); diff > 5 {
		t.Fatalf("len(buf.Samples) = %d, want ~16000 (mix format 48kHz estéreo reamostrado para 16kHz mono)", len(buf.Samples))
	}
}

// TestDefaultPreRoll_PequenoOBastanteParaNaoPegarAFalaAnterior é uma
// tripwire, não uma verificação de comportamento: ela existe para que
// aumentar o pre-roll exija passar por aqui e ler o motivo.
//
// O pre-roll retém o áudio ANTERIOR à tecla descer. Se for maior que o
// intervalo entre dois ditados, o final da frase anterior entra na
// seguinte — medido em 28-set-2026, 39 runas vazando para dentro da
// transcrição e daí para o campo de quem usa.
//
// O teto é o tempo que a pessoa leva, no mínimo, entre terminar um
// ditado e começar o próximo: a inserção sozinha custa perto de um
// segundo. 500 ms deixa folga de sobra e ainda assim torna o vazamento
// fisicamente impossível.
func TestDefaultPreRoll_PequenoOBastanteParaNaoPegarAFalaAnterior(t *testing.T) {
	const teto = 500 * time.Millisecond
	if DefaultPreRoll > teto {
		t.Errorf("DefaultPreRoll = %s, acima do teto de %s.\n"+
			"Um pre-roll maior que o intervalo entre dois ditados faz o FINAL da frase "+
			"anterior entrar na seguinte. Se o objetivo for parar de cortar sílaba "+
			"inicial, o caminho é aparar silêncio pelo VAD, não alargar a janela.",
			DefaultPreRoll, teto)
	}
	// E não pode ser zero: no "sempre pronto" (ADR-015), o AC-003 exige
	// que a fala iniciada em cima da tecla sobreviva.
	if DefaultPreRoll <= 0 {
		t.Error("DefaultPreRoll zerado: no sempre pronto, quem começa a falar em cima da tecla perde a primeira sílaba (AC-003)")
	}
}

// --- margem de cauda (28-set-2026) -------------------------------------

// encher devolve n amostras de um tom com a amplitude dada. 0 é silêncio.
func encher(n int, amp float32) []float32 {
	p := make([]float32, n)
	for i := range p {
		p[i] = amp
	}
	return p
}

func amostrasDe(d time.Duration) int { return int(d.Seconds() * float64(SampleRate)) }

// TestSession_NaoEsperaQuandoACaudaJaEhSilencio é a promessa inteira da
// margem adaptativa: quem termina de falar antes de soltar a tecla NÃO
// paga latência nenhuma.
//
// Um post-roll fixo cobraria a margem em todo ditado. Este teste é o que
// impede alguém de transformar a margem adaptativa num post-roll fixo
// sem perceber.
func TestSession_NaoEsperaQuandoACaudaJaEhSilencio(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	// Sempre pronto: o teste manda áudio antes da marca.
	sess := startTestSession(t, dev, Config{SemprePronto: true, PreRoll: 100 * time.Millisecond, MaxRecording: 2 * time.Second})

	dev.packets <- encher(amostrasDe(300*time.Millisecond), 0.5) // fala
	dev.packets <- encher(amostrasDe(200*time.Millisecond), 0)   // e então silêncio
	total := amostrasDe(500 * time.Millisecond)
	waitForCursor(t, sess, int64(total-2))

	sess.MarkStart(dictation.AttemptID(1), time.Now())
	dev.packets <- encher(amostrasDe(100*time.Millisecond), 0)
	waitForCursor(t, sess, int64(total+amostrasDe(100*time.Millisecond)-3))

	if _, _, err := sess.Stop(context.Background(), dictation.AttemptID(1)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := esperaDeCauda(sess); got != 0 {
		t.Errorf("esperou %s com a cauda já em silêncio — a margem virou post-roll fixo", got)
	}
}

// TestSession_EsperaQuandoACaudaAindaEhFala cobre o caso do Mauro:
// soltar a tecla em cima da última sílaba. Sem a espera, o áudio acaba
// no meio da palavra — "fator R" virou "fator E".
func TestSession_EsperaQuandoACaudaAindaEhFala(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{PreRoll: 100 * time.Millisecond, MaxRecording: 2 * time.Second})

	sess.MarkStart(dictation.AttemptID(1), time.Now())
	// Fala até o último instante: nada de silêncio antes do Stop.
	n := amostrasDe(400 * time.Millisecond)
	dev.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))

	if _, _, err := sess.Stop(context.Background(), dictation.AttemptID(1)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got := esperaDeCauda(sess)
	if got == 0 {
		t.Fatal("não esperou nada com a cauda ainda em fala — a última sílaba se perde")
	}
	if got > DefaultMargemDeCauda {
		t.Errorf("esperou %s, acima do teto de %s", got, DefaultMargemDeCauda)
	}
}

// TestSession_MargemNegativaDesligaAEspera: um chamador que não queira a
// espera (gravação de dataset, teste) precisa conseguir desligá-la sem
// depender de o zero value significar "desligado" — zero significa
// "use o default".
func TestSession_MargemNegativaDesligaAEspera(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{
		PreRoll: 100 * time.Millisecond, MaxRecording: 2 * time.Second, MargemDeCauda: -1,
	})

	sess.MarkStart(dictation.AttemptID(1), time.Now())
	n := amostrasDe(400 * time.Millisecond)
	dev.packets <- encher(n, 0.5) // fala até o fim
	waitForCursor(t, sess, int64(n-1))

	if _, _, err := sess.Stop(context.Background(), dictation.AttemptID(1)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := esperaDeCauda(sess); got != 0 {
		t.Errorf("esperou %s com MargemDeCauda negativa", got)
	}
}

// TestSession_EsperaNaoPassaDoTeto: se a pessoa continuar falando sem
// parar, a espera não pode ser indefinida — o ditado precisa terminar.
func TestSession_EsperaNaoPassaDoTeto(t *testing.T) {
	dev := newFakeDevice(SampleRate, 1)
	sess := startTestSession(t, dev, Config{
		PreRoll: 100 * time.Millisecond, MaxRecording: 2 * time.Second,
		MargemDeCauda: 120 * time.Millisecond,
	})

	sess.MarkStart(dictation.AttemptID(1), time.Now())
	n := amostrasDe(400 * time.Millisecond)
	dev.packets <- encher(n, 0.5)
	waitForCursor(t, sess, int64(n-1))

	if _, _, err := sess.Stop(context.Background(), dictation.AttemptID(1)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := esperaDeCauda(sess); got > 120*time.Millisecond {
		t.Errorf("esperou %s, acima do teto configurado de 120ms", got)
	}
}
