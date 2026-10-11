# Sprint 1 — Roteiro de teste manual

Hook de teclado, overlay e captura de áudio **não têm como ser provados por teste
automatizado**. Este roteiro é a única verificação possível dessas peças, e precisa de uma
pessoa olhando a tela.

Tempo estimado: 15 minutos.

---

## Antes de começar — leia

### O que o Agent faz enquanto está rodando

Ele instala um **hook global de teclado** e muda o comportamento da tecla **F**:

- **toque rápido em F** (< 700 ms) → digita `f` normalmente
- **segurar F** (> 700 ms) → **não digita `f`**, começa a gravar

Qualquer outra tecla, e F com modificador (`Ctrl+F`, `Alt+F`), seguem intactas.

### Como parar

`Ctrl+C` no terminal onde ele está rodando. O Agent desinstala o hook, para o áudio e destrói o
overlay. A tecla F volta ao normal **imediatamente**.

Se o terminal fechar ou o processo morrer, o Windows remove o hook sozinho — não fica resíduo.

### ✅ Resolvido no Sprint 2 — campo de senha está protegido

> Este aviso dizia, quando o roteiro foi escrito: *"campo de senha NÃO está protegido — não
> teste dentro de um campo de senha de verdade"*. **Isso deixou de valer em 17-set-2026**, com o
> Sprint 2.
>
> O `AC-036` exige bloquear o ditado em campo protegido. A lógica do núcleo estava pronta e
> testada desde a Fase 1, mas dependia da UI Automation para saber que o campo era de senha —
> e a UIA só chegou no Sprint 2. Até lá, `FocusTarget.Protected` era sempre falso e
> `StateBlocked` era inalcançável em produção.
>
> Agora o rastreador de foco preenche esse campo, e o bloqueio foi **verificado em máquina
> real**: o overlay mostra "Ditado desativado em campo protegido", nenhum áudio é capturado,
> e o toque curto continua digitando a letra normalmente.
>
> O teste está em `sprint-2-teste-manual.md`, **T12**. Corrigido aqui em vez de apagado: o
> aviso descreve uma lacuna que existiu de verdade, e saber quando ela fechou vale mais que
> fingir que nunca houve.

---

## Como subir

```bash
C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

Deixe esse terminal aberto e visível — os logs aparecem nele.

Se preferir recompilar antes:

```bash
go -C C:/Desenvolvimento/aurora-whisper build -o bin/aurora-whisper-agent.exe ./cmd/agent
```

> **Histórico — não use este comando hoje (29-set-2026).** Desde a bandeja (commit 73de148), o
> `bin/aurora-whisper-agent.exe` é o executável do início automático e precisa ser compilado com
> `-ldflags "-H=windowsgui"`. O roteiro atual está em `docs/sprint-4-teste-manual.md`.

---

## O que você vai ver ao ditar

O ASR é stub nesta fatia. Ao soltar a tecla, o texto inserido será literalmente:

```
(aurora-whisper: transcricao real pendente - ADR-010, ASR fica para o Sprint 3)
```

Isso é deliberado: um texto impossível de confundir com transcrição real. Se ele aparecer, o
caminho completo funcionou — hook, áudio, VAD, overlay, máquina de estados e inserção.

---

## Roteiro

Abra o **Bloco de Notas** e clique dentro dele.

### T1 — Toque curto preserva a tecla `AC-001` `TEST-001`

**Faça:** toque F rapidamente, 5 vezes.
**Esperado:** aparecem exatamente `fffff`, sem atraso perceptível.
**Falhou se:** falta algum `f`, aparece `f` duplicado, ou há atraso visível.

> Este teste **não consegue** detectar tecla fora de ordem: uma sequência de uma letra só sai
> igual em qualquer ordem. Quem cobre isso é o T9 — e ele deixou passar. Ver
> `defeito-ordem-da-tecla-de-ditado.md`.

### T2 — Segurar não digita e começa a gravar `AC-002` `TEST-002`

**Faça:** segure F por uns 2 segundos, sem falar. Solte.
**Esperado:**
- **nenhum `f`** aparece no Bloco de Notas
- um overlay aparece no **centro inferior** da tela, escrito "Gravando", com onda e duração
- um beep curto e discreto ao começar

**Falhou se:** aparece `f`, ou o overlay não aparece.

### T3 — Ditado completo `TEST-003`

**Faça:** segure F, fale uma frase qualquer, solte.
**Esperado:** o overlay passa por "Processando" e depois "Inserido", e o texto do stub é digitado
no Bloco de Notas.
**Falhou se:** nada é inserido, o overlay trava, ou o Agent para de responder.

### T4 — A onda reage à voz `AC-005`

**Faça:** segure F e fale alto, depois baixo, depois fique em silêncio.
**Esperado:** as barras da onda sobem e descem acompanhando a voz. A duração conta os segundos.
**Falhou se:** a onda fica estática, ou não muda com o volume.

### T5 — Atalho com modificador continua funcionando `AC-045`

**Faça:** abra o **Chrome** e pressione `Ctrl+F`.
**Esperado:** a busca do navegador abre normalmente. Nenhuma gravação começa.
**Falhou se:** a busca não abre, ou o overlay aparece.

### T6 — O overlay não rouba o foco

**Faça:** com o cursor piscando no Bloco de Notas, segure F e observe a barra de título.
**Esperado:** o Bloco de Notas continua sendo a janela ativa, e o cursor continua piscando lá.
**Falhou se:** a janela perde o destaque, ou o cursor para de piscar.

### T7 — O overlay não aparece no Alt+Tab

**Faça:** segure F com uma mão e pressione `Alt+Tab` com a outra (ou segure F por bastante tempo
e use o Alt+Tab).
**Esperado:** o overlay **não** aparece entre as janelas do Alt+Tab.
**Falhou se:** ele aparece.

### T8 — O overlay não bloqueia cliques

**Faça:** enquanto o overlay estiver visível, clique exatamente em cima dele.
**Esperado:** o clique atravessa e atinge a janela que está atrás.
**Falhou se:** o clique é engolido, ou o overlay ganha foco.

### T9 — Digitação normal continua intacta ⚠️ o mais importante

**Faça:** digite rápido uma frase com vários `f`, por exemplo:
`refrigerador fofoca afinal difícil fácil`
**Esperado:** tudo sai certo, sem atraso, sem caractere faltando ou fora de ordem.
**Falhou se:** falta letra, há travamento, ou a digitação fica lenta.

> ⚠️ **Este teste foi marcado como passou em 17-set-2026 e o defeito estava lá.** Em uso normal,
> `freio` sai `refio`: o `f` chega depois das letras seguintes. Ver
> `defeito-ordem-da-tecla-de-ditado.md`.
>
> Conferir "sem caractere fora de ordem" batendo o olho não funciona — a troca é de duas posições
> e some na leitura. **Digite a palavra, pare, e compare caractere a caractere.** Palavras curtas
> com F no começo (`freio`, `fruta`, `frase`) expõem melhor que uma frase longa.

> Este teste vale mais que os outros. A regressão proibida da spec é
> *"hook bloquear teclado inteiro"* — e o callback do hook tem orçamento de ~300 ms. Se
> estourar, **o Windows remove o hook em silêncio** e o ditado morre sem nenhum erro visível.

### T10 — Funciona em outros aplicativos

Repita **T1**, **T2** e **T3** em:

- **Chrome**, numa caixa de texto qualquer (busca do Google serve)
- **VS Code**, num arquivo aberto

**Falhou se:** funciona no Bloco de Notas mas não em algum deles. Anote em qual e o que aconteceu.

### T11 — Encerramento limpo

**Faça:** `Ctrl+C` no terminal do Agent. Depois volte ao Bloco de Notas e segure F por 2 segundos.
**Esperado:** o Agent encerra sem erro, e agora segurar F digita `fffff...` normalmente (repetição
do Windows) — o hook saiu.
**Falhou se:** o Agent trava ao encerrar, ou a tecla F continua sequestrada.

---

## O que me reportar

Para cada teste: **passou** ou **falhou**. Nos que falharem, o que você viu.

Se algum travar o teclado, mate o processo pelo Gerenciador de Tarefas
(`aurora-whisper-agent.exe`) — o Windows remove o hook junto.

Me mande também as últimas linhas do terminal do Agent, principalmente se aparecer algum `WARN`
ou `ERROR`.

---

## O que este roteiro NÃO prova

Para ninguém confundir "passou" com "pronto":

- **Qualidade de transcrição** — o ASR é stub; o texto inserido é fixo. O real depende do
  benchmark do Sprint 3.
- **Inserção robusta** — hoje é `SendInput` caractere a caractere. UI Automation e o fallback de
  clipboard com restauração são a fatia seguinte (`TASK-004`). Em app que bloqueie entrada
  sintética, pode falhar.
- **Campo de senha** — não bloqueado ainda (ver o aviso no topo).
- **Latência real** — a meta de `p50 ≤ 500 ms` só faz sentido medir com ASR de verdade.
- **Uso prolongado** — este roteiro leva minutos. Vazamento de handle COM ou de memória só
  aparece em horas de uso.

---

## Resultado da execução

**17 de setembro de 2026** — roteiro executado por Mauro na máquina de referência
(Dell G15, i7-13650HX, 32 GB, RTX 3050, Windows 11).

**Todos os 11 testes passaram.**

O que isso estabelece, e que nenhum teste automatizado conseguiria:

- o hook global convive com a digitação normal sem perder caractere nem introduzir atraso
  perceptível (T9 — a regressão proibida "hook bloquear teclado inteiro" não se materializou,
  e o callback não estourou o orçamento de ~300 ms)
- o `tap` curto preserva a tecla e o `hold` não emite caractere, em aplicativos reais
- `Ctrl+F` continua abrindo a busca do navegador — o bypass por modificador funciona
- o overlay não rouba foco, não aparece no Alt+Tab e não bloqueia cliques: as quatro flags
  de `WS_EX_*` fazem o que o `ADR-010` previu
- a captura WASAPI em Go puro funciona contra hardware de áudio real, e a onda reage à voz
- a inserção por `SendInput` chega ao campo com foco em Bloco de Notas, Chrome e VS Code
- o encerramento devolve a tecla F imediatamente

Continua valendo tudo da seção "o que este roteiro NÃO prova" — em especial que o campo de
senha **ainda não está protegido**, e que a qualidade de transcrição não foi avaliada porque
o ASR é stub.
