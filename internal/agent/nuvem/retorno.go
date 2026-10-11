package nuvem

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }

// O retorno ao PC (GET /nuvem/retorno?code=…&state=…), montado no servidor do
// painel local atrás do guard.RequireHost (internal/agent/painel): o navegador
// chega aqui pelo location.replace da página de aprovação da nuvem, sem o
// token da Local API. É um GET que muda estado por desenho (o retorno a
// loopback da RFC 8252), e o que o protege é o state do pedido em curso, de
// uso único e com prazo, e o PKCE na troca.

type trocaRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
}

type trocaResponse struct {
	DeviceID string `json:"device_id"`
	Token    string `json:"token"`
	Conta    struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"conta"`
}

// Retorno é o handler do GET /nuvem/retorno.
func (s *Servico) Retorno() http.Handler {
	return http.HandlerFunc(s.servirRetorno)
}

// O motivo de um retorno recusado, para o log (uma lista fechada; nunca o
// valor do code, do state ou do pedido).
const (
	motivoSemPedido = "sem_pedido"
	motivoVencido   = "vencido"
	motivoState     = "state"
	motivoCode      = "code"
)

func (s *Servico) servirRetorno(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		paginaDoRetorno(w, http.StatusMethodNotAllowed, false)
		return
	}
	q := r.URL.Query()
	state, code := q.Get("state"), q.Get("code")

	s.mu.Lock()
	p := s.pedido
	motivo := ""
	switch {
	case p == nil && s.concluido != "" && subtle.ConstantTimeCompare([]byte(state), []byte(s.concluido)) == 1:
		// O mesmo retorno de um pareamento que já deu certo (a página
		// recarregada): a mesma página, sem trocar nada de novo.
		s.mu.Unlock()
		paginaDoRetorno(w, http.StatusOK, true)
		return
	case p == nil:
		motivo = motivoSemPedido
	case !s.cfg.Agora().Before(p.prazo):
		// Vencido: o pedido sai, e o painel diz "O pedido venceu".
		s.pedido = nil
		s.aviso = AvisoVencido
		motivo = motivoVencido
	case subtle.ConstantTimeCompare([]byte(state), []byte(p.id)) != 1:
		// Um state errado NÃO consome o pedido: quem manda lixo ao
		// 127.0.0.1 não derruba o pareamento de quem o começou.
		motivo = motivoState
	case !pareceSegredo(code):
		motivo = motivoCode
	default:
		// O certo consome ANTES da rede: uso único, mesmo que dois retornos
		// cheguem juntos.
		s.pedido = nil
	}
	s.mu.Unlock()
	if motivo != "" {
		s.log.Warn("nuvem: retorno recusado", "motivo", motivo)
		paginaDoRetorno(w, http.StatusBadRequest, false)
		return
	}

	// A troca não depende da página ficar aberta: o contexto do pedido do
	// navegador não a cancela no meio (o token já pode ter nascido na nuvem).
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), PrazoDaRede)
	defer cancel()
	status, aviso := s.trocar(ctx, p, code)
	s.mu.Lock()
	s.aviso = aviso
	s.conferidoEm = time.Time{}
	aoConectar := s.aoConectar
	if aviso == "" {
		s.concluido = p.id
	}
	s.mu.Unlock()
	if aviso == "" && aoConectar != nil {
		// O laço de sync acorda já (F7b), em vez de esperar o relógio.
		aoConectar()
	}
	paginaDoRetorno(w, status, aviso == "")
}

// trocar troca o code pelo token, confere a D3 e guarda o token e a conta.
// Devolve o status da página e o aviso ("" é conectado).
func (s *Servico) trocar(ctx context.Context, p *pedido, code string) (int, string) {
	var out trocaResponse
	status, err := s.pedir(ctx, http.MethodPost, p.base+"/api/v1/device-auth/exchange", "",
		trocaRequest{Code: code, CodeVerifier: p.verificador}, &out)
	if err != nil || status != http.StatusOK || !pareceUUID(out.DeviceID) || !pareceSegredo(out.Token) ||
		!pareceUUID(out.Conta.ID) || !emailValido(out.Conta.Email) {
		s.log.Warn("nuvem: a troca do código não deu certo", "status", status, "erro_de_rede", err != nil)
		return http.StatusBadGateway, AvisoFalhou
	}
	conta := strings.ToLower(out.Conta.ID)
	aparelho := strings.ToLower(out.DeviceID)

	// A D3: a instalação já ligada a outra conta (ou a outra nuvem) não troca
	// de conta. O aparelho que acabou de nascer é revogado com o token novo,
	// que não é guardado.
	contaAntes, temConta, err := s.cfg.Instalacao.Ler(ctx, chaveConta)
	if err != nil {
		s.desfazer(ctx, p.base, aparelho, out.Token, "instalacao")
		return http.StatusInternalServerError, AvisoFalhou
	}
	urlAntes, temURL, err := s.cfg.Instalacao.Ler(ctx, chaveURL)
	if err != nil {
		s.desfazer(ctx, p.base, aparelho, out.Token, "instalacao")
		return http.StatusInternalServerError, AvisoFalhou
	}
	if (temConta && contaAntes != conta) || (temURL && urlAntes != p.base) {
		s.desfazer(ctx, p.base, aparelho, out.Token, "outra_conta")
		return http.StatusConflict, AvisoOutraConta
	}

	if err := s.cfg.Cofre.Gravar(s.cfg.NomeNoCofre, out.Token); err != nil {
		s.log.Warn("nuvem: não consegui guardar o token no Gerenciador de Credenciais", "err", err)
		s.desfazer(ctx, p.base, aparelho, out.Token, "cofre")
		return http.StatusInternalServerError, AvisoFalhou
	}
	if err := s.cfg.Instalacao.Gravar(ctx, map[string]string{
		chaveURL: p.base, chaveConta: conta, chaveEmail: out.Conta.Email, chaveAparelho: aparelho,
		chaveNome: p.nome, chaveConectadoEm: s.cfg.Agora().UTC().Format(time.RFC3339),
	}); err != nil {
		s.log.Warn("nuvem: não consegui gravar a conexão na instalação", "err", err)
		if err := s.cfg.Cofre.Apagar(s.cfg.NomeNoCofre); err != nil {
			s.log.Warn("nuvem: não consegui apagar o token guardado", "err", err)
		}
		s.desfazer(ctx, p.base, aparelho, out.Token, "instalacao")
		return http.StatusInternalServerError, AvisoFalhou
	}
	s.log.Info("nuvem: PC conectado")
	return http.StatusOK, ""
}

// desfazer revoga na nuvem o aparelho que acabou de nascer (com o token dele,
// que não fica em lugar nenhum).
func (s *Servico) desfazer(ctx context.Context, base, aparelho, token, motivo string) {
	revogou := s.revogar(ctx, base, aparelho, token)
	s.log.Warn("nuvem: o pareamento foi desfeito", "motivo", motivo, "revogado_na_nuvem", revogou)
}

// emailValido: o e-mail da conta, para mostrar no painel. Sem controle nem
// espaço, com @, até 254.
func emailValido(e string) bool {
	if e == "" || len(e) > 254 || !strings.Contains(e, "@") {
		return false
	}
	for i := 0; i < len(e); i++ {
		if e[i] <= 0x20 || e[i] == 0x7f {
			return false
		}
	}
	return true
}

// paginaDoRetorno é a página fixa que o navegador vê no 127.0.0.1: "PC
// conectado" ou "Não deu para conectar". Não ecoa nada do pedido, não carrega
// nada (a CSP própria: default-src 'none', só o estilo embutido) e não fica
// em cache.
func paginaDoRetorno(w http.ResponseWriter, status int, deuCerto bool) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", CSPDoRetorno)
	w.WriteHeader(status)
	if deuCerto {
		_, _ = io.WriteString(w, paginaConectado)
		return
	}
	_, _ = io.WriteString(w, paginaFalhou)
}

// CSPDoRetorno: a página do retorno só tem estilo embutido.
const CSPDoRetorno = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

const estiloDoRetorno = `<style>
:root{color-scheme:light dark;--fundo:#f5f7f5;--texto:#14201b;--fraco:#4d5c55}
@media (prefers-color-scheme:dark){:root{--fundo:#070a09;--texto:#e4ece8;--fraco:#9aaba3}}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--fundo);color:var(--texto);
font-family:system-ui,-apple-system,"Segoe UI",sans-serif;padding:0 16px}
main{max-width:28rem;text-align:center}
h1{font-size:1.25rem;font-weight:600;margin:0 0 .5rem}
p{margin:0;color:var(--fraco);line-height:1.5}
</style>`

const paginaConectado = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>PC conectado · Aurora Whisper</title>` + estiloDoRetorno + `</head>
<body><main><h1>PC conectado</h1><p>Pode fechar esta aba e voltar ao painel.</p></main></body></html>`

const paginaFalhou = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Não deu para conectar · Aurora Whisper</title>` + estiloDoRetorno + `</head>
<body><main><h1>Não deu para conectar</h1><p>Volte ao painel do PC e clique em Conectar à nuvem de novo.</p></main></body></html>`
