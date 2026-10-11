package chavestest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/dictionary"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/contracttest"
)

// Os casos da rota de ditar (ADR-021, decisão A e C5b): POST /api/v1/dictations,
// pela API HTTP, contra o Store em memória e o Postgres. Todos com o aceite de
// VERDADE (a dictation_consents), salvo onde o caso diz, e com os provedores
// falsos (provedores.go): nenhum caso chama um provedor de verdade, e cada um
// conta as chamadas.

const rotaDitar = "/api/v1/dictations"

// A chave de mentira de cada provedor (nunca uma chave de verdade).
func chaveDe(provedor string) string { return "sk-de-mentira-" + provedor + "-0123456789abcdef" }

// webm é um áudio de mentira com os primeiros bytes de um webm do Chrome (o
// cabeçalho EBML com o DocType "webm") e o recheio dado: nenhum decodificador o
// leria, e a rota nem tenta.
func webm(recheio ...string) []byte {
	cab := []byte{
		0x1A, 0x45, 0xDF, 0xA3, 0x9F,
		0x42, 0x86, 0x81, 0x01, 0x42, 0xF7, 0x81, 0x01, 0x42, 0xF2, 0x81, 0x04, 0x42, 0xF3, 0x81, 0x08,
		0x42, 0x82, 0x84, 'w', 'e', 'b', 'm',
		0x42, 0x87, 0x81, 0x04, 0x42, 0x85, 0x81, 0x02,
	}
	out := append([]byte(nil), cab...)
	for _, r := range recheio {
		out = append(out, r...)
	}
	if len(recheio) == 0 {
		out = append(out, bytes.Repeat([]byte{0x1F, 0x43, 0xB6, 0x75}, 64)...)
	}
	return out
}

// corpoVigiado conta o que a rota leu do corpo: "sem ler o corpo" é nenhuma
// leitura.
type corpoVigiado struct {
	r        io.Reader
	leituras atomic.Int32
	lidos    atomic.Int64
}

func (c *corpoVigiado) Read(p []byte) (int, error) {
	c.leituras.Add(1)
	n, err := c.r.Read(p)
	c.lidos.Add(int64(n))
	return n, err
}

func (c *corpoVigiado) Close() error { return nil }

func (c *corpoVigiado) naoLido() bool { return c.leituras.Load() == 0 }

// pedidoDeDitado monta o POST /dictations da sessão dada, com o áudio (num
// corpo vigiado), o id e as medidas de um ditado de 4 s com 2 s de fala.
func (m *mundo) pedidoDeDitado(quem, id string, audio []byte) (*http.Request, *corpoVigiado) {
	m.t.Helper()
	corpo := &corpoVigiado{r: bytes.NewReader(audio)}
	r := httptest.NewRequest(http.MethodPost, rotaDitar, nil)
	r.Body, r.ContentLength = corpo, int64(len(audio))
	r.Host = contracttest.HostNuvem
	r.Header.Set("Content-Type", "audio/webm")
	r.Header.Set("Ditado-Id", id)
	r.Header.Set("Ditado-Duracao-Ms", "4000")
	r.Header.Set("Ditado-Fala-Ms", "2000")
	if c, ok := m.creds[quem]; ok {
		if c.bearer {
			r.Header.Set("Authorization", "Bearer "+c.valor)
		} else {
			r.AddCookie(&http.Cookie{Name: "__Host-sessao", Value: c.valor})
			r.Header.Set("Sec-Fetch-Site", "same-origin")
		}
	}
	return r, corpo
}

// ditar manda o ditado e devolve a resposta e o corpo vigiado. ajustar mexe no
// pedido antes de ele ir.
func (m *mundo) ditar(quem, id string, audio []byte, ajustar ...func(*http.Request)) (*httptest.ResponseRecorder, *corpoVigiado) {
	m.t.Helper()
	r, corpo := m.pedidoDeDitado(quem, id, audio)
	for _, a := range ajustar {
		a(r)
	}
	rec := httptest.NewRecorder()
	m.h.ServeHTTP(rec, r)
	return rec, corpo
}

// comCabecalho troca (ou tira, com valor vazio) um cabeçalho do pedido.
func comCabecalho(nome, valor string) func(*http.Request) {
	return func(r *http.Request) {
		if valor == "" {
			r.Header.Del(nome)
			return
		}
		r.Header.Set(nome, valor)
	}
}

// ditadoResp é a resposta da rota, lida com DisallowUnknownFields: um campo
// novo na resposta sem o teste saber quebra aqui.
type ditadoResp struct {
	ID               string     `json:"id"`
	DeviceID         *string    `json:"device_id"`
	SourcePlatform   string     `json:"source_platform"`
	RawText          string     `json:"raw_text"`
	FinalText        string     `json:"final_text"`
	Mode             string     `json:"mode"`
	Language         *string    `json:"language"`
	ASRProvider      string     `json:"asr_provider"`
	ASRModel         *string    `json:"asr_model"`
	DurationMs       *int64     `json:"duration_ms"`
	ProcessingMs     *int64     `json:"processing_ms"`
	Confidence       *float32   `json:"confidence"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at"`
	Version          int64      `json:"version"`
	VersaoEmConflito any        `json:"versao_em_conflito"`
	Aviso            string     `json:"aviso"`
}

// ditadoOk exige o status e lê o ditado.
func (m *mundo) ditadoOk(rec *httptest.ResponseRecorder, status int) ditadoResp {
	m.t.Helper()
	if rec.Code != status {
		m.t.Fatalf("POST /dictations: %d %s, quero %d", rec.Code, rec.Body.String(), status)
	}
	return decodificar[ditadoResp](m.t, rec)
}

// prepararDitado deixa a conta pronta para ditar pela rota: o aceite de
// verdade (com o provedor do Polish, se houver), a chave de cada provedor (pelo
// PUT, que pergunta a lista ao provedor de modelos falso) e as preferências. O
// modelo do Polish é um que o provedor de modelos falso lista.
func (m *mundo) prepararDitado(quem, transcricao, polish string, dica bool) {
	m.t.Helper()
	if !m.aceiteDeVerdade {
		m.comAceiteDeVerdade()
	}
	var p *string
	if polish != "" {
		p = &polish
	}
	m.aceitarOk(quem, transcricao, p)
	m.exigirStatus(m.put(quem, transcricao, chaveDe(transcricao)), http.StatusOK, "PUT da chave da transcrição")
	if polish != "" && polish != transcricao {
		m.exigirStatus(m.put(quem, polish, chaveDe(polish)), http.StatusOK, "PUT da chave do Polish")
	}
	polishProv, polishModelo := "openai", ""
	if polish != "" {
		polishProv, polishModelo = polish, "modelo-novo"
	}
	m.exigirStatus(m.preferencias(quem, transcricao, "", dica, polish != "", polishProv, polishModelo), http.StatusOK, "PUT das preferências")
}

func (m *mundo) usuarioDe(quem string) string {
	switch quem {
	case celA, pcA:
		return m.userA
	case celB:
		return m.userB
	}
	if strings.HasPrefix(quem, celA) {
		return m.userA
	}
	return m.userB
}

// situacao lê o que a nuvem tem com o id, direto no Store.
func (m *mundo) situacao(quem, id string) cloudstore.SituacaoDoId {
	m.t.Helper()
	s, err := m.b.Store.Situacao(context.Background(), m.usuarioDe(quem), id)
	if err != nil {
		m.t.Fatalf("Situacao: %v", err)
	}
	return s
}

// aprenderPeloPC grava um par aprendido como o PC grava: pelo push, com o token
// do PC da conta A.
func (m *mundo) aprenderPeloPC(variante, termo string, dominio *string) {
	m.t.Helper()
	payload := map[string]any{"variant": variante, "term": termo, "restricted": dominio != nil, "domain": dominio}
	corpo := jsonDe(m.t, map[string]any{"mutations": []map[string]any{{
		"mutation_id": cloudstore.NewUUIDv7(), "entity_type": "learned", "entity_id": dictionary.IDDoPar(variante),
		"operation": "upsert", "base_version": 0, "payload": payload,
	}}})
	rec := m.pedir(pcA, http.MethodPost, "/api/v1/sync/push", corpo)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"applied"`) {
		m.t.Fatalf("push do aprendido: %d %s", rec.Code, rec.Body.String())
	}
}

// O caminho feliz: a sessão dita, o provedor da conta transcreve (uma chamada,
// com o Content-Length e sem chunked, o modelo, o idioma, o arquivo webm e a
// chave dela só no cabeçalho), e o ditado sai 201, como o item do histórico:
// source_platform web, device_id nulo, mode smart, a duração do app. Ele está no
// histórico e chega ao PC pelo pull, sem aparelho.
func testDitarCria(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	id, audio := cloudstore.NewUUIDv7(), webm()
	rec, _ := m.ditar(celA, id, audio)
	d := m.ditadoOk(rec, http.StatusCreated)
	texto := "hoje eu entreguei o relatório para o cliente da empresa"
	switch {
	case d.ID != id || d.DeviceID != nil || d.SourcePlatform != "web" || d.Status != "done":
		t.Fatalf("o ditado não é do app: %+v", d)
	case d.RawText != texto || d.FinalText != texto || d.Mode != "smart" || d.Aviso != "":
		t.Fatalf("o texto: %+v", d)
	case d.ASRProvider != "openai" || d.ASRModel == nil || *d.ASRModel != cloudstore.ModeloPadraoOpenAI:
		t.Fatalf("o provedor e o modelo: %+v", d)
	case d.DurationMs == nil || *d.DurationMs != 4000 || d.ProcessingMs == nil || d.Language == nil || *d.Language != "pt-BR":
		t.Fatalf("as medidas: %+v", d)
	case d.Version != 1 || d.DeletedAt != nil || d.VersaoEmConflito != nil || d.Confidence != nil:
		t.Fatalf("a linha: %+v", d)
	case !d.CreatedAt.Equal(agora0):
		t.Fatalf("created_at %v, quero o relógio do servidor (%v)", d.CreatedAt, agora0)
	}
	if n := m.voz.quantas(); n != 1 {
		t.Fatalf("%d chamadas ao provedor, quero 1", n)
	}
	p := m.voz.ultimo()
	switch {
	case p.caminho != "/v1/audio/transcriptions":
		t.Errorf("caminho %q", p.caminho)
	case p.contentLength <= 0 || p.chunked:
		t.Errorf("o pedido ao provedor foi sem Content-Length (%d) ou chunked (%v)", p.contentLength, p.chunked)
	case p.auth != "Bearer "+chaveDe("openai"):
		t.Errorf("Authorization %q", p.auth)
	case p.campos["model"] != cloudstore.ModeloPadraoOpenAI || p.campos["language"] != "pt":
		t.Errorf("campos %v", p.campos)
	case p.tipoArquivo != "audio/webm" || !bytes.Equal(p.arquivo, audio):
		t.Errorf("o arquivo: %q, %d bytes", p.tipoArquivo, len(p.arquivo))
	}
	if _, ok := p.campos["prompt"]; ok {
		t.Error("a dica foi com a preferência desligada")
	}
	if n := m.pol.quantas(); n != 0 {
		t.Errorf("o Polish desligado foi chamado %d vezes", n)
	}
	if rec := m.pedir(celA, http.MethodGet, "/api/v1/transcriptions/"+id, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), texto) {
		t.Fatalf("o ditado não está no histórico: %d %s", rec.Code, rec.Body.String())
	}
	pull := m.pedir(pcA, http.MethodGet, "/api/v1/sync/pull?after_seq=0", "")
	var resp struct {
		Changes []struct {
			EntityID string  `json:"entity_id"`
			DeviceID *string `json:"device_id"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(pull.Body.Bytes(), &resp); err != nil || pull.Code != http.StatusOK {
		t.Fatalf("pull: %d %s", pull.Code, pull.Body.String())
	}
	achou := false
	for _, c := range resp.Changes {
		if c.EntityID == id {
			achou = true
			if c.DeviceID != nil {
				t.Errorf("o ditado do celular chegou ao PC com aparelho %q", *c.DeviceID)
			}
		}
	}
	if !achou {
		t.Error("o ditado do celular não chegou ao pull do PC")
	}
	// A Groq, no endereço dela, com o modelo padrão dela.
	m.prepararDitado(celB, "groq", "", false)
	d = m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celB, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if p := m.voz.ultimo(); p.caminho != "/openai/v1/audio/transcriptions" || p.campos["model"] != cloudstore.ModeloPadraoGroq ||
		d.ASRProvider != "groq" || p.auth != "Bearer "+chaveDe("groq") {
		t.Errorf("a Groq: %q %v %+v", p.caminho, p.campos, d)
	}
}

// Só com a sessão do navegador: o token de um PC é 403 (o AcessoDitar nunca é o
// AcessoQualquer) e sem credencial é 401, sem ler o corpo nem chamar o provedor.
func testDitarSoComSessao(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	rec, corpo := m.ditar(pcA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusForbidden, "FORBIDDEN")
	if !corpo.naoLido() {
		t.Error("o 403 leu o corpo")
	}
	rec, corpo = m.ditar("", cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusUnauthorized, "UNAUTHORIZED")
	if !corpo.naoLido() {
		t.Error("o 401 leu o corpo")
	}
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
	achou := false
	for _, rt := range m.cloud.Rotas() {
		if strings.Contains(rt.Padrao, "/dictations") {
			achou = true
			if rt.Padrao != "POST "+rotaDitar || rt.Acesso != server.AcessoDitar {
				t.Errorf("%s com acesso %v, quero só POST com o AcessoDitar", rt.Padrao, rt.Acesso)
			}
		}
	}
	if !achou {
		t.Fatal("a rota de ditar não está na tabela")
	}
	if server.AcessoDitar == server.AcessoQualquer || server.AcessoDitar.String() != "ditar" {
		t.Fatal("o AcessoDitar é o AcessoQualquer")
	}
}

// Sem o arquivo da chave de cifra, a rota não é montada: 404 de rota não
// montada para qualquer credencial.
func testDitarSemOArquivo(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	m.montar("")
	for _, quem := range []string{celA, pcA, ""} {
		rec, corpo := m.ditar(quem, cloudstore.NewUUIDv7(), webm())
		if msg := exigirErro(t, rec, http.StatusNotFound, "NOT_FOUND"); msg != "rota não encontrada" {
			t.Errorf("%q: %q, quero o 404 de rota não montada", quem, msg)
		}
		if !corpo.naoLido() {
			t.Errorf("%q: o 404 leu o corpo", quem)
		}
	}
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
}

// Sem o aceite vigente do provedor da transcrição, ou, com o Polish ligado, do
// provedor do Polish: 403 SEM_ACEITE, sem ler o corpo nem chamar o provedor.
func testDitarSemAceite(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if !corpo.naoLido() {
		t.Error("sem aceite: o corpo foi lido")
	}

	// O aceite da transcrição, e as preferências com o Polish da Anthropic, que
	// o aceite não cobre (gravadas direto no store: a rota das preferências já
	// recusaria).
	m.prepararDitado(celA, "openai", "", false)
	prefs := m.lidas(m.userA)
	prefs.PolishLigado, prefs.Polish = true, cloudstore.ProvedorEModelo{Provedor: "anthropic", Modelo: "modelo-novo"}
	if _, err := m.b.Ditado.GravarPreferenciasDoDitado(context.Background(), m.userA, prefs, agora0); err != nil {
		t.Fatal(err)
	}
	rec, corpo = m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if !corpo.naoLido() {
		t.Error("sem aceite do Polish: o corpo foi lido")
	}

	// Revogado: o aceite e as chaves saem, e a recusa é a do aceite.
	prefs.PolishLigado = false
	if _, err := m.b.Ditado.GravarPreferenciasDoDitado(context.Background(), m.userA, prefs, agora0); err != nil {
		t.Fatal(err)
	}
	m.exigirStatus(m.pedir(celA, http.MethodDelete, rotaAceite, ""), http.StatusNoContent, "revogar")
	rec, _ = m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusForbidden, "SEM_ACEITE")
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
}

// As preferências pedem um provedor sem chave guardada (a da transcrição, ou a
// do Polish ligado), ou a chave guardada foi cifrada por outra chave de cifra:
// 409 SEM_CHAVE, sem ler o corpo nem chamar o provedor.
func testDitarSemChave(t *testing.T, m *mundo) {
	m.comAceiteDeVerdade()
	m.aceitarOk(celA, "openai", texto("anthropic"))
	rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusConflict, "SEM_CHAVE")
	if !corpo.naoLido() {
		t.Error("sem chave: o corpo foi lido")
	}
	m.exigirStatus(m.put(celA, "openai", chaveDe("openai")), http.StatusOK, "PUT da chave")
	m.exigirStatus(m.preferencias(celA, "openai", "", false, true, "anthropic", "modelo-novo"), http.StatusOK, "preferências")
	rec, _ = m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	exigirErro(t, rec, http.StatusConflict, "SEM_CHAVE")

	m.exigirStatus(m.preferencias(celA, "openai", "", false, false, "anthropic", "modelo-novo"), http.StatusOK, "preferências")
	m.montar(ChaveDeCifraB)
	rec, corpo = m.ditar(celA, cloudstore.NewUUIDv7(), webm())
	if msg := exigirErro(t, rec, http.StatusConflict, "SEM_CHAVE"); !strings.Contains(msg, "cole a chave de novo") {
		t.Errorf("a chave de outra chave de cifra: %q", msg)
	}
	if !corpo.naoLido() {
		t.Error("a chave que não vale: o corpo foi lido")
	}
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
}

// O corpo acima de 1 MiB pelo Content-Length e a duração acima do teto: 413,
// sem ler o corpo. Sem o Content-Length, o corpo é cortado no teto, e o
// provedor não é chamado.
func testDitarGrandeDemais(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	grande := webm(strings.Repeat("x", server.TetoDoCorpoDoDitado))
	rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), grande)
	exigirErro(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
	if !corpo.naoLido() {
		t.Error("o 413 pelo Content-Length leu o corpo")
	}
	rec, corpo = m.ditar(celA, cloudstore.NewUUIDv7(), webm(), comCabecalho("Ditado-Duracao-Ms", strconv.Itoa(125001)))
	exigirErro(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
	if !corpo.naoLido() {
		t.Error("o 413 da duração leu o corpo")
	}
	rec, corpo = m.ditar(celA, cloudstore.NewUUIDv7(), grande, func(r *http.Request) { r.ContentLength = -1 })
	exigirErro(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
	if lidos := corpo.lidos.Load(); lidos > server.TetoDoCorpoDoDitado+64<<10 {
		t.Errorf("sem Content-Length, a rota leu %d bytes", lidos)
	}
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
	// No teto, passa.
	rec, _ = m.ditar(celA, cloudstore.NewUUIDv7(), webm(strings.Repeat("x", server.TetoDoCorpoDoDitado-len(webm("")))),
		comCabecalho("Ditado-Duracao-Ms", strconv.Itoa(125000)))
	m.ditadoOk(rec, http.StatusCreated)
}

// O formato: o Content-Type que não é audio/webm é 415 sem ler o corpo; o corpo
// que não é webm pelos primeiros bytes (outra coisa, ou o Matroska) é 415 sem
// chamar o provedor. O audio/webm com os parâmetros do MediaRecorder passa.
func testDitarFormato(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	for _, tipo := range []string{"audio/ogg", "multipart/form-data; boundary=x", "application/json", "", "audio/webmx"} {
		rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm(), comCabecalho("Content-Type", tipo))
		exigirErro(t, rec, http.StatusUnsupportedMediaType, "FORMATO")
		if !corpo.naoLido() {
			t.Errorf("Content-Type %q: o 415 leu o corpo", tipo)
		}
	}
	matroska := webm()
	copy(matroska[bytes.Index(matroska, []byte("webm")):], "matr")
	for nome, audio := range map[string][]byte{
		"não é EBML": []byte(strings.Repeat("OggS", 64)),
		"Matroska":   matroska,
		"vazio":      {},
	} {
		rec, _ := m.ditar(celA, cloudstore.NewUUIDv7(), audio)
		if rec.Code != http.StatusUnsupportedMediaType || !strings.Contains(rec.Body.String(), `"FORMATO"`) {
			t.Errorf("%s: %d %s, quero 415 FORMATO", nome, rec.Code, rec.Body.String())
		}
	}
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
	rec, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm(), comCabecalho("Content-Type", "audio/webm;codecs=opus"))
	m.ditadoOk(rec, http.StatusCreated)
}

// Os cabeçalhos: sem o Ditado-Id, ou com um que não é UUID v7, ou repetido, e as
// medidas fora da forma: 400, sem ler o corpo nem chamar o provedor.
func testDitarCabecalhos(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	for nome, ajustar := range map[string]func(*http.Request){
		"sem id":            comCabecalho("Ditado-Id", ""),
		"id lixo":           comCabecalho("Ditado-Id", "nao-e-uuid"),
		"id v4":             comCabecalho("Ditado-Id", "3f2a1c4e-8b7d-4e6f-9a1b-2c3d4e5f6a7b"),
		"id repetido":       func(r *http.Request) { r.Header.Add("Ditado-Id", cloudstore.NewUUIDv7()) },
		"sem duração":       comCabecalho("Ditado-Duracao-Ms", ""),
		"duração negativa":  comCabecalho("Ditado-Duracao-Ms", "-1"),
		"duração com ponto": comCabecalho("Ditado-Duracao-Ms", "4000.5"),
		"fala lixo":         comCabecalho("Ditado-Fala-Ms", "dois"),
		"fala além":         comCabecalho("Ditado-Fala-Ms", "4001"),
	} {
		rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm(), ajustar)
		exigirErro(t, rec, http.StatusBadRequest, "BAD_REQUEST")
		if !corpo.naoLido() {
			t.Errorf("%s: o 400 leu o corpo", nome)
		}
	}
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
	// O id em maiúsculas é o mesmo ditado.
	id := cloudstore.NewUUIDv7()
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, strings.ToUpper(id), webm()); return r }(), http.StatusCreated)
	d := m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, id, webm()); return r }(), http.StatusOK)
	if d.ID != id || m.voz.quantas() != 1 {
		t.Fatalf("o mesmo id em outra caixa: %+v, %d chamadas", d, m.voz.quantas())
	}
}

// Sem fala: a fala medida abaixo de 300 ms é 422 SEM_FALA sem ler o corpo nem
// chamar o provedor. O provedor que não devolve texto é 422 depois da chamada;
// o mesmo id em 10 minutos é 422 sem chamar de novo, e depois disso chama.
func testDitarSemFala(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	rec, corpo := m.ditar(celA, cloudstore.NewUUIDv7(), webm(), comCabecalho("Ditado-Fala-Ms", "299"))
	exigirErro(t, rec, http.StatusUnprocessableEntity, "SEM_FALA")
	if !corpo.naoLido() || m.voz.quantas() != 0 {
		t.Fatalf("a fala medida curta: lido=%v, %d chamadas", !corpo.naoLido(), m.voz.quantas())
	}
	m.voz.responder("   ", 0)
	id := cloudstore.NewUUIDv7()
	rec, _ = m.ditar(celA, id, webm())
	exigirErro(t, rec, http.StatusUnprocessableEntity, "SEM_FALA")
	if n := m.voz.quantas(); n != 1 {
		t.Fatalf("%d chamadas, quero 1", n)
	}
	m.clk.Advance(memoria() - time.Second)
	rec, corpo = m.ditar(celA, id, webm())
	exigirErro(t, rec, http.StatusUnprocessableEntity, "SEM_FALA")
	if !corpo.naoLido() || m.voz.quantas() != 1 {
		t.Fatalf("o SEM_FALA repetido em 10 min: lido=%v, %d chamadas", !corpo.naoLido(), m.voz.quantas())
	}
	if s := m.situacao(celA, id); s.Estado != cloudstore.IdNada {
		t.Fatal("o SEM_FALA criou um ditado")
	}
	m.clk.Advance(2 * time.Second)
	m.voz.responder("agora sim eu falei alguma coisa", 0)
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, id, webm()); return r }(), http.StatusCreated)
	if n := m.voz.quantas(); n != 2 {
		t.Fatalf("depois de 10 min, %d chamadas, quero 2", n)
	}
}

// memoria é o tempo que o "sem fala" e o pago e não gravado ficam (ADR-021,
// escolha menor 23).
func memoria() time.Duration { return 10 * time.Minute }

// As falhas do provedor viram os códigos da lista fechada, sem o corpo de erro
// (que traz a chave): 401 PROVEDOR_RECUSOU_CHAVE (422), 429 PROVEDOR_LIMITE
// (502), 5xx e redirecionamento PROVEDOR_FORA (502). Nenhum ditado é criado, e
// o reenvio chama de novo.
func testDitarFalhasDoProvedor(t *testing.T, m *mundo) {
	m.limites = server.Limites{DitarPorMinuto: 100}
	m.prepararDitado(celA, "openai", "", false)
	for _, c := range []struct {
		status    int
		codigo    string
		statusApp int
	}{
		{http.StatusUnauthorized, "PROVEDOR_RECUSOU_CHAVE", http.StatusUnprocessableEntity},
		{http.StatusForbidden, "PROVEDOR_RECUSOU_CHAVE", http.StatusUnprocessableEntity},
		{http.StatusTooManyRequests, "PROVEDOR_LIMITE", http.StatusBadGateway},
		{http.StatusInternalServerError, "PROVEDOR_FORA", http.StatusBadGateway},
		{http.StatusServiceUnavailable, "PROVEDOR_FORA", http.StatusBadGateway},
		{http.StatusFound, "PROVEDOR_FORA", http.StatusBadGateway},
		{http.StatusRequestEntityTooLarge, "PROVEDOR_FORA", http.StatusBadGateway},
	} {
		m.voz.responder("", c.status)
		id := cloudstore.NewUUIDv7()
		antes := m.voz.quantas()
		rec, _ := m.ditar(celA, id, webm())
		msg := exigirErro(t, rec, c.statusApp, c.codigo)
		if strings.Contains(rec.Body.String(), chaveDe("openai")) || strings.Contains(msg, "Incorrect") {
			t.Fatalf("%d: a resposta repassa o corpo de erro do provedor: %s", c.status, rec.Body.String())
		}
		if s := m.situacao(celA, id); s.Estado != cloudstore.IdNada {
			t.Fatalf("%d: a falha do provedor criou um ditado", c.status)
		}
		rec, _ = m.ditar(celA, id, webm())
		exigirErro(t, rec, c.statusApp, c.codigo)
		if n := m.voz.quantas(); n != antes+2 {
			t.Fatalf("%d: o reenvio depois da falha não chamou o provedor (%d chamadas)", c.status, n-antes)
		}
	}
	if strings.Contains(m.logs.String(), chaveDe("openai")) || strings.Contains(m.logs.String(), "Incorrect") {
		t.Fatal("o log traz o corpo de erro do provedor")
	}
}

// A sessão revogada no meio é 401 de novo; e uma rota antiga do ASR continua
// 404 (o ditado só nasce pela rota nova).
func testDitarRotasAntigas404(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	for _, r := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/asr/transcribe"},
		{http.MethodPost, "/api/v1/transcriptions"},
		{http.MethodGet, rotaDitar},
		{http.MethodPut, rotaDitar},
		{http.MethodPost, rotaDitar + "/" + cloudstore.NewUUIDv7()},
	} {
		for _, quem := range []string{celA, pcA} {
			if msg := exigirErro(t, m.pedir(quem, r.method, r.path, `{"x":1}`), http.StatusNotFound, "NOT_FOUND"); msg != "rota não encontrada" {
				t.Errorf("%s %s (%s): %q", r.method, r.path, quem, msg)
			}
		}
	}
	if n := m.voz.quantas(); n != 0 {
		t.Fatalf("%d chamadas ao provedor", n)
	}
}

// A dica do aprendido: com a preferência ligada, os termos da conta vão ao
// provedor no prompt (sem o par preso a um domínio); desligada, nada vai. E o
// Smart da conta aplica o par sem domínio ao texto, como o PC; o par com
// domínio não age.
func testDitarDicaESmart(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "", false)
	m.aprenderPeloPC("dirfi", "DIRF", nil)
	dom := "contabil"
	m.aprenderPeloPC("viti", "Vitti", &dom)
	m.voz.responder("hoje eu entreguei a dirfi e a viti para o cliente", 0)
	d := m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.FinalText != "hoje eu entreguei a DIRF e a viti para o cliente" || d.RawText != "hoje eu entreguei a dirfi e a viti para o cliente" {
		t.Fatalf("o Smart da conta: bruto %q, final %q", d.RawText, d.FinalText)
	}
	if _, ok := m.voz.ultimo().campos["prompt"]; ok {
		t.Fatal("a dica foi com a preferência desligada")
	}
	m.exigirStatus(m.preferencias(celA, "openai", "", true, false, "openai", ""), http.StatusOK, "liga a dica")
	m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if prompt := m.voz.ultimo().campos["prompt"]; prompt != "DIRF" {
		t.Fatalf("a dica = %q, quero só o termo sem domínio", prompt)
	}
	// Um par novo muda o Smart da conta (o cache não fica velho).
	m.aprenderPeloPC("ecedê", "ECD", nil)
	m.voz.responder("mande o ecedê e a dirfi", 0)
	d = m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.FinalText != "mande o ECD e a DIRF" {
		t.Fatalf("o par novo não agiu: %q", d.FinalText)
	}
	if strings.Contains(m.logs.String(), "DIRF") || strings.Contains(m.logs.String(), "dirfi") {
		t.Fatal("o log traz o aprendido ou o texto")
	}
}

// O caminho do ditado no log: uma linha "ditado" com só contagens e códigos.
func linhaDoDitado(t *testing.T, logs string) string {
	t.Helper()
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, "msg=ditado ") {
			return l
		}
	}
	t.Fatalf("o log não tem a linha \"ditado\":\n%s", logs)
	return ""
}

// O Polish ligado: o texto do Smart vai ao provedor do Polish da conta, e a
// resposta que passa nas guardas vale, com mode polish. Na Anthropic, pelo
// endereço dela.
func testDitarPolishAceito(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "openai", false)
	m.voz.responder("hoje eu entreguei o relatório para o cliente da empresa", 0)
	m.pol.responder(0, func(s string) string { return "Hoje eu entreguei o relatório para o cliente da empresa." })
	d := m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.Mode != "polish" || d.FinalText != "Hoje eu entreguei o relatório para o cliente da empresa." || d.Aviso != "" ||
		d.RawText != "hoje eu entreguei o relatório para o cliente da empresa" {
		t.Fatalf("o Polish: %+v", d)
	}
	if m.pol.quantas() != 1 || m.pol.ultimo() != "hoje eu entreguei o relatório para o cliente da empresa" {
		t.Fatalf("o Polish recebeu %q em %d chamadas, quero o texto do Smart", m.pol.ultimo(), m.pol.quantas())
	}
	linha := linhaDoDitado(t, m.logs.String())
	for _, quer := range []string{"desfecho=criado", "polish=aceito", "polish_provedor=openai", "polish_modelo=modelo-novo", "tokens_entrada=12"} {
		if !strings.Contains(linha, quer) {
			t.Errorf("a linha \"ditado\" sem %q: %s", quer, linha)
		}
	}
	if strings.Contains(m.logs.String(), "relatório") {
		t.Fatal("o log traz o texto")
	}

	m.prepararDitado(celB, "groq", "anthropic", false)
	m.pol.responder(0, func(s string) string { return "Hoje eu entreguei o relatório para o cliente da empresa." })
	d = m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celB, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.Mode != "polish" || d.ASRProvider != "groq" {
		t.Fatalf("o Polish da Anthropic: %+v", d)
	}
	if p := m.pol.caminhos[len(m.pol.caminhos)-1]; p != "/v1/messages" {
		t.Fatalf("o Polish da Anthropic foi a %q", p)
	}
}

// O Polish que falha (o provedor fora, ou a resposta que não passa nas guardas)
// grava o texto do Smart com mode smart, e a resposta traz o aviso com o motivo.
// O texto curto nem vai ao provedor (sem aviso).
func testDitarPolishQueFalha(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "openai", false)
	m.pol.responder(http.StatusInternalServerError, nil)
	d := m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.Mode != "smart" || d.FinalText != "hoje eu entreguei o relatório para o cliente da empresa" || d.Aviso == "" {
		t.Fatalf("o Polish fora: %+v", d)
	}
	m.pol.responder(0, func(string) string { return "Texto inventado que não tem nada a ver com o ditado de hoje." })
	d = m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.Mode != "smart" || d.FinalText != "hoje eu entreguei o relatório para o cliente da empresa" || d.Aviso == "" {
		t.Fatalf("o Polish recusado pelas guardas: %+v", d)
	}
	antes := m.pol.quantas()
	m.voz.responder("ok pode mandar", 0)
	d = m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.Mode != "smart" || d.Aviso != "" || m.pol.quantas() != antes {
		t.Fatalf("o texto curto: %+v, %d chamadas ao Polish", d, m.pol.quantas()-antes)
	}
}

// O modelo do Polish que o provedor não lista não chega ao Polish (nem à
// política de esforço): o servidor pergunta a lista ao provedor de modelos, não
// acha, e grava o texto do Smart com o aviso modelo_inexistente. O log não
// escreve o nome.
func testDitarPolishModeloDesconhecido(t *testing.T, m *mundo) {
	m.prepararDitado(celA, "openai", "openai", false)
	m.exigirStatus(m.preferencias(celA, "openai", "", false, true, "openai", "modelo-inventado-pela-pessoa"), http.StatusOK, "preferências")
	antes := m.prov.quantas()
	d := m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.Mode != "smart" || d.Aviso != "modelo_inexistente" || m.pol.quantas() != 0 {
		t.Fatalf("o modelo desconhecido: %+v, %d chamadas ao Polish", d, m.pol.quantas())
	}
	if m.prov.quantas() != antes+1 || m.prov.ultima().chave != chaveDe("openai") {
		t.Fatalf("a lista de modelos: %d chamadas", m.prov.quantas()-antes)
	}
	if strings.Contains(m.logs.String(), "modelo-inventado-pela-pessoa") {
		t.Fatal("o log escreve o nome do modelo que o provedor não listou")
	}
	// O modelo da lista passa, sem perguntar a lista de novo.
	m.exigirStatus(m.preferencias(celA, "openai", "", false, true, "openai", "modelo-velho"), http.StatusOK, "preferências")
	d = m.ditadoOk(func() *httptest.ResponseRecorder { r, _ := m.ditar(celA, cloudstore.NewUUIDv7(), webm()); return r }(), http.StatusCreated)
	if d.Mode != "polish" || m.prov.quantas() != antes+1 {
		t.Fatalf("o modelo da lista: %+v, %d chamadas à lista", d, m.prov.quantas()-antes)
	}
}

// esperarAte espera a condição, até 10 s.
func esperarAte(t *testing.T, o string, cond func() bool) {
	t.Helper()
	limite := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(limite) {
			t.Fatalf("%s: não aconteceu em 10 s", o)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
