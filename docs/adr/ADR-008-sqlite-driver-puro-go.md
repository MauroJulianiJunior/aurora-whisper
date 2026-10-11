# ADR-008 — Driver SQLite em Go puro, sem CGO

Status: Accepted
Data: 2026-09-17
Contexto: Fase 1, bloco F1-B

## Contexto

O Windows Agent precisa de SQLite para histórico, dicionário, correções, settings e outbox
(ADR-004). Em Go há dois caminhos para isso, e a escolha tem consequências que vão muito além
do banco:

- **`mattn/go-sqlite3`** — bindings para o SQLite em C. Exige **CGO**, ou seja, um compilador C
  presente em toda máquina que compilar o projeto.
- **`modernc.org/sqlite`** — o SQLite transpilado para Go puro. Sem CGO, sem compilador C.

O gatilho concreto: a máquina de desenvolvimento **não tem toolchain C instalado**. Isso já foi
detectado na auditoria do F1-A, quando `go test -race` não pôde ser executado. Com CGO
obrigatório, nem `go build` funcionaria.

## Decisão

Usar **`modernc.org/sqlite`** (Go puro).

## Justificativa

1. **Build sem pré-requisito externo.** Qualquer máquina com Go compila o Agent. Não há passo
   "instale o gcc antes" no onboarding, no CI ou no pipeline do installer.
2. **Cross-compilation trivial.** Com CGO, compilar o Agent Windows a partir de um runner Linux
   exige toolchain cruzado. Sem CGO, é `GOOS=windows go build`. Isso importa direto para o
   `TASK-022` (installer) e para o `TASK-026` (CI).
3. **Coerência com a ADR-002**, que escolheu Go justamente por produzir binários simples.
   Reintroduzir dependência de compilador C contradiz o motivo da escolha.
4. **O perfil de carga não justifica o custo.** O banco local é de um único usuário: histórico
   de ditados, dicionário e outbox. São milhares de linhas, não milhões, com escritas
   esporádicas. A vantagem de desempenho do SQLite em C é real, mas irrelevante nesta escala.

## Consequências

- `modernc.org/sqlite` é mais lento que o SQLite em C em cargas pesadas, e gera binário maior.
  Aceito pelo item 4 acima. Se algum dia o perfil mudar, a troca é contida: só a implementação
  do repositório muda, porque o domínio fala com a interface, não com o driver.
- **CGO continua desligado por padrão.** Nenhum código de produção pode passar a exigir CGO sem
  revisar esta ADR.
- Um toolchain C ainda é desejável na máquina de desenvolvimento, mas por **outro motivo**:
  habilitar `go test -race`. Isso é ferramenta de teste, não requisito de build — a distinção
  importa e não deve ser confundida.

## Escopo nesta fase

No F1-B o driver é usado **apenas para executar as migrations em teste** e provar que o schema
é SQL válido, com as constraints funcionando de verdade (índices únicos, chaves estrangeiras,
`ON DELETE SET NULL` em `Correction.transcription_id`). Os repositórios permanecem em memória,
porque o objetivo da Fase 1 é validar contratos.

A promoção do driver para a implementação real dos repositórios acontece no **Sprint 2**.
Enquanto isso, ele não entra no binário do Agent — é dependência exercitada só por teste.

Um schema que nunca foi executado é um schema não verificado; é por isso que ele roda contra um
SQLite de verdade já agora, em vez de esperar o Sprint 2.

> **Emendada pela ADR-017 (30-set-2026), só no que diz respeito ao dicionário e às correções.** O
> driver e a decisão de não usar CGO não mudam. Mas "dicionário e correções no SQLite" (Contexto,
> acima) **não vale nesta etapa**: o dicionário mora no `dicionario.json`, e as tabelas
> `local_dictionary_*` e `local_corrections` ficam vazias e sem repositório até a etapa 5 (sync).
> Conflito resolvido pela ADR-017.
