package smartconta

import (
	"errors"
	"reflect"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// As conferências do Agent, puras sobre a Semente: nenhum teste toca um
// dicionario.json fora de t.TempDir().

// TestDominiosDaConferencia: o termo restrito a um domínio confere só no dele;
// o de domínio não restrito, no desconhecido (nil) e no dele; todos os outros
// casos, só no desconhecido. Com a restrição da variante (o alias restrito, ou
// a efetiva do par que chega), o termo de domínio não restrito confere só no
// dele; sem domínio, a restrição não muda nada (só o desconhecido, onde o
// restrito sem domínio não age). A comparação do termo é a do Search: sem
// caixa, acento nem espaço.
func TestDominiosDaConferencia(t *testing.T) {
	s := dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
		{Canonica: "Zulu", Dominio: "dev", Restrito: true},
		{Canonica: "Yankee", Dominio: "dev"},
		{Canonica: "Xray"},
		{Canonica: "Whiskey", Restrito: true},
		{Canonica: "Nota Fiscal", Dominio: "contabil", Restrito: true},
	}}
	casos := []struct {
		termo    string
		restrito bool
		quer     []string // "" é nil
	}{
		{"Zulu", false, []string{"dev"}},
		{"zulu", false, []string{"dev"}},
		{"nota fiscal", false, []string{"contabil"}},
		{"notafiscal", false, []string{"contabil"}},
		{"Yankee", false, []string{"", "dev"}},
		{"yankee", false, []string{"", "dev"}},
		{"Xray", false, []string{""}},
		{"Whiskey", false, []string{""}},
		{"Termo Novo", false, []string{""}},
		// A restrição efetiva do par (F8g).
		{"Yankee", true, []string{"dev"}},
		{"Zulu", true, []string{"dev"}},
		{"Xray", true, []string{""}},
		{"Whiskey", true, []string{""}},
		{"Termo Novo", true, []string{""}},
	}
	for _, c := range casos {
		t.Run(c.termo, func(t *testing.T) {
			var got []string
			for _, d := range DominiosDaConferencia(s, c.termo, c.restrito) {
				if d == nil {
					got = append(got, "")
				} else {
					got = append(got, *d)
				}
			}
			if !reflect.DeepEqual(got, c.quer) {
				t.Errorf("DominiosDaConferencia(%q, %v) = %q, quero %q", c.termo, c.restrito, got, c.quer)
			}
		})
	}
}

// sementeDoPC2 é o PC que recebe: DIRF tem o domínio contabil, sem restrição;
// LALUR é restrito a contabil; ECD não tem domínio.
func sementeDoPC2() dictionary.Semente {
	return dictionary.Semente{
		Usuario:  "mauro",
		Dominios: map[string][]string{"contabil": {"SistemaContabil.exe"}},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "DIRF", Prioridade: 90, Dominio: "contabil"},
			{Canonica: "LALUR", Prioridade: 90, Dominio: "contabil", Restrito: true},
			{Canonica: "ECD", Prioridade: 90},
		},
	}
}

// O par restrito que chega a um PC cujo termo tem o domínio sem restrição
// entra em alias_restrito (a adaptação deste PC), e o ValidarPar o confere só
// no domínio da entrada, onde o Search o mostra. Antes da F8g, a conferência
// rodava também no domínio desconhecido, onde o alias restrito nunca age, e o
// par ficava sempre "não aplicado" (nao_agiria).
func TestValidarPar_AliasRestritoConfereSoNoDominio(t *testing.T) {
	s := sementeDoPC2()
	par := dictionary.ParDaNuvem{Variante: "dirfi", Termo: "DIRF", Restrito: true, Dominio: "contabil"}
	novo, lote, err := s.AplicarPares([]dictionary.ParDaNuvem{par},
		dictionary.ConferenciasDoLote{PalavraComum: PalavraComum, Agiria: ValidarPar})
	if err != nil {
		t.Fatal(err)
	}
	r := lote.Resultados[0]
	if r.Situacao != dictionary.SituacaoAplicado || r.Projecao == nil || !r.Projecao.EmAliasRestrito || !r.Projecao.Restrito {
		t.Fatalf("o par restrito: %+v (projeção %+v); quero aplicado em alias_restrito", r, r.Projecao)
	}
	// A conferência antiga (sem a restrição do par) recusaria.
	if err := Agiria(novo, dictionary.Aprendida{Termo: "DIRF", Variante: "dirfi"}); !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Fatalf("pré-condição: no desconhecido o alias restrito agiria (%v)", err)
	}
	if err := ValidarPar(novo, *r.Projecao); err != nil {
		t.Errorf("ValidarPar = %v, quero nil", err)
	}

	// O par sem restrição no mesmo termo vai a alias, e confere nos dois.
	par2 := dictionary.ParDaNuvem{Variante: "dirfe", Termo: "DIRF", Dominio: "contabil"}
	_, lote, err = s.AplicarPares([]dictionary.ParDaNuvem{par2},
		dictionary.ConferenciasDoLote{PalavraComum: PalavraComum, Agiria: ValidarPar})
	if err != nil {
		t.Fatal(err)
	}
	if r := lote.Resultados[0]; r.Situacao != dictionary.SituacaoAplicado || r.Projecao.EmAliasRestrito {
		t.Fatalf("o par sem restrição: %+v", r)
	}
}

// O par restrito que não age no domínio dele fica "não aplicado"
// (nao_agiria), e o arquivo não muda: "Vite" (90), restrito a contabil, leva
// a busca por "Viti" ali, e "Vitti" (60) é o termo do par. A conferência roda
// só em contabil (o par é restrito), e é lá que ele perde.
func TestValidarPar_ConcorrenteNoDominioRecusa(t *testing.T) {
	s := dictionary.Semente{
		Usuario:  "mauro",
		Dominios: map[string][]string{"contabil": {"SistemaContabil.exe"}},
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "Vite", Prioridade: 90, Dominio: "contabil", Restrito: true},
			{Canonica: "Vitti", Prioridade: 60, Dominio: "contabil"},
		},
	}
	_, lote, err := s.AplicarPares([]dictionary.ParDaNuvem{{Variante: "Viti", Termo: "Vitti", Restrito: true, Dominio: "contabil"}},
		dictionary.ConferenciasDoLote{PalavraComum: PalavraComum, Agiria: ValidarPar})
	if err != nil {
		t.Fatal(err)
	}
	r := lote.Resultados[0]
	if r.Situacao != dictionary.SituacaoNaoAplicado || r.Motivo != dictionary.MotivoNaoAgiria || lote.Mudou {
		t.Errorf("o par que perde no domínio: %+v, mudou %v; quero não aplicado (nao_agiria), sem gravar", r, lote.Mudou)
	}
}

// O que o painel aprende continua conferido como antes: a variante em alias
// herda a restrição da entrada (Agiria, sem restrição a mais).
func TestAgiria_ComoOPainel(t *testing.T) {
	s := sementeDoPC2()
	novo, feito, err := s.AprenderVariante("LALUR", "lalurr")
	if err != nil {
		t.Fatal(err)
	}
	if err := Agiria(novo, feito); err != nil {
		t.Errorf("a variante do termo restrito, conferida no domínio dele: %v", err)
	}
	novo, feito, err = s.AprenderVariante("ECD", "ecedê")
	if err != nil {
		t.Fatal(err)
	}
	if err := Agiria(novo, feito); err != nil {
		t.Errorf("a variante do termo sem domínio: %v", err)
	}
}

func TestPalavraComum(t *testing.T) {
	if PalavraComum("dirfi", true) != "" || PalavraComum("DIRF", false) != "" {
		t.Error("um termo técnico foi tido por palavra comum")
	}
	if PalavraComum("não", true) == "" {
		t.Error("a polaridade passou como grafia")
	}
}

// TestConferencias_SaoAsDoPC: Conferencias entrega a palavra comum e o "agiria"
// do par recebido, as duas do Agent, e nenhuma é nula.
func TestConferencias_SaoAsDoPC(t *testing.T) {
	c := Conferencias()
	if c.PalavraComum == nil || c.Agiria == nil {
		t.Fatalf("Conferencias() = %+v, quero as duas", c)
	}
	if c.PalavraComum("não", true) == "" || c.PalavraComum("dirfi", true) != "" {
		t.Error("PalavraComum não é a do PC")
	}
	s := sementeDoPC2()
	par := dictionary.ParDoPainel{Variante: "dirfi", Termo: "DIRF", Restrito: true, Dominio: "contabil", EmAliasRestrito: true}
	novo, _, err := s.AplicarPares([]dictionary.ParDaNuvem{par.ParaNuvem()}, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Agiria(novo, par); err != nil {
		t.Errorf("Agiria = %v, quero nil: é o ValidarPar, que confere o alias restrito no domínio dele", err)
	}
}
