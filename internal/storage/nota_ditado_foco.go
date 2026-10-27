package storage

import (
	"context"
	"fmt"
	"time"
)

// Este arquivo é o "ditado feito dentro da nota" (ADR-022, "Ditado vira nota",
// seção "Ditado feito dentro da nota"). O ditado nasce já escondido do histórico:
// o Agent grava virou_nota com o id da nota em foco ou, se a nota ainda não
// existe (o "Criar uma nota" aberto), com "nova:<token>" (PrefixoDaNotaNova).
// Aqui ficam os dois destinos do "nova:<token>": virar a nota (NovaNota.Token,
// em Criar) ou voltar ao histórico (abaixo). Nenhum dos dois toca a versão nem
// a lixeira, e nenhum define ditado_de_origem da nota. Virar a nota põe o
// virou_nota de cada ditado na fila do sync (CampoVirouNota, N7); voltar ao
// histórico não põe nada: a nuvem nunca recebe o "nova:<token>" (o ditado sobe
// com virou_nota nulo), então para ela nada mudou.

func (r *SQLiteNotaRepository) SoltarDitadosDaNotaNova(ctx context.Context, userID, token string) (int, error) {
	if userID == "" {
		return 0, fmt.Errorf("storage: SoltarDitadosDaNotaNova requer UserID")
	}
	if !TokenDaNotaValido(token) {
		return 0, ErrTokenInvalido
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE local_transcriptions SET virou_nota = NULL WHERE user_id = ? AND virou_nota = ?`,
		userID, PrefixoDaNotaNova+token)
	if err != nil {
		return 0, fmt.Errorf("storage: soltando os ditados da caixa que não virou nota: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: soltando os ditados da caixa que não virou nota: %w", err)
	}
	return int(n), nil
}

// CarenciaDaNotaNova é quanto um ditado marcado "nova:<token>" espera por uma
// nota que talvez ainda nasça antes de o expurgo devolvê-lo ao histórico
// (LiberarDitadosDeNotasNovas). O painel descarta a caixa vazia na hora
// (SoltarDitadosDaNotaNova); isto só pega o que sobrou de um painel que fechou
// de repente ou de um Agent que reiniciou no meio.
const CarenciaDaNotaNova = 24 * time.Hour

// LiberarDitadosDeNotasNovas devolve ao histórico os ditados "nova:<token>"
// feitos antes de `antes` e devolve quantos. O expurgo das notas o chama a cada
// rodada (PurgeExpired) com agora - CarenciaDaNotaNova: nada fica escondido para
// sempre por uma caixa que nunca virou nota.
func (r *SQLiteNotaRepository) LiberarDitadosDeNotasNovas(ctx context.Context, antes time.Time) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE local_transcriptions SET virou_nota = NULL WHERE virou_nota LIKE ? AND created_at < ?`,
		PrefixoDaNotaNova+"%", emTexto(antes.UTC()))
	if err != nil {
		return 0, fmt.Errorf("storage: liberando os ditados de caixas abandonadas: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: liberando os ditados de caixas abandonadas: %w", err)
	}
	return int(n), nil
}
