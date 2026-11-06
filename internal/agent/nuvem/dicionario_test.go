package nuvem_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/nuvem"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	syncengine "github.com/MauroJulianiJunior/aurora-whisper/internal/sync"
)

// O dicionário para o celular no serviço da nuvem (ADR-023, F): ligar, desligar e
// o estado, contra a nuvem falsa (httptest). Nada aqui toca no sistema real.

// opcaoMem é a opção em memória.
type opcaoMem struct {
	mu        sync.Mutex
	ligada    bool
	errGravar error
	gravacoes []bool
}

func (o *opcaoMem) Ligada(context.Context) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ligada, nil
}

func (o *opcaoMem) Gravar(_ context.Context, l bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.errGravar != nil {
		return o.errGravar
	}
	o.ligada = l
	o.gravacoes = append(o.gravacoes, l)
	return nil
}

// gravouLigada diz se alguma gravação foi "ligada" (o pareamento grava
// "desligada" de propósito).
func (o *opcaoMem) gravouLigada() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Contains(o.gravacoes, true)
}

func (o *opcaoMem) estaLigada() bool { l, _ := o.Ligada(context.Background()); return l }

// fonteMem é o dicionario.json de agora.
type fonteMem struct {
	sem dictionary.Semente
	err error
}

func (f *fonteMem) Dicionario(context.Context) (dictionary.Semente, error) { return f.sem, f.err }

// dicionarioDaNuvem é o que a nuvem falsa responde nas três rotas.
type dicionarioDaNuvem struct {
	mu      sync.Mutex
	resp    func(metodo string) (status int, corpo string, cab map[string]string)
	pedidos []pedidoDoDicionario
}

type pedidoDoDicionario struct {
	metodo, bearer, sha, fonte string
	corpo                      string
}

func (d *dicionarioDaNuvem) servir(w http.ResponseWriter, r *http.Request, bearer string) {
	raw, _ := io.ReadAll(r.Body)
	d.mu.Lock()
	d.pedidos = append(d.pedidos, pedidoDoDicionario{r.Method, bearer, r.Header.Get("Dicionario-SHA256"), r.Header.Get("Dicionario-Fonte"), string(raw)})
	f := d.resp
	d.mu.Unlock()
	status, corpo, cab := f(r.Method)
	for k, v := range cab {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, corpo)
}

func (d *dicionarioDaNuvem) responder(f func(metodo string) (int, string, map[string]string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resp = f
}

func (d *dicionarioDaNuvem) metodos() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var m []string
	for _, p := range d.pedidos {
		m = append(m, p.metodo)
	}
	return m
}

func (d *dicionarioDaNuvem) ultimo() pedidoDoDicionario {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pedidos[len(d.pedidos)-1]
}

const estadoEste = `{"fonte":"este","aparelho":"DELL-G15","termos":2,"sha256":"s","atualizado_em":"2026-11-05T14:02:00Z"}`

func respostaEste(metodo string) (int, string, map[string]string) {
	if metodo == http.MethodDelete {
		return http.StatusNoContent, "", nil
	}
	return http.StatusOK, estadoEste, nil
}

type cenarioDoDicionario struct {
	*cenario
	opcao *opcaoMem
	fonte *fonteMem
	dic   *dicionarioDaNuvem
}

func novoCenarioDoDicionario(t *testing.T) *cenarioDoDicionario {
	t.Helper()
	op := &opcaoMem{}
	fo := &fonteMem{sem: dictionary.Semente{Usuario: "local", Entradas: []dictionary.EntradaSemente{
		{Canonica: "Kubernetes", Alias: []string{"cubernetes"}}, {Canonica: "Aurora"}}}}
	c := novoCenario(t, func(cfg *nuvem.Config) { cfg.OpcaoDoDicionario, cfg.FonteDoDicionario = op, fo })
	d := &dicionarioDaNuvem{resp: respostaEste}
	c.nuvem.mexer(func(n *nuvemFalsa) { n.dicionario = d.servir })
	return &cenarioDoDicionario{cenario: c, opcao: op, fonte: fo, dic: d}
}

func (c *cenarioDoDicionario) ligar() (nuvem.EstadoDoDicionario, error) {
	return c.s.LigarDicionario(context.Background())
}

func (c *cenarioDoDicionario) desligar() (nuvem.EstadoDoDicionario, error) {
	return c.s.DesligarDicionario(context.Background())
}

func TestLigarDicionario_PUTTomarNaHoraEOpcaoSoComOk(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	tok := c.conectarDeVez()

	e, err := c.ligar()
	if err != nil {
		t.Fatalf("ligar: %v", err)
	}
	if !e.Ligado || !c.opcao.estaLigada() || e.Fonte != "este" || e.Termos != 2 || e.AtualizadoEm != "2026-11-05T14:02:00Z" ||
		e.EnviadoEm != c.clk.agora().UTC().Format(time.RFC3339) || e.Aviso != "" {
		t.Errorf("estado depois de ligar: %+v", e)
	}
	// O PUT foi "tomar", com o token do aparelho e a cópia de agora.
	p := c.dic.pedidos[0]
	semente, _ := c.fonte.Dicionario(context.Background())
	esperado, sha, _ := semente.BytesDaCopia()
	if p.metodo != http.MethodPut || p.bearer != tok || p.fonte != "tomar" || p.sha != sha || p.corpo != string(esperado) {
		t.Errorf("o PUT: %+v", p)
	}
	// Em seguida, o GET do estado (o que o painel mostra).
	if m := c.dic.metodos(); len(m) != 2 || m[1] != http.MethodGet {
		t.Errorf("pedidos: %v", m)
	}
	if strings.Contains(c.textoDoLog(), "Kubernetes") || strings.Contains(c.textoDoLog(), tok) {
		t.Errorf("o log leva um termo ou o token:\n%s", c.textoDoLog())
	}
}

func TestLigarDicionario_QuandoFalhaAOpcaoFicaDesligadaEADizPorque(t *testing.T) {
	ctx := context.Background()
	casos := []struct {
		nome   string
		resp   func(string) (int, string, map[string]string)
		status int
		msg    string
	}{
		{"422 com o motivo", func(string) (int, string, map[string]string) {
			return 422, `{"error":{"code":"DICIONARIO_INVALIDO","motivo":"variantes_demais"}}`, nil
		}, http.StatusConflict, syncengine.FraseDoMotivoDaCopia("variantes_demais")},
		{"404: a nuvem de antes", func(string) (int, string, map[string]string) { return 404, "", nil },
			http.StatusBadGateway, "A nuvem ainda não recebe o dicionário."},
		{"429", func(string) (int, string, map[string]string) { return 429, "", map[string]string{"Retry-After": "5"} },
			http.StatusBadGateway, nuvem.MsgDicionarioEspere},
		{"500", func(string) (int, string, map[string]string) { return 500, "", nil },
			http.StatusBadGateway, nuvem.MsgDicionarioSemRede},
		{"redirecionamento", func(string) (int, string, map[string]string) {
			return 307, "", map[string]string{"Location": "http://127.0.0.1:1/x"}
		}, http.StatusBadGateway, nuvem.MsgDicionarioSemRede},
	}
	for _, k := range casos {
		c := novoCenarioDoDicionario(t)
		c.conectarDeVez()
		c.dic.responder(k.resp)
		_, err := c.ligar()
		querErro(t, err, k.status, k.msg)
		if c.opcao.estaLigada() || c.opcao.gravouLigada() {
			t.Errorf("%s: a opção foi gravada ligada (%v)", k.nome, c.opcao.gravacoes)
		}
	}

	// Sem rede: a nuvem fora do ar.
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.nuvem.srv.Close()
	_, err := c.ligar()
	querErro(t, err, http.StatusBadGateway, nuvem.MsgDicionarioSemRede)
	if c.opcao.estaLigada() {
		t.Error("sem rede, a opção ficou ligada")
	}

	// A cópia fora dos limites daqui nem sai do PC.
	c = novoCenarioDoDicionario(t)
	c.conectarDeVez()
	for i := 0; i <= dictionary.MaxTermosDaCopia; i++ {
		c.fonte.sem.Entradas = append(c.fonte.sem.Entradas, dictionary.EntradaSemente{Canonica: fmt.Sprintf("termo-%d", i)})
	}
	_, err = c.ligar()
	querErro(t, err, http.StatusConflict, syncengine.FraseDoMotivoDaCopia("termos_demais"))
	if len(c.dic.pedidos) != 0 || c.opcao.estaLigada() {
		t.Errorf("fora dos limites: %d pedidos, ligada %v", len(c.dic.pedidos), c.opcao.estaLigada())
	}

	// O dicionário que não se lê.
	c = novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.fonte.err = dictionary.ErrSemArquivo
	_, err = c.ligar()
	querErro(t, err, http.StatusConflict, nuvem.MsgDicionarioNaoLeu)

	// Desconectado.
	c = novoCenarioDoDicionario(t)
	_, err = c.ligar()
	querErro(t, err, http.StatusConflict, nuvem.MsgDicionarioConecte)

	// A opção que não grava: a cópia que subiu sai da nuvem.
	c = novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.opcao.errGravar = errors.New("banco fora")
	_, err = c.ligar()
	var ne *nuvem.Erro
	if !errors.As(err, &ne) || ne.Status != 500 || ne.Mensagem != nuvem.MsgDicionarioNaoGravou {
		t.Fatalf("opção que não grava: %v", err)
	}
	if m := c.dic.metodos(); len(m) != 2 || m[0] != http.MethodPut || m[1] != http.MethodDelete {
		t.Errorf("a cópia deveria sair da nuvem: %v", m)
	}
	_ = ctx
}

func TestLigarDicionario_401DesconectaOPC(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.dic.responder(func(string) (int, string, map[string]string) { return 401, "", nil })
	_, err := c.ligar()
	querErro(t, err, http.StatusConflict, nuvem.MsgDicionarioRecusouPC)
	if e := c.estado(); e.Estado != nuvem.Desconectado {
		t.Errorf("depois do 401 o PC deveria estar desconectado: %+v", e)
	}
	if _, err := c.cofre.Ler(nuvem.NomeNoCofrePortatil); !errors.Is(err, chaves.ErrAusente) {
		t.Error("o token recusado ficou no cofre")
	}
}

func TestDesligarDicionario_DELETEESoComOKDesliga(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	tok := c.conectarDeVez()
	if _, err := c.ligar(); err != nil {
		t.Fatal(err)
	}
	c.dic.responder(func(m string) (int, string, map[string]string) {
		if m == http.MethodDelete {
			return 204, "", nil
		}
		return 200, `{"fonte":"nenhuma"}`, nil
	})

	e, err := c.desligar()
	if err != nil {
		t.Fatalf("desligar: %v", err)
	}
	if e.Ligado || c.opcao.estaLigada() || e.Fonte != "nenhuma" || e.EnviadoEm != "" || e.Termos != 0 {
		t.Errorf("estado depois de desligar: %+v", e)
	}
	var del *pedidoDoDicionario
	for i := range c.dic.pedidos {
		if c.dic.pedidos[i].metodo == http.MethodDelete {
			del = &c.dic.pedidos[i]
		}
	}
	if del == nil || del.bearer != tok {
		t.Errorf("o DELETE: %+v", del)
	}
}

func TestDesligarDicionario_SemRedeAOpcaoContinuaLigada(t *testing.T) {
	for nome, resp := range map[string]func(string) (int, string, map[string]string){
		"500": func(string) (int, string, map[string]string) { return 500, "", nil },
		"429": func(string) (int, string, map[string]string) { return 429, "", nil },
		"403": func(string) (int, string, map[string]string) { return 403, "", nil },
	} {
		c := novoCenarioDoDicionario(t)
		c.conectarDeVez()
		if _, err := c.ligar(); err != nil {
			t.Fatal(err)
		}
		c.dic.responder(resp)
		_, err := c.desligar()
		querErro(t, err, http.StatusBadGateway, "Não deu para apagar a cópia da nuvem agora. Tente de novo.")
		if !c.opcao.estaLigada() {
			t.Errorf("%s: a opção desligou sem a cópia sair", nome)
		}
	}

	// A nuvem fora do ar.
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	if _, err := c.ligar(); err != nil {
		t.Fatal(err)
	}
	c.nuvem.srv.Close()
	_, err := c.desligar()
	querErro(t, err, http.StatusBadGateway, nuvem.MsgDicionarioNaoApagou)
	if !c.opcao.estaLigada() {
		t.Error("sem rede, a opção desligou")
	}
}

func TestDesligarDicionario_404DeUmaNuvemSemARotaDesliga(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.opcao.ligada = true
	c.dic.responder(func(string) (int, string, map[string]string) { return 404, "", nil })
	e, err := c.desligar()
	if err != nil || e.Ligado || c.opcao.estaLigada() {
		t.Fatalf("404 no DELETE: %+v, %v", e, err)
	}
}

func TestDesligarDicionario_DesconectadoSoGravaDesligada(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	c.opcao.ligada = true
	e, err := c.desligar()
	if err != nil || e.Ligado || c.opcao.estaLigada() || len(c.dic.pedidos) != 0 {
		t.Fatalf("desconectado: %+v, %v, %d pedidos", e, err, len(c.dic.pedidos))
	}
}

func TestDicionario_DesconectarEReconectarDesligam(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	if _, err := c.ligar(); err != nil {
		t.Fatal(err)
	}
	c.s.OpcaoDaCopia().Avisar("um aviso de antes")
	if _, err := c.s.Desconectar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.opcao.estaLigada() {
		t.Fatal("desconectar não desligou o dicionário")
	}
	e, err := c.s.EstadoDoDicionario(context.Background())
	if err != nil || e.Ligado || e.Aviso != "" || e.EnviadoEm != "" {
		t.Errorf("estado desconectado: %+v, %v", e, err)
	}

	// Reconectar não manda sem um clique novo: mesmo com a opção ligada à mão,
	// o pareamento novo começa desligado.
	c.opcao.ligada = true
	c.conectarDeVez()
	if c.opcao.estaLigada() {
		t.Error("reconectar deixou o dicionário ligado")
	}
}

func TestDicionario_O401DoSyncDesconectaEDesliga(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	tok := c.conectarDeVez()
	c.opcao.ligada = true
	if err := c.s.TokenRecusado(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	if c.opcao.estaLigada() {
		t.Error("o token recusado deixou o dicionário ligado")
	}
}

func TestEstadoDoDicionario_PerguntaANuvemEDesligaQuemPerdeuAFonte(t *testing.T) {
	ctx := context.Background()

	// Outro PC é a fonte: o GET mostra, e a opção ligada desliga com o aviso.
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.opcao.ligada = true
	c.dic.responder(func(string) (int, string, map[string]string) {
		return 200, `{"fonte":"outro","aparelho":"NOTEBOOK","termos":49,"sha256":"s","atualizado_em":"2026-11-05T14:02:00Z"}`, nil
	})
	e, err := c.s.EstadoDoDicionario(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if e.Ligado || c.opcao.estaLigada() || e.Fonte != "outro" || e.Aparelho != "NOTEBOOK" || e.Termos != 49 ||
		e.Aviso != "O celular passou a usar o dicionário de NOTEBOOK." {
		t.Errorf("fonte outro: %+v", e)
	}
	// O aviso fica para a próxima leitura.
	if e, _ = c.s.EstadoDoDicionario(ctx); e.Aviso != "O celular passou a usar o dicionário de NOTEBOOK." {
		t.Errorf("o aviso não ficou: %+v", e)
	}

	// Sem cópia na nuvem.
	c = novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.opcao.ligada = true
	c.dic.responder(func(string) (int, string, map[string]string) { return 200, `{"fonte":"nenhuma"}`, nil })
	e, err = c.s.EstadoDoDicionario(ctx)
	if err != nil || e.Ligado || e.Aviso != "A cópia do dicionário foi apagada da nuvem." || e.Fonte != "nenhuma" {
		t.Errorf("fonte nenhuma: %+v, %v", e, err)
	}

	// Outro PC é a fonte, com a opção desligada: só mostra (não mexe em nada).
	c = novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.dic.responder(func(string) (int, string, map[string]string) {
		return 200, `{"fonte":"outro","aparelho":"NOTEBOOK","termos":3}`, nil
	})
	e, err = c.s.EstadoDoDicionario(ctx)
	if err != nil || e.Ligado || e.Fonte != "outro" || e.Aviso != "" || c.opcao.gravouLigada() || c.opcao.estaLigada() {
		t.Errorf("fonte outro com a opção desligada: %+v, %v", e, err)
	}
}

func TestEstadoDoDicionario_SemRedeDizOQueSabe(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	if _, err := c.ligar(); err != nil {
		t.Fatal(err)
	}
	c.nuvem.srv.Close()
	e, err := c.s.EstadoDoDicionario(context.Background())
	if err != nil {
		t.Fatalf("sem rede não é erro: %v", err)
	}
	// A fonte vazia (a nuvem não pôde ser perguntada), a opção como está, e o
	// que o PC sabe do último envio.
	if !e.Ligado || e.Fonte != "" || e.Aparelho != "" || e.Termos != 2 || e.EnviadoEm == "" {
		t.Errorf("sem rede: %+v", e)
	}
}

func TestEstadoDoDicionario_Desconectado(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	e, err := c.s.EstadoDoDicionario(context.Background())
	if err != nil || e.Ligado || e.Fonte != "" || len(c.dic.pedidos) != 0 {
		t.Fatalf("desconectado: %+v, %v, %d pedidos", e, err, len(c.dic.pedidos))
	}
}

func TestEstadoDoDicionario_O401DaNuvemDesconecta(t *testing.T) {
	c := novoCenarioDoDicionario(t)
	c.conectarDeVez()
	c.opcao.ligada = true
	c.dic.responder(func(string) (int, string, map[string]string) { return 401, "", nil })
	e, err := c.s.EstadoDoDicionario(context.Background())
	if err != nil || e.Ligado || c.opcao.estaLigada() {
		t.Fatalf("401 no GET: %+v, %v", e, err)
	}
	if es := c.estado(); es.Estado != nuvem.Desconectado {
		t.Errorf("o PC deveria estar desconectado: %+v", es)
	}
}

// A opção que o sync usa (nuvem.Servico.OpcaoDaCopia): ler, desligar com o aviso,
// avisar e contar o envio.
func TestOpcaoDaCopia_OQueOSyncUsa(t *testing.T) {
	ctx := context.Background()
	c := novoCenarioDoDicionario(t)
	o := c.s.OpcaoDaCopia()

	if l, err := o.Ligada(ctx); err != nil || l {
		t.Fatalf("Ligada: %v, %v", l, err)
	}
	c.opcao.ligada = true
	if l, _ := o.Ligada(ctx); !l {
		t.Fatal("Ligada não viu a opção")
	}

	o.Avisar("A nuvem não aceitou o dicionário.")
	e, _ := c.s.EstadoDoDicionario(ctx)
	if !e.Ligado || e.Aviso != "A nuvem não aceitou o dicionário." {
		t.Errorf("Avisar: %+v", e)
	}
	quando := time.Date(2026, 11, 5, 14, 2, 0, 0, time.UTC)
	o.Enviou(49, quando)
	e, _ = c.s.EstadoDoDicionario(ctx)
	if e.Aviso != "" || e.EnviadoEm != "2026-11-05T14:02:00Z" || e.Termos != 49 {
		t.Errorf("Enviou: %+v", e)
	}
	if err := o.Desligar(ctx, "O celular passou a usar o dicionário de X."); err != nil {
		t.Fatal(err)
	}
	e, _ = c.s.EstadoDoDicionario(ctx)
	if e.Ligado || c.opcao.estaLigada() || e.Aviso != "O celular passou a usar o dicionário de X." || e.EnviadoEm != "" {
		t.Errorf("Desligar: %+v", e)
	}
	// Se a opção não grava, o Desligar devolve o erro (o motor o leva à rodada).
	c.opcao.errGravar = errors.New("banco fora")
	if err := o.Desligar(ctx, "x"); err == nil {
		t.Error("o Desligar engoliu o erro da gravação")
	}
}

func TestDicionario_SemOsFiosDaOpcaoAsRotasRespondem503(t *testing.T) {
	c := novoCenario(t)
	for nome, f := range map[string]func() error{
		"estado":   func() error { _, err := c.s.EstadoDoDicionario(context.Background()); return err },
		"ligar":    func() error { _, err := c.s.LigarDicionario(context.Background()); return err },
		"desligar": func() error { _, err := c.s.DesligarDicionario(context.Background()); return err },
	} {
		var ne *nuvem.Erro
		if err := f(); !errors.As(err, &ne) || ne.Status != http.StatusServiceUnavailable {
			t.Errorf("%s: %v", nome, err)
		}
	}
	// E o OpcaoDaCopia sem a opção é "desligada", sem erro.
	if l, err := c.s.OpcaoDaCopia().Ligada(context.Background()); err != nil || l {
		t.Errorf("OpcaoDaCopia sem a opção: %v, %v", l, err)
	}
	// UsarDicionario liga depois da montagem.
	c.s.UsarDicionario(&opcaoMem{}, &fonteMem{})
	if _, err := c.s.EstadoDoDicionario(context.Background()); err != nil {
		t.Errorf("depois do UsarDicionario: %v", err)
	}
}
