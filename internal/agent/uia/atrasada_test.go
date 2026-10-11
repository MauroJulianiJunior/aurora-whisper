package uia

import (
	"testing"
	"time"
)

// TestClassificarLeitura cobre as três condições do item 3 das decisões do
// Mauro (29-set-2026) para uma leitura completa acima do orçamento valer:
// (a) as essenciais responderam — é o que faz a leitura chegar a
// classificarLeitura —, (b) o sinal ES_PASSWORD rodou, (c) até 2 s.
func TestClassificarLeitura(t *testing.T) {
	const orcamento = 500 * time.Millisecond
	casos := []struct {
		nome         string
		estiloLido   bool
		duracao      time.Duration
		orcamento    time.Duration
		querOutcome  InspectOutcome
		querAtrasada bool
	}{
		// Dentro do orçamento, nada muda — nem com o hwnd sem ler, que é
		// o comportamento de antes (o fallback só não se aplica).
		{"rápida", true, 30 * time.Millisecond, orcamento, InspectOK, false},
		{"rápida sem o estilo lido", false, 30 * time.Millisecond, orcamento, InspectOK, false},
		{"exatamente no orçamento", true, orcamento, orcamento, InspectOK, false},

		// O 09:20:32 de 29-set-2026: 515 ms, is_password=false lido.
		{"515 ms completa vale", true, 515 * time.Millisecond, orcamento, InspectOK, true},
		// Sem o segundo sinal, uma leitura atrasada não vale: um Edit
		// clássico com ES_PASSWORD e IsPassword errado passaria livre.
		{"515 ms sem o estilo lido não vale", false, 515 * time.Millisecond, orcamento, InspectTimeout, false},

		// A borda do teto — e o número da decisão, escrito por extenso, para
		// que mexer na constante não passe calado.
		{"1,9 s completa vale", true, 1900 * time.Millisecond, orcamento, InspectOK, true},
		{"exatamente 2 s vale", true, TetoLeituraAtrasada, orcamento, InspectOK, true},
		{"logo acima de 2 s não vale", true, TetoLeituraAtrasada + time.Nanosecond, orcamento, InspectTimeout, false},
		{"2,5 s completa não vale", true, 2500 * time.Millisecond, orcamento, InspectTimeout, false},
		// As duas de um minuto de 29-set-2026 continuam protegidas.
		{"um minuto não vale", true, time.Minute, orcamento, InspectTimeout, false},

		// Orçamento maior que o teto: o orçamento continua mandando.
		{"dentro de um orçamento de 5 s", true, 3 * time.Second, 5 * time.Second, InspectOK, false},
		{"acima de um orçamento de 5 s", true, 6 * time.Second, 5 * time.Second, InspectTimeout, false},

		// Orçamento desligado (só em teste): tudo vale, como antes.
		{"sem orçamento", false, time.Hour, 0, InspectOK, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			o, atrasada := classificarLeitura(c.estiloLido, c.duracao, c.orcamento)
			if o != c.querOutcome || atrasada != c.querAtrasada {
				t.Errorf("classificarLeitura(%v, %v, %v) = (%v, %v), quero (%v, %v)",
					c.estiloLido, c.duracao, c.orcamento, o, atrasada, c.querOutcome, c.querAtrasada)
			}
		})
	}
}

// TestLeituraAtrasada_SenhaContinuaProtegida: valer não é liberar. Uma
// leitura atrasada que viu senha — por qualquer um dos dois sinais — é
// campo de senha.
func TestLeituraAtrasada_SenhaContinuaProtegida(t *testing.T) {
	for _, s := range []Snapshot{
		{Outcome: InspectOK, Atrasada: true, EstiloLido: true, IsPassword: true},
		{Outcome: InspectOK, Atrasada: true, EstiloLido: true, StyleHasPassword: true},
	} {
		if !s.Protected() {
			t.Errorf("%+v não ficou protegido", s)
		}
	}
	if (Snapshot{Outcome: InspectOK, Atrasada: true, EstiloLido: true}).Protected() {
		t.Error("a leitura atrasada completa e sem senha não liberou")
	}
}

// TestProximaReinspecao_LeituraCompletaAcimaDe2s: acima do teto, a leitura
// completa fica protegida e pede uma leitura NOVA já — mesmo acima do teto
// de duração da escada, porque o provedor respondeu tudo.
func TestProximaReinspecao_LeituraCompletaAcimaDe2s(t *testing.T) {
	completa := Snapshot{Outcome: InspectTimeout, EstiloLido: true}
	incompleta := Snapshot{Outcome: InspectTimeout}
	casos := []struct {
		nome      string
		snap      Snapshot
		duracao   time.Duration
		tentativa int
		querOK    bool
		querEsper time.Duration
	}{
		{"completa de 2,5 s: já", completa, 2500 * time.Millisecond, 0, true, esperaReinspecaoImediata},
		{"completa de um minuto: já", completa, time.Minute, 0, true, esperaReinspecaoImediata},
		{"completa na reinspeção: já", completa, 3 * time.Second, 1, true, esperaReinspecaoImediata},
		// A escada continua limitada: não vira laço num provedor que é
		// sempre lento.
		{"completa depois da terceira: não", completa, 3 * time.Second, MaxReinspecoes, false, 0},
		// Viu senha: reinspecionar só confirmaria o bloqueio.
		{"completa que viu senha: não", Snapshot{Outcome: InspectTimeout, EstiloLido: true, IsPassword: true},
			3 * time.Second, 0, false, 0},
		// Sem o estilo lido, é o timeout de antes: escada e teto de duração.
		{"incompleta de 2,5 s: não (teto)", incompleta, 2500 * time.Millisecond, 0, false, 0},
		{"incompleta de 515 ms: escada", incompleta, 515 * time.Millisecond, 0, true, 100 * time.Millisecond},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			espera, ok := ProximaReinspecao(c.snap, c.duracao, c.tentativa)
			if ok != c.querOK || espera != c.querEsper {
				t.Errorf("ProximaReinspecao = (%v, %v), quero (%v, %v)", espera, ok, c.querEsper, c.querOK)
			}
		})
	}
}

// TestLeituraAtrasada_ValeNoCacheEAcimaDe2sReinspecionaJa roda as duas
// pontas pela sequência inteira do Tracker: a leitura atrasada que vale
// libera o cache e sai marcada no diagnóstico; a que passou de 2 s deixa
// o cache protegido e agenda a leitura nova, que libera.
func TestLeituraAtrasada_ValeNoCacheEAcimaDe2sReinspecionaJa(t *testing.T) {
	t.Run("vale", func(t *testing.T) {
		m := montar(Snapshot{Outcome: InspectOK, Atrasada: true, EstiloLido: true})
		m.tr.inspecionarEPublicar(m.sis, 0, false)
		if m.tr.Current().Protected {
			t.Fatal("a leitura atrasada que vale não liberou o cache")
		}
		if d := m.ultimo(t); !d.Atrasada || d.ReinspecaoEm != 0 {
			t.Errorf("diagnóstico = %+v, quero atrasada e sem reinspeção", d)
		}
	})

	t.Run("acima de 2 s", func(t *testing.T) {
		m := montar(campoLivre, Snapshot{Outcome: InspectTimeout, EstiloLido: true}, campoLivre)
		m.insp.trava[1] = 3 * time.Second
		m.tr.inspecionarEPublicar(m.sis, 0, false)
		m.tr.inspecionarEPublicar(m.sis, 0, false)
		if !m.tr.Current().Protected {
			t.Fatal("a leitura completa acima de 2 s não protegeu o cache")
		}
		if len(m.timer.agendados) != 1 || m.timer.agendados[0] != esperaReinspecaoImediata {
			t.Fatalf("agendou %v, quero [%v]", m.timer.agendados, esperaReinspecaoImediata)
		}
		m.tr.reinspecionar(m.sis)
		if m.tr.Current().Protected {
			t.Error("a leitura nova não liberou o cache")
		}
	})
}
