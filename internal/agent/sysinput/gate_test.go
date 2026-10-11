package sysinput

import (
	"errors"
	"testing"
)

// TestGuard_SempreRecusaSobGoTest prova a primeira camada do portão: sob
// "go test", a ação real é recusada SEMPRE.
//
// Há uma ironia deliberada aqui, e ela vale ser nomeada: este teste só
// consegue exercitar o lado "dentro de teste" do portão. O lado "fora de
// teste" é, por construção, inalcançável a partir de uma suíte de testes —
// e é justamente essa assimetria que torna o portão seguro. Se um teste
// conseguisse provar o caminho liberado, ele conseguiria também injetar
// entrada, que é o que a trava existe para impedir.
//
// O caminho liberado é verificado no roteiro de teste manual, por uma
// pessoa rodando o Agent de verdade.
func TestGuard_SempreRecusaSobGoTest(t *testing.T) {
	// Estado inicial: desabilitado.
	if err := Guard(); !errors.Is(err, ErrDisabledInTest) {
		t.Fatalf("Guard() = %v, want ErrDisabledInTest", err)
	}
	if Allowed() {
		t.Fatal("Allowed() = true sob go test — a trava não está funcionando")
	}

	// O caso que importa: mesmo depois de Enable(), continua recusando.
	// É isto que impede que um teste futuro — por engano ou por pressa —
	// consiga injetar entrada na máquina de quem está desenvolvendo.
	Enable()
	t.Cleanup(Disable)

	if err := Guard(); !errors.Is(err, ErrDisabledInTest) {
		t.Fatalf("Guard() depois de Enable() = %v, want ErrDisabledInTest — "+
			"a primeira camada do portão precisa ser incondicional", err)
	}
	if Allowed() {
		t.Fatal("Allowed() = true depois de Enable() sob go test — " +
			"chamar Enable num teste não pode liberar nada")
	}
}

// TestGuard_ErrosSaoDistintos garante que os dois motivos de recusa não
// colapsam num erro só. Quem lê um log precisa distinguir "barrado por ser
// teste" (esperado) de "Agent não habilitou" (possível bug de
// inicialização).
func TestGuard_ErrosSaoDistintos(t *testing.T) {
	if errors.Is(ErrDisabledInTest, ErrDisabled) || errors.Is(ErrDisabled, ErrDisabledInTest) {
		t.Fatal("ErrDisabledInTest e ErrDisabled não podem ser equivalentes")
	}
}

// TestEnableDisable_NaoEntramEmPanico cobre o ciclo de vida básico. O
// efeito observável fica mascarado pela primeira camada sob teste, então
// o que se verifica aqui é apenas que as chamadas são seguras e
// idempotentes.
func TestEnableDisable_NaoEntramEmPanico(t *testing.T) {
	Enable()
	Enable()
	Disable()
	Disable()
	t.Cleanup(Disable)

	if Allowed() {
		t.Fatal("Allowed() = true sob go test")
	}
}
