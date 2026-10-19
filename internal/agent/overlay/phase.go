package overlay

import "github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"

// Este arquivo não tem build tag: é o mapeamento puro de
// dictation.OverlayPhase para o texto que docs/ai-spec/08-ui-ux-spec.md
// define para cada estado do overlay, testável sem Windows nenhum (ver
// phase_test.go).

// FaseJanelaElevada é o ditado recusado numa janela de administrador — a
// janela em primeiro plano roda com nível de integridade maior que o do
// Agent (decisão do Mauro de 29-set-2026, item 4). O ditado continua
// bloqueado como em OverlayBlocked: o SendInput não entra em janela
// elevada de qualquer forma (UIPI). Muda só o que a pílula diz, porque
// "campo protegido" numa janela sem campo de senha nenhum manda a pessoa
// procurar o problema no lugar errado.
//
// # Por que não é um valor de dictation.OverlayPhase
//
// O núcleo não muda (ADR-011, "Consequência que governa tudo"): o Machine
// continua emitindo OverlayBlocked, sem saber de janela nenhuma. Quem sabe
// é o runtime, pelo rastreador de foco (uia.Tracker.CurrentComElevacao), e
// é ele que troca a fase mostrada por esta (FaseDoBloqueio) na hora de
// chamar SetPhase. O valor fica bem longe da faixa do núcleo, e
// TestFaseJanelaElevada_ForaDoNucleo falha se um dia o núcleo ganhar uma
// fase com ele.
//
// Visualmente é a pílula de mensagem de um bloqueio, com o ponto vermelho
// dele (sinalPara).
//
// # Quase nunca aparece
//
// No teste manual de 29-set-2026 (roteiro B14), com o Gerenciador de
// Tarefas em primeiro plano, segurar Ctrl+Win não fez nada — nem pílula,
// nem bipe. O gancho de teclado de baixo nível de um processo comum não
// recebe as teclas quando a janela da frente é elevada (UIPI): o ditado
// nem começa, então nada é digitado, e esta fase não chega a ser mostrada.
// O código fica, para quando o atalho chegar ao Agent com a janela de
// administrador marcada; o B14 não viu esse caso acontecer (ADR-011, nota
// do B14).
const FaseJanelaElevada dictation.OverlayPhase = 1000

// FaseTextoNoHistorico é a transcrição que chegou DEPOIS do prazo, quando o
// núcleo já tinha encerrado a tentativa por timeout (incidente de
// 01-out-2026, docs/defeito-ditado-longo.md). O texto não é inserido — o
// foco já pode estar noutro lugar —, e vai para o histórico. A pílula diz
// isso, para a pessoa não ditar de novo um texto que já está salvo.
//
// Fora do núcleo pelo mesmo motivo de FaseJanelaElevada: o Machine já
// encerrou a tentativa e não sabe da transcrição atrasada; quem sabe é o
// runtime, que mostra esta fase só com a máquina parada (nenhum ditado em
// curso), para não atropelar a pílula de outro.
//
// Ponto cinza, como a área de transferência: o ditado não se perdeu, então
// não é erro; mas não foi inserido, e verde diria que foi.
const FaseTextoNoHistorico dictation.OverlayPhase = 1001

// FaseRepeticaoRemovida é o ditado inserido depois de a guarda tirar um laço
// de repetição do ASR (internal/asr/repeticao): a mesma frase várias vezes
// seguidas no lugar da fala, como no incidente de 01-out-2026. O texto
// entrou, com uma ocorrência só; mas o laço costuma tomar o lugar de parte
// da fala, e quem ditou precisa conferir.
//
// Fora do núcleo, como as outras desta lista: o núcleo emite OverlaySuccess
// e o runtime, que viu a guarda agir naquela tentativa, a troca por esta
// (FaseDoSucesso). Ponto verde: foi inserido.
const FaseRepeticaoRemovida dictation.OverlayPhase = 1002

// FaseMicrofoneBloqueado é o ditado que não ouviu nada porque a privacidade
// do Windows bloqueia o microfone (internal/agent/permissao). Sem ela, a
// pessoa que instalou o app com a chave desligada via só "Não consegui
// transcrever. Tente novamente." — e tentar de novo não resolve.
//
// Fora do núcleo, como as outras desta lista: o núcleo emite OverlayError
// (sem fala, falha de captura) e o runtime, que consulta o registro na hora,
// troca a fase por esta. A mensagem manda para o painel, que tem o botão
// para abrir a tela certa do Windows e o passo a passo. Ponto vermelho: o
// ditado não aconteceu.
const FaseMicrofoneBloqueado dictation.OverlayPhase = 1003

// FaseDoMicrofone escolhe a fase de um erro: OverlayError vira
// FaseMicrofoneBloqueado quando o microfone está bloqueado pelo Windows.
// Qualquer outra fase passa intacta — a marca só troca a mensagem de um
// erro, nunca transforma outra coisa em erro.
func FaseDoMicrofone(fase dictation.OverlayPhase, microfoneBloqueado bool) dictation.OverlayPhase {
	if fase == dictation.OverlayError && microfoneBloqueado {
		return FaseMicrofoneBloqueado
	}
	return fase
}

// FaseDoSucesso escolhe a fase de um sucesso: OverlaySuccess vira
// FaseRepeticaoRemovida quando a guarda tirou um laço daquela tentativa.
// Qualquer outra fase passa intacta — a marca só troca a mensagem de um
// sucesso, nunca transforma erro em sucesso.
func FaseDoSucesso(fase dictation.OverlayPhase, repeticaoRemovida bool) dictation.OverlayPhase {
	if fase == dictation.OverlaySuccess && repeticaoRemovida {
		return FaseRepeticaoRemovida
	}
	return fase
}

// FaseDoBloqueio escolhe a fase que o overlay mostra para fase, a que o
// núcleo emitiu: OverlayBlocked vira FaseJanelaElevada quando a proteção
// daquela tentativa era por janela de administrador. Qualquer outra fase
// passa intacta — a marca só troca a mensagem de um bloqueio, nunca
// transforma outra coisa em bloqueio.
func FaseDoBloqueio(fase dictation.OverlayPhase, janelaElevada bool) dictation.OverlayPhase {
	if fase == dictation.OverlayBlocked && janelaElevada {
		return FaseJanelaElevada
	}
	return fase
}

// Label devolve o texto que o overlay mostra para phase, verbatim de
// docs/ai-spec/08-ui-ux-spec.md ("Overlay").
//
// O estado "processing" da spec era, até 28-set-2026, uma lacuna
// registrada aqui: dictation.OverlayPhase não tinha o valor porque
// Transcribe e Resolve seguiam stub no Sprint 1 e não havia espera real
// para mostrar. Com ASR real passou a haver — 665 ms medidos — e o
// overlay ficava em "Gravando" durante ela, contador correndo, com o
// microfone já fechado. O valor existe agora (dictation.OverlayProcessing)
// e este switch ganhou o caso.
func Label(phase dictation.OverlayPhase) string {
	switch phase {
	case dictation.OverlayRecording:
		return "Gravando"
	case dictation.OverlayProcessing:
		return "Processando"
	case dictation.OverlaySuccess:
		return "Inserido"
	case dictation.OverlayError:
		return "Não consegui transcrever. Tente novamente."
	case dictation.OverlayBlocked:
		return "Ditado desativado em campo protegido."
	case FaseJanelaElevada:
		// É a mensagem mais longa, e a de duas linhas mais larga: quebra em
		// "O Windows não deixa ditar" / "em janela de administrador.", 25 e
		// 27 letras (ComporMensagem; travado em
		// TestComporMensagem_QuebrasDasMensagens). A sugestão da decisão, "Janela
		// de administrador: o Windows não deixa ditar aqui.", foi escrita
		// para a fonte de bitmap, em que precisaria de quatro linhas.
		//
		// Na prática ela quase nunca aparece: com uma janela elevada em
		// primeiro plano, o gancho de teclado do Agent não recebe o atalho
		// (ver FaseJanelaElevada).
		return "O Windows não deixa ditar em janela de administrador."
	case FaseTextoNoHistorico:
		// Diz o que aconteceu e onde está o texto. Sem "tente novamente":
		// ditar de novo é exatamente o que não precisa.
		return "O texto demorou e ficou no histórico."
	case FaseRepeticaoRemovida:
		// O que foi feito e o que fazer: conferir, porque o laço pode ter
		// tomado o lugar de parte da fala.
		return "Removi uma repetição. Confira o texto."
	case FaseMicrofoneBloqueado:
		// Diz quem bloqueou e onde olhar, em duas linhas como a da janela
		// elevada (travado em TestComporMensagem_QuebrasDasMensagens). O
		// passo a passo cabe no painel, não na pílula.
		return "O Windows bloqueou o microfone. Veja o painel."
	case dictation.OverlayInsertFailed:
		// A transcrição deu certo; só a inserção falhou, e o texto NÃO
		// está na área de transferência. Mandar colar aqui faria a
		// pessoa colar o que ela tinha copiado antes.
		return "Não consegui inserir o texto."
	case dictation.OverlayClipboard:
		// A transcrição deu certo; só a inserção falhou. Dizer "não
		// consegui transcrever" aqui faria a pessoa ditar de novo um
		// texto que já está pronto na área de transferência.
		//
		// A mensagem diz o que FAZER, não o que falhou: quem está com o
		// overlay na frente quer resolver, não diagnosticar.
		return "Texto na área de transferência: Ctrl+V."
	default:
		return ""
	}
}

// Visible reporta se phase deve deixar a janela do overlay visível.
// Só dictation.OverlayHidden — o zero value do tipo, e o que este
// pacote usa para se esconder sozinho depois de mostrar um desfecho
// terminal (ver autoHideDelay em overlay.go) — devolve false.
func Visible(phase dictation.OverlayPhase) bool {
	return phase != dictation.OverlayHidden
}

// isTerminalPhase reporta se phase é um desfecho de tentativa — os
// únicos após os quais o overlay se esconde sozinho depois de
// autoHideDelay. O núcleo nunca emite um "esconder" separado (ADR-007);
// esconder é responsabilidade deste pacote.
//
// "hidden", "recording" e "processing" não são desfecho: a tentativa
// ainda está em curso. Se "processing" se escondesse sozinho, o overlay
// sumiria no meio do ASR e voltaria com "Inserido" — pior que não ter
// fase nenhuma, porque pareceria que o ditado foi cancelado.
func isTerminalPhase(phase dictation.OverlayPhase) bool {
	switch phase {
	case dictation.OverlaySuccess, dictation.OverlayError,
		dictation.OverlayBlocked, dictation.OverlayClipboard,
		dictation.OverlayInsertFailed, FaseJanelaElevada, FaseTextoNoHistorico,
		FaseRepeticaoRemovida, FaseMicrofoneBloqueado:
		return true
	default:
		return false
	}
}
