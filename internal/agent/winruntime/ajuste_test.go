package winruntime

// O painel muda o limiar, o beep e o modo de texto com o Agent rodando
// (Runtime.Ajustar). Estes testes provam as duas regras: vale a partir do
// próximo ditado, e nunca no meio de um. Rodam com o relógio falso e um
// alvo protegido — que leva o keydown direto a StateBlocked sem abrir
// áudio, com o timer do hold armado igual.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// holdVenceu avança o relógio falso e diz se o timer do hold disparou.
func holdVenceu(t *testing.T, rt *Runtime, d time.Duration) bool {
	t.Helper()
	rt.clk.(*clock.Fake).Advance(d)
	select {
	case ev := <-rt.pending:
		rt.dispatch(ev)
		return true
	default:
		return false
	}
}

func TestAjustar_LimiarNovoValeNoProximoKeydown(t *testing.T) {
	rt, _, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})
	rt.Ajustar(Preferencias{HoldThreshold: 1500 * time.Millisecond, BeepEnabled: true})

	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if holdVenceu(t, rt, 700*time.Millisecond) {
		t.Fatal("o hold venceu nos 700ms antigos: o ajuste não foi aplicado")
	}
	if !holdVenceu(t, rt, 800*time.Millisecond) {
		t.Fatal("o hold não venceu nos 1500ms novos")
	}
	if rt.ajuste.Load() != nil {
		t.Fatal("o pedido aplicado continua pendente")
	}
}

// TestAjustar_NoMeioDeUmDitadoEsperaOProximo: um ajuste que chega com um
// ditado em curso não muda esse ditado — nem num keydown repetido no meio
// dele. Vale no keydown seguinte, com a máquina parada.
func TestAjustar_NoMeioDeUmDitadoEsperaOProximo(t *testing.T) {
	rt, _, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})

	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // ditado 1, limiar de 700ms armado
	rt.Ajustar(Preferencias{HoldThreshold: 1500 * time.Millisecond})
	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // repetição da tecla no meio
	if rt.ajuste.Load() == nil {
		t.Fatal("o ajuste foi aplicado com um ditado em curso")
	}
	if !holdVenceu(t, rt, 700*time.Millisecond) {
		t.Fatal("o ditado em curso não terminou com os 700ms com que começou")
	}
	if rt.machine.CurrentAttempt() != dictation.NoAttempt {
		t.Fatalf("o ditado 1 não terminou: State = %v", rt.machine.State())
	}

	rt.handleRaw(dictation.RawKeyEvent{Down: true}) // ditado 2: agora vale
	if holdVenceu(t, rt, 700*time.Millisecond) {
		t.Fatal("o ditado 2 ainda usou os 700ms")
	}
	if !holdVenceu(t, rt, 800*time.Millisecond) {
		t.Fatal("o ditado 2 não usou os 1500ms")
	}
}

// TestAjustar_OUltimoPedidoGanha: dois pedidos antes do próximo ditado —
// vale o segundo.
func TestAjustar_OUltimoPedidoGanha(t *testing.T) {
	rt, _, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})
	rt.Ajustar(Preferencias{HoldThreshold: 1500 * time.Millisecond})
	rt.Ajustar(Preferencias{HoldThreshold: 400 * time.Millisecond})
	rt.handleRaw(dictation.RawKeyEvent{Down: true})
	if !holdVenceu(t, rt, 400*time.Millisecond) {
		t.Fatal("o hold não venceu nos 400ms do último pedido")
	}
}

// resolverQueMarca troca o texto inteiro: se ele rodar, o teste vê.
type resolverQueMarca struct{}

func (resolverQueMarca) Resolve(_ context.Context, req resolver.ResolveRequest) (resolver.ResolveResult, error) {
	return resolver.ResolveResult{RawText: req.ASRResult.Text, FinalText: "RESOLVIDO"}, nil
}

// TestAjustar_LiteralDesligaAResolucaoEVolta: o modo Literal entrega o
// texto do ASR intocado, mesmo com o dicionário ligado, e o histórico diz
// "literal"; voltar ao Smart religa.
func TestAjustar_LiteralDesligaAResolucaoEVolta(t *testing.T) {
	rt, _, _ := runtimeComLog(t, fakeFocus{})
	rt.UsarResolver(resolverQueMarca{}, "mauro")
	const bruto = "texto do asr"

	if got := textoDe(t, rt.resolver(efeito(bruto))); got != "RESOLVIDO" || rt.modo() != "smart" {
		t.Fatalf("antes do ajuste: %q, modo %s", got, rt.modo())
	}

	rt.Ajustar(Preferencias{HoldThreshold: 700 * time.Millisecond, Literal: true})
	rt.aplicarAjuste()
	if got := textoDe(t, rt.resolver(efeito(bruto))); got != bruto {
		t.Fatalf("no Literal = %q, quero o texto do ASR intocado", got)
	}
	if rt.modo() != "literal" {
		t.Fatalf("modo no histórico = %q, quero literal", rt.modo())
	}

	rt.Ajustar(Preferencias{HoldThreshold: 700 * time.Millisecond})
	rt.aplicarAjuste()
	if got := textoDe(t, rt.resolver(efeito(bruto))); got != "RESOLVIDO" || rt.modo() != "smart" {
		t.Fatalf("de volta ao Smart: %q, modo %s", got, rt.modo())
	}
}

// TestAjustar_BeepChegaAoNucleo: o beep pedido é o que o núcleo do
// runtime passa a ter, nos dois sentidos. Que o núcleo não emite
// PlayBeepEffect com o beep desligado é provado em dictation
// (TestAjustarPreferencias_*); aqui não se chega ao beep de verdade,
// que tocaria som na máquina de quem está usando.
func TestAjustar_BeepChegaAoNucleo(t *testing.T) {
	rt, log, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})
	for _, beep := range []bool{true, false, true} {
		rt.Ajustar(Preferencias{HoldThreshold: 900 * time.Millisecond, BeepEnabled: beep})
		rt.aplicarAjuste()
		if cfg := rt.machine.Config(); cfg.BeepEnabled != beep || cfg.HoldThreshold != 900*time.Millisecond {
			t.Fatalf("pedi beep=%v, o núcleo ficou com %+v", beep, cfg)
		}
	}
	if !strings.Contains(log.String(), "preferências aplicadas") || !strings.Contains(log.String(), "hold_threshold=900ms") {
		t.Fatalf("o log não diz o que ficou valendo:\n%s", log.String())
	}
}

// TestAjustar_PedidoInvalidoMantemOQueValia: se o núcleo recusar (a API
// local já valida; chegar aqui é defeito), TUDO o que valia continua — o
// limiar e também o modo, que não entra pela metade — e o log avisa.
func TestAjustar_PedidoInvalidoMantemOQueValia(t *testing.T) {
	rt, log, _ := runtimeComLog(t, fakeFocus{target: alvoProtegido})
	rt.Ajustar(Preferencias{HoldThreshold: 5 * time.Second, BeepEnabled: true, Literal: true})
	rt.aplicarAjuste()
	if got := rt.machine.Config().HoldThreshold; got != 700*time.Millisecond {
		t.Fatalf("limiar = %v, quero os 700ms de antes", got)
	}
	if rt.literal {
		t.Fatal("o Literal do pedido recusado ficou valendo: aplicado pela metade")
	}
	if !strings.Contains(log.String(), "recusou") {
		t.Fatalf("a recusa não foi para o log:\n%s", log.String())
	}
}
