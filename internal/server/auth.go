package server

import "context"

// Principal é o usuário autenticado da requisição corrente — o ÚNICO lugar
// de onde um handler deste pacote pode obter um user_id
// (docs/ai-spec/09-security-privacy.md, "Cloud auth"; AC-035; NFR-012;
// THR-004: IDOR).
//
// Nenhum tipo de corpo de requisição (request DTO) deste pacote declara
// um campo UserID. Isso não é um lembrete de disciplina — é estrutural:
// um handler não tem de onde ler um user_id vindo do cliente, porque o
// tipo que ele decodifica do corpo simplesmente não tem esse campo. O
// único caminho para UserID é sempre principalFromContext, alimentado
// pelo portão de cada rota da tabela (Cloud.proteger, rotas.go) a partir
// da credencial autenticada.
type Principal struct {
	// UserID identifica o usuário dono dos dados. Deriva sempre da
	// sessão (cookie web) ou do token do aparelho, nunca do payload.
	UserID string
	// Email e DisplayName espelham o perfil resolvido na sessão, usados
	// por GET /me.
	Email       string
	DisplayName string
	// DeviceID identifica o aparelho autenticado quando a credencial é o
	// token de um PC (Bearer). Vazio para a sessão do navegador (cookie),
	// que não está associada a um aparelho específico.
	DeviceID string

	// Kind é o tipo da credencial (ADR-019, F3): a sessão do navegador ou o
	// token de um aparelho. Cada rota da tabela (rotas.go) declara quais
	// aceita.
	Kind PrincipalKind
	// SessionID é o id da sessão do navegador (web_sessions.id), só com
	// KindSessao: a lista de sessões marca a atual, e o "apagar meus dados"
	// a mantém.
	SessionID string
	// DeviceName e DevicePlatform são do aparelho, só com KindAparelho (o
	// GET /me os devolve).
	DeviceName     string
	DevicePlatform string
}

// PrincipalKind é o tipo da credencial de um Principal.
type PrincipalKind int

const (
	// KindNenhum é o valor zero: nenhuma credencial resolvida.
	KindNenhum PrincipalKind = iota
	// KindSessao: o cookie __Host-sessao do navegador (o celular, o painel
	// da nuvem).
	KindSessao
	// KindAparelho: o Bearer de um PC pareado.
	KindAparelho
)

type principalCtxKey struct{}

// principalFromContext devolve o Principal que o portão da rota
// (Cloud.proteger) pôs no contexto. ok é sempre true dentro de uma rota
// protegida; handlers deste pacote tratam ok=false como bug de montagem de
// rota (rota protegida sem o portão), nunca como caminho de negócio
// esperado.
func principalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(Principal)
	return p, ok
}
