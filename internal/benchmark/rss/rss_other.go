//go:build !windows

package rss

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// sample lê /proc/<pid>/status (Linux) e devolve VmRSS. Em qualquer outro
// SO desta branch (ex.: macOS, que não tem /proc), ok é sempre false — o
// Agent roda em Windows e o CI cobre GOOS=linux (docs/CLAUDE.md, "Testes
// que agem no sistema real" fala do Windows; o build multiplataforma é
// exigência do CI, não implica suporte de medição em todo SO). Não medido
// é honesto; um valor chutado não seria.
func sample(pid int) (int64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}

	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		// Formato: "VmRSS:	  12345 kB"
		if len(fields) < 2 {
			return 0, false
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

// sampleTree não tem implementação fora do Windows — enumerar a árvore de
// processos (topologia + instante de criação, para tratar reaproveitamento
// de PID) não tem uma API de /proc tão direta quanto VmRSS, e o Agent só
// roda em Windows mesmo. Devolve ok == false, como sample já faz para
// qualquer SO que não seja linux: não medido é honesto, um valor chutado
// não seria.
func sampleTree(pid int) (int64, bool) {
	return 0, false
}
