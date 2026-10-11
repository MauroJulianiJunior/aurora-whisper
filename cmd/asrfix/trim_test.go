package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

// sinal monta um sinal de teste: `quieto` segundos de ruído baixo, depois
// `falado` segundos de um tom forte que faz as vezes de fala.
func sinal(rate int, quieto, falado float64, nivelQuieto, nivelFalado float64) []float64 {
	n := int((quieto + falado) * float64(rate))
	corte := int(quieto * float64(rate))
	out := make([]float64, n)
	for i := range out {
		amp := nivelQuieto
		if i >= corte {
			amp = nivelFalado
		}
		out[i] = amp * math.Sin(2*math.Pi*120*float64(i)/float64(rate))
	}
	return out
}

func TestDecidirTrim_ApagaPreRollSilencioso(t *testing.T) {
	// 2 s de ruído baixo, depois 4 s de fala.
	x := sinal(16000, 2, 4, 0.001, 0.3)

	d := decidirTrim(x, 16000, 2*time.Second)
	if d.Recusado {
		t.Fatalf("recusou aparar pre-roll silencioso: %s", d.Motivo)
	}
	if d.Cortar != 32000 {
		t.Errorf("Cortar = %d, want 32000", d.Cortar)
	}
}

// TestDecidirTrim_ApagaPreRollCONTAMINADO é o teste que a primeira versão
// desta lógica reprovaria.
//
// Ela recusava cortar quando havia som alto no trecho, presumindo que som
// alto ali era fala DESTA frase. Mas a captura é contínua entre gravações:
// som alto no pre-roll é a cauda da frase ANTERIOR, medida em 26 dos 45
// pares consecutivos do dataset real, com até 1,01 s de amostras
// idênticas. Recusar ali deixava justamente os arquivos mais contaminados
// sem correção.
func TestDecidirTrim_ApagaPreRollCONTAMINADO(t *testing.T) {
	rate := 16000
	// 1,2 s de fala alta (a frase anterior), 0,8 s de silêncio, 4 s da
	// frase desta gravação.
	x := append(sinal(rate, 0, 1.2, 0, 0.35), sinal(rate, 0.8, 4, 0.0005, 0.3)...)

	d := decidirTrim(x, rate, 2*time.Second)
	if d.Recusado {
		t.Fatalf("recusou aparar pre-roll CONTAMINADO: %s\n"+
			"som alto no pre-roll é a cauda da frase anterior — é o motivo PARA cortar, não contra", d.Motivo)
	}
	if d.Cortar != 2*rate {
		t.Errorf("Cortar = %d, want %d", d.Cortar, 2*rate)
	}
	if d.ContaminadoFrac < 0.5 {
		t.Errorf("ContaminadoFrac = %.2f, want alto — o relatório precisa mostrar que havia contaminação", d.ContaminadoFrac)
	}
}

// TestDecidirTrim_RecusaQuandoCorteEntraNaFala cobre o ring buffer
// grampeado: a gravação começou com menos pre-roll que o nominal, então
// cortar 2 s fixos comeria sílaba.
func TestDecidirTrim_RecusaQuandoCorteEntraNaFala(t *testing.T) {
	// Só 0,5 s de silêncio antes da fala: cortar 2 s entra nela.
	x := sinal(16000, 0.5, 6, 0.001, 0.3)

	d := decidirTrim(x, 16000, 2*time.Second)
	if !d.Recusado {
		t.Fatalf("aparou %d amostras onde a fala já tinha começado — comeria sílaba", d.Cortar)
	}
}

func TestDecidirTrim_RecusaCorteGrandeDemais(t *testing.T) {
	x := sinal(16000, 1, 2, 0.001, 0.3) // 3 s no total

	d := decidirTrim(x, 16000, 2*time.Second)
	if !d.Recusado {
		t.Fatalf("aparou 2s de um arquivo de 3s (Cortar=%d)", d.Cortar)
	}
	// Afirmar o MOTIVO, e nao so que recusou: sem isto, desligar esta
	// guarda nao quebra o teste, porque a trava de fala atravessando o
	// corte recusa a mesma amostra por outro motivo, e a lacuna passa.
	if !strings.Contains(d.Motivo, "metade ou mais") {
		t.Errorf("Motivo = %q, want a recusa por tamanho do corte", d.Motivo)
	}
}

func TestDecidirTrim_DuracaoZeroNaoCorta(t *testing.T) {
	x := sinal(16000, 2, 4, 0.001, 0.3)
	for _, dur := range []time.Duration{0, -1 * time.Second} {
		if d := decidirTrim(x, 16000, dur); d.Cortar != 0 || d.Recusado {
			t.Errorf("decidirTrim(dur=%s) = {Cortar:%d Recusado:%v}, want sem corte e sem recusa", dur, d.Cortar, d.Recusado)
		}
	}
}
