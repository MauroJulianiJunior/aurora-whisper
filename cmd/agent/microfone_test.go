package main

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// TestConfigDeAudio_SobeSobDemanda: o Agent sobe com o microfone só
// enquanto dita (ADR-015). Sem este teste, uma regressão que voltasse a
// deixar o microfone aceso o tempo todo passaria verde: nada no log avisa
// de um microfone ligado sem ditado, e o único sinal seria o ícone do
// Windows (M0 do roteiro manual).
func TestConfigDeAudio_SobeSobDemanda(t *testing.T) {
	c := configDeAudio(defaultConfig())
	if c.SemprePronto {
		t.Error("o Agent sobe com o microfone sempre pronto; o padrão é só enquanto dita (ADR-015)")
	}
	if c.Politica != audio.PoliticaParar {
		t.Errorf("Politica = %v, quero %v (o plano B, liberar, só depois do roteiro manual)", c.Politica, audio.PoliticaParar)
	}
	// E o ring continua dimensionado pelos mesmos limites do núcleo: o de
	// gravação e o maior limiar de hold (a marca é no keydown).
	if c.MaxRecording != defaultConfig().MaxRecording {
		t.Errorf("MaxRecording = %s, quero o do núcleo, %s", c.MaxRecording, defaultConfig().MaxRecording)
	}
	if c.MaxHoldThreshold != dictation.MaxHoldThreshold {
		t.Errorf("MaxHoldThreshold = %s, quero o maior limiar que o núcleo aceita, %s", c.MaxHoldThreshold, dictation.MaxHoldThreshold)
	}
}
