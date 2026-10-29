package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/imagemdanota"
)

// O sync das imagens das notas no PC (ADR-022, "Decisões da N10b"; a 0015).
//
// A imagem é uma entidade à parte da nota, imutável, com o entity_type
// note_image. Os bytes NUNCA vão no push nem no pull: sobem pelo PUT e descem
// pelo GET do motor (internal/sync), fora de qualquer transação; aqui fica o
// SQL da fila e das linhas.
//
//   - Anexar com a nuvem conectada põe o marcador de criar (campos 0) na mesma
//     transação (emitirImagem); desconectado, a completação o põe depois do
//     aceite (CompletarImagens), como as notas da N7.
//   - Tirar da nota põe a lápide local (campos = CampoApagado, com a nota no
//     nota_id) se a nuvem tem ou pode ter a imagem (na_nuvem); senão, o
//     marcador sai e nada sobe (lapideDaImagem).
//   - A imagem sobe só depois da nota (a nota com server_version) e fora da
//     lixeira; a espera de uma recusa que pode passar (tentativas, tentou_em)
//     segue a BackoffPolicy das imagens, que o motor dá.
//   - A que vem da nuvem (a mudança note_image do pull, ou a lista imagens de
//     uma nota) nasce "aguardando": a linha sem o arquivo, que a varredura
//     respeita. GuardarBaixada grava o arquivo (temporário e rename, dentro da
//     transação) e só então a põe "normal". O PC aceita a imagem da nuvem
//     mesmo acima do teto local: quem decide o teto é a nuvem.
//
// Nada de bytes, tamanho, nome nem texto no log: os erros daqui só dizem o que
// falhou.

// EntidadeImagemDaNota é o entity_type da imagem da nota no sync.
const EntidadeImagemDaNota = "note_image"

// ChaveNuvemImagens é a capacidade das imagens (a tabela instalacao, com a hora
// em que o PC a viu): a nuvem disse, no pull, que sincroniza note_image. Sem
// ela, nada de imagem sobe nem desce. O "Esquecer esta nuvem" a apaga.
const ChaveNuvemImagens = "nuvem_imagens"

// ─── A emissão (na transação de quem grava) ───

// emitirImagem põe o marcador de criar da imagem id (da nota notaID) na fila,
// na transação do anexo, se a nuvem está conectada. Devolve se pôs.
func emitirImagem(ctx context.Context, tx *sql.Tx, id, notaID string, agora time.Time) (bool, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, nota_id)
		SELECT ?, ?, 0, ?, ?
		WHERE EXISTS (SELECT 1 FROM instalacao WHERE nome = ? AND valor <> '')
		ON CONFLICT (entity_type, entity_id) DO NOTHING`,
		EntidadeImagemDaNota, id, emTexto(agora), notaID, ChaveNuvemAparelho)
	if err != nil {
		return false, fmt.Errorf("storage: pondo a imagem na fila do sync: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: pondo a imagem na fila do sync: %w", err)
	}
	return n > 0, nil
}

// lapideDaImagem é o marcador do "Tirar da nota", na transação de quem tira,
// DEPOIS de a linha sair: com naNuvem (a nuvem tem, ou pode ter, a imagem), o
// marcador fica com campos = CampoApagado e a nota no nota_id (o DELETE sobe,
// conectado ou não); sem, o marcador sai, e nada sobe. Devolve se o marcador
// ficou.
func lapideDaImagem(ctx context.Context, tx *sql.Tx, id, notaID string, naNuvem bool, agora time.Time) (bool, error) {
	if !naNuvem {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`,
			EntidadeImagemDaNota, id); err != nil {
			return false, fmt.Errorf("storage: tirando da fila a imagem tirada da nota: %w", err)
		}
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, nota_id)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (entity_type, entity_id) DO UPDATE SET campos = excluded.campos, nota_id = excluded.nota_id,
		  mutation_id = NULL, versao_enviada = NULL`,
		EntidadeImagemDaNota, id, CampoApagado, emTexto(agora), notaID); err != nil {
		return false, fmt.Errorf("storage: pondo a lápide da imagem na fila do sync: %w", err)
	}
	return true, nil
}

// tirarMarcadoresDasImagensDaNota tira da fila os marcadores das imagens da
// nota que sai de vez (apagarNotaDaTabela), antes das linhas: a lápide da nota
// leva as imagens na nuvem (a cascata), e a do PC também.
func tirarMarcadoresDasImagensDaNota(ctx context.Context, tx *sql.Tx, notaID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ?
		AND (nota_id = ? OR entity_id IN (SELECT id FROM local_note_images WHERE note_id = ?))`,
		EntidadeImagemDaNota, notaID, notaID); err != nil {
		return fmt.Errorf("storage: tirando da fila as imagens da nota apagada: %w", err)
	}
	return nil
}

// ─── A capacidade e a completação ───

func temCapacidadeDeImagens(ctx context.Context, q consulta) (bool, error) {
	_, ok, err := lerChave(ctx, q, ChaveNuvemImagens)
	return ok, err
}

// TemCapacidadeDeImagens diz se a nuvem desta conexão já disse que sincroniza
// as imagens das notas.
func (f *Fila) TemCapacidadeDeImagens(ctx context.Context) (bool, error) {
	return temCapacidadeDeImagens(ctx, f.db)
}

// VerCapacidadeDasImagens é o que o pull viu em "entidades" (temImagens: a
// lista tem note_image), como VerCapacidade das notas: na primeira vez, grava a
// chave e devolve primeira (o motor zera o cursor e lê tudo de novo: um binário
// de antes da N10b passou pelas imagens sem as gravar, e as notas voltam com a
// lista imagens); sem a capacidade (uma volta de versão da nuvem), tira a chave.
func (f *Fila) VerCapacidadeDasImagens(ctx context.Context, temImagens bool, agora time.Time) (primeira bool, err error) {
	err = naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		primeira = false
		tem, err := temCapacidadeDeImagens(ctx, tx)
		if err != nil {
			return err
		}
		switch {
		case !temImagens && tem:
			if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome = ?`, ChaveNuvemImagens); err != nil {
				return fmt.Errorf("storage: tirando a capacidade das imagens: %w", err)
			}
		case temImagens && !tem:
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`,
				ChaveNuvemImagens, emTexto(agora)); err != nil {
				return fmt.Errorf("storage: gravando a capacidade das imagens: %w", err)
			}
			primeira = true
		}
		return nil
	})
	return primeira, err
}

// CompletarImagens é o envio inicial das imagens, em toda rodada (como
// CompletarNotas): com o aceite (ChaveNuvemEnvioAceitoEm) e a capacidade
// (ChaveNuvemImagens), põe o marcador de criar em toda imagem com o arquivo
// aqui (normal) que a nuvem nunca viu (na_nuvem 0), de nota fora da lixeira, sem
// marcador. É o que leva as imagens anexadas antes desta versão (ou com a nuvem
// desconectada). A imagem da nota da lixeira fica: sobe se a nota voltar.
// Devolve quantas marcou.
func (f *Fila) CompletarImagens(ctx context.Context, agora time.Time) (int, error) {
	marcadas := 0
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		marcadas = 0
		if _, aceito, err := lerChave(ctx, tx, ChaveNuvemEnvioAceitoEm); err != nil || !aceito {
			return err
		}
		if ok, err := temCapacidadeDeImagens(ctx, tx); err != nil || !ok {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, nota_id)
			SELECT ?, i.id, 0, ?, i.note_id FROM local_note_images i JOIN local_notes n ON n.id = i.note_id
			WHERE n.user_id = ? AND n.deleted_at IS NULL AND i.na_nuvem = 0 AND i.sync_state = ?
			  AND NOT EXISTS (SELECT 1 FROM sync_outbox o WHERE o.entity_type = ? AND o.entity_id = i.id)
			ORDER BY i.created_at, i.rowid
			ON CONFLICT (entity_type, entity_id) DO NOTHING`,
			EntidadeImagemDaNota, emTexto(agora), f.dono, SituacaoNormal, EntidadeImagemDaNota)
		if err != nil {
			return fmt.Errorf("storage: pondo as imagens na fila do sync: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("storage: pondo as imagens na fila do sync: %w", err)
		}
		marcadas = int(n)
		return nil
	})
	return marcadas, err
}

// ─── O envio (o PUT e o DELETE) ───

// LinhaDaImagemNoSync é a linha da imagem como o sync a vê: os metadados e a
// situação do sync dela.
type LinhaDaImagemNoSync struct {
	ImagemDaNota
	// NaNuvem: a nuvem tem, ou pode ter, a imagem (na_nuvem).
	NaNuvem bool
	// Situacao é SituacaoNormal, SituacaoAguardando ou SituacaoRecusado.
	Situacao   string
	Tentativas int
	TentouEm   *time.Time
}

// ItemDeImagemNaFila é um marcador note_image a subir: o de criar, com a linha,
// ou a lápide (Lapide, sem a linha). NotaID é a nota da imagem (o caminho do
// PUT e do DELETE).
type ItemDeImagemNaFila struct {
	ID     string
	NotaID string
	Lapide bool
	Linha  *LinhaDaImagemNoSync
}

const colunasDaImagemNoSync = colunasLidasDaImagem + `, na_nuvem, tentativas, tentou_em`

func lerImagemNoSync(s scanner) (LinhaDaImagemNoSync, error) {
	var l LinhaDaImagemNoSync
	var criada, tentou sql.NullString
	var naNuvem int
	if err := s.Scan(&l.ID, &l.NotaID, &l.Largura, &l.Altura, &l.Bytes, &l.SHA256, &criada, &l.Situacao,
		&naNuvem, &l.Tentativas, &tentou); err != nil {
		return LinhaDaImagemNoSync{}, err
	}
	var err error
	if l.CreatedAt, err = deTempo(criada.String); err != nil {
		return LinhaDaImagemNoSync{}, err
	}
	if l.TentouEm, err = deTempoNulo(tentou); err != nil {
		return LinhaDaImagemNoSync{}, err
	}
	if l.Situacao == SituacaoAguardando {
		l.Estado = EstadoACaminho
	}
	l.NaNuvem = naNuvem != 0
	return l, nil
}

// linhaDaImagemNoSync lê a imagem id do dono, ou nil.
func (f *Fila) linhaDaImagemNoSync(ctx context.Context, q consulta, id string) (*LinhaDaImagemNoSync, error) {
	l, err := lerImagemNoSync(q.QueryRowContext(ctx, `SELECT `+prefixar("i.", colunasDaImagemNoSync)+`
		FROM local_note_images i JOIN local_notes n ON n.id = i.note_id WHERE i.id = ? AND n.user_id = ?`, id, f.dono))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: lendo a imagem: %w", err)
	}
	return &l, nil
}

// esperando diz se a próxima tentativa da linha ainda não chegou: depois de
// tentativas falhas, ela espera espera(tentativas) desde tentou_em.
func esperando(tentativas int, tentouEm *time.Time, agora time.Time, espera func(int) time.Duration) bool {
	if tentativas <= 0 || tentouEm == nil || espera == nil {
		return false
	}
	return agora.Before(tentouEm.Add(espera(tentativas)))
}

// PendentesDeImagens devolve até limite marcadores note_image a subir, na ordem
// em que entraram: as lápides (o DELETE) e as imagens a criar (o PUT) cuja vez
// chegou. Vazia sem a capacidade (ChaveNuvemImagens). Numa transação, e sem
// devolver:
//   - a imagem cuja nota a nuvem ainda não tem (server_version nulo: o criar
//     dela ainda está na fila) ou que está na lixeira daqui: espera a rodada
//     seguinte, com o marcador;
//   - a que espera a vez de uma recusa que pode passar (espera, a
//     BackoffPolicy das imagens, a partir de tentou_em);
//   - o marcador de criar sem a linha (a imagem saiu sem passar pela lápide) ou
//     de uma linha que não é normal (a que veio da nuvem, a recusada): sai da
//     fila aqui mesmo.
func (f *Fila) PendentesDeImagens(ctx context.Context, limite int, agora time.Time, espera func(int) time.Duration) ([]ItemDeImagemNaFila, error) {
	if limite <= 0 {
		return nil, nil
	}
	var itens []ItemDeImagemNaFila
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		itens = nil
		if ok, err := temCapacidadeDeImagens(ctx, tx); err != nil || !ok {
			return err
		}
		type marcador struct {
			id, nota string
			campos   int
		}
		linhas, err := tx.QueryContext(ctx, `SELECT entity_id, COALESCE(nota_id, ''), campos FROM sync_outbox
			WHERE entity_type = ? ORDER BY criado_em, entity_id`, EntidadeImagemDaNota)
		if err != nil {
			return fmt.Errorf("storage: lendo a fila das imagens: %w", err)
		}
		var ms []marcador
		for linhas.Next() {
			var m marcador
			if err := linhas.Scan(&m.id, &m.nota, &m.campos); err != nil {
				linhas.Close()
				return fmt.Errorf("storage: lendo a fila das imagens: %w", err)
			}
			ms = append(ms, m)
		}
		linhas.Close()
		if err := linhas.Err(); err != nil {
			return fmt.Errorf("storage: lendo a fila das imagens: %w", err)
		}
		for _, m := range ms {
			if len(itens) == limite {
				return nil
			}
			if m.campos&CampoApagado != 0 {
				if m.nota != "" {
					itens = append(itens, ItemDeImagemNaFila{ID: m.id, NotaID: m.nota, Lapide: true})
					continue
				}
				// A lápide sem a nota (não acontece: a lápide sempre a grava)
				// não tem caminho para o DELETE.
			}
			l, err := f.linhaDaImagemNoSync(ctx, tx, m.id)
			if err != nil {
				return err
			}
			if l == nil || l.Situacao != SituacaoNormal {
				if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`,
					EntidadeImagemDaNota, m.id); err != nil {
					return fmt.Errorf("storage: tirando da fila uma imagem que não sobe: %w", err)
				}
				continue
			}
			var naNuvem, naLixeira bool
			if err := tx.QueryRowContext(ctx,
				`SELECT server_version IS NOT NULL, deleted_at IS NOT NULL FROM local_notes WHERE id = ?`, l.NotaID).
				Scan(&naNuvem, &naLixeira); err != nil {
				return fmt.Errorf("storage: lendo a nota da imagem: %w", err)
			}
			if !naNuvem || naLixeira || esperando(l.Tentativas, l.TentouEm, agora, espera) {
				continue
			}
			itens = append(itens, ItemDeImagemNaFila{ID: m.id, NotaID: l.NotaID, Linha: l})
		}
		return nil
	})
	return itens, err
}

// MarcarEnvioDaImagem grava, ANTES da rede, que a nuvem pode ter a imagem
// (na_nuvem = 1): o PUT que sai pode valer mesmo sem a resposta chegar, e daí em
// diante tirar a imagem manda o DELETE.
func (f *Fila) MarcarEnvioDaImagem(ctx context.Context, id string) error {
	if _, err := f.db.ExecContext(ctx, `UPDATE local_note_images SET na_nuvem = 1 WHERE id = ? AND na_nuvem = 0`, id); err != nil {
		return fmt.Errorf("storage: marcando o envio da imagem: %w", err)
	}
	return nil
}

// AcaoDaImagemNoEnvio é o que a resposta de um PUT ou de um DELETE manda fazer
// (a decisão é do motor, pura). O zero não faz nada.
type AcaoDaImagemNoEnvio struct {
	// TirarMarcador: o envio valeu (ou não há mais o que mandar): o marcador
	// sai, se ainda é o mesmo (o de criar, ou a lápide; um criar que virou
	// lápide no meio fica).
	TirarMarcador bool
	// Esperar: uma recusa que pode passar, ou a nuvem que não respondeu
	// direito; a linha conta mais uma tentativa (tentou_em = agora).
	Esperar bool
	// Recusar: a nuvem recusou a imagem de vez; a linha fica 'recusado' (a
	// imagem fica só neste PC).
	Recusar bool
	// Subiu: a nuvem tem a imagem; as tentativas voltam a zero.
	Subiu bool
	// Erro é o código da recusa ou da espera (last_sync_error).
	Erro string
}

// AplicarEnvioDaImagem executa, numa transação, o que a resposta do envio do
// item manda fazer.
func (f *Fila) AplicarEnvioDaImagem(ctx context.Context, it ItemDeImagemNaFila, a AcaoDaImagemNoEnvio, agora time.Time) error {
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		if a.TirarMarcador {
			campos := 0
			if it.Lapide {
				campos = CampoApagado
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ? AND campos = ?`,
				EntidadeImagemDaNota, it.ID, campos); err != nil {
				return fmt.Errorf("storage: tirando da fila a imagem enviada: %w", err)
			}
		}
		var erro any
		if a.Erro != "" {
			erro = a.Erro
		}
		switch {
		case a.Subiu:
			_, err := tx.ExecContext(ctx, `UPDATE local_note_images SET na_nuvem = 1, tentativas = 0, tentou_em = NULL,
				last_sync_error = NULL WHERE id = ?`, it.ID)
			if err != nil {
				return fmt.Errorf("storage: gravando o envio da imagem: %w", err)
			}
		case a.Recusar:
			_, err := tx.ExecContext(ctx, `UPDATE local_note_images SET sync_state = ?, last_sync_error = ? WHERE id = ?
				AND sync_state = ?`, SituacaoRecusado, erro, it.ID, SituacaoNormal)
			if err != nil {
				return fmt.Errorf("storage: gravando a recusa da imagem: %w", err)
			}
		case a.Esperar:
			_, err := tx.ExecContext(ctx, `UPDATE local_note_images SET tentativas = tentativas + 1, tentou_em = ?,
				last_sync_error = ? WHERE id = ?`, emTexto(agora), erro, it.ID)
			if err != nil {
				return fmt.Errorf("storage: gravando a espera da imagem: %w", err)
			}
		}
		return nil
	})
}

// ─── O download ───

// ImagemABaixar é uma imagem que veio da nuvem e espera o arquivo.
type ImagemABaixar struct {
	ID     string
	NotaID string
	Bytes  int64
	SHA256 string
}

// ImagensABaixar devolve até limite imagens "aguardando" do dono cuja vez
// chegou (espera, a BackoffPolicy das imagens), na ordem da criação. Vazia sem
// a capacidade.
func (f *Fila) ImagensABaixar(ctx context.Context, limite int, agora time.Time, espera func(int) time.Duration) ([]ImagemABaixar, error) {
	if limite <= 0 {
		return nil, nil
	}
	if ok, err := temCapacidadeDeImagens(ctx, f.db); err != nil || !ok {
		return nil, err
	}
	linhas, err := f.db.QueryContext(ctx, `SELECT `+prefixar("i.", colunasDaImagemNoSync)+`
		FROM local_note_images i JOIN local_notes n ON n.id = i.note_id
		WHERE n.user_id = ? AND i.sync_state = ? ORDER BY i.created_at, i.rowid`, f.dono, SituacaoAguardando)
	if err != nil {
		return nil, fmt.Errorf("storage: lendo as imagens a baixar: %w", err)
	}
	defer linhas.Close()
	var out []ImagemABaixar
	for linhas.Next() {
		l, err := lerImagemNoSync(linhas)
		if err != nil {
			return nil, fmt.Errorf("storage: lendo as imagens a baixar: %w", err)
		}
		if esperando(l.Tentativas, l.TentouEm, agora, espera) {
			continue
		}
		out = append(out, ImagemABaixar{ID: l.ID, NotaID: l.NotaID, Bytes: l.Bytes, SHA256: l.SHA256})
		if len(out) == limite {
			break
		}
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo as imagens a baixar: %w", err)
	}
	return out, nil
}

// AcaoDaImagemNaBaixa é o que a resposta de um GET manda fazer quando o arquivo
// não foi gravado (o gravado é GuardarBaixada). O zero não faz nada.
type AcaoDaImagemNaBaixa struct {
	// Sumiu: a nuvem não tem mais a imagem (tirada, ou a nota apagada de vez):
	// a linha que espera sai. A lápide, ou a da nota, chega pelo pull.
	Sumiu bool
	// Esperar: mais uma tentativa (tentou_em = agora), com o código.
	Esperar bool
	Erro    string
}

// AplicarBaixaDaImagem executa o que a resposta do GET da imagem id manda
// fazer. Só mexe na linha que ainda espera.
func (f *Fila) AplicarBaixaDaImagem(ctx context.Context, id string, a AcaoDaImagemNaBaixa, agora time.Time) error {
	var erro any
	if a.Erro != "" {
		erro = a.Erro
	}
	switch {
	case a.Sumiu:
		if _, err := f.db.ExecContext(ctx, `DELETE FROM local_note_images WHERE id = ? AND sync_state = ?`,
			id, SituacaoAguardando); err != nil {
			return fmt.Errorf("storage: tirando a imagem que sumiu da nuvem: %w", err)
		}
	case a.Esperar:
		if _, err := f.db.ExecContext(ctx, `UPDATE local_note_images SET tentativas = tentativas + 1, tentou_em = ?,
			last_sync_error = ? WHERE id = ? AND sync_state = ?`, emTexto(agora), erro, id, SituacaoAguardando); err != nil {
			return fmt.Errorf("storage: gravando a espera da imagem: %w", err)
		}
	}
	return nil
}

// GuardarBaixada grava o arquivo da imagem id, que veio da nuvem e esperava
// (aguardando), e a põe normal, numa transação, com o arquivo gravado (o
// temporário e o rename) ANTES da linha mudar: com a linha normal, o arquivo
// existe. Os bytes têm de ser exatamente os da linha (o tamanho e o SHA-256) e
// já limpos: o imagemdanota.Conferir os devolve iguais (o servidor guarda a
// cópia que o Conferir dele limpou). Bytes que não conferem são
// ErrImagemEstragada, e nada muda; a linha que não espera mais (tirada, ou já
// baixada) é ErrNotFound.
func (r *SQLiteNotaRepository) GuardarBaixada(ctx context.Context, id string, jpeg []byte) error {
	dir := r.pasta()
	if dir == "" {
		return ErrImagensSemPasta
	}
	soma := sha256.Sum256(jpeg)
	sha := hex.EncodeToString(soma[:])
	limpa, err := imagemdanota.Conferir(jpeg)
	if err != nil || limpa.SHA256 != sha {
		return ErrImagemEstragada
	}
	r.imagensMu.Lock()
	defer r.imagensMu.Unlock()
	var gravado string
	err = r.gravar(ctx, func(tx *sql.Tx, _ *naFila) error {
		var esperado string
		var bytes int64
		err := tx.QueryRowContext(ctx, `SELECT sha256, bytes FROM local_note_images WHERE id = ? AND sync_state = ?`,
			id, SituacaoAguardando).Scan(&esperado, &bytes)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo a imagem a baixar: %w", err)
		}
		if esperado != sha || bytes != int64(len(jpeg)) {
			return ErrImagemEstragada
		}
		if gravado, err = escreverArquivoDaImagem(dir, id, jpeg); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE local_note_images SET sync_state = ?, tentativas = 0, tentou_em = NULL,
			last_sync_error = NULL WHERE id = ?`, SituacaoNormal, id); err != nil {
			return fmt.Errorf("storage: gravando a imagem baixada: %w", err)
		}
		return nil
	})
	if err != nil && gravado != "" {
		_ = os.Remove(gravado)
	}
	return err
}

// TirarArquivosSemLinha apaga os arquivos de imagem que nenhuma linha cita
// (VarrerImagens sem tocar nas linhas): o motor o chama depois de uma página do
// pull que apagou imagens (a lápide da imagem, ou a da nota), para o arquivo
// sair já, e não só na rodada do expurgo. Devolve quantos apagou.
func (r *SQLiteNotaRepository) TirarArquivosSemLinha(ctx context.Context) (int, error) {
	v, err := r.varrer(ctx, false)
	return v.ArquivosSemLinha, err
}

// ─── A resposta do pull ───

// ImagemNoPull é uma imagem que o pull trouxe: a mudança note_image da página
// (Tombstone, na lápide) ou uma da lista imagens de uma nota da página.
type ImagemNoPull struct {
	ID string
	// NotaID é a nota da imagem (vazio na lápide).
	NotaID    string
	Tombstone bool
}

// EstadoDaImagem é o que o PC sabe de uma imagem na hora de decidir o que
// fazer com o que o pull trouxe.
type EstadoDaImagem struct {
	ID string
	// Linha é a imagem daqui, ou nil.
	Linha *LinhaDaImagemNoSync
	// NotaAqui: a nota da imagem (a que o pull diz) existe neste PC.
	NotaAqui bool
	// Marcador é o marcador note_image da fila, ou nil.
	Marcador *Marcador
}

// AcaoDaImagem é o que a decisão do pull manda fazer com uma imagem, executado
// na transação da página. O zero não faz nada. Nunca põe marcador.
type AcaoDaImagem struct {
	// Inserir grava a imagem que veio da nuvem, sem o arquivo (aguardando, com
	// na_nuvem 1), se a nota existe aqui e o id ainda não.
	Inserir *ImagemDaNota
	// ApagarLinha tira a imagem daqui (a lápide da nuvem). O arquivo sai na
	// varredura logo depois da página (TirarArquivosSemLinha).
	ApagarLinha bool
	// NaNuvem: a nuvem tem a imagem (na_nuvem = 1).
	NaNuvem bool
	// TirarMarcador tira o marcador da imagem, qualquer que seja; TirarCriar,
	// só o de criar (a lápide local fica).
	TirarMarcador bool
	TirarCriar    bool
}

// aplicarImagens executa, na transação de AplicarTudo (depois das notas), o que
// a decisão manda fazer com cada imagem.
func (f *Fila) aplicarImagens(ctx context.Context, tx *sql.Tx, imagens []ImagemNoPull, decidir func(EstadoDaImagem) AcaoDaImagem) error {
	for _, im := range imagens {
		e := EstadoDaImagem{ID: im.ID}
		var err error
		if e.Linha, err = f.linhaDaImagemNoSync(ctx, tx, im.ID); err != nil {
			return err
		}
		if im.NotaID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM local_notes WHERE id = ? AND user_id = ?)`,
				im.NotaID, f.dono).Scan(&e.NotaAqui); err != nil {
				return fmt.Errorf("storage: lendo a nota da imagem: %w", err)
			}
		}
		if e.Marcador, err = marcadorDaFila(ctx, tx, EntidadeImagemDaNota, im.ID); err != nil {
			return err
		}
		if err := f.executarImagem(ctx, tx, e, decidir(e)); err != nil {
			return err
		}
	}
	return nil
}

func (f *Fila) executarImagem(ctx context.Context, tx *sql.Tx, e EstadoDaImagem, a AcaoDaImagem) error {
	if a.ApagarLinha && e.Linha != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_images WHERE id = ?`, e.ID); err != nil {
			return fmt.Errorf("storage: apagando a imagem tirada na nuvem: %w", err)
		}
	}
	// O id que não vira nome de arquivo (a decisão já o recusa) fica de fora,
	// sem desfazer a página: um erro aqui prenderia o pull.
	if im := a.Inserir; im != nil && e.Linha == nil && idDeImagem.MatchString(im.ID) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_note_images
			(id, note_id, largura, altura, bytes, sha256, created_at, na_nuvem, sync_state)
			SELECT ?, ?, ?, ?, ?, ?, ?, 1, ? WHERE EXISTS (SELECT 1 FROM local_notes WHERE id = ? AND user_id = ?)
			ON CONFLICT (id) DO NOTHING`,
			im.ID, im.NotaID, im.Largura, im.Altura, im.Bytes, strings.ToLower(im.SHA256), emTexto(im.CreatedAt),
			SituacaoAguardando, im.NotaID, f.dono); err != nil {
			return fmt.Errorf("storage: gravando a imagem que veio da nuvem: %w", err)
		}
	}
	if a.NaNuvem && e.Linha != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE local_note_images SET na_nuvem = 1 WHERE id = ?`, e.ID); err != nil {
			return fmt.Errorf("storage: gravando que a nuvem tem a imagem: %w", err)
		}
	}
	switch {
	case a.TirarMarcador:
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`,
			EntidadeImagemDaNota, e.ID); err != nil {
			return fmt.Errorf("storage: tirando o marcador da imagem: %w", err)
		}
	case a.TirarCriar:
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ? AND campos = 0`,
			EntidadeImagemDaNota, e.ID); err != nil {
			return fmt.Errorf("storage: tirando o marcador da imagem: %w", err)
		}
	}
	return nil
}

// ─── O recomeço e o esquecer ───

// recomecarImagens é a parte das imagens do recomeço (o epoch novo): toda
// imagem com o arquivo aqui, de nota fora da lixeira, que a nuvem tinha ou podia
// ter (na_nuvem) ganha o marcador de criar (o PUT é idempotente: a nuvem
// restaurada que já a tem responde 200 sem ler o corpo). na_nuvem fica: a nuvem
// restaurada pode ter a imagem, e tirá-la daqui ainda precisa do DELETE. As que
// esperam o download continuam esperando (a nuvem restaurada que não as tem
// responde 404, e a linha sai).
func (f *Fila) recomecarImagens(ctx context.Context, tx *sql.Tx, agora time.Time) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, nota_id)
		SELECT ?, i.id, 0, ?, i.note_id FROM local_note_images i JOIN local_notes n ON n.id = i.note_id
		WHERE n.user_id = ? AND n.deleted_at IS NULL AND i.na_nuvem <> 0 AND i.sync_state = ?
		ON CONFLICT (entity_type, entity_id) DO NOTHING`,
		EntidadeImagemDaNota, emTexto(agora), f.dono, SituacaoNormal); err != nil {
		return fmt.Errorf("storage: pondo na fila as imagens que a nuvem já tinha: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE local_note_images SET tentativas = 0, tentou_em = NULL
		WHERE tentativas <> 0 OR tentou_em IS NOT NULL`); err != nil {
		return fmt.Errorf("storage: zerando as esperas das imagens: %w", err)
	}
	return nil
}

// esquecerImagens é a parte das imagens do "Esquecer esta nuvem", na transação
// dele (a fila inteira já sai lá): a nuvem seguinte não tem nada, então na_nuvem
// volta a 0, a recusa e a espera saem, e a imagem que esperava o download (sem
// arquivo, e sem a nuvem de onde viria) sai de vez.
func (f *Fila) esquecerImagens(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_images WHERE sync_state = ?
		AND note_id IN (SELECT id FROM local_notes WHERE user_id = ?)`, SituacaoAguardando, f.dono); err != nil {
		return fmt.Errorf("storage: tirando as imagens que esperavam a nuvem: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE local_note_images SET na_nuvem = 0, sync_state = ?, last_sync_error = NULL,
		tentativas = 0, tentou_em = NULL
		WHERE note_id IN (SELECT id FROM local_notes WHERE user_id = ?)
		  AND (na_nuvem <> 0 OR sync_state <> ? OR last_sync_error IS NOT NULL OR tentativas <> 0 OR tentou_em IS NOT NULL)`,
		SituacaoNormal, f.dono, SituacaoNormal); err != nil {
		return fmt.Errorf("storage: esquecendo a nuvem das imagens: %w", err)
	}
	return nil
}

// Dono é o dono local da fila (o motor o passa ao repositório das imagens).
func (f *Fila) Dono() string { return f.dono }
