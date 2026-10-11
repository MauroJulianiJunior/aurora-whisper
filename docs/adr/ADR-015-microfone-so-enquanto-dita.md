# ADR-015 — O microfone só fica ligado enquanto a pessoa dita

Status: Accepted (Mauro, 29-set-2026)
Data: 2026-09-29
Revisão adversarial de 29-set-2026: a recuperação do microfone numa leitura que falha (M1) e o ring
que cabe o pior caso (M2), em "Como funciona"; o alerta de contaminação por ditado (B1), em
"Medição".
Emenda: ADR-007 §4, ADR-010 "Ring buffer de pre-roll", ADR-011 decisão 2 (a frase sobre o
pre-roll). Redefine o AC-003 por modo.

## O problema, encontrado usando o produto

Com o Agent parado na bandeja, o Windows mostra o ícone de microfone na barra de tarefas o tempo
todo. Em 29-set-2026, o Mauro viu esse ícone, com a dica "Microfone em uso por:
aurora-whisper-agent". É o que foi observado: o que Configurações > Privacidade > Microfone lista
para o Agent não foi visto. O Wispr Flow, no mesmo computador, não faz isso.

Não é impressão. Desde o Sprint 1 o Agent dava `IAudioClient::Start` na subida e só parava no
encerramento, para guardar 300 ms de áudio de antes do aperto — o pre-roll da ADR-007 §4. O custo
não era só o ícone:

- em repouso, a thread de áudio consultava o microfone a cada 10 ms, e cada pacote acordava o
  laço do runtime só para ser jogado fora;
- o ring gravava áudio em RAM até enquanto a pessoa digitava uma senha — o AC-036 valia no
  núcleo (o áudio nunca era usado), não no dispositivo;
- um fone Bluetooth usado como microfone provavelmente ficaria preso no perfil de chamada (HFP)
  enquanto o Agent rodasse (documentação da Microsoft; não verificado aqui).

O REQ-005 pede áudio "desde keydown". Áudio **anterior** ao keydown foi escolha da ADR-007, não
requisito.

## Decisão

1. **O padrão passa a ser "só enquanto dita".** Na subida o Agent abre e prepara o microfone
   (`Initialize`), sem `Start`. O `Start` acontece no keydown, quando o núcleo emite
   `MarkAudioStartEffect` — que ele não emite em campo protegido. O microfone estaciona quando
   não há ditado marcado nem parada em curso, e **sempre depois do corte da cauda**.
2. **A `audio.Session` é dona do liga-desliga.** A regra é uma só: o microfone roda enquanto
   valer ao menos uma destas — modo "sempre pronto"; tentativa marcada; tentativa parando. O
   núcleo (`Machine` e os `Effect` de `internal/agent/dictation`) não muda; a interface
   `capturador` de `internal/agent/winruntime` ganhou o método `MicrofoneEntregando`, de que o
   runtime precisa para a decisão 5.
3. **Pre-roll:** 0 no padrão, por construção (a marca cai quando o microfone liga); 300 ms no
   "sempre pronto". Nos dois modos, o corte **nunca começa antes do início da janela** do
   microfone: antes dela, o ring guarda o fim do ditado anterior — o vazamento de 28-set-2026
   voltaria por outra porta, e em todo ditado.
4. **Estacionar é `Stop` seguido de `Reset`, mantendo o cliente** (política "parar"). O `Reset`
   é obrigatório: `Stop` só congela o relógio do stream, e o `Start` seguinte retoma de onde
   parou, entregando os pacotes que sobraram do ditado anterior
   ([IAudioClient::Stop](https://learn.microsoft.com/en-us/windows/win32/api/audioclient/nf-audioclient-iaudioclient-stop),
   [IAudioClient::Reset](https://learn.microsoft.com/en-us/windows/win32/api/audioclient/nf-audioclient-iaudioclient-reset)).
   Se o `Reset` falhar, o cliente é liberado naquela janela e reaberto no próximo ditado
   (política "liberar"). A escolha é uma função pura, testada.
5. **O bipe e a pílula "Gravando" só saem quando o microfone já entregou o primeiro pacote E o
   limiar de 700 ms passou.** Cabe no runtime, sem mudar o núcleo: o runtime segura o
   `ShowOverlayEffect{Recording}` e o `PlayBeepEffect` até a Session dizer que o microfone entrega
   áudio para a tentativa. Se a tecla subir antes, os dois não saem.
6. **O `Stop` espera, com teto de 1 s, até ter áudio com carimbo no instante do keyup ou
   depois.** Sem nenhum pacote, ou com a captura começando só depois de a tecla subir, devolve um
   erro próprio — "o microfone não abriu a tempo" (`audio.ErrMicrofoneNaoAbriu`) —, nunca um
   buffer vazio que o núcleo leria como "nada reconhecido como fala".
7. **"Microfone sempre pronto"** é opção, local, desligada por padrão, e mantém o comportamento
   anterior. O núcleo é `audio.Config.SemprePronto` e `Session.DefinirSemprePronto`, seguro de
   qualquer goroutine. A opção está na tela de Configurações, na seção Ditado ("Microfone sempre
   pronto"), e vale **na hora**: `aplicarPreferencias`, em `cmd/agent/main.go`, chama
   `DefinirSemprePronto` na subida (com o que estava gravado) e a cada **Salvar** do painel, sem
   reinício pendente. Ligar acende o ícone do microfone em poucos segundos; desligar o apaga. Um
   ditado em curso termina no modo do keydown dele, e desligar no meio só estaciona o microfone
   depois do corte. Cada mudança de modo deixa no log `audio: modo do microfone mudou`, com o
   `modo` novo — só com a sessão de áudio viva; com ela morta (`Session.Falha`), o modo fica
   gravado e o log diz, em `WARN`, `audio: o modo do microfone foi gravado, mas o microfone não
   responde`. É a exceção ao "tudo ou nada" das preferências do runtime (`aplicarAjuste`): se o
   núcleo recusar o limiar no keydown seguinte, o microfone já mudou. Aceito porque a recusa só
   existe como defeito — a API local valida o limiar e o modo de texto antes de gravar
   (`localapi.validar`).
8. **O gatilho continua `Ctrl+Win`** (decisão do Mauro). `Ctrl+Win+setas` e `Ctrl+Win+D` viram
   tentativas com `MarkStart` seguido de `Abort` (ADR-013): o ícone pisca por uma fração de
   segundo. Aceito.
9. **Toda tentativa que chega ao `Stop` deixa no log uma linha de números** (`audio: janela do
   microfone`), medidos com o QPC que o próprio `GetBuffer` carimba nos pacotes, e os alertas
   abaixo. Nunca amostra, texto ou contexto.

## Como funciona

**Subida.** `openDevice` faz `CoInitializeEx`, `GetDefaultAudioEndpoint`, `Activate`,
`GetMixFormat`, `Initialize` compartilhado e `GetService` — e para aí. A goroutine de áudio fica
**bloqueada** esperando um aviso: nenhuma consulta ao dispositivo em repouso. `cmd/agent` pede o
modo explicitamente (`configDeAudio`, com teste) e loga `audio: microfone preparado modo=sob_demanda
politica=parar`. Se a pessoa deixou o "sempre pronto" marcado nas Configurações, o Agent sobe
sob demanda e `carregarPreferencias` muda o modo logo depois (`audio: modo do microfone mudou`,
`preferencias: carregadas` com `microfone_sempre_pronto=true`): o ícone acende poucos segundos
depois da subida. Se a leitura das preferências falhar (o banco ocupado), `carregarPreferencias`
tenta de novo **uma** vez, 200 ms depois (`preferencias: não consegui ler; tento de novo`), antes de
seguir com as padrão — que deixariam o microfone sob demanda com o banco dizendo "sempre pronto".

**Keydown.** `MarkStart` grava a marca (cursor do ring e QPC), registra a tentativa e acorda a
goroutine, sem esperar o dispositivo — ela roda no laço que consome o hook. A goroutine cria um
`Resampler` novo, fixa o início da janela e dá `Start`. **Só a partir de estacionado**: acordar
com o stream rodando (um toque seguido de keydown, por exemplo) não chama `Start` de novo. Um
`AUDCLNT_E_NOT_STOPPED` (`0x88890005`) não conta como falha. Um `Start` que falha — o cliente
preparado ficou inválido com o Agent parado — solta o cliente e tenta **uma** vez de novo,
reabrindo o microfone padrão. Se isso também falhar, falha **só aquele ditado** (`ErrDeviceLost`
no keyup, com `WARN`), e o keydown seguinte tenta de novo — nos dois modos. No sempre pronto, a
religação que falhou pode ser também a do recuo da recuperação (abaixo), e aí ela conta para o
incidente.

A decisão de estacionar e o estado "desligado" mudam **no mesmo lock**: uma marca que chega antes
da decisão impede o estacionamento, e uma que chega depois se sabe numa janela nova (e não "já
ligado", com o alerta de microfone lento suprimido).

**Leitura.** `Read` volta a cada consulta vazia, depois de no máximo um intervalo de 10 ms. É o
que deixa um toque curto estacionar o microfone mesmo quando ele ainda não entregou nada — um
`Start` frio, um driver lento.

**Toque, `Ctrl+Win+<tecla>`, watchdog, falha → `Abort`.** Tira a tentativa marcada. Uma que já
esteja parando não é tocada: quem a tira é o `Stop` dela, depois do corte.

**Keyup ou 120 s → `Stop`.** A tentativa passa a "parando" e o microfone **segue ligado**. O
`Stop` espera o áudio do instante do keyup, depois a última sílaba calar (até 200 ms), fixa o corte
em `max(marca − pre-roll, início da janela)` até o cursor, tira a tentativa e acorda a goroutine.
Só então ela estaciona. Duas paradas sobrepostas (o watchdog de 5 s libera um keydown novo com o
`Stop` anterior em curso) só desligam o microfone depois da última.

**O ring cabe o pior caso** (revisão de 29-set-2026, M2). Até ali ele guardava só `PreRoll +
MaxRecording`, e não cabia: a marca é no **keydown**, o `TimerMaxRecording` do núcleo só é armado
quando o limiar vence, e o `Stop` do limite ainda espera o microfone e a cauda com o microfone
ligado. Numa fala de 120 s o trecho marca→corte passava do ring, e o `Range` subia o início do corte
**descartando o começo do ditado sem sinal nenhum**. Agora `audio.Config.capacitySamples` soma
`PreRoll + MaxHoldThreshold + MaxRecording + DefaultEsperaPeloMicrofone + MargemDeCauda` — 0,3 +
2 + 120 + 1 + 0,2 = **123,5 s**, cerca de 3,95 MB de `int16` a 16 kHz (3,2 s a mais que antes).
`MaxHoldThreshold` é o **maior** limiar que o núcleo aceita (`dictation.MaxHoldThreshold`, 2 s, o
mesmo que `localapi.validar` confere), não o configurado, que a tela muda com o Agent rodando;
`configDeAudio` o passa, e `TestConfig_RingCabeOPiorCaso` prende a conta. Se ainda assim o corte
pedido for mais antigo que o que o ring guarda, a janela diz quanto se perdeu (`perdeu_inicio_ms`,
com `WARN`).

**Recuperação do microfone** (revisão de 29-set-2026, M1). Com o sempre pronto, o microfone roda
em repouso, e um cliente invalidado — suspender e retomar o notebook, tirar o microfone USB — chega
pela **leitura**, não pelo `Start`. Até ali isso matava a sessão em silêncio: nenhum ditado
funcionava, o bipe nunca saía (o "gravando" retido só se solta com áudio), desligar a opção não
resolvia, e só reiniciar o Agent trazia o microfone de volta. Agora uma leitura que falha com o
microfone ligado:

- faz a tentativa em curso, marcada ou parando, **falhar** com `ErrDeviceLost` no `Stop` dela. O
  áudio de depois da reabertura nunca é dela, e o pre-roll não atravessa a falha: a janela seguinte
  é nova. Ela também deixa de segurar o microfone ligado;
- **solta o cliente**, como a política "liberar", e conta como um estacionamento (`fechamentos`);
- deixa a sessão voltar a ligar por `ligarOuReabrir`: no **sempre pronto**, sozinha; no **sob
  demanda**, no próximo keydown — o mesmo caminho de um cliente invalidado com o microfone
  estacionado.

A linha `audio: janela do microfone` do ditado perdido mede a janela **em que ele falhou**, guardada
na tentativa no momento da falha — e não a que a falha zerou nem a que a reabertura abriu depois.
No runtime, a onda da pílula "Gravando" para: ela só anda com o microfone entregando para o ditado
em curso, e no sempre pronto os frames da janela reaberta não são desse ditado.

Um custo, aceito: se a leitura falhar durante a espera da última sílaba (até 200 ms depois do
keyup), o ditado se perde **inteiro**, embora o áudio até a falha já cobrisse o keyup. O corte é
fixado no mesmo lock que confere a falha, e salvar esse caso pediria guardar na tentativa o cursor
e a janela da falha e cortar ali. A janela é estreita (a falha teria de cair nesses 200 ms), e
ficou para depois.

**Vigia de parada.** Se o Windows pausar o stream sem invalidar o cliente — não se sabe se é o que
acontece ao suspender (Riscos, abaixo) —, nenhuma leitura falha: ela só volta vazia, para sempre.
Sem vigia, o sempre pronto ficaria "ligado" e mudo, o bipe sairia sem microfone e o pre-roll do
ditado seguinte traria os últimos 300 ms de antes da parada. Por isso, numa consulta vazia, **500
ms sem pacote** (`VigiaDeParada`, 50 períodos do motor compartilhado), contados da chegada do último
e só depois do primeiro pacote da janela, contam como uma leitura que falhou, com tudo acima. Antes
do primeiro pacote vale o vigia do primeiro pacote, com mais folga (abaixo); e o vigia olha a
chegada, não o carimbo, para que um carimbo atrasado não vire falha.

A segunda revisão de 29-set-2026 restringiu o vigia, com prova em cópia:
- **Só no repouso do sempre pronto.** Com um ditado vivo, quem decide é o `Stop`, que já tem teto e
  `faltou_no_fim_ms`. Um soluço de 600 ms no meio de uma fala de 60 s corta aquele pedaço; derrubar
  o ditado inteiro por ele trocaria um parcial por nada. No sob demanda, o microfone ligado sem
  ditado é só o instante entre o `Stop` e o estacionamento, e ali o vigia soltaria o cliente pelo
  caminho errado, com um aviso falso de parada.
- **O instante da consulta é lido antes do `Read`.** O `Read` vazio dorme um período antes de
  voltar; lido depois, uma goroutine que acorda atrasada (CPU saturada, paginação) fazia o
  microfone parecer parado com os pacotes já esperando no buffer. No sempre pronto, oito atrasos
  assim matavam a sessão com o microfone perfeito.
- **A parada sem erro só recua, e não gasta a cota da desistência.** Um microfone que chegou a
  entregar e volta a entregar a cada reabertura "funciona com soluços"; desistir dele o deixaria
  mudo até reiniciar o Agent. As paradas escalam o recuo, mas a desistência conta só as falhas com
  erro do incidente: oito paradas seguidas de uma leitura com erro (o USB saiu e voltou) não matam
  a sessão.
- **No sob demanda, a recuperação é soltar no estacionamento.** Um ditado cuja janela não entregou
  pacote nenhum (`ErrMicrofoneNaoAbriu`), ou parou por 500 ms ou mais antes do keyup
  (`faltou_no_fim_ms`), faz o estacionamento seguinte usar a política "liberar" uma vez, e o keydown
  seguinte reabre o microfone padrão. Sem isso, um cliente parado que volta da suspensão ligando
  sem entregar seria reaproveitado para sempre pela política "parar". Um microfone só lento — que
  entregou, mas depois de a tecla subir — não entra: soltá-lo cobraria a reabertura de cada ditado
  curto. E a marca é apagada quando uma leitura que falha já soltou o cliente, para não soltar à
  toa, horas depois, um cliente saudável.

**Vigia do primeiro pacote** (terceira rodada da revisão de 29-set-2026). A segunda rodada deixou
uma janela sem saída no sempre pronto: o cliente parado que volta da suspensão ligando sem erro e
sem entregar nada. O vigia de parada exige o primeiro pacote, e o sempre pronto nunca estaciona
para soltar o cliente pela marca do `Stop`, que é como o sob demanda se recupera — todo ditado
falhava com `ErrMicrofoneNaoAbriu` até desmarcar e marcar de novo a opção. Agora, numa consulta
vazia, a janela ligada há **2 s** (`VigiaDoPrimeiroPacote`), contados do instante em que o `Start`
voltou, sem o primeiro pacote, conta como uma leitura que falhou, com uma causa própria. Vale nas
mesmas condições do vigia de parada — só no repouso do sempre pronto: com o ditado vivo, quem
responde é o `Stop`, e o vigia age logo depois dele — e, como a parada, **só recua**, sem gastar a
cota da desistência. Os 2 s são julgamento: bem acima do primeiro pacote de um `Start` frio
(algumas dezenas de ms, pela estimativa de "Consequências") e o dobro do teto do `Stop`; soltar um microfone só
lento cobraria a reabertura inteira.

Os dois vigias decidem numa consulta vazia e agem logo depois, noutro lock; entre uma coisa e outra
pode cair um keydown, que tira o microfone do repouso. A ação confere o repouso de novo, no lock em
que age, e a linha do vigia só sai depois de agir (quarta rodada da revisão, 30-set-2026). Antes,
esse keydown perdia o ditado com a causa do vigia, e o log dizia que o microfone tinha sido solto.

No sempre pronto, a religação tem **recuo**, para não virar laço quente: a primeira religação de um
incidente é imediata; depois, 1, 2, 5, 10 e 30 s, e 30 s daí em diante. Contam como falha do
incidente a leitura que falhou e a religação do recuo que não vingou (o `Start` e a reabertura
falhando), e duas falhas são do mesmo incidente se o microfone não ficou **10 s** (`JanelaSaudavel`)
sem falhar entre elas, contando do fim do recuo — a espera não é saúde. É o que segura um microfone
que religa e falha logo na primeira leitura. **Desiste na oitava falha seguida**, depois de ~78 s de
recuos (0 + 1 + 2 + 5 + 10 + 30 + 30 s): um microfone que não volta nesse tempo não é suspensão, é
hardware que sumiu, e tentar para sempre só encheria o log. Aí a sessão morre (`Session.Falha()`
diferente de `nil`), todo `Stop` devolve `ErrDeviceLost` e o microfone só volta reiniciando o Agent.
O recuo espera num temporizador injetável (o teste o controla) e acorda com qualquer aviso: `Close`
sai na hora, e `DefinirSemprePronto(false)` tira a goroutine do recuo — no sob demanda, o keydown
seguinte liga sem esperar.

**Um keydown no sempre pronto corta o recuo** (terceira rodada da revisão de 29-set-2026). Até ali
ele não o encurtava: com o recuo em 30 s, todo ditado no meio dele falhava com "nenhum pacote em 1
s". Agora o `MarkStart` que encontra o microfone fora com o recuo ainda correndo guarda o fim do
recuo e o zera, e a goroutine tenta ligar na hora, como no sob demanda. A janela que o keydown abre
fica **em teste até o primeiro pacote**. Se entregar, o ditado sai, o bipe espera o microfone como
sempre (decisão 5), e o recuo cortado acaba ali: é do primeiro pacote, e não do fim que o recuo
teria, que os 10 s de `JanelaSaudavel` contam. Se não ligar — ou ligar e cair antes do primeiro
pacote, na primeira leitura ou no vigia do primeiro pacote —, é como no sob demanda: falha **só
aquele ditado** (`ErrDeviceLost` no keyup, na hora), a tentativa **não conta** para o incidente —
nem para o recuo, nem para a desistência —, e o recuo cortado volta a valer **de onde estava**.
Keydowns num apagão, ou com um microfone que religa e cai **antes do primeiro pacote**, não matam a
sessão mais cedo, nem adiam a religação seguinte do recuo. **Risco aceito** (quinta revisão,
30-set-2026): um microfone que religa, entrega ao menos um pacote e cai com erro a cada reabertura
(USB com mau contato) faz cada keydown no recuo contar uma falha real, e a sessão morre com `ERROR`
em poucos keydowns, e não nos ~78 s do recuo. O desfecho é o mesmo, só mais cedo, num caso estreito
e só do sempre pronto; a correção completa (a janela do keydown em teste até o recuo cortado vencer
ou até `JanelaSaudavel` depois do primeiro pacote) tem protótipo, e fica para quando o caso
aparecer no uso; e cada keydown custa dois `Start` (o do keydown e o da
reabertura), como no sob demanda. Se a janela acabar sem veredito — o sempre pronto desligado no
meio dela, e o microfone estacionado —, o corte some com ela. Um keydown que chega com a religação
do recuo já em curso, ou com o recuo já vencido, não tem recuo a cortar: o ditado vai nessa
religação, que conta como sempre — e, se ela não vingar, o ditado falha na hora com o erro do
dispositivo, em vez de esperar o teto do `Stop` e sair como "o microfone não abriu a tempo".

A terceira rodada tirava a janela do teste já no `Start` que vingava, e a quarta rodada da revisão
(30-set-2026, com prova em cópia) achou os dois furos disso: com um microfone que religa e cai na
primeira leitura, cada keydown no recuo contava uma falha real e escalava o recuo — seis keydowns
seguidos matavam a sessão com `ERROR`, sem nenhum recuo vencer —; e o incidente seguia contando do
fim do recuo cortado, de modo que 15 s saudáveis depois de um keydown que cortou um recuo de 30 s
ainda eram o mesmo incidente, com o recuo no teto.

Linhas de log da recuperação, só com números, booleanos e o modo — nunca áudio:

| Linha | Nível | Campos |
|---|---|---|
| `audio: o microfone parou de entregar áudio sem erro; tratado como leitura que falhou` | `WARN` | `modo`, `sem_pacote_ms` — o vigia de parada; vem logo antes da linha da leitura que falhou |
| `audio: o microfone ligou e não entregou áudio nenhum; tratado como leitura que falhou` | `WARN` | `modo`, `ligado_ha_ms` — o vigia do primeiro pacote; vem logo antes da linha da leitura que falhou |
| `audio: a leitura do microfone falhou; o microfone foi solto` | `WARN` | `modo`, `tentativas_perdidas`, `falhas_de_leitura` (desde a subida), `reabre_sozinho`, `falhas_seguidas`, `recuo_ms`; na janela de um keydown que cortou o recuo e caiu antes do primeiro pacote, que não conta, `recuo_restante_ms` no lugar dos dois últimos, como na linha do keydown que não ligou |
| `audio: o microfone não ligou; o próximo ditado tenta de novo` | `WARN` | `modo`, `tentativas_perdidas`; no `sempre_pronto` (o keydown que cortou o recuo), também `recuo_restante_ms`, quanto falta para a sessão tentar de novo sozinha |
| `audio: o microfone não religou; tento de novo depois do recuo` | `WARN` | `modo`, `tentativas_perdidas`, `falhas_seguidas`, `recuo_ms` |
| `audio: o microfone reabriu depois de uma falha` | `INFO` | `modo`, `falhas_seguidas`, `ligar_levou_ms` |
| `audio: o microfone parou de responder; o ditado não funciona até reiniciar o Agent` | `ERROR` | `modo`, `falhas_seguidas`, `err` |
| `winruntime: o áudio parou; nenhum ditado funciona até reiniciar o Agent` | `ERROR` | — (o canal de frames fechou com o Agent rodando) |

No sob demanda, a sessão **nunca desiste**. Quando o `Start` do keydown falha também reabrindo — o
USB fora, o endpoint ainda voltando depois de retomar a suspensão —, falham as tentativas que
pediam o microfone (o `Stop` devolve `ErrDeviceLost` na hora, e o bipe não sai), a sessão segue
viva e o keydown seguinte tenta de novo. Até a segunda rodada da revisão de 29-set-2026 ela morria
aqui, e o microfone só voltava reiniciando o Agent, mesmo de volta segundos depois. Não vira laço:
uma tentativa que falhou não segura o microfone, e cada nova tentativa de ligar precisa de um
keydown — `Ctrl+Win+setas` com o microfone fora custam dois `Start` por atalho, e mais nada. Só o
sempre pronto, que tenta sozinho, precisa de um limite. A primeira religação que dá certo depois
de qualquer falha, nos dois modos, sai com a linha `INFO` de reabertura.

**Encerramento.** `Close` cancela o contexto; a goroutine acorda, `Close` do dispositivo dá `Stop`
(inofensivo com o stream parado) e os `Release`. Cada objeto COM é **zerado** depois do `Release`,
para que um `Close` depois de "liberar" não solte duas vezes o mesmo objeto. `Close` espera a
goroutine no máximo 2 s: com um `Start` por keydown, um `Start` preso no driver travaria o "Sair"
da bandeja — antes, o único `Start` ficava sob o teto da subida.

## Medição

Linha `INFO audio: janela do microfone`, uma por tentativa que chegou ao `Stop` — inclusive
quando ele falhou, e também com o microfone já morto:

| Campo | O que é |
|---|---|
| `modo` | `sob_demanda` ou `sempre_pronto`, o que valia no keydown |
| `ja_ligado`, `reabriu` | o microfone já rodava na marca; ligar precisou reabrir o endpoint |
| `reabriu_por_falha` | o `Start` do keydown falhou e o microfone só ligou reabrindo o padrão |
| `ligar_levou_ms` | quanto a chamada de `Start` (e a reabertura) levou |
| `marca_ate_primeiro_pacote_ms` | do keydown até a chegada do primeiro pacote |
| `inicio_rel_marca_ms` | o número do AC-003, com sinal: início do áudio entregue menos a marca. Negativo no sempre pronto (é o pre-roll) |
| `pre_roll_entregue_ms` | áudio anterior à marca no buffer — 0 no sob demanda, sempre |
| `perdeu_inicio_ms` | o corte pedido era mais antigo que o que o ring guarda: quanto do começo ficou de fora. Tem de ser sempre 0 (o ring cabe o pior caso) |
| `esperou_mic_ms`, `cauda_esperou_ms` | as duas esperas do `Stop` |
| `faltou_no_fim_ms` | o áudio acabou antes do keyup e a espera pelo microfone venceu o teto: quanto do fim ficou de fora. 0 no caso normal |
| `fala_no_inicio` | os primeiros 80 ms já são fala (o mesmo VAD da cauda) |
| `pacote_anterior_ao_estacionamento` | o detector de contaminação, abaixo — só no ditado cujo corte começa no início da janela |
| `erro_timestamp`, `posicao_primeiro_pacote` | carimbo do primeiro pacote inválido; posição no stream (perto de 0 depois de um `Reset`) |
| `desligou_antes_do_corte` | tem de ser sempre `false` |
| `aberturas`, `fechamentos` | acumulados desde a subida |
| `bip_esperou_mic`, `bip_espera_ms`, `bip_saiu` | se o bipe esperou o microfone, quanto, e se chegou a sair |

**Alertas (`WARN`):** áudio de antes desta tentativa entrou no buffer (contaminação, ou pre-roll
no sob demanda); o microfone desligou antes do corte; o microfone abriu depois de a fala começar
(no sob demanda, `inicio_rel_marca_ms` acima de L ou `fala_no_inicio`, e só sem começo perdido no
buffer); o áudio acabou antes de a tecla subir (`faltou_no_fim_ms` acima de 0 — um carimbo
atrasado de forma sistemática, ou um microfone que parou no meio do ditado: o vigia não age com
ditado vivo, e de 500 ms para cima o estacionamento seguinte solta o cliente); o ditado passou do que o
buffer guarda (`perdeu_inicio_ms` acima de 0); o `Start` falhou e ligou reabrindo o microfone
padrão. E, no runtime, quando o bipe esperou o microfone e quando a tecla subiu antes de ele
entregar áudio. As linhas da recuperação do microfone estão em "Como funciona".

**Detector de contaminação:** o primeiro pacote de uma janela com carimbo anterior ao
**estacionamento anterior**, com tolerância de um período do dispositivo (`GetDevicePeriod`). Não
compara com o `Start` desta janela: o motor compartilhado trabalha em períodos, e um pacote
legítimo pode vir carimbado até um período antes do `Start`. A marca é da **janela**, mas só vale
para o ditado cujo corte começa no início dela — é ele que carrega aquele primeiro pacote. Numa
janela longa do sempre pronto, os ditados seguintes não herdam o alerta (revisão de 29-set-2026,
B1).

## Mudança de critério: o AC-003 por modo

- **Sob demanda (padrão):** a captura começa até **L** depois da marca do keydown
  (`inicio_rel_marca_ms ≤ L`), e nada se perde entre o primeiro frame capturado e o corte.
- **Sempre pronto:** o buffer inclui os 300 ms anteriores ao keydown, sem atravessar uma parada
  do microfone.

**L = 100 ms, provisório, por julgamento e não medição** — coerente com a medição da cauda de
28-set-2026, em que cortar 100 ms do fim mudou só uma vírgula. O trecho entre a marca e o primeiro
frame é **sempre** perdido no sob demanda: é a própria latência do `Start`. Por isso o teste do
AC-003 usa um dispositivo falso que demora para gravar o primeiro frame; com um `Start`
instantâneo, o teste provaria a medição à toa.

O que o teste automático prova é a parte que o código controla: a **medição** do início acompanha
o microfone (com latência abaixo e acima de L) e nada se perde entre o primeiro frame e o corte. O
"até L" é propriedade do microfone real, e nenhum dublê o impõe: quem o vigia é o alerta do
runtime, o M4 do roteiro manual e o critério de revisão, abaixo. O valor de L está preso num teste
(`TestLimiteDeInicio`) para que mudar a constante obrigue a mudar este texto e o roteiro junto.

## Consequências

**Positivas:**
- nenhum trabalho de áudio em repouso: a goroutine fica bloqueada, e nenhum pacote acorda o
  runtime;
- em campo protegido o microfone nem liga. Essa garantia é **tão boa quanto o cache de foco**: a
  nota de 29-set-2026 da ADR-011 registra que ele pode ficar defasado até ~60 s nos dois sentidos,
  e um "livre" antigo emite `MarkAudioStartEffect` e liga o microfone num campo de senha;
- com endpoint local, espera-se que o ícone de microfone apague em repouso — **a confirmar pelo
  roteiro manual** (abaixo).

**Negativas:**
- no padrão, **a fala que começa antes do aperto se perde**. O Mauro aceitou: "a fala só começa
  quando o bipe tocar";
- o início da captura passa de ~300 ms antes para algumas dezenas de ms depois do keydown — **uma
  estimativa, não medida**: quem mede é a linha `audio: janela do microfone` (`inicio_rel_marca_ms`)
  e o M4 do roteiro manual. Quem espera o bipe não perde nada **desde que o microfone abra em menos
  de 700 ms**; se demorar mais, o bipe espera por ele (decisão 5);
- `Ctrl+Win+<tecla>` liga o microfone por uma fração de segundo.

## Riscos, e o que não foi verificado nesta máquina

- **O ícone pode não apagar com o cliente preparado e parado.** A documentação da Microsoft
  trata do **estado da sessão** — `Initialize` a deixa inativa, `Start` a torna ativa, `Stop`
  devolve a inativa
  ([AudioSessionState](https://learn.microsoft.com/en-us/windows/win32/api/audiosessiontypes/ne-audiosessiontypes-audiosessionstate))
  — e **não diz nada sobre o ícone**. A ligação com o ícone vem só de outro programa de ditado
  para Windows, o TypeWhisper, que relata "Windows shows the microphone as free while the client is
  stopped" ([PR #536](https://github.com/TypeWhisper/typewhisper-win/pull/536)), sem medição. Se o
  M0 do roteiro mostrar o ícone aceso, o plano B é a política "liberar" como padrão — uma linha em
  `cmd/agent` (`configDeAudio`).
- **Custo de "liberar":** de +50 a ~+250 ms até o primeiro áudio, segundo relatos de terceiros —
  o TypeWhisper mediu 326 ms até o primeiro buffer numa sessão nova
  ([PR #330](https://github.com/TypeWhisper/typewhisper-win/pull/330)). Não medido aqui.
- **Latência do `Start`** no microfone do Mauro: não medida. A linha da janela do microfone é o
  instrumento; o critério que decide é o uso real (abaixo).
- **Partida a frio de AGC/supressão de ruído** do driver pode atenuar os primeiros 100–500 ms
  mesmo com os pacotes chegando em dia (não verificado). As métricas não distinguem "fala
  atenuada" de "não falou" — só o Bruto do ASR no painel pega (M2 do roteiro).
- **Troca do microfone padrão:** com a política "parar", o cliente preparado **continua
  capturando do microfone antigo, sem erro** — trocar o padrão não o invalida. Só remover ou
  reconfigurar o endpoint dá `AUDCLNT_E_DEVICE_INVALIDATED`. Com o microfone estacionado, isso
  aparece no `Start` do keydown, que solta o cliente e reabre o microfone padrão atual uma vez
  (`reabriu_por_falha=true`, com `WARN`). **Com o microfone ligado**, aparece numa leitura que
  falha: a tentativa em curso se perde, e o microfone reabre — sozinho no sempre pronto, no próximo
  keydown no sob demanda ("Recuperação do microfone", acima). O áudio só morre até reiniciar o
  Agent no sempre pronto, depois de oito falhas seguidas; no sob demanda, cada keydown tenta de
  novo. Com "liberar", cada ditado pega o microfone padrão atual. Não há `IMMNotificationClient`:
  trocar o padrão sem invalidar o cliente continua sem efeito até ele cair.
- **Suspender e retomar:** não se sabe se o Windows invalida o cliente ou só pausa o stream, nem se
  o stream pausado volta sozinho. Com o sempre pronto, uma leitura que falha ou um dos vigias (a
  parada, ou a janela que liga e nunca entrega) soltam e reabrem o microfone, e um ditado no meio
  do recuo tenta ligar na hora; no sob demanda, o microfone estava estacionado, e o que falhar
  aparece no `Start` do keydown, que reabre — ou, se o endpoint ainda estiver voltando, falha
  aquele ditado e o seguinte tenta de novo. Um cliente parado que liga sem erro e não entrega faz
  aquele ditado falhar sem áudio; no sob demanda, o estacionamento seguinte o solta, e no sempre
  pronto, o vigia do primeiro pacote, no repouso: o próximo ditado reabre. Nos dois modos o ditado
  seguinte tem de funcionar; o M6 e o M7 do roteiro manual conferem, e as linhas de log dizem o
  que aconteceu. Se o contador de
  desempenho (QPC) andar durante a suspensão, o vigia dispara logo depois de retomar e o sempre
  pronto reabre mesmo que o stream voltasse sozinho — inofensivo, e a linha do vigia no log diz.
- **Falha do `Start` no keydown** (a da reabertura também) só chega à pessoa no keyup (o núcleo
  só aceita a falha de áudio como resposta ao `Stop`). Mas a pílula não diz mais "Gravando" com o
  microfone morto: sem primeiro pacote, ela nunca entra nessa fase, e o keyup mostra o erro.
- **Bluetooth.** O Mauro não usa fone Bluetooth como microfone (notebook ou USB), e nada aqui o
  trata de modo especial. O risco fica registrado: a página do `IOCTL_BTHHFP_STREAM_OPEN` diz
  "This request can remain pending for several seconds while resources are established."
  ([documentação de driver](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/bthhfpddi/ni-bthhfpddi-ioctl_bthhfp_stream_open)),
  e o bipe esperaria por isso. Não se sabe se, com o cliente parado, o fone volta ao A2DP. E, no
  **sempre pronto**, um fone cujo primeiro pacote legítimo chegue mais de 2 s depois de o `Start`
  voltar nunca se firma no repouso (quarta rodada da revisão, 30-set-2026): o vigia do primeiro
  pacote o solta aos 2 s, a reabertura demora de novo, e o recuo sobe até 30 s — o microfone passa a
  ciclar, fora por 30 s e ligado por 2 s, com três linhas no log a cada ~32 s (as duas do vigia e a
  da reabertura) e o ícone piscando, sem nunca desistir, porque o vigia só recua. O ditado ainda
  funciona: o keydown corta o recuo e liga na hora, o bipe espera o microfone, e, com a tecla
  segurada até o primeiro pacote, a janela se firma e o ciclo para. Quem solta a tecla antes perde
  aquele ditado (`ErrMicrofoneNaoAbriu`). Uma demora dentro do próprio `Start` não entra aqui: o vigia
  conta de quando ele volta. Não se aplica ao Mauro (notebook ou USB), e nada foi medido; se um dia
  aplicar, a saída considerada é suspender o vigia do primeiro pacote depois de alguns disparos
  seguidos sem nenhum pacote no meio — o que também deixaria sem saída o cliente que volta mudo da
  suspensão, e por isso fica para quando houver o caso. No sob demanda, nada disso: não há vigia.
- **Remote Desktop: adiado.** O TypeWhisper relata que, com redirecionamento de microfone, um
  cliente parado mantém o microfone do lado cliente em uso
  ([PR #536](https://github.com/TypeWhisper/typewhisper-win/pull/536)). O Windows 11 Home do Mauro
  não recebe conexão RDP de entrada, então esse ramo nunca rodaria aqui. Se um dia for preciso,
  a detecção tem de seguir a documentação completa
  ([Detecting the Terminal Services environment](https://learn.microsoft.com/en-us/windows/win32/termserv/detecting-the-terminal-services-environment),
  com `GlassSessionId`), não só `SM_REMOTESESSION`.
- Não se sabe se um cliente parado ainda segura o *power request* "An audio stream is currently
  in use", que impede a suspensão (`powercfg /requests`, como administrador).

## Testes

Automáticos, puros, contra um dispositivo falso que **recusa a ordem errada** (ligar duas vezes,
parar sem ligar, `Reset` com o stream rodando, ler com o microfone estacionado), em
`internal/agent/audio/microfone_test.go`: não liga na subida; zero leituras em repouso; toque
liga e estaciona com `Reset`; o microfone segue ligado durante toda a espera da cauda (conferido
de dentro da espera); duas paradas sobrepostas; `Abort` durante a parada; o segundo ditado não
tem nada do primeiro, mesmo com pacote pendente no dispositivo; `Reset` que falha leva a liberar
e reabrir; o detector de contaminação, com e sem a tolerância de um período; `Stop` espera o áudio
do keyup, e dá o erro próprio quando o microfone não abre ou abre tarde, e diz quanto faltou quando
o áudio acaba antes do keyup; o AC-003 com latência no ligar; a decisão de estacionar no mesmo lock,
e uma marca que cai dentro do `Stop` do dispositivo; o `Start` que falha e reabre, e o que falha de
novo e perde só aquele ditado (o keydown seguinte dita); os keydowns sem microfone que não viram
laço; `Close` com o `Start` preso; a janela medida também com o microfone morto; os HRESULTs de
`IAudioClient::Start`, escritos à mão a partir de `audioclient.h`; o sempre pronto intacto (liga na
subida, nunca estaciona, entrega 300 ms) e sem atravessar uma parada; o ditado termina no modo do
keydown, nos dois sentidos; o alerta de contaminação só no ditado que pega o início da janela; o
ring que cabe o pior caso, e a janela que diz quanto perdeu quando não cabe. A recuperação, em
`internal/agent/audio/recuperacao_test.go`: a leitura que falha em repouso no sempre pronto
reabre sozinha e o ditado seguinte sai sem nada de antes da falha; a que falha no meio de um
ditado (nos dois modos) e durante a parada derruba aquele ditado e não o seguinte, e no sob demanda
nada religa antes do keydown; a que falha com o `Stop` esperando o áudio do keyup dá o erro do
dispositivo na hora; a linha da janela de um ditado que falhou é a da janela em que ele falhou, e a
de um que nem ligou não herda a anterior; a religação que falha recua 1, 2, 5, 10, 30 e 30 s e
desiste na oitava falha com `ERROR`; o microfone que religa e falha na primeira leitura recua
igual; com o relógio andando o recuo inteiro, a espera não conta como tempo saudável (desiste aos
78 s); uma falha depois de 10 s saudáveis religa na hora; ligar o sempre pronto com o microfone
fora avisa quando reabre; o microfone que para sem erro é solto pelo vigia aos 500 ms, não antes, e
o ditado seguinte não traz a fala de antes da parada. Em `vigia_test.go`: a goroutine acordando
atrasada não derruba o ditado nem conta como parada no sempre pronto; um soluço no meio do ditado
corta só o pedaço, com `FaltouNoFim`; o cliente que liga e não entrega é solto no estacionamento e
o ditado seguinte funciona; a parada repetida só recua, e o sempre pronto não desiste; `DefinirSemprePronto` e `Close` tiram a
goroutine do recuo. E, da terceira rodada, também em `vigia_test.go`: o keydown no meio do recuo
liga o microfone na hora e o ditado sai, e a religação do recuo seguinte ainda conta; num apagão,
mais keydowns que `MaxFalhasSeguidas` perdem cada um só o seu ditado, na hora, sem mexer no recuo
nem na cota, e a sessão desiste na mesma oitava falha do recuo; o keydown com a religação do recuo
em curso perde o ditado na hora, e ela conta; o keydown com o recuo já vencido não o corta; a
janela que liga e nunca entrega é solta no repouso aos 2 s, não antes e não com ditado vivo, e o
ditado seguinte funciona; a janela muda repetida só recua, e não gasta a cota. Da quarta rodada
(30-set-2026), também em `vigia_test.go`: com um microfone que religa e cai na primeira leitura,
mais keydowns que `MaxFalhasSeguidas` no recuo não gastam a cota nem mexem no recuo, e a sessão
desiste na mesma oitava falha do recuo; depois de um keydown que religou, 15 s saudáveis abrem um
incidente novo, com a religação imediata; a janela do keydown que estaciona sem veredito não leva
o corte para o sempre pronto religado; um keydown que cai entre a decisão de um vigia e a ação dele
(os dois vigias) não perde o ditado, e a linha do vigia não sai; no sob demanda, o vigia do
primeiro pacote não age nem no instante entre o `Stop` e o estacionamento; e ele conta de quando o
`Start` voltou — um `Start` preso 1,5 s no driver não é janela muda. No runtime, `internal/agent/winruntime/microfone_test.go`: o bipe e a pílula
esperam o microfone, e não saem depois do keyup; um frame que chega sem o microfone entregando não
solta nada; o bipe fica retido sozinho se o microfone para entre a pílula e o bipe; com o bipe
desligado, nada toca ao liberar; a onda só anda com o microfone entregando para o ditado em curso;
a linha da janela e cada alerta; o `ERROR` quando o canal de frames fecha com o Agent rodando. `cmd/agent` sobe no sob demanda com o ring do pior caso, e `aplicarPreferencias`
leva a opção das Configurações ao microfone na subida e a cada Salvar, e não diz "mudou" com a
sessão morta; `carregarPreferencias` lê de novo depois de um erro passageiro
(`cmd/agent/preferencias_test.go`); `cmd/asrrec` pede captura contínua. Todos conferidos por
mutação.

Manuais: `docs/teste-manual-bandeja-e-painel.md`, parte 6 — o ícone, o bipe, a primeira sílaba no
microfone do notebook e no USB, `Ctrl+Win+D`, o campo de senha, as linhas novas do log (M0 a
M5), o "sempre pronto" pelas Configurações, inclusive suspendendo e retomando o notebook com ele
ligado (M6), e suspender e retomar no sob demanda, ditando assim que a sessão volta (M7).

## Revisão

O critério que decide é o **uso real**: depois de 50 ou mais ditados do dia a dia, o p95 de
`inicio_rel_marca_ms` fica em até L e não aparece nenhum alerta de contaminação nem de microfone
desligado antes do corte. Dez de dez no roteiro só limitam a taxa real de falha a ~26% (95% de
confiança); cinquenta, a ~6%. Se a primeira sílaba se perder, as saídas são: "sempre pronto"
ligado; manter o microfone ligado alguns segundos depois do ditado; ou a política "liberar". Troque
o L provisório por um número medido quando houver.
