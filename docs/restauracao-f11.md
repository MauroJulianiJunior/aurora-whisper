# A restauração da nuvem, passo a passo (F11)

**O que é e por que:** num desastre (o banco da nuvem estragou ou foi apagado, ou a VPS foi refeita),
este roteiro põe de volta um backup do B2. É a operação mais destrutiva da nuvem, e rara: cada passo
diz o porquê e traz o comando completo, pronto para colar. Você roda tudo; eu não rodo nada no servidor
nem no Docker. **É ponto de parada** (`docs/ai-spec/09-security-privacy.md`, "Operações destrutivas"):
siga só depois de decidir restaurar.

ADR-019, "Decisões da F11" (a restauração, e as escolhas menores 3 e 10). Os scripts são o
`deploy/scripts/restaurar.sh` (na VPS) e o `scripts\teste-restauracao-mensal.ps1` (no PC). Tempo
estimado: 30 a 60 minutos, com a nuvem fora do ar do passo 3 ao 6.

**A ordem, em uma linha:** baixar o backup → a sessão `ssh` que fica aberta → parar a API e o backup →
decifrar no PC e mandar à VPS → aplicar → subir de novo → conferir e reconectar → daqui a 7 dias, o
`DROP` do banco antigo.

## Antes de decidir: o que a restauração faz (são quatro avisos)

1. **Todo mundo sai.** Todas as sessões e todos os aparelhos são revogados: cada PC conecta de novo, e
   o celular entra de novo pelo Google.
2. **O que foi apagado depois do backup pode voltar.** O backup é de toda noite: o que você apagou de
   vez desde o último (dentro dos 6 meses) volta, ativo, e pode ser apagado de novo (decisão sua de
   09-out-2026: aceitar e deixar escrito; a política de privacidade diz isso). O que já passou dos 6
   meses a nuvem apaga antes de abrir a porta.
3. **Pode aparecer um selo** num ditado que o celular editou antes do backup e o PC editou depois: o
   texto principal fica o do celular, e o do PC vai para o selo, nos dois lados. Nada se perde: o
   "trocar" do selo recupera o do PC.
4. **Nada é apagado na hora.** O banco de agora não é apagado: vira `aurora_antes_AAAAMMDD` por **7
   dias**, e antes o script guarda uma cópia cifrada dele. O `DROP` dele, depois dos 7 dias, é seu, à
   mão (passo 8).

O `restaurar.sh aplicar` repete os avisos 1 a 4 antes do "digite RESTAURAR".

## 1. Escolher e baixar o backup (no navegador do PC)

**Por que:** a chave que abre os backups só existe no seu PC; a VPS nunca lê o B2.

No painel do B2: **Browse Files** > o seu bucket > `aurora-whisper/` > o
`aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age` mais novo **de antes do problema** > **Download**. Ele cai em
**Downloads**.

**Recomendado, antes de tocar na VPS:** o teste do mês com esse arquivo, que prova num contêiner
descartável do PC que ele decifra e restaura (com o Docker Desktop aberto; troque o nome do arquivo):

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\teste-restauracao-mensal.ps1 -Arquivo $env:USERPROFILE\Downloads\aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age -MaxHoras 720
```

**Deve aparecer:** "OK: o backup decifrou e restaurou: …". O `-MaxHoras 720` deixa passar um backup de
até 30 dias (sem ele, um de mais de 48 h dá FALHOU na idade, mesmo bom).

## 2. A sessão `ssh` que fica aberta até o fim (a janela da VPS)

**Por que:** o dump decifrado fica na memória **só sua** da VPS (`/run/user/<uid>`), que some quando a
sua última sessão fecha. Abra esta e deixe-a aberta até o passo 6 (`vps` é o apelido do seu acesso
ssh, como no `docs/deploy-f11.md`; se o seu tiver outro nome, troque-o aqui e no passo 4):

```powershell
ssh vps
```

**Se a VPS é nova** (o volume do Postgres se perdeu): antes do passo 3, nesta ordem, os passos 5, 6 e
6.1 do `docs/deploy-f11.md` (as imagens e o JSON do Google, os segredos com a **mesma** chave pública
do backup, a conferência do B2) e o `subir.sh` do passo 7 dele, respondendo **qualquer coisa que não
seja SIM** à primeira pergunta: o Postgres fica de pé, vazio, e a API não sobe. No passo 5 daqui, use o
`--sem-copia-antes` (o banco vazio não tem o que copiar).

## 3. Parar a API e o backup (na sessão `ssh`)

**Por que:** o `aplicar` recusa a API e o backup em qualquer estado vivo, e qualquer conexão ao banco:
eles abririam conexão no meio da troca. O Postgres fica de pé. **Daqui até o passo 6, a nuvem está fora
do ar.**

```bash
docker compose --project-name aurora-whisper --env-file ~/aurora-whisper/config.env -f ~/aurora-whisper/deploy/compose.yml stop aurora-whisper-api aurora-whisper-backup
```

```bash
docker ps --filter name=aurora-whisper --format "table {{.Names}}\t{{.Status}}"
```

**Deve aparecer:** só o `aurora-whisper-postgres`, com `(healthy)`.

## 4. Decifrar no PC e mandar à VPS (no PowerShell do PC)

**Por que:** a chave privada nunca sai do PC. A decifra é num contêiner da imagem do backup, sem rede,
com a chave e o arquivo montados só para leitura, e o dump decifrado vai direto, pelo `ssh`, para a
memória só sua da VPS: nunca para o disco de nenhum dos dois. O comando vai pelo `cmd`, que passa os
bytes sem mexer (o cano do PowerShell 5.1 os estragaria).

Com o Docker Desktop aberto (a imagem `aurora-whisper-backup:producao` já está no PC: o
`deploy-teste.ps1` e o teste do mês a montam), troque o nome do arquivo pelo que você baixou:

```powershell
cmd /d /c "docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges -v %USERPROFILE%\.aurora-whisper-dev\backup:/chave:ro -v %USERPROFILE%\Downloads\aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age:/entrada/backup.dump.age:ro aurora-whisper-backup:producao age --decrypt --identity /chave/aurora-whisper-backup.key /entrada/backup.dump.age | ssh vps bash ~/aurora-whisper/deploy/scripts/restaurar.sh receber"
```

**Deve aparecer:** "OK: dump recebido e conferido ate o fim (N bytes, N itens no indice), em memoria" e
o próximo passo. Se aparecer o **AVISO** do swap, siga logo para o passo 5 (a memória pode ir para o
disco do swap). Se aparecer **FALHOU**, nada mudou no banco: a frase diz o porquê (o dump cortado, o
arquivo errado, a sessão do passo 2 fechada).

## 5. Aplicar (na sessão `ssh` do passo 2)

**Por que:** restaura num banco **novo**, roda nele o `migrate` e o `pos-restauracao` (o epoch novo e
tudo revogado), e só por último troca os nomes. Qualquer parada antes da troca deixa o banco de agora
como estava.

```bash
bash ~/aurora-whisper/deploy/scripts/restaurar.sh aplicar
```

(VPS nova, com o banco vazio: `bash ~/aurora-whisper/deploy/scripts/restaurar.sh aplicar
--sem-copia-antes`.)

Ele mostra os avisos e pergunta: **digite `RESTAURAR`**. Se o backup é de antes de uma atualização, ele
pergunta também "Ha migration pendente. Digite SIM": digite `SIM` (migration em produção é ponto de
parada; aqui é a do próprio backup se pondo em dia).

**Deve aparecer:** "OK: copia cifrada do banco atual em …", "OK: dump restaurado no banco novo …",
"OK: o banco … esta pronto (migrations e pos-restauracao)", "OK: o banco restaurado agora e o aurora; o
anterior ficou como aurora_antes_AAAAMMDD" e "OK: restaurado." No fim, ele imprime o `DROP` do banco
antigo e o `rm` da cópia cifrada, para daqui a 7 dias: **guarde as duas linhas** (são as do passo 8).

Se parar no meio, a frase diz o que ficou: o banco de agora não foi tocado, e o banco novo, se já
existia, sai com o `DROP` dele impresso, para recomeçar do passo 4. Para desistir antes do
"RESTAURAR":

```bash
bash ~/aurora-whisper/deploy/scripts/restaurar.sh descartar
```

## 6. Subir de novo (na sessão `ssh`)

**Por que:** é o mesmo do passo 7 do deploy. A API roda a arrumação e a retenção (o que já venceu sai)
**antes** de abrir a porta, e o backup volta.

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

Responda `SIM` à pergunta da API. **Deve aparecer:** os mesmos `OK:` do passo 7 do `docs/deploy-f11.md`
e "OK: a nuvem esta no ar". Depois, o primeiro backup do banco restaurado:

```bash
docker exec aurora-whisper-backup backup.sh
```

**Deve aparecer:** `backup: OK (…)`. Agora pode fechar a sessão `ssh` (`exit`).

## 7. Conferir e reconectar

1. No PC, a conferência de fora (o passo 8 do `docs/deploy-f11.md`; troque `<IP-DA-VPS>` pelo IP do
   seu servidor):

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio aurora.exemplo.com.br -IPEsperado <IP-DA-VPS>
```

2. No navegador, entre de novo pelo Google (`https://aurora.exemplo.com.br`).
3. Em cada PC, conecte de novo: no painel do Agent, **Conectar à nuvem**, como no passo 8 do
   `docs/teste-final-f11.md`. **Deve aparecer:** "Conectado como …", e o PC recomeça lendo a nuvem.
4. No celular, entre de novo no app.

Apague o arquivo baixado de **Downloads** quando quiser (ele é cifrado, mas não precisa ficar).

## 8. Daqui a 7 dias: o banco antigo (seu, à mão)

**Por que:** o banco de antes da restauração fica 7 dias para o caso de algo ter dado errado; depois, é
dado que não deve mais existir (a política diz 7 dias). É destrutivo: só depois de conferir que a nuvem
está certa. Use as duas linhas que o `aplicar` imprimiu, ou estas, trocando a data pela do nome do banco:

```bash
docker exec -it aurora-whisper-postgres psql -U postgres -d postgres -c 'DROP DATABASE aurora_antes_AAAAMMDD'
```

```bash
rm -f ~/aurora-whisper/antes-de-restaurar/aurora-whisper-antes-de-restaurar-AAAAMMDDTHHMMSSZ.dump.age
```

Para ver os nomes, só lendo:

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d postgres -c "SELECT datname FROM pg_database WHERE datname LIKE 'aurora_%'"
```

```bash
ls ~/aurora-whisper/antes-de-restaurar/
```

## O que me contar

1. Qual backup você restaurou (a data do nome) e se o teste do passo 1 passou.
2. As linhas `OK:` (ou o FALHOU) do `receber`, do `aplicar` e do `subir.sh`.
3. Se cada PC e o celular voltaram, e se algum ditado apagado voltou ou ganhou um selo.
4. Daqui a 7 dias, que o `DROP` foi feito.
