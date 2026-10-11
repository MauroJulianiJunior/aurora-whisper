# ADR-020 — Adicionar termo pelo painel: a palavra certa, as grafias erradas e a dica

Status: Accepted (o pedido e a forma, Mauro, 01-out-2026: **somar** um "Adicionar termo" em
Configurações, sem tirar o Aprender pelo histórico). As decisões 1 a 3 do fim desta ADR ("decisões
do Mauro") estão implementadas com a recomendação, **até ele dizer o contrário**; a 4 está em aberto,
e a 3 pede confirmação. A redação desta ADR ainda não passou pela revisão dele.
Data: 2026-10-01
Contexto: depois da etapa 3 (ADR-017) e do plano da etapa 5 (ADR-019); pedido do Mauro de 01-out-2026,
sobre `41c30be`
Emenda: a **ADR-017** (revoga a decisão 7, "a lista que o ASR recebe como dica não muda", e a
alternativa descartada "promover o termo novo ao prompt do ASR"); `03-acceptance-criteria.md`
(AC-064); `05-data-model.md` (os valores de `origem` e a prioridade 100); `06-api-contract.md`
(seção "Termos"); `08-ui-ux-spec.md` (a seção "Dicionário" em Configurações); e
`13-implementation-plan.md` (TASK-019, fatias T0 a T4). **Não muda o núcleo** (ADR-007) e **cumpre
o AC-022** (nada entra no dicionário sem a pessoa pedir: aqui ela mesma digita).

## O problema

> "Se a gente puder colocar no dicionário as correções quando ele não identificar corretamente a
> palavra, vai ficar muuuuito bom." (Mauro, 01-out-2026)

O Aprender pelo histórico (ADR-017) só ensina o que o motor de sugestão acha numa edição, e o que ele
ensina é uma variante: corrige o texto **depois** do ASR, mas não ajuda o ASR a escrever certo. No
mesmo dia, "RAG" foi posto à mão no `bin\dicionario.json`, com prioridade 90 e as variantes que o
modelo escrevia ("HACI", "rague", "hage"), e saiu certo de primeira no ditado: o termo estava no
**prompt** do ASR. Não há como a pessoa fazer isso sem abrir o arquivo.

## Decisão

1. **Um "Adicionar termo" em Configurações, somado ao Aprender.** A pessoa digita a palavra certa,
   opcionalmente as grafias erradas, e marca se a palavra vira **dica para o reconhecimento de voz**
   (vai no prompt do ASR). Liga e desliga a dica depois, e remove o que pôs.
2. **Nada novo no JSON (ADR-019), um VALOR novo.** O termo da pessoa leva `"origem": "adicionado"` e
   cada grafia dela leva `alias_origem[v] = "adicionado"`. A dica é a **prioridade**: com dica,
   `100` (`PrioridadeDica`); sem, `50` (`PrioridadeTermoAprendido`). Um Agent de `41c30be` carrega o
   arquivo e trata o termo como um termo comum (só `"painel"` é `AliasOnly`), o que ele é: casamento
   exato, fonético e guarda do Polish. Um Agent anterior ao campo `origem` já recusa o arquivo hoje,
   por causa do `"painel"`; o valor novo não piora nada. `DictionaryEntry.Adicionado` vem da struct
   (`ParaEntradas`/`Popular`), e não do JSON; um teste confere que `Gravar` não escreve campo novo.
3. **A ordem do prompt: as dicas dela primeiro.** `VocabularioTermos` põe primeiro as dicas da pessoa
   (termo `adicionado`, ativo, prioridade 100 ou mais), na ordem do arquivo (a ordem em que ela as
   adicionou), e depois o resto como sempre (prioridade, depois ordem alfabética). Termo `adicionado`
   **sem** a dica nunca vai ao prompt, nem num dicionário com espaço sobrando. A regra é uma só,
   `dictionary.DisputaOPrompt`, que o vocabulário e a conta das "ativas" do log da subida usam.
4. **O orçamento é contado sobre as dicas dela, em runas.** `usadas` = a soma das runas das dicas
   dela mais o `", "` entre elas; a tela diz "Sobram N de 140 caracteres para dicas." (não em
   "termos": quantos cabem depende de quais). **A dica que não cabe é recusada** (`409`, nada
   gravado): uma dica nova nunca empurra outra dica dela. Quando cabe, empurra para fora termos do
   dicionário de antes, e a resposta diz quais ("Saiu das dicas: DCTFWeb.").
5. **O prompt é trocado em memória, e vale no próximo ditado (revoga a decisão 7 da ADR-017).** O
   vocabulário vai **por pedido** ao whisper-server (`asr.ASROptions.Vocabulary`, campo `prompt`):
   nada no ASR precisa reiniciar. `winruntime.Dicionario` ganha `Vocabulario *string` (nil mantém o
   que valia), aplicado junto com o resolver, na goroutine de Run, com a máquina parada. **A corrida
   que havia:** `transcrever` lia `rt.vocabulario` dentro da goroutine do ASR; agora o copia antes de
   soltá-la, e o ditado em voo segue com o prompt com que foi.
6. **O prompt é remontado do arquivo relido em toda gravação do painel** (`gravarETrocar`), também no
   Aprender e no Desfazer, que não o mudam (variante não entra, e o termo do painel é `AliasOnly`).
   Efeito colateral: uma edição feita à mão no arquivo desde a subida passa a valer no prompt já na
   próxima gravação do painel, e não só ao reiniciar. O log diz só contagens (`termos_no_prompt`,
   `vocabulario_runas`).
7. **O que é recusado.**
   - **A palavra certa:** quebra de linha ou tab (recusadas **antes** do `strings.Fields`, que as
     viraria espaço sem avisar); mais de 4 palavras ou 40 runas; caractere de controle; sem nada que
     se compare. Se ela **vira termo agora** (nova, ou a do painel adotada), também a palavra comum.
     Termo que é variante de outro (`ErrTermoEVarianteDeOutro`); termo desligado (`ErrTermoInativo`).
   - **As grafias:** até 5 por pedido e 10 dela por termo; cada uma com 2 a 40 runas e até 4
     palavras; as recusas de integridade do Aprender (`ErrSemNovidade`, `ErrJaExiste`,
     `ErrVarianteDeOutroTermo`), dizendo qual grafia (`ErroDaVariante`); a palavra comum.
   - **Palavra comum, na palavra certa**, é `learning.Arriscada`: um texto em que **nenhuma** palavra é
     de conteúdo, só de ligação ("de", "das", "no"), de polaridade ("não", "mais"), número ou numeral
     ("2025", "dois"). Basta uma palavra de conteúdo: "Nada Consta" passa como termo.
   - **Palavra comum, na grafia**, é `learning.GrafiaArriscada`, mais estrita (revisão de 01-out-2026),
     porque a grafia é trocada pelo termo em todo ditado e o que ela tiver a mais some do texto. Além do
     que `Arriscada` recusa: a palavra de **polaridade em qualquer lugar**, que é a regra do motor de
     sugestão ("um alias com uma delas em qualquer lado nunca é a correção de um termo", em `avaliar`):
     com "não rague", "eu não rague no projeto" virava "eu RAG no projeto"; e a grafia em que **toda
     palavra é funcional e frequente** (`palavrasFuncionais`, uma lista fechada: pronomes pessoais,
     possessivos e demonstrativos; ser, estar, ir, ter e haver curtos; advérbios curtos e
     interrogativos; as muletas "né", "tá", "tipo"): com "eu", "eu acho que eu vou" virava "RAG acho
     que RAG vou". Não é um léxico de frequência. Uma palavra de conteúdo ao lado de uma funcional
     passa ("n oito n", "eu rague"). O motivo da polaridade é `muda_sentido`, para a tela dizer o
     porquê ("tem palavra que muda o sentido da frase, e ela sumiria do texto").
   - **Não agiria:** cada grafia nova tem de agir (o Smart de depois, diante dela sozinha, escreve o
     termo, em todo domínio onde o termo vive): `*ErroNaoAgiria{Variante}`.
   - **Quebraria (regressão nova):** o termo com dica tem prioridade 100 e casamento fonético, e a
     busca põe a prioridade antes do casamento: ele pode passar na frente do alias confirmado de
     outro termo ("Viti" deixaria de virar "Vitti" com "Vite" na dica). A cada Adicionar, dica e
     Remover, `conferirMudanca(antes, depois)` exige que **todo alias que agia antes continue
     agindo**; o primeiro que não age volta como `*ErroQuebraria{Variante, Termo}`, e nada é gravado.
     Entra no `Arquivo` como o gancho `ConferirMudanca`, ao lado de `ValidarAprendida`.
   - **Quebraria a forma canônica de outro termo** (revisão de 01-out-2026): a fonética sozinha não é
     aplicada, mas a **corroborada pelo contexto** é. Com "Vitti" (60) no dicionário, "Vite" com a dica
     era aceito, e "reunião com o Vitti amanhã", com "npm create vite@latest" perto do cursor, virava
     "reunião com o Vite amanhã". Depois dos alias, a conferência resolve a forma canônica de cada
     termo ligado que a mudança alcança, **sozinha e com o contexto citando os termos mudados** (o
     pior caso); a que resolvia para ela mesma antes e deixaria de resolver volta como
     `*ErroQuebraria{Variante: canônica, Termo: canônica, Vira}`. A tela diz "Com “Vite”, “Vitti”,
     escrito certo, passaria a virar “Vite”.". Efeito: o termo cuja grafia só agiria **na frente** de
     outro termo por fonética ("Vity" com "Viti", diante de "Vite" e "Vitti") também troca a forma
     daquele quando o contexto o cita, e é recusado já no Adicionar.
   - **50 termos** dela, no máximo.
8. **O custo da conferência, medido (01-out-2026, máquina de desenvolvimento).** Conferir todo alias
   custou 2,4 ms no dicionário do repositório (45 termos, 17 alias), e **1,3 s** no teto (50 termos
   dela com 10 grafias cada, sobre o do repositório: a busca em memória percorre os alias de todas
   as entradas). Passou de 300 ms, e a conferência ficou restrita, como o plano previa, ao alias que
   a mudança **alcança**: o que tem um trecho (de até 4 palavras, como o Smart procura) que casa, por
   forma, alias ou fonética, com uma entrada que mudou, na versão de antes ou na de depois, num
   dicionário só com essas entradas e tudo liberado (sem domínio, restrição nem origem do painel).
   Para o alias que ela não alcança, toda busca que o Smart faz sobre ele devolve o mesmo antes e
   depois. Com o filtro: **1,0 ms** e **14,5 ms** (`BenchmarkConferirTermos` e
   `BenchmarkConferirTermos_NoTeto`, em `cmd/agent`). Com a conferência da forma canônica (revisão de
   01-out-2026), **1,6 ms** e **16,5 ms**: a mesma regra do filtro vale para a forma canônica.
9. **Termo que já existe** (comparado como o Search compara):
   - de origem `adicionado`: as grafias novas entram, e a dica segue a caixa;
   - de origem `painel`: é **adotado** (`origem` passa a `adicionado`, a prioridade segue a dica); as
     variantes do painel ficam do painel (saem pelo Desfazer do "Aprendido no painel");
   - do dicionário de antes: as grafias entram e a dica dele **não muda** (decisão 3, abaixo). O
     pedido com a dica não some calado (revisão de 01-out-2026): com grafias, a resposta traz
     `dica_ignorada` e a pílula diz "A dica não muda por aqui: “DCTFWeb” já era do dicionário."; só
     com a dica, é `409` "“ECF” já é do dicionário: a dica dele não muda por aqui." (e não "já está
     assim", que era falso sobre a dica).
   - Sem nada a mudar (nenhuma grafia nova e a dica igual), `400`.
10. **Remover** tira as grafias `adicionado` e, se o termo é dela, o termo. Se sobrarem variantes do
    painel, a entrada volta a `painel`, com prioridade 50. Num termo de antes, só as grafias dela
    saem. Adicionar e remover, sem mais nada ter mudado, devolve o arquivo **byte a byte**.
11. **A API local** (`06`, "Termos"): `GET /api/termos`; `POST /api/termos`, `/api/termos/dica` e
    `/api/termos/remover`. A mesma cadeia (`guard.Protect`), o texto só no corpo (16 KiB), `no-store`,
    e toda mutação devolve a lista inteira, mais `sairam_das_dicas`, `ja_existia` e
    `vale_ao_reiniciar`. `Preferencias.Termos` é uma interface à parte (`dictionary.Termos`), para os
    dublês de hoje não quebrarem: nil responde `503`. Quem a cumpre é o mesmo `aprendizadoDoAgent`,
    com a mesma trava.
12. **O log** só tem contagens e a classe do erro: nunca o termo nem a grafia (`09`).
13. **A tela** (`08`, "Dicionário"): o menor que funciona, nos tokens do `index.css`, claro e escuro.

## Sincronização (F8 da ADR-019): só nota

As grafias `adicionado` cabem no "par curto" da ADR-019 (variante, termo, restrição e domínio, com uma
origem **na linha do banco**, e não no JSON). O termo sem grafia e a dica não são um par: a F8 decide
se cria uma segunda entidade, `{termo, dica}`, que o PC de destino passaria pelas mesmas checagens
(cabe? quebraria?), ficando "não aplicado neste PC" se falhar. Nada novo no JSON de nenhum dos dois
jeitos. Esta etapa não grava `sync_event`, como a ADR-017.

**Decidido pelo Mauro em 07-out-2026 (D1 da F8; ADR-019, "Decisões da F8"): o "adicionado" fica em
cada PC.** Nem o termo, nem as grafias `"adicionado"`, nem a dica sobem para a nuvem; sobem só as
trocas do botão Aprender (`alias_origem` `"painel"`). Um termo do painel adotado aqui continua
sincronizando as variantes do painel dele; as grafias que a pessoa digitou, não. Subir as grafias ou
a dica fica para depois, e pode entrar sem refazer a F8.

## Alternativas descartadas

- **Um campo novo no JSON para a dica, ou para a prioridade de antes.** O `DisallowUnknownFields`
  faria um Agent anterior recusar o arquivo (ADR-019). A prioridade carrega a dica, e por isso a dica
  de um termo de antes não muda pelo painel (decisão 3).
- **Deixar a dica nova empurrar a dica mais antiga dela.** O desempate decidiria em silêncio qual dica
  dela sai; a tela mentiria sobre o que está no prompt.
- **Reiniciar o ASR para trocar o prompt.** Não é preciso: o prompt vai por pedido.
- **Conferir todo alias a cada mudança, sem filtro.** Passa de um segundo no teto (decisão 8).

## Riscos, e o que não foi verificado

- **O detector de corrida não rodou:** esta máquina não tem cgo (nem gcc nem Docker em execução). O
  teste de que o ditado em voo segue com o prompt de antes roda com um processador só, onde o pior
  caso é determinístico, e o de troca concorrente roda sem `-race`. Rodar `go test -race
  ./internal/agent/winruntime/` onde houver cgo.
- **A conferência olha os alias e a forma canônica escrita como está, não as outras formas dela.** Um
  termo novo de prioridade 100 que case por fonética com outro termo pode tirar dele a normalização
  de uma grafia **diferente** da canônica ("dag", minúsculo, deixaria de virar "DAG" se "RAG" ganhasse
  a busca): a fonética sem contexto não é aplicada, então o texto não piora, só deixa de melhorar. A
  forma canônica escrita certa é conferida com o contexto (decisão 7). Não medido.
- **A palavra comum da grafia é uma lista fechada, não um léxico de frequência.** Uma grafia que é
  palavra de conteúdo comum ("casa", "acho", "fazer") passa: a pessoa a digitou, e a conferência "age?"
  confirma só que ela age. As funcionais frequentes e a polaridade são recusadas (decisão 7).
- **O prompt de um Agent de antes.** Um Agent de `41c30be` não conhece a regra "sem dica não vai ao
  prompt": num dicionário com espaço, o termo dela sem dica (prioridade 50) pode entrar no prompt
  dele. Degradação inofensiva.
- **O ganho no ASR não foi medido nesta etapa:** só o caso RAG, à mão. A latência de um prompt cheio
  é a do orçamento de 140 runas, que já existia.
- **Roteiro manual (o Mauro):** com o Agent rodando, adicionar "RAG" com "rague, hage" e a dica,
  ditar "me explica como funciona um RAG" sem reiniciar, desligar a dica, remover; conferir no log a
  linha `aprendizado: dicionário atualizado` com `termos_no_prompt` e `vocabulario_runas`, e nenhum
  termo.

## Testes

- `internal/dictionary/termos_test.go`: adicionar com e sem dica, a ordem do prompt, o orçamento (cabe
  até o último caractere; a que não cabe é recusada dizendo quanto sobra), quem saiu das dicas, o
  termo dela, o do painel adotado, o de antes, todas as recusas, os tetos, a dica, o remover que volta
  ao que era (também byte a byte no arquivo), nenhum campo além dos de `41c30be` no JSON, e o
  `ConferirMudanca` sobre o arquivo.
- `internal/learning/arriscada_test.go`: o que é palavra comum e o que não é, no termo e na grafia (a
  polaridade em qualquer lugar e as palavras funcionais).
- `internal/agent/winruntime/vocabulario_test.go`: o prompt troca no próximo keydown, nunca no meio de
  um ditado; o ditado em voo segue com o prompt de antes; troca concorrente.
- `cmd/agent/termos_test.go`: grava e troca o prompt na hora, dica e remover, não agiria, quebraria
  ("Vite" com e sem a dica), a forma canônica de outro termo trocada com o contexto ("Vitti" com
  "Vite" na dica), a grafia que só agiria na frente de outro termo, palavra comum (e a grafia com
  polaridade ou só funcional, com a negação do ditado no lugar), gravado sem troca, sem dicionário, o
  filtro da conferência e os benchmarks.
- `internal/agent/localapi/termos_test.go`: as rotas, cada erro na frase da tela, `503`, corpo ruim,
  `no-store` e o log sem texto.
- `web/src/termos.test.tsx`: o formulário, a lista, a dica, o Remover, os avisos, os erros e o foco.

## Fatias

| Fatia | O quê | Commit |
|---|---|---|
| T1 | `dictionary/termos.go` e `arquivo_termos.go` (puro e sobre o arquivo), `DisputaOPrompt` e a ordem do prompt | `feat(dictionary)` |
| T2 | winruntime (`Vocabulario`, a corrida) e cmd/agent (`gravarETrocar`, `conferirTermos`) | `feat(agent)` |
| T3 | localapi (`/api/termos`) e `learning.Arriscada` | `feat(localapi)` |
| T4 | web (`Termos.tsx`) | `feat(web)` |
| T0 | esta ADR e as emendas | `docs` |

## Decisões do Mauro (a implementação usa a recomendação até ele dizer o contrário)

1. **Dica marcada por padrão no formulário?** Recomendado e implementado: **sim**. O caso RAG
   funcionou porque estava no prompt.
2. **As dicas dela passam na frente das do dicionário de antes**, empurrando-as para fora com aviso;
   quando as dela sozinhas enchem os 140 caracteres, a nova é recusada, em vez de tirar outra dela.
   Recomendado e implementado: **sim**.
3. **Termo que já existe no dicionário de antes:** as grafias entram nele, mas a dica dele não muda
   pelo painel. Recomendado e implementado: **sim**, porque sem campo novo o Remover não teria como
   devolver a prioridade de antes. **A confirmar** (revisão de 01-out-2026): o "RAG" posto à mão no
   `bin\dicionario.json` (prioridade 90, sem origem) é exatamente esse caso: o painel não o lista (as
   grafias dele não têm a origem "adicionado"), não liga nem desliga a dica dele, e o Remover responde
   "não foi adicionado por você". Hoje o pedido com a dica pelo menos diz isso (decisão 9).
4. **A dica é a prioridade, e por isso também muda a precedência da correção** (revisão de
   01-out-2026). A tela promete que a caixa só decide o prompt, mas com a dica o termo tem prioridade
   100 e, sem ela, 50: uma grafia dela pode só agir com a dica ligada, e então a dica não desliga mais
   (o Agent recusa com o `ErroQuebraria` da própria grafia) e readicionar sem a dica também é recusado.
   **Não decidido; nada mudou no modelo.** O que foi feito: a frase do erro diz que a grafia depende da
   dica e qual é a saída hoje ("“Viti” só vira “Vity” com a dica ligada. Para desligá-la, remova “Vity”
   e adicione de novo sem essa grafia."); e a conferência da forma canônica (decisão 7) já recusa no
   Adicionar o caso da revisão ("Vity" com "Viti", diante de "Vite" e "Vitti"). As opções:
   - **(a) sem a dica, 99 em vez de 50.** `DisputaOPrompt` já tira do prompt o termo `adicionado`
     abaixo de 100, então a caixa passa a mexer só no prompt. Custo: num Agent de `41c30be`, que não
     conhece essa regra, o termo sem dica entraria no prompt com mais folga; e o termo sem dica passa
     na frente de quase todo o dicionário de antes (o "Vite" sem dica, diante de "Vitti" com "Viti",
     passaria a ser recusado também).
   - **(b) manter 50**, e oferecer remover só aquela grafia (uma rota e uma ação novas para a grafia
     "adicionado"), em vez de remover o termo inteiro.
