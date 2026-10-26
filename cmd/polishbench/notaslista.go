package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/metrics"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/polish"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// O modo -notas-lista (ADR-022, fase N5): a categoria notas-lista, que mede a
// separação em itens das notas (prompt notas-lista@1 e as guardas de
// resolver.SepararItens) pelo MESMO caminho da API local do Agent, com a chave
// que o mantenedor cola. Fica fora da rodada do Polish de propósito: outro
// prompt, outra saída ({"itens": [...]}) e outras métricas; e o hash do dataset
// do Polish, que os relatórios antigos citam, não muda.
//
//	go run ./cmd/polishbench -gastar -notas-lista -rodadas 3 -modelos openai:gpt-6-luna
//
// As métricas: quantas vezes a IA valeu (como "ia"), quantas a regra assumiu
// e quantas nada separou; as recusas da guarda por motivo (e o "um_item", que é
// o modelo dizendo que o trecho não é lista); os erros do provedor; o F1 dos
// itens (o que a pessoa vê, contra o esperado, item a item e exato); o acerto
// da lista inteira por tipo de trecho; a latência; e o item que não é pedaço
// do trecho, que tem de ser zero (a guarda recorta o texto do próprio trecho).

const (
	// CatNotasLista é a categoria dos casos de notas-lista.json.
	CatNotasLista = "notas-lista"
	// padraoNotasLista é o dataset, a partir da raiz do repositório.
	padraoNotasLista = "testdata/polish-dataset/notas-lista.json"

	// Estimativa de tokens de uma chamada, só para o aviso de custo: o prompt
	// tem cerca de 700 tokens, mais o trecho; a saída é a lista, dezenas.
	tokensEntradaListaEstimados = 900
	tokensSaidaListaEstimados   = 60

	// maxErradasPorModelo limita os exemplos do relatório.
	maxErradasPorModelo = 15
)

// comandoNotasLista é o comando que o mantenedor roda (vai no -help, no
// relatório e no roteiro manual da N5).
const comandoNotasLista = "go run ./cmd/polishbench -gastar -notas-lista -rodadas 3 -modelos openai:gpt-6-luna"

// tiposDaLista são os tipos de trecho, na ordem do relatório.
var tiposDaLista = []string{"virgula", "sem_virgula", "numero", "unidade", "sigla", "um_item", "injecao"}

// motivosDaGuardaDaLista são os motivos em que a resposta CHEGOU e a guarda a
// recusou (os outros são do provedor ou da rede).
var motivosDaGuardaDaLista = []resolver.MotivoDaLista{
	resolver.MotivoDaLista(resolver.MotivoJSON), resolver.ListaUmItem, resolver.ListaItens,
	resolver.MotivoDaLista(resolver.MotivoFormato), resolver.MotivoDaLista(resolver.MotivoNumeros),
	resolver.MotivoDaLista(resolver.MotivoPalavras), resolver.MotivoDaLista(resolver.MotivoCaixa),
	resolver.MotivoDaLista(resolver.MotivoSiglasTermos),
}

// CasoLista é um trecho do dataset.
type CasoLista struct {
	ID        string `json:"id"`
	Categoria string `json:"categoria"`
	Tipo      string `json:"tipo"`
	Trecho    string `json:"trecho"`
	// Esperado são os itens que o painel mostraria. Um item só: o trecho não é
	// lista e fica como está.
	Esperado []string `json:"esperado"`
}

type arquivoDaLista struct {
	Versao int         `json:"versao"`
	Aviso  string      `json:"aviso"`
	Casos  []CasoLista `json:"casos"`
}

// carregarCasosDaLista lê notas-lista.json; campo desconhecido é erro.
func carregarCasosDaLista(dados []byte) ([]CasoLista, error) {
	var a arquivoDaLista
	dec := json.NewDecoder(bytes.NewReader(dados))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("notas-lista.json: %w", err)
	}
	if a.Versao != 1 {
		return nil, fmt.Errorf("notas-lista.json: versao %d desconhecida (quero 1)", a.Versao)
	}
	return a.Casos, nil
}

// validarCasosDaLista confere o dataset: ids únicos, categoria e tipo
// conhecidos, trecho de uma linha, e o esperado coerente com a guarda (o de um
// item é o trecho inteiro; o de dois ou mais é o que GuardarItens devolve
// quando o modelo corta exatamente ali). Assim o benchmark nunca cobra do
// modelo uma lista que a guarda recusaria.
func validarCasosDaLista(casos []CasoLista) error {
	if len(casos) == 0 {
		return errors.New("notas-lista: dataset vazio")
	}
	vistos := map[string]bool{}
	for _, c := range casos {
		switch {
		case strings.TrimSpace(c.ID) == "":
			return errors.New("notas-lista: caso sem id")
		case vistos[c.ID]:
			return fmt.Errorf("notas-lista: id repetido: %s", c.ID)
		case c.Categoria != CatNotasLista:
			return fmt.Errorf("%s: categoria %q (quero %s)", c.ID, c.Categoria, CatNotasLista)
		case !slices.Contains(tiposDaLista, c.Tipo):
			return fmt.Errorf("%s: tipo desconhecido %q", c.ID, c.Tipo)
		case strings.TrimSpace(c.Trecho) == "" || strings.ContainsAny(c.Trecho, "\r\n"):
			return fmt.Errorf("%s: o trecho tem de ser uma linha com texto", c.ID)
		case len(c.Esperado) == 0:
			return fmt.Errorf("%s: esperado vazio", c.ID)
		}
		vistos[c.ID] = true
		if len(c.Esperado) == 1 {
			if c.Esperado[0] != c.Trecho {
				return fmt.Errorf("%s: o esperado de um item só tem de ser o trecho inteiro", c.ID)
			}
			continue
		}
		bruto, _ := json.Marshal(map[string]any{"itens": c.Esperado})
		itens, motivo := resolver.GuardarItens(c.Trecho, bruto)
		if motivo != "" || !slices.Equal(itens, c.Esperado) {
			return fmt.Errorf("%s: a guarda não aceita o esperado como está (motivo %q, itens %q)", c.ID, motivo, itens)
		}
	}
	return nil
}

// hashDosCasosDaLista é o sha256 do dataset, como hashDoDataset: dos campos de
// cada caso, em ordem de id.
func hashDosCasosDaLista(casos []CasoLista) string {
	ordenados := slices.Clone(casos)
	sort.Slice(ordenados, func(i, j int) bool { return ordenados[i].ID < ordenados[j].ID })
	h := sha256.New()
	for _, c := range ordenados {
		for _, campo := range append([]string{c.ID, c.Categoria, c.Tipo, c.Trecho}, c.Esperado...) {
			h.Write([]byte(campo))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ---------------------------------------------------------------------
// A execução
// ---------------------------------------------------------------------

// MedidaDaLista é uma chamada: o caso, a rodada, o que SepararItens devolveu,
// se o provedor foi chamado, quanto demorou e quanto custou.
type MedidaDaLista struct {
	Caso, Rodada int
	Res          resolver.ResultadoDaLista
	Chamou       bool
	Duracao      time.Duration
	Tokens       ia.Tokens
}

// cronometro é o PolishClient que mede a chamada ao provedor.
type cronometro struct {
	interno resolver.PolishClient
	agora   func() time.Time
	dur     time.Duration
	chamou  bool
}

func (c *cronometro) Polish(ctx context.Context, req resolver.PolishRequest) ([]byte, error) {
	ini := c.agora()
	b, err := c.interno.Polish(ctx, req)
	c.dur = c.agora().Sub(ini)
	c.chamou = true
	return b, err
}

// SaidaDaLista é o que um candidato mediu.
type SaidaDaLista struct {
	Cand    Candidato
	Medidas []MedidaDaLista
	Custo   float64
	// Aborto: por que o candidato parou antes do fim ("" quando não parou).
	Aborto string
}

// rodadaDaLista é a configuração do modo.
type rodadaDaLista struct {
	Frases    string
	Relatorio string
	Rodadas   int
	PrazoMax  time.Duration
	LimiteUSD float64
	Cands     []Candidato
	Precos    map[string]Preco
	Comando   string
}

// rodarNotasLista é o modo -notas-lista inteiro: lê e confere o dataset, avisa
// o custo, pede as chaves, mede cada candidato e grava o relatório.
func rodarNotasLista(ctx context.Context, r rodadaDaLista, stdout, stderr io.Writer, d deps) int {
	dados, err := d.LerArquivo(r.Frases)
	if err != nil {
		fmt.Fprintf(stderr, "polishbench: lendo os trechos da notas-lista: %v (rode a partir da raiz do repositório)\n", err)
		return 1
	}
	casos, err := carregarCasosDaLista(dados)
	if err == nil {
		err = validarCasosDaLista(casos)
	}
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 1
	}
	hash := hashDosCasosDaLista(casos)
	inicio := d.Agora()
	planejadas := len(casos) * r.Rodadas

	fmt.Fprintf(stdout, "polishbench -notas-lista — %s\n", inicio.Format("2006-01-02 15:04"))
	fmt.Fprintf(stdout, "dataset: %d trechos, sha256 %s\n", len(casos), hash[:16])
	fmt.Fprintf(stdout, "prompt: %s (hash %s)\n", resolver.NotasListaPromptRef, resolver.NotasListaPromptHash())
	var estimativa float64
	for _, c := range r.Cands {
		estimativa += float64(planejadas) * custoDe(ia.Tokens{Entrada: tokensEntradaListaEstimados, Saida: tokensSaidaListaEstimados}, r.Precos[c.ChavePreco()])
		fmt.Fprintf(stdout, "modelo: %s (esforço: %s)\n", c.Rotulo(), c.EsforcoEfetivo())
	}
	fmt.Fprintf(stdout, "%d rodada(s); %d chamadas previstas por modelo; estimativa de custo cerca de %s (limite: %s)\n",
		r.Rodadas, planejadas, usd(estimativa, 3), usd(r.LimiteUSD, 2))
	if estimativa > r.LimiteUSD {
		fmt.Fprintf(stderr, "polishbench: recusado: a estimativa (%s) já passa do limite (%s). Nenhuma chave foi pedida e nenhuma conexão foi aberta.\n",
			usd(estimativa, 3), usd(r.LimiteUSD, 2))
		return 2
	}

	lidas, cod := lerChaves(r.Cands, stdout, stderr, d)
	if cod != 0 {
		return cod
	}
	defer lidas.apagar()

	var saidas []SaidaDaLista
	var gasto float64
	incompleta := ""
	for _, c := range lidas.aRodar {
		if ctx.Err() != nil {
			incompleta = "interrompida pela pessoa (Ctrl+C)"
			break
		}
		fmt.Fprintf(stdout, "rodando %s ...\n", c.Rotulo())
		s := medirCandidatoDaLista(ctx, r, c, casos, lidas.chaves[c.Provedor], &gasto, d)
		saidas = append(saidas, s)
		if s.Aborto != "" {
			fmt.Fprintf(stdout, "%s: parou (%s) depois de %d chamada(s)\n", c.Rotulo(), s.Aborto, len(s.Medidas))
		}
		if s.Aborto == AbortoInterrompido || s.Aborto == AbortoLimite {
			incompleta = s.Aborto
			break
		}
	}

	ms := make([]MetricasDaLista, len(saidas))
	for i, s := range saidas {
		ms[i] = calcularDaLista(casos, s)
	}
	texto := mascarar(relatorioDaLista(relatorioDaListaDados{
		Data: inicio, Comando: r.Comando, Casos: casos, Hash: hash, Rodadas: r.Rodadas,
		Metricas: ms, Pulados: lidas.pulados, Gasto: gasto, Incompleta: incompleta,
	}), lidas.segredos)
	gravar := d.GravarRelatorioDaLista
	if gravar == nil {
		gravar = gravarRelatorioDaLista
	}
	for _, m := range ms {
		fmt.Fprintln(stdout, mascarar(resumoDaLista(m), lidas.segredos))
	}
	caminho, err := gravar(r.Relatorio, inicio, []byte(texto))
	if err != nil {
		fmt.Fprintf(stderr, "polishbench: gravando o relatório: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "relatório: %s\n", caminho)
	if incompleta != "" {
		return 1
	}
	return 0
}

// medirCandidatoDaLista roda os casos, rodada a rodada, um depois do outro
// (a latência é métrica), pelo mesmo resolver.SepararItens da API local.
// Para no Ctrl+C, no limite de gasto e no motivo que repetir não resolve
// (chave recusada, sem crédito, modelo inexistente ou incompatível).
func medirCandidatoDaLista(ctx context.Context, r rodadaDaLista, c Candidato, casos []CasoLista, chave string, gasto *float64, d deps) SaidaDaLista {
	s := SaidaDaLista{Cand: c}
	base := d.Transporte
	if base == nil {
		base = ia.NovoTransporteHTTP1()
	}
	cli := &ia.Cliente{HTTP: &http.Client{Transport: base}, BaseOpenAI: d.BaseOpenAI, BaseAnthropic: d.BaseAnthropic, Timeout: r.PrazoMax}
	preco := r.Precos[c.ChavePreco()]
	for rodada := 1; rodada <= r.Rodadas; rodada++ {
		for i, caso := range casos {
			if ctx.Err() != nil {
				s.Aborto = AbortoInterrompido
				return s
			}
			if *gasto > r.LimiteUSD {
				s.Aborto = AbortoLimite
				return s
			}
			var tokens ia.Tokens
			crono := &cronometro{
				interno: &ia.Polidor{
					Cliente: cli,
					Chamada: c.Chamada(),
					Chave:   func(context.Context) (string, error) { return chave, nil },
					Uso:     func(t ia.Tokens) { tokens = t },
				},
				agora: d.Agora,
			}
			ctxChamada, cancelar := context.WithTimeout(ctx, r.PrazoMax)
			res := resolver.SepararItens(ctxChamada, crono, caso.Trecho)
			cancelar()
			custo := custoDe(tokens, preco)
			s.Custo += custo
			*gasto += custo
			s.Medidas = append(s.Medidas, MedidaDaLista{Caso: i, Rodada: rodada, Res: res, Chamou: crono.chamou, Duracao: crono.dur, Tokens: tokens})
			if motivoTerminal(resolver.MotivoFallback(res.Motivo)) {
				s.Aborto = string(res.Motivo)
				return s
			}
		}
	}
	return s
}

// ---------------------------------------------------------------------
// As métricas
// ---------------------------------------------------------------------

// ErradaDaLista é um caso que não deu a lista esperada (dataset sintético: o
// relatório mostra o texto).
type ErradaDaLista struct {
	ID       string
	Como     resolver.ComoSeparou
	Motivo   resolver.MotivoDaLista
	Esperado []string
	Saida    []string
}

// MetricasDaLista é o resumo de um candidato.
type MetricasDaLista struct {
	Rotulo   string
	Aborto   string
	Medidas  int
	ComIA    int
	PorRegra int
	Nenhum   int
	// Recusas: a resposta chegou e a guarda recusou, por motivo (um_item à
	// parte, em UmItem). Erros: o provedor ou a rede, por motivo.
	Recusas map[string]int
	UmItem  int
	Erros   map[string]int
	// TP, FP e FN comparam os itens que a pessoa veria com os esperados (item
	// exato, sem a caixa da inicial); os *IA, só nas respostas aceitas.
	TP, FP, FN       int
	TPIA, FPIA, FNIA int
	// Exatas: a lista inteira igual à esperada. PorTipo: [acertos, total].
	Exatas  int
	PorTipo map[string][2]int
	// ForaDoTrecho conta os itens que não são pedaço do trecho. Tem de ser 0:
	// é o achado mais grave da rodada.
	ForaDoTrecho int
	Latencia     metrics.Stats
	AcimaDoPrazo int
	Custo        float64
	Erradas      []ErradaDaLista
}

// FallbackTaxa é a fração das medidas em que a IA não valeu.
func (m MetricasDaLista) FallbackTaxa() float64 { return taxa(m.PorRegra+m.Nenhum, m.Medidas) }

// F1 dos itens que a pessoa veria, e o das respostas aceitas.
func (m MetricasDaLista) F1() float64   { _, _, f := f1De(m.TP, m.FP, m.FN); return f }
func (m MetricasDaLista) F1IA() float64 { _, _, f := f1De(m.TPIA, m.FPIA, m.FNIA); return f }

// itemNormal é a forma de comparar itens: aparado, sem a pontuação final e
// com a inicial em minúscula (a caixa da inicial é do painel, não do corte).
func itemNormal(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), ".;,!")
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}

// itensVistos são os itens que a pessoa veria: os separados ou, sem separação,
// o trecho inteiro numa linha só.
func itensVistos(c CasoLista, res resolver.ResultadoDaLista) []string {
	if res.Como == resolver.SeparouNenhum {
		return []string{c.Trecho}
	}
	return res.Itens
}

// compararItens conta os acertos item a item (multiconjunto).
func compararItens(esperado, saida []string) (tp, fp, fn int) {
	resto := map[string]int{}
	for _, e := range esperado {
		resto[itemNormal(e)]++
	}
	for _, s := range saida {
		if k := itemNormal(s); resto[k] > 0 {
			resto[k]--
			tp++
		} else {
			fp++
		}
	}
	for _, n := range resto {
		fn += n
	}
	return tp, fp, fn
}

// pedacoDoTrecho diz se o item é um pedaço do trecho (a inicial pode ter subido).
func pedacoDoTrecho(trecho, item string) bool {
	return strings.Contains(itemNormal(trecho), itemNormal(item)) || strings.Contains(trecho, item)
}

func calcularDaLista(casos []CasoLista, s SaidaDaLista) MetricasDaLista {
	m := MetricasDaLista{
		Rotulo: s.Cand.Rotulo(), Aborto: s.Aborto, Custo: s.Custo,
		Recusas: map[string]int{}, Erros: map[string]int{}, PorTipo: map[string][2]int{},
	}
	var latencias []float64
	for _, med := range s.Medidas {
		c := casos[med.Caso]
		m.Medidas++
		switch med.Res.Como {
		case resolver.SeparouComIA:
			m.ComIA++
		case resolver.SeparouPorRegra:
			m.PorRegra++
		default:
			m.Nenhum++
		}
		switch motivo := med.Res.Motivo; {
		case motivo == "":
		case motivo == resolver.ListaUmItem:
			m.UmItem++
		case slices.Contains(motivosDaGuardaDaLista, motivo):
			m.Recusas[string(motivo)]++
		default:
			m.Erros[string(motivo)]++
		}
		if med.Chamou {
			latencias = append(latencias, med.Duracao.Seconds())
			if med.Duracao > polish.Prazo(c.Trecho) {
				m.AcimaDoPrazo++
			}
		}

		vistos := itensVistos(c, med.Res)
		tp, fp, fn := compararItens(c.Esperado, vistos)
		m.TP, m.FP, m.FN = m.TP+tp, m.FP+fp, m.FN+fn
		if med.Res.Como == resolver.SeparouComIA {
			m.TPIA, m.FPIA, m.FNIA = m.TPIA+tp, m.FPIA+fp, m.FNIA+fn
		}
		for _, it := range vistos {
			if !pedacoDoTrecho(c.Trecho, it) {
				m.ForaDoTrecho++
			}
		}
		exata := fp == 0 && fn == 0
		t := m.PorTipo[c.Tipo]
		t[1]++
		if exata {
			m.Exatas++
			t[0]++
		} else if len(m.Erradas) < maxErradasPorModelo {
			m.Erradas = append(m.Erradas, ErradaDaLista{ID: c.ID, Como: med.Res.Como, Motivo: med.Res.Motivo, Esperado: c.Esperado, Saida: vistos})
		}
		m.PorTipo[c.Tipo] = t
	}
	m.Latencia = metrics.Aggregate(latencias)
	return m
}

// ---------------------------------------------------------------------
// O relatório
// ---------------------------------------------------------------------

type relatorioDaListaDados struct {
	Data       time.Time
	Comando    string
	Casos      []CasoLista
	Hash       string
	Rodadas    int
	Metricas   []MetricasDaLista
	Pulados    []Candidato
	Gasto      float64
	Incompleta string
}

// resumoDaLista é a linha do terminal de um candidato.
func resumoDaLista(m MetricasDaLista) string {
	return fmt.Sprintf("%s: IA valeu em %s; F1 dos itens %s; listas exatas %s; fora do trecho %d",
		m.Rotulo, pct(taxa(m.ComIA, m.Medidas)), num(m.F1(), 3), pct(taxa(m.Exatas, m.Medidas)), m.ForaDoTrecho)
}

func contagemOrdenada(m map[string]int) string {
	if len(m) == 0 {
		return "—"
	}
	chaves := make([]string, 0, len(m))
	for k := range m {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	partes := make([]string, len(chaves))
	for i, k := range chaves {
		partes[i] = fmt.Sprintf("%s %d", k, m[k])
	}
	return strings.Join(partes, ", ")
}

func listaEmLinha(itens []string) string {
	partes := make([]string, len(itens))
	for i, it := range itens {
		partes[i] = inline(it)
	}
	return strings.Join(partes, " · ")
}

// relatorioDaLista monta o markdown da rodada.
func relatorioDaLista(r relatorioDaListaDados) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Benchmark da separação em itens das notas — %s\n\n", r.Data.Format("2006-01-02"))
	if r.Incompleta != "" {
		fmt.Fprintf(&b, "> **RODADA INCOMPLETA** (%s): os números abaixo são só do que foi medido.\n\n", r.Incompleta)
	}
	fmt.Fprintf(&b, "- Comando: `%s`\n", r.Comando)
	fmt.Fprintf(&b, "- Prompt: `%s`, hash `%s`\n", resolver.NotasListaPromptRef, resolver.NotasListaPromptHash())
	fmt.Fprintf(&b, "- Dataset: `%s`, %d trechos (categoria `%s`), sha256 `%s`\n", padraoNotasLista, len(r.Casos), CatNotasLista, r.Hash)
	fmt.Fprintf(&b, "- Rodadas: %d; gasto da rodada: %s\n", r.Rodadas, usd(r.Gasto, 4))
	for _, c := range r.Pulados {
		fmt.Fprintf(&b, "- `%s`: sem chave, pulado\n", c.Rotulo())
	}
	b.WriteString("\nO caminho medido é o da API local (`resolver.SepararItens`): a IA, as guardas e, quando a IA não vale, a regra. \"Fora do trecho\" é o item que não é pedaço do trecho e tem de ser **0**.\n\n")

	b.WriteString("## Resumo\n\n")
	linha(&b, "Modelo", "Medidas", "IA valeu", "Regra", "Nada separou", "Fallback", "F1 dos itens", "F1 só IA", "Listas exatas", "Fora do trecho", "p50", "p95", "Acima do prazo", "Custo")
	separador(&b, 14)
	for _, m := range r.Metricas {
		rotulo := "`" + m.Rotulo + "`"
		if m.Aborto != "" {
			rotulo += " (parou: " + m.Aborto + ")"
		}
		fora := fmt.Sprint(m.ForaDoTrecho)
		if m.ForaDoTrecho > 0 {
			fora = "**" + fora + " — BURACO NA GUARDA**"
		}
		linha(&b, rotulo, fmt.Sprint(m.Medidas), fmt.Sprint(m.ComIA), fmt.Sprint(m.PorRegra), fmt.Sprint(m.Nenhum),
			pct(m.FallbackTaxa()), num(m.F1(), 3), num(m.F1IA(), 3), pct(taxa(m.Exatas, m.Medidas)), fora,
			seg(m.Latencia.P50), seg(m.Latencia.P95), fmt.Sprint(m.AcimaDoPrazo), usd(m.Custo, 4))
	}

	b.WriteString("\n## Recusas e erros\n\n")
	b.WriteString("\"Um item\" é o modelo dizendo que o trecho não é lista (esperado nos casos `um_item` e `injecao` de uma frase só).\n\n")
	linha(&b, "Modelo", "Recusas da guarda", "Um item", "Erros do provedor ou da rede")
	separador(&b, 4)
	for _, m := range r.Metricas {
		linha(&b, "`"+m.Rotulo+"`", contagemOrdenada(m.Recusas), fmt.Sprint(m.UmItem), contagemOrdenada(m.Erros))
	}

	b.WriteString("\n## Listas exatas por tipo de trecho\n\n")
	cab := []string{"Modelo"}
	for _, t := range tiposDaLista {
		cab = append(cab, t)
	}
	linha(&b, cab...)
	separador(&b, len(cab))
	for _, m := range r.Metricas {
		celulas := []string{"`" + m.Rotulo + "`"}
		for _, t := range tiposDaLista {
			v := m.PorTipo[t]
			celulas = append(celulas, fmt.Sprintf("%d/%d", v[0], v[1]))
		}
		linha(&b, celulas...)
	}

	b.WriteString("\n## O que não saiu como o esperado\n\n")
	for _, m := range r.Metricas {
		fmt.Fprintf(&b, "### `%s`\n\n", m.Rotulo)
		if len(m.Erradas) == 0 {
			b.WriteString("Nada: toda lista saiu como a esperada.\n\n")
			continue
		}
		for _, e := range m.Erradas {
			motivo := ""
			if e.Motivo != "" {
				motivo = ", motivo `" + string(e.Motivo) + "`"
			}
			fmt.Fprintf(&b, "- `%s` (%s%s): esperado %s; saiu %s\n", e.ID, e.Como, motivo, listaEmLinha(e.Esperado), listaEmLinha(e.Saida))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// gravarRelatorioDaLista grava em docs/benchmarks/notas-lista-AAAA-MM-DD.md
// (caminho vazio), sem nunca sobrescrever, como o do Polish.
func gravarRelatorioDaLista(caminho string, dia time.Time, dados []byte) (string, error) {
	return gravarRelatorioComPrefixo("notas-lista", caminho, dia, dados)
}
