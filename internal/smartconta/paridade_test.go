package smartconta_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/aprendiz"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/repeticao"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
)

// A PARIDADE (ADR-021, decisão F e C2): o Smart da conta é idêntico ao do PC
// para os mesmos pares. O Smart do PC aqui é o de um Agent de dicionário vazio
// (o dicionario.json criado na instalação, sem nenhum termo) que recebeu os
// pares da nuvem pelo Aplicador de verdade (aprendiz.Aplicador, sobre um
// arquivo em t.TempDir()), e o ditado dele passa pelo Colapsar e pelo Smart
// como o winruntime faz. Nenhum teste aqui toca o bin\, abre áudio, digita ou
// usa a área de transferência.

const usuario = "conta-1"

// smartDoPC devolve o Smart e a semente de um PC de dicionário vazio depois de
// aplicar os pares, na ordem em que o pull os entrega (do mais antigo ao mais
// novo), num lote só.
func smartDoPC(t *testing.T, pares []dictionary.ParDaNuvem) (*resolver.SmartResolver, dictionary.Semente) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if criou, err := dictionary.CriarSeFaltar(caminho, usuario); err != nil || !criou {
		t.Fatalf("CriarSeFaltar = %v, %v", criou, err)
	}
	var troca *aprendiz.Troca
	a := aprendiz.Novo(aprendiz.Config{
		Caminho: caminho,
		Usuario: usuario,
		Trocar:  func(x aprendiz.Troca) { troca = &x },
	})
	semente, _, err := a.AplicarPares(func(dictionary.Semente) []dictionary.ParDaNuvem { return pares }, nil)
	if err != nil {
		t.Fatalf("AplicarPares do PC: %v", err)
	}
	if troca == nil {
		// Nada mudou no dicionário: o PC segue com o Smart do dicionário vazio.
		smart, err := smartconta.SmartDaSemente(semente)
		if err != nil {
			t.Fatal(err)
		}
		return smart, semente
	}
	return troca.Smart, troca.Semente
}

// ditadoDoPC é o que o PC faz com o texto do ASR (winruntime): tira o laço de
// repetição e resolve com o app em foco desconhecido (Domain nulo), sem contexto
// e com a nuvem desligada.
func ditadoDoPC(t *testing.T, smart *resolver.SmartResolver, bruto string) string {
	t.Helper()
	texto := repeticao.Colapsar(bruto).Texto
	out, err := smart.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:      usuario,
		ASRResult:   asr.ASRResult{Text: texto, Language: "pt-BR"},
		CloudPolicy: resolver.CloudDisabled,
	})
	if err != nil {
		t.Fatalf("Resolve do PC: %v", err)
	}
	return out.FinalText
}

// maisRecentePrimeiro inverte a ordem do pull (a do PC) para a do ListLearned
// (a que a C5b entrega a Montar).
func maisRecentePrimeiro(pares []dictionary.ParDaNuvem) []dictionary.ParDaNuvem {
	r := slices.Clone(pares)
	slices.Reverse(r)
	return r
}

// textosDe monta os textos da prova: cada variante e cada termo dos pares,
// sozinhos e no meio de uma frase, mais os textos fixos.
func textosDe(pares []dictionary.ParDaNuvem, fixos ...string) []string {
	var out []string
	for _, p := range pares {
		out = append(out,
			p.Variante,
			"x "+p.Variante+" y",
			"antes de tudo, "+p.Variante+". Depois disso, o resto.",
			p.Termo,
			"x "+p.Termo+" y")
	}
	return append(out, fixos...)
}

type cenarioDeParidade struct {
	nome string
	// pares na ordem do PC: do mais antigo ao mais novo.
	pares []dictionary.ParDaNuvem
	fixos []string
	// quer são as respostas esperadas para textos escolhidos (o resto só tem de
	// ser igual ao PC).
	quer map[string]string
}

func cenariosDeParidade() []cenarioDeParidade {
	gerados := make([]dictionary.ParDaNuvem, 0, 30)
	for i := 0; i < 30; i++ {
		gerados = append(gerados, dictionary.ParDaNuvem{
			Variante: fmt.Sprintf("variantezq%dk", i),
			Termo:    fmt.Sprintf("TermoZQ%dK", i),
		})
	}
	return []cenarioDeParidade{
		{
			nome: "pares sem domínio",
			pares: []dictionary.ParDaNuvem{
				{Variante: "rague", Termo: "RAG"},
				{Variante: "n oito n", Termo: "n8n"},
				{Variante: "dirfi", Termo: "DIRF"},
				{Variante: "ecedê", Termo: "ECD"},
				{Variante: "ragui", Termo: "RAG"},
			},
			fixos: []string{"", "texto sem par nenhum", "quero dois, quer dizer três", "o rague e a dirfi e o n oito n"},
			quer: map[string]string{
				"o rague e a dirfi e o n oito n": "o RAG e a DIRF e o n8n",
				"x ragui y":                      "x RAG y",
				"mande o ecedê agora":            "mande o ECD agora",
				"texto sem par nenhum":           "texto sem par nenhum",
			},
		},
		{
			nome: "com par de domínio, par desfeito e palavra comum",
			pares: []dictionary.ParDaNuvem{
				{Variante: "rague", Termo: "RAG"},
				{Variante: "viti", Termo: "Vitti", Dominio: "contabil"},
				{Variante: "viti", Termo: "Vitti", Dominio: "contabil", Restrito: true},
				{Variante: "ecefe", Termo: "ECF", Desfeito: true},
				{Variante: "não", Termo: "NAO"},
				{Variante: "dirfi", Termo: "DIRF"},
				{Variante: "ecefi", Termo: "ECF", Dominio: "contabil"},
			},
			fixos: []string{"a viti não muda", "o ecefe e o ecefi ficam", "eu não vou"},
			quer: map[string]string{
				"a viti não muda":         "a viti não muda",
				"o ecefe e o ecefi ficam": "o ecefe e o ecefi ficam",
				"eu não vou":              "eu não vou",
				"x rague y":               "x RAG y",
			},
		},
		{
			nome: "conflito: a variante de um é o termo de outro",
			pares: []dictionary.ParDaNuvem{
				{Variante: "foo bar", Termo: "Foo"},
				{Variante: "Foo", Termo: "Bar"},
			},
			quer: map[string]string{"foo bar": "Foo", "foo": "foo"},
		},
		{
			// O termo aprendido é só alias (AliasOnly): ao contrário do DAS que
			// a pessoa pôs à mão, ele não veta a frase "pedro das". É igual no
			// PC e na conta.
			nome: "o termo DAS aprendido não veta a frase pedro das",
			pares: []dictionary.ParDaNuvem{
				{Variante: "pedro das", Termo: "Pedro Dias"},
				{Variante: "daz", Termo: "DAS"},
			},
			fixos: []string{"falei com pedro das ontem", "mande o daz"},
			quer: map[string]string{
				"falei com pedro das ontem": "falei com Pedro Dias ontem",
				"mande o daz":               "mande o DAS",
			},
		},
		{
			// O que o lote recusa pelas conferências do PC: o que não age
			// ("a b", "das") e a palavra comum ("vinte e cinco").
			nome: "pares que as conferências do PC recusam",
			pares: []dictionary.ParDaNuvem{
				{Variante: "a b", Termo: "AB"},
				{Variante: "vinte e cinco", Termo: "25"},
				{Variante: "pedro das", Termo: "Pedro Dias"},
				{Variante: "das", Termo: "DAS"},
				{Variante: "rague", Termo: "RAG"},
			},
			fixos: []string{"o a b e o das", "vinte e cinco reais"},
			quer: map[string]string{
				"o a b e o das":       "o a b e o das",
				"vinte e cinco reais": "vinte e cinco reais",
				"x rague y":           "x RAG y",
			},
		},
		{
			nome:  "trinta pares",
			pares: gerados,
			quer: map[string]string{
				"veja variantezq0k e variantezq29k": "veja TermoZQ0K e TermoZQ29K",
			},
		},
		{
			nome:  "sem pares",
			pares: nil,
			fixos: []string{"o rague é legal", "quero dois, quer dizer três"},
			quer:  map[string]string{"o rague é legal": "o rague é legal"},
		},
	}
}

// TestParidade_SmartDaContaIgualAoDoPC: os mesmos pares e o mesmo texto dão o
// mesmo resultado no Smart da conta e no Smart de um PC de dicionário vazio.
func TestParidade_SmartDaContaIgualAoDoPC(t *testing.T) {
	for _, c := range cenariosDeParidade() {
		t.Run(c.nome, func(t *testing.T) {
			smartPC, _ := smartDoPC(t, c.pares)
			conta, err := smartconta.Montar(usuario, maisRecentePrimeiro(c.pares))
			if err != nil {
				t.Fatalf("Montar: %v", err)
			}

			textos := textosDe(c.pares, c.fixos...)
			for texto := range c.quer {
				textos = append(textos, texto)
			}
			for _, texto := range textos {
				pc := ditadoDoPC(t, smartPC, texto)
				d := conta.Resolver(context.Background(), texto)
				if d.SmartFalhou {
					t.Fatalf("o Smart da conta falhou em %q", texto)
				}
				if d.Texto != pc {
					t.Errorf("texto %q: conta = %q, PC = %q", texto, d.Texto, pc)
				}
			}
			for texto, quer := range c.quer {
				if got := conta.Resolver(context.Background(), texto).Texto; got != quer {
					t.Errorf("texto %q: conta = %q, quero %q", texto, got, quer)
				}
				if got := ditadoDoPC(t, smartPC, texto); got != quer {
					t.Errorf("texto %q: PC = %q, quero %q", texto, got, quer)
				}
			}
		})
	}
}

// TestParidade_ColapsarNoMesmoLugar: o laço de repetição sai igual nos dois, e o
// texto que sobra vai ao Smart.
func TestParidade_ColapsarNoMesmoLugar(t *testing.T) {
	pares := []dictionary.ParDaNuvem{{Variante: "rague", Termo: "RAG"}}
	smartPC, _ := smartDoPC(t, pares)
	conta, err := smartconta.Montar(usuario, pares)
	if err != nil {
		t.Fatal(err)
	}
	laco := "vamos falar do rague " + "e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles vai ser utilizado na hora de récu, e como cada um deles"
	pc := ditadoDoPC(t, smartPC, laco)
	d := conta.Resolver(context.Background(), laco)
	if d.Texto != pc || d.Lacos != 1 {
		t.Errorf("conta = %q (laços %d), PC = %q", d.Texto, d.Lacos, pc)
	}
	if d.Bruto != laco {
		t.Error("o bruto não é o texto do ASR como chegou")
	}
}

// TestParidade_AMesmaSementeDoPC: a semente que o lote da conta deixa é a que o
// Aplicador do PC grava no dicionario.json, byte a byte da serialização.
func TestParidade_AMesmaSementeDoPC(t *testing.T) {
	for _, c := range cenariosDeParidade() {
		t.Run(c.nome, func(t *testing.T) {
			_, doPC := smartDoPC(t, c.pares)

			var aplicaveis []dictionary.ParDaNuvem
			for _, p := range c.pares {
				if !p.Desfeito && p.Dominio == "" {
					aplicaveis = append(aplicaveis, p)
				}
			}
			daConta, _, err := dictionary.Semente{Usuario: usuario}.AplicarPares(aplicaveis, smartconta.Conferencias())
			if err != nil {
				t.Fatal(err)
			}
			// O dicionário vazio do PC tem "entradas": [] e o da conta, nil.
			if doPC.Entradas == nil {
				doPC.Entradas = []dictionary.EntradaSemente{}
			}
			if daConta.Entradas == nil {
				daConta.Entradas = []dictionary.EntradaSemente{}
			}
			a, _ := json.Marshal(doPC)
			b, _ := json.Marshal(daConta)
			if string(a) != string(b) {
				t.Errorf("a semente do PC e a da conta diferem:\nPC:    %s\nconta: %s", a, b)
			}
		})
	}
}
