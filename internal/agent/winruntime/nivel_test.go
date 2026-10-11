package winruntime

import (
	"math"
	"strings"
	"testing"
	"time"
)

// senoide gera um tom de amplitude pico (fração do fundo de escala) —
// um sinal de nível conhecido, para as asserções serem contra
// aritmética e não contra "o que saiu".
func senoide(n int, pico float64) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(pico * 32767 * math.Sin(2*math.Pi*float64(i)/64))
	}
	return out
}

func TestNivelDBFS_FundoDeEscala(t *testing.T) {
	pico, rms := nivelDBFS(senoide(6400, 1.0))
	if pico < -0.5 || pico > 0.5 {
		t.Errorf("pico = %.1f dBFS, want ~0 para sinal em fundo de escala", pico)
	}
	// RMS de uma senoide é pico/raiz(2) = -3,01 dBFS.
	if rms < -3.5 || rms > -2.5 {
		t.Errorf("rms = %.1f dBFS, want ~-3,0 (senoide em fundo de escala)", rms)
	}
}

func TestNivelDBFS_MeiaEscalaEhMenosSeis(t *testing.T) {
	// Metade da amplitude é sempre -6,02 dBFS. Se esta conta sair
	// errada, todo o resto do diagnóstico mente.
	pico, _ := nivelDBFS(senoide(6400, 0.5))
	if pico < -6.5 || pico > -5.5 {
		t.Errorf("pico = %.1f dBFS, want ~-6,0 para metade do fundo de escala", pico)
	}
}

func TestNivelDBFS_SilencioNaoEhInfinito(t *testing.T) {
	// -inf quebraria a formatação do log e a comparação em dicaDeNivel.
	pico, rms := nivelDBFS(make([]int16, 1600))
	if math.IsInf(pico, 0) || math.IsInf(rms, 0) {
		t.Fatalf("silêncio devolveu infinito: pico=%v rms=%v", pico, rms)
	}
	if pico != silencioDBFS || rms != silencioDBFS {
		t.Errorf("silêncio = (%v, %v), want (%v, %v)", pico, rms, silencioDBFS, silencioDBFS)
	}
}

func TestNivelDBFS_BufferVazioNaoEstoura(t *testing.T) {
	pico, rms := nivelDBFS(nil)
	if pico != silencioDBFS || rms != silencioDBFS {
		t.Errorf("nivelDBFS(nil) = (%v, %v), want (%v, %v)", pico, rms, silencioDBFS, silencioDBFS)
	}
}

// TestNivelDBFS_MinInt16NaoViraPositivo: -32768 não tem simétrico em
// int16, e negar ingenuamente devolve -32768 de novo. Se o valor
// absoluto fosse calculado em int, o pico sairia negativo e o log
// mentiria justo no sinal mais forte possível.
func TestNivelDBFS_MinInt16NaoViraPositivo(t *testing.T) {
	pico, _ := nivelDBFS([]int16{math.MinInt16, 0, 0, 0})
	if pico < -0.5 || pico > 0.5 {
		t.Errorf("pico com MinInt16 = %.1f dBFS, want ~0", pico)
	}
}

func TestNivelDBFS_PicoNuncaMenorQueRMS(t *testing.T) {
	for _, amp := range []float64{1.0, 0.5, 0.1, 0.01, 0.001} {
		pico, rms := nivelDBFS(senoide(6400, amp))
		if rms > pico {
			t.Errorf("amplitude %v: rms (%.1f) > pico (%.1f), o que é impossível", amp, rms, pico)
		}
	}
}

func TestDuracaoDe(t *testing.T) {
	if got := duracaoDe(make([]int16, 16000), 16000); got != time.Second {
		t.Errorf("duracaoDe(16000 amostras a 16 kHz) = %v, want 1s", got)
	}
	if got := duracaoDe(make([]int16, 100), 0); got != 0 {
		t.Errorf("taxa zero = %v, want 0 (não pode dividir por zero)", got)
	}
	if got := duracaoDe(nil, 16000); got != 0 {
		t.Errorf("buffer vazio = %v, want 0", got)
	}
}

// TestDicaDeNivel_SeparaAsCausasOpostas é o teste que dá sentido ao
// resto: um log que diz "pico_dbfs=-91,4" só ajuda quem já sabe o que
// esperar. A dica precisa apontar microfone mudo e recusa do detector
// para lados DIFERENTES — são as duas causas de no_speech, e têm
// correções opostas.
func TestDicaDeNivel_SeparaAsCausasOpostas(t *testing.T) {
	casos := []struct {
		nome      string
		pico      float64
		contem    string
		naoContem string
	}{
		{"microfone mudo", -95, "mudo", "detector"},
		{"quase nada", -60, "dispositivo de entrada", "detector"},
		{"som fraco", -40, "ganho", "detector"},
		{"fala normal", -12, "detector", "mudo"},
		{"saturado", -0.2, "clipping", "detector"},
	}
	for _, c := range casos {
		got := dicaDeNivel(c.pico)
		if !strings.Contains(got, c.contem) {
			t.Errorf("%s (pico %.1f): dica %q não menciona %q", c.nome, c.pico, got, c.contem)
		}
		if strings.Contains(got, c.naoContem) {
			t.Errorf("%s (pico %.1f): dica %q aponta para a causa errada (%q)",
				c.nome, c.pico, got, c.naoContem)
		}
	}
}

// TestDicaDeNivel_NuncaVazia: uma dica em branco no log é pior que
// nenhum campo, porque parece que faltou informação.
func TestDicaDeNivel_NuncaVazia(t *testing.T) {
	for pico := -130.0; pico <= 6.0; pico += 0.5 {
		if dicaDeNivel(pico) == "" {
			t.Fatalf("dicaDeNivel(%.1f) devolveu string vazia", pico)
		}
	}
}

// TestDicaDeNivel_FalaRealMedidaCaiNaFaixaNormal ancora os cortes na
// medição de verdade: as 46 amostras gravadas nesta máquina têm pico
// entre -20 e -6 dBFS (ver overlay.WaveFloorDBFS). Se alguém mexer nos
// limiares e fala real passar a ser chamada de "fraca", isto falha.
func TestDicaDeNivel_FalaRealMedidaCaiNaFaixaNormal(t *testing.T) {
	for _, pico := range []float64{-20, -16, -13, -9, -6} {
		if got := dicaDeNivel(pico); !strings.Contains(got, "detector") {
			t.Errorf("pico %.0f dBFS é fala real medida, mas a dica diz %q", pico, got)
		}
	}
}
