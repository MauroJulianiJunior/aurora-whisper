<#
.SYNOPSIS
  Sobe a nuvem do Aurora Whisper no PC, para o teste manual da F3 (ADR-019).
  Compativel com o PowerShell 5.1. Quem roda e o Mauro.

.DESCRIPTION
  Junta num comando so os passos do docs/teste-manual-nuvem-f3.md:
    1. confere que o Postgres de teste esta ligado (scripts/postgres-de-teste.ps1);
    2. acha o JSON do client Google de desenvolvimento em
       %USERPROFILE%\.aurora-whisper-dev\google-client-dev.json. Se ele nao
       estiver la e houver UM client_secret_*.json em Downloads, move esse
       arquivo para la (fora do repositorio);
    3. roda o migrate no banco de teste;
    4. gera na hora as duas chaves do servidor (SESSION_SECRET e
       DEVICE_TOKEN_PEPPER), que valem so enquanto este script roda;
    5. compila o painel no modo nuvem (o app, F4) em
       %USERPROFILE%\.aurora-whisper-dev\painel-nuvem (PAINEL_DIR);
    6. sobe o servidor em http://localhost:8080, com o seu e-mail na lista.

  Sem o client Google, use o scripts\nuvem-local-teste.ps1 (sem Docker e sem
  Google, com a conta de teste).

  Nada disto vai para o repositorio, para o chat ou para o log: as variaveis
  vivem so no processo deste script e somem quando ele termina (Ctrl+C).

.PARAMETER Email
  A conta Google que pode entrar (so ela).

.EXAMPLE
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\nuvem-local.ps1 -Email voce@gmail.com
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Email
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$Raiz = Split-Path -Parent $PSScriptRoot
$Pasta = Join-Path $env:USERPROFILE '.aurora-whisper-dev'
$ArquivoDoBanco = Join-Path $Pasta 'postgres-de-teste.url'
$ArquivoDoGoogle = Join-Path $Pasta 'google-client-dev.json'

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('ERRO: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

# 1. O banco de teste.
if (-not (Test-Path -LiteralPath $ArquivoDoBanco)) {
    Falhar ('O Postgres de teste nao esta ligado. Abra o Docker Desktop, espere "Engine running" e rode:' +
        [Environment]::NewLine + '  powershell -File ' + $Raiz + '\scripts\postgres-de-teste.ps1')
}

# 2. O JSON do Google.
if (-not (Test-Path -LiteralPath $ArquivoDoGoogle)) {
    $baixados = @(Get-ChildItem -LiteralPath (Join-Path $env:USERPROFILE 'Downloads') -Filter 'client_secret_*.json' -ErrorAction SilentlyContinue)
    if ($baixados.Count -eq 1) {
        Move-Item -LiteralPath $baixados[0].FullName -Destination $ArquivoDoGoogle
        Write-Host ('Movi o JSON do Google de Downloads para ' + $ArquivoDoGoogle + ' (fora do projeto).')
    } elseif ($baixados.Count -gt 1) {
        Falhar ('Ha ' + $baixados.Count + ' arquivos client_secret_*.json em Downloads e nao sei qual e o certo.' +
            [Environment]::NewLine + 'Mova o do client "dev localhost" para ' + $ArquivoDoGoogle + ' e rode de novo.')
    } else {
        Falhar ('Falta o JSON do client Google de desenvolvimento.' +
            [Environment]::NewLine + 'Crie o client no Google Cloud (Aplicativo da Web, redirect http://localhost:8080/auth/google/callback),' +
            [Environment]::NewLine + 'baixe o JSON (ele vai para Downloads) e rode este script de novo.')
    }
}

$Email = $Email.Trim()
if ($Email -notmatch '^[^@\s]+@[^@\s]+\.[^@\s]+$') {
    Falhar 'O -Email nao parece um e-mail. Exemplo: -Email voce@gmail.com'
}

# 3. O migrate.
$env:DATABASE_URL = (Get-Content -LiteralPath $ArquivoDoBanco -Raw).Trim()
Write-Host 'Preparando o banco (migrate)...'
& go -C $Raiz run ./cmd/server migrate
if ($LASTEXITCODE -ne 0) {
    Falhar 'O migrate falhou (a mensagem esta acima).'
}

# 4. As chaves do servidor: 32 bytes aleatorios cada, diferentes entre si.
$gerador = New-Object System.Security.Cryptography.RNGCryptoServiceProvider
$bytes = New-Object byte[] 32
$gerador.GetBytes($bytes)
$env:SESSION_SECRET = [Convert]::ToBase64String($bytes)
$gerador.GetBytes($bytes)
$env:DEVICE_TOKEN_PEPPER = [Convert]::ToBase64String($bytes)
$gerador.Dispose()

# 5. O app: o painel no modo nuvem (F4), fora do repositorio e fora de bin\.
$Web = Join-Path $Raiz 'web'
$Painel = Join-Path $Pasta 'painel-nuvem'
if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) {
    Falhar 'Nao achei o pnpm no PATH (ele compila o painel).'
}
if (-not (Test-Path -LiteralPath (Join-Path $Web 'node_modules'))) {
    Write-Host 'Instalando as dependencias do painel (pnpm install)...'
    & pnpm -C $Web install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) {
        Falhar 'O pnpm install falhou (a mensagem esta acima).'
    }
}
Write-Host 'Compilando o painel no modo nuvem...'
& pnpm -C $Web exec vite build --mode nuvem --outDir $Painel --emptyOutDir --logLevel warn
if ($LASTEXITCODE -ne 0) {
    Falhar 'O build do painel falhou (a mensagem esta acima).'
}
$env:PAINEL_DIR = $Painel

# 6. O servidor.
$env:GOOGLE_CLIENT_SECRET_FILE = $ArquivoDoGoogle
$env:CONTAS_PERMITIDAS = $Email
$env:PUBLIC_BASE_URL = 'http://localhost:8080'

Write-Host ''
Write-Host 'Subindo a nuvem. Quando aparecer a linha "servindo", abra no Chrome:' -ForegroundColor Green
Write-Host ''
Write-Host '  http://localhost:8080' -ForegroundColor Green
Write-Host ''
Write-Host 'Escreva localhost (e nao 127.0.0.1). Para desligar: Ctrl+C nesta janela.'
Write-Host ''
& go -C $Raiz run ./cmd/server serve
