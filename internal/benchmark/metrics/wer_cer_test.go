package metrics

import (
	"errors"
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestWER_IdenticalText(t *testing.T) {
	got, err := WER("o Fisco cobrou o DCTFWeb", "o Fisco cobrou o DCTFWeb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 0) {
		t.Fatalf("WER = %v, want 0", got)
	}
}

func TestWER_CompletelyDifferent_SameLength(t *testing.T) {
	// mesma quantidade de tokens, todos diferentes: WER == 1 (teto
	// documentado: 1.0 é o caso "todo mundo errado, mesmo tamanho").
	got, err := WER("um dois tres", "quatro cinco seis")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 1) {
		t.Fatalf("WER = %v, want 1", got)
	}
}

func TestWER_HypothesisLonger_ExceedsOne(t *testing.T) {
	// teto documentado: NAO ha limite superior de 1.0 quando a hipotese
	// insere tokens em excesso.
	got, err := WER("a", "a b c d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 3) {
		t.Fatalf("WER = %v, want 3 (3 insercoes / 1 token de referencia)", got)
	}
}

func TestWER_OnlySubstitution(t *testing.T) {
	got, err := WER("o Fisco cobrou hoje", "o Fisco pagou hoje")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 0.25) {
		t.Fatalf("WER = %v, want 0.25 (1 substituicao / 4 tokens)", got)
	}
}

func TestWER_OnlyInsertion(t *testing.T) {
	got, err := WER("o Fisco cobrou", "o Fisco realmente cobrou")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 1.0/3.0) {
		t.Fatalf("WER = %v, want 1/3 (1 insercao / 3 tokens de referencia)", got)
	}
}

func TestWER_OnlyDeletion(t *testing.T) {
	got, err := WER("o Fisco realmente cobrou", "o Fisco cobrou")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 0.25) {
		t.Fatalf("WER = %v, want 0.25 (1 delecao / 4 tokens de referencia)", got)
	}
}

func TestWER_EmptyReference(t *testing.T) {
	_, err := WER("", "algo")
	if !errors.Is(err, ErrEmptyReference) {
		t.Fatalf("err = %v, want ErrEmptyReference", err)
	}
}

func TestWER_WhitespaceOnlyReference(t *testing.T) {
	_, err := WER("   \t  ", "algo")
	if !errors.Is(err, ErrEmptyReference) {
		t.Fatalf("err = %v, want ErrEmptyReference", err)
	}
}

func TestWER_BothEmpty(t *testing.T) {
	_, err := WER("", "")
	if !errors.Is(err, ErrEmptyReference) {
		t.Fatalf("err = %v, want ErrEmptyReference (referencia vazia continua sem denominador)", err)
	}
}

func TestWER_NormalizationIgnoresCaseSpacingAndEdgePunctuation(t *testing.T) {
	got, err := WER("O Fisco,  cobrou.", "o fisco cobrou")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 0) {
		t.Fatalf("WER = %v, want 0 (so difere em caixa, espaco e pontuacao de borda)", got)
	}
}

func TestWER_AccentIsNotStripped(t *testing.T) {
	got, err := WER("ele fez", "ele fé")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if almostEqual(got, 0) {
		t.Fatalf("WER = %v, want > 0 ('fez' vs 'fé' precisa contar como erro, acento nao pode ser removido", got)
	}
}

func TestCER_IdenticalText(t *testing.T) {
	got, err := CER("DCTFWeb", "DCTFWeb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 0) {
		t.Fatalf("CER = %v, want 0", got)
	}
}

func TestCER_CapturesErrorInsideWordThatWERRoundsToOneWordWrong(t *testing.T) {
	ref := "DCTFWeb"
	hyp := "DCTFWob" // 1 letra trocada
	wer, err := WER(ref, hyp)
	if err != nil {
		t.Fatalf("unexpected WER error: %v", err)
	}
	if !almostEqual(wer, 1) {
		t.Fatalf("WER = %v, want 1 (WER arredonda para '1 palavra errada' inteira)", wer)
	}

	cer, err := CER(ref, hyp)
	if err != nil {
		t.Fatalf("unexpected CER error: %v", err)
	}
	// 1 substituicao de caractere / 7 caracteres da referencia.
	if !almostEqual(cer, 1.0/7.0) {
		t.Fatalf("CER = %v, want 1/7 (so uma letra errada dentro da palavra)", cer)
	}
}

func TestCER_AccentIsNotStripped(t *testing.T) {
	got, err := CER("e", "é")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !almostEqual(got, 1) {
		t.Fatalf("CER = %v, want 1 ('e' vs 'é' e uma substituicao completa de 1 caractere)", got)
	}
}

func TestCER_EmptyReference(t *testing.T) {
	_, err := CER("", "algo")
	if !errors.Is(err, ErrEmptyReference) {
		t.Fatalf("err = %v, want ErrEmptyReference", err)
	}
}
