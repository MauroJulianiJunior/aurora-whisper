package winruntime

// Teste white-box (mesmo pacote) que prova a ligação do Sprint 2 entre
// handleRaw e o rastreador de foco: um alvo protegido leva o núcleo a
// StateBlocked (AC-036), que era inalcançável em produção antes desta
// fatia (ver doc.go). Não instala hook nenhum de Windows — handleRaw é
// chamado diretamente, e o Clock é o clock.Fake da Fase 1 (AfterFunc só
// registra o callback, nunca dispara tempo real), então este teste roda
// em qualquer SO.

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
)

// fakeFocus é o dublê de focusProvider usado pelos testes: devolve
// exatamente o dictation.FocusTarget configurado, sem depender do estado
// default de nenhum Tracker real.
type fakeFocus struct {
	target dictation.FocusTarget
}

func (f fakeFocus) Current() dictation.FocusTarget { return f.target }

func testRuntime(t *testing.T, focus focusProvider) *Runtime {
	t.Helper()
	machine, err := dictation.NewMachine(dictation.Config{
		Key:           dictation.KeyF,
		HoldThreshold: 700 * time.Millisecond,
		MaxRecording:  dictation.DefaultMaxRecording,
	})
	if err != nil {
		t.Fatalf("dictation.NewMachine: %v", err)
	}
	return &Runtime{
		machine: machine,
		clk:     clock.NewFake(time.Now()),
		focus:   focus,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		timers:  make(map[timerKey]clock.Timer),
		pending: make(chan dictation.Event, pendingBuffer),
	}
}

// TestHandleRaw_FocoProtegido_LevaAStateBlocked é o teste pedido pela
// tarefa desta fatia: prova que um alvo protegido, lido de focusProvider,
// leva Machine a StateBlocked — AC-036 alcançável em produção pela
// primeira vez.
func TestHandleRaw_FocoProtegido_LevaAStateBlocked(t *testing.T) {
	rt := testRuntime(t, fakeFocus{target: dictation.FocusTarget{
		ProcessName: "notas.exe",
		ControlType: "Edit",
		Protected:   true,
	}})

	rt.handleRaw(dictation.RawKeyEvent{Down: true})

	if got := rt.machine.State(); got != dictation.StateBlocked {
		t.Fatalf("machine.State() = %v, want StateBlocked (foco protegido)", got)
	}
}

// Não há um teste-espelho "foco não protegido -> StatePrimed" neste
// arquivo: esse caminho de onKeyDown emite MarkAudioStartEffect, que
// apply() aplica chamando rt.audio.MarkStart (audio.Session real, sem
// interface) — exercitá-lo aqui exigiria um *audio.Session utilizável
// sem dispositivo de áudio real, o que este pacote não tem como
// construir em teste. O caminho "não protegido" já é coberto pelas
// acceptance suites de internal/agent/dictation (Fase 1, contra
// fakes.Runtime); o que esta fatia precisa provar — e prova acima — é
// que handleRaw efetivamente lê focusProvider.Current() em vez do
// dictation.FocusTarget{} zero fixo do Sprint 1.
