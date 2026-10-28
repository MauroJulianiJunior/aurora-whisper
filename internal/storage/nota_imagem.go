package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
)

// As imagens das notas no PC (ADR-022, "As imagens (N10)"; migration 0014).
//
// # Onde ficam
//
// A linha (local_note_images) guarda o tamanho, o SHA-256 e a hora; os bytes são
// um arquivo <pasta>\<id>.jpg (UsarPastaDasImagens; no Agent, <dados>\notas-imagens).
// O nome original do arquivo nunca chega aqui. O que chega passa antes por
// imagemdanota.Conferir: só JPEG, já sem os metadados.
//
// # A ordem das gravações
//
// Anexar grava o arquivo (temporário e rename, dentro da transação) e só então a
// linha: com a linha, o arquivo existe. Tirar e apagar a nota apagam a linha na
// transação e o arquivo depois do commit. O que um desligamento no meio deixa
// para trás é arquivo sem linha (ou o temporário), e a varredura da subida
// (VarrerImagens) e a da rodada do expurgo o tiram.
//
// # O que fica para o sync (a segunda metade da N10)
//
// A imagem é imutável: o id (UUID v7) nasce aqui, e o SHA-256 vai junto para o
// outro lado conferir o que chegou. Anexar e tirar NÃO mudam a versão nem a hora
// de edição da nota (a imagem é uma linha à parte), e nada aqui põe marcador na
// sync_outbox.

// Os erros das imagens (além de ErrNotFound e ErrNotaNaLixeira). Os de
// imagemdanota (ErrNaoEJPEG, ErrGrandeDemais, ErrLadoGrande, ErrInvalida) passam
// como vieram.
var (
	// ErrImagensDemais: a nota já tem imagemdanota.MaxPorNota imagens.
	ErrImagensDemais = errors.New("storage: a nota já tem o máximo de imagens")
	// ErrTetoDasImagens: com esta, o total das imagens da pessoa passaria de
	// imagemdanota.TetoDoTotal.
	ErrTetoDasImagens = errors.New("storage: as imagens das notas chegaram ao teto")
	// ErrImagensSemPasta: o repositório não tem a pasta das imagens
	// (UsarPastaDasImagens não foi chamado).
	ErrImagensSemPasta = errors.New("storage: a pasta das imagens das notas não está montada")
	// ErrImagemEstragada: o arquivo da imagem não é o que a linha diz (o tamanho
	// ou o SHA-256 não conferem).
	ErrImagemEstragada = errors.New("storage: o arquivo da imagem não confere")
)

// ImagemDaNota é a imagem como a lista das notas a mostra: só os metadados.
type ImagemDaNota struct {
	ID      string
	NotaID  string
	Largura int
	Altura  int
	// Bytes é o tamanho do arquivo, em bytes.
	Bytes     int64
	SHA256    string
	CreatedAt time.Time
}

// ImagemDaNotaRepository é o que a API local usa das imagens. O
// *SQLiteNotaRepository o cumpre.
type ImagemDaNotaRepository interface {
	// AnexarImagem confere o JPEG (imagemdanota.Conferir), guarda a cópia limpa e
	// a põe no fim das imagens da nota. Nota inexistente: ErrNotFound; na lixeira:
	// ErrNotaNaLixeira; com o máximo de imagens: ErrImagensDemais; acima do teto
	// do total: ErrTetoDasImagens.
	AnexarImagem(ctx context.Context, userID, notaID string, jpeg []byte) (ImagemDaNota, error)

	// CriarNotaComImagem é o ícone de imagem do "Criar uma nota…": a nota nasce
	// com a imagem, numa transação só, e pode não ter texto.
	CriarNotaComImagem(ctx context.Context, p NovaNota, jpeg []byte) (Nota, error)

	// LerImagem devolve os metadados e os bytes da imagem id da nota notaID (a da
	// lixeira também). ErrNotFound se a imagem não existe, é de outra nota ou de
	// outra pessoa, ou se o arquivo sumiu; ErrImagemEstragada se o arquivo não
	// confere com a linha.
	LerImagem(ctx context.Context, userID, notaID, id string) (ImagemDaNota, []byte, error)

	// TirarImagem tira a imagem da nota de vez: a linha e o arquivo. Vale também
	// na nota da lixeira (o Desfazer do painel pode vencer depois de a nota ir
	// para lá). ErrNotFound se não existe.
	TirarImagem(ctx context.Context, userID, notaID, id string) error
}

var _ ImagemDaNotaRepository = (*SQLiteNotaRepository)(nil)

// idDeImagem é a forma do id de uma imagem (um UUID, como storage.NewID gera):
// só ele vira nome de arquivo.
var idDeImagem = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// extensaoDaImagem e prefixoTemporario: o arquivo da imagem é <id>.jpg, e o
// temporário da gravação, .gravando-<id>-*.tmp (a varredura o apaga).
const (
	extensaoDaImagem  = ".jpg"
	prefixoTemporario = ".gravando-"
	sufixoTemporario  = ".tmp"
	colunasDaImagem   = `id, note_id, largura, altura, bytes, sha256, created_at`
	permissaoDaPasta  = 0o700
)

// UsarPastaDasImagens diz onde ficam os arquivos das imagens. A pasta nasce na
// primeira gravação. Vazio desliga as imagens (as rotas respondem 503); as
// linhas que já existem continuam na lista das notas.
func (r *SQLiteNotaRepository) UsarPastaDasImagens(dir string) {
	r.pastaDasImagens.Store(&dir)
}

func (r *SQLiteNotaRepository) pasta() string {
	if p := r.pastaDasImagens.Load(); p != nil {
		return *p
	}
	return ""
}

// arquivoDaImagem é o caminho do arquivo da imagem id na pasta dir. Um id fora da
// forma nunca vira caminho.
func arquivoDaImagem(dir, id string) (string, error) {
	if !idDeImagem.MatchString(id) {
		return "", fmt.Errorf("storage: id de imagem inválido")
	}
	return filepath.Join(dir, id+extensaoDaImagem), nil
}

func lerImagem(s scanner) (ImagemDaNota, error) {
	var im ImagemDaNota
	var criada string
	if err := s.Scan(&im.ID, &im.NotaID, &im.Largura, &im.Altura, &im.Bytes, &im.SHA256, &criada); err != nil {
		return ImagemDaNota{}, err
	}
	var err error
	if im.CreatedAt, err = deTempo(criada); err != nil {
		return ImagemDaNota{}, err
	}
	return im, nil
}

// imagensDasNotas devolve as imagens por nota, na ordem da criação. notaID vazio
// traz as de todas as notas do usuário.
func imagensDasNotas(ctx context.Context, q consulta, userID, notaID string) (map[string][]ImagemDaNota, error) {
	consultaSQL := `SELECT ` + prefixar("i.", colunasDaImagem) + ` FROM local_note_images i
		JOIN local_notes n ON n.id = i.note_id WHERE n.user_id = ?`
	args := []any{userID}
	if notaID != "" {
		consultaSQL += ` AND i.note_id = ?`
		args = append(args, notaID)
	}
	consultaSQL += ` ORDER BY i.created_at, i.rowid`
	linhas, err := q.QueryContext(ctx, consultaSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: lendo as imagens das notas: %w", err)
	}
	defer linhas.Close()
	porNota := map[string][]ImagemDaNota{}
	for linhas.Next() {
		im, err := lerImagem(linhas)
		if err != nil {
			return nil, fmt.Errorf("storage: lendo as imagens das notas: %w", err)
		}
		porNota[im.NotaID] = append(porNota[im.NotaID], im)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo as imagens das notas: %w", err)
	}
	return porNota, nil
}

// idsDasImagensDaNota lê, na transação, os ids das imagens da nota: o que quem
// apaga a nota apaga do disco depois do commit.
func idsDasImagensDaNota(ctx context.Context, tx *sql.Tx, notaID string) ([]string, error) {
	return lerIDs(ctx, tx, `SELECT id FROM local_note_images WHERE note_id = ? ORDER BY rowid`, notaID)
}

// apagarArquivosDasImagens apaga os arquivos das imagens ids, depois do commit que
// tirou as linhas. Melhor esforço: o que não sair aqui (o arquivo aberto por outro
// programa, um disco com erro) a varredura tira depois.
func (r *SQLiteNotaRepository) apagarArquivosDasImagens(ids []string) {
	dir := r.pasta()
	if dir == "" {
		return
	}
	for _, id := range ids {
		if caminho, err := arquivoDaImagem(dir, id); err == nil {
			_ = os.Remove(caminho)
		}
	}
}

// escreverArquivoDaImagem grava b em <dir>\<id>.jpg: num temporário da mesma
// pasta, com o Sync, e depois o rename. Quem lê nunca vê o arquivo pela metade.
func escreverArquivoDaImagem(dir, id string, b []byte) (string, error) {
	destino, err := arquivoDaImagem(dir, id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, permissaoDaPasta); err != nil {
		return "", fmt.Errorf("storage: criando a pasta das imagens: %w", err)
	}
	tmp, err := os.CreateTemp(dir, prefixoTemporario+id+"-*"+sufixoTemporario)
	if err != nil {
		return "", fmt.Errorf("storage: criando o arquivo da imagem: %w", err)
	}
	nomeTmp := tmp.Name()
	falhou := func(e error) (string, error) {
		_ = tmp.Close()
		_ = os.Remove(nomeTmp)
		return "", fmt.Errorf("storage: gravando o arquivo da imagem: %w", e)
	}
	// O CreateTemp já cria só para o dono (0600). No Windows quem vale é a
	// permissão herdada da pasta de dados (só a conta de quem usa, ADR-018).
	if _, err := tmp.Write(b); err != nil {
		return falhou(err)
	}
	if err := tmp.Sync(); err != nil {
		return falhou(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(nomeTmp)
		return "", fmt.Errorf("storage: gravando o arquivo da imagem: %w", err)
	}
	if err := os.Rename(nomeTmp, destino); err != nil {
		_ = os.Remove(nomeTmp)
		return "", fmt.Errorf("storage: gravando o arquivo da imagem: %w", err)
	}
	return destino, nil
}

// cabeNaNota confere, na transação, o máximo por nota e o teto do total, contando
// a imagem nova (de tamanho n).
func cabeNaNota(ctx context.Context, tx *sql.Tx, userID, notaID string, n int) error {
	var naNota int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_note_images WHERE note_id = ?`, notaID).Scan(&naNota); err != nil {
		return fmt.Errorf("storage: contando as imagens da nota: %w", err)
	}
	if naNota >= imagemdanota.MaxPorNota {
		return ErrImagensDemais
	}
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(i.bytes), 0) FROM local_note_images i
		JOIN local_notes n ON n.id = i.note_id WHERE n.user_id = ?`, userID).Scan(&total); err != nil {
		return fmt.Errorf("storage: somando as imagens das notas: %w", err)
	}
	if total+int64(n) > imagemdanota.TetoDoTotal {
		return ErrTetoDasImagens
	}
	return nil
}

// guardarImagem é o miolo do AnexarImagem e do CriarNotaComImagem: confere o
// lugar, grava o arquivo e a linha, na transação tx. Devolve o caminho do arquivo
// gravado, que quem chama apaga se a transação não vingar.
func guardarImagem(ctx context.Context, tx *sql.Tx, dir, userID, notaID string, img imagemdanota.Limpa, id string, agora time.Time) (string, error) {
	if err := cabeNaNota(ctx, tx, userID, notaID, len(img.Bytes)); err != nil {
		return "", err
	}
	caminho, err := escreverArquivoDaImagem(dir, id, img.Bytes)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO local_note_images (`+colunasDaImagem+`) VALUES (?,?,?,?,?,?,?)`,
		id, notaID, img.Largura, img.Altura, len(img.Bytes), img.SHA256, emTexto(agora)); err != nil {
		return caminho, fmt.Errorf("storage: gravando a imagem: %w", err)
	}
	return caminho, nil
}

func (r *SQLiteNotaRepository) AnexarImagem(ctx context.Context, userID, notaID string, jpeg []byte) (ImagemDaNota, error) {
	img, err := imagemdanota.Conferir(jpeg)
	if err != nil {
		return ImagemDaNota{}, err
	}
	dir := r.pasta()
	if dir == "" {
		return ImagemDaNota{}, ErrImagensSemPasta
	}
	agora := r.now()
	id := NewID()
	r.imagensMu.Lock()
	defer r.imagensMu.Unlock()
	var gravado string
	err = r.gravar(ctx, func(tx *sql.Tx, _ *naFila) error {
		if err := conferirNotaEditavel(ctx, tx, userID, notaID, -1); err != nil {
			return err
		}
		var err error
		gravado, err = guardarImagem(ctx, tx, dir, userID, notaID, img, id, agora)
		return err
	})
	if err != nil {
		if gravado != "" {
			_ = os.Remove(gravado)
		}
		return ImagemDaNota{}, err
	}
	return ImagemDaNota{ID: id, NotaID: notaID, Largura: img.Largura, Altura: img.Altura, Bytes: int64(len(img.Bytes)),
		SHA256: img.SHA256, CreatedAt: agora}, nil
}

func (r *SQLiteNotaRepository) CriarNotaComImagem(ctx context.Context, p NovaNota, jpeg []byte) (Nota, error) {
	if p.UserID == "" {
		return Nota{}, fmt.Errorf("storage: CriarNotaComImagem requer UserID")
	}
	if err := validarTextoDaNota(&p.Titulo, &p.Corpo); err != nil {
		return Nota{}, err
	}
	if err := validarCor(p.Cor); err != nil {
		return Nota{}, err
	}
	if p.Token != "" {
		// O ícone de imagem é do "Criar uma nota…" fechado: não há caixa aberta, nem
		// ditado feito nela.
		return Nota{}, ErrTokenInvalido
	}
	img, err := imagemdanota.Conferir(jpeg)
	if err != nil {
		return Nota{}, err
	}
	dir := r.pasta()
	if dir == "" {
		return Nota{}, ErrImagensSemPasta
	}
	agora := r.now()
	id, idDaImagem := NewID(), NewID()
	r.imagensMu.Lock()
	defer r.imagensMu.Unlock()
	var gravado string
	err = r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		// A nota sem texto vale aqui: ela tem a imagem (a nota "só imagem" da prévia).
		if err := inserirNota(ctx, tx, id, agora, p); err != nil {
			return err
		}
		if err := fila.anotar(emitirNota(ctx, tx, id, 0, agora)); err != nil {
			return err
		}
		var err error
		gravado, err = guardarImagem(ctx, tx, dir, p.UserID, id, img, idDaImagem, agora)
		return err
	})
	if err != nil {
		if gravado != "" {
			_ = os.Remove(gravado)
		}
		return Nota{}, err
	}
	return r.Get(ctx, p.UserID, id)
}

func (r *SQLiteNotaRepository) LerImagem(ctx context.Context, userID, notaID, id string) (ImagemDaNota, []byte, error) {
	im, err := lerImagem(r.db.QueryRowContext(ctx, `SELECT `+prefixar("i.", colunasDaImagem)+` FROM local_note_images i
		JOIN local_notes n ON n.id = i.note_id WHERE i.id = ? AND i.note_id = ? AND n.user_id = ?`, id, notaID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return ImagemDaNota{}, nil, ErrNotFound
	}
	if err != nil {
		return ImagemDaNota{}, nil, fmt.Errorf("storage: lendo a imagem: %w", err)
	}
	dir := r.pasta()
	if dir == "" {
		return ImagemDaNota{}, nil, ErrImagensSemPasta
	}
	caminho, err := arquivoDaImagem(dir, im.ID)
	if err != nil {
		return ImagemDaNota{}, nil, ErrImagemEstragada
	}
	b, err := os.ReadFile(caminho)
	if errors.Is(err, fs.ErrNotExist) {
		return ImagemDaNota{}, nil, ErrNotFound
	}
	if err != nil {
		return ImagemDaNota{}, nil, fmt.Errorf("storage: lendo o arquivo da imagem: %w", err)
	}
	soma := sha256.Sum256(b)
	if int64(len(b)) != im.Bytes || hex.EncodeToString(soma[:]) != im.SHA256 {
		return ImagemDaNota{}, nil, ErrImagemEstragada
	}
	return im, b, nil
}

func (r *SQLiteNotaRepository) TirarImagem(ctx context.Context, userID, notaID, id string) error {
	err := r.gravar(ctx, func(tx *sql.Tx, _ *naFila) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM local_note_images WHERE id = ? AND note_id = ?
			AND note_id IN (SELECT id FROM local_notes WHERE user_id = ?)`, id, notaID, userID)
		if err != nil {
			return fmt.Errorf("storage: tirando a imagem da nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	r.apagarArquivosDasImagens([]string{id})
	return nil
}

// VarreduraDasImagens é o que VarrerImagens tirou: só contagens.
type VarreduraDasImagens struct {
	// ArquivosSemLinha: arquivos de imagem que nenhuma linha cita (um
	// desligamento entre o commit e o apagar do arquivo, ou a nota que o sync
	// apagou), apagados.
	ArquivosSemLinha int
	// LinhasSemArquivo: linhas cujo arquivo sumiu, apagadas (a imagem já não
	// existia; a nota fica).
	LinhasSemArquivo int
	// Temporarios: restos de uma gravação que não terminou, apagados.
	Temporarios int
}

// Vazia diz se a varredura não achou nada.
func (v VarreduraDasImagens) Vazia() bool {
	return v.ArquivosSemLinha == 0 && v.LinhasSemArquivo == 0 && v.Temporarios == 0
}

// VarrerImagens põe a pasta e a tabela de acordo: apaga o arquivo sem linha, a
// linha sem arquivo e o temporário que sobrou. É a da subida do Agent, antes de o
// painel abrir. Nomes que não são de imagem nem temporário da gravação (um
// desktop.ini que o Windows ponha ali) ficam. Sem a pasta montada, não faz nada.
func (r *SQLiteNotaRepository) VarrerImagens(ctx context.Context) (VarreduraDasImagens, error) {
	return r.varrer(ctx, true)
}

// varrer é VarrerImagens; comLinhas falso só apaga arquivos (a rodada do
// expurgo, que corre com o Agent em uso: a linha sem arquivo é coisa da subida).
func (r *SQLiteNotaRepository) varrer(ctx context.Context, comLinhas bool) (VarreduraDasImagens, error) {
	var v VarreduraDasImagens
	dir := r.pasta()
	if dir == "" {
		return v, nil
	}
	// Uma anexação no meio (arquivo gravado, linha ainda não) não pode ser vista
	// como arquivo sem linha.
	r.imagensMu.Lock()
	defer r.imagensMu.Unlock()
	entradas, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return v, fmt.Errorf("storage: lendo a pasta das imagens: %w", err)
	}
	noDisco := map[string]bool{}
	for _, e := range entradas {
		nome := e.Name()
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(nome, prefixoTemporario) && strings.HasSuffix(nome, sufixoTemporario) {
			if os.Remove(filepath.Join(dir, nome)) == nil {
				v.Temporarios++
			}
			continue
		}
		if id, ok := strings.CutSuffix(nome, extensaoDaImagem); ok && idDeImagem.MatchString(id) {
			noDisco[id] = true
		}
	}
	ids, err := lerIDs(ctx, r.db, `SELECT id FROM local_note_images`)
	if err != nil {
		return v, err
	}
	naTabela := map[string]bool{}
	var semArquivo []string
	for _, id := range ids {
		naTabela[id] = true
		if !noDisco[id] {
			semArquivo = append(semArquivo, id)
		}
	}
	for id := range noDisco {
		if naTabela[id] {
			continue
		}
		if caminho, err := arquivoDaImagem(dir, id); err == nil && os.Remove(caminho) == nil {
			v.ArquivosSemLinha++
		}
	}
	if !comLinhas || len(semArquivo) == 0 {
		return v, nil
	}
	err = r.gravar(ctx, func(tx *sql.Tx, _ *naFila) error {
		v.LinhasSemArquivo = 0
		for _, id := range semArquivo {
			res, err := tx.ExecContext(ctx, `DELETE FROM local_note_images WHERE id = ?`, id)
			if err != nil {
				return fmt.Errorf("storage: tirando a imagem sem arquivo: %w", err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				v.LinhasSemArquivo++
			}
		}
		return nil
	})
	return v, err
}
