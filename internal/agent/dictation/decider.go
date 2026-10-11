package dictation

// Decision é a resposta do Decider para um RawKeyEvent: o que fazer na
// borda síncrona do hook (Suppress) e se o evento deve ser encaminhado ao
// Machine (Notify). Os valores zero (false, false) equivalem a "deixe
// passar e não notifique ninguém".
type Decision struct {
	// Suppress: true faz o runtime engolir o evento físico (o hook não o
	// repassa ao sistema).
	Suppress bool
	// Notify: true faz o runtime traduzir e encaminhar este evento ao
	// Machine (fora da thread do hook).
	Notify bool
}

// passThrough é a decisão "não faça nada com este evento".
var passThrough = Decision{}

// Decider decide, de forma pura e síncrona, se um RawKeyEvent deve ser
// suprimido e/ou notificado ao núcleo. Nunca faz I/O, nunca bloqueia,
// nunca consulta UI Automation (ADR-007, seção 2) — é seguro chamar a
// partir da thread do WH_KEYBOARD_LL.
//
// Decider mantém o menor estado possível: se o par (Key, sem modifiers)
// está atualmente "armado" (keydown já suprimido, aguardando o keyup
// correspondente), para poder responder simetricamente ao keyup.
type Decider struct {
	key      KeyCode
	required Modifiers
	armed    bool
}

// Armado reporta se há um toque da tecla de ditado EM CURSO — entre o
// evento que armou e a soltura.
//
// Existe para a pausa do Agent (hook.Hook.Pausar) respeitar um toque que
// já começou. Pausar no meio de um hold — segurar a tecla com uma mão e
// clicar "Pausar" na bandeja com a outra — faria a soltura passar direto
// pelo hook, o núcleo nunca receberia o keyup, e a gravação seguiria até
// o limite de 120 s com o microfone aberto. Com Armado, a pausa vale para
// o PRÓXIMO toque, e o que está em curso termina normalmente.
func (d *Decider) Armado() bool { return d.armed }

// NewDecider cria um Decider para a tecla principal key. key normalmente
// vem de uma Config já validada (Config.Validate), mas o Decider em si não
// valida nada — isso é responsabilidade de quem monta a configuração.
func NewDecider(key KeyCode) *Decider {
	return &Decider{key: key}
}

// NewDeciderModificadores cria um Decider cujo gatilho é uma COMBINAÇÃO de
// modificadores (ex.: Ctrl+Win), sem tecla principal.
//
// # Por que este modo existe
//
// Com tecla de letra, o ditado precisa engolir o keydown (para o hold não
// digitar a letra, AC-002) e devolver a letra depois, se tiver sido um
// toque (AC-001). Entre engolir e devolver passa tempo, e as teclas
// seguintes chegam ANTES da devolvida: digitar "freio" produzia "refio".
// Está registrado em docs/defeito-ordem-da-tecla-de-ditado.md, e apareceu
// em uso real, não em teste.
//
// Uma combinação de modificadores não produz caractere nenhum. Não há o
// que engolir nem o que devolver, e a classe inteira do problema deixa de
// existir — não é remendo, é remoção da causa.
//
// # Este modo NUNCA suprime nada, e isso é deliberado
//
// Suprimir um keyup de modificador deixaria o Windows achando que a tecla
// continua pressionada. Isso já aconteceu neste projeto, em 17-set-2026:
// um teste injetou entrada e deixou um modificador preso, travando o
// teclado de quem estava usando a máquina. Não vale o risco para evitar,
// no pior caso, um menu Iniciar abrindo sem querer.
func NewDeciderModificadores(required Modifiers) *Decider {
	return &Decider{key: KeyUnknown, required: required}
}

// Decide aplica as regras da ADR-007, seção 3, na ordem descrita:
//
//  1. Evento injetado por nós mesmos (Injected && FromThisProcess) é
//     sempre PassThrough e nunca inicia sessão — evita o laço infinito do
//     SynthesizeTapEffect voltando pelo próprio hook.
//  2. Tecla diferente da configurada: sempre PassThrough.
//  3. Tecla configurada COM modificadores: PassThrough, bypass de ditado
//     (AC-045) — atalhos como Ctrl+F continuam funcionando.
//  4. Tecla configurada SEM modificadores, keydown: Suppress + Notify.
//  5. Tecla configurada SEM modificadores, keyup: Suppress + Notify
//     somente se o keydown correspondente foi armado; caso contrário
//     PassThrough (ex.: keyup depois de um keydown que veio com
//     modificadores, ou keyup perdido).
func (d *Decider) Decide(ev RawKeyEvent) Decision {
	if ev.Injected && ev.FromThisProcess {
		return passThrough
	}
	if d.key == KeyUnknown {
		return d.decideCombinacao(ev)
	}
	if ev.Key != d.key {
		return passThrough
	}
	if !ev.Modifiers.IsNone() {
		if !ev.Down {
			// Segurança: um keyup com modificador nunca deveria estar
			// armado, mas limpa o estado se algo ficou inconsistente.
			d.armed = false
		}
		return passThrough
	}
	if ev.Down {
		d.armed = true
		return Decision{Suppress: true, Notify: true}
	}
	// Keyup sem modificadores.
	if !d.armed {
		return passThrough
	}
	d.armed = false
	return Decision{Suppress: true, Notify: true}
}

// decideCombinacao aplica as regras do gatilho por combinação de
// modificadores.
//
// O Translator do hook atualiza o estado ANTES de montar RawKeyEvent, então
// ev.Modifiers já inclui a própria tecla do evento: apertar Win com Ctrl
// segurado chega como Modifiers = Ctrl|Meta. É isso que torna a regra uma
// simples transição de "tem o conjunto" para "não tem".
//
// A transição para completo sempre acontece num keydown, e a de completo
// para incompleto sempre num keyup — soltar tecla nunca ACRESCENTA
// modificador. Por isso o runtime traduz esses eventos para KeyDown/KeyUp
// do núcleo sem nenhum caso especial.
//
// Qualquer evento serve para reavaliar o conjunto, inclusive de tecla
// comum. Uma guarda que ignorasse teclas não modificadoras seria
// redundante no caso normal — com o ditado armado, o conjunto está
// pressionado, então Ctrl+Win+D continua chegando ao Windows e a gravação
// continua, que é o comportamento desejado.
//
// E ela seria PIOR no caso anormal: se um keyup de modificador se perder
// (troca de foco, evento descartado), a sessão ficaria armada para sempre
// esperando um modificador que nunca vem. Sem a guarda, a próxima tecla
// digitada já encontra o conjunto incompleto e encerra a sessão pendurada.
//
// A guarda existiu e foi removida quando um teste de mutação mostrou que
// nenhum caso a exercitava: ramo que nenhum teste consegue distinguir não
// deve existir.
func (d *Decider) decideCombinacao(ev RawKeyEvent) Decision {
	completo := ev.Modifiers.Has(d.required)
	switch {
	case completo && !d.armed:
		d.armed = true
		// Notify sem Suppress: ver NewDeciderModificadores.
		return Decision{Notify: true}
	case !completo && d.armed:
		d.armed = false
		return Decision{Notify: true}
	default:
		return passThrough
	}
}
