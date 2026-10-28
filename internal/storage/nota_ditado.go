package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Este arquivo é o "Virar nota" do histórico e o Desfazer dele (ADR-022,
// "Ditado vira nota", fase N3). As duas operações mexem em duas tabelas
// (local_transcriptions e local_notes) do mesmo banco, cada uma numa
// transação só: ou as duas mudanças, ou nenhuma.
//
// # O sync (N7)
//
// Na mesma transação: a nota nova (ou a lápide da nota apagada pelo Desfazer)
// e o virou_nota do ditado (CampoVirouNota, só num ditado que a nuvem tem:
// o que ela não tem sobe inteiro, com o virou_nota lido na hora). O ditado
// não sobe a versão local, e a nuvem esconde do histórico dela o ditado com
// ligação.
//
// # O que NÃO acontece
//
//   - A lixeira. O ditado marcado não ganha deleted_at nem purge_at: não é
//     expurgado em 30 dias e não aparece como apagado. Ele sai das listas
//     do histórico (List) e fica fora da regra dos 6 meses (seis_meses_sqlite.go).
//   - O "Aprender" e a auditoria. Eles leem o ditado pelo id (Get), que não
//     filtra virou_nota, e o raw_text continua lá.

func (r *SQLiteNotaRepository) VirarNota(ctx context.Context, userID, ditadoID string) (Nota, error) {
	if userID == "" {
		return Nota{}, fmt.Errorf("storage: VirarNota requer UserID")
	}
	agora := r.now()
	id := NewID()
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		var bruto, final string
		var apagado, virou sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT raw_text, final_text, deleted_at, virou_nota FROM local_transcriptions WHERE id = ? AND user_id = ?`,
			ditadoID, userID).Scan(&bruto, &final, &apagado, &virou)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo o ditado: %w", err)
		}
		if apagado.Valid {
			return ErrDitadoNaLixeira
		}
		if virou.Valid {
			return ErrDitadoJaVirouNota
		}
		// O texto que o histórico mostra: o final, e o bruto quando o final
		// está vazio.
		corpo := final
		if strings.TrimSpace(corpo) == "" {
			corpo = bruto
		}
		if err := validarTextoDaNota(nil, &corpo); err != nil {
			return err
		}
		if strings.TrimSpace(corpo) == "" {
			return ErrNotaVazia
		}
		if err := inserirNota(ctx, tx, id, agora, NovaNota{UserID: userID, Corpo: corpo, DitadoDeOrigem: ditadoID}); err != nil {
			return err
		}
		// Só a coluna virou_nota: nem versão, nem updated_at. O WHERE repete as
		// guardas (a leitura acima e este UPDATE estão na mesma transação, mas a
		// guarda no SQL é a que vale).
		res, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET virou_nota = ?
			 WHERE id = ? AND user_id = ? AND deleted_at IS NULL AND virou_nota IS NULL`, id, ditadoID, userID)
		if err != nil {
			return fmt.Errorf("storage: marcando o ditado que virou nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrDitadoJaVirouNota
		}
		if err := fila.anotar(emitirNota(ctx, tx, id, 0, agora)); err != nil {
			return err
		}
		return fila.anotar(emitir(ctx, tx, ditadoID, CampoVirouNota, agora))
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

func (r *SQLiteNotaRepository) DesfazerVirarNota(ctx context.Context, userID, notaID string) (DesfeitoOVirarNota, error) {
	var out DesfeitoOVirarNota
	agora := r.now()
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		out = DesfeitoOVirarNota{}
		var origem, apagada sql.NullString
		var versao int64
		var naNuvem sql.NullInt64
		err := tx.QueryRowContext(ctx,
			`SELECT ditado_de_origem, deleted_at, version, server_version FROM local_notes WHERE id = ? AND user_id = ?`,
			notaID, userID).Scan(&origem, &apagada, &versao, &naNuvem)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: lendo a nota: %w", err)
		}
		if !origem.Valid || origem.String == "" {
			return ErrNotaNaoVeioDeDitado
		}
		// Devolve o ditado ao histórico, e só se ele ainda aponta para esta nota
		// (o Desfazer repetido, ou o ditado que a pessoa já devolveu, não acha).
		res, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET virou_nota = NULL WHERE id = ? AND user_id = ? AND virou_nota = ?`,
			origem.String, userID, notaID)
		if err != nil {
			return fmt.Errorf("storage: devolvendo o ditado ao histórico: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotaNaoVeioDeDitado
		}
		out.DitadoID = origem.String
		if err := fila.anotar(emitir(ctx, tx, origem.String, CampoVirouNota, agora)); err != nil {
			return err
		}
		// Nota que ninguém mexeu (versão 1) e que não foi para a lixeira: sai.
		// Qualquer edição (texto, cor, fixar, arquivar, marcador, lixeira) sobe
		// a versão, e a nota editada fica. Anexar imagem não sobe a versão (a
		// imagem é uma linha à parte, N10), então a nota com imagem também fica:
		// a pessoa já mexeu nela.
		var comImagem int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_note_images WHERE note_id = ?`, notaID).Scan(&comImagem); err != nil {
			return fmt.Errorf("storage: contando as imagens da nota: %w", err)
		}
		if versao == 1 && !apagada.Valid && comImagem == 0 {
			if err := apagarNotaDaTabela(ctx, tx, notaID); err != nil {
				return err
			}
			out.NotaApagada = true
			// A nota sai de vez: a lápide local (nada sobe se a nuvem nunca a
			// teve e o envio dela não está no voo).
			return fila.anotar(lapideLocal(ctx, tx, EntidadeNota, notaID, naNuvem.Valid, agora))
		}
		return nil
	})
	if err != nil {
		return DesfeitoOVirarNota{}, err
	}
	return out, nil
}
