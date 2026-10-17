package main

// O diagnóstico do modo -historico: para cada resposta que uma guarda
// recusou, a diferença PALAVRA A PALAVRA entre o texto enviado (o do Smart) e
// o que o modelo devolveu, com uma categoria para cada diferença. É para
// leitura humana, no terminal: a comparação aqui não é a da guarda (essa
// continua em internal/resolver), só mostra o que mudou e de que tipo.

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// As categorias de uma diferença.
const (
	DifHesitacao  = "hesitação ou pontuação falada (a guarda permite)"
	DifRepeticao  = "repetição limpa"
	DifFiller     = "palavra de fala apagada (né, tipo, então...)"
	DifApagada    = "outra palavra apagada"
	DifAcento     = "acento trocado"
	DifContracao  = "contração trocada (pra/para, tá/está...)"
	DifJuncao     = "palavra juntada, separada ou com hífen"
	DifTrocada    = "palavra trocada"
	DifAcrescida  = "palavra acrescentada"
	contextoDaDif = 3
)

// Diferenca é um trecho em que a saída difere da entrada.
type Diferenca struct {
	Antes, Depois []string // contexto da entrada, até 3 palavras de cada lado
	Saiu, Entrou  []string // o que saiu da entrada e o que entrou na saída
	Categoria     string
}

// diferencas alinha as duas sequências pela maior subsequência comum e
// devolve os trechos diferentes, cada um com a categoria.
func diferencas(entrada, saida string) []Diferenca {
	a, b := palavrasDe(entrada), palavrasDe(saida)
	n, m := len(a), len(b)
	// lcs[i][j]: maior subsequência comum de a[i:] e b[j:].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i].Norm == b[j].Norm {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []Diferenca
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && a[i].Norm == b[j].Norm {
			i, j = i+1, j+1
			continue
		}
		ini := i
		var saiu, entrou []string
		for (i < n || j < m) && !(i < n && j < m && a[i].Norm == b[j].Norm) {
			if j >= m || (i < n && lcs[i+1][j] >= lcs[i][j+1]) {
				saiu = append(saiu, a[i].Bruta)
				i++
			} else {
				entrou = append(entrou, b[j].Bruta)
				j++
			}
		}
		d := Diferenca{Saiu: saiu, Entrou: entrou}
		for k := max(0, ini-contextoDaDif); k < ini; k++ {
			d.Antes = append(d.Antes, a[k].Bruta)
		}
		for k := i; k < n && k < i+contextoDaDif; k++ {
			d.Depois = append(d.Depois, a[k].Bruta)
		}
		d.Categoria = categoria(a, ini, saiu, entrou)
		out = append(out, d)
	}
	return out
}

var (
	hesitacoes = conjunto("ahn ahnn ahnnn ãhn ãh hum humm hmm hmmm uhm uhmm éé ééé " +
		"vírgula ponto final dois pontos interrogação exclamação nova novo linha parágrafo de e")
	fillers    = conjunto("né tipo então aí assim sabe bom olha enfim eh ah hã é tá ok")
	contracoes = map[string]string{
		"pra": "para", "pro": "para", "pras": "para", "pros": "para", "tá": "está", "tô": "estou", "tava": "estava",
		"tavam": "estavam", "tão": "estão", "cê": "você", "ocê": "você", "num": "não", "né": "não",
	}
)

func formas(ps []string) []string {
	var out []string
	for _, p := range palavrasDe(strings.Join(ps, " ")) {
		out = append(out, p.Norm)
	}
	return out
}

func todasEm(ps []string, m map[string]bool) bool {
	f := formas(ps)
	if len(f) == 0 {
		return false
	}
	for _, x := range f {
		if !m[x] {
			return false
		}
	}
	return true
}

// tiraAcento troca as letras acentuadas do português pela letra sem acento
// (as formas já vêm em minúscula).
var tiraAcento = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "é", "e", "ê", "e", "è", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ç", "c",
)

func semAcento(s string) string { return tiraAcento.Replace(s) }

// categoria classifica um trecho. a e ini dão o contexto da entrada (para a
// repetição: o que saiu repete o que vinha logo antes).
func categoria(a []palavra, ini int, saiu, entrou []string) string {
	fs, fe := formas(saiu), formas(entrou)
	switch {
	case len(fe) == 0:
		if todasEm(saiu, hesitacoes) && !todasEm(saiu, conjunto("de e dois nova novo final")) {
			return DifHesitacao
		}
		if k := len(fs); ini-k >= 0 {
			igual := true
			for x := 0; x < k; x++ {
				if a[ini-k+x].Norm != fs[x] {
					igual = false
					break
				}
			}
			if igual {
				return DifRepeticao
			}
		}
		if todasEm(saiu, fillers) {
			return DifFiller
		}
		return DifApagada
	case len(fs) == 0:
		return DifAcrescida
	}
	if strings.Join(fs, "") == strings.Join(fe, "") ||
		strings.ReplaceAll(strings.Join(fs, ""), "-", "") == strings.ReplaceAll(strings.Join(fe, ""), "-", "") {
		return DifJuncao
	}
	if len(fs) == len(fe) {
		acento, contracao := true, true
		for x := range fs {
			if semAcento(fs[x]) != semAcento(fe[x]) {
				acento = false
			}
			if fs[x] != fe[x] && contracoes[fs[x]] != fe[x] && contracoes[fe[x]] != fs[x] {
				contracao = false
			}
		}
		if acento {
			return DifAcento
		}
		if contracao {
			return DifContracao
		}
	}
	return DifTrocada
}

// textoDaSaida tira o final_text do JSON que o modelo devolveu ({"final_text":
// ...}); vazio se não der para ler.
func textoDaSaida(bruto []byte) string {
	var v struct {
		FinalText string `json:"final_text"`
	}
	if json.Unmarshal(bruto, &v) != nil {
		return ""
	}
	return v.FinalText
}

func (d Diferenca) linha() string {
	var b strings.Builder
	if len(d.Antes) > 0 {
		b.WriteString("… " + strings.Join(d.Antes, " ") + " ")
	}
	switch {
	case len(d.Entrou) == 0:
		b.WriteString("[- " + strings.Join(d.Saiu, " ") + "]")
	case len(d.Saiu) == 0:
		b.WriteString("[+ " + strings.Join(d.Entrou, " ") + "]")
	default:
		b.WriteString("[" + strings.Join(d.Saiu, " ") + " → " + strings.Join(d.Entrou, " ") + "]")
	}
	if len(d.Depois) > 0 {
		b.WriteString(" " + strings.Join(d.Depois, " ") + " …")
	}
	return b.String()
}

// escreverDiagnostico escreve, para cada ditado, o desfecho de cada rodada e,
// nas respostas recusadas pela guarda de palavras, as diferenças. No fim, a
// contagem das categorias e dos desfechos por tamanho.
func escreverDiagnostico(w io.Writer, ditados []Ditado, casos []Caso, saidas []SaidaCandidato) {
	contagem := map[string]int{}
	porFaixa := map[string]map[string]int{}
	for _, s := range saidas {
		fmt.Fprintf(w, "\n=== %s ===\n", s.Cand.Rotulo())
		porCaso := map[int][]Resultado{}
		for _, r := range s.Resultados {
			porCaso[r.Caso] = append(porCaso[r.Caso], r)
		}
		for i, c := range casos {
			d := ditados[i]
			n := len([]rune(c.Entrada))
			fmt.Fprintf(w, "\n%s · %d caracteres · %s · %s\n", c.ID, n, origemLegivel(d.Origem), d.CriadoEm)
			faixa := faixaDeTamanho(n)
			if porFaixa[faixa] == nil {
				porFaixa[faixa] = map[string]int{}
			}
			rs := porCaso[i]
			sort.Slice(rs, func(x, y int) bool { return rs[x].Rodada < rs[y].Rodada })
			for _, r := range rs {
				desfecho := "aceito"
				if !r.Res.Reescrito {
					desfecho = string(r.Res.MotivoFallback)
				}
				porFaixa[faixa][desfecho]++
				fmt.Fprintf(w, "  rodada %d: %s\n", r.Rodada, desfecho)
				if r.Res.MotivoFallback != resolver.MotivoPalavras && r.Res.MotivoFallback != resolver.MotivoTrechoNovo {
					continue
				}
				saida := textoDaSaida(r.Bruto)
				if saida == "" {
					continue
				}
				for _, dif := range diferencas(c.Entrada, saida) {
					contagem[dif.Categoria]++
					fmt.Fprintf(w, "    %s\n      (%s)\n", dif.linha(), dif.Categoria)
				}
			}
		}
	}
	fmt.Fprintln(w, "\n=== resumo ===")
	fmt.Fprintln(w, "desfechos por tamanho do ditado:")
	for _, faixa := range []string{"menos de 600", "600 a 999", "1000 ou mais"} {
		if m := porFaixa[faixa]; m != nil {
			fmt.Fprintf(w, "  %-13s %s\n", faixa, contagemEmLinha(m))
		}
	}
	if len(contagem) == 0 {
		fmt.Fprintln(w, "nenhuma resposta recusada pela guarda de palavras.")
		return
	}
	fmt.Fprintln(w, "o que mudou nas respostas recusadas pela guarda de palavras:")
	type item struct {
		cat string
		n   int
	}
	var itens []item
	for c, n := range contagem {
		itens = append(itens, item{c, n})
	}
	sort.Slice(itens, func(x, y int) bool {
		if itens[x].n != itens[y].n {
			return itens[x].n > itens[y].n
		}
		return itens[x].cat < itens[y].cat
	})
	for _, it := range itens {
		fmt.Fprintf(w, "  %3d  %s\n", it.n, it.cat)
	}
}

func faixaDeTamanho(n int) string {
	switch {
	case n < 600:
		return "menos de 600"
	case n < 1000:
		return "600 a 999"
	}
	return "1000 ou mais"
}

func contagemEmLinha(m map[string]int) string {
	var chaves []string
	for k := range m {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	var partes []string
	for _, k := range chaves {
		partes = append(partes, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(partes, ", ")
}
