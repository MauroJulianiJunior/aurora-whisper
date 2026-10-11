package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/chaves"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/dictation"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Modos de texto (16-ai-behavior-spec.md).
const (
	ModoLiteral = "literal"
	ModoSmart   = "smart"
	ModoPolish  = "polish"
)

// polishDisponivel diz se o Agent FAZ o Polish: com a etapa 2 (ADR-016) o
// Agent chama o provedor de IA com a chave de quem usa, e a tela libera o
// modo. O GET o devolve em "polish_disponivel"; um Agent mais velho não o
// manda, e o painel trata a ausência como false.
const polishDisponivel = true

// tamanhoMaximoDoModelo limita o nome do modelo: é texto livre, mas
// nome de modelo de provedor tem dezenas de caracteres.
const tamanhoMaximoDoModelo = 200

// limiteDoCorpo corta corpos de PUT/POST: nenhum pedido legítimo desta
// API passa de poucas centenas de bytes.
const limiteDoCorpo = 16 << 10

// settingsDTO é o que GET e PUT /api/settings devolvem
// (06-api-contract.md, "API local", "Settings").
type settingsDTO struct {
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

// settingsPutDTO é o corpo do PUT. Ponteiros: campo AUSENTE mantém o
// valor atual. Sem isso, um cliente que esquecesse "autostart" desligaria
// o início automático de quem usa sem ninguém pedir.
type settingsPutDTO struct {
	BaseVersion           *int64  `json:"base_version"`
	Hotkey                *string `json:"hotkey"`
	HoldThresholdMs       *int64  `json:"hold_threshold_ms"`
	BeepEnabled           *bool   `json:"beep_enabled"`
	MicrophoneAlwaysReady *bool   `json:"microphone_always_ready"`
	TextMode              *string `json:"text_mode"`
	AIProvider            *string `json:"ai_provider"`
	AIModel               *string `json:"ai_model"`
	Autostart             *bool   `json:"autostart"`
}

// atuais devolve o que está gravado, ou o Padrao com version 0 se nada
// foi gravado ainda.
func (s *Server) atuais(ctx context.Context) (storage.Settings, error) {
	cur, err := s.settings.Get(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		cur = s.pref.Padrao
		cur.Version = 0
		return cur, nil
	}
	return cur, err
}

func (s *Server) toDTO(cur storage.Settings) settingsDTO {
	ligado, indisponivel := s.pref.Inicio.Estado()
	s.mu.Lock()
	pendente := slices.Clone(s.reinicioPendente)
	s.mu.Unlock()
	if pendente == nil {
		pendente = []string{} // "[]" no JSON, nunca "null"
	}
	return settingsDTO{
		Hotkey:                cur.Hotkey,
		HoldThresholdMs:       cur.HoldThresholdMs,
		BeepEnabled:           cur.BeepEnabled,
		MicrophoneAlwaysReady: cur.MicrophoneAlwaysReady,
		TextMode:              cur.TextMode,
		AIProvider:            cur.AIProvider,
		AIModel:               cur.AIModel,
		Autostart:             ligado,
		AutostartIndisponivel: indisponivel,
		ReinicioPendente:      pendente,
		PolishDisponivel:      polishDisponivel,
		PolishUltimoMotivo:    s.ultimoMotivoDoPolish(),
		Version:               cur.Version,
	}
}

// ultimoMotivoDoPolish é a frase de por que o último Polish não agiu, ou ""
// quando agiu, quando ainda não houve Polish, quando o ditado era curto
// demais (o esperado) ou quando o motivo já é antigo (motivoRecente).
// Preferencias.UltimoPolish nil: o Agent não informa nada, e é "".
func (s *Server) ultimoMotivoDoPolish() string {
	if s.pref.UltimoPolish == nil {
		return ""
	}
	desfecho, ok := s.pref.UltimoPolish()
	if !ok {
		return ""
	}
	return fraseDoMotivo(desfecho, time.Now())
}

// handleGetSettings atende GET /api/settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cur, err := s.atuais(r.Context())
	if err != nil {
		s.log.Warn("localapi: lendo as preferências", "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui ler as configurações.")
		return
	}
	writeJSON(w, http.StatusOK, s.toDTO(cur))
}

// handlePutSettings atende PUT /api/settings.
//
// A ordem importa: a versão é conferida antes de tudo (409), depois a
// validação (400), depois o início automático — que é a única parte que
// age fora do banco —, e só então a gravação. Se o registro recusar, nada
// foi gravado.
//
// Da leitura da versão até o Aplicar, um PUT de cada vez (putMu). O corpo
// é lido antes, fora da fila: um cliente lento não segura os outros.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsPutDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limiteDoCorpo)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return
	}
	if body.BaseVersion == nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "Falta base_version: mande a version que veio no GET.")
		return
	}

	s.putMu.Lock()
	defer s.putMu.Unlock()

	cur, err := s.atuais(r.Context())
	if err != nil {
		s.log.Warn("localapi: lendo as preferências", "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui ler as configurações.")
		return
	}
	if *body.BaseVersion != cur.Version {
		writeError(w, http.StatusConflict, codeConflict, "As configurações mudaram em outro lugar. Recarregue e tente de novo.")
		return
	}

	novo := mesclar(cur, body)
	if body.Hotkey != nil && *body.Hotkey != cur.Hotkey {
		writeError(w, http.StatusBadRequest, codeBadRequest, "A tecla de ditado ainda não pode ser trocada por aqui.")
		return
	}
	if msg := validar(novo); msg != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, msg)
		return
	}
	if entraNoPolish(cur, novo) {
		if _, err := s.pref.Chaves.Ler(novo.AIProvider); errors.Is(err, chaves.ErrAusente) {
			writeError(w, http.StatusBadRequest, codeBadRequest, fmt.Sprintf(
				"O modo Polish precisa de uma chave de API da %s guardada.", ia.Nome(novo.AIProvider)))
			return
		} else if err != nil {
			s.log.Warn("localapi: lendo a chave para validar o Polish", "provedor", novo.AIProvider, "err", err)
			writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui ler o Gerenciador de Credenciais do Windows.")
			return
		}
	}

	if body.Autostart != nil {
		if ligado, indisponivel := s.pref.Inicio.Estado(); *body.Autostart != ligado {
			if indisponivel != "" {
				writeError(w, http.StatusBadRequest, codeBadRequest,
					"Iniciar com o Windows não pode ser mudado agora: "+indisponivel+".")
				return
			}
			if err := s.pref.Inicio.Definir(*body.Autostart); err != nil {
				s.log.Warn("localapi: mudando o início automático", "ligar", *body.Autostart, "err", err)
				writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui mudar o início com o Windows.")
				return
			}
		}
	}

	saved, err := s.settings.Put(r.Context(), novo, cur.Version)
	if errors.Is(err, storage.ErrVersionConflict) {
		writeError(w, http.StatusConflict, codeConflict, "As configurações mudaram em outro lugar. Recarregue e tente de novo.")
		return
	}
	if err != nil {
		s.log.Warn("localapi: gravando as preferências", "err", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "Não consegui gravar as configurações.")
		return
	}

	var pendente []string
	if s.pref.Aplicar != nil {
		pendente = slices.Sorted(slices.Values(s.pref.Aplicar(saved)))
	}
	s.mu.Lock()
	s.reinicioPendente = pendente
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, s.toDTO(saved))
}

// entraNoPolish diz se o PUT LEVA ao Polish: liga o modo, ou troca o
// provedor com ele ligado. Só aí a chave é exigida.
//
// Conferir a chave em todo PUT com o Polish gravado travava a tela: apagar
// a chave depois é permitido (DELETE /api/ia/chave; sem chave, o Polish
// cai para o Smart), e dali em diante nem o beep dava para mudar sem
// antes trocar o modo ou colar a chave de novo — inclusive para o cliente
// que reenvia o objeto inteiro do GET (revisão de 29-set-2026).
func entraNoPolish(cur, novo storage.Settings) bool {
	return novo.TextMode == ModoPolish &&
		(cur.TextMode != ModoPolish || novo.AIProvider != cur.AIProvider)
}

// mesclar aplica sobre cur só os campos presentes no corpo. hotkey e
// autostart não entram: a tecla é só leitura nesta etapa, e o início
// automático mora no registro.
func mesclar(cur storage.Settings, b settingsPutDTO) storage.Settings {
	novo := cur
	if b.HoldThresholdMs != nil {
		novo.HoldThresholdMs = *b.HoldThresholdMs
	}
	if b.BeepEnabled != nil {
		novo.BeepEnabled = *b.BeepEnabled
	}
	if b.MicrophoneAlwaysReady != nil {
		novo.MicrophoneAlwaysReady = *b.MicrophoneAlwaysReady
	}
	if b.TextMode != nil {
		novo.TextMode = *b.TextMode
	}
	if b.AIProvider != nil {
		novo.AIProvider = *b.AIProvider
	}
	if b.AIModel != nil {
		novo.AIModel = strings.TrimSpace(*b.AIModel)
	}
	return novo
}

// validar devolve a mensagem do 400, ou "" se novo é aceitável. Não
// confere a chave do Polish: isso pergunta ao Gerenciador de Credenciais,
// e fica no handler.
func validar(novo storage.Settings) string {
	minimo, maximo := dictation.MinHoldThreshold.Milliseconds(), dictation.MaxHoldThreshold.Milliseconds()
	switch {
	case novo.HoldThresholdMs < minimo || novo.HoldThresholdMs > maximo:
		return fmt.Sprintf("O tempo segurando a tecla até começar a gravar precisa ficar entre %d e %d ms.", minimo, maximo)
	case novo.TextMode != ModoLiteral && novo.TextMode != ModoSmart && novo.TextMode != ModoPolish:
		return "Modo de texto desconhecido: use literal, smart ou polish."
	case novo.AIProvider != "" && !ia.Valido(novo.AIProvider):
		return "Provedor de IA desconhecido: use openai ou anthropic."
	case len(novo.AIModel) > tamanhoMaximoDoModelo || strings.ContainsFunc(novo.AIModel, ehControle):
		return "Nome de modelo inválido."
	case pareceChave(novo.AIModel):
		// A mensagem não repete o que veio: se é mesmo a chave, ela não
		// volta nem aqui.
		return "Isso parece uma chave de API, não o nome de um modelo. Cole a chave no campo da chave."
	case novo.TextMode == ModoPolish && novo.AIProvider == "":
		return "O modo Polish precisa de um provedor de IA escolhido."
	case novo.TextMode == ModoPolish && novo.AIModel == "":
		return "O modo Polish precisa de um modelo escolhido."
	}
	return ""
}

func ehControle(r rune) bool { return r < 0x20 || r == 0x7f }

// pareceChave pega a chave colada por engano no campo livre do modelo, ao
// lado do campo da chave. Sem isto ela seria aceita e gravada em texto
// puro em local_settings.ai_model — que é preferência sincronizável
// (05-data-model.md) — e devolvida inteira no GET. "sk-" é o começo das
// chaves dos dois provedores (OpenAI "sk-", "sk-proj-"; Anthropic
// "sk-ant-"); nenhum modelo deles se chama assim. Não é lista de nomes de
// modelo: é uma rede de proteção.
func pareceChave(modelo string) bool {
	return strings.HasPrefix(strings.ToLower(modelo), "sk-")
}
