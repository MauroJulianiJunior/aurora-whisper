package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// O servir com o job de retenção (F9d; ADR-019, "Decisões da F9"), sobre o
// Store em memória, sem Docker: a linha "retenção" na subida, e o desligamento
// que espera o goroutine da arrumação antes de "desligado" (o job escreve, e
// no serve o pool fecha logo depois).

// retencaoLenta é o Store em memória com um Reter que segura até o ctx acabar
// e só então termina, devagar.
type retencaoLenta struct {
	*cloudstore.MemoryStore
	entrou    chan struct{}
	terminou  atomic.Bool
	terminouA atomic.Int64
}

func (r *retencaoLenta) Reter(ctx context.Context, userID string, now time.Time) (cloudstore.Retido, error) {
	select {
	case r.entrou <- struct{}{}:
	default:
	}
	<-ctx.Done()
	// Devagar: se o servir não esperasse, "desligado" sairia antes.
	time.Sleep(150 * time.Millisecond)
	r.terminouA.Store(time.Now().UnixNano())
	r.terminou.Store(true)
	return cloudstore.Retido{}, ctx.Err()
}

func nuvemDoServir(t *testing.T, store cloudstore.Store, acc cloudstore.Accounts, logger *slog.Logger) *server.Cloud {
	t.Helper()
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	cloud, err := server.NewCloud(server.CloudConfig{
		Store: store, Accounts: acc, BaseURL: contracttest.BaseURL,
		SessionSecret: []byte(testSessionSecret), DeviceTokenPepper: []byte(testPepper), Contas: contas,
		Google: cloudauth.NovoGoogle("cliente", testClientSecret, contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cloud
}

// servirAte sobe o servir num listener do loopback, espera pronto() e cancela,
// e devolve o código de saída.
func servirAte(t *testing.T, cloud *server.Cloud, logger *slog.Logger, pronto func()) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- servir(ctx, cloud, ln, logger, func(code int, format string, a ...any) int {
			t.Errorf("servir: "+format, a...)
			return code
		}, cloud.ArrumarSempre)
	}()
	pronto()
	cancel()
	select {
	case code := <-done:
		return code
	case <-time.After(20 * time.Second):
		t.Fatal("o servir não desligou em 20 s")
	}
	return -1
}

// O desligamento espera o goroutine da arrumação: quando o servir volta, o
// Reter que estava no meio já terminou, e a linha "retenção" dele sai antes
// de "desligado".
func TestServir_EsperaAArrumacaoAntesDeDesligado(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	mem.AddUser(contracttest.NewID())
	lenta := &retencaoLenta{MemoryStore: mem, entrou: make(chan struct{}, 1)}
	var logs safeBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	cloud := nuvemDoServir(t, lenta, mem, logger)
	code := servirAte(t, cloud, logger, func() {
		select {
		case <-lenta.entrou:
		case <-time.After(20 * time.Second):
			t.Fatal("o job não rodou na subida")
		}
	})
	voltou := time.Now().UnixNano()
	if code != 0 {
		t.Fatalf("o servir saiu com %d: %s", code, logs.String())
	}
	if !lenta.terminou.Load() || lenta.terminouA.Load() > voltou {
		t.Fatal("o servir voltou antes de o Reter terminar")
	}
	out := logs.String()
	ret, desl := strings.Index(out, `"msg":"retenção"`), strings.Index(out, `"msg":"desligado"`)
	if ret < 0 || desl < 0 || ret > desl {
		t.Fatalf("a linha \"retenção\" (%d) tem de vir antes de \"desligado\" (%d):\n%s", ret, desl, out)
	}
	if !strings.Contains(out, `"categoria":"contexto"`) {
		t.Errorf("o Reter interrompido não saiu como contexto:\n%s", out)
	}
}

// Na subida, a linha "retenção" sai só com as chaves permitidas, mesmo com
// zero, sem id nem texto.
func TestServir_ALinhaDaRetencaoNaSubida(t *testing.T) {
	mem := cloudstore.NewMemoryStore()
	user := contracttest.NewID()
	mem.AddUser(user)
	var logs safeBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	cloud := nuvemDoServir(t, mem, mem, logger)
	code := servirAte(t, cloud, logger, func() {
		deadline := time.Now().Add(20 * time.Second)
		for !strings.Contains(logs.String(), `"msg":"retenção"`) {
			if time.Now().After(deadline) {
				t.Fatal("a linha \"retenção\" não saiu na subida")
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	if code != 0 {
		t.Fatalf("o servir saiu com %d", code)
	}
	var linha map[string]any
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, `"msg":"retenção"`) {
			if err := json.Unmarshal([]byte(l), &linha); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	var ks []string
	for k := range linha {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	want := "ativos,eventos,falhas,lapides_criadas,lapides_vencidas,level,lixeira,ms,msg,mutacoes,notas,time,usuarios"
	if strings.Join(ks, ",") != want {
		t.Fatalf("as chaves = %v, quero %s", ks, want)
	}
	if linha["usuarios"] != float64(1) || linha["ativos"] != float64(0) || linha["falhas"] != float64(0) {
		t.Errorf("a linha = %v, quero 1 usuário e zero no resto", linha)
	}
	if strings.Contains(logs.String(), user) {
		t.Error("o log traz o id do usuário")
	}
}

// Um ditado do celular em andamento termina durante o desligamento (ADR-021,
// escolha menor 22): o SIGTERM chega com o provedor ainda respondendo; o servir
// espera o trabalho solto, a resposta 201 sai para o celular, o ditado fica
// gravado, e só então "desligado". Contra um provedor falso (httptest), sem rede
// de verdade.
func TestServir_ODitadoEmAndamentoTerminaNoDesligamento(t *testing.T) {
	desligarComDitado(t, false)
}

// O celular já caiu (o pedido acabou), e o trabalho solto do ditado segue: o
// desligamento não tem pedido aberto para esperar, e quem segura o servir é a
// espera dos ditados. O ditado fica gravado antes de "desligado", e o pool (no
// serve) só fecharia depois.
func TestServir_OTrabalhoSoltoTerminaNoDesligamentoSemOCelular(t *testing.T) {
	desligarComDitado(t, true)
}

// audioDoServir é um webm de mentira: o cabeçalho que o MediaRecorder do
// Chrome escreve e um recheio qualquer (o provedor falso não decodifica nada).
var audioDoServir = "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\xf2\x81\x04\x42\xf3\x81\x08\x42\x82\x84webm\x42\x87\x81\x04\x42\x85\x81\x02" +
	strings.Repeat("x", 4096)

// nuvemDoDitado é a nuvem do servir com uma conta pronta para ditar pelo
// celular: a sessão, o aceite da OpenAI e a chave de mentira dela, cifrada.
type nuvemDoDitado struct {
	mem    *cloudstore.MemoryStore
	user   string
	tok    string
	cloud  *server.Cloud
	logs   *safeBuffer
	logger *slog.Logger
}

// montarNuvemDoDitado monta a nuvem com a transcrição e o Polish no provedor
// falso da URL dada (httptest, sem rede de verdade). embrulhar, se não for nil,
// troca o Store que a nuvem recebe.
func montarNuvemDoDitado(t *testing.T, provedorURL string, embrulhar func(cloudstore.Store) cloudstore.Store) *nuvemDoDitado {
	t.Helper()
	mem := cloudstore.NewMemoryStore()
	u, err := mem.EntrarComGoogle(context.Background(), cloudstore.Identidade{Sub: "sub-servir", Email: "mauro@exemplo.test"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tok := cloudauth.NovoSegredo()
	if _, err := mem.CriarSessao(context.Background(), cloudstore.NovaSessao{
		UserID: u.ID, TokenHMAC: cloudauth.HMAC([]byte(testSessionSecret), tok), Rotulo: "Android · Chrome",
		Agora: time.Now(), ExpiraEm: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.GravarAceite(context.Background(), u.ID, cloudstore.NovoAceite{Versao: cloudstore.VersaoDoAviso, Transcricao: "openai"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	c, err := cofre.Novo(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	selada, err := c.Selar(u.ID, "openai", "sk-de-mentira-do-servir-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.GravarChaveDeIA(context.Background(), u.ID, cloudstore.ChaveDeIA{
		Provedor: "openai", IDDaCifra: selada.IDDaCifra, Nonce: selada.Nonce, Cifrado: selada.Cifrado, Final: "6789",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	n := &nuvemDoDitado{mem: mem, user: u.ID, tok: tok, logs: &safeBuffer{}}
	n.logger = slog.New(slog.NewJSONHandler(n.logs, nil))
	contas, err := cloudauth.ParseContas("mauro@exemplo.test")
	if err != nil {
		t.Fatal(err)
	}
	var store cloudstore.Store = mem
	if embrulhar != nil {
		store = embrulhar(mem)
	}
	n.cloud, err = server.NewCloud(server.CloudConfig{
		Store: store, Accounts: mem, BaseURL: contracttest.BaseURL,
		SessionSecret: []byte(testSessionSecret), DeviceTokenPepper: []byte(testPepper), Contas: contas,
		Google: cloudauth.NovoGoogle("cliente", testClientSecret, contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Logger: n.logger,
		Cofre: c, ChavesDeIA: mem, Ditado: mem, Modelos: server.ModelosPelaRede{Cliente: ia.Cliente{BaseOpenAI: provedorURL}},
		Voz:    &nuvem.Cliente{BaseOpenAI: provedorURL + "/v1"},
		Polish: &ia.Cliente{BaseOpenAI: provedorURL, BaseAnthropic: provedorURL},
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// servirNoFundo sobe o servir da nuvem num listener do loopback e devolve o
// listener e o canal com o código de saída. O ctx é o SIGTERM.
func (n *nuvemDoDitado) servirNoFundo(t *testing.T, ctx context.Context) (net.Listener, <-chan int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		done <- servir(ctx, n.cloud, ln, n.logger, func(code int, format string, a ...any) int {
			t.Errorf("servir: "+format, a...)
			return code
		}, n.cloud.ArrumarDeHoraEmHora)
	}()
	return ln, done
}

// esperarODesligado espera o servir voltar com 0 e confere o fim do
// desligamento com um ditado: ele ficou gravado antes de o servir voltar (no
// serve, o pool só fecha depois), a linha "ditado" saiu antes de "desligado", e
// o texto ditado não foi ao log.
func (n *nuvemDoDitado) esperarODesligado(t *testing.T, done <-chan int, id, texto string) {
	t.Helper()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("o servir saiu com %d: %s", code, n.logs.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("o servir não desligou")
	}
	if s, err := n.mem.Situacao(context.Background(), n.user, id); err != nil || s.Estado != cloudstore.IdDitado {
		t.Fatalf("o ditado não ficou gravado antes de o servir voltar: %+v %v", s, err)
	}
	out := n.logs.String()
	ditado, desl := strings.Index(out, `"msg":"ditado"`), strings.Index(out, `"msg":"desligado"`)
	if ditado < 0 || desl < 0 || ditado > desl {
		t.Fatalf("a linha \"ditado\" (%d) tem de vir antes de \"desligado\" (%d):\n%s", ditado, desl, out)
	}
	if strings.Contains(out, texto) {
		t.Fatal("o log traz o texto ditado")
	}
}

func desligarComDitado(t *testing.T, celularCai bool) {
	t.Helper()
	chegou := make(chan struct{}, 1)
	provedor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		chegou <- struct{}{}
		// O desligamento começa enquanto o provedor ainda responde.
		time.Sleep(1500 * time.Millisecond)
		_, _ = io.WriteString(w, `{"text":"o ditado que terminou durante o desligamento"}`)
	}))
	defer provedor.Close()
	n := montarNuvemDoDitado(t, provedor.URL, nil)
	logs := n.logs
	ctx, desligar := context.WithCancel(context.Background())
	defer desligar()
	ln, done := n.servirNoFundo(t, ctx)

	id := cloudstore.NewUUIDv7()
	req, err := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+"/api/v1/dictations", strings.NewReader(audioDoServir))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = contracttest.HostNuvem
	for k, v := range map[string]string{
		"Cookie": "__Host-sessao=" + n.tok, "Sec-Fetch-Site": "same-origin", "Content-Type": "audio/webm",
		"Ditado-Id": id, "Ditado-Duracao-Ms": "3000", "Ditado-Fala-Ms": "2000",
	} {
		req.Header.Set(k, v)
	}
	type resultado struct {
		status int
		corpo  string
		err    error
	}
	ctxDoCelular, cair := context.WithCancel(context.Background())
	defer cair()
	resposta := make(chan resultado, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req.WithContext(ctxDoCelular))
		if err != nil {
			resposta <- resultado{err: err}
			return
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resposta <- resultado{status: resp.StatusCode, corpo: string(raw)}
	}()
	select {
	case <-chegou:
	case <-time.After(20 * time.Second):
		t.Fatal("o ditado não chegou ao provedor")
	}
	if celularCai {
		cair()
		<-resposta
		// O pedido do celular já acabou no servidor quando o SIGTERM chega.
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(logs.String(), `"rota":"POST /api/v1/dictations"`) {
			if time.Now().After(deadline) {
				t.Fatal("o pedido do celular que caiu não terminou no servidor")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	desligar() // o SIGTERM
	if !celularCai {
		r := <-resposta
		if r.err != nil || r.status != http.StatusCreated || !strings.Contains(r.corpo, `"id":"`+id+`"`) {
			t.Fatalf("a resposta do ditado durante o desligamento: %v %d %s", r.err, r.status, r.corpo)
		}
	}
	n.esperarODesligado(t, done, id, "terminou durante")
	if out := logs.String(); celularCai && !strings.Contains(out, `"msg":"esperando os ditados em andamento"`) {
		t.Fatalf("sem o celular, quem segura o desligamento é a espera dos ditados:\n%s", out)
	}
}

// situacaoVigiada é o Store do servir que avisa (releu) a segunda leitura da
// situação de um ditado: a releitura de quem pegou a vaga, logo antes de ler o
// corpo.
type situacaoVigiada struct {
	cloudstore.Store
	leituras atomic.Int32
	releu    chan struct{}
}

func (s *situacaoVigiada) Situacao(ctx context.Context, userID, id string) (cloudstore.SituacaoDoId, error) {
	if s.leituras.Add(1) == 2 {
		close(s.releu)
	}
	return s.Store.Situacao(ctx, userID, id)
}

// O dono de um ditado que ainda lia o corpo quando o prazo do Shutdown acabou
// (a revisão da C5b): a primeira espera dos ditados volta sem nenhum, porque o
// trabalho dele ainda não começou. Ele termina de ler, solta o trabalho, e o
// celular cai logo depois: o pedido acaba sem esperar o trabalho, e o segundo
// Shutdown volta. O servir espera os ditados de novo, e o ditado já pago fica
// gravado antes de "desligado" (no serve, o pool só fecharia depois).
func TestServir_ODonoQueAindaLiaOCorpoQuandoOShutdownPassouDoPrazo(t *testing.T) {
	antes := shutdownTimeout
	shutdownTimeout = 200 * time.Millisecond
	t.Cleanup(func() { shutdownTimeout = antes })

	chegou := make(chan struct{}, 1)
	provedor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		chegou <- struct{}{}
		// O provedor responde depois de o celular cair e o pedido acabar.
		time.Sleep(1500 * time.Millisecond)
		_, _ = io.WriteString(w, `{"text":"o ditado do dono que lia o corpo no desligamento"}`)
	}))
	defer provedor.Close()
	vigiada := &situacaoVigiada{releu: make(chan struct{})}
	n := montarNuvemDoDitado(t, provedor.URL, func(s cloudstore.Store) cloudstore.Store {
		vigiada.Store = s
		return vigiada
	})
	ctx, desligar := context.WithCancel(context.Background())
	defer desligar()
	ln, done := n.servirNoFundo(t, ctx)

	// O celular manda os cabeçalhos e o começo do corpo, e segura o resto: a
	// conexão crua controla quando cada pedaço sai.
	celular, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer celular.Close()
	id := cloudstore.NewUUIDv7()
	cabecalhos := "POST /api/v1/dictations HTTP/1.1\r\nHost: " + contracttest.HostNuvem + "\r\n" +
		"Cookie: __Host-sessao=" + n.tok + "\r\nSec-Fetch-Site: same-origin\r\nContent-Type: audio/webm\r\n" +
		"Ditado-Id: " + id + "\r\nDitado-Duracao-Ms: 3000\r\nDitado-Fala-Ms: 2000\r\n" +
		"Content-Length: " + strconv.Itoa(len(audioDoServir)) + "\r\n\r\n"
	if _, err := io.WriteString(celular, cabecalhos+audioDoServir[:64]); err != nil {
		t.Fatal(err)
	}
	select {
	case <-vigiada.releu:
	case <-time.After(20 * time.Second):
		t.Fatal("o pedido não pegou a vaga")
	}

	desligar() // o SIGTERM, com o dono no meio do corpo
	// O Shutdown passa do prazo (o pedido está aberto), e a primeira espera dos
	// ditados volta sem nenhum.
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(n.logs.String(), `"msg":"desligando"`) {
		if time.Now().After(deadline) {
			t.Fatal("o servir não começou a desligar")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(shutdownTimeout + 800*time.Millisecond)
	if _, err := io.WriteString(celular, audioDoServir[64:]); err != nil {
		t.Fatal(err)
	}
	select {
	case <-chegou:
	case <-time.After(20 * time.Second):
		t.Fatal("o ditado não chegou ao provedor")
	}
	_ = celular.Close() // o celular cai com o trabalho no provedor
	n.esperarODesligado(t, done, id, "lia o corpo")
}
