package dictionary

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// sementeDoPainelJSON é o dicionário dos testes do painel: um termo com
// domínio e alias, um restrito, um inativo, um sem alias, e um alias com
// origem de dataset. Está no formato que Gravar escreve.
const sementeDoPainelJSON = `{
  "usuario": "mauro",
  "dominios": {
    "contabil": [
      "SistemaContabil.exe"
    ]
  },
  "alias_origem": {
    "DAAS": "cont-04"
  },
  "entradas": [
    {
      "canonica": "ECF",
      "prioridade": 90,
      "dominio": "contabil",
      "alias": [
        "ECFX"
      ]
    },
    {
      "canonica": "ECD",
      "prioridade": 90,
      "dominio": "contabil",
      "alias": [
        "DAAS"
      ]
    },
    {
      "canonica": "commit",
      "prioridade": 75,
      "dominio": "dev",
      "alias_restrito": [
        "comite"
      ],
      "restrito": true
    },
    {
      "canonica": "Desligado",
      "prioridade": 10,
      "alias": [
        "deslig"
      ],
      "inativa": true
    },
    {
      "canonica": "SPED",
      "prioridade": 95
    }
  ]
}
`

func sementeDoPainel(t *testing.T) Semente {
	t.Helper()
	s, err := CarregarSemente(strings.NewReader(sementeDoPainelJSON))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	return s
}

// emJSON serializa como o arquivo: é o que permite comparar sementes sem
// tropeçar em nil contra slice vazio.
func emJSON(t *testing.T, s Semente) string {
	t.Helper()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func entradaDe(t *testing.T, s Semente, canonica string) EntradaSemente {
	t.Helper()
	for _, e := range s.Entradas {
		if e.Canonica == canonica {
			return e
		}
	}
	t.Fatalf("a semente não tem o termo %q:\n%s", canonica, emJSON(t, s))
	return EntradaSemente{}
}

func TestFormatoDeTeste_EhOQueGravarEscreve(t *testing.T) {
	// Sem isto, os testes de "idêntico byte a byte" provariam pouco: partiriam
	// de um arquivo que a primeira gravação já reformataria.
	if got := emJSON(t, sementeDoPainel(t)); got != sementeDoPainelJSON {
		t.Fatalf("a semente de teste não está no formato de Gravar:\n%s", got)
	}
}

func TestAprenderVariante_EmTermoExistente(t *testing.T) {
	s := sementeDoPainel(t)
	got, ap, err := s.AprenderVariante("ECF", "SF")
	if err != nil {
		t.Fatalf("AprenderVariante: %v", err)
	}
	if ap != (Aprendida{Termo: "ECF", Variante: "SF"}) {
		t.Errorf("Aprendida = %+v", ap)
	}
	if len(got.Entradas) != len(s.Entradas) {
		t.Fatalf("criou entrada para termo que já existia: %d entradas", len(got.Entradas))
	}
	e := entradaDe(t, got, "ECF")
	if want := []string{"ECFX", "SF"}; strings.Join(e.Alias, ",") != strings.Join(want, ",") {
		t.Errorf("Alias = %v, want %v", e.Alias, want)
	}
	if e.Prioridade != 90 || e.Dominio != "contabil" || e.Origem != "" {
		t.Errorf("a entrada que já existia foi mexida além do alias: %+v", e)
	}
	if got.AliasOrigem["SF"] != OrigemPainel {
		t.Errorf("alias_origem[SF] = %q, want %q", got.AliasOrigem["SF"], OrigemPainel)
	}
	if got.AliasOrigem["DAAS"] != "cont-04" {
		t.Errorf("a origem de outro alias foi mexida: %v", got.AliasOrigem)
	}
}

// TestAprenderVariante_TermoSemCaixaCaiNoExistente: "ecf" tem de cair no
// termo "ECF" que existe, e não criar um segundo termo — que o Upsert
// trataria como o mesmo e trocaria a forma canônica para "ecf".
func TestAprenderVariante_TermoSemCaixaCaiNoExistente(t *testing.T) {
	s := sementeDoPainel(t)
	for _, termo := range []string{"ecf", "Ecf", "  ECF  ", "é c f"} {
		got, ap, err := s.AprenderVariante(termo, "SF")
		if err != nil {
			t.Fatalf("AprenderVariante(%q): %v", termo, err)
		}
		if ap.Termo != "ECF" {
			t.Errorf("%q: Termo = %q, quero a forma canônica ECF", termo, ap.Termo)
		}
		if len(got.Entradas) != len(s.Entradas) {
			t.Errorf("%q: criou um termo novo: %d entradas", termo, len(got.Entradas))
		}
		if e := entradaDe(t, got, "ECF"); e.Prioridade != 90 {
			t.Errorf("%q: a prioridade do termo existente mudou: %d", termo, e.Prioridade)
		}
	}
}

func TestAprenderVariante_CriaTermoComPrioridadeCinquenta(t *testing.T) {
	s := sementeDoPainel(t)
	got, ap, err := s.AprenderVariante("Wispr Flow", "Whisperflow")
	if err != nil {
		t.Fatalf("AprenderVariante: %v", err)
	}
	if ap != (Aprendida{Termo: "Wispr Flow", Variante: "Whisperflow"}) {
		t.Errorf("Aprendida = %+v", ap)
	}
	if len(got.Entradas) != len(s.Entradas)+1 {
		t.Fatalf("len(Entradas) = %d, want %d", len(got.Entradas), len(s.Entradas)+1)
	}
	nova := got.Entradas[len(got.Entradas)-1]
	want := EntradaSemente{
		Canonica:   "Wispr Flow",
		Prioridade: 50,
		Alias:      []string{"Whisperflow"},
		Origem:     OrigemPainel,
	}
	if emJSON(t, Semente{Usuario: "x", Entradas: []EntradaSemente{nova}}) != emJSON(t, Semente{Usuario: "x", Entradas: []EntradaSemente{want}}) {
		t.Errorf("entrada nova = %+v, want %+v", nova, want)
	}
	if nova.Dominio != "" || nova.Restrito {
		t.Errorf("a entrada nova tem domínio ou restrição: vale em qualquer app: %+v", nova)
	}
	if got.AliasOrigem["Whisperflow"] != OrigemPainel {
		t.Errorf("alias_origem = %v", got.AliasOrigem)
	}
}

func TestAprenderVariante_NaoMutaAOriginal(t *testing.T) {
	s := sementeDoPainel(t)
	antes := emJSON(t, s)
	aprendida, _, err := s.AprenderVariante("ECF", "SF")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AprenderVariante("Novo Termo", "Nvo"); err != nil {
		t.Fatal(err)
	}
	if got := emJSON(t, s); got != antes {
		t.Errorf("a semente original foi mutada ao aprender:\n%s", got)
	}
	if _, _, err := aprendida.DesfazerVariante("SF"); err != nil {
		t.Fatal(err)
	}
	if got := emJSON(t, aprendida); !strings.Contains(got, `"SF"`) {
		t.Error("desfazer mutou a semente que recebeu")
	}
}

func TestAprenderVariante_Recusas(t *testing.T) {
	s := sementeDoPainel(t)
	casos := []struct {
		nome, termo, variante string
		quer                  error
	}{
		{"variante vazia", "ECF", "", ErrVazio},
		{"termo vazio", "", "SF", ErrVazio},
		{"só pontuação", "ECF", " !!! ", ErrVazio},
		{"igual ao termo", "ECF", "ECF", ErrSemNovidade},
		{"só a caixa", "ECF", "ecf", ErrSemNovidade},
		{"só o acento", "SPED", "spéd", ErrSemNovidade},
		{"só o espaço", "ECF", "E C F", ErrSemNovidade},
		{"já é do mesmo termo", "ECF", "ECFX", ErrJaExiste},
		{"já é do mesmo termo, outra caixa", "ECF", "ecfx", ErrJaExiste},
		{"já é do mesmo termo, na lista restrita", "commit", "comite", ErrJaExiste},
		{"é alias de outro termo", "ECF", "DAAS", ErrVarianteDeOutroTermo},
		{"é alias de outro termo, outra caixa", "ECF", "daas", ErrVarianteDeOutroTermo},
		{"é outro termo", "ECF", "ecd", ErrVarianteDeOutroTermo},
		{"é alias de termo inativo", "ECF", "deslig", ErrVarianteDeOutroTermo},
		{"termo inativo", "Desligado", "xpto", ErrTermoInativo},
		{"caractere de controle", "ECF", "a\x00b", ErrInvalido},
		{"grande demais", "ECF", strings.Repeat("a", 81), ErrInvalido},
	}
	antes := emJSON(t, s)
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, ap, err := s.AprenderVariante(c.termo, c.variante)
			if !errors.Is(err, c.quer) {
				t.Fatalf("erro = %v, quero %v", err, c.quer)
			}
			if ap != (Aprendida{}) {
				t.Errorf("devolveu Aprendida %+v junto com o erro", ap)
			}
			if emJSON(t, got) != antes {
				t.Errorf("a semente devolvida com o erro mudou")
			}
		})
	}
	if emJSON(t, s) != antes {
		t.Error("uma recusa mudou a semente original")
	}
}

// TestAprenderVariante_DizAQuemAVarianteJaPertence: a tela explica
// "SF já vira ECD"; para isso o erro traz o dono.
func TestAprenderVariante_DizAQuemAVarianteJaPertence(t *testing.T) {
	s := sementeDoPainel(t)

	_, _, err := s.AprenderVariante("ECF", "daas")
	var e *ErroVarianteDeOutroTermo
	if !errors.As(err, &e) || e.Dono != "ECD" || e.ETermo {
		t.Errorf("variante de alias: %#v, quero Dono=ECD, ETermo=false", err)
	}

	_, _, err = s.AprenderVariante("ECF", "ecd")
	if !errors.As(err, &e) || e.Dono != "ECD" || !e.ETermo {
		t.Errorf("variante que é termo: %#v, quero Dono=ECD, ETermo=true", err)
	}
	if strings.Contains(err.Error(), "ecd") || strings.Contains(err.Error(), "ECD") {
		t.Errorf("o texto do erro traz o conteúdo do dicionário: %q", err.Error())
	}
}

// TestAprenderVariante_HerdaARestricaoDaEntrada: a variante aprendida entra
// na lista comum, mas a entrada "commit" continua restrita ao domínio dev
// — a pessoa a protegeu assim de propósito.
func TestAprenderVariante_HerdaARestricaoDaEntrada(t *testing.T) {
	got, _, err := sementeDoPainel(t).AprenderVariante("commit", "comiti")
	if err != nil {
		t.Fatal(err)
	}
	e := entradaDe(t, got, "commit")
	if !e.Restrito || e.Dominio != "dev" {
		t.Errorf("a entrada perdeu a restrição: %+v", e)
	}
	if len(e.Alias) != 1 || e.Alias[0] != "comiti" || len(e.AliasRestrito) != 1 || e.AliasRestrito[0] != "comite" {
		t.Errorf("Alias = %v, AliasRestrito = %v", e.Alias, e.AliasRestrito)
	}
}

// TestAprenderVariante_NaoReordenaOQueAPessoaOrdenou: numa lista em ordem
// alfabética a variante entra no seu lugar; numa lista que a pessoa
// arrumou de outro jeito, vai para o fim. Reordenar tudo faria o Desfazer
// não voltar ao arquivo de antes.
func TestAprenderVariante_NaoReordenaOQueAPessoaOrdenou(t *testing.T) {
	base := func(alias ...string) Semente {
		return Semente{Usuario: "m", Entradas: []EntradaSemente{{Canonica: "T", Prioridade: 60, Alias: alias}}}
	}
	got, _, err := base("alfa", "zeta").AprenderVariante("T", "meio")
	if err != nil {
		t.Fatal(err)
	}
	if want := "alfa,meio,zeta"; strings.Join(got.Entradas[0].Alias, ",") != want {
		t.Errorf("lista ordenada: %v, want %s", got.Entradas[0].Alias, want)
	}

	desordenada := base("zeta", "alfa")
	got, _, err = desordenada.AprenderVariante("T", "meio")
	if err != nil {
		t.Fatal(err)
	}
	if want := "zeta,alfa,meio"; strings.Join(got.Entradas[0].Alias, ",") != want {
		t.Errorf("lista desordenada: %v, want %s", got.Entradas[0].Alias, want)
	}
	voltou, _, err := got.DesfazerVariante("meio")
	if err != nil {
		t.Fatal(err)
	}
	if emJSON(t, voltou) != emJSON(t, desordenada) {
		t.Errorf("desfazer não voltou à lista que a pessoa tinha: %v", voltou.Entradas[0].Alias)
	}
}

func TestDesfazerVariante_VoltaAoQueEraAntes(t *testing.T) {
	casos := []struct{ nome, termo, variante string }{
		{"termo que já existia", "ECF", "SF"},
		{"termo criado pelo painel", "Wispr Flow", "Whisperflow"},
		{"termo que existia sem alias", "SPED", "SPEDE"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			s := sementeDoPainel(t)
			aprendida, _, err := s.AprenderVariante(c.termo, c.variante)
			if err != nil {
				t.Fatal(err)
			}
			if emJSON(t, aprendida) == emJSON(t, s) {
				t.Fatal("aprender não mudou nada")
			}
			volta, ap, err := aprendida.DesfazerVariante(c.variante)
			if err != nil {
				t.Fatalf("DesfazerVariante: %v", err)
			}
			if ap.Variante != c.variante {
				t.Errorf("Aprendida = %+v", ap)
			}
			if got := emJSON(t, volta); got != sementeDoPainelJSON {
				t.Errorf("desfazer não voltou ao que era:\n%s", got)
			}
		})
	}
}

// TestDesfazerVariante_SemAliasOrigemNoArquivo: um arquivo sem o mapa
// alias_origem volta sem ele — sem um "alias_origem": {} sobrando.
func TestDesfazerVariante_SemAliasOrigemNoArquivo(t *testing.T) {
	s := Semente{Usuario: "m", Entradas: []EntradaSemente{{Canonica: "ECF", Prioridade: 90}}}
	antes := emJSON(t, s)
	aprendida, _, err := s.AprenderVariante("ECF", "SF")
	if err != nil {
		t.Fatal(err)
	}
	volta, _, err := aprendida.DesfazerVariante("SF")
	if err != nil {
		t.Fatal(err)
	}
	if got := emJSON(t, volta); got != antes || strings.Contains(got, "alias_origem") {
		t.Errorf("sobrou rastro:\n%s", got)
	}
}

func TestDesfazerVariante_SoDesfazOQueOPainelAprendeu(t *testing.T) {
	s := sementeDoPainel(t)
	antes := emJSON(t, s)
	for _, variante := range []string{
		"DAAS",   // alias com origem de dataset
		"comite", // alias cadastrado à mão, sem origem
		"ECFX",   // idem
		"deslig", // idem
		"nunca existiu",
	} {
		got, ap, err := s.DesfazerVariante(variante)
		if !errors.Is(err, ErrNaoAprendida) {
			t.Errorf("Desfazer(%q): erro = %v, quero ErrNaoAprendida", variante, err)
		}
		if ap != (Aprendida{}) || emJSON(t, got) != antes {
			t.Errorf("Desfazer(%q) mudou a semente ou devolveu %+v", variante, ap)
		}
	}
	if _, _, err := s.DesfazerVariante("  "); !errors.Is(err, ErrVazio) {
		t.Errorf("Desfazer(vazio): erro = %v, quero ErrVazio", err)
	}
}

func TestDesfazerVariante_AceitaOutraCaixa(t *testing.T) {
	s, _, err := sementeDoPainel(t).AprenderVariante("ECF", "SF")
	if err != nil {
		t.Fatal(err)
	}
	volta, ap, err := s.DesfazerVariante("sf")
	if err != nil {
		t.Fatalf("Desfazer(sf): %v", err)
	}
	if ap.Variante != "SF" {
		t.Errorf("Variante = %q, quero a que estava no arquivo (SF)", ap.Variante)
	}
	if emJSON(t, volta) != sementeDoPainelJSON {
		t.Error("não voltou ao que era")
	}
}

// TestDesfazerVariante_TermoCriadoFicaEnquantoTemOutraVariante: o painel
// criou o termo com uma variante e depois acrescentou outra. Desfazer a
// primeira não pode apagar o termo: a segunda ainda precisa dele.
func TestDesfazerVariante_TermoCriadoFicaEnquantoTemOutraVariante(t *testing.T) {
	s := sementeDoPainel(t)
	a1, _, err := s.AprenderVariante("Wispr Flow", "Whisperflow")
	if err != nil {
		t.Fatal(err)
	}
	a2, ap2, err := a1.AprenderVariante("wispr flow", "Uispr Flow")
	if err != nil {
		t.Fatal(err)
	}
	if ap2.Termo != "Wispr Flow" || len(a2.Entradas) != len(s.Entradas)+1 {
		t.Fatalf("a segunda variante não caiu no termo criado: %+v, %d entradas", ap2, len(a2.Entradas))
	}

	d1, _, err := a2.DesfazerVariante("Whisperflow")
	if err != nil {
		t.Fatal(err)
	}
	e := entradaDe(t, d1, "Wispr Flow")
	if len(e.Alias) != 1 || e.Alias[0] != "Uispr Flow" {
		t.Errorf("o termo não guardou a variante que sobrou: %+v", e)
	}

	d2, _, err := d1.DesfazerVariante("Uispr Flow")
	if err != nil {
		t.Fatal(err)
	}
	if emJSON(t, d2) != sementeDoPainelJSON {
		t.Errorf("desfazer as duas não voltou ao que era:\n%s", emJSON(t, d2))
	}
}

// TestDesfazerVariante_NaoApagaOQueAPessoaMexeu: se depois de aprender a
// pessoa mudou a prioridade do termo criado à mão, desfazer tira a variante
// e deixa o termo. Apagá-lo jogaria fora uma edição dela.
func TestDesfazerVariante_NaoApagaOQueAPessoaMexeu(t *testing.T) {
	mexidas := map[string]func(*EntradaSemente){
		"prioridade": func(e *EntradaSemente) { e.Prioridade = 80 },
		"domínio":    func(e *EntradaSemente) { e.Dominio = "dev" },
		"idioma":     func(e *EntradaSemente) { e.Idioma = "en" },
		"restrição":  func(e *EntradaSemente) { e.Restrito = true },
		"desligado":  func(e *EntradaSemente) { e.Inativa = true },
		"outro alias à mão": func(e *EntradaSemente) {
			e.Alias = append(e.Alias, "zzz")
		},
	}
	for nome, mexer := range mexidas {
		t.Run(nome, func(t *testing.T) {
			s, _, err := sementeDoPainel(t).AprenderVariante("Wispr Flow", "Whisperflow")
			if err != nil {
				t.Fatal(err)
			}
			i := len(s.Entradas) - 1
			mexer(&s.Entradas[i])

			volta, _, err := s.DesfazerVariante("Whisperflow")
			if err != nil {
				t.Fatal(err)
			}
			if e := entradaDe(t, volta, "Wispr Flow"); strings.Contains(strings.Join(e.Alias, ","), "Whisperflow") {
				t.Errorf("a variante não saiu: %+v", e)
			}
		})
	}
}

func TestVariantesDoPainel(t *testing.T) {
	s := sementeDoPainel(t)
	if got := s.VariantesDoPainel(); got == nil || len(got) != 0 {
		t.Fatalf("sem nada aprendido: %#v, quero slice vazio e não nil", got)
	}

	var err error
	for _, c := range [][2]string{{"ECF", "SF"}, {"Wispr Flow", "Whisperflow"}, {"ECF", "EFC"}} {
		if s, _, err = s.AprenderVariante(c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	// Uma origem órfã (alias que não está em entrada nenhuma) não conta.
	s.AliasOrigem["fantasma"] = OrigemPainel

	got := s.VariantesDoPainel()
	// Na ordem do arquivo: as entradas e, dentro delas, a lista de alias
	// (ECFX é manual e não entra).
	want := []Aprendida{
		{Termo: "ECF", Variante: "EFC"},
		{Termo: "ECF", Variante: "SF"},
		{Termo: "Wispr Flow", Variante: "Whisperflow"},
	}
	if len(got) != len(want) {
		t.Fatalf("VariantesDoPainel = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
