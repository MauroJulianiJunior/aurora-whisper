package winruntime

// O painel troca o dicionário com o Agent rodando (Runtime.TrocarDicionario,
// na etapa 3: "aprendeu, o próximo ditado já sai certo"). Estes testes
// provam as duas regras, as mesmas de Ajustar: vale a partir do próximo
// ditado, e nunca no meio de um. Nada aqui abre áudio, digita ou toca a
// área de transferência: o alvo é protegido e o resolver é um dublê.

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// resolverDaGeracao é o Smart de um dicionário de UMA geração: o que ele
// devolve diz de qual dicionário veio.
type resolverDaGeracao int

func (g resolverDaGeracao) Resolve(_ context.Context, req resolver.ResolveRequest) (resolver.ResolveResult, error) {
	return resolver.ResolveResult{RawText: req.ASRResult.Text, FinalText: fmt.Sprintf("g%d", int(g))}, nil
}

// polidorDaGeracao é a metade de rede do Polish, com as guardas sobre o
// dicionário daquela geração.
type polidorDaGeracao int

func (polidorDaGeracao) Reescrever(_ context.Context, _ resolver.ResolveRequest, smart resolver.ResolveResult) resolver.ResolveResult {
	return smart
}

func dicionarioDaGeracao(g int) Dicionario {
	return Dicionario{
		Resolver: resolverDaGeracao(g),
		DominioDoApp: func(string) *string {
			s := fmt.Sprintf("g%d", g)
			return &s
		},
		AbrirPolidor: func(string, string, func(int, int)) (Polidor, string) {
			return polidorDaGeracao(g), ""
		},
	}
}

// geracaoDoRuntime lê do runtime as três peças do dicionário e exige que
// sejam da MESMA geração: é o que "trocar inteiro" quer dizer.
func geracaoDoRuntime(t *testing.T, rt *Runtime) int {
	t.Helper()
	r, ok := rt.resolucao.(resolverDaGeracao)
	if !ok {
		t.Fatalf("resolver = %T", rt.resolucao)
	}
	dom := rt.dominioDoApp("x.exe")
	pol, _ := rt.abrirPolidor("", "", nil)
	p, ok := pol.(polidorDaGeracao)
	if dom == nil || !ok || *dom != fmt.Sprintf("g%d", int(r)) || int(p) != int(r) {
		t.Fatalf("o runtime ficou com peças de gerações diferentes: resolver g%d, domínio %v, polidor %v", int(r), dom, pol)
	}
	return int(r)
}

func runtimeComDicionario(t *testing.T, geracao int) *Runtime {
	t.Helper()
	rt, _, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})
	d := dicionarioDaGeracao(geracao)
	rt.UsarResolver(d.Resolver, "mauro")
	rt.UsarDominioDoApp(d.DominioDoApp)
	rt.UsarPolish(d.AbrirPolidor)
	return rt
}

func TestTrocarDicionario_ValeNoProximoKeydown(t *testing.T) {
	rt := runtimeComDicionario(t, 1)
	rt.UsarVocabulario("ECD, DCTFWeb")
	rt.TrocarDicionario(dicionarioDaGeracao(2))

	// O pedido só guarda: nada muda até o keydown.
	if got := textoDe(t, rt.resolver(efeito("x"))); got != "g1" {
		t.Fatalf("antes do keydown o resolver deu %q, quero o dicionário antigo (g1)", got)
	}

	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if got := textoDe(t, rt.resolver(efeito("x"))); got != "g2" {
		t.Fatalf("depois do keydown o resolver deu %q, quero o dicionário novo (g2)", got)
	}
	if g := geracaoDoRuntime(t, rt); g != 2 {
		t.Fatalf("geração = %d", g)
	}
	if rt.dicionario.Load() != nil {
		t.Fatal("o pedido aplicado continua pendente")
	}
	if rt.vocabulario != "ECD, DCTFWeb" {
		t.Fatalf("o prompt do ASR mudou: %q (um pedido sem Vocabulario mantém o prompt)", rt.vocabulario)
	}
}

// TestTrocarDicionario_NoMeioDeUmDitadoEsperaOProximo: um dicionário que
// chega com um ditado em curso não muda esse ditado — nem num keydown
// repetido no meio dele. Vale no keydown seguinte, com a máquina parada.
func TestTrocarDicionario_NoMeioDeUmDitadoEsperaOProximo(t *testing.T) {
	rt := runtimeComDicionario(t, 1)

	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // ditado 1
	rt.TrocarDicionario(dicionarioDaGeracao(2))
	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // repetição da tecla no meio
	if rt.dicionario.Load() == nil {
		t.Fatal("o dicionário foi trocado com um ditado em curso")
	}
	if got := textoDe(t, rt.resolver(efeito("x"))); got != "g1" {
		t.Fatalf("o ditado em curso resolveu com %q, quero o dicionário com que começou (g1)", got)
	}
	if !holdVenceu(t, rt, 700*time.Millisecond) || rt.machine.CurrentAttempt() != dictation.NoAttempt {
		t.Fatal("o ditado 1 não terminou")
	}
	if g := geracaoDoRuntime(t, rt); g != 1 {
		t.Fatalf("o fim do ditado trocou o dicionário: g%d", g)
	}

	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // ditado 2: agora vale
	if g := geracaoDoRuntime(t, rt); g != 2 {
		t.Fatalf("o ditado 2 ficou com g%d, quero g2", g)
	}
}

func TestTrocarDicionario_OUltimoPedidoGanha(t *testing.T) {
	rt := runtimeComDicionario(t, 1)
	rt.TrocarDicionario(dicionarioDaGeracao(2))
	rt.TrocarDicionario(dicionarioDaGeracao(3))
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if g := geracaoDoRuntime(t, rt); g != 3 {
		t.Fatalf("geração = %d, quero a do último pedido (3)", g)
	}
}

// TestTrocarDicionario_SemResolverMantemOQueValia: um pedido torto não
// pode deixar o runtime sem resolver — o ditado perderia a correção de
// termos.
func TestTrocarDicionario_SemResolverMantemOQueValia(t *testing.T) {
	rt, log, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})
	d := dicionarioDaGeracao(1)
	rt.UsarResolver(d.Resolver, "mauro")
	rt.UsarDominioDoApp(d.DominioDoApp)
	rt.UsarPolish(d.AbrirPolidor)

	rt.TrocarDicionario(Dicionario{})
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if g := geracaoDoRuntime(t, rt); g != 1 {
		t.Fatalf("geração = %d, quero a de antes (1)", g)
	}
	if !strings.Contains(log.String(), "sem resolver") {
		t.Fatalf("o descarte não foi para o log:\n%s", log.String())
	}
}

// TestTrocarDicionario_LogSoDizQueTrocou: o log nunca leva texto ditado nem
// termo do dicionário.
func TestTrocarDicionario_LogSoDizQueTrocou(t *testing.T) {
	rt, log, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})
	rt.UsarResolver(resolverDaGeracao(1), "mauro")
	rt.TrocarDicionario(dicionarioDaGeracao(2))
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	linha := ""
	for _, l := range strings.Split(log.String(), "\n") {
		if strings.Contains(l, "dicionário trocado") {
			linha = l
		}
	}
	if linha == "" {
		t.Fatalf("a troca não foi ao log:\n%s", log.String())
	}
	if strings.Contains(linha, "g2") {
		t.Errorf("a linha do log traz dado do dicionário: %s", linha)
	}
}

// TestTrocarDicionario_Concorrencia: o painel troca o dicionário sem parar
// enquanto a goroutine de Run abre ditados e os termina. Vale por desenho
// — este ambiente não tem cgo para o detector de corrida —, e prova duas
// coisas que o desenho promete:
//
//   - o ditado em curso termina com o dicionário com que começou, por mais
//     pedidos que cheguem no meio;
//   - o runtime nunca fica com peças de dicionários diferentes (o Smart de
//     uma geração e as guardas do Polish de outra).
//
// Rodar com -count=20 -cpu=1,2,8.
func TestTrocarDicionario_Concorrencia(t *testing.T) {
	rt := runtimeComDicionario(t, 0)

	var geracao atomic.Int64
	parar := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-parar:
					return
				default:
				}
				rt.TrocarDicionario(dicionarioDaGeracao(int(geracao.Add(1))))
				runtime.Gosched()
			}
		}()
	}
	defer func() { close(parar); wg.Wait() }()

	comecaram := map[int]bool{} // as gerações com que algum ditado começou
	for rodada := 0; rodada < 150; rodada++ {
		rt.handleRaw(dictation.RawKeyEvent{Down: true}) // aplica o pedido, e abre o ditado
		comecou := geracaoDoRuntime(t, rt)
		comecaram[comecou] = true
		for i := 0; i < 40; i++ {
			rt.aplicarDicionario() // pedidos chegando com o ditado em curso
			runtime.Gosched()
			if g := geracaoDoRuntime(t, rt); g != comecou {
				t.Fatalf("rodada %d: o ditado começou com g%d e terminou com g%d", rodada, comecou, g)
			}
			if got := textoDe(t, rt.resolver(efeito("x"))); got != fmt.Sprintf("g%d", comecou) {
				t.Fatalf("rodada %d: o Smart resolveu com %q, o ditado começou com g%d", rodada, got, comecou)
			}
		}
		if !holdVenceu(t, rt, 700*time.Millisecond) || rt.machine.CurrentAttempt() != dictation.NoAttempt {
			t.Fatalf("rodada %d: o ditado não terminou", rodada)
		}
	}
	t.Logf("%d dicionários diferentes em 150 ditados", len(comecaram))
	// Sem isto o teste passaria também se a troca NUNCA acontecesse: um
	// runtime parado é coerente. Os pedidos têm de chegar ao próximo ditado.
	if len(comecaram) < 10 {
		t.Fatalf("em 150 ditados só %d dicionários diferentes chegaram: a troca não está acontecendo", len(comecaram))
	}
}
