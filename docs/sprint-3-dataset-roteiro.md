# Sprint 3 — Roteiro de gravação do dataset de voz

O benchmark de ASR não tem o que medir sem a sua voz. É isso que este roteiro resolve.

Tempo estimado: **20 a 25 minutos**, e dá para parar no meio — o gravador retoma de onde você
largou.

---

## Por que você, e não um áudio pronto da internet

O `ADR-012` fecha assim: *sem dataset real, o benchmark mede inglês genérico de laboratório e não
diz nada sobre o uso.*

Um dataset público mediria um locutor profissional, num estúdio, lendo texto neutro em inglês. O
produto vai transcrever **você**, no **seu** microfone, na **sua** sala, falando DCTFWeb, PGDAS,
n8n e Supabase. São problemas diferentes, e um modelo pode ser ótimo no primeiro e inútil no
segundo — que é exatamente a decisão que o Sprint 3 precisa tomar.

Voz sintética também não serve, e pelo mesmo motivo: TTS não tem o seu sotaque, o seu ritmo, nem o
ruído de fundo da sua casa.

---

## O que este dataset decide — e o que ele não decide

São **46 frases**. O `19-eval-plan.md` pede **≥30 exemplos por categoria crítica**, o que daria
algo entre 150 e 360 gravações. Este é um **piloto**, deliberadamente menor, e é importante você
saber o que cada número vale:

| Medida | O piloto de 46 frases basta? | Por quê |
|---|---|---|
| **Latência ponta a ponta** (`release→insert`) | **Sim** | cada frase é uma medição independente; 46 amostras já dão p50 e p95 com folga |
| **RTF** (tempo de processamento ÷ duração do áudio) | **Sim** | idem |
| **Pico de memória (RSS)** | **Sim** | o que importa é o pior caso, e ele aparece já nas primeiras frases |
| **WER / CER** (taxa de erro) | **Só para diferença grosseira** | serve para eliminar um candidato que não fala português; não para separar dois que ficaram próximos |
| **Entity Accuracy por categoria** | **Não** | 3 a 5 amostras por categoria é ruído, não estatística |

Traduzindo: **o piloto elimina candidatos pelo custo e pega desastre de qualidade.** Se dois
sobreviverem empatados em qualidade, aí sim vale expandir as categorias críticas (contabilidade,
desenvolvimento, nomes próprios, números) até os 30 do eval-plan — e a expansão sai muito melhor
depois, guiada pelos erros reais que o piloto mostrar, do que chutada agora.

Isso está escrito aqui para que ninguém — inclusive eu, daqui a três sprints — leia o relatório do
piloto como se fosse a medição definitiva.

---

## Duas coisas que você precisa saber antes de ler as frases

### 1. Nenhum nome de cliente real

Eu tinha te prometido frases com "nomes de clientes". **Retirei isso.** O `19-eval-plan.md` exige
*sem dados sensíveis reais*, e nome de cliente, CNPJ e valor de cliente são exatamente isso.

Os nomes do manifesto — Metalúrgica Toffanetti, Rosângela Bittencourt, Thiago Vasconcelos — são
**fictícios**, escolhidos para ter a mesma dificuldade fonética de nome brasileiro real: sobrenome
longo, dígrafo, nasal, acento. Medem a mesma coisa sem carregar dado de ninguém.

Se na hora de ler você trocar por um nome real por hábito, a gravação vira dado sensível. **Leia o
que está escrito.**

### 2. Números escritos em dígito, e uma cegueira conhecida da métrica

As cinco frases de `numeros-datas-valores` têm referência em **dígito** (`R$ 1.530,40`,
`03/10/2026`), porque é isso que você quer ver aparecer no campo — não "mil quinhentos e trinta".

Só que a métrica compara dígito com dígito, e **não sabe que "1530" e "mil quinhentos e trinta"
são a mesma coisa** (está documentado em `internal/benchmark/metrics/number_date.go`). Um
candidato que ouça perfeitamente mas escreva por extenso vai pontuar **zero** nessas cinco frases.

Isso não é um bug a esconder — para ditado, escrever `R$ 1.530,40` no campo **é** o comportamento
certo, e escrever por extenso **é** pior. Mas é uma medida de *formatação*, não de *audição*, e o
relatório precisa ser lido com isso em mente.

Por isso os dígitos ficam **concentrados nessa única categoria**: as outras 41 frases não têm
número nenhum, então o WER agregado delas não é contaminado por essa cegueira.

---

## Como gravar

O gravador mostra a frase, você aperta ENTER, fala, aperta ENTER de novo. Ele salva o WAV com o
nome certo, no lugar certo, e passa para a próxima.

> O gravador usa a **mesma captura WASAPI** que o Agent já usa e que já foi validada contra o seu
> microfone no Sprint 1 e 2 — inclusive a correção do PCM de 32 bits. Ele **não** instala hook de
> teclado, **não** mexe no clipboard e **não** cria janela: lê só o ENTER do próprio terminal.

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/asrrec -manifest C:/Desenvolvimento/aurora-whisper/testdata/asr-dataset/manifest.json
```

Se quiser regravar uma frase específica depois, pelo ID que aparece no progresso:

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/asrrec -manifest C:/Desenvolvimento/aurora-whisper/testdata/asr-dataset/manifest.json -redo cont-03
```

**Controles a cada frase:**

| Tecla | O que faz |
|---|---|
| `ENTER` | começa a gravar / encerra a gravação / aceita e vai para a próxima |
| `r` | regrava a mesma frase (use sem dó — errar a leitura é normal) |
| `s` | pula esta frase |
| `q` | salva o progresso e sai; rodar de novo continua daqui |

`Ctrl+C` também funciona e encerra na hora, liberando o microfone — mas a frase que estiver
gravando naquele instante se perde. Tudo que você já aceitou está salvo em disco. Prefira `q`
quando der.

---

## Condições de gravação

Onze das doze categorias são para gravar do jeito normal: **ambiente silencioso, microfone na
posição de sempre, fala natural**. Não impostar a voz, não falar mais devagar que o normal — o
produto vai te transcrever falando como você fala.

Três categorias pedem condição diferente, e o gravador avisa quando chegar nelas:

| Categoria | Como gravar |
|---|---|
| `ruido-moderado` (3 frases) | com ruído de fundo real — TV ligada, ventilador, janela aberta para a rua. Não ruído artificial, e não tão alto que você mesmo não se ouça |
| `fala-rapida` (3 frases) | rápido de verdade, do jeito que você fala quando está com pressa |
| `1-3s` (3 frases) | são frases curtas; só leia naturalmente, sem arrastar para "encher" o tempo |

Para todas: **comece a falar cerca de meio segundo depois de apertar ENTER** e espere meio segundo
antes de encerrar. Áudio cortado no início é o erro mais comum de dataset, e ele desclassifica o
candidato por um defeito que é da gravação.

---

## O áudio nunca entra no git

O `.gitignore` bloqueia `*.wav` — invariante `AC-028`. O que fica versionado é só o
**manifesto** (`testdata/asr-dataset/manifest.json`), que tem apenas texto: as frases, as
categorias e as entidades esperadas.

Os arquivos de áudio ficam em `testdata/asr-dataset/audio/`, na sua máquina, e o relatório de
benchmark os referencia por **hash**, não por conteúdo.

Já existe teste automatizado garantindo isso: `TestManifestVersionado_ApontaParaAudioForaDoGit`
falha se alguém acrescentar uma amostra com extensão que o `.gitignore` não cobre.

---

## O manifesto já foi validado — mas não contra a sua voz

Antes de te pedir 46 leituras, passei o manifesto pelas próprias métricas que vão pontuá-lo. Três
testes permanentes ficaram disso, em `internal/benchmark/dataset/manifest_consistency_test.go`:

- **toda entidade declarada aparece de fato na frase.** Se eu tivesse escrito `EFD Reinf` na lista
  e `EFD-Reinf` na frase, aquele acerto seria **impossível** — e o benchmark não acusaria erro
  nenhum, só reportaria Entity Accuracy mais baixa, e a culpa cairia no modelo de ASR em vez de no
  meu erro de digitação.
- **as 12 categorias têm pelo menos uma amostra.** Categoria vazia não gera erro em lugar nenhum:
  o relatório simplesmente não teria linha para ela.
- **toda frase de `numeros-datas-valores` tem número extraível.** Uma sem dígito daria "100% de
  acerto" sobre zero itens.

Verifiquei que os três falham quando o defeito é reintroduzido de propósito — não são testes
decorativos.

Duas frases foram **descartadas** nessa validação, e vale saber por quê: uma com CNPJ
(`12.345.678/0001-95` se fragmenta em cinco números soltos, sendo um deles `1`, vindo de `0001`) e
uma com `competência 08/2026` (não é reconhecida como data, vira `8` e `2026`). As duas mediriam
o parser de números, não o ASR.

---

## O que me reportar quando terminar

1. **quantas das 46 você gravou** (se parou antes, tudo bem — diga onde parou)
2. **quais você regravou muitas vezes**, e por quê: travou na leitura, ou o gravador acusou áudio
   baixo/clipping?
3. **se o gravador avisou de algum problema** — silêncio, nível baixo, clipping
4. **as últimas linhas do terminal**, se aparecer `WARN` ou `ERROR`

Com o áudio na mão, o passo seguinte é escolher os candidatos de ASR e rodar o `asrbench`. **Eu
não escolho o vencedor** — o harness produz os números e confronta com os gates; a decisão é sua,
registrada em `docs/benchmarks/`, conforme o `ADR-012`.

---

## O que este roteiro NÃO resolve

- **O vocabulário ainda é o meu chute do seu.** Saiu do que apareceu nas nossas conversas
  (DCTFWeb, PGDAS, eSocial, n8n, Supabase e nomes de provedores de hospedagem). Se tem termo que você fala todo dia e não
  está aqui, me diga — é barato acrescentar antes de você gravar, e caro depois.
- **Não cobre ditado longo.** A frase mais longa tem ~10 s, que é o limite do `02-prd.md`. Ditar
  um parágrafo inteiro é outro regime e não está medido.
- **Não mede o dicionário.** O harness mede ASR **bruto**; o gate de `Entity Accuracy ≥95%` do
  eval-plan é *depois* do dicionário. São números diferentes, e o relatório marca a linha do gate
  real como **NÃO MEDIDO** justamente para os dois não serem confundidos.
