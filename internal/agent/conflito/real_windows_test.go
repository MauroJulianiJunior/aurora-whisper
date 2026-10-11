//go:build maquinareal && windows

package conflito

import (
	"strings"
	"testing"
)

// TestListar_NaMaquinaDeVerdade confere que a enumeração do Windows
// devolve os nomes no formato que normalizar espera.
//
// # Por que fora da suíte normal
//
// O resultado depende do que está rodando na máquina AGORA, então ele
// mudaria de veredito entre duas execuções sem nada ter mudado no
// código. Um teste assim na suíte vira ruído e acaba ignorado.
//
// Não age no sistema: CreateToolhelp32Snapshot só LÊ a tabela de
// processos. Não injeta tecla, não move foco, não escreve em lugar
// nenhum — por isso é seguro rodar na máquina de quem usa, ao contrário
// dos testes de inserção.
//
// Existe porque o teste contra lista falsa tem um ponto cego exato:
// ele prova que Detectar casa "Wispr Flow" com "wispr flow.exe", mas
// não prova que o Windows reporta "Wispr Flow". Se a API devolvesse o
// caminho completo, ou o nome truncado em 32 caracteres, todos os
// testes normais continuariam verdes e o aviso nunca dispararia.
//
// Rodar:
//
//	go test -tags maquinareal -v -run Verdade ./internal/agent/conflito/
func TestListar_NaMaquinaDeVerdade(t *testing.T) {
	ps, err := Listar()
	if err != nil {
		t.Fatalf("Listar: %v", err)
	}
	if len(ps) < 10 {
		t.Fatalf("só %d processos — a enumeração parou cedo demais", len(ps))
	}
	t.Logf("%d processos enumerados", len(ps))

	// O explorer.exe é a âncora: está sempre presente numa sessão de
	// desktop, e o nome dele é conhecido. Se ele não casar, o formato
	// devolvido não é o que normalizar assume.
	var achouExplorer bool
	for _, p := range ps {
		if normalizar(p) == "explorer.exe" {
			achouExplorer = true
			t.Logf("âncora: %q normaliza para explorer.exe", p)
			break
		}
	}
	if !achouExplorer {
		t.Errorf("nenhum processo normalizou para explorer.exe — formato inesperado")
	}

	// Caminho: se a API devolvesse caminho completo, normalizar ainda
	// resolveria, mas é bom saber qual dos dois estamos vendo.
	for _, p := range ps {
		if strings.ContainsAny(p, `\/`) {
			t.Logf("nota: vem com caminho, ex.: %q", p)
			break
		}
	}

	for _, p := range Detectar(ps) {
		t.Logf("DETECTADO: %s", p.Aviso())
	}
}
