package main

import (
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Os tipos de exemplo que o relatório mostra.
const (
	ExemploCriticaAntes     = "critica_antes"
	ExemploCriticaDepois    = "critica_depois"
	ExemploGuardaRecusouBom = "guarda_recusou_sem_falha_critica"
	// ExemploMaiusculaAMais é a saída com palavra em maiúscula no meio da frase
	// que o esperado escreve em minúscula (só frases sintéticas).
	ExemploMaiusculaAMais = "maiuscula_a_mais"

	// maxExemplosPorTipo limita quantos exemplos de cada tipo o relatório traz
	// por candidato.
	maxExemplosPorTipo = 12
)

// Exemplo é um caso que o relatório mostra por inteiro. As frases são todas
// sintéticas (dataset do benchmark).
type Exemplo struct {
	Tipo      string
	CasoID    string
	Categoria string
	Rodada    int
	Entrada   string
	Esperado  string
	Saida     string
	Motivo    resolver.MotivoFallback
	Falhas    []TipoFalha
	// Palavras são, no exemplo de maiúscula a mais, as palavras contadas.
	Palavras []string
}

// PrimeiraChamada é o desfecho da primeira chamada que foi ao provedor.
type PrimeiraChamada struct {
	Presente bool
	Segundos float64
	// Motivo é o fallback da chamada; vazio quando a resposta foi aceita.
	Motivo resolver.MotivoFallback
}

// MetCategoria são as contagens de uma categoria do dataset.
type MetCategoria struct {
	Chamadas       int // enviadas ao provedor
	Fallbacks      int // como o Agent (com o corte de prazo)
	CriticasAntes  int
	CriticasDepois int
	TP, FP, FN     int
	// Maiúscula a mais: chamadas com alguma palavra, e o total de palavras.
	CaixaAMaisChamadas int
	CaixaAMaisPalavras int
}

// F1 é o F1 de pontuação da categoria.
func (c MetCategoria) F1() float64 { _, _, f := f1De(c.TP, c.FP, c.FN); return f }

// Metricas são todas as métricas de um candidato (19-eval-plan, "Polish").
type Metricas struct {
	Cand       Candidato
	Preco      Preco
	Aborto     string
	Planejadas int

	// Chamadas foram ao provedor; NaoEnviadas foram barradas antes (curtas).
	Chamadas    int
	NaoEnviadas int
	// Sucessos são as chamadas em que o modelo devolveu um final_text lido.
	Sucessos int

	// Preservação: taxa de edição por palavra entre a entrada e a saída do
	// modelo (média e p95), em fração das palavras da entrada.
	Preservacao metrics.Stats

	// Falha crítica, contada em chamadas: ANTES da guarda (o que o modelo
	// errou) e DEPOIS (o que chegaria ao campo; tem de ser 0).
	ChamadasCriticasAntes  int
	ChamadasCriticasDepois int
	CriticasAntes          map[TipoFalha]int
	CriticasDepois         map[TipoFalha]int
	// Alterações leves antes da guarda (chamadas com omissão de palavra comum,
	// com sobra de hesitação ou pontuação falada e com nome deixado em
	// minúscula que só o esperado capitaliza, e com correção de grafia em que o
	// modelo e o esperado discordam).
	LevesOmissao int
	LevesSobra   int
	LevesCaixa   int
	LevesGrafia  int
	// Maiúscula a mais (só medida, fora da regra de escolha): chamadas em que a
	// saída põe maiúscula, no meio da frase, numa palavra que o esperado escreve
	// em minúscula (mês, dia da semana, substantivo comum), e o total dessas
	// palavras. Sobre as chamadas com final_text lido (Sucessos).
	CaixaAMaisChamadas int
	CaixaAMaisPalavras int
	// O que as guardas fizeram: barraram falha crítica; barraram saída sem
	// falha crítica (o preço delas), por motivo.
	GuardaBarrouCritica     int
	GuardaRecusouBom        int
	GuardaRecusouBomMotivos map[string]int

	// Organização.
	Precisao, Revocacao, F1 float64
	TP, FP, FN              int
	EstruturaOK             int
	EstruturaN              int
	EstruturaListaOK        int // só as frases cujo esperado tem lista ou parágrafos
	EstruturaListaN         int
	MarcadorOK              int // tipo do marcador da lista (1. ou -)
	MarcadorN               int
	JaOrganizadaIgual       int
	JaOrganizadaN           int

	// Latência, em segundos. Latencia é das chamadas mornas; a fria vem à
	// parte. AcimaDoPrazo conta, entre todas as medidas (fria incluída), as que
	// passaram do prazo do Agent.
	Latencia     metrics.Stats
	TemFria      bool
	Fria         float64
	LatenciaN    int
	AcimaDoPrazo int
	// Primeira é o que a primeira chamada (a fria) deu, mesmo quando falhou.
	Primeira PrimeiraChamada

	// Tokens e custo.
	TokensEntrada    int
	TokensSaida      int
	TokensRaciocinio int
	RaciocinioMax    int
	CustoTotal       float64
	CustoPor1000     float64

	// Fallback, como o Agent o veria: o motivo da guarda ou do transporte, e
	// "prazo" quando a chamada passou do prazo do Agent mesmo que a guarda
	// tivesse aceitado.
	FallbackTotal     int
	FallbackPorMotivo map[string]int
	AceitasMasLentas  int
	// AceitasMasLentasComCritica são, dessas, as que tinham falha crítica: o
	// Agent as descartaria pelo prazo, então não entram no "depois da guarda".
	AceitasMasLentasComCritica int
	ChamadasSemArmadilha       int
	FallbacksSemArmadilha      int

	// Obediência: injeção obedecida e pergunta respondida, entre as chamadas
	// respondidas das categorias injecao e pergunta.
	InjecaoN                 int
	InjecaoObedecidaAntes    int
	InjecaoObedecidaDepois   int
	PerguntaN                int
	PerguntaRespondidaAntes  int
	PerguntaRespondidaDepois int

	// Variação entre rodadas: dos casos com 2 rodadas ou mais, quantos deram
	// saídas diferentes.
	VariacaoN     int
	VariacaoCasos int

	Formas       map[string]int
	Status       map[string]int
	Mensagens    map[string]int
	Envelopes    map[string]int
	PorCategoria map[string]*MetCategoria
	Exemplos     []Exemplo
}

// F1Organizando é o F1 de pontuação fora da categoria ja_organizada. Nela o
// esperado é a própria entrada, então um modelo que devolve o texto sem mexer
// já acerta tudo; o F1 geral, que a inclui, esconde quem não organiza nada.
func (m Metricas) F1Organizando() float64 {
	var tp, fp, fn int
	for cat, c := range m.PorCategoria {
		if cat != CatJaOrganizada {
			tp, fp, fn = tp+c.TP, fp+c.FP, fn+c.FN
		}
	}
	_, _, f := f1De(tp, fp, fn)
	return f
}

// EstruturaListaTaxa é a fração das frases com lista ou parágrafos no esperado
// cuja estrutura o modelo acertou; ok=false quando nenhuma dessas frases foi
// medida.
func (m Metricas) EstruturaListaTaxa() (taxa float64, ok bool) {
	if m.EstruturaListaN == 0 {
		return 0, false
	}
	return float64(m.EstruturaListaOK) / float64(m.EstruturaListaN), true
}

// RefOrganizacao é o que um modelo que só devolve a entrada, sem organizar,
// marcaria neste dataset: o piso contra o qual se lê a organização de cada
// modelo.
type RefOrganizacao struct {
	// F1 é o F1 de pontuação fora de ja_organizada.
	F1 float64
	// Estrutura é o acerto de estrutura nas frases com lista ou parágrafos; só
	// vale com EstruturaN > 0.
	Estrutura  float64
	EstruturaN int
}

// referenciaDoEco mede o que devolver a entrada dá, nas frases que vão ao
// provedor.
func referenciaDoEco(casos []Caso, minPalavras int) RefOrganizacao {
	if minPalavras <= 0 {
		minPalavras = resolver.PolishMinPalavras
	}
	var tp, fp, fn, ok, n int
	for _, c := range casos {
		if len(palavrasDe(c.Entrada)) < minPalavras {
			continue
		}
		if c.Categoria != CatJaOrganizada {
			a, b, d := sinaisComparados(c.Esperado, c.Entrada)
			tp, fp, fn = tp+a, fp+b, fn+d
		}
		if ee := estruturaDe(c.Esperado); ee.Itens > 0 || ee.Paragrafos > 1 {
			n++
			if mesmaEstrutura(estruturaDe(c.Entrada), ee) {
				ok++
			}
		}
	}
	_, _, f := f1De(tp, fp, fn)
	ref := RefOrganizacao{F1: f, EstruturaN: n}
	if n > 0 {
		ref.Estrutura = float64(ok) / float64(n)
	}
	return ref
}

// FallbackTaxa é a taxa de fallback usada na regra de escolha: como o Agent o
// veria, sem as frases-armadilha (que existem para medir o preço das guardas,
// e não representam um ditado comum).
func (m Metricas) FallbackTaxa() float64 {
	return taxa(m.FallbacksSemArmadilha, m.ChamadasSemArmadilha)
}

// FallbackTaxaTodas é a taxa de fallback com todas as categorias.
func (m Metricas) FallbackTaxaTodas() float64 { return taxa(m.FallbackTotal, m.Chamadas) }

// PercentualAcimaDoPrazo é a fração das chamadas medidas que passaram do prazo
// do Agent.
func (m Metricas) PercentualAcimaDoPrazo() float64 { return taxa(m.AcimaDoPrazo, m.LatenciaN) }

func taxa(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// f1De calcula precisão, revocação e F1 a partir das contagens. Sem nenhum
// sinal dos dois lados (0, 0, 0), vale 1: nada a acertar, nada errado.
func f1De(tp, fp, fn int) (precisao, revocacao, f1 float64) {
	if tp+fp+fn == 0 {
		return 1, 1, 1
	}
	precisao, revocacao = 1, 1
	if tp+fp > 0 {
		precisao = float64(tp) / float64(tp+fp)
	}
	if tp+fn > 0 {
		revocacao = float64(tp) / float64(tp+fn)
	}
	if precisao+revocacao == 0 {
		return precisao, revocacao, 0
	}
	return precisao, revocacao, 2 * precisao * revocacao / (precisao + revocacao)
}

// sinaisComparados compara, palavra a palavra, o sinal de pontuação depois de
// cada palavra da saída com o do esperado. As palavras são alinhadas; o sinal
// depois de uma palavra sem par (acrescentada ou apagada) conta como erro.
func sinaisComparados(esperado, saida string) (tp, fp, fn int) {
	pe, ps := palavrasDe(esperado), palavrasDe(saida)
	pares := alinharPalavras(normas(pe), normas(ps))
	for _, p := range pares {
		se, ss := sinalDepois(pe[p.A]), sinalDepois(ps[p.B])
		switch {
		case se != 0 && se == ss:
			tp++
		case se != 0 && ss != 0:
			fn++
			fp++
		case se != 0:
			fn++
		case ss != 0:
			fp++
		}
	}
	for _, i := range naoCasadas(len(pe), pares, true) {
		if sinalDepois(pe[i]) != 0 {
			fn++
		}
	}
	for _, j := range naoCasadas(len(ps), pares, false) {
		if sinalDepois(ps[j]) != 0 {
			fp++
		}
	}
	return tp, fp, fn
}

// edicaoPorPalavra é a distância de edição entre a entrada e a saída, em
// fração das palavras da entrada.
func edicaoPorPalavra(entrada, saida string) (fracao float64, ok bool) {
	e := normas(palavrasDe(entrada))
	if len(e) == 0 {
		return 0, false
	}
	return float64(distanciaDePalavras(e, normas(palavrasDe(saida)))) / float64(len(e)), true
}

// referenciaDePreservacao mede a edição por palavra do `esperado` sobre a
// entrada, nas frases que vão ao provedor: o mínimo legítimo de edição.
func referenciaDePreservacao(casos []Caso, minPalavras int) metrics.Stats {
	if minPalavras <= 0 {
		minPalavras = resolver.PolishMinPalavras
	}
	var edicoes []float64
	for _, c := range casos {
		if len(palavrasDe(c.Entrada)) < minPalavras {
			continue
		}
		if e, ok := edicaoPorPalavra(c.Entrada, c.Esperado); ok {
			edicoes = append(edicoes, e)
		}
	}
	return metrics.Aggregate(edicoes)
}

// medeLatencia diz se a chamada entra nas estatísticas de latência: a que o
// modelo respondeu, e as que passaram do prazo, foram cortadas ou recusadas
// depois de gerar. Erro rápido do provedor (chave, crédito, modelo, pedido,
// limite, sobrecarga, rede) não mede o modelo.
func medeLatencia(r Resultado) bool {
	if !r.Enviada {
		return false
	}
	if r.Bruto != nil {
		return true
	}
	switch r.Res.MotivoFallback {
	case resolver.MotivoPrazo, resolver.MotivoTruncada, resolver.MotivoRecusa, resolver.MotivoRespostaInvalida:
		return true
	}
	return false
}

// motivoDoAgente é o desfecho como o Agent o veria: passou do prazo dele, é
// "prazo", mesmo que a guarda tivesse aceitado; senão, o que a guarda ou o
// transporte disseram (vazio = aceita).
func motivoDoAgente(r Resultado, prazo time.Duration) resolver.MotivoFallback {
	if r.Enviada && r.Duracao > prazo {
		return resolver.MotivoPrazo
	}
	return r.Res.MotivoFallback
}

// calcular junta as métricas de um candidato.
func calcular(cfg Config, s SaidaCandidato) Metricas {
	m := Metricas{
		Cand: s.Cand, Preco: s.Preco, Aborto: s.Aborto, Planejadas: s.Planejadas, Status: s.Status, Mensagens: s.Mensagens, Envelopes: s.Envelopes,
		CriticasAntes: map[TipoFalha]int{}, CriticasDepois: map[TipoFalha]int{},
		GuardaRecusouBomMotivos: map[string]int{},
		FallbackPorMotivo:       map[string]int{},
		Formas:                  map[string]int{},
		PorCategoria:            map[string]*MetCategoria{},
	}
	var edicoes, mornas []float64
	saidasPorCaso := map[int][]string{}
	exemplos := map[string]int{}
	vistos := map[string]bool{}

	adicionar := func(tipo string, caso Caso, r Resultado, saida string, falhas []TipoFalha, palavras []string) {
		// Um exemplo por frase e por tipo (o da primeira vez): o relatório
		// mostra o teto de frases diferentes, não a mesma frase de cada rodada.
		chave := tipo + "\x00" + caso.ID
		if exemplos[tipo] >= maxExemplosPorTipo || vistos[chave] {
			return
		}
		vistos[chave] = true
		exemplos[tipo]++
		m.Exemplos = append(m.Exemplos, Exemplo{
			Tipo: tipo, CasoID: caso.ID, Categoria: caso.Categoria, Rodada: r.Rodada,
			Entrada: caso.Entrada, Esperado: caso.Esperado, Saida: saida,
			Motivo: r.Res.MotivoFallback, Falhas: falhas, Palavras: palavras,
		})
	}

	for _, r := range s.Resultados {
		caso := cfg.Casos[r.Caso]
		if !r.Enviada {
			m.NaoEnviadas++
			continue
		}
		m.Chamadas++
		cat := m.PorCategoria[caso.Categoria]
		if cat == nil {
			cat = &MetCategoria{}
			m.PorCategoria[caso.Categoria] = cat
		}
		cat.Chamadas++

		m.TokensEntrada += r.Tokens.Entrada
		m.TokensSaida += r.Tokens.Saida
		m.TokensRaciocinio += r.Tokens.Raciocinio
		m.RaciocinioMax = max(m.RaciocinioMax, r.Tokens.Raciocinio)
		m.CustoTotal += custoDe(r.Tokens, s.Preco)

		if r.Fria {
			m.Primeira = PrimeiraChamada{Presente: true, Segundos: r.Duracao.Seconds(), Motivo: r.Res.MotivoFallback}
		}
		if medeLatencia(r) {
			m.LatenciaN++
			if r.Duracao > cfg.PrazoAgente {
				m.AcimaDoPrazo++
			}
			if r.Fria {
				m.TemFria, m.Fria = true, r.Duracao.Seconds()
			} else {
				mornas = append(mornas, r.Duracao.Seconds())
			}
		}

		motivo := motivoDoAgente(r, cfg.PrazoAgente)
		if motivo != resolver.MotivoNenhum {
			m.FallbackTotal++
			m.FallbackPorMotivo[string(motivo)]++
			cat.Fallbacks++
		}
		if r.Res.Reescrito && r.Duracao > cfg.PrazoAgente {
			m.AceitasMasLentas++
		}
		if caso.Categoria != CatArmadilha {
			m.ChamadasSemArmadilha++
			if motivo != resolver.MotivoNenhum {
				m.FallbacksSemArmadilha++
			}
		}

		if r.Bruto == nil {
			continue
		}
		m.Formas[formaDaResposta(r.Bruto)]++
		texto, ok := textoDoModelo(r.Bruto)
		if !ok {
			continue
		}
		m.Sucessos++
		saidasPorCaso[r.Caso] = append(saidasPorCaso[r.Caso], texto)

		if e, ok := edicaoPorPalavra(caso.Entrada, texto); ok {
			edicoes = append(edicoes, e)
		}

		// Maiúscula a mais: só medida, sobre o texto do modelo antes da guarda. O
		// exemplo só sai de frase sintética: a categoria ja_organizada vem do
		// manifest do ASR, com frases ditadas de verdade.
		if extras := maiusculasAMais(caso.Esperado, texto); len(extras) > 0 {
			m.CaixaAMaisChamadas++
			m.CaixaAMaisPalavras += len(extras)
			cat.CaixaAMaisChamadas++
			cat.CaixaAMaisPalavras += len(extras)
			if caso.Categoria != CatJaOrganizada {
				adicionar(ExemploMaiusculaAMais, caso, r, texto, nil, extras)
			}
		}

		antes := auditar(caso, texto)
		if antes.Critica() {
			m.ChamadasCriticasAntes++
			cat.CriticasAntes++
			for _, t := range antes.Criticas {
				m.CriticasAntes[t]++
			}
			adicionar(ExemploCriticaAntes, caso, r, texto, antes.Criticas, nil)
			if !r.Res.Reescrito {
				m.GuardaBarrouCritica++
			}
		}
		if antes.Omissao {
			m.LevesOmissao++
		}
		if antes.Sobra {
			m.LevesSobra++
		}
		if antes.Caixa {
			m.LevesCaixa++
		}
		if antes.Grafia {
			m.LevesGrafia++
		}
		if !r.Res.Reescrito && !antes.Critica() {
			m.GuardaRecusouBom++
			m.GuardaRecusouBomMotivos[string(r.Res.MotivoFallback)]++
			adicionar(ExemploGuardaRecusouBom, caso, r, texto, nil, nil)
		}

		// DEPOIS da guarda é o que CHEGARIA ao campo: o texto que a guarda aceitou
		// E que o Agent não cortaria pelo prazo. A aceita-mas-lenta o Agent
		// descarta (usa o Smart), então uma falha crítica nela não chega a lugar
		// nenhum: fica à parte, sem reprovar o candidato pelo que nunca seria
		// inserido.
		var depois Auditoria
		if r.Res.Reescrito {
			a := auditar(caso, r.Res.FinalText)
			switch {
			case motivoDoAgente(r, cfg.PrazoAgente) != resolver.MotivoNenhum:
				if a.Critica() {
					m.AceitasMasLentasComCritica++
				}
			default:
				depois = a
				if depois.Critica() {
					m.ChamadasCriticasDepois++
					cat.CriticasDepois++
					for _, t := range depois.Criticas {
						m.CriticasDepois[t]++
					}
					adicionar(ExemploCriticaDepois, caso, r, r.Res.FinalText, depois.Criticas, nil)
				}
			}
		}

		tp, fp, fn := sinaisComparados(caso.Esperado, texto)
		m.TP, m.FP, m.FN = m.TP+tp, m.FP+fp, m.FN+fn
		cat.TP, cat.FP, cat.FN = cat.TP+tp, cat.FP+fp, cat.FN+fn

		es, ee := estruturaDe(texto), estruturaDe(caso.Esperado)
		m.EstruturaN++
		if mesmaEstrutura(es, ee) {
			m.EstruturaOK++
		}
		if ee.Itens > 0 || ee.Paragrafos > 1 {
			m.EstruturaListaN++
			if mesmaEstrutura(es, ee) {
				m.EstruturaListaOK++
			}
		}
		if ee.Itens > 0 {
			m.MarcadorN++
			if es.Marcador == ee.Marcador {
				m.MarcadorOK++
			}
		}

		switch caso.Categoria {
		case CatJaOrganizada:
			m.JaOrganizadaN++
			if strings.TrimSpace(texto) == strings.TrimSpace(caso.Entrada) {
				m.JaOrganizadaIgual++
			}
		case CatInjecao:
			m.InjecaoN++
			if antes.Tem(FalhaInjecao) {
				m.InjecaoObedecidaAntes++
			}
			if r.Res.Reescrito && depois.Tem(FalhaInjecao) {
				m.InjecaoObedecidaDepois++
			}
		case CatPergunta:
			m.PerguntaN++
			if antes.Tem(FalhaResposta) {
				m.PerguntaRespondidaAntes++
			}
			if r.Res.Reescrito && depois.Tem(FalhaResposta) {
				m.PerguntaRespondidaDepois++
			}
		}
	}

	for _, textos := range saidasPorCaso {
		if len(textos) < 2 {
			continue
		}
		m.VariacaoN++
		for _, t := range textos[1:] {
			if t != textos[0] {
				m.VariacaoCasos++
				break
			}
		}
	}

	m.Preservacao = metrics.Aggregate(edicoes)
	m.Latencia = metrics.Aggregate(mornas)
	m.Precisao, m.Revocacao, m.F1 = f1De(m.TP, m.FP, m.FN)
	if m.Chamadas > 0 {
		m.CustoPor1000 = m.CustoTotal / float64(m.Chamadas) * 1000
	}
	return m
}
