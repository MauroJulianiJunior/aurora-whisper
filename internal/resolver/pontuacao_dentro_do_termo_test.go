package resolver

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// repoReal carrega o dicionário pessoal de verdade — o mesmo arquivo que
// cmd/entitybench usa para medir o portão. O defeito desta slice não é
// hipotético: ele acontece com a semente que está em produção de
// medição, e o teste precisa provar isso com ela, não com um fixture
// desenhado para falhar.
func repoReal(t *testing.T) (dictionary.DictionaryRepository, string) {
	t.Helper()
	f, err := os.Open("../../testdata/asr-dataset/dicionario.json")
	if err != nil {
		t.Fatalf("abrindo dicionário real: %v", err)
	}
	defer f.Close()

	s, err := dictionary.CarregarSemente(f)
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	return repo, s.Usuario
}

// TestSmart_PontuacaoDentroDoSpanNaoEhEngolida trava uma CORREÇÃO FALSA —
// a categoria que o produto mede em 0,00% e trata como o ativo mais caro
// (internal/benchmark/entitygate/gate.go).
//
// Medido em 28-set-2026 contra o dicionário real, sem nenhuma mudança no
// código:
//
//	entra: "O Claude, Code e o Aurora resolvem coisas diferentes."
//	sai:   "O Claude Code e o Aurora resolvem coisas diferentes."
//
// Uma lista de TRÊS itens virou uma de dois. O mecanismo: Normalize trata
// pontuação como separador de palavra, então "Claude, Code" normaliza
// para "claude code" e casa EXATO com a canônica "Claude Code";
// diferePorMaisQueCaixa vê a vírgula como diferença a corrigir; e a
// substituição reescreve o intervalo de runas inteiro — vírgula inclusa.
//
// O portão não enxergava porque nenhuma das 46 amostras do dataset tem
// pontuação no meio de um termo de duas palavras.
func TestSmart_PontuacaoDentroDoSpanNaoEhEngolida(t *testing.T) {
	repo, usuario := repoReal(t)

	const entrada = "O Claude, Code e o Aurora resolvem coisas diferentes."

	out, err := NewSmartResolver(repo).Resolve(context.Background(), ResolveRequest{
		UserID:    usuario,
		ASRResult: asr.ASRResult{Text: entrada, Language: "pt"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if out.FinalText != entrada {
		t.Errorf("o resolver mexeu num texto que já estava certo:\n  entra: %q\n  sai:   %q\n"+
			"  aplicadas: %+v", entrada, out.FinalText, out.AppliedReplacements)
	}
	if !strings.Contains(out.FinalText, "Claude, Code") {
		t.Errorf("a vírgula foi engolida — a lista de três itens virou de dois: %q", out.FinalText)
	}
}

// TestSmart_PontuacaoDeSentencaNaoEhAtravessada generaliza: o defeito não
// era da vírgula, era de QUALQUER coisa que não fosse espaço entre as
// duas palavras. Ponto final, ponto e vírgula e dois-pontos apagavam
// igual, e cada um deles junta duas orações numa só.
func TestSmart_PontuacaoDeSentencaNaoEhAtravessada(t *testing.T) {
	repo, usuario := repoReal(t)

	for _, entrada := range []string{
		"Uso o Claude. Code eu nem abro.",
		"Prefiro o Claude; Code fica para depois.",
		"Sobre o Claude: Code é outra coisa.",
		"Terminei com o Claude — Code continua aberto.",
	} {
		out, err := NewSmartResolver(repo).Resolve(context.Background(), ResolveRequest{
			UserID:    usuario,
			ASRResult: asr.ASRResult{Text: entrada, Language: "pt"},
		})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", entrada, err)
		}
		if out.FinalText != entrada {
			t.Errorf("duas orações viraram uma:\n  entra: %q\n  sai:   %q", entrada, out.FinalText)
		}
	}
}

// TestSmart_CorrecaoLegitimaDeFraseContinua é o contrapeso: a recusa acima
// não pode ter desligado o casamento de frase, que é o que conserta erro
// de fronteira de token — boa parte do que o ASR erra de verdade.
//
// Todos estes casos têm ESPAÇO entre as palavras, que é a única coisa que
// a correção passou a exigir.
func TestSmart_CorrecaoLegitimaDeFraseContinua(t *testing.T) {
	repo, usuario := repoReal(t)

	casos := []struct{ entrada, quer string }{
		// Alias de duas palavras, o caso exato do dicionário real.
		{"abre o Cloud Code aqui", "Claude Code"},
		// Match exato com espaçamento errado: fronteira de token.
		{"transmitir a DCTF Web hoje", "DCTFWeb"},
		// Canônica que TEM pontuação e o span não: a direção oposta,
		// que continua valendo.
		{"manda o EFDRinf junto", "EFD-Reinf"},
	}
	for _, c := range casos {
		out, err := NewSmartResolver(repo).Resolve(context.Background(), ResolveRequest{
			UserID:    usuario,
			ASRResult: asr.ASRResult{Text: c.entrada, Language: "pt"},
		})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", c.entrada, err)
		}
		if !strings.Contains(out.FinalText, c.quer) {
			t.Errorf("%q\n  -> %q, faltou %q", c.entrada, out.FinalText, c.quer)
		}
	}
}

// TestSmart_TermoJaCorretoComHifenNaoEhMexido: "EFD-Reinf" escrito certo
// tem um hífen entre dois wordTokens, então a recusa de frase o
// decompõe em "EFD" e "Reinf". Nenhuma dessas palavras pode virar
// substituição — o texto já está certo.
func TestSmart_TermoJaCorretoComHifenNaoEhMexido(t *testing.T) {
	repo, usuario := repoReal(t)

	const entrada = "Manda o eSocial e o EFD-Reinf junto, o SPED já foi entregue."

	out, err := NewSmartResolver(repo).Resolve(context.Background(), ResolveRequest{
		UserID:    usuario,
		ASRResult: asr.ASRResult{Text: entrada, Language: "pt"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.FinalText != entrada {
		t.Errorf("o resolver mexeu num texto que já estava certo:\n  entra: %q\n  sai:   %q\n"+
			"  aplicadas: %+v", entrada, out.FinalText, out.AppliedReplacements)
	}
}
