# O deploy da nuvem, passo a passo (F11)

**O que é e por que:** este roteiro põe a nuvem do Aurora Whisper no ar em
**`https://aurora-whisper.mjj.net.br`**, na **sua VPS**, atrás do Traefik que já atende os outros sites
dela. Cada passo diz o porquê e traz o comando completo, pronto para colar. Você roda tudo; eu não rodo
nada no servidor nem no Docker.

Esse endereço é o da **instância do mantenedor**, fechada às contas de `CONTAS_PERMITIDAS`: não é um
serviço aberto. Para subir a sua nuvem, troque `aurora-whisper.mjj.net.br` pelo seu domínio em todos os
comandos daqui (o `gerar-segredos.sh` o pergunta e o grava no `config.env`).

ADR-019, "Decisões da F11" (a D1 que você confirmou em 09-out-2026: a VPS, o Traefik dela, o
domínio e o datacenter no Brasil). Tempo estimado: 1 h 30 a 2 h, fora a espera do B2.

**A ordem, em uma linha:** o Google no PC (feito) → o B2 → a chave do backup → o teste no Docker do PC →
mandar para a VPS → os segredos na VPS → subir → conferir de fora → o primeiro backup e a restauração
dele no PC. Depois, o `docs/teste-final-f11.md`.

**O ditado pelo celular (ADR-021) acrescenta o passo 10:** a chave de cifra das chaves da IA (numa VPS
que **já está no ar**, ela vem **antes** do `subir.sh`), as migrations 0017 a 0019 e, **antes do
primeiro áudio**, a leitura só de leitura do proxy e da swap.

## Antes de começar (são cinco avisos)

1. **A revisão de LGPD vem antes do passo 7.** O passo 7 roda o primeiro `migrate` em produção, e
   depois dele as migrations congelam (`migrations/README.md`): se a sua leitura do dossiê pedir mudança
   no banco, ela entra **antes**. Leia o `docs/lgpd-dossie.md` e responda "LGPD aprovada" (ou o que
   mudar). Sem o OK, **nenhum ditado seu sobe** (o `docs/teste-final-f11.md` é o primeiro a mandar).
2. **Nada do Traefik nem dos outros sites é tocado.** A nuvem entra só com os rótulos do próprio
   contêiner. Os comandos daqui que olham o Traefik só leem (`docker logs`, `docker network inspect`,
   `docker inspect`).
   Nunca rode `docker compose` na pasta do compose do Traefik nem reinicie o contêiner `traefik`.
3. **Nunca `docker compose down -v` na nuvem.** O `-v` apaga o volume do Postgres, com tudo. Para parar,
   o passo "Se algo der errado" tem o comando certo.
4. **A CPU da VPS pode já estar ocupada** pelos outros serviços do servidor. A nuvem tem limite de
   memória e de CPU e, na disputa, **peso de CPU menor** que o padrão dos outros contêineres (eles ganham
   a vez), então ela pode responder mais devagar nos picos deles. Se um dos outros serviços do servidor
   ficar lento depois do passo 7, me conte (é critério de parar, no `12-rollout.md`).
5. **Duas janelas.** Uma do PowerShell, no PC. Outra com uma sessão `ssh` na VPS, aberta só nos passos
   que dizem "na VPS". Os comandos usam `vps` como o apelido do seu acesso ssh (o `Host` do seu
   `~/.ssh/config`); se o seu tiver outro nome, troque-o nos comandos e passe `-Servidor` ao
   `enviar-para-servidor.ps1`:

```powershell
ssh vps
```

## 1. O Google no PC (feito)

**Por que:** o login com o Google de verdade (o PKCE, o aviso de "app não verificado", a recusa de uma
conta fora da lista) já foi provado no PC com o `scripts\nuvem-local.ps1`. O client Web que você criou
tem os dois endereços de volta: `http://localhost:8080/auth/google/callback` e
`https://aurora-whisper.mjj.net.br/auth/google/callback`.

**Confira só uma coisa:** o JSON do client. O `nuvem-local.ps1` o guardou em
`%USERPROFILE%\.aurora-whisper-dev\google-client-dev.json` (fora do repositório). Abra-o no Bloco de
Notas (só você o vê; não cole no chat) e procure `https://aurora-whisper.mjj.net.br/auth/google/callback`.
Se estiver lá, é o mesmo arquivo que vai para a VPS no passo 5. Se não estiver (o JSON foi baixado antes
de você pôr o endereço de produção), baixe-o de novo no Google Cloud (**APIs e serviços** >
**Credenciais** > o client > **Baixar JSON**) e use o caminho do novo no passo 5 (ele cai em
**Downloads**; o `enviar-para-servidor.ps1` recusa um JSON dentro do repositório).

## 2. O Backblaze B2

**Por que:** é para onde vai o backup cifrado, num bucket que ninguém esvazia antes de 30 dias.

Siga o `docs/backup-b2-f11.md` inteiro. No fim, você tem no gerenciador de senhas: o **nome do bucket**,
o **keyID** e a **applicationKey** da chave `aurora-whisper-backup` (só `listBuckets`, `listFiles` e
`writeFiles`). E me diz a região da conta.

## 3. A chave do backup (se ainda não fez)

**Por que:** o backup é cifrado para uma chave **pública**; a **privada**, que abre os backups, só existe
no seu PC e no gerenciador de senhas. Ela nasce num contêiner (o `age` não está no Windows).

Com o Docker Desktop aberto:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\backup-chave.ps1
```

**Deve aparecer:** "OK: a chave do backup foi gerada." e uma linha `age1...` (a pública; ela não é
segredo, mas também não precisa ir ao chat). Guarde a **privada** (o arquivo que o script diz) no
gerenciador de senhas e numa segunda cópia fora do PC: **perdê-la é perder todos os backups.**

Se aparecer "Ja existe uma chave em…", ela já foi feita: siga. Para ver a pública de novo:

```powershell
Get-Content $env:USERPROFILE\.aurora-whisper-dev\backup\aurora-whisper-backup.pub
```

## 4. O teste no Docker do PC

**Por que:** antes de qualquer coisa ir para a VPS, as duas imagens de produção são montadas e a nuvem
sobe **inteira** no seu PC, com o mesmo `deploy\compose.yml` da VPS: o Postgres, o `migrate`, a API, um
Traefik de teste (o da versão fixada no `deploy\compose.teste.yml`, que deve ser a mesma do Traefik do
seu servidor), o backup e a restauração com o epoch novo. Tudo com segredos
de mentira, só em `127.0.0.1`, e tudo é derrubado no fim. O que passar aqui é exatamente o que vai
para a VPS.

Com o Docker Desktop aberto:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\deploy-teste.ps1
```

Na primeira vez, o Docker baixa as imagens de base (o Node, o Go, o Postgres, o Traefik): pode levar 10
a 20 minutos. Depois, uns 5.

**Deve aparecer:** uma sequência de `OK:` (as imagens, os segredos de mentira, o Postgres com os cinco
ajustes, as migrations, a API saudável com a ordem arrumação, retenção, servindo, o https com o HSTS e a
CSP, o `Permissions-Policy` do microfone, as rotas do ditado no celular montadas (a chave de cifra de
mentira chegou ao servidor), o Host errado, o log de acesso desligado no nosso router, o backup, a restauração com o epoch
novo) e, no fim, **"OK: o deploy passou no Docker do PC"**. As imagens aprovadas ficam anotadas em
`%USERPROFILE%\.aurora-whisper-dev\deploy-teste-aprovado.txt`.

Se aparecer um `AVISO:` sobre o redirecionamento do http no log de acesso, siga (é só informação; me
conte). Se aparecer **FALHOU**, o script já derrubou tudo: me mande a linha do FALHOU e as linhas de log
que ele mostrou. Nada foi para a VPS.

**Ainda no PC, uma vez, logo depois do `deploy-teste.ps1`:** a imagem final do servidor (a
`distroless`) não tem número de versão, só um resumo (o digest). Para fixá-la no `deploy\Dockerfile`
depois (fica para mim), me mande o resumo. Ele vem do registro, e não da lista de imagens do Docker: a
imagem de base de um build fica no cache do BuildKit, e um `docker image inspect` dela daria "No such
image".

```powershell
docker buildx imagetools inspect gcr.io/distroless/static-debian12:nonroot
```

**Deve aparecer:** três linhas no topo, `Name:`, `MediaType:` e `Digest: sha256:…`; me mande a do
`Digest:`. É o resumo do nome `nonroot` agora, e o nome anda de tempos em tempos: rodado logo depois do
teste, é o que o teste usou. Quando eu o fixar no `deploy\Dockerfile`, o `deploy-teste.ps1` seguinte
prova a imagem com ele, antes de qualquer imagem nova ir para a VPS.

## 5. Mandar para a VPS

**Por que:** a imagem vai por `ssh` (`docker save` no PC, `docker load` na VPS), sem registro e sem
`git push`. O script recusa mandar uma imagem que não seja a que passou no passo 4. Junto vão a pasta
`deploy\` (o compose e os scripts da VPS) e o JSON do Google, para `~/aurora-whisper/` na VPS, fora de
qualquer repositório.

Antes, confira que o `ssh` chega à VPS sem pedir nada:

```powershell
ssh vps true
```

**Deve aparecer:** nada (é o certo). Se pedir senha ou disser que não conhece o nome, me conte.

Depois, com o JSON do passo 1 (se você baixou um novo, troque o caminho pelo dele):

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\enviar-para-servidor.ps1 -Dominio aurora-whisper.mjj.net.br -ArquivoGoogle $env:USERPROFILE\.aurora-whisper-dev\google-client-dev.json
```

**Deve aparecer:** "OK: as duas imagens sao as que passaram no deploy-teste.ps1", "OK: o JSON do
Google, com o redirect https://aurora-whisper.mjj.net.br/auth/google/callback", o progresso do `docker
load` ("Loaded image: aurora-whisper-server:producao" e a do backup), e "OK: tudo na VPS.". Leva
alguns minutos (são umas centenas de MB pela internet).

## 6. Os segredos, na VPS

**Por que:** a senha do Postgres, as chaves do servidor (a da sessão, o pepper dos aparelhos e a chave
de cifra das chaves da IA do celular) e a configuração nascem **na VPS**, de números aleatórios, e nunca
passam pelo chat nem pelo PC. O script também confere o JSON do Google e o move para a pasta de
segredos. (Este passo é de uma VPS **nova**: ele recusa rodar quando `segredos/` já existe. Numa VPS que
já está no ar, a chave de cifra nasce pelo passo 10.1.)

Na sessão `ssh` (a janela da VPS):

```bash
bash ~/aurora-whisper/deploy/scripts/gerar-segredos.sh
```

Ele pergunta, uma coisa de cada vez:

| Pergunta | O que responder |
|---|---|
| O endereço da nuvem | `aurora-whisper.mjj.net.br` |
| O e-mail que pode entrar | o da sua conta Google |
| O nome do bucket do B2 | o do passo 2 |
| A keyID (não aparece na tela) | o keyID da chave `aurora-whisper-backup` |
| A application key (não aparece) | a applicationKey dela (começa com `K`) |
| A chave PÚBLICA do backup (não aparece) | a linha `age1...` do passo 3 |

**Deve aparecer:** "OK: a chave publica do backup: age1…", "OK: segredos em …" e "OK: configuracao em
…", e o lembrete do `DEVICE_TOKEN_PEPPER`. **Guarde uma cópia dele no gerenciador de senhas agora**
(perdê-lo desconecta todos os PCs). Para vê-lo, só no seu terminal da VPS:

```bash
cat ~/aurora-whisper/segredos/device-token-pepper
```

A chave de cifra (`segredos/ai-keys-secret`) **não tem cópia fora da VPS** (ADR-021, escolha menor 11):
perdê-la faz cada pessoa colar a chave da IA de novo no app do celular, e nenhum ditado se perde. Por
isso o script não a mostra, e você não precisa guardá-la.

Se aparecer **FALHOU**, nada foi gravado: leia a frase (ela diz o que estava errado) e rode de novo.

### 6.1. Conferir a chave do B2, antes do primeiro backup

**Por que:** prova, do lado da VPS, que a chave sobe e lista, e que **não** consegue apagar de vez o que
subiu.

```bash
bash ~/aurora-whisper/deploy/scripts/conferir-b2.sh
```

**Deve aparecer:** três `OK:` ("a chave sobe arquivos…", "a chave lista o que subiu", "a chave nao
apaga de vez o que subiu") e o lembrete do que se confere no painel (o que você já fez no
`docs/backup-b2-f11.md`). Um arquivo pequeno de conferência fica no bucket, de propósito.

## 7. Subir a nuvem, na VPS

**Antes: o OK da LGPD** (aviso 1). Este passo roda o primeiro `migrate` em produção.

**Por que:** o `subir.sh` faz a sequência inteira, conferindo cada passo e parando no primeiro erro: o
relógio da VPS (NTP), a sub-rede da rede do Traefik (vira o `PROXY_CONFIAVEL`), o compose, o Postgres e
os cinco ajustes de log, as migrations, a API (que roda a arrumação e a retenção **antes** de abrir a
porta), o backup e, por fim, o https pelo Traefik.

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

Ele para duas vezes e pergunta:

1. "**Ha migration pendente** … Digite SIM para rodar o migrate agora": digite `SIM` (na primeira vez
   são as 19, da `0001` à `0019`; numa VPS que já estava no ar, só as pendentes: veja o passo 10.2).
2. "**Digite SIM para subir a API**": digite `SIM`. É a hora em que a nuvem passa a responder na
   internet.

**Deve aparecer**, nessa ordem: "OK: o Docker, o compose e as duas imagens…", "OK: o relogio
sincronizado (NTP)", "OK: PROXY_CONFIAVEL=…", "OK: o compose conferido", "OK: o Postgres de pe e
saudavel", os `OK:` dos cinco ajustes e dos papéis, "OK: as migrations em dia", "OK: a API saudavel:
arrumacao, retencao e so entao servindo, com o proxy confiavel ligado", "OK: a saude do job de retencao
(healthcheck -retencao)", "OK: o agendador do backup de pe", "OK: https://aurora-whisper.mjj.net.br
responde pelo Traefik, com o certificado valido" e "OK: a nuvem esta no ar".

A espera mais longa é a do certificado, na primeira vez (o Traefik pede ao Let's Encrypt): até 3
minutos. Se o último passo der **FALHOU** ("nao respondeu 200"), a nuvem já está de pé: me mande a
frase. Para ver, só lendo, o que o Traefik disse do certificado:

```bash
docker logs --since 15m traefik 2>&1 | grep -i -e acme -e aurora-whisper
```

## 8. Conferir de fora, e entrar uma vez

**Por que:** o `subir.sh` olhou de dentro da VPS. Este olha como o celular e o PC vão olhar, pela
internet: o DNS (sem proxy na frente), o certificado, o http indo para o https, os cabeçalhos de
segurança do servidor (uma CSP só, sem a do Traefik por cima) e a rota da saúde do job fechada por fora.
Só lê.

No PowerShell do PC (troque `<IP-DA-VPS>` pelo IP do seu servidor):

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio aurora-whisper.mjj.net.br -IPEsperado <IP-DA-VPS>
```

**Deve aparecer:** "OK: o DNS: aurora-whisper.mjj.net.br -> <IP-DA-VPS>", "OK: o certificado vale
para…, emitido por … Let's Encrypt…", e os `OK:` do http, do health, do painel e da saúde do job, e
no fim "OK: a nuvem em https://aurora-whisper.mjj.net.br responde certo de fora".

**O log de acesso do Traefik, desligado só para a nuvem** (na VPS, só leitura). Antes, onde o Traefik
grava o log de acesso (só os argumentos dele, sem o ambiente):

```bash
docker inspect traefik --format '{{json .Args}}'
```

**Deve aparecer:** uma lista com `"--accesslog=true"`, e **sem** `--accesslog.filepath` e sem
`--accesslog.format` (é o que está no compose do Traefik da VPS: o log de acesso no formato comum, na
saída do contêiner). Se aparecer um dos dois, **pare** e me mande só esses itens: as contagens abaixo
não valeriam.

Os pedidos que o `conferir-nuvem.ps1` acabou de fazer passaram pelo router da nuvem; nenhum pode estar
no log de acesso. As duas contagens olham **só as linhas do log de acesso** (nelas, o nome do router vem
entre aspas, entre o número do pedido e o endereço do serviço, antes do tempo em `ms`), e não as do log
do próprio Traefik, que sai no mesmo lugar e também cita o router (um erro do certificado, por
exemplo). A primeira conta os pedidos da nuvem; a segunda, os de qualquer site, para provar que o log de
acesso está ligado:

```bash
docker logs --since 15m traefik 2>&1 | grep -cE '[0-9]+ "aurora-whisper-api@docker" "[^"]*" [0-9]+ms'
```

```bash
docker logs --since 15m traefik 2>&1 | grep -cE '[0-9]+ "[^"]+@docker" "[^"]*" [0-9]+ms'
```

**Deve aparecer:** `0` no primeiro. O segundo conta os pedidos dos outros sites nesses 15 minutos (se
der `0` também, abra um dos outros sites no navegador e rode de novo: tem de passar de zero). Se o
primeiro passar de zero, **pare** e me conte: o rótulo do log de acesso não valeu no Traefik da VPS.

**Entrar uma vez (cria a sua conta, sem nenhum ditado):** no navegador do PC, abra
`https://aurora-whisper.mjj.net.br` e clique em **Entrar com o Google**, com a conta do e-mail do passo
6. **Deve aparecer:** o app, com o histórico vazio. O backup do passo 9 precisa dessa conta (o teste de
restauração confere que há pelo menos uma).

**A outra conta é recusada:** numa janela anônima, abra o mesmo endereço e entre com a sua **segunda**
conta Google (a de fora da lista). **Deve aparecer:** a recusa da Aurora, e nada é criado. Se ela
entrar, **pare tudo** (é critério de parar o rollout) e me conte.

## 9. O primeiro backup, e a restauração dele no PC

**Por que:** prova, de ponta a ponta e com dado de verdade (a sua conta), que o backup cifra, sobe, e
que a sua chave privada o abre e o restaura. É o mesmo teste que você vai fazer uma vez por mês.

### 9.1. O backup, na VPS

```bash
docker exec aurora-whisper-backup backup.sh
```

**Deve aparecer:** uma linha `backup: OK (aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age, N bytes, N s)`.
Daqui a uns 5 minutos, a saúde do contêiner passa a `healthy`:

```bash
docker ps --filter name=aurora-whisper --format "table {{.Names}}\t{{.Status}}"
```

**Deve aparecer:** os três (`aurora-whisper-api`, `aurora-whisper-postgres` e `aurora-whisper-backup`)
com `(healthy)`. Dali em diante, o backup roda sozinho às 03:00 de São Paulo.

### 9.2. Baixar o backup do B2 (no navegador do PC)

No painel do B2: **Browse Files** > o seu bucket > `aurora-whisper/` > o arquivo
`aurora-whisper-…dump.age` com a data de hoje > **Download**. Ele cai em **Downloads**. (O
`conferencia-da-chave-….txt` do passo 6.1 também está lá: deixe.)

### 9.3. Restaurar no PC, num contêiner descartável

Com o Docker Desktop aberto, troque o nome do arquivo pelo que você baixou:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\teste-restauracao-mensal.ps1 -Arquivo $env:USERPROFILE\Downloads\aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age
```

**Deve aparecer:** "OK: o backup decifrou e restaurou: 19 migration(s), 1 conta(s), 0 ditado(s)." e
"OK: o backup e de 0 h atras". O contêiner é removido no fim. Apague o arquivo baixado quando quiser.

Pronto: a nuvem está no ar, com backup. O próximo é o `docs/teste-final-f11.md` (o celular, o PC
conectado e os seus ditados), **só depois do OK da LGPD**.

## 10. O ditado pelo celular (ADR-021): a VPS que já está no ar, e a leitura antes do primeiro áudio

**Por que:** no ditado pelo celular, o app manda a voz ao servidor, e o servidor a repassa ao provedor de
IA com a chave **da própria pessoa** (ela a cola no app). O servidor guarda essa chave **cifrada**, e só
liga as rotas do ditado se tiver a **chave de cifra** (o arquivo `segredos/ai-keys-secret`). O compose
novo **exige** o arquivo: sem ele, o `docker compose up` e o `subir.sh` recusam. O áudio passa pela
memória da API e é descartado ao fim; nunca vai a disco, a swap, a log nem ao banco. Este passo tem duas
partes: o que muda numa VPS que **já estava no ar** (10.1 e 10.2) e as duas leituras, **só leitura**,
que valem para **qualquer** VPS e vêm **antes do primeiro áudio** (10.3 e 10.4). Numa VPS **nova**, o
`gerar-segredos.sh` do passo 6 já criou a chave de cifra e o `subir.sh` do passo 7 já aplicou as 19
migrations: só sobram o 10.3 e o 10.4.

**Pontos de parada (são seus):** o deploy, as migrations 0017 a 0019, o `memswap_limit`, o `ulimits` e a
chave de cifra nova. Você roda cada um, na VPS, pelos comandos daqui; eu não rodo nada. E o **primeiro
áudio real** só sai do aparelho depois de duas coisas: o seu OK na política e no dossiê reescritos (D0,
na ADR-021) e as leituras do 10.3 e do 10.4 sem nenhum **PARE**.

### 10.1. A chave de cifra, antes do `subir.sh` (VPS que já está no ar)

**Por que:** o `gerar-segredos.sh` recusa rodar quando a pasta `segredos/` existe (um segredo novo
desconectaria todos os PCs), então ele não cria o arquivo novo. O `criar-chave-de-cifra.sh` cria **só**
`segredos/ai-keys-secret`: 32 bytes de `/dev/urandom` em hexadecimal (64 caracteres), `0444` na pasta
`0700`, sem mostrar o valor e sem tocar nos outros arquivos. Ele vem **antes** do `subir.sh`, porque o
`subir.sh` confere os segredos no começo e para sem o arquivo.

Primeiro, a imagem nova e a pasta `deploy\` nova, do PC, **sem** o `-ArquivoGoogle` (veja "Uma imagem
nova, depois", em "Se algo der errado", que também manda rodar o passo 4 antes). Depois, na VPS:

```bash
bash ~/aurora-whisper/deploy/scripts/criar-chave-de-cifra.sh
```

**Deve aparecer:** "OK: chave de cifra em …/segredos/ai-keys-secret (0444, na pasta 0700, para o
conteiner da API ler)" e o aviso de que nenhum valor foi mostrado. Não há nada para guardar: a chave de
cifra **não tem cópia fora da VPS** (ADR-021, escolha menor 11), e perdê-la só faz cada pessoa colar a
chave da IA de novo no app.

- **`FALHOU: ja existe …`:** a chave já foi criada. Siga para o 10.2 e **não a troque**: uma chave de
  cifra nova tem o mesmo efeito de perder a antiga, e o script recusa de propósito.
- **`FALHOU: nao achei a pasta … segredos`:** é uma VPS nova. Volte ao passo 6 (o `gerar-segredos.sh`
  cria a chave de cifra junto dos outros segredos).
- **Se você pular este passo:** o `subir.sh` para no começo, sem mudar nada, com "falta …/ai-keys-secret,
  a chave de cifra das chaves da IA do celular: rode antes o criar-chave-de-cifra.sh pelo roteiro".

### 10.2. As migrations 0017 a 0019 (o `subir.sh` já roda o `migrate`)

**O `subir.sh` já roda o `migrate`:** no passo 6 dele, depois de conferir o Postgres, ele lê o
`migrate -status` e, se houver migration pendente, **para e pergunta** "Digite SIM para rodar o migrate
agora"; só com o `SIM` ele roda o `migrate`, num contêiner descartável da imagem nova da API, na rede
interna. Se a API está de pé, ela **para antes** do `migrate` (o servidor recusa subir com migration
pendente) e volta depois do segundo `SIM` (o da API). Não há comando à parte: é o mesmo `subir.sh` do
passo 7.

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

Numa VPS que já tinha as 16 primeiras, as pendentes são estas três, **só aditivas** (cada uma cria uma
tabela nova; nenhuma tabela anterior muda, e nada é apagado):

| Migration | O que cria |
|---|---|
| `0017_ai_provider_keys_table` | as chaves de IA das pessoas, **cifradas** (nunca em claro) |
| `0018_dictation_settings_table` | as preferências do ditado, uma linha por conta |
| `0019_dictation_consents_table` | o aceite do aviso do ditado, com a versão e os provedores |

**Deve aparecer:** o `migrate -status` com as três `pendente`, o `SIM` do `migrate`, "OK: as migrations
em dia", o `SIM` da API e os `OK:` do passo 7. Como a API ganha o segredo novo, o `memswap_limit`, o
`ulimits` e o `stop_grace_period` de 90 s, o compose a **recria** (você vê "OK: a API saudavel:
arrumacao, retencao e so entao servindo…" e não a frase de "nao foi recriada").

### 10.3. Antes do primeiro áudio: o proxy, só lendo

**Por que:** o corpo do ditado (até 1 MiB) passa pelo Traefik da VPS antes de chegar à API. Dois prazos
dele importam, e a ADR-021 só os conhece pela documentação do Traefik v3, não pela **sua** configuração:
o `readTimeout` do entrypoint (o padrão é 60 s, e o servidor corta o corpo em 30 s, então o proxy nunca
deve cortar antes) e o `writeTimeout` (o padrão é 0, sem prazo). Um **middleware de buffering** no
entrypoint levaria ao disco um corpo grande, e o nosso router não tem middleware. O Traefik não é
nosso e **não se edita**: aqui só se lê, e o que não bater é ponto de parada. É a mesma leitura do
passo 8, que já olha os argumentos do Traefik:

```bash
docker inspect traefik --format '{{json .Args}}' | tr ',' '\n' | grep -i -e respondingtimeouts -e middlewares -e buffering -e configfile
```

**Deve aparecer:** nada (nenhuma linha): os padrões do Traefik valem, sem prazo curto e sem middleware
no entrypoint. Se aparecerem linhas, leia cada uma:

- `…respondingTimeouts.readTimeout=…`: tem de ser `0` (sem prazo) ou **maior que 30 s**. De 1 s a 30 s,
  **PARE**: o proxy cortaria um corpo que o servidor aceitaria.
- `…respondingTimeouts.writeTimeout=…`: tem de ser `0` ou de **pelo menos 90 s** (o corpo leva até 30 s,
  e o trabalho depois dele, o provedor, o Smart, o Polish e a gravação, até 44 s). Menos que isso,
  **PARE**: o proxy cortaria a resposta de um ditado que o provedor já cobrou.
- `…http.middlewares=…` **no entrypoint** `websecure`, ou qualquer menção a `buffering`: **PARE** e me
  mande só os nomes. Um middleware de buffering poderia gravar o corpo em disco.
- `--configFile=…`: a configuração estática está num arquivo. Leia o `entryPoints` dele no host (o
  caminho de origem está em `docker inspect traefik --format '{{json .Mounts}}'`), com os mesmos
  critérios acima, e **sem editá-lo**.

### 10.4. Antes do primeiro áudio: a swap e o core dump, só lendo

**Por que:** o `memswap_limit` do compose (igual ao `mem_limit`, 256 MB) tira a swap do contêiner da API,
para que nem o heap (com o áudio e a chave decifrada) vá para o disco. **Isso depende do kernel da
VPS:** num cgroup v1 sem `swapaccount=1`, o Docker descarta o limite e só escreve "No swap limit
support". Por isso a conta é conferida na VPS, antes do primeiro áudio, em quatro leituras. **Se alguma
não bater, PARE e me conte**; a alternativa é uma VPS sem swap, e quem decide é você.

1. O Docker não descarta o limite de swap (a linha de aviso não pode aparecer):

```bash
docker info 2>&1 | grep -i "swap limit"
```

**Deve aparecer:** nada. Se aparecer "No swap limit support", **PARE**.

2. O que o Docker aplicou ao contêiner da API (a memória, a memória mais a swap, e o `ulimits`):

```bash
docker inspect aurora-whisper-api --format '{{.HostConfig.Memory}} {{.HostConfig.MemorySwap}} {{json .HostConfig.Ulimits}}'
```

**Deve aparecer:** `268435456 268435456 [{"Name":"core","Hard":0,"Soft":0}]`: os dois primeiros números
**iguais** (256 MB, sem swap) e o limite de `core` em `0`. Se o segundo número for outro (um `-1`, por
exemplo) ou o `core` faltar, **PARE**.

3. A swap do contêiner **no cgroup**, lida no host (a imagem da API não tem shell). Em um cgroup v2:

```bash
ID=$(docker inspect --format '{{.Id}}' aurora-whisper-api); cat $(find /sys/fs/cgroup -name memory.swap.max -path "*$ID*" 2>/dev/null)
```

**Deve aparecer:** `0`. Se aparecer `max` ou um número maior que zero, ou nada (o arquivo não existe:
cgroup v1, onde o equivalente é `memory.memsw.limit_in_bytes` igual a `memory.limit_in_bytes` na pasta
do contêiner), **PARE**.

4. Se a VPS tem swap (informação, para a decisão se a leitura anterior não der `0`):

```bash
swapon --show
```

**Deve aparecer:** o que a VPS tem. Sem nenhuma linha, a VPS não tem swap, e é a alternativa aceita pela
ADR-021 (nada do contêiner pode ir para um disco de swap que não existe). Com linhas, só vale o `0` da
leitura 3.

Estas leituras são **só leitura**: nenhuma mexe no Traefik, no Docker ou na swap. O **core dump** já vem
travado pelo `ulimits` (leitura 2), e o Go não grava core no padrão. Depois delas, e do seu OK da D0, a
ADR-021 (C7) segue com a chave da OpenAI colada **por você no app**, nunca no chat.

## Uma vez por mês

**Por que:** a v1 não tem canal de aviso. É aqui que se vê se o job que apaga o que venceu (a
retenção, de hora em hora) e o backup continuam rodando. A saúde do job **não** é o healthcheck da API
de propósito: o Traefik tira do ar um contêiner que não está `healthy`, e uma rodada com falha
derrubaria o endereço.

Na VPS (só lê):

```bash
docker exec aurora-whisper-api /usr/local/bin/aurora-whisper-server healthcheck -retencao
```

**Deve aparecer:** `healthcheck: OK (retenção há N s)`, com N abaixo de 7200 (2 h). Se aparecer
`FALHOU`, me mande a linha (ela só tem números).

```bash
docker ps --filter name=aurora-whisper --format "table {{.Names}}\t{{.Status}}"
```

**Deve aparecer:** os três com `(healthy)`. O `aurora-whisper-backup` sem o `healthy` quer dizer que o
último backup que subiu ao B2 tem mais de 26 h.

No PC: baixe o backup mais recente do B2 (passo 9.2) e rode o `teste-restauracao-mensal.ps1` (passo
9.3). **Deve aparecer:** os dois `OK:` do passo 9.3, com o backup de menos de 48 h.

## Se algo der errado

- **Tirar a nuvem do ar, sem apagar nada** (o Traefik deixa de atender o endereço; o banco fica):

```bash
docker compose --project-name aurora-whisper --env-file ~/aurora-whisper/config.env -f ~/aurora-whisper/deploy/compose.yml stop
```

  Para voltar: o `subir.sh` do passo 7 de novo.
- **Ver o estado, só lendo:**

```bash
docker compose --project-name aurora-whisper --env-file ~/aurora-whisper/config.env -f ~/aurora-whisper/deploy/compose.yml ps
```

  O `--project-name` vai sempre: sem ele, um `COMPOSE_PROJECT_NAME` exportado na sessão faria o
  comando procurar outro projeto (e o `stop` não pararia nada).

- **Restaurar um backup** (só num desastre; é ponto de parada): o `docs/restauracao-f11.md`, passo a
  passo, com a decifra no PC, o `restaurar.sh` na VPS e o `DROP` do banco antigo depois de 7 dias.
- **Ver as últimas linhas da API** (o log tem só nomes de variável e contagens, nunca texto ditado):

```bash
docker logs --tail 30 aurora-whisper-api
```

- **Uma imagem nova, depois:** o passo 4; no lugar do passo 5, o comando abaixo, **sem** o JSON do
  Google (ele já está na VPS); e o passo 7. O `gerar-segredos.sh` é só da primeira vez (rodado de novo,
  ele recusa e não muda nada). **Se a VPS já estava no ar antes do ditado pelo celular**, o passo 10.1
  (a chave de cifra) vem **antes** do passo 7: sem o arquivo novo, o `subir.sh` para no começo.

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\enviar-para-servidor.ps1 -Dominio aurora-whisper.mjj.net.br
```

  Se o `-ArquivoGoogle` for junto por engano, o `subir.sh` vê que é o mesmo JSON de `segredos/` e apaga
  a cópia de `recebido/` (nenhuma cópia do client secret fica esquecida lá).
- **Trocar o client do Google depois** (o secret vazou, ou um client novo). O JSON vai sozinho, sem as
  imagens, e o `subir.sh` o confere (o client Web, o redirect de produção), pede o SIM, troca o de
  `segredos/` e **recria** a API (o `docker compose up -d` comum não a recriaria, e ela só lê o JSON na
  subida):
  1. No Google Cloud, baixe o JSON do client, com o secret novo (ele cai em **Downloads**).
  2. No PowerShell do PC, a pasta de entrada na VPS e o JSON (troque `NOME-DO-JSON-NOVO.json` pelo nome
     do arquivo baixado):

```powershell
ssh vps "umask 077; mkdir -p aurora-whisper/recebido; chmod 700 aurora-whisper/recebido"
```

```powershell
Set-Location $env:USERPROFILE\Downloads; scp -q NOME-DO-JSON-NOVO.json vps:aurora-whisper/recebido/google-client.json
```

  3. Na VPS:

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

  **Deve aparecer:** "OK: um client do Google NOVO em recebido/, conferido…", e na lista antes do SIM,
  "o client do Google e TROCADO…". Digite `SIM`. Depois, "OK: o client do Google trocado…" e os outros
  `OK:` do passo 7. Sem o SIM, nada é trocado (o JSON fica em `recebido/`); se ele recusar o JSON, a
  frase diz o porquê e o `rm` para desistir.
  4. No PC, o `conferir-nuvem.ps1` do passo 8, e entre uma vez pelo Google. Se a troca foi por um
     vazamento, apague no Google Cloud o secret antigo do client, e o JSON de **Downloads**.

## O que me contar

1. A região do B2 (passo 2) e o digest da distroless (passo 4).
2. Se o `deploy-teste.ps1` passou de primeira, e se apareceu o AVISO do http.
3. As linhas `OK:` (ou o FALHOU) do `subir.sh` e do `conferir-nuvem.ps1`, e os dois números do log de
   acesso do Traefik (passo 8).
4. Se a segunda conta foi recusada.
5. A linha do `teste-restauracao-mensal.ps1`.
6. Se algum dos outros serviços do servidor ficou mais lento depois do passo 7.
7. Do passo 10 (o ditado pelo celular): o que apareceu no `criar-chave-de-cifra.sh` e nas migrations
   0017 a 0019, e, **antes do primeiro áudio**, a saída das leituras do proxy (10.3) e da swap (10.4).
   Qualquer **PARE** deles é ponto de parada: me conte antes de seguir.
