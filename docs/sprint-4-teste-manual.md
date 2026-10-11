# Sprint 4 — Roteiro de teste manual

Esta é a primeira vez que o texto inserido é **o que você falou**, e não o
marcador fixo dos sprints anteriores.

Tempo estimado: 15 minutos.

---

## O que mudou

O `stubTranscribe` acabou. O Agent agora sobe um `whisper-server` residente no
início, mantém o modelo carregado, e cada ditado manda o áudio por HTTP em
loopback.

| | |
|---|---|
| modelo | `small` quantizado em `q4_0` |
| latência esperada | p50 ~565 ms, p95 ~658 ms |
| memória residente | ~365 MB, o tempo todo |
| erro esperado | WER 0,26 — cerca de uma palavra errada a cada quatro |

**O WER de 0,26 é do ASR bruto.** `DCTFWeb` vai sair "DCTF Web", `n8n` vai sair
"N8n", datas vão sair por extenso. Isso é esperado nesta fatia: o dicionário,
que existe para corrigir exatamente esses termos, é o Sprint 5.

---

## Como subir

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/asrstage
```

Isso monta `bin/` com o servidor, as DLLs e o modelo, e **confere o checksum**.
Depois:

```bash
go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

O painel não entra no `go build`: é construído à parte, e vai para `bin/painel`. Sem ele o ditado
funciona igual, mas o "Abrir Aurora Whisper" da bandeja mostra a página "O painel não foi
construído" (revisão de 29-set-2026):

```bash
pnpm -C C:/Desenvolvimento/aurora-whisper/web install
```

```bash
pnpm -C C:/Desenvolvimento/aurora-whisper/web build
```

```bash
C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

> **Atualizado em 29-set-2026.** Desde a bandeja (commit 73de148) o Agent é aplicativo de
> bandeja: sem console, encerra pelo menu do ícone → **Sair**, e o log fica em
> `bin\agent.log`. O `-H=windowsgui` é obrigatório nesse caminho — é o executável para o qual o
> início automático aponta, e uma build de console ali abriria uma janela preta a cada logon.
>
> Para acompanhar o log no terminal durante o desenvolvimento, use a build de console com
> **outro nome**, e encerre com `Ctrl+C`:
>
> ```bash
> go -C C:/Desenvolvimento/aurora-whisper build -o bin/aurora-whisper-agent-console.exe ./cmd/agent
> ```
>
> As duas não rodam juntas: a segunda avisa que já há um Aurora Whisper rodando e sai.
>
> **Pela build de console, não mexa em "Iniciar com o Windows".** O item compara o registro
> com o próprio executável: nela ele aparece desmarcado mesmo com o início automático ligado, e
> clicado aponta o início automático para a build de console — a janela preta a cada logon que o
> `-H=windowsgui` existe para evitar (revisão de 29-set-2026). A correção da mesma revisão deixa
> o item desabilitado nessa build, com "(só na versão de bandeja)"; se ele aparecer clicável, a
> build é anterior à correção.

### O que muda de uma build para a outra

O resto deste roteiro foi escrito em 20-set-2026, quando o Agent só existia com terminal.
Revisado em 29-set-2026 para a build de bandeja, que é o caminho principal acima:

| | build de bandeja (`aurora-whisper-agent.exe`) | build de console (`aurora-whisper-agent-console.exe`) |
|---|---|---|
| encerrar | menu do ícone → **Sair** | `Ctrl+C` no terminal, ou o mesmo **Sair** |
| log | só em `bin\agent.log` | em `bin\agent.log` **e** no terminal |
| "Iniciar com o Windows" | pode usar | não use (acima); com a correção, aparece desabilitado |

Para acompanhar o `agent.log` enquanto testa, deixe uma janela do PowerShell com o comando
abaixo (depois da primeira subida: antes dela o arquivo não existe):

```bash
Get-Content C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Tail 40 -Wait
```

É só do PowerShell. O `-Encoding UTF8` é necessário: sem ele o PowerShell 5.1 lê o arquivo como
ANSI e os acentos saem trocados. `Ctrl+C` nessa janela fecha só a leitura do log, não o Agent.

> **A primeira subida demora.** O Agent carrega o modelo antes de aceitar
> qualquer ditado, de propósito: pagar isso no primeiro uso faria a primeira
> fala da sessão parecer travada. Na build de bandeja, espere o **ícone aparecer na
> bandeja**: ele só é criado depois de o modelo carregar. A linha
> `agent: rodando` sai no `bin\agent.log` (e, na build de console, também no terminal).
>
> O painel tem roteiro próprio: `docs/teste-manual-bandeja-e-painel.md`.

---

## Roteiro

> **A tecla de ditado é `Ctrl+Win`** desde 20-set-2026 (`ADR-013`). A execução de 20-set
> registrada no fim deste roteiro usou `F`, o gatilho da época; os passos abaixo foram
> atualizados para `Ctrl+Win` na revisão de 29-set-2026.
>
> **Espere o bipe antes de falar** desde 29-set-2026 (`ADR-015`): o microfone só liga quando
> você segura `Ctrl+Win`, e o bipe (junto com a pílula "Gravando") é o sinal de que ele já está
> captando. Quem fala em cima da tecla perde a primeira sílaba — é o custo aceito, não um
> defeito para anotar aqui. O que acontece na primeira sílaba tem roteiro próprio: parte 6 de
> `docs/teste-manual-bandeja-e-painel.md`.

### T18 — Ditado de verdade ⚠️ o teste que define o sprint

**Faça:** no Bloco de Notas, segure `Ctrl+Win`, espere o bipe, fale **"testando o ditado por
voz"**, solte.

**Esperado:** aparece o que você falou. Não o marcador, não texto fixo.

**Falhou se:** aparecer o texto antigo `(aurora-whisper: transcricao real
pendente...)` — significa que o binário é velho; recompile.

### T19 — A latência é a medida?

**Faça:** três ditados curtos seguidos, contando mentalmente.

**Esperado:** o texto aparece em cerca de meio segundo depois de você soltar a
tecla. Não instantâneo, mas sem parecer travado.

**Me reporte a sensação**, não o número: "imediato", "dá pra perceber", "demora
demais". O p50 medido foi 565 ms contra a meta de 500 ms, e saber como isso
**parece** vale tanto quanto o número.

### T20 — A digitação continua intacta durante a transcrição ⚠️ o segundo mais importante

**Faça:** segure `Ctrl+Win`, espere o bipe, fale uma frase longa, solte, **e imediatamente comece a
digitar** `abcdefghij` sem esperar o texto aparecer.

**Esperado:** as dez letras saem todas, na ordem, sem atraso. O texto ditado
aparece no meio ou depois — a ordem entre os dois não importa.

**Falhou se:** faltar letra, ou a digitação travar enquanto a transcrição
acontece.

> Este teste existe porque a transcrição roda em outra thread de propósito. Se
> ela rodasse no laço de eventos, as teclas ficariam paradas por meio segundo —
> e o hook do teclado, que tem orçamento de ~300 ms por callback, poderia ser
> **removido pelo Windows em silêncio**, matando o ditado sem nenhum erro
> visível. É a falha mais difícil de diagnosticar do projeto inteiro.

### T21 — Termos técnicos (o que o dicionário vai consertar)

**Faça:** dite `precisa transmitir a DCTFWeb antes do dia quinze`.

**Esperado:** o texto aparece, mas com `DCTFWeb` provavelmente separado em
"DCTF Web", e "quinze" possivelmente como "15".

**Me reporte o que saiu exatamente.** Não é falha desta fatia — é o insumo para
o dicionário do Sprint 5. Quanto mais exemplos reais, melhor.

### T22 — Campo de senha continua bloqueado (regressão do T12)

**Faça:** numa página de login no Chrome, clique no campo de **senha** e segure
`Ctrl+Win` por 2 segundos. **Não digite senha de verdade.**

**Esperado:** overlay com "Ditado desativado em campo protegido", nenhuma
gravação, nenhuma transcrição.

**Falhou se:** gravar. Seria voz capturada num campo de senha, com um ASR
ligado — bem pior que no Sprint 2.

### T23 — O servidor de ASR não fica órfão ⚠️ o que eu mais quero que você verifique

**Faça, nesta ordem exata:**

1. com o Agent rodando, abra o **Gerenciador de Tarefas**
2. confirme que existe um `whisper-server.exe` na lista
3. **mate o `aurora-whisper-agent.exe` pelo Gerenciador** (Finalizar tarefa) — não
   use o **Sair** do menu, nem `Ctrl+C` na build de console: os dois são o
   encerramento limpo, e aqui o teste é o encerramento à força
4. olhe a lista de novo

**Esperado:** o `whisper-server.exe` **sumiu junto**. O ícone pode continuar na
bandeja até você passar o mouse por cima dele: é o Windows, que só descobre
nessa hora que o dono do ícone morreu — não é falha deste teste.

**Falhou se:** ele continuar lá. Seriam 365 MB segurados para sempre por um
processo invisível, e a cada vez que você abrisse o Agent apareceria mais um.

> Isso é garantido por Job Object do Windows, e tem teste automatizado que mata
> o processo para provar. Mas teste automatizado prova o mecanismo; este prova
> o produto.

### T24 — Encerramento limpo

**Faça:** menu do ícone na bandeja → **Sair**. Confira o Gerenciador de Tarefas.
Na build de console, `Ctrl+C` no terminal faz o mesmo caminho: o Sair cancela o
mesmo contexto do `Ctrl+C` (`cmd/agent/main.go`).

**Esperado:** o ícone some da bandeja, os dois processos
(`aurora-whisper-agent.exe` e `whisper-server.exe`) saem, e o `bin\agent.log`
registra `tray: encerrando a pedido de quem usa`, sem `WARN` nem `ERROR` depois.
Segurar `Ctrl+Win` no Bloco de Notas não grava mais nada, e o teclado segue
normal — nenhum modificador preso.

> Em 20-set-2026 este teste foi executado com `Ctrl+C` e a tecla `F`: o
> esperado era "segurar F volta a digitar `fffff`".

---

---

## T25 — A onda do overlay (26-set-2026)

**Faça:** segure `Ctrl+Win`, espere o bipe, fale uma frase com volume variado — comece baixo,
levante a voz no meio, termine baixo — e **olhe a pílula** em vez do texto.

**Esperado:** a onda é espelhada em torno de uma linha central, as barras têm
ponta arredondada, e a altura **acompanha a voz de forma óbvia** — barra baixa e
escura nas pausas, barra alta e clara nas sílabas fortes. Dá para ver o contorno
da frase, com os buracos entre as palavras.

A pílula também está **25% menor** (420x60, contra 560x80).

**Me reporte qual dos três:**

| | o que fazer |
|---|---|
| "agora sim" | nada |
| "ainda tímido" | abaixo `WaveCeilDBFS` |
| "agora satura, fica alta o tempo todo" | subo `WaveCeilDBFS` |

Cada barra resume 39 ms, contra 256 ms antes — é isso que faz a onda desenhar a
frase em vez de virar uma cerca de barras iguais (ver `ADR-014`).

> **A cor continua provisória**, e a fonte também. Cinza sobre quase-preto, com
> um bitmap 5x7 sem suavização. O `ADR-010` adiou a identidade visual para o
> Sprint 2 e isso nunca foi feito. Mudei forma e tamanho, de propósito, para não
> escolher paleta por você — a conversa sobre o design ficou marcada.

### T25b — As mensagens longas, agora em duas linhas

**Faça:** dite num campo de senha (ver `T22`) para ver a mensagem de bloqueio.

**Esperado:** "Ditado desativado em campo protegido." aparece **inteira**, em
duas linhas centralizadas. Nada de "…" no fim.

Você autorizou encurtar a mensagem se ela atrapalhasse o tamanho — não foi
preciso. A quebra em duas linhas encolheu a pílula mantendo o texto da spec.

---

## T26 — O menu Iniciar abre? (pendente desde o ADR-013)

**Faça:** com o foco no Bloco de Notas, segure `Ctrl+Win` por ~2 s, fale depois do bipe, e
**solte as duas teclas ao mesmo tempo**. Repita soltando o `Ctrl` primeiro.

**Esperado:** não sei — é exatamente o que preciso descobrir.

O `ADR-013` decidiu **nunca suprimir** tecla neste modo, porque suprimir keyup de
modificador já travou o teclado desta máquina em 17-set-2026. O preço aceito foi
o menu Iniciar poder abrir. Se abrir, mitigo com dado; se não abrir, o assunto
morre. **Me diga o que aconteceu nas duas formas de soltar.**

---

## O que me reportar

Para **T18** a **T24**: passou ou falhou, e o que você viu nos que falharem.

Do **T21**, o texto exato que saiu — vira insumo do dicionário.

Do **T19**, a sensação de latência em palavras suas.

E as últimas linhas do `bin\agent.log`, principalmente se houver `WARN` ou
`ERROR` (na build de console, as mesmas linhas saem também no terminal). O
Agent agora registra cada transcrição com o modelo e a duração.

> **Teste a pílula com a tela COBERTA.** Maximize uma janela antes de ditar. Até
> 26-set-2026 o overlay não era `TOPMOST` e ficava atrás da janela em foco — o
> defeito só não aparecia quando o centro inferior da tela estava descoberto, e
> foi assim que ele passou por dois roteiros manuais. Ver
> `defeito-overlay-atras-da-janela-em-foco.md`.

> **Se você não vir o overlay aparecer**, olhe o `bin\agent.log` (ou o terminal,
> na build de console): desde 26-set-2026 o Agent registra cada mudança de fase
> do overlay — `winruntime: overlay attempt=1 fase=recording`. Isso separa dois
> problemas que antes davam o mesmo log (nenhum):
>
> | o que você vê no `agent.log` | o que significa |
> |---|---|
> | nenhuma linha `overlay` | o gatilho **não foi detectado** — o problema é o `Ctrl+Win` |
> | `fase=recording` mas nada na tela | o gatilho funcionou, a **janela** é que não apareceu |

---

## O que este roteiro NÃO prova

- **Qualidade da transcrição em uso real prolongado.** O WER de 0,26 foi medido
  em 46 frases lidas; falar naturalmente, com hesitação e correção, é outro
  regime.
- **O dicionário**, que não existe ainda (Sprint 5). Sem ele a Entity Accuracy
  bruta é 57%.
- **Recuperação de morte do servidor em uso real.** Há teste automatizado, mas
  matar o `whisper-server` no meio de um ditado de verdade é um cenário que
  ninguém exercitou fora do laboratório. Se quiser tentar, mate só o
  `whisper-server.exe` (não o Agent) e dite de novo — o esperado é que o
  próximo ditado funcione, depois de uma pausa.
- **Uso prolongado.** 365 MB residentes por horas, com o Agent aberto o dia
  inteiro, é outro tipo de teste.
- **A tecla fora de ordem** (`freio` → `refio`) foi **resolvida** em 20-set-2026
  pela troca de gatilho para `Ctrl+Win` — ver `adr/ADR-013-*` e
  `defeito-ordem-da-tecla-de-ditado.md`.
- **A identidade visual do overlay**, que o `ADR-010` adiou para o Sprint 2 e
  nunca foi feita. O `T25` mexe na forma da onda, não na cor.

---

## Resultado da execução

**20 de setembro de 2026** — executado por Mauro na máquina de referência.

**Todos os sete testes passaram (T18 a T24).**

O que isso estabelece, e nenhum teste automatizado conseguiria:

- **T18** — o ditado por voz funciona ponta a ponta na máquina real. Segurar a
  tecla, falar, soltar, e o que a pessoa disse aparece no campo com foco. É o
  produto existindo pela primeira vez.
- **T20** — a digitação continua intacta enquanto a transcrição acontece. Era a
  falha mais difícil de diagnosticar do projeto: se a transcrição bloqueasse o
  laço de eventos, o Windows removeria o hook em silêncio e o ditado morreria
  sem nenhum erro visível. A decisão de rodar o ASR em outra goroutine está
  validada fora do laboratório.
- **T23** — o `whisper-server.exe` morre junto quando o Agent é morto pelo
  Gerenciador de Tarefas. O Job Object funciona no produto montado, não só no
  teste que fecha o handle. Sem isso seriam 365 MB segurados para sempre, e
  mais um processo invisível a cada abertura.
- **T22** — o bloqueio em campo de senha continua valendo agora que existe um
  ASR de verdade ligado. A regressão aqui teria sido bem pior que no Sprint 2.

Continua valendo tudo da seção "o que este roteiro NÃO prova" — em especial
que o dicionário não existe, e que uso prolongado com 365 MB residentes é
outro tipo de teste.

---

## Execução de 26 de setembro de 2026 — T25 e T26

**T25 (a onda) — passou.** Mauro: *"no notepad rodou perfeitamente. Eu gostei
mais do tamanho da wave assim do que antes."* `WaveCeilDBFS` não precisou de
ajuste: não saturou nem ficou tímida.

Chegar a esse resultado exigiu três defeitos corrigidos no mesmo dia, nenhum
deles da onda:

1. **O overlay era desenhado atrás da janela em foco** — faltava
   `WS_EX_TOPMOST`. Sintoma: "eu ouço o som mas não vejo nada na tela". Ver
   `defeito-overlay-atras-da-janela-em-foco.md`.
2. **O foco que já existia quando o Agent subia nunca era inspecionado** — o
   cache começa em `Protected: true` e só era atualizado por *mudança* de foco.
   Sintoma: "Ditado desativado em campo protegido" numa janela comum.
3. **O microfone padrão do Windows entregava silêncio** (pico de −80,8 dBFS).
   Configuração da máquina, não código — mas só foi possível ver isso depois que
   o `no_speech` passou a registrar o nível captado.

**T26 (o menu Iniciar) — continua sem resposta.** É a última pendência do
`ADR-013`.

**T21 — RESOLVIDO em 27-set-2026, no Sprint 5.** Mauro ditou *"precisa
transmitir a DCTFWeb antes do dia quinze"* e o campo recebeu
`Precisa transmitir a DCTFWeb antes do dia 15.`

`DCTFWeb` saiu correto. A previsão registrada neste roteiro era que sairia
"DCTF Web" — e saía, até o dicionário existir. Ver
`sprint-5-portao-de-entidades.md`.
