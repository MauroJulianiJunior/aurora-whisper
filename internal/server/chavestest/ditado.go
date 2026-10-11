package chavestest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudauth"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Os casos do aceite e das preferências do ditado no celular (ADR-021, decisão
// H e C3b). Todos rodam com o aceite de VERDADE (a dictation_consents), não com
// o dublê dos casos das chaves: o que se prova aqui é que o aceite que a pessoa
// dá pela rota é o que liga o PUT das chaves e o PUT das preferências.

const (
	rotaEstado       = "/api/v1/dictation"
	rotaPreferencias = "/api/v1/dictation/settings"
	rotaAceite       = "/api/v1/dictation/consent"
)

// versaoDoAviso é a do servidor, a que o app manda quando mostra o texto atual.
const versaoDoAviso = cloudstore.VersaoDoAviso

// comAceiteDeVerdade monta a nuvem sem o dublê do aceite.
func (m *mundo) comAceiteDeVerdade() {
	m.t.Helper()
	m.aceiteDeVerdade = true
	m.montar(ChaveDeCifraA)
}

// estadoDTO é a resposta do GET /dictation (e do PUT e do POST), lida com
// DisallowUnknownFields: um campo novo na resposta sem o teste saber quebra aqui.
type estadoDTO struct {
	Aviso struct {
		Versao int `json:"versao"`
	} `json:"aviso"`
	Aceite *struct {
		Versao      int       `json:"versao"`
		Transcricao string    `json:"transcricao"`
		Polish      *string   `json:"polish"`
		AceitoEm    time.Time `json:"aceito_em"`
		Vigente     bool      `json:"vigente"`
	} `json:"aceite"`
	Preferencias struct {
		Transcricao struct {
			Provedor string `json:"provedor"`
			Modelo   string `json:"modelo"`
		} `json:"transcricao"`
		Dica   bool `json:"dica"`
		Polish struct {
			Ligado   bool   `json:"ligado"`
			Provedor string `json:"provedor"`
			Modelo   string `json:"modelo"`
		} `json:"polish"`
	} `json:"preferencias"`
	Chaves []struct {
		Provedor    string `json:"provedor"`
		Configurada bool   `json:"configurada"`
		Final       string `json:"final"`
		ColarDeNovo bool   `json:"colar_de_novo"`
	} `json:"chaves"`
}

func (m *mundo) estado(quem string) estadoDTO {
	m.t.Helper()
	rec := m.pedir(quem, http.MethodGet, rotaEstado, "")
	if rec.Code != http.StatusOK {
		m.t.Fatalf("GET /dictation: %d %s", rec.Code, rec.Body.String())
	}
	return decodificar[estadoDTO](m.t, rec)
}

// aceitar manda o aceite pela rota, com a versão, o provedor da transcrição e,
// se não for nil, o do Polish.
func (m *mundo) aceitar(quem string, versao int, transcricao string, polish *string) *httptest.ResponseRecorder {
	m.t.Helper()
	corpo := map[string]any{"versao": versao, "transcricao": transcricao}
	if polish != nil {
		corpo["polish"] = *polish
	}
	return m.pedir(quem, http.MethodPost, rotaAceite, jsonDe(m.t, corpo))
}

func texto(s string) *string { return &s }

// jsonDe codifica o corpo de um pedido.
func jsonDe(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// aceitarOk aceita com a versão atual e exige o 200.
func (m *mundo) aceitarOk(quem, transcricao string, polish *string) estadoDTO {
	m.t.Helper()
	rec := m.aceitar(quem, versaoDoAviso, transcricao, polish)
	if rec.Code != http.StatusOK {
		m.t.Fatalf("POST /dictation/consent (%s, %v): %d %s", transcricao, polish, rec.Code, rec.Body.String())
	}
	return decodificar[estadoDTO](m.t, rec)
}

// preferencias manda o PUT das preferências.
func (m *mundo) preferencias(quem, provedor, modelo string, dica, polishLigado bool, polishProvedor, polishModelo string) *httptest.ResponseRecorder {
	m.t.Helper()
	return m.pedir(quem, http.MethodPut, rotaPreferencias, jsonDe(m.t, map[string]any{
		"transcricao": map[string]any{"provedor": provedor, "modelo": modelo},
		"dica":        dica,
		"polish":      map[string]any{"ligado": polishLigado, "provedor": polishProvedor, "modelo": polishModelo},
	}))
}

// lidas são as preferências guardadas da conta, direto no store.
func (m *mundo) lidas(userID string) cloudstore.PreferenciasDoDitado {
	m.t.Helper()
	p, err := m.b.Ditado.LerPreferenciasDoDitado(context.Background(), userID)
	if err != nil {
		m.t.Fatalf("LerPreferenciasDoDitado: %v", err)
	}
	return p
}

func (m *mundo) aceites(userID string) []cloudstore.Aceite {
	m.t.Helper()
	l, err := m.b.Ditado.ListarAceites(context.Background(), userID)
	if err != nil {
		m.t.Fatalf("ListarAceites: %v", err)
	}
	return l
}

func abertos(l []cloudstore.Aceite) (n int) {
	for _, a := range l {
		if a.Aberto() {
			n++
		}
	}
	return n
}

// A conta nova não tem aceite, e as preferências são os padrões. O estado traz
// a versão do aviso, as três chaves sem nada, e nada foi gravado só por ler.
func testEstadoInicial(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	rec := m.pedir(celA, http.MethodGet, rotaEstado, "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("GET /dictation: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), `"aceite":null`) {
		t.Errorf("sem aceite, o estado devia trazer \"aceite\":null: %s", rec.Body.String())
	}
	est := decodificar[estadoDTO](t, rec)
	if est.Aviso.Versao != versaoDoAviso || est.Aceite != nil {
		t.Fatalf("aviso %d, aceite %+v", est.Aviso.Versao, est.Aceite)
	}
	p := est.Preferencias
	if p.Transcricao.Provedor != "openai" || p.Transcricao.Modelo != cloudstore.ModeloPadraoOpenAI || p.Dica ||
		p.Polish.Ligado || p.Polish.Provedor != "openai" || p.Polish.Modelo != "" {
		t.Errorf("os padrões não são os da ADR (OpenAI, sem dica, sem Polish): %+v", p)
	}
	if len(est.Chaves) != 3 || est.Chaves[0].Provedor != "openai" || est.Chaves[1].Provedor != "groq" || est.Chaves[2].Provedor != "anthropic" {
		t.Fatalf("as chaves do estado: %+v, quero uma por provedor, na ordem da lista", est.Chaves)
	}
	for _, k := range est.Chaves {
		if k.Configurada || k.Final != "" || k.ColarDeNovo {
			t.Errorf("a chave %+v não devia ter nada", k)
		}
	}
	// Ler não grava nada.
	if l := m.aceites(m.userA); len(l) != 0 {
		t.Errorf("ler o estado gravou %d aceites", len(l))
	}
	if !m.lidas(m.userA).AtualizadaEm.IsZero() {
		t.Error("ler o estado gravou as preferências")
	}
}

// O aceite grava a versão e os provedores (a transcrição e o Polish), com a
// data do servidor, e é ele que liga o PUT das chaves: só os provedores do
// aceite passam. Sem aceite, nada. O de uma conta não vale para a outra.
func testAceiteGravaELigaAsChaves(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")

	est := m.aceitarOk(celA, "openai", texto("anthropic"))
	a := est.Aceite
	if a == nil || a.Versao != versaoDoAviso || a.Transcricao != "openai" || a.Polish == nil || *a.Polish != "anthropic" ||
		!a.AceitoEm.Equal(agora0) || !a.Vigente {
		t.Fatalf("o aceite no estado: %+v", a)
	}
	l := m.aceites(m.userA)
	if len(l) != 1 {
		t.Fatalf("%d aceites gravados, quero 1", len(l))
	}
	if id, ok := cloudstore.CanonicalUUID(l[0].ID); !ok || id != l[0].ID {
		t.Errorf("o id do aceite não é um UUID: %q", l[0].ID)
	}
	if g := l[0]; g.Versao != versaoDoAviso || g.Transcricao != "openai" || g.Polish != "anthropic" || !g.AceitoEm.Equal(agora0) ||
		!g.Aberto() || g.Motivo != "" || !g.Vigente() {
		t.Errorf("o aceite gravado: %+v", g)
	}
	if aberto, err := m.b.Ditado.LerAceiteAberto(context.Background(), m.userA); err != nil || aberto.ID != l[0].ID {
		t.Errorf("LerAceiteAberto: %+v, %v", aberto, err)
	}

	// O aceite de verdade liga as chaves: os dois provedores do aceite passam,
	// o terceiro não, e o provedor de IA só é chamado quando passa.
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT openai com o aceite: %d %s", rec.Code, rec.Body.String())
	}
	if rec := m.put(celA, "anthropic", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT anthropic com o aceite (o do Polish): %d %s", rec.Code, rec.Body.String())
	}
	exigirErro(t, m.put(celA, "groq", canario), http.StatusForbidden, "SEM_ACEITE")
	if rec := m.modelos(celA, "openai"); rec.Code != http.StatusOK {
		t.Fatalf("GET models com o aceite: %d", rec.Code)
	}
	if m.prov.quantas() != 3 {
		t.Fatalf("%d chamadas ao provedor, quero 3 (as duas gravações e a lista)", m.prov.quantas())
	}
	// O aceite de A não vale para B.
	exigirErro(t, m.put(celB, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	if b := m.estado(celB); b.Aceite != nil {
		t.Errorf("o aceite de A apareceu no estado de B: %+v", b.Aceite)
	}

	// O estado traz o fim das chaves e nunca a chave.
	rec := m.pedir(celA, http.MethodGet, rotaEstado, "")
	semCanario(t, "o estado do ditado", rec.Body.String(), canario)
	est = decodificar[estadoDTO](t, rec)
	if k := est.Chaves[0]; k.Provedor != "openai" || !k.Configurada || k.Final != "WXYZ" || k.ColarDeNovo {
		t.Errorf("a chave openai no estado: %+v", k)
	}
	if k := est.Chaves[1]; k.Configurada || k.Final != "" {
		t.Errorf("a chave groq no estado: %+v", k)
	}
}

// O aceite só vale com a versão atual do aviso: a de outro texto é 409
// AVISO_DESATUALIZADO, e o corpo mal formado ou com valor fora da lista é
// recusado com uma frase fixa. Nada disso grava.
func testAceiteSoComAVersaoAtual(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	for _, v := range []int{0, -1, versaoDoAviso + 1, versaoDoAviso + 100} {
		exigirErro(t, m.aceitar(celA, v, "openai", nil), http.StatusConflict, "AVISO_DESATUALIZADO")
	}
	if !strings.Contains(m.logs.String(), "motivo=aviso") {
		t.Error("o log de acesso não diz o motivo do 409")
	}

	// Faltam campos: a versão e a transcrição.
	exigirErro(t, m.pedir(celA, http.MethodPost, rotaAceite, `{"transcricao":"openai"}`), http.StatusBadRequest, "VALIDATION")
	exigirErro(t, m.pedir(celA, http.MethodPost, rotaAceite, fmt.Sprintf(`{"versao":%d}`, versaoDoAviso)), http.StatusBadRequest, "VALIDATION")
	exigirErro(t, m.pedir(celA, http.MethodPost, rotaAceite, fmt.Sprintf(`{"versao":%d,"transcricao":null}`, versaoDoAviso)), http.StatusBadRequest, "VALIDATION")

	// Os provedores fora da lista de cada papel.
	for nome, corpo := range map[string]*httptest.ResponseRecorder{
		"transcrição pela anthropic": m.aceitar(celA, versaoDoAviso, "anthropic", nil),
		"transcrição fora da lista":  m.aceitar(celA, versaoDoAviso, "xai", nil),
		"transcrição em maiúsculas":  m.aceitar(celA, versaoDoAviso, "OpenAI", nil),
		"transcrição vazia":          m.aceitar(celA, versaoDoAviso, "", nil),
		"Polish pela groq":           m.aceitar(celA, versaoDoAviso, "openai", texto("groq")),
		"Polish fora da lista":       m.aceitar(celA, versaoDoAviso, "openai", texto("xai")),
		"Polish vazio":               m.aceitar(celA, versaoDoAviso, "openai", texto("")),
	} {
		if code, _ := erroDe(t, corpo); corpo.Code != http.StatusBadRequest || code != "VALIDATION" {
			t.Errorf("%s: %d %s, quero 400 VALIDATION", nome, corpo.Code, code)
		}
	}

	// O corpo mal formado: a frase fixa, sem eco do que veio.
	const eco = "CANARIO-DO-CORPO-DO-ACEITE"
	for nome, corpo := range map[string]string{
		"JSON quebrado":      `{"versao":`,
		"campo desconhecido": `{"versao":1,"transcricao":"openai","` + eco + `":1}`,
		"tipo errado":        `{"versao":"` + eco + `","transcricao":"openai"}`,
		"dois objetos":       `{"versao":1,"transcricao":"openai"} {"` + eco + `":1}`,
		"o dono no corpo":    `{"versao":1,"transcricao":"openai","user_id":"` + eco + `"}`,
		"vazio":              ``,
	} {
		rec := m.pedir(celA, http.MethodPost, rotaAceite, corpo)
		if msg := exigirErro(t, rec, http.StatusBadRequest, "BAD_REQUEST"); msg != server.FraseDoCorpoDoAceite {
			t.Errorf("%s: %q, quero a frase fixa", nome, msg)
		}
		if strings.Contains(rec.Body.String(), eco) {
			t.Errorf("%s: a resposta repete o que veio", nome)
		}
	}
	exigirErro(t, m.pedir(celA, http.MethodPost, rotaAceite, `{"versao":1,"transcricao":"`+strings.Repeat("a", 5000)+`"}`),
		http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
	if l := m.aceites(m.userA); len(l) != 0 {
		t.Fatalf("um aceite recusado gravou %d linhas", len(l))
	}

	// A versão atual, com o Polish nulo ou sem o campo, grava.
	if rec := m.pedir(celA, http.MethodPost, rotaAceite, fmt.Sprintf(`{"versao":%d,"transcricao":"groq","polish":null}`, versaoDoAviso)); rec.Code != http.StatusOK {
		t.Fatalf("o aceite com polish null: %d %s", rec.Code, rec.Body.String())
	}
	if est := m.estado(celA); est.Aceite == nil || est.Aceite.Polish != nil || est.Aceite.Transcricao != "groq" {
		t.Errorf("o estado depois do aceite sem Polish: %+v", est.Aceite)
	}
	if !strings.Contains(m.logs.String(), "motivo=aviso") || strings.Contains(m.logs.String(), eco) {
		t.Error("o log de acesso repete o corpo, ou perdeu o motivo")
	}
}

// Um aceite novo toma o lugar do anterior (que fica gravado, terminado como
// substituído), e a conta nunca tem dois em aberto. Aceitar de novo o mesmo não
// grava outra linha e a data é a da primeira vez. Os provedores cobertos são os
// do aceite que vale agora.
func testAceiteNovoSubstitui(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	m.clk.Advance(time.Hour)
	m.aceitarOk(celA, "openai", nil)
	if l := m.aceites(m.userA); len(l) != 1 || !l[0].AceitoEm.Equal(agora0) {
		t.Fatalf("aceitar o mesmo de novo: %+v, quero uma linha com a data da primeira vez", l)
	}

	// Outro provedor, com o Polish da OpenAI: troca.
	m.clk.Advance(time.Hour)
	t2 := m.clk.Now()
	m.aceitarOk(celA, "groq", texto("openai"))
	l := m.aceites(m.userA)
	if len(l) != 2 || abertos(l) != 1 {
		t.Fatalf("depois de trocar: %+v", l)
	}
	if l[0].Aberto() || l[0].Motivo != cloudstore.FimSubstituido || !l[0].EncerradoEm.Equal(t2) || l[0].Transcricao != "openai" {
		t.Errorf("o aceite anterior: %+v, quero terminado como substituído em %v", l[0], t2)
	}
	if !l[1].Aberto() || l[1].Transcricao != "groq" || l[1].Polish != "openai" || !l[1].AceitoEm.Equal(t2) {
		t.Errorf("o aceite novo: %+v", l[1])
	}
	// O novo cobre a groq e a openai (a do Polish), e não a anthropic.
	if rec := m.put(celA, "groq", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT groq: %d", rec.Code)
	}
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT openai (o do Polish): %d", rec.Code)
	}
	exigirErro(t, m.put(celA, "anthropic", canario), http.StatusForbidden, "SEM_ACEITE")

	// O Polish a menos: a openai sai da cobertura.
	m.clk.Advance(time.Hour)
	m.aceitarOk(celA, "groq", nil)
	if l := m.aceites(m.userA); len(l) != 3 || abertos(l) != 1 || l[1].Motivo != cloudstore.FimSubstituido {
		t.Fatalf("depois do terceiro aceite: %+v", l)
	}
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	if rec := m.put(celA, "groq", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT groq: %d", rec.Code)
	}
	if est := m.estado(celA); est.Aceite == nil || est.Aceite.Polish != nil || est.Aceite.Transcricao != "groq" || !est.Aceite.Vigente {
		t.Errorf("o aceite no estado: %+v", est.Aceite)
	}
	// As chaves que ficaram sem aceite continuam guardadas (só revogar as
	// apaga), mas não se usam.
	if k, err := m.linha(m.userA, "openai"); err != nil || k.Final != "WXYZ" {
		t.Errorf("a chave da openai depois de trocar o aceite: %+v, %v", k, err)
	}
	ids := map[string]bool{}
	for _, a := range m.aceites(m.userA) {
		if ids[a.ID] {
			t.Errorf("o id %s repete", a.ID)
		}
		ids[a.ID] = true
	}
}

// Mudar o texto do aviso pede o aceite de novo: um aceite de outra versão do
// aviso continua gravado, mas não vale (vigente=false), e o PUT das chaves e o
// das preferências recusam até a pessoa aceitar o texto atual.
func testAvisoNovoPedeAceiteNovo(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	// O servidor passou a outro texto do aviso: o aceite que a pessoa deu é de
	// uma versão que não é a atual.
	velho, err := m.b.Ditado.GravarAceite(context.Background(), m.userA,
		cloudstore.NovoAceite{Versao: versaoDoAviso + 1, Transcricao: "openai"}, agora0)
	if err != nil {
		t.Fatal(err)
	}
	if velho.Vigente() {
		t.Fatal("o aceite de outra versão do aviso não devia valer")
	}
	est := m.estado(celA)
	if est.Aceite == nil || est.Aceite.Vigente || est.Aceite.Versao != versaoDoAviso+1 || est.Aviso.Versao != versaoDoAviso {
		t.Fatalf("o estado com o aceite de outra versão: aviso %d, aceite %+v", est.Aviso.Versao, est.Aceite)
	}
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	exigirErro(t, m.modelos(celA, "openai"), http.StatusForbidden, "SEM_ACEITE")
	exigirErro(t, m.preferencias(celA, "openai", "", false, false, "openai", ""), http.StatusForbidden, "SEM_ACEITE")
	if m.prov.quantas() != 0 {
		t.Fatal("o aceite de outra versão deixou chamar o provedor")
	}

	m.clk.Advance(time.Minute)
	if est := m.aceitarOk(celA, "openai", nil); est.Aceite == nil || !est.Aceite.Vigente || est.Aceite.Versao != versaoDoAviso {
		t.Fatalf("depois de aceitar o texto atual: %+v", est.Aceite)
	}
	if l := m.aceites(m.userA); len(l) != 2 || abertos(l) != 1 || l[0].Motivo != cloudstore.FimSubstituido {
		t.Fatalf("os aceites: %+v", l)
	}
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT depois de aceitar o texto atual: %d", rec.Code)
	}
}

// As preferências só aceitam o provedor da transcrição, e o do Polish quando
// ele está ligado, que estejam no aceite vigente; trocar só o modelo não pede
// aceite novo. O que é recusado não grava.
func testPreferenciasForaDoAceite(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	exigirErro(t, m.preferencias(celA, "openai", "gpt-4o-mini-transcribe", false, false, "openai", ""), http.StatusForbidden, "SEM_ACEITE")
	if !m.lidas(m.userA).AtualizadaEm.IsZero() {
		t.Fatal("as preferências sem aceite gravaram")
	}
	if !strings.Contains(m.logs.String(), "motivo=sem_aceite") {
		t.Error("o log de acesso não diz o motivo do 403")
	}

	m.aceitarOk(celA, "openai", nil)
	rec := m.preferencias(celA, "openai", "gpt-4o-transcribe", false, false, "openai", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT das preferências com o aceite: %d %s", rec.Code, rec.Body.String())
	}
	est := decodificar[estadoDTO](t, rec)
	if est.Preferencias.Transcricao.Provedor != "openai" || est.Preferencias.Transcricao.Modelo != "gpt-4o-transcribe" || est.Aceite == nil {
		t.Fatalf("o PUT devolve o estado: %+v", est)
	}
	if p := m.lidas(m.userA); p.Transcricao.Modelo != "gpt-4o-transcribe" || !p.CriadaEm.Equal(agora0) || !p.AtualizadaEm.Equal(agora0) {
		t.Fatalf("as preferências guardadas: %+v", p)
	}

	// Mudar só o modelo do provedor aceito não pede aceite novo.
	m.clk.Advance(time.Hour)
	if rec := m.preferencias(celA, "openai", "gpt-4o-mini-transcribe", true, false, "openai", ""); rec.Code != http.StatusOK {
		t.Fatalf("trocar só o modelo: %d %s", rec.Code, rec.Body.String())
	}
	if l := m.aceites(m.userA); len(l) != 1 {
		t.Fatalf("trocar o modelo gravou um aceite novo (%d linhas)", len(l))
	}
	if p := m.lidas(m.userA); !p.Dica || p.Transcricao.Modelo != "gpt-4o-mini-transcribe" || !p.CriadaEm.Equal(agora0) ||
		!p.AtualizadaEm.Equal(agora0.Add(time.Hour)) {
		t.Fatalf("depois de trocar o modelo e ligar a dica: %+v", p)
	}
	antes := m.lidas(m.userA)

	// O que está fora do aceite é 403 SEM_ACEITE e não grava.
	exigirErro(t, m.preferencias(celA, "groq", "whisper-large-v3-turbo", false, false, "openai", ""), http.StatusForbidden, "SEM_ACEITE")
	exigirErro(t, m.preferencias(celA, "openai", "gpt-4o-mini-transcribe", false, true, "anthropic", "claude-x"), http.StatusForbidden, "SEM_ACEITE")
	if m.lidas(m.userA) != antes {
		t.Fatal("uma preferência recusada por falta de aceite gravou")
	}
	// O Polish ligado com o provedor que o aceite já cobre (a openai) passa; o
	// Polish desligado com um provedor fora do aceite também: é só a escolha
	// dela, e o aceite vale quando ligar.
	if rec := m.preferencias(celA, "openai", "gpt-4o-mini-transcribe", false, true, "openai", "gpt-x"); rec.Code != http.StatusOK {
		t.Fatalf("Polish ligado com o provedor aceito: %d %s", rec.Code, rec.Body.String())
	}
	if rec := m.preferencias(celA, "openai", "gpt-4o-mini-transcribe", false, false, "anthropic", ""); rec.Code != http.StatusOK {
		t.Fatalf("Polish desligado com um provedor fora do aceite: %d %s", rec.Code, rec.Body.String())
	}
	if p := m.lidas(m.userA); p.PolishLigado || p.Polish.Provedor != "anthropic" {
		t.Fatalf("o Polish desligado com a anthropic: %+v", p)
	}
	// O modelo da transcrição vazio é o padrão do provedor.
	if rec := m.preferencias(celA, "openai", "", false, false, "openai", ""); rec.Code != http.StatusOK {
		t.Fatalf("modelo vazio: %d", rec.Code)
	}
	if p := m.lidas(m.userA); p.Transcricao.Modelo != cloudstore.ModeloPadraoOpenAI {
		t.Fatalf("modelo vazio virou %q", p.Transcricao.Modelo)
	}

	// Aceite novo: a groq na transcrição e a anthropic no Polish.
	m.clk.Advance(time.Hour)
	m.aceitarOk(celA, "groq", texto("anthropic"))
	rec = m.preferencias(celA, "groq", "", false, true, "anthropic", "claude-y")
	if rec.Code != http.StatusOK {
		t.Fatalf("com o aceite novo: %d %s", rec.Code, rec.Body.String())
	}
	est = decodificar[estadoDTO](t, rec)
	if est.Preferencias.Transcricao.Provedor != "groq" || est.Preferencias.Transcricao.Modelo != cloudstore.ModeloPadraoGroq ||
		!est.Preferencias.Polish.Ligado || est.Preferencias.Polish.Provedor != "anthropic" || est.Preferencias.Polish.Modelo != "claude-y" {
		t.Fatalf("o estado depois do aceite novo: %+v", est.Preferencias)
	}
	// A openai saiu da cobertura: nem a transcrição nem o Polish.
	exigirErro(t, m.preferencias(celA, "openai", "", false, false, "openai", ""), http.StatusForbidden, "SEM_ACEITE")
	exigirErro(t, m.preferencias(celA, "groq", "", false, true, "openai", "gpt-x"), http.StatusForbidden, "SEM_ACEITE")
	if m.prov.quantas() != 0 {
		t.Fatal("as preferências chamaram o provedor")
	}
	// A outra conta não ganhou nada.
	if !m.lidas(m.userB).AtualizadaEm.IsZero() {
		t.Error("as preferências de A apareceram em B")
	}
}

// O PUT das preferências valida a forma, com frases fixas que não repetem o
// que veio, e o que é recusado não grava.
func testPreferenciasValidam(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", texto("anthropic"))
	const eco = "CANARIO<script>DO-MODELO"

	for nome, corpo := range map[string]string{
		"JSON quebrado":                `{"transcricao":`,
		"campo desconhecido":           `{"transcricao":{"provedor":"openai","modelo":"m"},"dica":false,"polish":{"ligado":false,"provedor":"openai","modelo":""},"` + eco + `":1}`,
		"campo desconhecido de dentro": `{"transcricao":{"provedor":"openai","modelo":"m","` + eco + `":1},"dica":false,"polish":{"ligado":false,"provedor":"openai","modelo":""}}`,
		"tipo errado":                  `{"transcricao":"` + eco + `","dica":false,"polish":{}}`,
		"o dono no corpo":              `{"user_id":"` + eco + `"}`,
		"vazio":                        ``,
	} {
		rec := m.pedir(celA, http.MethodPut, rotaPreferencias, corpo)
		if msg := exigirErro(t, rec, http.StatusBadRequest, "BAD_REQUEST"); msg != server.FraseDoCorpoDasPreferencias {
			t.Errorf("%s: %q, quero a frase fixa", nome, msg)
		}
		if strings.Contains(rec.Body.String(), eco) {
			t.Errorf("%s: a resposta repete o que veio", nome)
		}
	}
	// Falta um dos três campos.
	for nome, corpo := range map[string]string{
		"objeto vazio":      `{}`,
		"sem a dica":        `{"transcricao":{"provedor":"openai","modelo":"m"},"polish":{"ligado":false,"provedor":"openai","modelo":""}}`,
		"sem o Polish":      `{"transcricao":{"provedor":"openai","modelo":"m"},"dica":false}`,
		"sem a transcrição": `{"dica":false,"polish":{"ligado":false,"provedor":"openai","modelo":""}}`,
		"transcrição nula":  `{"transcricao":null,"dica":false,"polish":{"ligado":false,"provedor":"openai","modelo":""}}`,
	} {
		exigirErro(t, m.pedir(celA, http.MethodPut, rotaPreferencias, corpo), http.StatusBadRequest, "VALIDATION")
		_ = nome
	}
	exigirErro(t, m.pedir(celA, http.MethodPut, rotaPreferencias, `{"transcricao":{"provedor":"openai","modelo":"`+strings.Repeat("a", 5000)+`"}}`),
		http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")

	validacao := map[string]*httptest.ResponseRecorder{
		"transcrição pela anthropic": m.preferencias(celA, "anthropic", "m", false, false, "openai", ""),
		"transcrição fora da lista":  m.preferencias(celA, "xai", "m", false, false, "openai", ""),
		"transcrição em maiúsculas":  m.preferencias(celA, "OpenAI", "m", false, false, "openai", ""),
		"transcrição vazia":          m.preferencias(celA, "", "m", false, false, "openai", ""),
		"Polish pela groq":           m.preferencias(celA, "openai", "m", false, false, "groq", ""),
		"Polish vazio":               m.preferencias(celA, "openai", "m", false, false, "", ""),
		"Polish ligado sem modelo":   m.preferencias(celA, "openai", "m", false, true, "openai", ""),
		"modelo com espaço":          m.preferencias(celA, "openai", "gpt 4", false, false, "openai", ""),
		"modelo com quebra de linha": m.preferencias(celA, "openai", "gpt\n4", false, false, "openai", ""),
		"modelo com acento":          m.preferencias(celA, "openai", "modelo-é", false, false, "openai", ""),
		"modelo de 101 caracteres":   m.preferencias(celA, "openai", strings.Repeat("m", 101), false, false, "openai", ""),
		"modelo com ponto e vírgula": m.preferencias(celA, "openai", "a;b", false, false, "openai", ""),
		"modelo com barra invertida": m.preferencias(celA, "openai", `a\b`, false, false, "openai", ""),
		"modelo do Polish ruim":      m.preferencias(celA, "openai", "m", false, false, "openai", "polish ruim"),
		"modelo ruim e o eco":        m.preferencias(celA, "openai", eco, false, false, "openai", ""),
	}
	for nome, rec := range validacao {
		if code, _ := erroDe(t, rec); rec.Code != http.StatusBadRequest || code != "VALIDATION" {
			t.Errorf("%s: %d %s, quero 400 VALIDATION", nome, rec.Code, code)
		}
		if strings.Contains(rec.Body.String(), eco) {
			t.Errorf("%s: a resposta repete o modelo que veio", nome)
		}
	}
	if !m.lidas(m.userA).AtualizadaEm.IsZero() {
		t.Fatal("uma preferência recusada pela validação gravou")
	}

	// As formas que valem: o teto de 100, a barra, os dois-pontos, o @.
	for _, modelo := range []string{strings.Repeat("m", 100), "openai/gpt-oss-20b", "ft:gpt-4o-mini:org::id", "a.b_c-d@e", "gpt-4o-mini-transcribe-2025-12-15"} {
		if rec := m.preferencias(celA, "openai", modelo, false, false, "openai", ""); rec.Code != http.StatusOK {
			t.Errorf("o modelo %q devia valer: %d %s", modelo, rec.Code, rec.Body.String())
		}
	}
	if p := m.lidas(m.userA); p.Transcricao.Modelo != "gpt-4o-mini-transcribe-2025-12-15" {
		t.Errorf("o último modelo válido: %q", p.Transcricao.Modelo)
	}
}

// Revogar ("Desligar o ditado no celular") termina o aceite e apaga TODAS as
// chaves da conta, também as de um provedor que o aceite já não cobria. O
// aceite fica gravado, terminado como revogado. Depois, nada passa; e a conta
// pode aceitar de novo. Não haver aceite não é erro. A outra conta não sente.
func testRevogar(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	ctx := context.Background()
	// Uma chave da groq, de um aceite que já foi trocado: ela fica guardada.
	m.aceitarOk(celA, "groq", nil)
	m.put(celA, "groq", canario)
	m.clk.Advance(time.Minute)
	m.aceitarOk(celA, "openai", texto("anthropic"))
	m.put(celA, "openai", canario)
	m.put(celA, "anthropic", canario)
	m.aceitarOk(celB, "openai", nil)
	m.put(celB, "openai", canario)
	if rec := m.preferencias(celA, "openai", "gpt-4o-transcribe", true, false, "anthropic", ""); rec.Code != http.StatusOK {
		t.Fatalf("PUT das preferências: %d", rec.Code)
	}
	if l, _ := m.b.Chaves.ListarChavesDeIA(ctx, m.userA); len(l) != 3 {
		t.Fatalf("antes de revogar, A tem %d chaves, quero 3", len(l))
	}

	m.clk.Advance(time.Hour)
	fim := m.clk.Now()
	if rec := m.pedir(celA, http.MethodDelete, rotaAceite, ""); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE /dictation/consent: %d %q", rec.Code, rec.Body.String())
	}
	l := m.aceites(m.userA)
	if len(l) != 2 || abertos(l) != 0 {
		t.Fatalf("depois de revogar: %+v", l)
	}
	if u := l[1]; u.Motivo != cloudstore.FimRevogado || !u.EncerradoEm.Equal(fim) || u.Transcricao != "openai" || u.Polish != "anthropic" {
		t.Errorf("o aceite revogado: %+v, quero terminado como revogado em %v", u, fim)
	}
	if l[0].Motivo != cloudstore.FimSubstituido {
		t.Errorf("a revogação mexeu no aceite que já tinha terminado: %+v", l[0])
	}
	if _, err := m.b.Ditado.LerAceiteAberto(ctx, m.userA); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("LerAceiteAberto depois de revogar: %v", err)
	}
	if ch, _ := m.b.Chaves.ListarChavesDeIA(ctx, m.userA); len(ch) != 0 {
		t.Fatalf("revogar deixou %d chaves", len(ch))
	}
	if !strings.Contains(m.logs.String(), "chaves_apagadas=3") {
		t.Error("o log de acesso não conta as chaves apagadas")
	}
	// A outra conta ficou como estava.
	if l := m.aceites(m.userB); len(l) != 1 || !l[0].Aberto() {
		t.Errorf("revogar o aceite de A mexeu no de B: %+v", l)
	}
	if _, err := m.linha(m.userB, "openai"); err != nil {
		t.Errorf("revogar o aceite de A apagou a chave de B: %v", err)
	}

	// Sem aceite, nada passa: nem as chaves, nem a lista, nem as preferências.
	exigirErro(t, m.put(celA, "openai", canario), http.StatusForbidden, "SEM_ACEITE")
	exigirErro(t, m.modelos(celA, "openai"), http.StatusForbidden, "SEM_ACEITE")
	exigirErro(t, m.preferencias(celA, "openai", "", false, false, "openai", ""), http.StatusForbidden, "SEM_ACEITE")
	est := m.estado(celA)
	if est.Aceite != nil {
		t.Errorf("o estado depois de revogar traz o aceite %+v", est.Aceite)
	}
	for _, k := range est.Chaves {
		if k.Configurada || k.ColarDeNovo || k.Final != "" {
			t.Errorf("a chave %+v ficou depois de revogar", k)
		}
	}
	// As preferências continuam (são só escolhas, sem segredo).
	if !est.Preferencias.Dica || est.Preferencias.Transcricao.Modelo != "gpt-4o-transcribe" {
		t.Errorf("as preferências depois de revogar: %+v", est.Preferencias)
	}

	// Revogar de novo não é erro, e não muda nada.
	if rec := m.pedir(celA, http.MethodDelete, rotaAceite, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revogar sem aceite: %d", rec.Code)
	}
	if again := m.aceites(m.userA); len(again) != 2 || !again[1].EncerradoEm.Equal(fim) {
		t.Fatalf("revogar de novo mexeu no histórico: %+v", again)
	}
	// Quem nunca aceitou também revoga sem erro.
	m.pedir(celB, http.MethodDelete, rotaAceite, "")
	if rec := m.pedir(celB, http.MethodDelete, rotaAceite, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revogar duas vezes: %d", rec.Code)
	}

	// Aceitar de novo: uma linha nova, e as chaves se colam de novo.
	m.clk.Advance(time.Hour)
	m.aceitarOk(celA, "openai", nil)
	if l := m.aceites(m.userA); len(l) != 3 || abertos(l) != 1 {
		t.Fatalf("depois de aceitar de novo: %+v", l)
	}
	exigirErro(t, m.modelos(celA, "openai"), http.StatusConflict, "SEM_CHAVE")
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT depois de aceitar de novo: %d", rec.Code)
	}
}

// Uma chave guardada com outra chave de cifra (o arquivo trocou, ou o backup
// voltou sem ele) aparece no estado como "cole de novo", sem decifrar e sem
// erro; colar de novo a devolve. O estado nunca traz a chave.
func testChaveQueNaoVale(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d", rec.Code)
	}
	if k := m.estado(celA).Chaves[0]; !k.Configurada || k.Final != "WXYZ" || k.ColarDeNovo {
		t.Fatalf("a chave no estado: %+v", k)
	}

	m.montar(ChaveDeCifraB)
	rec := m.pedir(celA, http.MethodGet, rotaEstado, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dictation com a outra chave de cifra: %d %s", rec.Code, rec.Body.String())
	}
	semCanario(t, "o estado do ditado", rec.Body.String(), canario)
	est := decodificar[estadoDTO](t, rec)
	if k := est.Chaves[0]; k.Provedor != "openai" || k.Configurada || k.Final != "" || !k.ColarDeNovo {
		t.Fatalf("a chave cifrada por outra chave de cifra: %+v, quero colar_de_novo", k)
	}
	if est.Aceite == nil || !est.Aceite.Vigente {
		t.Errorf("trocar a chave de cifra mexeu no aceite: %+v", est.Aceite)
	}
	if rec := m.put(celA, "openai", canario); rec.Code != http.StatusOK {
		t.Fatalf("colar de novo: %d %s", rec.Code, rec.Body.String())
	}
	if k := m.estado(celA).Chaves[0]; !k.Configurada || k.Final != "WXYZ" || k.ColarDeNovo {
		t.Fatalf("a chave depois de colar de novo: %+v", k)
	}
}

// O "apagar meus dados" apaga as três tabelas do ditado no celular da conta
// (as chaves, as preferências e todos os aceites, os que terminaram também), e
// só as dela.
func testApagarAsTres(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	ctx := context.Background()
	m.aceitarOk(celA, "groq", nil)
	m.clk.Advance(time.Minute)
	m.aceitarOk(celA, "openai", texto("anthropic"))
	m.aceitarOk(celB, "openai", nil)
	for _, c := range []struct{ quem, prov string }{{celA, "openai"}, {celA, "anthropic"}, {celB, "openai"}} {
		if rec := m.put(c.quem, c.prov, canario); rec.Code != http.StatusOK {
			t.Fatalf("PUT %s %s: %d", c.quem, c.prov, rec.Code)
		}
	}
	for _, quem := range []string{celA, celB} {
		if rec := m.preferencias(quem, "openai", "gpt-4o-transcribe", true, false, "openai", ""); rec.Code != http.StatusOK {
			t.Fatalf("preferências de %s: %d", quem, rec.Code)
		}
	}
	idsA, idsB := idsDosAceites(m.aceites(m.userA)), idsDosAceites(m.aceites(m.userB))
	if len(idsA) != 2 || len(idsB) != 1 {
		t.Fatalf("antes de apagar: %d aceites de A e %d de B", len(idsA), len(idsB))
	}
	despejo := m.b.Despejo(t)
	for _, id := range append(append([]string{}, idsA...), idsB...) {
		if !strings.Contains(despejo, id) {
			t.Fatalf("o despejo não leu o aceite %s (a varredura não vê a tabela)", id)
		}
	}

	if rec := m.pedir(celA, http.MethodDelete, "/api/v1/me/data", `{"confirmar":"apagar"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me/data: %d %s", rec.Code, rec.Body.String())
	}
	if l := m.aceites(m.userA); len(l) != 0 {
		t.Errorf("depois de apagar, A tem %d aceites", len(l))
	}
	if _, err := m.b.Ditado.LerAceiteAberto(ctx, m.userA); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("LerAceiteAberto de A: %v", err)
	}
	if p := m.lidas(m.userA); !p.AtualizadaEm.IsZero() || p.Transcricao.Modelo != cloudstore.ModeloPadraoOpenAI || p.Dica {
		t.Errorf("as preferências de A depois de apagar: %+v, quero os padrões", p)
	}
	if l, err := m.b.Chaves.ListarChavesDeIA(ctx, m.userA); err != nil || len(l) != 0 {
		t.Errorf("depois de apagar, A tem %d chaves (%v)", len(l), err)
	}
	if est := m.estado(celA); est.Aceite != nil || est.Preferencias.Dica {
		t.Errorf("o estado de A depois de apagar: %+v", est)
	}
	despejo = m.b.Despejo(t)
	for _, id := range idsA {
		if strings.Contains(despejo, id) {
			t.Errorf("o aceite %s de A continua no banco", id)
		}
	}
	// B ficou inteira.
	if !strings.Contains(despejo, idsB[0]) {
		t.Error("apagar os dados de A levou o aceite de B")
	}
	if l := m.aceites(m.userB); len(l) != 1 || !l[0].Aberto() {
		t.Errorf("o aceite de B: %+v", l)
	}
	if p := m.lidas(m.userB); !p.Dica || p.Transcricao.Modelo != "gpt-4o-transcribe" {
		t.Errorf("as preferências de B: %+v", p)
	}
	if l, _ := m.b.Chaves.ListarChavesDeIA(ctx, m.userB); len(l) != 1 {
		t.Errorf("as chaves de B: %d", len(l))
	}
}

func idsDosAceites(l []cloudstore.Aceite) []string {
	var ids []string
	for _, a := range l {
		ids = append(ids, a.ID)
	}
	return ids
}

// As quatro rotas são só da sessão: o token de um PC recebe 403, e sem
// credencial é 401. Nada chega ao store do ditado.
func testDitadoSoComSessao(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	for _, r := range []struct{ method, path, corpo string }{
		{http.MethodGet, rotaEstado, ""},
		{http.MethodPut, rotaPreferencias, `{"transcricao":{"provedor":"openai","modelo":"m"},"dica":false,"polish":{"ligado":false,"provedor":"openai","modelo":""}}`},
		{http.MethodPost, rotaAceite, fmt.Sprintf(`{"versao":%d,"transcricao":"groq"}`, versaoDoAviso)},
		{http.MethodDelete, rotaAceite, ""},
	} {
		exigirErro(t, m.pedir(pcA, r.method, r.path, r.corpo), http.StatusForbidden, "FORBIDDEN")
		exigirErro(t, m.pedir("", r.method, r.path, r.corpo), http.StatusUnauthorized, "UNAUTHORIZED")
	}
	if l := m.aceites(m.userA); len(l) != 1 || !l[0].Aberto() || l[0].Transcricao != "openai" {
		t.Fatalf("um pedido sem sessão mexeu no aceite: %+v", l)
	}
	if !m.lidas(m.userA).AtualizadaEm.IsZero() {
		t.Fatal("um pedido sem sessão gravou as preferências")
	}
	achadas := 0
	for _, rt := range m.cloud.Rotas() {
		// A rota de ditar (/dictations, C5b) tem o acesso dela, o AcessoDitar:
		// é conferida em ditar.go.
		if strings.Contains(rt.Padrao, "/dictation") && !strings.Contains(rt.Padrao, "/dictations") {
			achadas++
			if rt.Acesso != server.AcessoSessao {
				t.Errorf("%s com acesso %v, quero só a sessão", rt.Padrao, rt.Acesso)
			}
			if strings.ContainsAny(rt.Padrao, "{}?") {
				t.Errorf("%s tem parâmetro na URL", rt.Padrao)
			}
		}
	}
	if achadas != 4 {
		t.Errorf("%d rotas /dictation na tabela, quero 4", achadas)
	}
}

// Sem o arquivo da chave de cifra, as quatro rotas não são montadas: 404 de
// rota não montada para qualquer credencial. E o cofre sem onde guardar o
// aceite não sobe.
func testDitadoSemOArquivo(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", nil)
	m.montar("")
	for _, r := range []struct{ method, path, corpo string }{
		{http.MethodGet, rotaEstado, ""},
		{http.MethodPut, rotaPreferencias, "{}"},
		{http.MethodPost, rotaAceite, fmt.Sprintf(`{"versao":%d,"transcricao":"groq"}`, versaoDoAviso)},
		{http.MethodDelete, rotaAceite, ""},
	} {
		for _, quem := range []string{celA, pcA, ""} {
			if msg := exigirErro(t, m.pedir(quem, r.method, r.path, r.corpo), http.StatusNotFound, "NOT_FOUND"); msg != "rota não encontrada" {
				t.Errorf("%s %s (%q): %q, quero o 404 de rota não montada", r.method, r.path, quem, msg)
			}
		}
	}
	if l := m.aceites(m.userA); len(l) != 1 || !l[0].Aberto() {
		t.Errorf("sem o arquivo, um pedido mexeu no aceite: %+v", l)
	}
	for _, rt := range m.cloud.Rotas() {
		if strings.Contains(rt.Padrao, "/dictation") {
			t.Errorf("sem o arquivo, a tabela tem %s", rt.Padrao)
		}
	}
	c, err := cofre.Novo(ChaveDeCifraA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.NewCloud(server.CloudConfig{
		Store: m.b.Store, Accounts: m.b.Accounts, BaseURL: contracttest.BaseURL,
		SessionSecret: segredoDeSessao, DeviceTokenPepper: pepperDeToken, Contas: m.contas,
		Google: cloudauth.NovoGoogle("c", "s", contracttest.BaseURL+"/auth/google/callback"),
		Painel: contracttest.PainelDeTeste(), Cofre: c, ChavesDeIA: m.b.Chaves, Modelos: m.prov,
	}); err == nil {
		t.Error("o cofre sem o Ditado subiu")
	}
}

// O DitadoNoCelular direto: a conferência antes do banco, a primeira data que
// fica, a leitura dos padrões, o aceite que substitui, as contas que não se
// veem, e o que cada método devolve para um usuário que não existe.
func testDitadoStore(t *testing.T, m *mundo) {
	ctx := context.Background()
	s := m.b.Ditado
	boa := func() cloudstore.PreferenciasDoDitado {
		return cloudstore.PreferenciasDoDitado{
			Transcricao: cloudstore.ProvedorEModelo{Provedor: "groq", Modelo: "whisper-large-v3-turbo"},
			Polish:      cloudstore.ProvedorEModelo{Provedor: "anthropic", Modelo: "claude-x"}, PolishLigado: true, Dica: true,
		}
	}
	for nome, c := range map[string]struct {
		mudar func(p *cloudstore.PreferenciasDoDitado)
		quero error
	}{
		"transcrição pela anthropic": {func(p *cloudstore.PreferenciasDoDitado) { p.Transcricao.Provedor = "anthropic" }, cloudstore.ErrProvedorDeTranscricao},
		"transcrição em maiúsculas":  {func(p *cloudstore.PreferenciasDoDitado) { p.Transcricao.Provedor = "Groq" }, cloudstore.ErrProvedorDeTranscricao},
		"Polish pela groq":           {func(p *cloudstore.PreferenciasDoDitado) { p.Polish.Provedor = "groq" }, cloudstore.ErrProvedorDePolish},
		"modelo vazio":               {func(p *cloudstore.PreferenciasDoDitado) { p.Transcricao.Modelo = "" }, cloudstore.ErrModeloInvalido},
		"modelo com espaço":          {func(p *cloudstore.PreferenciasDoDitado) { p.Transcricao.Modelo = "a b" }, cloudstore.ErrModeloInvalido},
		"modelo de 101":              {func(p *cloudstore.PreferenciasDoDitado) { p.Transcricao.Modelo = strings.Repeat("a", 101) }, cloudstore.ErrModeloInvalido},
		"modelo do Polish ruim":      {func(p *cloudstore.PreferenciasDoDitado) { p.Polish.Modelo = "a b" }, cloudstore.ErrModeloInvalido},
		"Polish ligado sem modelo":   {func(p *cloudstore.PreferenciasDoDitado) { p.Polish.Modelo = "" }, cloudstore.ErrPolishSemModelo},
	} {
		p := boa()
		c.mudar(&p)
		if _, err := s.GravarPreferenciasDoDitado(ctx, m.userA, p, agora0); !errors.Is(err, c.quero) {
			t.Errorf("%s: %v, quero %v", nome, err, c.quero)
		}
	}
	for _, u := range []string{contracttest.NewID(), "nao-e-uuid"} {
		if _, err := s.GravarPreferenciasDoDitado(ctx, u, boa(), agora0); !errors.Is(err, cloudstore.ErrUnknownUser) {
			t.Errorf("preferências do usuário %q: %v, quero ErrUnknownUser", u, err)
		}
		if _, err := s.GravarAceite(ctx, u, cloudstore.NovoAceite{Versao: 1, Transcricao: "openai"}, agora0); !errors.Is(err, cloudstore.ErrUnknownUser) {
			t.Errorf("aceite do usuário %q: %v, quero ErrUnknownUser", u, err)
		}
		if p, err := s.LerPreferenciasDoDitado(ctx, u); err != nil || p != cloudstore.PreferenciasPadrao() {
			t.Errorf("ler as preferências do usuário %q: %+v, %v, quero os padrões", u, p, err)
		}
		if _, err := s.LerAceiteAberto(ctx, u); !errors.Is(err, cloudstore.ErrSemAceite) {
			t.Errorf("ler o aceite do usuário %q: %v", u, err)
		}
		if r, err := s.RevogarAceite(ctx, u, agora0); err != nil || r != (cloudstore.Revogacao{}) {
			t.Errorf("revogar o aceite do usuário %q: %+v, %v", u, r, err)
		}
		if l, err := s.ListarAceites(ctx, u); err != nil || l == nil || len(l) != 0 {
			t.Errorf("listar os aceites do usuário %q: %v, %v", u, l, err)
		}
	}

	// As preferências: a primeira gravação fica como a data de criação.
	got, err := s.GravarPreferenciasDoDitado(ctx, m.userA, boa(), agora0)
	if err != nil || !got.CriadaEm.Equal(agora0) || !got.AtualizadaEm.Equal(agora0) {
		t.Fatalf("Gravar = %+v, %v", got, err)
	}
	depois := agora0.Add(time.Hour)
	outra := boa()
	outra.Dica, outra.PolishLigado = false, false
	if got, err = s.GravarPreferenciasDoDitado(ctx, m.userA, outra, depois); err != nil || !got.CriadaEm.Equal(agora0) || !got.AtualizadaEm.Equal(depois) {
		t.Fatalf("por cima = %+v, %v", got, err)
	}
	if lida := m.lidas(m.userA); lida.Dica || lida.PolishLigado || lida.Polish.Modelo != "claude-x" || !lida.CriadaEm.Equal(agora0) || !lida.AtualizadaEm.Equal(depois) {
		t.Fatalf("Ler = %+v", lida)
	}
	// O relógio que volta não põe a troca antes da criação.
	if got, err = s.GravarPreferenciasDoDitado(ctx, m.userA, boa(), agora0.Add(-time.Hour)); err != nil || got.AtualizadaEm.Before(got.CriadaEm) {
		t.Fatalf("com o relógio voltando: %+v, %v", got, err)
	}
	if p := m.lidas(m.userB); !p.AtualizadaEm.IsZero() {
		t.Errorf("B leu as preferências de A: %+v", p)
	}

	// O aceite.
	for nome, n := range map[string]cloudstore.NovoAceite{
		"versão zero":                {Versao: 0, Transcricao: "openai"},
		"versão negativa":            {Versao: -1, Transcricao: "openai"},
		"transcrição pela anthropic": {Versao: 1, Transcricao: "anthropic"},
		"transcrição vazia":          {Versao: 1},
		"Polish pela groq":           {Versao: 1, Transcricao: "openai", Polish: "groq"},
	} {
		if _, err := s.GravarAceite(ctx, m.userA, n, agora0); !errors.Is(err, cloudstore.ErrAceiteInvalido) {
			t.Errorf("%s: %v, quero ErrAceiteInvalido", nome, err)
		}
	}
	if _, err := s.LerAceiteAberto(ctx, m.userA); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Fatalf("Ler antes de gravar: %v", err)
	}
	primeiro, err := s.GravarAceite(ctx, m.userA, cloudstore.NovoAceite{Versao: 1, Transcricao: "openai", Polish: "openai"}, agora0)
	if err != nil || !primeiro.Aberto() || !primeiro.AceitoEm.Equal(agora0) || primeiro.Polish != "openai" {
		t.Fatalf("GravarAceite = %+v, %v", primeiro, err)
	}
	if !primeiro.Inclui("openai") || primeiro.Inclui("groq") || primeiro.Inclui("") {
		t.Errorf("Inclui: %+v", primeiro)
	}
	// Repetir o mesmo devolve a mesma linha.
	igual, err := s.GravarAceite(ctx, m.userA, cloudstore.NovoAceite{Versao: 1, Transcricao: "openai", Polish: "openai"}, depois)
	if err != nil || igual.ID != primeiro.ID || !igual.AceitoEm.Equal(agora0) {
		t.Fatalf("repetir o aceite = %+v, %v", igual, err)
	}
	segundo, err := s.GravarAceite(ctx, m.userA, cloudstore.NovoAceite{Versao: 1, Transcricao: "groq"}, depois)
	if err != nil || segundo.ID == primeiro.ID {
		t.Fatalf("o aceite novo = %+v, %v", segundo, err)
	}
	lista, err := s.ListarAceites(ctx, m.userA)
	if err != nil || len(lista) != 2 || lista[0].ID != primeiro.ID || lista[1].ID != segundo.ID {
		t.Fatalf("Listar = %+v, %v; quero o mais velho primeiro", lista, err)
	}
	if lista[0].Aberto() || lista[0].Motivo != cloudstore.FimSubstituido || !lista[0].EncerradoEm.Equal(depois) {
		t.Errorf("o primeiro depois de substituído: %+v", lista[0])
	}
	// O relógio que volta: o fim nunca fica antes do começo.
	terceiro, err := s.GravarAceite(ctx, m.userA, cloudstore.NovoAceite{Versao: 1, Transcricao: "openai"}, agora0.Add(-time.Hour))
	if err != nil {
		t.Fatalf("com o relógio voltando: %v", err)
	}
	for _, a := range m.aceites(m.userA) {
		if !a.Aberto() && a.EncerradoEm.Before(a.AceitoEm) {
			t.Errorf("o aceite %+v termina antes de começar", a)
		}
	}
	// B não vê nada de A.
	if _, err := s.LerAceiteAberto(ctx, m.userB); !errors.Is(err, cloudstore.ErrSemAceite) {
		t.Errorf("B leu o aceite de A: %v", err)
	}
	if l := m.aceites(m.userB); len(l) != 0 {
		t.Errorf("B lista os aceites de A: %+v", l)
	}
	// A lista vai do mais velho ao mais novo pela data do aceite: o terceiro,
	// de um relógio que voltou, vem antes dos outros.
	if u := m.aceites(m.userA); len(u) != 3 || u[0].ID != terceiro.ID {
		t.Errorf("a lista com o relógio voltando: %+v", u)
	}
	// Revogar sem chave: Revogou, 0 chaves; a lista mantém a linha.
	if r, err := s.RevogarAceite(ctx, m.userB, depois); err != nil || r.Revogou || r.ChavesApagadas != 0 {
		t.Errorf("revogar o que não existe: %+v, %v", r, err)
	}
	if r, err := s.RevogarAceite(ctx, m.userA, depois.Add(time.Hour)); err != nil || !r.Revogou || r.ChavesApagadas != 0 {
		t.Errorf("revogar: %+v, %v", r, err)
	}
	var revogado cloudstore.Aceite
	for _, a := range m.aceites(m.userA) {
		if a.ID == terceiro.ID {
			revogado = a
		}
	}
	if revogado.Motivo != cloudstore.FimRevogado || !revogado.EncerradoEm.Equal(depois.Add(time.Hour)) {
		t.Errorf("o aceite revogado: %+v", revogado)
	}
	// Sem aceite, as chaves ainda saem (e dizem quantas).
	for _, p := range []string{"openai", "groq"} {
		if _, err := m.b.Chaves.GravarChaveDeIA(ctx, m.userB, cloudstore.ChaveDeIA{
			Provedor: p, IDDaCifra: make([]byte, 8), Nonce: make([]byte, 12), Cifrado: make([]byte, 40), Final: "WXYZ",
		}, agora0); err != nil {
			t.Fatal(err)
		}
	}
	if r, err := s.RevogarAceite(ctx, m.userB, depois); err != nil || r.Revogou || r.ChavesApagadas != 2 {
		t.Errorf("revogar só com chaves: %+v, %v, quero Revogou false e 2 chaves", r, err)
	}
	if l, _ := m.b.Chaves.ListarChavesDeIA(ctx, m.userB); len(l) != 0 {
		t.Errorf("revogar deixou %d chaves", len(l))
	}
}

// Várias gravações do aceite ao mesmo tempo (e uma revogação no meio) nunca
// deixam dois aceites em aberto nem dão erro: a segunda vê o que a primeira
// deixou.
func testDitadoConcorrencia(t *testing.T, m *mundo) {
	ctx := context.Background()
	provedores := []string{"openai", "groq"}
	var wg sync.WaitGroup
	erros := make(chan error, 32)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := cloudstore.NovoAceite{Versao: 1, Transcricao: provedores[i%2]}
			if i%4 == 0 {
				n.Polish = "anthropic"
			}
			if _, err := m.b.Ditado.GravarAceite(ctx, m.userA, n, agora0.Add(time.Duration(i)*time.Second)); err != nil {
				erros <- fmt.Errorf("GravarAceite %d: %w", i, err)
			}
			if i == 8 {
				if _, err := m.b.Ditado.RevogarAceite(ctx, m.userA, agora0.Add(time.Hour)); err != nil {
					erros <- fmt.Errorf("RevogarAceite: %w", err)
				}
			}
		}()
	}
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Error(err)
	}
	l := m.aceites(m.userA)
	if n := abertos(l); n > 1 {
		t.Fatalf("%d aceites em aberto, quero no máximo 1: %+v", n, l)
	}
	if len(l) < 2 || len(l) > 16 {
		t.Fatalf("%d aceites gravados", len(l))
	}
	for _, a := range l {
		if !a.Aberto() && (a.EncerradoEm.Before(a.AceitoEm) || (a.Motivo != cloudstore.FimSubstituido && a.Motivo != cloudstore.FimRevogado)) {
			t.Errorf("aceite terminado errado: %+v", a)
		}
	}
	// Quem lê o aberto lê o mesmo que a lista diz.
	aberto, err := m.b.Ditado.LerAceiteAberto(ctx, m.userA)
	switch {
	case abertos(l) == 0 && !errors.Is(err, cloudstore.ErrSemAceite):
		t.Errorf("sem aceite em aberto, LerAceiteAberto = %+v, %v", aberto, err)
	case abertos(l) == 1 && err != nil:
		t.Errorf("LerAceiteAberto: %v", err)
	}
}
