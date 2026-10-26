package overlay

import (
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// ErrNotSupported é devolvido por New fora do Windows — existe só para
// que este pacote (e o runtime que o importa) continue compilando e
// testável no runner Linux do CI (ver CLAUDE.md, "compilação
// multiplataforma"). A janela nativa só existe no Windows.
var ErrNotSupported = errors.New("overlay: overlay nativo só é suportado no Windows")

// autoHideDelay é quanto tempo um desfecho terminal (success/error/
// blocked) fica visível antes do overlay se esconder sozinho.
// dictation.Machine emite ShowOverlayEffect uma única vez por
// tentativa, no desfecho (ADR-007) — nunca um "esconder" separado —
// então esconder de novo depois de um tempo é responsabilidade deste
// pacote, não do núcleo.
const autoHideDelay = 2500 * time.Millisecond

// JanelaDaOnda é quanto PASSADO a onda mostra. O que sai desta janela é
// descartado, então a onda é sempre "o último JanelaDaOnda de voz".
//
// # Por que por tempo, e não por número de amostras
//
// Até 26-set-2026 isto era `maxAmplitudeSamples = 512` — um anel contado
// em QUADROS. E quadro, aqui, não tem duração fixa: a captura entrega o
// que IAudioCaptureClient::GetNextPacketSize devolver, que é o período
// do dispositivo (wasapi_windows.go). Tipicamente 10 ms, mas não é
// garantido e varia entre placas.
//
// O efeito era silencioso e feio: 512 quadros valem 5,1 s numa máquina
// de pacote de 10 ms e 10,2 s numa de 20 ms. Como Bars divide o que
// houver em BarCount baldes, cada barra passava a cobrir o DOBRO de
// tempo — e uma barra que resume meio segundo de fala é sempre o pico de
// alguma sílaba, então todas ficam iguais. A onda virava uma cerca, na
// segunda máquina, sem nada no código dizendo por quê.
//
// 1,3 s foi escolhido olhando: ver docs/adr/ADR-014. Abaixo disso a onda
// fica curta demais para mostrar o contorno da frase; acima, cada barra
// volta a resumir uma sílaba inteira.
const JanelaDaOnda = 1300 * time.Millisecond

// maxAmplitudeSamples é só um teto de memória para o caso de um
// dispositivo de período muito curto: 1,3 s a 3 ms por pacote dá ~433
// amostras. Não é o que define a janela — JanelaDaOnda é.
const maxAmplitudeSamples = 1024

// Mensagens de janela privadas deste pacote (WM_APP..0xBFFF é a faixa
// reservada pelo Win32 para uso de aplicação — winuser.h). msgUpdate
// pede um redesenho a partir do estado atual; msgQuit pede que o loop
// de mensagens termine. Ambas são só acordadas via PostMessageW
// (window.Wake / window.RequestQuit), nunca despachadas por outro meio.
const (
	msgApp    = 0x8000
	msgUpdate = msgApp + 1
	msgQuit   = msgApp + 2

	// wmClose é o WM_CLOSE de winuser.h — ver wndProc.
	wmClose = 0x0010
)

// defWindowProc é win32.DefWindowProcW. É variável só para o teste do
// wndProc pôr um dublê no lugar e verificar que uma mensagem NÃO chegou
// ao Windows — sem chamada de sistema nenhuma.
var defWindowProc = win32.DefWindowProcW

// window é a fronteira de plataforma do overlay: tudo que precisa de
// HWND ou handle de GDI de verdade. Implementado de verdade em
// window_windows.go; window_other.go sempre devolve ErrNotSupported,
// mantendo este pacote — e sua lógica pura (layout.go, canvas.go,
// mensagem.go, tipografia.go, render.go) — compilável e testável em GOOS=linux (ver
// doc.go).
//
// Draw e SetVisible só são chamados de dentro do loop de mensagens
// (run(), na goroutine que criou a janela — ADR-010, "Modelo de
// threads"). Wake e RequestQuit são a única porta de entrada segura de
// QUALQUER outra goroutine: por baixo, postam uma mensagem via
// PostMessageW, que o próprio Win32 documenta como seguro entre
// threads — elas não tocam HWND/GDI diretamente, só enfileiram.
//
// Draw apresenta c com a janela no tamanho de c e o canto em (x, y) da
// tela: conteúdo, posição e tamanho mudam juntos, numa chamada só (ver
// "A janela acompanha a pílula", em layout.go).
type window interface {
	Draw(c *Canvas, x, y int32) error
	SetVisible(visible bool) error
	Wake() error
	RequestQuit() error
	Close() error
}

// Overlay é o overlay de estado do ditado (ADR-010, D2): uma janela
// layered, sem foco, sem clique, fora do Alt+Tab, mostrando a fase
// atual de uma tentativa (dictation.OverlayPhase) e a onda de
// amplitude ao vivo durante a gravação.
//
// SetPhase e PushAmplitude são seguros para chamar de qualquer
// goroutine — em particular, do runtime que é o único consumidor
// serializado de dictation.Machine (ADR-007). Eles nunca tocam a
// janela nativa diretamente; só escrevem em campos protegidos por mu e
// acordam a goroutine dona da janela via window.Wake.
type Overlay struct {
	win  window
	done chan struct{}

	mu    sync.Mutex
	phase dictation.OverlayPhase
	// texto é o da mensagem no lugar do Label da fase (SetPhaseComTexto).
	// SetPhase o limpa: só a fase que veio com ele o mostra.
	texto     string
	amps      []amostraOnda
	hideTimer *time.Timer

	// agora é injetável só para teste: a janela da onda é medida em
	// tempo real, e um teste que dependesse de dormir 1,3 s seria lento e
	// instável. Nil usa time.Now.
	agora func() time.Time

	// aoFalharDesenho é chamado na primeira falha de apresentação (ver
	// reportarFalhaDeDesenho). Nil mantém o silêncio de antes.
	aoFalharDesenho      func(error)
	avisouFalhaDeDesenho bool

	// aoPedirFechar recebe o WM_CLOSE que chega de fora — ver
	// AoPedirFechar. fecharPendente guarda o que chegou antes de alguém
	// registrar o observador.
	aoPedirFechar  func()
	fecharPendente bool

	// geo é preenchido por run() ANTES de destravar New(), então quem
	// chamou New pode ler sem corrida.
	geo geometria
}

// geometria é onde a janela do overlay foi posta e em que tela.
type geometria struct {
	X, Y, W, H   int32
	TelaW, TelaH int32
}

// Geometria devolve a posição e o tamanho com que a janela do overlay foi
// CRIADA — os da pílula compacta, que é o primeiro quadro de todo ditado —
// e o tamanho da tela primária em que ela foi posicionada. Depois disso a
// janela acompanha a pílula de cada fase, sempre com o centro no mesmo
// ponto (CentroAcimaDaBase).
//
// Existe para diagnóstico: quando a pílula não aparece, saber que ela
// foi criada em (896, 994) numa tela de 1920x1080 separa "a janela não
// foi desenhada" de "a janela está num monitor que você não está
// olhando" — e o segundo caso é invisível em qualquer outro log.
func (o *Overlay) Geometria() (x, y, w, h, telaW, telaH int32) {
	g := o.geo
	return g.X, g.Y, g.W, g.H, g.TelaW, g.TelaH
}

// amostraOnda é uma amplitude com o instante em que chegou — é o
// carimbo que permite descartar por IDADE em vez de por contagem (ver
// JanelaDaOnda).
type amostraOnda struct {
	rms float64
	at  time.Time
}

func (o *Overlay) relogio() time.Time {
	if o.agora != nil {
		return o.agora()
	}
	return time.Now()
}

// recentes devolve as amplitudes dentro de JanelaDaOnda contadas a
// partir de ref, da mais antiga para a mais nova — a ordem que Bars
// espera. Chamador precisa segurar o.mu.
func (o *Overlay) recentes(ref time.Time) []float64 {
	corte := ref.Add(-JanelaDaOnda)
	out := make([]float64, 0, len(o.amps))
	for _, a := range o.amps {
		if a.at.After(corte) {
			out = append(out, a.rms)
		}
	}
	return out
}

// New cria o overlay: registra a classe de janela, cria a janela
// layered escondida e devolve assim que a goroutine dedicada termina de
// inicializar — ou devolve o erro de inicialização (por exemplo
// ErrNotSupported fora do Windows). Deve ser chamado uma única vez por
// processo, na inicialização do Agent (ver window_windows.go).
func New() (*Overlay, error) {
	o := &Overlay{done: make(chan struct{})}

	ready := make(chan error, 1)
	go o.run(ready)

	if err := <-ready; err != nil {
		return nil, err
	}
	return o, nil
}

// run é a única goroutine que toca HWND e handle de GDI do início ao
// fim: cria a janela, bloqueia em GetMessageW/DispatchMessageW até
// pedirem para sair (RequestQuit -> msgQuit -> PostQuitMessage,
// disparado de dentro de wndProc, na própria thread — a única forma
// válida, já que PostQuitMessage sempre posta na fila da thread
// CHAMADORA), e só então destrói a janela.
func (o *Overlay) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(o.done)

	screenW := win32.GetSystemMetrics(win32.SMCxScreen)
	screenH := win32.GetSystemMetrics(win32.SMCyScreen)
	// A janela nasce com a geometria da pílula compacta, o primeiro quadro
	// de todo ditado. Cada redesenho a move e redimensiona para a pílula da
	// fase (redraw); a superfície de desenho já nasce no tamanho da maior.
	d := DimensoesPara(dictation.OverlayRecording)
	w, h := int32(d.W), int32(d.H)
	x, y := Position(screenW, screenH, w, h, CentroAcimaDaBase)

	// Guardado para Geometria() e para redraw: quando o overlay não
	// aparece, a primeira pergunta é ONDE ele foi posto. GetSystemMetrics
	// devolve só a tela PRIMÁRIA — num setup de dois monitores a pílula vai
	// para o primário, que pode não ser onde a pessoa está olhando — e
	// devolve coordenada virtualizada se o processo não for DPI-aware.
	o.geo = geometria{X: x, Y: y, W: w, H: h, TelaW: screenW, TelaH: screenH}

	supW, supH := TamanhoDaSuperficie()
	win, err := newWindow(x, y, w, h, int32(supW), int32(supH), o.wndProc)
	if err != nil {
		ready <- err
		return
	}
	o.win = win
	ready <- nil

	var msg win32.MSG
	for {
		n, err := win32.GetMessageW(&msg, 0, 0, 0)
		if n <= 0 || err != nil {
			break // WM_QUIT (n==0) ou erro: sai do loop
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessageW(&msg)
	}

	o.win.Close()
}

// wndProc roda SEMPRE na goroutine de run() — é DispatchMessageW quem
// chama isto, nunca outra goroutine — e é por isso o único lugar deste
// pacote, fora de run()/newWindow/Close, autorizado a acionar Draw ou
// SetVisible.
func (o *Overlay) wndProc(hwnd win32.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case msgUpdate:
		o.redraw()
		return 0
	case msgQuit:
		win32.PostQuitMessage(0)
		return 0
	case wmClose:
		// Nunca vai ao DefWindowProc, que destruía a pílula: ela sumia pelo
		// resto da sessão, e no encerramento o Close falhava no HWND morto,
		// sem esperar a thread, e o Agent registrava "overlay: Close falhou"
		// — um erro que não existia (revisão de 29-set-2026). A janela daqui
		// só vai embora pelo Close do próprio Agent.
		//
		// Mas também não pode ser engolido: é por aqui que o "taskkill" sem
		// /F pede para fechar o Agent — ver AoPedirFechar.
		o.pedirFechar()
		return 0
	default:
		return defWindowProc(hwnd, msg, wParam, lParam)
	}
}

func (o *Overlay) redraw() {
	o.mu.Lock()
	agora := o.relogio()
	state := FrameState{Phase: o.phase, Amps: o.recentes(agora), Texto: o.texto}
	o.mu.Unlock()

	if !Visible(state.Phase) {
		o.win.SetVisible(false)
		return
	}

	// O quadro tem o tamanho da pílula da fase, e a janela vai junto: com
	// o centro sempre no mesmo ponto da tela (CentroAcimaDaBase), a pílula
	// cresce e encolhe em volta dele. Posição, tamanho e conteúdo mudam
	// numa chamada só (window.Draw), e só DEPOIS a janela é mostrada — uma
	// janela escondida que reaparece nunca mostra o quadro anterior.
	canvas := Quadro(state)
	x, y := Position(o.geo.TelaW, o.geo.TelaH, int32(canvas.W), int32(canvas.H), CentroAcimaDaBase)
	if err := o.win.Draw(canvas, x, y); err != nil {
		// Best-effort: uma falha de apresentação não deve derrubar o
		// loop de mensagens nem a tentativa de ditado em andamento —
		// o overlay é feedback visual, não o caminho crítico do ADR-007.
		//
		// Mas engolir sem dizer nada foi um erro: quando o overlay
		// parou de aparecer em 26-set-2026, este caminho era
		// indistinguível de "a janela apareceu e você não viu", porque
		// neste return o SetVisible(true) nem chega a ser chamado.
		//
		// O relato é uma única vez por processo: UpdateLayeredWindow
		// falha a ~30 fps enquanto a causa existir, e um log por frame
		// afogaria o terminal.
		o.reportarFalhaDeDesenho(err)
		return
	}
	o.win.SetVisible(true)
}

// SetPhase atualiza a fase mostrada pelo overlay — o runtime chama isto
// ao executar um dictation.ShowOverlayEffect. Também arma (fases
// terminais) ou cancela (recording/hidden) o timer de auto-esconder —
// ver autoHideDelay.
func (o *Overlay) SetPhase(phase dictation.OverlayPhase) {
	o.SetPhaseComTexto(phase, "")
}

// SetPhaseComTexto é SetPhase com o texto da mensagem no lugar do Label da
// fase: "Lista de compras +3" no ditar para nota (FaseNaNota). Texto vazio é o
// Label. O texto vale até a próxima fase.
func (o *Overlay) SetPhaseComTexto(phase dictation.OverlayPhase, texto string) {
	o.mu.Lock()
	o.phase = phase
	o.texto = texto
	if phase == dictation.OverlayRecording {
		o.amps = o.amps[:0]
	}
	if o.hideTimer != nil {
		o.hideTimer.Stop()
		o.hideTimer = nil
	}
	if isTerminalPhase(phase) {
		o.hideTimer = time.AfterFunc(autoHideDelay, func() {
			o.SetPhase(dictation.OverlayHidden)
		})
	}
	o.mu.Unlock()
	o.wake()
}

// PushAmplitude alimenta a onda ao vivo durante a gravação —
// best-effort, do mesmo jeito que audio.Session.Frames() é best-effort
// (ADR-007, seção 4): o runtime encaminha aqui o audio.Frame.RMS de
// cada frame recebido, já normalizado em [0,1]. Fora dessa faixa é
// apenas recortado por Bars (layout.go) ao desenhar, nunca aqui.
//
// O descarte é por IDADE (JanelaDaOnda); maxAmplitudeSamples é só um
// teto de memória para dispositivo de período muito curto.
// Fora da gravação, a amostra é DESCARTADA e nenhum redesenho é
// acordado.
//
// A captura de áudio NÃO para quando a tecla é solta —
// audio.Session.MarkStart/Stop só anotam o corte no ring buffer, e o laço
// do dispositivo segue lendo (session.go): no "sempre pronto" o tempo
// todo, e no padrão, sob demanda (ADR-015), pelo menos até o fim do Stop,
// que espera a cauda antes de estacionar o microfone. E o runtime
// encaminha todo frame para cá sem olhar a fase. Sem esta porta, durante o
// processamento a onda seguia sendo alimentada com áudio que NÃO está
// na transcrição — respiração, teclado, a sala — e cada frame acordava
// um redesenho, uns 66 deles num ASR de 665 ms, todos produzindo o
// mesmo quadro.
func (o *Overlay) PushAmplitude(amplitude float64) {
	o.mu.Lock()
	if o.phase != dictation.OverlayRecording {
		o.mu.Unlock()
		return
	}
	agora := o.relogio()
	o.amps = append(o.amps, amostraOnda{rms: amplitude, at: agora})

	corte := agora.Add(-JanelaDaOnda)
	descarte := 0
	for descarte < len(o.amps) && !o.amps[descarte].at.After(corte) {
		descarte++
	}
	if descarte > 0 {
		o.amps = append(o.amps[:0], o.amps[descarte:]...)
	}
	if len(o.amps) > maxAmplitudeSamples {
		o.amps = append(o.amps[:0], o.amps[len(o.amps)-maxAmplitudeSamples:]...)
	}
	o.mu.Unlock()
	o.wake()
}

// AoFalharDesenho registra um observador chamado na PRIMEIRA vez que
// apresentar um frame falhar. O Agent liga o logger aqui; sem
// observador, a falha continua silenciosa e o overlay segue
// best-effort, como sempre foi.
//
// Nil desliga. Só pode ser chamado antes de a janela começar a
// desenhar — na prática, logo depois de New.
func (o *Overlay) AoFalharDesenho(f func(error)) {
	o.mu.Lock()
	o.aoFalharDesenho = f
	o.mu.Unlock()
}

// AoPedirFechar registra quem recebe o pedido de fechar que chega de fora
// à pílula (WM_CLOSE). O Agent liga aqui o mesmo encerramento do item
// Sair da bandeja.
//
// # Por que a pílula, e não a bandeja
//
// "taskkill" sem /F manda UM WM_CLOSE, e não um a cada janela do
// processo: a primeira janela de nível superior sem dono na ordem do
// EnumWindows, com preferência para as visíveis (desmontagem do
// taskkill.exe do Windows 11 26200, registrada na revisão de
// 29-set-2026; a Microsoft não documenta isso). A pílula é sem dono e
// topmost (style.go), e janela topmost vem antes das outras nessa ordem,
// visível ou não — então o pedido cai nela, e não na janela oculta da
// bandeja.
//
// Até a revisão de 29-set-2026 a pílula repassava o WM_CLOSE ao
// DefWindowProc, que a destruía e deixava o Agent rodando sem ela. A
// primeira correção passou a engoli-lo, e o taskkill ficou sem efeito
// nenhum, imprimindo SUCCESS.
//
// Pode ser registrado a qualquer momento: um pedido que chegou antes,
// enquanto o Agent ainda subia, é entregue aqui mesmo, na hora do
// registro, em vez de perdido. Então f roda fora de o.mu, na goroutine
// da janela ou na de quem registra — precisa ser segura entre threads.
// Nil desliga.
func (o *Overlay) AoPedirFechar(f func()) {
	o.mu.Lock()
	o.aoPedirFechar = f
	pendente := f != nil && o.fecharPendente
	if pendente {
		o.fecharPendente = false
	}
	o.mu.Unlock()

	if pendente {
		f()
	}
}

// pedirFechar entrega o WM_CLOSE a quem AoPedirFechar registrou, ou o
// guarda até o registro.
func (o *Overlay) pedirFechar() {
	o.mu.Lock()
	f := o.aoPedirFechar
	if f == nil {
		o.fecharPendente = true
	}
	o.mu.Unlock()

	if f != nil {
		f()
	}
}

func (o *Overlay) reportarFalhaDeDesenho(err error) {
	o.mu.Lock()
	f := o.aoFalharDesenho
	jaAvisou := o.avisouFalhaDeDesenho
	o.avisouFalhaDeDesenho = true
	o.mu.Unlock()

	if f != nil && !jaAvisou {
		f(err)
	}
}

func (o *Overlay) wake() {
	if o.win == nil {
		return
	}
	_ = o.win.Wake() // best-effort: se a janela já estiver fechando, não há o que fazer
}

// Close pede o encerramento da janela e espera o loop de mensagens
// terminar. Depois de Close, o Overlay não deve mais ser usado.
func (o *Overlay) Close() error {
	o.mu.Lock()
	if o.hideTimer != nil {
		o.hideTimer.Stop()
		o.hideTimer = nil
	}
	o.mu.Unlock()

	if o.win != nil {
		if err := o.win.RequestQuit(); err != nil {
			return err
		}
	}
	<-o.done
	return nil
}
