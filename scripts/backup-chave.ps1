<#
.SYNOPSIS
  Gera a chave do backup da nuvem (o par do age) DENTRO de um conteiner e
  mostra so a chave PUBLICA. Compativel com o PowerShell 5.1. Quem roda e o
  Mauro, uma vez, antes do deploy.

.DESCRIPTION
  O backup da nuvem (ADR-019, "Decisoes da F11", D2) e cifrado com o age para
  uma chave PUBLICA: o servidor so cifra. A chave PRIVADA e so do Mauro. Ela
  nasce aqui, no PC, e fica em %USERPROFILE%\.aurora-whisper-dev\backup (desta
  conta do Windows, fora do repositorio) e no gerenciador de senhas dele, com
  uma segunda copia fora do PC. Se ela se perder, todos os backups se perdem.

  O age nao esta no Windows, e nada e instalado: a chave nasce num conteiner
  descartavel da imagem do backup (deploy/backup/Dockerfile), sem rede, com so
  a pasta da chave montada, removido ao fim. Se a imagem ainda nao existe, o
  script a constroi (na primeira vez o Docker baixa as imagens de base).

  O que ele faz:
    1. confere que o Docker responde (ele NAO inicia o Docker Desktop);
    2. recusa uma pasta dentro do repositorio, e recusa gerar de novo se ja ha
       uma chave (uma chave nova deixaria os backups ja feitos sem quem os
       abra);
    3. gera o par com o age-keygen, no conteiner;
    4. le de volta so a publica (age-keygen -y), grava-a ao lado da privada e a
       mostra. A privada nunca e impressa.

  A linha age1... que ele mostra e a que o gerar-segredos.sh pergunta na VPS.
  Ela nao e segredo.

.PARAMETER Pasta
  Onde a chave fica. Padrao: %USERPROFILE%\.aurora-whisper-dev\backup.

.EXAMPLE
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\backup-chave.ps1
#>
[CmdletBinding()]
param(
    [string]$Pasta = (Join-Path $env:USERPROFILE '.aurora-whisper-dev\backup')
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

# A imagem do backup e os nomes dos arquivos: o teste-restauracao-mensal.ps1 e
# o roteiro usam os mesmos.
$Imagem = 'aurora-whisper-backup:producao'
$NomeDaChave = 'aurora-whisper-backup.key'
$NomeDaPublica = 'aurora-whisper-backup.pub'

# O repositorio: o script esta em <repo>\scripts\.
$Raiz = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))

# Roda "docker <argumentos>" sem nunca lancar excecao: devolve o codigo de saida
# e o texto (stdout e stderr juntos). O PowerShell 5.1 transforma stderr
# redirecionado em erro quando $ErrorActionPreference e Stop; por isso a
# preferencia muda aqui dentro.
function Docker-Silencioso([string[]]$Argumentos) {
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $linhas = @(& docker @Argumentos 2>&1 | ForEach-Object { $_.ToString() })
        $codigo = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $anterior
    }
    return [pscustomobject]@{ Codigo = $codigo; Texto = (($linhas -join [Environment]::NewLine).Trim()) }
}

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('FALHOU: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

# Diz se $Caminho e $Pasta ou fica dentro dela (sem diferenciar maiusculas).
function Dentro-De([string]$Caminho, [string]$Pasta) {
    $c = $Caminho.TrimEnd('\') + '\'
    $p = $Pasta.TrimEnd('\') + '\'
    return $c.StartsWith($p, [StringComparison]::OrdinalIgnoreCase)
}

# Constroi a imagem do backup se ela ainda nao existe (a vista, com o progresso
# do Docker).
function Garantir-Imagem {
    $existe = Docker-Silencioso @('image', 'inspect', $Imagem)
    if ($existe.Codigo -eq 0) { return }
    Write-Host ('Construindo a imagem ' + $Imagem + ' (so na primeira vez; o Docker baixa as imagens de base)...')
    $dockerfile = Join-Path $Raiz 'deploy\backup\Dockerfile'
    $contexto = Join-Path $Raiz 'deploy\scripts'
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & docker build -f $dockerfile -t $Imagem $contexto
    $codigo = $LASTEXITCODE
    $ErrorActionPreference = $anterior
    if ($codigo -ne 0) {
        Falhar ('O Docker nao conseguiu construir a imagem ' + $Imagem + '. Confira a conexao com a internet e rode de novo.')
    }
}

# 1. O Docker esta respondendo?
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Falhar 'O comando "docker" nao foi achado. Abra (ou instale) o Docker Desktop e rode de novo.'
}
$info = Docker-Silencioso @('info')
if ($info.Codigo -ne 0) {
    Falhar 'O Docker nao esta respondendo. Abra o Docker Desktop, espere ele terminar de iniciar e rode este script de novo.'
}

# 2. A pasta: fora do repositorio, e sem chave ainda.
$PastaCheia = [IO.Path]::GetFullPath($Pasta)
if (Dentro-De $PastaCheia $Raiz) {
    Falhar ('A pasta da chave nao pode ficar dentro do repositorio (' + $Raiz + '). Use a padrao, em %USERPROFILE%\.aurora-whisper-dev\backup.')
}
$ArquivoDaChave = Join-Path $PastaCheia $NomeDaChave
$ArquivoDaPublica = Join-Path $PastaCheia $NomeDaPublica
if (Test-Path -LiteralPath $ArquivoDaChave) {
    Falhar ('Ja existe uma chave em ' + $ArquivoDaChave + '. Uma chave nova deixaria os backups ja feitos sem quem os abra. Nada foi feito.')
}
if (-not (Test-Path -LiteralPath $PastaCheia)) {
    New-Item -ItemType Directory -Path $PastaCheia | Out-Null
}

# 3. A imagem e o par, num conteiner sem rede, so com a pasta da chave.
Garantir-Imagem
$gerar = Docker-Silencioso @(
    'run', '--rm',
    '--network', 'none',
    '--read-only',
    '--cap-drop', 'ALL',
    '--security-opt', 'no-new-privileges',
    '-v', ($PastaCheia + ':/chave'),
    $Imagem,
    'age-keygen', '-o', ('/chave/' + $NomeDaChave)
)
if (($gerar.Codigo -ne 0) -or -not (Test-Path -LiteralPath $ArquivoDaChave)) {
    Falhar 'O age-keygen nao conseguiu gravar a chave na pasta. Nada foi mostrado.'
}

# 4. So a publica, lida de volta da privada.
$lida = Docker-Silencioso @(
    'run', '--rm',
    '--network', 'none',
    '--read-only',
    '--cap-drop', 'ALL',
    '--security-opt', 'no-new-privileges',
    '-v', ($PastaCheia + ':/chave:ro'),
    $Imagem,
    'age-keygen', '-y', ('/chave/' + $NomeDaChave)
)
$publica = $lida.Texto.Trim()
if (($lida.Codigo -ne 0) -or ($publica -notmatch '^age1[02-9ac-hj-np-z]{58}$')) {
    Falhar ('Nao consegui ler a chave publica de volta. A privada ficou em ' + $ArquivoDaChave + '; apague-a e rode de novo.')
}
[IO.File]::WriteAllText($ArquivoDaPublica, $publica + "`n")

Write-Host ''
Write-Host 'OK: a chave do backup foi gerada.' -ForegroundColor Green
Write-Host ''
Write-Host 'A chave PUBLICA (nao e segredo; e a que o gerar-segredos.sh pergunta na VPS):'
Write-Host ''
Write-Host ('  ' + $publica)
Write-Host ''
Write-Host ('A chave PRIVADA ficou em ' + $ArquivoDaChave + ' e nao foi mostrada.')
Write-Host 'Agora, so voce:'
Write-Host '  1. abra esse arquivo no Bloco de Notas e guarde o conteudo inteiro no gerenciador de senhas;'
Write-Host '  2. guarde uma segunda copia fora deste PC (um pendrive guardado, por exemplo);'
Write-Host '  3. nunca a cole em chat, e-mail ou servidor: sem ela, nenhum backup abre.'
