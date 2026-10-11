package localapi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Nada aqui abre arquivo, digita ou mexe na área de transferência: o
// aprendizado é um dublê em memória (o que ele faz com a semente é a lógica
// pura de dictionary.Semente, a mesma de produção), e o repositório do
// histórico é o de memória.

// aprendizadoFalso faz o papel do aprendizadoDoAgent: mantém a Semente em
// memória e conta cada chamada, para os testes provarem o que foi (e o que
// NÃO foi) gravado.
type aprendizadoFalso struct {
	mu  sync.Mutex
	dic dictionary.Semente

	dicErr       error // o que Dicionario devolve, se não nil
	aprenderErr  error // o que Aprender devolve, sem gravar, se não nil
	aprendidaErr error // o que Aprendidas devolve, se não nil
	// gravadoSemTroca: Aprender e Desfazer gravam de verdade (na semente em
	// memória) e devolvem, junto com o que gravaram, dictionary.ErrGravadoSemTroca.
	gravadoSemTroca bool

	nDicionario, nAprender, nDesfazer int
	vistos                            [][2]string // (termo, variante) de cada Aprender
}

func dicDeTeste() dictionary.Semente {
	return dictionary.Semente{
		Usuario: "mauro",
		Entradas: []dictionary.EntradaSemente{
			{Canonica: "ECF", Prioridade: 90},
			{Canonica: "ECD", Prioridade: 90},
		},
	}
}

func novoFalso() *aprendizadoFalso { return &aprendizadoFalso{dic: dicDeTeste()} }

func (f *aprendizadoFalso) Dicionario(context.Context) (dictionary.Semente, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nDicionario++
	if f.dicErr != nil {
		return dictionary.Semente{}, f.dicErr
	}
	return f.dic, nil
}

func (f *aprendizadoFalso) Aprender(_ context.Context, termo, variante string) (dictionary.Aprendida, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nAprender++
	f.vistos = append(f.vistos, [2]string{termo, variante})
	if f.aprenderErr != nil {
		return dictionary.Aprendida{}, f.aprenderErr
	}
	novo, feito, err := f.dic.AprenderVariante(termo, variante)
	if err != nil {
		return dictionary.Aprendida{}, err
	}
	f.dic = novo
	if f.gravadoSemTroca {
		return feito, dictionary.ErrGravadoSemTroca
	}
	return feito, nil
}

func (f *aprendizadoFalso) Desfazer(_ context.Context, variante string) (dictionary.Aprendida, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nDesfazer++
	novo, feito, err := f.dic.DesfazerVariante(variante)
	if err != nil {
		return dictionary.Aprendida{}, err
	}
	f.dic = novo
	if f.gravadoSemTroca {
		return feito, dictionary.ErrGravadoSemTroca
	}
	return feito, nil
}

func (f *aprendizadoFalso) Aprendidas(context.Context) ([]dictionary.Aprendida, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.aprendidaErr != nil {
		return nil, f.aprendidaErr
	}
	return f.dic.VariantesDoPainel(), nil
}

// gravacoes é o total de Aprender e Desfazer: o que mexeria no dicionário.
func (f *aprendizadoFalso) gravacoes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nAprender + f.nDesfazer
}

func (f *aprendizadoFalso) consultas() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nDicionario
}

// semente devolve uma cópia profunda do dicionário de agora, para comparar
// antes e depois.
func (f *aprendizadoFalso) semente(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return fmt.Sprintf("%#v", f.dic)
}

// Os textos do caso principal: o ASR escreveu "sf", o Smart e o Polish
// deixaram como estava, e a pessoa corrigiu para ECF.
const (
	brutoSF  = "vou mandar o sf amanhã"
	antesSF  = "Vou mandar o sf amanhã."
	depoisSF = "Vou mandar o ECF amanhã."
)

type ambiente struct {
	srv  *localapi.Server
	h    http.Handler
	repo *storage.InMemoryTranscriptionRepository
	ap   *aprendizadoFalso
	log  *bytes.Buffer
}

// novoAmbiente monta o servidor com o aprendizado falso, com o log num
// buffer, e semeia o ditado t1 com os textos de brutoSF.
func novoAmbiente(t *testing.T, ap *aprendizadoFalso) *ambiente {
	t.Helper()
	repo := storage.NewInMemoryTranscriptionRepository()
	buf := &bytes.Buffer{}
	pref := prefsDeTeste()
	if ap != nil {
		pref.Aprendizado = ap
	}
	pref.Log = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := localapi.New(testUserID, repo, storage.NewInMemorySettingsRepository(), pref)
	seedTranscription(repo, "t1", time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), func(tr *storage.Transcription) {
		tr.RawText, tr.FinalText = brutoSF, antesSF
	})
	return &ambiente{srv: srv, h: srv.Handler(), repo: repo, ap: ap, log: buf}
}

type sugestaoView struct {
	De             string `json:"de"`
	Para           string `json:"para"`
	TermoExistente string `json:"termo_existente"`
	Ocorrencias    int    `json:"ocorrencias"`
}

type editadoView struct {
	ID        string          `json:"id"`
	FinalText string          `json:"final_text"`
	Version   int64           `json:"version"`
	Sugestoes *[]sugestaoView `json:"sugestoes"`
}

type aprendidaView struct {
	Termo    string `json:"termo"`
	Variante string `json:"variante"`
}

type aprendidasView struct {
	Items *[]aprendidaView `json:"items"`
}

// salvar faz o PATCH e devolve a resposta.
func (a *ambiente) salvar(t *testing.T, id string, base int64, final string) *httptest.ResponseRecorder {
	t.Helper()
	return fazer(t, a.h, http.MethodPatch, "/api/history/"+id, map[string]any{"base_version": base, "final_text": final})
}

// sugestoesDe exige 200 e devolve a lista "sugestoes", que tem de existir.
func sugestoesDe(t *testing.T, w *httptest.ResponseRecorder) []sugestaoView {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: status = %d, body = %s", w.Code, w.Body.String())
	}
	v := decodeJSON[editadoView](t, w)
	if v.Sugestoes == nil {
		t.Fatalf("a resposta não traz o campo sugestoes: %s", w.Body.String())
	}
	return *v.Sugestoes
}

func (a *ambiente) aprender(t *testing.T, id, de, para string) *httptest.ResponseRecorder {
	t.Helper()
	return fazer(t, a.h, http.MethodPost, "/api/aprendizado", map[string]string{"id": id, "de": de, "para": para})
}

func (a *ambiente) desfazer(t *testing.T, variante string) *httptest.ResponseRecorder {
	t.Helper()
	return fazer(t, a.h, http.MethodPost, "/api/aprendizado/desfazer", map[string]string{"variante": variante})
}

func (a *ambiente) listar(t *testing.T) []aprendidaView {
	t.Helper()
	w := fazer(t, a.h, http.MethodGet, "/api/aprendizado", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/aprendizado: status = %d, body = %s", w.Code, w.Body.String())
	}
	v := decodeJSON[aprendidasView](t, w)
	if v.Items == nil {
		t.Fatalf("a lista não pode ser null: %s", w.Body.String())
	}
	return *v.Items
}

func exigirStatus(t *testing.T, w *httptest.ResponseRecorder, quer int) {
	t.Helper()
	if w.Code != quer {
		t.Fatalf("status = %d, quer %d; body = %s", w.Code, quer, w.Body.String())
	}
}

// AC-022: salvar a edição devolve a sugestão e NÃO mexe no dicionário. Nem
// uma chamada de gravação, e a semente fica como estava.
func TestAC022_SalvarSugereENaoGravaNoDicionario(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	antes := a.ap.semente(t)

	w := a.salvar(t, "t1", 1, depoisSF)
	sug := sugestoesDe(t, w)

	want := []sugestaoView{{De: "sf", Para: "ECF", TermoExistente: "ECF", Ocorrencias: 0}}
	if !reflect.DeepEqual(sug, want) {
		t.Fatalf("sugestoes = %+v, quer %+v", sug, want)
	}
	if got := decodeJSON[editadoView](t, w); got.FinalText != depoisSF || got.Version != 2 {
		t.Errorf("o item devolvido não é o gravado: %+v", got)
	}
	if n := a.ap.gravacoes(); n != 0 {
		t.Errorf("salvar chamou Aprender/Desfazer %d vez(es): só o clique grava", n)
	}
	if got := a.ap.semente(t); got != antes {
		t.Error("salvar mudou o dicionário")
	}
	if l := a.listar(t); len(l) != 0 {
		t.Errorf("salvar deixou algo em 'aprendido no painel': %+v", l)
	}
	// E a edição foi gravada de verdade.
	if g, err := a.repo.Get(context.Background(), testUserID, "t1"); err != nil || g.FinalText != depoisSF {
		t.Errorf("final_text gravado = %q (%v)", g.FinalText, err)
	}
}

// Sem nada que sugerir, o campo vem, vazio: a tela não precisa tratar
// ausência.
func TestSalvarSemTrocaDeTermoDevolveListaVazia(t *testing.T) {
	for nome, final := range map[string]string{
		"só pontuação":       "Vou mandar o sf amanhã!",
		"só maiúscula":       "vou mandar o SF amanhã.",
		"reescreveu tudo":    "Preciso de outra coisa completamente diferente hoje à noite.",
		"mesmo texto":        antesSF,
		"só acrescentou":     "Vou mandar o sf amanhã, sem falta.",
		"com um número":      "Vou mandar o 4 amanhã.",
		"palavra de ligação": "Vou mandar do sf amanhã.",
	} {
		t.Run(nome, func(t *testing.T) {
			a := novoAmbiente(t, novoFalso())
			if got := sugestoesDe(t, a.salvar(t, "t1", 1, final)); len(got) != 0 {
				t.Errorf("sugeriu %+v", got)
			}
			if n := a.ap.gravacoes(); n != 0 {
				t.Errorf("gravou %d vez(es)", n)
			}
		})
	}
}

// Até 2 sugestões, na ordem do texto.
func TestSalvarComDuasTrocasSugereAsDuas(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	a.repo.Put(storage.Transcription{
		ID: "t2", UserID: testUserID, SourcePlatform: "windows", RawText: "o sf e o rj",
		FinalText: "O sf e o rj.", Mode: "smart", ASRProvider: "local", Status: "completed",
		CreatedAt: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Now(), Version: 1,
	})
	got := sugestoesDe(t, a.salvar(t, "t2", 1, "O ECF e o RF."))
	// O "sf" também está no bruto de t1 (o outro ditado semeado): aparece em 1.
	want := []sugestaoView{
		{De: "sf", Para: "ECF", TermoExistente: "ECF", Ocorrencias: 1},
		{De: "rj", Para: "RF", TermoExistente: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sugestoes = %+v, quer %+v", got, want)
	}
	// Um clique por troca, as duas valem.
	for _, s := range want {
		exigirStatus(t, a.aprender(t, "t2", s.De, s.Para), http.StatusOK)
	}
	if l := a.listar(t); len(l) != 2 {
		t.Errorf("aprendidas = %+v, quero 2", l)
	}
}

// Conflito de versão: 409, e SEM sugestão. O motor nem é consultado, e nada
// é gravado.
func TestPatchComVersaoVelhaDa409SemSugestao(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	exigirStatus(t, a.salvar(t, "t1", 1, "Vou mandar o sf amanhã, ok."), http.StatusOK) // v2

	w := a.salvar(t, "t1", 1, depoisSF) // base velha
	exigirStatus(t, w, http.StatusConflict)
	if strings.Contains(w.Body.String(), "sugestoes") {
		t.Errorf("o 409 trouxe sugestão: %s", w.Body.String())
	}
	if g, _ := a.repo.Get(context.Background(), testUserID, "t1"); g.FinalText == depoisSF {
		t.Error("o 409 gravou o texto")
	}
	if n := a.ap.consultas(); n != 1 { // só a do primeiro PATCH, que foi 200
		t.Errorf("o dicionário foi consultado %d vezes; o 409 não pode consultar", n)
	}
	// E um clique com o par que o 409 "teria" sugerido não passa.
	exigirStatus(t, a.aprender(t, "t1", "sf", "ECF"), http.StatusConflict)
	if n := a.ap.gravacoes(); n != 0 {
		t.Errorf("gravou %d vez(es)", n)
	}
}

// Ditado que não existe: 404, como antes de a etapa 3.
func TestPatchDeDitadoInexistenteDa404(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	exigirStatus(t, a.salvar(t, "nao-existe", 1, "x"), http.StatusNotFound)
}

// A contagem "aparece em N ditados": só os outros ditados ativos do dono,
// como palavra inteira, no texto bruto.
func TestOcorrenciasContamOsOutrosDitadosAtivosDoDono(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	outro := func(id, bruto string, mut func(*storage.Transcription)) {
		seedTranscription(a.repo, id, base, func(tr *storage.Transcription) {
			tr.RawText, tr.FinalText = bruto, bruto
			if mut != nil {
				mut(tr)
			}
		})
	}
	outro("o1", "mandei o SF ontem", nil)                          // conta
	outro("o2", "o sf travou", nil)                                // conta
	outro("o3", "sf, de novo", nil)                                // conta (pontuação)
	outro("o4", "a sfera girou", nil)                              // palavra inteira: não
	outro("o5", "nada a ver", nil)                                 // não
	outro("o6", "sf na lixeira", func(tr *storage.Transcription) { // lixeira: não
		d := base
		tr.DeletedAt = &d
	})
	outro("o7", "sf de outro dono", func(tr *storage.Transcription) { tr.UserID = "outro-usuario" }) // não

	got := sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))
	if len(got) != 1 || got[0].Ocorrencias != 3 {
		t.Fatalf("sugestoes = %+v, quero 1 com ocorrencias = 3 (o editado, a lixeira e outro dono não contam)", got)
	}
}

// A contagem olha os últimos 500 ditados, e atravessa páginas do histórico.
func TestOcorrenciasOlhamSoOsUltimos500(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	// Do mais velho (0) ao mais novo (599). A janela são os 500 mais novos
	// (índices 100 a 599) e o histórico vem em páginas de 100: 599 está na
	// primeira página, 450 na segunda, 300 na quarta e 100 na quinta (a
	// última posição da janela); 99 e 0 ficam de fora.
	comSF := map[int]bool{599: true, 450: true, 300: true, 100: true, 99: true, 0: true}
	for i := 0; i < 600; i++ {
		bruto := "ditado comum"
		if comSF[i] {
			bruto = "falei sf aqui"
		}
		seedTranscription(a.repo, fmt.Sprintf("m%03d", i), base.Add(time.Duration(i)*time.Minute), func(tr *storage.Transcription) {
			tr.RawText, tr.FinalText = bruto, bruto
		})
	}
	got := sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))
	// t1 é de 2026-09-01: mais novo que os 600, e sai da conta.
	if len(got) != 1 || got[0].Ocorrencias != 4 {
		t.Fatalf("ocorrencias = %+v, quero 4 (os 2 mais velhos ficam fora da janela de 500, e os 4 de dentro estão em páginas diferentes)", got)
	}
}

// O ciclo: sugere, aprende com o clique, lista, desfaz.
func TestAprenderListarDesfazer(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	if l := a.listar(t); len(l) != 0 {
		t.Fatalf("a lista começa vazia, veio %+v", l)
	}
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))

	w := a.aprender(t, "t1", "sf", "ECF")
	exigirStatus(t, w, http.StatusOK)
	if got := decodeJSON[aprendidaView](t, w); got != (aprendidaView{Termo: "ECF", Variante: "sf"}) {
		t.Errorf("aprendida = %+v", got)
	}
	if !reflect.DeepEqual(a.ap.vistos, [][2]string{{"ECF", "sf"}}) {
		t.Errorf("Aprender recebeu %v, quero (termo=ECF, variante=sf)", a.ap.vistos)
	}
	if l := a.listar(t); !reflect.DeepEqual(l, []aprendidaView{{Termo: "ECF", Variante: "sf"}}) {
		t.Errorf("lista = %+v", l)
	}

	w = a.desfazer(t, "sf")
	exigirStatus(t, w, http.StatusOK)
	if got := decodeJSON[aprendidaView](t, w); got != (aprendidaView{Termo: "ECF", Variante: "sf"}) {
		t.Errorf("desfeita = %+v", got)
	}
	if l := a.listar(t); len(l) != 0 {
		t.Errorf("depois de desfazer a lista tem %+v", l)
	}
	// Desfazer o que não está aprendido: 404, e com a frase da tela.
	w = a.desfazer(t, "sf")
	exigirStatus(t, w, http.StatusNotFound)
	if e := decodeJSON[erroView](t, w); !strings.Contains(e.Error.Message, "sf") {
		t.Errorf("mensagem = %q", e.Error.Message)
	}
}

// O servidor refaz a checagem do "de": ele tem de estar no texto BRUTO
// daquele ditado. Vale mesmo para um par que o servidor nunca sugeriu, e
// nada é gravado.
func TestAprenderRecusaDeQueNaoEstaNoBruto(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))

	for nome, de := range map[string]string{
		"fora do bruto":            "xyz",
		"só no texto final":        "amanhã, ok", // não contíguo no bruto
		"pedaço de palavra":        "s",
		"a forma do texto final":   "ECF",
		"outra palavra do ditado?": "sf amanhã hoje",
	} {
		t.Run(nome, func(t *testing.T) {
			w := a.aprender(t, "t1", de, "ECF")
			exigirStatus(t, w, http.StatusBadRequest)
			if e := decodeJSON[erroView](t, w); e.Error.Code != "BAD_REQUEST" || e.Error.Message == "" {
				t.Errorf("erro = %+v", e)
			}
		})
	}
	if n := a.ap.gravacoes(); n != 0 {
		t.Errorf("gravou %d vez(es)", n)
	}
}

// Além de estar no bruto, o par tem de ser uma sugestão que o servidor
// devolveu para esta versão do ditado.
func TestAprenderExigeUmaSugestaoDoServidor(t *testing.T) {
	a := novoAmbiente(t, novoFalso())

	// Nenhuma edição foi salva: nada foi sugerido.
	exigirStatus(t, a.aprender(t, "t1", "sf", "ECF"), http.StatusConflict)

	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF)) // v2: sugere sf → ECF
	for nome, p := range map[string][2]string{
		"outro para":                  {"sf", "ECD"},
		"outro de, que está no bruto": {"mandar", "ECF"},
		"par que ninguém sugeriu":     {"amanhã", "ECD"},
	} {
		t.Run(nome, func(t *testing.T) {
			w := a.aprender(t, "t1", p[0], p[1])
			exigirStatus(t, w, http.StatusConflict)
			if e := decodeJSON[erroView](t, w); e.Error.Code != "CONFLICT" {
				t.Errorf("erro = %+v", e)
			}
		})
	}
	if n := a.ap.gravacoes(); n != 0 {
		t.Errorf("gravou %d vez(es)", n)
	}

	// Com espaço nas pontas, o par sugerido passa (a tela pode aparar).
	exigirStatus(t, a.aprender(t, "t1", " sf ", " ECF "), http.StatusOK)
}

// Salvar de novo invalida a sugestão da edição anterior: a versão do ditado
// mudou, e o texto de antes também.
func TestSugestaoVenceQuandoOTextoMuda(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))                                             // v2
	if got := sugestoesDe(t, a.salvar(t, "t1", 2, "Vou mandar o sf amanhã.")); len(got) != 0 { // v3, sem troca de termo
		t.Fatalf("sugeriu %+v", got)
	}
	exigirStatus(t, a.aprender(t, "t1", "sf", "ECF"), http.StatusConflict)
	if n := a.ap.gravacoes(); n != 0 {
		t.Errorf("gravou %d vez(es)", n)
	}
}

// A versão do ditado é o que amarra o par à edição: se o texto mudou por
// fora da API (outro escritor do histórico), o clique não vale, mesmo com o
// par intacto na memória do servidor.
func TestSugestaoVenceQuandoOTextoMudaPorForaDaAPI(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF)) // v2
	if _, err := a.repo.UpdateFinalText(context.Background(), testUserID, "t1", 2, "Vou mandar o ECF depois."); err != nil {
		t.Fatal(err)
	}
	exigirStatus(t, a.aprender(t, "t1", "sf", "ECF"), http.StatusConflict)
	if n := a.ap.gravacoes(); n != 0 {
		t.Errorf("gravou %d vez(es)", n)
	}
}

// Ditado que não existe, ou de outro dono: 404 (o Get é por dono).
func TestAprenderDeDitadoQueNaoExiste(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	exigirStatus(t, a.aprender(t, "nao-existe", "sf", "ECF"), http.StatusNotFound)
	seedTranscription(a.repo, "do-outro", time.Now(), func(tr *storage.Transcription) {
		tr.UserID, tr.RawText = "outro-usuario", brutoSF
	})
	exigirStatus(t, a.aprender(t, "do-outro", "sf", "ECF"), http.StatusNotFound)
}

func TestAprenderComCorpoRuim(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	for nome, corpo := range map[string]any{
		"não é JSON":   "{isto não",
		"sem id":       map[string]string{"de": "sf", "para": "ECF"},
		"sem de":       map[string]string{"id": "t1", "para": "ECF"},
		"sem para":     map[string]string{"id": "t1", "de": "sf"},
		"só espaços":   map[string]string{"id": "t1", "de": "  ", "para": "ECF"},
		"corpo enorme": `{"id":"t1","de":"` + strings.Repeat("a", 40<<10) + `","para":"ECF"}`,
	} {
		t.Run(nome, func(t *testing.T) {
			exigirStatus(t, fazer(t, a.h, http.MethodPost, "/api/aprendizado", corpo), http.StatusBadRequest)
		})
	}
	exigirStatus(t, fazer(t, a.h, http.MethodPost, "/api/aprendizado/desfazer", "{isto não"), http.StatusBadRequest)
	exigirStatus(t, a.desfazer(t, "  "), http.StatusBadRequest)
	if n := a.ap.gravacoes(); n != 0 {
		t.Errorf("gravou %d vez(es)", n)
	}
}

// As recusas do dicionário viram a frase da tela, com o status certo. O que
// é defeito não vaza o texto do erro.
func TestAprenderTraduzOsErrosDoDicionario(t *testing.T) {
	const canario = "CANARIO-DO-ERRO-INTERNO"
	casos := []struct {
		nome   string
		erro   error
		status int
		code   string
		frase  []string // trechos que a mensagem tem de ter
	}{
		{"variante de outro termo", &dictionary.ErroVarianteDeOutroTermo{Dono: "ECD"},
			http.StatusConflict, "CONFLICT", []string{`"sf"`, `"ECD"`}},
		{"variante que é um termo", &dictionary.ErroVarianteDeOutroTermo{Dono: "sf", ETermo: true},
			http.StatusConflict, "CONFLICT", []string{`"sf"`, "termo"}},
		{"termo que é variante de outro", &dictionary.ErroTermoEVarianteDeOutro{Dono: "ECD"},
			http.StatusConflict, "CONFLICT", []string{`"ECF"`, `"ECD"`}},
		{"já existe", dictionary.ErrJaExiste, http.StatusConflict, "CONFLICT", []string{`"sf"`, `"ECF"`}},
		{"termo inativo", dictionary.ErrTermoInativo, http.StatusConflict, "CONFLICT", []string{`"ECF"`, "desligado"}},
		{"não agiria", dictionary.ErrNaoAgiria, http.StatusConflict, "CONFLICT", []string{`"sf"`, "Nada foi gravado"}},
		{"sem novidade", dictionary.ErrSemNovidade, http.StatusBadRequest, "BAD_REQUEST", []string{"maiúscula, acento"}},
		{"vazio", dictionary.ErrVazio, http.StatusBadRequest, "BAD_REQUEST", []string{"Faltou"}},
		{"inválido", dictionary.ErrInvalido, http.StatusBadRequest, "BAD_REQUEST", []string{"grande demais"}},
		{"sem arquivo", dictionary.ErrSemArquivo, http.StatusServiceUnavailable, "UNAVAILABLE", []string{"dicionário"}},
		{"arquivo inválido", fmt.Errorf("%w: %s", dictionary.ErrArquivoInvalido, canario),
			http.StatusServiceUnavailable, "UNAVAILABLE", []string{"Nada foi gravado"}},
		{"outro dono", dictionary.ErrUsuarioDiferente, http.StatusServiceUnavailable, "UNAVAILABLE", []string{"Reinicie"}},
		{"não carrega", fmt.Errorf("%w: %s", dictionary.ErrNaoCarrega, canario),
			http.StatusInternalServerError, "INTERNAL", []string{"Nada foi gravado"}},
		{"defeito qualquer", errors.New("falha com " + canario),
			http.StatusInternalServerError, "INTERNAL", []string{"Tente de novo"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso()
			a := novoAmbiente(t, f)
			sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))
			f.aprenderErr = c.erro

			w := a.aprender(t, "t1", "sf", "ECF")
			exigirStatus(t, w, c.status)
			e := decodeJSON[erroView](t, w)
			if e.Error.Code != c.code {
				t.Errorf("code = %q, quer %q", e.Error.Code, c.code)
			}
			for _, trecho := range c.frase {
				if !strings.Contains(e.Error.Message, trecho) {
					t.Errorf("a mensagem %q não tem %q", e.Error.Message, trecho)
				}
			}
			if strings.Contains(w.Body.String(), canario) || strings.Contains(a.log.String(), canario) {
				t.Errorf("o texto do erro interno vazou (resposta ou log)")
			}
		})
	}
}

// "SF já vira ECD": o dicionário mudou entre a sugestão e o clique (o
// dictlearn, ou outra aba, ensinou SF a outro termo). A recusa é a REAL do
// dicionário, e a frase cita o dono, que é o que a tela precisa mostrar.
func TestAprenderVarianteQueVirouDeOutroTermo(t *testing.T) {
	f := novoFalso()
	a := novoAmbiente(t, f)
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))

	novo, _, err := f.dic.AprenderVariante("ECD", "sf") // outro termo ganha o "sf" no meio
	if err != nil {
		t.Fatal(err)
	}
	f.dic = novo
	antes := f.semente(t)

	w := a.aprender(t, "t1", "sf", "ECF")
	exigirStatus(t, w, http.StatusConflict)
	if e := decodeJSON[erroView](t, w); !strings.Contains(e.Error.Message, `"sf"`) || !strings.Contains(e.Error.Message, `"ECD"`) {
		t.Errorf("a mensagem não diz que SF já vira ECD: %q", e.Error.Message)
	}
	if f.semente(t) != antes {
		t.Error("a recusa mexeu no dicionário")
	}
}

// Sem dicionário (ErrSemArquivo) ou com ele quebrado, a edição é gravada
// como sempre e sai sem sugestão: não se oferece um botão que não funciona.
func TestSalvarSemDicionarioNaoSugere(t *testing.T) {
	for nome, erro := range map[string]error{
		"sem arquivo":      dictionary.ErrSemArquivo,
		"arquivo inválido": dictionary.ErrArquivoInvalido,
		"outro dono":       dictionary.ErrUsuarioDiferente,
	} {
		t.Run(nome, func(t *testing.T) {
			f := novoFalso()
			f.dicErr = erro
			a := novoAmbiente(t, f)
			if got := sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF)); len(got) != 0 {
				t.Errorf("sugeriu %+v", got)
			}
			if g, _ := a.repo.Get(context.Background(), testUserID, "t1"); g.FinalText != depoisSF {
				t.Error("a edição não foi gravada")
			}
		})
	}
}

// O Agent sem o aprendizado montado: salvar segue como antes (com a lista
// vazia), a lista é vazia e aprender/desfazer dizem 503.
func TestSemAprendizadoMontado(t *testing.T) {
	a := novoAmbiente(t, nil)
	if got := sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF)); len(got) != 0 {
		t.Errorf("sugeriu %+v", got)
	}
	if l := a.listar(t); len(l) != 0 {
		t.Errorf("lista = %+v", l)
	}
	exigirStatus(t, a.aprender(t, "t1", "sf", "ECF"), http.StatusServiceUnavailable)
	exigirStatus(t, a.desfazer(t, "sf"), http.StatusServiceUnavailable)
}

// repoComGetAtrasado devolve, no Get, o ditado como estava uma versão atrás:
// o que aconteceria se alguém gravasse entre a leitura do texto de antes e a
// gravação da edição. O texto de antes deixa de ser o que a edição trocou.
type repoComGetAtrasado struct {
	storage.TranscriptionRepository
}

func (r repoComGetAtrasado) Get(ctx context.Context, user, id string) (storage.Transcription, error) {
	t, err := r.TranscriptionRepository.Get(ctx, user, id)
	t.Version--
	t.FinalText = "Vou mandar o sfera amanhã."
	return t, err
}

func TestSalvarComOTextoDeAntesQueMudouNoMeioNaoSugere(t *testing.T) {
	f := novoFalso()
	repo := storage.NewInMemoryTranscriptionRepository()
	pref := prefsDeTeste()
	pref.Aprendizado = f
	srv := localapi.New(testUserID, repoComGetAtrasado{repo}, storage.NewInMemorySettingsRepository(), pref)
	seedTranscription(repo, "t1", time.Now(), func(tr *storage.Transcription) {
		tr.RawText, tr.FinalText, tr.Version = brutoSF, antesSF, 2
	})
	a := &ambiente{srv: srv, h: srv.Handler(), repo: repo, ap: f}

	got := sugestoesDe(t, a.salvar(t, "t1", 2, depoisSF))
	if len(got) != 0 {
		t.Errorf("sugeriu com um texto de antes que não é o que a edição trocou: %+v", got)
	}
	if f.consultas() != 0 {
		t.Error("consultou o dicionário")
	}
}

// Nenhum texto em log: só contagens e códigos fechados. O canário é o que a
// pessoa ditou e o que ela corrigiu; nenhum pedaço dele pode ir ao log, nem
// quando o aprendizado é recusado.
func TestLogDoAprendizadoSoTemContagens(t *testing.T) {
	const (
		brutoCanario  = "enviei o xqzcanarioalfa ontem"
		antesCanario  = "Enviei o xqzcanarioalfa ontem."
		depoisCanario = "Enviei o XqzCanarioBeta ontem."
	)
	canarios := []string{"xqzcanarioalfa", "xqzcanariobeta", "enviei"}

	f := novoFalso()
	a := novoAmbiente(t, f)
	seedTranscription(a.repo, "c1", time.Now(), func(tr *storage.Transcription) {
		tr.RawText, tr.FinalText = brutoCanario, antesCanario
	})
	seedTranscription(a.repo, "c2", time.Now(), func(tr *storage.Transcription) {
		tr.RawText, tr.FinalText = "falei xqzcanarioalfa de novo", "falei xqzcanarioalfa de novo"
	})

	got := sugestoesDe(t, a.salvar(t, "c1", 1, depoisCanario))
	if len(got) != 1 || got[0].Ocorrencias != 1 {
		t.Fatalf("o caso não exercitou a sugestão e a contagem: %+v", got)
	}
	exigirStatus(t, a.aprender(t, "c1", got[0].De, got[0].Para), http.StatusOK)
	a.aprender(t, "c1", got[0].De, got[0].Para) // recusado: já existe
	a.aprender(t, "c1", "xyz", "ECF")           // recusado: fora do bruto
	a.listar(t)
	exigirStatus(t, a.desfazer(t, got[0].De), http.StatusOK)
	a.desfazer(t, "xqzcanarioalfa") // recusado: não está aprendido
	f.dicErr = dictionary.ErrSemArquivo
	a.salvar(t, "c1", 2, "Enviei o XqzCanarioBeta ontem!") // sem dicionário

	log := strings.ToLower(a.log.String())
	if !strings.Contains(log, "sugestoes=1") {
		t.Fatalf("o log não tem a contagem de sugestões: o teste não está vendo o que se registra:\n%s", log)
	}
	for _, c := range canarios {
		if strings.Contains(log, c) {
			t.Errorf("o log tem texto ditado (%q):\n%s", c, log)
		}
	}
}
