# ADR-025 — Ditar sem segurar: dois toques no Ctrl+Win

Status: **Aceita (07-nov-2026).** As decisões do Mauro estão em "As decisões do Mauro", como ele as
deu. O resto é desenho desta ADR, feito a partir da leitura do código em `643dc80` (`main` com o
dicionário no celular). **Tudo o que não sai das decisões dele está marcado [derivada]**, com a linha
"Para quem usa" quando muda o que ele vê.

Data: 2026-11-07
Contexto: hoje o ditado só grava enquanto a pessoa segura o gatilho (Ctrl+Win, ADR-013); soltar
para a gravação (AC-007), e o toque curto não faz nada (AC-001: na combinação de modificadores não há
caractere a devolver). Uma usuária sugeriu ao Mauro, em 07-nov: "Hoje eu tenho que segurar as teclas
para ditar. Ela disse que no whisprflow, se fizermos um duplo clique nas teclas, ele grava até você
agir de novo."

Emenda:
- **ADR-007** (a máquina do ditado): uma gravação que a soltura não para, e dois jeitos novos de
  parar (o toque e o clique) e um de descartar (o Esc).
- **ADR-013** (o gatilho por combinação): o "toque limpo" e o duplo toque.
- **Spec:** `03-acceptance-criteria.md` (os AC novos, abaixo), `04-system-design.md` (o hook do
  mouse), `07-flows.md` ("Tap curto vs ditado", "Estados da sessão" e "Timeouts"),
  `08-ui-ux-spec.md` (a pílula da gravação solta), `09-security-privacy.md` (o mouse e o Esc),
  `11-observability.md` (as linhas de log), `13-implementation-plan.md` e o `README.md` ("Como usar").

## As decisões do Mauro (07-nov-2026)

A proposta foi: dois toques rápidos no Ctrl+Win ligam a gravação sem segurar, um toque para;
segurar continua como hoje. Ele respondeu, ponto a ponto:

| O quê | Ele disse |
|---|---|
| Parar | "o parar pode ser também um clique do mouse. que tal? Assim já define o campo e para a gravação. Mas o toque na tecla de novo eu acho que vale. Vale ter os 2." |
| Limite de tempo | "Vamos manter os dois minutos." |
| Trocar de janela no meio | "Acho que minha proposta do mouse resolve o lance da janela." |
| Desistir | "Esc descarta. Concordo." |
| A pílula | "Concordo com a pilula" (a mesma pílula, com um detalhe mínimo que mostra a gravação solta, mostrado a ele antes de entrar). |
| Ditar para nota | "sim nota fica como está por hora." |

## Decisão

### A. Ligar: dois toques limpos

1. Dois toques no Ctrl+Win ligam a **gravação solta**. Cada toque é mais curto que o tempo de
   segurar (700 ms, o padrão do painel), e o segundo começa até **400 ms** depois de o primeiro
   acabar. [derivada] Os 400 ms são uma constante, ajustada depois do teste dele; o duplo clique do
   Windows usa 500 ms.
2. **Só o toque limpo conta:** o Ctrl e o Win, sem nenhuma outra tecla no meio. [derivada] O
   Ctrl+Win+seta (trocar de área de trabalho) e o Ctrl+Win+D, dados duas vezes seguidas, seriam um
   duplo toque sem querer. O Ctrl+Win+Alt (o gatilho da nota) também não conta: a nota fica como está.
   - Para quem usa: segurar o Ctrl e tocar duas vezes no Win também liga, porque o conjunto fica
     inteiro duas vezes.
3. **O segundo aperto já grava** e a pílula aparece nele, sem esperar os 700 ms. Se a pessoa soltar
   rápido, a gravação fica solta. Se segurar além do tempo de segurar, vira o ditado de sempre, e
   soltar para. [derivada]
4. **Num campo de senha, não liga**, como o ditado de sempre (AC-036): nenhum áudio é capturado.
5. **Só existe com a combinação de teclas** (o padrão). Com uma tecla de letra no gatilho, dois
   toques são a letra digitada duas vezes, e segurar a primeira para esperar a segunda traria de
   volta o defeito da ordem das teclas (`docs/defeito-ordem-da-tecla-de-ditado.md`). [derivada]

### B. Parar e descartar

1. **Um toque limpo no Ctrl+Win** para: transcreve e escreve. O toque com outra tecla no meio não
   para (o Ctrl+Win+seta troca de área de trabalho e a gravação segue). [derivada]
2. **Um clique** para, e o clique segue para o app como sempre: ele põe o cursor no campo, e o texto
   é escrito ali. [derivada] Só o botão esquerdo; o botão direito e a rodinha não param, porque abrem
   menu e rolam a página. O toque numa tela sensível ao toque deve chegar como clique esquerdo, mas
   não foi conferido (o PC do Mauro não tem tela de toque).
   - Para quem usa: o clique que para é o clique no campo. Clicar no botão de enviar envia antes de o
     texto chegar; o texto fica no histórico.
3. **Os 2 minutos** param e escrevem, como hoje (`DefaultMaxRecording`, que já não descarta a fala).
4. **O Esc descarta:** a captura para, nada vai ao ASR, nada é escrito e nada entra no histórico.
   Só durante a gravação solta o Agent segura o Esc (o aperto e a soltura, em par), para o app não
   recebê-lo; fora dela, o Esc passa como sempre. [derivada]

### C. Onde o texto vai

1. **No campo que está com o cursor na hora de escrever**, lido de novo depois da transcrição, e não
   no campo do começo. [derivada] Depois de um clique, o app já moveu o cursor; depois de um toque, é
   onde a pessoa está. O contexto do Smart (o programa, para o dicionário por domínio) é lido junto.
2. **Se esse campo for de senha, não escreve:** o texto fica no histórico, e a pílula diz isso.
   [derivada] É o AC-036 no fim da gravação, já que o campo pode ter mudado no meio. Na D2, a pílula
   ficou "Campo protegido: o texto ficou no histórico." (ponto cinza, como a do texto que demorou), e
   o histórico guarda a linha como inserção que falhou, sem dizer que era campo de senha (AC-027).
   **Para quem usa:** o texto não some; está no painel.
3. A escrita é a de sempre (ADR-011): se falhar, o texto fica na área de transferência e a pílula
   pede para colar.

### D. O mouse e o teclado, por dentro [derivada]

1. **O hook do mouse (`WH_MOUSE_LL`) existe só durante a gravação solta:** a thread do hook de
   teclado o instala ao ligar e o retira ao parar ou descartar. Fora disso, o Agent não vê o mouse.
2. Ele olha só o botão esquerdo apertado; **nunca suprime** nada e **nunca guarda** a posição, a
   janela ou o que foi clicado. O callback faz o mínimo, como o do teclado (ADR-010: sem I/O, sem
   lock, mandando por canal sem bloquear).
3. A instalação fica atrás do mesmo portão do hook de teclado (`sysinput.Guard`): teste nenhum
   instala hook.
4. O "toque limpo" é decidido no decisor puro (`dictation.Gatilhos`/`Decider`), que já vê toda tecla
   apertada com o conjunto inteiro. O Esc também: a thread do hook lê, sem lock, se a gravação solta
   está ligada.
5. A sessão solta é uma só, como hoje: a pausa da bandeja (`Decider.Armado`) a respeita, e o clique
   na bandeja para a gravação, como qualquer clique. Na D2, a pausa ganhou mais duas coisas em curso
   que ela deixa terminar: a gravação solta e o Esc engolido, cuja soltura tem de ser engolida também
   (`hook.Hook.deveIgnorar`).

### E. A pílula

A mesma pílula e a mesma onda, com um detalhe mínimo que diz "gravando sem segurar". As opções vão
ao Mauro como imagens antes de entrar (fase D3), e a escolhida entra no `08-ui-ux-spec.md`.

**Escolhida em 07-nov-2026: o cadeado** ("A", entre cadeado, ponto e contorno aceso, vistos em PNG
nos fundos claro, escuro e preto). Um cadeado de 7x8 px em cinza, na ponta esquerda, com a pílula
10 px mais larga só durante a gravação solta. Ao parar, o cadeado sai e a pílula volta à compacta
do "Processando": é o sinal de que a gravação já não está solta. [derivada] É o mesmo sinal do áudio
travado do WhatsApp.

### F. Privacidade

Nada novo sai do PC. O mouse só é visto durante a gravação solta, e o log registra só como ela
terminou (`toque`, `clique`, `limite`, `segurou` ou `descartada`), nunca a posição, a janela ou o texto
(`CLAUDE.md`, "Nunca gravar contexto de janela"). O Esc só é segurado durante a gravação solta.

## Critérios de aceite novos

| ID | Critério |
|---|---|
| AC-093 | Dois toques limpos no gatilho, o segundo até 400 ms depois do primeiro, ligam a gravação solta; segurar o segundo além do tempo de segurar é o ditado de sempre |
| AC-094 | O toque com outra tecla no meio (Ctrl+Win+seta, Ctrl+Win+Alt) não liga nem para a gravação solta; se ele for o segundo aperto, a gravação que o aperto começou é descartada sem ir ao ASR |
| AC-095 | Na gravação solta, um toque limpo ou um clique esquerdo param e escrevem; o clique segue para o app |
| AC-096 | Na gravação solta, o Esc descarta: nada vai ao ASR, nada é escrito, nada entra no histórico; fora dela o Esc passa |
| AC-097 | O texto vai ao campo com o cursor na hora de escrever; se for de senha, fica só no histórico |
| AC-098 | O hook do mouse só existe durante a gravação solta e nunca suprime nem registra o clique |
| AC-099 | Os 2 minutos param e escrevem a gravação solta, como no ditado de sempre |

## Fases

| Fase | O quê | Para |
|---|---|---|
| D1 | O núcleo puro: o toque limpo no decisor; na máquina, a janela do duplo toque, a gravação solta, parar pelo toque, pelo clique e pelo limite, o Esc que descarta. Testes de tabela em `go test`. | — |
| D2 | O runtime: o Esc na thread do hook, o hook do mouse só durante a gravação solta, o campo relido antes de escrever, o campo de senha no fim. Testes com dublês. | — |
| D3 | A pílula: as opções em imagem, ele escolhe, entra. | o Mauro escolhe |
| D4 | A spec, o README e o roteiro manual (`docs/teste-manual-ditar-sem-segurar.md`). | o Mauro remonta o Agent e roda o roteiro |

Cada fase é um commit local verde; o merge na `main` e o push dependem dele.

## Teste no PC (08-nov-2026)

O Mauro remontou o Agent da `main` (merge local de 07-nov-2026) e rodou parte do roteiro; no fim,
"ta tudo funcionando. Siga."

- **No log:** o ditado de sempre, segurando, escreveu; duas gravações soltas ligaram pelos dois
  toques e pararam pelo clique (`fim=clique`), as duas escritas, uma de 16 s com Alt+Tab no meio.
- **Relatado por ele, sem a gravação no log da sessão:** o Esc descarta e o campo de senha não recebe
  o texto.
- **Não rodado:** o clique que abre um menu (risco 5), segurar o Ctrl e tocar o Win duas vezes (risco
  1), os atalhos no meio, o segundo aperto segurado, o limite de 2 minutos e o ditado de 1 min 30 s
  (risco 4).
- **Achado e corrigido:** o primeiro dos dois toques registrava um WARN (`synthesize tap falhou`, "no
  character mapping for key 0"). O núcleo pedia ao hook que devolvesse a tecla do toque, e no Ctrl+Win
  não há tecla suprimida a devolver: o hook recusava antes de injetar qualquer coisa. Já acontecia
  desde outubro em todo toque curto no Ctrl+Win. Agora o núcleo só pede no gatilho por tecla
  (`dictation/devolver_toque_test.go`).
- **Fora desta ADR:** quatro ditados de sempre foram recusados como "nada reconhecido como fala" com
  pico de −22 a −25 dBFS: o detector de fala, por energia, pede −30,5 dBFS de RMS numa janela de 20
  ms (`audio/vad.go`). Com o Agent reiniciado e o microfone trocado no Windows, passaram. Baixar o
  limiar é uma decisão do Mauro, ainda não pedida.

## Testes

- **Puros, em `go test ./...`:** o decisor (toque limpo e sujo, segurar o Ctrl e tocar o Win, o Alt
  no meio, o Esc só na gravação solta e sempre em par); a máquina (a janela de 400 ms nas duas
  bordas, o segundo aperto segurado, o toque e o clique que param, o limite, o Esc, o campo de senha
  no começo e no fim, os eventos atrasados de uma tentativa encerrada).
- **Nunca em teste:** instalar o hook do mouse ou do teclado, injetar clique ou tecla
  (`CLAUDE.md`, "Testes que agem no sistema real"). A instalação real fica no roteiro manual, que o
  Mauro roda sabendo o que vai acontecer.

## Alternativas descartadas

- **Um toque liga, outro para:** o toque curto acontece sem querer (o Ctrl+Win+seta); o duplo toque
  é o que a usuária pediu e erra menos.
- **Um atalho separado** (outra combinação): mais uma coisa para lembrar; o pedido é o duplo toque.
- **Engolir o clique que para:** ele não poria o cursor no campo, que é o motivo do clique.
- **O hook do mouse sempre instalado:** o Agent veria todo clique da máquina sem precisar.

## Riscos

1. **O menu Iniciar.** Se um toque rápido no Ctrl+Win abrir o Iniciar, o duplo toque o abre e fecha.
   Conferido com o Mauro em 07-nov-2026, antes da D2: "quando dou toque rapido nada acontece". Fica
   no roteiro manual (D4) o jeito que ele não testou: segurar o Ctrl e tocar o Win duas vezes.
2. **O hook do mouse.** Todo clique da máquina passa pelo Agent durante a gravação solta; se o
   callback demorar, o Windows o derruba (`LowLevelHooksTimeout`) e o clique atrasa. Mitigação: só
   durante a gravação solta, e o callback só manda por canal.
3. **O duplo toque sem querer.** Mitigação: só o toque limpo conta, a pílula aparece no segundo
   aperto, e o Esc descarta.
4. **Ditado mais longo.** Sem segurar, os ditados de 1 a 2 minutos ficam comuns. O prazo da
   transcrição já cresce com o áudio (até 90 s), mas o áudio longo ainda não foi medido
   (`docs/defeito-ditado-longo.md`). O roteiro manual tem um ditado de 1 min 30 s.
5. **O clique que abre menu.** Achado ao escrever o roteiro da D4: o clique para e o texto é escrito
   onde o cursor ficou. Se o clique abriu um menu (o botão direito num app, o ícone da bandeja, que abre
   o menu do Aurora com o clique esquerdo), o texto é digitado dentro do menu, e no Windows uma letra
   pode acionar um item do menu. O texto nunca se perde (vai ao histórico), mas o menu pode fazer algo.
   **Decidido pelo Mauro em 07-nov-2026** ("segue a tua sugestao"): com um menu aberto no fim da
   gravação solta, o texto não é escrito e fica no histórico, como no campo de senha, e a pílula diz
   "Menu aberto: o texto ficou no histórico." (ponto cinza). "Menu aberto" é: a thread em primeiro plano está num menu do Win32 (`GetGUIThreadInfo`, só leitura) ou o controle em foco é de menu pela UI Automation (`Menu`, `MenuBar`, `MenuItem`).
   Os dois sinais, porque nenhum basta sozinho: num menu do Win32 aberto pelo clique nenhum item tem o
   foco ainda, e um menu que não é do Win32 não liga o modo de menu. Um menu que não faça nenhum dos
   dois não é reconhecido; o roteiro manual confere o da bandeja e o de contexto do Bloco de Notas.
