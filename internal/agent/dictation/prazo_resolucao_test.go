package dictation

import (
	"math"
	"strings"
	"testing"
	"time"
)

// O prazo da resolução acompanha o tamanho do texto (decisão 5 de
// docs/defeito-ditado-longo.md, 01-out-2026). Estes testes travam a fórmula
// e, principalmente, que a fala curta continua com o prazo de antes ao
// milissegundo.

// textoDe devolve um texto de n caracteres (runas).
func textoDe(n int) string { return strings.Repeat("a", n) }

func TestAcrescimoPeloTexto_Tabela(t *testing.T) {
	for _, c := range []struct {
		caracteres int
		want       time.Duration
	}{
		// Texto vazio, curto, ou o maior sem acréscimo: nada a mais.
		{0, 0},
		{9, 0},
		{99, 0},
		{CaracteresSemAcrescimo, 0},
		// Daí em diante, 6 ms por caractere.
		{251, 6 * time.Millisecond},
		{254, 24 * time.Millisecond},
		// Os ditados longos do log de 01-out-2026.
		{359, 654 * time.Millisecond},
		{802, 3312 * time.Millisecond},
		{907, 3942 * time.Millisecond},
		// O teto (4,5 s) chega aos 1000 caracteres.
		{999, 4494 * time.Millisecond},
		{1000, 4500 * time.Millisecond},
		{1001, 4500 * time.Millisecond},
		{1373, 4500 * time.Millisecond},
		{100_000, 4500 * time.Millisecond},
	} {
		if got := AcrescimoPeloTexto(textoDe(c.caracteres)); got != c.want {
			t.Errorf("AcrescimoPeloTexto(%d caracteres) = %v, want %v", c.caracteres, got, c.want)
		}
	}
}

// TestAcrescimoPeloTexto_ContaCaracteresENaoBytes: "ç" são dois bytes. O
// log do Polish conta caracteres (entrada_chars), e é nessa unidade que os
// números do incidente foram medidos.
func TestAcrescimoPeloTexto_ContaCaracteresENaoBytes(t *testing.T) {
	if got := AcrescimoPeloTexto(strings.Repeat("ç", 250)); got != 0 {
		t.Errorf("250 caracteres (500 bytes): acréscimo %v, want 0", got)
	}
	if got := AcrescimoPeloTexto(strings.Repeat("ç", 300)); got != 300*time.Millisecond {
		t.Errorf("300 caracteres (600 bytes): acréscimo %v, want 300ms", got)
	}
}

func TestPrazoDeResolucao_Tabela(t *testing.T) {
	for _, c := range []struct {
		caracteres int
		want       time.Duration
	}{
		// A fala curta tem o prazo de antes, ao milissegundo.
		{0, DefaultResolveTimeout},
		{38, DefaultResolveTimeout},
		{CaracteresSemAcrescimo, DefaultResolveTimeout},
		{359, 5654 * time.Millisecond},
		{802, 8312 * time.Millisecond},
		{907, 8942 * time.Millisecond},
		{1000, 9500 * time.Millisecond},
		{5000, 9500 * time.Millisecond},
	} {
		if got := PrazoDeResolucao(textoDe(c.caracteres)); got != c.want {
			t.Errorf("PrazoDeResolucao(%d caracteres) = %v, want %v", c.caracteres, got, c.want)
		}
	}
	if PisoPrazoResolucao != 5*time.Second || TetoPrazoResolucao != 9500*time.Millisecond {
		t.Errorf("piso %v e teto %v; quero 5 s (o prazo de antes) e 9,5 s (os 8 s do Polish + 1,5 s)", PisoPrazoResolucao, TetoPrazoResolucao)
	}
}

// TestPrazoDeResolucao_NuncaDiminuiComOTexto: mais texto nunca tem menos
// prazo, e o prazo nunca sai de [piso, teto].
func TestPrazoDeResolucao_NuncaDiminuiComOTexto(t *testing.T) {
	anterior := PrazoDeResolucao("")
	for n := 0; n <= 3000; n++ {
		p := PrazoDeResolucao(textoDe(n))
		if p < anterior {
			t.Fatalf("%d caracteres: prazo %v, menor que o anterior %v", n, p, anterior)
		}
		if p < PisoPrazoResolucao || p > TetoPrazoResolucao {
			t.Fatalf("%d caracteres: prazo %v fora de [%v, %v]", n, p, PisoPrazoResolucao, TetoPrazoResolucao)
		}
		anterior = p
	}
}

// TestPrazoDeResolucao_UmMinutoDeFalaChegaPertoDoTeto: a decisão do Mauro
// fala em "uns 8 s num ditado de 1 minuto" para o Polish (o watchdog fica
// 1,5 s acima). Nos ditados do incidente a fala rendeu de 13,2 a 15,4
// caracteres por segundo; um minuto disso tem de dar ao Polish entre 6,5 s
// e 8 s.
func TestPrazoDeResolucao_UmMinutoDeFalaChegaPertoDoTeto(t *testing.T) {
	for _, porSegundo := range []float64{13.2, 15.4} {
		n := int(math.Round(60 * porSegundo))
		polish := PrazoDeResolucao(textoDe(n)) - 1500*time.Millisecond
		if polish < 6500*time.Millisecond || polish > 8*time.Second {
			t.Errorf("1 minuto a %.1f caracteres/s (%d caracteres): o Polish teria %v, quero entre 6,5 s e 8 s", porSegundo, n, polish)
		}
	}
}

// TestMachine_PrazoDaResolucaoAcompanhaOTexto: o timer que o núcleo arma
// junto com o ResolveEffect é o do tamanho do texto que vai NESSE efeito.
func TestMachine_PrazoDaResolucaoAcompanhaOTexto(t *testing.T) {
	for _, c := range []struct {
		nome  string
		texto string
		want  time.Duration
	}{
		{"texto vazio fica no piso", "", DefaultResolveTimeout},
		{"fala curta fica no piso", "reunião às três", DefaultResolveTimeout},
		{"o ditado de 802 caracteres do incidente", textoDe(802), 8312 * time.Millisecond},
		{"conta caracteres, não bytes", strings.Repeat("ã", 300), 5300 * time.Millisecond},
		{"ditado enorme fica no teto", textoDe(1373), TetoPrazoResolucao},
	} {
		t.Run(c.nome, func(t *testing.T) {
			m := newTestMachine(t, false)
			id := startRecording(t, m)
			m.Handle(KeyUpEvent{Attempt: id})
			m.Handle(AudioCapturedEvent{Attempt: id, Audio: AudioBufferRef{ID: "buf"}, VAD: VADSummary{HasSpeech: true}})
			efeitos := m.Handle(TranscribeResultEvent{Attempt: id, Result: ASRResultDTO{Text: c.texto}})
			resolves := effectsOfType[ResolveEffect](efeitos)
			if len(resolves) != 1 || resolves[0].ASRResult.Text != c.texto {
				t.Fatalf("resolves = %#v, want um ResolveEffect com o texto do ASR", resolves)
			}
			timers := effectsOfType[StartTimerEffect](efeitos)
			if len(timers) != 1 || timers[0].Kind != TimerResolveTimeout {
				t.Fatalf("timers = %#v, want um TimerResolveTimeout", timers)
			}
			if timers[0].Duration != c.want {
				t.Errorf("prazo da resolução = %v, want %v", timers[0].Duration, c.want)
			}
			if got := PrazoDeResolucao(resolves[0].ASRResult.Text); got != timers[0].Duration {
				t.Errorf("o timer (%v) não é o prazo do texto que foi no ResolveEffect (%v)", timers[0].Duration, got)
			}
		})
	}
}
