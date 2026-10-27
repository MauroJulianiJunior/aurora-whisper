# O deploy das notas no celular (N8), passo a passo

**O que é e por que:** a fase N8 (ADR-022, "As notas no celular (N8)" e "Decisões da N8") leva a aba
**Notas** ao app do celular: o celular passa a criar, editar, marcar e apagar notas, a ditar para uma nota
("Ditar para nota" e o microfone dentro da nota) e a marcar a lista sem rede. Para isso o servidor ganha
rotas novas, o ditado do celular aprende para onde ir, e o trabalho diário de limpeza passa a esvaziar a
lixeira das notas depois de 30 dias. O app do celular vem junto, dentro da mesma imagem do servidor.

**Não há migration nova.** O banco não muda: a N8 usa as seis tabelas que a N7 criou. Por isso este deploy
é mais simples que o da N7, e voltar atrás é só subir a imagem anterior.

Este roteiro é o que **você** roda; eu não rodo nada no servidor. Ele usa os mesmos scripts do
`docs/deploy-f11.md` e do `docs/deploy-notas-n7.md` (o `deploy-teste.ps1`, o `enviar-para-servidor.ps1` e o
`subir.sh`), na mesma VPS que já está no ar.

**Pontos de parada (são seus):** o backup antes e a imagem nova da API. Nada disso foi rodado por mim.

**A ordem:** o backup → o teste no Docker do PC → a imagem para a VPS → o `subir.sh` (a API nova) →
conferir → o roteiro de teste no celular (`docs/teste-manual-notas-n8.md`). **O Agent dos PCs não precisa
mudar**: ele já recebe e manda notas desde a N7, e o que o celular grava chega a ele pelo mesmo caminho.

Tempo estimado: 20 a 30 minutos.

**Nos comandos abaixo, troque:**
- `<seu-dominio>` pelo endereço da sua nuvem (o mesmo dos deploys anteriores);
- `<seu-acesso-ssh>` pelo nome do acesso ssh à VPS que você já usa (o mesmo dos deploys anteriores);
- `<IP-DA-VPS>` pelo IP da VPS.

## Antes de começar

1. **Nada do Traefik nem dos outros sites é tocado.** Como no `docs/deploy-f11.md`, aviso 2.
2. **Nunca `docker compose down -v` na nuvem** (apaga o volume do Postgres).
3. **Duas janelas:** o PowerShell no PC e uma sessão `ssh <seu-acesso-ssh>` na VPS.
4. **O deploy da N7 tem de estar feito** (as migrations `0020` a `0025` no ar). Se você ainda não fez o
   `docs/deploy-notas-n7.md`, faça-o antes, até o fim; este roteiro vem depois dele.

## 1. O backup, antes de tudo (na VPS)

**Por que:** é a volta garantida, se algo der errado no meio. O backup é o mesmo de todo dia (cifrado e
mandado ao B2).

```bash
docker exec aurora-whisper-backup backup.sh
```

**Deve aparecer:** `backup: OK (aurora-whisper-AAAAMMDDTHHMMSSZ.dump.age, N bytes, N s)`. Anote o nome do
arquivo. Se aparecer outra coisa, **pare** e me mande a linha.

**Anote também** quantas notas e quantos ditados a nuvem tem agora (só contagens, nunca o texto), para
comparar depois:

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT (SELECT count(*) FROM notes) AS notas, (SELECT count(*) FROM transcriptions) AS ditados"
```

## 2. O teste no Docker do PC

**Por que:** a imagem nova sobe inteira no seu PC, com o mesmo compose da VPS, num Postgres de teste. É
também aqui que os testes com Postgres desta fase rodam de verdade (eles não rodaram no meu lado: o Docker
estava fechado). Com o Docker Desktop aberto:

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\deploy-teste.ps1
```

**Deve aparecer**, no fim: **"OK: o deploy passou no Docker do PC"**. Se aparecer **FALHOU**, o script já
derrubou tudo: me mande a linha do FALHOU e as de log que ele mostrou. Nada foi para a VPS.

**Os testes do Postgres (peça a mim, com o Docker aberto):** antes do passo 3, me avise que o Docker está
aberto e eu rodo `go test ./migrations/ ./internal/server/... ./cmd/...` contra o Postgres de teste (o
`postgres-de-teste.ps1`). Eles cobrem o que é novo no banco: as escritas do celular, o "Ditar para nota"
gravando a nota e o ditado juntos, o Desfazer e a limpeza da lixeira das notas. **Só siga para o passo 3
quando eu disser que deu tudo `ok`.**

## 3. A imagem nova para a VPS

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\enviar-para-servidor.ps1 -Dominio <seu-dominio> -Servidor <seu-acesso-ssh>
```

(**Sem** o `-ArquivoGoogle`: o JSON do Google já está na VPS.)

**Deve aparecer:** "OK: as duas imagens sao as que passaram no deploy-teste.ps1", o `Loaded image:` e
"OK: tudo na VPS.".

## 4. A API nova (na VPS)

```bash
bash ~/aurora-whisper/deploy/scripts/subir.sh
```

1. **Não deve aparecer** "Ha migration pendente": a N8 não tem migration. Deve aparecer **"OK: as
   migrations em dia"**. Se ele perguntar por migration pendente, **não** digite SIM: me mande a lista.
2. "**Digite SIM para subir a API**": digite `SIM`.

**Deve aparecer:** "OK: a API saudavel: arrumacao, retencao e so entao servindo…" e, no fim, "OK: a nuvem
esta no ar".

## 5. Conferir (na VPS e no PC, só lendo)

**A limpeza diária já sabe das notas.** A linha `retenção` que a API escreve ao subir ganhou o número
`notas` (quantas notas da lixeira com mais de 30 dias ela apagou de vez; deve ser `0` agora, a não ser que
algum PC tenha ficado desligado por mais de um mês com nota na lixeira):

```bash
docker logs aurora-whisper-api --since 15m 2>&1 | grep '"msg":"retenção"'
```

**Deve aparecer:** uma linha com `"notas":0` (ou outro número pequeno) perto do fim. Se não aparecer a
palavra `notas` na linha, a imagem que subiu é a antiga: me avise.

**Nada mudou nas contagens** (as mesmas do passo 1, ou um pouco mais, se algum PC mandou algo no meio):

```bash
docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT (SELECT count(*) FROM notes) AS notas, (SELECT count(*) FROM transcriptions) AS ditados"
```

**De fora, no PC:**

```powershell
powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio <seu-dominio> -IPEsperado <IP-DA-VPS>
```

**Deve aparecer:** "OK: a nuvem em https://<seu-dominio> responde certo de fora".

**O app novo no celular:** abra o app (ou a página da nuvem) no celular e puxe a tela para baixo para
recarregar. Ele busca a versão nova sozinho; se a aba **Notas** não aparecer ao lado do Histórico, feche o
app de vez (tire-o da lista de apps abertos) e abra de novo. Depois, siga o `docs/teste-manual-notas-n8.md`.

## Se algo der errado

- **A API nova não sobe** (o `subir.sh` deu FALHOU depois do `SIM`): me mande a linha. Como não há
  migration nova, a volta é segura: subir a imagem anterior (abaixo).
- **Voltar ao estado de antes (rollback):** **me chame**: eu preparo a imagem da N7 de novo (o commit
  antes da N8) e você roda os passos 3 e 4 com ela. O banco não muda na volta: as notas que o celular gravou
  pela N8 são notas comuns, e a versão anterior as lê e as manda aos PCs normalmente. O que deixa de
  existir é só o que é novo: as escritas pelo celular, o "Ditar para nota" e a limpeza da lixeira das notas
  na nuvem (a do PC continua). O backup do passo 1 só é preciso se algo der errado no banco, o que esta
  fase não toca; se for, a restauração é a do `docs/restauracao-f11.md` (ponto de parada: me chame antes).
- **Tirar a nuvem do ar, sem apagar nada:** o comando do `docs/deploy-f11.md`, "Se algo der errado".
- **O celular sem rede depois do deploy:** é o esperado do teste (parte 4 do roteiro). O que ele marcou
  sem rede sobe sozinho quando a rede volta.

## O que me contar

1. A linha do backup e as duas contagens do passo 1.
2. Se o `deploy-teste.ps1` passou.
3. Os `OK:` (ou o FALHOU) do `subir.sh`, e se ele perguntou por migration (não deveria).
4. A linha `retenção` com o `notas` e as contagens do passo 5.
5. Se a aba Notas apareceu no celular.
