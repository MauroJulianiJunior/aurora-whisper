package retention

import (
	"testing"
	"time"
)

// O freio dos 6 meses no PC (ADR-019, "Decisões da F10", D2): só a última
// rodada há MAIS de 7 dias dispara; sem rodada gravada, nunca; e o relógio que
// volta não freia.
func TestFreioDaRodada(t *testing.T) {
	rodada := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		nome  string
		em    *time.Time
		agora time.Time
		quer  bool
	}{
		{"sem rodada gravada", nil, rodada.AddDate(1, 0, 0), false},
		{"uma hora depois", &rodada, rodada.Add(time.Hour), false},
		{"exatamente 7 dias", &rodada, rodada.Add(FreioDaRodadaApos), false},
		{"7 dias e 1 ns", &rodada, rodada.Add(FreioDaRodadaApos + time.Nanosecond), true},
		{"8 dias", &rodada, rodada.AddDate(0, 0, 8), true},
		{"o relógio voltou um ano", &rodada, rodada.AddDate(-1, 0, 0), false},
		{"num fuso diferente, 8 dias", &rodada, rodada.AddDate(0, 0, 8).In(time.FixedZone("UTC+14", 14*3600)), true},
	}
	for _, c := range cases {
		if got := FreioDaRodada(c.em, c.agora); got != c.quer {
			t.Errorf("%s: FreioDaRodada = %v, quero %v", c.nome, got, c.quer)
		}
	}
}

func TestDiasParado(t *testing.T) {
	rodada := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		em    *time.Time
		agora time.Time
		quer  int
	}{
		{nil, rodada, 0},
		{&rodada, rodada.Add(-time.Hour), 0},
		{&rodada, rodada.Add(23 * time.Hour), 0},
		{&rodada, rodada.AddDate(0, 0, 8).Add(time.Hour), 8},
	} {
		if got := DiasParado(c.em, c.agora); got != c.quer {
			t.Errorf("DiasParado(%v, %s) = %d, quero %d", c.em, c.agora, got, c.quer)
		}
	}
}
