package metrics

import "testing"

func TestAggregate_EmptyList(t *testing.T) {
	s := Aggregate(nil)
	if s.N != 0 {
		t.Fatalf("N = %d, want 0", s.N)
	}
	if s.Mean != 0 || s.P50 != 0 || s.P95 != 0 {
		t.Fatalf("got %+v, want all zero for empty input", s)
	}
}

func TestAggregate_SingleValue(t *testing.T) {
	s := Aggregate([]float64{42})
	if s.N != 1 {
		t.Fatalf("N = %d, want 1", s.N)
	}
	if !almostEqual(s.Mean, 42) || !almostEqual(s.P50, 42) || !almostEqual(s.P95, 42) {
		t.Fatalf("got %+v, want Mean=P50=P95=42", s)
	}
}

func TestAggregate_SmallN_P95IsNearlyTheMax(t *testing.T) {
	// N=10: ceil(0.95*10) = 10 -> ultimo elemento (o maximo). O comentario
	// do 19-eval-plan.md avisa exatamente isso: com poucas amostras, p95
	// praticamente E o maximo.
	samples := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 100}
	s := Aggregate(samples)
	if s.N != 10 {
		t.Fatalf("N = %d, want 10", s.N)
	}
	if !almostEqual(s.P95, 100) {
		t.Fatalf("P95 = %v, want 100 (o maximo, com N=10)", s.P95)
	}
}

func TestAggregate_LargeN(t *testing.T) {
	samples := make([]float64, 1000)
	for i := range samples {
		samples[i] = float64(i + 1) // 1..1000
	}
	s := Aggregate(samples)
	if s.N != 1000 {
		t.Fatalf("N = %d, want 1000", s.N)
	}
	if !almostEqual(s.Mean, 500.5) {
		t.Fatalf("Mean = %v, want 500.5", s.Mean)
	}
	// p50 = ceil(0.5*1000) = 500-esimo elemento (1-based) = valor 500.
	if !almostEqual(s.P50, 500) {
		t.Fatalf("P50 = %v, want 500", s.P50)
	}
	// p95 = ceil(0.95*1000) = 950-esimo elemento (1-based) = valor 950.
	if !almostEqual(s.P95, 950) {
		t.Fatalf("P95 = %v, want 950", s.P95)
	}
}

func TestAggregate_DoesNotMutateInput(t *testing.T) {
	samples := []float64{5, 3, 1, 4, 2}
	original := append([]float64(nil), samples...)
	Aggregate(samples)
	for i := range samples {
		if samples[i] != original[i] {
			t.Fatalf("Aggregate mutated input slice: got %v, want %v", samples, original)
		}
	}
}

func TestAggregate_MaxIsGlobalMaximumNotMean(t *testing.T) {
	// Um outlier alto isolado: a media fica baixa, mas Max precisa
	// capturar o pior caso de verdade (gate de RSS decide pelo Max, nao
	// pela Mean -- ver pacote report).
	samples := []float64{10, 20, 30, 999}
	s := Aggregate(samples)
	if !almostEqual(s.Max, 999) {
		t.Fatalf("Max = %v, want 999 (o maior valor observado)", s.Max)
	}
	if s.Mean >= s.Max {
		t.Fatalf("Mean = %v, Max = %v — Mean deveria ficar bem abaixo do outlier", s.Mean, s.Max)
	}
}

func TestAggregate_MaxSingleValue(t *testing.T) {
	s := Aggregate([]float64{7})
	if !almostEqual(s.Max, 7) {
		t.Fatalf("Max = %v, want 7", s.Max)
	}
}

func TestAggregate_MaxEmptyIsZero(t *testing.T) {
	s := Aggregate(nil)
	if s.Max != 0 {
		t.Fatalf("Max = %v, want 0 (N=0, zero-value, nunca um maximo inventado)", s.Max)
	}
}

func TestAggregate_P50EvenN(t *testing.T) {
	// nearest-rank, nao interpolacao: N=4 -> ceil(0.5*4)=2 -> 2o elemento
	// (1-based) da lista ordenada [10,20,30,40] = 20 (nao a media 15/25
	// entre os dois centrais, que seria interpolacao linear).
	s := Aggregate([]float64{40, 10, 30, 20})
	if !almostEqual(s.P50, 20) {
		t.Fatalf("P50 = %v, want 20 (nearest-rank, nao interpolado)", s.P50)
	}
}
