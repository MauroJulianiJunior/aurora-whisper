package painel

import (
	"errors"
	"runtime"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

// Sob go test o portão recusa: nenhuma janela das Configurações abre na
// máquina de quem roda a suíte.
func TestAbrirConfiguracoesDoMicrofone_RecusadoSobTeste(t *testing.T) {
	err := AbrirConfiguracoesDoMicrofone()
	if err == nil {
		t.Fatal("abriu a janela sob go test")
	}
	// Fora do Windows nem existe o que abrir; no Windows quem recusa é o portão.
	if runtime.GOOS == "windows" && !errors.Is(err, sysinput.ErrDisabledInTest) {
		t.Fatalf("err = %v, quero sysinput.ErrDisabledInTest", err)
	}
}

func TestConfiguracoesDoMicrofone_Alvo(t *testing.T) {
	if ConfiguracoesDoMicrofone != "ms-settings:privacy-microphone" {
		t.Errorf("alvo = %q", ConfiguracoesDoMicrofone)
	}
}
