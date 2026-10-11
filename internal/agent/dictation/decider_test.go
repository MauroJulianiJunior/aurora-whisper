package dictation

import "testing"

func TestDecider_InjectedFromThisProcess_AlwaysPassThrough(t *testing.T) {
	d := NewDecider(KeyF)
	ev := RawKeyEvent{Key: KeyF, Down: true, Injected: true, FromThisProcess: true}
	got := d.Decide(ev)
	if got.Suppress || got.Notify {
		t.Errorf("Decide(%+v) = %+v, want PassThrough (never starts a session)", ev, got)
	}

	// Mesmo depois, o keyup correspondente também deve passar: o evento
	// injetado nunca arma o Decider.
	up := RawKeyEvent{Key: KeyF, Down: false, Injected: true, FromThisProcess: true}
	got = d.Decide(up)
	if got.Suppress || got.Notify {
		t.Errorf("Decide(%+v) = %+v, want PassThrough", up, got)
	}
}

func TestDecider_InjectedFromOtherProcess_TreatedAsPhysical(t *testing.T) {
	// Gap conhecido e aceito pela ADR-007, seção 3: evento injetado por
	// outro processo (ex.: AutoHotkey) é tratado como físico.
	d := NewDecider(KeyF)
	ev := RawKeyEvent{Key: KeyF, Down: true, Injected: true, FromThisProcess: false}
	got := d.Decide(ev)
	if !got.Suppress || !got.Notify {
		t.Errorf("Decide(%+v) = %+v, want Suppress+Notify", ev, got)
	}
}

func TestDecider_OtherKey_AlwaysPassThrough(t *testing.T) {
	d := NewDecider(KeyF)
	got := d.Decide(RawKeyEvent{Key: KeyG, Down: true})
	if got.Suppress || got.Notify {
		t.Errorf("Decide(other key down) = %+v, want PassThrough", got)
	}
}

func TestDecider_ConfiguredKeyWithModifiers_BypassesDictation(t *testing.T) {
	// AC-045: tecla com modifier preserva comportamento normal (ex.: Ctrl+F
	// continua abrindo "localizar").
	d := NewDecider(KeyF)
	got := d.Decide(RawKeyEvent{Key: KeyF, Down: true, Modifiers: ModControl})
	if got.Suppress || got.Notify {
		t.Errorf("Decide(Ctrl+F down) = %+v, want PassThrough", got)
	}
}

func TestDecider_ConfiguredKeyNoModifiers_KeyDown_SuppressAndNotify(t *testing.T) {
	// AC-004 caminho de comportamento normal, sessão real: keydown puro da
	// tecla configurada é suprimido e notificado.
	d := NewDecider(KeyF)
	got := d.Decide(RawKeyEvent{Key: KeyF, Down: true})
	if !got.Suppress || !got.Notify {
		t.Errorf("Decide(F down) = %+v, want Suppress+Notify", got)
	}
}

func TestDecider_KeyUp_AfterArmedKeyDown_SuppressAndNotify(t *testing.T) {
	d := NewDecider(KeyF)
	d.Decide(RawKeyEvent{Key: KeyF, Down: true})

	got := d.Decide(RawKeyEvent{Key: KeyF, Down: false})
	if !got.Suppress || !got.Notify {
		t.Errorf("Decide(F up após F down) = %+v, want Suppress+Notify", got)
	}
}

func TestDecider_KeyUp_WithoutArmedKeyDown_PassThrough(t *testing.T) {
	// Ex.: keydown chegou com modificador (bypass, nunca armou), e depois o
	// keyup chega sem modificador. Não deve suprimir algo que nunca foi
	// suprimido.
	d := NewDecider(KeyF)
	d.Decide(RawKeyEvent{Key: KeyF, Down: true, Modifiers: ModControl})

	got := d.Decide(RawKeyEvent{Key: KeyF, Down: false})
	if got.Suppress || got.Notify {
		t.Errorf("Decide(F up sem keydown armado) = %+v, want PassThrough", got)
	}
}

func TestDecider_KeyUp_IsIdempotentAfterConsumed(t *testing.T) {
	d := NewDecider(KeyF)
	d.Decide(RawKeyEvent{Key: KeyF, Down: true})
	d.Decide(RawKeyEvent{Key: KeyF, Down: false})

	// Um segundo keyup (ex.: evento duplicado do SO) não deve ser suprimido
	// de novo, pois já não há keydown armado.
	got := d.Decide(RawKeyEvent{Key: KeyF, Down: false})
	if got.Suppress || got.Notify {
		t.Errorf("Decide(segundo F up) = %+v, want PassThrough", got)
	}
}

func TestDecider_KeyRepeat_KeepsReturningSuppressAndNotify(t *testing.T) {
	// Auto-repeat do SO gera múltiplos keydown enquanto a tecla está
	// segurada; o Decider continua suprimindo/notificando cada um (é o
	// Machine, não o Decider, quem garante idempotência de sessão).
	d := NewDecider(KeyF)
	for i := 0; i < 3; i++ {
		got := d.Decide(RawKeyEvent{Key: KeyF, Down: true})
		if !got.Suppress || !got.Notify {
			t.Fatalf("repeat #%d: Decide(F down) = %+v, want Suppress+Notify", i, got)
		}
	}
}
