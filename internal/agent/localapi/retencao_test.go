package localapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/expurgo"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// As rotas /api/retencao* (ADR-019, "Decisões da F10", F10d e F10e) contra um
// dublê do ciclo do expurgo: o handler só traduz. O ciclo e o banco de verdade
// estão em cmd/agent (retencao_rotas_test.go); o 401 sem token, em
// ac012_external_test.go.

// retencaoFalsa é o ciclo do expurgo visto pela API local.
type retencaoFalsa struct {
	estado      storage.EstadoDosSeisMeses
	erroEstado  error
	ligar       storage.ResultadoDoLigar
	erroLigar   error
	erroDesliga error
	copias      storage.ResultadoDasCopias
	erroCopias  error

	chamadas []string
	previa   int
}

func (f *retencaoFalsa) Estado(context.Context) (storage.EstadoDosSeisMeses, error) {
	f.chamadas = append(f.chamadas, "estado")
	return f.estado, f.erroEstado
}

func (f *retencaoFalsa) Ligar(_ context.Context, previa int) (storage.ResultadoDoLigar, error) {
	f.chamadas = append(f.chamadas, "ligar")
	f.previa = previa
	return f.ligar, f.erroLigar
}

func (f *retencaoFalsa) Desligar(context.Context) error {
	f.chamadas = append(f.chamadas, "desligar")
	return f.erroDesliga
}

func (f *retencaoFalsa) ApagarCopias(context.Context) (storage.ResultadoDasCopias, error) {
	f.chamadas = append(f.chamadas, "copias")
	return f.copias, f.erroCopias
}

func apiComRetencao(t *testing.T, r localapi.Retencao) http.Handler {
	t.Helper()
	pref := prefsDeTeste()
	pref.Retencao = r
	return localapi.New("u-local", storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()
}

func emRetencao(total, lixeira, selo, outro int) storage.EstadoDosSeisMeses {
	return storage.EstadoDosSeisMeses{
		ContagemDosSeisMeses: storage.ContagemDosSeisMeses{Total: total, NaLixeira: lixeira, ComSelo: selo, DeOutroDono: outro},
	}
}

var rotasDaRetencao = []struct{ metodo, caminho, corpo string }{
	{"GET", "/api/retencao", ""},
	{"POST", "/api/retencao/ligar", `{"previa":3}`},
	{"POST", "/api/retencao/desligar", ""},
	{"POST", "/api/retencao/copias/apagar", ""},
}

// O GET devolve exatamente as nove chaves do contrato, e só números e
// booleanos; com a contagem do ciclo.
func TestRetencao_GetSoDevolveNumeros(t *testing.T) {
	f := &retencaoFalsa{estado: emRetencao(1240, 3, 2, 5)}
	f.estado.Ligada, f.estado.Pausada, f.estado.Copias, f.estado.CopiasApagaveis, f.estado.CopiaDosSeisMeses = false, true, 2, 1, true
	w := fazer(t, apiComRetencao(t, f), "GET", "/api/retencao", nil)
	if w.Code != 200 {
		t.Fatalf("GET = %d %s", w.Code, w.Body)
	}
	var campos map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &campos); err != nil {
		t.Fatal(err)
	}
	quer := map[string]any{
		"previa": 1240.0, "na_lixeira": 3.0, "com_selo": 2.0, "de_outro_dono": 5.0,
		"ligada": false, "pausada": true, "copias": 2.0, "copias_apagaveis": 1.0, "copia_dos_6_meses": true,
	}
	if len(campos) != len(quer) {
		t.Fatalf("o GET tem %d chaves: %v; quero as %d do contrato", len(campos), campos, len(quer))
	}
	for k, v := range quer {
		if campos[k] != v {
			t.Errorf("%s = %v; quero %v", k, campos[k], v)
		}
	}
	for k, v := range campos {
		switch v.(type) {
		case float64, bool:
		default:
			t.Errorf("%s é %T; o GET só devolve números e booleanos", k, v)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("sem no-store")
	}
	if len(f.chamadas) != 1 || f.chamadas[0] != "estado" {
		t.Errorf("o GET chamou %v; quero só ler o estado", f.chamadas)
	}
}

// Toda rota responde no-store, 200 com o estado, e chama só o que deve.
func TestRetencao_RotasNoStoreEChamadas(t *testing.T) {
	quer := map[string][]string{
		"/api/retencao":               {"estado"},
		"/api/retencao/ligar":         {"ligar", "estado"},
		"/api/retencao/desligar":      {"desligar", "estado"},
		"/api/retencao/copias/apagar": {"copias", "estado"},
	}
	for _, r := range rotasDaRetencao {
		f := &retencaoFalsa{estado: emRetencao(3, 0, 0, 0), ligar: storage.ResultadoDoLigar{Previa: 3, Apagados: 3}}
		var corpo any
		if r.corpo != "" {
			corpo = r.corpo
		}
		w := fazer(t, apiComRetencao(t, f), r.metodo, r.caminho, corpo)
		if w.Code != 200 {
			t.Errorf("%s %s = %d %s", r.metodo, r.caminho, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: sem no-store", r.caminho)
		}
		if strings.Join(f.chamadas, ",") != strings.Join(quer[r.caminho], ",") {
			t.Errorf("%s chamou %v; quero %v", r.caminho, f.chamadas, quer[r.caminho])
		}
	}
}

// Sem a retenção montada, as quatro rotas respondem 503 UNAVAILABLE com a
// frase de "reinicie".
func TestRetencao_SemAInterface503(t *testing.T) {
	h := apiComRetencao(t, nil)
	for _, r := range rotasDaRetencao {
		var corpo any
		if r.corpo != "" {
			corpo = r.corpo
		}
		w := fazer(t, h, r.metodo, r.caminho, corpo)
		var e erroDaAPI
		if w.Code != http.StatusServiceUnavailable || json.Unmarshal(w.Body.Bytes(), &e) != nil ||
			e.Error.Code != "UNAVAILABLE" || e.Error.Message != localapi.MsgRetencaoNaoPronta {
			t.Errorf("%s %s sem a interface = %d %s", r.metodo, r.caminho, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: sem no-store", r.caminho)
		}
	}
}

// O Ligar leva a contagem que a tela mostrou, e devolve o estado e quantos
// saíram (igual ao que o ciclo apagou).
func TestRetencao_LigarLevaAPreviaEDevolveApagados(t *testing.T) {
	f := &retencaoFalsa{estado: emRetencao(0, 0, 0, 1), ligar: storage.ResultadoDoLigar{Previa: 1240, Apagados: 1240, Copia: "historico.db.antes-dos-6-meses-x.db"}}
	f.estado.Ligada, f.estado.Copias = true, 1
	w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", `{"previa":1240}`)
	if w.Code != 200 || f.previa != 1240 {
		t.Fatalf("Ligar = %d %s (previa %d)", w.Code, w.Body, f.previa)
	}
	var campos map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &campos); err != nil {
		t.Fatal(err)
	}
	if campos["apagados"] != 1240.0 || campos["ligada"] != true || campos["previa"] != 0.0 || campos["copias"] != 1.0 || campos["de_outro_dono"] != 1.0 {
		t.Fatalf("o Ligar devolveu %v", campos)
	}
	// O nome da cópia não vai para a tela.
	if strings.Contains(w.Body.String(), "historico.db") {
		t.Fatalf("a resposta tem o nome da cópia: %s", w.Body)
	}

	// Já ligada: 200, sem apagar nada.
	f = &retencaoFalsa{estado: emRetencao(0, 0, 0, 0), ligar: storage.ResultadoDoLigar{JaLigada: true}}
	f.estado.Ligada = true
	w = fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", `{"previa":7}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"apagados":0`) {
		t.Fatalf("Ligar já ligada = %d %s", w.Code, w.Body)
	}
}

// O Ligar que gravou o OK e apagou parte, mas não tudo (um lote seguinte
// caiu), ligou: 200 com o estado (ligada, e a prévia do que falta), os
// apagados até ali e incompleto, e nunca a frase de "tente de novo". Vale com
// o resultado marcado (o ciclo já tira o erro) e com o erro embrulhado.
func TestRetencao_LigarIncompletoEUm200Ligado(t *testing.T) {
	for nome, f := range map[string]*retencaoFalsa{
		"marcado pelo ciclo": {ligar: storage.ResultadoDoLigar{Previa: 1240, Apagados: 500, Incompleto: true}},
		"com o erro do storage": {ligar: storage.ResultadoDoLigar{Previa: 1240, Apagados: 500, Incompleto: true},
			erroLigar: fmt.Errorf("%w: %w", storage.ErrLigadoIncompleto, errors.New("context canceled"))},
	} {
		f.estado = emRetencao(740, 0, 0, 0)
		f.estado.Ligada, f.estado.Copias = true, 1
		w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", `{"previa":1240}`)
		var campos map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &campos) != nil {
			t.Fatalf("%s: Ligar incompleto = %d %s; quero 200", nome, w.Code, w.Body)
		}
		if campos["apagados"] != 500.0 || campos["incompleto"] != true || campos["ligada"] != true || campos["previa"] != 740.0 {
			t.Fatalf("%s: o Ligar incompleto devolveu %v", nome, campos)
		}
		if strings.Contains(w.Body.String(), localapi.MsgLigarFalhou) {
			t.Fatalf("%s: a resposta manda tentar de novo: %s", nome, w.Body)
		}
	}
	// O Ligar completo diz incompleto false.
	f := &retencaoFalsa{estado: emRetencao(0, 0, 0, 0), ligar: storage.ResultadoDoLigar{Previa: 3, Apagados: 3}}
	f.estado.Ligada = true
	w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", `{"previa":3}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"incompleto":false`) {
		t.Fatalf("Ligar completo = %d %s", w.Code, w.Body)
	}
}

// Sem a contagem no corpo, com ela negativa, ou com um corpo inválido ou
// grande demais: 400, e o ciclo nem é chamado (um OK sem número seria um OK
// às cegas).
func TestRetencao_LigarSemAPreviaE400(t *testing.T) {
	grande := `{"previa":3,"lixo":"` + strings.Repeat("x", 2000) + `"}`
	for nome, corpo := range map[string]string{
		"vazio":      ``,
		"objeto":     `{}`,
		"nulo":       `{"previa":null}`,
		"negativo":   `{"previa":-1}`,
		"texto":      `{"previa":"3"}`,
		"quebrado":   `{"previa":`,
		"fracionado": `{"previa":1.5}`,
		"grande":     grande,
	} {
		f := &retencaoFalsa{estado: emRetencao(3, 0, 0, 0)}
		w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", corpo)
		var e erroDaAPI
		if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != "BAD_REQUEST" {
			t.Errorf("%s: Ligar = %d %s; quero 400", nome, w.Code, w.Body)
		}
		for _, c := range f.chamadas {
			if c == "ligar" {
				t.Errorf("%s: o ciclo foi chamado", nome)
			}
		}
	}
}

// As recusas do Ligar: o status, o código e a frase de cada uma.
func TestRetencao_LigarRecusas(t *testing.T) {
	for _, c := range []struct {
		nome   string
		err    error
		status int
		code   string
		frase  string
	}{
		{"nada a apagar", storage.ErrNadaAApagar, 409, "CONFLICT", "Não há nada a apagar agora."},
		{"sem o dono fixo", storage.ErrSemDonoFixo, 409, "CONFLICT", localapi.MsgRetencaoNaoPronta},
		{"ciclo parado", expurgo.ErrParado, 503, "UNAVAILABLE", localapi.MsgRetencaoNaoPronta},
		{"sem os 6 meses", expurgo.ErrSemSeisMeses, 503, "UNAVAILABLE", localapi.MsgRetencaoNaoPronta},
		{"cópias demais", &storage.ErroDaCopia{Motivo: storage.ErrCopiasDemais, Detalhe: "já há 3 cópias"}, 409, "CONFLICT",
			"Não deu para copiar o histórico. Nada foi apagado. Já há 3 cópias de antes dos 6 meses ao lado do histórico: apague as que não quiser."},
		{"disco cheio", &storage.ErroDaCopia{Motivo: storage.ErrDiscoCheio, Detalhe: "disk is full"}, 507, "INTERNAL",
			"Não deu para copiar o histórico. Nada foi apagado. Falta espaço no disco."},
		{"cópia falhou", &storage.ErroDaCopia{Motivo: storage.ErrCopiaDosSeisMeses, Detalhe: "x"}, 500, "INTERNAL",
			"Não deu para copiar o histórico. Nada foi apagado."},
		{"outro erro", errors.New("storage: apagando: database is locked"), 500, "INTERNAL", localapi.MsgLigarFalhou},
	} {
		f := &retencaoFalsa{erroLigar: c.err}
		w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", `{"previa":3}`)
		var e erroDaAPI
		if w.Code != c.status || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != c.code || e.Error.Message != c.frase {
			t.Errorf("%s: Ligar = %d %s; quero %d %s %q", c.nome, w.Code, w.Body, c.status, c.code, c.frase)
		}
	}
}

// A contagem que mudou: 409 com o envelope de erro e, ao lado dele, o número
// novo (a tela troca a frase e pergunta de novo).
func TestRetencao_LigarComAContagemQueMudou(t *testing.T) {
	f := &retencaoFalsa{erroLigar: fmt.Errorf("embrulhado: %w", &storage.ErrPreviaMudou{Previa: 1301})}
	w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", `{"previa":1240}`)
	var corpo struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
		Previa *int `json:"previa"`
	}
	if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &corpo) != nil {
		t.Fatalf("Ligar = %d %s", w.Code, w.Body)
	}
	if corpo.Previa == nil || *corpo.Previa != 1301 || corpo.Error.Code != "CONFLICT" || corpo.Error.Message != localapi.MsgPreviaMudou || corpo.Error.RequestID == "" {
		t.Fatalf("o corpo do 409 = %s", w.Body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

// O Desligar devolve o estado; o erro dele vira uma frase.
func TestRetencao_Desligar(t *testing.T) {
	f := &retencaoFalsa{estado: emRetencao(7, 0, 0, 0)}
	w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/desligar", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"previa":7`) || !strings.Contains(w.Body.String(), `"ligada":false`) {
		t.Fatalf("Desligar = %d %s", w.Code, w.Body)
	}

	f = &retencaoFalsa{erroDesliga: errors.New("storage: desligando: disco com problema em C:\\Users\\<usuario>\\AppData\\historico.db")}
	w = fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/desligar", nil)
	var e erroDaAPI
	if w.Code != 500 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Message != localapi.MsgDesligarFalhou {
		t.Fatalf("Desligar com erro = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "usuario") || strings.Contains(w.Body.String(), "AppData") {
		t.Fatalf("o erro de dentro foi para a tela: %s", w.Body)
	}
}

// O apagar das cópias devolve o estado, quantas saíram e quantas ficaram.
func TestRetencao_ApagarCopias(t *testing.T) {
	f := &retencaoFalsa{estado: emRetencao(0, 0, 0, 0), copias: storage.ResultadoDasCopias{Apagadas: 2, Ficaram: 1}}
	f.estado.Copias = 1
	w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/copias/apagar", nil)
	var campos map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &campos) != nil {
		t.Fatalf("apagar = %d %s", w.Code, w.Body)
	}
	if campos["apagadas"] != 2.0 || campos["ficaram"] != 1.0 || campos["copias"] != 1.0 || campos["previa"] != 0.0 {
		t.Fatalf("apagar devolveu %v", campos)
	}

	// Uma que o sistema não deixou apagar: 500 com a frase que diz que as
	// outras saíram; sem o caminho.
	f = &retencaoFalsa{
		copias:     storage.ResultadoDasCopias{Apagadas: 2, Ficaram: 1},
		erroCopias: errors.New("storage: apagando uma cópia do histórico: The process cannot access the file"),
	}
	w = fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/copias/apagar", nil)
	var e erroDaAPI
	if w.Code != 500 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Message != localapi.MsgCopiasNaoSairamTodas {
		t.Fatalf("apagar com falha parcial = %d %s", w.Code, w.Body)
	}
	f = &retencaoFalsa{erroCopias: storage.ErrSemCaminhoDoBanco}
	w = fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/copias/apagar", nil)
	if w.Code != 500 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Message != localapi.MsgCopiasFalhou {
		t.Fatalf("apagar com falha = %d %s", w.Code, w.Body)
	}
	f = &retencaoFalsa{erroCopias: expurgo.ErrParado}
	if w = fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/copias/apagar", nil); w.Code != 503 {
		t.Fatalf("apagar com o ciclo parado = %d %s", w.Code, w.Body)
	}
}

// Nenhuma resposta traz texto, id nem caminho: nem a de sucesso, nem a de
// erro, mesmo quando o erro de dentro os tem.
func TestRetencao_NenhumaRespostaTemTextoIdOuCaminho(t *testing.T) {
	const veneno = `C:\Users\<usuario>\AppData\Local\historico.db 0199f000-0000-7000-8000-000000000001 a jabuticaba ficou na geladeira`
	erros := []error{
		errors.New(veneno),
		&storage.ErroDaCopia{Motivo: storage.ErrCopiaDosSeisMeses, Detalhe: veneno},
		&storage.ErroDaCopia{Motivo: storage.ErrCopiasDemais, Detalhe: veneno},
		&storage.ErroDaCopia{Motivo: storage.ErrDiscoCheio, Detalhe: veneno},
	}
	for i, err := range erros {
		f := &retencaoFalsa{
			estado: emRetencao(1, 0, 0, 0), erroLigar: err, erroDesliga: err, erroCopias: err, erroEstado: err,
			copias: storage.ResultadoDasCopias{Apagadas: 1}, ligar: storage.ResultadoDoLigar{Copia: veneno},
		}
		h := apiComRetencao(t, f)
		for _, r := range rotasDaRetencao {
			var corpo any
			if r.corpo != "" {
				corpo = r.corpo
			}
			w := fazer(t, h, r.metodo, r.caminho, corpo)
			for _, proibido := range []string{"jabuticaba", "0199f000", "AppData", "usuario", "historico.db"} {
				if strings.Contains(w.Body.String(), proibido) {
					t.Errorf("erro %d, %s: a resposta tem %q: %s", i, r.caminho, proibido, w.Body)
				}
			}
		}
	}
	// E a de sucesso, com a cópia no resultado do Ligar.
	f := &retencaoFalsa{estado: emRetencao(0, 0, 0, 0), ligar: storage.ResultadoDoLigar{Copia: veneno, Apagados: 1}}
	w := fazer(t, apiComRetencao(t, f), "POST", "/api/retencao/ligar", `{"previa":1}`)
	if strings.Contains(w.Body.String(), "AppData") || strings.Contains(w.Body.String(), "historico.db") {
		t.Fatalf("a resposta do Ligar tem o nome da cópia: %s", w.Body)
	}
}

// O GET com a leitura do estado falhando: 500 com uma frase, sem o erro de
// dentro.
func TestRetencao_GetComErroDeLeitura(t *testing.T) {
	f := &retencaoFalsa{erroEstado: errors.New("storage: lendo C:\\Users\\<usuario>\\historico.db: locked")}
	w := fazer(t, apiComRetencao(t, f), "GET", "/api/retencao", nil)
	var e erroDaAPI
	if w.Code != 500 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != "INTERNAL" || strings.Contains(w.Body.String(), "usuario") {
		t.Fatalf("GET com erro = %d %s", w.Code, w.Body)
	}
}
