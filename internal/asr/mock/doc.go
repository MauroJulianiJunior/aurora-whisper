// Package mock fornece implementações determinísticas e síncronas de
// asr.ASRProvider, usadas nos testes de conformidade (AC-029) e do motor
// de policy (internal/asr.Engine). Nenhuma delas toca rede, disco ou
// modelo real — a Fase 1 é validação de contratos, sem ASR de verdade.
//
// Os três providers têm capabilities deliberadamente diferentes entre si,
// para provar que o domínio fala com todos pela mesma interface
// (docs/ai-spec/03-acceptance-criteria.md, AC-029):
//
//   - Confident: fornece confidence e N-best.
//   - Unscored: nunca fornece confidence (Capabilities().Confidence ==
//     false), nem N-best.
//   - Failing: sempre falha, para exercitar degradação de policy sem
//     panic.
package mock
