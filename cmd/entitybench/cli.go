package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/entitygate"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("entitybench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifesto := fs.String("manifest", "testdata/asr-dataset/manifest-normalizado.json",
		"manifesto do dataset (JSON, internal/benchmark/dataset)")
	cli := fs.String("whisper", "thirdparty/whisper/Release/whisper-cli.exe",
		"executável do whisper-cli")
	modelo := fs.String("model", "models/quant/small-q4_0.bin", "modelo do ASR")
	prompt := fs.String("prompt", "", "vocabulário literal para o ASR (normalmente vem do dicionário, via -dict)")
	dicionario := fs.String("dict", "", "semente do dicionário pessoal (JSON) — alimenta o vocabulário e o resolver Smart")
	orcamento := fs.Int("vocab-budget", dictionary.OrcamentoRunasPadrao,
		"orçamento do vocabulário em runas; 0 desliga o vocabulário")
	modo := fs.String("mode", "literal", "resolver: literal|smart|nenhum")
	paralelo := fs.Int("jobs", 2, "quantas transcrições em paralelo")
	detalhe := fs.Bool("diff", true, "listar o diff por entidade")
	holdout := fs.Bool("holdout", false,
		"mede GENERALIZACAO: remove os alias aprendidos da metade de teste e mede so nela")
	dominio := fs.String("domain", "",
		"dominio do app em foco: um nome fixo (ex.: dev), \"auto\" para deduzir da categoria da amostra, ou vazio = desconhecido (o caso SEGURO)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	amostras, err := dataset.LoadManifest(*manifesto)
	if err != nil {
		fmt.Fprintln(stderr, "entitybench:", err)
		return 1
	}

	semente, err := carregarDicionario(*dicionario)
	if err != nil {
		fmt.Fprintln(stderr, "entitybench:", err)
		return 1
	}

	rotulo := "dataset completo"
	if *holdout {
		if semente == nil {
			fmt.Fprintln(stderr, "entitybench: -holdout exige -dict")
			return 2
		}
		var teste map[string]bool
		amostras, teste = metadeDeTeste(amostras)
		s := semente.SemAliasDe(teste)
		semente = &s
		rotulo = fmt.Sprintf("HOLDOUT — %d amostras de teste, alias delas removidos", len(amostras))
	}

	res, err := resolverDe(*modo, semente)
	if err != nil {
		fmt.Fprintln(stderr, "entitybench:", err)
		return 2
	}

	// O vocabulário sai do DICIONÁRIO, não de uma string digitada: é o
	// mesmo caminho que o Agent vai usar, e limitado pelo mesmo orçamento.
	// -prompt continua existindo para experimento manual, e vence quando
	// os dois são passados.
	vocab := *prompt
	if vocab == "" && semente != nil {
		vocab = dictionary.Vocabulario(semente.ParaEntradas(), *orcamento)
	}

	ctx := context.Background()
	medidas, falhas := transcreverETratar(ctx, amostras, asrConfig{
		cli: *cli, modelo: *modelo, prompt: vocab, usuario: usuarioDe(semente),
		dominio: *dominio, paralelo: *paralelo,
	}, res, stderr)

	if len(medidas) == 0 {
		fmt.Fprintln(stderr, "entitybench: nenhuma amostra com entidade esperada foi medida")
		return 1
	}

	r := entitygate.Medir(medidas)
	fmt.Fprintln(stdout, "recorte:", rotulo)
	imprimir(stdout, r, *modo, vocab, *dominio, falhas, *detalhe)

	// Sai com código 1 quando o portão não passa: isso torna o comando
	// utilizável como verificação, não só como relatório de tela.
	if !r.PassouNoPortao() {
		return 1
	}
	return 0
}

func resolverDe(modo string, semente *dictionary.Semente) (resolver.Resolver, error) {
	switch modo {
	case "nenhum":
		return nil, nil
	case "literal":
		return resolver.NewLiteralResolver(), nil
	case "smart":
		// Sem dicionário, o Smart degrada para literal + autocorreção e
		// avisa. Isso é um resultado válido de medir (mostra o piso do
		// Smart), mas nunca o que se quer medir sem querer — por isso o
		// relatório imprime se havia dicionário.
		var repo dictionary.DictionaryRepository
		if semente != nil {
			r := dictionary.NewInMemoryDictionaryRepository(nil)
			if err := semente.Popular(context.Background(), r); err != nil {
				return nil, err
			}
			repo = r
		}
		return resolver.NewSmartResolver(repo), nil
	default:
		return nil, fmt.Errorf("modo desconhecido %q (use literal, smart ou nenhum)", modo)
	}
}

// carregarDicionario lê a semente do caminho dado. Caminho vazio devolve
// nil (sem dicionário), que é um modo de medição legítimo — a linha de
// base contra a qual o ganho é comparado.
func carregarDicionario(caminho string) (*dictionary.Semente, error) {
	if caminho == "" {
		return nil, nil
	}
	f, err := os.Open(caminho)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s, err := dictionary.CarregarSemente(f)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

type asrConfig struct {
	cli, modelo, prompt string
	usuario             string
	// dominio simula o app em foco. Vazio significa DESCONHECIDO, que é
	// o caso seguro: entrada restrita não se aplica.
	dominio  string
	paralelo int
}

// transcreverETratar roda o ASR em cada amostra COM ENTIDADE ESPERADA e
// passa o texto pelo resolver. Amostra sem entidade é pulada: ela não
// contribui para o portão e custaria uma transcrição à toa.
func transcreverETratar(ctx context.Context, amostras []dataset.Sample, cfg asrConfig,
	res resolver.Resolver, stderr io.Writer) ([]entitygate.Amostra, int) {

	type saida struct {
		medida entitygate.Amostra
		ok     bool
	}
	saidas := make([]saida, len(amostras))

	if cfg.paralelo < 1 {
		cfg.paralelo = 1
	}
	sem := make(chan struct{}, cfg.paralelo)
	var wg sync.WaitGroup

	// TODAS as amostras são transcritas, inclusive as sem entidade
	// esperada.
	//
	// Pular as sem entidade parecia economia — elas não contribuem para
	// o portão. Mas o WER passava a ser medido só no subconjunto CHEIO
	// de termos técnicos, que é exatamente onde o vocabulário do ASR
	// mais ajuda. O número saía otimista por construção, e a pergunta
	// "o vocabulário piora a frase comum?" ficava sem resposta —
	// justamente a pergunta que Mauro levantou em 28-set-2026 ditando
	// uma frase sem nenhum termo técnico.
	for i := range amostras {
		wg.Add(1)
		go func(i int, s dataset.Sample) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			bruto, err := transcrever(cfg, s.AudioPath)
			if err != nil {
				fmt.Fprintf(stderr, "entitybench: %s: %v\n", s.ID, err)
				return
			}
			resolvido, err := aplicar(ctx, res, cfg.usuario, dominioDaAmostra(cfg.dominio, s.Category), bruto)
			if err != nil {
				fmt.Fprintf(stderr, "entitybench: %s: resolver: %v\n", s.ID, err)
				return
			}
			saidas[i] = saida{ok: true, medida: entitygate.Amostra{
				ID: s.ID, Entidades: s.Entities, Bruto: bruto, Resolvido: resolvido,
				Referencia: s.Reference, Intencao: s.Intended,
			}}
		}(i, amostras[i])
	}
	wg.Wait()

	var medidas []entitygate.Amostra
	var falhas int
	for i, s := range amostras {
		if !saidas[i].ok {
			falhas++
			continue
		}
		_ = s
		medidas = append(medidas, saidas[i].medida)
	}
	return medidas, falhas
}

// aplicar passa o texto bruto pelo resolver. res nil devolve o texto
// intocado — é o controle que prova que a medição não inventa diferença.
//
// usuario escopa a busca no dicionário pessoal e precisa ser o mesmo da
// semente — ver TestAplicar_UsuarioErradoNaoAchaNada.
// dominioDaAmostra resolve o domínio a usar numa amostra.
//
// "auto" deduz da categoria do dataset, e existe porque simular UM
// domínio para o dataset inteiro não é medição, é troca: as amostras de
// contabilidade e de desenvolvimento usam termos de domínios diferentes,
// e fixar "dev" desliga o dicionário contábil inteiro. Foi o que
// aconteceu em 27-set-2026 — a medição ganhou "commit" e perdeu "DAS" e
// "EFD-Reinf", e o total não mexeu.
//
// Em produção o problema não existe: o app em foco decide a cada ditado.
// É a ferramenta de medição que precisava alcançar isso.
//
// Categoria que não mapeia para domínio nenhum devolve vazio —
// desconhecido, o caso seguro.
func dominioDaAmostra(modo string, categoria dataset.Category) string {
	if modo != "auto" {
		return modo
	}
	switch categoria {
	case "contabilidade":
		return "contabil"
	case "desenvolvimento", "code-switching":
		return "dev"
	default:
		return ""
	}
}

func aplicar(ctx context.Context, res resolver.Resolver, usuario, dominio, bruto string) (string, error) {
	if res == nil {
		return bruto, nil
	}
	// Domínio vazio vira nil — "desconhecido", que deixa de fora as
	// entradas restritas. Nunca string vazia, que casaria com nada e
	// pareceria um domínio de verdade.
	var dom *string
	if dominio != "" {
		dom = &dominio
	}
	out, err := res.Resolve(ctx, resolver.ResolveRequest{
		// O usuário TEM de ser o da semente: dictionary.Search filtra por
		// UserID, então uma divergência aqui faria o dicionário não achar
		// nada — e a medição mostraria ganho zero, parecendo que o
		// dicionário não funciona em vez de que ele nem foi consultado.
		UserID:    usuario,
		Domain:    dom,
		ASRResult: asr.ASRResult{Text: bruto, Language: "pt"},
		// CloudPolicy fica no zero value (CloudDisabled): a medição do
		// portão local não pode depender de rede nem de provider externo.
	})
	if err != nil {
		return "", err
	}
	return out.FinalText, nil
}

func transcrever(cfg asrConfig, audio string) (string, error) {
	// Mesmos flags da configuração escolhida no benchmark de 18-set-2026
	// (docs/benchmarks/asr-decisao-2026-09-18.md): q4_0, pt, 12 threads,
	// audio-ctx 750, sem timestamps.
	args := []string{"-m", cfg.modelo, "-l", "pt", "-np", "-nt", "-t", "12", "-ac", "750"}
	if strings.TrimSpace(cfg.prompt) != "" {
		args = append(args, "--prompt", cfg.prompt)
	}
	args = append(args, "-f", audio)

	out, err := exec.Command(cfg.cli, args...).Output()
	if err != nil {
		if _, statErr := os.Stat(cfg.cli); os.IsNotExist(statErr) {
			return "", fmt.Errorf("%s não existe — falta baixar o whisper.cpp? Ver docs/benchmarks/asr-decisao-2026-09-18.md", cfg.cli)
		}
		return "", err
	}
	texto := strings.TrimSpace(string(out))
	if texto == "" {
		// Transcrição vazia é FALHA de medição, nunca "nota zero" — a
		// mesma regra de harness.ErrEmptyTranscription.
		return "", fmt.Errorf("transcrição vazia")
	}
	return texto, nil
}

func dominioOuDesconhecido(d string) string {
	if d == "" {
		return "desconhecido"
	}
	return d
}

func imprimir(w io.Writer, r entitygate.Resultado, modo, prompt, dom string, falhas int, detalhe bool) {
	fmt.Fprintf(w, "entitybench — %s\n", time.Now().Format("2006-01-02 15:04"))
	// O domínio PRECISA aparecer: sem ele, duas execuções com resultados
	// diferentes ficam indistinguíveis, e não dá para saber se a flag foi
	// aplicada. Entrada restrita só vale no domínio certo — ver
	// dictionary.DictionaryEntry.Restricted.
	fmt.Fprintf(w, "resolver: %s   prompt de vocabulário: %s   dominio: %s\n",
		modo, simNao(prompt != ""), dominioOuDesconhecido(dom))
	if falhas > 0 {
		fmt.Fprintf(w, "AMOSTRAS QUE FALHARAM: %d (não entram em nenhum número)\n", falhas)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "  Entity Accuracy BRUTA      %6.1f%%  (%d/%d)\n",
		100*r.Bruto.Score(), r.Bruto.Correct, r.Bruto.Total)
	fmt.Fprintf(w, "  Entity Accuracy RESOLVIDA  %6.1f%%  (%d/%d)   meta >= %.0f%%  %s\n",
		100*r.Resolvido.Score(), r.Resolvido.Correct, r.Resolvido.Total,
		100*entitygate.MetaEntityAccuracy, passou(r.Resolvido.Score() >= entitygate.MetaEntityAccuracy))
	fmt.Fprintf(w, "  correção falsa             %6.2f%%  (%d de %d que já estavam certas)   meta <= %.0f%%  %s\n",
		100*r.TaxaCorrecaoFalsa(), r.Regressoes, r.Bruto.Correct,
		100*entitygate.MetaCorrecaoFalsa, passou(r.TaxaCorrecaoFalsa() <= entitygate.MetaCorrecaoFalsa))
	fmt.Fprintf(w, "  afastadas da intenção      %6d   amostras com texto mais longe do que a pessoa queria   meta 0  %s\n",
		len(r.PiorouTexto), passou(r.NenhumaAmostraAfastada()))
	fmt.Fprintf(w, "\n  ganhos: %d      regressões: %d\n", r.Ganhos, r.Regressoes)

	// WER/CER do texto INTEIRO, ao lado das entidades.
	//
	// Medir só entidade esconderia um vocabulário que acerta o termo
	// técnico e estraga o resto da frase — exatamente o estrago que
	// Mauro viu em 28-set-2026 ("o vencimento é no dia doze" ->
	// "Ovencimento ano dia 12"), onde não havia entidade nenhuma
	// envolvida e nenhuma métrica em uso enxergava nada.
	qb, qr := r.QualidadeBruta, r.QualidadeResolvida
	fmt.Fprintf(w, "\n  WER  bruto %.3f   resolvido %.3f   (N=%d)\n",
		qb.WER, qr.WER, qb.Amostras-qb.WERFalhou)
	fmt.Fprintf(w, "  CER  bruto %.3f   resolvido %.3f\n", qb.CER, qr.CER)
	fmt.Fprintf(w, "  WER  do resolvido até a INTENÇÃO %.3f   (a autocorreção remove a retratação)\n",
		r.DistanciaDaIntencao.WER)

	// O portão não olha WER, então o resolver pode piorar o texto como
	// um todo e ainda assim PASSAR. Listar as amostras em que isso
	// aconteceu é o mínimo para alguém ver — ver
	// entitygate.Resultado.PiorouTexto.
	if n := len(r.PiorouTexto); n > 0 {
		fmt.Fprintf(w, "\n  o resolver afastou o texto da INTENÇÃO em %d amostra(s) — invisível para a correção falsa:\n", n)
		for _, p := range r.PiorouTexto {
			fmt.Fprintf(w, "\n    %s   WER %.3f -> %.3f\n", p.ID, p.WERBruto, p.WERResolvido)
			fmt.Fprintf(w, "      intenção:   %s\n", p.Alvo)
			fmt.Fprintf(w, "      bruto:      %s\n", p.Bruto)
			fmt.Fprintf(w, "      resolvido:  %s\n", p.Resolvido)
		}
	}
	fmt.Fprintf(w, "\nPORTÃO: %s\n", passou(r.PassouNoPortao()))

	if !detalhe {
		return
	}

	porStatus := map[entitygate.StatusEntidade][]entitygate.Ocorrencia{}
	for _, oc := range r.Ocorrencias {
		porStatus[oc.Status] = append(porStatus[oc.Status], oc)
	}
	// Regressão primeiro: é o que exige ação imediata.
	for _, st := range []entitygate.StatusEntidade{entitygate.Regrediu, entitygate.ContinuaErrada, entitygate.Corrigida} {
		ocs := porStatus[st]
		if len(ocs) == 0 {
			continue
		}
		sort.Slice(ocs, func(i, j int) bool { return ocs[i].Entidade < ocs[j].Entidade })
		fmt.Fprintf(w, "\n%s (%d)\n%s\n", strings.ToUpper(st.String()), len(ocs), strings.Repeat("-", 70))
		for _, oc := range ocs {
			fmt.Fprintf(w, "  [%s] %q\n", oc.AmostraID, oc.Entidade)
			fmt.Fprintf(w, "      ASR:      %s\n", oc.TextoBruto)
			if oc.TextoResolvido != oc.TextoBruto {
				fmt.Fprintf(w, "      resolver: %s\n", oc.TextoResolvido)
			}
		}
	}
}

// usuarioDe devolve o usuário da semente, ou um rótulo qualquer quando
// não há dicionário (nenhuma busca vai acontecer de todo jeito).
// metadeDeTeste parte o dataset em duas metades DETERMINÍSTICAS e
// devolve a de teste.
//
// O corte é por posição em ordem de ID, alternando — e não aleatório —
// porque a medição precisa ser reproduzível: duas execuções do mesmo
// comando têm de dar o mesmo número, senão não dá para comparar nada.
//
// Alternar (e não cortar ao meio) mantém as duas metades com mistura
// parecida de categorias: cortar "as primeiras 23" colocaria quase toda
// a contabilidade de um lado, e o número mediria o desequilíbrio em vez
// de generalização.
func metadeDeTeste(todas []dataset.Sample) ([]dataset.Sample, map[string]bool) {
	ordenadas := append([]dataset.Sample(nil), todas...)
	sort.Slice(ordenadas, func(i, j int) bool { return ordenadas[i].ID < ordenadas[j].ID })

	var teste []dataset.Sample
	ids := map[string]bool{}
	for i, s := range ordenadas {
		if i%2 == 1 {
			teste = append(teste, s)
			ids[s.ID] = true
		}
	}
	return teste, ids
}

func usuarioDe(s *dictionary.Semente) string {
	if s == nil {
		return "sem-dicionario"
	}
	return s.Usuario
}

func passou(ok bool) string {
	if ok {
		return "PASSOU"
	}
	return "NAO PASSOU"
}

func simNao(v bool) string {
	if v {
		return "sim"
	}
	return "nao"
}
