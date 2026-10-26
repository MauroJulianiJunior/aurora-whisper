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
// # O que NÃO acontece
//
//   - O sync. O motor ainda não conhece notas (N7), então nenhum marcador
//     entra na sync_outbox, nem o da nota nem o do ditado: a coluna
//     virou_nota é só deste PC, não passa por emitir, e o ditado não sobe a
//     versão (a base do próximo envio à nuvem fica a mesma). A nuvem
//     continua mostrando o ditado no histórico dela. Ponto em aberto da N7.
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
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
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
		// Só a coluna virou_nota: nem versão, nem updated_at, nem fila do sync.
		// O WHERE repete as guardas (a leitura acima e este UPDATE estão na
		// mesma transação, mas a guarda no SQL é a que vale).
		res, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET virou_nota = ?
			 WHERE id = ? AND user_id = ? AND deleted_at IS NULL AND virou_nota IS NULL`, id, ditadoID, userID)
		if err != nil {
			return fmt.Errorf("storage: marcando o ditado que virou nota: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrDitadoJaVirouNota
		}
		return nil
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

func (r *SQLiteNotaRepository) DesfazerVirarNota(ctx context.Context, userID, notaID string) (DesfeitoOVirarNota, error) {
	var out DesfeitoOVirarNota
	err := naTransacao(ctx, r.db, func(tx *sql.Tx) error {
		var origem, apagada sql.NullString
		var versao int64
		err := tx.QueryRowContext(ctx,
			`SELECT ditado_de_origem, deleted_at, version FROM local_notes WHERE id = ? AND user_id = ?`,
			notaID, userID).Scan(&origem, &apagada, &versao)
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
		// Nota que ninguém mexeu (versão 1) e que não foi para a lixeira: sai.
		// Qualquer edição (texto, cor, fixar, arquivar, marcador, lixeira) sobe
		// a versão, e a nota editada fica.
		if versao == 1 && !apagada.Valid {
			if _, err := tx.ExecContext(ctx, `DELETE FROM local_note_labels WHERE note_id = ?`, notaID); err != nil {
				return fmt.Errorf("storage: apagando os marcadores da nota: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM local_notes WHERE id = ? AND user_id = ?`, notaID, userID); err != nil {
				return fmt.Errorf("storage: apagando a nota: %w", err)
			}
			out.NotaApagada = true
		}
		return nil
	})
	if err != nil {
		return DesfeitoOVirarNota{}, err
	}
	return out, nil
}
