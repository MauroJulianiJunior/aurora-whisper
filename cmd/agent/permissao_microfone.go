package main

import (
	"log/slog"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/permissao"
)

// avisarMicrofoneBloqueado grava uma linha WARN na subida quando a
// privacidade do Windows bloqueia o microfone, e devolve o motivo lido.
//
// É o rastro para quem for diagnosticar "instalei e não acontece nada": o
// log diz que o Agent PERCEBEU o bloqueio, antes de qualquer ditado. Só o
// motivo (uma palavra: empresa, aparelho, conta, programas, este_app) —
// nunca caminho, título nem texto (CLAUDE.md, "Safety rules"). Liberado e
// "não deu para ler" não dizem nada: na dúvida o Agent não afirma bloqueio.
func avisarMicrofoneBloqueado(log *slog.Logger, consulta func() permissao.Motivo) permissao.Motivo {
	m := consulta()
	if m.Bloqueado() {
		log.Warn("permissao: o Windows bloqueia o microfone; o painel explica como liberar", "motivo", string(m))
	}
	return m
}
