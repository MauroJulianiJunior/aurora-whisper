package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
)

// O comando "healthcheck" (F11c; ADR-019, "Decisões da F11"): a checagem do
// contêiner. A imagem final não tem shell nem curl, então o HEALTHCHECK do
// Dockerfile e o healthcheck do compose chamam o próprio binário:
//
//	aurora-whisper-server healthcheck            GET /api/v1/health/live
//	aurora-whisper-server healthcheck -retencao  GET /api/v1/health/retencao
//
// O pedido vai a 127.0.0.1 (ou ao loopback de HTTP_ADDR), na porta de
// HTTP_ADDR, com prazo curto, sem proxy e sem seguir redirecionamento. Sai com
// 0 (saudável) ou 1 (não saudável), e nunca com outro código: o 2 é reservado
// pelo Docker. Imprime uma linha só, sem valor de configuração nenhum.
//
// Com -retencao, sai com 1 se a última rodada do job de retenção terminou há
// mais de 2 h, se ainda não houve rodada, ou se a última teve falha de
// categoria banco ou outro: o alerta duplo do 11-observability.md, sem o
// socket do Docker e sem ler log. A falha de categoria contexto (o
// desligamento no meio de uma rodada) fica de fora.

const (
	rotaDeVida          = "/api/v1/health/live"
	rotaDaRetencao      = "/api/v1/health/retencao"
	maxIdadeDaRetencao  = 2 * time.Hour
	maxRespostaDaSaude  = 4 << 10
	saudavel, doente    = 0, 1
	usoDoHealthcheckErr = "healthcheck: uso: healthcheck [-retencao]"
)

// prazoDoHealthcheck é o prazo do pedido inteiro. É variável só para o teste
// do prazo estourado não esperar 3 s.
var prazoDoHealthcheck = 3 * time.Second

func runHealthcheck(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	retencao := flags.Bool("retencao", false, "confere a última rodada do job de retenção, e não só se o processo está de pé")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return saudavel
		}
		return doente
	}
	if flags.NArg() > 0 {
		fmt.Fprintln(stderr, usoDoHealthcheckErr)
		return doente
	}
	alvo, err := enderecoDaSaude(getenv(envHTTPAddr))
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck: "+err.Error())
		return doente
	}
	rota := rotaDeVida
	if *retencao {
		rota = rotaDaRetencao
	}
	client := &http.Client{
		Timeout: prazoDoHealthcheck,
		// Sem proxy (o HTTP_PROXY do ambiente não vale para o loopback) e sem
		// seguir redirecionamento: a saúde é a resposta desta porta.
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get("http://" + alvo + rota)
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck: FALHOU (sem resposta)")
		return doente
	}
	defer func() { _ = resp.Body.Close() }()
	corpo, err := io.ReadAll(io.LimitReader(resp.Body, maxRespostaDaSaude))
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck: FALHOU (resposta cortada)")
		return doente
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "healthcheck: FALHOU (status %d)\n", resp.StatusCode)
		return doente
	}
	if !*retencao {
		fmt.Fprintln(stdout, "healthcheck: OK")
		return saudavel
	}
	var s server.SaudeDaRetencao
	if err := json.Unmarshal(corpo, &s); err != nil {
		fmt.Fprintln(stderr, "healthcheck: FALHOU (a resposta da retenção não é a esperada)")
		return doente
	}
	switch {
	case s.Rodadas < 1:
		fmt.Fprintln(stderr, "healthcheck: FALHOU (a retenção ainda não rodou)")
		return doente
	case s.UltimaHaS > int64(maxIdadeDaRetencao/time.Second):
		fmt.Fprintf(stderr, "healthcheck: FALHOU (a última rodada da retenção terminou há %d s, mais de %d s)\n",
			s.UltimaHaS, int64(maxIdadeDaRetencao/time.Second))
		return doente
	case s.FalhasGraves > 0:
		fmt.Fprintf(stderr, "healthcheck: FALHOU (a última rodada da retenção teve %d falha(s) de banco ou outra)\n", s.FalhasGraves)
		return doente
	}
	fmt.Fprintf(stdout, "healthcheck: OK (retenção há %d s)\n", s.UltimaHaS)
	return saudavel
}

// enderecoDaSaude é o host:porta do pedido: a porta de HTTP_ADDR (padrão
// 8080), no próprio host dela se ele é um IP de loopback, ou em 127.0.0.1 (o
// 0.0.0.0 do contêiner, o "localhost" e qualquer outro). O erro não traz o
// valor.
func enderecoDaSaude(httpAddr string) (string, error) {
	if httpAddr == "" {
		httpAddr = defaultHTTPAddr
	}
	host, porta, err := net.SplitHostPort(httpAddr)
	if n, errPorta := strconv.Atoi(porta); err != nil || errPorta != nil || n < 1 || n > 65535 {
		return "", errors.New(envHTTPAddr + ": tem de ser endereço:porta (por exemplo 0.0.0.0:8080)")
	}
	if a, err := netip.ParseAddr(host); err == nil && a.Unmap().IsLoopback() {
		return net.JoinHostPort(a.Unmap().String(), porta), nil
	}
	return net.JoinHostPort("127.0.0.1", porta), nil
}
