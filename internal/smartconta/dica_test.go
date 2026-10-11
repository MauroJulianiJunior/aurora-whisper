package smartconta

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// A dica do aprendido (ADR-021, decisão E): os termos distintos, do mais
// recente ao mais antigo, até 140 runas, sem par preso a um domínio. Ela vem
// desligada por padrão, mas a função existe e é testada.

func TestDicaLigadaPorPadrao(t *testing.T) {
	if DicaLigadaPorPadrao {
		t.Error("a dica vem desligada até a C8 medir (ADR-021, escolha menor 8)")
	}
}

func TestOrcamentoDaDica_EOdoPC(t *testing.T) {
	// O número que a ADR escreve (140 runas) e o orçamento do PC são o mesmo:
	// se o do PC mudar, a dica muda junto, e a ADR precisa saber.
	if OrcamentoDaDica != 140 {
		t.Errorf("OrcamentoDaDica = %d, quero as 140 runas da ADR-021", OrcamentoDaDica)
	}
	if dictionary.OrcamentoRunasPadrao != OrcamentoDaDica {
		t.Errorf("o orçamento do PC (%d) e o da dica (%d) divergiram", dictionary.OrcamentoRunasPadrao, OrcamentoDaDica)
	}
}

func TestDica_Tabela(t *testing.T) {
	casos := []struct {
		nome  string
		pares []dictionary.ParDaNuvem // do mais recente ao mais antigo
		quer  string
	}{
		{"sem pares", nil, ""},
		{"um termo", []dictionary.ParDaNuvem{{Variante: "rague", Termo: "RAG"}}, "RAG"},
		{
			"do mais recente ao mais antigo",
			[]dictionary.ParDaNuvem{
				{Variante: "c", Termo: "Terceiro"},
				{Variante: "b", Termo: "Segundo"},
				{Variante: "a", Termo: "Primeiro"},
			},
			"Terceiro, Segundo, Primeiro",
		},
		{
			"termos distintos: o termo repetido entra uma vez, na posição do mais recente",
			[]dictionary.ParDaNuvem{
				{Variante: "ragui", Termo: "RAG"},
				{Variante: "dirfi", Termo: "DIRF"},
				{Variante: "rague", Termo: "RAG"},
			},
			"RAG, DIRF",
		},
		{
			"termos iguais para o dicionário (caixa, acento, espaço) são um só, com a grafia do mais recente",
			[]dictionary.ParDaNuvem{
				{Variante: "x", Termo: "Nota Fiscal"},
				{Variante: "y", Termo: "nota fiscal"},
				{Variante: "z", Termo: "NotaFiscal"},
				{Variante: "w", Termo: "Nôta Fiscál"},
			},
			"Nota Fiscal",
		},
		{
			"o par preso a um domínio não entra",
			[]dictionary.ParDaNuvem{
				{Variante: "viti", Termo: "Vitti", Dominio: "contabil"},
				{Variante: "rague", Termo: "RAG"},
			},
			"RAG",
		},
		{
			"o par restrito não entra",
			[]dictionary.ParDaNuvem{
				{Variante: "viti", Termo: "Vitti", Restrito: true},
				{Variante: "rague", Termo: "RAG"},
			},
			"RAG",
		},
		{
			"o par desfeito não entra",
			[]dictionary.ParDaNuvem{
				{Variante: "ecefe", Termo: "ECF", Desfeito: true},
				{Variante: "rague", Termo: "RAG"},
			},
			"RAG",
		},
		{
			"o termo que só tem par com domínio não entra, mesmo com outro par sem domínio de outro termo",
			[]dictionary.ParDaNuvem{
				{Variante: "a", Termo: "Vitti", Dominio: "contabil"},
				{Variante: "b", Termo: "Vitti", Dominio: "dev"},
				{Variante: "c", Termo: "DIRF"},
			},
			"DIRF",
		},
		{
			"o termo com um par com domínio e outro sem entra pelo sem domínio",
			[]dictionary.ParDaNuvem{
				{Variante: "a", Termo: "Vitti", Dominio: "contabil"},
				{Variante: "b", Termo: "Vitti"},
			},
			"Vitti",
		},
		{
			"o termo vazio não entra",
			[]dictionary.ParDaNuvem{{Variante: "a", Termo: "   "}, {Variante: "b", Termo: "!!!"}, {Variante: "c", Termo: "RAG"}},
			"RAG",
		},
	}
	for _, k := range casos {
		t.Run(k.nome, func(t *testing.T) {
			if got := Dica(k.pares); got != k.quer {
				t.Errorf("Dica = %q, quero %q", got, k.quer)
			}
		})
	}
}

// TestDica_Respeita140Runas: a dica nunca passa de 140 runas (o separador
// conta), o corte é em termo inteiro e começa pelos mais recentes.
func TestDica_Respeita140Runas(t *testing.T) {
	var pares []dictionary.ParDaNuvem // do mais recente ao mais antigo
	for i := 0; i < 60; i++ {
		pares = append(pares, dictionary.ParDaNuvem{
			Variante: fmt.Sprintf("v%d", i),
			Termo:    fmt.Sprintf("Termo%02d", i), // 7 runas
		})
	}
	dica := Dica(pares)
	if n := utf8.RuneCountInString(dica); n > OrcamentoDaDica {
		t.Fatalf("a dica tem %d runas, o teto é %d", n, OrcamentoDaDica)
	}
	termos := strings.Split(dica, ", ")
	// 7 runas por termo e 2 do separador: 7 + 9*n <= 140 => n = 15 (7 + 9*14 = 133).
	if len(termos) != 15 {
		t.Fatalf("a dica tem %d termos, quero 15 (140 runas, 7 por termo e 2 de separador): %q", len(termos), dica)
	}
	for i, termo := range termos {
		if quer := fmt.Sprintf("Termo%02d", i); termo != quer {
			t.Errorf("termo %d = %q, quero %q (do mais recente ao mais antigo, inteiro)", i, termo, quer)
		}
	}
}

// TestDica_TermoLongoNaoImpedeOsCurtos: o corte é o do PC. O termo que não
// cabe fica de fora e os seguintes, mais curtos, ainda entram.
func TestDica_TermoLongoNaoImpedeOsCurtos(t *testing.T) {
	longo := strings.Repeat("L", 79)
	pares := []dictionary.ParDaNuvem{ // do mais recente ao mais antigo
		{Variante: "a", Termo: longo + "1"}, // 80 runas
		{Variante: "b", Termo: longo + "2"}, // 80 + 2 de separador: 162 > 140, fica de fora
		{Variante: "c", Termo: "Curto"},     // 80 + 2 + 5 = 87
		{Variante: "d", Termo: "Menor"},     // 87 + 2 + 5 = 94
	}
	got := Dica(pares)
	if quer := longo + "1, Curto, Menor"; got != quer {
		t.Errorf("Dica = %q, quero %q", got, quer)
	}
	if n := utf8.RuneCountInString(got); n > OrcamentoDaDica {
		t.Errorf("a dica tem %d runas, o teto é %d", n, OrcamentoDaDica)
	}
}

// TestDica_TermoQueNuncaCabeFicaDeFora: um termo maior que o orçamento inteiro
// não entra, e os outros não são impedidos.
func TestDica_TermoQueNuncaCabeFicaDeFora(t *testing.T) {
	pares := []dictionary.ParDaNuvem{
		{Variante: "a", Termo: strings.Repeat("x", 141)},
		{Variante: "b", Termo: "RAG"},
	}
	if got := Dica(pares); got != "RAG" {
		t.Errorf("Dica = %q, quero %q", got, "RAG")
	}
}

// TestDica_NaoMudaOsPares: a função só lê.
func TestDica_NaoMudaOsPares(t *testing.T) {
	pares := []dictionary.ParDaNuvem{
		{Variante: "b", Termo: "Segundo"},
		{Variante: "a", Termo: "Primeiro", Dominio: "dev"},
	}
	antes := append([]dictionary.ParDaNuvem(nil), pares...)
	Dica(pares)
	for i := range pares {
		if pares[i] != antes[i] {
			t.Errorf("pares[%d] mudou: %+v, era %+v", i, pares[i], antes[i])
		}
	}
}
