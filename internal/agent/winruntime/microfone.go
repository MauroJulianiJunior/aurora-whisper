package winruntime

import (
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/audio"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// No padrão, o microfone só liga no keydown (ADR-015; no "sempre pronto"
// ele já entrega áudio, e nada é retido). O núcleo não sabe disso e não
// muda: quando o hold vence (700 ms), ele pede a pílula "Gravando" e o
// bipe. Se o microfone ainda não entregou áudio nesse instante — um Start
// lento, um driver frio —, os dois mentiriam: quem espera o bipe para
// falar perderia o começo sem aviso.
//
// Por isso o runtime SEGURA os dois até a Session dizer que o microfone já
// entrega áudio para a tentativa (ajuste A da contestação da ADR-015). A
// liberação vem com o primeiro frame de áudio que chega ao laço de Run.
// Se a tecla subir antes, os dois não saem: o momento passou.

// sinalRetido é o "gravando" (pílula e bipe) que o núcleo pediu e o
// runtime ainda não mostrou.
type sinalRetido struct {
	attempt dictation.AttemptID
	overlay bool
	bip     bool
	desde   time.Time
}

func (r sinalRetido) ativo() bool { return r.overlay || r.bip }

// sinalDaTentativa é o que aconteceu com o "gravando" de uma tentativa,
// para a linha "audio: janela do microfone".
type sinalDaTentativa struct {
	attempt    dictation.AttemptID
	esperouMic bool
	espera     time.Duration
	saiu       bool
}

// agora é o relógio do runtime; sem relógio (dublês de teste que não o
// montam), o do sistema.
func (rt *Runtime) agora() time.Time {
	if rt.clk == nil {
		return time.Now()
	}
	return rt.clk.Now()
}

// microfonePronto diz se o microfone já entrega áudio para a tentativa.
// Sem capturador, não há o que esperar.
func (rt *Runtime) microfonePronto(id dictation.AttemptID) bool {
	if rt.audio == nil {
		return true
	}
	return rt.audio.MicrofoneEntregando(id)
}

// reterGravando decide se um ShowOverlayEffect sai agora. Devolve true
// quando o efeito ficou retido.
//
// Só a fase Recording espera o microfone. Qualquer outra fase da MESMA
// tentativa encerra a espera: a tecla subiu (Processing) ou a tentativa
// acabou — dizer "gravando" depois disso seria mentir de outro jeito.
func (rt *Runtime) reterGravando(e dictation.ShowOverlayEffect) bool {
	if e.Phase != dictation.OverlayRecording {
		if rt.retido.ativo() && rt.retido.attempt == e.Attempt {
			rt.descartarRetido()
		}
		return false
	}
	if rt.microfonePronto(e.Attempt) {
		rt.sinal = sinalDaTentativa{attempt: e.Attempt, saiu: true}
		return false
	}
	rt.retido = sinalRetido{attempt: e.Attempt, overlay: true, desde: rt.agora()}
	rt.sinal = sinalDaTentativa{attempt: e.Attempt, esperouMic: true}
	rt.log.Info("winruntime: o bipe e a pílula esperam o microfone entregar áudio", "attempt", e.Attempt)
	return true
}

// reterBip decide se um PlayBeepEffect sai agora. Devolve true quando o
// bipe ficou retido.
//
// O núcleo pede o bipe no mesmo lote do ShowOverlayEffect{Recording}, logo
// depois dele. São três casos: a pílula foi retida, e o bipe vai junto; o
// microfone entrega, e o bipe sai; ou o microfone entregava quando a pílula
// foi decidida e parou de entregar (falhou) antes do bipe — aí a pílula sai
// e o bipe fica retido sozinho, porque "pode falar" não se diz a um
// microfone que acabou de morrer. O terceiro é raro, e por isso tem teste
// próprio (TestGravando_BipeRetidoSemAPilula).
func (rt *Runtime) reterBip(id dictation.AttemptID) bool {
	if rt.retido.ativo() && rt.retido.attempt == id {
		rt.retido.bip = true
		return true
	}
	if rt.microfonePronto(id) {
		return false
	}
	rt.retido = sinalRetido{attempt: id, bip: true, desde: rt.agora()}
	rt.sinal = sinalDaTentativa{attempt: id, esperouMic: true}
	return true
}

// liberarGravandoSePronto solta o "gravando" retido se o microfone já
// entrega áudio. Só na goroutine de Run.
//
// Não confere se a tentativa ainda está em "gravando": um retido ativo já
// implica isso, e conferir seria um ramo que nenhum caminho alcança. O
// núcleo só sai de Recording emitindo um ShowOverlayEffect de outra fase —
// no keyup e no limite de gravação (TestMachine_KeyUp_SaiDeGravandoNaHora e
// TestMachine_MaxRecording_TambemSaiDeGravando) — e reterGravando descarta
// o retido nesse mesmo lote de efeitos, na goroutine que também trata os
// frames: nenhum frame chega entre a saída de "gravando" e o descarte. Quem
// garante isso é o descarte, e TestGravando_TeclaSobeAntesDoMicrofone o
// cobre.
func (rt *Runtime) liberarGravandoSePronto() {
	r := rt.retido
	if !r.ativo() || !rt.microfonePronto(r.attempt) {
		return
	}
	rt.retido = sinalRetido{}

	var efeitos []dictation.Effect
	if r.overlay {
		efeitos = append(efeitos, dictation.ShowOverlayEffect{Attempt: r.attempt, Phase: dictation.OverlayRecording})
	}
	if r.bip {
		efeitos = append(efeitos, dictation.PlayBeepEffect{Attempt: r.attempt})
	}
	rt.apply(efeitos)

	espera := rt.agora().Sub(r.desde)
	rt.sinal = sinalDaTentativa{attempt: r.attempt, esperouMic: true, espera: espera, saiu: true}
	rt.log.Warn("winruntime: o microfone demorou; o bipe e a pílula esperaram por ele",
		"attempt", r.attempt, "espera_ms", espera.Milliseconds())
}

// descartarRetido joga fora o "gravando" retido: a tentativa passou dele
// sem o microfone ter entregue áudio.
func (rt *Runtime) descartarRetido() {
	r := rt.retido
	rt.retido = sinalRetido{}
	espera := rt.agora().Sub(r.desde)
	rt.sinal = sinalDaTentativa{attempt: r.attempt, esperouMic: true, espera: espera}
	rt.log.Warn("winruntime: a tecla subiu antes de o microfone entregar áudio; o bipe e a pílula não saíram",
		"attempt", r.attempt, "espera_ms", espera.Milliseconds())
}

// aoReceberFrame trata um frame de áudio no laço de Run: solta o
// "gravando" retido, se houver, e alimenta a onda.
//
// Best-effort (mesmo espírito de audio.Session.Frames() e de
// overlay.PushAmplitude, ambos documentados como best-effort): alimentar
// a onda do overlay nunca deve atrasar o consumo de hookEvents/pending,
// por isso PushAmplitude só grava e acorda — nunca bloqueia neste laço.
// Um frame perdido não prende o "gravando": o microfone ligado manda
// outro a cada ~10 ms.
//
// A onda só anda com o microfone entregando para o ditado em curso. No
// sempre pronto, uma leitura que falha no meio do ditado faz o microfone
// reabrir sozinho e os frames voltarem — mas o áudio deles não é do ditado,
// que o Stop vai devolver com erro (audio.Session, M1). Reagindo à voz, a
// onda diria que grava o que já se perdeu; parada, fica como no sob
// demanda.
func (rt *Runtime) aoReceberFrame(f audio.Frame) {
	rt.liberarGravandoSePronto()
	if rt.microfonePronto(rt.machine.CurrentAttempt()) {
		rt.overlay.PushAmplitude(f.RMS)
	}
}

// tocarBip toca o bipe de confirmação (AC-006). PlayBeepEffect nunca
// espera resposta (effects.go) — uma falha aqui é só logada, nunca
// propagada ao núcleo.
func (rt *Runtime) tocarBip(id dictation.AttemptID) {
	bipar := rt.bipar
	if bipar == nil {
		bipar = beep
	}
	if err := bipar(); err != nil {
		rt.log.Warn("winruntime: beep falhou", "attempt", id, "err", err)
	}
}

// registrarJanela escreve a linha "audio: janela do microfone" da
// tentativa e os alertas que ela pede (ADR-015).
//
// Só números, durações e booleanos: nenhuma amostra, nenhum texto, nenhum
// contexto de janela (CLAUDE.md, "Safety rules"; 11-observability.md).
func (rt *Runtime) registrarJanela(attempt dictation.AttemptID, j audio.Janela) {
	if j.Modo == "" {
		return // o capturador não mediu (dublê de teste)
	}
	ms := func(d time.Duration) int64 { return d.Milliseconds() }
	args := []any{
		"attempt", attempt,
		"modo", j.Modo,
		"ja_ligado", j.JaLigado,
		"reabriu", j.Reabriu,
		"reabriu_por_falha", j.ReabriuPorFalha,
		"ligar_levou_ms", ms(j.LigarLevou),
		"marca_ate_primeiro_pacote_ms", ms(j.MarcaAtePrimeiroPacote),
		"inicio_rel_marca_ms", ms(j.InicioRelMarca),
		"pre_roll_entregue_ms", ms(j.PreRollEntregue),
		"perdeu_inicio_ms", ms(j.PerdeuInicio),
		"esperou_mic_ms", ms(j.EsperouMicrofone),
		"faltou_no_fim_ms", ms(j.FaltouNoFim),
		"cauda_esperou_ms", ms(j.CaudaEsperou),
		"fala_no_inicio", j.FalaNoInicio,
		"pacote_anterior_ao_estacionamento", j.PacoteAnteriorAoEstacionamento,
		"erro_timestamp", j.ErroTimestamp,
		"posicao_primeiro_pacote", j.PosicaoPrimeiroPacote,
		"desligou_antes_do_corte", j.DesligouAntesDoCorte,
		"aberturas", j.Aberturas,
		"fechamentos", j.Fechamentos,
	}
	if rt.sinal.attempt == attempt {
		args = append(args,
			"bip_esperou_mic", rt.sinal.esperouMic,
			"bip_espera_ms", ms(rt.sinal.espera),
			"bip_saiu", rt.sinal.saiu)
	}
	rt.log.Info("audio: janela do microfone", args...)

	sobDemanda := j.Modo == audio.ModoSobDemanda
	if j.PacoteAnteriorAoEstacionamento || (sobDemanda && j.PreRollEntregue > 0) {
		rt.log.Warn("audio: áudio de antes desta tentativa entrou no buffer",
			"attempt", attempt,
			"pacote_anterior_ao_estacionamento", j.PacoteAnteriorAoEstacionamento,
			"pre_roll_entregue_ms", ms(j.PreRollEntregue))
	}
	if j.DesligouAntesDoCorte {
		rt.log.Warn("audio: o microfone desligou antes do corte; o fim da fala pode ter se perdido", "attempt", attempt)
	}
	if j.ReabriuPorFalha {
		rt.log.Warn("audio: o Start do microfone falhou; ligou reabrindo o microfone padrão",
			"attempt", attempt,
			"ligar_levou_ms", ms(j.LigarLevou))
	}
	if j.FaltouNoFim > 0 {
		rt.log.Warn("audio: o áudio acabou antes de a tecla subir; o fim da fala pode ter ficado de fora",
			"attempt", attempt,
			"faltou_no_fim_ms", ms(j.FaltouNoFim),
			"esperou_mic_ms", ms(j.EsperouMicrofone))
	}
	if j.PerdeuInicio > 0 {
		rt.log.Warn("audio: o ditado passou do que o buffer guarda; o começo da fala ficou de fora",
			"attempt", attempt,
			"perdeu_inicio_ms", ms(j.PerdeuInicio))
	}
	// Com o começo perdido no buffer, o início tardio e a fala no início
	// são efeito disso, e não do microfone: o alerta acima já diz o que houve.
	if sobDemanda && !j.JaLigado && j.PerdeuInicio == 0 && (j.InicioRelMarca > audio.LimiteDeInicio || j.FalaNoInicio) {
		rt.log.Warn("audio: o microfone abriu depois de a fala começar",
			"attempt", attempt,
			"inicio_rel_marca_ms", ms(j.InicioRelMarca),
			"limite_ms", ms(audio.LimiteDeInicio),
			"fala_no_inicio", j.FalaNoInicio)
	}
}
