# Sprint 5 — o portão de entidades, e como ele é medido

## O portão

Duas metas do `19-eval-plan.md` e do `02-prd.md`, que precisam ser batidas **ao
mesmo tempo**:

| meta | valor |
|---|---|
| Entity Accuracy após dictionary | **≥95%** |
| False Correction Smart | **≤1%** |

Elas puxam em direções opostas de propósito. Um dicionário agressivo o bastante
para chegar a 100% de acurácia quase certamente estraga texto que já estava
certo — e esse dano aparece justamente onde a pessoa não está conferindo.

## Por que um comando novo, e não o `asrbench`

`cmd/asrbench` existe para **comparar candidatos de ASR**. Esse trabalho foi
concluído em 18-set-2026 (`benchmarks/asr-decisao-2026-09-18.md`) e não deve ser
mexido para afinar dicionário. Os relatórios dele dizem, explicitamente, que a
linha "após dictionary" é **NÃO MEDIDA** — porque `internal/dictionary` nunca é
chamado lá.

Afinar dicionário é outra medição:

- roda **muitas vezes seguidas** enquanto os alias são ajustados;
- precisa do **diff por entidade**, não de uma média;
- não pode arriscar a comparação de candidatos que já está fechada.

Por isso: `cmd/entitybench`, com a lógica pura em `internal/benchmark/entitygate`.

## Os quatro desfechos

Para cada entidade esperada, o comando classifica o que aconteceu. Três deles
pedem ações **opostas**, e um agregado que os some esconde a informação que
decide se o dicionário pode ser liberado:

| desfecho | significado |
|---|---|
| `ja_estava_certa` | o ASR acertou e o resolver não estragou |
| `corrigida` | o ASR errou e o resolver consertou — **ganho** |
| `continua_errada` | o ASR errou e o resolver não resolveu — **trabalho que falta** |
| `regrediu` | o ASR **acertou** e o resolver estragou — **dano** |

A taxa de correção falsa usa como denominador quantas estavam certas **antes**,
não o total: o risco que o portão mede é "o resolver estraga o que já
funcionava", e entidade que já estava errada não pode ser estragada.

## Como rodar

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/entitybench -mode literal
```

Modos: `nenhum` (controle), `literal`, `smart`. `-prompt "<vocabulário>"` passa
termos ao ASR como prompt inicial. `-diff=false` esconde o detalhe por entidade.

O comando **sai com código 1** quando o portão não passa, então serve como
verificação e não só como relatório de tela.

## Linha de base — 27-set-2026

Dataset `manifest-normalizado.json`, 46 amostras, 49 entidades esperadas,
configuração de ASR de produção (`small-q4_0`, `-t 12`, `-ac 750`).

| resolver | bruta | resolvida | ganhos | regressões |
|---|---|---|---|---|
| `nenhum` | 61,2% (30/49) | 61,2% (30/49) | 0 | 0 |
| `literal` | 61,2% (30/49) | 61,2% (30/49) | 0 | 0 |

**Os dois são controles, e os dois passaram.**

- `nenhum` prova que o instrumento **não inventa diferença**: sem resolver, os
  dois números são idênticos.
- `literal` prova que o `LiteralResolver` **não toca em entidade** — o `AC-023`
  ("Literal não corrige") verificado contra áudio real, não contra dublê.

Faltam **17 entidades** para o portão: 47 de 49, com no máximo 2 erros.

## O que já foi medido fora deste comando

Antes de escrever o `entitybench`, três medições exploratórias orientaram o
plano do sprint (registradas aqui porque o número sozinho não diz de onde veio):

| configuração | Entity Accuracy | custo de latência |
|---|---|---|
| sem prompt | 61,2% | — |
| prompt com 13 termos | 73,5% | +40 ms |
| prompt com 36 termos | 79,6% | +146 ms |

O prompt curto é **2,4x mais eficiente por milissegundo**. O longo tem retorno
decrescente claro.

**Isso tensiona o portão de latência**: o p50 de produção já é 565 ms contra
meta de 500 ms (`asr-decisao-2026-09-18.md`), e o prompt piora um estouro que já
existia — 7% com o prompt curto, 42% com o longo. Decisão de Mauro pendente
sobre tratar os 500 ms como portão duro ou como meta.

## O achado que define o resto do sprint

`internal/resolver/smart.go` **só auto-aplica match de dicionário quando ele é
`exact` ou `alias`**. Match **fonético** sozinho é tratado como evidência fraca
e só é aplicado se o contexto corroborar — e a captura de contexto (TASK-009)
**não existe**, é só contrato.

Os erros restantes do ASR são quase todos quase-acertos fonéticos (`DAAS`→`DAS`,
`Supabaese`→`Supabase`, `Vit`→`Vite`). Então, em produção, o dicionário como
está consertaria **zero** deles.

Isso não é defeito: é o freio deliberado contra correção falsa, e está certo. O
caminho é **alias** — variante já confirmada, que é auto-aplicada. E alias é
exatamente o que o `US-007` ("ensinar correções") e o `AC-022` ("aprendizado
exige confirmação") descrevem.

> O portão de 95% não se atinge com um casador esperto. Atinge-se com o laço de
> aprendizado funcionando.

## Honestidade sobre a medição

Aprender alias a partir dos **mesmos 46 áudios** usados para medir faz o número
validar a si mesmo. Serão reportados **dois**:

1. **O portão da spec** — alias aprendidos, medido no dataset aprovado. É o que
   o `19-eval-plan.md` pede ("no dataset aprovado").
2. **A generalização** — aprender numa metade, medir na outra. Vai ser pior, e é
   o número que diz se funciona para termo que o dicionário não conhece.

---

## Fatia 2 — vocabulário no ASR (27-set-2026)

`asr.ASROptions.Vocabulary` passa termos ao decodificador — o "initial prompt"
do whisper.cpp. `dictionary.Vocabulario` monta esse texto a partir das entradas
do dicionário pessoal, **limitado por orçamento**.

### Por que limitado

Um dicionário pessoal cresce a cada termo aprendido. Sem orçamento, o ditado
ficaria mais lento para sempre — de forma gradual e sem causa visível, que é o
pior tipo de regressão. O corte é por **runas**, não por número de termos,
porque o custo acompanha o tamanho do prompt: "Metalúrgica Toffanetti" custa o
que três siglas custam.

Quem entra é decidido por `Priority` decrescente — o mesmo critério que
`Search` usa — com desempate alfabético para o prompt ser **determinístico** e a
medição, reproduzível.

### Verificado contra o servidor REAL

O teste de fiação usa um servidor falso que aceitaria qualquer nome de campo. Se
o whisper-server esperasse outro nome, o teste ficaria verde e produção não
faria nada — a classe de defeito que já apareceu três vezes neste projeto.

Então foi medido pelo caminho de produção (`internal/asr/local.Provider` +
whisper-server real, quente):

| amostra | sem prompt | com prompt |
|---|---|---|
| `cont-01` | `DCTF Web` | **`DCTFWeb`** |
| `cs-03` | `End Point` / `Time Out` / `BackEnd` | **`EndPoint`** / **`timeout`** / **`backend`** |
| `dev-02` | `seu post de grias` | **`postgreSQL`** |

Quatro entidades consertadas **na decodificação**, antes de existir texto para
corrigir — mais confiável que casar string depois.

### O custo, no caminho de produção

| amostra | sem | com |
|---|---|---|
| `dev-02` | 546 ms | 588 ms |
| `cont-01` | 528 ms | 594 ms |
| `cs-03` | 515 ms | 571 ms |

**+55 ms em média.** São três amostras, não um p50 — mas é a primeira medição
feita no caminho real (servidor quente), e não no `whisper-cli` frio. O p50
publicado era 565 ms; com vocabulário, a ordem de grandeza é ~585 ms.

A decisão sobre tratar os 500 ms como portão duro continua pendente e agora tem
número concreto para ser tomada.

---

## Fatia 3 — dicionário populado (27-set-2026)

### A progressão, medida

| etapa | Entity Accuracy | ganhos | regressões |
|---|---|---|---|
| linha de base, ASR cru | 61,2% (30/49) | — | — |
| + vocabulário do dicionário no ASR | 75,5% (37/49) | — | — |
| + resolver Smart, sem alias | 77,6% (38/49) | 1 | **0** |
| + alias seguros | **81,6%** (40/49) | 3 | **0** |
| meta | 95% (47/49) | | |

**Correção falsa ficou em 0,00% o tempo todo.** Nenhuma entidade que já estava
certa foi estragada em nenhuma das configurações.

### Dois defeitos no resolver, achados ao ligar o dicionário

**1. Match exato não corrigia grafia.** `dictionary.Search` tolera espaçamento
(`CollapseSpaces` compara "dctf web" com "dctfweb"), então `"DCTF Web"` casa
como **exato** — e o ramo exato do `smart.go` não fazia nada. O resolver era
estruturalmente incapaz de consertar erro de fronteira de token, que é boa parte
do que o ASR erra.

**2. Evidência fraca engolia evidência forte.** O laço de frases ia da mais longa
para a mais curta e parava no primeiro acerto:

```
"DCTF Web hoje" -> phonetic (0,48)   <- ganhava e CONSUMIA os tokens
"DCTF Web"      -> exact    (1,00)   <- nunca era tentado
```

Isolado, o termo era corrigido; **dentro de uma frase, não** — que é o único jeito
que o produto é usado. Agora a força do match decide primeiro.

### O ASR não é determinístico

Os 8 alias adicionados renderam 3 ganhos. Parte da diferença é que o vocabulário
já tinha consertado alguns; parte é que **a saída do ASR varia entre execuções**
em algumas amostras: `cont-02` deu `PGDAS` correto numa rodada e `PGDAAS` na
seguinte, com a mesma configuração.

> **Correção (27-set-2026).** A primeira versão deste parágrafo citava 57,1%,
> 59,2% e 61,2% como "a linha de base medida em rodadas diferentes". Estava
> errado: esses três números vêm de `asr-2026-09-18-cpu-audioctx.md` e são de
> **candidatos de ASR diferentes** (cold, warm, audio-ctx), não de repetições da
> mesma configuração. Usá-los como evidência de variância exagerou o efeito.
>
> Rodando a MESMA configuração duas vezes em 27-set-2026, os agregados
> reproduziram exatamente: 95,9% no dataset completo e 70,8% no holdout, com os
> mesmos brutos (77,6% e 66,7%). A variância por amostra é real; o agregado tem
> se mostrado estável.

Ainda assim, afinar alias contra uma única execução persegue um alvo que se
move em algumas amostras — e continua sendo motivo para não caçar os últimos
pontos com string matching.

### Por que os 95% NÃO foram atrás dos alias que faltavam

Os três alias que valeriam os pontos restantes colidem com palavra comum do
português:

| alias | canônico | o que quebraria |
|---|---|---|
| `comite` | `commit` | **comitê**: "o comitê aprovou" → "o commit aprovou" |
| `DRA` | `DRE` | **Dra.**: "Dra. Silva" → "DRE Silva" |
| `postgres` | `PostgreSQL` | **postgres** é termo legítimo (o daemon, o usuário do banco) |

E aqui está a armadilha: o portão de correção falsa é medido **neste dataset**,
que não tem nenhuma frase sobre comitê nem nenhuma Dra. Adicionar esses alias
mostraria **0% de correção falsa na medição** e causaria dano real no uso diário.

Bater o número e piorar o produto é pior que não bater o número.

### A saída certa é o contexto, não mais string matching

`DictionaryEntry` tem **`Domain`**, e `commit` está marcado como `dev`. Se o
resolver soubesse que a janela em foco é um editor de código, o alias
`comite`→`commit` dispararia só ali — e nunca num e-mail sobre o comitê.

Isso é a **TASK-009**, que este documento antes classificava como fora do caminho
crítico. **Estava errado.** Os últimos pontos não vêm de casar string melhor;
vêm de saber onde a pessoa está digitando.

### Um conflito de spec que precisa de decisão

A amostra `corr-01` tem referência *"Manda para a Rosângela... não, na verdade
manda para o Thiago."* e espera as entidades `Rosângela` **e** `Thiago`.

Mas a autocorreção do produto **remove deliberadamente** a parte retratada, e a
métrica compara contra a referência, que a mantém. Duas funcionalidades do
produto se contradizem nessa amostra, e nenhuma das duas está errada.

Ou a referência dessa amostra passa a ser o texto JÁ autocorrigido, ou as
entidades esperadas dela não devem incluir o que foi retratado. É decisão de
Mauro.

---

## Fatia 4 — o Agent usa o dicionário (27-set-2026)

`stubResolve` acabou. O `Runtime` chama o resolver de verdade, e o Agent carrega
o dicionário de `dicionario.json` **ao lado do executável** — mesmo critério do
modelo de ASR: o Agent abre por atalho, autostart ou terminal, e o diretório de
trabalho não é confiável para achar arquivo de instalação.

O `asrstage` passa a levar o dicionário para `bin/` junto com o modelo.

### Síncrono, e por quê

`BenchmarkSmartResolve_DicionarioReal`: **2,3 ms** numa frase de 25 palavras com
43 termos no dicionário.

A transcrição foi para outra goroutine porque custa ~500 ms e o `T20` do roteiro
manual existe por causa disso. Aqui não se aplica: o hook de teclado entrega por
canal e **não espera** o laço de eventos — o orçamento de ~300 ms por callback é
do Decider, não do `Run`. Então 2,3 ms no laço são inofensivos, e um caminho
assíncrono a mais seria complexidade sem motivo.

O que **não** é inofensivo é o crescimento: o custo escala com tokens VEZES
tamanho do dicionário, e o dicionário cresce a cada termo aprendido. Por isso há
um aviso em log acima de `limiarResolveLento` (25 ms, uma ordem de grandeza acima
do medido). Passar disso significa que o dicionário merece índice — não que o
ditado quebrou.

### Duas regras que os testes travam

**Falha de resolução nunca perde o ditado.** Dicionário corrompido, termo
malformado, bug no resolver — o texto do ASR segue para inserção de qualquer
jeito. Sem isso o modo de falha seria: a pessoa fala, o overlay mostra sucesso, e
nada aparece no campo.

**Dicionário ausente não é erro.** O Agent sobe e avisa em log. Recusar subir por
falta de um arquivo opcional trocaria uma degradação suave por uma falha dura.
Arquivo **presente e inválido**, esse sim é erro: significa que alguém tentou
configurar um dicionário e ele não vai funcionar.

### O log não carrega o texto

`winruntime: resolucao aplicada` registra quantas substituições e quantos spans
ficaram incertos — nunca o transcript, que é proibido (`AC-027`).

---

## Verificação em máquina real — 27-set-2026

O `T21`, pendente desde o Sprint 4, foi executado por Mauro:

**Ditado:** *"precisa transmitir a DCTFWeb antes do dia quinze"*
**Campo recebeu:** `Precisa transmitir a DCTFWeb antes do dia 15.`

`DCTFWeb` saiu **correto**. No Sprint 4 a previsão registrada era que sairia
"DCTF Web" — e saía.

### O detalhe que vale mais que o acerto

O log **não tem** nenhuma linha `winruntime: resolucao aplicada`. Isso significa
zero substituições: **o ASR acertou na origem**, pelo viés do vocabulário. A
camada de correção do dicionário nem precisou entrar.

É o desenho funcionando na ordem certa — consertar na decodificação é mais
confiável que casar string depois.

### Latência

`duracao=559,8ms` **com** vocabulário, contra um p50 publicado de 565 ms sem.
Uma amostra só, então não é um p50 — mas o custo de +55 ms medido em bancada não
apareceu aqui. Vale remedir com N maior antes de concluir qualquer coisa.

## O orçamento do vocabulário já está saturado

| | |
|---|---|
| termos no dicionário | 43 |
| termos que cabem no vocabulário | **11** |
| runas usadas | 139 de 140 |

E três nomes próprios longos consomem **61 runas — 44% do orçamento**:

```
Metalúrgica Toffanetti  (22)
Rosângela Bittencourt   (21)
Thiago Vasconcelos      (18)
```

Os três custam o mesmo que nove siglas curtas. Ficaram **de fora**: `DAS`, `DRE`,
`MEI`, `ECD`, `eSocial`, `Vite`, `n8n`, `commit`, `endpoint`, `timeout`, e mais
22.

Isso não é defeito do orçamento — é o orçamento fazendo o trabalho dele, que é
tornar o custo visível em vez de deixar o ditado ficar lento sozinho. Mas é uma
decisão que precisa ser tomada, e é de Mauro:

1. **Baixar a prioridade dos nomes longos.** Eles são caros e o ASR pode acertá-los
   por contexto; as siglas curtas não têm essa sorte.
2. **Aumentar o orçamento.** Custa latência medida, e o p50 já está acima da meta.
3. **Aceitar 11 termos** e deixar o resto para a camada de alias.

O que NÃO dá é o dicionário crescer sem que uma dessas três coisas aconteça — a
cada termo novo, uma fração menor recebe o viés do ASR.

---

## TASK-009, primeira parte — domínio por app (27-set-2026)

**A parte valiosa não precisou de COM nenhum.** `ResolveEffect.Focus.ProcessName`
já chegava ao runtime desde o Sprint 2; faltava usá-lo. Nada de `TextPattern`,
nada de `ValuePattern`.

O caminho: o dicionário mapeia domínio → executáveis, o runtime traduz o app em
foco, e o `Search` filtra.

### A armadilha do filtro existente

```go
if query.Domain != nil && entry.Domain != nil && *entry.Domain != *query.Domain
```

Consulta com domínio **nulo** não restringe nada — certo para LOOKUP, e
exatamente invertido para segurança: o alias perigoso dispararia justamente onde
não se sabe onde a pessoa está.

Não mexi nessa semântica (quebraria a busca e a medição). `DictionaryEntry.
Restricted` é aditivo e opt-in; entrada comum se comporta como antes.

**A invariante, travada por teste:** domínio DESCONHECIDO se comporta como o
domínio ERRADO, nunca como o certo. Quanto menos se sabe sobre onde a pessoa
está digitando, **menos** o dicionário mexe no texto.

### O resultado

| etapa | Entity Accuracy | regressões |
|---|---|---|
| linha de base | 61,2% (30/49) | — |
| + vocabulário no ASR | 75,5% (37/49) | — |
| + vocabulário rebalanceado | 77,6% (38/49) | — |
| + alias seguros | 81,6% (40/49) | 0 |
| **+ alias perigosos, restritos por domínio** | **83,7% (41/49)** | **0** |
| meta | 95% (47/49) | |

Correção falsa **0,00% em todas as etapas**.

### Um erro de método meu, que quase virou conclusão errada

A primeira medição com domínio rodou `-domain dev` para o dataset inteiro, e deu
81,6% — igual a não ter domínio nenhum. A leitura natural seria "o mecanismo não
funciona".

Funcionava. `DAS`, `PGDAS` e `EFD-Reinf` têm domínio `contabil`, e fixar "dev"
**desligou o dicionário contábil inteiro**: a medição ganhou `commit` e perdeu
`DAS` e `EFD-Reinf`. Troca, não ganho.

Em produção o problema não existe — o app em foco decide a cada ditado. Era a
ferramenta que precisava alcançar isso, e `-domain auto` passou a deduzir o
domínio da categoria de cada amostra.

É o mesmo erro do "espalhamento de 38 px" da onda, em outra roupa: **o número não
mentiu, o experimento é que estava mal montado.**

### E uma cegueira na própria ferramenta

O domínio não aparecia no cabeçalho do relatório. Rodei uma medição com
`-domain dev` sem ter como saber, pela saída, se a flag tinha pegado. Agora tem
teste exigindo que duas execuções com domínios diferentes produzam saídas
distinguíveis.

---

## PORTÃO BATIDO — 27-set-2026

```
Entity Accuracy RESOLVIDA    95,9%  (47/49)   meta >= 95%   PASSOU
correção falsa               0,00%            meta <= 1%    PASSOU
```

| etapa | Entity Accuracy | regressões |
|---|---|---|
| linha de base, ASR cru | 61,2% (30/49) | — |
| + vocabulário no ASR | 75,5% | — |
| + vocabulário rebalanceado | 77,6% | — |
| + alias seguros | 81,6% | 0 |
| + alias restritos por domínio | 83,7% | 0 |
| + palavra forte deixa de ser engolida | 87,8% | 0 |
| + restrição por alias, não por entrada | 93,9% | 0 |
| **+ palavra forte em qualquer posição** | **95,9% (47/49)** | **0** |

**Correção falsa 0,00% nas oito etapas.**

### De onde veio o ganho

| origem | contribuição aproximada |
|---|---|
| vocabulário no ASR (consertar na origem) | ~16 pontos |
| **defeitos de ordenação no resolver** | **~14 pontos** |
| alias e prioridades | ~5 pontos |

**Quase metade veio de consertar bugs, não de adicionar dado.** O dicionário já
existia antes deste sprint; ele não funcionava porque o resolver o consultava
errado.

### O defeito, e as três vezes que eu o consertei pela metade

Todas as três instâncias têm a mesma forma: **uma frase de casamento fraco
consumia tokens cujo casamento individual era mais forte.**

```
"DCTF Web hoje" (phonetic 0,48) engolia "DCTF Web" (exact 1,00)
"Supabaese e"   (phonetic)      engolia "Supabaese" (alias)
"e o EFDRinf"   (phonetic)      engolia "EFDRinf"   (alias)
```

| conserto | o que tratava |
|---|---|
| 1º | comparar frases entre si |
| 2º | comparar com a palavra INICIAL |
| 3º | comparar com QUALQUER palavra dentro da frase |

Os dois primeiros funcionaram, e cada um me deixou achar que tinha resolvido. Só
o terceiro caso — token forte na **terceira** posição — mostrou a forma real.

O que expôs os três foi o **diff por entidade**. Olhando a média, eu teria parado
no primeiro conserto com o número subindo e dois defeitos vivos.

### As duas entidades que continuam falhando

1. **`Rosângela` em `corr-01`** — o conflito de spec: a autocorreção remove a
   parte retratada, como foi desenhada para fazer, e a referência a mantém.
   Continua precisando de decisão.
2. **`Rosângela Bittencourt` em `nome-02`** — o ASR produziu "Ros?ngela bit
   encurpe de um". Destruído; não tem conserto por dicionário.

O portão passou **com as duas ainda falhando** — não depende de resolver o
conflito de spec.

### A ressalva que continua valendo

Os alias foram aprendidos dos **mesmos 46 áudios** usados para medir. Para um
dicionário pessoal isso é legítimo — a premissa do produto é "são os seus
termos" —, mas **o número não generaliza** para termo que o dicionário não
conhece.

O segundo número prometido (aprender numa metade, medir na outra) **ainda não
foi medido**, e sem ele este 95,9% vale para o dataset aprovado e nada além.

---

## O segundo número: generalização — 27-set-2026

Prometido desde a Fatia 3, e agora medido: **alias aprendidos de metade do
dataset, medidos na outra metade.**

```
recorte: HOLDOUT — 23 amostras de teste, alias delas removidos

  Entity Accuracy BRUTA        66,7%  (16/24)
  Entity Accuracy RESOLVIDA    70,8%  (17/24)   meta >= 95%  NAO PASSOU
  correção falsa               0,00%                          PASSOU
```

| | Entity Accuracy | quanto o resolver somou |
|---|---|---|
| dataset completo | **95,9%** (47/49) | **+18,3 pontos** |
| holdout — erros nunca vistos | **70,8%** (17/24) | **+4,2 pontos** |

### O que este número diz

**Quase toda a força de correção do dicionário vem de alias aprendidos do erro
exato.** Para um erro que ele nunca viu, o dicionário soma pouco.

Isso não é defeito — é a consequência direta de uma decisão de segurança tomada
de propósito: **match fonético sozinho não é auto-aplicado.** É o freio que
mantém a correção falsa em 0,00%, e é exatamente ele que limita a
generalização. As duas coisas são o mesmo mecanismo visto de dois lados.

Corrigir erro nunca visto exigiria afrouxar esse freio, e o preço apareceria em
texto que a pessoa não está conferindo.

### O que isso significa para o produto

O `95,9%` é verdadeiro para o dataset aprovado — que é o que o `19-eval-plan.md`
pede — e **não prevê** o comportamento com termo ou erro novo. Um dicionário
recém-instalado, só com formas canônicas, entrega algo perto de **70%**, não de
95%.

A diferença entre os dois números é o valor do **laço de aprendizado**: a pessoa
dita, vê o erro, confirma a correção, o dicionário aprende o alias. É o `US-007`
e o `AC-022`, e é o que transforma 70% em 96% ao longo do uso.

Ou seja: o produto não nasce em 95,9%. Ele **chega** lá, para cada pessoa, na
medida em que ela o corrige.

### O que ainda não sabemos

Quanto tempo de uso real leva para um dicionário sair de 70% e chegar perto de
95%. Isso não é medível em bancada — depende de quantos termos distintos a
pessoa usa e de com que frequência os repete.

---

## O laço de aprendizado — 27-set-2026

A medição de generalização disse que a diferença entre **70,8%** e **95,9%** é
exatamente o valor de ensinar o dicionário. Até agora não havia nenhum jeito de
ensiná-lo a não ser editar o JSON à mão.

`cmd/dictlearn` fecha esse laço:

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/dictlearn \
  -dict testdata/asr-dataset/dicionario.json -saiu "Supabaese" -queria "Supabase"
```

Mostra a classificação, pede confirmação, e só então grava.

### Ele usa o `internal/learning` real

A garantia do `AC-022` — nada é gravado sem confirmação explícita — é
**estrutural**: `Confirmation` tem o campo booleano não exportado, e a única
forma de obter "sim" é `ConfirmedByUser`. Zero-value é sempre "não".

Reimplementar a regra aqui teria dado o mesmo resultado nos testes e perdido a
garantia. O comando decide **quando** perguntar; o pacote decide **se** pode
gravar.

### Persistência: JSON, não SQLite

O `04-system-design.md` aponta SQLite para o Windows, e para histórico e escala
é a escolha certa. Para o **dicionário**, hoje, não é:

- são 4,4 KB e algumas dezenas de termos;
- a pessoa consegue **abrir e auditar** o que o produto aprendeu sobre ela;
- o arquivo é versionado, então cada aprendizado vira um diff legível;
- não exige dependência de produção nova.

A escrita é **atômica** (temporário + rename): um dicionário truncado faria o
Agent recusar subir, e a pessoa perderia o ditado inteiro por causa de um alias.

Quando o dicionário crescer a ponto de a leitura linear incomodar — o mesmo
ponto em que `winruntime.limiarResolveLento` começa a avisar —, a troca entra
atrás do mesmo `DictionaryRepository`, que já é interface por isso.

### Um limite do fluxo da spec que apareceu no caminho

`learning.Suggestion` usa `Before`/`After` **inteiros** como alias e forma
canônica, e `AliasEligible` limita por número de palavras e caracteres.

Consequência: **uma correção de uma palavra dentro de uma frase longa nunca vira
alias.** O `Before` é a frase inteira e estoura o teto; a edição vira só uma
`Correction` no histórico.

Isso significa que o fluxo do `07-flows.md` — editar o `final_text` no dashboard
— raramente produziria alias, que é justamente o que a generalização precisa.

**RESOLVIDO em 27-set-2026.** `Classify` passou a extrair o trecho mínimo que
mudou (`AliasFrom`/`AliasTo`), descartando prefixo e sufixo comuns em
granularidade de PALAVRA. `Before`/`After` continuam sendo os textos inteiros —
eles são o fato histórico da `Correction`; o trecho é o que vira regra.

Agora `"sobe a migration no Supabaese e confere"` → `"...no Supabase e confere"`
produz o alias `"Supabaese" -> "Supabase"`, e não a frase de sete palavras.

### E um segundo problema que isso abriu

A elegibilidade proibia **pontuação de frase**, e isso barrava — por acidente —
edições como `"isso está certo?"` → `"isso está errado!"`. Ao passar a olhar o
trecho, a pontuação some junto com o resto da frase, e `"certo"` → `"errado"`
vira uma edição pequena e perfeitamente elegível.

Aprender esse alias faria **todo ditado futuro com "certo" virar "errado"**.

Tentei um portão de proximidade: os dois termos precisariam ser confundíveis.
Mas os números reais mataram a ideia:

| par | proximidade | é erro de transcrição? |
|---|---|---|
| `Supabaese` / `Supabase` | 0,89 | sim |
| `comite` / `commit` | 0,67 | sim |
| `amanhã de manhã` / `depois de amanhã bem` | 0,45 | não |
| `certo` / `errado` | 0,33 | não |
| **`n oito n` / `n8n`** | **0,25** | **sim — e é o TEST-008 da spec** |

O caso que a spec **exige** que seja aprendido está do lado mais distante de
todos. Qualquer corte que o aceitasse aceitaria `"certo"` → `"errado"` junto —
"oito" e "8" são a mesma fala, e nenhuma medida ortográfica ou fonética
disponível aqui enxerga isso.

Escolher um limiar que coubesse nos exemplos que eu tinha na mão seria ajustar a
regra de segurança aos dados. A spec já tem o mecanismo certo para este
julgamento: **confirmação explícita** (`AC-022`).

Então a proximidade virou **aviso medido**, não veto: `dictlearn` mostra o número
e explica o risco, e quem confirma decide com o dado na frente.

### O que ainda falta para o laço da spec

| peça | estado |
|---|---|
| `learning.Classify` / `Confirm` | pronto |
| persistência do alias | **pronto** (`dictlearn`) |
| Agent persistir o ditado | não faz |
| Agent servir a API local de histórico | não faz |
| extrair o par de palavras de uma edição longa | não existe |

---

## Uso real — 27-set-2026, noite

Mauro ditou:

> "Testando, testando, **DCTFWeb**, **Simples Nacional**, Quero que você faça
> uma análise do Balancete e coloque isso na minha mesa até amanhã de manhã."

**`DCTFWeb` e `Simples Nacional` saíram corretos.** `Simples Nacional` era uma
das entidades que falhavam antes dos consertos de ordenação do resolver.

Relato dele: *"Funcionou legal. A wave tá linda."*

### E uma observação dele virou defeito encontrado

> "notei que se eu não estiver num campo de texto ele deixa eu clicar em um antes
> de colar"

Isso é o fallback do `ADR-011`, decisão 3 funcionando: quando a inserção falha
nos dois caminhos (SendInput e clipboard+Ctrl+V), o texto ditado **fica retido na
área de transferência** de propósito, para não se perder.

O preço, que vale saber: nesse caminho o *restore* do clipboard **não acontece**,
então o que estava copiado antes é perdido. A troca é "perder o que você copiou"
contra "perder o que você acabou de falar", e a segunda é pior. A decisão está
certa.

**Mas a tela estava mentindo.** `overlayPhaseFor` escolhia a fase só pelo
DESFECHO, e `OutcomeError` cobre coisas que pedem ações **opostas**:

| o que falhou | o que a pessoa deve fazer | o que a tela dizia |
|---|---|---|
| transcrição | ditar de novo | "Não consegui transcrever. Tente novamente." |
| **inserção** | **Ctrl+V** | "Não consegui transcrever. Tente novamente." |

Uma mensagem de erro que descreve o erro errado é pior que nenhuma: ela faz a
pessoa **refazer trabalho que já está pronto**.

Agora há uma fase própria, `OverlayClipboard`, com a mensagem *"Texto na área de
transferência: Ctrl+V."* — que diz o que **fazer**, não o que falhou. Quem está
com o overlay na frente quer resolver, não diagnosticar.

---

## O vocabulário piora o texto comum? — 28-set-2026

Mauro ditou **"o vencimento é no dia doze"** — sem nenhum termo técnico — e
recebeu `Ovencimento ano dia 12`. A suspeita: o prompt de vocabulário, que
condiciona o decodificador com 140 runas de termos, estaria estragando a frase
comum para acertar a técnica.

Nunca tinha sido medido. Eu tinha medido só o que queria que melhorasse.

### A primeira medição estava enviesada

`entitybench` só transcrevia amostras **com entidade esperada** — economia que
parecia inofensiva, porque só elas contam para o portão. Mas isso fazia o WER
ser medido exatamente no subconjunto cheio de termos técnicos, que é onde o
vocabulário mais ajuda:

```
N=25 (só com entidade):   WER com 0,192   sem 0,227   <- otimista por construção
```

Quase reportei isso como "o vocabulário melhora o WER em 15%". O `N=25` estava
impresso na própria saída; o que me fez olhar de novo foi o caso de Mauro não
ter entidade nenhuma.

### Medindo as 46

```
N=46 (todas):             WER com 0,249   sem 0,249
                          CER com 0,115   sem 0,117
```

**WER idêntico.** O vocabulário não melhora nem piora a transcrição geral — a
melhora aparente era inteiramente artefato do recorte.

| | WER bruto | Entity Accuracy bruta |
|---|---|---|
| com vocabulário | 0,249 | **78,7%** |
| sem vocabulário | 0,249 | 59,6% |

O valor do vocabulário é **inteiramente** em entidade, e é grande: 19 pontos. Sem
ele o portão não passa nem com o dicionário inteiro (83,0% contra 97,9%).

### O caso de Mauro não é regressão

WER 0,249 significa ~1 palavra errada a cada 4 — a qualidade medida deste modelo
nesta configuração, registrada desde o Sprint 4 (WER 0,26). "o vencimento é no
dia doze" tem 6 palavras; o esperado são ~1,5 erros. Ele teve 2.

Está dentro do normal medido. Não é o vocabulário, não é o inserter, não é o
resolver.

### E um achado de brinde

O resolver **piora levemente o WER** quando o vocabulário está ligado (0,249 →
0,261) e **melhora** quando está desligado (0,249 → 0,237).

Faz sentido: sem vocabulário o ASR erra mais e o resolver conserta mais (11
ganhos contra 9); com vocabulário o ASR já acerta as entidades, e o que sobra
para o resolver mexer tem mais chance de divergir da referência. Parte disso
também é a autocorreção removendo texto que a referência mantém — comportamento
correto que o WER penaliza.
