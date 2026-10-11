package uia

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestNewTracker_ComecaProtegido garante o erro seguro descrito em
// NewTracker: antes de qualquer EVENT_OBJECT_FOCUS ser processado, o
// cache já devolve Protected == true, nunca o zero value "falso" de
// dictation.FocusTarget.
func TestNewTracker_ComecaProtegido(t *testing.T) {
	tr := NewTracker(Config{})
	got := tr.Current()
	if !got.Protected {
		t.Errorf("Current() = %+v, want Protected == true antes de qualquer foco observado", got)
	}
}

// TestTracker_StoreCurrent_RoundTrip prova que o cache é O(1) e reflete
// exatamente o último valor publicado — o mecanismo que a thread dedicada
// (tracker_windows.go) usa a cada troca de foco, exercitado aqui sem
// nenhuma chamada de sistema.
func TestTracker_StoreCurrent_RoundTrip(t *testing.T) {
	tr := NewTracker(Config{})

	want := dictation.FocusTarget{
		ProcessName: "chrome.exe",
		ControlType: "Document",
		Protected:   false,
	}
	tr.store(estadoFoco{alvo: want})

	if got := tr.Current(); got != want {
		t.Errorf("Current() = %+v, want %+v", got, want)
	}

	// Uma nova troca de foco substitui o valor anterior por inteiro —
	// nunca acumula estado, nem a marca de janela de administrador.
	next := dictation.FocusTarget{ProcessName: "mstsc.exe", ControlType: "Edit", Protected: true}
	tr.store(estadoFoco{alvo: next, janelaElevada: true})
	if got, elevada := tr.CurrentComElevacao(); got != next || !elevada {
		t.Errorf("CurrentComElevacao() depois do segundo store = %+v, %v; want %+v, true", got, elevada, next)
	}
	tr.store(estadoFoco{alvo: want})
	if _, elevada := tr.CurrentComElevacao(); elevada {
		t.Error("a marca de janela de administrador sobreviveu a um store sem ela")
	}
}

// TestConfig_WithDefaults garante que um InspectBudget não informado (ou
// negativo, por engano de quem monta Config) sempre vira
// defaultInspectBudget, nunca zero — um orçamento zero desativaria por
// acidente a checagem de timeout (ADR-011, decisão 5: timeout é um dos
// ramos obrigatórios de "na dúvida, protege").
func TestConfig_WithDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"zero usa default", 0, defaultInspectBudget},
		{"negativo usa default", -1, defaultInspectBudget},
		{"valor explicito e preservado", 250 * time.Millisecond, 250 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Config{InspectBudget: tt.in}.withDefaults()
			if got.InspectBudget != tt.want {
				t.Errorf("withDefaults().InspectBudget = %v, want %v", got.InspectBudget, tt.want)
			}
		})
	}
}

// TestNewTracker_AplicaDefaults garante que NewTracker já aplica
// Config.withDefaults, e não só armazena o que recebeu.
func TestNewTracker_AplicaDefaults(t *testing.T) {
	tr := NewTracker(Config{})
	if tr.cfg.InspectBudget != defaultInspectBudget {
		t.Errorf("NewTracker(Config{}).cfg.InspectBudget = %v, want %v", tr.cfg.InspectBudget, defaultInspectBudget)
	}
}
