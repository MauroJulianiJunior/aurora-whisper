package server

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// listCursorKey é a posição de desempate estável usada pela paginação
// por cursor de List (CreatedAt desc, ID desc como desempate) — mesmo
// desenho de internal/storage (ver seu transcription_memory.go), só que
// duplicado aqui: os dois pacotes não compartilham tipo não exportado, e
// isso é intencional (ver AGENTS.md/CLAUDE.md: cada agente fica no seu
// pacote; storage e server não devem se acoplar por um detalhe interno
// de paginação).
type listCursorKey struct {
	createdAt time.Time
	id        string
}

func encodeListCursor(k listCursorKey) string {
	raw := k.createdAt.UTC().Format(time.RFC3339Nano) + "|" + k.id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeListCursor(s string) (listCursorKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return listCursorKey{}, fmt.Errorf("server: cursor inválido: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return listCursorKey{}, fmt.Errorf("server: cursor malformado")
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return listCursorKey{}, fmt.Errorf("server: cursor com timestamp inválido: %w", err)
	}
	return listCursorKey{createdAt: ts, id: parts[1]}, nil
}

// after é true quando k vem estritamente depois do ponto de corte cur
// na ordenação (CreatedAt desc, ID desc) — ou seja, quando k deve
// entrar na página seguinte a cur.
func (k listCursorKey) after(cur listCursorKey) bool {
	if k.createdAt.Equal(cur.createdAt) {
		return k.id < cur.id
	}
	return k.createdAt.Before(cur.createdAt)
}
