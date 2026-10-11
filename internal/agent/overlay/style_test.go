package overlay

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/win32"
)

// TestOverlayExStyle_EhTopmost é o teste que faltava em 26-set-2026.
//
// A pílula era criada, desenhada e mostrada — os logs provavam os três —
// e ficava ATRÁS da janela em foco, invisível. Faltava um bit:
// WS_EX_TOPMOST. Com WS_EX_NOACTIVATE e SW_SHOWNOACTIVATE, a janela
// nunca sobe sozinha na pilha.
//
// O defeito sobreviveu a dois roteiros de teste manual porque depende da
// sorte: quando o centro inferior da tela está descoberto, a pílula
// aparece do mesmo jeito.
func TestOverlayExStyle_EhTopmost(t *testing.T) {
	if overlayExStyle&win32.WSExTopmost == 0 {
		t.Error("a janela do overlay não é topmost — a pílula fica atrás da janela " +
			"em foco, que é exatamente onde a pessoa está olhando")
	}
}

// TestOverlayExStyle_NuncaRoubaOFoco trava o bit mais perigoso deste
// conjunto.
//
// O ditado insere texto no campo que TEM o foco de teclado. Um overlay
// que ativasse a si mesmo destruiria o alvo da inserção — o texto iria
// para lugar nenhum, ou pior, para a janela errada.
func TestOverlayExStyle_NuncaRoubaOFoco(t *testing.T) {
	if overlayExStyle&win32.WSExNoActivate == 0 {
		t.Fatal("falta WS_EX_NOACTIVATE: o overlay roubaria o foco do campo " +
			"em que o texto ditado seria inserido")
	}
}

// TestOverlayExStyle_NaoRoubaClique: a pílula cobre o centro inferior da
// tela durante o ditado. Sem WS_EX_TRANSPARENT ela engoliria cliques
// numa área que a pessoa usa.
func TestOverlayExStyle_NaoRoubaClique(t *testing.T) {
	if overlayExStyle&win32.WSExTransparent == 0 {
		t.Error("falta WS_EX_TRANSPARENT: a pílula engoliria cliques")
	}
}

// TestOverlayExStyle_ExigidoPorUpdateLayeredWindow: sem WS_EX_LAYERED a
// chamada falha, e o overlay fica invisível — o mesmo sintoma do bug
// deste ADR, por outra causa.
func TestOverlayExStyle_ExigidoPorUpdateLayeredWindow(t *testing.T) {
	if overlayExStyle&win32.WSExLayered == 0 {
		t.Error("falta WS_EX_LAYERED: UpdateLayeredWindow falharia e nada apareceria")
	}
}

// TestOverlayExStyle_ForaDoAltTab: é um HUD transitório, não uma janela
// de aplicativo. Aparecer no Alt+Tab e na barra de tarefas seria ruído.
func TestOverlayExStyle_ForaDoAltTab(t *testing.T) {
	if overlayExStyle&win32.WSExToolWindow == 0 {
		t.Error("falta WS_EX_TOOLWINDOW: o overlay apareceria no Alt+Tab e na barra de tarefas")
	}
}

// ---------------------------------------------------------------------------
// Mostrar a pílula: sem ativar, reafirmando o topo (30-set-2026)
// ---------------------------------------------------------------------------
//
// Mauro relatou a pílula não aparecendo sobre uma janela em primeiro plano, num
// build antigo; no novo apareceu. A defesa barata contra outra janela topmost
// que suba depois é reafirmar o topo (SetWindowPos com HWND_TOPMOST) a cada vez
// que a pílula é mostrada. O que não pode ceder é o NoActivate: o ditado insere
// no campo que TEM o foco. Os valores abaixo são os de winuser.h, escritos de
// novo e à mão, e não lidos das constantes do pacote: se alguém trocar uma
// constante lá, estes testes precisam discordar.

// TestComoMostrar_NuncaAtiva: nem o ShowWindow nem o SetWindowPos podem ativar a
// pílula. Sem SWP_NOACTIVATE a reafirmação do topo roubaria o foco do campo de
// texto; com SW_SHOW ou SW_SHOWNORMAL no lugar de SW_SHOWNOACTIVATE, também.
func TestComoMostrar_NuncaAtiva(t *testing.T) {
	m := comoMostrar()
	if m.CmdShow != 4 { // SW_SHOWNOACTIVATE
		t.Errorf("cmdShow = %d, quero 4 (SW_SHOWNOACTIVATE): qualquer outro valor (SW_SHOW=5, SW_SHOWNORMAL=1...) ativaria a pílula e tiraria o foco do campo de texto", m.CmdShow)
	}
	if m.Flags&0x0010 == 0 { // SWP_NOACTIVATE
		t.Error("faltou SWP_NOACTIVATE: SetWindowPos ativaria a pílula e o ditado perderia o campo em que insere o texto")
	}
}

// TestComoMostrar_ReafirmaOTopo: a chamada tem de poder fazer o que existe para
// fazer. HWND_TOPMOST como insertAfter, e SEM SWP_NOZORDER, que faz o
// SetWindowPos ignorar o insertAfter e transforma a reafirmação numa chamada
// vazia (sem sintoma nenhum: o teste manual passaria quando nada cobrisse a pílula).
func TestComoMostrar_ReafirmaOTopo(t *testing.T) {
	m := comoMostrar()
	if m.InsertAfter != win32.HWND(^uintptr(0)) { // HWND_TOPMOST = (HWND)-1
		t.Errorf("insertAfter = %#x, quero HWND_TOPMOST ((HWND)-1)", uintptr(m.InsertAfter))
	}
	if m.Flags&0x0004 != 0 { // SWP_NOZORDER
		t.Error("tem SWP_NOZORDER: o SetWindowPos ignora o HWND_TOPMOST e não reafirma nada")
	}
}

// TestComoMostrar_NaoMoveNemRedimensiona: posição e tamanho são do
// UpdateLayeredWindow, no mesmo quadro. Um SetWindowPos que os tocasse brigaria
// com ele (e com cx e cy zerados, deixaria a janela com tamanho zero).
func TestComoMostrar_NaoMoveNemRedimensiona(t *testing.T) {
	m := comoMostrar()
	if m.Flags&0x0001 == 0 { // SWP_NOSIZE
		t.Error("faltou SWP_NOSIZE: com cx e cy zerados a pílula ficaria com tamanho zero")
	}
	if m.Flags&0x0002 == 0 { // SWP_NOMOVE
		t.Error("faltou SWP_NOMOVE: com x e y zerados a pílula iria para o canto da tela")
	}
}

// TestComoMostrar_MostraSemEsconderENaoMexeNaDona: SWP_SHOWWINDOW mostra, e
// SWP_HIDEWINDOW esconderia a pílula que acabou de ser mostrada. A pílula não
// tem dona; SWP_NOOWNERZORDER só diz que não há ordem de dona a mexer.
func TestComoMostrar_MostraSemEsconderENaoMexeNaDona(t *testing.T) {
	m := comoMostrar()
	if m.Flags&0x0040 == 0 { // SWP_SHOWWINDOW
		t.Error("faltou SWP_SHOWWINDOW")
	}
	if m.Flags&0x0080 != 0 { // SWP_HIDEWINDOW
		t.Error("tem SWP_HIDEWINDOW: esconderia a pílula no mesmo instante em que ela é mostrada")
	}
	if m.Flags&0x0200 == 0 { // SWP_NOOWNERZORDER
		t.Error("faltou SWP_NOOWNERZORDER")
	}
	if want := uint32(0x0001 | 0x0002 | 0x0010 | 0x0040 | 0x0200); m.Flags != want {
		t.Errorf("flags = %#x, quero exatamente NOSIZE|NOMOVE|NOACTIVATE|SHOWWINDOW|NOOWNERZORDER (%#x): flag a mais é comportamento não revisado", m.Flags, want)
	}
}

// TestFlagsDoSetWindowPos_SaoOsDoWinuserH: as constantes de win32 são valores
// do Windows, não do pacote. Um valor trocado (SWP_NOACTIVATE por SWP_FRAMECHANGED,
// por exemplo) passaria nos testes acima se eles lessem a constante.
func TestFlagsDoSetWindowPos_SaoOsDoWinuserH(t *testing.T) {
	for _, c := range []struct {
		nome string
		got  uint32
		quer uint32
	}{
		{"SWP_NOSIZE", win32.SWPNoSize, 0x0001},
		{"SWP_NOMOVE", win32.SWPNoMove, 0x0002},
		{"SWP_NOZORDER", win32.SWPNoZOrder, 0x0004},
		{"SWP_NOACTIVATE", win32.SWPNoActivate, 0x0010},
		{"SWP_SHOWWINDOW", win32.SWPShowWindow, 0x0040},
		{"SWP_HIDEWINDOW", win32.SWPHideWindow, 0x0080},
		{"SWP_NOOWNERZORDER", win32.SWPNoOwnerZOrder, 0x0200},
	} {
		if c.got != c.quer {
			t.Errorf("%s = %#x, quero %#x (winuser.h)", c.nome, c.got, c.quer)
		}
	}
	if win32.HWNDTopmost != win32.HWND(^uintptr(0)) {
		t.Errorf("HWNDTopmost = %#x, quero (HWND)-1", uintptr(win32.HWNDTopmost))
	}
	if swHide != 0 || swShowNoActivate != 4 {
		t.Errorf("swHide = %d, swShowNoActivate = %d; quero 0 e 4 (SW_HIDE, SW_SHOWNOACTIVATE)", swHide, swShowNoActivate)
	}
}

// ---------------------------------------------------------------------------
// A ligação ao Windows, conferida no código
// ---------------------------------------------------------------------------

// TestSetVisible_UsaADecisaoPura: window_windows.go fica atrás de uma build tag
// que nenhum teste da suíte atravessa, e foi assim que um bit faltando passou por
// dois roteiros manuais (26-set-2026). A decisão é pura e testada acima; aqui se
// confere, lendo o código, que SetVisible a USA, e só ela: ShowWindow e
// SetWindowPos com o que comoMostrar devolve, SetWindowPos com x, y, cx e cy
// zerados, e nenhuma outra chamada de ShowWindow ou SetWindowPos no arquivo.
//
// O arquivo só é lido (parser); nada é executado, e nenhuma janela é criada.
func TestSetVisible_UsaADecisaoPura(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "window_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	chamadas := func(raiz ast.Node) []string {
		var out []string
		ast.Inspect(raiz, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			nome := types.ExprString(c.Fun)
			if nome != "win32.ShowWindow" && nome != "win32.SetWindowPos" && nome != "comoMostrar" {
				return true
			}
			args := make([]string, len(c.Args))
			for i, a := range c.Args {
				args[i] = types.ExprString(a)
			}
			out = append(out, nome+"("+strings.Join(args, ", ")+")")
			return true
		})
		return out
	}

	achouSetVisible := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		got := chamadas(fn.Body)
		if fn.Name.Name != "SetVisible" {
			if len(got) > 0 {
				t.Errorf("%s chama %v: só SetVisible mostra ou esconde a pílula, e só pela decisão de comoMostrar", fn.Name.Name, got)
			}
			continue
		}
		achouSetVisible = true

		// A forma: "if !visible { esconde; return }" e, depois, o mostrar. A
		// condição conta: invertida, a pílula esconderia ao ser mostrada.
		if len(fn.Body.List) == 0 {
			t.Fatal("SetVisible está vazio")
		}
		se, ok := fn.Body.List[0].(*ast.IfStmt)
		if !ok || types.ExprString(se.Cond) != "!visible" {
			t.Fatal("SetVisible tem de começar por `if !visible { ... }`: o ramo de esconder")
		}
		if got, want := chamadas(se.Body), []string{"win32.ShowWindow(w.hwnd, swHide)"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("o ramo de esconder faz as chamadas %v, quero %v", got, want)
		}
		var resto []string
		for _, s := range fn.Body.List[1:] {
			resto = append(resto, chamadas(s)...)
		}
		want := []string{
			"comoMostrar()",
			"win32.ShowWindow(w.hwnd, m.CmdShow)",
			"win32.SetWindowPos(w.hwnd, m.InsertAfter, 0, 0, 0, 0, m.Flags)",
		}
		if strings.Join(resto, "\n") != strings.Join(want, "\n") {
			t.Errorf("o ramo de mostrar faz as chamadas\n  %s\nquero\n  %s", strings.Join(resto, "\n  "), strings.Join(want, "\n  "))
		}

		// TODA chamada do corpo, de qualquer função: as quatro de cima e mais
		// nenhuma. A lista filtrada acima só enxerga as três funções que
		// conhece, e uma ligação por outro caminho (como o PostMessageW deste
		// arquivo, que vem de user32.NewProc) a deixaria passar: bastava
		// `procTrazer = user32.NewProc("SetForegroundWindow")` e chamá-la aqui.
		var todas []string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				todas = append(todas, types.ExprString(c.Fun))
			}
			return true
		})
		querTodas := []string{"win32.ShowWindow", "comoMostrar", "win32.ShowWindow", "win32.SetWindowPos"}
		if strings.Join(todas, "\n") != strings.Join(querTodas, "\n") {
			t.Errorf("SetVisible chama %v; só %v: a pílula não pode ativar a janela nem tocar em nada além de mostrar e esconder", todas, querTodas)
		}
	}
	if !achouSetVisible {
		t.Error("window_windows.go não tem SetVisible")
	}
}

// TestOverlay_NadaAtivaAJanela: nenhuma função que ativa janela ou move o foco
// pode aparecer no código do overlay. É o que o NoActivate protege: o ditado
// insere no campo que tem o foco. Olha os identificadores e os literais de
// texto do código, e não os comentários, que explicam justamente por que elas
// não estão aqui. Os literais contam porque uma função do Windows também entra
// por nome (`user32.NewProc("SetForegroundWindow")`, como este pacote faz com o
// PostMessageW), e aí nenhum identificador a carrega.
func TestOverlay_NadaAtivaAJanela(t *testing.T) {
	proibidas := map[string]bool{
		"SetForegroundWindow":      true,
		"SetActiveWindow":          true,
		"SetFocus":                 true,
		"BringWindowToTop":         true,
		"SwitchToThisWindow":       true,
		"AllowSetForegroundWindow": true,
		"AttachThreadInput":        true,
		"SetCapture":               true,
	}
	arquivos, err := filepath.Glob("*.go")
	if err != nil || len(arquivos) == 0 {
		t.Fatalf("não achei os fontes do overlay: %v", err)
	}
	for _, arq := range arquivos {
		if strings.HasSuffix(arq, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, arq, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if proibidas[x.Name] {
					t.Errorf("%s: o overlay usa %s, que ativa janela ou move o foco", fset.Position(x.Pos()), x.Name)
				}
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					break
				}
				// GetProcAddress distingue a caixa, então a comparação é exata.
				if s, err := strconv.Unquote(x.Value); err == nil && proibidas[s] {
					t.Errorf("%s: o overlay liga %q por nome, e ela ativa janela ou move o foco", fset.Position(x.Pos()), s)
				}
			}
			return true
		})
	}
}
