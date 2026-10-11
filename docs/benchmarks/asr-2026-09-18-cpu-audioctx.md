# Benchmark de ASR — 2026-09-18

## Ambiente

- Hardware: Dell G15, i7-13650HX, 32 GB RAM
- SO: Windows 11 Home 26200

## Dataset

- N total de amostras: 46
- Hash do dataset (sha256): `3b8a26ab18de096eea7c12a59ecc071ebe945810f404aab4648d2bce68ef9830`

### Amostras por categoria

| Categoria | N |
|---|---|
| pt-br-geral | 4 |
| ingles | 3 |
| code-switching | 4 |
| contabilidade | 5 |
| desenvolvimento | 5 |
| nomes-proprios | 4 |
| numeros-datas-valores | 5 |
| self-corrections | 4 |
| ruido-moderado | 3 |
| fala-rapida | 3 |
| 1-3s | 3 |
| 3-10s | 3 |

## Candidato: whisper-cpp-small-cpu-ac1000

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 3.1480931s (N=46)
- Latência p95: 3.5840534s (N=46)
- Latência média: 3.125369717s (N=46)
- RTF médio: 0.7460 (N=46)
- RSS de pico: máximo 750.61 MB, média 747.86 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2379 (N=46)
- CER médio: 0.1231 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.6327 (Correct=31, Total=49)
- Number/Date Accuracy: 0.8182 (Correct=9, Total=11)

### Warm (processo residente, modelo carregado)

Não medido (candidato não declara suporte a este modo).

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | 750.61 MB (máx.), 747.86 MB (média) | passou |
| RSS ≤ 1.00 GB (warm, pico máximo) | não medido | sem dados suficientes |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 0 falhas | falhou |
| ≥100 execuções estáveis (warm) | 0 tentativas, 0 falhas | sem dados suficientes |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | 63.3% (31/49) | falhou |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | 3.1480931s | falhou |
| p50 ≤ 500ms (warm) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (cold) | 3.5840534s | falhou |
| p95 ≤ 1.2s (warm) | não medido | sem dados suficientes |

> **Nota (ADR-012):** este candidato só foi medido em modo cold. Sem medição warm, não é possível saber se ele cumpriria os gates de latência em uso contínuo — reportar isso como "reprovado" ou "aprovado" seria inventar uma medição que não existe.

## Candidato: whisper-cpp-small-cpu-ac750

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 2.446566s (N=46)
- Latência p95: 2.7087422s (N=46)
- Latência média: 2.452386269s (N=46)
- RTF médio: 0.5857 (N=46)
- RSS de pico: máximo 739.71 MB, média 738.28 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2420 (N=46)
- CER médio: 0.1232 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.5714 (Correct=28, Total=49)
- Number/Date Accuracy: 0.8182 (Correct=9, Total=11)

### Warm (processo residente, modelo carregado)

Não medido (candidato não declara suporte a este modo).

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | 739.71 MB (máx.), 738.28 MB (média) | passou |
| RSS ≤ 1.00 GB (warm, pico máximo) | não medido | sem dados suficientes |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 0 falhas | falhou |
| ≥100 execuções estáveis (warm) | 0 tentativas, 0 falhas | sem dados suficientes |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | 57.1% (28/49) | falhou |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | 2.446566s | falhou |
| p50 ≤ 500ms (warm) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (cold) | 2.7087422s | falhou |
| p95 ≤ 1.2s (warm) | não medido | sem dados suficientes |

> **Nota (ADR-012):** este candidato só foi medido em modo cold. Sem medição warm, não é possível saber se ele cumpriria os gates de latência em uso contínuo — reportar isso como "reprovado" ou "aprovado" seria inventar uma medição que não existe.

## Candidato: whisper-cpp-small-cpu-ac500

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 1.8891327s (N=46)
- Latência p95: 2.1546315s (N=46)
- Latência média: 1.911751993s (N=46)
- RTF médio: 0.4552 (N=46)
- RSS de pico: máximo 729.15 MB, média 727.00 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2734 (N=46)
- CER médio: 0.1413 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.5918 (Correct=29, Total=49)
- Number/Date Accuracy: 0.8182 (Correct=9, Total=11)

### Warm (processo residente, modelo carregado)

Não medido (candidato não declara suporte a este modo).

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | 729.15 MB (máx.), 727.00 MB (média) | passou |
| RSS ≤ 1.00 GB (warm, pico máximo) | não medido | sem dados suficientes |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 0 falhas | falhou |
| ≥100 execuções estáveis (warm) | 0 tentativas, 0 falhas | sem dados suficientes |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | 59.2% (29/49) | falhou |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | 1.8891327s | falhou |
| p50 ≤ 500ms (warm) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (cold) | 2.1546315s | falhou |
| p95 ≤ 1.2s (warm) | não medido | sem dados suficientes |

> **Nota (ADR-012):** este candidato só foi medido em modo cold. Sem medição warm, não é possível saber se ele cumpriria os gates de latência em uso contínuo — reportar isso como "reprovado" ou "aprovado" seria inventar uma medição que não existe.

## Decisão técnica

_Este benchmark não escolhe sozinho (ADR-012). Preencher depois de revisar os números acima e os gates de docs/ai-spec/19-eval-plan.md:_

- Candidato escolhido: 
- Justificativa: 
- Modo (cold/warm) assumido em produção: 
- Consequência para o Sprint 4 (se aplicável): 
