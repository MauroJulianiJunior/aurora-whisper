package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// Nada aqui lê o dataset real nem sobe servidor: as amostras são sintéticas,
// em memória ou numa pasta temporária.

// amostraSintetica tem seg segundos a 16 kHz, com todas as amostras iguais a
// valor (nunca zero, para o silêncio da pausa ser distinguível).
func amostraSintetica(id, ref string, seg float64, valor int16) trecho {
	pcm := make([]int16, int(seg*16000))
	for i := range pcm {
		pcm[i] = valor
	}
	return trecho{id: id, referencia: ref, taxa: 16000, pcm: pcm}
}

func quatroAmostras() []trecho {
	return []trecho{
		amostraSintetica("a", "Frase a.", 2, 1),
		amostraSintetica("b", " Frase b. ", 3, 2),
		amostraSintetica("c", "Frase c.", 4, 3),
		amostraSintetica("d", "Frase d.", 1, 4),
	}
}

func TestCompor_ConcatenaComPausaAtePassarDoAlvo(t *testing.T) {
	longos, err := compor(quatroAmostras(), []time.Duration{6 * time.Second}, 2, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("compor: %v", err)
	}
	if len(longos) != 2 {
		t.Fatalf("%d áudios, quero 2", len(longos))
	}

	// O primeiro: a (2 s) + 0,5 + b (3 s) = 5,5 s, abaixo de 6; + 0,5 + c (4 s) = 10 s.
	l := longos[0]
	if l.id != "longo-6s-1" || l.alvo != 6*time.Second {
		t.Errorf("id = %q, alvo = %v", l.id, l.alvo)
	}
	if l.duracao() != 10*time.Second {
		t.Errorf("duração = %v, quero 10 s", l.duracao())
	}
	if !reflect.DeepEqual(l.fontes, []string{"a", "b", "c"}) {
		t.Errorf("fontes = %v", l.fontes)
	}
	if l.referencia != "Frase a. Frase b. Frase c." {
		t.Errorf("referência = %q: as das amostras, em ordem, separadas por um espaço", l.referencia)
	}
	// O áudio é exatamente a, silêncio, b, silêncio, c.
	esperado := []struct {
		de, ate int
		valor   int16
	}{
		{0, 32000, 1}, {32000, 40000, 0}, {40000, 88000, 2}, {88000, 96000, 0}, {96000, 160000, 3},
	}
	if len(l.pcm) != 160000 {
		t.Fatalf("%d amostras, quero 160000", len(l.pcm))
	}
	for _, e := range esperado {
		for i := e.de; i < e.ate; i++ {
			if l.pcm[i] != e.valor {
				t.Fatalf("amostra %d = %d, quero %d (trecho [%d, %d))", i, l.pcm[i], e.valor, e.de, e.ate)
			}
		}
	}

	// O segundo continua de onde o primeiro parou (d, depois a do começo).
	if got := longos[1].fontes; !reflect.DeepEqual(got, []string{"d", "a", "b"}) {
		t.Errorf("fontes do segundo = %v, quero [d a b]: o cursor continua, e não repete a fala do primeiro", got)
	}
	if longos[1].id != "longo-6s-2" {
		t.Errorf("id do segundo = %q", longos[1].id)
	}
}

func TestCompor_SemPausaEVariasDuracoes(t *testing.T) {
	longos, err := compor(quatroAmostras(), []time.Duration{3 * time.Second, 90 * time.Second}, 1, 0)
	if err != nil {
		t.Fatalf("compor: %v", err)
	}
	if len(longos) != 2 || longos[0].id != "longo-3s-1" || longos[1].id != "longo-90s-1" {
		t.Fatalf("áudios = %v", ids(longos))
	}
	if longos[0].duracao() != 5*time.Second { // a (2) + b (3), sem pausa
		t.Errorf("duração do de 3 s = %v, quero 5 s", longos[0].duracao())
	}
	// 90 s passa por todas as amostras várias vezes; nunca fica abaixo do alvo.
	if d := longos[1].duracao(); d < 90*time.Second || d >= 94*time.Second {
		t.Errorf("duração do de 90 s = %v: tem de passar do alvo por menos que a maior amostra", d)
	}
}

func ids(ts []trecho) []string {
	var s []string
	for _, t := range ts {
		s = append(s, t.id)
	}
	return s
}

func TestCompor_Deterministico(t *testing.T) {
	a, _ := compor(quatroAmostras(), []time.Duration{30 * time.Second, 60 * time.Second}, 2, 300*time.Millisecond)
	b, _ := compor(quatroAmostras(), []time.Duration{30 * time.Second, 60 * time.Second}, 2, 300*time.Millisecond)
	if !reflect.DeepEqual(a, b) {
		t.Error("o mesmo comando montou áudios diferentes")
	}
}

func TestCompor_Erros(t *testing.T) {
	outraTaxa := quatroAmostras()
	outraTaxa[2].taxa = 8000
	vazia := quatroAmostras()
	vazia[1].pcm = nil
	for _, c := range []struct {
		nome     string
		amostras []trecho
		alvos    []time.Duration
		por      int
		pausa    time.Duration
		trecho   string
	}{
		{"sem amostras", nil, []time.Duration{time.Second}, 1, 0, "nenhuma amostra"},
		{"taxas diferentes", outraTaxa, []time.Duration{time.Second}, 1, 0, "Hz"},
		{"amostra sem áudio", vazia, []time.Duration{time.Second}, 1, 0, "não tem áudio"},
		{"por-duracao zero", quatroAmostras(), []time.Duration{time.Second}, 0, 0, "por-duracao"},
		{"pausa negativa", quatroAmostras(), []time.Duration{time.Second}, 1, -time.Second, "pausa"},
		{"alvo zero", quatroAmostras(), []time.Duration{0}, 1, 0, "alvo"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			_, err := compor(c.amostras, c.alvos, c.por, c.pausa)
			if err == nil || !strings.Contains(err.Error(), c.trecho) {
				t.Errorf("erro = %v, quero um que fale de %q", err, c.trecho)
			}
		})
	}
}

func TestLerDuracoes(t *testing.T) {
	ds, err := lerDuracoes(" 30s, 1m ,90s,")
	if err != nil || !reflect.DeepEqual(ds, []time.Duration{30 * time.Second, time.Minute, 90 * time.Second}) {
		t.Errorf("lerDuracoes = %v, %v", ds, err)
	}
	for _, ruim := range []string{"", ",", "30", "-5s", "0s", "trinta"} {
		if _, err := lerDuracoes(ruim); err == nil {
			t.Errorf("lerDuracoes(%q) aceitou", ruim)
		}
	}
}

func TestRotuloDuracao(t *testing.T) {
	for d, want := range map[time.Duration]string{30 * time.Second: "30s", 90 * time.Second: "90s", 1500 * time.Millisecond: "2s"} {
		if got := rotuloDuracao(d); got != want {
			t.Errorf("rotuloDuracao(%v) = %q, want %q", d, got, want)
		}
	}
}

// TestMontagem_WAVIdaEVolta: o áudio montado, gravado pelo mesmo WriteWAV do
// dataset, volta igual — taxa, amostras e duração.
func TestMontagem_WAVIdaEVolta(t *testing.T) {
	longos, err := compor(quatroAmostras(), []time.Duration{6 * time.Second}, 1, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("compor: %v", err)
	}
	caminho := filepath.Join(t.TempDir(), "longo.wav")
	if err := dataset.WriteWAV(caminho, longos[0].taxa, longos[0].pcm); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}
	taxa, pcm, err := dataset.ReadWAV(caminho)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}
	if taxa != 16000 || !reflect.DeepEqual(pcm, longos[0].pcm) {
		t.Errorf("o WAV não voltou igual: taxa %d, %d amostras (quero %d)", taxa, len(pcm), len(longos[0].pcm))
	}
	if d, err := dataset.Duration(caminho); err != nil || d != longos[0].duracao() {
		t.Errorf("Duration = %v, %v; quero %v", d, err, longos[0].duracao())
	}
}
