package chaves

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/sysinput"
)

// chaveDeTeste tem cara de chave de API sem ser uma.
const chaveDeTeste = "sk-teste-0123456789abcdefghijklmnopqrstuvwxyzABCD"

func TestAlvoDe(t *testing.T) {
	for _, p := range []string{"openai", "anthropic", "a", "x-1"} {
		alvo, err := AlvoDe(p)
		if err != nil || alvo != "AuroraWhisper/"+p {
			t.Errorf("AlvoDe(%q) = %q, %v", p, alvo, err)
		}
	}
	for _, p := range []string{"", "OpenAI", "open ai", "../x", "a/b", "é", strings.Repeat("a", 33)} {
		if _, err := AlvoDe(p); !errors.Is(err, ErrProvedor) {
			t.Errorf("AlvoDe(%q): err = %v, quero ErrProvedor", p, err)
		}
	}
}

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
		if _, err := Validar(c.chave); !errors.Is(err, c.quero) {
			t.Errorf("%s: err = %v, quero %v", nome, err, c.quero)
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

// TestFinal_NuncaMaisQueQuatroNemQueUmQuarto varre tamanhos: a regra vale
// para todos, não só para os exemplos acima.
func TestFinal_NuncaMaisQueQuatroNemQueUmQuarto(t *testing.T) {
	for n := 0; n <= 200; n++ {
		chave := strings.Repeat("x", n)
		f := Final(chave)
		if len(f) > 4 || len(f)*4 > n {
			t.Fatalf("Final de %d caracteres mostrou %d", n, len(f))
		}
		if !strings.HasSuffix(chave, f) {
			t.Fatalf("Final de %d caracteres não é o fim da chave", n)
		}
	}
}

func TestMemoria_GravaLeApaga(t *testing.T) {
	m := NovaMemoria()
	if _, err := m.Ler("openai"); !errors.Is(err, ErrAusente) {
		t.Fatalf("Ler antes de gravar: err = %v, quero ErrAusente", err)
	}
	if err := m.Gravar("openai", " "+chaveDeTeste+"\n"); err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	if got, err := m.Ler("openai"); err != nil || got != chaveDeTeste {
		t.Fatalf("Ler = %q, %v; quero a chave limpa", got, err)
	}
	// Um provedor não enxerga a chave do outro.
	if _, err := m.Ler("anthropic"); !errors.Is(err, ErrAusente) {
		t.Fatalf("Ler de outro provedor: err = %v, quero ErrAusente", err)
	}
	if err := m.Gravar("openai", "sk-outra-chave-bem-comprida-1234"); err != nil {
		t.Fatalf("regravar: %v", err)
	}
	if got, _ := m.Ler("openai"); got != "sk-outra-chave-bem-comprida-1234" {
		t.Fatalf("regravar não substituiu: %q", got)
	}
	if err := m.Apagar("openai"); err != nil {
		t.Fatalf("Apagar: %v", err)
	}
	if _, err := m.Ler("openai"); !errors.Is(err, ErrAusente) {
		t.Fatalf("Ler depois de apagar: err = %v, quero ErrAusente", err)
	}
	if err := m.Apagar("openai"); err != nil {
		t.Fatalf("Apagar sem chave não é erro: %v", err)
	}
}

func TestMemoria_RecusaOInvalidoSemGuardar(t *testing.T) {
	m := NovaMemoria()
	if err := m.Gravar("openai", "sk-abc def"); !errors.Is(err, ErrChaveInvalida) {
		t.Fatalf("err = %v, quero ErrChaveInvalida", err)
	}
	if err := m.Gravar("OpenAI", chaveDeTeste); !errors.Is(err, ErrProvedor) {
		t.Fatalf("err = %v, quero ErrProvedor", err)
	}
	if _, err := m.Ler("openai"); !errors.Is(err, ErrAusente) {
		t.Fatalf("a chave inválida foi guardada: %v", err)
	}
}

// TestGerenciador_RecusaSobGoTest: o Cofre de produção NUNCA toca o
// Gerenciador de Credenciais real a partir de um teste — nem para ler. No
// Windows é o portão sysinput que recusa, antes de qualquer chamada ao
// sistema; fora dele, não há Gerenciador. E a recusa não carrega a chave.
func TestGerenciador_RecusaSobGoTest(t *testing.T) {
	quero := sysinput.ErrDisabledInTest
	if runtime.GOOS != "windows" {
		quero = ErrNaoSuportado
	}
	g := NovoGerenciador()

	errGravar := g.Gravar("openai", chaveDeTeste)
	_, errLer := g.Ler("openai")
	errApagar := g.Apagar("openai")
	for nome, err := range map[string]error{"Gravar": errGravar, "Ler": errLer, "Apagar": errApagar} {
		if !errors.Is(err, quero) {
			t.Errorf("%s: err = %v, quero %v", nome, err, quero)
		}
		if err != nil && strings.Contains(err.Error(), chaveDeTeste) {
			t.Errorf("%s: a mensagem de erro carrega a chave", nome)
		}
	}
}

// O comentário no Gerenciador diz o que a credencial é: a chave de API de um
// provedor de IA, ou o token do PC na nuvem (F6).
func TestComentarioDe(t *testing.T) {
	const api, nuvem = "Aurora Whisper: chave de API do provedor de IA", "Aurora Whisper: token deste PC na nuvem"
	for provedor, quero := range map[string]string{
		"openai": api, "anthropic": api, "nuvemx": api, "x-nuvem": api,
		"nuvem": nuvem, "nuvem-portatil": nuvem,
	} {
		if got := ComentarioDe(provedor); got != quero {
			t.Errorf("ComentarioDe(%q) = %q, quero %q", provedor, got, quero)
		}
	}
}
