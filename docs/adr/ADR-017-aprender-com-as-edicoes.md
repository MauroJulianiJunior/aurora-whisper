# ADR-017 — Aprender com as edições do painel: só com um clique, guardado no `dicionario.json`

Status: Accepted (as quatro decisões, Mauro, 30-set-2026, sobre o plano da etapa 3). A redação desta
ADR ainda não passou pela revisão dele: passa antes da primeira linha de código (fatia F1).
Data: 2026-09-30
Contexto: etapa 3 do roteiro de 29-set-2026 ("aprender com as edições do painel"), depois do Polish
(ADR-016) e antes da nuvem (etapa 5)
Emenda: `04-system-design.md`, `05-data-model.md` e a ADR-008 (onde o dicionário mora);
`06-api-contract.md` (resposta de salvar e rota `/api/aprendizado`); `07-flows.md` ("Learning");
`08-ui-ux-spec.md` (aviso e lista); `09-security-privacy.md`; `18-tool-contract.md`;
`13-implementation-plan.md` (TASK-019: ordem, fatias e o que fica para a etapa 5);
`fase-1-cobertura.md`; `PRODUCT.md`; e o registro em `00-index.md` e `20-traceability.md`.
**Não muda o núcleo** (ADR-007) e **cumpre o AC-022** (aprendizado só com confirmação).
**Emendada pela ADR-020 (01-out-2026):** a decisão 7 ("a lista que o ASR recebe como dica não muda")
e a alternativa descartada "promover o termo novo ao prompt do ASR" estão revogadas: o prompt agora é
trocado em memória, e quem põe um termo nele é a pessoa, pela caixa "Dica" do "Adicionar termo".
**Nota de 10-out-2026 (ADR-021, ditar no celular):** o que esta ADR diz do prompt do ASR vale para o
**PC**: o termo aprendido pelo botão Aprender continua fora do prompt do ASR local. Só a nuvem diverge:
no ditado do celular, os termos distintos do aprendido da conta podem ir como **dica** ao provedor de
transcrição (OpenAI ou Groq), porque o celular não tem outra fonte de dica. O interruptor é da pessoa e
**começa desligado**; ligado, os termos (que podem ser nomes de clientes ou de terceiros) saem do
servidor junto com a voz de cada ditado. Nada muda no PC. Ver a ADR-021, decisão E.

## O problema

A spec já descreve o laço (`07-flows.md`, "Learning"): editar o `final_text`, salvar, calcular o
diff, sugerir se a troca é pequena e mapeável, **a pessoa confirma**, criar Correction/alias,
sincronizar. E exige a confirmação (REQ-020, AC-022; `16-ai-behavior-spec.md`, "Human-in-the-loop":
aprender correção é obrigatório confirmar). O Mauro quer aprender "sem mexer no dicionário à mão".
Isso só é compatível com a spec se houver um botão de um clique, não um aprendizado automático.
(**Emenda, 07-out-2026, ADR-019, "Decisões da F8", D2:** com o sync, o clique é no PC de origem; os
outros PCs aplicam o par que chega, sem clique lá, com as mesmas checagens do Aprender. O REQ-020 e
o AC-022 foram emendados para dizer isso. Não é aprender sozinho: alguém clicou.)

Metade do laço existe, como biblioteca:

- `internal/learning` (`Classify`, `Confirm`) só é chamado pelo `cmd/dictlearn`, o comando de
  terminal. O painel não chama nada disso.
- A persistência real do dicionário é o arquivo `dicionario.json`, que o Agent lê **uma vez**, na
  subida, para um repositório em memória. `Semente.AcrescentarAlias` e `Gravar` (escrita atômica),
  em `internal/dictionary/aprender.go`, só o `dictlearn` usa.
- As tabelas SQLite `local_dictionary_*` e `local_corrections` existem no schema e nenhum
  repositório Go as usa.
- A API local não tem rota de dicionário nem de correção. O painel mostra o diff palavra a palavra
  do bruto contra o texto, mas não tem botão de aprender (`PRODUCT.md`, "Ainda não existe").
- `PATCH /api/history/{id}` sobrescreve o `final_text` e sobe a versão. **O texto anterior não é
  guardado.**

### O que foi medido

- **Uso real.** No histórico local, na leitura que fundamentou o plano (main em `0bf27ca`), havia
  12 ditados. Dois foram editados no painel, e nenhuma das duas edições foi troca de termo do tipo
  "SF" → "ECF". **O ganho real ainda não foi medido.** Esta etapa entrega o caminho completo, e
  quanto ele ajuda se vê no uso.
- **O código atual, com entradas sintéticas** (execução descartável com `go test -overlay`, sem
  tocar em arquivo do repositório). O que isso mostrou, e por que o desenho abaixo não reaproveita o
  `Classify` como está:

  | # | O que aconteceu | Consequência no desenho |
  |---|---|---|
  | 1 | `Classify` devolve **um** trecho contíguo: "o SF e o RJ" → "o ECF e o RF" vira um par só, grande, e elegível. O comentário do código diz o contrário | motor novo, que acha vários trechos por edição |
  | 2 | Com o **bruto** como base, o que o Polish faz (pontuação, maiúscula) esconde a troca real, ou gera um par sem palavra trocada ("vou mandar o sf" → "Vou mandar o sf.") | a base da comparação é o **texto de antes da edição**, não o bruto |
  | 3 | Ensinar só o acento: como termo, o Smart corrige "recíbo" → "recibo", mas "está" faz "esta casa" virar "está casa" em todo texto | decisão 3 |
  | 4 | "vou de carro" → "vou do carro" é elegível e sem aviso de similaridade baixa | filtro de palavra de ligação; a proteção é o clique |
  | 5 | `AcrescentarAlias` só confere duplicata dentro da própria entrada: "SF" vai para outro termo e os dois passam a casar, ganha o de maior prioridade. Não existe remover variante | recusar variante que já é de outro termo; criar o "desfazer" |
  | 6 | `Confirm`, com a forma canônica em outra caixa, substitui a entrada e zera a prioridade. É um defeito de `internal/learning`, que o `dictlearn` usa | o painel grava pelo caminho do JSON (`AcrescentarAlias`), que não tem o defeito; o `dictlearn` fica como está (fora desta etapa) |

### Conflitos com a spec

Por `CLAUDE.md`, conflito entre documentos é ponto de parada. Por `00-index.md` ("Fonte da verdade"),
a decisão humana mais recente vale, desde que registrada: esta ADR é o registro.

1. **Onde o dicionário mora.** `04-system-design.md` ("Local") manda o SQLite guardar
   `dictionary/corrections`, `05-data-model.md` tem os espelhos `local_dictionary_*` e
   `local_corrections`, e a ADR-008 fala em SQLite para dicionário e correções. O código usa o JSON,
   por uma decisão que só existia num comentário (`internal/dictionary/aprender.go`), sem ADR.
   **Resolvido por esta ADR: decisão 5.**
2. **A correção que sincroniza.** TEST-008 ("n oito n" → "n8n", confirmar cria uma Correction
   **sincronizável**), a parte de sync do AC-021 e a de TASK-019 (`13`). O Agent não tem outbox nem
   usa `internal/sync`. **Resolvido: ficam para a etapa 5** (seção "Como funciona", "Nuvem").
3. **A ordem do plano.** S9 (Learning) vinha depois de S6 a S8 (`13`, "Ordem"). O roteiro de
   29-set-2026 o antecipou para antes da nuvem. **Resolvido: registrado no `13`.**
4. **Confirmar ou aprender sozinho.** Um modo automático contrariaria AC-022, REQ-020, o `16` e o
   princípio "Quem usa decide" (`PRODUCT.md`), e exigiria uma ADR própria. **Não se aplica: a
   decisão 1 é a confirmação.** (Emenda, 07-out-2026: com o sync, a confirmação é o clique no PC de
   origem, e os outros PCs aplicam o par sem clique; ADR-019, "Decisões da F8", D2.)
5. **A base do diff.** `07` diz "editar `final_text`, calcular diff". A decisão de 28-set-2026
   diz que o alias é a forma que o **ASR** escreveu, não a que a pessoa lembra, e o PATCH
   apaga a única pista do que o sistema já tinha mudado. **Resolvido: decisão 6 e seção
   "Detecção".**

## Decisão

1. **Só com o clique de quem usa.** Salvar uma edição **nunca** grava no dicionário. Salvar devolve
   sugestões; o botão **Aprender** as confirma. Se o aviso some sem clique, nada foi aprendido. Um
   caso só já basta para sugerir: esperar o erro se repetir exigiria guardar frases corrigidas, e
   isso é mais dado guardado. O filtro é o clique.
2. **O que aprende, e o que não aprende nem sugere.**

   | Aprende (com o clique) | Não aprende, nem sugere |
   |---|---|
   | **Troca curta de termo:** até 4 palavras e 30 letras de cada lado. Exemplos: "SF" → "ECF", "Whisperflow" → "Wispr Flow" | Pontuação, quebra de linha, e **maiúscula ou acento sozinhos** (decisão 3) |
   | O "de" é a forma que o ASR escreveu (decisão 6) | **Reescrita:** mais de 4 palavras, só acrescentar ou só apagar palavras, ou mais de 2 trocas na mesma edição |
   | A troca vira **variante** do termo. Se o termo ainda não existe no dicionário, é criado com prioridade **50** | **Número.** Isso já é barrado hoje |
   | Com 2 trocas na mesma edição, um clique aprende as duas | "De" que seja **palavra de ligação** (de, do, na, em...): "de" → "do" nunca vira regra |
   | | **Desfazer uma troca que o próprio sistema fez.** O sistema trocou "comite" por "commit" e a pessoa voltou para "comitê": não se aprende "commit" → "comitê"; só se conta no registro |
   | | **Variante que já pertence a outro termo** (SF já vira ECF e agora a pessoa quer ECD): não sugere, para não apagar a regra anterior sem avisar |

   O limite de 4 palavras é também o que o Smart sabe procurar (ele testa frases de até 4 palavras;
   uma variante maior nunca casaria).
3. **Acento ou maiúscula sozinhos não se aprendem nesta etapa.** Mostrou-se acima (linha 3 da tabela)
   que ensinar "está" quebra "esta casa". Que o ASR raramente erra só o acento é **estimativa**
   **(est.)**, não medida. Reabre com dado depois do uso.
4. **O aprendido vale em qualquer aplicativo.** A variante nasce sem restrição de domínio, e
   **nenhum contexto de janela é guardado** para aprender. O painel não sabe em que aplicativo a
   pessoa ditou: isso não é gravado, de propósito (`target_app`, `05`/`09`). Restringir ao grupo de
   aplicativos do termo (o dicionário separa, por exemplo, contábil e desenvolvimento) não
   funcionaria no navegador, que fica fora desses grupos de propósito. O Desfazer está a um clique.
5. **Mora no `dicionario.json`**, com a origem **"painel"** no campo que já existe
   (`alias_origem`, que hoje diz de qual amostra do dataset cada alias veio). É o arquivo que o
   Agent já lê; a pessoa pode abri-lo e ler o que foi aprendido; nenhuma migration; as tabelas do
   banco ficam para a etapa 5. A marca "painel" corresponde ao `source=learned` do modelo de dados
   (`05`).
6. **O "de" é sempre o bruto.** Vem do `raw_text` do ditado, não do que estava na tela (decisão de
   28-set-2026). O Agent confere isso de novo na hora do clique.
7. **Vale já no próximo ditado, sem reiniciar** (`06`: "vale a partir do próximo ditado"). O
   dicionário em memória é trocado na hora. A lista que o ASR recebe como dica **não muda**.
   (Revogado pela ADR-020: o prompt é remontado e trocado junto, e o "Adicionar termo" o muda.)
8. **Desfazer existe e está a um clique.** Fica no aviso logo depois de aprender, e em
   Configurações, numa lista "Aprendido no painel".
9. **Nada sai da máquina, e o registro de funcionamento só guarda contagens**, nunca o texto, nem o
   par de/para (AC-027, AC-047, `09`).

## As quatro respostas do Mauro (30-set-2026)

| # | Pergunta | Resposta |
|---|---|---|
| 1 | Aprender com um clique ou sozinho? | Um clique. Nunca sozinho. |
| 2 | Guardar no `dicionario.json` ou no banco? | No `dicionario.json`, com a origem "painel" em `alias_origem`. Pede esta ADR, porque a spec previa o SQLite. |
| 3 | Aprender correção só de acento ou de maiúscula? | Não, nesta etapa. |
| 4 | Onde vale o que foi aprendido? | Em qualquer aplicativo. |

Todas "como recomendado" no plano. As recomendações, resumidas: a spec exige a confirmação, e nas
duas edições reais o modo "sozinho" teria ensinado a troca de uma palavra comum por outra, valendo
para todo texto dali para frente (1); o JSON é o que o programa já lê, dá para abrir e auditar, e não
muda o banco (2); ensinar só o acento quebra texto correto (3); o painel não sabe o aplicativo, e o
Desfazer está a um clique (4).

### Aprovado no plano, sem pergunta

Fazem parte do plano aprovado, e não foram perguntas: o limite de 4 palavras e 30 letras; no máximo
2 sugestões por edição; prioridade 50 para termo novo; um caso basta; o `dictlearn` intocado; o
`asrstage` passa a preservar o arquivo existente; a Correction **não** é guardada como registro à
parte; o prompt do ASR não muda.

## Como funciona

### Detecção

Quando a pessoa salva uma edição, o Agent lê o texto **como estava antes** de gravá-la (como, na
F3; a alternativa de uma coluna nova foi descartada, abaixo). Compara o antes com o texto salvo, e
assim separa o que a pessoa mudou do que o sistema já tinha mudado sozinho (Smart, Polish).
Depois **alinha cada troca com o texto bruto** do ditado para achar a forma que o ASR escreveu,
aplica os filtros da decisão 2 e devolve **até 2 sugestões** na própria resposta de salvar. Salvar
nunca grava no dicionário (AC-022).

Se outra aba salvou antes, o PATCH já devolve `409` (`06`), e um conflito de versão não gera
sugestão.

O diff palavra a palavra que o painel desenha para a pessoa (`web/src/diff.ts`) é só para a tela, e
continua assim. O motor de sugestão é outro código: no servidor, e testado.

### Motor

Uma função nova, **sem efeito colateral**, em `internal/learning`. Acha vários trechos por edição,
e ignora maiúscula, acento e pontuação na hora de comparar. O `Classify` e o `cmd/dictlearn` ficam
como estão. Assinaturas, tokenização e o algoritmo de alinhamento: definidos na F1.

### Confirmação

O botão Aprender chama um endereço novo do painel, `/api/aprendizado`, atrás da mesma cadeia que o
resto da API local (Host, Origin e token; `06`). O Agent **refaz todas as checagens** antes de
gravar: por exemplo, o "de" tem de estar no texto bruto **daquele** ditado. O mesmo endereço lista o
que foi aprendido e desfaz. Métodos, caminhos e campos: definidos na F3.

Uma proteção a mais, calculada no Agent: se o "de" aparece em outros ditados da própria pessoa, a
sugestão diz quantos ("aparece em 3 ditados"). A conta vem do histórico dela, não de uma lista de
"palavras comuns" escrita à mão (uma lista assim apodrece: a lição de 28-set-2026). O que conta como ditado (ativo, lixeira) é definido
na F3.

### Onde fica

No `dicionario.json`, ao lado do executável. O Agent **relê o arquivo**, acrescenta a variante e
grava de um jeito que não corrompe se faltar luz (temporário e rename, como o `dictlearn` já faz por
`Gravar`). Cada variante leva a marca `"painel"` em `alias_origem`.

O `cmd/asrstage` hoje **sobrescreve** `bin/dicionario.json` com a cópia do dataset
(`testdata/asr-dataset/dicionario.json`) e apagaria o que foi aprendido. Ele passa a **preservar o
arquivo que já existe**. O que fazer quando o dataset traz entradas novas que o `bin` ainda não tem:
definido na F2.

O arquivo do Agent (`bin/dicionario.json`) está no `.gitignore`: o aprendido no painel nunca chega
ao repositório sozinho.

### Quando passa a valer

- O Agent **troca o dicionário em memória na hora**. A correção de termos (Smart) e as guardas do
  Polish (que consultam o mesmo dicionário, só localmente) já veem a mudança no próximo ditado.
  Como a troca é feita, e o teste dela com o detector de concorrência ligado: F2.
- **A lista de termos que o ASR recebe como dica não muda.** Variante não entra nela, e um termo novo
  com prioridade 50 fica abaixo do mínimo para entrar (no log do Agent lido para o plano: 45 termos,
  17 no prompt, a menor prioridade dentro dele era 85, com orçamento de 140 runas). Promover um
  termo para essa lista **continua manual**; e como o vocabulário é montado na subida, só chega ao
  ASR depois de reiniciar.

### O `dictlearn`

Continua, para a curadoria pelo terminal. Usa o **mesmo arquivo e o mesmo jeito de gravar**, e cada
um relê o arquivo antes de gravar.

### Banco local (SQLite)

As tabelas `local_dictionary_entries`, `local_dictionary_aliases` e `local_corrections` continuam
**vazias**, sem repositório. Elas também não têm a coluna `restricted` nem o mapa de domínios que o
modelo do dicionário já usa (`05`). **Não se guarda a correção como registro à parte**: nada a usa
hoje, e ela guardaria a **frase inteira** (`Confirm` grava `FromText`/`ToText` como o texto antes e
depois) numa tabela que, pela spec, sobrevive à retenção de 6 meses. O que fica guardado é só o par
curto, dentro do dicionário.

### Nuvem (etapa 5)

Três coisas ficam para lá, e nada desta etapa as bloqueia:

- a **correção que sincroniza** (TEST-008, AC-021, a parte de sync da TASK-019);
- a decisão de **qual usuário é o dono**: o dicionário usa o `usuario` do arquivo ("mauro"), e o
  histórico usa o dono fixo da instalação (`05`, "Dono local", pendente);
- a **ida do dicionário para o banco**, com a coluna e o mapa que hoje faltam.

## Tela (o menor que funciona)

`PRODUCT.md`: "o menor que funciona", a referência é o Wispr Flow, sem janela nova.

- Usa o **aviso que já existe** no rodapé. Depois de salvar, com sugestão:
  `Texto salvo. Aprender "SF" → "ECF"?` com o botão **Aprender**. Se o aviso sumir sozinho, a
  sugestão foi ignorada. Com 2 trocas, as duas aparecem no mesmo texto e um clique aprende as duas.
- Depois do clique: `Aprendido: "SF" vira "ECF" no próximo ditado.` com **Desfazer**.
- Em Configurações, a seção **"Aprendido no painel"**: uma linha `SF → ECF` com **Desfazer** por par.
  Sem nada aprendido, a seção não aparece.
- Visual do painel: IBM Plex Mono nos termos, e verde só no que foi confirmado.
- O aviso diz o efeito (a troca vale para todo texto futuro) e, quando o "de" aparece em outros
  ditados da própria pessoa, quantos. Os textos finais: F4.

## Alternativas descartadas

- **Aprender sozinho (modo automático).** Contraria AC-022, REQ-020, o `16` e "Quem usa decide". E
  não há rede de segurança medida: o portão de entidades mede a ausência de armadilha, não a
  correção falsa que um alias novo cria (sessão de 28-set-2026, "o portão é cego por
  construção"). O clique é o único filtro que existe. (Emenda, 07-out-2026: o par aprendido com o
  clique chega sozinho aos outros PCs pelo sync, que refazem as checagens do Aprender; isso não é o
  modo automático descartado aqui, porque o clique existiu no PC de origem. ADR-019, "Decisões da
  F8", D2.)
- **Guardar no SQLite agora.** Exigiria migration (a coluna `restricted` e o mapa de domínios não
  cabem no schema), decidir o dono ("mauro" × dono fixo) e acoplar esta etapa à sincronização, que
  é da etapa 5. O JSON é o que o programa já lê e dá para abrir e auditar. O comentário de
  `aprender.go` já dizia isso; esta ADR o promove a decisão.
- **Aprender acento ou maiúscula sozinhos.** Decisão 3: quebra texto correto.
- **Guardar o texto de antes da edição numa coluna nova (migration).** Seria mais uma cópia do
  texto ditado no banco, e uma migration. O Agent lê o texto antes de gravar a edição.
- **Guardar a Correction como registro.** Nada a usa hoje, e guardaria a frase inteira além da
  retenção de 6 meses. Volta na etapa 5, com o sync, se ainda fizer sentido.
- **Esperar o erro se repetir antes de sugerir.** Exigiria guardar as frases corrigidas.
- **Restringir a variante ao grupo de aplicativos do termo.** O painel não sabe o aplicativo, e o
  navegador está fora dos grupos de propósito (decisão 4).
- **Promover o termo novo ao prompt do ASR.** Deslocaria um termo que já está lá. Continua manual.
  (Revogado pela ADR-020: é decisão da pessoa, pela caixa "Dica" do "Adicionar termo".)
- **Reaproveitar o `Classify` com o bruto como base.** Mostrado acima (linhas 1 e 2).

## Consequências

**Positivas:**
- o laço fecha com um clique, e a pessoa não precisa mexer no arquivo;
- vale no próximo ditado, sem reiniciar;
- nenhum dado sai da máquina; o arquivo é legível, e cada aprendizado pode ser desfeito;
- o `dictlearn` segue funcionando, sobre o mesmo arquivo;
- nenhuma migration, nenhuma dependência nova de produção.

**Negativas:**
- **o aprendizado de uma palavra comum é perigoso.** O Smart aplica a variante sempre, sem olhar o
  contexto, em qualquer aplicativo. Só o clique e o "aparece em N ditados" protegem. O Desfazer
  conserta, mas só depois do dano;
- **o termo novo entra no conjunto que o Polish protege** (guarda 6, ADR-016). É bom, mas se o termo
  for uma palavra comum, aumenta o risco de o Polish cair no Smart;
- **o prompt do ASR não muda:** o aprendido corrige o texto depois, não ajuda o ASR a escrever certo;
- **a variante é um trecho de até 4 palavras do que o ASR escreveu** e fica no dicionário até a
  pessoa desfazer (`05`: dicionário persiste até exclusão), inclusive depois dos 6 meses do ditado de
  onde veio. É local e visível, e é o preço de aprender; a spec classifica o dicionário como
  Confidencial (`09`);
- **dois escritores no mesmo arquivo** (o Agent e o `dictlearn`): cada um relê antes de gravar, mas
  a janela entre ler e renomear não é travada. Se isso precisa de trava: F2;
- **o `asrstage` passa a preservar:** entradas novas do dataset deixam de chegar ao `bin` sozinhas.
  Como resolver: F2;
- **uma variante que já é de outro termo não gera sugestão**, e o plano não mostra nada nesse caso:
  trocar SF de ECF para ECD exige desfazer a anterior (se foi do painel) ou editar o arquivo.

## Riscos, e o que não foi verificado

- **O benefício não foi medido** (0 trocas de termo em 2 edições). Se depois de uso real quase ninguém
  clicar em Aprender, a etapa entregou o caminho e não o ganho.
- **As estimativas.** "Uma sessão por fatia" e "o ASR raramente erra só o acento" são estimativas
  **(est.)**.
- **`-race`.** A troca do dicionário com um ditado acontecendo ao mesmo tempo precisa do detector de
  concorrência, que precisa de toolchain C, e a ADR-008 e o `fase-1-cobertura` registram que esta
  máquina não a tinha. A F2 confere; se faltar, registra o limite, sem fingir a prova.
- **Aviso que some sozinho, com uma ação dentro.** Para uso só pelo teclado e leitor de tela isso é
  um problema (`PRODUCT.md`, "Acessibilidade"). A F4 confere o tempo, o foco e o `aria-live`.
- **Dois "donos"** (o `usuario` do dicionário, o dono fixo do histórico): não atrapalham esta
  etapa, porque a variante vai para o arquivo e a conta do "aparece em N ditados" lê o histórico.
  Ficam para a etapa 5.
- **Vigência "no próximo ditado".** É a regra de `06`, e só vira fato verificado no roteiro manual
  da F4 (feito pelo Mauro em 30-set e 01-out-2026, OK; ver "Resultado do roteiro manual").
- **Uma troca errada que passe pelos filtros.** Filtros e limites são testados com casos
  escolhidos; uma edição errada que passe só o clique (e o Desfazer) pega.
- **O defeito do `Confirm`** (linha 6 da tabela) **não é tratado aqui**: o `dictlearn` o usa e fica
  como está.

## Testes previstos

Nenhum teste digita, clica ou mexe na área de transferência. Todos os de gravação rodam em **pasta
temporária**, nunca em `bin/` e nunca no dicionário real. Os de peças falsas usam dublês. Nenhum
ditado real entra em teste nem no repositório.

- **F1, motor** (`internal/learning`), por tabela de casos já conferidos: "SF" → "ECF" sugere;
  "recíbo" → "recibo" não sugere; "de" → "do" não sugere; "o SF e o RJ" gera 2 sugestões;
  pontuação e maiúscula do Polish não sugerem; "comite" → "commit" → "comitê" não sugere; uma troca
  entre duas palavras comuns **sugere** (quem barra é o clique); número não sugere. Mutação numa
  cópia isolada (`git archive`), com o diff conferido.
- **F2, gravar, desfazer e trocar** (`internal/dictionary`, `cmd/asrstage`): desfazer devolve o
  arquivo **idêntico, byte a byte**; variante que já é de outro termo é recusada; "ecf" cai no termo
  "ECF" que já existe; arquivo inválido não é tocado; troca do dicionário com um ditado em andamento,
  com o detector de concorrência ligado (se a máquina o tiver).
- **F3, endereços** (`internal/agent/localapi`), com peças falsas: salvar devolve a sugestão e **não
  mexe no dicionário** (AC-022); aprender com um "de" que não está no texto bruto é recusado;
  conflito de versão não gera sugestão; o registro de funcionamento **não contém o texto**.
- **F4, painel** (`web/`): testes do aviso e da lista, lint e build; a imagem do aviso é
  renderizada e olhada nos temas claro e escuro antes de concluir (só a métrica não basta).

**Manual, do Mauro, no fim (F4):**
1. Dita algo com ECF que saia errado como SF, corrige no painel e clica **Aprender**.
2. Dita de novo: tem de sair ECF, **sem reiniciar**.
3. Desfaz em Configurações e dita de novo: volta a sair SF.
4. Reinicia o programa: o que ficou aprendido continua lá.

**Resultado do roteiro manual (Mauro, 30-set e 01-out-2026): OK.** O relato, na ordem em que veio:
corrigiu no painel, clicou em **Aprender**, ditou de novo, usou o **Desfazer**, reiniciou o Agent e
o par continuou. O relato não diz qual termo usou (o roteiro usa ECF e SF) e não separa, passo a
passo, o "sem reiniciar" do passo 2 (ditar de novo veio antes do reiniciar). Não deu detalhe de
contagem nem de texto; **nada além disso foi relatado**.

**Uma ambiguidade que a revisão de 01-out-2026 apontou, e que o relato não resolve.** Pelo roteiro, o
**Desfazer** (passo 3) **remove** o par: depois dele, ele não existe mais. Então "reiniciou o Agent e
o par continuou" (passo 4) só é verdade se o par foi **aprendido de novo** antes de reiniciar, o que o
relato não diz; se não foi, o que continuou foi o dicionário **sem** o par, e o passo 4 (o aprendido
persiste depois de reiniciar) **não foi exercitado**. Os passos 1 a 3 (aprender, o ditado seguinte sair
certo, desfazer) estão como o Mauro os relatou. **O passo 4 fica sem base clara até ele dizer qual
foi a sequência**: se o par foi reaprendido antes de reiniciar, ou se reiniciou logo depois do Desfazer.

Na F2, o Mauro também roda o `asrstage` e confere que o dicionário dele continua igual (**não
relatado**).

## Fatias

Toda fatia roda `go test` e `go vet`, atualiza os documentos que ela muda e termina com **commit
local**, sem `git push`.

| Fatia | O que entrega | O que define (detalhe, não decisão) |
|---|---|---|
| F0 | esta ADR e as emendas da spec (só documentação) | — |
| F1 | motor de sugestão, sem efeito colateral | assinaturas, tokenização, alinhamento com o bruto |
| F2 | gravar, desfazer e trocar na hora; `asrstage` preserva | troca em memória e concorrência; trava do arquivo; dataset novo no `asrstage`; desfazer de termo criado |
| F3 | endereços do painel; sugestão na resposta de salvar; contagens no log | métodos, caminhos e campos; "aparece em N ditados"; nomes dos campos do log |
| F4 | aviso e lista no painel | textos finais; tempo e foco do aviso |

## Decisões da F3 (API local)

Detalhes que a ADR deixou para a F3, e o que ficou decidido. Os campos exatos estão em
`06-api-contract.md` ("PATCH /api/history/{id}" e "Aprendizado"); o código, em
`internal/agent/localapi/aprendizado.go`.

1. **Três rotas**, atrás da mesma cadeia das demais: `GET /api/aprendizado` (a lista),
   `POST /api/aprendizado` (aprender) e `POST /api/aprendizado/desfazer`. O desfazer é `POST` com a
   variante no corpo, e não `DELETE` com ela no caminho: a variante é texto ditado, e texto ditado
   não vai para a URL.
2. **Uma troca por pedido.** Com duas sugestões, o clique da tela faz dois pedidos seguidos. Um pedido
   só com as duas pareceria atômico e não seria: o dicionário grava uma variante por vez, e o erro de
   uma (por exemplo, "SF já vira ECD") não pode esconder o sucesso da outra. A tela conta o que
   aprendeu e o que foi recusado.
3. **O clique refaz as checagens, e o servidor não confia na página.** Nesta ordem: o ditado existe e
   é do dono; o "de" está no `raw_text` **daquele** ditado; o par é uma sugestão que o servidor
   devolveu **para a versão atual** do ditado; e o dicionário decide o resto. A ADR dizia que o
   motor podia rodar de novo no clique, mas o texto de **antes** da edição não é guardado (a coluna
   nova foi descartada), então não há como repetir a entrada. O que o servidor guarda, **só em
   memória**, são os pares curtos que o motor produziu, por ditado e versão (até 32 ditados; o mais
   antigo sai primeiro). Salvar de novo o mesmo ditado apaga os pares do salvamento anterior (a
   versão mudou). Custo: os pares somem ao reiniciar o Agent, e a aba que tinha o botão some com
   ele (a porta e o token também mudam). É a troca por **não guardar o texto ditado de antes**.
4. **O texto de antes** é lido por `Get` antes do `UpdateFinalText`. Se o `Get` falha, a resposta é a
   mesma de antes da etapa (`404`). O `409` de versão não gera sugestão, e o motor nem é consultado.
   A sugestão só sai se a versão gravada for a lida mais um: se alguém gravou entre a leitura e a
   gravação, o texto de antes não é o que a edição trocou, e sai lista vazia.
5. **"Aparece em N ditados".** Conta nos **últimos 500 ditados ativos** do dono, sem o ditado
   editado e **sem a lixeira** (o que a pessoa descartou não mede o alcance que ela vê no
   histórico). Lê o histórico por páginas de 100 e só se o motor chegar a pedir a contagem: uma
   edição sem sugestão não custa a leitura. Erro de leitura: a contagem fica 0, e a edição segue.
6. **Sem dicionário, sem sugestão.** Se não há `dicionario.json` (ou ele não abre, ou é de outro
   usuário), salvar devolve a lista vazia: não se oferece um botão que vai falhar. Sem o aprendizado
   montado (`Preferencias.Aprendizado` nulo), o mesmo, e as rotas de escrita respondem `503`.
   **Emenda (ADR-018, 30-set-2026):** o Agent cria um `dicionario.json` vazio na subida, só se não
   houver nada ali (`garantirDicionario`), então um Agent instalado, que não vem com dicionário,
   aprende desde o primeiro dia. A regra acima passa a valer só quando a criação falha (pasta sem
   permissão, disco cheio), ou quando o arquivo existe e é inválido ou de outro usuário.
7. **Os erros do dicionário viram a frase da tela**, com o status certo (tabela em
   `06-api-contract.md`). A frase do `409` de variante que já é de outro termo **cita o dono**
   (`"SF" já vira "ECD"`): é o que a tela precisa mostrar. O termo é do dicionário da própria
   pessoa, a API é local e atrás do token, e a frase só vai na resposta. `UNAVAILABLE` (`503`) é
   um código novo do envelope, só de `/api/aprendizado*`. Um defeito que não é de regra vira uma
   frase inteira e sem detalhe (`Deu um erro dentro do Agent. Tente de novo.`, e não o `erro
   interno` cru), sem o texto do erro. Ver "Terceira revisão", abaixo.
8. **Log só com contagens e códigos fechados** (`09-security-privacy.md`). O dono da variante, o "de"
   e o "para" nunca entram em linha de log. Um teste com um texto-canário confere o log inteiro,
   incluindo as recusas.
9. **O acoplamento é uma interface** (`localapi.Aprendizado`): a de F2 (`dictionary.Aprendizado`)
   mais `Dicionario(ctx)`, que entrega o dicionário de agora ao motor. Em produção, o
   `aprendizadoDoAgent` a cumpre por `dictionary.Arquivo.Atual`, que relê o arquivo (o mesmo que
   `Aprender` relê antes de gravar, e não a cópia da subida).

## Decisões da F4 (painel)

O que a ADR deixou para a F4 ("textos finais; tempo e foco do aviso"), e o que ficou decidido. Os
textos e o visual estão em `08-ui-ux-spec.md` ("Aprender correção" e "Aprendido no painel"); o código,
em `web/src` (`aprendizado.tsx`, `Aviso.tsx`, `tempoDoAviso.ts`, `AprendidoNoPainel.tsx`, `Painel.tsx`).

1. **Os textos.** `Texto salvo. Aprender “SF” → “ECF”?` (com duas trocas, `“SF” → “ECF” e “RJ” →
   “RF”`); `Aprendido: “SF” vira “ECF” no próximo ditado.`; `Desfeito: “SF” não vira mais “ECF”.`. Os
   termos vão na IBM Plex Mono, entre aspas tipográficas. O "aparece em N ditados" é uma frase à
   parte, em cor secundária, e só existe quando o número é maior que zero.
2. **Verde só no confirmado.** O verde do sistema cobre os termos do "Aprendido:" e mais nada do
   aprendizado; a pergunta fica no off-white da pílula. `estilo.test.ts` lista essa regra entre as
   únicas que usam verde.
3. **Aprender é um pedido por troca**, na ordem do texto (decisão 2 da F3). Com duas trocas, o aviso
   diz o que valeu e, colada, a frase do Agent para o que não valeu; o **Desfazer** do aviso é só do
   que valeu. A frase do Agent vai como veio: o cliente não a reescreve.
4. **O `409` do aprendizado não é conflito de versão.** `pedir` em `api.ts` trata todo `409` como
   `Conflito` (versão velha, releitura, nota de "alterado em outra aba"); nas três chamadas do
   aprendizado ele chega como `ErroDaApi`, com a mensagem. Sem isso, "SF já vira ECD" viraria a nota
   de outra aba.
5. **Tempo e foco do aviso.** Conferido: o aviso já esperava enquanto o ponteiro **ou** o foco
   estivessem nele, e passava o foco para a pílula que o substitui. O que não bastava era o tempo: o
   aviso com ação sumia em 8 s, e a pílula fica no fim da página, a dezenas de Tabs. Decidido: o
   aviso com **Aprender** espera **30 s**; o com Desfazer continua em 8 s, e o que só informa, em 4 s.
   E **Alt+A**, fora de campo de texto, faz o mesmo que o botão enquanto o aviso existe (o desenho do
   Ctrl+Z do Desfazer). Escolheu-se o Alt sozinho: Ctrl+Alt é o AltGr, e Ctrl+Enter, que salva no
   editor, repetiria o salvar como aprender, e uma tecla segurada aprenderia duas vezes.
   `repeat` e um segundo disparo antes da resposta são ignorados. O Alt+A foi conferido no
   navegador de verdade, e não só no jsdom.
6. **Quem troca a pílula é o Painel.** O botão Aprender não tira o aviso: ele fica até a resposta
   chegar, e a pílula do resultado herda o foco. Assim, quem chegou ao botão pelo teclado não cai no
   início da página entre o clique e a resposta. Ajustado na terceira revisão: o foco herdado vai
   para o **texto** da pílula nova, e não para o Desfazer (ver "Terceira revisão", abaixo). A
   pílula ainda espera enquanto o foco estiver nela, até um clique em outro lugar; é só uma espera
   mais longa, e o Desfazer das Configurações é permanente.
7. **A lista das Configurações** é lida quando a tela aparece e quando o aviso aprende ou desfaz algo
   (um contador no Painel), e não tem estado próprio para sincronizar. Desfazer a última linha leva o
   foco ao Salvar configurações, acima dela.
8. **Nada novo em produção.** Nenhuma dependência nova; nenhuma rota além das três da F3.

## Terceira revisão (30-set-2026): os pontos baixos

Fechados em uma só passada, com teste para cada um. Nenhum muda o que a etapa promete.

**Do lado do dicionário e do resolver (F2)**

1. **A inicial maiúscula é só da palavra comum.** `formaDoAlias` acompanha a maiúscula de quem abre
   a frase apenas quando a forma canônica é **toda minúscula e sem dígito**. Uma marca com grafia
   própria (`iFood`, `n8n`, `iPhone`, `macOS`, `eSocial`) fica como foi gravada: "Aifude atrasou"
   vira `iFood atrasou`, e não `IFood atrasou`.
2. **A conferência "a variante age?" roda em todo lugar onde o termo vive** (`agiria` e
   `dominiosDaConferencia`, em `cmd/agent`). Termo sem domínio e termo novo: no domínio
   **desconhecido**. Termo com domínio que a pessoa **não** restringiu: no desconhecido **e** no
   dele, porque aparece nos dois e cada um tem concorrentes diferentes (o restrito a esse domínio só
   aparece nele); a variante recusada em qualquer um é recusada. Termo `restrito: true`: só no
   domínio dele, o único onde age. Restrito sem domínio nunca age e é recusado.
   - Esta revisão tinha primeiro trocado a conferência do termo com domínio, do dele para o
     desconhecido; a revisão seguinte mostrou que isso só trocava um buraco por outro: com "Vite"
     restrito ao dev (prioridade 90) e "Vitti" do dev, sem restrição (60), `Aprender("Vitti",
     "Viti")` era aceito e "Viti" continuava "Viti" no dev, o domínio de "Vitti". Agora é recusado.
   - O termo **sem** domínio também aparece em todo domínio, e não é conferido em cada um de
     propósito: lá o termo restrito a um domínio leva a busca por desenho, e recusar por isso
     recusaria quase toda variante.
3. **A troca pelo N-best também protege o termo no Polish.** `comTermosTrocados` aceita
   `ReasonContext`, além de `ReasonDictionary`: o termo do painel que o Smart escreveu por causa da
   alternativa do ASR, corroborada pelo contexto, tem a mesma proteção do que entrou pela variante.

**Do lado da API (F3)**

4. **O motor concorda com o dicionário sobre o termo desativado.** O dicionário recusa a variante de
   qualquer termo, ligado ou não; o motor só via os ativos, e oferecia um Aprender que sempre falhava,
   com uma frase de `409` falsa (`"sf" já vira "Segredo"`, sendo que o termo está desligado). Agora
   `De` ou `Para` que pertencem a um termo desativado viram descarte `termo_inativo`, e um teste
   confere motor e `Semente.AprenderVariante` nos dois sentidos. Os dois testes que diziam "termo
   desativado não é de ninguém" foram **invertidos** de propósito.
5. **A frase do `409` da sugestão vencida é verdadeira.** Era `Essa sugestão não vale mais. Salve a
   edição de novo.`, e salvar de novo o mesmo texto não devolve a sugestão (antes e depois ficam
   iguais, e o motor não vê troca). Depois: `Esse ditado mudou em outro lugar; corrija de novo para
   aprender.` Mas a sugestão também vence quando o Agent reinicia (elas vivem só em memória) e
   quando o 33º ditado solta o mais antigo, e nesses casos nada mudou em outro lugar. Agora a frase
   não afirma a causa: `Essa sugestão não vale mais; corrija o ditado de novo para aprender.`
   Corrigir de novo, de fato, devolve a sugestão; um teste percorre esse caminho.
6. **Frases inteiras no lugar dos textos crus.** A pílula junta `Não deu para aprender.` com a frase
   do Agent, e por isso todo erro de `/api/aprendizado*` é uma frase completa: o `404` do ditado é
   `Esse ditado não existe mais.` (e não `registro não encontrado`), e o defeito é `Deu um erro dentro
   do Agent. Tente de novo.` (e não `erro interno`). O texto do erro de dentro continua sem vazar.
7. **Gravado, mas não trocado.** Se o arquivo foi gravado e o dicionário em memória não pôde ser
   trocado (na prática não acontece: a validação roda a mesma montagem antes de gravar), a pílula
   dizia que não aprendeu enquanto a lista das Configurações mostrava o par. Decidido: é
   **sucesso**. O Agent devolve o que ficou gravado com `dictionary.ErrGravadoSemTroca`, a API
   responde `200` com `"vale_ao_reiniciar": true`, e a pílula diz `Aprendido: “SF” vira “ECF”, mas só
   depois de reiniciar o Agent.` (e `Desfeito: … , mas só depois de reiniciar o Agent.`), com o
   Desfazer de sempre. O campo só existe nesse caso. O log registra `vale_ao_reiniciar`, sem texto.
8. **`Arquivo.Atual` usa os internos da F2** (`a.mu`, `a.ler`). É aceitável (o mesmo pacote, e é o
   que `Aprendidas` faz), e ficou documentado em `arquivo_atual.go`, com um teste que prende as duas
   leituras à trava: se a F2 mudar a trava, é ali que `Atual` acusa. O ideal é a F2 expor um `Atual`
   público e este arquivo deixar de existir.
9. **Garantias do contrato agora têm teste:** `Cache-Control: no-store` nas três rotas (também na
   recusa), o teto de 16 KiB do corpo (com um corpo de par bom que só o teto derruba), o erro do
   `GET` que não vira lista vazia, e as sugestões de vários ditados vivas ao mesmo tempo (até 32; o
   33º solta o mais antigo).

**Do lado do painel (F4)**

10. **O foco não vai para o Desfazer.** Depois do clique em Aprender, a pílula do resultado recebe o
    foco herdado **no texto** (`tabIndex=-1`, `focoNoTexto` em `Aviso.tsx`), e não no Desfazer: com
    o foco no Desfazer, um Espaço ou Enter a mais, por exemplo para rolar a página, desfazia o que
    acabou de ser aprendido, e depois disso não havia mais botão Aprender. O texto guarda o lugar de
    quem usa o teclado (Tab leva ao Desfazer), o leitor de tela lê o resultado, e nenhuma ação fica
    sob o teclado. A pílula continua esperando enquanto o foco estiver nela (a pausa por foco já
    existia, e é o que deixa alguém chegar ao Desfazer). Quem não tinha o foco na pílula que saiu
    não tem o foco puxado.
11. **A seta não é lida.** A troca da pílula é `“SF” → “ECF”` para os olhos; o leitor de tela ouve
    `“SF” vira “ECF”`, como na lista das Configurações (a seta em `aria-hidden`, e "vira" só para o
    leitor).
12. **Mutações vivas viraram teste:** a ligação que cai no meio das trocas (as outras nem são
    pedidas, e a pílula não diz `Não deu para aprender` ao lado da faixa), a lista das Configurações
    que perde a ligação (acende a faixa) e o Desfazer duplo da mesma linha (um pedido só).

## Quarta revisão (30-set-2026): o que a revisão da terceira achou

Um achado médio e os baixos baratos, cada um com teste (e mutação, em cópia, que o derruba).

1. **(média) A conferência "a variante age?" confere onde o termo vive.** O ponto 2 da terceira
   revisão trocou a conferência do termo com domínio, do dele para o desconhecido, e deixou de
   checar o domínio do próprio termo. A premissa ("o desconhecido é onde mais termos disputam a
   busca") era falsa: o termo restrito de outro termo só aparece no domínio dele. Com "Vite"
   restrito ao dev e "Vitti" do dev, `Aprender("Vitti", "Viti")` era aceito e a variante não agia
   no dev. Agora `dominiosDaConferencia` devolve onde o termo vive (só o domínio dele se é
   restrito; o desconhecido e o dele se tem domínio e não é restrito; só o desconhecido nos demais)
   e a recusa em qualquer um deles recusa a variante. Os três textos que afirmavam a premissa
   (comentário, ponto 2 acima e `18-tool-contract.md`) foram corrigidos.
2. **A frase do `409` da sugestão vencida não afirma a causa.** "Esse ditado mudou em outro lugar"
   só é verdade num dos casos: a sugestão também vence quando o Agent reinicia (elas vivem só em
   memória) e quando o 33º ditado solta o mais antigo. Agora: `Essa sugestão não vale mais; corrija
   o ditado de novo para aprender.` (ver o ponto 5 da terceira revisão).
3. **O teto de 16 KiB do corpo é provado dos dois lados:** um corpo de par bom com exatamente 16 KiB
   é aceito, e com um byte a mais é recusado, em `/api/aprendizado` e em `/desfazer`. Antes, o teste
   só provava "algum teto entre 3 bytes e 20 KiB".
4. **O log do "gravado, mas não trocado" só tem o aviso e a ação.** O teste confere que a linha é
   exatamente o aviso e `acao=aprender` ou `acao=desfazer` (nem a variante, nem o termo, nem a causa),
   e o contrato da API passou a listar esse WARN na seção Log.
5. **A ressalva de reinício é de cada troca.** Com duas trocas no mesmo clique e só uma ficando para
   o reinício, a pílula dizia que as duas só valiam depois de reiniciar. Agora separa: `Aprendido:
   “SF” vira “ECF” no próximo ditado; “RJ” vira “RF”, mas só depois de reiniciar o Agent.` Com uma
   só (ou todas) igual, a frase é a de sempre.
6. **A troca pelo N-best também usa `formaDoAlias`.** O ramo da alternativa do ASR escrevia a forma
   canônica direto, e "Xpto do áudio ficou ruim" virava `descrição do áudio ficou ruim`, com minúscula
   abrindo a frase, enquanto o caminho da variante dava `Descrição do áudio`. A palavra que sai é a
   da hipótese principal, e é a inicial dela que a troca acompanha.

**Não feito, e por quê.** A revisão sugeriu, como opcional, herdar o foco para o texto da pílula só
quando o foco anterior era do teclado (`:focus-visible`), para que o resultado de um clique de mouse
não fique parado na tela até um clique fora (o custo do ponto 6 acima, que já existia com o
Desfazer). Fica como está: o jsdom não implementa `:focus-visible` (nunca casa), então nenhum teste
da herança de foco (de `Aviso.tsx` nem do painel) poderia provar o caso do teclado sem uma dublagem do
navegador, e a mudança vale para toda pílula, não só a do Aprender. Se o custo incomodar no uso real,
é uma mudança própria, com prova em navegador de verdade.

## Revisão

Depois de uso real: quantas sugestões apareceram, quantas viraram aprendizado e quantas foram
desfeitas (só contagens; quais o registro terá é da F3); se algum aprendizado causou correção falsa.
Com dado, reabrir: acento e maiúscula sozinhos (decisão 3), o limite de 4 palavras, e a ida do
dicionário para o banco com o sync (etapa 5).
