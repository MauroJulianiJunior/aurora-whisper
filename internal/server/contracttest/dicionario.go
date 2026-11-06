package contracttest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
)

// A cópia do dicionário de um PC na nuvem (ADR-023; dicionario_routes.go e
// cloudstore/dicionario.go), pela API HTTP, contra os dois Stores: o tomar e o
// manter em cada estado da fonte (nenhuma, este, outro), o reenvio do mesmo
// SHA-256 que não grava, o SHA-256 e os cabeçalhos errados, a cópia inválida em
// cada motivo, o DELETE de quem não é a fonte, o 403 da sessão do navegador, a
// revogação e o "Apagar meus dados" que apagam a cópia, e o log sem termo.

const caminhoDoDicionario = "/api/v1/sync/dictionary"

// chavesDoEstadoDaCopia são as chaves exatas do estado com cópia; sem cópia, só
// a fonte.
var chavesDoEstadoDaCopia = []string{"aparelho", "atualizado_em", "fonte", "sha256", "termos"}

// estadoJSON é o estado da cópia no fio.
type estadoJSON struct {
	Fonte        string     `json:"fonte"`
	Aparelho     string     `json:"aparelho"`
	Termos       *int       `json:"termos"`
	SHA256       string     `json:"sha256"`
	AtualizadoEm *time.Time `json:"atualizado_em"`
}

// copiaDaSuite é a cópia de um dicionário com os termos dados (cada um com uma
// variante), e o SHA-256 dos bytes.
func copiaDaSuite(t *testing.T, termos ...string) ([]byte, string) {
	t.Helper()
	s := dictionary.Semente{Usuario: "local"}
	for _, termo := range termos {
		s.Entradas = append(s.Entradas, dictionary.EntradaSemente{Canonica: termo, Prioridade: 5,
			Alias: []string{strings.ToLower(termo) + " errado"}})
	}
	b, sha, err := s.BytesDaCopia()
	if err != nil {
		t.Fatalf("a cópia da suíte não vale: %v", err)
	}
	return b, sha
}

// putCopia é o PUT da cópia, com o SHA-256 dos bytes e o modo.
func (w *world) putCopia(token string, corpo []byte, modo string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.putCopiaCom(token, corpo, map[string]string{
		"Dicionario-SHA256": dictionary.SHA256DaCopia(corpo), "Dicionario-Fonte": modo,
	})
}

// putCopiaCom é o PUT com os cabeçalhos dados (e o Content-Type de JSON).
func (w *world) putCopiaCom(token string, corpo []byte, cab map[string]string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.doBytes(http.MethodPut, caminhoDoDicionario, token, corpo, "application/json", cab)
}

// estadoDaCopia lê o estado pelo GET.
func (w *world) estadoDaCopia(token string) estadoJSON {
	w.t.Helper()
	rec := w.do(http.MethodGet, caminhoDoDicionario, token, nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET do dicionário: %d %s", rec.Code, rec.Body.String())
	}
	return lerEstadoDaCopia(w.t, rec.Body.Bytes())
}

// lerEstadoDaCopia decodifica o estado, com as chaves exatas.
func lerEstadoDaCopia(t *testing.T, raw []byte) estadoJSON {
	t.Helper()
	var e estadoJSON
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("o estado não é JSON: %v\n%s", err, raw)
	}
	if e.Fonte == "nenhuma" {
		conferirChaves(t, raw, []string{"fonte"}, "o estado sem cópia")
	} else {
		conferirChaves(t, raw, chavesDoEstadoDaCopia, "o estado com cópia")
	}
	return e
}

// lerEstadoDoConflito lê o estado do 409: o mesmo corpo do GET, com o bloco error
// ao lado.
func lerEstadoDoConflito(t *testing.T, raw []byte) estadoJSON {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("o 409 não é JSON: %v: %s", err, raw)
	}
	if _, ok := obj["error"]; !ok {
		t.Fatalf("o 409 sem o bloco error: %s", raw)
	}
	delete(obj, "error")
	sem, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return lerEstadoDaCopia(t, sem)
}

// querEstado confere o estado: a fonte, os termos e o SHA-256.
func querEstado(t *testing.T, o string, e estadoJSON, fonte string, termos int, sha string) {
	t.Helper()
	if e.Fonte != fonte || e.Termos == nil || *e.Termos != termos || e.SHA256 != sha || e.Aparelho == "" || e.AtualizadoEm == nil {
		t.Fatalf("%s: o estado %+v, quero fonte %q, %d termos, sha %s (e o aparelho e a hora)", o, e, fonte, termos, sha)
	}
}

func querSemCopia(t *testing.T, o string, e estadoJSON) {
	t.Helper()
	if e.Fonte != "nenhuma" || e.Termos != nil || e.SHA256 != "" || e.Aparelho != "" || e.AtualizadoEm != nil {
		t.Fatalf("%s: o estado %+v, quero nenhuma cópia", o, e)
	}
}

// erroDaCopia lê o motivo do 422 (e confere o código).
func erroDaCopia(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	e := decode[struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
			Motivo    string `json:"motivo"`
		} `json:"error"`
	}](t, rec)
	if rec.Code != http.StatusUnprocessableEntity || e.Error.Code != "DICIONARIO_INVALIDO" || e.Error.Message == "" || e.Error.RequestID == "" {
		t.Fatalf("a cópia inválida: %d %s, quero 422 DICIONARIO_INVALIDO", rec.Code, rec.Body.String())
	}
	return e.Error.Motivo
}

// tomar e manter em cada estado da fonte; o reenvio que não grava.
func testDicionarioTomarEManter(t *testing.T, w *world) {
	// Sem cópia: o GET diz nenhuma, e o manter (o envio automático) não grava.
	querSemCopia(t, "o GET sem cópia", w.estadoDaCopia(pcA1))
	c1, sha1 := copiaDaSuite(t, "Aurora", "Whisper")
	rec := w.putCopia(pcA1, c1, "manter")
	querStatus(t, "manter sem cópia", rec, http.StatusConflict, "NAO_E_A_FONTE")
	querSemCopia(t, "o 409 sem cópia leva o estado", lerEstadoDoConflito(t, rec.Body.Bytes()))
	querSemCopia(t, "o manter sem cópia não gravou", w.estadoDaCopia(pcA1))

	// Tomar grava, e este PC é a fonte.
	rec = w.putCopia(pcA1, c1, "tomar")
	if rec.Code != http.StatusOK {
		t.Fatalf("tomar: %d %s", rec.Code, rec.Body.String())
	}
	e := lerEstadoDaCopia(t, rec.Body.Bytes())
	querEstado(t, "tomar", e, "este", 2, sha1)
	if !e.AtualizadoEm.Equal(now0) {
		t.Errorf("a hora da cópia = %s, quero a do servidor %s", e.AtualizadoEm, now0)
	}
	querEstado(t, "o GET do PC fonte", w.estadoDaCopia(pcA1), "este", 2, sha1)
	// O outro PC da conta vê a fonte como outro, com o nome do PC.
	outro := w.estadoDaCopia(pcA2)
	querEstado(t, "o GET de outro PC", outro, "outro", 2, sha1)

	// O reenvio do mesmo SHA-256 do mesmo PC é 200 sem gravar (a hora não anda),
	// tanto no manter quanto no tomar.
	w.clk.Advance(time.Minute)
	for _, modo := range []string{"manter", "tomar"} {
		rec = w.putCopia(pcA1, c1, modo)
		if rec.Code != http.StatusOK {
			t.Fatalf("reenvio (%s): %d %s", modo, rec.Code, rec.Body.String())
		}
		e = lerEstadoDaCopia(t, rec.Body.Bytes())
		querEstado(t, "reenvio "+modo, e, "este", 2, sha1)
		if !e.AtualizadoEm.Equal(now0) {
			t.Errorf("o reenvio (%s) gravou: a hora virou %s", modo, e.AtualizadoEm)
		}
	}

	// O manter da fonte com a cópia nova grava (o arquivo mudou).
	c2, sha2 := copiaDaSuite(t, "Aurora", "Whisper", "Smart")
	rec = w.putCopia(pcA1, c2, "manter")
	if rec.Code != http.StatusOK {
		t.Fatalf("manter da fonte: %d %s", rec.Code, rec.Body.String())
	}
	e = lerEstadoDaCopia(t, rec.Body.Bytes())
	querEstado(t, "manter da fonte", e, "este", 3, sha2)
	if !e.AtualizadoEm.Equal(now0.Add(time.Minute)) {
		t.Errorf("a hora depois do manter = %s, quero %s", e.AtualizadoEm, now0.Add(time.Minute))
	}

	// O manter de outro PC (que não é a fonte) é 409 com o estado, e nada muda.
	c3, _ := copiaDaSuite(t, "Outro")
	rec = w.putCopia(pcA2, c3, "manter")
	querStatus(t, "manter de quem não é a fonte", rec, http.StatusConflict, "NAO_E_A_FONTE")
	querEstado(t, "o estado do 409", lerEstadoDoConflito(t, rec.Body.Bytes()), "outro", 3, sha2)
	querEstado(t, "o 409 não gravou", w.estadoDaCopia(pcA1), "este", 3, sha2)

	// Tomar de outro PC grava, mesmo com outra fonte: a fonte passa para ele.
	c3, sha3 := copiaDaSuite(t, "Outro")
	rec = w.putCopia(pcA2, c3, "tomar")
	if rec.Code != http.StatusOK {
		t.Fatalf("tomar de outro PC: %d %s", rec.Code, rec.Body.String())
	}
	querEstado(t, "tomar de outro PC", lerEstadoDaCopia(t, rec.Body.Bytes()), "este", 1, sha3)
	querEstado(t, "o PC que perdeu a fonte", w.estadoDaCopia(pcA1), "outro", 1, sha3)
	// E o manter do que perdeu a fonte agora é 409.
	querStatus(t, "manter de quem perdeu a fonte", w.putCopia(pcA1, c2, "manter"), http.StatusConflict, "NAO_E_A_FONTE")

	// Uma cópia só por conta, e cada conta com a sua: o PC da conta B não vê a de A.
	querSemCopia(t, "o PC de outra conta", w.estadoDaCopia(pcB))
	cB, shaB := copiaDaSuite(t, "Da conta B")
	if rec = w.putCopia(pcB, cB, "tomar"); rec.Code != http.StatusOK {
		t.Fatalf("tomar na conta B: %d %s", rec.Code, rec.Body.String())
	}
	querEstado(t, "a conta B", w.estadoDaCopia(pcB), "este", 1, shaB)
	querEstado(t, "a conta A depois da B", w.estadoDaCopia(pcA2), "este", 1, sha3)
}

// O SHA-256 que não bate e os cabeçalhos que faltam ou valem outra coisa são
// 400, e nada é gravado.
func testDicionarioCabecalhos(t *testing.T, w *world) {
	c, sha := copiaDaSuite(t, "Aurora")
	outro := dictionary.SHA256DaCopia([]byte("outros bytes"))
	for nome, cab := range map[string]map[string]string{
		"o SHA-256 de outros bytes":        {"Dicionario-SHA256": outro, "Dicionario-Fonte": "tomar"},
		"sem o SHA-256":                    {"Dicionario-Fonte": "tomar"},
		"o SHA-256 curto":                  {"Dicionario-SHA256": sha[:10], "Dicionario-Fonte": "tomar"},
		"o SHA-256 que não é hexa":         {"Dicionario-SHA256": strings.Repeat("z", 64), "Dicionario-Fonte": "tomar"},
		"sem o Dicionario-Fonte":           {"Dicionario-SHA256": sha},
		"o Dicionario-Fonte desconhecido":  {"Dicionario-SHA256": sha, "Dicionario-Fonte": "forcar"},
		"o Dicionario-Fonte em maiúsculas": {"Dicionario-SHA256": sha, "Dicionario-Fonte": "TOMAR"},
	} {
		if rec := w.putCopiaCom(pcA1, c, cab); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, quero 400", nome, rec.Code, rec.Body.String())
		}
	}
	// O Content-Type que não é JSON é 415.
	rec := w.doBytes(http.MethodPut, caminhoDoDicionario, pcA1, c, "text/plain",
		map[string]string{"Dicionario-SHA256": sha, "Dicionario-Fonte": "tomar"})
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d %s, quero 415", rec.Code, rec.Body.String())
	}
	querSemCopia(t, "depois dos cabeçalhos errados", w.estadoDaCopia(pcA1))
	// O SHA-256 em maiúsculas é aceito (é o mesmo valor).
	if rec := w.putCopiaCom(pcA1, c, map[string]string{"Dicionario-SHA256": strings.ToUpper(sha), "Dicionario-Fonte": "tomar"}); rec.Code != http.StatusOK {
		t.Errorf("o SHA-256 em maiúsculas: %d %s, quero 200", rec.Code, rec.Body.String())
	}
}

// A cópia inválida é 422 DICIONARIO_INVALIDO, com o motivo fechado, em cada
// motivo; nada é gravado, e a resposta nunca traz o texto mandado.
func testDicionarioInvalida(t *testing.T, w *world) {
	copia := func(s dictionary.Semente) []byte {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	base := func(entradas ...dictionary.EntradaSemente) dictionary.Semente {
		return dictionary.Semente{Usuario: dictionary.UsuarioDaCopia, Entradas: entradas}
	}
	muitos := make([]dictionary.EntradaSemente, dictionary.MaxTermosDaCopia+1)
	for i := range muitos {
		muitos[i] = dictionary.EntradaSemente{Canonica: "TermoSecreto" + strconv.Itoa(i)}
	}
	var variantes []string
	for i := 0; i <= dictionary.MaxVariantesPorTermoDaCopia; i++ {
		variantes = append(variantes, "variante secreta "+strconv.Itoa(i))
	}
	casos := []struct {
		nome   string
		corpo  []byte
		motivo string
	}{
		{"não é JSON", []byte(`{"usuario":`), "json"},
		{"um array", []byte(`[]`), "json"},
		{"um segundo objeto", append(copia(base()), copia(base())...), "json"},
		{"campo desconhecido", []byte(`{"usuario":"copia","entradas":[],"segredo":"TermoSecreto"}`), "json"},
		{"o usuário local", copia(dictionary.Semente{Usuario: "mauro", Entradas: []dictionary.EntradaSemente{{Canonica: "TermoSecreto"}}}), "json"},
		{"o mapa de domínios", copia(dictionary.Semente{Usuario: dictionary.UsuarioDaCopia,
			Dominios: map[string][]string{"dev": {"Code.exe"}}, Entradas: []dictionary.EntradaSemente{{Canonica: "TermoSecreto"}}}), "json"},
		{"o id de amostra", copia(dictionary.Semente{Usuario: dictionary.UsuarioDaCopia,
			AliasOrigem: map[string]string{"variante secreta": "cont-04"}, Entradas: []dictionary.EntradaSemente{{Canonica: "TermoSecreto"}}}), "json"},
		{"UTF-8 inválido", []byte("{\"usuario\":\"copia\",\"entradas\":[{\"canonica\":\"Termo\xff\xfeSecreto\"}]}"), "json"},
		{"termos demais", copia(base(muitos...)), "termos_demais"},
		{"variantes demais num termo", copia(base(dictionary.EntradaSemente{Canonica: "TermoSecreto", Alias: variantes})), "variantes_demais"},
		{"o termo vazio", copia(base(dictionary.EntradaSemente{Canonica: "   "})), "termo_invalido"},
		{"o termo de 81 caracteres", copia(base(dictionary.EntradaSemente{Canonica: strings.Repeat("a", 81)})), "termo_invalido"},
		{"a variante de 81 caracteres", copia(base(dictionary.EntradaSemente{Canonica: "TermoSecreto", Alias: []string{strings.Repeat("v", 81)}})), "termo_invalido"},
		// Passa nos limites, mas o Smart não monta: o termo só de pontuação não
		// tem forma canônica.
		{"o termo que o Smart não monta", copia(base(dictionary.EntradaSemente{Canonica: "!!!"})), "termo_invalido"},
	}
	for _, c := range casos {
		rec := w.putCopia(pcA1, c.corpo, "tomar")
		if motivo := erroDaCopia(t, rec); motivo != c.motivo {
			t.Errorf("%s: o motivo %q, quero %q", c.nome, motivo, c.motivo)
		}
		if corpo := rec.Body.String(); strings.Contains(corpo, "Secreto") || strings.Contains(corpo, "secreta") {
			t.Errorf("%s: a resposta traz o texto mandado: %s", c.nome, corpo)
		}
	}
	// O corpo acima do teto é 422 grande_demais, e não 413: com o tamanho
	// declarado, e sem ele (o corpo em pedaços).
	grande := []byte(`{"usuario":"copia","entradas":[],"x":"` + strings.Repeat("a", dictionary.MaxBytesDaCopia) + `"}`)
	if motivo := erroDaCopia(t, w.putCopia(pcA1, grande, "tomar")); motivo != "grande_demais" {
		t.Errorf("o corpo grande: o motivo %q, quero grande_demais", motivo)
	}
	rec := w.doWith(http.MethodPut, caminhoDoDicionario, pcA1, string(grande), func(r *http.Request) {
		r.ContentLength = -1
		r.Header.Set("Dicionario-SHA256", dictionary.SHA256DaCopia(grande))
		r.Header.Set("Dicionario-Fonte", "tomar")
	})
	if motivo := erroDaCopia(t, rec); motivo != "grande_demais" {
		t.Errorf("o corpo grande sem Content-Length: o motivo %q, quero grande_demais", motivo)
	}
	querSemCopia(t, "depois das cópias inválidas", w.estadoDaCopia(pcA1))

	// Os limites valem no número exato: 2.000 termos passam.
	exatos := make([]dictionary.EntradaSemente, dictionary.MaxTermosDaCopia)
	for i := range exatos {
		exatos[i] = dictionary.EntradaSemente{Canonica: "termo " + strconv.Itoa(i)}
	}
	b, sha, err := dictionary.Semente{Usuario: "x", Entradas: exatos}.BytesDaCopia()
	if err != nil {
		t.Fatal(err)
	}
	rec = w.putCopia(pcA1, b, "tomar")
	if rec.Code != http.StatusOK {
		t.Fatalf("2.000 termos: %d %s, quero 200", rec.Code, rec.Body.String())
	}
	querEstado(t, "2.000 termos", lerEstadoDaCopia(t, rec.Body.Bytes()), "este", dictionary.MaxTermosDaCopia, sha)
	// A cópia inválida não apaga a que já está lá.
	if motivo := erroDaCopia(t, w.putCopia(pcA1, []byte(`{"usuario":`), "manter")); motivo != "json" {
		t.Errorf("o motivo %q, quero json", motivo)
	}
	querEstado(t, "a cópia de antes continua", w.estadoDaCopia(pcA1), "este", dictionary.MaxTermosDaCopia, sha)
}

// O DELETE apaga só a cópia do PC que é a fonte; é idempotente.
func testDicionarioDeleteSoDaFonte(t *testing.T, w *world) {
	querStatus(t, "apagar sem cópia", w.do(http.MethodDelete, caminhoDoDicionario, pcA1, nil), http.StatusNoContent, "")
	c, sha := copiaDaSuite(t, "Aurora")
	if rec := w.putCopia(pcA1, c, "tomar"); rec.Code != http.StatusOK {
		t.Fatalf("tomar: %d %s", rec.Code, rec.Body.String())
	}
	// Outro PC da conta, e o PC de outra conta: 204, e a cópia fica.
	for _, quem := range []string{pcA2, pcB} {
		rec := w.do(http.MethodDelete, caminhoDoDicionario, quem, nil)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Errorf("apagar de %s: %d %q, quero 204 sem corpo", quem, rec.Code, rec.Body.String())
		}
	}
	querEstado(t, "a cópia depois do DELETE de quem não é a fonte", w.estadoDaCopia(pcA1), "este", 1, sha)
	querStatus(t, "apagar a própria cópia", w.do(http.MethodDelete, caminhoDoDicionario, pcA1, nil), http.StatusNoContent, "")
	querSemCopia(t, "depois de apagar", w.estadoDaCopia(pcA2))
	querStatus(t, "apagar de novo", w.do(http.MethodDelete, caminhoDoDicionario, pcA1, nil), http.StatusNoContent, "")
}

// As três rotas são só do token de um PC: a sessão do navegador é 403, e sem
// credencial é 401.
func testDicionarioSoComTokenDePC(t *testing.T, w *world) {
	c, sha := copiaDaSuite(t, "Aurora")
	if rec := w.putCopia(pcA1, c, "tomar"); rec.Code != http.StatusOK {
		t.Fatalf("tomar: %d %s", rec.Code, rec.Body.String())
	}
	cab := map[string]string{"Dicionario-SHA256": sha, "Dicionario-Fonte": "tomar"}
	for _, quem := range []string{celA, celB} {
		querStatus(t, "GET pela sessão", w.do(http.MethodGet, caminhoDoDicionario, quem, nil), http.StatusForbidden, "FORBIDDEN")
		querStatus(t, "PUT pela sessão", w.doBytes(http.MethodPut, caminhoDoDicionario, quem, c, "application/json", cab), http.StatusForbidden, "FORBIDDEN")
		querStatus(t, "DELETE pela sessão", w.do(http.MethodDelete, caminhoDoDicionario, quem, nil), http.StatusForbidden, "FORBIDDEN")
	}
	querStatus(t, "GET sem credencial", w.do(http.MethodGet, caminhoDoDicionario, "", nil), http.StatusUnauthorized, "UNAUTHORIZED")
	querStatus(t, "PUT sem credencial", w.doBytes(http.MethodPut, caminhoDoDicionario, "", c, "application/json", cab), http.StatusUnauthorized, "UNAUTHORIZED")
	querStatus(t, "DELETE sem credencial", w.do(http.MethodDelete, caminhoDoDicionario, "", nil), http.StatusUnauthorized, "UNAUTHORIZED")
	querStatus(t, "POST não existe", w.do(http.MethodPost, caminhoDoDicionario, pcA1, nil), http.StatusNotFound, "NOT_FOUND")
	querEstado(t, "a cópia continua", w.estadoDaCopia(pcA1), "este", 1, sha)
}

// Revogar ou desconectar o PC que é a fonte apaga a cópia; revogar outro PC não.
func testDicionarioRevogarApagaACopia(t *testing.T, w *world) {
	c, sha := copiaDaSuite(t, "Aurora")
	if rec := w.putCopia(pcA2, c, "tomar"); rec.Code != http.StatusOK {
		t.Fatalf("tomar: %d %s", rec.Code, rec.Body.String())
	}
	// Revogar o PC que NÃO é a fonte (pela sessão) deixa a cópia.
	querStatus(t, "revogar o PC 1", w.do(http.MethodDelete, "/api/v1/devices/"+w.devA1, celA, nil), http.StatusNoContent, "")
	querEstado(t, "a cópia depois de revogar outro PC", w.estadoDaCopia(pcA2), "este", 1, sha)
	// Desconectar a fonte (o PC com o próprio token) apaga a cópia.
	querStatus(t, "desconectar o PC 2", w.do(http.MethodDelete, "/api/v1/devices/"+w.devA2, pcA2, nil), http.StatusNoContent, "")
	w.newDevice(w.userA, "pc-a3")
	querSemCopia(t, "a cópia depois de desconectar a fonte", w.estadoDaCopia("pc-a3"))
	if e, err := w.b.Store.DictionaryCopyInfo(context.Background(), w.userA); err != nil || e != nil {
		t.Errorf("o Store ainda tem a cópia: %+v, %v", e, err)
	}
	// Um PC revogado não grava mais: o token já não vale (401).
	querStatus(t, "PUT do PC revogado", w.putCopia(pcA2, c, "tomar"), http.StatusUnauthorized, "")
	// E revogar a fonte numa conta não toca a cópia da outra.
	cB, shaB := copiaDaSuite(t, "Da conta B")
	if rec := w.putCopia(pcB, cB, "tomar"); rec.Code != http.StatusOK {
		t.Fatalf("tomar na conta B: %d %s", rec.Code, rec.Body.String())
	}
	querEstado(t, "a conta B", w.estadoDaCopia(pcB), "este", 1, shaB)
}

// O "Apagar meus dados" apaga a cópia da conta (e só a dela); o PC novo recomeça
// sem fonte, e o manter dele é 409.
func testDicionarioApagarMeusDadosApagaACopia(t *testing.T, w *world) {
	cA, _ := copiaDaSuite(t, "Aurora")
	cB, shaB := copiaDaSuite(t, "Da conta B")
	if rec := w.putCopia(pcA1, cA, "tomar"); rec.Code != http.StatusOK {
		t.Fatalf("tomar: %d %s", rec.Code, rec.Body.String())
	}
	if rec := w.putCopia(pcB, cB, "tomar"); rec.Code != http.StatusOK {
		t.Fatalf("tomar na conta B: %d %s", rec.Code, rec.Body.String())
	}
	querStatus(t, "apagar meus dados", w.do(http.MethodDelete, "/api/v1/me/data", celA, map[string]any{"confirmar": "apagar"}), http.StatusNoContent, "")
	w.newDevice(w.userA, "pc-novo")
	querSemCopia(t, "a cópia depois do apagar meus dados", w.estadoDaCopia("pc-novo"))
	querStatus(t, "manter depois do apagar", w.putCopia("pc-novo", cA, "manter"), http.StatusConflict, "NAO_E_A_FONTE")
	querEstado(t, "a conta B continua com a cópia", w.estadoDaCopia(pcB), "este", 1, shaB)
}

// O log de acesso leva o padrão da rota, o status, o motivo, o tamanho em faixas
// e o número de termos; nunca um termo, uma variante nem o nome do aparelho.
func testDicionarioNadaNoLog(t *testing.T, w *world) {
	b, _ := copiaDaSuite(t, "ZzTermoSecretoUm", "ZzTermoSecretoDois")
	w.putCopia(pcA1, b, "tomar")
	w.putCopia(pcA2, b, "manter")
	w.putCopia(pcA1, []byte(`{"usuario":"copia","entradas":[{"canonica":"ZzTermoSecretoTres","ZzCampo":1}]}`), "manter")
	w.do(http.MethodGet, caminhoDoDicionario, pcA2, nil)
	w.do(http.MethodDelete, caminhoDoDicionario, pcA1, nil)
	log := w.logs.String()
	for _, proibido := range []string{"ZzTermoSecreto", "zztermosecreto", "ZzCampo", "errado", "PC de teste"} {
		if strings.Contains(log, proibido) {
			t.Errorf("o log traz %q:\n%s", proibido, log)
		}
	}
	for _, quero := range []string{"PUT", "/api/v1/sync/dictionary", "termos=2", "tamanho=ate_8k", "motivo=nao_e_a_fonte", "motivo=json"} {
		if !strings.Contains(log, quero) {
			t.Errorf("o log não traz %q:\n%s", quero, log)
		}
	}
}
