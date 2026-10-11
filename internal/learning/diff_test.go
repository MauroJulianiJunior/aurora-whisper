package learning_test

import (
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
)

// TestClassify é a mesa de diff pedida pela tarefa: vários pares
// (antes, depois) classificados como sugerível ou não, com o porquê
// (Suggestion.Reason). Cobre também os três casos que nunca devem
// sugerir nada: texto vazio, texto idêntico e edição só de
// espaço/pontuação.
func TestClassify(t *testing.T) {
	cases := []struct {
		name          string
		before        string
		after         string
		wantOK        bool
		wantAlias     bool
		wantReasonHas string // substring esperada em Reason quando wantOK
		// wantAliasFrom/To são o TRECHO que deve virar alias — vazio
		// significa "não verificado neste caso".
		wantAliasFrom string
		wantAliasTo   string
		// verificaSuspeita liga a checagem de wantSuspeita: os dois
		// termos estão distantes o bastante para a UI avisar quem
		// confirma (ver Suggestion.Similaridade).
		verificaSuspeita bool
		wantSuspeita     bool
	}{
		{
			name:   "texto vazio (before)",
			before: "",
			after:  "n8n",
			wantOK: false,
		},
		{
			name:   "texto vazio (after, usuário apagou tudo)",
			before: "n8n",
			after:  "   ",
			wantOK: false,
		},
		{
			name:   "texto idêntico",
			before: "reunião às dez",
			after:  "reunião às dez",
			wantOK: false,
		},
		{
			name:   "edição só de espaços",
			before: "n8n   está   no ar",
			after:  "n8n está no ar",
			wantOK: false,
		},
		{
			name:   "edição só de pontuação",
			before: "Cliente confirmou.",
			after:  "Cliente confirmou",
			wantOK: false,
		},
		{
			name:          "TEST-008: n oito n -> n8n (pequena e mapeável)",
			before:        "n oito n",
			after:         "n8n",
			wantOK:        true,
			wantAlias:     true,
			wantReasonHas: "pequena e mapeável",
			// Trava o fato que justifica NÃO existir limiar automático:
			// o caso exigido pela spec está do lado "suspeito" (0,25),
			// abaixo de "certo"/"errado" (0,33). Qualquer corte que
			// aceitasse este aceitaria aquele.
			verificaSuspeita: true,
			wantSuspeita:     true,
		},
		{
			name:          "dctf web -> DCTFWeb (pequena e mapeável)",
			before:        "dctf web",
			after:         "DCTFWeb",
			wantOK:        true,
			wantAlias:     true,
			wantReasonHas: "pequena e mapeável",
		},
		{
			name:          "correção de caixa de uma palavra",
			before:        "fisco",
			after:         "Fisco",
			wantOK:        true,
			wantAlias:     true,
			wantReasonHas: "pequena e mapeável",
		},
		{
			name:          "edição grande: reescrita de frase inteira com pontuação",
			before:        "a reunião de hoje foi muito produtiva e todos concordaram com o plano.",
			after:         "o encontro de hoje rendeu bastante e o time topou seguir com a proposta apresentada.",
			wantOK:        true,
			wantAlias:     false,
			wantReasonHas: "edição grande",
		},
		{
			// MUDOU em 27-set-2026: Classify passou a extrair o TRECHO
			// que mudou ("certo" -> "errado") em vez de olhar o texto
			// inteiro, então o tamanho deixou de barrar este caso.
			//
			// O que o barra agora é o humano: Similaridade fica em 0,33
			// e a UI avisa. Não há medida automática que separe isto de
			// "n oito n" -> "n8n" (0,25), que é o TEST-008 e PRECISA ser
			// aprendido — ver Suggestion.Similaridade.
			name:             "termos distantes: elegível por tamanho, mas avisado",
			before:           "isso está certo?",
			after:            "isso está errado!",
			wantOK:           true,
			wantAlias:        true,
			verificaSuspeita: true,
			wantSuspeita:     true,
			wantAliasFrom:    "certo",
			wantAliasTo:      "errado",
		},
		{
			// Idem: o trecho ("amanhã de manhã" -> "depois de amanhã
			// bem") cabe nos tetos. Similaridade 0,45 avisa.
			name:             "reescrita de expressão: elegível por tamanho, mas avisada",
			before:           "marca o compromisso para amanhã de manhã cedo",
			after:            "marca o compromisso para depois de amanhã bem cedo",
			wantOK:           true,
			wantAlias:        true,
			verificaSuspeita: true,
			wantSuspeita:     true,
			wantAliasFrom:    "amanhã de manhã",
			wantAliasTo:      "depois de amanhã bem",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := learning.Classify(tc.before, tc.after)
			if ok != tc.wantOK {
				t.Fatalf("Classify(%q, %q) ok = %v, want %v (got=%+v)", tc.before, tc.after, ok, tc.wantOK, got)
			}
			if !ok {
				if got != (learning.Suggestion{}) {
					t.Fatalf("esperava Suggestion zero-value quando ok=false, veio %+v", got)
				}
				return
			}
			if got.AliasEligible != tc.wantAlias {
				t.Fatalf("Classify(%q, %q).AliasEligible = %v, want %v (reason=%q)", tc.before, tc.after, got.AliasEligible, tc.wantAlias, got.Reason)
			}
			if got.Reason == "" {
				t.Fatalf("Reason não deveria ficar vazio quando ok=true")
			}
			if tc.wantReasonHas != "" && !strings.Contains(got.Reason, tc.wantReasonHas) {
				t.Fatalf("Reason = %q, esperava conter %q", got.Reason, tc.wantReasonHas)
			}
			if tc.wantAliasFrom != "" && got.AliasFrom != tc.wantAliasFrom {
				t.Errorf("AliasFrom = %q, want %q — o alias tem de ser o TRECHO, não a frase",
					got.AliasFrom, tc.wantAliasFrom)
			}
			if tc.wantAliasTo != "" && got.AliasTo != tc.wantAliasTo {
				t.Errorf("AliasTo = %q, want %q", got.AliasTo, tc.wantAliasTo)
			}
			if suspeita := got.Similaridade < learning.SimilaridadeSuspeita; tc.verificaSuspeita && suspeita != tc.wantSuspeita {
				t.Errorf("Similaridade = %.2f (suspeita=%v), want suspeita=%v — é o aviso "+
					"que quem confirma precisa ver", got.Similaridade, suspeita, tc.wantSuspeita)
			}
		})
	}
}

// --- palavra de ligação na borda (28-set-2026) -------------------------

// TestTrechoQueMudou_NaoAssaArtigoNaFormaCanonica é o teste do defeito
// que bloqueava a persistência dos ditados.
//
// trechoQueMudou é um diff de prefixo/sufixo comum, e engolia o artigo
// que a pessoa acrescentava JUNTO da correção do termo. O artigo entrava
// na forma CANÔNICA, e como alias se aplica a todo ditado futuro, o
// resolver passaria a escrever "Abri o o Wispr Flow" — sem aviso nenhum,
// porque a similaridade fica bem acima de SimilaridadeSuspeita.
//
// Era sistemático, não um caso: toda correção de termo que vinha
// acompanhada de um artigo faltando caía nisto.
func TestTrechoQueMudou_NaoAssaArtigoNaFormaCanonica(t *testing.T) {
	casos := []struct {
		nome          string
		antes, depois string
		quero         [2]string
	}{
		{
			nome:   "artigo acrescentado antes do termo",
			antes:  "Agora estamos fazendo um teste com Whisperflow aberto.",
			depois: "Agora estamos fazendo um teste com o Wispr Flow aberto.",
			quero:  [2]string{"Whisperflow", "Wispr Flow"},
		},
		{
			nome:   "artigo feminino",
			antes:  "Precisa transmitir DCTF web amanha.",
			depois: "Precisa transmitir a DCTFWeb amanha.",
			quero:  [2]string{"DCTF web", "DCTFWeb"},
		},
		{
			// Aparar o artigo reexpõe contexto comum nos dois lados:
			// "migracao" e "do" viram prefixo comum depois que o "a" sai,
			// e o trecho mínimo de verdade aparece.
			nome:   "aparar reexpoe contexto comum",
			antes:  "Sobe migracao do subabase agora.",
			depois: "Sobe a migracao do Supabase agora.",
			quero:  [2]string{"subabase", "Supabase"},
		},
		{
			nome:   "preposicao contraida na borda",
			antes:  "Confere postgres do servidor.",
			depois: "Confere o PostgreSQL do servidor.",
			quero:  [2]string{"postgres", "PostgreSQL"},
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s, ok := learning.Classify(c.antes, c.depois)
			if !ok {
				t.Fatal("Classify recusou a edição")
			}
			if s.AliasFrom != c.quero[0] || s.AliasTo != c.quero[1] {
				t.Errorf("alias = %q -> %q, quero %q -> %q",
					s.AliasFrom, s.AliasTo, c.quero[0], c.quero[1])
			}
			if !s.AliasEligible {
				t.Errorf("alias inelegível (%s) — a correção do termo se perde", s.Reason)
			}
		})
	}
}

// TestTrechoQueMudou_NaoAparaOQueNaoEhLigacao: a poda é de classe
// FECHADA. Uma correção normal não pode encolher.
func TestTrechoQueMudou_NaoAparaOQueNaoEhLigacao(t *testing.T) {
	casos := []struct {
		antes, depois string
		quero         [2]string
	}{
		{"Manda o balancete hoje.", "Manda o balancete amanha.", [2]string{"hoje", "amanha"}},
		{"Esse comit quebrou tudo.", "Esse commit quebrou tudo.", [2]string{"comit", "commit"}},
		// "do" no MIOLO fica: só a borda é podada.
		{"Manda relatorio do mes.", "Manda relatório do mês.", [2]string{"relatorio do mes", "relatório do mês"}},
	}
	for _, c := range casos {
		s, ok := learning.Classify(c.antes, c.depois)
		if !ok {
			t.Fatalf("learning.Classify(%q, %q) recusou", c.antes, c.depois)
		}
		if s.AliasFrom != c.quero[0] || s.AliasTo != c.quero[1] {
			t.Errorf("learning.Classify(%q, %q) = %q -> %q, quero %q -> %q",
				c.antes, c.depois, s.AliasFrom, s.AliasTo, c.quero[0], c.quero[1])
		}
	}
}

// TestTrechoQueMudou_InsercaoPuraContinuaInelegivel: a poda não pode
// esvaziar um lado. "" de um lado é o sinal de inserção/remoção pura, e
// fabricá-lo transformaria uma edição legítima num par vazio — ou, pior,
// esvaziar os DOIS lados faria uma inserção pura virar um alias de nada.
func TestTrechoQueMudou_InsercaoPuraContinuaInelegivel(t *testing.T) {
	for _, c := range [][2]string{
		{"Manda balancete hoje.", "Manda o balancete hoje."},
		{"Manda o balancete hoje.", "Manda balancete hoje."},
	} {
		s, ok := learning.Classify(c[0], c[1])
		if !ok {
			continue // recusado ainda antes: também serve
		}
		if s.AliasEligible {
			t.Errorf("learning.Classify(%q, %q) virou alias %q -> %q; inserção/remoção pura não mapeia termo em termo",
				c[0], c[1], s.AliasFrom, s.AliasTo)
		}
	}
}
