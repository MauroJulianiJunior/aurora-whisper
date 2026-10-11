<#
.SYNOPSIS
  Confere a nuvem de FORA, pela internet, como o celular e o PC a veem: o DNS,
  o certificado, o https, os cabecalhos e o health. Compativel com o
  PowerShell 5.1. So le: nada muda no servidor.

.DESCRIPTION
  ADR-019, "Decisoes da F11", Parte 2. Depois do subir.sh na VPS (e quando
  quiser, depois). O que ele confere, e para no primeiro FALHOU:
    1. o DNS: o nome aponta para algum endereco (e para o -IPEsperado, se
       dado), sem proxy na frente;
    2. o certificado: valido para o nome, conferido pelo Windows (a cadeia, o
       nome e a data), e com mais de 7 dias pela frente (avisa com menos de 20:
       o Let's Encrypt renova com 30);
    3. o http vai para o https;
    4. o health pelo https (200), com o HSTS de um ano, uma CSP so (a do
       servidor: o Traefik nao acrescenta outra), o nosniff, o no-referrer e o
       no-store;
    5. o painel em / (200);
    6. a saude do job fechada por fora (404: ela so responde ao loopback).
  So faz GET e o aperto de mao do TLS. Usa o curl do Windows
  (C:\Windows\System32\curl.exe), sem --insecure: um certificado que nao vale
  e FALHOU.

.PARAMETER Dominio
  O endereco da nuvem, sem https://.

.PARAMETER IPEsperado
  O IP da VPS, para conferir o DNS (opcional).

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio aurora-whisper.exemplo.com.br -IPEsperado 203.0.113.10
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Dominio,

    [string]$IPEsperado = ''
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$Curl = Join-Path $env:SystemRoot 'System32\curl.exe'
# A CSP e o HSTS do servidor (internal\server\seguranca.go).
$CspDoServidor = 'default-src ''self''; base-uri ''none''; object-src ''none''; frame-ancestors ''none''; form-action ''self'''
$HstsDoServidor = 'max-age=31536000'

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

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('FALHOU: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

function Ok([string]$Mensagem) {
    Write-Host ('OK: ' + $Mensagem) -ForegroundColor Green
}

# Um GET pelo curl do Windows, sem seguir redirecionamento, com o certificado
# conferido. Devolve o codigo e os cabecalhos.
function Pedir([string]$Url) {
    $r = Rodar-Silencioso $Curl @('--silent', '--show-error', '--max-time', '15', '--ssl-revoke-best-effort',
        '--dump-header', '-', '--output', 'NUL', $Url)
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
    return [pscustomobject]@{ Codigo = $codigo; Cabecalhos = $cabecalhos; Saida = $r.Codigo; Texto = $r.Texto }
}

function Cabecalho($Resposta, [string]$Nome) {
    # A virgula na frente impede o PowerShell de desembrulhar a lista: sem ela, um
    # cabecalho so vira um texto solto, e o .Count dele quebra no StrictMode.
    if ($Resposta.Cabecalhos.ContainsKey($Nome)) { return ,@($Resposta.Cabecalhos[$Nome]) }
    return ,@()
}

if ($Dominio -notmatch '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$') {
    Falhar 'o -Dominio e o endereco em minusculas, sem https:// e sem barra.'
}
if (-not (Test-Path -LiteralPath $Curl -PathType Leaf)) {
    Falhar ('nao achei o curl do Windows em ' + $Curl + '.')
}

# --- 1. o DNS -------------------------------------------------------------------
try {
    $enderecos = @([System.Net.Dns]::GetHostAddresses($Dominio) | ForEach-Object { $_.ToString() })
} catch {
    Falhar ('o nome ' + $Dominio + ' nao resolve no DNS.')
}
if ($enderecos.Count -eq 0) { Falhar ('o nome ' + $Dominio + ' nao resolve no DNS.') }
if ($IPEsperado -and -not ($enderecos -contains $IPEsperado)) {
    Falhar ('o DNS de ' + $Dominio + ' aponta para ' + ($enderecos -join ', ') + ', e nao para ' + $IPEsperado + ' (um proxy na frente leria o texto).')
}
Ok ('o DNS: ' + $Dominio + ' -> ' + ($enderecos -join ', '))

# --- 2. o certificado -------------------------------------------------------------
$script:ErrosDoTls = 'nao conferido'
$validar = [System.Net.Security.RemoteCertificateValidationCallback] {
    param($remetente, $certificado, $cadeia, $erros)
    $script:ErrosDoTls = $erros.ToString()
    return $true
}
$tcp = New-Object System.Net.Sockets.TcpClient
$certificado = $null
try {
    $tcp.Connect($Dominio, 443)
    $tls = New-Object System.Net.Security.SslStream($tcp.GetStream(), $false, $validar)
    try {
        $tls.AuthenticateAsClient($Dominio, $null, [System.Security.Authentication.SslProtocols]::Tls12, $true)
        $certificado = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($tls.RemoteCertificate)
    } finally {
        $tls.Dispose()
    }
} catch {
    Falhar ('o aperto de mao do TLS com ' + $Dominio + ':443 falhou: ' + $_.Exception.Message)
} finally {
    $tcp.Dispose()
}
if ($script:ErrosDoTls -ne 'None') {
    Falhar ('o certificado de ' + $Dominio + ' nao vale para o Windows (' + $script:ErrosDoTls + '). Emitido por: ' + $certificado.Issuer)
}
$dias = [int][Math]::Floor(($certificado.NotAfter.ToUniversalTime() - [DateTime]::UtcNow).TotalDays)
if ($dias -lt 7) { Falhar ('o certificado vence em ' + $dias + ' dia(s): a renovacao do Traefik parou?') }
Ok ('o certificado vale para ' + $Dominio + ', emitido por ' + $certificado.Issuer + ', por mais ' + $dias + ' dia(s)')
if ($dias -lt 20) {
    Write-Host ('AVISO: faltam ' + $dias + ' dias; o Let''s Encrypt renova com 30. Confira a renovacao do Traefik.') -ForegroundColor Yellow
}

# --- 3. o http vai para o https ---------------------------------------------------
$http = Pedir ('http://' + $Dominio + '/')
$local = Cabecalho $http 'location'
if (($http.Codigo -lt 301) -or ($http.Codigo -gt 308) -or ($local.Count -ne 1) -or (-not $local[0].StartsWith('https://' + $Dominio + '/'))) {
    Falhar ('o http nao manda para o https (' + $http.Codigo + ').')
}
Ok 'o http manda para o https'

# --- 4. o health e os cabecalhos ----------------------------------------------------
$vida = Pedir ('https://' + $Dominio + '/api/v1/health/live')
if ($vida.Codigo -ne 200) { Falhar ('o health pelo https deu ' + $vida.Codigo + ' ' + $vida.Texto) }
$hsts = Cabecalho $vida 'strict-transport-security'
if (($hsts.Count -ne 1) -or ($hsts[0] -ne $HstsDoServidor)) { Falhar ('o HSTS: ' + ($hsts -join ' | ')) }
$csp = Cabecalho $vida 'content-security-policy'
if (($csp.Count -ne 1) -or ($csp[0] -ne $CspDoServidor)) { Falhar ('a CSP (tem de ser uma so, a do servidor): ' + ($csp -join ' | ')) }
if ((Cabecalho $vida 'x-content-type-options') -notcontains 'nosniff') { Falhar 'falta o X-Content-Type-Options: nosniff' }
if ((Cabecalho $vida 'referrer-policy') -notcontains 'no-referrer') { Falhar 'falta o Referrer-Policy: no-referrer' }
if ((Cabecalho $vida 'cache-control') -notcontains 'no-store') { Falhar 'falta o Cache-Control: no-store na API' }
Ok 'o health pelo https: 200, o HSTS de um ano, uma CSP so (a do servidor), nosniff, no-referrer e no-store'

# --- 5. o painel ----------------------------------------------------------------
$painel = Pedir ('https://' + $Dominio + '/')
if (($painel.Codigo -ne 200) -or ((Cabecalho $painel 'content-security-policy').Count -ne 1)) { Falhar ('o painel em / deu ' + $painel.Codigo) }
Ok 'o painel em /'

# --- 6. a saude do job, fechada por fora --------------------------------------------
$job = Pedir ('https://' + $Dominio + '/api/v1/health/retencao')
if ($job.Codigo -ne 404) { Falhar ('a saude do job respondeu ' + $job.Codigo + ' de fora (ela so responde ao loopback).') }
Ok 'a saude do job fechada por fora (404)'

Write-Host ''
Write-Host ('OK: a nuvem em https://' + $Dominio + ' responde certo de fora. Nada foi mudado no servidor.') -ForegroundColor Green
