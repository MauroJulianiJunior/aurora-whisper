package dictionary

import (
	"context"
	"errors"
	"time"
)

// Erros sentinela devolvidos por este pacote. Chamadores comparam com
// errors.Is.
var (
	// ErrInvalidEntry é devolvido por Upsert quando a entrada não tem os
	// campos obrigatórios preenchidos (ID, UserID ou CanonicalForm).
	ErrInvalidEntry = errors.New("dictionary: entrada inválida")

	// ErrInvalidAlias é devolvido por UpsertAlias quando o alias não tem
	// os campos obrigatórios preenchidos (ID, EntryID ou Alias), ou
	// referencia uma entrada que não existe no repositório.
	ErrInvalidAlias = errors.New("dictionary: alias inválido")
)

// Source identifica a origem de um DictionaryAlias, conforme
// docs/ai-spec/05-data-model.md, tabela DictionaryAlias.
type Source string

const (
	// SourceManual é um alias cadastrado explicitamente pelo usuário.
	SourceManual Source = "manual"
	// SourceLearned é um alias derivado de uma correção confirmada pelo
	// usuário (ver docs/ai-spec/16-ai-behavior-spec.md, "Human-in-the-loop":
	// "aprender correção").
	SourceLearned Source = "learned"
	// SourceImported é um alias trazido de uma importação em lote (ex.:
	// glossário de domínio).
	SourceImported Source = "imported"
)

// DictionaryEntry é um termo canônico do dicionário pessoal do usuário
// (ex.: "DCTFWeb", "n8n", "Fisco"), conforme docs/ai-spec/05-data-model.md,
// seção DictionaryEntry.
//
// Unicidade de negócio: (UserID, NormalizedForm). Upsert é responsável por
// preservar essa unicidade (ver InMemoryDictionaryRepository.Upsert).
type DictionaryEntry struct {
	ID     string
	UserID string

	// CanonicalForm é a grafia exibida/inserida no texto final (ex.:
	// "DCTFWeb").
	CanonicalForm string

	// NormalizedForm é derivada de CanonicalForm por Normalize e nunca
	// deve ser definida manualmente pelo chamador: Upsert a recalcula
	// sempre, para garantir que a chave de unicidade reflita a forma
	// canônica atual.
	NormalizedForm string

	// Language e Domain são nil quando o termo vale para qualquer idioma
	// ou domínio (ex.: um domínio fiscal como "dominio-fiscal" ou um
	// idioma "pt-BR").
	Language *string
	Domain   *string

	// Priority desempata candidatos concorrentes na busca: maior
	// priority vence. Default 0.
	Priority int

	// Restricted marca uma entrada que só vale DENTRO do seu Domain.
	//
	// Existe para um caso específico e perigoso: alias que colidem com
	// palavra comum. "comite" -> "commit" é correção certa num editor de
	// código e ERRO num e-mail sobre o comitê; "DRA" -> "DRE" quebra
	// "Dra. Silva".
	//
	// A diferença para o filtro normal de Domain importa: uma entrada
	// comum com Domain "dev" ainda aparece quando a busca não informa
	// domínio (Search trata nil como "sem restrição", que é o certo para
	// LOOKUP). Uma entrada Restricted NÃO aparece — domínio desconhecido
	// é motivo para não arriscar, não para liberar.
	//
	// Por construção: quanto menos se sabe sobre onde a pessoa está
	// digitando, MENOS o dicionário mexe no texto.
	Restricted bool

	// Active controla se a entrada participa de Search. Entradas
	// inativas continuam persistidas (histórico/auditoria) mas nunca
	// geram candidato.
	Active bool

	// AliasOnly marca a entrada cujo TERMO o painel inventou ao aprender uma
	// variante (Origem "painel" no arquivo): ela age só pela variante que a
	// pessoa confirmou.
	//
	// Sem isto, criar o termo "esta" para aprender "ista" criaria, junto, uma
	// regra que nunca foi pedida: o match exato tolera acento e espaço, então
	// "a casa está pronta" viria a ser reescrito para "a casa esta pronta".
	// O que a pessoa aceitou foi a variante, não o termo.
	//
	// Efeito em Search: o termo ainda é ACHADO quando aparece escrito, mas o
	// candidato sai com DictionaryCandidate.AliasOnly (SoPresenca), e o Smart
	// o ignora por inteiro: não reescreve, não disputa trecho com outro termo
	// e não entra na lista de termos que trava o Polish (só entra quando o
	// Smart escreveu o termo no lugar da variante, neste ditado). A entrada
	// não gera casamento fonético. O alias segue valendo como qualquer outro.
	// A marca some quando a pessoa apaga o campo "origem" do arquivo: é assim
	// que um termo do painel vira termo dela.
	AliasOnly bool

	// Adicionado marca a entrada que a pessoa pôs pelo "Adicionar termo" das
	// Configurações (Origem "adicionado" no arquivo; ADR-020). É um termo de
	// verdade, com casamento exato e fonético, como qualquer outro: a marca
	// só decide o lugar dele no prompt do ASR. Com prioridade PrioridadeDica
	// ou mais, é uma dica da pessoa e entra no prompt antes de tudo; abaixo
	// disso, nunca entra (ver DisputaOPrompt). Vem da struct, e não de um
	// campo novo do JSON.
	Adicionado bool

	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64
}

// DictionaryAlias é uma variação textual que deve resolver para o
// DictionaryEntry apontado por EntryID (grafia alternativa, forma sem
// espaço, correção aprendida etc.), conforme docs/ai-spec/05-data-model.md,
// seção DictionaryAlias.
type DictionaryAlias struct {
	ID      string
	EntryID string

	Alias string

	// NormalizedAlias é derivada de Alias por Normalize; assim como em
	// DictionaryEntry, UpsertAlias sempre a recalcula.
	NormalizedAlias string

	// PhoneticKey é calculada por UpsertAlias com o PhoneticEncoder do
	// repositório. Fica persistida (como pede o data model, para permitir
	// índice em um backend real), mas Search nunca confia cegamente nela:
	// ela é recalculada a partir de NormalizedAlias sempre que o
	// PhoneticEncoder ativo muda, então nunca fica desatualizada dentro
	// do processo.
	PhoneticKey *string

	// Restricted marca este ALIAS como válido só dentro do Domain da
	// entrada.
	//
	// A restrição mora no alias, e não na entrada, porque o perigo é do
	// alias: "postgres" -> "PostgreSQL" precisa de domínio (postgres é
	// termo legítimo), mas "Postgre" -> "PostgreSQL" não precisa de nada
	// (não é palavra de idioma nenhum).
	//
	// Marcar a ENTRADA inteira como restrita trancava os dois juntos, e
	// foi o que aconteceu em 27-set-2026: proteger "postgres" tirou
	// "Postgre" de circulação e custou uma entidade na medição.
	Restricted bool

	Source     Source
	UsageCount int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DictionaryQuery descreve uma busca no dicionário pessoal, disparada pelo
// Resolver (modo Smart) durante o scoring de candidatos.
type DictionaryQuery struct {
	// UserID é obrigatório: toda busca é escopada ao usuário (mesma regra
	// de internal/storage, ver docs/ai-spec/09-security-privacy.md).
	UserID string

	// Term é o texto transcrito (palavra ou expressão curta) a resolver.
	// É DADO NÃO CONFIÁVEL (THR-008): Search nunca o interpreta como
	// instrução, só como string a comparar/normalizar.
	Term string

	// Language e Domain, quando não-nil, restringem a busca a entradas
	// universais (campo nil) ou que casam exatamente com o valor pedido.
	Language *string
	Domain   *string

	// Limit corta a lista de candidatos devolvida (já ordenada) ao
	// tamanho pedido. Limit <= 0 não limita.
	Limit int
}

// MatchReason identifica por que um DictionaryCandidate casou, para que o
// Resolver decida o quanto confiar nele (docs/ai-spec/16-ai-behavior-spec.md,
// "Dictionary": "pode boostar/sugerir, não pode substituir distantemente").
type MatchReason string

const (
	// MatchReasonExact: o termo buscado (normalizado, com ou sem espaços
	// internos) é idêntico à forma canônica da entrada.
	MatchReasonExact MatchReason = "exact"
	// MatchReasonAlias: o termo buscado casa com um alias cadastrado.
	MatchReasonAlias MatchReason = "alias"
	// MatchReasonPhonetic: o termo buscado casa por chave fonética com a
	// forma canônica ou um alias, e passou no filtro de contenção de
	// similaridade textual (ver Search).
	MatchReasonPhonetic MatchReason = "phonetic"
)

// DictionaryCandidate é um resultado de Search: um termo canônico que pode
// substituir/anotar o texto transcrito, com a razão do casamento e um
// score para desempate com outros sinais do Resolver.
//
// Score nunca deve ser apresentado como confidence do ASR (16-ai-behavior-spec.md,
// "Uncertainty": "não inventar score como se viesse do ASR") — é um sinal
// derivado localmente pelo dicionário, com procedência explícita em Reason.
type DictionaryCandidate struct {
	EntryID       string
	CanonicalForm string
	Reason        MatchReason
	Score         float64
	Priority      int
	// AliasOnly repete DictionaryEntry.AliasOnly. Num casamento Exact, quem
	// consome NÃO deve reescrever o texto para a forma canônica: o termo está
	// presente, e só isso. Em Alias o candidato vale como sempre.
	AliasOnly bool
}

// SoPresenca reporta se o candidato só diz que o termo ESTÁ escrito no texto:
// o casamento exato de uma entrada AliasOnly. Quem consome candidatos para
// trocar texto, ou para escolher entre trechos (o Smart), o ignora por
// inteiro: um termo que o painel inventou age só pela variante, e se o seu
// casamento exato contasse como o mais forte ele desligaria, em silêncio, o
// alias de frase de outro termo que o contém ("Cloud Code", com o termo
// "Code" do painel).
func (c DictionaryCandidate) SoPresenca() bool {
	return c.AliasOnly && c.Reason == MatchReasonExact
}

// DictionaryRepository é o contrato de persistência do dicionário pessoal,
// consumido pelo internal/resolver na etapa de scoring fonético/contextual
// do modo Smart (docs/ai-spec/18-tool-contract.md).
type DictionaryRepository interface {
	// Search devolve os candidatos do dicionário do usuário que casam com
	// query.Term, ordenados por Priority desc, depois Score desc, com
	// desempate determinístico (CanonicalForm asc, depois EntryID asc).
	// Entradas inativas nunca aparecem no resultado.
	Search(ctx context.Context, query DictionaryQuery) ([]DictionaryCandidate, error)

	// Upsert insere ou atualiza uma entrada, mantendo a unicidade
	// (UserID, NormalizedForm). entry.ID e entry.UserID são obrigatórios;
	// NormalizedForm é sempre recalculada a partir de CanonicalForm.
	//
	// Quando já existe uma entrada com o mesmo (UserID, NormalizedForm)
	// mas ID diferente, essa entrada existente é atualizada no lugar
	// (o ID original é preservado; entry.ID é ignorado nesse caso) —
	// esse é o comportamento de "upsert por chave de negócio" pedido
	// pelo contrato.
	Upsert(ctx context.Context, entry DictionaryEntry) error

	// UpsertAlias insere ou atualiza um alias, mantendo a unicidade
	// (EntryID, NormalizedAlias). Devolve ErrInvalidAlias se EntryID não
	// existir. alias.PhoneticKey é sempre recalculada.
	UpsertAlias(ctx context.Context, alias DictionaryAlias) error
}
