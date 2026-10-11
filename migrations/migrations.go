// Package migrations embute as migrations PostgreSQL do backend cloud
// (migrations/*.sql) no binário do servidor, para que elas viajem junto com
// ele e o comando "aurora-whisper-server migrate" não dependa de arquivos
// soltos no servidor.
//
// O pacote só entrega os arquivos. Quem os lê, confere e aplica é
// internal/server/pgmigrate. O que as migrations dizem e como foram
// verificadas está em migrations/README.md; a verificação estática do SQL é
// schema_test.go, e a que roda contra um Postgres de verdade é
// postgres_test.go.
package migrations

import "embed"

// FS contém os arquivos NNNN_<nome>.sql, na raiz do sistema de arquivos.
// Só *.sql entra: o README e os testes ficam de fora do binário.
//
//go:embed *.sql
var FS embed.FS
