package rss

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// rssHelperEnvVar seleciona, no mesmo padrão "TestHelperProcess" que
// internal/benchmark/candidate usa (candidate/helper_process_test.go): o
// próprio binário de teste, reexecutado com essa variável de ambiente
// definida, vira um processo trivial que só dorme — sem abrir microfone,
// sem instalar hook, sem gastar CPU de verdade. É o "algo trivial e
// barato" pedido para testar SampleTree contra um processo real sem usar
// whisper nem benchmark.
const rssHelperEnvVar = "AURORA_RSS_HELPER_PROCESS"

func TestMain(m *testing.M) {
	if os.Getenv(rssHelperEnvVar) == "sleep" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestSample_CurrentProcess amostra o próprio processo de teste. Em
// Windows (onde o Agent roda de verdade) e em Linux (coberto por /proc)
// espera medição bem-sucedida com um valor plausível; em qualquer outro
// SO, aceita ok == false (não medido) como resultado válido — nunca exige
// um valor inventado.
func TestSample_CurrentProcess(t *testing.T) {
	bytes, ok := Sample(os.Getpid())

	switch runtime.GOOS {
	case "windows", "linux":
		if !ok {
			t.Fatal("Sample: want ok == true no processo atual, got false")
		}
		if bytes <= 0 {
			t.Fatalf("Sample: bytes = %d, want > 0", bytes)
		}
	default:
		// Não medido é uma resposta honesta e válida fora de
		// windows/linux — este pacote não inventa um número.
	}
}

// TestSample_InvalidPID exercita um PID que quase certamente não existe.
// Não deve dar panic, e ok deve ser false.
func TestSample_InvalidPID(t *testing.T) {
	_, ok := Sample(-1)
	if ok {
		t.Fatal("Sample(-1): want ok == false para PID inválido")
	}
}

// TestSampleTree_InvalidPID é o par de TestSample_InvalidPID para
// SampleTree: PID inválido nunca deve dar panic, e ok deve ser false —
// em qualquer SO, já que sampleTree em rss_other.go também nunca mede.
func TestSampleTree_InvalidPID(t *testing.T) {
	_, ok := SampleTree(-1)
	if ok {
		t.Fatal("SampleTree(-1): want ok == false para PID inválido")
	}
}

// TestSampleTree_RealProcessNoChildren sobe um processo real (o próprio
// binário de teste reexecutado, só dormindo — ver TestMain) que não tem
// nenhum filho, e confirma que SampleTree consegue medi-lo e devolve um
// valor pelo menos tão grande quanto Sample sozinho mediria.
//
// A comparação é ">=", não "==": um processo console recém-criado no
// Windows pode ganhar um conhost.exe associado dependendo da versão do
// SO e de como o processo pai já está conectado a um console, e isso é
// específico do ambiente, não do candidato sendo medido — não é o que
// este teste quer travar. A garantia matemática exata de "sem filhos ⇒
// soma == Sample" já é provada de forma determinística, em qualquer SO,
// por TestSumTree_SingleNode (tree_test.go), que soma sobre uma árvore
// sintética sem tocar em processo nenhum. Este teste aqui é só a
// confirmação de ponta a ponta pedida na tarefa, tolerante a ruído do
// ambiente real.
//
// Só roda no Windows — é a única plataforma onde sampleTree tem
// implementação (rss_other.go sempre devolve ok == false).
func TestSampleTree_RealProcessNoChildren(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("SampleTree só está implementado no Windows (rss_other.go devolve ok == false)")
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), rssHelperEnvVar+"=sleep")
	if err := cmd.Start(); err != nil {
		t.Fatalf("iniciando processo helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Dá um instante para o processo terminar de subir antes de medir —
	// sem isso, a primeira amostra corre o risco de pegar o processo
	// antes do SO terminar de contabilizar seu working set inicial.
	time.Sleep(100 * time.Millisecond)

	pid := cmd.Process.Pid
	treeBytes, treeOK := SampleTree(pid)
	sampleBytes, sampleOK := Sample(pid)

	if !treeOK {
		t.Fatal("SampleTree: want ok == true para processo real recém-criado")
	}
	if !sampleOK {
		t.Fatal("Sample: want ok == true para processo real recém-criado")
	}
	if treeBytes < sampleBytes {
		t.Fatalf("SampleTree = %d bytes, want >= Sample = %d bytes (a árvore nunca pode medir menos que a raiz sozinha)",
			treeBytes, sampleBytes)
	}
}
