package dictation

import "testing"

// Os testes do toque limpo (ADR-025, A.2): só a soltura do gatilho por
// combinação leva a marca, e só quando o toque teve só o Ctrl e o Win.

// solturas devolve a marca Limpo de cada soltura notificada da sequência.
func solturas(d interface{ Decide(RawKeyEvent) Decision }, eventos []RawKeyEvent) []bool {
	var out []bool
	for _, e := range eventos {
		got := d.Decide(e)
		if got.Notify && !e.Down {
			out = append(out, got.Limpo)
		}
	}
	return out
}

func TestToqueLimpo_Combinacao(t *testing.T) {
	casos := []struct {
		nome    string
		eventos []RawKeyEvent
		quer    []bool
	}{
		{"Ctrl e Win, soltos", []RawKeyEvent{
			ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
			ev(MetaLeft, ModControl, false), ev(ControlLeft, 0, false),
		}, []bool{true}},
		{"Win primeiro, solta o Ctrl primeiro", []RawKeyEvent{
			ev(MetaLeft, ModMeta, true), ev(ControlLeft, ctrlWin, true),
			ev(ControlLeft, ModMeta, false), ev(MetaLeft, 0, false),
		}, []bool{true}},
		{"a repetição do Win segurado não suja", []RawKeyEvent{
			ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
			ev(MetaLeft, ctrlWin, true), ev(MetaLeft, ctrlWin, true),
			ev(MetaLeft, ModControl, false), ev(ControlLeft, 0, false),
		}, []bool{true}},
		{"Ctrl+Win+seta (KeyUnknown) suja", []RawKeyEvent{
			ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
			ev(KeyUnknown, ctrlWin, true), ev(KeyUnknown, ctrlWin, false),
			ev(MetaLeft, ModControl, false), ev(ControlLeft, 0, false),
		}, []bool{false}},
		{"Ctrl+Win+D suja", []RawKeyEvent{
			ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
			ev(KeyD, ctrlWin, true), ev(KeyD, ctrlWin, false),
			ev(MetaLeft, ModControl, false), ev(ControlLeft, 0, false),
		}, []bool{false}},
		{"Ctrl+Win+Alt suja", []RawKeyEvent{
			ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
			ev(AltLeft, ctrlWinAlt, true), ev(AltLeft, ctrlWin, false),
			ev(MetaLeft, ModControl, false), ev(ControlLeft, 0, false),
		}, []bool{false}},
		{"o Shift segurado antes de armar suja", []RawKeyEvent{
			ev(ShiftLeft, ModShift, true), ev(ControlLeft, ModShift|ModControl, true),
			ev(MetaLeft, ModShift|ctrlWin, true), ev(MetaLeft, ModShift|ModControl, false),
			ev(ControlLeft, ModShift, false), ev(ShiftLeft, 0, false),
		}, []bool{false}},
		{"a soltura de uma tecla apertada antes não suja", []RawKeyEvent{
			ev(KeyA, 0, true), ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
			ev(KeyA, ctrlWin, false),
			ev(MetaLeft, ModControl, false), ev(ControlLeft, 0, false),
		}, []bool{true}},
		{"segurar o Ctrl e tocar o Win duas vezes", []RawKeyEvent{
			ev(ControlLeft, ModControl, true),
			ev(MetaLeft, ctrlWin, true), ev(MetaLeft, ModControl, false),
			ev(ControlLeft, ModControl, true), // repetição do Ctrl, com o conjunto incompleto
			ev(MetaLeft, ctrlWin, true), ev(MetaLeft, ModControl, false),
			ev(ControlLeft, 0, false),
		}, []bool{true, true}},
		{"o toque sujo não suja o seguinte", []RawKeyEvent{
			ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
			ev(KeyUnknown, ctrlWin, true), ev(KeyUnknown, ctrlWin, false),
			ev(MetaLeft, ModControl, false),
			ev(MetaLeft, ctrlWin, true), ev(MetaLeft, ModControl, false),
			ev(ControlLeft, 0, false),
		}, []bool{false, true}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := solturas(NewDeciderModificadores(ctrlWin), c.eventos)
			if len(got) != len(c.quer) {
				t.Fatalf("solturas notificadas %v, quer %v", got, c.quer)
			}
			for i := range got {
				if got[i] != c.quer[i] {
					t.Errorf("soltura %d: Limpo=%v, quer %v", i, got[i], c.quer[i])
				}
			}
		})
	}
}

// TestToqueLimpo_SoNaSoltura: o aperto que arma nunca leva a marca; ela só
// diz algo quando o toque acabou.
func TestToqueLimpo_SoNaSoltura(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)
	d.Decide(ev(ControlLeft, ModControl, true))
	if got := d.Decide(ev(MetaLeft, ctrlWin, true)); !got.Notify || got.Limpo {
		t.Fatalf("o aperto que arma: %+v, quer Notify sem Limpo", got)
	}
}

func TestToqueLimpo_TeclaDeLetraNuncaEhLimpa(t *testing.T) {
	got := solturas(NewDecider(KeyF), []RawKeyEvent{ev(KeyF, 0, true), ev(KeyF, 0, false)})
	if len(got) != 1 || got[0] {
		t.Fatalf("tecla de letra: solturas %v, quer uma, sem Limpo (o duplo toque não existe nela)", got)
	}
}

// TestToqueLimpo_PelosDoisGatilhos: com o gatilho da nota (Ctrl+Win+Alt), o
// Ctrl+Win continua limpo e o da nota nunca é.
func TestToqueLimpo_PelosDoisGatilhos(t *testing.T) {
	g := NovosGatilhos(NewDeciderModificadores(ctrlWin), ctrlWinAlt)
	limpo := solturas(g, []RawKeyEvent{
		ev(ControlLeft, ModControl, true), ev(MetaLeft, ctrlWin, true),
		ev(MetaLeft, ModControl, false), ev(ControlLeft, 0, false),
	})
	if len(limpo) != 1 || !limpo[0] {
		t.Errorf("Ctrl+Win pelos dois gatilhos: %v, quer uma soltura limpa", limpo)
	}
	nota := solturas(g, []RawKeyEvent{
		ev(ControlLeft, ModControl, true), ev(AltLeft, ModControl|ModAlt, true),
		ev(MetaLeft, ctrlWinAlt, true), ev(MetaLeft, ModControl|ModAlt, false),
		ev(AltLeft, ModControl, false), ev(ControlLeft, 0, false),
	})
	if len(nota) != 1 || nota[0] {
		t.Errorf("Ctrl+Win+Alt: %v, quer uma soltura, sem Limpo (a nota fica como está)", nota)
	}
}
