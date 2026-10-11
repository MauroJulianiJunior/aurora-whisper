package retention

import "time"

// TrashRetentionDays é quantos dias, a partir do delete manual, um item
// fica restaurável na lixeira antes do purge definitivo (AC-018, AC-019;
// docs/ai-spec/05-data-model.md: "manual delete: deleted_at + purge_at 30
// dias").
const TrashRetentionDays = 30

// ActiveRetentionMonths é quantos meses, a partir de CreatedAt, um item do
// histórico ativo sobrevive antes do purge definitivo (AC-020;
// docs/ai-spec/05-data-model.md: "active history: 6 meses").
//
// É expresso em meses (via time.Time.AddDate), não em time.Duration,
// porque meses têm comprimento variável e a spec fala em "6 meses" de
// calendário, não em uma quantidade fixa de horas.
const ActiveRetentionMonths = 6

// DefaultSyncRetentionDays é o valor padrão de retenção de
// SyncEvent/AppliedMutation.
//
// [ASSUMPTION] docs/ai-spec/05-data-model.md marca isso como suposição,
// não decisão de produto confirmada: "SyncEvents/AppliedMutation:
// [ASSUMPTION] 90 dias ou compactação". Por isso o valor é configurável em
// Job.SyncRetentionDays em vez de embutido sem alternativa.
const DefaultSyncRetentionDays = 90

// ActiveExpiryAt devolve o instante em que um item do histórico ativo,
// criado em createdAt, completa a retenção de 6 meses e se torna elegível
// para purge (AC-020).
func ActiveExpiryAt(createdAt time.Time) time.Time {
	return createdAt.AddDate(0, ActiveRetentionMonths, 0)
}

// CorteDoAtivo é o pré-filtro grosseiro do SQL para o ativo vencido em agora:
// created_at <= CorteDoAtivo(agora). Só traz candidatos a mais (até uns 3
// dias perto do fim do mês, pelos meses de comprimento diferente), nunca de
// menos: quem decide é a conta exata, linha a linha (ActiveExpiryAt). É o
// mesmo corte no PC (os 6 meses da F10) e na nuvem (cloudstore.CorteDoAtivo,
// F9): um lugar só (ADR-019, "Decisões da F10").
//
// O .UTC() importa: o relógio do Agent é o do sistema, em hora local, e o
// AddDate soma meses no relógio de parede do fuso. Em America/Sao_Paulo, com
// agora = 2027-03-01T01:00Z (28/fev 22:00 local), o corte local daria
// 2026-08-29T01:00Z, e o ditado de 2026-09-01T00:30Z, vencido desde
// 2027-03-01T00:30Z, ficaria de fora do pré-filtro por uns 3 dias.
func CorteDoAtivo(agora time.Time) time.Time {
	return agora.UTC().AddDate(0, -ActiveRetentionMonths, 0)
}

// TrashExpiryAt devolve o instante em que um item deletado em deletedAt
// completaria os 30 dias de lixeira, ignorando a regra de sobreposição
// (AC-019). Use EffectiveTrashPurgeAt para a data real de purge de um item
// na lixeira.
func TrashExpiryAt(deletedAt time.Time) time.Time {
	return deletedAt.AddDate(0, 0, TrashRetentionDays)
}

// EffectiveTrashPurgeAt computa a data de purge definitiva de um item na
// lixeira, aplicando a REGRA DE SOBREPOSIÇÃO: entre os 30 dias de lixeira
// (a partir de deletedAt) e os 6 meses de histórico ativo (a partir de
// createdAt), vale a que vencer PRIMEIRO.
//
// É a regra mais perigosa da retenção — onde dado some sem ninguém
// entender por quê, se não for respeitada: deletar um item não pode
// estender a vida dele além do que o histórico ativo já previa. Um item
// criado há quase 6 meses e deletado ontem NÃO sobrevive 30 dias; ele
// morre quando completa 6 meses de criado.
func EffectiveTrashPurgeAt(createdAt, deletedAt time.Time) time.Time {
	trashDeadline := TrashExpiryAt(deletedAt)
	activeDeadline := ActiveExpiryAt(createdAt)
	if activeDeadline.Before(trashDeadline) {
		return activeDeadline
	}
	return trashDeadline
}
