package winruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/repeticao"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// O Polish no ditado longo (decisão 5 de docs/defeito-ditado-longo.md): o
// prazo do Polish e o watchdog da resolução crescem com o texto, pelo mesmo
// acréscimo, e são calculados sobre o MESMO texto — o que entra na
// resolução, já sem o laço de repetição do ASR. Aqui o núcleo é o de
// verdade, com o relógio falso; o Polish é o polidorFalso, com o relógio de
// verdade (o prazo dele é um ctx). Nada abre microfone nem rede.

// lacoDoIncidente é a frase que entrou doze vezes no campo em 01-out-2026.
const lacoDoIncidente = "e como cada um deles vai ser utilizado na hora de récu, "

// ditadoComLaco é um ditado longo com o laço no meio: o ASR devolve mais de
// 1000 caracteres, e a guarda deixa uns 450.
func ditadoComLaco() string {
	return "hoje a gente vai falar sobre os documentos que o cliente precisa mandar todo mês " +
		"para o escritório, começando pelo balancete de março, que ainda não chegou, " +
		"depois as notas fiscais de entrada e de saída, o extrato do banco, " +
		"a folha de pagamento com os encargos, e por fim o livro de apuração do ICMS, " +
		"que a equipe fiscal confere antes de fechar a guia, " +
		strings.Repeat(lacoDoIncidente, 12) +
		"e a reunião ficou para quinta-feira"
}

type bancadaLonga struct {
	*bancada
	asr      *asrFalso
	polish   *polishFalso
	segurar  chan struct{}
	posGuard string // o texto que entra na resolução
}

// novaBancadaLonga monta o runtime com o núcleo de verdade no relógio
// falso, o Polish ligado sobre um polidorFalso que fica preso até
// b.segurar fechar, e a fila de inserção para o teste ler.
func novaBancadaLonga(t *testing.T) *bancadaLonga {
	t.Helper()
	bruto := ditadoComLaco()
	r := repeticao.Colapsar(bruto)
	if !r.Colapsou() {
		t.Fatal("pré-condição: a guarda não viu o laço do ditado de teste")
	}
	// O teste só distingue os dois textos se os prazos deles diferirem, e
	// só prova o acréscimo se o texto sem o laço também tiver um.
	if dictation.PrazoDeResolucao(bruto) != dictation.TetoPrazoResolucao ||
		dictation.PrazoDeResolucao(r.Texto) >= dictation.TetoPrazoResolucao-time.Second ||
		dictation.PrazoDeResolucao(r.Texto) <= dictation.DefaultResolveTimeout+time.Second {
		t.Fatalf("pré-condição: bruto com %d caracteres (%s), sem o laço %d (%s)",
			len([]rune(bruto)), dictation.PrazoDeResolucao(bruto), len([]rune(r.Texto)), dictation.PrazoDeResolucao(r.Texto))
	}

	b := &bancadaLonga{bancada: novaBancada(t, true), segurar: make(chan struct{}), posGuard: r.Texto}
	b.asr = &asrFalso{texto: bruto, entrou: make(chan struct{}), liberar: make(chan struct{})}
	b.rt.asr = b.asr
	b.rt.UsarResolver(resolver.NewSmartResolver(nil), "mauro")
	b.polish = novoPolishFalso(passoPolish{segurar: b.segurar, texto: ditadoPolido})
	b.rt.UsarPolish(b.polish.abrir)
	b.rt.polidos = make(chan polidoPronto, 1)
	b.rt.polish, b.rt.provedorIA, b.rt.modeloIA = true, "openai", "modelo-x"
	b.rt.vaga, b.rt.insercoes = vagaLivre(), make(chan dictation.InsertEffect, 1)
	t.Cleanup(func() {
		select {
		case <-b.segurar:
		default:
			close(b.segurar)
		}
	})
	return b
}

// ateOPolish leva o ditado até a chamada do Polish começar e devolve o ctx
// dela e o instante em que a resolução começou (no relógio de verdade, o do
// prazo do Polish).
func (b *bancadaLonga) ateOPolish(t *testing.T) (dictation.AttemptID, context.Context, time.Time) {
	t.Helper()
	attempt := b.ditarAteOASR(t, b.asr, time.Minute)
	close(b.asr.liberar)
	var ev dictation.Event
	select {
	case ev = <-b.rt.pending:
	case <-time.After(2 * time.Second):
		t.Fatal("o resultado do ASR não voltou")
	}
	antes := time.Now()
	b.rt.aoReceberPendente(ev)
	select {
	case ctx := <-b.polish.entrou:
		return attempt, ctx, antes
	case <-time.After(2 * time.Second):
		t.Fatal("a chamada do Polish não começou")
		return 0, nil, time.Time{}
	}
}

// avancar anda o relógio do núcleo e devolve o timer que venceu, se algum.
func (b *bancadaLonga) avancar(d time.Duration) (dictation.TimerElapsedEvent, bool) {
	b.rt.clk.(*clock.Fake).Advance(d)
	select {
	case ev := <-b.rt.pending:
		te, ok := ev.(dictation.TimerElapsedEvent)
		if ok {
			b.rt.aoReceberPendente(ev)
		}
		return te, ok
	default:
		return dictation.TimerElapsedEvent{}, false
	}
}

// TestPolishLongo_OPrazoEODoTextoSemOLaco: o ctx do Polish vence no prazo
// do texto que entrou na resolução (sem o laço), e não no do texto do ASR
// como chegou.
func TestPolishLongo_OPrazoEODoTextoSemOLaco(t *testing.T) {
	b := novaBancadaLonga(t)
	_, ctx, antes := b.ateOPolish(t)

	want := prazoPolish(b.posGuard)
	limite, ok := ctx.Deadline()
	if !ok || limite.Before(antes.Add(want)) || limite.After(time.Now().Add(want)) {
		t.Fatalf("o ctx do Polish vence em %v (%v); quero o começo da resolução + %s (o texto sem o laço; com ele seria %s)",
			limite.Sub(antes), ok, want, prazoPolish(ditadoComLaco()))
	}
}

// TestPolishLongo_RespostaDepoisDos5sDeAntesEntra: a resposta do Polish que
// chega depois dos 5 s do watchdog antigo, mas dentro do novo, é inserida.
// Com o watchdog fixo, este ditado acabava SEM texto.
func TestPolishLongo_RespostaDepoisDos5sDeAntesEntra(t *testing.T) {
	b := novaBancadaLonga(t)
	attempt, _, _ := b.ateOPolish(t)

	prazo := dictation.PrazoDeResolucao(b.posGuard)
	if te, ok := b.avancar(dictation.DefaultResolveTimeout); ok {
		t.Fatalf("aos 5 s venceu %v: o watchdog ainda é o fixo", te.Kind)
	}
	if te, ok := b.avancar(prazo - dictation.DefaultResolveTimeout - time.Millisecond); ok {
		t.Fatalf("1 ms antes do prazo de %s venceu %v", prazo, te.Kind)
	}
	if got := b.rt.machine.State(); got != dictation.StateProcessing {
		t.Fatalf("State = %v, quero processing", got)
	}

	close(b.segurar)
	b.rt.dispatch(b.rt.concluirPolish(receberPolido(t, b.rt, 2*time.Second)))
	if got := b.rt.machine.State(); got != dictation.StateInserting {
		t.Fatalf("State = %v depois do Polish, quero inserting", got)
	}
	select {
	case e := <-b.rt.insercoes:
		if e.Attempt != attempt || e.Text != ditadoPolido {
			t.Fatalf("inserção = %+v, quero o texto do Polish da tentativa %d", e, attempt)
		}
	default:
		t.Fatal("nada foi para a inserção")
	}
	// O watchdog foi desarmado pela resposta: passar do prazo não muda nada.
	if te, ok := b.avancar(time.Second); ok && te.Kind == dictation.TimerResolveTimeout {
		t.Fatal("o watchdog da resolução venceu depois da resposta")
	}
}

// TestPolishLongo_WatchdogNoPrazoDoTextoSemOLaco: com o Polish preso, o
// watchdog vence exatamente no prazo do texto sem o laço; a resposta que
// chega depois é descartada pelo núcleo e não é inserida.
func TestPolishLongo_WatchdogNoPrazoDoTextoSemOLaco(t *testing.T) {
	b := novaBancadaLonga(t)
	b.ateOPolish(t)

	prazo := dictation.PrazoDeResolucao(b.posGuard)
	if te, ok := b.avancar(prazo - time.Millisecond); ok {
		t.Fatalf("1 ms antes do prazo de %s venceu %v", prazo, te.Kind)
	}
	te, ok := b.avancar(time.Millisecond)
	if !ok || te.Kind != dictation.TimerResolveTimeout {
		t.Fatalf("no prazo de %s, venceu %v (%v); quero o watchdog da resolução", prazo, te.Kind, ok)
	}
	if got := b.rt.machine.State(); got != dictation.StateError || b.rt.machine.CurrentAttempt() != dictation.NoAttempt {
		t.Fatalf("State = %v, tentativa %d; quero o timeout", got, b.rt.machine.CurrentAttempt())
	}

	close(b.segurar)
	b.rt.dispatch(b.rt.concluirPolish(receberPolido(t, b.rt, 2*time.Second)))
	select {
	case e := <-b.rt.insercoes:
		t.Fatalf("a resposta depois do watchdog foi inserida: %+v", e)
	default:
	}
	if got := b.rt.machine.State(); got != dictation.StateError {
		t.Fatalf("State = %v depois da resposta tardia, quero continuar em error", got)
	}
}
