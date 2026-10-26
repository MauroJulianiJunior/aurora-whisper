package overlay

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestNaNota_TextoNoLugarDoLabel: a pílula do ditar para nota diz para onde o
// texto foi ("Lista de compras +3"), na mesma composição das outras mensagens.
func TestNaNota_TextoNoLugarDoLabel(t *testing.T) {
	m := ComporMensagemComTexto(FaseNaNota, "Lista de compras +3")
	if strings.Join(m.Linhas, "|") != "Lista de compras +3" {
		t.Fatalf("linhas %q", m.Linhas)
	}
	if !m.TemSinal || m.Sinal != corVerde {
		t.Fatalf("ponto %v %v, quer verde", m.Sinal, m.TemSinal)
	}
	// Sem texto, o Label.
	if got := ComporMensagemComTexto(FaseNaNota, "").Linhas; strings.Join(got, "|") != Label(FaseNaNota) {
		t.Fatalf("sem texto: %q", got)
	}
}

// TestNaNota_TituloLongoCabe: um texto que não cabe em duas linhas é cortado
// com "…", sem sair da pílula nem da superfície da janela.
func TestNaNota_TituloLongoCabe(t *testing.T) {
	longo := strings.Repeat("palavrinha ", 20) + "+4"
	m := ComporMensagemComTexto(FaseNaNota, longo)
	if len(m.Linhas) > MaxLinhas {
		t.Fatalf("%d linhas", len(m.Linhas))
	}
	supW, supH := TamanhoDaSuperficie()
	if m.Dim.W > supW || m.Dim.H > supH {
		t.Fatalf("pílula %dx%d maior que a superfície %dx%d", m.Dim.W, m.Dim.H, supW, supH)
	}
	for _, l := range m.Linhas {
		if tipo().largura(l) > larguraUtil(true) {
			t.Fatalf("linha %q sai da pílula", l)
		}
	}
}

// TestSetPhaseComTexto_ValeSoParaAFaseQueVeioComEle: o quadro desenhado tem o
// tamanho da mensagem com o texto, e a fase seguinte (SetPhase) volta ao Label.
func TestSetPhaseComTexto_ValeSoParaAFaseQueVeioComEle(t *testing.T) {
	j := &janelaDeTeste{}
	o := &Overlay{win: j, geo: geometria{TelaW: 1920, TelaH: 1080}}
	defer func() {
		o.mu.Lock()
		if o.hideTimer != nil {
			o.hideTimer.Stop()
		}
		o.mu.Unlock()
	}()

	texto := "Ideias para o roteiro +2"
	o.SetPhaseComTexto(FaseNaNota, texto)
	o.redraw()
	q := j.quadros[len(j.quadros)-1]
	if d := ComporMensagemComTexto(FaseNaNota, texto).Dim; q.c.W != d.W || q.c.H != d.H {
		t.Fatalf("quadro %dx%d, quer a pílula do texto %dx%d", q.c.W, q.c.H, d.W, d.H)
	}
	if d := DimensoesPara(FaseNaNota); d.W == q.c.W {
		t.Fatal("o quadro saiu do tamanho do Label: o texto não chegou ao desenho")
	}

	o.SetPhase(dictation.OverlaySuccess)
	o.mu.Lock()
	sobrou := o.texto
	o.mu.Unlock()
	if sobrou != "" {
		t.Fatalf("o texto %q ficou para a fase seguinte", sobrou)
	}
}
