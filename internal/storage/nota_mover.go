package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Mover é o "arrastar a nota" (ADR-022, "Ordem manual (N2b)"): grava a chave
// nova da nota, entre as duas vizinhas que ela terá depois de soltar. Ver
// NotaRepository.Mover.
func (r *SQLiteNotaRepository) Mover(ctx context.Context, userID, id, antesID, depoisID string) (Nota, error) {
	if antesID == "" && depoisID == "" {
		return Nota{}, ErrMoverSemVizinho
	}
	if id == antesID || id == depoisID {
		return Nota{}, ErrVizinhoInvalido // a nota não é vizinha de si mesma
	}
	agora := r.now()
	err := r.gravar(ctx, func(tx *sql.Tx, fila *naFila) error {
		if err := conferirNotaEditavel(ctx, tx, userID, id, -1); err != nil {
			return err
		}
		arquivada, fixada, err := grupoDaNota(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		var atual string
		if err := tx.QueryRowContext(ctx,
			`SELECT posicao FROM local_notes WHERE id = ? AND user_id = ?`, id, userID).Scan(&atual); err != nil {
			return fmt.Errorf("storage: lendo a posição da nota: %w", err)
		}
		antes, err := posicaoDaVizinha(ctx, tx, userID, antesID, arquivada, fixada)
		if err != nil {
			return err
		}
		depois, err := posicaoDaVizinha(ctx, tx, userID, depoisID, arquivada, fixada)
		if err != nil {
			return err
		}
		if antes != "" && depois != "" && antes >= depois {
			return ErrVizinhoInvalido
		}
		// Já está entre as duas (soltou onde estava, ou a segunda tentativa de um
		// pedido repetido): nada a gravar, e a versão não sobe à toa.
		if atual != "" && (antes == "" || antes < atual) && (depois == "" || atual < depois) {
			return nil
		}
		posicao, err := PosicaoEntre(antes, depois)
		if err != nil {
			return fmt.Errorf("storage: gerando a posição da nota: %w", err)
		}
		// A versão sobe (a nota mudou) e a posição entra na fila do sync
		// (CampoNotaPosicao); a hora de edição NÃO muda: arrastar não é editar.
		if _, err := tx.ExecContext(ctx,
			`UPDATE local_notes SET posicao = ?, version = version + 1 WHERE id = ? AND user_id = ?`,
			posicao, id, userID); err != nil {
			return fmt.Errorf("storage: movendo a nota: %w", err)
		}
		return fila.anotar(emitirNota(ctx, tx, id, CampoNotaPosicao, agora))
	})
	if err != nil {
		return Nota{}, err
	}
	return r.Get(ctx, userID, id)
}

// posicaoDaVizinha lê a posição da vizinha e confere que ela vale: existe, é
// do usuário, não está na lixeira e está no mesmo grupo. id vazio é "sem
// vizinha desse lado" e devolve posição vazia.
func posicaoDaVizinha(ctx context.Context, tx *sql.Tx, userID, id string, arquivada, fixada bool) (string, error) {
	if id == "" {
		return "", nil
	}
	var posicao string
	err := tx.QueryRowContext(ctx,
		`SELECT posicao FROM local_notes
		 WHERE id = ? AND user_id = ? AND deleted_at IS NULL AND arquivada = ? AND fixada = ?`,
		id, userID, boolInt(arquivada), boolInt(fixada)).Scan(&posicao)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrVizinhoInvalido
	}
	if err != nil {
		return "", fmt.Errorf("storage: lendo a vizinha: %w", err)
	}
	return posicao, nil
}
