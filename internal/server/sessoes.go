package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As sessões abertas da conta (ADR-019, "Sessão"; 06, "Sessions"): a pessoa
// vê e derruba qualquer uma (o celular perdido). Só com a sessão do navegador.

type sessaoDTO struct {
	ID        string    `json:"id"`
	Rotulo    string    `json:"rotulo"`
	CriadaEm  time.Time `json:"criada_em"`
	UltimoUso time.Time `json:"ultimo_uso_em"`
	Atual     bool      `json:"atual"`
}

func (c *Cloud) handleListarSessoes(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	list, err := c.cfg.Accounts.ListarSessoes(r.Context(), p.UserID, c.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	items := make([]sessaoDTO, 0, len(list))
	for _, s := range list {
		items = append(items, sessaoDTO{ID: s.ID, Rotulo: s.Rotulo, CriadaEm: s.CriadaEm, UltimoUso: s.UltimoUso, Atual: s.ID == p.SessionID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleDerrubarSessao: a sessão de outro usuário, ou uma que não existe, é
// 404 (o mesmo erro, de propósito). Derrubar a atual também apaga o cookie.
func (c *Cloud) handleDerrubarSessao(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	err := c.cfg.Accounts.DerrubarSessao(r.Context(), p.UserID, r.PathValue("id"))
	switch {
	case errors.Is(err, cloudstore.ErrSessaoNaoEncontrada):
		writeError(w, http.StatusNotFound, codeNotFound, "sessão não encontrada")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	if id, ok := cloudstore.CanonicalUUID(r.PathValue("id")); ok && id == p.SessionID {
		apagarCookie(w, cookieSessao)
	}
	w.WriteHeader(http.StatusNoContent)
}

// O GET /me da nuvem: com a sessão ou com o token do PC (que vê também o
// aparelho dele).

type aparelhoDTO struct {
	ID         string `json:"id"`
	Nome       string `json:"nome"`
	Plataforma string `json:"plataforma"`
}

type meNuvemDTO struct {
	ID          string       `json:"id"`
	Email       string       `json:"email"`
	DisplayName string       `json:"display_name,omitempty"`
	Aparelho    *aparelhoDTO `json:"aparelho,omitempty"`
}

func (c *Cloud) handleMe(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	out := meNuvemDTO{ID: p.UserID, Email: p.Email, DisplayName: p.DisplayName}
	if p.Kind == KindAparelho {
		out.Aparelho = &aparelhoDTO{ID: p.DeviceID, Nome: p.DeviceName, Plataforma: p.DevicePlatform}
	}
	writeJSON(w, http.StatusOK, out)
}

// O "apagar meus dados da nuvem" (ADR-019, D2; 06, DELETE /me/data): só com a
// sessão (a tabela de rotas) e com a confirmação no corpo. Sem a
// confirmação, 400 e nada muda.

type apagarDadosRequest struct {
	Confirmar string `json:"confirmar"`
}

// ConfirmacaoDeApagar é o texto que o corpo tem de trazer.
const ConfirmacaoDeApagar = "apagar"

func (c *Cloud) handleApagarDados(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	body, ok := decodeJSONLimit[apagarDadosRequest](w, r, maxCorpoAuth)
	if !ok {
		return
	}
	if body.Confirmar != ConfirmacaoDeApagar {
		writeError(w, http.StatusBadRequest, codeValidation, `confirmação obrigatória: {"confirmar":"apagar"}`)
		return
	}
	if _, err := c.cfg.Accounts.ApagarDadosDaConta(r.Context(), p.UserID, p.SessionID, c.now()); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	// O que a rota de ditar guarda da conta na memória sai junto (ADR-021,
	// C5b): o Smart montado com o aprendido dela e o texto pago e não gravado.
	c.esquecerDitadoDaConta(p.UserID)
	w.WriteHeader(http.StatusNoContent)
}
