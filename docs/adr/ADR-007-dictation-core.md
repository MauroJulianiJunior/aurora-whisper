# ADR-007 — Núcleo do ditado: redutor puro com borda síncrona

Status: Accepted
Data: 2026-09-16
Contexto: Fase 1, bloco F1-A

## Contexto

A Fase 1 valida contratos com mocks antes de qualquer código nativo. O núcleo do ditado
(tap vs hold, captura, inserção) precisa ser exercitável por eventos sintéticos e relógio
falso, de forma 100% determinista, e a **mesma suíte de testes precisa continuar verde no
Sprint 1**, quando o hook Win32, o WASAPI e a UI Automation reais entrarem. Se a troca de
mock por implementação real exigir reescrever os testes, a Fase 1 não terá provado nada.

Três desenhos independentes foram produzidos, com vieses opostos (pureza, fidelidade ao
Win32, minimalismo). Esta ADR é a síntese.

## Decisão

### 1. O núcleo é um redutor puro

```go
func (m *Machine) Handle(ev Event) []Effect
```

Todo estímulo entra como dado (`Event`); toda ação sai como dado (`Effect`). O núcleo não
executa I/O, não cria goroutine, não chama `time.Now()`. Quem executa os efeitos é um
runtime externo — fake na Fase 1, Windows real no Sprint 1.

Efeitos que precisam de resposta (`Transcribe`, `Resolve`, `Insert`, `StopAudioCapture`)
são pareados com um `*ResultEvent` que o runtime devolve ao `Handle`.

### 2. A borda do hook é síncrona e separada do núcleo

O callback do `WH_KEYBOARD_LL` roda numa thread do sistema com orçamento de tempo curto
(`LowLevelHooksTimeout`, tipicamente 300 ms). Estourar esse orçamento faz o Windows
**remover o hook silenciosamente** — o que cairia direto na regressão proibida "hook
bloquear teclado inteiro".

Por isso o `Decider` é separado do `Machine`:

```go
// Decider é puro e síncrono. Responde apenas "suprimo ou deixo passar?"
// Nunca faz I/O, nunca bloqueia, nunca consulta UI Automation.
func (d *Decider) Decide(ev RawKeyEvent) Decision
```

`Machine` (o trabalho pesado) roda fora dessa thread, alimentado por um canal.

### 3. Eventos injetados são filtrados

O tap sintetizado por `SendInput` volta para o nosso próprio hook. Sem filtro, isso é um
laço infinito. Portanto `RawKeyEvent` carrega, desde já:

```go
type RawKeyEvent struct {
    Key             KeyCode
    Modifiers       Modifiers
    Down            bool
    At              time.Time
    Injected        bool // veio de SendInput (qualquer processo)
    FromThisProcess bool // fomos nós que injetamos
}
```

Evento com `Injected && FromThisProcess` é sempre `PassThrough` e nunca inicia sessão.

> Gap conhecido e aceito: evento injetado por outro processo (ex.: AutoHotkey) é tratado
> como físico. Documentado, não corrigido agora.

### 4. Áudio tem pre-roll; a captura já está rodando antes do keydown

> **Emendada pela ADR-015 (29-set-2026).** O padrão passou a ser o microfone **só enquanto
> dita**: o dispositivo continua aberto e preparado na inicialização, mas o `Start` acontece no
> keydown (`MarkStart`) e o microfone estaciona depois do corte. O pre-roll anterior à marca só
> existe no modo opcional "sempre pronto", e nunca atravessa uma parada do microfone. O AC-003
> passou a ser definido por modo. O texto abaixo descreve o desenho original, que hoje é o
> "sempre pronto".

Abrir o dispositivo no keydown é lento e perde o início da fala. Para o **AC-003** ser
demonstrável, a captura roda continuamente num ring buffer, e o keydown apenas marca o
ponto de corte:

```go
type AudioSource interface {
    // Start abre o dispositivo e começa a alimentar o ring buffer de pre-roll.
    // É chamado na inicialização do Agent, NÃO no keydown.
    Start(ctx context.Context, cfg AudioConfig) (AudioSession, error)
}

type AudioSession interface {
    // MarkStart marca o instante do keydown. O buffer devolvido por Stop
    // inclui PreRoll de áudio ANTERIOR a esta marca.
    MarkStart(at time.Time) AttemptID
    Frames() <-chan AudioFrame // ao vivo, best-effort, pode perder frame
    Stop(ctx context.Context, id AttemptID) (AudioBuffer, VADSummary, error) // autoritativo
    Abort(id AttemptID)
    Close() error
}
```

Assimetria deliberada: `Frames()` é best-effort (alimenta a onda do overlay, pode atrasar);
o buffer de `Stop()` é autoritativo e sem perdas.

### 5. Toda tentativa tem identidade; eventos velhos são descartados

Um timer atrasado da tentativa anterior não pode promover a tentativa atual. Todo `Event`
carrega `AttemptID`, e o `Machine` descarta evento cujo `AttemptID` não seja o corrente.
Isso mata a corrida por construção, em vez de por sorte de escalonamento.

### 6. Todo estado que espera algo de fora tem timer armado pelo próprio núcleo

O núcleo não pode depender de o runtime sempre devolver um resultado — um runtime com bug
travaria a sessão em `processing` para sempre. Portanto, ao emitir um efeito que aguarda
resposta, o núcleo **sempre** emite junto um `StartTimerEffect` de timeout. A garantia de
retorno a `idle` é do núcleo, não do runtime.

> **Correção de 2026-09-17.** A formulação original desta seção dizia "todo *efeito* que espera
> resposta", e isso deixou um buraco: `StateRecording` não espera resposta de nenhum efeito —
> espera o **keyup**, que é um evento externo que pode simplesmente nunca chegar (tecla presa,
> app com foco morto, keyup perdido). A primeira implementação entrou em `StateRecording` sem
> armar timer nenhum e a sessão travava para sempre; pior, novos keydowns eram descartados, o
> que matava a tecla de ditado até reiniciar o processo — exatamente a regressão proibida
> "hook bloquear teclado inteiro". Encontrado pela auditoria adversarial.
>
> A regra correta é mais ampla: **todo estado que depende de algo vindo de fora precisa de um
> timer de saída armado pelo núcleo**. Para `StateRecording` esse timer é o `TimerMaxRecording`
> (`Config.MaxRecording`, default 120 s, conforme `07-flows.md`).
>
> Ao expirar, ele encerra a captura pelo **mesmo caminho do keyup**, em vez de descartar o
> áudio. Assim o VAD decide se havia fala de verdade: tecla presa em silêncio não insere nada
> (AC-010), e uma fala longa legítima não é jogada fora.

Consequência prática para quem for mexer no núcleo: ao introduzir um estado novo, a pergunta
obrigatória é "qual evento **ou timer** garante que se sai daqui?". Se a única resposta for
"o runtime vai mandar", o estado está errado.

> **Nota de 2026-10-01: o prazo da transcrição acompanha o áudio.** Até aqui o
> `TimerTranscribeTimeout` era fixo em 10 s (`DefaultTranscribeTimeout`). Numa aula pelo Meet,
> um ditado de 37 s levou 18,8 s no ASR e um de 89 s levou 16,1 s: o watchdog encerrou as duas
> tentativas e a transcrição chegou depois, já sem dono (`docs/defeito-ditado-longo.md`).
>
> O `AudioCapturedEvent` ganhou o campo `Duration`, a duração do áudio capturado (das amostras,
> não do relógio da tecla), e o núcleo arma o timer com `PrazoDeTranscricao(Duration)`:
> 5 s + 0,75 × duração, entre 10 s e 90 s. O fator é a pior razão ASR/áudio medida (0,51) com
> 1,5× de folga; o teto cobre 120 s de áudio (o `MaxRecording` padrão) nessa razão com 1,47×.
> Duração zero quer dizer desconhecida e dá o piso, que é o prazo de antes: o campo é aditivo, e
> quem não o preenche continua como estava. A garantia desta seção não muda: o timer continua
> sendo armado pelo núcleo, junto com o `TranscribeEffect`, e só a duração dele passou a
> depender do áudio. O custo é a tecla presa por mais tempo quando o servidor trava (o keydown
> durante `processing` é ignorado), até 90 s no pior caso.
>
> Se o prazo ainda assim estourar, a transcrição que chega depois **não vai ao núcleo**: a seção 5
> continua valendo (ele a descartaria). Quem a pega é o runtime, antes de despachar
> (`winruntime/tardio.go`): grava o texto na linha do histórico daquela tentativa, sem inserir
> nada, e o desfecho dela continua `timeout`. O núcleo não muda.

> **Nota de 2026-10-01 (segunda): o prazo da resolução acompanha o texto.** O
> `TimerResolveTimeout` era fixo em 5 s (`DefaultResolveTimeout`). No modo com IA, o Polish tinha
> 3,5 s dentro desses 5 s, e no ditado longo não cabia: um de 802 caracteres caiu no Smart por
> 1 ms. O Mauro decidiu que o prazo do Polish cresce com o texto, até 8 s
> (`docs/defeito-ditado-longo.md`, decisão 5; ADR-016, "Emenda de 01-out-2026"). O watchdog tinha
> de crescer junto: se ele vencer antes do Polish, a tentativa acaba **sem texto**, e o ditado
> inteiro se perde.
>
> O núcleo arma o timer com `PrazoDeResolucao(e.Result.Text)` em `onTranscribeResult`: 5 s mais
> `AcrescimoPeloTexto`, que é zero até 250 caracteres (runas), 6 ms por caractere acima disso, e
> no máximo 4,5 s (aos 1000). De 5 s a 9,5 s. O runtime soma o **mesmo** acréscimo, sobre o
> **mesmo** texto (o do `ResolveEffect`), aos 3,5 s do Polish; a folga de 1,5 s fica constante por
> construção, e é por isso que o acréscimo mora aqui: o núcleo é folha e não importa o runtime.
>
> Segue o padrão da nota anterior, com uma diferença: o `AudioCapturedEvent` precisou de um campo
> `Duration`, porque o núcleo só via uma referência ao áudio; o `TranscribeResultEvent` já traz o
> texto, e o tamanho é medido nele. Um campo à parte com o tamanho seria redundante e poderia
> divergir do texto que segue no `ResolveEffect`, que é o que o runtime mede. Texto vazio ou curto
> dá o piso, que é o prazo de antes: a fala curta não muda.
>
> A garantia da seção 6 continua: o timer é armado pelo núcleo, junto com o `ResolveEffect`, e só
> a duração passou a depender do texto. A seção 5 também: uma resposta que chega depois do
> watchdog, ou de uma tentativa já encerrada, é descartada. O custo é a tecla presa por mais tempo
> se a resolução nunca responder (o keydown durante `processing` é ignorado): até 9,5 s, e não mais
> 5 s. Nos modos Literal e Smart, que respondem em milissegundos, o prazo maior só pesa nesse caso.

### 7. Um único ponto de saída libera o áudio

Todos os desfechos (sucesso, erro, blocked, watchdog, tap curto) passam pelo mesmo
`endAttempt()`, que emite `ReleaseAudioEffect`. É isso que torna o **TEST-007** uma prova
verificável por teste, e não uma promessa em comentário.

### 8. Relógio injetado, com fake que dispara síncrono

```go
type Clock interface {
    Now() time.Time
    AfterFunc(d time.Duration, f func()) Timer
}
```

O `Fake` dispara os callbacks **de dentro do `Advance()`**, em ordem cronológica. Testes não
têm `sleep` nem polling, e conseguem exercitar ordenações críticas (ex.: watchdog e keyup
competindo).

### 9. Campo protegido bloqueia o ditado, não a tecla

O `AC-036`/`REQ-046` mandam bloquear o ditado em campo protegido. A leitura ingênua — encerrar
a tentativa no keydown — cria um bug sério: o `Decider` já suprimiu a tecla na borda do hook,
então, sem `SynthesizeTap`, o caractere **some**. Com a tecla default `F`, o usuário ficaria
incapaz de digitar a letra `f` em qualquer campo de senha.

O comportamento correto separa as duas coisas:

| Situação | Comportamento |
|---|---|
| keydown em campo protegido | `StateBlocked`, timer de threshold armado, **nenhum áudio** (não há `MarkAudioStart` neste caminho) |
| keyup antes do threshold | `SynthesizeTap` + fim como `OutcomeTap` — a tecla digita normalmente (AC-001) |
| threshold expira | `ShowOverlay{OverlayBlocked}` + fim como `OutcomeBlocked` — nenhum áudio foi capturado em momento algum (AC-036) |

**Blocked não vai para o histórico.** Registrar "tentou ditar aqui" carregaria metadado sobre
uso de campo de senha, o que contraria a postura de privacidade da spec (AC-027). O usuário já
recebe o retorno pelo overlay; nada persiste.

## O que foi rejeitado

| Rejeitado | Por quê |
|---|---|
| Núcleo sem relógio, com o teste chamando `HandleThresholdExpired` na mão (Minimalismo) | Não consegue testar **ordenação** entre threshold, recording-max e watchdog — que é justamente onde moram os bugs |
| Cortar o VAD do núcleo (Minimalismo) | `REQ-008`/`AC-010` exigem validação de fala. Mantido, mas mínimo: só `VADSummary` no resultado da captura; streaming/partial fica para depois |
| `FocusLocator` como interface nova (Win32) | Desvia do `18-tool-contract.md`. Na F1-A o `FocusTarget` chega pronto como dado no `KeyDownEvent`; *como* obtê-lo é problema do Sprint 1 |
| Interfaces para overlay e beep | São `Effect` (dado), não colaboradores. Nenhuma interface nova |

## Consequências

- O núcleo é uma leaf package: importa apenas a stdlib.
- `Machine.Handle` **não é thread-safe por projeto**. O runtime precisa garantir um único
  consumidor serializado. Isso é obrigação de construção, não recomendação.
- O runtime precisa traduzir os tipos ricos (`asr.ASRResult`, `resolver.ResolveResult`) para
  os DTOs mínimos que o núcleo usa para decidir. Atrito aceito em troca de um núcleo isolado.
- No Sprint 1, o que muda é **apenas o runtime**: `Decider` e `Machine` ficam idênticos, e a
  suíte de aceite continua verde. Se não continuar, é bug do Sprint 1, não do desenho.

## Pendência para o Sprint 1

`KBDLLHOOKSTRUCT.time` é um contador de ticks de 32 bits desde o boot, com overflow a cada
~49,7 dias — não é hora de parede. A conversão para `time.Time` precisa ancorar num par
(tick de referência, wall time de referência) capturado na instalação do hook, e tratar o
wraparound. A Fase 1 **não** pega esse bug, porque o evento sintético já entrega `time.Time`
pronto. Registrado aqui para não se perder.
