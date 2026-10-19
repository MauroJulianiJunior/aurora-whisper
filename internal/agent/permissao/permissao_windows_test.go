//go:build windows

package permissao

import "testing"

// Só lê o registro desta máquina (nada é gravado): o resultado depende de
// como ela está configurada, então o teste só confere que devolve um
// motivo conhecido e não entra em pânico.
func TestConsultar_DevolveMotivoConhecido(t *testing.T) {
	switch m := Consultar(); m {
	case Liberado, Desconhecido, Empresa, Aparelho, Conta, Programas, EsteApp:
	default:
		t.Errorf("motivo inesperado %q", m)
	}
}
