package polish

import (
	"context"
	"errors"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Montagem tem tudo de que o Polish de UM ditado precisa. Quem dita (o
// Agent, a cada ditado; o servidor, a cada pedido) a preenche e chama
// Abrir. É um valor para ser montado na hora: nada nela dura além do
// ditado, a não ser o que já é compartilhado de propósito (o Cliente e a
// Esforco).
type Montagem struct {
	// Smart é o Smart do ditado: as guardas do Polish o consultam
	// localmente (resolver.PolishResolver.Reescrever). Nunca vai ao
	// provedor.
	Smart *resolver.SmartResolver
	// Cliente é o transporte HTTP dos provedores (internal/ia). Nil usa o
	// valor zero de ia.Cliente, que fala com as APIs oficiais.
	Cliente *ia.Cliente
	// Esforco é a política do processo. Obrigatória quando Provedor é
	// válido.
	Esforco *PoliticaEsforco
	// Provedor e Modelo são os do ditado (no Agent, os do keydown).
	Provedor, Modelo string
	// Chave devolve a chave de quem dita, a cada chamada ao provedor, ou
	// resolver.ErrPolishSemChave se não há. Nenhum campo daqui nem do
	// ia.Polidor a guarda (ADR-016, decisão 2): remover a chave vale no
	// ditado seguinte. Um erro dela, qualquer que seja, vira o motivo
	// sem_chave (ia.Polidor), sem abrir a rede. Nil é "sem chave".
	Chave func(context.Context) (string, error)
	// Contar, se não for nil, recebe os tokens que o provedor informou, da
	// goroutine da chamada. Só números.
	Contar func(entrada, saida int)
}

// Abrir monta o resolver.PolishResolver do ditado e devolve também o
// rótulo do esforço de raciocínio que a chamada vai mandar, só para o log
// ("nao_se_aplica" fora da OpenAI; "" quando não há provedor).
//
// Sem provedor válido (a API local do Agent já recusa o Polish assim) não
// há cliente: o resolver devolve sem_cliente sem chamar ninguém nem ler a
// chave.
func (m Montagem) Abrir() (*resolver.PolishResolver, string) {
	pr := &resolver.PolishResolver{Smart: m.Smart}
	if !ia.Valido(m.Provedor) {
		return pr, ""
	}
	esforco := m.Esforco.Atual(m.Provedor, m.Modelo)
	pr.Client = &clienteDoDitado{
		polidor: ia.Polidor{
			Cliente: m.Cliente,
			Chamada: ia.Chamada{Provedor: m.Provedor, Modelo: m.Modelo, Esforco: esforco},
			Chave:   m.Chave,
			Uso: func(t ia.Tokens) {
				if m.Contar != nil {
					m.Contar(t.Entrada, t.Saida)
				}
			},
		},
		politica: m.Esforco,
		provedor: m.Provedor,
		modelo:   m.Modelo,
		esforco:  esforco,
	}
	return pr, RotuloDoEsforco(m.Provedor, esforco)
}

// clienteDoDitado é o resolver.PolishClient de um ditado: o ia.Polidor, e
// a política de esforço ouvindo a resposta.
type clienteDoDitado struct {
	polidor          ia.Polidor
	politica         *PoliticaEsforco
	provedor, modelo string
	esforco          string
}

var _ resolver.PolishClient = (*clienteDoDitado)(nil)

// Polish implementa resolver.PolishClient. O erro passa como veio (o
// ia.Polidor já o traduz num motivo fixo, sem corpo e sem chave).
func (c *clienteDoDitado) Polish(ctx context.Context, req resolver.PolishRequest) ([]byte, error) {
	texto, err := c.polidor.Polish(ctx, req)
	var e *resolver.ErroPolish
	if errors.As(err, &e) && e.Motivo == resolver.MotivoModeloIncompativel {
		c.politica.Recusou(c.provedor, c.modelo, c.esforco)
	}
	return texto, err
}
