# Teste manual da nuvem, F7: o PC conectado manda os ditados e recebe as mudanças

**O que é:** a F7 inteira (a fila, o laço de sync e, na F7c, o envio do histórico, o selo no PC e o
"Esquecer esta nuvem"), de ponta a ponta, no seu PC: o Agent do `bin\` conectado à nuvem de teste,
que roda na memória, sem Google e sem servidor de verdade.

**Por que com cuidado:** é a primeira vez que o **histórico real do `bin\`** passa pelo sync. Por isso
o envio do histórico só começa quando você clica, e antes dele o Agent faz **uma cópia inteira do
banco** ao lado dele (`bin\historico.db.antes-da-nuvem-…db`). A migration nova (0006) só acrescenta
colunas à fila e uma tabela nova, vazia; nenhum ditado muda (a prova está na ADR-019, "Decisões da
F7c").

ADR-019, fatias F7a, F7b e F7c. Tempo estimado: 30 minutos. Precisa da F7c no `main` (depois do
merge).

**O que conferir, em uma frase:** conectar, enviar o histórico e ver no app; ditar e ver no app;
editar no app e ver no PC; ver e escolher o selo nos dois lados; a lixeira; e, no fim, esquecer a
nuvem sem perder nada.

## Antes de começar (são cinco avisos)

1. **O Agent instalado fecha durante o teste** (a instância é única). O passo 2 o fecha; o 13 o abre
   de novo.
2. **Não desligue a nuvem de teste até o passo 12.** Ela guarda tudo na memória: religada, esquece os
   ditados e o PC, e o painel passa a dizer "Este PC foi desconectado na nuvem." (é só conectar de
   novo, mas o que tinha subido some da nuvem).
3. **O endereço é sempre `http://localhost:8080`**, e não `127.0.0.1`. O `bin\` ficou ligado a ele na
   F6. Se a porta 8080 estiver ocupada e a nuvem subir com `-Porta 8081`, o painel recusa ("Este PC já
   está ligado a outra conta."): aí use o **Esquecer esta nuvem** (passo 13) antes de conectar.
4. **A nuvem de teste sobe vazia** (com `-SemExemplos`, no passo 1): tudo o que a nuvem tem desce
   para o histórico do `bin\`, que é o seu histórico de uso, e os 7 ditados inventados dos outros
   roteiros ficariam lá para sempre.
5. **O token vai para o Gerenciador de Credenciais** no nome `AuroraWhisper/nuvem-portatil`, separado
   do Agent instalado. O passo 13 confere que ele saiu.

Use duas janelas do PowerShell: a da nuvem (passo 1) fica presa no servidor; o resto vai na outra.

## 1. Subir a nuvem de teste (janela 1)

```powershell
powershell -File C:/Desenvolvimento/aurora-whisper/scripts/nuvem-local-teste.ps1 -SemExemplos
```

**Deve aparecer:** "Compilando o painel no modo nuvem...", "Sem os ditados de exemplo: a nuvem sobe
vazia, so com a conta de teste.", depois **"Nuvem de teste em http://localhost:8080"**. Deixe a
janela aberta.

## 2. Fechar o Agent instalado (janela 2)

O `taskkill` sem `/F` é o mesmo que **Sair** na bandeja.

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

**Deve aparecer:** o `Get-Process` não mostra nada. Se mostrar, espere uns segundos e rode de novo.

## 3. Montar o painel do PC (passo próprio)

O painel vai para `bin\painel`; sem ele, o Agent novo abre o painel velho.

```powershell
pnpm -C C:/Desenvolvimento/aurora-whisper/web build
```

**Deve aparecer:** "built in …" no fim.

## 4. Montar o Agent do `bin\`

```powershell
go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

**Deve aparecer:** nada (é o certo).

## 5. Abrir o Agent do `bin\` e conferir que é o novo

```powershell
Start-Process C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent | Select-Object StartTime, Path
```

**Deve aparecer:** o `StartTime` com a hora de agora e o `Path` em `...\aurora-whisper\bin\`. Se a hora
for antiga, o Agent velho continua aberto: volte ao passo 2.

## 6. Conectar (como na F6)

1. Abra o painel pela bandeja. Se uma aba antiga do painel estava aberta, feche-a antes.
2. **Configurações**, seção **Nuvem**: o campo já vem com `http://localhost:8080`. Clique em
   **Conectar à nuvem**.
3. Na aba que abre sozinha: **Entrar com a conta de teste**, confira o código (o mesmo do painel) e
   clique em **Aprovar**. A aba vai para "**PC conectado**". Feche-a e volte ao painel.

**Deve aparecer, na seção Nuvem:** "Conectado como **voce@teste.local**." e, em seguida (é a primeira
conferência com a nuvem, alguns segundos), "Conferindo a nuvem…" e depois a pergunta:
"**Vão subir N ditados dos últimos 6 meses. A lixeira não vai. Antes, uma cópia do histórico fica
neste PC.**", com o botão **Enviar o histórico**. Se a pergunta não aparecer em meio minuto, clique em
**Histórico** e em **Configurações** de novo.

**Anote o N.** Ele é: os ditados do `bin\` dos últimos 6 meses, fora da lixeira. Na **Privacidade**, logo abaixo, entra a frase "Com a nuvem conectada,
o texto dos seus ditados vai para a sua nuvem, em http://localhost:8080. …".

**Na bandeja**, a linha da nuvem: "Nuvem: em dia".

## 7. Enviar o histórico e ver no app

1. Clique em **Enviar o histórico**.
   **Deve aparecer:** o botão vira "Copiando o histórico…" por um instante; depois, "Enviando o
   histórico: X de N." (o X sobe sozinho, com o N do passo 6) e, no fim, essa linha some e aparecem
   o aviso "O histórico subiu para a nuvem." e a linha "Em dia com a nuvem. Conferido às HH:MM.".
   Com mais de 5.000 ditados, o envio para uns segundos no meio (a nuvem pede para esperar) e
   continua sozinho. **Se a nuvem recusar algum ditado** (texto grande demais, ou com a data no
   futuro), o aviso e a linha dizem também "**k ditados não subiram para a nuvem.**" (ou "1 ditado não
   subiu…"): eles ficam só no PC. Anote o k.
2. **Na bandeja**, durante o envio: "Nuvem: enviando X de N".
3. A cópia, ao lado do banco:

```powershell
Get-ChildItem C:/Desenvolvimento/aurora-whisper/bin/historico.db.antes-da-nuvem-*.db | Select-Object Name, Length, LastWriteTime
```

   **Deve aparecer:** um arquivo com a hora do clique, do tamanho do `historico.db` (ou quase).
4. No navegador, abra `http://localhost:8080` (você já está com a conta de teste). Nem o app nem o
   painel mostram o total, então a conferência é por amostra:
   - **as primeiras linhas** do Histórico do app são as mesmas do Histórico do PC, na mesma ordem
     (os seus ditados mais novos);
   - **a Lixeira do app** está vazia;
   - **um ditado da sua lixeira não subiu:** no PC, abra **Lixeira** e escolha uma frase de lá (se
     ela estiver vazia, pule este); no app, no Histórico, aperte **Ctrl+K** e busque essa frase.
     **Deve aparecer:** nada;
   - (opcional, se o histórico não for grande) no app, clique em **Mostrar mais antigos** até o
     botão sumir: o ditado mais antigo do app tem menos de 6 meses. Se o seu histórico do `bin\`
     não tem nada de mais de 6 meses, não há o que ver aqui.

## 8. Ditar e ver no app

1. Dite uma frase qualquer, num bloco de notas.
2. No app, recarregue a página (ou espere: a lista relê ao voltar à aba).

**Deve aparecer:** a frase no topo do histórico do app em poucos segundos (no máximo cerca de 1
minuto).

## 9. Editar no app e ver no PC

1. No app, abra a frase do passo 8, mude uma palavra e clique em **Salvar**.
2. No painel do PC, clique em **Histórico** (ou volte à aba).

**Deve aparecer:** a mudança no PC em até 1 minuto (o PC busca a cada 60 s).

## 10. O selo ("2 versões") nos dois lados

**Um selo feito por você** (a nuvem de teste fica ligada; o PC é que sai e volta):

1. No PC, **Configurações** > **Nuvem** > **Desconectar**.
2. No app, abra a frase do passo 8 e mude o texto para "**versão do app**". Salve.
3. No PC, abra a mesma frase e mude o texto para "**versão do PC**". Salve.
4. No PC, **Conectar à nuvem** e aprove de novo (como no passo 6).
5. Em até 1 minuto, **deve aparecer**, nos dois (no app, recarregando): "2 versões" nessa frase, com
   "versão do app" como a que ficou (chegou primeiro) e "versão do PC" como a outra. Ao abrir:
   "Mudou em dois lugares. Ficou esta; a outra está guardada.", os botões **Ficar com esta** e **Ficar
   com a outra** e, embaixo, "A que você não escolher é descartada."
6. No PC, escolha **Ficar com a outra**. **Deve aparecer:** no PC, o aviso "Ficou a versão
   escolhida." e "versão do PC"; no app, em até 1 minuto, "versão do PC" e o selo sumindo.

## 11. A lixeira nos dois sentidos

1. No PC, mova um ditado para a lixeira. **Deve aparecer** no app, em **Lixeira**, em até 1 minuto.
2. No app, **Restaurar** esse ditado. **Deve aparecer** de volta no histórico do PC em até 1 minuto.

## 12. (Opcional) A nuvem fora do ar

1. Na janela 1, aperte **Ctrl+C**. **Deve aparecer na bandeja**, em até 1 minuto: "Nuvem: sem
   conexão". Dite uma frase: ela entra normalmente.
2. Suba a nuvem de novo (passo 1). Ela volta **vazia** e não conhece mais o PC: na próxima conferência
   o painel diz "Este PC foi desconectado na nuvem." É o esperado.
3. Se conectar de novo (passo 6), a nuvem nova tem outro "epoch" (é como uma nuvem restaurada de um
   backup): o PC manda de novo, sozinho, o que já tinha subido (sem a lixeira e sem pergunta). Se não
   quiser isso, não conecte de novo: vá ao passo 13.

## 13. Esquecer a nuvem e limpar

1. No PC, **Configurações** > **Nuvem**. Se o PC ainda estiver conectado, clique em **Desconectar**.
   **Deve aparecer:** "Desligada. Nada vai para a nuvem até você conectar.", o campo com o endereço
   e, embaixo, **Esquecer esta nuvem** e "Para ligar este PC a outra nuvem ou conta." Se você fez o
   passo 12 e não conectou de novo, a seção **já** está assim (com "Este PC foi desconectado na
   nuvem." em cima) e não há o botão Desconectar: vá direto ao 2.
2. Clique em **Esquecer esta nuvem**. **Deve aparecer**, na própria linha: "**Esquecer
   http://localhost:8080?** Este PC deixa de saber dessa nuvem e da conta voce@teste.local. Os ditados
   continuam aqui; na nuvem, nada muda.", com **Esquecer** (em vermelho) e **Cancelar** (o foco está
   nele; o Esc também cancela). Se você editou, pôs na lixeira ou escolheu um selo com o PC
   desconectado, num ditado que já estava na nuvem, aparece também "N mudanças feitas aqui ainda não
   subiram. Elas ficam neste PC." (a frase ditada com a nuvem fora do ar, no passo 12, não conta: é um
   ditado novo, e não uma mudança; ela fica no PC do mesmo jeito).
3. Clique em **Esquecer**. **Deve aparecer:** o aviso "Este PC esqueceu a nuvem.", o campo vazio e o
   cursor nele. Clique em **Histórico**: os seus ditados continuam todos lá.
4. Confira que o token de teste saiu:

```powershell
cmdkey /list:AuroraWhisper/nuvem-portatil
```

   **Deve aparecer:** nenhuma credencial (`NONE` ou `NENHUM`).
5. Na janela 1, **Ctrl+C** para desligar a nuvem de teste.
6. Feche o Agent do `bin\`:

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

7. Abra o Agent instalado pelo **Menu Iniciar** (Aurora Whisper).

**A cópia de antes do envio** fica em `C:\Desenvolvimento\aurora-whisper\bin\` (um arquivo por envio
feito). Ela tem todo o texto ditado até o clique, inclusive a lixeira, e o Agent nunca a apaga. Pode
guardar até a F10 (que decide quando essas cópias somem) ou apagar depois de conferir o app:

```powershell
Remove-Item C:/Desenvolvimento/aurora-whisper/bin/historico.db.antes-da-nuvem-*.db -Confirm
```

## O que me contar

- O N do passo 6, se viu "Enviando o histórico: X de N." no painel e "Nuvem: enviando X de N" na
  bandeja, e se apareceu "k ditados não subiram para a nuvem." (com o k).
- No passo 7.4: se as primeiras linhas do app eram as mesmas do PC, se a Lixeira do app estava vazia
  e se a frase da sua lixeira ficou de fora do app.
- Quanto tempo a frase ditada levou para aparecer no app (passo 8) e a edição do app no PC (passo 9).
- Se o selo apareceu nos dois lados no passo 10 e se sumiu depois da escolha no PC.
- O que o `cmdkey` mostrou no passo 13.
- Qualquer frase diferente das deste roteiro, com um print.

## O que este roteiro não testa

O Google de verdade, o celular e o servidor na VPS (F11); o aprendido (F8, que tem o roteiro
`docs/teste-manual-nuvem-f8.md`); a retenção de 6 meses (F9 e F10); e o Postgres (a nuvem de teste é
a de memória, que tem as mesmas regras).
