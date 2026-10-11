package server

import (
	"errors"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O login Google (ADR-019, "Login" e "Sessão"): o fluxo de código no
// servidor, com state guardado no servidor, nonce, PKCE (S256) e o cookie
// __Host-pre de cinco minutos ligando o navegador que começou ao que termina.

// Motivos de um login recusado: a lista fechada do log (login_recusado). O
// log nunca diz o e-mail.
const (
	MotivoForaDaLista        = "fora_da_lista"
	MotivoSubDiferente       = "sub_diferente"
	MotivoEmailNaoVerificado = "email_nao_verificado"
	MotivoEmailEmUso         = "email_em_uso"
	MotivoContaDesativada    = "conta_desativada"
	MotivoStateInvalido      = "state_invalido"
	MotivoCancelado          = "cancelado"
	MotivoTokenInvalido      = "token_invalido"
	MotivoGoogleFalhou       = "google_falhou"
	MotivoFalhaInterna       = "falha_interna"
)

// maxCorpoAuth: o corpo do POST /auth/start (só a volta) e do login de teste.
const maxCorpoAuth = 1 << 10

type authStartRequest struct {
	Volta string `json:"volta"`
}

// handleAuthStart é o POST /auth/start, chamado pelo app por fetch (o CSRF já
// passou: é um POST). Grava o login pendente e o cookie __Host-pre, e devolve
// a URL do Google.
func (c *Cloud) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	if !c.permitir(w, r, c.limLogin, c.chaveDoIP(r), 1) || !c.permitir(w, r, c.limInicio, "total", 1) {
		return
	}
	body, ok := decodeJSONLimit[authStartRequest](w, r, maxCorpoAuth)
	if !ok {
		return
	}
	volta, ok := cloudauth.ValidarVolta(body.Volta)
	if !ok {
		writeError(w, http.StatusBadRequest, codeValidation, "volta inválida: tem de ser um caminho desta origem")
		return
	}
	state, pre, verificador, nonce := cloudauth.NovoSegredo(), cloudauth.NovoSegredo(), cloudauth.NovoSegredo(), cloudauth.NovoSegredo()
	now := c.now()
	err := c.cfg.Accounts.CriarLoginPendente(r.Context(), cloudstore.LoginPendente{
		StateHash: cloudauth.Hash(state), PreHash: cloudauth.Hash(pre),
		CodeVerifier: verificador, Nonce: nonce, ReturnTo: volta,
		CriadoEm: now, ExpiraEm: now.Add(PrazoDoLogin),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookiePre, Value: pre, Path: "/", MaxAge: int(PrazoDoLogin.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"url": c.cfg.Google.URLDeAutorizacao(state, nonce, cloudauth.DesafioS256(verificador))})
}

// handleCallback é o GET /auth/google/callback: o único GET que muda estado,
// protegido pelo state de uso único e pelo cookie __Host-pre.
func (c *Cloud) handleCallback(w http.ResponseWriter, r *http.Request) {
	// O __Host-pre sai sempre: vale uma vez, dê certo ou não.
	apagarCookie(w, cookiePre)
	if ok, wait := c.limLogin.Permitir(c.chaveDoIP(r), 1); !ok {
		negarPorLimite(w, r, wait)
		paginaFixa(w, http.StatusTooManyRequests, "falhou.html")
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	state := q.Get("state")
	if !cloudauth.PareceSegredo(state) {
		c.recusar(w, r, MotivoStateInvalido)
		return
	}
	// O __Host-pre entra na busca do login pendente (o hash dele está no
	// WHERE): sem ele, ou com o de outro login, nada é consumido, e quem só
	// tem o state de outra pessoa não queima o login em curso dela.
	pre, err := r.Cookie(cookiePre)
	if err != nil {
		c.recusar(w, r, MotivoStateInvalido)
		return
	}
	now := c.now()
	p, err := c.cfg.Accounts.ConsumirLoginPendente(ctx, cloudauth.Hash(state), cloudauth.Hash(pre.Value), now)
	if err != nil {
		if errors.Is(err, cloudstore.ErrLoginPendente) {
			c.recusar(w, r, MotivoStateInvalido)
		} else {
			c.recusar(w, r, MotivoFalhaInterna)
		}
		return
	}
	if q.Has("error") {
		c.recusar(w, r, MotivoCancelado)
		return
	}
	code := q.Get("code")
	if code == "" || len(code) > 2048 {
		c.recusar(w, r, MotivoGoogleFalhou)
		return
	}
	idToken, err := c.cfg.Google.TrocarCodigo(ctx, code, p.CodeVerifier)
	if err != nil {
		c.recusar(w, r, MotivoGoogleFalhou)
		return
	}
	id, err := cloudauth.ValidarIDToken(idToken, c.cfg.Google.ClientID, p.Nonce, now)
	switch {
	case errors.Is(err, cloudauth.ErrEmailNaoVerificado):
		c.recusar(w, r, MotivoEmailNaoVerificado)
		return
	case err != nil:
		c.recusar(w, r, MotivoTokenInvalido)
		return
	}
	// A lista vem antes de qualquer escrita: conta fora dela não cria nada.
	if !c.cfg.Contas.Permite(id.Email) {
		c.recusar(w, r, MotivoForaDaLista)
		return
	}
	u, err := c.cfg.Accounts.EntrarComGoogle(ctx, cloudstore.Identidade{Sub: id.Sub, Email: id.Email, Nome: id.Nome}, now)
	if err != nil {
		c.recusar(w, r, motivoDeEntrar(err))
		return
	}
	if err := c.abrirSessao(w, r, u.ID); err != nil {
		c.recusar(w, r, MotivoFalhaInterna)
		return
	}
	http.Redirect(w, r, p.ReturnTo, http.StatusSeeOther)
}

func motivoDeEntrar(err error) string {
	switch {
	case errors.Is(err, cloudstore.ErrSubDiferente):
		return MotivoSubDiferente
	case errors.Is(err, cloudstore.ErrEmailEmUso):
		return MotivoEmailEmUso
	case errors.Is(err, cloudstore.ErrContaDesativada):
		return MotivoContaDesativada
	}
	return MotivoFalhaInterna
}

// abrirSessao cria a sessão e grava o cookie __Host-sessao. A sessão que o
// navegador trouxer é apagada: o id muda a cada login.
func (c *Cloud) abrirSessao(w http.ResponseWriter, r *http.Request, userID string) error {
	tok := cloudauth.NovoSegredo()
	var substitui []byte
	if old, err := r.Cookie(cookieSessao); err == nil && cloudauth.PareceSegredo(old.Value) {
		substitui = cloudauth.HMAC(c.cfg.SessionSecret, old.Value)
	}
	now := c.now()
	_, err := c.cfg.Accounts.CriarSessao(r.Context(), cloudstore.NovaSessao{
		UserID: userID, TokenHMAC: cloudauth.HMAC(c.cfg.SessionSecret, tok),
		Rotulo: cloudauth.Rotulo(r.UserAgent()), Agora: now,
		ExpiraEm: c.politica.ExpiracaoDaSessao(now, now), Substitui: substitui,
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieSessao, Value: tok, Path: "/", MaxAge: int(SessaoTeto.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// recusar responde a um login que não deu com uma página fixa, sem ecoar nada
// do pedido, e registra o motivo (nunca o e-mail).
func (c *Cloud) recusar(w http.ResponseWriter, r *http.Request, motivo string) {
	anotarMotivo(r.Context(), motivo)
	c.log.Warn("login_recusado", "motivo", motivo)
	switch motivo {
	case MotivoForaDaLista, MotivoSubDiferente, MotivoEmailNaoVerificado, MotivoEmailEmUso, MotivoContaDesativada:
		paginaFixa(w, http.StatusForbidden, "recusado.html")
	case MotivoGoogleFalhou:
		paginaFixa(w, http.StatusBadGateway, "falhou.html")
	case MotivoFalhaInterna:
		paginaFixa(w, http.StatusInternalServerError, "falhou.html")
	default:
		paginaFixa(w, http.StatusBadRequest, "falhou.html")
	}
}

// handleLogout é o POST /auth/logout: apaga a sessão no servidor e o cookie.
// Sem sessão, só apaga o cookie.
func (c *Cloud) handleLogout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(cookieSessao); err == nil && cloudauth.PareceSegredo(ck.Value) {
		if err := c.cfg.Accounts.DerrubarSessaoPorHMAC(r.Context(), cloudauth.HMAC(c.cfg.SessionSecret, ck.Value)); err != nil {
			writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
			return
		}
	}
	apagarCookie(w, cookieSessao)
	w.WriteHeader(http.StatusNoContent)
}
