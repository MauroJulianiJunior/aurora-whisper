package uia

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

// vigiaArmado é um vigia que a inspeção armou: o teste decide quando ele
// "vence" — em geral de dentro da inspeção presa, que é quando o timer de
// verdade venceria.
type vigiaArmado struct {
	espera time.Duration
	f      func()
	ativo  bool
	parado bool
}

// vigiaDuble faz o papel de vigiarComTimer: guarda cada vigia armado, na
// ordem, sem timer nenhum.
type vigiaDuble struct {
	armados []*vigiaArmado
}

func (v *vigiaDuble) vigiar(espera time.Duration, f func()) func() {
	a := &vigiaArmado{espera: espera, f: f, ativo: true}
	v.armados = append(v.armados, a)
	return func() {
		a.ativo = false
		a.parado = true
	}
}

// vencer dispara o i-ésimo vigia armado, se ele ainda não foi parado — como
// o timer faria ao chegar no orçamento.
func (v *vigiaDuble) vencer(t *testing.T, i int) {
	t.Helper()
	if i >= len(v.armados) {
		t.Fatalf("vigia %d não foi armado (armados: %d)", i, len(v.armados))
	}
	a := v.armados[i]
	if a.ativo {
		a.ativo = false
		a.f()
	}
}

// TestVigia_ChamadaPresaProtegeOCacheAteTerminar é a decisão 1 do Mauro
// (29-set-2026): enquanto uma inspeção passa do orçamento sem terminar, o
// cache fica protegido — e não com o "livre" do campo anterior, que é o
// buraco que a nota de 29-set-2026 da ADR-011 registrou (60 s com um livre
// velho valendo). Quando ela termina, publica o que leu.
//
// A pessoa vem de um campo livre: partindo do protegido inicial de
// NewTracker, "ficou protegido durante" passaria sem vigia nenhum.
func TestVigia_ChamadaPresaProtegeOCacheAteTerminar(t *testing.T) {
	casos := []struct {
		nome          string
		resultado     Snapshot
		querProtegido bool
	}{
		// O sentido que importa para a segurança: a chamada presa era a do
		// campo de senha. Sem o vigia, o livre do campo anterior valeria
		// durante o minuto inteiro.
		{"termina num campo de senha", campoSenha, true},
		// E o vigia não prende o cache: terminou livre, fica livre.
		{"termina num campo livre", campoLivre, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			rel := &relogio{agora: t0, passo: time.Millisecond}
			v := &vigiaDuble{}
			tr := NewTracker(Config{})
			var diags []DiagnosticoFoco
			tr.AoInspecionar(func(d DiagnosticoFoco) { diags = append(diags, d) })

			livre := func() Snapshot { return campoLivre }
			tr.inspecionarEPublicar(sistema{inspecionar: livre, agora: rel.ler, vigiar: v.vigiar}, 0, false)
			if tr.Current().Protected {
				t.Fatal("pré-condição: o campo livre não liberou o cache")
			}

			var antesDoOrcamento, depoisDoOrcamento bool
			presa := func() Snapshot {
				antesDoOrcamento = tr.Current().Protected
				rel.agora = rel.agora.Add(time.Minute) // a chamada fica presa um minuto
				v.vencer(t, 1)                         // e o orçamento vence no meio dela
				depoisDoOrcamento = tr.Current().Protected
				return c.resultado
			}
			tr.inspecionarEPublicar(sistema{inspecionar: presa, agora: rel.ler, vigiar: v.vigiar}, 0, false)

			if antesDoOrcamento {
				t.Error("o cache ficou protegido antes de o orçamento vencer")
			}
			if !depoisDoOrcamento {
				t.Fatal("com a chamada presa além do orçamento, o cache ficou com o livre do campo anterior")
			}
			if got := tr.Current().Protected; got != c.querProtegido {
				t.Errorf("depois da inspeção: Protected = %v, quero %v (o resultado dela)", got, c.querProtegido)
			}
			d := diags[len(diags)-1]
			if !d.Vigia {
				t.Error("o diagnóstico não diz que o vigia protegeu o cache")
			}
			// Leituras do relógio: t0 e t0+1ms na primeira inspeção; t0+2ms
			// (início) e t0+3ms+1min (fim) na presa. O vigia protegeu em
			// início+500ms.
			if !c.querProtegido {
				querPor := time.Minute + 3*time.Millisecond - 502*time.Millisecond
				if !d.Liberou || d.ProtegidoPor != querPor {
					t.Errorf("volta a livre: liberou=%v por=%v, quero true por %v — o intervalo do vigia conta",
						d.Liberou, d.ProtegidoPor, querPor)
				}
			}
		})
	}
}

// TestVigia_ArmaNoOrcamentoEDesarmaSempre: o vigia é armado com o
// orçamento da Config e desarmado ao fim de TODA inspeção — um vigia
// esquecido armado protegeria o cache depois de a inspeção já ter
// publicado. Numa inspeção rápida ele não chega a agir.
func TestVigia_ArmaNoOrcamentoEDesarmaSempre(t *testing.T) {
	rel := &relogio{agora: t0, passo: time.Millisecond}
	v := &vigiaDuble{}
	tr := NewTracker(Config{InspectBudget: 250 * time.Millisecond})
	var diags []DiagnosticoFoco
	tr.AoInspecionar(func(d DiagnosticoFoco) { diags = append(diags, d) })

	for _, s := range []Snapshot{campoLivre, {Outcome: InspectFailed, Falha: FalhaGetFocusedElement}, campoSenha} {
		s := s
		tr.inspecionarEPublicar(sistema{inspecionar: func() Snapshot { return s }, agora: rel.ler, vigiar: v.vigiar}, 0, false)
	}

	if len(v.armados) != 3 {
		t.Fatalf("armou %d vigias, quero um por inspeção (3)", len(v.armados))
	}
	for i, a := range v.armados {
		if a.espera != 250*time.Millisecond {
			t.Errorf("vigia %d armado para %v, quero o orçamento (250ms)", i, a.espera)
		}
		if !a.parado {
			t.Errorf("vigia %d não foi desarmado no fim da inspeção", i)
		}
	}
	for i, d := range diags {
		if d.Vigia {
			t.Errorf("inspeção %d: Vigia=true sem o vigia ter vencido", i)
		}
	}
}

// TestProtegerSeAindaFor_NaoSobrescreveResultadoNovo é a corrida que a
// contestação da investigação de 29-set-2026 apontou (TOCTOU): se o vigia
// checasse um campo à parte e gravasse depois, a inspeção podia publicar
// entre as duas coisas — e o protegido do vigia ficaria por cima do
// resultado novo, preso até a próxima troca de foco. O CAS no próprio
// ponteiro do cache fecha isso.
func TestProtegerSeAindaFor_NaoSobrescreveResultadoNovo(t *testing.T) {
	tr := NewTracker(Config{})
	tr.store(estadoFoco{alvo: dictation.FocusTarget{ProcessName: "anterior.exe"}})
	observado := tr.cache.Load() // o que o vigia viu no início da inspeção

	// A inspeção publicou antes do vigia.
	tr.store(estadoFoco{alvo: dictation.FocusTarget{ProcessName: "novo.exe"}})

	if tr.protegerSeAindaFor(observado) {
		t.Error("o vigia disse que protegeu um cache que já tinha o resultado novo")
	}
	if got := tr.Current(); got.Protected || got.ProcessName != "novo.exe" {
		t.Fatalf("Current() = %+v, quero o resultado novo intacto", got)
	}

	// E quando o cache ainda é o observado, protege.
	observado = tr.cache.Load()
	if !tr.protegerSeAindaFor(observado) {
		t.Error("o vigia não protegeu um cache que ainda era o observado")
	}
	if !tr.Current().Protected {
		t.Error("Current() não ficou protegido")
	}
}

// TestProtegerSeAindaFor_JaProtegidoFicaComoEsta: não há livre velho a
// cobrir, e trocar um protegido por outro só apagaria o que ele diz.
func TestProtegerSeAindaFor_JaProtegidoFicaComoEsta(t *testing.T) {
	tr := NewTracker(Config{}) // nasce protegido
	observado := tr.cache.Load()
	if tr.protegerSeAindaFor(observado) {
		t.Error("o vigia trocou um cache que já era protegido")
	}
	if tr.cache.Load() != observado {
		t.Error("o ponteiro do cache mudou")
	}
	if tr.protegerSeAindaFor(nil) {
		t.Error("o vigia trocou a partir de um observado nulo")
	}

	// O caso em que isso importa: uma dúvida numa janela de administrador
	// cuja reinspeção passa do orçamento. O vigia não pode apagar a marca —
	// a pílula voltaria a dizer "campo protegido".
	tr.store(estadoFoco{alvo: dictation.FocusTarget{Protected: true}, janelaElevada: true})
	tr.protegerSeAindaFor(tr.cache.Load())
	if _, elevada := tr.CurrentComElevacao(); !elevada {
		t.Error("o vigia apagou a marca de janela de administrador")
	}
}

// TestVigia_ReentranciaNaoFabricaProtegidoPreso roda a corrida acima pela
// sequência inteira: a inspeção do campo anterior prende, a do foco novo
// roda DENTRO dela (reentrância) e publica livre, e só então o vigia da
// antiga vence. O vigia não pode pôr protegido por cima — a antiga é
// descartada pela geração e não publicaria nada para corrigir.
func TestVigia_ReentranciaNaoFabricaProtegidoPreso(t *testing.T) {
	rel := &relogio{agora: t0, passo: time.Millisecond}
	v := &vigiaDuble{}
	tr := NewTracker(Config{})
	var diags []DiagnosticoFoco
	tr.AoInspecionar(func(d DiagnosticoFoco) { diags = append(diags, d) })

	sis := func(f func() Snapshot) sistema {
		return sistema{inspecionar: f, agora: rel.ler, vigiar: v.vigiar}
	}
	tr.inspecionarEPublicar(sis(func() Snapshot { return campoLivre }), 0, false) // vigia 0

	velha := func() Snapshot { // vigia 1
		// A nova roda aqui dentro (vigia 2) e publica livre.
		tr.inspecionarEPublicar(sis(func() Snapshot { return campoLivre }), 0, false)
		// O orçamento da velha vence depois da publicação da nova.
		v.vencer(t, 1)
		return Snapshot{Outcome: InspectTimeout}
	}
	tr.inspecionarEPublicar(sis(velha), 0, false)

	if tr.Current().Protected {
		t.Fatal("o vigia da inspeção velha pôs protegido por cima do livre da nova — e ficaria preso")
	}
	if len(diags) != 3 || !diags[2].Descartada || diags[2].Vigia {
		t.Errorf("a velha deveria sair descartada e sem vigia: %+v", diags[len(diags)-1])
	}
}

// TestCicloFoco_AnotarVigia: o intervalo do vigia entra na contagem de
// protegido_por só se a inspeção dele ainda é a atual.
func TestCicloFoco_AnotarVigia(t *testing.T) {
	var ciclo cicloFoco
	g := ciclo.comecar()
	ciclo.concluir(g, 0, campoLivre, 0, t0)

	velha := ciclo.comecar()
	ciclo.comecar() // uma mais nova começou (reentrância)
	ciclo.anotarVigia(velha, t0.Add(time.Second))
	if ciclo.publicouProtegido {
		t.Fatal("o vigia de uma inspeção que já não é a atual mexeu na contagem")
	}

	atual := ciclo.comecar()
	ciclo.anotarVigia(atual, t0.Add(2*time.Second))
	d := ciclo.concluir(atual, 0, campoLivre, 0, t0.Add(5*time.Second))
	if !d.Liberou || d.ProtegidoPor != 3*time.Second {
		t.Errorf("liberou=%v por=%v, quero true por 3s (do vigia até a publicação)", d.Liberou, d.ProtegidoPor)
	}
}

// TestVigiarComTimer_PararAntesDoPrazoNaoRoda: o vigia de produção,
// desarmado antes do prazo, nunca roda.
func TestVigiarComTimer_PararAntesDoPrazoNaoRoda(t *testing.T) {
	rodou := make(chan struct{}, 1)
	parar := vigiarComTimer(time.Hour, func() { rodou <- struct{}{} })
	parar()
	select {
	case <-rodou:
		t.Fatal("o vigia rodou depois de desarmado")
	default:
	}
}

// TestVigiarComTimer_PararEsperaOVigiaTerminar: se o vigia já disparou,
// parar só volta depois de ele terminar — é o que deixa a thread dedicada
// ler, sem corrida, o que ele escreveu (e ter certeza de que ele não toca
// mais no cache depois dela).
func TestVigiarComTimer_PararEsperaOVigiaTerminar(t *testing.T) {
	comecou := make(chan struct{})
	solta := make(chan struct{})
	var terminou bool
	parar := vigiarComTimer(time.Millisecond, func() {
		close(comecou)
		<-solta
		terminou = true
	})
	select {
	case <-comecou:
	case <-time.After(5 * time.Second):
		t.Fatal("o vigia não disparou")
	}

	voltou := make(chan struct{})
	go func() {
		parar()
		close(voltou)
	}()
	select {
	case <-voltou:
		t.Fatal("parar voltou com o vigia ainda rodando")
	case <-time.After(50 * time.Millisecond):
	}
	close(solta)
	select {
	case <-voltou:
	case <-time.After(5 * time.Second):
		t.Fatal("parar não voltou depois de o vigia terminar")
	}
	if !terminou {
		t.Error("parar voltou antes de o vigia terminar")
	}
}
