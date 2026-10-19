// Package permissao percebe se a privacidade do Windows bloqueia o
// microfone para o Agent.
//
// # Por que existe
//
// Quem instala o app com o microfone bloqueado em Configurações >
// Privacidade e segurança > Microfone não vê nada: o runtime retém a
// pílula "Gravando" e o bipe até o microfone entregar áudio (ADR-015), e
// bloqueado ele nunca entrega. O app não pode — nem deve — ligar essa
// permissão sozinho; o que faz é PERCEBER o bloqueio, AVISAR (pílula e
// painel) e abrir a tela certa do Windows para a pessoa ligar a chave.
//
// # Só leitura
//
// A leitura é do registro (ConsentStore e a política AppPrivacy) e nada é
// gravado, então não passa pelo portão de internal/agent/sysinput. O
// valor ausente é "liberado": é o padrão do Windows.
//
// Este arquivo não tem build tag: a decisão (valores lidos → motivo) roda
// e é testada em qualquer sistema. A leitura fica em permissao_windows.go.
package permissao

import "strings"

// Motivo é a razão do bloqueio, como string estável: vai na API local
// (campo "motivo"), no log e no painel.
type Motivo string

const (
	// Liberado: nenhum dos cinco pontos nega. Vazio de propósito.
	Liberado Motivo = ""
	// Empresa: a política do Windows (administrador) nega o microfone aos
	// aplicativos. A pessoa não resolve sozinha.
	Empresa Motivo = "empresa"
	// Aparelho: "Acesso ao microfone" desligado para o aparelho todo.
	Aparelho Motivo = "aparelho"
	// Conta: "Acesso ao microfone" desligado para esta conta de usuário.
	Conta Motivo = "conta"
	// Programas: "Permitir que aplicativos da área de trabalho acessem o
	// microfone" desligado.
	Programas Motivo = "programas"
	// EsteApp: o Windows recente nega o microfone só a este executável.
	EsteApp Motivo = "este_app"
	// Desconhecido: não deu para ler (outro sistema, ou erro de leitura).
	// Nunca afirma bloqueio.
	Desconhecido Motivo = "desconhecido"
)

// Bloqueado reporta se o motivo é um bloqueio de fato. Liberado e
// Desconhecido não são: na dúvida o app não afirma nada.
func (m Motivo) Bloqueado() bool {
	switch m {
	case Empresa, Aparelho, Conta, Programas, EsteApp:
		return true
	}
	return false
}

// SemPolitica é o valor de Valores.PoliticaEmpresa quando a política não
// existe (o DWORD 0 também é um valor válido, "o usuário decide", então o
// ausente precisa de um valor próprio).
const SemPolitica = -1

// politicaNegar é o valor de LetAppsAccessMicrophone que força negar.
const politicaNegar = 2

// Valores é o que a leitura do registro devolveu, um campo por ponto.
// String vazia é "valor ausente" (liberado). Só "Deny" bloqueia — "Allow"
// e qualquer outro texto, não.
type Valores struct {
	// PoliticaEmpresa é o DWORD LetAppsAccessMicrophone da política
	// AppPrivacy (HKLM). 2 = forçar negar. Ausente = SemPolitica.
	PoliticaEmpresa int
	// Aparelho, Conta, Programas e EsteApp são o "Value" do ConsentStore.
	Aparelho  string
	Conta     string
	Programas string
	EsteApp   string
}

func nega(v string) bool { return strings.EqualFold(strings.TrimSpace(v), "Deny") }

// Decidir devolve o motivo do bloqueio, na ordem de precedência: empresa,
// aparelho, conta, programas, este app. A ordem importa para o que a
// pessoa lê: a empresa vem primeiro porque ligar a chave de baixo não
// adianta nada enquanto ela nega.
func Decidir(v Valores) Motivo {
	switch {
	case v.PoliticaEmpresa == politicaNegar:
		return Empresa
	case nega(v.Aparelho):
		return Aparelho
	case nega(v.Conta):
		return Conta
	case nega(v.Programas):
		return Programas
	case nega(v.EsteApp):
		return EsteApp
	}
	return Liberado
}

// ChaveDoApp é o nome da subchave do ConsentStore para o executável em
// caminho: o Windows grava o caminho com a barra invertida trocada por
// cerquilha (C:#Desenvolvimento#...#agent.exe).
func ChaveDoApp(caminho string) string {
	return strings.ReplaceAll(caminho, `\`, "#")
}
