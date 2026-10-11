# Defeito — o overlay era desenhado atrás da janela em foco

Status: **RESOLVIDO** em 26-set-2026
Encontrado por: Mauro, usando o produto

## O sintoma

> "eu ouço o som mas não vejo nada na tela"

Dois monitores, nada em nenhum dos dois.

## Por que demorou a ser diagnosticado

Porque tudo indicava que estava funcionando. Os logs provavam três coisas:

1. `winruntime: overlay attempt=1 fase=recording` — o gatilho foi detectado e o
   núcleo pediu o overlay;
2. nenhum `overlay: nao consegui apresentar o frame` — o `UpdateLayeredWindow`
   **não** falhou;
3. nenhum erro em lugar nenhum.

A janela era criada, desenhada e mostrada. E ficava invisível.

Vale registrar que esses dois primeiros logs **não existiam** quando o problema
foi reportado — foram acrescentados no mesmo dia justamente porque "não aparece
nada" era indistinguível de "o gatilho não foi detectado". Sem eles, o defeito
abaixo continuaria escondido atrás de três hipóteses igualmente plausíveis.

## A causa

Faltava **um bit**.

```go
exStyle := win32.WSExLayered | win32.WSExTransparent |
           win32.WSExToolWindow | win32.WSExNoActivate
//                                 ^ sem WS_EX_TOPMOST
```

`WS_EX_NOACTIVATE` combinado com `SW_SHOWNOACTIVATE` garante que a janela **nunca
suba** na pilha de z-order. Os dois estão ali de propósito e são corretos — o
overlay não pode roubar o foco do campo em que o texto vai ser inserido. Mas sem
`WS_EX_TOPMOST` a consequência é que a pílula fica **atrás da janela que tem o
foco**, que é exatamente onde a pessoa está olhando e digitando.

## Por que passou por dois roteiros de teste manual

Porque o defeito **depende de sorte**: quando o centro inferior da tela está
descoberto — desktop visível, janela não maximizada — a pílula aparece
normalmente. Foi o que aconteceu em 20-set-2026, quando Mauro relatou "ele
mostrou gravando, a onda se mexeu conforme a voz falava".

Um teste que diz "olhe se a pílula aparece" passa ou falha conforme o que estiver
aberto na hora. Não é um roteiro ruim; é um defeito que esse tipo de roteiro não
consegue prender.

## A correção, e onde ela foi posta

`WS_EX_TOPMOST` entrou no conjunto. Mas o mais importante é **onde** o conjunto
passou a morar.

O `exStyle` estava dentro de `window_windows.go`, atrás de `//go:build windows`
— onde nenhum teste da suíte chega. Ele foi movido para `style.go`, **sem build
tag**, porque é dado: uma máscara de bits, não uma chamada de sistema.

É literalmente a regra que o `CLAUDE.md` já registrava:

> "A lógica de decisão é pura e vai sem build tag (...) e é onde moram os bugs
> interessantes."

Cinco testes agora prendem o conjunto, cada um com a consequência de perder o
bit:

| bit | o que acontece sem ele |
|---|---|
| `TOPMOST` | a pílula fica atrás da janela em foco — **este defeito** |
| `NOACTIVATE` | o overlay rouba o foco e destrói o alvo da inserção |
| `TRANSPARENT` | a pílula engole cliques no canto inferior da tela |
| `LAYERED` | `UpdateLayeredWindow` falha e nada aparece |
| `TOOLWINDOW` | o overlay aparece no Alt+Tab e na barra de tarefas |

`TestOverlayExStyle_EhTopmost` foi confirmado por mutação: removendo o bit, ele
falha com a mensagem que descreve o sintoma real.

## O que este episódio deixou de diagnóstico

Três logs que não existiam antes, e que continuam valendo:

- `winruntime: overlay ... fase=...` — separa "o gatilho não foi detectado" de
  "a janela não apareceu";
- `overlay: janela criada x=... tela_primaria=...` — mostra em que tela e em que
  posição a pílula foi posta (`GetSystemMetrics` só enxerga a tela **primária**;
  com dois monitores ela vai para o primário de qualquer jeito);
- `overlay: nao consegui apresentar o frame` — a falha de `Draw`, que antes era
  engolida com um comentário de *best-effort* num caminho em que o
  `SetVisible(true)` nem chegava a ser chamado.

## Depois, em 30-set/01-out-2026: o bit não garante o topo

O `TOPMOST` põe a pílula na faixa das janelas topmost, mas dentro da faixa vale quem subiu por
último, e a pílula nasce uma vez, na subida do Agent. Mauro relatou de novo a pílula não aparecendo
sobre uma janela em primeiro plano, num build antigo; no build novo apareceu, e a causa **não foi
estabelecida**. A defesa barata contra a hipótese (outra janela topmost que subiu depois): ao
mostrar, a pílula reafirma o topo com `SetWindowPos(HWND_TOPMOST)`, sem ativar, sem mover e sem
redimensionar. As flags moram em `comoMostrar` (`overlay/style.go`, sem build tag), e uma trava lê o
`window_windows.go` para garantir que `SetVisible` as usa. Ver o ADR-010, D2.

O limite é o do defeito original: nenhum teste cria janela, então **só o roteiro manual prova** que a
pílula aparece por cima de uma topmost comum, aberta depois do Agent e posta sobre o centro inferior
da tela, com o Bloco de Notas em foco (um formulário do PowerShell com `TopMost`, ou o "Always On Top"
do PowerToys; o passo está no I6 de `docs/teste-manual-instalador.md`).

**O Gerenciador de Tarefas não serve de teste** (a revisão de 01-out-2026 apontou que a primeira
versão desta seção o usava). Desde o Windows 8, com "Sempre visível" ele fica numa faixa própria,
acima de todas as topmost comuns, e uma janela de programa comum não passa dele com
`SetWindowPos(HWND_TOPMOST)`: a pílula pode ficar atrás dele mesmo com a correção certa. É o que
descrevem terceiros, e não a Microsoft, e não foi verificado aqui. Quem registrasse "defeito" ou
"a correção não funciona" a partir dele estaria medindo outra coisa.
