package uia

import (
	"os"
	"testing"
	"time"
)

// hr converte o HRESULT como ele aparece no log (0x8004...) para o int32
// que Snapshot guarda.
func hr(u uint32) int32 { return int32(u) }

const (
	eFail                   = 0x80004005 // E_FAIL — as rajadas do Gerenciador de Tarefas
	uiaEElementNotAvailable = 0x80040201 // o Learn manda tentar de novo
	uiaETimeout             = 0x80131505
)

// rapida é a duração de uma inspeção que não chegou perto do teto.
const rapida = 5 * time.Millisecond

// TestProximaReinspecao cobre em que casos, e depois de quanto tempo, a
// dúvida ganha nova tentativa. Salvo onde o caso diz o contrário, a
// inspeção foi rápida (ver TestProximaReinspecao_TetoDeDuracao).
func TestProximaReinspecao(t *testing.T) {
	casos := []struct {
		nome      string
		snap      Snapshot
		tentativa int
		querOK    bool
		querEsper time.Duration
	}{
		// Resposta certa: nada a reinspecionar.
		{"ok livre", Snapshot{Outcome: InspectOK}, 0, false, 0},
		{"ok senha pela UIA", Snapshot{Outcome: InspectOK, IsPassword: true}, 0, false, 0},
		{"ok senha pelo estilo", Snapshot{Outcome: InspectOK, StyleHasPassword: true}, 0, false, 0},

		// Falha: qualquer chamada, qualquer HRESULT.
		{"E_FAIL no get_focused_element",
			Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)}, 0, true, 100 * time.Millisecond},
		{"UIA_E_ELEMENTNOTAVAILABLE",
			Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaEElementNotAvailable)}, 0, true, 100 * time.Millisecond},
		{"UIA_E_TIMEOUT",
			Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaETimeout)}, 0, true, 100 * time.Millisecond},
		{"falha no is_password",
			Snapshot{Outcome: InspectFailed, Falha: FalhaIsPassword, HRESULT: hr(eFail)}, 0, true, 100 * time.Millisecond},
		{"falha no control_type",
			Snapshot{Outcome: InspectFailed, Falha: FalhaControlType, HRESULT: hr(uiaEElementNotAvailable)}, 0, true, 100 * time.Millisecond},

		// Elemento nulo e timeout do orçamento: dúvida também.
		{"nil_element", Snapshot{Outcome: InspectNilElement}, 0, true, 100 * time.Millisecond},
		{"timeout do orçamento sem sinal de senha", Snapshot{Outcome: InspectTimeout}, 0, true, 100 * time.Millisecond},

		// Timeout que viu senha: a leitura atrasada já aponta para campo
		// de senha; reinspecionar só confirmaria o bloqueio.
		{"timeout que viu senha pela UIA", Snapshot{Outcome: InspectTimeout, IsPassword: true}, 0, false, 0},
		{"timeout que viu senha pelo estilo", Snapshot{Outcome: InspectTimeout, StyleHasPassword: true}, 0, false, 0},

		// Um Outcome que ninguém conhece é dúvida, como em Protected().
		{"outcome desconhecido", Snapshot{Outcome: InspectOutcome(99)}, 0, true, 100 * time.Millisecond},

		// A escada de esperas, e o fim dela.
		{"segunda reinspeção", Snapshot{Outcome: InspectNilElement}, 1, true, 300 * time.Millisecond},
		{"terceira reinspeção", Snapshot{Outcome: InspectNilElement}, 2, true, time.Second},
		{"não há quarta", Snapshot{Outcome: InspectNilElement}, 3, false, 0},
		{"muito além", Snapshot{Outcome: InspectFailed}, 100, false, 0},
		{"tentativa negativa não reinspeciona", Snapshot{Outcome: InspectFailed}, -1, false, 0},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			espera, ok := ProximaReinspecao(c.snap, rapida, c.tentativa)
			if ok != c.querOK || espera != c.querEsper {
				t.Errorf("ProximaReinspecao = (%v, %v), quero (%v, %v)", espera, ok, c.querEsper, c.querOK)
			}
		})
	}
}

// TestProximaReinspecao_TetoDeDuracao: uma inspeção que levou mais que
// TetoParaReinspecionar não volta ao provedor, seja qual for o desfecho.
// É o que impede uma chamada presa (60 s em 29-set-2026) de se repetir até
// três vezes por troca de foco, com a thread dedicada cega o tempo todo.
func TestProximaReinspecao_TetoDeDuracao(t *testing.T) {
	timeout := Snapshot{Outcome: InspectTimeout}
	casos := []struct {
		nome      string
		snap      Snapshot
		duracao   time.Duration
		tentativa int
		querOK    bool
		querEsper time.Duration
	}{
		// O que o teto separa, com os números do log de 29-set-2026.
		{"timeout de 515 ms reinspeciona", timeout, 515 * time.Millisecond, 0, true, 100 * time.Millisecond},
		{"timeout de um minuto não reinspeciona", timeout, time.Minute + 30*time.Millisecond, 0, false, 0},

		// A borda: no teto ainda pergunta de novo; um nanossegundo acima, não.
		{"exatamente no teto reinspeciona", timeout, TetoParaReinspecionar, 0, true, 100 * time.Millisecond},
		{"logo acima do teto não reinspeciona", timeout, TetoParaReinspecionar + time.Nanosecond, 0, false, 0},

		// Vale para qualquer dúvida, não só para o timeout do orçamento:
		// uma falha também pode ter levado um minuto para voltar.
		{"UIA_E_TIMEOUT depois de 20 s",
			Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaETimeout)}, 20 * time.Second, 0, false, 0},
		{"nil_element depois de um minuto", Snapshot{Outcome: InspectNilElement}, time.Minute, 0, false, 0},

		// E no meio da escada: a reinspeção que travou encerra a escada.
		{"reinspeção que travou não pede a próxima", timeout, time.Minute, 1, false, 0},
		{"reinspeção rápida segue a escada", timeout, rapida, 1, true, 300 * time.Millisecond},

		// Uma resposta certa continua sem reinspeção, lenta ou não.
		{"ok lento não reinspeciona", Snapshot{Outcome: InspectOK}, time.Minute, 0, false, 0},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			espera, ok := ProximaReinspecao(c.snap, c.duracao, c.tentativa)
			if ok != c.querOK || espera != c.querEsper {
				t.Errorf("ProximaReinspecao(%v, %v, %d) = (%v, %v), quero (%v, %v)",
					c.snap.Outcome, c.duracao, c.tentativa, espera, ok, c.querEsper, c.querOK)
			}
		})
	}
}

// TestProximaReinspecao_FalhaPermanenteNaoViraLaco: o E_FAIL do
// Gerenciador de Tarefas (elevado, fora do alcance do Agent) falha
// sempre. Três reinspeções e acabou.
func TestProximaReinspecao_FalhaPermanenteNaoViraLaco(t *testing.T) {
	falha := Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)}
	n, total := 0, time.Duration(0)
	for tentativa := 0; ; tentativa++ {
		espera, ok := ProximaReinspecao(falha, rapida, tentativa)
		if !ok {
			break
		}
		n++
		total += espera
		if n > 100 {
			t.Fatal("ProximaReinspecao não para numa falha permanente")
		}
	}
	if n != MaxReinspecoes || MaxReinspecoes != 3 {
		t.Errorf("%d reinspeções (MaxReinspecoes=%d), quero 3", n, MaxReinspecoes)
	}
	if total != 1400*time.Millisecond {
		t.Errorf("espera total = %v, quero 1,4 s (100 ms + 300 ms + 1 s)", total)
	}
}

// timerDuble faz o papel do SetTimer da thread dedicada: guarda o que
// foi agendado, e o teste decide quando "vence".
type timerDuble struct {
	agendados []time.Duration
	recusar   bool
}

func (d *timerDuble) agendar(espera time.Duration) bool {
	if d.recusar {
		return false
	}
	d.agendados = append(d.agendados, espera)
	return true
}

// inspecaoDuble devolve, a cada chamada, o próximo Snapshot da fila —
// e o último para sempre depois dela. Se trava[i] estiver preenchido, a
// chamada i (contando de 0) "prende" esse tempo: adianta o relógio antes
// de devolver.
type inspecaoDuble struct {
	fila   []Snapshot
	feitas int
	rel    *relogio
	trava  map[int]time.Duration
}

func (d *inspecaoDuble) inspecionar() Snapshot {
	i := d.feitas
	d.feitas++
	if presa, ok := d.trava[i]; ok {
		d.rel.agora = d.rel.agora.Add(presa)
	}
	if i >= len(d.fila) {
		i = len(d.fila) - 1
	}
	return d.fila[i]
}

type montagem struct {
	tr    *Tracker
	insp  *inspecaoDuble
	timer *timerDuble
	sis   sistema
	diags []DiagnosticoFoco
}

func montar(fila ...Snapshot) *montagem {
	rel := &relogio{agora: t0, passo: 5 * time.Millisecond}
	m := &montagem{
		tr:    NewTracker(Config{}),
		insp:  &inspecaoDuble{fila: fila, rel: rel, trava: map[int]time.Duration{}},
		timer: &timerDuble{},
	}
	m.sis = sistema{inspecionar: m.insp.inspecionar, agora: rel.ler, agendar: m.timer.agendar}
	m.tr.AoInspecionar(func(d DiagnosticoFoco) { m.diags = append(m.diags, d) })
	return m
}

func (m *montagem) ultimo(t *testing.T) DiagnosticoFoco {
	t.Helper()
	if len(m.diags) == 0 {
		t.Fatal("nenhuma inspeção relatada")
	}
	return m.diags[len(m.diags)-1]
}

// TestReinspecao_FalhaPassageiraLiberaSemTrocarDeJanela é o defeito de
// 29-set-2026: um UIA_E_ELEMENTNOTAVAILABLE deixava o cache protegido até
// a pessoa ir para outra janela e voltar. Agora a reinspeção 100 ms
// depois libera — e, até ela, o cache fica protegido.
//
// A pessoa vem de um campo livre: é o que dá dentes ao "até ela, fica
// protegido". Partindo do valor inicial de NewTracker, que já é
// protegido, a checagem passaria mesmo que a dúvida nunca fosse
// publicada (revisão da fase 1, mutante M1).
func TestReinspecao_FalhaPassageiraLiberaSemTrocarDeJanela(t *testing.T) {
	m := montar(
		campoLivre,
		Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaEElementNotAvailable)},
		campoLivre,
	)

	m.tr.inspecionarEPublicar(m.sis, 0, false) // o campo de onde a pessoa veio
	if m.tr.Current().Protected {
		t.Fatal("pré-condição: o campo livre não liberou o cache")
	}
	m.tr.inspecionarEPublicar(m.sis, 0, false) // a troca de foco que deu dúvida
	if !m.tr.Current().Protected {
		t.Fatal("a falha não protegeu o cache — na dúvida, protege")
	}
	if len(m.timer.agendados) != 1 || m.timer.agendados[0] != 100*time.Millisecond {
		t.Fatalf("agendou %v, quero [100ms]", m.timer.agendados)
	}
	if d := m.ultimo(t); d.ReinspecaoEm != 100*time.Millisecond || d.Tentativa != 0 {
		t.Errorf("diagnóstico da falha: reinspeciona_em=%v tentativa=%d, quero 100ms e 0", d.ReinspecaoEm, d.Tentativa)
	}

	m.tr.reinspecionar(m.sis) // o timer venceu

	if m.tr.Current().Protected {
		t.Fatal("a reinspeção OK não liberou o cache")
	}
	d := m.ultimo(t)
	if d.Tentativa != 1 || !d.Liberou || d.ProtegidoPor <= 0 || d.ReinspecaoEm != 0 {
		t.Errorf("diagnóstico da reinspeção = %+v, quero tentativa 1, liberou, protegido_por > 0, sem nova reinspeção", d)
	}
	if len(m.timer.agendados) != 1 {
		t.Errorf("a reinspeção OK agendou outra: %v", m.timer.agendados)
	}
}

// TestReinspecao_DoLivreParaADuvidaProtegeEnquantoReinspeciona trava a
// propriedade de segurança da reinspeção: enquanto ela está agendada, o
// cache já tem a dúvida — protegido — e não o "livre" do campo anterior.
//
// É o caso em que errar custa caro: a pessoa sai de um campo livre para
// um campo de senha cujo provedor devolveu UIA_E_ELEMENTNOTAVAILABLE. Se a
// dúvida só fosse publicada quando a escada acabasse, o "livre" velho
// valeria por até 1,4 s mais a duração das inspeções, e um ditado nesse
// intervalo iria para o campo de senha.
//
// Cada caso começa num campo livre — partir do valor inicial de
// NewTracker, que já é protegido, deixaria a checagem sem dentes.
func TestReinspecao_DoLivreParaADuvidaProtegeEnquantoReinspeciona(t *testing.T) {
	duvidas := []struct {
		nome string
		snap Snapshot
	}{
		{"UIA_E_ELEMENTNOTAVAILABLE", Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaEElementNotAvailable)}},
		{"UIA_E_TIMEOUT", Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaETimeout)}},
		{"E_FAIL", Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)}},
		{"falha no is_password", Snapshot{Outcome: InspectFailed, Falha: FalhaIsPassword, HRESULT: hr(eFail)}},
		{"nil_element", Snapshot{Outcome: InspectNilElement}},
		{"timeout do orçamento", Snapshot{Outcome: InspectTimeout}},
	}

	for _, c := range duvidas {
		t.Run(c.nome, func(t *testing.T) {
			m := montar(campoLivre, c.snap) // a dúvida se repete em toda reinspeção

			m.tr.inspecionarEPublicar(m.sis, 0, false)
			if m.tr.Current().Protected {
				t.Fatal("pré-condição: o campo livre não liberou o cache")
			}

			m.tr.inspecionarEPublicar(m.sis, 0, false)
			if len(m.timer.agendados) != 1 {
				t.Fatalf("agendou %v, quero uma reinspeção", m.timer.agendados)
			}
			if !m.tr.Current().Protected {
				t.Fatal("com a reinspeção agendada, o cache ficou com o livre do campo anterior")
			}

			for i := 1; i <= MaxReinspecoes; i++ {
				m.tr.reinspecionar(m.sis)
				if !m.tr.Current().Protected {
					t.Fatalf("a reinspeção %d, ainda em dúvida, liberou o cache", i)
				}
			}
		})
	}
}

// TestReinspecao_FalhaPermanenteParaNaTerceira: o E_FAIL da janela
// elevada falha sempre. O cache fica protegido o tempo todo, e depois da
// terceira reinspeção nenhum timer vencido inspeciona de novo.
func TestReinspecao_FalhaPermanenteParaNaTerceira(t *testing.T) {
	m := montar(Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)})

	m.tr.inspecionarEPublicar(m.sis, 0, false)
	for i := 0; i < 10; i++ { // o teste "vence" o timer muito mais vezes do que deveria haver
		m.tr.reinspecionar(m.sis)
		if !m.tr.Current().Protected {
			t.Fatalf("o cache foi liberado numa falha, depois de %d reinspeções", i+1)
		}
	}

	if m.insp.feitas != 1+MaxReinspecoes {
		t.Errorf("%d inspeções, quero %d (a original e três reinspeções)", m.insp.feitas, 1+MaxReinspecoes)
	}
	quero := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, time.Second}
	if len(m.timer.agendados) != len(quero) {
		t.Fatalf("agendou %v, quero %v", m.timer.agendados, quero)
	}
	for i := range quero {
		if m.timer.agendados[i] != quero[i] {
			t.Errorf("agendamento %d = %v, quero %v", i, m.timer.agendados[i], quero[i])
		}
	}
	if d := m.ultimo(t); d.Tentativa != MaxReinspecoes || d.ReinspecaoEm != 0 {
		t.Errorf("última: tentativa=%d reinspeciona_em=%v, quero %d e nenhuma", d.Tentativa, d.ReinspecaoEm, MaxReinspecoes)
	}
}

// TestReinspecao_TrocaDeFocoAntesDoTimerCancelaAReinspecao: se o foco
// mudou antes de o timer vencer, a inspeção da troca já respondeu. O
// timer vencido não inspeciona de novo.
func TestReinspecao_TrocaDeFocoAntesDoTimerCancelaAReinspecao(t *testing.T) {
	m := montar(
		Snapshot{Outcome: InspectNilElement},
		Snapshot{Outcome: InspectOK}, // a troca de foco
	)

	m.tr.inspecionarEPublicar(m.sis, 0, false) // dúvida, agenda
	m.tr.inspecionarEPublicar(m.sis, 0, false) // EVENT_OBJECT_FOCUS, OK
	m.tr.reinspecionar(m.sis)                  // o timer da primeira vence

	if m.insp.feitas != 2 {
		t.Errorf("%d inspeções, quero 2: o timer vencido não devia inspecionar", m.insp.feitas)
	}
	if m.tr.Current().Protected {
		t.Error("o cache devia estar com o OK da troca de foco")
	}
}

// TestReinspecao_TrocaDeFocoParaOutraDuvidaRecomecaAEscada: a dúvida da
// troca nova é outra dúvida, com escada própria — não herda as
// tentativas da anterior.
func TestReinspecao_TrocaDeFocoParaOutraDuvidaRecomecaAEscada(t *testing.T) {
	m := montar(Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)})

	m.tr.inspecionarEPublicar(m.sis, 0, false) // agenda 100ms
	m.tr.reinspecionar(m.sis)                  // tentativa 1, agenda 300ms
	m.tr.inspecionarEPublicar(m.sis, 0, false) // troca de foco, outra falha: agenda 100ms
	m.tr.reinspecionar(m.sis)                  // tentativa 1 da troca nova

	quero := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 100 * time.Millisecond, 300 * time.Millisecond}
	if len(m.timer.agendados) != len(quero) {
		t.Fatalf("agendou %v, quero %v", m.timer.agendados, quero)
	}
	for i := range quero {
		if m.timer.agendados[i] != quero[i] {
			t.Errorf("agendamento %d = %v, quero %v", i, m.timer.agendados[i], quero[i])
		}
	}
	if d := m.ultimo(t); d.Tentativa != 1 {
		t.Errorf("tentativa = %d, quero 1 (a escada recomeça na troca de foco)", d.Tentativa)
	}
}

// TestReinspecao_SemTimerFicaProtegidoEDizQueNaoAgendou: se o SetTimer
// falhar, nada de reinspeção — o cache fica protegido como antes desta
// mudança, e o log não promete uma reinspeção que não vem.
func TestReinspecao_SemTimerFicaProtegidoEDizQueNaoAgendou(t *testing.T) {
	m := montar(Snapshot{Outcome: InspectNilElement}, Snapshot{Outcome: InspectOK})
	m.timer.recusar = true

	m.tr.inspecionarEPublicar(m.sis, 0, false)
	if d := m.ultimo(t); d.ReinspecaoEm != 0 {
		t.Errorf("reinspeciona_em = %v sem timer", d.ReinspecaoEm)
	}
	m.tr.reinspecionar(m.sis) // um WM_TIMER perdido não pode reinspecionar
	if m.insp.feitas != 1 {
		t.Errorf("%d inspeções, quero 1", m.insp.feitas)
	}
	if !m.tr.Current().Protected {
		t.Error("sem reinspeção, o cache tem de continuar protegido")
	}
}

// TestReinspecao_RespostaCertaNaoAgenda: OK (livre ou senha) e o timeout
// que viu senha não agendam nada.
func TestReinspecao_RespostaCertaNaoAgenda(t *testing.T) {
	for _, s := range []Snapshot{
		{Outcome: InspectOK},
		{Outcome: InspectOK, IsPassword: true},
		{Outcome: InspectTimeout, IsPassword: true},
	} {
		m := montar(s)
		m.tr.inspecionarEPublicar(m.sis, 0, false)
		if len(m.timer.agendados) != 0 {
			t.Errorf("%+v agendou %v", s, m.timer.agendados)
		}
	}
}

// TestReinspecao_ADoStartTambemReinspeciona: a inspeção do Start que dá
// dúvida — quem sobe o Agent com o foco numa janela lenta — ganha a
// mesma escada.
func TestReinspecao_ADoStartTambemReinspeciona(t *testing.T) {
	m := montar(Snapshot{Outcome: InspectTimeout}, Snapshot{Outcome: InspectOK})
	m.tr.inspecionarEPublicar(m.sis, 0, true)
	if len(m.timer.agendados) != 1 {
		t.Fatalf("a inspeção do Start em dúvida agendou %v", m.timer.agendados)
	}
	m.tr.reinspecionar(m.sis)
	if m.tr.Current().Protected {
		t.Error("a reinspeção da inspeção do Start não liberou")
	}
	if d := m.ultimo(t); d.Inicial {
		t.Error("a reinspeção foi marcada como inicial; só a do Start é")
	}
}

// TestReinspecao_ChamadaPresaNaoVoltaAoProvedor é o 09:15:58 de
// 29-set-2026 com a reinspeção: a inspeção fica presa um minuto e volta
// como timeout. Reinspecionar 100 ms depois voltaria ao provedor que
// acabou de travar. Não reinspeciona — e o cache fica protegido, como
// antes da reinspeção existir.
func TestReinspecao_ChamadaPresaNaoVoltaAoProvedor(t *testing.T) {
	m := montar(campoLivre, Snapshot{Outcome: InspectTimeout, LentaEm: ChamadaGetFocusedElement, LentaDuracao: time.Minute})
	m.insp.trava[1] = time.Minute

	m.tr.inspecionarEPublicar(m.sis, 0, false)
	m.tr.inspecionarEPublicar(m.sis, 0, false) // presa um minuto

	d := m.ultimo(t)
	if d.Duracao < time.Minute {
		t.Fatalf("pré-condição: a inspeção durou %v, quero um minuto", d.Duracao)
	}
	if len(m.timer.agendados) != 0 || d.ReinspecaoEm != 0 {
		t.Errorf("agendou %v (reinspeciona_em=%v) depois de uma chamada presa", m.timer.agendados, d.ReinspecaoEm)
	}
	if !m.tr.Current().Protected {
		t.Error("o timeout não protegeu o cache — na dúvida, protege")
	}
	m.tr.reinspecionar(m.sis) // um WM_TIMER velho não pode reinspecionar
	if m.insp.feitas != 2 {
		t.Errorf("%d inspeções, quero 2", m.insp.feitas)
	}
}

// TestReinspecao_AReinspecaoQueTravaEncerraAEscada: a original falha
// rápido, e a própria reinspeção prende. O teto não evita essa trava,
// mas encerra a escada nela: no máximo uma trava a mais por troca de
// foco, não três.
func TestReinspecao_AReinspecaoQueTravaEncerraAEscada(t *testing.T) {
	m := montar(
		Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaEElementNotAvailable)},
		Snapshot{Outcome: InspectTimeout},
	)
	m.insp.trava[1] = time.Minute

	m.tr.inspecionarEPublicar(m.sis, 0, false) // falha rápida: agenda 100 ms
	for i := 0; i < 5; i++ {
		m.tr.reinspecionar(m.sis)
		if !m.tr.Current().Protected {
			t.Fatalf("o cache foi liberado numa dúvida, no disparo %d", i+1)
		}
	}

	if m.insp.feitas != 2 {
		t.Errorf("%d inspeções, quero 2: a reinspeção presa não pode pedir outra", m.insp.feitas)
	}
	if len(m.timer.agendados) != 1 {
		t.Errorf("agendou %v, quero só a primeira reinspeção", m.timer.agendados)
	}
	if d := m.ultimo(t); d.Tentativa != 1 || d.ReinspecaoEm != 0 {
		t.Errorf("última: tentativa=%d reinspeciona_em=%v, quero 1 e nenhuma", d.Tentativa, d.ReinspecaoEm)
	}
}

// TestReinspecao_NoProprioAgentNaoPublicaEEncerraAEscada: depois de uma
// dúvida, a pessoa abre o menu da bandeja antes de a reinspeção sair. O
// SetForegroundWindow da janela oculta põe o foco no próprio Agent, que o
// hook não vê (WINEVENT_SKIPOWNPROCESS) — mas a reinspeção pergunta pelo
// foco atual e lê o Agent. Ela não pode publicar esse "livre": o cache
// fica com a dúvida, e a escada acaba. Quando a pessoa volta ao
// aplicativo, o evento de foco dele inspeciona de novo.
func TestReinspecao_NoProprioAgentNaoPublicaEEncerraAEscada(t *testing.T) {
	// O PID deste processo — em produção, o do Agent (ver NewTracker).
	agent := uint32(os.Getpid())
	m := montar(
		campoLivre,
		Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(uiaEElementNotAvailable)},
		Snapshot{Outcome: InspectOK, ProcessID: agent},     // a reinspeção lê o menu da bandeja
		Snapshot{Outcome: InspectOK, ProcessID: agent + 1}, // a volta ao aplicativo
	)

	m.tr.inspecionarEPublicar(m.sis, 0, false)
	m.tr.inspecionarEPublicar(m.sis, 0, false) // dúvida: agenda 100 ms
	m.tr.reinspecionar(m.sis)                  // lê o próprio Agent

	if !m.tr.Current().Protected {
		t.Fatal("a reinspeção publicou o livre do próprio Agent por cima da dúvida")
	}
	d := m.ultimo(t)
	if !d.NoProprioAgent || d.Liberou || d.ReinspecaoEm != 0 || d.Tentativa != 1 {
		t.Errorf("diagnóstico = %+v, quero no_proprio_agent, sem liberou, sem nova reinspeção, tentativa 1", d)
	}
	if len(m.timer.agendados) != 1 {
		t.Errorf("agendou %v, quero só a primeira reinspeção", m.timer.agendados)
	}
	m.tr.reinspecionar(m.sis) // um WM_TIMER velho não reinspeciona
	if m.insp.feitas != 3 {
		t.Errorf("%d inspeções, quero 3: a escada acabou no próprio Agent", m.insp.feitas)
	}

	m.tr.inspecionarEPublicar(m.sis, 0, false) // EVENT_OBJECT_FOCUS da volta ao aplicativo
	if m.tr.Current().Protected {
		t.Fatal("a volta ao aplicativo não liberou o cache")
	}
	if d := m.ultimo(t); !d.Liberou || d.NoProprioAgent {
		t.Errorf("volta ao aplicativo: %+v, quero liberou e sem no_proprio_agent", d)
	}
}

// TestReinspecao_ProprioAgentSoContaNaReinspecao: a regra do próprio
// Agent é da reinspeção, que é o que esta fase acrescentou. A inspeção de
// uma troca de foco (tentativa 0) publica como antes.
func TestReinspecao_ProprioAgentSoContaNaReinspecao(t *testing.T) {
	m := montar(Snapshot{Outcome: InspectOK, ProcessID: uint32(os.Getpid())})
	m.tr.inspecionarEPublicar(m.sis, 0, false)
	if m.tr.Current().Protected {
		t.Error("a inspeção de uma troca de foco deixou de publicar")
	}
	if m.ultimo(t).NoProprioAgent {
		t.Error("a inspeção de uma troca de foco foi marcada como no_proprio_agent")
	}
}
