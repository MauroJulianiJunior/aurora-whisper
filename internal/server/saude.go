package server

import (
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// A saúde do job de retenção e o aviso do X-Forwarded-For (ADR-019,
// "Decisões da F11", F11c).
//
// O alerta "retention job missed" é duplo (11-observability.md): a falta da
// linha "retenção" por mais de 2 h, e uma falha de categoria banco ou outro.
// Ler o log de outro contêiner pediria o socket do Docker, que é raiz no
// servidor; por isso a nuvem guarda em memória a última rodada e a mostra numa
// rota que só responde a quem pede pela própria máquina: o subcomando
// "healthcheck -retencao", que roda dentro do contêiner da API
// (cmd/server/healthcheck.go), lê a rota e decide.

// saudeDoJob é a última rodada do Reter: quando terminou (o relógio da nuvem),
// quantas rodadas houve desde a subida e quantas falhas graves teve a última.
type saudeDoJob struct {
	mu      sync.Mutex
	rodadas int64
	fim     time.Time
	graves  int
}

func (s *saudeDoJob) anotar(fim time.Time, r RodadaDaRetencao) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rodadas++
	s.fim = fim
	s.graves = r.FalhasGraves
}

// SaudeDaRetencao é o que a rota da saúde do job responde: só números.
type SaudeDaRetencao struct {
	// Rodadas desde a subida (a primeira roda antes de a porta abrir, então
	// pela rota nunca é zero no serve).
	Rodadas int64 `json:"rodadas"`
	// UltimaHaS: há quantos segundos a última rodada terminou (zero sem
	// rodada).
	UltimaHaS int64 `json:"ultima_ha_s"`
	// FalhasGraves da última rodada (banco ou outro).
	FalhasGraves int `json:"falhas_graves"`
}

// SaudeDaRetencao devolve a última rodada, com a idade contada no relógio da
// nuvem.
func (c *Cloud) SaudeDaRetencao() SaudeDaRetencao {
	c.saude.mu.Lock()
	defer c.saude.mu.Unlock()
	s := SaudeDaRetencao{Rodadas: c.saude.rodadas, FalhasGraves: c.saude.graves}
	if s.Rodadas > 0 {
		s.UltimaHaS = int64(c.now().Sub(c.saude.fim) / time.Second)
	}
	return s
}

// handleSaudeDaRetencao é o GET /api/v1/health/retencao. Responde só a quem
// pede pela própria máquina e sem cabeçalho de proxy: o proxy (o Traefik ou o
// Caddy, em outro contêiner ou em outro servidor) nunca chega pelo loopback, e
// um proxy na mesma máquina acrescenta o X-Forwarded-For. Para qualquer outro,
// a rota não existe (o mesmo 404 da rota não montada).
func (c *Cloud) handleSaudeDaRetencao(w http.ResponseWriter, r *http.Request) {
	if !daPropriaMaquina(r) {
		handleNaoMontada(w, r)
		return
	}
	writeJSON(w, http.StatusOK, c.SaudeDaRetencao())
}

// cabecalhosDeProxy são os que um proxy acrescenta ao encaminhar um pedido.
var cabecalhosDeProxy = []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip"}

// daPropriaMaquina: o RemoteAddr é de loopback e o pedido não passou por um
// proxy.
func daPropriaMaquina(r *http.Request) bool {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || !ap.Addr().Unmap().IsLoopback() {
		return false
	}
	for _, h := range cabecalhosDeProxy {
		if len(r.Header.Values(h)) > 0 {
			return false
		}
	}
	return true
}

// avisarXFF registra UM WARN por processo quando chega um X-Forwarded-For de
// quem não é o PROXY_CONFIAVEL. É o sinal de que o valor está errado (ou
// vazio, com um proxy na frente): o limite de tentativas usa então o endereço
// da conexão, que é o do proxy, e todo mundo cai no mesmo balde de 10 logins
// por minuto, em silêncio (cloudauth.ProxyConfiavel). O aviso não traz IP nem
// valor de cabeçalho; diz só se há um proxy configurado.
func (c *Cloud) avisarXFF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.xffAvisado.Load() && len(r.Header.Values("X-Forwarded-For")) > 0 &&
			!c.cfg.Proxy.Confia(r.RemoteAddr) && c.xffAvisado.CompareAndSwap(false, true) {
			c.log.Warn("X-Forwarded-For de quem não é o proxy confiável: o limite de tentativas usa o endereço "+
				"da conexão; confira PROXY_CONFIAVEL (este aviso sai uma vez só)",
				"proxy_confiavel", c.cfg.Proxy.Ligado())
		}
		next.ServeHTTP(w, r)
	})
}
