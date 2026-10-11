package uia

import (
	"testing"
	"time"
)

// Níveis de integridade (winnt.h, SECURITY_MANDATORY_*_RID).
const (
	ilMedio         = 0x2000
	ilMedioUIAccess = 0x2100
	ilAlto          = 0x3000
	ilSistema       = 0x4000
)

// TestClassificarElevacao: "janela de administrador" só com prova — os
// dois níveis lidos e o da janela maior que o do Agent.
func TestClassificarElevacao(t *testing.T) {
	casos := []struct {
		nome              string
		ilJanela, ilAgent uint32
		okJanela, okAgent bool
		quer              Elevacao
	}{
		{"Gerenciador de Tarefas elevado", ilAlto, ilMedio, true, true, ElevacaoSim},
		{"processo de sistema", ilSistema, ilMedio, true, true, ElevacaoSim},
		{"janela comum, mesmo nível", ilMedio, ilMedio, true, true, ElevacaoNao},
		// Mais alto só por pouco ainda é mais alto: uiAccess é outro nível.
		{"uiAccess acima do médio", ilMedioUIAccess, ilMedio, true, true, ElevacaoSim},
		// Agent rodando elevado (fora do roteiro): nada acima dele é comum.
		{"Agent elevado, janela média", ilMedio, ilAlto, true, true, ElevacaoNao},
		{"Agent elevado, janela elevada", ilAlto, ilAlto, true, true, ElevacaoNao},
		// Sem prova, na dúvida a mensagem fica a de sempre.
		{"token da janela não abriu", 0, ilMedio, false, true, ElevacaoDesconhecida},
		{"nível do Agent não lido", ilAlto, 0, true, false, ElevacaoDesconhecida},
		{"nada lido", 0, 0, false, false, ElevacaoDesconhecida},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			if got := classificarElevacao(c.ilJanela, c.okJanela, c.ilAgent, c.okAgent); got != c.quer {
				t.Errorf("classificarElevacao = %v, quero %v", got, c.quer)
			}
		})
	}
}

// TestDeveOlharPrimeiroPlano: só depois de uma inspeção que não trouxe
// elemento. Numa que trouxe, a UIA alcançou a janela — ela não é de
// administrador, e não há por que olhar.
func TestDeveOlharPrimeiroPlano(t *testing.T) {
	casos := []struct {
		s    Snapshot
		quer bool
	}{
		{Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)}, true},
		{Snapshot{Outcome: InspectFailed, Falha: FalhaIsPassword}, true},
		{Snapshot{Outcome: InspectNilElement}, true},
		{Snapshot{Outcome: InspectOK}, false},
		{Snapshot{Outcome: InspectOK, IsPassword: true}, false},
		{Snapshot{Outcome: InspectTimeout}, false},
	}
	for _, c := range casos {
		if got := deveOlharPrimeiroPlano(c.s); got != c.quer {
			t.Errorf("deveOlharPrimeiroPlano(%v) = %v, quero %v", c.s.Outcome, got, c.quer)
		}
	}
}

// primeiroPlanoDuble conta quantas vezes a janela em primeiro plano foi
// olhada e devolve a elevação configurada.
type primeiroPlanoDuble struct {
	elevacao Elevacao
	olhadas  int
}

func (p *primeiroPlanoDuble) ler() primeiroPlano {
	p.olhadas++
	return primeiroPlano{elevacao: p.elevacao}
}

// TestJanelaElevada_MarcaOCacheSemMudarAProtecao roda a sequência inteira
// da thread dedicada: a falha na janela de administrador marca o cache — e
// o alvo continua exatamente tão protegido quanto antes. A marca troca a
// mensagem, nunca a decisão.
func TestJanelaElevada_MarcaOCacheSemMudarAProtecao(t *testing.T) {
	falha := Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)}
	casos := []struct {
		nome         string
		inspecao     Snapshot
		elevacao     Elevacao
		querOlhar    bool
		querElevada  bool
		querProtecao bool
	}{
		{"falha em janela de administrador", falha, ElevacaoSim, true, true, true},
		{"falha em janela comum", falha, ElevacaoNao, true, false, true},
		{"falha sem prova", falha, ElevacaoDesconhecida, true, false, true},
		{"nenhum elemento em janela de administrador", Snapshot{Outcome: InspectNilElement}, ElevacaoSim, true, true, true},
		// Se a inspeção leu o elemento, ninguém olha — mesmo que o dublê
		// dissesse "sim".
		{"campo livre", campoLivre, ElevacaoSim, false, false, false},
		{"campo de senha", campoSenha, ElevacaoSim, false, false, true},
		{"timeout", Snapshot{Outcome: InspectTimeout}, ElevacaoSim, false, false, true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rel := &relogio{agora: t0, passo: time.Millisecond}
			pp := &primeiroPlanoDuble{elevacao: c.elevacao}
			tr := NewTracker(Config{})
			var got DiagnosticoFoco
			tr.AoInspecionar(func(d DiagnosticoFoco) { got = d })

			s := c.inspecao
			tr.inspecionarEPublicar(sistema{
				inspecionar:   func() Snapshot { return s },
				agora:         rel.ler,
				primeiroPlano: pp.ler,
			}, 0, false)

			if (pp.olhadas > 0) != c.querOlhar {
				t.Errorf("olhou a janela em primeiro plano %d vezes, quero olhar=%v", pp.olhadas, c.querOlhar)
			}
			alvo, elevada := tr.CurrentComElevacao()
			if elevada != c.querElevada {
				t.Errorf("janelaElevada = %v, quero %v", elevada, c.querElevada)
			}
			if alvo.Protected != c.querProtecao || tr.Current().Protected != c.querProtecao {
				t.Errorf("Protected = %v, quero %v — a marca não pode mudar a proteção", alvo.Protected, c.querProtecao)
			}
			querDiag := ElevacaoNaoVerificada
			if c.querOlhar {
				querDiag = c.elevacao
			}
			if got.Elevacao != querDiag {
				t.Errorf("diagnóstico: Elevacao = %v, quero %v", got.Elevacao, querDiag)
			}
		})
	}
}

// TestEstadoDe_MarcaSoEmAlvoProtegido: a marca nunca aparece num alvo
// livre, mesmo que um Snapshot incoerente a traga.
func TestEstadoDe_MarcaSoEmAlvoProtegido(t *testing.T) {
	if e := estadoDe(Snapshot{Outcome: InspectOK, Elevacao: ElevacaoSim}); e.janelaElevada {
		t.Error("um alvo livre saiu marcado como janela de administrador")
	}
	if e := estadoDe(Snapshot{Outcome: InspectFailed, Elevacao: ElevacaoSim}); !e.janelaElevada || !e.alvo.Protected {
		t.Errorf("estadoDe(falha elevada) = %+v, quero protegido e marcado", e)
	}
}

// TestJanelaElevada_SairDelaTiraAMarca: voltar a uma janela comum publica
// um estado novo, sem a marca.
func TestJanelaElevada_SairDelaTiraAMarca(t *testing.T) {
	rel := &relogio{agora: t0, passo: time.Millisecond}
	pp := &primeiroPlanoDuble{elevacao: ElevacaoSim}
	tr := NewTracker(Config{})
	falha := Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)}
	for _, s := range []Snapshot{falha, campoLivre} {
		s := s
		tr.inspecionarEPublicar(sistema{inspecionar: func() Snapshot { return s }, agora: rel.ler, primeiroPlano: pp.ler}, 0, false)
	}
	if alvo, elevada := tr.CurrentComElevacao(); elevada || alvo.Protected {
		t.Errorf("depois de voltar a um campo livre: Protected=%v janelaElevada=%v", alvo.Protected, elevada)
	}
}

func TestElevacao_String(t *testing.T) {
	quero := map[Elevacao]string{
		ElevacaoNaoVerificada: "nao_verificada",
		ElevacaoNao:           "nao",
		ElevacaoSim:           "sim",
		ElevacaoDesconhecida:  "desconhecida",
		Elevacao(200):         "invalida",
	}
	for e, want := range quero {
		if got := e.String(); got != want {
			t.Errorf("Elevacao(%d).String() = %q, quero %q", e, got, want)
		}
	}
}
