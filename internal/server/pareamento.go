package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// O pareamento de um PC (ADR-019, "Conectar o PC"; fatia F6): retorno a
// 127.0.0.1 com PKCE (RFC 8252, §7.3), sem consulta repetida.
//
//  1. POST /api/v1/device-auth/start (público, com limite): o Agent manda o
//     desafio do PKCE, o nome do PC e a porta do painel local; a nuvem grava
//     o pedido, sem aprovação, por PrazoDoPedido.
//  2. GET /device/authorize?pedido=… (o app, painel.go): a página, logada,
//     lê o pedido por GET /api/v1/device-auth/requests/{id} e mostra o nome do
//     PC e o código de conferência.
//  3. POST /api/v1/device-auth/requests/{id}/approve (sessão e CSRF): grava o
//     hash de um code novo e devolve o endereço do retorno ao PC
//     (http://127.0.0.1:<porta>/nuvem/retorno?code=…&state=<pedido>), que o
//     app abre com location.replace. Muda do 06 (formulário e 303): o
//     Chromium aplica o form-action 'self' da CSP ao redirecionamento de um
//     formulário, e assim a CSP fica como está e o erro aparece na tela.
//  4. POST /api/v1/device-auth/exchange (público, com limite): o Agent troca o
//     code com o verificador e recebe o token do aparelho.
//
// O log é só o padrão da rota (acesso_log.go): nunca o code, o state, o
// pedido, o verificador, a conferência nem o token.

// maxCorpoPareamento: o corpo do start e da troca.
const maxCorpoPareamento = 1 << 10

// codeCodigoInvalido: a troca que não vale, qualquer que seja o motivo (o
// code desconhecido, usado, vencido, não aprovado, com outro verificador).
const codeCodigoInvalido = "CODIGO_INVALIDO"

// PlataformasDoPareamento: só o PC Windows pareia na v1.
const plataformaWindows = "windows"

// portaDeRetornoMinima é o começo da faixa dinâmica do Windows (49152 a
// 65535), de onde o painel do PC tira a porta: o retorno do pareamento só vai
// para ela.
const portaDeRetornoMinima = 49152

// Os limites do texto que o Agent manda (os mesmos CHECK da 0013).
const (
	maxNomeDoPC      = 80
	maxVersaoDoAgent = 64
)

type inicioDePareamentoRequest struct {
	InstallID     string `json:"install_id"`
	DeviceName    string `json:"device_name"`
	Platform      string `json:"platform"`
	AppVersion    string `json:"app_version"`
	CodeChallenge string `json:"code_challenge"`
	RedirectPort  int    `json:"redirect_port"`
}

type inicioDePareamentoResponse struct {
	Pedido      string `json:"pedido"`
	AuthURL     string `json:"auth_url"`
	Conferencia string `json:"conferencia"`
	ExpiresIn   int    `json:"expires_in"`
}

type pedidoDTO struct {
	Nome        string    `json:"nome"`
	Plataforma  string    `json:"plataforma"`
	Versao      string    `json:"versao"`
	Conferencia string    `json:"conferencia"`
	ExpiraEm    time.Time `json:"expira_em"`
}

type trocaRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
}

type contaDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type trocaResponse struct {
	DeviceID string   `json:"device_id"`
	Token    string   `json:"token"`
	Conta    contaDTO `json:"conta"`
}

type aparelhoListaDTO struct {
	ID          string    `json:"id"`
	Nome        string    `json:"nome"`
	Plataforma  string    `json:"plataforma"`
	Versao      string    `json:"versao"`
	ConectadoEm time.Time `json:"conectado_em"`
	UltimoUso   time.Time `json:"ultimo_uso_em"`
}

// decodeEstrito lê um objeto JSON só, sem campo desconhecido nem nada
// depois dele, até limit bytes. A recusa é uma frase fixa: o corpo é de quem
// pede (sem sessão), e o 400 não ecoa nada dele.
func decodeEstrito[T any](w http.ResponseWriter, r *http.Request, limit int64) (T, bool) {
	var v T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil || dec.Decode(new(json.RawMessage)) != io.EOF {
		writeError(w, http.StatusBadRequest, codeValidation, "corpo inválido")
		var zero T
		return zero, false
	}
	return v, true
}

// textoDoPC: de 1 a max caracteres, UTF-8 válido, sem caractere de controle
// e não só de espaços.
func textoDoPC(s string, max int) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// versaoDoAgent: até maxVersaoDoAgent caracteres ASCII visíveis (vazia
// vale: um Agent sem versão).
func versaoDoAgent(s string) bool {
	if len(s) > maxVersaoDoAgent {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// validarInicio devolve o pedido pronto para gravar, ou a frase da recusa.
func validarInicio(b inicioDePareamentoRequest) (cloudstore.PedidoDePareamento, string) {
	installID, ok := cloudstore.CanonicalUUID(b.InstallID)
	switch {
	case !ok:
		return cloudstore.PedidoDePareamento{}, "install_id inválido"
	case !textoDoPC(b.DeviceName, maxNomeDoPC):
		return cloudstore.PedidoDePareamento{}, "device_name inválido"
	case b.Platform != plataformaWindows:
		return cloudstore.PedidoDePareamento{}, "platform inválida"
	case !versaoDoAgent(b.AppVersion):
		return cloudstore.PedidoDePareamento{}, "app_version inválida"
	case !cloudauth.PareceSegredo(b.CodeChallenge):
		return cloudstore.PedidoDePareamento{}, "code_challenge inválido (S256, base64url de 43 caracteres)"
	case b.RedirectPort < portaDeRetornoMinima || b.RedirectPort > 65535:
		// Só a faixa dinâmica do Windows (49152 a 65535), de onde vem a porta
		// do painel do PC (guard.Listen(0)). Fora dela ficam as portas de
		// serviços conhecidos, que poderiam repassar o code de um
		// pareamento aprovado por engano (decisão do Mauro, 06-out-2026).
		return cloudstore.PedidoDePareamento{}, "redirect_port inválida"
	}
	return cloudstore.PedidoDePareamento{
		CodeChallenge: b.CodeChallenge, InstallID: installID, Nome: b.DeviceName,
		Plataforma: b.Platform, Versao: b.AppVersion, Porta: b.RedirectPort,
	}, ""
}

// base é a origem pública (PUBLIC_BASE_URL normalizada).
func (c *Cloud) base() string { return c.scheme() + "://" + c.host }

// handleInicioDePareamento é o POST /api/v1/device-auth/start: público, com
// o limite por IP (o mesmo balde da troca) e o total.
func (c *Cloud) handleInicioDePareamento(w http.ResponseWriter, r *http.Request) {
	if !c.permitir(w, r, c.limPareamento, c.chaveDoIP(r), 1) || !c.permitir(w, r, c.limPareamentoTotal, "total", 1) {
		return
	}
	body, ok := decodeEstrito[inicioDePareamentoRequest](w, r, maxCorpoPareamento)
	if !ok {
		return
	}
	p, recusa := validarInicio(body)
	if recusa != "" {
		writeError(w, http.StatusBadRequest, codeValidation, recusa)
		return
	}
	now := c.now()
	p.ID = cloudstore.NewUUIDv7()
	p.Conferencia = cloudauth.NovaConferencia()
	p.CriadoEm, p.ExpiraEm = now, now.Add(cloudstore.PrazoDoPedido)
	if err := c.cfg.Accounts.CriarPedidoDePareamento(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	writeJSON(w, http.StatusOK, inicioDePareamentoResponse{
		Pedido:      p.ID,
		AuthURL:     c.base() + CaminhoDeAprovar + "?" + url.Values{"pedido": {p.ID}}.Encode(),
		Conferencia: p.Conferencia,
		ExpiresIn:   int(cloudstore.PrazoDoPedido.Seconds()),
	})
}

// handleLerPedido é o GET /api/v1/device-auth/requests/{id} (só sessão): o
// que a página de aprovação mostra. Vencido, usado, aprovado ou desconhecido
// é o mesmo 404.
func (c *Cloud) handleLerPedido(w http.ResponseWriter, r *http.Request) {
	p, err := c.cfg.Accounts.LerPedidoDePareamento(r.Context(), r.PathValue("id"), c.now())
	if c.falhaDoPedido(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, pedidoDTO{
		Nome: p.Nome, Plataforma: p.Plataforma, Versao: p.Versao, Conferencia: p.Conferencia, ExpiraEm: p.ExpiraEm,
	})
}

// handleAprovarPedido é o POST /api/v1/device-auth/requests/{id}/approve (só
// sessão; o CSRF já passou): grava o hash de um code novo e devolve o retorno
// ao PC. Aprovar duas vezes, ou depois do prazo, é 404.
func (c *Cloud) handleAprovarPedido(w http.ResponseWriter, r *http.Request) {
	pr, _ := principalFromContext(r.Context())
	code := cloudauth.NovoSegredo()
	p, err := c.cfg.Accounts.AprovarPedidoDePareamento(r.Context(), r.PathValue("id"), pr.UserID, cloudauth.Hash(code), c.now())
	if c.falhaDoPedido(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"retorno": EnderecoDoRetorno(p.Porta, code, p.ID)})
}

// EnderecoDoRetorno é para onde o navegador leva o code: o painel local do
// Agent, só em 127.0.0.1 (o Agent escuta só lá, guard.Listen), na porta que
// ele disse no start. O state é o id do pedido.
func EnderecoDoRetorno(porta int, code, pedido string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/nuvem/retorno?%s", porta, url.Values{"code": {code}, "state": {pedido}}.Encode())
}

func (c *Cloud) falhaDoPedido(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, cloudstore.ErrPedido):
		writeError(w, http.StatusNotFound, codeNotFound, "pedido de pareamento vencido, já usado ou desconhecido")
	default:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
	}
	return true
}

// handleTrocarCodigo é o POST /api/v1/device-auth/exchange: público, com o
// limite por IP. Tudo o que não vale é o mesmo 400 CODIGO_INVALIDO.
func (c *Cloud) handleTrocarCodigo(w http.ResponseWriter, r *http.Request) {
	if !c.permitir(w, r, c.limPareamento, c.chaveDoIP(r), 1) {
		return
	}
	body, ok := decodeEstrito[trocaRequest](w, r, maxCorpoPareamento)
	if !ok {
		return
	}
	if !cloudauth.PareceSegredo(body.Code) || !cloudauth.PareceSegredo(body.CodeVerifier) {
		codigoInvalido(w)
		return
	}
	token := cloudauth.NovoSegredo()
	a, err := c.cfg.Accounts.TrocarCodigoDePareamento(r.Context(), cloudstore.TrocaDeCodigo{
		CodeHash: cloudauth.Hash(body.Code), Desafio: cloudauth.DesafioS256(body.CodeVerifier),
		TokenHMAC: cloudauth.HMAC(c.cfg.DeviceTokenPepper, token), Agora: c.now(), Permite: c.cfg.Contas.Permite,
	})
	switch {
	case errors.Is(err, cloudstore.ErrCodigo):
		codigoInvalido(w)
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	writeJSON(w, http.StatusOK, trocaResponse{DeviceID: a.DeviceID, Token: token, Conta: contaDTO{ID: a.UserID, Email: a.Email}})
}

func codigoInvalido(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, codeCodigoInvalido, "código de pareamento inválido ou vencido")
}

// handleListarAparelhos é o GET /api/v1/devices (só sessão).
func (c *Cloud) handleListarAparelhos(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	list, err := c.cfg.Accounts.ListarAparelhos(r.Context(), p.UserID, c.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	items := make([]aparelhoListaDTO, 0, len(list))
	for _, a := range list {
		items = append(items, aparelhoListaDTO{
			ID: a.ID, Nome: a.Nome, Plataforma: a.Plataforma, Versao: a.Versao, ConectadoEm: a.ConectadoEm, UltimoUso: a.UltimoUso,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleRevogarAparelho é o DELETE /api/v1/devices/{id}: com a sessão, um
// aparelho da conta; com o token, só o próprio (o Desconectar do PC); outro,
// 403. O de outro usuário, um que não existe ou um já revogado: 404.
func (c *Cloud) handleRevogarAparelho(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	id := r.PathValue("id")
	if p.Kind == KindAparelho {
		if canon, ok := cloudstore.CanonicalUUID(id); !ok || canon != p.DeviceID {
			writeError(w, http.StatusForbidden, codeForbidden, "o token de um aparelho só desconecta o próprio aparelho")
			return
		}
	}
	err := c.cfg.Accounts.RevogarAparelho(r.Context(), p.UserID, id, c.now())
	switch {
	case errors.Is(err, cloudstore.ErrAparelhoNaoEncontrado):
		writeError(w, http.StatusNotFound, codeNotFound, "aparelho não encontrado")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
