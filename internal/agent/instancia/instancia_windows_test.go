//go:build windows

package instancia

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// nomeDeTeste nunca é NomeDoAgent: um Agent rodando na máquina de quem
// roda a suíte não pode fazer o teste falhar, e o teste não pode tomar a
// instância do Agent de verdade.
//
// Criar um mutex com nome aleatório não atua no sistema de quem usa: é um
// objeto do kernel que some quando o teste fecha o handle.
func nomeDeTeste(t *testing.T) string {
	return fmt.Sprintf(`Local\AuroraWhisperTeste-%d-%d`, os.Getpid(), time.Now().UnixNano())
}

// TestGarantir_SegundoAgentRecusado é o caso da noite de 27-set, com a
// causa trocada: o Windows sobe um Agent pelo início automático, a
// pessoa abre outro na mão, e dois hooks na mesma tecla duplicam o texto.
func TestGarantir_SegundoAgentRecusado(t *testing.T) {
	nome := nomeDeTeste(t)

	liberar, err := Garantir(nome)
	if err != nil {
		t.Fatalf("primeiro Garantir: %v", err)
	}
	defer liberar()

	if _, err := Garantir(nome); !errors.Is(err, ErrJaRodando) {
		t.Fatalf("segundo Garantir = %v, want ErrJaRodando — dois Agents duplicariam o texto", err)
	}
}

// TestGarantir_LiberadaPodeSerTomadaDeNovo: fechar o Agent tem de deixar
// o próximo subir. Um mutex que sobrasse impediria o Agent de voltar até
// reiniciar a máquina.
func TestGarantir_LiberadaPodeSerTomadaDeNovo(t *testing.T) {
	nome := nomeDeTeste(t)

	liberar, err := Garantir(nome)
	if err != nil {
		t.Fatalf("Garantir: %v", err)
	}
	liberar()

	liberar2, err := Garantir(nome)
	if err != nil {
		t.Fatalf("depois de liberar, Garantir = %v — o Agent não subiria de novo", err)
	}
	liberar2()
}

// TestJaExiste_AcessoNegadoEhOutroAgent: com o primeiro Agent elevado, o
// CreateMutex do segundo falha com acesso negado. Isso é "já rodando",
// não "mecanismo quebrado" — senão o segundo sobe junto e o texto sai
// duplicado.
func TestJaExiste_AcessoNegadoEhOutroAgent(t *testing.T) {
	if !jaExiste(windows.ERROR_ALREADY_EXISTS) {
		t.Error("ALREADY_EXISTS não conta como já rodando")
	}
	if !jaExiste(fmt.Errorf("embrulhado: %w", windows.ERROR_ACCESS_DENIED)) {
		t.Error("ACCESS_DENIED (Agent elevado) não conta como já rodando")
	}
	if jaExiste(nil) || jaExiste(windows.ERROR_INVALID_NAME) {
		t.Error("sucesso ou outra falha contados como já rodando")
	}
}
