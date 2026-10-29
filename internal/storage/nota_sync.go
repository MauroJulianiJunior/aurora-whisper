package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// O sync das notas e dos marcadores de nota no PC (ADR-022, "Sync", fase N7),
// no molde do ditado (fila_sqlite.go) e do aprendido (aprendido_sqlite.go):
//
//   - a emissão: toda gravação local de nota ou marcador põe o marcador da
//     entidade na sync_outbox na MESMA transação (emitirNota,
//     emitirMarcadorDeNota; o apagar de vez, lapideLocal);
//   - a fila: o que o motor (internal/sync) lê para o push
//     (PendentesDeNotas, PendentesDeMarcadoresDeNota), a completação de cada
//     rodada (CompletarNotas) e a capacidade (VerCapacidade);
//   - a resposta da nuvem (push e pull), aplicada por AplicarTudo na mesma
//     transação dos ditados, do aprendido e do cursor, sem nunca pôr
//     marcador;
//   - o recomeço do epoch novo e o "Esquecer esta nuvem".
//
// Nada de texto no log: as mensagens de erro daqui só dizem o que falhou.

// Os entity_type das notas e dos marcadores de nota no sync (o servidor fala
// inglês, como "transcription" e "learned").
const (
	EntidadeNota           = "note"
	EntidadeMarcadorDeNota = "label"
)

// Os campos do marcador da nota (sync_outbox.campos), em bits: o que mudou
// desde a última versão que a nuvem confirmou. Criar não é bit: a nota com
// server_version nulo sobe inteira.
const (
	CampoNotaConteudo   = 1  // o título e o corpo, juntos
	CampoNotaLixeira    = 2  // entrar ou sair da lixeira
	CampoNotaCor        = 4  // a cor
	CampoNotaFixada     = 8  // fixar, desafixar
	CampoNotaArquivada  = 16 // arquivar, desarquivar
	CampoNotaMarcadores = 32 // o conjunto dos marcadores
	CampoNotaPosicao    = 64 // a ordem manual
	// CampoApagado é a lápide local da nota e do marcador de nota: apagar de
	// vez (a mutação delete, sem linha). Sozinho no marcador: substitui os
	// outros bits.
	CampoApagado = 128
	// CampoMarcadorNome é o nome do marcador de nota (o único campo dele).
	CampoMarcadorNome = 1
)

// camposDaReconciliacaoDaNota são os bits do marcador que a reconciliação do
// pull põe (MarcadorCriar): todos os campos da nota. Não mudam o envio (sem
// server_version, a nota sobe inteira), mas dizem que a nuvem TEM a nota: a
// lixeira feita aqui antes do envio não a tira da fila (lixeiraDaNotaNaFila),
// como os dois campos do MarcadorCriar do ditado.
const camposDaReconciliacaoDaNota = CampoNotaConteudo | CampoNotaLixeira | CampoNotaCor | CampoNotaFixada |
	CampoNotaArquivada | CampoNotaMarcadores | CampoNotaPosicao

// ChaveNuvemNotas é a capacidade das notas (a tabela instalacao, com a hora
// em que o PC a viu): a nuvem disse, no pull, que conhece notas e marcadores.
// Sem ela, nada de nota nem de marcador sobe, e o bit CampoVirouNota do
// ditado fica parado na fila (um deploy na ordem errada não estraga nada). O
// "Esquecer esta nuvem" a apaga.
const ChaveNuvemNotas = "nuvem_notas"

// SituacaoAguardando é a situação do "marcador que espera" (local_labels): o
// id que uma nota da nuvem cita e que o PC ainda não recebeu. A linha tem
// deleted_at preenchido e nome vazio, então nenhuma lista a vê; vira o
// marcador de verdade quando ele chega (o Gravar do marcador). Nunca põe
// marcador na fila.
const SituacaoAguardando = "aguardando"

// ─── A emissão (na transação de quem grava) ───

// naFila anota, numa gravação, se algum marcador entrou na fila (para o
// aviso depois do commit).
type naFila struct{ emitiu bool }

func (q *naFila) anotar(emitiu bool, err error) error {
	if err != nil {
		return err
	}
	if emitiu {
		q.emitiu = true
	}
	return nil
}

// emitirNota põe o marcador da nota id na fila, ou junta os campos ao que já
// está lá (campos | novos), na transação da gravação. Quando, lido na própria
// transação: a nota tem server_version (a nuvem a tem: conectado ou não), OU
// a nuvem está conectada (o aparelho na instalacao): conectado, tudo entra;
// desconectado, só o que a nuvem já tem (o resto sobe pela completação,
// CompletarNotas, depois do aceite). Diferente do ditado, os bits valem também
// numa nota que a nuvem ainda não tem: com o envio de criar no voo, é por
// eles que a mudança feita no meio sobe depois da resposta. Devolve se o
// marcador foi posto ou mudou.
func emitirNota(ctx context.Context, tx *sql.Tx, id string, campos int, agora time.Time) (bool, error) {
	return emitirDaTabela(ctx, tx, EntidadeNota, "local_notes", id, campos, agora)
}

// emitirMarcadorDeNota é emitirNota para o marcador (rótulo) de nota. O
// "marcador que espera" (SituacaoAguardando) nunca entra.
func emitirMarcadorDeNota(ctx context.Context, tx *sql.Tx, id string, campos int, agora time.Time) (bool, error) {
	return emitirDaTabela(ctx, tx, EntidadeMarcadorDeNota, "local_labels", id, campos, agora)
}

// emitirDaTabela é o miolo das duas: tabela vem do código, nunca de fora.
func emitirDaTabela(ctx context.Context, tx *sql.Tx, entidade, tabela, id string, campos int, agora time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em)
		SELECT ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM `+tabela+` WHERE id = ? AND sync_state <> ?
		  AND (server_version IS NOT NULL OR EXISTS (SELECT 1 FROM instalacao WHERE nome = ? AND valor <> '')))
		ON CONFLICT (entity_type, entity_id) DO UPDATE SET campos = campos | excluded.campos`,
		entidade, id, campos, emTexto(agora), id, SituacaoAguardando, ChaveNuvemAparelho)
	if err != nil {
		return false, fmt.Errorf("storage: pondo a %s na fila do sync: %w", entidade, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: pondo a %s na fila do sync: %w", entidade, err)
	}
	return n > 0, nil
}

// lapideLocal é o marcador do apagar de vez (a nota do ApagarDeVez, do
// expurgo e do Desfazer do "Virar nota"; o marcador de nota do
// ApagarMarcador), na transação de quem apaga, DEPOIS de a linha sair (ou de
// o marcador ir para o soft delete):
//   - a nuvem nunca teve a entidade (temVersao falso) e o envio dela não está
//     no voo: o marcador sai, e nada sobe;
//   - senão (a nuvem a tem, ou pode ter recebido o envio em voo), o marcador
//     fica com campos = CampoApagado (a lápide local, até a nuvem responder),
//     o envio em curso limpo (a linha sumiu, e o envio de antes não pode ser
//     repetido: o próximo leva outro mutation_id) e sem escolha de selo.
//     Conectado ou não: a nuvem que tem a entidade precisa saber.
//
// Devolve se o marcador ficou.
func lapideLocal(ctx context.Context, tx *sql.Tx, entidade, id string, temVersao bool, agora time.Time) (bool, error) {
	if !temVersao {
		var emVoo int
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM sync_outbox WHERE entity_type = ? AND entity_id = ? AND mutation_id IS NOT NULL)`,
			entidade, id).Scan(&emVoo); err != nil {
			return false, fmt.Errorf("storage: lendo o envio da %s: %w", entidade, err)
		}
		if emVoo == 0 {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, entidade, id); err != nil {
				return false, fmt.Errorf("storage: tirando da fila a %s apagada: %w", entidade, err)
			}
			return false, nil
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES (?, ?, ?, ?)
		 ON CONFLICT (entity_type, entity_id) DO UPDATE SET campos = excluded.campos, mutation_id = NULL,
		   versao_enviada = NULL, inicial = 0, selo_escolha = NULL, selo_em = NULL`,
		entidade, id, CampoApagado, emTexto(agora)); err != nil {
		return false, fmt.Errorf("storage: pondo a lápide da %s na fila do sync: %w", entidade, err)
	}
	return true, nil
}

// lixeiraDaNotaNaFila é o marcador do SoftDelete da nota, como o do ditado
// (mudarLixeira): numa nota que a nuvem tem, o bit da lixeira (emitirNota).
// Numa que ela não tem, o marcador de criar parado (fora do voo, e que não é
// o da reconciliação) sai: a lixeira de uma nota que nunca subiu não sobe (e
// a completação pula a lixeira). O em voo e o da reconciliação ficam e
// ganham o bit (a nuvem pode ter, ou tem, a nota). Nenhum marcador novo
// nasce aqui para a nota que a nuvem não tem.
func lixeiraDaNotaNaFila(ctx context.Context, tx *sql.Tx, id string, agora time.Time) (bool, error) {
	var versao sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT server_version FROM local_notes WHERE id = ?`, id).Scan(&versao); err != nil {
		return false, fmt.Errorf("storage: lendo a versão da nota: %w", err)
	}
	if versao.Valid {
		return emitirNota(ctx, tx, id, CampoNotaLixeira, agora)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ? AND mutation_id IS NULL AND campos <> ?`,
		EntidadeNota, id, camposDaReconciliacaoDaNota); err != nil {
		return false, fmt.Errorf("storage: tirando da fila a nota que foi para a lixeira: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE sync_outbox SET campos = campos | ? WHERE entity_type = ? AND entity_id = ?`,
		CampoNotaLixeira, EntidadeNota, id)
	if err != nil {
		return false, fmt.Errorf("storage: pondo a lixeira da nota na fila do sync: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: pondo a lixeira da nota na fila do sync: %w", err)
	}
	return n > 0, nil
}

// lerIDs lê uma coluna de ids (a primeira) da consulta dada.
func lerIDs(ctx context.Context, q consulta, consultaSQL string, args ...any) ([]string, error) {
	linhas, err := q.QueryContext(ctx, consultaSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: lendo ids: %w", err)
	}
	defer linhas.Close()
	var ids []string
	for linhas.Next() {
		var id string
		if err := linhas.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: lendo ids: %w", err)
		}
		ids = append(ids, id)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo ids: %w", err)
	}
	return ids, nil
}

// ─── A escolha do selo ───

func (r *SQLiteNotaRepository) ResolverConflitoDaNota(ctx context.Context, userID, id string, baseVersion int64, escolha string) (Nota, error) {
	if escolha != EscolhaManter && escolha != EscolhaTrocar {
		return Nota{}, ErrEscolhaInvalida
	}
	agora := r.now()
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		var versao int64
		var selo, seloEm sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT version, conflito_titulo, conflito_em FROM local_notes WHERE id = ? AND user_id = ?`,
			id, userID).Scan(&versao, &selo, &seloEm)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo o selo da nota: %w", err)
		}
		if versao != baseVersion || !selo.Valid {
			return ErrVersionConflict
		}
		titulo, corpo := `titulo`, `corpo`
		if escolha == EscolhaTrocar {
			// O lado direito do SET lê a linha de antes: o texto do selo.
			titulo, corpo = `conflito_titulo`, `COALESCE(conflito_corpo, '')`
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE local_notes SET titulo = `+titulo+`, corpo = `+corpo+`, conflito_titulo = NULL, conflito_corpo = NULL,
			   conflito_device_id = NULL, conflito_em = NULL, version = version + 1, updated_at = ?
			 WHERE id = ? AND user_id = ? AND version = ?`,
			emTexto(agora), id, userID, baseVersion)
		if err != nil {
			return fmt.Errorf("storage: escolhendo a versão do selo da nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrVersionConflict
		}
		// Como no ditado: só a escolha vai (o texto não; a nuvem troca ela
		// mesma, e só se o selo ainda é o mesmo), também desconectado, numa
		// nota que a nuvem tem. Sem a versão dela, a escolha é só daqui.
		res, err = tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, selo_escolha, selo_em)
			SELECT ?, ?, 0, ?, ?, ?
			WHERE EXISTS (SELECT 1 FROM local_notes WHERE id = ? AND server_version IS NOT NULL)
			ON CONFLICT (entity_type, entity_id) DO UPDATE SET selo_escolha = excluded.selo_escolha, selo_em = excluded.selo_em`,
			EntidadeNota, id, emTexto(agora), escolha, textoNulo(deTexto(seloEm)), id)
		if err != nil {
			return fmt.Errorf("storage: pondo a escolha do selo da nota na fila do sync: %w", err)
		}
		n, err := res.RowsAffected()
		return fila.anotar(n > 0, err)
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

// ─── A capacidade ───

// temCapacidadeDeNotas diz se a nuvem já disse que conhece notas
// (ChaveNuvemNotas).
func temCapacidadeDeNotas(ctx context.Context, q consulta) (bool, error) {
	_, ok, err := lerChave(ctx, q, ChaveNuvemNotas)
	return ok, err
}

// TemCapacidadeDeNotas diz se a nuvem desta conexão já disse que conhece
// notas (o motor: só então o payload do ditado leva o virou_nota).
func (f *Fila) TemCapacidadeDeNotas(ctx context.Context) (bool, error) {
	return temCapacidadeDeNotas(ctx, f.db)
}

// VerCapacidade é o que o pull viu em "entidades" (temNotas: a lista tem
// "note"), numa transação:
//   - com notas, na primeira vez (a chave ainda não existe): grava a chave
//     com a hora e põe o bit CampoVirouNota em todo ditado que a nuvem tem
//     (server_version) e que virou uma nota de verdade (o id, e não o
//     "nova:<token>" da caixa): antes da capacidade, o virou_nota não subia,
//     e o marcador que só o tinha ficou parado. Devolve primeira: o motor
//     zera o cursor (um binário antigo pode ter passado por notas sem as
//     aplicar);
//   - com notas, de novo: nada;
//   - sem notas (a nuvem voltou a uma versão que não as conhece): tira a
//     chave, e nada de nota sobe até ela voltar (e voltar é outra primeira
//     vez).
func (f *Fila) VerCapacidade(ctx context.Context, temNotas bool, agora time.Time) (primeira bool, err error) {
	err = naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		primeira = false
		tem, err := temCapacidadeDeNotas(ctx, tx)
		if err != nil {
			return err
		}
		if !temNotas {
			if tem {
				if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome = ?`, ChaveNuvemNotas); err != nil {
					return fmt.Errorf("storage: tirando a capacidade das notas: %w", err)
				}
			}
			return nil
		}
		if tem {
			return nil
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`,
			ChaveNuvemNotas, emTexto(agora)); err != nil {
			return fmt.Errorf("storage: gravando a capacidade das notas: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em)
			 SELECT ?, id, ?, ? FROM local_transcriptions
			 WHERE user_id = ? AND server_version IS NOT NULL AND virou_nota IS NOT NULL
			   AND virou_nota NOT LIKE ? AND sync_state <> ?
			 ON CONFLICT (entity_type, entity_id) DO UPDATE SET campos = campos | excluded.campos`,
			EntidadeDitado, CampoVirouNota, emTexto(agora), f.dono, PrefixoDaNotaNova+"%", SituacaoForaDaJanela); err != nil {
			return fmt.Errorf("storage: pondo na fila os ditados que viraram nota: %w", err)
		}
		primeira = true
		return nil
	})
	return primeira, err
}

// ─── O push ───

// ItemDeNotaNaFila é um marcador de nota com a nota, lida na hora de enviar.
// Linha é nil só na lápide (CampoApagado): a mutação é delete.
type ItemDeNotaNaFila struct {
	Marcador Marcador
	Linha    *Nota
}

// ItemDeMarcadorNaFila é um marcador da fila de um marcador (rótulo) de nota,
// com a linha. Linha é nil só na lápide (CampoApagado).
type ItemDeMarcadorNaFila struct {
	Marcador Marcador
	Linha    *MarcadorDeNota
}

// PendentesDeNotas devolve até limite marcadores de nota com a nota de cada
// um, os em voo primeiro, depois o resto na ordem em que entraram. Vazia sem
// a capacidade (ChaveNuvemNotas): os marcadores esperam na fila. A lápide
// (CampoApagado) volta sem linha; o marcador sem linha e sem lápide (a nota
// sumiu sem passar pela lápide: só um banco mexido à mão) sai da fila aqui
// mesmo. Os marcadores da nota (Linha.Marcadores) incluem os "que esperam"
// (o conjunto que a nuvem mandou), em ordem de id.
func (f *Fila) PendentesDeNotas(ctx context.Context, limite int) ([]ItemDeNotaNaFila, error) {
	if limite <= 0 {
		return nil, nil
	}
	var itens []ItemDeNotaNaFila
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		itens = nil
		if ok, err := temCapacidadeDeNotas(ctx, tx); err != nil || !ok {
			return err
		}
		ms, err := lerMarcadores(ctx, tx,
			`WHERE entity_type = ? ORDER BY mutation_id IS NULL, inicial, criado_em, entity_id LIMIT ?`, EntidadeNota, limite)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if m.Campos&CampoApagado != 0 {
				itens = append(itens, ItemDeNotaNaFila{Marcador: m})
				continue
			}
			n, err := f.notaParaSync(ctx, tx, m.EntityID)
			if err != nil {
				return err
			}
			if n == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`,
					EntidadeNota, m.EntityID); err != nil {
					return fmt.Errorf("storage: tirando da fila uma nota que não está aqui: %w", err)
				}
				continue
			}
			itens = append(itens, ItemDeNotaNaFila{Marcador: m, Linha: n})
		}
		return nil
	})
	return itens, err
}

// PendentesDeMarcadoresDeNota é PendentesDeNotas para os marcadores
// (rótulos) de nota. O "que espera" nunca tem marcador; se tiver (um banco
// mexido à mão), o marcador sai.
func (f *Fila) PendentesDeMarcadoresDeNota(ctx context.Context, limite int) ([]ItemDeMarcadorNaFila, error) {
	if limite <= 0 {
		return nil, nil
	}
	var itens []ItemDeMarcadorNaFila
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		itens = nil
		if ok, err := temCapacidadeDeNotas(ctx, tx); err != nil || !ok {
			return err
		}
		ms, err := lerMarcadores(ctx, tx,
			`WHERE entity_type = ? ORDER BY mutation_id IS NULL, inicial, criado_em, entity_id LIMIT ?`,
			EntidadeMarcadorDeNota, limite)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if m.Campos&CampoApagado != 0 {
				itens = append(itens, ItemDeMarcadorNaFila{Marcador: m})
				continue
			}
			l, err := f.marcadorParaSync(ctx, tx, m.EntityID)
			if err != nil {
				return err
			}
			if l == nil {
				if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`,
					EntidadeMarcadorDeNota, m.EntityID); err != nil {
					return fmt.Errorf("storage: tirando da fila um marcador que não está aqui: %w", err)
				}
				continue
			}
			itens = append(itens, ItemDeMarcadorNaFila{Marcador: m, Linha: l})
		}
		return nil
	})
	return itens, err
}

// notaParaSync lê a nota do dono como o sync a vê, ou nil: os marcadores são
// os vivos e os "que esperam" (o conjunto que a nuvem conhece), em ordem de
// id, como a nuvem os devolve.
func (f *Fila) notaParaSync(ctx context.Context, tx *sql.Tx, id string) (*Nota, error) {
	n, err := lerNota(tx.QueryRowContext(ctx,
		`SELECT `+colunasNota+` FROM local_notes WHERE id = ? AND user_id = ?`, id, f.dono))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: lendo a nota: %w", err)
	}
	ms, err := lerIDs(ctx, tx, `SELECT nl.label_id FROM local_note_labels nl JOIN local_labels l ON l.id = nl.label_id
		WHERE nl.note_id = ? AND l.user_id = ? AND (l.deleted_at IS NULL OR l.sync_state = ?)
		ORDER BY nl.label_id`, id, f.dono, SituacaoAguardando)
	if err != nil {
		return nil, err
	}
	if ms != nil {
		n.Marcadores = ms
	}
	return &n, nil
}

// marcadorParaSync lê o marcador de nota do dono, ou nil (também para o "que
// espera": para o sync, ele não está aqui).
func (f *Fila) marcadorParaSync(ctx context.Context, tx *sql.Tx, id string) (*MarcadorDeNota, error) {
	m, err := lerMarcador(tx.QueryRowContext(ctx,
		`SELECT `+colunasMarcador+` FROM local_labels WHERE id = ? AND user_id = ? AND sync_state <> ?`,
		id, f.dono, SituacaoAguardando))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: lendo o marcador: %w", err)
	}
	return &m, nil
}

// CompletarNotas é o envio inicial das notas e dos marcadores, em toda rodada
// (sem cópia nem pergunta nova: o clique em "Enviar o histórico" já é o
// aceite da pessoa). Com o aceite (ChaveNuvemEnvioAceitoEm) e a capacidade
// (ChaveNuvemNotas), põe o marcador de criar (campos 0), numa transação, em
// todo marcador de nota vivo e em toda nota fora da lixeira que a nuvem não
// tem (server_version nulo), sem recusa e sem marcador. Ficam de fora a
// lixeira (como no histórico) e o "que espera" (deleted_at preenchido).
// Devolve quantos marcou.
func (f *Fila) CompletarNotas(ctx context.Context, agora time.Time) (int, error) {
	marcados := 0
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		marcados = 0
		if _, aceito, err := lerChave(ctx, tx, ChaveNuvemEnvioAceitoEm); err != nil || !aceito {
			return err
		}
		if ok, err := temCapacidadeDeNotas(ctx, tx); err != nil || !ok {
			return err
		}
		for _, alvo := range []struct{ entidade, tabela string }{
			{EntidadeMarcadorDeNota, "local_labels"}, {EntidadeNota, "local_notes"},
		} {
			res, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em)
				SELECT ?, x.id, 0, ? FROM `+alvo.tabela+` x
				WHERE x.user_id = ? AND x.deleted_at IS NULL AND x.server_version IS NULL AND x.sync_state = ?
				  AND NOT EXISTS (SELECT 1 FROM sync_outbox o WHERE o.entity_type = ? AND o.entity_id = x.id)
				ORDER BY x.created_at, x.id
				ON CONFLICT (entity_type, entity_id) DO NOTHING`,
				alvo.entidade, emTexto(agora), f.dono, SituacaoNormal, alvo.entidade)
			if err != nil {
				return fmt.Errorf("storage: pondo as notas na fila do sync: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("storage: pondo as notas na fila do sync: %w", err)
			}
			marcados += int(n)
		}
		return nil
	})
	return marcados, err
}

// ─── A resposta da nuvem (o push e o pull) ───

// EstadoDaNota é o que o PC sabe de uma nota na hora de decidir o que fazer
// com uma resposta da nuvem.
type EstadoDaNota struct {
	ID string
	// Linha é a nota deste PC (como o sync a vê: notaParaSync); nil se não
	// existe aqui.
	Linha *Nota
	// Marcador é o da fila; nil sem marcador.
	Marcador *Marcador
}

// AcaoDaNota é o que uma decisão do sync manda fazer com uma nota, executada
// por AplicarTudo na transação da resposta. O zero não faz nada.
type AcaoDaNota struct {
	// Gravar é o estado da nuvem: insere a nota, ou muda todos os campos dela
	// (o título, o corpo, a cor, fixada, arquivada, a posição, a lixeira e o
	// prazo do expurgo, a origem, a hora de edição, o selo e o conjunto dos
	// marcadores), com corpo_nuvem = o corpo e a versão local subindo um.
	// Nunca põe marcador: aplicar não ecoa.
	Gravar *Nota
	// ServerVersion grava a versão da nuvem que o PC passa a conhecer.
	ServerVersion *int64
	// ZerarServerVersion: o PC não conhece versão nenhuma (o próximo envio
	// sobe a nota inteira).
	ZerarServerVersion bool
	// CorpoNuvemAtual: corpo_nuvem passa a ser o corpo atual da linha (o
	// push aplicado, sem mudança no voo: o corpo enviado é o que está aqui).
	CorpoNuvemAtual bool
	// EsquecerCorpoNuvem: corpo_nuvem volta a nulo (o PC não sabe mais o
	// corpo da versão da nuvem).
	EsquecerCorpoNuvem bool
	// ApagarLinha apaga a nota, as ligações dela e o marcador da fila (a
	// lápide que veio da nuvem, o gone "apagado").
	ApagarLinha bool
	// Situacao, se não vazia, grava a situação da nota (SituacaoNormal,
	// SituacaoRecusado); Erro é o código da recusa.
	Situacao string
	Erro     string
	// Marcador é MarcadorFica, MarcadorSai, MarcadorReenviar ou
	// MarcadorCriar, como no ditado.
	Marcador int
}

// EstadoDoMarcadorDeNota é o que o PC sabe de um marcador de nota na hora de
// decidir. O "que espera" não conta como linha (Linha nil).
type EstadoDoMarcadorDeNota struct {
	ID       string
	Linha    *MarcadorDeNota
	Marcador *Marcador
}

// AcaoDoMarcadorDeNota é AcaoDaNota para o marcador de nota. Gravar insere
// ou atualiza o nome (e tira o "que espera"); um nome que colide com outro
// marcador vivo daqui ganha " (2)", " (3)"... só aqui.
type AcaoDoMarcadorDeNota struct {
	Gravar             *MarcadorDeNota
	ServerVersion      *int64
	ZerarServerVersion bool
	ApagarLinha        bool
	Situacao, Erro     string
	Marcador           int
}

// RespostaDasNotas é a parte das notas de uma resposta da nuvem, aplicada na
// mesma transação dos ditados, do aprendido e do cursor (AplicarTudo): os
// marcadores antes das notas. Cada decisão recebe o estado lido NESTA
// transação e não pode tocar o banco (é pura).
type RespostaDasNotas struct {
	Marcadores      []string
	DecidirMarcador func(EstadoDoMarcadorDeNota) AcaoDoMarcadorDeNota
	Notas           []string
	DecidirNota     func(EstadoDaNota) AcaoDaNota
	// Imagens são as imagens das notas que o pull trouxe (N10b;
	// nota_imagem_sync.go): as mudanças note_image da página e as das listas
	// imagens das notas dela. Aplicadas depois das notas, para a imagem achar
	// a nota que chegou na mesma página.
	Imagens       []ImagemNoPull
	DecidirImagem func(EstadoDaImagem) AcaoDaImagem
}

// aplicarNotas executa, na transação de AplicarTudo, os marcadores e depois
// as notas.
func (f *Fila) aplicarNotas(ctx context.Context, tx *sql.Tx, r RespostaDasNotas) error {
	for _, id := range r.Marcadores {
		e, err := f.estadoDoMarcadorDeNota(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := f.executarMarcadorDeNota(ctx, tx, e, r.DecidirMarcador(e)); err != nil {
			return err
		}
	}
	for _, id := range r.Notas {
		e, err := f.estadoDaNota(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := f.executarNota(ctx, tx, e, r.DecidirNota(e)); err != nil {
			return err
		}
	}
	if len(r.Imagens) == 0 {
		return nil
	}
	return f.aplicarImagens(ctx, tx, r.Imagens, r.DecidirImagem)
}

// marcadorDaFila lê o marcador da fila de uma entidade, ou nil.
func marcadorDaFila(ctx context.Context, tx *sql.Tx, entidade, id string) (*Marcador, error) {
	ms, err := lerMarcadores(ctx, tx, `WHERE entity_type = ? AND entity_id = ?`, entidade, id)
	if err != nil || len(ms) != 1 {
		return nil, err
	}
	return &ms[0], nil
}

func (f *Fila) estadoDaNota(ctx context.Context, tx *sql.Tx, id string) (EstadoDaNota, error) {
	e := EstadoDaNota{ID: id}
	var err error
	if e.Linha, err = f.notaParaSync(ctx, tx, id); err != nil {
		return e, err
	}
	e.Marcador, err = marcadorDaFila(ctx, tx, EntidadeNota, id)
	return e, err
}

func (f *Fila) estadoDoMarcadorDeNota(ctx context.Context, tx *sql.Tx, id string) (EstadoDoMarcadorDeNota, error) {
	e := EstadoDoMarcadorDeNota{ID: id}
	var err error
	if e.Linha, err = f.marcadorParaSync(ctx, tx, id); err != nil {
		return e, err
	}
	e.Marcador, err = marcadorDaFila(ctx, tx, EntidadeMarcadorDeNota, id)
	return e, err
}

// executarNota faz o que a ação manda com a nota, na transação.
func (f *Fila) executarNota(ctx context.Context, tx *sql.Tx, e EstadoDaNota, a AcaoDaNota) error {
	if a.CorpoNuvemAtual && a.EsquecerCorpoNuvem {
		return acaoInvalida("corpo_nuvem atual e esquecido na mesma ação")
	}
	if a.ApagarLinha {
		if e.Linha != nil {
			if err := apagarNotaDaTabela(ctx, tx, e.ID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, EntidadeNota, e.ID); err != nil {
			return fmt.Errorf("storage: tirando da fila a nota apagada na nuvem: %w", err)
		}
		return nil
	}
	if a.Gravar != nil {
		if err := f.gravarNotaRemota(ctx, tx, e, *a.Gravar, a.ServerVersion); err != nil {
			return err
		}
	}
	if e.Linha != nil || a.Gravar != nil {
		sets := []string{}
		args := []any{}
		if a.ServerVersion != nil {
			sets, args = append(sets, "server_version = ?"), append(args, *a.ServerVersion)
		}
		if a.ZerarServerVersion {
			sets = append(sets, "server_version = NULL")
		}
		if a.CorpoNuvemAtual {
			sets = append(sets, "corpo_nuvem = corpo")
		}
		if a.EsquecerCorpoNuvem {
			sets = append(sets, "corpo_nuvem = NULL")
		}
		if a.Situacao != "" {
			var erro any
			if a.Erro != "" {
				erro = a.Erro
			}
			sets, args = append(sets, "sync_state = ?", "last_sync_error = ?"), append(args, a.Situacao, erro)
		}
		if len(sets) > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE local_notes SET `+strings.Join(sets, ", ")+` WHERE id = ? AND user_id = ?`,
				append(args, e.ID, f.dono)...); err != nil {
				return fmt.Errorf("storage: gravando o sync da nota: %w", err)
			}
		}
	}
	return executarMarcadorDaFila(ctx, tx, EntidadeNota, e.ID, a.Marcador, e.Linha != nil, camposDaReconciliacaoDaNota)
}

// executarMarcadorDaFila faz, na transação, o que a ação manda com o marcador
// da fila de uma nota ou de um marcador de nota (MarcadorFica, MarcadorSai,
// MarcadorReenviar, MarcadorCriar). O de criar só nasce com a linha aqui, com
// os campos da reconciliação.
func executarMarcadorDaFila(ctx context.Context, tx *sql.Tx, entidade, id string, acao int, temLinha bool, camposDeCriar int) error {
	switch acao {
	case MarcadorSai:
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, entidade, id); err != nil {
			return fmt.Errorf("storage: tirando o marcador da %s: %w", entidade, err)
		}
	case MarcadorReenviar:
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_outbox SET mutation_id = NULL, versao_enviada = NULL WHERE entity_type = ? AND entity_id = ?`,
			entidade, id); err != nil {
			return fmt.Errorf("storage: limpando o envio da %s: %w", entidade, err)
		}
	case MarcadorCriar:
		if !temLinha {
			return nil
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES (?, ?, ?, ?)
			 ON CONFLICT (entity_type, entity_id) DO NOTHING`,
			entidade, id, camposDeCriar, emTexto(time.Now())); err != nil {
			return fmt.Errorf("storage: pondo na fila a %s a reconciliar: %w", entidade, err)
		}
	}
	return nil
}

// gravarNotaRemota grava o estado da nuvem: insere a nota (versão 1) ou muda
// todos os campos da que existe (a versão sobe um, e uma aba aberta no painel
// recebe 409 em vez de sobrescrever), com corpo_nuvem = corpo e a situação
// normal, e troca as ligações pelo conjunto da nuvem. Nunca põe marcador.
func (f *Fila) gravarNotaRemota(ctx context.Context, tx *sql.Tx, e EstadoDaNota, r Nota, sv *int64) error {
	var purga *time.Time
	if r.DeletedAt != nil {
		p := r.DeletedAt.Add(PrazoDaLixeira)
		purga = &p
	}
	var cor any
	if r.Cor != nil && CorValida(*r.Cor) {
		cor = *r.Cor
	}
	posicao := r.Posicao
	if !PosicaoValida(posicao) {
		// Não acontece (a nuvem confere a mesma regra); se acontecer, a nota
		// vai para o topo do grupo dela em vez de ficar sem lugar na lista.
		var err error
		if posicao, err = posicaoNoTopo(ctx, tx, f.dono, r.Arquivada, r.Fixada); err != nil {
			return err
		}
	}
	var seloTitulo, seloCorpo, seloDevice, seloEm any
	if r.Conflito != nil {
		seloTitulo, seloCorpo, seloDevice, seloEm = r.Conflito.Titulo, r.Conflito.Corpo,
			textoNulo(r.Conflito.DeviceID), emTexto(r.Conflito.Em)
	}
	if e.Linha == nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_notes
			(id, user_id, titulo, corpo, cor, fixada, arquivada, ditado_de_origem, created_at, updated_at, deleted_at,
			 purge_at, version, sync_state, server_version, posicao,
			 conflito_titulo, conflito_corpo, conflito_device_id, conflito_em, corpo_nuvem)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,?,?,?,?,?)`,
			e.ID, f.dono, r.Titulo, r.Corpo, cor, boolInt(r.Fixada), boolInt(r.Arquivada), textoNulo(r.DitadoDeOrigem),
			emTexto(r.CreatedAt), emTexto(r.UpdatedAt), instanteNulo(r.DeletedAt), instanteNulo(purga),
			SituacaoNormal, inteiroNulo(sv), posicao, seloTitulo, seloCorpo, seloDevice, seloEm, r.Corpo); err != nil {
			return fmt.Errorf("storage: gravando a nota que veio da nuvem: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE local_notes SET titulo = ?, corpo = ?, cor = ?, fixada = ?, arquivada = ?,
			  ditado_de_origem = ?, updated_at = ?, deleted_at = ?, purge_at = ?, posicao = ?,
			  conflito_titulo = ?, conflito_corpo = ?, conflito_device_id = ?, conflito_em = ?, corpo_nuvem = ?,
			  sync_state = ?, last_sync_error = NULL, version = version + 1
			WHERE id = ? AND user_id = ?`,
			r.Titulo, r.Corpo, cor, boolInt(r.Fixada), boolInt(r.Arquivada), textoNulo(r.DitadoDeOrigem),
			emTexto(r.UpdatedAt), instanteNulo(r.DeletedAt), instanteNulo(purga), posicao,
			seloTitulo, seloCorpo, seloDevice, seloEm, r.Corpo, SituacaoNormal, e.ID, f.dono); err != nil {
			return fmt.Errorf("storage: aplicando a nota que veio da nuvem: %w", err)
		}
	}
	return f.trocarLigacoes(ctx, tx, e.ID, r.Marcadores)
}

// trocarLigacoes troca os marcadores da nota pelo conjunto da nuvem. O id que
// o PC não tem vira um "marcador que espera" (SituacaoAguardando): a ligação
// fica, invisível, e aparece quando o marcador chegar.
func (f *Fila) trocarLigacoes(ctx context.Context, tx *sql.Tx, notaID string, marcadores []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_labels WHERE note_id = ?`, notaID); err != nil {
		return fmt.Errorf("storage: trocando os marcadores da nota: %w", err)
	}
	agora := emTexto(time.Now())
	for _, m := range marcadores {
		if m == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_labels
			(id, user_id, nome, nome_dobrado, created_at, updated_at, deleted_at, version, sync_state)
			VALUES (?, ?, '', '', ?, ?, ?, 1, ?)
			ON CONFLICT (id) DO NOTHING`,
			m, f.dono, agora, agora, agora, SituacaoAguardando); err != nil {
			return fmt.Errorf("storage: guardando o marcador que a nota espera: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO local_note_labels (note_id, label_id) VALUES (?, ?)`, notaID, m); err != nil {
			return fmt.Errorf("storage: pondo o marcador na nota: %w", err)
		}
	}
	return nil
}

// executarMarcadorDeNota faz o que a ação manda com o marcador de nota, na
// transação.
func (f *Fila) executarMarcadorDeNota(ctx context.Context, tx *sql.Tx, e EstadoDoMarcadorDeNota, a AcaoDoMarcadorDeNota) error {
	if a.ApagarLinha {
		// A lápide: o marcador sai daqui (também o "que espera", e também o
		// que este PC já tinha apagado), com as ligações. As notas não ganham
		// marcador: a nuvem já tirou o id do conjunto delas.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM local_note_labels WHERE label_id IN (SELECT id FROM local_labels WHERE id = ? AND user_id = ?)`,
			e.ID, f.dono); err != nil {
			return fmt.Errorf("storage: tirando das notas o marcador apagado na nuvem: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_labels WHERE id = ? AND user_id = ?`, e.ID, f.dono); err != nil {
			return fmt.Errorf("storage: apagando o marcador apagado na nuvem: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, EntidadeMarcadorDeNota, e.ID); err != nil {
			return fmt.Errorf("storage: tirando da fila o marcador apagado na nuvem: %w", err)
		}
		return nil
	}
	if a.Gravar != nil {
		if err := f.gravarMarcadorRemoto(ctx, tx, e.ID, *a.Gravar, a.ServerVersion); err != nil {
			return err
		}
	}
	if e.Linha != nil || a.Gravar != nil {
		sets := []string{}
		args := []any{}
		if a.ServerVersion != nil {
			sets, args = append(sets, "server_version = ?"), append(args, *a.ServerVersion)
		}
		if a.ZerarServerVersion {
			sets = append(sets, "server_version = NULL")
		}
		if a.Situacao != "" {
			sets, args = append(sets, "sync_state = ?"), append(args, a.Situacao)
		}
		if len(sets) > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE local_labels SET `+strings.Join(sets, ", ")+
				` WHERE id = ? AND user_id = ? AND sync_state <> ?`,
				append(args, e.ID, f.dono, SituacaoAguardando)...); err != nil {
				return fmt.Errorf("storage: gravando o sync do marcador: %w", err)
			}
		}
	}
	return executarMarcadorDaFila(ctx, tx, EntidadeMarcadorDeNota, e.ID, a.Marcador, e.Linha != nil, CampoMarcadorNome)
}

// gravarMarcadorRemoto grava o marcador da nuvem: insere (versão 1), ou
// atualiza o nome da linha que existe (a versão sobe), tirando o "espera"
// (deleted_at nulo, situação normal). O nome que colide com outro marcador
// vivo daqui ganha " (2)", " (3)"... (nomeLivre): só aqui, nada sobe.
func (f *Fila) gravarMarcadorRemoto(ctx context.Context, tx *sql.Tx, id string, r MarcadorDeNota, sv *int64) error {
	nome, chave, err := nomeLivre(ctx, tx, f.dono, id, r.Nome)
	if err != nil {
		return err
	}
	var esperava sql.NullBool
	err = tx.QueryRowContext(ctx,
		`SELECT sync_state = ? FROM local_labels WHERE id = ? AND user_id = ?`, SituacaoAguardando, id, f.dono).Scan(&esperava)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("storage: lendo o marcador: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_labels
			(id, user_id, nome, nome_dobrado, created_at, updated_at, version, sync_state, server_version)
			VALUES (?,?,?,?,?,?,1,?,?)`,
			id, f.dono, nome, chave, emTexto(r.CreatedAt), emTexto(r.UpdatedAt), SituacaoNormal, inteiroNulo(sv)); err != nil {
			return fmt.Errorf("storage: gravando o marcador que veio da nuvem: %w", err)
		}
		return nil
	}
	// O "que espera" vira o marcador de verdade: a hora de criação passa a ser
	// a da nuvem (a do espera é a hora em que a nota chegou).
	if _, err := tx.ExecContext(ctx, `UPDATE local_labels SET nome = ?, nome_dobrado = ?, updated_at = ?, deleted_at = NULL,
		  created_at = CASE WHEN ? THEN ? ELSE created_at END, sync_state = ?, version = version + 1
		WHERE id = ? AND user_id = ?`,
		nome, chave, emTexto(r.UpdatedAt), esperava.Bool, emTexto(r.CreatedAt), SituacaoNormal, id, f.dono); err != nil {
		return fmt.Errorf("storage: aplicando o marcador que veio da nuvem: %w", err)
	}
	return nil
}

// nomeLivre devolve o nome com que o marcador id que veio da nuvem é gravado
// aqui, e a chave dele: o da nuvem, limpo; se outro marcador vivo do dono já
// tem esse nome (sem diferenciar maiúscula nem acento: a nuvem não impõe nome
// único, e dois PCs podem criar "Mercado" desconectados), com " (2)", " (3)"...
// cortando o nome para caber no teto.
func nomeLivre(ctx context.Context, tx *sql.Tx, dono, id, bruto string) (string, string, error) {
	base, _, err := nomeDoMarcador(bruto)
	if err != nil {
		// A nuvem confere a mesma regra; se vier algo que o PC recusaria, o
		// nome fica legível em vez de travar a página.
		base = strings.Join(strings.Fields(strings.ReplaceAll(bruto, "\x00", "")), " ")
		if base == "" {
			base = "Marcador"
		}
	}
	for k := 1; k <= 1000; k++ {
		nome := base
		if k > 1 {
			sufixo := fmt.Sprintf(" (%d)", k)
			nome = strings.TrimSpace(cortarRunas(base, MaxNomeDoMarcador-utf8.RuneCountInString(sufixo))) + sufixo
		}
		chave := dobrar(nome)
		usado, err := nomeEmUso(ctx, tx, dono, chave, id)
		if err != nil {
			return "", "", err
		}
		if !usado {
			return nome, chave, nil
		}
	}
	return "", "", fmt.Errorf("storage: sem nome livre para o marcador que veio da nuvem")
}

// cortarRunas corta s em no máximo n caracteres.
func cortarRunas(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for p := range s {
		if i == n {
			return s[:p]
		}
		i++
	}
	return s
}

// ─── O recomeço e o esquecer ───

// recomecarNotas é a parte das notas do recomeço (o epoch novo, e a primeira
// rodada de um pareamento sem estado), na transação dele, no molde dos
// ditados: cada nota e cada marcador de nota que a nuvem tinha (server_version
// não nulo), fora da lixeira, ganha um marcador de criar; e server_version (e
// o corpo_nuvem, que é da mesma versão) volta a nulo em todos. Os marcadores
// que já existiam ficam (sem a versão, também sobem inteiros), e a lápide
// local sobe como delete.
func (f *Fila) recomecarNotas(ctx context.Context, tx *sql.Tx, agora time.Time) error {
	for _, alvo := range []struct{ entidade, tabela string }{
		{EntidadeMarcadorDeNota, "local_labels"}, {EntidadeNota, "local_notes"},
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em)
			SELECT ?, id, 0, ? FROM `+alvo.tabela+`
			WHERE user_id = ? AND server_version IS NOT NULL AND deleted_at IS NULL
			ON CONFLICT (entity_type, entity_id) DO NOTHING`,
			alvo.entidade, emTexto(agora), f.dono); err != nil {
			return fmt.Errorf("storage: pondo na fila as notas que a nuvem já tinha: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE local_notes SET server_version = NULL, corpo_nuvem = NULL
		 WHERE user_id = ? AND (server_version IS NOT NULL OR corpo_nuvem IS NOT NULL)`, f.dono); err != nil {
		return fmt.Errorf("storage: esquecendo as versões das notas: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE local_labels SET server_version = NULL WHERE user_id = ? AND server_version IS NOT NULL`, f.dono); err != nil {
		return fmt.Errorf("storage: esquecendo as versões dos marcadores: %w", err)
	}
	return nil
}

// esquecerNotas é a parte das notas do "Esquecer esta nuvem", na transação
// dele (a fila inteira já sai lá): as versões, a situação, o erro e o
// corpo_nuvem saem; o selo, o conteúdo e o "marcador que espera" ficam.
func (f *Fila) esquecerNotas(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE local_notes SET server_version = NULL, corpo_nuvem = NULL, sync_state = ?, last_sync_error = NULL
		 WHERE user_id = ? AND (server_version IS NOT NULL OR corpo_nuvem IS NOT NULL OR sync_state <> ?
		   OR last_sync_error IS NOT NULL)`,
		SituacaoNormal, f.dono, SituacaoNormal); err != nil {
		return fmt.Errorf("storage: esquecendo as versões das notas: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE local_labels SET server_version = NULL, sync_state = ?
		 WHERE user_id = ? AND sync_state <> ? AND (server_version IS NOT NULL OR sync_state <> ?)`,
		SituacaoNormal, f.dono, SituacaoAguardando, SituacaoNormal); err != nil {
		return fmt.Errorf("storage: esquecendo as versões dos marcadores: %w", err)
	}
	return nil
}
