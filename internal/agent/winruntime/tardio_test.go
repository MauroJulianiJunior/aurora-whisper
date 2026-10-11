package winruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// A transcrição que chega depois do prazo vai para a linha do histórico da
// tentativa, sem inserir nada (incidente de 01-out-2026,
// docs/defeito-ditado-longo.md). Tudo com relógio falso, ASR falso e
// histórico falso: nada aqui abre microfone, sobe servidor ou insere texto.

// smartMarcado é um Smart que marca o que resolveu, e guarda o domínio que
// recebeu: prova que o texto tardio passou pelo Smart, e sem domínio.
type smartMarcado struct {
	chamadas int
	dominios []*string
}

func (s *smartMarcado) Resolve(_ context.Context, req resolver.ResolveRequest) (resolver.ResolveResult, error) {
	s.chamadas++
	s.dominios = append(s.dominios, req.Domain)
	return resolver.ResolveResult{FinalText: "smart(" + req.ASRResult.Text + ")"}, nil
}

const textoTardio = "e como cada um deles vai ser utilizado na hora de recuperar"

// bancadaTardia é a novaBancada com ASR preso, histórico, Smart, e o Polish
// escolhido — que a linha tardia NÃO pode usar.
func bancadaTardia(t *testing.T) (*bancada, *asrFalso, *historicoFalso, *smartMarcado) {
	t.Helper()
	b := novaBancada(t, true)
	prov := &asrFalso{
		texto:         textoTardio,
		entrou:        make(chan struct{}),
		liberar:       make(chan struct{}),
		processamento: 16080 * time.Millisecond,
	}
	b.rt.asr = prov
	hist := &historicoFalso{}
	b.rt.UsarHistorico(hist, provedorDeTeste, modeloDeTeste, "mauro")
	smart := &smartMarcado{}
	b.rt.UsarResolver(smart, "mauro")
	b.rt.polish = true
	b.rt.UsarPolish(func(string, string, func(int, int)) (Polidor, string) {
		t.Error("o texto tardio foi ao Polish: é uma chamada de rede para um texto que ninguém espera")
		return nil, ""
	})
	return b, prov, hist, smart
}

// ditarAteOASR segura, solta e entrega a captura de duracao de áudio: a
// transcrição sai para o ASR, que fica preso até prov.liberar.
func (b *bancada) ditarAteOASR(t *testing.T, prov *asrFalso, duracao time.Duration) dictation.AttemptID {
	t.Helper()
	b.segurar(t)
	attempt := b.rt.machine.CurrentAttempt()
	b.rt.handleRaw(dictation.RawKeyEvent{Down: false})
	// O Stop do microfone falso volta pela goroutine de pedirCaptura; aqui
	// entra a captura do tamanho que o teste quer.
	select {
	case <-b.rt.capturas:
	case <-time.After(2 * time.Second):
		t.Fatal("a captura não voltou")
	}
	amostras := int(duracao / time.Second * 16000)
	b.rt.dispatch(b.rt.concluirCaptura(capturaPronta{
		attempt: attempt,
		buf:     audio.AudioBuffer{Samples: make([]int16, amostras), SampleRate: 16000},
		vad:     dictation.VADSummary{HasSpeech: true, SpeechDuration: duracao},
	}))
	select {
	case <-prov.entrou:
	case <-time.After(2 * time.Second):
		t.Fatal("o ASR nunca foi chamado")
	}
	return attempt
}

// estourarOPrazo avança o relógio até o prazo da transcrição vencer e
// entrega o timer ao runtime, como Run faria.
func (b *bancada) estourarOPrazo(t *testing.T, duracao time.Duration) {
	t.Helper()
	b.rt.clk.(*clock.Fake).Advance(dictation.PrazoDeTranscricao(duracao))
	select {
	case ev := <-b.rt.pending:
		if te, ok := ev.(dictation.TimerElapsedEvent); !ok || te.Kind != dictation.TimerTranscribeTimeout {
			t.Fatalf("evento = %#v, quero o timer da transcrição", ev)
		}
		b.rt.aoReceberPendente(ev)
	default:
		t.Fatal("o prazo da transcrição não venceu")
	}
	if got := b.rt.machine.State(); got != dictation.StateError {
		t.Fatalf("State = %v depois do prazo, quero StateError (timeout)", got)
	}
}

// receberTardio solta o ASR e entrega o resultado ao runtime, como Run.
func (b *bancada) receberTardio(t *testing.T, prov *asrFalso) {
	t.Helper()
	close(prov.liberar)
	select {
	case ev := <-b.rt.pending:
		b.rt.aoReceberPendente(ev)
	case <-time.After(2 * time.Second):
		t.Fatal("o resultado do ASR não voltou")
	}
}

// TestTardio_TextoQueChegaDepoisDoPrazoFicaNoHistorico é o ditado de 37 s do
// incidente: o ASR levou mais que o prazo, o núcleo encerrou a tentativa por
// timeout, e o texto chegou depois. Antes, ele era descartado em silêncio.
func TestTardio_TextoQueChegaDepoisDoPrazoFicaNoHistorico(t *testing.T) {
	b, prov, hist, smart := bancadaTardia(t)
	const audioDoDitado = 37 * time.Second
	b.ditarAteOASR(t, prov, audioDoDitado)
	antes := time.Now()

	b.estourarOPrazo(t, audioDoDitado)
	if len(hist.linhas) != 0 {
		t.Fatalf("o timeout gravou %d linha(s) sem texto", len(hist.linhas))
	}
	if got := b.pilula.ultima(t); got != dictation.OverlayError {
		t.Fatalf("pílula no timeout = %v, quero a do núcleo (erro)", got)
	}

	b.receberTardio(t, prov)

	l := hist.unica(t)
	if l.Status != "error" {
		t.Errorf("Status = %q, quero error: o desfecho continua timeout", l.Status)
	}
	if l.RawText != textoTardio {
		t.Errorf("RawText = %q, quero o do ASR", l.RawText)
	}
	if l.FinalText != "smart("+textoTardio+")" || l.Mode != "smart" {
		t.Errorf("FinalText = %q, Mode = %q; quero o Smart, e modo smart mesmo com o Polish escolhido", l.FinalText, l.Mode)
	}
	if smart.chamadas != 1 || smart.dominios[0] != nil {
		t.Errorf("Smart chamado %d vez(es), domínios %v; quero uma, sem domínio (o app do ditado não é mais conhecido)", smart.chamadas, smart.dominios)
	}
	if l.DurationMs == nil || *l.DurationMs != 37000 {
		t.Errorf("DurationMs = %v, quero 37000: a duração do áudio daquela tentativa", derefInt(l.DurationMs))
	}
	if l.ProcessingMs == nil || *l.ProcessingMs != 16080 {
		t.Errorf("ProcessingMs = %v, quero 16080: o que o provider mediu", derefInt(l.ProcessingMs))
	}
	if l.CreatedAt.After(antes) {
		t.Errorf("CreatedAt = %v, depois de a transcrição sair (%v): a linha ficaria fora de ordem", l.CreatedAt, antes)
	}
	if got := b.pilula.ultima(t); got != overlay.FaseTextoNoHistorico {
		t.Errorf("pílula = %v, quero FaseTextoNoHistorico", got)
	}
	// Nada é inserido: o núcleo não recebeu o resultado.
	if got := b.rt.machine.State(); got != dictation.StateError || b.rt.machine.CurrentAttempt() != dictation.NoAttempt {
		t.Errorf("a máquina saiu do timeout (State = %v, tentativa %d): o texto tardio chegou ao núcleo", got, b.rt.machine.CurrentAttempt())
	}
	log := b.log.String()
	if strings.Contains(log, "inserção anterior ainda em curso") {
		t.Error("o runtime tentou inserir o texto tardio")
	}
	if strings.Contains(log, textoTardio) || strings.Contains(log, "recuperar") {
		t.Errorf("o texto ditado foi para o log:\n%s", log)
	}
	if !strings.Contains(log, "o texto ficou no histórico") || !strings.Contains(log, "runas=59") {
		t.Errorf("a linha de log do texto tardio não saiu, ou sem o tamanho:\n%s", log)
	}
	if len(b.rt.emVoo) != 0 {
		t.Errorf("emVoo = %v depois de o resultado chegar, quero vazio", b.rt.emVoo)
	}
}

// TestTardio_ChegaDuranteOutroDitado: a pessoa já começou outro ditado
// quando o texto do anterior chega. A linha é a da tentativa ANTERIOR, a
// pílula do ditado em curso não é atropelada, e o ditado em curso segue com
// o bruto dele.
func TestTardio_ChegaDuranteOutroDitado(t *testing.T) {
	b, prov, hist, _ := bancadaTardia(t)
	const audioDoDitado = 20 * time.Second
	primeiro := b.ditarAteOASR(t, prov, audioDoDitado)
	b.estourarOPrazo(t, audioDoDitado)

	// O segundo ditado começa e está gravando quando o primeiro volta.
	prov2 := &asrFalso{texto: "segundo ditado", entrou: make(chan struct{})}
	b.rt.asr = prov2
	b.segurar(t)
	segundo := b.rt.machine.CurrentAttempt()
	if segundo == primeiro {
		t.Fatal("pré-condição: o segundo ditado tem de ser outra tentativa")
	}
	fasesAntes := len(b.pilula.fases)

	b.receberTardio(t, prov)

	l := hist.unica(t)
	if l.RawText != textoTardio {
		t.Errorf("RawText = %q, quero o do PRIMEIRO ditado", l.RawText)
	}
	if len(b.pilula.fases) != fasesAntes {
		t.Errorf("a pílula do ditado em curso foi trocada: %v", b.pilula.fases[fasesAntes:])
	}
	if b.rt.machine.CurrentAttempt() != segundo || b.rt.machine.State() != dictation.StateRecording {
		t.Fatalf("o ditado em curso foi afetado: tentativa %d, State %v", b.rt.machine.CurrentAttempt(), b.rt.machine.State())
	}

	// O segundo termina e grava a linha dele, com o bruto dele. Em Smart,
	// para a resolução voltar no mesmo laço (o Polish é outro teste).
	b.rt.polish = false
	b.rt.handleRaw(dictation.RawKeyEvent{Down: false})
	<-b.rt.capturas
	b.rt.dispatch(b.rt.concluirCaptura(capturaPronta{
		attempt: segundo,
		buf:     audio.AudioBuffer{Samples: make([]int16, 16000), SampleRate: 16000},
		vad:     dictation.VADSummary{HasSpeech: true},
	}))
	select {
	case ev := <-b.rt.pending:
		b.rt.aoReceberPendente(ev)
	case <-time.After(2 * time.Second):
		t.Fatal("a transcrição do segundo não voltou")
	}
	// A inserção não responde (a bancada não tem inserter): o watchdog dela
	// encerra com o texto final, que vai para a linha.
	b.rt.clk.(*clock.Fake).Advance(dictation.DefaultInsertTimeout)
	for {
		select {
		case ev := <-b.rt.pending:
			b.rt.aoReceberPendente(ev)
			continue
		default:
		}
		break
	}
	if len(hist.linhas) != 2 {
		t.Fatalf("linhas = %d, quero 2 (uma por ditado)", len(hist.linhas))
	}
	if got := hist.linhas[1].RawText; got != "segundo ditado" {
		t.Errorf("bruto do segundo ditado = %q: o texto tardio contaminou a tentativa seguinte", got)
	}
}

// TestTardio_FalhaOuVazioNaoGravaNemAvisa: o ASR que volta tarde com erro
// não tem texto para guardar, e a pílula não pode dizer que guardou.
func TestTardio_FalhaOuVazioNaoGravaNemAvisa(t *testing.T) {
	for _, c := range []struct {
		nome  string
		texto string
		err   error
	}{
		{"erro", "", errors.New("servidor caiu")},
		{"so_espaco", "   ", nil},
	} {
		t.Run(c.nome, func(t *testing.T) {
			b, prov, hist, _ := bancadaTardia(t)
			prov.texto, prov.err = c.texto, c.err
			b.ditarAteOASR(t, prov, 30*time.Second)
			b.estourarOPrazo(t, 30*time.Second)
			fases := len(b.pilula.fases)

			b.receberTardio(t, prov)

			if len(hist.linhas) != 0 {
				t.Errorf("gravou %d linha(s) sem texto", len(hist.linhas))
			}
			if len(b.pilula.fases) != fases {
				t.Errorf("a pílula mudou para %v sem texto guardado", b.pilula.fases[fases:])
			}
			if !strings.Contains(b.log.String(), "sem texto") {
				t.Errorf("o resultado tardio sem texto não foi registrado no log:\n%s", b.log.String())
			}
		})
	}
}

// TestTardio_SemHistoricoNaoDizQueGuardou: sem histórico ligado, a pílula
// não pode afirmar que o texto ficou nele.
func TestTardio_SemHistoricoNaoDizQueGuardou(t *testing.T) {
	b, prov, _, _ := bancadaTardia(t)
	b.rt.hist = nil
	b.ditarAteOASR(t, prov, 30*time.Second)
	b.estourarOPrazo(t, 30*time.Second)
	fases := len(b.pilula.fases)

	b.receberTardio(t, prov)

	if len(b.pilula.fases) != fases {
		t.Errorf("a pílula mudou para %v sem histórico", b.pilula.fases[fases:])
	}
	if strings.Contains(b.log.String(), textoTardio) {
		t.Error("o texto ditado foi para o log")
	}
}

// TestTardio_LiteralGuardaOBruto: no modo Literal o texto tardio não passa
// pelo Smart, como o ditado normal.
func TestTardio_LiteralGuardaOBruto(t *testing.T) {
	b, prov, hist, smart := bancadaTardia(t)
	b.rt.literal = true
	b.ditarAteOASR(t, prov, 30*time.Second)
	b.estourarOPrazo(t, 30*time.Second)
	b.receberTardio(t, prov)

	l := hist.unica(t)
	if l.FinalText != textoTardio || l.Mode != "literal" || smart.chamadas != 0 {
		t.Errorf("FinalText = %q, Mode = %q, Smart chamado %d vez(es); quero o bruto, literal, nenhuma", l.FinalText, l.Mode, smart.chamadas)
	}
}

// TestTardio_DentroDoPrazoSegueONucleo: o resultado que chega a tempo é o
// caminho de sempre, e não deixa a tentativa anotada como em voo.
func TestTardio_DentroDoPrazoSegueONucleo(t *testing.T) {
	b, prov, hist, _ := bancadaTardia(t)
	b.rt.polish = false
	attempt := b.ditarAteOASR(t, prov, 30*time.Second)
	b.receberTardio(t, prov)

	if b.rt.machine.CurrentAttempt() != attempt || b.rt.machine.State() != dictation.StateInserting {
		t.Fatalf("State = %v, quero StateInserting: o resultado a tempo vai ao núcleo", b.rt.machine.State())
	}
	if len(hist.linhas) != 0 {
		t.Errorf("gravou %d linha(s) antes de a tentativa acabar", len(hist.linhas))
	}
	if len(b.rt.emVoo) != 0 {
		t.Errorf("emVoo = %v, quero vazio", b.rt.emVoo)
	}
}

// TestRecordHistory_MedicaoDeOutraTentativaFicaParaElaMesma: a linha de uma
// tentativa não consome a medição de outra. É a da transcrição que estourou
// o prazo e ainda vai chegar: a linha tardia dela precisa do número.
func TestRecordHistory_MedicaoDeOutraTentativaFicaParaElaMesma(t *testing.T) {
	rt := runtimeMinimo(&asrFalso{texto: "ditado oito"})
	rt.UsarHistorico(&historicoFalso{}, provedorDeTeste, modeloDeTeste, "mauro")
	rt.resolver(dictation.ResolveEffect{Attempt: 8, ASRResult: dictation.ASRResultDTO{Text: "ditado oito"}})
	rt.asrMedido.Store(&medicaoASR{attempt: 7, processamento: 3 * time.Second})

	rt.recordHistory(dictation.RecordHistoryEffect{Attempt: 8, Outcome: dictation.OutcomeSuccess, Text: "ditado oito"})

	if m := rt.asrMedido.Load(); m == nil || m.attempt != 7 {
		t.Errorf("asrMedido = %+v, quero a medição da tentativa 7 intacta", m)
	}
}
