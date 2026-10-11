# Teste manual da F10: os 6 meses no PC, e a lixeira que espera a nuvem

**O que é e por que:** até hoje, o PC guarda todo ditado para sempre; só a lixeira sai, aos 30 dias.
A F10 traz ao PC a regra que a nuvem já tem: o ditado que completa 6 meses sai de vez deste
computador. Como isso apaga ditado de verdade, a regra **só liga com o seu clique**, depois de o
painel mostrar quantos ditados sairiam. Este roteiro monta o Agent novo no `bin\`, mostra essa
contagem e deixa a decisão com você.

**O que o OK faz (leia antes de qualquer passo):**

- **Apaga de verdade.** Os ditados com mais de 6 meses saem do `historico.db` do `bin\` e não voltam,
  nem pelo **Desligar**. Saem também os que estão na lixeira e os que têm duas versões.
- **Uma cópia fica.** Antes de apagar, o Agent copia o histórico inteiro para um arquivo ao lado do
  banco (`historico.db.antes-dos-6-meses-…db`) e só apaga se a cópia der certo. O Agent nunca apaga
  essa cópia sozinho.
- **Daí em diante, sai sozinho.** O que completar 6 meses sai em até 1 hora, até você clicar em
  **Desligar**.
- **Ver a contagem não apaga nada.** Você pode ir só até ela e parar. Sem o OK, nada dos 6 meses é
  apagado, nunca.

**Provavelmente não há nada a apagar hoje.** O projeto começou em 16-set-2026: o ditado mais antigo
do `bin\` é de setembro de 2026 e só completa 6 meses em março de 2027. Nesse caso a pergunta não
aparece (o passo 6 diz o que aparece no lugar dela), e a decisão fica para o dia em que ela aparecer
sozinha nas Configurações. Para ver hoje a pergunta e o OK, sem tocar em nada seu, o passo 13 abre as
telas com um Agent de mentira.

ADR-019, fatia F10 (F10a a F10f). Tempo estimado: 15 minutos (mais 5 com o passo 13). **Precisa da
F10 juntada à `main`.** Ela traz a F9 junto, e a recomendação da F9 é rodar antes os testes de
Postgres (ADR-019, "O que o Mauro providencia", F9).

**O que conferir, em uma frase:** montar e abrir o Agent novo; ver o bloco **Histórico** nas
Configurações e a contagem; decidir; se der o OK, conferir a cópia e o que saiu; e, se quiser, apagar
as cópias.

## Antes de começar (são cinco avisos)

1. **O Agent aberto fecha durante o teste** (a instância é única). O passo 1 o fecha; o 4 abre o novo.
2. **Não precisa da nuvem nem de internet.** Os 6 meses valem também num PC que nunca conectou. Se o
   `bin\` ainda estiver ligado a uma nuvem de teste desligada, a seção **Nuvem** diz que ela não
   responde; isso não atrapalha.
3. **O Agent novo não muda o `historico.db` ao abrir:** a F10 não tem migration. O que muda já na
   subida é o expurgo da lixeira, que passa a **apagar menos** que antes: a lixeira que a nuvem ainda
   não soube fica (passo 12).
4. **Só o `bin\`.** Tudo aqui é no Agent de `C:\Desenvolvimento\aurora-whisper\bin\`, com o histórico
   dele. O Agent instalado (o do dia a dia, se houver) guarda o histórico em outra pasta, e este
   roteiro não o toca.
5. **Se você ligar a regra e o Agent do `bin\` ficar mais de 7 dias fechado**, na volta ela estará
   pausada, com a frase "A regra foi pausada: o Agent ficou parado por mais de 7 dias, ou o relógio do
   PC mudou. Nada foi apagado." É o freio, e é o certo: nada sai até você dar o OK de novo.

Use uma janela do PowerShell para tudo (o passo 13, opcional, pede uma segunda).

## 1. Fechar o Agent e conferir

Primeiro, veja qual Agent está aberto (só leitura):

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue | Select-Object Id, Path
```

**Deve aparecer:** o `Path` em `C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe`. Se o
`Path` for outro (o Agent instalado, o do dia a dia, tem o mesmo nome de arquivo), **pare e me avise**
antes de fechar: o `taskkill` abaixo o fecharia também. Se não aparecer nada, não há Agent aberto: o
`taskkill` só vai reclamar que não achou o processo, e o passo 2 segue igual.

O `taskkill` sem `/F` é o mesmo que **Sair** na bandeja.

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

**Deve aparecer:** o `Get-Process` não mostra nada. Se mostrar, espere uns segundos e rode de novo.

## 2. Montar o painel do PC (passo próprio)

O painel vai para `bin\painel`; sem ele, o Agent novo abre o painel velho, sem o bloco **Histórico**.

```powershell
pnpm -C C:/Desenvolvimento/aurora-whisper/web build
```

**Deve aparecer:** "built in …" no fim.

## 3. Montar o Agent do `bin\`

```powershell
go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

**Deve aparecer:** nada (é o certo).

## 4. Abrir o Agent do `bin\` e conferir que é o novo

```powershell
Start-Process C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent | Select-Object StartTime, Path
```

**Deve aparecer:** o `StartTime` com a hora de agora e o `Path` em `...\aurora-whisper\bin\`. Se a hora
for antiga, o Agent velho continua aberto: volte ao passo 1.

O ícone volta à bandeja quando o modelo termina de carregar.

## 5. O expurgo novo, no log (só leitura)

O Agent roda o expurgo na subida e de hora em hora. A linha da subida é a prova de que ele é o novo:

```powershell
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "lixeira: expurgo" | Select-Object -Last 1
```

**Deve aparecer:** uma linha com a hora de agora, com `apagados=` e `esperando_a_nuvem=`. O
`esperando_a_nuvem` é novo: é quantos ditados da lixeira o PC está segurando porque a nuvem ainda não
soube deles (o passo 12). Hoje deve dar `0`. Se a linha não tiver `esperando_a_nuvem`, ela é do Agent
velho: volte ao passo 1. Anote os dois números.

Depois, se o passo dos 6 meses roda neste histórico:

```powershell
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "6 meses pulados" | Select-Object -Last 1
```

**Deve aparecer:** nada. Uma linha com a hora de agora (com `motivo=sem_dono_fixo`) quer dizer que o
histórico do `bin\` não tem o dono fixo: o passo dos 6 meses não roda e a contagem vem zerada. Nesse
caso, **pare e me avise**.

## 6. O bloco Histórico nas Configurações

1. Feche qualquer aba antiga do painel e abra o painel pela bandeja (**Abrir Aurora Whisper**).
   Atualizar a aba velha não basta.
2. Clique em **Configurações** e desça até a seção **Nuvem**. O bloco **Histórico** fica logo depois
   dela, antes da **Privacidade**, e **só aparece quando há o que decidir**. Você vai ver um destes
   três:

   - **A. A pergunta** (há ditados com mais de 6 meses):

     > N ditados têm mais de 6 meses. Ao confirmar, eles saem deste PC de vez, e daqui em diante o que
     > fizer 6 meses sai sozinho. Antes, uma cópia do histórico fica neste PC.

     com o botão **Apagar os mais antigos**. Embaixo, cada uma só se houver: "M deles estão na
     lixeira.", "K têm duas versões, e as duas saem." e "J são de outro dono e ficam." (com um ditado
     só: "1 ditado tem mais de 6 meses. Ao confirmar, ele sai…", "Ele está na lixeira." e "Ele tem
     duas versões, e as duas saem."). Mais abaixo, se houver cópias do banco, vem a linha delas (a
     do caso B). **Anote o N, e o M, o K e o J, se aparecerem.** Siga para o passo 7.
   - **B. Só as cópias** (nada a apagar, mas há cópias do banco ao lado dele): "Há C cópias do
     histórico ao lado do banco, com o texto de antes." (ou "Há 1 cópia…") e o botão **Apagar as
     cópias**. Se uma delas for a de um envio à nuvem que não terminou, vem também "1 delas é a do
     envio do histórico à nuvem, que fica até o envio terminar." **Anote o C.** Não há o que decidir
     sobre os 6 meses: siga para o passo 7 e depois pule para o 11.
   - **C. Nada:** a **Privacidade** vem logo depois da **Nuvem**, sem o bloco. Não há ditado com mais
     de 6 meses nem cópia ao lado do banco. Pule para o passo 12.

   Se aparecer "Este Agent ainda não tem os 6 meses. Reinicie o Agent.", o painel é o novo e o Agent é
   o velho: volte ao passo 1.

## 7. As cópias que já estão ao lado do banco (só leitura)

```powershell
Get-ChildItem C:/Desenvolvimento/aurora-whisper/bin/historico.db.antes-*.db | Select-Object Name, Length, LastWriteTime
```

**Deve aparecer:** a lista das cópias, uma por linha (ou nada, no caso C). Cada nome diz de onde veio:
`antes-do-dono` (a migração para o dono fixo), `antes-da-nuvem` (o envio do histórico à nuvem, na F7
ou na F8) e `antes-dos-6-meses` (o OK da F10; ainda não deve haver nenhuma). Todas guardam o texto
inteiro do histórico do dia em que foram feitas, inclusive a lixeira. O C do bloco deve ser o número
de linhas daqui; se a lista tiver mais arquivos que o C, algum nome não segue o padrão exato, o botão
não o apaga, e eu quero saber qual.

## 8. Decidir (só no caso A)

**O OK apaga de verdade.** Os N ditados saem do `historico.db` do `bin\` e não voltam; só a cópia
os guarda. Os que têm duas versões saem sem você escolher uma. O que fica: os ditados com menos de 6
meses, as linhas de outro dono (o J) e tudo o que você aprendeu (o dicionário e as trocas do
**Aprender**).

1. Clique em **Apagar os mais antigos**. **Deve aparecer**, logo abaixo, na própria linha (sem
   janela): "**Apagar N ditados?** Não dá para desfazer.", com **Apagar** (em vermelho) e
   **Cancelar**. O contorno de foco está no **Cancelar**.
2. Aperte **Esc**. **Deve aparecer:** a confirmação some e volta o botão **Apagar os mais antigos**.
   Nada foi apagado. (Clicar em **Cancelar** faz o mesmo.)
3. **A decisão é sua.** Se não quiser ligar a regra agora, pare aqui: nada foi apagado, o PC continua
   guardando tudo, e a pergunta fica nas Configurações (só lá: sem aviso, sem janela). Pule para o
   passo 11.
4. Para ligar: clique em **Apagar os mais antigos** e, na confirmação, em **Apagar**.
   **Deve aparecer:** o botão vira "Copiando e apagando…" (com milhares de ditados, uns segundos) e,
   no fim, o aviso "N ditados saíram deste PC." O histórico é relido.
   - **"A contagem mudou."**, com a pergunta trazendo outro número: entre a tela e o clique, um
     ditado completou 6 meses (ou a lixeira levou um). Nada foi apagado. Confira o número novo e
     confirme de novo, se quiser.
   - **"Não há nada a apagar agora."**, e a pergunta some: o que havia saiu pela lixeira antes do
     clique. Nada foi copiado nem ligado.
   - **"Não deu para copiar o histórico. Nada foi apagado."** (às vezes com "Falta espaço no disco."
     ou "Já há 3 cópias de antes dos 6 meses ao lado do histórico: apague as que não quiser."): a
     cópia falhou, nada foi apagado e a regra continua desligada. Anote a frase.

## 9. Depois do OK

1. O bloco **Histórico** agora diz "Os 6 meses estão ligados: o que fizer 6 meses sai sozinho deste
   PC, em até 1 hora.", com o botão **Desligar** e, embaixo, "Desligar volta ao de antes: o PC guarda
   tudo. O que já saiu não volta." Mais abaixo, a linha das cópias, com uma a mais que no passo 7.
2. Na **Privacidade**, logo depois, entra "O que completa 6 meses é apagado deste computador."
3. A cópia, ao lado do banco:

```powershell
Get-ChildItem C:/Desenvolvimento/aurora-whisper/bin/historico.db.antes-dos-6-meses-*.db | Select-Object Name, Length, LastWriteTime
```

   **Deve aparecer:** um arquivo com a hora do clique, do tamanho do `historico.db` (ou quase).
4. O log, só com números:

```powershell
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "historico: 6 meses" | Select-Object -Last 3
```

   **Deve aparecer:** uma linha "historico: 6 meses ligados" com `previa=N`, `apagados=N` (o mesmo N do
   passo 6) e `copia=feita`. Nenhum texto, id ou caminho.
5. (Opcional; com muitos ditados, demora.) No **Histórico** do painel, clique em **Mostrar mais
   antigos** até o botão sumir: o ditado mais antigo tem menos de 6 meses. Na **Lixeira**, também.

## 10. Desligar, ou deixar ligada

Também é decisão sua. Ligada, o que completar 6 meses sai sozinho do `bin\`, em até 1 hora, enquanto o
Agent do `bin\` estiver aberto (e o freio do aviso 5 vale). Para desligar:

1. Clique em **Desligar**. **Deve aparecer:** o aviso "Os 6 meses foram desligados. O PC guarda tudo de
   novo.", a linha "O que completa 6 meses é apagado deste computador." some da **Privacidade**, e o
   bloco fica só com as cópias. O que já saiu não volta: só a cópia o guarda.
2. Se mudar de ideia, a pergunta volta sozinha nas Configurações no dia em que houver o que apagar. O
   OK seguinte não faz outra cópia enquanto a dos 6 meses existir, e a pergunta diz isso ("Não se faz
   outra cópia: a que já está neste PC é de antes, e o que nasceu depois dela não está nela.").

## 11. Apagar as cópias, se quiser (casos A e B)

As cópias guardam o texto que a lixeira e os 6 meses já apagaram: enquanto existirem, esse texto
continua no disco. O Agent nunca as apaga sozinho; só este botão. Ele não mexe no `historico.db`, só
nos arquivos que o passo 7 listou. Pode deixar para depois: nada no roteiro depende disso.

1. Clique em **Apagar as cópias**. **Deve aparecer**, na própria linha: "**Apagar as C cópias?** Elas
   guardam o texto que os 6 meses apagam. Não dá para desfazer." (com uma só: "**Apagar a cópia?** Ela
   guarda…"), com **Apagar** (em vermelho) e **Cancelar**, e o foco no **Cancelar**. **Esc** ou
   **Cancelar**: nada sai.
2. Para apagar: **Apagar**. **Deve aparecer:** "C cópias apagadas." (ou "1 cópia apagada."). Se uma
   delas for a do envio à nuvem que não terminou, ela fica, e o aviso diz "… 1 fica: o envio do
   histórico à nuvem ainda não terminou." Sem mais nada a decidir, o bloco some.
3. Confira com o comando do passo 7. **Deve aparecer:** nada (ou só a do envio que não terminou).

Se a regra estiver ligada, apagar as cópias não a desliga: ela continua apagando o que completa 6
meses, agora sem cópia nenhuma. O próximo OK (depois de um **Desligar** ou do freio) faz uma cópia
nova.

## 12. A lixeira que espera a nuvem (a D3)

**O que é:** a lixeira do PC passou a esperar a nuvem. O ditado que você pôs na lixeira com a nuvem
ainda sem saber (o PC desconectado, por exemplo) fica na lixeira do PC além dos 30 dias, até a nuvem
saber, e no máximo até o ditado completar 6 meses. Num PC que nunca conectou, nada muda: 30 dias.

**Por que não dá para ver à mão:** precisa de um ditado posto na lixeira há mais de 30 dias com o PC
desconectado. Só esperando 30 dias, ou adiantando o relógio do Windows, o que este roteiro não faz:
mexe no resto do PC e dispara o freio dos 6 meses. **A prova é o teste ponta a ponta**, com a nuvem e
o PC de verdade e um relógio de mentira: desconecta, põe na lixeira, passam 31 dias (o ditado fica),
reconecta, a nuvem recebe a lixeira, e só então o PC a apaga. Se quiser vê-lo passar (ele não toca no
`bin\`: usa uma pasta temporária e a nuvem na memória):

```powershell
go -C C:/Desenvolvimento/aurora-whisper test ./internal/server/integration -run TestSyncE2E_D3 -count=1 -v
```

**Deve aparecer:** "--- PASS: TestSyncE2E_D3_ALixeiraDesconectadaEsperaANuvem" e, no fim, "ok".

No dia a dia, o efeito aparece no log do passo 5: `esperando_a_nuvem` maior que 0 é a lixeira que o PC
está segurando para a nuvem.

## 13. As telas sem o Agent (opcional)

Para ver a pergunta, a confirmação e o OK sem ditado velho e sem tocar no `bin\`: o painel abre contra
um Agent de mentira, que vive só na aba do navegador. Numa segunda janela do PowerShell:

```powershell
pnpm -C C:/Desenvolvimento/aurora-whisper/web dev
```

**Deve aparecer:** uma linha `Local:` com o endereço (normalmente `http://localhost:5173/`). Abra no
navegador, trocando o endereço se o seu for outro:

- `http://localhost:5173/telas-dev.html?cenario=historico`: a pergunta com 1.240 ditados, as três
  linhas de baixo e 2 cópias. **Apagar os mais antigos** e **Apagar** ligam a regra no Agent de
  mentira: o bloco passa a dizer que os 6 meses estão ligados, e a **Privacidade** ganha a linha.
- troque o fim por `historico-1` (o singular), `historico-ligado`, `historico-pausado` (a frase do
  freio), `historico-copias`, `historico-envio` (a cópia do envio que fica), `historico-de-novo` (a
  pergunta com a cópia de um OK anterior) ou `historico-velho` (o Agent de antes da F10).

Para desligar, **Ctrl+C** nessa janela.

## 14. Voltar ao Agent do dia a dia

Se você usa o Agent instalado, feche o do `bin\`:

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

**Deve aparecer:** nada. Depois, abra o instalado pelo **Menu Iniciar**. Se a regra ficou ligada no
`bin\`, lembre do aviso 5: depois de 7 dias com o Agent do `bin\` fechado, ela pausa na volta.

## O que me contar

- Qual dos três casos do passo 6 apareceu (A, B ou C), e os números: o N (e o M, o K e o J), o C e
  os dois números do log do passo 5.
- A lista do passo 7 (os nomes bastam), e se ela bateu com o C.
- Se decidiu dar o OK. Se deu: o aviso do passo 8, o arquivo da cópia e a linha do log do passo 9.
- Se desligou (passo 10) ou apagou as cópias (passo 11), o aviso que apareceu.
- Se rodou o teste do passo 12, se passou.
- Qualquer frase diferente das deste roteiro, com um print.
- Só se algo não bater: as últimas linhas do log, que têm só contagens (nunca texto):

```powershell
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "historico:" | Select-Object -Last 8
```

## O que este roteiro não testa

- **A D3 à mão** (passo 12): o teste ponta a ponta prova, com o relógio de mentira.
- **O freio** (o Agent parado mais de 7 dias, ou o relógio que pulou): pede 7 dias ou mexer no
  relógio. Provado por teste automático (o ciclo de verdade, fechado e reaberto 8 dias depois), e o
  cenário `historico-pausado` mostra a tela.
- **"A contagem mudou." e o OK que liga mas não termina** (um lote que cai no meio): pedem uma corrida
  ou uma falha no meio do apagar. Provados por teste automático.
- **O texto apagado fora do disco** (o `secure_delete`): provado por teste, que procura o texto nos
  bytes do arquivo depois de apagar.
- **A lixeira depois do "Esquecer esta nuvem" e com outra conta**, e o ditado que a nuvem recusou:
  provados por teste, uma situação por linha.
- **O Agent instalado**: este roteiro é só no `bin\`.
