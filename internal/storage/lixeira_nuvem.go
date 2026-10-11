package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// A guarda da D3 na lixeira do PC (ADR-019, "Decisões da F10", D3, a correção
// da D4 da F9): a lixeira só sai de vez depois de a nuvem saber dela, no
// máximo até o ditado completar 6 meses.
//
// O PurgeExpired lê os candidatos (purge_at vencido), decide item a item aqui
// (esperaANuvem, pura) e apaga só os liberados. O resultado é sempre um
// subconjunto do que o expurgo apagava antes, então não pede OK.
//
// É uma guarda NEGATIVA: o PC segura só o que "a nuvem pode ter e ainda não
// soube". Ler a D4 da F9 ao pé da letra ("server_version e sem marcador
// pendente") prenderia para sempre a lixeira do PC que nunca conectou, a do
// ditado que a nuvem nunca terá e tudo o que vem depois de um "Esquecer esta
// nuvem" ou de um epoch novo (os dois zeram o server_version), contra o AC-019.

// ChaveRetencaoRodadaEm é a última rodada do ciclo do expurgo (a tabela
// instalacao; ADR-019, "Decisões da F10"): o freio compara o agora com ela
// (retention.FreioDaRodada). Quem a grava é o ciclo (F10c), ao fim de toda
// rodada, e o Ligar dos 6 meses; o Desligar e a pausa a tiram. Sem ela, nada
// freia. Não começa por "nuvem_": o Esquecer não a apaga, e o acessor da
// nuvem (Instalacao) não a alcança.
const ChaveRetencaoRodadaEm = "retencao_6m_rodada_em"

// ResultadoDoExpurgo é o que uma rodada do expurgo da lixeira fez. Só
// contagens.
type ResultadoDoExpurgo struct {
	// Apagados: os ditados apagados de vez.
	Apagados int
	// EsperandoANuvem: os ditados com o prazo da lixeira vencido que ficaram,
	// porque a nuvem pode tê-los e ainda não soube da lixeira (a D3).
	EsperandoANuvem int
}

// itemDaLixeira é o que a guarda lê de um ditado com o prazo da lixeira
// vencido, na transação do expurgo.
type itemDaLixeira struct {
	criadoEm time.Time
	// naLixeiraEm é o deleted_at; nil numa linha com purge_at e sem
	// deleted_at (nada grava assim; só um banco mexido à mão).
	naLixeiraEm *time.Time
	// comVersao: a linha tem server_version (a nuvem já a confirmou).
	comVersao bool
	situacao  string
	// comMarcador: há um marcador dele na sync_outbox (de ditado), parado,
	// em voo, a repetir ou de reconciliação.
	comMarcador bool
	// anotado: ele está na sync_mudou_aqui (a pendência que o Esquecer
	// anotou).
	anotado bool
}

// esperaANuvem diz se um ditado com o prazo da lixeira vencido FICA (true),
// porque a nuvem pode tê-lo e ainda não soube da lixeira. Fica quando, ao
// mesmo tempo:
//   - ele não é recusado nem fora_da_janela (a nuvem já disse que não o quer,
//     ou que ele não vale mais lá: segurar não ajuda);
//   - ele está dentro da janela de 6 meses (naJanela, em UTC): é o teto da
//     D3. Depois dela, a nuvem recusa o push e já apagou o ativo, então
//     esperar não avisa nada. Na rodada em que o freio dispara (o Agent ficou
//     parado mais de 7 dias, ou o relógio pulou para a frente), o teto não
//     vale: o item fica mais uma rodada, e o sync da subida fala com a nuvem
//     antes;
//   - e cumpre pelo menos uma de:
//   - P1: há um marcador dele na fila;
//   - P2: ele está na sync_mudou_aqui E não tem server_version (estava
//     pendente na hora do Esquecer; com a versão, a nuvem já o
//     reconciliou, e a anotação, que só o próximo Esquecer ou o Recomeçar
//     limpam, não o prende);
//   - P3: há esquecidaEm (o instante do Esquecer), e ele não tem
//     server_version, foi criado até ele e foi para a lixeira depois dele
//     (a lixeira feita depois do Esquecer num ditado que a nuvem esquecida
//     pode ter).
//
// Pura: tudo o que ela olha foi lido na mesma transação que apaga.
func esperaANuvem(it itemDaLixeira, esquecidaEm *time.Time, agora time.Time, freio bool) bool {
	if it.situacao == SituacaoRecusado || it.situacao == SituacaoForaDaJanela {
		return false
	}
	if !freio && !naJanela(it.criadoEm, agora) {
		return false
	}
	p1 := it.comMarcador
	p2 := it.anotado && !it.comVersao
	p3 := esquecidaEm != nil && !it.comVersao && !it.criadoEm.After(*esquecidaEm) &&
		it.naLixeiraEm != nil && it.naLixeiraEm.After(*esquecidaEm)
	return p1 || p2 || p3
}

// freioDaRodada lê, na transação, a última rodada do ciclo e diz se o freio
// dispara em agora. Um valor ilegível conta como freio: segurar uma rodada a
// mais nunca apaga cedo, e a rodada seguinte do ciclo grava o valor certo.
func freioDaRodada(ctx context.Context, q consulta, agora time.Time) (bool, error) {
	v, ok, err := lerChave(ctx, q, ChaveRetencaoRodadaEm)
	if err != nil || !ok {
		return false, err
	}
	em, err := deTempo(v)
	if err != nil {
		return true, nil
	}
	return retention.FreioDaRodada(&em, agora), nil
}

// expurgarLixeira é o PurgeExpired com a guarda, numa transação só (a do
// chamador): (1) lê o instante do Esquecer, o freio e os candidatos, com o
// marcador e a anotação de cada um; (2) decide item a item (esperaANuvem);
// (3) apaga por id, dos liberados, o marcador de ditado, a anotação e a
// linha. Ler e apagar na mesma transação fecha a corrida com o push e com o
// Aplicar, que rodam em transações separadas pela mesma conexão. O marcador
// do aprendido (learned) nunca é tocado.
func expurgarLixeira(ctx context.Context, tx *sql.Tx, now time.Time) (ResultadoDoExpurgo, error) {
	agora := now.UTC()
	esquecidaEm, err := nuvemEsquecidaEm(ctx, tx)
	if err != nil {
		return ResultadoDoExpurgo{}, err
	}
	freio, err := freioDaRodada(ctx, tx, agora)
	if err != nil {
		return ResultadoDoExpurgo{}, err
	}
	linhas, err := tx.QueryContext(ctx, `SELECT t.id, t.created_at, t.deleted_at, t.server_version IS NOT NULL, t.sync_state,
		  EXISTS (SELECT 1 FROM sync_outbox o WHERE o.entity_type = ? AND o.entity_id = t.id),
		  EXISTS (SELECT 1 FROM sync_mudou_aqui m WHERE m.entity_type = ? AND m.entity_id = t.id)
		FROM local_transcriptions t WHERE t.purge_at IS NOT NULL AND t.purge_at <= ?`,
		EntidadeDitado, EntidadeDitado, emTexto(agora))
	if err != nil {
		return ResultadoDoExpurgo{}, fmt.Errorf("storage: lendo a lixeira vencida: %w", err)
	}
	var liberados []string
	var r ResultadoDoExpurgo
	for linhas.Next() {
		var id, criado string
		var naLixeira sql.NullString
		var it itemDaLixeira
		if err := linhas.Scan(&id, &criado, &naLixeira, &it.comVersao, &it.situacao, &it.comMarcador, &it.anotado); err != nil {
			linhas.Close()
			return ResultadoDoExpurgo{}, fmt.Errorf("storage: lendo a lixeira vencida: %w", err)
		}
		if it.criadoEm, err = deTempo(criado); err != nil {
			linhas.Close()
			return ResultadoDoExpurgo{}, err
		}
		if it.naLixeiraEm, err = deTempoNulo(naLixeira); err != nil {
			linhas.Close()
			return ResultadoDoExpurgo{}, err
		}
		if esperaANuvem(it, esquecidaEm, agora, freio) {
			r.EsperandoANuvem++
			continue
		}
		liberados = append(liberados, id)
	}
	if err := linhas.Err(); err != nil {
		linhas.Close()
		return ResultadoDoExpurgo{}, fmt.Errorf("storage: lendo a lixeira vencida: %w", err)
	}
	linhas.Close()
	for _, id := range liberados {
		if err := apagarDitado(ctx, tx, id); err != nil {
			return ResultadoDoExpurgo{}, fmt.Errorf("storage: expurgando a lixeira: %w", err)
		}
		r.Apagados++
	}
	return r, nil
}

// apagarDitado apaga de vez um ditado por id, na transação: o marcador de
// ditado dele (nunca o do aprendido), a anotação do Esquecer e a linha. O
// local_corrections.transcription_id vira nulo pela FK (0001).
func apagarDitado(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, EntidadeDitado, id); err != nil {
		return fmt.Errorf("tirando o ditado da fila: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sync_mudou_aqui WHERE entity_type = ? AND entity_id = ?`, EntidadeDitado, id); err != nil {
		return fmt.Errorf("tirando a anotação do ditado: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM local_transcriptions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("apagando o ditado: %w", err)
	}
	return nil
}
