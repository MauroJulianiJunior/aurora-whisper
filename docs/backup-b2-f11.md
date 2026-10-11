# O backup no Backblaze B2: a conta, o bucket e a chave

**O que é e por que:** toda madrugada (03:00 de São Paulo), a nuvem faz uma cópia do banco, cifra com a
**sua** chave pública (só você abre) e manda para um bucket do Backblaze B2. Este roteiro prepara o lado
do B2: a conta, um bucket que **ninguém consegue esvaziar antes de 30 dias** (nem quem invadir a VPS), a
regra que apaga o que passou de 30 dias, e uma chave que **só sobe e lista**, que é a que vai para a VPS.

ADR-019, "Decisões da F11", D2 (confirmada por você em 09-out-2026) e a revisão da Parte 1 (o Object
Lock). É o passo 2 do `docs/deploy-f11.md`. Tempo estimado: 20 a 30 minutos.

**Duas regras para o caminho todo:**

1. **Nenhuma chave vai para o chat.** Nem a mestra (a da conta) nem a do backup. Você as copia do B2 para
   o gerenciador de senhas e, a do backup, para a pergunta do `gerar-segredos.sh` na VPS.
2. **Se a tela do B2 estiver diferente do que está escrito aqui**, pare e me mande um print. O painel do
   B2 muda de tempos em tempos, e um clique errado aqui (por exemplo, criar o bucket sem o Object Lock)
   só se desfaz criando outro bucket.

## 1. Criar a conta

1. Abra `https://www.backblaze.com/sign-up/cloud-storage` e crie a conta com o seu e-mail.
2. **A região.** A página de cadastro pede a região dos dados (algo como **US West**, **US East** ou
   **EU Central**). Ela vale para a conta inteira e **não muda depois**. Pelo que se sabe, o B2 não tem
   região no Brasil.

   **Por que importa:** o dossiê de LGPD e a política dizem onde o backup mora (hoje está
   "[A PREENCHER: região do bucket]"). O backup é cifrado e o B2 não consegue lê-lo, mas continua sendo
   uma cópia dos dados fora do Brasil. Escolha a que preferir e **me diga qual foi** (é só o nome da
   região, não é segredo).
3. Confirme o e-mail. Ligue a **verificação em duas etapas** da conta (no menu da conta, algo como
   **My Settings** ou **Account** > **Two-Factor Verification**). Quem entrar na conta apaga os backups
   por cima de tudo.

## 2. Criar o bucket, com o Object Lock

No painel: **Buckets** > **Create a Bucket**.

1. **Bucket Unique Name:** um nome só seu, de 6 a 63 letras, números ou hífens (por exemplo,
   `aurora-whisper-backup-` seguido de algo aleatório seu). Anote: o `gerar-segredos.sh` pergunta.
2. **Files in Bucket are:** **Private**.
3. **Default Encryption:** pode ficar **Disable** (o arquivo já vai cifrado por você) ou **Enable**;
   tanto faz.
4. **Object Lock:** **Enable**. **Este é o passo que importa.** Ele só pode ser ligado aqui, na criação.

   **Por que:** a chave que fica na VPS precisa poder escrever para subir os backups, e "escrever", no
   B2, também deixa **esconder** um arquivo. A regra do passo 4 apaga de vez o que foi escondido. Sem o
   Object Lock, quem invadisse a VPS esconderia os 30 backups e, um dia depois, eles sumiriam. Com ele,
   cada cópia fica guardada até o fim do prazo, mesmo escondida.
5. **Create a Bucket**.

**Deve aparecer:** o bucket na lista, com **Private** e **Object Lock: Enabled** (ou um cadeado). Se
não aparecer o Object Lock, **pare e me mande um print**: apague esse bucket (ele está vazio) e crie
outro.

## 3. A retenção padrão: 30 dias, modo governance

No bucket: **Bucket Settings** (ou o link **Object Lock** do bucket) > **Default Retention Policy**.

1. **Mode:** **Governance**.
   **Por que governance, e não compliance:** no governance, a sua chave mestra ainda consegue apagar uma
   cópia antes do prazo (para um pedido de apagar da LGPD, ou um engano seu). No compliance, **ninguém**
   consegue, nem você nem o suporte do B2, até o prazo acabar. A chave da VPS não tem o poder de passar
   por cima do governance (passo 5), então para ela os dois são iguais.
2. **Period:** **30 days**.
3. **Update** (ou **Save**).

**Deve aparecer:** "Governance, 30 days" na retenção padrão do bucket.

## 4. A regra de ciclo de vida: o giro de 30 dias

No bucket: **Lifecycle Settings**.

1. Escolha a opção de **regra própria** (algo como **Use custom lifecycle rules**).
2. A regra:
   - **fileNamePrefix:** `aurora-whisper/` (a pasta onde o backup sobe);
   - **daysFromUploadingToHiding:** `30`;
   - **daysFromHidingToDeleting:** `1`.
3. **Update Bucket**.

**Por que estes números:** aos 30 dias o B2 esconde a cópia e, um dia depois, apaga. Com o Object Lock,
nada é apagado antes do fim da retenção dele (30 dias), então o giro é de cerca de 30 dias. **Anote o que
o painel mostrar** como prazo real: a política diz "30 dias, mais o tempo que o B2 leva para apagar o que
a regra escondeu" até você me dizer o número.

Se o painel só oferecer as opções prontas ("Keep only the last version", "Keep prior versions for…"),
**mande um print**: não escolha nenhuma delas.

## 5. A chave do backup, pela CLI do B2 num contêiner

**O jeito curto (desde 10-out-2026):** um comando no PowerShell faz os passos 5.1 a 5.5 sozinho. Ele
pede a mestra no próprio PowerShell (onde o Ctrl+V funciona; dentro do contêiner não funciona), apaga
a chave antiga com o mesmo nome, cria a nova, **grava-a num arquivo sem mostrar** e confere as
capacidades. Depois é só abrir o arquivo e copiar para o gerenciador de senhas. Troque
`NOME-DO-SEU-BUCKET` pelo nome do passo 2:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\b2-chave-do-backup.ps1 -Bucket NOME-DO-SEU-BUCKET
```

Os passos abaixo são o mesmo caminho à mão.

**Por que pela CLI:** pelo que se sabe, o painel do B2 só cria chaves com conjuntos prontos ("Read and
Write", que traz o poder de apagar; "Write Only", que não lista). A chave da VPS tem de ter **só**
`listBuckets`, `listFiles` e `writeFiles`. A CLI do B2 cria assim, e roda num contêiner descartável no
PC: nada é instalado, e nada fica guardado depois.

Antes, no painel: **Application Keys**. No topo está a **Master Application Key**. Se você ainda não a
gerou, clique em **Generate New Master Application Key** e copie o **keyID** e a **applicationKey** para o
gerenciador de senhas (o painel mostra a applicationKey uma vez só). Ela alcança a conta inteira: **nunca
vai para a VPS**.

### 5.1. Abrir a CLI (PowerShell, com o Docker Desktop aberto)

```powershell
docker run --rm -it --entrypoint sh backblazeit/b2:4.8.0
```

**Por que assim:** a imagem abre direto no programa `b2`; o `--entrypoint sh` abre o terminal no lugar
dele (sem isso, o `sh` vira um comando do `b2` e dá erro). O `--rm` apaga o contêiner (e o login que você fizer nele) quando você sair. Na
primeira vez o Docker baixa a imagem oficial da CLI.

**Deve aparecer:** um `#` ou `$` esperando comando (você está dentro do contêiner). Se aparecer um erro
como "sh: not found" ou "executable file not found", mande um print.

### 5.2. Entrar com a chave mestra (dentro do contêiner)

```sh
b2 account authorize > /dev/null && echo AUTORIZADO
```

**Por que o `> /dev/null`:** sem ele, o comando imprime no fim um bloco com a **applicationKey da
mestra** e um token da conta, e um bloco assim colado no chat entrega a conta inteira (aconteceu em
09-out-2026: a mestra teve de ser trocada). Com ele, só sai a palavra AUTORIZADO.

**Deve aparecer:** a pergunta do **application key ID** (cole o keyID da mestra e tecle Enter) e depois a
da **application key** (cole a applicationKey da mestra, com o botão direito do mouse; ela não aparece
na tela). No fim, só `AUTORIZADO`. Se a chave for recusada (`bad_auth_token`), gere uma mestra nova no
painel e use a nova: cada "Generate New Master Application Key" invalida a anterior.

**Nunca cole no chat a saída desta janela**, a não ser a dos passos 5.4 e do `b2 bucket get`, que não
têm chave.

### 5.3. Criar a chave do backup (dentro do contêiner)

Troque `NOME-DO-SEU-BUCKET` pelo nome do passo 2:

```sh
b2 key create --bucket NOME-DO-SEU-BUCKET aurora-whisper-backup listBuckets,listFiles,writeFiles
```

**Deve aparecer:** uma linha com dois valores separados por espaço: o **keyID** da chave nova (25
caracteres) e a **applicationKey** dela (começa com `K` e três dígitos). **Copie os dois para o
gerenciador de senhas agora**: a applicationKey não aparece de novo. Eles vão, na VPS, para as perguntas
do `gerar-segredos.sh` (o passo 6 do `docs/deploy-f11.md`), e para mais nenhum lugar.

### 5.4. Conferir as capacidades (dentro do contêiner)

```sh
b2 key list --long
```

**Deve aparecer:** a linha da `aurora-whisper-backup` com o nome do seu bucket e **exatamente**
`listBuckets,listFiles,writeFiles`. Se aparecer `deleteFiles`, `readFiles`, `bypassGovernance`,
`writeFileRetentions` ou `writeBucketRetentions`, apague essa chave (`b2 key delete <keyID dela>`),
crie de novo pelo 5.3 e me mande um print.

### 5.5. Sair

```sh
exit
```

**Deve aparecer:** o PowerShell de volta. O contêiner, com o login da mestra, foi apagado.

## 6. O que fica para a VPS

- O **nome do bucket**, o **keyID** e a **applicationKey** da chave do backup: o `gerar-segredos.sh` os
  pergunta (sem mostrar na tela) e grava o `rclone.conf` na pasta de segredos da VPS.
- Na VPS, **antes do primeiro backup**, o `conferir-b2.sh` confere que a chave sobe, lista e **não**
  consegue apagar (o passo 6.1 do `docs/deploy-f11.md`). O Object Lock, a regra e as capacidades
  exatas essa chave não enxerga: por isso os passos 2 a 5.4 acima são conferidos aqui, no painel e na
  CLI.

## O que me contar

- a **região** da conta (passo 1.2);
- se o Object Lock ficou ligado com **Governance, 30 days** (passos 2 e 3);
- o que o painel mostrou na regra de ciclo de vida (passo 4);
- que o `b2 key list --long` mostrou só as três capacidades (passo 5.4).

Nenhuma chave, nenhum keyID: só isso.
