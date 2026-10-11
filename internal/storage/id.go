package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewID gera um identificador global ordenável (UUID v7, RFC 9562) para
// uso como chave primária TEXT nas tabelas locais e, futuramente, como id
// das entidades espelhadas no Postgres (ver docs/ai-spec/05-data-model.md,
// "IDs": "UUID v7 ou ID global ordenável equivalente").
//
// UUID v7 embute um timestamp de milissegundos nos primeiros 48 bits,
// então IDs gerados em sequência ordenam lexicograficamente na mesma
// ordem em que foram criados — útil para os índices de histórico e para
// cursors de paginação estáveis sem depender só de created_at.
//
// Não há dependência externa aqui de propósito: nesta fase a única
// dependência de produção nova aprovada é modernc.org/sqlite, e só para
// teste (ADR-008). Gerar UUID v7 é pouco código e não justifica uma
// dependência nova.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read só falha em cenários de sistema
		// irrecuperáveis (ex.: sem fonte de entropia); não há um
		// retorno de erro sensato para um gerador de ID.
		panic(fmt.Errorf("storage: gerar UUID v7: %w", err))
	}

	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	// Versão 7 nos 4 bits altos do byte 6.
	b[6] = (b[6] & 0x0F) | 0x70
	// Variante RFC 9562 (10xxxxxx) nos 2 bits altos do byte 8.
	b[8] = (b[8] & 0x3F) | 0x80

	hexStr := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}
