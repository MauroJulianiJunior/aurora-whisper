package server

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O que a rota de ditar guarda em memória (ADR-021, "Idempotência" e
// "Limites"; C5b): os ditados em andamento, o "sem fala" e o desfecho pago e
// não gravado dos últimos 10 minutos, as vagas e os limites por conta, e os
// trabalhos soltos que o desligamento espera. Tudo por processo: reiniciar
// esquece (ADR-021, risco 11: duas instâncias da API exigiriam levar isto ao
// banco).
//
// Tudo é indexado por (user_id, id), nunca só pelo id: o mesmo id em duas contas
// são dois ditados, e uma conta nunca espera nem recebe a resposta da outra.
//
// O "em andamento" e o "sem fala" guardam só o desfecho (o status, o código, o
// aviso do Polish), nunca o áudio nem o texto. O desfecho pago e não gravado
// guarda o que seria gravado (o texto do ASR e o final, o modo, o provedor e o
// modelo), porque é isso que o reenvio grava: nunca vai a log, e some em 10
// minutos, no reinício, quando o epoch da conta muda, no "Apagar meus dados" e
// no "Desligar o ditado".

// chaveDoDitado identifica um ditado na memória: a conta e o id.
type chaveDoDitado struct {
	usuario, id string
}

// desfecho é como um ditado terminou, sem texto: a resposta de erro (status,
// código e frase fixa) ou o sucesso (o ditado está gravado), com o aviso do
// Polish. É o que quem esperou o mesmo id recebe.
type desfecho struct {
	status      int
	code, frase string
	// retryAfter, em segundos, para os 429 e o 503.
	retryAfter int
	// aviso é o motivo de o Polish ter caído para o Smart (um
	// resolver.MotivoFallback), só no sucesso.
	aviso string
	// reprocessar: o dono do ditado parou antes do provedor (o corpo dele não
	// chegou, ou não era webm). Quem esperava tenta com o próprio corpo.
	reprocessar bool
}

// ok diz se o ditado ficou gravado.
func (d desfecho) ok() bool { return d.code == "" && !d.reprocessar }

// andamento é um ditado em andamento: o primeiro pedido com o id é o dono, e
// os outros esperam pronto.
type andamento struct {
	pronto chan struct{}
	// desfecho é escrito antes de pronto fechar, e só lido depois.
	desfecho desfecho
	// vaga: o ditado pegou uma vaga (ele lê um corpo e chama o provedor). O
	// reenvio que só grava o desfecho pago não pega.
	vaga bool
}

// pagoNaoGravado é o desfecho que o provedor cobrou e a gravação não guardou.
type pagoNaoGravado struct {
	epoch  string
	novo   cloudstore.NewDictation
	aviso  string
	expira time.Time
}

// limitesDoDitado são os limites da rota, já com os padrões.
type limitesDoDitado struct {
	porConta, porMinuto, porDia, total int
}

// baldeDoDitado é um balde de fichas com a capacidade e o período de quem o
// usa (10 por minuto, 200 por dia).
type baldeDoDitado struct {
	fichas float64
	em     time.Time
}

func (b *baldeDoDitado) encher(now time.Time, capacidade int, periodo time.Duration) {
	if d := now.Sub(b.em); d > 0 {
		b.fichas = math.Min(float64(capacidade), b.fichas+float64(capacidade)*d.Seconds()/periodo.Seconds())
	}
	b.em = now
}

// espera é quanto falta para a próxima ficha.
func (b *baldeDoDitado) espera(capacidade int, periodo time.Duration) time.Duration {
	falta := 1 - b.fichas
	if falta <= 0 {
		return 0
	}
	return time.Duration(math.Ceil(falta / float64(capacidade) * float64(periodo)))
}

// registroDoDitado é a memória da rota de ditar. Seguro para uso concorrente.
type registroDoDitado struct {
	mu  sync.Mutex
	now func() time.Time
	lim limitesDoDitado

	andamentos map[chaveDoDitado]*andamento
	// vagasPorConta e vagas são os ditados com vaga, por conta e no total.
	vagasPorConta map[string]int
	vagas         int
	// semFala e pagos vencem em memoriaDoDitado.
	semFala map[chaveDoDitado]time.Time
	pagos   map[chaveDoDitado]pagoNaoGravado
	// porMinuto e porDia são os baldes de cada conta.
	porMinuto, porDia map[string]*baldeDoDitado

	// trabalhos é quantos trabalhos soltos estão rodando; ocioso fecha quando
	// o número chega a zero (e é trocado quando sobe de novo).
	trabalhos  int
	ocioso     chan struct{}
	desligando bool
}

// memoriaDoDitado: o "sem fala" e o desfecho pago e não gravado ficam 10
// minutos (ADR-021, escolha menor 23).
const memoriaDoDitado = 10 * time.Minute

// esperaDaVaga é o Retry-After do UM_POR_VEZ e do OCUPADO: um ditado típico leva
// de 2 a 4 s.
const esperaDaVaga = 5 * time.Second

// maxBaldes: acima disso, os baldes cheios (iguais a um novo) saem.
const maxBaldes = 10000

func novoRegistroDoDitado(now func() time.Time, lim limitesDoDitado) *registroDoDitado {
	ocioso := make(chan struct{})
	close(ocioso)
	return &registroDoDitado{
		now: now, lim: lim,
		andamentos:    map[chaveDoDitado]*andamento{},
		vagasPorConta: map[string]int{},
		semFala:       map[chaveDoDitado]time.Time{},
		pagos:         map[chaveDoDitado]pagoNaoGravado{},
		porMinuto:     map[string]*baldeDoDitado{},
		porDia:        map[string]*baldeDoDitado{},
		ocioso:        ocioso,
	}
}

// olhada é o que a memória diz de um id antes de o pedido ler o corpo.
type olhada struct {
	// esperar: o mesmo id está em andamento.
	esperar *andamento
	// semFala: o mesmo id deu SEM_FALA nos últimos 10 minutos.
	semFala bool
	// regravar: há o desfecho pago e não gravado, do mesmo epoch; o pedido é o
	// dono do andamento, que já foi registrado (sem vaga).
	regravar *pagoNaoGravado
	dono     *andamento
}

// olhar confere a memória para o id, com o epoch que a conta tem agora. Um
// desfecho pago de outro epoch (a conta apagou os dados ou o banco foi
// restaurado depois) é esquecido.
func (g *registroDoDitado) olhar(k chaveDoDitado, epoch string) olhada {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.vencer(now)
	if a := g.andamentos[k]; a != nil {
		return olhada{esperar: a}
	}
	if _, ok := g.semFala[k]; ok {
		return olhada{semFala: true}
	}
	if p, ok := g.pagos[k]; ok && !g.desligando {
		delete(g.pagos, k)
		if p.epoch != epoch {
			return olhada{}
		}
		a := &andamento{pronto: make(chan struct{})}
		g.andamentos[k] = a
		return olhada{regravar: &p, dono: a}
	}
	return olhada{}
}

// recusaDaVaga é por que o pedido não pegou vaga.
type recusaDaVaga struct {
	status      int
	code, frase string
	espera      time.Duration
	motivo      string
}

// olharDeNovo é o andamento que entrar devolve para esperar quando o id ganhou
// um desfecho na memória (o "sem fala", ou o pago e não gravado) depois da
// olhada do pedido: o primeiro com o mesmo id terminou entre a olhada e a vaga.
// Já está pronto, com o reprocessar: quem o espera volta na hora e olha de novo.
// Nunca entra no mapa dos andamentos, e ninguém o conclui.
var olharDeNovo = func() *andamento {
	a := &andamento{pronto: make(chan struct{}), desfecho: desfecho{reprocessar: true}}
	close(a.pronto)
	return a
}()

// entrar tenta pegar a vaga do ditado k: o mesmo id em andamento é esperado; o
// id que ganhou um desfecho na memória depois da olhada volta à olhada
// (olharDeNovo); o desligamento, a conta com outro ditado em andamento, os
// limites da conta e as vagas do total recusam. Só o pedido que pega a vaga
// gasta as fichas da conta (a resposta idempotente não conta).
func (g *registroDoDitado) entrar(k chaveDoDitado) (dono, esperar *andamento, recusa *recusaDaVaga) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if a := g.andamentos[k]; a != nil {
		return nil, a, nil
	}
	now := g.now()
	switch {
	case g.desligando:
		return nil, nil, &recusaDaVaga{status: 503, code: codeOcupado, frase: fraseOcupado, espera: esperaDaVaga, motivo: "desligando"}
	case g.temDesfecho(k, now):
		// Sem isto, o reenvio que olhou a memória antes de o primeiro terminar
		// pegaria a vaga e pagaria o provedor de novo pelo mesmo ditado.
		return nil, olharDeNovo, nil
	case g.vagasPorConta[k.usuario] >= g.lim.porConta:
		return nil, nil, &recusaDaVaga{status: 429, code: codeUmPorVez, frase: fraseUmPorVez, espera: esperaDaVaga, motivo: "um_por_vez"}
	}
	minuto := g.balde(g.porMinuto, k.usuario, g.lim.porMinuto, time.Minute, now)
	dia := g.balde(g.porDia, k.usuario, g.lim.porDia, 24*time.Hour, now)
	if minuto.fichas < 1 || dia.fichas < 1 {
		espera := max(minuto.espera(g.lim.porMinuto, time.Minute), dia.espera(g.lim.porDia, 24*time.Hour))
		return nil, nil, &recusaDaVaga{status: 429, code: codeLimite, frase: fraseLimiteDoDitado, espera: espera, motivo: "limite"}
	}
	if g.vagas >= g.lim.total {
		return nil, nil, &recusaDaVaga{status: 503, code: codeOcupado, frase: fraseOcupado, espera: esperaDaVaga, motivo: "ocupado"}
	}
	minuto.fichas--
	dia.fichas--
	g.vagasPorConta[k.usuario]++
	g.vagas++
	a := &andamento{pronto: make(chan struct{}), vaga: true}
	g.andamentos[k] = a
	return a, nil, nil
}

// balde é o balde da conta no mapa, cheio no começo, e já enchido até now.
// Chamador segura mu.
func (g *registroDoDitado) balde(m map[string]*baldeDoDitado, usuario string, capacidade int, periodo time.Duration, now time.Time) *baldeDoDitado {
	b := m[usuario]
	if b == nil {
		if len(m) >= maxBaldes {
			// Os baldes cheios são iguais a um novo: saem.
			for u, velho := range m {
				velho.encher(now, capacidade, periodo)
				if velho.fichas >= float64(capacidade) {
					delete(m, u)
				}
			}
		}
		b = &baldeDoDitado{fichas: float64(capacidade), em: now}
		m[usuario] = b
	}
	b.encher(now, capacidade, periodo)
	return b
}

// temDesfecho diz se a memória tem, ainda dentro dos 10 minutos, o "sem fala"
// ou o desfecho pago e não gravado do id. Chamador segura mu.
func (g *registroDoDitado) temDesfecho(k chaveDoDitado, now time.Time) bool {
	if exp, ok := g.semFala[k]; ok && now.Before(exp) {
		return true
	}
	p, ok := g.pagos[k]
	return ok && now.Before(p.expira)
}

// concluir encerra o ditado k: o desfecho fica para quem espera, a vaga volta, e
// a memória ganha o "sem fala" ou o desfecho pago e não gravado, na mesma trava
// (quem chega depois vê um ou outro, nunca nenhum dos dois).
func (g *registroDoDitado) concluir(k chaveDoDitado, a *andamento, d desfecho, semFala bool, pago *pagoNaoGravado) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.concluirTravado(k, a, d, semFala, pago)
}

// desistir encerra o andamento k de um pedido que pegou a vaga e não chegou a
// processar o ditado (a releitura da situação, já como dono do id, achou o
// ditado gravado ou a lápide, ou falhou): as fichas voltam à conta (só o envio
// processado conta no limite, e a resposta idempotente não), a vaga volta, e
// quem espera olha de novo.
func (g *registroDoDitado) desistir(k chaveDoDitado, a *andamento) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.vaga {
		if b := g.porMinuto[k.usuario]; b != nil {
			b.fichas = math.Min(float64(g.lim.porMinuto), b.fichas+1)
		}
		if b := g.porDia[k.usuario]; b != nil {
			b.fichas = math.Min(float64(g.lim.porDia), b.fichas+1)
		}
	}
	g.concluirTravado(k, a, desfecho{reprocessar: true}, false, nil)
}

// concluirTravado é o concluir com mu já segura.
func (g *registroDoDitado) concluirTravado(k chaveDoDitado, a *andamento, d desfecho, semFala bool, pago *pagoNaoGravado) {
	now := g.now()
	if semFala {
		g.semFala[k] = now.Add(memoriaDoDitado)
	}
	if pago != nil {
		p := *pago
		p.expira = now.Add(memoriaDoDitado)
		g.pagos[k] = p
	}
	if a.vaga {
		a.vaga = false
		if g.vagasPorConta[k.usuario]--; g.vagasPorConta[k.usuario] <= 0 {
			delete(g.vagasPorConta, k.usuario)
		}
		g.vagas--
	}
	if g.andamentos[k] == a {
		delete(g.andamentos, k)
	}
	a.desfecho = d
	close(a.pronto)
}

// vencer tira o "sem fala" e o pago que passaram dos 10 minutos. Chamador
// segura mu.
func (g *registroDoDitado) vencer(now time.Time) {
	for k, exp := range g.semFala {
		if !now.Before(exp) {
			delete(g.semFala, k)
		}
	}
	for k, p := range g.pagos {
		if !now.Before(p.expira) {
			delete(g.pagos, k)
		}
	}
}

// esquecerConta tira da memória o "sem fala" e o desfecho pago da conta: o
// "Apagar meus dados" e o "Desligar o ditado". O texto do pago não fica 10
// minutos depois de a pessoa pedir para apagar.
func (g *registroDoDitado) esquecerConta(usuario string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k := range g.semFala {
		if k.usuario == usuario {
			delete(g.semFala, k)
		}
	}
	for k := range g.pagos {
		if k.usuario == usuario {
			delete(g.pagos, k)
		}
	}
}

// comecarTrabalho conta um trabalho solto (o provedor, o Smart, o Polish e a
// gravação de um ditado), que o desligamento espera.
func (g *registroDoDitado) comecarTrabalho() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.trabalhos == 0 {
		g.ocioso = make(chan struct{})
	}
	g.trabalhos++
}

func (g *registroDoDitado) terminarTrabalho() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.trabalhos--; g.trabalhos == 0 {
		close(g.ocioso)
	}
}

// esperarTrabalhos marca o desligamento (nenhum ditado novo pega vaga) e espera
// os trabalhos soltos terminarem, até ctx acabar.
func (g *registroDoDitado) esperarTrabalhos(ctx context.Context) error {
	for {
		g.mu.Lock()
		g.desligando = true
		n, ocioso := g.trabalhos, g.ocioso
		g.mu.Unlock()
		if n == 0 {
			return nil
		}
		select {
		case <-ocioso:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// emAndamento diz quantos trabalhos soltos estão rodando (para os testes e o log
// do desligamento).
func (g *registroDoDitado) emAndamento() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.trabalhos
}
