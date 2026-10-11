package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
)

// Candidato é um modelo a medir: provedor, nome do modelo e, só na OpenAI, o
// esforço de raciocínio. Os nomes dos modelos moram SÓ no comando (na flag
// -modelos e na tabela de preços abaixo), nunca no Agent (ADR-016, decisão 10).
type Candidato struct {
	Provedor string
	Modelo   string
	// Esforco vazio usa o padrão do cliente ("none"); ia.EsforcoOmitido tira o
	// campo reasoning do pedido.
	Esforco string
	// LinhaDeBase marca o candidato que é só referência (ADR-016, decisão 9: o
	// gpt-5-nano, que sai do ar em 11-dez-2026): é medido e a regra o julga,
	// mas ele nunca vence. Vem da flag -linha-de-base, não de -modelos.
	LinhaDeBase bool
}

// Rotulo é como o candidato aparece: "openai:gpt-5-nano@minimal".
func (c Candidato) Rotulo() string {
	r := c.Provedor + ":" + c.Modelo
	if c.Esforco != "" {
		r += "@" + c.Esforco
	}
	return r
}

// ChavePreco é a chave da tabela de preços: o esforço não muda o preço.
func (c Candidato) ChavePreco() string { return c.Provedor + ":" + c.Modelo }

// Chamada é o que o cliente do Agent recebe.
func (c Candidato) Chamada() ia.Chamada {
	return ia.Chamada{Provedor: c.Provedor, Modelo: c.Modelo, Esforco: c.Esforco}
}

// EsforcoEfetivo é o esforço que vai no pedido, para o relatório ("none" é o
// padrão do cliente; a Anthropic não tem esforço).
func (c Candidato) EsforcoEfetivo() string {
	switch {
	case c.Provedor != ia.OpenAI:
		return "não se aplica"
	case c.Esforco == "":
		return ia.EsforcoPadrao
	case c.Esforco == ia.EsforcoOmitido:
		return "campo reasoning omitido"
	}
	return c.Esforco
}

// analisarCandidatos lê a flag -modelos: "provedor:modelo[@esforco]" separados
// por vírgula.
func analisarCandidatos(s string) ([]Candidato, error) {
	var out []Candidato
	vistos := map[string]bool{}
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		provedor, resto, ok := strings.Cut(item, ":")
		if !ok || !ia.Valido(provedor) {
			return nil, fmt.Errorf("modelo %q: quero provedor:modelo com provedor %s ou %s", item, ia.OpenAI, ia.Anthropic)
		}
		modelo, esforco, temEsforco := strings.Cut(resto, "@")
		modelo = strings.TrimSpace(modelo)
		if modelo == "" {
			return nil, fmt.Errorf("modelo %q: falta o nome do modelo", item)
		}
		if temEsforco {
			if provedor != ia.OpenAI {
				return nil, fmt.Errorf("modelo %q: o esforço só vale na OpenAI", item)
			}
			if !esforcoValido(esforco) {
				return nil, fmt.Errorf("modelo %q: esforço %q inválido (letras minúsculas, como none, minimal ou low; %s tira o campo)", item, esforco, ia.EsforcoOmitido)
			}
		}
		c := Candidato{Provedor: provedor, Modelo: modelo, Esforco: esforco}
		if vistos[c.Rotulo()] {
			return nil, fmt.Errorf("modelo %q repetido", c.Rotulo())
		}
		vistos[c.Rotulo()] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nenhum modelo em -modelos")
	}
	return out, nil
}

// marcarLinhaDeBase lê a flag -linha-de-base (a mesma sintaxe de -modelos) e
// devolve os candidatos com a marca. Cada item tem de estar em -modelos: um
// rótulo que não casa com nenhum é quase sempre erro de digitação, e o modelo
// que era para ficar de fora venceria sem ninguém ver.
func marcarLinhaDeBase(s string, cands []Candidato) ([]Candidato, error) {
	out := append([]Candidato(nil), cands...)
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	base, err := analisarCandidatos(s)
	if err != nil {
		return nil, fmt.Errorf("-linha-de-base: %w", err)
	}
	for _, b := range base {
		achou := false
		for i := range out {
			if out[i].Rotulo() == b.Rotulo() {
				out[i].LinhaDeBase, achou = true, true
			}
		}
		if !achou {
			return nil, fmt.Errorf("-linha-de-base %s não está em -modelos (erro de digitação?)", b.Rotulo())
		}
	}
	return out, nil
}

func esforcoValido(e string) bool {
	if e == "" {
		return false
	}
	for _, r := range e {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// Preco é o preço por 1 milhão de tokens, em US$.
type Preco struct{ Entrada, Saida float64 }

// precosPadrao são os preços da pesquisa de 29-set-2026 (docs/adr/ADR-016,
// "Riscos"), só para os candidatos do plano. Qualquer outro modelo exige
// -preco. Os preços mudam: quem roda confere na página do provedor.
var precosPadrao = map[string]Preco{
	"openai:gpt-6-luna":          {Entrada: 0.10, Saida: 0.50},
	"openai:gpt-5-nano":          {Entrada: 0.05, Saida: 0.40},
	"anthropic:claude-haiku-4-5": {Entrada: 1.00, Saida: 5.00},
}

// analisarPrecos lê a flag -preco: "provedor:modelo=entrada/saida" separados
// por vírgula, em US$ por 1 milhão de tokens.
func analisarPrecos(s string) (map[string]Preco, error) {
	out := map[string]Preco{}
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		chave, valores, ok := strings.Cut(item, "=")
		provedor, modelo, ok2 := strings.Cut(chave, ":")
		ent, sai, ok3 := strings.Cut(valores, "/")
		if !ok || !ok2 || !ok3 || !ia.Valido(provedor) || modelo == "" {
			return nil, fmt.Errorf("preço %q: quero provedor:modelo=entrada/saída (US$ por 1 milhão de tokens)", item)
		}
		e, err1 := strconv.ParseFloat(strings.TrimSpace(ent), 64)
		sd, err2 := strconv.ParseFloat(strings.TrimSpace(sai), 64)
		if err1 != nil || err2 != nil || !precoSensato(e) || !precoSensato(sd) {
			return nil, fmt.Errorf("preço %q: os valores têm de ser números de 0 a 10000", item)
		}
		out[provedor+":"+modelo] = Preco{Entrada: e, Saida: sd}
	}
	return out, nil
}

func precoSensato(v float64) bool { return !math.IsNaN(v) && v >= 0 && v <= 10000 }

// precosDos junta os preços da flag aos padrões (a flag vence) e exige um
// preço para cada candidato: um custo calado em zero decidiria "o mais
// barato" errado.
func precosDos(cands []Candidato, flag map[string]Preco) (map[string]Preco, error) {
	out := map[string]Preco{}
	// Um -preco de modelo que não está em -modelos é quase sempre erro de
	// digitação, e o candidato ficaria com o preço padrão sem ninguém ver.
	for chave := range flag {
		achou := false
		for _, c := range cands {
			achou = achou || c.ChavePreco() == chave
		}
		if !achou {
			return nil, fmt.Errorf("-preco de %s, que não está em -modelos (erro de digitação?)", chave)
		}
	}
	for _, c := range cands {
		if p, ok := flag[c.ChavePreco()]; ok {
			out[c.ChavePreco()] = p
		} else if p, ok := precosPadrao[c.ChavePreco()]; ok {
			out[c.ChavePreco()] = p
		} else {
			return nil, fmt.Errorf("sem preço para %s: passe -preco %s=entrada/saída (US$ por 1 milhão de tokens)", c.ChavePreco(), c.ChavePreco())
		}
	}
	return out, nil
}

// custoDe é o custo em US$ de uma chamada. Ignora o desconto de cache: é um
// limite superior.
func custoDe(t ia.Tokens, p Preco) float64 {
	return float64(t.Entrada)*p.Entrada/1e6 + float64(t.Saida)*p.Saida/1e6
}
