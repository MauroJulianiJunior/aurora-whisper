package polish

import (
	"log/slog"
	"sync"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// degrausDeEsforco é o que se manda como reasoning.effort a um modelo da
// OpenAI, em ordem, cada vez que o anterior foi recusado: o padrão do ia
// ("" manda "none", ADR-016, decisão 8), depois "minimal", depois nenhum
// campo (ia.EsforcoOmitido, para modelo sem raciocínio).
var degrausDeEsforco = []string{"", "minimal", ia.EsforcoOmitido}

// PoliticaEsforco escolhe o esforço de raciocínio sem nome de modelo no
// código (ADR-016, decisão 9: "os nomes ficam só no comando").
//
// Começa no padrão ("none"). Se o provedor responder modelo_incompativel
// (um 400 que recusa reasoning...), lembra o modelo e o próximo ditado vai
// com o degrau seguinte: "minimal", e depois sem o campo. O ditado que
// recebeu a recusa já caiu no Smart (sem retry: o prazo, de 3,5 s na fala
// curta, não comporta uma segunda chamada).
//
// A memória é por provedor e modelo, e só desta execução do processo: sai
// no reinício, e um modelo que passe a aceitar "none" volta a recebê-lo.
// Só a OpenAI recebe esforço; a Anthropic nunca recebe pensamento, e a
// política não se aplica a ela.
//
// Segura de qualquer goroutine: a chamada de um ditado que o prazo
// descartou ainda pode responder enquanto o seguinte começa. Um processo
// tem UMA política, compartilhada por todos os ditados dele (o Agent a
// guarda no adaptador; o servidor, na montagem das rotas).
type PoliticaEsforco struct {
	log    *slog.Logger
	mu     sync.Mutex
	degrau map[chaveDoModelo]int
}

type chaveDoModelo struct{ provedor, modelo string }

// NovaPoliticaEsforco devolve uma política vazia (todo modelo no padrão).
// log nil usa slog.Default().
func NovaPoliticaEsforco(log *slog.Logger) *PoliticaEsforco {
	if log == nil {
		log = slog.Default()
	}
	return &PoliticaEsforco{log: log, degrau: map[chaveDoModelo]int{}}
}

// Atual é o esforço que o próximo ditado manda a este modelo. Para a
// Anthropic é sempre o primeiro degrau (""), porque recusar nunca a faz
// descer — e o ia não manda esforço a ela de qualquer jeito.
func (p *PoliticaEsforco) Atual(provedor, modelo string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return degrausDeEsforco[p.degrau[chaveDoModelo{provedor, modelo}]]
}

// Recusou registra que o modelo recusou o esforço mandado. Só desce um
// degrau se esse esforço ainda é o atual — duas recusas do mesmo degrau
// (dois ditados em voo) não pulam o "minimal" —, e o último degrau fica.
func (p *PoliticaEsforco) Recusou(provedor, modelo, mandado string) {
	if provedor != ia.OpenAI {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	k := chaveDoModelo{provedor, modelo}
	i := p.degrau[k]
	if degrausDeEsforco[i] != mandado || i+1 >= len(degrausDeEsforco) {
		return
	}
	p.degrau[k] = i + 1
	// Só identificadores: o modelo é o nome que a pessoa escolheu na tela.
	p.log.Info("polish: o modelo recusou o esforço de raciocínio; os próximos ditados mandam o seguinte",
		"provedor", provedor, "modelo", modelo,
		"recusado", RotuloDoEsforco(provedor, mandado),
		"proximo", RotuloDoEsforco(provedor, degrausDeEsforco[i+1]))
}

// RotuloDoEsforco é como o esforço aparece no log.
func RotuloDoEsforco(provedor, esforco string) string {
	switch {
	case provedor != ia.OpenAI:
		return "nao_se_aplica"
	case esforco == "":
		return ia.EsforcoPadrao
	default:
		return esforco
	}
}
