package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Os termos que o PAINEL cria ao aprender uma variante (Origem "painel")
// agem só pela variante. Sem isso, aprender "ista" -> "esta" criava também
// uma regra de acento sobre todo texto futuro: o match exato tolera acento,
// e "a casa está pronta" virava "a casa esta pronta". A pessoa confirmou a
// variante, não o termo (decisão 3 de 30-set-2026: acento sozinho não se
// aprende).

// Dois dicionários, porque "esta" e "está" são o MESMO termo para o
// dicionário (a forma normalizada é a mesma): o painel nunca cria os dois.
// Num o termo nasce sem acento; no outro, com.
const (
	dicionarioDoPainelSemAcentoJSON = `{
  "usuario": "mauro",
  "alias_origem": {"ista": "painel", "eh": "painel", "dctf vebe": "painel", "sf": "painel"},
  "entradas": [
    {"canonica": "esta", "prioridade": 50, "alias": ["ista"], "origem": "painel"},
    {"canonica": "e", "prioridade": 50, "alias": ["eh"], "origem": "painel"},
    {"canonica": "DCTFWeb", "prioridade": 50, "alias": ["dctf vebe"], "origem": "painel"},
    {"canonica": "ECF", "prioridade": 90, "alias": ["sf"]}
  ]
}`
	dicionarioDoPainelComAcentoJSON = `{
  "usuario": "mauro",
  "alias_origem": {"istá": "painel", "eeh": "painel", "dctf vebe": "painel", "sf": "painel"},
  "entradas": [
    {"canonica": "está", "prioridade": 50, "alias": ["istá"], "origem": "painel"},
    {"canonica": "é", "prioridade": 50, "alias": ["eeh"], "origem": "painel"},
    {"canonica": "DCTFWeb", "prioridade": 50, "alias": ["dctf vebe"], "origem": "painel"},
    {"canonica": "ECF", "prioridade": 90, "alias": ["sf"]}
  ]
}`
)

// repoDoPainel monta o repositório como o Agent o monta: Popular sobre a
// semente.
func repoDoPainel(t *testing.T, json string) (*dictionary.InMemoryDictionaryRepository, string) {
	t.Helper()
	s, err := dictionary.CarregarSemente(strings.NewReader(json))
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	return repo, s.Usuario
}

func resolverTexto(t *testing.T, sr *SmartResolver, user, texto string) string {
	t.Helper()
	got, err := sr.Resolve(context.Background(), ResolveRequest{
		UserID:      user,
		ASRResult:   asr.ASRResult{Text: texto},
		CloudPolicy: CloudDisabled,
	})
	if err != nil {
		t.Fatalf("Resolve(%q): %v", texto, err)
	}
	return got.FinalText
}

// TestSmart_TermoDoPainelNaoReescreveAcento é o caso de revisão: criar o
// termo não pode criar a regra de acento. Cada par abaixo é um texto CERTO
// que o termo do painel não pode tocar.
func TestSmart_TermoDoPainelNaoReescreveAcento(t *testing.T) {
	for nome, json := range map[string]string{
		"termo sem acento": dicionarioDoPainelSemAcentoJSON,
		"termo com acento": dicionarioDoPainelComAcentoJSON,
	} {
		t.Run(nome, func(t *testing.T) {
			repo, user := repoDoPainel(t, json)
			sr := NewSmartResolver(repo)
			for _, texto := range []string{
				"a casa está pronta",
				"a casa esta pronta",
				"esta casa é nova",
				"esta casa e nova",
				"é assim e pronto",
				"o sistema está e é novo",
				"a dctf web está pronta", // espaçamento: "dctf web" casa exato com DCTFWeb
			} {
				if got := resolverTexto(t, sr, user, texto); got != texto {
					t.Errorf("o termo do painel reescreveu texto certo: entrou %q, saiu %q", texto, got)
				}
			}
		})
	}
}

// TestSmart_TermoDoPainelNaoCorrigeCaixaTampouco: a caixa já não era
// reescrita (diferePorMaisQueCaixa); confere que continua assim.
func TestSmart_TermoDoPainelNaoCorrigeCaixaTampouco(t *testing.T) {
	repo, user := repoDoPainel(t, dicionarioDoPainelSemAcentoJSON)
	if got := resolverTexto(t, NewSmartResolver(repo), user, "a Esta casa"); got != "a Esta casa" {
		t.Errorf("saiu %q", got)
	}
}

// TestSmart_VarianteDoPainelAgeComoSempre: o que a pessoa confirmou
// continua valendo, e o termo novo é o que sai.
func TestSmart_VarianteDoPainelAgeComoSempre(t *testing.T) {
	repo, user := repoDoPainel(t, dicionarioDoPainelSemAcentoJSON)
	sr := NewSmartResolver(repo)

	for entrada, quer := range map[string]string{
		"a casa ista pronta":            "a casa esta pronta",
		"vou transmitir dctf vebe hoje": "vou transmitir DCTFWeb hoje",
		"a SF esta certa":               "a ECF esta certa", // o termo da pessoa, sem origem, segue como sempre
	} {
		if got := resolverTexto(t, sr, user, entrada); got != quer {
			t.Errorf("%q -> %q, quero %q", entrada, got, quer)
		}
	}

	repo, user = repoDoPainel(t, dicionarioDoPainelComAcentoJSON)
	if got, quer := resolverTexto(t, NewSmartResolver(repo), user, "a casa istá pronta"), "a casa está pronta"; got != quer {
		t.Errorf("termo com acento: saiu %q, quero %q", got, quer)
	}
}

// TestSmart_TermoDoPainelNaoCasaPorFonetica: a entrada do painel não gera
// casamento fonético: a pessoa só confirmou a variante exata. Uma entrada
// igual, sem a origem, gera (o controle do teste).
func TestSmart_TermoDoPainelNaoCasaPorFonetica(t *testing.T) {
	ctx := context.Background()
	busca := func(json, termo string) []dictionary.DictionaryCandidate {
		repo, user := repoDoPainel(t, json)
		cs, err := repo.Search(ctx, dictionary.DictionaryQuery{UserID: user, Term: termo})
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	const (
		doPainel = `{"usuario":"m","entradas":[{"canonica":"Comercial","prioridade":50,"alias":["comerciau"],"origem":"painel"}]}`
		daPessoa = `{"usuario":"m","entradas":[{"canonica":"Comercial","prioridade":50,"alias":["comerciau"]}]}`
	)
	if cs := busca(daPessoa, "comersial"); len(cs) != 1 || cs[0].Reason != dictionary.MatchReasonPhonetic {
		t.Fatalf("controle: a entrada da pessoa devia casar por fonética: %+v", cs)
	}
	if cs := busca(doPainel, "comersial"); len(cs) != 0 {
		t.Errorf("a entrada do painel casou por fonética: %+v", cs)
	}
}

// Que o termo do painel não entra na lista de termos do Polish só por estar
// escrito, e entra quando a variante age, está em painel_aliasonly_revisao_test.go.

// TestMaxPalavrasVarianteAcompanhaOSmart: o dicionário recusa variante com
// mais palavras do que o Smart procura, e os dois limites têm de ser o mesmo
// número. Se o Smart passar a procurar frases maiores (ou menores), este é o
// teste que lembra de mudar o outro.
func TestMaxPalavrasVarianteAcompanhaOSmart(t *testing.T) {
	if dictionary.MaxPalavrasVariante != maxPhraseWords {
		t.Errorf("dictionary.MaxPalavrasVariante = %d, e o Smart procura frases de até %d palavras",
			dictionary.MaxPalavrasVariante, maxPhraseWords)
	}
}

// TestSmart_VarianteNoLimiteDePalavrasAge: uma variante de exatamente
// MaxPalavrasVariante palavras é achada pelo Smart. Sem isto, o limite do
// dicionário podia estar certo no número e errado na conta.
func TestSmart_VarianteNoLimiteDePalavrasAge(t *testing.T) {
	repo, user := repoDoPainel(t, `{"usuario":"m","alias_origem":{"um dois tres quatro":"painel"},"entradas":[`+
		`{"canonica":"Zeta","prioridade":50,"alias":["um dois tres quatro"],"origem":"painel"}]}`)
	if got := resolverTexto(t, NewSmartResolver(repo), user, "veja um dois tres quatro agora"); got != "veja Zeta agora" {
		t.Errorf("saiu %q", got)
	}
}
