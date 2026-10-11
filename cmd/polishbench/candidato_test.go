package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

func TestAnalisarCandidatos(t *testing.T) {
	got, err := analisarCandidatos(" openai:gpt-6-luna , openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5,openai:x@omitir ")
	if err != nil {
		t.Fatal(err)
	}
	want := []Candidato{
		{Provedor: "openai", Modelo: "gpt-6-luna"},
		{Provedor: "openai", Modelo: "gpt-5-nano", Esforco: "minimal"},
		{Provedor: "anthropic", Modelo: "claude-haiku-4-5"},
		{Provedor: "openai", Modelo: "x", Esforco: ia.EsforcoOmitido},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidatos = %+v, quero %+v", got, want)
	}
	rotulos := []string{"openai:gpt-6-luna", "openai:gpt-5-nano@minimal", "anthropic:claude-haiku-4-5", "openai:x@omitir"}
	for i, c := range got {
		if c.Rotulo() != rotulos[i] {
			t.Errorf("Rotulo = %q, quero %q", c.Rotulo(), rotulos[i])
		}
	}
	if got[1].ChavePreco() != "openai:gpt-5-nano" {
		t.Errorf("a chave de preço não leva o esforço: %q", got[1].ChavePreco())
	}
}

func TestAnalisarCandidatos_Erros(t *testing.T) {
	casos := []struct{ entrada, trecho string }{
		{"", "nenhum modelo"},
		{" , ,", "nenhum modelo"},
		{"gpt-6-luna", "provedor:modelo"},
		{"google:gemini", "provedor:modelo"},
		{"openai:", "falta o nome"},
		{"openai:@low", "falta o nome"},
		{"anthropic:claude-haiku-4-5@low", "só vale na OpenAI"},
		{"openai:x@", "esforço"},
		{"openai:x@Low", "esforço"},
		{"openai:x@low-2", "esforço"},
		{"openai:a,openai:a", "repetido"},
		{"openai:a@low,openai:a@low", "repetido"},
	}
	for _, c := range casos {
		_, err := analisarCandidatos(c.entrada)
		if err == nil || !strings.Contains(err.Error(), c.trecho) {
			t.Errorf("%q: erro %v, quero um com %q", c.entrada, err, c.trecho)
		}
	}
	// O mesmo modelo com esforços diferentes NÃO é repetição.
	if _, err := analisarCandidatos("openai:a,openai:a@low,openai:a@omitir"); err != nil {
		t.Errorf("esforços diferentes: %v", err)
	}
}

func TestCandidato_EsforcoEChamada(t *testing.T) {
	casos := []struct {
		c     Candidato
		quero string
	}{
		{Candidato{Provedor: "openai", Modelo: "m"}, "none"},
		{Candidato{Provedor: "openai", Modelo: "m", Esforco: "minimal"}, "minimal"},
		{Candidato{Provedor: "openai", Modelo: "m", Esforco: ia.EsforcoOmitido}, "campo reasoning omitido"},
		{Candidato{Provedor: "anthropic", Modelo: "m"}, "não se aplica"},
	}
	for _, k := range casos {
		if got := k.c.EsforcoEfetivo(); got != k.quero {
			t.Errorf("%s: esforço efetivo %q, quero %q", k.c.Rotulo(), got, k.quero)
		}
	}
	ch := Candidato{Provedor: "openai", Modelo: "m", Esforco: "low"}.Chamada()
	if ch != (ia.Chamada{Provedor: "openai", Modelo: "m", Esforco: "low"}) {
		t.Errorf("Chamada = %+v", ch)
	}
}

func TestAnalisarPrecos(t *testing.T) {
	got, err := analisarPrecos("openai:gpt-6-luna=0.10/0.50, anthropic:claude-haiku-4-5=1/5,")
	if err != nil {
		t.Fatal(err)
	}
	if got["openai:gpt-6-luna"] != (Preco{0.10, 0.50}) || got["anthropic:claude-haiku-4-5"] != (Preco{1, 5}) || len(got) != 2 {
		t.Errorf("preços = %v", got)
	}
	if m, err := analisarPrecos(""); err != nil || len(m) != 0 {
		t.Errorf("vazio: %v %v", m, err)
	}
	for _, ruim := range []string{
		"openai:x", "openai:x=1", "openai:x=1/", "openai:x=a/b", "x=1/2", "google:x=1/2", "openai:=1/2",
		"openai:x=-1/2", "openai:x=1/NaN", "openai:x=1/Inf", "openai:x=1/20000", "openai:x=0,10/0,50",
	} {
		if _, err := analisarPrecos(ruim); err == nil {
			t.Errorf("%q devia dar erro", ruim)
		}
	}
}

func TestPrecosDos(t *testing.T) {
	cands, _ := analisarCandidatos("openai:gpt-6-luna,openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5,openai:outro")
	if _, err := precosDos(cands, nil); err == nil || !strings.Contains(err.Error(), "sem preço para openai:outro") {
		t.Fatalf("modelo sem preço: %v", err)
	}
	flag := map[string]Preco{"openai:outro": {2, 3}, "openai:gpt-6-luna": {9, 9}}
	got, err := precosDos(cands, flag)
	if err != nil {
		t.Fatal(err)
	}
	if got["openai:gpt-6-luna"] != (Preco{9, 9}) {
		t.Errorf("a flag tem de vencer o padrão: %v", got["openai:gpt-6-luna"])
	}
	if got["openai:gpt-5-nano"] != precosPadrao["openai:gpt-5-nano"] || got["anthropic:claude-haiku-4-5"] != precosPadrao["anthropic:claude-haiku-4-5"] {
		t.Errorf("os padrões faltam: %v", got)
	}
	if got["openai:outro"] != (Preco{2, 3}) {
		t.Errorf("preço da flag de um modelo sem padrão: %v", got["openai:outro"])
	}
	// Um preço para um modelo que não está em -modelos é erro de digitação.
	if _, err := precosDos(cands, map[string]Preco{"openai:gpt-6-lunna": {1, 1}, "openai:outro": {1, 1}}); err == nil || !strings.Contains(err.Error(), "erro de digitação") {
		t.Errorf("preço de modelo fora da lista: %v", err)
	}
}

// Os preços padrão são os da pesquisa de 29-set-2026 (ADR-016).
func TestPrecosPadrao_SaoOsDaPesquisa(t *testing.T) {
	quero := map[string]Preco{
		"openai:gpt-6-luna":          {0.10, 0.50},
		"openai:gpt-5-nano":          {0.05, 0.40},
		"anthropic:claude-haiku-4-5": {1.00, 5.00},
	}
	if !reflect.DeepEqual(precosPadrao, quero) {
		t.Errorf("precosPadrao = %v, quero %v", precosPadrao, quero)
	}
}
