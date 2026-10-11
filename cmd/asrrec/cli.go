package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/dataset"
)

// defaultMaxRecording dá folga generosa sobre a fala esperada (as frases
// do dataset vão até ~10s — testdata/asr-dataset/manifest.json), porque
// quem está gravando pode hesitar, reler a frase, ou ser interrompido
// antes do segundo ENTER.
const defaultMaxRecording = 30 * time.Second

// openTimeout limita quanto tempo run() espera o microfone abrir antes de
// desistir — o mesmo padrão de audioStartTimeout em cmd/agent/main.go.
const openTimeout = 10 * time.Second

// run é toda a lógica do comando, parametrizada por stdin/stdout/stderr e
// por um recorder — nunca os.Stdin/os.Stdout/newRecorder() diretos — para
// que cli_test.go possa rodar a sessão inteira, ponta a ponta, com um
// dublê de gravação e um buffer de respostas roteirizado, sem hardware
// nenhum e sem nenhum risco de injetar entrada no sistema real.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, rec recorder) int {
	fs := flag.NewFlagSet("asrrec", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", "", "caminho do manifesto do dataset (JSON, internal/benchmark/dataset)")
	redoID := fs.String("redo", "", "regrava só a amostra com este ID, mesmo que o áudio já exista")
	maxRecording := fs.Duration("max-recording", defaultMaxRecording, "duração máxima de cada gravação")

	if err := fs.Parse(args); err != nil {
		return 2 // flag já escreveu a mensagem em stderr
	}
	if *manifestPath == "" {
		fmt.Fprintln(stderr, "asrrec: -manifest é obrigatório")
		fs.Usage()
		return 2
	}

	samples, err := loadManifestForRecording(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, "asrrec:", err)
		return 1
	}

	if *redoID != "" && !sampleIDExists(samples, *redoID) {
		fmt.Fprintf(stderr, "asrrec: -redo %s: nenhuma amostra com este ID no manifesto\n", *redoID)
		return 2
	}

	pending := pendingSamples(samples, fileExists, *redoID)
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "asrrec: nenhuma amostra pendente — dataset completo.")
		return 0
	}

	// Posição 1-based de cada amostra no manifesto INTEIRO (não só no que
	// falta), pro contador de progresso mostrar "[12/46]" e não "[1/3]"
	// quando já faltam poucas.
	total := len(samples)
	position := make(map[string]int, total)
	for i, s := range samples {
		position[s.ID] = i + 1
	}

	openCtx, cancel := context.WithTimeout(context.Background(), openTimeout)
	openErr := rec.Open(openCtx, *maxRecording)
	cancel()
	if openErr != nil {
		fmt.Fprintln(stderr, "asrrec: abrindo microfone:", openErr)
		return 1
	}
	defer func() {
		if closeErr := rec.Close(); closeErr != nil {
			fmt.Fprintln(stderr, "asrrec: fechando microfone:", closeErr)
		}
	}()

	fmt.Fprintf(stdout, "asrrec: %d de %d amostras pendentes nesta sessão.\n", len(pending), total)

	scanner := bufio.NewScanner(stdin)
	done := 0
	for _, sample := range pending {
		switch recordSampleLoop(rec, scanner, stdout, stderr, sample, position[sample.ID], total) {
		case sampleOutcomeAccepted:
			done++
		case sampleOutcomeSkipped:
			// segue para a próxima amostra pendente
		case sampleOutcomeQuit:
			fmt.Fprintf(stdout, "\nasrrec: progresso salvo. %d/%d amostras desta sessão gravadas.\n", done, len(pending))
			return 0
		}
	}

	fmt.Fprintf(stdout, "\nasrrec: sessão concluída. %d/%d amostras pendentes gravadas.\n", done, len(pending))
	return 0
}

// sampleOutcome é o que recordSampleLoop decidiu para UMA amostra: o
// resultado de zero ou mais tomadas regravadas até um ENTER (aceita), s
// (pula) ou q (sai) encerrar o laço.
type sampleOutcome int

const (
	sampleOutcomeAccepted sampleOutcome = iota
	sampleOutcomeSkipped
	sampleOutcomeQuit
)

// recordSampleLoop conduz o fluxo de UMA amostra: mostra progresso e
// frase, espera ENTER para gravar, grava uma tomada, mostra duração e
// avisos, e pergunta aceitar/regravar/pular/sair — regravando (r) quantas
// vezes for preciso antes de decidir.
func recordSampleLoop(rec recorder, scanner *bufio.Scanner, stdout, stderr io.Writer, sample dataset.Sample, position, total int) sampleOutcome {
	for {
		fmt.Fprintf(stdout, "\n%s\n", formatProgress(position, total, sample.Category))
		fmt.Fprintf(stdout, "Frase: >>> %s <<<\n", sample.Reference)

		start, ok := readAction(scanner, stdout, stderr,
			"ENTER para gravar (s pula, q sai)... ", actionAccept, actionSkip, actionQuit)
		if !ok || start == actionQuit {
			return sampleOutcomeQuit
		}
		if start == actionSkip {
			return sampleOutcomeSkipped
		}

		samples, sampleRate, vad, err := recordOneTake(rec, scanner, stdout)
		if err != nil {
			fmt.Fprintln(stderr, "asrrec: gravando:", err)
			continue
		}

		fmt.Fprintf(stdout, "Duração: %s\n", sampleDuration(len(samples), sampleRate).Round(10*time.Millisecond))
		if rel := rec.DeviceReport(); rel != "" {
			fmt.Fprintf(stdout, "  %s\n", rel)
		}
		for _, w := range recordingWarnings(samples, vad) {
			fmt.Fprintf(stdout, "  aviso: %s\n", w)
		}

		decision, ok := readAction(scanner, stdout, stderr,
			"ENTER aceita, r regrava, s pula, q salva e sai: ", actionAccept, actionRedo, actionSkip, actionQuit)
		if !ok || decision == actionQuit {
			return sampleOutcomeQuit
		}
		switch decision {
		case actionAccept:
			// testdata/asr-dataset/manifest.json aponta pra "audio/*.wav"
			// relativo ao próprio manifesto, e esse diretório não existe
			// até a primeira amostra ser aceita — dataset.WriteWAV usa
			// os.Create, que exige o diretório pai já existir.
			if err := os.MkdirAll(filepath.Dir(sample.AudioPath), 0o755); err != nil {
				fmt.Fprintln(stderr, "asrrec: criando diretório do áudio:", err)
				continue
			}
			if err := dataset.WriteWAV(sample.AudioPath, sampleRate, samples); err != nil {
				fmt.Fprintln(stderr, "asrrec: salvando WAV:", err)
				continue // não perde a tomada por um erro de escrita passageiro
			}
			return sampleOutcomeAccepted
		case actionSkip:
			return sampleOutcomeSkipped
		case actionRedo:
			continue
		}
	}
}

// readAction imprime prompt, lê uma linha de scanner e devolve a
// userAction correspondente, repetindo o prompt enquanto a resposta não
// bater com nenhuma de allowed — nunca adivinha. ok=false significa que
// stdin fechou (EOF): o chamador trata isso como pedido de saída (nunca
// como uma resposta válida), o mesmo caminho de "q", para que fechar o
// terminal ou canalizar um arquivo de respostas incompleto não deixe o
// progresso gravado até agora inconsistente.
func readAction(scanner *bufio.Scanner, stdout, stderr io.Writer, prompt string, allowed ...userAction) (userAction, bool) {
	for {
		fmt.Fprint(stdout, prompt)
		if !scanner.Scan() {
			return actionQuit, false
		}
		action := parseUserAction(scanner.Text())
		for _, a := range allowed {
			if action == a {
				return action, true
			}
		}
		fmt.Fprintln(stderr, "asrrec: resposta não reconhecida, tente de novo")
	}
}

// recordOneTake grava uma tomada: marca o início em rec, desenha o
// indicador de nível ao vivo (Frames()/RMS da sessão, via rec.BeginSample)
// até o usuário apertar ENTER de novo, e devolve o PCM capturado.
func recordOneTake(rec recorder, scanner *bufio.Scanner, stdout io.Writer) ([]int16, int, dictation.VADSummary, error) {
	levels := rec.BeginSample()
	fmt.Fprint(stdout, "Gravando... ENTER para parar.\n")

	// A goroutine do medidor só é criada DEPOIS do Fprint acima, e a
	// goroutine principal não escreve mais nada em stdout até <-meterDone
	// — por isso as duas nunca escrevem ao mesmo tempo, mesmo com
	// stdout/stderr sendo um io.Writer qualquer (em teste, um
	// *bytes.Buffer, que não é seguro para uso concorrente).
	stopMeter := make(chan struct{})
	meterDone := make(chan struct{})
	go func() {
		defer close(meterDone)
		showLevelMeter(stdout, levels, stopMeter)
	}()

	scanner.Scan() // EOF aqui não precisa de tratamento à parte: readAction, no prompt seguinte, já vê stdin fechado e sai
	close(stopMeter)
	<-meterDone

	return rec.EndSample()
}

// showLevelMeter redesenha, na mesma linha (\r), uma barra de nível
// proporcional a cada RMS recebido de levels, até stop fechar (ou levels
// fechar, o que não deveria acontecer em produção — só existe pra este
// loop não vazar caso aconteça). Só desenho: a decisão de quantos
// caracteres preencher é levelBar (session.go), testada isoladamente.
func showLevelMeter(out io.Writer, levels <-chan float64, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			fmt.Fprint(out, "\n")
			return
		case rms, ok := <-levels:
			if !ok {
				return
			}
			fmt.Fprintf(out, "\r%s", levelBar(rms))
		}
	}
}
