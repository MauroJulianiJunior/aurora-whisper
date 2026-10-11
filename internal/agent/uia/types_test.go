package uia

import "testing"

// TestSnapshot_Protected cobre os ramos que ADR-011, decisão 5, exige
// testar explicitamente: inspeção ok e não-senha, ok e senha (por cada um
// dos dois sinais, isolado e combinado), falhou, timeout, elemento nulo.
// "Na dúvida, protege": qualquer Outcome != InspectOK devolve true,
// independente do resto do Snapshot.
func TestSnapshot_Protected(t *testing.T) {
	tests := []struct {
		name string
		snap Snapshot
		want bool
	}{
		{
			name: "ok e nao-senha",
			snap: Snapshot{Outcome: InspectOK, IsPassword: false, StyleHasPassword: false},
			want: false,
		},
		{
			name: "ok e senha via UIA IsPassword",
			snap: Snapshot{Outcome: InspectOK, IsPassword: true, StyleHasPassword: false},
			want: true,
		},
		{
			name: "ok e senha via fallback ES_PASSWORD",
			snap: Snapshot{Outcome: InspectOK, IsPassword: false, StyleHasPassword: true},
			want: true,
		},
		{
			name: "ok e senha pelos dois sinais",
			snap: Snapshot{Outcome: InspectOK, IsPassword: true, StyleHasPassword: true},
			want: true,
		},
		{
			name: "falhou -> na duvida, protege",
			snap: Snapshot{Outcome: InspectFailed, IsPassword: false, StyleHasPassword: false},
			want: true,
		},
		{
			name: "timeout -> na duvida, protege, mesmo com sinais falsos",
			snap: Snapshot{Outcome: InspectTimeout, IsPassword: false, StyleHasPassword: false},
			want: true,
		},
		{
			name: "timeout com sinal de senha nao muda o resultado (ja protegido)",
			snap: Snapshot{Outcome: InspectTimeout, IsPassword: true},
			want: true,
		},
		{
			name: "elemento nulo -> na duvida, protege",
			snap: Snapshot{Outcome: InspectNilElement},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.snap.Protected(); got != tt.want {
				t.Errorf("Snapshot{Outcome: %v, IsPassword: %v, StyleHasPassword: %v}.Protected() = %v, want %v",
					tt.snap.Outcome, tt.snap.IsPassword, tt.snap.StyleHasPassword, got, tt.want)
			}
		})
	}
}

// TestSnapshot_ToFocusTarget garante que só o que o núcleo contrata
// atravessa a fronteira, e que Protected() é recomputado corretamente
// (não é um campo solto que possa dessincronizar de Outcome/IsPassword).
func TestSnapshot_ToFocusTarget(t *testing.T) {
	snap := Snapshot{
		Outcome:     InspectOK,
		ProcessID:   1234,
		ProcessName: "notepad.exe",
		ControlType: "Edit",
		IsPassword:  true,
	}

	ft := snap.ToFocusTarget()

	if ft.ProcessName != "notepad.exe" {
		t.Errorf("ProcessName = %q, want %q", ft.ProcessName, "notepad.exe")
	}
	if ft.ControlType != "Edit" {
		t.Errorf("ControlType = %q, want %q", ft.ControlType, "Edit")
	}
	if !ft.Protected {
		t.Error("Protected = false, want true (IsPassword era true)")
	}
}

// TestSnapshot_ToFocusTarget_FalhaProtegePorPadrao garante que um
// Snapshot cuja inspeção falhou também vira FocusTarget.Protected == true
// depois de ToFocusTarget — a fronteira não perde a política de segurança.
func TestSnapshot_ToFocusTarget_FalhaProtegePorPadrao(t *testing.T) {
	snap := Snapshot{Outcome: InspectFailed}
	if ft := snap.ToFocusTarget(); !ft.Protected {
		t.Error("ToFocusTarget().Protected = false para InspectFailed, want true")
	}
}

func TestControlTypeName(t *testing.T) {
	tests := []struct {
		id   int32
		want string
	}{
		{50004, "Edit"},
		{50020, "Text"},
		{50032, "Window"},
		{0, "Unknown(0)"},
		{999999, "Unknown(999999)"},
	}
	for _, tt := range tests {
		if got := controlTypeName(tt.id); got != tt.want {
			t.Errorf("controlTypeName(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestInspectOutcome_String(t *testing.T) {
	tests := []struct {
		o    InspectOutcome
		want string
	}{
		{InspectOK, "ok"},
		{InspectFailed, "failed"},
		{InspectTimeout, "timeout"},
		{InspectNilElement, "nil_element"},
		{InspectOutcome(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.o.String(); got != tt.want {
			t.Errorf("InspectOutcome(%d).String() = %q, want %q", tt.o, got, tt.want)
		}
	}
}
