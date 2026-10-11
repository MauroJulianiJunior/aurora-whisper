// Package dictionary gerencia termos, aliases e correções que alimentam
// o Resolver durante a resolução contextual do texto ditado, conforme
// docs/ai-spec/04-system-design.md.
//
// # Responsabilidade
//
// Armazenar e consultar entradas de dicionário (termos, aliases,
// correções aprendidas) usadas pelo internal/resolver na etapa de
// scoring fonético/contextual do modo Smart (TASK-008,
// docs/ai-spec/13-implementation-plan.md).
//
// # Contrato (docs/ai-spec/18-tool-contract.md)
//
//	type DictionaryRepository interface {
//	    Search(context.Context, DictionaryQuery) ([]DictionaryCandidate, error)
//	    Upsert(context.Context, DictionaryEntry) error
//	    UpsertAlias(context.Context, DictionaryAlias) error // extensão deste pacote: gerenciar DictionaryAlias também é parte do motor (05-data-model.md).
//	}
//
// InMemoryDictionaryRepository é a implementação de validação desta fase
// (Fase 1 / F1-C), sem I/O real — mesmo espírito de
// internal/storage.InMemoryTranscriptionRepository. A implementação real
// (SQLite local + Postgres cloud, Sprint 2+) entra atrás do mesmo
// contrato.
//
// # O dicionario.json e o aprendizado
//
// O dicionário pessoal mora num JSON (Semente, seed.go), e o que a pessoa
// ensina pelo painel é gravado nele com a origem "painel": Aprendizado
// (painel.go) é a interface que a API local chama; Arquivo (arquivo.go) lê,
// muda e grava o arquivo de forma atômica; aprender.go é o caminho do
// dictlearn. Ver docs/ai-spec/18-tool-contract.md, "Aprendizado".
//
// # O aprendido na nuvem (ADR-019, "Decisões da F8")
//
// Só o que o painel aprendeu sincroniza. par.go é o par como a nuvem o vê: o
// id (IDDoPar, um UUID v5 da ChaveDoPar), as pré-checagens que o servidor
// também faz (ConferirPar) e a projeção do arquivo no que sobe
// (Semente.ParesDoPainel). lote.go é o PC que recebe: as regras da ADR, na
// ordem, sobre a semente (Semente.AplicarPares) e sobre o arquivo, numa
// gravação por lote (Arquivo.AplicarPares). O formato do dicionario.json não
// muda. Este pacote só importa a biblioteca padrão: o servidor o importa.
//
// # Normalização e busca fonética
//
// Normalize (normalize.go) reduz um termo à forma canônica PT-BR usada
// como chave de unicidade e de busca. PhoneticEncoder (phonetic.go) é
// plugável, como pede TASK-008 ("phonetic key pluggable"); PTBRSoundex é
// a implementação default, com seus limites documentados no próprio tipo.
//
// # Regra de contenção (16-ai-behavior-spec.md, "Dictionary")
//
// O dicionário pode dar boost e sugerir, mas não pode autorizar a troca de
// uma palavra distante só porque o termo existe. Search reflete isso: um
// match fonético só vira candidato quando passa também no filtro de
// similaridade textual (phoneticSimilarityThreshold, repository_memory.go)
// — ver TestInMemoryDictionaryRepository_Search_DoesNotSuggestDistantWord.
package dictionary
