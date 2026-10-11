package main

import (
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

var criteriosPadrao = Criterios{FallbackMax: 0.05, P95Max: 2500 * time.Millisecond}

// bom é um candidato que passa em tudo: 100 chamadas, nenhuma falha, sem
// fallback, p95 de 1 s. Cada teste estraga uma coisa.
func bom(prov, modelo string, custo1000, f1 float64) Metricas {
	return Metricas{
		Cand:                 Candidato{Provedor: prov, Modelo: modelo},
		Chamadas:             100,
		ChamadasSemArmadilha: 100,
		Latencia:             metrics.Stats{N: 99, P50: 0.8, P95: 1.0},
		CustoPor1000:         custo1000,
		F1:                   f1,
	}
}

func TestAvaliar_FronteirasDaRegra(t *testing.T) {
	casos := []struct {
		nome   string
		muda   func(m *Metricas)
		passou bool
		motivo string
	}{
		{"tudo em ordem", func(m *Metricas) {}, true, ""},
		{"uma chamada com falha crítica depois da guarda reprova", func(m *Metricas) { m.ChamadasCriticasDepois = 1 }, false, "falha crítica DEPOIS da guarda"},
		{"falha crítica ANTES da guarda não reprova", func(m *Metricas) { m.ChamadasCriticasAntes = 40 }, true, ""},
		{"fallback exatamente no limite passa", func(m *Metricas) { m.FallbacksSemArmadilha = 5 }, true, ""},
		{"um fallback acima do limite reprova", func(m *Metricas) { m.FallbacksSemArmadilha = 6 }, false, "fallback de 6,0% (máximo 5,0%)"},
		{"o fallback das armadilhas não conta", func(m *Metricas) { m.FallbackTotal = 30 }, true, ""},
		{"p95 exatamente no limite passa", func(m *Metricas) { m.Latencia.P95 = 2.5 }, true, ""},
		{"p95 acima do limite reprova", func(m *Metricas) { m.Latencia.P95 = 2.51 }, false, "p95 de 2,51 s (máximo 2,50 s)"},
		{"p50 alto não reprova", func(m *Metricas) { m.Latencia.P50 = 9 }, true, ""},
		{"sem chamada nenhuma reprova", func(m *Metricas) { m.Chamadas, m.ChamadasSemArmadilha = 0, 0 }, false, "nenhuma chamada foi ao provedor"},
		{"sem latência medida reprova", func(m *Metricas) { m.Latencia = metrics.Stats{} }, false, "nenhuma latência morna medida"},
		{"só armadilhas reprova (sem dado)", func(m *Metricas) { m.ChamadasSemArmadilha = 0 }, false, "nenhuma chamada fora das frases-armadilha"},
		{"abortado por chave recusada reprova", func(m *Metricas) { m.Aborto = "chave_recusada" }, false, "abortado: o provedor respondeu chave_recusada"},
	}
	for _, c := range casos {
		m := bom(ia.OpenAI, "x", 0.1, 0.9)
		c.muda(&m)
		v := avaliar(m, criteriosPadrao)
		if v.Passou != c.passou {
			t.Errorf("%s: Passou = %v (%v), quero %v", c.nome, v.Passou, v.Motivos, c.passou)
		}
		if c.motivo != "" && !strings.Contains(strings.Join(v.Motivos, "|"), c.motivo) {
			t.Errorf("%s: motivos %v não têm %q", c.nome, v.Motivos, c.motivo)
		}
		if c.passou && len(v.Motivos) != 0 {
			t.Errorf("%s: passou mas tem motivos %v", c.nome, v.Motivos)
		}
	}
}

func TestAvaliar_OsLimiaresVemDosCriterios(t *testing.T) {
	m := bom(ia.OpenAI, "x", 0.1, 0.9)
	m.FallbacksSemArmadilha = 10 // 10%
	m.Latencia.P95 = 3.0
	if avaliar(m, criteriosPadrao).Passou {
		t.Error("passou com 10% de fallback e p95 de 3 s no padrão")
	}
	if !avaliar(m, Criterios{FallbackMax: 0.10, P95Max: 3 * time.Second}).Passou {
		t.Error("não passou com os limiares afrouxados por flag")
	}
	if got := avaliar(m, criteriosPadrao); len(got.Motivos) != 2 {
		t.Errorf("os dois motivos deviam aparecer: %v", got.Motivos)
	}
}

func TestEscolher_MaisBaratoPorProvedor(t *testing.T) {
	ms := []Metricas{
		bom(ia.OpenAI, "caro", 0.50, 0.99),
		bom(ia.OpenAI, "barato", 0.10, 0.80),
		bom(ia.Anthropic, "unico", 1.30, 0.95),
		bom(ia.OpenAI, "mais-barato-mas-reprovado", 0.01, 0.99),
	}
	ms[3].ChamadasCriticasDepois = 1
	d := escolher(ms, criteriosPadrao, false)
	if d.Vencedores[ia.OpenAI] != "openai:barato" {
		t.Errorf("vencedor OpenAI = %q, quero o mais barato dos aprovados", d.Vencedores[ia.OpenAI])
	}
	if d.Vencedores[ia.Anthropic] != "anthropic:unico" {
		t.Errorf("vencedor Anthropic = %q", d.Vencedores[ia.Anthropic])
	}
	if len(d.Vencedores) != 2 {
		t.Errorf("vencedores = %v, quero um por provedor", d.Vencedores)
	}
	want := []bool{true, true, true, false}
	for i, v := range d.Vereditos {
		if v.Passou != want[i] {
			t.Errorf("veredito %d = %v", i, v.Passou)
		}
	}
}

func TestEscolher_EmpateDeCustoVaiParaOMaiorF1(t *testing.T) {
	ms := []Metricas{
		bom(ia.OpenAI, "a", 0.10, 0.90),
		bom(ia.OpenAI, "b", 0.10, 0.95),
		bom(ia.OpenAI, "c", 0.10, 0.92),
	}
	if got := escolher(ms, criteriosPadrao, false).Vencedores[ia.OpenAI]; got != "openai:b" {
		t.Errorf("vencedor = %q, quero o de maior F1 (b)", got)
	}
	// A diferença de custo abaixo de um milionésimo é empate.
	ms = []Metricas{bom(ia.OpenAI, "a", 0.1000004, 0.90), bom(ia.OpenAI, "b", 0.1, 0.95)}
	if got := escolher(ms, criteriosPadrao, false).Vencedores[ia.OpenAI]; got != "openai:b" {
		t.Errorf("com custo empatado, vencedor = %q, quero b (maior F1)", got)
	}
	// Custo diferente: F1 não salva o mais caro.
	ms = []Metricas{bom(ia.OpenAI, "a", 0.11, 0.99), bom(ia.OpenAI, "b", 0.10, 0.10)}
	if got := escolher(ms, criteriosPadrao, false).Vencedores[ia.OpenAI]; got != "openai:b" {
		t.Errorf("vencedor = %q, quero b (o mais barato, com F1 baixo)", got)
	}
}

func TestEscolher_EmpateTotalDesempataPeloRotulo(t *testing.T) {
	ms := []Metricas{bom(ia.OpenAI, "z", 0.10, 0.9), bom(ia.OpenAI, "a", 0.10, 0.9), bom(ia.OpenAI, "m", 0.10, 0.9)}
	for i := 0; i < 3; i++ { // a ordem de entrada não pode mudar o resultado
		if got := escolher(ms, criteriosPadrao, false).Vencedores[ia.OpenAI]; got != "openai:a" {
			t.Errorf("vencedor = %q, quero openai:a", got)
		}
		ms = append(ms[1:], ms[0])
	}
}

func TestEscolher_NinguemPassaNaoTemVencedor(t *testing.T) {
	ms := []Metricas{bom(ia.OpenAI, "a", 0.1, 0.9), bom(ia.Anthropic, "b", 0.1, 0.9)}
	ms[0].Latencia.P95 = 4
	ms[1].ChamadasCriticasDepois = 2
	d := escolher(ms, criteriosPadrao, false)
	if len(d.Vencedores) != 0 {
		t.Errorf("vencedores = %v, quero nenhum", d.Vencedores)
	}
	if d.Vereditos[0].Passou || d.Vereditos[1].Passou {
		t.Error("algum passou")
	}
}

func TestEscolher_RodadaIncompletaNaoDecide(t *testing.T) {
	d := escolher([]Metricas{bom(ia.OpenAI, "a", 0.1, 0.9)}, criteriosPadrao, true)
	if !d.Incompleta || len(d.Vencedores) != 0 {
		t.Errorf("decisão de rodada incompleta: %+v", d)
	}
	if len(d.Vereditos) != 1 {
		t.Error("os vereditos parciais continuam calculados")
	}
}

func TestMelhorQue(t *testing.T) {
	a, b := bom(ia.OpenAI, "a", 0.10, 0.9), bom(ia.OpenAI, "b", 0.20, 0.9)
	if !melhorQue(a, b) || melhorQue(b, a) {
		t.Error("o mais barato vence")
	}
	if melhorQue(a, a) {
		t.Error("ninguém é melhor que si mesmo")
	}
}
