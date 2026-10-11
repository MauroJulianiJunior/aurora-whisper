package localapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/learning"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// /api/termos (ADR-020) contra um dublê em memória: o que ele faz com a
// semente é a lógica pura de dictionary.Semente, a mesma de produção, com a
// guarda de palavra comum de produção (learning.Arriscada). Nada aqui abre
// arquivo, digita ou mexe na área de transferência.

type termosFalso struct {
	mu  sync.Mutex
	dic dictionary.Semente

	err             error // o que toda chamada devolve, sem gravar, se não nil
	gravadoSemTroca bool  // as mutações gravam e devolvem ErrGravadoSemTroca
	mutacoes        int
}

func novoTermosFalso() *termosFalso {
	return &termosFalso{dic: dictionary.Semente{
		Usuario: "mauro",
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "DCTFWeb", Prioridade: 100},
			{Canonica: "ECF", Prioridade: 90, Alias: []string{"sf"}},
		},
	}}
}

// comum é a guarda de produção (cmd/agent, palavraComum): a do motor para o
// termo, e a mais estrita para a grafia.
func comum(t string, grafia bool) string {
	if grafia {
		return string(learning.GrafiaArriscada(t))
	}
	return string(learning.Arriscada(t))
}

func (f *termosFalso) ListarTermos(context.Context) (dictionary.ListaDeTermos, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return dictionary.ListaDeTermos{}, f.err
	}
	return f.dic.TermosAdicionados(), nil
}

func (f *termosFalso) mudar(op func(dictionary.Semente) (dictionary.Semente, dictionary.MudancaDeTermos, error)) (dictionary.MudancaDeTermos, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutacoes++
	if f.err != nil {
		return dictionary.MudancaDeTermos{}, f.err
	}
	novo, m, err := op(f.dic)
	if err != nil {
		return dictionary.MudancaDeTermos{}, err
	}
	f.dic = novo
	if f.gravadoSemTroca {
		return m, dictionary.ErrGravadoSemTroca
	}
	return m, nil
}

func (f *termosFalso) AdicionarTermo(_ context.Context, p dictionary.PedidoTermo) (dictionary.MudancaDeTermos, error) {
	return f.mudar(func(s dictionary.Semente) (dictionary.Semente, dictionary.MudancaDeTermos, error) {
		return s.AdicionarTermo(p, comum)
	})
}

func (f *termosFalso) DicaDoTermo(_ context.Context, termo string, ligar bool) (dictionary.MudancaDeTermos, error) {
	return f.mudar(func(s dictionary.Semente) (dictionary.Semente, dictionary.MudancaDeTermos, error) {
		return s.DicaDoTermo(termo, ligar)
	})
}

func (f *termosFalso) RemoverTermo(_ context.Context, termo string) (dictionary.MudancaDeTermos, error) {
	return f.mudar(func(s dictionary.Semente) (dictionary.Semente, dictionary.MudancaDeTermos, error) {
		return s.RemoverTermo(termo)
	})
}

type ambienteDeTermos struct {
	h   http.Handler
	f   *termosFalso
	log *bytes.Buffer
}

// novoAmbienteDeTermos monta o servidor com o dublê (nil: sem Termos).
func novoAmbienteDeTermos(t *testing.T, f *termosFalso) *ambienteDeTermos {
	t.Helper()
	buf := &bytes.Buffer{}
	pref := prefsDeTeste()
	if f != nil {
		pref.Termos = f
	}
	pref.Log = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref)
	return &ambienteDeTermos{h: srv.Handler(), f: f, log: buf}
}

type termoView struct {
	Termo        string   `json:"termo"`
	Variantes    []string `json:"variantes"`
	Dica         bool     `json:"dica"`
	DoDicionario bool     `json:"do_dicionario"`
}

type termosView struct {
	Items *[]termoView `json:"items"`
	Dicas struct {
		Usadas    int `json:"usadas"`
		Orcamento int `json:"orcamento"`
	} `json:"dicas"`
	Termo           string    `json:"termo"`
	JaExistia       bool      `json:"ja_existia"`
	DicaIgnorada    bool      `json:"dica_ignorada"`
	SairamDasDicas  *[]string `json:"sairam_das_dicas"`
	ValeAoReiniciar bool      `json:"vale_ao_reiniciar"`
}

func (a *ambienteDeTermos) adicionar(t *testing.T, corpo any) *httptest.ResponseRecorder {
	t.Helper()
	return fazer(t, a.h, http.MethodPost, "/api/termos", corpo)
}

func TestTermos_AdicionarListarDicaRemover(t *testing.T) {
	a := novoAmbienteDeTermos(t, novoTermosFalso())

	w := fazer(t, a.h, http.MethodGet, "/api/termos", nil)
	exigirStatus(t, w, http.StatusOK)
	v := decodeJSON[termosView](t, w)
	if v.Items == nil || len(*v.Items) != 0 || v.Dicas.Orcamento != 140 || v.Dicas.Usadas != 0 {
		t.Fatalf("lista vazia = %s", w.Body.String())
	}

	w = a.adicionar(t, map[string]any{"termo": "RAG", "variantes": []string{"rague", "hage"}, "dica": true})
	exigirStatus(t, w, http.StatusOK)
	v = decodeJSON[termosView](t, w)
	if v.Termo != "RAG" || v.JaExistia || v.SairamDasDicas == nil || len(*v.SairamDasDicas) != 0 || v.ValeAoReiniciar {
		t.Errorf("resposta = %s", w.Body.String())
	}
	if len(*v.Items) != 1 || (*v.Items)[0].Termo != "RAG" || !(*v.Items)[0].Dica || len((*v.Items)[0].Variantes) != 2 || v.Dicas.Usadas != 3 {
		t.Errorf("lista = %s", w.Body.String())
	}
	for _, marca := range []string{"vale_ao_reiniciar", "dica_ignorada"} {
		if strings.Contains(w.Body.String(), marca) {
			t.Errorf("a resposta comum traz %s: %s", marca, w.Body.String())
		}
	}

	// O termo de antes, com grafia nova: aparece como do dicionário.
	w = a.adicionar(t, map[string]any{"termo": "ecf", "variantes": []string{"ecefe"}, "dica": true})
	exigirStatus(t, w, http.StatusOK)
	v = decodeJSON[termosView](t, w)
	// Na ordem do arquivo: o ECF, de antes, vem primeiro.
	if !v.JaExistia || v.Termo != "ECF" || len(*v.Items) != 2 || !(*v.Items)[0].DoDicionario || (*v.Items)[0].Dica {
		t.Errorf("termo de antes = %s", w.Body.String())
	}
	// O pedido trazia a dica, e a dele não muda por aqui: a resposta diz.
	if !v.DicaIgnorada {
		t.Errorf("a resposta não diz que a dica do termo de antes não mudou: %s", w.Body.String())
	}

	w = fazer(t, a.h, http.MethodPost, "/api/termos/dica", map[string]any{"termo": "RAG", "dica": false})
	exigirStatus(t, w, http.StatusOK)
	if v = decodeJSON[termosView](t, w); (*v.Items)[1].Dica || v.Dicas.Usadas != 0 {
		t.Errorf("dica desligada = %s", w.Body.String())
	}

	w = fazer(t, a.h, http.MethodPost, "/api/termos/remover", map[string]any{"termo": "RAG"})
	exigirStatus(t, w, http.StatusOK)
	if v = decodeJSON[termosView](t, w); len(*v.Items) != 1 || (*v.Items)[0].Termo != "ECF" || v.Termo != "RAG" {
		t.Errorf("removido = %s", w.Body.String())
	}
}

// Quando as dicas dela empurram termos de antes para fora do prompt, a
// resposta diz quais.
func TestTermos_DizQuemSaiuDasDicas(t *testing.T) {
	a := novoAmbienteDeTermos(t, novoTermosFalso())
	for _, termo := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)} {
		exigirStatus(t, a.adicionar(t, map[string]any{"termo": termo, "dica": true}), http.StatusOK)
	}
	w := a.adicionar(t, map[string]any{"termo": strings.Repeat("d", 14), "dica": true})
	exigirStatus(t, w, http.StatusOK)
	v := decodeJSON[termosView](t, w)
	if v.SairamDasDicas == nil || strings.Join(*v.SairamDasDicas, ",") != "DCTFWeb,ECF" {
		t.Errorf("saíram = %s", w.Body.String())
	}
}

func TestTermos_ErrosViramAFraseDaTela(t *testing.T) {
	casos := []struct {
		nome   string
		antes  []map[string]any // pedidos de Adicionar que preparam o caso
		rota   string
		corpo  map[string]any
		status int
		frase  string
	}{
		{"palavra comum", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"das"}}, 400,
			"“das” é palavra comum demais: trocaria texto certo."},
		{"só número", nil, "/api/termos", map[string]any{"termo": "2025"}, 400,
			"“2025” é só número: trocaria texto certo."},
		{"grafia que muda o sentido", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"não rague"}}, 400,
			"“não rague” tem palavra que muda o sentido da frase, e ela sumiria do texto."},
		{"grafia funcional", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"eu"}}, 400,
			"“eu” é palavra comum demais: trocaria texto certo."},
		{"grafia de outro termo", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"SF"}}, 409,
			"“SF” já vira “ECF” no dicionário."},
		{"grafia que é outro termo", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"dctf web"}}, 409,
			"“dctf web” já é um termo do dicionário."},
		{"termo que é grafia de outro", nil, "/api/termos", map[string]any{"termo": "sf"}, 409,
			"“sf” já vira “ECF” no dicionário e não pode virar termo."},
		{"grafia que já é dele", []map[string]any{{"termo": "RAG", "variantes": []string{"rague"}}}, "/api/termos",
			map[string]any{"termo": "rag", "variantes": []string{"Rague"}}, 409, "“Rague” já vira “RAG” no dicionário."},
		{"grafia que é o termo", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"rág"}}, 400,
			"“rág” só muda maiúscula, acento ou espaço, e o dicionário já entende isso."},
		{"grafia curta", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"r"}}, 400,
			"“r” não serve: cada grafia tem de 2 a 40 caracteres, até 4 palavras, numa linha só."},
		{"termo com quebra de linha", nil, "/api/termos", map[string]any{"termo": "RAG\nX"}, 400,
			"A palavra certa vai até 40 caracteres e 4 palavras, numa linha só."},
		{"sem termo", nil, "/api/termos", map[string]any{"termo": "  "}, 400, "Faltou a palavra certa."},
		{"seis grafias", nil, "/api/termos", map[string]any{"termo": "RAG", "variantes": []string{"aa", "bb", "cc", "dd", "ee", "ff"}}, 400,
			"Dá para mandar até 5 grafias erradas por vez."},
		{"nada a mudar", nil, "/api/termos", map[string]any{"termo": "ECF"}, 400,
			"“ECF” já está no dicionário assim."},
		{"só a dica, num termo do dicionário", nil, "/api/termos", map[string]any{"termo": "ECF", "dica": true}, 409,
			"“ECF” já é do dicionário: a dica dele não muda por aqui."},
		{"sem espaço", []map[string]any{
			{"termo": strings.Repeat("a", 40), "dica": true}, {"termo": strings.Repeat("b", 40), "dica": true},
			{"termo": strings.Repeat("c", 40), "dica": true}, {"termo": strings.Repeat("e", 15)},
		}, "/api/termos/dica", map[string]any{"termo": strings.Repeat("e", 15), "dica": true}, 409,
			"Não cabe nas dicas: sobram 16 caracteres."},
		{"dica de termo que não é dela", nil, "/api/termos/dica", map[string]any{"termo": "ECF", "dica": true}, 404,
			"“ECF” não foi adicionado por você."},
		{"remover o que não é dela", nil, "/api/termos/remover", map[string]any{"termo": "ECF"}, 404,
			"“ECF” não foi adicionado por você."},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			a := novoAmbienteDeTermos(t, novoTermosFalso())
			for _, p := range c.antes {
				exigirStatus(t, a.adicionar(t, p), http.StatusOK)
			}
			antes := a.f.dic.TermosAdicionados()
			w := fazer(t, a.h, http.MethodPost, c.rota, c.corpo)
			exigirStatus(t, w, c.status)
			if got := decodeJSON[erroView](t, w).Error.Message; got != c.frase {
				t.Errorf("frase = %q, quero %q", got, c.frase)
			}
			if depois := a.f.dic.TermosAdicionados(); len(depois.Itens) != len(antes.Itens) || depois.Dicas != antes.Dicas {
				t.Error("a recusa mudou o dicionário")
			}
		})
	}
}

// Os erros que só o Agent produz (ConferirMudanca, o arquivo), com o dublê
// devolvendo cada um.
func TestTermos_ErrosDoAgent(t *testing.T) {
	casos := []struct {
		nome   string
		err    error
		rota   string
		corpo  map[string]any
		status int
		frase  string
	}{
		{"não agiria", &dictionary.ErroNaoAgiria{Variante: "Viti"}, "/api/termos", map[string]any{"termo": "Vitti"}, 409,
			"O dicionário não aplicaria “Viti” num ditado. Nada foi gravado."},
		{"quebraria, ao adicionar", &dictionary.ErroQuebraria{Variante: "sf", Termo: "ECF"}, "/api/termos", map[string]any{"termo": "RAG"}, 409,
			"Com “RAG”, “sf” deixaria de virar “ECF”. Nada foi gravado."},
		{"quebraria, na dica", &dictionary.ErroQuebraria{Variante: "sf", Termo: "ECF"}, "/api/termos/dica", map[string]any{"termo": "RAG"}, 409,
			"Mudando a dica de “RAG”, “sf” deixaria de virar “ECF”. Nada foi gravado."},
		{"quebraria, ao remover", &dictionary.ErroQuebraria{Variante: "x", Termo: "Y"}, "/api/termos/remover", map[string]any{"termo": "RAG"}, 409,
			"Sem “RAG”, “x” deixaria de virar “Y”. Nada foi gravado."},
		{"quebraria a forma de outro", &dictionary.ErroQuebraria{Variante: "Vitti", Termo: "Vitti", Vira: "Vite"}, "/api/termos", map[string]any{"termo": "Vite"}, 409,
			"Com “Vite”, “Vitti”, escrito certo, passaria a virar “Vite”. Nada foi gravado."},
		{"quebraria a forma de outro, sem saber o quê", &dictionary.ErroQuebraria{Variante: "Vitti", Termo: "Vitti"}, "/api/termos/dica", map[string]any{"termo": "Vite"}, 409,
			"Mudando a dica de “Vite”, “Vitti”, escrito certo, passaria a ser trocado. Nada foi gravado."},
		{"a grafia dele depende da dica, ao desligar", &dictionary.ErroQuebraria{Variante: "Viti", Termo: "Vity"}, "/api/termos/dica", map[string]any{"termo": "vity"}, 409,
			"“Viti” só vira “Vity” com a dica ligada. Para desligá-la, remova “Vity” e adicione de novo sem essa grafia. Nada foi gravado."},
		{"a grafia dele depende da dica, ao adicionar", &dictionary.ErroQuebraria{Variante: "Viti", Termo: "Vity"}, "/api/termos", map[string]any{"termo": "Vity"}, 409,
			"“Viti” só vira “Vity” com a dica ligada: deixe a dica marcada, ou remova “Vity” e adicione de novo sem essa grafia. Nada foi gravado."},
		{"a grafia de outro termo, ao remover", &dictionary.ErroQuebraria{Variante: "Viti", Termo: "Vity"}, "/api/termos/remover", map[string]any{"termo": "Vity"}, 409,
			"Sem “Vity”, “Viti” deixaria de virar “Vity”. Nada foi gravado."},
		{"desligado", &dictionary.ErroTermoInativoDe{Termo: "Desligado"}, "/api/termos", map[string]any{"termo": "desligado"}, 409,
			"“Desligado” está desligado no dicionário."},
		{"limite de termos", &dictionary.ErroLimite{Do: dictionary.LimiteTermos, Maximo: 50}, "/api/termos", map[string]any{"termo": "RAG"}, 400,
			"Já há 50 termos adicionados. Remova um antes."},
		{"limite por termo", &dictionary.ErroLimite{Do: dictionary.LimiteVariantesDoTermo, Maximo: 10}, "/api/termos", map[string]any{"termo": "RAG"}, 400,
			"Cada termo leva até 10 grafias erradas."},
		{"sem espaço nenhum", &dictionary.ErroSemEspaco{Sobram: 0}, "/api/termos", map[string]any{"termo": "RAG"}, 409,
			"Não cabe nas dicas: não sobra nenhum caractere."},
		{"sem espaço, 1", &dictionary.ErroSemEspaco{Sobram: 1}, "/api/termos", map[string]any{"termo": "RAG"}, 409,
			"Não cabe nas dicas: sobra 1 caractere."},
		{"sem arquivo", dictionary.ErrSemArquivo, "/api/termos", map[string]any{"termo": "RAG"}, 503,
			"Não achei o dicionário do Agent, então não há onde adicionar."},
		{"arquivo inválido", dictionary.ErrArquivoInvalido, "/api/termos/remover", map[string]any{"termo": "RAG"}, 503,
			"O dicionário do Agent não abre. Nada foi gravado."},
		{"não carregaria", dictionary.ErrNaoCarrega, "/api/termos", map[string]any{"termo": "RAG"}, 500,
			"O dicionário com essa mudança não carregaria. Nada foi gravado."},
		{"defeito, com texto de dentro", errors.New("abrindo C:\\Users\\<usuario>\\RAG: negado"), "/api/termos", map[string]any{"termo": "RAG"}, 500,
			"Deu um erro dentro do Agent. Tente de novo."},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			f := novoTermosFalso()
			f.err = c.err
			a := novoAmbienteDeTermos(t, f)
			w := fazer(t, a.h, http.MethodPost, c.rota, c.corpo)
			exigirStatus(t, w, c.status)
			if got := decodeJSON[erroView](t, w).Error.Message; got != c.frase {
				t.Errorf("frase = %q, quero %q", got, c.frase)
			}
		})
	}
	// A lista que não carrega não vira lista vazia.
	f := novoTermosFalso()
	f.err = dictionary.ErrSemArquivo
	w := fazer(t, novoAmbienteDeTermos(t, f).h, http.MethodGet, "/api/termos", nil)
	exigirStatus(t, w, http.StatusServiceUnavailable)
}

func TestTermos_GravadoSemTrocaRespondeSucessoComAMarca(t *testing.T) {
	f := novoTermosFalso()
	f.gravadoSemTroca = true
	a := novoAmbienteDeTermos(t, f)
	w := a.adicionar(t, map[string]any{"termo": "RAG", "dica": true})
	exigirStatus(t, w, http.StatusOK)
	v := decodeJSON[termosView](t, w)
	if !v.ValeAoReiniciar || v.Termo != "RAG" || len(*v.Items) != 1 {
		t.Errorf("resposta = %s", w.Body.String())
	}
}

func TestTermos_SemTermosMontado(t *testing.T) {
	a := novoAmbienteDeTermos(t, nil)
	for _, r := range []struct{ metodo, rota string }{
		{http.MethodGet, "/api/termos"},
		{http.MethodPost, "/api/termos"},
		{http.MethodPost, "/api/termos/dica"},
		{http.MethodPost, "/api/termos/remover"},
	} {
		w := fazer(t, a.h, r.metodo, r.rota, map[string]any{"termo": "RAG"})
		exigirStatus(t, w, http.StatusServiceUnavailable)
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q", r.metodo, r.rota, got)
		}
	}
}

func TestTermos_CorpoRuim(t *testing.T) {
	a := novoAmbienteDeTermos(t, novoTermosFalso())
	for _, rota := range []string{"/api/termos", "/api/termos/dica", "/api/termos/remover"} {
		exigirStatus(t, fazer(t, a.h, http.MethodPost, rota, "não é json"), http.StatusBadRequest)
		exigirStatus(t, fazer(t, a.h, http.MethodPost, rota, map[string]any{}), http.StatusBadRequest)
		// Acima de 16 KiB.
		grande := `{"termo":"RAG","folga":"` + strings.Repeat("a", 16<<10) + `"}`
		exigirStatus(t, fazer(t, a.h, http.MethodPost, rota, grande), http.StatusBadRequest)
	}
	// O texto não vai pela URL: não há rota que o leia de lá.
	exigirStatus(t, fazer(t, a.h, http.MethodGet, "/api/termos/dica?termo=RAG", nil), http.StatusMethodNotAllowed)
	if a.f.mutacoes != 0 {
		t.Errorf("%d mutações com corpo ruim", a.f.mutacoes)
	}
}

func TestTermos_NaoCacheiam(t *testing.T) {
	a := novoAmbienteDeTermos(t, novoTermosFalso())
	for _, r := range []struct {
		metodo, rota string
		corpo        any
		status       int
	}{
		{http.MethodGet, "/api/termos", nil, 200},
		{http.MethodPost, "/api/termos", map[string]any{"termo": "RAG", "dica": true}, 200},
		{http.MethodPost, "/api/termos", map[string]any{"termo": "das"}, 400},
		{http.MethodPost, "/api/termos/dica", map[string]any{"termo": "RAG", "dica": false}, 200},
		{http.MethodPost, "/api/termos/remover", map[string]any{"termo": "RAG"}, 200},
		{http.MethodPost, "/api/termos/remover", map[string]any{"termo": "RAG"}, 404},
	} {
		w := fazer(t, a.h, r.metodo, r.rota, r.corpo)
		exigirStatus(t, w, r.status)
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s (%d): Cache-Control = %q", r.metodo, r.rota, r.status, got)
		}
	}
}

// O log só tem contagens e a classe do erro: nem o termo, nem as grafias, nem
// o termo de quem a variante já é, em sucesso ou em recusa.
func TestTermos_LogSemTexto(t *testing.T) {
	f := novoTermosFalso()
	a := novoAmbienteDeTermos(t, f)
	a.adicionar(t, map[string]any{"termo": "Kubrique", "variantes": []string{"kubriqui", "cubrique"}, "dica": true})
	a.adicionar(t, map[string]any{"termo": "Kubrique", "variantes": []string{"SF"}})
	a.adicionar(t, map[string]any{"termo": "Kubrique", "variantes": []string{"das"}})
	fazer(t, a.h, http.MethodPost, "/api/termos/dica", map[string]any{"termo": "Kubrique", "dica": false})
	fazer(t, a.h, http.MethodPost, "/api/termos/remover", map[string]any{"termo": "Kubrique"})
	fazer(t, a.h, http.MethodPost, "/api/termos/remover", map[string]any{"termo": "Kubrique"})
	f.err = &dictionary.ErroQuebraria{Variante: "Zebedeu", Termo: "Tarcisio"}
	a.adicionar(t, map[string]any{"termo": "Kubrique"})

	log := a.log.String()
	for _, texto := range []string{"Kubrique", "kubriqui", "cubrique", "SF", "ECF", "das", "Zebedeu", "Tarcisio"} {
		if strings.Contains(log, texto) {
			t.Errorf("o log traz %q:\n%s", texto, log)
		}
	}
	for _, quer := range []string{"termos: adicionado", "termos: dica", "termos: removido", "motivo=variante_de_outro_termo",
		"motivo=palavra_comum", "motivo=nao_adicionado", "motivo=quebraria"} {
		if !strings.Contains(log, quer) {
			t.Errorf("o log não tem %q:\n%s", quer, log)
		}
	}
}
