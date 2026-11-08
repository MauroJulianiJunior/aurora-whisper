package dictation

import (
	"testing"
	"time"
)

// Os testes da gravação solta (ADR-025): dois toques limpos no Ctrl+Win ligam,
// um toque limpo, um clique ou o limite param, o Esc descarta.

// t0 é um instante fixo: os testes medem a janela do duplo toque pelos
// instantes dos eventos, nunca pelo relógio.
var t0 = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

func em(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

func maquinaDaCombinacao(t *testing.T, beep bool) *Machine {
	t.Helper()
	m, err := NewMachine(Config{Modifiers: ctrlWin, HoldThreshold: 700 * time.Millisecond, BeepEnabled: beep})
	if err != nil {
		t.Fatalf("NewMachine() error = %v", err)
	}
	return m
}

// apertar e soltar mandam o gatilho à máquina como o runtime manda: o keydown
// sem tentativa, e o keyup com a tentativa corrente.
func apertar(m *Machine, ms int) []Effect {
	return m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: unprotectedFocus(), At: em(ms)})
}

func soltar(m *Machine, ms int, limpo bool) []Effect {
	return m.Handle(KeyUpEvent{Attempt: m.CurrentAttempt(), At: em(ms), Limpo: limpo})
}

// ligarPorDuploToque faz o duplo toque inteiro (0–100 ms e 300–400 ms) e
// confere que a gravação ficou solta.
func ligarPorDuploToque(t *testing.T, m *Machine) AttemptID {
	t.Helper()
	apertar(m, 0)
	soltar(m, 100, true)
	apertar(m, 300)
	if up := soltar(m, 400, true); len(up) != 0 {
		t.Fatalf("soltura do segundo toque curto e limpo: efeitos %#v, quer nenhum (a gravação fica solta)", up)
	}
	if m.State() != StateRecording {
		t.Fatalf("depois do duplo toque: estado %v, quer StateRecording", m.State())
	}
	return m.CurrentAttempt()
}

func soltaLigada(effects []Effect) (ligou, desligou int) {
	for _, e := range effectsOfType[GravacaoSoltaEffect](effects) {
		if e.Ligada {
			ligou++
		} else {
			desligou++
		}
	}
	return ligou, desligou
}

func temTimer(effects []Effect, kind TimerKind) bool {
	for _, e := range effectsOfType[StartTimerEffect](effects) {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func TestSolta_DoisToquesLimposLigam(t *testing.T) {
	m := maquinaDaCombinacao(t, true)
	apertar(m, 0)
	soltar(m, 100, true)

	down := apertar(m, 300)
	if m.State() != StateRecording {
		t.Fatalf("segundo aperto: estado %v, quer StateRecording (grava já, sem esperar o tempo de segurar)", m.State())
	}
	if ligou, _ := soltaLigada(down); ligou != 1 {
		t.Errorf("segundo aperto: %#v, quer um GravacaoSoltaEffect ligado", down)
	}
	if len(effectsOfType[MarkAudioStartEffect](down)) != 1 {
		t.Errorf("segundo aperto: %#v, quer o MarkAudioStartEffect", down)
	}
	if got := effectsOfType[ShowOverlayEffect](down); len(got) != 1 || got[0].Phase != OverlayRecording {
		t.Errorf("segundo aperto: overlay %#v, quer a pílula gravando já", got)
	}
	if !temTimer(down, TimerMaxRecording) || temTimer(down, TimerHoldThreshold) {
		t.Errorf("segundo aperto: %#v, quer o limite de gravação e nenhum timer do tempo de segurar", down)
	}
	for _, e := range effectsOfType[StartTimerEffect](down) {
		if e.Kind == TimerMaxRecording && e.Duration != DefaultMaxRecording {
			t.Errorf("limite da gravação solta = %v, quer os 2 min de sempre", e.Duration)
		}
	}
	if len(effectsOfType[PlayBeepEffect](down)) != 1 {
		t.Errorf("com o beep ligado: %#v, quer o beep no segundo aperto", down)
	}

	if up := soltar(m, 400, true); len(up) != 0 {
		t.Errorf("soltura curta e limpa do segundo aperto: %#v, quer nenhum efeito", up)
	}
	if m.State() != StateRecording {
		t.Errorf("depois de soltar o segundo aperto: estado %v, quer continuar gravando", m.State())
	}
}

func TestSolta_JanelaNasBordas(t *testing.T) {
	casos := []struct {
		nome          string
		segundoAperto int
		liga          bool
	}{
		{"no limite da janela", 100 + 400, true},
		{"1 ms depois da janela", 100 + 401, false},
		{"logo depois", 101, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			m := maquinaDaCombinacao(t, false)
			apertar(m, 0)
			soltar(m, 100, true)
			apertar(m, c.segundoAperto)
			if got := m.State() == StateRecording; got != c.liga {
				t.Errorf("segundo aperto em %d ms: estado %v, liga=%v, quer liga=%v",
					c.segundoAperto, m.State(), got, c.liga)
			}
		})
	}
}

func TestSolta_SoOToqueLimpoAbreAJanela(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	soltar(m, 100, false) // Ctrl+Win+seta: trocou de área de trabalho
	apertar(m, 300)
	if m.State() != StatePrimed {
		t.Fatalf("depois de um toque sujo: estado %v, quer StatePrimed (o ditado de sempre)", m.State())
	}
}

func TestSolta_InstanteDesconhecidoNaoLiga(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: unprotectedFocus()})
	m.Handle(KeyUpEvent{Attempt: m.CurrentAttempt(), Limpo: true})
	m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: unprotectedFocus()})
	if m.State() != StatePrimed {
		t.Fatalf("sem os instantes: estado %v, quer StatePrimed", m.State())
	}
}

func TestSolta_ComTeclaDeLetraNaoExiste(t *testing.T) {
	m := newTestMachine(t, false) // gatilho na tecla F
	m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: unprotectedFocus(), At: em(0)})
	m.Handle(KeyUpEvent{Attempt: m.CurrentAttempt(), At: em(100), Limpo: true})
	m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: unprotectedFocus(), At: em(300)})
	if m.State() != StatePrimed {
		t.Fatalf("tecla de letra, dois toques: estado %v, quer StatePrimed (são duas letras)", m.State())
	}
}

func TestSolta_CampoProtegidoNaoLiga(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	soltar(m, 100, true)
	protegido := FocusTarget{ProcessName: "app.exe", ControlType: "Edit", Protected: true}
	down := m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: protegido, At: em(300)})
	if m.State() != StateBlocked {
		t.Fatalf("segundo aperto num campo de senha: estado %v, quer StateBlocked", m.State())
	}
	if ligou, _ := soltaLigada(down); ligou != 0 || len(effectsOfType[MarkAudioStartEffect](down)) != 0 {
		t.Errorf("campo de senha: %#v, quer nada de gravação solta nem de áudio (AC-036)", down)
	}
}

func TestSolta_ToqueNoCampoProtegidoNaoAbreAJanela(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	protegido := FocusTarget{ProcessName: "app.exe", ControlType: "Edit", Protected: true}
	m.Handle(KeyDownEvent{Attempt: NoAttempt, Focus: protegido, At: em(0)})
	soltar(m, 100, true)
	apertar(m, 300)
	if m.State() != StatePrimed {
		t.Fatalf("depois de um toque no campo de senha: estado %v, quer StatePrimed", m.State())
	}
}

func TestSolta_SegundoApertoSeguradoEhODitadoDeSempre(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	soltar(m, 100, true)
	apertar(m, 300)
	up := soltar(m, 300+700, true)
	if m.State() != StateProcessing {
		t.Fatalf("segundo aperto segurado 700 ms: estado %v, quer StateProcessing (soltar para)", m.State())
	}
	if len(effectsOfType[StopAudioCaptureEffect](up)) != 1 {
		t.Errorf("soltura: %#v, quer o StopAudioCaptureEffect", up)
	}
	if got := effectsOfType[GravacaoSoltaEffect](up); len(got) != 1 || got[0].Ligada || got[0].Fim != FimSegurou {
		t.Errorf("soltura: %#v, quer a gravação solta desligada como segurada", up)
	}
}

func TestSolta_SegundoApertoComOutraTeclaDescarta(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	soltar(m, 100, true)
	apertar(m, 300)
	up := soltar(m, 450, false) // o segundo foi um Ctrl+Win+seta
	conferirDescarte(t, m, up)
}

func TestSolta_KeyDownNoMeioNaoFazNada(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	if down := apertar(m, 2000); len(down) != 0 {
		t.Errorf("aperto com a gravação solta: %#v, quer nenhum efeito (a soltura decide)", down)
	}
	if m.CurrentAttempt() != id || m.State() != StateRecording {
		t.Errorf("depois do aperto: tentativa %v estado %v, quer a mesma tentativa gravando", m.CurrentAttempt(), m.State())
	}
}

func TestSolta_ToqueLimpoPara(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	apertar(m, 5000)
	conferirParada(t, m, id, soltar(m, 5100, true), true, FimToque)
}

func TestSolta_ToqueComOutraTeclaNaoPara(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	ligarPorDuploToque(t, m)
	apertar(m, 5000)
	if up := soltar(m, 5100, false); len(up) != 0 {
		t.Errorf("Ctrl+Win+seta com a gravação solta: %#v, quer nenhum efeito (a gravação segue)", up)
	}
	if m.State() != StateRecording {
		t.Errorf("estado %v, quer continuar gravando", m.State())
	}
}

func TestSolta_CliquePara(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	conferirParada(t, m, id, m.Handle(CliqueEvent{Attempt: id}), true, FimClique)
}

func TestSolta_CliqueComOSegundoApertoSeguroPara(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	soltar(m, 100, true)
	apertar(m, 300)
	id := m.CurrentAttempt()
	conferirParada(t, m, id, m.Handle(CliqueEvent{Attempt: id}), true, FimClique)
	if up := soltar(m, 2000, true); len(up) != 0 {
		t.Errorf("soltura depois do clique: %#v, quer nenhum efeito", up)
	}
}

func TestSolta_LimiteParaEEscreve(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	conferirParada(t, m, id, m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerMaxRecording}), false, FimLimite)
}

func TestSolta_EscDescarta(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	conferirDescarte(t, m, m.Handle(EscEvent{Attempt: id}))
}

func TestSolta_CliqueEEscForaDaSoltaNaoFazemNada(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	// O ditado de sempre: segurar.
	apertar(m, 0)
	id := m.CurrentAttempt()
	m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})
	if m.State() != StateRecording {
		t.Fatalf("hold: estado %v, quer StateRecording", m.State())
	}
	if got := m.Handle(CliqueEvent{Attempt: id}); len(got) != 0 {
		t.Errorf("clique segurando o gatilho: %#v, quer nenhum efeito", got)
	}
	if got := m.Handle(EscEvent{Attempt: id}); len(got) != 0 {
		t.Errorf("Esc segurando o gatilho: %#v, quer nenhum efeito", got)
	}
	if m.State() != StateRecording {
		t.Errorf("estado %v, quer continuar gravando", m.State())
	}
	// Um clique atrasado, sem tentativa, também não.
	if got := m.Handle(CliqueEvent{Attempt: NoAttempt}); len(got) != 0 {
		t.Errorf("clique sem tentativa: %#v, quer nenhum efeito", got)
	}
}

func TestSolta_DepoisDeParar_CliqueEEscNaoFazemNada(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	m.Handle(CliqueEvent{Attempt: id})
	if got := m.Handle(CliqueEvent{Attempt: id}); len(got) != 0 {
		t.Errorf("segundo clique, já transcrevendo: %#v, quer nenhum efeito", got)
	}
	if got := m.Handle(EscEvent{Attempt: id}); len(got) != 0 {
		t.Errorf("Esc já transcrevendo: %#v, quer nenhum efeito (o texto já está a caminho)", got)
	}
}

// TestSolta_OFocoEhRelidoNaHoraDeEscrever segue a tentativa solta até o fim:
// a resolução e a inserção pedem o foco relido, e o texto vai ao histórico.
func TestSolta_OFocoEhRelidoNaHoraDeEscrever(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	m.Handle(CliqueEvent{Attempt: id})

	if len(seguirAteInserir(t, m, id, true)) != 1 {
		t.Fatal("a tentativa não chegou à inserção")
	}
	fim := m.Handle(InsertResultEvent{Attempt: id, Result: InsertResultDTO{Success: true}})
	if got := effectsOfType[RecordHistoryEffect](fim); len(got) != 1 || got[0].Text != "texto ditado" {
		t.Errorf("fim: histórico %#v, quer o texto", got)
	}
	if m.State() != StateSuccess {
		t.Errorf("fim: estado %v, quer StateSuccess", m.State())
	}
}

// TestSolta_ATentativaSeguinteNaoReleFoco: a marca da gravação solta morre
// com a tentativa; o ditado de sempre, depois dela, usa o foco do começo.
func TestSolta_ATentativaSeguinteNaoReleFoco(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	m.Handle(CliqueEvent{Attempt: id})
	seguirAteInserir(t, m, id, true)
	m.Handle(InsertResultEvent{Attempt: id, Result: InsertResultDTO{Success: true}})

	apertar(m, 20000)
	novo := m.CurrentAttempt()
	m.Handle(TimerElapsedEvent{Attempt: novo, Kind: TimerHoldThreshold})
	soltar(m, 23000, true)
	seguirAteInserir(t, m, novo, false)
}

func TestSolta_OHoldNaoReleFoco(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	id := m.CurrentAttempt()
	m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerHoldThreshold})
	soltar(m, 3000, true)
	seguirAteInserir(t, m, id, false)
}

// seguirAteInserir leva a tentativa parada do áudio à inserção e confere o
// ReleFoco da resolução e da inserção.
func seguirAteInserir(t *testing.T, m *Machine, id AttemptID, releFoco bool) []InsertEffect {
	t.Helper()
	tr := m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}, Duration: 3 * time.Second})
	if len(effectsOfType[TranscribeEffect](tr)) != 1 {
		t.Fatalf("áudio capturado: %#v, quer a transcrição", tr)
	}
	res := m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "texto bruto"}})
	r := effectsOfType[ResolveEffect](res)
	if len(r) != 1 || r[0].ReleFoco != releFoco {
		t.Fatalf("transcrição: resolução %#v, quer uma com ReleFoco=%v", r, releFoco)
	}
	ins := effectsOfType[InsertEffect](m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: "texto ditado"}}))
	if len(ins) != 1 || ins[0].ReleFoco != releFoco {
		t.Fatalf("resolução: inserção %#v, quer uma com ReleFoco=%v", ins, releFoco)
	}
	return ins
}

func TestSolta_OToqueQueParaNaoAbreJanela(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	apertar(m, 5000)
	soltar(m, 5100, true)
	m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: false}})
	if m.CurrentAttempt() != NoAttempt {
		t.Fatalf("sem fala: a tentativa %v não acabou", m.CurrentAttempt())
	}
	apertar(m, 5300)
	if m.State() != StatePrimed {
		t.Fatalf("aperto logo depois do toque que parou: estado %v, quer StatePrimed", m.State())
	}
}

func TestSolta_DepoisDoDescarte_ODitadoDeSempreFunciona(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	id := ligarPorDuploToque(t, m)
	m.Handle(EscEvent{Attempt: id})
	apertar(m, 9000)
	novo := m.CurrentAttempt()
	if novo == id || m.State() != StatePrimed {
		t.Fatalf("depois do descarte: tentativa %v estado %v, quer uma nova, StatePrimed", novo, m.State())
	}
	m.Handle(TimerElapsedEvent{Attempt: novo, Kind: TimerHoldThreshold})
	if m.State() != StateRecording {
		t.Errorf("hold depois do descarte: estado %v, quer StateRecording", m.State())
	}
}

// conferirParada confere a parada da gravação solta: desliga a solta antes de
// tudo, para a captura e espera o áudio. cancelaLimite diz se o limite de
// gravação ainda precisava ser cancelado (falso quando foi ele que parou).
func conferirParada(t *testing.T, m *Machine, id AttemptID, effects []Effect, cancelaLimite bool, fim FimDaSolta) {
	t.Helper()
	if m.State() != StateProcessing || m.CurrentAttempt() != id {
		t.Fatalf("parada: estado %v tentativa %v, quer StateProcessing na tentativa %v", m.State(), m.CurrentAttempt(), id)
	}
	if len(effects) == 0 {
		t.Fatal("parada sem efeitos")
	}
	if e, ok := effects[0].(GravacaoSoltaEffect); !ok || e.Ligada || e.Fim != fim {
		t.Errorf("parada: primeiro efeito %#v, quer o GravacaoSoltaEffect desligado com o fim %v", effects[0], fim)
	}
	if len(effectsOfType[StopAudioCaptureEffect](effects)) != 1 || !temTimer(effects, TimerAudioStopTimeout) {
		t.Errorf("parada: %#v, quer o StopAudioCaptureEffect e o prazo dele", effects)
	}
	cancelou := false
	for _, c := range effectsOfType[CancelTimerEffect](effects) {
		if c.Kind == TimerMaxRecording {
			cancelou = true
		}
	}
	if cancelou != cancelaLimite {
		t.Errorf("parada: cancelou o limite=%v, quer %v", cancelou, cancelaLimite)
	}
	if got := effectsOfType[ShowOverlayEffect](effects); len(got) != 1 || got[0].Phase != OverlayProcessing {
		t.Errorf("parada: overlay %#v, quer OverlayProcessing", got)
	}
}

// conferirDescarte confere o descarte: nada vai ao ASR, nada entra no
// histórico, a pílula some, e o áudio é liberado por último.
func conferirDescarte(t *testing.T, m *Machine, effects []Effect) {
	t.Helper()
	if m.State() != StateIdle || m.CurrentAttempt() != NoAttempt {
		t.Fatalf("descarte: estado %v tentativa %v, quer StateIdle sem tentativa", m.State(), m.CurrentAttempt())
	}
	if got := effectsOfType[GravacaoSoltaEffect](effects); len(got) != 1 || got[0].Ligada || got[0].Fim != FimDescartada {
		t.Errorf("descarte: %#v, quer a gravação solta desligada como descartada", effects)
	}
	if len(effectsOfType[AbortAudioCaptureEffect](effects)) != 1 {
		t.Errorf("descarte: %#v, quer o AbortAudioCaptureEffect", effects)
	}
	if len(effectsOfType[StopAudioCaptureEffect](effects)) != 0 || len(effectsOfType[TranscribeEffect](effects)) != 0 {
		t.Errorf("descarte: %#v, quer nada de parar a captura para transcrever", effects)
	}
	if len(effectsOfType[RecordHistoryEffect](effects)) != 0 {
		t.Errorf("descarte: %#v, quer nada no histórico", effects)
	}
	if got := effectsOfType[ShowOverlayEffect](effects); len(got) != 1 || got[0].Phase != OverlayHidden {
		t.Errorf("descarte: overlay %#v, quer a pílula escondida", got)
	}
	if _, ok := effects[len(effects)-1].(ReleaseAudioEffect); !ok {
		t.Errorf("descarte: último efeito %#v, quer o ReleaseAudioEffect (TEST-007)", effects[len(effects)-1])
	}
}

// TestSolta_GravandoSemSegurar: só depois de o segundo aperto ser solto a
// gravação está solta de verdade; segurando o segundo aperto, e no ditado de
// sempre, não.
func TestSolta_GravandoSemSegurar(t *testing.T) {
	m := maquinaDaCombinacao(t, false)
	apertar(m, 0)
	if m.GravandoSemSegurar() {
		t.Fatal("primeiro aperto: já gravando sem segurar")
	}
	soltar(m, 100, true)
	apertar(m, 300)
	if m.GravandoSemSegurar() {
		t.Fatal("segundo aperto ainda seguro: gravando sem segurar")
	}
	soltar(m, 400, true)
	if !m.GravandoSemSegurar() {
		t.Fatal("segundo aperto solto: quer gravando sem segurar")
	}
	id := m.CurrentAttempt()
	m.Handle(CliqueEvent{Attempt: id})
	if m.GravandoSemSegurar() {
		t.Fatal("depois do clique: ainda gravando sem segurar")
	}

	// O ditado de sempre nunca está solto.
	h := maquinaDaCombinacao(t, false)
	apertar(h, 0)
	h.Handle(TimerElapsedEvent{Attempt: h.CurrentAttempt(), Kind: TimerHoldThreshold})
	if h.State() != StateRecording || h.GravandoSemSegurar() {
		t.Fatalf("hold: estado %v, solto=%v; quer gravando, não solto", h.State(), h.GravandoSemSegurar())
	}
}

// TestFimDaSolta_String: os nomes que vão ao log, e nada para o efeito que
// liga.
func TestFimDaSolta_String(t *testing.T) {
	for f, quer := range map[FimDaSolta]string{
		SemFim: "", FimToque: "toque", FimClique: "clique", FimLimite: "limite",
		FimSegurou: "segurou", FimDescartada: "descartada",
	} {
		if got := f.String(); got != quer {
			t.Errorf("FimDaSolta(%d).String() = %q, quer %q", f, got, quer)
		}
	}
}
