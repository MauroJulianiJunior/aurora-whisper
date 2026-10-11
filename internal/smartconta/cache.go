package smartconta

import (
	"context"
	"sync"
)

// CacheMaxPadrao é quantas contas o Cache guarda por padrão. Uma conta com 500
// pares ocupa cerca de 0,7 MiB (medido em 10-out-2026), e a API roda com
// GOMEMLIMIT de 200 MiB: 16 contas no pior caso são uns 12 MiB.
const CacheMaxPadrao = 16

// Cache guarda o Smart da conta já montado, uma conta por usuário, para o
// ditado seguinte não montá-lo de novo.
//
// Existe porque a montagem de 500 pares não cabe nos 25 ms da meta da ADR-021
// (veja BenchmarkMontar500 e BenchmarkResolver500): resolver um ditado com o
// Smart pronto leva poucos milissegundos, montar leva centenas. A ADR já previa
// "um cache por conta, invalidado pelo last_seq" para esse caso. A versão que
// invalida é de quem chama (Obter): o Cache só compara.
//
// # Uma montagem por conta
//
// Dois ditados da mesma conta ao mesmo tempo esperam a mesma montagem, em vez de
// montar duas vezes. A montagem roda numa goroutine própria: quem desiste de
// esperar (o contexto cancelado) não a interrompe, e o resultado fica no cache
// para o ditado seguinte.
//
// # O que não é guardado
//
// O erro de montagem nunca é guardado: a próxima chamada tenta de novo.
//
// Seguro para uso concorrente.
type Cache struct {
	mu     sync.Mutex
	max    int
	itens  map[string]*itemDoCache
	pulsos uint64
}

type itemDoCache struct {
	conta  *Conta
	versao string
	// uso é o último pulso em que alguém obteve este item; o de menor uso sai
	// quando o cache enche.
	uso uint64
	// montando é a montagem em andamento, a mais recente pedida.
	montando *montagemEmAndamento
}

type montagemEmAndamento struct {
	versao string
	pronto chan struct{}
	conta  *Conta
	err    error
}

// NovoCache cria um Cache de até max contas; max <= 0 é CacheMaxPadrao.
func NovoCache(max int) *Cache {
	if max <= 0 {
		max = CacheMaxPadrao
	}
	return &Cache{max: max, itens: map[string]*itemDoCache{}}
}

// Obter devolve o Smart da conta usuario na versão dada: o que está guardado
// para essa versão, ou o que montar devolver (guardado em seguida). versao é
// opaca para o Cache: qualquer mudança no aprendido da conta, inclusive um par
// desfeito, tem de mudar a versão (o last_seq da conta, e não o maior last_seq
// dos pares ativos, que cai quando o par mais novo é desfeito).
//
// Se a versão guardada é outra, a conta é remontada. Se já há uma montagem da
// mesma versão em andamento, espera por ela. Se ctx termina antes, devolve o
// erro do contexto, e a montagem segue.
func (c *Cache) Obter(ctx context.Context, usuario, versao string, montar func() (*Conta, error)) (*Conta, error) {
	c.mu.Lock()
	it := c.itens[usuario]
	if it != nil && it.conta != nil && it.versao == versao {
		c.pulsos++
		it.uso = c.pulsos
		conta := it.conta
		c.mu.Unlock()
		return conta, nil
	}

	var m *montagemEmAndamento
	if it != nil && it.montando != nil && it.montando.versao == versao {
		m = it.montando
	} else {
		m = &montagemEmAndamento{versao: versao, pronto: make(chan struct{})}
		if it == nil {
			it = &itemDoCache{}
			c.itens[usuario] = it
		}
		it.montando = m
		go c.montar(usuario, m, montar)
	}
	c.mu.Unlock()

	select {
	case <-m.pronto:
		return m.conta, m.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// montar roda a montagem e, se ela ainda é a mais recente da conta, guarda o
// resultado.
func (c *Cache) montar(usuario string, m *montagemEmAndamento, montar func() (*Conta, error)) {
	defer close(m.pronto)
	defer func() {
		// Uma falha inesperada da montagem vira o erro de sempre, sem o texto
		// do pânico, que pode citar o que a pessoa ditou.
		if recover() != nil {
			m.conta, m.err = nil, ErrMontar
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		it := c.itens[usuario]
		if it == nil || it.montando != m {
			return // outra montagem, mais nova, tomou o lugar, ou a conta foi esquecida
		}
		it.montando = nil
		if m.err != nil {
			if it.conta == nil {
				delete(c.itens, usuario)
			}
			return
		}
		c.pulsos++
		it.conta, it.versao, it.uso = m.conta, m.versao, c.pulsos
		c.enxugar(usuario)
	}()
	m.conta, m.err = montar()
}

// enxugar tira as contas de menor uso até caber em max. Nunca tira a conta
// poupada nem a que está sendo montada. Chamador segura c.mu.
func (c *Cache) enxugar(poupada string) {
	for len(c.itens) > c.max {
		velha, uso := "", uint64(0)
		for u, it := range c.itens {
			if u == poupada || it.montando != nil {
				continue
			}
			if velha == "" || it.uso < uso {
				velha, uso = u, it.uso
			}
		}
		if velha == "" {
			return
		}
		delete(c.itens, velha)
	}
}

// Esquecer tira a conta do cache: "Apagar meus dados", o ditado desligado, ou o
// que fizer o aprendido dela deixar de valer. Uma montagem em andamento termina
// para quem a espera, e não é guardada.
func (c *Cache) Esquecer(usuario string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.itens, usuario)
}

// Len diz quantas contas o cache tem (as montadas e as sendo montadas).
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.itens)
}
