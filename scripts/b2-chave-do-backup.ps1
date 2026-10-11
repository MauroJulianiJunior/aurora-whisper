<#
.SYNOPSIS
  Cria (ou recria) a chave do backup no B2, so com listBuckets, listFiles e
  writeFiles, sem mostrar segredo na tela. Compativel com o PowerShell 5.1.
  Quem roda e o dono da nuvem, no PC, com o Docker Desktop aberto.

.DESCRIPTION
  1. pergunta o keyID e a applicationKey da chave MESTRA do B2 (a segunda nao
     aparece na tela);
  2. apaga as chaves antigas com o nome aurora-whisper-backup;
  3. cria a nova e grava o keyID e a applicationKey dela num arquivo desta
     conta do Windows, fora do repositorio, sem mostrar;
  4. confere que a nova so tem listBuckets, listFiles e writeFiles, so no
     bucket.
  A mestra vai ao conteiner da CLI do B2 pelo ambiente do processo (docker run
  -e NOME, sem o valor na linha de comando), e cada conteiner e apagado ao
  terminar. Nasceu porque o Ctrl+V nao cola no terminal de dentro do conteiner
  (09-out-2026), e porque a saida do "b2 key create" traz a chave nova.

.PARAMETER Bucket
  O nome do SEU bucket no B2 (o do passo 2 do docs/backup-b2-f11.md, por
  exemplo aurora-whisper-backup-exemplo). Obrigatorio.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\b2-chave-do-backup.ps1 -Bucket aurora-whisper-backup-exemplo
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, HelpMessage = 'o nome do seu bucket no B2')]
    [ValidatePattern('^[a-z0-9][a-z0-9-]{4,48}[a-z0-9]$')]
    [string]$Bucket,

    [ValidatePattern('^[A-Za-z0-9-]{1,100}$')]
    [string]$NomeDaChave = 'aurora-whisper-backup'
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$ImagemDaCli = 'backblazeit/b2:4.8.0'
$Capacidades = 'listBuckets,listFiles,writeFiles'
$PastaDaChave = Join-Path $env:USERPROFILE '.aurora-whisper-dev\backup'
$ArquivoDaChave = Join-Path $PastaDaChave 'b2-chave-do-backup.txt'

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('FALHOU: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

function Ok([string]$Mensagem) {
    Write-Host ('OK: ' + $Mensagem) -ForegroundColor Green
}

# Roda a CLI do B2 num conteiner descartavel, com a mestra pelo ambiente.
function B2([string[]]$Argumentos) {
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        # So a saida normal: um aviso da CLI (na saida de erro) nao se mistura
        # com a linha da chave nova.
        $tudo = @(& docker run --rm -e B2_APPLICATION_KEY_ID -e B2_APPLICATION_KEY $ImagemDaCli @Argumentos 2>&1)
        $codigoDaCli = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $anterior
    }
    $saida = @($tudo | Where-Object { $_ -isnot [System.Management.Automation.ErrorRecord] } | ForEach-Object { $_.ToString() } | Where-Object { $_.Trim() })
    # A saida de erro da CLI do B2 nunca traz a chave: so o motivo (ex.:
    # "Invalid authorization token ... (bad_auth_token)").
    $erro = @($tudo | Where-Object { $_ -is [System.Management.Automation.ErrorRecord] } | ForEach-Object { $_.ToString() } | Where-Object { $_ -match 'ERROR|Server said|error' })
    return [pscustomobject]@{ Codigo = $codigoDaCli; Texto = (($saida -join "`n").Trim()); Erro = (($erro -join ' ').Trim()) }
}

# As linhas do "b2 key list --long" (keyID, nome, bucket, ..., capacidades):
# nenhuma traz segredo.
function Linhas-Da-Chave([string]$Lista) {
    $achadas = @()
    foreach ($l in ($Lista -split "`n")) {
        $colunas = @($l.Trim() -split '\s+')
        if (($colunas.Count -ge 3) -and ($colunas[1] -eq $NomeDaChave)) { $achadas += ,$colunas }
    }
    return ,$achadas
}

# --- 1. o Docker -----------------------------------------------------------------
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Falhar 'o comando "docker" nao foi achado. Abra o Docker Desktop e rode de novo.'
}
# A imagem da CLI antes de tudo, para nenhuma mensagem do download se misturar
# com as respostas do B2.
$ErrorActionPreference = 'Continue'
& docker image inspect $ImagemDaCli 2> $null | Out-Null
if ($LASTEXITCODE -ne 0) {
    Write-Host ('Baixando a CLI do B2 (' + $ImagemDaCli + ')...')
    & docker pull -q $ImagemDaCli 2> $null | Out-Null
}
$codigoDaImagem = $LASTEXITCODE
$ErrorActionPreference = 'Stop'
if ($codigoDaImagem -ne 0) { Falhar 'o Docker nao conseguiu a imagem da CLI do B2. O Docker Desktop esta aberto?' }

# --- 2. a mestra -----------------------------------------------------------------
Write-Host ''
Write-Host 'Vou pedir a chave MESTRA do B2 (a do seu gerenciador de senhas). Ela so e usada aqui no PC, e nada dela aparece na tela.'
$idDaMestra = (Read-Host 'keyID da chave MESTRA (12 letras e numeros)').Trim()
if ($idDaMestra -notmatch '^[0-9a-f]{12}$') {
    Falhar 'o keyID da mestra tem 12 letras e numeros, como 0123456789ab. Confira no seu gerenciador de senhas e rode de novo.'
}
$segura = Read-Host 'applicationKey da chave MESTRA (nao aparece; cole e tecle Enter)' -AsSecureString
$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($segura)
try {
    $chaveDaMestra = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr).Trim()
} finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
}
if (-not $chaveDaMestra) { Falhar 'a applicationKey veio vazia. Rode de novo e cole a da mestra.' }
# Um retrato que nao entrega a chave: o tamanho e as 3 primeiras letras. A
# mestra do B2 comeca com 005; uma chave restrita (como a do backup) comeca
# com K.
$retrato = ('tem ' + $chaveDaMestra.Length + ' caracteres e comeca com "' + $chaveDaMestra.Substring(0, [Math]::Min(3, $chaveDaMestra.Length)) + '"')
if ($chaveDaMestra.StartsWith('K')) {
    Falhar ('a chave colada ' + $retrato + ': e uma chave RESTRITA (comeca com K), e nao a mestra. A applicationKey da mestra comeca com 005.')
}

$env:B2_APPLICATION_KEY_ID = $idDaMestra
$env:B2_APPLICATION_KEY = $chaveDaMestra
$chaveDaMestra = $null
try {
    # --- 3. as chaves antigas com o nome ---------------------------------------
    Write-Host ''
    Write-Host 'Falando com o B2 (na primeira vez o Docker baixa a CLI)...'
    $lista = B2 @('key', 'list', '--long')
    if ($lista.Codigo -ne 0) {
        Falhar ('o B2 recusou. A chave colada ' + $retrato + '. O B2 disse: ' + $lista.Erro)
    }
    Ok 'a mestra foi aceita'
    foreach ($colunas in (Linhas-Da-Chave $lista.Texto)) {
        $apagou = B2 @('key', 'delete', $colunas[0])
        if ($apagou.Codigo -ne 0) { Falhar ('nao consegui apagar a chave antiga ' + $colunas[0] + '.') }
        Ok ('a chave antiga ' + $colunas[0] + ' foi apagada')
    }

    # --- 4. a chave nova, gravada sem mostrar ------------------------------------
    $nova = B2 @('key', 'create', '--bucket', $Bucket, $NomeDaChave, $Capacidades)
    if ($nova.Codigo -ne 0) { Falhar ('o B2 nao criou a chave nova (o bucket ' + $Bucket + ' existe?). Nada foi mostrado.') }
    $ultima = @($nova.Texto -split "`n")[-1]
    $partes = @($ultima.Trim() -split '\s+')
    $ultima = $null
    if (($partes.Count -ne 2) -or ($partes[0] -notmatch '^[0-9a-f]{25}$') -or ($partes[1] -notmatch '^K[0-9A-Za-z+/]{20,}$')) {
        Falhar 'a resposta do B2 nao veio no formato esperado. Nada foi mostrado; rode de novo (a chave criada agora sera apagada e refeita).'
    }
    if (-not (Test-Path -LiteralPath $PastaDaChave)) { $null = New-Item -ItemType Directory -Path $PastaDaChave }
    $texto = 'Chave do backup do B2 (a restrita, para a VPS; o gerar-segredos.sh pede as duas linhas)' + "`r`n" +
        'keyID: ' + $partes[0] + "`r`n" +
        'applicationKey: ' + $partes[1] + "`r`n"
    [IO.File]::WriteAllText($ArquivoDaChave, $texto, (New-Object System.Text.UTF8Encoding $false))
    $partes = $null
    $nova = $null
    Ok ('a chave nova foi criada e ficou em ' + $ArquivoDaChave + ' (nao foi mostrada)')

    # --- 5. as capacidades da nova -------------------------------------------------
    $conferir = B2 @('key', 'list', '--long')
    if ($conferir.Codigo -ne 0) { Falhar 'nao consegui listar as chaves para conferir.' }
    $achadas = Linhas-Da-Chave $conferir.Texto
    if ($achadas.Count -ne 1) { Falhar ('ha ' + $achadas.Count + ' chaves com o nome ' + $NomeDaChave + '; o esperado e 1.') }
    $colunas = $achadas[0]
    if (($colunas[2] -ne $Bucket) -or ($colunas[$colunas.Count - 1] -ne $Capacidades)) {
        Falhar ('a chave nova nao ficou como devia (bucket ' + $colunas[2] + ', capacidades ' + $colunas[$colunas.Count - 1] + '). Me mande esta linha.')
    }
    Ok ('a chave nova so tem ' + $Capacidades + ', so no bucket ' + $Bucket)
} finally {
    Remove-Item Env:B2_APPLICATION_KEY -ErrorAction SilentlyContinue
    Remove-Item Env:B2_APPLICATION_KEY_ID -ErrorAction SilentlyContinue
}

Write-Host ''
Write-Host 'Agora, so voce:'
Write-Host ('  1. abra o arquivo: notepad ' + $ArquivoDaChave)
Write-Host '  2. copie as duas linhas para uma nota nova no seu gerenciador de senhas, "B2 - chave do backup (VPS)";'
Write-Host '  3. depois de guardar, apague o arquivo:'
Write-Host ('     Remove-Item ' + $ArquivoDaChave)
Write-Host 'Nunca cole o conteudo dele no chat.'
