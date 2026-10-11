package hook

import (
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
)

func teclaF(down bool) dictation.RawKeyEvent {
	return dictation.RawKeyEvent{Key: dictation.KeyF, Down: down, At: time.Now()}
}

// TestPausa_TeclaPassaDireto: pausado, a tecla de ditado volta a ser uma
// tecla comum.
func TestPausa_TeclaPassaDireto(t *testing.T) {
	h := New(Config{Key: dictation.KeyF})
	h.Pausar(true)
	if !h.deveIgnorar() {
		t.Fatal("pausado e sem toque em curso: o callback devia deixar a tecla passar")
	}
	h.Pausar(false)
	if h.deveIgnorar() {
		t.Fatal("despausado: o callback voltou a ignorar a tecla de ditado")
	}
}

// TestPausa_ToqueEmCursoTermina é o caso de borda que fez
// dictation.Decider.Armado existir.
//
// Segurar a tecla com uma mão e clicar "Pausar" na bandeja com a outra.
// Se a pausa valesse na hora, a SOLTURA passaria direto pelo hook, o
// núcleo nunca receberia o keyup, e a gravação seguiria até o limite de
// 120 s com o microfone aberto. A pausa tem de valer para o PRÓXIMO toque.
func TestPausa_ToqueEmCursoTermina(t *testing.T) {
	h := New(Config{Key: dictation.KeyF})

	// Toque começa despausado: o decisor arma.
	h.decider.Decide(teclaF(true))
	if !h.decider.Armado() {
		t.Fatal("pré-condição: o keydown devia armar o decisor")
	}

	// Pausa no MEIO do hold.
	h.Pausar(true)
	if h.deveIgnorar() {
		t.Fatal("a pausa ignorou um toque em curso: a soltura escaparia do núcleo e a gravação ficaria aberta")
	}

	// A soltura chega ao decisor e desarma.
	h.decider.Decide(teclaF(false))
	if h.decider.Armado() {
		t.Fatal("a soltura não desarmou o decisor")
	}

	// Só AGORA a pausa vale.
	if !h.deveIgnorar() {
		t.Fatal("terminado o toque, a pausa devia valer para o próximo")
	}
}

// TestPausa_DespausarNaoHerdaEstadoVelho: enquanto pausado, o decisor não
// é consultado fora de um toque em curso, então nunca arma — e ao
// despausar não há "tecla presa" herdada para desfazer.
func TestPausa_DespausarNaoHerdaEstadoVelho(t *testing.T) {
	h := New(Config{Key: dictation.KeyF})
	h.Pausar(true)
	for i := 0; i < 5; i++ {
		if !h.deveIgnorar() {
			t.Fatal("pausado: devia ignorar")
		}
		// O callback real NÃO chama Decide quando deveIgnorar é true;
		// portanto o decisor não vê estes eventos e não arma.
	}
	h.Pausar(false)
	if h.decider.Armado() {
		t.Fatal("ao despausar o decisor estava armado — herdaria uma tecla presa")
	}
	if d := h.decider.Decide(teclaF(true)); !d.Suppress && !d.Notify {
		t.Errorf("despausado, o primeiro keydown não foi tratado como tecla de ditado: %+v", d)
	}
}
