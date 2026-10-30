# A política de privacidade da sua nuvem, passo a passo

**O que é e por que:** a nuvem passa a servir **a política de privacidade da sua instância** numa página
pública, `https://<seu-dominio>/privacidade`, e os apps (o do celular e o painel do PC) mostram um link para
ela, **só se ela existir**. O modelo está em `docs/politica-de-privacidade.md`; **a cópia preenchida tem o seu
nome e o seu e-mail, e por isso nunca entra no repositório** (o GitHub é público). Ela mora no seu PC, fora do
repositório, e o `enviar-para-servidor.ps1` a leva para a VPS. O servidor a lê uma vez, na subida.

Também entra aqui o comando que cumpre a promessa da seção 8 da política, **"tirar o cadastro a pedido"**
(`tirar-cadastro.sh`, no fim deste roteiro).

**Quando fazer:** depois de preencher a política (o dossiê, `docs/lgpd-dossie.md`, seção 7, diz o que
falta decidir: as regiões da VPS e do bucket, decisão 1) e de ler o texto inteiro. Sem a cópia na VPS, a
nuvem continua como está: não há a página e o app não mostra link.

**Este roteiro é o que você roda; eu não rodo nada no servidor.** Ele usa os mesmos scripts do
`docs/deploy-f11.md` e do `docs/deploy-notas-n10.md`, na mesma VPS que já está no ar.

**Não precisa de backup antes:** esta mudança **não tem migration** e não mexe no banco (a política é um
arquivo na pasta `~/aurora-whisper/politica/`, fora do volume do Postgres). A API é recriada por causa do
arquivo, e só.

Tempo estimado: 15 a 20 minutos (mais o tempo de preencher o texto).

**Nos comandos abaixo, troque:**
- `<seu-dominio>` pelo endereço da sua nuvem;
- `<seu-acesso-ssh>` pelo nome do acesso ssh à VPS que você já usa;
- `<sua-pasta-privada>` por uma pasta do seu PC **fora do repositório** (o script recusa um arquivo dentro
  dele).

## 1. Preencher a cópia (no PC, fora do repositório)

1. Copie `docs/politica-de-privacidade.md` para `<sua-pasta-privada>\politica-de-privacidade.md`.
2. Preencha **todos** os campos `[A PREENCHER ...]` (o responsável, o contato, as regiões, a condição do
   art. 33 etc.) e **apague a nota do topo** (a citação que começa em "Modelo para quem hospeda a própria
   nuvem"): ela também tem um `[A PREENCHER]`, e o servidor e os scripts recusam qualquer cópia com um
   `[A PREENCHER` sobrando. Isso é de propósito: um modelo pela metade nunca vira página.
3. O que vem a partir do título **"Os textos do app sobre privacidade"** é interno (as frases do app) e **nunca
   é publicado**: o servidor corta ali, mesmo que você deixe. Pode apagar também.
4. Só vale este Markdown: títulos `#`, parágrafos, listas, tabelas, citações, **negrito**, `código` e linha
   horizontal. Link, imagem, itálico e HTML aparecem como texto puro (de propósito: tudo é escapado).
5. Até 96 KiB, em UTF-8, sem caractere de controle.

## 2. O teste no Docker do PC

**Por que:** a imagem sobe inteira no seu PC, com o mesmo compose da VPS, e o script confere que a
`/privacidade` responde `200` (com uma política de mentira, sem o seu texto). Com o Docker Desktop aberto:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\deploy-teste.ps1
```

**Deve aparecer**, no fim: **"OK: o deploy passou no Docker do PC"**. Se aparecer **FALHOU**, o script já
derrubou tudo: me mande a linha. Nada foi para a VPS.

## 3. Mandar para a VPS

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\enviar-para-servidor.ps1 -Dominio <seu-dominio> -Servidor <seu-acesso-ssh> -ArquivoPolitica <sua-pasta-privada>\politica-de-privacidade.md
```

(**Sem** o `-ArquivoGoogle`: o JSON do Google já está na VPS.)

O script confere a cópia **antes de mandar** e **recusa**: um arquivo dentro do repositório, com
`[A PREENCHER`, que não é UTF-8, com caractere de controle, vazio ou maior que 96 KiB. Ele nunca mostra o
texto. Manda também a imagem e a pasta `deploy\` (o compose novo, que monta a pasta `politica/`, e os
scripts novos); o arquivo da política vai para `~/aurora-whisper/recebido/politica-de-privacidade.md`.

**Deve aparecer:** "OK: as duas imagens sao as que passaram no deploy-teste.ps1" e "OK: tudo na VPS.".

## 4. Publicar (na VPS)

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

O `subir.sh` confere o arquivo de novo (as mesmas regras), cria a pasta `~/aurora-whisper/politica/` (se não
houver), e, se a política de `recebido/` é **nova** (diferente da que já está em `politica/`), avisa na lista
do que vai fazer e **pede o `SIM`** para subir a API: a política passa para `politica/` (modo `0444`, a API
só lê), a API é **recriada** (ela lê o arquivo só na subida) e a cópia de `recebido/` sai. Se a política é a
mesma que já está publicada, ele só tira a cópia de entrada, sem recriar nada.

**Deve aparecer:** "OK: a API publica a politica de privacidade da instancia" (o log `servindo` da API diz
`politica_publicada`, sem nunca mostrar o texto), "OK: a politica de privacidade em …/politica (0444)" e, no
fim, "OK: a nuvem esta no ar", com a conferência de `https://<seu-dominio>/privacidade` respondendo `200`.

## 5. Conferir (no PC, só lendo)

```powershell
curl.exe -sI https://<seu-dominio>/privacidade
```

**Deve aparecer:** `HTTP/2 200`, `content-type: text/html; charset=utf-8`, `cache-control: no-cache` e um
`etag`. Abra a página no navegador e leia com calma: o que você escreveu, sem o título "Os textos do app".

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio <seu-dominio> -IPEsperado <IP-DA-VPS>
```

**Deve aparecer:** "OK: a nuvem em https://<seu-dominio> responde certo de fora". (Este script confere a
nuvem de fora, mas **não** olha a `/privacidade`: quem a confere é o `curl` acima e o próprio `subir.sh`.)

No app do celular, o link "Política de privacidade" aparece na tela de entrar e em Configurações,
Privacidade. No painel do PC, o link aparece na seção Nuvem, **com o PC conectado e depois da primeira
conferência com a nuvem** (ao abrir as Configurações), e só com o Agent desta versão.

**A folha do aceite do ditado no celular não mudou** (e a versão do aviso fica a mesma): quem já aceitou não
é chamado a aceitar de novo. O link é só um atalho nas Configurações (`docs/ai-spec/09-security-privacy.md`,
"Texto da tela").

## Mudar o texto, ou tirar a página

- **Mudar o texto:** edite a cópia no PC e repita os passos 3 e 4. (Para mudar só o texto, sem mandar a imagem
  de novo, também dá para copiar o arquivo à mão para `~/aurora-whisper/recebido/politica-de-privacidade.md`
  com o `scp` e rodar só o `subir.sh`; ele faz o mesmo.) O `ETag` muda sozinho.
- **Tirar a página:** na VPS, apague `~/aurora-whisper/politica/politica-de-privacidade.md` (é `0444`, mas a
  pasta é sua: `rm -f` basta) e reinicie a API (`docker restart aurora-whisper-api`; ela lê o arquivo só na
  subida). Sem o arquivo, a nuvem sobe sem a rota (com um aviso `Info` no log) e o app para de mostrar o
  link. O `subir.sh` não recria a API só por isso: ele só a recria quando há política nova ou client novo
  do Google.
- **Um arquivo inválido na pasta `politica/`** (alguém editou à mão) faz a API **não subir** (saída 2, com a
  linha `serve: POLITICA_FILE: …`, sem o caminho nem o texto). A saída é pôr uma cópia boa e recriar.

## Tirar o cadastro de alguém, a pedido

A política promete (seção 8): o "Apagar meus dados" apaga o que a pessoa fez, mas **deixa o cadastro** (o
e-mail, o identificador do Google e o nome); quem o tira é você, pelo contato, a pedido. O comando:

1. **Tire o e-mail do `CONTAS_PERMITIDAS`** (em `~/aurora-whisper/config.env`) e rode o `subir.sh`: a API lê a
   lista só na subida. Sem isso, a pessoa poderia entrar de novo e o cadastro voltaria. O script **recusa**
   se o e-mail ainda estiver no `config.env` ou na API que está de pé.
2. **A pessoa usa o "Apagar meus dados" no app**, se ainda não usou. O script **recusa** se ela ainda tem
   dado em qualquer tabela que aponta para `users` (ele lê a lista do próprio banco, então tabelas novas
   entram sozinhas), e diz qual tabela e quantas linhas, nunca o conteúdo. As duas tabelas que o "Apagar meus
   dados" deixa de propósito (o contador de sincronização e a sessão) saem junto com o cadastro.
3. Rode:

   ```bash
   bash ~/aurora-whisper/deploy/scripts/tirar-cadastro.sh pessoa@exemplo.com
   ```

   Ele mostra o cadastro (e-mail, nome, data) e o que vai sair, pede **`SIM`** e apaga numa transação só
   (tudo ou nada). O e-mail vai ao banco como variável, nunca dentro do texto do SQL, e nada é gravado em
   arquivo.
4. **Responda a pessoa** (a lei dá até 15 dias, art. 19, II). **A cópia de segurança ainda tem o cadastro por
   cerca de 30 dias**, como a política diz na seção 7 (a restauração, se houver, traz a pessoa de volta; veja
   `docs/restauracao-f11.md`).

**A cópia eletrônica dos dados** (a promessa da seção 8, decisão 3 do dossiê): **não há botão nem comando**
para isso. A pedido, você entrega uma cópia eletrônica; hoje isso é feito à mão (o PC da pessoa já tem a
cópia completa dos ditados e das notas dela).

## Se algo der errado

- **O `enviar-para-servidor.ps1` recusou a cópia:** a frase diz o motivo (dentro do repositório, `[A
  PREENCHER`, UTF-8, caractere de controle, tamanho). Nada foi mandado.
- **O `subir.sh` recusou a cópia de `recebido/`:** a frase diz o motivo e como desistir (`rm` da cópia). Nada
  foi mudado na VPS.
- **A API subiu sem publicar** (o `subir.sh` deu FALHOU em `politica_publicada`): olhe o log da API
  (`docker logs --tail 20 aurora-whisper-api`), que diz o problema sem mostrar o texto. A causa mais comum é
  uma pasta `politica/` sem permissão de leitura para a API (ela roda sem root: a pasta tem de ser `0755` e
  o arquivo `0444`, como o `subir.sh` deixa).
- **Voltar ao estado de antes:** como não há migration, basta tirar o arquivo (acima). Não há banco a
  restaurar.

## O que me contar

1. Se o `deploy-teste.ps1` passou.
2. Os `OK:` (ou o FALHOU) do `subir.sh`, em especial "a API publica a politica".
3. O resultado do `curl -sI` do passo 5.
