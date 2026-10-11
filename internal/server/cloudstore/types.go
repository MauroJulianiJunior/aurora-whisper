package cloudstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Transcription é o ditado na nuvem (05-data-model.md, "Transcription", como
// a F1 fechou a 0003). Não tem target_app (ADR-019, decisão 10), nem áudio,
// nem contexto de janela: não existe campo para isso.
//
// Os ponteiros nunca são alterados no lugar: as regras trocam o ponteiro
// inteiro, então copiar a struct é copiar o estado.
type Transcription struct {
	ID     string
	UserID string
	// DeviceID é o aparelho onde o ditado nasceu (o que fez o push que o
	// criou). Não muda depois. O aparelho da ÚLTIMA mudança é outra coisa:
	// mora no sync_event e sai no pull como o device_id da mudança. Nulo no
	// ditado que nasceu na nuvem (Store.Create; SourcePlatform web ou android).
	DeviceID       *string
	SourcePlatform string
	RawText        string
	FinalText      string
	Mode           string
	Language       *string
	ASRProvider    string
	ASRModel       *string
	DurationMs     *int64
	ProcessingMs   *int64
	// Confidence é REAL no Postgres (4 bytes); aqui também, para o servidor em
	// memória e o de Postgres devolverem o mesmo número.
	Confidence *float32
	Status     string
	// CreatedAt é a data do ditado, a do PC. A retenção conta por ela.
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
	PurgeAt   *time.Time

	// Version sobe a cada gravação da linha.
	Version int64
	// TextVersion é a versão em que o texto final mudou por último, e
	// TrashVersion, a versão em que a lixeira mudou por último (entrar ou
	// sair). É com elas que o push sabe o que o servidor mudou DEPOIS da base
	// que o PC conhecia: sem isso, a lixeira de um lado e o texto do outro
	// pareceriam um conflito (ADR-019, "Conflito" e "Eco e mescla").
	TextVersion  int64
	TrashVersion int64
	// TextDeviceID e TrashDeviceID são o aparelho que fez a última mudança do
	// texto e a da lixeira (nulos quando veio do celular). Com eles, a
	// mudança que o próprio PC mandou não conta, para ele, como "mudou depois
	// da base": depois de um applied mesclado, o PC continua com a base velha
	// (ADR-019, "Eco e mescla"), mas o campo que ele mesmo mudou por último
	// ele conhece, porque o valor é o que ele mandou. Sem isso, o restaurar
	// seguinte do mesmo PC perdia para a lixeira dele mesmo, e a segunda
	// edição perdia para a primeira (ADR-019, "Decisões da F2").
	TextDeviceID  *string
	TrashDeviceID *string
	// LastSeq é o seq da última mudança (o pull lê as linhas por ele).
	LastSeq int64

	// Conflict é o selo: a versão que perdeu um conflito. Nulo sem selo.
	Conflict *Conflict
}

// Conflict é a versão que perdeu um conflito de texto, guardada no próprio
// item até a pessoa escolher (o selo; a escolha pelo CRUD entrou na F4,
// ResolveConflict).
type Conflict struct {
	FinalText string
	// DeviceID é o aparelho cuja versão perdeu; nulo quando veio do celular.
	DeviceID *string
	At       time.Time
}

// Learned é o aprendido na nuvem (learned_pairs, a 0016; ADR-019, "Decisões
// da F8"): uma variante que o botão Aprender de um PC gravou, e o termo certo.
//
// A identidade é a variante, e não o par: ID é o UUID v5 da chave
// dictionary.ChaveDoPar(Variant) (dictionary.IDDoPar), o mesmo em todo PC e
// em toda conta, e NormalizedVariant é essa chave. O servidor calcula os dois
// e nunca os recebe prontos (DecideLearned confere o entity_id contra a
// variante). Trocar o termo é mudar a mesma linha.
//
// Não tem a prioridade, a dica, o idioma, o usuario do arquivo nem a origem; e
// Domain é só o NOME do domínio, nunca o mapa dominios (que programas a
// pessoa usa). Os ponteiros nunca são alterados no lugar, como em
// Transcription.
type Learned struct {
	ID                string
	UserID            string
	Variant           string
	NormalizedVariant string
	Term              string
	// Restricted é a restrição efetiva: a troca só age no domínio. Só com
	// Domain.
	Restricted bool
	// Domain é o nome do domínio; nulo sem domínio.
	Domain    *string
	CreatedAt time.Time
	UpdatedAt time.Time
	// UndoneAt é quando foi desfeito. A linha continua (o aprendido não tem
	// lápide: "desfeito em outro aparelho" precisa ser lembrado); reaprender
	// é a mesma linha, com UndoneAt limpo e a versão mais um.
	UndoneAt *time.Time
	// Version sobe a cada gravação da linha; LastSeq é o seq da última.
	Version int64
	LastSeq int64
}

// Actor é quem grava: o usuário (sempre da sessão ou do token, nunca do
// corpo) e o aparelho, quando a credencial é o token de um PC. DeviceID vazio
// é a sessão do navegador (o celular), que não é um aparelho pareado.
type Actor struct {
	UserID   string
	DeviceID string
}

// Device devolve o aparelho como ponteiro, nulo quando não há.
func (a Actor) Device() *string {
	if a.DeviceID == "" {
		return nil
	}
	d := a.DeviceID
	return &d
}

// Status é o resultado de uma mutação do push (06-api-contract.md, "Sync",
// "Como fica na etapa 5").
type Status string

const (
	StatusApplied   Status = "applied"
	StatusDuplicate Status = "duplicate"
	StatusConflict  Status = "conflict"
	StatusRejected  Status = "rejected"
	StatusGone      Status = "gone"
)

// Códigos de recusa (rejected): a lista fechada da F2.
const (
	// CodeInvalidMutation: mutation_id ou entity_id que não é UUID, ou
	// entity_type grande demais. Esta recusa não é gravada (não há chave para
	// gravar), então um reenvio recebe rejected de novo, e não duplicate.
	CodeInvalidMutation = "mutacao_invalida"
	// CodeUnknownType: entity_type fora de transcription e learned.
	CodeUnknownType = "tipo_desconhecido"
	// CodeTypeNotSynced: learned numa nuvem anterior à F8. Desde a F8b (a
	// memória) e a F8c (o Postgres), nenhum Store deste módulo o devolve; ele
	// fica na lista porque o PC o trata (uma nuvem antiga).
	CodeTypeNotSynced = "tipo_ainda_nao_sincroniza"
	// CodeInvalidOperation: operation fora de upsert e delete, ou delete de
	// um ditado (a lixeira é o campo deleted_at do upsert, e apagar de vez é
	// só da nuvem). O delete só vale para o learned: é o desfazer (F8).
	CodeInvalidOperation = "operacao_invalida"
	// CodeUnknownField: campo fora da lista fechada do tipo (target_app,
	// áudio, contexto, ou qualquer outro).
	CodeUnknownField = "campo_desconhecido"
	// CodeInvalidField: campo da lista com tipo, valor ou formato errado, ou
	// campo obrigatório que falta.
	CodeInvalidField = "campo_invalido"
	// CodeFutureDate: created_at ou deleted_at além da folga para o futuro.
	CodeFutureDate = "data_no_futuro"
	// CodeInvalidBase: base_version negativa, ou maior que a versão atual (o
	// PC diz conhecer uma versão que o servidor nunca deu).
	CodeInvalidBase = "base_invalida"
	// CodeReusedMutation: o mesmo mutation_id já foi aplicado a OUTRA
	// entidade, ou mandado por OUTRO aparelho do usuário (cliente com
	// defeito, ou um historico.db copiado). Responder duplicate perderia a
	// mudança em silêncio (ADR-019, "Decisões da F1" e "Decisões da F2").
	// Desde a F8, também o mesmo mutation_id com OUTRA operação: o desfazer
	// em voo reenviado como reaprender perderia para o próprio desfazer.
	CodeReusedMutation = "mutacao_reusada"
	// CodeNothingToUndo: o delete (o desfazer) de um aprendido que a nuvem
	// não tem (F8). O PC o trata como "o desfazer está feito": o marcador
	// sai. Sem ele, o desfazer de um par que nunca subiu voltaria
	// base_invalida em toda rodada, para sempre.
	CodeNothingToUndo = "nada_a_desfazer"
)

// Motivos de gone.
const (
	// ReasonDeleted: a entidade foi apagada de vez e deixou lápide; o PC
	// apaga a cópia dele.
	ReasonDeleted = "apagado"
	// ReasonOutOfWindow: o ditado está fora da janela de 6 meses (ou já foi
	// apagado e a lápide venceu); o PC mantém a cópia e para de sincronizá-la.
	ReasonOutOfWindow = "fora_da_janela"
)

// Tipos e operações do contrato.
const (
	EntityTranscription = "transcription"
	EntityLearned       = "learned"

	OpUpsert    = "upsert"
	OpDelete    = "delete"
	OpTombstone = "tombstone"
)

// FutureTolerance é a folga para o futuro que a ADR-019 deixou para a F2
// fixar ("Retenção pelo created_at do PC"): um ditado (ou uma ida para a
// lixeira) com data mais de 24 h à frente do relógio do servidor é recusado.
// Um dia deixa passar o relógio de um PC um pouco adiantado sem perder o
// sync do ditado, e alonga a retenção na nuvem em no máximo um dia.
const FutureTolerance = 24 * time.Hour

// Limites.
const (
	DefaultPullLimit = 500
	MaxPullLimit     = 500
	// MaxPushMutations limita o lote do push por número. [ASSUMPTION] A F7c
	// fixa o lote do envio inicial (por número e por bytes); o corpo já tem o
	// teto de 1 MiB do servidor.
	MaxPushMutations = 500
	DefaultListLimit = 50
	MaxListLimit     = 100
	// MaxLearnedList é o teto fixo do GET /learned (o aprendido que o celular
	// lista; ADR-019, "Decisões da F8", escolha menor 3): os mais recentes
	// (last_seq), e LearnedPage.HasMore diz que passou dele. 500 é o teto do
	// lote do push e da página do pull. Confirmado pelo Mauro em 08-out-2026
	// ("Confirmo todas", na entrega da F8).
	MaxLearnedList = 500
	// MaxTextBytes é o teto de cada texto do ditado, em bytes: raw_text e
	// final_text (no push e no PATCH do CRUD) e, por consequência, o texto
	// do selo, que é um final_text que perdeu. [ASSUMPTION] Um ditado tem no
	// máximo 2 minutos de gravação (DefaultMaxRecording), uns 3 KiB de
	// texto; 64 KiB é folga de sobra para uma edição longa, e impede que um
	// cliente com defeito grave megabytes num item.
	MaxTextBytes = 64 << 10
	// PageTextBudget é o orçamento de texto de uma página do pull e da
	// listagem: assim que o texto acumulado (TextBytes) chega a ele, a página
	// para no item que chegou (sempre pelo menos um) e diz que há mais. Sem
	// isso, 500 itens perto do teto fariam uma resposta de dezenas de MiB,
	// montada inteira em memória. [ASSUMPTION] 2 MiB: uma página de ditados
	// comuns (500 de 1 a 3 KiB) cabe inteira.
	PageTextBudget = 2 << 20
	// maxEntityTypeLen: entity_type maior que isso nem é gravado (é lixo).
	maxEntityTypeLen = 32
)

// Mutation é uma mutação do push. Não tem user_id nem device_id: os dois vêm
// da credencial (o Actor).
type Mutation struct {
	MutationID  string
	EntityType  string
	EntityID    string
	Operation   string
	BaseVersion int64
	Payload     json.RawMessage
}

// Result é a resposta a UMA mutação do push.
type Result struct {
	// MutationID é o que o PC mandou, como mandou.
	MutationID string
	Status     Status
	// Original é o status da primeira aplicação; só no duplicate.
	Original Status
	// EntityVersion: a versão nova (applied), a versão atual (conflict), ou a
	// da primeira vez (duplicate).
	EntityVersion int64
	// Merged é o "mesclado": a base era velha e o servidor juntou a mudança
	// do PC com as que vieram depois dela. O PC NÃO guarda essa versão como a
	// que conhece; o estado mesclado chega no pull seguinte (ADR-019, "Eco e
	// mescla"). Só em applied (e no duplicate de um applied).
	Merged bool
	// Code é o código de rejected.
	Code string
	// Reason é o motivo de gone.
	Reason string
	// State é o estado atual do ditado, só no conflict de um ditado.
	State *Transcription
	// LearnedState é o estado atual do aprendido, só no conflict de um
	// aprendido (F8: vale o que chegou primeiro, e o desfazer vence). Nunca
	// junto com State.
	LearnedState *Learned
}

// Applied é o que fica gravado de uma mutação aplicada (applied_mutations),
// para um reenvio receber o mesmo resultado. Nada aqui carrega texto.
type Applied struct {
	EntityType string
	EntityID   string
	// DeviceID é o aparelho que mandou a mutação da primeira vez.
	DeviceID string
	// Operation é a operação da primeira vez, como RecordedOperation a
	// grava: upsert ou delete, e vazia quando a mandada não era da lista. O
	// reenvio com outra é mutacao_reusada (F8).
	Operation string
	Status    Status
	Version   int64
	Merged    bool
	// Code é o código de rejected ou o motivo de gone (result_code).
	Code string
}

// Change é uma linha do pull: o estado atual de uma entidade que mudou depois
// do cursor (Operation upsert) ou uma lápide (Operation tombstone).
type Change struct {
	Seq           int64
	EntityType    string
	EntityID      string
	Operation     string
	EntityVersion int64
	// DeviceID é o aparelho da última mudança (do sync_event), nulo quando
	// veio do celular. Serve para mostrar de onde veio; NÃO serve para
	// descartar o eco, que o PC reconhece pela versão.
	DeviceID *string
	// Item é o estado atual de um ditado, e Learned, o de um aprendido (F8:
	// sempre upsert; o par desfeito vem com UndoneAt, porque o aprendido não
	// tem lápide). Os dois nulos na lápide, e nunca os dois juntos.
	Item    *Transcription
	Learned *Learned
}

// TextBytes é o tamanho do texto ditado de um item: o bruto, o final e o do
// selo. É o que conta no orçamento de uma página (PageTextBudget).
func TextBytes(t Transcription) int {
	n := len(t.RawText) + len(t.FinalText)
	if t.Conflict != nil {
		n += len(t.Conflict.FinalText)
	}
	return n
}

// LearnedTextBytes é o tamanho do texto de um aprendido: a variante, o termo e
// o nome do domínio (até 80 runas cada).
func LearnedTextBytes(l Learned) int {
	n := len(l.Variant) + len(l.Term)
	if l.Domain != nil {
		n += len(*l.Domain)
	}
	return n
}

// TextBytes de uma mudança do pull: o do ditado ou o do aprendido, e zero na
// lápide.
func (c Change) TextBytes() int {
	switch {
	case c.Item != nil:
		return TextBytes(*c.Item)
	case c.Learned != nil:
		return LearnedTextBytes(*c.Learned)
	}
	return 0
}

// CutPage decide quantos itens de uma página ficam, entre os candidatos em
// ordem: no máximo limit e, assim que o texto acumulado chega a
// PageTextBudget, para no item que chegou (sempre pelo menos um). more diz se
// sobrou algum candidato depois do corte. O Store em memória a aplica à lista
// inteira; o do Postgres para de ler as linhas logo depois do item que chegou
// ao orçamento e a aplica ao que leu, com o mesmo resultado.
func CutPage[T any](items []T, limit int, size func(T) int) (n int, more bool) {
	used := 0
	for i, it := range items {
		if i == limit {
			return limit, true
		}
		used += size(it)
		if used >= PageTextBudget {
			return i + 1, i+1 < len(items)
		}
	}
	return len(items), false
}

// PushResponse é a resposta do push.
type PushResponse struct {
	Results []Result
	Epoch   string
}

// PullResponse é a resposta do pull.
type PullResponse struct {
	Changes []Change
	LastSeq int64
	HasMore bool
	Epoch   string
}

// ListFilter são os filtros da listagem (06-api-contract.md, "GET
// /transcriptions"). UserID vem sempre da sessão.
type ListFilter struct {
	UserID   string
	Query    string
	From, To *time.Time
	DeviceID *string
	Mode     *string
	Trash    bool
	Cursor   string
	Limit    int
}

// Page é uma página da listagem.
type Page struct {
	Items      []Transcription
	NextCursor string
}

// LearnedPage é o aprendido que o celular lista (GET /learned): só os não
// desfeitos, do mais recente (last_seq) para o mais antigo, até o teto.
type LearnedPage struct {
	// Items nunca é nil.
	Items []Learned
	// HasMore diz que há mais pares não desfeitos do que o teto deixou
	// passar.
	HasMore bool
}

// Erros do Store. Nenhum carrega texto ditado.
var (
	// ErrNotFound: o ditado não existe, ou é de outro usuário. Os dois casos
	// são o mesmo erro de propósito (AC-035, THR-004).
	ErrNotFound = errors.New("cloudstore: ditado não encontrado")
	// ErrLearnedNotFound: o aprendido não existe, é de outro usuário, ou o id
	// não é um UUID (UndoLearned). Os três casos são o mesmo erro de propósito,
	// e é outro erro que o do ditado: a resposta do celular tem a frase dele
	// (ADR-019, "Decisões da F8", as rotas do celular).
	ErrLearnedNotFound = errors.New("cloudstore: aprendido não encontrado")
	// ErrVersionConflict: o PATCH do CRUD veio com uma base_version que não é
	// a atual (409).
	ErrVersionConflict = errors.New("cloudstore: versão desatualizada")
	// ErrNotInTrash: "excluir definitivamente" de um ditado fora da lixeira.
	ErrNotInTrash = errors.New("cloudstore: o ditado não está na lixeira")
	// ErrNoConflict: escolher a versão de um ditado que não tem selo (409:
	// outra aba, ou outro aparelho, já escolheu).
	ErrNoConflict = errors.New("cloudstore: o ditado não tem versão em conflito")
	// ErrInvalidChoice: a escolha do selo não é "manter" nem "trocar".
	ErrInvalidChoice = errors.New("cloudstore: escolha inválida")
	// ErrInvalidText: texto com o caractere nulo, que o Postgres não guarda.
	ErrInvalidText = errors.New("cloudstore: texto com caractere inválido")
	// ErrTextTooLong: texto maior que MaxTextBytes.
	ErrTextTooLong = errors.New("cloudstore: texto grande demais")
	// ErrInvalidCursor: cursor de listagem malformado.
	ErrInvalidCursor = errors.New("cloudstore: cursor inválido")
	// ErrInvalidFilter: filtro de listagem malformado (device_id que não é
	// UUID, por exemplo).
	ErrInvalidFilter = errors.New("cloudstore: filtro inválido")
	// ErrUnknownUser: o usuário não tem a linha do contador de sync. Quem cria
	// o usuário cria o contador (F3); sem ele, nada é gravado.
	ErrUnknownUser = errors.New("cloudstore: usuário sem contador de sync")
	// ErrNoDevice: o push foi chamado sem aparelho.
	ErrNoDevice = errors.New("cloudstore: o push exige o aparelho")
	// ErrUnknownDevice: o aparelho não é do usuário (o Postgres recusa pela
	// chave estrangeira composta; o servidor em memória imita).
	ErrUnknownDevice = errors.New("cloudstore: aparelho desconhecido")

	// Os erros do Create (o ditado que nasce na nuvem; ADR-021, C1).
	//
	// ErrInvalidID: o id do ditado não é um UUID (Create e Situacao).
	ErrInvalidID = errors.New("cloudstore: id de ditado inválido")
	// ErrInvalidDictation: o ditado novo não passa na validação do payload de
	// criar (os mesmos campos, tamanhos e valores do push), ou a origem não é
	// uma das da nuvem. Nada foi gravado. A mensagem leva só o código de recusa,
	// de uma lista fechada, nunca o texto.
	ErrInvalidDictation = errors.New("cloudstore: ditado novo inválido")
	// ErrDeviceNotAllowed: o ditado da nuvem não nasce de um aparelho (device_id
	// fica nulo, também no app nativo do passo 2): o Create recusa um Actor com
	// aparelho, em vez de atribuir o ditado a ele em silêncio.
	ErrDeviceNotAllowed = errors.New("cloudstore: o ditado da nuvem não nasce de um aparelho")
	// ErrEpochChanged: o epoch esperado não é o da conta agora. Os dados dela
	// foram trocados (o "apagar meus dados" ou um backup restaurado) depois de
	// o epoch ser lido, e nada foi gravado. Um epoch vazio nunca confere.
	ErrEpochChanged = errors.New("cloudstore: os dados da conta foram trocados depois de o epoch ser lido")
	// ErrTombstoned: o id tem lápide (o ditado foi apagado de vez). O Create
	// não o recria.
	ErrTombstoned = errors.New("cloudstore: o ditado foi apagado de vez")
)

// EstadoDoId é o que a nuvem sabe de um id de ditado de um usuário.
type EstadoDoId int

const (
	// IdNada: não há ditado nem lápide com esse id na conta.
	IdNada EstadoDoId = iota
	// IdDitado: há o ditado, fora da lixeira ou nela.
	IdDitado
	// IdLapide: o ditado foi apagado de vez e a lápide ainda existe. Uma lápide
	// vencida que o job de retenção ainda não apagou também conta: a resposta
	// é a conservadora (o app nunca reusa um id, a não ser para reenviar).
	IdLapide
)

// SituacaoDoId é a resposta de Store.Situacao: uma leitura só de um id, e o
// epoch da conta lido junto, no mesmo instante.
type SituacaoDoId struct {
	Estado EstadoDoId
	// Ditado é o ditado, só em IdDitado.
	Ditado *Transcription
	// Epoch é o sync_epoch da conta no instante da leitura. É o epoch esperado
	// que o Create recebe: se a conta apagar os dados entre a leitura e a
	// gravação, o Create recusa.
	Epoch string
}

// Store é o que o servidor HTTP da etapa 5 usa para o histórico e o sync.
// Toda escrita passa pelo ponto único de gravação da implementação.
type Store interface {
	// List e Get só leem.
	List(ctx context.Context, f ListFilter) (Page, error)
	Get(ctx context.Context, userID, id string) (Transcription, error)

	// Situacao diz o que a nuvem sabe do id na conta do usuário: o ditado (também
	// o da lixeira), a lápide, ou nada. É o que o Get não mostra: ele não vê a
	// lápide. Só lê, não trava nada e não gasta seq. Devolve também o epoch da
	// conta (SituacaoDoId.Epoch). O id de outro usuário é IdNada, de propósito
	// (o mesmo id em duas contas são dois ditados); o que não é UUID é
	// ErrInvalidID, e o usuário sem contador, ErrUnknownUser.
	Situacao(ctx context.Context, userID, id string) (SituacaoDoId, error)
	// Create grava um ditado que nasceu na nuvem (o do celular; ADR-021), pelo
	// ponto único de gravação: a regra de criar do push (decideCreate) sobre o
	// payload validado como o do push, o evento de sync sem aparelho e o seq. O
	// Actor não tem aparelho (ErrDeviceNotAllowed). A data do ditado é now, o
	// relógio do servidor, e a origem é SourcePlatformWeb ou
	// SourcePlatformAndroid.
	//
	// epoch é o sync_epoch que quem chama leu antes (SituacaoDoId.Epoch). A
	// conferência é feita dentro da transação que trava o contador, então não
	// tem janela: se a conta trocou os dados desde então, ErrEpochChanged e nada
	// é gravado.
	//
	// O Create confere só o id, o usuário e o epoch, nunca o texto: se o id já
	// existe na conta (também na lixeira), devolve o que está gravado, com
	// created=false, sem gravar nem gastar seq. Numa corrida, o segundo recebe o
	// que o primeiro gravou. O id com lápide é ErrTombstoned.
	Create(ctx context.Context, a Actor, epoch, id string, n NewDictation, now time.Time) (t Transcription, created bool, err error)

	// Edit troca o texto final, só sobre a versão atual (ErrVersionConflict).
	Edit(ctx context.Context, a Actor, id string, baseVersion int64, finalText string, now time.Time) (Transcription, error)
	// ResolveConflict é a escolha do selo (EscolhaManter ou EscolhaTrocar),
	// só sobre a versão atual e só com selo (ErrNoConflict).
	ResolveConflict(ctx context.Context, a Actor, id string, baseVersion int64, escolha string, now time.Time) (Transcription, error)
	// Trash põe na lixeira; Restore tira. Já estar lá (ou fora) não é erro e
	// não grava nada.
	Trash(ctx context.Context, a Actor, id string, now time.Time) (Transcription, error)
	Restore(ctx context.Context, a Actor, id string, now time.Time) (Transcription, error)
	// Purge apaga de vez um ditado da lixeira e deixa a lápide e o evento
	// tombstone, quando a lápide não nasceria vencida (DeixaLapide); fora da
	// janela, só apaga, sem gastar seq (F9).
	Purge(ctx context.Context, a Actor, id string, now time.Time) error

	// ListLearned lista o aprendido do usuário que ainda vale (não desfeito),
	// do mais recente (last_seq) para o mais antigo, até limit pares; limit <=
	// 0 ou acima de MaxLearnedList é MaxLearnedList. HasMore diz que passou do
	// limite. Só lê.
	ListLearned(ctx context.Context, userID string, limit int) (LearnedPage, error)
	// UndoLearned desfaz um aprendido pelo ponto único de gravação (o
	// desfazer do celular), com o evento upsert e undone_at, como o delete do
	// PC (DecideLearned). Já estar desfeito não é erro e não grava nada nem
	// gasta seq (como a lixeira). O id que não é UUID, o que não existe e o de
	// outro usuário são ErrLearnedNotFound.
	UndoLearned(ctx context.Context, a Actor, id string, now time.Time) (Learned, error)

	// Push aplica as mutações do PC, cada uma na sua transação, na ordem: o
	// ditado por DecidePush, o aprendido por DecideLearned (F8).
	Push(ctx context.Context, a Actor, muts []Mutation, now time.Time) (PushResponse, error)
	// Pull lê o estado atual do que mudou depois de afterSeq (os ditados e o
	// aprendido) e as lápides, num snapshot só, fundidos por seq.
	Pull(ctx context.Context, userID string, afterSeq int64, limit int) (PullResponse, error)

	// Reter é a retenção de um usuário em now (ADR-019, "Decisões da F9";
	// retencao.go): apaga o ditado ativo com 6 meses (sem lápide nem evento),
	// a lixeira vencida pelo purge_at gravado (VencimentoDaLixeira, com o
	// piso da chegada; com a lápide e o evento tombstone, sem aparelho,
	// quando DeixaLapide) e as lápides vencidas (LapideVencida). Os ditados
	// candidatos são lidos sem trava, em lotes, escolhidos por DecideRetencao
	// e decididos DE NOVO sobre a linha travada, um por transação, pelo ponto
	// único de gravação: o restaurado ou mudado no meio fica, e o seq só é
	// gasto pela lápide nova. Não toca o aprendido (o desfeito fica enquanto
	// a conta existir) nem a marca d'água. Idempotente: a segunda rodada não
	// acha nada. Com o ctx cancelado, para entre um item e outro e devolve o
	// que já fez, com o erro. Um usuário sem contador é ErrUnknownUser.
	Reter(ctx context.Context, userID string, now time.Time) (Retido, error)
	// UsuariosDaRetencao lista os usuários que têm contador de sync (os que o
	// job percorre), em ordem.
	UsuariosDaRetencao(ctx context.Context) ([]string, error)
}
