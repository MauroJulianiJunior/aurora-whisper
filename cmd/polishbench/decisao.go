package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// Criterios são os limiares da regra de escolha (ADR-016, decisão 9; 19-eval-plan).
// Os valores do plano aprovado são os padrões das flags; a S3 os calibra com o
// que for medido.
type Criterios struct {
	// FallbackMax é a taxa máxima de fallback (0,05 = 5%), como o Agent a
	// veria e sem as frases-armadilha.
	FallbackMax float64
	// P95Max é o p95 máximo da latência morna (2,5 s).
	P95Max time.Duration
	// F1Min e EstruturaMin são o PISO de organização: F1 de pontuação (fora de
	// ja_organizada) e acerto de estrutura das frases com lista ou parágrafos
	// mínimos. Zero desliga: a regra aprovada (ADR-016, decisão 9) não tem
	// piso, e ligá-lo é decisão do mantenedor. Sem piso, quem só devolve a entrada
	// passa, e o relatório avisa (avisosDeOrganizacao).
	F1Min        float64
	EstruturaMin float64
}

// Veredito é o resultado da regra para um candidato.
type Veredito struct {
	Passou bool
	// Motivos são as razões da reprovação, para o relatório.
	Motivos []string
}

// avaliar aplica a regra: passa quem tem falha crítica DEPOIS da guarda igual
// a zero, fallback até FallbackMax e p95 até P95Max. Sem dado não passa:
// candidato sem chamada, ou sem latência medida, não tem como provar nada.
func avaliar(m Metricas, c Criterios) Veredito {
	var v Veredito
	reprova := func(f string, a ...any) { v.Motivos = append(v.Motivos, fmt.Sprintf(f, a...)) }

	if m.Aborto != "" && m.Aborto != AbortoInterrompido && m.Aborto != AbortoLimite {
		reprova("abortado: o provedor respondeu %s nas primeiras chamadas", m.Aborto)
	}
	if m.Chamadas == 0 {
		reprova("nenhuma chamada foi ao provedor")
	}
	if m.ChamadasCriticasDepois > 0 {
		reprova("%d chamada(s) com falha crítica DEPOIS da guarda (tem de ser 0)", m.ChamadasCriticasDepois)
	}
	if m.ChamadasSemArmadilha == 0 {
		if m.Chamadas > 0 {
			reprova("nenhuma chamada fora das frases-armadilha para medir o fallback")
		}
	} else if m.FallbackTaxa() > c.FallbackMax {
		reprova("fallback de %s (máximo %s)", pct(m.FallbackTaxa()), pct(c.FallbackMax))
	}
	if m.Latencia.N == 0 {
		reprova("nenhuma latência morna medida")
	} else if m.Latencia.P95 > c.P95Max.Seconds() {
		reprova("p95 de %s (máximo %s)", seg(m.Latencia.P95), seg(c.P95Max.Seconds()))
	}
	if c.F1Min > 0 && m.F1Organizando() < c.F1Min {
		reprova("F1 de pontuação, fora de já organizada, de %s (mínimo %s)", num(m.F1Organizando(), 3), num(c.F1Min, 3))
	}
	if c.EstruturaMin > 0 {
		if t, ok := m.EstruturaListaTaxa(); !ok {
			reprova("nenhuma frase com lista ou parágrafos para medir a estrutura")
		} else if t < c.EstruturaMin {
			reprova("acerto de estrutura das listas de %s (mínimo %s)", pct(t), pct(c.EstruturaMin))
		}
	}
	v.Passou = len(v.Motivos) == 0
	return v
}

// Decisao é a escolha da rodada.
type Decisao struct {
	// Incompleta diz que a rodada foi interrompida: não há decisão.
	Incompleta bool
	// Vereditos, na ordem dos candidatos.
	Vereditos []Veredito
	// Vencedores é o rótulo do vencedor de cada provedor que teve algum
	// candidato aprovado.
	Vencedores map[string]string
}

// escolher aplica a regra a todos os candidatos: entre os que passam, vence o
// mais barato (custo por 1.000 ditados); no empate, o de maior F1 de pontuação;
// no empate de novo, o rótulo em ordem alfabética (para o resultado ser
// estável). Sai um vencedor por provedor. O candidato de linha de base é
// julgado como os outros, mas nunca vence.
func escolher(ms []Metricas, c Criterios, incompleta bool) Decisao {
	d := Decisao{Incompleta: incompleta, Vencedores: map[string]string{}}
	for _, m := range ms {
		d.Vereditos = append(d.Vereditos, avaliar(m, c))
	}
	if incompleta {
		return d
	}
	melhor := map[string]int{}
	for i, m := range ms {
		if !d.Vereditos[i].Passou || m.Cand.LinhaDeBase {
			continue
		}
		j, tem := melhor[m.Cand.Provedor]
		if !tem || melhorQue(m, ms[j]) {
			melhor[m.Cand.Provedor] = i
		}
	}
	for provedor, i := range melhor {
		d.Vencedores[provedor] = ms[i].Cand.Rotulo()
	}
	return d
}

// melhorQue diz se a vence b: mais barato; no empate (diferença menor que
// um milionésimo de dólar por 1.000 ditados), maior F1; depois, o rótulo.
func melhorQue(a, b Metricas) bool {
	if dif := a.CustoPor1000 - b.CustoPor1000; math.Abs(dif) > 1e-6 {
		return dif < 0
	}
	if a.F1 != b.F1 {
		return a.F1 > b.F1
	}
	return a.Cand.Rotulo() < b.Cand.Rotulo()
}

// provedoresNaOrdem devolve os provedores dos vencedores em ordem estável.
func (d Decisao) provedoresNaOrdem() []string {
	var ps []string
	for p := range d.Vencedores {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

// nomeDoProvedor é o nome para quem lê o relatório.
func nomeDoProvedor(p string) string { return ia.Nome(p) }

// A regra aprovada não tem piso de organização: quem devolve a entrada sem
// organizar passa se a guarda, o fallback e o p95 estiverem em ordem, e pode
// até vencer por custar menos. Como decidir um piso é do mantenedor, o relatório ao
// menos avisa quando o vencedor organiza pouco: quase o mesmo que devolver a
// entrada, ou muito menos que outro candidato da rodada.
const (
	// margemDeF1 e margemDeEstrutura são a folga que separa "organiza" de
	// "quase não organiza"; a estrutura tem folga maior porque tem menos
	// frases (só as que têm lista ou parágrafos).
	margemDeF1        = 0.10
	margemDeEstrutura = 0.20
)

// avisosDeOrganizacao devolve um aviso por vencedor que organiza pouco. Sem
// vencedor (ou com a rodada incompleta) não há o que avisar.
func avisosDeOrganizacao(r Rodada) []string {
	if r.Incompleta {
		return nil
	}
	var avisos []string
	for _, p := range r.Decisao.provedoresNaOrdem() {
		var w *Metricas
		for i := range r.Metricas {
			if r.Metricas[i].Cand.Rotulo() == r.Decisao.Vencedores[p] {
				w = &r.Metricas[i]
			}
		}
		if w == nil || w.Sucessos == 0 {
			continue
		}
		var motivos []string
		f1 := w.F1Organizando()
		if f1 <= r.RefEco.F1+margemDeF1 {
			motivos = append(motivos, fmt.Sprintf("F1 de pontuação (fora de já organizada) de %s, perto dos %s de um modelo que só devolve a entrada", num(f1, 3), num(r.RefEco.F1, 3)))
		}
		if melhor, quem := melhorF1(r.Metricas); melhor-f1 > margemDeF1 {
			motivos = append(motivos, fmt.Sprintf("F1 de pontuação de %s, contra %s de `%s`", num(f1, 3), num(melhor, 3), quem))
		}
		if t, ok := w.EstruturaListaTaxa(); ok {
			if r.RefEco.EstruturaN > 0 && t <= r.RefEco.Estrutura+margemDeEstrutura {
				motivos = append(motivos, fmt.Sprintf("acerto de estrutura das listas de %s, perto dos %s de um modelo que só devolve a entrada", pct(t), pct(r.RefEco.Estrutura)))
			}
			if melhor, quem := melhorEstrutura(r.Metricas); melhor-t > margemDeEstrutura {
				motivos = append(motivos, fmt.Sprintf("acerto de estrutura das listas de %s, contra %s de `%s`", pct(t), pct(melhor), quem))
			}
		}
		if len(motivos) > 0 {
			avisos = append(avisos, fmt.Sprintf("`%s` venceu %s, mas organiza pouco: %s. Sem piso de organização a regra não o barra; leia os exemplos e a tabela por categoria antes de adotar (ou defina -f1-min e -estrutura-min).", w.Cand.Rotulo(), nomeDoProvedor(p), strings.Join(motivos, "; ")))
		}
	}
	return avisos
}

// melhorF1 é o maior F1 de pontuação (fora de já organizada) entre os
// candidatos que responderam, de linha de base ou não, reprovados ou não.
func melhorF1(ms []Metricas) (melhor float64, quem string) {
	for i := range ms {
		if ms[i].Sucessos == 0 {
			continue
		}
		if f := ms[i].F1Organizando(); quem == "" || f > melhor {
			melhor, quem = f, ms[i].Cand.Rotulo()
		}
	}
	return melhor, quem
}

// melhorEstrutura é o maior acerto de estrutura das listas entre os candidatos
// que o mediram.
func melhorEstrutura(ms []Metricas) (melhor float64, quem string) {
	for i := range ms {
		if t, ok := ms[i].EstruturaListaTaxa(); ok && (quem == "" || t > melhor) {
			melhor, quem = t, ms[i].Cand.Rotulo()
		}
	}
	return melhor, quem
}
