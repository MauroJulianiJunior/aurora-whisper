package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/MauroJulianiJunior/aurora-whisper/internal/agent/uia"
)

// TestRegistroDaAutomacao: o log da subida diz qual objeto de UI
// Automation o rastreador usa e com que teto — e sai em WARN quando o teto
// não está inteiro, porque aí uma chamada presa volta a poder durar um
// minuto (29-set-2026).
func TestRegistroDaAutomacao(t *testing.T) {
	casos := []struct {
		nome       string
		a          uia.Automacao
		querNivel  slog.Level
		querCampos map[string]any
		semCampos  []string
	}{
		{
			nome: "CUIAutomation8 com teto é Info",
			a: uia.Automacao{Objeto: uia.ObjetoCUIAutomation8,
				TetoConexao: 3 * time.Second, TetoTransacao: 3 * time.Second},
			querNivel: slog.LevelInfo,
			querCampos: map[string]any{"objeto": "CUIAutomation8",
				"teto_conexao": 3 * time.Second, "teto_transacao": 3 * time.Second},
			semCampos: []string{"hresult_cuiautomation8", "hresult_teto_conexao", "hresult_teto_transacao"},
		},
		{
			nome:       "caiu no CUIAutomation antigo é WARN e diz por quê",
			a:          uia.Automacao{Objeto: uia.ObjetoCUIAutomation, HRESULT8: -2147221164},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"objeto": "CUIAutomation", "hresult_cuiautomation8": "0x80040154"},
		},
		{
			nome: "um teto que não se aplicou é WARN e diz qual",
			a: uia.Automacao{Objeto: uia.ObjetoCUIAutomation8, TetoTransacao: 3 * time.Second,
				HRESULTConexao: -2147467259},
			querNivel:  slog.LevelWarn,
			querCampos: map[string]any{"hresult_teto_conexao": "0x80004005", "teto_conexao": time.Duration(0)},
			semCampos:  []string{"hresult_teto_transacao"},
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			nivel, args := registroDaAutomacao(c.a)
			if nivel != c.querNivel {
				t.Errorf("nível = %v, quero %v", nivel, c.querNivel)
			}
			campos := camposDoRegistro(t, args)
			for k, v := range c.querCampos {
				if got, ok := campos[k]; !ok || got != v {
					t.Errorf("%s = %v (presente=%v), quero %v", k, got, ok, v)
				}
			}
			for _, k := range c.semCampos {
				if _, ok := campos[k]; ok {
					t.Errorf("%s não deveria sair: %v", k, args)
				}
			}
		})
	}
}
