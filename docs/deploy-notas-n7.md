# O deploy das notas na nuvem (N7), passo a passo

**O que é e por que:** a fase N7 (ADR-022, "Sync") leva as notas e os marcadores do PC para a nuvem e
de volta, e faz a nuvem esconder do histórico o ditado que virou nota. Para isso a nuvem ganha seis
tabelas novas (as migrations `0020` a `0025`) e um servidor novo. Este roteiro é o que **você** roda;
eu não rodo nada no servidor nem no Docker. Ele usa os mesmos scripts do `docs/deploy-f11.md` (o
`deploy-teste.ps1`, o `enviar-para-servidor.ps1` e o `subir.sh`), na mesma VPS que já está no ar.

**Pontos de parada (são seus):** o backup antes, as migrations `0020` a `0025` em produção, a imagem
nova da API e, por último, o Agent novo nos PCs. Nada disso foi rodado por mim.

**A ordem importa, e é esta:** o backup → o teste no Docker do PC → a imagem para a VPS → o `subir.sh`
(as migrations e a API nova) → conferir → **só então** o Agent novo nos PCs. O Agent novo só manda
nota depois de ver que a nuvem sabe de notas (o campo `entidades` do pull), então ele não estraga nada
se for instalado antes; mas as notas só sobem depois do passo 4, e o primeiro pull depois dele lê tudo
de novo uma vez (é esperado: o cursor volta a 0, e os ditados voltam como eco, sem mudar nada).

Tempo estimado: 30 a 40 minutos.

## Antes de começar

1. **Nada do Traefik nem dos outros sites é tocado.** Como no `docs/deploy-f11.md`, aviso 2.
2. **Nunca `docker compose down -v` na nuvem** (apaga o volume do Postgres).
3. **As migrations são só aditivas:** cada uma cria uma tabela nova (e os índices dela). Nenhuma tabela
   que já existe muda, nenhuma linha é apagada. Mesmo assim, o backup vem antes.
4. **Duas janelas:** o PowerShell no PC e uma sessão `ssh vps` na VPS, como no `docs/deploy-f11.md`.

## 1. O backup, antes de tudo (na VPS)

**Por que:** é a volta garantida, se algo der errado no meio. O backup é o mesmo de todo dia (cifrado e
mandado ao B2).

```bash
docker exec aurora-whisper-backup backup.sh
```

**Deve aparecer:** `backup: OK (aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age, N bytes, N s)`. Anote o nome
do arquivo. Se aparecer outra coisa, **pare** e me mande a linha.

## 2. O teste no Docker do PC

**Por que:** a imagem nova sobe inteira no seu PC, com o mesmo compose da VPS, e aplica as `0001` a
`0025` num Postgres de teste. É o mesmo passo 4 do `docs/deploy-f11.md`. Com o Docker Desktop aberto:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\deploy-teste.ps1
```

**Deve aparecer**, no fim: **"OK: o deploy passou no Docker do PC"**. Se aparecer **FALHOU**, o script
já derrubou tudo: me mande a linha do FALHOU e as de log que ele mostrou. Nada foi para a VPS.

**Os testes do Postgres (opcional, mas recomendado, no PC):** os testes das tabelas novas contra um
Postgres de verdade não rodaram na minha máquina (o Docker estava parado). Para rodá-los:

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1
```

Cole a linha `$env:AURORA_TEST_POSTGRES_URL = '...'` que ele imprime, e então:

```powershell
go -C C:\Desenvolvimento\aurora-whisper test ./migrations/ ./internal/server/... -count=1
```

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1 -Parar
```

**Deve aparecer:** `ok` em cada pacote. Um `FAIL` aqui é ponto de parada: me mande o nome do teste e as
linhas dele, antes do passo 3.

## 3. A imagem nova para a VPS

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\enviar-para-servidor.ps1 -Dominio aurora.exemplo.com.br
```

(Troque `aurora.exemplo.com.br` pelo seu domínio, como no `docs/deploy-f11.md`. **Sem** o
`-ArquivoGoogle`: o JSON do Google já está na VPS.)

**Deve aparecer:** "OK: as duas imagens sao as que passaram no deploy-teste.ps1", o `Loaded image:` e
"OK: tudo na VPS.".

## 4. As migrations e a API nova (na VPS)

**Por que:** o `subir.sh` confere o Postgres, mostra as migrations pendentes e **para e pergunta** antes
de rodá-las; depois, pergunta de novo antes de subir a API nova.

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

As pendentes são estas seis, **só aditivas**:

| Migration | O que cria |
|---|---|
| `0020_labels_table` | os marcadores das notas (o nome, a versão) |
| `0021_notes_table` | as notas (título, corpo, cor, fixada, arquivada, posição, lixeira, o selo do conflito) |
| `0022_note_labels_table` | quais marcadores cada nota tem |
| `0023_note_tombstones_table` | as lápides das notas e dos marcadores apagados de vez (só os ids) |
| `0024_note_sync_events_table` | o registro de cada mudança das notas e dos marcadores (sem texto) |
| `0025_transcription_note_links_table` | o ditado que virou nota (o id do ditado e o da nota) |

1. "**Ha migration pendente** … Digite SIM para rodar o migrate agora": confira que a lista é a de cima
   (seis, da `0020` à `0025`) e digite `SIM`. Se a lista tiver outra coisa, **não** digite: me mande a
   lista.
2. "**Digite SIM para subir a API**": digite `SIM`.

**Deve aparecer:** "OK: as migrations em dia", "OK: a API saudavel: arrumacao, retencao e so entao
servindo…" e, no fim, "OK: a nuvem esta no ar".

## 5. Conferir (na VPS, só lendo)

**As tabelas novas existem e estão vazias** (nenhuma nota subiu ainda):

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT 'labels', count(*) FROM labels UNION ALL SELECT 'notes', count(*) FROM notes UNION ALL SELECT 'note_labels', count(*) FROM note_labels UNION ALL SELECT 'note_tombstones', count(*) FROM note_tombstones UNION ALL SELECT 'note_sync_events', count(*) FROM note_sync_events UNION ALL SELECT 'transcription_note_links', count(*) FROM transcription_note_links"
```

**Deve aparecer:** seis linhas, cada uma com `|0` no fim.

**As migrations aplicadas:**

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT version, name FROM schema_migrations WHERE version >= 20 ORDER BY version"
```

**Deve aparecer:** `20|labels_table` até `25|transcription_note_links_table`.

**Os ditados de antes continuam lá** (o número tem de ser o mesmo de antes do passo 4; se você não o
anotou, compare com o que o celular mostra):

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT count(*) FROM transcriptions"
```

De fora, no PC, o mesmo `conferir-nuvem.ps1` do passo 8 do `docs/deploy-f11.md`:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio aurora.exemplo.com.br -IPEsperado <IP-DA-VPS>
```

**Deve aparecer:** "OK: a nuvem em https://aurora.exemplo.com.br responde certo de fora".

## 6. O Agent novo nos PCs

Só depois do passo 5. Feche o Agent, remonte-o como de costume (`scripts\remontar-agent.ps1`) e confira
que o processo novo subiu (o `StartTime` de agora). Depois, o roteiro `docs/teste-manual-notas-n7.md`.

**Na primeira rodada do sync**, o Agent vê que a nuvem sabe de notas, volta o cursor a 0 e lê tudo de
novo uma vez; em seguida sobem os marcadores e as notas que já existem no PC (o "Enviar o histórico"
que você já clicou na F7 vale como o aceite delas). Num PC com muitas notas, isso leva umas rodadas.

**Conferir que as notas subiram (na VPS, só contagens, nunca o texto):**

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT (SELECT count(*) FROM notes) AS notas, (SELECT count(*) FROM labels) AS marcadores, (SELECT count(*) FROM transcription_note_links) AS ditados_que_viraram_nota"
```

**Deve aparecer:** os números de notas (fora da lixeira) e de marcadores do PC, e os ditados que viraram
nota.

## Se algo der errado

- **A API nova não sobe** (o `subir.sh` deu FALHOU depois do `SIM` da API): me mande a linha, e não
  tente voltar à imagem antiga: o servidor recusa subir com uma migration aplicada que o binário dele não
  conhece (`migrations/README.md`, "Como o `migrate` controla a história"). A saída é uma imagem nova
  corrigida, ou o backup, abaixo.
- **Voltar ao estado de antes (rollback):** as migrations não têm "desfazer"; a volta é o backup do
  passo 1, pelo `docs/restauracao-f11.md` (é ponto de parada: me chame antes). A restauração troca o
  epoch da conta: cada PC recomeça sozinho e sobe de novo o que tem (ditados e notas), sem perder nada do
  PC. O backup feito no passo 1 não tem nota nenhuma: o que os PCs tiverem subido depois volta pelo
  recomeço.
- **Tirar a nuvem do ar, sem apagar nada:** o comando do `docs/deploy-f11.md`, "Se algo der errado".
- **O Agent novo instalado antes da nuvem nova:** nada se perde. Ele não manda nota, marcador nem o
  "virou nota" do ditado enquanto a nuvem não disser que sabe de notas; o resto do sync segue como hoje.

## O que me contar

1. A linha do backup do passo 1.
2. Se o `deploy-teste.ps1` passou, e o resultado dos testes do Postgres (se você os rodou).
3. A lista das migrations pendentes e os `OK:` (ou o FALHOU) do `subir.sh`.
4. As três contagens do passo 5 e a do passo 6.
