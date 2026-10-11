package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Este arquivo é a metade do Polish que só o binário do Agent monta
// (ADR-016, fatia S4): o adaptador que liga a chave do Gerenciador de
// Credenciais, o cliente HTTP de internal/ia e as guardas de
// internal/resolver ao runtime (winruntime.AbrirPolidor).

// polishDoAgent monta, a cada ditado, o Polidor do provedor e do modelo
// que valiam no keydown.
//
// # A chave
//
// Lida do cofre A CADA DITADO, pelo provedor do keydown, dentro da própria
// chamada (ia.Polidor.Chave): "Remover chave" na tela vale no ditado
// seguinte, e nenhum campo daqui nem do ia.Polidor guarda a chave (ADR-016,
// decisão 2). Em produção o cofre é o Gerenciador de Credenciais, e a
// leitura passa pelo portão sysinput, que só abre no binário do Agent. Um
// erro de leitura, qualquer que seja, vira o motivo sem_chave (ia.Polidor),
// sem abrir a rede.
type polishDoAgent struct {
	smart   *resolver.SmartResolver
	cofre   chaves.Cofre
	cliente *ia.Cliente
	esforco *politicaEsforco
}

func novoPolishDoAgent(smart *resolver.SmartResolver, cofre chaves.Cofre, cliente *ia.Cliente, log *slog.Logger) *polishDoAgent {
	return &polishDoAgent{smart: smart, cofre: cofre, cliente: cliente, esforco: novaPoliticaEsforco(log)}
}

// com devolve o mesmo adaptador sobre o Smart de um dicionário novo (o que
// o painel acabou de aprender, ver aprendizadoDoAgent). Cofre, cliente e a
// política de esforço são os mesmos: a memória dos modelos que recusaram o
// esforço não se perde ao trocar o dicionário.
func (a *polishDoAgent) com(smart *resolver.SmartResolver) *polishDoAgent {
	c := *a
	c.smart = smart
	return &c
}

// abrir implementa winruntime.AbrirPolidor.
//
// O dicionário é o mesmo do Smart, consultado localmente pelas guardas
// (resolver.PolishResolver.Reescrever); nunca vai ao provedor.
func (a *polishDoAgent) abrir(provedor, modelo string, contar func(entrada, saida int)) (winruntime.Polidor, string) {
	pr := &resolver.PolishResolver{Smart: a.smart}
	if !ia.Valido(provedor) {
		// Sem provedor escolhido (a API local já recusa o Polish assim):
		// sem cliente, resolver.Polir devolve sem_cliente sem chamar
		// ninguém nem ler a chave.
		return pr, ""
	}
	esforco := a.esforco.atual(provedor, modelo)
	pr.Client = &clienteDoDitado{
		polidor: ia.Polidor{
			Cliente: a.cliente,
			Chamada: ia.Chamada{Provedor: provedor, Modelo: modelo, Esforco: esforco},
			Chave:   func(context.Context) (string, error) { return a.cofre.Ler(provedor) },
			Uso: func(t ia.Tokens) {
				if contar != nil {
					contar(t.Entrada, t.Saida)
				}
			},
		},
		politica: a.esforco,
		provedor: provedor,
		modelo:   modelo,
		esforco:  esforco,
	}
	return pr, rotuloDoEsforco(provedor, esforco)
}

// clienteDoDitado é o resolver.PolishClient de um ditado: o ia.Polidor, e
// a política de esforço ouvindo a resposta.
type clienteDoDitado struct {
	polidor          ia.Polidor
	politica         *politicaEsforco
	provedor, modelo string
	esforco          string
}

// Polish implementa resolver.PolishClient. O erro passa como veio (o
// ia.Polidor já o traduz num motivo fixo, sem corpo e sem chave).
func (c *clienteDoDitado) Polish(ctx context.Context, req resolver.PolishRequest) ([]byte, error) {
	texto, err := c.polidor.Polish(ctx, req)
	var e *resolver.ErroPolish
	if errors.As(err, &e) && e.Motivo == resolver.MotivoModeloIncompativel {
		c.politica.recusou(c.provedor, c.modelo, c.esforco)
	}
	return texto, err
}

// degrausDeEsforco é o que o Agent manda como reasoning.effort a um modelo
// da OpenAI, em ordem, cada vez que o anterior foi recusado: o padrão do
// ia ("" manda "none", ADR-016, decisão 8), depois "minimal", depois
// nenhum campo (ia.EsforcoOmitido, para modelo sem raciocínio).
var degrausDeEsforco = []string{"", "minimal", ia.EsforcoOmitido}

// politicaEsforco escolhe o esforço de raciocínio sem nome de modelo no
// código (ADR-016, decisão 9: "os nomes ficam só no comando").
//
// Começa no padrão ("none"). Se o provedor responder modelo_incompativel
// (um 400 que recusa reasoning...), lembra o modelo e o próximo ditado vai
// com o degrau seguinte: "minimal", e depois sem o campo. O ditado que
// recebeu a recusa já caiu no Smart (sem retry: o prazo, de 3,5 s na fala
// curta, não comporta uma segunda chamada).
//
// A memória é por provedor e modelo, e só desta execução: sai no
// reinício, e um modelo que passe a aceitar "none" volta a recebê-lo. Só
// a OpenAI recebe esforço; a Anthropic nunca recebe pensamento, e a
// política não se aplica a ela.
//
// Segura de qualquer goroutine: a chamada de um ditado que o prazo
// descartou ainda pode responder enquanto o seguinte começa.
type politicaEsforco struct {
	log    *slog.Logger
	mu     sync.Mutex
	degrau map[chaveDoModelo]int
}

type chaveDoModelo struct{ provedor, modelo string }

func novaPoliticaEsforco(log *slog.Logger) *politicaEsforco {
	if log == nil {
		log = slog.Default()
	}
	return &politicaEsforco{log: log, degrau: map[chaveDoModelo]int{}}
}

// atual é o esforço que o próximo ditado manda a este modelo. Para a
// Anthropic é sempre o primeiro degrau (""), porque recusou nunca a faz
// descer — e o ia não manda esforço a ela de qualquer jeito.
func (p *politicaEsforco) atual(provedor, modelo string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return degrausDeEsforco[p.degrau[chaveDoModelo{provedor, modelo}]]
}

// recusou registra que o modelo recusou o esforço mandado. Só desce um
// degrau se esse esforço ainda é o atual — duas recusas do mesmo degrau
// (dois ditados em voo) não pulam o "minimal" —, e o último degrau fica.
func (p *politicaEsforco) recusou(provedor, modelo, mandado string) {
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
		"recusado", rotuloDoEsforco(provedor, mandado),
		"proximo", rotuloDoEsforco(provedor, degrausDeEsforco[i+1]))
}

// rotuloDoEsforco é como o esforço aparece no log.
func rotuloDoEsforco(provedor, esforco string) string {
	switch {
	case provedor != ia.OpenAI:
		return "nao_se_aplica"
	case esforco == "":
		return ia.EsforcoPadrao
	default:
		return esforco
	}
}

// ultimoPolishParaATela traduz o último Polish do runtime para a API local
// (polish_ultimo_motivo, fatia S5): o motivo, o provedor do keydown e a
// hora. Nenhum Polish ainda: false, e a tela não mostra nada. Só o enum
// atravessa — a frase é da API local, e texto ditado nunca passa aqui.
func ultimoPolishParaATela(ultimo func() winruntime.EstadoPolish) func() (localapi.DesfechoDoPolish, bool) {
	return func() (localapi.DesfechoDoPolish, bool) {
		e := ultimo()
		if e.Quando.IsZero() {
			return localapi.DesfechoDoPolish{}, false
		}
		return localapi.DesfechoDoPolish{Motivo: string(e.Motivo), Provedor: e.Provedor, Quando: e.Quando}, true
	}
}
