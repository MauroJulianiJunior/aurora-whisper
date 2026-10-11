package retention

import (
	"testing"
	"time"
	_ "time/tzdata" // America/Sao_Paulo mesmo numa máquina sem a base de fusos
)

// fusosDoCorte são os fusos em que o relógio do Agent pode estar: UTC, o do
// Mauro (America/Sao_Paulo, negativo) e um positivo extremo.
func fusosDoCorte(t *testing.T) []*time.Location {
	t.Helper()
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("America/Sao_Paulo: %v", err)
	}
	return []*time.Location{time.UTC, sp, time.FixedZone("UTC+14", 14*3600)}
}

// comFusoLocal troca o time.Local durante o teste (o relógio do Agent é o do
// sistema, em hora local).
func comFusoLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	antes := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = antes })
}

// A propriedade do corte do pré-filtro (ADR-019, "Decisões da F10"), hora a
// hora, por quatro anos (um bissexto no meio), com o agora no fuso local do
// Agent em UTC, em America/Sao_Paulo e num fuso positivo: o corte nunca
// exclui um ditado vencido (no vencimento, e nas horas seguintes, ele está
// dentro do corte), e nada acima do corte venceu (só traz a mais).
func TestCorteDoAtivo_NuncaExcluiUmVencido(t *testing.T) {
	if testing.Short() {
		t.Skip("varre quatro anos hora a hora, em três fusos")
	}
	inicio := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	fim := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, loc := range fusosDoCorte(t) {
		t.Run(loc.String(), func(t *testing.T) {
			comFusoLocal(t, loc)
			// Do lado do ditado: criado em c, vence em ActiveExpiryAt(c), e
			// no vencimento e depois dele está dentro do corte.
			for c := inicio; c.Before(fim); c = c.Add(time.Hour) {
				vence := ActiveExpiryAt(c)
				for _, agora := range []time.Time{vence, vence.Add(time.Nanosecond), vence.Add(time.Hour), vence.Add(72 * time.Hour)} {
					agora = agora.In(time.Local)
					if agora.Before(ActiveExpiryAt(c)) {
						t.Fatalf("o ditado de %s não venceu em %s", c, agora)
					}
					if c.After(CorteDoAtivo(agora)) {
						t.Fatalf("o ditado de %s venceu em %s e ficou acima do corte %s", c, agora, CorteDoAtivo(agora))
					}
				}
			}
			// Do lado do agora: nada acima do corte venceu. O estouro do fim
			// do mês é de no máximo 3 dias: 80 horas cobrem a zona.
			for n := inicio; n.Before(fim); n = n.Add(time.Hour) {
				agora := n.In(time.Local)
				corte := CorteDoAtivo(agora)
				for k := 0; k <= 80; k++ {
					c := corte.Add(time.Duration(k)*time.Hour + time.Nanosecond)
					if !agora.Before(ActiveExpiryAt(c)) {
						t.Fatalf("agora %s: o ditado de %s venceu e está acima do corte %s", agora, c, corte)
					}
				}
			}
		})
	}
}

// O corte é o mesmo instante com o agora em qualquer fuso, e o caso que o
// fuso local erraria: em America/Sao_Paulo, com agora = 2027-03-01T01:00Z
// (28/fev 22:00 local), o ditado de 2026-09-01T00:30Z venceu e está dentro do
// corte; contado no fuso local, o corte cairia em 29/ago e o deixaria de fora.
func TestCorteDoAtivo_EmUTC(t *testing.T) {
	agora := time.Date(2027, time.March, 1, 1, 0, 0, 0, time.UTC)
	ditado := time.Date(2026, time.September, 1, 0, 30, 0, 0, time.UTC)
	for _, loc := range fusosDoCorte(t) {
		comFusoLocal(t, loc)
		local := agora.In(time.Local)
		if a, b := CorteDoAtivo(local), CorteDoAtivo(agora); !a.Equal(b) || a.Location() != time.UTC {
			t.Errorf("%s: o corte com o agora local é %s; com o agora em UTC, %s", loc, a, b)
		}
		if local.Before(ActiveExpiryAt(ditado)) || ditado.After(CorteDoAtivo(local)) {
			t.Errorf("%s: o ditado de %s venceu em %s e ficou fora do corte %s", loc, ditado, local, CorteDoAtivo(local))
		}
	}
	sp := fusosDoCorte(t)[1]
	if errado := agora.In(sp).AddDate(0, -ActiveRetentionMonths, 0); !ditado.After(errado) {
		t.Fatalf("o cenário não exercita o fuso: o corte local %s já incluiria o ditado", errado.UTC())
	}
}
