# Defeito: o ditado longo perde texto e entra em laço (01-out-2026)

Status: **corrigido no código, sem medição com áudio longo e sem teste manual.** O benchmark de
áudio longo e o roteiro manual estão no fim deste documento.

## Sintomas

Na manhã de 01-out-2026 o Mauro deu uma aula pelo Google Meet e ditou prompts longos (30 a 90 s)
com o Agent de `bin\`. Era um Agent anterior ao merge `dc37104`, mas o caminho do ditado é o mesmo
de `main`. O comportamento foi o mesmo no modo Polish e no Smart.

1. **Laço de repetição.** Um ditado de 59 s saiu com o começo certo e depois a mesma frase doze
   vezes ("e como cada um deles vai ser utilizado na hora de récu,"); o resto da fala sumiu.
2. **"Não consegui transcrever."** Ditados de 37 s e de 89 s terminaram em `outcome=timeout`: o
   Agent desistiu aos 10 s, e a transcrição chegou depois (18,8 s e 16,1 s de ASR). O texto pronto
   foi jogado fora, e o histórico ficou sem linha nenhuma.
3. **"Errando muito"** nos ditados longos, igual no Polish e no Smart.
4. **"Não conseguiu gravar"** várias vezes: os dois timeouts acima, e dois toques curtos que
   terminaram em `no_speech` (sem fala reconhecida; caminho normal de toque curto).

Os ditados longos da sessão, do `bin\agent.log` casado com o `bin\whisper-server.log`:

| tentativa | áudio | ASR | ASR/áudio | runas | desfecho |
|---|---|---|---|---|---|
| 3 | 27,5 s | 2,39 s | 0,09 | 362 | sucesso, texto coerente |
| 4 | 59,3 s | 8,13 s | 0,14 | 911 | sucesso, **laço** |
| 5 | 37,2 s | 18,84 s | 0,51 | 416 | **timeout**, texto perdido (chegou 8,8 s depois) |
| 8 | 29,3 s | 9,17 s | 0,31 | 808 | sucesso, 27,6 caracteres/s (suspeita de laço, não confirmada) |
| 9 | 88,9 s | 16,08 s | 0,18 | 1373 | **timeout**, texto perdido (chegou 6,1 s depois) |

## Causas

### Comprovadas (log e código)

- **O prazo da transcrição era fixo em 10 s** (`dictation.DefaultTranscribeTimeout`), e o ASR de
  um ditado longo leva mais que isso. O log mostra `transcrição concluída` depois de
  `dictation attempt finished outcome=timeout`.
- **O resultado que chegava depois era descartado em silêncio.** O núcleo descarta evento de
  tentativa encerrada (ADR-007, seção 5), e o histórico não gravava linha sem texto. A transcrição
  também não era cancelada: seguia ocupando a CPU e o servidor, e o ditado seguinte esperava atrás.
- **O `-ac 750` foi medido só com fala curta.** O dataset de 18-set-2026 tem no máximo 9,3 s de
  áudio; com 750 o encoder do whisper.cpp vê 15 s por vez, e o resto do laço de janelas dele supõe
  30 s (`docs/benchmarks/asr-decisao-2026-09-18.md`, nota de 01-out-2026).
- **O guarda contra laço do whisper.cpp não pega um laço desse tamanho.** O limiar de entropia
  padrão (2,4, sobre os últimos 32 tokens) só rejeita laços de até 11 tokens. A frase do incidente
  tem uns 15 a 20 tokens (estimativa: o tokenizador não foi rodado).
- **A captura estava inteira.** Os WAV que o servidor recebeu tinham 27,5 / 59,3 / 37,2 / 29,3 /
  88,9 s, iguais às gravações. A divisão do microfone com o Meet não explica a perda de texto.

### Hipóteses (o benchmark de áudio longo é que confirma)

- **H1, a principal: áudio acima de ~15 s com `-ac 750` quebra o laço de janelas.** No whisper.cpp
  b5130, a janela avança pelo último timestamp decodificado; quando a sequência termina num
  timestamp só, ela pula 30 s, descartando 15 s que o encoder nunca viu; e o texto de uma janela
  entra como prompt da seguinte, então um laço contamina o resto. O corte em "récu" cai a uns 12 a
  17 s de fala, compatível com a janela de 15 s. Contra: o ditado de 27,5 s saiu coerente, então
  é frágil, e não determinístico.
- **H2: o fallback de temperatura explica a lentidão variável** (razão ASR/áudio de 0,09 a 0,51 no
  mesmo dia). Cada tentativa rejeitada redecodifica até 220 tokens, com até 6 temperaturas. O
  `whisper-server.log` não registra as rejeições, então não dá para confirmar pelo log.
- **H3: a CPU disputada com o Meet** aumenta o tempo. Não medido.
- **H4, risco da correção:** com a flash attention, o whisper.cpp lê o K/V arredondado para
  múltiplos de 256 sem máscara. Com o servidor alternando a janela por requisição (a correção
  abaixo), a requisição de janela inteira lê 36 linhas que guardam o K/V da última requisição curta,
  em 6 das 12 camadas do decodificador. A variante intercalada do benchmark mede se isso muda o
  texto. É contaminação vinda do ditado anterior, a mesma classe do defeito "texto que a pessoa não
  ditou" que o projeto já teve: **o Agent de uso diário não é recompilado sem esse número** (ver
  "Ordem", no benchmark).
- **H5: o vocabulário sai do contexto no ditado longo.** No b5130, sem `carry_initial_prompt`, o
  prompt com o vocabulário do dicionário vai para o contexto que rola de uma janela de 30 s para a
  seguinte, e é empurrado para fora pelo texto já decodificado depois de uns 223 tokens
  (whisper.cpp:7046, 7077-7081, 7246, 7730-7740). Da segunda ou terceira janela em diante, os termos
  do dicionário deixam de enviesar o decodificador, o que pode contribuir para o "errando muito". O
  servidor aceita `carry_initial_prompt` por requisição (server.cpp:570); a variante `prompt-fixo`
  do benchmark mede.

"Errando muito" no Polish e no Smart é coerente com uma causa no ASR: os dois modos partem do mesmo
texto bruto. Mas a igualdade entre os dois modos tem também outra causa, comprovada no log: **o
Polish não agiu nos ditados longos.** A tentativa 8 (802 caracteres) caiu no Smart por `prazo`
(latência de 3469 ms contra um prazo de 3468 ms), e a tentativa 4 (907 caracteres, a do laço)
caiu por `palavras`: provavelmente o LLM tirou o laço, e a checagem de palavras recusou o texto
mais curto. A tentativa 3 (359 caracteres) passou com 3143 ms, perto do teto. O prazo do Polish é
fixo em 3,5 s (`winruntime/polish.go`, `prazoPolish`), dentro do `DefaultResolveTimeout` de 5 s
do núcleo, e não cresce com o texto: no modo com IA, o ditado longo sai como Smart. Isto não
mudou na branch do ditado longo; **mudou depois, com a decisão 5** (ver "Decisões que são do
Mauro"): o prazo do Polish e o do núcleo crescem com o texto. O `palavras` da tentativa 4 deve
sumir, porque a guarda contra o laço agora roda antes da resolução; o roteiro manual confere.

## O que mudou

| Commit | O quê |
|---|---|
| `feat(ditado): prazo da transcrição proporcional à duração do áudio` | `dictation.PrazoDeTranscricao`: 5 s + 0,75 × duração, entre 10 s e 90 s. O `AudioCapturedEvent` leva a duração (ADR-007, nota de 2026-10-01). Nos ditados do incidente: 37,2 s têm 32,9 s de prazo (levou 18,8 s); 88,9 s têm 71,7 s (levou 16,1 s). |
| `feat(ditado): transcrição que chega depois do prazo fica no histórico` | Se o prazo ainda estourar, o texto que chega depois vai para a linha do histórico **daquela** tentativa (bruto e final pelo Smart, sem Polish, `status` de erro), **sem inserir**. Com nenhum ditado em curso, a pílula diz "O texto demorou e ficou no histórico." |
| `feat(asr): janela do encoder por requisição, reduzida só na fala curta` | O servidor sobe sem `-ac`; cada requisição leva `audio_ctx`: 768 até 12 s, janela inteira acima (`local.JanelaRecomendada`). 768 e não 750: múltiplo de 256, sem linhas de K/V da requisição anterior na fala curta. |
| `feat(asr): guarda contra o laço de repetição do ASR antes da resolução` | `internal/asr/repeticao`: a mesma unidade de palavras repetida em seguida vira uma ocorrência (10+ palavras: 4+ vezes; 3 a 9 palavras: 6+ vezes e 60+ caracteres; 1 ou 2 palavras: 8+ vezes; número sozinho nunca). A pílula diz "Removi uma repetição. Confira o texto." O texto do ASR como chegou, com as cópias, fica no `raw_text` do histórico; o final e o que foi inserido saem sem elas. (Os limiares de frase e a cópia pela metade com cauda de até 2 palavras são da revisão de 01-out-2026.) |
| `feat(ditado): prazo da resolução proporcional ao tamanho do texto` e `feat(polish): prazo do Polish cresce com o texto, até 8 s` | A decisão 5, depois do merge. O Polish tem 3,5 s até 250 caracteres e 6 ms por caractere acima disso, até 8 s aos 1000; o watchdog da resolução no núcleo (`dictation.PrazoDeResolucao`) é sempre 1,5 s maior, de 5 s a 9,5 s, pelo mesmo acréscimo e sobre o mesmo texto (o que entra na resolução, já sem o laço). Nos ditados do incidente: 359 caracteres têm 4,15 s (levou 3,14 s); 802, 6,81 s (caiu no Smart por 1 ms); 907, 7,44 s (levou 3,03 s). A fala curta continua com 3,5 s e 5 s, ao milissegundo (ADR-016, "Emenda de 01-out-2026"; ADR-007, segunda nota de 2026-10-01). |
| `feat(bench): asrlongo monta ditado longo do dataset e mede o ASR nele` | `cmd/asrlongo`: áudios de 30, 60 e 90 s feitos com o dataset, medidos com o mesmo provider do Agent em seis variantes (a `prompt-fixo`, com `carry_initial_prompt`, é da revisão de 01-out-2026). |

O que **não** mudou de propósito:

- A transcrição continua não sendo cancelada no timeout: cancelar libera a CPU, mas perderia o
  texto que a segunda mudança guarda. Com o prazo proporcional, o timeout fica raro.
- Os ajustes do decodificador (`entropy_thold` 2,8, `temperature_inc` 0,4) ficaram disponíveis no
  provider, mas desligados no Agent: são variantes do benchmark, e só entram medidos.
- O corte do áudio longo em pedaços pelas pausas (a correção estrutural que a leitura do b5130
  sugere) não foi feito: muda a máquina de estados e depende do benchmark.
- O `carry_initial_prompt` (H5) ficou disponível no provider (`local.Decodificacao.PromptFixo`),
  desligado no Agent: é a variante `prompt-fixo` do benchmark.
- O prazo do Polish (3,5 s) e o `DefaultResolveTimeout` do núcleo (5 s) não acompanhavam o
  tamanho do texto, e no ditado longo o Polish caía no Smart por prazo. Era decisão; foi feito
  depois, com a decisão 5 (abaixo).
- **Agent fechado com uma transcrição em voo: o texto se perde.** Quando o Agent fecha, `Run`
  sai, o `provider.Close` derruba o servidor, e a goroutine da transcrição manda o erro para
  `rt.pending`, que ninguém mais lê (com o buffer de 4 cheio, ela fica bloqueada até o processo
  terminar). O texto em voo não chega ao histórico. Não trava o encerramento, e não é regressão:
  antes desta branch ele também se perdia.

## Decisões que são do Mauro

1. **A fórmula do prazo:** fator 0,75, piso 10 s, teto 90 s. O custo do teto é a tecla ficar
   ignorada até 90 s se o servidor travar. O 0,75 é a pior razão ASR/áudio do incidente (0,51) com
   1,5× de folga, mas essas razões vêm de um servidor com `-ac 750`, que pulava parte de cada
   janela de 30 s e decodificava laço. Com a janela inteira o encoder custa mais por janela e o
   decodificador processa a fala inteira, então a razão do ditado longo tende a subir, e mais com
   o Meet disputando a CPU; o benchmark mede com a máquina livre. Depois dele, rever o fator com a
   maior razão medida na janela inteira e uma folga para CPU disputada, ou medir uma vez com carga.
   A fala curta não piora (o piso de 10 s é o prazo de antes).
2. **Os textos das pílulas:** "O texto demorou e ficou no histórico." (ponto cinza) e "Removi uma
   repetição. Confira o texto." (ponto verde). As duas foram conferidas desenhadas (folha de PNGs do
   overlay).
3. **O painel:** a linha do texto tardio aparece com a etiqueta "falhou" e a nota "o texto pode não
   ter entrado no campo". Uma etiqueta própria ("não inserido") é mudança só de painel; uma marca
   explícita de "chegou depois" ou "repetição removida" na linha pede coluna nova (migration, sync).
4. **Depois do benchmark:** manter o 768 e a janela inteira; se a variante intercalada mostrar
   texto diferente (H4), escolher entre desligar a flash attention (`-nfa`, +22% em toda
   transcrição) e a janela inteira sempre (`Janela` zero em `cmd/agent/main.go`, perde o corte de
   ~40% da fala curta); ligar ou não os ajustes do decodificador e o `carry_initial_prompt`; e se
   vale cortar o áudio longo em pedaços.
   **Medido e decidido (01-out-2026).** `docs/benchmarks/asr-longo-2026-10-01.md` confirmou a
   causa: na fala longa o `-ac 750` dava WER 0,537 em 12,9 s, e a janela por duração, 0,283 em
   4,9 s. Confirmou também o H4: com flash attention, a fala curta antes mudou o texto do longo em
   6 de 6 áudios (WER 0,283 para 0,308). `docs/benchmarks/asr-longo-2026-10-01-nfa.md` mediu o
   `-nfa`: 6 de 6 iguais, WER 0,282, ao custo de a fala curta passar de 0,59 s para 0,76 s (p50) e a
   longa de 4,9 s para 6,8 s. **O Mauro escolheu o `-nfa`** (`SemFlashAttention` em
   `configDoServidor`). Os ajustes do decodificador (entropia 2,8, temperatura 0,4) não mudaram
   nada e ficam desligados; o `carry_initial_prompt` piorou (0,333 contra 0,308) e fica desligado.
   Nenhum laço apareceu no dataset concatenado, que é fala lida com pausas: o laço do incidente
   não se reproduziu, e cortar em pedaços fica para quando houver uma gravação longa de fala
   natural que o reproduza.
5. **O Polish no ditado longo:** hoje ele não cabe (prazo fixo de 3,5 s, dentro dos 5 s do
   núcleo), e o ditado longo sai como Smart. As saídas são o prazo do Polish e o
   `DefaultResolveTimeout` do núcleo proporcionais ao tamanho do texto (muda o núcleo: nota no
   ADR-007, como a do prazo da transcrição), ou um aviso de que o Polish não cabe no ditado longo.
   **Decidido pelo Mauro (01-out-2026): o prazo cresce com o texto**, até uns 8 s num ditado de
   1 minuto (esperar alguns segundos a mais num ditado longo quase não se nota).
   **Feito (01-out-2026).** Prazo do Polish = 3,5 s + acréscimo; watchdog do núcleo = 5 s +
   acréscimo; o acréscimo é zero até 250 caracteres, 6 ms por caractere acima disso, e no máximo
   4,5 s (aos 1000 caracteres). Um ditado de 1 minuto (790 a 920 caracteres no ritmo do incidente)
   dá ao Polish de 6,8 a 7,5 s; acima de 1000 caracteres, 8 s. O 6 ms vem do log: cada caractere
   rende de 0,21 a 0,32 token de saída acima de 250 caracteres, e 6 ms por caractere cobrem a
   geração a partir de 35 a 53 tokens/s, abaixo dos 80 tokens/s do ritmo mais lento estimado na
   ADR-016. A fala curta não muda (até 250 caracteres, os
   mesmos 3,5 s e 5 s). A pílula e os estados não mudam. A conta, as corridas e os testes estão na
   ADR-016, "Emenda de 01-out-2026: o prazo cresce com o texto".
6. **O texto original no histórico** (revisão de 01-out-2026): quando a guarda tira um laço, o
   `raw_text` guarda o texto do ASR como chegou, com as cópias, e o `final_text` sai sem elas. É o
   que o contrato da API já chamava de `raw_text` ("texto bruto do ASR"), e é o único jeito de
   recuperar uma repetição dita de propósito. O custo: o painel mostra as cópias na diferença
   entre o bruto e o final, e um ditado com laço muito grande pode ficar sem sugestão de
   aprendizado (grande demais para alinhar). Desfazer são duas linhas em `winruntime`.
   **Confirmado pelo Mauro (01-out-2026):** o bruto guarda o original.

## O que falta medir: o benchmark de áudio longo

**Ainda não rodou.** Usa a CPU inteira por uns 5 a 25 minutos (seis variantes × seis áudios
longos, mais as 46 amostras curtas em duas variantes, cada variante num servidor recém-subido).
Rodar com a máquina livre, sem aula e sem o Agent aberto.

**Ordem: merge, benchmark, e só então compilar o Agent de uso diário.** A janela por requisição
traz um risco novo (H4: o K/V do ditado anterior lido no ditado longo), e só a variante
intercalada diz se ele muda o texto. Sem esse número, o Agent de `bin\` fica como está.

Depois do merge desta branch em `main`, um comando só:

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/asrlongo medir -manifest testdata/asr-dataset/manifest-normalizado.json -servidor thirdparty/whisper/Release/whisper-server.exe -modelo models/quant/small-q4_0.bin -dicionario testdata/asr-dataset/dicionario.json -out docs/benchmarks/asr-longo-2026-10-02.md
```

(Troque a data do nome do arquivo pela do dia.) Saem três arquivos em `docs/benchmarks`: o
relatório `.md`, que é o que entra no commit; o `.md.json`, com os textos transcritos (são as frases
do dataset, não ditado de uso), para olhar um laço de perto; e o `.md.servidor.log`, a saída do
whisper-server, só para quando um servidor não subir.

Para ouvir os áudios montados, sem transcrever nada:

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/asrlongo montar -manifest testdata/asr-dataset/manifest-normalizado.json -saida testdata/asr-dataset/audio-longo
```

(`*.wav` é ignorado pelo git; o `manifesto-longo.json` que sai junto não é, e não precisa entrar em
commit.)

O que olhar no relatório:

- **`ac750` contra `por-duracao`, no resumo do áudio longo.** Se `ac750` tiver laços ou WER bem
  maior e `por-duracao` não, H1 se confirma e a correção vale. Se os dois tiverem laço, a janela
  não é a causa (ou não é a única), e o corte em pedaços entra na conversa.
- **"A fala curta antes muda o texto do áudio longo?"** Texto diferente entre `por-duracao` e
  `por-duracao-intercalada` quer dizer que H4 acontece; aí o Agent não é instalado como está, e a
  decisão é entre `-nfa` (+22%) e a janela inteira sempre (decisão 4).
- **`entropia-2.8` e `temperatura-0.4`:** só valem se tirarem laço ou tempo sem piorar o WER. A
  entropia 2,8 ainda precisaria ser medida na fala curta (falso positivo custa uma redecodificação).
- **"Fala curta: o 768 por requisição contra o -ac 750 medido":** o esperado é WER igual e tempo
  uns 2% maior. Se o tempo subir muito, rever o 768.
- **`prompt-fixo` contra `por-duracao-intercalada`:** só diferem com o vocabulário (`-dicionario`,
  já no comando acima). WER menor no `prompt-fixo` confirma H5.
- **Razão tempo/áudio máxima e "estouraria o prazo":** se algum áudio passar do prazo, ou a razão
  máxima passar de 0,5 com a máquina livre, rever o fator 0,75 (decisão 1).

## O que o teste de 01-out mostrou

O Mauro testou o Agent novo em 01-out-2026 e comparou o `raw_text` e o `final_text` do histórico.

- **O laço não apareceu.** Nenhum ditado do teste entrou em laço de repetição.
- **A autocorreção comia o "não" enfático.** Ele ditou "Não, não, não, isso não está certo.", o ASR
  devolveu exatamente isso (35 runas), e o Smart entregou "não, isso não está certo." (25), no modo
  Smart e no com IA (o Polish recebe o texto do Smart). O segundo "não" era lido como marcador de
  autocorreção, o primeiro como a oração errada e o terceiro como a correção. Corrigido em
  `fix(smart): negação enfática repetida não vira autocorreção`: o marcador isolado não dispara
  quando a oração que ele corrigiria, ou a de reposição, é ela mesma só um marcador
  (`16-ai-behavior-spec.md`, "Self-correction"). A autocorreção de verdade (corr-01 a corr-04)
  continua. A regra só via o "não" repetido colado. O Mauro decidiu depois (01-out-2026) que o
  "não" isolado também não dispara quando a oração que ele corrigiria já nega: "Eu não sei, não,
  cara." sai inteira (`feat(smart): "não" isolado não corrige quando a oração anterior já nega`;
  `16-ai-behavior-spec.md`, "Oração que já nega não é corrigida", com a lista de negação e o
  custo). Seguem como limitação, sem regra: "Não, Mauro, não, isso não está certo." e "Quer café,
  não, não quero".
- **A caixa do eSocial.** O ASR devolveu "ESOCIAL" e "ESOcial", e o Smart deixou assim, com
  "eSocial" no dicionário: o casamento exato ignorava diferença só de caixa, de propósito. Agora o
  termo de grafia própria sai na caixa do dicionário quando o ASR escreveu alguma maiúscula e o
  trecho não encosta em separador de código, e a sigla toda maiúscula ("das" nunca vira "DAS"), o
  termo só com a inicial e a palavra comum ficam como vieram (`feat(smart): termo de grafia
  própria sai na caixa do dicionário`; `16-ai-behavior-spec.md`, "Caixa do termo"). A revisão
  achou que a primeira versão reescrevia também o minúsculo de URL, pacote e comando
  (`gov.br/esocial`, `npm install typescript`, `systemctl restart postgresql`), e a regra ficou
  mais conservadora (`fix(smart): grafia própria só troca a caixa se o ASR escreveu maiúscula e
  fora de código`). O preço: "esocial" todo em minúsculo no meio de uma frase também sai como veio.
- **"Repitam comigo: o RAG busca antes de gerar." dita quatro vezes saiu uma vez só.** É limitação
  do ASR, não do Agent: o Whisper juntou as quatro numa frase só. O Agent não tirou nada — quando a
  guarda contra o laço age, o `raw_text` guarda as cópias e a pílula diz "Removi uma repetição", e
  uma frase de 8 palavras repetida 4 vezes nem chega ao limiar dela (3 a 9 palavras: 6 vezes ou
  mais). O L3 do roteiro abaixo espera o texto como foi dito; nesta parte, quem decide é o ASR.

## Roteiro manual

Para o Mauro, depois do merge **e do benchmark**, **fora de aula** (a primeira parte) e depois
numa chamada de teste pelo Meet (a segunda).

### Antes de começar

0. O benchmark já rodou, e a seção "A fala curta antes muda o texto do áudio longo?" do relatório
   diz "não". Se disser "sim", pare aqui: falta a decisão 4.
1. Feche o Agent pelo ícone da bandeja → **Sair**. No Gerenciador de Tarefas, nenhum
   `aurora-whisper-agent.exe` nem `whisper-server.exe`.
2. Monte e compile, um comando por vez:

   ```bash
   go -C C:/Desenvolvimento/aurora-whisper run ./cmd/asrstage
   ```

   ```bash
   go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
   ```

3. Abra `C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe`.
4. Confira que é o Agent novo: depois do primeiro ditado, a linha `transcrição concluída` do log
   tem o campo `audio=`. O comando abaixo mostra as linhas que interessam a este roteiro:

   ```powershell
   Select-String -Path C:\Desenvolvimento\aurora-whisper\bin\agent.log -Encoding UTF8 -Pattern "transcrição concluída|chegou depois do prazo|laço de repetição|polish attempt|dictation attempt finished" | Select-Object -Last 20
   ```

### Sem o Meet

**L1. Ditado curto.** Num bloco de notas, dite uma frase de uns 5 s.
Esperado: "Inserido", o texto certo, e no log `audio=` perto de 5 s.

**L2. Ditado de 60 a 90 s.** Leia em voz alta um texto corrido (um parágrafo de um documento seu),
sem parar, por 60 a 90 s.
Esperado: a pílula fica em "Processando" alguns segundos (até uns 20), e sai "Inserido" com o
texto **inteiro**, sem frase repetida.

**L3. Repetição de gente.** Dite "não, não, não, isso não"; depois "ficou muito muito bom"; e
depois, num ditado só, "Repitam comigo: o RAG busca antes de gerar." quatro vezes seguidas.
Esperado: os três entram como foram ditos, com "Inserido" (e não "Removi uma repetição"). Se o
terceiro sair com a frase uma vez só e o `raw_text` também, foi o Whisper que juntou (ver "O que o
teste de 01-out mostrou").

**L4. Polish no ditado longo.** Com o modo com IA, dite uns 60 s seguidos (como em L2). No log,
a linha `polish attempt=` diz em `desfecho=` se o Polish agiu (`aceito`) ou caiu no Smart, e em
`prazo_ms=` quanto tempo ele teve. Esperado: `desfecho=aceito`, com `prazo_ms=` perto de 7000 a
8000 (de uns 6700 a 8000: é 3500 + 6 × (`entrada_chars` − 250), até 8000, com diferença de alguns
milissegundos, porque o prazo é medido no texto antes do Smart); e a pílula de sempre, com o texto
organizado. **Nunca** `palavras` num ditado com laço (a guarda agora roda antes). Se sair `prazo`
com `prazo_ms=` perto de 8000, a IA passou de 8 s: anote o `latencia_ms=`. Se o `prazo_ms=` sair
perto de 3500 num ditado de 60 s, o Agent é o de antes (volte ao passo 2). Para conferir que a
fala curta não mudou, dite uma frase de uns 5 s no mesmo modo: `prazo_ms=` perto de 3500. Anote o
`desfecho=`, o `prazo_ms=`, o `latencia_ms=` e o `entrada_chars=` dos dois.
Para a faixa mais apertada, dite uma frase de 300 a 450 caracteres (uns 20 a 30 s): o prazo cresce
devagar logo depois dos 250 (359 caracteres têm 4,15 s, e no incidente levaram 3,14 s).
Esperado: `aceito`. Se sair `prazo` aí, anote `entrada_chars=` e `latencia_ms=`: a correção é subir
`AcrescimoPorCaractere` ou descer `CaracteresSemAcrescimo` (`internal/agent/dictation/effects.go`;
os testes de tabela acompanham).

### Com o Meet

**M1.** Abra uma chamada de teste no Meet, com o microfone ligado nela, e repita L2.
Esperado: o mesmo de L2; o tempo pode ser maior.

**M2.** Ainda na chamada, dite duas vezes seguidas, de 60 a 90 s cada, como na aula.

### O que pode aparecer, e o que significa

- **"O texto demorou e ficou no histórico."** O ASR passou do prazo e o texto chegou depois. Ele
  **não** foi inserido: abra o painel, a linha do ditado está lá (com a etiqueta "falhou") e dá
  para copiar. No log, `transcrição chegou depois do prazo`.
- **"Removi uma repetição. Confira o texto."** O ASR entrou em laço e o Agent deixou uma
  ocorrência só. Confira o que entrou: o laço costuma tomar o lugar de parte da fala. No log,
  `laço de repetição do ASR removido`. Se a repetição era de propósito, o texto como o ASR o
  devolveu, com as cópias, está no painel: abra o ditado e veja a diferença entre o texto do
  reconhecimento e o final.
- **"Não consegui transcrever. Tente novamente."** sem a pílula do histórico depois: o ASR falhou de
  verdade, ou voltou sem texto. Mande o trecho do log.

### O que anotar, por ditado

| | |
|---|---|
| quanto tempo falou (aproximado) | |
| Meet aberto? | |
| pílula que apareceu | |
| o texto entrou inteiro? faltou o quê? | |
| alguma frase repetida? | |
| linhas do log (o comando acima) | `audio=` e `duracao=` da transcrição, `outcome=` |
