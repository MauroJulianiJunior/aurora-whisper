package dictation

import (
	"math"
	"testing"
	"time"
)

// O prazo da transcrição acompanha a duração do áudio (incidente de
// 01-out-2026, docs/defeito-ditado-longo.md). Estes testes travam a fórmula,
// os limites e, principalmente, que nenhum ditado do incidente estoura.

// seg converte segundos com até três casas em Duration exata (pelos
// milissegundos inteiros), para a tabela não depender de arredondamento.
func seg(s float64) time.Duration { return time.Duration(math.Round(s*1000)) * time.Millisecond }

func TestPrazoDeTranscricao_Tabela(t *testing.T) {
	for _, c := range []struct {
		audio time.Duration
		want  time.Duration
	}{
		// Duração desconhecida ou absurda: o piso, que é o prazo de antes.
		{0, 10 * time.Second},
		{-time.Second, 10 * time.Second},
		// Fala curta: 5 s + 0,75 × áudio fica abaixo do piso até 6,67 s.
		{seg(1.3), 10 * time.Second},
		{seg(6), 10 * time.Second},
		// A partir daí cresce 0,75 s por segundo de áudio.
		{seg(10), seg(12.5)},
		{seg(27.5), seg(25.625)},
		{seg(37.2), seg(32.9)},
		{seg(59.3), seg(49.475)},
		{seg(88.9), seg(71.675)},
		// O teto chega aos 113,3 s de áudio, antes do limite de gravação.
		{seg(113), seg(89.75)},
		{seg(114), 90 * time.Second},
		{DefaultMaxRecording, 90 * time.Second},
		{10 * time.Minute, 90 * time.Second},
		{1000 * time.Hour, 90 * time.Second},
	} {
		if got := PrazoDeTranscricao(c.audio); got != c.want {
			t.Errorf("PrazoDeTranscricao(%v) = %v, want %v", c.audio, got, c.want)
		}
	}
}

// TestPrazoDeTranscricao_NenhumDitadoDoIncidenteEstoura: os cinco ditados
// longos do agent.log de 01-out-2026, com o tempo que o ASR de fato levou.
// Com o prazo fixo de 10 s, o de 37,2 s e o de 88,9 s caíram no timeout e o
// texto pronto foi descartado.
func TestPrazoDeTranscricao_NenhumDitadoDoIncidenteEstoura(t *testing.T) {
	for _, d := range []struct{ audio, asr float64 }{
		{27.5, 2.39},
		{59.3, 8.13},
		{37.2, 18.84},
		{29.3, 9.17},
		{88.9, 16.08},
	} {
		prazo := PrazoDeTranscricao(seg(d.audio))
		if prazo <= seg(d.asr) {
			t.Errorf("%.1f s de áudio: prazo %v, e o ASR levou %.2f s — o texto se perderia", d.audio, prazo, d.asr)
		}
	}
}

// TestPrazoDeTranscricao_TetoCobreAGravacaoMaxima: o teto tem de caber o
// pior caso medido (razão ASR/áudio de 0,51) no maior áudio que o núcleo
// aceita gravar, com folga.
func TestPrazoDeTranscricao_TetoCobreAGravacaoMaxima(t *testing.T) {
	const piorRazao = 0.51
	pior := time.Duration(float64(DefaultMaxRecording) * piorRazao)
	if folga := float64(TetoPrazoTranscricao) / float64(pior); folga < 1.4 {
		t.Errorf("teto %v para %v de áudio no pior caso (%v): folga %.2f×, quero pelo menos 1,4×",
			TetoPrazoTranscricao, DefaultMaxRecording, pior, folga)
	}
	if PrazoDeTranscricao(DefaultMaxRecording) != TetoPrazoTranscricao {
		t.Errorf("a gravação máxima não chega ao teto: %v", PrazoDeTranscricao(DefaultMaxRecording))
	}
}

// TestPrazoDeTranscricao_NuncaDiminuiComOAudio: mais áudio nunca tem menos
// prazo.
func TestPrazoDeTranscricao_NuncaDiminuiComOAudio(t *testing.T) {
	anterior := PrazoDeTranscricao(0)
	for ms := 0; ms <= 300_000; ms += 250 {
		p := PrazoDeTranscricao(time.Duration(ms) * time.Millisecond)
		if p < anterior {
			t.Fatalf("%d ms de áudio: prazo %v, menor que o anterior %v", ms, p, anterior)
		}
		if p < PisoPrazoTranscricao || p > TetoPrazoTranscricao {
			t.Fatalf("%d ms de áudio: prazo %v fora de [%v, %v]", ms, p, PisoPrazoTranscricao, TetoPrazoTranscricao)
		}
		anterior = p
	}
}

// TestMachine_PrazoDaTranscricaoAcompanhaOAudio: o timer que o núcleo arma
// junto com o TranscribeEffect é o do tamanho do áudio, e não o fixo.
func TestMachine_PrazoDaTranscricaoAcompanhaOAudio(t *testing.T) {
	for _, c := range []struct {
		nome  string
		audio time.Duration
		want  time.Duration
	}{
		{"duração desconhecida fica no piso", 0, PisoPrazoTranscricao},
		{"fala curta fica no piso", 3 * time.Second, PisoPrazoTranscricao},
		{"ditado de um minuto", 60 * time.Second, 50 * time.Second},
		{"ditado de 89 s do incidente", seg(88.9), seg(71.675)},
	} {
		t.Run(c.nome, func(t *testing.T) {
			m := newTestMachine(t, false)
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			efeitos := m.Handle(AudioCapturedEvent{
				Attempt: id,
				Audio:   AudioBufferRef{ID: "buf"},
				// A fala detectada é menor que o áudio (pausas, cauda): o
				// prazo é do ÁUDIO, que é o que o ASR ouve inteiro.
				VAD:      VADSummary{HasSpeech: true, SpeechDuration: c.audio / 2},
				Duration: c.audio,
			})
			timers := effectsOfType[StartTimerEffect](efeitos)
			if len(timers) != 1 || timers[0].Kind != TimerTranscribeTimeout {
				t.Fatalf("timers = %#v, want um TimerTranscribeTimeout", timers)
			}
			if timers[0].Duration != c.want {
				t.Errorf("prazo da transcrição = %v, want %v", timers[0].Duration, c.want)
			}
		})
	}
}
