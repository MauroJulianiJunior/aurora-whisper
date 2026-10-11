package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/winruntime"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr"
	uiacontext "github.com/MauroJulianiJunior/aurora-whisper/internal/context"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/resolver"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/smartconta"
)

// O "Adicionar termo" (ADR-020) do lado do Agent: grava, troca o dicionário e
// o PROMPT do ASR na hora, e recusa o que não agiria ou quebraria o que já
// age. Tudo em t.TempDir(), com o runtime falso: nada toca o
// bin\dicionario.json, abre áudio, digita ou usa a área de transferência.

// dicionarioDe monta o dicionário do runtime de um JSON, como a subida.
func dicionarioDe(t *testing.T, conteudo string) winruntime.Dicionario {
	t.Helper()
	smart, err := smartconta.SmartDaSemente(mustSemente(t, conteudo))
	if err != nil {
		t.Fatal(err)
	}
	return winruntime.Dicionario{Resolver: smart}
}

func promptDe(t *testing.T, m montagem) string {
	t.Helper()
	d := m.rt.ultimo(t)
	if d.Vocabulario == nil {
		t.Fatal("o dicionário trocado não leva o prompt do ASR")
	}
	return *d.Vocabulario
}

func TestTermos_AdicionarGravaETrocaOPromptNaHora(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	mud, err := m.a.AdicionarTermo(context.Background(), dictionary.PedidoTermo{
		Termo: "RAG", Variantes: []string{"rague", "hage"}, Dica: true,
	})
	if err != nil {
		t.Fatalf("AdicionarTermo: %v", err)
	}
	if mud.Termo != "RAG" || mud.JaExistia || len(mud.Lista.Itens) != 1 {
		t.Errorf("mudança = %+v", mud)
	}
	if m.rt.quantos() != 1 {
		t.Fatalf("o runtime recebeu %d dicionários, quero 1", m.rt.quantos())
	}
	// A dica dela vai na frente do prompt do próximo ditado.
	if got := promptDe(t, m); !strings.HasPrefix(got, "RAG, ") {
		t.Errorf("prompt = %q, quero começando por RAG", got)
	}
	// E as grafias erradas já viram o termo.
	d := m.rt.ultimo(t)
	if got := resolve(t, d, "me explica como funciona um rague"); got != "me explica como funciona um RAG" {
		t.Errorf("o Smart novo resolveu %q", got)
	}
	if smartDasGuardas(t, d) != d.Resolver {
		t.Error("as guardas do Polish consultam outro dicionário que não o do Smart novo")
	}
	if !bytes.Contains(m.bytes(t), []byte(`"origem": "adicionado"`)) {
		t.Errorf("o arquivo não foi gravado:\n%s", m.bytes(t))
	}
	// O log diz quantos termos foram ao prompt, e nada do texto.
	log := m.log.String()
	if !strings.Contains(log, "acao=adicionar_termo") || !strings.Contains(log, "termos_no_prompt=3") {
		t.Errorf("log sem a ação ou a contagem do prompt:\n%s", log)
	}
	for _, texto := range []string{"RAG", "rague", "hage"} {
		if strings.Contains(log, texto) {
			t.Errorf("o log traz %q:\n%s", texto, log)
		}
	}
}

func TestTermos_DicaERemoverTrocamOPrompt(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	ctx := context.Background()
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}, Dica: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.a.DicaDoTermo(ctx, "RAG", false); err != nil {
		t.Fatal(err)
	}
	if got := promptDe(t, m); strings.Contains(got, "RAG") {
		t.Errorf("a dica desligada continua no prompt: %q", got)
	}
	// Sem a dica, a grafia segue valendo.
	if got := resolve(t, m.rt.ultimo(t), "um rague"); got != "um RAG" {
		t.Errorf("sem a dica, o Smart resolveu %q", got)
	}
	if _, err := m.a.RemoverTermo(ctx, "rag"); err != nil {
		t.Fatal(err)
	}
	if got := resolve(t, m.rt.ultimo(t), "um rague"); got != "um rague" {
		t.Errorf("removido, o Smart ainda resolveu %q", got)
	}
	if !bytes.Equal(m.bytes(t), []byte(dicionarioDoAprendizado)) {
		t.Errorf("adicionar, desligar e remover não voltou ao arquivo de antes:\n%s", m.bytes(t))
	}
	l, err := m.a.ListarTermos(ctx)
	if err != nil || len(l.Itens) != 0 {
		t.Errorf("lista = %+v, %v", l, err)
	}
}

// Uma grafia nova que o dicionário não aplicaria é recusada, dizendo qual, e
// nada é gravado: "Vite" (90) leva a busca por "Viti".
func TestTermos_GrafiaQueNaoAgiriaERecusada(t *testing.T) {
	conteudo := dicionarioComConcorrente(``, ``)
	m := montarAprendizado(t, conteudo, "mauro")
	_, err := m.a.AdicionarTermo(context.Background(), dictionary.PedidoTermo{
		Termo: "Vitti", Variantes: []string{"bitti", "Viti"},
	})
	var na *dictionary.ErroNaoAgiria
	if !errors.As(err, &na) || na.Variante != "Viti" || !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Fatalf("err = %v, quero ErroNaoAgiria de Viti", err)
	}
	if !bytes.Equal(m.bytes(t), []byte(conteudo)) || m.rt.quantos() != 0 {
		t.Error("a recusa gravou ou trocou o dicionário")
	}
}

// dicionarioComViti: "Viti" é variante de "Vitti" (90), e age.
const dicionarioComViti = `{
  "usuario": "mauro",
  "entradas": [
    {"canonica": "Vitti", "prioridade": 90, "alias": ["Viti"]}
  ]
}
`

// A regressão nova: "Vite" com a dica (100) passa na frente do alias "Viti"
// de "Vitti" (90), que deixaria de agir. Recusado, nada gravado. Sem a dica
// (50), "Vitti" continua levando a busca, e o termo entra.
func TestTermos_DicaQueQuebrariaUmAliasERecusada(t *testing.T) {
	m := montarAprendizado(t, dicionarioComViti, "mauro")
	ctx := context.Background()
	if got := resolve(t, dicionarioDe(t, dicionarioComViti), "Viti"); got != "Vitti" {
		t.Fatalf("pré-condição: Viti não vira Vitti (%q)", got)
	}
	_, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "Vite", Dica: true})
	var q *dictionary.ErroQuebraria
	if !errors.As(err, &q) || q.Variante != "Viti" || q.Termo != "Vitti" {
		t.Fatalf("err = %v, quero ErroQuebraria de Viti/Vitti", err)
	}
	if !bytes.Equal(m.bytes(t), []byte(dicionarioComViti)) || m.rt.quantos() != 0 {
		t.Fatal("a recusa gravou ou trocou o dicionário")
	}
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "Vite"}); err != nil {
		t.Fatalf("sem a dica: %v", err)
	}
	if got := resolve(t, m.rt.ultimo(t), "Viti"); got != "Vitti" {
		t.Errorf("Viti virou %q", got)
	}
	// E ligar a dica depois é recusado pelo mesmo motivo.
	if _, err := m.a.DicaDoTermo(ctx, "Vite", true); !errors.As(err, &q) {
		t.Errorf("ligar a dica: err = %v, quero ErroQuebraria", err)
	}
}

// dicionarioComVitti: "Vitti" (60), sem alias. A forma canônica, escrita
// certa, fica como está.
const dicionarioComVitti = `{
  "usuario": "mauro",
  "entradas": [
    {"canonica": "Vitti", "prioridade": 60}
  ]
}
`

// resolveComContexto resolve texto com perto no texto antes do cursor: é o
// contexto que corrobora a fonética no Smart.
func resolveComContexto(t *testing.T, d winruntime.Dicionario, texto, perto string) string {
	t.Helper()
	out, err := d.Resolver.Resolve(context.Background(), resolver.ResolveRequest{
		UserID:    "mauro",
		ASRResult: asr.ASRResult{Text: texto},
		Context:   uiacontext.ContextSnapshot{PrefixNearCaret: perto},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return out.FinalText
}

// A conferência olha também a forma canônica dos outros termos (revisão de
// 01-out-2026): "Vite" com a dica (100) passa na frente de "Vitti" (60) por
// fonética, e com o contexto citando "vite" o Smart trocaria "Vitti", escrito
// certo, por "Vite". Recusado, nada gravado. Sem a dica (50), "Vitti" segue na
// frente, o termo entra e o texto certo fica.
func TestTermos_DicaQueTrocariaAFormaCanonicaDeOutroERecusada(t *testing.T) {
	m := montarAprendizado(t, dicionarioComVitti, "mauro")
	ctx := context.Background()
	const ditado, perto = "reunião com o Vitti amanhã", "npm create vite@latest"
	if got := resolveComContexto(t, dicionarioDe(t, dicionarioComVitti), ditado, perto); got != ditado {
		t.Fatalf("pré-condição: o ditado virou %q", got)
	}
	_, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "Vite", Dica: true})
	var q *dictionary.ErroQuebraria
	if !errors.As(err, &q) || q.Variante != "Vitti" || q.Termo != "Vitti" || q.Vira != "Vite" {
		t.Fatalf("err = %v (%+v), quero ErroQuebraria da forma Vitti, que viraria Vite", err, q)
	}
	if !bytes.Equal(m.bytes(t), []byte(dicionarioComVitti)) || m.rt.quantos() != 0 {
		t.Fatal("a recusa gravou ou trocou o dicionário")
	}
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "Vite"}); err != nil {
		t.Fatalf("sem a dica: %v", err)
	}
	if got := resolveComContexto(t, m.rt.ultimo(t), ditado, perto); got != ditado {
		t.Errorf("sem a dica, o ditado virou %q", got)
	}
	// Ligar a dica depois é recusado pelo mesmo motivo.
	if _, err := m.a.DicaDoTermo(ctx, "Vite", true); !errors.As(err, &q) || q.Variante != "Vitti" {
		t.Errorf("ligar a dica: err = %v, quero ErroQuebraria de Vitti", err)
	}
}

// A dica é a prioridade, e a grafia que a pessoa pôs pode só agir com ela
// (revisão de 01-out-2026: com "Vite" (90) e "Vitti" (60), "Vity" com a grafia
// "Viti" e a dica entrava, e depois a dica não desligava mais, porque "Viti"
// deixaria de virar "Vity"). O termo que só age na frente de outro, por
// fonética, também troca a forma canônica dele quando o contexto o cita: a
// conferência da forma canônica o recusa já no Adicionar. Sem a dica, a
// grafia não agiria.
func TestTermos_GrafiaQueSoAgeComADicaNaFrenteDeOutroTermo(t *testing.T) {
	conteudo := dicionarioComConcorrente(``, ``)
	m := montarAprendizado(t, conteudo, "mauro")
	ctx := context.Background()
	_, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "Vity", Variantes: []string{"Viti"}, Dica: true})
	var q *dictionary.ErroQuebraria
	if !errors.As(err, &q) || q.Variante != q.Termo || q.Vira != "Vity" {
		t.Fatalf("com a dica: err = %v (%+v), quero ErroQuebraria de uma forma canônica que viraria Vity", err, q)
	}
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "Vity", Variantes: []string{"Viti"}}); !errors.Is(err, dictionary.ErrNaoAgiria) {
		t.Errorf("sem a dica: err = %v, quero ErrNaoAgiria", err)
	}
	if !bytes.Equal(m.bytes(t), []byte(conteudo)) || m.rt.quantos() != 0 {
		t.Error("a recusa gravou ou trocou o dicionário")
	}
}

// A forma canônica que já virava outro termo antes da mudança não é
// conferida: a mudança não a piora. Com "Vite" (90) na frente de "Vitti" (60),
// "Vitti" com o contexto citando "vite" já virava "Vite"; uma grafia nova em
// "Vite" não é recusada por isso.
func TestTermos_FormaCanonicaQueJaNaoFicavaNaoERecusada(t *testing.T) {
	conteudo := dicionarioComConcorrente(``, ``)
	if got := resolveComContexto(t, dicionarioDe(t, conteudo), "Vitti", "Vite"); got != "Vite" {
		t.Fatalf("pré-condição: Vitti virou %q", got)
	}
	m := montarAprendizado(t, conteudo, "mauro")
	if _, err := m.a.AdicionarTermo(context.Background(), dictionary.PedidoTermo{Termo: "Vite", Variantes: []string{"vaite"}}); err != nil {
		t.Errorf("err = %v", err)
	}
}

// O Agent liga a guarda do motor (learning.Arriscada): "das" como grafia, e
// "não" como termo, são recusados sem gravar.
func TestTermos_PalavraComumERecusada(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	ctx := context.Background()
	for _, p := range []dictionary.PedidoTermo{
		{Termo: "RAG", Variantes: []string{"rague", "das"}},
		{Termo: "Não"},
		{Termo: "2025", Dica: true},
	} {
		if _, err := m.a.AdicionarTermo(ctx, p); !errors.Is(err, dictionary.ErrPalavraComum) {
			t.Errorf("%+v: err = %v, quero ErrPalavraComum", p, err)
		}
	}
	if !bytes.Equal(m.bytes(t), []byte(dicionarioDoAprendizado)) || m.rt.quantos() != 0 {
		t.Error("a recusa gravou ou trocou o dicionário")
	}
}

// A grafia é mais estrita que o termo (learning.GrafiaArriscada): a palavra de
// polaridade em qualquer lugar apagaria a negação de todo ditado ("eu não
// rague no projeto" viraria "eu RAG no projeto"), e a palavra funcional
// sozinha trocaria a palavra de toda frase ("eu" viraria "RAG"). As duas são
// recusadas sem gravar; o "Nada Consta" segue passando como termo.
func TestTermos_GrafiaComPolaridadeOuFuncionalERecusada(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	ctx := context.Background()
	for _, g := range []string{"não rague", "nunca rague", "mais rague", "sempre hage", "eu", "né", "isso", "já", "tá", "aí", "lá", "foi"} {
		_, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "RAG", Variantes: []string{g}, Dica: true})
		var pc *dictionary.ErroPalavraComum
		if !errors.As(err, &pc) || pc.Texto != g {
			t.Errorf("grafia %q: err = %v, quero ErroPalavraComum dela", g, err)
		}
	}
	if !bytes.Equal(m.bytes(t), []byte(dicionarioDoAprendizado)) || m.rt.quantos() != 0 {
		t.Fatal("a recusa gravou ou trocou o dicionário")
	}

	// A grafia sem a negação entra, e a negação do ditado fica onde estava.
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "RAG", Variantes: []string{"rague"}, Dica: true}); err != nil {
		t.Fatal(err)
	}
	d := m.rt.ultimo(t)
	if got := resolve(t, d, "eu não rague no projeto"); got != "eu não RAG no projeto" {
		t.Errorf("resolveu %q, quero a negação no lugar", got)
	}
	if got := resolve(t, d, "eu acho que eu vou na terça"); got != "eu acho que eu vou na terça" {
		t.Errorf("resolveu %q, quero o ditado como veio", got)
	}
	// O termo com polaridade e uma palavra de conteúdo continua passando.
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "Nada Consta"}); err != nil {
		t.Errorf("Nada Consta: %v", err)
	}
}

func TestTermos_SemDicionarioNaSubida(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "")
	ctx := context.Background()
	if _, err := m.a.ListarTermos(ctx); !errors.Is(err, dictionary.ErrSemArquivo) {
		t.Errorf("listar: %v", err)
	}
	if _, err := m.a.AdicionarTermo(ctx, dictionary.PedidoTermo{Termo: "RAG"}); !errors.Is(err, dictionary.ErrSemArquivo) {
		t.Errorf("adicionar: %v", err)
	}
}

// Gravado, mas não trocado: a mudança vem preenchida junto com o erro.
func TestTermos_GravadoSemTroca(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	m.a.montar = func(dictionary.Semente) (*resolver.SmartResolver, error) { return nil, errors.New("falhou") }
	mud, err := m.a.AdicionarTermo(context.Background(), dictionary.PedidoTermo{Termo: "RAG", Dica: true})
	if !errors.Is(err, dictionary.ErrGravadoSemTroca) || mud.Termo != "RAG" || len(mud.Lista.Itens) != 1 {
		t.Errorf("mudança = %+v, err = %v", mud, err)
	}
	if m.rt.quantos() != 0 {
		t.Error("o runtime recebeu dicionário")
	}
}

// O aprender pelo histórico também leva o prompt, remontado do arquivo: é o
// mesmo de antes (a variante não entra nele).
func TestAprender_LevaOPromptDeSempre(t *testing.T) {
	m := montarAprendizado(t, dicionarioDoAprendizado, "mauro")
	if _, err := m.a.Aprender(context.Background(), "ECF", "SF"); err != nil {
		t.Fatal(err)
	}
	quer := dictionary.Vocabulario(mustSemente(t, dicionarioDoAprendizado).ParaEntradas(), dictionary.OrcamentoRunasPadrao)
	if got := promptDe(t, m); got != quer {
		t.Errorf("prompt = %q, quero o de antes, %q", got, quer)
	}
}

// BenchmarkConferirTermos mede a conferência de uma mudança no dicionário do
// repositório (testdata/asr-dataset/dicionario.json), com uma dica nova.
func BenchmarkConferirTermos(b *testing.B) {
	dados, err := os.ReadFile(arquivoDicionarioVersionado)
	if err != nil {
		b.Fatal(err)
	}
	antes, err := dictionary.CarregarSemente(bytes.NewReader(dados))
	if err != nil {
		b.Fatal(err)
	}
	depois, mud, err := antes.AdicionarTermo(dictionary.PedidoTermo{Termo: "RAG", Variantes: []string{"rague", "hage"}, Dica: true}, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := conferirTermos(antes, depois, mud.Novas); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkConferirTermos_NoTeto mede o pior caso do "Adicionar termo": os
// termos da pessoa no teto (MaxTermosAdicionados), cada um com as
// MaxVariantesPorTermo grafias, sobre o dicionário do repositório.
func BenchmarkConferirTermos_NoTeto(b *testing.B) {
	dados, err := os.ReadFile(arquivoDicionarioVersionado)
	if err != nil {
		b.Fatal(err)
	}
	antes, err := dictionary.CarregarSemente(bytes.NewReader(dados))
	if err != nil {
		b.Fatal(err)
	}
	letras := "bcdfgjklmnpqrstvxz"
	for i := 0; i < dictionary.MaxTermosAdicionados-1; i++ {
		termo := "Kw" + string(letras[i%len(letras)]) + string(letras[(i/len(letras))%len(letras)]) + "yo"
		for j := 0; j < dictionary.MaxVariantesPorTermo; j += dictionary.MaxVariantesPorPedido {
			var vs []string
			for k := j; k < j+dictionary.MaxVariantesPorPedido; k++ {
				vs = append(vs, termo+"x"+string(letras[k]))
			}
			if antes, _, err = antes.AdicionarTermo(dictionary.PedidoTermo{Termo: termo, Variantes: vs}, nil); err != nil {
				b.Fatal(err)
			}
		}
	}
	depois, mud, err := antes.AdicionarTermo(dictionary.PedidoTermo{Termo: "RAG", Variantes: []string{"rague", "hage"}, Dica: true}, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := conferirTermos(antes, depois, mud.Novas); err != nil {
			b.Fatal(err)
		}
	}
}
