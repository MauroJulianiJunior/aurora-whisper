package smartconta

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// O Smart da conta, puro: nenhum teste toca disco, rede, áudio ou área de
// transferência. Os pares chegam na ordem do ListLearned (do mais recente ao
// mais antigo), como a C5b os entrega.

// paresDaConta é o aprendido de uma conta, do mais recente ao mais antigo:
//   - rague, n oito n, dirfi e ecedê: pares sem domínio, que agem;
//   - viti: preso ao domínio contabil, que não age no celular;
//   - ecefe: desfeito, que não age;
//   - não: palavra comum (a polaridade), que o lote recusa.
func paresDaConta() []dictionary.ParDaNuvem {
	return []dictionary.ParDaNuvem{
		{Variante: "não", Termo: "NAO"},
		{Variante: "ecefe", Termo: "ECF", Desfeito: true},
		{Variante: "viti", Termo: "Vitti", Dominio: "contabil"},
		{Variante: "ecedê", Termo: "ECD"},
		{Variante: "dirfi", Termo: "DIRF"},
		{Variante: "n oito n", Termo: "n8n"},
		{Variante: "rague", Termo: "RAG"},
	}
}

func montarOuFalhar(t *testing.T, pares []dictionary.ParDaNuvem) *Conta {
	t.Helper()
	c, err := Montar("conta-1", pares)
	if err != nil {
		t.Fatalf("Montar: %v", err)
	}
	return c
}

// TestResolver_Tabela: o par sem domínio corrige; o par com domínio, o desfeito
// e o ausente não agem; e o texto sem par passa igual.
func TestResolver_Tabela(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	casos := []struct {
		nome  string
		texto string
		quer  string
		subs  int
	}{
		{"par sem domínio corrige", "o rague é legal", "o RAG é legal", 1},
		{"par de várias palavras corrige", "usei o n oito n ontem", "usei o n8n ontem", 1},
		{"outro par sem domínio", "entreguei a dirfi hoje", "entreguei a DIRF hoje", 1},
		{"par com acento", "mande o ecedê agora", "mande o ECD agora", 1},
		{"caixa diferente da variante", "O RAGUE é legal", "O RAG é legal", 1},
		{"dois pares no mesmo texto", "o rague e a dirfi", "o RAG e a DIRF", 2},
		{"par com domínio não age", "a viti não muda", "a viti não muda", 0},
		{"par desfeito não age", "o ecefe fica", "o ecefe fica", 0},
		{"par ausente não age", "o sped fica", "o sped fica", 0},
		{"palavra comum recusada não age", "eu não vou", "eu não vou", 0},
		{"texto sem par passa igual", "texto sem par nenhum", "texto sem par nenhum", 0},
		{"texto vazio", "", "", 0},
	}
	for _, k := range casos {
		t.Run(k.nome, func(t *testing.T) {
			d := c.Resolver(context.Background(), k.texto)
			if d.Texto != k.quer {
				t.Errorf("Resolver(%q).Texto = %q, quero %q", k.texto, d.Texto, k.quer)
			}
			if d.Bruto != k.texto {
				t.Errorf("Bruto = %q, quero o texto do ASR como chegou (%q)", d.Bruto, k.texto)
			}
			if d.Substituicoes != k.subs {
				t.Errorf("Substituicoes = %d, quero %d", d.Substituicoes, k.subs)
			}
			if d.SmartFalhou || d.Lacos != 0 || d.PalavrasRemovidas != 0 {
				t.Errorf("sem laço e sem falha, veio %+v", d)
			}
		})
	}
}

// TestMontar_Resumo: as contagens da montagem, sem texto nenhum.
func TestMontar_Resumo(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	quer := Resumo{Recebidos: 7, ComDominio: 1, Desfeitos: 1, Aplicados: 4, NaoAplicados: 1, Termos: 4}
	if got := c.Resumo(); got != quer {
		t.Errorf("Resumo = %+v, quero %+v", got, quer)
	}
}

// TestMontar_SemPares: a conta sem aprendido tem um Smart que só faz a
// autocorreção, e nada quebra.
func TestMontar_SemPares(t *testing.T) {
	for _, pares := range [][]dictionary.ParDaNuvem{nil, {}} {
		c := montarOuFalhar(t, pares)
		if got := c.Resumo(); got != (Resumo{}) {
			t.Errorf("Resumo = %+v, quero o zero", got)
		}
		if d := c.Resolver(context.Background(), "o rague é legal"); d.Texto != "o rague é legal" {
			t.Errorf("sem pares o texto passa igual, veio %q", d.Texto)
		}
	}
}

func TestMontar_SemUsuario(t *testing.T) {
	if _, err := Montar("", paresDaConta()); !errors.Is(err, ErrSemUsuario) {
		t.Errorf("Montar sem usuário: erro = %v, quero ErrSemUsuario", err)
	}
}

// TestMontar_SoComDominioNaoAgeEmNenhumLugar: o par preso a um domínio nunca
// age no celular (fail-closed): nem sozinho, nem junto de um par sem domínio
// do mesmo termo, nem restrito.
func TestMontar_SoComDominioNaoAgeEmNenhumLugar(t *testing.T) {
	pares := []dictionary.ParDaNuvem{
		{Variante: "ragui", Termo: "RAG", Dominio: "dev", Restrito: true},
		{Variante: "ragi", Termo: "RAG", Dominio: "dev"},
		{Variante: "rague", Termo: "RAG"},
	}
	c := montarOuFalhar(t, pares)
	for texto, quer := range map[string]string{
		"o rague":    "o RAG",
		"o ragui":    "o ragui",
		"o ragi":     "o ragi",
		"o RAG fica": "o RAG fica",
	} {
		if d := c.Resolver(context.Background(), texto); d.Texto != quer {
			t.Errorf("Resolver(%q) = %q, quero %q", texto, d.Texto, quer)
		}
	}
	if r := c.Resumo(); r.ComDominio != 2 || r.Aplicados != 1 {
		t.Errorf("Resumo = %+v, quero 2 com domínio e 1 aplicado", r)
	}
}

// TestMontar_OMaisAntigoLevaAMelhorNoConflito: o lote aplica do mais antigo ao
// mais novo, como o pull entrega ao PC. A variante "Foo" do par novo é o termo
// do par antigo, e o par novo é o que o lote recusa.
func TestMontar_OMaisAntigoLevaAMelhorNoConflito(t *testing.T) {
	pares := []dictionary.ParDaNuvem{ // do mais recente ao mais antigo
		{Variante: "Foo", Termo: "Bar"},
		{Variante: "foo bar", Termo: "Foo"},
	}
	c := montarOuFalhar(t, pares)
	if d := c.Resolver(context.Background(), "foo bar"); d.Texto != "Foo" {
		t.Errorf("o par antigo age: %q, quero %q", d.Texto, "Foo")
	}
	if d := c.Resolver(context.Background(), "foo"); d.Texto != "foo" {
		t.Errorf("o par novo, que perde o conflito, não age: %q", d.Texto)
	}
	if r := c.Resumo(); r.Aplicados != 1 || r.NaoAplicados != 1 {
		t.Errorf("Resumo = %+v, quero 1 aplicado e 1 não aplicado", r)
	}
}

// TestMontar_TetoDePares: com mais de MaxPares, valem os mais recentes.
func TestMontar_TetoDePares(t *testing.T) {
	var pares []dictionary.ParDaNuvem // do mais recente ao mais antigo
	for i := 0; i < MaxPares+3; i++ {
		pares = append(pares, dictionary.ParDaNuvem{
			Variante: fmt.Sprintf("varzq%dx", i),
			Termo:    fmt.Sprintf("TermoZQ%dX", i),
		})
	}
	c, err := Montar("conta-1", pares)
	if err != nil {
		t.Fatal(err)
	}
	if r := c.Resumo(); r.Recebidos != MaxPares || r.Aplicados != MaxPares {
		t.Errorf("Resumo = %+v, quero %d recebidos e aplicados", r, MaxPares)
	}
	if d := c.Resolver(context.Background(), "veja varzq0x"); d.Texto != "veja TermoZQ0X" {
		t.Errorf("o par mais recente age: %q", d.Texto)
	}
	if d := c.Resolver(context.Background(), fmt.Sprintf("veja varzq%dx", MaxPares)); strings.Contains(d.Texto, "TermoZQ") {
		t.Errorf("o par além do teto não age: %q", d.Texto)
	}
}

// TestResolver_Colapsar: o laço de repetição do ASR sai antes do Smart, o bruto
// fica como chegou e as trocas valem sobre o texto sem o laço.
func TestResolver_Colapsar(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())

	laco := "vamos rague " + strings.Repeat("rague ", 11) + "agora"
	d := c.Resolver(context.Background(), laco)
	if d.Texto != "vamos RAG agora" {
		t.Errorf("Texto = %q, quero %q", d.Texto, "vamos RAG agora")
	}
	if d.Bruto != laco {
		t.Errorf("Bruto = %q, quero o texto do ASR com o laço, como chegou", d.Bruto)
	}
	if d.Lacos != 1 || d.PalavrasRemovidas != 11 || d.Substituicoes != 1 {
		t.Errorf("Lacos %d, PalavrasRemovidas %d, Substituicoes %d; quero 1, 11 e 1", d.Lacos, d.PalavrasRemovidas, d.Substituicoes)
	}

	// A fala repetida de propósito, abaixo do limiar do laço, fica.
	if d := c.Resolver(context.Background(), "não, não, não, rague"); d.Lacos != 0 || d.Texto != "não, não, não, RAG" {
		t.Errorf("a repetição curta não é laço: %+v", d)
	}
}

// TestResolver_SmartFalhaSegueOTextoDoASR: se o Smart falha, vale o texto do
// ASR, já sem o laço, como no PC.
func TestResolver_SmartFalhaSegueOTextoDoASR(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar() // o Resolve devolve o erro do contexto

	laco := "o rague " + strings.Repeat("rague ", 11) + "fim"
	d := c.Resolver(ctx, laco)
	if !d.SmartFalhou {
		t.Fatal("SmartFalhou = false com o contexto cancelado")
	}
	if d.Texto != "o rague fim" {
		t.Errorf("Texto = %q, quero o texto do ASR sem o laço (%q)", d.Texto, "o rague fim")
	}
	if d.Bruto != laco || d.Lacos != 1 || d.Substituicoes != 0 {
		t.Errorf("Ditado = %+v", d)
	}

	// Sem laço, o texto do ASR passa como chegou.
	if d := c.Resolver(ctx, "o rague é legal"); !d.SmartFalhou || d.Texto != "o rague é legal" {
		t.Errorf("sem laço: %+v", d)
	}
}

// TestConta_UsoConcorrente: a Conta montada só se lê. Roda com -race.
func TestConta_UsoConcorrente(t *testing.T) {
	c := montarOuFalhar(t, paresDaConta())
	feito := make(chan string, 16)
	for i := 0; i < cap(feito); i++ {
		go func() { feito <- c.Resolver(context.Background(), "o rague e a dirfi").Texto }()
	}
	for i := 0; i < cap(feito); i++ {
		if got := <-feito; got != "o RAG e a DIRF" {
			t.Errorf("Texto = %q", got)
		}
	}
}
