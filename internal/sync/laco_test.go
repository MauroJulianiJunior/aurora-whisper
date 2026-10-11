package syncengine

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// O laço em segundo plano, com tempos curtos: quando ele roda, quando não
// roda, a espera depois de falhar, o 401 e a linha da bandeja.

type conexaoDeTeste struct {
	mu        sync.Mutex
	cred      Credencial
	recusados []string
}

func (c *conexaoDeTeste) Credencial(context.Context) (Credencial, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cred, nil
}

func (c *conexaoDeTeste) TokenRecusado(_ context.Context, token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recusados = append(c.recusados, token)
	if token == c.cred.Token {
		c.cred.Conectado, c.cred.Token = false, ""
	}
	return nil
}

func conectada() *conexaoDeTeste {
	return &conexaoDeTeste{cred: Credencial{Configurada: true, Conectado: true, URL: "https://nuvem.exemplo.test",
		Token: tokenDeMentira, Aparelho: "a1", PareadoEm: "p1"}}
}

// lacoDeTeste é o laço rodando numa goroutine, com a nuvem de roteiro.
type lacoDeTeste struct {
	l        *Laco
	n        *nuvemDeRoteiro
	clientes atomic.Int32
	log      *lockedBuf
	parar    func()
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func rodarLaco(t *testing.T, p pcDeTeste, con Conexao, n *nuvemDeRoteiro, intervalo time.Duration) *lacoDeTeste {
	t.Helper()
	lt := &lacoDeTeste{n: n, log: &lockedBuf{}}
	lt.l = NovoLaco(ConfigDoLaco{
		Fila: p.fila, Conexao: con,
		Cliente: func(base, token string) SyncClient {
			lt.clientes.Add(1)
			return n
		},
		Intervalo: intervalo, Debounce: 5 * time.Millisecond,
		Espera: BackoffPolicy{Base: 300 * time.Millisecond, Max: 300 * time.Millisecond, Rand: func() float64 { return 0.999 }},
		NovoID: novoMotor(p, n).NovoID,
		Log:    slog.New(slog.NewTextHandler(lt.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	ctx, cancel := context.WithCancel(context.Background())
	feito := make(chan struct{})
	go func() { lt.l.Rodar(ctx); close(feito) }()
	lt.parar = func() { cancel(); <-feito }
	t.Cleanup(lt.parar)
	return lt
}

func esperarAte(t *testing.T, oQue string, cond func() bool) {
	t.Helper()
	limite := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(limite) {
			t.Fatalf("esperei %s e não aconteceu", oQue)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (n *nuvemDeRoteiro) contar(tipo string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	c := 0
	for _, p := range n.pedidos {
		if p == tipo {
			c++
		}
	}
	return c
}

// Avisar nunca bloqueia: o caminho do ditado chama isto.
func TestLaco_AvisarNuncaBloqueia(t *testing.T) {
	l := NovoLaco(ConfigDoLaco{Fila: novoPC(t).fila, Conexao: conectada(), NovoID: func() string { return "" }})
	feito := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			l.Avisar()
		}
		close(feito)
	}()
	select {
	case <-feito:
	case <-time.After(2 * time.Second):
		t.Fatal("Avisar bloqueou com o laço parado")
	}
}

// Na subida, a primeira rodada; depois de um ditado (Avisar), outra, sem
// esperar o intervalo; e a bandeja diz "em dia".
func TestLaco_RodaNaSubidaEDepoisDeCadaGravacao(t *testing.T) {
	p := novoPC(t)
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
	lt := rodarLaco(t, p, conectada(), n, time.Hour)
	esperarAte(t, "a rodada da subida", func() bool { return n.contar("pull") == 1 })
	esperarAte(t, "a bandeja em dia", func() bool { return lt.l.Resumo() == "Nuvem: em dia" })

	p.repo.AoEnfileirar(lt.l.Avisar)
	p.ditar(t, idM1, "um")
	esperarAte(t, "o push do ditado", func() bool { return n.contar("push") == 1 && n.contar("pull") == 2 })
	esperarAte(t, "a fila vazia", func() bool { c, _ := p.fila.Contar(context.Background()); return c == 0 })
	esperarAte(t, "a bandeja em dia de novo", func() bool { return lt.l.Resumo() == "Nuvem: em dia" })
	// O log só com contagens.
	esperarAte(t, "o log da rodada", func() bool { return strings.Contains(lt.log.String(), "enviados=1") })
	log := lt.log.String()
	for _, proibido := range []string{tokenDeMentira, idM1, "bruto um", "nuvem.exemplo.test"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log tem %q: %s", proibido, log)
		}
	}
}

// Desconectado: nenhum cliente é montado, nenhum pedido sai, e a bandeja diz
// "desconectada" (ou nada, se a nuvem nunca foi configurada).
func TestLaco_DesconectadoNenhumPedido(t *testing.T) {
	for _, c := range []struct {
		cred   Credencial
		resumo string
	}{
		{Credencial{}, ""},
		{Credencial{Configurada: true, URL: "https://nuvem.exemplo.test"}, "Nuvem: desconectada"},
	} {
		p := novoPC(t)
		n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
		lt := rodarLaco(t, p, &conexaoDeTeste{cred: c.cred}, n, 10*time.Millisecond)
		for i := 0; i < 5; i++ {
			lt.l.Avisar()
			time.Sleep(15 * time.Millisecond)
		}
		lt.parar()
		if lt.clientes.Load() != 0 || len(n.pedidos) != 0 {
			t.Errorf("%+v: %d clientes, pedidos %v", c.cred, lt.clientes.Load(), n.pedidos)
		}
		if r := lt.l.Resumo(); r != c.resumo {
			t.Errorf("%+v: Resumo = %q; quero %q", c.cred, r, c.resumo)
		}
	}
}

// Depois de uma falha, o laço espera: as gravações seguintes não furam a
// espera (a rede caída não vira um pedido por ditado), e a bandeja diz "sem
// conexão". Quando a rede volta, o laço volta sozinho.
func TestLaco_EsperaDepoisDeFalhar(t *testing.T) {
	p := novoPC(t)
	var fora atomic.Bool
	fora.Store(true)
	n := &nuvemDeRoteiro{pull: paginaVazia("e1")}
	n.push = func(muts []Mutation) (PushResult, error) {
		if fora.Load() {
			return PushResult{}, transitorio(CodigoRede)
		}
		return aplicaTudo("e1")(muts)
	}
	p.ditar(t, idM1, "um")
	lt := rodarLaco(t, p, conectada(), n, time.Hour)
	esperarAte(t, "a primeira falha", func() bool { return n.contar("push") == 1 })
	esperarAte(t, "a bandeja", func() bool { return lt.l.Resumo() == "Nuvem: sem conexão" })
	for i := 0; i < 10; i++ {
		lt.l.Avisar()
		time.Sleep(5 * time.Millisecond)
	}
	if got := n.contar("push"); got != 1 {
		t.Errorf("%d pushes durante a espera; quero 1", got)
	}
	if n.contar("pull") != 0 {
		t.Error("houve pull depois do push sem resposta")
	}
	fora.Store(false)
	esperarAte(t, "a volta", func() bool { return n.contar("pull") == 1 })
	esperarAte(t, "em dia", func() bool { return lt.l.Resumo() == "Nuvem: em dia" })
	// A linha da volta é escrita logo depois da bandeja mudar.
	esperarAte(t, "o log da volta", func() bool { return strings.Contains(lt.log.String(), "a nuvem voltou") })
}

// O 401: o laço chama TokenRecusado com o token da rodada, a bandeja diz
// "desconectada", e nada mais sai.
func TestLaco_401Desconecta(t *testing.T) {
	p := novoPC(t)
	n := &nuvemDeRoteiro{
		push: aplicaTudo("e1"),
		pull: func(int64) (ChangePage, error) { return ChangePage{}, ErrNaoAutorizado },
	}
	con := conectada()
	lt := rodarLaco(t, p, con, n, 10*time.Millisecond)
	esperarAte(t, "o 401", func() bool { return lt.l.Resumo() == "Nuvem: desconectada" })
	time.Sleep(50 * time.Millisecond)
	lt.parar()
	con.mu.Lock()
	defer con.mu.Unlock()
	if len(con.recusados) != 1 || con.recusados[0] != tokenDeMentira {
		t.Errorf("recusados = %v", con.recusados)
	}
	if n.contar("pull") != 1 {
		t.Errorf("%d pulls; quero 1 (depois do 401, nada)", n.contar("pull"))
	}
}

// A bandeja com a fila esperando: "N para enviar".
func TestLaco_ResumoComPendentes(t *testing.T) {
	l := NovoLaco(ConfigDoLaco{Fila: novoPC(t).fila, Conexao: conectada(), NovoID: func() string { return "" }})
	l.situacao.Store(situacaoEmDia)
	l.pendentes.Store(3)
	if r := l.Resumo(); r != "Nuvem: 3 para enviar" {
		t.Errorf("Resumo = %q", r)
	}
}

// A linha da bandeja diz o que aconteceu: a rede é "sem conexão", o 429 é
// "aguardando" (a nuvem respondeu, e pediu para esperar), e a resposta fora do
// contrato (um 403 persistente, por exemplo) é "resposta inesperada". Só o
// texto muda; a espera é a de EsperaDepois. E a volta de qualquer uma delas é
// "a nuvem voltou a responder".
func TestLaco_BandejaDizOQueAconteceu(t *testing.T) {
	for _, c := range []struct {
		nome   string
		err    error
		resumo string
	}{
		{"a rede", transitorio(CodigoRede), "Nuvem: sem conexão"},
		{"o 5xx", transitorio(CodigoServidor), "Nuvem: sem conexão"},
		{"o 429", &ErrEspere{Espera: time.Second}, "Nuvem: aguardando"},
		{"o 403", contrato(CodigoStatus), "Nuvem: resposta inesperada"},
		{"o 410 seguido", ErrRecomeco, "Nuvem: resposta inesperada"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			p := novoPC(t)
			p.ditar(t, idM1, "um")
			var falhar atomic.Bool
			falhar.Store(true)
			n := &nuvemDeRoteiro{pull: paginaVazia("e1")}
			pushes := 0
			n.push = func(muts []Mutation) (PushResult, error) {
				pushes++
				if pushes > 5 {
					// Um motor sem o limite do recomeço giraria aqui.
					return PushResult{}, contrato(CodigoLote)
				}
				if falhar.Load() {
					return PushResult{}, c.err
				}
				return aplicaTudo("e1")(muts)
			}
			log := &lockedBuf{}
			l := NovoLaco(ConfigDoLaco{Fila: p.fila, Conexao: conectada(), NovoID: novoMotor(p, n).NovoID,
				Cliente: func(string, string) SyncClient { return n },
				Log:     slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))})
			if err := l.RodadaAgora(context.Background()); err == nil {
				t.Fatal("a rodada deu certo")
			}
			if r := l.Resumo(); r != c.resumo {
				t.Errorf("Resumo = %q; quero %q", r, c.resumo)
			}
			falhar.Store(false)
			if err := l.RodadaAgora(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r := l.Resumo(); r != "Nuvem: em dia" || !strings.Contains(log.String(), "a nuvem voltou a responder") {
				t.Errorf("depois da volta: %q\n%s", r, log.String())
			}
		})
	}
}

// A rodada que fez algo anota last_success_at na hora (para o "último envio"
// da F7c); a ociosa, logo depois, não escreve.
func TestLaco_AnotaARodadaQueFezAlgo(t *testing.T) {
	p := novoPC(t)
	n := &nuvemDeRoteiro{push: aplicaTudo("e1"), pull: paginaVazia("e1")}
	relogio := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	l := NovoLaco(ConfigDoLaco{Fila: p.fila, Conexao: conectada(), NovoID: novoMotor(p, n).NovoID,
		Cliente: func(string, string) SyncClient { return n }, Agora: func() time.Time { return relogio }})
	sucesso := func() string {
		t.Helper()
		var s sql.NullString
		if err := p.db.QueryRow(`SELECT last_success_at FROM sync_state`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s.String
	}
	p.ditar(t, idM1, "um")
	if err := l.RodadaAgora(context.Background()); err != nil {
		t.Fatal(err)
	}
	primeira := sucesso()
	relogio = relogio.Add(time.Minute)
	p.ditar(t, idM2, "dois")
	if err := l.RodadaAgora(context.Background()); err != nil {
		t.Fatal(err)
	}
	if segunda := sucesso(); segunda == primeira || !strings.HasPrefix(segunda, "2026-10-06T12:01:00") {
		t.Errorf("a rodada que enviou não anotou: %q (antes %q)", segunda, primeira)
	}
	relogio = relogio.Add(time.Minute)
	if err := l.RodadaAgora(context.Background()); err != nil {
		t.Fatal(err)
	}
	if terceira := sucesso(); !strings.HasPrefix(terceira, "2026-10-06T12:01:00") {
		t.Errorf("a rodada ociosa anotou: %q", terceira)
	}
}

// Uma sequência de falhas de tipos diferentes (o 429, e depois a rede) é uma
// sequência só: a primeira em WARN, as seguintes em DEBUG, para o log não
// encher de avisos enquanto a nuvem não volta.
func TestLaco_UmaSequenciaDeFalhasUmWarn(t *testing.T) {
	p := novoPC(t)
	p.ditar(t, idM1, "um")
	erros := []error{&ErrEspere{Espera: time.Second}, transitorio(CodigoRede), contrato(CodigoStatus)}
	n := &nuvemDeRoteiro{pull: paginaVazia("e1")}
	n.push = func([]Mutation) (PushResult, error) {
		e := erros[0]
		erros = erros[1:]
		return PushResult{}, e
	}
	log := &lockedBuf{}
	l := NovoLaco(ConfigDoLaco{Fila: p.fila, Conexao: conectada(), NovoID: novoMotor(p, n).NovoID,
		Cliente: func(string, string) SyncClient { return n },
		Log:     slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	for i := 0; i < 3; i++ {
		if err := l.RodadaAgora(context.Background()); err == nil {
			t.Fatal("a rodada deu certo")
		}
	}
	if w := strings.Count(log.String(), "level=WARN"); w != 1 {
		t.Errorf("%d WARN numa sequência de três falhas; quero 1\n%s", w, log.String())
	}
	if d := strings.Count(log.String(), "a rodada não terminou de novo"); d != 2 {
		t.Errorf("%d DEBUG; quero 2\n%s", d, log.String())
	}
}
