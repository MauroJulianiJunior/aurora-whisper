package pgstore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// A retenção no Postgres (ADR-019, "Decisões da F9"): cloudstore.Store.Reter.
//
// # Os ditados
//
// Os candidatos são lidos SEM trava, em lotes de cloudstore.LoteDaRetencao,
// por cursor (created_at, id) maior que o último lido: os que ficam não voltam
// na leitura seguinte. O WHERE é só o pré-filtro grosseiro (o ativo com
// created_at <= CorteDoAtivo, a lixeira com purge_at <= now): traz candidatos
// a mais, nunca de menos, e nunca decide. Nada de interval no SQL: o Postgres
// prende o fim do mês (31/ago + 6 meses = 28/fev) e decidir por ele apagaria
// até três dias antes.
//
// Cada linha lida é decidida em Go (cloudstore.DecideRetencao, em UTC, com o
// purge_at gravado: o vencimento da lixeira com o piso da chegada). Só o
// que parece vencido passa pelo gravar (o contador sobe primeiro, a linha vem
// FOR UPDATE) e é decidido DE NOVO sobre a linha travada: o ditado restaurado
// ou mudado entre a leitura e a trava fica (AC-018). Um item por transação:
// um lote numa transação só seguraria a trava do contador, e o push e o CRUD
// do mesmo usuário esperariam.
//
// # As lápides
//
// Em lotes, cada um numa transação curta: trava o contador (a ordem de travas
// é a de sempre: contador, entidade, evento; um push do mesmo usuário espera
// em vez de ver a lápide sumir no meio), lê um lote de lápides com o
// pré-filtro grosseiro (dictated_at <= CorteDaLapide), por cursor, decide cada
// uma com cloudstore.LapideVencida e apaga as vencidas pela chave. A marca
// d'água não sobe (D2).
//
// # A arrumação (F9c)
//
// Depois das lápides, também em lotes de loteDaArrumacao, cada um na sua
// transação curta com o contador travado (a revisão da F9: um DELETE sem
// limite seguraria a trava por um tempo proporcional ao que venceu): o
// sync_event com changed_at até CorteDosEventos que NÃO é o last_seq de um
// ditado nem de um aprendido vivo (o NOT EXISTS usa os dois UNIQUE (user_id,
// last_seq) como índice; o pull junta a entidade ao evento dela para dar o
// aparelho de origem), e a resposta de applied_mutations com applied_at até
// CorteDasMutacoes. Com o contador travado, o "apagar meus dados" e o push do
// mesmo usuário esperam um lote: dois DELETE sobre os mesmos eventos nunca
// disputam linha um com o outro.

// antesDaTravaDaRetencao, quando não é nil, roda antes de cada candidato ser
// travado e decidido de novo, fora de qualquer transação. Só os testes o
// ligam, para restaurar um ditado no meio ou cancelar o ctx.
var antesDaTravaDaRetencao func(id string)

// depoisDeUmLoteDaArrumacao, quando não é nil, roda depois de cada lote da
// arrumação confirmar, com o contador já solto. Só os testes o ligam.
var depoisDeUmLoteDaArrumacao func()

// loteDasLapides e loteDaArrumacao são quantas lápides cada transação da
// arrumação lê, e quantos eventos ou respostas cada uma apaga. Variáveis só
// para os testes provarem os lotes com poucas linhas.
var (
	loteDasLapides  = cloudstore.LoteDaRetencao
	loteDaArrumacao = 5000
)

const (
	sqlCandidatosDaRetencao = `SELECT id, created_at, deleted_at, purge_at FROM transcriptions
		WHERE user_id = $1
			AND ((deleted_at IS NULL AND created_at <= $2)
				OR (deleted_at IS NOT NULL AND (purge_at IS NULL OR purge_at <= $3)))`
	sqlCandidatosDepois = ` AND (created_at, id) > ($5, $6)`
	sqlCandidatosOrdem  = ` ORDER BY created_at, id LIMIT $4`

	sqlTravarContador = `SELECT last_seq FROM user_sync_counters WHERE user_id = $1 FOR UPDATE`

	sqlLapidesCandidatas = `SELECT entity_id, dictated_at FROM tombstones
		WHERE user_id = $1 AND entity_type = 'transcription' AND dictated_at <= $2`
	sqlLapidesDepois = ` AND (dictated_at, entity_id) > ($4, $5)`
	sqlLapidesOrdem  = ` ORDER BY dictated_at, entity_id LIMIT $3`

	// O DELETE é pela chave: as vencidas que o Go escolheu, e só elas.
	sqlApagarLapides = `DELETE FROM tombstones
		WHERE user_id = $1 AND entity_type = 'transcription' AND entity_id = ANY ($2::text[]::uuid[])`

	sqlUsuariosDaRetencao = `SELECT user_id FROM user_sync_counters ORDER BY user_id`

	// Um lote de até $3 linhas por vez, pela chave primária. O índice da
	// escolha é o idx_sync_events_changed_at (user_id, changed_at) da 0008.
	sqlArrumarEventos = `DELETE FROM sync_events
		WHERE user_id = $1 AND seq IN (
			SELECT e.seq FROM sync_events e
			WHERE e.user_id = $1 AND e.changed_at <= $2
				AND NOT EXISTS (SELECT 1 FROM transcriptions t WHERE t.user_id = e.user_id AND t.last_seq = e.seq)
				AND NOT EXISTS (SELECT 1 FROM learned_pairs l WHERE l.user_id = e.user_id AND l.last_seq = e.seq)
			LIMIT $3)`
	// Os eventos das notas e dos marcadores (N7; a 0024), pela mesma regra: o
	// órfão vencido sai, e o que é o last_seq de uma nota ou de um marcador
	// vivo fica. O índice da escolha é o idx_note_sync_events_changed_at. As
	// lápides das notas nunca vencem: a arrumação não as toca.
	sqlArrumarEventosDasNotas = `DELETE FROM note_sync_events
		WHERE user_id = $1 AND seq IN (
			SELECT e.seq FROM note_sync_events e
			WHERE e.user_id = $1 AND e.changed_at <= $2
				AND NOT EXISTS (SELECT 1 FROM notes n WHERE n.user_id = e.user_id AND n.last_seq = e.seq)
				AND NOT EXISTS (SELECT 1 FROM labels b WHERE b.user_id = e.user_id AND b.last_seq = e.seq)
			LIMIT $3)`
	sqlArrumarMutacoes = `DELETE FROM applied_mutations
		WHERE user_id = $1 AND mutation_id IN (
			SELECT a.mutation_id FROM applied_mutations a
			WHERE a.user_id = $1 AND a.applied_at <= $2
			LIMIT $3)`
)

// candidato é um ditado lido sem trava, só com o que DecideRetencao precisa.
type candidato struct {
	id        string
	createdAt time.Time
	deletedAt *time.Time
	purgeAt   *time.Time
}

func (s *Store) UsuariosDaRetencao(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, sqlUsuariosDaRetencao)
	if err != nil {
		return nil, sanitize("listar os usuários", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, sanitize("listar os usuários", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, sanitize("listar os usuários", err)
	}
	return ids, nil
}

func (s *Store) Reter(ctx context.Context, userID string, now time.Time) (cloudstore.Retido, error) {
	now = cloudstore.Normalize(now)
	var r cloudstore.Retido
	var depois *candidato
	for {
		lote, err := s.candidatosDaRetencao(ctx, userID, now, depois)
		if err != nil {
			return r, err
		}
		for _, c := range lote {
			if cloudstore.DecideRetencao(c.createdAt, c.deletedAt, c.purgeAt, now) == cloudstore.RetencaoFica {
				continue
			}
			feito, err := s.reterUm(ctx, userID, c.id, now)
			if err != nil {
				return r, err
			}
			r.Somar(feito)
		}
		if len(lote) < cloudstore.LoteDaRetencao {
			break
		}
		ultimo := lote[len(lote)-1]
		depois = &ultimo
	}
	arrumado, err := s.arrumar(ctx, userID, now)
	r.Somar(arrumado)
	return r, err
}

// candidatosDaRetencao lê, sem trava, um lote de ditados que passam no
// pré-filtro, depois do cursor (nil: do começo).
func (s *Store) candidatosDaRetencao(ctx context.Context, userID string, now time.Time, depois *candidato) ([]candidato, error) {
	sql := sqlCandidatosDaRetencao
	args := []any{userID, cloudstore.CorteDoAtivo(now), now, cloudstore.LoteDaRetencao}
	if depois != nil {
		sql += sqlCandidatosDepois
		args = append(args, depois.createdAt, depois.id)
	}
	rows, err := s.pool.Query(ctx, sql+sqlCandidatosOrdem, args...)
	if err != nil {
		return nil, sanitize("ler os candidatos da retenção", err)
	}
	defer rows.Close()
	var lote []candidato
	for rows.Next() {
		var c candidato
		if err := rows.Scan(&c.id, &c.createdAt, &c.deletedAt, &c.purgeAt); err != nil {
			return nil, sanitize("ler os candidatos da retenção", err)
		}
		// O cursor volta ao banco como foi lido; a decisão é em UTC.
		lote = append(lote, c)
	}
	if err := rows.Err(); err != nil {
		return nil, sanitize("ler os candidatos da retenção", err)
	}
	return lote, nil
}

// reterUm decide de novo, sobre a linha travada, e apaga um ditado candidato,
// na sua transação. Devolve o que contou (nada, se ficou ou já não existia).
func (s *Store) reterUm(ctx context.Context, userID, id string, now time.Time) (cloudstore.Retido, error) {
	if antesDaTravaDaRetencao != nil {
		antesDaTravaDaRetencao(id)
	}
	var feito cloudstore.Retido
	err := s.gravar(ctx, userID, func(tx pgx.Tx, seq int64, _ string) (bool, error) {
		feito = cloudstore.Retido{}
		cur, err := readForUpdate(ctx, tx, userID, id)
		if err != nil || cur == nil {
			return false, err
		}
		d := cloudstore.DecideRetencao(cur.CreatedAt, cur.DeletedAt, cur.PurgeAt, now)
		if d == cloudstore.RetencaoFica {
			return false, nil
		}
		lapide := d == cloudstore.RetencaoComLapide
		if err := apagarDeVez(ctx, tx, *cur, lapide, seq, nil, now); err != nil {
			return false, err
		}
		feito = d.Contou(cur.DeletedAt != nil)
		return lapide, nil
	})
	if err != nil {
		return cloudstore.Retido{}, err
	}
	return feito, nil
}

// arrumar apaga as lápides vencidas do usuário (decididas linha a linha em
// Go), o sync_event órfão vencido e a resposta guardada vencida, em lotes
// limitados, cada lote na sua transação curta que trava o contador. Só conta
// o que confirmou; com o ctx cancelado, para entre um lote e outro e devolve
// o que já fez, com o erro.
//
// Um DELETE só, sem limite, seguraria a trava do contador por um tempo
// proporcional ao que venceu desde a rodada anterior (o token com defeito da
// D3 enche applied_mutations a 5.000 mutações por minuto; o job parado dias
// acumula), e o push e o CRUD do mesmo usuário esperariam além dos prazos do
// servidor (30 s e 60 s). Com o lote, a espera é a de um lote. A trava
// continua em cada um: é ela que evita o deadlock entre o DELETE da arrumação
// e o do "apagar meus dados", que varrem os eventos em ordens diferentes.
func (s *Store) arrumar(ctx context.Context, userID string, now time.Time) (cloudstore.Retido, error) {
	var r cloudstore.Retido
	var depois *lapideLida
	for {
		var apagadas int64
		var lidas int
		var ultimo *lapideLida
		err := s.comOContador(ctx, userID, func(tx pgx.Tx) error {
			var err error
			apagadas, lidas, ultimo, err = loteDeLapides(ctx, tx, userID, now, depois)
			return err
		})
		if err != nil {
			return r, err
		}
		r.LapidesVencidas += apagadas
		if lidas < loteDasLapides {
			break
		}
		depois = ultimo
	}
	for _, d := range []struct {
		op, sql string
		corte   time.Time
		conta   *int64
	}{
		{"arrumar os eventos", sqlArrumarEventos, cloudstore.CorteDosEventos(now), &r.Eventos},
		{"arrumar os eventos das notas", sqlArrumarEventosDasNotas, cloudstore.CorteDosEventos(now), &r.Eventos},
		{"arrumar as mutações", sqlArrumarMutacoes, cloudstore.CorteDasMutacoes(now), &r.Mutacoes},
	} {
		for {
			var n int64
			err := s.comOContador(ctx, userID, func(tx pgx.Tx) error {
				tag, err := tx.Exec(ctx, d.sql, userID, d.corte, loteDaArrumacao)
				if err != nil {
					return sanitize(d.op, err)
				}
				n = tag.RowsAffected()
				return nil
			})
			if err != nil {
				return r, err
			}
			*d.conta += n
			if n < int64(loteDaArrumacao) {
				break
			}
		}
	}
	return r, nil
}

// comOContador roda f numa transação curta que trava o contador do usuário
// (a mesma trava do gravar e do "apagar meus dados") e confirma. Um usuário
// sem contador é ErrUnknownUser.
func (s *Store) comOContador(ctx context.Context, userID string, f func(tx pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return sanitize("abrir a transação", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := travarContador(ctx, tx, userID); err != nil {
		return err
	}
	if err := f(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return sanitize("confirmar", err)
	}
	if depoisDeUmLoteDaArrumacao != nil {
		depoisDeUmLoteDaArrumacao()
	}
	return nil
}

// travarContador trava a linha do contador do usuário até o fim da transação
// (a mesma trava do gravar e do "apagar meus dados").
func travarContador(ctx context.Context, tx pgx.Tx, userID string) error {
	var last int64
	if err := tx.QueryRow(ctx, sqlTravarContador, userID).Scan(&last); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cloudstore.ErrUnknownUser
		}
		return sanitize("travar o contador", err)
	}
	return nil
}

// lapideLida é o cursor das lápides: a última lida, por (dictated_at,
// entity_id).
type lapideLida struct {
	at time.Time
	id string
}

// loteDeLapides lê um lote de lápides do pré-filtro depois do cursor (nil: do
// começo) e apaga, pela chave, as que LapideVencida decide, na transação tx
// (com o contador travado). Devolve quantas apagou, quantas leu e a última
// lida. O cursor passa de uma transação para a outra: uma lápide que nasce no
// meio, atrás dele, fica para a rodada seguinte (o Reter é idempotente).
func loteDeLapides(ctx context.Context, tx pgx.Tx, userID string, now time.Time, depois *lapideLida) (int64, int, *lapideLida, error) {
	sql := sqlLapidesCandidatas
	args := []any{userID, cloudstore.CorteDaLapide(now), loteDasLapides}
	if depois != nil {
		sql += sqlLapidesDepois
		args = append(args, depois.at, depois.id)
	}
	rows, err := tx.Query(ctx, sql+sqlLapidesOrdem, args...)
	if err != nil {
		return 0, 0, nil, sanitize("ler as lápides", err)
	}
	var vencidas []string
	var ultimo *lapideLida
	lidas := 0
	for rows.Next() {
		var l lapideLida
		if err := rows.Scan(&l.id, &l.at); err != nil {
			rows.Close()
			return 0, 0, nil, sanitize("ler as lápides", err)
		}
		lidas++
		ultimo = &l
		if cloudstore.LapideVencida(l.at.UTC(), now) {
			vencidas = append(vencidas, l.id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, nil, sanitize("ler as lápides", err)
	}
	if len(vencidas) == 0 {
		return 0, lidas, ultimo, nil
	}
	tag, err := tx.Exec(ctx, sqlApagarLapides, userID, vencidas)
	if err != nil {
		return 0, 0, nil, sanitize("apagar as lápides", err)
	}
	if tag.RowsAffected() != int64(len(vencidas)) {
		// Não acontece com o contador travado: a lápide só nasce e só some
		// com ele preso.
		return 0, 0, nil, errors.New("pgstore: apagar as lápides: " + strconv.FormatInt(tag.RowsAffected(), 10) +
			" de " + strconv.Itoa(len(vencidas)))
	}
	return tag.RowsAffected(), lidas, ultimo, nil
}
