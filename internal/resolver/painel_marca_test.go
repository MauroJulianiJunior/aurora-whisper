package resolver

import (
	"context"
	"slices"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Terceira revisão da etapa 3 (30-set-2026), pontos baixos da F2.

// TestSmart_MarcaDoPainelComMinusculaInicialNaoGanhaMaiuscula: a inicial
// maiúscula de quem abre a frase só acompanha o que foi dito quando a forma
// canônica é uma palavra comum (toda minúscula, sem dígito). Uma marca como
// "iFood" ou "n8n" tem grafia própria, e "IFood" / "N8n" seria outra grafia.
func TestSmart_MarcaDoPainelComMinusculaInicialNaoGanhaMaiuscula(t *testing.T) {
	casos := []struct {
		termo, variante, entra, quer string
	}{
		{"iFood", "aifude", "Aifude atrasou", "iFood atrasou"},
		{"iFood", "aifude", "o aifude atrasou", "o iFood atrasou"},
		{"n8n", "nê oito nê", "Nê oito nê caiu", "n8n caiu"},
		{"n8n", "nê oito nê", "o nê oito nê caiu", "o n8n caiu"},
		{"iPhone", "aifone", "Aifone novo", "iPhone novo"},
		{"macOS", "mac ou ésse", "Mac ou ésse atualizou", "macOS atualizou"},
		{"eSocial", "e sosial", "E sosial abriu", "eSocial abriu"},
	}
	for _, c := range casos {
		t.Run(c.termo+"/"+c.entra, func(t *testing.T) {
			sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, c.termo, c.variante))
			if got := resolverTexto(t, sr, user, c.entra); got != c.quer {
				t.Errorf("%q -> %q, quero %q", c.entra, got, c.quer)
			}
		})
	}
}

// TestFormaDoAlias: a regra da inicial, sem passar pelo dicionário. Só a
// palavra comum (toda minúscula, sem dígito) acompanha a maiúscula do que foi
// dito; maiúscula ou dígito em qualquer posição da forma canônica a deixa
// como está.
func TestFormaDoAlias(t *testing.T) {
	casos := []struct {
		nome      string
		canonica  string
		aliasOnly bool
		original  string
		quer      string
	}{
		{"palavra comum abre a frase", "descrição", true, "Transcrição", "Descrição"},
		{"palavra comum no meio", "descrição", true, "transcrição", "descrição"},
		{"frase comum", "nota fiscal", true, "Nota fical", "Nota fiscal"},
		{"maiúscula no meio", "iFood", true, "Aifude", "iFood"},
		{"maiúscula no fim", "macOS", true, "Mac", "macOS"},
		{"dígito no meio", "n8n", true, "Nê", "n8n"},
		{"dígito no fim", "base64", true, "Base", "base64"},
		{"começa em maiúscula", "ECF", true, "Esse", "ECF"},
		{"não é do painel", "commit", false, "Comite", "commit"},
		{"forma vazia", "", true, "Comite", ""},
		{"original vazio", "descrição", true, "", "descrição"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			top := dictionary.DictionaryCandidate{CanonicalForm: c.canonica, AliasOnly: c.aliasOnly}
			if got := formaDoAlias(top, c.original); got != c.quer {
				t.Errorf("formaDoAlias(%q, %q) = %q, quero %q", c.canonica, c.original, got, c.quer)
			}
		})
	}
}

// TestComTermosTrocados_AceitaOMotivoContexto: o ramo N-best do Smart troca a
// palavra por uma variante do dicionário que o contexto corrobora, e marca a
// troca com ReasonContext. O termo que entrou no texto por causa dela tem a
// mesma proteção do que entrou por ReasonDictionary.
func TestComTermosTrocados_AceitaOMotivoContexto(t *testing.T) {
	got := comTermosTrocados([]string{"ECF"}, []AppliedReplacement{
		{Reason: ReasonContext, Replacement: "descrição"},
		{Reason: ReasonContext, Replacement: "descrição"}, // repetido
		{Reason: ReasonContext, Replacement: "  "},        // vazio
		{Reason: ReasonPhonetic, Replacement: "fonético"}, // sem corroboração: não é troca de variante
	})
	if want := []string{"ECF", "descrição"}; !slices.Equal(got, want) {
		t.Errorf("termos = %q, quero %q", got, want)
	}
}

// TestPolish_TermoDoPainelTrocadoPeloNBestContinuaProtegido: o mesmo caso do
// teste da variante aplicada, mas pelo ramo em que a troca vem da alternativa
// do ASR corroborada pelo contexto (ReasonContext). O Smart escreve
// "descrição" por causa do painel e o Polish não pode devolvê-la com
// maiúscula no meio da frase.
func TestPolish_TermoDoPainelTrocadoPeloNBestContinuaProtegido(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, "descrição", "transcrição"))

	// O texto principal tem uma palavra que nada no dicionário conhece; a
	// alternativa tem a variante, e o contexto menciona o termo.
	req := ResolveRequest{
		UserID: user,
		ASRResult: asr.ASRResult{
			Text:         "eu revisei a xpto do áudio ontem de noite",
			Alternatives: []asr.Alternative{{Text: "eu revisei a transcrição do áudio ontem de noite"}},
		},
		Context:     uiacontext.ContextSnapshot{PrefixNearCaret: "a descrição"},
		CloudPolicy: CloudDisabled,
		PolishOptIn: true,
	}
	smart, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if smart.FinalText != "eu revisei a descrição do áudio ontem de noite" {
		t.Fatalf("o controle não trocou pelo N-best: %q (trocas: %+v)", smart.FinalText, smart.AppliedReplacements)
	}
	if len(smart.AppliedReplacements) != 1 || smart.AppliedReplacements[0].Reason != ReasonContext {
		t.Fatalf("a troca não veio pelo ramo de contexto: %+v", smart.AppliedReplacements)
	}

	pr := NewPolishResolver(sr, clienteFixo{texto: "Eu revisei a Descrição do áudio ontem de noite"})
	got, err := pr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reescrito || got.MotivoFallback != MotivoSiglasTermos {
		t.Errorf("com a troca pelo N-best: reescrito=%v motivo=%q, quero fallback por %q",
			got.Reescrito, got.MotivoFallback, MotivoSiglasTermos)
	}
}

// TestSmart_NBestDoPainelAcompanhaAInicialDaFrase: o ramo da alternativa do
// ASR escreve o termo do painel pela mesma regra do ramo da variante
// (formaDoAlias): a palavra comum abre a frase com maiúscula quando a palavra
// que ela substitui abria, e a marca de grafia própria fica como foi gravada.
// Antes, o ramo escrevia a forma canônica direto, e "Xpto do áudio ficou ruim"
// virava "descrição do áudio ficou ruim", com minúscula abrindo a frase.
func TestSmart_NBestDoPainelAcompanhaAInicialDaFrase(t *testing.T) {
	casos := []struct {
		nome               string
		termo, variante    string
		texto, alternativa string
		contexto, quer     string
	}{
		{"palavra comum abre a frase", "descrição", "transcrição",
			"Xpto do áudio ficou ruim", "Transcrição do áudio ficou ruim", "a descrição", "Descrição do áudio ficou ruim"},
		{"palavra comum no meio da frase", "descrição", "transcrição",
			"eu revisei a xpto do áudio ontem", "eu revisei a transcrição do áudio ontem", "a descrição", "eu revisei a descrição do áudio ontem"},
		// A palavra que sai é a da hipótese principal: a alternativa pode ter a
		// caixa diferente, e quem manda na inicial é a principal.
		{"a principal abre a frase e a alternativa não", "descrição", "transcrição",
			"Xpto do áudio ficou ruim", "transcrição do áudio ficou ruim", "a descrição", "Descrição do áudio ficou ruim"},
		{"a alternativa abre a frase e a principal não", "descrição", "transcrição",
			"xpto do áudio ficou ruim", "Transcrição do áudio ficou ruim", "a descrição", "descrição do áudio ficou ruim"},
		{"marca de grafia própria abre a frase", "iFood", "aifude",
			"Xpto atrasou hoje", "Aifude atrasou hoje", "o iFood", "iFood atrasou hoje"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, c.termo, c.variante))
			out, err := sr.Resolve(context.Background(), ResolveRequest{
				UserID: user,
				ASRResult: asr.ASRResult{
					Text:         c.texto,
					Alternatives: []asr.Alternative{{Text: c.alternativa}},
				},
				Context:     uiacontext.ContextSnapshot{PrefixNearCaret: c.contexto},
				CloudPolicy: CloudDisabled,
			})
			if err != nil {
				t.Fatal(err)
			}
			if out.FinalText != c.quer {
				t.Errorf("%q -> %q, quero %q (trocas: %+v)", c.texto, out.FinalText, c.quer, out.AppliedReplacements)
			}
			if len(out.AppliedReplacements) != 1 || out.AppliedReplacements[0].Reason != ReasonContext {
				t.Errorf("a troca não veio pelo ramo de contexto: %+v", out.AppliedReplacements)
			}
		})
	}
}
