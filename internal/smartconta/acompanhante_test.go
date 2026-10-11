package smartconta

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// O acompanhante (acompanhante.go) tem de dar as MESMAS respostas que o
// ValidarPar do PC. Aqui o lote roda duas vezes sobre os mesmos pares, uma com
// Conferencias() (o Smart montado do zero a cada par) e outra com as da conta
// (o Smart que acompanha a semente), e as duas têm de terminar com a mesma
// semente e os mesmos resultados, par a par.

// parQueInteressa junta pares que se atrapalham: a mesma variante em dois
// termos, o termo de um que é variante de outro, a variante que é palavra de
// outro termo, a mesma chave com caixa, acento e espaço diferentes, variantes
// que o Smart não aplicaria, palavra comum, par com domínio, restrito e
// desfeito.
var paresQueInteressam = []dictionary.ParDaNuvem{
	{Variante: "rague", Termo: "RAG"},
	{Variante: "ragui", Termo: "RAG"},
	{Variante: "Rague", Termo: "Rag"},
	{Variante: "rague", Termo: "RAGG"},
	{Variante: "dirfi", Termo: "DIRF"},
	{Variante: "dirfe", Termo: "DIRF"},
	{Variante: "ecedê", Termo: "ECD"},
	{Variante: "ecede", Termo: "ECD"},
	{Variante: "n oito n", Termo: "n8n"},
	{Variante: "ene oito ene", Termo: "n8n"},
	{Variante: "n8n", Termo: "N oito N"},
	{Variante: "pedro das", Termo: "Pedro Dias"},
	{Variante: "das", Termo: "DAS"},
	{Variante: "dass", Termo: "DAS"},
	{Variante: "pedro dass", Termo: "Pedro Dias"},
	{Variante: "viti", Termo: "Vitti"},
	{Variante: "vite", Termo: "Vitti"},
	{Variante: "Vitti", Termo: "Vite"},
	{Variante: "logui", Termo: "Loggi"},
	{Variante: "log", Termo: "Loggi"},
	{Variante: "sped fiscau", Termo: "Sped Fiscal"},
	{Variante: "sped", Termo: "SPED"},
	{Variante: "nota-fiscal", Termo: "Nota Fiscal"},
	{Variante: "notafiscal", Termo: "NF"},
	{Variante: "fisco", Termo: "Físico"},
	{Variante: "físico", Termo: "Fisco"},
	{Variante: "a b", Termo: "AB"},
	{Variante: "não", Termo: "NAO"},
	{Variante: "vinte e cinco", Termo: "25"},
	{Variante: "quer dizer", Termo: "QD"},
	// Com domínio, restritos e desfeitos.
	{Variante: "comite", Termo: "commit", Dominio: "dev", Restrito: true},
	{Variante: "comiti", Termo: "commit", Dominio: "dev"},
	{Variante: "ecefe", Termo: "ECF", Dominio: "contabil"},
	{Variante: "ecefi", Termo: "ECF", Dominio: "contabil", Restrito: true},
	{Variante: "folha", Termo: "Folha", Dominio: "rh"},
	{Variante: "ecefe", Termo: "ECF", Desfeito: true},
	{Variante: "rague", Termo: "RAG", Desfeito: true},
	{Variante: "fantasma", Termo: "Fantasma", Restrito: true},
	{Variante: "deslig", Termo: "Desligado"},
}

// sementesDoAcompanhante são os dicionários em que o lote roda: o da conta
// (vazio) e o de um PC com domínios, termos restritos, desligados e à mão.
func sementesDoAcompanhante() map[string]dictionary.Semente {
	return map[string]dictionary.Semente{
		"vazia": {Usuario: "u"},
		"de um PC": {
			Usuario:  "u",
			Dominios: map[string][]string{"contabil": {"SistemaContabil.exe"}, "dev": {"Code.exe"}},
			Entradas: []dictionary.EntradaSemente{
				{Canonica: "ECF", Prioridade: 90, Dominio: "contabil", Alias: []string{"ECFX"}},
				{Canonica: "ECD", Prioridade: 90, Dominio: "contabil", Alias: []string{"DAAS"}},
				{Canonica: "commit", Prioridade: 75, Dominio: "dev", Restrito: true, AliasRestrito: []string{"comite"}},
				{Canonica: "SPED", Prioridade: 95},
				{Canonica: "DAS", Prioridade: 90},
				{Canonica: "Vite", Prioridade: 90},
				{Canonica: "Fantasma", Prioridade: 60, Restrito: true},
				{Canonica: "Folha", Prioridade: 80, Dominio: "rh"},
				{Canonica: "Desligado", Prioridade: 10, Alias: []string{"deslig"}, Inativa: true},
			},
		},
	}
}

func emJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAcompanhante_IgualAoValidarPar(t *testing.T) {
	var refeitos, acrescidos int
	situacoes := map[dictionary.SituacaoDoPar]int{}
	motivos := map[dictionary.MotivoDoPar]int{}

	for nome, base := range sementesDoAcompanhante() {
		for semente := uint64(1); semente <= 120; semente++ {
			t.Run(fmt.Sprintf("%s/semente-%d", nome, semente), func(t *testing.T) {
				r := rand.New(rand.NewPCG(semente, 21))
				n := 1 + r.IntN(24)
				pares := make([]dictionary.ParDaNuvem, 0, n)
				for i := 0; i < n; i++ {
					pares = append(pares, paresQueInteressam[r.IntN(len(paresQueInteressam))])
				}

				querSemente, querLote, querErr := base.AplicarPares(pares, Conferencias())
				conferencias, acompanha := conferenciasDaConta()
				gotSemente, gotLote, gotErr := base.AplicarPares(pares, conferencias)
				if (querErr == nil) != (gotErr == nil) {
					t.Fatalf("erro do lote: referência = %v, acompanhante = %v", querErr, gotErr)
				}
				if querErr != nil {
					return
				}
				if a, b := emJSON(t, querSemente), emJSON(t, gotSemente); a != b {
					t.Fatalf("a semente difere:\nreferência:   %s\nacompanhante: %s\npares: %+v", a, b, pares)
				}
				if !reflect.DeepEqual(querLote, gotLote) {
					t.Fatalf("o lote difere:\nreferência:   %+v\nacompanhante: %+v\npares: %+v", querLote, gotLote, pares)
				}

				// O Smart final do acompanhante age como o montado do zero.
				smartFinal, err := acompanha.smartDe(gotSemente)
				if err != nil {
					t.Fatal(err)
				}
				smartZero, err := SmartDaSemente(querSemente)
				if err != nil {
					t.Fatal(err)
				}
				contaA := &Conta{usuario: "u", smart: smartFinal}
				contaB := &Conta{usuario: "u", smart: smartZero}
				for _, p := range paresQueInteressam {
					for _, texto := range []string{p.Variante, "x " + p.Variante + " y", p.Termo} {
						a, b := contaA.Resolver(t.Context(), texto), contaB.Resolver(t.Context(), texto)
						if a != b {
							t.Fatalf("o Smart difere em %q: acompanhante %+v, do zero %+v", texto, a, b)
						}
					}
				}

				refeitos += acompanha.refeitos
				acrescidos += acompanha.acrescidos
				for _, res := range gotLote.Resultados {
					situacoes[res.Situacao]++
					if res.Situacao == dictionary.SituacaoNaoAplicado {
						motivos[res.Motivo]++
					}
				}
			})
		}
	}

	// A prova só vale se os dois caminhos do acompanhante e os desfechos do
	// lote foram percorridos.
	if refeitos == 0 || acrescidos == 0 {
		t.Errorf("caminhos percorridos: refeitos = %d, acrescidos = %d; quero os dois", refeitos, acrescidos)
	}
	for _, s := range []dictionary.SituacaoDoPar{
		dictionary.SituacaoAplicado, dictionary.SituacaoConvergente, dictionary.SituacaoJaValeAqui,
		dictionary.SituacaoDesfeito, dictionary.SituacaoAusente, dictionary.SituacaoNaoAplicado,
	} {
		if situacoes[s] == 0 {
			t.Errorf("nenhum par terminou %q: o sorteio não cobre o caso", s)
		}
	}
	for _, m := range []dictionary.MotivoDoPar{
		dictionary.MotivoNaoAgiria, dictionary.MotivoPalavraComum, dictionary.MotivoVarianteDeOutroTermo,
		dictionary.MotivoDominioAusente,
	} {
		if motivos[m] == 0 {
			t.Errorf("nenhum par foi recusado por %q: o sorteio não cobre o caso", m)
		}
	}
}

// TestAcompanhante_RefazDoZeroQuandoASementeNaoSoCresce: a semente que perde uma
// variante, ou outro usuário, nunca é conferida sobre o Smart de antes.
func TestAcompanhante_RefazDoZeroQuandoASementeNaoSoCresce(t *testing.T) {
	a := &acompanhante{}
	base := dictionary.Semente{Usuario: "u"}
	s1, _, err := base.AprenderVariante("RAG", "rague")
	if err != nil {
		t.Fatal(err)
	}
	s2, _, err := s1.AprenderVariante("RAG", "ragui")
	if err != nil {
		t.Fatal(err)
	}
	passos := []struct {
		nome     string
		s        dictionary.Semente
		refeitos int
		acresc   int
	}{
		{"primeira vez monta do zero", s1, 1, 0},
		{"a mesma semente não muda nada", s1, 1, 1},
		{"variante nova só acrescenta", s2, 1, 2},
		{"volta a s1 (perdeu uma variante): refaz", s1, 2, 2},
		{"outro usuário: refaz", dictionary.Semente{Usuario: "outro", Entradas: s1.Entradas}, 3, 2},
	}
	for _, p := range passos {
		if _, err := a.smartDe(p.s); err != nil {
			t.Fatalf("%s: %v", p.nome, err)
		}
		if a.refeitos != p.refeitos || a.acrescidos != p.acresc {
			t.Errorf("%s: refeitos = %d, acrescidos = %d; quero %d e %d", p.nome, a.refeitos, a.acrescidos, p.refeitos, p.acresc)
		}
	}
}

// TestAcompanhante_QualquerCampoMudadoRefaz: o lote nunca muda a prioridade, o
// domínio, o idioma, a restrição, o desligamento nem a origem de um termo que já
// está no Smart; se a semente trouxer isso, o Smart se refaz em vez de confiar
// no que tem.
func TestAcompanhante_QualquerCampoMudadoRefaz(t *testing.T) {
	antes := dictionary.Semente{Usuario: "u", Entradas: []dictionary.EntradaSemente{
		{Canonica: "RAG", Prioridade: 50, Alias: []string{"rague"}},
	}}
	mudancas := map[string]func(e *dictionary.EntradaSemente){
		"prioridade":   func(e *dictionary.EntradaSemente) { e.Prioridade = 99 },
		"domínio":      func(e *dictionary.EntradaSemente) { e.Dominio = "dev" },
		"idioma":       func(e *dictionary.EntradaSemente) { e.Idioma = "pt-BR" },
		"restrição":    func(e *dictionary.EntradaSemente) { e.Restrito = true },
		"desligado":    func(e *dictionary.EntradaSemente) { e.Inativa = true },
		"origem":       func(e *dictionary.EntradaSemente) { e.Origem = dictionary.OrigemPainel },
		"forma":        func(e *dictionary.EntradaSemente) { e.Canonica = "RAGS" },
		"variante":     func(e *dictionary.EntradaSemente) { e.Alias = []string{"outra"} },
		"sem variante": func(e *dictionary.EntradaSemente) { e.Alias = nil },
	}
	for nome, mudar := range mudancas {
		t.Run(nome, func(t *testing.T) {
			a := &acompanhante{}
			if _, err := a.smartDe(antes); err != nil {
				t.Fatal(err)
			}
			depois := dictionary.Semente{Usuario: "u", Entradas: []dictionary.EntradaSemente{copiaDaEntrada(antes.Entradas[0])}}
			mudar(&depois.Entradas[0])
			if _, err := a.smartDe(depois); err != nil {
				t.Fatal(err)
			}
			if a.refeitos != 2 || a.acrescidos != 0 {
				t.Errorf("refeitos = %d, acrescidos = %d; quero o Smart refeito (2 e 0)", a.refeitos, a.acrescidos)
			}
		})
	}
}

// TestAcompanhante_FormaRepetidaRefaz: dois termos de mesma forma normalizada
// fariam o repositório fundir as entradas; o acompanhante deixa isso para a
// montagem do zero.
func TestAcompanhante_FormaRepetidaRefaz(t *testing.T) {
	a := &acompanhante{}
	antes := dictionary.Semente{Usuario: "u", Entradas: []dictionary.EntradaSemente{{Canonica: "RAG", Prioridade: 50}}}
	if _, err := a.smartDe(antes); err != nil {
		t.Fatal(err)
	}
	depois := dictionary.Semente{Usuario: "u", Entradas: []dictionary.EntradaSemente{
		{Canonica: "RAG", Prioridade: 50},
		{Canonica: "rag", Prioridade: 50},
	}}
	// Do zero, o Popular dessa semente falha ou funde; o que importa é que o
	// acompanhante dê o mesmo que SmartDaSemente.
	_, errAcomp := a.smartDe(depois)
	_, errZero := SmartDaSemente(depois)
	if (errAcomp == nil) != (errZero == nil) {
		t.Errorf("acompanhante = %v, do zero = %v", errAcomp, errZero)
	}
	if a.refeitos != 2 || a.acrescidos != 0 {
		t.Errorf("refeitos = %d, acrescidos = %d; quero o Smart refeito (2 e 0)", a.refeitos, a.acrescidos)
	}
}

// TestAcompanhante_AliasRestritoNovoSoAcrescenta: a variante nova em
// alias_restrito de um termo que já estava no Smart também só acrescenta, e o
// Smart que sai age como o montado do zero.
func TestAcompanhante_AliasRestritoNovoSoAcrescenta(t *testing.T) {
	antes := dictionary.Semente{Usuario: "u", Entradas: []dictionary.EntradaSemente{
		{Canonica: "commit", Prioridade: 75, Dominio: "dev", Restrito: true, Alias: []string{"comiti"}},
	}}
	depois := dictionary.Semente{Usuario: "u", Entradas: []dictionary.EntradaSemente{
		{Canonica: "commit", Prioridade: 75, Dominio: "dev", Restrito: true, Alias: []string{"comiti"}, AliasRestrito: []string{"comite"}},
	}}
	a := &acompanhante{}
	if _, err := a.smartDe(antes); err != nil {
		t.Fatal(err)
	}
	smart, err := a.smartDe(depois)
	if err != nil {
		t.Fatal(err)
	}
	if a.refeitos != 1 || a.acrescidos != 1 {
		t.Errorf("refeitos = %d, acrescidos = %d; quero 1 e 1", a.refeitos, a.acrescidos)
	}
	zero, err := SmartDaSemente(depois)
	if err != nil {
		t.Fatal(err)
	}
	dev := "dev"
	for _, texto := range []string{"comite", "comiti", "commit", "x comite y"} {
		for _, dominio := range []*string{nil, &dev} {
			a1, e1 := AgeNoDominio(smart, "u", dictionary.Aprendida{Termo: "commit", Variante: texto}, dominio)
			a2, e2 := AgeNoDominio(zero, "u", dictionary.Aprendida{Termo: "commit", Variante: texto}, dominio)
			if a1 != a2 || (e1 == nil) != (e2 == nil) {
				t.Errorf("%q no domínio %v: acompanhante %v/%v, do zero %v/%v", texto, dominio, a1, e1, a2, e2)
			}
		}
	}
}
