package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
)

// As preferências e o aceite do ditado no celular (ADR-021, decisões H e N e
// C3b; 06, "O aceite e as preferências do ditado no celular"). Quatro rotas,
// todas só da sessão do navegador e só com o arquivo da chave de cifra (como as
// das chaves da IA, rotasDoDitado):
//
//	GET    /api/v1/dictation           o estado: o aviso, o aceite, as preferências e as chaves
//	PUT    /api/v1/dictation/settings  as preferências, recusando o provedor fora do aceite
//	POST   /api/v1/dictation/consent   grava o aceite, só com a versão atual do aviso
//	DELETE /api/v1/dictation/consent   revoga o aceite e apaga as chaves
//
// O app decide pelo error.code, nunca pela frase. Nada aqui sincroniza com os
// PCs. Nada aqui guarda nem devolve a chave da IA (só se há uma e o fim dela),
// áudio ou texto ditado. Os corpos não têm user_id: a conta vem da sessão.

// codeAvisoDesatualizado: o app mandou o aceite de uma versão do aviso que não
// é a atual (409). Um app com o texto velho na memória não pode registrar que a
// pessoa aceitou o texto novo.
const codeAvisoDesatualizado = "AVISO_DESATUALIZADO"

// As frases fixas das rotas do ditado no celular. Nenhuma depende do que veio
// no pedido.
const (
	// FraseDoCorpoDasPreferencias e FraseDoCorpoDoAceite são a resposta a
	// QUALQUER erro de decodificação do corpo (JSON quebrado, campo
	// desconhecido, tipo errado, algo depois do objeto).
	FraseDoCorpoDasPreferencias = "corpo inválido: mande transcricao, dica e polish"
	FraseDoCorpoDoAceite        = "corpo inválido: mande versao, transcricao e, se houver, polish"
	fraseDoCorpoGrandeDoDitado  = "corpo grande demais"
)

// maxCorpoDoDitado: as preferências e o aceite são um objeto curto.
const maxCorpoDoDitado = 4 << 10

// aceitePeloStore é o AceiteDoDitado de produção: o aceite em aberto da conta,
// da versão atual do aviso (cloudstore.Aceite.Vigente), que inclui o provedor.
// Um aceite revogado, ou de uma versão velha do aviso, não vale: a pessoa
// aceita de novo.
type aceitePeloStore struct{ ditado cloudstore.DitadoNoCelular }

func (a aceitePeloStore) AceiteVigente(ctx context.Context, userID, provedor string) (bool, error) {
	ac, err := a.ditado.LerAceiteAberto(ctx, userID)
	if errors.Is(err, cloudstore.ErrSemAceite) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ac.Vigente() && ac.Inclui(provedor), nil
}

// O que o app recebe.

type avisoDTO struct {
	// Versao é a versão do texto do aviso que o servidor aceita
	// (cloudstore.VersaoDoAviso).
	Versao int `json:"versao"`
}

// aceiteDTO é o aceite em aberto da conta. Vigente diz se ele vale agora: só
// vale se a versão dele é a do aviso atual. Sem aceite em aberto, o estado traz
// null no lugar.
type aceiteDTO struct {
	Versao      int       `json:"versao"`
	Transcricao string    `json:"transcricao"`
	Polish      *string   `json:"polish"`
	AceitoEm    time.Time `json:"aceito_em"`
	Vigente     bool      `json:"vigente"`
}

type provedorEModeloDTO struct {
	Provedor string `json:"provedor"`
	Modelo   string `json:"modelo"`
}

type polishDTO struct {
	Ligado   bool   `json:"ligado"`
	Provedor string `json:"provedor"`
	Modelo   string `json:"modelo"`
}

type preferenciasDTO struct {
	Transcricao provedorEModeloDTO `json:"transcricao"`
	Dica        bool               `json:"dica"`
	Polish      polishDTO          `json:"polish"`
}

// chaveDoEstadoDTO é uma chave no estado: uma entrada por provedor da lista, com
// ou sem chave. Nunca a chave. Configurada diz que há uma chave guardada que
// vale (decifra com a chave de cifra deste servidor) e Final é o fim dela;
// ColarDeNovo diz que havia uma e ela não vale mais (a chave de cifra trocou, ou
// o backup voltou sem ela): a pessoa cola de novo.
type chaveDoEstadoDTO struct {
	Provedor    string `json:"provedor"`
	Configurada bool   `json:"configurada"`
	Final       string `json:"final"`
	ColarDeNovo bool   `json:"colar_de_novo"`
}

type estadoDoDitadoDTO struct {
	Aviso        avisoDTO           `json:"aviso"`
	Aceite       *aceiteDTO         `json:"aceite"`
	Preferencias preferenciasDTO    `json:"preferencias"`
	Chaves       []chaveDoEstadoDTO `json:"chaves"`
}

// O que o app manda. Os campos são ponteiros para a falta de um deles ser
// diferente de "falso" e de "vazio": as preferências são gravadas inteiras.

type preferenciasRequest struct {
	Transcricao *provedorEModeloDTO `json:"transcricao"`
	Dica        *bool               `json:"dica"`
	Polish      *polishDTO          `json:"polish"`
}

type aceiteRequest struct {
	Versao      *int    `json:"versao"`
	Transcricao *string `json:"transcricao"`
	Polish      *string `json:"polish"`
}

func aceiteParaDTO(a cloudstore.Aceite) *aceiteDTO {
	d := &aceiteDTO{Versao: a.Versao, Transcricao: a.Transcricao, AceitoEm: a.AceitoEm.UTC(), Vigente: a.Vigente()}
	if a.Polish != "" {
		p := a.Polish
		d.Polish = &p
	}
	return d
}

func preferenciasParaDTO(p cloudstore.PreferenciasDoDitado) preferenciasDTO {
	return preferenciasDTO{
		Transcricao: provedorEModeloDTO{Provedor: p.Transcricao.Provedor, Modelo: p.Transcricao.Modelo},
		Dica:        p.Dica,
		Polish:      polishDTO{Ligado: p.PolishLigado, Provedor: p.Polish.Provedor, Modelo: p.Polish.Modelo},
	}
}

// estadoDoDitado monta o que o GET devolve, e o que o PUT e o POST devolvem
// depois de gravar: o app atualiza a tela de uma vez.
func (c *Cloud) estadoDoDitado(ctx context.Context, userID string) (estadoDoDitadoDTO, error) {
	est := estadoDoDitadoDTO{Aviso: avisoDTO{Versao: cloudstore.VersaoDoAviso}}
	prefs, err := c.cfg.Ditado.LerPreferenciasDoDitado(ctx, userID)
	if err != nil {
		return est, err
	}
	est.Preferencias = preferenciasParaDTO(prefs)
	switch a, err := c.cfg.Ditado.LerAceiteAberto(ctx, userID); {
	case err == nil:
		est.Aceite = aceiteParaDTO(a)
	case !errors.Is(err, cloudstore.ErrSemAceite):
		return est, err
	}
	linhas, err := c.cfg.ChavesDeIA.ListarChavesDeIA(ctx, userID)
	if err != nil {
		return est, err
	}
	est.Chaves = make([]chaveDoEstadoDTO, 0, len(cloudstore.ProvedoresDeIA))
	for _, p := range cloudstore.ProvedoresDeIA {
		d := chaveDoEstadoDTO{Provedor: p}
		for _, k := range linhas {
			if k.Provedor != p {
				continue
			}
			// Sem decifrar: o id da chave de cifra que cifrou a linha diz se
			// ela ainda vale neste servidor.
			if c.cofre.MesmoID(k.IDDaCifra) {
				d.Configurada, d.Final = true, k.Final
			} else {
				d.ColarDeNovo = true
			}
		}
		est.Chaves = append(est.Chaves, d)
	}
	return est, nil
}

// handleEstadoDoDitado é o GET /dictation.
func (c *Cloud) handleEstadoDoDitado(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	c.responderComOEstado(w, r, p.UserID)
}

func (c *Cloud) responderComOEstado(w http.ResponseWriter, r *http.Request, userID string) {
	est, err := c.estadoDoDitado(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	writeJSON(w, http.StatusOK, est)
}

// handleGravarPreferenciasDoDitado é o PUT /dictation/settings: grava as
// preferências inteiras. Recusa (403 SEM_ACEITE) o provedor da transcrição, e o
// do Polish quando ele está ligado, que não está no aceite vigente: o app abre a
// folha do aceite. Mudar só o modelo de um provedor já aceito não pede aceite
// novo. O modelo da transcrição vazio é o padrão do provedor. 200 com o estado.
func (c *Cloud) handleGravarPreferenciasDoDitado(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	req, ok := lerCorpoEstrito[preferenciasRequest](w, r, maxCorpoDoDitado, FraseDoCorpoDasPreferencias, fraseDoCorpoGrandeDoDitado)
	if !ok {
		return
	}
	if req.Transcricao == nil || req.Dica == nil || req.Polish == nil {
		writeError(w, http.StatusBadRequest, codeValidation, "faltam campos: mande transcricao, dica e polish")
		return
	}
	prefs := cloudstore.PreferenciasDoDitado{
		Transcricao:  cloudstore.ProvedorEModelo{Provedor: req.Transcricao.Provedor, Modelo: req.Transcricao.Modelo},
		Dica:         *req.Dica,
		PolishLigado: req.Polish.Ligado,
		Polish:       cloudstore.ProvedorEModelo{Provedor: req.Polish.Provedor, Modelo: req.Polish.Modelo},
	}
	if prefs.Transcricao.Modelo == "" {
		prefs.Transcricao.Modelo = cloudstore.ModeloPadraoDaTranscricao(prefs.Transcricao.Provedor)
	}
	if err := cloudstore.ConferirPreferencias(prefs); err != nil {
		writeErroDasPreferencias(w, err)
		return
	}
	if !c.conferirAceite(w, r, p.UserID, prefs.Transcricao.Provedor) {
		return
	}
	if prefs.PolishLigado && !c.conferirAceite(w, r, p.UserID, prefs.Polish.Provedor) {
		return
	}
	if _, err := c.cfg.Ditado.GravarPreferenciasDoDitado(r.Context(), p.UserID, prefs, c.now()); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	c.responderComOEstado(w, r, p.UserID)
}

// writeErroDasPreferencias responde ao erro de cloudstore.ConferirPreferencias
// com uma frase fixa, que não repete o que o app mandou.
func writeErroDasPreferencias(w http.ResponseWriter, err error) {
	const forma = "letras, dígitos e . _ : / @ - (de 1 a 100 caracteres)"
	switch {
	case errors.Is(err, cloudstore.ErrProvedorDeTranscricao):
		writeError(w, http.StatusBadRequest, codeValidation, "a transcrição é feita pela openai ou pela groq")
	case errors.Is(err, cloudstore.ErrProvedorDePolish):
		writeError(w, http.StatusBadRequest, codeValidation, "o Polish é feito pela openai ou pela anthropic")
	case errors.Is(err, cloudstore.ErrPolishSemModelo):
		writeError(w, http.StatusBadRequest, codeValidation, "o Polish ligado precisa de um modelo")
	case errors.Is(err, cloudstore.ErrModeloInvalido):
		writeError(w, http.StatusBadRequest, codeValidation, "o nome do modelo só pode ter "+forma)
	default:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
	}
}

// handleGravarAceiteDoDitado é o POST /dictation/consent: grava o aceite, só com
// a versão atual do aviso (409 AVISO_DESATUALIZADO senão). O aceite anterior,
// se havia, termina como substituído; aceitar de novo o mesmo não grava outro.
// Os provedores são os que a folha do aceite mostrou. 200 com o estado.
func (c *Cloud) handleGravarAceiteDoDitado(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	req, ok := lerCorpoEstrito[aceiteRequest](w, r, maxCorpoDoDitado, FraseDoCorpoDoAceite, fraseDoCorpoGrandeDoDitado)
	if !ok {
		return
	}
	if req.Versao == nil || req.Transcricao == nil {
		writeError(w, http.StatusBadRequest, codeValidation, "faltam campos: mande versao e transcricao")
		return
	}
	if *req.Versao != cloudstore.VersaoDoAviso {
		anotarMotivo(r.Context(), "aviso")
		writeError(w, http.StatusConflict, codeAvisoDesatualizado,
			"o aviso que o app mostrou não é o atual: recarregue o app e leia o aviso de novo")
		return
	}
	novo := cloudstore.NovoAceite{Versao: *req.Versao, Transcricao: *req.Transcricao}
	if req.Polish != nil {
		novo.Polish = *req.Polish
		if novo.Polish == "" {
			// "" não é um provedor: "sem Polish" é null ou a falta do campo.
			writeError(w, http.StatusBadRequest, codeValidation, "o Polish é feito pela openai ou pela anthropic")
			return
		}
	}
	if err := cloudstore.ConferirAceite(novo); err != nil {
		if !cloudstore.ProvedorDeTranscricaoValido(novo.Transcricao) {
			writeError(w, http.StatusBadRequest, codeValidation, "a transcrição é feita pela openai ou pela groq")
		} else {
			writeError(w, http.StatusBadRequest, codeValidation, "o Polish é feito pela openai ou pela anthropic")
		}
		return
	}
	if _, err := c.cfg.Ditado.GravarAceite(r.Context(), p.UserID, novo, c.now()); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	c.responderComOEstado(w, r, p.UserID)
}

// handleRevogarAceiteDoDitado é o DELETE /dictation/consent: "Desligar o ditado
// no celular". Termina o aceite em aberto e apaga as chaves da IA da conta,
// numa transação só. Não haver aceite não é erro (as chaves saem do mesmo
// jeito). 204. As cópias cifradas nos backups ficam: a tela manda revogar a
// chave no provedor.
func (c *Cloud) handleRevogarAceiteDoDitado(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	rev, err := c.cfg.Ditado.RevogarAceite(r.Context(), p.UserID, c.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	anotarContagens(r.Context(), "chaves_apagadas", rev.ChavesApagadas)
	w.WriteHeader(http.StatusNoContent)
}
