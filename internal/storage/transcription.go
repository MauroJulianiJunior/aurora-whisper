package storage

import (
	"context"
	"errors"
	"time"
)

// Erros sentinela devolvidos pelos repositórios deste pacote. Chamadores
// comparam com errors.Is.
var (
	// ErrNotFound é devolvido quando a entidade pedida não existe (ou já
	// foi purgada) para o usuário informado.
	ErrNotFound = errors.New("storage: registro não encontrado")

	// ErrVersionConflict é devolvido por UpdateFinalText quando
	// base_version não bate com a versão atual — concorrência otimista
	// (ver docs/ai-spec/06-api-contract.md, "version").
	ErrVersionConflict = errors.New("storage: versão desatualizada (conflito de concorrência)")

	// ErrEscolhaInvalida: a escolha do selo não é EscolhaManter nem
	// EscolhaTrocar.
	ErrEscolhaInvalida = errors.New("storage: escolha do selo inválida")
)

// As escolhas do selo (ADR-019, "Decisões da F4" e da F7c), os mesmos nomes
// da nuvem (cloudstore.EscolhaManter e EscolhaTrocar).
const (
	// EscolhaManter fica com o texto atual; o do selo é descartado.
	EscolhaManter = "manter"
	// EscolhaTrocar fica com o texto do selo; o atual é descartado.
	EscolhaTrocar = "trocar"
)

// Transcription é o espelho local de Transcription (Postgres) mais os
// campos de sincronização, conforme docs/ai-spec/05-data-model.md,
// tabela local_transcriptions.
//
// Nunca deve carregar áudio nem contexto de outras janelas — isso é
// invariante de produto (AC-027, AC-028) e é verificado contra o schema
// SQL em schema_test.go.
type Transcription struct {
	ID             string
	UserID         string
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
	Confidence     *float64
	TargetApp      *string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
	PurgeAt        *time.Time
	Version        int64
	// SyncState é a situação do ditado diante da nuvem (ADR-019, F7a):
	// SituacaoNormal ("pending", o nome legado de "nada a dizer"),
	// SituacaoRecusado (a nuvem recusou a última mudança; o código fica em
	// LastSyncError) ou SituacaoForaDaJanela (a nuvem disse que o ditado está
	// fora dos 6 meses: o PC o mantém e para de sincronizá-lo).
	SyncState string
	// ServerVersion é a versão da nuvem que este PC conhece (nula: a nuvem
	// não o tem, ou o PC recomeçou). É a base do próximo envio e o que
	// reconhece o eco no pull.
	ServerVersion *int64
	LastSyncError *string
	// Conflito é o selo: a versão do texto que perdeu um conflito, como a
	// nuvem a guarda (ADR-019, "Conflito"). Nulo sem selo.
	Conflito *Conflito
	// VirouNota esconde o ditado do histórico sem lixeira (ADR-022, "Ditado vira
	// nota"; migration 0010): o id da nota em que ele virou ("Virar nota"), ou o
	// "nova:<token>" do ditado feito dentro de uma nota que ainda não foi criada
	// (PrefixoDaNotaNova). Só deste PC: não vai para o sync. Nulo, o ditado comum.
	VirouNota *string
}

// Conflito é a versão que perdeu um conflito de texto, guardada no próprio
// ditado até a pessoa escolher (a escolha no PC é da F7c).
type Conflito struct {
	FinalText string
	// DeviceID é o aparelho cuja versão perdeu; nulo quando veio do celular.
	DeviceID *string
	Em       time.Time
}

// As situações de um ditado diante da nuvem (Transcription.SyncState).
const (
	SituacaoNormal       = "pending"
	SituacaoRecusado     = "recusado"
	SituacaoForaDaJanela = "fora_da_janela"
)

// MaxTextoBytes é o teto de cada texto do ditado, o mesmo da nuvem
// (cloudstore.MaxTextBytes; ADR-019, "Decisões da F2"): o PC não aceita uma
// edição que a nuvem recusaria.
const MaxTextoBytes = 64 << 10

// TranscriptionListFilter descreve os critérios de busca do histórico
// (AC-013, docs/ai-spec/08-ui-ux-spec.md, "Histórico" → "Filtros").
type TranscriptionListFilter struct {
	// UserID é obrigatório: toda listagem é escopada ao usuário da
	// sessão (docs/ai-spec/09-security-privacy.md, "Integridade":
	// "user sempre deriva da sessão").
	UserID string

	// Query busca por substring, sem diferenciar maiúscula nem acento, em RawText e
	// FinalText. Vazio não filtra por texto.
	Query string

	// From/To filtram por CreatedAt (inclusive). Nil não limita aquele
	// lado do período.
	From *time.Time
	To   *time.Time

	// DeviceID, Mode e ASRProvider filtram por igualdade exata. Nil não
	// filtra por aquele campo.
	DeviceID    *string
	Mode        *string
	ASRProvider *string

	// Trash seleciona a lixeira (DeletedAt != nil) quando true, e o
	// histórico ativo (DeletedAt == nil) quando false.
	Trash bool

	// Cursor é o cursor opaco devolvido por uma chamada anterior de
	// List em TranscriptionPage.NextCursor. Vazio começa do início.
	Cursor string

	// Limit é o tamanho máximo da página. Valores <= 0 usam
	// DefaultTranscriptionListLimit; valores acima de
	// MaxTranscriptionListLimit são reduzidos a esse teto (mesmo teto
	// de 100 do endpoint cloud, ver docs/ai-spec/06-api-contract.md).
	Limit int
}

// Limites de paginação do histórico, compartilhados entre o contrato
// local e o endpoint cloud (docs/ai-spec/06-api-contract.md: "limit max
// 100").
const (
	DefaultTranscriptionListLimit = 50
	MaxTranscriptionListLimit     = 100
)

// TranscriptionPage é uma página de resultados de List. NextCursor é ""
// quando não há mais páginas.
type TranscriptionPage struct {
	Items      []Transcription
	NextCursor string
}

// TranscriptionRepository é o contrato de persistência do histórico de
// ditados, consumido pelo dashboard local (AC-011, AC-012, AC-013,
// AC-043) e, no Sprint 2, pela sincronização com o backend cloud.
//
// A ordenação de List é estável (CreatedAt desc no Histórico, DeletedAt
// desc na Lixeira, ID desc como desempate) e a paginação por cursor é
// real: o cursor codifica a posição exata do último item devolvido, então
// itens inseridos ou removidos entre chamadas não pulam nem repetem itens
// já vistos antes do ponto de corte.
type TranscriptionRepository interface {
	// List devolve uma página do histórico que casa com filter,
	// ordenada por CreatedAt desc — ou, com filter.Trash, por DeletedAt
	// desc: na Lixeira, o que foi apagado por último vem primeiro, por
	// mais antigo que seja o ditado (revisão de 29-set-2026).
	List(ctx context.Context, filter TranscriptionListFilter) (TranscriptionPage, error)

	// Get devolve a transcrição userID/id, incluindo itens na lixeira.
	// Devolve ErrNotFound se não existir ou pertencer a outro usuário.
	Get(ctx context.Context, userID, id string) (Transcription, error)

	// UpdateFinalText edita o texto final (AC-013: "editar"), com
	// concorrência otimista: baseVersion precisa bater com a versão
	// atual, senão devolve ErrVersionConflict. Em sucesso, Version é
	// incrementada e UpdatedAt avança.
	UpdateFinalText(ctx context.Context, userID, id string, baseVersion int64, finalText string) (Transcription, error)

	// SoftDelete move a transcrição para a lixeira: define DeletedAt =
	// now e PurgeAt = now + 30 dias (retenção de lixeira, ver
	// docs/ai-spec/05-data-model.md, "Retenção").
	//
	// Em item que JÁ está na lixeira não faz nada e não devolve erro:
	// DeletedAt, PurgeAt, UpdatedAt e Version ficam como estavam. Reapagar
	// (uma aba com a lista velha) não pode reiniciar o prazo da lixeira
	// (revisão de 29-set-2026).
	SoftDelete(ctx context.Context, userID, id string, now time.Time) error

	// ResolverConflito é a escolha do selo (ADR-019, F7c): EscolhaManter
	// fica com o texto atual, EscolhaTrocar põe o do selo como texto final;
	// as duas limpam o selo e sobem a versão. Só sobre a versão atual e só
	// num ditado com selo; senão, ErrVersionConflict (o painel relê). Uma
	// escolha fora das duas é ErrEscolhaInvalida.
	ResolverConflito(ctx context.Context, userID, id string, baseVersion int64, escolha string) (Transcription, error)

	// Restore tira a transcrição da lixeira antes da purge (AC-013 /
	// "Trash" → "restore"): limpa DeletedAt e PurgeAt.
	//
	// Em item que não está na lixeira não faz nada e não devolve erro,
	// pelo mesmo motivo do SoftDelete.
	Restore(ctx context.Context, userID, id string) error

	// PurgeExpired apaga definitivamente toda transcrição cujo PurgeAt
	// não seja nil e seja <= now, e devolve quantas foram apagadas.
	// Quem decide QUANDO PurgeAt é atingido (retenção de 30 dias da
	// lixeira) é a camada de domínio que grava PurgeAt; este método só
	// executa a purge já decidida.
	//
	// No SQLite, com a guarda da D3 (ADR-019, "Decisões da F10"): o que a
	// nuvem pode ter e ainda não soube fica, no máximo até os 6 meses do
	// ditado, e é contado em EsperandoANuvem. A versão em memória não tem
	// fila, e não tem a guarda (EsperandoANuvem é sempre zero).
	PurgeExpired(ctx context.Context, now time.Time) (ResultadoDoExpurgo, error)
}
