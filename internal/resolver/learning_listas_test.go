package resolver

import (
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
)

// O motor de sugestão do painel (learning.Sugerir) não deixa o dicionário
// aprender um alias que mude um número por extenso ou inverta o sentido de uma
// frase: "oito" para "seis", "mais" para "mas". As palavras são as mesmas que
// o Polish protege (numeraisPorExtenso e protegidas, em polish_guardas.go).
// Learning não importa este pacote (é pesado, e nada mais de learning usa
// dele), então mantém uma cópia das duas listas. Estes testes são o que avisa
// quando a cópia fica para trás: se uma palavra entrar numa lista do Polish, o
// aprendizado do painel tem de barrá-la também.

// sugestoesDaTroca é o que Sugerir devolve para a edição de "eu disse <de>
// hoje" para "eu disse <para> hoje", com o texto de antes como o Polish o
// entregaria.
func sugestoesDaTroca(de, para string) learning.Resultado {
	return learning.Sugerir(
		"eu disse "+de+" hoje", "Eu disse "+de+" hoje.", "Eu disse "+para+" hoje.",
		dictionary.Semente{}, nil)
}

func TestNumeraisAcompanhamOsDoResolver(t *testing.T) {
	for w := range numeraisPorExtenso {
		// O numeral é o De: o número virou outra palavra. (Numeral só no Para não
		// é barrado: a pessoa consertou uma palavra que o ASR errou.)
		if r := sugestoesDaTroca(w, "xpto"); len(r.Sugestoes) != 0 {
			t.Errorf("o aprendizado do painel sugere trocar o numeral %q: %+v", w, r.Sugestoes)
		}
	}
}

func TestPolaridadeAcompanhaProtegidasDoResolver(t *testing.T) {
	for w := range protegidas {
		if r := sugestoesDaTroca(w, "xpto"); len(r.Sugestoes) != 0 {
			t.Errorf("o aprendizado do painel sugere trocar %q: %+v", w, r.Sugestoes)
		}
		if r := sugestoesDaTroca("xpto", w); len(r.Sugestoes) != 0 {
			t.Errorf("o aprendizado do painel sugere trocar por %q: %+v", w, r.Sugestoes)
		}
	}
}
