package dictation

import "testing"

// O toque curto só devolve a tecla no gatilho por tecla (AC-001). No Ctrl+Win
// o hook nunca suprime nada, e pedir a devolução só fazia o hook recusar e o
// runtime registrar um WARN a cada toque, inclusive no primeiro dos dois que
// ligam a gravação solta (visto no log do teste manual de 08-nov-2026).

func TestToqueCurto_CombinacaoNaoDevolveTecla(t *testing.T) {
	casos := []struct {
		nome string
		foco FocusTarget
	}{
		{"campo comum", unprotectedFocus()},
		{"campo protegido", FocusTarget{ProcessName: "bank.exe", ControlType: "Password", Protected: true}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			m := maquinaDaCombinacao(t, false)
			m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: c.foco, At: em(0)})
			up := soltar(m, 100, true)
			if got := effectsOfType[SynthesizeTapEffect](up); len(got) != 0 {
				t.Fatalf("toque no Ctrl+Win: efeitos %#v, quer nenhum SynthesizeTapEffect (não há tecla suprimida a devolver)", up)
			}
			if m.State() != StateIdle {
				t.Fatalf("depois do toque: estado %v, quer StateIdle", m.State())
			}
		})
	}
}

// O gatilho por tecla continua devolvendo, nos dois caminhos do toque curto.
func TestToqueCurto_TeclaDevolveATecla(t *testing.T) {
	for _, foco := range []FocusTarget{unprotectedFocus(), {ProcessName: "bank.exe", ControlType: "Password", Protected: true}} {
		m := newTestMachine(t, false)
		m.Handle(KeyDownEvent{Focus: foco})
		up := m.Handle(KeyUpEvent{Attempt: m.CurrentAttempt()})
		if got := effectsOfType[SynthesizeTapEffect](up); len(got) != 1 {
			t.Fatalf("toque na tecla (protegido=%v): efeitos %#v, quer um SynthesizeTapEffect (AC-001)", foco.Protected, up)
		}
	}
}

// O primeiro toque do duplo toque não pede devolução, e o segundo liga a
// gravação solta como antes.
func TestDuploToque_PrimeiroToqueNaoDevolveTecla(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	if up := soltar(m, 100, true); len(effectsOfType[SynthesizeTapEffect](up)) != 0 {
		t.Fatalf("primeiro toque: efeitos %#v, quer nenhum SynthesizeTapEffect", up)
	}
	down := apertar(m, 300)
	if ligou, _ := soltaLigada(down); ligou != 1 {
		t.Fatalf("segundo aperto: efeitos %#v, quer a gravação solta ligada", down)
	}
}
