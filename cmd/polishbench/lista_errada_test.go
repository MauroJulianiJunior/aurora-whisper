package main

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// A lista errada (fase L da ADR-022): o relatório mostra a frase sintética em
// que a lista não saiu, saiu sem o esperado ter, ou saiu com outros itens ou
// outro marcador. A frase de "já organizada" conta na estrutura, mas não vira
// exemplo (é frase ditada de verdade).
func TestCalcular_ListaErrada(t *testing.T) {
	lista := Caso{ID: "lista-a", Categoria: CatLista,
		Entrada:  "as tarefas de hoje são conciliar o banco lançar as notas e fechar o caixa",
		Esperado: "As tarefas de hoje são:\n- Conciliar o banco\n- Lançar as notas\n- Fechar o caixa"}
	frase := Caso{ID: "lista-b", Categoria: CatLista,
		Entrada:  "mandei o balancete a DRE e o fluxo de caixa para o cliente",
		Esperado: "Mandei o balancete, a DRE e o fluxo de caixa para o cliente."}
	real := Caso{ID: prefixoJaOrganiza + "c", Categoria: CatJaOrganizada,
		Entrada:  "Faltam:\n- O contrato\n- A procuração",
		Esperado: "Faltam:\n- O contrato\n- A procuração"}
	casos := []Caso{lista, frase, real}

	rs := []Resultado{
		resultadoDe(lista, 0, 1, "As tarefas de hoje são conciliar o banco, lançar as notas e fechar o caixa.", time.Second, ia.Tokens{}),          // não saiu
		resultadoDe(lista, 0, 2, "As tarefas de hoje são:\n1. Conciliar o banco\n2. Lançar as notas\n3. Fechar o caixa", time.Second, ia.Tokens{}), // outro marcador (mesma frase: não repete)
		resultadoDe(lista, 0, 3, lista.Esperado, time.Second, ia.Tokens{}),                                                                         // certa
		resultadoDe(frase, 1, 1, "Mandei:\n- O balancete\n- A DRE\n- O fluxo de caixa para o cliente", time.Second, ia.Tokens{}),                   // a mais
		resultadoDe(real, 2, 1, "Faltam o contrato e a procuração.", time.Second, ia.Tokens{}),                                                     // real: só conta
	}
	m := calcular(cfgDe(casos), SaidaCandidato{Resultados: rs})

	var ids []string
	for _, e := range m.Exemplos {
		if e.Tipo == ExemploListaErrada {
			ids = append(ids, e.CasoID)
		}
	}
	if len(ids) != 2 || ids[0] != "lista-a" || ids[1] != "lista-b" {
		t.Fatalf("exemplos de lista errada = %v, quero [lista-a lista-b]", ids)
	}
}

func TestListaErrada(t *testing.T) {
	casos := []struct {
		saida, esperado string
		quer            bool
	}{
		{"Frase.", "Frase.", false},
		{"Um.\n\nDois.", "Um. Dois.", false}, // parágrafo sem lista: é o acerto de estrutura
		{"São:\n- a\n- b\n- c", "São:\n- a\n- b\n- c", false},
		{"São a, b e c.", "São:\n- a\n- b\n- c", true},
		{"São:\n- a\n- b\n- c", "São a, b e c.", true},
		{"São:\n- a\n- b", "São:\n- a\n- b\n- c", true},
		{"São:\n1. a\n2. b\n3. c", "São:\n- a\n- b\n- c", true},
	}
	for _, k := range casos {
		if got := listaErrada(estruturaDe(k.saida), estruturaDe(k.esperado)); got != k.quer {
			t.Errorf("listaErrada(%q, %q) = %v, quero %v", k.saida, k.esperado, got, k.quer)
		}
	}
}
