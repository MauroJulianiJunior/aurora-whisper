<#
.SYNOPSIS
  Manda para a VPS as imagens que passaram no deploy-teste.ps1, a pasta
  deploy\ e (na primeira vez) o JSON do client do Google. Compativel com o
  PowerShell 5.1. Quem roda e o Mauro, com o Docker Desktop aberto.

.DESCRIPTION
  ADR-019, "Decisoes da F11", D4: a imagem testada no PC vai por ssh (docker
  save, e docker load na VPS), sem registro e sem git push. O que sobe e
  exatamente o que passou no teste: o script recusa mandar imagens com IDs
  diferentes dos que o deploy-teste.ps1 anotou em
  %USERPROFILE%\.aurora-whisper-dev\deploy-teste-aprovado.txt.

  O que ele faz, em ordem (para no primeiro FALHOU):
    1. confere o Docker, o ssh e o scp do Windows, e as duas imagens
       aprovadas;
    2. confere o JSON do Google, se for mandado (o client Web, com o redirect
       https://<dominio>/auth/google/callback), sem mostrar nada dele, e
       recusa um JSON dentro do repositorio;
    3. na VPS, cria a pasta ~/aurora-whisper (0700) e a recebido/ dentro dela;
    4. docker save das duas imagens | ssh <servidor> docker load (pelo cmd,
       que passa os bytes sem mexer; o cano do PowerShell 5.1 os estragaria);
    5. confere que as duas imagens estao na VPS;
    6. scp da pasta deploy\ para ~/aurora-whisper/deploy;
    7. scp do JSON do Google para ~/aurora-whisper/recebido/google-client.json,
       que o gerar-segredos.sh confere e move para segredos/.
  Ele nao sobe nada na VPS (isso e o subir.sh, la) e nunca ve segredo da VPS.

.PARAMETER Dominio
  O endereco da nuvem, sem https:// (o do redirect do Google).

.PARAMETER ArquivoGoogle
  O JSON do client Web do Google, baixado do Google Cloud. So na primeira vez
  (o gerar-segredos.sh o move para segredos/). Numa imagem nova, fica de fora;
  se for junto, o subir.sh ve que e o mesmo e apaga a copia de recebido/. Para
  trocar o client depois, o roteiro manda o JSON sozinho, sem as imagens, e o
  subir.sh o troca.

.PARAMETER Servidor
  O apelido do seu acesso ssh a VPS (o Host do ~/.ssh/config). Padrao: vps.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\enviar-para-servidor.ps1 -Dominio aurora-whisper.exemplo.com.br -ArquivoGoogle C:\Users\voce\Downloads\client_secret.json
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Dominio,

    [string]$ArquivoGoogle = '',

    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]*$')]
    [string]$Servidor = 'vps'
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$Raiz = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$ImagemApi = 'aurora-whisper-server:producao'
$ImagemBackup = 'aurora-whisper-backup:producao'
$Aprovado = Join-Path $env:USERPROFILE '.aurora-whisper-dev\deploy-teste-aprovado.txt'
$MaxJsonDoGoogle = 65536

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

# Roda a vista (o progresso aparece), sem lancar excecao; devolve o codigo.
function Rodar-AVista([string]$Programa, [string[]]$Argumentos) {
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        # O Out-Host mostra a saida e a tira do valor da funcao: sem ele, o
        # "Loaded image: ..." do docker load voltava junto com o codigo, e o
        # "-ne 0" dava falha com tudo certo (a primeira rodada do Mauro).
        & $Programa @Argumentos | Out-Host
        $codigo = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $anterior
    }
    return $codigo
}

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('FALHOU: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

function Ok([string]$Mensagem) {
    Write-Host ('OK: ' + $Mensagem) -ForegroundColor Green
}

function Dentro-De([string]$Caminho, [string]$Pasta) {
    $c = $Caminho.TrimEnd('\') + '\'
    $p = $Pasta.TrimEnd('\') + '\'
    return $c.StartsWith($p, [StringComparison]::OrdinalIgnoreCase)
}

# --- 1. o que tem de existir ----------------------------------------------------
if ($Dominio -notmatch '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$') {
    Falhar 'o -Dominio e o endereco em minusculas, sem https:// e sem barra.'
}
foreach ($p in @('docker', 'ssh', 'scp')) {
    if (-not (Get-Command $p -ErrorAction SilentlyContinue)) {
        Falhar ('o comando "' + $p + '" nao foi achado (o ssh e o scp sao o cliente OpenSSH do Windows; o docker, o Docker Desktop).')
    }
}
if ((Rodar-Silencioso 'docker' @('info')).Codigo -ne 0) {
    Falhar 'o Docker nao esta respondendo. Abra o Docker Desktop, espere ele terminar de iniciar e rode de novo.'
}
if (-not (Test-Path -LiteralPath $Aprovado -PathType Leaf)) {
    Falhar ('nao achei ' + $Aprovado + ': rode antes o scripts\deploy-teste.ps1 (so o que passou nele vai para a VPS).')
}
$aprovadas = @{}
foreach ($l in [IO.File]::ReadAllLines($Aprovado)) {
    if ($l -match '^(aurora-whisper-(server|backup):producao) (sha256:[0-9a-f]{64})$') { $aprovadas[$Matches[1]] = $Matches[3] }
}
foreach ($img in @($ImagemApi, $ImagemBackup)) {
    $id = Rodar-Silencioso 'docker' @('image', 'inspect', '-f', '{{.Id}}', $img)
    if ($id.Codigo -ne 0) { Falhar ('a imagem ' + $img + ' nao existe no PC: rode o scripts\deploy-teste.ps1.') }
    if ((-not $aprovadas.ContainsKey($img)) -or ($aprovadas[$img] -ne $id.Texto.Trim())) {
        Falhar ('a imagem ' + $img + ' nao e a que passou no deploy-teste.ps1 (foi montada de novo depois?). Rode o deploy-teste.ps1 de novo.')
    }
}
Ok 'as duas imagens sao as que passaram no deploy-teste.ps1'

# --- 2. o JSON do Google --------------------------------------------------------
$JsonCheio = ''
if ($ArquivoGoogle) {
    $JsonCheio = [IO.Path]::GetFullPath($ArquivoGoogle)
    if (-not (Test-Path -LiteralPath $JsonCheio -PathType Leaf)) { Falhar ('nao achei o arquivo ' + $JsonCheio + '.') }
    if (Dentro-De $JsonCheio $Raiz) { Falhar 'o JSON do Google nao pode ficar dentro do repositorio: guarde-o fora (Downloads, por exemplo).' }
    if ((Get-Item -LiteralPath $JsonCheio).Length -gt $MaxJsonDoGoogle) { Falhar 'o arquivo e grande demais para ser o JSON do client do Google.' }
    $json = $null
    try {
        $json = [IO.File]::ReadAllText($JsonCheio) | ConvertFrom-Json
    } catch {
        Falhar 'o arquivo nao e um JSON (e o que o Google Cloud baixa do client?).'
    }
    if (-not ($json.PSObject.Properties.Name -contains 'web')) {
        Falhar 'o JSON nao e de um client do tipo Aplicativo da Web (falta o "web").'
    }
    $web = $json.web
    $nomes = @($web.PSObject.Properties.Name)
    if (-not (($nomes -contains 'client_id') -and ($nomes -contains 'client_secret') -and ($nomes -contains 'redirect_uris'))) {
        Falhar 'o JSON do client nao tem client_id, client_secret e redirect_uris.'
    }
    $redirect = 'https://' + $Dominio + '/auth/google/callback'
    if (-not (@($web.redirect_uris) -contains $redirect)) {
        Falhar ('o client do Google nao tem o redirect ' + $redirect + ': acrescente-o no Google Cloud, baixe o JSON de novo e rode de novo.')
    }
    Ok ('o JSON do Google, com o redirect ' + $redirect + ' (nada dele foi mostrado)')
}

# --- 3. a pasta na VPS ----------------------------------------------------------
Write-Host ''
Write-Host ('Falando com ' + $Servidor + ' pelo ssh...')
$pasta = Rodar-Silencioso 'ssh' @($Servidor, 'set -e; umask 077; mkdir -p aurora-whisper/recebido; chmod 700 aurora-whisper aurora-whisper/recebido')
if ($pasta.Codigo -ne 0) { Falhar ('o ssh ' + $Servidor + ' nao criou a pasta ~/aurora-whisper: ' + $pasta.Texto) }
Ok ('a pasta ~/aurora-whisper (0700) na ' + $Servidor)

# --- 4. as imagens --------------------------------------------------------------
Write-Host ''
Write-Host 'Mandando as duas imagens (docker save | ssh | docker load; pode levar alguns minutos)...'
$linha = 'docker save ' + $ImagemApi + ' ' + $ImagemBackup + ' | ssh -C ' + $Servidor + ' docker load'
$codigo = Rodar-AVista 'cmd.exe' @('/d', '/c', $linha)
if ($codigo -ne 0) { Falhar 'o docker save | ssh | docker load falhou (acima). Nada foi trocado na VPS.' }

# --- 5. conferir ----------------------------------------------------------------
foreach ($img in @($ImagemApi, $ImagemBackup)) {
    $la = Rodar-Silencioso 'ssh' @($Servidor, ('docker image inspect -f {{.Id}} ' + $img))
    if ($la.Codigo -ne 0) { Falhar ('a imagem ' + $img + ' nao apareceu na VPS depois do docker load.') }
}
Ok 'as duas imagens estao na VPS'

# --- 6. a pasta deploy ----------------------------------------------------------
# Pelo caminho relativo, de dentro do repositorio: o scp leria o "C:" de um
# caminho completo como o nome de um servidor.
Push-Location -LiteralPath $Raiz
try {
    $codigo = Rodar-AVista 'scp' @('-q', '-r', 'deploy', ($Servidor + ':aurora-whisper/'))
} finally {
    Pop-Location
}
if ($codigo -ne 0) { Falhar 'o scp da pasta deploy falhou.' }
Ok ('a pasta deploy em ~/aurora-whisper/deploy na ' + $Servidor)

# --- 7. o JSON do Google --------------------------------------------------------
if ($JsonCheio) {
    Push-Location -LiteralPath (Split-Path -Parent $JsonCheio)
    try {
        $codigo = Rodar-AVista 'scp' @('-q', (Split-Path -Leaf $JsonCheio), ($Servidor + ':aurora-whisper/recebido/google-client.json'))
    } finally {
        Pop-Location
    }
    if ($codigo -ne 0) { Falhar 'o scp do JSON do Google falhou.' }
    Ok 'o JSON do Google em ~/aurora-whisper/recebido/ (na primeira vez o gerar-segredos.sh o move para segredos/; depois, o subir.sh o confere e o troca, ou o apaga se for o mesmo)'
}

Write-Host ''
Write-Host 'OK: tudo na VPS.' -ForegroundColor Green
Write-Host 'Agora, na VPS (pelo roteiro, docs\deploy-f11.md):'
Write-Host '  na primeira vez: bash ~/aurora-whisper/deploy/scripts/gerar-segredos.sh'
Write-Host '  e sempre:        bash ~/aurora-whisper/deploy/scripts/subir.sh'
