package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// deps são as dependências de run que os testes trocam. O binário de verdade
// (main) passa o console, o relógio e os arquivos reais.
type deps struct {
	// Leitor lê a chave com o eco desligado. Só o de verdade toca no console.
	Leitor LeitorChave
	Agora  func() time.Time
	// Transporte fica por baixo do observador. Nil usa o transporte do Agent
	// (ia.NovoTransporteHTTP1: só HTTP/1.1). Os testes passam um que só
	// alcança o próprio computador.
	Transporte http.RoundTripper
	// BaseOpenAI e BaseAnthropic trocam o endereço das APIs (vazio: as
	// oficiais). Só os testes usam.
	BaseOpenAI, BaseAnthropic string
	LerArquivo                func(string) ([]byte, error)
	// GravarRelatorio grava o relatório e devolve o caminho onde ficou.
	// caminho vazio escolhe docs/benchmarks/polish-AAAA-MM-DD.md, sem
	// sobrescrever nada.
	GravarRelatorio func(caminho string, dia time.Time, dados []byte) (string, error)
	// AbrirHistorico abre o historico.db só para leitura (modo -historico).
	// Nil usa abrirHistorico.
	AbrirHistorico func(caminho string) (*sql.DB, error)
	// Getenv lê o ambiente (o LOCALAPPDATA do -historico local). Nil usa
	// os.Getenv.
	Getenv func(string) string
}

// Os arquivos do dataset, a partir da raiz do repositório.
const (
	padraoFrases    = "testdata/polish-dataset/frases.json"
	padraoManifesto = "testdata/asr-dataset/manifest-normalizado.json"
)

// comandoSoLuna é o comando para medir só o gpt-6-luna, com conexão nova a
// cada chamada: a latência do uso real (ditados espaçados) e a métrica
// "maiúscula a mais". Vai no -help e no relato; um teste o roda de verdade
// contra o dublê, para o comando impresso nunca ficar com uma flag que não
// existe.
const comandoSoLuna = "go run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna -conexao-nova"

// comandoHistorico é o diagnóstico com os ditados longos do histórico do
// Agent instalado: o motivo das recusas da guarda de palavras (16-out-2026).
const comandoHistorico = "go run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna -historico local"

func depsReais() deps {
	return deps{
		Leitor:          novoLeitorConsole(),
		Agora:           time.Now,
		LerArquivo:      os.ReadFile,
		GravarRelatorio: gravarRelatorio,
	}
}

// gravarRelatorio grava sem nunca sobrescrever: um relatório é o registro de
// uma rodada que custou dinheiro.
func gravarRelatorio(caminho string, dia time.Time, dados []byte) (string, error) {
	tentativas := []string{caminho}
	if caminho == "" {
		base := filepath.Join("docs", "benchmarks", "polish-"+dia.Format("2006-01-02"))
		tentativas = []string{base + ".md"}
		for i := 2; i < 100; i++ {
			tentativas = append(tentativas, fmt.Sprintf("%s-%d.md", base, i))
		}
	}
	for _, t := range tentativas {
		if err := os.MkdirAll(filepath.Dir(t), 0o755); err != nil {
			return "", err
		}
		f, err := os.OpenFile(t, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(dados); err != nil {
			f.Close()
			return "", err
		}
		return t, f.Close()
	}
	return "", fmt.Errorf("o relatório já existe (%s); apague-o ou passe outro -relatorio", tentativas[len(tentativas)-1])
}

// perguntaDaChave é o prompt de cada provedor.
func perguntaDaChave(provedor string) string {
	return "Chave " + ia.Nome(provedor) + " (Enter pula):"
}

// Estimativa (est.) de tokens de uma chamada, só para o aviso de custo antes de
// gastar: o prompt tem cerca de 1.000 tokens, mais o ditado (algumas dezenas de
// palavras); a saída é o texto organizado, cem a duzentos tokens. O custo real
// vem dos tokens que o provedor informa.
const (
	tokensEntradaEstimados = 1200
	tokensSaidaEstimados   = 150
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer, d deps) int {
	fs := flag.NewFlagSet("polishbench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	gastar := fs.Bool("gastar", false, "OBRIGATÓRIA: confirma que você sabe que o comando chama as APIs e gasta dinheiro da sua conta")
	modelos := fs.String("modelos", "", "candidatos, separados por vírgula: provedor:modelo[@esforço] (ex.: openai:gpt-6-luna,openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5); @esforço só vale na OpenAI e vazio usa none; @"+ia.EsforcoOmitido+" tira o campo reasoning")
	rodadas := fs.Int("rodadas", 1, "quantas vezes cada frase roda em cada modelo (1 a 20)")
	precoFlag := fs.String("preco", "", "preços em US$ por 1 milhão de tokens, provedor:modelo=entrada/saída com ponto decimal (ex.: openai:gpt-6-luna=0.10/0.50), separados por vírgula; vence os padrões")
	frases := fs.String("frases", padraoFrases, "dataset de frases sintéticas")
	manifesto := fs.String("asr-manifest", padraoManifesto, "manifest do dataset de ASR: as referências (reference) viram a categoria ja_organizada")
	relatorio := fs.String("relatorio", "", "onde gravar o relatório (vazio: docs/benchmarks/polish-AAAA-MM-DD.md; nunca sobrescreve)")
	prazoAgente := fs.Duration("prazo-agente", 3500*time.Millisecond, "prazo do Agent por ditado: passou disso, o Agent usa o Smart")
	prazoMax := fs.Duration("prazo-max", 10*time.Second, "prazo real de cada chamada aqui (maior que o do Agent, para medir também as que estourariam)")
	fallbackMax := fs.Float64("fallback-max", 0.05, "regra de escolha: taxa máxima de fallback (0,05 = 5%)")
	p95Max := fs.Duration("p95-max", 2500*time.Millisecond, "regra de escolha: p95 máximo da latência morna")
	limiteUSD := fs.Float64("limite-usd", 2.0, "para a rodada se o gasto passar disto (US$)")
	paralelo := fs.Int("paralelo", 1, "chamadas em paralelo por modelo (1 a 16); a primeira (a fria) roda sempre sozinha. Mais de 1 pode distorcer a latência")
	minPalavras := fs.Int("min-palavras", 0, "mínimo de palavras para a frase ir ao provedor (0 = o padrão do Agent, 4)")
	maxEdicao := fs.Float64("max-edicao", 0, "orçamento de palavras apagadas das guardas (0 = o padrão do Agent, zero); só para calibrar com dados")
	linhaDeBase := fs.String("linha-de-base", "", "candidatos que só servem de referência (mesma sintaxe de -modelos; cada um tem de estar em -modelos): são medidos e a regra os julga, mas eles nunca vencem. A ADR-016 (decisão 9) pede o gpt-5-nano assim")
	f1Min := fs.Float64("f1-min", 0, "PISO de organização (0 = sem piso, a regra aprovada): F1 de pontuação mínimo, fora das frases já organizadas. Decisão do mantenedor; sem piso, quem só devolve a entrada passa, e o relatório avisa")
	estruturaMin := fs.Float64("estrutura-min", 0, "PISO de organização (0 = sem piso): acerto mínimo de estrutura nas frases cujo esperado tem lista ou parágrafos (0 a 1)")
	historico := fs.String("historico", "", "DIAGNÓSTICO: em vez do dataset, os ditados longos do seu histórico (o caminho do historico.db, ou "+historicoLocal+" para o do Agent instalado). Mostra NO TERMINAL o que o modelo mudou nas respostas recusadas pela guarda de palavras; não grava relatório nem arquivo nenhum")
	minCaracteres := fs.Int("min-caracteres", 500, "com -historico: só os ditados com pelo menos estes caracteres")
	maxDitados := fs.Int("max-ditados", 15, "com -historico: no máximo estes ditados, os mais recentes (1 a 100)")
	conexaoNova := fs.Bool("conexao-nova", false, "abre uma conexão nova (TCP+TLS) a cada chamada, sem reaproveitar: é o que o Agent paga quando os ditados são espaçados por mais de 90 s, o pior caso da latência. Padrão: conexão reaproveitada (as chamadas saem seguidas), que subestima esse caso")
	fs.Usage = func() { uso(stderr, fs) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		// Não repete o argumento: pode ser uma chave colada por engano.
		fmt.Fprintln(stderr, "polishbench: argumento inesperado (a chave nunca vai por argumento; o comando a pede no terminal)")
		return 2
	}

	// A recusa vem ANTES de qualquer coisa: nenhum arquivo lido, nenhuma chave
	// pedida, nenhuma conexão aberta.
	if !*gastar {
		fmt.Fprintln(stderr, "polishbench: recusado. Este comando chama as APIs da OpenAI e da Anthropic com a SUA chave e gasta dinheiro da sua conta (a rodada completa custa, por estimativa, menos de US$ 1).")
		fmt.Fprintln(stderr, "Para rodar, passe -gastar. Nenhuma chave foi pedida e nenhuma conexão foi aberta. Veja -help.")
		return 2
	}

	cands, err := analisarCandidatos(*modelos)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 2
	}
	cands, err = marcarLinhaDeBase(*linhaDeBase, cands)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 2
	}
	flagPrecos, err := analisarPrecos(*precoFlag)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 2
	}
	precos, err := precosDos(cands, flagPrecos)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 2
	}
	switch {
	case *rodadas < 1 || *rodadas > 20:
		fmt.Fprintln(stderr, "polishbench: -rodadas tem de ficar entre 1 e 20")
		return 2
	case *paralelo < 1 || *paralelo > 16:
		fmt.Fprintln(stderr, "polishbench: -paralelo tem de ficar entre 1 e 16")
		return 2
	case *prazoAgente <= 0 || *prazoMax <= 0 || *p95Max <= 0:
		fmt.Fprintln(stderr, "polishbench: os prazos têm de ser positivos")
		return 2
	case *limiteUSD <= 0:
		fmt.Fprintln(stderr, "polishbench: -limite-usd tem de ser positivo")
		return 2
	case *fallbackMax < 0 || *fallbackMax > 1:
		fmt.Fprintln(stderr, "polishbench: -fallback-max tem de ficar entre 0 e 1")
		return 2
	case *minPalavras < 0 || *maxEdicao < 0 || *maxEdicao > 1:
		fmt.Fprintln(stderr, "polishbench: -min-palavras e -max-edicao não podem ser negativos, e -max-edicao vai até 1")
		return 2
	case !(*f1Min >= 0 && *f1Min <= 1) || !(*estruturaMin >= 0 && *estruturaMin <= 1):
		fmt.Fprintln(stderr, "polishbench: -f1-min e -estrutura-min têm de ficar entre 0 e 1")
		return 2
	case *minCaracteres < 1 || *maxDitados < 1 || *maxDitados > 100:
		fmt.Fprintln(stderr, "polishbench: -min-caracteres tem de ser positivo e -max-ditados tem de ficar entre 1 e 100")
		return 2
	}

	if *historico != "" {
		return rodarHistorico(ctx, rodadaHistorico{
			Caminho: *historico, MinCaracteres: *minCaracteres, MaxDitados: *maxDitados,
			Cands: cands, Precos: precos,
			Cfg: Config{
				Rodadas: *rodadas, Paralelo: *paralelo, PrazoAgente: *prazoAgente, PrazoMax: *prazoMax,
				MinPalavras: *minPalavras, MaxEdicao: *maxEdicao, LimiteUSD: *limiteUSD, ConexaoNova: *conexaoNova,
			},
		}, stdout, stderr, d)
	}

	dadosFrases, err := d.LerArquivo(*frases)
	if err != nil {
		fmt.Fprintf(stderr, "polishbench: lendo as frases: %v (rode a partir da raiz do repositório: go -C <raiz> run ./cmd/polishbench ...)\n", err)
		return 1
	}
	dadosManifesto, err := d.LerArquivo(*manifesto)
	if err != nil {
		fmt.Fprintf(stderr, "polishbench: lendo o manifest do ASR: %v\n", err)
		return 1
	}
	sinteticas, err := carregarFrases(dadosFrases)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 1
	}
	referencias, err := carregarReferencias(dadosManifesto)
	if err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 1
	}
	casos := append(append([]Caso(nil), sinteticas...), referencias...)
	if err := validarCasos(casos); err != nil {
		fmt.Fprintln(stderr, "polishbench:", err)
		return 1
	}
	hash := hashDoDataset(casos)

	cfg := Config{
		Casos: casos, Rodadas: *rodadas, Paralelo: *paralelo,
		PrazoAgente: *prazoAgente, PrazoMax: *prazoMax,
		MinPalavras: *minPalavras, MaxEdicao: *maxEdicao, LimiteUSD: *limiteUSD, ConexaoNova: *conexaoNova,
	}
	exec := &Executor{Cfg: cfg, Agora: d.Agora, Progresso: stderr}
	planejadas := exec.planejadas()
	inicio := d.Agora()

	fmt.Fprintf(stdout, "polishbench — %s\n", inicio.Format("2006-01-02 15:04"))
	fmt.Fprintf(stdout, "dataset: %d frases (%d sintéticas, %d já organizadas), sha256 %s\n", len(casos), len(sinteticas), len(referencias), hash[:16])
	fmt.Fprintf(stdout, "prompt: %s (hash %s)\n", resolver.PolishPromptRef, resolver.PolishPromptHash())
	var estimativa float64
	for _, c := range cands {
		p := precos[c.ChavePreco()]
		estimativa += float64(planejadas) * custoDe(ia.Tokens{Entrada: tokensEntradaEstimados, Saida: tokensSaidaEstimados}, p)
		fmt.Fprintf(stdout, "modelo: %s (esforço: %s)%s\n", c.Rotulo(), c.EsforcoEfetivo(), notaLinhaDeBase(c))
	}
	fmt.Fprintf(stdout, "%d rodada(s); %d chamadas previstas por modelo, %d no total; estimativa de custo cerca de %s, sem contar tokens de raciocínio (limite da rodada: %s)\n",
		cfg.Rodadas, planejadas, planejadas*len(cands), usd(estimativa, 2), usd(cfg.LimiteUSD, 2))
	// A estimativa não é um teto, mas se ela já passa do limite a rodada para
	// no meio, gasta o limite e não decide nada: melhor recusar antes de pedir
	// a chave.
	if estimativa > cfg.LimiteUSD {
		fmt.Fprintf(stderr, "polishbench: recusado: a estimativa (%s) já passa do limite da rodada (%s), e a rodada pararia no meio, sem decisão. Reduza -rodadas ou os modelos, ou suba -limite-usd de propósito. Nenhuma chave foi pedida e nenhuma conexão foi aberta.\n",
			usd(estimativa, 2), usd(cfg.LimiteUSD, 2))
		return 2
	}

	ex, cod := pedirChavesERodar(ctx, cands, precos, exec, stdout, stderr, d)
	if cod != 0 {
		return cod
	}
	saidas, pulados, segredos, motivoIncompleta := ex.saidas, ex.pulados, ex.segredos, ex.incompleta

	metricas := make([]Metricas, len(saidas))
	for i, s := range saidas {
		metricas[i] = calcular(cfg, s)
	}
	crit := Criterios{FallbackMax: *fallbackMax, P95Max: *p95Max, F1Min: *f1Min, EstruturaMin: *estruturaMin}
	incompleta := motivoIncompleta != ""
	r := Rodada{
		Data: inicio, Comando: comandoDe(fs), Cfg: cfg, Casos: casos, HashDataset: hash,
		ArqFrases: *frases, ArqManifest: *manifesto,
		Candidatos: cands, Pulados: pulados, Metricas: metricas, Criterios: crit,
		Decisao:    escolher(metricas, crit, incompleta),
		GastoTotal: ex.gasto, Incompleta: incompleta, Motivo: motivoIncompleta,
		Go: runtime.Version(), SO: runtime.GOOS + "/" + runtime.GOARCH,
		RefPreservacao: referenciaDePreservacao(casos, cfg.MinPalavras),
		RefEco:         referenciaDoEco(casos, cfg.MinPalavras),
	}
	texto := mascarar(gerarRelatorio(r), segredos)
	caminho, err := d.GravarRelatorio(*relatorio, inicio, []byte(texto))
	// O resumo sai por um caminho só, sempre mascarado, gravando o relatório ou não.
	fmt.Fprintln(stdout, textoDoResumo(r, segredos))
	if err != nil {
		fmt.Fprintf(stderr, "polishbench: gravando o relatório: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "relatório: %s\n", caminho)
	if incompleta {
		return 1
	}
	return 0
}

// execucao é o que pedirChavesERodar devolve.
type execucao struct {
	saidas  []SaidaCandidato
	pulados []Candidato
	// segredos são as chaves lidas, para mascarar tudo o que sai.
	segredos []string
	// incompleta é o motivo de a rodada ter parado no meio (vazio: completa).
	incompleta string
	// gasto é o custo da rodada inteira, de todos os candidatos.
	gasto float64
}

// pedirChavesERodar pede as chaves (uma por provedor, lidas com o eco
// desligado; ficam só na memória do processo e são apagadas no fim) e roda
// cada candidato que tem chave, um depois do outro, parando no Ctrl+C e no
// limite de gasto. cod diferente de zero: parou antes de gastar.
func pedirChavesERodar(ctx context.Context, cands []Candidato, precos map[string]Preco, exec *Executor, stdout, stderr io.Writer, d deps) (execucao, int) {
	var ex execucao
	cfg := exec.Cfg
	chaves := map[string]string{}
	for _, c := range cands {
		if _, lida := chaves[c.Provedor]; lida {
			continue
		}
		chave, err := d.Leitor.Ler(perguntaDaChave(c.Provedor))
		if err != nil {
			if errors.Is(err, ErrInterrompido) {
				fmt.Fprintln(stderr, "polishbench: interrompido antes de gastar.")
				return ex, 1
			}
			fmt.Fprintf(stderr, "polishbench: %v\n", err)
			return ex, 1
		}
		chaves[c.Provedor] = chave
		if chave != "" {
			ex.segredos = append(ex.segredos, chave)
		}
	}
	defer func() {
		for k := range chaves {
			chaves[k] = ""
		}
	}()
	var aRodar []Candidato
	for _, c := range cands {
		if chaves[c.Provedor] == "" {
			ex.pulados = append(ex.pulados, c)
			fmt.Fprintf(stdout, "%s: sem chave, pulado\n", c.Rotulo())
			continue
		}
		aRodar = append(aRodar, c)
	}
	if len(aRodar) == 0 {
		fmt.Fprintln(stderr, "polishbench: nenhuma chave informada; nada a medir.")
		return ex, 1
	}

	// A execução.
	total := &gasto{}
	for i, c := range aRodar {
		if ctx.Err() != nil {
			ex.incompleta = "interrompida pela pessoa (Ctrl+C)"
			break
		}
		if total.valor() > cfg.LimiteUSD {
			ex.incompleta = fmt.Sprintf("o gasto passou do limite de %s; sem rodar %s", usd(cfg.LimiteUSD, 2), restantes(aRodar[i:]))
			break
		}
		fmt.Fprintf(stdout, "rodando %s ...\n", c.Rotulo())
		base := d.Transporte
		if base == nil {
			base = ia.NovoTransporteHTTP1()
		}
		if cfg.ConexaoNova {
			base = semReaproveitar{base: base}
		}
		obs := novoObservador(base)
		cli := &ia.Cliente{
			HTTP:          &http.Client{Transport: obs},
			BaseOpenAI:    d.BaseOpenAI,
			BaseAnthropic: d.BaseAnthropic,
			Timeout:       cfg.PrazoMax,
		}
		s := exec.Rodar(ctx, c, precos[c.ChavePreco()], cli, obs, chaves[c.Provedor], total)
		ex.saidas = append(ex.saidas, s)
		if s.Aborto != "" {
			fmt.Fprintf(stdout, "%s: parou (%s) depois de %d chamada(s)\n", c.Rotulo(), s.Aborto, len(s.Resultados))
		}
		switch s.Aborto {
		case AbortoInterrompido:
			ex.incompleta = "interrompida pela pessoa (Ctrl+C)"
		case AbortoLimite:
			ex.incompleta = fmt.Sprintf("o gasto passou do limite de %s", usd(cfg.LimiteUSD, 2))
		}
		if ex.incompleta != "" {
			break
		}
	}
	ex.gasto = total.valor()
	return ex, 0
}

// flagsDoComando são as flags que o relatório repete, na ordem. Ficam de fora
// os caminhos de arquivo (o relatório é versionado, e um caminho pode ter o
// nome de quem rodou).
var flagsDoComando = []string{
	"gastar", "rodadas", "modelos", "preco", "paralelo", "prazo-agente", "prazo-max",
	"fallback-max", "p95-max", "limite-usd", "min-palavras", "max-edicao",
	"linha-de-base", "f1-min", "estrutura-min", "conexao-nova",
}

// comandoDe reconstrói as flags que a pessoa passou, só as de flagsDoComando.
func comandoDe(fs *flag.FlagSet) string {
	definidas := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { definidas[f.Name] = true })
	var partes []string
	for _, nome := range flagsDoComando {
		f := fs.Lookup(nome)
		switch {
		case f == nil || !definidas[nome]:
		case nome == "gastar" || nome == "conexao-nova":
			// Flags booleanas: só o nome (e nada se a pessoa passou =false).
			if f.Value.String() == "true" {
				partes = append(partes, "-"+nome)
			}
		default:
			partes = append(partes, "-"+nome+" "+f.Value.String())
		}
	}
	return strings.Join(partes, " ")
}

// semReaproveitar pede ao transporte que feche a conexão depois de cada
// resposta ("Connection: close"): a chamada seguinte abre TCP+TLS de novo. Vale
// para qualquer transporte, o do Agent ou o dos testes.
type semReaproveitar struct{ base http.RoundTripper }

func (s semReaproveitar) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Close = true
	return s.base.RoundTrip(r)
}

// notaLinhaDeBase é o aviso que acompanha um candidato de linha de base.
func notaLinhaDeBase(c Candidato) string {
	if c.LinhaDeBase {
		return " [linha de base: medido, nunca vence]"
	}
	return ""
}

func restantes(cs []Candidato) string {
	var rs []string
	for _, c := range cs {
		rs = append(rs, c.Rotulo())
	}
	return strings.Join(rs, ", ")
}

// textoDoResumo é o que o terminal mostra do resumo: passa por mascarar, como
// tudo o que o programa escreve. Hoje o resumo não leva texto de fora (só
// rótulos, contagens e os motivos do próprio programa), mas quem o alterar não
// pode abrir um caminho para a chave sem que este ponto a esconda.
func textoDoResumo(r Rodada, segredos []string) string {
	return mascarar(resumo(r), segredos)
}

// resumo é o resumo curto do terminal.
func resumo(r Rodada) string {
	var b strings.Builder
	b.WriteString("\n")
	if r.Incompleta {
		fmt.Fprintf(&b, "RODADA INCOMPLETA: %s. Sem decisão.\n\n", r.Motivo)
	}
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "modelo\tchamadas\tfalha crítica depois\tantes\tfallback\tp95\tF1\tcusto/1.000\tregra")
	for i, m := range r.Metricas {
		veredito := "sem decisão"
		if !r.Decisao.Incompleta && i < len(r.Decisao.Vereditos) {
			veredito = "não passou"
			if r.Decisao.Vereditos[i].Passou {
				veredito = "PASSOU"
				if m.Cand.LinhaDeBase {
					veredito = "passou (linha de base: não vence)"
				}
			}
		}
		p95 := "—"
		if m.Latencia.N > 0 {
			p95 = seg(m.Latencia.P95)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\n", m.Cand.Rotulo(), m.Chamadas,
			m.ChamadasCriticasDepois, m.ChamadasCriticasAntes, pct(m.FallbackTaxa()), p95,
			num(m.F1, 3), usd(m.CustoPor1000, 4), veredito)
	}
	tw.Flush()
	b.WriteString("\n")
	for i, m := range r.Metricas {
		if m.Aborto != "" {
			fmt.Fprintf(&b, "%s: parou (%s)\n", m.Cand.Rotulo(), m.Aborto)
		}
		if m.ChamadasCriticasDepois > 0 {
			fmt.Fprintf(&b, "BURACO NA GUARDA: %s teve %d chamada(s) com falha crítica DEPOIS da guarda (veja os exemplos no relatório)\n", m.Cand.Rotulo(), m.ChamadasCriticasDepois)
		}
		if !r.Decisao.Incompleta && i < len(r.Decisao.Vereditos) && !r.Decisao.Vereditos[i].Passou {
			fmt.Fprintf(&b, "%s não passou: %s\n", m.Cand.Rotulo(), strings.Join(r.Decisao.Vereditos[i].Motivos, "; "))
		}
	}
	if !r.Incompleta {
		if len(r.Decisao.Vencedores) == 0 {
			b.WriteString("Nenhum candidato passou pela regra." + notaSoLinhaDeBase(r, "") + " O Smart segue como padrão.\n")
		}
		for _, p := range r.Decisao.provedoresNaOrdem() {
			fmt.Fprintf(&b, "vencedor %s: %s\n", nomeDoProvedor(p), r.Decisao.Vencedores[p])
		}
		for _, a := range avisosDeOrganizacao(r) {
			fmt.Fprintf(&b, "ATENÇÃO: %s\n", a)
		}
	}
	fmt.Fprintf(&b, "gasto da rodada: %s\n", usd(r.GastoTotal, 4))
	return b.String()
}

// uso é a ajuda: o que o comando faz, o comando exato para rodar e os padrões.
func uso(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprint(w, `polishbench mede modelos de IA para o modo Polish (ADR-016, docs/ai-spec/19-eval-plan.md).

Roda cada frase do dataset pelo MESMO caminho do Agent (resolver.Polir, o Polidor
de internal/ia e as guardas) e mede preservação, falha crítica antes e depois da
guarda, organização, latência, custo, fallback por motivo e obediência à injeção.
Depois aplica a regra de escolha e grava docs/benchmarks/polish-AAAA-MM-DD.md.

ATENÇÃO: chama as APIs da OpenAI e da Anthropic com a SUA chave e GASTA DINHEIRO.
Recusa rodar sem -gastar. A chave é pedida no terminal com o eco desligado
("Chave OpenAI (Enter pula):") e fica só na memória: nunca em flag, variável de
ambiente, arquivo, log ou relatório. Rode a partir da raiz do repositório.

Comando (PowerShell 5.1, sem &&):

  go run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna,openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5 -linha-de-base openai:gpt-5-nano@minimal

O gpt-5-nano entra como linha de base (ADR-016, decisão 9): é medido e julgado, e nunca
vence. Some -conexao-nova ao comando para medir a latência do pior caso (uma conexão nova a
cada chamada; sem ela o p95 subestima o Agent com ditados espaçados por mais de 90 s).
A regra aprovada não tem piso de organização: o relatório avisa se o vencedor organiza
pouco, e -f1-min e -estrutura-min (desligados) ligam o piso, decisão do mantenedor.

Só o gpt-6-luna, com conexão nova a cada chamada (mede também a latência do uso real, com
ditados espaçados, e a métrica "maiúscula a mais" do prompt 1.0.2: mês e substantivo comum
com maiúscula no meio da frase; só medida, fora da regra de escolha):

  `+comandoSoLuna+`

DIAGNÓSTICO com os SEUS ditados longos (por que a guarda de palavras recusou o Polish):
em vez do dataset, lê o histórico do Agent (só para leitura) e mostra NO TERMINAL, palavra
por palavra, o que o modelo mudou nas respostas recusadas. Não grava relatório nem arquivo:

  `+comandoHistorico+`

Preços padrão (US$ por 1 milhão de tokens, entrada/saída; pesquisa de 29-set-2026,
confira na página do provedor; -preco vence):
`)
	var chaves []string
	for k := range precosPadrao {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	for _, k := range chaves {
		p := precosPadrao[k]
		fmt.Fprintf(w, "  %s=%.2f/%.2f\n", k, p.Entrada, p.Saida)
	}
	fmt.Fprint(w, "\nFlags:\n")
	fs.PrintDefaults()
}
