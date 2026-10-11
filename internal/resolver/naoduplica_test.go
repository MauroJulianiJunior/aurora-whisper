package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
)

// contarPalavras é usado só pelos testes de invariante abaixo.
func contarPalavras(s string) map[string]int {
	m := map[string]int{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		m[w]++
	}
	return m
}

// TestResolver_NuncaDuplicaTexto trava o invariante que Mauro cobrou em
// 28-set-2026, depois de ver texto duplicado no campo:
//
//	o resolver SUBSTITUI e REMOVE; ele nunca repete o que já estava lá.
//
// Nenhuma palavra presente no texto do ASR pode aparecer MAIS vezes no
// texto final. Palavra nova pode surgir — é o que uma correção faz —,
// mas o que já estava não pode multiplicar.
//
// Este teste existe porque a investigação daquele relato passou por
// quatro hipóteses sem nenhuma delas ser verificável: não havia
// invariante travando "o resolver não duplica", só exemplos.
func TestResolver_NuncaDuplicaTexto(t *testing.T) {
	repo := repoDe(t, `{
	  "usuario":"m",
	  "dominios":{"dev":["Code.exe"]},
	  "entradas":[
	    {"canonica":"DCTFWeb","prioridade":100,"alias":["DCTF Web"]},
	    {"canonica":"Supabase","prioridade":100,"alias":["Supabaese"]},
	    {"canonica":"PostgreSQL","prioridade":100,"dominio":"dev",
	     "alias":["Postgre"],"alias_restrito":["postgres"]},
	    {"canonica":"endpoint","prioridade":90},
	    {"canonica":"timeout","prioridade":90},
	    {"canonica":"commit","prioridade":75,"dominio":"dev","alias_restrito":["comite"]}
	  ]}`)

	dev := "dev"
	frases := []string{
		"O vencimento é no dia 12",
		"O vencimento é no dia 12. O vencimento é no dia 12.",
		"Ovencimento ano dia 12",
		"precisa transmitir a DCTF Web antes do dia quinze",
		"DCTF Web DCTF Web DCTF Web",
		"sobe a migration no Supabaese e confere seu postgres",
		"o End Point esta retornando Time Out, confere o log do BackEnd",
		"esse comite quebrou o pipeline do CI",
		"manda para a Rosangela, nao, na verdade manda para o Thiago",
		"o vencimento e dia dez, desculpa, dia doze",
		"dia 12 dia 12 dia 12",
		"",
		"   ",
		"a",
	}

	for _, txt := range frases {
		for _, dom := range []*string{nil, &dev} {
			out, err := NewSmartResolver(repo).Resolve(context.Background(), ResolveRequest{
				UserID: "m", Domain: dom,
				ASRResult: asr.ASRResult{Text: txt, Language: "pt"},
			})
			if err != nil {
				t.Fatalf("Resolve(%q): %v", txt, err)
			}
			antes, depois := contarPalavras(txt), contarPalavras(out.FinalText)
			for palavra, n := range depois {
				if antes[palavra] > 0 && n > antes[palavra] {
					t.Errorf("%q apareceu %dx e virou %dx\n  in:  %q\n  out: %q",
						palavra, antes[palavra], n, txt, out.FinalText)
				}
			}
			// E o texto nunca pode crescer de forma desproporcional: uma
			// substituição troca termo por termo, não frase por duas.
			if len([]rune(out.FinalText)) > len([]rune(txt))+30 {
				t.Errorf("texto cresceu demais\n  in:  %q\n  out: %q", txt, out.FinalText)
			}
		}
	}
}

// TestAutocorrecao_NuncaAlonga: a autocorreção remove o trecho
// retratado. O resultado não pode ser mais longo que a entrada.
func TestAutocorrecao_NuncaAlonga(t *testing.T) {
	sujeitos := []string{"O vencimento", "Manda o balancete", "Sobe no Supabase", "São três mil reais"}
	marcadores := []string{"não", "na verdade", "desculpa", "quer dizer", "corrigindo", "melhor", "no"}
	caudas := []string{"dia doze", "dia 12", "o vencimento é no dia 12", "Thiago", ""}

	for _, s := range sujeitos {
		for _, m := range marcadores {
			for _, c := range caudas {
				for _, forma := range []string{
					s + ", " + m + ", " + c,
					s + ", " + m + " " + c,
					s + " " + m + ", " + c,
					s + ", " + c + ", " + m,
				} {
					got := applySelfCorrection(forma).Text
					if len([]rune(got)) > len([]rune(forma)) {
						t.Errorf("alongou:\n  in:  %q\n  out: %q", forma, got)
					}
					antes, depois := contarPalavras(forma), contarPalavras(got)
					for p, n := range depois {
						if n > antes[p] {
							t.Errorf("%q repetiu:\n  in:  %q\n  out: %q", p, forma, got)
						}
					}
				}
			}
		}
	}
}
