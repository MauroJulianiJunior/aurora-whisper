<#
.SYNOPSIS
  Remonta o Agent do bin\ e o painel dele, e abre o Agent novo. Compativel com
  o PowerShell 5.1. Quem roda e o Mauro (ou quem monta do codigo).

.DESCRIPTION
  Num comando so, na ordem certa:
    1. fecha o Agent aberto como o "Sair" da bandeja (taskkill sem /F) e espera
       ele sair, ate 30 s. Recompilar com o Agent aberto deixa o antigo rodando
       sem aviso: o exe novo e gravado, mas o Agent novo desiste em silencio,
       porque so pode haver um;
    2. compila o Agent em bin\aurora-whisper-agent.exe;
    3. monta o painel em bin\painel (o pnpm build). E passo proprio e facil de
       esquecer: sem ele, o painel continua o de antes;
    4. abre o Agent novo e confere pelo StartTime que e ele que esta rodando.

  Nao mexe nos dados (o historico e as configuracoes ficam no bin\, ao lado do
  exe) nem na nuvem.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\remontar-agent.ps1
#>
[CmdletBinding()]
param()

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$Raiz = Split-Path -Parent $PSScriptRoot
$Web = Join-Path $Raiz 'web'
$Exe = Join-Path $Raiz 'bin\aurora-whisper-agent.exe'

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('FALHOU: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

function Ok([string]$Mensagem) {
    Write-Host ('OK: ' + $Mensagem) -ForegroundColor Green
}

foreach ($Ferramenta in @('go', 'pnpm')) {
    if (-not (Get-Command $Ferramenta -ErrorAction SilentlyContinue)) {
        Falhar ('nao achei o ' + $Ferramenta + ' no PATH')
    }
}
if (-not (Test-Path (Join-Path $Web 'node_modules'))) {
    Write-Host 'Instalando as dependencias do painel (pnpm install)...'
    & pnpm -C $Web install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { Falhar 'o pnpm install falhou (a mensagem esta acima)' }
}

# 1. Fechar o Agent aberto, como o Sair da bandeja.
if (Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue) {
    & taskkill /IM aurora-whisper-agent.exe | Out-Null
    $Prazo = (Get-Date).AddSeconds(30)
    while ((Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue) -and ((Get-Date) -lt $Prazo)) {
        Start-Sleep -Milliseconds 500
    }
    if (Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue) {
        Falhar 'o Agent nao fechou em 30 s. Feche pela bandeja (Sair) e rode de novo.'
    }
    Ok 'o Agent antigo fechou'
} else {
    Ok 'nenhum Agent estava aberto'
}

# 2. Compilar o Agent.
Write-Host 'Compilando o Agent...'
& go -C $Raiz build -ldflags '-H=windowsgui' -o 'bin/aurora-whisper-agent.exe' ./cmd/agent
if ($LASTEXITCODE -ne 0) { Falhar 'a compilacao do Agent falhou (a mensagem esta acima). Nada foi aberto.' }
Ok 'o Agent compilado em bin\aurora-whisper-agent.exe'

# 3. Montar o painel em bin\painel.
Write-Host 'Montando o painel...'
& pnpm -C $Web build
if ($LASTEXITCODE -ne 0) { Falhar 'a montagem do painel falhou (a mensagem esta acima). Nada foi aberto.' }
Ok 'o painel montado em bin\painel'

# 4. Abrir o Agent novo e conferir que e ele.
$Antes = Get-Date
Start-Process -FilePath $Exe
Start-Sleep -Seconds 3
$Novo = Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $Novo) {
    Falhar 'o Agent novo nao ficou aberto. Veja o fim do agent.log em bin\.'
}
if ($Novo.StartTime -lt $Antes.AddSeconds(-2)) {
    Falhar ('o Agent aberto e o antigo (StartTime ' + $Novo.StartTime.ToString('HH:mm:ss') + '). Feche pela bandeja e rode de novo.')
}
Ok ('o Agent novo esta rodando desde ' + $Novo.StartTime.ToString('HH:mm:ss'))
Write-Host ''
Write-Host 'Se o painel estava aberto, feche a aba e abra de novo pela bandeja: o painel novo so aparece numa aba nova.'
