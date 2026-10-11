package localapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Terceira revisão da etapa 3 (30-set-2026): os pontos baixos da F3. Mesmo
// desenho do aprendizado_test.go: aprendizado em memória e histórico em
// memória, nada em disco, nada no sistema real.

const (
	fraseVencida = "Essa sugestão não vale mais; corrija o ditado de novo para aprender."
	fraseDefeito = "Deu um erro dentro do Agent. Tente de novo."
)

// Um termo desativado é dono das suas formas, como no dicionário: a sugestão
// que o dicionário sempre recusaria não aparece, e o botão Aprender não
// promete o que falha. Antes, o motor só via os termos ativos: o PATCH sugeria
// "sf" para ECF e o clique respondia 409 com "sf já vira Segredo", sendo que
// "sf" não vira nada, porque o termo está desligado.
func TestSalvarNaoSugereOQueUmTermoDesativadoJaTem(t *testing.T) {
	f := novoFalso()
	f.dic = dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{
		{Canonica: "ECF", Prioridade: 90},
		{Canonica: "Segredo", Alias: []string{"sf"}, Inativa: true},
	}}
	a := novoAmbiente(t, f)

	if got := sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF)); len(got) != 0 {
		t.Fatalf("sugeriu %+v, que o dicionário recusaria (o \"sf\" é de um termo desativado)", got)
	}
	// O controle: o dicionário recusa mesmo, e com a razão que o motor usou.
	if _, _, err := f.dic.AprenderVariante("ECF", "sf"); err == nil {
		t.Fatal("o controle não vale: o dicionário aceitaria a variante")
	}
	if !strings.Contains(a.log.String(), "termo_inativo") {
		t.Errorf("o log não tem o motivo do descarte:\n%s", a.log.String())
	}
}

// A frase do 409 da sugestão vencida tem de ser verdadeira: salvar de novo o
// MESMO texto não devolve a sugestão (antes e depois ficam iguais, e o motor
// não vê troca), e o que devolve é corrigir o ditado de novo.
func TestSugestaoVencidaDizOQueFunciona(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF)) // v2: sugere sf -> ECF
	// O ditado muda por fora da API (outra aba, a lixeira e a volta).
	if _, err := a.repo.UpdateFinalText(context.Background(), testUserID, "t1", 2, depoisSF); err != nil {
		t.Fatal(err)
	}

	w := a.aprender(t, "t1", "sf", "ECF")
	exigirStatus(t, w, http.StatusConflict)
	if e := decodeJSON[erroView](t, w); e.Error.Message != fraseVencida {
		t.Fatalf("mensagem = %q, quero %q", e.Error.Message, fraseVencida)
	}

	// Salvar de novo o mesmo texto NÃO resolve (o que a frase antiga mandava).
	if got := sugestoesDe(t, a.salvar(t, "t1", 3, depoisSF)); len(got) != 0 {
		t.Fatalf("salvar o mesmo texto devolveu %+v; a premissa da frase mudou", got)
	}
	exigirStatus(t, a.aprender(t, "t1", "sf", "ECF"), http.StatusConflict)

	// Corrigir de novo resolve: volta ao que o ASR escreveu e corrige outra vez.
	sugestoesDe(t, a.salvar(t, "t1", 4, antesSF))
	if got := sugestoesDe(t, a.salvar(t, "t1", 5, depoisSF)); len(got) != 1 {
		t.Fatalf("corrigir de novo devolveu %+v, quero a sugestão", got)
	}
	exigirStatus(t, a.aprender(t, "t1", "sf", "ECF"), http.StatusOK)
}

// repoQueFalhaNoGet deixa o PATCH passar e faz o Get do clique falhar, como um
// disco que some entre o salvar e o Aprender.
type repoQueFalhaNoGet struct {
	storage.TranscriptionRepository
	falhar *bool
}

func (r repoQueFalhaNoGet) Get(ctx context.Context, user, id string) (storage.Transcription, error) {
	if *r.falhar {
		return storage.Transcription{}, errors.New("disco sumiu: CANARIO-DO-ERRO-INTERNO")
	}
	return r.TranscriptionRepository.Get(ctx, user, id)
}

// O 404 e o 500 do clique vão para a pílula depois de "Não deu para aprender.":
// são frases inteiras, e não o "registro não encontrado" e o "erro interno"
// crus das outras rotas. O texto do erro interno não vaza.
func TestAprenderFalaPorExtensoQuandoONaoLeOuNaoAchaODitado(t *testing.T) {
	f := novoFalso()
	a := novoAmbiente(t, f)
	w := a.aprender(t, "nao-existe", "sf", "ECF")
	exigirStatus(t, w, http.StatusNotFound)
	e := decodeJSON[erroView](t, w)
	if e.Error.Code != "NOT_FOUND" || e.Error.Message != "Esse ditado não existe mais." {
		t.Errorf("404: erro = %+v", e.Error)
	}

	repo := storage.NewInMemoryTranscriptionRepository()
	falhar := false
	pref := prefsDeTeste()
	pref.Aprendizado = f
	srv := localapi.New(testUserID, repoQueFalhaNoGet{repo, &falhar}, storage.NewInMemorySettingsRepository(), pref)
	seedTranscription(repo, "t1", time.Now(), func(tr *storage.Transcription) { tr.RawText, tr.FinalText = brutoSF, antesSF })
	b := &ambiente{srv: srv, h: srv.Handler(), repo: repo, ap: f}
	falhar = true
	w = b.aprender(t, "t1", "sf", "ECF")
	exigirStatus(t, w, http.StatusInternalServerError)
	e = decodeJSON[erroView](t, w)
	if e.Error.Code != "INTERNAL" || e.Error.Message != fraseDefeito {
		t.Errorf("500: erro = %+v", e.Error)
	}
	if strings.Contains(w.Body.String(), "CANARIO") {
		t.Errorf("o texto do erro interno vazou: %s", w.Body.String())
	}
	if n := f.gravacoes(); n != 0 {
		t.Errorf("gravou %d vez(es)", n)
	}
}

// O arquivo foi gravado, mas o dicionário em memória não foi trocado: a
// resposta é de sucesso, com o que ficou gravado e a marca de que só vale ao
// reiniciar. A pílula não pode dizer que não aprendeu enquanto a lista das
// Configurações mostra o par. No caso comum, o campo nem aparece.
func TestAprenderGravadoSemTrocaRespondeSucessoComAMarca(t *testing.T) {
	type resposta struct {
		Termo           string `json:"termo"`
		Variante        string `json:"variante"`
		ValeAoReiniciar bool   `json:"vale_ao_reiniciar"`
	}
	f := novoFalso()
	f.gravadoSemTroca = true
	a := novoAmbiente(t, f)
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))

	w := a.aprender(t, "t1", "sf", "ECF")
	exigirStatus(t, w, http.StatusOK)
	if got := decodeJSON[resposta](t, w); got != (resposta{Termo: "ECF", Variante: "sf", ValeAoReiniciar: true}) {
		t.Errorf("aprender: %+v", got)
	}
	if l := a.listar(t); len(l) != 1 || l[0].Variante != "sf" {
		t.Errorf("a lista não tem o par gravado: %+v", l)
	}

	w = a.desfazer(t, "sf")
	exigirStatus(t, w, http.StatusOK)
	if got := decodeJSON[resposta](t, w); got != (resposta{Termo: "ECF", Variante: "sf", ValeAoReiniciar: true}) {
		t.Errorf("desfazer: %+v", got)
	}
	if l := a.listar(t); len(l) != 0 {
		t.Errorf("depois de desfazer a lista tem %+v", l)
	}
	if log := a.log.String(); strings.Count(log, "vale_ao_reiniciar=true") != 2 {
		t.Errorf("o log deveria marcar as duas operações (aprender e desfazer):\n%s", log)
	}

	// O caso comum: sem a marca na resposta.
	g := novoAmbiente(t, novoFalso())
	sugestoesDe(t, g.salvar(t, "t1", 1, depoisSF))
	w = g.aprender(t, "t1", "sf", "ECF")
	exigirStatus(t, w, http.StatusOK)
	if strings.Contains(w.Body.String(), "vale_ao_reiniciar") {
		t.Errorf("a resposta comum traz a marca: %s", w.Body.String())
	}
}

// As três rotas mandam Cache-Control: no-store (06-api-contract): a variante e
// o termo são texto ditado, e nenhuma cópia fica no cache do navegador. Vale
// também na recusa, que é onde o erro cita o termo.
func TestRotasDeAprendizadoNaoCacheiam(t *testing.T) {
	a := novoAmbiente(t, novoFalso())
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))

	rotas := []struct {
		nome   string
		status int
		fazer  func() *httptest.ResponseRecorder
	}{
		{"GET lista", http.StatusOK, func() *httptest.ResponseRecorder {
			return fazer(t, a.h, http.MethodGet, "/api/aprendizado", nil)
		}},
		{"POST aprender, recusado (corpo vazio)", http.StatusBadRequest, func() *httptest.ResponseRecorder {
			return fazer(t, a.h, http.MethodPost, "/api/aprendizado", map[string]string{})
		}},
		{"POST aprender, gravado", http.StatusOK, func() *httptest.ResponseRecorder {
			return a.aprender(t, "t1", "sf", "ECF")
		}},
		{"POST desfazer, gravado", http.StatusOK, func() *httptest.ResponseRecorder {
			return a.desfazer(t, "sf")
		}},
		{"POST desfazer, recusado (corpo vazio)", http.StatusBadRequest, func() *httptest.ResponseRecorder {
			return fazer(t, a.h, http.MethodPost, "/api/aprendizado/desfazer", map[string]string{})
		}},
	}
	for _, r := range rotas {
		t.Run(r.nome, func(t *testing.T) {
			w := r.fazer()
			exigirStatus(t, w, r.status)
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, quero no-store", got)
			}
		})
	}

	// E sem o aprendizado montado, o 503 também.
	b := novoAmbiente(t, nil)
	w := fazer(t, b.h, http.MethodPost, "/api/aprendizado", map[string]string{"id": "t1", "de": "sf", "para": "ECF"})
	exigirStatus(t, w, http.StatusServiceUnavailable)
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("503: Cache-Control = %q", got)
	}
}

// corpoDeTamanho devolve um JSON válido, de par bom, com EXATAMENTE n bytes:
// um campo de folga, que o servidor ignora, completa o tamanho.
func corpoDeTamanho(abre string, n int) string {
	const fecha = `"}`
	return abre + strings.Repeat("a", n-len(abre)-len(fecha)) + fecha
}

// O teto do corpo é de 16 KiB (16384 bytes), e o teste o prende dos dois lados:
// um corpo de par bom com exatamente 16 KiB é aceito, e com um byte a mais é
// recusado. O caso "corpo enorme" do outro teste passa pela recusa do "de"
// (40 KiB de "a" não estão no bruto), e não prova o teto; este prova, porque
// só o tamanho muda. O limite está escrito aqui, e não lido da constante, de
// propósito: é o número do contrato (docs/ai-spec/06-api-contract.md).
func TestCorpoAcimaDoTetoECortado(t *testing.T) {
	const teto = 16 << 10
	const (
		abreAprender = `{"id":"t1","de":"sf","para":"ECF","folga":"`
		abreDesfazer = `{"variante":"sf","folga":"`
	)

	a := novoAmbiente(t, novoFalso())
	sugestoesDe(t, a.salvar(t, "t1", 1, depoisSF))

	w := fazer(t, a.h, http.MethodPost, "/api/aprendizado", corpoDeTamanho(abreAprender, teto+1))
	exigirStatus(t, w, http.StatusBadRequest)
	if e := decodeJSON[erroView](t, w); e.Error.Message != "corpo JSON inválido" {
		t.Errorf("mensagem = %q", e.Error.Message)
	}
	if n := a.ap.gravacoes(); n != 0 {
		t.Fatalf("o corpo de teto+1 gravou %d vez(es)", n)
	}
	// O controle: o mesmo corpo, no teto, passa. Só o tamanho mudou.
	exigirStatus(t, fazer(t, a.h, http.MethodPost, "/api/aprendizado", corpoDeTamanho(abreAprender, teto)), http.StatusOK)

	w = fazer(t, a.h, http.MethodPost, "/api/aprendizado/desfazer", corpoDeTamanho(abreDesfazer, teto+1))
	exigirStatus(t, w, http.StatusBadRequest)
	if a.ap.nDesfazer != 0 {
		t.Errorf("o corpo de teto+1 chegou ao dicionário")
	}
	exigirStatus(t, fazer(t, a.h, http.MethodPost, "/api/aprendizado/desfazer", corpoDeTamanho(abreDesfazer, teto)), http.StatusOK)
}

// O erro de GET /api/aprendizado não vira "lista vazia": a lista vazia diria
// que não há nada aprendido, e a tela esconderia a seção com o que a pessoa
// precisa desfazer. É um 503 (ou 500) com a frase pronta.
func TestListaComErroNaoViraListaVazia(t *testing.T) {
	casos := []struct {
		nome   string
		erro   error
		status int
		code   string
		frase  string
	}{
		{"arquivo inválido", dictionary.ErrArquivoInvalido, http.StatusServiceUnavailable, "UNAVAILABLE", "O dicionário do Agent não abre. Nada foi gravado."},
		{"outro dono", dictionary.ErrUsuarioDiferente, http.StatusServiceUnavailable, "UNAVAILABLE", "O dicionário mudou de dono. Reinicie o Agent."},
		{"defeito qualquer", errors.New("CANARIO-DO-ERRO-INTERNO"), http.StatusInternalServerError, "INTERNAL", fraseDefeito},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			f := novoFalso()
			f.aprendidaErr = c.erro
			a := novoAmbiente(t, f)
			w := fazer(t, a.h, http.MethodGet, "/api/aprendizado", nil)
			exigirStatus(t, w, c.status)
			e := decodeJSON[erroView](t, w)
			if e.Error.Code != c.code || e.Error.Message != c.frase {
				t.Errorf("erro = %+v, quero %s %q", e.Error, c.code, c.frase)
			}
			if strings.Contains(w.Body.String(), "CANARIO") || strings.Contains(a.log.String(), "CANARIO") {
				t.Error("o texto do erro interno vazou")
			}
		})
	}
}

// O servidor lembra as sugestões de vários ditados ao mesmo tempo (até 32), e
// solta o mais antigo quando chega o 33º. Com a memória de um ditado só, quem
// salva dois ditados seguidos perderia o botão do primeiro.
func TestSugestoesDeVariosDitadosVivemAoMesmoTempo(t *testing.T) {
	const (
		quantos = 33
		limite  = 32
	)
	a := novoAmbiente(t, novoFalso())
	palavra := func(i int) string { return fmt.Sprintf("zq%c%c", 'a'+i/26, 'a'+i%26) }
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	for i := 0; i < quantos; i++ {
		w := palavra(i)
		seedTranscription(a.repo, fmt.Sprintf("e%02d", i), base.Add(time.Duration(i)*time.Minute), func(tr *storage.Transcription) {
			tr.RawText, tr.FinalText = "vou mandar o "+w+" amanhã", "Vou mandar o "+w+" amanhã."
		})
		if got := sugestoesDe(t, a.salvar(t, fmt.Sprintf("e%02d", i), 1, depoisSF)); len(got) != 1 || got[0].De != w {
			t.Fatalf("e%02d: sugestoes = %+v, quero a troca de %q", i, got, w)
		}
	}

	// O mais antigo foi solto.
	w := a.aprender(t, "e00", palavra(0), "ECF")
	exigirStatus(t, w, http.StatusConflict)
	if e := decodeJSON[erroView](t, w); e.Error.Message != fraseVencida {
		t.Errorf("o mais antigo: mensagem = %q", e.Error.Message)
	}
	// Os outros 32 estão vivos, o primeiro e o último da fila entre eles.
	for _, i := range []int{1, 2, limite - 1, limite} {
		exigirStatus(t, a.aprender(t, fmt.Sprintf("e%02d", i), palavra(i), "ECF"), http.StatusOK)
	}
	if n := a.ap.gravacoes(); n != 4 {
		t.Errorf("gravou %d vezes, quero 4", n)
	}
}
