// Package uia contém os bindings de UI Automation (COM, mmdevapi-style
// vtable manual — mesma disciplina do WASAPI do Sprint 1, ADR-010 D1) e o
// rastreador de foco do Windows Agent (ADR-011, decisões 1, 2 e 5).
//
// # Inspeção, não inserção (ADR-011, decisão 1)
//
// Este pacote NÃO escreve nada em controle nenhum. Ele só responde "o que
// tem foco agora, de que tipo é, e é campo de senha?". Não existe (e não
// deve existir) nenhum caminho de SetValue aqui — SetValue substitui o
// valor INTEIRO de um controle e destruiria texto que a pessoa já tinha
// digitado; a inserção real, em camadas (SendInput / clipboard), é
// responsabilidade de outra fatia (ADR-011, decisão 3), fora deste
// pacote.
//
// # O rastreador de foco (ADR-011, decisão 2)
//
// [Tracker] expõe leitura O(1), sem I/O:
//
//	func (t *Tracker) Current() dictation.FocusTarget
//
// Por trás, Start() instala uma thread dedicada, travada com
// runtime.LockOSThread(), que roda seu próprio loop de mensagens e um
// SetWinEventHook(EVENT_OBJECT_FOCUS). A cada troca de foco, essa thread
// consulta a UIA FORA do caminho do teclado e publica o resultado num
// cache atômico. O KeyDownEvent do núcleo lê esse cache — nunca consulta
// UIA de forma síncrona. Uma consulta UIA custa de milissegundos a
// centenas de milissegundos; o callback do WH_KEYBOARD_LL tem orçamento
// de ~300ms (ADR-010), e estourá-lo faz o Windows remover o hook em
// silêncio. Regredir isso quebraria algo já verificado em máquina real no
// roteiro de teste manual do Sprint 1.
//
// # Campo protegido, na dúvida protege (ADR-011, decisão 5)
//
// [Snapshot.Protected] combina dois sinais — IsPassword da própria UIA, e
// o fallback de estilo ES_PASSWORD em controles Edit clássicos (para
// quando a UIA não responde IsPassword corretamente) — e trata qualquer
// inspeção que não tenha sido um sucesso claro (falhou, deu timeout,
// elemento nulo) como protegida. O custo de proteger à toa é um ditado
// que não acontece; o custo do erro oposto é voz capturada num campo de
// senha. Esta política é lógica pura, sem build tag, testada contra
// todos os ramos em types_test.go.
//
// Depois de uma inspeção que não trouxe elemento, a thread olha, só
// leitura, se a janela em primeiro plano roda com nível de integridade
// maior que o do Agent ([Elevacao]). Se roda, o cache ganha a marca de
// janela de administrador (Tracker.CurrentComElevacao), que o runtime usa
// para trocar a mensagem da pílula — o bloqueio é o mesmo.
//
// Uma leitura completa — as essenciais responderam e o sinal ES_PASSWORD
// rodou — que passou do orçamento ainda vale até TetoLeituraAtrasada (2
// s); acima disso fica protegida e ganha uma reinspeção imediata (ver
// classificarLeitura). Decisão do Mauro de 29-set-2026, só segura junto
// com o vigia abaixo.
//
// Uma inspeção que passa do orçamento (Config.InspectBudget) sem
// terminar aciona o vigia: um time.AfterFunc que troca o valor livre que
// estava no cache por um protegido, por CompareAndSwap no próprio
// ponteiro do cache, até a inspeção terminar e publicar o que leu (ver
// Tracker.inspecionarEPublicar, "O vigia"). É a única escrita no cache
// fora da thread dedicada.
//
// Uma inspeção em dúvida ganha até três reinspeções (100 ms, 300 ms,
// 1 s — [ProximaReinspecao]) enquanto o foco não muda; o cache fica
// protegido até uma delas responder. Uma inspeção que levou mais que
// [TetoParaReinspecionar] não é reinspecionada: seria voltar a um
// provedor que acabou de travar. A reinspeção lê o foco atual; se ele
// estiver numa janela do próprio Agent (o menu da bandeja), ela não
// publica e a escada acaba.
//
// # Organização
//
//   - types.go: [Snapshot], [InspectOutcome] e a decisão de Protected —
//     puros, sem build tag, testados em qualquer SO.
//   - ciclo.go, reinspecao.go, medicao.go, atrasada.go: o que a thread
//     dedicada decide com cada inspeção — publicar ou descartar (geração),
//     quando reinspecionar, qual chamada foi a mais lenta, quando o cache
//     voltou a livre, quando uma leitura atrasada ainda vale. Puros, sem
//     build tag, testados contra dublês.
//   - tracker.go: [Tracker] e [Config] — o cache atômico e Current() são
//     só memória, sem build tag, como a sequência inspeciona-publica-
//     relata (com as chamadas de sistema injetadas) e o vigia (um
//     time.AfterFunc, sem chamada de sistema); Start()/Stop() (que
//     de fato instalam o hook) ficam no par tracker_windows.go /
//     tracker_other.go, mesma convenção de internal/agent/hook e
//     internal/agent/audio.
//   - automacao.go: qual objeto de UI Automation criar — CUIAutomation8
//     com TetoChamadaUIA (3 s) em ConnectionTimeout e TransactionTimeout,
//     ou o CUIAutomation antigo se o 8 não existir. Puro, testado contra
//     um dublê do COM.
//   - automation_windows.go: os bindings COM de IUIAutomation2 /
//     IUIAutomation / IUIAutomationElement (CLSID_CUIAutomation8 e
//     CLSID_CUIAutomation), o SetWinEventHook, o timer da reinspeção, o
//     fallback ES_PASSWORD e a resolução de PID -> nome de processo.
//     Usado só por tracker_windows.go; não precisa de par _other.go
//     próprio (mesma exceção de audio/wasapi_windows.go: só o ponto de
//     entrada, aqui Tracker.Start/Stop, precisa do par multiplataforma).
//
// # Portão de segurança (ADR-011, decisão 6)
//
// SetWinEventHook instala um hook de sistema — mesma classe de risco que
// o WH_KEYBOARD_LL do pacote hook, ainda que só leia eventos e nunca
// injete nada. Por isso Start() passa por sysinput.Guard() antes de
// qualquer chamada de sistema — COM, objeto de UI Automation, tetos, hook
// (desde 29-set-2026; antes o portão vinha só antes do hook): sob "go
// test" a subida é sempre recusada, então nenhum teste desta suíte cria
// objeto de UI Automation nem instala hook de sistema de verdade, mesmo
// que exercite o caminho real de Start(). A leitura de
// propriedades da UIA em si (ControlType, IsPassword, ProcessId,
// NativeWindowHandle) não injeta entrada nem altera estado global do
// sistema — não passa pelo portão.
package uia
