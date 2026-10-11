package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// O aprendido no PC (ADR-019, "Decisões da F8", F8e): o espelho
// (sync_aprendido, migration 0007), o conjunto "veio da nuvem"
// (sync_veio_da_nuvem) e o marcador learned na sync_outbox. Como no ditado,
// as DECISÕES (a varredura do arquivo diante do espelho, o que fazer com o
// pull e com a resposta do push) moram em internal/sync, puras (F8f); aqui só
// se lê o estado de um id e se executa a AcaoDoAprendido decidida, na
// transação certa. Nada aqui lê nem escreve o dicionario.json.
//
// # O que o espelho guarda, por id e pelo dono
//
//   - a base: a server_version que a nuvem confirmou, se o par dela está
//     desfeito, e a versão local que ela cobre (versao_base). A base não
//     guarda outra cópia do texto do par: o de uma linha que vale no arquivo
//     é a projeção guardada, que o dicionario.json já tem;
//   - a versão local, que sobe a cada mudança daqui (AcaoDoAprendido.
//     MudouAqui, da varredura, e SubirVersaoLocal, no ato do Aprender e do
//     Desfazer do painel). VersaoLocal acima da VersaoBase é "mudou aqui, e a
//     nuvem não confirmou";
//   - a projeção guardada (dictionary.ParDoPainel): o que o arquivo tinha
//     deste id da última vez que a varredura ou o aplicador o viram. Ausente
//     (nil): o id fora da projeção;
//   - o par da nuvem guardado, com a versão: o que ainda não vale aqui (não
//     aplicado, pulado, arquivo indisponível), o da intenção ("aplicando") ou
//     o da etiqueta ("desfeito em outro aparelho", "trocado em outro
//     aparelho");
//   - a projeção esperada da intenção do aplicador (write-ahead), só na
//     situação "aplicando";
//   - a situação e o motivo, de listas fechadas, e quando a situação começou;
//   - o par perdido (a 0008): a troca deste PC que perdeu na nuvem, só com a
//     etiqueta (e na linha "aplicando" que a aplica). É a linha de estado do
//     "trocado em outro aparelho", ao lado da que vale aqui, que é a da
//     nuvem.
//
// # O marcador
//
// Um por id na sync_outbox: entity_type learned, campos 0, nunca inicial,
// sem texto. A operação não fica nele: sai do espelho na hora do envio
// (EstadoDoAprendido.Presente: presente aqui, upsert; ausente, delete).
// Durante o voo, a projeção guardada não muda (ErrProjecaoEmVoo): o reenvio
// repete a mesma operação, com o mesmo mutation_id, e a mudança feita durante
// o voo só sobe a versão local (o "mudou durante o voo"), e sobe depois, com
// outro id e a base que a resposta trouxe. Na reconexão, o envio em voo fica
// a repetir (Marcador.Repetir): a mesma operação, com o id do aparelho novo,
// e a projeção continua congelada até a resposta dele.
//
// # O "Enviar o histórico" (D3)
//
// Antes do aceite (ChaveNuvemEnvioAceitoEm), só sai o delete: o upsert de
// qualquer par espera o clique (PendentesDoAprendido o segura na fila). Quem
// decide se o delete pode nascer antes do clique (só de um id que a nuvem
// tem) é a varredura.

// EntidadeAprendido é o entity_type do aprendido no sync.
const EntidadeAprendido = "learned"

// ChaveNuvemConta: a conta (o id dela na nuvem) a que esta instalação está
// ligada, gravada no pareamento (internal/agent/nuvem). O esquecer a guarda
// junto do "desfeito aqui" (conta_esquecida) antes de apagá-la.
const ChaveNuvemConta = "nuvem_conta"

// ChaveNuvemDicionario: o "usuario" do dicionario.json que o espelho do
// aprendido conhece, gravado pelo Agent na subida (cmd/agent,
// prepararOAprendido). Outro usuário na subida seguinte, ou o arquivo
// recriado vazio, é outro arquivo: o espelho do antigo é solto antes de o
// laço rodar (syncengine.SoltarOArquivoAntigo), para os pares dele não virarem
// delete. O valor vazio é o arquivo novo cujo espelho ainda não foi solto (a
// subida que caiu no meio): a seguinte refaz. Só o nome do usuário, que o
// próprio arquivo traz; nada do que foi aprendido.
const ChaveNuvemDicionario = "nuvem_dicionario_usuario"

// SituacaoDoEspelho é a situação de uma linha do espelho. Lista fechada
// (SituacoesDoEspelho); EspelhoNenhuma é a linha sem situação.
type SituacaoDoEspelho string

const (
	// EspelhoNenhuma: nenhuma situação a mostrar.
	EspelhoNenhuma SituacaoDoEspelho = ""
	// EspelhoAplicado: o par da nuvem passou a valer aqui.
	EspelhoAplicado SituacaoDoEspelho = "aplicado"
	// EspelhoAplicando: a intenção do aplicador, gravada antes do arquivo
	// (write-ahead), com a projeção esperada. A varredura a reconcilia pelo
	// arquivo, e o esquecer não a toca.
	EspelhoAplicando SituacaoDoEspelho = "aplicando"
	// EspelhoNaoAplicado: o par da nuvem não vale aqui; o motivo é um
	// dictionary.MotivoDoPar.
	EspelhoNaoAplicado SituacaoDoEspelho = "nao_aplicado"
	// EspelhoDesfeitoAqui: o par saiu do arquivo aqui, e a nuvem não
	// confirmou. A regra do esquecer e do epoch novo não depende dela: lê a
	// projeção e as versões (DesfeitoAqui).
	EspelhoDesfeitoAqui SituacaoDoEspelho = "desfeito_aqui"
	// EspelhoDesfeitoEmOutro: a etiqueta "desfeito em outro aparelho" (uma
	// mudança deste PC perdeu na nuvem para um desfazer).
	EspelhoDesfeitoEmOutro SituacaoDoEspelho = "desfeito_em_outro"
	// EspelhoTrocadoEmOutro: a etiqueta "trocado em outro aparelho" (perdeu
	// para outro termo).
	EspelhoTrocadoEmOutro SituacaoDoEspelho = "trocado_em_outro"
	// EspelhoJaValeAqui: a variante vale aqui sem a origem "painel" (posta à
	// mão, ou adotada).
	EspelhoJaValeAqui SituacaoDoEspelho = "ja_vale_aqui"
	// EspelhoRecusado: a nuvem recusou a mudança daqui; o motivo é o código
	// da recusa, e a projeção guardada é a recusada (só uma mudança do par,
	// um epoch novo ou o esquecer liberam a linha).
	EspelhoRecusado SituacaoDoEspelho = "recusado"
)

// SituacoesDoEspelho é a lista fechada das situações (sem EspelhoNenhuma).
func SituacoesDoEspelho() []SituacaoDoEspelho {
	return []SituacaoDoEspelho{
		EspelhoAplicado, EspelhoAplicando, EspelhoNaoAplicado, EspelhoDesfeitoAqui,
		EspelhoDesfeitoEmOutro, EspelhoTrocadoEmOutro, EspelhoJaValeAqui, EspelhoRecusado,
	}
}

// O que fazer com o id no conjunto "veio da nuvem" (AcaoDoAprendido.
// VeioDaNuvem).
const (
	// VeioFica: não mexe.
	VeioFica = iota
	// VeioEntra: o aplicador vai escrever o par no arquivo.
	VeioEntra
	// VeioSai: a pessoa aprendeu o par aqui (a varredura vê a mudança como
	// daqui).
	VeioSai
)

// ParPerdido é a troca deste PC que perdeu na nuvem (D4): a variante e o
// termo que a projeção guardada tinha quando o conflict chegou. Só o texto da
// linha de estado: nada a aplica nem a envia.
type ParPerdido struct {
	Variante, Termo string
}

// GuardaOPerdido diz se s é uma situação que guarda o par perdido: as
// etiquetas da D4, e a "aplicando" (a intenção que aplica o par da nuvem de
// um conflict que este PC perdeu, e que devolve a etiqueta ao terminar).
// Qualquer outra situação gravada tira o par perdido.
func GuardaOPerdido(s SituacaoDoEspelho) bool {
	return s == EspelhoDesfeitoEmOutro || s == EspelhoTrocadoEmOutro || s == EspelhoAplicando
}

// ParGuardado é o par da nuvem que uma linha do espelho guarda, com a versão
// dele na nuvem.
type ParGuardado struct {
	// Versao é a entity_version do par; nil sem versão (o epoch novo a
	// esquece).
	Versao *int64
	Par    dictionary.ParDaNuvem
}

// EstadoDoAprendido é o que o PC sabe de um id do aprendido: a linha do
// espelho (o zero quando não há), o marcador e o "veio da nuvem".
type EstadoDoAprendido struct {
	ID string
	// TemLinha: o id tem linha no espelho.
	TemLinha bool
	// ServerVersion é a versão da base; nil quando a nuvem não tem o par, ou
	// o PC a esqueceu.
	ServerVersion *int64
	// BaseDesfeita: o par que a nuvem confirmou está desfeito lá.
	BaseDesfeita bool
	// VersaoBase é a versão local que a base cobre; VersaoLocal sobe a cada
	// mudança daqui.
	VersaoBase, VersaoLocal int64
	// Projecao é a projeção guardada; nil: o id fora da projeção.
	Projecao *dictionary.ParDoPainel
	// Esperada é a projeção esperada da intenção, só em EspelhoAplicando; nil
	// nessa situação é a intenção que tira o par.
	Esperada *dictionary.ParDoPainel
	// Nuvem é o par da nuvem guardado; nil sem par.
	Nuvem *ParGuardado
	// Situacao, Motivo e SituacaoEm (quando a situação começou; nil sem
	// situação).
	Situacao   SituacaoDoEspelho
	Motivo     string
	SituacaoEm *time.Time
	// Perdido é a troca deste PC que perdeu na nuvem (a da etiqueta); nil
	// sem ela (sem etiqueta, ou quando o que perdeu foi um desfazer daqui, que
	// não tem par).
	Perdido *ParPerdido
	// Marcador é o da fila; nil sem marcador.
	Marcador *Marcador
	// VeioDaNuvem: o id está no conjunto "veio da nuvem".
	VeioDaNuvem bool
	// MesmaContaEsquecida: a linha guarda, de um esquecer, a conta a que a
	// mudança daqui pertence (conta_esquecida), e ela é a conta a que a
	// instalação está ligada agora. Só a linha com a projeção presente e
	// mudada aqui, que a nuvem desta conta tinha, a guarda depois da primeira
	// rodada (soltarDesfeitosDeOutraConta): o Desfazer que a varredura ainda
	// não tinha visto quando o PC esqueceu a nuvem. A varredura não o assenta
	// quando vê o par sair do arquivo: o pull traz a versão, e o desfazer
	// sobe. A marca sai quando a linha ganha uma projeção, uma base ou uma
	// versão coberta (executarAprendido).
	MesmaContaEsquecida bool
}

// Presente diz se o par está na projeção guardada: o envio dele é upsert;
// senão, delete.
func (e EstadoDoAprendido) Presente() bool { return e.Projecao != nil }

// MudouAqui diz se houve mudança daqui que a nuvem não confirmou (a versão
// local passou da base).
func (e EstadoDoAprendido) MudouAqui() bool { return e.VersaoLocal > e.VersaoBase }

// DesfeitoAqui diz se o par saiu daqui e a nuvem não confirmou: ausente da
// projeção guardada, com a versão local acima da base. É a regra que o
// esquecer e o epoch novo usam (a situação EspelhoDesfeitoAqui não é
// gravada). A projeção ausente é a que a varredura gravou: o Desfazer do
// painel que ela ainda não viu (o PC desconectado, ou o par com um envio em
// voo, cuja projeção não muda) só subiu a versão local, e a projeção guardada
// ainda é a do par. Esse não é DesfeitoAqui; o esquecer o guarda pela conta
// também (a linha presente mudada aqui que a nuvem tinha: esquecerAprendido),
// e a varredura o reconhece de volta à mesma conta (MesmaContaEsquecida).
func (e EstadoDoAprendido) DesfeitoAqui() bool { return !e.Presente() && e.MudouAqui() }

// SeguradoPeloClique diz se o par espera o clique em "Enviar o histórico"
// para subir (ADR-019, "Decisões da F8", D3: o M da pergunta "Vão subir N
// ditados e M trocas aprendidas"), no estado que a última varredura deixou: o
// par presente (na projeção guardada; na linha "aplicando", na esperada),
// fora de um envio congelado e da linha "recusado", mudado aqui, ou sem
// versão da nuvem e fora do "veio da nuvem". É a linha do upsert da varredura
// (internal/sync, DecidirVarredura, Segura) com o arquivo igual à projeção
// guardada: o teste do sync confere que as duas contam igual. Só quer dizer
// algo antes do aceite; depois dele, o upsert vai à fila.
func (e EstadoDoAprendido) SeguradoPeloClique() bool {
	p := e.Projecao
	if e.Situacao == EspelhoAplicando {
		p = e.Esperada
	}
	if p == nil || e.Situacao == EspelhoRecusado || e.Marcador != nil && e.Marcador.Congelado() {
		return false
	}
	nuvemTem := e.ServerVersion != nil || e.Nuvem != nil && e.Nuvem.Versao != nil
	return e.MudouAqui() || !nuvemTem && !e.VeioDaNuvem
}

// AcaoDoAprendido é o que uma decisão manda fazer com um id do aprendido,
// executada na transação de quem a recebe (MudarAprendido, Aplicar). O zero
// não faz nada, e não escreve no banco.
type AcaoDoAprendido struct {
	// Base grava a versão da nuvem e se o par dela está desfeito.
	Base *BaseDoAprendido
	// ZerarBase esquece a versão da nuvem (a base_invalida: o próximo envio
	// vai com a base 0).
	ZerarBase bool
	// Cobre grava a versão local que a base passa a cobrir (de 0 até a
	// versão local de agora): a enviada, na resposta do push; a lida antes
	// do arquivo, no aplicador.
	Cobre *int64
	// Projecao grava a projeção guardada. Recusada durante o voo
	// (ErrProjecaoEmVoo).
	Projecao *NovaProjecao
	// MudouAqui sobe a versão local (a varredura viu uma mudança daqui).
	MudouAqui bool
	// Nuvem grava o par da nuvem guardado; LimparNuvem o tira (o texto e a
	// versão).
	Nuvem       *ParGuardado
	LimparNuvem bool
	// Situacao grava a situação e o motivo. A projeção esperada só existe em
	// EspelhoAplicando: ela vem em Esperada, e qualquer outra situação a
	// apaga.
	Situacao *NovaSituacao
	Esperada *NovaProjecao
	// Perdido grava o par perdido, só junto de uma etiqueta da D4 (em
	// Situacao). A situação gravada fora de GuardaOPerdido o tira.
	Perdido *ParPerdido
	// Marcador é MarcadorFica, MarcadorSai, MarcadorReenviar ou
	// MarcadorCriar (põe o marcador, se não houver: campos 0, nunca
	// inicial).
	Marcador int
	// VeioDaNuvem é VeioFica, VeioEntra ou VeioSai.
	VeioDaNuvem int
}

// vazia diz se a ação não faz nada.
func (a AcaoDoAprendido) vazia() bool {
	return a == AcaoDoAprendido{}
}

// BaseDoAprendido é a base que a nuvem confirmou.
type BaseDoAprendido struct {
	ServerVersion int64
	Desfeita      bool
}

// NovaProjecao é uma projeção a gravar; Par nil: o id fora da projeção.
type NovaProjecao struct {
	Par *dictionary.ParDoPainel
}

// NovaSituacao é a situação a gravar, com o motivo: um dictionary.MotivoDoPar
// em EspelhoNaoAplicado; o código da recusa (de a a z e "_", até 40) em
// EspelhoRecusado; vazio nas outras.
type NovaSituacao struct {
	Situacao SituacaoDoEspelho
	Motivo   string
}

// Os erros da execução de uma ação do aprendido. São defeito de quem decide
// (internal/sync confere antes), e desfazem a transação inteira. Nenhum traz
// texto.
var (
	// ErrAcaoDoAprendido: a ação não é válida (o par não é deste id, não
	// passa no ConferirPar, a situação ou o motivo fora da lista, a versão
	// coberta fora de 0 até a local, a esperada sem "aplicando").
	ErrAcaoDoAprendido = errors.New("storage: ação do aprendido inválida")
	// ErrProjecaoEmVoo: a ação troca a projeção guardada de um id com o
	// envio em voo. O reenvio repete a operação e o payload que saíram dela.
	ErrProjecaoEmVoo = errors.New("storage: a projeção do aprendido não muda durante o voo do envio")
)

func acaoInvalida(motivo string) error { return fmt.Errorf("%w: %s", ErrAcaoDoAprendido, motivo) }

// ─── Ler ───

// colunasDoEspelho são as colunas lidas de sync_aprendido, na ordem de
// lerLinhaDoEspelho.
const colunasDoEspelho = `entity_id, server_version, base_desfeita, versao_base, versao_local,
	variante, termo, restrito, dominio, em_alias_restrito,
	esperada_variante, esperada_termo, esperada_restrito, esperada_dominio, esperada_em_alias_restrito,
	nuvem_versao, nuvem_variante, nuvem_termo, nuvem_restrito, nuvem_dominio, nuvem_desfeito,
	situacao, motivo, situacao_em,
	COALESCE(conta_esquecida = (SELECT valor FROM instalacao WHERE nome = '` + ChaveNuvemConta + `'), 0),
	perdido_variante, perdido_termo`

// lerLinhaDoEspelho lê uma linha de sync_aprendido (colunasDoEspelho) para e.
func lerLinhaDoEspelho(s interface{ Scan(...any) error }, e *EstadoDoAprendido) error {
	var (
		sv, nuvemVersao              sql.NullInt64
		baseDesfeita                 int64
		variante, termo, dominio     sql.NullString
		restrito, emRestrito         sql.NullInt64
		eVariante, eTermo, eDominio  sql.NullString
		eRestrito, eEmRestrito       sql.NullInt64
		nVariante, nTermo, nDominio  sql.NullString
		nRestrito, nDesfeito         sql.NullInt64
		situacao, motivo, situacaoEm sql.NullString
		mesmaConta                   int64
		pVariante, pTermo            sql.NullString
	)
	if err := s.Scan(&e.ID, &sv, &baseDesfeita, &e.VersaoBase, &e.VersaoLocal,
		&variante, &termo, &restrito, &dominio, &emRestrito,
		&eVariante, &eTermo, &eRestrito, &eDominio, &eEmRestrito,
		&nuvemVersao, &nVariante, &nTermo, &nRestrito, &nDominio, &nDesfeito,
		&situacao, &motivo, &situacaoEm, &mesmaConta, &pVariante, &pTermo); err != nil {
		return err
	}
	if pVariante.Valid {
		e.Perdido = &ParPerdido{Variante: pVariante.String, Termo: pTermo.String}
	}
	e.TemLinha = true
	e.MesmaContaEsquecida = mesmaConta == 1
	if sv.Valid {
		v := sv.Int64
		e.ServerVersion = &v
	}
	e.BaseDesfeita = baseDesfeita == 1
	e.Projecao = projecaoLida(e.ID, variante, termo, restrito, dominio, emRestrito)
	e.Esperada = projecaoLida(e.ID, eVariante, eTermo, eRestrito, eDominio, eEmRestrito)
	if nVariante.Valid {
		g := &ParGuardado{Par: dictionary.ParDaNuvem{
			Variante: nVariante.String, Termo: nTermo.String, Restrito: nRestrito.Int64 == 1,
			Dominio: nDominio.String, Desfeito: nDesfeito.Int64 == 1,
		}}
		if nuvemVersao.Valid {
			v := nuvemVersao.Int64
			g.Versao = &v
		}
		e.Nuvem = g
	}
	e.Situacao, e.Motivo = SituacaoDoEspelho(situacao.String), motivo.String
	em, err := deTempoNulo(situacaoEm)
	if err != nil {
		return err
	}
	e.SituacaoEm = em
	return nil
}

// projecaoLida monta a projeção de um grupo de colunas; nil quando a
// variante é nula.
func projecaoLida(id string, variante, termo sql.NullString, restrito sql.NullInt64, dominio sql.NullString, emRestrito sql.NullInt64) *dictionary.ParDoPainel {
	if !variante.Valid {
		return nil
	}
	return &dictionary.ParDoPainel{
		ID: id, Variante: variante.String, Termo: termo.String, Restrito: restrito.Int64 == 1,
		Dominio: dominio.String, EmAliasRestrito: emRestrito.Int64 == 1,
	}
}

// estadoDoAprendido lê, na transação, a linha do espelho, o marcador e o
// "veio da nuvem" de um id.
func (f *Fila) estadoDoAprendido(ctx context.Context, tx *sql.Tx, id string) (EstadoDoAprendido, error) {
	e := EstadoDoAprendido{ID: id}
	err := lerLinhaDoEspelho(tx.QueryRowContext(ctx,
		`SELECT `+colunasDoEspelho+` FROM sync_aprendido WHERE user_id = ? AND entity_id = ?`, f.dono, id), &e)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		e = EstadoDoAprendido{ID: id}
	case err != nil:
		return e, fmt.Errorf("storage: lendo o espelho do aprendido: %w", err)
	}
	ms, err := lerMarcadores(ctx, tx, `WHERE entity_type = ? AND entity_id = ?`, EntidadeAprendido, id)
	if err != nil {
		return e, err
	}
	if len(ms) == 1 {
		e.Marcador = &ms[0]
	}
	var veio int
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sync_veio_da_nuvem WHERE user_id = ? AND entity_id = ?)`, f.dono, id).Scan(&veio); err != nil {
		return e, fmt.Errorf("storage: lendo o que veio da nuvem: %w", err)
	}
	e.VeioDaNuvem = veio == 1
	return e, nil
}

// Espelho devolve, numa leitura só, o estado de todo id do aprendido que o PC
// conhece (com linha no espelho, marcador ou no "veio da nuvem"), em ordem de
// id. É o que a varredura compara com o arquivo, e o que a Local API mostra
// das linhas de estado.
func (f *Fila) Espelho(ctx context.Context) ([]EstadoDoAprendido, error) {
	var out []EstadoDoAprendido
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		out = nil
		ids, err := f.idsDoAprendido(ctx, tx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			e, err := f.estadoDoAprendido(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

// idsDoAprendido lista, em ordem, os ids do aprendido deste dono: os do
// espelho, os do "veio da nuvem" e os dos marcadores learned.
func (f *Fila) idsDoAprendido(ctx context.Context, tx *sql.Tx) ([]string, error) {
	linhas, err := tx.QueryContext(ctx,
		`SELECT entity_id FROM sync_aprendido WHERE user_id = ?
		 UNION SELECT entity_id FROM sync_veio_da_nuvem WHERE user_id = ?
		 UNION SELECT entity_id FROM sync_outbox WHERE entity_type = ?
		 ORDER BY 1`, f.dono, f.dono, EntidadeAprendido)
	if err != nil {
		return nil, fmt.Errorf("storage: listando o aprendido: %w", err)
	}
	defer linhas.Close()
	var ids []string
	for linhas.Next() {
		var id string
		if err := linhas.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: listando o aprendido: %w", err)
		}
		ids = append(ids, id)
	}
	if err := linhas.Err(); err != nil {
		return nil, fmt.Errorf("storage: listando o aprendido: %w", err)
	}
	return ids, nil
}

// ─── Mudar ───

// MudarAprendido executa, numa transação, o que decidir manda fazer com cada
// id de ids, na ordem, sem conferir o epoch: é a varredura (a projeção nova e
// os marcadores juntos), a intenção do aplicador antes do arquivo
// (write-ahead) e a reconciliação dela. decidir recebe o estado lido NESTA
// transação e não pode tocar o banco. A ação vazia não escreve: a varredura
// que não acha nada não escreve no SQLite.
func (f *Fila) MudarAprendido(ctx context.Context, agora time.Time, ids []string, decidir func(EstadoDoAprendido) AcaoDoAprendido) error {
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		return f.mudarAprendido(ctx, tx, agora, ids, decidir)
	})
}

func (f *Fila) mudarAprendido(ctx context.Context, tx *sql.Tx, agora time.Time, ids []string, decidir func(EstadoDoAprendido) AcaoDoAprendido) error {
	for _, id := range ids {
		e, err := f.estadoDoAprendido(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := f.executarAprendido(ctx, tx, agora, e, decidir(e)); err != nil {
			return err
		}
	}
	return nil
}

// SubirVersaoLocal é o Aprender e o Desfazer do painel (o caminho do gancho
// AoMudar): para cada id que já tem linha no espelho, a versão local sobe um,
// com o laço parado ou o PC desconectado. Assim um Desfazer seguido de um
// Aprender do mesmo par, que deixa o arquivo como era, continua sendo
// mudança daqui. O id sem linha não ganha linha: a varredura o verá como par
// novo.
func (f *Fila) SubirVersaoLocal(ctx context.Context, ids []string) error {
	return naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				`UPDATE sync_aprendido SET versao_local = versao_local + 1 WHERE user_id = ? AND entity_id = ?`,
				f.dono, id); err != nil {
				return fmt.Errorf("storage: subindo a versão local do aprendido: %w", err)
			}
		}
		return nil
	})
}

// executarAprendido faz o que a ação manda com o id de e, na transação,
// depois de conferi-la inteira (conferirAcao). A ordem: a linha (criada se a
// ação escreve algo), o marcador, a projeção, a versão local, a base, o par
// da nuvem, a situação (com a esperada) e o "veio da nuvem".
func (f *Fila) executarAprendido(ctx context.Context, tx *sql.Tx, agora time.Time, e EstadoDoAprendido, a AcaoDoAprendido) error {
	if a.vazia() {
		return nil
	}
	if err := conferirAcao(e, a); err != nil {
		return err
	}
	if !e.TemLinha {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_aprendido (user_id, entity_id) VALUES (?, ?) ON CONFLICT (user_id, entity_id) DO NOTHING`,
			f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: criando a linha do espelho: %w", err)
		}
	}
	switch a.Marcador {
	case MarcadorSai:
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`, EntidadeAprendido, e.ID); err != nil {
			return fmt.Errorf("storage: tirando o marcador do aprendido: %w", err)
		}
	case MarcadorReenviar:
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_outbox SET mutation_id = NULL, versao_enviada = NULL WHERE entity_type = ? AND entity_id = ?`,
			EntidadeAprendido, e.ID); err != nil {
			return fmt.Errorf("storage: limpando o envio do aprendido: %w", err)
		}
	case MarcadorCriar:
		if err := porMarcadorDoAprendido(ctx, tx, e.ID, agora); err != nil {
			return err
		}
	}
	if a.Projecao != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET variante = ?, termo = ?, restrito = ?, dominio = ?, em_alias_restrito = ?
			 WHERE user_id = ? AND entity_id = ?`,
			append(colunasDaProjecao(a.Projecao.Par), f.dono, e.ID)...); err != nil {
			return fmt.Errorf("storage: gravando a projeção do aprendido: %w", err)
		}
	}
	if a.MudouAqui {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET versao_local = versao_local + 1 WHERE user_id = ? AND entity_id = ?`,
			f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: subindo a versão local do aprendido: %w", err)
		}
	}
	if a.ZerarBase {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET server_version = NULL WHERE user_id = ? AND entity_id = ?`, f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: esquecendo a base do aprendido: %w", err)
		}
	}
	if b := a.Base; b != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET server_version = ?, base_desfeita = ? WHERE user_id = ? AND entity_id = ?`,
			b.ServerVersion, umOuZero(b.Desfeita), f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: gravando a base do aprendido: %w", err)
		}
	}
	if a.Cobre != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET versao_base = ? WHERE user_id = ? AND entity_id = ?`, *a.Cobre, f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: gravando a versão que a base cobre: %w", err)
		}
	}
	if a.Projecao != nil || a.Base != nil || a.Cobre != nil {
		// A marca do esquecer (MesmaContaEsquecida) diz que a projeção
		// guardada é de antes dele, e que a nuvem desta conta tinha o par:
		// vale até a linha ganhar uma projeção vista agora, uma base ou uma
		// versão coberta.
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET conta_esquecida = NULL
			 WHERE user_id = ? AND entity_id = ? AND conta_esquecida IS NOT NULL`, f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: tirando a marca do esquecer: %w", err)
		}
	}
	if a.LimparNuvem {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET nuvem_versao = NULL, nuvem_variante = NULL, nuvem_termo = NULL,
			   nuvem_restrito = NULL, nuvem_dominio = NULL, nuvem_desfeito = NULL
			 WHERE user_id = ? AND entity_id = ?`, f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: tirando o par da nuvem guardado: %w", err)
		}
	}
	if g := a.Nuvem; g != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET nuvem_versao = ?, nuvem_variante = ?, nuvem_termo = ?, nuvem_restrito = ?,
			   nuvem_dominio = ?, nuvem_desfeito = ?
			 WHERE user_id = ? AND entity_id = ?`,
			inteiroNulo(g.Versao), g.Par.Variante, g.Par.Termo, umOuZero(g.Par.Restrito), dominioNulo(g.Par.Dominio),
			umOuZero(g.Par.Desfeito), f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: guardando o par da nuvem: %w", err)
		}
	}
	if s := a.Situacao; s != nil {
		var esperada []any
		if s.Situacao == EspelhoAplicando {
			esperada = colunasDaProjecao(a.Esperada.Par)
		} else {
			esperada = colunasDaProjecao(nil)
		}
		em := any(nil)
		switch {
		case s.Situacao == EspelhoNenhuma:
		case s.Situacao == e.Situacao && e.SituacaoEm != nil:
			// A mesma situação continua: a data é a de quando ela começou.
			em = emTexto(*e.SituacaoEm)
		default:
			em = emTexto(agora)
		}
		args := append([]any{textoOuNulo(string(s.Situacao)), textoOuNulo(s.Motivo), em}, esperada...)
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET situacao = ?, motivo = ?, situacao_em = ?,
			   esperada_variante = ?, esperada_termo = ?, esperada_restrito = ?, esperada_dominio = ?,
			   esperada_em_alias_restrito = ?
			 WHERE user_id = ? AND entity_id = ?`, append(args, f.dono, e.ID)...); err != nil {
			return fmt.Errorf("storage: gravando a situação do aprendido: %w", err)
		}
		if !GuardaOPerdido(s.Situacao) && e.Perdido != nil {
			// O par perdido só vive com a etiqueta: sai com ela.
			if _, err := tx.ExecContext(ctx,
				`UPDATE sync_aprendido SET perdido_variante = NULL, perdido_termo = NULL WHERE user_id = ? AND entity_id = ?`,
				f.dono, e.ID); err != nil {
				return fmt.Errorf("storage: tirando o par perdido: %w", err)
			}
		}
	}
	if p := a.Perdido; p != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET perdido_variante = ?, perdido_termo = ? WHERE user_id = ? AND entity_id = ?`,
			p.Variante, p.Termo, f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: guardando o par perdido: %w", err)
		}
	}
	switch a.VeioDaNuvem {
	case VeioEntra:
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_veio_da_nuvem (user_id, entity_id) VALUES (?, ?) ON CONFLICT (user_id, entity_id) DO NOTHING`,
			f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: anotando o que veio da nuvem: %w", err)
		}
	case VeioSai:
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM sync_veio_da_nuvem WHERE user_id = ? AND entity_id = ?`, f.dono, e.ID); err != nil {
			return fmt.Errorf("storage: tirando o que veio da nuvem: %w", err)
		}
	}
	return nil
}

// conferirAcao recusa a ação que deixaria o espelho incoerente. Nada é
// gravado com ela: a transação inteira desfaz.
func conferirAcao(e EstadoDoAprendido, a AcaoDoAprendido) error {
	switch a.Marcador {
	case MarcadorFica, MarcadorSai, MarcadorReenviar, MarcadorCriar:
	default:
		return acaoInvalida("marcador fora da lista")
	}
	switch a.VeioDaNuvem {
	case VeioFica, VeioEntra, VeioSai:
	default:
		return acaoInvalida("veio da nuvem fora da lista")
	}
	if a.Projecao != nil {
		// Durante o voo (e no envio a repetir depois de uma reconexão), a
		// projeção guardada é a do envio. A resposta que tira o marcador, ou
		// limpa o envio, pode trocá-la na mesma ação.
		emVoo := e.Marcador != nil && e.Marcador.Congelado()
		if emVoo && a.Marcador != MarcadorSai && a.Marcador != MarcadorReenviar {
			return ErrProjecaoEmVoo
		}
		if err := conferirProjecao(e.ID, a.Projecao.Par); err != nil {
			return err
		}
	}
	if a.Base != nil && a.ZerarBase {
		return acaoInvalida("a base gravada e zerada juntas")
	}
	if a.Base != nil && a.Base.ServerVersion < 1 {
		return acaoInvalida("versão da nuvem menor que 1")
	}
	if a.Cobre != nil {
		local := e.VersaoLocal
		if a.MudouAqui {
			local++
		}
		if *a.Cobre < 0 || *a.Cobre > local {
			return acaoInvalida("a versão coberta fora de 0 até a local")
		}
	}
	if a.Nuvem != nil && a.LimparNuvem {
		return acaoInvalida("o par da nuvem gravado e tirado juntos")
	}
	if g := a.Nuvem; g != nil {
		if g.Versao != nil && *g.Versao < 1 {
			return acaoInvalida("versão do par da nuvem menor que 1")
		}
		if dictionary.IDDoPar(g.Par.Variante) != e.ID {
			return acaoInvalida("o par da nuvem não é deste id")
		}
		if dictionary.ConferirPar(g.Par.Variante, g.Par.Termo, g.Par.Restrito, dominioDoFio(g.Par.Dominio)) != nil {
			return acaoInvalida("o par da nuvem não passa no ConferirPar")
		}
	}
	if a.Esperada != nil && (a.Situacao == nil || a.Situacao.Situacao != EspelhoAplicando) {
		return acaoInvalida("a projeção esperada sem a situação aplicando")
	}
	if p := a.Perdido; p != nil {
		if a.Situacao == nil || (a.Situacao.Situacao != EspelhoDesfeitoEmOutro && a.Situacao.Situacao != EspelhoTrocadoEmOutro) {
			return acaoInvalida("o par perdido sem a etiqueta")
		}
		if dictionary.IDDoPar(p.Variante) != e.ID {
			return acaoInvalida("o par perdido não é deste id")
		}
		if dictionary.ConferirPar(p.Variante, p.Termo, false, nil) != nil {
			return acaoInvalida("o par perdido não passa no ConferirPar")
		}
	}
	if s := a.Situacao; s != nil {
		if err := conferirSituacao(*s); err != nil {
			return err
		}
		if s.Situacao == EspelhoAplicando {
			if a.Esperada == nil {
				return acaoInvalida("a situação aplicando sem a projeção esperada")
			}
			if err := conferirProjecao(e.ID, a.Esperada.Par); err != nil {
				return err
			}
		}
	}
	return nil
}

// conferirProjecao: a projeção (nil é a ausente) é deste id e passa no
// ConferirPar, como a projeção do arquivo (dictionary.ParesDoPainel).
func conferirProjecao(id string, p *dictionary.ParDoPainel) error {
	if p == nil {
		return nil
	}
	if p.ID != id || dictionary.IDDoPar(p.Variante) != id {
		return acaoInvalida("a projeção não é deste id")
	}
	if dictionary.ConferirPar(p.Variante, p.Termo, p.Restrito, dominioDoFio(p.Dominio)) != nil {
		return acaoInvalida("a projeção não passa no ConferirPar")
	}
	return nil
}

// conferirSituacao confere a situação e o motivo contra as listas fechadas.
func conferirSituacao(s NovaSituacao) error {
	switch s.Situacao {
	case EspelhoNaoAplicado:
		for _, m := range dictionary.MotivosDoPar() {
			if string(m) == s.Motivo {
				return nil
			}
		}
		return acaoInvalida("motivo do não aplicado fora da lista")
	case EspelhoRecusado:
		if !codigoDeRecusa(s.Motivo) {
			return acaoInvalida("código da recusa fora do formato")
		}
		return nil
	case EspelhoNenhuma:
	default:
		valida := false
		for _, v := range SituacoesDoEspelho() {
			valida = valida || v == s.Situacao
		}
		if !valida {
			return acaoInvalida("situação fora da lista")
		}
	}
	if s.Motivo != "" {
		return acaoInvalida("motivo numa situação que não tem motivo")
	}
	return nil
}

// codigoDeRecusa: o código de uma recusa da nuvem cabe no espelho (de 1 a 40
// caracteres, de a a z e "_"), como o last_sync_error do ditado. O que vem
// fora disso, internal/sync troca por um código próprio antes.
func codigoDeRecusa(c string) bool {
	if c == "" || len(c) > 40 {
		return false
	}
	for i := 0; i < len(c); i++ {
		if !(c[i] >= 'a' && c[i] <= 'z' || c[i] == '_') {
			return false
		}
	}
	return true
}

// colunasDaProjecao são os valores das cinco colunas de uma projeção (a
// guardada ou a esperada); todos nulos para a ausente.
func colunasDaProjecao(p *dictionary.ParDoPainel) []any {
	if p == nil {
		return []any{nil, nil, nil, nil, nil}
	}
	return []any{p.Variante, p.Termo, umOuZero(p.Restrito), dominioNulo(p.Dominio), umOuZero(p.EmAliasRestrito)}
}

// porMarcadorDoAprendido põe o marcador learned do id, se não houver: campos
// 0, nunca inicial, sem texto.
func porMarcadorDoAprendido(ctx context.Context, tx *sql.Tx, id string, agora time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_outbox (entity_type, entity_id, campos, criado_em, inicial) VALUES (?, ?, 0, ?, 0)
		 ON CONFLICT (entity_type, entity_id) DO NOTHING`,
		EntidadeAprendido, id, emTexto(agora)); err != nil {
		return fmt.Errorf("storage: pondo o aprendido na fila do sync: %w", err)
	}
	return nil
}

func umOuZero(b bool) int {
	if b {
		return 1
	}
	return 0
}

// dominioNulo: o domínio vazio é o nulo (sem domínio).
func dominioNulo(d string) any {
	if d == "" {
		return nil
	}
	return d
}

// dominioDoFio é o domínio como ConferirPar o recebe: "" é nil.
func dominioDoFio(d string) *string {
	if d == "" {
		return nil
	}
	return &d
}

func textoOuNulo(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ─── O envio ───

// PendentesDoAprendido devolve até limite marcadores learned com o estado de
// cada id, os em voo (e os a repetir) primeiro e depois na ordem em que
// entraram. Antes do aceite do "Enviar o histórico" (D3), o marcador cuja
// operação seria upsert (o par presente na projeção guardada) e que não está
// em voo fica na fila, e não sai: só o delete sobe antes do clique. Um marcador sem linha no
// espelho (não acontece: só por um banco mexido à mão) sai da fila aqui
// mesmo.
func (f *Fila) PendentesDoAprendido(ctx context.Context, limite int) ([]EstadoDoAprendido, error) {
	if limite <= 0 {
		return nil, nil
	}
	var itens []EstadoDoAprendido
	err := naTransacao(ctx, f.db, func(tx *sql.Tx) error {
		itens = nil
		_, aceito, err := lerChave(ctx, tx, ChaveNuvemEnvioAceitoEm)
		if err != nil {
			return err
		}
		ms, err := lerMarcadores(ctx, tx,
			`WHERE entity_type = ? ORDER BY mutation_id IS NULL AND versao_enviada IS NULL, criado_em, entity_id`,
			EntidadeAprendido)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if len(itens) == limite {
				break
			}
			e, err := f.estadoDoAprendido(ctx, tx, m.EntityID)
			if err != nil {
				return err
			}
			if !e.TemLinha {
				if _, err := tx.ExecContext(ctx, `DELETE FROM sync_outbox WHERE entity_type = ? AND entity_id = ?`,
					EntidadeAprendido, m.EntityID); err != nil {
					return fmt.Errorf("storage: tirando da fila um aprendido sem espelho: %w", err)
				}
				continue
			}
			if !aceito && e.Presente() && !m.Congelado() {
				continue
			}
			itens = append(itens, e)
		}
		return nil
	})
	return itens, err
}

// ─── O recomeço e o esquecer ───

// recomecarAprendido é a parte do aprendido do recomeço (recomecar: o epoch
// novo, e a primeira rodada de um pareamento sem estado), na transação dele.
// Na ordem:
//
//  1. o "desfeito aqui" que o esquecer guardou (soltarDesfeitosDeOutraConta);
//  2. todo par do espelho é marcado de novo: o presente na projeção guardada
//     vira upsert (com a base 0: a versão some no passo 3), mas só depois do
//     aceite (D3: antes dele, a varredura o conta, e o clique o solta), e só
//     o que não está no "veio da nuvem"; o ausente que a nuvem tinha desfeito
//     (base desfeita), ou que saiu daqui sem a nuvem confirmar (DesfeitoAqui)
//     e que ela tinha (com uma versão, da base ou do par guardado), vira
//     delete (sem a linha lá, a nuvem responde nada_a_desfazer, e o marcador
//     sai). Ficam de fora a linha "aplicando" (a varredura a reconcilia pelo
//     arquivo antes) e o par do "veio da nuvem": ele pode ser de uma conta
//     esquecida (o "veio da nuvem" sobrevive ao esquecer), e está com a
//     adaptação deste PC; quem o sobe de novo é o PC de origem, no epoch novo
//     dele (o celular não aprende). Assim a adaptação deste PC nunca volta
//     como upsert, e o par de uma conta esquecida nunca sobe para a conta
//     nova. Os marcadores que já existem ficam;
//  3. a versão da base e a do par guardado somem (no epoch novo, as versões
//     guardadas não valem nada), e o "recusado" é liberado: tenta uma vez de
//     novo.
//
// Na primeira rodada depois de um esquecer, não sobra versão nem aceite: o
// passo 2 não marca nada.
func (f *Fila) recomecarAprendido(ctx context.Context, tx *sql.Tx, agora time.Time) error {
	if err := f.soltarDesfeitosDeOutraConta(ctx, tx); err != nil {
		return err
	}
	_, aceito, err := lerChave(ctx, tx, ChaveNuvemEnvioAceitoEm)
	if err != nil {
		return err
	}
	ids, err := f.idsDoAprendido(ctx, tx)
	if err != nil {
		return err
	}
	var marcar []string
	for _, id := range ids {
		e, err := f.estadoDoAprendido(ctx, tx, id)
		if err != nil {
			return err
		}
		if !e.TemLinha || e.Situacao == EspelhoAplicando {
			continue
		}
		tinhaVersao := e.ServerVersion != nil || (e.Nuvem != nil && e.Nuvem.Versao != nil)
		switch {
		case e.Presente():
			if aceito && !e.VeioDaNuvem {
				marcar = append(marcar, id)
			}
		case e.BaseDesfeita, e.DesfeitoAqui() && tinhaVersao:
			marcar = append(marcar, id)
		}
	}
	sort.Strings(marcar)
	for _, id := range marcar {
		if err := porMarcadorDoAprendido(ctx, tx, id, agora); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET server_version = NULL, nuvem_versao = NULL
		 WHERE user_id = ? AND (server_version IS NOT NULL OR nuvem_versao IS NOT NULL)`, f.dono); err != nil {
		return fmt.Errorf("storage: esquecendo as versões do aprendido: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET situacao = NULL, motivo = NULL, situacao_em = NULL WHERE user_id = ? AND situacao = ?`,
		f.dono, EspelhoRecusado); err != nil {
		return fmt.Errorf("storage: liberando o aprendido recusado: %w", err)
	}
	return nil
}

// soltarDesfeitosDeOutraConta resolve a mudança daqui que o esquecer guardou
// com a conta (conta_esquecida), na primeira rodada do pareamento seguinte:
//
//   - o "desfeito aqui" (fora da projeção guardada): de volta à mesma conta,
//     ele continua mudado aqui (a varredura põe o delete quando o pull trouxer
//     a versão); numa conta nova, ou sem saber a conta, ele é solto (a versão
//     local passa a ser a coberta, e nada sobe): lá, ele tiraria uma troca que
//     a conta nova aprendeu por conta própria. A marca sai;
//   - a linha com a projeção presente e mudada aqui (o Desfazer que a
//     varredura ainda não tinha visto, ou a mudança que não subiu): de volta à
//     mesma conta, a marca fica, para a varredura não assentar o par quando o
//     vir sair do arquivo (EstadoDoAprendido.MesmaContaEsquecida); numa conta
//     nova, a marca sai, e a mudança continua mudada aqui (se o par saiu, a
//     varredura o assenta: a conta nova não tem a versão dele);
//   - o par aprendido aqui que a conta esquecida confirmou (presente, sem
//     mudança daqui, fora do "veio da nuvem"): numa conta nova, ele passa a
//     ser mudança daqui (a versão local sobe). A D3 manda a conta nova
//     receber as trocas aprendidas neste PC: a varredura o segura para o
//     clique (o M), o pull da conta nova o pula até lá (sem isso, a mesma
//     variante desfeita ou com outro termo lá o tiraria daqui em silêncio), e
//     depois do clique ele sobe com a base 0, e a nuvem decide (D4, com a
//     etiqueta no PC que perder). De volta à mesma conta, nada muda: o pull do
//     cursor 0 o reconcilia, e o que a conta mudou enquanto o PC estava
//     esquecido vale aqui, como no ditado. A marca sai.
func (f *Fila) soltarDesfeitosDeOutraConta(ctx context.Context, tx *sql.Tx) error {
	conta, _, err := lerChave(ctx, tx, ChaveNuvemConta)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET versao_base = versao_local
		 WHERE user_id = ? AND conta_esquecida IS NOT NULL AND (? = '' OR conta_esquecida <> ?) AND variante IS NULL`,
		f.dono, conta, conta); err != nil {
		return fmt.Errorf("storage: soltando o desfeito de outra conta: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET versao_local = versao_local + 1
		 WHERE user_id = ? AND conta_esquecida IS NOT NULL AND (? = '' OR conta_esquecida <> ?)
		   AND variante IS NOT NULL AND versao_local <= versao_base
		   AND entity_id NOT IN (SELECT entity_id FROM sync_veio_da_nuvem WHERE user_id = ?)`,
		f.dono, conta, conta, f.dono); err != nil {
		return fmt.Errorf("storage: passando o aprendido aqui à conta nova: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET conta_esquecida = NULL
		 WHERE user_id = ? AND conta_esquecida IS NOT NULL
		   AND (? = '' OR conta_esquecida <> ? OR variante IS NULL OR versao_local <= versao_base)`,
		f.dono, conta, conta); err != nil {
		return fmt.Errorf("storage: soltando o desfeito de outra conta: %w", err)
	}
	return nil
}

// esquecerAprendido é a parte do aprendido do "Esquecer esta nuvem", na
// transação dele, ANTES de a conta sair da instalação (a fila inteira sai
// depois, pelo esquecer):
//
//   - o "desfeito aqui" (DesfeitoAqui) fica, só com o id e as versões
//     locais, e guarda a conta a que pertence (conta_esquecida): ele sobe só
//     de volta à mesma conta (soltarDesfeitosDeOutraConta);
//   - a linha com a projeção presente e mudada aqui, que a nuvem tinha (uma
//     versão, da base ou do par guardado), também guarda a conta: pode ser o
//     Desfazer que a varredura ainda não viu (o PC desconectado, ou o par com
//     um envio em voo, cuja projeção não muda), e o esquecer não lê o
//     arquivo. De volta à mesma conta, a varredura não o assenta quando vê o
//     par sair (EstadoDoAprendido.MesmaContaEsquecida). A projeção dela fica
//     até a varredura seguinte (o esquecer não sabe se o par ainda está no
//     arquivo);
//   - o par aprendido aqui que a nuvem confirmou (presente, sem mudança
//     daqui, fora do "veio da nuvem" e fora da linha "aplicando") também
//     guarda a conta: numa conta nova, ele passa a ser mudança daqui e sobe
//     com o clique (soltarDesfeitosDeOutraConta);
//   - a base sai em toda linha (a versão da nuvem e o desfeito dela),
//     inclusive na "aplicando": a versão de uma conta esquecida nunca é
//     tomada pelo eco de outra;
//   - o texto que veio da conta esquecida e não está no arquivo sai: o par da
//     nuvem guardado (o não aplicado, o pendente, o arquivo indisponível, o
//     das etiquetas e o da intenção), com a versão dele, e o par perdido das
//     etiquetas (a 0008);
//   - a situação e o motivo saem, MENOS na linha "aplicando", que fica com a
//     projeção esperada até a varredura seguinte a reconciliar pelo arquivo
//     (o id dela já está no "veio da nuvem", e continua lá). O "recusado" é
//     liberado;
//   - ficam a projeção guardada (o que o arquivo tem), as versões locais e o
//     "veio da nuvem" inteiro.
func (f *Fila) esquecerAprendido(ctx context.Context, tx *sql.Tx) error {
	conta, temConta, err := lerChave(ctx, tx, ChaveNuvemConta)
	if err != nil {
		return err
	}
	if temConta && conta != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sync_aprendido SET conta_esquecida = ?
			 WHERE user_id = ? AND conta_esquecida IS NULL AND (
			   (versao_local > versao_base
			     AND (variante IS NULL OR server_version IS NOT NULL OR nuvem_versao IS NOT NULL))
			   OR (variante IS NOT NULL AND versao_local <= versao_base
			     AND (situacao IS NULL OR situacao <> ?)
			     AND entity_id NOT IN (SELECT entity_id FROM sync_veio_da_nuvem WHERE user_id = ?)))`,
			conta, f.dono, EspelhoAplicando, f.dono); err != nil {
			return fmt.Errorf("storage: guardando o desfeito aqui: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET server_version = NULL, base_desfeita = 0,
		   nuvem_versao = NULL, nuvem_variante = NULL, nuvem_termo = NULL, nuvem_restrito = NULL,
		   nuvem_dominio = NULL, nuvem_desfeito = NULL
		 WHERE user_id = ?`, f.dono); err != nil {
		return fmt.Errorf("storage: esquecendo a base do aprendido: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET situacao = NULL, motivo = NULL, situacao_em = NULL,
		   esperada_variante = NULL, esperada_termo = NULL, esperada_restrito = NULL, esperada_dominio = NULL,
		   esperada_em_alias_restrito = NULL
		 WHERE user_id = ? AND (situacao IS NULL OR situacao <> ?)`, f.dono, EspelhoAplicando); err != nil {
		return fmt.Errorf("storage: esquecendo a situação do aprendido: %w", err)
	}
	// O par perdido é o texto das etiquetas da conta esquecida: sai em toda
	// linha, inclusive na "aplicando" (a etiqueta que ela devolveria é da
	// conta esquecida).
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_aprendido SET perdido_variante = NULL, perdido_termo = NULL
		 WHERE user_id = ? AND perdido_variante IS NOT NULL`, f.dono); err != nil {
		return fmt.Errorf("storage: esquecendo o par perdido: %w", err)
	}
	return nil
}
