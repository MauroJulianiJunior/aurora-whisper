// Command asrbench é o CLI do harness de benchmark de ASR (TASK-006,
// docs/ai-spec/13-implementation-plan.md): recebe um manifesto de dataset
// (internal/benchmark/dataset) e uma configuração de candidatos
// (internal/benchmark/candidate, ADR-012), mede cada candidato nos modos
// cold e warm (internal/benchmark/harness) e escreve o relatório Markdown
// (internal/benchmark/report) exigido por docs/ai-spec/19-eval-plan.md.
//
// Este binário NUNCA baixa nem instala nenhum runtime de ASR — ele só
// sabe invocar um executável já presente na máquina, conforme
// docs/adr/ADR-012-asr-local-como-processo-externo.md.
//
// # Subcomando oculto: mockcandidate
//
// `asrbench mockcandidate cold <audio>` e `asrbench mockcandidate warm`
// fazem este mesmo binário se comportar como um candidato de teste que
// usa internal/asr/mock de verdade (mockcandidate.go). Isso existe para
// UMA coisa: permitir rodar o harness inteiro, ponta a ponta — dataset,
// subprocess, cold, warm, métricas, relatório — sem nenhum modelo de ASR
// instalado, apontando "asrbench mockcandidate" como o executável de um
// candidato na configuração. Não é um candidato real e não aparece em
// nenhuma lista de ajuda pública além deste comentário.
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
