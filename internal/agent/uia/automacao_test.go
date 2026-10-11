package uia

import (
	"math"
	"strings"
	"testing"
	"time"
)

// fabricaDuble faz o papel do COM em montarAutomacao: devolve os HRESULTs
// configurados e anota o que foi chamado.
type fabricaDuble struct {
	hr8, hrConexao, hrTransacao, hrAntigo int32

	chamouAntigo bool
	tetosPedidos []uint32
}

func (f *fabricaDuble) criar8() int32 { return f.hr8 }

func (f *fabricaDuble) aplicarTetos(ms uint32) (int32, int32) {
	f.tetosPedidos = append(f.tetosPedidos, ms)
	return f.hrConexao, f.hrTransacao
}

func (f *fabricaDuble) criarAntigo() int32 {
	f.chamouAntigo = true
	return f.hrAntigo
}

const (
	regdbEClassNotReg = 0x80040154 // CoCreateInstance: a classe não está registrada
	eNoInterface      = 0x80004002
)

// TestMontarAutomacao cobre o item 2 das decisões do Mauro (29-set-2026):
// CUIAutomation8 com teto de 3 s; se ele não existir, o CUIAutomation
// antigo, registrado; erro só se nenhum dos dois existir.
func TestMontarAutomacao(t *testing.T) {
	casos := []struct {
		nome        string
		fab         fabricaDuble
		quer        Automacao
		querAntigo  bool
		querTetos   []uint32
		querComTeto bool
	}{
		{
			nome:        "CUIAutomation8 com os dois tetos",
			fab:         fabricaDuble{},
			quer:        Automacao{Objeto: ObjetoCUIAutomation8, TetoConexao: 3 * time.Second, TetoTransacao: 3 * time.Second},
			querTetos:   []uint32{3000},
			querComTeto: true,
		},
		{
			// O objeto continua melhor que o antigo — tem os tetos padrão do
			// Windows —, e o registro diz qual put_ falhou.
			nome: "CUIAutomation8 sem o teto de conexão",
			fab:  fabricaDuble{hrConexao: hr(eFail)},
			quer: Automacao{Objeto: ObjetoCUIAutomation8, TetoTransacao: 3 * time.Second,
				HRESULTConexao: hr(eFail)},
			querTetos: []uint32{3000},
		},
		{
			nome: "CUIAutomation8 sem o teto de transação",
			fab:  fabricaDuble{hrTransacao: hr(eFail)},
			quer: Automacao{Objeto: ObjetoCUIAutomation8, TetoConexao: 3 * time.Second,
				HRESULTTransacao: hr(eFail)},
			querTetos: []uint32{3000},
		},
		{
			// Sem o 8, cai no antigo — e não pede teto a um objeto que não
			// tem onde recebê-lo (a vtable do IUIAutomation acaba antes).
			nome:       "sem CUIAutomation8 cai no antigo",
			fab:        fabricaDuble{hr8: hr(regdbEClassNotReg)},
			quer:       Automacao{Objeto: ObjetoCUIAutomation, HRESULT8: hr(regdbEClassNotReg)},
			querAntigo: true,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			fab := c.fab
			got, err := montarAutomacao(&fab, TetoChamadaUIA)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if got != c.quer {
				t.Errorf("Automacao = %+v, quero %+v", got, c.quer)
			}
			if fab.chamouAntigo != c.querAntigo {
				t.Errorf("chamou o CUIAutomation antigo = %v, quero %v", fab.chamouAntigo, c.querAntigo)
			}
			if len(fab.tetosPedidos) != len(c.querTetos) ||
				(len(c.querTetos) > 0 && fab.tetosPedidos[0] != c.querTetos[0]) {
				t.Errorf("tetos pedidos = %v, quero %v", fab.tetosPedidos, c.querTetos)
			}
			if got.ComTeto() != c.querComTeto {
				t.Errorf("ComTeto() = %v, quero %v", got.ComTeto(), c.querComTeto)
			}
		})
	}
}

// TestMontarAutomacao_NenhumDosDois: sem objeto nenhum não há rastreador
// — o Start falha, e o erro diz os dois HRESULTs.
func TestMontarAutomacao_NenhumDosDois(t *testing.T) {
	fab := fabricaDuble{hr8: hr(regdbEClassNotReg), hrAntigo: hr(eNoInterface)}
	got, err := montarAutomacao(&fab, TetoChamadaUIA)
	if err == nil {
		t.Fatalf("sem erro, com %+v", got)
	}
	for _, s := range []string{"0x80040154", "0x80004002"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("o erro %q não diz %s", err, s)
		}
	}
	if got.Objeto != ObjetoNenhum {
		t.Errorf("Objeto = %v, quero nenhum", got.Objeto)
	}
}

// TestTetoChamadaUIA_E3s trava o número da decisão (29-set-2026): é um
// teto contra travamento, não o orçamento. Um valor perto de 500 ms
// transformaria leituras lentas mas certas em UIA_E_TIMEOUT.
func TestTetoChamadaUIA_E3s(t *testing.T) {
	if TetoChamadaUIA != 3*time.Second {
		t.Errorf("TetoChamadaUIA = %v, quero 3s (decisão do Mauro)", TetoChamadaUIA)
	}
	if milissegundos(TetoChamadaUIA) != 3000 {
		t.Errorf("milissegundos(TetoChamadaUIA) = %d, quero 3000", milissegundos(TetoChamadaUIA))
	}
}

func TestMilissegundos(t *testing.T) {
	casos := []struct {
		d    time.Duration
		quer uint32
	}{
		{0, 1},
		{-time.Second, 1},
		{500 * time.Microsecond, 1},
		{1500 * time.Millisecond, 1500},
		{time.Duration(math.MaxInt64), math.MaxUint32},
	}
	for _, c := range casos {
		if got := milissegundos(c.d); got != c.quer {
			t.Errorf("milissegundos(%v) = %d, quero %d", c.d, got, c.quer)
		}
	}
}

// TestTetoParaReinspecionar_AbaixoDoTetoDaChamada trava a decisão de
// 29-set-2026 sobre a regra da fase 1: uma chamada que bateu no teto de 3 s
// (UIA_E_TIMEOUT) não é reinspecionada — o provedor acabou de travar. Se
// alguém subir TetoParaReinspecionar acima do teto, a escada volta a um
// provedor travado até três vezes.
func TestTetoParaReinspecionar_AbaixoDoTetoDaChamada(t *testing.T) {
	if TetoParaReinspecionar >= TetoChamadaUIA {
		t.Fatalf("TetoParaReinspecionar (%v) >= TetoChamadaUIA (%v)", TetoParaReinspecionar, TetoChamadaUIA)
	}
	noTeto := Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaETimeout)}
	if espera, ok := ProximaReinspecao(noTeto, TetoChamadaUIA+10*time.Millisecond, 0); ok {
		t.Errorf("uma chamada que bateu no teto foi reinspecionada em %v", espera)
	}
	// E uma falha rápida continua ganhando a escada.
	if _, ok := ProximaReinspecao(noTeto, 76*time.Millisecond, 0); !ok {
		t.Error("o UIA_E_TIMEOUT rápido (76 ms, 29-set-2026) deixou de ser reinspecionado")
	}
}

func TestObjetoUIA_String(t *testing.T) {
	quero := map[ObjetoUIA]string{
		ObjetoNenhum:         "nenhum",
		ObjetoCUIAutomation8: "CUIAutomation8",
		ObjetoCUIAutomation:  "CUIAutomation",
		ObjetoUIA(200):       "desconhecido",
	}
	for o, want := range quero {
		if got := o.String(); got != want {
			t.Errorf("ObjetoUIA(%d).String() = %q, quero %q", o, got, want)
		}
	}
}

// TestTracker_AutomacaoAntesDoStart: sem Start (e em qualquer teste, que
// nunca passa do portão), nenhum objeto foi criado.
func TestTracker_AutomacaoAntesDoStart(t *testing.T) {
	if got := NewTracker(Config{}).Automacao(); got != (Automacao{}) {
		t.Errorf("Automacao() = %+v antes do Start, quero o zero value", got)
	}
}
