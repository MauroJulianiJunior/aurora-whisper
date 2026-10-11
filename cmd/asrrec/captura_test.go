package main

import (
	"testing"
	"time"
)

// TestConfigDeGravacao_CapturaContinua: o dataset é gravado com o
// microfone aberto do começo ao fim e 300 ms de pre-roll. Desde a ADR-015
// o valor zero de audio.Config é o microfone só enquanto dita; sem pedir
// o sempre pronto, o pre-roll antes do ENTER sumiria e o começo de cada
// frase ficaria por conta da latência do Start.
func TestConfigDeGravacao_CapturaContinua(t *testing.T) {
	c := configDeGravacao(30 * time.Second)
	if !c.SemprePronto {
		t.Error("a gravação do dataset precisa de captura contínua (SemprePronto)")
	}
	if c.PreRoll != 300*time.Millisecond {
		t.Errorf("PreRoll = %s, quero 300ms", c.PreRoll)
	}
	if c.MaxRecording != 30*time.Second {
		t.Errorf("MaxRecording = %s, quero o pedido", c.MaxRecording)
	}
}
