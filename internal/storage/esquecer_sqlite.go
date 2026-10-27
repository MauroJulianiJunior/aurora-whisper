package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// chavesDaConexao são as chaves que o "Esquecer esta nuvem" apaga: tudo o que
// liga esta instalação à nuvem e à conta. Ficam o install_id (o mesmo PC) e o
// dono fixo (que nem passa por ChaveDaNuvem).
var chavesDaConexao = []string{
	"nuvem_url", ChaveNuvemConta, "nuvem_email", "nuvem_nome", ChaveNuvemAparelho, ChaveNuvemConectadoEm,
	ChaveNuvemEnvioAceitoEm, ChaveNuvemEnvioTotal, ChaveNuvemCopia, ChaveNuvemNotas,
}

// EsquecerNuvem é o "Esquecer esta nuvem" (ADR-019, "Decisões da F7c"), numa
// transação. Esquece tudo o que liga este PC à nuvem e à conta: as chaves da
// conexão e do envio (chavesDaConexao), o cursor e o epoch (sync_state), a
// fila inteira (sync_outbox) e, nas linhas, só o que é do sync
// (server_version, a situação e o erro; nas notas, também o corpo_nuvem).
// Nenhum ditado nem nota muda de conteúdo: o
// texto, a lixeira, o aparelho de origem e o selo ficam; ficam também o dono
// fixo, o install_id, as preferências e as cópias.
//
// Grava a referência da reconciliação, se o PC voltar à mesma conta: o
// instante do esquecer (nuvem_esquecida_em; o que mudar aqui depois dele
// conta como "mudou aqui", e não é sobrescrito pelo pull) e, item por item, o
// que mudou aqui ANTES dele sem a nuvem receber (anotarMudouAqui, na
// sync_mudou_aqui). Se a nuvem esquecida tinha confirmado algum ditado
// nascido neste PC, grava também nuvem_ditados_em_outra (a pergunta do envio,
// na nuvem seguinte, diz isso).
//
// No espelho do aprendido (F8e, esquecerAprendido): a base e a situação saem
// (menos na linha "aplicando"), o texto que veio da conta esquecida e não
// está no arquivo sai, o "desfeito aqui" fica só com o id e a conta a que
// pertence, e o "veio da nuvem" fica inteiro. Os marcadores learned saem com
// a fila.
//
// Quem chama confere antes que o PC está desconectado (o token, que mora no
// Gerenciador de Credenciais, não passa por aqui).
func (f *Fila) EsquecerNuvem(ctx context.Context, agora time.Time) error {
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		var recebeu int
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM local_transcriptions
			   WHERE user_id = ? AND server_version IS NOT NULL AND `+nasceuAquiSQL+`)`,
			f.dono).Scan(&recebeu); err != nil {
			return fmt.Errorf("storage: lendo o que a nuvem recebeu: %w", err)
		}
		// Antes de apagar a fila e as versões: elas dizem o que mudou aqui e
		// não subiu.
		if err := anotarMudouAqui(ctx, tx, f.dono); err != nil {
			return err
		}
		// O aprendido (F8e), antes de a conta sair da instalação: o
		// "desfeito aqui" guarda a conta a que pertence.
		if err := f.esquecerAprendido(ctx, tx); err != nil {
			return err
		}
		marcas := strings.TrimSuffix(strings.Repeat("?, ", len(chavesDaConexao)), ", ")
		args := make([]any, len(chavesDaConexao))
		for i, c := range chavesDaConexao {
			args[i] = c
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome IN (`+marcas+`)`, args...); err != nil {
			return fmt.Errorf("storage: esquecendo a conexão: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_state`); err != nil {
			return fmt.Errorf("storage: esquecendo o cursor: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox`); err != nil {
			return fmt.Errorf("storage: esquecendo a fila: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET server_version = NULL, sync_state = ?, last_sync_error = NULL
			 WHERE user_id = ? AND (server_version IS NOT NULL OR sync_state <> ? OR last_sync_error IS NOT NULL)`,
			SituacaoNormal, f.dono, SituacaoNormal); err != nil {
			return fmt.Errorf("storage: esquecendo as versões da nuvem: %w", err)
		}
		// As notas e os marcadores de nota (N7), como os ditados: as versões,
		// a situação e o corpo_nuvem saem (a nuvem seguinte não tem nada
		// disso); o selo e o "marcador que espera" ficam.
		if err := f.esquecerNotas(ctx, tx); err != nil {
			return err
		}
		anotar := map[string]string{ChaveNuvemEsquecidaEm: emTexto(agora)}
		if recebeu == 1 {
			anotar[ChaveNuvemDitadosEmOutra] = "1"
		}
		for nome, valor := range anotar {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`,
				nome, valor); err != nil {
				return fmt.Errorf("storage: anotando o esquecer: %w", err)
			}
		}
		return nil
	})
}

// anotarMudouAqui grava na sync_mudou_aqui, item por item, os ditados que
// mudaram aqui sem a nuvem esquecida receber a mudança. De volta à mesma
// conta, cada um conta como "mudou aqui" (EstadoDoItem.MudouAqui) e sobe com
// a base vazia, e a nuvem decide (apagar vence; texto contra texto vira
// selo); sem isso, o pull gravava o estado da nuvem por cima: a edição feita
// desconectado sumia, e a lixeira voltava (revisão da F7c). São:
//   - os que têm um marcador na fila (a mudança que não subiu), menos o
//     marcador do envio inicial num ditado que a nuvem não tem e que não
//     mudou desde o clique: ele é do histórico, e a nuvem não o tem;
//   - os que um esquecer anterior anotou e que continuam sem a versão de uma
//     nuvem (nenhuma os reconciliou desde então);
//   - havendo um esquecer anterior, os sem a versão da nuvem que mudaram
//     depois dele (o PC esqueceu de novo sem uma rodada que os
//     reconciliasse, ou esteve ligado a outra conta, que não os tem).
//
// Sai o que deixou de valer: o ditado que voltou a ter a versão de uma nuvem
// (foi reconciliado; se mudou de novo, o marcador o anota outra vez) e o que
// não existe mais.
//
// Item por item, e não a hora de referência recuada até a mudança mais
// antiga, como a revisão da F7c fez (reconferência da F7c): o recuo pegava
// também o ditado que o PC recebeu da nuvem depois dela, que, mudado por
// outro aparelho com o PC esquecido, virava selo à toa na volta; e, depois de
// um esquecer, a mudança num ditado que nunca sobe (de mais de 6 meses, de
// outro PC, na lixeira, recusado) prendia a referência nela para sempre.
// Anotar a mais custa pouco: o anotado que a nuvem não tem nunca chega pelo
// pull, e o que ela tem pode, no máximo, virar selo sozinho, sem perder nada.
func anotarMudouAqui(ctx context.Context, tx *sql.Tx, dono string) error {
	anterior, err := nuvemEsquecidaEm(ctx, tx)
	if err != nil {
		return err
	}
	var desde any // nulo sem esquecer anterior: a última condição não vale
	if anterior != nil {
		desde = emTexto(*anterior)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sync_mudou_aqui WHERE entity_type = ? AND NOT EXISTS (SELECT 1 FROM local_transcriptions t
		   WHERE t.id = sync_mudou_aqui.entity_id AND t.user_id = ? AND t.server_version IS NULL)`,
		EntidadeDitado, dono); err != nil {
		return fmt.Errorf("storage: limpando o que mudou aqui: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_mudou_aqui (entity_type, entity_id)
		 SELECT ?, t.id FROM local_transcriptions t
		 WHERE t.user_id = ? AND (
		   EXISTS (SELECT 1 FROM sync_outbox o WHERE o.entity_type = ? AND o.entity_id = t.id
		     AND NOT (o.inicial = 1 AND t.server_version IS NULL AND t.updated_at <= o.criado_em))
		   OR (t.server_version IS NULL AND t.updated_at > ?))
		 ON CONFLICT (entity_type, entity_id) DO NOTHING`,
		EntidadeDitado, dono, EntidadeDitado, desde); err != nil {
		return fmt.Errorf("storage: anotando o que mudou aqui e não subiu: %w", err)
	}
	return nil
}

// Esquecer é Fila.EsquecerNuvem.
func (h HistoricoDaNuvem) Esquecer(ctx context.Context, agora time.Time) error {
	return h.Fila.EsquecerNuvem(ctx, agora)
}
