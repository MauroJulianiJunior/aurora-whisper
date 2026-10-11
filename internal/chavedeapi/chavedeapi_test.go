package chavedeapi

import (
	"errors"
	"strings"
	"testing"
)

// chaveDeTeste tem cara de chave de API sem ser uma.
const chaveDeTeste = "sk-teste-0123456789abcdefghijklmnopqrstuvwxyzABCD"

func TestValidar(t *testing.T) {
	if got, err := Validar("  \t" + chaveDeTeste + "\r\n"); err != nil || got != chaveDeTeste {
		t.Errorf("as pontas não foram limpas: %q, %v", got, err)
	}
	if got, err := Validar(strings.Repeat("a", TamanhoMaximo)); err != nil || len(got) != TamanhoMaximo {
		t.Errorf("chave no tamanho máximo recusada: %v", err)
	}
	for nome, c := range map[string]struct {
		chave string
		quero error
	}{
		"vazia":            {"", ErrChaveVazia},
		"só espaço":        {" \r\n\t ", ErrChaveVazia},
		"longa demais":     {strings.Repeat("a", TamanhoMaximo+1), ErrChaveLonga},
		"espaço no meio":   {"sk-abc def", ErrChaveInvalida},
		"quebra no meio":   {"sk-abc\ndef", ErrChaveInvalida},
		"cabeçalho injet.": {"sk-abc\r\nX-Outro: 1", ErrChaveInvalida},
		"controle":         {"sk-abc\x00def", ErrChaveInvalida},
		"fora do ASCII":    {"sk-abcçdef", ErrChaveInvalida},
		"DEL":              {"sk-abc\x7fdef", ErrChaveInvalida},
	} {
		got, err := Validar(c.chave)
		if !errors.Is(err, c.quero) || got != "" {
			t.Errorf("%s: %q, err = %v, quero %v", nome, got, err, c.quero)
		}
		if limpa := strings.TrimSpace(c.chave); err != nil && len(limpa) > 3 && strings.Contains(err.Error(), limpa) {
			t.Errorf("%s: a mensagem de erro carrega a chave", nome)
		}
	}
}

// As mensagens são as que o Agent sempre teve: ele registra a de uma gravação
// recusada, e a linha do log dele não pode mudar com a mudança de pacote.
func TestValidar_AsMensagensDoAgent(t *testing.T) {
	for err, quero := range map[error]string{
		ErrChaveVazia:    "chaves: chave vazia",
		ErrChaveLonga:    "chaves: chave longa demais",
		ErrChaveInvalida: "chaves: a chave tem espaço, quebra de linha ou caractere fora do ASCII",
	} {
		if err.Error() != quero {
			t.Errorf("mensagem %q, quero %q", err.Error(), quero)
		}
	}
}

func TestFinal(t *testing.T) {
	for chave, quero := range map[string]string{
		chaveDeTeste:       "ABCD",
		"abcdefghijklmnop": "mnop", // 16: um quarto são 4
		"abcdefghijkl":     "jkl",  // 12: um quarto são 3
		"abcdefgh":         "gh",
		"abcd":             "d",
		"abc":              "",
		"":                 "",
	} {
		if got := Final(chave); got != quero {
			t.Errorf("Final(%q) = %q, quero %q", chave, got, quero)
		}
	}
}

// TestFinal_NuncaMaisQueQuatroNemQueUmQuarto varre tamanhos: a regra vale para
// todos, não só para os exemplos acima.
func TestFinal_NuncaMaisQueQuatroNemQueUmQuarto(t *testing.T) {
	for n := 0; n <= TamanhoMaximo; n++ {
		chave := strings.Repeat("x", n)
		f := Final(chave)
		if len(f) > TamanhoMaximoDoFinal || len(f)*4 > n {
			t.Fatalf("Final de %d caracteres mostrou %d", n, len(f))
		}
		if !strings.HasSuffix(chave, f) {
			t.Fatalf("Final de %d caracteres não é o fim da chave", n)
		}
	}
}
