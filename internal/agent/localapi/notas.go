package localapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// Este arquivo é a API local das notas e dos marcadores (ADR-022, fase N1;
// docs/ai-spec/06-api-contract.md, "Notas e marcadores (Local API)"). Atrás
// da mesma cadeia de segurança de todas as rotas (guard.Protect); o texto da
// nota vai e vem no corpo JSON, nunca na URL (a busca é a exceção, como no
// histórico: o parâmetro q).
//
// # Rotas
//
//	GET    /api/notas                                  ?vista=ativas|arquivo|lixeira &marcador=<id> &q=<busca>
//	POST   /api/notas                                  cria (201)
//	GET    /api/notas/{id}
//	PATCH  /api/notas/{id}                             edita, fixa, arquiva, cor; base_version obrigatório
//	DELETE /api/notas/{id}                             para a lixeira (204)
//	POST   /api/notas/{id}/restaurar                   tira da lixeira
//	POST   /api/notas/{id}/mover                       arrasta a nota: {antes, depois} (200, a nota)
//	POST   /api/notas/{id}/resolver-conflito           a escolha do selo: {base_version, escolha} (200, a nota)
//	DELETE /api/notas/{id}/definitivo                  apaga de vez, só da lixeira (204)
//	PUT    /api/notas/{id}/marcadores/{marcador}       põe o marcador
//	DELETE /api/notas/{id}/marcadores/{marcador}       tira o marcador
//	GET    /api/marcadores
//	POST   /api/marcadores                             cria (201)
//	PATCH  /api/marcadores/{id}                        renomeia; base_version obrigatório
//	DELETE /api/marcadores/{id}                        apaga e tira das notas (204)
//	POST   /api/history/{id}/virar-nota                "Virar nota" do ditado (201, a nota)
//	POST   /api/notas/{id}/desfazer-virar              o Desfazer do aviso (200)
//	PUT    /api/notas/foco                             o cursor está dentro da nota (204; notas_foco.go)
//	DELETE /api/notas/foco                             o cursor saiu (204; corpo opcional)
//	POST   /api/notas/separar-itens                    uma linha em itens, com IA ou por regra (notas_lista.go)
//
// Sem o repositório montado (Preferencias.Notas nil), 503.
//
// Esta fase não toca o sync: nada aqui põe marcador na fila da nuvem (N7).

// Os 409 do "Virar nota", para o painel dizer a frase certa: o ditado está na
// lixeira, ou já virou nota (outra aba, ou o segundo clique).
const (
	codeInTrash     = "IN_TRASH"
	codeAlreadyNote = "ALREADY_NOTE"
)

// codeNameInUse é o 409 de um nome de marcador que já existe, para o painel
// distinguir de uma versão desatualizada (também 409, CONFLICT).
const codeNameInUse = "NAME_IN_USE"

// MsgNotasNaoProntas é a frase do 503 das rotas de notas.
const MsgNotasNaoProntas = "As notas não estão disponíveis nesta versão."

type notaDTO struct {
	ID        string  `json:"id"`
	Titulo    string  `json:"titulo"`
	Corpo     string  `json:"corpo"`
	Cor       *string `json:"cor"`
	Fixada    bool    `json:"fixada"`
	Arquivada bool    `json:"arquivada"`
	// Marcadores são os ids dos marcadores da nota, na ordem do nome.
	Marcadores []string `json:"marcadores"`
	// Posicao é a ordem manual (índice fracionário): o painel não a usa para
	// ordenar (a lista já vem ordenada), mas é o que o sync da N7 leva.
	Posicao        string     `json:"posicao"`
	DitadoDeOrigem *string    `json:"ditado_de_origem,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	Version        int64      `json:"version"`
	// Conflito é o selo (ADR-022, N7): o título e o corpo que perderam um
	// conflito na nuvem, guardados até a pessoa escolher. Ausente sem selo.
	Conflito *seloDaNotaDTO `json:"conflito,omitempty"`
}

// seloDaNotaDTO é o selo da nota como o painel o lê, no formato do selo do
// ditado (seloDTO): quem escreveu (null: o aparelho saiu da conta) e quando.
type seloDaNotaDTO struct {
	Titulo     string    `json:"titulo"`
	Corpo      string    `json:"corpo"`
	DeviceID   *string   `json:"device_id"`
	ConflictAt time.Time `json:"conflict_at"`
}

func toNotaDTO(n storage.Nota) notaDTO {
	ms := n.Marcadores
	if ms == nil {
		ms = []string{}
	}
	var selo *seloDaNotaDTO
	if c := n.Conflito; c != nil {
		selo = &seloDaNotaDTO{Titulo: c.Titulo, Corpo: c.Corpo, DeviceID: c.DeviceID, ConflictAt: c.Em}
	}
	return notaDTO{
		ID: n.ID, Titulo: n.Titulo, Corpo: n.Corpo, Cor: n.Cor, Fixada: n.Fixada, Arquivada: n.Arquivada,
		Marcadores: ms, Posicao: n.Posicao, DitadoDeOrigem: n.DitadoDeOrigem,
		CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, DeletedAt: n.DeletedAt, Version: n.Version,
		Conflito: selo,
	}
}

type notasDTO struct {
	Items []notaDTO `json:"items"`
}

type marcadorDTO struct {
	ID        string    `json:"id"`
	Nome      string    `json:"nome"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version"`
}

func toMarcadorDTO(m storage.MarcadorDeNota) marcadorDTO {
	return marcadorDTO{ID: m.ID, Nome: m.Nome, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Version: m.Version}
}

type marcadoresDTO struct {
	Items []marcadorDTO `json:"items"`
}

// notasProntas devolve o repositório, ou responde 503 e devolve false.
func (s *Server) notasProntas(w http.ResponseWriter) (storage.NotaRepository, bool) {
	if s.pref.Notas == nil {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, MsgNotasNaoProntas)
		return nil, false
	}
	return s.pref.Notas, true
}

// lerCorpoDasNotas decodifica o corpo JSON com o teto de maxCorpoDaEdicao e
// sem aceitar campo desconhecido: um "corpos" digitado errado num PATCH não
// pode virar um PATCH vazio calado.
func lerCorpoDasNotas(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCorpoDaEdicao))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "corpo JSON inválido")
		return false
	}
	return true
}

// writeNotaError traduz os erros do repositório das notas para a resposta do
// contrato, com a frase pronta para a tela.
func writeNotaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "registro não encontrado")
	case errors.Is(err, storage.ErrVersionConflict):
		writeError(w, http.StatusConflict, codeConflict, "versão desatualizada")
	case errors.Is(err, storage.ErrNotaNaLixeira):
		writeError(w, http.StatusConflict, codeConflict, "a nota está na lixeira: restaure antes de mexer nela")
	case errors.Is(err, storage.ErrVizinhoInvalido):
		writeError(w, http.StatusConflict, codeConflict,
			"as notas vizinhas mudaram (ou não estão no mesmo grupo): a lista será lida de novo")
	case errors.Is(err, storage.ErrEscolhaInvalida):
		writeError(w, http.StatusBadRequest, codeBadRequest, "escolha do selo inválida: use manter ou trocar")
	case errors.Is(err, storage.ErrMoverSemVizinho):
		writeError(w, http.StatusBadRequest, codeBadRequest, "mover pede a nota de antes ou a de depois")
	case errors.Is(err, storage.ErrNotaForaDaLixeira):
		writeError(w, http.StatusConflict, codeConflict, "só se apaga de vez a nota que está na lixeira")
	case errors.Is(err, storage.ErrDitadoNaLixeira):
		writeError(w, http.StatusConflict, codeInTrash, "o ditado está na lixeira: restaure antes de virar nota")
	case errors.Is(err, storage.ErrDitadoJaVirouNota):
		writeError(w, http.StatusConflict, codeAlreadyNote, "este ditado já virou nota")
	case errors.Is(err, storage.ErrNotaNaoVeioDeDitado):
		writeError(w, http.StatusConflict, codeConflict, "a nota não está virada de um ditado, ou o ditado já voltou ao histórico")
	case errors.Is(err, storage.ErrMarcadorDuplicado):
		writeError(w, http.StatusConflict, codeNameInUse, "já existe um marcador com esse nome")
	case errors.Is(err, storage.ErrCorInvalida):
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"cor inválida: use "+strings.Join(storage.CoresDaNota, ", ")+" ou null")
	case errors.Is(err, storage.ErrTokenInvalido):
		writeError(w, http.StatusBadRequest, codeBadRequest, "token inválido")
	case errors.Is(err, storage.ErrNotaVazia):
		writeError(w, http.StatusBadRequest, codeBadRequest, "a nota está vazia")
	case errors.Is(err, storage.ErrTituloGrande):
		writeError(w, http.StatusBadRequest, codeBadRequest, "título grande demais: até 1 KiB")
	case errors.Is(err, storage.ErrCorpoGrande):
		writeError(w, http.StatusBadRequest, codeBadRequest, "texto grande demais: até 64 KiB")
	case errors.Is(err, storage.ErrTextoInvalido):
		writeError(w, http.StatusBadRequest, codeBadRequest, "texto com caractere inválido")
	case errors.Is(err, storage.ErrNomeDeMarcadorInvalido):
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"nome de marcador inválido: de 1 a 50 caracteres, em uma linha")
	default:
		writeError(w, http.StatusInternalServerError, codeInternal, "erro interno")
	}
}

// handleListNotas atende GET /api/notas.
func (s *Server) handleListNotas(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	vista := storage.VistaDasNotas(q.Get("vista"))
	if vista == "" {
		vista = storage.VistaAtivas
	}
	if !vista.Valida() {
		writeError(w, http.StatusBadRequest, codeBadRequest, "parâmetro vista inválido: use ativas, arquivo ou lixeira")
		return
	}
	notas, err := repo.Listar(r.Context(), storage.FiltroDeNotas{
		UserID: s.userID, Vista: vista, MarcadorID: q.Get("marcador"), Busca: q.Get("q"),
	})
	if err != nil {
		writeNotaError(w, err)
		return
	}
	items := make([]notaDTO, len(notas))
	for i, n := range notas {
		items[i] = toNotaDTO(n)
	}
	writeJSON(w, http.StatusOK, notasDTO{Items: items})
}

type criarNotaRequest struct {
	Titulo         string   `json:"titulo"`
	Corpo          string   `json:"corpo"`
	Cor            *string  `json:"cor"`
	Fixada         bool     `json:"fixada"`
	Arquivada      bool     `json:"arquivada"`
	DitadoDeOrigem string   `json:"ditado_de_origem"`
	Marcadores     []string `json:"marcadores"`
	// Token é o do "Criar uma nota" aberto (PUT /api/notas/foco): os ditados
	// feitos dentro da caixa passam a ser os desta nota, na mesma transação.
	Token string `json:"token"`
}

// handleCriarNota atende POST /api/notas.
func (s *Server) handleCriarNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	var body criarNotaRequest
	if !lerCorpoDasNotas(w, r, &body) {
		return
	}
	cor := ""
	if body.Cor != nil {
		cor = *body.Cor
	}
	n, err := repo.Criar(r.Context(), storage.NovaNota{
		UserID: s.userID, Titulo: body.Titulo, Corpo: body.Corpo, Cor: cor, Fixada: body.Fixada,
		Arquivada: body.Arquivada, DitadoDeOrigem: body.DitadoDeOrigem, Marcadores: body.Marcadores,
		Token: body.Token,
	})
	if errors.Is(err, storage.ErrNotFound) {
		// Aqui o 404 não é da nota (ela nem existe ainda): é um marcador do
		// pedido que não existe.
		writeError(w, http.StatusBadRequest, codeBadRequest, "marcador inexistente")
		return
	}
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toNotaDTO(n))
}

// handleGetNota atende GET /api/notas/{id}, inclusive a da lixeira.
func (s *Server) handleGetNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	n, err := repo.Get(r.Context(), s.userID, r.PathValue("id"))
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotaDTO(n))
}

// campoCor distingue "cor" ausente (não muda), null ou "" (tira a cor) e um
// nome (põe a cor), que um *string sozinho não distingue.
type campoCor struct {
	Presente bool
	Valor    *string
}

func (c *campoCor) UnmarshalJSON(b []byte) error {
	c.Presente = true
	return json.Unmarshal(b, &c.Valor)
}

type patchNotaRequest struct {
	// BaseVersion é obrigatório: a concorrência otimista de toda edição.
	BaseVersion *int64   `json:"base_version"`
	Titulo      *string  `json:"titulo"`
	Corpo       *string  `json:"corpo"`
	Cor         campoCor `json:"cor"`
	Fixada      *bool    `json:"fixada"`
	Arquivada   *bool    `json:"arquivada"`
}

// handlePatchNota atende PATCH /api/notas/{id}: só muda o que veio no corpo.
// 409 CONFLICT se base_version não é a versão atual, ou se a nota está na
// lixeira.
func (s *Server) handlePatchNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	var body patchNotaRequest
	if !lerCorpoDasNotas(w, r, &body) {
		return
	}
	if body.BaseVersion == nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "base_version é obrigatório")
		return
	}
	m := storage.MudancaDaNota{
		Titulo: body.Titulo, Corpo: body.Corpo, Fixada: body.Fixada, Arquivada: body.Arquivada,
		MudarCor: body.Cor.Presente,
	}
	if body.Cor.Valor != nil {
		m.Cor = *body.Cor.Valor
	}
	if m.Vazia() {
		writeError(w, http.StatusBadRequest, codeBadRequest, "nada a mudar: mande titulo, corpo, cor, fixada ou arquivada")
		return
	}
	n, err := repo.Atualizar(r.Context(), s.userID, r.PathValue("id"), *body.BaseVersion, m)
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotaDTO(n))
}

// handleDeleteNota atende DELETE /api/notas/{id}: a lixeira (30 dias).
// Repetido, responde 204 sem reiniciar o prazo.
func (s *Server) handleDeleteNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	if err := repo.SoftDelete(r.Context(), s.userID, r.PathValue("id"), time.Now()); err != nil {
		writeNotaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRestaurarNota atende POST /api/notas/{id}/restaurar e devolve a nota
// (o painel precisa da versão nova).
func (s *Server) handleRestaurarNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	n, err := repo.Restaurar(r.Context(), s.userID, r.PathValue("id"))
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotaDTO(n))
}

type moverNotaRequest struct {
	// Antes e Depois são as vizinhas da nota DEPOIS de soltar: a de cima e a de
	// baixo (na grade, a anterior e a seguinte na ordem de leitura). null ou
	// ausente é "nada desse lado": Antes nulo é o topo do grupo, Depois nulo o fim.
	Antes  *string `json:"antes"`
	Depois *string `json:"depois"`
}

// handleMoverNota atende POST /api/notas/{id}/mover: o arrastar da ordem
// manual (ADR-022, N2b). Sem base_version: mover não é editar o texto. 409 se
// as vizinhas não valem (outra aba mudou a lista, ou são de outro grupo, da
// lixeira ou do Arquivo); 400 sem nenhuma vizinha. Devolve a nota, com a
// versão nova.
func (s *Server) handleMoverNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	var body moverNotaRequest
	if !lerCorpoDasNotas(w, r, &body) {
		return
	}
	antes, depois := "", ""
	if body.Antes != nil {
		antes = *body.Antes
	}
	if body.Depois != nil {
		depois = *body.Depois
	}
	n, err := repo.Mover(r.Context(), s.userID, r.PathValue("id"), antes, depois)
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotaDTO(n))
}

type resolverConflitoDaNotaRequest struct {
	// BaseVersion é obrigatório, como na edição: a escolha vale para a versão
	// que a pessoa viu.
	BaseVersion *int64 `json:"base_version"`
	// Escolha é "manter" (fica o texto atual) ou "trocar" (fica o do selo).
	Escolha string `json:"escolha"`
}

// handleResolverConflitoDaNota atende POST /api/notas/{id}/resolver-conflito:
// a escolha do selo da nota (ADR-022, N7), como a do ditado (PATCH
// resolver_conflito). 400 sem base_version ou com escolha fora de "manter" e
// "trocar"; 404 sem a nota; 409 com a versão velha ou sem selo (o painel
// relê). A escolha entra na fila do sync na própria transação do repositório.
// Devolve a nota, com a versão nova. Nada do texto vai ao log.
func (s *Server) handleResolverConflitoDaNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	var body resolverConflitoDaNotaRequest
	if !lerCorpoDasNotas(w, r, &body) {
		return
	}
	if body.BaseVersion == nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "base_version é obrigatório")
		return
	}
	n, err := repo.ResolverConflitoDaNota(r.Context(), s.userID, r.PathValue("id"), *body.BaseVersion, body.Escolha)
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotaDTO(n))
}

// handleApagarNotaDeVez atende DELETE /api/notas/{id}/definitivo: só a nota
// que já está na lixeira.
func (s *Server) handleApagarNotaDeVez(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	if err := repo.ApagarDeVez(r.Context(), s.userID, r.PathValue("id")); err != nil {
		writeNotaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMarcarNota atende PUT /api/notas/{id}/marcadores/{marcador}.
// Idempotente; devolve a nota.
func (s *Server) handleMarcarNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	n, err := repo.Marcar(r.Context(), s.userID, r.PathValue("id"), r.PathValue("marcador"))
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotaDTO(n))
}

// handleDesmarcarNota atende DELETE /api/notas/{id}/marcadores/{marcador}.
// Idempotente; devolve a nota.
func (s *Server) handleDesmarcarNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	n, err := repo.Desmarcar(r.Context(), s.userID, r.PathValue("id"), r.PathValue("marcador"))
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNotaDTO(n))
}

// handleListMarcadores atende GET /api/marcadores.
func (s *Server) handleListMarcadores(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	ms, err := repo.ListarMarcadores(r.Context(), s.userID)
	if err != nil {
		writeNotaError(w, err)
		return
	}
	items := make([]marcadorDTO, len(ms))
	for i, m := range ms {
		items[i] = toMarcadorDTO(m)
	}
	writeJSON(w, http.StatusOK, marcadoresDTO{Items: items})
}

type criarMarcadorRequest struct {
	Nome string `json:"nome"`
}

// handleCriarMarcador atende POST /api/marcadores. Nome já usado: 409 com o
// código NAME_IN_USE.
func (s *Server) handleCriarMarcador(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	var body criarMarcadorRequest
	if !lerCorpoDasNotas(w, r, &body) {
		return
	}
	m, err := repo.CriarMarcador(r.Context(), s.userID, body.Nome)
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toMarcadorDTO(m))
}

type patchMarcadorRequest struct {
	BaseVersion *int64 `json:"base_version"`
	Nome        string `json:"nome"`
}

// handlePatchMarcador atende PATCH /api/marcadores/{id}: renomeia.
func (s *Server) handlePatchMarcador(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	var body patchMarcadorRequest
	if !lerCorpoDasNotas(w, r, &body) {
		return
	}
	if body.BaseVersion == nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "base_version é obrigatório")
		return
	}
	m, err := repo.RenomearMarcador(r.Context(), s.userID, r.PathValue("id"), *body.BaseVersion, body.Nome)
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMarcadorDTO(m))
}

// handleDeleteMarcador atende DELETE /api/marcadores/{id}: tira o marcador
// das notas e não apaga nota.
func (s *Server) handleDeleteMarcador(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	if err := repo.ApagarMarcador(r.Context(), s.userID, r.PathValue("id")); err != nil {
		writeNotaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVirarNota atende POST /api/history/{id}/virar-nota: a ação "Virar
// nota" do histórico (ADR-022). Cria a nota com o texto do ditado e esconde o
// ditado do histórico, numa transação; devolve a nota (201). 404 se o ditado
// não existe, 409 IN_TRASH se está na lixeira, 409 ALREADY_NOTE se já virou
// nota. Não toca o sync.
func (s *Server) handleVirarNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	n, err := repo.VirarNota(r.Context(), s.userID, r.PathValue("id"))
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toNotaDTO(n))
}

type desfeitoDTO struct {
	DitadoID string `json:"ditado_id"`
	// NotaApagada: a nota estava intacta e saiu; falso, a nota foi editada e
	// ficou (só o ditado voltou ao histórico).
	NotaApagada bool `json:"nota_apagada"`
}

// handleDesfazerVirarNota atende POST /api/notas/{id}/desfazer-virar: o
// Desfazer do aviso "Virou nota". Devolve o ditado ao histórico e apaga a nota
// se ninguém a editou; 409 se a nota não veio de um ditado ou o ditado já
// voltou.
func (s *Server) handleDesfazerVirarNota(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.notasProntas(w)
	if !ok {
		return
	}
	d, err := repo.DesfazerVirarNota(r.Context(), s.userID, r.PathValue("id"))
	if err != nil {
		writeNotaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, desfeitoDTO{DitadoID: d.DitadoID, NotaApagada: d.NotaApagada})
}
