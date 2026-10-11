# TASK-009 — desenho da captura de contexto (proposta)

Status: **PROPOSTA — não implementado**
Data: 27-set-2026

Este documento existe para ser revisado **antes** de qualquer código. A captura
de contexto lê o texto ao redor do cursor de quem usa: é a superfície mais
sensível do produto inteiro, e um erro aqui não é um bug, é um vazamento.

## Por que agora

O Sprint 5 parou em 81,6% de Entity Accuracy contra meta de 95%. Os pontos que
faltam exigem alias que colidem com palavra comum do português:

| alias | canônico | o que quebraria |
|---|---|---|
| `comite` | `commit` | "o **comitê** aprovou" → "o commit aprovou" |
| `DRA` | `DRE` | "**Dra.** Silva" → "DRE Silva" |
| `postgres` | `PostgreSQL` | **postgres** é termo legítimo |

`DictionaryEntry` já tem **`Domain`**, e `commit` já está marcado como `dev`. Com
a janela em foco conhecida, esse alias dispararia só num editor de código — e
nunca num e-mail sobre o comitê.

**O contexto não é enfeite do Sprint 5: é o que torna a correção agressiva
segura.**

## O que já existe

| peça | estado |
|---|---|
| `uiacontext.ContextProvider` (contrato) | pronto |
| `ContextSnapshot` + limites | pronto |
| `MarshalJSON` sempre falha; `String()` redige | pronto |
| `FakeContextProvider` | pronto |
| `resolver` consome contexto (`contextCorroborates`) | pronto |
| `uia.Tracker` — foco, app, controle, campo protegido | pronto |
| **implementação Windows de `ContextProvider`** | **não existe** |
| **texto próximo ao cursor / seleção** | **não existe** |

Não há nenhum `TextPattern` nem `ValuePattern` em nenhum lugar do código hoje.

## Decisão 1 — QUANDO capturar

**Em paralelo com a transcrição, não no caminho crítico.**

Contexto capturado na troca de foco ficaria velho: a pessoa troca de janela e
digita mais um parágrafo antes de ditar. Capturar depois do release seria fresco,
mas somaria latência de UIA (milissegundos a centenas) ao `release→insert`, que
já está acima da meta.

A transcrição já roda ~560 ms em outra goroutine. **A captura de contexto cabe
inteira dentro dessa janela, de graça.** Quando o texto do ASR chega, o contexto
já está pronto.

Se a captura estourar o orçamento, o ditado segue **sem contexto** — nunca
espera por ela.

## Decisão 2 — em QUAL thread

Na thread do `uia.Tracker`, que já é dona do apartment COM (ADR-011, decisão 2 e
a mesma regra do WASAPI em ADR-010 D1). O `Runtime` pede por canal e recebe por
canal; nenhuma chamada COM sai da thread dona.

## Decisão 3 — QUAIS padrões UIA

Em ordem, parando no primeiro que responder:

1. **`IUIAutomationTextPattern`** — documentos ricos (Word, navegador, editores).
   Dá seleção e o texto ao redor do caret.
2. **`IUIAutomationValuePattern`** — caixas de edição simples. Dá o valor
   inteiro, sem posição de caret.
3. **Nada.** Contexto ausente é um resultado válido; o resolver já degrada.

## Decisão 4 — o que NUNCA acontece

Estas não são precauções teóricas. Cada uma corresponde a uma linha de
`09-security-privacy.md` ou do `CLAUDE.md`:

| regra | como é garantida |
|---|---|
| **Campo protegido nunca é lido** | o portão é `uia.Snapshot.Protected()`, que já trata falha/timeout/nulo como protegido. A captura nem começa. |
| **Contexto nunca vai para log** | `ContextSnapshot.String()` já redige. O tipo bruto lido da UIA precisa da mesma trava — é o furo que o relatório do levantamento apontou. |
| **Contexto nunca é persistido** | `MarshalJSON` já falha sempre. Nada de contexto entra em SQLite/Postgres. |
| **Contexto é limitado** | `MaxNearCaretRunes=200`, `MaxSelectionRunes=500`. O truncamento acontece **antes** de o texto sair da thread de UIA, não depois — um documento inteiro não pode nem chegar a existir em outro lugar. |
| **Contexto é dado, nunca instrução** (THR-008) | o resolver só faz substring match e **jamais** introduz candidato novo a partir do contexto — só corrobora um que o dicionário já levantou. Isso já está implementado e testado. |
| **Sem screenshot** | `noscreenshot_test.go` varre a AST por token proibido. Precisa passar a valer também para o pacote novo. |

## Decisão 5 — o que o contexto vai DECIDIR

Só uma coisa, no começo: **liberar alias por domínio**.

O `SmartResolver` hoje auto-aplica match `exact` e `alias`, e trata `phonetic`
como evidência fraca. A proposta **não** afrouxa isso. O que muda é que um alias
marcado com `Domain: "dev"` passa a exigir que o app em foco seja de
desenvolvimento para ser aplicado.

Ou seja: o contexto entra **restringindo**, não ampliando. Um alias perigoso
passa a disparar menos, não mais.

Como o app em foco vira domínio é a única parte que precisa de heurística, e ela
vai ser explícita e configurável — não uma lista escondida no código.

## O que pode dar errado

1. **UIA trava em alguns apps.** Chamada COM sem timeout pendura a thread. O
   `Tracker` já tem `InspectBudget`; a captura precisa do mesmo orçamento,
   medido de fora.
2. **Documento gigante.** `TextPattern` de um Word de 200 páginas pode devolver
   tudo. Por isso o truncamento acontece na thread de UIA, antes de qualquer
   cópia.
3. **App que mente.** O contexto é dado não confiável por definição — e o
   resolver já o trata assim.
4. **Custo de latência invisível.** Rodar em paralelo esconde o custo até o dia
   em que a captura demora mais que a transcrição. Vai ter log de duração, como
   o resolve tem.

## O que eu preciso de Mauro antes de implementar

1. **Concorda com "em paralelo com a transcrição"?** É o que evita somar latência
   num caminho que já está acima da meta.
2. **Concorda que o contexto entra RESTRINGINDO?** A alternativa — contexto
   ampliando o que pode ser corrigido — é mais poderosa e muito mais perigosa.
3. **Como o app em foco vira domínio?** Proposta: uma lista explícita no próprio
   dicionário (`"apps": ["Code.exe", "devenv.exe"]` por domínio), editável por
   ele. Nunca uma heurística escondida.
