package dictation

// Esc decide o Esc da gravação solta (ADR-025, B.4): durante ela, o Esc puro
// descarta a gravação, e o app em foco não o recebe. Fora dela, o Esc passa
// como sempre.
//
// Puro como o Decider, e da mesma thread (a do hook): quem chama passa se a
// gravação solta está ligada naquele instante, lido sem lock pelo hook.
//
// # Em par, sempre
//
// O aperto engolido leva junto a soltura e as repetições do mesmo aperto,
// mesmo que a gravação solta acabe no meio — e acaba, porque é o próprio Esc
// que a descarta. Engolir só o aperto deixaria passar as repetições sem a
// soltura, e o Windows ficaria com o Esc preso; engolir só a soltura faria o
// mesmo com o aperto que passou. É a lição do modificador preso de
// 17-set-2026 (ver NewDeciderModificadores), aplicada a uma tecla comum.
//
// Pelo mesmo motivo, um Esc que já estava apertado quando a gravação solta
// começou solta normalmente: o aperto passou, a soltura passa.
//
// Só o Esc sem modificador descarta. O Ctrl+Shift+Esc (o Gerenciador de
// Tarefas) e o Ctrl+Esc (o Iniciar) passam, com ou sem gravação solta.
type Esc struct {
	preso bool
}

// Preso reporta se um Esc engolido ainda não foi solto. Enquanto estiver, a
// soltura dele tem de passar por Decide, com ou sem gravação solta e com ou
// sem pausa: é ela que fecha o par (ver hook.Hook.deveIgnorar).
func (e *Esc) Preso() bool { return e.preso }

// Decide decide um evento do teclado. solta diz se a gravação solta está
// ligada. Notify sai uma vez, no aperto que descarta.
func (e *Esc) Decide(ev RawKeyEvent, solta bool) Decision {
	if ev.Key != KeyEscape || (ev.Injected && ev.FromThisProcess) {
		return passThrough
	}
	if e.preso {
		if !ev.Down {
			e.preso = false
		}
		return Decision{Suppress: true}
	}
	if ev.Down && solta && ev.Modifiers.IsNone() {
		e.preso = true
		return Decision{Suppress: true, Notify: true}
	}
	return passThrough
}
