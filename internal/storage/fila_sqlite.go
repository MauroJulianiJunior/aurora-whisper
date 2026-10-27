package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// A fila do sync no PC (ADR-019, F7a): o SQL da sync_outbox, da sync_state e
// do aplicador. As DECISÕES (o que cada resposta da nuvem manda fazer) moram
// em internal/sync, puras e testadas sem banco; aqui só se lê o estado de um
// ditado e se executa a AcaoLocal decidida, na transação certa. Nenhuma rede
// passa por aqui, e nenhuma transação daqui espera rede.
//
// Toda leitura e escrita é do dono local (o dono fixo da instalação): a nuvem
// grava os ditados de outros PCs com ele (ADR-019, "Dono local e conta").

// EntidadeDitado é o entity_type do ditado no sync.
const EntidadeDitado = "transcription"

// Os campos do marcador (sync_outbox.campos), em bits: o que mudou desde a
// última versão que a nuvem confirmou. Criar não é bit: um ditado com
// server_version nulo sobe inteiro.
const (
	CampoTexto   = 1
	CampoLixeira = 2
	// CampoVirouNota (N7): o virou_nota do ditado mudou (virar nota, desfazer,
	// a nota criada com os ditados da caixa). Só sobe depois de a nuvem dizer
	// que conhece notas (ChaveNuvemNotas); antes, fica parado na fila.
	CampoVirouNota = 4
)

// As chaves da nuvem na tabela instalacao que o sync lê (as mesmas do
// pareamento, internal/agent/nuvem).
const (
	// ChaveNuvemAparelho: o aparelho dado no pareamento. Presente é "a nuvem
	// está conectada" para a fila: todo ditado gravado entra nela. Sem ele,
	// só a mudança num ditado que a nuvem já tem (emitir).
	ChaveNuvemAparelho = "nuvem_aparelho"
	// ChaveNuvemConectadoEm: quando o pareamento aconteceu. Outro valor é uma
	// reconexão: o cursor passa para o pareamento novo (Preparar).
	ChaveNuvemConectadoEm = "nuvem_conectado_em"
)

// ErrEpochDiferente: a resposta da nuvem é de outro epoch (um backup
// restaurado, ADR-019). Nada da resposta foi aplicado, e o PC recomeça.
var ErrEpochDiferente = errors.New("storage: a nuvem respondeu com outro epoch")

// ErrPareamentoMudou: a resposta é de um pareamento que já não é o desta
// instalação (o PC desconectou, reconectou ou esqueceu a nuvem enquanto o
// pedido estava na rede). Nada foi aplicado, e a rodada só termina: a
// seguinte lê a credencial de agora. Recomeçar aqui, com o aparelho velho,
// apagaria o cursor do pareamento novo e as versões da nuvem (F7c).
var ErrPareamentoMudou = errors.New("storage: o pareamento mudou durante a rodada")

// Marcador é uma linha da sync_outbox: "este ditado mudou e não subiu".
type Marcador struct {
	EntityType string
	EntityID   string
	// Campos são os bits do que mudou (CampoTexto, CampoLixeira).
	Campos int
	// MutationID e VersaoEnviada são do envio em curso; vazio e zero fora de
	// voo.
	MutationID    string
	VersaoEnviada int64
	// Repetir: o envio do aprendido que estava em voo quando o PC reconectou
	// (Preparar, InicioReconexao): sem mutation_id (o aparelho novo manda
	// outro), com a versão enviada. O próximo envio repete a mesma operação e
	// o mesmo payload (a projeção guardada continua congelada), e a mudança
	// feita durante o voo sobe só depois da resposta dele, com a base que ela
	// trouxer. Só o aprendido (ADR-019, "Decisões da F8", desvio 9).
	Repetir  bool
	CriadoEm time.Time
	// Inicial: o marcador foi posto pelo "Enviar o histórico" (F7c). Conta
	// para o progresso e sobe depois do resto.
	Inicial bool
	// SeloEscolha é a escolha do selo feita no PC (EscolhaManter ou
	// EscolhaTrocar; vazio sem escolha), e SeloEm, o conflict_at do selo
	// escolhido (F7c).
	SeloEscolha string
	SeloEm      time.Time
}

// EmVoo diz se o marcador foi enviado e ainda não teve resposta.
func (m Marcador) EmVoo() bool { return m.MutationID != "" }

// Congelado diz se o envio do marcador ainda espera resposta: em voo, ou a
// repetir depois de uma reconexão (Repetir). No aprendido, a projeção
// guardada não muda enquanto ele está congelado (ErrProjecaoEmVoo).
func (m Marcador) Congelado() bool { return m.EmVoo() || m.Repetir }

// ItemDaFila é um marcador com a linha do ditado, lida na hora de enviar.
type ItemDaFila struct {
	Marcador Marcador
	Linha    Transcription
}

// Envio é o que MarcarEnvio grava antes da rede: o id da mutação e a versão
// local que foi lida para montá-la.
type Envio struct {
	EntityType string
	EntityID   string
	MutationID string
	Versao     int64
}

// EstadoDaSync é o cursor e o epoch deste pareamento.
type EstadoDaSync struct {
	Cursor    int64
	Epoch     string
	PareadoEm string
}

// Inicio é o que Preparar fez antes de devolver o cursor.
type Inicio int

const (
	// InicioMesmo: o estado já era deste pareamento.
	InicioMesmo Inicio = iota
	// InicioReconexao: o estado era de outro pareamento desta instalação (a
	// mesma conta, que a D3 garante). O cursor, o epoch e as versões da nuvem
	// continuam valendo: passaram para o pareamento novo.
	InicioReconexao
	// InicioRecomeco: não havia estado (a primeira rodada da instalação), e o
	// PC recomeçou (Recomecar).
	InicioRecomeco
)

// EstadoDoItem é o que o PC sabe de um ditado na hora de decidir o que fazer
// com uma resposta da nuvem.
type EstadoDoItem struct {
	ID string
	// Linha é o ditado deste PC; nil se não existe aqui.
	Linha *Transcription
	// DeOutroDono: existe uma linha com esse id, mas de outro dono (uma linha
	// antiga, fora do painel). O aplicador não a toca.
	DeOutroDono bool
	// Marcador é o da fila; nil sem marcador.
	Marcador *Marcador
	// MudouAqui: a linha, sem a versão da nuvem, mudou neste PC depois do
	// "Esquecer esta nuvem" (updated_at depois de nuvem_esquecida_em), ou
	// antes dele sem a nuvem receber (o esquecer a anotou na
	// sync_mudou_aqui), ou não houve esquecer. É o que separa, na
	// reconciliação do pull (F7c, D3), "o PC mudou o ditado" (ele sobe, e a
	// nuvem decide) de "o PC tem o ditado velho" (vale o da nuvem, sem selo
	// à toa).
	MudouAqui bool
}

// O que fazer com o marcador (AcaoLocal.Marcador).
const (
	// MarcadorFica: não mexe.
	MarcadorFica = iota
	// MarcadorSai: o marcador sai da fila.
	MarcadorSai
	// MarcadorReenviar: fica, com os campos, e o envio em curso é limpo: o
	// próximo envio leva outro mutation_id.
	MarcadorReenviar
	// MarcadorCriar: põe um marcador de criar (o ditado sobe inteiro, com a
	// base vazia, e a nuvem decide pelas regras de sempre), se não houver um.
	// É a reconciliação do pull (ADR-019, "Decisões da F7c", D3): o ditado
	// que existe aqui sem ter sido confirmado, e que a nuvem tem diferente,
	// não é sobrescrito. O marcador nasce com os dois campos, que não mudam
	// o envio (sem server_version, ele leva o ditado inteiro), mas dizem que
	// a nuvem TEM o ditado: a lixeira feita aqui antes do envio não o tira
	// da fila, e sobe (SoftDelete).
	MarcadorCriar
)

// AcaoLocal é o que uma decisão do sync manda fazer com um ditado, executada
// por Aplicar na transação da resposta. O zero não faz nada.
type AcaoLocal struct {
	// Gravar é o estado da nuvem. Sem a linha aqui, ela é inserida inteira,
	// com o dono local e version 1; com a linha, mudam o texto final, a
	// lixeira (e o prazo do expurgo), o status e o selo, e a version local
	// sobe um (uma aba aberta no painel recebe 409 em vez de sobrescrever).
	// Nunca cria marcador: aplicar não ecoa.
	Gravar *Transcription
	// ServerVersion grava a versão da nuvem que o PC passa a conhecer.
	ServerVersion *int64
	// ZerarServerVersion: o PC não conhece versão nenhuma (o próximo envio
	// sobe o ditado inteiro).
	ZerarServerVersion bool
	// ApagarLinha apaga o ditado e o marcador dele (a lápide, o gone
	// "apagado").
	ApagarLinha bool
	// Situacao, se não vazia, grava a situação do ditado (SituacaoNormal,
	// SituacaoRecusado, SituacaoForaDaJanela); Erro é o código da recusa.
	Situacao string
	Erro     string
	// Marcador é MarcadorFica, MarcadorSai ou MarcadorReenviar.
	Marcador int
	// MudarVirouNota grava só o virou_nota do ditado (VirouNota; nil é o
	// ditado comum): o eco de uma versão que o PC já tem, em que só a ligação
	// com a nota mudou (N7). Vale a guarda do "nova:" (gravarRemoto), e só
	// com a capacidade das notas (antes dela, a nuvem não sabe do virou_nota,
	// e o nulo que ela manda não quer dizer nada).
	MudarVirouNota bool
	VirouNota      *string
}

// Fila é a fila do sync de um dono, sobre o historico.db.
type Fila struct {
	db   *sql.DB
	dono string
}

// NovaFila monta a fila sobre db (já migrado), para o dono local.
func NovaFila(db *sql.DB, dono string) *Fila {
	if db == nil {
		panic("storage: db nil")
	}
	if dono == "" {
		panic("storage: fila sem dono")
	}
	return &Fila{db: db, dono: dono}
}

// Fila devolve a fila do sync no mesmo banco do histórico.
func (r *SQLiteTranscriptionRepository) Fila(dono string) *Fila { return NovaFila(r.db, dono) }

// ─── O pareamento, o cursor e o recomeço ───

// Preparar lê o cursor e o epoch deste pareamento (ADR-019, "Decisões da
// F7a/b", decisão 2):
//   - o estado é deste pareamento: devolve-o (InicioMesmo);
//   - o estado é de OUTRO pareamento desta instalação (o mesmo PC
//     reconectado, outro aparelho; a D3 garante a mesma conta): o cursor e o
//     epoch passam para o pareamento novo, e as versões da nuvem que as
//     linhas guardam continuam valendo. Só os envios em curso são esquecidos
//     (o aparelho novo os manda com outros ids); o do aprendido fica a
//     repetir (Marcador.Repetir), com a mesma operação. Se o epoch da nuvem
//     mudou enquanto o PC estava fora, a primeira resposta o diz, e aí sim o
//     PC recomeça (InicioReconexao);
//   - não há estado (a primeira rodada da instalação), ou há mais de um (não
//     acontece: só por um banco mexido à mão): recomeça (InicioRecomeco).
func (f *Fila) Preparar(ctx context.Context, aparelho, pareadoEm string, agora time.Time) (EstadoDaSync, Inicio, error) {
	var est EstadoDaSync
	inicio := InicioMesmo
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		est, inicio = EstadoDaSync{}, InicioMesmo
		var cursor int64
		var epoch, pareado sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT last_server_seq, sync_epoch, pareado_em FROM sync_state WHERE user_id = ? AND device_id = ?`,
			f.dono, aparelho).Scan(&cursor, &epoch, &pareado)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("storage: lendo o estado do sync: %w", err)
		}
		if err == nil && pareado.Valid && pareado.String == pareadoEm {
			est = EstadoDaSync{Cursor: cursor, Epoch: epoch.String, PareadoEm: pareado.String}
			return nil
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_state`).Scan(&n); err != nil {
			return fmt.Errorf("storage: contando os cursores: %w", err)
		}
		if n != 1 {
			inicio = InicioRecomeco
			est = EstadoDaSync{PareadoEm: pareadoEm}
			return f.recomecar(ctx, tx, aparelho, pareadoEm, agora)
		}
		inicio = InicioReconexao
		if err := tx.QueryRowContext(ctx,
			`UPDATE sync_state SET user_id = ?, device_id = ?, pareado_em = ?
			 RETURNING last_server_seq, sync_epoch`,
			f.dono, aparelho, pareadoEm).Scan(&cursor, &epoch); err != nil {
			return fmt.Errorf("storage: passando o cursor ao pareamento novo: %w", err)
		}
		if err := esquecerEnvios(ctx, tx, true); err != nil {
			return err
		}
		est = EstadoDaSync{Cursor: cursor, Epoch: epoch.String, PareadoEm: pareadoEm}
		return nil
	})
	return est, inicio, err
}

// ZerarCursor é a volta do 410 no mesmo epoch (ADR-019, "Decisões da
// F7a/b"): o PC perdeu lápides, então lê tudo de novo do cursor 0. Só o
// cursor muda: as versões da nuvem que as linhas guardam continuam valendo
// (o epoch é o mesmo), então o pull do 0 reconhece pela versão o que o PC já
// tem, e nada é reenviado. A fila fica como está.
func (f *Fila) ZerarCursor(ctx context.Context, aparelho string) error {
	if _, err := f.db.ExecContext(ctx,
		`UPDATE sync_state SET last_server_seq = 0 WHERE user_id = ? AND device_id = ?`, f.dono, aparelho); err != nil {
		return fmt.Errorf("storage: zerando o cursor: %w", err)
	}
	return nil
}

// Recomecar é o recomeço do epoch novo (ADR-019, "Lápides, gone, epoch e
// recomeço": um backup restaurado, ou o "apagar meus dados"), numa transação:
// o cursor volta a 0 e o epoch a nulo; os envios em curso são esquecidos (o
// próximo leva outro mutation_id); cada ditado que a nuvem já tinha
// (server_version não nulo), dentro da janela de 6 meses, fora da lixeira e
// que ela não pôs fora da janela, ganha um marcador; e server_version volta a
// nulo em todos (no epoch novo, as versões guardadas não valem nada), então
// cada marcador sobe o ditado inteiro, com a base vazia. Os marcadores que já
// existiam (as mudanças daqui ainda não enviadas) ficam, e também sobem
// inteiros.
//
// A lixeira fica de fora, como a ADR manda: o que o PC pôs na lixeira depois
// do backup pode voltar dela pelo pull do 0 (o risco que a revisão da F2
// deixou para a F7c escolher). A reconexão e o 410 do mesmo epoch NÃO passam
// por aqui (Preparar e ZerarCursor).
//
// O aprendido recomeça no mesmo molde (recomecarAprendido, F8e): o par
// presente vira upsert com a base 0 (menos o do "veio da nuvem"), o desfeito
// vira delete, e o recusado é liberado.
func (f *Fila) Recomecar(ctx context.Context, aparelho, pareadoEm string, agora time.Time) error {
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		if err := f.recomecar(ctx, tx, aparelho, pareadoEm, agora); err != nil {
			return err
		}
		// No epoch novo (um backup restaurado), o PC tende a ser o mais
		// novo: a reconciliação do pull (F7c, D3) sobe toda diferença, sem a
		// referência de um esquecer antigo (o instante e o anotado). (A
		// primeira rodada de um pareamento, que também recomeça pelo
		// Preparar, a mantém: é nela que o PC que voltou à mesma conta
		// reconcilia.)
		if _, err := tx.ExecContext(ctx, `DELETE FROM instalacao WHERE nome = ?`, ChaveNuvemEsquecidaEm); err != nil {
			return fmt.Errorf("storage: tirando a referência do esquecer: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sync_mudou_aqui`); err != nil {
			return fmt.Errorf("storage: tirando o que o esquecer anotou: %w", err)
		}
		return nil
	})
}

func (f *Fila) recomecar(ctx context.Context, tx *sql.Tx, aparelho, pareadoEm string, agora time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_state (user_id, device_id, last_server_seq, sync_epoch, pareado_em)
		 VALUES (?, ?, 0, NULL, ?)
		 ON CONFLICT (user_id, device_id) DO UPDATE SET
		   last_server_seq = 0, sync_epoch = NULL, pareado_em = excluded.pareado_em`,
		f.dono, aparelho, pareadoEm); err != nil {
		return fmt.Errorf("storage: zerando o cursor: %w", err)
	}
	// Um cursor só: o de outro pareamento não vale mais nada.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sync_state WHERE NOT (user_id = ? AND device_id = ?)`, f.dono, aparelho); err != nil {
		return fmt.Errorf("storage: apagando o cursor antigo: %w", err)
	}
	if err := esquecerEnvios(ctx, tx, false); err != nil {
		return err
	}

	linhas, err := tx.QueryContext(ctx,
		`SELECT id, created_at FROM local_transcriptions
		 WHERE user_id = ? AND server_version IS NOT NULL AND deleted_at IS NULL AND sync_state <> ?`,
		f.dono, SituacaoForaDaJanela)
	if err != nil {
		return fmt.Errorf("storage: lendo o que a nuvem já tinha: %w", err)
	}
	var ids []string
	for linhas.Next() {
		var id, criado string
		if err := linhas.Scan(&id, &criado); err != nil {
			linhas.Close()
			return fmt.Errorf("storage: lendo o que a nuvem já tinha: %w", err)
		}
		em, err := deTempo(criado)
		if err != nil {
			linhas.Close()
			return err
		}
		// A janela é a mesma conta da nuvem (cloudstore.OutOfWindow).
		if agora.Before(retention.ActiveExpiryAt(em)) {
			ids = append(ids, id)
		}
	}
	if err := linhas.Err(); err != nil {
		linhas.Close()
		return fmt.Errorf("storage: lendo o que a nuvem já tinha: %w", err)
	}
	linhas.Close()
	for _, id := range ids {
		// O marcador que já existe fica como está (com os campos): sem
		// server_version, ele também sobe inteiro.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES (?, ?, 0, ?)
			 ON CONFLICT (entity_type, entity_id) DO NOTHING`,
			EntidadeDitado, id, emTexto(agora)); err != nil {
			return fmt.Errorf("storage: pondo na fila o que a nuvem já tinha: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE local_transcriptions SET server_version = NULL WHERE user_id = ? AND server_version IS NOT NULL`,
		f.dono); err != nil {
		return fmt.Errorf("storage: esquecendo as versões da nuvem: %w", err)
	}
	// As notas e os marcadores de nota (N7), no mesmo molde dos ditados.
	if err := f.recomecarNotas(ctx, tx, agora); err != nil {
		return err
	}
	// O aprendido, no mesmo molde (F8e): todo par do espelho marcado de novo,
	// as versões esquecidas e o recusado liberado.
	return f.recomecarAprendido(ctx, tx, agora)
}

// esquecerEnvios limpa os envios em curso: o próximo envio de cada marcador
// leva outro mutation_id.
//
// Na reconexão (repetirAprendido), o envio do aprendido em voo fica a repetir
// (Marcador.Repetir): sai o mutation_id, e fica a versão enviada. A nuvem pode
// já ter aplicado o envio cuja resposta se perdeu; o aparelho novo repete a
// mesma operação, com o mesmo payload (a projeção guardada continua
// congelada), e a nuvem responde applied com a versão de agora. Só depois a
// mudança feita durante o voo sobe, com a base que a resposta trouxe. Sem
// isso, ela subia com a base velha e perdia, num conflict, para o próprio
// envio deste PC (ADR-019, "As regras da nuvem": um desfazer sem resposta
// seguido de um reaprender termina reaprendido, sem etiqueta). No recomeço
// (o epoch novo), todo envio é esquecido: a nuvem restaurada não tem o que
// foi aplicado depois do backup.
func esquecerEnvios(ctx context.Context, tx *sql.Tx, repetirAprendido bool) error {
	if repetirAprendido {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_outbox SET mutation_id = NULL, versao_enviada = NULL
			 WHERE mutation_id IS NOT NULL AND entity_type <> ?`, EntidadeAprendido); err != nil {
			return fmt.Errorf("storage: esquecendo os envios em curso: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_outbox SET mutation_id = NULL WHERE mutation_id IS NOT NULL AND entity_type = ?`,
			EntidadeAprendido); err != nil {
			return fmt.Errorf("storage: pondo o envio do aprendido a repetir: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_outbox SET mutation_id = NULL, versao_enviada = NULL
		 WHERE mutation_id IS NOT NULL OR versao_enviada IS NOT NULL`); err != nil {
		return fmt.Errorf("storage: esquecendo os envios em curso: %w", err)
	}
	return nil
}

// AnotacaoOciosa é de quanto em quanto tempo uma rodada que deu certo sem
// fazer nada anota last_success_at: o PC conectado e parado não escreve no
// historico.db a cada busca.
const AnotacaoOciosa = 10 * time.Minute

// RegistrarRodada anota o fim de uma rodada do sync: a hora do último
// sucesso, ou o código (lista fechada) da falha. Só anotação: nenhuma
// decisão lê isto. Para não escrever no banco a cada busca vazia, o sucesso
// só é anotado se a rodada fez algo (teve), se a anterior tinha falhado, ou
// se a última anotação passou de AnotacaoOciosa; a falha, só se o código
// mudou.
func (f *Fila) RegistrarRodada(ctx context.Context, aparelho string, agora time.Time, erro string, teve bool) error {
	var err error
	if erro == "" {
		_, err = f.db.ExecContext(ctx,
			`UPDATE sync_state SET last_success_at = ?, ultimo_erro = NULL
			 WHERE user_id = ? AND device_id = ?
			   AND (? OR ultimo_erro IS NOT NULL OR last_success_at IS NULL OR last_success_at <= ?)`,
			emTexto(agora), f.dono, aparelho, teve, emTexto(agora.Add(-AnotacaoOciosa)))
	} else {
		_, err = f.db.ExecContext(ctx,
			`UPDATE sync_state SET ultimo_erro = ? WHERE user_id = ? AND device_id = ? AND ultimo_erro IS NOT ?`,
			erro, f.dono, aparelho, erro)
	}
	if err != nil {
		return fmt.Errorf("storage: anotando a rodada do sync: %w", err)
	}
	return nil
}

// ─── O push ───

// Pendentes devolve até limite marcadores com a linha de cada um, os em voo
// primeiro (o reenvio de um lote sem resposta vem antes de tudo), depois o
// ditado novo e as edições, e por último o histórico do envio inicial
// (inicial, F7c), cada grupo na ordem em que entrou: um ditado feito durante
// o envio de 800 sai no lote seguinte, e não depois deles. Um marcador sem
// linha (o ditado sumiu: expurgado) ou de um ditado que a nuvem pôs fora da
// janela sai da fila aqui mesmo.
//
// Antes da capacidade das notas (ChaveNuvemNotas, N7), o marcador que só tem
// o CampoVirouNota (sem escolha do selo e fora do voo) não é devolvido: fica
// parado na fila até a nuvem saber do virou_nota.
func (f *Fila) Pendentes(ctx context.Context, limite int) ([]ItemDaFila, error) {
	if limite <= 0 {
		return nil, nil
	}
	var itens []ItemDaFila
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		itens = nil
		comNotas, err := temCapacidadeDeNotas(ctx, tx)
		if err != nil {
			return err
		}
		marcadores, err := lerMarcadores(ctx, tx,
			`WHERE entity_type = ?
			   AND NOT (? = 0 AND campos = ? AND selo_escolha IS NULL AND mutation_id IS NULL)
			 ORDER BY mutation_id IS NULL, inicial, criado_em, entity_id LIMIT ?`,
			EntidadeDitado, boolInt(comNotas), CampoVirouNota, limite)
		if err != nil {
			return err
		}
		for _, m := range marcadores {
			linha, err := f.linha(ctx, tx, m.EntityID)
			if err != nil {
				return err
			}
			if linha == nil || linha.SyncState == SituacaoForaDaJanela {
				if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`,
					m.EntityType, m.EntityID); err != nil {
					return fmt.Errorf("storage: tirando da fila um ditado que não está aqui: %w", err)
				}
				continue
			}
			itens = append(itens, ItemDaFila{Marcador: m, Linha: *linha})
		}
		return nil
	})
	return itens, err
}

// MarcarEnvio grava, antes da rede, o mutation_id e a versão lida de cada
// envio, só nos marcadores que não estão em voo (o em voo repete o mesmo id).
// O marcador a repetir (Marcador.Repetir) ganha o id novo e fica com a versão
// enviada de antes: é com ela que a resposta reconhece a mudança feita
// durante o voo.
func (f *Fila) MarcarEnvio(ctx context.Context, envios []Envio) error {
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		for _, e := range envios {
			if _, err := tx.ExecContext(ctx,
				`UPDATE sync_outbox SET mutation_id = ?, versao_enviada = COALESCE(versao_enviada, ?)
				 WHERE entity_type = ? AND entity_id = ? AND mutation_id IS NULL`,
				e.MutationID, e.Versao, e.EntityType, e.EntityID); err != nil {
				return fmt.Errorf("storage: marcando o envio: %w", err)
			}
		}
		return nil
	})
}

// Recusar marca os ditados como recusados com o código e tira o marcador
// deles, sem passar pela nuvem: o que o PC já sabe que ela recusaria (um
// texto acima do teto, de antes do teto no painel). Sem isso, um ditado
// grande demais para o corpo do push travaria a fila inteira.
func (f *Fila) Recusar(ctx context.Context, ids []string, codigo string) error {
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				`UPDATE local_transcriptions SET sync_state = ?, last_sync_error = ? WHERE id = ? AND user_id = ?`,
				SituacaoRecusado, codigo, id, f.dono); err != nil {
				return fmt.Errorf("storage: recusando o ditado: %w", err)
			}
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, EntidadeDitado, id); err != nil {
				return fmt.Errorf("storage: tirando o recusado da fila: %w", err)
			}
		}
		return nil
	})
}

// Contar devolve quantos ditados esperam para subir.
func (f *Fila) Contar(ctx context.Context) (int, error) {
	var n int
	if err := f.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sync_outbox WHERE entity_type = ?`, EntidadeDitado).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: contando a fila do sync: %w", err)
	}
	return n, nil
}

// Marcadores devolve a fila inteira, na ordem do envio (para os testes e o
// diagnóstico).
func (f *Fila) Marcadores(ctx context.Context) ([]Marcador, error) {
	var ms []Marcador
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		var err error
		ms, err = lerMarcadores(ctx, tx, `ORDER BY mutation_id IS NULL, inicial, criado_em, entity_id`)
		return err
	})
	return ms, err
}

// ─── A resposta da nuvem (o push e o pull) ───

// Aplicar executa, numa transação só, o que decidir manda fazer com cada
// ditado de ids, na ordem, e, se cursor não é nil, grava o cursor novo junto
// (a página do pull e o cursor dela são uma coisa só). Antes de tudo, o
// epoch: o guardado nulo adota o da resposta; outro devolve
// ErrEpochDiferente sem aplicar nada. Um erro de E/S desfaz tudo, inclusive
// o cursor.
//
// decidir recebe o estado do ditado lido NESTA transação e não pode tocar o
// banco (é puro).
func (f *Fila) Aplicar(ctx context.Context, aparelho, epoch string, ids []string, cursor *int64, decidir func(EstadoDoItem) AcaoLocal) error {
	return f.AplicarComAprendido(ctx, aparelho, epoch, ids, cursor, decidir, RespostaDoAprendido{})
}

// RespostaDoAprendido é a parte do aprendido de uma resposta da nuvem (o
// push, ou a página do pull), aplicada na mesma transação dos ditados e do
// cursor (ADR-019, "Decisões da F8": o espelho, os ditados da página e o
// cursor numa transação, com a conferência do epoch e do pareamento).
type RespostaDoAprendido struct {
	// IDs são os ids do aprendido, na ordem.
	IDs []string
	// Decidir recebe o estado do id lido NESTA transação e não pode tocar o
	// banco (é puro).
	Decidir func(EstadoDoAprendido) AcaoDoAprendido
	// Agora é a hora de quem aplica (o começo de uma situação, o marcador
	// novo).
	Agora time.Time
}

// AplicarComAprendido é Aplicar com o aprendido junto: depois dos ditados e
// antes do cursor, na mesma transação, o que aprendido.Decidir manda fazer
// com cada id de aprendido.IDs (executarAprendido). Um erro desfaz tudo. É
// AplicarTudo sem as notas.
func (f *Fila) AplicarComAprendido(ctx context.Context, aparelho, epoch string, ids []string, cursor *int64,
	decidir func(EstadoDoItem) AcaoLocal, aprendido RespostaDoAprendido) error {
	return f.AplicarTudo(ctx, aparelho, epoch, ids, cursor, decidir, aprendido, RespostaDasNotas{})
}

// AplicarTudo é AplicarComAprendido com as notas (N7): numa transação só, os
// ditados, o aprendido, os marcadores de nota, as notas (os marcadores antes:
// a nota que cita um marcador que chega na mesma página o acha) e o cursor.
// Um erro desfaz tudo.
func (f *Fila) AplicarTudo(ctx context.Context, aparelho, epoch string, ids []string, cursor *int64,
	decidir func(EstadoDoItem) AcaoLocal, aprendido RespostaDoAprendido, notas RespostaDasNotas) error {
	if epoch == "" {
		return ErrEpochDiferente
	}
	if len(aprendido.IDs) > 0 && aprendido.Decidir == nil {
		return acaoInvalida("ids do aprendido sem a decisão")
	}
	if len(notas.Marcadores) > 0 && notas.DecidirMarcador == nil {
		return acaoInvalida("ids de marcadores de nota sem a decisão")
	}
	if len(notas.Notas) > 0 && notas.DecidirNota == nil {
		return acaoInvalida("ids de notas sem a decisão")
	}
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		var guardado sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT sync_epoch FROM sync_state WHERE user_id = ? AND device_id = ?`, f.dono, aparelho).Scan(&guardado)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Sem o estado deste pareamento (outro aparelho entrou no meio, ou
			// o PC esqueceu a nuvem): a resposta não é deste cursor.
			return ErrPareamentoMudou
		case err != nil:
			return fmt.Errorf("storage: lendo o epoch: %w", err)
		case !guardado.Valid:
			if _, err := tx.ExecContext(ctx,
				`UPDATE sync_state SET sync_epoch = ? WHERE user_id = ? AND device_id = ?`, epoch, f.dono, aparelho); err != nil {
				return fmt.Errorf("storage: gravando o epoch: %w", err)
			}
		case guardado.String != epoch:
			return ErrEpochDiferente
		}
		esquecidaEm, err := nuvemEsquecidaEm(ctx, tx)
		if err != nil {
			return err
		}
		comNotas, err := temCapacidadeDeNotas(ctx, tx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			e, err := f.estado(ctx, tx, id, esquecidaEm)
			if err != nil {
				return err
			}
			if err := f.executar(ctx, tx, e, decidir(e), comNotas); err != nil {
				return err
			}
		}
		if len(aprendido.IDs) > 0 {
			if err := f.mudarAprendido(ctx, tx, aprendido.Agora, aprendido.IDs, aprendido.Decidir); err != nil {
				return err
			}
		}
		if err := f.aplicarNotas(ctx, tx, notas); err != nil {
			return err
		}
		if cursor != nil {
			if _, err := tx.ExecContext(ctx,
				`UPDATE sync_state SET last_server_seq = ? WHERE user_id = ? AND device_id = ?`,
				*cursor, f.dono, aparelho); err != nil {
				return fmt.Errorf("storage: gravando o cursor: %w", err)
			}
		}
		return nil
	})
}

// ConferirPareamento é a leitura curta que vem antes de o aplicador do
// aprendido tocar o dicionario.json (ADR-019, "Decisões da F8", "A ordem",
// passo 1): o estado deste pareamento ainda existe (senão ErrPareamentoMudou:
// o PC desconectou, reconectou ou esqueceu a nuvem) e tem o epoch dado (senão
// ErrEpochDiferente; o epoch nulo, ainda não visto, também). Só lê: diferente
// do Aplicar, nunca adota o epoch.
func (f *Fila) ConferirPareamento(ctx context.Context, aparelho, epoch string) error {
	var guardado sql.NullString
	err := f.db.QueryRowContext(ctx,
		`SELECT sync_epoch FROM sync_state WHERE user_id = ? AND device_id = ?`, f.dono, aparelho).Scan(&guardado)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrPareamentoMudou
	case err != nil:
		return fmt.Errorf("storage: lendo o epoch: %w", err)
	case !guardado.Valid || epoch == "" || guardado.String != epoch:
		return ErrEpochDiferente
	}
	return nil
}

// estado lê a linha e o marcador de um ditado, na transação. esquecidaEm é
// a referência da reconciliação (nuvemEsquecidaEm), ou nil.
func (f *Fila) estado(ctx context.Context, tx *sql.Tx, id string, esquecidaEm *time.Time) (EstadoDoItem, error) {
	e := EstadoDoItem{ID: id}
	t, err := lerTranscricao(tx.QueryRowContext(ctx,
		`SELECT `+colunasTranscricao+` FROM local_transcriptions WHERE id = ?`, id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return e, fmt.Errorf("storage: lendo o ditado: %w", err)
	case t.UserID != f.dono:
		e.DeOutroDono = true
	default:
		e.Linha = &t
	}
	ms, err := lerMarcadores(ctx, tx, `WHERE entity_type = ? AND entity_id = ?`, EntidadeDitado, id)
	if err != nil {
		return e, err
	}
	if len(ms) == 1 {
		e.Marcador = &ms[0]
	}
	if l := e.Linha; l != nil && l.ServerVersion == nil {
		e.MudouAqui = esquecidaEm == nil || l.UpdatedAt.After(*esquecidaEm)
		if !e.MudouAqui {
			// Mudou aqui antes do esquecer, e a nuvem não recebeu: o
			// esquecer o anotou (anotarMudouAqui).
			var anotado int
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM sync_mudou_aqui WHERE entity_type = ? AND entity_id = ?)`,
				EntidadeDitado, id).Scan(&anotado); err != nil {
				return e, fmt.Errorf("storage: lendo o que mudou aqui: %w", err)
			}
			e.MudouAqui = anotado == 1
		}
	}
	return e, nil
}

// nuvemEsquecidaEm lê quando este PC esqueceu a nuvem (ChaveNuvemEsquecidaEm),
// a referência da reconciliação; nil se nunca esqueceu (ou se o recomeço de
// um epoch novo a tirou).
func nuvemEsquecidaEm(ctx context.Context, q consulta) (*time.Time, error) {
	v, ok, err := lerChave(ctx, q, ChaveNuvemEsquecidaEm)
	if err != nil || !ok {
		return nil, err
	}
	t, err := deTempo(v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// executar faz o que a ação manda, na transação. comNotas é a capacidade das
// notas (ChaveNuvemNotas), lida na mesma transação: sem ela, o virou_nota
// que vem da nuvem não é gravado.
func (f *Fila) executar(ctx context.Context, tx *sql.Tx, e EstadoDoItem, a AcaoLocal, comNotas bool) error {
	if e.DeOutroDono {
		// A linha é de outro dono: nada aqui a toca (o marcador, se houver,
		// é deste dono e segue a ação).
		a.Gravar, a.ServerVersion, a.ZerarServerVersion, a.ApagarLinha, a.Situacao = nil, nil, false, false, ""
		a.MudarVirouNota = false
	}
	if a.ApagarLinha {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM local_transcriptions WHERE id = ? AND user_id = ?`, e.ID, f.dono); err != nil {
			return fmt.Errorf("storage: apagando o ditado apagado na nuvem: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, EntidadeDitado, e.ID); err != nil {
			return fmt.Errorf("storage: tirando da fila o ditado apagado na nuvem: %w", err)
		}
		return nil
	}
	if a.Gravar != nil {
		if err := f.gravarRemoto(ctx, tx, e, *a.Gravar, a.ServerVersion, comNotas); err != nil {
			return err
		}
	}
	if a.MudarVirouNota && comNotas && e.Linha != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE local_transcriptions SET virou_nota = `+virouNotaDaNuvemSQL+` WHERE id = ? AND user_id = ?`,
			textoNulo(a.VirouNota), textoNulo(a.VirouNota), e.ID, f.dono); err != nil {
			return fmt.Errorf("storage: gravando a nota do ditado que veio da nuvem: %w", err)
		}
	}
	if e.Linha != nil || a.Gravar != nil {
		if a.ServerVersion != nil {
			if _, err := tx.ExecContext(ctx,
				`UPDATE local_transcriptions SET server_version = ? WHERE id = ? AND user_id = ?`,
				*a.ServerVersion, e.ID, f.dono); err != nil {
				return fmt.Errorf("storage: gravando a versão da nuvem: %w", err)
			}
		}
		if a.ZerarServerVersion {
			if _, err := tx.ExecContext(ctx,
				`UPDATE local_transcriptions SET server_version = NULL WHERE id = ? AND user_id = ?`,
				e.ID, f.dono); err != nil {
				return fmt.Errorf("storage: esquecendo a versão da nuvem: %w", err)
			}
		}
		if a.Situacao != "" {
			var erro any
			if a.Erro != "" {
				erro = a.Erro
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE local_transcriptions SET sync_state = ?, last_sync_error = ? WHERE id = ? AND user_id = ?`,
				a.Situacao, erro, e.ID, f.dono); err != nil {
				return fmt.Errorf("storage: gravando a situação do ditado: %w", err)
			}
		}
	}
	switch a.Marcador {
	case MarcadorSai:
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, EntidadeDitado, e.ID); err != nil {
			return fmt.Errorf("storage: tirando o marcador: %w", err)
		}
	case MarcadorReenviar:
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_outbox SET mutation_id = NULL, versao_enviada = NULL WHERE entity_type = ? AND entity_id = ?`,
			EntidadeDitado, e.ID); err != nil {
			return fmt.Errorf("storage: limpando o envio: %w", err)
		}
	case MarcadorCriar:
		if e.Linha == nil {
			return nil
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em) VALUES (?, ?, ?, ?)
			 ON CONFLICT (entity_type, entity_id) DO NOTHING`,
			EntidadeDitado, e.ID, CampoTexto|CampoLixeira, emTexto(time.Now())); err != nil {
			return fmt.Errorf("storage: pondo na fila o ditado a reconciliar: %w", err)
		}
	}
	return nil
}

// PrazoDaLixeira é quanto um ditado fica na lixeira do PC (o mesmo do
// SoftDelete), contado do deleted_at.
const PrazoDaLixeira = 30 * 24 * time.Hour

// virouNotaDaNuvemSQL é o valor novo do virou_nota que veio da nuvem (os dois
// argumentos são o mesmo valor), com a guarda do "nova:" (N7): a nuvem nunca
// recebe o "nova:<token>" (o ditado feito numa caixa de "Criar uma nota" que
// ainda não virou nota), então o nulo dela não tira o ditado da caixa.
const virouNotaDaNuvemSQL = `CASE WHEN ? IS NULL AND virou_nota LIKE '` + PrefixoDaNotaNova + `%' THEN virou_nota ELSE ? END`

// gravarRemoto grava o estado da nuvem: insere o ditado novo aqui, ou muda o
// texto, a lixeira, o status e o selo do que existe, subindo a version local.
// Com a capacidade das notas (comNotas), também o virou_nota, com a guarda
// do "nova:" (virouNotaDaNuvemSQL); sem ela, a nuvem não sabe do virou_nota,
// e o daqui fica. Nunca põe marcador.
func (f *Fila) gravarRemoto(ctx context.Context, tx *sql.Tx, e EstadoDoItem, r Transcription, sv *int64, comNotas bool) error {
	var purga *time.Time
	if r.DeletedAt != nil {
		p := r.DeletedAt.Add(PrazoDaLixeira)
		purga = &p
	}
	if e.Linha == nil {
		r.ID, r.UserID = e.ID, f.dono
		r.PurgeAt = purga
		r.TargetApp = nil
		r.Version = 1
		r.SyncState = SituacaoNormal
		r.ServerVersion = sv
		r.LastSyncError = nil
		if !comNotas || (r.VirouNota != nil && strings.HasPrefix(*r.VirouNota, PrefixoDaNotaNova)) {
			r.VirouNota = nil
		}
		if err := inserirLinha(ctx, tx, `INSERT`, r); err != nil {
			return fmt.Errorf("storage: gravando o ditado que veio da nuvem: %w", err)
		}
		return nil
	}
	var conflitoTexto, conflitoDevice, conflitoEm any
	if r.Conflito != nil {
		conflitoTexto, conflitoDevice, conflitoEm = r.Conflito.FinalText, textoNulo(r.Conflito.DeviceID), emTexto(r.Conflito.Em)
	}
	virou := `virou_nota`
	args := []any{r.FinalText, instanteNulo(r.DeletedAt), instanteNulo(purga), r.Status, emTexto(r.UpdatedAt),
		conflitoTexto, conflitoDevice, conflitoEm}
	if comNotas {
		virou = virouNotaDaNuvemSQL
		args = append(args, textoNulo(r.VirouNota), textoNulo(r.VirouNota))
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE local_transcriptions SET final_text = ?, deleted_at = ?, purge_at = ?, status = ?, updated_at = ?,
		   conflito_texto = ?, conflito_device_id = ?, conflito_em = ?, version = version + 1, virou_nota = `+virou+`
		 WHERE id = ? AND user_id = ?`,
		append(args, e.ID, f.dono)...); err != nil {
		return fmt.Errorf("storage: aplicando o ditado que veio da nuvem: %w", err)
	}
	return nil
}

// linha lê o ditado do dono, ou nil.
func (f *Fila) linha(ctx context.Context, tx *sql.Tx, id string) (*Transcription, error) {
	t, err := lerTranscricao(tx.QueryRowContext(ctx,
		`SELECT `+colunasTranscricao+` FROM local_transcriptions WHERE id = ? AND user_id = ?`, id, f.dono))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: lendo o ditado: %w", err)
	}
	return &t, nil
}

// lerMarcadores lê marcadores com o filtro e a ordem dados.
func lerMarcadores(ctx context.Context, tx *sql.Tx, resto string, args ...any) ([]Marcador, error) {
	linhas, err := tx.QueryContext(ctx,
		`SELECT entity_type, entity_id, campos, mutation_id, versao_enviada, criado_em, inicial, selo_escolha, selo_em
		 FROM sync_outbox `+resto, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: lendo a fila do sync: %w", err)
	}
	defer linhas.Close()
	var ms []Marcador
	for linhas.Next() {
		var m Marcador
		var mutation, escolha, seloEm sql.NullString
		var versao sql.NullInt64
		var criado string
		var inicial int
		if err := linhas.Scan(&m.EntityType, &m.EntityID, &m.Campos, &mutation, &versao, &criado,
			&inicial, &escolha, &seloEm); err != nil {
			return nil, fmt.Errorf("storage: lendo a fila do sync: %w", err)
		}
		m.MutationID, m.VersaoEnviada, m.Inicial = mutation.String, versao.Int64, inicial == 1
		m.Repetir = !mutation.Valid && versao.Valid
		if m.CriadoEm, err = deTempo(criado); err != nil {
			return nil, err
		}
		if escolha.Valid && seloEm.Valid {
			if m.SeloEm, err = deTempo(seloEm.String); err != nil {
				return nil, err
			}
			m.SeloEscolha = escolha.String
		}
		ms = append(ms, m)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo a fila do sync: %w", err)
	}
	return ms, nil
}
