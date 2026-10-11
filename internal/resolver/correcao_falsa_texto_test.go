package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/entitygate"
)

// A medição do portão de correção falsa SEM o ASR: o Smart roda sobre o texto
// das referências do dataset (testdata/asr-dataset) e das frases do Polish
// (testdata/polish-dataset), que já estão certos, e em variantes de caixa
// deles — tudo minúsculo, minúsculo com a inicial da frase maiúscula (como o
// Whisper escreve) e tudo maiúsculo (o ASR devolveu "ESOCIAL" no teste do
// Mauro de 01-out-2026).
//
// Três medidas, porque uma não vê a outra (a terceira, a da autocorreção, vem
// no fim da lista):
//
//   - a do portão (entitygate.Medir, a mesma de cmd/entitybench): entidade e
//     WER, sem caixa. Toda referência já está certa, então toda entidade que
//     o Smart estraga é correção falsa.
//   - a de caixa, palavra por palavra e COM caixa: cada palavra que o Smart
//     trocou só na caixa tem de sair igual à da referência. A medida do
//     portão é insensível a maiúscula e não enxerga "das" virando "DAS".
//   - a da autocorreção: em texto certo nenhuma pode disparar, e nas falas em
//     que a pessoa se corrigiu ela tem de continuar chegando à intenção (piso
//     medido antes da regra da negação, ver o teste). É o que impede uma regra
//     de falha fechada, como a da negação na oração, de desligar a correção
//     de verdade sem que nada acuse.
//
// Limites desta medida (leia antes de confiar em "0 trocas de caixa erradas"):
//
//   - as referências trazem o termo sempre na caixa canônica e não têm URL,
//     caminho, e-mail nem comando. Para o termo de grafia própria, "caixa
//     falsa = 0" é quase garantido por construção, e a medida não vê a
//     reescrita indevida num contexto onde a minúscula é a certa
//     ("npm install typescript", "gov.br/esocial"). Quem cobre esse contexto é
//     TestSmart_GrafiaPropriaNaoMexeEmCodigoNemEndereco, com casos negativos
//     explícitos;
//   - a contagem de caixa pula a amostra em que a autocorreção mudou o número
//     de palavras ("sem alinhamento" no relatório, 12 no corte do manifest):
//     nela só vale a medida do portão, que é insensível a caixa.

type amostraDeTexto struct {
	id, categoria, referencia, intencao string
	entidades                           []string
}

func amostrasDoManifest(t *testing.T) []amostraDeTexto {
	t.Helper()
	b, err := os.ReadFile(arquivoManifestASR)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Samples []struct {
			ID        string   `json:"id"`
			Category  string   `json:"category"`
			Reference string   `json:"reference"`
			Intended  string   `json:"intended"`
			Entities  []string `json:"entities"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var out []amostraDeTexto
	for _, s := range m.Samples {
		out = append(out, amostraDeTexto{s.ID, s.Category, s.Reference, s.Intended, s.Entities})
	}
	return out
}

func amostrasDasFrasesDoPolish(t *testing.T) []amostraDeTexto {
	t.Helper()
	b, err := os.ReadFile(arquivoFrasesPolish)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Frases []struct {
			ID      string   `json:"id"`
			Entrada string   `json:"entrada"`
			Termos  []string `json:"termos"`
		} `json:"frases"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	var out []amostraDeTexto
	for _, fr := range f.Frases {
		out = append(out, amostraDeTexto{id: "polish/" + fr.ID, referencia: fr.Entrada, entidades: fr.Termos})
	}
	return out
}

// variantesDeCaixa são as transformações que imitam o ASR errando só a caixa.
var variantesDeCaixa = []struct {
	nome string
	f    func(string) string
}{
	{"como está", func(s string) string { return s }},
	{"minúsculas", strings.ToLower},
	{"minúsculas, inicial da frase maiúscula", func(s string) string {
		s = strings.ToLower(s)
		r, n := utf8.DecodeRuneInString(s)
		return string(unicode.ToUpper(r)) + s[n:]
	}},
	{"MAIÚSCULAS", strings.ToUpper},
}

// dominioDaCategoria é o "auto" de cmd/entitybench: o domínio que o app em
// foco teria. Vazio é desconhecido, o caso seguro.
func dominioDaCategoria(categoria string) *string {
	var d string
	switch categoria {
	case "contabilidade":
		d = "contabil"
	case "desenvolvimento", "code-switching":
		d = "dev"
	default:
		return nil
	}
	return &d
}

type medidaDeTexto struct {
	portao                entitygate.Resultado
	amostras, alteradas   int
	trocasDeCaixa         int      // palavras que o Smart trocou só na caixa
	caixaFalsa            []string // as que saíram diferentes da referência
	semAlinhamento        int      // a contagem de palavras mudou (autocorreção)
	trocasDeCaixaPorTermo map[string]int

	// A autocorreção, nos dois sentidos. Amostra SEM intenção é texto que já
	// está certo, e uma autocorreção nele é falsa; amostra COM intenção é uma
	// fala em que a pessoa se corrigiu, e a autocorreção tem de acontecer e
	// chegar nela (EqualFold: o Smart não devolve a caixa da fala).
	autocorrecoesFalsas []string // as que dispararam em texto certo
	comIntencao         int      // amostras com intenção (fala com autocorreção)
	autocorrigidas      int      // as que dispararam
	naIntencao          int      // as que chegaram à intenção
	foraDaIntencao      []string // as que não chegaram
}

func medirEmTexto(t *testing.T, amostras []amostraDeTexto, comDominio bool) medidaDeTexto {
	t.Helper()
	sr, user := smartDaSementeParaTeste(t, sementeVersionada(t))
	m := medidaDeTexto{trocasDeCaixaPorTermo: map[string]int{}}
	var paraOPortao []entitygate.Amostra
	for _, a := range amostras {
		var dom *string
		if comDominio {
			dom = dominioDaCategoria(a.categoria)
		}
		for _, v := range variantesDeCaixa {
			bruto := v.f(a.referencia)
			out, err := sr.Resolve(context.Background(), ResolveRequest{
				UserID: user, Domain: dom,
				ASRResult: asr.ASRResult{Text: bruto, Language: "pt"},
			})
			if err != nil {
				t.Fatalf("Resolve(%q): %v", bruto, err)
			}
			m.amostras++
			if out.FinalText != bruto {
				m.alteradas++
			}
			disparou := false
			for _, r := range out.AppliedReplacements {
				disparou = disparou || r.Reason == ReasonSelfCorrection
			}
			if a.intencao == "" {
				if disparou {
					m.autocorrecoesFalsas = append(m.autocorrecoesFalsas,
						fmt.Sprintf("%s (%s): %q -> %q", a.id, v.nome, bruto, out.FinalText))
				}
			} else {
				m.comIntencao++
				if disparou {
					m.autocorrigidas++
				}
				if strings.EqualFold(out.FinalText, a.intencao) {
					m.naIntencao++
				} else {
					m.foraDaIntencao = append(m.foraDaIntencao,
						fmt.Sprintf("%s (%s): %q -> %q, a intenção é %q", a.id, v.nome, bruto, out.FinalText, a.intencao))
				}
			}
			paraOPortao = append(paraOPortao, entitygate.Amostra{
				ID: a.id + " (" + v.nome + ")", Entidades: a.entidades,
				Bruto: bruto, Resolvido: out.FinalText,
				Referencia: a.referencia, Intencao: a.intencao,
			})

			entra, sai, ref := strings.Fields(bruto), strings.Fields(out.FinalText), strings.Fields(a.referencia)
			if len(entra) != len(sai) {
				m.semAlinhamento++
				continue
			}
			for k := range entra {
				if entra[k] == sai[k] || !strings.EqualFold(entra[k], sai[k]) {
					continue
				}
				m.trocasDeCaixa++
				m.trocasDeCaixaPorTermo[strings.Trim(sai[k], ".,;:!?")]++
				if sai[k] != ref[k] {
					m.caixaFalsa = append(m.caixaFalsa, fmt.Sprintf("%s (%s): %q -> %q, a referência é %q",
						a.id, v.nome, entra[k], sai[k], ref[k]))
				}
			}
		}
	}
	m.portao = entitygate.Medir(paraOPortao)
	return m
}

func (m medidaDeTexto) String() string {
	return fmt.Sprintf("amostras=%d alteradas=%d | portão: entidades certas antes %d/%d, depois %d/%d, "+
		"regressões=%d, correção falsa=%.2f%%, amostras afastadas da intenção=%d | "+
		"caixa: trocas=%d %v, falsas=%d, sem alinhamento=%d | "+
		"autocorreção: falsas=%d, falas com intenção=%d, disparou=%d, chegou à intenção=%d",
		m.amostras, m.alteradas,
		m.portao.Bruto.Correct, m.portao.Bruto.Total, m.portao.Resolvido.Correct, m.portao.Resolvido.Total,
		m.portao.Regressoes, 100*m.portao.TaxaCorrecaoFalsa(), len(m.portao.PiorouTexto),
		m.trocasDeCaixa, m.trocasDeCaixaPorTermo, len(m.caixaFalsa), m.semAlinhamento,
		len(m.autocorrecoesFalsas), m.comIntencao, m.autocorrigidas, m.naIntencao)
}

// amostrasComNegacaoNaOracao são falas em que um marcador de autocorreção vem
// depois de uma oração que JÁ NEGA (decisão do Mauro de 01-out-2026): o texto
// tem de sair como foi dito, então não há intenção diferente da referência.
// Começou no "não" isolado e vale para todos os marcadores; as falas com os
// outros marcadores são o custo aceito (a correção verdadeira, com a oração
// corrigida negando, também sai como foi dita). Nenhuma delas está no dataset do
// ASR — esta medida é o único lugar onde o portão as vê.
func amostrasComNegacaoNaOracao() []amostraDeTexto {
	var out []amostraDeTexto
	for i, fala := range []string{
		"Eu não sei, não, cara.",
		"Eu não vou, não, tá?",
		"Não gostei, não, sério.",
		"Claro que não, não, isso nunca.",
		"Eu acho que não, não, deixa pra lá.",
		"Eu não vou na segunda, não, vou na terça.", // o custo aceito
		"Nem eu vou na segunda, não, vou na terça.",
		"Ninguém sabe, não, a gente combinou outra coisa.",
		"I don't know, no, man.",
		// Os outros marcadores, todos com o mesmo custo aceito.
		"Eu não vou na segunda, na verdade vou na terça.",
		"Eu não vou na segunda, não, na verdade vou na terça.",
		"Eu não vou na segunda, corrigindo, vou na terça.",
		"Eu não vou na segunda, quero dizer, vou na terça.",
		"Eu não vou na segunda, melhor terça.",
		"Eu não vou na segunda, quer dizer, vou na terça.",
		"Eu não vou na segunda, desculpa, vou na terça.",
		"Eu não vou na segunda, desculpe, vou na terça.",
		"I don't want Monday, actually Tuesday.",
		"I don't want Monday, I mean Tuesday.",
		"I don't want Monday, rather Tuesday.",
	} {
		out = append(out, amostraDeTexto{id: fmt.Sprintf("negacao-%02d", i+1), referencia: fala})
	}
	return out
}

// amostrasDeAutocorrecaoSemNegacao são autocorreções de verdade, com o "não" ou
// um dos outros marcadores depois de uma oração SEM negação, e a intenção que a
// pessoa tinha. A regra da negação não pode desligá-las. Uma delas ("no" na
// oração anterior, que em português é a contração de "em + o") e outra com
// "nota" (que contém "not") existem para a lista de negações não casar por
// trecho, e uma com a reticência colando o "não" na oração anterior (como o ASR
// escreve corr-01) existe para esse "não", que é o marcador, não contar como
// negação.
func amostrasDeAutocorrecaoSemNegacao() []amostraDeTexto {
	return []amostraDeTexto{
		{id: "real-01", referencia: "Manda para a Rosângela, não, manda para o Thiago.", intencao: "Manda para o Thiago."},
		{id: "real-02", referencia: "São três mil reais, não, três mil e quinhentos.", intencao: "São três mil e quinhentos reais."},
		{id: "real-03", referencia: "Atenção: o prazo é dia dez, não, dia doze.", intencao: "Atenção: o prazo é dia doze."},
		{id: "real-04", referencia: "Sobe no Supabase, não, no PostgreSQL.", intencao: "Sobe no PostgreSQL.", entidades: []string{"PostgreSQL"}},
		{id: "real-05", referencia: "Manda a nota fiscal, não, a nota de débito.", intencao: "Manda a nota de débito."},
		{id: "real-06", referencia: "Abre o cinema, não, o teatro.", intencao: "Abre o teatro."},
		// Os outros marcadores.
		{id: "real-07", referencia: "Manda para a Rosângela... não, na verdade manda para o Thiago.", intencao: "Manda para o Thiago."},
		{id: "real-08", referencia: "O vencimento é dia dez, desculpa, dia doze.", intencao: "O vencimento é dia doze."},
		{id: "real-09", referencia: "Sobe no Supabase, quer dizer, no PostgreSQL.", intencao: "Sobe no PostgreSQL.", entidades: []string{"PostgreSQL"}},
		{id: "real-10", referencia: "Manda para a Rosângela, não, na verdade manda para o Thiago.", intencao: "Manda para o Thiago."},
		{id: "real-11", referencia: "Eu vou na segunda, corrigindo, vou na terça.", intencao: "Eu vou na terça."},
		{id: "real-12", referencia: "I will go Monday, actually Tuesday.", intencao: "Tuesday."},
	}
}

// TestSmart_CorrecaoFalsaEmTextoDoDataset é o portão de correção falsa
// (≤1%, docs/ai-spec/02-prd.md) medido sobre texto que já está certo. Sobre
// texto certo, a meta de verdade é zero: toda troca é um estrago.
//
// Mede também a autocorreção, nos dois sentidos. Em texto certo nenhuma pode
// disparar. Nas falas em que a pessoa se corrigiu, a autocorreção tem de
// continuar chegando à intenção: o piso abaixo é o que o dataset dava antes da
// regra da negação (corr-01 a corr-03 nas 4 caixas; corr-04 como o manifest a
// escreve, "reais... não,", nunca disparou, porque a reticência cola o "não" na
// oração anterior). Dois cortes são montados aqui, e não vêm do dataset: as
// falas com negação na oração (com qualquer marcador), que têm de sair intactas,
// e as autocorreções reais sem negação (também com qualquer marcador), que têm
// de sair todas corrigidas.
func TestSmart_CorrecaoFalsaEmTextoDoDataset(t *testing.T) {
	cortes := []struct {
		nome       string
		amostras   []amostraDeTexto
		comDominio bool
		// naIntencaoPiso: quantas falas com intenção a autocorreção tem de levar
		// até ela, no mínimo.
		naIntencaoPiso int
		// tudoNaIntencao: toda fala com intenção dispara a autocorreção e chega
		// nela.
		tudoNaIntencao bool
		// intacto: nenhuma amostra pode mudar.
		intacto bool
	}{
		{"manifest, domínio desconhecido", amostrasDoManifest(t), false, 12, false, false},
		{"manifest, domínio da categoria", amostrasDoManifest(t), true, 12, false, false},
		{"frases do Polish, domínio desconhecido", amostrasDasFrasesDoPolish(t), false, 0, false, false},
		{"falas com negação na oração, sai como foi dito", amostrasComNegacaoNaOracao(), false, 0, false, true},
		{"autocorreções reais sem negação na oração", amostrasDeAutocorrecaoSemNegacao(), false, 0, true, false},
	}
	for _, c := range cortes {
		t.Run(c.nome, func(t *testing.T) {
			m := medirEmTexto(t, c.amostras, c.comDominio)
			t.Log(m)
			for _, f := range m.autocorrecoesFalsas {
				t.Errorf("autocorreção em texto que já estava certo: %s", f)
			}
			if m.naIntencao < c.naIntencaoPiso {
				t.Errorf("a autocorreção levou %d fala(s) à intenção, o piso é %d: %v",
					m.naIntencao, c.naIntencaoPiso, m.foraDaIntencao)
			}
			if c.tudoNaIntencao && (m.autocorrigidas != m.comIntencao || m.naIntencao != m.comIntencao) {
				t.Errorf("%d fala(s) com intenção: disparou em %d, chegou à intenção em %d; fora dela: %v",
					m.comIntencao, m.autocorrigidas, m.naIntencao, m.foraDaIntencao)
			}
			if c.intacto && m.alteradas != 0 {
				t.Errorf("%d de %d amostra(s) mudaram, e todas deviam sair como foram ditas", m.alteradas, m.amostras)
			}
			if m.portao.Regressoes != 0 || m.portao.TaxaCorrecaoFalsa() > entitygate.MetaCorrecaoFalsa {
				t.Errorf("o Smart estragou %d entidade(s) que já estavam certas (meta: <= %.0f%%)",
					m.portao.Regressoes, 100*entitygate.MetaCorrecaoFalsa)
				for _, o := range m.portao.Ocorrencias {
					if o.Status == entitygate.Regrediu {
						t.Logf("  %s: %q: %q -> %q", o.AmostraID, o.Entidade, o.TextoBruto, o.TextoResolvido)
					}
				}
			}
			if !m.portao.NenhumaAmostraAfastada() {
				for _, p := range m.portao.PiorouTexto {
					t.Errorf("afastou da intenção: %s: %q -> %q (alvo %q)", p.ID, p.Bruto, p.Resolvido, p.Alvo)
				}
			}
			for _, f := range m.caixaFalsa {
				t.Errorf("caixa trocada errado: %s", f)
			}
		})
	}
}
