package uia

// Este arquivo não tem build tag: é a decisão sobre a janela em primeiro
// plano depois de uma inspeção que não trouxe elemento — quando olhar, e o
// que concluir do nível de integridade. Quem lê o processo e o token de
// verdade é automation_windows.go (lerPrimeiroPlano).

// Elevacao diz se a janela em primeiro plano roda com nível de integridade
// maior que o do Agent — a "janela de administrador" (decisão do Mauro de
// 29-set-2026, item 4).
//
// # Por que existe
//
// Em 29-set-2026 o log teve 21 falhas E_FAIL em get_focused_element, em
// rajadas, enquanto o Gerenciador de Tarefas estava aberto. O Gerenciador
// roda elevado (autoElevate), e um processo de nível médio sem uiAccess não
// enxerga a interface de um de nível alto — nem a UIA, nem o SendInput, que
// o UIPI descarta em silêncio (Microsoft Learn, "SendInput"). Bloquear ali
// está certo: o ditado não entraria de qualquer forma. O errado era a
// pílula dizer "campo protegido" numa janela sem campo de senha nenhum.
//
// Como FalhaEm, é um tipo enumerado e não uma string: o conjunto inteiro de
// valores que pode ir para o log está escrito em String().
type Elevacao uint8

const (
	// ElevacaoNaoVerificada: ninguém olhou — a inspeção trouxe um elemento
	// (ok ou timeout), ou não houve como olhar. É o zero value.
	ElevacaoNaoVerificada Elevacao = iota
	// ElevacaoNao: o nível de integridade da janela em primeiro plano foi
	// lido e não é maior que o do Agent.
	ElevacaoNao
	// ElevacaoSim: foi lido e é maior — janela de administrador. Só este
	// valor muda a mensagem da pílula; o ditado continua bloqueado como
	// antes.
	ElevacaoSim
	// ElevacaoDesconhecida: tentou-se olhar e não deu — sem janela em
	// primeiro plano, processo inacessível, token que não abre. Na dúvida,
	// a mensagem fica a de sempre. A Microsoft documenta que um processo
	// não elevado só abre o token de um elevado se os dois forem da mesma
	// conta (OpenProcessToken, Remarks) — um administrador de outra conta
	// cai aqui.
	ElevacaoDesconhecida
)

// String nomeia o valor — só para o log.
func (e Elevacao) String() string {
	switch e {
	case ElevacaoNaoVerificada:
		return "nao_verificada"
	case ElevacaoNao:
		return "nao"
	case ElevacaoSim:
		return "sim"
	case ElevacaoDesconhecida:
		return "desconhecida"
	default:
		return "invalida"
	}
}

// primeiroPlano é o que se lê, só leitura, do processo da janela em
// primeiro plano depois de uma inspeção que não trouxe elemento (ver
// deveOlharPrimeiroPlano).
type primeiroPlano struct {
	elevacao Elevacao
	// executavel é o nome-base do executável dela ("" se não deu para
	// ler). Só vai para o aviso — ver executavelDoAviso.
	executavel string
}

// deveOlharPrimeiroPlano diz se, depois da inspeção s, vale olhar a janela
// em primeiro plano: só quando a UIA não trouxe elemento — falhou
// (InspectFailed) ou disse que não há foco (InspectNilElement). "Só depois
// da falha", como decidido: numa inspeção que leu o elemento, ok ou
// timeout, a UIA alcançou a janela, e ela não é de administrador.
func deveOlharPrimeiroPlano(s Snapshot) bool {
	return s.Outcome == InspectFailed || s.Outcome == InspectNilElement
}

// classificarElevacao compara o nível de integridade da janela em primeiro
// plano (ilJanela, lido se okJanela) com o do Agent (ilAgent, lido se
// okAgent). Os níveis são o último sub-authority do SID do rótulo
// obrigatório do token (TokenIntegrityLevel): 0x2000 médio, 0x3000 alto,
// 0x4000 sistema (winnt.h, SECURITY_MANDATORY_*_RID). Maior é mais
// privilegiado.
//
// Só dá ElevacaoSim com prova: os dois níveis lidos, e o da janela maior.
// Qualquer leitura que falhou dá ElevacaoDesconhecida.
func classificarElevacao(ilJanela uint32, okJanela bool, ilAgent uint32, okAgent bool) Elevacao {
	switch {
	case !okJanela || !okAgent:
		return ElevacaoDesconhecida
	case ilJanela > ilAgent:
		return ElevacaoSim
	default:
		return ElevacaoNao
	}
}
