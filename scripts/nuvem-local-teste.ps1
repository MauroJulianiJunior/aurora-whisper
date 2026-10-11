<#
.SYNOPSIS
  Sobe a nuvem de TESTE no PC, para ver o app da nuvem (ADR-019, F4) sem
  Docker e sem Google. Compativel com o PowerShell 5.1. Quem roda e o Mauro.

.DESCRIPTION
  Num comando so:
    1. confere que o go e o pnpm existem; se faltar web\node_modules, roda o
       pnpm install --frozen-lockfile;
    2. compila o painel no modo nuvem em
       %USERPROFILE%\.aurora-whisper-dev\painel-nuvem (fora do repositorio e
       fora de bin\, que e do painel do PC);
    3. sobe o servidor de teste (go run -tags logindeteste ./cmd/server teste):
       guarda tudo na memoria, nao tem login do Google, entra so com a conta de
       teste (voce@teste.local) e ja abre com ditados de exemplo inventados.

  O servidor escuta so na propria maquina (127.0.0.1). As chaves dele nascem
  dentro do processo e nunca aparecem na tela. Tudo some ao desligar (Ctrl+C).
  Roteiros: docs\teste-manual-nuvem-f4.md (o app no navegador),
  docs\teste-manual-nuvem-f5.md (o app instalado no PC e no Android),
  docs\teste-manual-nuvem-f7.md (o Agent do bin\ ligado a esta nuvem) e
  docs\teste-manual-nuvem-f8.md (o aprendido, do PC ao app e de volta).

.PARAMETER Porta
  A porta do servidor. Padrao: 8080.

.EXAMPLE
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\nuvem-local-teste.ps1

.EXAMPLE
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\nuvem-local-teste.ps1 -Porta 8081
#>
[CmdletBinding()]
param(
    [ValidateRange(1024, 65535)]
    [int]$Porta = 8080,

    # Sobe a nuvem vazia (so a conta de teste), sem os ditados de exemplo. Use
    # quando conectar o Agent do bin\: tudo o que a nuvem tem desce para o
    # historico dele, e os exemplos inventados ficariam la (roteiro da F7).
    [switch]$SemExemplos
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$Raiz = Split-Path -Parent $PSScriptRoot
$Web = Join-Path $Raiz 'web'
$Pasta = Join-Path $env:USERPROFILE '.aurora-whisper-dev'
$Painel = Join-Path $Pasta 'painel-nuvem'

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('ERRO: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

# 1. As ferramentas.
foreach ($Ferramenta in @('go', 'pnpm')) {
    if (-not (Get-Command $Ferramenta -ErrorAction SilentlyContinue)) {
        Falhar ('Nao achei o ' + $Ferramenta + ' no PATH. Instale e abra uma janela nova do PowerShell.')
    }
}
if (-not (Test-Path -LiteralPath (Join-Path $Web 'node_modules'))) {
    Write-Host 'Instalando as dependencias do painel (pnpm install)...'
    & pnpm -C $Web install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) {
        Falhar 'O pnpm install falhou (a mensagem esta acima).'
    }
}

# 2. A porta, antes de compilar: se ela estiver ocupada, nao adianta.
$Ocupada = Get-NetTCPConnection -State Listen -LocalPort $Porta -ErrorAction SilentlyContinue
if ($Ocupada) {
    Falhar ('A porta ' + $Porta + ' ja esta em uso. Feche o programa que a usa, ou rode de novo com -Porta 8081.')
}

# 3. O painel no modo nuvem.
if (-not (Test-Path -LiteralPath $Pasta)) {
    New-Item -ItemType Directory -Path $Pasta | Out-Null
}
Write-Host 'Compilando o painel no modo nuvem...'
& pnpm -C $Web exec vite build --mode nuvem --outDir $Painel --emptyOutDir --logLevel warn
if ($LASTEXITCODE -ne 0) {
    Falhar 'O build do painel falhou (a mensagem esta acima).'
}

# 4. O servidor de teste. As variaveis vivem so neste processo.
$env:PAINEL_DIR = $Painel
$env:HTTP_ADDR = '127.0.0.1:' + $Porta
if ($SemExemplos) {
    $env:TESTE_SEM_EXEMPLOS = '1'
    Write-Host 'Sem os ditados de exemplo: a nuvem sobe vazia, so com a conta de teste.'
}

Write-Host ''
Write-Host 'Subindo a nuvem de teste. Quando aparecer "Nuvem de teste em", abra no Chrome:' -ForegroundColor Green
Write-Host ''
Write-Host ('  http://localhost:' + $Porta) -ForegroundColor Green
Write-Host ''
Write-Host 'Escreva localhost (e nao 127.0.0.1). Para desligar: Ctrl+C nesta janela.'
Write-Host ''
& go -C $Raiz run -tags logindeteste ./cmd/server teste
