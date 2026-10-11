package server

import (
	"sync"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// Os modelos conhecidos (ADR-021, C5b): os nomes de modelo que um provedor já
// listou para alguma chave neste processo (o PUT da chave, o GET dos modelos e
// a conferência do ditado). O nome do modelo vem das preferências da conta, que
// só conferem a forma dele (cloudstore.ModeloValido). Duas coisas dependem de o
// nome ser um que o provedor de fato tem:
//
//   - a política de esforço do Polish (polish.PoliticaEsforco) guarda uma
//     entrada por (provedor, modelo) que recusou o esforço, e a linha de log
//     dela leva o nome: só um modelo que o provedor listou chega ao Polish, e
//     assim o mapa não cresce além do catálogo dos provedores, nem o log leva um
//     nome inventado;
//   - a linha "ditado" do log escreve o modelo só se ele é conhecido, e "outro"
//     no lugar de qualquer outro nome.
//
// Fica em memória, por processo, como a política de esforço: reiniciar esquece,
// e o primeiro ditado com o Polish depois disso pergunta a lista de novo. O nome
// é de um modelo, nunca da pessoa: não é por conta.
type modelosConhecidos struct {
	mu          sync.Mutex
	porProvedor map[string]map[string]bool
}

// maxModelosConhecidos é o teto de nomes por provedor. O catálogo da OpenAI tem
// umas centenas; acima do teto, um nome novo não entra, e o ditado com ele
// pergunta a lista de novo (e, se ainda não couber, segue sem o Polish).
const maxModelosConhecidos = 2000

func novosModelosConhecidos() *modelosConhecidos {
	m := &modelosConhecidos{porProvedor: map[string]map[string]bool{}}
	// Os padrões da transcrição são conhecidos desde a subida: a conta que nunca
	// escolheu dita com eles, e o log os mostra.
	m.anotar(cloudstore.ProvedorOpenAI, []string{cloudstore.ModeloPadraoOpenAI})
	m.anotar(cloudstore.ProvedorGroq, []string{cloudstore.ModeloPadraoGroq})
	return m
}

// anotar acrescenta os modelos que o provedor listou. Só os da forma de um nome
// de modelo, e só os provedores da lista fechada.
func (m *modelosConhecidos) anotar(provedor string, modelos []string) {
	if !cloudstore.ProvedorDeIAValido(provedor) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	set := m.porProvedor[provedor]
	if set == nil {
		set = map[string]bool{}
		m.porProvedor[provedor] = set
	}
	for _, mod := range modelos {
		if len(set) >= maxModelosConhecidos {
			return
		}
		if cloudstore.ModeloValido(mod) {
			set[mod] = true
		}
	}
}

// conhece diz se o provedor já listou o modelo neste processo.
func (m *modelosConhecidos) conhece(provedor, modelo string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.porProvedor[provedor][modelo]
}

// paraOLog é o nome do modelo como a linha "ditado" o escreve: o próprio, se é
// conhecido, ou "outro".
func (m *modelosConhecidos) paraOLog(provedor, modelo string) string {
	if modelo != "" && m.conhece(provedor, modelo) {
		return modelo
	}
	return "outro"
}
