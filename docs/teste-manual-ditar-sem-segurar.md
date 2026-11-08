# Teste manual do "ditar sem segurar" (ADR-025)

Este roteiro é seu (do Mauro). Os testes automáticos cobrem o toque limpo, a janela dos dois toques, o
que para e o que descarta, o Esc, o campo relido e o cadeado da pílula, todos com dublês: eles nunca
instalam o gancho de teclado nem o do mouse, nunca apertam tecla, nunca clicam e nunca mexem na área de
transferência. Só aqui as teclas e o mouse de verdade são usados.

**Para quê:** confirmar, no PC, que o **Ctrl+Win de sempre não mudou**; que **dois toques rápidos** no
Ctrl+Win ligam a gravação sem segurar, com o cadeado na pílula; que ela **para** com um toque, com um
clique ou nos 2 minutos, e escreve onde o cursor está na hora; e que o **Esc descarta**.

**Nunca dite uma senha de verdade.** Num campo de senha, o esperado é o ditado **não** entrar.

**Não pare a gravação com um clique que abre menu** (o botão direito, o ícone da bandeja): o clique para,
e o texto seria escrito dentro do menu, onde uma letra pode acionar um item. Está em aberto (ADR-025,
risco 5), e por isso este roteiro não faz isso.

**Se algo não sair como está escrito:** pare naquele passo, anote o número do passo e o que apareceu (um
print ajuda), e siga para a próxima parte. No fim, me conte tudo, junto com a saída do passo 10.

## 0. Antes

1. A `main` precisa estar com o ditado sem segurar. Eu faço o merge local (sem push) quando você
   autorizar; confira que ele está lá:

   ```powershell
   git -C C:/Desenvolvimento/aurora-whisper log --oneline -1 --grep "ADR-025, D3"
   ```

   Tem de aparecer uma linha com `o cadeado na pílula da gravação solta`.
2. Feche o painel no navegador, se estiver aberto.
3. Num PowerShell, rode (fecha o Agent como o "Sair" da bandeja, recompila o Agent e o painel, abre o
   Agent novo e confere que é ele que está rodando):

   ```powershell
   powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\remontar-agent.ps1
   ```

   Tem de terminar com as linhas `OK:`. Nada muda no banco nem nas configurações.
4. Abra o Bloco de Notas e deixe o cursor dentro dele. Ele é o campo de quase todos os passos.

**O que "dois toques" quer dizer aqui:** aperte e solte o **Ctrl+Win** juntos, duas vezes seguidas,
rápido (como um clique duplo, menos de meio segundo entre um e outro). Nos dois toques, só o Ctrl e o
Win: nenhuma outra tecla no meio.

## 1. O Ctrl+Win de sempre (primeiro, antes de tudo)

1. Segure **Ctrl+Win**, diga "teste do ditado comum", solte. O texto entra no Bloco de Notas, com o bipe e
   a pílula de sempre, **sem cadeado**.
2. Um toque só no **Ctrl+Win**, rápido: nada acontece, nem pílula. O menu Iniciar **não** abre.
3. **Ctrl+Win+→** e depois **Ctrl+Win+←** (trocar de área de trabalho e voltar): funcionam como sempre, e
   nenhuma pílula aparece.
4. **Ctrl+Win+→** duas vezes seguidas, rápido, e **Ctrl+Win+←** duas vezes para voltar: troca de área de
   trabalho, e **nenhuma gravação** começa.
5. Um toque só no **Ctrl+Win** e, logo em seguida, **Ctrl+Win+→** (e depois **Ctrl+Win+←** para voltar):
   troca de área de trabalho. A pílula pode aparecer por um instante e sumir; **nada** é escrito, e nada
   entra no **Histórico** do painel.

Se algo daqui mudou, pare e me avise: é o que este trabalho não podia mexer.

## 2. Ligar e parar com um toque

1. Com o cursor no Bloco de Notas, dê **dois toques** no Ctrl+Win.
   **Deve acontecer:** no segundo toque, o bipe e a pílula com a onda e um **cadeado** na ponta esquerda.
   Solte as teclas: a pílula continua, gravando.
2. Sem tocar em nada, diga: "primeiro teste sem segurar, com as mãos livres".
3. Dê **um toque** no Ctrl+Win.
   **Deve acontecer:** o cadeado sai, a pílula volta ao tamanho de sempre ("Processando") e o texto entra
   no Bloco de Notas ("Inserido").
4. Agora segure só o **Ctrl** e toque o **Win** duas vezes (sem soltar o Ctrl). **Deve acontecer** o mesmo
   do passo 1, com o cadeado, e o menu Iniciar **não** abre. Diga uma frase e pare com um toque no
   Ctrl+Win.

## 3. Parar com um clique

1. Abra um segundo Bloco de Notas e deixe as duas janelas visíveis, lado a lado.
2. Com o cursor no **primeiro**, dê dois toques no Ctrl+Win e diga "este texto vai para a segunda janela".
3. **Clique** dentro do **segundo** Bloco de Notas.
   **Deve acontecer:** o clique para a gravação (o cadeado sai) e o texto entra no **segundo** Bloco de
   Notas, onde você clicou. No primeiro, nada.
4. Repita e, no meio da gravação, **role a roda do mouse** sobre o Bloco de Notas: **não** para (só o
   clique esquerdo para). Pare com um toque no Ctrl+Win.

## 4. O Esc descarta

1. No Bloco de Notas, dois toques no Ctrl+Win, diga "isto não pode aparecer em lugar nenhum", e aperte
   **Esc**.
   **Deve acontecer:** a pílula some, sem mensagem. Nada entra no Bloco de Notas.
2. No painel, aba **Histórico**: a frase **não** está lá.
3. Fora da gravação, o Esc é o de sempre: abra **Arquivo → Salvar como** no Bloco de Notas e aperte
   **Esc**. A janela fecha.
4. Dois toques no Ctrl+Win, diga uma frase, e aperte **Ctrl+Shift+Esc**: o **Gerenciador de Tarefas**
   abre, e a gravação **continua** (a pílula com cadeado segue lá). Com ele na frente, o toque no Ctrl+Win
   pode não chegar ao Aurora (o Windows não deixa um programa comum ver as teclas de uma janela de
   administrador). Por isso, pare com um **clique dentro do Bloco de Notas**: a gravação para e o texto
   entra ali. Anote se o clique não parou.

## 5. Atalhos no meio não param

1. No Bloco de Notas, dois toques no Ctrl+Win e comece a falar.
2. No meio da fala, **Ctrl+Win+→** (vai para a outra área de trabalho) e **Ctrl+Win+←** (volta).
   **Deve acontecer:** a pílula com cadeado continua; a gravação não parou.
3. Ainda gravando, aperte **Ctrl+Win+Alt** juntos e solte.
   **Deve acontecer:** também não para.
4. Clique dentro do Bloco de Notas para parar.
   **Deve acontecer:** a frase entra **no Bloco de Notas**, não numa nota (o Ctrl+Win+Alt no meio não
   manda para a nota).

## 6. Segurar o segundo aperto

1. Dê um toque no Ctrl+Win e, logo em seguida, **aperte e segure** o Ctrl+Win, falando "segurando o
   segundo".
   **Deve acontecer:** a pílula aparece já no aperto, com o cadeado (é esperado).
2. Solte depois de uns 2 segundos.
   **Deve acontecer:** a gravação para ao soltar e o texto entra, como no ditado de sempre.

## 7. Campo de senha

Use a senha clássica do Windows: `Win+R`, digite `rundll32.exe keymgr.dll,KRShowKeyMgr` e `Enter`; na
janela **Nomes de Usuário e Senhas Armazenados**, clique **Adicionar...**. **Não salve nada**: no fim,
**Cancelar**.

1. **Terminar no campo de senha:** com o cursor no Bloco de Notas, dois toques no Ctrl+Win, diga "não pode
   entrar na senha", e **clique no campo Senha** do diálogo.
   **Deve acontecer:** a pílula diz **"Campo protegido: o texto ficou no histórico."**, com o ponto cinza.
   O campo Senha continua **vazio**. No painel, aba **Histórico**, a frase está lá, marcada como erro.
2. **Começar no campo de senha:** com o cursor no campo **Senha**, dê dois toques no Ctrl+Win.
   **Deve acontecer:** nenhuma gravação começa (nem cadeado). Se aparecer "Ditado desativado em campo
   protegido.", também está certo.
3. **Cancelar** no diálogo, e feche a janela de nomes de usuário.

## 8. O limite de 2 minutos, e um ditado longo

1. **O limite:** no Bloco de Notas, dois toques no Ctrl+Win, diga uma frase curta e **não toque em mais
   nada**. Espere.
   **Deve acontecer:** com 2 minutos, a gravação para sozinha (o cadeado sai) e a frase entra no Bloco de
   Notas.
2. **Ditado longo (1 min 30 s):** dois toques no Ctrl+Win e fale sem parar por uns **90 segundos** (leia
   em voz alta um texto qualquer, um e-mail ou uma página). Pare com um toque.
   **Anote:** quantos segundos ficou em "Processando", e se o texto saiu **inteiro** (o começo, o meio e o
   fim). É o risco 4 da ADR: o áudio longo ainda não tinha sido medido.

## 9. Clicar fora de um campo

O clique que para escreve no lugar que ficou com o cursor. Este passo só observa o que acontece quando
esse lugar não é um campo de texto (e não é um menu: ver o aviso no começo).

1. Minimize as janelas até ver um pedaço vazio da área de trabalho, e abra o Bloco de Notas por cima,
   sem cobrir tudo.
2. No Bloco de Notas, dois toques no Ctrl+Win, diga "clique na área de trabalho", e clique num ponto
   **vazio da área de trabalho**.
   **Anote:** se o texto apareceu em algum lugar, e se está no **Histórico** do painel (deve estar).

## 10. O log

No PowerShell:

```powershell
Select-String -Path C:\Desenvolvimento\aurora-whisper\bin\agent.log -Encoding UTF8 -SimpleMatch "ligada=" | Select-Object -Last 20
```

**Deve aparecer:** uma linha `ligada=true` por gravação sem segurar, e uma `ligada=false` com o `fim=`
dela: `toque`, `clique`, `limite`, `segurou` ou `descartada`. **Nenhuma** linha leva a frase ditada, o nome
de um programa, uma janela ou a posição de um clique. Se aparecer uma linha `o hook do mouse falhou`,
anote: o clique não funcionou naquela vez.

Cole a saída para mim junto com as suas anotações.
