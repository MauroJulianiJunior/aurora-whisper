//go:build windows

package autostart

import (
	"errors"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

// TestEscritaRecusadaSobGoTest prova a garantia que torna este pacote
// seguro de testar na máquina de quem usa: sob go test, Ligar e Desligar
// NÃO chegam ao registro.
//
// Não é cortesia. CLAUDE.md proíbe executar qualquer coisa que altere
// recurso global do sistema, e uma entrada em HKCU\...\Run é exatamente
// isso — ela faria o Agent de teste subir com o Windows da pessoa.
func TestEscritaRecusadaSobGoTest(t *testing.T) {
	exe := `C:\nao-existe\agente-de-teste.exe`
	if err := Ligar(exe); !errors.Is(err, sysinput.ErrDisabledInTest) {
		t.Fatalf("Ligar sob go test = %v, want ErrDisabledInTest — o teste teria gravado no registro", err)
	}
	if err := Desligar(); !errors.Is(err, sysinput.ErrDisabledInTest) {
		t.Fatalf("Desligar sob go test = %v, want ErrDisabledInTest", err)
	}
	// E a leitura, que não passa pelo portão, não acha a entrada que a
	// escrita recusada teria criado.
	if ligado, err := Ligado(exe); err != nil || ligado {
		t.Fatalf("Ligado(%q) = %v, %v — a escrita recusada deixou rastro", exe, ligado, err)
	}
}
