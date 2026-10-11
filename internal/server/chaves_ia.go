package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/asr/nuvem"
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
	// FraseDaContaQueMudou é a do 409 CONFLICT do PUT cuja conta apagou os
	// dados (ou teve o banco restaurado) enquanto o provedor respondia.
	FraseDaContaQueMudou = "os dados da conta mudaram enquanto a chave era conferida: nada foi guardado"
)

// maxCorpoDaChave: a maior chave (chavedeapi.TamanhoMaximo, 2560 bytes) com
// cada byte escapado no JSON (\u00XX, 6 bytes) cabe com folga.
const maxCorpoDaChave = 16 << 10

// AceiteDoDitado diz se a conta tem um aceite vigente do ditado no celular
// que inclui o provedor (ADR-021, decisão H). O de produção é o
// aceitePeloStore (ditado_celular.go), sobre a dictation_consents; sem ele
// nem o Ditado (CloudConfig), ninguém tem aceite e o PUT da chave recusa com
// SEM_ACEITE (falha fechada).
type AceiteDoDitado interface {
	AceiteVigente(ctx context.Context, userID, provedor string) (bool, error)
}

// semAceite é o AceiteDoDitado de quando nada guarda o aceite (falha fechada).
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
// decisão O): as das chaves da IA (C3a), as das preferências e do aceite
// (/dictation, C3b) e a de ditar (/dictations, C5b; ditar.go). Sem o cofre,
// nenhuma é montada, e todas caem no 404 de rota não montada. Todas só da
// sessão do navegador: a de ditar pelo AcessoDitar, que no passo 1 é a sessão e
// nunca o token de um PC.
func (c *Cloud) rotasDoDitado() []rota {
	if c.cofre == nil {
		return nil
	}
	return []rota{
		{"POST /api/v1/dictations", AcessoDitar, c.handleDitar},

		{"PUT /api/v1/ai-keys/{provedor}", AcessoSessao, c.handleGravarChaveDeIA},
		{"DELETE /api/v1/ai-keys/{provedor}", AcessoSessao, c.handleApagarChaveDeIA},
		{"GET /api/v1/ai-keys/{provedor}/models", AcessoSessao, c.handleModelosDaChaveDeIA},

		{"GET /api/v1/dictation", AcessoSessao, c.handleEstadoDoDitado},
		{"PUT /api/v1/dictation/settings", AcessoSessao, c.handleGravarPreferenciasDoDitado},
		{"POST /api/v1/dictation/consent", AcessoSessao, c.handleGravarAceiteDoDitado},
		{"DELETE /api/v1/dictation/consent", AcessoSessao, c.handleRevogarAceiteDoDitado},
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

// handleGravarChaveDeIA é o PUT /ai-keys/{provedor}: o epoch da conta (lido
// primeiro), o aceite (antes de ler o corpo), a chave do corpo, as regras de
// chavedeapi.Validar, o limite por conta, o teste no provedor (a lista de
// modelos), a cifra e a gravação por cima da anterior. 200 com o provedor,
// configurada e o fim.
//
// O provedor leva até 10 s para responder, e nesse tempo a pessoa pode apagar
// os dados da conta, ou desligar o ditado, em outro aparelho. Por isso a
// gravação não confia no que foi lido no começo: ela confere de novo o epoch
// (lido aqui, antes de tudo) e o aceite DENTRO da transação que trava a conta
// (GravarChaveDeIAConferindo). A chave nunca sobrevive a um apagar nem a um
// revogar, nem os que terminam durante a chamada ao provedor.
func (c *Cloud) handleGravarChaveDeIA(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	provedor, ok := provedorDaRota(w, r)
	if !ok {
		return
	}
	epoch, err := c.cfg.ChavesDeIA.EpochDaConta(r.Context(), p.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	if !c.conferirAceite(w, r, p.UserID, provedor) {
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
	modelos, err := c.cfg.Modelos.ListarModelos(r.Context(), provedor, chave)
	if err != nil {
		writeFalhaDoProvedor(w, r, err)
		return
	}
	// A lista que o provedor deu é a dos modelos que o ditado aceita sem
	// perguntar de novo (modelos_conhecidos.go).
	c.conhecidos.anotar(provedor, modelos)
	selada, err := c.cofre.Selar(p.UserID, provedor, chave)
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "falha interna")
		return
	}
	k, err := c.cfg.ChavesDeIA.GravarChaveDeIAConferindo(r.Context(), p.UserID,
		cloudstore.CondicaoDaChave{Epoch: epoch, ExigeAceite: c.aceiteNaGravacao},
		cloudstore.ChaveDeIA{
			Provedor: provedor, IDDaCifra: selada.IDDaCifra, Nonce: selada.Nonce, Cifrado: selada.Cifrado,
			Final: chavedeapi.Final(chave),
		}, c.now())
	switch {
	case errors.Is(err, cloudstore.ErrEpochChanged):
		// A conta apagou os dados enquanto o provedor respondia: nada foi
		// guardado. A pessoa vê o estado de agora ao tentar de novo.
		anotarMotivo(r.Context(), "conta_mudou")
		writeError(w, http.StatusConflict, codeConflict, FraseDaContaQueMudou)
		return
	case errors.Is(err, cloudstore.ErrSemAceite):
		// O aceite foi revogado (ou trocado por um que não cobre o provedor)
		// enquanto o provedor respondia: a mesma recusa da conferência do começo.
		c.negarSemAceite(w, r)
		return
	case err != nil:
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
//
// É um GET com efeito colateral: chama o provedor com a chave da conta e gasta
// uma ficha do limite. O http.CrossOriginProtection só barra os métodos que
// mudam estado, e o cookie da sessão é SameSite=Lax, que o navegador manda numa
// navegação de topo vinda de outro site. Então esta rota recusa o pedido que o
// navegador diz não ser da própria origem (Sec-Fetch-Site presente e diferente
// de same-origin): 403, antes do aceite, da chave e do limite. O que não manda o
// cabeçalho (um cliente que não é navegador) passa. A resposta nem seria lida
// pelo outro site; o que se evita é o gasto da chave e das fichas.
func (c *Cloud) handleModelosDaChaveDeIA(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFromContext(r.Context())
	provedor, ok := provedorDaRota(w, r)
	if !ok || !c.somenteDaMesmaOrigem(w, r) || !c.conferirAceite(w, r, p.UserID, provedor) {
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
	c.conhecidos.anotar(provedor, modelos)
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
		c.negarSemAceite(w, r)
		return false
	}
	return true
}

// negarSemAceite é a resposta 403 SEM_ACEITE, a mesma na conferência do começo
// e na da gravação.
func (c *Cloud) negarSemAceite(w http.ResponseWriter, r *http.Request) {
	anotarMotivo(r.Context(), "sem_aceite")
	writeError(w, http.StatusForbidden, codeSemAceite, "aceite o aviso do ditado no celular para este provedor antes")
}

// lerCorpoDaChave lê {"chave": "..."} e nada mais. QUALQUER erro de
// decodificação tem a mesma frase fixa (FraseDoCorpoDaChave), e o corpo acima
// do teto, a dele: a mensagem do decodificador nunca vai para a resposta.
func lerCorpoDaChave(w http.ResponseWriter, r *http.Request) (string, bool) {
	body, ok := lerCorpoEstrito[chaveDeIARequest](w, r, maxCorpoDaChave, FraseDoCorpoDaChave, FraseDoCorpoGrande)
	return body.Chave, ok
}

// lerCorpoEstrito lê um objeto JSON de T e nada mais: campo desconhecido, tipo
// errado, corpo vazio e um segundo valor depois do objeto são o mesmo erro, e a
// resposta é SEMPRE a frase fixa que o chamador deu (400 BAD_REQUEST): a
// mensagem do decodificador nunca vai para a resposta, porque pode trazer um
// pedaço do que foi mandado. O corpo acima do teto é 413, com a frase dele.
// Em caso de erro, já escreve a resposta e devolve ok=false.
func lerCorpoEstrito[T any](w http.ResponseWriter, r *http.Request, teto int64, fraseInvalida, fraseGrande string) (T, bool) {
	var body T
	r.Body = http.MaxBytesReader(w, r.Body, teto)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
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
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, fraseGrande)
		} else {
			writeError(w, http.StatusBadRequest, codeBadRequest, fraseInvalida)
		}
		var zero T
		return zero, false
	}
	return body, true
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
// sai daqui. A rota de ditar (C5b; ditar.go) usa a mesma tradução: além do
// *ia.Erro (a lista de modelos), ela recebe o *nuvem.Erro da transcrição, com
// os mesmos motivos e dois a mais (sem_chave, que é SEM_CHAVE, e
// arquivo_grande, que é PROVEDOR_FORA: o teto do corpo fica muito abaixo do
// do provedor).
func FalhaDoProvedor(err error) (status int, code, frase string) {
	var e *ia.Erro
	var v *nuvem.Erro
	motivo := ""
	switch {
	case errors.As(err, &e):
		motivo = string(e.Motivo)
	case errors.As(err, &v):
		motivo = string(v.Motivo)
	}
	switch motivo {
	case string(ia.MotivoChaveRecusada), string(ia.MotivoSemPermissao):
		return http.StatusUnprocessableEntity, codeProvedorRecusouChave,
			"o provedor recusou a chave: confira se ela foi copiada inteira, se está ativa e se pode listar os modelos"
	case string(ia.MotivoLimite):
		return http.StatusBadGateway, codeProvedorLimite,
			"o provedor recusou por limite de uso ou falta de crédito: confira o saldo e tente de novo em alguns minutos"
	case string(nuvem.MotivoSemChave):
		return http.StatusConflict, codeSemChave, "a chave guardada não vale mais neste servidor: cole a chave de novo"
	}
	return http.StatusBadGateway, codeProvedorFora, "o provedor não respondeu como devia: tente de novo mais tarde"
}

func writeFalhaDoProvedor(w http.ResponseWriter, r *http.Request, err error) {
	status, code, frase := FalhaDoProvedor(err)
	anotarMotivo(r.Context(), "provedor")
	writeError(w, status, code, frase)
}
