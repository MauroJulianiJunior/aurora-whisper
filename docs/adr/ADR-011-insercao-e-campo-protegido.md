# ADR-011 — Inserção de texto e detecção de campo protegido

Status: Accepted
Data: 2026-09-17
Contexto: Fase 2, Sprint 2 (TASK-004 + a parte de campo protegido da TASK-009)

## Contexto

O Sprint 1 entregou hook, áudio e overlay reais, validados em máquina de referência. Ficaram
duas pontas soltas, e elas se resolvem juntas porque dependem da **mesma máquina de UI
Automation**:

1. A inserção é `SendInput` caractere a caractere — lenta em texto longo e frágil em aplicativo
   que bloqueia entrada sintética.
2. `FocusTarget.Protected` nunca é preenchido, então `StateBlocked` (`AC-036`) é inalcançável
   em produção: **o ditado não é bloqueado em campo de senha**.

## Decisão 1 — UIA é para INSPEÇÃO, não para inserção

A spec diz "UI Automation primeiro, fallback clipboard" (`04-system-design.md`). A leitura
ingênua disso é usar `IUIAutomationValuePattern::SetValue` para escrever o texto. **Isso está
errado e seria destrutivo.**

`SetValue` substitui o **valor inteiro** do controle. Usá-lo para "inserir" apagaria tudo que já
estava escrito e descartaria a posição do cursor. Num campo com conteúdo prévio, ou num editor
rico, é perda de trabalho do usuário.

Portanto:

| Papel | Mecanismo |
|---|---|
| **Inspecionar** o alvo — o que tem foco, qual o tipo de controle, é senha? | UI Automation |
| **Inserir** o texto no cursor | `SendInput` (unicode) ou clipboard + colar |

`SetValue` fica reservado a um caso específico e verificável: campo comprovadamente vazio onde os
outros caminhos falharam. Nunca como primeira escolha.

## Decisão 2 — O foco é lido de um cache, nunca consultado dentro do hook

Uma consulta UIA custa de milissegundos a centenas de milissegundos. O callback do
`WH_KEYBOARD_LL` tem orçamento de ~300 ms; estourar faz o Windows **remover o hook em silêncio**
(`ADR-010`). Uma consulta UIA síncrona ali travaria a digitação da pessoa — a regressão proibida
explícita da spec.

O `ADR-007` já previu isso: `FocusTarget` chega **pronto, como dado**, no `KeyDownEvent`. Esta
ADR define de onde ele vem:

- uma thread dedicada instala `SetWinEventHook(EVENT_OBJECT_FOCUS)` — mais leve que registrar um
  handler de evento da própria UIA
- a cada troca de foco, essa thread faz a consulta UIA **fora do caminho do teclado** e atualiza
  um cache
- o `KeyDownEvent` lê o cache: leitura O(1), sem I/O, sem espera

Consequência aceita: o cache pode estar defasado por uma fração de segundo logo após uma troca de
foco muito rápida. Isso é preferível a travar o teclado — e o pre-roll do áudio já absorve
atraso dessa ordem.

> **Emendada pela ADR-015 (29-set-2026).** No padrão, o microfone só liga no keydown e não há
> pre-roll anterior à marca; a frase acima sobre o pre-roll absorver a defasagem só vale no modo
> opcional "sempre pronto". Em contrapartida, em campo protegido o microfone nem liga — garantia
> tão boa quanto este cache, com a defasagem descrita na nota abaixo.

### Nota de 29-set-2026 — a defasagem real pode chegar a um minuto

**Observado:** o `agent.log` de 29-set-2026 tem duas inspeções que levaram um minuto cada
(`duracao=1m0.03s` e `1m0.10s`, em passos diferentes do roteiro). A origem desse teto de ~60 s
não foi identificada.

**Consequência:** a thread dedicada é uma só, e o cache só muda quando uma inspeção **volta**.
Enquanto uma chamada UIA está presa, o cache mantém o valor **anterior** — e os eventos de foco
seguintes esperam na fila. A defasagem real do cache, portanto, não é "uma fração de segundo": ela
pode chegar à duração de uma chamada presa (60 s observados). Nos dois sentidos:

- um "livre" antigo continua valendo mesmo que o foco já esteja num campo de senha — o que
  contraria o espírito desta decisão e da decisão 5;
- um "protegido" antigo continua recusando o ditado num campo comum.

**O que a fase 1 da correção (29-set-2026) mudou, e o que não mudou:** passou a medir qual chamada
foi a mais lenta (`lenta_em`) e quanto tempo o cache ficou protegido (`protegido_por`); pôs uma
guarda por geração, que só age se o callback for reentrado (sem reentrância, a fila é processada
em ordem e a guarda não descarta nada); e passou a reinspecionar uma dúvida até três vezes
(100 ms, 300 ms, 1 s) com o cache protegido enquanto isso. **Nada disso encurta a defasagem
durante uma chamada presa**, e nenhuma dessas mudanças afrouxa a decisão 5.

**A reinspeção pode, ela mesma, alongar a defasagem.** Ela volta a perguntar à UIA, e a pergunta
pode travar como a original. Sem limite, uma chamada presa de 60 s seria reinspecionada 100 ms
depois, e o provedor que acabou de travar poderia prender a thread mais três vezes: cerca de três
minutos a mais com o rastreador cego por troca de foco, e uma troca para um campo sadio, feita
nesse meio-tempo, recusada até o fim. (Plausível, não verificado: não se sabe se o provedor que
travou uma vez trava de novo.) Para isso não acontecer, **uma inspeção que levou mais de
2 s não é reinspecionada** (`uia.TetoParaReinspecionar`): a dúvida fica como ficava antes da
reinspeção existir, protegida até a próxima troca de foco. O teto não impede que a própria
reinspeção trave (a original falhou rápido, a reinspeção prende), mas encerra a escada ali — no
máximo uma trava a mais por troca de foco. O valor de 2 s era **provisório** (revisto e mantido
na fase 2, abaixo, item 2): separa o timeout de
515 ms observado (que vale perguntar de novo) da chamada presa de 60 s, e nenhum valor afrouxa a
proteção, só muda quantas vezes se pergunta.

**A reinspeção lê o foco atual, não "o mesmo elemento".** Sem evento de troca de foco no meio, é em
geral o mesmo campo; uma troca para outro aplicativo dispara o próprio evento e cancela a
reinspeção. Mas o hook filtra o próprio processo (`WINEVENT_SKIPOWNPROCESS`), e a reinspeção não:
se, até 1,4 s depois de uma dúvida, a pessoa abre o menu da bandeja — que põe o foco na janela
oculta do Agent —, a reinspeção leria o próprio Agent e publicaria "livre", uma volta a livre que
não aconteceu no aplicativo. (Inferido do mecanismo, não verificado rodando: o clique no ícone
pode dar foco à barra de tarefas antes, e esse evento já cancelaria a reinspeção.) Por isso **a
reinspeção que lê um elemento do próprio Agent não publica e encerra a escada**
(`no_proprio_agent=true` no log): o cache fica com a dúvida, protegido, até a volta ao aplicativo
disparar o evento de foco dele.

**Decidido pelo Mauro em 29-set-2026, e feito na fase 2** (branch `fix/foco-protegido-fase2`) —
a lista que estava aqui como pendente:

1. **Vigia.** Uma inspeção que passa do orçamento (500 ms) sem terminar deixa o cache protegido
   até terminar; quando termina, publica o que leu pela regra de sempre. O vigia é um
   `time.AfterFunc` que troca o valor observado no início da inspeção por um protegido com
   `CompareAndSwap` **no próprio ponteiro do cache** — nunca checando um campo à parte e gravando
   depois, que abriria uma corrida (TOCTOU) em que o protegido do vigia ficaria preso por cima do
   resultado novo. Muda o contrato "o cache só é escrito pela thread dedicada": o vigia também
   escreve, mas só por CAS e só de livre para protegido. Com isso, **a defasagem de um "livre"
   velho durante uma chamada presa cai de até 60 s para o orçamento, 500 ms.** Um "protegido" velho
   continua podendo durar a chamada presa — agora limitada pelo teto do item 2.
2. **Teto de 3 s por chamada.** A thread cria o `CUIAutomation8` (interface `IUIAutomation2`) e
   pede `ConnectionTimeout` e `TransactionTimeout` de 3 s — um teto contra travamento, não o
   orçamento (os padrões do Windows são 2 s e 20 s). Se o `CUIAutomation8` não existir, cai no
   `CUIAutomation` antigo, sem teto, e o log da subida diz isso em WARN (`uia: automação`). GUIDs e
   índices de vtable conferidos contra o `uiautomationclient.h` do mingw-w64 (fonte citada no
   código). **A matriz de detecção de senha com o novo objeto tem de ser refeita à mão** — roteiro
   `docs/teste-manual-bandeja-e-painel.md`, B13.
   - **A regra "não reinspeciona o que passou de 2 s" foi revista e fica**, em 2 s, abaixo do teto
     de propósito: uma chamada que bate no teto (`UIA_E_TIMEOUT` perto de 3 s) indica provedor
     travado — a única evidência são travas de um minuto —, e reinspecioná-la prenderia a thread
     até mais 3 s por tentativa sem processar troca de foco. Sem reinspeção, a fila anda em ~3 s e
     trocar de janela e voltar inspeciona de novo. A exceção é a leitura completa do item 3.
3. **Leitura atrasada até 2 s.** Uma leitura acima do orçamento vale se (a) `GetFocusedElement`,
   `ControlType` e `IsPassword` responderam, (b) o sinal `ES_PASSWORD` rodou
   (`NativeWindowHandle` lido com sucesso; hwnd 0 aceito como "não se aplica") e (c) a leitura
   inteira levou até 2 s. Acima de 2 s: protegido como antes e uma reinspeção imediata (leitura
   nova, nunca a velha). **Só é seguro junto com o vigia**: a leitura atrasada descreve o foco de
   quando ela começou, e o evento de uma troca feita no meio espera na fila. O que sobra — e foi
   aceito — é um "livre" atrasado valendo entre a publicação dele e a inspeção do evento seguinte,
   no máximo um orçamento (500 ms), porque o vigia desta cobre o resto. Isto muda a decisão 2 (a
   "fração de segundo" passa a ser até 500 ms nesse caso) e a leitura da decisão 5 ("ambígua" não
   inclui mais a leitura completa de até 2 s).
4. **Janela de administrador.** Depois de uma inspeção sem elemento (falha ou nenhum elemento), a
   thread olha, só leitura, se o processo da janela em primeiro plano tem nível de integridade
   maior que o do Agent. Se tem — com prova; token que não abre é "desconhecido" —, a pílula diz
   "O Windows não deixa ditar em janela de administrador." (`08-ui-ux-spec.md`). O ditado continua
   bloqueado igual: o `SendInput` não entra em janela elevada de qualquer forma (UIPI). **O núcleo
   não mudou**: o `Machine` continua emitindo `OverlayBlocked` a partir de `Protected`; a marca vive
   no cache do rastreador ao lado do `FocusTarget` (não dentro dele), o runtime a lê no keydown e
   troca a fase mostrada por uma fase do overlay (`overlay.FaseJanelaElevada`), fora da faixa do
   núcleo.

   **Nota do B14 (teste manual do Mauro, 29-set-2026).** Com o Gerenciador de Tarefas (janela de
   administrador) em primeiro plano, segurar `Ctrl+Win` não fez **nada** — nem pílula, nem bipe.
   O gancho de teclado de baixo nível (`WH_KEYBOARD_LL`) de um processo de integridade média não
   recebe as teclas quando a janela da frente é elevada: é a mesma UIPI que barra o `SendInput`,
   agora na entrada. Consequências:
   - **é seguro**: o ditado nem começa, então nada é gravado, transcrito nem digitado — o Agent
     não chega a ver o `Ctrl+Win`, e não tem o que injetar nem o que soltar;
   - a mensagem "O Windows não deixa ditar em janela de administrador." **praticamente nunca
     aparece** — a marca de janela elevada continua sendo lida (`janela_elevada=sim` nos `WARN`
     do rastreador), mas raramente há um keydown para usá-la. O código da mensagem fica, para o
     caso de o atalho chegar ao Agent com a marca no cache; o B14 não viu esse caso;
   - o roteiro (`docs/teste-manual-bandeja-e-painel.md`, B14) passou a esperar "nada acontece", e
     `08-ui-ux-spec.md` registra que a mensagem quase nunca é vista.

   O que não se sabe: se há situação comum em que o gancho recebe a tecla com uma janela elevada na
   frente. Não foi investigado, e não muda nada de segurança — só de quão raro a mensagem é.
5. **Nome do executável nos avisos — exceção à regra de não registrar contexto de janela,
   autorizada pelo Mauro em 29-set-2026.** Só o nome-base do executável em foco (ex.:
   `brave.exe`), só nas linhas WARN de `uia: foco inspecionado` — nunca título, classe, caminho
   completo ou texto, nunca em Info/Debug. Vem do processo do elemento, quando a UIA o devolveu;
   senão, do processo da janela em primeiro plano. Registrada também em
   `09-security-privacy.md` e no `CLAUDE.md`; o teste por reflexão que proibia campo string no
   diagnóstico foi mudado de propósito para aceitar só esse campo.

Com isso, o limite desta consequência passa a ser: um "livre" velho não dura mais que um orçamento
(500 ms) seguido, mesmo com a chamada presa — ou a inspeção em curso termina, ou o vigia dela
protege o cache; com a leitura atrasada, pode haver dois desses intervalos numa mesma troca de
foco (o da inspeção em curso e o da publicação atrasada). Um "protegido" velho dura no máximo a
chamada presa, agora limitada a ~3 s por chamada pelo teto (sem o `CUIAutomation8`, volta a não
ter limite). (Deduzido do mecanismo e travado nos testes com dublês; não medido em máquina.)

## Decisão 3 — Inserção em camadas, com o clipboard como caminho principal para texto longo

1. `SendInput` com `KEYEVENTF_UNICODE` para texto curto
2. clipboard + `Ctrl+V` para texto longo ou quando o `SendInput` falha
3. se ambos falharem: **não perder o texto** — registra no histórico e informa que está
   disponível para cópia manual (`04-system-design.md`, "Inserção")

O limiar entre 1 e 2 é uma **pergunta em aberto**, a ser resolvida com medição, não com palpite.

> **Emendada pela ADR-016 (29-set-2026).** Texto com quebra de linha (o Polish produz parágrafos e
> listas) vai **sempre pelo clipboard, com `\r\n`**, qualquer que seja o tamanho: pelo
> `SendInput`, um `\n` curto pode virar Enter e enviar a mensagem num chat. A regra é uma decisão
> pura, testada com dublês; o roteiro manual confere Bloco de Notas, textarea do Chrome, WhatsApp
> Web e VS Code. O limiar entre 1 e 2 para texto de uma linha só segue em aberto.
>
> **Implementada na S5 (30-set-2026), em `internal/agent/inserter/strategy.go`.** "Quebra de linha"
> é `\n` **ou** `\r`. As quebras são normalizadas para `\r\n` **antes** de colar: `\n` solto e `\r`
> solto viram `\r\n`, e `\r\n` fica como está (nunca `\r\r\n`, nunca duas quebras). Só a quebra
> muda, e só o que vai ao clipboard: o `final_text` do histórico continua com `\n`. O caminho
> unicode **nem é tentado** — antes de tudo, e nunca como queda: se o clipboard falhar com texto de
> quebra, vale a regra de falha dele (`Result.Success=false`, `ClipboardRetained` diz se o texto
> ficou lá para colar) e **o texto não é digitado**, porque o Enter é justamente o risco. O log
> diz `multilinha=true|false` e a contagem de unidades UTF-16, nunca o texto. O teste é de tabela
> contra os dublês (`strategy_quebra_test.go`), sem `SendInput` e sem clipboard reais.

## Decisão 4 — O clipboard é preservado e restaurado, e a corrida é reconhecida

`AC-009` exige restaurar o clipboard. A sequência é snapshot → set → paste → restore, e ela tem
uma corrida real: **restaurar cedo demais faz o aplicativo colar o conteúdo antigo**, porque ele
ainda não leu o clipboard quando a restauração acontece.

Isto não é detalhe de implementação — é a diferença entre colar o ditado e colar o que a pessoa
tinha copiado antes. A estratégia de espera precisa ser **medida em aplicativos reais** e
documentada, não chutada.

Hoje o teste do `AC-009` não tem dentes: a sequência inteira vive dentro do `FakeInserter`, então
ele só prova que o fake faz o que o fake foi escrito para fazer (registrado no F1-B). Com um
inserter real atrás da mesma interface, esse teste passa a valer.

## Decisão 5 — Campo protegido, com dois sinais

- **UIA `IsPassword`** no elemento com foco — funciona em controles Win32 e, geralmente, em
  navegadores modernos
- **fallback por estilo de janela**: `ES_PASSWORD` em controles Edit clássicos, para o caso de a
  UIA não responder

Quando qualquer um dos dois indicar campo protegido, `FocusTarget.Protected` é verdadeiro e o
núcleo faz o resto — a lógica de bloqueio já existe, testada desde o F1-A, incluindo a preservação
do tap para que a pessoa **continue conseguindo digitar a letra da tecla de ditado** dentro do
campo de senha.

**Na dúvida, proteger.** Se a inspeção falhar ou for ambígua, tratar como protegido é o erro
seguro: o custo é um ditado não realizado; o custo do erro oposto é voz capturada num campo de
senha.

## Decisão 6 — Injeção de entrada tem portão que recusa por padrão

Acrescentada em 17-set-2026, depois de um incidente: durante a construção deste sprint, um agente
rodou um teste que chamou `SendInput` de verdade, digitou texto no Bloco de Notas do usuário e
deixou um modificador preso, travando o teclado dele.

A causa não foi desatenção do agente — foi ausência de trava. Código que injeta entrada é
perigoso **por natureza** numa máquina com desktop em uso, e a proteção não pode depender de
disciplina de quem escreve o teste.

Portanto:

- toda função que injeta entrada (`SendInput`) ou escreve no clipboard fica atrás de um portão
  que **recusa por padrão**, devolvendo erro
- o portão é liberado **apenas** pelo binário do Agent em execução real — nunca por teste, nunca
  por variável de ambiente que um teste possa definir sem querer
- a lógica de decisão (escolha de caminho, limiar, retry, tratamento de falha) fica fora do
  portão, é pura, e é testada contra dublês
- se algum dia um teste precisar da chamada real, ele usa build tag própria (`manualinput`) para
  que `go test ./...` nunca o execute

A regra completa está em `CLAUDE.md`, seção "Testes que agem no sistema real".

## Limitação conhecida da decisão 3 — a promessa ainda não é cumprível por inteiro

A decisão 3 diz que, se os dois caminhos falharem, o texto "não se perde" e fica disponível para
cópia manual. Isso é verdade no caminho comum: quando o `SendInput` falha e o clipboard é escrito
com sucesso, o texto está lá e um `Ctrl+V` recupera.

**Mas existe um caminho onde o texto se perde mesmo:** quando a própria escrita no clipboard
falha — por exemplo, outro processo segurando o clipboard além do limite de tentativas. Aí não
há onde guardar. O único rastro é um log estruturado com a contagem de caracteres, nunca o
conteúdo (por privacidade), e não há persistência.

A razão é estrutural, não negligência: **o histórico ainda não persiste**. O SQLite real chega na
`TASK-005`. Enquanto não houver onde gravar, "manter no histórico" não é uma opção disponível.

Registrado como dívida, com pré-requisito explícito: quando a `TASK-005` entregar a persistência,
o resultado da inserção precisa propagar se o clipboard reteve o texto, e o desfecho falho precisa
gravar o texto no histórico local antes de desistir. Só então a promessa desta decisão passa a ser
verdadeira em todos os caminhos.

Encontrado pela auditoria do Sprint 2, não por teste — a suíte não cobre a falha dupla seguida de
falha de clipboard.

## Perguntas em aberto — resolver com evidência, não com opinião

O implementador precisa **medir e reportar**, não decidir por intuição:

1. Qual o limiar de tamanho a partir do qual o clipboard vence o `SendInput`?
2. Quanto tempo esperar antes de restaurar o clipboard, e existe mecanismo melhor que esperar
   (por exemplo, observar o clipboard ser lido)?
3. Quais aplicativos da matriz exigem qual caminho? A matriz mínima da spec
   (`13-implementation-plan.md`, TASK-004) é: Bloco de Notas, textarea do Chrome, WhatsApp Web,
   VS Code e inputs web comuns.

As respostas entram no relatório da fatia e, se mudarem o desenho, nesta ADR.

## Consequência que governa tudo

O núcleo (`Machine`, `Decider`) **continua sem mudar uma linha**, como no Sprint 1. O que muda é
o runtime e os colaboradores. Se a implementação pedir para alterar o núcleo ou reescrever um
teste anterior, isso é sinal de violação do desenho — parar e reavaliar, não adaptar o teste.
