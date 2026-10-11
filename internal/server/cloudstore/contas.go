package cloudstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// Accounts é o que o servidor da nuvem usa para o login, as sessões e o
// token do aparelho (ADR-019, fatia F3). Como o Store, tem uma implementação
// em memória (MemoryStore, que implementa as duas interfaces sob o mesmo
// mutex) e uma no Postgres (internal/server/pgstore), e a mesma suíte
// (internal/server/authtest) roda contra as duas.
//
// Nada aqui guarda um segredo em claro: o state e o cookie __Host-pre chegam
// como SHA-256, o id da sessão e o token do aparelho como HMAC (a chave fica
// no servidor, nunca no banco). As exceções são o verificador do PKCE e o
// nonce do login pendente, que o servidor precisa inteiros e que vencem em
// cinco minutos.
type Accounts interface {
	// CriarLoginPendente grava o início de um login (POST /auth/start).
	CriarLoginPendente(ctx context.Context, p LoginPendente) error
	// ConsumirLoginPendente acha o login pelo hash do state E pelo hash do
	// cookie __Host-pre, e o marca como usado no MESMO comando: uso único,
	// sem corrida. O pre faz parte da busca, e não de uma conferência
	// depois: quem tem só o state de outra pessoa (a URL de retorno vazada,
	// por exemplo) não acha a linha e não queima o login dela. Desconhecido,
	// já usado, vencido em now ou com outro pre é ErrLoginPendente.
	ConsumirLoginPendente(ctx context.Context, stateHash, preHash []byte, now time.Time) (LoginPendente, error)

	// EntrarComGoogle acha o usuário pelo sub ou o cria, numa transação só.
	// Quem chama já conferiu a lista CONTAS_PERMITIDAS e o email_verified:
	// nada aqui escreve antes disso. Regras (ADR-019, "Login" e D7):
	//   - sub conhecido: a conta desativada é ErrContaDesativada; se o e-mail
	//     mudou no Google, o novo não pode ser de outro usuário
	//     (ErrEmailEmUso), e users.email é atualizado;
	//   - sub novo, e-mail já fixado em outro sub: ErrSubDiferente (D7);
	//   - sub novo, e-mail novo: cria o usuário (UUID v7) e a linha de
	//     user_sync_counters (sync_epoch UUID v7) na MESMA transação.
	EntrarComGoogle(ctx context.Context, id Identidade, now time.Time) (Usuario, error)

	// CriarSessao grava uma sessão nova. Com Substitui, a sessão que o
	// navegador trouxe é apagada antes: o id muda a cada login.
	CriarSessao(ctx context.Context, s NovaSessao) (Sessao, error)
	// ResolverSessao acha a sessão pelo HMAC do cookie e anota o uso no mesmo
	// comando: last_seen_at = now e o vencimento anda para now + SemUso, sem
	// passar do Teto contado da criação. Vencida, apagada, de conta
	// desativada ou desconhecida é ErrCredencial.
	ResolverSessao(ctx context.Context, tokenHMAC []byte, now time.Time, p PoliticaDeSessao) (SessaoResolvida, error)
	// ListarSessoes devolve as sessões que ainda valem em now, a mais usada
	// por último primeiro.
	ListarSessoes(ctx context.Context, userID string, now time.Time) ([]Sessao, error)
	// DerrubarSessao apaga uma sessão do usuário. A de outro usuário, ou uma
	// que não existe, é ErrSessaoNaoEncontrada (os dois são o mesmo erro, de
	// propósito).
	DerrubarSessao(ctx context.Context, userID, sessionID string) error
	// DerrubarSessaoPorHMAC apaga a sessão do cookie (o "Sair"). Não existir
	// não é erro.
	DerrubarSessaoPorHMAC(ctx context.Context, tokenHMAC []byte) error

	// ResolverTokenDeAparelho acha o aparelho pelo HMAC do token e anota o
	// uso (last_used_at). Aparelho revogado ou apagado, conta desativada,
	// token sem uso há TokenSemUso (contado do último uso, ou da criação) ou
	// token desconhecido é ErrCredencial.
	ResolverTokenDeAparelho(ctx context.Context, tokenHMAC []byte, now time.Time) (AparelhoResolvido, error)

	// O pareamento de um PC (ADR-019, F6; pareamento.go).
	//
	// CriarPedidoDePareamento grava o pedido do POST /device-auth/start, sem
	// aprovação.
	CriarPedidoDePareamento(ctx context.Context, p PedidoDePareamento) error
	// LerPedidoDePareamento devolve o pedido que ainda espera aprovação em
	// now. Desconhecido, vencido, aprovado ou usado é ErrPedido.
	LerPedidoDePareamento(ctx context.Context, id string, now time.Time) (PedidoDePareamento, error)
	// AprovarPedidoDePareamento aprova o pedido pelo usuário e grava o hash do
	// code, no MESMO comando que confere que ele ainda espera aprovação e não
	// venceu: aprovar duas vezes, ou depois do prazo, é ErrPedido.
	AprovarPedidoDePareamento(ctx context.Context, id, userID string, codeHash []byte, now time.Time) (PedidoDePareamento, error)
	// TrocarCodigoDePareamento consome o code (uso único; o desafio está na
	// busca) e, na mesma transação, grava o aparelho (o mesmo de antes, pelo
	// usuário e o install_id, volta sem revogação e com a versão mais um) e
	// troca o token dele. Qualquer coisa que não vale é ErrCodigo.
	TrocarCodigoDePareamento(ctx context.Context, t TrocaDeCodigo) (AparelhoPareado, error)
	// ListarAparelhos devolve os aparelhos do usuário que não foram
	// revogados e cujo token ainda vale em now, o usado por último primeiro.
	ListarAparelhos(ctx context.Context, userID string, now time.Time) ([]Aparelho, error)
	// RevogarAparelho marca o aparelho do usuário como revogado (revoked_at,
	// versão mais um) e apaga o token dele. A linha fica: os ditados que
	// vieram dele apontam para ela, e parear de novo a reaproveita. O de
	// outro usuário, um que não existe ou um já revogado é
	// ErrAparelhoNaoEncontrado.
	RevogarAparelho(ctx context.Context, userID, deviceID string, now time.Time) error

	// ApagarDadosDaConta é o "apagar meus dados da nuvem" (ADR-019, D2):
	// numa transação só, cujo PRIMEIRO comando trava o contador (como o
	// ponto único de gravação), troca o sync_epoch (UUID v7), sobe a marca
	// d'água das lápides ao last_seq (o seq continua, não volta a 0) e apaga
	// os ditados, as lápides, os eventos, as mutações, o aprendido, as
	// correções, o dicionário, as preferências, as três tabelas do ditado no
	// celular (ADR-021: as chaves da IA, as preferências do ditado e todos os
	// aceites, os que terminaram também), os pedidos de pareamento que o
	// usuário aprovou, os aparelhos (com os tokens) e as outras sessões.
	// Ficam o usuário, o contador e a sessão keepSessionID. Devolve o epoch
	// novo.
	ApagarDadosDaConta(ctx context.Context, userID, keepSessionID string, now time.Time) (string, error)

	// Arrumar apaga os logins pendentes vencidos, as sessões vencidas e os
	// pedidos de pareamento vencidos (a rotina de hora em hora do servidor).
	Arrumar(ctx context.Context, now time.Time) (Arrumacao, error)
}

// Identidade é quem o Google diz que entrou: o sub (a chave), o e-mail (em
// minúsculas) e o nome.
type Identidade struct {
	Sub   string
	Email string
	Nome  string
}

// Usuario é a conta na nuvem.
type Usuario struct {
	ID    string
	Email string
	Nome  string
}

// LoginPendente é a linha de pending_logins: o login entre o POST
// /auth/start e o callback.
type LoginPendente struct {
	StateHash    []byte
	PreHash      []byte
	CodeVerifier string
	Nonce        string
	ReturnTo     string
	CriadoEm     time.Time
	ExpiraEm     time.Time
}

// NovaSessao é o que CriarSessao grava.
type NovaSessao struct {
	UserID    string
	TokenHMAC []byte
	Rotulo    string
	Agora     time.Time
	ExpiraEm  time.Time
	// Substitui é o HMAC da sessão que o navegador trouxe, se trouxe.
	Substitui []byte
}

// Sessao é uma sessão do navegador, como a lista de sessões a mostra.
type Sessao struct {
	ID        string
	UserID    string
	Rotulo    string
	CriadaEm  time.Time
	UltimoUso time.Time
	ExpiraEm  time.Time
}

// SessaoResolvida é a sessão de um pedido, com o usuário dela.
type SessaoResolvida struct {
	Sessao
	Email string
	Nome  string
}

// AparelhoResolvido é o aparelho de um token, com o usuário dele.
type AparelhoResolvido struct {
	UserID     string
	Email      string
	Nome       string
	DeviceID   string
	DeviceNome string
	Plataforma string
}

// PoliticaDeSessao são os prazos da sessão: SemUso (30 dias na F3) e Teto
// desde o login (90 dias; ADR-019, "Decisões da F3").
type PoliticaDeSessao struct {
	SemUso time.Duration
	Teto   time.Duration
}

// Arrumacao conta o que Arrumar apagou.
type Arrumacao struct {
	LoginsPendentes int64
	Sessoes         int64
	Pedidos         int64
}

// Erros de Accounts. Nenhum carrega e-mail, sub nem segredo.
var (
	ErrLoginPendente       = errors.New("cloudstore: login pendente desconhecido, já usado ou vencido")
	ErrSubDiferente        = errors.New("cloudstore: o e-mail já está fixado em outro sub")
	ErrEmailEmUso          = errors.New("cloudstore: o e-mail novo da conta é de outro usuário")
	ErrContaDesativada     = errors.New("cloudstore: conta desativada")
	ErrCredencial          = errors.New("cloudstore: credencial desconhecida, vencida ou revogada")
	ErrSessaoNaoEncontrada = errors.New("cloudstore: sessão não encontrada")
)

// NewUUIDv7 gera um UUID v7 (RFC 9562), em minúsculas: o id do usuário, da
// sessão e o sync_epoch. O começo é o instante em milissegundos, então os ids
// novos ordenam pela criação.
func NewUUIDv7() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	ms := uint64(time.Now().UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = (b[6] & 0x0F) | 0x70
	b[8] = (b[8] & 0x3F) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// ExpiracaoDaSessao é o vencimento de uma sessão usada em now: now + SemUso,
// sem passar de criada + Teto.
func (p PoliticaDeSessao) ExpiracaoDaSessao(criada, now time.Time) time.Time {
	exp := now.Add(p.SemUso)
	if teto := criada.Add(p.Teto); teto.Before(exp) {
		exp = teto
	}
	return exp
}
