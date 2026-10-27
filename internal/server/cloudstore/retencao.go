package cloudstore

import (
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// Este arquivo tem as regras puras da retenção na nuvem (ADR-019, "Decisões
// da F9"): o que o job decide sobre cada ditado e cada lápide, e os prazos da
// arrumação. Nada aqui toca banco, rede ou log; quem apaga é o Store (Reter),
// que decide de novo, com estas mesmas funções, sobre a linha travada.
//
// # Linha a linha, em Go, e em UTC
//
// Toda decisão é por item, com a data do item: nunca por um corte de data no
// SQL. O AddDate empurra o fim do mês para a frente (31/ago + 6 meses = 3/mar),
// e "now menos 6 meses" não é a conta inversa (em 1/mar dá 1/set, e o 31/ago
// já estaria antes dele): decidir pelo corte apagaria até três dias antes
// (internal/retention/job.go explica, e o regression_test.go de lá fixa o
// defeito). Os cortes daqui (CorteDoAtivo, CorteDaLapide) são só o pré-filtro
// do SQL: trazem candidatos a mais, nunca de menos.
//
// O AddDate depende do fuso (soma meses no relógio de parede), e o pgx devolve
// timestamptz no fuso local. O push e o PC contam em UTC; aqui também: toda
// função põe as datas em UTC antes de somar.
//
// # A folga de 24 horas
//
// É a mesma em toda decisão do limite dos 6 meses que envolve lápide: a lápide
// vive a janela mais um dia (LapideVencida), e só nasce quando não nasceria
// vencida (DeixaLapide). Assim um relógio do servidor que volte atrás por
// menos de um dia não abre brecha para recriar um ditado apagado. O ativo
// vencido é a exceção: sai sem lápide (o PC mantém a cópia até a F10, e uma
// lápide o faria apagá-la), e a recusa por janela (OutOfWindow) cobre.

// FolgaDaLapide é a folga do limite dos 6 meses nas decisões da lápide: o
// mesmo número do FutureTolerance (a folga para o futuro do push).
const FolgaDaLapide = 24 * time.Hour

// PrazoDosEventos: o sync_event órfão (o que nenhuma entidade viva guarda como
// last_seq) sai com 90 dias. O pull não lê evento nenhum além do last_seq das
// entidades vivas, então a idade não importa para o sync (ADR-019, "Decisões
// da F9", escolha menor 1). Confirmado pelo Mauro em 08-out-2026 ("Confirmo
// todas").
const PrazoDosEventos = 90 * 24 * time.Hour

// PrazoDasMutacoes: a resposta guardada de uma mutação (applied_mutations)
// sai com TokenSemUso mais 10 dias (190 dias), DERIVADO da vida do token e
// nunca abaixo dela. Um PC que volta dentro da vida do token reenvia os mesmos
// mutation_id do lote em voo e tem de receber duplicate; depois dela a
// reconexão esquece os envios. Com um prazo menor, o reenvio de um PC que
// volta entre o prazo e os 180 dias seria uma mutação nova, e com a base velha
// daria um selo à toa (ADR-019, "Decisões da F9", escolha menor 1).
const PrazoDasMutacoes = TokenSemUso + 10*24*time.Hour

// Retencao é a decisão da retenção sobre um ditado.
type Retencao int

const (
	// RetencaoFica: o ditado não venceu.
	RetencaoFica Retencao = iota
	// RetencaoSemLapide: sai sem lápide e sem evento. É o ativo com 6 meses
	// (o PC mantém a cópia, fora_da_janela, até a F10) e a lixeira que venceu
	// pelos 6 meses há mais de FolgaDaLapide (a lápide nasceria vencida).
	RetencaoSemLapide
	// RetencaoComLapide: a lixeira vencida ainda dentro da janela sai e deixa
	// a lápide e o evento tombstone, como o "excluir definitivamente".
	RetencaoComLapide
)

func (r Retencao) String() string {
	switch r {
	case RetencaoFica:
		return "fica"
	case RetencaoSemLapide:
		return "apaga sem lápide"
	case RetencaoComLapide:
		return "apaga com lápide"
	}
	return "?"
}

// DecideRetencao decide o que a retenção faz com um ditado criado em
// createdAt (a data do PC) e, se está na lixeira, posto lá em deletedAt, com
// o vencimento purgeAt que a lixeira gravou:
//
//   - fora da lixeira, sai sem lápide quando completa os 6 meses
//     (OutOfWindow, o mesmo limite da recusa do push);
//   - na lixeira, sai quando vence o purge_at gravado (VencimentoDaLixeira:
//     os 30 dias, ou os 6 meses, o que vier primeiro, e nunca antes de 24 h
//     depois de a lixeira chegar à nuvem), com lápide se DeixaLapide e sem
//     ela se não.
//
// O vencimento da lixeira é o gravado, e não uma conta refeita do created_at
// e do deleted_at: o piso da chegada (a decisão do Mauro de 08-out-2026) só
// existe nele. Um purge_at nulo na lixeira (o CHECK da 0003 o permite, e o
// código nunca o grava) conta FimDaLixeira, sem o piso.
//
// Pura e em UTC. O Store a chama sobre a linha lida sem trava (para escolher
// os candidatos) e de novo sobre a linha travada (para decidir de verdade): o
// ditado restaurado ou mudado entre as duas fica.
func DecideRetencao(createdAt time.Time, deletedAt, purgeAt *time.Time, now time.Time) Retencao {
	created, now := createdAt.UTC(), now.UTC()
	if deletedAt == nil {
		if OutOfWindow(created, now) {
			return RetencaoSemLapide
		}
		return RetencaoFica
	}
	vence := FimDaLixeira(created, *deletedAt)
	if purgeAt != nil {
		vence = purgeAt.UTC()
	}
	if now.Before(vence) {
		return RetencaoFica
	}
	if DeixaLapide(created, now) {
		return RetencaoComLapide
	}
	return RetencaoSemLapide
}

// FimDaLixeira é quando um ditado criado em createdAt e posto na lixeira em
// deletedAt vence pelas datas do PC: retention.EffectiveTrashPurgeAt (os 30
// dias da lixeira ou os 6 meses do ditado, o que vier primeiro), em UTC. O
// purge_at gravado é VencimentoDaLixeira, que parte daqui e acrescenta o piso
// da chegada.
//
// A lixeira nunca começa antes do próprio ditado (a revisão da F9): um
// deleted_at anterior ao created_at conta do created_at. O push só recusa a
// data no futuro, e o deleted_at vem do relógio do PC que pôs na lixeira,
// que pode não ser o que criou: um PC com o relógio semanas atrás mandaria um
// vencimento já no passado, e o job apagaria de vez, com a lápide que faz
// todos os PCs apagarem a cópia, na rodada seguinte, sem os 30 dias para
// restaurar (AC-018). O deleted_at gravado continua o que o PC mandou; o piso
// é só do vencimento.
func FimDaLixeira(createdAt, deletedAt time.Time) time.Time {
	created, deleted := createdAt.UTC(), deletedAt.UTC()
	if deleted.Before(created) {
		deleted = created
	}
	return retention.EffectiveTrashPurgeAt(created, deleted)
}

// VencimentoDaLixeira é o purge_at que o push e o CRUD gravam quando o ditado
// criado em createdAt entra na lixeira, posto lá em deletedAt (a data do PC)
// e chegado à nuvem em chegada (o relógio do servidor): FimDaLixeira, nunca
// antes de chegada + FolgaDaLapide, e esse piso nunca depois dos 6 meses do
// ditado. O pré-filtro do SQL o lê, e DecideRetencao o confere.
//
// O piso é a decisão do Mauro de 08-out-2026 ("Fica 24 h na lixeira";
// ADR-019, "Decisões da F9"): a lixeira com o deleted_at antigo (o PC que
// ficou sem internet, a D4; o relógio do PC atrasado, depois do created_at)
// nasceria vencida, e o job a apagaria em até 1 h, com a lápide que faz todos
// os PCs apagarem a cópia, sem ela passar pela lixeira restaurável da nuvem e
// do celular. Com o piso, fica 24 h e então sai. A lixeira de sempre (o
// deleted_at recente) não muda: os 30 dias contam do deleted_at, e o piso só
// pesa quando faltam menos de 24 h para eles.
//
// A janela de 6 meses ganha do piso: o ditado que chega à lixeira perto dela
// vence com ela, como sem o piso (com lápide até a folga, sem ela depois;
// DecideRetencao). Assim o purge_at também nunca passa da janela, e o
// pré-filtro do SQL (purge_at <= now) traz todo item que a janela venceu.
//
// O deleted_at gravado continua o que o PC mandou. O restaurar limpa o
// purge_at, e a lixeira seguinte calcula de novo, com a chegada dela; o
// reenvio e a lixeira que já estava lá não gravam nada, então não mexem nele.
func VencimentoDaLixeira(createdAt, deletedAt, chegada time.Time) time.Time {
	vence := FimDaLixeira(createdAt, deletedAt)
	piso := chegada.UTC().Add(FolgaDaLapide)
	if janela := retention.ActiveExpiryAt(createdAt.UTC()); piso.After(janela) {
		piso = janela
	}
	if vence.Before(piso) {
		return piso
	}
	return vence
}

// LapideVencida diz se a lápide de um ditado de dictatedAt já pode sair em
// now: a janela de 6 meses do ditado passou há FolgaDaLapide ou mais. Até lá,
// um envio com a base 0 desse ditado recebe gone apagado; depois, a recusa por
// janela (fora_da_janela) cobre, porque LapideVencida implica OutOfWindow.
func LapideVencida(dictatedAt, now time.Time) bool {
	return OutOfWindow(dictatedAt.UTC(), now.UTC().Add(-FolgaDaLapide))
}

// DeixaLapide diz se apagar de vez, em now, um ditado de createdAt deixa
// lápide: só quando ela não nasceria vencida. Vale para a retenção da lixeira
// e para o "excluir definitivamente" do CRUD (Purge).
func DeixaLapide(createdAt, now time.Time) bool {
	return !LapideVencida(createdAt, now)
}

// CorteDoAtivo é o pré-filtro grosseiro do SQL para o ativo vencido:
// created_at <= CorteDoAtivo(now). Só traz candidatos a mais: o AddDate só
// empurra para a frente, então o corte nunca cai antes do created_at de um
// ditado que já venceu (a propriedade é provada hora a hora, por quatro anos,
// em retencao_test.go). Nunca decide: quem decide é DecideRetencao.
//
// É retention.CorteDoAtivo, o mesmo corte do PC (F10): o nome fica aqui para
// não mexer nos chamadores nem nos testes do Postgres.
func CorteDoAtivo(now time.Time) time.Time {
	return retention.CorteDoAtivo(now)
}

// CorteDaLapide é o pré-filtro grosseiro do SQL para a lápide vencida:
// dictated_at <= CorteDaLapide(now), com a mesma propriedade de CorteDoAtivo.
// Quem decide é LapideVencida, linha a linha.
func CorteDaLapide(now time.Time) time.Time {
	return CorteDoAtivo(now.UTC().Add(-FolgaDaLapide))
}

// EventoVencido diz se um sync_event mudado em changedAt já passou de
// PrazoDosEventos em now. Só o órfão sai: o Store poupa o evento que é o
// last_seq de um ditado ou de um aprendido vivo.
func EventoVencido(changedAt, now time.Time) bool {
	return !changedAt.After(CorteDosEventos(now))
}

// CorteDosEventos é o instante até o qual (inclusive) o evento órfão vence:
// changed_at <= ele. Um prazo fixo em horas: o corte do SQL é a decisão.
func CorteDosEventos(now time.Time) time.Time {
	return now.UTC().Add(-PrazoDosEventos)
}

// MutacaoVencida diz se a resposta guardada de uma mutação aplicada em
// appliedAt já passou de PrazoDasMutacoes em now.
func MutacaoVencida(appliedAt, now time.Time) bool {
	return !appliedAt.After(CorteDasMutacoes(now))
}

// CorteDasMutacoes é o instante até o qual (inclusive) a resposta guardada
// vence: applied_at <= ele.
func CorteDasMutacoes(now time.Time) time.Time {
	return now.UTC().Add(-PrazoDasMutacoes)
}

// LoteDaRetencao é quantos ditados candidatos o Reter lê por vez, sem trava,
// por cursor (created_at, id): os que ficam não voltam na leitura seguinte.
// Cada um é decidido de novo e apagado na sua transação (ADR-019, "Decisões
// da F9", escolha menor 5).
const LoteDaRetencao = 500

// Retido conta o que uma rodada do Reter apagou de um usuário. Só contagens:
// nada aqui diz de quem nem o quê.
type Retido struct {
	// Ativos: ditados fora da lixeira apagados aos 6 meses, sem lápide.
	Ativos int64
	// Lixeira: ditados da lixeira apagados, com ou sem lápide.
	Lixeira int64
	// LapidesCriadas: dos da lixeira, os que deixaram lápide.
	LapidesCriadas int64
	// LapidesVencidas: lápides apagadas por LapideVencida.
	LapidesVencidas int64
	// Eventos: sync_events órfãos apagados por PrazoDosEventos.
	Eventos int64
	// Mutacoes: respostas de applied_mutations apagadas por PrazoDasMutacoes.
	Mutacoes int64
	// Notas: notas da lixeira apagadas de vez aos 30 dias (NotaVencida), cada
	// uma com a lápide e o evento tombstone (ADR-022, N8). As lápides das notas
	// nunca vencem.
	Notas int64
}

// Somar acrescenta as contagens de o.
func (r *Retido) Somar(o Retido) {
	r.Ativos += o.Ativos
	r.Lixeira += o.Lixeira
	r.LapidesCriadas += o.LapidesCriadas
	r.LapidesVencidas += o.LapidesVencidas
	r.Eventos += o.Eventos
	r.Mutacoes += o.Mutacoes
	r.Notas += o.Notas
}

// NotaVencida diz se a nota da lixeira já passou do purge_at em now (ADR-022,
// N8: a lixeira das notas esvazia sozinha também na nuvem, em 30 dias, como no
// PC). A nota fora da lixeira nunca vence: ela não tem a janela de 6 meses do
// ditado.
func NotaVencida(n Note, now time.Time) bool {
	return n.DeletedAt != nil && n.PurgeAt != nil && !n.PurgeAt.After(now)
}

// ExpurgoDaNota é a mutação do expurgo de uma nota vencida: o apagar de vez,
// como o do PC (DecideNote deixa a lápide e o evento tombstone, sem aparelho).
func ExpurgoDaNota(id string) Mutation {
	return Mutation{MutationID: NewUUIDv7(), EntityType: EntityNote, EntityID: id, Operation: OpDelete}
}

// Contou diz o que apagar um ditado com a decisão d conta: o ativo ou a
// lixeira, e a lápide. O Store chama depois de apagar de verdade (a decisão
// sobre a linha travada).
func (d Retencao) Contou(naLixeira bool) Retido {
	var r Retido
	switch {
	case d == RetencaoFica:
		return r
	case naLixeira:
		r.Lixeira = 1
	default:
		r.Ativos = 1
	}
	if d == RetencaoComLapide {
		r.LapidesCriadas = 1
	}
	return r
}
