package overlay

import (
	"runtime"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// dubleDefWindowProc põe no lugar do DefWindowProc um dublê que só anota
// o que recebeu. Nenhuma mensagem destes testes chega ao Windows.
func dubleDefWindowProc(t *testing.T) *[]uint32 {
	t.Helper()
	var repassadas []uint32
	original := defWindowProc
	defWindowProc = func(_ win32.HWND, msg uint32, _, _ uintptr) uintptr {
		repassadas = append(repassadas, msg)
		return 0
	}
	t.Cleanup(func() { defWindowProc = original })
	return &repassadas
}

// TestWndProc_WMCloseNaoDestroiAPilula trava o defeito da revisão de
// 29-set-2026: o WM_CLOSE do taskkill chegava ao DefWindowProc, que
// destruía a pílula; depois o Close do Agent falhava no HWND morto e
// registrava um erro falso.
//
// O wndProc é chamado direto, com hwnd zero e um Overlay sem janela: no
// caminho do WM_CLOSE ele não faz chamada de sistema nenhuma. Aqui ainda
// não há quem receba o pedido (ver AoPedirFechar).
func TestWndProc_WMCloseNaoDestroiAPilula(t *testing.T) {
	repassadas := dubleDefWindowProc(t)
	o := &Overlay{}
	if r := o.wndProc(0, wmClose, 0, 0); r != 0 {
		t.Errorf("wndProc(WM_CLOSE) = %d, quer 0", r)
	}
	if len(*repassadas) != 0 {
		t.Errorf("WM_CLOSE chegou ao DefWindowProc, que destrói a pílula: %v", *repassadas)
	}
}

// TestWndProc_WMCloseRepassaOPedidoDeFechar trava o segundo defeito da
// revisão de 29-set-2026: o "taskkill" sem /F manda um WM_CLOSE só, e ele
// cai na pílula, não na bandeja. Engolido aqui, o taskkill imprimia
// SUCCESS e o Agent seguia rodando.
func TestWndProc_WMCloseRepassaOPedidoDeFechar(t *testing.T) {
	repassadas := dubleDefWindowProc(t)
	pedidos := 0
	o := &Overlay{}
	o.AoPedirFechar(func() { pedidos++ })

	if r := o.wndProc(0, wmClose, 0, 0); r != 0 {
		t.Errorf("wndProc(WM_CLOSE) = %d, quer 0", r)
	}
	if pedidos != 1 {
		t.Errorf("WM_CLOSE na pílula chegou %d vezes a quem encerra o Agent, quer 1", pedidos)
	}
	if len(*repassadas) != 0 {
		t.Errorf("WM_CLOSE chegou ao DefWindowProc, que destrói a pílula: %v", *repassadas)
	}
}

// TestAoPedirFechar_PedidoDaSubidaNaoSePerde: o WM_CLOSE pode chegar
// enquanto o Agent ainda sobe (o warmup do ASR leva segundos), antes de
// existir a quem entregá-lo. Ele fica guardado e é entregue no registro —
// uma vez só.
func TestAoPedirFechar_PedidoDaSubidaNaoSePerde(t *testing.T) {
	dubleDefWindowProc(t)
	o := &Overlay{}
	o.wndProc(0, wmClose, 0, 0)

	pedidos := 0
	o.AoPedirFechar(func() { pedidos++ })
	if pedidos != 1 {
		t.Fatalf("o taskkill da subida chegou %d vezes, quer 1", pedidos)
	}
	o.AoPedirFechar(func() { pedidos++ })
	if pedidos != 1 {
		t.Errorf("o mesmo pedido foi entregue de novo num segundo registro: %d", pedidos)
	}
}

// TestAoPedirFechar_SemPedidoNaoChama: registrar não é pedir para fechar.
func TestAoPedirFechar_SemPedidoNaoChama(t *testing.T) {
	o := &Overlay{}
	o.AoPedirFechar(func() { t.Error("o registro chamou o encerramento sem WM_CLOSE nenhum") })
}

// TestWndProc_OutrasMensagensSeguemParaOWindows: só o WM_CLOSE é desviado.
func TestWndProc_OutrasMensagensSeguemParaOWindows(t *testing.T) {
	repassadas := dubleDefWindowProc(t)
	const wmNcHitTest = 0x0084
	(&Overlay{}).wndProc(0, wmNcHitTest, 0, 0)
	if len(*repassadas) != 1 || (*repassadas)[0] != wmNcHitTest {
		t.Errorf("WM_NCHITTEST não chegou ao DefWindowProc: %v", *repassadas)
	}
}

// janelaDeTeste é um dublê da fronteira de plataforma (window): anota o
// que o overlay pediu, na ordem, sem Win32 nenhum.
type janelaDeTeste struct {
	eventos []string
	quadros []quadroApresentado
}

type quadroApresentado struct {
	x, y int32
	c    *Canvas
}

func (j *janelaDeTeste) Draw(c *Canvas, x, y int32) error {
	j.eventos = append(j.eventos, "desenha")
	j.quadros = append(j.quadros, quadroApresentado{x: x, y: y, c: c})
	return nil
}

func (j *janelaDeTeste) SetVisible(v bool) error {
	if v {
		j.eventos = append(j.eventos, "mostra")
	} else {
		j.eventos = append(j.eventos, "esconde")
	}
	return nil
}

func (j *janelaDeTeste) Wake() error        { return nil }
func (j *janelaDeTeste) RequestQuit() error { return nil }
func (j *janelaDeTeste) Close() error       { return nil }

// redesenhaEm põe o overlay na fase p e roda o redesenho que a thread da
// janela rodaria — sem SetPhase, para não armar o timer de esconder.
func redesenhaEm(o *Overlay, p dictation.OverlayPhase) {
	o.mu.Lock()
	o.phase = p
	o.mu.Unlock()
	o.redraw()
}

// TestRedraw_JanelaAcompanhaAPilula: a cada fase, a janela recebe o quadro
// do tamanho da pílula dela, na posição que põe o centro da pílula sempre
// no mesmo ponto — centrado na tela, CentroAcimaDaBase acima da borda — e
// só é mostrada DEPOIS de o quadro novo estar nela. Esconder não desenha.
//
// É a parte do redimensionamento que dá para testar sem Windows: o que
// window_windows.go recebe. Lá, posição, tamanho e conteúdo vão juntos no
// mesmo UpdateLayeredWindow.
func TestRedraw_JanelaAcompanhaAPilula(t *testing.T) {
	const telaW, telaH = 1920, 1080
	j := &janelaDeTeste{}
	o := &Overlay{win: j, geo: geometria{TelaW: telaW, TelaH: telaH}}

	fases := []dictation.OverlayPhase{
		dictation.OverlayRecording, dictation.OverlayProcessing, dictation.OverlaySuccess,
		dictation.OverlayRecording, dictation.OverlayError, dictation.OverlayInsertFailed,
		dictation.OverlayClipboard, FaseJanelaElevada, dictation.OverlayBlocked,
		FaseTextoNoHistorico, FaseRepeticaoRemovida,
	}
	for i, p := range fases {
		redesenhaEm(o, p)
		if got := strings.Join(j.eventos[2*i:], ","); got != "desenha,mostra" {
			t.Fatalf("%s: a janela recebeu %q, want \"desenha,mostra\" — mostrar antes de desenhar "+
				"expõe o quadro da fase anterior", nomeDaFase(p), got)
		}
		q := j.quadros[i]
		d := DimensoesPara(p)
		if q.c.W != d.W || q.c.H != d.H {
			t.Errorf("%s: quadro %dx%d, want a pílula %dx%d", nomeDaFase(p), q.c.W, q.c.H, d.W, d.H)
		}
		// O centro da pílula fica no mesmo ponto em toda fase (meio pixel
		// de arredondamento em cada eixo).
		cx2, cy2 := 2*q.x+int32(q.c.W), 2*q.y+int32(q.c.H)
		if cx2 < telaW-1 || cx2 > telaW+1 || cy2 != 2*(telaH-CentroAcimaDaBase) {
			t.Errorf("%s: pílula %dx%d em (%d,%d), centro em (%.1f,%.1f); want (%d,%d)", nomeDaFase(p),
				q.c.W, q.c.H, q.x, q.y, float64(cx2)/2, float64(cy2)/2, telaW/2, telaH-CentroAcimaDaBase)
		}
	}

	antes := len(j.quadros)
	redesenhaEm(o, dictation.OverlayHidden)
	if len(j.quadros) != antes || j.eventos[len(j.eventos)-1] != "esconde" {
		t.Errorf("esconder: eventos %q — want só \"esconde\", sem quadro novo", j.eventos[2*antes:])
	}
}

// New() cria uma janela nativa de verdade no Windows (ADR-010, D2) —
// não é exercitada aqui, nem no Windows: "o desenho e a janela ficam
// atrás de //go:build windows" (ver CLAUDE.md deste sprint), validados
// manualmente. O único comportamento cross-platform testável
// automaticamente é a falha limpa fora do Windows.
func TestNew_ErrNotSupportedForaDoWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("New() cria uma janela nativa de verdade no Windows; verificação manual, não automatizada")
	}
	o, err := New()
	if err != ErrNotSupported {
		t.Fatalf("New() err = %v, want ErrNotSupported", err)
	}
	if o != nil {
		t.Fatalf("New() overlay = %v, want nil quando err != nil", o)
	}
}
