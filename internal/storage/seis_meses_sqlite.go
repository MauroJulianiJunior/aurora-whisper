package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// Os 6 meses no PC (ADR-019, "Decisões da F10"; AC-020 e AC-062): todo ditado
// do dono fixo fora da janela de 6 meses sai de vez, esteja ativo ou na
// lixeira, com o selo e os pendentes. Apaga o que hoje fica, então fica atrás
// de um PORTÃO QUE RECUSA POR PADRÃO: o único ponto que apaga
// (apagarLoteComPortao) lê o OK (ChaveRetencaoAceitaEm) dentro da transação
// que recebe e, sem ele, apaga zero, mesmo com o relógio anos à frente. Não
// existe parâmetro de "forçar" nem caminho que apague sem ler a chave.
//
// # A regra, linha a linha
//
// A decisão é !agora.Before(retention.ActiveExpiryAt(created_at)), em Go e em
// UTC, a mesma conta da nuvem (cloudstore.OutOfWindow): 31/ago vence em 3/mar.
// Nunca um corte de data como regra: o corte (retention.CorteDoAtivo) é só o
// pré-filtro do SQL, que traz candidatos a mais, nunca de menos.
//
// # Lotes por cursor
//
// Cada lote é uma transação: lê até LoteDosSeisMeses candidatos com
// (created_at, id) maior que o último LIDO (nunca o último apagado), decide
// cada um em Go e apaga por id os que venceram. O laço só termina quando uma
// leitura volta com menos de LoteDosSeisMeses linhas, nunca por "nenhum
// apagado no lote": o pré-filtro traz de propósito candidatos que ainda não
// venceram (em 1/mar, os ditados de 29 a 31/ago), e um laço que parasse ali
// pararia cedo; um cursor pelo último apagado releria os mesmos para sempre.
// Todos os lotes de uma rodada usam o mesmo agora.
//
// # O ditado que virou nota
//
// Fica fora da regra (virou_nota IS NULL no pré-filtro): o texto dele vive na
// nota, que não tem a retenção de 6 meses (ADR-022, "Ditado vira nota"). Nem a
// prévia o conta.
//
// # O que sai com o ditado, e o que fica
//
// Sai a linha, o marcador de ditado da sync_outbox e a sync_mudou_aqui; o
// local_corrections.transcription_id vira nulo pela FK (0001). Fica o
// aprendido (o marcador learned, a sync_aprendido, a sync_veio_da_nuvem) e
// toda linha de outro dono (de um Agent anterior ao dono fixo): a prévia diz
// quantas são, e elas não entram no total.

// ChaveRetencaoAceitaEm é o OK dos 6 meses (a tabela instalacao): o portão.
// Presente, a regra está ligada; quem a grava é o Ligar, na mesma transação
// do primeiro lote, e quem a tira é o Desligar e a pausa do freio (F10c).
const ChaveRetencaoAceitaEm = "retencao_6m_aceita_em"

// LoteDosSeisMeses é quantos candidatos cada transação dos 6 meses lê: o Put
// de um ditado que chegue no meio espera um lote, nunca o histórico inteiro.
const LoteDosSeisMeses = 500

// ContagemDosSeisMeses é a prévia dos 6 meses: só contagens.
type ContagemDosSeisMeses struct {
	// Total: os ditados do dono fixo fora da janela, que a regra apagaria
	// agora (ativos e na lixeira).
	Total int
	// NaLixeira: dos Total, os que estão na lixeira.
	NaLixeira int
	// ComSelo: dos Total, os que têm duas versões (o selo), que saem sem a
	// escolha de quem usa.
	ComSelo int
	// DeOutroDono: as linhas de outro dono fora da janela, que ficam (não
	// entram no Total).
	DeOutroDono int
}

// SeisMeses é a regra dos 6 meses sobre o historico.db. O dono é sempre o
// dono fixo, lido na transação: sem ele, nada roda (ErrSemDonoFixo).
type SeisMeses struct {
	db *sql.DB
	// caminhoBanco é o arquivo do historico.db: a cópia do Ligar fica ao
	// lado dele. Vazio, o Ligar não copia, e por isso não liga.
	caminhoBanco string
}

// NovosSeisMeses monta a regra sobre db (já migrado), cujo arquivo é
// caminhoBanco.
func NovosSeisMeses(db *sql.DB, caminhoBanco string) *SeisMeses {
	if db == nil {
		panic("storage: db nil")
	}
	return &SeisMeses{db: db, caminhoBanco: caminhoBanco}
}

// SeisMeses devolve a regra dos 6 meses no mesmo banco do histórico, cujo
// arquivo é caminhoBanco.
func (r *SQLiteTranscriptionRepository) SeisMeses(caminhoBanco string) *SeisMeses {
	return NovosSeisMeses(r.db, caminhoBanco)
}

// cursorDosSeisMeses é o último candidato lido: (created_at, id). O zero lê do
// começo.
type cursorDosSeisMeses struct {
	criado, id string
}

// vencidoDosSeisMeses é um candidato que venceu a janela.
type vencidoDosSeisMeses struct {
	id        string
	naLixeira bool
	comSelo   bool
}

// foraDaJanela lê um lote: até LoteDosSeisMeses candidatos do dono (ou, com
// deOutroDono, de todo user_id diferente dele) com o pré-filtro created_at <=
// CorteDoAtivo(agora) e (created_at, id) maior que depois, em ordem; decide
// cada um em Go e devolve os que venceram, quantas linhas leu e a última
// lida. É a leitura única da prévia e do apagar.
func foraDaJanela(ctx context.Context, q consulta, dono string, deOutroDono bool, agora time.Time,
	depois cursorDosSeisMeses) ([]vencidoDosSeisMeses, int, cursorDosSeisMeses, error) {
	agora = agora.UTC()
	donoSQL := `user_id = ?`
	if deOutroDono {
		donoSQL = `user_id IS NOT ?`
	}
	args := []any{dono, emTexto(retention.CorteDoAtivo(agora))}
	cursorSQL := ``
	if depois.criado != "" {
		cursorSQL = ` AND (created_at > ? OR (created_at = ? AND id > ?))`
		args = append(args, depois.criado, depois.criado, depois.id)
	}
	args = append(args, LoteDosSeisMeses)
	linhas, err := q.QueryContext(ctx, `SELECT id, created_at, deleted_at IS NOT NULL, conflito_texto IS NOT NULL
		FROM local_transcriptions WHERE `+donoSQL+` AND virou_nota IS NULL AND created_at <= ?`+cursorSQL+`
		ORDER BY created_at, id LIMIT ?`, args...)
	if err != nil {
		return nil, 0, depois, fmt.Errorf("storage: lendo o que passou dos 6 meses: %w", err)
	}
	defer linhas.Close()
	var vencidos []vencidoDosSeisMeses
	lidos := 0
	ultimo := depois
	for linhas.Next() {
		var v vencidoDosSeisMeses
		var criado string
		if err := linhas.Scan(&v.id, &criado, &v.naLixeira, &v.comSelo); err != nil {
			return nil, 0, depois, fmt.Errorf("storage: lendo o que passou dos 6 meses: %w", err)
		}
		lidos++
		ultimo = cursorDosSeisMeses{criado: criado, id: v.id}
		em, err := deTempo(criado)
		if err != nil {
			return nil, 0, depois, err
		}
		if !naJanela(em, agora) {
			vencidos = append(vencidos, v)
		}
	}
	if err := linhas.Err(); err != nil {
		return nil, 0, depois, fmt.Errorf("storage: lendo o que passou dos 6 meses: %w", err)
	}
	return vencidos, lidos, ultimo, nil
}

// PreviaDosSeisMeses conta, com agora, o que a regra apagaria: os ditados do
// dono fixo fora da janela (quantos na lixeira e quantos com o selo) e as
// linhas de outro dono que ficam. Numa transação de leitura (uma foto só),
// pelos mesmos lotes e pelo mesmo cursor do apagar. Sem o dono fixo,
// ErrSemDonoFixo.
func (s *SeisMeses) PreviaDosSeisMeses(ctx context.Context, agora time.Time) (ContagemDosSeisMeses, error) {
	var c ContagemDosSeisMeses
	err := naTransacao(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		c, err = previaDosSeisMeses(ctx, tx, agora)
		return err
	})
	return c, err
}

func previaDosSeisMeses(ctx context.Context, tx *sql.Tx, agora time.Time) (ContagemDosSeisMeses, error) {
	dono, err := lerDonoFixo(ctx, tx)
	if err != nil {
		return ContagemDosSeisMeses{}, err
	}
	var c ContagemDosSeisMeses
	for _, outro := range []bool{false, true} {
		var depois cursorDosSeisMeses
		for {
			vencidos, lidos, ultimo, err := foraDaJanela(ctx, tx, dono, outro, agora, depois)
			if err != nil {
				return ContagemDosSeisMeses{}, err
			}
			for _, v := range vencidos {
				if outro {
					c.DeOutroDono++
					continue
				}
				c.Total++
				if v.naLixeira {
					c.NaLixeira++
				}
				if v.comSelo {
					c.ComSelo++
				}
			}
			if lidos < LoteDosSeisMeses {
				break
			}
			depois = ultimo
		}
	}
	return c, nil
}

// apagarLoteComPortao é o ÚNICO ponto que apaga pelos 6 meses: lê o OK
// (ChaveRetencaoAceitaEm) e o dono fixo DENTRO da transação que recebe; sem o
// OK, não lê nem apaga nada (zero, e fim). Com ele, lê um lote depois do
// cursor (foraDaJanela) e apaga por id os do dono que venceram. Devolve
// quantos apagou, quantos leu (menos de LoteDosSeisMeses é o fim) e o último
// lido, de onde o lote seguinte continua.
func apagarLoteComPortao(ctx context.Context, tx *sql.Tx, agora time.Time,
	depois cursorDosSeisMeses) (apagados, lidos int, ultimo cursorDosSeisMeses, err error) {
	if _, aceito, err := lerChave(ctx, tx, ChaveRetencaoAceitaEm); err != nil || !aceito {
		return 0, 0, depois, err
	}
	dono, err := lerDonoFixo(ctx, tx)
	if err != nil {
		return 0, 0, depois, err
	}
	vencidos, lidos, ultimo, err := foraDaJanela(ctx, tx, dono, false, agora, depois)
	if err != nil {
		return 0, 0, depois, err
	}
	for _, v := range vencidos {
		if err := apagarDitado(ctx, tx, v.id); err != nil {
			return 0, 0, depois, fmt.Errorf("storage: apagando o que passou dos 6 meses: %w", err)
		}
	}
	return len(vencidos), lidos, ultimo, nil
}

// ApagarForaDaJanela apaga, com agora (o mesmo em todos os lotes), os ditados
// do dono fixo fora da janela, lote a lote, cada lote na sua transação, pelo
// portão (apagarLoteComPortao): sem o OK gravado, zero. Devolve quantos
// apagou.
func (s *SeisMeses) ApagarForaDaJanela(ctx context.Context, agora time.Time) (int, error) {
	return s.apagarDesde(ctx, agora, cursorDosSeisMeses{}, 0)
}

// apagarDesde continua o apagar a partir do cursor depois, somando a feitos.
func (s *SeisMeses) apagarDesde(ctx context.Context, agora time.Time, depois cursorDosSeisMeses, feitos int) (int, error) {
	for {
		var apagados, lidos int
		var ultimo cursorDosSeisMeses
		err := naTransacao(ctx, s.db, func(tx *sql.Tx) error {
			var err error
			apagados, lidos, ultimo, err = apagarLoteComPortao(ctx, tx, agora, depois)
			return err
		})
		if err != nil {
			return feitos, err
		}
		feitos += apagados
		if lidos < LoteDosSeisMeses {
			return feitos, nil
		}
		depois = ultimo
	}
}
