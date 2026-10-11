<#
.SYNOPSIS
  Sobe (ou remove) o Postgres DESCARTAVEL dos testes do Aurora Whisper, num
  conteiner Docker. Compativel com o PowerShell 5.1. Quem roda e o Mauro.

.DESCRIPTION
  Os testes com Postgres (migrations, pgmigrate, comando migrate; ADR-019, F1)
  NAO sobem conteiner sozinhos: o "go test ./..." normal nunca toca no Docker.
  Eles so rodam quando a variavel AURORA_TEST_POSTGRES_URL existe, e este
  script e quem sobe o Postgres para ela apontar.

  O que ele faz:
    1. Confere que o Docker esta respondendo. Se nao estiver, manda abrir o
       Docker Desktop e para.
    2. Sobe um conteiner chamado aurora-whisper-pg-teste, da imagem oficial
       postgres:18, com:
         - a porta ligada SO em 127.0.0.1 (nunca na rede);
         - uma senha ALEATORIA gerada na hora (nunca fixa, nunca no repositorio);
         - sem volume persistente: o conteiner e a unica copia, e -Parar o apaga
           por inteiro (com o volume anonimo que a imagem cria);
         - fsync desligado e 1 GB de memoria no maximo: e banco de teste, o que
           importa e ser rapido e descartavel.
    3. Espera o Postgres aceitar conexao e imprime a linha EXATA para voce colar
       no PowerShell, definindo AURORA_TEST_POSTGRES_URL naquela sessao.
    4. Guarda o mesmo endereco em %USERPROFILE%\.aurora-whisper-dev\postgres-de-teste.url
       (desta conta do Windows, fora do repositorio), para quem roda os testes
       sem colar a linha.

  Com -Parar, remove o conteiner, o volume dele e o arquivo do endereco.

  Na primeira vez o Docker baixa a imagem postgres:18 do Docker Hub (algumas
  centenas de MB). O script NAO inicia o Docker Desktop: abri-lo e com voce.

  Cada teste cria o PROPRIO banco (aw_test_<aleatorio>) dentro deste servidor e o
  apaga no fim; o servidor em si fica ate o -Parar.

.PARAMETER Parar
  Remove o conteiner aurora-whisper-pg-teste (e o volume dele).

.PARAMETER Porta
  Porta no 127.0.0.1 desta maquina (padrao 54329, para nao brigar com um
  Postgres local na 5432).

.EXAMPLE
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1

.EXAMPLE
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1 -Parar
#>
[CmdletBinding()]
param(
    [switch]$Parar,

    [ValidateRange(1024, 65535)]
    [int]$Porta = 54329
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

# O nome do conteiner e da imagem: pgtest e a documentacao citam os dois.
$NomeDoConteiner = 'aurora-whisper-pg-teste'
$Imagem = 'postgres:18'

# O repositorio, para imprimir o comando de teste com o caminho completo. O
# script esta em <repo>\scripts\.
$Raiz = (Split-Path -Parent $PSScriptRoot).Replace('\', '/')

# O endereco (com a senha) tambem vai para um arquivo so desta conta do
# Windows, FORA do repositorio, para quem roda os testes sem colar a linha (o
# agente, enquanto o Mauro deixa o banco ligado; decisao de 05-out-2026). O
# -Parar apaga o arquivo junto com o conteiner.
$PastaDoEndereco = Join-Path $env:USERPROFILE '.aurora-whisper-dev'
$ArquivoDoEndereco = Join-Path $PastaDoEndereco 'postgres-de-teste.url'

# Roda "docker <argumentos>" sem nunca lancar excecao: devolve o codigo de saida
# e o texto (stdout e stderr juntos). O Docker escreve os erros em stderr, e o
# PowerShell 5.1 transforma stderr redirecionado em erro quando
# $ErrorActionPreference e Stop; por isso a preferencia muda aqui dentro.
function Docker-Silencioso([string[]]$Argumentos) {
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        # ToString() em cada item: um erro do docker chega como ErrorRecord, e
        # o ToString dele e so a mensagem (sem o "At line:..." do PowerShell).
        $linhas = @(& docker @Argumentos 2>&1 | ForEach-Object { $_.ToString() })
        $codigo = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $anterior
    }
    return [pscustomobject]@{ Codigo = $codigo; Texto = (($linhas -join [Environment]::NewLine).Trim()) }
}

function Falhar([string]$Mensagem) {
    Write-Host ''
    Write-Host ('ERRO: ' + $Mensagem) -ForegroundColor Red
    exit 1
}

# 1. O Docker esta respondendo?
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Falhar 'O comando "docker" nao foi achado. Instale o Docker Desktop (ou abra-o, se ja esta instalado) e rode de novo.'
}
$info = Docker-Silencioso @('info')
if ($info.Codigo -ne 0) {
    Falhar ('O Docker nao esta respondendo. Abra o Docker Desktop, espere ele terminar de iniciar (o icone da baleia para de animar) e rode este script de novo.' +
        [Environment]::NewLine + 'Resposta do Docker: ' + $info.Texto)
}

# ---------------------------------------------------------------------------
# -Parar: remove o conteiner e o volume anonimo dele.
# ---------------------------------------------------------------------------
if ($Parar) {
    $existe = Docker-Silencioso @('container', 'inspect', $NomeDoConteiner)
    if ($existe.Codigo -ne 0) {
        Write-Host ('O conteiner ' + $NomeDoConteiner + ' nao existe: nada a remover.')
    } else {
        $removido = Docker-Silencioso @('rm', '-f', '-v', $NomeDoConteiner)
        if ($removido.Codigo -ne 0) {
            Falhar ('Nao consegui remover o conteiner ' + $NomeDoConteiner + '. Resposta do Docker: ' + $removido.Texto)
        }
        Write-Host ('Conteiner ' + $NomeDoConteiner + ' removido (com o volume dele).')
    }
    if (Test-Path -LiteralPath $ArquivoDoEndereco) {
        Remove-Item -LiteralPath $ArquivoDoEndereco -Force
        Write-Host ('Endereco apagado: ' + $ArquivoDoEndereco)
    }
    Write-Host ''
    Write-Host 'Para limpar a variavel desta sessao do PowerShell:'
    Write-Host '  Remove-Item Env:AURORA_TEST_POSTGRES_URL -ErrorAction SilentlyContinue'
    exit 0
}

# ---------------------------------------------------------------------------
# Subir.
# ---------------------------------------------------------------------------
$jaExiste = Docker-Silencioso @('container', 'inspect', $NomeDoConteiner)
if ($jaExiste.Codigo -eq 0) {
    # Ja ligado e com o endereco guardado: nao e erro, e o caso comum de quem
    # roda o script de novo so para garantir (01-out-2026: o Mauro leu "ERRO"
    # e achou que o roteiro tinha quebrado).
    $rodando = Docker-Silencioso @('container', 'inspect', '-f', '{{.State.Running}}', $NomeDoConteiner)
    if (($rodando.Texto.Trim() -eq 'true') -and (Test-Path -LiteralPath $ArquivoDoEndereco)) {
        Write-Host ''
        Write-Host ('O Postgres de teste ja esta ligado (' + $NomeDoConteiner + '). Nada a fazer: pode seguir.') -ForegroundColor Green
        Write-Host ('O endereco dele esta em ' + $ArquivoDoEndereco + '.')
        exit 0
    }
    Falhar ('O conteiner ' + $NomeDoConteiner + ' ja existe. O endereco dele (com a senha) esta em ' + $ArquivoDoEndereco + ', se ele foi subido por esta versao do script.' +
        [Environment]::NewLine + 'Para recomecar com uma senha nova, rode primeiro:' +
        [Environment]::NewLine + '  powershell -File ' + $Raiz + '/scripts/postgres-de-teste.ps1 -Parar')
}

# Senha aleatoria: 24 bytes do gerador criptografico, em hexadecimal (48
# caracteres, sem nada que precise de escape numa URL).
$bytes = New-Object byte[] 24
$gerador = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $gerador.GetBytes($bytes)
} finally {
    $gerador.Dispose()
}
$senha = -join ($bytes | ForEach-Object { $_.ToString('x2') })

# A porta so no loopback: "127.0.0.1:<porta>:5432". Sem o 127.0.0.1 o Docker
# publicaria em todas as interfaces, e passaria por cima do firewall do Windows.
$publicar = '127.0.0.1:' + $Porta + ':5432'

# Na primeira vez a imagem precisa ser baixada. Isso roda a vista (com a barra de
# progresso do Docker), e nao dentro de Docker-Silencioso, que so mostraria o
# texto no fim.
$imagemLocal = Docker-Silencioso @('image', 'inspect', $Imagem)
if ($imagemLocal.Codigo -ne 0) {
    Write-Host ('Baixando a imagem ' + $Imagem + ' do Docker Hub (so na primeira vez; algumas centenas de MB)...')
    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & docker pull $Imagem
    $codigoDoPull = $LASTEXITCODE
    $ErrorActionPreference = $anterior
    if ($codigoDoPull -ne 0) {
        Falhar ('O Docker nao conseguiu baixar a imagem ' + $Imagem + '. Confira a conexao com a internet e rode de novo.')
    }
}

Write-Host ('Subindo ' + $NomeDoConteiner + ' (' + $Imagem + ') em 127.0.0.1:' + $Porta + ' ...')

# A senha vai pelo ambiente deste processo ("-e POSTGRES_PASSWORD" sem valor
# copia a variavel do ambiente): ela nao aparece na linha de comando do docker.
$env:POSTGRES_PASSWORD = $senha
try {
    $subida = Docker-Silencioso @(
        'run', '-d',
        '--name', $NomeDoConteiner,
        '-e', 'POSTGRES_PASSWORD',
        '-p', $publicar,
        '--memory', '1g',
        $Imagem,
        '-c', 'fsync=off',
        '-c', 'synchronous_commit=off',
        '-c', 'full_page_writes=off'
    )
} finally {
    Remove-Item Env:POSTGRES_PASSWORD -ErrorAction SilentlyContinue
}
if ($subida.Codigo -ne 0) {
    Falhar ('O Docker nao conseguiu subir o conteiner. Se a porta ' + $Porta + ' ja esta em uso, rode de novo com outra: -Porta 54330.' +
        [Environment]::NewLine + 'Resposta do Docker: ' + $subida.Texto)
}

# Espera o Postgres aceitar conexao pela rede. "pg_isready -h 127.0.0.1" so
# responde quando o servidor FINAL esta de pe: durante a inicializacao da imagem
# existe um servidor temporario que escuta so no socket, e ele nao conta. E
# confere tambem, de fora, que a porta do Windows esta abrindo.
$pronto = $false
for ($tentativa = 1; $tentativa -le 60; $tentativa++) {
    $dentro = Docker-Silencioso @('exec', $NomeDoConteiner, 'pg_isready', '-h', '127.0.0.1', '-U', 'postgres')
    if ($dentro.Codigo -eq 0) {
        $cliente = New-Object System.Net.Sockets.TcpClient
        try {
            $cliente.Connect('127.0.0.1', $Porta)
            $pronto = $true
        } catch {
            $pronto = $false
        } finally {
            $cliente.Close()
        }
        if ($pronto) { break }
    }
    Start-Sleep -Seconds 1
}
if (-not $pronto) {
    $logs = Docker-Silencioso @('logs', '--tail', '20', $NomeDoConteiner)
    Falhar ('O Postgres nao ficou pronto em 60 segundos. Ultimas linhas do log:' + [Environment]::NewLine + $logs.Texto +
        [Environment]::NewLine + 'Para remover o conteiner: powershell -File ' + $Raiz + '/scripts/postgres-de-teste.ps1 -Parar')
}

$url = 'postgres://postgres:' + $senha + '@127.0.0.1:' + $Porta + '/postgres?sslmode=disable'

# A pasta e criada se faltar (diretorio, nunca arquivo); o arquivo e reescrito.
if (-not (Test-Path -LiteralPath $PastaDoEndereco)) {
    New-Item -ItemType Directory -Path $PastaDoEndereco | Out-Null
}
[IO.File]::WriteAllText($ArquivoDoEndereco, $url)

Write-Host ''
Write-Host ('Pronto. O Postgres de teste esta em 127.0.0.1:' + $Porta + ' (so este computador alcanca).') -ForegroundColor Green
Write-Host ''
# A senha NAO aparece na tela (06-out-2026: a saida inteira foi colada no
# chat). A linha abaixo le o endereco do arquivo da conta.
Write-Host 'Se for rodar os testes nesta janela, cole esta linha (ela le o endereco do arquivo, sem mostrar a senha):'
Write-Host ''
Write-Host ('$env:AURORA_TEST_POSTGRES_URL = (Get-Content -LiteralPath "' + $ArquivoDoEndereco + '" -Raw).Trim()')
Write-Host ''
Write-Host 'Depois, para rodar os testes que usam o Postgres:'
Write-Host ''
Write-Host ('go -C ' + $Raiz + ' test ./migrations/ ./internal/server/pgmigrate/ ./internal/server/pgtest/ ./cmd/server/ -count=1 -v')
Write-Host ''
Write-Host 'Quando terminar, remova o conteiner (e a variavel):'
Write-Host ''
Write-Host ('powershell -File ' + $Raiz + '/scripts/postgres-de-teste.ps1 -Parar')
Write-Host ''
Write-Host ('A senha foi gerada agora e fica so em ' + $ArquivoDoEndereco + ' (desta conta, fora do repositorio), que o -Parar apaga. Esta saida pode ir para o chat: nao tem a senha.')
