package uia

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRelatar_SeparaFalsoPositivoDeCampoDeSenhaDeVerdade é o motivo de
// DiagnosticoFoco existir.
//
// Protected() devolve true em quatro situações, e só UMA delas é um
// campo de senha. As outras três — timeout, falha de COM, nenhum
// elemento com foco — são "na dúvida, protege" (ADR-011, decisão 5), e
// para quem está usando produzem exatamente a mesma tela: "Ditado
// desativado em campo protegido".
//
// Em 26-set-2026 Mauro levou três recusas seguidas com o cursor numa
// janela comum, e o log dizia só `fase=blocked`. Sem separar as causas
// não há o que investigar.
func TestRelatar_SeparaFalsoPositivoDeCampoDeSenhaDeVerdade(t *testing.T) {
	casos := []struct {
		nome          string
		snap          Snapshot
		querProtegido bool
		querSenha     bool
	}{
		{
			nome:          "campo de senha de verdade",
			snap:          Snapshot{Outcome: InspectOK, IsPassword: true},
			querProtegido: true,
			querSenha:     true,
		},
		{
			nome:          "senha detectada pelo estilo da janela",
			snap:          Snapshot{Outcome: InspectOK, StyleHasPassword: true},
			querProtegido: true,
			querSenha:     true,
		},
		{
			nome:          "timeout: falso positivo",
			snap:          Snapshot{Outcome: InspectTimeout},
			querProtegido: true,
			querSenha:     false,
		},
		{
			nome:          "falha de COM: falso positivo",
			snap:          Snapshot{Outcome: InspectFailed},
			querProtegido: true,
			querSenha:     false,
		},
		{
			nome:          "nenhum elemento com foco: falso positivo",
			snap:          Snapshot{Outcome: InspectNilElement},
			querProtegido: true,
			querSenha:     false,
		},
		{
			nome:          "campo comum",
			snap:          Snapshot{Outcome: InspectOK},
			querProtegido: false,
			querSenha:     false,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var got DiagnosticoFoco
			tr := NewTracker(Config{})
			tr.AoInspecionar(func(d DiagnosticoFoco) { got = d })
			tr.relatar(c.snap, 42*time.Millisecond, false, desfecho{})

			if got.Protected != c.querProtegido {
				t.Errorf("Protected = %v, want %v", got.Protected, c.querProtegido)
			}
			senha := got.IsPassword || got.StyleSenha
			if senha != c.querSenha {
				t.Errorf("sinal de senha = %v, want %v — é o que separa recusa "+
					"legítima de falso positivo", senha, c.querSenha)
			}
			if got.Outcome != c.snap.Outcome {
				t.Errorf("Outcome = %v, want %v", got.Outcome, c.snap.Outcome)
			}
			if got.Duracao != 42*time.Millisecond {
				t.Errorf("Duracao = %v, want 42ms", got.Duracao)
			}
		})
	}
}

// TestDiagnosticoFoco_NaoCarregaContextoDeJanela trava a regra do
// CLAUDE.md: "Nunca gravar contexto de janela/seleção/texto próximo em
// logs".
//
// O Snapshot carrega ProcessName e ControlType — justamente o que NÃO
// pode sair em log (Name e ClassName nem são mais lidos, ver readElement).
// Este teste alimenta um Snapshot com valores reconhecíveis e confere que
// nenhum deles atravessa para o diagnóstico. Se alguém acrescentar um
// campo desses "só para ajudar a depurar", falha aqui.
//
// Esta inspeção é OK: não é aviso, e nem o nome do executável — a única
// exceção, só em aviso — pode sair. O aviso tem teste próprio
// (TestDiagnosticoFoco_AvisoSoLevaONomeBase).
func TestDiagnosticoFoco_NaoCarregaContextoDeJanela(t *testing.T) {
	const marcador = "SEGREDO-DA-JANELA"
	// Tudo preenchido, inclusive a medição e a volta a livre: um campo
	// novo que copie texto do Snapshot tem de ter o que copiar.
	snap := Snapshot{
		Outcome:      InspectOK,
		ProcessID:    4242,
		ProcessName:  marcador,
		ControlType:  marcador,
		LentaEm:      ChamadaNativeWindowHandle,
		LentaDuracao: time.Minute,
		EstiloLido:   true,
		Atrasada:     true,
		// O executável da janela em primeiro plano também é texto de
		// aplicativo: fora de um aviso, não sai.
		ExecutavelDoPrimeiroPlano: marcador,
	}

	var got DiagnosticoFoco
	tr := NewTracker(Config{})
	tr.AoInspecionar(func(d DiagnosticoFoco) { got = d })
	tr.relatar(snap, time.Millisecond, false, desfecho{
		Liberou: true, ProtegidoPor: time.Second, Tentativa: 2, ReinspecaoEm: time.Second,
		NoProprioAgent: true, Vigia: true,
	})

	if texto := formatarParaInspecao(got); strings.Contains(texto, marcador) {
		t.Fatalf("DiagnosticoFoco vazou contexto de janela: %s", texto)
	}
	if texto := fmt.Sprintf("%+v", got); strings.Contains(texto, marcador) {
		t.Fatalf("DiagnosticoFoco vazou contexto de janela: %s", texto)
	}
	if got.LentaEm != ChamadaNativeWindowHandle || !got.Liberou || !got.NoProprioAgent || !got.Vigia || !got.Atrasada {
		t.Fatalf("o teste não exercitou os campos novos: %+v", got)
	}
}

// TestDiagnosticoFoco_SoTemCamposQueNaoCarregamTexto é a metade do teste
// acima que não depende de alguém lembrar de atualizar
// formatarParaInspecao: um campo string (ou fatia, mapa, interface,
// ponteiro — qualquer coisa que possa levar texto do elemento
// inspecionado) posto em DiagnosticoFoco falha aqui, antes de chegar a um
// log. Os tipos enumerados valem porque o conjunto inteiro dos seus
// nomes está escrito nos respectivos String().
//
// # A exceção, e só ela
//
// Desde 29-set-2026 há UM campo string permitido: Executavel, o nome-base
// do executável em foco (ex.: brave.exe), que vai só nas linhas WARN do
// rastreador. Mudança deliberada deste teste, autorizada pelo Mauro
// (decisão 5 de 29-set-2026; registrada em 09-security-privacy.md, na
// ADR-011 e no CLAUDE.md): sem o nome do aplicativo, o log de um
// travamento nunca diz qual aplicativo travou. A permissão é pelo NOME do
// campo e pelo tipo, juntos — qualquer outro campo string continua
// falhando aqui, e o conteúdo dele é travado em
// TestExecutavelDoAviso_SoONomeBase.
func TestDiagnosticoFoco_SoTemCamposQueNaoCarregamTexto(t *testing.T) {
	permitidos := map[reflect.Type]bool{
		reflect.TypeOf(InspectOutcome(0)): true,
		reflect.TypeOf(FalhaEm(0)):        true,
		reflect.TypeOf(ChamadaUIA(0)):     true,
		reflect.TypeOf(Elevacao(0)):       true,
		reflect.TypeOf(time.Duration(0)):  true,
		reflect.TypeOf(false):             true,
		reflect.TypeOf(int(0)):            true,
		reflect.TypeOf(int32(0)):          true,
	}
	const excecao = "Executavel" // autorizada pelo Mauro em 29-set-2026
	tipo := reflect.TypeOf(DiagnosticoFoco{})
	viuExcecao := false
	for i := 0; i < tipo.NumField(); i++ {
		campo := tipo.Field(i)
		if campo.Name == excecao && campo.Type == reflect.TypeOf("") {
			viuExcecao = true
			continue
		}
		if !permitidos[campo.Type] {
			t.Errorf("DiagnosticoFoco.%s é %v: só vão para o log tipos que não "+
				"carregam texto do elemento inspecionado (CLAUDE.md); a única "+
				"exceção é %s", campo.Name, campo.Type, excecao)
		}
	}
	if !viuExcecao {
		t.Errorf("DiagnosticoFoco.%s sumiu ou mudou de tipo: se a exceção acabou, "+
			"tire-a deste teste também", excecao)
	}
}

// formatarParaInspecao rende todos os campos de DiagnosticoFoco em
// texto, do jeito que um log estruturado faria. Escrito à mão de
// propósito: %+v seguiria funcionando se alguém trocasse o tipo de um
// campo, mas um campo NOVO só aparece aqui se for adicionado — e é
// exatamente isso que se quer notar.
func formatarParaInspecao(d DiagnosticoFoco) string {
	var b strings.Builder
	b.WriteString(d.Outcome.String())
	b.WriteString(" " + d.Falha.String())
	b.WriteString(" " + strconv.FormatInt(int64(d.HRESULT), 16))
	b.WriteString(boolStr(d.Protected))
	b.WriteString(boolStr(d.IsPassword))
	b.WriteString(boolStr(d.StyleSenha))
	b.WriteString(d.Duracao.String())
	b.WriteString(" " + d.LentaEm.String())
	b.WriteString(" " + d.LentaDuracao.String())
	b.WriteString(boolStr(d.Inicial))
	b.WriteString(boolStr(d.Liberou))
	b.WriteString(" " + d.ProtegidoPor.String())
	b.WriteString(boolStr(d.Descartada))
	b.WriteString(boolStr(d.NoProprioAgent))
	b.WriteString(" " + strconv.Itoa(d.Tentativa))
	b.WriteString(" " + d.ReinspecaoEm.String())
	b.WriteString(boolStr(d.Vigia))
	b.WriteString(boolStr(d.Atrasada))
	b.WriteString(" " + d.Elevacao.String())
	b.WriteString(" " + d.Executavel)
	return b.String()
}

func boolStr(v bool) string {
	if v {
		return " true"
	}
	return " false"
}

// TestAoInspecionar_NilNaoEstoura: sem observador, relatar não pode
// entrar em pânico — é o caminho de qualquer teste e de qualquer
// chamador que não queira diagnóstico.
func TestAoInspecionar_NilNaoEstoura(t *testing.T) {
	tr := NewTracker(Config{})
	tr.relatar(Snapshot{Outcome: InspectOK}, time.Millisecond, false, desfecho{})
	tr.AoInspecionar(func(DiagnosticoFoco) {})
	tr.AoInspecionar(nil)
	tr.relatar(Snapshot{Outcome: InspectOK}, time.Millisecond, false, desfecho{})
}

// TestRelatar_MarcaAInspecaoInicial: a inspeção feita no Start, antes de
// qualquer troca de foco, precisa ser distinguível no log — é ela que
// cobre "subi o Agent e ditei na janela que já estava em foco".
func TestRelatar_MarcaAInspecaoInicial(t *testing.T) {
	var got DiagnosticoFoco
	tr := NewTracker(Config{})
	tr.AoInspecionar(func(d DiagnosticoFoco) { got = d })

	tr.relatar(Snapshot{Outcome: InspectOK}, time.Millisecond, true, desfecho{})
	if !got.Inicial {
		t.Error("a inspeção do Start não foi marcada como inicial")
	}
	tr.relatar(Snapshot{Outcome: InspectOK}, time.Millisecond, false, desfecho{})
	if got.Inicial {
		t.Error("uma inspeção por troca de foco foi marcada como inicial")
	}
}

// TestRelatar_AbreOOutcomeFailed é o teste do episódio de 28-set-2026.
//
// Mauro viu três "Ditado desativado em campo protegido" seguidos num
// campo de texto comum e desistiu de usar o Agent. O log dizia
// outcome=failed nos três, e mais nada.
//
// Só que outcome=failed sai de DOIS lugares que significam coisas
// opostas: a UIA não conseguiu dizer quem tem o foco, ou o elemento veio
// e uma propriedade não leu. A primeira aponta para a janela estar fora
// de alcance; a segunda, para um controle exótico. As correções não se
// parecem, e o log não distinguia — então não havia o que consertar,
// só o que adivinhar.
func TestRelatar_AbreOOutcomeFailed(t *testing.T) {
	casos := []struct {
		nome  string
		snap  Snapshot
		falha FalhaEm
		hr    int32
	}{
		{
			nome:  "UIA nao diz quem tem o foco",
			snap:  Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: -2147220991},
			falha: FalhaGetFocusedElement,
			hr:    -2147220991,
		},
		{
			nome:  "elemento veio, is_password nao leu",
			snap:  Snapshot{Outcome: InspectFailed, Falha: FalhaIsPassword, HRESULT: -2147467259},
			falha: FalhaIsPassword,
			hr:    -2147467259,
		},
		{
			// Timeout e elemento nulo TAMBÉM bloqueiam o ditado, mas
			// nenhum dos dois é chamada que falhou. Reportar falha aqui
			// mandaria quem lê o log procurar um HRESULT que não existe.
			nome:  "timeout nao e falha de chamada",
			snap:  Snapshot{Outcome: InspectTimeout},
			falha: FalhaNenhuma,
			hr:    0,
		},
		{
			nome:  "campo de senha de verdade nao e falha",
			snap:  Snapshot{Outcome: InspectOK, IsPassword: true},
			falha: FalhaNenhuma,
			hr:    0,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var got DiagnosticoFoco
			tr := NewTracker(Config{})
			tr.AoInspecionar(func(d DiagnosticoFoco) { got = d })
			tr.relatar(c.snap, time.Millisecond, false, desfecho{})

			if got.Falha != c.falha {
				t.Errorf("Falha = %v, quero %v", got.Falha, c.falha)
			}
			if got.HRESULT != c.hr {
				t.Errorf("HRESULT = 0x%08X, quero 0x%08X", uint32(got.HRESULT), uint32(c.hr))
			}
			// O ditado continua bloqueado em tudo que não é InspectOK sem
			// senha: esta fatia torna a recusa LEGÍVEL, não mais
			// permissiva. ADR-011, decisão 5, segue de pé.
			if quero := c.snap.Protected(); got.Protected != quero {
				t.Errorf("Protected = %v, quero %v — a política mudou sem querer", got.Protected, quero)
			}
		})
	}
}

// TestFalhaEm_String cobre todos os valores.
//
// Não é zelo: o conjunto inteiro de strings que FalhaEm pode produzir é o
// argumento de por que ela não vaza contexto de janela. Um valor novo sem
// case cai em "desconhecida", e um valor novo COM um case que alguém
// preencha com algo dinâmico é justamente o que este teste faz aparecer.
func TestFalhaEm_String(t *testing.T) {
	quero := map[FalhaEm]string{
		FalhaNenhuma:           "nenhuma",
		FalhaGetFocusedElement: "get_focused_element",
		FalhaControlType:       "get_CurrentControlType",
		FalhaIsPassword:        "get_CurrentIsPassword",
		FalhaEm(200):           "desconhecida",
	}
	for f, want := range quero {
		if got := f.String(); got != want {
			t.Errorf("FalhaEm(%d).String() = %q, quero %q", f, got, want)
		}
	}
}
