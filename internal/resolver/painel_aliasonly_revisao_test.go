package resolver

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// Segunda revisão da etapa 3 (30-set-2026). O termo que o painel cria
// (AliasOnly) tem de agir SÓ pela variante: nem reescrever o que é dele, nem
// desligar uma regra de outro termo, nem travar o Polish em ditado que não o
// usou. Em vez de caçar caso a caso, o teste central prova a invariante: o
// resultado do Smart, e o do Polish, é o mesmo com e sem o termo do painel,
// sobre o dataset inteiro.

const (
	arquivoDicionarioVersionado = "../../testdata/asr-dataset/dicionario.json"
	arquivoFrasesPolish         = "../../testdata/polish-dataset/frases.json"
	arquivoManifestASR          = "../../testdata/asr-dataset/manifest-normalizado.json"
)

func sementeVersionada(t *testing.T) dictionary.Semente {
	t.Helper()
	f, err := os.Open(arquivoDicionarioVersionado)
	if err != nil {
		t.Fatalf("abrindo o dicionário versionado: %v", err)
	}
	defer f.Close()
	s, err := dictionary.CarregarSemente(f)
	if err != nil {
		t.Fatalf("CarregarSemente: %v", err)
	}
	return s
}

func smartDaSementeParaTeste(t *testing.T, s dictionary.Semente) (*SmartResolver, string) {
	t.Helper()
	repo := dictionary.NewInMemoryDictionaryRepository(nil)
	if err := s.Popular(context.Background(), repo); err != nil {
		t.Fatalf("Popular: %v", err)
	}
	return NewSmartResolver(repo), s.Usuario
}

// comTermoDoPainel devolve a semente com o termo criado pelo painel, como o
// Agent o grava: AprenderVariante.
func comTermoDoPainel(t *testing.T, s dictionary.Semente, termo, variante string) dictionary.Semente {
	t.Helper()
	novo, _, err := s.AprenderVariante(termo, variante)
	if err != nil {
		t.Fatalf("AprenderVariante(%q, %q): %v", termo, variante, err)
	}
	return novo
}

// textosDoDataset junta as frases do Polish, as referências do manifest do ASR
// e algumas frases com erro típico de ASR que o dicionário versionado corrige.
func textosDoDataset(t *testing.T) (entradaEEsperado [][2]string, todos []string) {
	t.Helper()
	b, err := os.ReadFile(arquivoFrasesPolish)
	if err != nil {
		t.Fatal(err)
	}
	var frases struct {
		Frases []struct {
			ID       string `json:"id"`
			Entrada  string `json:"entrada"`
			Esperado string `json:"esperado"`
		} `json:"frases"`
	}
	if err := json.Unmarshal(b, &frases); err != nil {
		t.Fatal(err)
	}
	for _, f := range frases.Frases {
		entradaEEsperado = append(entradaEEsperado, [2]string{f.Entrada, f.Esperado})
		todos = append(todos, f.Entrada)
	}

	b, err = os.ReadFile(arquivoManifestASR)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Samples []struct {
			Reference string `json:"reference"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, s := range manifest.Samples {
		todos = append(todos, s.Reference)
	}

	todos = append(todos,
		"o Cloud Code travou de novo",
		"falei com o Tiago Vasconcelos hoje",
		"a Rosângela Bittencurt ligou",
		"o e social foi transmitido",
		"vou transmitir a dctf web hoje",
		"preciso da descrição do produto e do que ele faz",
		"o request flow da web nacional está esta semana",
		"o code review social da esta equipe",
		"a transcrição do áudio ficou pronta",
	)
	return entradaEEsperado, todos
}

// palavrasComunsDoPainel são termos que uma pessoa aprenderia por um clique e
// que aparecem em quase todo ditado. O nome da variante nunca aparece nos
// textos.
var palavrasComunsDoPainel = []string{
	"esta", "e", "descrição", "de", "o", "que", "a", "web", "code", "social",
	"nacional", "request", "flow", "Cloud", "Code", "Vasconcelos", "ponto", "vírgula",
}

// TestSmart_TermoDoPainelNaoMudaNadaSemAVariante é a invariante: sobre o
// dataset inteiro, o Smart devolve o MESMO texto com e sem o termo do painel,
// enquanto a variante não aparece. Antes desta correção o termo ainda desligava
// regras de outros termos ("o Cloud Code travou", "Tiago Vasconcelos") porque o
// match exato dele contava como o mais forte em melhorFrase.
func TestSmart_TermoDoPainelNaoMudaNadaSemAVariante(t *testing.T) {
	_, textos := textosDoDataset(t)
	base := sementeVersionada(t)
	controle, user := smartDaSementeParaTeste(t, base)

	quer := make([]string, len(textos))
	mudou := 0
	for i, tx := range textos {
		quer[i] = resolverTexto(t, controle, user, tx)
		if quer[i] != tx {
			mudou++
		}
	}
	if mudou < 5 {
		t.Fatalf("o controle só corrigiu %d textos: o teste não prova nada com tão pouco", mudou)
	}

	for _, termo := range palavrasComunsDoPainel {
		t.Run(termo, func(t *testing.T) {
			sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, base, termo, "xqzvariante"))
			for i, tx := range textos {
				if got := resolverTexto(t, sr, user, tx); got != quer[i] {
					t.Errorf("o termo do painel %q mudou o resultado:\n entrada  %q\n sem ele  %q\n com ele  %q", termo, tx, quer[i], got)
				}
			}
		})
	}
}

// TestSmart_TermoDoPainelNaoDesligaAliasDeFraseDeOutroTermo são os três casos
// da revisão, com o dicionário versionado: a palavra que é termo do painel
// está DENTRO de uma frase que é alias de outro termo.
func TestSmart_TermoDoPainelNaoDesligaAliasDeFraseDeOutroTermo(t *testing.T) {
	base := sementeVersionada(t)
	casos := []struct{ termo, variante string }{
		{"Code", "coude"},
		{"Cloud", "claud"},
		{"Vasconcelos", "Vasconselos"},
	}
	textos := map[string]string{
		"o Cloud Code travou de novo":        "o Claude Code travou de novo",
		"falei com o Tiago Vasconcelos hoje": "falei com o Thiago Vasconcelos hoje",
	}

	ctrl, user := smartDaSementeParaTeste(t, base)
	for entra, quer := range textos {
		if got := resolverTexto(t, ctrl, user, entra); got != quer {
			t.Fatalf("controle sem o termo do painel: %q -> %q, quero %q", entra, got, quer)
		}
	}
	for _, c := range casos {
		sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, base, c.termo, c.variante))
		for entra, quer := range textos {
			if got := resolverTexto(t, sr, user, entra); got != quer {
				t.Errorf("com o termo %q do painel: %q -> %q, quero %q", c.termo, entra, got, quer)
			}
		}
	}
}

// TestSmart_TermoDoPainelNaoReescreveAcentoPeloNBest: o ramo da alternativa
// N-best também não pode reescrever por um termo do painel. Hoje só o
// asr/mock preenche Alternatives, mas o ramo existe.
func TestSmart_TermoDoPainelNaoReescreveAcentoPeloNBest(t *testing.T) {
	sr, _ := smartDaSementeParaTeste(t, comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, "esta", "ista"))
	req := ResolveRequest{
		UserID: "mauro",
		ASRResult: asr.ASRResult{
			Text:         "a casa está pronta",
			Alternatives: []asr.Alternative{{Text: "a casa esta pronta"}},
		},
		Context:     uiacontext.ContextSnapshot{PrefixNearCaret: "esta mensagem"},
		CloudPolicy: CloudDisabled,
	}
	got, err := sr.Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.FinalText != "a casa está pronta" {
		t.Errorf("o N-best reescreveu pelo termo do painel: saiu %q (motivos: %+v)", got.FinalText, got.AppliedReplacements)
	}
}

// TestSmart_VarianteDoPainelMantemAInicialMaiuscula: o termo que o painel
// cria em minúscula ("descrição") não tira a maiúscula de quem abre a frase.
func TestSmart_VarianteDoPainelMantemAInicialMaiuscula(t *testing.T) {
	s := comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, "descrição", "transcrição")
	sr, user := smartDaSementeParaTeste(t, s)
	for entra, quer := range map[string]string{
		"Transcrição do áudio pronta.":  "Descrição do áudio pronta.",
		"a transcrição do áudio pronta": "a descrição do áudio pronta",
		"veja a Transcrição abaixo":     "veja a Descrição abaixo", // a inicial do que foi dito acompanha
	} {
		if got := resolverTexto(t, sr, user, entra); got != quer {
			t.Errorf("%q -> %q, quero %q", entra, got, quer)
		}
	}

	// Frase de mais de uma palavra: a inicial do trecho.
	s = comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, "nota fiscal", "nota fical")
	sr, user = smartDaSementeParaTeste(t, s)
	if got, quer := resolverTexto(t, sr, user, "Nota fical emitida."), "Nota fiscal emitida."; got != quer {
		t.Errorf("frase: saiu %q, quero %q", got, quer)
	}

	// Termo do painel que a pessoa escreveu MAIÚSCULO não é mexido, e um termo
	// que NÃO é do painel continua na forma canônica de sempre.
	s = comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, "ECF", "esse efe")
	sr, user = smartDaSementeParaTeste(t, s)
	if got, quer := resolverTexto(t, sr, user, "Esse efe venceu"), "ECF venceu"; got != quer {
		t.Errorf("sigla: saiu %q, quero %q", got, quer)
	}
	sr, user = smartDaSementeParaTeste(t, dictionary.Semente{
		Usuario: "mauro",
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "commit", Prioridade: 75, Alias: []string{"comite"}},
		},
	})
	if got, quer := resolverTexto(t, sr, user, "Comite agora"), "commit agora"; got != quer {
		t.Errorf("termo que não é do painel: saiu %q, quero %q (a regra de caixa dele não mudou)", got, quer)
	}
}

// clienteFixo é um PolishClient que devolve sempre o mesmo final_text.
type clienteFixo struct{ texto string }

func (c clienteFixo) Polish(context.Context, PolishRequest) ([]byte, error) {
	return json.Marshal(map[string]string{"final_text": c.texto})
}

func polirComDoPainel(t *testing.T, sr *SmartResolver, user, entrada, devolve string) ResolveResult {
	t.Helper()
	pr := NewPolishResolver(sr, clienteFixo{texto: devolve})
	got, err := pr.Resolve(context.Background(), ResolveRequest{
		UserID:      user,
		ASRResult:   asr.ASRResult{Text: entrada},
		CloudPolicy: CloudDisabled,
		PolishOptIn: true,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return got
}

// TestPolish_TermoDoPainelNaoTravaOPolishSemAVariante: aprender uma variante
// de uma palavra comum ("e", "ponto") não pode desligar as listas e a
// pontuação falada em todo ditado que tem a palavra. O dataset inteiro, com o
// que o provedor devolveria (o esperado de cada frase), sai igual com e sem o
// termo do painel.
func TestPolish_TermoDoPainelNaoTravaOPolishSemAVariante(t *testing.T) {
	pares, _ := textosDoDataset(t)
	base := sementeVersionada(t)
	controle, user := smartDaSementeParaTeste(t, base)

	quer := make([]ResolveResult, len(pares))
	aceitas := map[string]bool{}
	for i, p := range pares {
		quer[i] = polirComDoPainel(t, controle, user, p[0], p[1])
		if quer[i].Reescrito {
			aceitas[p[0]] = true
		}
	}
	if len(aceitas) < 40 {
		t.Fatalf("o controle só aceitou %d das %d frases do Polish: o teste não prova nada", len(aceitas), len(pares))
	}

	for _, termo := range []string{"e", "ponto", "vírgula", "de", "o", "que", "a", "esta", "social", "web"} {
		t.Run(termo, func(t *testing.T) {
			sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, base, termo, "xqzvariante"))
			for i, p := range pares {
				got := polirComDoPainel(t, sr, user, p[0], p[1])
				if got.Reescrito != quer[i].Reescrito || got.MotivoFallback != quer[i].MotivoFallback || got.FinalText != quer[i].FinalText {
					t.Errorf("o termo do painel %q mudou o Polish de %q:\n sem ele  reescrito=%v motivo=%q\n com ele  reescrito=%v motivo=%q",
						termo, p[0], quer[i].Reescrito, quer[i].MotivoFallback, got.Reescrito, got.MotivoFallback)
				}
			}
		})
	}
}

// TestPolish_ListaComOTermoEDoPainel é o caso de ponta a ponta da revisão.
func TestPolish_ListaComOTermoEDoPainel(t *testing.T) {
	const (
		entrada = "os documentos que faltam são o contrato social o cartão CNPJ e a última DEFIS"
		lista   = "Os documentos que faltam são:\n- O contrato social\n- O cartão CNPJ\n- A última DEFIS"
	)
	sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, sementeVersionada(t), "e", "i"))
	got := polirComDoPainel(t, sr, user, entrada, lista)
	if !got.Reescrito || got.MotivoFallback != MotivoNenhum {
		t.Errorf("a lista saiu como fallback (motivo=%q) por causa do termo \"e\" do painel", got.MotivoFallback)
	}
	if got.FinalText != lista {
		t.Errorf("o texto final não é a lista: %q", got.FinalText)
	}
}

// TestPolish_TermoDoPainelTrocadoPelaVarianteContinuaProtegido: a exceção. No
// ditado em que o Smart aplicou a variante, o termo trocado é protegido como
// qualquer termo do dicionário (o Polish não pode devolver "Descrição" no meio
// da frase). Sem a variante no ditado, o termo não trava nada.
func TestPolish_TermoDoPainelTrocadoPelaVarianteContinuaProtegido(t *testing.T) {
	sr, user := smartDaSementeParaTeste(t, comTermoDoPainel(t, dictionary.Semente{Usuario: "mauro"}, "descrição", "transcrição"))

	// A variante age: o Smart escreve "descrição"; o provedor a devolve com
	// maiúscula no meio da frase.
	got := polirComDoPainel(t, sr, user,
		"eu revisei a transcrição do áudio ontem de noite",
		"Eu revisei a Descrição do áudio ontem de noite")
	if got.Reescrito || got.MotivoFallback != MotivoSiglasTermos {
		t.Errorf("com a variante aplicada: reescrito=%v motivo=%q, quero fallback por %q",
			got.Reescrito, got.MotivoFallback, MotivoSiglasTermos)
	}

	// Sem a variante, o termo não é protegido pela guarda de termos (outra
	// guarda pode recusar; esta não).
	got = polirComDoPainel(t, sr, user,
		"eu revisei a descrição do áudio ontem de noite",
		"Eu revisei a Descrição do áudio ontem de noite")
	if got.MotivoFallback == MotivoSiglasTermos {
		t.Errorf("sem a variante no ditado, o termo do painel travou o Polish por %q", MotivoSiglasTermos)
	}

	// A troca aceita do provedor continua passando com a variante aplicada.
	got = polirComDoPainel(t, sr, user,
		"eu revisei a transcrição do áudio ontem de noite",
		"Eu revisei a descrição do áudio ontem de noite.")
	if !got.Reescrito {
		t.Errorf("a saída certa foi recusada com a variante aplicada (motivo=%q)", got.MotivoFallback)
	}
}

// TestSmart_TermosNoTextoNaoTemOTermoDoPainelQueSoEstaEscrito: a lista de
// termos que as guardas do Polish recebem não inclui o termo do painel só por
// ele estar escrito no texto. Um termo comum do dicionário, no mesmo texto,
// continua entrando (o controle).
func TestSmart_TermosNoTextoNaoTemOTermoDoPainelQueSoEstaEscrito(t *testing.T) {
	s := comTermoDoPainel(t, dictionary.Semente{
		Usuario:  "mauro",
		Entradas: []dictionary.EntradaSemente{{Canonica: "áudio", Prioridade: 60}},
	}, "descrição", "transcrição")
	sr, user := smartDaSementeParaTeste(t, s)

	termos, err := sr.termosNoTexto(context.Background(), ResolveRequest{UserID: user}, "a descrição do áudio e a descrição final")
	if err != nil {
		t.Fatal(err)
	}
	achou := map[string]bool{}
	for _, tm := range termos {
		achou[tm] = true
	}
	if !achou["áudio"] {
		t.Errorf("o controle sumiu: o termo comum não está em %v", termos)
	}
	if achou["descrição"] {
		t.Errorf("o termo do painel entrou na lista só por estar escrito: %v", termos)
	}
}

// TestSmart_TermoDoPainelNaoEscondeOCandidatoSeguinte: quando o casamento
// exato do termo do painel vem primeiro na busca (prioridade maior), o
// candidato seguinte é o que valeria se o termo não existisse, e é ele que o
// Smart usa. O dicionário tem um alias "zeta" de outro termo, de prioridade
// menor, que só pode ter entrado depois (à mão ou pelo dictlearn), porque o
// painel recusaria criar "zeta" como termo.
func TestSmart_TermoDoPainelNaoEscondeOCandidatoSeguinte(t *testing.T) {
	const (
		semPainel = `{"usuario":"m","entradas":[` +
			`{"canonica":"Zeta Corp","prioridade":40,"alias":["zeta"]}]}`
		comPainel = `{"usuario":"m","alias_origem":{"zz":"painel"},"entradas":[` +
			`{"canonica":"zeta","prioridade":50,"alias":["zz"],"origem":"painel"},` +
			`{"canonica":"Zeta Corp","prioridade":40,"alias":["zeta"]}]}`
	)
	repoA, userA := repoDoPainel(t, semPainel)
	repoB, userB := repoDoPainel(t, comPainel)
	const entra = "abri o zeta ontem"
	quer := resolverTexto(t, NewSmartResolver(repoA), userA, entra)
	if quer != "abri o Zeta Corp ontem" {
		t.Fatalf("o controle (sem o termo do painel) saiu %q", quer)
	}
	if got := resolverTexto(t, NewSmartResolver(repoB), userB, entra); got != quer {
		t.Errorf("com o termo do painel saiu %q, e sem ele %q", got, quer)
	}
}

// TestComTermosTrocados: só a forma que o Smart escreveu no lugar de uma
// variante do dicionário entra, sem repetir e sem vazio.
func TestComTermosTrocados(t *testing.T) {
	got := comTermosTrocados([]string{"ECF"}, []AppliedReplacement{
		{Reason: ReasonDictionary, Replacement: "descrição"},
		{Reason: ReasonDictionary, Replacement: "ECF"},       // já estava
		{Reason: ReasonDictionary, Replacement: "descrição"}, // repetido
		{Reason: ReasonDictionary, Replacement: "   "},       // vazio
		{Reason: ReasonCloud, Replacement: "texto do cloud"}, // não é troca de variante
		{Reason: ReasonSelfCorrection, Replacement: "correção"},
	})
	if want := []string{"ECF", "descrição"}; !slices.Equal(got, want) {
		t.Errorf("termos = %q, quero %q", got, want)
	}
	if got := comTermosTrocados(nil, nil); len(got) != 0 {
		t.Errorf("sem nada a acrescentar saiu %q", got)
	}
}
