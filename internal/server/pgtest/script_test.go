package pgtest

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// O script scripts/postgres-de-teste.ps1 é o que o Mauro roda para subir o
// Postgres dos testes, e NINGUÉM o roda nos testes (subir contêiner muda o
// estado da máquina). Estes testes só LEEM o texto dele e conferem as
// promessas da ADR-019 e do pedido da F1: porta só no loopback, senha
// aleatória, sem volume persistente, e sem iniciar o Docker Desktop. Não
// provam que ele funciona (isso é o primeiro uso do Mauro); provam que o texto
// não quebrou nenhuma dessas promessas.

const scriptPath = "../../../scripts/postgres-de-teste.ps1"

func readScript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("ler o script: %v", err)
	}
	return string(data)
}

func TestScript_SoASCIIESemEncadeamentoDoBash(t *testing.T) {
	text := readScript(t)
	for i, r := range text {
		if r > 127 {
			t.Fatalf("byte %d: caractere não ASCII %q (o PowerShell 5.1 lê o arquivo na página de código do sistema)", i, r)
		}
	}
	for _, bad := range []string{"&&", "||"} {
		if strings.Contains(text, bad) {
			t.Errorf("o script tem %q, que não existe no PowerShell 5.1", bad)
		}
	}
}

func TestScript_PortaSoNoLoopback(t *testing.T) {
	text := readScript(t)
	if !strings.Contains(text, "'127.0.0.1:' + $Porta + ':5432'") {
		t.Error("a publicação da porta deveria ser '127.0.0.1:' + $Porta + ':5432'")
	}
	if !strings.Contains(text, "'-p', $publicar") {
		t.Error("o docker run deveria receber '-p', $publicar")
	}
	for _, bad := range []string{"0.0.0.0", "'-P'", "--publish-all", "'--network', 'host'", "--net=host"} {
		if strings.Contains(text, bad) {
			t.Errorf("o script tem %q: a porta não pode sair do loopback", bad)
		}
	}
	// A URL que o script imprime também aponta para o loopback.
	if !strings.Contains(text, "'@127.0.0.1:'") {
		t.Error("a URL impressa deveria usar @127.0.0.1:<porta>")
	}
}

func TestScript_SenhaAleatoriaNuncaFixa(t *testing.T) {
	text := readScript(t)
	if !strings.Contains(text, "RandomNumberGenerator") {
		t.Error("a senha deveria vir do gerador criptográfico (RandomNumberGenerator)")
	}
	if strings.Contains(text, "Get-Random") {
		t.Error("Get-Random não é criptográfico; a senha não pode sair dele")
	}
	// "-e POSTGRES_PASSWORD" sem valor: a senha vem do ambiente do processo e
	// não aparece na linha de comando do docker.
	if !strings.Contains(text, "'-e', 'POSTGRES_PASSWORD'") {
		t.Error("o docker run deveria receber '-e', 'POSTGRES_PASSWORD' (sem valor)")
	}
	literal := regexp.MustCompile(`(?i)POSTGRES_PASSWORD\s*=\s*['"][^'"\s]`)
	if m := literal.FindString(text); m != "" {
		t.Errorf("senha escrita no script: %q", m)
	}
	if !strings.Contains(text, "Remove-Item Env:POSTGRES_PASSWORD") {
		t.Error("a variável POSTGRES_PASSWORD deveria ser removida do ambiente depois do docker run")
	}
}

func TestScript_SemVolumePersistente(t *testing.T) {
	text := readScript(t)
	for _, bad := range []string{"--mount", "--volume", "volume create", "--restart"} {
		if strings.Contains(text, bad) {
			t.Errorf("o script tem %q: o banco é descartável, sem volume nem reinício automático", bad)
		}
	}
	// O único '-v' é o do "docker rm -f -v", que REMOVE o volume anônimo.
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "'-v'") && !strings.Contains(line, "'rm'") {
			t.Errorf("'-v' fora do docker rm: %q", strings.TrimSpace(line))
		}
	}
	if !strings.Contains(text, "'rm', '-f', '-v'") {
		t.Error("-Parar deveria rodar docker rm -f -v (leva o volume junto)")
	}
}

func TestScript_NomesEFlags(t *testing.T) {
	text := readScript(t)
	for _, want := range []string{
		"$NomeDoConteiner = 'aurora-whisper-pg-teste'",
		"$Imagem = 'postgres:18'",
		"[switch]$Parar",
		EnvURL,
		"Docker Desktop", // a mensagem de quando o Docker não responde
	} {
		if !strings.Contains(text, want) {
			t.Errorf("o script deveria conter %q", want)
		}
	}
}

func TestScript_NaoIniciaODockerDesktop(t *testing.T) {
	text := readScript(t)
	for _, bad := range []string{"Start-Process", "Docker Desktop.exe", "Start-Service", "wsl --"} {
		if strings.Contains(text, bad) {
			t.Errorf("o script tem %q: ele nunca inicia o Docker Desktop, só manda abri-lo", bad)
		}
	}
}

// TestScript_EnderecoNumArquivoForaDoRepositorio: o endereço (com a senha) vai
// para %USERPROFILE%\.aurora-whisper-dev, nunca para uma pasta do repositório
// ($Raiz, $PSScriptRoot), e o -Parar apaga o arquivo (decisão do Mauro,
// 05-out-2026: o agente roda os testes enquanto o banco fica ligado).
func TestScript_EnderecoNumArquivoForaDoRepositorio(t *testing.T) {
	text := readScript(t)
	for _, want := range []string{
		"$PastaDoEndereco = Join-Path $env:USERPROFILE '.aurora-whisper-dev'",
		"$ArquivoDoEndereco = Join-Path $PastaDoEndereco 'postgres-de-teste.url'",
		"[IO.File]::WriteAllText($ArquivoDoEndereco, $url)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("o script deveria conter %q", want)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "$PastaDoEndereco =") && (strings.Contains(line, "$Raiz") || strings.Contains(line, "$PSScriptRoot")) {
			t.Errorf("o arquivo do endereço não pode morar no repositório: %q", strings.TrimSpace(line))
		}
	}
	// A senha nunca vai para a tela (06-out-2026): nenhuma linha que imprime
	// cita o $url, que a contém.
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "Write-Host") && strings.Contains(line, "$url") {
			t.Errorf("linha que imprime o endereço com a senha: %q", strings.TrimSpace(line))
		}
	}
	// O Remove-Item do arquivo fica dentro do bloco do -Parar, antes do exit.
	parar := text[strings.Index(text, "if ($Parar) {"):]
	parar = parar[:strings.Index(parar, "exit 0")]
	if !strings.Contains(parar, "Remove-Item -LiteralPath $ArquivoDoEndereco") {
		t.Error("o -Parar deveria apagar o arquivo do endereço")
	}
}

func TestScript_DocumentadoNoReadmeEnoEnv(t *testing.T) {
	for path, wants := range map[string][]string{
		"../../../scripts/README.md":                  {"postgres-de-teste.ps1", EnvURL, "-Parar"},
		"../../../docs/ai-spec/10-env-and-secrets.md": {EnvURL, "DATABASE_URL"},
		"../../../migrations/README.md":               {EnvURL, "migrate"},
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("ler %s: %v", path, err)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s deveria mencionar %q", path, want)
			}
		}
	}
}
