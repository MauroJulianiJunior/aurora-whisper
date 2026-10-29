# O deploy das imagens das notas na nuvem (N10), passo a passo

**O que é e por que:** a fase N10 (ADR-022, "As imagens (N10)", "Decisões da N10" e "Decisões da N10b")
leva as imagens das notas do PC para a nuvem e de volta: a foto anexada num PC chega aos outros PCs; a do
celular chega ao PC; tirar da nota tira de todo lado; e a imagem vai e volta da lixeira com a nota e sai de
vez com ela. **Na nuvem, as imagens ficam no banco** (a sua decisão de 28-out-2026), numa tabela nova (a
migration `0026`), e entram sozinhas no backup cifrado de todo dia. O servidor ganha as rotas das imagens; o
Agent dos PCs ganha o envio e o download delas.

**Quando fazer:** depois que a **parte do celular** da N10 também estiver pronta (a tela das imagens no app
do celular). A imagem da API leva o app do celular junto, então um deploy só leva as duas. Se você quiser
subir antes, o servidor sozinho funciona: os PCs já trocam imagens, e o celular só não as mostra ainda.

Este roteiro é o que **você** roda; eu não rodo nada no servidor. Ele usa os mesmos scripts do
`docs/deploy-f11.md` e do `docs/deploy-notas-n7.md` (o `deploy-teste.ps1`, o `enviar-para-servidor.ps1` e o
`subir.sh`), na mesma VPS que já está no ar.

**Pontos de parada (são seus):** o backup antes, a migration `0026` em produção, a imagem nova da API e,
por último, o Agent novo nos PCs. Nada disso foi rodado por mim.

**A ordem importa, e é esta:** o backup → o espaço da restauração (só lendo) → o teste no Docker do PC → a
imagem para a VPS → o `subir.sh` (a migration e a API nova) → conferir → **só então** o Agent novo nos PCs.
O Agent novo só manda e baixa imagem depois de ver que a nuvem sabe delas (o campo `entidades` do pull), então
ele não estraga nada se for instalado antes; mas as imagens só sobem depois do passo 5, e o primeiro pull
depois dele lê tudo de novo uma vez (é esperado: o cursor volta a 0, e o que o PC já tem volta como eco, sem
mudar nada).

Tempo estimado: 30 a 40 minutos.

**Nos comandos abaixo, troque:**
- `<seu-dominio>` pelo endereço da sua nuvem (o mesmo dos deploys anteriores);
- `<seu-acesso-ssh>` pelo nome do acesso ssh à VPS que você já usa (o mesmo dos deploys anteriores);
- `<IP-DA-VPS>` pelo IP da VPS.

## Antes de começar

1. **Nada do Traefik nem dos outros sites é tocado.** Como no `docs/deploy-f11.md`, aviso 2.
2. **Nunca `docker compose down -v` na nuvem** (apaga o volume do Postgres).
3. **A migration é só aditiva:** a `0026` cria uma tabela nova (`note_images`) e o índice dela. Nenhuma
   tabela que já existe muda, nenhuma linha é apagada. Mesmo assim, o backup vem antes.
4. **Duas janelas:** o PowerShell no PC e uma sessão `ssh <seu-acesso-ssh>` na VPS.
5. **O deploy da N8 tem de estar feito** (as notas no celular). Se não estiver, faça o
   `docs/deploy-notas-n8.md` antes, até o fim.

## 1. O backup, antes de tudo (na VPS)

**Por que:** é a volta garantida, se algo der errado no meio. O backup é o mesmo de todo dia (cifrado e
mandado ao B2).

```bash
docker exec aurora-whisper-backup backup.sh
```

**Deve aparecer:** `backup: OK (aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age, N bytes, N s)`. Anote o nome do
arquivo **e o número de bytes**: é o tamanho do backup antes das imagens. Se aparecer outra coisa, **pare** e
me mande a linha.

**Anote também** quantas notas e quantos ditados a nuvem tem agora (só contagens, nunca o texto):

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT (SELECT count(*) FROM notes) AS notas, (SELECT count(*) FROM transcriptions) AS ditados"
```

## 2. O espaço da restauração e do disco (na VPS, só lendo)

**Por que:** com as imagens no banco, o backup cresce (as fotos quase não comprimem). O teto de 400 MB de
imagens por conta foi escolhido pela pasta em memória onde a restauração abre o backup decifrado
(`/run/user/<o seu número>`, uns 795 MB quando você mediu). Estes dois comandos só leem: conferem que a pasta
e o disco continuam com folga.

```bash
df -h /run/user/$(id -u)
```

**Deve aparecer:** uma linha `tmpfs` com o **Size** perto de `795M`. Se for bem menor (abaixo de uns
`500M`), **pare** e me avise antes de seguir: o teto das imagens teria de baixar.

```bash
df -h /var/lib/docker
```

**Deve aparecer:** o disco com pelo menos uns **5 GB livres** (a coluna **Avail**). O volume do backup guarda
3 dias de backups na VPS, e cada um vai crescer até uns 450 MB por conta cheia de imagens. Se estiver com
menos, me avise.

## 3. O teste no Docker do PC

**Por que:** a imagem nova sobe inteira no seu PC, com o mesmo compose da VPS, e aplica as `0001` a `0026`
num Postgres de teste. Com o Docker Desktop aberto:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\deploy-teste.ps1
```

**Deve aparecer**, no fim: **"OK: o deploy passou no Docker do PC"**. Se aparecer **FALHOU**, o script já
derrubou tudo: me mande a linha do FALHOU e as de log que ele mostrou. Nada foi para a VPS.

**Os testes do Postgres já rodaram (29-out-2026):** com o Docker aberto, o Claude rodou
`go test ./migrations/ ./internal/server/pgstore/` contra o Postgres de teste do `postgres-de-teste.ps1`
(a `0026` aplicada sobre um banco com dados, as restrições dela, e a suíte de contrato inteira com as
imagens, no Postgres de verdade): tudo `ok`. Se algo mudar no código do servidor depois disso, peça que eu
rode de novo antes do passo 4.

## 4. A imagem nova para a VPS

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\enviar-para-servidor.ps1 -Dominio <seu-dominio> -Servidor <seu-acesso-ssh>
```

(**Sem** o `-ArquivoGoogle`: o JSON do Google já está na VPS.)

**Deve aparecer:** "OK: as duas imagens sao as que passaram no deploy-teste.ps1", o `Loaded image:` e
"OK: tudo na VPS.".

## 5. A migration e a API nova (na VPS)

**Por que:** o `subir.sh` confere o Postgres, mostra as migrations pendentes e **para e pergunta** antes de
rodá-las; depois, pergunta de novo antes de subir a API nova.

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

A pendente é **uma só, só aditiva**:

| Migration | O que cria |
|---|---|
| `0026_note_images_table` | as imagens das notas: os bytes da foto já limpa (sem a localização), o tamanho, a largura e a altura, e a marca de "tirada da nota" |

1. "**Ha migration pendente** … Digite SIM para rodar o migrate agora": confira que a lista é a de cima (só
   a `0026`) e digite `SIM`. Se a lista tiver outra coisa, **não** digite: me mande a lista.
2. "**Digite SIM para subir a API**": digite `SIM`.

**Deve aparecer:** "OK: as migrations em dia", "OK: a API saudavel: arrumacao, retencao e so entao
servindo…" e, no fim, "OK: a nuvem esta no ar".

## 6. Conferir (na VPS e no PC, só lendo)

**A migration aplicada:**

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT version, name FROM schema_migrations WHERE version >= 25 ORDER BY version"
```

**Deve aparecer:** as linhas `25|transcription_note_links_table` e `26|note_images_table`.

**Nada mudou nas contagens** (as mesmas do passo 1, ou um pouco mais, se algum PC mandou algo no meio), e a
tabela das imagens nasceu vazia:

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT (SELECT count(*) FROM notes) AS notas, (SELECT count(*) FROM transcriptions) AS ditados, (SELECT count(*) FROM note_images WHERE removed_at IS NULL) AS imagens"
```

**Deve aparecer:** as duas contagens do passo 1 e `0` imagens.

**De fora, no PC:**

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio <seu-dominio> -IPEsperado <IP-DA-VPS>
```

**Deve aparecer:** "OK: a nuvem em https://<seu-dominio> responde certo de fora".

## 7. O Agent novo nos PCs

Só depois do passo 6. Feche o Agent, remonte-o como de costume (`scripts\remontar-agent.ps1`) e confira que o
processo novo subiu (o `StartTime` de agora).

**Na primeira rodada do sync**, o Agent vê que a nuvem sabe das imagens, volta o cursor a 0 e lê tudo de novo
uma vez; em seguida sobem as imagens que já estão nas notas do PC (o "Enviar o histórico" que você já clicou
vale como o aceite delas, como valeu para as notas na N7). Sobem até 20 por rodada, uma rodada por minuto.
**A nota sobe antes da imagem dela:** uma nota que ainda não estava na nuvem sobe primeiro, e a imagem vem na
mesma rodada ou na seguinte.

**Conferir que as imagens subiram (na VPS, só contagens e o total):**

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT count(*) AS imagens, pg_size_pretty(coalesce(sum(bytes), 0)) AS total FROM note_images WHERE removed_at IS NULL"
```

**Deve aparecer:** o número de imagens que as notas do PC têm (fora as de notas da lixeira, que só sobem se a
nota voltar) e o total delas. Se um PC estiver com uma imagem que não sobe, o log da rodada do Agent diz
`imagens_esperando=N` ou `imagens_recusadas=N` (só números): me mande a linha.

Depois, o roteiro de teste da N10 (o `docs/teste-manual-notas-n10.md`, com a parte do celular). **Inclua nele
uma foto grande** (de uns 3 ou 4 MB, depois de reduzida) anexada no PC e no celular: é o que confere, de
verdade, que o caminho até a nuvem deixa passar o tamanho todo (veja "Se algo der errado", o proxy).

**Um backup depois:** rode de novo o passo 1 depois de as imagens subirem e compare o número de bytes com o
de antes: o backup cresce mais ou menos o total das imagens.

## Se algo der errado

- **A API nova não sobe** (o `subir.sh` deu FALHOU depois do `SIM` da API): me mande a linha, e não tente
  voltar à imagem antiga: o servidor recusa subir com uma migration aplicada que o binário dele não conhece
  (`migrations/README.md`, "Como o `migrate` controla a história"). A saída é uma imagem nova corrigida, ou o
  backup, abaixo.
- **Voltar ao estado de antes (rollback):** a migration não tem "desfazer"; a volta é o backup do passo 1,
  pelo `docs/restauracao-f11.md` (é ponto de parada: me chame antes). A restauração troca o epoch da conta:
  cada PC recomeça sozinho e sobe de novo o que tem (ditados, notas e as imagens com o arquivo no PC), sem
  perder nada do PC. A imagem que só o celular tinha anexado depois do backup não volta.
- **Uma imagem grande não sobe** (o log do Agent diz `imagens_esperando=1`, e a foto pequena sobe): o mais
  provável é o caminho até a nuvem cortando o pedido. O Traefik da VPS não é nosso e não é mexido: sem um
  limite de corpo configurado nele, ele deixa passar, mas no Traefik 3 o padrão é cortar um pedido que leva
  mais de 60 s para chegar (uma rede bem lenta, abaixo de uns 0,55 Mbit/s de subida). Nada se perde: a imagem
  fica no PC e tenta de novo, cada vez mais espaçado. Me mande a linha do log e a velocidade da rede.
- **Tirar a nuvem do ar, sem apagar nada:** o comando do `docs/deploy-f11.md`, "Se algo der errado".
- **O Agent novo instalado antes da nuvem nova:** nada se perde. Ele não sobe nem baixa imagem enquanto a
  nuvem não disser que sabe delas; o resto do sync segue como hoje.

## O que me contar

1. A linha do backup do passo 1 (com os bytes) e as duas contagens.
2. O `Size` do `df` da pasta em memória e o `Avail` do disco (passo 2).
3. Se o `deploy-teste.ps1` passou.
4. A lista das migrations pendentes e os `OK:` (ou o FALHOU) do `subir.sh`.
5. As contagens do passo 6 e a do passo 7, e os bytes do backup de depois.
