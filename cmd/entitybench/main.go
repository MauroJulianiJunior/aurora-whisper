// Command entitybench mede o portão do Sprint 5: "Entity Accuracy ≥95%
// APÓS dictionary" (docs/ai-spec/19-eval-plan.md) junto com o portão que
// puxa contra ele, "False Correction Smart ≤1%" (02-prd.md, seção 7).
//
// # Por que não é o asrbench
//
// cmd/asrbench existe para COMPARAR CANDIDATOS de ASR — trabalho
// concluído em 18-set-2026 (docs/benchmarks/asr-decisao-2026-09-18.md).
// Ele mede o texto BRUTO do candidato e diz isso explicitamente; a linha
// de gate "após dictionary" sai dos relatórios dele como NÃO MEDIDO,
// porque internal/dictionary nunca é chamado ali.
//
// Afinar dicionário é outra medição: roda muitas vezes seguidas enquanto
// os alias são ajustados, precisa do DIFF por entidade em vez de uma
// média, e não pode arriscar a comparação de candidatos que já está
// fechada. Por isso é um comando próprio.
//
// # O que ele faz
//
//	dataset -> ASR -> resolver -> entitygate.Medir -> relatório
//
// O ASR é o MESMO binário e os MESMOS flags que o Agent usa em produção
// (docs/adr/ADR-012), e o resolver é o de internal/resolver — nunca uma
// reimplementação. Medir com um dublê daria um número que não vale para
// o produto.
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
