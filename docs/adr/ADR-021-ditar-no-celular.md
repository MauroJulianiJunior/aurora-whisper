# ADR-021 — Ditar no celular: a voz vira texto no servidor, com o provedor e a chave da pessoa

Status: **Accepted (10-out-2026).** As quatro decisões do Mauro estão em "As decisões do Mauro", como
ele as deu. O resto é desenho desta ADR, feito a partir do plano e da crítica do mesmo dia. **Tudo o
que não sai dessas quatro decisões está marcado [derivada]**, com uma linha "Para quem usa" que diz o
que muda na mão de quem dita. As "Escolhas menores" também são derivadas, cada uma com o padrão e a
alternativa.
- **A aprovação, do Mauro em 10-out-2026:** a ADR e as derivadas **com os padrões** ("Aprovo com os
  padrões": as 27 escolhas menores ficam no padrão, inclusive a calibração e a medição sem arquivo de
  áudio); **a S com 10 s com o painel aberto** e 60 s fechado; e, para a D0, **ele vai ditar dado de
  cliente pelo celular às vezes** ("Sim, às vezes"): a política e o dossiê dizem que a voz e a dica
  podem levar dado de terceiro.
- **A LG continua pedindo as respostas dele** (as pendências de LGPD da instância).
- **Nenhum áudio real sai do aparelho antes do OK dele na política e no dossiê reescritos (D0).**
- **O estado em 11-out-2026:** as fases 1 e 2 estão na `main`. A fase 1 (S, D0, C1, C2, C3a, C3b, C4,
  C5a, C6a, C6b e C6c) e a fase 2 (a C3-correções, a C6-alinhar, a C5b com a revisão adversarial e a
  C5c). **A C7 (o deploy e o teste no aparelho) vem a seguir:** o Mauro autorizou o deploy em
  11-out-2026, e o OK dele na política e no dossiê continua sendo o portão do primeiro áudio real. A
  C8 (opcional) e a LG estão pendentes. O estado de cada uma está na coluna "Estado" de "Subfatias", e
  o que a implementação fixou (nomes, status, números, desvios) está em "O que a implementação fixou"
  (a fase 1) e "O que a implementação fixou (a fase 2)". O que a implementação mudou no texto de baixo
  vem marcado **[C1, 10-out-2026]** e parecidos: a subfatia e o dia em que ela foi feita.

Data: 2026-10-10 (estado e notas da implementação: 2026-10-11)
Contexto: depois da F11 da ADR-019. Em 10-out-2026 a nuvem já estava no ar, o PC conectado e o app
instalado no Android. O pedido do Mauro é do mesmo dia, e a leitura do código é a desse dia. Os
documentos que a preparação do código aberto reescreve (a política, o dossiê, o `09`, o `12` e a
ADR-019) são citados pelo nome da seção, sem número de linha.
Emenda:
- **ADR-019:** a decisão 9 e a derivada D1 (o ditado só nasce no PC); "Fora do escopo" (o ditado no
  celular); a decisão 8 e "O que nunca vai" (a chave do Polish).
- **ADR-016:** "a chave nunca sai do PC" passa a valer para a chave **do PC**.
- **ADR-003 e `18-tool-contract.md`:** o `AudioBuffer` passa a aceitar áudio comprimido.
- **ADR-017 e ADR-020:** só uma nota. O termo aprendido pode ir como dica ao provedor da nuvem, e no PC
  continua fora do prompt.
- **Spec:** `02-prd.md` (REQ-028, REQ-043 e o "Ditar no celular continua fora");
  `03-acceptance-criteria.md` (AC-040, AC-041, AC-058 e os ACs novos); `05-data-model.md` (três
  tabelas); `06-api-contract.md` (as rotas novas, a lista fechada dos códigos de erro e a seção "ASR
  cloud"); `08-ui-ux-spec.md` (:626 e "Configurações do celular"); `09-security-privacy.md` (THR-007,
  "Polish: o que sai da máquina", "Nuvem: o que sai da máquina" e uma seção nova);
  `10-env-and-secrets.md` (o segredo novo); `12-rollout.md`; `13-implementation-plan.md`;
  `20-traceability.md`.
- **Fora da spec:** o `README.md` e o `PRODUCT.md` (Privacidade), a política de privacidade e o dossiê
  de LGPD.

**Não muda o núcleo** (ADR-007) **nem o jeito de ditar no PC.** O PC recebe o ditado do celular pelo
sync, como já recebe o de outro PC. No PC mudam o laço do sync (S), a origem que o histórico mostra
(C1) e, sem mudar o comportamento, o lugar de três peças que o servidor passa a usar: as conferências
do lote e a palavra comum (C2) e o adaptador do Polish (C5a). Por isso a C5a tem teste no PC.

## O problema

O app do celular mostra e edita o histórico, mas não dita. A ADR-019 deixou isso fora de propósito
(decisão 1: "ver e editar"; "Ditar no celular continua fora", no `02`). O Mauro descobriu esse limite
com o app já instalado e pediu o ditado no celular como a próxima etapa (ADR-019, "O PC na nuvem de
produção, em 10-out-2026").

Na mesma rodada ele fez duas queixas:
- a busca de 60 s do PC parece lenta;
- a lista do painel do PC só se atualiza quando ele volta à tela.

### Onde estamos (leitura do código de 10-out-2026; nada rodou)

- **Nada no servidor cria um ditado.**
  - `POST /transcriptions` e `/asr/transcribe` caem no `404` de propósito (`internal/server/rotas.go:70-75`
    e `:136-140`; ADR-019, decisão 9 e D1).
  - O `Store` não tem uma operação de criar (`internal/server/cloudstore/types.go:455`), e o push
    exige o token de um PC (`ErrNoDevice`, `types.go:447`).
  - O `Store.Get` devolve o ditado ou `ErrNotFound` (`types.go:458`): **a lápide não aparece nele.**
  - Dois testes travam o `404`: `authtest.testNaoMontadas` e `contracttest.testUnmountedRoutes`.
- **O que já aceita um ditado sem aparelho:**
  - na tabela, `device_id` pode ser nulo e `source_platform` é texto livre
    (`migrations/0003_transcriptions_table.sql`); a chave primária é `(user_id, id)`;
  - o ponto único de gravação já grava pela sessão do navegador, sem aparelho, no Edit e no Trash;
  - o pull entrega o ditado sem `device_id`;
  - o PC aplica um `upsert` válido sem olhar `source_platform` (`internal/sync/decisao.go:169-205`;
    falta um teste que prove). **[C1, 10-out-2026: o teste existe, `TestDecidirMudanca_DitadoDoApp` em
    `internal/sync/decisao_test.go`, e há o ponta a ponta.]**
- **O acesso de hoje** (`internal/server/rotas.go:20-32` e `proteger`, `:150-172`):
  - `AcessoSessao` (o navegador), `AcessoAparelho` e `AcessoQualquer` (a sessão ou o token);
  - **qualquer token de aparelho passa no `AcessoAparelho`**, que é o push e o pull do histórico
    inteiro. Hoje todo aparelho é um PC.
- **Os erros** vão em `error.code`, em maiúsculas (`internal/server/httpx.go:17-26`; o pareamento já
  usa `CODIGO_INVALIDO`). O app ainda distingue alguns `404` pela frase (`web/src/nuvem/cliente.ts`, o
  comentário do `aprendidos`). O `decodeJSON` põe a mensagem do decodificador no `400`
  (`httpx.go:91`).
- **Não há limite por conta nas rotas de sessão.** O `LimitesPadrao` (`internal/server/nuvem.go:64-71`)
  só cobre o login, o push e o pareamento.
- **O "Apagar meus dados"** troca o `sync_epoch` da conta e apaga as tabelas dela numa transação, mas
  **mantém a sessão** de quem apagou (`internal/server/pgstore/contas.go:91-106` e `:321-353`).
- **O contrato do ASR só conhece PCM.** O `asr.AudioBuffer` é `[]int16` mono em RAM
  (`internal/asr/types.go:14-26`), e o único provider concreto é o whisper local
  (`internal/asr/provider.go:9-28`). O gateway antigo `internal/server/asr_gateway.go` é código
  morto: só o servidor de teste o monta (`internal/server/server.go:135`). Uma branch ainda fora da
  `main` remove esse servidor antigo da F1-D, com o gateway. **[C5c, 11-out-2026: o gateway e o
  servidor em memória da F1-D saíram da `main`, com a remoção refeita do zero, sem a branch.]**
- **O Smart roda no servidor sem mudar nada.** O motor é Go puro (`internal/resolver`,
  `internal/dictionary`). A nuvem guarda o aprendido (`ListLearned`, até 500 pares). O dicionário
  completo fica em cada PC (ADR-019, decisão 7).
  - Duas peças moram em pacotes do Agent: as conferências do lote (`dictionary.ConferenciasDoLote`,
    `internal/dictionary/lote.go:177-185`) e o `SmartDaSemente` (`internal/agent/aprendiz/conferir.go`).
  - A palavra comum (`learning.Arriscada`) mora num pacote que importa o `internal/storage`.
  - **[C2, 10-out-2026: as conferências do lote e o `SmartDaSemente` foram para `internal/smartconta`.
    A `Arriscada` e a `GrafiaArriscada` ficaram em `internal/learning`: o léxico e o tokenizador são
    usados em dezenas de pontos do motor de sugestão, e mover geraria churn grande sem ganho de
    comportamento. O que saiu foi a aresta de `learning` para o `storage` (o `confirm.go` importava o
    `storage` só para o `NewID`), por um pacote novo, `internal/ids`.]**
- **O Polish é de quem usa.** O `internal/ia` fala com a OpenAI e a Anthropic com a chave que a pessoa
  colou, só por HTTP/1.1, sem seguir redirecionamento e sem repassar o corpo de erro
  (`internal/ia/ia.go:1-35`).
  - A chave mora no Gerenciador de Credenciais do Windows e nunca sai do PC (ADR-016; ADR-019,
    decisão 8; AC-040).
  - A tela mostra só o fim dela: `chaves.Final`, no máximo 4 caracteres e nunca mais que um quarto da
    chave (`internal/agent/chaves/chaves.go:150`).
  - **O prazo do Polish no PC cresce com o texto:** `prazoPolish`, de 3,5 s a 8 s
    (`internal/agent/winruntime/polish.go:45-68`). Os 10 s do `ia.TimeoutPadrao` são do teste da chave.
  - O adaptador do ditado (`clienteDoDitado` e a política de esforço) mora em `cmd/agent/polish.go`.
    **[C5a, 10-out-2026: o prazo e o adaptador foram para `internal/polish` (`polish.Prazo`,
    `polish.PoliticaEsforco`, `polish.Montagem{...}.Abrir()`). Os nomes `prazoPolish` e
    `clienteDoDitado` do texto de baixo são os do desenho.]**
- **O servidor tem prazos fixos** (`cmd/server/serve.go:38-43`): 10 s para os cabeçalhos, 30 s para
  ler o pedido e 60 s para escrever, contados do fim dos cabeçalhos. O desligamento espera 15 s, e o
  `stop_grace_period` da API é 30 s. Um corpo JSON tem limite de 1 MiB.
- **O contêiner da API** (`deploy/compose.yml`):
  - memória: `mem_limit: 256m` e `GOMEMLIMIT: 200MiB`, **sem `memswap_limit`** e sem `ulimits`;
  - disco: sistema de arquivos `read_only`, com `/tmp` em tmpfs de 16 MB;
  - segredos: todos por arquivo `*_FILE`, em `/run/secrets`, nunca por variável de ambiente. Cada um é
    declarado com `file:` obrigatório.
- **Os segredos do deploy:** o `gerar-segredos.sh` faz 32 bytes em hexadecimal (64 caracteres) e
  **recusa rodar se a pasta `segredos/` já existe** (`deploy/scripts/gerar-segredos.sh:36` e `:47`); o
  `subir.sh` exige cada arquivo da lista `SEGREDOS_ESPERADOS` (`deploy/scripts/subir.sh:92` e
  `:175-179`).
- **A VPS de referência é dividida com outros serviços.** Ela tem pouca CPU, e um critério de parar o
  rollout é "o servidor impacta serviços que já rodam nele" (`12-rollout.md`, os critérios de parar). A
  entrada https é o proxy que já roda lá (o Traefik, no deploy de referência), e encostar nele é ponto
  de parada (ADR-019).
- **O que foi prometido por escrito:**
  - "o áudio não sai do PC": a política (§1, §3 e §9), o dossiê (§1 e §6, "Não leva áudio"), o `09`
    ("Polish: o que sai da máquina" e "Nuvem: o que sai da máquina"), o README e o `PRODUCT.md`;
  - a tela do app diz "Nunca vão: o áudio, ..., a chave do Polish" (`web/src/nuvem/ConfiguracoesNuvem.tsx:454`),
    e dois testes travam essa frase.
- **A revisão de LGPD foi aprovada pelo Mauro em 10-out-2026** (ADR-019, "A revisão de LGPD"), com os
  campos `[A PREENCHER]` que ainda faltam (veja "Pendências de LGPD").
- **O celular não guarda nada.** O `web/bundle.test.ts:188-190` recusa `localStorage`,
  `sessionStorage` e `indexedDB` no build da nuvem, e o `:187` recusa a frase "Gerenciador de
  Credenciais". O service worker não toca em `/api` (`web/pwa/sw.js`).
- **Nenhum código grava áudio no navegador.** Não há `getUserMedia`, `MediaRecorder` nem
  `AudioContext` em `web/src`. O `.gitignore` já recusa `*.webm`, `*.opus` e os outros formatos de
  áudio.
- **O PC busca a nuvem a cada 60 s** (`internal/sync/laco.go:62`; ADR-019, "Decisões da F7a/b",
  decisão 1, confirmada pelo Mauro em 06-out-2026). O painel do PC relê a lista quando volta à aba
  (`web/src/Painel.tsx:208-224`), mas nada o avisa de que um pull trouxe mudança.

### Conflitos com a spec e com o que foi prometido

Pelo `CLAUDE.md`, conflito entre documentos é ponto de parada. Pelo `00-index.md`, vale a decisão
humana mais recente, desde que registrada, e esta ADR é o registro.

1. **ADR-019, decisão 9 e D1, e "Fora do escopo"** (o ditado no celular). **Resolvido:** decisão 1 do
   Mauro. Continuam sem rota `/asr/transcribe`, `/resolve` e `POST /transcriptions`, e os dois testes
   de `404` continuam valendo para elas. O ditado do celular nasce numa rota nova (decisão A).
2. **"O áudio não sai do PC"** (política, dossiê, `09`, README, `PRODUCT.md`, tela do app). **Resolvido:**
   a política e o dossiê são reescritos (D0), e quem dita aceita um aviso específico (decisão H). Para o
   PC, a frase continua verdadeira, e a política passa a separar o PC do app.
3. **"A chave nunca sai do PC"** (ADR-016; ADR-019, decisão 8; AC-040; a tela do app). **Resolvido:**
   decisão 3 do Mauro (decisão G).
4. **O `AudioBuffer` só aceita PCM** (ADR-003; `18-tool-contract.md`). **Resolvido:** decisão C
   **[derivada]**.
5. **O termo aprendido fica fora do prompt no PC** (ADR-017; ADR-020, `DisputaOPrompt`). **Resolvido:**
   na nuvem ele pode ir como dica (decisão E) **[derivada]**. No PC nada muda.
6. **Segurar para falar** (ADR-005). **Resolvido:** decisão 1 do Mauro. No celular é tocar para começar
   e tocar para parar. A ADR-005 continua valendo no PC.
7. **As preferências não sincronizam** (ADR-019, decisão 8). **Resolvido:** as preferências do ditado no
   celular são novas. Moram só na nuvem e nunca descem aos PCs. As do PC continuam no PC.
8. **"O celular não guarda ditado"** (`08-ui-ux-spec.md:657-659`). **Continua valendo:** o áudio fica só
   em memória até chegar ao servidor ou ser descartado.

## Decisão

### As decisões do Mauro (10-out-2026)

| # | Pergunta | Resposta |
|---|---|---|
| 1 | Como entra o ditado no celular? | **Em dois passos.** **Passo 1 (esta ADR):** o servidor ganha a rota que leva a voz a texto, passa pelo Smart e, se a pessoa quiser, pelo Polish. O app de hoje (o PWA) ganha um botão de microfone: toque para começar e toque para parar, com a onda e o tempo, discreto e no estilo do painel. O texto entra no histórico, desce aos PCs pelo sync e tem um "Copiar". **Passo 2 (outra ADR, depois):** o app nativo do Android, com uma bolha flutuante que escreve em qualquer campo (acessibilidade) ou com um teclado, usando **a mesma rota**. O passo 1 deixa a rota pronta para o app nativo. |
| 2 | Quem transcreve? | **A pessoa escolhe o provedor: OpenAI ou Groq.** Ela usa a **chave dela** (BYOK), como no Polish do PC. O Mauro vai usar a OpenAI. |
| 3 | Polish no celular? | **Sim.** Isso exige guardar a chave da IA da pessoa fora do PC, **cifrada no servidor**. Muda a regra de hoje ("a chave nunca sai do PC"), e a mudança entra na política e no dossiê. |
| 4 | Que dicionário o celular usa? | **O Smart com o aprendido da conta.** O dicionário completo continua no PC. |

### Decisões desta ADR

A. **Uma rota nova, `POST /api/v1/dictations`** (decisão 1 do Mauro) **[derivada: o nome, o acesso e
   os códigos]**.
   - **Contrato:** o corpo é o áudio cru e a resposta é o ditado gravado. O id do ditado vem do app.
   - **Acesso:** um valor novo de `Acesso`, só para ditar (`AcessoDitar`, nome a confirmar). No passo 1
     ele aceita só a sessão do navegador; no passo 2, a sessão ou o token de um aparelho Android com o
     escopo de ditar. **Nunca `AcessoQualquer`**, que admitiria o token de um PC. Um teste prova o `403`
     com o token de um PC. O handler recebe o `Principal` e o `Actor`, nunca lê cookie, e não sabe que
     tela o chamou. **[C5b, 11-out-2026: o nome ficou `AcessoDitar` (`internal/server/rotas.go`); a
     rota está em `rotasDoDitado()` (`chaves_ia.go`), e o `403` com o token de um PC é provado em
     `Ditar_SoComASessaoETokenDeUmPCDa403`, da suíte `chavestest`.]**
   - **Os códigos de recusa** vão em `error.code`, em maiúsculas, como os de hoje, numa lista fechada
     documentada no `06`. O app decide pelo código, nunca pela frase.
   - **Por que uma rota nova:** `POST /transcriptions` e `/asr/transcribe` continuam `404`. Um caminho
     novo não reabre o contrato de criar ditado por JSON (D1), e o texto nunca vem do cliente.
   - **Para quem usa:** nada visível.
B. **O servidor só repassa o áudio, e não transcreve** (decisão 2 do Mauro). Ele lê o áudio para a
   memória, manda ao provedor escolhido e descarta. A CPU da VPS é pouca e dividida (ADR-019,
   diagnóstico da F11), e o whisper local já ficou fora pela mesma razão no plano de 10-out-2026.
C. **O áudio vai comprimido, em webm/opus, e o `AudioBuffer` aprende a carregá-lo** **[derivada]**.
   - **Gravação:** o Chrome do Android grava `audio/webm;codecs=opus` pelo `MediaRecorder`. A OpenAI e
     a Groq aceitam webm direto (veja "O motor").
   - **Tamanho:** a 32 kbit/s, 120 s dão cerca de 0,5 MB. Em PCM seriam 3,84 MB. O Chrome pode não
     respeitar o `audioBitsPerSecond`: a C6c e a C7 medem os bytes reais.
   - **No servidor:** nada é decodificado e não entra nenhum decodificador.
   - **O contrato muda:**
     - `asr.AudioBuffer` ganha um campo opcional `Comprimido` (os bytes e o tipo, por exemplo
       `audio/webm`), e cada buffer leva PCM ou o comprimido, nunca os dois;
     - `ASRCapabilities` ganha `Formatos`, a lista do que o provider aceita comprimido;
     - o provider local não declara nenhum formato e recusa o comprimido com um erro próprio. O Agent
       não muda.
   - **Por quê:** a alternativa era uma segunda interface só para a nuvem, e ela faria dois contratos de
     ASR no lugar de um (ADR-003).
   - **Para quem usa:** nada visível; o envio é menor.
D. **Os provedores de transcrição: OpenAI e Groq, por trás do `ASRProvider`** (decisão 2 do Mauro).
   - **Um pacote só** (`internal/asr/nuvem`, nome a confirmar) **[derivada]**, porque as duas APIs têm
     a mesma forma (`/audio/transcriptions`, multipart).
   - **Seguem a regra do `internal/ia`:** só HTTP/1.1, sem seguir redirecionamento, sem repassar o corpo
     de erro, e cada falha vira um motivo de uma lista fechada.
   - **O corpo do multipart tem tamanho conhecido:** o cabeçalho das partes, os bytes recebidos e o
     fecho, juntos por um `io.MultiReader`, com o `Content-Length` calculado. Sem `chunked` (não está
     provado que os dois provedores aceitam) e sem segunda cópia do áudio.
   - **A chave é lida a cada chamada** por uma função, como no `ia.Polidor.Chave`.
   - **Sem troca automática de provedor** **[derivada]**. Se o escolhido falha, o ditado falha. Mandar a
     voz a um segundo operador sem o aceite da pessoa não é permitido.
   - **Para quem usa:** se o provedor falha, aparece o erro, com "Tentar de novo".
E. **A dica do aprendido** **[derivada]**. A dica é o `prompt` do provedor. Ela leva os termos distintos
   do aprendido da conta, sem os pares presos a um domínio, no mesmo orçamento de 140 runas do PC
   (`dictionary.Vocabulario`). Tem um interruptor nas preferências do celular e **começa desligada até
   a C8 medir** (escolha menor 8).
   - **No PC é diferente:** o termo aprendido é `AliasOnly` e fica fora do prompt (ADR-017; ADR-020,
     decisão 3).
   - **Por que a nuvem pode divergir:** o celular não tem outra fonte de dica (o "adicionado" fica no PC,
     D1 da F8). No PC, a dica foi a maior melhora medida: a Entity Accuracy foi de 61,2% a 79,6% com 36
     termos (`internal/asr/types.go:84-96`). **Esse ganho foi medido no initial prompt do whisper.cpp
     local, e não nos modelos `gpt-*-transcribe` nem no whisper da Groq.**
   - **O que vai junto:** os termos do aprendido, a cada ditado, qualquer que seja o conteúdo. Eles
     podem ser nomes de clientes ou de terceiros. A folha do aceite e o dossiê (§7, item 4) dizem isso.
   - **O risco:** a dica pode induzir o termo, e a própria OpenAI avisa disso. A medição está na C8.
   - **Para quem usa:** um interruptor "Mandar os termos aprendidos como dica"; ligado, os termos vão ao
     provedor junto com a voz.
F. **O Smart da conta, idêntico ao do PC para os mesmos pares** (decisão 4 do Mauro).
   - **O pacote:** um pacote puro **[derivada: o nome]** monta o Smart a partir do `ListLearned`, pelo
     mesmo caminho do PC: `Semente{}.AplicarPares` com as **mesmas** `ConferenciasDoLote`, depois
     `Popular` e `NewSmartResolver`.
   - **O que sai do Agent** **[derivada]**: a palavra comum e o `Agiria` saem de
     `internal/agent/aprendiz` e de `internal/learning` para um pacote sem `storage` nem `sync`, e o
     Agent passa a importá-lo de lá. O comportamento do Agent não muda (a paridade da C2). **[C2,
     10-out-2026: o pacote é `internal/smartconta`. A palavra comum (`Arriscada`) ficou em
     `internal/learning`, e saiu só a aresta para o `storage` (veja "Onde estamos"). O `Agiria` do Smart
     da conta usa o "acompanhante", um Smart que só acrescenta, e não o `ValidarPar` literal, por
     velocidade; as respostas são as mesmas (há um teste de equivalência), e o Agent continua no
     `ValidarPar`.]**
   - **Como roda no ditado:** `repeticao.Colapsar`, depois `Resolve` com `Domain` nulo, sem contexto e
     com `CloudPolicy` desligada.
   - **O par preso a um domínio não age no celular, de propósito (fail-closed)** **[derivada]**. O PWA
     não sabe em que programa o texto vai entrar.
   - **Se o Smart falha:** segue o texto do ASR, como no PC.
   - **Para quem usa:** uma troca que o PC aprendeu só para um programa (por exemplo, só no Excel) não
     corrige no celular; as trocas sem programa corrigem igual ao PC.
G. **A chave da pessoa fica cifrada no servidor, e o app só vê o fim dela** (decisão 3 do Mauro; os
   meios são **[derivada]**).
   - **A cifra:** AES-256-GCM, da biblioteca padrão, com uma subchave derivada do arquivo por HKDF
     (`crypto/hkdf`) com um rótulo fixo (`aurora-whisper/ai-keys/v1`). O arquivo nunca é usado cru.
     O nonce é aleatório a cada gravação.
   - **O id da chave de cifra:** cada linha guarda um id curto da chave que a cifrou (os 8 primeiros
     bytes de um HMAC dela com outro rótulo), e não um número de versão. O AAD prende o texto cifrado ao
     usuário, ao provedor e a esse id.
   - **A chave de cifra:** é do servidor, vem de um arquivo `*_FILE` como os outros segredos (64
     caracteres em hexadecimal, como eles), e nunca entra no banco nem no backup.
   - **Trocar a chave de cifra** leva toda linha com o id antigo a `SEM_CHAVE`: a pessoa cola a chave de
     novo. Manter duas chaves de cifra ao mesmo tempo fica fora.
   - **O que o app recebe:** só `configurada` e o `final` (as regras de `chaves.Final`). A chave nunca
     é devolvida inteira, nem a quem a colou **[derivada]**.
   - **Uso:** ela é decifrada a cada ditado, só durante a chamada ao provedor.
   - **Validação:** **depois do aceite** (decisão H), a chave passa pelas regras de `chaves.Validar` e é
     testada no provedor, pedindo a lista de modelos, como o painel do PC faz **[derivada]**.
   - **A regra muda assim:** "a chave **do PC** nunca sai do PC; para ditar no celular, a pessoa cola
     uma chave no app, e o servidor a guarda cifrada". A recomendação na tela é usar uma chave só para o
     celular, com limite de gasto no provedor.
   - **Para quem usa:** cola a chave uma vez e vê "Chave terminada em WXYZ". Ao "Trocar" ou "Remover",
     a tela pede para revogar a antiga no provedor, porque uma cópia cifrada dela fica nos backups.
H. **O aceite é de quem dita, e não só do Mauro como controlador** **[derivada: o desenho inteiro]**.
   - **Quando aparece:** no primeiro toque no microfone, **antes de pedir a chave**. Uma folha só, em
     destaque, específica e separada das outras finalidades. Ela diz o que vai (a voz de cada ditado;
     com a dica, os termos do aprendido; com o Polish, o texto), para quem (o provedor escolhido e o do
     Polish), para onde (o que a documentação oficial de cada um diz, veja "O motor") e por quanto tempo
     fica lá.
   - **Os botões:** "Aceito" e "Agora não". **Sem o aceite, o microfone nem abre** (`getUserMedia` só
     depois dele), a chave não é pedida, e o servidor recusa o ditado (`403`, `SEM_ACEITE`).
   - **Onde fica:** o aceite é gravado **no servidor, por conta**, com a data, a versão do aviso e os
     provedores aceitos. Assim vale para qualquer aparelho dela e para o app nativo do passo 2 (o
     celular não pode guardar nada).
   - **Quem confere:**
     - o `PUT /ai-keys` exige um aceite vigente que inclua aquele provedor;
     - o `PUT /dictation/settings` recusa um provedor de transcrição ou de Polish fora do aceite
       vigente (`SEM_ACEITE`), e o app abre a folha;
     - a rota de ditar confere os dois provedores, o da transcrição e, com o Polish ligado, o do Polish,
       na hora de chamar.
   - **Quando pede de novo:** trocar de provedor, ligar o Polish com outro provedor ou mudar o texto do
     aviso. Mudar só o modelo não pede (escolha menor 21). **[C3b, 11-out-2026: o aceite cobre o
     CONJUNTO dos dois provedores aceitos (o da transcrição e o do Polish). Ligar o Polish com um
     provedor que já está no conjunto (a OpenAI aceita para a transcrição, por exemplo) não pede aceite
     novo; pede um provedor fora dele, trocar de provedor ou mudar a versão do aviso. Com o Polish
     desligado, o provedor dele pode ser qualquer um da lista: a conferência só vale quando liga.]**
   - **[C3b, 11-out-2026] A versão do aviso** é um inteiro: `cloudstore.VersaoDoAviso` no servidor e
     `VERSAO_DO_AVISO` em `web/src/nuvem/avisoDoDitado.ts`, presos por um teste
     (`TestVersaoDoAviso_AppEServidorConferem`, que lê o arquivo do app). **O `POST` do aceite recusa uma
     versão diferente da atual** (`409`, `AVISO_DESATUALIZADO`), para cima ou para baixo: um app com o
     texto velho na memória não registra que a pessoa aceitou o texto novo. Mudar o texto da folha é
     subir os dois números juntos.
   - **Revogar:** "Desligar o ditado no celular", nas Configurações. Revoga o aceite e apaga as chaves
     (escolha menor 12).
   - **Para quem usa:** uma folha no primeiro toque; trocar de provedor mostra a folha de novo.
I. **Os prazos cabem no proxy e no servidor, e um envio lento não estoura** **[derivada]** (veja
   "Prazos").
   - **Antes do provedor:** o corpo é lido sob um prazo **menor** que o do proxy.
   - **Depois do corpo:** o handler estende o prazo de escrita com `http.NewResponseController`.
   - **Desligado do celular:** a chamada ao provedor, o Smart, o Polish e a gravação rodam num contexto
     solto da conexão, com prazo próprio (`context.WithoutCancel`). Uma transcrição já paga não se perde
     porque o celular caiu.
   - **O trabalho solto confere de novo antes do Polish e antes de gravar:** o aceite vigente e o
     `sync_epoch` da conta, lido no começo. Se o aceite foi revogado ou a conta apagou os dados no meio,
     ele descarta sem chamar o Polish e sem gravar. A conferência do epoch fica dentro do próprio
     `Store.Create`, na transação que trava o contador (C1), e não tem janela.
   - **No desligamento do servidor**, o `serve` espera os ditados em andamento (escolha menor 22).
   - **Para quem usa:** se o celular cai depois do envio, o ditado aparece no histórico quando ela
     volta; se ela desligou o ditado ou apagou os dados no meio, ele não aparece.
J. **Idempotência, para não cobrar duas vezes** **[derivada]** (veja "Idempotência"). O app gera o id
   do ditado (UUID v7) e o manda num cabeçalho. Antes de ler o corpo, o servidor procura o id da conta:
   se o ditado já existe, devolve o que está gravado, sem chamar o provedor. Os casos em que o reenvio
   ainda paga de novo estão em "Riscos".
   - **Para quem usa:** "Tentar de novo" não cobra de novo, salvo os casos de "Riscos".
K. **A memória da API fica protegida** **[derivada]** (veja "Limites"):
   - a vaga e os limites por conta são conferidos **antes** de ler o corpo;
   - o teto do corpo é recusado pelo `Content-Length` antes de qualquer leitura;
   - o multipart para o provedor usa os bytes recebidos, sem segunda cópia (decisão D).
   - **Para quem usa:** com 3 ditados de todas as contas ao mesmo tempo, o quarto recebe "Espere um
     pouco.".
L. **O áudio nunca vai a disco, swap ou log no contêiner da API** (a regra do `CLAUDE.md`; os meios são
   **[derivada]**; veja "Memória, disco e log"):
   - não há multipart no servidor (o corpo é cru) nem arquivo temporário;
   - o serviço da API ganha `memswap_limit` igual ao `mem_limit` e `ulimits: core: 0`;
   - **o `memswap_limit` só vale se o kernel da VPS contar a swap no cgroup**, e a C7 confere isso antes
     do primeiro áudio;
   - um teste de canário cobre log, erro, resposta e `/tmp`;
   - a promessa honesta é "descartado ao fim", e não "apagado da memória". O proxy, que não é nosso,
     fica em "Riscos".
   - **Para quem usa:** nada visível.
M. **Sem fala, sem envio** **[derivada]** (veja "Detecção de fala"). O app mede a fala enquanto grava.
   Sem fala bastante, nem envia, e só isso evita a cobrança. O servidor confere o que o app mediu e
   filtra os trechos que o provedor marca como silêncio, mas esse filtro roda depois de o provedor
   cobrar: **ele só evita o ditado vazio, não a cobrança.**
   - **Para quem usa:** com menos de 0,3 s de fala, o app diz "Não ouvi nada." e não envia.
N. **O Polish no celular é o mesmo motor do PC** (decisão 3 do Mauro: `internal/ia`, OpenAI ou
   Anthropic, com a chave da pessoa).
   - **Opt-in** **[derivada]:** fica desligado até a pessoa ligar nas preferências do celular.
   - **O adaptador** **[derivada; muda o Agent]:** o do PC (`cmd/agent/polish.go`, o `clienteDoDitado`
     e a política de esforço) vai para um pacote comum, e o PC e o servidor passam a usar o mesmo. É a
     C5a, com paridade no Agent e teste no PC.
   - **O prazo** **[derivada]:** o do PC, `prazoPolish` (3,5 s a 8 s conforme o texto), que vai junto
     para o pacote comum. Não os 10 s do `ia.TimeoutPadrao`, que são do teste da chave. **[C5a,
     10-out-2026: no pacote comum, `internal/polish`, ele é `polish.Prazo(texto)`, e o piso é
     `polish.PisoPrazo`. O acréscimo por caractere é o do núcleo (`dictation.AcrescimoPeloTexto`), e não
     repetido: a folga de 1,5 s até o watchdog do núcleo continua garantida por construção.]**
   - **Regras:** as mesmas do PC, que são 4 palavras ou mais, as guardas sobre o Smart da conta e a
     queda para o texto do Smart quando falha ou estoura o prazo.
   - **Para quem usa:** o Polish do celular começa desligado; quando o provedor demora, vale o texto do
     Smart, como no PC.
O. **A rota, as chaves e as preferências só existem se o servidor tiver a chave de cifra**
   **[derivada]**.
   - **No binário:** sem o arquivo, essas rotas não são montadas e caem no `404`, como o login sem o
     client do Google. Um compose antigo com a imagem nova continua igual, sem o ditado.
   - **No compose de referência:** o segredo novo é declarado com `file:`, como os outros, a partir da
     C5c. Sem o arquivo, o `docker compose up` falha. Por isso a C3a entrega um passo próprio que cria
     só esse arquivo numa VPS que já está no ar, e o roteiro de deploy o roda antes (escolha menor 25).
   - **Para quem usa:** nada. Para quem hospeda: um arquivo a mais, criado por um comando.
P. **A chegada ao PC fica mais rápida** **[derivada; escopo novo, com OK próprio]** (subfatia S). Com o
   painel do PC aberto e na frente, o Agent busca a nuvem mais vezes, e o painel relê a lista quando um
   pull traz mudança. O intervalo é escolha do Mauro (escolha menor 17), porque os 60 s foram decisão
   dele.
   - **Para quem usa:** com o painel do PC aberto, o ditado do celular e as edições aparecem em
     segundos.

## Como funciona

### O caminho de um ditado

1. **O primeiro uso.** No primeiro toque no microfone, o app mostra a folha do aceite (decisão H).
   Sem o aceite, para aí. Com ele, pede o provedor e a chave, se ainda não houver; o servidor confere o
   aceite, testa a chave no provedor e a guarda cifrada. **[C6b, 11-out-2026: a chave não é pedida
   dentro da folha. Depois do "Aceito", se falta a chave, a pílula diz "Falta a chave da OpenAI. Cole a
   chave nas Configurações." e o app abre as Configurações na seção do ditado. Assim há um único lugar
   que cola, troca e remove a chave, e a disciplina de nunca ecoá-la fica num componente só. O
   microfone também não começa sozinho depois da folha: a pílula diz "Aceito. Toque no microfone para
   ditar." (a pessoa acabou de ler um aviso longo, e isso evita depender do gesto do toque para o
   `AudioContext`). E o primeiro toque sem rede não abre o microfone ("Sem conexão."): sem saber do
   aceite, não abre. Depois de a porta passar uma vez na página, o ditado sem rede segue gravando, e o
   áudio espera o "Tentar de novo".]**
2. **A gravação.** O app pede o microfone e começa a gravar. Pede também a tela acesa (Wake Lock) e
   mostra a onda e o tempo.
3. **O fim.** Num toque, ou no teto de duração, o app para. O microfone desliga na hora, como na
   ADR-015.
4. **A conferência no app.** Sem fala bastante, o app descarta e diz "Não ouvi nada.". Nada é enviado.
5. **O envio.** O app envia o áudio cru, com o id, a duração e a fala medida, e mostra "Transcrevendo".
6. **No servidor, antes de ler o corpo:** confere a credencial (`AcessoDitar`), o aceite dos dois
   provedores, as preferências e a chave, o id e o que já existe com ele, os limites por conta e a
   vaga, o tamanho e o tipo. Lê o `sync_epoch` da conta.
7. **No servidor, o resto:** lê o corpo, confere o formato pelos primeiros bytes e manda ao provedor,
   com a dica se estiver ligada. Depois aplica o `Colapsar` e o Smart da conta. Com o Polish ligado,
   confere de novo o aceite e chama o Polish.
8. **A gravação do ditado.** Confere de novo o aceite e grava pelo ponto único (`Store.Create`), com o
   evento de sync. O `Create` recusa se o epoch mudou. Descarta o áudio e responde com o ditado.
9. **No app:** o ditado entra no topo da lista (`inserirEmOrdem`), em destaque, com o "Copiar".
10. **No PC:** o ditado chega no pull seguinte e fica como "recebido".

### A rota

`POST /api/v1/dictations`.

| Parte | O quê |
|---|---|
| Acesso | `AcessoDitar`: no passo 1, só a sessão do navegador |
| Corpo | o áudio cru (`Content-Type: audio/webm`), até 1 MiB. Nunca multipart nem JSON |
| `Ditado-Id` | UUID v7 gerado no app (o `05` pede v7). Com o usuário, é a chave da idempotência |
| `Ditado-Duracao-Ms` | a duração medida no app. Fica no ditado (`duration_ms`) quando o provedor não devolve a dele, e passa por um teto. **[C5b: o teto é de 125.000 ms, os 120 s do PC com 5 s de folga para o cabeçalho; acima disso é `413`. A duração vem sempre deste cabeçalho, nunca do provedor]** |
| `Ditado-Fala-Ms` | quanto do áudio o app achou que era fala (decisão M). O servidor não tem como conferir esse número |
| Resposta | `201` com o ditado (o mesmo `itemDTO` do `GET /transcriptions/{id}`). Quando o Polish caiu para o Smart, traz um campo `aviso` com o motivo (um `ia.Motivo`, nunca texto). **[C5b, 11-out-2026: o motivo é um `resolver.MotivoFallback` (`prazo`, `sem_chave`, `chave_recusada`, `modelo_inexistente`, `dicionario` e os demais da lista fechada do Polish), e não um `ia.Motivo`. O reenvio de um id já gravado é `200` sem `aviso`; o que esperou o mesmo id em andamento é `200` com o `aviso` do primeiro; o que só grava o desfecho pago e não gravado é `201`]** |

O formato do ditado gravado:
- `source_platform` é `"web"` para o app e fica `"android"` para o app nativo do passo 2 (escolha menor
  13);
- `device_id` fica nulo;
- `mode` diz o que valeu de fato: `polish` se o Polish foi aplicado, senão `smart`;
- `asr_provider` e `asr_model` dizem o que foi escolhido;
- `processing_ms` é medido no servidor, e `created_at` vem do relógio do servidor.

Os códigos de recusa vão em `error.code`, em maiúsculas. Os que já existem continuam (`UNAUTHORIZED`,
`FORBIDDEN` para o token de um PC, `PAYLOAD_TOO_LARGE`). Os novos formam uma lista fechada no `06`, e
o status de cada um é fixado na C5b:

| Código | Quando | Chamou o provedor? |
|---|---|---|
| `SEM_ACEITE` (403) | sem aceite vigente para o provedor da transcrição ou, com o Polish ligado, para o do Polish; ou o aceite foi revogado (ou a conta apagou os dados) durante o ditado | antes: não. Durante: sim, e nada foi gravado |
| `SEM_CHAVE` | as preferências pedem um provedor sem chave guardada, ou a chave não decifra mais (a chave de cifra trocou, ou o backup foi restaurado sem ela) | não |
| `UM_POR_VEZ`, `LIMITE` (429, `Retry-After`) | os limites por conta | não |
| `OCUPADO` (503, `Retry-After`) | sem vaga no total | não |
| `PAYLOAD_TOO_LARGE` (413) | corpo ou duração acima do teto | não |
| `FORMATO` (415) | não é webm pelos primeiros bytes | não |
| `SEM_FALA` (422) | fala medida abaixo do mínimo, ou o provedor não devolveu texto | só no segundo caso |
| `PROVEDOR_RECUSOU_CHAVE`, `PROVEDOR_LIMITE`, `PROVEDOR_FORA` | a falha do provedor, já traduzida | sim, e não houve ditado |
| `APAGADO` (410) | o id tem lápide | não |

**[C3a e C3b, 11-out-2026]** Os status já fixados, nas rotas das chaves, do aceite e das preferências.
A C5b reaproveitou a tradução da falha do provedor (`server.FalhaDoProvedor`) e fixou os que faltavam
(o último item desta lista):
- `SEM_ACEITE` 403; `SEM_CHAVE` 409; `LIMITE` 429, com `Retry-After`.
- `PROVEDOR_RECUSOU_CHAVE` 422 (o provedor recusou a chave com `401`, ou negou a lista de modelos com
  `403`); `PROVEDOR_LIMITE` 502 (limite de uso ou falta de crédito, o `429` do provedor);
  `PROVEDOR_FORA` 502 (qualquer outra falha: `5xx`, prazo, rede, redirecionamento, resposta inesperada).
- **`AVISO_DESATUALIZADO` 409, código novo da C3b:** o aceite veio com uma versão do aviso que não é a
  atual, ou seja, o app tem o texto velho; o app recarrega e mostra o texto novo, e nada é gravado.
- O `{provedor}` fora da lista (`openai`, `groq`, `anthropic`) é `404` `NOT_FOUND`, "provedor
  desconhecido", sem eco do valor.
- **[C5b, 11-out-2026] Os status que a rota de ditar fixou:** `UM_POR_VEZ` 429; `OCUPADO` 503;
  `FORMATO` 415; `SEM_FALA` 422; `APAGADO` 410; `PAYLOAD_TOO_LARGE` 413. Os dos outros códigos
  (`SEM_ACEITE` 403, `SEM_CHAVE` 409, `LIMITE` 429, `PROVEDOR_RECUSOU_CHAVE` 422, `PROVEDOR_LIMITE` e
  `PROVEDOR_FORA` 502) ficaram iguais aos da C3a, e `FalhaDoProvedor` passou a traduzir também o
  `*nuvem.Erro` (`sem_chave` vira `SEM_CHAVE` `409`, e `arquivo_grande` vira `PROVEDOR_FORA`). Sem
  `Ditado-Id` válido, cabeçalho de medida fora da forma, ou corpo que não chega inteiro: `400`
  `BAD_REQUEST`. A gravação que falha por outro motivo (o banco fora) é `500` `INTERNAL`.

### Prazos

| Trecho | Prazo | Quem corta |
|---|---|---|
| Cabeçalhos | 10 s | `readHeaderTimeout` (`cmd/server/serve.go:38`), que já existe |
| Corpo (até 1 MiB) | 30 s desde o começo do pedido | o `readTimeout` de hoje (`serve.go:39`), repetido no handler com `SetReadDeadline` |
| O proxy da VPS | 60 s para ler o pedido inteiro, corpo incluído | no Traefik v3, o `readTimeout` padrão do entrypoint, pela documentação oficial. O nosso de 30 s corta antes, então o proxy nunca corta um corpo que o servidor aceitaria. O `writeTimeout` padrão do entrypoint é 0, sem prazo, pela mesma documentação. **As duas coisas se conferem só lendo a configuração real antes do primeiro áudio (C7).** |
| Transcrição | 20 s | o contexto do provedor. O provedor pode cobrar o que já processou (Riscos) |
| Smart | menos de 25 ms (medido no PC) | — |
| Polish | o `prazoPolish` do PC: 3,5 s a 8 s, conforme o texto | o contexto do Polish. Se estoura, vale o texto do Smart |
| Gravar | 5 s | o contexto da gravação |
| Escrever a resposta | 45 s depois do fim do corpo | `SetWriteDeadline` no handler. O `writeTimeout` de 60 s (`serve.go:40`) conta do fim dos cabeçalhos e não cobriria 30 + 20 + 8 + 5 |
| Quem espera o mesmo id | até o fim do primeiro, mais a folga | o handler que espera estende o próprio prazo de escrita com `SetWriteDeadline`: o `writeTimeout` dele conta do fim dos cabeçalhos dele |
| O desligamento | o `Shutdown` de hoje (15 s) e, depois, até 70 s pelos ditados em andamento | o `serve`, com o `stop_grace_period` da API em 90 s (escolha menor 22) |
| O celular | 90 s | um `AbortController`. Depois disso, o app diz "O envio demorou demais." e oferece "Tentar de novo" com o mesmo id. **[C6a, 10-out-2026: ele não reenvia sozinho, para não pagar o provedor de novo sem a pessoa ver.]** |

**As contas.**
- **Subida:** 1 MiB em 30 s pede uns 280 kbit/s de subida. Um ditado típico de 0,5 MB pede uns 140
  kbit/s.
- **O pior caso:** cerca de 65 s entre o toque e o texto (30 + 20 + 8 + 5, mais o app).
- **O caso típico:** a estimativa é de 2 a 4 s, a medir na C7.
- **[C5b, 11-out-2026] Os prazos que esta ADR não fixava e a rota escolheu:**
  - o corpo conta **de quando o pedido pega a vaga** (`SetReadDeadline` de agora mais 30 s, logo antes
    da leitura), e não do começo do pedido, porque o handler não sabe quando o pedido começou; o pior
    caso é 30 s mais as leituras do banco antes (milissegundos), abaixo dos 60 s do proxy;
  - o Smart: 5 s para ler o aprendido da conta e 5 s para montar e resolver; a lista de modelos do
    Polish, 3 s (só quando o modelo ainda não é conhecido);
  - o prazo do Polish conta do começo da chamada ao Polish, e usa o tamanho do texto do Smart;
  - o trabalho solto inteiro tem o teto de **44 s depois do corpo** (os 45 s da resposta menos 1 s de
    folga para escrevê-la); o que passar dele depois de o provedor cobrar vira o desfecho pago e não
    gravado;
  - quem espera o mesmo id estende o próprio prazo de escrita em 80 s;
  - o `Retry-After` do `UM_POR_VEZ` e do `OCUPADO` é de 5 s, e o do `LIMITE` é o tempo até a próxima
    ficha.

**O que um prazo estourado faz.**
- **Antes do provedor:** o pedido falha e nada foi cobrado.
- **No provedor (os 20 s):** o pedido falha, mas o provedor pode já ter cobrado. O reenvio paga de novo
  (Riscos).
- **Depois do provedor:** o ditado é gravado mesmo assim (decisão I), e o reenvio o recebe de graça. Se
  a gravação falha, veja "Idempotência".

### Idempotência

Antes de ler o corpo, o servidor procura o id **da conta**, por `(user_id, id)`:

| O que o servidor acha | Resposta | Chama o provedor? |
|---|---|---|
| o ditado já gravado (também na lixeira) | `200` com o ditado | não |
| uma lápide (apagado de vez) | `410`, `APAGADO` | não |
| o mesmo id em andamento | espera o primeiro terminar, estendendo o próprio prazo de escrita, e devolve a mesma resposta | não |
| o desfecho pago e não gravado (a gravação falhou) nos últimos 10 min | tenta só gravar de novo | não |
| o mesmo id deu `SEM_FALA` nos últimos 10 min | `422` | não |
| nada | processa | sim |

Como funciona:
- **A lápide:** o `Store.Get` não a mostra. A C1 acrescenta ao `Store` uma consulta só de leitura
  (`Store.Situacao`, nome a confirmar) que diz "ditado", "lápide" ou "nada" para `(user_id, id)`. Uma
  lápide vencida já não existe (o `Purge` fora da janela não a deixa), e o id volta a "nada"; o app
  nunca reusa um id, a não ser para reenviar.
- **O corpo:** nos cinco primeiros casos ele não é lido.
- **O `Store.Create`:** confere só o id, o usuário e o epoch, nunca o texto. Numa corrida, o segundo
  recebe o que o primeiro gravou.
- **O que fica em memória**, sempre indexado por `(user_id, id)`, nunca só pelo id:
  - "em andamento" e "sem fala" guardam só o desfecho, nunca o áudio nem o texto;
  - **o desfecho pago e não gravado** guarda o que seria gravado (o texto do ASR e o final, o modo, o
    provedor e o modelo), porque é isso que o reenvio grava. Some em 10 min (escolha menor 23), no
    reinício ou quando o epoch da conta muda, e nunca vai a log;
  - o mesmo id em duas contas são dois ditados, e uma conta nunca espera nem recebe a resposta da
    outra.
- **O que não cobre (Riscos):** o prazo de 20 s que corta uma transcrição já cobrada; um reinício além
  da espera do desligamento; duas instâncias da API (esse estado teria de ir para o banco; hoje há
  uma).
- **Os testes:** dois envios com o mesmo id geram **uma** chamada ao provedor; o mesmo id em duas
  contas gera duas; a gravação que falha uma vez é refeita no reenvio sem chamar o provedor; uma
  lápide devolve `410` sem chamar o provedor (contagem de chamadas no `httptest`).

### Memória, disco e log

- **Antes do corpo:** a vaga e os limites. Com 3 vagas no total e o corpo de até 1 MiB, o pico de
  áudio na API fica em poucos MiB, contra o `GOMEMLIMIT` de 200 MiB. O teste manda 10 envios ao mesmo
  tempo e mede o pico.
- **O corpo:**
  - é lido com `http.MaxBytesReader`, que também protege o envio sem `Content-Length`;
  - o tipo vem dos primeiros bytes (o cabeçalho EBML do webm e o DocType `webm`; o Matroska é
    recusado), nunca só do `Content-Type`. **[C5b, 11-out-2026: a rota exige também o `Content-Type`
    `audio/webm` (os parâmetros valem) e responde `415` antes de ler o corpo; o tipo de verdade segue
    vindo dos bytes.]**
  - os bytes ficam numa variável do handler, saem de escopo no fim, com sucesso ou erro, e nunca
    entram em estrutura de vida longa (cache, pool, erro, pânico).
- **Sem disco, no contêiner da API:**
  - o servidor não usa `ParseMultipartForm` (o corpo é cru) nem arquivo temporário;
  - o contêiner é `read_only`;
  - **a swap:** o serviço da API ganha `memswap_limit: 256m`, igual ao `mem_limit`, para que nem o heap
    nem o tmpfs vão para a swap, se a VPS tiver uma (`deploy/README.md`, a restauração, já registra
    esse risco para o dump). **Isso depende do kernel:** num cgroup v1 sem `swapaccount=1`, o Docker
    descarta o limite e só escreve um aviso ("No swap limit support" no `docker info`). A C7 confere,
    antes do primeiro áudio, o `docker info` sem esse aviso e o `memory.swap.max` do cgroup do
    contêiner em `0`, lido no host (a imagem não tem shell). **Se não der `0`, para e pergunta**; a
    alternativa é uma VPS sem swap, vista no `swapon --show`;
  - **o core dump:** o Go não grava core no padrão (só com `GOTRACEBACK=crash`, que o compose não
    põe). O `ulimits: core: 0` é a segunda trava, para que nenhum dump leve o heap (áudio e chave
    decifrada) ao disco;
  - as duas mudanças são só no nosso contêiner e ganham casos no `deploy/compose_test.go`; **[C5c,
    11-out-2026: estão no `deploy/compose.yml` (`memswap_limit: 256m`, `ulimits: core: 0`), e o
    `compose_test` as prende. A leitura do kernel da VPS é a do passo 10.4 do `docs/deploy-f11.md`,
    antes do primeiro áudio.]**
  - **o proxy:** um middleware de buffering no entrypoint levaria ao disco um corpo acima de 1 MiB. O
    nosso router não tem middleware, e o corpo fica perto de 0,5 MB. **Conferir só lendo os
    middlewares do entrypoint antes do primeiro áudio (C7).** Sem buffering, o proxy ainda segura
    pedaços do corpo na memória dele, fora do nosso `memswap_limit` (Riscos).
- **Sem log:**
  - o log de acesso fica como é: método, padrão da rota, status, ms e bytes;
  - uma linha `ditado:` traz só contagens e códigos: duração, fala, bytes, provedor, modelo, desfecho,
    desfecho do Polish, termos na dica e ms do provedor;
  - nunca vão para o log o áudio, o texto, a chave, o id nem o corpo de erro do provedor (a OpenAI
    devolve um pedaço da chave no `401`, `internal/ia/ia.go:15-18`);
  - o canário de áudio e de chave cobre log, mensagem de erro, resposta e `/tmp`.
- **No aparelho:**
  - o app não grava o áudio em lugar nenhum: nada em `IndexedDB`, Cache ou `localStorage`, como o
    `bundle.test` já trava;
  - ele guarda os bytes só numa variável, até o envio dar certo ou a pessoa descartar, e solta as
    referências no fim;
  - **o que não se garante:** o `MediaRecorder` entrega um `Blob`, e o Chrome pode paginar Blobs em
    disco acima de um limite de memória. Isso não foi verificado para o tamanho de um ditado e entra em
    "Riscos".

### Detecção de fala

- **No app, enquanto grava:**
  - o mesmo `AnalyserNode` que desenha a onda mede o RMS de cada quadro e soma o tempo acima de um
    limiar;
  - **com menos de 300 ms de fala**, sem contar os últimos 200 ms (o toque para parar), o app não
    envia. É o único ponto que evita a cobrança;
  - o limiar parte do valor do PC (RMS 0,03, `internal/agent/audio/vad.go:32`, calibrado em amostras
    sintéticas) e é **calibrado no próprio Android na C6c**, por uma página que mede e mostra só
    números, sem gravar arquivo (veja C6c);
  - o `echoCancellation`, o `noiseSuppression` e o `autoGainControl` do `getUserMedia` começam no
    padrão do Chrome e são fixados pela medição.
- **No servidor:**
  - um `Ditado-Fala-Ms` abaixo do mínimo dá `SEM_FALA` antes de ler o corpo. É defesa contra defeito
    do app, e não contra ataque: o servidor não tem como conferir esse número, e quem paga é a própria
    pessoa (BYOK);
  - **o que o provedor devolve:** quando o modelo devolve segmentos com `no_speech_prob` (o
    `verbose_json` da Groq e do `whisper-1`), o servidor descarta o segmento com `no_speech_prob`
    acima de 0,6 **e** `avg_logprob` abaixo de -1,0. São os limiares padrão do próprio Whisper, a
    calibrar. **Esse filtro roda depois da cobrança:** ele evita o ditado vazio, e não o custo;
  - texto vazio no fim dá `SEM_FALA`, e nenhum ditado é criado;
  - os modelos `gpt-*-transcribe` podem não devolver esses campos (a conferir na C4). Neles vale só a
    medição do app. **[C4, 10-out-2026: o provider trata os `gpt-*-transcribe` como se não devolvessem
    segmentos: pede `json` a eles, e `verbose_json` aos ids com `whisper`. Nos `gpt-*-transcribe` vale
    só a medição do app. O que o modelo de fato aceita fica sem chamada real até a C7.]**
- **Por que não a VAD do PC no servidor:** ela precisa de PCM, e o servidor não decodifica opus.
  Importá-la levaria o `internal/agent/dictation` para o servidor.

### O motor: os provedores de transcrição

O que segue foi lido nas páginas oficiais pelo levantamento de 10-out-2026, com preços em dólar, sem
câmbio nem IOF. A D0 releu cada página em 10-out-2026 e **corrigiu três pontos que este levantamento
tinha errado ou deixado vagos** (a OpenAI e o Brasil, a Groq e o treino, a retenção zero da Groq); a
tabela abaixo já traz o texto corrigido, e o Anexo B de `docs/lgpd-dossie.md` diz, página por página, o
que cada uma afirma e o que **não** afirma. **O custo é da pessoa, na conta dela no provedor.** O
projeto não paga nada.

| | OpenAI | Groq |
|---|---|---|
| Endereço | `api.openai.com/v1/audio/transcriptions` | `api.groq.com/openai/v1/audio/transcriptions` |
| Modelos (o app lista os que a chave alcança, filtrando `transcribe` e `whisper` no `/models`. **[C4]** O filtro do pacote dos provedores, `nuvem.EhModeloDeTranscricao`, exclui também os ids de diarização, `...diarize`, que pedem outro formato de resposta, e os só de inglês, terminados em `-en` ou `.en`; hoje o `GET /ai-keys/{provedor}/models` devolve a lista inteira e o app filtra por nome) | `gpt-transcribe` (US$ 0,0045/min, o que a OpenAI recomenda hoje; o português não aparece escrito nas páginas lidas); `gpt-4o-mini-transcribe` (US$ 0,003/min); `gpt-4o-transcribe` e `whisper-1` (US$ 0,006/min) | `whisper-large-v3-turbo` (US$ 0,04/h, mínimo de 10 s por pedido); `whisper-large-v3` (US$ 0,111/h) |
| Formatos | mp3, mp4, mpeg, mpga, m4a, wav e **webm**; até 25 MB | flac, mp3, mp4, mpeg, mpga, m4a, ogg, wav e **webm**; até 25 MB no plano gratuito e 100 MB no Developer |
| Dica | `prompt` (o `gpt-transcribe` tem também `keywords`, que a OpenAI diz que são só dicas e podem induzir termos) | `prompt`, até 224 tokens |
| Treino | a API não treina sem opt-in | **as páginas lidas não falam em treino com dados de clientes.** A página de dados não fala; a política de privacidade (em vigor desde 12-nov-2025) não se aplica aos dados de clientes da nuvem e os remete ao contrato de serviço e ao DPA (15-out-2025); o DPA limita o tratamento a prestar o serviço, em nome do cliente, e também não fala em treino. A política e a folha **não afirmam** que a Groq não treina |
| Retenção lá | ambígua: a tabela de endpoints diz retenção de abuso "None" para `/v1/audio/transcriptions`, com ZDR elegível; o texto geral da mesma página fala em logs de abuso por até 30 dias para todo uso da API. **A política assume até 30 dias** | não guarda por padrão; pode registrar entrada e saída por até 30 dias, só para investigar falha ou abuso, a menos que o "não guardar" (ZDR) esteja ligado em Data Controls. **A página diz que essa opção está ao alcance de todos os clientes** (na OpenAI, a retenção zero depende de aprovação dela). **O servidor não tem como conferir se está ligado** |
| **Onde processa** | a página diz que um projeto pode fixar a residência dos dados nos **EUA** ou na **Europa**, e que a transcrição de áudio processa na região escolhida. A Austrália, o Canadá, o Japão, a Índia, Singapura, a Coreia do Sul e o Reino Unido são só de armazenamento; a Europa exige retenção zero ou monitoramento modificado; a residência depende de elegibilidade (a página manda falar com o comercial). **O Brasil não aparece na página, nem como armazenamento** (a versão anterior desta tabela dizia que aparecia só como armazenamento, e a releitura da D0 não o achou). **Sem a residência fixada, a página lida não diz o país.** | a página diz que o dado **guardado** fica em buckets do GCP **nos EUA**. **Não diz onde a transcrição roda.** A Groq anunciou 13 data centers na América do Norte, na Europa, no Oriente Médio e na Ásia-Pacífico (22-jun-2026). O DPA diz que ela pode transferir e tratar dados nos EUA e em outros países onde ela ou seus subprocessadores operam; a lista de subprocessadores não carregou como texto e não foi lida |
| Limites | os da conta da pessoa | plano Developer: 20 pedidos por minuto e 7.200 s de áudio por hora, para a organização inteira |

**O que a política diz sobre o país.** A política não afirma "EUA" sem prova. Ela escreve o que a
documentação diz:
- **OpenAI:** "nos EUA ou na Europa, se o projeto da sua chave tiver a residência de dados fixada; sem
  isso, a documentação não diz em que país". Se a conta de quem dita tiver a residência de dados, ela a
  fixa, e o país passa a ser conhecido (a conferir na C7, na conta do Mauro).
- **Groq:** "o que ela guarda fica nos EUA; a documentação não diz onde a transcrição roda; o contrato
  prevê tratamento nos EUA e em outros países".
- **O tratamento que a D0 deu (a confirmar pelo Mauro):** como nenhum dos dois diz processar no Brasil,
  a política e a folha tratam o envio como transferência internacional, e não afirmam "fora do Brasil".
  A hipótese do art. 33 (a proposta é o inciso VIII, o consentimento específico e em destaque que a folha
  cumpre) está como `[A PREENCHER]`, para o responsável.

A D0 pode ainda pedir à Groq onde a transcrição roda, pelo DPA, pela lista de suboperadores ou pelo
suporte.

**O que o provider faz em cada chamada.**
- **Campos:** o multipart leva o arquivo (`ditado.webm`, `audio/webm`), o modelo, o idioma `pt` (no
  `gpt-transcribe`, o campo é `languages`), o `prompt` com a dica (se ligada), `temperature` 0 e o
  `response_format` que o modelo aceita (`verbose_json` quando dá, senão `json`). O que cada modelo
  aceita é conferido na C4 e numa chamada de teste na C7.
- **[C4, 10-out-2026] O que o pacote `internal/asr/nuvem` fixou:**
  - **A tabela por modelo** (`perfilDoModelo`) é mínima e **não foi conferida numa chamada real**, como
    esta ADR já previa para a C7: o campo `languages` (um só, `pt`) vai só aos ids que começam com
    `gpt-transcribe`; o `response_format` é `verbose_json` para os ids com `whisper` e `json` para os
    outros; `temperature=0` e `prompt` vão a todos. Se o `gpt-transcribe` recusar `languages=pt` ou a
    temperatura, o ajuste é só nessa tabela.
  - **Só áudio comprimido:** o provider da nuvem recusa PCM (`asr.ErrPCMNaoSuportado`). Embrulhar o PCM
    em WAV faria uma segunda cópia do áudio, contra a decisão D. Por isso ele ainda não é
    intercambiável com o local num `Engine` que receba PCM do PC; no servidor o buffer sempre vem
    comprimido, e isso não importa.
  - **Erros:** o pacote tem um `Erro` e um `Motivo` próprios, e duplica o transporte só HTTP/1.1 do
    `internal/ia` (cerca de 15 linhas), porque o `internal/ia` importa o `resolver`, o `dictionary` e o
    `agent/dictation`, e o pacote do ASR não deve arrastar isso. Os motivos iguais aos do `ia` têm o
    mesmo texto, e há dois a mais: `sem_chave` e `arquivo_grande` (o `413` do provedor e o teto de
    25 MB). O erro não embrulha o erro de origem (a única causa embrulhada é `context.DeadlineExceeded`);
    o cancelamento do chamador volta como `context.Canceled`, e não como falha do provedor; o `429` do
    provedor não repassa `Retry-After`.
  - **Nenhum arquivo, nenhum log:** uma guarda de fonte (`TestFonteNaoTocaDiscoNemLog`) proíbe no
    código do pacote os imports `os`, `io/ioutil`, `path/filepath`, `log`, `log/slog` e `runtime/debug`.
  - **Falta:** o provider não expõe a duração do áudio nem o idioma detectado (o `ASRResult` não tem
    campo para isso); a duração do ditado vem do cabeçalho `Ditado-Duracao-Ms`. Os limiares do silêncio
    (0,6 e -1,0) são constantes (`LimiteSemFala`, `LimiteLogprob`), a calibrar na C7 e na C8.
- **O envio:** com `Content-Length` calculado, sem `chunked` (decisão D). A chamada de teste da C7 passa
  pelo mesmo caminho de código.
- **A resposta:** é lida com teto (`io.LimitReader`), e nenhum corpo de erro passa adiante.
- **O custo de um ditado de 30 s:**
  - OpenAI: US$ 0,0015 no `gpt-4o-mini-transcribe` e US$ 0,00225 no `gpt-transcribe`;
  - Groq: US$ 0,0003.
- **Por mês, com 20 min por dia:**
  - OpenAI: US$ 1,80 no `gpt-4o-mini-transcribe` e US$ 2,70 no `gpt-transcribe`;
  - Groq: US$ 0,40, ou até US$ 0,80 com o mínimo de 10 s.
- **O Polish:** é cobrado à parte, pelos tokens.

### O Smart da conta e a dica

- **Montagem:** o Smart é montado a partir do aprendido da conta, até 500 pares. A meta era ficar abaixo
  de 25 ms para montar e resolver, medida por benchmark na C2, e só se não couber entraria um cache por
  conta, invalidado pelo `last_seq`. **[C2, 10-out-2026: não coube, e o cache entrou.]**
  - **Os números reais:** resolver com o Smart pronto leva cerca de **4 ms** (um ditado de 40
    palavras); montar do zero leva cerca de **1 s** com 500 pares e cerca de **50 ms** com 100; com o
    cache acertado, um ditado custa cerca de **3,7 ms**. A meta de 25 ms vale para resolver e para o
    cache acertado, e **não** para montar do zero.
  - **Por quê:** o lote do PC confere um par de cada vez sobre a semente acumulada (O(n²) por
    construção), e a paridade com o PC exige esse caminho. Antes dos ajustes, montar levava 21,5 s e
    resolver 818 ms (a busca varria entradas por aliases). Mais ganho pede reescrever as varreduras do
    lote em `dictionary` (`aprenderEm`, `projecaoDe`, `DominiosDaConferencia`), o que não foi feito para
    não arriscar o comportamento do PC; a estimativa é de cerca de 0,5 s com 500 pares.
  - **O cache** é o `smartconta.Cache` (tamanho padrão 16, cerca de 12 MiB no pior caso, contra o
    `GOMEMLIMIT` de 200 MiB). A versão dele é o `last_seq` **da conta**, contando os pares desfeitos, e
    não o maior `last_seq` dos pares ativos, que cai quando o par mais novo é desfeito. Ele se esquece
    no "Apagar meus dados" e ao desligar o ditado. A primeira montagem de 500 pares custa cerca de 1 s
    de CPU numa VPS pouco potente, e por isso entra nas vagas e nos limites da C5b.
    **[C5b, 11-out-2026: a versão do cache, na rota, não é o `last_seq` da conta. É um resumo SHA-256
    dos pares que montam o Smart, na ordem do `ListLearned` (`versaoDosPares`, em
    `internal/server/ditar_trabalho.go`): os mesmos pares dão a mesma versão, e qualquer mudança (um par
    novo, desfeito ou trocado, ou o aprendido apagado e reaprendido) dá outra, inclusive acima de 500
    pares e depois do "Apagar meus dados" com o par reaprendido (o id do par é o mesmo em toda conta).
    Não precisou de método novo no `Store`, e custa um `ListLearned` por ditado, que a dica e a
    montagem pedem de qualquer jeito. O cache se esquece no "Apagar meus dados" e no "Desligar o
    ditado", e de novo no fim de um ditado que um deles cortou (a correção `1eea4b9`).]**
  - **O índice do dicionário** (`1d76697`): o `Search` em memória e o `Normalize` rápido, sem mudar o
    resultado (testes diferenciais contra a versão antiga). No PC, o benchmark com o dicionário real
    foi de 6,03 ms para 0,243 ms.
  - **Duas escolhas da C2:** `Montar` reverte a lista (o `ListLearned` vem do mais recente ao mais
    antigo, e o lote aplica do mais antigo ao mais novo, a ordem do pull no PC: quem entra primeiro
    leva a melhor num conflito); e devolve só o erro fixo `ErrMontar`, sem a causa, porque os erros do
    dicionário citam o termo e texto ditado não vai a erro.
- **A dica (desligada por padrão até a C8):** termos distintos dos pares sem domínio nem restrição, do
  mais recente ao mais antigo, até 140 runas. **[C2: a dica sai direto dos pares, como esta ADR
  descreve, e não do Smart montado (`smartconta.Dica`, `DicaLigadaPorPadrao` falso). Por isso ela leva
  também o termo de um par que o lote recusou (palavra comum, que não agiria). Se o Mauro preferir só
  os que agem, a dica passa a exigir o Smart montado.]**
- **O que o celular não tem:** os termos do `dicionario.json` de cada PC (os de antes, os
  "adicionados" e as dicas deles), e os pares presos a um domínio. Com isso, o texto do celular pode
  sair pior que o do PC num termo que só o PC conhece. Isso fica escrito na tela de privacidade e em
  "Riscos".

### O Polish no celular

- **As preferências do celular** (decisão N): ligado ou desligado, o provedor (OpenAI ou Anthropic) e o
  modelo, da lista que a chave alcança (`ia.ListarModelos`).
- **A chave:** a do provedor do Polish. Se for a OpenAI dos dois lados, a mesma chave serve.
- **O aceite:** o `PUT` das preferências recusa ligar o Polish com um provedor fora do aceite vigente
  (`SEM_ACEITE`), e o app abre a folha (decisão H). A rota de ditar confere de novo antes de chamar.
- **O prazo:** o `prazoPolish` do PC, de 3,5 s a 8 s conforme o texto.
- **O que vai ao provedor:** o que o `09` já descreve para o PC (o texto do Smart, com 4 palavras ou
  mais, e o prompt fixo). Só muda quem chama: o servidor, e não o PC.

### As chaves

- **A tabela `ai_provider_keys`:**
  - a chave da tabela é (usuário, provedor), com o provedor em `openai`, `groq` ou `anthropic`;
  - colunas: o id da chave de cifra (8 bytes), o nonce de 12 bytes, o texto cifrado (com teto) e o
    `final` (até 4 caracteres, para a tela não precisar decifrar), mais as datas.
- **A chave de cifra:**
  - vem do arquivo `AI_KEYS_SECRET_FILE` **[derivada: o nome]**, com 64 caracteres em hexadecimal, como
    os outros segredos. **[C3a, 11-out-2026: o arquivo se chama `segredos/ai-keys-secret`. A variável
    comum `AI_KEYS_SECRET` também vale, como nos outros segredos, e as duas juntas dão saída 2. Uma
    chave de cifra igual (sem diferenciar maiúsculas) ao `SESSION_SECRET` ou ao `DEVICE_TOKEN_PEPPER` é
    recusada na subida.]**
  - a cifra usa uma subchave dela, derivada por HKDF com o rótulo `aurora-whisper/ai-keys/v1`, e o id
    é um HMAC dela com outro rótulo, truncado (decisão G). **[C3a: o rótulo do id é
    `aurora-whisper/ai-keys/id/v1`.]**
  - **numa VPS nova,** o `gerar-segredos.sh` a cria junto com os outros. **[C3a: ele NÃO a cria, ainda.
    O `compose_test` exige que o número de segredos que ele grava seja igual ao do compose, e o compose
    só declara o novo na C5c. Até lá, numa VPS nova, o passo é `gerar-segredos.sh` e depois
    `criar-chave-de-cifra.sh`, e o script novo aceita esse caso. A C5c passa a criá-la junto.]**
    **[C5c, 11-out-2026: feito. O `gerar-segredos.sh` cria o `ai-keys-secret` numa VPS nova (64
    hexadecimais de `/dev/urandom`, com a saída suprimida), e o `criar-chave-de-cifra.sh` recusa nesse
    caso ("ja existe"), sem mudar nada nem mostrar o valor; um teste prova as duas coisas. O
    `gerar-segredos.sh` também avisa que a chave de cifra não tem cópia fora da VPS.]**
  - **numa VPS que já está no ar,** o `gerar-segredos.sh` recusa rodar (a pasta `segredos/` existe). A
    C3a entrega um passo próprio, **[C3a: um script novo, `deploy/scripts/criar-chave-de-cifra.sh`]**,
    que cria só esse arquivo, `0444` na pasta `0700`, com a saída suprimida, e recusa sobrescrever. Ele
    é testado em `deploy/scripts_test.go`;
  - montada em `/run/secrets`, como o `SESSION_SECRET`, e acrescentada ao `SEGREDOS_ESPERADOS` do
    `subir.sh` quando o compose a declarar (C5c); **[C5c, 11-out-2026: feito. A recusa do `subir.sh`
    para este arquivo é separada das dos outros segredos: aponta o `criar-chave-de-cifra.sh` (com o
    caminho da cópia da VPS) e não o `gerar-segredos.sh`, que recusa rodar numa VPS no ar e só
    confundiria. O `subir.sh` também ganhou uma linha final que lembra a leitura do proxy e da swap
    antes do primeiro áudio]**;
  - entra na varredura de segredos e no `compose_test`;
  - um arquivo malformado impede a subida, com uma mensagem que não mostra o conteúdo.
- **O backup:** leva só o texto cifrado. Restaurado sem a mesma chave de cifra, cada chave vira
  `SEM_CHAVE`, a tela pede para colar de novo e o servidor não dá erro 500.
- **O que fica para trás ao "Trocar" ou "Remover":** a chave antiga, cifrada, nos backups (cerca de 30
  dias) e no banco `aurora_antes_AAAAMMDD` de uma restauração (7 dias, `deploy/README.md`). Se a chave
  de cifra vazar depois, essas cópias se decifram. Por isso a tela, ao "Trocar" ou "Remover", diz para
  **revogar a chave antiga no provedor**, e o dossiê (§6) registra as cópias.
- **As rotas** **[derivada: os nomes]**, todas com a sessão:
  - `PUT /api/v1/ai-keys/{provedor}` exige um aceite vigente com aquele provedor, valida, testa no
    provedor, cifra, grava e devolve só `configurada` e `final`;
  - `DELETE /api/v1/ai-keys/{provedor}` apaga;
  - `GET /api/v1/ai-keys/{provedor}/models` lista os modelos com a chave guardada.
- **Um limite por conta** nas duas que chamam o provedor (o `PUT` e o `GET` dos modelos): 10 por
  minuto, juntas (escolha menor 24). O `LimitesPadrao` de hoje não tem nada para rotas de sessão.
  **[C3a, 11-out-2026: a ficha é gasta logo antes de chamar o provedor; um pedido barrado antes (JSON
  quebrado, validação, sem aceite, sem chave) não gasta ficha. É um balde de fichas (10 de rajada e 1 a
  cada 6 s), e por isso numa janela de 60 s cabem até uns 19 pedidos; o sustentado é 10 por minuto, como
  os outros limites do projeto.]**
- **[C3a, 11-out-2026] O que a C3a fixou nas rotas:**
  - o `GET` dos modelos também exige o aceite (ele manda a chave ao provedor); o `DELETE` não pede;
  - o `GET` devolve a lista **inteira** do provedor, sem filtrar por uso, e o app filtra por nome (voz:
    `transcribe` ou `whisper`; Polish: o resto). A Groq é chamada pelo `internal/ia` na forma da OpenAI
    (`BaseOpenAI` com `https://api.groq.com/openai`, `GET /openai/v1/models` com `Bearer`), em
    `server.ModelosPelaRede`, o que não exigiu mexer no `ia` nem no Agent; a C7 confere numa chamada
    real. A lista filtrada da C4 (`nuvem.EhModeloDeTranscricao`) pode entrar no lugar;
  - o "Apagar meus dados" **já apaga a `ai_provider_keys` desde a C3a** (e não só na C3b, como a linha
    da C3b em "Subfatias" dizia), para que entre o merge da C3a e o da C3b a chave cifrada da conta não
    fique para trás;
  - pacotes novos: `internal/chavedeapi` (o pacote neutro de `Validar` e `Final`, com as mensagens que
    mantêm o prefixo `chaves:`, para o log do Agent não mudar), `internal/server/cofre` (a cifra) e
    `internal/server/chavestest` (a suíte de contrato, na memória e no Postgres). **Nenhuma dependência
    nova de produção** (`crypto/hkdf`, `crypto/aes`, `crypto/cipher`, `crypto/hmac`, `crypto/sha256`);
  - o servidor de teste do PC (`cmd/server`, `logindeteste`) sobe sem a chave de cifra, então as rotas do
    ditado no celular são `404` nele. Se o `pnpm dev` precisar delas, é gerar um cofre por processo,
    como o `SESSION_SECRET` de lá. **[C3-correções, 11-out-2026: feito. O servidor de teste monta
    `/dictation`, `/ai-keys` e, desde a C5b, `/dictations`, com um cofre por processo (a chave de cifra
    nasce a cada subida e nunca é impressa) e provedores de mentira, sem rede; ver `scripts/README.md`.]**
- **[C3a-revisão, 11-out-2026] O que a revisão adversarial achou:** nenhuma correção. Cinco mutações
  pegas e uma lacuna de teste (nenhum caso prova que duas sessões da MESMA conta dividem o balde do
  limite; o código usa o `UserID`, certo). Ficaram como risco: a corrida do `PUT` contra o apagar e o
  revogar (`13-implementation-plan.md`, TASK-028, "Fica aberto"); o `GET` dos modelos, que tem efeito
  colateral e não é barrado pelo `http.CrossOriginProtection`; e, só com acesso ao banco, o tamanho do
  texto cifrado que revela o tamanho da chave, o `key_suffix` em claro e fora do AAD (quem escreve no
  banco pode trocar o "terminada em" ou recolocar um cifrado antigo da mesma conta e do mesmo provedor),
  e uma chave de cifra fraca que a subida aceita (o script gera de `/dev/urandom`). **[11-out-2026: a
  corrida do `PUT` e o `GET` dos modelos foram fechados na C3-correções (`6925ecc`; riscos 18 e 19), e
  a lacuna do limite ganhou um teste (duas sessões da mesma conta dividem o balde). Os três itens
  teóricos, que só valem com acesso ao banco ou ao arquivo, seguem como estavam.]**
- **Em todo caminho de erro:** a chave decifrada nunca vai a log, erro, resposta ou URL. **A rota da
  chave não usa a mensagem do `decodeJSON`:** qualquer erro de decodificação responde com uma frase
  fixa, porque a mensagem do decodificador pode trazer um pedaço do que foi colado.
- **Para o servidor não importar o Agent:** as regras `Validar` e `Final` saem de
  `internal/agent/chaves` para um pacote neutro.
- **O que a cifra protege, e o que não protege.**
  - **Protege:** um dump ou backup do banco que vaze, uma leitura indevida do banco e o log do Postgres.
  - **Não protege:** quem tem a raiz da VPS. Essa pessoa lê o arquivo da chave de cifra e o banco, e
    decifra tudo. O dossiê (§6) diz isso com essas palavras, e a saída num incidente é revogar a chave
    no provedor.

### O aceite e as preferências

- **A tabela `dictation_settings`** (uma linha por conta): o provedor e o modelo da transcrição, a dica
  ligada, o Polish ligado, o provedor e o modelo do Polish.
  - **[C3b, 11-out-2026] Os modelos padrão:** o da OpenAI é o `gpt-4o-mini-transcribe` **até a C7** (o
    caminho "se não" da escolha menor 1, porque a chamada de teste em pt-BR ainda não rodou; trocar
    para o `gpt-transcribe` é uma constante, `ModeloPadraoOpenAI`, em
    `internal/server/cloudstore/ditado_celular.go`); o da Groq é o `whisper-large-v3-turbo`. O Polish
    não tem modelo padrão: fica vazio até a pessoa escolher, como no PC, e só pode ficar vazio com o
    Polish desligado. O modelo da transcrição vazio no `PUT` vira o padrão do provedor, resolvido no
    servidor.
- **A tabela `dictation_consents`:** cada aceite é uma linha, com a versão do aviso, os provedores
  aceitos, a data e, depois, a data da revogação. Provar o consentimento é do controlador (art. 8º,
  §2º, já citado no dossiê).
  - **[C3b, 11-out-2026] O aceite terminado tem `ended_at` e `end_reason`** (`revogado` ou
    `substituido`), e não só uma "data da revogação": o aceite novo substitui o anterior, e chamar isso
    de revogação falsearia a prova do consentimento. Um índice único parcial garante no máximo um
    aceite em aberto por conta, e a gravação e a revogação travam a linha do usuário
    (`FOR NO KEY UPDATE`). Aceitar de novo exatamente o mesmo (a mesma versão e os mesmos provedores)
    não grava outra linha e mantém a data da primeira vez.
  - **O aceite fica gravado como prova** (uma linha por aceite, os terminados também) **enquanto a
    conta existir**, e sai no "Apagar meus dados". As cópias nos backups seguem a retenção do backup.
- **As rotas:**
  - `GET /api/v1/dictation` traz o estado: as preferências, o aceite vigente e as chaves (só
    `configurada` e `final`);
  - `PUT /api/v1/dictation/settings` muda as preferências e recusa um provedor fora do aceite vigente;
  - `POST /api/v1/dictation/consent` grava o aceite, e só com a versão atual do aviso;
  - `DELETE /api/v1/dictation/consent` revoga e apaga as chaves.
  - **[C3b, 11-out-2026] O formato** (o `06` tem o detalhe): o `GET`, o `PUT` e o `POST` devolvem o
    mesmo estado, `{aviso, aceite, preferencias, chaves}`; o `DELETE` do aceite devolve `204`. O
    `aceite` do estado traz `vigente: false` quando a versão não é a atual. `chaves` traz uma entrada
    por provedor, com `colar_de_novo` (e `configurada: false`) quando a linha foi cifrada por outra
    chave de cifra, calculado com `cofre.MesmoID`, sem decifrar. O `PUT` das preferências é a
    substituição inteira (os três campos são obrigatórios). **Revogar mantém as preferências** (são só
    escolhas, sem segredo) e apaga **todas** as chaves da conta, também as de um provedor que o aceite
    já não cobria, mesmo sem aceite em aberto. As frases de erro são fixas, o corpo é estrito (sem eco) e
    o `AVISO_DESATUALIZADO` (`409`) é o código novo.
- **A versão do aviso:** mora num lugar que o app e o servidor leem, e um teste prende os dois (o
  detalhe é da C3b). **[C3b: em dois lugares presos por um teste, e não um arquivo só lido pelos dois,
  porque o Go não embute arquivo de fora do pacote e o Vite não deve importar de fora de `web/`:
  `cloudstore.VersaoDoAviso` (Go) e `VERSAO_DO_AVISO` (`web/src/nuvem/avisoDoDitado.ts`). Ver a decisão
  H.]**
- **Nada disso sincroniza com os PCs.** O "Apagar meus dados da nuvem" apaga as três tabelas
  (`internal/server/pgstore/contas.go`, a lista `apagarDaConta`), e um ditado em andamento é descartado
  (decisão I). **[C3a e C3b: a `ai_provider_keys` já saía no "Apagar" desde a C3a, e a C3b somou as duas
  tabelas dela.]**
- **No passo 2:** o aceite, as preferências e as chaves continuam sendo feitos pelo PWA, com a sessão.
  O app nativo recebe `SEM_ACEITE` ou `SEM_CHAVE` e manda a pessoa ao PWA, a menos que a ADR do passo 2
  dê o escopo a essas rotas.
- **As migrations:** 0017, 0018 e 0019, uma tabela por arquivo, só aditivas (as 0001 a 0016 já estão
  em produção). O `migrations/schema_test.go` prova que nenhuma coluna guarda áudio. **[C3a e C3b:
  `0017_ai_provider_keys_table`, `0018_dictation_settings_table` e `0019_dictation_consents_table`. O
  `serve` recusa subir com migration pendente: elas rodam pelo `migrate` antes da imagem nova e entram
  no roteiro de deploy (C5c).]** **[C5c, 11-out-2026: entraram no passo 10.2 do `docs/deploy-f11.md`.
  Não pedem comando à parte: o `subir.sh` já roda o `migrate` (no passo 6 dele, depois de conferir o
  Postgres, lê o `migrate -status` e, havendo pendente, para e pergunta "Digite SIM para rodar o migrate
  agora"; com o `SIM`, roda num contêiner descartável da imagem nova da API, na rede interna, e antes
  para a API se ela estiver de pé). As três são só aditivas: cada uma cria uma tabela, e nenhuma tabela
  anterior muda.]**

### A tela

- **O microfone:**
  - é um botão pequeno ao lado da busca, no cabeçalho. A pílula de aviso fixa no rodapé colidiria com
    um botão flutuante;
  - **gravando**, a linha da busca vira uma faixa com a onda, o tempo e um "Cancelar" (descarta sem
    enviar), e o microfone vira o botão de parar;
  - o "Gravando" fica só para o leitor de tela, sem rótulo visível. Segue a preferência do Mauro: o
    menor que funciona, com forma e movimento no lugar de texto (memória "UI pequena e discreta");
  - usa os tokens do `index.css`, claro e escuro, e respeita o `prefers-reduced-motion`.
- **A onda:**
  - vem do `AnalyserNode`, em dBFS;
  - os limites do PC (-48 a -14, ADR-014) foram medidos no microfone do PC, e no celular são
    **recalibrados na C6c e renderizados antes de concluir**;
  - a onda e a medição de fala usam um `AnalyserNode`, sem `AudioWorklet`, então a CSP
    (`default-src 'self'`) não muda.
- **Depois de parar:** a faixa diz "Transcrevendo" e o botão fica ocupado. Gravar o próximo enquanto o
  anterior transcreve fica fora da v1.
- **Quando chega:**
  - o ditado entra no topo, em destaque, com o "Copiar" que o `ItemDitado` já tem;
  - copiar sozinho ao chegar depende do navegador: escrever na área de transferência pode pedir um
    gesto recente, e a resposta chega segundos depois do toque (escolha menor 14, a conferir no
    aparelho). **[C6a, 10-out-2026: não foi feito; só o botão "Copiar" que a linha já tem. Fica para
    conferir no aparelho, na C7.]**
- **As frases de erro** (o app decide pelo `error.code`):

  | Situação | Frase |
  |---|---|
  | permissão negada | "Sem acesso ao microfone. Libere nas permissões do site." |
  | sem rede, ou o prazo estourou | o áudio fica na memória, com "Tentar de novo" (o mesmo id) e "Descartar" |
  | `401` | a mesma regra do texto não salvo (F4): o áudio fica na memória, e a pessoa entra de novo |
  | `SEM_ACEITE` | a folha do aceite |
  | `SEM_CHAVE` | leva às preferências |
  | `PAYLOAD_TOO_LARGE` | "Ficou longo demais." |
  | `SEM_FALA` | "Não ouvi nada." |
  | `UM_POR_VEZ`, `LIMITE` ou `OCUPADO` | "Espere um pouco." e o tempo |
  | `PROVEDOR_RECUSOU_CHAVE` | "O provedor recusou a chave." |
  | `PROVEDOR_LIMITE` ou `PROVEDOR_FORA` | "O provedor não respondeu agora." |
  | `APAGADO` | "Esse ditado foi apagado." |

  O áudio some ao fechar o app.
- **As Configurações do celular** ganham "Ditar pelo celular":
  - o estado ("Desligado" ou "Ligado desde 10/10/2026", com "Desligar");
  - um link que reabre a folha do aceite;
  - o provedor da transcrição;
  - a chave (colar; depois, "Chave terminada em WXYZ", com "Trocar" e "Remover", que lembram de
    revogar a antiga no provedor);
  - o modelo e a dica do aprendido;
  - o Polish, com o provedor, a chave e o modelo.
- **O texto de Privacidade da tela muda** (`ConfiguracoesNuvem.tsx:454` e os dois testes que o travam),
  com o texto aprovado na D0. Ele diz "a chave do PC continua no PC", sem a frase "Gerenciador de
  Credenciais", que o `bundle.test` proíbe no build da nuvem.
- **O build:** o gravador entra pelo `Ambiente` (`ditar?`, só no `ambienteNuvem`, como o
  `excluirDeVez`) e pelo `ClienteNuvem.ditar`, via `pedir()`. O `bundle.test` prova que o build local
  não tem `getUserMedia`, `MediaRecorder` nem as rotas novas.
- **Um cabeçalho novo:** `Permissions-Policy: microphone=(self)` (escolha menor 18), com teste.

### Tela apagada e segundo plano

**O que acontece hoje no Chrome do Android:**
- **Com a tela apagada ou o app fora da frente, o microfone para ou fica mudo.** Há um bug no Chromium
  em que um engenheiro diz que o bloqueio de tela desliga o microfone por privacidade
  (issues.chromium.org/issues/331092194).
- O gravador seguiria "gravando" silêncio, e o Whisper costuma inventar frases num fim mudo.
- O Android costuma apagar a tela em 30 s a 1 min, menos que o teto de 120 s.

**O desenho:**
1. **Ao começar,** o app pede `navigator.wakeLock.request('screen')` e solta ao parar. Só vale com HTTPS
   e com a página visível (Baseline 2025). Se o pedido falha, a faixa avisa: "A tela pode apagar e
   parar o ditado.".
2. **Se a página sai da frente** (`visibilitychange`), **ou a faixa do microfone fica muda ou acaba**
   (`mute` e `ended`, como numa ligação recebida), o app para. Ele envia o que já gravou, se tiver fala,
   e diz "Parei quando o app saiu da frente." ou "O microfone foi interrompido.".
3. **No passo 1 não existe ditado em segundo plano nem com a tela apagada.** A tela diz isso na
   primeira vez, e a política também. O passo 2 resolve, com um serviço em primeiro plano do app
   nativo. **[C6b, 11-out-2026: o aviso do "primeiro uso" entrou DENTRO do texto da folha ("O que isto
   não faz") e na introdução da seção do ditado nas Configurações, e não como um "visto" separado,
   gravado no servidor.]**
4. **Se o Android congela ou descarta a aba no meio do envio,** o servidor já está com o pedido e grava
   o ditado mesmo assim (decisão I). Ele aparece no histórico quando a pessoa volta.

### Limites

Os valores são escolhas menores (escolhas 4 e 24). Ficam em memória e zeram ao reiniciar, como os do
login.

| Limite | Padrão até o Mauro dizer | Por quê |
|---|---|---|
| Duração | 120 s, com uma folga para o cabeçalho **[C5b: 125 s, `TetoDaDuracaoDoDitado`]** | o mesmo teto do PC (`internal/agent/dictation/config.go:17`) |
| Corpo | 1 MiB | opus a 32 kbit/s: uns 0,5 MB para 120 s. A C6c e a C7 medem os bytes reais |
| Ditar, por conta | 1 de cada vez; 10 por minuto; 200 por dia | uma pessoa não dita mais que isso. Com a sessão roubada, o teto de gasto fica em uns 400 min por dia, cerca de US$ 1,80 por dia no `gpt-transcribe` |
| Ditar, no total | 3 ao mesmo tempo | a memória da API e a CPU dividida |
| As chaves, por conta | 10 por minuto, o `PUT` e o `GET` dos modelos juntos | cada pedido chama o provedor |

Só o envio que pega a vaga conta no limite, e a resposta idempotente não conta. A defesa principal
contra gasto é da pessoa: uma chave só para o celular, com limite de gasto no provedor.

### O que fica pronto para o passo 2

**O que o app nativo herda sem mudança:**
- a rota e o contrato (corpo cru, id do cliente, idempotência, resposta com o `final_text`, que ele
  escreve no campo);
- o `AcessoDitar`, que só precisa passar a aceitar o token com o escopo;
- os códigos de recusa estáveis, em `error.code`;
- o aceite e as preferências por conta, feitos pelo PWA;
- as chaves no servidor;
- os limites por conta;
- o Smart e o Polish no servidor.

**O que a ADR do passo 2 decide:**
- **A credencial.** Um aparelho Android pareado ganha um token com o escopo de ditar. O retorno por
  `127.0.0.1` do PC não serve no Android: provavelmente App Links com PKCE (RFC 8252), a conferir lá.
  O `AcessoDitar` passa a aceitar esse token ao lado da sessão. **O escopo também passa a ser
  conferido no `AcessoAparelho`** (o push e o pull), para que um token Android de ditar não ganhe o
  sync inteiro, e no `AcessoQualquer`.
- **O aceite e as chaves pelo app nativo,** se ele precisar: as rotas da C3 ganham o escopo, ou
  continuam só no PWA.
- **O formato.** O app nativo grava webm/opus (o `MediaRecorder` do Android tem WEBM com OPUS desde a
  API 29, a conferir), ou a lista de formatos cresce, conferida contra os dois provedores.
- **A captura em segundo plano** (serviço em primeiro plano).
- **A escrita em outro app** e o campo de senha. A acessibilidade e o teclado veem o que se digita em
  qualquer app (THR-011 e THR-012; `CLAUDE.md`: nunca transmitir contexto de campo protegido). É
  parada própria.

### A chegada ao PC (subfatia S)

Escopo novo, com OK próprio (decisão P).
- **No Agent:**
  - ganha `GET /api/nuvem/mudancas` na API local (loopback, a mesma cadeia de guardas), com um contador
    que sobe a cada pull que aplica alguma coisa. Não leva texto;
  - com o painel aberto (alguém leu o contador nos últimos 15 s), o laço busca a nuvem a cada 10 s,
    senão nos 60 s de hoje. É a opção recomendada e a escolha menor 17.
- **No painel do PC:**
  - visível, ele lê o contador a cada 3 s;
  - quando o contador muda, relê a lista, pelo mesmo caminho do "voltar à aba".
- **A alternativa:** 30 s sempre (a alternativa já registrada na ADR-019, "Decisões da F7a/b", decisão
  1), com o mesmo contador para reler a lista.
- **O que não muda:** o celular continua relendo ao voltar à aba.
- **Não depende do ditado** nem de LGPD, chave ou deploy na nuvem. Com o OK dele no intervalo, pode vir
  primeiro: é a queixa de agora.
- **[S, 10-out-2026] Implementada** (`6e599dc`, `cbb6d8b`), com os 10 s com o painel aberto e os 60 s
  fechado que o Mauro aprovou (escolha menor 17). O teste no PC é dele, depois de remontar o Agent. O
  que ela fixou, incluindo o que o texto acima não dizia, está em "O que a implementação fixou".

## Segurança e privacidade

### O que sai, e para quem, com o ditado no celular

| Para | O quê | Quando |
|---|---|---|
| A nuvem (a VPS) | a voz de cada ditado, **só em memória, durante o pedido, descartada ao fim**; o texto, que vira ditado no histórico como os do PC; a chave da pessoa, cifrada; o aceite e as preferências | a chave, depois do aceite; a voz, a cada ditado |
| O provedor de transcrição (OpenAI ou Groq, escolhido pela pessoa, com a chave dela) | a voz; com a dica ligada, os termos do aprendido (que podem ser nomes de clientes ou de terceiros) | a cada ditado, com o aceite |
| O provedor do Polish (OpenAI ou Anthropic) | o texto do Smart, com 4 palavras ou mais | só com o Polish ligado e aceito |
| Os provedores, ao guardar ou trocar a chave | a própria chave, para o teste (a lista de modelos) | depois do aceite |

**O que nunca vai:**
- a chave **do PC** (ela continua no PC);
- o áudio do PC;
- o contexto de outras janelas (o PWA nem o enxerga);
- o dicionário completo;
- o mapa de domínios.

**A voz não é usada para identificar a pessoa.** A política diz isso. A leitura de que a voz seria
dado biométrico (art. 5º, II; art. 11) depende desse uso, e fica a conferir no corpus da lei na D0.

**Quem é quem.** Quem hospeda a nuvem é o controlador; na instância de referência, o Mauro. Quem dita
é o titular; hoje, ele mesmo. Os provedores são operadores. Com BYOK, o contrato com o provedor é o da
conta da pessoa. As vozes de terceiros que o microfone captar, e os nomes de terceiros na dica, são de
outros titulares (§12 da política).

### O que a D0 reescreve

A preparação do código aberto transforma a política em modelo e o dossiê em exemplo genérico, com o
que é de cada instância em `[A PREENCHER]`. A D0 escreve o ditado no celular nesse modelo, em termos
gerais ("o provedor que a pessoa escolheu"), e o que é da instância do Mauro vai para a cópia dela
(escolha menor 26).

- **A política:**
  - §1: separar "o PC" de "o app": o áudio do PC continua no PC, e o do app vai ao provedor que a
    pessoa escolheu;
  - §3, §4 e §5: o ditado no celular, a finalidade e o consentimento específico;
  - §6: os operadores novos, com o país pelo que a documentação diz;
  - §7: a retenção nos provedores, e a chave antiga que fica nos backups;
  - §9: a chave cifrada e o "descartado ao fim";
  - §10: uma quarta "coisa para ler com calma";
  - §12: as vozes de terceiros, os nomes de terceiros na dica e o dado de cliente;
  - §13: aviso em destaque da mudança de finalidade (art. 8º, §6º).
- **O dossiê:**
  - §1: o que sai;
  - §2: a voz em memória na VPS e no provedor;
  - §3: os prazos dos provedores;
  - §4: as linhas novas da tabela de operadores, e o art. 33 para a voz, que é legível e não cifrada;
  - §6: quem toma a VPS ouve o áudio em trânsito e decifra as chaves; as chaves antigas, cifradas, nos
    backups e no banco da restauração;
  - §7: as decisões novas, e o item 4 (dado de cliente) estendido à voz **e à dica**. Com dado de
    cliente, quem dita pode ser operador desse dado (art. 39), e mandar a voz ou os termos a um
    terceiro fora do Brasil pode ir além do que o cliente autorizou. A decisão é de quem dita; na
    instância de referência, do Mauro.
- **O texto da folha do aceite e o texto novo da tela.**
- **Os artigos citados de memória** (art. 8º, §4º; art. 33, VIII; art. 5º, II e art. 11) são conferidos
  no corpus na D0, com o consultor de LGPD.

### Pendências de LGPD (registro; esta ADR não as resolve)

A revisão de LGPD da nuvem foi **aprovada pelo Mauro em 10-out-2026** (ADR-019, "A revisão de LGPD"),
com os três pontos em aberto da D3 aceitos como estão. Não há nada a perguntar de novo sobre ela. O
que continua vazio são os campos que o dossiê pede "antes do primeiro dado real" (dossiê §4) e os que
a política deixa para o responsável:

| # | O quê | Onde |
|---|---|---|
| L1 | a região do bucket do backup, já escolhida por ele na criação, ainda não escrita | política §6; dossiê §4 e §7, item 1 |
| L2 | a hipótese do art. 33 para o backup cifrado fora do Brasil | política §6; dossiê §4 |
| L3 | o nome e o contato do controlador, e o encarregado | política §2; dossiê §7, item 5 |
| L4 | o prazo real de apagar no provedor do backup | política §7 |
| L5 | exportar e o cadastro que não sai (as decisões 2 e 3 do dossiê) | política §8; dossiê §7 |
| L6 | o cabeçalho "RASCUNHO, não está em vigor" da política e o "falta o ok" do `09`, que só precisam refletir a aprovação | o cabeçalho da política; `09`, "LGPD" |

**Recomendação:** fechar a L1 a L5 na LG, uma fatia de documentos só delas, antes do OK da D0. Somar
uma segunda transferência internacional, agora de dado legível, antes de dar base escrita à primeira
piora a situação. A L6 é só texto, e a preparação do código aberto já a faz. As respostas são do
Mauro (a região, a hipótese, o contato e as duas decisões), e esta ADR não as preenche.

## Fora do escopo (passo 1)

- **O app nativo, a bolha flutuante e o teclado:** passo 2, outra ADR.
- **Ditar sem internet, com a tela apagada ou em segundo plano.**
- **O iPhone:** o Safari grava mp4/aac, e o login é o da ADR-019.
- **Gravar o próximo enquanto o anterior transcreve.**
- **Troca automática de provedor.**
- **O dicionário completo no servidor** (decisão 4 do Mauro); **subir as dicas "adicionadas" do PC.**
- **Copiar a chave do PC para a nuvem:** a pessoa cola de novo, de propósito.
- **Manter duas chaves de cifra ao mesmo tempo:** trocar o arquivo leva as chaves guardadas a
  `SEM_CHAVE`, e a pessoa cola de novo.
- **Cifra ponta a ponta do texto** (ADR-019).

## Alternativas descartadas

- **Whisper na própria VPS.** Disputa a CPU com os serviços vizinhos, o que é critério de parar. Exige
  um contêiner e um binário novos, e erra 2,3 vezes mais que o turbo (`docs/benchmarks/asr-decisao-2026-09-18.md:127`).
- **PCM pelo `AudioWorklet`.** Pesa 3,84 MB em 120 s e exige subida contínua de uns 0,5 Mbit/s para
  caber nos prazos. O que o PCM comprava (a VAD e a duração no servidor) foi para o app (decisão M).
- **A chave do projeto no servidor.** A ADR-016 já a descartou: o projeto é aberto, e cada instalação
  usa a sua chave.
- **O celular falando direto com o provedor.** A chave ficaria no navegador, a CSP (`connect-src`
  `'self'`) teria de abrir, e o Smart e o Polish não rodariam no mesmo lugar.
- **A Web Speech API do Chrome.** O código do Aurora mandaria a voz ao Google, sem contrato e sem a
  escolha da pessoa. Não leva a dica e não tem qualidade medida em pt-BR.
- **O microfone do Gboard numa caixa de texto.** Pede outro gesto, com dois toques a mais, e o Aurora
  só acrescentaria o histórico.
- **Reaproveitar o `asr_gateway.go` antigo.** Ele era multipart de até 20 MiB, só PCM e sem
  idempotência. **[C5c, 11-out-2026: saiu, inteiro, junto do servidor em memória da F1-D (16 arquivos
  apagados de `internal/server`, 11 de código e 5 de teste; escolha menor 19). A remoção foi refeita do
  zero, a partir do `deadcode` e do compilador, sem branch nem commit antigo. `POST /asr/transcribe` e
  `POST /transcriptions` seguem `404` com qualquer credencial, e a suíte `authtest`
  (`testNaoMontadas`) e a `contracttest` (`testUnmountedRoutes`) travam isso.]**
- **Uma segunda interface de ASR só para áudio comprimido.** Seriam dois contratos (decisão C).
- **Uma fila de áudio**, no servidor ou no aparelho, para absorver falha. Seria persistir áudio, o que é
  parada do `CLAUDE.md`. Falhou, o áudio fica em memória até "Tentar de novo" ou "Descartar".
- **O multipart por `io.Pipe`.** Vai em `chunked`, sem `Content-Length`, e não está provado que os dois
  provedores aceitam. O tamanho é conhecido, então ele é calculado (decisão D).
- **Calibrar com arquivos gravados no celular.** Um gravador de outro app dá m4a, sem o eco, o ruído e
  o ganho do `getUserMedia` do Chrome, e a calibração não valeria; um arquivo no aparelho ou no PC é
  persistir áudio. A C6c mede no próprio aparelho, sem arquivo.

## Consequências

**Positivas:**
- o Mauro dita no celular, e o texto sai pelo Smart com o que ele ensinou e, se ele quiser, pelo Polish;
- o texto chega aos PCs pelo sync, e com a S chega mais rápido;
- o servidor não ganha carga de CPU: só repassa, em geral, 0,5 MB;
- a rota já serve ao app nativo do passo 2, com acesso e códigos próprios.

**Negativas:**
- **a voz sai do aparelho** para um operador estrangeiro escolhido pela pessoa, e passa pela memória da
  VPS;
- **uma chave de cobrança da pessoa mora no servidor**, cifrada, mas legível para quem tomar a VPS, e
  as antigas ficam cifradas nos backups;
- **o servidor fala com mais três APIs externas** (OpenAI, Groq e Anthropic);
- **o ditado do celular depende de internet**, ao contrário do PC;
- **o texto do celular não usa o dicionário completo** do PC, nem os pares presos a um domínio;
- **mais superfície:** oito rotas novas, três tabelas e um segredo novo;
- **o Agent muda de lugar três peças** (C2 e C5a), sem mudar o comportamento.

## Riscos aceitos, e o que não foi verificado

1. **Quem tem a raiz da VPS** ouve o áudio em trânsito e decifra as chaves (a chave de cifra e o banco
   moram no mesmo host). Mitigação: uma chave só para o celular, com limite de gasto, revogada no
   provedor num incidente.
2. **O país do processamento não está comprovado:** na Groq, não se sabe onde a transcrição roda; na
   OpenAI, só com a residência de dados fixada. A política escreve o que a documentação diz.
3. **O servidor não confere se o ZDR está ligado** na conta da pessoa.
4. **O reenvio ainda paga de novo em três casos:**
   - o prazo de 20 s corta uma transcrição que o provedor já processou e cobrou;
   - o servidor reinicia além da espera do desligamento, ou cai;
   - o desfecho pago e não gravado passa dos 10 min em memória.
   O valor em jogo é de centavos por ditado: um ditado de 120 s custa no máximo US$ 0,009 no
   `gpt-transcribe` (US$ 0,012 no `gpt-4o-transcribe`), mais os tokens do Polish.
5. **No aparelho, "nunca em disco" não é garantido:** o `Blob` do `MediaRecorder` pode ser paginado
   pelo Chrome. Não verificado.
6. **"Descartado ao fim", e não "apagado":** o Go não zera a memória.
7. **A dica pode induzir termos, e o ganho na nuvem não foi medido** (o 61,2% → 79,6% é do whisper.cpp
   local). Ela começa desligada; a C8 mede.
8. **A qualidade não foi medida** no microfone do celular, em falas de 30 a 120 s, nem nos modelos da
   OpenAI em pt-BR. O "WER 0,111" do turbo foi medido no PC, com falas de até 9,3 s.
9. **Preço, limites e modelos dos provedores mudam:** a OpenAI troca modelo sem alias, e a Groq está
   mudando de hardware e de direção. O provider lista os modelos que a chave alcança, e nenhum preço
   mora no código.
10. **Uma sessão roubada gasta o dinheiro da pessoa** até os limites por conta.
11. **O estado da idempotência é de um processo só.** Duas instâncias da API exigiriam levá-lo ao banco.
12. **O Smart do servidor depende de pacotes movidos do Agent.** O teste de paridade da C2 prende o
    comportamento. O `resolver` continua puxando `internal/context` e `internal/agent/dictation`, que
    compilam no Linux. **[C2 e C5a, 10-out-2026: o teste de paridade existe (`paridade_test.go`), e o
    fecho do `smartconta` é puro (`dependencias_test.go`: sem `storage`, `sync`, Windows nem
    terceiros). O `internal/polish` importa `internal/agent/dictation` só por
    `AcrescimoPeloTexto`, para a folga de 1,5 s até o watchdog do núcleo valer por construção, e o
    `limites_test.go` proíbe qualquer outro import de `internal/agent`, de `cmd/` e de `x/sys`. Se o
    Mauro quiser zero import de `internal/agent` no servidor, a saída é subir `AcrescimoPeloTexto` para
    uma folha neutra que o `dictation` também use: pequena, independente e não feita, para não tocar o
    núcleo (ADR-007).]**
13. **A configuração real do proxy** (o `readTimeout`, o `writeTimeout` e o buffering do entrypoint) e
    **a swap da VPS** (se o kernel conta a swap no cgroup) não foram lidas. A C7 as lê antes do
    primeiro áudio, e para se a swap do contêiner não der `0`.
14. **O proxy segura pedaços do corpo na memória dele,** fora do `memswap_limit` do nosso contêiner, e
    o proxy não é nosso nem pode ser editado. A promessa "nunca vai a swap" vale para o contêiner da
    API.
15. **O formato do Chrome:** o webm do `MediaRecorder` não traz a duração no cabeçalho, e o
    `audioBitsPerSecond` pode não ser respeitado. A C6c mede os bytes por segundo; a C7 confere uma
    gravação real nos dois provedores e os bytes de um ditado de 120 s.
16. **A revogação no meio do Polish:** a conferência do aceite antes do Polish e antes de gravar deixa
    uma janela de milissegundos. O "Apagar meus dados" não tem essa janela (o epoch é conferido dentro
    do `Create`). **[C5b, 11-out-2026: segue aceito. A rota confere o aceite de novo antes do Polish e
    antes de gravar, mas não o fecha dentro do `Create`: mexer na ordem das travas do ponto único de
    gravação, nos dois Stores, é risco maior que o ganho, porque o provedor já foi chamado e só o
    armazenamento ficaria na janela. Só as chaves tiveram a janela fechada (risco 18). Um caso de
    corrida, `Ditar_Corridas_AceiteSemOPolishNoMeio`, prova que a revogação no meio não chama o
    Polish.]**
17. **As chaves antigas ficam cifradas nos backups** (cerca de 30 dias) e no banco da restauração (7
    dias) depois de "Trocar" ou "Remover". A tela manda revogá-las no provedor.
18. **[Achado na revisão da C3a e na C3b, 11-out-2026. FECHADO na C3-correções, `6925ecc`, no mesmo dia]
    O `PUT /ai-keys` não fechava a corrida com o apagar e o revogar.** O `GravarChaveDeIA` não conferia
    o `sync_epoch` nem o aceite na hora de gravar. Um `PUT` cujo provedor responde (até 10 s) depois de
    um "Apagar meus dados", ou de um "Desligar o ditado no celular", gravava a chave cifrada de novo, e
    ela sobrevivia (a chave estrangeira de `users` continuava valendo). Ela não era usável sem aceite (a
    rota de ditar o confere a cada ditado) e sumia no próximo revogar ou "Apagar meus dados".
    **Como fechou:** o `PUT` lê o `sync_epoch` primeiro; a gravação (`GravarChaveDeIAConferindo`) é uma
    transação que trava a linha do usuário e o contador de sync (`FOR NO KEY UPDATE`, nessa ordem) e só
    então reconfere o epoch e o aceite em aberto que cobre o provedor (`Aceite.Cobre`). Se o epoch
    mudou, `409` `CONFLICT` com a frase fixa "os dados da conta mudaram enquanto a chave era conferida:
    nada foi guardado" (motivo de log `conta_mudou`); se o aceite deixou de valer, `403` `SEM_ACEITE`.
    Nos dois, nada é gravado. As duas travas são necessárias (o revogar e o aceitar travam só a linha do
    usuário, e o apagar só o contador), e cada uma tem uma mutação que a pega. Cobre a memória e o
    Postgres (`Correcoes_*`, em `chavestest`).
19. **[Achado na revisão da C3a, 11-out-2026. FECHADO na C3-correções, `6925ecc`]
    O `GET /ai-keys/{provedor}/models` tinha efeito colateral:** chama o provedor com a chave guardada e
    gasta uma ficha do limite, e o `http.CrossOriginProtection` não barra `GET` (o cookie de sessão é
    `SameSite=Lax`). Com o aceite e a chave já gravados, uma navegação de topo vinda de outro site
    gastava fichas da conta; o atacante não lê a resposta, a lista de modelos é grátis, e o efeito
    máximo era travar as rotas das chaves por um minuto. **Como fechou:** o `GET` e o `HEAD` recusam
    (`403` `FORBIDDEN`, "pedido de outra origem", motivo de log `origem_cruzada`) um `Sec-Fetch-Site` que
    venha e não seja exatamente `same-origin`: `cross-site`, `same-site`, `none`, o vazio e vários
    valores são recusados; o ausente passa. A recusa vem depois do `404` de provedor desconhecido e
    antes do aceite, da chave, do limite e do provedor. O `none` (digitar a URL) também é recusado, ao
    contrário do `CrossOriginProtection` dos outros métodos. O app faz o `GET` com `fetch` da própria
    origem, que o navegador manda como `same-origin`; **a C7 confere que nenhum proxy do caminho remove
    ou troca esse cabeçalho** (hoje, sem ele, a rota aceita).
20. **[Achado na integração da fase 1, 11-out-2026. FECHADO na C6-alinhar, `796d17d`, no mesmo dia]
    O app (C6b) e o servidor (C3b) não falavam o mesmo JSON.** A C6b desenhou o cliente e o servidor de
    mentira antes de a C3b existir, e a C3b fixou outro formato (o `06`): o estado traz as chaves como
    lista e as preferências aninhadas, e o app esperava um mapa e as preferências planas; o `POST` do
    aceite leva `{versao, transcricao, polish}` com a versão **inteira**, e o app mandava
    `{versao, provedores}` com a versão em **texto** (`web/src/nuvem/ditar/aviso.ts` guardava
    `'2026-10-10'`, provisória, e `avisoDoDitado.ts` guardava o inteiro `1`, preso ao Go). **Como
    fechou:** o `cliente.ts` e o servidor de mentira passaram a falar o `06`; a versão do aviso é só a
    `VERSAO_DO_AVISO` inteira (o `aviso.ts` não guarda mais versão); e o `contratoDoDitado.test.ts` lê o
    `06` (`?raw`) e prende os campos e os status do servidor de mentira e do cliente ao exemplo e à
    tabela de códigos. **Fica sem prova**, no Go, a ponta entre o servidor real e o exemplo do `06`: um
    teste em `internal/server` que serialize o estado real e compare os campos com o exemplo.
21. **[Achados na revisão adversarial da C5b, 11-out-2026; todos de baixo risco. Os três primeiros
    foram corrigidos no acabamento do mesmo dia, cada um com um teste que falhava antes]**
    - ~~O "sem fala" e o desfecho pago e não gravado (que tem o **texto**) vencem só quando chega um
      pedido novo.~~ **Corrigido em `7b20b1e`:** eles vencem sozinhos aos 10 min (um `AfterFunc` no
      `concluir`, com o relógio injetável no teste), num servidor ocioso também.
    - Um pago gravado na memória depois de um "Apagar meus dados" que aconteceu durante uma gravação que
      falhou (o banco fora no mesmo instante) fica com o texto até vencer (agora, no máximo 10 min). Ele
      não é usável, porque o epoch não bate.
    - ~~No desligamento, o ditado pago se perdia se o dono soltasse o trabalho depois do primeiro
      `Shutdown`.~~ **Corrigido em `18c2aa1`:** o `EsperarDitados` roda de novo depois do segundo
      `Shutdown`, dentro dos 15 s + 70 s, abaixo dos 90 s do `stop_grace_period`.
    - ~~Um pânico entre pegar a vaga e soltar o trabalho deixaria a vaga e o andamento presos.~~
      **Corrigido em `609d928`:** um `defer` de guarda encerra o andamento com `500 INTERNAL` e devolve a
      vaga (a ficha gasta não volta).
    - De projeto, aceito: três contas da lista de permitidos com o corpo lento (até 30 s cada) seguram
      as 3 vagas do total.
    - O `modelosConhecidos` é global por provedor e tem teto de 2000 nomes: uma conta com muitos modelos
      ajustados poderia encher a lista, e o Polish de outra conta com um modelo novo cairia em
      `modelo_inexistente`. O nome de um modelo ajustado (`ft:...:org`) listado pela chave de uma conta
      pode aparecer na linha `ditado` do log, só no servidor.
    - Cosmético: quando as 3 voltas de quem espera o mesmo id se esgotam, a resposta é `503` `OCUPADO`
      mesmo que a última espera tenha visto o ditado gravado (o reenvio seguinte recebe `200`); o log de
      acesso marca `200` no pedido cujo celular caiu sem resposta; no reenvio que só regrava, a linha
      `ditado` sai com `duracao_ms=0` e com o provedor das preferências atuais.
    - **O `go test -race` não rodou** (a máquina não tem compilador C): vale rodar `internal/server` (o
      registro, o `desistir`, o `olharDeNovo` e o trabalho solto), `internal/smartconta` e
      `internal/asr/nuvem` com `-race` no CI Linux.
    - Teórico: a conferência do epoch antes do Polish só tem prova indireta (o "Apagar meus dados"
      também apaga as chaves, então o Polish não sai de qualquer jeito); um backup restaurado com as
      chaves seria o caso que só ela pega. Os limites e a memória da rota são por processo (somem no
      reinício), como os outros.

## Pontos de parada (`CLAUDE.md`: parar e perguntar)

- o OK do Mauro nesta ADR e nas derivadas, antes de qualquer código do ditado;
- o OK dele na S (o intervalo) e na LG (as respostas), cada um à parte;
- o OK dele na política e no dossiê reescritos (D0), antes do primeiro áudio real sair do aparelho.
  Recomendado: a LG antes;
- deploy, as migrations 0017 a 0019 em produção, o `memswap_limit`, o `ulimits` e a chave de cifra nova,
  gerada por ele na VPS com a saída suprimida: tudo isso é com ele; **[11-out-2026: o Mauro autorizou o
  deploy. O resto desse ponto segue sendo dele, e o primeiro áudio real segue esperando o OK da D0.]**
- a configuração do proxy e a swap da VPS: só ler, nunca editar o proxy. **Se a swap do contêiner não
  der `0`, parar e perguntar** antes do primeiro áudio;
- a chave da OpenAI dele: colada por ele no app, **nunca no chat** (memória "Saída com segredo vai ao
  chat");
- **a calibração da C6c, com o OK dele:** ele abre a página de calibração no próprio Android, pelo
  cabo. O microfone abre, mas nenhum áudio é gravado em arquivo nem sai do aparelho; a página mostra
  só números;
- **a medição da C8, com o OK dele:** mandar a voz dele ao provedor para medir. No desenho, sem
  arquivo de áudio (veja C8);
- **qualquer arquivo de áudio** (por exemplo, para comparar com e sem a dica sobre o mesmo áudio) é
  persistência de áudio e parada própria: antes, o meio (só o cabo USB, nunca um app de mensagens nem
  um drive), a pasta fora do repositório (o `.gitignore` já recusa `*.webm`), e a data em que os
  arquivos são apagados, tudo com o OK dele;
- dependência nova de produção: nenhuma prevista (`crypto/aes`, `crypto/cipher`, `crypto/hkdf`,
  `crypto/hmac`, `mime/multipart`, `net/http`). Qualquer uma é justificada na subfatia;
- **testes:** nenhum `go test` chama provedor real (só `httptest`). Nenhum teste do app abre o
  microfone ou a área de transferência de verdade (dublês do `getUserMedia`, do `MediaRecorder`, do
  `wakeLock` e do `clipboard`).

## Subfatias

Toda subfatia roda `go test ./...` e `go vet ./...`, e o lint, o teste e o build do `web/` quando mexe
nele. Toda subfatia atualiza os documentos que muda e termina com commit local, sem `git push`. As de
Postgres rodam contra o contêiner de teste, quando o Mauro o sobe.

A coluna "Estado" é de 11-out-2026. Os hashes são os dos commits na `main` (os merges da fase 1 são
`919338a`, `fc3cec7`, `ce08af4`, `c044fcf` e `a4405ec`; os da fase 2, `922627c` para os documentos,
`e4a9aac` para o app e `50fd062` para o servidor). O resto de cada linha é o que foi desenhado.

| Subfatia | Estado | O que entrega | O que prova | O que o Mauro faz |
|---|---|---|---|---|
| **S** (OK próprio) | **Implementada (10-out-2026):** `6e599dc`, `cbb6d8b`. 10 s e 60 s, aprovados pelo Mauro. **Teste no PC: dele, depois de remontar o Agent.** | A chegada mais rápida no PC: o contador na API local; 10 s com o painel aberto (ou 30 s sempre, a escolha dele); o painel relê a lista quando o contador muda | Teste do laço com relógio falso (10 s com o painel lendo, 60 s depois de 15 s sem ler); teste da rota local sem texto; vitest do painel relendo; no aparelho, uma edição feita no celular aparece no painel aberto do PC em até ~13 s, sem tocar em nada | escolhe o intervalo; testa no PC (fechar o Agent e conferir o `StartTime`) |
| **LG** (OK próprio) | **Pendente:** espera as respostas do Mauro. | As pendências L1 a L5 da LGPD, só documentos, na cópia da instância dele | Nenhum `[A PREENCHER]` da instância dele em aberto | responde a região, a hipótese do art. 33, o contato e as decisões 2 e 3 do dossiê, e dá o OK |
| **D0** | **Escrita (10-out-2026):** `b0820dd` (esta ADR), `c4d0223` (as emendas), `feaaa12` (política, dossiê, README e PRODUCT). **OK do Mauro na ADR: 10-out-2026**, com os padrões (libera o código). **OK na política e no dossiê: pendente** (libera o primeiro áudio real). | Esta ADR e as emendas (cabeçalho "Emenda"); o modelo da política e o dossiê reescritos; o texto da folha do aceite e o da tela; as páginas dos provedores relidas, com data; os artigos conferidos no corpus | Nada de código. **O OK do Mauro na ADR libera o código do ditado; o OK na política e no dossiê libera o primeiro áudio real.** As duas datas ficam registradas aqui | lê e aprova (pode ser em dois momentos) |
| **C1** | **Implementada (10-out-2026):** `1d36609`. | `Store.Create` nos dois Stores, pelo ponto único de gravação, reaproveitando a regra de criar (`cloudstore/rules.go:291`) e a validação do payload, **com o epoch esperado**; o `Store.Situacao` (ditado, lápide ou nada); o `Actor` sem aparelho; o id só confere usuário; a origem "pelo app" no histórico do PC | Suíte de contrato verde na memória e no Postgres; o evento de sync sai sem aparelho; o pull entrega o ditado; um teste do sync do PC aplica um ditado `web` como recebido; o mesmo id não cria dois; o mesmo id em duas contas cria dois; **o `Create` com o epoch velho recusa e não grava** (depois do "Apagar meus dados"); o `Situacao` acha a lápide | sobe o Postgres de teste |
| **C2** | **Implementada (10-out-2026):** `43e6648`, e `1d76697` (o índice do dicionário). Os números reais e o cache por conta estão abaixo. | O pacote puro do Smart da conta (as conferências e o `SmartDaSemente` movidos do Agent, que passa a importá-los); o `Colapsar`; a dica | Testes de tabela: o par sem domínio corrige, o par com domínio, desfeito ou ausente não age, e o texto sem par passa igual. **Paridade:** os mesmos pares e o mesmo texto dão o mesmo resultado que o Smart do PC sem o `dicionario.json`. A dica respeita as 140 runas e não leva par preso a domínio. Benchmark: 500 pares montados e resolvidos em menos de 25 ms. **[Medido: resolver leva cerca de 4 ms; montar do zero leva cerca de 1 s com 500 pares e cerca de 50 ms com 100; o cache por conta entrou e, acertado, custa cerca de 3,7 ms. A meta de 25 ms vale para resolver e para o cache, e não para montar do zero.]** A suíte do Agent continua verde | nada (o teste no PC da C5a cobre também esta mudança) |
| **C3a** | **Implementada (11-out-2026):** `d0ae5b6`. A revisão adversarial rodou **sem correção** (cinco mutações pegas, uma lacuna de teste); as pendências foram para a C3b e para o `06`. | O cofre: AES-GCM com a subchave por HKDF, o id da chave de cifra, a chave de cifra por `*_FILE` em hexadecimal; a migration de `ai_provider_keys`; as rotas das chaves, com o limite por conta e a frase fixa no erro de decodificação; o `Validar` e o `Final` num pacote neutro; as rotas fora quando falta o arquivo; o passo que cria só o arquivo da chave de cifra numa VPS no ar | Canário: a chave nunca aparece em resposta, log, erro nem em claro no banco (varredura dos bytes da tabela), **também com um JSON malformado que leva a chave**. O texto cifrado copiado para outro usuário ou provedor não decifra. Trocar o arquivo leva a `SEM_CHAVE`. Só o `final` volta. Sem o arquivo, `404`; com ele malformado, a subida falha sem mostrar o conteúdo. O `PUT` sem aceite vigente recusa sem chamar o provedor. O 11º pedido no minuto é recusado sem chamar o provedor. O passo novo cria só o arquivo, `0444` na pasta `0700`, sem mostrar nada, e recusa sobrescrever (`deploy/scripts_test.go`) | sobe o Postgres de teste |
| **C3b** | **Implementada (11-out-2026):** `6447cf0`. A corrida do `PUT` da chave contra o apagar e o revogar, levada da revisão da C3a, **seguiu aberta nela e foi fechada na C3-correções (`6925ecc`, linha abaixo; risco 18)**. | As preferências e o aceite: as migrations de `dictation_settings` e `dictation_consents`; as rotas; a versão do aviso presa entre o app e o servidor; o "Apagar meus dados" apagando as três tabelas | O aceite fica gravado com a versão e os provedores, e a revogação o encerra e apaga as chaves. O `PUT` das preferências recusa um provedor de transcrição ou de Polish fora do aceite. O "Apagar" deixa as três tabelas vazias. As migrations aplicam sobre um banco com dados, e o `schema_test` não acha coluna de áudio | sobe o Postgres de teste |
| **C3-correções** | **Implementada (11-out-2026):** `6925ecc`. Fecha os riscos 18 e 19 e a lacuna de teste do limite da C3a-revisão. | A gravação da chave sob a trava da conta (o `PUT` lê o epoch primeiro; `GravarChaveDeIAConferindo` trava a linha do usuário e o contador, nessa ordem, e reconfere o epoch e o aceite que cobre o provedor); `409` `CONFLICT` (`conta_mudou`) e `403` `SEM_ACEITE` sem gravar; o `GET` e o `HEAD` dos modelos só da mesma origem (`origem_cruzada`); `Aceite.Cobre` como única conferência de uso; o deadlock do revogar com o apagar corrigido (`apagarDaConta` apaga os aceites antes das chaves); o servidor de teste do PC com o ditado (cofre por processo e provedor de mentira) | Os casos `Correcoes_*` da suíte `chavestest`, na memória e no Postgres: apagar, revogar, trocar o aceite e apagar e aceitar de novo durante o provedor falso não gravam a chave; a gravação e o apagar ao mesmo tempo não deixam chave; o `GET` de outra origem é recusado; duas sessões da mesma conta dividem o balde. `TestPostgres_RevogarEApagarNaoSeTravam` reproduz o deadlock. **13 mutações, todas pegas** (inclusive as duas travas da gravação, uma de cada vez) | nada |
| **C4** | **Implementada (10-out-2026):** `bb82253` (o contrato), `42d92c5` (os provedores). Sem chamada real: os campos de cada modelo se conferem na C7. | O contrato do ASR com o áudio comprimido (`AudioBuffer.Comprimido` e `Formatos`); o provider local recusando o comprimido; o pacote dos provedores OpenAI e Groq, com o multipart de tamanho calculado; a lista de modelos filtrada; o filtro de silêncio | Testes contra um servidor falso (`httptest`): os campos enviados (modelo, `pt`, `prompt`, arquivo e tipo); **o pedido chega com `Content-Length` e sem `chunked`**; `401`, `403`, `413`, `429`, `5xx` e redirecionamento viram os motivos da lista, sem corpo e sem seguir; texto vazio vira `SEM_FALA`; o segmento de silêncio sai. Canário de áudio e de chave fora de erro e log. Nenhum arquivo novo em disco. Nenhuma chamada real. O Agent compila e os testes dele passam sem mudança | nada |
| **C5a** | **Implementada (10-out-2026):** `b1d428f`. Paridade provada. **Teste no PC: dele, depois de remontar o Agent.** | O adaptador do Polish (`clienteDoDitado`, a política de esforço e o `prazoPolish`) num pacote comum; o Agent passa a usá-lo de lá | **Paridade no Agent:** os testes do Polish do PC (`winruntime` e `cmd/agent`) passam sem mudança, com o mesmo prazo por tamanho de texto; o pacote comum compila no Linux | testa no PC: fechar o Agent e conferir o `StartTime`, e ditar com o Polish ligado (curto e longo) |
| **C5b** | **Implementada (11-out-2026):** `d8e03b9`, com a revisão adversarial do mesmo dia, que achou dois defeitos reais e os corrigiu em `ca2d21f` (o reenvio que chega no fim do primeiro não paga o provedor de novo) e `1eea4b9` (o ditado cortado pelo "Apagar meus dados" ou pelo "Desligar" não guarda de novo o Smart da conta). Os achados de baixo risco que ficaram estão no risco 21. **Nada foi visto num aparelho nem numa chamada real.** | A rota `POST /api/v1/dictations`: o `AcessoDitar`; os códigos em `error.code`; as conferências antes do corpo; os prazos; a idempotência por `(user_id, id)`, com o desfecho pago guardado; o "em andamento" e quem espera estendendo o próprio prazo; as vagas e os limites; o formato pelos primeiros bytes; o provedor; o Smart; o Polish (o pacote da C5a) com o aceite conferido de novo; a gravação desligada da conexão, com o epoch; a espera dos ditados no desligamento; a linha de log | `authtest` e `contracttest`: `401`, **`403` com o token de um PC**, `SEM_ACEITE` (sem aceite do provedor da transcrição **e** sem aceite do provedor do Polish), `SEM_CHAVE`, `413` sem ler o corpo, `415`, `422` sem chamar o provedor, `429` com `Retry-After` sem ler o corpo, **`404` sem o arquivo da chave de cifra**. **Idempotência:** dois envios com o mesmo id dão uma chamada ao provedor; o mesmo id em duas contas dá duas; o reenvio durante o andamento recebe a mesma resposta, mesmo esperando mais que o `writeTimeout`; com o celular caindo depois da resposta do provedor, o ditado fica gravado e o reenvio sai `200` sem chamada; **a gravação que falha uma vez é refeita no reenvio sem chamada**; **o `SEM_FALA` repetido em 10 min não chama o provedor**; **a lápide dá `410` sem chamada**. **Corridas:** revogar o aceite durante o provedor falso não chama o Polish e não grava; "Apagar meus dados" durante o provedor falso não grava. **Prazos:** um corpo lento no `httptest` dentro do prazo recebe a resposta inteira, e um corpo além do prazo é cortado antes do provedor; um ditado em andamento termina durante o desligamento. **Memória:** 10 envios ao mesmo tempo nunca leem mais de 3 corpos, com o pico medido. O Polish que falha grava o texto do Smart com `mode` `smart`. Canário de áudio e de texto no log de acesso, nos erros e no `/tmp`. `/asr/transcribe` e `POST /transcriptions` continuam `404` | nada |
| **C5c** | **Implementada (11-out-2026):** `a8cf426`. **O Docker e o `scripts\deploy-teste.ps1` não rodaram** (regra do projeto): a parte nova do `.ps1` é provada só por texto, e fica por rodar na máquina do Mauro. Três pontos do roteiro foram escritos pela documentação e se conferem na VPS (C7). | O compose e o deploy: o segredo novo declarado (e no `SEGREDOS_ESPERADOS` do `subir.sh`), o `memswap_limit`, o `ulimits: core: 0` e o `stop_grace_period` de 90 s da API; o `gerar-segredos.sh` criando o `ai-keys-secret`; o `asr_gateway.go` antigo fora, com o servidor em memória inteiro, refeito do zero (escolha menor 19); o roteiro de deploy com o passo da chave de cifra, as migrations (o `subir.sh` já roda o `migrate`) e a leitura da swap e do proxy (passos 10.1 a 10.4) | Casos no `compose_test` para o segredo, o `memswap_limit` igual ao `mem_limit`, o `core: 0` e o `stop_grace_period`; o `subir.sh` recusa sem o arquivo novo, com a mensagem que aponta o passo da C3a; o `roteiros_test` encontra o passo novo | nada |
| **C6a** | **Implementada (10-out-2026):** `227d1b1` (o `Permissions-Policy`), `06b1296` (o gravador e o envio), `543f217` (os testes aguentam a máquina ocupada). **Corrigido em 11-out-2026, `4c1419f`:** o gravador lia o nível por `requestAnimationFrame`, e numa janela que não está sendo desenhada (com a página ainda visível) o navegador não dá quadros: a fala de verdade virava "Não ouvi nada" e o ditado era jogado fora. A leitura passou a um temporizador de 16 ms, com teste sem quadros. **Visto de ponta a ponta em 11-out-2026** contra a nuvem de teste (`cmd/server teste`), no navegador do PC com um tom sintético no lugar do microfone: a folha antes do microfone, a chave guardada mostrando só o final, o ditado no topo do histórico "pelo celular", e a linha `ditado` do log só com contagens (2,8 s, 9.890 bytes, uns 28 kbit/s). Nada foi visto num Android real. | O gravador e o envio: `Ambiente.ditar`, `ClienteNuvem.ditar`; a máquina de estados (parado, pedindo permissão, gravando, enviando, pronto, erro); o `MediaRecorder` em webm/opus a 32 kbit/s; a onda, o tempo e o "Cancelar"; a medição de fala; o Wake Lock; a parada por `visibilitychange`, `mute` e `ended`, com o aviso; o teto; "Tentar de novo" com o mesmo id; o `Permissions-Policy` | Vitest da máquina de estados, de cada código de erro, da parada por tela e por ligação, do reenvio com o mesmo id e da medição de fala (dublês do navegador). O `bundle.test`: o gravador só no build da nuvem, sem `IndexedDB` nem `localStorage`. Teste do servidor: o `Permissions-Policy` sai nas respostas do app. Capturas renderizadas em 320 e 375 px, claro e escuro | nada |
| **C6b** | **Implementada (11-out-2026):** `243d17f`. O cliente do app não falava o JSON que a C3b fixou; foi alinhado na C6-alinhar (`796d17d`, linha abaixo; risco 20). | As Configurações e o aceite: a folha do aceite, antes da chave; "Ditar pelo celular" (o estado, os provedores, as chaves com o lembrete de revogar, o modelo, a dica e o Polish); o texto de Privacidade e os dois testes dele | Vitest: sem aceite, nem o `getUserMedia` nem o pedido da chave acontecem; trocar de provedor reabre a folha; o `SEM_ACEITE` do `PUT` das preferências abre a folha. O `bundle.test` continua sem "Gerenciador de Credenciais" | nada |
| **C6-alinhar** | **Implementada (11-out-2026):** `796d17d`. Fecha o risco 20. **Nada visto num Android real.** | O cliente do app (`cliente.ts`) e o servidor de mentira do app (`servidorFalsoNuvem.ts`) falando o contrato real do `06` (o estado com as chaves em lista e as preferências aninhadas; o aceite `{versao, transcricao, polish?}` com a versão inteira); a versão do aviso só em `VERSAO_DO_AVISO` (inteira); o `AVISO_DESATUALIZADO` recarrega o app; o `vigente: false` reabre a folha; o `colar_de_novo` pede a chave de novo; o aceite do servidor tem dois lugares e cada aceite novo substitui o anterior (o `jaAceitos` saiu); o Polish só liga com modelo | `contratoDoDitado.test.ts` prende os campos e os status do servidor de mentira e do cliente ao exemplo e à tabela de códigos do `06` (lidos por `?raw`); `avisoDoDitado.test.ts` guarda uma impressão FNV-1a do texto que a folha mostra, e quebra se o texto mudar sem a versão. 7 mutações, todas pegas. Conferência visual a 375 px, escuro, com o Polish marcado | nada |
| **C6c** (OK próprio) | **A página e as provas, implementadas (11-out-2026):** `6ae9447`. **A calibração no aparelho continua com o Mauro.** | A calibração: uma página só do `pnpm dev` (como a `telas-dev-nuvem.html`), aberta no Chrome do Android pelo cabo USB (o encaminhamento de porta do `docs/teste-manual-nuvem-f4.md`, §9), com as mesmas restrições do `getUserMedia` e a mesma medição do gravador. Ela mede o piso e os picos em dBFS, a distribuição do RMS, a fala medida e os bytes por segundo do `MediaRecorder` (os pedaços são contados e jogados fora), e **mostra só números**. Nenhum arquivo, nenhum envio | O `bundle.test` prova que a página e o texto dela não estão em nenhum dos dois builds. **A onda e o limiar de fala calibrados com os números do Android, renderizados antes de concluir** | dá o OK, abre a página no celular pelo cabo, fala e fica em silêncio pelo roteiro, e passa os números (ou uma captura da tela) |
| **C7** | **No ar (11-out-2026); falta o aparelho.** **O deploy, feito em 11-out-2026** (autorizado pelo Mauro): o ensaio no Docker do PC passou inteiro (as 19 migrations, as rotas do ditado montadas, o `Permissions-Policy` pela borda, o backup decifrado e restaurado); as imagens foram pela `ssh`; a chave de cifra foi criada na VPS pelo `criar-chave-de-cifra.sh`, sem mostrar o valor; o `subir.sh` aplicou a 0017, a 0018 e a 0019 e subiu a API. **As leituras de antes do primeiro áudio, sem nenhum PARE:** o Traefik sem prazo curto nem middleware no entrypoint; o `docker info` sem o aviso de swap; a API com `Memory` igual a `MemorySwap` (256 MB) e o `core` em 0; o `memory.swap.max` do contêiner em `0`; e a VPS **sem swap nenhuma** (`/proc/swaps` vazio). De fora: as rotas novas respondem `401` sem sessão, e a conferência do PC passou. **Falta:** o OK do Mauro na política e no dossiê (D0), a chave dele no app, o aceite e os testes no Android. | No ar e no aparelho, com a autorização do Mauro: o roteiro de deploy (o passo da chave de cifra, as migrations, o compose); a leitura só de leitura do proxy e da swap; a chave da OpenAI dele colada no app; o aceite | **Antes do primeiro áudio** (passos 10.3 e 10.4 do `docs/deploy-f11.md`, só leitura, parando em qualquer PARE): o `readTimeout` do entrypoint em `0` ou acima de 30 s, o `writeTimeout` em `0` ou de pelo menos 90 s e nenhum middleware de buffering; o `docker info` sem "No swap limit support"; `HostConfig.MemorySwap` igual a `Memory` e o `ulimits` com `core` em `0`; e o `memory.swap.max` do contêiner em `0` (senão, para). **Depois, no Android:** um ditado curto, um de 30 s, um de 120 s (com os bytes medidos contra o teto de 1 MiB), um silêncio (sem envio), sem rede (o "Tentar de novo"), a tela apagando (o Wake Lock), a troca de app no meio e uma ligação recebida. A mediana do toque final até o texto, com a meta de até 4 s, a confirmar. O `docker stats` da API sem salto. O ditado chega ao PC (com a S, em segundos). Uma chamada de teste, pelo mesmo caminho de código, confirma o português, os campos do modelo escolhido e o envio com `Content-Length` | autoriza e roda o deploy pelo roteiro; gera a chave de cifra; cria uma chave só para o celular, com limite de gasto, e a cola no app; liga a residência de dados e o ZDR, se a conta tiver; aceita; roda os testes do aparelho |
| **C8** (opcional, OK próprio) | **Pendente.** | A medição de qualidade **sem arquivo de áudio**: o Mauro lê no app, pelo caminho real, um roteiro de 15 a 20 falas curtas e longas, cada uma duas vezes (com e sem a dica, alternando); o texto vem do histórico e é comparado com o roteiro pelas métricas do `internal/benchmark/metrics` (as do `asrbench` e do `entitybench`) | O WER e a Entity Accuracy do celular; se a dica ajuda ou induz (e se ela passa a vir ligada); se o `gpt-transcribe` ganha do `gpt-4o-mini-transcribe` em pt-BR | autoriza mandar essas falas ao provedor e lê o roteiro |

**A ordem:**
1. A S vem primeiro, com o OK dela. A LG corre em paralelo, com o OK dela.
2. A D0 vem antes de qualquer código do ditado.
3. Depois, a C1, a C2, a C3a, a C3b, a C4 e a C5a, que não dependem umas das outras (a C3b usa a
   migration da C3a só na ordem dos números).
4. A C5b junta as anteriores. A C5c vem depois dela. **[Fase 2, 11-out-2026: a C3-correções e a
   C6-alinhar entraram na mesma rodada, em paralelo com a C5b e a C5c, para fechar as pontas que a
   fase 1 deixou (riscos 18, 19 e 20).]**
5. A C6a e a C6b dependem da C3b e da C5b. A C6c pode vir junto da C6a, com o OK dele.
6. A C7 vem por último, depois do OK da D0. A C8 vem depois da C7, se o Mauro quiser.

## O que a implementação fixou (a fase 1, em 10 e 11-out-2026)

O que cada subfatia decidiu dentro do que esta ADR deixou para ela, ou fez diferente do texto acima, e o
que ela provou e **não** provou. O que já está marcado no corpo da ADR (os números da C2, os nomes e os
status da C3a e da C3b, a C4) não se repete aqui. Em divergência com o código, vale o código.

**S (a chegada ao PC).**
- `GET /api/nuvem/mudancas` responde `{"contador": N}` (o nome do campo é da S, para não confundir com o
  `historico.mudancas` do `GET /api/nuvem`); `503` `UNAVAILABLE` sem o laço; um Agent de antes da S
  responde `404`, e o painel para de perguntar.
- O contador sobe **uma vez por rodada** que gravou algo da nuvem, **também com o que o push traz** (a
  resposta com lápide, "apagar vence", e o conflito com o estado vencedor), e não só com o pull, como a
  decisão P dizia. É um superconjunto: reler a lista a mais é barato, e deixar de reler seria um defeito
  visível.
- **A abertura do painel adianta a busca.** A primeira leitura depois de uma janela de 15 s sem leitura
  acorda o laço, e a próxima busca sai 10 s depois da última (ou já, se isso passou). Sem isso, a edição
  do celular só chegaria em até 60 s na primeira vez em que o painel abre, e o alvo de cerca de 13 s não
  valeria. Não fura o backoff depois de uma falha nem gira a cada 10 s desconectado.
- O laço passou a criar um timer por volta, por um "despertador" injetável (o padrão é o `time.Timer`),
  no lugar do `timer.Reset`, para o teste com relógio falso; é refactor interno sem mudança de
  comportamento.
- **Uma janela conhecida:** uma mudança que chega nos milissegundos entre a primeira carga da lista e a
  primeira leitura do contador só aparece na mudança seguinte ou ao voltar à aba; as leituras seguintes
  não têm essa janela.
- A alternativa "30 s sempre" é `IntervaloComPainel` igual a `Intervalo`, em 30 s, no `configDoSync`; o
  contador e o painel não mudam. O `bundle.test` do build local passou a exigir a rota nova.

**C1 (`Store.Create` e `Store.Situacao`).**
- O `Store.Situacao` devolve também o ditado e o epoch da conta (`SituacaoDoId`: `Estado`, `Ditado`,
  `Epoch`), lidos num snapshot só (`REPEATABLE READ` somente leitura no Postgres), para a C5b ler o epoch
  esperado de forma consistente com o que existe. Ele trata como lápide a que venceu e que o job de
  retenção (de hora em hora) ainda não apagou: a resposta conservadora é `410`, e não pagar de novo.
- O `Store.Create` recusa um `Actor` com aparelho (`ErrDeviceNotAllowed`) e só aceita `source_platform`
  `web` ou `android` (`ErrInvalidDictation`). O `device_id` fica nulo também no app nativo, então o
  `Actor` do passo 2 também se monta sem aparelho. Id com lápide: `ErrTombstoned`; epoch velho:
  `ErrEpochChanged`. O `NewDictation` vira o payload de criar do push e passa pela mesma validação, sem
  cópia das regras; um pré-teste de UTF-8 inválido existe porque o `json.Marshal` trocaria o byte
  inválido por U+FFFD em silêncio.
- **O storage do PC passou a considerar a origem em "nasceu aqui"** (`internal/storage/origem.go`,
  `envio_sqlite.go`, `esquecer_sqlite.go`, `transcription_sqlite.go`). O PC tratava `device_id` nulo
  como "nasceu neste PC" em três SQLs (o envio inicial, o "Esquecer esta nuvem" e a fila do sync), e o
  ditado do app chega sem aparelho e, depois do esquecer, sem `server_version`: ele entraria no envio
  inicial como histórico deste PC e iria para uma conta nova. Agora "nasceu aqui" é `device_id` nulo
  **e** `source_platform` fora de `web` e `android` (um filtro só, `nasceuAquiSQL`). As listas das
  origens moram em dois lugares (`cloudstore.SourcePlatform*` e `storage.Plataforma*`, porque o storage
  do PC não importa o servidor), e `TestOrigensDaNuvemConferemEntreOPCEOServidor` confere os nomes: uma
  origem nova (por exemplo `ios`) entra nos dois.
- "Pelo app" aparece na linha de detalhes do ditado aberto (`web/src/ItemDitado.tsx`), nos dois builds,
  para `web` e `android`; o do PC não diz nada.
- **Não feito:** o teste de mutação da conferência do epoch (o classificador de permissão negou a
  edição que a enfraquecia). Os testes de epoch são afirmativos, e a corrida real contra a trava do
  contador está em `TestPostgres_Criar_OEpochTrocadoEnquantoEsperaATrava` e
  `TestPostgres_Criar_JuntoComOApagarMeusDados`. Um backup restaurado também troca o epoch e recusa do
  mesmo jeito, sem teste específico.

**C2 (`internal/smartconta`).** Além do que está no corpo: o `Cache` e o `Conta.Resolver` (`Colapsar` e
`Resolve`) vão além do mínimo pedido, mas a ADR os descreve; o teste de tempo (`desempenho_test.go`) pula
sob `-short` e com o detector de corrida, com tetos folgados de propósito (25 ms para resolver; 10 s só
como alarme para montar). O `go test -race` não rodou (a máquina não tem compilador C): convém rodar o
`Cache` e o `TestConta_UsoConcorrente` no CI Linux.

**C3a e C3b.** Além do corpo:
- Até a C3b, o `serve` subia com o aceite em falha fechada (a interface `server.AceiteDoDitado`, com o
  `Aceite` nulo: ninguém tem aceite). **[C3b]** O `CloudConfig` ganhou o campo `Ditado`
  (`cloudstore.DitadoNoCelular`), obrigatório junto com o `Cofre`; o `Aceite` ficou como sobrescrita só
  para testes, e, nulo, o aceite é o `aceitePeloStore` sobre o `Ditado`. A suíte `chavestest` ganhou os
  casos `Ditado_*`.
- Motivo novo do log de acesso: `aviso` (no `AVISO_DESATUALIZADO`); contagem `chaves_apagadas` no
  `DELETE` do aceite. Todas as frases de erro são fixas.
- **Provas que não rodaram:** os testes de mutação do aceite, do limite e do AAD foram negados pelo
  classificador de permissão (só a da frase fixa foi exercida, e pegou); esses testes são afirmativos
  (contam as chamadas ao provedor e conferem `ErrNaoDecifra`).
- O teste de tempo dos pacotes pesados (`deploy`, `internal/storage`, `internal/agent/audio`) é sensível a
  carga; ver o `13`, TASK-028, "Fica aberto".

**C5a (`internal/polish`).**
- O pacote leva também a composição do antigo `polishDoAgent.abrir` (`Montagem.Abrir`), porque o
  `clienteDoDitado` tem campos não exportados e o servidor da C5b não conseguiria montá-lo de fora. Os
  nomes são escolha da C5a: `Prazo`, `PisoPrazo`, `PoliticaEsforco`, `NovaPoliticaEsforco`, `Atual`,
  `Recusou`, `RotuloDoEsforco`, `Montagem` e `Abrir`.
- **Os testes antigos passam sem nenhuma edição, como a prova pedia.** Isso exigiu dois arquivos
  `_test.go` novos que guardam os nomes antigos só na compilação dos testes
  (`cmd/agent/polish_paridade_test.go` e `internal/agent/winruntime/polish_paridade_test.go`); o código
  de produção do Agent não ficou com nenhum repassador. Se for melhor editar os testes antigos para os
  nomes novos, é uma troca mecânica de poucas linhas.
- A explicação longa de por que o prazo cabe no watchdog continua em `internal/agent/winruntime/polish.go`
  (é do runtime do Agent); a de por que ele cresce com o texto foi para `internal/polish/prazo.go`.
- **Provas:** o pacote compila e testa em Linux; o canário de chave e de texto fora de log e de erro
  existe também em `internal/polish` (`TestAbrir_NadaDeSegredoNemDeTextoSaiPeloAdaptador`), além do
  `TestChave_NuncaSaiDoPolishDoAgent` em `cmd/agent`, que continua. O binário de teste compilado para
  Linux rodou também no WSL, só com leitura do drive. A memória da política de esforço é por processo,
  e não por conta.
- **Não provado:** o Polish do PC de verdade, que é o teste do Mauro.

**C6a (o gravador e o envio).**
- O `Ambiente.ditar` é um **componente** (`ComponentType<PropsDoDitar>`), e não uma função como o
  `excluirDeVez`: o gravador (`getUserMedia`, `MediaRecorder`) não pode morar no `Painel` compartilhado,
  ou entraria no bundle local, que o `bundle.test` proíbe. O `Painel` só recebe o campo de busca (para
  pôr ao lado do microfone) e devolve ganchos (`aoChegar`, `aoAudioPendente`, `aoSemSessao`,
  `abrirConfiguracoes`, `avisar`).
- **`401` durante o envio:** o áudio conta como texto não salvo (o `Painel` não troca a tela inteira por
  "Sessão expirada"); a faixa da sessão (`EntrarDeNovo.tsx`) fala do ditado, não oferece "Copiar o
  texto" de um áudio e pergunta antes de a página sair. A frase da nota é "O ditado espera você entrar
  de novo.".
- O ditado chega no topo com destaque de 10 s (o fundo `--entrou` e a etiqueta "novo"; em tema de alto
  contraste não há timer, e a etiqueta carrega o sentido). O microfone fica à vista também na Lixeira
  (só some nas Configurações, e volta se houver gravação ou áudio à espera); lá, e numa busca, o ditado
  não entra na lista e o aviso diz "Ditado salvo no histórico.".
- O gravador desiste quando o `MediaRecorder` não entrega nenhum byte: "A gravação falhou. Tente de
  novo.", sem enviar corpo vazio. Defesa que a ADR não pedia.
- O ESLint proíbe `src/` importar `src/nuvem`; por isso a `Plataforma` de mentira mora em
  `src/nuvem/ditar/plataformaFalsa.ts`, e não em `src/testes`.
- **Não provado, só num Android real (C7):** o `AudioContext` que nasce suspenso (o app diria "Não ouvi
  nada." com a pessoa falando; o contexto nasce no gesto do toque e leva `resume()`), o `mute` da faixa
  disparando sem querer no início, o `tracks.stop()` logo depois do `recorder.stop()` sem perder o último
  pedaço (há um prazo de 3 s se o `stop` nunca vier), o `Blob` paginado em disco, os bytes reais a
  32 kbit/s, o Wake Lock, a tela apagando e o `visibilitychange`. As capturas da entrega usam uma fala
  sintética com o relógio parado, e não um microfone.

**C6b (as Configurações e o aceite).**
- O aceite novo **mantém** os provedores que já valiam (a união do da voz e do do Polish): trocar de
  volta para um provedor já aceito na versão atual não reabre a folha, como o servidor confere.
  **[Corrigido na C6-alinhar, 11-out-2026: isto era o app acumulando provedores (`jaAceitos`), e o
  servidor não faz assim. O aceite do servidor tem dois lugares (`transcricao` e `polish`) e cada
  aceite novo **substitui** o anterior. O `jaAceitos` saiu: a folha grava só o que mostrou. Trocar a voz
  da OpenAI para a Groq tira a OpenAI do aceite, e voltar a ela pede a folha de novo.]**
- O texto da folha, da Privacidade e das Configurações segue a proposta da D0 (`feaaa12`); os blocos
  por provedor vêm de lá, e o da Anthropic ganhou "visto em 29-set-2026, e não relido em 10-out-2026" e
  "não afirma em que país". Entraram as duas linhas novas da Privacidade (6 meses, lixeira de 30 dias,
  cópia cifrada de cerca de 30 dias) e a frase do "Apagar meus dados" (aceite, preferências e chaves).
  O "Política completa: {link}" ficou de fora, porque a política não está publicada. **O texto da folha
  tem de continuar batendo com `docs/politica-de-privacidade.md`**, e qualquer mudança dele sobe a versão
  do aviso nos dois lados.
- A infra de teste ganhou `web/src/testes/preparar.ts` (em `test.setupFiles`), um `testTimeout` de 20 s e
  um `asyncUtilTimeout` de 5 s no `vite.config.ts`, e timeouts explícitos nos testes mais lentos: a
  máquina ocupada derrubava a suíte por lentidão.
- A página de calibração foi renomeada de `Calibracao.tsx` para `PaginaDeCalibracao.tsx` (e o teste):
  `calibracao.ts` e `Calibracao.tsx` só diferiam pela caixa, e no Windows um import resolvia o `.ts`.

**C6c (a calibração).** A página é `/calibracao-dev.html`, só do `pnpm dev`, aberta no Chrome do Android
pelo cabo (a porta 5173 do Vite, no encaminhamento do `docs/teste-manual-nuvem-f4.md`, §9, e não a
8080). Escolhas que a ADR não fixava: silêncio de 6 s e fala de 12 s; o piso é o p5 e o pico é o máximo;
percentis por posição; bandas de 6 dB de -96 a 0 dBFS; limiares de RMS de 0,003 a 0,2, com o 0,03 do app
marcado; pedaços do `MediaRecorder` de 1 s (`start(1000)`) só para contar bytes por segundo (o gravador
do app não pica); RMS zero vale -120 dBFS. A página também mostra o eco, o ruído, o ganho e a taxa que o
navegador diz ter aplicado, e o ritmo de 120 s contra o teto de 1 MiB. **Falta, e é do Mauro:** com os
números do aparelho, fixar o limiar de fala, o piso e o teto da onda, a janela dela e as restrições do
`getUserMedia` (`restricoesDoMicrofone`, em `navegador.ts`), e renderizar a onda antes de concluir.

## O que a implementação fixou (a fase 2, em 11-out-2026)

A C3-correções, a C6-alinhar, a C5b (com a revisão adversarial) e a C5c. Vale o mesmo critério da fase 1:
o que já está marcado no corpo da ADR não se repete, e em divergência com o código vale o código. O
contrato da rota está no `06`, "Ditar pelo celular"; a privacidade, no `09`, "Ditar no celular".

**C3-correções (`6925ecc`).** Fecha os riscos 18 e 19. Os detalhes estão nos riscos; o que mais ela fixou:
- A interface `cloudstore.ChavesDeIA` ganhou `EpochDaConta` e `GravarChaveDeIAConferindo(ctx, userID,
  CondicaoDaChave{Epoch, ExigeAceite}, chave, agora)`. O `GravarChaveDeIA` antigo manteve a assinatura
  (gravação crua, **só para montar testes; não usar em rota**), para não quebrar as frentes paralelas.
  Quem tiver um dublê próprio da interface precisa implementar os dois métodos novos.
- A gravação precisa das **duas** travas, a da linha do usuário e a do contador, nessa ordem: o revogar e
  o aceitar travam só a linha do usuário (sem ela a gravação leria o aceite ainda em aberto durante um
  revogar), e o apagar trava só o contador (sem ele o apagar passaria). Cada uma tem uma mutação que a
  pega.
- `Aceite.Cobre(provedor)` (`Vigente()` e `Inclui(provedor)`) é a única conferência de uso: o
  `aceitePeloStore` e a gravação da chave a usam, para a rota e a gravação não divergirem. Com o dublê de
  aceite dos testes (`CloudConfig.Aceite`) a gravação reconfere só o epoch.
- O epoch mudado responde `409` `CONFLICT` (código geral do contrato, fora da lista fechada do ditado),
  com o motivo de log novo `conta_mudou`, e não um código novo.
- **Achado fora da lista e corrigido:** o deadlock da C3b entre o `RevogarAceite` e o
  `ApagarDadosDaConta`. O revogar trava o aceite e depois apaga as chaves; o apagar apagava as chaves
  antes dos aceites. Com um aceite em aberto e uma chave, os dois se esperavam (`SQLSTATE 40P01`) e o
  Postgres abortava o apagar. A correção é a ordem do `apagarDaConta` (os aceites antes das chaves), em
  `pgstore/contas.go`, com `TestPostgres_RevogarEApagarNaoSeTravam`.
- O servidor de teste do PC (`logindeteste`) monta o ditado com um cofre por processo e provedores de
  mentira; uma chave que começa com `recusada`, `sem-credito` ou `fora-do-ar` simula a recusa, a falta
  de crédito e o provedor fora do ar. A janela do risco 16 (a revogação no meio do Polish) segue valendo
  para a rota de ditar; só a das chaves foi fechada.

**C6-alinhar (`796d17d`).** Fecha o risco 20. O que fixou além do corpo:
- O aceite do servidor tem dois lugares (`transcricao` e `polish`), e **cada aceite novo substitui o
  anterior**; por isso o `jaAceitos` saiu do `PedidoDeAceite` (veja a correção na C6b, acima).
- **O Polish só liga com modelo** (`ErrPolishSemModelo`: o servidor não tem modelo padrão do Polish), e a
  C6b ligava com o modelo vazio. A tela guarda o Polish marcado como **rascunho** (provedor, chave e
  modelo aparecem sem escrever na conta); o `PUT` com o Polish ligado sai quando o modelo é escolhido, e
  trocar o provedor do Polish zera o modelo e deixa a conta com o Polish desligado até novo modelo.
- O modelo da voz: a conta guarda sempre o nome de verdade (o servidor resolve o vazio do `PUT`), então o
  seletor mostra o nome do padrão marcado e **"Padrão do provedor" nunca fica marcado** depois do `PUT`.
- **A versão do aviso** é a `VERSAO_DO_AVISO` inteira (`avisoDoDitado.ts`), a única do app. O texto que
  a folha mostra tem uma impressão FNV-1a de 64 bits no `avisoDoDitado.test.ts`; **qualquer ajuste de
  texto da folha (inclusive para bater com a política) exige atualizar a impressão, e subir a versão se
  alguém real já aceitou.**
- `AVISO_DESATUALIZADO` fecha a folha sem aceite e **recarrega a página**; um ditado gravado e não enviado
  que estava na memória se perde (decisão do Mauro, na C7: a alternativa é uma frase com o botão
  "Recarregar" quando há áudio pendente). O `aceitarAviso` devolve `ErroDaApi` `502` se o `200` não
  trouxe o aceite gravado (antes inventava um aceite local).
- `web/vite.config.ts`: `'../docs/ai-spec'` entrou no `server.fs.allow` (como o `'../docs/marca'`), só
  para o servidor de desenvolvimento ler o `06` por `?raw`. Nenhum build é tocado.
- Dois status do `erros.test.ts` estavam errados em relação ao `06` (`SEM_CHAVE` é `409`, e
  `PROVEDOR_LIMITE` é `502`) e foram corrigidos; o `413` (corpo acima de 16 KiB) vale como chave inválida.

**C5b (`d8e03b9`, e a revisão `ca2d21f` e `1eea4b9`).** O que a rota fixou além do corpo e do `06`:
- **O acesso e a ordem.** `AcessoDitar` só aceita a sessão (o token de um PC é `403`, sem credencial é
  `401`), passa pelo CSRF e só existe com a chave de cifra. A ordem é a do `06`: o aceite, as chaves
  (sem decifrar), o `Ditado-Id`, `Store.Situacao`, a memória, as medidas, o `Content-Type` e o tamanho,
  as vagas, e só então o corpo.
- **Os status** estão no bullet **[C5b]** do bloco "Os status já fixados". **O teto da duração é de
  125 s** (`413`). **O `Content-Type` `audio/webm` é exigido** (`415` antes de ler), e o tipo de verdade
  segue vindo dos primeiros bytes (EBML e DocType `webm`; Matroska é recusado). **O `Ditado-Id` é UUID
  v7** (versão 7 e variante 8 a b; maiúsculas viram minúsculas). Cada cabeçalho tem um valor só.
- **O `aviso`** da resposta é um `resolver.MotivoFallback`, e a ADR e o app diziam `ia.Motivo`. O
  reenvio que esperou o mesmo id em andamento recebe `200` com o `aviso` do primeiro (o andamento
  guarda só o desfecho, nunca o texto: a resposta é relida do `Store`); o pago regravado é `201`.
- **A versão do Smart** no `smartconta.Cache` é o resumo SHA-256 dos pares (veja "O Smart da conta e a
  dica"), e não o `last_seq`.
- **O registro dos modelos conhecidos** (`modelos_conhecidos.go`): por processo, global por provedor, com
  o teto de 2000 nomes. Ele alimenta a validação do modelo do Polish (modelo fora da lista: o Polish nem
  é chamado, aviso `modelo_inexistente`) e decide se a linha `ditado` escreve o nome do modelo ou
  `outro`. Alimentado pelo `PUT` da chave, pelo `GET` dos modelos e por uma pergunta à lista no próprio
  ditado, com a chave do Polish e prazo de 3 s.
- **Os prazos escolhidos** estão em "Prazos" (**[C5b]**): o Smart 5 s mais 5 s, a lista de modelos 3 s, o
  teto do trabalho solto de 44 s, a espera do mesmo id em 80 s, o `Retry-After` de 5 s, e o corpo contado
  da vaga.
- **O que o Polish faz quando o resto falha:** uma falha do banco na conferência antes do Polish não
  descarta o ditado (o Polish não roda, com o aviso `transporte`, e a gravação, que confere tudo de novo,
  decide); o Smart que falha (sem o aprendido, ou a montagem atrasada) leva o Polish a não rodar, com o
  aviso `dicionario` (falha fechada, como o PC quando a consulta local falha).
- **O "Apagar meus dados" e o "Desligar o ditado"** esquecem a memória da conta na rota (o Smart montado e
  o texto pago e não gravado), além do epoch que já invalidava o pago. No desligamento do servidor, o
  pago não começa reenvio novo (`OCUPADO`). O trabalho solto é `context.WithoutCancel(r.Context())` mais
  o teto; nada nele chama `anotarMotivo` nem `anotarContagens` (a linha de acesso é do handler). Um
  pânico no trabalho vira `500` e a linha `ditado` com o desfecho `panico`, sem o valor do pânico.
- **O risco 16 segue aceito** (veja o risco). `smartconta` mudou além do consumo: o que o Polish precisa
  (o pedido e o resultado do Smart) ficou num tipo novo, `Resolvido`, com `ResolverParaPolir`, `Smart()`
  e `Reescrever`; `Resolver` delega a `ResolverParaPolir`, e a suíte do `smartconta` passou sem mudança.
- **A revisão adversarial achou dois defeitos reais.** (1) `ca2d21f`, a cobrança dupla numa corrida: o
  reenvio lia o `Store.Situacao` como "nada" enquanto o primeiro estava no provedor; se o primeiro
  gravava e saía da memória antes de o reenvio olhar a memória, o reenvio pegava a vaga, lia o corpo e
  pagava o provedor de novo (o `Create` só devolvia o ditado do primeiro) e ainda gastava uma ficha. A
  correção tem duas partes: quem pega a vaga **relê o `Store.Situacao`** já como dono do id (enquanto um
  pedido é dono, ninguém grava aquele id), e o `entrar` devolve o andamento `olharDeNovo` quando há um
  desfecho na memória; o novo `desistir` devolve a vaga e a ficha (a resposta idempotente não conta). O
  custo é uma leitura a mais no banco por ditado novo, antes de o prazo do corpo começar. (2) `1eea4b9`,
  o Smart da conta de volta no cache: o "Apagar" ou o "Desligar" feito durante a chamada ao provedor
  esquecia o Smart, mas o trabalho, que tinha lido o aprendido antes, o montava de novo depois e o
  guardava no cache, onde ficava até a conta ditar outra vez ou o LRU tirar (só RAM, nada em disco nem em
  log, mas a promessa do esquecer não valia). Esse ditado sempre acaba em `SEM_ACEITE`, e por isso o
  trabalho chama `smart.Esquecer` quando termina assim. O `(*Cloud).ContasComOSmartNaMemoria()`, só para
  testes, devolve a contagem do cache. **Conferido e sem problema:** o token de um PC dá `403`; o mesmo
  id em duas contas não cruza (a chave é `(user_id, id)`, e a memória é por usuário); o Polish só roda
  com o aceite do provedor dele; nenhum middleware lê o corpo antes das conferências; o corpo sem
  `Content-Length` ou `chunked` passa pelo `MaxBytesReader`; nenhum arquivo temporário; o modelo da
  conta no log passa pelo `paraOLog`; o mapa de andamentos é limitado pelas vagas mais os pagos.
- **Provas:** `internal/server/chavestest` (os casos `Ditar_*`, na memória e no Postgres) e o
  desligamento em `cmd/server/servir_test.go`. Uma mutação não era pega por nenhum caso (a conferência do
  aceite antes do Polish: as chaves somem junto com o revogar e o apagar) e ganhou o caso
  `Ditar_Corridas_AceiteSemOPolishNoMeio`. **O `go test -race` não rodou** (sem compilador C).

**C5c (`a8cf426`).**
- **O compose:** `AI_KEYS_SECRET_FILE=/run/secrets/ai-keys-secret` é obrigatório, então as rotas
  `/ai-keys`, `/dictation` e `/dictations` existem sempre que a API sobe pelo compose, e o `404` delas
  fica só para o binário rodado sem o arquivo (a escolha menor 25 está cumprida). `memswap_limit` igual
  ao `mem_limit`, `ulimits: core: 0` e `stop_grace_period: 90s`. **O `stop_grace_period` está amarrado
  por teste aos dois números do `cmd/server/serve.go`** (`shutdownTimeout` de 15 s e `esperaDosDitados`
  de 70 s, lidos por expressão regular): exige `90 s` e `>= 15 s + 70 s`, de modo que mudar um dos três
  quebra o teste.
- **Os scripts:** o `gerar-segredos.sh` **cria** o `ai-keys-secret` numa VPS nova, e o
  `criar-chave-de-cifra.sh` recusa nesse caso (um teste prova que não muda nada nem mostra o valor). O
  `subir.sh` recusa a falta do `ai-keys-secret` à parte, apontando o `criar-chave-de-cifra.sh`, e **já
  roda o `migrate`** (veja "O aceite e as preferências", as migrations). Os valores de parada do roteiro,
  passos 10.3 e 10.4 do `docs/deploy-f11.md`: `readTimeout` do proxy `0` ou acima de 30 s;
  `writeTimeout` `0` ou de pelo menos 90 s; `memory.swap.max` do contêiner em `0`; `HostConfig.MemorySwap`
  igual a `Memory`. **O limiar de 90 s do `writeTimeout` é a soma dos prazos do servidor com folga (30 s
  de corpo mais 44 s de trabalho): escolha da C5c, a confirmar com o Mauro.**
- **O servidor antigo** saiu inteiro (veja "Alternativas descartadas"). No `auth.go` ficaram só
  `Principal`, `PrincipalKind`, `principalCtxKey` e `principalFromContext`, que o `Cloud` usa; o
  `SessionStore`, o `InMemorySessionStore`, o `bearerOrCookieToken`, o `requireSession` e o cookie
  `aurora_session` saíram. O `doc.go` foi reescrito (descrevia a F1-D como corrente) e dois comentários do
  `httpx.go` mudaram. O `deadcode` (`golang.org/x/tools/cmd/deadcode`) rodou com `go run`, só como
  ferramenta de análise, sem tocar no `go.mod` nem no `go.sum`. Ficam, como API de apoio a testes,
  `Cloud.Rotas`, `Cloud.ArrumarSempre`, `Acesso.String` e `Cloud.ContasComOSmartNaMemoria`.
- **O que cobre agora o que os testes apagados cobriam** (AC-028, AC-035, AC-041, AC-042, NFR-012 e as
  rotas de saúde): está na nota de 11-out-2026 do `docs/fase-1-cobertura.md`.
- **O teste do PC (`scripts\deploy-teste.ps1` e `deploy/pc_parte2_test.go`)** entrou no escopo sem estar
  na lista: o compose novo declara o segredo com `file:`, então sem o `ai-keys-secret` o teste do PC
  quebraria no `docker compose up`. Ele grava o arquivo de mentira (64 hexadecimais) e confere que
  `GET /api/v1/dictation` sem credencial dá `401` (e não `404`, o que provaria que a chave de cifra não
  chegou) e que o `Permissions-Policy: microphone=(self)` chega pela borda uma vez só. **Não rodou** (não
  se roda Docker aqui); só o texto é provado. O `memswap_limit` e o `ulimits` não são conferidos ali de
  propósito: o kernel do Docker Desktop pode descartar a swap e daria um falso `FALHOU`.
- **Não conferido em máquina real** (a C7 lê na primeira vez): o formato de `docker inspect ...
  {{json .HostConfig.Ulimits}}` (`[{"Name":"core","Hard":0,"Soft":0}]`) e que um limite de swap
  descartado pelo Docker aparece como `MemorySwap` diferente de `Memory` (`-1`); que `docker info 2>&1 |
  grep -i "swap limit"` pega o aviso (o Docker o escreve em `stderr`); o caminho do `memory.swap.max`,
  que o comando acha com `find` pelo id do contêiner (cgroup v2; em cgroup v1 o roteiro manda comparar
  `memory.memsw.limit_in_bytes` com `memory.limit_in_bytes`).
- O `Permissions-Policy: microphone=(self)` já saía em toda resposta (`internal/server/seguranca.go`,
  `227d1b1`) e passou a constar na lista de cabeçalhos de segurança do `06`.

## Escolhas menores, a confirmar pelo Mauro na entrega

Cada uma vai implementada com o padrão abaixo até ele dizer o contrário.

1. **O modelo padrão da OpenAI:** o `gpt-transcribe`, se a chamada de teste em pt-BR da C7 for boa; se
   não, o `gpt-4o-mini-transcribe`, o mais barato, com português no FLEURS. A lista vem do provedor, e
   a pessoa troca. **[C3b, 11-out-2026: até a C7, o padrão implementado é o `gpt-4o-mini-transcribe`.]**
2. **O modelo padrão da Groq:** o `whisper-large-v3-turbo`.
3. **O teto de duração:** 120 s, como no PC. A alternativa é 60 s ou 300 s. **[C5b, 11-out-2026: o
   servidor recusa acima de 125 s no `Ditado-Duracao-Ms` (os 120 s mais 5 s de folga para o
   cabeçalho); a confirmar com o Mauro na C7.]**
4. **Os limites do ditar:** 1 de cada vez por conta, 10 por minuto, 200 por dia e 3 ao mesmo tempo no
   total.
5. **Os prazos:** corpo em 30 s, transcrição em 20 s, Polish no `prazoPolish` do PC (3,5 s a 8 s),
   gravação em 5 s, resposta até 45 s depois do corpo e 90 s no celular. A alternativa para o Polish
   é um prazo fixo de 10 s. **[C5b, 11-out-2026: a rota escolheu os que faltavam (o Smart 5 s mais 5 s,
   a lista de modelos 3 s, o teto do trabalho solto de 44 s, a espera do mesmo id em 80 s, o
   `Retry-After` de 5 s, o corpo contado da vaga); estão em "Prazos". A confirmar com o Mauro na C7.]**
6. **O corpo:** 1 MiB, com o opus a 32 kbit/s (a alternativa é 24 kbit/s).
7. **A fala mínima:** 300 ms, sem contar os últimos 200 ms. Os limiares do silêncio do provedor: 0,6 e
   -1,0.
8. **A dica do aprendido:** **desligada por padrão até a C8 medir**, com 140 runas, do mais recente ao
   mais antigo. A alternativa é ligada desde o começo, com a folha do aceite dizendo isso.
9. **O Polish no celular:** desligado até a pessoa ligar.
10. **O `getUserMedia`:** o cancelamento de eco, a supressão de ruído e o ganho automático no padrão do
    Chrome até a calibração da C6c.
11. **A chave de cifra sem cópia fora da VPS:** perder a VPS faz a pessoa colar a chave de novo. A
    alternativa é guardar uma cópia no gerenciador de senhas, como o pepper.
12. **Desligar o ditado no celular apaga as chaves da nuvem.**
13. **`source_platform`:** `web` no app e `android` no app nativo.
14. **O "Copiar":** o botão. Copiar sozinho ao chegar só se o aparelho deixar, a conferir na C6a.
    **[C6a: não foi feito; fica para conferir no aparelho, na C7.]**
15. **O "Cancelar"** durante a gravação descarta sem enviar.
16. **O áudio que falhou** fica em memória até "Tentar de novo" ou "Descartar", e some ao fechar o app.
17. **O intervalo do sync (S):** 10 s com o painel aberto e 60 s fechado (recomendado), ou 30 s sempre.
    **[Aprovado pelo Mauro em 10-out-2026: 10 s com o painel aberto e 60 s fechado. Implementado na S.]**
18. **`Permissions-Policy: microphone=(self)`** nas respostas do app.
19. **O `asr_gateway.go` antigo** sai na C5c: integrar antes a branch que remove o servidor antigo da
    F1-D (recomendado, porque o trabalho já está feito), ou refazer a remoção. **[C5c, 11-out-2026:
    cumprida. Ele saiu inteiro, junto do servidor em memória da F1-D, e a remoção foi refeita do zero,
    sem a branch (o repositório é público e não carrega branch nem commit antigo). Veja "Alternativas
    descartadas" e "O que a implementação fixou (a fase 2)".]**
20. **Os nomes** **[derivada]**: as rotas (`/dictations`, `/dictation`, `/ai-keys`), o `AcessoDitar`, o
    `Store.Situacao`, as tabelas (`ai_provider_keys`, `dictation_settings`, `dictation_consents`), o
    segredo (`AI_KEYS_SECRET_FILE`), os rótulos do HKDF, os cabeçalhos (`Ditado-Id`,
    `Ditado-Duracao-Ms`, `Ditado-Fala-Ms`) e os códigos novos (`SEM_ACEITE`, `SEM_CHAVE`, `UM_POR_VEZ`,
    `LIMITE`, `OCUPADO`, `FORMATO`, `SEM_FALA`, `PROVEDOR_RECUSOU_CHAVE`, `PROVEDOR_LIMITE`,
    `PROVEDOR_FORA`, `APAGADO`).
21. **O aceite que pede de novo:** trocar de provedor, ligar o Polish com um provedor novo ou mudar o
    texto do aviso. Mudar só o modelo não pede.
22. **O desligamento:** o `serve` espera os ditados em andamento até 70 s depois do `Shutdown`, com o
    `stop_grace_period` da API em 90 s. A alternativa é deixar os 15 s e 30 s de hoje, aceitar o risco
    e não ditar durante o deploy. **[C5b e C5c, 11-out-2026: feito. O `serve` espera os trabalhos
    soltos depois do `Shutdown`, e o `stop_grace_period: 90s` do compose está amarrado por teste a esses
    números.]**
23. **O desfecho pago e não gravado** fica 10 min em memória. **[C5b, 11-out-2026: feito, e o "Apagar
    meus dados" e o "Desligar o ditado" o esquecem na hora, sem esperar os 10 min. O vencimento roda
    quando chega um pedido novo (risco 21).]**
24. **O limite das rotas das chaves:** 10 por minuto por conta, o `PUT` e o `GET` dos modelos juntos.
25. **O segredo novo no compose:** obrigatório a partir da C5c, com o passo da C3a no roteiro. A
    alternativa é declará-lo num arquivo de override opcional, para quem não quer o ditado. **[C5c,
    11-out-2026: cumprida. O `ai-keys-secret` é obrigatório no compose, o `gerar-segredos.sh` o cria
    numa VPS nova, e o passo 10.1 do `docs/deploy-f11.md` o cria numa VPS que já está no ar.]**
26. **Onde ficam as respostas da LG e as partes da D0 que são da instância dele:** na cópia da
    política e do dossiê da instância, fora do repositório, como a preparação do código aberto já fez
    com a região e o bucket. A alternativa é escrevê-las no repositório.
27. **A calibração e a medição sem arquivo de áudio** (C6c e C8). Com arquivo, só com o OK próprio de
    "Pontos de parada".

## O que o Mauro providencia, e quando

- **Agora:** ler esta ADR, confirmar ou mudar as derivadas, e dar (ou não) o OK da S com o intervalo.
- **Na LG:** a região do bucket, a hipótese do art. 33 do backup, o contato de privacidade, e exportar e
  o cadastro (as decisões 2 e 3 do dossiê).
- **Na D0:** ler a política e o dossiê reescritos e dar o OK no chat. Se ele for ditar dado de cliente
  pelo celular, ou ligar a dica com nomes de clientes no aprendido, dizer isso aqui (dossiê, item 4).
- **Na S (11-out-2026: o código está na `main`; falta o teste dele):** depois de remontar o Agent, fechar
  o Agent, conferir o `StartTime`, abrir o painel e ver uma edição feita no celular chegar a ele em até
  cerca de 13 s, sem tocar em nada.
- **Na C5a:** fechar o Agent, conferir o `StartTime` e ditar com o Polish ligado.
- **Na C6c, com o OK dele:** abrir a página de calibração no Android pelo cabo, seguir o roteiro (falas
  e silêncio) e passar os números. Nenhum áudio é gravado em arquivo nem sai do aparelho.
- **Antes da C7 (11-out-2026), no PC dele:** rodar o `scripts\deploy-teste.ps1` (Docker Desktop). Ele
  agora grava o `ai-keys-secret` de mentira e faz duas conferências novas (`GET /api/v1/dictation` sem
  credencial dá `401`, e o `Permissions-Policy` chega uma vez só pela borda); só o texto foi provado
  nas subfatias, porque o Docker não se roda nelas.
- **Na C7, com a autorização dele (dada em 11-out-2026):**
  - o deploy pelo roteiro (`docs/deploy-f11.md`, o passo 10 e o `subir.sh`, que já roda o `migrate`
    das 0017 a 0019 e pergunta antes);
  - as leituras do proxy e da swap (passos 10.3 e 10.4), parando em qualquer PARE, **antes** do primeiro
    áudio;
  - a chave de cifra gerada na VPS pelo passo novo, com a saída suprimida;
  - na OpenAI, um projeto e uma chave só para o celular, com limite de gasto, e a residência de dados e
    o ZDR se a conta tiver. A chave é colada no app, nunca no chat;
  - o aceite;
  - os testes do aparelho.
- **Na C8, se quiser:** o OK para mandar as falas de medição ao provedor, e a leitura do roteiro no app.

## Revisão

Depois de duas semanas de uso real, olhar:
- a mediana e o pior tempo do toque até o texto;
- quantos ditados deram `SEM_FALA` no app e no servidor;
- quantos reenvios a idempotência segurou, e quantos pagaram de novo;
- o gasto no provedor;
- se a dica ajudou (se ela foi ligada);
- quantas vezes a tela apagada ou a troca de app cortou um ditado.

Com esses dados, a ADR do passo 2 (o app nativo) parte daqui.
