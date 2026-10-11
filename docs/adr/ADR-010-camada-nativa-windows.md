# ADR-010 — Camada nativa Windows: áudio, overlay e dependências

Status: Accepted
Data: 2026-09-17
Contexto: Fase 2, Sprint 1

## Contexto

A Fase 1 terminou com 512 testes verdes, **todos contra mocks**. Nenhuma linha do repositório
usa `syscall` ou `golang.org/x/sys/windows`: hook, áudio, UI Automation e inserção não existem.

O Sprint 1 troca os mocks do núcleo do ditado por Win32 de verdade. O critério de sucesso não é
"funciona" — é:

> **a suíte da Fase 1 continua verde depois da troca, sem ser reescrita.**

Se os testes precisarem mudar, o desenho do [ADR-007](ADR-007-dictation-core.md) falhou, e é
melhor descobrir isso agora do que no Sprint 10.

Três desenhos independentes foram produzidos, com vieses opostos (menor risco de implementação,
Go puro, isolamento de processo). Esta ADR é a síntese, com a escolha de áudio confirmada pelo
dono do produto.

## D1 — Áudio: WASAPI em Go puro, no mesmo processo

**Decisão:** implementar a captura WASAPI em Go puro, por `syscall` sobre vtable COM, dentro do
processo do Agent, atrás da interface `AudioSource` que já existe desde o ADR-007.

**Escopo deliberadamente apertado.** Só o caminho mínimo:

- `IMMDeviceEnumerator` → `GetDefaultAudioEndpoint` (dispositivo padrão, sem seleção)
- `IAudioClient::Initialize` em **modo compartilhado** (nunca exclusivo)
- `IAudioCaptureClient::GetBuffer` / `ReleaseBuffer`
- resampling do mix format para **16 kHz mono int16**, em Go — matemática pura, inteiramente
  testável sem hardware

> **Correção de 17-set-2026.** Este item dizia "tipicamente float32 estéreo a 44,1/48 kHz", e a
> implementação leu "tipicamente" como "sempre": aceitava apenas float32 ou PCM de 16 bits, e
> recusava o resto na abertura do dispositivo. O Agent não subiu numa máquina real cujo microfone
> reporta **PCM de 32 bits inteiro** — formato perfeitamente legal em modo compartilhado, que
> alguns drivers expõem.
>
> Os formatos suportados agora são **float32, e PCM de 16, 24 e 32 bits**. A conversão foi movida
> para `pcm.go`, fora da build tag `windows`, porque escala e extensão de sinal são matemática
> pura — e é exatamente onde nascem os erros silenciosos (áudio distorcido, sinal invertido) que
> um teste de tamanho de slice nunca pegaria. Presa dentro do arquivo Windows, nenhum teste a
> alcançava.
>
> Lição para o resto do documento: onde esta ADR escreve "tipicamente", leia "o caso comum, e o
> código precisa lidar com os outros" — nunca "o único que existe".

Fora de escopo no Sprint 1: modo exclusivo, seleção de dispositivo, `IMMNotificationClient`
para hot-plug. Microfone removido no meio é detectado pelo erro de `GetBuffer` e vira
`AudioFailedEvent` — que o núcleo já sabe tratar.

### Por que não CGO

O `ADR-008` rejeitou CGO por três razões: build sem pré-requisito, cross-compilation trivial
para o installer, e coerência com o ADR-002. Um gcc mingw foi instalado na máquina de
desenvolvimento durante esta sessão, e é importante não confundir o que isso muda:

> **O gcc mudou a viabilidade, não o cálculo.** O CI compila Go em matriz ubuntu + windows;
> com `CGO_ENABLED=1`, o runner Linux passa a precisar de um cross-compiler mingw versionado e
> mantido. E qualquer pessoa que clonar o repositório amanhã volta a precisar de toolchain C.
> Nenhuma dessas consequências é resolvida por um compilador existir nesta máquina hoje.

### Por que não processo auxiliar isolado

O argumento a favor era que um crash em código nativo é uma exceção de SO, não um `panic`
recuperável, e derrubaria o processo que segura o hook.

A primeira metade é verdadeira. A segunda, que sustentava o peso do argumento, não é: **quando
o processo que instalou um `WH_KEYBOARD_LL` morre, o Windows remove o hook e o teclado volta ao
normal.** O estrago de um crash é "o Agent parou", não "o teclado da pessoa quebrou" — e isso
um tray com restart resolve (Sprint 10).

Com o dano real muito menor do que o alegado, pagar IPC no caminho quente — com latência que os
três desenhos admitiram **não ter medido** — é prematuro. Revisitar só com dado de crash real.

### Custo aceito, sem minimizar

São na ordem de 800–1200 linhas de COM escritas à mão, com `unsafe` e índice de vtable manual.
Todo erro de layout de struct ou de offset aparece **em runtime**, não na compilação nem no
`go vet`. Bugs de dispositivo exótico que uma biblioteca madura já teria tratado passam a ser
nossos para descobrir.

### Ordem de fuga, se doer demais

Só com evidência medida, nesta ordem: `moutend/go-wca` (pure-Go, evita escrever o COM) →
CGO/miniaudio → processo auxiliar. A interface `AudioSource` existe justamente para que qualquer
uma dessas trocas seja contida.

## D2 — Overlay: janela layered Win32 com GDI

**Decisão:** janela nativa com `WS_EX_LAYERED | WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW |
WS_EX_TRANSPARENT`, desenhada em memory DC ARGB e apresentada por `UpdateLayeredWindow`.

Cada flag resolve um requisito explícito do `08-ui-ux-spec.md`:

| Flag | Requisito que atende |
|---|---|
| `NOACTIVATE` | não rouba foco |
| `TRANSPARENT` | não bloqueia cliques (hit-test passa para a janela de baixo) |
| `TOOLWINDOW` | não aparece no Alt+Tab nem na taskbar |
| `LAYERED` + `UpdateLayeredWindow` | alpha por pixel, necessário para cantos arredondados e sombra |

`SetLayeredWindowAttributes` sozinho não serve: dá colorkey ou alpha constante, não alpha por
pixel.

**Atualização de 29-set-2026 — a janela muda de tamanho por fase.** Com as mensagens do tamanho
do texto (ADR-014, "as mensagens"), a janela deixou de ser fixa: cada `UpdateLayeredWindow`
leva, além do conteúdo, a posição (`pptDst`) e o tamanho (`psize`) da pílula da fase, e o
Windows troca os três juntos — sem `SetWindowPos` à parte, sem piscar. O DIB continua alocado
uma vez, no tamanho da maior pílula; cada quadro ocupa o canto dele. O texto é IBM Plex Mono
rasterizada em Go puro sobre os bits — continua sem GDI de texto.

**Atualização de 01-out-2026 — a pílula reafirma o topo ao ser mostrada.** `WS_EX_TOPMOST` põe a
janela na faixa das topmost, mas dentro da faixa vale quem subiu por último, e a pílula nasce uma
vez, na subida do Agent: uma janela topmost que apareça depois (a de qualquer programa comum com
"sempre visível") fica acima dela. O Gerenciador de Tarefas **não** é exemplo disto nem serve de
teste (ver a conferência manual, abaixo). O Mauro relatou em 30-set-2026 a pílula não aparecendo sobre uma
janela em primeiro plano, num build antigo; no novo apareceu, e a causa do relato **não foi
estabelecida**. A defesa barata contra a hipótese: ao mostrar, `SetVisible(true)` faz o
`ShowWindow(SW_SHOWNOACTIVATE)` de sempre **e** `SetWindowPos(HWND_TOPMOST, 0, 0, 0, 0,
SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE | SWP_NOOWNERZORDER | SWP_SHOWWINDOW)`. É a exceção ao "sem
`SetWindowPos`" acima: esta não move nem redimensiona (quem faz isso continua sendo o
`UpdateLayeredWindow`), só recoloca a janela no alto da faixa. Como `SetVisible(true)` corre a cada
quadro da gravação, a chamada vai junto; sobre uma pílula que já está no alto, não muda nada.
**O `NoActivate` não cede:** o ditado insere no campo que tem o foco, e uma flag a menos aqui o
tiraria dele. A decisão (`comoMostrar`, em `overlay/style.go`, pura e sem build tag) tem testes das
flags (sem `SWP_NOACTIVATE`, ou com `SWP_NOZORDER`, que faria o `SetWindowPos` ignorar o
`HWND_TOPMOST`, o teste cai), e uma trava lê `window_windows.go` e confere que `SetVisible` usa
exatamente o que ela devolve e que nada no overlay ativa a janela. Mutações numa cópia isolada
(`git archive`, diff conferido, `-timeout 90s`, restauração conferida): 18 mutantes, todos mortos
(cada flag removida ou acrescentada, `SW_SHOW` no lugar de `SW_SHOWNOACTIVATE`, `HWND_TOPMOST`
trocado, valores das constantes, o `SetWindowPos` removido ou com flags à mão, a condição de esconder
invertida, uma função de ativar foco no código por identificador). **A revisão achou um furo nas
travas:** a ligação por nome, como o arquivo já faz com o `PostMessageW`
(`procTrazer = user32.NewProc("SetForegroundWindow")`, chamada em `SetVisible`), passava por todos
os testes do overlay. Agora a trava de "nada ativa a janela" confere também os literais de texto, e a
de `SetVisible`, **toda** chamada do corpo (só `ShowWindow`, `comoMostrar`, `ShowWindow` e
`SetWindowPos`); quatro mutantes novos (o da revisão, a ligação sem chamada, uma chamada de
`postMessageW` em `SetVisible` e a ligação de `SetFocus`) morrem. **Não verificado no Windows de
verdade:** que a reafirmação traz a pílula acima de uma topmost que subiu depois (nenhum teste cria
janela). **Conferência manual, do Mauro, com uma topmost comum aberta DEPOIS do Agent** (o roteiro
completo está no I6 de `docs/teste-manual-instalador.md`): um formulário do PowerShell com
`TopMost`, ou o "Always On Top" do PowerToys, posto sobre o centro inferior da tela; dê foco ao Bloco
de Notas (na parte de cima da tela) e dite; a pílula tem de aparecer **por cima** da janela de teste,
e o texto entrar no Bloco de Notas. **O Gerenciador de Tarefas não serve:** desde o Windows 8 ele, com
"Sempre visível", fica numa faixa própria, acima de todas as topmost comuns (a mesma razão pela qual
fica por cima de jogos em tela cheia; é o que descrevem terceiros, e não a Microsoft, e não foi
verificado aqui). Uma janela de programa comum não passa dele com `SetWindowPos(HWND_TOPMOST)`,
então a pílula pode ficar atrás dele com a correção certa: o teste não distinguiria o código certo
do errado. A revisão de 01-out-2026 apontou isto; a primeira versão da conferência usava o Gerenciador.

**Rejeitado — WebView2:** o runtime Chromium consome tipicamente 80–150 MB. O orçamento do
Agent **inteiro** é 250 MB (`02-prd.md` §7). Gastar metade dele numa pílula de status é
desproporcional, e garantir `NOACTIVATE` + click-through através de um host de browser — que
por natureza quer foco — é mais difícil, não menos.

**Rejeitado — Direct2D:** superior para animação, mas exige subir `ID2D1Factory` e swapchain
DXGI/D3D11 por COM. Mais superfície COM para desenhar poucas dezenas de barras a ~30 fps. Fica
como upgrade se a onda parecer picotada no uso real.

## D3 — `golang.org/x/sys/windows` como dependência direta

**Decisão:** promover de `// indirect` para `require` direto.

O fato decisivo é verificável: **ele já está no grafo de módulos hoje**, puxado
transitivamente por `modernc.org/libc` (o driver SQLite do ADR-008). Promover não baixa artefato
novo e não muda nenhum hash já em `go.sum` — muda o comentário de bookkeeping. Não é
"a primeira dependência de produção"; já está presente e vetada.

Além disso, o pacote `syscall` da stdlib **está congelado para Windows** por política do time
Go: APIs novas entram em `golang.org/x/sys/windows`. É o mais próximo de uma extensão oficial da
stdlib que existe.

**Honestidade sobre o alcance:** o `x/sys/windows` cobre bem kernel32/ntdll/advapi32, mas **não
tem wrappers** para `SetWindowsHookEx`, `CreateWindowEx`, `UpdateLayeredWindow` ou `GetMessage`.
Ainda vamos escrever bindings finos para user32/gdi32/mmdevapi — só que construídos sobre
`NewLazySystemDLL` e `NewCallback`, que já resolvem a parte perigosa (convenção de chamada,
`GetLastError`, tipos de handle), em vez de reimplementar isso sobre a stdlib congelada.

## Modelo de threads

O ponto mais fácil de errar, e o que mata o produto em silêncio se errado.

| Thread | O que roda | Regra |
|---|---|---|
| Hook | `Decider.Decide` (puro) + envio **não bloqueante** a canal | Nunca faz I/O, nunca aloca pesado, nunca espera. O orçamento é ~300 ms; estourar faz o Windows **remover o hook sem avisar** |
| Overlay | `CreateWindowEx` + loop `GetMessage`/`DispatchMessage` | `runtime.LockOSThread()`. Mensagem só chega na thread que criou a janela. Nenhuma outra goroutine toca HWND/GDI |
| Áudio | loop `GetBuffer`/`ReleaseBuffer` | `runtime.LockOSThread()` + `CoInitializeEx`. COM tem apartment threading e o runtime do Go move goroutines entre threads — sem o lock, a chamada COM escapa para a thread errada |
| Runtime | **único consumidor serializado** de `Machine.Handle` | O `Machine` não é thread-safe por projeto (ADR-007). Um único consumidor é obrigação de construção, não recomendação |

## Ring buffer de pre-roll

> **Emendada pela ADR-015 (29-set-2026).** A captura deixou de ser contínua por padrão: o
> dispositivo é aberto e preparado na inicialização, e o `Start` é no keydown — abrir continua na
> subida, o keydown paga só o `Start`. Em repouso a thread de áudio fica bloqueada, sem consultar
> o dispositivo. O pre-roll anterior à marca só existe no modo opcional "sempre pronto" e nunca
> começa antes do início da janela do microfone. O AC-003 passou a ser definido por modo. O ring
> buffer cresceu na revisão adversarial de 29-set-2026: `MaxRecording` + pre-roll não cabia o pior
> caso, porque a marca é no keydown e o limite de gravação só conta depois do limiar; hoje são
> 123,5 s, cerca de 3,95 MB (ADR-015, "O ring cabe o pior caso"). O texto abaixo descreve o
> desenho original.

O `AC-003` ("o início da fala não é perdido") só é verdade se a captura já estiver rodando
quando a tecla desce. Abrir o dispositivo no keydown perde o começo.

- captura contínua desde a inicialização do Agent, gravando num ring buffer circular
- `MarkStart(at)` apenas **marca o ponto de corte**; o buffer devolvido por `Stop` inclui o
  pre-roll anterior à marca
- dimensionado para o pior caso: `MaxRecording` (120 s) + pre-roll, a 16 kHz mono int16 —
  cerca de 3,9 MB, tamanho fixo, alocado uma vez

Assimetria deliberada, já no contrato: `Frames()` é *best-effort* (alimenta a onda do overlay,
pode atrasar sob carga); o buffer de `Stop()` é **autoritativo e sem perdas**.

## Escopo do Sprint 1

**Entra:** hook, áudio com pre-roll, VAD, overlay, beep, o runtime de produção que executa os
`Effect` e devolve os `Event`, e uma **inserção mínima por `SendInput`** (`KEYEVENTF_UNICODE`)
— só para que a fatia seja demonstrável de ponta a ponta.

**Não entra:** o inserter robusto com UI Automation, fallback de clipboard e restauração
(TASK-004); ASR real (depende do benchmark, Sprint 3); Smart/Polish reais.

Transcribe e Resolve seguem stub. Isso é deliberado: o Sprint 1 prova **o núcleo real
alimentado por Win32 real**, não o produto completo.

## Consequência que governa tudo

`Machine` e `Decider` **não mudam uma linha**. O que muda é o runtime em volta deles. Se em
algum momento a implementação pedir para alterar o núcleo ou reescrever um teste da Fase 1,
isso é sinal de que o desenho está sendo violado — pare e reavalie, não adapte o teste.
