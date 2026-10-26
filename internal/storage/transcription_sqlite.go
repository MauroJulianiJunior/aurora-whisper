package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// formatoTempo é como um instante vira TEXT na tabela.
//
// NÃO é RFC3339Nano, e a diferença é de correção, não de gosto:
// RFC3339Nano CORTA os zeros à direita da fração, então o texto tem
// largura variável e a ordem lexicográfica deixa de valer. ".5Z" vem
// DEPOIS de ".50001Z" ao comparar caractere a caractere ('Z' > '0'), e
// antes ao comparar instantes.
//
// Isso importa porque a paginação por cursor ordena por created_at (e,
// na Lixeira, por deleted_at) no SQL, onde a coluna é TEXT. Com fração
// de largura fixa em nove dígitos, a ordem do texto e a ordem do tempo
// são a mesma.
//
// O 'Z' final é literal: o repositório sempre grava em UTC.
const formatoTempo = "2006-01-02T15:04:05.000000000Z"

// SQLiteTranscriptionRepository persiste o histórico local na tabela
// local_transcriptions (internal/storage/migrations).
//
// É a implementação de produção do mesmo contrato que
// InMemoryTranscriptionRepository define — e as duas são exercitadas
// pela MESMA suíte (transcription_memory_test.go, paraCadaRepo), para
// que "o que o histórico faz" tenha uma resposta só.
type SQLiteTranscriptionRepository struct {
	db *sql.DB
	// aviso é chamado depois do commit de uma gravação que pôs um marcador na
	// fila do sync (AoEnfileirar). Nunca dentro da transação, e quem o recebe
	// não pode bloquear: é o caminho do ditado.
	aviso atomic.Pointer[func()]
}

// NewSQLiteTranscriptionRepository monta o repositório sobre db, que já
// deve ter passado pelo Migrator.
func NewSQLiteTranscriptionRepository(db *sql.DB) *SQLiteTranscriptionRepository {
	if db == nil {
		panic("storage: db nil")
	}
	return &SQLiteTranscriptionRepository{db: db}
}

const colunasTranscricao = `id, user_id, device_id, source_platform, raw_text, final_text,
	mode, language, asr_provider, asr_model, duration_ms, processing_ms, confidence,
	target_app, status, created_at, updated_at, deleted_at, purge_at, version,
	sync_state, server_version, last_sync_error,
	conflito_texto, conflito_device_id, conflito_em, virou_nota`

// AoEnfileirar liga o aviso de que uma gravação pôs um marcador na fila do
// sync (ADR-019, F7a): no Agent, o "acorde" não bloqueante do laço de sync
// (syncengine.Laco.Avisar). É chamado depois do commit, na goroutine de quem
// gravou (a do ditado, num ditado novo), e por isso não pode bloquear nem
// fazer E/S. nil desliga.
func (r *SQLiteTranscriptionRepository) AoEnfileirar(f func()) {
	if f == nil {
		r.aviso.Store(nil)
		return
	}
	r.aviso.Store(&f)
}

func (r *SQLiteTranscriptionRepository) avisar() {
	if f := r.aviso.Load(); f != nil {
		(*f)()
	}
}

// naTransacao roda fn numa transação: commit se fn devolve nil, rollback se
// não.
func naTransacao(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: abrindo a transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op depois do commit
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: confirmando a transação: %w", err)
	}
	return nil
}

// Put grava a transcrição, substituindo a de mesmo id. É o ditado novo e o
// texto que chega atrasado (os dois com id novo): com a nuvem conectada, o
// marcador do ditado entra na fila na MESMA transação (ADR-019, F7a), sem
// campo nenhum (o ditado sem server_version sobe inteiro).
//
// Devolve erro, ao contrário da versão em memória de antes de
// 28-set-2026: escrever em disco falha de formas que escrever num mapa
// não falha, e engolir isso faria um ditado sumir do histórico em
// silêncio — exatamente o tipo de defeito mudo que este projeto já
// pagou caro para achar.
func (r *SQLiteTranscriptionRepository) Put(t Transcription) error {
	ctx := context.Background()
	emitiu := false
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		if err := inserirLinha(ctx, tx, `INSERT OR REPLACE`, t); err != nil {
			return fmt.Errorf("storage: gravando transcrição: %w", err)
		}
		var err error
		emitiu, err = emitir(ctx, tx, t.ID, 0, time.Now())
		return err
	})
	if err != nil {
		return err
	}
	if emitiu {
		r.avisar()
	}
	return nil
}

// inserirLinha grava t inteira, com o verbo dado (INSERT, ou INSERT OR
// REPLACE).
func inserirLinha(ctx context.Context, tx *sql.Tx, verbo string, t Transcription) error {
	var conflitoTexto, conflitoDevice, conflitoEm any
	if t.Conflito != nil {
		conflitoTexto, conflitoDevice, conflitoEm = t.Conflito.FinalText, textoNulo(t.Conflito.DeviceID), emTexto(t.Conflito.Em)
	}
	_, err := tx.ExecContext(ctx, verbo+` INTO local_transcriptions (`+colunasTranscricao+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, textoNulo(t.DeviceID), t.SourcePlatform, t.RawText, t.FinalText,
		t.Mode, textoNulo(t.Language), t.ASRProvider, textoNulo(t.ASRModel),
		inteiroNulo(t.DurationMs), inteiroNulo(t.ProcessingMs), realNulo(t.Confidence),
		textoNulo(t.TargetApp), t.Status, emTexto(t.CreatedAt), emTexto(t.UpdatedAt),
		instanteNulo(t.DeletedAt), instanteNulo(t.PurgeAt), t.Version,
		t.SyncState, inteiroNulo(t.ServerVersion), textoNulo(t.LastSyncError),
		conflitoTexto, conflitoDevice, conflitoEm, textoNulo(t.VirouNota))
	return err
}

// emitir põe o marcador do ditado id na fila do sync, ou junta os campos ao
// que já está lá (duas edições, um marcador), na transação da gravação
// (ADR-019, "Fila no PC: um marcador por entidade"), só para um ditado que a
// nuvem não pôs fora da janela. Quando, lido na própria transação:
//   - num ditado que a nuvem já tem (server_version não nulo), conectado ou
//     não: a mudança feita fora sobe na volta com a base certa e os campos
//     certos, e a nuvem compara campo a campo (ADR-019, "Decisões da F7a/b",
//     decisão 2);
//   - num ditado que a nuvem NÃO tem (server_version nulo), só o Put do
//     ditado novo (campos 0), com a nuvem conectada (o aparelho na tabela
//     instalacao), e só o nascido aqui (nasceuAquiSQL: device_id nulo, e não
//     um ditado que o celular criou na nuvem, ADR-021). A edição, a lixeira
//     e o restaurar dele não põem marcador: o histórico sobe pelo envio
//     inicial, com o clique e a cópia antes (D1), e depois do aceite pela
//     completação de cada rodada (CompletarEnvio), as duas sem a lixeira e
//     dentro dos 6 meses (revisão da F7c). Se o ditado já tem um marcador
//     (o de criar), ele fica como está: o ditado sobe inteiro, lido na hora
//     do envio, e os campos não valem nada para ele.
//
// Daí também que o ditado que veio de outro PC e que a nuvem desta conexão
// não tem (device_id não nulo e server_version nulo: depois do "Esquecer esta
// nuvem", ele é da conta antiga; ADR-019, "Decisões da F7c", D2) nunca entra.
// O que o celular criou na nuvem (source_platform web ou android, sem
// aparelho) é igual: veio da nuvem, e não nasceu aqui.
// O que veio de outro PC pela nuvem de agora tem server_version, e a mudança
// nele sobe.
//
// Devolve se o marcador foi posto (ou mudou). O aplicador do que vem da nuvem
// nunca chama isto: aplicar não ecoa.
func emitir(ctx context.Context, tx *sql.Tx, id string, campos int, agora time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em)
		SELECT ?, ?, ?, ?
		WHERE EXISTS (SELECT 1 FROM local_transcriptions WHERE id = ? AND sync_state <> ?
		  AND (server_version IS NOT NULL
		    OR (? = 0 AND `+nasceuAquiSQL+` AND EXISTS (SELECT 1 FROM instalacao WHERE nome = ? AND valor <> ''))))
		ON CONFLICT (entity_type, entity_id) DO UPDATE SET campos = campos | excluded.campos`,
		EntidadeDitado, id, campos, emTexto(agora), id, SituacaoForaDaJanela, campos, ChaveNuvemAparelho)
	if err != nil {
		return false, fmt.Errorf("storage: pondo o ditado na fila do sync: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: pondo o ditado na fila do sync: %w", err)
	}
	return n > 0, nil
}

func (r *SQLiteTranscriptionRepository) List(ctx context.Context, filter TranscriptionListFilter) (TranscriptionPage, error) {
	if filter.UserID == "" {
		return TranscriptionPage{}, fmt.Errorf("storage: List requer UserID")
	}
	limite := filter.Limit
	if limite <= 0 {
		limite = DefaultTranscriptionListLimit
	}
	if limite > MaxTranscriptionListLimit {
		limite = MaxTranscriptionListLimit
	}

	// ordem é a coluna da ordem da vista, a mesma de chaveDeOrdem: a
	// Lixeira ordena pela exclusão (revisão de 29-set-2026), o que usa o
	// índice idx_local_transcriptions_trash. As duas colunas são TEXT em
	// formatoTempo, de largura fixa, então a ordem do texto é a do tempo.
	// O nome vem daqui, nunca do filtro, e por isso pode ir no SQL.
	// virou_nota IS NULL: o ditado que virou nota ("Virar nota", ADR-022) sai do
	// histórico e da lixeira sem ir para a lixeira; fica no banco, achado pelo
	// Get (o Aprender e a auditoria leem por id).
	onde := []string{"user_id = ?", "virou_nota IS NULL"}
	args := []any{filter.UserID}
	ordem := "created_at"
	if filter.Trash {
		onde = append(onde, "deleted_at IS NOT NULL")
		ordem = "deleted_at"
	} else {
		onde = append(onde, "deleted_at IS NULL")
	}
	if filter.DeviceID != nil {
		onde, args = append(onde, "device_id = ?"), append(args, *filter.DeviceID)
	}
	if filter.Mode != nil {
		onde, args = append(onde, "mode = ?"), append(args, *filter.Mode)
	}
	if filter.ASRProvider != nil {
		onde, args = append(onde, "asr_provider = ?"), append(args, *filter.ASRProvider)
	}
	if filter.From != nil {
		onde, args = append(onde, "created_at >= ?"), append(args, emTexto(*filter.From))
	}
	if filter.To != nil {
		onde, args = append(onde, "created_at <= ?"), append(args, emTexto(*filter.To))
	}
	if q := strings.ToLower(strings.TrimSpace(filter.Query)); q != "" {
		// LOWER() no SQLite só rebaixa ASCII. A busca do histórico
		// precisa achar "balancete" digitando "Balancete", mas também
		// "Rosângela" digitando "rosângela" — por isso a comparação de
		// acentuados não pode depender do LOWER do banco. O filtro de
		// texto é aplicado em Go, abaixo.
		_ = q
	}
	if filter.Cursor != "" {
		ck, err := decodeCursor(filter.Cursor)
		if err != nil {
			return TranscriptionPage{}, err
		}
		onde = append(onde, "("+ordem+" < ? OR ("+ordem+" = ? AND id < ?))")
		args = append(args, emTexto(ck.instante), emTexto(ck.instante), ck.id)
	}

	// Sem LIMIT no SQL quando há busca textual: o filtro de acentuados
	// roda em Go, então cortar aqui devolveria página curta demais.
	sufixo := " ORDER BY " + ordem + " DESC, id DESC"
	comBusca := strings.TrimSpace(filter.Query) != ""
	if !comBusca {
		sufixo += fmt.Sprintf(" LIMIT %d", limite+1) // +1 para saber se há próxima página
	}

	linhas, err := r.db.QueryContext(ctx,
		`SELECT `+colunasTranscricao+` FROM local_transcriptions WHERE `+strings.Join(onde, " AND ")+sufixo,
		args...)
	if err != nil {
		return TranscriptionPage{}, fmt.Errorf("storage: listando transcrições: %w", err)
	}
	defer linhas.Close()

	busca := strings.ToLower(strings.TrimSpace(filter.Query))
	achadas := make([]Transcription, 0, limite+1)
	for linhas.Next() {
		t, err := lerTranscricao(linhas)
		if err != nil {
			return TranscriptionPage{}, err
		}
		if busca != "" &&
			!strings.Contains(strings.ToLower(t.RawText), busca) &&
			!strings.Contains(strings.ToLower(t.FinalText), busca) {
			continue
		}
		achadas = append(achadas, t)
		if len(achadas) > limite {
			break
		}
	}
	if err := linhas.Err(); err != nil {
		return TranscriptionPage{}, fmt.Errorf("storage: lendo transcrições: %w", err)
	}

	pagina := TranscriptionPage{Items: achadas}
	if len(achadas) > limite {
		pagina.Items = achadas[:limite]
		ultima := pagina.Items[len(pagina.Items)-1]
		pagina.NextCursor = encodeCursor(chaveDeOrdem(ultima, filter.Trash))
	}
	return pagina, nil
}

func (r *SQLiteTranscriptionRepository) Get(ctx context.Context, userID, id string) (Transcription, error) {
	linha := r.db.QueryRowContext(ctx,
		`SELECT `+colunasTranscricao+` FROM local_transcriptions WHERE id = ? AND user_id = ?`, id, userID)
	t, err := lerTranscricao(linha)
	if errors.Is(err, sql.ErrNoRows) {
		return Transcription{}, ErrNotFound
	}
	return t, err
}

// UpdateFinalText edita o texto final. Num ditado que a nuvem já tem
// (emitir), conectado ou não, o marcador do ditado (com o campo do texto)
// entra na fila na mesma transação.
func (r *SQLiteTranscriptionRepository) UpdateFinalText(ctx context.Context, userID, id string, baseVersion int64, finalText string) (Transcription, error) {
	agora := time.Now().UTC()
	emitiu := false
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		var versao int64
		err := tx.QueryRowContext(ctx,
			`SELECT version FROM local_transcriptions WHERE id = ? AND user_id = ?`, id, userID).Scan(&versao)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo a versão do ditado: %w", err)
		}
		if versao != baseVersion {
			return ErrVersionConflict
		}
		// A versão volta ao WHERE, como segunda guarda: o conflito é decidido
		// aqui, na mesma transação, e não presumido lá em cima.
		res, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET final_text = ?, version = version + 1, updated_at = ?
			 WHERE id = ? AND user_id = ? AND version = ?`,
			finalText, emTexto(agora), id, userID, baseVersion)
		if err != nil {
			return fmt.Errorf("storage: editando texto final: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrVersionConflict
		}
		emitiu, err = emitir(ctx, tx, id, CampoTexto, agora)
		return err
	})
	if err != nil {
		return Transcription{}, err
	}
	if emitiu {
		r.avisar()
	}
	return r.Get(ctx, userID, id)
}

// ResolverConflito é a escolha do selo no PC (ADR-019, "Decisões da F7c"),
// numa transação: só sobre a versão atual e só num ditado com selo (senão
// ErrVersionConflict). EscolhaTrocar põe o texto do selo como texto final; as
// duas limpam o selo, sobem a versão e anotam a hora.
//
// Num ditado que a nuvem tem (server_version), a escolha entra na fila (o
// marcador ganha selo_escolha e selo_em, o conflict_at do selo escolhido),
// também desconectado, para a nuvem aplicar a mesma escolha; o texto não vai
// junto (a nuvem troca ela mesma, e só se a escolha ainda valer). Sem a
// versão da nuvem (depois do "Esquecer esta nuvem"), a escolha é só daqui.
func (r *SQLiteTranscriptionRepository) ResolverConflito(ctx context.Context, userID, id string, baseVersion int64, escolha string) (Transcription, error) {
	if escolha != EscolhaManter && escolha != EscolhaTrocar {
		return Transcription{}, ErrEscolhaInvalida
	}
	agora := time.Now().UTC()
	emitiu := false
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		var versao int64
		var selo, seloEm sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT version, conflito_texto, conflito_em FROM local_transcriptions WHERE id = ? AND user_id = ?`,
			id, userID).Scan(&versao, &selo, &seloEm)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo o selo do ditado: %w", err)
		}
		if versao != baseVersion || !selo.Valid {
			return ErrVersionConflict
		}
		texto := `final_text`
		if escolha == EscolhaTrocar {
			// O lado direito do SET lê a linha de antes: o texto do selo.
			texto = `conflito_texto`
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET final_text = `+texto+`, conflito_texto = NULL, conflito_device_id = NULL,
			   conflito_em = NULL, version = version + 1, updated_at = ?
			 WHERE id = ? AND user_id = ? AND version = ?`,
			emTexto(agora), id, userID, baseVersion)
		if err != nil {
			return fmt.Errorf("storage: escolhendo a versão do selo: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrVersionConflict
		}
		res, err = tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, selo_escolha, selo_em)
			SELECT ?, ?, 0, ?, ?, ?
			WHERE EXISTS (SELECT 1 FROM local_transcriptions WHERE id = ? AND server_version IS NOT NULL AND sync_state <> ?)
			ON CONFLICT (entity_type, entity_id) DO UPDATE SET selo_escolha = excluded.selo_escolha, selo_em = excluded.selo_em`,
			EntidadeDitado, id, emTexto(agora), escolha, textoNulo(deTexto(seloEm)), id, SituacaoForaDaJanela)
		if err != nil {
			return fmt.Errorf("storage: pondo a escolha do selo na fila do sync: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("storage: pondo a escolha do selo na fila do sync: %w", err)
		}
		emitiu = n > 0
		return nil
	})
	if err != nil {
		return Transcription{}, err
	}
	if emitiu {
		r.avisar()
	}
	return r.Get(ctx, userID, id)
}

// SoftDelete põe na lixeira. Só se a linha mudou, e num ditado que a nuvem
// já tem (emitir), o marcador do ditado (com o campo da lixeira) entra na
// fila na mesma transação. Num ditado que a nuvem não tem, o marcador de
// criar que ainda não saiu (o do ditado novo, do envio inicial ou da
// completação) sai da fila: a lixeira não sobe (resposta 3 do Mauro; revisão
// da F7c).
func (r *SQLiteTranscriptionRepository) SoftDelete(ctx context.Context, userID, id string, now time.Time) error {
	agora := now.UTC()
	purga := agora.AddDate(0, 0, 30) // lixeira: 30 dias, igual à versão em memória.
	// "deleted_at IS NULL": item que já está na lixeira não é tocado.
	// Sem isso, uma aba com a lista velha apagava de novo o que outra aba
	// já tinha apagado, e isso reescrevia "na lixeira desde", empurrava a
	// purga mais 30 dias para frente e subia a versão (revisão de
	// 29-set-2026).
	return r.mudarLixeira(ctx, userID, id, true,
		`UPDATE local_transcriptions SET deleted_at = ?, purge_at = ?, updated_at = ?, version = version + 1
		 WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		emTexto(agora), emTexto(purga), emTexto(agora), id, userID)
}

// Restore tira da lixeira, com o marcador como no SoftDelete (num ditado que
// a nuvem não tem, nenhum: a completação do envio o leva, depois do aceite).
func (r *SQLiteTranscriptionRepository) Restore(ctx context.Context, userID, id string) error {
	// "deleted_at IS NOT NULL", pelo mesmo motivo do SoftDelete: restaurar
	// o que não está na lixeira não muda nada, nem a versão.
	return r.mudarLixeira(ctx, userID, id, false,
		`UPDATE local_transcriptions SET deleted_at = NULL, purge_at = NULL, updated_at = ?, version = version + 1
		 WHERE id = ? AND user_id = ? AND deleted_at IS NOT NULL`,
		emTexto(time.Now().UTC()), id, userID)
}

// mudarLixeira roda o UPDATE da lixeira (pôr, com poe, ou tirar) e, se ele
// mudou a linha, o marcador, numa transação só. Sem efeito, é semEfeito.
func (r *SQLiteTranscriptionRepository) mudarLixeira(ctx context.Context, userID, id string, poe bool, update string, args ...any) error {
	emitiu := false
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, update, args...)
		if err != nil {
			return fmt.Errorf("storage: mudando a lixeira: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return semEfeito(ctx, tx, userID, id)
		}
		if poe {
			// O marcador de criar (campos 0) parado na fila, de um ditado que
			// a nuvem não tem: ele sai. O em voo fica (a nuvem pode já ter o
			// ditado, e a resposta decide), e o da reconciliação também (a
			// nuvem tem o ditado; ele nasce com os campos: MarcadorCriar).
			// O reenvio do em voo lê a linha na hora e leva a lixeira: se o
			// primeiro envio não chegou, a nuvem cria o ditado já na lixeira
			// dela; se chegou, a lixeira sobe como mudança. Os dois terminam
			// iguais, e criar sem a lixeira só faria o ditado passar pelo
			// histórico do app antes (risco aceito na reconferência da F7c).
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ? AND mutation_id IS NULL AND campos = 0
				   AND EXISTS (SELECT 1 FROM local_transcriptions WHERE id = ? AND server_version IS NULL)`,
				EntidadeDitado, id, id); err != nil {
				return fmt.Errorf("storage: tirando da fila o ditado que foi para a lixeira: %w", err)
			}
		}
		emitiu, err = emitir(ctx, tx, id, CampoLixeira, time.Now())
		return err
	})
	if err == nil && emitiu {
		r.avisar()
	}
	return err
}

// semEfeito diz o que significa um SoftDelete ou Restore que não mudou
// linha nenhuma: se o registro existe, ele já estava no estado pedido e a
// operação não tem o que fazer (nil); se não existe, ou é de outro
// usuário, ErrNotFound. Roda na transação do UPDATE.
func semEfeito(ctx context.Context, tx *sql.Tx, userID, id string) error {
	var existe int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM local_transcriptions WHERE id = ? AND user_id = ?`, id, userID).Scan(&existe)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: conferindo transcrição: %w", err)
	}
	return nil
}

// PurgeExpired apaga de vez o que venceu na lixeira, menos o que a nuvem pode
// ter e ainda não soube (a guarda da D3: esperaANuvem, lixeira_nuvem.go), e o
// marcador e a anotação de cada um junto, na mesma transação: um ditado
// expurgado não sobe a partir da fila (ADR-019, "Fila no PC" e "Decisões da
// F10"). O que fica é contado em EsperandoANuvem.
func (r *SQLiteTranscriptionRepository) PurgeExpired(ctx context.Context, now time.Time) (ResultadoDoExpurgo, error) {
	var res ResultadoDoExpurgo
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		var err error
		res, err = expurgarLixeira(ctx, tx, now)
		return err
	})
	if err != nil {
		return ResultadoDoExpurgo{}, err
	}
	return res, nil
}

// --- conversão de colunas -----------------------------------------------

// scanner cobre *sql.Row e *sql.Rows, para lerTranscricao servir aos dois.
type scanner interface{ Scan(dest ...any) error }

func lerTranscricao(s scanner) (Transcription, error) {
	var t Transcription
	var deviceID, language, asrModel, targetApp, lastSyncError sql.NullString
	var deletedAt, purgeAt sql.NullString
	var durationMs, processingMs, serverVersion sql.NullInt64
	var confidence sql.NullFloat64
	var createdAt, updatedAt string
	var conflitoTexto, conflitoDevice, conflitoEm, virouNota sql.NullString

	if err := s.Scan(&t.ID, &t.UserID, &deviceID, &t.SourcePlatform, &t.RawText, &t.FinalText,
		&t.Mode, &language, &t.ASRProvider, &asrModel, &durationMs, &processingMs, &confidence,
		&targetApp, &t.Status, &createdAt, &updatedAt, &deletedAt, &purgeAt, &t.Version,
		&t.SyncState, &serverVersion, &lastSyncError,
		&conflitoTexto, &conflitoDevice, &conflitoEm, &virouNota); err != nil {
		return Transcription{}, err
	}
	t.VirouNota = deTexto(virouNota)
	if conflitoTexto.Valid {
		em, err := deTempoNulo(conflitoEm)
		if err != nil {
			return Transcription{}, err
		}
		t.Conflito = &Conflito{FinalText: conflitoTexto.String, DeviceID: deTexto(conflitoDevice)}
		if em != nil {
			t.Conflito.Em = *em
		}
	}

	t.DeviceID = deTexto(deviceID)
	t.Language = deTexto(language)
	t.ASRModel = deTexto(asrModel)
	t.TargetApp = deTexto(targetApp)
	t.LastSyncError = deTexto(lastSyncError)
	t.DurationMs = deInteiro(durationMs)
	t.ProcessingMs = deInteiro(processingMs)
	t.ServerVersion = deInteiro(serverVersion)
	if confidence.Valid {
		t.Confidence = &confidence.Float64
	}

	var err error
	if t.CreatedAt, err = deTempo(createdAt); err != nil {
		return Transcription{}, err
	}
	if t.UpdatedAt, err = deTempo(updatedAt); err != nil {
		return Transcription{}, err
	}
	if t.DeletedAt, err = deTempoNulo(deletedAt); err != nil {
		return Transcription{}, err
	}
	if t.PurgeAt, err = deTempoNulo(purgeAt); err != nil {
		return Transcription{}, err
	}
	return t, nil
}

func emTexto(t time.Time) string { return t.UTC().Format(formatoTempo) }

func deTempo(s string) (time.Time, error) {
	t, err := time.Parse(formatoTempo, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("storage: instante inválido %q: %w", s, err)
	}
	return t.UTC(), nil
}

func deTempoNulo(n sql.NullString) (*time.Time, error) {
	if !n.Valid {
		return nil, nil
	}
	t, err := deTempo(n.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func textoNulo(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func inteiroNulo(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func realNulo(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func instanteNulo(p *time.Time) any {
	if p == nil {
		return nil
	}
	return emTexto(*p)
}

func deTexto(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func deInteiro(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
