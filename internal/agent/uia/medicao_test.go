package uia

import (
	"testing"
	"time"
)

// TestMaisLenta_FicaComAChamadaMaisLenta: é a pergunta que os dois minutos
// de 29-set-2026 deixaram sem resposta — QUAL chamada levou o minuto.
func TestMaisLenta_FicaComAChamadaMaisLenta(t *testing.T) {
	type anotacao struct {
		c ChamadaUIA
		d time.Duration
	}
	casos := []struct {
		nome      string
		anotacoes []anotacao
		quer      ChamadaUIA
		querDur   time.Duration
	}{
		{
			nome:    "nada anotado",
			quer:    ChamadaNenhuma,
			querDur: 0,
		},
		{
			nome:      "só a primeira chamada, e ela falhou rápido",
			anotacoes: []anotacao{{ChamadaGetFocusedElement, 2 * time.Millisecond}},
			quer:      ChamadaGetFocusedElement,
			querDur:   2 * time.Millisecond,
		},
		{
			nome: "o minuto numa leitura de melhor esforço",
			anotacoes: []anotacao{
				{ChamadaGetFocusedElement, 3 * time.Millisecond},
				{ChamadaControlType, time.Millisecond},
				{ChamadaIsPassword, time.Millisecond},
				{ChamadaProcessID, time.Millisecond},
				{ChamadaNomeDoProcesso, time.Millisecond},
				{ChamadaNativeWindowHandle, time.Minute},
				{ChamadaEstiloDaJanela, time.Millisecond},
			},
			quer:    ChamadaNativeWindowHandle,
			querDur: time.Minute,
		},
		{
			nome: "o minuto no GetFocusedElement",
			anotacoes: []anotacao{
				{ChamadaGetFocusedElement, time.Minute},
				{ChamadaControlType, 5 * time.Millisecond},
				{ChamadaIsPassword, 7 * time.Millisecond},
			},
			quer:    ChamadaGetFocusedElement,
			querDur: time.Minute,
		},
		{
			// A ordem das chamadas é fixa: no empate, fica a primeira, e
			// isso não muda de lado de uma inspeção para outra.
			nome: "empate fica com a primeira",
			anotacoes: []anotacao{
				{ChamadaControlType, 4 * time.Millisecond},
				{ChamadaIsPassword, 4 * time.Millisecond},
			},
			quer:    ChamadaControlType,
			querDur: 4 * time.Millisecond,
		},
		{
			// Uma chamada de duração zero (relógio grosso) ainda é a
			// chamada mais lenta de uma inspeção que só fez ela.
			nome:      "duração zero ainda conta",
			anotacoes: []anotacao{{ChamadaGetFocusedElement, 0}},
			quer:      ChamadaGetFocusedElement,
			querDur:   0,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var m maisLenta
			for _, a := range c.anotacoes {
				m.anotar(a.c, a.d)
			}
			if m.chamada != c.quer || m.duracao != c.querDur {
				t.Errorf("mais lenta = %v em %v, quero %v em %v", m.chamada, m.duracao, c.quer, c.querDur)
			}
		})
	}
}

// TestChamadaUIA_String cobre todos os valores — mesmo motivo de
// TestFalhaEm_String: o conjunto inteiro de nomes é o argumento de que o
// campo não vaza contexto de janela.
func TestChamadaUIA_String(t *testing.T) {
	quero := map[ChamadaUIA]string{
		ChamadaNenhuma:            "nenhuma",
		ChamadaGetFocusedElement:  "get_focused_element",
		ChamadaControlType:        "control_type",
		ChamadaIsPassword:         "is_password",
		ChamadaProcessID:          "process_id",
		ChamadaNomeDoProcesso:     "process_image_name",
		ChamadaNativeWindowHandle: "native_window_handle",
		ChamadaEstiloDaJanela:     "window_style",
		ChamadaUIA(200):           "desconhecida",
	}
	for c, want := range quero {
		if got := c.String(); got != want {
			t.Errorf("ChamadaUIA(%d).String() = %q, quero %q", c, got, want)
		}
	}
}

// TestRelatar_LevaAChamadaMaisLenta: a medição sai do Snapshot e chega ao
// diagnóstico — em qualquer outcome, inclusive na falha.
func TestRelatar_LevaAChamadaMaisLenta(t *testing.T) {
	var got DiagnosticoFoco
	tr := NewTracker(Config{})
	tr.AoInspecionar(func(d DiagnosticoFoco) { got = d })

	tr.relatar(Snapshot{
		Outcome:      InspectFailed,
		Falha:        FalhaGetFocusedElement,
		LentaEm:      ChamadaGetFocusedElement,
		LentaDuracao: 76 * time.Millisecond,
	}, 80*time.Millisecond, false, desfecho{})

	if got.LentaEm != ChamadaGetFocusedElement || got.LentaDuracao != 76*time.Millisecond {
		t.Errorf("lenta = %v em %v, quero get_focused_element em 76ms", got.LentaEm, got.LentaDuracao)
	}
}
