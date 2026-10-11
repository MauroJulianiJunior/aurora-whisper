// Package benchmark reúne o harness que mede candidatos de ASR local por
// execução real, conforme docs/adr/ADR-012-asr-local-como-processo-externo.md
// e docs/ai-spec/19-eval-plan.md (TASK-006, docs/ai-spec/13-implementation-plan.md).
//
// # Por que este pacote existe
//
// O ADR-003 já decidiu que nenhum modelo de ASR entra como dependência
// estrutural: tudo fala com asr.ASRProvider. O ADR-012 decide COMO um
// candidato local roda (processo externo, nunca CGO) e QUE CUSTO o
// benchmark mede (ponta a ponta, cold e warm separados — nunca somados).
// Este pacote implementa essa decisão.
//
// # Subpacotes
//
//   - dataset: carrega e valida o manifesto do dataset de voz (fora do
//     git, invariante AC-028) e calcula seu hash.
//   - candidate: adaptador genérico de processo externo (ADR-012) — roda
//     um candidato em modo cold (processo novo por amostra) ou warm
//     (processo residente, protocolo de linha por stdin/stdout).
//   - rss: amostragem de RSS (memória residente) de um processo por PID,
//     específica de plataforma.
//   - metrics (já entregue, fora do escopo deste sprint): WER, CER,
//     Entity Accuracy, Number/Date Accuracy, Aggregate — matemática pura.
//   - harness: orquestra dataset + candidate + metrics + rss em uma
//     medição completa (BenchmarkResult).
//   - report: renderiza o BenchmarkResult como Markdown, no formato que
//     docs/ai-spec/19-eval-plan.md exige, com os gates confrontados e a
//     decisão técnica deixada em branco (o benchmark NÃO decide sozinho —
//     ADR-012, "O que o benchmark NÃO pode fazer").
//
// # O que este pacote nunca faz
//
// Não baixa nem instala nenhum runtime de ASR. Não inventa uma medição:
// quando algo não pôde ser medido no ambiente, o resultado carrega essa
// informação explicitamente (ex.: RSSMeasured == false) em vez de um
// zero que parece um valor real. Não decide qual candidato vence — produz
// números e confronta com os gates; a decisão fica para uma pessoa
// preencher no relatório.
package benchmark
