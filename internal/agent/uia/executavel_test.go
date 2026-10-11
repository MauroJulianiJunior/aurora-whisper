package uia

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// semCaminho falha se nome tiver qualquer coisa de caminho.
func semCaminho(t *testing.T, nome string) {
	t.Helper()
	if strings.ContainsAny(nome, `\/:`) {
		t.Errorf("%q tem pedaço de caminho: só o nome-base pode sair", nome)
	}
}

// TestNomeBase: só o nome do arquivo, com as duas barras e a unidade — e
// no Linux também, onde filepath.Base não trataria "\" como separador.
func TestNomeBase(t *testing.T) {
	casos := map[string]string{
		`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`: "brave.exe",
		`C:/Windows/System32/notepad.exe`:                                    "notepad.exe",
		`\\?\C:\Users\alguem\AppData\Local\Programs\app.exe`:                 "app.exe",
		`C:Taskmgr.exe`: "Taskmgr.exe",
		`brave.exe`:     "brave.exe",
		`C:\pasta\`:     "",
		``:              "",
	}
	for caminho, quero := range casos {
		got := nomeBase(caminho)
		if got != quero {
			t.Errorf("nomeBase(%q) = %q, quero %q", caminho, got, quero)
		}
		semCaminho(t, got)
	}
}

// TestExecutavelDoAviso_SoONomeBase é a exceção autorizada pelo Mauro em
// 29-set-2026 (decisão 5), com as duas travas: só numa inspeção em
// dúvida — as que saem em WARN — e só o nome-base, nunca o caminho.
func TestExecutavelDoAviso_SoONomeBase(t *testing.T) {
	const (
		caminhoDoElemento = `C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`
		caminhoDoPlano    = `C:\Windows\System32\Taskmgr.exe`
	)
	casos := []struct {
		nome string
		s    Snapshot
		quer string
	}{
		// Aviso com elemento lido (timeout): o executável do elemento.
		{"timeout", Snapshot{Outcome: InspectTimeout, ProcessName: caminhoDoElemento, ExecutavelDoPrimeiroPlano: caminhoDoPlano}, "brave.exe"},
		// Aviso sem elemento: o da janela em primeiro plano.
		{"falha", Snapshot{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, ExecutavelDoPrimeiroPlano: caminhoDoPlano}, "Taskmgr.exe"},
		{"nil_element", Snapshot{Outcome: InspectNilElement, ExecutavelDoPrimeiroPlano: caminhoDoPlano}, "Taskmgr.exe"},
		{"falha sem nome nenhum", Snapshot{Outcome: InspectFailed}, ""},
		// Não é aviso: nada sai, nem o nome.
		{"campo livre", Snapshot{Outcome: InspectOK, ProcessName: caminhoDoElemento, ExecutavelDoPrimeiroPlano: caminhoDoPlano}, ""},
		{"campo de senha", Snapshot{Outcome: InspectOK, IsPassword: true, ProcessName: caminhoDoElemento}, ""},
		{"leitura atrasada que valeu", Snapshot{Outcome: InspectOK, Atrasada: true, EstiloLido: true, ProcessName: caminhoDoElemento}, ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := executavelDoAviso(c.s)
			if got != c.quer {
				t.Errorf("executavelDoAviso = %q, quero %q", got, c.quer)
			}
			semCaminho(t, got)
		})
	}
}

// TestDiagnosticoFoco_AvisoSoLevaONomeBase: num aviso, o diagnóstico leva o
// nome-base do executável — e nada mais do Snapshot: nem o caminho, nem o
// ControlType.
func TestDiagnosticoFoco_AvisoSoLevaONomeBase(t *testing.T) {
	const (
		pasta = "Pasta-Secreta"
		tipo  = "TIPO-SEGREDO"
	)
	snap := Snapshot{
		Outcome:     InspectTimeout,
		ProcessID:   4242,
		ProcessName: `C:\` + pasta + `\app-marcador.exe`,
		ControlType: tipo,
		EstiloLido:  true,
	}
	var got DiagnosticoFoco
	tr := NewTracker(Config{})
	tr.AoInspecionar(func(d DiagnosticoFoco) { got = d })
	tr.relatar(snap, time.Second, false, desfecho{})

	if got.Executavel != "app-marcador.exe" {
		t.Errorf("Executavel = %q, quero %q", got.Executavel, "app-marcador.exe")
	}
	for _, texto := range []string{formatarParaInspecao(got), fmt.Sprintf("%+v", got)} {
		if strings.Contains(texto, pasta) || strings.Contains(texto, tipo) {
			t.Fatalf("o aviso vazou mais que o nome-base: %s", texto)
		}
	}
}

// TestInspecionarEPublicar_AvisoDizOExecutavelDoPrimeiroPlano: pela
// sequência inteira — uma falha na janela do Gerenciador de Tarefas sai
// com o nome dele; a volta a um campo livre, sem nome nenhum.
func TestInspecionarEPublicar_AvisoDizOExecutavelDoPrimeiroPlano(t *testing.T) {
	rel := &relogio{agora: t0, passo: time.Millisecond}
	var diags []DiagnosticoFoco
	tr := NewTracker(Config{})
	tr.AoInspecionar(func(d DiagnosticoFoco) { diags = append(diags, d) })
	plano := func() primeiroPlano { return primeiroPlano{elevacao: ElevacaoSim, executavel: "Taskmgr.exe"} }

	for _, s := range []Snapshot{
		{Outcome: InspectFailed, Falha: FalhaGetFocusedElement, HRESULT: hr(eFail)},
		{Outcome: InspectOK, ProcessName: "notepad.exe"},
	} {
		s := s
		tr.inspecionarEPublicar(sistema{inspecionar: func() Snapshot { return s }, agora: rel.ler, primeiroPlano: plano}, 0, false)
	}
	if diags[0].Executavel != "Taskmgr.exe" {
		t.Errorf("aviso: Executavel = %q, quero Taskmgr.exe", diags[0].Executavel)
	}
	if diags[1].Executavel != "" {
		t.Errorf("inspeção OK levou Executavel = %q: só aviso leva", diags[1].Executavel)
	}
}
