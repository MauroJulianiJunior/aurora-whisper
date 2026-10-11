package main

import (
	"log/slog"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/clock"
	"github.com/MauroJulianiJunior/aurora-whisper/internal/storage"
)

// abrirHistoricoEm é abrirHistoricoComRetencao sem devolver o ciclo do
// expurgo, para os testes que só querem o repositório, as preferências, o dono
// e a função de fechar (que também para o ciclo antes de fechar o banco). O
// Agent chama abrirHistoricoComRetencao direto (ADR-019, F10d: as rotas dos 6
// meses precisam do ciclo); o conferente de caminhos.go lê só o main.go, e por
// isso este wrapper mora aqui, fora dele.
func abrirHistoricoEm(log *slog.Logger, caminho, usuarioDoDicionario string, relogio clock.Clock) (*storage.SQLiteTranscriptionRepository, *storage.SQLiteSettingsRepository, string, func(), error) {
	h, err := abrirHistoricoComRetencao(log, caminho, usuarioDoDicionario, relogio)
	return h.hist, h.prefs, h.dono, h.fechar, err
}
