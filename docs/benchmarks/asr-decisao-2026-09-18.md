# Decisão do provider de ASR local — 18-set-2026

Status: **decidido**
Decidido por: Mauro Juliani Junior
Base: os relatórios deste diretório, medidos nos 46 arquivos do dataset de voz gravado por ele

O `ADR-012` reserva esta escolha para uma pessoa, com justificativa registrada. O benchmark produz
números e confronta com os gates; não escolhe.

---

## A decisão

**whisper.cpp, modelo `small` quantizado em `q4_0`, caminho só CPU, processo residente.**

Sem GPU.

### Justificativa

O `small` otimizado cumpre o gate de p95 com folga, fica a 13% do gate de p50, e usa um terço da
memória permitida. O `large-v3-turbo` tem menos da metade do erro de transcrição, mas não alcança
a meta de latência em CPU por nenhum arranjo medido — e habilitar a GPU custaria 674 MB no
instalador para uma pessoa não técnica baixar.

---

## A configuração exata, e ela não é negociável por descuido

Cada peça abaixo foi medida. Trocar qualquer uma sem medir de novo desfaz o resultado.

| Peça | Valor | Por quê |
|---|---|---|
| Binário | whisper.cpp tag de **build** `b5130`, `whisper-bin-x64.zip` | as tags semver não têm assets |
| Modelo | `small` quantizado localmente em **`q4_0`** | q4_0 é 48% mais rápido que f16 **no mesmo modelo** |
| Threads | **`-t 12`** | o default é 4, numa máquina de 20 núcleos lógicos |
| Janela do encoder | **`-ac 750`** | corta 40% sem estragar a transcrição (**só fala curta**: ver a nota abaixo) |
| Flash attention | **ligado** (default) | medido: desligar custa 22% |
| Modo | **warm**, servidor residente | cold custa 976 ms contra 565 ms |

> **Nota de 01-out-2026: o `-ac 750` foi medido só em fala curta.** O dataset tem no máximo
> 9,3 s de áudio (média de 5,6 s); com 750 o encoder vê 15 s por vez, e acima disso o laço de
> janelas do whisper.cpp, que supõe 30 s, fica frágil. Numa aula pelo Meet um ditado de 59 s
> entrou em laço de repetição e perdeu o resto da fala, e outros dois estouraram o prazo
> (`docs/defeito-ditado-longo.md`). O Agent não passa mais `-ac` ao servidor: manda `audio_ctx`
> por requisição, **768 até 12 s e a janela inteira acima** (`local.JanelaRecomendada`). 768 em vez
> de 750 porque é múltiplo de 256: com a flash attention, o K/V é lido arredondado para múltiplos
> de 256 sem máscara, e com o servidor alternando janelas as linhas que sobram guardariam o K/V da
> requisição anterior. **Nenhum dos dois números foi medido ainda**: nem o 768 na fala curta (o
> esperado é ~2% a mais que 750), nem a janela inteira no ditado longo. O benchmark que mede os
> dois é o `cmd/asrlongo` (comando em `docs/defeito-ditado-longo.md`), e roda com a máquina livre.

### O caminho até o número

| Configuração | p50 | p95 | RSS máx | WER |
|---|---|---|---|---|
| defaults do binário (f16, 4 threads, cold) | 4,11 s | 4,61 s | 768,8 MB | 0,229 |
| q4_0 + 12 threads | 1,57 s | 1,94 s | 441,0 MB | 0,246 |
| + audio-ctx 750 | 976 ms | 1,098 s | 412,6 MB | 0,249 |
| **+ warm** | **565 ms** | **658 ms** | **365,2 MB** | **0,260** |

### Contra os gates do `19-eval-plan.md`

| Gate | Medido | |
|---|---|---|
| RSS ≤ 1 GB | 365,2 MB | **passa**, com quase 3× de folga |
| p95 ≤ 1,2 s | 658 ms | **passa**, com quase 2× de folga |
| p50 ≤ 500 ms | 565 ms | **falha por 13%** |
| ≥100 execuções estáveis | 46 tentativas, 0 falhas | **não atingido** — o dataset tem 46 amostras |
| Entity Accuracy ≥95% após dicionário | não medido | o harness mede ASR **bruto**; o dicionário nunca é chamado |

---

## O que esta decisão NÃO resolve, e precisa entrar no planejamento

### 1. O p50 está 13% acima da meta

565 ms contra 500 ms. Não é um empate técnico: é uma falha pequena e conhecida. As saídas, nenhuma
medida ainda:

- afinar `audio-ctx` entre 750 e 1000 com mais granularidade
- o ganho do adaptador sair do caminho: produção falaria HTTP direto com o servidor, sem o salto de
  processo extra (custo medido abaixo de 0,7 ms, então o ganho é pequeno)
- aceitar 565 ms e revisar a meta, registrando por quê

### 2. `-t 12` foi afinado NESTA máquina

O Dell G15 tem 20 núcleos lógicos. Numa máquina de 4 núcleos, 12 threads provavelmente **piora**.
O Agent precisa escolher isso em runtime, não carregar 12 fixo — e isso é trabalho de sprint, não
configuração.

### 3. O `q4_0` não existe pronto para baixar

Foi gerado aqui com `whisper-quantize.exe` a partir do `ggml-small.bin`. O instalador precisa
decidir: embarcar o `q4_0` já pronto (139 MB) ou baixar o f16 (465 MB) e quantizar na instalação.
Embarcar é mais simples e menor.

### 4. O WER de 0,260 é o do ASR **bruto**

A Entity Accuracy bruta é 57,1%: `DCTFWeb` sai como "DCTF Web", `n8n` como "N8n". O gate real do
eval-plan é **≥95% depois do dicionário**, e o dicionário nunca foi exercitado. Sem ele, o produto
erra exatamente o vocabulário que existe para acertar.

### 5. A meta de 500 ms é agressiva para o mercado

A pesquisa de 18-set-2026 encontrou o Wispr Flow — produto comercial, ditado por nuvem — com
latência publicada de **1 a 3 segundos** até o texto formatado aparecer. Se isso se confirmar, os
565 ms medidos aqui já estão à frente do estado da arte comercial, e a meta do `02-prd.md` merece
ser revisitada com dado em vez de ambição.

---

## Consequência direta para o Sprint 4

**O Agent tem de manter um processo de ASR residente.** Isso deixou de ser opção de desenho: cold
custa 976 ms contra 565 ms do warm, e o `ADR-012` previu exatamente essa bifurcação quando exigiu
medir os dois separados.

Isso traz problemas que o Sprint 4 precisa resolver e que não existiam antes: ciclo de vida do
processo filho, o que fazer quando ele morre, como não deixar processo órfão quando o Agent cai, e
quanto de memória fica residente o tempo todo (365 MB) numa máquina que a pessoa usa para
trabalhar.

---

## O que ficou medido e não foi usado

- **`large-v3-turbo`**: WER 0,111 contra 0,260, menos da metade do erro. Otimizado ao máximo em CPU
  chega a 6,8 s; mesmo com o melhor ganho de quantização da literatura ficaria em ~1,7 s, 3,5×
  acima da meta. **Volta a ser candidato se a GPU entrar** — a decisão de hoje é de CPU, não é
  definitiva sobre o modelo.
- **`q5_0`**: descartado por medição, não por preferência. É menor que o `q8_0` e **mais lento** que
  ele. Foi a quantização que escolhi na primeira rodada, e ela sozinha respondia por boa parte dos
  18,3 s que quase reprovaram o turbo.
- **Parakeet TDT**: não medido. A arquitetura FastConformer não tem a janela fixa de 30 s do
  Whisper, e o `parakeet-cli.exe` já veio no mesmo zip. Continua sendo o candidato mais promissor
  para reduzir latência **sem** GPU, e o conflito de licença (NVIDIA diz CC-BY-4.0, o repositório
  GGUF declara MIT) precisa ser resolvido antes de qualquer uso em produto.
