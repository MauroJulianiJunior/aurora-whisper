package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/local"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// run é a lógica do comando, separada de main para os testes.
//
// abrir é quem sobe o servidor de ASR em "medir"; os testes passam um dublê
// e nenhum servidor sobe.
func run(args []string, stdout, stderr io.Writer, abrir func(nomeModelo string) abridor) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, uso)
		return 2
	}
	switch args[0] {
	case "montar":
		return montar(args[1:], stdout, stderr)
	case "medir":
		return medirCLI(args[1:], stdout, stderr, abrir)
	default:
		fmt.Fprintln(stderr, uso)
		return 2
	}
}

const uso = `uso:
  asrlongo montar -manifest <manifesto> -saida <pasta> [-duracoes 30s,60s,90s] [-por-duracao 2] [-pausa 500ms]
  asrlongo medir  -manifest <manifesto> -servidor <whisper-server.exe> -modelo <modelo.bin> -out <relatorio.md>
                  [-duracoes 30s,60s,90s] [-por-duracao 2] [-pausa 500ms] [-variantes a,b] [-curtas=true]
                  [-dicionario <dicionario.json>] [-threads N] [-prazo 5m] [-log-servidor <arquivo>]`

// opcoesDeMontagem são as flags comuns aos dois subcomandos.
type opcoesDeMontagem struct {
	manifest   *string
	duracoes   *string
	porDuracao *int
	pausa      *time.Duration
}

func flagsDeMontagem(fs *flag.FlagSet) opcoesDeMontagem {
	return opcoesDeMontagem{
		manifest:   fs.String("manifest", "", "manifesto do dataset (testdata/asr-dataset/manifest-normalizado.json)"),
		duracoes:   fs.String("duracoes", "30s,60s,90s", "durações dos áudios longos, separadas por vírgula"),
		porDuracao: fs.Int("por-duracao", 2, "quantos áudios de cada duração"),
		pausa:      fs.Duration("pausa", 500*time.Millisecond, "silêncio entre as amostras concatenadas"),
	}
}

// carregar lê o manifesto e o áudio de cada amostra e monta os longos.
func (o opcoesDeMontagem) carregar() (amostras, longos []trecho, hash string, alvos []time.Duration, err error) {
	if *o.manifest == "" {
		return nil, nil, "", nil, errors.New("-manifest é obrigatório")
	}
	alvos, err = lerDuracoes(*o.duracoes)
	if err != nil {
		return nil, nil, "", nil, err
	}
	samples, err := dataset.LoadManifest(*o.manifest)
	if err != nil {
		return nil, nil, "", nil, err
	}
	hash, err = dataset.Hash(samples)
	if err != nil {
		return nil, nil, "", nil, err
	}
	for _, s := range samples {
		taxa, pcm, err := dataset.ReadWAV(s.AudioPath)
		if err != nil {
			return nil, nil, "", nil, err
		}
		amostras = append(amostras, trecho{id: s.ID, referencia: s.Reference, taxa: taxa, pcm: pcm})
	}
	longos, err = compor(amostras, alvos, *o.porDuracao, *o.pausa)
	return amostras, longos, hash, alvos, err
}

// montar grava os áudios longos e o manifesto deles numa pasta, para ouvir.
// O benchmark não precisa disto: "medir" monta em memória.
func montar(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("asrlongo montar", flag.ContinueOnError)
	fs.SetOutput(stderr)
	o := flagsDeMontagem(fs)
	saida := fs.String("saida", "", "pasta onde gravar os .wav e o manifesto-longo.json (fora do git: *.wav é ignorado)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *saida == "" {
		fmt.Fprintln(stderr, "asrlongo montar: -saida é obrigatório")
		return 2
	}
	_, longos, _, _, err := o.carregar()
	if err != nil {
		fmt.Fprintln(stderr, "asrlongo montar:", err)
		return 1
	}
	if err := os.MkdirAll(*saida, 0o755); err != nil {
		fmt.Fprintln(stderr, "asrlongo montar:", err)
		return 1
	}
	type item struct {
		ID         string   `json:"id"`
		Audio      string   `json:"audio"`
		Referencia string   `json:"referencia"`
		DuracaoMs  int64    `json:"duracao_ms"`
		Fontes     []string `json:"fontes"`
	}
	var itens []item
	for _, l := range longos {
		arquivo := l.id + ".wav"
		if err := dataset.WriteWAV(filepath.Join(*saida, arquivo), l.taxa, l.pcm); err != nil {
			fmt.Fprintln(stderr, "asrlongo montar:", err)
			return 1
		}
		itens = append(itens, item{ID: l.id, Audio: arquivo, Referencia: l.referencia, DuracaoMs: l.duracao().Milliseconds(), Fontes: l.fontes})
		fmt.Fprintf(stdout, "%s: %.1f s, %d amostras\n", l.id, l.duracao().Seconds(), len(l.fontes))
	}
	dados, _ := json.MarshalIndent(map[string]any{"longos": itens}, "", "  ")
	if err := os.WriteFile(filepath.Join(*saida, "manifesto-longo.json"), dados, 0o644); err != nil {
		fmt.Fprintln(stderr, "asrlongo montar:", err)
		return 1
	}
	return 0
}

// medirCLI sobe o servidor de ASR uma vez por variante e mede os áudios.
func medirCLI(args []string, stdout, stderr io.Writer, abrir func(string) abridor) int {
	fs := flag.NewFlagSet("asrlongo medir", flag.ContinueOnError)
	fs.SetOutput(stderr)
	o := flagsDeMontagem(fs)
	servidor := fs.String("servidor", "", "whisper-server.exe (o do pacote: whisper.cpp b5130)")
	modelo := fs.String("modelo", "", "o modelo .bin (small q4_0)")
	out := fs.String("out", "", "relatório Markdown; ao lado ficam <out>.json (com os textos) e <out>.servidor.log")
	nomes := fs.String("variantes", "", "variantes do áudio longo, separadas por vírgula (vazio: todas)")
	curtas := fs.Bool("curtas", true, "medir também as amostras curtas do dataset com ac750 e por-duracao")
	dicionario := fs.String("dicionario", "", "dicionário cujo vocabulário vai no campo prompt, como no Agent (vazio: sem vocabulário)")
	threads := fs.Int("threads", local.ThreadsRecomendadas(), "threads do servidor (-t)")
	prazo := fs.Duration("prazo", 5*time.Minute, "teto de cada transcrição")
	logServidor := fs.String("log-servidor", "", "saída do whisper-server (vazio: <out>.servidor.log)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *servidor == "" || *modelo == "" || *out == "" {
		fmt.Fprintln(stderr, "asrlongo medir: -servidor, -modelo e -out são obrigatórios")
		return 2
	}
	var escolhidas []string
	if strings.TrimSpace(*nomes) != "" {
		escolhidas = strings.Split(*nomes, ",")
	}
	longas, err := escolherVariantes(variantesLongas, escolhidas)
	if err != nil {
		fmt.Fprintln(stderr, "asrlongo medir:", err)
		return 2
	}
	amostras, longos, hash, alvos, err := o.carregar()
	if err != nil {
		fmt.Fprintln(stderr, "asrlongo medir:", err)
		return 1
	}
	vocab := ""
	if *dicionario != "" {
		f, err := os.Open(*dicionario)
		if err != nil {
			fmt.Fprintln(stderr, "asrlongo medir:", err)
			return 1
		}
		s, err := dictionary.CarregarSemente(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(stderr, "asrlongo medir:", err)
			return 1
		}
		vocab = dictionary.Vocabulario(s.ParaEntradas(), dictionary.OrcamentoRunasPadrao)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(stderr, "asrlongo medir:", err)
		return 1
	}
	caminhoLog := *logServidor
	if caminhoLog == "" {
		caminhoLog = *out + ".servidor.log"
	}
	logF, err := os.Create(caminhoLog)
	if err != nil {
		fmt.Fprintln(stderr, "asrlongo medir:", err)
		return 1
	}
	defer logF.Close()

	base := local.Config{ServerPath: *servidor, ModelPath: *modelo, Language: "pt", Threads: *threads, SaidaDoServidor: logF}
	curta := amostras[0]
	ctx := context.Background()
	rsLongos := medir(ctx, plano{base: base, variantes: longas, audios: longos, curta: &curta, vocabulario: vocab, prazo: *prazo}, abrir(filepath.Base(*modelo)), stdout)
	var rsCurtos []resultado
	var vsCurtas []variante
	if *curtas {
		vsCurtas = variantesCurtas
		rsCurtos = medir(ctx, plano{base: base, variantes: vsCurtas, audios: amostras, vocabulario: vocab, prazo: *prazo}, abrir(filepath.Base(*modelo)), stdout)
	}

	cab := cabecalho{
		data: time.Now(), servidor: *servidor, modelo: *modelo, threads: *threads,
		vocabularioRunas: len([]rune(vocab)), hashDataset: hash, duracoes: alvos,
		porDuracao: *o.porDuracao, pausa: *o.pausa, longas: longas, curtas: vsCurtas,
		nAmostrasCurtas: len(amostras), intercaladaCom: curta.id,
		comando: "asrlongo medir " + strings.Join(args, " "),
	}
	if err := os.WriteFile(*out, []byte(renderizar(cab, rsLongos, rsCurtos)), 0o644); err != nil {
		fmt.Fprintln(stderr, "asrlongo medir:", err)
		return 1
	}
	if err := escreverJSON(*out+".json", rsLongos, rsCurtos); err != nil {
		fmt.Fprintln(stderr, "asrlongo medir:", err)
		return 1
	}
	fmt.Fprintf(stdout, "asrlongo: relatório em %s\n", *out)
	return 0
}

// escreverJSON guarda tudo, com os textos transcritos, para olhar um laço
// de perto. Fica ao lado do relatório; o dataset é a voz do Mauro lendo
// frases do próprio dataset, e não ditado de uso.
func escreverJSON(caminho string, longos, curtos []resultado) error {
	type linha struct {
		Grupo     string  `json:"grupo"`
		Variante  string  `json:"variante"`
		Audio     string  `json:"audio"`
		DuracaoS  float64 `json:"duracao_s"`
		AudioCtx  string  `json:"audio_ctx"`
		TempoS    float64 `json:"tempo_s"`
		ParedeS   float64 `json:"parede_s"`
		WER       float64 `json:"wer"`
		WERGuarda float64 `json:"wer_com_guarda"`
		Lacos     int     `json:"lacos"`
		Texto     string  `json:"texto"`
		Erro      string  `json:"erro,omitempty"`
	}
	var ls []linha
	for grupo, rs := range map[string][]resultado{"longo": longos, "curto": curtos} {
		for _, r := range rs {
			l := linha{Grupo: grupo, Variante: r.variante, Audio: r.audio, DuracaoS: r.duracao.Seconds(), AudioCtx: r.audioCtx,
				TempoS: r.tempo.Seconds(), ParedeS: r.parede.Seconds(), WER: r.wer, WERGuarda: r.werGuarda, Lacos: r.lacos, Texto: r.texto}
			if r.err != nil {
				l.Erro = r.err.Error()
			}
			ls = append(ls, l)
		}
	}
	dados, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(caminho, dados, 0o644)
}
