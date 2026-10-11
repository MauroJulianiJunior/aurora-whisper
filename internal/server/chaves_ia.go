package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/chavedeapi"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/ia"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cloudstore"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/server/cofre"
)

// As chaves da IA de cada pessoa (ADR-021, decisão G e C3a; 06, "As chaves da
// IA"): a chave de API do provedor que a pessoa cola no app do celular, para
// ditar pelo celular. O servidor a testa no provedor, a cifra (cofre) e a
// guarda (cloudstore.ChavesDeIA); o app só recebe se há chave e o fim dela.
//
// As três rotas são só da sessão do navegador e só existem com o arquivo da
// chave de cifra (AI_KEYS_SECRET_FILE; decisão O): sem ele, caem no 404 de
// rota não montada, como as do ditado que vêm depois (rotasDoDitado).
//
// A chave da pessoa nunca vai a log, a erro, a resposta nem a URL. O corpo do
// PUT tem uma frase FIXA para qualquer erro de decodificação: a mensagem do
// decodificador pode trazer um pedaço do que foi colado (um campo
// desconhecido, um caractere inválido).

// Os códigos de recusa das rotas da IA (ADR-021, "A rota": a lista fechada do
// 06). O app decide pelo código, nunca pela frase.
const (
	codeSemAceite            = "SEM_ACEITE"
	codeSemChave             = "SEM_CHAVE"
	codeLimite               = "LIMITE"
	codeProvedorRecusouChave = "PROVEDOR_RECUSOU_CHAVE"
	codeProvedorLimite       = "PROVEDOR_LIMITE"
	codeProvedorFora         = "PROVEDOR_FORA"
)

// As frases fixas das rotas das chaves. Nenhuma depende do que veio no pedido.
const (
	// FraseDoCorpoDaChave é a resposta a QUALQUER erro de decodificação do
	// corpo do PUT (JSON quebrado, campo desconhecido, tipo errado, algo
	// depois do objeto). Exportada para o teste conferir que é ela, e só ela.
	FraseDoCorpoDaChave = `corpo inválido: mande só {"chave": "..."}`
	// FraseDoCorpoGrande é a do corpo acima de maxCorpoDaChave.
	FraseDoCorpoGrande = "corpo grande demais para uma chave de API"
)

// maxCorpoDaChave: a maior chave (chavedeapi.TamanhoMaximo, 2560 bytes) com
// cada byte escapado no JSON (\u00XX, 6 bytes) cabe com folga.
const maxCorpoDaChave = 16 << 10

// AceiteDoDitado diz se a conta tem um aceite vigente do ditado no celular
// que inclui o provedor (ADR-021, decisão H). A C3b o implementa sobre a
// dictation_consents; sem ele (CloudConfig.Aceite nil), ninguém tem aceite e
// o PUT da chave recusa com SEM_ACEITE (falha fechada).
type AceiteDoDitado interface {
	AceiteVigente(ctx context.Context, userID, provedor string) (bool, error)
}

// semAceite é o AceiteDoDitado de quando a C3b ainda não ligou o dela.
type semAceite struct{}

func (semAceite) AceiteVigente(context.Context, string, string) (bool, error) { return false, nil }

// ModelosDoProvedor pergunta ao provedor, com a chave, que modelos ela
// alcança: é o teste da chave (o PUT) e a lista do app (o GET). Uma falha
// *ia.Erro vira o código do motivo dela; qualquer outra, PROVEDOR_FORA. O
// erro nunca é repassado nem registrado.
type ModelosDoProvedor interface {
	ListarModelos(ctx context.Context, provedor, chave string) ([]string, error)
}

// BaseGroq é a API da Groq compatível com a da OpenAI: a lista de modelos é
// GET /openai/v1/models, com a chave no Authorization: Bearer.
const BaseGroq = "https://api.groq.com/openai"

// ModelosPelaRede é o ModelosDoProvedor de produção: o internal/ia, o mesmo
// cliente do Polish do PC (só HTTP/1.1, sem seguir redirecionamento, sem
// repassar o corpo de erro, e com o prazo de ia.TimeoutPadrao). A Groq é
// chamada pela forma da OpenAI, no endereço dela. Os campos existem para os
// testes apontarem para um httptest.Server; vazios, os endereços oficiais.
type ModelosPelaRede struct {
	Cliente  ia.Cliente
	BaseGroq string
}

// errProvedorDaIA: um provedor fora da lista fechada (não chega aqui pelas
// rotas, que conferem antes).
var errProvedorDaIA = errors.New("server: provedor da IA desconhecido")

func (m ModelosPelaRede) ListarModelos(ctx context.Context, provedor, chave string) ([]string, error) {
	cli := m.Cliente
	switch provedor {
	case cloudstore.ProvedorOpenAI:
		return cli.ListarModelos(ctx, ia.OpenAI, chave)
	case cloudstore.ProvedorAnthropic:
		return cli.ListarModelos(ctx, ia.Anthropic, chave)
	case cloudstore.ProvedorGroq:
		cli.BaseOpenAI = m.BaseGroq
		if cli.BaseOpenAI == "" {
			cli.BaseOpenAI = BaseGroq
		}
		return cli.ListarModelos(ctx, ia.OpenAI, chave)
	}
	return nil, errProvedorDaIA
}

// rotasDoDitado são as rotas que só existem com a chave de cifra (ADR-021,
// decisão O): hoje, as das chaves da IA; a C3b e a C5b acrescentam as dela
// aqui. Sem o cofre, nenhuma é montada, e todas caem no 404 de rota não
// montada.
func (c *Cloud) rotasDoDitado() []rota {
	if c.cofre == nil {
		return nil
	}
	return []rota{
		{"PUT /api/v1/ai-keys/{provedor}", AcessoSessao, c.handleGravarChaveDeIA},
		{"DELETE /api/v1/ai-keys/{provedor}", AcessoSessao, c.handleApagarChaveDeIA},
		{"GET /api/v1/ai-keys/{provedor}/models", AcessoSessao, c.handleModelosDaChaveDeIA},
	}
}

// chaveDeIADTO é o que o app recebe de uma chave: o provedor, se há chave e o
// fim dela (no máximo 4 caracteres). Nunca a chave.
type chaveDeIADTO struct {
	Provedor    string `json:"provedor"`
	Configurada bool   `json:"configurada"`
	Final       string `json:"final"`
}

// chaveDeIARequest é o corpo do PUT. O dono e o provedor vêm da sessão e da
// rota, nunca do corpo (não há campo para eles).
type chaveDeIARequest struct {
	Chave string `json:"chave"`
}

// handleGravarChaveDeIA é o PUT /ai-keys/{provedor}: o aceite (antes de ler
// o corpo), a chave do corpo, as regras de chavedeapi.Validar, o limite por
// conta, o teste no provedor (a lista de modelos), a cifra e a gravação por
// cima da anterior. 200 com o provedor, configurada e o fim.
func (c *Cloud) handleGravarChaveDeIA(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	provedor, ok := provedorDaRota(w, r)
	if !ok || !c.conferirAceite(w, r, p.UserID, provedor) {
		return
	}
	colada, ok := lerCorpoDaChave(w, r)
	if !ok {
		return
	}
	chave, err := chavedeapi.Validar(colada)
	if err != nil {
		writeErroDaValidacao(w, err)
		return
	}
	if !c.permitirChaves(w, r, p.UserID) {
		return
	}
	if _, err := c.cfg.Modelos.ListarModelos(r.Context(), provedor, chave); err != nil {
		writeFalhaDoProvedor(w, r, err)
		return
	}
	selada, err := c.cofre.Selar(p.UserID, provedor, chave)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	k, err := c.cfg.ChavesDeIA.GravarChaveDeIA(r.Context(), p.UserID, cloudstore.ChaveDeIA{
		Provedor: provedor, IDDaCifra: selada.IDDaCifra, Nonce: selada.Nonce, Cifrado: selada.Cifrado,
		Final: chavedeapi.Final(chave),
	}, c.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	writeJSON(w, http.StatusOK, chaveDeIADTO{Provedor: provedor, Configurada: true, Final: k.Final})
}

// handleApagarChaveDeIA é o DELETE /ai-keys/{provedor}: apaga a chave cifrada
// da conta. Não haver chave não é erro. 204. Não pede o aceite: tirar a chave
// sempre pode. As cópias cifradas nos backups ficam (a tela manda revogar a
// chave no provedor).
func (c *Cloud) handleApagarChaveDeIA(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	provedor, ok := provedorDaRota(w, r)
	if !ok {
		return
	}
	if _, err := c.cfg.ChavesDeIA.ApagarChaveDeIA(r.Context(), p.UserID, provedor); err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleModelosDaChaveDeIA é o GET /ai-keys/{provedor}/models: a lista de
// modelos que a chave guardada alcança, perguntada ao provedor. Pede o aceite
// (a chave vai ao provedor) e conta no limite por conta.
func (c *Cloud) handleModelosDaChaveDeIA(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	provedor, ok := provedorDaRota(w, r)
	if !ok || !c.conferirAceite(w, r, p.UserID, provedor) {
		return
	}
	chave, err := c.chaveDeIA(r.Context(), p.UserID, provedor)
	if err != nil {
		writeFalhaDaChaveGuardada(w, r, err)
		return
	}
	if !c.permitirChaves(w, r, p.UserID) {
		return
	}
	modelos, err := c.cfg.Modelos.ListarModelos(r.Context(), provedor, chave)
	if err != nil {
		writeFalhaDoProvedor(w, r, err)
		return
	}
	if modelos == nil {
		modelos = []string{}
	}
	anotarContagens(r.Context(), "modelos", len(modelos))
	writeJSON(w, http.StatusOK, map[string]any{"provedor": provedor, "modelos": modelos})
}

// chaveDeIA lê e decifra a chave guardada da conta para o provedor, para usar
// só durante a chamada ao provedor (a C5b a chama a cada ditado). Os erros:
// cloudstore.ErrChaveDeIAAusente (nunca colou), cofre.ErrOutraChaveDeCifra
// (o arquivo da chave de cifra trocou, ou o backup voltou sem ele) e
// cofre.ErrNaoDecifra (a linha não é desta conta e deste provedor); os três
// são SEM_CHAVE para o app (writeFalhaDaChaveGuardada). Outro erro é do
// banco.
func (c *Cloud) chaveDeIA(ctx context.Context, userID, provedor string) (string, error) {
	k, err := c.cfg.ChavesDeIA.LerChaveDeIA(ctx, userID, provedor)
	if err != nil {
		return "", err
	}
	return c.cofre.Abrir(userID, provedor, cofre.Selada{IDDaCifra: k.IDDaCifra, Nonce: k.Nonce, Cifrado: k.Cifrado})
}

// semChave diz se o erro de chaveDeIA é "a pessoa precisa colar a chave".
func semChave(err error) bool {
	return errors.Is(err, cloudstore.ErrChaveDeIAAusente) || errors.Is(err, cofre.ErrOutraChaveDeCifra) ||
		errors.Is(err, cofre.ErrNaoDecifra)
}

// writeFalhaDaChaveGuardada responde ao erro de chaveDeIA: SEM_CHAVE (409),
// com uma frase para "nunca colou" e outra para "não vale mais", ou 500.
func writeFalhaDaChaveGuardada(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, cloudstore.ErrChaveDeIAAusente):
		anotarMotivo(r.Context(), "sem_chave")
		writeError(w, http.StatusConflict, codeSemChave, "nenhuma chave guardada para este provedor")
	case semChave(err):
		anotarMotivo(r.Context(), "sem_chave")
		writeError(w, http.StatusConflict, codeSemChave, "a chave guardada não vale mais neste servidor: cole a chave de novo")
	default:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
	}
}

// provedorDaRota lê o {provedor} da rota: exatamente um da lista fechada, em
// minúsculas. Outro é 404, sem eco do valor.
func provedorDaRota(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := r.PathValue("provedor")
	if !cloudstore.ProvedorDeIAValido(p) {
		writeError(w, http.StatusNotFound, codeNotFound, "provedor desconhecido")
		return "", false
	}
	return p, true
}

// conferirAceite recusa (403 SEM_ACEITE) quem não tem o aceite vigente do
// provedor, antes de ler o corpo e sem chamar o provedor.
func (c *Cloud) conferirAceite(w http.ResponseWriter, r *http.Request, userID, provedor string) bool {
	ok, err := c.aceite.AceiteVigente(r.Context(), userID, provedor)
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return false
	case !ok:
		anotarMotivo(r.Context(), "sem_aceite")
		writeError(w, http.StatusForbidden, codeSemAceite, "aceite o aviso do ditado no celular para este provedor antes")
		return false
	}
	return true
}

// lerCorpoDaChave lê {"chave": "..."} e nada mais. QUALQUER erro de
// decodificação tem a mesma frase fixa (FraseDoCorpoDaChave), e o corpo acima
// do teto, a dele: a mensagem do decodificador nunca vai para a resposta.
func lerCorpoDaChave(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCorpoDaChave)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body chaveDeIARequest
	err := dec.Decode(&body)
	if err == nil {
		// Nada depois do objeto (só espaço).
		if err = dec.Decode(&struct{}{}); errors.Is(err, io.EOF) {
			err = nil
		} else if err == nil {
			err = errors.New("server: algo depois do objeto")
		}
	}
	if err != nil {
		var grande *http.MaxBytesError
		if errors.As(err, &grande) {
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, FraseDoCorpoGrande)
			return "", false
		}
		writeError(w, http.StatusBadRequest, codeBadRequest, FraseDoCorpoDaChave)
		return "", false
	}
	return body.Chave, true
}

// writeErroDaValidacao responde ao erro de chavedeapi.Validar com as frases do
// painel do PC (localapi), sem a chave.
func writeErroDaValidacao(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chavedeapi.ErrChaveVazia):
		writeError(w, http.StatusBadRequest, codeValidation, "Cole a chave antes de salvar.")
	case errors.Is(err, chavedeapi.ErrChaveLonga):
		writeError(w, http.StatusBadRequest, codeValidation, "Isso é longo demais para ser uma chave de API.")
	default:
		writeError(w, http.StatusBadRequest, codeValidation,
			"A chave tem espaço, quebra de linha ou caractere estranho no meio. Copie de novo, só a chave.")
	}
}

// permitirChaves gasta uma ficha do limite das chaves da conta (o PUT e o GET
// dos modelos juntos: ADR-021, escolha menor 24) logo antes de chamar o
// provedor. Sem ficha, 429 LIMITE com Retry-After, sem chamar.
func (c *Cloud) permitirChaves(w http.ResponseWriter, r *http.Request, userID string) bool {
	ok, wait := c.limChaves.Permitir(userID, 1)
	if ok {
		return true
	}
	negarPorLimite(w, r, wait)
	writeError(w, http.StatusTooManyRequests, codeLimite, "pedidos demais às chaves da IA; espere um pouco")
	return false
}

// FalhaDoProvedor é o que uma falha do provedor vira para o app: o status, o
// código da lista fechada e uma frase fixa. Nunca a mensagem do erro, que não
// sai daqui. A rota do ditado (C5b) usa a mesma tradução.
func FalhaDoProvedor(err error) (status int, code, frase string) {
	var e *ia.Erro
	if errors.As(err, &e) {
		switch e.Motivo {
		case ia.MotivoChaveRecusada, ia.MotivoSemPermissao:
			return http.StatusUnprocessableEntity, codeProvedorRecusouChave,
				"o provedor recusou a chave: confira se ela foi copiada inteira, se está ativa e se pode listar os modelos"
		case ia.MotivoLimite:
			return http.StatusBadGateway, codeProvedorLimite,
				"o provedor recusou por limite de uso ou falta de crédito: confira o saldo e tente de novo em alguns minutos"
		}
	}
	return http.StatusBadGateway, codeProvedorFora, "o provedor não respondeu como devia: tente de novo mais tarde"
}

func writeFalhaDoProvedor(w http.ResponseWriter, r *http.Request, err error) {
	status, code, frase := FalhaDoProvedor(err)
	anotarMotivo(r.Context(), "provedor")
	writeError(w, status, code, frase)
}
