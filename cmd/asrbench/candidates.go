package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/benchmark/candidate"
)

// candidateFile é o formato JSON de configuração de candidatos (ADR-012):
// um por candidato, com nome, executável, argv de cada modo e timeouts.
// Timeouts são strings (ex.: "30s") em vez de nanossegundos crus, para o
// arquivo de configuração ser legível por uma pessoa.
type candidateFile struct {
	Candidates []candidateEntry `json:"candidates"`
}

type candidateEntry struct {
	Name           string   `json:"name"`
	Executable     string   `json:"executable"`
	ColdArgs       []string `json:"coldArgs"`
	WarmArgs       []string `json:"warmArgs,omitempty"`
	Timeout        string   `json:"timeout,omitempty"`
	StartupTimeout string   `json:"startupTimeout,omitempty"`
}

// loadCandidates lê e converte o arquivo de configuração de candidatos em
// path para []candidate.Config. Erros de validação identificam o
// candidato pelo nome (ou índice, se o nome estiver vazio) — nunca um
// panic.
func loadCandidates(path string) ([]candidate.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lendo configuração de candidatos %s: %w", path, err)
	}

	var cf candidateFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		return nil, fmt.Errorf("configuração de candidatos %s não é JSON válido: %w", path, err)
	}
	if len(cf.Candidates) == 0 {
		return nil, fmt.Errorf("configuração de candidatos %s não declara nenhum candidato", path)
	}

	out := make([]candidate.Config, 0, len(cf.Candidates))
	for i, e := range cf.Candidates {
		label := e.Name
		if label == "" {
			label = fmt.Sprintf("candidato[%d]", i)
		}
		if e.Executable == "" {
			return nil, fmt.Errorf("%s: %s não declara executable", path, label)
		}
		// coldArgs vazio significa "este candidato não participa da
		// medição cold", simétrico ao que warmArgs vazio já significava
		// para o warm. Existe porque há candidato que SÓ faz sentido em
		// warm: cmd/whisperwarm sobe um servidor residente e fala
		// protocolo de linha, e não tem modo de um tiro.
		//
		// Inventar um coldArgs para ele seria pior que recusar: RunCold
		// não escreve nada em stdin, o adaptador leria EOF na hora e
		// devolveria stdout vazio — que o harness contaria como
		// transcrição, não como falha. Um candidato quebrado apareceria
		// no relatório como modelo ruim.
		if len(e.ColdArgs) == 0 && len(e.WarmArgs) == 0 {
			return nil, fmt.Errorf("%s: %s não declara coldArgs nem warmArgs — não há o que medir", path, label)
		}

		timeout, err := parseDurationOrZero(e.Timeout)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: timeout inválido %q: %w", path, label, e.Timeout, err)
		}
		startupTimeout, err := parseDurationOrZero(e.StartupTimeout)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: startupTimeout inválido %q: %w", path, label, e.StartupTimeout, err)
		}

		out = append(out, candidate.Config{
			Name:           label,
			Executable:     e.Executable,
			ColdArgs:       e.ColdArgs,
			WarmArgs:       e.WarmArgs,
			Timeout:        timeout,
			StartupTimeout: startupTimeout,
		})
	}
	return out, nil
}

func parseDurationOrZero(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}
