package uia

// Este arquivo não tem build tag: é a política de qual objeto de UI
// Automation a thread dedicada cria, e com que teto. Quem chama o COM de
// verdade é automation_windows.go (fabricaWindows); aqui só se decide, e
// se testa contra um dublê.

import (
	"fmt"
	"math"
	"time"
)

// TetoChamadaUIA é o ConnectionTimeout e o TransactionTimeout que a thread
// dedicada pede ao IUIAutomation2 (decisão do Mauro, 29-set-2026, item 2).
//
// É um TETO contra travamento, não o orçamento da inspeção: o orçamento
// (Config.InspectBudget, 500 ms) diz quando a resposta deixa de valer; o
// teto diz quando a UIA desiste de esperar o provedor e devolve
// UIA_E_TIMEOUT, soltando a thread. Um teto perto do orçamento
// transformaria leituras lentas mas certas em falha (contestação da
// investigação de 29-set-2026).
//
// Os padrões do Windows, segundo o Microsoft Learn, são 2 s para
// ConnectionTimeout ("put_ConnectionTimeout": "The default connection
// timeout value is two seconds") e 20 s para TransactionTimeout
// ("put_TransactionTimeout": "The default transaction timeout value is 20
// seconds"). 3 s nos dois é o valor decidido: baixa o de transação de 20
// para 3 e sobe o de conexão de 2 para 3.
//
// Com o CUIAutomation antigo não há teto nenhum que se possa pedir — é daí
// que podem ter vindo as chamadas de um minuto de 29-set-2026 (hipótese,
// não verificado).
const TetoChamadaUIA = 3 * time.Second

// ObjetoUIA diz qual objeto de UI Automation a thread dedicada criou.
type ObjetoUIA uint8

const (
	// ObjetoNenhum: nenhum objeto criado (o Start falhou, ou ainda não
	// rodou).
	ObjetoNenhum ObjetoUIA = iota
	// ObjetoCUIAutomation8: CLSID_CUIAutomation8, pela interface
	// IUIAutomation2 — a que aceita ConnectionTimeout e
	// TransactionTimeout.
	ObjetoCUIAutomation8
	// ObjetoCUIAutomation: o CLSID_CUIAutomation de antes de 29-set-2026,
	// pela interface IUIAutomation. Só sobra quando o CUIAutomation8 não
	// pôde ser criado (Windows 8 em diante o têm).
	ObjetoCUIAutomation
)

// String nomeia o objeto — só para o log.
func (o ObjetoUIA) String() string {
	switch o {
	case ObjetoNenhum:
		return "nenhum"
	case ObjetoCUIAutomation8:
		return "CUIAutomation8"
	case ObjetoCUIAutomation:
		return "CUIAutomation"
	default:
		return "desconhecido"
	}
}

// Automacao descreve o objeto de UI Automation que a thread dedicada
// criou no Start, e se o teto foi aplicado. Ver Tracker.Automacao.
type Automacao struct {
	Objeto ObjetoUIA
	// HRESULT8 é o HRESULT com que a criação do CUIAutomation8 falhou,
	// quando Objeto == ObjetoCUIAutomation. Zero nos demais casos.
	HRESULT8 int32
	// TetoConexao e TetoTransacao são o que foi aplicado a
	// ConnectionTimeout e TransactionTimeout. Zero é "não aplicado": vale
	// o padrão do Windows (com o CUIAutomation8) ou nenhum teto (com o
	// antigo).
	TetoConexao   time.Duration
	TetoTransacao time.Duration
	// HRESULTConexao e HRESULTTransacao são as falhas ao aplicar cada um,
	// quando houve. Zero é sucesso ou "nem tentado".
	HRESULTConexao   int32
	HRESULTTransacao int32
}

// ComTeto diz se os dois tetos foram aplicados.
func (a Automacao) ComTeto() bool {
	return a.TetoConexao > 0 && a.TetoTransacao > 0
}

// fabricaUIA é o que montarAutomacao precisa do COM. Cada método devolve o
// HRESULT cru (negativo é falha). Em produção, fabricaWindows
// (automation_windows.go); nos testes, um dublê.
type fabricaUIA interface {
	// criar8: CoCreateInstance(CLSID_CUIAutomation8, IID_IUIAutomation2).
	criar8() (hr int32)
	// aplicarTetos: put_ConnectionTimeout e put_TransactionTimeout no
	// objeto criado por criar8, os dois com ms milissegundos.
	aplicarTetos(ms uint32) (hrConexao, hrTransacao int32)
	// criarAntigo: CoCreateInstance(CLSID_CUIAutomation, IID_IUIAutomation).
	criarAntigo() (hr int32)
}

// montarAutomacao cria o objeto de UI Automation da thread dedicada: o
// CUIAutomation8 com o teto aplicado; se ele não puder ser criado, o
// CUIAutomation antigo, sem teto — e o registro diz isso (Automacao). Só
// devolve erro se nenhum dos dois puder ser criado.
//
// Um teto que não se aplica não derruba o CUIAutomation8: ele continua
// melhor que o antigo, porque tem os tetos padrão do Windows. O registro
// diz qual put_ falhou.
func montarAutomacao(f fabricaUIA, teto time.Duration) (Automacao, error) {
	hr8 := f.criar8()
	if hr8 >= 0 {
		a := Automacao{Objeto: ObjetoCUIAutomation8}
		hc, ht := f.aplicarTetos(milissegundos(teto))
		if hc >= 0 {
			a.TetoConexao = teto
		} else {
			a.HRESULTConexao = hc
		}
		if ht >= 0 {
			a.TetoTransacao = teto
		} else {
			a.HRESULTTransacao = ht
		}
		return a, nil
	}

	if hr := f.criarAntigo(); hr < 0 {
		return Automacao{}, fmt.Errorf("CoCreateInstance: CUIAutomation8 HRESULT 0x%08X, CUIAutomation HRESULT 0x%08X",
			uint32(hr8), uint32(hr))
	}
	return Automacao{Objeto: ObjetoCUIAutomation, HRESULT8: hr8}, nil
}

// milissegundos converte d para o DWORD em milissegundos que os put_*Timeout
// recebem, recortado em [1, MaxUint32]: o teto pedido nunca é zero (o que
// a UIA faria com zero não está documentado), e um valor grande demais não
// dá a volta.
func milissegundos(d time.Duration) uint32 {
	ms := d.Milliseconds()
	switch {
	case ms < 1:
		return 1
	case ms > math.MaxUint32:
		return math.MaxUint32
	default:
		return uint32(ms)
	}
}
