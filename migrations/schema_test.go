// Package migrations_test verifica estaticamente o SQL das migrations
// PostgreSQL contra docs/ai-spec/05-data-model.md.
//
// Este arquivo é a verificação ESTÁTICA: roda sempre, em qualquer máquina,
// sem Postgres. O que ele prova é leitura textual — que cada entidade e
// cada coluna que a especificação lista tem uma tabela/coluna
// correspondente no arquivo certo, que as constraints exigidas (UNIQUE,
// PRIMARY KEY por usuário, ON DELETE SET NULL) estão presentes no texto, e
// que nenhuma coluna de áudio/contexto (nem target_app) foi introduzida.
// Ele NÃO prova que o SQL é válido para o PostgreSQL. Isso é o que
// postgres_test.go faz: aplica as migrations num banco descartável de um
// Postgres de verdade, mas só quando AURORA_TEST_POSTGRES_URL está
// definida (ver migrations/README.md, "Como rodar os testes contra um
// Postgres").
package migrations_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tableSpec descreve o que docs/ai-spec/05-data-model.md (seção
// "PostgreSQL") exige de uma tabela: o arquivo de migration que deve
// contê-la, o nome da tabela e a lista exata de colunas documentadas
// para aquela entidade.
type tableSpec struct {
	file    string
	table   string
	columns []string
}

// wantTables espelha, entidade por entidade, a lista de
// docs/ai-spec/05-data-model.md ("PostgreSQL"): User, Device,
// Transcription, DictionaryEntry, DictionaryAlias, Correction,
// UserSettings, SyncEvent, AppliedMutation (0001 a 0009, corrigidas no lugar
// pela F1) e as sete tabelas novas da etapa 5 (0010 a 0016): UserSyncCounter,
// WebSession, LoginPendente, PedidoDePareamento, Token do aparelho, Lapide e
// Aprendido ("Etapa 5: o que entra no Postgres").
var wantTables = []tableSpec{
	{
		file:  "0001_users_table.sql",
		table: "users",
		columns: []string{
			"id", "google_sub", "email", "display_name", "avatar_url",
			"created_at", "updated_at", "disabled_at",
		},
	},
	{
		file:  "0002_devices_table.sql",
		table: "devices",
		columns: []string{
			"id", "user_id", "install_id", "name", "platform", "app_version",
			"last_seen_at", "created_at", "revoked_at", "version",
		},
	},
	{
		file:  "0003_transcriptions_table.sql",
		table: "transcriptions",
		columns: []string{
			"id", "user_id", "device_id", "source_platform", "raw_text",
			"final_text", "mode", "language", "asr_provider", "asr_model",
			"duration_ms", "processing_ms", "confidence",
			"status", "created_at", "updated_at", "deleted_at", "purge_at",
			"version",
			// ADR-019: o pull pelo estado atual e o selo de conflito.
			"last_seq", "conflict_final_text", "conflict_device_id",
			"conflict_at",
			// ADR-019, F2: o que mudou depois da base do PC (a mescla).
			"text_version", "trash_version",
			// ADR-019, F2 (revisão): quem mudou cada campo por último.
			"text_device_id", "trash_device_id",
		},
	},
	{
		file:  "0004_dictionary_entries_table.sql",
		table: "dictionary_entries",
		columns: []string{
			"id", "user_id", "canonical_form", "normalized_form", "language",
			"domain", "priority", "active", "created_at", "updated_at",
			"version",
		},
	},
	{
		file:  "0005_dictionary_aliases_table.sql",
		table: "dictionary_aliases",
		columns: []string{
			"id", "entry_id", "alias", "normalized_alias", "phonetic_key",
			"source", "usage_count", "created_at", "updated_at",
		},
	},
	{
		file:  "0006_corrections_table.sql",
		table: "corrections",
		columns: []string{
			"id", "user_id", "transcription_id", "from_text", "to_text",
			"normalized_from", "normalized_to", "language",
			"confirmed_by_user", "use_count", "created_at", "updated_at",
			"version",
		},
	},
	{
		file:  "0007_user_settings_table.sql",
		table: "user_settings",
		columns: []string{
			"user_id", "settings", "updated_at", "version",
		},
	},
	{
		file:  "0008_sync_events_table.sql",
		table: "sync_events",
		columns: []string{
			"seq", "user_id", "entity_type", "entity_id", "operation",
			"entity_version", "changed_at",
			// ADR-019: o aparelho de origem da mudança.
			"device_id",
		},
	},
	{
		file:  "0009_applied_mutations_table.sql",
		table: "applied_mutations",
		columns: []string{
			"mutation_id", "user_id", "device_id", "applied_at",
			"result_version",
			// ADR-019: o resultado da primeira aplicação, para o
			// reenvio receber "duplicate" com a mesma resposta.
			"status", "merged", "result_code",
			// A entidade da mutação: um mutation_id reusado para outra
			// entidade não pode virar "duplicate" em silêncio.
			"entity_type", "entity_id",
			// ADR-019, F8 (corrigida no lugar pela F8c): a operação da
			// primeira vez; o reenvio com outra é mutacao_reusada.
			"operation",
		},
	},
	// Tabelas novas da etapa 5 (ADR-019, F1): docs/ai-spec/05-data-model.md,
	// "Etapa 5: o que entra no Postgres".
	{
		file:  "0010_user_sync_counters_table.sql",
		table: "user_sync_counters",
		columns: []string{
			"user_id", "last_seq", "sync_epoch", "tombstone_watermark",
		},
	},
	{
		file:  "0011_web_sessions_table.sql",
		table: "web_sessions",
		columns: []string{
			"id", "user_id", "token_hmac", "label", "created_at",
			"last_seen_at", "expires_at", "revoked_at",
		},
	},
	{
		file:  "0012_pending_logins_table.sql",
		table: "pending_logins",
		columns: []string{
			"state_hash", "pre_hash", "code_verifier", "nonce", "created_at",
			"expires_at", "used_at",
			// Para onde o callback leva a pessoa depois do login (um caminho
			// da própria origem, nunca uma URL).
			"return_to",
		},
	},
	{
		file:  "0013_pairing_requests_table.sql",
		table: "pairing_requests",
		columns: []string{
			"id", "code_challenge", "install_id", "device_name", "platform",
			"app_version", "redirect_port", "confirmation_code", "created_at",
			"expires_at", "approved_by", "approved_at", "code_hash", "used_at",
		},
	},
	{
		file:  "0014_device_tokens_table.sql",
		table: "device_tokens",
		columns: []string{
			"token_hmac", "device_id", "created_at", "last_used_at",
		},
	},
	{
		file:  "0015_tombstones_table.sql",
		table: "tombstones",
		columns: []string{
			"user_id", "entity_type", "entity_id", "reason", "dictated_at",
			"seq",
		},
	},
	{
		file:  "0016_learned_pairs_table.sql",
		table: "learned_pairs",
		columns: []string{
			"id", "user_id", "variant", "normalized_variant", "term",
			"restricted", "domain", "created_at", "updated_at", "undone_at",
			"version", "last_seq",
		},
	},
	// ADR-021 (o ditado no celular), C3a: a chave da IA de cada pessoa, só
	// cifrada (docs/ai-spec/05-data-model.md, "As chaves da IA").
	{
		file:  "0017_ai_provider_keys_table.sql",
		table: "ai_provider_keys",
		columns: []string{
			"user_id", "provider", "cipher_key_id", "nonce", "ciphertext",
			"key_suffix", "created_at", "updated_at",
		},
	},
}

// TestSchema_EveryMigrationIsCoveredBySpec garante que a lista acima e os
// arquivos *.sql deste diretório são o mesmo conjunto: migration nova sem
// entrada na lista (e sem a leitura contra o 05) não passa.
func TestSchema_EveryMigrationIsCoveredBySpec(t *testing.T) {
	want := make(map[string]bool, len(wantTables))
	for _, spec := range wantTables {
		want[spec.file] = true
	}
	for _, file := range allMigrationFiles(t) {
		if !want[file] {
			t.Errorf("%s: migration sem entrada em wantTables (adicione a tabela e as colunas do 05-data-model.md)", file)
		}
		delete(want, file)
	}
	for file := range want {
		t.Errorf("%s: está em wantTables mas não existe em migrations/", file)
	}
}

// TestSchema_OneTablePerFile prova a convenção "um arquivo, uma tabela" que
// o README e o leitor de migrations assumem, e o nome do arquivo
// "NNNN_<tabela>_table.sql".
func TestSchema_OneTablePerFile(t *testing.T) {
	createTable := regexp.MustCompile(`(?im)^\s*CREATE TABLE\s+([a-z_][a-z0-9_]*)`)
	for _, file := range allMigrationFiles(t) {
		found := createTable.FindAllStringSubmatch(stripSQLComments(readMigration(t, file)), -1)
		if len(found) != 1 {
			t.Errorf("%s: esperava exatamente um CREATE TABLE, achei %d", file, len(found))
			continue
		}
		if !strings.HasSuffix(file, "_"+found[0][1]+"_table.sql") {
			t.Errorf("%s: o nome do arquivo deveria terminar em _%s_table.sql", file, found[0][1])
		}
	}
}

// TestSchema_AllEntitiesAndColumnsFromDataModelExist cruza cada
// entidade e cada campo de docs/ai-spec/05-data-model.md ("PostgreSQL")
// com o SQL real das migrations. Pega omissão de tabela e erro/omissão
// de nome de coluna, que são os defeitos mais comuns numa tradução
// manual de spec para schema.
func TestSchema_AllEntitiesAndColumnsFromDataModelExist(t *testing.T) {
	for _, spec := range wantTables {
		spec := spec
		t.Run(spec.table, func(t *testing.T) {
			content := readMigration(t, spec.file)
			block := extractCreateTableBlock(t, content, spec.table)
			for _, col := range spec.columns {
				if !columnDefined(block, col) {
					t.Errorf("%s: coluna %q (exigida por docs/ai-spec/05-data-model.md) não encontrada na definição de CREATE TABLE %s", spec.file, col, spec.table)
				}
			}
		})
	}
}

// TestSchema_UniqueConstraints prova textualmente as constraints UNIQUE
// que a tarefa e docs/ai-spec/05-data-model.md exigem.
func TestSchema_UniqueConstraints(t *testing.T) {
	cases := []struct {
		file   string
		unique string
	}{
		{"0002_devices_table.sql", "UNIQUE (user_id, install_id)"},
		// Alvo das chaves estrangeiras compostas (user_id, device_id): o
		// aparelho de um ditado, de um evento ou de uma mutação tem de ser
		// do mesmo usuário.
		{"0002_devices_table.sql", "UNIQUE (user_id, id)"},
		// Cada mudança toma um seq novo do contador do usuário e grava uma
		// entidade só: o last_seq da linha é único por usuário.
		{"0003_transcriptions_table.sql", "UNIQUE (user_id, last_seq)"},
		// O id do ditado vem do cliente: a PK é por usuário (ver também
		// TestSchema_TranscriptionPKIsPerUser).
		{"0003_transcriptions_table.sql", "PRIMARY KEY (user_id, id)"},
		{"0004_dictionary_entries_table.sql", "UNIQUE (user_id, normalized_form)"},
		{"0005_dictionary_aliases_table.sql", "UNIQUE (entry_id, normalized_alias)"},
		// A lápide é uma por entidade (a PK) e tem o seq único do usuário,
		// como o last_seq das entidades.
		{"0015_tombstones_table.sql", "PRIMARY KEY (user_id, entity_type, entity_id)"},
		{"0015_tombstones_table.sql", "UNIQUE (user_id, seq)"},
		// O aprendido tem o id derivado da variante: o mesmo em todo PC e
		// em todo usuário, então a PK é por usuário.
		{"0016_learned_pairs_table.sql", "PRIMARY KEY (user_id, id)"},
		{"0016_learned_pairs_table.sql", "UNIQUE (user_id, normalized_variant)"},
		{"0016_learned_pairs_table.sql", "UNIQUE (user_id, last_seq)"},
		// Uma chave por provedor e por pessoa (ADR-021, C3a): trocar é gravar
		// por cima.
		{"0017_ai_provider_keys_table.sql", "PRIMARY KEY (user_id, provider)"},
	}
	for _, c := range cases {
		// Sem os comentários: eles explicam as constraints, e citam o texto delas.
		content := stripSQLComments(readMigration(t, c.file))
		if !strings.Contains(content, c.unique) {
			t.Errorf("%s: esperava a constraint %q, não encontrada", c.file, c.unique)
		}
	}
}

// TestSchema_CorrectionTranscriptionIDSetNull prova que
// corrections.transcription_id referencia transcriptions com
// ON DELETE SET NULL, conforme exigido pela tarefa e por
// docs/ai-spec/05-data-model.md ("Integridade"). A PK de transcriptions é
// (user_id, id), então a chave é composta, e o SET NULL leva a lista de
// colunas: sem ela, o Postgres tentaria zerar o user_id (NOT NULL) e o DELETE
// do ditado falharia.
func TestSchema_CorrectionTranscriptionIDSetNull(t *testing.T) {
	block := extractCreateTableBlock(t, readMigration(t, "0006_corrections_table.sql"), "corrections")

	want := "FOREIGN KEY (user_id, transcription_id) REFERENCES transcriptions (user_id, id) ON DELETE SET NULL (transcription_id)"
	if !strings.Contains(squashSQL(block), want) {
		t.Errorf("0006_corrections_table.sql: esperava %q, não encontrado", want)
	}
	// A chave simples (id) não serve mais, e uma coluna com REFERENCES na
	// própria linha seria a forma antiga.
	simple := regexp.MustCompile(`(?im)^\s*transcription_id\s+UUID\b.*\bREFERENCES\b`)
	if simple.MatchString(block) {
		t.Error("0006_corrections_table.sql: transcription_id referencia transcriptions pela chave simples; a PK é (user_id, id)")
	}
}

// TestSchema_TranscriptionPKIsPerUser prova que a PK de transcriptions é
// (user_id, id): o id do ditado vem do cliente (UUID v7 do PC), e um espaço de
// ids global deixaria o push de uma conta colidir com o de outra e revelar que
// o id existe (ADR-019, "Decisões da F1").
func TestSchema_TranscriptionPKIsPerUser(t *testing.T) {
	block := extractCreateTableBlock(t, readMigration(t, "0003_transcriptions_table.sql"), "transcriptions")

	if !strings.Contains(squashSQL(block), "PRIMARY KEY (user_id, id)") {
		t.Error("0003: esperava PRIMARY KEY (user_id, id), não encontrado")
	}
	global := regexp.MustCompile(`(?im)^\s*id\s+UUID\b.*\bPRIMARY KEY\b`)
	if global.MatchString(block) {
		t.Error("0003: id voltou a ser PRIMARY KEY sozinho (global); a PK é por usuário")
	}
}

// TestSchema_AppliedMutationPKIsPerUser prova que a PRIMARY KEY de
// applied_mutations é (user_id, mutation_id), por usuário — é essa PK que
// faz uma mutação reenviada colidir em vez de virar um segundo registro
// (meta dura de F1-D: "Sync confirmado perdido: 0"), sem que o id de uma
// pessoa colida com o de outra (ADR-019).
func TestSchema_AppliedMutationPKIsPerUser(t *testing.T) {
	content := readMigration(t, "0009_applied_mutations_table.sql")
	block := extractCreateTableBlock(t, content, "applied_mutations")

	perUser := regexp.MustCompile(`(?im)^\s*PRIMARY KEY\s*\(\s*user_id\s*,\s*mutation_id\s*\)\s*,?\s*$`)
	if !perUser.MatchString(block) {
		t.Error("0009_applied_mutations_table.sql: esperava PRIMARY KEY (user_id, mutation_id), não encontrado")
	}

	// A PK global antiga: mutation_id sozinho como chave.
	global := regexp.MustCompile(`(?im)^\s*mutation_id\s+UUID\b.*\bPRIMARY KEY\b`)
	if global.MatchString(block) {
		t.Error("0009_applied_mutations_table.sql: mutation_id voltou a ser PRIMARY KEY sozinho (global); a PK é por usuário")
	}
}

// TestSchema_AppliedMutationOperation prova, no texto da 0009, a coluna da
// operação (ADR-019, "Decisões da F8"; corrigida no lugar pela F8c): a lista
// fechada é exatamente upsert e delete (o tombstone é só da nuvem, nunca de
// uma mutação); a coluna é nula, para a recusa operacao_invalida de uma
// operação fora da lista ser gravada sem guardar o texto que o cliente
// mandou; e não tem DEFAULT, para um INSERT que esquecesse a coluna não
// gravar "upsert" no lugar de um delete (o reenvio do desfazer viraria
// mutacao_reusada).
func TestSchema_AppliedMutationOperation(t *testing.T) {
	block := extractCreateTableBlock(t, readMigration(t, "0009_applied_mutations_table.sql"), "applied_mutations")
	line := regexp.MustCompile(`(?im)^\s*operation\s+.*$`).FindString(stripSQLComments(block))
	if line == "" {
		t.Fatal("0009: a coluna operation não está no CREATE TABLE applied_mutations")
	}
	got := strings.Join(strings.Fields(line), " ")
	if want := "operation TEXT CHECK (operation IN ('upsert', 'delete')),"; got != want {
		t.Errorf("0009: a linha da coluna operation é %q, quero %q (nula, sem DEFAULT, só upsert e delete)", got, want)
	}
}

// TestSchema_SyncEventSeqIsPerUser prova que sync_events.seq é por usuário:
// a PK é (user_id, seq), sem sequence global. O BIGSERIAL da versão antiga
// deixa um número menor ficar visível depois de um maior sob transações
// concorrentes, e o cursor do pull pularia o evento (ADR-019).
func TestSchema_SyncEventSeqIsPerUser(t *testing.T) {
	content := readMigration(t, "0008_sync_events_table.sql")
	block := extractCreateTableBlock(t, content, "sync_events")

	perUser := regexp.MustCompile(`(?im)^\s*PRIMARY KEY\s*\(\s*user_id\s*,\s*seq\s*\)\s*,?\s*$`)
	if !perUser.MatchString(block) {
		t.Error("0008_sync_events_table.sql: esperava PRIMARY KEY (user_id, seq), não encontrado")
	}

	seqLine := regexp.MustCompile(`(?im)^\s*seq\s+BIGINT\s+NOT NULL\b`)
	if !seqLine.MatchString(block) {
		t.Error("0008_sync_events_table.sql: esperava seq BIGINT NOT NULL (o número vem do contador do usuário, não de uma sequence)")
	}
}

// TestSchema_NoGlobalSequencesNorDatabaseIDs garante que nenhuma migration
// usa sequence global (SERIAL, IDENTITY, nextval) nem gera ID no banco
// (gen_random_uuid, uuid_generate_*): os IDs são UUID v7 gerados pela
// aplicação (05-data-model.md, "IDs") e o seq é o do contador do usuário.
// Comentários são ignorados: eles explicam por que o BIGSERIAL saiu.
func TestSchema_NoGlobalSequencesNorDatabaseIDs(t *testing.T) {
	banned := regexp.MustCompile(`(?i)\b(big|small)?serial\b|\bgenerated\b.*\bas identity\b|\bnextval\b|\bgen_random_uuid\b|\buuid_generate_v\d\w*`)
	for _, file := range allMigrationFiles(t) {
		sql := stripSQLComments(readMigration(t, file))
		if m := banned.FindString(sql); m != "" {
			t.Errorf("%s: %q não é permitido (sem sequence global; o id é UUID v7 gerado pela aplicação)", file, m)
		}
	}
}

// TestSchema_TargetAppIsGone prova que target_app não existe em nenhuma
// migration: é o nome do app em foco, contexto de outra aplicação
// (REQ-025), que o Agent nunca grava e a nuvem recusa (ADR-019, decisão
// 10). Comentários são ignorados, porque explicam a ausência.
func TestSchema_TargetAppIsGone(t *testing.T) {
	for _, file := range allMigrationFiles(t) {
		sql := strings.ToLower(stripSQLComments(readMigration(t, file)))
		if strings.Contains(sql, "target_app") {
			t.Errorf("%s: target_app voltou ao schema (contexto de outra aplicação não vai para a nuvem)", file)
		}
	}
}

// TestSchema_TranscriptionIndexes prova os três índices de
// Transcription exigidos pela tarefa e por
// docs/ai-spec/05-data-model.md ("Índices"): ativo (created_at desc),
// lixeira (deleted_at desc) e sincronização incremental (updated_at).
func TestSchema_TranscriptionIndexes(t *testing.T) {
	content := stripSQLComments(readMigration(t, "0003_transcriptions_table.sql"))

	wantFragments := []string{
		"CREATE INDEX idx_transcriptions_active",
		"ON transcriptions (user_id, created_at DESC)",
		"WHERE deleted_at IS NULL",
		"CREATE INDEX idx_transcriptions_trash",
		"ON transcriptions (user_id, deleted_at DESC)",
		"WHERE deleted_at IS NOT NULL",
		"CREATE INDEX idx_transcriptions_updated_at",
		"ON transcriptions (user_id, updated_at)",
	}
	for _, frag := range wantFragments {
		if !strings.Contains(content, frag) {
			t.Errorf("0003_transcriptions_table.sql: esperava conter %q, não encontrado", frag)
		}
	}
}

// TestSchema_RetentionIndexes prova os índices da arrumação do job de
// retenção (ADR-019, "Decisões da F9"): o do sync_event órfão por usuário e
// idade (a 0008, corrigida no lugar pela F9c), o da resposta guardada por
// idade (0009) e o pré-filtro das lápides pela data do ditado (0015).
func TestSchema_RetentionIndexes(t *testing.T) {
	for file, frags := range map[string][]string{
		"0008_sync_events_table.sql":       {"CREATE INDEX idx_sync_events_changed_at ON sync_events (user_id, changed_at);"},
		"0009_applied_mutations_table.sql": {"CREATE INDEX idx_applied_mutations_applied_at ON applied_mutations (applied_at);"},
		"0015_tombstones_table.sql":        {"CREATE INDEX idx_tombstones_dictated_at ON tombstones (dictated_at);"},
	} {
		content := stripSQLComments(readMigration(t, file))
		for _, frag := range frags {
			if !strings.Contains(content, frag) {
				t.Errorf("%s: esperava conter %q, não encontrado", file, frag)
			}
		}
	}
	// A 0008 diz no texto o que a arrumação poupa: o evento que é o last_seq
	// de uma entidade viva.
	if c := readMigration(t, "0008_sync_events_table.sql"); !strings.Contains(c, "last_seq de uma entidade viva fica") {
		t.Error("0008_sync_events_table.sql: o comentário não diz mais que o evento do last_seq de uma entidade viva fica")
	}
	// O 410 da marca d'água saiu (D2): nenhum comentário o promete.
	for _, file := range []string{"0008_sync_events_table.sql", "0010_user_sync_counters_table.sql", "0015_tombstones_table.sql"} {
		c := readMigration(t, file)
		if strings.Contains(c, "responde 410") || strings.Contains(c, "sobe a marca d'água de user_sync_counters") {
			t.Errorf("%s: o comentário ainda promete o 410 ou o job subindo a marca d'água", file)
		}
	}
}

// TestSchema_OptimisticConcurrencyVersionColumn prova que a coluna
// version (bigint) existe exatamente nas entidades que
// docs/ai-spec/05-data-model.md documenta com version — Device,
// Transcription, DictionaryEntry, Correction, UserSettings, e o
// aprendido da ADR-019 — e não nas demais (User, DictionaryAlias,
// SyncEvent, AppliedMutation e as tabelas de estado da etapa 5, que a
// spec não lista com version).
func TestSchema_OptimisticConcurrencyVersionColumn(t *testing.T) {
	wantVersion := map[string]bool{
		"0001_users_table.sql":              false,
		"0002_devices_table.sql":            true,
		"0003_transcriptions_table.sql":     true,
		"0004_dictionary_entries_table.sql": true,
		"0005_dictionary_aliases_table.sql": false,
		"0006_corrections_table.sql":        true,
		"0007_user_settings_table.sql":      true,
		"0008_sync_events_table.sql":        false,
		"0009_applied_mutations_table.sql":  false,
		// Etapa 5 (ADR-019): só o aprendido é uma entidade editável com
		// concorrência otimista. As demais são de estado (contador, sessão,
		// login, pareamento, token) ou imutáveis (lápide).
		"0010_user_sync_counters_table.sql": false,
		"0011_web_sessions_table.sql":       false,
		"0012_pending_logins_table.sql":     false,
		"0013_pairing_requests_table.sql":   false,
		"0014_device_tokens_table.sql":      false,
		"0015_tombstones_table.sql":         false,
		"0016_learned_pairs_table.sql":      true,
		// ADR-021, C3a: a chave cifrada é trocada inteira (gravar por cima),
		// sem concorrência otimista.
		"0017_ai_provider_keys_table.sql": false,
	}

	re := regexp.MustCompile(`(?im)^\s*version\s+BIGINT`)
	for file, want := range wantVersion {
		content := readMigration(t, file)
		got := re.MatchString(content)
		if got != want {
			t.Errorf("%s: coluna version presente=%v, esperado=%v (ver docs/ai-spec/05-data-model.md)", file, got, want)
		}
	}
}

// forbiddenColumnPatterns replica, para o schema PostgreSQL, a mesma
// lista já auditada para o SQLite local em
// internal/storage/schema_test.go: nomes de coluna que indicariam
// áudio ou contexto de janela/seleção/clipboard vazando para
// persistência (AC-027, AC-028, docs/ai-spec/09-security-privacy.md
// "Logging proibido").
var forbiddenColumnPatterns = []string{
	"audio", "pcm", "wav", "buffer", "samples", "frames",
	"context", "nearby", "selection", "window_text", "clipboard",
	// ADR-019, decisão 10: o nome do app em foco é contexto (REQ-025) e
	// não vai para a nuvem.
	"target_app",
}

// TestSchema_NoAudioOrContextColumns varre o SQL real de todas as
// migrations e falha se qualquer definição de coluna contiver um nome
// proibido. Nenhuma exceção foi necessária: ao contrário do
// local_settings do SQLite (que tem local_context_enabled, uma
// flag), o schema Postgres desta batch não tem nenhuma coluna cujo
// nome colida com os padrões abaixo.
func TestSchema_NoAudioOrContextColumns(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(".", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations/*.sql: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("nenhum arquivo de migration encontrado em migrations/*.sql")
	}

	columnLine := regexp.MustCompile(`(?im)^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s+[A-Z]`)

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("ler %s: %v", file, err)
		}

		for _, line := range strings.Split(string(content), "\n") {
			m := columnLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			columnName := strings.ToLower(m[1])
			for _, pattern := range forbiddenColumnPatterns {
				if strings.Contains(columnName, pattern) {
					t.Errorf("%s: coluna %q contém padrão proibido %q (áudio/contexto não pode ser persistido)", file, columnName, pattern)
				}
			}
		}
	}
}

// tablesWithoutUserID são as tabelas que não carregam user_id, e por quê.
// Toda outra tabela tem user_id NOT NULL referenciando users (id): o
// isolamento por usuário é estrutural (AC-035, NFR-012, THR-004).
var tablesWithoutUserID = map[string]string{
	"users":              "é a tabela de usuários",
	"dictionary_aliases": "pertence a uma entrada (entry_id), que tem user_id",
	"pending_logins":     "existe antes de o usuário ser conhecido (o login em andamento)",
	"pairing_requests":   "existe antes de a pessoa aprovar (approved_by é o usuário que aprovou)",
	"device_tokens":      "pertence a um aparelho (device_id), que tem user_id",
}

// TestSchema_EveryTableIsScopedToAUser prova que toda tabela, fora a lista
// explícita acima, tem user_id NOT NULL referenciando users (id) — ou é a
// chave primária que referencia users (id), no caso do contador e das
// preferências.
func TestSchema_EveryTableIsScopedToAUser(t *testing.T) {
	scoped := regexp.MustCompile(`(?im)^\s*user_id\s+UUID\s+(NOT NULL|PRIMARY KEY)\s+REFERENCES\s+users\s*\(id\)`)
	for _, spec := range wantTables {
		block := extractCreateTableBlock(t, readMigration(t, spec.file), spec.table)
		if why, exempt := tablesWithoutUserID[spec.table]; exempt {
			if scoped.MatchString(block) {
				t.Errorf("%s: %s tem user_id mas está na lista de exceções (%s); tire-a da lista", spec.file, spec.table, why)
			}
			continue
		}
		if !scoped.MatchString(block) {
			t.Errorf("%s: %s não tem user_id UUID NOT NULL REFERENCES users (id); toda tabela é escopada por usuário (ou entra, com o motivo, em tablesWithoutUserID)", spec.file, spec.table)
		}
	}
}

// TestSchema_DeviceForeignKeysAreCompositePerUser prova que, em toda tabela
// que tem user_id, TODA coluna de aparelho (device_id, conflict_device_id, e
// qualquer outra que termine em device_id) é uma chave estrangeira COMPOSTA
// (user_id, <coluna>) -> devices (user_id, id): o banco recusa apontar para o
// aparelho de outro usuário. A forma simples (device_id UUID REFERENCES
// devices (id)) não garante isso.
func TestSchema_DeviceForeignKeysAreCompositePerUser(t *testing.T) {
	found := map[string]bool{}
	for _, spec := range wantTables {
		block := extractCreateTableBlock(t, readMigration(t, spec.file), spec.table)
		if !columnDefined(block, "user_id") {
			continue
		}
		flat := squashSQL(block)
		for _, col := range columnNames(block) {
			if col != "device_id" && !strings.HasSuffix(col, "_device_id") {
				continue
			}
			found[spec.table+"."+col] = true
			composite := "FOREIGN KEY (user_id, " + col + ") REFERENCES devices (user_id, id)"
			if !strings.Contains(flat, composite) {
				t.Errorf("%s: %s.%s é um aparelho, mas não tem %q", spec.file, spec.table, col, composite)
			}
			simple := regexp.MustCompile(`(?im)^\s*` + regexp.QuoteMeta(col) + `\s+UUID\b.*\bREFERENCES\b`)
			if simple.MatchString(block) {
				t.Errorf("%s: %s.%s referencia devices pela chave simples (%s UUID REFERENCES ...); use a chave composta", spec.file, spec.table, col, col)
			}
		}
	}
	// Seis colunas de aparelho hoje. Se a generalização acima não achar
	// nenhuma (um erro de leitura), o teste passaria sem provar nada.
	for _, want := range []string{
		"transcriptions.device_id", "transcriptions.conflict_device_id",
		"transcriptions.text_device_id", "transcriptions.trash_device_id",
		"sync_events.device_id", "applied_mutations.device_id",
	} {
		if !found[want] {
			t.Errorf("o teste não viu a coluna de aparelho %s (uma coluna nova de aparelho entra nesta lista, uma que sumiu sai)", want)
		}
	}
	if len(found) != 6 {
		t.Errorf("achei %d colunas de aparelho em tabelas com user_id (%v), esperava 6", len(found), found)
	}
}

// sqlContracts são as restrições (CHECK, NOT NULL, FOREIGN KEY, ON DELETE)
// que o 05 e a ADR-019 exigem e que dá para provar LENDO o SQL. Cada fragmento
// é procurado só no CREATE TABLE da tabela, sem comentários e com os espaços
// reduzidos a um (squashSQL), então um comentário que cita a regra não a
// "cumpre", e o alinhamento das colunas não importa. postgres_test.go prova o
// mesmo no comportamento, mas só com um Postgres; sem ele, é isto que impede
// uma restrição de sumir sem ninguém ver.
var sqlContracts = []struct {
	file  string
	table string
	want  []string
}{
	{"0001_users_table.sql", "users", []string{
		// ADR-019, F3: a chave do usuário é o sub (fixado no primeiro login), e
		// o e-mail é guardado como a lista o compara, em minúsculas.
		"google_sub TEXT NOT NULL UNIQUE CHECK (char_length(google_sub) BETWEEN 1 AND 255)",
		"email TEXT NOT NULL UNIQUE CHECK (email = lower(email))",
	}},
	{"0008_sync_events_table.sql", "sync_events", []string{
		"seq BIGINT NOT NULL CHECK (seq > 0)",
		// A lista fechada da etapa 5: o dicionário e a correção não viram evento.
		"entity_type TEXT NOT NULL CHECK (entity_type IN ('transcription', 'learned'))",
		"operation TEXT NOT NULL CHECK (operation IN ('upsert', 'tombstone'))",
		"entity_version BIGINT NOT NULL CHECK (entity_version >= 1)",
	}},
	{"0009_applied_mutations_table.sql", "applied_mutations", []string{
		// "duplicate" nunca é gravado: é a resposta a um reenvio.
		"status TEXT NOT NULL CHECK (status IN ('applied', 'conflict', 'rejected', 'gone'))",
		"result_version BIGINT CHECK (result_version >= 1)",
		"merged BOOLEAN NOT NULL DEFAULT false",
		// A mescla só vale para applied.
		"CHECK (NOT merged OR status = 'applied')",
		// A entidade e o aparelho existem sempre: sem eles, o reenvio de um
		// mutation_id para outra entidade viraria "duplicate" e a mudança
		// se perderia sem aviso.
		"device_id UUID NOT NULL",
		"entity_type TEXT NOT NULL",
		"entity_id UUID NOT NULL",
		// F8: a operação da primeira vez (o learned tem upsert e delete), da
		// lista fechada. Nula, e só nula, quando a mandada não é da lista (a
		// recusa operacao_invalida): o banco nunca guarda texto livre do
		// cliente. "operation TEXT CHECK" sem NOT NULL no meio: a coluna
		// nula (TestSchema_AppliedMutationOperation).
		"operation TEXT CHECK (operation IN ('upsert', 'delete'))",
	}},
	{"0011_web_sessions_table.sql", "web_sessions", []string{
		"label TEXT NOT NULL CHECK (char_length(label) BETWEEN 1 AND 64)",
		"CHECK (expires_at > created_at)",
	}},
	{"0012_pending_logins_table.sql", "pending_logins", []string{
		"CHECK (expires_at > created_at)",
		// Para onde voltar depois do login: um caminho da própria origem, não
		// uma URL (sem open redirect). Os três pedaços da guarda do banco.
		"return_to TEXT NOT NULL DEFAULT '/'",
		"char_length(return_to) BETWEEN 1 AND 256",
		"return_to ~ '^/[A-Za-z0-9._~/?&=%:@+-]*$'",
		"return_to NOT LIKE '//%'",
	}},
	{"0013_pairing_requests_table.sql", "pairing_requests", []string{
		"redirect_port INTEGER NOT NULL CHECK (redirect_port BETWEEN 1024 AND 65535)",
		"approved_by UUID REFERENCES users (id)",
		"code_hash BYTEA UNIQUE CHECK (octet_length(code_hash) = 32)",
		"CHECK (expires_at > created_at)",
		// A aprovação vem inteira (quem, quando e o hash do code) ou não vem.
		"CHECK ((approved_by IS NULL) = (approved_at IS NULL))",
		"CHECK ((approved_by IS NULL) = (code_hash IS NULL))",
		// Uso único: só um pedido aprovado pode ser usado.
		"CHECK (used_at IS NULL OR approved_at IS NOT NULL)",
	}},
	{"0014_device_tokens_table.sql", "device_tokens", []string{
		// Um token por aparelho, e apagar o aparelho apaga o token.
		"device_id UUID NOT NULL UNIQUE REFERENCES devices (id) ON DELETE CASCADE",
	}},
	{"0015_tombstones_table.sql", "tombstones", []string{
		"seq BIGINT NOT NULL CHECK (seq > 0)",
		// Só ditado deixa lápide: o aprendido é desfeito por undone_at.
		"entity_type TEXT NOT NULL CHECK (entity_type = 'transcription')",
		"reason TEXT NOT NULL CHECK (reason = 'apagado')",
		"dictated_at TIMESTAMPTZ NOT NULL",
	}},
	{"0016_learned_pairs_table.sql", "learned_pairs", []string{
		"variant TEXT NOT NULL CHECK (char_length(variant) BETWEEN 1 AND 80)",
		"normalized_variant TEXT NOT NULL CHECK (char_length(normalized_variant) BETWEEN 1 AND 80)",
		"term TEXT NOT NULL CHECK (char_length(term) BETWEEN 1 AND 80)",
		"restricted BOOLEAN NOT NULL DEFAULT false",
		"domain TEXT CHECK (char_length(domain) BETWEEN 1 AND 80)",
		"version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1)",
		"last_seq BIGINT NOT NULL CHECK (last_seq > 0)",
		// A restrição e o domínio: restrito sem domínio nunca age.
		"CHECK (NOT restricted OR domain IS NOT NULL)",
	}},
	// ADR-021, C3a: a chave da IA só cifrada. A lista fechada dos provedores,
	// os tamanhos fixos do id da chave de cifra (8) e do nonce do GCM (12), o
	// teto do cifrado (2560 da chave mais 16 da etiqueta) e o fim da chave
	// que a tela mostra (até 4 caracteres ASCII imprimíveis).
	{"0017_ai_provider_keys_table.sql", "ai_provider_keys", []string{
		"user_id UUID NOT NULL REFERENCES users (id)",
		"provider TEXT NOT NULL CHECK (provider IN ('openai', 'groq', 'anthropic'))",
		"cipher_key_id BYTEA NOT NULL CHECK (octet_length(cipher_key_id) = 8)",
		"nonce BYTEA NOT NULL CHECK (octet_length(nonce) = 12)",
		"ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) BETWEEN 17 AND 2576)",
		"key_suffix TEXT NOT NULL CHECK (key_suffix ~ '^[!-~]{0,4}$')",
		// As datas vêm de quem grava, sem DEFAULT.
		"created_at TIMESTAMPTZ NOT NULL,",
		"updated_at TIMESTAMPTZ NOT NULL,",
		"CHECK (updated_at >= created_at)",
	}},
}

// TestSchema_ReturnToGuard tira da 0012 a expressão regular do CHECK de
// return_to e a aplica a exemplos: o texto está lá (sqlContracts), e isto
// prova que ela faz o que o comentário diz. É uma classe de caracteres simples
// (igual na ARE do Postgres e na RE2 do Go), então a leitura em Go vale; o
// Postgres a confere de verdade em TestPostgres_LoginPendente.
func TestSchema_ReturnToGuard(t *testing.T) {
	flat := squashSQL(extractCreateTableBlock(t, readMigration(t, "0012_pending_logins_table.sql"), "pending_logins"))
	m := regexp.MustCompile(`return_to ~ '([^']+)'`).FindStringSubmatch(flat)
	if m == nil {
		t.Fatal("0012: não achei o return_to ~ '...' no CHECK")
	}
	re, err := regexp.Compile(m[1])
	if err != nil {
		t.Fatalf("0012: a expressão do return_to (%s) não compila: %v", m[1], err)
	}
	// Mesmo critério do CHECK: tamanho de 1 a 256, a expressão e "não começa com //".
	ok := func(s string) bool {
		return len(s) >= 1 && len(s) <= 256 && re.MatchString(s) && !strings.HasPrefix(s, "//")
	}
	for _, good := range []string{
		"/", "/nuvem", "/device/authorize?pedido=0190b3a0-0000-7000-8000-00000000f001",
		"/a/b/c.html?x=1&y=a%20b",
	} {
		if !ok(good) {
			t.Errorf("return_to %q deveria ser aceito", good)
		}
	}
	for _, bad := range []string{
		"", "evil", "device/authorize", "https://exemplo.test/", "//exemplo.test/", `/\exemplo.test`,
		"/a b", "/a\nb", "/a\tb", "/a\"b", "/a<b", "/a#b",
	} {
		if ok(bad) {
			t.Errorf("return_to %q deveria ser recusado", bad)
		}
	}
}

// TestSchema_ConstraintsReadFromSQL confere, no texto de cada migration, as
// restrições de sqlContracts.
func TestSchema_ConstraintsReadFromSQL(t *testing.T) {
	for _, c := range sqlContracts {
		c := c
		t.Run(c.table, func(t *testing.T) {
			flat := squashSQL(extractCreateTableBlock(t, readMigration(t, c.file), c.table))
			for _, want := range c.want {
				if !strings.Contains(flat, want) {
					t.Errorf("%s: esperava %q no CREATE TABLE %s, não encontrado", c.file, want, c.table)
				}
			}
		})
	}
}

// secretColumnWords são palavras que, num nome de coluna, indicam material
// secreto. Uma coluna assim só pode guardar o HASH ou o HMAC (sufixo _hash
// ou _hmac): quem lê o banco não pode ganhar sessão, token nem login.
var secretColumnWords = []string{"token", "secret", "password", "passwd", "credential", "api_key", "apikey"}

// TestSchema_SecretsAreStoredOnlyAsHashes varre os nomes de coluna: nada
// com cara de segredo guarda o valor em claro (ADR-019: sessão e token de
// aparelho só como HMAC; state do login e code do pareamento só como hash).
// code_verifier e nonce ficam de fora de propósito: são efêmeros (minutos)
// e o servidor precisa deles inteiros.
func TestSchema_SecretsAreStoredOnlyAsHashes(t *testing.T) {
	for _, spec := range wantTables {
		for _, col := range columnNames(extractCreateTableBlock(t, readMigration(t, spec.file), spec.table)) {
			if col == "code" || col == "state" || col == "cookie" || col == "session_id" {
				t.Errorf("%s: coluna %q guardaria um segredo em claro; guarde só o hash (ex.: code_hash, state_hash)", spec.file, col)
				continue
			}
			for _, word := range secretColumnWords {
				if strings.Contains(col, word) && !strings.HasSuffix(col, "_hmac") && !strings.HasSuffix(col, "_hash") {
					t.Errorf("%s: coluna %q parece segredo mas não termina em _hmac nem _hash", spec.file, col)
				}
			}
		}
	}
}

// TestSchema_HashColumnsAreFixedLength prova que toda coluna *_hmac ou *_hash
// tem o CHECK de 32 bytes (SHA-256 e HMAC-SHA256): um hash truncado, ou um
// valor em claro que alguém gravou ali por engano, não entra.
func TestSchema_HashColumnsAreFixedLength(t *testing.T) {
	found := 0
	for _, spec := range wantTables {
		block := extractCreateTableBlock(t, readMigration(t, spec.file), spec.table)
		for _, line := range strings.Split(block, "\n") {
			for _, col := range columnNames(block) {
				if !(strings.HasSuffix(col, "_hmac") || strings.HasSuffix(col, "_hash")) {
					continue
				}
				if !columnDefined(line, col) {
					continue
				}
				found++
				if !strings.Contains(line, "octet_length("+col+") = 32") {
					t.Errorf("%s: a coluna %s não tem CHECK (octet_length(%s) = 32)", spec.file, col, col)
				}
			}
		}
	}
	// token_hmac (web_sessions, device_tokens), state_hash, pre_hash e code_hash.
	if found != 5 {
		t.Errorf("achei %d colunas *_hmac/*_hash, esperava 5 (se entrou uma nova, ela precisa do CHECK, e este número sobe)", found)
	}
}

// TestSchema_TranscriptionContract prova, no texto da 0003, as três decisões
// que a ADR-019 tomou e que nenhuma leitura de nome de coluna pega: created_at
// SEM DEFAULT (é a data do ditado, a do PC, e a retenção conta por ela),
// e as listas fechadas de status e de mode.
func TestSchema_TranscriptionContract(t *testing.T) {
	block := extractCreateTableBlock(t, readMigration(t, "0003_transcriptions_table.sql"), "transcriptions")

	createdAt := regexp.MustCompile(`(?im)^\s*created_at\s+TIMESTAMPTZ\s+NOT NULL\s*,\s*$`)
	if !createdAt.MatchString(block) {
		t.Error("0003: created_at deveria ser TIMESTAMPTZ NOT NULL, sem DEFAULT (a data do ditado é a do PC)")
	}
	for _, want := range []string{
		"CHECK (status IN ('done', 'error'))",
		"CHECK (mode IN ('literal', 'smart', 'polish'))",
		"CHECK (purge_at IS NULL OR deleted_at IS NOT NULL)",
		"CHECK ((conflict_final_text IS NULL) = (conflict_at IS NULL))",
		// F2: as versões de cada campo nunca passam da versão da linha.
		"CHECK (text_version BETWEEN 1 AND version)",
		"CHECK (trash_version BETWEEN 1 AND version)",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("0003: esperava conter %q", want)
		}
	}
}

// TestSchema_CounterContract prova, no texto da 0010, que a marca d'água das
// lápides nunca passa do last_seq e que o epoch não tem DEFAULT (é gerado
// pela aplicação, UUID v7).
func TestSchema_CounterContract(t *testing.T) {
	block := extractCreateTableBlock(t, readMigration(t, "0010_user_sync_counters_table.sql"), "user_sync_counters")
	if !strings.Contains(block, "CHECK (tombstone_watermark <= last_seq)") {
		t.Error("0010: esperava CHECK (tombstone_watermark <= last_seq)")
	}
	epoch := regexp.MustCompile(`(?im)^\s*sync_epoch\s+UUID\s+NOT NULL\s*,\s*$`)
	if !epoch.MatchString(block) {
		t.Error("0010: sync_epoch deveria ser UUID NOT NULL, sem DEFAULT")
	}
	lastSeq := regexp.MustCompile(`(?im)^\s*last_seq\s+BIGINT\s+NOT NULL\s+DEFAULT\s+0\b`)
	if !lastSeq.MatchString(block) {
		t.Error("0010: last_seq deveria ser BIGINT NOT NULL DEFAULT 0 (o primeiro seq entregue é 1)")
	}
}

// TestSchema_ShortLivedTablesExpire prova que as tabelas de coisas que
// vencem (sessão, login pendente, pedido de pareamento) têm expires_at
// NOT NULL: nenhuma entra sem prazo.
func TestSchema_ShortLivedTablesExpire(t *testing.T) {
	files := map[string]string{
		"web_sessions":     "0011_web_sessions_table.sql",
		"pending_logins":   "0012_pending_logins_table.sql",
		"pairing_requests": "0013_pairing_requests_table.sql",
	}
	re := regexp.MustCompile(`(?im)^\s*expires_at\s+TIMESTAMPTZ\s+NOT NULL\b`)
	for table, file := range files {
		if !re.MatchString(extractCreateTableBlock(t, readMigration(t, file), table)) {
			t.Errorf("%s: %s precisa de expires_at TIMESTAMPTZ NOT NULL", file, table)
		}
	}
}

// TestSchema_StatementsAreWellFormed é um lint LÉXICO do SQL, barato e sem
// Postgres: cada comando termina em ";" e é um CREATE TABLE ou CREATE
// [UNIQUE] INDEX; os parênteses fecham; e, dentro de um CREATE TABLE, toda
// linha termina em vírgula, menos a última (o erro de digitação mais comum
// em SQL escrito à mão). NÃO prova que o Postgres aceita o SQL: isso é
// postgres_test.go.
func TestSchema_StatementsAreWellFormed(t *testing.T) {
	start := regexp.MustCompile(`(?i)^CREATE (TABLE|INDEX|UNIQUE INDEX)\s`)
	for _, file := range allMigrationFiles(t) {
		sql := strings.TrimSpace(stripSQLComments(readMigration(t, file)))
		if !strings.HasSuffix(sql, ";") {
			t.Errorf("%s: o arquivo não termina em \";\"", file)
		}
		for _, stmt := range strings.Split(sql, ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if !start.MatchString(stmt) {
				t.Errorf("%s: comando fora do que as migrations usam (CREATE TABLE ou CREATE INDEX): %.40q", file, stmt)
				continue
			}
			if strings.Count(stmt, "(") != strings.Count(stmt, ")") {
				t.Errorf("%s: parênteses desbalanceados em: %.60q", file, stmt)
			}
			if !strings.HasPrefix(strings.ToUpper(stmt), "CREATE TABLE") {
				continue
			}
			lines := nonEmptyLines(stmt)
			// lines[0] é "CREATE TABLE x (" e a última é ")".
			if len(lines) < 3 || lines[len(lines)-1] != ")" {
				t.Errorf("%s: CREATE TABLE fora do formato \"uma definição por linha, ) sozinho no fim\": %.60q", file, stmt)
				continue
			}
			body := lines[1 : len(lines)-1]
			for i, line := range body {
				last := i == len(body)-1
				switch {
				case last && strings.HasSuffix(line, ","):
					t.Errorf("%s: vírgula sobrando na última definição: %q", file, line)
				case !last && !strings.HasSuffix(line, ","):
					t.Errorf("%s: falta vírgula no fim da definição: %q", file, line)
				}
			}
		}
	}
}

// columnNames devolve os nomes das colunas de um bloco de CREATE TABLE:
// o primeiro identificador de cada linha, menos as linhas de constraint de
// tabela (PRIMARY KEY, UNIQUE, FOREIGN KEY, CHECK, CONSTRAINT).
func columnNames(block string) []string {
	var cols []string
	first := regexp.MustCompile(`^\s*([a-z_][a-z0-9_]*)\s`)
	for i, line := range strings.Split(block, "\n") {
		if i == 0 {
			continue // "CREATE TABLE x ("
		}
		m := first.FindStringSubmatch(strings.ToLower(line))
		if m == nil {
			continue
		}
		switch m[1] {
		case "primary", "unique", "foreign", "check", "constraint":
			continue
		}
		cols = append(cols, m[1])
	}
	return cols
}

// nonEmptyLines divide s em linhas, apara cada uma e descarta as vazias.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// allMigrationFiles lista os nomes dos arquivos *.sql deste diretório, em
// ordem. Falha se não houver nenhum.
func allMigrationFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(".", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations/*.sql: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("nenhum arquivo de migration encontrado em migrations/*.sql")
	}
	files := make([]string, 0, len(paths))
	for _, p := range paths {
		files = append(files, filepath.Base(p))
	}
	sort.Strings(files)
	return files
}

// stripSQLComments remove os comentários de linha ("-- ...") do SQL. As
// migrations deste diretório só usam esse estilo, e nenhuma tem "--" dentro
// de um literal de texto.
func stripSQLComments(sql string) string {
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "--"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// squashSQL tira os comentários e reduz toda sequência de espaço, tabulação e
// quebra de linha a um espaço só: o SQL das migrations alinha as colunas, e os
// testes de restrição procuram o texto sem depender do alinhamento.
func squashSQL(sql string) string {
	return strings.Join(strings.Fields(stripSQLComments(sql)), " ")
}

// readMigration lê um arquivo de migration deste diretório.
func readMigration(t *testing.T, file string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(".", file))
	if err != nil {
		t.Fatalf("ler %s: %v", file, err)
	}
	return string(content)
}

// extractCreateTableBlock isola o corpo de "CREATE TABLE <table> ( ... );"
// dentro de content, para que asserções de coluna não deem falso
// positivo batendo em comentários ou em outra tabela do mesmo arquivo.
func extractCreateTableBlock(t *testing.T, content, table string) string {
	t.Helper()
	start := strings.Index(content, "CREATE TABLE "+table+" (")
	if start == -1 {
		t.Fatalf("CREATE TABLE %s não encontrado", table)
	}
	rest := content[start:]
	end := strings.Index(rest, "\n);")
	if end == -1 {
		t.Fatalf("fim de CREATE TABLE %s (\"\\n);\") não encontrado", table)
	}
	return rest[:end]
}

// columnDefined reporta se col aparece como o primeiro identificador de
// alguma linha dentro de block, isto é, como uma definição de coluna
// (e não, por exemplo, dentro de uma UNIQUE (...) ou como substring de
// outro identificador).
func columnDefined(block, col string) bool {
	re := regexp.MustCompile(`(?im)^\s*` + regexp.QuoteMeta(col) + `\s`)
	return re.MatchString(block)
}
