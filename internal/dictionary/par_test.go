package dictionary

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Os testes do aprendido como a nuvem o vê (F8a): o id, a chave, as
// pré-checagens que a nuvem também usa, e a projeção do arquivo.

// TestUUIDv5_VetorDaRFC confere a implementação contra o exemplo da RFC 9562
// (apêndice A.4): o NameSpace_DNS com "www.example.com".
func TestUUIDv5_VetorDaRFC(t *testing.T) {
	dns := [16]byte{0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	if got, want := uuidV5(dns, "www.example.com"), "2ed6657d-e927-568b-95e1-2665a8aea6a2"; got != want {
		t.Fatalf("uuidV5 = %s, want %s", got, want)
	}
}

// TestIDDoPar_VetoresCongelados congela o id de algumas variantes. Os valores
// foram calculados FORA do Go (Python, uuid.uuid5(uuid.NAMESPACE_URL,
// "aurora-whisper:aprendido:v1:" + chave), 07-out-2026).
//
// Se este teste quebrar, não o conserte trocando os valores: o id é a
// identidade do par na nuvem (a PK da learned_pairs), e um id novo para a
// mesma variante deixa duas linhas lá. Mudar Normalize, CollapseSpaces, o
// namespace ou o nome é uma migração dos dados, com um nome novo ("v2").
func TestIDDoPar_VetoresCongelados(t *testing.T) {
	casos := []struct {
		id        string
		variantes []string
	}{
		// caixa e pontuação
		{"7a9889df-ca14-52d4-ac18-12506f5e8d70", []string{"sf", "SF", "Sf", "S.F.", "s f", "  S   F  "}},
		// acento
		{"c1327365-7477-5f73-b126-7d1739cf39e6", []string{"fisico", "Físico", "FÍSICO", "físico"}},
		{"894054d0-9006-56b5-8786-bbef5c3bfeef", []string{"acao", "ação", "AÇÃO", "Ação"}},
		{"c97f8917-364d-547e-950c-3e5e496b8c17", []string{"comite", "comitê", "Comitê", "COMITÉ"}},
		// espaço: "DCTF Web" é "DCTFWeb", como no Search
		{"92b12122-12f4-54fe-86e8-79a047a9678b", []string{"dctfweb", "DCTFWeb", "DCTF Web", "dctf-web", "DCTF  WEB"}},
		// número
		{"e187aaa3-d036-5c51-96cd-abc6bcd7dbfc", []string{"n8n", "N8N", "n 8 n"}},
		{"bd16e060-c9c8-512b-8cba-f0151d8a9ae9", []string{"ecf", "ECF"}},
		// o ß não está na tabela de acentos: fica como é (só minúscula)
		{"0a93b78b-bf65-5665-8510-54b1636895b8", []string{"straße", "STRAßE", "Stra ße"}},
	}
	for _, c := range casos {
		for _, v := range c.variantes {
			if got := IDDoPar(v); got != c.id {
				t.Errorf("IDDoPar(%q) = %s, want %s (o id é a identidade na nuvem: ver o comentário do teste)", v, got, c.id)
			}
		}
	}
	if IDDoPar("sf") == IDDoPar("sfe") || IDDoPar("ECF") == IDDoPar("ECD") {
		t.Error("variantes diferentes com o mesmo id")
	}
}

// TestIDDoPar_OIdVemDaChavePeloNome é a premissa da conferência da chave do
// pgstore (sqlLearnedKeyTaken): o id é o v5 do nome mais a chave. Uma regra de
// chave mudada sem trocar o nome (aqui, sem o CollapseSpaces) dá outra chave
// E outro id, que a guarda não vê; só outro nome ("v2") dá outro id para a
// MESMA chave, e é isso que a guarda recusa.
func TestIDDoPar_OIdVemDaChavePeloNome(t *testing.T) {
	const v = "DCTF Web"
	chave := ChaveDoPar(v)
	if IDDoPar(v) != uuidV5(namespaceDoPar, nomeDoParV1+chave) {
		t.Fatal("o id não é o v5 do nome mais a chave")
	}
	semCollapse := Normalize(v) // a "regra de chave mudada": "dctf web"
	if semCollapse == chave {
		t.Fatalf("a regra mudada dá a mesma chave %q: o teste não prova nada", chave)
	}
	if uuidV5(namespaceDoPar, nomeDoParV1+semCollapse) == IDDoPar(v) {
		t.Error("a chave mudada com o mesmo nome deu o mesmo id")
	}
	if uuidV5(namespaceDoPar, "aurora-whisper:aprendido:v2:"+chave) == IDDoPar(v) {
		t.Error("outro nome deu o mesmo id para a mesma chave")
	}
}

// TestIDDoPar_EhUUIDv5Canonico: minúsculas, 8-4-4-4-12, versão 5 e variante
// da RFC. É o formato que o servidor compara com o entity_id (CanonicalUUID).
func TestIDDoPar_EhUUIDv5Canonico(t *testing.T) {
	formato := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for _, v := range []string{"sf", "Físico", "DCTF Web", "x", strings.Repeat("ã", 80), "!!!"} {
		if id := IDDoPar(v); !formato.MatchString(id) {
			t.Errorf("IDDoPar(%q) = %q, não é um UUID v5 canônico", v, id)
		}
	}
}

// TestIDDoPar_NaoDependeDoUsuario: o mesmo par tem o mesmo id em dois
// dicionários de donos diferentes (o idDeterministico de seed.go depende).
func TestIDDoPar_NaoDependeDoUsuario(t *testing.T) {
	um := Semente{Usuario: "mauro", AliasOrigem: map[string]string{"sf": OrigemPainel},
		Entradas: []EntradaSemente{{Canonica: "ECF", Prioridade: 90, Alias: []string{"sf"}}}}
	outro := um
	outro.Usuario = "outra pessoa"
	a, b := um.ParesDoPainel(), outro.ParesDoPainel()
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID || a[0].ID != IDDoPar("SF") {
		t.Fatalf("ids diferentes por usuário: %+v e %+v", a, b)
	}
}

// TestChaveDoPar_CabeNaColuna: a chave nunca tem mais runas que a variante, e
// não é vazia quando a variante normaliza para algo. Assim a variante que passa
// em ConferirPar tem o normalized_variant dentro do CHECK da 0016 (1 a 80).
func TestChaveDoPar_CabeNaColuna(t *testing.T) {
	variantes := []string{
		strings.Repeat("a", 80),
		strings.Repeat("ã", 80),
		strings.Repeat("İ", 80), // maiúscula cuja minúscula tem outro tamanho em bytes
		strings.Repeat("ß", 80),
		strings.Repeat("A.", 40),
		strings.Repeat("-", 79) + "x",
		"DCTF Web",
	}
	for _, v := range variantes {
		if ConferirPar(v, "termo", false, nil) != nil {
			t.Fatalf("ConferirPar recusou %q", v)
		}
		k := ChaveDoPar(v)
		if n := utf8.RuneCountInString(k); n < 1 || n > utf8.RuneCountInString(v) || n > MaxRunasPar {
			t.Errorf("ChaveDoPar(%q) tem %d runas", v, n)
		}
	}
	if ChaveDoPar("DCTF Web") != "dctfweb" || ChaveDoPar("Físico!") != "fisico" {
		t.Error("a chave não é CollapseSpaces(Normalize(v))")
	}
}

func TestConferirPar(t *testing.T) {
	dom := func(s string) *string { return &s }
	r80, r81 := strings.Repeat("ã", 80), strings.Repeat("ã", 81) // 160 e 162 bytes: conta runas
	casos := []struct {
		nome            string
		variante, termo string
		restrito        bool
		dominio         *string
		want            error
	}{
		{"o par comum", "sf", "ECF", false, nil, nil},
		{"com domínio", "sf", "ECF", false, dom("contabil"), nil},
		{"restrito com domínio", "comite", "commit", true, dom("dev"), nil},
		{"80 runas em tudo", r80, r80, true, dom(r80), nil},
		{"uma runa em tudo", "x", "y", true, dom("d"), nil},

		{"nulo na variante", "s\x00f", "ECF", false, nil, ErrParTexto},
		{"nulo no termo", "sf", "E\x00CF", false, nil, ErrParTexto},
		{"nulo no domínio", "sf", "ECF", false, dom("con\x00tabil"), ErrParTexto},
		{"UTF-8 inválido na variante", "s\xfff", "ECF", false, nil, ErrParTexto},
		{"UTF-8 inválido no termo", "sf", "EC\xc3", false, nil, ErrParTexto},
		{"UTF-8 inválido no domínio", "sf", "ECF", false, dom("\xe2\x82"), ErrParTexto},
		{"o nulo vem antes do tamanho", r81 + "\x00", "ECF", false, nil, ErrParTexto},

		{"variante vazia", "", "ECF", false, nil, ErrParTamanho},
		{"variante de 81 runas", r81, "ECF", false, nil, ErrParTamanho},
		{"termo vazio", "sf", "", false, nil, ErrParTamanho},
		{"termo de 81 runas", "sf", r81, false, nil, ErrParTamanho},
		{"domínio vazio, e não nulo", "sf", "ECF", false, dom(""), ErrParTamanho},
		{"domínio de 81 runas", "sf", "ECF", true, dom(r81), ErrParTamanho},

		{"variante só de pontuação", "!!!", "ECF", false, nil, ErrParVarianteVazia},
		{"variante só de espaço", "   ", "ECF", false, nil, ErrParVarianteVazia},

		{"restrito sem domínio", "comite", "commit", true, nil, ErrParRestritoSemDominio},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			err := ConferirPar(c.variante, c.termo, c.restrito, c.dominio)
			if !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
				t.Fatalf("ConferirPar = %v, want %v", err, c.want)
			}
			if err != nil && (strings.Contains(err.Error(), c.variante) && c.variante != "" ||
				strings.Contains(err.Error(), "ECF")) {
				t.Errorf("o erro cita o texto: %q", err)
			}
		})
	}
}

// TestPacote_SoImportaABibliotecaPadrao: a nuvem (internal/server/cloudstore)
// importa este pacote para o id e as pré-checagens; ele não pode importar nada
// do módulo, ou um dia o servidor ganharia um ciclo, ou o Agent inteiro.
func TestPacote_SoImportaABibliotecaPadrao(t *testing.T) {
	arquivos, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	vistos := 0
	for _, nome := range arquivos {
		if strings.HasSuffix(nome, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, nome, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		vistos++
		for _, imp := range f.Imports {
			caminho, _ := strconv.Unquote(imp.Path.Value)
			if primeiro, _, _ := strings.Cut(caminho, "/"); strings.Contains(primeiro, ".") {
				t.Errorf("%s importa %s: este pacote só pode importar a biblioteca padrão", nome, caminho)
			}
		}
	}
	if vistos == 0 {
		t.Fatal("nenhum arquivo do pacote lido")
	}
}

// --- A projeção (Semente.ParesDoPainel) ---

// semente monta um dicionário a partir das entradas, com as origens dadas.
func semente(origens map[string]string, entradas ...EntradaSemente) Semente {
	return Semente{
		Usuario:     "mauro",
		Dominios:    map[string][]string{"contabil": {"SistemaContabil.exe"}, "dev": {"Code.exe"}},
		AliasOrigem: origens,
		Entradas:    entradas,
	}
}

func painel(vs ...string) map[string]string {
	m := map[string]string{}
	for _, v := range vs {
		m[v] = OrigemPainel
	}
	return m
}

// TestParesDoPainel_TabelaDaProjecao é a tabela "No PC, a projeção" da
// ADR-019, linha a linha.
func TestParesDoPainel_TabelaDaProjecao(t *testing.T) {
	r81 := strings.Repeat("a", 81)
	casos := []struct {
		nome string
		s    Semente
		want []ParDoPainel // sem o ID: o teste o preenche
	}{
		{
			"em alias, numa entrada sem dominio: sobe, sem restrição e sem domínio",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"sf"}}),
			[]ParDoPainel{{Variante: "sf", Termo: "ECF"}},
		},
		{
			"em alias, numa entrada com dominio, sem restrito: sobe com o nome do domínio",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Dominio: "contabil", Alias: []string{"sf"}}),
			[]ParDoPainel{{Variante: "sf", Termo: "ECF", Dominio: "contabil"}},
		},
		{
			"em alias, numa entrada com dominio e restrito: sobe restrita",
			semente(painel("cmt"), EntradaSemente{Canonica: "commit", Prioridade: 75, Dominio: "dev", Restrito: true, Alias: []string{"cmt"}}),
			[]ParDoPainel{{Variante: "cmt", Termo: "commit", Restrito: true, Dominio: "dev"}},
		},
		{
			"em alias_restrito, numa entrada com dominio: sobe restrita",
			semente(painel("efe"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Dominio: "contabil", AliasRestrito: []string{"efe"}}),
			[]ParDoPainel{{Variante: "efe", Termo: "ECF", Restrito: true, Dominio: "contabil", EmAliasRestrito: true}},
		},
		{
			"em alias_restrito, numa entrada restrita com dominio: sobe restrita",
			semente(painel("comite"), EntradaSemente{Canonica: "commit", Prioridade: 75, Dominio: "dev", Restrito: true, AliasRestrito: []string{"comite"}}),
			[]ParDoPainel{{Variante: "comite", Termo: "commit", Restrito: true, Dominio: "dev", EmAliasRestrito: true}},
		},
		{
			"em alias, numa entrada restrito sem dominio: não sobe (nunca age)",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Restrito: true, Alias: []string{"sf"}}),
			nil,
		},
		{
			"em alias_restrito, numa entrada sem dominio: não sobe (nunca age)",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, AliasRestrito: []string{"sf"}}),
			nil,
		},
		{
			"domínio só de espaços é sem domínio",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Dominio: "  ", Alias: []string{"sf"}}),
			[]ParDoPainel{{Variante: "sf", Termo: "ECF"}},
		},
		{
			"domínio só de espaços, restrito: não sobe",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Dominio: " ", Restrito: true, Alias: []string{"sf"}}),
			nil,
		},
		{
			"a mesma variante, com a origem, em duas entradas: nenhuma das duas",
			semente(painel("sf", "ok"),
				EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"ok", "sf"}},
				EntradaSemente{Canonica: "ECD", Prioridade: 90, Alias: []string{"sf"}}),
			[]ParDoPainel{{Variante: "ok", Termo: "ECF"}},
		},
		{
			"a mesma variante escrita de outro jeito (o mesmo id), em duas entradas: nenhuma",
			semente(painel("sf", "S.F."),
				EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"sf"}},
				EntradaSemente{Canonica: "ECD", Prioridade: 90, Alias: []string{"S.F."}}),
			nil,
		},
		{
			"a mesma variante em alias e alias_restrito da mesma entrada: nenhuma",
			semente(painel("sf", "S F"),
				EntradaSemente{Canonica: "ECF", Prioridade: 90, Dominio: "contabil", Alias: []string{"sf"}, AliasRestrito: []string{"S F"}}),
			nil,
		},
		{
			"a nuvem recusaria: Normalize da variante vazio (posta à mão com a origem)",
			semente(painel("!!!"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"!!!"}}),
			nil,
		},
		{
			"a nuvem recusaria: variante de 81 runas",
			semente(painel(r81), EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{r81}}),
			nil,
		},
		{
			"a nuvem recusaria: termo de 81 runas",
			semente(painel("sf"), EntradaSemente{Canonica: r81, Prioridade: 90, Alias: []string{"sf"}}),
			nil,
		},
		{
			"a nuvem recusaria: domínio de 81 runas",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Dominio: r81, Alias: []string{"sf"}}),
			nil,
		},
		{
			"a nuvem recusaria: o caractere nulo na variante",
			semente(painel("s\x00f"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"s\x00f"}}),
			nil,
		},
		{
			"a nuvem recusaria: o caractere nulo no termo",
			semente(painel("sf"), EntradaSemente{Canonica: "E\x00CF", Prioridade: 90, Alias: []string{"sf"}}),
			nil,
		},
		{
			"a nuvem recusaria: o caractere nulo no domínio",
			semente(painel("sf"), EntradaSemente{Canonica: "ECF", Prioridade: 90, Dominio: "con\x00tabil", Alias: []string{"sf"}}),
			nil,
		},
		{
			"o termo do painel adotado pelo Adicionar termo: as variantes do painel sobem, as grafias dele não (D1)",
			semente(map[string]string{"sf": OrigemPainel, "efe": OrigemAdicionado},
				EntradaSemente{Canonica: "ECF", Prioridade: 100, Origem: OrigemAdicionado, Alias: []string{"efe", "sf"}}),
			[]ParDoPainel{{Variante: "sf", Termo: "ECF"}},
		},
		{
			"sem a origem painel: à mão, dictlearn e adicionado não sobem (D1)",
			semente(map[string]string{"DAAS": "cont-04", "rague": OrigemAdicionado},
				EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"efe"}},
				EntradaSemente{Canonica: "ECD", Prioridade: 90, Alias: []string{"DAAS"}},
				EntradaSemente{Canonica: "RAG", Prioridade: 100, Origem: OrigemAdicionado, Alias: []string{"rague"}}),
			nil,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			antes := emJSON(t, c.s)
			got := c.s.ParesDoPainel()
			if got == nil {
				t.Fatal("ParesDoPainel devolveu nil")
			}
			want := []ParDoPainel{}
			for _, w := range c.want {
				w.ID = IDDoPar(w.Variante)
				want = append(want, w)
			}
			if len(got) != len(want) {
				t.Fatalf("ParesDoPainel = %+v, want %+v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("par %d = %+v, want %+v", i, got[i], want[i])
				}
			}
			if depois := emJSON(t, c.s); depois != antes {
				t.Error("a projeção mudou a semente")
			}
		})
	}
}

// TestParesDoPainel_OrdemEAsDuasListas: na ordem do arquivo, cada entrada com
// alias e depois alias_restrito; a variante como está escrita, o termo como
// está escrito, e o que a nuvem guarda (ParaNuvem) sem a forma local.
func TestParesDoPainel_OrdemEAsDuasListas(t *testing.T) {
	s := semente(painel("Efe", "S F", "comite", "dctf web"),
		EntradaSemente{Canonica: "DCTFWeb", Prioridade: 80, Alias: []string{"dctf web"}},
		EntradaSemente{Canonica: "ecf", Prioridade: 90, Dominio: "contabil", Alias: []string{"ECFX", "S F"}, AliasRestrito: []string{"Efe"}},
		EntradaSemente{Canonica: "commit", Prioridade: 75, Dominio: "dev", Restrito: true, AliasRestrito: []string{"comite"}},
	)
	got := s.ParesDoPainel()
	var vs []string
	for _, p := range got {
		vs = append(vs, p.Variante+"→"+p.Termo)
		if err := ConferirPar(p.Variante, p.Termo, p.Restrito, dominioDoPar(p.Dominio)); err != nil {
			t.Errorf("a projeção tem um par que a nuvem recusa: %+v (%v)", p, err)
		}
	}
	if want := "dctf web→DCTFWeb,S F→ecf,Efe→ecf,comite→commit"; strings.Join(vs, ",") != want {
		t.Fatalf("ordem = %s, want %s", strings.Join(vs, ","), want)
	}
	if n := got[2].ParaNuvem(); n != (ParDaNuvem{Variante: "Efe", Termo: "ecf", Restrito: true, Dominio: "contabil"}) {
		t.Errorf("ParaNuvem = %+v", n)
	}
}

// TestParesDoPainel_ODicionarioVersionadoNaoSobeNada: o dicionário do
// repositório não tem nada do painel (D1: o dictlearn e a mão não sobem).
func TestParesDoPainel_ODicionarioVersionadoNaoSobeNada(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "asr-dataset", "dicionario.json"))
	if err != nil {
		t.Skipf("o dicionário versionado não está aqui: %v", err)
	}
	s, err := CarregarSemente(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.ParesDoPainel(); len(got) != 0 {
		t.Fatalf("o dicionário versionado sobe %d pares", len(got))
	}
}

// TestVarianteNoArquivo: o que a varredura do sync (F8f3) pergunta sobre uma
// variante que não está na projeção. Fora dela, com a origem em algum lugar,
// o par está no arquivo e não sobe (repetido, numa entrada restrita sem
// domínio); sem a origem, no mesmo termo (como o Search compara), ele foi
// adotado; em outro termo, ou fora do arquivo, nenhum dos dois.
func TestVarianteNoArquivo(t *testing.T) {
	casos := []struct {
		nome            string
		s               Semente
		painel, noTermo bool
	}{
		{"repetida com a origem", semente(painel("sf"),
			EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"sf"}},
			EntradaSemente{Canonica: "ECD", Prioridade: 90, Alias: []string{"sf"}}), true, false},
		{"numa entrada restrita sem domínio", semente(painel("S.F."),
			EntradaSemente{Canonica: "ECF", Prioridade: 90, Restrito: true, Alias: []string{"S.F."}}), true, false},
		{"adotada: sem a origem, no termo", semente(nil,
			EntradaSemente{Canonica: "ECF", Prioridade: 90, Alias: []string{"SF"}}), false, true},
		{"adotada em alias_restrito, o termo com outra caixa", semente(map[string]string{"sf": "cont-04"},
			EntradaSemente{Canonica: "e.c.f", Prioridade: 90, Dominio: "contabil", AliasRestrito: []string{"sf"}}), false, true},
		{"sem a origem, em outro termo", semente(nil,
			EntradaSemente{Canonica: "ECD", Prioridade: 90, Alias: []string{"sf"}}), false, false},
		{"fora do arquivo", semente(nil, EntradaSemente{Canonica: "ECF", Prioridade: 90}), false, false},
	}
	for _, c := range casos {
		p, m := c.s.VarianteNoArquivo("sf", "ECF")
		if p != c.painel || m != c.noTermo {
			t.Errorf("%s: doPainel %v, aMaoNoTermo %v; want %v, %v", c.nome, p, m, c.painel, c.noTermo)
		}
	}
	if p, m := semente(painel("."), EntradaSemente{Canonica: "ECF", Alias: []string{"."}}).VarianteNoArquivo(".", "ECF"); p || m {
		t.Error("a variante sem letra nem dígito foi achada")
	}
}
