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
// Cada gravação põe o marcador da nota ou do marcador na sync_outbox na
// mesma transação (N7, nota_sync.go), e avisa o laço do sync depois do
// commit (AoEnfileirar).
type SQLiteNotaRepository struct {
	db *sql.DB
	// agora é o relógio das gravações; os testes trocam por UsarRelogio.
	agora atomic.Pointer[func() time.Time]
	// aviso é chamado depois do commit de uma gravação que pôs (ou mudou) um
	// marcador na fila do sync. Nunca dentro da transação, e não pode
	// bloquear (o mesmo contrato do SQLiteTranscriptionRepository).
	aviso atomic.Pointer[func()]
}

// AoEnfileirar liga o aviso de que uma gravação pôs um marcador de nota, de
// marcador de nota ou de ditado (o virou_nota) na fila do sync (N7): no
// Agent, o "acorde" não bloqueante do laço (syncengine.Laco.Avisar). nil
// desliga.
func (r *SQLiteNotaRepository) AoEnfileirar(f func()) {
	if f == nil {
		r.aviso.Store(nil)
		return
	}
	r.aviso.Store(&f)
}

func (r *SQLiteNotaRepository) avisar() {
	if f := r.aviso.Load(); f != nil {
		(*f)()
	}
}

// gravar roda fn numa transação, com o anotador do que entrou na fila, e
// avisa depois do commit se algo entrou.
func (r *SQLiteNotaRepository) gravar(ctx context.Context, fn func(tx *sql.Tx, fila *naFila) error) error {
	var fila naFila
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		fila = naFila{}
		return fn(tx, &fila)
	})
	if err == nil && fila.emitiu {
		r.avisar()
	}
	return err
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
	created_at, updated_at, deleted_at, purge_at, version, sync_state, server_version, last_sync_error, posicao,
	conflito_titulo, conflito_corpo, conflito_device_id, conflito_em, corpo_nuvem`

const colunasMarcador = `id, user_id, nome, created_at, updated_at, deleted_at, version, sync_state, server_version`

func lerNota(s scanner) (Nota, error) {
	var n Nota
	var cor, origem, lastErr sql.NullString
	var deletedAt, purgeAt sql.NullString
	var fixada, arquivada int
	var serverVersion sql.NullInt64
	var criada, atualizada string
	var seloTitulo, seloCorpo, seloDevice, seloEm, corpoNuvem sql.NullString
	if err := s.Scan(&n.ID, &n.UserID, &n.Titulo, &n.Corpo, &cor, &fixada, &arquivada, &origem,
		&criada, &atualizada, &deletedAt, &purgeAt, &n.Version, &n.SyncState, &serverVersion, &lastErr, &n.Posicao,
		&seloTitulo, &seloCorpo, &seloDevice, &seloEm, &corpoNuvem); err != nil {
		return Nota{}, err
	}
	var err error
	// O selo: os quatro juntos (a escrita grava sempre os quatro); o título é
	// o que diz que há selo.
	if seloTitulo.Valid {
		n.Conflito = &ConflitoDaNota{Titulo: seloTitulo.String, Corpo: seloCorpo.String, DeviceID: deTexto(seloDevice)}
		em, err := deTempoNulo(seloEm)
		if err != nil {
			return Nota{}, err
		}
		if em != nil {
			n.Conflito.Em = *em
		}
	}
	n.CorpoNuvem = deTexto(corpoNuvem)
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
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		if err := inserirNota(ctx, tx, id, agora, p); err != nil {
			return err
		}
		if err := fila.anotar(emitirNota(ctx, tx, id, 0, agora)); err != nil {
			return err
		}
		if p.Token == "" {
			return nil
		}
		// Os ditados feitos dentro desta caixa de "Criar uma nota" passam a ser
		// os desta nota, na mesma transação em que ela nasce: ou a nota e o
		// vínculo, ou nenhum. Não mexe na versão do ditado; o virou_nota de cada
		// um entra na fila (CampoVirouNota) se a nuvem já tem o ditado (o que
		// ela ainda não tem sobe inteiro, com o virou_nota lido na hora).
		ligados, err := lerIDs(ctx, tx,
			`SELECT id FROM local_transcriptions WHERE user_id = ? AND virou_nota = ?`, p.UserID, PrefixoDaNotaNova+p.Token)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET virou_nota = ? WHERE user_id = ? AND virou_nota = ?`,
			id, p.UserID, PrefixoDaNotaNova+p.Token); err != nil {
			return fmt.Errorf("storage: ligando os ditados feitos na caixa à nota: %w", err)
		}
		for _, d := range ligados {
			if err := fila.anotar(emitir(ctx, tx, d, CampoVirouNota, agora)); err != nil {
				return err
			}
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
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		if err := conferirNotaEditavel(ctx, tx, userID, id, baseVersion); err != nil {
			return err
		}
		antes, err := lerNota(tx.QueryRowContext(ctx,
			`SELECT `+colunasNota+` FROM local_notes WHERE id = ? AND user_id = ?`, id, userID))
		if err != nil {
			return fmt.Errorf("storage: lendo a nota: %w", err)
		}
		// Os bits do sync são do que MUDOU de fato: o painel manda o título e o
		// corpo juntos a cada salvar, e um bit à toa vira um conflito à toa.
		campos := camposDaMudanca(antes, m)
		sets := []string{"version = version + 1", "updated_at = ?"}
		args := []any{emTexto(agora)}
		// Fixar, desafixar, arquivar e desarquivar levam a nota para o topo do
		// grupo de destino. Editar o resto (texto, cor) não mexe na posição.
		if m.Fixada != nil || m.Arquivada != nil {
			arquivada, fixada := antes.Arquivada, antes.Fixada
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
				campos |= CampoNotaPosicao
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
		if campos == 0 {
			return nil // nada mudou de verdade: nada a subir
		}
		return fila.anotar(emitirNota(ctx, tx, id, campos, agora))
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

// camposDaMudanca devolve os bits do sync do que m muda de fato em antes (a
// posição, que depende do grupo, fica com quem chama).
func camposDaMudanca(antes Nota, m MudancaDaNota) int {
	campos := 0
	if (m.Titulo != nil && *m.Titulo != antes.Titulo) || (m.Corpo != nil && *m.Corpo != antes.Corpo) {
		campos |= CampoNotaConteudo
	}
	if m.MudarCor {
		atual := ""
		if antes.Cor != nil {
			atual = *antes.Cor
		}
		if m.Cor != atual {
			campos |= CampoNotaCor
		}
	}
	if m.Fixada != nil && *m.Fixada != antes.Fixada {
		campos |= CampoNotaFixada
	}
	if m.Arquivada != nil && *m.Arquivada != antes.Arquivada {
		campos |= CampoNotaArquivada
	}
	return campos
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
	return r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
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
		return fila.anotar(lixeiraDaNotaNaFila(ctx, tx, id, agora))
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
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
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
		return fila.anotar(emitirNota(ctx, tx, id, CampoNotaLixeira|CampoNotaPosicao, agora))
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

func (r *SQLiteNotaRepository) ApagarDeVez(ctx context.Context, userID, id string) error {
	agora := r.now()
	return r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		var deletedAt sql.NullString
		var versao sql.NullInt64
		err := tx.QueryRowContext(ctx,
			`SELECT deleted_at, server_version FROM local_notes WHERE id = ? AND user_id = ?`, id, userID).Scan(&deletedAt, &versao)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo a nota: %w", err)
		}
		if !deletedAt.Valid {
			return ErrNotaForaDaLixeira
		}
		if err := apagarNotaDaTabela(ctx, tx, id); err != nil {
			return err
		}
		return fila.anotar(lapideLocal(ctx, tx, EntidadeNota, id, versao.Valid, agora))
	})
}

// apagarNotaDaTabela apaga a nota e as ligações dela com os marcadores, na
// transação. A fila fica com quem chama (a lápide, ou a resposta da nuvem).
func apagarNotaDaTabela(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_labels WHERE note_id = ?`, id); err != nil {
		return fmt.Errorf("storage: apagando os marcadores da nota: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM local_notes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: apagando a nota: %w", err)
	}
	return nil
}

func (r *SQLiteNotaRepository) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	apagadas := 0
	agora := now.UTC()
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		apagadas = 0
		linhas, err := tx.QueryContext(ctx,
			`SELECT id, server_version IS NOT NULL FROM local_notes
			 WHERE deleted_at IS NOT NULL AND purge_at IS NOT NULL AND purge_at <= ?`, emTexto(agora))
		if err != nil {
			return fmt.Errorf("storage: lendo as notas vencidas: %w", err)
		}
		type vencida struct {
			id        string
			comVersao bool
		}
		var vencidas []vencida
		for linhas.Next() {
			var v vencida
			if err := linhas.Scan(&v.id, &v.comVersao); err != nil {
				linhas.Close()
				return fmt.Errorf("storage: lendo as notas vencidas: %w", err)
			}
			vencidas = append(vencidas, v)
		}
		if err := linhas.Err(); err != nil {
			linhas.Close()
			return fmt.Errorf("storage: lendo as notas vencidas: %w", err)
		}
		linhas.Close()
		// Cada nota expurgada vira a lápide local dela (a nuvem que a tem fica
		// sabendo; a que nunca a teve não ouve nada), na mesma transação.
		for _, v := range vencidas {
			if err := apagarNotaDaTabela(ctx, tx, v.id); err != nil {
				return fmt.Errorf("storage: expurgando as notas: %w", err)
			}
			if err := fila.anotar(lapideLocal(ctx, tx, EntidadeNota, v.id, v.comVersao, agora)); err != nil {
				return err
			}
			apagadas++
		}
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
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
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
		if err := subirANota(ctx, tx, userID, notaID, agora); err != nil {
			return err
		}
		return fila.anotar(emitirNota(ctx, tx, notaID, CampoNotaMarcadores, agora))
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, notaID)
}

func (r *SQLiteNotaRepository) Desmarcar(ctx context.Context, userID, notaID, marcadorID string) (Nota, error) {
	agora := r.now()
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
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
		if err := subirANota(ctx, tx, userID, notaID, agora); err != nil {
			return err
		}
		return fila.anotar(emitirNota(ctx, tx, notaID, CampoNotaMarcadores, agora))
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
	err = r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
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
		return fila.anotar(emitirMarcadorDeNota(ctx, tx, id, 0, agora))
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
	err = r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		var versao int64
		var nomeAntes string
		err := tx.QueryRowContext(ctx,
			`SELECT version, nome FROM local_labels WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, id, userID).Scan(&versao, &nomeAntes)
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
		if limpo == nomeAntes {
			return nil // o mesmo nome: nada a subir
		}
		return fila.anotar(emitirMarcadorDeNota(ctx, tx, id, CampoMarcadorNome, agora))
	})
	if err != nil {
		return MarcadorDeNota{}, err
	}
	return r.getMarcador(ctx, userID, id)
}

func (r *SQLiteNotaRepository) ApagarMarcador(ctx context.Context, userID, id string) error {
	agora := r.now()
	return r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		var versao sql.NullInt64
		err := tx.QueryRowContext(ctx,
			`SELECT server_version FROM local_labels WHERE id = ? AND user_id = ? AND deleted_at IS NULL`, id, userID).Scan(&versao)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("storage: lendo o marcador: %w", err)
		}
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
		// Tira o marcador das notas (a mudança de cada uma sobe a versão e
		// entra na fila com CampoNotaMarcadores: a nuvem não sobe a versão das
		// notas ao apagar o marcador, e quem manda as notas mudadas é o PC que
		// apagou; a hora de edição não muda, para a nota não pular para o topo
		// da lista só porque um marcador sumiu).
		notas, err := lerIDs(ctx, tx,
			`SELECT note_id FROM local_note_labels WHERE label_id = ? ORDER BY note_id`, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE local_notes SET version = version + 1
			 WHERE user_id = ? AND id IN (SELECT note_id FROM local_note_labels WHERE label_id = ?)`,
			userID, id); err != nil {
			return fmt.Errorf("storage: subindo a versão das notas do marcador: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_labels WHERE label_id = ?`, id); err != nil {
			return fmt.Errorf("storage: tirando o marcador das notas: %w", err)
		}
		for _, n := range notas {
			if err := fila.anotar(emitirNota(ctx, tx, n, CampoNotaMarcadores, agora)); err != nil {
				return err
			}
		}
		// O marcador fica na tabela (soft, como sempre), e a fila leva a
		// lápide dele (lapideLocal).
		return fila.anotar(lapideLocal(ctx, tx, EntidadeMarcadorDeNota, id, versao.Valid, agora))
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
