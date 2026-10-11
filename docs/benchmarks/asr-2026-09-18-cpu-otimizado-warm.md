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

## Candidato: small-q4_0-t12-ac750-warm

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
- Latência p50: 565.2673ms (N=46)
- Latência p95: 657.8943ms (N=46)
- Latência média: 574.186865ms (N=46)
- RTF médio: 0.1368 (N=46)
- RSS de pico: máximo 365.16 MB, média 358.12 MB (N=46 de 46 tentadas). O gate compara com o MÁXIMO — a média esconde a pior chamada.
- WER médio: 0.2604 (N=46)
- CER médio: 0.1214 (N=46)
- Entity Accuracy do ASR bruto (sem dictionary): 0.5714 (Correct=28, Total=49)
- Number/Date Accuracy: 0.9091 (Correct=10, Total=11)

### Gates (docs/ai-spec/19-eval-plan.md)

| Gate | Medido | Passou? |
|---|---|---|
| PT-BR + inglês | não aplicável a este harness | não aplicável — gate de composição do DATASET, não de candidato — ver seção Dataset acima (N por categoria; categorias PT-BR geral e inglês) |
| RSS ≤ 1.00 GB (cold, pico máximo) | não medido | sem dados suficientes |
| RSS ≤ 1.00 GB (warm, pico máximo) | 365.16 MB (máx.), 358.12 MB (média) | passou |
| GPU opcional | não aplicável a este harness | não aplicável — não é um limiar de passou/falhou — GPU é uma opção de configuração do candidato; RSS e latência acima já refletem o hardware usado, com ou sem GPU |
| CPU funcional | não aplicável a este harness | não aplicável — não medido como linha própria — evidenciado pelas execuções bem-sucedidas nas seções Cold/Warm acima (amostras tentadas vs. falhas) |
| ≥100 execuções estáveis (cold) | 46 tentativas, 46 falhas | falhou |
| ≥100 execuções estáveis (warm) | 46 tentativas, 0 falhas | falhou |
| sem persistência (áudio não fica em disco/nuvem) | não aplicável a este harness | não aplicável — invariante de código (AC-028, CLAUDE.md "Áudio não deve ser persistido"), não uma medição de runtime — verificado por revisão de código e no roteiro de teste manual, não por este harness |
| Entity Accuracy do ASR bruto (sem dictionary) — cold | não medido (nenhuma amostra com entidades esperadas) | sem dados suficientes |
| Entity Accuracy do ASR bruto (sem dictionary) — warm | 57.1% (28/49) | falhou |
| Entity Accuracy ≥ 95% após dictionary (gate real do eval-plan) | não aplicável a este harness | não aplicável — NÃO MEDIDO por este harness — mede o texto BRUTO do candidato, internal/dictionary nunca é chamado; falta popular o dictionary com os termos reais do dono do produto (trabalho de dataset) e medir "ASR + dictionary", decisão deliberadamente adiada para não confundir com a comparação de candidatos deste sprint |
| Smart false correction ≤ 1% | não aplicável a este harness | não aplicável — não medido por este harness — pertence à avaliação contextual do resolver (19-eval-plan.md, seção "Eval contextual"), não à comparação de candidatos de ASR |
| p50 ≤ 500ms (cold) | não medido | sem dados suficientes |
| p50 ≤ 500ms (warm) | 565.2673ms | falhou |
| p95 ≤ 1.2s (cold) | não medido | sem dados suficientes |
| p95 ≤ 1.2s (warm) | 657.8943ms | passou |

## Decisão técnica

_Este benchmark não escolhe sozinho (ADR-012). Preencher depois de revisar os números acima e os gates de docs/ai-spec/19-eval-plan.md:_

- Candidato escolhido: **whisper.cpp `small` quantizado em `q4_0`, caminho só CPU**
- Justificativa: cumpre p95 com quase o dobro de folga (658 ms contra 1,2 s) e usa um terço da
  memória permitida (365 MB de 1 GB). Fica 13% acima do gate de p50. O `large-v3-turbo` tem menos
  da metade do erro, mas não alcança a meta em CPU por nenhum arranjo medido, e habilitar a GPU
  custaria 674 MB no instalador.
- Modo assumido em produção: **warm** — processo residente. Cold custa 976 ms contra 565 ms.
- Consequência para o Sprint 4: manter processo de ASR residente deixou de ser opção de desenho.
  Traz ciclo de vida do processo filho, recuperação de morte do servidor, prevenção de órfão, e
  365 MB residentes numa máquina de trabalho.
- Decidido por: Mauro Juliani Junior, 18-set-2026. Registro completo em `asr-decisao-2026-09-18.md`. 
