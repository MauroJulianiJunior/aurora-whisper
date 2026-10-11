package winruntime

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// historicoFalso guarda o que o Runtime mandou gravar. Não há banco:
// o que se testa aqui é O QUE vai para a linha, não o SQLite.
type historicoFalso struct {
	linhas []storage.Transcription
}

func (h *historicoFalso) Put(t storage.Transcription) error {
	h.linhas = append(h.linhas, t)
	return nil
}

func (h *historicoFalso) unica(t *testing.T) storage.Transcription {
	t.Helper()
	if len(h.linhas) != 1 {
		t.Fatalf("linhas gravadas = %d, want 1", len(h.linhas))
	}
	return h.linhas[0]
}

const (
	provedorDeTeste = "whisper-local"
	modeloDeTeste   = "ggml-small-q4_0"
)

// ditarAteOHistorico percorre, com os pedaços reais do Runtime, o
// caminho de uma tentativa até o registro: captura, transcrição e
// resolução. Só não há Windows — nem hook, nem microfone, nem inserção:
// o desfecho da inserção entra direto como RecordHistoryEffect.
//
// amostras a 16 kHz: 24000 são 1,5 s de áudio.
func ditarAteOHistorico(t *testing.T, rt *Runtime, attempt dictation.AttemptID, amostras int, app string) {
	t.Helper()
	rt.concluirCaptura(capturaPronta{
		attempt: attempt,
		buf:     audioFalso(amostras),
		vad:     dictation.VADSummary{HasSpeech: true, SpeechDuration: time.Second},
	})
	rt.transcrever(dictation.TranscribeEffect{Attempt: attempt})
	var te dictation.TranscribeResultEvent
	select {
	case ev := <-rt.pending:
		te = ev.(dictation.TranscribeResultEvent)
	case <-time.After(2 * time.Second):
		t.Fatal("a transcrição não voltou")
	}
	if te.Reason != dictation.ErrorReasonNone {
		t.Fatalf("pré-condição: transcrição falhou (%v)", te.Reason)
	}
	rt.resolver(dictation.ResolveEffect{
		Attempt:   attempt,
		ASRResult: te.Result,
		Focus:     dictation.FocusTarget{ProcessName: app},
	})
}

// TestRecordHistory_GravaModeloDuracaoEProcessamento é o AC-043 em uso
// real. Até a revisão de 29-set-2026 o Runtime tinha os três números na
// mão e montava a linha sem nenhum deles; o painel caía para "data ·
// provider" em todo ditado, e o teste da API, que preenchia os campos à
// mão, dizia que estava tudo certo.
func TestRecordHistory_GravaModeloDuracaoEProcessamento(t *testing.T) {
	const texto = "precisa transmitir a DCTFWeb hoje"
	rt := runtimeMinimo(&asrFalso{texto: texto, processamento: 437 * time.Millisecond})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")

	ditarAteOHistorico(t, rt, 3, 24000, "OUTLOOK.EXE")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 3, Outcome: dictation.OutcomeSuccess, Text: texto})

	l := hist.unica(t)
	if l.ASRProvider != provedorDeTeste {
		t.Errorf("ASRProvider = %q, want %q", l.ASRProvider, provedorDeTeste)
	}
	if l.ASRModel == nil || *l.ASRModel != modeloDeTeste {
		t.Errorf("ASRModel = %v, want %q", deref(l.ASRModel), modeloDeTeste)
	}
	// 24000 amostras a 16 kHz: a duração do ÁUDIO transcrito, não o
	// tempo de tecla apertada.
	if l.DurationMs == nil || *l.DurationMs != 1500 {
		t.Errorf("DurationMs = %v, want 1500", derefInt(l.DurationMs))
	}
	// O que o provider mediu, e não um relógio do Runtime.
	if l.ProcessingMs == nil || *l.ProcessingMs != 437 {
		t.Errorf("ProcessingMs = %v, want 437", derefInt(l.ProcessingMs))
	}
	if l.Status != "done" || l.RawText != texto || l.FinalText != texto {
		t.Errorf("linha = status %q, bruto %q, final %q", l.Status, l.RawText, l.FinalText)
	}
}

// TestRecordHistory_NuncaGravaOAppEmFoco: o nome do app em foco é
// contexto de outro aplicativo, e o 09-security-privacy.md manda não
// persistir contexto. A coluna target_app existe no schema, e é
// exatamente por isso que precisa de um teste dizendo que fica vazia —
// senão a próxima pessoa a "completar os metadados" a preenche.
func TestRecordHistory_NuncaGravaOAppEmFoco(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "o comitê aprovou"})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")

	ditarAteOHistorico(t, rt, 1, 16000, "OUTLOOK.EXE")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 1, Outcome: dictation.OutcomeSuccess, Text: "o comitê aprovou"})

	if l := hist.unica(t); l.TargetApp != nil {
		t.Errorf("TargetApp = %q — contexto de outro app foi persistido, contra o 09-security-privacy.md", *l.TargetApp)
	}
}

// TestRecordHistory_FalhaDaInsercaoGuardaOTextoEAsMedicoes: o núcleo
// passou a mandar o texto final quando só a inserção falhou (revisão de
// 29-set-2026). A linha tem de chegar ao banco com ele, com status de
// erro, e com as medições — o ASR trabalhou do mesmo jeito.
func TestRecordHistory_FalhaDaInsercaoGuardaOTextoEAsMedicoes(t *testing.T) {
	const bruto = "precisa transmitir a DCTF Web"
	const final = "precisa transmitir a DCTFWeb"
	rt := runtimeMinimo(&asrFalso{texto: bruto, processamento: 610 * time.Millisecond})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")

	ditarAteOHistorico(t, rt, 5, 32000, "Code.exe")
	rt.recordHistory(dictation.RecordHistoryEffect{
		Attempt: 5,
		Outcome: dictation.OutcomeError,
		Reason:  dictation.ErrorReasonInsertClipboard,
		Text:    final,
	})

	l := hist.unica(t)
	if l.Status != "error" {
		t.Errorf("Status = %q, want error", l.Status)
	}
	if l.RawText != bruto || l.FinalText != final {
		t.Errorf("bruto = %q, final = %q; want %q, %q", l.RawText, l.FinalText, bruto, final)
	}
	if l.DurationMs == nil || *l.DurationMs != 2000 || l.ProcessingMs == nil || *l.ProcessingMs != 610 {
		t.Errorf("DurationMs = %v, ProcessingMs = %v; want 2000, 610", derefInt(l.DurationMs), derefInt(l.ProcessingMs))
	}
}

// TestRecordHistory_MedicaoDeOutraTentativaNaoEntra é o "nunca gravar
// número que não foi medido" aplicado ao caso difícil: a medição existe,
// mas é de OUTRO ditado. Acontece quando uma tentativa cai no watchdog e
// a goroutine dela termina depois, já durante a seguinte. Atribuir o
// número dela a esta seria pior que deixar vazio.
func TestRecordHistory_MedicaoDeOutraTentativaNaoEntra(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "primeiro ditado", processamento: 900 * time.Millisecond})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")

	// A tentativa 1 capturou e transcreveu, e acabou sem chegar ao
	// histórico por este caminho.
	ditarAteOHistorico(t, rt, 1, 48000, "")

	// A tentativa 2 chega ao histórico com o bruto dela, mas sem
	// medição própria.
	rt.resolver(dictation.ResolveEffect{Attempt: 2, ASRResult: dictation.ASRResultDTO{Text: "segundo ditado"}})
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 2, Outcome: dictation.OutcomeSuccess, Text: "segundo ditado"})

	l := hist.unica(t)
	if l.DurationMs != nil {
		t.Errorf("DurationMs = %d — é a duração da tentativa 1 atribuída à 2", *l.DurationMs)
	}
	if l.ProcessingMs != nil {
		t.Errorf("ProcessingMs = %d — é o tempo do ASR da tentativa 1 atribuído à 2", *l.ProcessingMs)
	}
	// O modelo não é medição: é configuração do Agent, e vale para toda linha.
	if l.ASRModel == nil || *l.ASRModel != modeloDeTeste {
		t.Errorf("ASRModel = %v, want %q", deref(l.ASRModel), modeloDeTeste)
	}
}

// TestRecordHistory_MedicaoAtrasadaDeOutraTentativa: a mesma regra, pelo
// lado da goroutine. A medição da tentativa 7 é publicada DEPOIS de a
// tentativa 8 ter tudo pronto — é a corrida que o ponteiro atômico com a
// tentativa gravada junto existe para resolver.
func TestRecordHistory_MedicaoAtrasadaDeOutraTentativa(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "ditado oito"})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")

	rt.concluirCaptura(capturaPronta{attempt: 8, buf: audioFalso(16000), vad: dictation.VADSummary{HasSpeech: true}})
	rt.resolver(dictation.ResolveEffect{Attempt: 8, ASRResult: dictation.ASRResultDTO{Text: "ditado oito"}})
	rt.asrMedido.Store(&medicaoASR{attempt: 7, processamento: 3 * time.Second})
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 8, Outcome: dictation.OutcomeSuccess, Text: "ditado oito"})

	l := hist.unica(t)
	if l.ProcessingMs != nil {
		t.Errorf("ProcessingMs = %d — é o tempo da tentativa 7", *l.ProcessingMs)
	}
	if l.DurationMs == nil || *l.DurationMs != 1000 {
		t.Errorf("DurationMs = %v, want 1000 — a duração desta tentativa foi medida", derefInt(l.DurationMs))
	}
}

// TestRecordHistory_MedicoesSaoConsumidas: gravada uma linha, a medição
// dela não fica esperando a próxima com o mesmo número de tentativa.
func TestRecordHistory_MedicoesSaoConsumidas(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "ok", processamento: 300 * time.Millisecond})
	rt.UsarHistorico(&historicoFalso{}, provedorDeTeste, modeloDeTeste, "mauro")

	ditarAteOHistorico(t, rt, 4, 16000, "")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 4, Outcome: dictation.OutcomeSuccess, Text: "ok"})

	if rt.audioDaTentativa.duracao != 0 || rt.audioDaTentativa.attempt != 0 {
		t.Errorf("audioDaTentativa = %+v depois de gravar, want zerado", rt.audioDaTentativa)
	}
	if m := rt.asrMedido.Load(); m != nil {
		t.Errorf("asrMedido = %+v depois de gravar, want nil", *m)
	}
}

// TestRecordHistory_SemModeloConfiguradoGravaAusente: vazio é "não sei",
// e "não sei" no banco é NULL, não "".
func TestRecordHistory_SemModeloConfiguradoGravaAusente(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "ok"})
	hist := &historicoFalso{}
	rt.UsarHistorico(hist, provedorDeTeste, "", "mauro")

	ditarAteOHistorico(t, rt, 1, 16000, "")
	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 1, Outcome: dictation.OutcomeSuccess, Text: "ok"})

	if l := hist.unica(t); l.ASRModel != nil {
		t.Errorf("ASRModel = %q, want ausente", *l.ASRModel)
	}
}

// TestMsOuAusente: zero é "não medido", nunca "0 ms".
func TestMsOuAusente(t *testing.T) {
	if got := msOuAusente(0); got != nil {
		t.Errorf("msOuAusente(0) = %d, want ausente — gravaria um ditado instantâneo que ninguém mediu", *got)
	}
	if got := msOuAusente(-time.Second); got != nil {
		t.Errorf("msOuAusente(-1s) = %d, want ausente", *got)
	}
	if got := msOuAusente(1500 * time.Millisecond); got == nil || *got != 1500 {
		t.Errorf("msOuAusente(1,5 s) = %v, want 1500", derefInt(got))
	}
}

func deref(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
