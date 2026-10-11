<#
.SYNOPSIS
  O teste do deploy da nuvem no Docker do PC, antes de mandar qualquer coisa
  para a VPS. Compativel com o PowerShell 5.1. Quem roda e o Mauro, com o
  Docker Desktop aberto.

.DESCRIPTION
  ADR-019, "Decisoes da F11", Parte 2. Monta as duas imagens de producao
  (aurora-whisper-server:producao e aurora-whisper-backup:producao) e sobe o
  MESMO deploy\compose.yml da VPS, com o deploy\compose.teste.yml por cima: o
  projeto aurora-whisper-teste, a nuvem de teste em
  https://aurora-whisper.exemplo.test, e um Traefik de teste (a mesma versao
  do da VPS) como borda, publicado so em 127.0.0.1. Os segredos sao de mentira,
  gerados na hora numa pasta temporaria, e o JSON do Google tambem (ninguem
  entra pelo Google neste teste). A chave do backup e uma chave de teste,
  gerada aqui: a sua chave de verdade nao e tocada.

  O que ele confere, em ordem, e para no primeiro FALHOU:
    1. o build das duas imagens;
    2. o Postgres: de pe, os cinco ajustes de log por SHOW e os dois papeis
       sem poder;
    3. o migrate: as migrations pendentes, aplicadas, e nenhuma pendente
       depois (o mesmo binario, num conteiner descartavel);
    4. a API: saudavel pelo healthcheck de vida, o log na ordem arrumacao,
       retencao, servindo (a retencao antes de a porta abrir), com o proxy
       confiavel ligado, e a saude do job (healthcheck -retencao) por docker
       exec, como no subir.sh;
    5. a borda: o health pelo https, o HSTS e a CSP do servidor (uma so), o
       Host errado dando 404 (ou 421), o http indo para o https, a rota da
       saude do job fechada por fora, o log de acesso ligado para os outros e
       DESLIGADO no nosso router, e nenhum aviso de X-Forwarded-For;
    6. o backup: uma conta de teste no banco, o backup.sh do conteiner do
       backup (o B2 e uma pasta de mentira dentro do volume do backup), o que
       subiu igual ao que ficou, e a saude do agendador;
    7. a restauracao: o backup decifrado e restaurado num banco novo, num
       conteiner descartavel; o pos-restauracao nele; e o epoch da conta
       trocado no restaurado e igual no original.
  No fim, de certo ou de errado, derruba tudo: o projeto do compose, os
  volumes dele, a rede do proxy do teste e a pasta temporaria. So toca no que
  e do teste (o projeto aurora-whisper-teste e o que leva o rotulo
  aurora-whisper-teste=1); nunca em outro conteiner, rede ou volume.

  Se tudo passou, anota os IDs das duas imagens em
  %USERPROFILE%\.aurora-whisper-dev\deploy-teste-aprovado.txt: o
  enviar-para-servidor.ps1 so manda imagens com esses IDs (o que sobe e
  exatamente o que passou aqui).

.PARAMETER PortaHttps
  A porta de 127.0.0.1 da borda de teste (https). Padrao 18443.

.PARAMETER PortaHttp
  A porta de 127.0.0.1 da borda de teste (http). Padrao 18080.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\deploy-teste.ps1
#>
[CmdletBinding()]
param(
    [ValidateRange(1024, 65535)]
    [int]$PortaHttps = 18443,

    [ValidateRange(1024, 65535)]
    [int]$PortaHttp = 18080
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

# O repositorio: o script esta em <repo>\scripts\.
$Raiz = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))

# Os nomes do teste: o prefixo e o projeto nunca sao os da VPS.
$Projeto = 'aurora-whisper-teste'
$Prefixo = 'aurora-whisper-teste'
$Rotulo = 'aurora-whisper-teste=1'
$RedeDoProxy = 'aurora-whisper-teste-web'
$RedeInterna = 'aurora-whisper-teste-interna'
$VolumeDoBackup = 'aurora-whisper-teste-backup'
$Dominio = 'aurora-whisper.exemplo.test'
$Conta = 'teste@exemplo.test'
$Bucket = 'bucket-de-teste'
$ImagemApi = 'aurora-whisper-server:producao'
$BinarioDaApi = '/usr/local/bin/aurora-whisper-server'
$ImagemBackup = 'aurora-whisper-backup:producao'
$Compose = Join-Path $Raiz 'deploy\compose.yml'
$ComposeTeste = Join-Path $Raiz 'deploy\compose.teste.yml'
$Pasta = Join-Path ([IO.Path]::GetTempPath()) 'aurora-whisper-deploy-teste'
$PastaSegredos = Join-Path $Pasta 'segredos'
$PastaChave = Join-Path $Pasta 'chave'
$ArquivoConfig = Join-Path $Pasta 'config.env'
$Aprovado = Join-Path $env:USERPROFILE '.aurora-whisper-dev\deploy-teste-aprovado.txt'
$Curl = Join-Path $env:SystemRoot 'System32\curl.exe'

# A CSP e o HSTS do servidor (internal\server\seguranca.go).
$CspDoServidor = 'default-src ''self''; base-uri ''none''; object-src ''none''; frame-ancestors ''none''; form-action ''self'''
$HstsDoServidor = 'max-age=31536000'
# O microfone so para a propria origem (ADR-021, escolha menor 18).
$PermissionsPolicyDoServidor = 'microphone=(self)'

# A conta de teste do backup e o epoch dela (UUIDs de mentira).
$IdDaConta = '01900000-0000-7000-8000-000000000001'
$EpochAntes = '01900000-0000-7000-8000-0000000000e1'
$BancoRestaurado = 'aurora_restaurado_teste'

# Roda um programa sem nunca lancar excecao: devolve o codigo de saida e o
# texto (stdout e stderr juntos). O PowerShell 5.1 transforma stderr
# redirecionado em erro quando $ErrorActionPreference e Stop; por isso a
# preferencia muda aqui dentro.
function Rodar-Silencioso([string]$Programa, [string[]]$Argumentos) {
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $linhas = @(& $Programa @Argumentos 2>&1 | ForEach-Object { $_.ToString() })
        $codigo = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $anterior
    }
    return [pscustomobject]@{ Codigo = $codigo; Texto = (($linhas -join "`n").Trim()) }
}

function Docker-Silencioso([string[]]$Argumentos) {
    return Rodar-Silencioso 'docker' $Argumentos
}

# O compose do teste: os dois arquivos, o projeto do teste e o config.env do
# teste.
function Dc([string[]]$Resto) {
    $base = @('compose', '--project-name', $Projeto, '--env-file', $ArquivoConfig, '-f', $Compose, '-f', $ComposeTeste)
    return Docker-Silencioso ($base + $Resto)
}

# Um conteiner descartavel, com o rotulo do teste (a limpeza o acha se ele
# sobrar), sem capacidades.
function Descartavel([string[]]$Resto) {
    $base = @('run', '--rm', '--label', $Rotulo, '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges')
    return Docker-Silencioso ($base + $Resto)
}

# Um psql no Postgres do teste, pelo superusuario (so o teste usa).
function Psql([string]$Banco, [string]$Sql) {
    return Docker-Silencioso @('exec', ($Prefixo + '-postgres'), 'psql', '-X', '-A', '-t', '-q', '-v', 'ON_ERROR_STOP=1',
        '-U', 'postgres', '-d', $Banco, '-c', $Sql)
}

function Falhar([string]$Mensagem) {
    throw ('FALHOU: ' + $Mensagem)
}

function Ok([string]$Mensagem) {
    Write-Host ('OK: ' + $Mensagem) -ForegroundColor Green
}

function Passo([string]$Titulo) {
    Write-Host ''
    Write-Host ('== ' + $Titulo) -ForegroundColor Cyan
}

# 32 bytes do gerador criptografico, em hexadecimal (64 caracteres), como o
# gerar-segredos.sh da VPS.
function Hex-Aleatorio([int]$Bytes) {
    $b = New-Object byte[] $Bytes
    $gerador = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $gerador.GetBytes($b)
    } finally {
        $gerador.Dispose()
    }
    return (-join ($b | ForEach-Object { $_.ToString('x2') }))
}

function Gravar([string]$Caminho, [string]$Texto) {
    [IO.File]::WriteAllText($Caminho, $Texto, (New-Object System.Text.UTF8Encoding $false))
}

# Espera o healthcheck do conteiner dizer healthy.
function Esperar-Saudavel([string]$Conteiner, [int]$Segundos) {
    for ($i = 0; $i -lt $Segundos; $i += 2) {
        $estado = Docker-Silencioso @('inspect', '-f', '{{.State.Health.Status}}', $Conteiner)
        if ($estado.Texto -eq 'healthy') { return $true }
        if ($estado.Texto -eq 'unhealthy') { return $false }
        Start-Sleep -Seconds 2
    }
    return $false
}

# O numero da primeira linha que tem o trecho, ou -1.
function Linha-Com([string[]]$Linhas, [string]$Trecho) {
    for ($i = 0; $i -lt $Linhas.Count; $i++) {
        if ($Linhas[$i].Contains($Trecho)) { return $i }
    }
    return -1
}

# Um pedido pelo curl do Windows a borda de teste, que esta em 127.0.0.1: o
# --connect-to manda o nome do teste para la, com o Host e o SNI dele. O
# --insecure e so do teste (o certificado e o padrao do Traefik); o
# conferir-nuvem.ps1 confere o de verdade. Devolve o codigo e os cabecalhos.
function Pedir([string]$Url, [string]$ConectarEm) {
    $r = Rodar-Silencioso $Curl @('--silent', '--show-error', '--insecure', '--max-time', '15',
        '--connect-to', $ConectarEm, '--dump-header', '-', '--output', 'NUL', $Url)
    $codigo = 0
    $cabecalhos = @{}
    foreach ($l in ($r.Texto -split "`n")) {
        $s = $l.Trim()
        if ($s -match '^HTTP/\S+ (\d{3})') {
            $codigo = [int]$Matches[1]
            $cabecalhos = @{}
        } elseif ($s -match '^([A-Za-z0-9-]+):\s*(.*)$') {
            $nome = $Matches[1].ToLowerInvariant()
            if (-not $cabecalhos.ContainsKey($nome)) { $cabecalhos[$nome] = @() }
            $cabecalhos[$nome] += $Matches[2]
        }
    }
    return [pscustomobject]@{ Codigo = $codigo; Cabecalhos = $cabecalhos; Saida = $r.Codigo }
}

function Cabecalho($Resposta, [string]$Nome) {
    # A virgula na frente impede o PowerShell de desembrulhar a lista: sem ela, um
    # cabecalho so vira um texto solto, e o .Count dele quebra no StrictMode.
    if ($Resposta.Cabecalhos.ContainsKey($Nome)) { return ,@($Resposta.Cabecalhos[$Nome]) }
    return ,@()
}

# Derruba tudo o que e do teste, e so o que e do teste: o projeto do compose
# (com os volumes), o que levar o rotulo do teste (os descartaveis e a rede do
# proxy do teste), e a pasta temporaria (os segredos e a chave de mentira).
function Derrubar {
    if (Test-Path -LiteralPath $ArquivoConfig) {
        $null = Dc @('down', '-v', '--remove-orphans')
    }
    foreach ($filtro in @(('label=com.docker.compose.project=' + $Projeto), ('label=' + $Rotulo))) {
        $c = Docker-Silencioso @('ps', '-a', '-q', '--filter', $filtro)
        foreach ($id in ($c.Texto -split "`n")) {
            if ($id.Trim()) { $null = Docker-Silencioso @('rm', '-f', '-v', $id.Trim()) }
        }
        $v = Docker-Silencioso @('volume', 'ls', '-q', '--filter', $filtro)
        foreach ($id in ($v.Texto -split "`n")) {
            if ($id.Trim()) { $null = Docker-Silencioso @('volume', 'rm', $id.Trim()) }
        }
        $n = Docker-Silencioso @('network', 'ls', '-q', '--filter', $filtro)
        foreach ($id in ($n.Texto -split "`n")) {
            if ($id.Trim()) { $null = Docker-Silencioso @('network', 'rm', $id.Trim()) }
        }
    }
    if (Test-Path -LiteralPath $Pasta) {
        Remove-Item -LiteralPath $Pasta -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# --- antes de tudo: o que tem de existir --------------------------------------
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host 'FALHOU: o comando "docker" nao foi achado. Abra (ou instale) o Docker Desktop e rode de novo.' -ForegroundColor Red
    exit 1
}
if ((Docker-Silencioso @('info')).Codigo -ne 0) {
    Write-Host 'FALHOU: o Docker nao esta respondendo. Abra o Docker Desktop, espere ele terminar de iniciar e rode de novo.' -ForegroundColor Red
    exit 1
}
if ((Docker-Silencioso @('compose', 'version')).Codigo -ne 0) {
    Write-Host 'FALHOU: o "docker compose" nao responde. Atualize o Docker Desktop.' -ForegroundColor Red
    exit 1
}
if (-not (Test-Path -LiteralPath $Curl -PathType Leaf)) {
    Write-Host ('FALHOU: nao achei o curl do Windows em ' + $Curl + '.') -ForegroundColor Red
    exit 1
}
foreach ($v in @('AW_PREFIXO', 'AW_SEGREDOS', 'AW_REDE_DO_PROXY', 'AW_PORTA_HTTPS_TESTE', 'AW_PORTA_HTTP_TESTE',
        'PUBLIC_BASE_URL', 'DOMINIO', 'CONTAS_PERMITIDAS', 'B2_BUCKET', 'PROXY_CONFIAVEL')) {
    # O compose le o ambiente antes do --env-file: so o config.env do teste vale.
    Remove-Item ('Env:' + $v) -ErrorAction SilentlyContinue
}

$falha = $null
$passou = $false
try {
    Passo 'Limpando sobras de um teste anterior (so as do teste)'
    Derrubar
    Ok 'nada do teste ficou de antes'

    # --- 1. as imagens ----------------------------------------------------------
    Passo 'Montando as imagens de producao (na primeira vez o Docker baixa as imagens de base)'
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & docker build -f (Join-Path $Raiz 'deploy\Dockerfile') -t $ImagemApi $Raiz
    $codigoApi = $LASTEXITCODE
    & docker build -f (Join-Path $Raiz 'deploy\backup\Dockerfile') -t $ImagemBackup (Join-Path $Raiz 'deploy\scripts')
    $codigoBackup = $LASTEXITCODE
    $ErrorActionPreference = $anterior
    if ($codigoApi -ne 0) { Falhar ('o build da ' + $ImagemApi + ' falhou (acima)') }
    if ($codigoBackup -ne 0) { Falhar ('o build da ' + $ImagemBackup + ' falhou (acima)') }
    Ok ('as imagens ' + $ImagemApi + ' e ' + $ImagemBackup)

    # --- os segredos de mentira, a chave de teste e a rede do proxy --------------
    Passo 'Gerando os segredos de mentira e a chave de teste do backup'
    $null = New-Item -ItemType Directory -Path $PastaSegredos -Force
    $null = New-Item -ItemType Directory -Path $PastaChave -Force
    $senhaAurora = Hex-Aleatorio 32
    Gravar (Join-Path $PastaSegredos 'postgres-superusuario-senha') ((Hex-Aleatorio 32) + "`n")
    Gravar (Join-Path $PastaSegredos 'postgres-aurora-senha') ($senhaAurora + "`n")
    Gravar (Join-Path $PastaSegredos 'postgres-backup-senha') ((Hex-Aleatorio 32) + "`n")
    Gravar (Join-Path $PastaSegredos 'session-secret') ((Hex-Aleatorio 32) + "`n")
    Gravar (Join-Path $PastaSegredos 'device-token-pepper') ((Hex-Aleatorio 32) + "`n")
    # A chave de cifra das chaves da IA do celular (ADR-021, C5c): o compose a
    # declara, e sem o arquivo o "docker compose up" falha. Como na VPS, 64
    # hexadecimais.
    Gravar (Join-Path $PastaSegredos 'ai-keys-secret') ((Hex-Aleatorio 32) + "`n")
    Gravar (Join-Path $PastaSegredos 'database-url') ('postgres://aurora:' + $senhaAurora + '@aurora-whisper-postgres:5432/aurora?sslmode=disable' + "`n")
    Gravar (Join-Path $PastaSegredos 'database-url-restaurada') ('postgres://aurora:' + $senhaAurora + '@aurora-whisper-postgres:5432/' + $BancoRestaurado + '?sslmode=disable' + "`n")
    # O "B2" do teste: uma pasta dentro do volume do backup (o rclone com um
    # remoto alias para o disco local, com o mesmo nome b2 do de verdade).
    Gravar (Join-Path $PastaSegredos 'rclone.conf') ("[b2]`ntype = alias`nremote = /backup/b2-de-teste`n")
    # O JSON do Google de mentira, com o redirect da nuvem de teste.
    Gravar (Join-Path $PastaSegredos 'google-client.json') ('{"web":{"client_id":"teste.apps.googleusercontent.com","client_secret":"nao-e-um-segredo","redirect_uris":["https://' + $Dominio + '/auth/google/callback"]}}' + "`n")

    $gerar = Descartavel @('--network', 'none', '--read-only', '-v', ($PastaChave + ':/chave'),
        $ImagemBackup, 'age-keygen', '-o', '/chave/teste.key')
    if ($gerar.Codigo -ne 0) { Falhar 'o age-keygen nao gerou a chave de teste' }
    $lida = Descartavel @('--network', 'none', '--read-only', '-v', ($PastaChave + ':/chave:ro'),
        $ImagemBackup, 'age-keygen', '-y', '/chave/teste.key')
    $publica = $lida.Texto.Trim()
    if (($lida.Codigo -ne 0) -or ($publica -notmatch '^age1[02-9ac-hj-np-z]{58}$')) { Falhar 'nao li a chave publica de teste' }
    Gravar (Join-Path $PastaSegredos 'backup-chave-publica') ($publica + "`n")
    Ok ('os segredos de mentira em ' + $PastaSegredos + ' (apagados no fim)')

    $rede = Docker-Silencioso @('network', 'create', '--label', $Rotulo, $RedeDoProxy)
    if ($rede.Codigo -ne 0) { Falhar ('nao consegui criar a rede ' + $RedeDoProxy) }
    $subredes = Docker-Silencioso @('network', 'inspect', $RedeDoProxy, '--format', '{{range .IPAM.Config}}{{.Subnet}} {{end}}')
    $subrede = ''
    foreach ($s in ($subredes.Texto -split ' ')) {
        if ($s -match '^\d{1,3}(\.\d{1,3}){3}/\d{1,2}$') { $subrede = $s }
    }
    if (-not $subrede) { Falhar ('nao li a sub-rede da rede ' + $RedeDoProxy) }

    # O config.env do teste: o que o gerar-segredos.sh e o subir.sh gravam na
    # VPS, mais as variaveis AW_* do teste.
    $config = @(
        ('PUBLIC_BASE_URL=https://' + $Dominio),
        ('DOMINIO=' + $Dominio),
        ('CONTAS_PERMITIDAS=' + $Conta),
        ('B2_BUCKET=' + $Bucket),
        ('PROXY_CONFIAVEL=' + $subrede),
        ('AW_PREFIXO=' + $Prefixo),
        ('AW_SEGREDOS=' + $PastaSegredos.Replace('\', '/')),
        ('AW_REDE_DO_PROXY=' + $RedeDoProxy),
        ('AW_PORTA_HTTPS_TESTE=' + $PortaHttps),
        ('AW_PORTA_HTTP_TESTE=' + $PortaHttp)
    )
    Gravar $ArquivoConfig (($config -join "`n") + "`n")
    $conferido = Dc @('config', '--quiet')
    if ($conferido.Codigo -ne 0) { Falhar ('o docker compose recusou o compose: ' + $conferido.Texto) }
    Ok ('a rede do proxy do teste (' + $subrede + ') e o compose conferido')

    # --- 2. o Postgres ------------------------------------------------------------
    Passo 'O Postgres'
    $subida = Dc @('up', '-d', 'aurora-whisper-postgres')
    if ($subida.Codigo -ne 0) { Falhar ('o compose nao subiu o Postgres: ' + $subida.Texto) }
    if (-not (Esperar-Saudavel ($Prefixo + '-postgres') 120)) { Falhar 'o Postgres nao ficou saudavel em 120 s' }
    $esperado = [ordered]@{
        'log_error_verbosity'               = 'terse'
        'log_statement'                     = 'none'
        'log_min_duration_statement'        = '-1'
        'log_parameter_max_length_on_error' = '0'
    }
    foreach ($nome in $esperado.Keys) {
        $v = Psql 'postgres' ('SHOW ' + $nome)
        if (($v.Codigo -ne 0) -or ($v.Texto -ne $esperado[$nome])) { Falhar ('SHOW ' + $nome + ' = ' + $v.Texto + ' (o esperado e ' + $esperado[$nome] + ')') }
        Ok ($nome + ' = ' + $v.Texto)
    }
    foreach ($lista in @('shared_preload_libraries', 'session_preload_libraries')) {
        $v = Psql 'postgres' ('SHOW ' + $lista)
        if (($v.Codigo -ne 0) -or $v.Texto.Contains('auto_explain')) { Falhar ($lista + ' carrega o auto_explain') }
    }
    Ok 'o auto_explain fora'
    $papel = Psql 'postgres' "SELECT concat_ws(' ', rolsuper::text, rolcreatedb::text, rolcreaterole::text, rolreplication::text, rolbypassrls::text) FROM pg_roles WHERE rolname = 'aurora'"
    if ($papel.Texto -ne 'false false false false false') { Falhar ('o papel aurora tem poder demais ou nao existe: ' + $papel.Texto) }
    $papelBackup = Psql 'postgres' "SELECT concat_ws(' ', rolsuper::text, pg_has_role(oid, 'pg_read_all_data', 'MEMBER')::text, pg_has_role(oid, 'pg_write_all_data', 'MEMBER')::text) FROM pg_roles WHERE rolname = 'aurora_backup'"
    if ($papelBackup.Texto -ne 'false true false') { Falhar ('o papel aurora_backup nao e so de leitura: ' + $papelBackup.Texto) }
    $dono = Psql 'postgres' "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'aurora'"
    if ($dono.Texto -ne 'aurora') { Falhar ('o banco aurora e de ' + $dono.Texto) }
    Ok 'os papeis aurora (sem poder, dono do banco) e aurora_backup (so leitura)'

    # --- 3. o migrate -------------------------------------------------------------
    Passo 'O migrate (o mesmo binario, num conteiner descartavel)'
    $migrar = @('--network', $RedeInterna, '--read-only',
        '-v', ((Join-Path $PastaSegredos 'database-url') + ':/run/secrets/database-url:ro'),
        '-e', 'DATABASE_URL_FILE=/run/secrets/database-url', $ImagemApi)
    $antes = Descartavel ($migrar + @('migrate', '-status'))
    if ($antes.Codigo -ne 0) { Falhar ('o migrate -status falhou: ' + $antes.Texto) }
    $pendentes = @($antes.Texto -split "`n" | Where-Object { $_ -match '^pendente ' }).Count
    if ($pendentes -lt 1) { Falhar 'o banco novo deveria ter migrations pendentes' }
    $aplicar = Descartavel ($migrar + @('migrate'))
    if ($aplicar.Codigo -ne 0) { Falhar ('o migrate falhou: ' + $aplicar.Texto) }
    $depois = Descartavel ($migrar + @('migrate', '-status'))
    if (($depois.Codigo -ne 0) -or ($depois.Texto -match '(?m)^pendente ')) { Falhar 'ainda ha migration pendente depois do migrate' }
    Ok ($pendentes.ToString() + ' migration(s) aplicada(s), nenhuma pendente')

    # --- 4. a API -------------------------------------------------------------------
    Passo 'A API e a borda de teste'
    $subida = Dc @('up', '-d', 'aurora-whisper-api', 'aurora-whisper-borda-teste')
    if ($subida.Codigo -ne 0) { Falhar ('o compose nao subiu a API e a borda: ' + $subida.Texto) }
    if (-not (Esperar-Saudavel ($Prefixo + '-api') 300)) { Falhar 'a API nao ficou saudavel em 300 s (o healthcheck de vida)' }
    $log = @((Docker-Silencioso @('logs', ($Prefixo + '-api'))).Texto -split "`n")
    $nArrumacao = Linha-Com $log '"logins_pendentes"'
    $nRetencao = Linha-Com $log '"lapides_criadas"'
    $nServindo = Linha-Com $log '"msg":"servindo"'
    if (($nArrumacao -lt 0) -or ($nRetencao -lt 0) -or ($nServindo -lt 0) -or ($nArrumacao -ge $nRetencao) -or ($nRetencao -ge $nServindo)) {
        Falhar 'o log da API nao mostra a ordem arrumacao, retencao, servindo'
    }
    if (-not $log[$nServindo].Contains('"proxy_confiavel":true')) { Falhar 'a linha servindo diz que o proxy confiavel esta desligado' }
    Ok 'a API saudavel: arrumacao, retencao e so entao servindo, com o proxy confiavel ligado'
    # A saude do job por fora do roteamento, como no subir.sh: o healthcheck
    # do compose e so o de vida (o Traefik nao roteia um conteiner unhealthy).
    $job = Docker-Silencioso @('exec', ($Prefixo + '-api'), $BinarioDaApi, 'healthcheck', '-retencao')
    if ($job.Codigo -ne 0) { Falhar ('a saude do job de retencao falhou: ' + $job.Texto) }
    Ok 'a saude do job de retencao (healthcheck -retencao, por docker exec)'

    # --- 5. a borda ---------------------------------------------------------------
    Passo 'A borda: https, cabecalhos, Host errado e o log de acesso'
    $nosso = ($Dominio + ':443:127.0.0.1:' + $PortaHttps)
    $sentinelaNossa = 'sentinela' + (Hex-Aleatorio 8)
    $sentinelaOutra = 'controle' + (Hex-Aleatorio 8)
    $sentinelaHttp = 'redirecionar' + (Hex-Aleatorio 8)
    $vida = Pedir ('https://' + $Dominio + '/api/v1/health/live') $nosso
    if ($vida.Codigo -ne 200) { Falhar ('o health pelo https deu ' + $vida.Codigo) }
    $hsts = Cabecalho $vida 'strict-transport-security'
    if (($hsts.Count -ne 1) -or ($hsts[0] -ne $HstsDoServidor)) { Falhar ('o HSTS: ' + ($hsts -join ' | ')) }
    $csp = Cabecalho $vida 'content-security-policy'
    if (($csp.Count -ne 1) -or ($csp[0] -ne $CspDoServidor)) { Falhar ('a CSP (tem de ser uma so, a do servidor): ' + ($csp -join ' | ')) }
    if ((Cabecalho $vida 'x-content-type-options') -notcontains 'nosniff') { Falhar 'falta o X-Content-Type-Options' }
    if ((Cabecalho $vida 'cache-control') -notcontains 'no-store') { Falhar 'falta o Cache-Control: no-store na API' }
    Ok 'o health pelo https, com o HSTS e uma CSP so, a do servidor'
    $painel = Pedir ('https://' + $Dominio + '/') $nosso
    if (($painel.Codigo -ne 200) -or ((Cabecalho $painel 'content-security-policy').Count -ne 1)) { Falhar ('o painel em / deu ' + $painel.Codigo) }
    Ok 'o painel em /'
    $microfone = Cabecalho $painel 'permissions-policy'
    if (($microfone.Count -ne 1) -or ($microfone[0] -ne $PermissionsPolicyDoServidor)) { Falhar ('o Permissions-Policy (tem de ser um so, o do servidor): ' + ($microfone -join ' | ')) }
    Ok 'o Permissions-Policy do servidor (microphone=(self)) chega pela borda'
    # As rotas do ditado no celular so existem com a chave de cifra (ADR-021,
    # decisao O): se o ai-keys-secret nao chegasse ao servidor, elas cairiam no
    # 404 de rota nao montada. Sem sessao, a das preferencias responde 401.
    $ditado = Pedir ('https://' + $Dominio + '/api/v1/dictation') $nosso
    if ($ditado.Codigo -ne 401) { Falhar ('as rotas do ditado no celular nao estao montadas (GET /api/v1/dictation deu ' + $ditado.Codigo + ', quero 401): a chave de cifra nao chegou ao servidor') }
    Ok 'as rotas do ditado no celular montadas (a chave de cifra chegou ao servidor)'
    # A sentinela no CAMINHO (o log de acesso sempre traz o caminho), por
    # este router: a resposta pode ser 200 (o app) ou 404; so importa que
    # passou pela borda.
    $marcado = Pedir ('https://' + $Dominio + '/' + $sentinelaNossa) $nosso
    if ($marcado.Codigo -eq 0) { Falhar 'o pedido com a sentinela nao teve resposta da borda' }
    $retencao = Pedir ('https://' + $Dominio + '/api/v1/health/retencao') $nosso
    if ($retencao.Codigo -ne 404) { Falhar ('a saude do job respondeu ' + $retencao.Codigo + ' por fora (so pelo loopback)') }
    Ok 'a saude do job fechada por fora (404)'
    $errado = Pedir ('https://outro.exemplo.test/' + $sentinelaOutra) ('outro.exemplo.test:443:127.0.0.1:' + $PortaHttps)
    if (($errado.Codigo -ne 404) -and ($errado.Codigo -ne 421)) { Falhar ('o Host errado deu ' + $errado.Codigo + ' (quero 404 ou 421)') }
    Ok ('o Host errado deu ' + $errado.Codigo)
    $http = Pedir ('http://' + $Dominio + '/' + $sentinelaHttp) ($Dominio + ':80:127.0.0.1:' + $PortaHttp)
    $local = Cabecalho $http 'location'
    if (($http.Codigo -lt 301) -or ($http.Codigo -gt 308) -or ($local.Count -ne 1) -or (-not $local[0].StartsWith('https://' + $Dominio))) {
        Falhar ('o http nao foi para o https: ' + $http.Codigo)
    }
    Ok 'o http vai para o https'
    Start-Sleep -Seconds 2
    $acesso = (Docker-Silencioso @('logs', ($Prefixo + '-borda'))).Texto
    if (-not $acesso.Contains($sentinelaOutra)) { Falhar 'o log de acesso da borda nao registrou nem o pedido de controle: o teste nao valeu' }
    if ($acesso.Contains($sentinelaNossa)) { Falhar 'o log de acesso registrou um pedido do nosso router (o accessLogs=false nao valeu)' }
    Ok 'o log de acesso ligado para os outros e desligado no nosso router'
    if ($acesso.Contains($sentinelaHttp)) {
        Write-Host 'AVISO: o redirecionamento do http (porta 80) aparece no log de acesso; o callback do Google vem sempre por https.' -ForegroundColor Yellow
    } else {
        Ok 'o redirecionamento do http tambem fora do log de acesso'
    }
    $logApi = (Docker-Silencioso @('logs', ($Prefixo + '-api'))).Texto
    if ($logApi.Contains('X-Forwarded-For de quem')) { Falhar 'a API avisou de X-Forwarded-For de fora do PROXY_CONFIAVEL' }
    Ok 'nenhum aviso de X-Forwarded-For (o PROXY_CONFIAVEL e a rede do proxy)'

    # --- 6. o backup ----------------------------------------------------------------
    Passo 'O backup (o B2 de mentira e uma pasta no volume do backup)'
    $contaCriada = Psql 'aurora' ("INSERT INTO users (id, google_sub, email) VALUES ('" + $IdDaConta + "', 'sub-de-teste', '" + $Conta + "'); " +
        "INSERT INTO user_sync_counters (user_id, sync_epoch) VALUES ('" + $IdDaConta + "', '" + $EpochAntes + "')")
    if ($contaCriada.Codigo -ne 0) { Falhar ('nao consegui criar a conta de teste: ' + $contaCriada.Texto) }
    $subida = Dc @('up', '-d', 'aurora-whisper-backup')
    if ($subida.Codigo -ne 0) { Falhar ('o compose nao subiu o backup: ' + $subida.Texto) }
    $saude = Docker-Silencioso @('exec', ($Prefixo + '-backup'), 'agendar-backup.sh', 'saude')
    if ($saude.Codigo -eq 0) { Falhar 'a saude do backup passou sem nenhum backup' }
    $feito = Docker-Silencioso @('exec', ($Prefixo + '-backup'), 'backup.sh')
    if ($feito.Codigo -ne 0) { Falhar ('o backup.sh falhou: ' + $feito.Texto) }
    if (-not ($feito.Texto -match 'backup: OK \((aurora-whisper-\d{8}T\d{6}Z\.dump\.age), (\d+) bytes')) {
        Falhar ('o backup.sh nao disse o nome do backup: ' + $feito.Texto)
    }
    $nomeDoBackup = $Matches[1]
    $remoto = '/backup/b2-de-teste/' + $Bucket + '/aurora-whisper/' + $nomeDoBackup
    $igual = Docker-Silencioso @('exec', ($Prefixo + '-backup'), 'cmp', ('/backup/' + $nomeDoBackup), $remoto)
    if ($igual.Codigo -ne 0) { Falhar 'o que subiu ao B2 de mentira nao e igual ao que ficou no volume' }
    $saude = Docker-Silencioso @('exec', ($Prefixo + '-backup'), 'agendar-backup.sh', 'saude')
    if ($saude.Codigo -ne 0) { Falhar ('a saude do backup nao passou depois do backup: ' + $saude.Texto) }
    Ok ('o backup ' + $nomeDoBackup + ' subiu, igual ao do volume, e a saude do agendador passou')

    # --- 7. a restauracao -----------------------------------------------------------
    Passo 'A restauracao num banco novo, com o epoch novo'
    $criar = Psql 'postgres' ('CREATE DATABASE ' + $BancoRestaurado + ' OWNER aurora')
    if ($criar.Codigo -ne 0) { Falhar ('nao consegui criar o banco ' + $BancoRestaurado) }
    $restaurar = 'set -euo pipefail; export PGPASSWORD=$(cat /run/secrets/postgres-aurora-senha); ' +
        'age --decrypt --identity /chave/teste.key ' + $remoto + ' | pg_restore -h aurora-whisper-postgres -U aurora -d ' + $BancoRestaurado +
        ' --no-owner --no-privileges --exit-on-error --single-transaction'
    $restaurado = Descartavel @('--network', $RedeInterna, '--read-only',
        '-v', ($PastaChave + ':/chave:ro'),
        '-v', ((Join-Path $PastaSegredos 'postgres-aurora-senha') + ':/run/secrets/postgres-aurora-senha:ro'),
        '-v', ($VolumeDoBackup + ':/backup:ro'),
        $ImagemBackup, 'bash', '-c', $restaurar)
    if ($restaurado.Codigo -ne 0) { Falhar ('a restauracao falhou: ' + $restaurado.Texto) }
    $migracoes = Psql 'aurora' 'SELECT count(*) FROM schema_migrations'
    $migracoesRestauradas = Psql $BancoRestaurado 'SELECT count(*) FROM schema_migrations'
    if (($migracoesRestauradas.Codigo -ne 0) -or ($migracoesRestauradas.Texto -ne $migracoes.Texto)) { Falhar 'o banco restaurado nao tem as mesmas migrations' }
    $urlRestaurada = (Join-Path $PastaSegredos 'database-url-restaurada') + ':/run/secrets/database-url:ro'
    $pos = Descartavel @('--network', $RedeInterna, '--read-only', '-v', $urlRestaurada,
        '-e', 'DATABASE_URL_FILE=/run/secrets/database-url', $ImagemApi, 'pos-restauracao', '-confirmo')
    if (($pos.Codigo -ne 0) -or ($pos.Texto -notmatch 'pos-restauracao: 1 conta\(s\) com o epoch novo')) { Falhar ('o pos-restauracao: ' + $pos.Texto) }
    $epochOriginal = Psql 'aurora' ("SELECT sync_epoch::text FROM user_sync_counters WHERE user_id = '" + $IdDaConta + "'")
    $epochRestaurado = Psql $BancoRestaurado ("SELECT sync_epoch::text FROM user_sync_counters WHERE user_id = '" + $IdDaConta + "'")
    if ($epochOriginal.Texto -ne $EpochAntes) { Falhar 'o epoch do banco original mudou (o pos-restauracao rodou no banco errado?)' }
    if (($epochRestaurado.Texto -notmatch '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') -or ($epochRestaurado.Texto -eq $EpochAntes)) {
        Falhar 'o epoch da conta nao mudou no banco restaurado'
    }
    Ok 'o backup decifrou e restaurou num banco novo, e o pos-restauracao trocou o epoch da conta so nele'

    # --- tudo certo: as imagens aprovadas ------------------------------------------
    $ids = @()
    foreach ($img in @($ImagemApi, $ImagemBackup)) {
        $id = Docker-Silencioso @('image', 'inspect', '-f', '{{.Id}}', $img)
        if ($id.Codigo -ne 0) { Falhar ('nao li o ID da imagem ' + $img) }
        $ids += ($img + ' ' + $id.Texto.Trim())
    }
    $pastaAprovado = Split-Path -Parent $Aprovado
    if (-not (Test-Path -LiteralPath $pastaAprovado)) { $null = New-Item -ItemType Directory -Path $pastaAprovado }
    $linhasAprovado = @('# deploy-teste.ps1 aprovado em ' + [DateTime]::UtcNow.ToString("yyyy-MM-dd'T'HH:mm:ss'Z'")) + $ids
    Gravar $Aprovado (($linhasAprovado -join "`n") + "`n")
    $passou = $true
} catch {
    $falha = $_.Exception.Message
    if ($falha -notlike 'FALHOU:*') { $falha = 'FALHOU: ' + $falha }
    Write-Host ''
    Write-Host 'As ultimas linhas do log da API e da borda de teste (so contagens e os pedidos do teste):'
    foreach ($c in @(($Prefixo + '-api'), ($Prefixo + '-borda'))) {
        $l = Docker-Silencioso @('logs', '--tail', '15', $c)
        if ($l.Codigo -eq 0) { Write-Host $l.Texto }
    }
} finally {
    Passo 'Derrubando tudo o que e do teste'
    Derrubar
}

Write-Host ''
if ($passou) {
    Write-Host 'OK: o deploy passou no Docker do PC, e tudo o que era do teste foi derrubado.' -ForegroundColor Green
    Write-Host ('As imagens aprovadas ficaram anotadas em ' + $Aprovado + ': o enviar-para-servidor.ps1 manda so elas.')
    exit 0
}
Write-Host $falha -ForegroundColor Red
Write-Host 'Tudo o que era do teste foi derrubado. Nada foi mandado para a VPS.'
exit 1
