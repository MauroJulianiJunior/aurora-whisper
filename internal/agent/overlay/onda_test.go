package overlay

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// relogioFalso permite exercitar a janela da onda sem dormir: a janela é
// medida em tempo real, e um teste que esperasse 1,3 s de verdade seria
// lento e instável.
type relogioFalso struct{ t time.Time }

func (r *relogioFalso) agora() time.Time       { return r.t }
func (r *relogioFalso) avanca(d time.Duration) { r.t = r.t.Add(d) }

func novoOverlayDeTeste() (*Overlay, *relogioFalso) {
	r := &relogioFalso{t: time.Date(2026, 9, 26, 15, 0, 0, 0, time.UTC)}
	// win nil de propósito: PushAmplitude/wake são seguros sem janela
	// (wake devolve cedo), e o que se testa aqui é o buffer, não o GDI.
	//
	// A fase começa em OverlayRecording porque desde 28-set-2026
	// PushAmplitude DESCARTA amostra fora da gravação (ver o comentário
	// dele). O zero value é OverlayHidden, e com ele todo teste deste
	// arquivo mediria um buffer que nunca recebeu nada — passando a
	// afirmar o vazio, não a janela.
	o := &Overlay{agora: r.agora, phase: dictation.OverlayRecording}
	return o, r
}

// TestOnda_ForaDeGravacaoNaoAcumula trava o portão em si.
//
// A captura de áudio é CONTÍNUA e não para quando a tecla é solta — o
// laço do dispositivo segue lendo e o runtime segue encaminhando todo
// frame para o overlay, sem olhar a fase. Sem este portão, a onda
// continuaria sendo alimentada durante o ASR com som que não está na
// transcrição, e cada amostra acordaria um redesenho.
func TestOnda_ForaDeGravacaoNaoAcumula(t *testing.T) {
	for _, fase := range []dictation.OverlayPhase{
		dictation.OverlayHidden,
		dictation.OverlayProcessing,
		dictation.OverlaySuccess,
	} {
		o, r := novoOverlayDeTeste()
		o.phase = fase
		alimenta(o, r, 40, 10*time.Millisecond, 0.7)
		if n := len(o.recentes(r.agora())); n != 0 {
			t.Errorf("fase %s acumulou %d amostras — a onda se alimenta fora da gravação", fase, n)
		}
	}

	// Vacuidade: em gravação as MESMAS amostras têm de entrar.
	o, r := novoOverlayDeTeste()
	alimenta(o, r, 40, 10*time.Millisecond, 0.7)
	if n := len(o.recentes(r.agora())); n == 0 {
		t.Fatal("nem em gravação a onda acumula — o teste acima não prova nada")
	}
}

// alimenta empurra n amplitudes iguais, espaçadas de passo — o
// comportamento de um dispositivo entregando pacotes de duração passo.
func alimenta(o *Overlay, r *relogioFalso, n int, passo time.Duration, rms float64) {
	for i := 0; i < n; i++ {
		o.PushAmplitude(rms)
		r.avanca(passo)
	}
}

// TestOnda_JanelaIndependeDoTamanhoDoPacote é o teste que trava o
// defeito corrigido em 26-set-2026.
//
// A captura entrega o que IAudioCaptureClient::GetNextPacketSize
// devolver — o período do dispositivo, tipicamente 10 ms, mas não
// garantido. Com um anel contado em QUADROS, uma placa de 20 ms
// guardava o dobro de TEMPO, cada barra passava a resumir o dobro de
// fala, e a onda virava uma cerca de barras iguais numa máquina e não
// na outra — sem nada no código dizendo por quê.
//
// A janela agora é por idade, então os dois dispositivos mostram o mesmo
// trecho de passado.
func TestOnda_JanelaIndependeDoTamanhoDoPacote(t *testing.T) {
	casos := []struct {
		nome  string
		passo time.Duration
	}{
		{"dispositivo de 10 ms", 10 * time.Millisecond},
		{"dispositivo de 20 ms", 20 * time.Millisecond},
		{"dispositivo de 3 ms", 3 * time.Millisecond},
	}
	for _, c := range casos {
		o, r := novoOverlayDeTeste()
		// 4 segundos de captura contínua: bem mais que JanelaDaOnda.
		alimenta(o, r, int(4*time.Second/c.passo), c.passo, 0.05)

		o.mu.Lock()
		amostras := o.amps
		o.mu.Unlock()
		if len(amostras) < 2 {
			t.Fatalf("%s: só %d amostras retidas", c.nome, len(amostras))
		}

		span := amostras[len(amostras)-1].at.Sub(amostras[0].at)
		// Tolerância de um passo: o descarte acontece em múltiplos do
		// pacote, então o trecho retido não fecha exato na janela.
		if span > JanelaDaOnda || span < JanelaDaOnda-2*c.passo {
			t.Errorf("%s: a onda retém %v de passado, want ~%v — "+
				"a janela não pode depender do período do dispositivo",
				c.nome, span, JanelaDaOnda)
		}
	}
}

// TestOnda_DescartaPorIdade: o que passou da janela some, mesmo que
// caibam poucas amostras.
func TestOnda_DescartaPorIdade(t *testing.T) {
	o, r := novoOverlayDeTeste()

	o.PushAmplitude(0.9) // esta vai envelhecer
	r.avanca(JanelaDaOnda + 100*time.Millisecond)
	o.PushAmplitude(0.1)

	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.amps) != 1 {
		t.Fatalf("len(amps) = %d, want 1 — a amostra velha deveria ter saído", len(o.amps))
	}
	if o.amps[0].rms != 0.1 {
		t.Errorf("sobrou a amostra errada: rms = %v, want 0.1", o.amps[0].rms)
	}
}

// TestOnda_TetoDeMemoria: dispositivo de período muito curto não faz o
// buffer crescer sem limite dentro da janela.
func TestOnda_TetoDeMemoria(t *testing.T) {
	o, r := novoOverlayDeTeste()
	// Passo de 100 µs: 1,3 s daria 13.000 amostras sem o teto.
	alimenta(o, r, 20000, 100*time.Microsecond, 0.05)

	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.amps) > maxAmplitudeSamples {
		t.Errorf("len(amps) = %d, passou do teto de %d", len(o.amps), maxAmplitudeSamples)
	}
}

// TestOnda_RecentesPreservaAOrdem: Bars espera a mais recente por
// último. Inverter aqui faria a onda rolar para o lado errado — defeito
// visual que nenhum teste de Bars pegaria.
func TestOnda_RecentesPreservaAOrdem(t *testing.T) {
	o, r := novoOverlayDeTeste()
	for i, v := range []float64{0.1, 0.2, 0.3, 0.4} {
		o.PushAmplitude(v)
		if i < 3 {
			r.avanca(10 * time.Millisecond)
		}
	}

	o.mu.Lock()
	got := o.recentes(r.agora())
	o.mu.Unlock()

	want := []float64{0.1, 0.2, 0.3, 0.4}
	if len(got) != len(want) {
		t.Fatalf("recentes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recentes() = %v, want %v (mais recente por último)", got, want)
		}
	}
}

// TestOnda_SetPhaseRecordingLimpaAOndaAnterior: começar um ditado não
// pode herdar a onda do ditado passado.
func TestOnda_SetPhaseRecordingLimpaAOndaAnterior(t *testing.T) {
	o, r := novoOverlayDeTeste()
	alimenta(o, r, 10, 10*time.Millisecond, 0.5)

	o.SetPhase(dictation.OverlayRecording)

	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.amps) != 0 {
		t.Errorf("len(amps) = %d depois de entrar em Recording, want 0", len(o.amps))
	}
}

// TestOnda_JanelaCobreVariasBarras: a janela precisa render mais de uma
// amostra POR BARRA num dispositivo típico, senão Bars passa a preencher
// a esquerda com silêncio e a onda fica encolhida à direita.
func TestOnda_JanelaCobreVariasBarras(t *testing.T) {
	const passoTipico = 10 * time.Millisecond
	amostrasNaJanela := int(JanelaDaOnda / passoTipico)
	if amostrasNaJanela < BarCount {
		t.Fatalf("a janela cabe %d amostras num dispositivo de %v e a onda tem %d barras — "+
			"Bars preencheria a esquerda com silêncio", amostrasNaJanela, passoTipico, BarCount)
	}
	msPorBarra := JanelaDaOnda.Milliseconds() / int64(BarCount)
	// Acima de ~100 ms cada barra resume uma sílaba inteira e todas
	// ficam iguais — foi exatamente o sintoma corrigido no ADR-014.
	if msPorBarra > 100 {
		t.Errorf("cada barra resume %d ms; acima de ~100 ms a onda vira uma cerca", msPorBarra)
	}
}
