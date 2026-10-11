package dictation

import (
	"testing"
	"time"
)

// Revisão de 29-set-2026: todo ditado cuja INSERÇÃO falhou chegava ao
// histórico com o texto final vazio. O painel mostrava "sem texto" na
// linha, o editor abria vazio, e o botão Copiar gravava "" na área de
// transferência — destruindo o texto que o Agent tinha deixado lá de
// propósito (ErrorReasonInsertClipboard, ADR-011).
//
// A regra que estes testes travam tem dois lados, e os dois importam:
//
//   - quando o texto final EXISTIU (a resolução terminou e só a inserção
//     falhou), ele vai para o histórico;
//   - quando NÃO existiu (a tentativa acabou antes da resolução), o
//     histórico fica vazio — nunca com um texto inventado ou herdado do
//     ditado anterior.

// ateInserindo leva a máquina até o instante em que o texto final já
// existe e a inserção foi pedida.
func ateInserindo(t *testing.T, m *Machine, final string) AttemptID {
	t.Helper()
	id := startRecording(t, m)
	m.Handle(KeyUpEvent{Attempt: id})
	m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true, SpeechDuration: time.Second}})
	m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "bruto do asr"}})
	efeitos := m.Handle(ResolveResultEvent{Attempt: id, Result: ResolveResultDTO{Text: final}})
	if len(effectsOfType[InsertEffect](efeitos)) != 1 {
		t.Fatalf("pré-condição: a resolução devia ter pedido a inserção, efeitos = %#v", efeitos)
	}
	return id
}

func historicoUnico(t *testing.T, efeitos []Effect) RecordHistoryEffect {
	t.Helper()
	h := effectsOfType[RecordHistoryEffect](efeitos)
	if len(h) != 1 {
		t.Fatalf("RecordHistoryEffect = %#v, want exatamente um", h)
	}
	return h[0]
}

func TestMachine_FalhaDaInsercao_GuardaOTextoFinalNoHistorico(t *testing.T) {
	const final = "Precisa transmitir a DCTFWeb hoje."

	casos := []struct {
		nome        string
		saida       func(m *Machine, id AttemptID) []Effect
		wantOutcome Outcome
		wantReason  ErrorReason
	}{
		{
			// O caso que motivou a revisão: o overlay diz "está na área de
			// transferência", e o histórico precisa dizer O QUÊ.
			nome: "texto retido na área de transferência",
			saida: func(m *Machine, id AttemptID) []Effect {
				return m.Handle(InsertResultEvent{Attempt: id, Reason: ErrorReasonInsertClipboard})
			},
			wantOutcome: OutcomeError,
			wantReason:  ErrorReasonInsertClipboard,
		},
		{
			nome: "inserção falha com motivo",
			saida: func(m *Machine, id AttemptID) []Effect {
				return m.Handle(InsertResultEvent{Attempt: id, Reason: ErrorReasonInsertFailed})
			},
			wantOutcome: OutcomeError,
			wantReason:  ErrorReasonInsertFailed,
		},
		{
			nome: "inserção sem sucesso e sem motivo",
			saida: func(m *Machine, id AttemptID) []Effect {
				return m.Handle(InsertResultEvent{Attempt: id, Result: InsertResultDTO{Success: false}})
			},
			wantOutcome: OutcomeError,
			wantReason:  ErrorReasonInsertFailed,
		},
		{
			nome: "watchdog da inserção",
			saida: func(m *Machine, id AttemptID) []Effect {
				return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerInsertTimeout})
			},
			wantOutcome: OutcomeTimeout,
			wantReason:  ErrorReasonTimeout,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			m := newTestMachine(t, false)
			id := ateInserindo(t, m, final)

			h := historicoUnico(t, c.saida(m, id))
			if h.Outcome != c.wantOutcome || h.Reason != c.wantReason {
				t.Fatalf("desfecho = %v/%v, want %v/%v", h.Outcome, h.Reason, c.wantOutcome, c.wantReason)
			}
			if h.Text != final {
				t.Errorf("Text = %q, want %q — o texto que ia entrar no campo tem de ficar no "+
					"histórico, que é onde a pessoa o recupera", h.Text, final)
			}
		})
	}
}

// TestMachine_SemResolucao_HistoricoFicaSemTexto é o outro lado: onde o
// texto final não existiu, nada é inventado. O ditado ANTERIOR, que
// terminou com texto, é o candidato perigoso — um texto final que
// sobrevivesse à tentativa dele apareceria aqui como se fosse deste.
func TestMachine_SemResolucao_HistoricoFicaSemTexto(t *testing.T) {
	const anterior = "ditado anterior que deu certo"

	casos := []struct {
		nome  string
		saida func(m *Machine, id AttemptID) []Effect
	}{
		{
			nome: "transcrição falha",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				return m.Handle(TranscribeResultEvent{Attempt: id, Reason: ErrorReasonTranscribeFailed})
			},
		},
		{
			nome: "watchdog da transcrição",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerTranscribeTimeout})
			},
		},
		{
			nome: "resolução falha",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "bruto"}})
				return m.Handle(ResolveResultEvent{Attempt: id, Reason: ErrorReasonResolveFailed})
			},
		},
		{
			nome: "watchdog do resolver",
			saida: func(m *Machine, id AttemptID) []Effect {
				m.Handle(AudioCapturedEvent{Attempt: id, VAD: VADSummary{HasSpeech: true}})
				m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: "bruto"}})
				return m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerResolveTimeout})
			},
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			m := newTestMachine(t, false)

			// Um ditado completo antes, com texto final.
			primeiro := ateInserindo(t, m, anterior)
			if h := historicoUnico(t, m.Handle(InsertResultEvent{Attempt: primeiro, Result: InsertResultDTO{Success: true}})); h.Text != anterior {
				t.Fatalf("pré-condição: sucesso gravou %q, want %q", h.Text, anterior)
			}

			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			h := historicoUnico(t, c.saida(m, id))
			if h.Text != "" {
				t.Errorf("Text = %q, want vazio — esta tentativa não chegou a ter texto final, "+
					"e o histórico não pode inventar um", h.Text)
			}
		})
	}
}

// TestMachine_TextoFinalMorreComATentativa trava o invariante que torna
// os dois testes acima robustos a um caminho novo: nenhuma tentativa
// herda o texto final de outra, qualquer que seja o jeito como terminou.
func TestMachine_TextoFinalMorreComATentativa(t *testing.T) {
	desfechos := map[string]func(m *Machine, id AttemptID){
		"sucesso": func(m *Machine, id AttemptID) {
			m.Handle(InsertResultEvent{Attempt: id, Result: InsertResultDTO{Success: true}})
		},
		"falha da inserção": func(m *Machine, id AttemptID) {
			m.Handle(InsertResultEvent{Attempt: id, Reason: ErrorReasonInsertFailed})
		},
		"watchdog da inserção": func(m *Machine, id AttemptID) {
			m.Handle(TimerElapsedEvent{Attempt: id, Kind: TimerInsertTimeout})
		},
	}
	for nome, terminar := range desfechos {
		t.Run(nome, func(t *testing.T) {
			m := newTestMachine(t, false)
			id := ateInserindo(t, m, "texto desta tentativa")
			terminar(m, id)
			if m.CurrentAttempt() != NoAttempt {
				t.Fatalf("pré-condição: a tentativa devia ter terminado")
			}
			if m.finalText != "" {
				t.Errorf("finalText = %q depois do fim da tentativa, want vazio — o próximo "+
					"ditado herdaria este texto", m.finalText)
			}
		})
	}
}
