package pgtest

import (
	"os"
	"strings"
	"testing"
)

// scripts/nuvem-local.ps1 sobe a nuvem no PC do Mauro para o teste da F3.
// Ninguém o roda nos testes; estes só leem o texto: PowerShell 5.1 (ASCII,
// sem && nem ||), nenhum segredo impresso, e o JSON do Google e o endereço do
// banco fora do repositório.
func TestNuvemLocal_Script(t *testing.T) {
	data, err := os.ReadFile("../../../scripts/nuvem-local.ps1")
	if err != nil {
		t.Fatalf("ler o script: %v", err)
	}
	text := string(data)
	for i, r := range text {
		if r > 127 {
			t.Fatalf("byte %d: caractere não ASCII %q", i, r)
		}
	}
	for _, bad := range []string{"&&", "||"} {
		if strings.Contains(text, bad) {
			t.Errorf("o script tem %q, que não existe no PowerShell 5.1", bad)
		}
	}
	for _, want := range []string{
		"$Pasta = Join-Path $env:USERPROFILE '.aurora-whisper-dev'",
		"RNGCryptoServiceProvider",
		"$env:PUBLIC_BASE_URL = 'http://localhost:8080'",
		"run ./cmd/server migrate",
		"run ./cmd/server serve",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("o script deveria conter %q", want)
		}
	}
	// Nenhum segredo vai para a tela: nenhuma linha que imprime cita as
	// variáveis com segredo nem o conteúdo dos arquivos.
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "Write-Host") {
			continue
		}
		for _, segredo := range []string{"$env:SESSION_SECRET", "$env:DEVICE_TOKEN_PEPPER", "$env:DATABASE_URL", "Get-Content"} {
			if strings.Contains(line, segredo) {
				t.Errorf("linha que imprime cita %s: %q", segredo, strings.TrimSpace(line))
			}
		}
	}
	if strings.Contains(text, "$Pasta = Join-Path $Raiz") || strings.Contains(text, "$Pasta = Join-Path $PSScriptRoot") {
		t.Error("a pasta dos segredos não pode ficar no repositório")
	}
}
