package storage

import "github.com/MauroJulianiJunior/aurora-whisper/internal/ids"

// NewID gera um identificador global ordenável (UUID v7, RFC 9562) para uso
// como chave primária TEXT nas tabelas locais e como id das entidades
// espelhadas no Postgres (docs/ai-spec/05-data-model.md, "IDs").
//
// A implementação mora em internal/ids (ADR-021, C2), para que um pacote puro
// gere um id sem importar o storage; esta função delega para lá, com o mesmo
// formato e o mesmo comportamento de sempre.
func NewID() string { return ids.New() }
