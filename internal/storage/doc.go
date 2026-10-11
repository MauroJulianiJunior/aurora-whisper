// Package storage abstrai a persistência local (SQLite, no Windows
// Agent) e serve de base para a persistência consolidada usada pelo
// backend cloud (PostgreSQL), conforme docs/ai-spec/04-system-design.md
// (seções "Local" e "Sync").
//
// F1-B (Fase 1, validação de contratos): migrations SQLite embutidas no
// binário (go:embed), um Migrator transacional e idempotente, e os
// contratos de repositório (TranscriptionRepository, SettingsRepository)
// com implementação em memória. A implementação real em SQLite dos
// repositórios fica para o Sprint 2 (ver docs/adr/ADR-008-sqlite-driver-puro-go.md,
// "Escopo nesta fase").
//
// # Responsabilidade
//
// No Agent, armazenar localmente: transcriptions, dictionary/corrections,
// settings, outbox de mutações pendentes e cursor de sync. Áudio e
// contexto de outras aplicações nunca são persistidos, por padrão nem
// exceção neste pacote — nenhuma tabela em internal/storage/migrations
// tem coluna de áudio ou de contexto de janela (AC-027, AC-028), e isso
// é verificado contra o SQL real das migrations em schema_test.go.
//
// # Contratos previstos para a Fase 1
//
// Este pacote não tem uma interface única listada em
// docs/ai-spec/18-tool-contract.md; ele fornece os repositórios
// concretos consumidos pelas interfaces de domínio de outros pacotes,
// por exemplo o DictionaryRepository usado por internal/dictionary e o
// armazenamento de outbox/cursor usado por internal/sync.
//
// Implementados: TranscriptionRepository e SettingsRepository, cada um
// em SQLite (produção: SQLiteTranscriptionRepository,
// SQLiteSettingsRepository) e em memória (testes), as duas passando pela
// mesma suíte de conformidade. Ainda não implementados:
// DictionaryRepository/DictionaryAliasRepository, CorrectionRepository e
// o armazenamento de sync_outbox/sync_state — as tabelas SQLite
// correspondentes já existem nas migrations, mas seus contratos Go
// chegam em blocos futuros.
//
// # Dono fixo
//
// O user_id de todas as linhas locais é o dono fixo da instalação,
// gravado na tabela instalacao (GarantirDonoFixo, LerDonoFixo — dono.go).
// A primeira subida sem ele copia o banco e migra as linhas antigas; ver
// docs/ai-spec/05-data-model.md, "Dono local".
package storage
