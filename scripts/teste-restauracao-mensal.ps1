<#
.SYNOPSIS
  O teste de restauracao do mes: decifra um backup da nuvem e o restaura num
  Postgres DESCARTAVEL, tudo dentro de um conteiner. Compativel com o
  PowerShell 5.1. Quem roda e o Mauro, uma vez por mes.

.DESCRIPTION
  A ADR-019 pede um teste de restauracao por mes, num conteiner descartavel. E
  tambem o que mostra se o backup parou: o script FALHA se o backup tem mais
  de 48 horas (a data esta no nome do arquivo).

  O arquivo cifrado e o que voce baixou a mao do painel do Backblaze B2
  (aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age): nenhuma credencial do B2 fica no
  PC. A chave privada e a que o scripts\backup-chave.ps1 gerou.

  O que ele faz:
    1. confere que o Docker responde (ele NAO inicia o Docker Desktop);
    2. confere o arquivo, a idade dele e a chave (fora do repositorio);
    3. sobe um Postgres 18 descartavel da imagem do backup: sem rede nenhuma
       (sem porta publicada), com uma senha aleatoria gerada na hora, com a
       chave e o arquivo montados so para leitura;
    4. dentro dele, decifra com o age e confere o dump (pg_restore --list), e
       restaura num banco novo, parando no primeiro erro. O dump decifrado
       passa so por cano, dentro do conteiner: nao vai para o disco do PC;
    5. confere que o banco abre, com as migrations, as contas e os ditados, e
       mostra so as contagens;
    6. remove o conteiner (com o volume), de certo ou de errado.

.PARAMETER Arquivo
  O backup cifrado baixado do B2 (aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age).

.PARAMETER PastaDaChave
  Onde esta a chave privada. Padrao: %USERPROFILE%\.aurora-whisper-dev\backup.

.PARAMETER MaxHoras
  A idade maxima do backup, em horas (padrao 48).

.EXAMPLE
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\teste-restauracao-mensal.ps1 -Arquivo C:\Users\<usuario>\Downloads\aurora-whisper-20261009T030000Z.dump.age
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Arquivo,

    [string]$PastaDaChave = (Join-Path $env:USERPROFILE '.aurora-whisper-dev\backup'),

    [ValidateRange(1, 720)]
    [int]$MaxHoras = 48
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$Imagem = 'aurora-whisper-backup:producao'
$NomeDaChave = 'aurora-whisper-backup.key'
$NomeDoConteiner = 'aurora-whisper-restauracao-mensal'

# O repositorio: o script esta em <repo>\scripts\.
$Raiz = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))

# Roda "docker <argumentos>" sem nunca lancar excecao: devolve o codigo de saida
# e o texto (stdout e stderr juntos).
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

function Dentro-De([string]$Caminho, [string]$Pasta) {
    $c = $Caminho.TrimEnd('\') + '\'
    $p = $Pasta.TrimEnd('\') + '\'
    return $c.StartsWith($p, [StringComparison]::OrdinalIgnoreCase)
}

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

# 2. O arquivo, a idade dele e a chave.
$ArquivoCheio = [IO.Path]::GetFullPath($Arquivo)
if (-not (Test-Path -LiteralPath $ArquivoCheio -PathType Leaf)) {
    Falhar ('Nao achei o arquivo ' + $ArquivoCheio + '.')
}
$nomeDoArquivo = Split-Path -Leaf $ArquivoCheio
if (-not ($nomeDoArquivo -match '^aurora-whisper-(\d{8}T\d{6}Z)\.dump\.age$')) {
    Falhar ('O nome do arquivo deveria ser aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age (o que o backup sobe ao B2), e e ' + $nomeDoArquivo + '.')
}
$estilo = [Globalization.DateTimeStyles]::AssumeUniversal -bor [Globalization.DateTimeStyles]::AdjustToUniversal
$feitoEm = [DateTime]::ParseExact($Matches[1], "yyyyMMdd'T'HHmmss'Z'", [Globalization.CultureInfo]::InvariantCulture, $estilo)
$horas = [int][Math]::Floor(([DateTime]::UtcNow - $feitoEm).TotalHours)

$PastaDaChaveCheia = [IO.Path]::GetFullPath($PastaDaChave)
if (Dentro-De $PastaDaChaveCheia $Raiz) {
    Falhar ('A pasta da chave nao pode ficar dentro do repositorio (' + $Raiz + ').')
}
if (-not (Test-Path -LiteralPath (Join-Path $PastaDaChaveCheia $NomeDaChave) -PathType Leaf)) {
    Falhar ('Nao achei a chave privada em ' + (Join-Path $PastaDaChaveCheia $NomeDaChave) + ' (o scripts\backup-chave.ps1 a gera).')
}

Garantir-Imagem

# Uma sobra de uma vez anterior (o conteiner e nosso e descartavel).
$sobra = Docker-Silencioso @('container', 'inspect', $NomeDoConteiner)
if ($sobra.Codigo -eq 0) {
    $removida = Docker-Silencioso @('rm', '-f', '-v', $NomeDoConteiner)
    if ($removida.Codigo -ne 0) {
        Falhar ('Ficou um ' + $NomeDoConteiner + ' de uma vez anterior, e nao consegui remove-lo.')
    }
}

# Senha aleatoria: 24 bytes do gerador criptografico, em hexadecimal. Vai pelo
# ambiente deste processo ("-e POSTGRES_PASSWORD" sem valor): nao aparece na
# linha de comando do docker.
$bytes = New-Object byte[] 24
$gerador = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $gerador.GetBytes($bytes)
} finally {
    $gerador.Dispose()
}
$senha = -join ($bytes | ForEach-Object { $_.ToString('x2') })

$restaurou = $false
$contagens = ''
$motivo = ''
$env:POSTGRES_PASSWORD = $senha
try {
    # 3. O Postgres descartavel: sem rede, sem porta, so leitura no que monta.
    $subida = Docker-Silencioso @(
        'run', '-d',
        '--name', $NomeDoConteiner,
        '--network', 'none',
        '--memory', '1g',
        '-e', 'POSTGRES_PASSWORD',
        '-v', ($PastaDaChaveCheia + ':/chave:ro'),
        '-v', ($ArquivoCheio + ':/entrada/backup.dump.age:ro'),
        $Imagem,
        'postgres', '-c', 'fsync=off', '-c', 'synchronous_commit=off'
    )
    Remove-Item Env:POSTGRES_PASSWORD -ErrorAction SilentlyContinue
    if ($subida.Codigo -ne 0) {
        $motivo = 'o Docker nao conseguiu subir o Postgres descartavel'
    } else {
        $pronto = $false
        for ($tentativa = 1; $tentativa -le 60; $tentativa++) {
            # Sem rede, o pg_isready vai pelo socket, e o servidor temporario
            # da inicializacao tambem responde por ele. Por isso, primeiro a
            # linha do fim da inicializacao no log da imagem; so depois dela o
            # pg_isready e o do servidor de verdade.
            $logs = Docker-Silencioso @('logs', $NomeDoConteiner)
            if ($logs.Texto -match 'PostgreSQL init process complete') {
                $p = Docker-Silencioso @('exec', $NomeDoConteiner, 'pg_isready', '-q', '-U', 'postgres', '-d', 'postgres')
                if ($p.Codigo -eq 0) { $pronto = $true; break }
            }
            Start-Sleep -Seconds 1
        }
        if (-not $pronto) {
            $motivo = 'o Postgres descartavel nao ficou pronto em 60 segundos'
        } else {
            # 4. Decifrar, conferir e restaurar, tudo dentro do conteiner.
            # O pg_restore --list le so o cabecalho e o indice, do comeco do
            # fluxo, e sai; sem o cat que drena o resto, o age morreria de
            # SIGPIPE (141) num dump maior que o cano (64 KiB), e o pipefail
            # faria o teste falhar com um backup bom. O codigo do pg_restore
            # e o que vale (o "if" o guarda, e o set -e nao corta o dreno).
            $decifrar = 'age --decrypt --identity /chave/' + $NomeDaChave + ' /entrada/backup.dump.age'
            $lista = Docker-Silencioso @('exec', $NomeDoConteiner, 'bash', '-c',
                ('set -euo pipefail; ' + $decifrar + ' | { if pg_restore --list > /dev/null; then rc=0; else rc=$?; fi; cat > /dev/null; exit $rc; }'))
            if ($lista.Codigo -ne 0) {
                $motivo = 'o arquivo nao decifrou com esta chave, ou nao e um dump (pg_restore --list)'
            } else {
                $banco = Docker-Silencioso @('exec', $NomeDoConteiner, 'createdb', '-U', 'postgres', 'aurora')
                $restaurar = Docker-Silencioso @('exec', $NomeDoConteiner, 'bash', '-c',
                    ('set -euo pipefail; ' + $decifrar + ' | pg_restore -U postgres -d aurora --no-owner --no-privileges --exit-on-error --single-transaction'))
                if (($banco.Codigo -ne 0) -or ($restaurar.Codigo -ne 0)) {
                    $motivo = 'o pg_restore parou num erro'
                } else {
                    # 5. O banco abre, com o que se espera dele. So contagens.
                    $consulta = "SELECT concat_ws(' ', (SELECT count(*) FROM schema_migrations), (SELECT count(*) FROM users), (SELECT count(*) FROM transcriptions))"
                    $numeros = Docker-Silencioso @('exec', $NomeDoConteiner, 'psql', '-X', '-A', '-t', '-q', '-U', 'postgres', '-d', 'aurora', '-c', $consulta)
                    $partes = $numeros.Texto.Trim() -split ' '
                    if (($numeros.Codigo -ne 0) -or ($partes.Count -ne 3)) {
                        $motivo = 'o banco restaurado nao tem as tabelas esperadas'
                    } elseif ([int]$partes[0] -lt 1) {
                        $motivo = 'o banco restaurado nao tem nenhuma migration aplicada'
                    } elseif ([int]$partes[1] -lt 1) {
                        $motivo = 'o banco restaurado nao tem nenhuma conta'
                    } else {
                        $restaurou = $true
                        $contagens = ($partes[0] + ' migration(s), ' + $partes[1] + ' conta(s), ' + $partes[2] + ' ditado(s)')
                    }
                }
            }
        }
    }
} finally {
    Remove-Item Env:POSTGRES_PASSWORD -ErrorAction SilentlyContinue
    # 6. O conteiner sai, com o volume, de certo ou de errado.
    $null = Docker-Silencioso @('rm', '-f', '-v', $NomeDoConteiner)
}

Write-Host ''
$tudoCerto = $true
if ($restaurou) {
    Write-Host ('OK: o backup decifrou e restaurou: ' + $contagens + '.') -ForegroundColor Green
} else {
    Write-Host ('FALHOU: a restauracao nao deu certo: ' + $motivo + '.') -ForegroundColor Red
    $tudoCerto = $false
}
if ($horas -gt $MaxHoras) {
    Write-Host ('FALHOU: o backup e de ' + $horas + ' h atras (mais de ' + $MaxHoras + ' h). O backup da VPS parou? Confira o conteiner do backup.') -ForegroundColor Red
    $tudoCerto = $false
} else {
    Write-Host ('OK: o backup e de ' + $horas + ' h atras (no maximo ' + $MaxHoras + ' h).') -ForegroundColor Green
}
Write-Host ('O conteiner ' + $NomeDoConteiner + ' foi removido. Apague o arquivo baixado quando quiser: ele e cifrado, mas nao precisa ficar.')
if ($tudoCerto) { exit 0 }
exit 1
