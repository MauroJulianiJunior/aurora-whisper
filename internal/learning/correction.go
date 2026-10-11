package learning

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrInvalidCorrection é devolvido por Upsert quando a Correction não tem
// os campos obrigatórios preenchidos (ID, UserID, FromText ou ToText).
var ErrInvalidCorrection = errors.New("learning: correction inválida")

// Correction é o espelho local de Correction
// (docs/ai-spec/05-data-model.md, seção Correction). TranscriptionID é
// nullable porque a política do data model é `ON DELETE SET NULL` quando
// a transcrição original é apagada.
type Correction struct {
	ID              string
	UserID          string
	TranscriptionID *string

	FromText string
	ToText   string

	// NormalizedFrom e NormalizedTo são derivadas de FromText/ToText por
	// normalizeText e nunca devem ser definidas manualmente pelo
	// chamador: Upsert as recalcula sempre, para garantir que a chave de
	// unicidade de negócio reflita o texto atual (mesmo padrão de
	// internal/dictionary.DictionaryEntry.NormalizedForm).
	NormalizedFrom string
	NormalizedTo   string

	Language *string

	// ConfirmedByUser só chega a true através de Confirm (AC-022) — este
	// pacote nunca grava uma Correction com ConfirmedByUser=false.
	ConfirmedByUser bool

	// UseCount conta quantas vezes o mesmo par (FromText, ToText) foi
	// confirmado por este usuário. Confirmar o mesmo par de novo
	// incrementa UseCount em vez de duplicar a Correction — é o
	// mecanismo de idempotência do fluxo (ver InMemoryCorrectionRepository.Upsert).
	UseCount int

	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64
}

// CorrectionRepository é o contrato de persistência local de
// corrections, consumido por Confirm. A unicidade de negócio é
// (UserID, NormalizedFrom, NormalizedTo): confirmar o mesmo par duas
// vezes deve atualizar a mesma linha (UseCount++), nunca duplicar.
type CorrectionRepository interface {
	// Upsert grava ou atualiza c, mantendo a unicidade de negócio acima.
	// c.CreatedAt/UpdatedAt já vêm preenchidos pelo chamador (Confirm),
	// a partir do Clock injetado — este contrato não chama time.Now().
	// Devolve a Correction efetivamente persistida (com ID/UseCount/Version
	// já resolvidos, que podem diferir do c recebido quando já existia
	// uma linha com a mesma chave de negócio).
	Upsert(ctx context.Context, c Correction) (Correction, error)
}

// keySep separa os componentes de uma chave de unicidade interna. Não é
// exposto: só usado no índice deste arquivo (mesmo padrão de
// internal/dictionary/repository_memory.go).
const keySep = "\x1f"

func correctionKey(userID, normalizedFrom, normalizedTo string) string {
	return userID + keySep + normalizedFrom + keySep + normalizedTo
}

// InMemoryCorrectionRepository é a implementação em memória de
// CorrectionRepository usada na Fase 1 para validar o contrato — mesma
// estratégia de internal/dictionary.InMemoryDictionaryRepository: mock
// determinístico e síncrono, sem I/O real. É segura para uso concorrente.
type InMemoryCorrectionRepository struct {
	mu sync.Mutex

	byID  map[string]Correction
	byKey map[string]string // correctionKey -> Correction.ID
}

// NewInMemoryCorrectionRepository constrói um repositório vazio.
func NewInMemoryCorrectionRepository() *InMemoryCorrectionRepository {
	return &InMemoryCorrectionRepository{
		byID:  make(map[string]Correction),
		byKey: make(map[string]string),
	}
}

var _ CorrectionRepository = (*InMemoryCorrectionRepository)(nil)

func (r *InMemoryCorrectionRepository) Upsert(_ context.Context, c Correction) (Correction, error) {
	if c.ID == "" || c.UserID == "" || strings.TrimSpace(c.FromText) == "" || strings.TrimSpace(c.ToText) == "" {
		return Correction{}, ErrInvalidCorrection
	}
	c.NormalizedFrom = normalizeText(c.FromText)
	c.NormalizedTo = normalizeText(c.ToText)
	if c.NormalizedFrom == "" || c.NormalizedTo == "" {
		return Correction{}, ErrInvalidCorrection
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := correctionKey(c.UserID, c.NormalizedFrom, c.NormalizedTo)

	// Já existe uma Correction com essa chave de negócio (UserID,
	// NormalizedFrom, NormalizedTo), possivelmente com outro ID: é ela
	// que recebe o incremento — c.ID pedido é ignorado nesse caso, e
	// UseCount/CreatedAt vêm da linha existente (IDEMPOTÊNCIA: confirmar
	// o mesmo par de novo nunca duplica).
	if existingID, ok := r.byKey[key]; ok {
		existing := r.byID[existingID]
		c.ID = existing.ID
		c.CreatedAt = existing.CreatedAt
		c.UseCount = existing.UseCount + 1
		c.Version = existing.Version + 1
		r.byID[existing.ID] = c
		return c, nil
	}

	c.UseCount = 1
	c.Version = 1
	if c.CreatedAt.IsZero() {
		c.CreatedAt = c.UpdatedAt
	}
	r.byID[c.ID] = c
	r.byKey[key] = c.ID
	return c, nil
}

// normalizeText reduz s a uma forma comparável para a chave de unicidade
// de negócio de Correction: espaços colapsados e bordas removidas.
// Diferente de internal/dictionary.Normalize (que também dobra acentos e
// minúsculas para busca fonética/difusa), aqui a comparação é só de
// espaçamento: uma correção é um par (de, para) exato escolhido pelo
// próprio usuário, então preservar maiúsculas/acentos evita fundir
// correções que ele deliberadamente diferenciou por grafia/caixa.
func normalizeText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
