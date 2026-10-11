package deploy

import (
	"regexp"
	"strings"
	"testing"
)

// Os scripts do PC da Parte 2 (F11j): o deploy-teste.ps1 (o deploy inteiro
// no Docker do PC, derrubado no fim), o enviar-para-servidor.ps1 (as imagens
// aprovadas por ssh, a pasta deploy e o JSON do Google) e o conferir-nuvem.ps1
// (de fora, só leitura). O agente só escreve; quem roda é o Mauro. Os testes
// leem o texto e, no Windows, passam o parser do PowerShell 5.1
// (TestPC_ParserDoPowerShell).

const (
	deployTeste = "scripts/deploy-teste.ps1"
	enviar      = "scripts/enviar-para-servidor.ps1"
	conferirNuv = "scripts/conferir-nuvem.ps1"
	composeTst  = "deploy/compose.teste.yml"
	traefikTst  = "deploy/teste/traefik-dinamico.yml"
)

var scriptsDoPCParte2 = []string{deployTeste, enviar, conferirNuv}

func TestPC2_SoASCIICRLFSemEncadeamentoDoBash(t *testing.T) {
	for _, s := range scriptsDoPCParte2 {
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
		exigirNaoContem(t, s, c, "Start-Process", "Docker Desktop.exe", "Start-Service", "Set-ExecutionPolicy",
			"Invoke-Expression", "iex ", "Invoke-WebRequest", "docker.sock", "--privileged", "'--network', 'host'",
			"Get-Random", "system', 'prune", "'prune'", "Set-Clipboard")
		// Nenhum dado binário pelo cano do PowerShell (ele o trataria como
		// texto): o docker save | ssh vai pelo cmd.
		for _, l := range linhasDeCodigo(texto) {
			if strings.HasPrefix(l.texto, "$linha = ") {
				continue
			}
			// O texto entre aspas simples é mensagem, não comando.
			semTexto := regexp.MustCompile(`'[^']*'`).ReplaceAllString(l.texto, "''")
			if regexp.MustCompile(`\|\s*&?\s*(docker|ssh|scp)\b`).MatchString(semTexto) {
				t.Errorf("%s, linha %d: dados pelo cano do PowerShell: %s", s, l.n, l.texto)
			}
		}
	}
}

// A CSP e o HSTS que os scripts esperam são os do servidor.
func TestPC2_CabecalhosSaoOsDoServidor(t *testing.T) {
	seg := ler(t, "internal/server/seguranca.go")
	m := regexp.MustCompile(`const csp = "([^"]+)"`).FindStringSubmatch(seg)
	h := regexp.MustCompile(`const hsts = "([^"]+)"`).FindStringSubmatch(seg)
	if m == nil || h == nil {
		t.Fatal("não achei a csp e o hsts em internal/server/seguranca.go")
	}
	for _, s := range []string{deployTeste, conferirNuv} {
		exigirContem(t, s, codigo(ler(t, s)),
			"$CspDoServidor = '"+strings.ReplaceAll(m[1], "'", "''")+"'",
			"$HstsDoServidor = '"+h[1]+"'")
	}
}

func TestPC2_DeployTesteSoTocaNoQueEDoTeste(t *testing.T) {
	c := codigo(ler(t, deployTeste))
	exigirContem(t, deployTeste, c,
		"$Projeto = 'aurora-whisper-teste'", "$Prefixo = 'aurora-whisper-teste'", "$Rotulo = 'aurora-whisper-teste=1'",
		"$RedeDoProxy = 'aurora-whisper-teste-web'", "$Dominio = 'aurora-whisper.exemplo.test'",
		"$Compose = Join-Path $Raiz 'deploy\\compose.yml'", "$ComposeTeste = Join-Path $Raiz 'deploy\\compose.teste.yml'",
		"@('compose', '--project-name', $Projeto, '--env-file', $ArquivoConfig, '-f', $Compose, '-f', $ComposeTeste)",
		// Os descartáveis levam o rótulo do teste (a limpeza os acha) e
		// nenhuma capacidade.
		"@('run', '--rm', '--label', $Rotulo, '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges')",
		// A limpeza: o down do projeto do teste e a varredura só pelos rótulos
		// do teste.
		"$null = Dc @('down', '-v', '--remove-orphans')",
		"foreach ($filtro in @(('label=com.docker.compose.project=' + $Projeto), ('label=' + $Rotulo))) {",
		"@('ps', '-a', '-q', '--filter', $filtro)", "@('volume', 'ls', '-q', '--filter', $filtro)",
		"@('network', 'ls', '-q', '--filter', $filtro)",
		"@('network', 'create', '--label', $Rotulo, $RedeDoProxy)",
		// A pasta temporária e o aprovado.
		"$Pasta = Join-Path ([IO.Path]::GetTempPath()) 'aurora-whisper-deploy-teste'",
		"$Aprovado = Join-Path $env:USERPROFILE '.aurora-whisper-dev\\deploy-teste-aprovado.txt'",
	)
	// O finally derruba tudo, de certo ou de errado; o aprovado só é
	// gravado no fim do try, depois de todas as conferências.
	fim := strings.LastIndex(c, "} finally {")
	if fim < 0 || !strings.HasPrefix(strings.TrimSpace(c[fim+len("} finally {"):]), "Passo 'Derrubando tudo o que e do teste'\nDerrubar\n}") {
		t.Error("o finally do deploy-teste.ps1 derruba tudo")
	}
	aprova := strings.Index(c, "$passou = $true")
	for _, antes := range []string{"'pos-restauracao', '-confirmo'", "o epoch da conta nao mudou", "o log de acesso registrou", "'backup.sh')"} {
		if i := strings.Index(c, antes); i < 0 || aprova < i {
			t.Errorf("o aprovado vem depois de %q", antes)
		}
	}
	// Nenhum docker exec, logs ou inspect num contêiner com o nome da VPS:
	// só pelo prefixo do teste.
	for _, l := range linhasDeCodigo(ler(t, deployTeste)) {
		if regexp.MustCompile(`'(exec|logs|inspect|rm)'[^)]*'aurora-whisper-(api|postgres|backup|borda)'`).MatchString(l.texto) {
			t.Errorf("linha %d usa o nome de um contêiner da VPS: %s", l.n, l.texto)
		}
	}
	// O teste nunca publica porta fora do 127.0.0.1 (só a borda, pelo compose
	// do teste) nem roda contêiner com rede do host.
	exigirNaoContem(t, deployTeste, c, "'-p'", "'--publish'", "0.0.0.0")
}

func TestPC2_DeployTesteConfereOQueAADRPede(t *testing.T) {
	c := codigo(ler(t, deployTeste))
	exigirContem(t, deployTeste, c,
		// o build das duas imagens, com os mesmos comandos dos Dockerfiles
		"& docker build -f (Join-Path $Raiz 'deploy\\Dockerfile') -t $ImagemApi $Raiz",
		"& docker build -f (Join-Path $Raiz 'deploy\\backup\\Dockerfile') -t $ImagemBackup (Join-Path $Raiz 'deploy\\scripts')",
		// os segredos de mentira, do gerador criptográfico
		"[System.Security.Cryptography.RandomNumberGenerator]::Create()",
		// o Postgres: os SHOW e os papéis
		"'log_error_verbosity'               = 'terse'", "'log_statement'                     = 'none'",
		"'log_min_duration_statement'        = '-1'", "'log_parameter_max_length_on_error' = '0'",
		"Contains('auto_explain')", "'false false false false false'", "'false true false'",
		// o migrate, pelo mesmo binário, num descartável
		"Descartavel ($migrar + @('migrate', '-status'))", "Descartavel ($migrar + @('migrate'))",
		// a ordem do log da API
		`'"logins_pendentes"'`, `'"lapides_criadas"'`, `'"msg":"servindo"'`, `'"proxy_confiavel":true'`,
		"($nArrumacao -ge $nRetencao) -or ($nRetencao -ge $nServindo)",
		// a saúde do job por docker exec, como no subir.sh (o healthcheck do
		// compose é só o de vida)
		"@('exec', ($Prefixo + '-api'), $BinarioDaApi, 'healthcheck', '-retencao')",
		"$BinarioDaApi = '"+constanteDoScript(t, subirSh, "BINARIO_DA_API")+"'",
		// a borda
		"($errado.Codigo -ne 404) -and ($errado.Codigo -ne 421)",
		"'/api/v1/health/retencao'", "($retencao.Codigo -ne 404)",
		"($csp.Count -ne 1) -or ($csp[0] -ne $CspDoServidor)", "($hsts.Count -ne 1) -or ($hsts[0] -ne $HstsDoServidor)",
		"$acesso.Contains($sentinelaOutra)", "$acesso.Contains($sentinelaNossa)",
		"$logApi.Contains('X-Forwarded-For de quem')",
		// o backup e a saúde do agendador
		"'agendar-backup.sh', 'saude'", "@('exec', ($Prefixo + '-backup'), 'backup.sh')", "'cmp'",
		// a restauração num banco novo, num descartável, e o epoch
		"--exit-on-error --single-transaction", "'pos-restauracao', '-confirmo'",
		"($epochOriginal.Texto -ne $EpochAntes)", "($epochRestaurado.Texto -eq $EpochAntes)",
	)
	// Os nomes que o teste lê no log são os que o servidor escreve.
	exigirContem(t, "internal/server/nuvem.go", ler(t, "internal/server/nuvem.go"), `c.log.Info("arrumação", "logins_pendentes"`, `"lapides_criadas"`)
	exigirContem(t, "cmd/server/serve.go", ler(t, "cmd/server/serve.go"), `logger.Info("servindo"`, `"proxy_confiavel", cfg.proxy.Ligado()`)
	exigirContem(t, "internal/server/saude.go", ler(t, "internal/server/saude.go"), `c.log.Warn("X-Forwarded-For de quem`)
	exigirContem(t, "cmd/server/posrestauracao.go", ler(t, "cmd/server/posrestauracao.go"), `"pos-restauracao: %d conta(s) com o epoch novo`)
	// O B2 de mentira é uma pasta DENTRO do volume do backup, que o backup.sh
	// não varre (só os arquivos do topo e as pastas .trabalho.*).
	exigirContem(t, deployTeste, c, `"[b2]`+"`n"+`type = alias`+"`n"+`remote = /backup/b2-de-teste`+"`n"+`"`)
	exigirContem(t, backupSh, ler(t, backupSh), `for f in "$PASTA"/aurora-whisper-*.dump.age; do`, `for d in "$PASTA"/.trabalho.*; do`)
	// O config.env do teste tem as variáveis que o compose exige.
	for _, v := range []string{"PUBLIC_BASE_URL", "DOMINIO", "CONTAS_PERMITIDAS", "B2_BUCKET", "PROXY_CONFIAVEL", "AW_PREFIXO", "AW_SEGREDOS", "AW_REDE_DO_PROXY"} {
		exigirContem(t, deployTeste, c, "('"+v+"=")
		exigirContem(t, composeYml, ler(t, composeYml), "${"+v)
	}
	// O JSON do Google de mentira tem o redirect que o servidor deriva do
	// PUBLIC_BASE_URL do teste.
	exigirContem(t, deployTeste, c, `"redirect_uris":["https://' + $Dominio + '/auth/google/callback"]`, "('PUBLIC_BASE_URL=https://' + $Dominio)")
}

// O compose do teste: só a borda, só em 127.0.0.1, sem socket, e com o mesmo
// Traefik da VPS; e o roteador do arquivo é o dos rótulos do compose.
func TestPC2_ComposeDoTesteEABorda(t *testing.T) {
	texto := ler(t, composeTst)
	exigirASCII(t, composeTst, texto)
	exigirLF(t, composeTst, texto)
	raiz := lerYAML(t, composeTst, texto)
	if got := strings.Join(chavesYAML(raiz.(map[string]any)), " "); got != "services" {
		t.Errorf("o compose do teste só acrescenta serviço: %s", got)
	}
	if got := strings.Join(chavesYAML(mapaYAML(t, raiz, "services")), " "); got != "aurora-whisper-borda-teste" {
		t.Errorf("o compose do teste só tem a borda: %s", got)
	}
	b := func(c ...string) string {
		return textoYAML(t, raiz, append([]string{"services", "aurora-whisper-borda-teste"}, c...)...)
	}
	if b("image") != "traefik:v3.6.21" {
		t.Error("a borda de teste é o Traefik da versão fixada (v3.6.21), a mesma do servidor")
	}
	if b("container_name") != "${AW_PREFIXO:?so no teste do PC}-borda" {
		t.Error("a borda só existe com o prefixo do teste")
	}
	for _, p := range listaYAML(t, raiz, "services", "aurora-whisper-borda-teste", "ports") {
		if !strings.HasPrefix(p, "127.0.0.1:") {
			t.Errorf("porta fora do 127.0.0.1: %s", p)
		}
	}
	cmd := strings.Join(listaYAML(t, raiz, "services", "aurora-whisper-borda-teste", "command"), " ")
	exigirContem(t, composeTst, cmd, "--providers.file.filename=/etc/traefik/dinamico.yml", "--accesslog=true",
		"--entrypoints.web.http.redirections.entrypoint.to=websecure", "--entrypoints.websecure.address=")
	exigirNaoContem(t, composeTst, texto, "docker.sock", "providers.docker", "/var/run")
	if got := strings.Join(listaYAML(t, raiz, "services", "aurora-whisper-borda-teste", "volumes"), " "); got != "./teste/traefik-dinamico.yml:/etc/traefik/dinamico.yml:ro" {
		t.Errorf("a borda monta só o arquivo dos roteadores: %s", got)
	}

	// O roteador do arquivo e os rótulos do compose dizem o mesmo.
	tt := ler(t, traefikTst)
	exigirASCII(t, traefikTst, tt)
	exigirLF(t, traefikTst, tt)
	din := lerYAML(t, traefikTst, tt)
	rot := rotulos(t, compose(t), svcAPI)
	r := func(c ...string) string {
		return textoYAML(t, din, append([]string{"http", "routers", "aurora-whisper-api"}, c...)...)
	}
	if r("rule") != "Host(`aurora-whisper.exemplo.test`)" {
		t.Errorf("a regra do teste: %s", r("rule"))
	}
	if strings.Replace(rot["traefik.http.routers.aurora-whisper-api.rule"], "${DOMINIO:?falta o DOMINIO no config.env}", "aurora-whisper.exemplo.test", 1) != r("rule") {
		t.Error("a regra do teste e a do rótulo só diferem no domínio")
	}
	if strings.Join(listaYAML(t, din, "http", "routers", "aurora-whisper-api", "entryPoints"), " ") != rot["traefik.http.routers.aurora-whisper-api.entrypoints"] {
		t.Error("o entrypoint do teste é o do rótulo")
	}
	if r("service") != rot["traefik.http.routers.aurora-whisper-api.service"] {
		t.Error("o serviço do teste é o do rótulo")
	}
	if r("observability", "accessLogs") != rot["traefik.http.routers.aurora-whisper-api.observability.accesslogs"] || r("observability", "accessLogs") != "false" {
		t.Error("o log de acesso desligado no router, no teste e no rótulo")
	}
	if len(mapaYAML(t, din, "http", "routers", "aurora-whisper-api", "tls")) != 0 {
		t.Error("o TLS do teste é o padrão do Traefik (sem resolvedor)")
	}
	srv := noYAML(t, din, "http", "services", "aurora-whisper-api", "loadBalancer", "servers").([]any)
	if len(srv) != 1 || srv[0].(map[string]any)["url"] != "http://"+svcAPI+":"+rot["traefik.http.services.aurora-whisper-api.loadbalancer.server.port"] {
		t.Errorf("o servidor do teste é a API na porta do rótulo: %#v", srv)
	}
}

func TestPC2_EnviarSoOQuePassouNoTeste(t *testing.T) {
	c := codigo(ler(t, enviar))
	exigirContem(t, enviar, c,
		"$Aprovado = Join-Path $env:USERPROFILE '.aurora-whisper-dev\\deploy-teste-aprovado.txt'",
		"@('image', 'inspect', '-f', '{{.Id}}', $img)",
		"($aprovadas[$img] -ne $id.Texto.Trim())",
		// o save | load pelo cmd, que passa os bytes sem mexer
		"$linha = 'docker save ' + $ImagemApi + ' ' + $ImagemBackup + ' | ssh -C ' + $Servidor + ' docker load'",
		"Rodar-AVista 'cmd.exe' @('/d', '/c', $linha)",
		// a pasta fora do repositório, 0700, e o destino do JSON que o
		// gerar-segredos.sh lê
		"'set -e; umask 077; mkdir -p aurora-whisper/recebido; chmod 700 aurora-whisper aurora-whisper/recebido'",
		"@('-q', '-r', 'deploy', ($Servidor + ':aurora-whisper/'))",
		"($Servidor + ':aurora-whisper/recebido/google-client.json')",
		// o JSON do Google: fora do repositório, com o redirect de produção
		"if (Dentro-De $JsonCheio $Raiz) {",
		"$redirect = 'https://' + $Dominio + '/auth/google/callback'",
		"[string]$Servidor = 'vps'",
	)
	exigirContem(t, gerarSegredos, ler(t, gerarSegredos), `RECEBIDO="$BASE/recebido/google-client.json"`, `BASE="${AW_BASE:-$HOME/aurora-whisper}"`)
	// As imagens são as do compose.
	exigirContem(t, enviar, c, "$ImagemApi = '"+constanteDoScript(t, restaurarSh, "IMAGEM_API")+"'",
		"$ImagemBackup = '"+constanteDoScript(t, restaurarSh, "IMAGEM_BACKUP")+"'")
	// O ssh só cria a pasta e confere as imagens: nada roda script nem sobe
	// serviço na VPS daqui.
	for _, m := range regexp.MustCompile(`Rodar-Silencioso 'ssh' @\(\$Servidor, ([^)]*)\)`).FindAllStringSubmatch(c, -1) {
		switch {
		case strings.HasPrefix(m[1], "'set -e; umask 077; mkdir"):
		case strings.HasPrefix(m[1], "('docker image inspect -f {{.Id}} '"):
		default:
			t.Errorf("comando remoto inesperado: %s", m[1])
		}
	}
	var executado strings.Builder
	for _, l := range linhasDeCodigo(ler(t, enviar)) {
		// As mensagens (Write-Host, Ok, Falhar) podem citar os scripts da VPS.
		if !strings.HasPrefix(l.texto, "Write-Host") && !strings.HasPrefix(l.texto, "Ok ") && !strings.HasPrefix(l.texto, "Falhar ") {
			executado.WriteString(l.texto + "\n")
		}
	}
	exigirNaoContem(t, enviar, executado.String(), "subir.sh", "gerar-segredos.sh", "compose up", "'bash'")
	// Nada do JSON vai para a tela.
	for _, l := range linhasDeCodigo(ler(t, enviar)) {
		if strings.HasPrefix(l.texto, "Write-Host") && regexp.MustCompile(`\$json|\$web|client_secret|ReadAllText`).MatchString(l.texto) {
			t.Errorf("linha %d mostra o JSON: %s", l.n, l.texto)
		}
	}
}

func TestPC2_ConferirSoLe(t *testing.T) {
	c := codigo(ler(t, conferirNuv))
	exigirContem(t, conferirNuv, c,
		"$Curl = Join-Path $env:SystemRoot 'System32\\curl.exe'",
		"if ($script:ErrosDoTls -ne 'None') {", "if ($dias -lt 7) {",
		"'/api/v1/health/live'", "'/api/v1/health/retencao'", "($job.Codigo -ne 404)",
		"($http.Codigo -lt 301) -or ($http.Codigo -gt 308)",
		"[System.Net.Dns]::GetHostAddresses($Dominio)",
	)
	// Só GET, com o certificado conferido: nada de --insecure, de outro
	// método, de corpo, nem de ssh, scp ou docker.
	exigirNaoContem(t, conferirNuv, c, "--insecure", "'-k'", "'-X'", "--request", "--data", "'-d'", "--upload", "'-T'",
		"'ssh'", "'scp'", "docker", "--connect-to")
}
