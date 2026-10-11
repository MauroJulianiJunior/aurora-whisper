package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/uia"
)

// TestRegistroDoFoco_ExecutavelSoNoWarn trava a segunda metade da exceção
// autorizada pelo Mauro em 29-set-2026 (decisão 5): o nome-base do
// executável em foco sai SÓ na linha WARN do rastreador. Mesmo que o
// diagnóstico traga o campo, uma linha Info ou Debug não o leva — a
// primeira metade (só o nome-base, só num aviso) é do pacote uia.
func TestRegistroDoFoco_ExecutavelSoNoWarn(t *testing.T) {
	const exe = "brave.exe"
	casos := []struct {
		nome      string
		d         uia.DiagnosticoFoco
		querNivel slog.Level
		querExe   bool
	}{
		{
			nome: "timeout é WARN e diz o executável",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectTimeout, Protected: true, Duracao: time.Minute,
				Executavel: exe},
			querNivel: slog.LevelWarn,
			querExe:   true,
		},
		{
			nome: "falha é WARN e diz o executável",
			d: uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true, Falha: uia.FalhaGetFocusedElement,
				Executavel: "Taskmgr.exe", Elevacao: uia.ElevacaoSim},
			querNivel: slog.LevelWarn,
			querExe:   true,
		},
		{
			// Descartada sai em Info mesmo sendo timeout: não leva o nome.
			nome:      "descartada é Info e não diz",
			d:         uia.DiagnosticoFoco{Outcome: uia.InspectTimeout, Protected: true, Descartada: true, Executavel: exe},
			querNivel: slog.LevelInfo,
		},
		{
			nome:      "no próprio Agent é Info e não diz",
			d:         uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true, NoProprioAgent: true, Executavel: exe},
			querNivel: slog.LevelInfo,
		},
		{
			nome:      "campo de senha é Info e não diz",
			d:         uia.DiagnosticoFoco{Outcome: uia.InspectOK, Protected: true, IsPassword: true, Executavel: exe},
			querNivel: slog.LevelInfo,
		},
		{
			nome:      "volta a livre é Info e não diz",
			d:         uia.DiagnosticoFoco{Outcome: uia.InspectOK, Liberou: true, Executavel: exe},
			querNivel: slog.LevelInfo,
		},
		{
			nome:      "inspeção comum é Debug e não diz",
			d:         uia.DiagnosticoFoco{Outcome: uia.InspectOK, Executavel: exe},
			querNivel: slog.LevelDebug,
		},
		{
			nome:      "aviso sem nome não inventa campo vazio",
			d:         uia.DiagnosticoFoco{Outcome: uia.InspectFailed, Protected: true},
			querNivel: slog.LevelWarn,
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			nivel, args := registroDoFoco(c.d)
			if nivel != c.querNivel {
				t.Fatalf("nível = %v, quero %v", nivel, c.querNivel)
			}
			got, tem := camposDoRegistro(t, args)["executavel"]
			if tem != c.querExe {
				t.Fatalf("executavel presente = %v (%v), quero %v", tem, got, c.querExe)
			}
			if tem && got != c.d.Executavel {
				t.Errorf("executavel = %v, quero %q", got, c.d.Executavel)
			}
		})
	}
}
