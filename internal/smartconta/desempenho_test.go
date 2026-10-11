package smartconta

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A meta da ADR-021 (C2): 500 pares montados e resolvidos em menos de 25 ms.
//
// O que a medição mostrou (10-out-2026, i7-13650HX, go test -bench):
//
//	                         antes dos ajustes    agora
//	BenchmarkResolver500        818 ms            ~5 ms     (o Smart pronto, um ditado de 40 palavras)
//	BenchmarkMontar500          21,5 s            ~1,1 s    (do zero, pelas conferências do PC)
//
// Resolver com o Smart pronto cabe na meta com folga. Montar do zero NÃO cabe:
// o lote do PC confere um par de cada vez, e cada conferência é uma busca e uma
// comparação sobre a semente acumulada (O(n²) por construção, e é o mesmo
// caminho do PC, que a paridade exige). A ADR já previa o caso: "só se não
// couber entra um cache por conta". É o Cache (cache.go): a montagem custa
// centenas de milissegundos uma vez por versão do aprendido, e cada ditado
// seguinte custa o Resolver.
//
// Os tempos abaixo são tetos folgados, para pegar a volta do problema (uma
// busca que varre tudo, uma montagem que refaz o Smart a cada par), e não para
// medir: quem mede é o benchmark. Pulam sob -short e com o detector de corrida.

func medianaDe(n int, f func()) time.Duration {
	duracoes := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		inicio := time.Now()
		f()
		duracoes = append(duracoes, time.Since(inicio))
	}
	slices.Sort(duracoes)
	return duracoes[n/2]
}

func pularSeForMedirDemais(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("teste de tempo: pulado sob -short")
	}
	if emRace {
		t.Skip("teste de tempo: o detector de corrida deixa tudo mais lento")
	}
}

// TestDesempenho_500ParesResolvidosCabemNaMeta: 500 pares, o Smart pronto, um
// ditado de 40 palavras com duas variantes: menos de 25 ms.
func TestDesempenho_500ParesResolvidosCabemNaMeta(t *testing.T) {
	pularSeForMedirDemais(t)
	pares := paresSinteticos(MaxPares)
	conta, err := Montar("conta-1", pares)
	if err != nil {
		t.Fatal(err)
	}
	if r := conta.Resumo(); r.Aplicados < MaxPares*9/10 {
		t.Fatalf("o benchmark não mede o que diz: só %d de %d pares agem (%+v)", r.Aplicados, MaxPares, r)
	}
	texto := textoDeDitado(pares)
	if d := conta.Resolver(context.Background(), texto); d.Substituicoes == 0 {
		t.Fatalf("o ditado do benchmark não aciona nenhum par: %q", d.Texto)
	}

	const meta = 25 * time.Millisecond
	if got := medianaDe(9, func() { conta.Resolver(context.Background(), texto) }); got >= meta {
		t.Errorf("resolver com 500 pares leva %v (mediana de 9), a meta é %v", got, meta)
	} else {
		t.Logf("resolver com 500 pares: %v (mediana de 9), meta %v", got, meta)
	}
}

// TestDesempenho_CacheAcertadoCabeNaMeta: com o Cache, o ditado seguinte ao que
// montou não paga a montagem: obter e resolver, menos de 25 ms.
func TestDesempenho_CacheAcertadoCabeNaMeta(t *testing.T) {
	pularSeForMedirDemais(t)
	pares := paresSinteticos(MaxPares)
	texto := textoDeDitado(pares)
	cache := NovoCache(4)
	montar := func() (*Conta, error) { return Montar("conta-1", pares) }
	if _, err := cache.Obter(context.Background(), "conta-1", "v1", montar); err != nil {
		t.Fatal(err)
	}

	const meta = 25 * time.Millisecond
	got := medianaDe(9, func() {
		conta, err := cache.Obter(context.Background(), "conta-1", "v1", montar)
		if err != nil {
			t.Fatal(err)
		}
		conta.Resolver(context.Background(), texto)
	})
	if got >= meta {
		t.Errorf("obter do cache e resolver leva %v (mediana de 9), a meta é %v", got, meta)
	} else {
		t.Logf("cache acertado + resolver com 500 pares: %v (mediana de 9), meta %v", got, meta)
	}
}

// TestDesempenho_MontarNaoVoltaAoQueEra: montar 500 pares do zero levava 21 s
// antes dos ajustes e leva cerca de 1 s. O teto de 10 s não mede: pega a volta
// do problema, numa máquina bem mais lenta que a de referência.
func TestDesempenho_MontarNaoVoltaAoQueEra(t *testing.T) {
	pularSeForMedirDemais(t)
	pares := paresSinteticos(MaxPares)
	inicio := time.Now()
	if _, err := Montar("conta-1", pares); err != nil {
		t.Fatal(err)
	}
	got := time.Since(inicio)
	t.Logf("montar 500 pares do zero: %v (a meta de 25 ms não cabe aqui: veja o comentário do arquivo)", got)
	const teto = 10 * time.Second
	if got > teto {
		t.Errorf("montar 500 pares leva %v, o teto de alarme é %v", got, teto)
	}
}
