package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/retention"
)

// O envio inicial do histórico e o "Esquecer esta nuvem" (ADR-019, resposta 3
// do Mauro e "Decisões da F7c").
//
// # O envio inicial
//
// Na primeira conexão, sobe o que está nos últimos 6 meses, MENOS a lixeira,
// com uma cópia do banco antes. "Sobe" é pôr um marcador sem campo (criar) em
// cada ditado elegível e deixar a fila da F7a levá-lo, em lotes por número e
// por bytes: o envio inicial não tem caminho de rede próprio.
//
//   - Elegível (elegiveis, o filtro único da prévia, do envio e da
//     completação): do dono; sem server_version (a nuvem não o tem); fora da
//     lixeira; sem recusa nem "fora da janela" (sync_state pending); nascido
//     neste PC (device_id nulo e origem que não é da nuvem, nasceuAquiSQL: o que
//     veio de outro PC é dele, D2, e o que o celular criou, "pelo app", é da
//     nuvem, ADR-021); dentro da
//     janela de 6 meses (a conta da nuvem, retention.ActiveExpiryAt); e sem
//     marcador (o que já está na fila sobe por ela).
//   - A pessoa clica em "Enviar o histórico" (D1): EnviarHistorico faz a
//     cópia, marca em blocos curtos e grava o aceite. Depois do aceite, toda
//     rodada completa o que faltar (CompletarEnvio: o ditado novo feito
//     desconectado, o restaurado desconectado), sem cópia nem pergunta.
//   - Sem nada elegível, não há cópia nem pergunta: a rodada grava o aceite
//     sozinha (como o dono fixo num banco vazio, dono.go).
//
// Nenhuma linha muda aqui: só a sync_outbox e as chaves nuvem_* da tabela
// instalacao. O texto continua sendo lido da linha na hora do envio.
//
// # A cópia
//
// VACUUM INTO, conferida tabela por tabela, ao lado do banco
// (historico.db.antes-da-nuvem-AAAAMMDD-HHMMSS.db), no padrão do dono.go
// (copiarBanco, com a marca própria e MaxCopias por marca). Ela roda pela
// conexão que o Agent tem (SetMaxOpenConns(1)), SEGURANDO-A (copiar): a
// contagem de antes e o VACUUM INTO não deixam nenhuma escrita passar no meio,
// e o Put de um ditado que chegue nesse tempo só espera (o texto já está no
// campo). Uma segunda conexão, sem WAL nem busy_timeout, faria o Put falhar
// com SQLITE_BUSY. A espera é medida (envio_espera_test.go).

// As chaves do envio e do esquecer na tabela instalacao (ChaveDaNuvem).
const (
	// ChaveNuvemEnvioAceitoEm: quando a pessoa clicou em "Enviar o
	// histórico" (ou quando a rodada viu que não havia nada a enviar).
	// Presente, a rodada completa o que faltar.
	ChaveNuvemEnvioAceitoEm = "nuvem_envio_aceito_em"
	// ChaveNuvemEnvioTotal: quantos marcadores o envio inicial pôs (o "de
	// 800" do progresso).
	ChaveNuvemEnvioTotal = "nuvem_envio_total"
	// ChaveNuvemCopia: onde ficou a cópia de antes do envio. Uma por conta:
	// o clique de novo (depois de uma queda no meio) não copia outra vez.
	ChaveNuvemCopia = "nuvem_copia"
	// ChaveNuvemEsquecidaEm: quando o PC esqueceu a nuvem. É a referência da
	// reconciliação do pull (EstadoDoItem.MudouAqui): de volta à mesma
	// conta, sobe só o que mudou aqui depois disso, e o que mudou antes sem a
	// nuvem receber (anotado pelo esquecer na sync_mudou_aqui).
	ChaveNuvemEsquecidaEm = "nuvem_esquecida_em"
	// ChaveNuvemDitadosEmOutra: a nuvem esquecida tinha confirmado ditados
	// nascidos neste PC. A pergunta do envio, na nuvem seguinte, diz que
	// eles já foram para outra.
	ChaveNuvemDitadosEmOutra = "nuvem_ditados_em_outra"
)

// marcaDaCopiaDoEnvio separa, no nome da cópia, o nome do banco da hora.
const marcaDaCopiaDoEnvio = ".antes-da-nuvem-"

// CaminhoDaCopiaDoEnvio é onde o envio inicial copia o banco antes de marcar:
// ao lado dele, com a hora local no nome, por exemplo
// historico.db.antes-da-nuvem-20261006-153000.db.
func CaminhoDaCopiaDoEnvio(caminhoBanco string, agora time.Time) string {
	return caminhoBanco + marcaDaCopiaDoEnvio + agora.Format("20060102-150405") + ".db"
}

// BlocoDoEnvio é quantos marcadores cada transação do envio põe: o Put de um
// ditado que chegue no meio espera um bloco, nunca o histórico inteiro.
const BlocoDoEnvio = 500

// Os motivos de uma cópia que não deu certo (ErroDaCopia.Motivo). Nada foi
// marcado (no envio) nem apagado (nos 6 meses) em nenhum deles.
var (
	// ErrCopiaDoEnvio: a cópia de antes do envio falhou, ou não confere.
	ErrCopiaDoEnvio = errors.New("storage: a cópia do histórico antes do envio falhou")
	// ErrCopiaDosSeisMeses: a cópia de antes dos 6 meses (o Ligar, F10)
	// falhou, ou não confere.
	ErrCopiaDosSeisMeses = errors.New("storage: a cópia do histórico antes dos 6 meses falhou")
	// ErrCopiasDemais: já há MaxCopias cópias da mesma marca ao lado do
	// histórico, e nenhuma nova é feita.
	ErrCopiasDemais = errors.New("storage: já há cópias demais ao lado do histórico")
	// ErrDiscoCheio: o disco não tem espaço para a cópia.
	ErrDiscoCheio = errors.New("storage: falta espaço no disco para a cópia do histórico")
)

// ErroDaCopia é a cópia que não deu certo: o motivo (um dos Err acima) e o
// detalhe, com os caminhos trocados pelo nome do arquivo (o caminho leva o
// nome de usuário do Windows, que não vai para log).
type ErroDaCopia struct {
	Motivo  error
	Detalhe string
	// base é de qual cópia o erro é: ErrCopiaDoEnvio (o zero, o envio) ou
	// ErrCopiaDosSeisMeses.
	base error
}

func (e *ErroDaCopia) Error() string { return e.Motivo.Error() + ": " + e.Detalhe }

// Unwrap deixa errors.Is achar o motivo e de qual cópia ele é
// (ErrCopiaDoEnvio, ou ErrCopiaDosSeisMeses).
func (e *ErroDaCopia) Unwrap() []error {
	base := e.base
	if base == nil {
		base = ErrCopiaDoEnvio
	}
	return []error{e.Motivo, base}
}

// ResultadoDoEnvio é o que EnviarHistorico fez.
type ResultadoDoEnvio struct {
	// Marcados: quantos ditados entraram na fila agora.
	Marcados int
	// Copia: onde ficou a cópia feita agora ("" se nenhuma: nada a enviar,
	// ou a cópia desta conta já existia).
	Copia string
	// JaAceito: o envio já tinha sido aceito; nada foi feito (a rodada
	// completa o que faltar).
	JaAceito bool
}

// ResumoDoEnvio é o que o painel mostra da nuvem, do lado do histórico. Só
// contagens e datas: nunca texto, id ou caminho.
type ResumoDoEnvio struct {
	// Previa: quantos ditados o "Enviar o histórico" marcaria agora.
	Previa int
	// Trocas: quantas trocas aprendidas o clique soltaria (o M de "Vão subir
	// N ditados e M trocas aprendidas", ADR-019, "Decisões da F8", D3), no
	// estado que a última varredura deixou no espelho
	// (EstadoDoAprendido.SeguradoPeloClique). Zero depois do aceite.
	Trocas int
	// Aceito: a pessoa já mandou enviar (ou não havia nada).
	Aceito bool
	// Feitos e Total: o envio inicial em curso ("enviando 120 de 800"); Total
	// zero quando não há.
	Feitos, Total int
	// NaoSubiram: ditados deste PC que a nuvem recusou ou pôs fora da
	// janela, sem nunca os ter recebido.
	NaoSubiram int
	// Pendentes: marcadores de ditado na fila (o que ainda não subiu).
	Pendentes int
	// Mudancas: dos Pendentes, os que levam uma mudança feita aqui num
	// ditado que a nuvem tem: a edição, a lixeira, o restaurar e a escolha
	// do selo (o ditado tem a versão da nuvem) e a reconciliação (o marcador
	// com os campos). Fora o ditado novo e o histórico que ainda não subiram
	// (o marcador de criar, sem campo): é o número da pergunta do esquecer,
	// "{n} mudanças feitas aqui ainda não subiram" (reconferência da F7c).
	// Soma também o marcador do aprendido de uma mudança feita aqui (F8e: a
	// troca aprendida, desfeita ou trocada aqui, com a versão local acima da
	// base), que não conta em Pendentes; o marcador que o epoch novo põe num
	// par que a nuvem já tinha confirmado não conta, como no ditado.
	Mudancas int
	// ConferidoEm: a última rodada que deu certo (sync_state.last_success_at;
	// gravado no máximo a cada AnotacaoOciosa numa rodada parada).
	ConferidoEm *time.Time
	// Falhou: a última rodada não deu certo (o laço tenta de novo sozinho).
	Falhou bool
	// OutraNuvem: este PC esqueceu uma nuvem que já tinha recebido ditados
	// dele.
	OutraNuvem bool
}

// filtroElegivel é o pedaço SQL do filtro único (sobre local_transcriptions
// AS t), sem a janela de 6 meses, que é conferida em Go. Os argumentos são
// argsElegivel.
const filtroElegivel = `t.user_id = ? AND t.server_version IS NULL AND t.deleted_at IS NULL
	AND t.sync_state = ? AND ` + nasceuAquiTSQL

func (f *Fila) argsElegivel() []any { return []any{f.dono, SituacaoNormal} }

// naJanela é a janela de 6 meses, a mesma conta da nuvem
// (cloudstore.OutOfWindow): um ditado de createdAt ainda vale em agora.
func naJanela(createdAt, agora time.Time) bool {
	return agora.Before(retention.ActiveExpiryAt(createdAt))
}

// elegiveis devolve, numa leitura só, os ids dos ditados que o envio inicial
// marcaria agora, do mais antigo para o mais novo. O corte por created_at no
// SQL é grosso (uma semana de folga, pelos meses de comprimento diferente);
// a janela exata é conferida aqui.
func (f *Fila) elegiveis(ctx context.Context, q consulta, agora time.Time) ([]string, error) {
	corte := agora.AddDate(0, -retention.ActiveRetentionMonths, -7)
	args := append(f.argsElegivel(), emTexto(corte), EntidadeDitado)
	linhas, err := q.QueryContext(ctx, `SELECT t.id, t.created_at FROM local_transcriptions t
		WHERE `+filtroElegivel+` AND t.created_at >= ?
		  AND NOT EXISTS (SELECT 1 FROM sync_outbox o WHERE o.entity_type = ? AND o.entity_id = t.id)
		ORDER BY t.created_at, t.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: lendo o que o envio inicial leva: %w", err)
	}
	defer linhas.Close()
	var ids []string
	for linhas.Next() {
		var id, criado string
		if err := linhas.Scan(&id, &criado); err != nil {
			return nil, fmt.Errorf("storage: lendo o que o envio inicial leva: %w", err)
		}
		em, err := deTempo(criado)
		if err != nil {
			return nil, err
		}
		if naJanela(em, agora) {
			ids = append(ids, id)
		}
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: lendo o que o envio inicial leva: %w", err)
	}
	return ids, nil
}

// Previa é quantos ditados o "Enviar o histórico" marcaria agora.
func (f *Fila) Previa(ctx context.Context, agora time.Time) (int, error) {
	ids, err := f.elegiveis(ctx, f.db, agora)
	return len(ids), err
}

// EnviarHistorico é o clique em "Enviar o histórico" (D1): a cópia do banco,
// os marcadores do envio inicial em blocos curtos e o aceite. caminhoBanco é
// o arquivo do historico.db (a cópia fica ao lado dele).
//
//   - Já aceito: não faz nada (JaAceito); a rodada completa o que faltar.
//   - Nada elegível: só o aceite, sem cópia.
//   - A cópia: uma por conta (nuvem_copia). Se ela falha, NADA é marcado e o
//     aceite não é gravado (*ErroDaCopia); o próximo clique tenta de novo.
//   - Os marcadores: blocos de BlocoDoEnvio, cada um numa transação, com o
//     filtro de novo no INSERT e ON CONFLICT DO NOTHING (clicar duas vezes,
//     ou cair no meio, não duplica). O total do progresso soma em cada bloco,
//     e a cópia é registrada no primeiro.
//   - O aceite, por último: uma queda no meio deixa a pergunta na tela, e o
//     clique seguinte continua de onde parou, sem outra cópia.
func (f *Fila) EnviarHistorico(ctx context.Context, caminhoBanco string, agora time.Time) (ResultadoDoEnvio, error) {
	if _, aceito, err := lerChave(ctx, f.db, ChaveNuvemEnvioAceitoEm); err != nil {
		return ResultadoDoEnvio{}, err
	} else if aceito {
		return ResultadoDoEnvio{JaAceito: true}, nil
	}
	ids, err := f.elegiveis(ctx, f.db, agora)
	if err != nil {
		return ResultadoDoEnvio{}, err
	}
	var r ResultadoDoEnvio
	if len(ids) > 0 {
		_, temCopia, err := lerChave(ctx, f.db, ChaveNuvemCopia)
		if err != nil {
			return ResultadoDoEnvio{}, err
		}
		if !temCopia {
			if caminhoBanco == "" {
				return ResultadoDoEnvio{}, &ErroDaCopia{Motivo: ErrCopiaDoEnvio, Detalhe: "sem o caminho do banco"}
			}
			r.Copia = CaminhoDaCopiaDoEnvio(caminhoBanco, agora)
			if err := f.copiar(ctx, r.Copia); err != nil {
				return ResultadoDoEnvio{}, err
			}
		}
	}
	copia := r.Copia
	for i := 0; i < len(ids); i += BlocoDoEnvio {
		n, err := f.marcar(ctx, ids[i:min(i+BlocoDoEnvio, len(ids))], true, copia, agora)
		if err != nil {
			return r, err
		}
		r.Marcados += n
		copia = "" // registrada no primeiro bloco
	}
	if err := gravarChave(ctx, f.db, ChaveNuvemEnvioAceitoEm, emTexto(agora)); err != nil {
		return r, err
	}
	return r, nil
}

// ganchoDaCopia é chamado com a conexão já segura pela cópia, antes da
// contagem: só os testes o ligam (a medida da espera do Put).
var ganchoDaCopia func()

// copiar faz a cópia do envio em destino (copiarSegurando, com a marca do
// envio).
func (f *Fila) copiar(ctx context.Context, destino string) error {
	return copiarSegurando(ctx, f.db, destino, marcaDaCopiaDoEnvio, ErrCopiaDoEnvio)
}

// copiarSegurando faz uma cópia do banco em destino, com a marca dada no nome
// (MaxCopias por marca), segurando a conexão do banco entre a contagem e o
// VACUUM INTO (nenhuma escrita passa no meio), e a confere, tabela por
// tabela, depois de soltá-la. A que não dá certo é um *ErroDaCopia, com o
// motivo e a base (ErrCopiaDoEnvio ou ErrCopiaDosSeisMeses).
func copiarSegurando(ctx context.Context, db *sql.DB, destino, marca string, base error) error {
	antes, err := func() (map[string]int64, error) {
		conn, err := db.Conn(ctx)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		if ganchoDaCopia != nil {
			ganchoDaCopia()
		}
		tabelas, err := tabelasDoBanco(ctx, conn)
		if err != nil {
			return nil, err
		}
		antes, err := contarLinhas(ctx, conn, tabelas)
		if err != nil {
			return nil, err
		}
		return antes, vacuumInto(ctx, conn, destino, marca)
	}()
	if err == nil {
		err = conferirCopia(ctx, destino, antes)
	}
	if err == nil {
		return nil
	}
	motivo := base
	switch {
	case errors.Is(err, errCopiasDemais):
		motivo = ErrCopiasDemais
	case strings.Contains(strings.ToLower(err.Error()), "disk is full"),
		strings.Contains(strings.ToLower(err.Error()), "not enough space"):
		motivo = ErrDiscoCheio
	}
	detalhe := strings.NewReplacer(destino, filepath.Base(destino), filepath.Dir(destino), "…").Replace(err.Error())
	return &ErroDaCopia{Motivo: motivo, Detalhe: detalhe, base: base}
}

// marcar põe, numa transação, o marcador de criar em cada ditado de ids que
// ainda é elegível (o filtro vai de novo no INSERT; a janela foi conferida na
// leitura), sem tocar no que já está na fila. Com inicial, soma o total do
// progresso; com copia, registra onde ela ficou. Devolve quantos entraram.
func (f *Fila) marcar(ctx context.Context, ids []string, inicial bool, copia string, agora time.Time) (int, error) {
	n := 0
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		n = 0
		flag := 0
		if inicial {
			flag = 1
		}
		for _, id := range ids {
			args := append([]any{EntidadeDitado, emTexto(agora), flag, id}, f.argsElegivel()...)
			res, err := tx.ExecContext(ctx, `INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, inicial)
				SELECT ?, t.id, 0, ?, ? FROM local_transcriptions t WHERE t.id = ? AND `+filtroElegivel+`
				ON CONFLICT (entity_type, entity_id) DO NOTHING`, args...)
			if err != nil {
				return fmt.Errorf("storage: pondo o histórico na fila do sync: %w", err)
			}
			k, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("storage: pondo o histórico na fila do sync: %w", err)
			}
			n += int(k)
		}
		if inicial && n > 0 {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO instalacao (nome, valor) VALUES (?, ?)
				 ON CONFLICT (nome) DO UPDATE SET valor = CAST(CAST(instalacao.valor AS INTEGER) + CAST(excluded.valor AS INTEGER) AS TEXT)`,
				ChaveNuvemEnvioTotal, strconv.Itoa(n)); err != nil {
				return fmt.Errorf("storage: somando o total do envio: %w", err)
			}
		}
		if copia != "" {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO NOTHING`,
				ChaveNuvemCopia, copia); err != nil {
				return fmt.Errorf("storage: registrando a cópia do envio: %w", err)
			}
		}
		return nil
	})
	return n, err
}

// CompletarEnvio é o começo de toda rodada do sync (Motor.Rodada, logo depois
// do Preparar e da varredura do aprendido):
//   - sem o aceite: se não há nada elegível (N = 0) e nenhuma troca aprendida
//     a subir (trocas, o M que a varredura contou: o que o clique solta), grava
//     o aceite (o envio inicial de um histórico sem nada a enviar está feito,
//     sem cópia); senão, espera o clique e não faz nada. Com M > 0, a pergunta
//     aparece mesmo com N = 0 (ADR-019, "Decisões da F8", D3): sem isso, num
//     PC com o histórico vazio, a primeira rodada gravaria o aceite e as
//     trocas subiriam sem pergunta;
//   - com o aceite: marca o que ficou de fora (o ditado novo feito
//     desconectado, o restaurado desconectado), sem cópia nem pergunta, e
//     fora do progresso (inicial 0); e, sem nenhum marcador do envio inicial
//     na fila, apaga o total dele.
//
// Devolve quantos marcou.
func (f *Fila) CompletarEnvio(ctx context.Context, agora time.Time, trocas int) (int, error) {
	_, aceito, err := lerChave(ctx, f.db, ChaveNuvemEnvioAceitoEm)
	if err != nil {
		return 0, err
	}
	ids, err := f.elegiveis(ctx, f.db, agora)
	if err != nil {
		return 0, err
	}
	if !aceito {
		if len(ids) == 0 && trocas == 0 {
			return 0, gravarChave(ctx, f.db, ChaveNuvemEnvioAceitoEm, emTexto(agora))
		}
		return 0, nil
	}
	marcados := 0
	for i := 0; i < len(ids); i += BlocoDoEnvio {
		n, err := f.marcar(ctx, ids[i:min(i+BlocoDoEnvio, len(ids))], false, "", agora)
		if err != nil {
			return marcados, err
		}
		marcados += n
	}
	if _, err := f.db.ExecContext(ctx,
		`DELETE FROM instalacao WHERE nome = ? AND NOT EXISTS (SELECT 1 FROM sync_outbox WHERE inicial = 1)`,
		ChaveNuvemEnvioTotal); err != nil {
		return marcados, fmt.Errorf("storage: apagando o total do envio: %w", err)
	}
	return marcados, nil
}

// EnvioAceito diz se o "Enviar o histórico" já foi aceito (clicado, ou
// gravado sozinho pelo CompletarEnvio sem nada a enviar). Só lê. É o que a
// varredura do aprendido consulta (ADR-019, "Decisões da F8", D3): antes do
// aceite, ela não põe o upsert, e o conta (o M).
func (f *Fila) EnvioAceito(ctx context.Context) (bool, error) {
	_, aceito, err := lerChave(ctx, f.db, ChaveNuvemEnvioAceitoEm)
	return aceito, err
}

// Progresso é o envio inicial em curso: feitos de total ("enviando 120 de
// 800"). Total zero quando não há nenhum marcador dele na fila. Uma recusa ou
// um gone tiram o marcador e contam como feitos.
func (f *Fila) Progresso(ctx context.Context) (feitos, total int, err error) {
	return progresso(ctx, f.db)
}

func progresso(ctx context.Context, q consulta) (feitos, total int, err error) {
	var faltam int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_outbox WHERE inicial = 1`).Scan(&faltam); err != nil {
		return 0, 0, fmt.Errorf("storage: contando o envio inicial: %w", err)
	}
	if faltam == 0 {
		return 0, 0, nil
	}
	valor, _, err := lerChave(ctx, q, ChaveNuvemEnvioTotal)
	if err != nil {
		return 0, 0, err
	}
	total, _ = strconv.Atoi(valor)
	if total < faltam {
		total = faltam
	}
	return total - faltam, total, nil
}

// Resumo é o que o painel mostra da nuvem do lado do histórico, numa
// transação de leitura (uma foto só).
func (f *Fila) Resumo(ctx context.Context, agora time.Time) (ResumoDoEnvio, error) {
	var r ResumoDoEnvio
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		r = ResumoDoEnvio{}
		ids, err := f.elegiveis(ctx, tx, agora)
		if err != nil {
			return err
		}
		r.Previa = len(ids)
		if _, r.Aceito, err = lerChave(ctx, tx, ChaveNuvemEnvioAceitoEm); err != nil {
			return err
		}
		if !r.Aceito {
			if r.Trocas, err = f.trocasSeguradas(ctx, tx); err != nil {
				return err
			}
		}
		if _, r.OutraNuvem, err = lerChave(ctx, tx, ChaveNuvemDitadosEmOutra); err != nil {
			return err
		}
		if r.Feitos, r.Total, err = progresso(ctx, tx); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_outbox WHERE entity_type = ?`, EntidadeDitado).
			Scan(&r.Pendentes); err != nil {
			return fmt.Errorf("storage: contando a fila do sync: %w", err)
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sync_outbox o JOIN local_transcriptions t ON t.id = o.entity_id
			 WHERE o.entity_type = ? AND t.user_id = ? AND (t.server_version IS NOT NULL OR o.campos <> 0)`,
			EntidadeDitado, f.dono).Scan(&r.Mudancas); err != nil {
			return fmt.Errorf("storage: contando as mudanças na fila do sync: %w", err)
		}
		// O aprendido (F8e): o marcador learned de uma mudança feita aqui (a
		// troca aprendida, desfeita ou trocada) que a nuvem não confirmou: a
		// versão local acima da base. Como no ditado, o marcador que o epoch
		// novo põe num par que a nuvem já tinha confirmado não conta.
		var trocas int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sync_outbox o JOIN sync_aprendido a ON a.entity_id = o.entity_id
			 WHERE o.entity_type = ? AND a.user_id = ? AND a.versao_local > a.versao_base`,
			EntidadeAprendido, f.dono).Scan(&trocas); err != nil {
			return fmt.Errorf("storage: contando as trocas na fila do sync: %w", err)
		}
		r.Mudancas += trocas
		// As notas e os marcadores de nota (N7): o marcador de uma entidade que
		// a nuvem tem (a versão dela) e a lápide local (a nuvem tem, ou pode
		// ter, o que foi apagado de vez). A nota nova, que ainda não subiu,
		// não conta, como o ditado novo. Das imagens (N10b), só a lápide (a
		// imagem tirada da nota que a nuvem ainda mostra): a nova é como a nota
		// nova.
		var notas int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sync_outbox o
			 WHERE (o.entity_type = ? AND (o.campos & ? <> 0 OR EXISTS (SELECT 1 FROM local_notes n
			         WHERE n.id = o.entity_id AND n.user_id = ? AND n.server_version IS NOT NULL)))
			    OR (o.entity_type = ? AND (o.campos & ? <> 0 OR EXISTS (SELECT 1 FROM local_labels l
			         WHERE l.id = o.entity_id AND l.user_id = ? AND l.server_version IS NOT NULL)))
			    OR (o.entity_type = ? AND o.campos & ? <> 0)`,
			EntidadeNota, CampoApagado, f.dono, EntidadeMarcadorDeNota, CampoApagado, f.dono,
			EntidadeImagemDaNota, CampoApagado).Scan(&notas); err != nil {
			return fmt.Errorf("storage: contando as notas na fila do sync: %w", err)
		}
		r.Mudancas += notas
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM local_transcriptions WHERE user_id = ? AND server_version IS NULL AND sync_state <> ?`,
			f.dono, SituacaoNormal).Scan(&r.NaoSubiram); err != nil {
			return fmt.Errorf("storage: contando o que não subiu: %w", err)
		}
		var sucesso, erro sql.NullString
		err = tx.QueryRowContext(ctx,
			`SELECT last_success_at, ultimo_erro FROM sync_state WHERE user_id = ? ORDER BY last_success_at DESC LIMIT 1`,
			f.dono).Scan(&sucesso, &erro)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("storage: lendo a última rodada do sync: %w", err)
		default:
			r.Falhou = erro.Valid
			if sucesso.Valid {
				if em, err := deTempo(sucesso.String); err == nil {
					r.ConferidoEm = &em
				}
			}
		}
		return nil
	})
	return r, err
}

// trocasSeguradas conta, na transação, os pares do espelho que esperam o
// clique para subir (EstadoDoAprendido.SeguradoPeloClique).
func (f *Fila) trocasSeguradas(ctx context.Context, tx *sql.Tx) (int, error) {
	ids, err := f.idsDoAprendido(ctx, tx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		e, err := f.estadoDoAprendido(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if e.SeguradoPeloClique() {
			n++
		}
	}
	return n, nil
}

// lerChave lê uma chave da tabela instalacao.
func lerChave(ctx context.Context, q consulta, nome string) (string, bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valor FROM instalacao WHERE nome = ?`, nome).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("storage: lendo a instalação: %w", err)
	}
	return v, true, nil
}

// gravarChave grava uma chave da tabela instalacao.
func gravarChave(ctx context.Context, db *sql.DB, nome, valor string) error {
	if _, err := db.ExecContext(ctx,
		`INSERT INTO instalacao (nome, valor) VALUES (?, ?) ON CONFLICT (nome) DO UPDATE SET valor = excluded.valor`,
		nome, valor); err != nil {
		return fmt.Errorf("storage: gravando a instalação: %w", err)
	}
	return nil
}

// HistoricoDaNuvem é o que o painel usa do histórico para a nuvem: o resumo,
// o "Enviar o histórico" e o "Esquecer esta nuvem", sobre a fila do dono e o
// arquivo do banco (a cópia fica ao lado dele).
type HistoricoDaNuvem struct {
	Fila           *Fila
	CaminhoDoBanco string
}

// Resumo é Fila.Resumo.
func (h HistoricoDaNuvem) Resumo(ctx context.Context, agora time.Time) (ResumoDoEnvio, error) {
	return h.Fila.Resumo(ctx, agora)
}

// Enviar é Fila.EnviarHistorico com o caminho do banco.
func (h HistoricoDaNuvem) Enviar(ctx context.Context, agora time.Time) (ResultadoDoEnvio, error) {
	return h.Fila.EnviarHistorico(ctx, h.CaminhoDoBanco, agora)
}
