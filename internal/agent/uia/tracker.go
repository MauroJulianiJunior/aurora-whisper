package uia

// Este arquivo não tem build tag: Tracker guarda só memória (o cache
// atômico, o Config e o estado de ciclo de vida) — nenhum campo chama
// para dentro do Win32 por si só. O que de fato instala o hook de sistema
// (Start/Stop) está em tracker_windows.go / tracker_other.go, no par
// exigido pelo CI multiplataforma (ver doc.go e o doc.go de
// internal/agent/win32).

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// defaultInspectBudget é o orçamento de tempo usado quando Config.
// InspectBudget não é informado.
const defaultInspectBudget = 500 * time.Millisecond

// Config configura um Tracker.
type Config struct {
	// InspectBudget é o tempo máximo tolerado para uma consulta UIA ao
	// elemento com foco, medido de FORA da chamada — não é uma forma de
	// cancelar a chamada COM em curso. COM é apartment-threaded; a consulta
	// roda inteira na thread dedicada do Tracker, e não há como interrompê-
	// la com segurança de outra goroutine sem violar essa regra (a mesma
	// que rege o WASAPI, ADR-010 D1). Uma consulta que ultrapassa o
	// orçamento é tratada como InspectTimeout (ADR-011, decisão 5: "na
	// dúvida, protege"), a menos que seja uma leitura completa dentro de
	// TetoLeituraAtrasada (decisão do Mauro de 29-set-2026, ver
	// classificarLeitura).
	//
	// É também o prazo do vigia: uma inspeção que passa dele sem terminar
	// deixa o cache protegido até terminar (ver inspecionarEPublicar).
	//
	// Zero usa defaultInspectBudget.
	InspectBudget time.Duration
}

func (c Config) withDefaults() Config {
	if c.InspectBudget <= 0 {
		c.InspectBudget = defaultInspectBudget
	}
	return c
}

// DiagnosticoFoco é o que se pode contar sobre uma inspeção de foco sem
// violar a regra do CLAUDE.md de nunca registrar contexto de janela.
//
// Não carrega ControlType nem nada que diga o que há na janela (Name e
// ClassName nem são lidos, ver readElement). Do aplicativo, carrega só o
// nome-base do executável, e só num aviso (Executavel — exceção autorizada
// pelo Mauro em 29-set-2026). Fora isso, só o RESULTADO da inspeção e os
// dois sinais que decidem Protected, que é o suficiente para separar
// causas muito diferentes com a mesma consequência:
//
//	outcome=timeout    a UIA demorou demais (app pesado, árvore lenta)
//	outcome=failed     uma chamada COM essencial falhou
//	outcome=nil_element nenhum controle tem foco de teclado agora
//	outcome=ok + senha  é campo de senha DE VERDADE
//
// As três primeiras caem em "na dúvida, protege" (ADR-011, decisão 5) e
// bloqueiam o ditado exatamente como um campo de senha real — com a
// diferença de que só a última está certa.
//
// Falha e HRESULT abrem o outcome=failed, que sozinho ainda era ambíguo:
// ele sai tanto quando a UIA não consegue dizer quem tem o foco quanto
// quando o elemento veio e uma propriedade não leu — causas opostas, com
// correções opostas. Ver FalhaEm.
type DiagnosticoFoco struct {
	Outcome    InspectOutcome
	Falha      FalhaEm
	HRESULT    int32
	Protected  bool
	IsPassword bool
	StyleSenha bool
	Duracao    time.Duration
	// LentaEm e LentaDuracao: qual chamada da inspeção foi a mais lenta,
	// e quanto levou (ver ChamadaUIA). É o que separa "a UIA não disse
	// quem tem o foco" de "uma leitura de melhor esforço travou".
	LentaEm      ChamadaUIA
	LentaDuracao time.Duration
	// Inicial marca a inspeção feita uma única vez no Start, antes de
	// qualquer troca de foco (ver run em tracker_windows.go).
	Inicial bool
	// Liberou marca a inspeção que devolveu o cache de protegido para
	// livre, e ProtegidoPor diz quanto tempo ele ficou protegido antes
	// dela. Sem isto, a volta a livre saía em Debug e o log não dizia se
	// um WARN tinha deixado o ditado recusado por um segundo ou por dez
	// minutos.
	Liberou      bool
	ProtegidoPor time.Duration
	// Descartada marca a inspeção que NÃO publicou porque uma mais nova
	// começou enquanto ela rodava — só acontece se o callback for
	// reentrado (ver devePublicar). O cache não mudou por causa dela.
	Descartada bool
	// NoProprioAgent marca a reinspeção que leu um elemento do próprio
	// Agent — a janela oculta do menu da bandeja, por exemplo — e por isso
	// NÃO publicou: o cache ficou com a dúvida anterior, protegido, e a
	// escada acabou (ver cicloFoco.concluir). Diz só que o elemento é do
	// Agent; nada sobre outra janela.
	NoProprioAgent bool
	// Tentativa é 0 na inspeção de uma troca de foco (ou na do Start), e
	// de 1 a MaxReinspecoes nas reinspeções que a seguiram.
	// ReinspecaoEm é maior que zero quando esta inspeção foi dúvida e
	// agendou a seguinte (ver ProximaReinspecao).
	Tentativa    int
	ReinspecaoEm time.Duration
	// Vigia marca a inspeção que passou do orçamento sem terminar e, por
	// isso, deixou o cache protegido enquanto rodava (ver
	// inspecionarEPublicar, "O vigia"). Se ela terminou livre depois de um
	// cache livre, Liberou e ProtegidoPor contam esse intervalo.
	Vigia bool
	// Atrasada marca a leitura completa que passou do orçamento e valeu
	// mesmo assim (ver classificarLeitura). Não é bloqueio; sai no log para
	// dizer quantas vezes a decisão de 29-set-2026 liberou o que antes
	// seria timeout.
	Atrasada bool
	// Elevacao: depois de uma inspeção sem elemento, se a janela em
	// primeiro plano é de administrador (ver Elevacao). Enumerado, como
	// FalhaEm: não carrega texto da janela.
	Elevacao Elevacao
	// Executavel é o nome-base do executável em foco (ex.: brave.exe), e
	// só numa inspeção em dúvida — as que saem em WARN; nas demais, "".
	// É o ÚNICO campo string deste tipo, e é uma exceção à regra do
	// CLAUDE.md de não registrar contexto de janela, autorizada pelo Mauro
	// em 29-set-2026: nunca título, classe, caminho ou texto, e nunca em
	// linha Info/Debug (ver executavelDoAviso e, em cmd/agent,
	// registroDoFoco).
	Executavel string
}

// AoInspecionar registra um observador chamado a cada inspeção de foco.
// Nil desliga. Deve ser chamado antes de Start.
//
// Existe para diagnóstico: até 26-set-2026 um ditado recusado por
// "campo protegido" não dizia qual dos quatro motivos o recusou, e três
// deles são falso positivo.
func (t *Tracker) AoInspecionar(f func(DiagnosticoFoco)) {
	t.aoInspecionar = f
}

func (t *Tracker) relatar(s Snapshot, d time.Duration, inicial bool, fim desfecho) {
	if t.aoInspecionar == nil {
		return
	}
	t.aoInspecionar(DiagnosticoFoco{
		Outcome:        s.Outcome,
		Falha:          s.Falha,
		HRESULT:        s.HRESULT,
		Protected:      s.Protected(),
		IsPassword:     s.IsPassword,
		StyleSenha:     s.StyleHasPassword,
		Duracao:        d,
		LentaEm:        s.LentaEm,
		LentaDuracao:   s.LentaDuracao,
		Inicial:        inicial,
		Liberou:        fim.Liberou,
		ProtegidoPor:   fim.ProtegidoPor,
		Descartada:     fim.Descartada,
		NoProprioAgent: fim.NoProprioAgent,
		Tentativa:      fim.Tentativa,
		ReinspecaoEm:   fim.ReinspecaoEm,
		Vigia:          fim.Vigia,
		Atrasada:       s.Atrasada,
		Elevacao:       s.Elevacao,
		Executavel:     executavelDoAviso(s),
	})
}

// sistema é o que a thread dedicada precisa do Windows para uma inspeção,
// injetado para que a sequência inteira seja testável com dublês: em
// produção (tracker_windows.go), inspecionar chama inspectFocusedElement,
// agora é time.Now, agendar cria um timer da própria thread e vigiar é
// vigiarComTimer; nos testes, dublês.
type sistema struct {
	inspecionar func() Snapshot
	agora       func() time.Time
	// agendar pede UMA reinspeção para daqui a espera, substituindo a que
	// estiver agendada. Devolve false se não conseguiu. Nil equivale a
	// nunca conseguir.
	agendar func(espera time.Duration) bool
	// vigiar arma f para rodar daqui a espera, noutra goroutine, e devolve
	// parar: desarma f, ou, se f já começou, espera ela terminar. Depois
	// de parar voltar, f não roda mais nem está rodando. Nil desliga o
	// vigia (ver inspecionarEPublicar).
	vigiar func(espera time.Duration, f func()) (parar func())
	// primeiroPlano lê, só leitura, o processo da janela em primeiro plano
	// (ver deveOlharPrimeiroPlano). Nil não olha: Elevacao fica
	// ElevacaoNaoVerificada.
	primeiroPlano func() primeiroPlano
}

// inspecionarEPublicar é uma inspeção completa, do jeito que a thread
// dedicada a faz a cada EVENT_OBJECT_FOCUS, uma vez no Start e em cada
// reinspeção: inspeciona, publica no cache, agenda a reinspeção se a
// inspeção foi dúvida, e relata. tentativa é 0 exceto numa reinspeção
// (ver desfecho.Tentativa).
//
// Só publica se nenhuma inspeção mais nova começou enquanto esta rodava
// (ver devePublicar: isso só acontece com reentrância), e se não é uma
// reinspeção que caiu no próprio Agent (ver cicloFoco.concluir).
//
// # O vigia (decisão do Mauro, 29-set-2026)
//
// Enquanto a chamada UIA está presa, o cache guardava o valor ANTERIOR — e
// um "livre" antigo continuava valendo mesmo que o foco já estivesse num
// campo de senha (ADR-011, nota de 29-set-2026: 60 s observados). Agora,
// se a inspeção passa do orçamento (Config.InspectBudget) sem terminar, o
// vigia publica protegido até ela terminar; quando ela termina, publica o
// resultado dela pela regra de sempre (geração, próprio Agent).
//
// O vigia troca o valor observado no INÍCIO da inspeção por um protegido
// com CompareAndSwap no próprio ponteiro do cache (protegerSeAindaFor) —
// nunca checando um campo à parte e gravando depois: a inspeção poderia
// publicar entre a checagem e a gravação, e o protegido do vigia ficaria
// por cima do resultado novo, preso até a próxima troca de foco
// (contestação da investigação de 29-set-2026, TOCTOU). Com o CAS, ou o
// vigia vem antes e a publicação o sobrescreve, ou a publicação vem antes
// e o CAS falha; nos dois casos o cache termina com o resultado da
// inspeção.
func (t *Tracker) inspecionarEPublicar(sis sistema, tentativa int, inicial bool) {
	geracao := t.ciclo.comecar()
	inicio := sis.agora()

	// vigiou só é escrito pelo vigia e só é lido depois de parar(), que
	// espera o vigia terminar: não há corrida. Serve ao diagnóstico e à
	// contagem de protegido_por — a proteção em si é o CAS.
	var vigiou bool
	parar := func() {}
	if sis.vigiar != nil {
		observado := t.cache.Load()
		parar = sis.vigiar(t.cfg.InspectBudget, func() {
			vigiou = t.protegerSeAindaFor(observado)
		})
	}
	snap := sis.inspecionar()
	parar()
	fim := sis.agora()
	duracao := fim.Sub(inicio)

	// Janela de administrador (decisão do Mauro de 29-set-2026, item 4):
	// só depois de uma inspeção que não trouxe elemento, e só leitura. Fica
	// fora de duracao, que mede a UIA.
	if deveOlharPrimeiroPlano(snap) && sis.primeiroPlano != nil {
		pp := sis.primeiroPlano()
		snap.Elevacao, snap.ExecutavelDoPrimeiroPlano = pp.elevacao, pp.executavel
	}

	if vigiou {
		t.ciclo.anotarVigia(geracao, inicio.Add(t.cfg.InspectBudget))
	}
	d := t.ciclo.concluir(geracao, tentativa, snap, duracao, fim)
	d.Vigia = vigiou
	if d.publica() {
		t.store(estadoDe(snap))
	}
	if d.ReinspecaoEm > 0 && (sis.agendar == nil || !sis.agendar(d.ReinspecaoEm)) {
		t.ciclo.cancelarReinspecao()
		d.ReinspecaoEm = 0
	}
	t.relatar(snap, duracao, inicial, d)
}

// reinspecionar é o que a thread dedicada faz quando o timer de
// reinspeção vence: pergunta de novo quem tem o foco, se a reinspeção
// ainda vale (ver cicloFoco.disparar). Lê o foco ATUAL, não "o mesmo
// elemento" — ver ProximaReinspecao.
func (t *Tracker) reinspecionar(sis sistema) {
	tentativa, ok := t.ciclo.disparar()
	if !ok {
		return
	}
	t.inspecionarEPublicar(sis, tentativa, false)
}

// Tracker mantém, num cache atômico, o dictation.FocusTarget do elemento
// com foco de teclado mais recente (ADR-011, decisão 2). Current() é
// O(1), sem I/O, e nunca consulta UI Automation no caminho de leitura —
// quem popula o cache é a thread dedicada instalada por Start()
// (tracker_windows.go), a cada EVENT_OBJECT_FOCUS; e, desde 29-set-2026,
// o vigia de uma inspeção que passou do orçamento, que só pode trocar um
// livre por um protegido (ver inspecionarEPublicar, "O vigia").
//
// Start/Stop devem ser chamados pela mesma goroutine "dona" do Tracker,
// nunca concorrentemente entre si — mesmo contrato de internal/agent/hook.
// Current() é seguro de qualquer goroutine, a qualquer momento, mesmo
// antes de Start().
type Tracker struct {
	cfg   Config
	cache atomic.Pointer[estadoFoco]

	// mu protege apenas o ciclo de vida (started/done) contra chamadas
	// concorrentes indevidas a Start/Stop — mesma disciplina de
	// internal/agent/hook.Hook. threadID e hookHandle são escritos uma
	// única vez, na goroutine da thread dedicada, antes de serem
	// publicados ao chamador de Start() através do canal ready; dali em
	// diante só são lidos pela própria goroutine (callback) ou por Stop
	// (que, por contrato, só é chamado depois que Start já retornou).
	mu      sync.Mutex
	started bool
	done    chan struct{}

	threadID uint32
	// hookHandle é o HWINEVENTHOOK devolvido por SetWinEventHook,
	// mantido como uintptr para que este campo não precise de build tag
	// (é só memória) — ver o comentário de topo deste arquivo.
	hookHandle uintptr

	// aoInspecionar é escrito antes de Start e só lido pela thread
	// dedicada dali em diante (ver AoInspecionar).
	aoInspecionar func(DiagnosticoFoco)

	// ciclo é o estado de decisão da thread dedicada (ciclo.go). Só ela o
	// toca.
	ciclo cicloFoco

	// automacao é escrito uma vez pela thread dedicada, antes de ela
	// publicar prontidão ao Start; lido por quem chamou Start, depois que
	// ele voltou (ver Automacao).
	automacao Automacao
}

// Automacao diz qual objeto de UI Automation a thread dedicada criou e se
// o teto (TetoChamadaUIA) foi aplicado. Só vale depois de Start devolver
// nil; antes disso, e fora do Windows, é o zero value (ObjetoNenhum).
func (t *Tracker) Automacao() Automacao {
	return t.automacao
}

// NewTracker cria um Tracker ainda não instalado — chame Start(). O cache
// começa com Protected: true: até o primeiro EVENT_OBJECT_FOCUS ser
// processado, nenhum foco real foi observado, e o erro seguro (ADR-011,
// decisão 5, "na dúvida, protege") é assumir campo protegido em vez de
// liberar o ditado contra um alvo desconhecido.
func NewTracker(cfg Config) *Tracker {
	t := &Tracker{cfg: cfg.withDefaults()}
	t.ciclo.pidProprio = uint32(os.Getpid())
	t.cache.Store(&estadoFoco{alvo: dictation.FocusTarget{Protected: true}})
	return t
}

// estadoFoco é o que o cache guarda: o alvo que o núcleo consome e, à
// parte, se a proteção dele é por janela de administrador.
//
// A marca não vai dentro de dictation.FocusTarget de propósito: o núcleo
// não muda (ADR-011, "Consequência que governa tudo"), e ela não decide
// nada — o ditado é bloqueado por Protected, igual. Ela só escolhe a
// mensagem da pílula, que é do runtime (ver CurrentComElevacao).
//
// Os dois ficam num ponteiro só para serem lidos juntos: dois atômicos
// separados poderiam dar o alvo de uma inspeção com a marca de outra.
type estadoFoco struct {
	alvo          dictation.FocusTarget
	janelaElevada bool
}

// estadoDe é o que uma inspeção publica: o alvo, e a marca de janela de
// administrador só quando ela está provada (ElevacaoSim) E o alvo está
// protegido — a marca nunca aparece num alvo livre.
func estadoDe(s Snapshot) estadoFoco {
	alvo := s.ToFocusTarget()
	return estadoFoco{alvo: alvo, janelaElevada: alvo.Protected && s.Elevacao == ElevacaoSim}
}

// Current devolve o dictation.FocusTarget mais recente conhecido: leitura
// O(1) por ponteiro atômico, sem lock, sem I/O, nunca bloqueia. É isto
// que ADR-011 decisão 2 exige do KeyDownEvent — nunca uma consulta UIA
// síncrona no caminho do teclado.
func (t *Tracker) Current() dictation.FocusTarget {
	return t.cache.Load().alvo
}

// CurrentComElevacao é Current mais a marca de janela de administrador, os
// dois da MESMA leitura do cache. janelaElevada só é true com
// alvo.Protected true: o ditado seria bloqueado de qualquer forma, e a
// marca só troca a mensagem (decisão do Mauro de 29-set-2026, item 4).
func (t *Tracker) CurrentComElevacao() (alvo dictation.FocusTarget, janelaElevada bool) {
	e := t.cache.Load()
	return e.alvo, e.janelaElevada
}

// store publica um novo estado no cache. Chamado só pela thread
// dedicada instalada por Start() (ADR-011, decisão 2) — nunca pelo caminho
// de leitura.
//
// Até 29-set-2026 a thread dedicada era a ÚNICA a escrever no cache. Não
// é mais: o vigia (protegerSeAindaFor) também escreve, de outra goroutine
// — mas só por CompareAndSwap, e só trocando um valor livre observado no
// início da inspeção por um protegido. store continua incondicional, e é
// isso que garante que o resultado da inspeção sempre prevalece sobre o
// protegido do vigia (ver inspecionarEPublicar, "O vigia").
//
// Cada store aloca um ponteiro novo: é o que dá ao CAS do vigia a
// identidade de "o valor que eu observei" — um ponteiro reaproveitado
// poderia voltar ao cache depois de trocado, e o CAS acertaria um valor
// que não é mais o mesmo.
func (t *Tracker) store(e estadoFoco) {
	t.cache.Store(&e)
}

// protegerSeAindaFor é o que o vigia faz quando a inspeção passa do
// orçamento: troca observado — o ponteiro que estava no cache quando a
// inspeção começou — por um alvo protegido, SE o cache ainda for
// observado. Devolve se trocou.
//
// Um observado já protegido fica como está: não há "livre" velho a cobrir,
// e trocá-lo só apagaria o que ele diz — a marca de janela de
// administrador, por exemplo.
//
// Roda na goroutine do timer, não na thread dedicada. Por isso só mexe no
// cache, por CAS, e em nada mais: o cicloFoco é só da thread dedicada.
func (t *Tracker) protegerSeAindaFor(observado *estadoFoco) bool {
	if observado == nil || observado.alvo.Protected {
		return false
	}
	return t.cache.CompareAndSwap(observado, &estadoFoco{alvo: dictation.FocusTarget{Protected: true}})
}

// vigiarComTimer é o vigiar de produção (ver sistema): um time.AfterFunc,
// sem chamada de sistema nenhuma — nem COM, nem Win32. f roda na goroutine
// do timer do Go.
//
// parar desarma o timer; se ele já disparou, espera f terminar. É isso que
// deixa quem chamou ler, sem corrida, o que f escreveu.
func vigiarComTimer(espera time.Duration, f func()) (parar func()) {
	feito := make(chan struct{})
	tm := time.AfterFunc(espera, func() {
		defer close(feito)
		f()
	})
	return func() {
		if !tm.Stop() {
			<-feito
		}
	}
}
