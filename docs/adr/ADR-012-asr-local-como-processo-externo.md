# ADR-012 — ASR local roda como processo externo, e o benchmark mede o custo real

Status: Accepted
Data: 2026-09-17
Contexto: Fase 2, Sprint 3 (TASK-006 — benchmark de ASR)

## Contexto

O Sprint 3 escolhe o provider de ASR local por medição, não por preferência (`ADR-003`). Mas
antes de medir é preciso responder uma pergunta que restringe a lista de candidatos: **como um
modelo de ASR roda a partir de um Agent em Go sem CGO?**

Os runtimes de ASR relevantes — família Whisper, Parakeet e equivalentes — são C/C++ ou Python.
Nenhum é Go.

## Decisão

**Candidatos de ASR local são executados como processo externo**, com o Agent falando com eles
por uma fronteira de processo. Nenhum entra no binário do Agent por CGO.

`ADR-008` e `ADR-010` já rejeitaram CGO duas vezes, pelos mesmos três motivos: build sem
pré-requisito, cross-compilation trivial para o installer, e coerência com `ADR-002`. Um modelo
de ASR é um motivo muito mais forte para reabrir essa discussão do que um driver de SQLite — e
ainda assim não é suficiente, porque a alternativa existe e é boa: um processo separado dá o
mesmo resultado, com isolamento de crash de brinde.

**Consequência para a elegibilidade:** um candidato que só funcione embutido via CGO está fora,
independentemente da qualidade de transcrição. Não adianta ser o melhor modelo se ele obriga a
quebrar a fundação do projeto.

## O benchmark mede o custo REAL, não o custo de laboratório

Esta é a parte que mais facilmente produz um número bonito e mentiroso.

Medir apenas o tempo de inferência do modelo daria um resultado muito melhor do que a pessoa vai
sentir. A meta do produto é **`release→insert` p50 ≤ 500 ms** (`02-prd.md` §7) — o relógio começa
quando a tecla é solta e para quando o texto aparece na tela. Tudo que acontece no meio conta:

- escrever o áudio onde o processo externo consegue ler, ou empurrá-lo por um pipe
- o custo de atravessar a fronteira de processo
- o modelo carregado e aquecido, ou carregado na hora (a diferença é enorme e precisa ser
  medida separadamente, não escondida numa média)
- ler o resultado de volta e desserializar

**O benchmark registra o tempo de ponta a ponta, do jeito que produção vai pagar.** Se medir
outra coisa, escolhemos o candidato errado com confiança.

### Dois números distintos, nunca somados

| Medida | O que responde |
|---|---|
| **cold** — processo iniciado a cada transcrição | quanto custa se não mantivermos nada vivo |
| **warm** — processo vivo, modelo já carregado | quanto custa em uso real e contínuo |

Se apenas o `warm` cumprir a meta, a decisão de arquitetura do Sprint 4 é obrigatória: manter um
processo residente com protocolo próprio, não iniciar um a cada fala. É informação de projeto, e
precisa sair do benchmark explicitamente.

## O que o benchmark NÃO pode fazer

- **Não escolhe sozinho.** Ele produz números e confronta com os gates do `19-eval-plan.md`. A
  decisão é registrada por uma pessoa, com justificativa, em `docs/benchmarks/asr-AAAA-MM-DD.md`.
- **Não pode inventar medição.** Se um número não foi medido, o relatório diz que não foi. O
  `ADR-010` já teve uma lição sobre isso: um agente escreveu "tipicamente float32" e quem leu
  entendeu "sempre".
- **Não roda com áudio sintético.** Voz gerada por TTS não mede o que importa — sotaque, ritmo,
  ruído de fundo e os termos técnicos que a pessoa realmente fala. O dataset é gravado por quem
  vai usar o produto.

## Se nenhum candidato local passar

O `19-eval-plan.md` já prevê: a nuvem vira o padrão, o local continua disponível como
experimental, e os resultados ficam documentados. Isso **não é falha do sprint** — é o sprint
cumprindo a função dele, que é decidir com evidência em vez de com esperança.

O `ADR-003` existe justamente para que essa troca custe pouco: tudo fala com `ASRProvider`.

## Consequência para o dataset

Sem dataset real, o benchmark mede inglês genérico de laboratório e não diz nada sobre o uso.
O dataset precisa conter o vocabulário que o dono do produto fala todo dia — termos de
contabilidade, nomes de ferramentas, nomes próprios, números e datas — e precisa ser gravado na
voz dele, no microfone dele, no ambiente dele.

Ele fica **fora do git** (`.gitignore` bloqueia áudio, invariante `AC-028`) e é referenciado por
hash no relatório, conforme o `19-eval-plan.md` já exige.

## Nota de 2026-10-01: o dataset não tinha áudio longo, e a janela do encoder passou a ser por requisição

O dataset de 46 amostras tem no máximo 9,3 s de áudio. A janela reduzida do encoder (`-ac 750`, 15 s)
foi escolhida por ele e nunca viu um ditado longo. Numa aula pelo Meet, ditados de 27 a 89 s
mostraram o custo: laço de repetição com perda do resto da fala, e transcrições de 16 a 19 s que
estouraram o prazo (`docs/defeito-ditado-longo.md`).

O Agent passou a subir o servidor sem `-ac` e a mandar o campo `audio_ctx` em cada requisição,
pela duração do áudio: 768 até 12 s, janela inteira acima (`local.JanelaRecomendada`). O
whisper-server b5130 lê o campo por requisição (`server.cpp:510-513`), então a troca não reinicia
nada. A configuração "não negociável" de `docs/benchmarks/asr-decisao-2026-09-18.md` muda nesse
ponto, com nota lá.

O benchmark ganhou o caso que faltava: `cmd/asrlongo` monta áudios de 30, 60 e 90 s a partir do
dataset (concatenando amostras com pausas) e mede, com o servidor real, o `-ac 750` antigo, a
janela inteira, a janela por duração (servidor só com áudio longo e intercalado com fala curta) e
os ajustes do decodificador. Ele ainda não rodou: depende da máquina livre (o Mauro dá a aula nela).
