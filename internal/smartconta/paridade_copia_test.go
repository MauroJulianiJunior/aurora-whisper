package smartconta_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/aprendiz"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
)

// A PARIDADE COM A CÓPIA (ADR-023, D): o Smart da conta montado da cópia do
// dicionário de um PC escreve o mesmo texto que o Smart desse PC, com o app em
// foco fora do mapa de domínios (Domain nulo), com e sem o aprendido por cima.
// O PC aqui é um dicionario.json de verdade em t.TempDir(), com o Aplicador de
// verdade; a nuvem recebe os bytes que o PC mandaria (BytesDaCopia) e os confere
// como a rota confere (LerCopia). Nenhum teste aqui toca o bin\, abre áudio,
// digita ou usa a área de transferência.

// contaDaNuvem é o dono da conta na nuvem: outro nome que o usuário do PC, para
// provar que a montagem troca o usuário da cópia.
const contaDaNuvem = "0b8f3c5e-6a43-4e2b-9f1a-6d2c1e7b9a10"

// dicionarioDoPC é um arquivo com o que a cópia leva e o que ela corta: o mapa de
// domínios, um termo de domínio sem restrição (vale fora do domínio), uma
// variante restrita (não vale), uma entrada restrita (não vale), uma desligada,
// um termo com dica e um do Aprender.
func dicionarioDoPC() dictionary.Semente {
	return dictionary.Semente{
		Usuario:  usuario,
		Dominios: map[string][]string{"contabil": {"Dominio.exe"}, "dev": {"Code.exe"}},
		AliasOrigem: map[string]string{
			"promptes": dictionary.OrigemAdicionado,
			"esseefe":  dictionary.OrigemPainel,
		},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "DCTFWeb", Prioridade: 100, Dominio: "contabil", Alias: []string{"dectefe web"}},
			{Canonica: "Vitti", Prioridade: 90, Dominio: "contabil", Alias: []string{"viti"}, AliasRestrito: []string{"vite"}},
			{Canonica: "commit", Prioridade: 80, Dominio: "dev", Restrito: true, Alias: []string{"comiti"}},
			{Canonica: "prompts", Prioridade: dictionary.PrioridadeDica, Alias: []string{"promptes"}, Origem: dictionary.OrigemAdicionado},
			{Canonica: "Desligado", Prioridade: 70, Inativa: true, Alias: []string{"desligadu"}},
			{Canonica: "ECF", Prioridade: dictionary.PrioridadeTermoAprendido, Alias: []string{"esseefe"}, Origem: dictionary.OrigemPainel},
			{Canonica: "Anthropic", Prioridade: 60, Alias: []string{"antropic"}},
		},
	}
}

// dicionarioDoDataset é o dicionário da medição do portão, com o usuário do PC.
func dicionarioDoDataset(t *testing.T) dictionary.Semente {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "asr-dataset", "dicionario.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := dictionary.CarregarSemente(f)
	if err != nil {
		t.Fatal(err)
	}
	s.Usuario = usuario
	return s
}

// smartDoPCComArquivo é o PC com este dicionario.json, depois de receber os
// pares pelo Aplicador (num lote só, como o pull). Devolve o Smart e a semente
// final do arquivo.
func smartDoPCComArquivo(t *testing.T, s dictionary.Semente, pares []dictionary.ParDaNuvem) (*resolver.SmartResolver, dictionary.Semente) {
	t.Helper()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	caminho := filepath.Join(t.TempDir(), "dicionario.json")
	if err := os.WriteFile(caminho, b, 0o600); err != nil {
		t.Fatal(err)
	}
	var troca *aprendiz.Troca
	a := aprendiz.Novo(aprendiz.Config{
		Caminho: caminho,
		Usuario: s.Usuario,
		Trocar:  func(x aprendiz.Troca) { troca = &x },
	})
	semente, _, err := a.AplicarPares(func(dictionary.Semente) []dictionary.ParDaNuvem { return pares }, nil)
	if err != nil {
		t.Fatalf("AplicarPares do PC: %v", err)
	}
	if troca == nil {
		smart, err := smartconta.SmartDaSemente(semente)
		if err != nil {
			t.Fatal(err)
		}
		return smart, semente
	}
	return troca.Smart, troca.Semente
}

// copiaDe é o que a nuvem guarda e lê: os bytes do PC, conferidos pela rota.
func copiaDe(t *testing.T, s dictionary.Semente) dictionary.Semente {
	t.Helper()
	b, _, err := s.BytesDaCopia()
	if err != nil {
		t.Fatalf("BytesDaCopia: %v", err)
	}
	c, err := dictionary.LerCopia(b)
	if err != nil {
		t.Fatalf("LerCopia: %v", err)
	}
	return c
}

// textosDoDicionario são cada termo e cada variante (as restritas também),
// sozinhos e no meio de uma frase.
func textosDoDicionario(s dictionary.Semente, fixos ...string) []string {
	var out []string
	for _, e := range s.Entradas {
		for _, v := range append(append([]string{e.Canonica}, e.Alias...), e.AliasRestrito...) {
			out = append(out, v, "x "+v+" y", "antes de tudo, "+v+". Depois disso, o resto.")
		}
	}
	return append(out, fixos...)
}

func conferirParidade(t *testing.T, smartPC *resolver.SmartResolver, conta *smartconta.Conta, textos []string) {
	t.Helper()
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
}

func TestParidadeComCopia_OSmartDaContaEscreveComoOPC(t *testing.T) {
	pares := []dictionary.ParDaNuvem{
		{Variante: "rague", Termo: "RAG"},
		{Variante: "anthropique", Termo: "Anthropic"},
		{Variante: "viti", Termo: "Vitti", Dominio: "contabil"},
		{Variante: "promptis", Termo: "prompts"},
	}
	fixos := []string{"", "texto sem termo nenhum", "o rague e o antropic e o viti", "quero dois, quer dizer três"}

	casos := []struct {
		nome   string
		pc     func(t *testing.T) dictionary.Semente
		quer   map[string]string
		comPar map[string]string
	}{
		{
			nome: "o arquivo com tudo o que a cópia corta",
			pc:   func(*testing.T) dictionary.Semente { return dicionarioDoPC() },
			quer: map[string]string{
				// O termo de um domínio sem restrição vale fora dele, como no PC
				// num programa fora do mapa.
				"mande a dectefe web": "mande a DCTFWeb",
				"o viti chegou":       "o Vitti chegou",
				// A variante restrita e a entrada restrita não valem sem domínio.
				"a vite subiu":     "a vite subiu",
				"faça o comiti":    "faça o comiti",
				"o desligadu":      "o desligadu",
				"os promptes":      "os prompts",
				"o esseefe saiu":   "o ECF saiu",
				"a antropic lança": "a Anthropic lança",
			},
			comPar: map[string]string{"o rague e o anthropique": "o RAG e o Anthropic"},
		},
		{
			nome: "o dicionário do dataset",
			pc:   dicionarioDoDataset,
		},
	}
	for _, c := range casos {
		t.Run(c.nome+", sem aprendido", func(t *testing.T) {
			arquivo := c.pc(t)
			smartPC, _ := smartDoPCComArquivo(t, arquivo, nil)
			conta, err := smartconta.MontarComCopia(contaDaNuvem, ptr(copiaDe(t, arquivo)), nil)
			if err != nil {
				t.Fatalf("MontarComCopia: %v", err)
			}
			conferirParidade(t, smartPC, conta, textosDoDicionario(arquivo, fixos...))
			for texto, quer := range c.quer {
				if got := conta.Resolver(context.Background(), texto).Texto; got != quer {
					t.Errorf("texto %q: conta = %q, quero %q", texto, got, quer)
				}
			}
			if r := conta.Resumo(); !r.ComCopia || r.TermosDaCopia != len(arquivo.Entradas) {
				t.Errorf("resumo = %+v", r)
			}
		})
		// A cópia de ANTES do pull: o PC fonte ainda não recebeu os pares, e a
		// nuvem os aplica por cima, como o PC fará.
		t.Run(c.nome+", cópia antes do aprendido", func(t *testing.T) {
			arquivo := c.pc(t)
			smartPC, final := smartDoPCComArquivo(t, arquivo, pares)
			conta, err := smartconta.MontarComCopia(contaDaNuvem, ptr(copiaDe(t, arquivo)), maisRecentePrimeiro(pares))
			if err != nil {
				t.Fatalf("MontarComCopia: %v", err)
			}
			conferirParidade(t, smartPC, conta, textosDoDicionario(final, append(fixos, textosDe(pares)...)...))
			for texto, quer := range c.comPar {
				if got := conta.Resolver(context.Background(), texto).Texto; got != quer {
					t.Errorf("texto %q: conta = %q, quero %q", texto, got, quer)
				}
			}
		})
		// A cópia de DEPOIS do pull: os pares já estão nela, e aplicá-los de novo
		// não muda nada.
		t.Run(c.nome+", cópia depois do aprendido", func(t *testing.T) {
			arquivo := c.pc(t)
			smartPC, final := smartDoPCComArquivo(t, arquivo, pares)
			conta, err := smartconta.MontarComCopia(contaDaNuvem, ptr(copiaDe(t, final)), maisRecentePrimeiro(pares))
			if err != nil {
				t.Fatalf("MontarComCopia: %v", err)
			}
			conferirParidade(t, smartPC, conta, textosDoDicionario(final, append(fixos, textosDe(pares)...)...))
		})
	}
}

// A dica da cópia é o prompt do PC: a mesma função sobre as mesmas entradas, e o
// aprendido por cima não a muda.
func TestDicaDaCopia_EhOPromptDoPC(t *testing.T) {
	for _, arquivo := range []dictionary.Semente{dicionarioDoPC(), dicionarioDoDataset(t)} {
		promptDoPC := dictionary.Vocabulario(arquivo.ParaEntradas(), dictionary.OrcamentoRunasPadrao)
		if promptDoPC == "" {
			t.Fatal("o prompt do PC veio vazio: o teste não provaria nada")
		}
		if got := smartconta.DicaDaCopia(copiaDe(t, arquivo)); got != promptDoPC {
			t.Errorf("dica = %q, o PC manda %q", got, promptDoPC)
		}
		_, final := smartDoPCComArquivo(t, arquivo, []dictionary.ParDaNuvem{
			{Variante: "rague", Termo: "RAG"}, {Variante: "promptis", Termo: "prompts"},
		})
		if got := smartconta.DicaDaCopia(copiaDe(t, final)); got != promptDoPC {
			t.Errorf("com o aprendido, dica = %q, quero %q", got, promptDoPC)
		}
	}
	// O termo com dica vem primeiro, e o do Aprender e o desligado ficam de fora.
	dica := smartconta.DicaDaCopia(copiaDe(t, dicionarioDoPC()))
	if len(dica) < len("prompts") || dica[:len("prompts")] != "prompts" {
		t.Errorf("dica = %q, o termo com dica não veio primeiro", dica)
	}
	for _, fora := range []string{"ECF", "Desligado"} {
		if containsTermo(dica, fora) {
			t.Errorf("dica = %q leva %q", dica, fora)
		}
	}
}

func containsTermo(dica, termo string) bool {
	for _, t := range splitVirgula(dica) {
		if t == termo {
			return true
		}
	}
	return false
}

func splitVirgula(s string) []string {
	var out []string
	inicio := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] == ',' && s[i+1] == ' ' {
			out = append(out, s[inicio:i])
			inicio = i + 2
		}
	}
	return append(out, s[inicio:])
}

// A montagem não mexe na cópia: a mesma cópia serve a montagens seguidas.
func TestMontarComCopia_NaoMexeNaCopia(t *testing.T) {
	copia := copiaDe(t, dicionarioDoPC())
	antes := copiaDe(t, dicionarioDoPC())
	if _, err := smartconta.MontarComCopia(contaDaNuvem, &copia, maisRecentePrimeiro([]dictionary.ParDaNuvem{
		{Variante: "rague", Termo: "RAG"}, {Variante: "anthropique", Termo: "Anthropic"},
	})); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copia, antes) {
		t.Errorf("a montagem mexeu na cópia:\n got %+v\nwant %+v", copia, antes)
	}
}

// Sem cópia, MontarComCopia é o Montar de sempre.
func TestMontarComCopia_SemCopiaEhOMontar(t *testing.T) {
	pares := maisRecentePrimeiro([]dictionary.ParDaNuvem{{Variante: "rague", Termo: "RAG"}})
	a, err := smartconta.Montar(usuario, pares)
	if err != nil {
		t.Fatal(err)
	}
	b, err := smartconta.MontarComCopia(usuario, nil, pares)
	if err != nil {
		t.Fatal(err)
	}
	if a.Resumo() != b.Resumo() || b.Resumo().ComCopia {
		t.Errorf("resumos: %+v e %+v", a.Resumo(), b.Resumo())
	}
	for _, texto := range []string{"o rague", "nada"} {
		if x, y := a.Resolver(context.Background(), texto).Texto, b.Resolver(context.Background(), texto).Texto; x != y {
			t.Errorf("%q: %q e %q", texto, x, y)
		}
	}
}

func ptr[T any](v T) *T { return &v }
