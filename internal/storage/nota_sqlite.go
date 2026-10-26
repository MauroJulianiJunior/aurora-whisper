package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// DiasDaLixeiraDasNotas é o prazo da lixeira das notas: o mesmo dos ditados.
const DiasDaLixeiraDasNotas = 30

// SQLiteNotaRepository persiste as notas e os marcadores nas tabelas
// local_notes, local_labels e local_note_labels (migration 0009).
//
// Nada daqui toca a sync_outbox: o motor de sync ainda não conhece notas nem
// marcadores (ver nota.go); as linhas ficam com sync_state 'pending' até a N7.
type SQLiteNotaRepository struct {
	db *sql.DB
	// agora é o relógio das gravações; os testes trocam por UsarRelogio.
	agora atomic.Pointer[func() time.Time]
}

var _ NotaRepository = (*SQLiteNotaRepository)(nil)

// NewSQLiteNotaRepository monta o repositório sobre db, que já deve ter
// passado pelo Migrator.
func NewSQLiteNotaRepository(db *sql.DB) *SQLiteNotaRepository {
	if db == nil {
		panic("storage: db nil")
	}
	return &SQLiteNotaRepository{db: db}
}

// UsarRelogio troca o relógio das gravações (padrão: time.Now). nil volta ao
// padrão. Existe para os testes da ordem por edição e da lixeira.
func (r *SQLiteNotaRepository) UsarRelogio(f func() time.Time) {
	if f == nil {
		r.agora.Store(nil)
		return
	}
	r.agora.Store(&f)
}

func (r *SQLiteNotaRepository) now() time.Time {
	if f := r.agora.Load(); f != nil {
		return (*f)().UTC()
	}
	return time.Now().UTC()
}

const colunasNota = `id, user_id, titulo, corpo, cor, fixada, arquivada, ditado_de_origem,
	created_at, updated_at, deleted_at, purge_at, version, sync_state, server_version, last_sync_error, posicao`

const colunasMarcador = `id, user_id, nome, created_at, updated_at, deleted_at, version, sync_state, server_version`

func lerNota(s scanner) (Nota, error) {
	var n Nota
	var cor, origem, lastErr sql.NullString
	var deletedAt, purgeAt sql.NullString
	var fixada, arquivada int
	var serverVersion sql.NullInt64
	var criada, atualizada string
	if err := s.Scan(&n.ID, &n.UserID, &n.Titulo, &n.Corpo, &cor, &fixada, &arquivada, &origem,
		&criada, &atualizada, &deletedAt, &purgeAt, &n.Version, &n.SyncState, &serverVersion, &lastErr, &n.Posicao); err != nil {
		return Nota{}, err
	}
	var err error
	if n.CreatedAt, err = deTempo(criada); err != nil {
		return Nota{}, err
	}
	if n.UpdatedAt, err = deTempo(atualizada); err != nil {
		return Nota{}, err
	}
	if n.DeletedAt, err = deTempoNulo(deletedAt); err != nil {
		return Nota{}, err
	}
	if n.PurgeAt, err = deTempoNulo(purgeAt); err != nil {
		return Nota{}, err
	}
	n.Cor, n.DitadoDeOrigem, n.LastSyncError = deTexto(cor), deTexto(origem), deTexto(lastErr)
	n.ServerVersion = deInteiro(serverVersion)
	n.Fixada, n.Arquivada = fixada == 1, arquivada == 1
	n.Marcadores = []string{}
	return n, nil
}

func lerMarcador(s scanner) (MarcadorDeNota, error) {
	var m MarcadorDeNota
	var deletedAt sql.NullString
	var serverVersion sql.NullInt64
	var criado, atualizado string
	if err := s.Scan(&m.ID, &m.UserID, &m.Nome, &criado, &atualizado, &deletedAt, &m.Version,
		&m.SyncState, &serverVersion); err != nil {
		return MarcadorDeNota{}, err
	}
	var err error
	if m.CreatedAt, err = deTempo(criado); err != nil {
		return MarcadorDeNota{}, err
	}
	if m.UpdatedAt, err = deTempo(atualizado); err != nil {
		return MarcadorDeNota{}, err
	}
	if m.DeletedAt, err = deTempoNulo(deletedAt); err != nil {
		return MarcadorDeNota{}, err
	}
	m.ServerVersion = deInteiro(serverVersion)
	return m, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// corNula: "" é sem cor.
func corNula(cor string) any {
	if cor == "" {
		return nil
	}
	return cor
}

// marcadoresDasNotas devolve os ids dos marcadores (não apagados) por nota,
// na ordem do nome. notaID vazio traz as notas todas do usuário.
func marcadoresDasNotas(ctx context.Context, q consulta, userID, notaID string) (map[string][]string, error) {
	consultaSQL := `SELECT nl.note_id, nl.label_id
		FROM local_note_labels nl
		JOIN local_notes n ON n.id = nl.note_id
		JOIN local_labels l ON l.id = nl.label_id
		WHERE n.user_id = ? AND l.user_id = ? AND l.deleted_at IS NULL`
	args := []any{userID, userID}
	if notaID != "" {
		consultaSQL += ` AND nl.note_id = ?`
		args = append(args, notaID)
	}
	consultaSQL += ` ORDER BY l.nome_dobrado, l.id`
	linhas, err := q.QueryContext(ctx, consultaSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: lendo os marcadores das notas: %w", err)
	}
	defer linhas.Close()
	porNota := map[string][]string{}
	for linhas.Next() {
		var nota, marcador string
		if err := linhas.Scan(&nota, &marcador); err != nil {
			return nil, fmt.Errorf("storage: lendo os marcadores das notas: %w", err)
		}
		porNota[nota] = append(porNota[nota], marcador)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo os marcadores das notas: %w", err)
	}
	return porNota, nil
}

// lerNotaCompleta lê a nota e os marcadores dela.
func lerNotaCompleta(ctx context.Context, q consulta, userID, id string) (Nota, error) {
	n, err := lerNota(q.QueryRowContext(ctx,
		`SELECT `+colunasNota+` FROM local_notes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Nota{}, ErrNotFound
	}
	if err != nil {
		return Nota{}, fmt.Errorf("storage: lendo a nota: %w", err)
	}
	porNota, err := marcadoresDasNotas(ctx, q, userID, id)
	if err != nil {
		return Nota{}, err
	}
	if ms := porNota[id]; ms != nil {
		n.Marcadores = ms
	}
	return n, nil
}

func (r *SQLiteNotaRepository) Get(ctx context.Context, userID, id string) (Nota, error) {
	return lerNotaCompleta(ctx, r.db, userID, id)
}

// marcadorExiste confere, na transação, que o marcador é do usuário e não foi
// apagado.
func marcadorExiste(ctx context.Context, tx *sql.Tx, userID, id string) error {
	var um int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM local_labels WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, id, userID).Scan(&um)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: conferindo o marcador: %w", err)
	}
	return nil
}

func (r *SQLiteNotaRepository) Criar(ctx context.Context, p NovaNota) (Nota, error) {
	if p.UserID == "" {
		return Nota{}, fmt.Errorf("storage: Criar requer UserID")
	}
	if err := validarTextoDaNota(&p.Titulo, &p.Corpo); err != nil {
		return Nota{}, err
	}
	if err := validarCor(p.Cor); err != nil {
		return Nota{}, err
	}
	if p.Token != "" && !TokenDaNotaValido(p.Token) {
		return Nota{}, ErrTokenInvalido
	}
	if strings.TrimSpace(p.Titulo) == "" && strings.TrimSpace(p.Corpo) == "" {
		return Nota{}, ErrNotaVazia
	}
	agora := r.now()
	id := NewID()
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		if err := inserirNota(ctx, tx, id, agora, p); err != nil {
			return err
		}
		if p.Token == "" {
			return nil
		}
		// Os ditados feitos dentro desta caixa de "Criar uma nota" passam a ser
		// os desta nota, na mesma transação em que ela nasce: ou a nota e o
		// vínculo, ou nenhum. Não mexe na versão nem na fila do sync.
		if _, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET virou_nota = ? WHERE user_id = ? AND virou_nota = ?`,
			id, p.UserID, PrefixoDaNotaNova+p.Token); err != nil {
			return fmt.Errorf("storage: ligando os ditados feitos na caixa à nota: %w", err)
		}
		return nil
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, p.UserID, id)
}

// inserirNota grava a nota nova e os marcadores dela na transação tx. É o
// miolo do Criar e do VirarNota (que a cria na mesma transação em que marca
// o ditado).
func inserirNota(ctx context.Context, tx *sql.Tx, id string, agora time.Time, p NovaNota) error {
	// A nota nova entra no topo do grupo dela (ADR-022, N2b).
	posicao, err := posicaoNoTopo(ctx, tx, p.UserID, p.Arquivada, p.Fixada)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO local_notes
		(id, user_id, titulo, corpo, cor, fixada, arquivada, ditado_de_origem, created_at, updated_at, version, sync_state, posicao)
		VALUES (?,?,?,?,?,?,?,?,?,?,1,'pending',?)`,
		id, p.UserID, p.Titulo, p.Corpo, corNula(p.Cor), boolInt(p.Fixada), boolInt(p.Arquivada),
		corNula(p.DitadoDeOrigem), emTexto(agora), emTexto(agora), posicao); err != nil {
		return fmt.Errorf("storage: gravando a nota: %w", err)
	}
	for _, m := range p.Marcadores {
		if err := marcadorExiste(ctx, tx, p.UserID, m); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO local_note_labels (note_id, label_id) VALUES (?, ?)`, id, m); err != nil {
			return fmt.Errorf("storage: pondo o marcador na nota: %w", err)
		}
	}
	return nil
}

func (r *SQLiteNotaRepository) Listar(ctx context.Context, f FiltroDeNotas) ([]Nota, error) {
	if f.UserID == "" {
		return nil, fmt.Errorf("storage: Listar requer UserID")
	}
	vista := f.Vista
	if vista == "" {
		vista = VistaAtivas
	}
	if !vista.Valida() {
		return nil, fmt.Errorf("storage: vista de notas inválida %q", vista)
	}
	onde := []string{"n.user_id = ?"}
	args := []any{f.UserID}
	// A ordem vem daqui, nunca do filtro: a posição manual (a menor é o topo), e
	// o rowid desempata em ordem de criação (o id v7 só ordena por milissegundo).
	ordem := "n.fixada DESC, n.posicao ASC, n.rowid DESC"
	switch vista {
	case VistaAtivas:
		onde = append(onde, "n.deleted_at IS NULL", "n.arquivada = 0")
	case VistaArquivo:
		onde = append(onde, "n.deleted_at IS NULL", "n.arquivada = 1")
	case VistaLixeira:
		onde = append(onde, "n.deleted_at IS NOT NULL")
		ordem = "n.deleted_at DESC, n.rowid DESC"
	}
	if f.MarcadorID != "" {
		onde = append(onde, `EXISTS (SELECT 1 FROM local_note_labels nl
			JOIN local_labels l ON l.id = nl.label_id
			WHERE nl.note_id = n.id AND nl.label_id = ? AND l.deleted_at IS NULL)`)
		args = append(args, f.MarcadorID)
	}
	linhas, err := r.db.QueryContext(ctx, `SELECT `+prefixar("n.", colunasNota)+` FROM local_notes n WHERE `+
		strings.Join(onde, " AND ")+` ORDER BY `+ordem, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: listando as notas: %w", err)
	}
	defer linhas.Close()

	// A busca roda em Go: o LOWER do SQLite só rebaixa ASCII, e a busca tem de
	// achar "reuniao" em "Reunião" (o mesmo motivo do histórico).
	busca := dobrar(strings.TrimSpace(f.Busca))
	notas := []Nota{}
	for linhas.Next() {
		n, err := lerNota(linhas)
		if err != nil {
			return nil, fmt.Errorf("storage: lendo as notas: %w", err)
		}
		if busca != "" && !strings.Contains(dobrar(n.Titulo), busca) && !strings.Contains(dobrar(n.Corpo), busca) {
			continue
		}
		notas = append(notas, n)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo as notas: %w", err)
	}
	linhas.Close()

	porNota, err := marcadoresDasNotas(ctx, r.db, f.UserID, "")
	if err != nil {
		return nil, err
	}
	for i := range notas {
		if ms := porNota[notas[i].ID]; ms != nil {
			notas[i].Marcadores = ms
		}
	}
	return notas, nil
}

// posicaoNoTopo devolve a posição que põe uma nota acima da primeira do grupo
// (arquivada, fixada) do usuário: as ativas fixadas, as ativas comuns, e assim
// por diante. Grupo vazio, a chave do meio. Lê na transação de quem vai gravar:
// o SQLite deixa uma escrita por vez, então duas notas novas não pegam a mesma
// chave.
func posicaoNoTopo(ctx context.Context, q consulta, userID string, arquivada, fixada bool) (string, error) {
	var primeira sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT MIN(posicao) FROM local_notes
		 WHERE user_id = ? AND arquivada = ? AND fixada = ? AND deleted_at IS NULL AND posicao <> ''`,
		userID, boolInt(arquivada), boolInt(fixada)).Scan(&primeira)
	if err != nil {
		return "", fmt.Errorf("storage: lendo a primeira posição do grupo: %w", err)
	}
	k, err := PosicaoEntre("", primeira.String)
	if err != nil {
		return "", fmt.Errorf("storage: gerando a posição da nota: %w", err)
	}
	return k, nil
}

// grupoDaNota lê o grupo em que a nota está (arquivada, fixada).
func grupoDaNota(ctx context.Context, tx *sql.Tx, userID, id string) (arquivada, fixada bool, err error) {
	var a, f int
	err = tx.QueryRowContext(ctx,
		`SELECT arquivada, fixada FROM local_notes WHERE id = ? AND user_id = ?`, id, userID).Scan(&a, &f)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, ErrNotFound
	}
	if err != nil {
		return false, false, fmt.Errorf("storage: lendo o grupo da nota: %w", err)
	}
	return a == 1, f == 1, nil
}

// prefixar põe o prefixo de tabela em cada coluna de uma lista "a, b, c".
func prefixar(prefixo, colunas string) string {
	partes := strings.Split(colunas, ",")
	for i, p := range partes {
		partes[i] = prefixo + strings.TrimSpace(p)
	}
	return strings.Join(partes, ", ")
}

func (r *SQLiteNotaRepository) Atualizar(ctx context.Context, userID, id string, baseVersion int64, m MudancaDaNota) (Nota, error) {
	if err := validarTextoDaNota(m.Titulo, m.Corpo); err != nil {
		return Nota{}, err
	}
	if m.MudarCor {
		if err := validarCor(m.Cor); err != nil {
			return Nota{}, err
		}
	}
	agora := r.now()
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		if err := conferirNotaEditavel(ctx, tx, userID, id, baseVersion); err != nil {
			return err
		}
		sets := []string{"version = version + 1", "updated_at = ?"}
		args := []any{emTexto(agora)}
		// Fixar, desafixar, arquivar e desarquivar levam a nota para o topo do
		// grupo de destino. Editar o resto (texto, cor) não mexe na posição.
		if m.Fixada != nil || m.Arquivada != nil {
			arquivada, fixada, err := grupoDaNota(ctx, tx, userID, id)
			if err != nil {
				return err
			}
			destArquivada, destFixada := arquivada, fixada
			if m.Arquivada != nil {
				destArquivada = *m.Arquivada
			}
			if m.Fixada != nil {
				destFixada = *m.Fixada
			}
			if destArquivada != arquivada || destFixada != fixada {
				posicao, err := posicaoNoTopo(ctx, tx, userID, destArquivada, destFixada)
				if err != nil {
					return err
				}
				sets, args = append(sets, "posicao = ?"), append(args, posicao)
			}
		}
		if m.Titulo != nil {
			sets, args = append(sets, "titulo = ?"), append(args, *m.Titulo)
		}
		if m.Corpo != nil {
			sets, args = append(sets, "corpo = ?"), append(args, *m.Corpo)
		}
		if m.MudarCor {
			sets, args = append(sets, "cor = ?"), append(args, corNula(m.Cor))
		}
		if m.Fixada != nil {
			sets, args = append(sets, "fixada = ?"), append(args, boolInt(*m.Fixada))
		}
		if m.Arquivada != nil {
			sets, args = append(sets, "arquivada = ?"), append(args, boolInt(*m.Arquivada))
		}
		args = append(args, id, userID, baseVersion)
		// A versão volta ao WHERE como segunda guarda, como no ditado.
		res, err := tx.ExecContext(ctx, `UPDATE local_notes SET `+strings.Join(sets, ", ")+
			` WHERE id = ? AND user_id = ? AND version = ? AND deleted_at IS NULL`, args...)
		if err != nil {
			return fmt.Errorf("storage: editando a nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrVersionConflict
		}
		return nil
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

// conferirNotaEditavel confere, na transação, que a nota existe, é do usuário,
// não está na lixeira e (com baseVersion >= 0) está na versão esperada.
func conferirNotaEditavel(ctx context.Context, tx *sql.Tx, userID, id string, baseVersion int64) error {
	var versao int64
	var deletedAt sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT version, deleted_at FROM local_notes WHERE id = ? AND user_id = ?`, id, userID).Scan(&versao, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: lendo a versão da nota: %w", err)
	}
	if deletedAt.Valid {
		return ErrNotaNaLixeira
	}
	if baseVersion >= 0 && versao != baseVersion {
		return ErrVersionConflict
	}
	return nil
}

func (r *SQLiteNotaRepository) SoftDelete(ctx context.Context, userID, id string, now time.Time) error {
	agora := now.UTC()
	purga := agora.AddDate(0, 0, DiasDaLixeiraDasNotas)
	return naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		// "deleted_at IS NULL": a nota que já está na lixeira não é tocada;
		// reapagar não reinicia o prazo (como no histórico).
		res, err := tx.ExecContext(ctx,
			`UPDATE local_notes SET deleted_at = ?, purge_at = ?, updated_at = ?, version = version + 1
			 WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
			emTexto(agora), emTexto(purga), emTexto(agora), id, userID)
		if err != nil {
			return fmt.Errorf("storage: movendo a nota para a lixeira: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return notaExiste(ctx, tx, userID, id)
		}
		return nil
	})
}

// notaExiste: nil se a nota existe (e é do usuário), ErrNotFound se não.
func notaExiste(ctx context.Context, tx *sql.Tx, userID, id string) error {
	var um int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM local_notes WHERE id = ? AND user_id = ?`, id, userID).Scan(&um)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: conferindo a nota: %w", err)
	}
	return nil
}

func (r *SQLiteNotaRepository) Restaurar(ctx context.Context, userID, id string) (Nota, error) {
	agora := r.now()
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		// A nota que volta da lixeira vai para o topo do grupo dela (a ordem de
		// quando foi apagada não vale mais: o grupo pode ter mudado de tamanho).
		arquivada, fixada, err := grupoDaNota(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		posicao, err := posicaoNoTopo(ctx, tx, userID, arquivada, fixada)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE local_notes SET deleted_at = NULL, purge_at = NULL, updated_at = ?, version = version + 1, posicao = ?
			 WHERE id = ? AND user_id = ? AND deleted_at IS NOT NULL`,
			emTexto(agora), posicao, id, userID)
		if err != nil {
			return fmt.Errorf("storage: restaurando a nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return notaExiste(ctx, tx, userID, id)
		}
		return nil
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

func (r *SQLiteNotaRepository) ApagarDeVez(ctx context.Context, userID, id string) error {
	return naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		var deletedAt sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT deleted_at FROM local_notes WHERE id = ? AND user_id = ?`, id, userID).Scan(&deletedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo a nota: %w", err)
		}
		if !deletedAt.Valid {
			return ErrNotaForaDaLixeira
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_labels WHERE note_id = ?`, id); err != nil {
			return fmt.Errorf("storage: apagando os marcadores da nota: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_notes WHERE id = ? AND user_id = ?`, id, userID); err != nil {
			return fmt.Errorf("storage: apagando a nota: %w", err)
		}
		return nil
	})
}

func (r *SQLiteNotaRepository) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	apagadas := 0
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		vencidas := `FROM local_notes WHERE deleted_at IS NOT NULL AND purge_at IS NOT NULL AND purge_at <= ?`
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM local_note_labels WHERE note_id IN (SELECT id `+vencidas+`)`, emTexto(now.UTC())); err != nil {
			return fmt.Errorf("storage: expurgando os marcadores das notas: %w", err)
		}
		res, err := tx.ExecContext(ctx, `DELETE `+vencidas, emTexto(now.UTC()))
		if err != nil {
			return fmt.Errorf("storage: expurgando as notas: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("storage: expurgando as notas: %w", err)
		}
		apagadas = int(n)
		return nil
	})
	if err != nil {
		return 0, err
	}
	// Carona do mesmo ciclo: o ditado feito numa caixa "Criar uma nota" que
	// nunca virou nota volta ao histórico depois da carência (nota_ditado_foco.go).
	// Uma falha aqui não derruba o expurgo das notas: tenta de novo na rodada
	// seguinte.
	_, _ = r.LiberarDitadosDeNotasNovas(ctx, now.Add(-CarenciaDaNotaNova))
	return apagadas, nil
}

func (r *SQLiteNotaRepository) Marcar(ctx context.Context, userID, notaID, marcadorID string) (Nota, error) {
	agora := r.now()
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		if err := conferirNotaEditavel(ctx, tx, userID, notaID, -1); err != nil {
			return err
		}
		if err := marcadorExiste(ctx, tx, userID, marcadorID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO local_note_labels (note_id, label_id) VALUES (?, ?)`, notaID, marcadorID)
		if err != nil {
			return fmt.Errorf("storage: pondo o marcador na nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // já tinha: nada mudou, a versão fica
		}
		return subirANota(ctx, tx, userID, notaID, agora)
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, notaID)
}

func (r *SQLiteNotaRepository) Desmarcar(ctx context.Context, userID, notaID, marcadorID string) (Nota, error) {
	agora := r.now()
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		if err := conferirNotaEditavel(ctx, tx, userID, notaID, -1); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`DELETE FROM local_note_labels WHERE note_id = ? AND label_id = ?`, notaID, marcadorID)
		if err != nil {
			return fmt.Errorf("storage: tirando o marcador da nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return subirANota(ctx, tx, userID, notaID, agora)
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, notaID)
}

// subirANota sobe a versão e a hora da nota depois de uma mudança nos
// marcadores dela.
func subirANota(ctx context.Context, tx *sql.Tx, userID, notaID string, agora time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE local_notes SET version = version + 1, updated_at = ? WHERE id = ? AND user_id = ?`,
		emTexto(agora), notaID, userID); err != nil {
		return fmt.Errorf("storage: subindo a versão da nota: %w", err)
	}
	return nil
}

func ehUnicidadeDoMarcador(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}

// nomeEmUso diz se outro marcador não apagado do usuário já tem a chave.
func nomeEmUso(ctx context.Context, tx *sql.Tx, userID, chave, exceto string) (bool, error) {
	var um int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM local_labels WHERE user_id = ? AND nome_dobrado = ? AND deleted_at IS NULL AND id <> ?`,
		userID, chave, exceto).Scan(&um)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: conferindo o nome do marcador: %w", err)
	}
	return true, nil
}

func (r *SQLiteNotaRepository) CriarMarcador(ctx context.Context, userID, nome string) (MarcadorDeNota, error) {
	if userID == "" {
		return MarcadorDeNota{}, fmt.Errorf("storage: CriarMarcador requer UserID")
	}
	limpo, chave, err := nomeDoMarcador(nome)
	if err != nil {
		return MarcadorDeNota{}, err
	}
	agora := r.now()
	id := NewID()
	err = naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		if usado, err := nomeEmUso(ctx, tx, userID, chave, ""); err != nil {
			return err
		} else if usado {
			return ErrMarcadorDuplicado
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_labels
			(id, user_id, nome, nome_dobrado, created_at, updated_at, version, sync_state)
			VALUES (?,?,?,?,?,?,1,'pending')`,
			id, userID, limpo, chave, emTexto(agora), emTexto(agora)); err != nil {
			if ehUnicidadeDoMarcador(err) {
				return ErrMarcadorDuplicado
			}
			return fmt.Errorf("storage: gravando o marcador: %w", err)
		}
		return nil
	})
	if err != nil {
		return MarcadorDeNota{}, err
	}
	return r.getMarcador(ctx, userID, id)
}

func (r *SQLiteNotaRepository) getMarcador(ctx context.Context, userID, id string) (MarcadorDeNota, error) {
	m, err := lerMarcador(r.db.QueryRowContext(ctx,
		`SELECT `+colunasMarcador+` FROM local_labels WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return MarcadorDeNota{}, ErrNotFound
	}
	if err != nil {
		return MarcadorDeNota{}, fmt.Errorf("storage: lendo o marcador: %w", err)
	}
	return m, nil
}

func (r *SQLiteNotaRepository) RenomearMarcador(ctx context.Context, userID, id string, baseVersion int64, nome string) (MarcadorDeNota, error) {
	limpo, chave, err := nomeDoMarcador(nome)
	if err != nil {
		return MarcadorDeNota{}, err
	}
	agora := r.now()
	err = naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		var versao int64
		err := tx.QueryRowContext(ctx,
			`SELECT version FROM local_labels WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, id, userID).Scan(&versao)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo a versão do marcador: %w", err)
		}
		if versao != baseVersion {
			return ErrVersionConflict
		}
		if usado, err := nomeEmUso(ctx, tx, userID, chave, id); err != nil {
			return err
		} else if usado {
			return ErrMarcadorDuplicado
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE local_labels SET nome = ?, nome_dobrado = ?, updated_at = ?, version = version + 1
			 WHERE id = ? AND user_id = ? AND version = ? AND deleted_at IS NULL`,
			limpo, chave, emTexto(agora), id, userID, baseVersion)
		if err != nil {
			if ehUnicidadeDoMarcador(err) {
				return ErrMarcadorDuplicado
			}
			return fmt.Errorf("storage: renomeando o marcador: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrVersionConflict
		}
		return nil
	})
	if err != nil {
		return MarcadorDeNota{}, err
	}
	return r.getMarcador(ctx, userID, id)
}

func (r *SQLiteNotaRepository) ApagarMarcador(ctx context.Context, userID, id string) error {
	agora := r.now()
	return naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE local_labels SET deleted_at = ?, updated_at = ?, version = version + 1
			 WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
			emTexto(agora), emTexto(agora), id, userID)
		if err != nil {
			return fmt.Errorf("storage: apagando o marcador: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// Já apagado: nada a fazer. Inexistente (ou de outro usuário): 404.
			var um int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM local_labels WHERE id = ? AND user_id = ?`, id, userID).Scan(&um)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("storage: conferindo o marcador: %w", err)
			}
			return nil
		}
		// Tira o marcador das notas (a mudança de cada uma sobe a versão, para
		// o sync da N7 vê-la; a hora de edição não muda, para a nota não pular
		// para o topo da lista só porque um marcador sumiu).
		if _, err := tx.ExecContext(ctx,
			`UPDATE local_notes SET version = version + 1
			 WHERE user_id = ? AND id IN (SELECT note_id FROM local_note_labels WHERE label_id = ?)`,
			userID, id); err != nil {
			return fmt.Errorf("storage: subindo a versão das notas do marcador: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_labels WHERE label_id = ?`, id); err != nil {
			return fmt.Errorf("storage: tirando o marcador das notas: %w", err)
		}
		return nil
	})
}

func (r *SQLiteNotaRepository) ListarMarcadores(ctx context.Context, userID string) ([]MarcadorDeNota, error) {
	if userID == "" {
		return nil, fmt.Errorf("storage: ListarMarcadores requer UserID")
	}
	linhas, err := r.db.QueryContext(ctx,
		`SELECT `+colunasMarcador+` FROM local_labels WHERE user_id = ? AND deleted_at IS NULL`, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: listando os marcadores: %w", err)
	}
	defer linhas.Close()
	ms := []MarcadorDeNota{}
	for linhas.Next() {
		m, err := lerMarcador(linhas)
		if err != nil {
			return nil, fmt.Errorf("storage: lendo os marcadores: %w", err)
		}
		ms = append(ms, m)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo os marcadores: %w", err)
	}
	// Por nome sem caixa nem acento (a mesma chave da unicidade); o id
	// desempata, e a ordem fica estável.
	sort.SliceStable(ms, func(i, j int) bool {
		a, b := dobrar(ms[i].Nome), dobrar(ms[j].Nome)
		if a != b {
			return a < b
		}
		return ms[i].ID < ms[j].ID
	})
	return ms, nil
}
