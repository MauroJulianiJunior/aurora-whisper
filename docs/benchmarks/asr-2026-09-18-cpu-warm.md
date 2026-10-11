# Benchmark de ASR — 2026-09-18

## Ambiente

- Hardware: Dell G15, i7-13650HX, 32 GB RAM, RTX 3050 6 GB (nao usada: build so CPU)
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

## Candidato: whisper-cpp-small-cpu-warm

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 46 (das quais timeout: 0)
- Latência p50: não medido (N=0)
- Latência p95: não medido (N=0)
- Latência média: não medido (N=0)
- RTF médio: não medido (N=0)
- RSS de pico: não medido neste ambiente (N=0)
- WER médio: não medido (N=0)
- CER médio: não medido (N=0)
- Entity Accuracy do ASR bruto (sem dictionary): não medido (Correct=0, Total=0)
- Number/Date Accuracy: não medido (Correct=0, Total=0)

### Warm (processo residente, modelo carregado)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 3.1570543s (N=46)
- Latência p95: 3.5938914s (N=46)
- Latência média: 3.195940928s (N=46)
- RTF médio: 0.7750 (N=46)
- RSS de pico: máximo 666.73 MB, média 665.56 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2445 (N=46)
- CER médio: 0.1163 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.6122 (Correct=30, Total=49)
- Number/Date Accuracy: 0.9091 (Correct=10, Total=11)

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | não medido | sem dados suficientes |
| RSS ≤ 1.00 GB (warm, pico máximo) | 666.73 MB (máx.), 665.56 MB (média) | passou |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 46 falhas | falhou |
| ≥100 execuções estáveis (warm) | 46 tentativas, 0 falhas | falhou |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | 61.2% (30/49) | falhou |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | não medido | sem dados suficientes |
| p50 ≤ 500ms (warm) | 3.1570543s | falhou |
| p95 ≤ 1.2s (cold) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (warm) | 3.5938914s | falhou |

## Candidato: whisper-cpp-large-v3-turbo-q5_0-cpu-warm

### Cold (processo novo por transcrição)

- Amostras tentadas: 46
- Falhas: 46 (das quais timeout: 0)
- Latência p50: não medido (N=0)
- Latência p95: não medido (N=0)
- Latência média: não medido (N=0)
- RTF médio: não medido (N=0)
- RSS de pico: não medido neste ambiente (N=0)
- WER médio: não medido (N=0)
- CER médio: não medido (N=0)
- Entity Accuracy do ASR bruto (sem dictionary): não medido (Correct=0, Total=0)
- Number/Date Accuracy: não medido (Correct=0, Total=0)

### Warm (processo residente, modelo carregado)

- Amostras tentadas: 46
- Falhas: 0 (das quais timeout: 0)
- Latência p50: 17.5910968s (N=46)
- Latência p95: 18.2700957s (N=46)
- Latência média: 17.616509878s (N=46)
- RTF médio: 4.2755 (N=46)
- RSS de pico: máximo 787.98 MB, média 783.32 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.1108 (N=46)
- CER médio: 0.0561 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.6939 (Correct=34, Total=49)
- Number/Date Accuracy: 0.9091 (Correct=10, Total=11)

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | não medido | sem dados suficientes |
| RSS ≤ 1.00 GB (warm, pico máximo) | 787.98 MB (máx.), 783.32 MB (média) | passou |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 46 falhas | falhou |
| ≥100 execuções estáveis (warm) | 46 tentativas, 0 falhas | falhou |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | 69.4% (34/49) | falhou |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | não medido | sem dados suficientes |
| p50 ≤ 500ms (warm) | 17.5910968s | falhou |
| p95 ≤ 1.2s (cold) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (warm) | 18.2700957s | falhou |

## Decisão técnica

_Este benchmark não escolhe sozinho (ADR-012). Preencher depois de revisar os números acima e os gates de docs/ai-spec/19-eval-plan.md:_

- Candidato escolhido: 
- Justificativa: 
- Modo (cold/warm) assumido em produção: 
- Consequência para o Sprint 4 (se aplicável): 
