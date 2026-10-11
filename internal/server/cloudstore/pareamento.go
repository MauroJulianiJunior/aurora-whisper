package cloudstore

import (
	"errors"
	"time"
)

// O pareamento de um PC e os aparelhos da conta (ADR-019, "Conectar o PC" e
// "Token do dispositivo"; fatia F6). O PC pede um pareamento com o desafio do
// PKCE e a porta do painel local; a pessoa aprova, logada, na página da nuvem;
// o navegador leva o code ao 127.0.0.1 do PC; o PC troca o code com o
// verificador e recebe o token do aparelho. Os métodos estão em Accounts, nas
// duas implementações (memória e Postgres), e a suíte internal/server/authtest
// roda contra as duas.
//
// Nada aqui guarda um segredo em claro: o code chega como SHA-256 (vive minutos
// e é de uso único) e o token como HMAC com DEVICE_TOKEN_PEPPER. O desafio do
// PKCE não é segredo (é o hash do verificador).

// PrazoDoPedido: o pedido de pareamento vale 10 minutos, aprovado ou não.
const PrazoDoPedido = 10 * time.Minute

// TokenSemUso: o token de um aparelho que ninguém usa há 180 dias deixa de
// valer (ADR-019, D7; decisão 2 da F6, a recomendação como padrão). Conta do
// último uso, ou da criação para quem nunca usou. O PC aparece desconectado
// (401), e é só conectar de novo.
const TokenSemUso = 180 * 24 * time.Hour

// PedidoDePareamento é a linha de pairing_requests, como o start a grava e a
// página de aprovação a lê.
type PedidoDePareamento struct {
	// ID é o pedido: o state do retorno ao PC.
	ID string
	// CodeChallenge é o desafio do PKCE (S256, base64url de 43 caracteres).
	CodeChallenge string
	InstallID     string
	Nome          string
	Plataforma    string
	Versao        string
	// Porta é a do painel local do Agent, em 127.0.0.1.
	Porta int
	// Conferencia é o código curto que a página e o painel mostram. Não
	// protege nada sozinho.
	Conferencia string
	CriadoEm    time.Time
	ExpiraEm    time.Time
}

// TrocaDeCodigo é o que o POST /device-auth/exchange pede ao armazenamento.
type TrocaDeCodigo struct {
	// CodeHash é o SHA-256 do code que o PC recebeu no retorno.
	CodeHash []byte
	// Desafio é o S256 do verificador que o PC mandou, calculado por quem
	// chama. Ele entra na busca, junto com o code: um verificador errado não
	// acha a linha e NÃO queima o code (o link copiado não conecta, e o PC
	// que começou ainda troca).
	Desafio string
	// TokenHMAC é o HMAC do token novo do aparelho.
	TokenHMAC []byte
	Agora     time.Time
	// Permite é a lista CONTAS_PERMITIDAS, conferida dentro da troca, antes de
	// gravar o aparelho: a conta que saiu da lista entre a aprovação e a troca
	// não ganha aparelho. Nil é recusar tudo.
	Permite func(email string) bool
}

// AparelhoPareado é o que a troca devolve: o aparelho (novo ou o mesmo de
// antes, pelo install_id) e a conta dele.
type AparelhoPareado struct {
	DeviceID string
	UserID   string
	Email    string
}

// Aparelho é uma linha da lista de aparelhos da conta (GET /devices).
type Aparelho struct {
	ID         string
	Nome       string
	Plataforma string
	Versao     string
	// ConectadoEm é quando o token atual nasceu (o último pareamento).
	ConectadoEm time.Time
	// UltimoUso é o último uso do token, ou ConectadoEm se nunca foi usado.
	UltimoUso time.Time
}

// Erros do pareamento. Nenhum carrega o code, o token nem o pedido.
var (
	// ErrPedido: o pedido não existe, venceu, já foi aprovado ou usado (a
	// página e a aprovação respondem o mesmo 404 para todos).
	ErrPedido = errors.New("cloudstore: pedido de pareamento desconhecido, vencido, aprovado ou usado")
	// ErrCodigo: o code não vale (desconhecido, não aprovado, usado, vencido,
	// com outro verificador, ou de uma conta que não pode mais entrar).
	ErrCodigo = errors.New("cloudstore: código de pareamento inválido")
	// ErrAparelhoNaoEncontrado: o aparelho não existe, é de outro usuário ou
	// já foi revogado (o mesmo erro, de propósito).
	ErrAparelhoNaoEncontrado = errors.New("cloudstore: aparelho não encontrado")
)

// limiteDoTokenSemUso é o instante antes do qual (inclusive) um último uso
// faz o token vencer.
func limiteDoTokenSemUso(now time.Time) time.Time { return now.Add(-TokenSemUso) }

// TokenVencido diz se um token usado por último em ultimoUso (ou criado
// então, se nunca usado) já venceu em now.
func TokenVencido(ultimoUso, now time.Time) bool {
	return !ultimoUso.After(limiteDoTokenSemUso(now))
}
