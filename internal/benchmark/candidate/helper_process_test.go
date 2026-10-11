package candidate

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Este arquivo implementa o padrão clássico "TestHelperProcess" (o mesmo
// que os testes de os/exec da stdlib usam): o próprio binário de teste
// compilado por `go test` é reexecutado como um subprocesso candidato
// falso, selecionado por uma variável de ambiente. Isso testa o adaptador
// de processo externo de verdade — Start/Wait, pipes, timeout, kill —
// sem depender de nenhum executável externo (nada é baixado nem
// instalado), e sem precisar compilar um segundo binário à parte.
//
// AURORA_BENCHMARK_HELPER_PROCESS=cold|warm|fail|hang seleciona o
// comportamento; ver TestMain.

const helperEnvVar = "AURORA_BENCHMARK_HELPER_PROCESS"

// TestMain intercepta a execução do próprio binário de teste: se a
// variável de ambiente do helper estiver definida, roda o comportamento
// do "candidato falso" e sai, em vez de rodar a suíte de testes. Isso é
// exatamente o que acontece quando exec.Command reexecuta os.Args[0]
// dentro dos testes de cold.go/warm.go abaixo.
func TestMain(m *testing.M) {
	switch os.Getenv(helperEnvVar) {
	case "":
		os.Exit(m.Run())
	case "cold":
		os.Exit(helperCold())
	case "warm":
		os.Exit(helperWarm())
	case "fail":
		fmt.Fprintln(os.Stderr, "falha proposital do candidato de teste")
		os.Exit(1)
	case "hang":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "helper desconhecido")
		os.Exit(2)
	}
}

// helperCold imita um candidato cold: recebe o caminho do áudio como
// último argumento (RunCold substitui AudioPlaceholder por ele) e
// imprime uma transcrição fixa derivada do próprio caminho, só para
// provar que o caminho certo chegou até o processo.
func helperCold() int {
	args := os.Args
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "sem argumentos")
		return 1
	}
	audioPath := args[len(args)-1]
	fmt.Printf("transcricao de %s\n", audioPath)
	return 0
}

// helperWarm imita um candidato warm: lê uma linha (caminho do áudio) de
// stdin por vez e responde com uma linha de transcrição, até stdin
// fechar.
func helperWarm() int {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		audioPath := strings.TrimSpace(scanner.Text())
		fmt.Printf("warm: %s\n", audioPath)
	}
	return 0
}

// helperSelfConfig devolve um Config que reexecuta o próprio binário de
// teste com AURORA_BENCHMARK_HELPER_PROCESS=mode.
func helperSelfConfig(t *testing.T, mode string) Config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return Config{
		Name:       "helper-" + mode,
		Executable: exe,
		ColdArgs:   []string{AudioPlaceholder},
		// O conteúdo de WarmArgs não importa para o helper (o TestMain
		// intercepta pela variável de ambiente, não pelo argv) — só
		// precisa ser não vazio para StartWarmSession não devolver
		// ErrWarmNotSupported.
		WarmArgs: []string{"--warm"},
		Timeout:  5 * time.Second,
	}
}
