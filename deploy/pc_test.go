package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Os scripts do PC (F11g): o agente só escreve; quem roda é o Mauro. Nenhum
// teste os executa. Eles leem o texto e conferem as promessas da ADR-019 e,
// no Windows, passam o parser do PowerShell (que lê o arquivo e não executa
// nada), como foi feito com o scripts/postgres-de-teste.ps1.

const (
	backupChave       = "scripts/backup-chave.ps1"
	restauracaoMensal = "scripts/teste-restauracao-mensal.ps1"
)

var scriptsDoPC = []string{backupChave, restauracaoMensal}

func TestPC_SoASCIICRLFSemEncadeamentoDoBash(t *testing.T) {
	for _, s := range scriptsDoPC {
		texto := ler(t, s)
		exigirASCII(t, s, texto)
		exigirCRLF(t, s, texto)
		c := codigo(texto)
		for _, ruim := range []string{"&&", "||"} {
			if strings.Contains(c, ruim) {
				t.Errorf("%s tem %q, que não existe no PowerShell 5.1", s, ruim)
			}
		}
		exigirContem(t, s, c, "Set-StrictMode -Version 2.0", "$ErrorActionPreference = 'Stop'")
		// Nunca inicia o Docker Desktop, nem mexe no sistema.
		exigirNaoContem(t, s, c, "Start-Process", "Docker Desktop.exe", "Start-Service", "wsl ", "Set-ExecutionPolicy",
			"Invoke-WebRequest", "Invoke-Expression", "iex ", "curl ", "ssh ", "scp ")
	}
}

// Os contêineres do PC: sem rede, sem porta, sem privilégio, e só com a pasta da
// chave e o arquivo do backup montados.
func TestPC_ConteinerFechado(t *testing.T) {
	montagens := regexp.MustCompile(`'-v', \((\$\w+) \+ '([^']+)'\)`)
	for _, s := range scriptsDoPC {
		c := codigo(ler(t, s))
		exigirNaoContem(t, s, c, "--privileged", "'--network', 'host'", "'-p'", "'-P'", "--publish", "docker.sock", "0.0.0.0", "'--user', 'root'")
		runs := strings.Count(c, "'run', ")
		if runs == 0 || strings.Count(c, "'--network', 'none',") != runs {
			t.Errorf("%s: todo docker run tem de ser --network none (%d run)", s, runs)
		}
		achadas := montagens.FindAllStringSubmatch(c, -1)
		if len(achadas) == 0 {
			t.Errorf("%s: não achei as montagens", s)
		}
		for _, m := range achadas {
			ok := (m[1] == "$PastaCheia" || m[1] == "$PastaDaChaveCheia") && (m[2] == ":/chave" || m[2] == ":/chave:ro") ||
				m[1] == "$ArquivoCheio" && m[2] == ":/entrada/backup.dump.age:ro"
			if !ok {
				t.Errorf("%s: montagem inesperada %s + '%s'", s, m[1], m[2])
			}
		}
		// Nenhum dado binário passa pelo cano do PowerShell (ele o trataria
		// como texto): a decifra e a restauração ficam dentro do contêiner.
		if regexp.MustCompile(`\|\s*&?\s*docker\b`).MatchString(c) {
			t.Errorf("%s passa dados pelo cano do PowerShell para o docker", s)
		}
	}
	c := codigo(ler(t, backupChave))
	if strings.Count(c, "'run', '--rm',") != 2 || strings.Count(c, "'--read-only',") != 2 {
		t.Error("os dois contêineres da chave são --rm e --read-only")
	}
	gravavel := strings.Index(c, "($PastaCheia + ':/chave'),")
	soLeitura := strings.Index(c, "($PastaCheia + ':/chave:ro'),")
	if gravavel < 0 || soLeitura < 0 || gravavel > soLeitura || strings.Count(c, "':/chave')") != 1 {
		t.Error("a pasta da chave é gravável só para gerar (uma vez) e só leitura para ler a pública, nessa ordem")
	}
	m := codigo(ler(t, restauracaoMensal))
	// O último finally é o do contêiner (o primeiro é o do Docker-Silencioso).
	fim := strings.LastIndex(m, "} finally {")
	if fim < 0 || !strings.Contains(m[fim:], "$null = Docker-Silencioso @('rm', '-f', '-v', $NomeDoConteiner)") {
		t.Error("o contêiner do teste mensal sai no finally, com o volume (rm -f -v)")
	}
	exigirContem(t, restauracaoMensal, m, "$NomeDoConteiner = 'aurora-whisper-restauracao-mensal'", "'--memory', '1g',")
}

func TestPC_RecusaPastaDentroDoRepositorio(t *testing.T) {
	for s, chamada := range map[string]string{
		backupChave:       "if (Dentro-De $PastaCheia $Raiz) {",
		restauracaoMensal: "if (Dentro-De $PastaDaChaveCheia $Raiz) {",
	} {
		c := codigo(ler(t, s))
		exigirContem(t, s, c,
			"$Raiz = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))",
			"function Dentro-De([string]$Caminho, [string]$Pasta) {",
			"return $c.StartsWith($p, [StringComparison]::OrdinalIgnoreCase)",
			"(Join-Path $env:USERPROFILE '.aurora-whisper-dev\\backup')",
			chamada,
		)
	}
	c := codigo(ler(t, backupChave))
	recusa := strings.Index(c, "if (Test-Path -LiteralPath $ArquivoDaChave) {")
	gera := strings.Index(c, "'age-keygen', '-o'")
	if recusa < 0 || gera < 0 || recusa > gera {
		t.Error("a recusa de gerar de novo vem antes do age-keygen")
	}
}

func TestPC_ChavePrivadaESenhaNuncaImpressas(t *testing.T) {
	c := codigo(ler(t, backupChave))
	// O age-keygen só com -o (grava a privada no arquivo) e -y (lê a pública).
	var flags []string
	for _, m := range regexp.MustCompile(`'age-keygen', '(-[a-z])'`).FindAllStringSubmatch(c, -1) {
		flags = append(flags, m[1])
	}
	if strings.Join(flags, " ") != "-o -y" {
		t.Errorf("o age-keygen roda duas vezes, -o (grava a privada) e depois -y (lê a pública): %q", flags)
	}
	exigirNaoContem(t, backupChave, c, "Get-Content", "ReadAllText", "ReadAllBytes", "Set-Clipboard", "Out-File")
	for _, l := range linhasDeCodigo(ler(t, backupChave)) {
		if strings.HasPrefix(l.texto, "Write-Host") && strings.Contains(l.texto, ".Texto") {
			t.Errorf("linha %d imprime a saída do docker: %s", l.n, l.texto)
		}
	}
	if !strings.Contains(c, "($publica -notmatch '^age1[02-9ac-hj-np-z]{58}$')") {
		t.Error("só a linha que é uma chave pública do age é mostrada")
	}

	m := codigo(ler(t, restauracaoMensal))
	exigirContem(t, restauracaoMensal, m,
		"[System.Security.Cryptography.RandomNumberGenerator]::Create()",
		"'-e', 'POSTGRES_PASSWORD',",
		"Remove-Item Env:POSTGRES_PASSWORD -ErrorAction SilentlyContinue")
	exigirNaoContem(t, restauracaoMensal, m, "Get-Random", "POSTGRES_PASSWORD=")
	for _, l := range linhasDeCodigo(ler(t, restauracaoMensal)) {
		if strings.HasPrefix(l.texto, "Write-Host") && (strings.Contains(l.texto, "$senha") || strings.Contains(l.texto, ".Texto")) {
			t.Errorf("linha %d imprime a senha ou a saída do docker: %s", l.n, l.texto)
		}
	}
}

func TestPC_TesteMensalConfereIdadeERestauracao(t *testing.T) {
	m := codigo(ler(t, restauracaoMensal))
	exigirContem(t, restauracaoMensal, m,
		"[int]$MaxHoras = 48",
		"'^aurora-whisper-(\\d{8}T\\d{6}Z)\\.dump\\.age$'",
		"\"yyyyMMdd'T'HHmmss'Z'\"",
		"age --decrypt --identity /chave/",
		// O pg_restore --list sai depois do índice; o cat drena o resto (sem
		// ele, o age morre de SIGPIPE num dump grande) e o código que vale é o
		// do pg_restore.
		"| { if pg_restore --list > /dev/null; then rc=0; else rc=$?; fi; cat > /dev/null; exit $rc; }",
		"--exit-on-error --single-transaction",
		"SELECT count(*) FROM schema_migrations", "SELECT count(*) FROM users",
		"if ($horas -gt $MaxHoras) {",
	)
	// O nome que o teste mensal espera é o que o backup.sh sobe.
	exigirContem(t, backupSh, ler(t, backupSh), `nome="aurora-whisper-$(date -u +%Y%m%dT%H%M%SZ).dump.age"`)
}

// O ageDecifraDeMentira escreve o cabeçalho de um dump e, no caso "grande",
// 1 MB atrás dele: bem mais que o cano (64 KiB), como um backup de verdade
// com algumas centenas de ditados. O head morre de SIGPIPE se ninguém lê.
const ageDecifraDeMentira = `
if [ "${FALSO_AGE_FALHA:-}" = 1 ]; then printf 'age: error: no identity matched any of the recipients\n' >&2; exit 1; fi
case "${FALSO_DUMP:-grande}" in
  lixo) printf 'isto nao e um dump\n' ;;
  pequeno) printf 'PGDMP-FALSO\n' ;;
  *) printf 'PGDMP-FALSO\n'; head -c 1000000 /dev/zero ;;
esac
`

// O trecho do teste mensal que confere o dump (age | pg_restore --list),
// tirado do próprio .ps1 e rodado no bash, com o age e o pg_restore de
// mentira. O pg_restore --list lê só o começo do fluxo e sai: sem o dreno, o
// age morre de SIGPIPE e o pipefail dá 141 com um backup bom (o controle
// abaixo prova que o ambiente reproduz isso).
func TestPC_TesteMensalConfereODumpGrandeSemSIGPIPE(t *testing.T) {
	m := codigo(ler(t, restauracaoMensal))
	decifrar := regexp.MustCompile(`\$decifrar = 'age --decrypt --identity /chave/' \+ \$NomeDaChave \+ '([^']*)'`).FindStringSubmatch(m)
	trechos := regexp.MustCompile(`\('set -euo pipefail; ' \+ \$decifrar \+ '([^']*)'\)\)`).FindAllStringSubmatch(m, -1)
	if decifrar == nil || len(trechos) != 2 || !strings.Contains(trechos[0][1], "pg_restore --list") {
		t.Fatalf("não achei no .ps1 o $decifrar e os dois comandos (a lista e a restauração): %q %q", decifrar, trechos)
	}
	comando := func(resto string) string {
		return "set -euo pipefail; age --decrypt --identity /chave/aurora-whisper-backup.key" + decifrar[1] + resto + "\n"
	}
	rodar := func(t *testing.T, resto string, env map[string]string) resultado {
		t.Helper()
		a := novoAmbienteBash(t)
		a.falso("age", ageDecifraDeMentira)
		a.falso("pg_restore", pgRestoreDeMentira)
		for k, v := range env {
			a.env[k] = v
		}
		s := filepath.Join(a.dir, "lista.sh")
		if err := os.WriteFile(s, []byte(comando(resto)), 0o755); err != nil {
			t.Fatal(err)
		}
		return a.rodar(s, nil)
	}

	t.Run("controle: sem o dreno, o dump grande dá SIGPIPE", func(t *testing.T) {
		if r := rodar(t, " | pg_restore --list > /dev/null", nil); r.codigo == 0 {
			t.Fatalf("o ambiente não reproduz o SIGPIPE; o teste não prova nada: %q", r.tudo())
		}
	})
	for _, c := range []struct {
		nome string
		env  map[string]string
		ok   bool
	}{
		{"dump grande", nil, true},
		{"dump pequeno", map[string]string{"FALSO_DUMP": "pequeno"}, true},
		{"chave errada (o age falha)", map[string]string{"FALSO_AGE_FALHA": "1"}, false},
		{"não é um dump", map[string]string{"FALSO_DUMP": "lixo"}, false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			r := rodar(t, trechos[0][1], c.env)
			if (r.codigo == 0) != c.ok {
				t.Fatalf("código %d, quero sucesso=%v: %q", r.codigo, c.ok, r.tudo())
			}
		})
	}
}

// Os nomes que os dois lados usam batem: a imagem, a chave e o .gitignore.
func TestPC_NomesBatem(t *testing.T) {
	for _, s := range scriptsDoPC {
		exigirContem(t, s, ler(t, s), "$Imagem = 'aurora-whisper-backup:producao'", "$NomeDaChave = 'aurora-whisper-backup.key'",
			"Join-Path $Raiz 'deploy\\backup\\Dockerfile'", "Join-Path $Raiz 'deploy\\scripts'")
	}
	exigirContem(t, restaurarSh, ler(t, restaurarSh), `IMAGEM_BACKUP="${AW_IMAGEM_BACKUP:-aurora-whisper-backup:producao}"`)
}

func TestPC_Gitignore(t *testing.T) {
	linhas := map[string]bool{}
	for _, l := range strings.Split(strings.ReplaceAll(ler(t, ".gitignore"), "\r\n", "\n"), "\n") {
		linhas[strings.TrimSpace(l)] = true
	}
	for _, want := range []string{"*.key", "*.age", "*.dump", "*.dump.age", "*.partial", "config.env", "rclone.conf", "/backup/", "/segredos/", "**/segredos/", "AGE-SECRET-KEY*"} {
		if !linhas[want] {
			t.Errorf(".gitignore deveria ter a linha %q", want)
		}
	}
}

// O parser do PowerShell 5.1, só no Windows: ele lê os arquivos e não executa
// nada.
func TestPC_ParserDoPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("o parser do PowerShell 5.1 só existe no Windows")
	}
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("sem o powershell.exe")
	}
	var b strings.Builder
	b.WriteString("$falhas = 0; ")
	todos := append(append(append([]string{}, scriptsDoPC...), scriptsDoPCParte2...), "scripts/postgres-de-teste.ps1")
	for _, s := range todos {
		abs := caminhoAbsoluto(t, s)
		b.WriteString("$t = $null; $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('" +
			strings.ReplaceAll(abs, "'", "''") + "', [ref]$t, [ref]$e); ")
		b.WriteString("foreach ($x in $e) { $falhas++; Write-Output ('" + s + ": ' + $x.Message + ' (linha ' + $x.Extent.StartLineNumber + ')') }; ")
	}
	b.WriteString("Write-Output ('FALHAS=' + $falhas)")
	out, err := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", b.String()).CombinedOutput()
	if err != nil {
		t.Fatalf("powershell: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "FALHAS=0") {
		t.Errorf("o parser do PowerShell achou erro:\n%s", out)
	}
}
