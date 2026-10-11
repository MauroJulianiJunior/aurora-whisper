package localapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/localapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// settingsView espelha a resposta de GET/PUT /api/settings.
type settingsView struct {
	Hotkey                string   `json:"hotkey"`
	HoldThresholdMs       int64    `json:"hold_threshold_ms"`
	BeepEnabled           bool     `json:"beep_enabled"`
	MicrophoneAlwaysReady bool     `json:"microphone_always_ready"`
	TextMode              string   `json:"text_mode"`
	AIProvider            string   `json:"ai_provider"`
	AIModel               string   `json:"ai_model"`
	Autostart             bool     `json:"autostart"`
	AutostartIndisponivel string   `json:"autostart_indisponivel"`
	ReinicioPendente      []string `json:"reinicio_pendente"`
	PolishDisponivel      bool     `json:"polish_disponivel"`
	PolishUltimoMotivo    string   `json:"polish_ultimo_motivo"`
	Version               int64    `json:"version"`
}

type erroView struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// servidorDeSettings monta o Server com os dublês e devolve as peças que
// os testes inspecionam.
type servidorDeSettings struct {
	h        http.Handler
	repo     *storage.InMemorySettingsRepository
	inicio   *inicioFalso
	cofre    *chaves.Memoria
	aplicado []storage.Settings
	pendente []string
}

func novoServidorDeSettings(t *testing.T) *servidorDeSettings {
	t.Helper()
	s := &servidorDeSettings{
		repo:   storage.NewInMemorySettingsRepository(),
		inicio: &inicioFalso{},
		cofre:  chaves.NovaMemoria(),
	}
	pref := prefsDeTeste()
	pref.Inicio, pref.Chaves = s.inicio, s.cofre
	pref.Aplicar = func(saved storage.Settings) []string {
		s.aplicado = append(s.aplicado, saved)
		return s.pendente
	}
	s.h = localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), s.repo, pref).Handler()
	return s
}

func (s *servidorDeSettings) get(t *testing.T) settingsView {
	t.Helper()
	w := fazer(t, s.h, http.MethodGet, "/api/settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", w.Code, w.Body.String())
	}
	return decodeJSON[settingsView](t, w)
}

// TestSettings_GetDevolveOContratoExato: os campos são exatamente os do
// contrato comum — nem a mais (o painel não deve depender do que não foi
// combinado), nem a menos.
func TestSettings_GetDevolveOContratoExato(t *testing.T) {
	s := novoServidorDeSettings(t)
	w := fazer(t, s.h, http.MethodGet, "/api/settings", nil)
	var campos map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &campos); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var got []string
	for k := range campos {
		got = append(got, k)
	}
	sort.Strings(got)
	quero := []string{"ai_model", "ai_provider", "autostart", "autostart_indisponivel", "beep_enabled",
		"hold_threshold_ms", "hotkey", "microphone_always_ready", "polish_disponivel", "polish_ultimo_motivo", "reinicio_pendente",
		"text_mode", "version"}
	if !reflect.DeepEqual(got, quero) {
		t.Fatalf("campos = %v\nquero    %v", got, quero)
	}
	if string(campos["reinicio_pendente"]) != "[]" {
		t.Fatalf("reinicio_pendente = %s, quero [] (nunca null)", campos["reinicio_pendente"])
	}
}

// TestSettings_GetAntesDoPrimeiroPutDevolveOPadrao: instalação nova não é
// 404 — é o padrão, com version 0, que o primeiro PUT usa de base.
func TestSettings_GetAntesDoPrimeiroPutDevolveOPadrao(t *testing.T) {
	s := novoServidorDeSettings(t)
	s.inicio.ligado, s.inicio.indisponivel = true, "só na versão de bandeja"
	got := s.get(t)
	quero := settingsView{
		Hotkey: "ctrl+win", HoldThresholdMs: 700, BeepEnabled: true, TextMode: "smart",
		Autostart: true, AutostartIndisponivel: "só na versão de bandeja",
		ReinicioPendente: []string{}, PolishDisponivel: true, PolishUltimoMotivo: "", Version: 0,
	}
	if !reflect.DeepEqual(got, quero) {
		t.Fatalf("GET = %+v\nquero %+v", got, quero)
	}
}

func TestSettings_PutGravaAplicaEDevolve(t *testing.T) {
	s := novoServidorDeSettings(t)
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{
		"base_version": 0, "hold_threshold_ms": 900, "beep_enabled": false,
		"text_mode": "literal", "ai_provider": "anthropic", "ai_model": "  um-modelo  ",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	put := decodeJSON[settingsView](t, w)
	if put.HoldThresholdMs != 900 || put.BeepEnabled || put.TextMode != "literal" ||
		put.AIProvider != "anthropic" || put.AIModel != "um-modelo" || put.Version != 1 {
		t.Fatalf("PUT devolveu %+v", put)
	}
	if got := s.get(t); !reflect.DeepEqual(got, put) {
		t.Fatalf("GET depois do PUT = %+v, o PUT devolveu %+v", got, put)
	}
	gravado, err := s.repo.Get(t.Context())
	if err != nil || gravado.HoldThresholdMs != 900 || gravado.TextMode != "literal" || gravado.Hotkey != "ctrl+win" {
		t.Fatalf("no repositório: %+v, %v", gravado, err)
	}
	if len(s.aplicado) != 1 || s.aplicado[0].HoldThresholdMs != 900 || s.aplicado[0].BeepEnabled || s.aplicado[0].Version != 1 {
		t.Fatalf("o Agent recebeu %+v, quero UMA aplicação do que foi gravado", s.aplicado)
	}
}

// TestSettings_CampoAusenteMantemOAtual: PUT só com o que mudou não zera
// o resto — e não mexe no início automático de quem usa.
func TestSettings_CampoAusenteMantemOAtual(t *testing.T) {
	s := novoServidorDeSettings(t)
	s.inicio.ligado = true
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "beep_enabled": false})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decodeJSON[settingsView](t, w)
	if got.HoldThresholdMs != 700 || got.TextMode != "smart" || got.BeepEnabled || !got.Autostart {
		t.Fatalf("PUT parcial = %+v", got)
	}
	if len(s.inicio.pedidos) != 0 {
		t.Fatalf("o PUT sem autostart mexeu no registro: %v", s.inicio.pedidos)
	}
}

// TestSettings_MicrofoneSempreProntoVaiEVolta: o padrão é desligado (ADR-015,
// o microfone só abre durante o ditado); o PUT com true liga e devolve; o
// PUT de OUTRO campo — o beep, o limiar — não manda nem muda a preferência,
// nos dois sentidos; e só um false explícito a desliga. O Agent recebe o
// valor gravado em Aplicar, e a preferência não entra sozinha em
// reinicio_pendente (quem decide isso é o Aplicar).
func TestSettings_MicrofoneSempreProntoVaiEVolta(t *testing.T) {
	s := novoServidorDeSettings(t)
	if s.get(t).MicrophoneAlwaysReady {
		t.Fatal("o padrão do GET tem o microfone sempre pronto ligado, quero desligado")
	}

	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "microphone_always_ready": true})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	put := decodeJSON[settingsView](t, w)
	if !put.MicrophoneAlwaysReady || len(put.ReinicioPendente) != 0 {
		t.Fatalf("PUT ligando devolveu %+v", put)
	}
	if got := s.get(t); !reflect.DeepEqual(got, put) {
		t.Fatalf("GET depois do PUT = %+v, o PUT devolveu %+v", got, put)
	}
	if gravado, err := s.repo.Get(t.Context()); err != nil || !gravado.MicrophoneAlwaysReady {
		t.Fatalf("no repositório: %+v, %v", gravado, err)
	}
	if len(s.aplicado) != 1 || !s.aplicado[0].MicrophoneAlwaysReady {
		t.Fatalf("o Agent recebeu %+v, quero a preferência ligada em UMA aplicação", s.aplicado)
	}

	// Campo ausente mantém: outros PUTs não a desligam.
	w = fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 1, "beep_enabled": false, "hold_threshold_ms": 900})
	if got := decodeJSON[settingsView](t, w); w.Code != http.StatusOK || !got.MicrophoneAlwaysReady {
		t.Fatalf("PUT de outros campos: status = %d, %+v; quero a preferência mantida ligada", w.Code, got)
	}
	// E só o false explícito a desliga.
	w = fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 2, "microphone_always_ready": false})
	if got := decodeJSON[settingsView](t, w); w.Code != http.StatusOK || got.MicrophoneAlwaysReady || got.HoldThresholdMs != 900 || got.BeepEnabled {
		t.Fatalf("PUT desligando: status = %d, %+v", w.Code, got)
	}
	// Desligada, continua desligada quando outro campo muda.
	w = fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 3, "beep_enabled": true})
	if got := decodeJSON[settingsView](t, w); w.Code != http.StatusOK || got.MicrophoneAlwaysReady {
		t.Fatalf("PUT de outro campo com a preferência desligada: status = %d, %+v", w.Code, got)
	}
}

// TestSettings_VersaoVelhaDa409: duas abas abertas na versão 0; a
// primeira salva, a segunda recebe 409 — e nada do que ela mandou vale.
func TestSettings_VersaoVelhaDa409(t *testing.T) {
	s := novoServidorDeSettings(t)
	if w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "hold_threshold_ms": 900}); w.Code != http.StatusOK {
		t.Fatalf("primeiro PUT: %d %s", w.Code, w.Body.String())
	}
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "hold_threshold_ms": 300, "autostart": true})
	if w.Code != http.StatusConflict || decodeJSON[erroView](t, w).Error.Code != "CONFLICT" {
		t.Fatalf("segundo PUT: status = %d, body = %s; quero 409 CONFLICT", w.Code, w.Body.String())
	}
	if got := s.get(t); got.HoldThresholdMs != 900 || got.Version != 1 {
		t.Fatalf("o PUT recusado mudou alguma coisa: %+v", got)
	}
	if len(s.inicio.pedidos) != 0 || len(s.aplicado) != 1 {
		t.Fatalf("o PUT recusado agiu: registro %v, aplicações %d", s.inicio.pedidos, len(s.aplicado))
	}
}

// TestSettings_InvalidoDa400ENaoGrava cobre cada regra de validação: a
// resposta é 400 com mensagem legível, e nada é gravado nem aplicado.
func TestSettings_InvalidoDa400ENaoGrava(t *testing.T) {
	for nome, c := range map[string]struct {
		corpo  map[string]any
		trecho string
	}{
		"sem base_version":      {map[string]any{"hold_threshold_ms": 900}, "base_version"},
		"limiar abaixo de 300":  {map[string]any{"hold_threshold_ms": 299}, "entre 300 e 2000"},
		"limiar acima de 2000":  {map[string]any{"hold_threshold_ms": 2001}, "entre 300 e 2000"},
		"modo desconhecido":     {map[string]any{"text_mode": "criativo"}, "literal, smart ou polish"},
		"modo em maiúscula":     {map[string]any{"text_mode": "Smart"}, "literal, smart ou polish"},
		"provedor desconhecido": {map[string]any{"ai_provider": "google"}, "openai ou anthropic"},
		"modelo com controle":   {map[string]any{"ai_model": "a\nb"}, "modelo inválido"},
		"modelo longo demais":   {map[string]any{"ai_model": strings.Repeat("m", 201)}, "modelo inválido"},
		"polish sem provedor":   {map[string]any{"text_mode": "polish", "ai_model": "m"}, "provedor de IA escolhido"},
		"polish sem modelo":     {map[string]any{"text_mode": "polish", "ai_provider": "openai"}, "modelo escolhido"},
		"polish sem chave":      {map[string]any{"text_mode": "polish", "ai_provider": "openai", "ai_model": "m"}, "chave de API da OpenAI"},
		"trocar a tecla":        {map[string]any{"hotkey": "F9"}, "tecla de ditado"},
	} {
		t.Run(nome, func(t *testing.T) {
			s := novoServidorDeSettings(t)
			if _, ok := c.corpo["base_version"]; !ok && nome != "sem base_version" {
				c.corpo["base_version"] = 0
			}
			w := fazer(t, s.h, http.MethodPut, "/api/settings", c.corpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s; quero 400", w.Code, w.Body.String())
			}
			if e := decodeJSON[erroView](t, w).Error; e.Code != "BAD_REQUEST" || !strings.Contains(e.Message, c.trecho) {
				t.Fatalf("erro = %+v; quero BAD_REQUEST com %q", e, c.trecho)
			}
			if _, err := s.repo.Get(t.Context()); !errors.Is(err, storage.ErrNotFound) {
				t.Fatal("o PUT inválido gravou")
			}
			if len(s.aplicado) != 0 {
				t.Fatal("o PUT inválido foi aplicado no Agent")
			}
		})
	}
}

// TestSettings_LimiaresDaFaixaSaoAceitos: 300 e 2000 são válidos — a
// faixa é fechada, como a de dictation.Config.Validate.
func TestSettings_LimiaresDaFaixaSaoAceitos(t *testing.T) {
	s := novoServidorDeSettings(t)
	for i, ms := range []int64{300, 2000} {
		w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": i, "hold_threshold_ms": ms})
		if w.Code != http.StatusOK {
			t.Fatalf("%d ms: status = %d, body = %s", ms, w.Code, w.Body.String())
		}
	}
}

// TestSettings_PolishComChaveEAceito: com provedor, modelo e chave
// guardada, o Polish é aceito — e o GET diz que o Agent o faz
// (polish_disponivel true, ADR-016).
func TestSettings_PolishComChaveEAceito(t *testing.T) {
	s := novoServidorDeSettings(t)
	if err := s.cofre.Gravar("openai", "sk-teste-0123456789abcdefghij"); err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{
		"base_version": 0, "text_mode": "polish", "ai_provider": "openai", "ai_model": "m",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := decodeJSON[settingsView](t, w); got.TextMode != "polish" || !got.PolishDisponivel {
		t.Fatalf("PUT = %+v; quero polish gravado e polish_disponivel true", got)
	}
	// A chave de OUTRO provedor não serve.
	w = fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 1, "ai_provider": "anthropic"})
	if w.Code != http.StatusBadRequest || !strings.Contains(decodeJSON[erroView](t, w).Error.Message, "Anthropic") {
		t.Fatalf("polish com a chave de outro provedor: status = %d, body = %s", w.Code, w.Body.String())
	}
}

// TestSettings_PolishSemChaveNaoTravaORestoDaTela: gravar o Polish com a
// chave e apagar a chave depois é permitido (sem chave, o Polish cai para
// o Smart). Daí em diante a tela tem de continuar mudando o beep, o
// limiar e o início automático — inclusive reenviando o objeto inteiro do
// GET. A chave só é exigida de novo quando o PUT LEVA ao Polish: religar
// o modo, ou trocar o provedor com ele ligado.
func TestSettings_PolishSemChaveNaoTravaORestoDaTela(t *testing.T) {
	s := novoServidorDeSettings(t)
	if err := s.cofre.Gravar("openai", "sk-teste-0123456789abcdefghij"); err != nil {
		t.Fatalf("Gravar: %v", err)
	}
	if w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{
		"base_version": 0, "text_mode": "polish", "ai_provider": "openai", "ai_model": "m",
	}); w.Code != http.StatusOK {
		t.Fatalf("PUT polish com chave: %d %s", w.Code, w.Body.String())
	}
	if w := fazer(t, s.h, http.MethodDelete, "/api/ia/chave?provedor=openai", nil); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE da chave: %d %s", w.Code, w.Body.String())
	}

	versao := int64(1)
	put := func(corpo map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		corpo["base_version"] = versao
		w := fazer(t, s.h, http.MethodPut, "/api/settings", corpo)
		if w.Code == http.StatusOK {
			versao = decodeJSON[settingsView](t, w).Version
		}
		return w
	}
	for nome, corpo := range map[string]map[string]any{
		"só o beep":       {"beep_enabled": false},
		"só o limiar":     {"hold_threshold_ms": 900},
		"só o autostart":  {"autostart": true},
		"só o modelo":     {"ai_model": "outro"},
		"o objeto do GET": {"hold_threshold_ms": 900, "beep_enabled": true, "text_mode": "polish", "ai_provider": "openai", "ai_model": "outro", "autostart": true},
	} {
		if w := put(corpo); w.Code != http.StatusOK {
			t.Fatalf("%s, com o Polish gravado e sem chave: status = %d, body = %s; quero 200", nome, w.Code, w.Body.String())
		}
	}
	if got := s.get(t); got.TextMode != "polish" || got.AIProvider != "openai" {
		t.Fatalf("o modo gravado mudou sozinho: %+v", got)
	}

	// Trocar o provedor com o Polish ligado leva a um Polish novo: exige a
	// chave desse provedor.
	if w := put(map[string]any{"ai_provider": "anthropic"}); w.Code != http.StatusBadRequest ||
		!strings.Contains(decodeJSON[erroView](t, w).Error.Message, "chave de API da Anthropic") {
		t.Fatalf("trocar o provedor do Polish sem chave: status = %d, body = %s; quero 400", w.Code, w.Body.String())
	}
	// Sair do Polish é livre; voltar a ele sem chave, não.
	if w := put(map[string]any{"text_mode": "smart"}); w.Code != http.StatusOK {
		t.Fatalf("sair do Polish: %d %s", w.Code, w.Body.String())
	}
	if w := put(map[string]any{"text_mode": "polish"}); w.Code != http.StatusBadRequest ||
		!strings.Contains(decodeJSON[erroView](t, w).Error.Message, "chave de API da OpenAI") {
		t.Fatalf("voltar ao Polish sem chave: status = %d, body = %s; quero 400", w.Code, w.Body.String())
	}
}

// TestSettings_ChaveNoCampoDoModeloERecusada: a chave colada por engano no
// campo livre do modelo não é gravada (local_settings é preferência
// sincronizável) e não volta na resposta, nem na mensagem do 400.
func TestSettings_ChaveNoCampoDoModeloERecusada(t *testing.T) {
	for _, colada := range []string{
		"sk-proj-ChaveColadaNoLugarErrado0123456789",
		"  sk-ant-api03-ChaveColadaNoLugarErrado0123  ",
		"SK-ChaveEmMaiusculaColadaNoLugarErrado01",
	} {
		s := novoServidorDeSettings(t)
		w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "ai_model": colada})
		if w.Code != http.StatusBadRequest || !strings.Contains(decodeJSON[erroView](t, w).Error.Message, "parece uma chave de API") {
			t.Fatalf("ai_model %q: status = %d, body = %s; quero 400 apontando para o campo da chave", colada, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), strings.TrimSpace(colada)[3:]) {
			t.Fatalf("a chave colada voltou na resposta: %s", w.Body.String())
		}
		if _, err := s.repo.Get(t.Context()); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("ai_model %q foi gravado", colada)
		}
	}
	// "sk-" no MEIO do nome não é chave.
	s := novoServidorDeSettings(t)
	if w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "ai_model": "desk-model"}); w.Code != http.StatusOK {
		t.Fatalf("ai_model com sk- no meio: %d %s", w.Code, w.Body.String())
	}
}

// inicioQueSegura é o registro do Windows demorando: cada Definir avisa
// que entrou e espera ser solto.
type inicioQueSegura struct {
	inicioFalso
	entrou chan struct{}
	solta  chan struct{}
}

func (f *inicioQueSegura) Definir(ligar bool) error {
	f.entrou <- struct{}{}
	<-f.solta
	return f.inicioFalso.Definir(ligar)
}

// TestSettings_PutsSimultaneosEntramEmFila: duas abas salvam no mesmo
// instante, com a mesma base_version, pedindo o início automático. A
// primeira está no meio da mudança do registro quando a segunda chega. A
// segunda tem de esperar — e aí recebe 409 SEM ter tocado o registro: no
// 409, nada do que foi mandado vale.
func TestSettings_PutsSimultaneosEntramEmFila(t *testing.T) {
	inicio := &inicioQueSegura{entrou: make(chan struct{}, 2), solta: make(chan struct{})}
	pref := prefsDeTeste()
	pref.Inicio = inicio
	h := localapi.New(testUserID, storage.NewInMemoryTranscriptionRepository(), storage.NewInMemorySettingsRepository(), pref).Handler()

	codigos := make(chan int, 2)
	pedir := func(corpo map[string]any) {
		go func() { codigos <- fazer(t, h, http.MethodPut, "/api/settings", corpo).Code }()
	}
	pedir(map[string]any{"base_version": 0, "autostart": true})
	<-inicio.entrou // a primeira está mudando o registro

	pedir(map[string]any{"base_version": 0, "autostart": true, "beep_enabled": false})
	select {
	case <-inicio.entrou:
		t.Error("a segunda mexeu no registro enquanto a primeira ainda gravava")
	case <-time.After(300 * time.Millisecond):
		// esperando na fila, como deve
	}
	close(inicio.solta)

	got := []int{<-codigos, <-codigos}
	sort.Ints(got)
	if !slices.Equal(got, []int{http.StatusOK, http.StatusConflict}) {
		t.Fatalf("status = %v, quero um 200 e um 409", got)
	}
	inicio.mu.Lock()
	defer inicio.mu.Unlock()
	if len(inicio.pedidos) != 1 {
		t.Fatalf("pedidos ao registro = %v, quero só o da primeira", inicio.pedidos)
	}
}

func TestSettings_AutostartLigaPeloRegistro(t *testing.T) {
	s := novoServidorDeSettings(t)
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "autostart": true})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !slices.Equal(s.inicio.pedidos, []bool{true}) || !decodeJSON[settingsView](t, w).Autostart {
		t.Fatalf("pedidos ao registro = %v", s.inicio.pedidos)
	}
	// Pedir o que já está não toca o registro de novo.
	fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 1, "autostart": true})
	if len(s.inicio.pedidos) != 1 {
		t.Fatalf("pedido redundante ao registro: %v", s.inicio.pedidos)
	}
}

// TestSettings_AutostartIndisponivelDa400ComOMotivo: a mesma regra da
// bandeja — na build de console, não se mexe no início automático.
func TestSettings_AutostartIndisponivelDa400ComOMotivo(t *testing.T) {
	s := novoServidorDeSettings(t)
	s.inicio.indisponivel = "só na versão de bandeja"
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "autostart": true, "beep_enabled": false})
	if w.Code != http.StatusBadRequest || !strings.Contains(decodeJSON[erroView](t, w).Error.Message, "só na versão de bandeja") {
		t.Fatalf("status = %d, body = %s; quero 400 com o motivo", w.Code, w.Body.String())
	}
	if len(s.inicio.pedidos) != 0 {
		t.Fatalf("o registro foi tocado: %v", s.inicio.pedidos)
	}
	if _, err := s.repo.Get(t.Context()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("o resto do PUT foi gravado mesmo com o 400")
	}
	// Mandar o valor que já vale é aceito: não há mudança a recusar.
	if w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "autostart": false}); w.Code != http.StatusOK {
		t.Fatalf("autostart igual ao atual: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestSettings_FalhaDoRegistroNaoGravaNada(t *testing.T) {
	s := novoServidorDeSettings(t)
	s.inicio.falha = errors.New("acesso negado")
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "autostart": true, "hold_threshold_ms": 900})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s; quero 500", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "acesso negado") {
		t.Fatal("o erro interno vazou na resposta")
	}
	if _, err := s.repo.Get(t.Context()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("as preferências foram gravadas apesar da falha do registro")
	}
}

// TestSettings_ReinicioPendenteVemDoAgent: quem diz o que só vale no
// próximo início é o Agent (Aplicar). O GET seguinte repete — e um PUT
// em que tudo foi aplicado limpa a lista.
func TestSettings_ReinicioPendenteVemDoAgent(t *testing.T) {
	s := novoServidorDeSettings(t)
	s.pendente = []string{"text_mode", "hold_threshold_ms"}
	w := fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 0, "hold_threshold_ms": 900})
	if got := decodeJSON[settingsView](t, w).ReinicioPendente; !slices.Equal(got, []string{"hold_threshold_ms", "text_mode"}) {
		t.Fatalf("PUT: reinicio_pendente = %v", got)
	}
	if got := s.get(t).ReinicioPendente; !slices.Equal(got, []string{"hold_threshold_ms", "text_mode"}) {
		t.Fatalf("GET: reinicio_pendente = %v", got)
	}
	s.pendente = nil
	fazer(t, s.h, http.MethodPut, "/api/settings", map[string]any{"base_version": 1, "hold_threshold_ms": 800})
	if got := s.get(t).ReinicioPendente; got == nil || len(got) != 0 {
		t.Fatalf("depois de tudo aplicado: reinicio_pendente = %#v, quero []", got)
	}
}
