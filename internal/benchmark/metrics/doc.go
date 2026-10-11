// Package metrics implementa as métricas de qualidade do benchmark de ASR
// definidas em docs/ai-spec/19-eval-plan.md e usadas pelo TASK-006
// (docs/ai-spec/13-implementation-plan.md) para decidir, por medição, qual
// provider local o Aurora Whisper vai usar (docs/adr/ADR-003, ADR-012).
//
// # Escopo
//
// Este pacote é matemática pura, sem I/O e sem dependência de plataforma:
// recebe texto de referência e texto transcrito (ou listas numéricas já
// coletadas) e devolve números. Ele não sabe rodar um provider de ASR, não
// sabe ler áudio e não sabe escrever relatório — isso é responsabilidade de
// outras partes do harness de benchmark, fora deste pacote.
//
// Um erro de métrica escolhe o provider errado com confiança, porque o
// número final parece razoável mesmo estando incorreto. Por isso cada
// decisão de normalização deste pacote é documentada explicitamente nos
// comentários das funções, e cada uma tem cobertura de teste dedicada.
//
// # Métricas
//
//   - WER / CER (wer_cer.go): taxa de erro por palavra e por caractere,
//     por distância de edição.
//   - Entity Accuracy (entity.go): quantas entidades esperadas (nomes de
//     ferramentas, termos técnicos) aparecem corretamente na transcrição.
//     É o gate mais importante do produto (>=95%, docs/ai-spec/19-eval-plan.md).
//   - Number/Date Accuracy (number_date.go): números e datas extraídos da
//     referência batem com os extraídos da hipótese, tolerando apenas
//     diferenças de formatação (separador de milhar, formato de data).
//   - Agregação (aggregate.go): média, p50 e p95 de um conjunto de
//     amostras, sempre acompanhados de N, para o relatório poder dizer
//     honestamente quando um percentil não significa muita coisa (N
//     pequeno).
package metrics
