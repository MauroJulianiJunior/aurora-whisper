package uia

import (
	"os"
	"testing"
	"time"
)

// relogio é um relógio de teste: cada leitura avança passo.
type relogio struct {
	agora time.Time
	passo time.Duration
}

func (r *relogio) ler() time.Time {
	t := r.agora
	r.agora = r.agora.Add(r.passo)
	return t
}

var t0 = time.Date(2026, 9, 29, 9, 14, 58, 0, time.UTC)

// Respostas certas da UIA — nenhuma das duas é dúvida, então nenhuma
// agenda reinspeção.
var (
	campoLivre = Snapshot{Outcome: InspectOK}
	campoSenha = Snapshot{Outcome: InspectOK, IsPassword: true}
)

func respostaCerta(protegido bool) Snapshot {
	if protegido {
		return campoSenha
	}
	return campoLivre
}

// semTimer monta o sistema dublê de quem não liga para a reinspeção:
// agendar nil nunca consegue agendar.
func semTimer(inspecionar func() Snapshot, rel *relogio) sistema {
	return sistema{inspecionar: inspecionar, agora: rel.ler}
}

// TestCicloFoco_Publicar_ContaQuantoTempoFicouProtegido: o log de
// 29-set-2026 não dizia quanto tempo cada WARN deixou o ditado recusado,
// porque a volta a livre saía em Debug. publicar diz, e conta da PRIMEIRA
// publicação protegida da sequência.
func TestCicloFoco_Publicar_ContaQuantoTempoFicouProtegido(t *testing.T) {
	type pub struct {
		protegido bool
		em        time.Duration // desde t0
	}
	casos := []struct {
		nome        string
		pubs        []pub
		querLiberou bool          // na última publicação
		querPor     time.Duration // na última publicação
	}{
		{
			// O cache nasce protegido em NewTracker, mas isso não veio de
			// inspeção nenhuma: a inspeção do Start que libera não é uma
			// "volta" a registrar.
			nome:        "primeira publicação livre não é volta",
			pubs:        []pub{{false, 0}},
			querLiberou: false,
		},
		{
			nome:        "livre depois de livre",
			pubs:        []pub{{false, 0}, {false, time.Second}},
			querLiberou: false,
		},
		{
			nome:        "protegido depois de livre ainda não libera",
			pubs:        []pub{{false, 0}, {true, time.Second}},
			querLiberou: false,
		},
		{
			nome:        "volta a livre depois de um minuto",
			pubs:        []pub{{false, 0}, {true, time.Second}, {false, 61 * time.Second}},
			querLiberou: true,
			querPor:     time.Minute,
		},
		{
			nome: "conta da primeira protegida, não da última",
			pubs: []pub{
				{true, 0},
				{true, 10 * time.Second},
				{true, 20 * time.Second},
				{false, 30 * time.Second},
			},
			querLiberou: true,
			querPor:     30 * time.Second,
		},
		{
			nome: "a segunda sequência conta do zero",
			pubs: []pub{
				{true, 0},
				{false, 5 * time.Second},
				{true, 100 * time.Second},
				{false, 102 * time.Second},
			},
			querLiberou: true,
			querPor:     2 * time.Second,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var ciclo cicloFoco
			var liberou bool
			var por time.Duration
			for _, p := range c.pubs {
				liberou, por = ciclo.publicar(p.protegido, t0.Add(p.em))
			}
			if liberou != c.querLiberou || por != c.querPor {
				t.Errorf("liberou=%v por=%v, quero liberou=%v por=%v", liberou, por, c.querLiberou, c.querPor)
			}
		})
	}
}

// TestDevePublicar: uma inspeção só publica se nenhuma mais nova começou
// depois dela.
func TestDevePublicar(t *testing.T) {
	casos := []struct {
		nome          string
		inicio, atual uint64
		quer          bool
	}{
		{"nenhuma começou depois", 5, 5, true},
		{"uma começou depois", 5, 6, false},
		{"muitas começaram depois", 1, 1000, false},
		{"a primeira de todas", 1, 1, true},
	}
	for _, c := range casos {
		if got := devePublicar(c.inicio, c.atual); got != c.quer {
			t.Errorf("%s: devePublicar(%d, %d) = %v, quero %v", c.nome, c.inicio, c.atual, got, c.quer)
		}
	}
}

// TestDoProprioAgent: só um PID lido de verdade e igual ao do Agent. Um
// ProcessID zero é leitura que falhou (ou elemento nenhum), nunca o Agent.
func TestDoProprioAgent(t *testing.T) {
	casos := []struct {
		nome        string
		pid, agente uint32
		quer        bool
	}{
		{"elemento do Agent", 4242, 4242, true},
		{"elemento de outro processo", 4243, 4242, false},
		{"PID não lido", 0, 4242, false},
		{"PID não lido e Agent sem PID", 0, 0, false},
	}
	for _, c := range casos {
		if got := doProprioAgent(Snapshot{ProcessID: c.pid}, c.agente); got != c.quer {
			t.Errorf("%s: doProprioAgent(%d, %d) = %v, quero %v", c.nome, c.pid, c.agente, got, c.quer)
		}
	}
}

// TestNewTracker_SabeOProprioPID: sem o PID do Agent, a reinspeção que
// cai no menu da bandeja publicaria livre.
func TestNewTracker_SabeOProprioPID(t *testing.T) {
	if got, quero := NewTracker(Config{}).ciclo.pidProprio, uint32(os.Getpid()); got != quero {
		t.Errorf("pidProprio = %d, quero %d", got, quero)
	}
}

// TestCicloFoco_AMaisAntigaChegaDepoisEDescartada: com reentrância, a
// inspeção que começou primeiro pode terminar por último. Ela não publica
// — o elemento que ela leu já não tem o foco.
func TestCicloFoco_AMaisAntigaChegaDepoisEDescartada(t *testing.T) {
	var ciclo cicloFoco
	velha := ciclo.comecar()
	nova := ciclo.comecar() // o próximo foco, rodando DENTRO da velha

	if d := ciclo.concluir(nova, 0, campoSenha, 0, t0); d.Descartada {
		t.Fatal("a inspeção mais nova foi descartada")
	}
	d := ciclo.concluir(velha, 0, campoLivre, 0, t0.Add(time.Minute))
	if !d.Descartada {
		t.Fatal("a inspeção mais antiga, que chegou depois, publicou por cima da nova")
	}
	if d.Liberou {
		t.Error("uma inspeção descartada contou volta a livre")
	}
	if !ciclo.publicouProtegido {
		t.Error("a descartada mexeu no estado da última publicação")
	}
}

// TestCicloFoco_Disparar: a reinspeção pendente só sai se nenhuma outra
// inspeção começou desde a que a pediu, sai uma vez só, e uma inspeção
// descartada não mexe nela.
func TestCicloFoco_Disparar(t *testing.T) {
	duvida := Snapshot{Outcome: InspectNilElement}

	t.Run("sem pendência não dispara", func(t *testing.T) {
		var ciclo cicloFoco
		if _, ok := ciclo.disparar(); ok {
			t.Error("disparou sem nada pendente")
		}
	})

	t.Run("dispara uma vez só, com a tentativa seguinte", func(t *testing.T) {
		var ciclo cicloFoco
		g := ciclo.comecar()
		ciclo.concluir(g, 0, duvida, 0, t0)
		tentativa, ok := ciclo.disparar()
		if !ok || tentativa != 1 {
			t.Fatalf("disparar = (%d, %v), quero (1, true)", tentativa, ok)
		}
		if _, ok := ciclo.disparar(); ok {
			t.Error("o mesmo timer disparou duas reinspeções")
		}
	})

	t.Run("outra inspeção começou: não dispara", func(t *testing.T) {
		var ciclo cicloFoco
		g := ciclo.comecar()
		ciclo.concluir(g, 0, duvida, 0, t0)
		ciclo.comecar() // o foco mudou; a inspeção dele está em curso
		if _, ok := ciclo.disparar(); ok {
			t.Error("disparou a reinspeção de um foco que já não é o atual")
		}
	})

	t.Run("resposta certa depois da dúvida cancela", func(t *testing.T) {
		var ciclo cicloFoco
		g := ciclo.comecar()
		ciclo.concluir(g, 0, duvida, 0, t0)
		g = ciclo.comecar()
		ciclo.concluir(g, 0, campoLivre, 0, t0)
		if _, ok := ciclo.disparar(); ok {
			t.Error("disparou depois de uma resposta certa")
		}
	})

	t.Run("descartada não mexe na pendência da mais nova", func(t *testing.T) {
		var ciclo cicloFoco
		velha := ciclo.comecar()
		nova := ciclo.comecar()
		ciclo.concluir(nova, 0, duvida, 0, t0)      // publica e agenda
		ciclo.concluir(velha, 0, campoLivre, 0, t0) // descartada
		if tentativa, ok := ciclo.disparar(); !ok || tentativa != 1 {
			t.Errorf("disparar = (%d, %v), quero (1, true): a descartada apagou a pendência", tentativa, ok)
		}
	})

	t.Run("cancelar desiste", func(t *testing.T) {
		var ciclo cicloFoco
		g := ciclo.comecar()
		ciclo.concluir(g, 0, duvida, 0, t0)
		ciclo.cancelarReinspecao()
		if _, ok := ciclo.disparar(); ok {
			t.Error("disparou depois de cancelada")
		}
	})
}

// TestCicloFoco_SemReentranciaNadaEDescartado registra o limite da guarda
// (contestação da investigação de 29-set-2026, item 4): na thread única,
// sem reentrância, cada inspeção termina antes de a próxima começar, e a
// guarda não descarta nada — nem uma leitura que levou um minuto.
func TestCicloFoco_SemReentranciaNadaEDescartado(t *testing.T) {
	var ciclo cicloFoco
	agora := t0
	for i, prot := range []bool{false, true, false, true, true, false} {
		g := ciclo.comecar()
		agora = agora.Add(time.Minute)
		if d := ciclo.concluir(g, 0, respostaCerta(prot), time.Minute, agora); d.Descartada {
			t.Fatalf("inspeção %d descartada sem reentrância", i)
		}
	}
}

// TestInspecionarEPublicar_ReentranciaNaoDeixaOVelhoSobrescrever roda a
// sequência inteira do Tracker com uma inspeção dublê que, no meio da
// chamada, dispara a inspeção do próximo foco — o que a espera de uma
// chamada COM entre processos pode fazer numa thread que bombeia
// mensagens. O cache tem de ficar com o resultado da mais nova, nos dois
// sentidos.
func TestInspecionarEPublicar_ReentranciaNaoDeixaOVelhoSobrescrever(t *testing.T) {
	casos := []struct {
		nome          string
		velha, nova   Snapshot
		querProtegido bool
	}{
		{
			// O sentido que importa para a segurança: a pessoa foi para o
			// campo de senha enquanto a leitura do campo anterior estava
			// presa. O "livre" velho não pode liberar o ditado ali.
			nome:          "livre velho não sobrescreve campo de senha novo",
			velha:         Snapshot{Outcome: InspectOK},
			nova:          Snapshot{Outcome: InspectOK, IsPassword: true},
			querProtegido: true,
		},
		{
			nome:          "timeout velho não sobrescreve livre novo",
			velha:         Snapshot{Outcome: InspectTimeout},
			nova:          Snapshot{Outcome: InspectOK},
			querProtegido: false,
		},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rel := &relogio{agora: t0, passo: time.Millisecond}
			tr := NewTracker(Config{})
			var diags []DiagnosticoFoco
			tr.AoInspecionar(func(d DiagnosticoFoco) { diags = append(diags, d) })

			nova := func() Snapshot { return c.nova }
			velha := func() Snapshot {
				tr.inspecionarEPublicar(semTimer(nova, rel), 0, false) // reentrada
				return c.velha
			}
			tr.inspecionarEPublicar(semTimer(velha, rel), 0, false)

			if got := tr.Current().Protected; got != c.querProtegido {
				t.Errorf("Protected = %v, quero %v (o resultado da inspeção mais nova)", got, c.querProtegido)
			}
			if len(diags) != 2 {
				t.Fatalf("relatou %d inspeções, quero 2", len(diags))
			}
			if diags[0].Descartada {
				t.Error("a mais nova (relatada primeiro) foi marcada como descartada")
			}
			if !diags[1].Descartada {
				t.Error("a mais antiga (relatada por último) não foi marcada como descartada")
			}
		})
	}
}

// TestInspecionarEPublicar_RelataAVoltaALivre exercita a sequência
// inteira da thread dedicada — inspeciona, publica, relata — com uma
// inspeção dublê e um relógio de teste: o WARN deixa o cache protegido,
// e a inspeção que o libera sai com quanto tempo ele ficou assim.
func TestInspecionarEPublicar_RelataAVoltaALivre(t *testing.T) {
	rel := &relogio{agora: t0, passo: 10 * time.Millisecond}
	tr := NewTracker(Config{})
	var diags []DiagnosticoFoco
	tr.AoInspecionar(func(d DiagnosticoFoco) { diags = append(diags, d) })

	ok := func() Snapshot { return Snapshot{Outcome: InspectOK} }
	timeout := func() Snapshot { return Snapshot{Outcome: InspectTimeout} }

	tr.inspecionarEPublicar(semTimer(ok, rel), 0, true) // leituras em t0, t0+10ms
	if tr.Current().Protected {
		t.Fatal("a inspeção OK do Start não liberou o cache")
	}

	tr.inspecionarEPublicar(semTimer(timeout, rel), 0, false) // t0+20ms, t0+30ms: protege em +30ms
	if !tr.Current().Protected {
		t.Fatal("timeout não protegeu o cache — na dúvida, protege")
	}

	rel.agora = rel.agora.Add(12 * time.Second)
	tr.inspecionarEPublicar(semTimer(ok, rel), 0, false) // +12.04s, +12.05s: libera em +12.05s
	if tr.Current().Protected {
		t.Fatal("a inspeção OK não liberou o cache")
	}

	if len(diags) != 3 {
		t.Fatalf("relatou %d inspeções, quero 3", len(diags))
	}
	if diags[0].Liberou || diags[1].Liberou {
		t.Errorf("só a terceira inspeção libera: %+v / %+v", diags[0], diags[1])
	}
	querPor := 12*time.Second + 20*time.Millisecond
	if !diags[2].Liberou || diags[2].ProtegidoPor != querPor {
		t.Errorf("volta a livre: liberou=%v por=%v, quero true por %v",
			diags[2].Liberou, diags[2].ProtegidoPor, querPor)
	}
	if diags[2].Duracao != 10*time.Millisecond {
		t.Errorf("Duracao = %v, quero 10ms (entre as duas leituras do relógio)", diags[2].Duracao)
	}
	if !diags[0].Inicial || diags[1].Inicial || diags[2].Inicial {
		t.Error("só a inspeção do Start é inicial")
	}
}
