# Candidatos de ASR local — levantamento de 17-set-2026

Status: **levantamento**, não é a decisão. O `ADR-012` reserva a escolha para uma pessoa, com
justificativa registrada, depois de medir.

Nada foi baixado, instalado ou executado. Isto é pesquisa de fontes públicas.

---

## O achado que decide o sprint

**De cerca de 45 afirmações quantitativas levantadas, ZERO medem o regime que nos importa** — fala
de 1 a 10 s, ponta a ponta `release→insert`, em português, em hardware de consumidor classe
RTX 3050 no Windows.

Os números publicados se dividem em: transcrição de arquivo longo em lote (13 min, 3:24, 16,7 s),
GPU de datacenter (Tesla T4, A100), encoder isolado sem o resto do caminho, fator relativo sem
absoluto ("2-5x mais rápido"), ou parâmetro de configuração vendido como latência.

Isso não é lacuna a preencher com mais busca. É a confirmação de que **o benchmark do Sprint 3 não
tem substituto na literatura.**

---

## Duas correções que invertem premissas

### 1. Quantização pode ser MAIS LENTA na GPU

A frente de levantamento elegeu como achado central o sherpa-onnx com `large-v3 int8` num Tesla T4:
6,625 s de áudio em 19,190 s de processamento, e concluiu que `large-v3` em processo de um disparo
está fora, por volta de 38× a meta.

**Na mesma página, uma linha acima**, está o mesmo modelo em `float32`, mesmo GPU, mesmo áudio:
**5,910 s** — RTF 0,892, mais de três vezes mais rápido que o int8.

O custo não era do `large-v3` nem do regime cold. Era da **quantização int8 no CUDA**, patologia
conhecida: operadores quantizados caem para a CPU ou exigem conversão de ida e volta em torno de
kernels não suportados.

Isso derruba uma premissa que atravessava o levantamento inteiro — as quatro frentes trataram
quantização como "menor e mais rápido", o que vale em CPU e **não necessariamente em GPU**.

### 2. O encoder do Whisper sempre processa 30 segundos

Toda a família Whisper preenche a entrada com zeros até 30 s. **Uma fala de 1 s custa
praticamente o mesmo que uma de 10 s.**

Duas consequências:

- a meta de `p50 ≤ 500 ms` "para falas de 1 a 10 s" é, na prática, **uma meta única**, não uma
  curva que piora com a duração;
- todo benchmark publicado da família mede arquivo longo, regime em que esse custo fixo se
  amortiza e some. **Nenhum diz nada sobre ditado curto.**

---

## Eliminados por restrição dura

Não entram no benchmark, por melhor que transcrevam. O `ADR-012` exige processo externo, Windows
x64 e licença que permita produto proprietário distribuído.

| Candidato | Motivo |
|---|---|
| Purfview / whisper-standalone-win | **sem licença declarada** — pior que licença restritiva: sem concessão expressa não há direito de redistribuir |
| nvidia/canary-1b (v1) | pesos **CC-BY-NC** (o `canary-1b-v2` é CC-BY-4.0 e **não** está eliminado) |
| Meta MMS, SeamlessM4T | **CC-BY-NC** |
| Silero STT | pesos **CC-BY-NC-SA**, e português não consta |
| vosk-model-pt-fb | **GPLv3** — copyleft forte, incompatível com distribuição proprietária |
| Vosk pt-0.3 (pequeno) | licença limpa, mas **WER 32,6%** — erra uma palavra em três. Dicionário corrige termo conhecido, não reconstrói transcrição |
| distil-whisper (oficial) | checkpoints oficiais são **só inglês** |
| Moonshine | português não consta |
| Coqui STT | projeto morto desde jan/2024, Model Zoo fora do ar |
| Zipformer, Paraformer, Kaldi | runtime bom, **sem modelo pt-BR público** |
| WhisperX | o ganho dele é lote de segmentos; em fala de 1-10 s não há o que lotear |
| Whisper.net | é o mesmo whisper.cpp com runtime .NET por cima no instalador |
| FL33TW00D/whisper-turbo | Rust/WASM/WebGPU — exigiria embarcar Chromium |
| NeMo (toolkit Python) **como distribuição** | exigir Python + PyTorch + CUDA na máquina do usuário final é inviável. **Não elimina os pesos da NVIDIA**, que seguem elegíveis por runtimes C++ |

---

## O que medir, em ordem

### 0. Custo de transporte, sem modelo nenhum

Antes de qualquer ASR: um processo de eco que lê N bytes e devolve N bytes, nos quatro
transportes (pipe por stdin, POST em loopback, WebSocket, named pipe), com o mesmo cronômetro do
benchmark real. Um WAV PCM16 de 10 s são 320.044 bytes.

**Se isso não for medido primeiro, todo número de warm sai com o custo de transporte embutido e
não separável** — e a decisão do Sprint 4 sobre qual protocolo usar fica sem base. Não baixa nada,
não custa nada, e é a única medição que não pode ser feita depois.

### 1. whisper.cpp, caminho **só CPU**

`whisper-cli` para cold, `whisper-server` (loopback) para warm. Modelos `small` e
`large-v3-turbo-q5_0`.

A hipótese que isto testa vale o sprint inteiro: **se o caminho só CPU já cumpre a meta em warm no
i7-13650HX, o instalador perde ~675 MB e a questão de GPU morre.**

Pegadinha de empacotamento confirmada: os binários Windows saem nas tags de **build** (`b5130`),
não nas tags semver — `v1.9.4` e `v1.9.3` têm assets vazios. O instalador tem de apontar para a
tag de build.

### 2. Varredura de quantização × backend

O mesmo modelo e o mesmo áudio em fp16 / q8_0 / q5_0 / q4_k, em CPU e em CUDA. Existe justamente
para não repetir o erro da correção 1: medir se quantizar ajuda ou atrapalha **neste** hardware.

### 3. Parakeet TDT 0.6B v3, em GGUF

Único não-Whisper que sobrevive com modelo pt-BR real. Testa duas coisas: se cabe no alvo de
~1 GB (o único RSS já medido no mundo é 1,23 GB, em iOS), e se a arquitetura TDT foge do custo
fixo de 30 s do Whisper.

### 4. NeMo-Speech.cpp + Nemotron streaming

A única hipótese **arquiteturalmente diferente** da lista: um modelo streaming consome o áudio
*enquanto a tecla está pressionada*, então o que sobra depois do `release` é só a cauda. Se
funcionar, é o caminho mais curto para a meta de p50.

Risco declarado: v0.1.0, publicada há menos de um mês. O gate de 100 execuções estáveis do
`19-eval-plan.md` é exatamente onde um projeto nessa idade costuma cair.

---

## Duas decisões que não são minhas

### GPU entra no benchmark?

Habilitar a RTX 3050 custa **674,5 MB** no instalador (zip cuBLAS 12.4) contra **8,6 MB** do
caminho só CPU. O `19-eval-plan.md` já diz "GPU opcional; CPU funcional".

**Minha recomendação:** medir CPU primeiro. Se passar, a GPU nunca precisa entrar. Medir GPU antes
e só depois descobrir que o instalador dobrou de tamanho é a ordem errada.

### O conflito de licença do Parakeet

O upstream `nvidia/parakeet-tdt-0.6b-v3` diz textualmente **CC-BY-4.0**. O repositório de conversão
`ggml-org/parakeet-GGUF` declara **MIT** no card.

São licenças diferentes para o mesmo peso, e quem converte não pode relicenciar o que não é dele.
CC-BY-4.0 permite uso comercial, mas **exige atribuição** — o que tem consequência para o produto,
não para o benchmark. **Isso é decisão sua**, e vale resolver antes de o Parakeet virar vencedor,
não depois.

---

## O que este levantamento não fez

- **não baixou nem executou nada.** Todo número aqui é de fonte pública, com o regime que a fonte
  mediu declarado junto — e quase nenhum é o nosso regime.
- **não escolheu vencedor.** O `ADR-012` reserva isso para uma pessoa, depois da medição.
- **não mediu português com termos técnicos.** É exatamente o que o dataset gravado existe para
  medir, e não há literatura que substitua.
