package acceptance

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation/fakes"
)

// TestAC001_TapCurtoProduzFuncaoNormal cobre o AC-001: "Tap curto produz
// função normal" (REQ-002). Um toque bem abaixo do threshold sintetiza
// exatamente o caractere original e não cria nenhuma gravação.
func TestAC001_TapCurtoProduzFuncaoNormal(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(120 * time.Millisecond) // bem abaixo do threshold de 700ms
	h.keyUp(attempt)

	if len(h.Runtime.Synthesized) != 1 || h.Runtime.Synthesized[0] != attempt {
		t.Fatalf("synthesized = %v, want exatamente um tap sintetizado para attempt %d", h.Runtime.Synthesized, attempt)
	}
	if h.Audio.Allocated != 0 {
		t.Fatalf("audio.Allocated = %d, want 0 (tap não deve criar gravação)", h.Audio.Allocated)
	}
	if got := h.Machine.State(); got != dictation.StateIdle {
		t.Fatalf("state = %s, want idle", got)
	}
}

// TestAC002_HoldNaoProduzCaractereEIniciaGravacao cobre o AC-002: "Hold não
// produz caractere e inicia gravação" (REQ-003).
func TestAC002_HoldNaoProduzCaractereEIniciaGravacao(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)

	if len(h.Runtime.Synthesized) != 0 {
		t.Fatalf("synthesized = %v, want nenhum caractere sintetizado num hold", h.Runtime.Synthesized)
	}
	if got := h.Machine.State(); got != dictation.StateRecording {
		t.Fatalf("state = %s, want recording", got)
	}
	if len(h.Runtime.Overlays) != 1 || h.Runtime.Overlays[0].Attempt != attempt || h.Runtime.Overlays[0].Phase != dictation.OverlayRecording {
		t.Fatalf("overlays = %+v, want [Recording] para attempt %d", h.Runtime.Overlays, attempt)
	}
}

// TestAC003_InicioDaFalaNaoEPerdido cobre o AC-003: "Início da fala não é
// perdido" (REQ-005) no modo "sempre pronto" (ADR-007, seção 4; ADR-015): a
// captura já está rodando antes do keydown, e o buffer que Stop devolve
// inclui áudio anterior à marca. O dublê daqui é esse modo. No padrão da
// ADR-015 (sob demanda) o microfone só liga no keydown e não há áudio
// anterior à marca; o AC-003 dele é provado em audio.Session
// (TestSobDemanda_AC003_CapturaComecaAteLDepoisDaMarca).
func TestAC003_InicioDaFalaNaoEPerdido(t *testing.T) {
	h := newHarness(t, defaultConfig())

	// Fala começa a chegar ANTES do usuário sequer pressionar a tecla —
	// exatamente o cenário que o pre-roll do "sempre pronto" existe para
	// proteger.
	h.Audio.Feed(fakes.AudioFrame{At: baseTime.Add(-200 * time.Millisecond), RMS: 0.4})
	h.Audio.Feed(fakes.AudioFrame{At: baseTime.Add(-100 * time.Millisecond), RMS: 0.6})

	attempt := h.keyDown(normalFocus())

	h.Clock.Advance(50 * time.Millisecond)
	h.Audio.Feed(fakes.AudioFrame{At: h.Clock.Now(), RMS: 0.7}) // fala continua durante a gravação

	h.Clock.Advance(defaultConfig().HoldThreshold - 50*time.Millisecond)
	if got := h.Machine.State(); got != dictation.StateRecording {
		t.Fatalf("state = %s, want recording", got)
	}

	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 400 * time.Millisecond})
	h.keyUp(attempt)

	buf, ok := h.Audio.Buffer(attempt)
	if !ok {
		t.Fatalf("nenhum buffer capturado para attempt %d", attempt)
	}
	if len(buf.Frames) < 3 {
		t.Fatalf("buffer tem %d frames, want >= 3 (2 de pre-roll + 1 ao vivo)", len(buf.Frames))
	}

	var sawPreRoll bool
	for _, f := range buf.Frames {
		if f.At.Before(buf.MarkedAt) {
			sawPreRoll = true
			break
		}
	}
	if !sawPreRoll {
		t.Fatalf("buffer não contém nenhum frame anterior a MarkStart (%s) — pre-roll foi perdido; frames: %+v", buf.MarkedAt, buf.Frames)
	}
}

// TestAC004_ModificadorPuroNaoPodeSerConfiguradoComoTecla cobre o AC-004:
// "Modificadores puros não são configuráveis" (REQ-004).
func TestAC004_ModificadorPuroNaoPodeSerConfiguradoComoTecla(t *testing.T) {
	modifiers := []dictation.KeyCode{
		dictation.ShiftLeft, dictation.ShiftRight,
		dictation.ControlLeft, dictation.ControlRight,
		dictation.AltLeft, dictation.AltRight,
		dictation.MetaLeft, dictation.MetaRight,
	}
	for _, key := range modifiers {
		key := key
		t.Run(fmt.Sprintf("key_%d", key), func(t *testing.T) {
			cfg := dictation.Config{Key: key, HoldThreshold: 700 * time.Millisecond}
			err := cfg.Validate()
			if !errors.Is(err, dictation.ErrPureModifierKey) {
				t.Fatalf("Validate() = %v, want errors.Is(_, ErrPureModifierKey)", err)
			}
			if _, err := dictation.NewMachine(cfg); err == nil {
				t.Fatalf("NewMachine aceitou uma tecla modificadora pura (%v) como tecla principal", key)
			}
		})
	}
}

// TestAC005_OverlayRecebeOndaDuracaoEEstado cobre o AC-005: "Overlay mostra
// onda/duração" (REQ-006). O núcleo só expõe o "estado" via
// ShowOverlayEffect.Phase (ADR-007, "O que foi rejeitado": overlay é
// Effect, não colaborador) — onda e duração fluem, no runtime real,
// diretamente da porta de áudio para a UI. Provamos aqui que essa
// informação de fato existe, com dados reais, na borda que o overlay
// consumiria.
func TestAC005_OverlayRecebeOndaDuracaoEEstado(t *testing.T) {
	h := newHarness(t, defaultConfig())

	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)

	if len(h.Runtime.Overlays) != 1 || h.Runtime.Overlays[0].Phase != dictation.OverlayRecording {
		t.Fatalf("overlays = %+v, want exatamente [Recording]", h.Runtime.Overlays)
	}

	// "Onda": frames chegando ao vivo enquanto o overlay está em recording.
	h.Audio.Feed(fakes.AudioFrame{At: h.Clock.Now(), RMS: 0.5})
	h.Clock.Advance(30 * time.Millisecond)
	h.Audio.Feed(fakes.AudioFrame{At: h.Clock.Now(), RMS: 0.8})

	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 900 * time.Millisecond})
	h.keyUp(attempt)

	buf, ok := h.Audio.Buffer(attempt)
	if !ok {
		t.Fatalf("nenhum buffer capturado")
	}
	if len(buf.Frames) == 0 {
		t.Fatalf("nenhum frame de onda disponível para o overlay desenhar")
	}
	// "Duração": um intervalo real, derivado dos carimbos de tempo
	// efetivamente alimentados — não um número prometido em comentário.
	span := buf.Frames[len(buf.Frames)-1].At.Sub(buf.Frames[0].At)
	if span <= 0 {
		t.Fatalf("duração observável do buffer = %s, want > 0", span)
	}

	// "Estado": a sequência completa de fases do overlay.
	//
	// Até 28-set-2026 esta asserção era len(overlays) == 2, o que travava
	// o comportamento de então em vez do critério: AC-005 pede onda e
	// duração, e não diz nada sobre quantas fases existem. O efeito
	// colateral foi que a tela ficava em "Gravando" durante o ASR
	// inteiro — o contador seguia correndo com o microfone já fechado —
	// e nenhum teste reclamava.
	//
	// O que se afirma agora é o contrato de verdade, e ele é mais
	// exigente: começa gravando, PASSA por processando, termina em
	// sucesso.
	overlays := h.Runtime.Overlays
	quero := []dictation.OverlayPhase{
		dictation.OverlayRecording,
		dictation.OverlayProcessing,
		dictation.OverlaySuccess,
	}
	if len(overlays) != len(quero) {
		t.Fatalf("overlays = %+v, want %v", overlays, quero)
	}
	for i, fase := range quero {
		if overlays[i].Phase != fase {
			t.Fatalf("overlays = %+v, want %v", overlays, quero)
		}
	}
}

// TestAC006_BeepPodeSerDesligado cobre o AC-006: "Beep pode ser desligado"
// (REQ-007).
func TestAC006_BeepPodeSerDesligado(t *testing.T) {
	t.Run("habilitado", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.BeepEnabled = true
		h := newHarness(t, cfg)
		h.keyDown(normalFocus())
		h.Clock.Advance(cfg.HoldThreshold)
		if len(h.Runtime.Beeps) != 1 {
			t.Fatalf("beeps = %v, want exatamente um beep quando BeepEnabled=true", h.Runtime.Beeps)
		}
	})
	t.Run("desabilitado", func(t *testing.T) {
		cfg := defaultConfig()
		cfg.BeepEnabled = false
		h := newHarness(t, cfg)
		h.keyDown(normalFocus())
		h.Clock.Advance(cfg.HoldThreshold)
		if len(h.Runtime.Beeps) != 0 {
			t.Fatalf("beeps = %v, want nenhum beep quando BeepEnabled=false", h.Runtime.Beeps)
		}
	})
}

// TestAC007_SoltarFinalizaACaptura cobre o AC-007: "Soltar finaliza
// captura" (REQ-009).
func TestAC007_SoltarFinalizaACaptura(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)

	// O ASR é propositalmente pausado (nunca responde ainda) para que o
	// estado processing fique observável em vez de o runtime síncrono
	// cascatear direto até success — igual ao TEST003.
	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second})
	h.ASR.ExpectSilence(attempt)

	effects := h.keyUp(attempt)

	var stopped bool
	for _, e := range effects {
		if se, ok := e.(dictation.StopAudioCaptureEffect); ok && se.Attempt == attempt {
			stopped = true
		}
	}
	if !stopped {
		t.Fatalf("keyUp não emitiu StopAudioCaptureEffect: %+v", effects)
	}
	if got := h.Machine.State(); got != dictation.StateProcessing {
		t.Fatalf("state = %s, want processing logo após soltar (captura encerrada, aguardando transcrição)", got)
	}
	if h.Audio.Allocated == 0 {
		t.Fatalf("captura não foi encerrada: nenhum buffer foi alocado por Stop")
	}
}

// TestAC008_TextoEntraNoCampoFocado cobre o AC-008: "Texto entra no campo
// focado" (REQ-010).
func TestAC008_TextoEntraNoCampoFocado(t *testing.T) {
	h := newHarness(t, defaultConfig())
	focus := normalFocus()
	attempt := h.keyDown(focus)
	h.Clock.Advance(defaultConfig().HoldThreshold)

	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 300 * time.Millisecond})
	h.ASR.ExpectSuccess(attempt, "reunião às três", 0.95)
	h.Resolve.ExpectSuccess(attempt, "reunião às três")
	h.Insert.ExpectDirectSuccess(attempt)

	h.keyUp(attempt)

	if len(h.Insert.Calls) != 1 {
		t.Fatalf("Insert.Calls = %+v, want exatamente uma chamada", h.Insert.Calls)
	}
	call := h.Insert.Calls[0]
	if call.Text != "reunião às três" {
		t.Fatalf("texto inserido = %q, want %q", call.Text, "reunião às três")
	}
	if call.Focus != focus {
		t.Fatalf("focus da inserção = %+v, want %+v", call.Focus, focus)
	}
	if got := h.Machine.State(); got != dictation.StateSuccess {
		t.Fatalf("state = %s, want success", got)
	}
}

// TestAC009_ClipboardERestaurado cobre o AC-009: "Clipboard é restaurado"
// (REQ-011) — inclusive quando a inserção falha.
func TestAC009_ClipboardERestaurado(t *testing.T) {
	// Honestidade sobre o alcance deste teste: hoje a sequência
	// snapshot->set->paste->restore é inteiramente exercitada dentro do
	// FakeInserter (fakes/inserter.go); só ganha valor de regressão real
	// contra a Área de Transferência do Windows quando o Inserter nativo
	// entrar no Sprint 1 atrás da mesma interface.
	t.Run("fallback_bem_sucedido", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		h.Insert.SeedClipboard("conteudo original do usuario")
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 300 * time.Millisecond})
		h.ASR.ExpectSuccess(attempt, "texto ditado", 0.9)
		h.Resolve.ExpectSuccess(attempt, "texto ditado")
		h.Insert.ExpectClipboardFallbackSuccess(attempt)

		h.keyUp(attempt)

		wantOps := []fakes.ClipboardOp{fakes.ClipSnapshot, fakes.ClipSet, fakes.ClipPaste, fakes.ClipRestore}
		if !equalOps(h.Insert.Ops(), wantOps) {
			t.Fatalf("ops = %v, want %v", h.Insert.Ops(), wantOps)
		}
		if got := h.Insert.ClipboardContent(); got != "conteudo original do usuario" {
			t.Fatalf("clipboard final = %q, want conteúdo original restaurado", got)
		}
		if got := h.Machine.State(); got != dictation.StateSuccess {
			t.Fatalf("state = %s, want success", got)
		}
	})

	t.Run("insercao_falha_mesmo_assim_restaura", func(t *testing.T) {
		h := newHarness(t, defaultConfig())
		h.Insert.SeedClipboard("conteudo original do usuario")
		attempt := h.keyDown(normalFocus())
		h.Clock.Advance(defaultConfig().HoldThreshold)
		h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: true, SpeechDuration: 300 * time.Millisecond})
		h.ASR.ExpectSuccess(attempt, "texto ditado", 0.9)
		h.Resolve.ExpectSuccess(attempt, "texto ditado")
		h.Insert.ExpectTotalFailure(attempt)

		h.keyUp(attempt)

		if got := h.Insert.ClipboardContent(); got != "conteudo original do usuario" {
			t.Fatalf("clipboard final = %q, want conteúdo original restaurado mesmo com falha", got)
		}
		hist := h.lastHistory()
		if hist.Outcome != dictation.OutcomeError || hist.Reason != dictation.ErrorReasonInsertFailed {
			t.Fatalf("history = %+v, want outcome=error reason=insert_failed", hist)
		}
		// Revisão de 29-set-2026: o texto que não entrou no campo fica no
		// histórico. Antes ia vazio, e o painel mostrava "sem texto"
		// justamente no ditado que a pessoa precisava recuperar.
		if hist.Text != "texto ditado" {
			t.Fatalf("history.Text = %q, want %q — o texto que ia entrar", hist.Text, "texto ditado")
		}
	})
}

// TestAC010_SemFalaValidaNadaEInserido cobre o AC-010: "Sem fala válida,
// nada é inserido" (REQ-008,045).
func TestAC010_SemFalaValidaNadaEInserido(t *testing.T) {
	h := newHarness(t, defaultConfig())
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(defaultConfig().HoldThreshold)

	h.Audio.ExpectStopSuccess(attempt, dictation.VADSummary{HasSpeech: false})
	h.keyUp(attempt)

	if len(h.ASR.Calls) != 0 {
		t.Fatalf("ASR foi chamado (%v) mesmo sem fala válida", h.ASR.Calls)
	}
	if len(h.Insert.Calls) != 0 {
		t.Fatalf("Insert foi chamado (%v) mesmo sem fala válida", h.Insert.Calls)
	}
	hist := h.lastHistory()
	if hist.Outcome != dictation.OutcomeNoSpeech {
		t.Fatalf("outcome = %s, want no_speech", hist.Outcome)
	}
	if !h.Audio.AllReleased() {
		t.Fatalf("audio não foi totalmente liberado: allocated=%d released=%d", h.Audio.Allocated, h.Audio.Released)
	}
}

// TestAC044_TeclaEThresholdConfiguraveis cobre o AC-044: "Tecla e threshold
// configuráveis" (REQ-003), incluindo os limites 300ms/2000ms e fora deles.
func TestAC044_TeclaEThresholdConfiguraveis(t *testing.T) {
	cases := []struct {
		name      string
		threshold time.Duration
		wantErr   error
	}{
		{"minimo_inclusive_300ms", dictation.MinHoldThreshold, nil},
		{"maximo_inclusive_2000ms", dictation.MaxHoldThreshold, nil},
		{"abaixo_do_minimo_299ms", dictation.MinHoldThreshold - time.Millisecond, dictation.ErrThresholdOutOfRange},
		{"acima_do_maximo_2001ms", dictation.MaxHoldThreshold + time.Millisecond, dictation.ErrThresholdOutOfRange},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			cfg := dictation.Config{Key: dictation.KeyJ, HoldThreshold: c.threshold}
			err := cfg.Validate()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, want errors.Is(_, %v)", err, c.wantErr)
			}
		})
	}

	// A tecla também é configurável: J funciona exatamente como F faria.
	h := newHarness(t, dictation.Config{Key: dictation.KeyJ, HoldThreshold: 700 * time.Millisecond})
	attempt := h.keyDown(normalFocus())
	h.Clock.Advance(200 * time.Millisecond)
	h.keyUp(attempt)
	if len(h.Runtime.Synthesized) != 1 {
		t.Fatalf("tecla configurada para J: tap não sintetizou caractere")
	}
}

// TestAC045_TeclaComModificadorPreservaComportamentoNormal cobre o AC-045:
// "Tecla com modifier preserva comportamento normal" (REQ-002). ADR-007,
// seção 3, regra 3: a tecla configurada COM modificadores é sempre
// PassThrough — atalhos como Ctrl+F continuam funcionando.
func TestAC045_TeclaComModificadorPreservaComportamentoNormal(t *testing.T) {
	d := dictation.NewDecider(dictation.KeyF)

	down := dictation.RawKeyEvent{Key: dictation.KeyF, Modifiers: dictation.ModControl, Down: true, At: baseTime}
	if decision := d.Decide(down); decision.Suppress || decision.Notify {
		t.Fatalf("Decide(Ctrl+F down) = %+v, want PassThrough", decision)
	}

	up := dictation.RawKeyEvent{Key: dictation.KeyF, Modifiers: dictation.ModControl, Down: false, At: baseTime}
	if decision := d.Decide(up); decision.Suppress || decision.Notify {
		t.Fatalf("Decide(Ctrl+F up) = %+v, want PassThrough", decision)
	}

	// F sozinho, sem modificador, continua sendo capturado normalmente.
	plain := dictation.RawKeyEvent{Key: dictation.KeyF, Modifiers: dictation.None(), Down: true, At: baseTime}
	if decision := d.Decide(plain); !decision.Suppress || !decision.Notify {
		t.Fatalf("Decide(F down) = %+v, want Suppress+Notify", decision)
	}
}

func equalOps(got, want []fakes.ClipboardOp) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
