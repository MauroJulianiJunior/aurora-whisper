package winruntime

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

// A gravação solta no runtime (ADR-025, D2). O núcleo decide tudo (quando
// liga, o que para, o que descarta); aqui só se liga o que está fora dele:
//
//   - o Esc e o hook do mouse, no hook (teclado.GravacaoSolta), que existem
//     só durante a gravação solta;
//   - o clique, que volta do hook por teclado.Cliques e vai ao núcleo como
//     CliqueEvent (Run);
//   - o campo relido na hora de escrever (ResolveEffect.ReleFoco e
//     InsertEffect.ReleFoco), porque depois de um clique ou de um minuto
//     ditando o cursor já não está onde estava no começo.

// aplicarGravacaoSolta liga ou desliga o Esc e o hook do mouse, e registra a
// mudança. O log diz só como a gravação solta terminou (toque, clique,
// limite, segurou, descartada), nunca a posição do clique, a janela ou o
// texto (ADR-025, F).
func (rt *Runtime) aplicarGravacaoSolta(e dictation.GravacaoSoltaEffect) {
	args := []any{"attempt", e.Attempt, "ligada", e.Ligada}
	if !e.Ligada {
		args = append(args, "fim", e.Fim.String())
	}
	rt.log.Info("winruntime: gravação solta", args...)
	if rt.hook == nil {
		return
	}
	if err := rt.hook.GravacaoSolta(e.Ligada); err != nil {
		// A gravação segue: o toque, o Esc e o limite não dependem do mouse.
		rt.log.Warn("winruntime: o hook do mouse falhou; o clique não parou esta gravação solta",
			"attempt", e.Attempt, "err", err)
	}
}

// recusarCampoProtegido relê o campo em foco antes de escrever o texto da
// gravação solta (ADR-025, C.2). Se ele for protegido, o texto não é escrito:
// a resposta é a de uma inserção que falhou, e o núcleo grava o texto no
// histórico como grava o de toda inserção que falha. O motivo no histórico é o
// de sempre (status "error"), sem dizer que era um campo de senha: isso seria
// metadado sobre o uso de campo de senha (AC-027). Só a pílula diz, na hora
// (overlay.FaseCampoProtegidoNoHistorico).
//
// Fora da gravação solta não relê nada: o ditado de sempre já foi bloqueado no
// aperto se o campo era protegido (AC-036), e escreve onde a pessoa segura a
// tecla.
func (rt *Runtime) recusarCampoProtegido(e dictation.InsertEffect) (dictation.Event, bool) {
	if !e.ReleFoco {
		return nil, false
	}
	if alvo, _ := rt.lerFoco(); !alvo.Protected {
		return nil, false
	}
	rt.protegidoNoFim = e.Attempt
	rt.log.Info("winruntime: a gravação solta terminou num campo protegido; o texto fica no histórico",
		"attempt", e.Attempt)
	return dictation.InsertResultEvent{
		Attempt: e.Attempt,
		Reason:  dictation.ErrorReasonInsertFailed,
		Result:  dictation.InsertResultDTO{Success: false},
	}, true
}
