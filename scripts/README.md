# Scripts

Automações, utilitários e harness de benchmark para Aurora Whisper.

## Propósito

Este diretório contém:

- Scripts de setup e inicialização
- Harness de benchmark de ASR (Sprint 3+)
- Utilitários de migração, seed de dados, limpeza
- Scripts de CI/CD local
- `montar-instalador.ps1`: monta o pacote do instalador (staging em `dist\`) e compila o Setup
  com o Inno Setup, se houver (ADR-018). PowerShell 5.1, só ASCII; nunca executa o Agent nem o
  Setup. Veja o `README.md` da raiz, "Montar o Setup".
- `diagnostico-servidor.sh`: diagnóstico **só de leitura** do servidor onde a nuvem pode morar
  (ADR-019, "Hospedagem"). Veja a seção abaixo.
- `postgres-de-teste.ps1`: sobe (ou remove, com `-Parar`) o Postgres **descartável** dos testes da
  nuvem (ADR-019, F1), num contêiner Docker. PowerShell 5.1, só ASCII. Quem roda é o Mauro: nenhum
  teste nem agente o roda. Veja a seção abaixo.
- `nuvem-local.ps1 -Email <conta>`: sobe a nuvem no PC para o teste manual da F3, num comando só
  (confere o Postgres de teste, acha o JSON do client Google e o move de Downloads para
  `%USERPROFILE%\.aurora-whisper-dev`, roda o `migrate`, gera as chaves, compila o app (o painel no
  modo nuvem, F4) em `%USERPROFILE%\.aurora-whisper-dev\painel-nuvem` e sobe o servidor em
  `http://localhost:8080`). As variáveis vivem só no processo do script. Roteiro:
  `docs/teste-manual-nuvem-f3.md`.
- `nuvem-local-teste.ps1 [-Porta 8080] [-SemExemplos]`: a nuvem de **teste** no PC, sem Docker e sem
  Google (ADR-019, F4): compila o app em `%USERPROFILE%\.aurora-whisper-dev\painel-nuvem` e sobe o
  servidor de teste (`go run -tags logindeteste ./cmd/server teste`), na memória, com a conta de teste
  e ditados de exemplo inventados, só em `127.0.0.1`. **O ditado pelo celular (ADR-021) também
  funciona aqui, sem rede e sem chave de verdade:** as rotas `/api/v1/dictation` e `/api/v1/ai-keys` não
  são `404`, a chave de cifra nasce a cada subida (um cofre por processo, que some com ele) e os
  provedores são de mentira. O `POST /api/v1/dictations` joga o áudio fora e devolve a transcrição de
  mentira "este é um ditado de mentira do servidor de teste pelo celular", e o Polish de mentira
  devolve o texto com a primeira letra maiúscula e um ponto no fim. Uma chave colada que começa com
  `recusada`, `sem-credito` ou `fora-do-ar` faz o provedor de mentira recusar a chave, ficar sem crédito
  ou cair, para a tela mostrar cada erro (o banner do servidor os explica). Nada disso existe no build
  normal do servidor. Com `-SemExemplos` (variável `TESTE_SEM_EXEMPLOS=1`), sobe vazia, só com a
  conta: é o que se usa ao conectar o Agent do `bin\`,
  para os exemplos inventados não descerem ao histórico de uso (roteiro da F7). As chaves nascem no
  servidor e nunca aparecem. PowerShell 5.1, só ASCII. Roteiros: `docs/teste-manual-nuvem-f4.md`,
  `docs/teste-manual-nuvem-f7.md` e `docs/teste-manual-nuvem-f8.md`.
- `backup-chave.ps1`: gera, uma vez, a chave do backup da nuvem (o par do `age`) **dentro de um
  contêiner** da imagem do backup, e mostra só a chave pública (ADR-019, F11). Veja a seção abaixo.
- `teste-restauracao-mensal.ps1 -Arquivo <backup baixado do B2>`: o teste de restauração do mês, num
  Postgres descartável dentro de um contêiner; falha também se o backup tem mais de 48 h. Veja a
  seção abaixo.
- `deploy-teste.ps1`, `enviar-para-servidor.ps1 -Dominio <endereço> [-ArquivoGoogle <JSON>]` e
  `conferir-nuvem.ps1 -Dominio <endereço> [-IPEsperado <IP>]`: o deploy da nuvem (ADR-019, F11, Parte
  2). Veja a seção "O deploy da nuvem", abaixo, e o roteiro `docs/deploy-f11.md`.
- `remontar-agent.ps1`: remonta o Agent do `bin\` e o painel num comando só. Fecha o Agent aberto
  como o "Sair" da bandeja, compila, monta o painel em `bin\painel`, abre o Agent novo e confere pelo
  `StartTime` que é ele. Não mexe nos dados. PowerShell 5.1, só ASCII.

## Diagnóstico do servidor (`diagnostico-servidor.sh`)

Para decidir se a nuvem (etapa 5, ADR-019) cabe no servidor que já roda outros serviços, sem
encostar neles. Quem roda é o Mauro, no servidor; ele cola a saída na conversa. Ninguém precisa de
acesso SSH além dele.

**O que ele garante:** só lê. Não escreve nada em disco (nem arquivo temporário: nada de
here-document), não acessa a internet, não instala, não para nem reinicia nada e não usa `sudo` (se
faltar permissão para algo, diz e segue). Não mostra variáveis de ambiente, labels, `docker inspect`
nem logs de contêiner. Endereço IP específico sai como `<ip>`.

**O que ele mostra:** sistema e kernel; CPU e carga; RAM e swap livres; disco livre (e o da pasta de
dados do Docker); versão do Docker e do Compose; contêineres rodando (só nome, imagem, portas e
status) e a memória que cada um usa agora; quem escuta nas portas 80 e 443 (`ss -ltnp`, ou
`netstat`); qual proxy parece estar na frente (Traefik, Nginx, Caddy, Nginx Proxy Manager, HAProxy
ou Apache, pelo nome da imagem, do serviço ou do processo); os nomes das redes do Docker; e o fuso
do sistema. O país e a região do datacenter **não** saem sem chamar um serviço externo: o script
pede que o Mauro os informe (estão no painel do provedor da VPS).

**Como rodar**, sem copiar o arquivo para o servidor (o script entra pela entrada do `ssh`; troque
`usuario@servidor` pelo seu acesso):

```bash
ssh usuario@servidor 'bash -s' < /c/Desenvolvimento/aurora-whisper/scripts/diagnostico-servidor.sh
```

No PowerShell, o `<` não existe; vá pelo `cmd`:

```powershell
cmd /c "ssh usuario@servidor bash -s < C:\Desenvolvimento\aurora-whisper\scripts\diagnostico-servidor.sh"
```

Rode como o seu usuário de sempre. Se ele não estiver no grupo `docker`, as seções do Docker saem
como "sem permissão", e o resto continua valendo.

**O que a saída nunca afirma sem ter lido:**
- "Ninguém escuta em 80 nem em 443" só sai se a leitura deu certo e listou alguma porta. Se o `ss` (ou
  o `netstat`) falhar ou vier vazio, a saída diz "não deu para ler as portas", que **não** quer dizer
  que ninguém escuta. O `ss` é lido sem `-H`, que o `iproute2` antigo não tem.
- Sem acesso ao Docker, "Proxy na frente" não diz que não há proxy: avisa que os contêineres não
  foram vistos e que um proxy em contêiner só aparece nos processos e nas portas. O mesmo vale sem
  `systemctl` (serviços não conferidos) e sem `ps` ou com um `ps` que falha (processos não conferidos).

**Como foi conferido:** `bash -n` (sintaxe); uma execução no Git Bash do Windows; e execuções com
`ss`, `netstat` e `docker` de mentira, num `PATH` sem o resto do sistema, para os casos que importam:
`ss` que falha, que vem vazio ou que lista só outras portas; só `netstat`; `ss` e `netstat` falhando;
Docker sem permissão, parado ou ausente. **Ainda não rodou num Linux de verdade:** o WSL desta máquina
não tem distribuição instalada, e instalar uma é mudar o sistema, então não instalei. A primeira
execução real é a do Mauro no servidor, e a saída diz o que ela não conseguiu ler. O arquivo é só ASCII
e com fim de linha LF (`.gitattributes`).

## Postgres de teste (`postgres-de-teste.ps1`)

Os testes que precisam de um Postgres de verdade (as migrations, o `pgmigrate` e o comando `migrate`;
ADR-019, F1) **não sobem contêiner sozinhos**: o `go test ./...` normal nunca toca no Docker
(`CLAUDE.md`: subir contêiner muda o estado da máquina). Eles só rodam quando existe a variável
`AURORA_TEST_POSTGRES_URL`, e este script é quem sobe o Postgres para ela apontar. Sem a variável,
cada teste pula com uma mensagem que diz como ligar.

**Para ligar** (Docker Desktop aberto; no PowerShell):

```powershell
powershell -File C:/Desenvolvimento/aurora-whisper/scripts/postgres-de-teste.ps1
```

Se o PowerShell reclamar da política de execução, acrescente `-ExecutionPolicy Bypass` antes do
`-File`: vale só para aquele processo.

O script confere que o Docker responde (se não, manda abrir o Docker Desktop e para; ele **não** o
inicia), sobe o contêiner `aurora-whisper-pg-teste` (imagem oficial `postgres:18`), espera o Postgres
aceitar conexão e imprime a linha exata para colar:

```text
$env:AURORA_TEST_POSTGRES_URL = 'postgres://postgres:<senha gerada na hora>@127.0.0.1:54329/postgres?sslmode=disable'
```

O mesmo endereço fica também em `%USERPROFILE%\.aurora-whisper-dev\postgres-de-teste.url`, um arquivo
desta conta do Windows e **fora do repositório** (decisão do Mauro, 05-out-2026): é assim que o agente
roda os testes com Postgres enquanto o Mauro deixa o banco ligado, sem que a linha passe pelo chat.
Num shell do Git Bash, por exemplo:

```bash
AURORA_TEST_POSTGRES_URL="$(cat "$USERPROFILE/.aurora-whisper-dev/postgres-de-teste.url")" go test ./migrations/ -count=1
```

O `-Parar` apaga o arquivo junto com o contêiner.

Cole essa linha no mesmo PowerShell, e então rode os testes (o script também imprime este comando):

```powershell
go -C C:/Desenvolvimento/aurora-whisper test ./migrations/ ./internal/server/pgmigrate/ ./internal/server/pgtest/ ./internal/server/pgstore/ ./cmd/server/ -count=1 -v
```

Com a variável definida, os testes `TestPostgres_*`, `TestRun_*`, `TestInspect_*`,
`TestMigrate_Postgres_*`, `TestContrato_Postgres` e `TestAuth_Postgres` (as suítes de contrato e de
login da nuvem, F2 e F3) e `TestServe_Postgres_*` (o servidor de verdade) rodam em vez de pular. O `go test ./...` inteiro também serve: o que não usa
Postgres roda do mesmo jeito.

**Para remover** (o contêiner e o volume dele; a senha se perde junto):

```powershell
powershell -File C:/Desenvolvimento/aurora-whisper/scripts/postgres-de-teste.ps1 -Parar
Remove-Item Env:AURORA_TEST_POSTGRES_URL -ErrorAction SilentlyContinue
```

**O que ele garante:**
- **Só este computador alcança o banco.** A porta é publicada como `127.0.0.1:<porta>:5432` (padrão
  `54329`, para não brigar com um Postgres local na 5432; mude com `-Porta`). Sem o `127.0.0.1` o
  Docker publicaria em todas as interfaces e passaria por cima do firewall do Windows.
- **A senha é aleatória e gerada na hora** (24 bytes do gerador criptográfico, em hexadecimal). Nunca
  fica no repositório, em arquivo nem na linha de comando do `docker` (vai pelo ambiente do processo).
  Ela só existe na linha que o script imprime: não cole essa linha em chat, commit nem log. Com um
  contêiner já de pé, o script recusa subir outro (a senha do antigo não se recupera): rode `-Parar`
  primeiro.
- **Sem volume persistente.** Não há `-v` nem volume nomeado: o contêiner é a única cópia, e `-Parar`
  o apaga com o volume anônimo que a imagem cria. O servidor roda com `fsync` desligado e no máximo 1
  GB de memória: é banco de teste.
- **Cada teste cria o próprio banco** (`CREATE DATABASE aw_test_<16 hex>`) e o apaga no fim
  (`t.Cleanup`), então uma execução não suja a seguinte e vários pacotes de teste rodam ao mesmo tempo.
  O servidor em si fica até o `-Parar`. Os testes **recusam** uma `AURORA_TEST_POSTGRES_URL` que não
  aponte para a própria máquina (`localhost`, `127.0.0.1`, `::1` ou socket Unix): criar bancos de
  teste num servidor de verdade é o engano que isso impede.

Na primeira vez, o Docker baixa a imagem `postgres:18` do Docker Hub (algumas centenas de MB).

**Como foi conferido:** o parser do PowerShell 5.1 aceitou o arquivo (sem executá-lo), a geração da
senha e as junções de texto foram exercitadas à parte, e `internal/server/pgtest/script_test.go` lê o
texto e confere as promessas acima (loopback, senha aleatória, sem volume, sem iniciar o Docker
Desktop, só ASCII). **O script nunca rodou:** quando foi escrito, o Docker Desktop desta máquina
estava parado e ninguém o iniciou. A primeira execução real é a do Mauro.

## Backup da nuvem: a chave e o teste do mês

O backup da nuvem (ADR-019, "Decisões da F11", D2) é cifrado na VPS com o `age`, para uma chave
**pública**, e enviado ao Backblaze B2. A chave **privada** é só do Mauro. Os dois scripts abaixo são
dele: **nenhum agente nem teste os roda.** O `age` não está no Windows, e nada é instalado: os dois
usam a imagem do backup (`deploy/backup/Dockerfile`) no Docker Desktop, que eles constroem na primeira
vez (o Docker baixa as imagens de base). PowerShell 5.1, só ASCII, CRLF.

**A chave, uma vez, antes do deploy** (Docker Desktop aberto):

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\backup-chave.ps1
```

Ela nasce num contêiner sem rede, com só a pasta `%USERPROFILE%\.aurora-whisper-dev\backup` montada (o
script recusa uma pasta dentro do repositório, e recusa gerar de novo se já há chave: uma chave nova
deixaria os backups já feitos sem quem os abra). A tela mostra só a linha pública `age1...`, que é a
que o `deploy/scripts/gerar-segredos.sh` pergunta na VPS. A privada fica em
`%USERPROFILE%\.aurora-whisper-dev\backup\aurora-whisper-backup.key`: guarde-a no gerenciador de
senhas e numa segunda cópia fora do PC. Se ela se perder, todos os backups se perdem.

**O teste do mês:** baixe à mão, do painel do B2, o backup mais recente
(`aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age`) e rode:

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\teste-restauracao-mensal.ps1 -Arquivo C:\Users\<voce>\Downloads\aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age
```

Ele sobe um Postgres 18 descartável sem rede nenhuma (sem porta), com uma senha aleatória gerada na
hora, decifra e confere o dump dentro do contêiner (o dump decifrado nunca vai para o disco do PC),
restaura parando no primeiro erro, confere que o banco abre com as migrations e as contas, mostra só
as contagens, e remove o contêiner com o volume. Falha também se o backup tem mais de 48 h: é o que
mostra que o backup da VPS parou. Nenhuma credencial do B2 fica no PC.

**Como foi conferido:** o parser do PowerShell 5.1 aceitou os dois (sem executá-los), e
`deploy/pc_test.go` lê o texto e confere as promessas (só ASCII e CRLF, contêiner sem rede, sem porta e
sem privilégio, só a pasta da chave e o arquivo montados, a recusa de uma pasta no repositório, a chave
privada e a senha nunca impressas, o contêiner removido no `finally`, os nomes iguais aos da VPS e o
`.gitignore`). **Nenhum dos dois rodou:** a primeira execução é a do Mauro.

## O deploy da nuvem (F11, Parte 2)

Os três são do Mauro, na ordem do roteiro `docs/deploy-f11.md`; **nenhum agente nem teste os roda**.
PowerShell 5.1, só ASCII, CRLF.

- **`deploy-teste.ps1`** (Docker Desktop aberto): monta as duas imagens de produção e sobe o **mesmo**
  `deploy\compose.yml` da VPS, com o `deploy\compose.teste.yml` por cima, no projeto
  `aurora-whisper-teste`: a nuvem de teste em `https://aurora-whisper.exemplo.test`, um Traefik de teste
  da mesma versão do da VPS, só em `127.0.0.1`, segredos de mentira e uma chave de teste numa pasta
  temporária. Confere o Postgres (os cinco ajustes, os papéis), o `migrate`, a API (a ordem
  arrumação, retenção, servindo), a borda (o HSTS, uma CSP, o Host errado, o http para o https, a
  saúde do job fechada, o log de acesso desligado no nosso router), o backup e a restauração com o
  epoch novo. Derruba tudo no fim, só o que é do teste. Se passou, anota os IDs das imagens em
  `%USERPROFILE%\.aurora-whisper-dev\deploy-teste-aprovado.txt`.
- **`enviar-para-servidor.ps1`**: só manda imagens com os IDs aprovados, por `docker save | ssh
  docker load` (pelo `cmd`, que passa os bytes), mais a pasta `deploy\` e, na primeira vez, o JSON do
  Google (`-ArquivoGoogle`), para `~/aurora-whisper/` na VPS. Numa imagem nova, sem o JSON; para trocar
  o client depois, o roteiro manda o JSON sozinho e o `subir.sh` o troca.
- **`conferir-nuvem.ps1`**: de fora, só GET: o DNS, o certificado (pela cadeia do Windows), o http para
  o https, o health com os cabeçalhos, o painel e a saúde do job fechada.

**Como foi conferido:** o parser do PowerShell 5.1 e `deploy/pc_parte2_test.go`, que lê o texto (só o
que é do teste é tocado, a limpeza no `finally`, a CSP e o HSTS iguais aos do servidor, as conferências
que a ADR pede, as imagens aprovadas, o `cmd` no cano dos bytes, o `conferir` sem `--insecure`).
**Nenhum dos três rodou.**

## Benchmark de ASR

O harness de benchmark de ASR (conforme `docs/ai-spec/19-eval-plan.md`) será implementado no **Sprint 3**.

### Dataset de voz

**Importante:** os arquivos de áudio do dataset de benchmark **não são versionados no git** — são bloqueados em `.gitignore` por causa da invariante AC-028 (privacidade).

O dataset é referenciado por hash SHA256 no relatório final de benchmark. Consulte `docs/ai-spec/19-eval-plan.md` para detalhes sobre:

- Como obter o dataset
- Como executar o harness
- Como validar a integridade via hash
- Formato esperado do relatório

## Referência

- `docs/ai-spec/19-eval-plan.md` — plano de avaliação completo
- `.gitignore` — regras de exclusão (audio/)
