# O teste final da nuvem (F11): o celular, o PC e os seus ditados

**O que é e por que:** com a nuvem no ar (`docs/deploy-f11.md`), este roteiro usa ela de verdade: instala o
app no celular, liga o PC do `bin\` à nuvem de produção, manda o seu histórico, e confere que uma
mudança feita num lado aparece no outro, que o aprendido vai e volta, que revogar desliga o PC e que
ficar sem internet não perde nada. É também o teste que **cobre os testes à mão da F8 e da F10** que
você adiou em 09-out-2026 (enviar o histórico, aprender e desfazer pelo celular).

ADR-019, "Decisões da F11" (o teste final). Tempo estimado: 45 minutos a 1 hora.

**O endereço é `https://aurora-whisper.mjj.net.br`** em todo o roteiro.

## Antes de começar (são cinco avisos)

1. **Só depois do OK da LGPD.** Este é o primeiro roteiro em que o seu texto ditado sobe para a nuvem.
   Se você ainda não respondeu "LGPD aprovada" ao dossiê, pare aqui.
2. **O histórico do `bin\` vai para a nuvem de verdade:** os ditados dos últimos 6 meses, sem a lixeira.
   Antes, o PC faz uma cópia do banco ao lado dele (`historico.db.antes-da-nuvem-….db`), que só sai pelo
   botão **Apagar as cópias**.
3. **A nuvem de teste (`http://localhost:8080`) fica desligada.** O PC deixa de conhecê-la no passo 7.
4. **O Agent aberto fecha durante o teste** (a instância é única): o passo 3 o fecha; o 6 abre o novo.
5. **Uma palavra-sentinela.** Escolha agora uma palavra rara, que você nunca dita (por exemplo,
   **pterodáctilo**). Você vai ditá-la no passo 10 e, no passo 15, contar quantas vezes ela aparece nos
   logs da API, do Postgres e do Traefik. O certo é **zero**: os logs nunca têm texto ditado.

Use o celular, o navegador do PC, uma janela do PowerShell e uma sessão `ssh` na VPS (`ssh vps`, com o
apelido do seu acesso ssh, como no `docs/deploy-f11.md`) para os passos que dizem "na VPS".

# Parte 1: o celular

## 1. Instalar o app e entrar

**Por que:** é o app que você vai usar no dia a dia, e só com o https de verdade ele instala de fato (no
teste da F5, pelo cabo, o Chrome podia criar só um atalho).

1. No Chrome do celular, abra `https://aurora-whisper.mjj.net.br`.
2. **Entrar com o Google**, com a conta do e-mail que você deu ao `gerar-segredos.sh`. Se o Google mostrar
   o aviso de "app não verificado", siga por **Avançado** (é o seu próprio app, em modo de teste).
3. **⋮** > **Instalar app** (ou **Adicionar à tela inicial** > **Instalar**).

**Deve aparecer:** na tela inicial, o ícone **Whisper** inteiro no formato do launcher (sem o selo
pequeno do Chrome no canto); o app abre **sem a barra de endereço**, com a tela de abertura escura, e
mostra o histórico vazio (nenhum ditado subiu ainda).

## 2. Sem internet, no celular

1. Ligue o **modo avião**. Feche o app pelos recentes e abra de novo pelo ícone.
   **Deve aparecer:** "**Sem conexão.**", com **Tentar de novo**. Não pode aparecer o dinossauro nem
   "Não é possível acessar esse site".
2. Desligue o modo avião e toque em **Tentar de novo**.
   **Deve aparecer:** o app de volta, ainda logado.

# Parte 2: o PC

## 3. Fechar o Agent e conferir

Veja qual Agent está aberto (só leitura):

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue | Select-Object Id, Path
```

**Deve aparecer:** o `Path` em `C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe`. Se for
outro (o Agent instalado tem o mesmo nome), **pare e me avise** antes de fechar. Se não aparecer nada,
siga.

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

**Deve aparecer:** nada. Se aparecer, espere uns segundos e rode de novo.

## 4. Montar o painel do PC (passo próprio)

**Por que:** o painel vai para `bin\painel`; sem este passo, o Agent novo abre o painel velho.

```powershell
pnpm -C C:/Desenvolvimento/aurora-whisper/web build
```

**Deve aparecer:** "built in …" no fim.

## 5. Montar o Agent do `bin\`

```powershell
go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

**Deve aparecer:** nada (é o certo).

## 6. Abrir o Agent e conferir que é o novo

```powershell
Start-Process C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent | Select-Object StartTime, Path
```

**Deve aparecer:** o `StartTime` com a hora de agora e o `Path` em `...\aurora-whisper\bin\`. Se a hora
for antiga, o Agent velho continua aberto: volte ao passo 3.

## 7. Desconectar e esquecer a nuvem de teste

**Por que:** o PC ainda guarda a nuvem de teste (`http://localhost:8080`) e a conta dela; ligado a uma
nuvem, ele recusa outra ("Este PC já está ligado a outra conta.").

1. Abra o painel pela bandeja: **Configurações** > **Nuvem**.
2. Se aparecer **Desconectar**, clique nele. **Deve aparecer:** "Desligada. Nada vai para a nuvem até
   você conectar." e, embaixo, **Esquecer esta nuvem**. (Se a seção já estiver assim, siga.)
3. **Esquecer esta nuvem** > **Esquecer**. **Deve aparecer:** "Este PC esqueceu a nuvem." e o campo do
   endereço vazio. Os seus ditados continuam no **Histórico**.

## 8. Conectar à nuvem de produção

1. No campo do endereço, digite `https://aurora-whisper.mjj.net.br` e clique em **Conectar à nuvem**.
2. Na aba que abre: se pedir, **Entrar com o Google** (a mesma conta). Confira que o código é o mesmo do
   painel e clique em **Aprovar**. **Deve aparecer:** "**PC conectado**". Feche a aba.

**Deve aparecer, no painel:** "Conectado como **<o seu e-mail>**." e, em alguns segundos, a pergunta
"**Vão subir N ditados dos últimos 6 meses. A lixeira não vai. Antes, uma cópia do histórico fica neste
PC.**", com **Enviar o histórico**. **Anote o N.** Na bandeja: "Nuvem: em dia".

No celular, no app: **Configurações** > **Aparelhos**. **Deve aparecer:** o nome deste PC.

## 9. Enviar o histórico

1. Clique em **Enviar o histórico**. **Deve aparecer:** "Copiando o histórico…", depois "Enviando o
   histórico: X de N." e, no fim, "O histórico subiu para a nuvem." e "Em dia com a nuvem. Conferido às
   HH:MM.". Se a nuvem recusar algum ditado, aparece "**k ditados não subiram para a nuvem.**": anote o
   k (eles ficam só no PC).
2. A cópia, ao lado do banco (só leitura):

```powershell
Get-ChildItem C:/Desenvolvimento/aurora-whisper/bin/historico.db.antes-da-nuvem-*.db | Select-Object Name, Length, LastWriteTime
```

   **Deve aparecer:** um arquivo com a hora do clique, do tamanho do `historico.db` (ou quase).
3. No celular, recarregue o app (puxe a lista para baixo, ou feche e abra). **Deve aparecer:** os seus
   ditados mais novos no topo, na mesma ordem do Histórico do PC; a **Lixeira** do app vazia.
4. **O efeito nos outros serviços** (na VPS, só leitura), logo depois do envio:

```bash
docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}"
```

   **Deve aparecer:** a `aurora-whisper-api` abaixo de 256 MiB e o `aurora-whisper-postgres` abaixo de
   512 MiB. Anote a CPU dos outros serviços do servidor, para me contar.

## 10. Ditar (com a palavra-sentinela) e ver no celular

1. Num Bloco de Notas, dite uma frase com a palavra-sentinela (aviso 5). Veja como ela saiu escrita.
2. No celular, recarregue o app.

**Deve aparecer:** a frase no topo do histórico do app, em até 1 minuto.

## 11. Editar no celular e ver no PC

1. No app, abra a frase do passo 10, mude uma palavra (não a sentinela) e **Salvar**.
2. No PC, clique em **Histórico** (ou volte à aba).

**Deve aparecer:** a mudança no PC. Com o painel do PC aberto, em até cerca de 13 s: o Agent busca a
nuvem a cada 10 s enquanto o painel está aberto, e o painel lê o contador de mudanças a cada 3 s e
relê a lista sozinho (ADR-021, subfatia S). Com o painel fechado, em até 1 minuto (o Agent busca a
cada 60 s).

## 12. Aprender no PC e desfazer pelo celular

**Por que:** é o teste à mão da F8: a troca aprendida sobe para a nuvem, chega ao app e, desfeita lá,
some do PC.

1. Num Bloco de Notas, dite: **"o contrato está assinado"**.
2. No painel, **Histórico**: clique no ditado do topo e, no **Texto final**, troque **contrato** por
   **acordo**. **Salvar**. Na pílula **Aprender “contrato” → “acordo”?**, clique em **Aprender**.
   **Deve aparecer:** "Aprendido: “contrato” vira “acordo” no próximo ditado." (Se o ASR escreveu
   "Contrato", use a palavra como ele escreveu.)
3. No celular, recarregue o app e abra **Configurações**. **Deve aparecer**, em até 1 minuto, a seção
   **Aprendido** com `contrato → acordo` e **Desfazer**.
4. No celular, **Desfazer** > **Desfazer**. **Deve aparecer:** "Desfeito em todos os aparelhos." e a
   seção "Nada aprendido ainda."
5. No PC, espere até 1 minuto (cerca de 13 s, com o painel aberto) e clique em **Histórico** e em
   **Configurações** (o aviso de mudanças do painel não conta o que é só do aprendido, e a seção só
   relê quando a tela aparece). **Deve aparecer:** a
   seção **Aprendido no painel** sem a troca.
6. Dite de novo **"o contrato está assinado"**. **Deve aparecer:** "contrato", e não mais "acordo".

## 13. Revogar o PC pelo celular

1. No app, **Configurações** > **Aparelhos** > o nome deste PC > **Revogar** (confirme).
2. No PC, espere até 2 minutos. **Deve aparecer**, na seção **Nuvem**: "**Este PC foi desconectado na
   nuvem.**". Dite uma frase: ela entra normalmente no PC (e não sobe).
3. Conecte de novo pelo passo 8 (o mesmo endereço já está no campo). **Deve aparecer:** "Conectado como
   …" e **nenhuma** pergunta de envio (o histórico já foi aceito); a frase do item 2 sobe sozinha e
   aparece no app em até 1 minuto.

## 14. O PC sem internet, e a volta

1. Desligue o Wi-Fi (ou tire o cabo) do PC. **Deve aparecer, na bandeja**, em até 1 minuto: "Nuvem: sem
   conexão".
2. Dite uma frase. **Deve aparecer:** a frase no Histórico do PC, normal: ditar não depende da nuvem.
3. Ligue a internet de novo. **Deve aparecer:** na bandeja, "Nuvem: em dia" em até 1 minuto, e a frase do
   item 2 no app do celular (recarregando).

## 15. A palavra-sentinela nos logs (na VPS, só leitura)

**Por que:** os logs da API, do Postgres e do Traefik nunca podem ter texto ditado (é critério de parar
o rollout). Troque `PALAVRA` pela sentinela **como ela saiu escrita** no passo 10, sem acento se o ASR a
escreveu sem:

```bash
docker logs aurora-whisper-api 2>&1 | grep -c -i "PALAVRA"
```

```bash
docker logs aurora-whisper-postgres 2>&1 | grep -c -i "PALAVRA"
```

```bash
docker logs --since 3h traefik 2>&1 | grep -c -i "PALAVRA"
```

**Deve aparecer:** `0` nos três. Qualquer outro número: **pare** e me conte (sem colar a linha).

## 16. Os 6 meses no PC (a F10), só olhando

**Por que:** o outro teste à mão adiado. Os 6 meses só têm o que mostrar no `bin\` a partir de março de
2027 (o primeiro ditado é de 29-set-2026); até lá, quem prova é o teste automático.

No painel: **Configurações** > **Histórico**. **Deve aparecer:** a contagem dos ditados com mais de 6
meses (o mais provável é nenhum) e, se houver, a pergunta. **Não ligue a regra** se não quiser: ela só
liga pelo seu clique.

## O que me contar

1. Se o app instalou de verdade (sem o selo do Chrome) e se o "Sem conexão." apareceu (passos 1 e 2).
2. O N e o k do envio (passos 8 e 9), e o `docker stats` (CPU dos outros serviços do servidor).
3. Os tempos de ida e volta (passos 10, 11 e 12): dentro de 1 minuto? (No passo 11, com o painel do
   PC aberto, o esperado é cerca de 13 s.)
4. Se a revogação desligou o PC e se a volta mandou a frase (passo 13), e a volta da internet (passo
   14).
5. Os três `0` da sentinela (passo 15).

A cópia `historico.db.antes-da-nuvem-….db` fica até você usar **Apagar as cópias** (Configurações >
Histórico), depois de conferir o app.

## O que este roteiro não testa

- **A restauração em produção.** Ela só se faz num desastre, pelo `docs/restauracao-f11.md`. A troca de
  epoch foi provada no `deploy-teste.ps1` (o backup restaurado num banco novo, com o
  `pos-restauracao`), e a cifra e a chave, no teste do mês (`teste-restauracao-mensal.ps1`, que você
  roda uma vez por mês).
- **O selo de conflito** e a **lixeira** nos dois sentidos, que o roteiro da F7 já conferiu.
- **O iPhone**, que fica para depois do MVP.
