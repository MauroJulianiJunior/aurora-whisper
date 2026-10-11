<#
.SYNOPSIS
  Monta o pacote do instalador do Aurora Whisper (ADR-018) e, se houver o
  Inno Setup, compila o Setup. Compativel com o PowerShell 5.1.

.DESCRIPTION
  Parte do repositorio e produz a pasta de staging (dist\staging por padrao) e
  o Setup (dist\AuroraWhisper-Setup-<versao>.exe, com o .sha256 ao lado).

  Passos, nesta ordem:
    1. asrstage -destino <staging> -sem-dicionario: o whisper-server, as 12 DLLs
       e o modelo (conferido pelo SHA-256 que o Agent usa), SEM dicionario,
       SEM o marcador "portatil", SEM historico.
    2. go build -trimpath -ldflags "-H=windowsgui -X main.version=<versao>" do
       Agent para <staging>\aurora-whisper-agent.exe. Confere que a versao entrou
       no binario e que nenhum caminho desta maquina ficou dentro dele.
    3. pnpm build do painel, com --outDir <staging>\painel.
    4. <staging>\licencas\: o LICENSE e o THIRD_PARTY_NOTICES.md do projeto, a OFL
       da fonte do overlay, a licenca do Go e dos modulos do Agent (lidas do
       GOROOT e do cache de modulos), e as do whisper.cpp, dos pesos da OpenAI
       (MIT) e a Apache-2.0 (baixadas, veja $LicencasRemotas abaixo).
    5. Confere o staging (o que tem de ter e o que NAO pode ter).
    6. Se houver o ISCC (Inno Setup 6), compila installer\aurora-whisper.iss.

  O script NAO embute dicionario pessoal. NUNCA executa o Agent nem o Setup
  gerado: so compila e monta arquivos. Escreve so em dist\ (a pasta de staging
  e a de saida tem de ficar dentro dela) e nunca em bin\.

  Pre-requisitos que o script NAO baixa (o workflow de release os baixa, com
  SHA-256 fixo; local, quem monta os poe, como no README):
    thirdparty\whisper\Release\whisper-server.exe (e as DLLs)
    models\quant\small-q4_0.bin

.PARAMETER Versao
  Versao do Agent e do Setup, sem o "v": 0.1.0 ou 0.1.0-rc.1.

.PARAMETER Staging
  Pasta de staging, dentro de dist\ . Se existir, e apagada e refeita.

.PARAMETER Saida
  Pasta do Setup, dentro de dist\ .

.PARAMETER Iscc
  Caminho do ISCC.exe. Sem ele, procura no PATH e nas pastas padrao do Inno Setup 6.

.PARAMETER ExigirIscc
  Falha se o ISCC nao for achado (o workflow de release usa). Sem esta opcao,
  o script so avisa e termina depois de montar o staging.

.EXAMPLE
  powershell -File scripts\montar-instalador.ps1 -Versao 0.1.0
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Versao,

    [string]$Staging = 'dist\staging',

    [string]$Saida = 'dist',

    [string]$Iscc = '',

    [switch]$ExigirIscc
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

# ---------------------------------------------------------------------------
# O que o pacote leva e de onde vem (ADR-018). Mexer aqui e mexer no pacote.
# ---------------------------------------------------------------------------

# O nome do modelo montado: internal/asr/model.SmallQ4_0.Arquivo (um teste confere).
$ArquivoDoModelo = 'ggml-small-q4_0.bin'

# Licencas que nao estao no repositorio nem no zip do whisper.cpp, baixadas na
# montagem do pacote e conferidas pelo SHA-256 fixado aqui (campo Sha256), como
# o workflow ja faz com o zip e o modelo: um arquivo diferente para a montagem.
# Sem o campo, o script so imprime o SHA-256, para quem o for fixar.
# Os dois enderecos do GitHub apontam para um COMMIT (e nao para uma tag ou para
# "main"), entao o texto nao muda debaixo do script. O LICENSE existe nos dois
# commits (conferido na arvore do GitHub, sem baixar o arquivo): whisper.cpp 1078
# bytes, openai/whisper 1063 bytes.
# NAO TESTADO: estes enderecos nao foram baixados quando o script foi escrito.
$LicencasRemotas = @(
    # 927cfce... e o commit da tag b5130, a do zip do whisper-server.
    @{ Nome = 'whisper.cpp-LICENSE-MIT.txt';
       Url  = 'https://raw.githubusercontent.com/ggml-org/whisper.cpp/927cfce34f31707e17f2bff35c349632fb9e2c3a/LICENSE';
       Sha256 = '94f29bbed6a22c35b992c5c6ebf0e7c92f13b836b90f36f461c9cf2f0f1d010d' },
    # Os pesos do Whisper (OpenAI): o README do openai/whisper diz MIT, e o card do
    # Hugging Face diz Apache-2.0. O pacote leva os dois textos (ADR-018, risco 6).
    # 8609812... era o main de 31-ago-2026.
    @{ Nome = 'openai-whisper-LICENSE-MIT.txt';
       Url  = 'https://raw.githubusercontent.com/openai/whisper/86098128c0b4f24f0e2aa2994de830614b474227/LICENSE';
       Sha256 = 'b5d65a59060e68c4ff940e1eddfa6f94b2d68fdf58ed7f4dd57721c997e35e9d' },
    @{ Nome = 'Apache-2.0.txt';
       Url  = 'https://www.apache.org/licenses/LICENSE-2.0.txt';
       Sha256 = 'cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30' }
)

# ---------------------------------------------------------------------------
# Utilidades
# ---------------------------------------------------------------------------

function Escrever-Passo([string]$Texto) {
    Write-Host ''
    Write-Host ('==> ' + $Texto)
}

# Roda um comando nativo e falha se o codigo de saida nao for 0.
function Executar([string]$Descricao, [scriptblock]$Comando) {
    & $Comando
    if ($LASTEXITCODE -ne 0) {
        throw ($Descricao + ' falhou (codigo ' + $LASTEXITCODE + ')')
    }
}

# Caminho absoluto de $Caminho, relativo a $Base se ele for relativo.
function Caminho-Absoluto([string]$Caminho, [string]$Base) {
    if ([IO.Path]::IsPathRooted($Caminho)) {
        return [IO.Path]::GetFullPath($Caminho)
    }
    return [IO.Path]::GetFullPath((Join-Path $Base $Caminho))
}

# O script so apaga e escreve dentro de <repo>\dist\ : confere antes de qualquer coisa.
function Garantir-Dentro-De([string]$Caminho, [string]$Pasta, [string]$Nome) {
    $prefixo = $Pasta.TrimEnd('\') + '\'
    if (-not $Caminho.StartsWith($prefixo, [StringComparison]::OrdinalIgnoreCase) -or $Caminho.Length -le $prefixo.Length) {
        throw ($Nome + ' tem de ficar DENTRO de ' + $Pasta + ' (e nunca ser a propria pasta): ' + $Caminho)
    }
}

function Baixar-Arquivo([string]$Url, [string]$Destino) {
    # O PowerShell 5.1 do Windows 10 pode nao ter o TLS 1.2 ligado por padrao.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Destino
}

# ---------------------------------------------------------------------------
# Entradas
# ---------------------------------------------------------------------------

if ($Versao -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$') {
    throw ('Versao invalida: "' + $Versao + '". Use 0.1.0 ou 0.1.0-rc.1, sem o "v".')
}
# x.y.z.w para as propriedades do arquivo, sem o sufixo de prerelease.
$VersaoNumerica = (($Versao -split '-')[0]) + '.0'

$Raiz = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $Raiz
$Dist = Join-Path $Raiz 'dist'
$StagingAbs = Caminho-Absoluto $Staging $Raiz
$SaidaAbs = Caminho-Absoluto $Saida $Raiz
Garantir-Dentro-De $StagingAbs $Dist 'Staging'
if (-not $SaidaAbs.TrimEnd('\').Equals($Dist, [StringComparison]::OrdinalIgnoreCase)) {
    Garantir-Dentro-De $SaidaAbs $Dist 'Saida'
}
$Iss = Join-Path $Raiz 'installer\aurora-whisper.iss'
if (-not (Test-Path -LiteralPath $Iss)) { throw ('Nao achei ' + $Iss) }

Escrever-Passo ('Aurora Whisper ' + $Versao + ': staging em ' + $StagingAbs)

# O que este script nao baixa: avisa ANTES de gastar tempo com o resto.
$Falta = @()
if (-not (Test-Path -LiteralPath 'thirdparty\whisper\Release\whisper-server.exe')) { $Falta += 'thirdparty\whisper\Release\whisper-server.exe (o zip do whisper.cpp, README)' }
if (-not (Test-Path -LiteralPath 'models\quant\small-q4_0.bin')) { $Falta += 'models\quant\small-q4_0.bin (o modelo q4_0, README)' }
if ($Falta.Count -gt 0) {
    throw ("Faltam arquivos que este script nao baixa:`n  " + ($Falta -join "`n  "))
}
foreach ($ferramenta in @('go', 'pnpm')) {
    if (-not (Get-Command $ferramenta -ErrorAction SilentlyContinue)) { throw ('Nao achei o "' + $ferramenta + '" no PATH.') }
}

# ---------------------------------------------------------------------------
# Staging limpo
# ---------------------------------------------------------------------------

if (Test-Path -LiteralPath $StagingAbs) {
    Escrever-Passo 'Apagando o staging anterior'
    Remove-Item -LiteralPath $StagingAbs -Recurse -Force
}
New-Item -ItemType Directory -Path $StagingAbs -Force | Out-Null

# ---------------------------------------------------------------------------
# 1. O servidor de voz, as DLLs e o modelo (sem dicionario, sem marcador)
# ---------------------------------------------------------------------------

Escrever-Passo 'asrstage: servidor, DLLs e modelo (sem dicionario)'
Executar 'asrstage' { & go run ./cmd/asrstage -destino $StagingAbs -sem-dicionario }

# ---------------------------------------------------------------------------
# 2. O Agent
# ---------------------------------------------------------------------------

Escrever-Passo ('go build do Agent (-trimpath, versao ' + $Versao + ')')
$AgentExe = Join-Path $StagingAbs 'aurora-whisper-agent.exe'
# -H=windowsgui: aplicativo de bandeja, sem janela de console. -X exige que
# main.version seja uma VARIAVEL (e e: cmd/agent/main.go). Sempre com -o: sem
# ele o Go grava um agent.exe longe do modelo.
Executar 'go build do Agent' { & go build -trimpath ('-ldflags=-H=windowsgui -X main.version=' + $Versao) -o $AgentExe ./cmd/agent }

# Conferencias que NAO executam o Agent (so leem o arquivo):
#
# (a) O -X pegou. Procurar o texto da versao no binario nao prova nada para a
# 0.1.0: o valor de desenvolvimento ("0.1.0-sprint2", cmd/agent/main.go) a
# contem, e o Go ainda o deixa no binario depois do -X. O que prova e o simbolo
# main.version.str, que o linker so cria quando aplica um -X a main.version.
# (O go version -m nao serve: nao registra o -ldflags.)
$Simbolos = & go tool nm $AgentExe
if ($LASTEXITCODE -ne 0) { throw 'go tool nm falhou no executavel do Agent' }
if (@($Simbolos | Where-Object { $_ -match '\smain\.version\.str\s*$' }).Count -eq 0) {
    throw 'O -X main.version nao pegou: o linker nao criou o simbolo main.version.str (main.version e uma variavel string em cmd/agent?).'
}
# (b) O -trimpath pegou: o Go o registra nas informacoes de build do executavel.
$Info = (& go version -m $AgentExe) -join "`n"
if ($LASTEXITCODE -ne 0) { throw 'go version -m falhou no executavel do Agent' }
if (-not $Info.Contains('-trimpath=true')) {
    throw 'O executavel nao foi compilado com -trimpath.'
}
# (c) A versao esta la, e nenhum caminho desta maquina ficou dentro do binario.
$bytes = [IO.File]::ReadAllBytes($AgentExe)
$texto = [Text.Encoding]::GetEncoding(28591).GetString($bytes)   # ISO-8859-1: byte a byte
if (-not $texto.Contains($Versao)) {
    throw ('A versao ' + $Versao + ' nao esta dentro do executavel: o -X main.version nao pegou.')
}
$Vazou = @($Raiz, $env:USERPROFILE) | Where-Object { $_ } | ForEach-Object { $_; $_.Replace('\', '/') }
foreach ($caminho in $Vazou) {
    if ($texto.Contains($caminho)) {
        throw ('O executavel traz um caminho desta maquina (' + $caminho + '): o -trimpath nao pegou.')
    }
}
$texto = $null; $bytes = $null

# ---------------------------------------------------------------------------
# 3. O painel
# ---------------------------------------------------------------------------

Escrever-Passo 'Painel: pnpm install e pnpm build (--outDir no staging)'
$PainelDir = Join-Path $StagingAbs 'painel'
Executar 'pnpm install' { & pnpm -C web install --frozen-lockfile }
# O vite.config.ts aponta o destino padrao para bin\painel; o --outDir manda
# para o staging. emptyOutDir ja e true la.
Executar 'pnpm build' { & pnpm -C web build --outDir $PainelDir }
if (-not (Test-Path -LiteralPath (Join-Path $PainelDir 'index.html'))) {
    throw 'O build do painel nao gerou painel\index.html no staging.'
}

# ---------------------------------------------------------------------------
# 4. As licencas
# ---------------------------------------------------------------------------

Escrever-Passo 'licencas\'
$Lic = Join-Path $StagingAbs 'licencas'
New-Item -ItemType Directory -Path $Lic -Force | Out-Null

# Do repositorio.
Copy-Item -LiteralPath 'LICENSE' -Destination (Join-Path $Lic 'LICENSE')
Copy-Item -LiteralPath 'THIRD_PARTY_NOTICES.md' -Destination (Join-Path $Lic 'THIRD_PARTY_NOTICES.md')
Copy-Item -LiteralPath 'internal\agent\overlay\fontes\OFL.txt' -Destination (Join-Path $Lic 'OFL-IBMPlexMono-overlay.txt')

# Do Go: a biblioteca padrao e o runtime vao dentro do Agent.
$GoRoot = (& go env GOROOT).Trim()
Copy-Item -LiteralPath (Join-Path $GoRoot 'LICENSE') -Destination (Join-Path $Lic 'Go-LICENSE.txt')

# Dos modulos que entram no Agent: cada LICENSE/COPYING/NOTICE/PATENTS do cache de modulos.
$ModuloDoProjeto = (& go list -m).Trim()
$Modulos = & go list -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Dir}}{{end}}' ./cmd/agent
if ($LASTEXITCODE -ne 0) { throw 'go list -deps falhou' }
$Modulos = @($Modulos | Where-Object { $_ } | Sort-Object -Unique)
$SemLicenca = @()
$Copiados = 0
foreach ($linha in $Modulos) {
    $partes = $linha.Split('|')
    $caminhoDoModulo = $partes[0]
    $dirDoModulo = $partes[1]
    if ($caminhoDoModulo -eq $ModuloDoProjeto -or -not $dirDoModulo) { continue }
    $achados = @(Get-ChildItem -LiteralPath $dirDoModulo -File -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match '^(LICENSE|LICENCE|COPYING|NOTICE|PATENTS)' })
    if ($achados.Count -eq 0) { $SemLicenca += $caminhoDoModulo; continue }
    $destino = Join-Path (Join-Path $Lic 'go-modulos') ($caminhoDoModulo.Replace('/', '_'))
    New-Item -ItemType Directory -Path $destino -Force | Out-Null
    foreach ($arq in $achados) {
        Copy-Item -LiteralPath $arq.FullName -Destination (Join-Path $destino $arq.Name)
        $Copiados++
    }
}
Write-Host ('  Go: ' + $Modulos.Count + ' modulos, ' + $Copiados + ' arquivos de licenca copiados')
if ($SemLicenca.Count -gt 0) {
    Write-Warning ('Modulos do Agent SEM arquivo de licenca no cache (confira a mao antes de publicar): ' + ($SemLicenca -join ', '))
}

# Baixadas: whisper.cpp, pesos da OpenAI, Apache-2.0.
foreach ($l in $LicencasRemotas) {
    $destino = Join-Path $Lic $l.Nome
    Baixar-Arquivo $l.Url $destino
    $hash = (Get-FileHash -LiteralPath $destino -Algorithm SHA256).Hash.ToLowerInvariant()
    if ((Get-Item -LiteralPath $destino).Length -lt 200) { throw ($l.Nome + ' veio vazio ou quase: ' + $l.Url) }
    if ($l.Sha256) {
        if ($hash -ne $l.Sha256.ToLowerInvariant()) { throw ($l.Nome + ': SHA-256 diferente do fixado (' + $hash + ')') }
    } else {
        Write-Host ('  ' + $l.Nome + ' sha256 ' + $hash + '  (nao fixado: fixe em $LicencasRemotas)')
    }
}

# ---------------------------------------------------------------------------
# 5. Conferir o staging
# ---------------------------------------------------------------------------

Escrever-Passo 'Conferindo o staging'
$Tem = @('aurora-whisper-agent.exe', 'whisper-server.exe', $ArquivoDoModelo, 'painel\index.html',
         'licencas\LICENSE', 'licencas\THIRD_PARTY_NOTICES.md', 'licencas\Go-LICENSE.txt')
foreach ($rel in $Tem) {
    if (-not (Test-Path -LiteralPath (Join-Path $StagingAbs $rel))) { throw ('Falta no staging: ' + $rel) }
}
# O que o Setup NUNCA leva: dado de quem desenvolve e o marcador do modo portatil.
$Proibido = @(Get-ChildItem -LiteralPath $StagingAbs -Recurse -Force -File |
    Where-Object { $_.Name -eq 'dicionario.json' -or $_.Name -eq 'portatil' -or $_.Name -like 'historico.db*' -or $_.Name -like '*.log' -or $_.Name -like '*.log.1' })
if ($Proibido.Count -gt 0) {
    throw ("O staging tem arquivo que o pacote nao pode levar:`n  " + (($Proibido | ForEach-Object { $_.FullName }) -join "`n  "))
}
$total = (Get-ChildItem -LiteralPath $StagingAbs -Recurse -File | Measure-Object -Property Length -Sum).Sum
Write-Host ('  OK: ' + [math]::Round($total / 1MB, 1) + ' MB em ' + (Get-ChildItem -LiteralPath $StagingAbs -Recurse -File).Count + ' arquivos')

# ---------------------------------------------------------------------------
# 6. O Setup (Inno Setup)
# ---------------------------------------------------------------------------

function Achar-Iscc([string]$Pedido) {
    if ($Pedido) {
        if (Test-Path -LiteralPath $Pedido) { return (Resolve-Path -LiteralPath $Pedido).Path }
        throw ('ISCC nao existe em ' + $Pedido)
    }
    $noPath = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($noPath) { return $noPath.Source }
    $candidatos = @(
        (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe')
    )
    foreach ($c in $candidatos) {
        if ($c -and (Test-Path -LiteralPath $c)) { return $c }
    }
    return $null
}

$IsccExe = Achar-Iscc $Iscc
if (-not $IsccExe) {
    if ($ExigirIscc) { throw 'O ISCC.exe (Inno Setup 6) nao foi achado, e -ExigirIscc foi pedido.' }
    Write-Warning 'ISCC.exe (Inno Setup 6) nao encontrado: o staging esta pronto, mas o Setup NAO foi compilado.'
    Write-Host ('  Staging: ' + $StagingAbs)
    exit 0
}

Escrever-Passo ('Compilando o Setup com ' + $IsccExe)
New-Item -ItemType Directory -Path $SaidaAbs -Force | Out-Null
Executar 'ISCC' {
    & $IsccExe ('/DVersaoDoApp=' + $Versao) ('/DVersaoNumerica=' + $VersaoNumerica) `
        ('/DPastaStaging=' + $StagingAbs) ('/DPastaSaida=' + $SaidaAbs) $Iss
}

$Setup = Join-Path $SaidaAbs ('AuroraWhisper-Setup-' + $Versao + '.exe')
if (-not (Test-Path -LiteralPath $Setup)) { throw ('O ISCC terminou, mas nao achei ' + $Setup) }
$hashSetup = (Get-FileHash -LiteralPath $Setup -Algorithm SHA256).Hash.ToLowerInvariant()
# Formato do sha256sum: "<hash> *<arquivo>". Sem BOM, em ASCII.
[IO.File]::WriteAllText(($Setup + '.sha256'), ($hashSetup + ' *' + (Split-Path $Setup -Leaf) + "`n"), [Text.Encoding]::ASCII)

Escrever-Passo 'Pronto'
Write-Host ('  Setup:  ' + $Setup + '  (' + [math]::Round((Get-Item -LiteralPath $Setup).Length / 1MB, 1) + ' MB)')
Write-Host ('  SHA-256: ' + $hashSetup)
Write-Host '  Sem assinatura digital (0.x): o Windows vai mostrar o aviso do SmartScreen.'
