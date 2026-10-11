package winruntime

import (
	"context"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
)

// Este arquivo guarda a transcrição que chega DEPOIS do prazo.
//
// No incidente de 01-out-2026 (docs/defeito-ditado-longo.md), dois ditados
// longos estouraram o prazo da transcrição e o texto chegou 8,8 s e 6,1 s
// depois. O núcleo já tinha encerrado a tentativa (timeout), descartou o
// resultado por ser de uma tentativa que não existe mais, e a fala se
// perdeu. O prazo agora acompanha o áudio (dictation.PrazoDeTranscricao),
// mas um servidor lento ou a CPU disputada ainda podem estourá-lo; quando
// isso acontece, o texto vai para a linha do histórico daquela tentativa.
//
// O que NÃO muda: o desfecho continua timeout, o núcleo não volta atrás, e
// nada é inserido nem vai para a área de transferência — o foco já pode
// estar noutro campo, ou noutro ditado. A garantia de voltar a idle continua
// sendo do núcleo (ADR-007, seção 6): tudo aqui é runtime.

// transcricaoEmVoo é o que o runtime sabe de uma transcrição que saiu para
// o ASR e ainda não voltou. Só a goroutine de Run lê e escreve.
type transcricaoEmVoo struct {
	// duracao é a do áudio enviado, para o duration_ms da linha tardia: a
	// medição da tentativa (audioDaTentativa) já foi consumida no timeout.
	duracao time.Duration
	// inicio é quando a transcrição saiu. Vira o created_at da linha, para
	// ela ficar no lugar do ditado, e não depois de um ditado mais novo.
	inicio time.Time
}

// registrarEmVoo anota a transcrição que está saindo. Só na goroutine de
// Run (transcrever).
func (rt *Runtime) registrarEmVoo(attempt dictation.AttemptID, buf asr.AudioBuffer) {
	if rt.emVoo == nil {
		rt.emVoo = make(map[dictation.AttemptID]transcricaoEmVoo)
	}
	rt.emVoo[attempt] = transcricaoEmVoo{
		duracao: duracaoDe(buf.PCM, buf.SampleRateHz),
		inicio:  time.Now(),
	}
}

// aoReceberPendente é o que Run faz com cada evento que chega por
// rt.pending: timers e respostas de outra goroutine.
//
// A transcrição de uma tentativa que o núcleo já encerrou não vai ao núcleo
// (ele a descartaria): vai para guardarTardio. "Já encerrou" é a tentativa
// corrente ser outra, ou nenhuma. Uma transcrição só sai para o ASR com o
// núcleo esperando por ela, e o único jeito de a tentativa acabar sem ela é
// o watchdog — então é tardia.
func (rt *Runtime) aoReceberPendente(ev dictation.Event) {
	switch e := ev.(type) {
	case dictation.TimerElapsedEvent:
		// Limpeza do timer disparado. Só acontece aqui — na goroutine de
		// Run — nunca dentro do callback do clock.System.AfterFunc (que
		// roda em goroutine própria): tocar rt.timers de duas goroutines
		// seria uma corrida de dados. Ver o comentário de startTimer.
		delete(rt.timers, timerKey{Attempt: e.Attempt, Kind: e.Kind})
	case dictation.TranscribeResultEvent:
		// Depois do ASR e antes da resolução: o laço de repetição sai aqui,
		// e vale tanto para o resultado a tempo quanto para o tardio.
		e = rt.guardaDeRepeticao(e)
		ev = e
		voo, emVoo := rt.emVoo[e.Attempt]
		delete(rt.emVoo, e.Attempt)
		if emVoo && e.Attempt != rt.machine.CurrentAttempt() {
			rt.guardarTardio(e, voo)
			return
		}
	}
	rt.dispatch(ev)
}

// guardarTardio grava na linha do histórico da tentativa a transcrição que
// chegou depois do prazo: o bruto, e o final pelo Smart. Roda na goroutine
// de Run.
//
// Smart, e nunca Polish: o Polish é uma chamada de rede com prazo próprio,
// para um texto que ninguém está esperando. Sem domínio do app: o app em
// foco do ditado já não é conhecido, e domínio desconhecido é o padrão
// seguro (as entradas restritas do dicionário não se aplicam).
//
// O log leva tamanhos e tempos, nunca o texto (CLAUDE.md, "Safety rules").
// A pílula só aparece com a máquina parada: um ditado em curso tem a pílula
// dele, e a linha no histórico já basta.
func (rt *Runtime) guardarTardio(e dictation.TranscribeResultEvent, voo transcricaoEmVoo) {
	espera := time.Since(voo.inicio)
	texto := strings.TrimSpace(e.Result.Text)
	// O raw_text é o texto do ASR como chegou, com um laço que a guarda
	// tenha tirado (repeticao.go); o final parte do texto sem ele.
	bruto := strings.TrimSpace(rt.brutoParaOHistorico(e.Attempt, e.Result.Text))
	if e.Reason != dictation.ErrorReasonNone || texto == "" {
		rt.log.Warn("winruntime: transcrição chegou depois do prazo, sem texto",
			"attempt", e.Attempt, "reason", e.Reason.String(), "espera", espera)
		return
	}
	if rt.hist == nil {
		// Sem histórico ligado não há onde guardar, e a pílula não pode
		// dizer que guardou.
		rt.log.Warn("winruntime: transcrição chegou depois do prazo e não há histórico para guardá-la",
			"attempt", e.Attempt, "runas", len([]rune(bruto)), "espera", espera)
		return
	}

	final, modo := rt.smartTardio(texto)
	var processamento time.Duration
	if m := rt.asrMedido.Load(); m != nil && m.attempt == e.Attempt {
		rt.asrMedido.CompareAndSwap(m, nil)
		processamento = m.processamento
	}
	linha := rt.novaLinha(bruto, final, modo, "error", voo.duracao, processamento, voo.inicio)
	if err := rt.hist.Put(linha); err != nil {
		rt.log.Warn("winruntime: transcrição chegou depois do prazo e não consegui gravá-la no histórico",
			"attempt", e.Attempt, "err", err)
		return
	}
	rt.log.Info("winruntime: transcrição chegou depois do prazo; o texto ficou no histórico, sem inserir",
		"attempt", e.Attempt, "runas", len([]rune(bruto)), "espera", espera,
		"duracao_audio", voo.duracao, "modo", modo)

	if rt.machine.CurrentAttempt() == dictation.NoAttempt && rt.overlay != nil {
		rt.log.Info("winruntime: overlay", "attempt", e.Attempt, "fase", "texto_no_historico")
		rt.overlay.SetPhase(overlay.FaseTextoNoHistorico)
	}
}

// smartTardio resolve o texto tardio sem Polish e sem domínio. Falha do
// Smart nunca perde o texto: fica o bruto, como em resolver.
func (rt *Runtime) smartTardio(bruto string) (final, modo string) {
	if rt.resolucao == nil || rt.literal {
		return bruto, "literal"
	}
	out, err := rt.resolucao.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:    rt.usuario,
		ASRResult: asr.ASRResult{Text: bruto, Language: "pt-BR"},
	})
	if err != nil {
		rt.log.Warn("winruntime: resolucao do texto tardio falhou, guardando o texto do ASR", "err", err)
		return bruto, "smart"
	}
	return out.FinalText, "smart"
}
