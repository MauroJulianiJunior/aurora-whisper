package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// A tabela de rotas da nuvem (ADR-019, F3): uma linha por rota, com quem pode
// chamá-la. O roteador é montado a partir dela, e os testes a percorrem (com
// cada credencial, contra cada rota), então uma rota nova sem a coluna "quem
// pode" não compila, e uma rota fora da tabela não existe.

// Acesso é quem pode chamar uma rota.
type Acesso int

const (
	// AcessoPublico: sem credencial (health, /auth/*, o app). A rota nem
	// olha o Authorization nem o cookie.
	AcessoPublico Acesso = iota
	// AcessoSessao: só a sessão do navegador (o CRUD, a busca, o excluir de
	// vez, as sessões e o "apagar meus dados").
	AcessoSessao
	// AcessoAparelho: só o token de um PC (o sync).
	AcessoAparelho
	// AcessoQualquer: a sessão ou o token (o GET /me e o DELETE
	// /devices/{id}, que com o token só alcança o próprio aparelho).
	AcessoQualquer
	// AcessoDitar: quem pode ditar (o POST /dictations; ADR-021, decisão A).
	// No passo 1, só a sessão do navegador; o token de um PC é 403. O passo 2
	// (o app nativo) acrescenta aqui o token de um aparelho Android com o
	// escopo de ditar. NUNCA vira AcessoQualquer, que admitiria o token de um
	// PC.
	AcessoDitar
)

func (a Acesso) String() string {
	switch a {
	case AcessoPublico:
		return "publico"
	case AcessoSessao:
		return "sessao"
	case AcessoAparelho:
		return "aparelho"
	case AcessoQualquer:
		return "qualquer"
	case AcessoDitar:
		return "ditar"
	}
	return "?"
}

type rota struct {
	padrao string
	acesso Acesso
	h      http.HandlerFunc
}

// Rota é uma linha da tabela, para os testes.
type Rota struct {
	Padrao string
	Acesso Acesso
}

// Rotas devolve a tabela, na ordem.
func (c *Cloud) Rotas() []Rota {
	out := make([]Rota, len(c.rotas))
	for i, rt := range c.rotas {
		out[i] = Rota{Padrao: rt.padrao, Acesso: rt.acesso}
	}
	return out
}

// tabela é a lista de rotas. O que NÃO está aqui não é montado e cai num dos
// três padrões do fim, que respondem 404 a qualquer método (POST
// /transcriptions, /asr/transcribe, /resolve, /updates, /settings,
// /dictionary, /corrections e o PATCH /devices, o renomear, que fica para
// depois). Com eles, nenhum caminho responde 405: um método que a rota não
// tem também é 404.
func (c *Cloud) tabela() []rota {
	st := c.cfg.Store
	t := []rota{
		{"GET /api/v1/health/live", AcessoPublico, handleHealthLive},
		{"GET /api/v1/health/ready", AcessoPublico, handleHealthReady(c.cfg.ReadyCheckers...)},
		// A saúde do job de retenção (F11c, saude.go): só de quem pede pela
		// própria máquina (o healthcheck -retencao, dentro do contêiner); de
		// qualquer outro, ou com um cabeçalho de proxy, é o 404 de rota não
		// montada.
		{"GET /api/v1/health/retencao", AcessoPublico, c.handleSaudeDaRetencao},

		{"GET /api/v1/me", AcessoQualquer, c.handleMe},
		{"DELETE /api/v1/me/data", AcessoSessao, c.handleApagarDados},
		{"GET /api/v1/sessions", AcessoSessao, c.handleListarSessoes},
		{"DELETE /api/v1/sessions/{id}", AcessoSessao, c.handleDerrubarSessao},

		{"GET /api/v1/transcriptions", AcessoSessao, handleStoreList(st)},
		{"POST /api/v1/transcriptions/search", AcessoSessao, handleStoreSearch(st)},
		{"GET /api/v1/transcriptions/{id}", AcessoSessao, handleStoreGet(st)},
		{"PATCH /api/v1/transcriptions/{id}", AcessoSessao, handleStoreEdit(st, c.now)},
		{"DELETE /api/v1/transcriptions/{id}", AcessoSessao, handleStoreTrash(st, c.now)},
		{"POST /api/v1/transcriptions/{id}/restore", AcessoSessao, handleStoreRestore(st, c.now)},
		{"POST /api/v1/transcriptions/{id}/purge", AcessoSessao, handleStorePurge(st, c.now)},

		// O aprendido no celular (F8d, learned_routes.go): ver e desfazer, só
		// com a sessão. O PC desfaz pelo sync; o celular não aprende.
		{"GET /api/v1/learned", AcessoSessao, handleLearnedList(st)},
		{"POST /api/v1/learned/undo", AcessoSessao, handleLearnedUndo(st, c.now)},

		// As notas e os marcadores de nota no celular (ADR-022, N7 e N8;
		// notas_routes.go e notas_celular_routes.go): ler e escrever, só com a
		// sessão, como o CRUD. O PC lê e escreve pelo sync; cada escrita daqui
		// grava o evento que o pull leva a ele.
		{"GET /api/v1/notes", AcessoSessao, handleNotesList(st)},
		{"POST /api/v1/notes", AcessoSessao, handleNoteCreate(st, c.now)},
		{"GET /api/v1/notes/{id}", AcessoSessao, handleNoteGet(st)},
		{"PATCH /api/v1/notes/{id}", AcessoSessao, handleNotePatch(st, c.now)},
		{"DELETE /api/v1/notes/{id}", AcessoSessao, handleNoteOp(st, c.now, cloudstore.NotaLixeira)},
		{"POST /api/v1/notes/{id}/restore", AcessoSessao, handleNoteOp(st, c.now, cloudstore.NotaRestaurar)},
		{"POST /api/v1/notes/{id}/move", AcessoSessao, handleNoteMove(st, c.now)},
		{"POST /api/v1/notes/{id}/purge", AcessoSessao, handleNoteOp(st, c.now, cloudstore.NotaApagarDeVez)},
		{"PUT /api/v1/notes/{id}/labels/{label}", AcessoSessao, handleNoteOp(st, c.now, cloudstore.NotaMarcar)},
		{"DELETE /api/v1/notes/{id}/labels/{label}", AcessoSessao, handleNoteOp(st, c.now, cloudstore.NotaDesmarcar)},
		// As imagens das notas no celular (N10b; imagens_routes.go): anexar só
		// com rede, ler sem cache, tirar. O "with-image" é a nota nova só com a
		// imagem (o ícone de imagem do "Criar uma nota…").
		{"POST /api/v1/notes/with-image", AcessoSessao, c.handleNoteWithImage},
		{"POST /api/v1/notes/{id}/images", AcessoSessao, c.handleNoteImageAttach},
		{"GET /api/v1/notes/{id}/images/{imagem}", AcessoSessao, c.handleImageGet},
		{"DELETE /api/v1/notes/{id}/images/{imagem}", AcessoSessao, c.handleImageDelete},
		{"GET /api/v1/labels", AcessoSessao, handleLabelsList(st)},
		{"POST /api/v1/labels", AcessoSessao, handleLabelCreate(st, c.now)},
		{"PATCH /api/v1/labels/{id}", AcessoSessao, handleLabelRename(st, c.now)},
		{"DELETE /api/v1/labels/{id}", AcessoSessao, handleLabelDelete(st, c.now)},
		// O Desfazer do "Ditar para nota" (N8): o ditado volta ao histórico, e
		// a nota ao que era. Existe mesmo sem o ditado pelo celular montado
		// (o Desfazer não fala com provedor nenhum).
		{"POST /api/v1/transcriptions/{id}/undo-note", AcessoSessao, handleDictationUndoNote(st, c.now)},

		{"POST /api/v1/sync/push", AcessoAparelho, c.handlePush},
		{"GET /api/v1/sync/pull", AcessoAparelho, handleStorePull(st)},
		// As imagens das notas no sync do PC (N10b; imagens_routes.go): os
		// bytes nunca vão no push nem no pull. O PC sobe o que anexou (PUT,
		// idempotente), baixa o que veio de outro lugar (GET) e tira (DELETE).
		{"PUT /api/v1/sync/notes/{id}/images/{imagem}", AcessoAparelho, c.handleSyncImagePut},
		{"GET /api/v1/sync/notes/{id}/images/{imagem}", AcessoAparelho, c.handleImageGet},
		{"DELETE /api/v1/sync/notes/{id}/images/{imagem}", AcessoAparelho, c.handleImageDelete},

		// O pareamento de um PC (F6, pareamento.go). O start e a troca são
		// públicos (o PC ainda não tem token), com limite por IP; ler e
		// aprovar o pedido, só a sessão de quem está logado.
		{"POST /api/v1/device-auth/start", AcessoPublico, c.handleInicioDePareamento},
		{"GET /api/v1/device-auth/requests/{id}", AcessoSessao, c.handleLerPedido},
		{"POST /api/v1/device-auth/requests/{id}/approve", AcessoSessao, c.handleAprovarPedido},
		{"POST /api/v1/device-auth/exchange", AcessoPublico, c.handleTrocarCodigo},
		// Os aparelhos da conta: a lista só com a sessão; revogar, com a
		// sessão (qualquer aparelho da conta) ou com o token (só o próprio).
		{"GET /api/v1/devices", AcessoSessao, c.handleListarAparelhos},
		{"DELETE /api/v1/devices/{id}", AcessoQualquer, c.handleRevogarAparelho},

		{"POST /auth/logout", AcessoPublico, c.handleLogout},

		// O estilo das duas páginas fixas do login que não deu (pagina/).
		{"GET /pagina.css", AcessoPublico, servirPagina("pagina.css")},
	}
	// As chaves da IA do ditado no celular (ADR-021, C3a; chaves_ia.go): só
	// com o arquivo da chave de cifra. Sem ele, caem no 404 do /api/v1/.
	t = append(t, c.rotasDoDitado()...)
	// O login do Google. Sem o client (só o servidor de teste do PC, com a
	// build tag logindeteste), as duas rotas caem no 404 do /auth/.
	if c.cfg.Google != nil {
		t = append(t,
			rota{"POST /auth/start", AcessoPublico, c.handleAuthStart},
			rota{"GET " + callbackPath, AcessoPublico, c.handleCallback},
		)
	}
	// O app: o build da nuvem do painel (painel.go).
	t = append(t, c.painel.rotas()...)
	t = append(t, c.rotasDeTeste()...)
	return append(t,
		rota{"/api/v1/", AcessoPublico, handleNaoMontada},
		rota{"/auth/", AcessoPublico, handleNaoMontada},
		rota{"/", AcessoPublico, handleNaoMontada},
	)
}

func handleNaoMontada(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, codeNotFound, "rota não encontrada")
}

// proteger é o portão de cada rota: anota o padrão para o log e, fora das
// públicas, autentica e confere o tipo da credencial. É o único lugar de onde
// um handler da nuvem recebe o Principal (e o user_id).
func (c *Cloud) proteger(rt rota) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if info := acessoDe(r.Context()); info != nil {
			info.rota = r.Pattern
		}
		if rt.acesso == AcessoPublico {
			rt.h(w, r)
			return
		}
		p, ok := c.autenticar(w, r)
		if !ok {
			return
		}
		switch {
		case rt.acesso == AcessoSessao && p.Kind != KindSessao:
			writeError(w, http.StatusForbidden, codeForbidden, "esta rota exige a sessão do navegador")
			return
		case rt.acesso == AcessoDitar && !podeDitar(p):
			writeError(w, http.StatusForbidden, codeForbidden, "esta rota exige a sessão do navegador")
			return
		case rt.acesso == AcessoAparelho && p.Kind != KindAparelho:
			writeError(w, http.StatusForbidden, codeForbidden, "esta rota exige o token de um aparelho")
			return
		}
		rt.h(w, r.WithContext(context.WithValue(r.Context(), principalCtxKey{}, p)))
	})
}

// podeDitar é o AcessoDitar: no passo 1 da ADR-021, só a sessão do navegador.
// O token de um aparelho, que hoje é sempre um PC, não dita: ele tem o push e o
// pull do histórico inteiro, e não é ele quem grava a voz.
func podeDitar(p Principal) bool { return p.Kind == KindSessao }

// Nomes dos cookies (ADR-019, D6). O prefixo __Host- obriga Secure, Path=/ e
// nenhum Domain: o cookie é só desta origem.
const (
	cookieSessao = "__Host-sessao"
	cookiePre    = "__Host-pre"
)

// autenticar resolve a credencial do pedido (ADR-019, "Proteção do
// navegador" e "Token do dispositivo"):
//   - com Authorization, só vale "Bearer <token>" de um aparelho; qualquer
//     outra coisa, ou um token desconhecido ou revogado, é 401, SEM olhar o
//     cookie;
//   - sem Authorization, vale o cookie __Host-sessao;
//   - nos dois casos, o e-mail do usuário ainda tem de estar em
//     CONTAS_PERMITIDAS.
//
// Escreve a resposta de erro quando não autentica.
func (c *Cloud) autenticar(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	ctx := r.Context()
	if values, has := r.Header["Authorization"]; has {
		tok, ok := bearer(values)
		if !ok {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "credencial inválida")
			return Principal{}, false
		}
		a, err := c.cfg.Accounts.ResolverTokenDeAparelho(ctx, cloudauth.HMAC(c.cfg.DeviceTokenPepper, tok), c.now())
		if err != nil {
			c.falhaDeCredencial(w, err, false)
			return Principal{}, false
		}
		if !c.cfg.Contas.Permite(a.Email) {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "credencial inválida")
			return Principal{}, false
		}
		return Principal{
			UserID: a.UserID, Email: a.Email, DisplayName: a.Nome, Kind: KindAparelho,
			DeviceID: a.DeviceID, DeviceName: a.DeviceNome, DevicePlatform: a.Plataforma,
		}, true
	}
	ck, err := r.Cookie(cookieSessao)
	if err != nil {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "autenticação obrigatória")
		return Principal{}, false
	}
	if !cloudauth.PareceSegredo(ck.Value) {
		c.falhaDeCredencial(w, cloudstore.ErrCredencial, true)
		return Principal{}, false
	}
	s, err := c.cfg.Accounts.ResolverSessao(ctx, cloudauth.HMAC(c.cfg.SessionSecret, ck.Value), c.now(), c.politica)
	if err != nil {
		c.falhaDeCredencial(w, err, true)
		return Principal{}, false
	}
	if !c.cfg.Contas.Permite(s.Email) {
		c.falhaDeCredencial(w, cloudstore.ErrCredencial, true)
		return Principal{}, false
	}
	return Principal{UserID: s.UserID, Email: s.Email, DisplayName: s.Nome, Kind: KindSessao, SessionID: s.ID}, true
}

// bearer lê "Bearer <token>" (o esquema sem distinção de maiúsculas, RFC
// 9110), com um cabeçalho só e o token no formato de um segredo.
func bearer(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	v := values[0]
	const prefix = "bearer "
	if len(v) <= len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(v[len(prefix):])
	return tok, cloudauth.PareceSegredo(tok)
}

// falhaDeCredencial responde 401 à credencial que não vale (e, no cookie,
// apaga o cookie velho) e 500 a uma falha do banco.
func (c *Cloud) falhaDeCredencial(w http.ResponseWriter, err error, cookie bool) {
	if !errors.Is(err, cloudstore.ErrCredencial) {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	if cookie {
		apagarCookie(w, cookieSessao)
	}
	writeError(w, http.StatusUnauthorized, codeUnauthorized, "sessão inválida ou expirada")
}

// apagarCookie manda o navegador esquecer um cookie __Host- (os mesmos
// atributos, vencido).
func apagarCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}
