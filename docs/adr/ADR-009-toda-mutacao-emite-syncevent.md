# ADR-009 — Toda mutação aceita pelo servidor emite um SyncEvent

Status: Accepted (invariante declarada; implementação pendente no Sprint 6)
Data: 2026-09-17
Contexto: Fase 1, fechamento do bloco F1-E

## Contexto

O servidor tem **duas portas de entrada de escrita**, e isso é proposital:

| Porta | Quem usa | Por quê |
|---|---|---|
| CRUD tipado (`PATCH /transcriptions/{id}`, `POST /dictionary`, …) | o dashboard web | é um navegador; fala REST, está online, quer resposta imediata |
| `POST /sync/push` | o Windows Agent | precisa funcionar offline, com outbox e idempotência |

O problema descoberto no fechamento do F1-E: **só a porta de sync grava `SyncEvent`**. O CRUD
tipado escreve no seu próprio repositório e não registra nada no log de eventos.

## Por que isso importa

`GET /sync/pull` devolve `SyncEvent`. Se uma mutação não gera evento, ela **nunca é vista por
nenhum outro dispositivo**. Na prática: a pessoa edita um ditado no dashboard cloud, e a edição
jamais chega ao Windows. O `AC-016` ("Edição cloud retorna ao Windows") falharia na vida real.

O que tornou isso difícil de enxergar: o teste do `AC-016` **está verde**. Ele chama o segundo
device de `dev-dashboard`, o que faz o cenário parecer uma edição de dashboard, mas a edição é
feita por `Client.Push(...)` — ou seja, por um segundo cliente de sync, não pela rota REST que
um navegador usaria. O teste prova o caminho sync→sync, que é legítimo e vale ter, mas **não é
o caminho que o critério descreve**.

É um bom lembrete de que teste verde prova o que ele exercita, não o que o nome dele promete.

## Decisão

**O log de `SyncEvent` é o único mecanismo de propagação entre dispositivos. Qualquer mutação
aceita pelo servidor — por qualquer porta de entrada — precisa acrescentar um `SyncEvent` para
o usuário afetado.**

Uma escrita que não gera evento é, por definição, invisível para os outros dispositivos. Não
existe "escrita local do servidor".

## Consequências

- No Sprint 6, quando o PostgreSQL entrar e houver um único armazenamento real, os handlers do
  CRUD tipado **não podem** escrever direto no repositório. Eles devem passar pela mesma camada
  que o `sync/push` usa para aplicar mutações — a que versiona a entidade e grava o evento.
  A forma mais segura é o CRUD ser construído **em cima** do aplicador de mutações, não ao lado
  dele: assim a regra é estrutural, e não depende de alguém lembrar.
- O teste do `AC-016` precisa ganhar uma variante que edite pela rota REST e prove que a mudança
  aparece no `pull` do Agent. Enquanto ela não existir, o `AC-016` é PARCIAL, não verde.
- O mesmo vale para o dicionário (`AC-021`) e para qualquer entidade sincronizável.

## Situação nesta fase

**Não implementado.** A Fase 1 valida contratos com armazenamentos em memória separados, e
unificá-los agora seria construir a camada de persistência real antes da hora. A dívida está
registrada aqui e no relatório `docs/fase-1-cobertura.md`, e é pré-requisito do Sprint 7 (sync),
não do Sprint 6 (cloud core) — mas a decisão de desenho precisa ser respeitada já no Sprint 6,
quando os handlers reais forem escritos.
