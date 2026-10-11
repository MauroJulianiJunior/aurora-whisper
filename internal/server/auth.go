package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
)

// Principal é o usuário autenticado da sessão corrente — o ÚNICO lugar
// de onde um handler deste pacote pode obter um user_id
// (docs/ai-spec/09-security-privacy.md, "Cloud auth"; AC-035; NFR-012;
// THR-004: IDOR).
//
// Nenhum tipo de corpo de requisição (request DTO) deste pacote declara
// um campo UserID. Isso não é um lembrete de disciplina — é estrutural:
// um handler não tem de onde ler um user_id vindo do cliente, porque o
// tipo que ele decodifica do corpo simplesmente não tem esse campo. O
// único caminho para UserID é sempre principalFromContext, alimentado
// pelo middleware requireSession a partir do token/cookie de sessão.
type Principal struct {
	// UserID identifica o usuário dono dos dados. Deriva sempre da
	// sessão (cookie web ou device token), nunca do payload.
	UserID string
	// Email e DisplayName espelham o perfil resolvido na sessão, usados
	// por GET /me.
	Email       string
	DisplayName string
	// DeviceID identifica o device autenticado quando a sessão vem de
	// um device token (Bearer). Vazio para sessão web (cookie), que não
	// está associada a um device específico.
	DeviceID string

	// Kind é o tipo da credencial no servidor da nuvem (ADR-019, F3): a
	// sessão do navegador ou o token de um aparelho. Cada rota da tabela
	// (rotas.go) declara quais aceita. No servidor antigo (Server, sem a
	// nuvem) fica KindNenhum: ali as rotas não distinguem credencial.
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
	// KindNenhum: o servidor antigo, que não distingue credencial.
	KindNenhum PrincipalKind = iota
	// KindSessao: o cookie __Host-sessao do navegador (o celular, o painel
	// da nuvem).
	KindSessao
	// KindAparelho: o Bearer de um PC pareado.
	KindAparelho
)

// SessionStore resolve um token opaco (Bearer de device, ou o valor do
// cookie de sessão web) para o Principal autenticado.
//
// A implementação real (Google OAuth para o cookie web, challenge/
// exchange para o device token) é trabalho de uma slice futura
// (docs/ai-spec/13-implementation-plan.md, TASK-013). Nesta fase
// (F1-D, validação de contratos com mocks), InMemorySessionStore valida
// o contrato: qualquer produtor de sessão real só precisa implementar
// esta interface para plugar no resto do pacote sem mudança de rota ou
// de handler.
type SessionStore interface {
	// Resolve devolve o Principal associado a token, e ok=false se o
	// token for vazio, desconhecido ou revogado.
	Resolve(ctx context.Context, token string) (Principal, bool)
}

// InMemorySessionStore é o SessionStore usado para validar o contrato
// nesta fase. É seguro para uso concorrente.
type InMemorySessionStore struct {
	mu      sync.RWMutex
	byToken map[string]Principal
}

// NewInMemorySessionStore constrói um SessionStore vazio.
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{byToken: make(map[string]Principal)}
}

var _ SessionStore = (*InMemorySessionStore)(nil)

// Put registra (ou substitui) a sessão para token. Existe para
// bootstrap/testes: é assim que um teste "loga" dois usuários distintos
// e obtém dois tokens que os isolam um do outro.
func (s *InMemorySessionStore) Put(token string, p Principal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byToken[token] = p
}

// Revoke remove a sessão de token, se existir.
func (s *InMemorySessionStore) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byToken, token)
}

func (s *InMemorySessionStore) Resolve(_ context.Context, token string) (Principal, bool) {
	if token == "" {
		return Principal{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byToken[token]
	return p, ok
}

// sessionCookieName é o cookie de sessão web (Google OAuth), conforme
// docs/ai-spec/06-api-contract.md, "Auth" → "Web": HttpOnly/Secure/
// SameSite. A emissão do cookie (login) é trabalho da TASK-013; aqui
// só lemos o valor para resolver a sessão.
const sessionCookieName = "aurora_session"

// bearerOrCookieToken extrai o token de sessão de uma requisição:
// "Authorization: Bearer <token>" (device) tem prioridade sobre o
// cookie de sessão web, e ambos são aceitos pelas mesmas rotas — o
// contrato não distingue rotas por tipo de credencial.
func bearerOrCookieToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		}
		return ""
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value
	}
	return ""
}

type principalCtxKey struct{}

// requireSession é o middleware que autentica a requisição e injeta o
// Principal resolvido no contexto. Toda rota protegida deste pacote
// passa por aqui antes do handler — é o único ponto de entrada de
// user_id no resto do código.
func requireSession(store SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerOrCookieToken(r)
			if token == "" {
				writeError(w, http.StatusUnauthorized, codeUnauthorized, "autenticação obrigatória")
				return
			}
			principal, ok := store.Resolve(r.Context(), token)
			if !ok {
				writeError(w, http.StatusUnauthorized, codeUnauthorized, "sessão inválida ou expirada")
				return
			}
			ctx := context.WithValue(r.Context(), principalCtxKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// principalFromContext devolve o Principal injetado por requireSession.
// ok é sempre true dentro de uma rota protegida; handlers deste pacote
// tratam ok=false como bug de montagem de rota (rota protegida faltando
// o middleware), nunca como caminho de negócio esperado.
func principalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(Principal)
	return p, ok
}
