package dictation

import (
	"testing"
	"time"
)

// ev monta um RawKeyEvent como o Translator do hook entregaria: Modifiers
// já reflete o estado DEPOIS do evento, incluindo a própria tecla.
func ev(key KeyCode, mods Modifiers, down bool) RawKeyEvent {
	return RawKeyEvent{Key: key, Modifiers: mods, Down: down, At: time.Now()}
}

const ctrlWin = ModControl | ModMeta

// TestCombinacao_NuncaSuprime é o teste mais importante deste arquivo.
//
// Suprimir o keyup de um modificador deixa o Windows achando que a tecla
// continua pressionada. Aconteceu neste projeto em 17-set-2026: um teste
// injetou entrada, deixou um modificador preso, e travou o teclado de quem
// estava usando a máquina.
//
// Nenhum evento deste modo pode vir com Suppress. O preço é, no pior caso,
// o menu Iniciar abrir sem querer; o preço do contrário é um teclado
// travado.
func TestCombinacao_NuncaSuprime(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)

	eventos := []RawKeyEvent{
		ev(ControlLeft, ModControl, true),
		ev(MetaLeft, ctrlWin, true),
		ev(KeyA, ctrlWin, true),
		ev(KeyA, ctrlWin, false),
		ev(ControlLeft, ModMeta, false),
		ev(MetaLeft, 0, false),
		ev(MetaLeft, ModMeta, true),
		ev(MetaLeft, 0, false),
	}
	for i, e := range eventos {
		if got := d.Decide(e); got.Suppress {
			t.Errorf("evento %d (%+v): Suppress = true — modificador suprimido pode ficar preso no Windows", i, e)
		}
	}
}

// TestCombinacao_ArmaAoCompletarEDesarmaAoQuebrar cobre a ordem natural:
// Ctrl primeiro, Win depois; solta Ctrl, solta Win.
func TestCombinacao_ArmaAoCompletarEDesarmaAoQuebrar(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)

	if got := d.Decide(ev(ControlLeft, ModControl, true)); got.Notify {
		t.Error("Ctrl sozinho notificou — o conjunto ainda não está completo")
	}
	got := d.Decide(ev(MetaLeft, ctrlWin, true))
	if !got.Notify {
		t.Fatal("completar Ctrl+Win não notificou o núcleo")
	}

	// Soltar o Ctrl quebra o conjunto: é o fim do ditado.
	got = d.Decide(ev(ControlLeft, ModMeta, false))
	if !got.Notify {
		t.Fatal("quebrar o conjunto não notificou o núcleo")
	}
	// Soltar o Win depois não pode notificar de novo — a sessão já acabou.
	if got := d.Decide(ev(MetaLeft, 0, false)); got.Notify {
		t.Error("soltar a segunda tecla notificou de novo — geraria um keyup órfão no núcleo")
	}
}

// TestCombinacao_OrdemInversaFunciona: apertar Win antes de Ctrl tem de
// armar igual. Quem usa não segue uma ordem fixa.
func TestCombinacao_OrdemInversaFunciona(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)

	if got := d.Decide(ev(MetaLeft, ModMeta, true)); got.Notify {
		t.Error("Win sozinho notificou")
	}
	if got := d.Decide(ev(ControlLeft, ctrlWin, true)); !got.Notify {
		t.Fatal("completar na ordem inversa não armou")
	}
}

// TestCombinacao_VarianteDireitaFunciona: dictation.Modifiers não
// distingue lado, e quem usa aperta o Ctrl que estiver mais perto.
func TestCombinacao_VarianteDireitaFunciona(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)
	d.Decide(ev(ControlRight, ModControl, true))
	if got := d.Decide(ev(MetaRight, ctrlWin, true)); !got.Notify {
		t.Fatal("Ctrl direito + Win direito não armou")
	}
}

// TestCombinacao_TeclaComumNaoDesarma: com o ditado armado, Ctrl+Win+D
// continua chegando ao Windows e a gravação continua. Documentado no
// ADR-013 como escolha, não como esquecimento.
func TestCombinacao_TeclaComumNaoDesarma(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)
	d.Decide(ev(ControlLeft, ModControl, true))
	d.Decide(ev(MetaLeft, ctrlWin, true))

	if got := d.Decide(ev(KeyD, ctrlWin, true)); got.Notify || got.Suppress {
		t.Errorf("tecla comum durante o gatilho = %+v, want passThrough", got)
	}
	// E o conjunto continua armado: quebrar depois ainda notifica.
	if got := d.Decide(ev(ControlLeft, ModMeta, false)); !got.Notify {
		t.Error("o conjunto desarmou por causa de uma tecla comum")
	}
}

// TestCombinacao_EventoNossoNaoDispara mantém a regra 1 do ADR-007 neste
// modo: evento que nós injetamos nunca inicia sessão.
func TestCombinacao_EventoNossoNaoDispara(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)
	e := ev(ControlLeft, ModControl, true)
	e.Injected, e.FromThisProcess = true, true
	d.Decide(e)

	e2 := ev(MetaLeft, ctrlWin, true)
	e2.Injected, e2.FromThisProcess = true, true
	if got := d.Decide(e2); got.Notify {
		t.Error("evento injetado por nós armou o ditado — é o laço infinito que o ADR-007 evita")
	}
}

// TestCombinacao_NaoArmaDuasVezes: segurar as teclas gera keydown repetido
// pelo autorepeat do Windows. Armar de novo geraria uma segunda sessão.
func TestCombinacao_NaoArmaDuasVezes(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)
	d.Decide(ev(ControlLeft, ModControl, true))
	if got := d.Decide(ev(MetaLeft, ctrlWin, true)); !got.Notify {
		t.Fatal("não armou")
	}
	for i := 0; i < 5; i++ {
		if got := d.Decide(ev(MetaLeft, ctrlWin, true)); got.Notify {
			t.Fatalf("autorepeat %d notificou de novo — geraria sessão duplicada", i)
		}
	}
}

// TestValidate_CombinacaoExigeDoisModificadores trava a leitura do REQ-004
// que o ADR-013 registra: um modificador SOLTO continua proibido (Ctrl fica
// pressionado o tempo todo em uso normal), e dois ou mais são permitidos.
func TestValidate_CombinacaoExigeDoisModificadores(t *testing.T) {
	base := Config{HoldThreshold: 700 * time.Millisecond}

	casos := []struct {
		nome   string
		mods   Modifiers
		valido bool
	}{
		{"Ctrl+Win", ModControl | ModMeta, true},
		{"Ctrl+Shift", ModControl | ModShift, true},
		{"Ctrl+Alt+Win", ModControl | ModAlt | ModMeta, true},
		{"só Ctrl", ModControl, false},
		{"só Win", ModMeta, false},
		{"nenhum", 0, false},
	}
	for _, c := range casos {
		cfg := base
		cfg.Key, cfg.Modifiers = KeyUnknown, c.mods
		err := cfg.Validate()
		if c.valido && err != nil {
			t.Errorf("%s: Validate = %v, want nil", c.nome, err)
		}
		if !c.valido && err == nil {
			t.Errorf("%s: Validate = nil, want erro — um modificador solto fica pressionado "+
				"o tempo todo em uso normal e dispararia o ditado sem parar", c.nome)
		}
	}
}

// TestValidate_TeclaDeLetraContinuaValendo: o modo antigo não pode ter sido
// quebrado pela adição do novo.
func TestValidate_TeclaDeLetraContinuaValendo(t *testing.T) {
	cfg := Config{Key: KeyF, HoldThreshold: 700 * time.Millisecond}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate(tecla F) = %v, want nil", err)
	}
	cfg.Key = ControlLeft
	if err := cfg.Validate(); err == nil {
		t.Error("Validate(modificador como tecla principal) = nil — o REQ-004 continua valendo aqui")
	}
}

// TestValidate_CombinacaoAindaValidaOThreshold: o caminho novo não pode ter
// pulado a validação que o antigo faz.
func TestValidate_CombinacaoAindaValidaOThreshold(t *testing.T) {
	cfg := Config{Key: KeyUnknown, Modifiers: ctrlWin, HoldThreshold: 10 * time.Millisecond}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate(threshold fora da faixa) = nil no gatilho por combinação")
	}
}

// TestCombinacao_KeyupPerdidoNaoPenduraASessao.
//
// Um keyup de modificador pode se perder: troca de foco, evento
// descartado, o Windows engolindo o evento durante um Alt+Tab. Sem
// recuperação, a sessão ficaria armada para sempre esperando um
// modificador que nunca vem — e o ditado seguinte nunca começaria.
//
// Qualquer evento com o conjunto incompleto encerra a sessão pendurada,
// inclusive de tecla comum.
func TestCombinacao_KeyupPerdidoNaoPenduraASessao(t *testing.T) {
	d := NewDeciderModificadores(ctrlWin)
	d.Decide(ev(ControlLeft, ModControl, true))
	if got := d.Decide(ev(MetaLeft, ctrlWin, true)); !got.Notify {
		t.Fatal("não armou")
	}

	// Os keyups dos modificadores se perderam. A pessoa volta a digitar,
	// e a primeira tecla chega sem o conjunto.
	got := d.Decide(ev(KeyA, 0, true))
	if !got.Notify {
		t.Fatal("a sessão continuou armada depois de perder os keyups — " +
			"o ditado seguinte nunca começaria")
	}
	if got.Suppress {
		t.Error("suprimiu a tecla que a pessoa digitou")
	}
	// E não notifica de novo nas teclas seguintes.
	if got := d.Decide(ev(KeyB, 0, true)); got.Notify {
		t.Error("notificou de novo — geraria keyup órfão no núcleo")
	}
}
