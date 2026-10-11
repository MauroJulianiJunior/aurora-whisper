package uia

import "errors"

// ErrNotSupported é devolvido por Tracker.Start/Stop em qualquer SO que
// não seja Windows (tracker_other.go) — existe só para que este pacote, e
// tudo que o importa, continue compilando e testável (a parte pura) no
// runner Linux do CI (ver doc.go e AGENTS.md/CLAUDE.md, "compilação
// multiplataforma"). Nenhum caminho de produção chama isto fora do
// Windows.
var ErrNotSupported = errors.New("uia: rastreador de foco só é suportado no Windows")
