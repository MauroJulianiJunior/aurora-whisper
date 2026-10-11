package resolver

import "testing"

// TestAutocorrecao_DevolveAUnidadeQueSoEstavaNaRetratacao é o defeito da
// amostra corr-04, confirmado por Mauro em 28-set-2026.
//
// Quando a pessoa corrige só o NÚMERO, a unidade que vinha depois dele
// saía junto com a retratação: "São 3 mil reais, não, 3 mil e
// quinhentos" virava "São 3 mil e quinhentos." — perdendo a informação de
// que eram reais. A pessoa retratou o valor, não a moeda.
func TestAutocorrecao_DevolveAUnidadeQueSoEstavaNaRetratacao(t *testing.T) {
	casos := []struct{ nome, fala, quero string }{
		{"moeda", "São 3 mil reais, não, 3 mil e quinhentos.", "São 3 mil e quinhentos reais."},
		{"prazo", "Prazo de 30 dias, não, de 45.", "Prazo de 45 dias."},
		{"unidade de duas palavras", "Taxa de 10 por cento, não, de 12.", "Taxa de 12 por cento."},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := applySelfCorrection(c.fala).Text; got != c.quero {
				t.Errorf("applySelfCorrection(%q) = %q, quero %q", c.fala, got, c.quero)
			}
		})
	}
}

// TestAutocorrecao_NaoDevolveOQueFoiCorrigido: as duas condições da regra
// existem para isto. Se a correção já diz uma unidade, a da retratação é
// justamente o que foi corrigido; e só uma unidade no FIM do que sobrou
// conta.
func TestAutocorrecao_NaoDevolveOQueFoiCorrigido(t *testing.T) {
	casos := []struct{ nome, fala, quero string }{
		{"troca de moeda", "Compra 3 dólares, não, 3 euros.", "Compra 3 euros."},
		{"unidade no meio, não no fim", "Manda os 3 dias de folga, não, os 5 relatórios.", "Manda os 5 relatórios."},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := applySelfCorrection(c.fala).Text; got != c.quero {
				t.Errorf("applySelfCorrection(%q) = %q, quero %q", c.fala, got, c.quero)
			}
		})
	}
}

// TestAutocorrecao_UnidadeNaoDuplica: a correção que JÁ repete a unidade
// não pode ganhar uma segunda. "por cento" é o caso que pegou a primeira
// versão desta regra — palavra por palavra, nem "por" nem "cento" são
// unidade, e a correção parecia não ter nenhuma.
func TestAutocorrecao_UnidadeNaoDuplica(t *testing.T) {
	casos := []struct{ fala, quero string }{
		{"São 3 mil reais, não, 3 mil e quinhentos reais.", "São 3 mil e quinhentos reais."},
		{"Taxa de 10 por cento, não, de 12 por cento.", "Taxa de 12 por cento."},
	}
	for _, c := range casos {
		if got := applySelfCorrection(c.fala).Text; got != c.quero {
			t.Errorf("applySelfCorrection(%q) = %q, quero %q", c.fala, got, c.quero)
		}
	}
}
