// Package retention implementa o motor de retenção do Aurora Whisper:
// o job periódico que apaga dado do usuário de forma DEFINITIVA, conforme
// docs/ai-spec/05-data-model.md ("Retenção"), docs/ai-spec/09-security-privacy.md
// ("Retenção") e docs/ai-spec/02-prd.md ("5. Regras").
//
// # Regras de produto
//
//   - Histórico ativo: 6 meses a partir de CreatedAt, depois purge
//     definitivo (AC-020).
//   - Delete manual: vai para a lixeira; restore funciona enquanto não
//     houver purge (AC-018); a lixeira purga em D+30 a partir do delete
//     (AC-019).
//   - Regra de sobreposição (a mais perigosa das duas): se a lixeira
//     atingiria 6 meses de CRIADO antes de completar 30 dias de
//     DELETADO, a retenção de 6 meses prevalece — deletar nunca estende
//     a vida de um dado além do que o histórico ativo já previa. Ver
//     EffectiveTrashPurgeAt.
//   - Corrections e dictionary persistem até exclusão explícita; não
//     expiram por tempo. Quando a transcrição de origem é purgada,
//     Correction.TranscriptionID é desvinculado (ON DELETE SET NULL) —
//     a correção aprendida sobrevive ao ditado que a originou.
//   - SyncEvent/AppliedMutation: a spec marca [ASSUMPTION] 90 dias ou
//     compactação — não é uma decisão de produto confirmada. Este pacote
//     implementa os 90 dias, mas o número é configurável
//     (Job.SyncRetentionDays) e o comentário deixa a suposição explícita.
//
// # Desenho
//
// O motor opera inteiramente sobre interfaces de repositório
// (TranscriptionRepository, CorrectionRepository, SyncRepository) — nunca
// sobre um banco concreto. Este bloco (F1-E) valida esses contratos com
// implementações em memória (fake.go), como os demais blocos da Fase 1
// validaram os deles. A implementação real (SQLite local / Postgres
// cloud) desses contratos fica para um bloco futuro.
//
// O relógio é sempre injetado via internal/clock — nenhum código deste
// pacote chama time.Now diretamente — para que os testes de fronteira
// (D+29 vs D+30, "um dia antes dos 6 meses" etc.) sejam determinísticos.
//
// O job (Job.Run) é idempotente (NFR-013): ele recalcula, a cada
// execução, quais itens já venceram a partir do estado atual do
// repositório, então rodar duas vezes seguidas nunca apaga a mais nem
// falha — a segunda rodada simplesmente não encontra mais nada vencido.
package retention
