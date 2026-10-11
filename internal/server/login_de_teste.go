//go:build logindeteste

package server

import (
	"net/http"
	"net/netip"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O login de teste (ADR-019, "Segurança e privacidade"): entrar sem o Google,
// para desenvolvimento. SÓ EXISTE com a build tag logindeteste, nunca por
// variável de ambiente: uma configuração errada viraria entrada sem senha em
// produção. Com a tag, NewCloud e o serve recusam PUBLIC_BASE_URL fora de
// localhost, o serve recusa HTTP_ADDR fora do loopback, a rota responde 404 a
// quem não pede da própria máquina, e o serve avisa no log. O build de
// produção (F11) não passa a tag.

// LoginDeTesteCompilado diz se este binário tem o login de teste.
const LoginDeTesteCompilado = true

type loginDeTesteRequest struct {
	Email string `json:"email"`
}

// rotasDeTeste monta o login de teste:
//   - GET /auth/teste: o que a tela Entrar do app pergunta para saber se
//     oferece o botão "Entrar com a conta de teste" (ADR-019, F4). Responde
//     {"conta": o primeiro e-mail de CONTAS_PERMITIDAS, "google": se o login
//     do Google também existe}. Sem a tag, é 404, e a tela não mostra nada;
//   - POST /auth/teste: com {"email"} de CONTAS_PERMITIDAS, abre a mesma
//     sessão do login de verdade.
//
// O sub é "teste:" + e-mail, e o D7 vale nos dois sentidos: o login de teste
// não toma um e-mail já fixado no Google, e o primeiro login de teste com um
// e-mail o fixa no sub "teste:", e daí o login Google desse e-mail, no mesmo
// banco, é recusado (sub_diferente). Por isso, o login de teste usa um e-mail
// da lista que não é o do Google de quem testa, ou outro banco.
func (c *Cloud) rotasDeTeste() []rota {
	return []rota{
		{"GET /auth/teste", AcessoPublico, c.handleInfoDoLoginDeTeste},
		{"POST /auth/teste", AcessoPublico, c.handleLoginDeTeste},
	}
}

type infoDoLoginDeTeste struct {
	Conta  string `json:"conta"`
	Google bool   `json:"google"`
}

func (c *Cloud) handleInfoDoLoginDeTeste(w http.ResponseWriter, r *http.Request) {
	// A mesma guarda do POST: de fora da máquina, a rota não existe.
	if !pedidoDaPropriaMaquina(r) {
		handleNaoMontada(w, r)
		return
	}
	writeJSON(w, http.StatusOK, infoDoLoginDeTeste{Conta: c.cfg.Contas.Primeira(), Google: c.cfg.Google != nil})
}

// pedidoDaPropriaMaquina: o RemoteAddr é de loopback e não há cabeçalho de
// proxy (um proxy local repassaria pedidos da rede).
func pedidoDaPropriaMaquina(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" {
		return false
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	return err == nil && ap.Addr().Unmap().IsLoopback()
}

func (c *Cloud) handleLoginDeTeste(w http.ResponseWriter, r *http.Request) {
	// A segunda guarda, além do HTTP_ADDR do serve: quem não pede da própria
	// máquina vê a rota como não montada.
	if !pedidoDaPropriaMaquina(r) {
		handleNaoMontada(w, r)
		return
	}
	if !c.permitir(w, r, c.limLogin, c.chaveDoIP(r), 1) {
		return
	}
	body, ok := decodeJSONLimit[loginDeTesteRequest](w, r, maxCorpoAuth)
	if !ok {
		return
	}
	email := cloudauth.NormalizarEmail(body.Email)
	if !c.cfg.Contas.Permite(email) {
		anotarMotivo(r.Context(), MotivoForaDaLista)
		writeError(w, http.StatusForbidden, codeForbidden, "conta fora da lista")
		return
	}
	u, err := c.cfg.Accounts.EntrarComGoogle(r.Context(), cloudstore.Identidade{Sub: "teste:" + email, Email: email, Nome: "Conta de teste"}, c.now())
	if err != nil {
		motivo := motivoDeEntrar(err)
		anotarMotivo(r.Context(), motivo)
		writeError(w, http.StatusForbidden, codeForbidden, "login de teste recusado: "+motivo)
		return
	}
	if err := c.abrirSessao(w, r, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
