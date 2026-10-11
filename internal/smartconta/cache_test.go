package smartconta

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// O Cache do Smart da conta, sem montar Smart de verdade: a montagem é um dublê
// que devolve uma Conta vazia, e conta quantas vezes foi chamada.

var ctxDeFundo = context.Background()

// montagemFalsa devolve uma montar que conta as chamadas.
func montagemFalsa(chamadas *atomic.Int32) func() (*Conta, error) {
	return func() (*Conta, error) {
		chamadas.Add(1)
		return &Conta{usuario: "u"}, nil
	}
}

func TestCache_AcertoNaoMontaDeNovo(t *testing.T) {
	c := NovoCache(4)
	var chamadas atomic.Int32
	a, err := c.Obter(ctxDeFundo, "u1", "v1", montagemFalsa(&chamadas))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Obter(ctxDeFundo, "u1", "v1", montagemFalsa(&chamadas))
	if err != nil {
		t.Fatal(err)
	}
	if a != b || chamadas.Load() != 1 {
		t.Errorf("a segunda chamada devia ser um acerto: mesma conta = %v, montagens = %d", a == b, chamadas.Load())
	}
}

func TestCache_VersaoNovaRemonta(t *testing.T) {
	c := NovoCache(4)
	var chamadas atomic.Int32
	a, _ := c.Obter(ctxDeFundo, "u1", "v1", montagemFalsa(&chamadas))
	b, _ := c.Obter(ctxDeFundo, "u1", "v2", montagemFalsa(&chamadas))
	if a == b || chamadas.Load() != 2 {
		t.Errorf("a versão nova devia remontar: mesma conta = %v, montagens = %d", a == b, chamadas.Load())
	}
	// A versão que fica é a nova.
	if _, _ = c.Obter(ctxDeFundo, "u1", "v2", montagemFalsa(&chamadas)); chamadas.Load() != 2 {
		t.Errorf("a v2 devia estar guardada: montagens = %d", chamadas.Load())
	}
	// Voltar à versão antiga também remonta: o cache guarda uma versão por conta.
	if _, _ = c.Obter(ctxDeFundo, "u1", "v1", montagemFalsa(&chamadas)); chamadas.Load() != 3 {
		t.Errorf("voltar à v1 devia remontar: montagens = %d", chamadas.Load())
	}
}

func TestCache_ContasDiferentesNaoSeMisturam(t *testing.T) {
	c := NovoCache(4)
	var chamadas atomic.Int32
	a, _ := c.Obter(ctxDeFundo, "u1", "v1", montagemFalsa(&chamadas))
	b, _ := c.Obter(ctxDeFundo, "u2", "v1", montagemFalsa(&chamadas))
	if a == b || chamadas.Load() != 2 {
		t.Errorf("duas contas, duas montagens: mesma conta = %v, montagens = %d", a == b, chamadas.Load())
	}
}

// TestCache_UmaMontagemPorConta: dez ditados da mesma conta ao mesmo tempo
// esperam a mesma montagem. Roda com -race.
func TestCache_UmaMontagemPorConta(t *testing.T) {
	c := NovoCache(4)
	var chamadas atomic.Int32
	solta := make(chan struct{})
	montar := func() (*Conta, error) {
		chamadas.Add(1)
		<-solta
		return &Conta{usuario: "u"}, nil
	}
	const n = 10
	contas := make([]*Conta, n)
	var espera sync.WaitGroup
	for i := 0; i < n; i++ {
		espera.Add(1)
		go func() {
			defer espera.Done()
			contas[i], _ = c.Obter(ctxDeFundo, "u1", "v1", montar)
		}()
	}
	// Dá tempo de todos chegarem à espera antes de soltar a montagem.
	time.Sleep(50 * time.Millisecond)
	close(solta)
	espera.Wait()
	if chamadas.Load() != 1 {
		t.Errorf("montagens = %d, quero 1", chamadas.Load())
	}
	for i := 1; i < n; i++ {
		if contas[i] != contas[0] || contas[i] == nil {
			t.Fatalf("os %d ditados deviam receber a mesma conta", n)
		}
	}
}

// TestCache_ContextoCanceladoNaoInterrompeAMontagem: quem desiste de esperar
// recebe o erro do contexto, e a conta montada fica para o ditado seguinte.
func TestCache_ContextoCanceladoNaoInterrompeAMontagem(t *testing.T) {
	c := NovoCache(4)
	var chamadas atomic.Int32
	solta := make(chan struct{})
	montar := func() (*Conta, error) {
		chamadas.Add(1)
		<-solta
		return &Conta{usuario: "u"}, nil
	}
	ctx, cancelar := context.WithCancel(ctxDeFundo)
	feito := make(chan error, 1)
	go func() {
		_, err := c.Obter(ctx, "u1", "v1", montar)
		feito <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancelar()
	if err := <-feito; !errors.Is(err, context.Canceled) {
		t.Fatalf("erro = %v, quero context.Canceled", err)
	}
	close(solta)

	// O ditado seguinte espera a mesma montagem (ou acha a conta pronta).
	conta, err := c.Obter(ctxDeFundo, "u1", "v1", montar)
	if err != nil || conta == nil {
		t.Fatalf("Obter = %v, %v", conta, err)
	}
	if chamadas.Load() != 1 {
		t.Errorf("montagens = %d, quero 1: a primeira não foi interrompida", chamadas.Load())
	}
}

func TestCache_ErroNaoEGuardado(t *testing.T) {
	c := NovoCache(4)
	var chamadas atomic.Int32
	falha := errors.New("falhou")
	montar := func() (*Conta, error) {
		if chamadas.Add(1) == 1 {
			return nil, falha
		}
		return &Conta{usuario: "u"}, nil
	}
	if _, err := c.Obter(ctxDeFundo, "u1", "v1", montar); !errors.Is(err, falha) {
		t.Fatalf("erro = %v, quero o da montagem", err)
	}
	if c.Len() != 0 {
		t.Errorf("a conta que falhou ficou no cache (%d itens)", c.Len())
	}
	if conta, err := c.Obter(ctxDeFundo, "u1", "v1", montar); err != nil || conta == nil {
		t.Fatalf("a segunda tentativa devia montar: %v, %v", conta, err)
	}
	if chamadas.Load() != 2 {
		t.Errorf("montagens = %d, quero 2", chamadas.Load())
	}
}

// TestCache_PanicoViraErro: uma montagem que entra em pânico não derruba o
// processo, e o texto do pânico (que poderia citar o que a pessoa ditou) não
// volta no erro.
func TestCache_PanicoViraErro(t *testing.T) {
	c := NovoCache(4)
	_, err := c.Obter(ctxDeFundo, "u1", "v1", func() (*Conta, error) {
		panic("texto que a pessoa ditou")
	})
	if !errors.Is(err, ErrMontar) {
		t.Fatalf("erro = %v, quero ErrMontar", err)
	}
	if err.Error() != ErrMontar.Error() {
		t.Errorf("o erro traz mais que o fixo: %q", err)
	}
	if c.Len() != 0 {
		t.Errorf("a conta que falhou ficou no cache (%d itens)", c.Len())
	}
}

// TestCache_SaiAContaDeMenorUso: com o cache cheio, sai a menos usada.
func TestCache_SaiAContaDeMenorUso(t *testing.T) {
	c := NovoCache(2)
	var chamadas atomic.Int32
	c.Obter(ctxDeFundo, "a", "v", montagemFalsa(&chamadas))
	c.Obter(ctxDeFundo, "b", "v", montagemFalsa(&chamadas))
	c.Obter(ctxDeFundo, "a", "v", montagemFalsa(&chamadas)) // a foi usada de novo: b é a menos usada
	c.Obter(ctxDeFundo, "c", "v", montagemFalsa(&chamadas)) // enche: sai b
	if c.Len() != 2 {
		t.Fatalf("Len = %d, quero 2", c.Len())
	}
	antes := chamadas.Load()
	c.Obter(ctxDeFundo, "a", "v", montagemFalsa(&chamadas))
	c.Obter(ctxDeFundo, "c", "v", montagemFalsa(&chamadas))
	if chamadas.Load() != antes {
		t.Errorf("a e c deviam estar guardadas, mas remontaram")
	}
	c.Obter(ctxDeFundo, "b", "v", montagemFalsa(&chamadas))
	if chamadas.Load() != antes+1 {
		t.Errorf("b devia ter saído, e remontado")
	}
}

func TestCache_MaxPadrao(t *testing.T) {
	if got := NovoCache(0).max; got != CacheMaxPadrao {
		t.Errorf("max = %d, quero o padrão %d", got, CacheMaxPadrao)
	}
	if got := NovoCache(-3).max; got != CacheMaxPadrao {
		t.Errorf("max = %d, quero o padrão %d", got, CacheMaxPadrao)
	}
}

func TestCache_Esquecer(t *testing.T) {
	c := NovoCache(4)
	var chamadas atomic.Int32
	c.Obter(ctxDeFundo, "u1", "v1", montagemFalsa(&chamadas))
	c.Esquecer("u1")
	if c.Len() != 0 {
		t.Fatalf("Len = %d depois de Esquecer", c.Len())
	}
	c.Obter(ctxDeFundo, "u1", "v1", montagemFalsa(&chamadas))
	if chamadas.Load() != 2 {
		t.Errorf("a conta esquecida devia remontar: montagens = %d", chamadas.Load())
	}
	c.Esquecer("conta-que-nao-existe") // não faz nada
}

// TestCache_EsquecerNoMeioDaMontagemNaoGuarda: a conta apagada durante a
// montagem não volta ao cache.
func TestCache_EsquecerNoMeioDaMontagemNaoGuarda(t *testing.T) {
	c := NovoCache(4)
	solta := make(chan struct{})
	comecou := make(chan struct{})
	montar := func() (*Conta, error) {
		close(comecou)
		<-solta
		return &Conta{usuario: "u"}, nil
	}
	feito := make(chan struct{})
	go func() {
		c.Obter(ctxDeFundo, "u1", "v1", montar)
		close(feito)
	}()
	<-comecou
	c.Esquecer("u1")
	close(solta)
	<-feito
	if c.Len() != 0 {
		t.Errorf("a conta esquecida no meio da montagem foi guardada (%d itens)", c.Len())
	}
}

// TestCache_VersaoNovaDuranteAMontagemDaAntiga: a montagem mais nova é a que
// fica, mesmo que a antiga termine depois.
func TestCache_VersaoNovaDuranteAMontagemDaAntiga(t *testing.T) {
	c := NovoCache(4)
	soltaAntiga := make(chan struct{})
	comecouAntiga := make(chan struct{})
	antiga := &Conta{usuario: "antiga"}
	nova := &Conta{usuario: "nova"}

	feitoAntiga := make(chan *Conta, 1)
	go func() {
		conta, _ := c.Obter(ctxDeFundo, "u1", "v1", func() (*Conta, error) {
			close(comecouAntiga)
			<-soltaAntiga
			return antiga, nil
		})
		feitoAntiga <- conta
	}()
	<-comecouAntiga

	// A versão nova monta sem esperar a antiga.
	got, err := c.Obter(ctxDeFundo, "u1", "v2", func() (*Conta, error) { return nova, nil })
	if err != nil || got != nova {
		t.Fatalf("Obter(v2) = %v, %v", got, err)
	}
	close(soltaAntiga)
	if conta := <-feitoAntiga; conta != antiga {
		t.Errorf("quem pediu a v1 recebe a v1: %v", conta)
	}

	// O cache ficou com a v2.
	var chamadas atomic.Int32
	got, _ = c.Obter(ctxDeFundo, "u1", "v2", montagemFalsa(&chamadas))
	if got != nova || chamadas.Load() != 0 {
		t.Errorf("o cache devia ter a v2 (nova): conta = %v, montagens = %d", got, chamadas.Load())
	}
}
