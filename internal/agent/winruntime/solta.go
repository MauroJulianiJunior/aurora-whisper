package winruntime

import (
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/overlay"
)

// A gravação solta no runtime (ADR-025, D2). O núcleo decide tudo (quando
// liga, o que para, o que descarta); aqui só se liga o que está fora dele:
//
//   - o Esc e o hook do mouse, no hook (teclado.GravacaoSolta), que existem
//     só durante a gravação solta;
//   - o clique, que volta do hook por teclado.Cliques e vai ao núcleo como
//     CliqueEvent (Run);
//   - o campo relido na hora de escrever (ResolveEffect.ReleFoco e
//     InsertEffect.ReleFoco), porque depois de um clique ou de um minuto
//     ditando o cursor já não está onde estava no começo;
//   - o cadeado da pílula (mostradorDaSolta).

// mostradorDaSolta é o que o overlay oferece além de mostrador: o cadeado da
// gravação solta (ADR-025, D3). Opcional, como mostradorComTexto: uma pílula
// que não o tem mostra a gravação solta como a de sempre.
type mostradorDaSolta interface {
	SetGravacaoSolta(ligada bool)
}

// O overlay de verdade tem o cadeado. Sem esta linha, renomear o método
// desligaria o cadeado calado: a asserção de tipo em aplicarGravacaoSolta só
// deixaria de casar.
var _ mostradorDaSolta = (*overlay.Overlay)(nil)

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
	// O cadeado entra e sai antes da fase: o núcleo emite este efeito antes
	// do "Gravando" ao ligar e antes do "Processando" ao parar.
	if m, ok := rt.overlay.(mostradorDaSolta); ok {
		m.SetGravacaoSolta(e.Ligada)
	}
	if rt.hook == nil {
		return
	}
	if err := rt.hook.GravacaoSolta(e.Ligada); err != nil {
		// A gravação segue: o toque, o Esc e o limite não dependem do mouse.
		rt.log.Warn("winruntime: o hook do mouse falhou; o clique não parou esta gravação solta",
			"attempt", e.Attempt, "err", err)
	}
}

// recusarNoFim relê o lugar do cursor antes de escrever o texto da gravação
// solta, e não escreve em dois casos:
//
//   - **campo protegido** (ADR-025, C.2): é o AC-036 no fim da gravação, já
//     que o campo pode ter mudado no meio;
//   - **menu aberto** (ADR-025, risco 5; decisão do Mauro de 07-nov-2026): o
//     clique que para pode ter aberto um menu (o botão direito, o ícone da
//     bandeja), e escrever num menu do Windows aciona itens pela primeira
//     letra. No da bandeja do Aurora, um "s" no texto seria o "Sair".
//
// Nos dois, a resposta é a de uma inserção que falhou, e o núcleo grava o
// texto no histórico como grava o de toda inserção que falha. O motivo no
// histórico é o de sempre (status "error"), sem dizer qual dos dois: o do
// campo seria metadado sobre o uso de campo de senha (AC-027). Só a pílula diz,
// na hora. O campo protegido vem antes: um menu aberto sobre um campo de senha
// é, antes de tudo, um campo de senha.
//
// Fora da gravação solta não relê nada: o ditado de sempre já foi bloqueado no
// aperto se o campo era protegido (AC-036), e escreve onde a pessoa segura a
// tecla.
func (rt *Runtime) recusarNoFim(e dictation.InsertEffect) (dictation.Event, bool) {
	if !e.ReleFoco {
		return nil, false
	}
	alvo, _ := rt.lerFoco()
	switch {
	case alvo.Protected:
		rt.protegidoNoFim = e.Attempt
		rt.log.Info("winruntime: a gravação solta terminou num campo protegido; o texto fica no histórico",
			"attempt", e.Attempt)
	case focoEhMenu(alvo, rt.menuAberto != nil && rt.menuAberto()):
		rt.menuNoFim = e.Attempt
		rt.log.Info("winruntime: a gravação solta terminou com um menu aberto; o texto fica no histórico",
			"attempt", e.Attempt)
	default:
		return nil, false
	}
	return dictation.InsertResultEvent{
		Attempt: e.Attempt,
		Reason:  dictation.ErrorReasonInsertFailed,
		Result:  dictation.InsertResultDTO{Success: false},
	}, true
}

// focoEhMenu decide se o lugar do cursor no fim da gravação solta é um menu:
// a thread em primeiro plano está num menu do Win32 (menuAberto, de
// menuEmPrimeiroPlano), ou o controle em foco é de menu pela UI Automation
// (os menus que não são do Win32, quando um item deles tem o foco).
//
// Um sinal só não basta. Num menu do Win32 aberto pelo clique nenhum item está
// destacado, e o rastreador de foco, que acompanha a troca de foco, ainda vê o
// campo de antes; e um menu que não é do Win32 não liga o modo de menu.
func focoEhMenu(alvo dictation.FocusTarget, menuAberto bool) bool {
	if menuAberto {
		return true
	}
	switch alvo.ControlType {
	case "Menu", "MenuBar", "MenuItem":
		return true
	}
	return false
}
