package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Config são os parâmetros da execução.
type Config struct {
	Casos    []Caso
	Rodadas  int
	Paralelo int
	// PrazoAgente é o prazo do Agent por ditado (3,5 s): passou disso, o Agent
	// usaria o texto do Smart. Só classifica; a chamada roda com PrazoMax.
	PrazoAgente time.Duration
	// PrazoMax é o prazo real de cada chamada aqui, maior que o do Agent para
	// medir também o tempo das que estourariam.
	PrazoMax time.Duration
	// MinPalavras e MaxEdicao são os parâmetros das guardas; zero usa o padrão
	// do Agent.
	MinPalavras int
	MaxEdicao   float64
	// LimiteUSD interrompe a rodada quando o gasto passa disto.
	LimiteUSD float64
	// ConexaoNova abre uma conexão nova a cada chamada (sem keep-alive): a
	// latência do pior caso do Agent, com ditados espaçados.
	ConexaoNova bool
}

// Resultado é uma chamada: um caso, numa rodada, para um candidato.
type Resultado struct {
	Caso   int
	Rodada int
	// Fria marca a primeira chamada que foi ao provedor, com o cliente e o
	// esquema ainda frios.
	Fria bool
	// Enviada diz que o pedido foi entregue ao cliente do provedor (não foi
	// barrado por ser curto).
	Enviada bool
	// Bruto é o JSON que o modelo devolveu ({"final_text": ...}); nil quando
	// não houve resposta.
	Bruto []byte
	// Duracao é o tempo da chamada ao provedor, medido com o prazo maior.
	Duracao time.Duration
	Tokens  ia.Tokens
	// Res é o que resolver.Polir devolveu: o que o Agent faria.
	Res resolver.ResolveResult
}

// gravador é o PolishClient que o Polir chama: repassa ao cliente do provedor
// e guarda o que o modelo devolveu e quanto demorou. É o que permite medir
// ANTES da guarda, sem que o Polir saiba de nada.
type gravador struct {
	interno resolver.PolishClient
	agora   func() time.Time

	enviada bool
	bruto   []byte
	dur     time.Duration
}

func (g *gravador) Polish(ctx context.Context, req resolver.PolishRequest) ([]byte, error) {
	g.enviada = true
	ini := g.agora()
	b, err := g.interno.Polish(ctx, req)
	g.dur = g.agora().Sub(ini)
	g.bruto = b
	return b, err
}

// gasto soma o custo da rodada inteira, de todos os candidatos.
type gasto struct {
	mu    sync.Mutex
	total float64
}

func (g *gasto) somar(v float64) float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.total += v
	return g.total
}

func (g *gasto) valor() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.total
}

// SaidaCandidato é o que a execução de um candidato produziu.
type SaidaCandidato struct {
	Cand       Candidato
	Preco      Preco
	Resultados []Resultado
	// Planejadas é o número de chamadas previstas (casos com palavras
	// suficientes, vezes as rodadas).
	Planejadas int
	// Aborto é vazio quando o candidato completou; senão, por que parou: um
	// motivo do provedor (chave_recusada, sem_credito, modelo_inexistente,
	// modelo_incompativel, pedido_recusado: três seguidos), "limite_de_gasto"
	// ou "interrompido".
	Aborto string
	// Status é o que o observador viu (status HTTP e identificadores de erro).
	Status map[string]int
	// Mensagens são as mensagens higienizadas dos erros de pedido (400, 404,
	// 422), com a contagem.
	Mensagens map[string]int
	// Envelopes são as impressões digitais (só a forma, nunca texto) das
	// respostas 200, com a contagem.
	Envelopes map[string]int
}

const (
	AbortoLimite       = "limite_de_gasto"
	AbortoInterrompido = "interrompido"
	// seguidasParaAbortar é quantas chamadas seguidas com o mesmo motivo
	// terminal (que repetir não resolve) encerram o candidato.
	seguidasParaAbortar = 3
)

// motivoTerminal diz se repetir a chamada não resolve: a chave, a conta, o
// modelo ou o pedido estão errados.
func motivoTerminal(m resolver.MotivoFallback) bool {
	switch m {
	case resolver.MotivoChaveRecusada, resolver.MotivoSemCredito, resolver.MotivoModeloInexistente,
		resolver.MotivoModeloIncompativel, resolver.MotivoPedidoRecusado:
		return true
	}
	return false
}

// Executor roda os candidatos sobre o dataset.
type Executor struct {
	Cfg   Config
	Agora func() time.Time
	// Progresso recebe uma linha a cada tantas chamadas. Só números.
	Progresso io.Writer
}

type tarefa struct{ caso, rodada int }

// Planejadas conta as chamadas que a rodada vai fazer por candidato.
func (e *Executor) planejadas() int {
	min := e.Cfg.MinPalavras
	if min <= 0 {
		min = resolver.PolishMinPalavras
	}
	n := 0
	for _, c := range e.Cfg.Casos {
		if len(palavrasDe(c.Entrada)) >= min {
			n++
		}
	}
	return n * e.Cfg.Rodadas
}

// Rodar executa um candidato: cada caso, em cada rodada, pelo mesmo caminho
// do Agent. A chave só existe dentro da função que o Polidor chama a cada
// ditado; nada dela vai a campo de struct, log ou relatório.
func (e *Executor) Rodar(ctx context.Context, cand Candidato, preco Preco, cli *ia.Cliente, obs *observador, chave string, total *gasto) SaidaCandidato {
	tarefas := make([]tarefa, 0, len(e.Cfg.Casos)*e.Cfg.Rodadas)
	for r := 1; r <= e.Cfg.Rodadas; r++ {
		for i := range e.Cfg.Casos {
			tarefas = append(tarefas, tarefa{caso: i, rodada: r})
		}
	}
	saida := SaidaCandidato{Cand: cand, Preco: preco, Planejadas: e.planejadas()}
	resultados := make([]Resultado, len(tarefas))
	feito := make([]bool, len(tarefas))

	ctxCand, cancelar := context.WithCancel(ctx)
	defer cancelar()

	var mu sync.Mutex
	var seguidas int
	var ultimo resolver.MotivoFallback
	// categoriasDaSequencia são as categorias das frases da sequência atual de
	// erros terminais iguais.
	var categoriasDaSequencia map[string]bool
	enviadas := 0

	registrar := func(k int, r Resultado) {
		mu.Lock()
		defer mu.Unlock()
		resultados[k], feito[k] = r, true
		if !r.Enviada {
			return
		}
		enviadas++
		if motivoTerminal(r.Res.MotivoFallback) {
			cat := e.Cfg.Casos[tarefas[k].caso].Categoria
			if r.Res.MotivoFallback == ultimo {
				seguidas++
				categoriasDaSequencia[cat] = true
			} else {
				ultimo, seguidas = r.Res.MotivoFallback, 1
				categoriasDaSequencia = map[string]bool{cat: true}
			}
			// O dataset é agrupado por categoria: um provedor que recusa um TIPO
			// de frase (as injeções, por exemplo) daria três recusas seguidas
			// sem que o protocolo esteja errado. Só aborta quando a sequência
			// atravessa duas categorias ou mais.
			if seguidas >= seguidasParaAbortar && len(categoriasDaSequencia) >= 2 && saida.Aborto == "" {
				saida.Aborto = string(ultimo)
				cancelar()
			}
		} else {
			ultimo, seguidas = resolver.MotivoNenhum, 0
		}
		if v := total.somar(custoDe(r.Tokens, preco)); v > e.Cfg.LimiteUSD && saida.Aborto == "" {
			saida.Aborto = AbortoLimite
			cancelar()
		}
		if e.Progresso != nil && enviadas%25 == 0 {
			fmt.Fprintf(e.Progresso, "  [%s] %d/%d chamadas, gasto total %s\n", cand.Rotulo(), enviadas, saida.Planejadas, usd(total.valor(), 4))
		}
	}
	executar := func(k int) {
		if ctxCand.Err() != nil {
			return
		}
		r := e.chamar(ctxCand, cand, cli, chave, tarefas[k])
		// Uma chamada cortada por cancelamento não mediu nada: descarta.
		if ctxCand.Err() != nil {
			return
		}
		registrar(k, r)
	}

	// A primeira chamada que vai ao provedor roda sozinha: é a fria.
	k := 0
	for ; k < len(tarefas) && ctxCand.Err() == nil; k++ {
		executar(k)
		if feito[k] && resultados[k].Enviada {
			resultados[k].Fria = true
			k++
			break
		}
	}
	if e.Cfg.Paralelo <= 1 {
		for ; k < len(tarefas); k++ {
			executar(k)
		}
	} else {
		var wg sync.WaitGroup
		sem := make(chan struct{}, e.Cfg.Paralelo)
		for ; k < len(tarefas); k++ {
			sem <- struct{}{}
			wg.Add(1)
			go func(k int) {
				defer wg.Done()
				defer func() { <-sem }()
				executar(k)
			}(k)
		}
		wg.Wait()
	}

	mu.Lock()
	defer mu.Unlock()
	for k, ok := range feito {
		if ok {
			saida.Resultados = append(saida.Resultados, resultados[k])
		}
	}
	if saida.Aborto == "" && ctx.Err() != nil {
		saida.Aborto = AbortoInterrompido
	}
	if obs != nil {
		saida.Status = obs.Contagens()
		saida.Mensagens = obs.Mensagens()
		saida.Envelopes = obs.Envelopes()
	}
	return saida
}

// chamar faz uma chamada: resolver.Polir com o Polidor de internal/ia, as
// mesmas guardas e a mesma entrada do Agent, com um prazo maior para medir o
// tempo das chamadas que o Agent cortaria.
func (e *Executor) chamar(ctx context.Context, cand Candidato, cli *ia.Cliente, chave string, t tarefa) Resultado {
	caso := e.Cfg.Casos[t.caso]
	var tokens ia.Tokens
	polidor := &ia.Polidor{
		Cliente: cli,
		Chamada: cand.Chamada(),
		Chave:   func(context.Context) (string, error) { return chave, nil },
		Uso:     func(u ia.Tokens) { tokens = u },
	}
	g := &gravador{interno: polidor, agora: e.Agora}

	ctxChamada, cancelar := context.WithTimeout(ctx, e.Cfg.PrazoMax)
	defer cancelar()
	smart := resolver.ResolveResult{Mode: resolver.ModeSmart, RawText: caso.Entrada, FinalText: caso.Entrada}
	res := resolver.Polir(ctxChamada, g, smart, resolver.PolirOpcoes{
		Idioma:      "pt",
		MinPalavras: e.Cfg.MinPalavras,
		MaxEdicao:   e.Cfg.MaxEdicao,
		Termos:      caso.Termos,
	})
	return Resultado{
		Caso: t.caso, Rodada: t.rodada,
		Enviada: g.enviada, Bruto: g.bruto, Duracao: g.dur, Tokens: tokens, Res: res,
	}
}
