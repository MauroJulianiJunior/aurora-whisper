# Benchmark de ASR — 2026-09-18

## Ambiente

- Hardware: Dell G15, i7-13650HX (20 threads), 32 GB RAM
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

## Candidato: small-q4_0-t12-semac

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 1.5732042s (N=46)
- Latência p95: 1.9396148s (N=46)
- Latência média: 1.617484158s (N=46)
- RTF médio: 0.3858 (N=46)
- RSS de pico: máximo 440.98 MB, média 437.41 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2461 (N=46)
- CER médio: 0.1130 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.6122 (Correct=30, Total=49)
- Number/Date Accuracy: 0.8182 (Correct=9, Total=11)

### Warm (processo residente, modelo carregado)

Não medido (candidato não declara suporte a este modo).

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | 440.98 MB (máx.), 437.41 MB (média) | passou |
| RSS ≤ 1.00 GB (warm, pico máximo) | não medido | sem dados suficientes |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 0 falhas | falhou |
| ≥100 execuções estáveis (warm) | 0 tentativas, 0 falhas | sem dados suficientes |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | 61.2% (30/49) | falhou |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | 1.5732042s | falhou |
| p50 ≤ 500ms (warm) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (cold) | 1.9396148s | falhou |
| p95 ≤ 1.2s (warm) | não medido | sem dados suficientes |

> **Nota (ADR-012):** este candidato só foi medido em modo cold. Sem medição warm, não é possível saber se ele cumpriria os gates de latência em uso contínuo — reportar isso como "reprovado" ou "aprovado" seria inventar uma medição que não existe.

## Candidato: small-q4_0-t12-ac1000

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 1.1678384s (N=46)
- Latência p95: 1.3877627s (N=46)
- Latência média: 1.190716193s (N=46)
- RTF médio: 0.2825 (N=46)
- RSS de pico: máximo 421.63 MB, média 419.34 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2605 (N=46)
- CER médio: 0.1258 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.6122 (Correct=30, Total=49)
- Number/Date Accuracy: 0.8182 (Correct=9, Total=11)

### Warm (processo residente, modelo carregado)

Não medido (candidato não declara suporte a este modo).

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | 421.63 MB (máx.), 419.34 MB (média) | passou |
| RSS ≤ 1.00 GB (warm, pico máximo) | não medido | sem dados suficientes |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 0 falhas | falhou |
| ≥100 execuções estáveis (warm) | 0 tentativas, 0 falhas | sem dados suficientes |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | 61.2% (30/49) | falhou |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | 1.1678384s | falhou |
| p50 ≤ 500ms (warm) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (cold) | 1.3877627s | falhou |
| p95 ≤ 1.2s (warm) | não medido | sem dados suficientes |

> **Nota (ADR-012):** este candidato só foi medido em modo cold. Sem medição warm, não é possível saber se ele cumpriria os gates de latência em uso contínuo — reportar isso como "reprovado" ou "aprovado" seria inventar uma medição que não existe.

## Candidato: small-q4_0-t12-ac750

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 976.1162ms (N=46)
- Latência p95: 1.0980306s (N=46)
- Latência média: 1.003708106s (N=46)
- RTF médio: 0.2383 (N=46)
- RSS de pico: máximo 412.62 MB, média 410.63 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2486 (N=46)
- CER médio: 0.1165 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.6122 (Correct=30, Total=49)
- Number/Date Accuracy: 0.8182 (Correct=9, Total=11)

### Warm (processo residente, modelo carregado)

Não medido (candidato não declara suporte a este modo).

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | 412.62 MB (máx.), 410.63 MB (média) | passou |
| RSS ≤ 1.00 GB (warm, pico máximo) | não medido | sem dados suficientes |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 0 falhas | falhou |
| ≥100 execuções estáveis (warm) | 0 tentativas, 0 falhas | sem dados suficientes |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | 61.2% (30/49) | falhou |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | 976.1162ms | falhou |
| p50 ≤ 500ms (warm) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (cold) | 1.0980306s | passou |
| p95 ≤ 1.2s (warm) | não medido | sem dados suficientes |

> **Nota (ADR-012):** este candidato só foi medido em modo cold. Sem medição warm, não é possível saber se ele cumpriria os gates de latência em uso contínuo — reportar isso como "reprovado" ou "aprovado" seria inventar uma medição que não existe.

## Decisão técnica

_Este benchmark não escolhe sozinho (ADR-012). Preencher depois de revisar os números acima e os gates de docs/ai-spec/19-eval-plan.md:_

- Candidato escolhido: 
- Justificativa: 
- Modo (cold/warm) assumido em produção: 
- Consequência para o Sprint 4 (se aplicável): 
