package harness

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/mock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/candidate"
)

// Mesmo padrão TestHelperProcess de internal/benchmark/candidate: o
// binário de teste se reexecuta como um candidato falso. Aqui o candidato
// falso usa internal/asr/mock de verdade (via mock.NewConfident) para
// provar que o harness funciona de ponta a ponta com o provider mock,
// exigência explícita da tarefa: "precisa funcionar com o provider MOCK
// de internal/asr/mock para ser testável ponta a ponta sem nenhum modelo
// instalado".
//
// A transcrição devolvida para cada amostra vem de um "fixture" — um
// arquivo <audioPath>.transcript.txt escrito pelo teste ao lado do WAV —
// para que cada teste controle exatamente o texto que o "candidato"
// produz (inclusive texto propositalmente errado, para provar que WER/CER
// aparecem no relatório) sem precisar hardcodar isso no helper.

const helperEnvVar = "AURORA_HARNESS_HELPER_PROCESS"

// A mesma invocação de Run() dispara processos cold (argv =
// [AudioPlaceholder substituído]) e warm (argv = WarmArgs, ex. "--warm")
// no MESMO teste — então o modo não pode depender só da variável de
// ambiente (que vale para o processo de teste inteiro). "helper" cobre os
// dois: o processo se identifica como warm pela presença de "--warm" em
// argv, e como cold caso contrário. "fail"/"hang" continuam globais
// porque só são usados em testes que nunca chegam a subir um processo
// warm (candidato sem WarmArgs).
func TestMain(m *testing.M) {
	switch os.Getenv(helperEnvVar) {
	case "":
		os.Exit(m.Run())
	case "helper":
		if isWarmInvocation(os.Args) {
			os.Exit(helperWarm())
		}
		os.Exit(helperCold())
	case "fail":
		fmt.Fprintln(os.Stderr, "falha proposital")
		os.Exit(1)
	case "hang":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func isWarmInvocation(args []string) bool {
	for _, a := range args {
		if a == "--warm" {
			return true
		}
	}
	return false
}

// mockTranscribe lê o fixture de texto ao lado do áudio e devolve o que
// internal/asr/mock.Confident transcreveria — a chamada real ao provider
// mock, não um atalho que finge chamá-lo.
func mockTranscribe(audioPath string) (string, error) {
	fixture := audioPath + ".transcript.txt"
	raw, err := os.ReadFile(fixture)
	text := ""
	if err == nil {
		text = strings.TrimSpace(string(raw))
	}
	provider := mock.NewConfident(text, 0.9)
	res, err := provider.Transcribe(context.Background(), asr.AudioBuffer{}, asr.ASROptions{})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func helperCold() int {
	args := os.Args
	if len(args) == 0 {
		return 1
	}
	audioPath := args[len(args)-1]
	text, err := mockTranscribe(audioPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(text)
	return 0
}

func helperWarm() int {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		audioPath := strings.TrimSpace(scanner.Text())
		text, err := mockTranscribe(audioPath)
		if err != nil {
			fmt.Println("")
			continue
		}
		fmt.Println(text)
	}
	return 0
}

func helperSelfConfig(t *testing.T, name string, warmSupported bool) candidate.Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cfg := candidate.Config{
		Name:       name,
		Executable: exe,
		ColdArgs:   []string{candidate.AudioPlaceholder},
		Timeout:    5 * time.Second,
	}
	if warmSupported {
		cfg.WarmArgs = []string{"--warm"}
	}
	return cfg
}
