# ADR-019 — Nuvem e celular: servidor próprio, sync por usuário e o Android por PWA

Status: Accepted (o plano revisado da etapa 5 e as quatro respostas, Mauro, 01-out-2026).
**O que a ADR escreve e não vem do plano nem das respostas** (saiu da crítica de arquitetura ou da
leitura do código) está em "Derivadas, a confirmar" e marcado **[derivada]** no texto. **O Mauro
confirmou as derivadas D1 a D7 em 01-out-2026**, sem mudança, e autorizou a troca da linha 7 do
`CLAUDE.md` (conflito 16, já feita). Também decidiu criar o client Google de desenvolvimento antes
da F3, para provar o login real no PC e no Android pelo USB, muito antes do deploy.
Data: 2026-10-01
Contexto: etapa 5 do roteiro de 29-set-2026 ("nuvem e celular como PWA instalável"), depois do
instalador (ADR-018)
Emenda: `00-index.md`; `01-intake.md` (contexto na nuvem); `02-prd.md` (celular, requisitos novos e
os adiados); `03-acceptance-criteria.md` (ACs da etapa 5 e os adiados); `04-system-design.md`
(DEC-008, sync e device auth); `05-data-model.md`; `06-api-contract.md`; `07-flows.md` (Sync e
Device auth); `08-ui-ux-spec.md` (celular); `09-security-privacy.md`; `10-env-and-secrets.md`;
`11-observability.md`; `12-rollout.md`; `13-implementation-plan.md`; `18-tool-contract.md` (só uma
nota: o `SyncClient` muda na F7b); `20-traceability.md`; `migrations/README.md`;
`PRODUCT.md`; e o `README.md` (Privacidade). Depois da aprovação, com a autorização do Mauro:
o `CLAUDE.md`, linha 7 (conflito 16). **Implementa a ADR-009** (toda mutação emite
SyncEvent), que até aqui era só decisão. **Não muda o núcleo** (ADR-007): o Agent continua ditando
sem nuvem (AC-033).

**Nota de 11-out-2026 (ADR-021, ditar no celular).** O Mauro pediu o ditado no celular depois da F11,
e a ADR-021 **emenda** esta ADR em cinco pontos, cada um com uma nota datada no lugar dele: a decisão 9
e a derivada D1 (o ditado só nasce no PC); "O que nunca vai" (a chave do Polish e o áudio); a decisão 8
(a chave nunca sai do PC, e as preferências); "Fora do escopo, e depois do MVP" (o ditado no celular e a
transcrição na nuvem); e a decisão 1 da F7a/b (a busca de 60 s, que passa a ser de 10 s com o painel do
PC aberto). O resto desta ADR continua valendo como está.

## O problema

O Mauro quer o histórico no celular e entre os PCs. A spec desenha nuvem, sync, login Google e
pareamento em contrato (`05`, `06`, `07`), mas não fala de celular instalável. E o que existe da
nuvem é biblioteca testada só em memória.

### Onde estamos (leitura do repositório em `dc37104`; nada rodou contra Postgres)

- **O servidor não existe.** `cmd/server/main.go:19-21` só imprime nome e versão. A API `/api/v1` é
  biblioteca em `internal/server`, com repositórios em memória, e nada a monta
  (`internal/server/server.go:89-128`).
- **Rotas da biblioteca:** health, `/updates/windows/latest`, `/me`, `/devices` (sem POST),
  `/transcriptions` com lixeira, `/dictionary`, `POST /corrections`, `/settings` e
  `/sync/push|pull`. `POST /asr/transcribe` só é montada se houver `ASRProvider`
  (`server.go:126-128`). **Não existem:** login Google, logout, `/device-auth/*` e `/resolve`. A
  sessão é só `InMemorySessionStore`, e o cookie `aurora_session` só é lido, nunca emitido
  (`internal/server/auth.go:95`).
- **A ADR-009 não está implementada** (`ADR-009:55`): o CRUD tipado não emite `SyncEvent`. Uma edição
  feita no celular nunca chegaria ao PC. Por isso o AC-016 está PARCIAL
  (`docs/fase-1-cobertura.md:92`).
- **As migrations 0001-0009 nunca rodaram** num Postgres (`migrations/README.md:27`).
  - `sync_events.seq` é `BIGSERIAL` global (`migrations/0008_sync_events_table.sql:19`). Um cursor
    sobre ele pode pular linha com transações concorrentes, e o comentário da migration diz o
    contrário.
  - `applied_mutations` tem a PK só em `mutation_id`, global (`0009_applied_mutations_table.sql:17`);
    o motor em memória já escopa por usuário.
  - `sync_events` não tem payload, e o pull do `06-api-contract.md:176-192` devolve `payload`.
- **O motor de sync do Agent** (`internal/sync`) não serve como está:
  - só resolve um conflito se a mudança mais nova vier no pull da **mesma rodada**
    (`internal/sync/engine.go:257-268`); senão a entrada fica em espera para sempre;
  - um `Apply` que falha não deixa o cursor andar e trava o sync inteiro (`engine.go:233-242`);
  - nada de produção usa esse pacote.
- **No SQLite**, `sync_outbox` e `sync_state` existem sem escritor
  (`internal/storage/migrations/0001_init_schema.sql:153-176`). Todo ditado nasce `pending` e assim
  fica (`internal/agent/winruntime/history.go:141`). Os ids são UUID v7 (`history.go:107`,
  `internal/storage/id.go`): dois PCs não colidem.
- **Retenção.**
  - O expurgo da lixeira de 30 dias **já roda** no Agent, na subida e de hora em hora
    (`cmd/agent/main.go:1118-1146`, `internal/agent/expurgo`, desde 29-set-2026).
  - Os **6 meses do histórico ativo não existem no Agent**: `PurgeExpired` só apaga o que tem
    `purge_at` (`internal/storage/transcription_sqlite.go:271-273`), e só a lixeira o grava.
  - `internal/retention` (`ActiveRetentionMonths = 6`, `policy.go:18`) só roda com dublês.
  - **[Em `3b40fd7` (08-out-2026, F10.0): o ciclo está em `cmd/agent/main.go:1184-1187` e
    `internal/agent/expurgo`; o `PurgeExpired`, em `transcription_sqlite.go:502-524`. Os 6 meses
    continuam sem existir no PC, e a F10 os decide: ver "Decisões da F10".]** **[F10f (08-out-2026):
    a F10 os implementou, atrás do OK de quem usa, e pôs a guarda da D3 na lixeira; o `retention.Job`
    continua sem chamador no PC. Ver "Decisões da F10".]**
- **O painel é fixo no modo local:** bilhete `?t=` (`web/src/sessao.ts`), token em memória mandado
  como Bearer (`web/src/api.ts:232`), build em `../bin/painel` (`web/vite.config.ts:15`). Não há
  manifest nem service worker: `web/public` só tem `favicon.svg` e `licencas-fontes.txt`.
- **Dependências.** O `go.mod` não tem driver de Postgres nem biblioteca de OAuth.
- **Desinstalador.** Apaga só `AuroraWhisper/openai` e `AuroraWhisper/anthropic`
  (`installer/aurora-whisper.iss:67-68` e `:502-503`), e o `09-security-privacy.md:299` dizia
  `AuroraWhisper/*`.
- **Docker.** O Docker Desktop está instalado nesta máquina, e o serviço estava parado na leitura de
  01-out-2026. Não há Postgres local.

### Conflitos com a spec

Por `CLAUDE.md`, conflito entre documentos é ponto de parada. Por `00-index.md` ("Fonte da
verdade"), vale a decisão humana mais recente, desde que registrada: esta ADR é o registro. As linhas
citadas são as de `dc37104`, antes das emendas desta ADR.

1. **Celular e PWA.** A spec não fala de PWA nem de app instalável. Fala de "dashboard cloud"
   (`02:37`, `08:7`, `04:174-177`), de "futuro Android/IME" (`08:9`), de Android completo fora do
   escopo (`02:10`) e de contratos prontos para Android (REQ-042, `06:283-286`). **Resolvido:
   decisão 1.**
2. **Transcrição e contexto na nuvem.** REQ-043 (Must, `02:66`), AC-041 (`03:46`),
   `POST /asr/transcribe` (`06:194-215`), `POST /resolve` com `context.app` e `nearby_text`
   (`06:217-255`), a flag `cloud_context_resolve` (`12:47`), "Cloud contextual permitido"
   (`01-intake.md:81`), as policies `local_then_cloud` e `cloud_only` (TASK-018) e as variáveis
   `ASR_CLOUD_*` e `CONTEXT_*` (`10:22-27`). **Resolvido: decisão 9 (depois).**
3. **Dicionário no banco.** A ADR-017 (`:224`), o `04:162` e o `13:247-248` prometem a ida do
   dicionário para o banco na etapa 5. **Resolvido: decisão 7 (fica no `dicionario.json`).**
4. **A Correction sincronizável** (REQ-017, TEST-008, `POST /corrections`, migration 0006). A
   ADR-017 decidiu não guardar a frase inteira (`ADR-017:253-254`). **Resolvido: decisão 7 (só o par
   curto).**
5. **O pareamento.** O `06:36-60` e o `04:179-185` desenham um início com `poll_token` e `auth_url`
   em `/device/authorize`: consulta repetida, que a RFC 8628 (§5.4) mostra ser phishável.
   **Resolvido: seção "Conectar o PC".**
6. **`target_app`.** Está no `05:50`, no `06:97` e no `06:403`, na migration 0003 e em
   `internal/server/transcriptions.go:293,377`. O Agent nunca o grava, de propósito
   (`winruntime/history.go:129`). **Resolvido: decisão 10 (sai da nuvem).**
7. **Dono local × conta Google.** Pendente na ADR-017 (`:222-223`), no `05:242-245` e no
   `13:247-248`. **Resolvido: seção "Dono local e conta".**
8. **Quem pode criar conta.** Nenhum documento define; o `01-intake.md:21` diz "inicialmente apenas
   o proprietário". Com o OAuth na internet e o repositório aberto, qualquer conta Google viraria
   usuário. **Resolvido: decisão 5.**
9. **Preferências sincronizáveis** (REQ-018, `06:145-149`, `08:297-299`). **Resolvido: decisão 8
   (depois).**
10. **O contrato de sync.** `seq` global (`05:108-117` e a 0008), mutação com PK global (0009), pull
    com um payload que a tabela não tem (`06:176-192`) e eventos guardados por 90 dias, como
    [ASSUMPTION] (`05:256`). **Resolvido: seção "Contrato de sync".**
11. **O build do painel.** O `04:174-177` e a DEC-008 (`04:210`) dizem "mesmo React bundle".
    **Resolvido: decisão 12 (mesmo código, dois builds).**
12. **Migrations.** O `migrations/README.md:7` diz que elas rodam na subida do servidor, e o `:14`
    diz UUID v4. O `05:266` manda rodar separado, e o `05:9` diz UUID v7. **Resolvido: vale o `05`;
    o README é corrigido.**
13. **Privacidade.** O `README.md:159` diz que o Polish é "o único caso em que o texto ditado sai da
    máquina", e o `03:138` proíbe texto ditado sair "fora do Polish". **Resolvido: decisão 4 e a
    seção "Segurança e privacidade".**
14. **Desinstalador.** O `09:299` e a tabela de fatias da ADR-018 (`:606`) dizem que ele apaga as
    chaves `AuroraWhisper/*`; o `.iss` apaga só as duas. **Resolvido no `09`**, que passa a dizer o
    que o desinstalador apaga hoje; o token do PC entra na opção "apagar dados" na F6. A linha da
    ADR-018 é registro da F4 e fica como está; a descrição certa está nela mesma (`ADR-018:781`).
15. **Expurgo da lixeira.** O `PRODUCT.md:65-68` e o `06:458-459` dizem que ele "ainda está sendo
    ligado"; ele já roda. **Resolvido: os dois são corrigidos.**
16. **O `CLAUDE.md` do projeto.** A linha 7 diz que o sistema é "cloud-connected para sincronizar
    histórico, dicionário, correções, preferências compatíveis e dispositivos". A etapa 5 sincroniza
    só os ditados, o aprendido e os dispositivos; o dicionário completo, as correções e as
    preferências ficam para depois (decisões 7, 8 e 9). **Resolvido:** com a autorização do Mauro
    (01-out-2026), a linha 7 passou a dizer "cloud-connected, só depois de a pessoa conectar, para
    sincronizar o histórico, o que o painel aprende e os dispositivos (dicionário completo,
    correções e preferências: depois, ADR-019)".

## Decisão

### As quatro respostas do Mauro (01-out-2026)

| # | Pergunta | Resposta |
|---|---|---|
| 1 | Android ou iPhone? | **Android primeiro.** O iPhone fica para depois do MVP. |
| 2 | Onde hospedar? | **Na VPS atual**, num compose isolado, sem Cloudflare na frente, **se** o diagnóstico só de leitura mostrar RAM e disco com folga. Encostar no proxy que já roda lá é ponto de parada. |
| 3 | O que sobe na primeira conexão? | **O que está nos últimos 6 meses, menos a lixeira**, com cópia do banco antes. Os ditados de um PC aparecem no histórico dos outros PCs. |
| 4 | Texto legível no banco do servidor? | **Sim, na v1** (sem cifra ponta a ponta), com o Postgres fechado, o HTTPS terminando na VPS e o backup cifrado com uma chave só dele. **A revisão de LGPD é pré-condição do deploy.** |

### Aprovado no plano, sem pergunta

- O "Aprender" fica só no PC. No celular, a pessoa vê, busca, edita, apaga e desfaz.
- As preferências não sincronizam na v1.
- Transcrição na nuvem, resolução com contexto na nuvem e atualização automática ficam para depois.
  As rotas não são montadas, e um teste prova isso. Na spec viram "depois"; nada é apagado.
- O dicionário continua no `dicionario.json`.
- Os 6 meses no PC só ligam com a prévia da contagem e o OK expresso do Mauro (F10).

### Derivadas, a confirmar

Estas escolhas estão escritas nesta ADR (e nas emendas da spec) como se estivessem decididas, mas
**não vêm do plano que o Mauro aprovou nem das quatro respostas**: saíram da crítica de arquitetura
ou da leitura do código. Estão marcadas **[derivada]** no texto. O Mauro as confirma, ou muda, ao
aprovar a ADR; se mudar uma, as linhas da terceira coluna são as que mudam com ela.

| # | O que a ADR escreveu | O que o plano dizia | Onde está escrito |
|---|---|---|---|
| D1 | `POST /transcriptions` **não é montada**: o ditado só nasce no PC e sobe pelo sync. **[Nota de 11-out-2026, ADR-021: a rota continua não montada, mas "o ditado só nasce no PC" deixou de valer: o do celular nasce numa rota nova. Ver a decisão 9.]** | O plano deixa de fora só a transcrição, a resolução com contexto e a atualização; não cita essa rota. | Decisão 9; `06` (rotas não montadas e "Como fica na etapa 5") |
| D2 | **"Apagar meus dados da nuvem" revoga todos os aparelhos** e as outras sessões. Os PCs ficam com a cópia deles e mostram "desconectado". | O plano diz que, depois do "apagar meus dados", o PC "baixa tudo de novo" (seção 3, "Recomeço seguro"), e que ele fica com a cópia dele (seção 4). A ADR muda o primeiro: sem a revogação, o recomeço de um PC mandaria tudo de novo e desfaria o pedido. | "Segurança e privacidade", "Apagar"; `06`, `DELETE /me/data`; `07`, "Delete"; `09`, THR-024 |
| D3 | **Trocar a conta de uma instalação é recusado na v1**, com aviso. | A crítica pedia "exigir recomeço para trocar"; o plano não fala de troca. | "Dono local e conta"; "Fora do escopo" |
| D4 | **O que sobe é mais do que o plano lista:** do ditado, também o modo, o aparelho de origem e os metadados do ASR que o histórico já mostra; do aparelho, também a plataforma. | Ditado: texto bruto, texto final, data, status e lixeira. Aparelho: nome, versão e último acesso. | "Segurança e privacidade", "O que vai para a nuvem"; `09`, "O que sai" |
| D5 | **Hospedagem:** Postgres 18; com as portas 80 e 443 livres, um Caddy só nosso dentro do compose; backup com `pg_dump -Fc` e, fora do servidor, restic (sugestão). | "O HTTPS entra pelo que já existe no servidor"; backup cifrado fora do servidor, sem dizer com quê. | "Hospedagem e backup"; `04`; `12`; F11 |
| D6 | **Os nomes das rotas e dos cookies novos**: `/auth/start`, `/auth/google/callback`, `/auth/logout`, `/device/authorize`, `/device-auth/*`, `/sessions`, `/me/data`, `/learned`, `/learned/undo`, `/transcriptions/search`, `/transcriptions/{id}/purge`, `__Host-sessao`. | O plano descreve o comportamento; só o cookie `__Host-pre` tem nome (resposta 1). | `06`; "Login", "Sessão" |
| D7 | **Detalhes de segurança:** o `sub` fica fixado no primeiro login (o mesmo e-mail com outro `sub` é recusado); tokens de aparelho sem uso expiram (sugestão: 180 dias); CSP e HSTS; o build de desenvolvimento só conecta no modo portátil (`bin\`), com uma conta de teste na lista. | O plano pede a lista de contas, o token só para sincronizar e, "se quiser", uma conta de teste. | "Login", "Token do dispositivo", "Dono local e conta", "Servidor" |

Os prazos e limites que a ADR chama de "sugestão" (a sessão de 30 dias, o vencimento do pedido de
pareamento, os 180 dias do token) são propostas, não decisões, e a F3 e a F6 as confirmam.

### Decisões

1. **O celular é um PWA instalável, Android primeiro.** O app é o painel em modo nuvem, instalado
   pelo "Instalar app" do Chrome. Por quê: um código só para o PC e o celular (DEC-008), sem loja.
   No Android, o Chrome instala como WebAPK, e a ida ao Google abre numa Custom Tab que divide os
   cookies com o app instalado: o login volta direto (documentação do Chromium; não testado).
2. **Um servidor Go só**, com Postgres, serve a API, o app e o login. Por quê: o mínimo que
   funciona numa VPS que já roda outros serviços. Memória: cerca de 250 MB (estimativa).
3. **Toda gravação passa por um ponto único**, que grava o item e o evento de sync na mesma
   transação (ADR-009). Por quê: sem isso, a edição do celular nunca chega ao PC.
4. **A nuvem é opcional e começa desligada.** Sem conectar, o Agent não abre conexão nenhuma, e vale
   a promessa do README e da ADR-018 (decisão 1: nenhuma conexão sem ação de quem usa). Conectar
   cria a **primeira conexão permanente** do Agent, e só depois que a pessoa conecta. Por quê:
   local-first (ADR-001). E o projeto é aberto: o campo da URL vem vazio, e quem quiser sobe a
   própria nuvem.
5. **Só entram as contas da lista `CONTAS_PERMITIDAS`.** Qualquer outra conta Google é recusada sem
   criar nada. Por quê: o repositório é aberto e o login fica na internet; sem lista, qualquer conta
   viraria usuário.
6. **Login só com Google, escopos `openid email profile`.** Por quê: a spec já decidiu o Google
   (REQ-015), e o escopo básico não exige verificação do app nem expira em 7 dias no modo de teste
   (documentação do Google; não testado).
7. **Do dicionário, sincroniza só o aprendido:** o par curto ("SF" → "ECF"), com a restrição e o
   nome do domínio do termo. O `dicionario.json` continua a fonte da verdade em cada PC, e a
   Correction com a frase inteira não sobe. Por quê: é o que a ADR-017 guarda. Mover o dicionário
   para o banco perderia o arquivo que a pessoa abre e audita, e o celular não precisa disso.
8. **As preferências não sincronizam na v1.** Cada PC guarda as suas: atalho, microfone, modo de
   texto, Polish, bipe, modelo de voz e a chave do provedor de IA. Por quê: "herdar ao conectar" já
   é meio sync, com versão própria. E a chave nunca sai do PC (ADR-016).
   *(Nota de 11-out-2026, ADR-021, decisão G: a chave **do PC** continua só no PC. Para ditar no
   celular, a pessoa cola uma chave no app, e o servidor a guarda **cifrada**; é outra chave, e o Agent
   nunca a vê. As preferências do ditado no celular são novas: moram só na nuvem e nunca descem aos
   PCs. As do PC continuam no PC.)*
9. **Transcrição, resolução com contexto e atualização pela nuvem: depois.** As rotas
   `/asr/transcribe`, `/resolve`, `/updates/windows/latest`, `/settings`, `/dictionary` e
   `/corrections` **não são montadas**, e um teste pede cada uma e recebe `404`. Por quê: áudio e
   contexto não saem do PC nesta etapa, e cada rota a menos é superfície a menos. Pelo mesmo motivo,
   `POST /transcriptions` também não é montada: o ditado só nasce no PC e sobe pelo sync.
   **[derivada, D1]**
   *(Nota de 11-out-2026, ADR-021, decisão A: `/asr/transcribe`, `/resolve` e `POST /transcriptions`
   **continuam não montadas**, e os testes de `404` continuam valendo para elas. O ditado do celular
   nasce numa rota nova, `POST /api/v1/dictations`, com acesso próprio (no passo 1, só a sessão do
   navegador; o token de um PC recebe `403`), que só repassa a voz ao provedor que a pessoa escolheu e
   grava o texto pelo ponto único. O texto nunca vem do cliente. "Áudio e contexto não saem do PC" vale
   para o PC; o áudio do celular tem o caminho e o aceite da ADR-021.)*
10. **O `target_app` sai da nuvem.** O servidor não tem a coluna e recusa o campo. Por quê: o nome do
    app em foco é contexto (REQ-025), e o Agent já não o grava.
11. **Texto legível no servidor** (resposta 4). O que o protege está em "Segurança e privacidade".
12. **Mesmo código React, dois builds.** O build local continua em `bin/painel`, sem service worker
    e sem login. O da nuvem tem login, manifest e service worker. Por quê: o painel local nunca
    registra service worker, e o código de login não precisa ir para o PC. A DEC-008 passa de "mesmo
    bundle" para "mesmo código".
13. **Migrations por comando próprio** (`migrate`), nunca na subida do servidor. As 0001-0009 nunca
    rodaram e são **corrigidas no lugar** na F1. Por quê: o `05:266` já manda rodar separado, e
    remendar numa 0010 tabelas que ninguém criou só acumula história falsa.
14. **Uma dependência nova de produção: o driver do Postgres (`pgx`)**, justificada na F1. O login
    Google usa a biblioteca padrão: a validação do ID token cabe em poucas checagens (F3 confirma, e
    justifica qualquer biblioteca a mais).
15. **Os testes com Postgres não sobem contêiner sozinhos.** O `go test ./...` normal não toca no
    Docker. Os testes de banco rodam só com uma build tag ou variável própria, contra um contêiner
    que a pessoa sobe por script. Por quê: subir contêiner muda o estado da máquina (`CLAUDE.md`).

## Como funciona

### Servidor

- O `cmd/server` passa a ser de verdade: configuração por ambiente (`10`), `http.Server` com
  timeouts e desligamento limpo, e `/health/ready` que confere o Postgres.
- **[derivada, D6]** Os nomes das rotas e dos cookies novos, aqui e no `06`, são proposta: a F2 e a
  F3 podem ajustá-los.
- Uma origem só, com três partes: a API em `/api/v1`; o login em `/auth/*` e a aprovação de PC em
  `/device/authorize`; e o app (o build da nuvem) no resto.
- Cabeçalhos **[derivada, D7]**: CSP própria (`default-src 'self'`, como o painel local, com `frame-ancestors 'none'`),
  `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` e HSTS. `Cache-Control:
  no-store` em toda a API. Nenhum cabeçalho de CORS.
- Log: método, padrão da rota (`r.Pattern`, nunca a URL crua), status, duração e contagens. Nunca
  texto, query, token, cookie nem e-mail.

### Gravação única (a ADR-009, implementada)

Toda mudança passa por um aplicador só, numa transação curta, venha do celular (CRUD), do PC (push),
da retenção ou do "apagar meus dados":

1. `UPDATE user_sync_counters SET last_seq = last_seq + 1 WHERE user_id = $1 RETURNING last_seq` é
   o **primeiro** comando. A trava fica até o commit: a ordem do `seq` é a ordem dos commits, sem
   buraco, e um rollback devolve o número.
2. Grava a entidade, com a versão mais um e o `last_seq` dela.
3. Grava o `sync_event`: usuário, `seq`, tipo, id, operação, versão e o aparelho de origem.
4. Quando a mudança veio do push, grava a `applied_mutation` (usuário e `mutation_id`, com o status e
   o resultado).

A ordem das travas é fixa. O contador mora numa tabela própria, e não em `users`, para não disputar
trava com o login. O gargalo é por usuário: irrelevante com uma pessoa só.

### Contrato de sync, por usuário

**O PC manda (push).** `POST /api/v1/sync/push`, só com o token do PC. Cada mutação leva
`mutation_id`, tipo, id, `upsert` ou `delete`, `base_version` e o payload. O payload tem uma lista
fechada de campos por tipo: campo fora dela é `rejected` (nunca `target_app`, áudio ou contexto).
Status por mutação:
- `applied`, com a versão nova. Quando o servidor **mesclou** (a `base_version` era velha e os campos
  mudados eram diferentes, como a lixeira de um lado e o texto do outro), traz também
  `mesclado: true` (nome final na F2). O `applied` não traz o estado: ver "Eco e mescla", abaixo;
- `duplicate`, com o mesmo resultado da primeira vez (lido da `applied_mutations`, inclusive o
  `mesclado`);
- `conflict`, com a versão e o **estado atual** da entidade;
- `rejected`, com um código de uma lista fechada;
- `gone`, com o motivo: a entidade foi apagada de vez, ou está fora da janela de 6 meses.

A resposta traz também o `sync_epoch`.

**O PC recebe (pull).** `GET /api/v1/sync/pull?after_seq=N&limit=M`, só com o token do PC. O
servidor devolve o **estado atual** de cada entidade que mudou depois de `N` (cada uma com o seu
`last_seq`) e as **lápides**, em ordem de `seq`, mais `last_seq`, `has_more` e o `sync_epoch`. Não
devolve eventos com conteúdo. Assim a idade do evento não importa, e os 90 dias do `05:256` deixam
de ser problema.

**Fila no PC: um marcador por entidade.** A `sync_outbox` passa a guardar só "este item mudou"
(`UNIQUE(entity_type, entity_id)`). O texto é lido da linha na hora de enviar, nunca copiado para a
fila. Assim:
- duas edições seguidas do mesmo ditado viram um envio só, e a segunda não briga com a primeira;
- um item expurgado não sobe a partir da fila;
- o texto não sobrevive à retenção dentro da fila.

Em `applied` ou `duplicate`, o marcador só sai se a `version` local não mudou desde o envio. Se
mudou, vai de novo, com a base que o PC conhece (a versão devolvida, se não houve mescla) e outro
`mutation_id`. Sem nuvem conectada, nada entra na fila: o envio inicial cobre o que veio antes.
**[Revisão da F7a/b, confirmada pelo Mauro em 06-out-2026. Ela muda a regra aprovada acima ("sem
nuvem conectada, nada entra na fila"), e a mudança foi aprovada: desconectado, entra a mudança num
ditado que a nuvem já tem, para subir na volta com a base certa; o ditado que ela nunca teve
continua de fora. Ver "Decisões da F7a/b", decisão 2.]** O
evento leva o `device_id` de origem, que serve para mostrar de que aparelho veio o ditado, e **não**
para descartar o eco (ver "Eco e mescla"). A tabela está vazia em todo banco (nenhum escritor), e a
F7a a refaz por uma migration nova do SQLite, sem perda.

**Conflito** (o mesmo ditado mudado em dois lugares antes de sincronizar):
- O servidor decide: vale o que chegou primeiro.
- A versão que perdeu **fica guardada no próprio item**, no servidor, e chega a todos os aparelhos
  com um **selo** discreto. A pessoa escolhe uma; escolher limpa o selo. Nada se perde sem aviso.
- Não é conflito: a mesma mudança (texto igual) ou campos diferentes (lixeira ou restaurar de um
  lado, texto do outro). É a regra que o painel já usa (`web/src/edicao.ts:68-76`).
- Apagar vence uma edição antiga: o item fica na lixeira, e a edição vai para o selo.
- No PC, o conflito aparece sempre **depois**. O `409` local compara só a versão local
  (`transcription_sqlite.go:187-210`): a edição é aceita no PC e conflita no push. Por isso, no PC, o
  selo vale sempre.
- O aplicador do PC **nunca sobrescreve um item com marcador pendente**. Ele pula esse item, e o
  estado dele chega pela resposta do push (o `conflict` traz o estado atual) ou, quando o servidor
  mescla, pelo pull seguinte (ver "Eco e mescla"). É isso que resolve o conflito que hoje fica
  parado para sempre (`engine.go:257-268`).
- Com uma pessoa só, conflito deve ser raro (estimativa).

**Recusa por regra não trava o sync.** Se uma mudança vinda da nuvem não vale num PC (por exemplo,
um aprendido que o dicionário dali recusa), o PC registra "não aplicado neste PC", que aparece em
Aprendido, e o cursor anda. Só erro de E/S segura o cursor. Hoje qualquer erro trava
(`engine.go:233-242`).

**Aplicar não ecoa.** O aplicador grava a mudança remota sem criar marcador. Ele sobe a `version`
local, para que uma aba aberta no painel receba `409` em vez de sobrescrever.

**Eco e mescla.** O PC reconhece o eco de uma mudança dele **pela versão, nunca só pelo
`device_id`**: no pull, uma entidade cuja `entity_version` é igual à `server_version` que o PC
guardou já está aplicada, e ele não a regrava. O `device_id` do evento não basta, porque uma
**mescla** feita pelo servidor leva o `device_id` do PC que enviou e, ao mesmo tempo, traz campos
mudados em outro aparelho. Por isso:
- quando o `applied` vem com `mesclado: true`, o PC **não** guarda a versão devolvida como
  `server_version` (fica a de antes). O estado mesclado chega no pull seguinte, onde a versão difere
  e ele é aplicado como qualquer mudança remota. O laço faz o push e depois o pull (F7b), então a
  espera é de uma volta;
- sem essa regra, uma mudança feita em outro aparelho (a lixeira, por exemplo), que um pull
  anterior **pulou** porque o item tinha marcador, nunca chegaria a esse PC: o pull seguinte
  traria o item com o `device_id` dele, e o PC o tomaria por eco;
- se a `version` local mudou durante o envio, o marcador fica e o item vai de novo com a base que o
  PC conhece, e o servidor mescla de novo ou acusa conflito;
- nesse reenvio com a base velha, o campo (texto ou lixeira) que o **próprio** PC mudou por último
  conta, no servidor, como conhecido por ele: o valor é o que ele mandou. Sem isso, o restaurar
  seguinte do mesmo PC perdia para a lixeira dele mesmo, e a segunda edição virava conflito com a
  primeira. **[Proposta da revisão da F2, confirmada pelo Mauro em 05-out-2026]** (ver "Decisões da F2").
O teste está na F2 (o servidor marca a mescla) e na F7a (o PC recebe o estado mesclado).

**Lápides, `gone`, epoch e recomeço.**
- Apagar de vez um ditado ainda dentro da janela de 6 meses (lixeira vencida, "excluir
  definitivamente") deixa uma **lápide**: tipo, id, motivo, a data do ditado e o `seq`. A lápide
  vive até a data do ditado passar dos 6 meses. Depois disso, a recusa por janela (abaixo) cobre.
- Uma mudança para uma entidade com lápide, ou fora da janela, recebe `gone`. Com o motivo
  "apagado", o PC apaga a cópia dele. Com "fora da janela", o PC a mantém e para de sincronizá-la.
- Cada usuário tem um `sync_epoch`. Ele muda quando um backup é restaurado (o roteiro de restauração
  troca o epoch, F11) e no "apagar meus dados".
- **A marca d'água das lápides.** O servidor guarda, em `user_sync_counters`, o **maior** `seq` de
  lápide já apagada. Quando o job de retenção apaga uma lápide, a marca sobe para o `seq` dela e
  nunca desce. O pull responde `410` quando `after_seq` é **menor que a marca d'água**.
  - É o **maior**, e não o menor, porque a lápide é apagada pela data do ditado, e não em ordem de
    `seq`. Um PC com cursor acima do menor `seq` apagado, mas abaixo de outro maior, perdeu a
    lápide do maior e ressuscitaria o item. Só "menor que o maior `seq` apagado" cobre os dois.
  - Com `after_seq=0` o pull **nunca** responde `410`: é a leitura inteira do estado atual, e é o
    cursor do recomeço. Sem essa exceção, o recomeço levaria `410` para sempre.
  - O PC também compara o epoch de cada resposta com o que guardou: diferente, ele descarta a
    resposta.
  - **[Decisão 2 da F9, confirmada pelo Mauro em 08-out-2026: este parágrafo não se cumpre. A nuvem
    não responde `410` por esta marca, e o job de retenção não a sobe: a coluna fica (o "apagar meus
    dados" a sobe ao `last_seq`) e ninguém a lê. A lápide que um PC parado perde é a de um ditado com
    mais de 6 meses; o `gone` (`apagado` enquanto a lápide vive, `fora_da_janela` depois) impede o
    ressuscitar, e o PC apaga o ditado velho pela retenção dele (F10). O `410` que o PC já trata fica
    como robustez. Ver "Decisões da F9", D2.]**
- **Recomeço**, depois de um `410` ou de um epoch novo: cursor 0, `server_version` vazio em todas as
  linhas e o envio de novo do que está na janela, sem a lixeira. É o mesmo caminho do envio inicial.
  **[Revisão da F7a/b, confirmada pelo Mauro em 06-out-2026: o recomeço fica para o epoch novo. O
  `410` no mesmo epoch só volta o cursor a 0, e a reconexão continua do cursor; as `server_version`
  continuam valendo, e nada é reenviado com a base vazia, que criava selos sem conflito e trocava o
  selo de verdade. Ver "Decisões da F7a/b", decisão 2.]**
- Por quê: sem isso, um PC desligado por meses, a restauração de um backup ou o "apagar meus dados"
  fariam um ditado apagado ressuscitar.

**Retenção pelo `created_at` do PC.**
- Na nuvem, um job diário apaga o ativo com mais de 6 meses e a lixeira com mais de 30 dias (vale a
  regra dos 6 meses se vier antes, `05`). Conta **pela data do ditado** que o PC mandou, nunca pela
  hora em que a nuvem recebeu.
- O servidor recusa criar ditado com data fora da janela (`gone`, "fora da janela") ou no futuro
  além de uma folga (a F2 fixa a folga).
- O job reaproveita `internal/retention`. Ele existe antes de qualquer dado real subir (F9 antes da
  F11). **[F9, 08-out-2026: "reaproveita" quer dizer a política (`ActiveExpiryAt` e
  `EffectiveTrashPurgeAt`), e não o `Job`; o job roda no processo do `serve`, na subida e de hora em
  hora, por usuário; o ativo vencido sai sem lápide; a lápide vence um dia depois da janela. Ver
  "Decisões da F9".]**

**Envio inicial (resposta 3).** Ao conectar, o Agent mostra quantos ditados vão subir: os dos
últimos 6 meses, sem a lixeira. Faz uma cópia do banco, do mesmo jeito da cópia `antes-do-dono`
(`VACUUM INTO`, conferida; `internal/storage/dono.go`). E envia em lotes limitados por número **e
por bytes**. Os ditados de outro PC chegam pelo pull e aparecem no histórico deste, com o aparelho de
origem.

**O aprendido** (decisão 7).
- Cada par que o painel do PC aprendeu (`alias_origem: "painel"`) é uma entidade. O id é um UUID
  derivado do hash da variante normalizada: o mesmo em todo PC, sem depender do `usuario` do
  arquivo (o id de hoje, `idDeterministico` em `internal/dictionary/seed.go:266`, depende dele e não
  é UUID).
- O registro leva a variante, o termo, a restrição e o **nome** do domínio. O mapa de domínios para
  executáveis (`dominios`, no arquivo) **não** sobe: ele diz que programas a pessoa usa.
- No PC que recebe, o par passa pelas mesmas checagens do Aprender (ADR-017). Se não vale ali (o
  domínio não existe nesse PC, a variante já é de outro termo, ou o dicionário a recusaria), fica
  "não aplicado neste PC", e nenhuma regra muda (o par que já valia ali e a nuvem tornou mais
  restrito, sem que o PC consiga honrar, sai do arquivo: ver "Decisões da F8"). A restrição nunca se
  perde no caminho.
- A base da última sincronização fica no SQLite, para a diferença em 3 vias. Assim, uma edição feita
  à mão no arquivo, ou pelo `dictlearn`, não volta como eco.
- **Nada novo entra no `dicionario.json`.** O `DisallowUnknownFields`
  (`internal/dictionary/seed.go:163`) faria um Agent antigo recusar o arquivo.
- Desfazer em outro aparelho remove o par. Se ele foi reaprendido offline depois do desfazer, o PC
  mostra "desfeito em outro aparelho".
- No celular, o aprendido aparece em lista, com Desfazer. O botão Aprender continua só no PC: o
  motor e o texto bruto estão lá (ADR-017).
- ~~**Fica para a F8, conferido com o Mauro:** o escopo exato (só o de origem "painel"; o que a pessoa
  ou o `dictlearn` puseram no arquivo fica em cada PC) e se o PC que não tem o termo o cria com
  prioridade 50, como o Aprender faz.~~ **Decidido pelo Mauro em 07-out-2026** (ver "Decisões da
  F8", que tem o desenho inteiro): sobe só o de origem "painel", e o que a pessoa, o `dictlearn` ou o
  "Adicionar termo" (ADR-020) puseram no arquivo fica em cada PC (D1); o PC que não tem o termo o
  cria com prioridade 50, como o Aprender faz, quando a troca não é presa a um domínio (D2); nada
  aprendido sobe antes do clique em "Enviar o histórico" (D3); e no desacordo vale o primeiro na
  nuvem, com o desfazer vencendo (D4).

### Login

- Fluxo de código do Google, no servidor, com cliente Web confidencial. Escopos **`openid email
  profile`**, com `state`, `nonce` e PKCE (S256). O PKCE com cliente confidencial não está na
  documentação do Google; a F3 confere na prática.
- O app chama `POST /auth/start` por `fetch`. A resposta grava o cookie **`__Host-pre`** (5 min,
  `HttpOnly`, `Secure`, `SameSite=Lax`, uso único) e devolve a URL do Google. O `state` fica **no
  servidor**, com o verificador do PKCE e o `nonce`, e vale uma vez.
- No callback (`GET /auth/google/callback`), o servidor confere o `state` e o cookie `pre`, troca o
  código e valida o ID token (`iss`, `aud`, `exp`, `nonce`; ele vem direto do endpoint de token, por
  TLS). Exige `email_verified`.
- **Lista permitida.** Só entra e-mail que está em `CONTAS_PERMITIDAS`. No primeiro login de um
  e-mail da lista, o usuário é criado e o `sub` fica **fixado** **[derivada, D7]**. O mesmo e-mail com outro `sub` é
  recusado. A chave do usuário é o `sub`, nunca o e-mail. Conta fora da lista é recusada sem criar
  usuário, sessão ou linha nenhuma; o log diz só "login recusado".
- **iPhone, depois do MVP.** No iPhone, o app da Tela de Início tem cookies separados do Safari, e
  nada garante que o login do Google volte para o app (Apple, WWDC23, sessão 10120; web.dev, "Learn
  PWA"). A rede de segurança é um código de 6 dígitos, mostrado na página de retorno e digitado no
  app. **Não entra no MVP.** O desenho acima já guarda o `state` no servidor e usa o cookie `pre` de
  curta duração: o código entra depois sem refazer nada.
- O `POST /auth/start` aceita um corpo opcional, `volta`: o **caminho** da própria origem para onde o
  callback leva a pessoa no fim (o caso de `/device/authorize`, que exige sessão). Fica em
  `pending_logins.return_to`, com um `CHECK` contra open redirect, e o servidor o valida antes
  **[proposta da F1; implementada na F3 como estava, com a mesma regra no servidor e no banco]**.

### Sessão

- Cookie **`__Host-sessao`**: `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, sem `Domain`. Id
  aleatório de 256 bits, trocado a cada login. O banco guarda só o HMAC dele com `SESSION_SECRET`:
  quem lê o banco não ganha sessão.
- Expira em 30 dias sem uso (sugestão do plano). **F3:** 30 dias sem uso e um teto de 90 dias desde o
  login (decisão 1 da F3, confirmada pelo Mauro em 05-out-2026; ver "Decisões da F3").
- "Sair" apaga a sessão no servidor (`POST /auth/logout`).
- A pessoa vê as sessões abertas e derruba qualquer uma: é o caso do celular perdido.

### Proteção do navegador

- `http.CrossOriginProtection`, da biblioteca padrão (Go 1.25 ou mais novo; o projeto usa 1.27), em
  todo pedido que não é `GET` nem `HEAD`. Ela só aceita `Sec-Fetch-Site` `same-origin` ou `none`. Isso
  protege mesmo se o subdomínio dividir o domínio com outros serviços: aí eles são "same-site", e o
  `SameSite=Lax` sozinho não protege.
- Nenhum `GET` muda estado. A única exceção é o callback do Google, protegido pelo `state` de uso
  único e pelo cookie `pre`.
- Nenhum CORS.
- Com `Authorization: Bearer`, o cookie é ignorado. Bearer inválido é `401`, sem cair no cookie.
  (F3: com qualquer `Authorization`, mesmo que não seja `Bearer`.)
- Limite de tentativas no login, no início do pareamento e na troca do código. O `X-Forwarded-For`
  só vale quando vem do proxy configurado.

### Conectar o PC (pareamento)

Retorno para `127.0.0.1` com PKCE (RFC 8252, §7.3). Não é consulta repetida.

1. Em Configurações, no painel local, a pessoa cola a URL da nuvem e clica em **Conectar à nuvem**.
2. O Agent gera o verificador do PKCE e chama `POST /api/v1/device-auth/start` com o desafio, o nome
   do PC, a versão, o `install_id` e a porta do painel local. A resposta traz o endereço da página de
   aprovação, um código curto **só para conferência** e o `pedido` (o id do pedido), que é o `state`
   do retorno (passo 6) **[proposta da F1; a F6 confirma]**.
3. O Agent abre o navegador nessa página, atrás do portão `sysinput`, como o
   `painel.AbrirNoNavegador` já faz (`internal/agent/painel/abrir_windows.go:24-27`).
4. A página (`/device/authorize`) exige sessão. Mostra o nome do PC e o código, e avisa: "Só aprove
   se você clicou em Conectar neste PC agora." O painel local mostra o mesmo código.
5. **Aprovar é um `POST`**, que responde com um redirecionamento para
   `http://127.0.0.1:<porta>/nuvem/retorno?code=…&state=…`.
6. O painel local confere o `state` (o servidor dele já barra Host estranho,
   `internal/agent/painel/servidor.go:44-47`). O Agent troca o código, com o verificador, em
   `POST /api/v1/device-auth/exchange`, e recebe o `device_id` e o token.
7. O token vai para o Gerenciador de Credenciais, no alvo `AuroraWhisper/nuvem`, pelo portão. A nuvem
   guarda só o HMAC dele com `DEVICE_TOKEN_PEPPER`.

**Por que resiste a phishing.** Quem manda um link de aprovação para o Mauro não recebe nada. O
código sai para o `127.0.0.1` **do PC do Mauro**, e trocá-lo exige o verificador, que só o Agent que
começou tem. No fluxo de consulta repetida (o da spec, e o "código de dispositivo" da RFC 8628), o
atacante começa, manda o link, e recebe o token depois do clique do Mauro (RFC 8628, §5.4). O código
curto não protege nada sozinho: é só conferência.

A Local Network Access do Chrome (versão 142) cobre `fetch`, sub-recursos e subframes; o anúncio do
Chrome não fala da navegação principal. **A F6 confere no Chrome do PC** que o redirecionamento
chega ao painel local.

### Token do dispositivo

- Opaco, 256 bits. Na nuvem, só o HMAC.
- **Escopo:** `/api/v1/sync/*`, `GET /api/v1/me` e o próprio aparelho (renomear e desconectar).
  Nunca "apagar meus dados", revogar outro aparelho, ver as sessões ou mexer no histórico pelo CRUD.
- Revogar o PC, pelo celular ou pelo navegador, faz o token dele receber `401` na chamada seguinte.
  O Agent mostra "desconectado" e continua ditando e guardando localmente.
- Apagar o token local (desinstalar apagando os dados) não revoga o aparelho na nuvem: ele fica na
  lista até alguém revogar. Tokens sem uso expiram (o prazo é da F6; sugestão: 180 dias).
  **[derivada, D7]**
- Um build de desenvolvimento só conecta no modo portátil (o `bin\`) **[derivada, D7]**, para nunca ligar os dados
  reais à nuvem por engano (ADR-018, risco 11). Como: F6.

### Dono local e conta

- As linhas do SQLite continuam com o **dono fixo** da instalação (`05`, "Dono local"). O usuário da
  nuvem vem da sessão ou do token; nenhum payload leva `user_id`.
- A instalação fica ligada a **uma** conta. Ao conectar, o `sub` dela vai para a tabela
  `instalacao`, com a URL da nuvem e o `device_id`.
- O pull grava as linhas com o dono local, e um ditado de outro PC entra no histórico deste.
- **Trocar de conta numa instalação é recusado na v1** **[derivada, D3]**, com aviso. Sem isso, o histórico que veio da
  conta A subiria para a B. Trocar fica para depois.
- O `bin\` de desenvolvimento tem banco e dono próprios: conectado, ele é outro aparelho. Ele usa uma
  **conta de teste** na lista permitida, para não misturar dado de teste no histórico real.

### App do celular (PWA)

- **Manifest:** `name`, `short_name`, `id`, `start_url: "/"`, `scope: "/"`,
  `display: "standalone"`, `theme_color` e `background_color` `#070a09` (a paleta do painel,
  `web/src/index.css`), `lang: "pt-BR"`.
- **Ícones**, gerados do símbolo (`docs/marca/pico-estourado`): 192 e 512 px; um 512 maskable, em
  entrada separada; e o de 180 px para o iPhone, depois. Os ícones são mostrados renderizados antes
  de concluir.
- Manifest e ícones são públicos (o navegador os busca sem cookie). O `start_url` abre a tela Entrar
  quando não há sessão.
- **Service worker mínimo, só da casca:**
  - trata só `GET` de `/assets/*` e a navegação para as rotas do app;
  - na navegação, **rede primeiro**; a casca guardada só vale sem rede, para mostrar "sem conexão";
  - **nunca** `/api`, `/auth` nem `/device` (o callback e a aprovação não podem virar a casca);
  - cache com versão, limpo no `activate`; `Cache-Control: no-cache` no `index.html` e no `sw.js`,
    e `immutable` em `/assets`;
  - **nunca guarda texto ditado.** O celular não guarda ditado no aparelho.
- **O painel local nunca registra service worker.** O registro só existe no build da nuvem, por
  constante de compilação. Um teste confere que o bundle local não tem `serviceWorker.register`. E o
  Agent responde `404` em `/sw.js` (hoje, um caminho que não é arquivo devolve o `index.html`).
- **Teste cedo no Android, pelo cabo USB:** o Chrome do PC encaminha a porta, e o celular abre o
  `http://localhost` do PC, que conta como contexto seguro (F4 e F5).

### Hospedagem e backup

- **Condição (resposta 2):** o diagnóstico só de leitura (`scripts/diagnostico-servidor.sh`) mostra
  RAM e disco com folga. Se não mostrar, a hospedagem volta para o Mauro decidir.
- **Um compose só nosso:** a API e o Postgres numa rede própria. O Postgres não publica porta
  nenhuma: porta publicada pelo Docker passa por cima do firewall do sistema. Limite de memória em
  cada contêiner. Segredos por arquivo (`POSTGRES_PASSWORD_FILE`).
- **Postgres 18** **[derivada, D5]**, a imagem oficial atual. No 18, o volume monta em `/var/lib/postgresql`, e não
  mais em `.../data`.
- **O HTTPS termina na VPS**, sem Cloudflare na frente: ela terminaria o TLS e leria o texto. Como o
  tráfego chega:
  - com as portas 80 e 443 livres, um Caddy só nosso, dentro do compose **[derivada, D5]**;
  - com um proxy já rodando (Traefik, Nginx, Caddy, Nginx Proxy Manager), **encostar nele é ponto de
    parada**: muda o roteamento de um serviço em produção.
- **Backup:**
  - `pg_dump -Fc` toda noite;
  - uma cópia cifrada fora do servidor, com uma chave que só o Mauro tem (sugestão: restic **[derivada, D5]**)
    **[F11, 09-out-2026: o `age`, com a chave pública no servidor, enviado ao Backblaze B2 por uma
    chave sem `deleteFiles`; o restic ficou de fora. Ver "Decisões da F11".]**;
  - um teste de restauração por mês, num contêiner descartável;
  - restaurar troca o `sync_epoch` **[F11: e o que foi apagado depois do backup pode voltar; ver "A
    restauração: aceitar e deixar escrito"]**;
  - o backup guarda texto já apagado até a cópia girar. O prazo de giro entra na F11 e no texto de
    privacidade **[F11: 30 dias, pela regra de ciclo de vida do bucket]**.

## Segurança e privacidade

- **O que vai para a nuvem:**
  - de cada ditado **[derivada, D4]**: texto bruto e final, data, status, lixeira, modo, aparelho de origem e os
    metadados do ASR que o histórico já mostra;
  - o aprendido: par curto, termo, restrição e nome do domínio;
  - de cada aparelho: nome, plataforma, versão e último acesso;
  - da conta: e-mail e `sub`.
- **O que nunca vai:** áudio; contexto de janela (o nome do app em foco, título, texto de perto); a
  chave do Polish; os logs; o mapa de domínios para executáveis; as preferências.
  *(Nota de 11-out-2026, ADR-021: isto vale para o áudio **do PC**, a chave do Polish **do PC** e as
  preferências **do PC**. O celular manda ao servidor outras coisas, só depois do aceite específico de
  quem dita: a voz de cada ditado, em memória e descartada ao fim; a chave que a pessoa cola no app, que
  o servidor guarda cifrada; o aceite e as preferências do ditado. Elas estão em `09`, "Ditar no celular:
  o que sai do aparelho", e na ADR-021.)*
- **Texto legível no servidor** (resposta 4). Protegem: o Postgres fechado, sem porta publicada; o
  HTTPS terminando na VPS, sem terceiro na frente; e o backup cifrado. Quem administra a VPS lê o
  texto. A cifra ponta a ponta fica para depois.
- **Logs do servidor:** rota, status, tempo e contagens. Nunca texto, busca, token, cookie, código de
  pareamento ou e-mail.
- **O log do próprio Postgres** também não pode trazer texto ditado (achado da revisão da F2,
  05-out-2026). Com o padrão (`log_error_verbosity = default`, `log_min_error_statement = error`,
  conferido no Postgres 18 de teste), toda violação de constraint grava o `DETAIL` "Failing row
  contains (...)", com o `raw_text` e o `final_text`. Hoje só um bug interno dispara isso (a entrada é
  validada antes), mas a regra vale fora do processo Go também. **A F11 configura:**
  `log_error_verbosity = terse` (sem `DETAIL`, `HINT`, `QUERY` nem `CONTEXT`), `log_statement = none`,
  `log_min_duration_statement = -1`, `log_parameter_max_length_on_error = 0` e `auto_explain`
  desligado; e o roteiro de deploy confere os cinco com `SHOW` antes do primeiro dado real.
- **Busca fora da URL.** A busca vai no corpo de um `POST`, para o texto ditado não parar no log de
  acesso de um proxy. A API local continua com `?q=`: ela é loopback, sem proxy, e não loga a URL.
- **Login de teste só por build tag** de teste, nunca por variável de ambiente. Uma configuração
  errada viraria entrada sem senha em produção.
- **Apagar:**
  - "excluir definitivamente", na lixeira (deixa lápide);
  - **"apagar meus dados da nuvem"**: só com sessão (nunca com o token do PC) e com confirmação.
    Apaga tudo da conta na nuvem, troca o epoch e **revoga todos os aparelhos** **[derivada, D2]**, para nenhum PC subir
    tudo de novo no recomeço. Os PCs ficam com a cópia deles e mostram "desconectado";
  - a retenção: 6 meses no ativo e 30 dias na lixeira, pela data do ditado.
- **Os 6 meses no PC** ainda não existem no Agent. A primeira vez que rodarem, apagam de vez os
  ditados com mais de 6 meses. Antes, o Agent mostra quantos são, faz uma cópia e só liga com o **OK
  expresso** do Mauro (F10). A F10 também decide quando somem a cópia do envio inicial e a
  `antes-do-dono`, que hoje ficam fora da retenção. **[F10 (08-out-2026): implementada. Os 6 meses
  existem no Agent e só ligam com o clique no bloco Histórico das Configurações, depois da contagem e
  com a cópia `antes-dos-6-meses`; as cópias só saem pelo botão Apagar as cópias. Ver "Decisões da
  F10".]**
- **LGPD: pré-condição do deploy.** O texto deixa de ficar só no PC. A revisão vem antes do primeiro
  dado real subir (F11) e cobre: base legal; transparência; retenção, inclusive a do backup;
  direitos do titular; o país do servidor; e dados de terceiros que a pessoa dite (de clientes, por
  exemplo). Sem ela, nada vai para o servidor.
- **O desinstalador**, na opção "apagar dados", passa a apagar também o token do PC
  (`AuroraWhisper/nuvem`), na F6. Só nessa opção: apagar sempre desconectaria o PC a cada
  atualização.

## Fora do escopo, e depois do MVP

Nada disso sai da spec: vira "depois", com o motivo.

- **iPhone**, e com ele o login pelo código de 6 dígitos. O desenho do login já o comporta.
- **Aprender no celular.** O motor e o texto bruto estão no PC.
- **Sync de preferências** (a parte de preferências do REQ-018). A rota `/settings` não é montada.
- **Transcrição na nuvem** (REQ-028, REQ-043, AC-041, TASK-017) e as policies `local_then_cloud` e
  `cloud_only` (REQ-029, AC-030, AC-032, TASK-018). *(Nota de 11-out-2026, ADR-021: a rota
  `/asr/transcribe` e as policies continuam fora. O que entrou é o ditado do celular, que repassa a voz
  ao provedor da pessoa por uma rota própria; ver o item "Ditado no celular", abaixo.)*
- **Resolução com contexto na nuvem** (`/resolve`, `cloud_context_resolve`).
- **Atualização pela nuvem** (`/updates/windows/latest`, TASK-023). A ADR-018 já decidiu a
  atualização manual nas 0.x.
- **O dicionário completo no banco**, e as telas de Dicionário e de Correções na nuvem.
- **A Correction com a frase inteira.** O TEST-008 passa a valer como o par curto.
- **Ditado no celular** e texto guardado no aparelho. *(Nota de 11-out-2026, ADR-021: o ditado no
  celular **entrou**, no passo 1 (o PWA: toque para começar e toque para parar, com a voz transcrita
  pelo provedor que a pessoa escolhe, OpenAI ou Groq, com a chave dela). O texto guardado no aparelho
  continua fora. O app nativo Android, a bolha flutuante e o teclado são o passo 2, outra ADR.)*
- **Trocar a conta** de uma instalação.
- **Cifra ponta a ponta.**
- **App nativo Android ou teclado (IME).**

## Alternativas descartadas

- **Cloudflare, ou outro proxy de terceiro, na frente.** Terminaria o TLS e leria o texto.
- **Pareamento por consulta repetida** (o da spec, e o código de dispositivo da RFC 8628).
  Phishável (§5.4).
- **Login por link mágico.** Exige um remetente de e-mail com SPF e DKIM. No iPhone, o link abre
  fora do app.
- **Passkeys agora.** Exigem conta já verificada, e a biblioteca em Go ainda é 0.x. Ficam como
  complemento.
- **`vite-plugin-pwa`.** Traz o Workbox e dezenas de dependências; um `sw.js` pequeno resolve.
- **Fila com cópia do texto, uma linha por mudança** (o desenho de hoje). Conflita consigo mesma e
  guarda texto além da retenção.
- **Pull de eventos com conteúdo.** Exige guardar o payload em cada evento, e perde mudança de quem
  fica parado além da retenção dos eventos.
- **Dicionário no banco agora.** Perde o arquivo auditável, e o celular não precisa disso.
- **"O último que escreve vence"** no conflito (o `RebaseResolver` de hoje,
  `internal/sync/resolve.go`). Perde uma edição sem aviso.
- **Uma migration 0010 remendando a 0001-0009.** Ninguém criou essas tabelas.

## Consequências

**Positivas:**
- o histórico no celular, com o mesmo visual do painel;
- o que se aprende num PC chega aos outros;
- quem não conecta continua exatamente como hoje;
- a ADR-009 deixa de ser só decisão, e o AC-016 passa a valer pela porta real.

**Negativas:**
- **o texto fica legível no servidor**, para quem administra a VPS;
- **existe uma conexão permanente**, para quem conecta;
- **cada PC passa a mostrar o histórico dos outros** (é o pedido). O "aparece em N ditados" do
  Aprender passa a contar os ditados de todos os PCs;
- **mais superfície:** login, sessão, pareamento e um servidor na internet;
- **o iPhone fica de fora** até depois do MVP;
- **uma dependência nova de produção** (`pgx`);
- **os testes de banco precisam do Docker Desktop aberto**, e não rodam no `go test ./...` normal;
- **o backup guarda texto apagado** até a cópia girar.

## Riscos, e o que não foi verificado

1. **A VPS não foi vista.** RAM, disco, proxy e país dependem do diagnóstico. Os 250 MB são
   estimativa.
2. **O Google não foi testado:** o aviso de "app não verificado" no modo de teste com escopo básico,
   o PKCE com cliente confidencial e o tempo até o client valer (de minutos a horas, pela
   documentação).
3. **O redirecionamento para `127.0.0.1`** pode ser barrado pela Local Network Access do Chrome. A F6
   confere. Se for barrado, o retorno teria de virar um código colado no painel local, o que perde a
   resistência a phishing: volta para o Mauro decidir.
4. **O Android instalado** (WebAPK, Custom Tab dividindo cookie) vem da documentação do Chromium;
   nada foi instalado. O teste por USB na F5 confere cedo.
5. **Casca velha do service worker.** Rede primeiro na navegação, cache com versão e `no-cache` no
   `index.html` e no `sw.js` reduzem o risco, sem zerar.
6. **Conflito raro** é estimativa.
7. **A primeira purga de 6 meses no PC apaga dados reais.** Só com prévia, cópia e OK (F10).
8. **Dado de teste no histórico real**, se o `bin\` conectar com a conta real. Por isso, a conta de
   teste na lista.
9. **O proxy que já roda na VPS.** Encostar nele muda um serviço em produção. Ponto de parada.
10. **Relógio errado num PC** muda a data do ditado, e a retenção usa essa data. A folga para o
    futuro (F2) reduz o risco.
11. **O `code` e o `state` do callback do Google** passam na URL e podem ir para o log do proxy da
    VPS. São de uso único e vencem em minutos.
12. **Uma sessão por fatia** é estimativa.

### Pontos de parada (`CLAUDE.md`: parar e perguntar)

- **as decisões derivadas D1 a D7** ("Derivadas, a confirmar"): o Mauro as confirma ao aprovar a ADR
  (F0), e nenhuma fatia que dependa delas começa antes disso;
- deploy, DNS, proxy e migration em produção (F11): quem roda é o Mauro, com o OK dele;
- encostar no proxy que já roda na VPS;
- segredos reais (o client do Google, a senha do Postgres, o `SESSION_SECRET`, o
  `DEVICE_TOKEN_PEPPER` e a chave do backup): gerados pelo Mauro, no servidor ou num arquivo local
  fora do repositório, nunca no chat;
- a primeira purga de 6 meses no PC (F10);
- a revisão de LGPD antes do primeiro dado real;
- ~~o escopo exato do aprendido que sincroniza (F8)~~ (decidido pelo Mauro em 07-out-2026, com a
  criação do termo no PC que recebe: ver "Decisões da F8");
- o diagnóstico mostrar a VPS sem folga.

## Fatias

Toda fatia roda `go test ./...` e `go vet ./...` (e o lint, o teste e o build do `web/` quando mexe
nele), atualiza os documentos que muda e termina com commit local, sem `git push`. Nenhuma toca o
servidor real. Os testes com Postgres rodam só quando pedidos, contra um contêiner que a pessoa sobe
por script.

| Fatia | O que entrega | O que prova | O que o Mauro testa | Estado |
|---|---|---|---|---|
| F0 | esta ADR, as emendas da spec e `scripts/diagnostico-servidor.sh` | nada de código (só documentação) | aprova a ADR e confirma as derivadas D1 a D7; roda o diagnóstico na VPS e cola a saída (pode ser durante F1 a F3) | **aprovada (01-out-2026);** falta o diagnóstico |
| F1 | Postgres de verdade: a 0001-0009 corrigida no lugar (`seq` e mutação por usuário, sem `target_app`); as tabelas novas (contador, sessões, login pendente, pareamento, tokens, lápides, aprendido); o comando `migrate`; o `pgx`; o script do contêiner de teste; `migrations/schema_test.go` atualizado junto com a 0003, a 0008 e a 0009 (ele fixa `target_app`, `seq BIGSERIAL` e `mutation_id` como PK) | o SQL roda num Postgres 18; o isolamento por usuário | abre o Docker Desktop e roda o script do contêiner | **implementada (01-out-2026) e verde num Postgres 18 de verdade (05-out-2026, rodada do Mauro com `scripts/postgres-de-teste.ps1`: `migrations` 4,45 s e `pgmigrate` 1,83 s, contra 0,24 s e 0,26 s quando os testes pulam).** Revisada no mesmo dia (PK do ditado por usuário, entidade na mutação, `return_to` do login: três propostas a confirmar). Ver "Decisões da F1" |
| F2 | o ponto único de gravação; o push e o pull novos; `gone`; o contador por usuário | uma edição pela API aparece no pull do PC; gravações simultâneas não pulam evento; reenviar dá `duplicate`; a mescla de campos diferentes (com um pull no meio) responde `applied` com `mesclado: true`, e o `duplicate` a repete | nada | **implementada (05-out-2026) e verde num Postgres 18 de verdade** (`pgstore` 2,67 s e `migrations` 3,90 s, contra 0,30 s e 0,33 s quando pulam). Só o ditado; o aprendido é da F8. A 0003 ganhou `text_version` e `trash_version` (corrigida no lugar). **Revisada no mesmo dia** (`pgstore` 3,40 s, `migrations` 4,07 s): a mudança mesclada do próprio PC conta como conhecida por ele (a 0003 ganhou `text_device_id` e `trash_device_id`), o `duplicate` confere o aparelho, teto de 64 KiB por texto e 2 MiB de texto por página, busca com nulo dá `400`, e o isolamento da lápide e do "excluir definitivamente" testado. As três propostas (a regra do próprio PC, os dois números e a gravação sem `ON CONFLICT`) e as escolhas de "Decisões da F2" foram confirmadas pelo Mauro em 05-out-2026. Ver "Decisões da F2" |
| F3 | o servidor de verdade, no PC: configuração, login Google (simulado nos testes, por build tag), lista permitida, sessões revogáveis, CSRF, busca por `POST`, "excluir definitivamente", "apagar meus dados" e as rotas não montadas | conta fora da lista não cria nada; o token do PC não alcança o CRUD; as rotas não montadas dão `404`; o log não tem texto | sobe no PC e entra com a conta dele (precisa do client Google de desenvolvimento; roteiro em `docs/teste-manual-nuvem-f3.md`); **adiado para a F11 por decisão do Mauro (05-out-2026)** | **implementada (05-out-2026) e verde num Postgres 18 de verdade** (`pgstore` 5,09 s, `migrations` 4,28 s, `pgmigrate` 1,84 s e `cmd/server` 0,77 s, contra 0,30 s, 0,31 s, 0,30 s e 0,32 s quando pulam). O login Google é provado por um dublê (nenhum teste chama o Google; o "login de teste" só existe com a build tag `logindeteste`); a lista, as sessões, o escopo do token, o CSRF, os cabeçalhos, os limites, a busca por `POST`, o "excluir definitivamente", o "apagar meus dados", as rotas não montadas e o `serve`. Nenhuma dependência nova. As três decisões do plano (o teto da sessão, os limites e o "apagar meus dados") usam a recomendação, **confirmada pelo Mauro em 05-out-2026**. ~~Falta o teste manual com o client Google de verdade.~~ **Feito pelo Mauro em 09-out-2026**, com o client Web de produção (`aurora-whisper-dev`, o redirect de produção e o de `http://localhost:8080`) e o `scripts/nuvem-local.ps1` contra o Postgres de teste: as 16 migrations aplicadas, a retenção antes do "servindo", e o login de ponta a ponta (`POST /auth/start` 200, o callback 303, `/api/v1/me` 200, o histórico 200), com o log só com a rota e o status. O Google deixou entrar sem a conta na lista de usuários de teste (o dono do projeto, ou o app publicado); quem decide é a lista `CONTAS_PERMITIDAS` da nuvem. A recusa de uma segunda conta não foi testada à mão (fica para o teste final). **Revisada no mesmo dia:** o `__Host-pre` na busca do login pendente, o IPv6 por `/64` no limite, o login de teste só no loopback, e o `Host` e o `/` de resto testados em toda rota (26 mutações, 25 mortas e 1 equivalente). Ver "Decisões da F3" e "Revisão da F3" |
| F4 | o painel em modo nuvem: build separado, cliente da nuvem, Entrar, sem conexão, sessão expirada, telas reduzidas e o selo | o bundle local não tem login nem service worker | abre em `localhost`; se quiser, no Android pelo cabo USB | **implementada (05-out-2026).** O mesmo código em dois builds (`vite build` e `vite build --mode nuvem`), com o `bundle.test.ts` construindo os dois e conferindo que o local não tem `/auth`, `/api/v1` nem `serviceWorker`, e o da nuvem nada do PC; o cliente da nuvem por cookie; Entrar (com a Aurora), Sem conexão, Sessão expirada, as Configurações da conta, o selo com a escolha (`PATCH` com `resolver_conflito`, na memória e no Postgres) e o "excluir definitivamente"; o servidor serve o app (`PAINEL_DIR`) no lugar da página provisória; e o servidor de teste no PC (`teste`, build tag `logindeteste`: na memória, sem Google, com ditados de exemplo). Conferida no navegador (claro e escuro, 375 px e desktop). As três escolhas do plano usam a recomendação, **a confirmar pelo Mauro**. Teste manual: `scripts/nuvem-local-teste.ps1` e `docs/teste-manual-nuvem-f4.md`. Revista no mesmo dia (o foco ao entrar, o "Abrindo…", o Google sem esperar, a volta do Google, o selo que avisa do descarte, as abas de 44 px e um teste que falhava de vez em quando); o rascunho que sumia com o `401`, que ficou para o Mauro, foi decidido e feito no mesmo dia: com um texto não salvo na tela, o Painel fica, e a pessoa entra de novo numa janela à parte, sem perder o texto. Ver "Decisões da F4" |
| F5 | o app instalável: manifest, ícones e service worker; o Agent responde `404` em `/sw.js` | o service worker não guarda `/api`; o bundle local não o registra | confere os ícones renderizados; instala no Chrome do PC e no Android, pelo USB | **implementada (05-out-2026) e instalada pelo Mauro no Chrome do PC no mesmo dia** (o log mostra o manifest, o `sw.js` e os ícones de 512 e maskable baixados na instalação, e a janela do app abrindo em seguida). O nome "Whisper", o ícone da bandeja e a abertura escura são escolhas dele. O manifest e o `sw.js` em `web/pwa/`, postos na raiz do build só no modo nuvem (um plugin do `vite.config.ts`); os ícones do `go run ./cmd/gerarico` (o do .exe em 192 e 512, e o 512 maskable de fundo cheio); o registro em `src/nuvem/registrarSW.ts`; o Agent responde `404` em `/sw.js`. Nenhuma dependência nova e nenhuma mudança no servidor da nuvem (só testes). O `bundle.test.ts` roda o `sw.js` emitido num `node:vm`: guarda só a casca, nunca toca `/api`, `/auth` nem `/device`. As três escolhas do plano são decisões do Mauro (05-out-2026). Duas mudanças do plano: sem navigation preload (ele pediria duas vezes a volta de uso único do Google; medido) e o favicon respondido pelo service worker. Conferida num Chromium de verdade (o Edge sem janela, num perfil de rascunho: o navegador do app não registra service worker): registra, assume a página, o Chromium não vê erro de instalação e, sem o servidor, abre em "Sem conexão.". 33 mutações, 33 mortas. Falta o Mauro ver a folha dos ícones e rodar `docs/teste-manual-nuvem-f5.md`. Ver "Decisões da F5" |
| F6 | conectar o PC: pareamento por `127.0.0.1` com PKCE, token no Gerenciador de Credenciais, revogar e desconectar; o desinstalador apaga o token na opção "apagar dados" | link copiado não conecta; revogar dá `401` e o Agent segue local; sob `go test`, nada passa do portão | conecta o Agent do `bin\` (conta de teste) e revoga (`docs/teste-manual-nuvem-f6.md`) | **implementada (05-out-2026) e testada pelo Mauro em 06-out-2026** com a nuvem de teste e o Agent do `bin\`: conectou (aprovação em 2 s), viu "Conectado", viu "A nuvem não respondeu agora. O PC continua conectado." com a nuvem desligada, o PC se desconectou sozinho quando a nuvem religada não o conhecia, reconectou e revogou pelo app (o PC mostrou "Este PC foi desconectado na nuvem." em 79 s). **Autorizada pelo Mauro em 05-out** ("um PC conectado que manda os ditados para o celular"; a F6 é só o pareamento). O servidor (as rotas do pareamento e dos aparelhos, os 180 dias), o lado do PC (`internal/agent/nuvem`, sem build tag), a Local API `/api/nuvem*`, o `/nuvem/retorno` no servidor do painel, os dois portões provados sob `go test`, a seção Nuvem do PC, a página de aprovação e os Aparelhos no app da nuvem, e o desinstalador. Nenhuma dependência nova; nenhuma migration. Conferida de ponta a ponta num Chromium (o navegador do app). As três decisões do plano usam a recomendação, **confirmada pelo Mauro em 06-out-2026**, e o retorno aceita só as portas 49152 a 65535 (também decisão dele). **Os testes de Postgres não rodaram** (o contêiner estava desligado, na entrega e na revisão). **Revista em 06-out-2026:** a conta de teste com o id fixo (a nuvem de teste reiniciada não prende mais o `bin\`), a conta que aprova na página de aprovação, o teste da segunda guarda da D3, o comentário do token no Gerenciador e o `09` arrumado (15 mutações, todas mortas). Ver "Decisões da F6" e "A revisão da F6" |
| F7a | a fila por entidade e o aplicador no PC, sem rede | o aplicador não sobrescreve pendência; recusa por regra não trava o cursor; aplicar não ecoa; o eco só é descartado pela versão, nunca pelo `device_id`: dois aparelhos editando campos diferentes, com um pull no meio, chegam inteiros ao PC | nada | **implementada (06-out-2026), autorizada pelo Mauro no mesmo dia** ("deu certinho" na F6). A migration 0005 do SQLite, aditiva e provada contra um banco na 0004 com dados; o marcador em toda gravação do histórico, na mesma transação (conectado; desconectado, só num ditado que a nuvem já tem, desde a revisão); as decisões puras do push e do pull; a fila que as executa; o `PATCH` local com o teto de 64 KiB. Sai o motor antigo. Ver "Decisões da F7a/b" |
| F7b | o cliente HTTPS e o laço de sync: `401` vira "desconectado", `410` volta o cursor a 0 (o epoch novo é que recomeça), espera com jitter, log só com contagens; atualiza o `SyncClient` no `18-tool-contract.md` | o laço sobrevive à rede caindo e ao servidor fora do ar | nada (testa depois da F7c) | **implementada (06-out-2026).** O motor da rodada, o cliente HTTPS, o laço em segundo plano montado no `cmd/agent` e a linha da nuvem na bandeja; a ponta a ponta com dois PCs e o celular contra a nuvem de verdade. As três decisões do plano usam a recomendação, **confirmadas pelo Mauro em 06-out-2026** (com a mudança na regra da fila). **Revista no mesmo dia:** a volta do PC que ficou fora (a reconexão e o `410`) não reenvia mais com a base vazia, que criava selos sem conflito e trocava o selo de verdade (a decisão 2 refeita; a derivada da lixeira saiu); a resposta até 16 MiB; a bandeja diz "aguardando" e "resposta inesperada"; o PC parado não escreve no banco a cada minuto (23 mutações, 23 mortas). Ver "Decisões da F7a/b" e "A revisão da F7a/b" |
| F7c | o envio inicial (prévia, cópia, 6 meses sem a lixeira, lotes por bytes), o selo de conflito no PC e o "Esquecer esta nuvem" | nada da lixeira sobe; o conflito guarda as duas versões; o histórico real não perde nem muda nada | a F7 inteira, pelo `docs/teste-manual-nuvem-f7.md`: conecta o `bin\`, envia o histórico, dita, edita nos dois lados, escolhe o selo no PC, a lixeira e o esquecer | **implementada (06-out-2026), autorizada pelo Mauro no mesmo dia.** A migration 0006 (aditiva, só a fila e, desde a reconferência, a tabela `sync_mudou_aqui`, vazia), provada contra um banco na 0005 com dados; o envio inicial com um clique (D1), a cópia antes (pela conexão única, conferida; a espera do ditado medida), os blocos curtos e o progresso no painel e na bandeja; a reconciliação do pull (D3); a escolha do selo no PC, que sobe pelo push (a regra nova em `cloudstore.DecidePush`, na memória e no Postgres; o Postgres não rodou); o "Esquecer esta nuvem", com a conta nova recebendo só o que nasceu aqui (D2). A prova com o `historico.db` do `bin\` copiado de um fixture e a ponta a ponta contra a nuvem de verdade. Nenhuma dependência nova. As três decisões do plano usam a recomendação, **confirmadas pelo Mauro em 06-out-2026** (a D3 com as correções da reconferência). **Revista no mesmo dia:** o esquecer não deixa mais o pull desfazer a mudança que não subiu (a referência recuava para antes dela; **reconferida** no mesmo dia, a referência voltou a ser o instante do esquecer, e o que mudou aqui sem a nuvem receber passou a ser anotado item por item, na `sync_mudou_aqui` da 0006, sem selo à toa no ditado recebido da nuvem; a pergunta do esquecer conta só as mudanças; a lixeira de um ditado com o lote em voo e o ditado de outro aparelho editado desconectado ficaram como risco aceito); conectado, o histórico só sobe pelo clique e pela completação (a edição, a lixeira e o restaurar de um ditado que a nuvem não tem não o põem na fila, e a lixeira tira o marcador de criar parado); a reconciliação não reconcilia mais o ditado que só a nuvem pôs na lixeira (não convergia); a linha da nuvem diz quantos não subiram; o roteiro confere o que dá para ver (21 mutações, 21 mortas). Ver "Decisões da F7c", "A revisão da F7c" e "A reconferência da F7c". **Testada pelo Mauro em 07-out-2026** com a nuvem de teste vazia (`-SemExemplos`) e o `bin\` recompilado: conectou; o ditado novo apareceu nos dois lados; a edição no app chegou ao PC em segundos e a do painel ao app; a lixeira valeu nos dois sentidos. O histórico antigo não subiu na primeira vez porque o "Enviar o histórico" não foi clicado (sem cópia, sem aceite e sem erro no log; a mesma conta do painel numa cópia do banco dava 42); o Mauro refez e disse "funcionou", mas **a conferência de 08-out-2026 mostrou que o histórico ainda não subiu**: o `historico.db` do `bin\` não tem o `nuvem_envio_aceito_em` nem a `nuvem_copia`, não há cópia `antes-da-nuvem` e não houve pareamento novo depois das 13:05 de 07-out; só os 14 ditados feitos depois de conectar têm versão da nuvem (o "funcionou" deve ter sido eles aparecendo no app). O envio do histórico fica para o roteiro da F8, que o repete (com o M) e confere no banco. O selo no PC e o esquecer não foram relatados à parte ("rodei tudo", "tá tudo OK") |
| F8 | o aprendido sincronizado (par curto, restrição e domínio; "não aplicado neste PC"; base em SQLite) | a restrição viaja junto; nada novo no JSON; sem eco | aprende num PC e vê no outro, ou no celular, e desfaz | **decidida (07-out-2026); implementada (F8a a F8i, 07 e 08-out-2026); falta o Mauro rodar o roteiro e confirmar as escolhas menores.** As quatro decisões do Mauro (D1 só o Aprender, D2 cria a palavra, D3 com o "Enviar o histórico", D4 vale o primeiro na nuvem), todas com a recomendação, e o desenho com as correções da crítica estão em "Decisões da F8". A F8.0 (os documentos) está feita. **A F8a a F8d estão implementadas e revisadas (07-out-2026):** o par no dicionário, o id e o PC que recebe (`internal/dictionary`, puro e sobre o arquivo); a nuvem sem banco (`DecideLearned`, o `delete` só no aprendido, a operação no `duplicate`, o pull e o `state` por tipo, o Store em memória); o Postgres (a coluna `operation` na 0009, corrigida no lugar; o ramo do aprendido no ponto único de gravação; a terceira consulta do pull no mesmo snapshot); e as rotas do celular (`GET /api/v1/learned`, `POST /api/v1/learned/undo`). `go test ./...`, `go vet ./...` e o `-tags logindeteste` do servidor verdes; nenhuma dependência nova. **Os testes de Postgres rodaram em 07-out-2026, verdes**, num contêiner novo que o Mauro subiu (ver "O estado da F8a a F8d"). Seis desvios do desenho, a confirmar na entrega. **A F8e está implementada (07-out-2026):** a 0007 do SQLite (o espelho `sync_aprendido` e o `sync_veio_da_nuvem`, aditiva, sem voltar o cursor) e o percurso do aprendido no `storage` (o executor do espelho, o marcador `learned`, o envio que segura o `upsert` antes do aceite, o `CompletarEnvio` com o M, o esquecer, o epoch novo e a pergunta do esquecer), com sete desvios e leituras a confirmar. **A F8f1 a F8f3 estão implementadas (07-out-2026):** o fio por tipo, as decisões puras do aprendido e a varredura do arquivo diante do espelho no começo da rodada, com o M, o freio (mais de 10 pares saindo à mão) e o envio do `learned` pelo motor, com catorze desvios e leituras a confirmar. **A F8f4 está implementada (07-out-2026):** o `Receber` com o aplicador (o arquivo relido sob a trava, a leitura curta do epoch e do pareamento antes de tocá-lo, a intenção antes da gravação e o espelho na transação da página), com seis desvios e leituras a confirmar. **A F8e a F8f4 estão revisadas e fechadas (08-out-2026):** sete correções da revisão, cada uma com o teste que falhava, e a última viva da mutação (o prazo de 30 dias vencia qualquer situação, e não só as etiquetas) morta por teste; `go test ./... -count=1` e `go vet ./...` verdes; os desvios 7 a 33 aceitos para seguir, a confirmar pelo Mauro na entrega. **A F8g está implementada, revisada e fechada (08-out-2026):** o Agent liga tudo (o aplicador em `internal/agent/aprendiz`, sob a trava do painel, vai ao laço na varredura e no `Receber`; o gancho do painel sobe a versão local no ato; o `GET /api/aprendizado` com a linha do arquivo e as linhas de estado; o `historico` com `previa_trocas` e `freio_trocas`; o log só com contagens), com a ponta a ponta de dois PCs e o celular sobre a nuvem em memória e o aplicador de verdade (o par com a restrição e o domínio, a D3, a D4 com as etiquetas, o desfazer do celular, o esquecer, a volta e a conta nova, o `dicionario.json` só com os campos de antes, o log sem texto). A revisão tratou nove achados distintos (oito no código, cada um com o teste que falhava, com a 0008 do SQLite, aditiva, para a troca que este PC perdeu; o do `503` no documento); o fechamento matou as duas mutações vivas (o `AplicarPares` sem a trava do `Aplicador` e um campo do log do lote) com testes novos. `go test ./... -count=1` e `go vet ./...` verdes; nenhuma dependência nova. Os desvios 34 a 44, a confirmar pelo Mauro na entrega. **A F8h (a web) está feita (07 e 08-out-2026):** o Aprendido no celular com o Desfazer, as linhas de estado no PC, a pergunta com o M, o aviso do freio e as duas Privacidades, vistas no navegador em 320 e 375 px, claro e escuro; juntas na `main` em 08-out-2026. **A F8i está feita (08-out-2026):** o roteiro `docs/teste-manual-nuvem-f8.md`, **pronto para o Mauro** (um PC só: o `bin\` recompilado e o app da nuvem no navegador do PC no lugar do celular; aprende uma troca antes de enviar, envia o histórico que ainda não subiu e confere, vê a troca no app, desfaz dos dois lados), e os documentos que a tabela de subfatias listava, com os textos velhos corrigidos; nenhum código, nenhuma migration, nenhum teste novo. O roteiro ainda não rodou. Ver "Decisões da F8", "O estado da F8a a F8d", "O estado da F8e", "O estado da F8f3", "O estado da F8f4", "A revisão da F8e a F8f4", "O fechamento da F8e a F8f4", "O estado da F8g", "A revisão da F8g", "O fechamento da F8g", "A F8h (07-out-2026)", "A F8i (08-out-2026)" |
| F9 | a retenção na nuvem e as lápides: o `Reter` por usuário (o ativo e a lixeira vencidos, decididos linha a linha em Go e de novo sob a trava; a lápide só depois da janela mais um dia), a arrumação do `sync_events` órfão e da `applied_mutations`, e o job no `serve`, na subida e de hora em hora | apagado não ressuscita de um PC parado; data fora da janela dá `gone`; com lápides apagadas fora de ordem de `seq`, o ditado de fim de mês não volta; a segunda rodada não faz nada; a arrumação poupa o evento de entidade viva e a resposta que um PC que volta ainda reenvia | nada | **decidida (08-out-2026); implementada (F9.0 a F9e), revista e fechada no mesmo dia; falta a rodada no Postgres de teste, antes do merge: nenhum teste de Postgres da F9 rodou.** O fechamento (a passada de mutação: três vivas não equivalentes, todas no `pgstore` e com teste que só o Postgres roda, uma delas com o teste novo do cursor dos candidatos) está em "O estado da F9". As quatro decisões do Mauro (D1 a D4, todas com a recomendação) e o desenho, com as correções da crítica, estão em "Decisões da F9". **Sem o `410` da marca d'água** (D2: a nuvem não o responde e o PC não muda; só o epoch novo recomeça); a cota de escrita por conta saiu para a F11 (D3). A F9.0 a F9e estão feitas; a revisão (a lixeira que não começa antes do ditado, a arrumação em lotes, o tique provado, a prova da D2) está em "A revisão da F9". **Depois do fechamento (08-out-2026):** o Mauro confirmou as escolhas menores 1 a 5 ("Confirmo todas") e decidiu o piso da lixeira que chega pelo push ("Fica 24 h na lixeira", a D5, feita em `851bed6`: a lixeira com o `deleted_at` antigo fica 24 h na lixeira da nuvem e do celular e então sai; a janela de 6 meses ganha do piso) |
| F10 | os 6 meses no PC, e a correção da D4 da F9: o PC só apaga de vez da lixeira o que a nuvem já soube, no máximo até os 6 meses do ditado | a contagem aparece antes; nada é apagado sem o OK (o portão recusa por padrão, lido dentro da transação); um ditado na lixeira que a nuvem ainda não soube não é expurgado; a cópia antes, o freio e o Desligar | vê a contagem e decide (pode parar nela); apaga as cópias quando quiser, pelo `docs/teste-manual-nuvem-f10.md` | **decidida (08-out-2026); implementada (F10.0 a F10f), revista e fechada no mesmo dia; falta o Mauro rodar o roteiro e confirmar as escolhas menores.** As quatro decisões do Mauro (D1 as Configurações com a confirmação na linha, D2 sai sozinho com Desligar e freio, D3 a lixeira espera a nuvem no máximo até 6 meses, D4 o botão das cópias), todas com a recomendação, e o desenho com as correções da crítica estão em "Decisões da F10". A F10.0 (os documentos), a F10a (a guarda da D3 na lixeira), a F10b (os 6 meses no storage, atrás do portão), a F10c (a cópia, o Ligar, o Desligar, o freio e o passo no ciclo do Agent), a F10d (as rotas locais e o bloco Histórico nas Configurações), a F10e (o botão Apagar as cópias) e a F10f (os documentos e o roteiro `docs/teste-manual-nuvem-f10.md`, **pronto para o Mauro**) estão feitas. A revisão (três médios e oito baixos, todos feitos, cada um com o teste que falhava) está em "A revisão da F10"; o fechamento (a passada de mutação: uma viva não equivalente, o cursor sem o desempate pelo id, morta por teste novo) em "O fechamento da F10". A regra liga pela tela, mas só pelo clique do Mauro: a primeira purga real é esse clique no `bin\` dele, pelo roteiro, e nenhum agente liga a regra. O mais provável é não haver ainda ditado com mais de 6 meses no `bin\` (o primeiro é de setembro de 2026), e então o roteiro para na contagem e mostra a pergunta pelas telas de desenvolvimento. A correção da D4 foi confirmada em 08-out-2026 (ver "Decisões da F9"). Vem sobre a F9, que espera a rodada no Postgres de teste antes do merge |
| F11 | deploy (**ponto de parada**): o servidor pronto para contêiner, a imagem, os scripts e o backup (`age` e Backblaze B2), a restauração com epoch novo, o compose e a borda que a D1 mandar, e o roteiro, testados no Docker do PC com `exemplo.test` | sobe; a retenção roda antes de a porta abrir; faz backup e restaura, com epoch novo, num contêiner descartável; nenhum serviço monta o socket do Docker | o Google de verdade no PC antes do deploy (`scripts/nuvem-local.ps1`); roda no servidor, depois da revisão de LGPD. Teste final em duas partes: instalar no Android, entrar, editar no celular e ver no PC, revogar, ficar sem internet e voltar (absorve os testes à mão da F8 e da F10) | **decidida em parte (09-out-2026): D2 (backup cifrado com `age`, no B2, sem poder apagar, giro de 30 dias), D3 (LGPD pelo dossiê, aprovado no chat), D4 (a imagem por `ssh`, sem `git push`) e a restauração (o apagado depois do backup pode voltar; fica escrito) confirmadas pelo Mauro; a D1 (onde a nuvem mora e quem atende o https) espera o diagnóstico da VPS.** O desenho da Parte 1, que não depende da D1 (o `*_FILE`, o `healthcheck` e a retenção antes da porta, o pós-restauração, a imagem, o papel do Postgres, os scripts da VPS e do PC, a varredura de segredos e o dossiê de LGPD), as subfatias das duas partes, o que é do Mauro e o que fica para depois da D1 (o compose, a borda, o roteiro) estão em "Decisões da F11". **[09-out-2026: a Parte 1 está na `main`, com a revisão; a D1 foi confirmada pelo Mauro no mesmo dia (uma VPS, atrás do Traefik que já roda, no Brasil, num domínio próprio), e a Parte 2 está escrita (o compose, o agendador do backup, o `subir.sh`, os scripts do PC e os roteiros): ver "A Parte 2 da F11". Falta o Mauro: o B2, a LGPD aprovada, o deploy pelo `docs/deploy-f11.md` e o teste final.]** Nada de deploy, DNS, proxy ou segredo real feito pelo agente |

### Decisões da F1 (01-out-2026)

O que a F1 decidiu dentro do que a ADR deixou para ela, e o que ela **não** provou.

- **Onde mora o `migrate`.** Um **subcomando do binário do servidor**: `aurora-whisper-server migrate`
  (e `migrate -status`, que só lê). A ADR e o `05` dizem só "comando próprio do servidor". Por que não
  um binário à parte: seria mais um artefato para construir, versionar e levar ao servidor, e as
  migrations vão **embutidas** (`migrations.FS`) no binário que as aplica, então ele e o esquema que
  espera não se separam. Por que não uma opção do servidor: a subida nunca altera o esquema (`05`,
  "Integridade"). A URL do banco vem só de `DATABASE_URL`, nunca de uma opção (iria para o histórico
  do shell). O código está em `internal/server/pgmigrate` (leitor e planejador puros, mais a parte com
  `pgx`) e `cmd/server`.
- **Controle de versão.** `schema_migrations` (versão, nome, checksum SHA-256 com fim de linha
  normalizado, quando); uma transação por arquivo, com o registro na mesma transação; `pg_advisory_lock`
  contra dois `migrate` ao mesmo tempo; recusa de arquivo alterado depois de aplicado, de aplicada que o
  binário não tem, de renomeada e de pendente com número menor que uma aplicada; recusa de banco que
  não é UTF8. O detalhe está em `migrations/README.md`.
- **O `pgx` (decisão 14), justificado.** `github.com/jackc/pgx/v5` v5.11.0 (07-set-2026), MIT, com
  `github.com/jackc/pgpassfile` e `github.com/jackc/pgservicefile` (MIT): **três módulos novos** no
  `go.mod`. Por que ele, e não `database/sql` com `github.com/lib/pq` (v1.12.3, abr-2026):
  - Os dois são Go puro e mantidos em 2026, então manutenção **não** desempata. Conferido:
    `CGO_ENABLED=0 go build ./cmd/server` passa para `windows/amd64`, `linux/amd64` e `linux/arm64`
    (a arquitetura da VPS não foi vista); nenhum pacote da árvore do servidor usa cgo.
  - O `pgx` fala o protocolo nativo, com os tipos que as tabelas usam (`uuid`, `bytea`, `timestamptz`,
    `jsonb`, arrays) sem a camada de conversão do `database/sql`, e o erro traz o `SQLSTATE`
    estruturado (`pgconn.PgError`), que os testes de constraint usam.
  - Traz o `pgxpool` no mesmo módulo, que a F2 e a F3 vão precisar (transação curta por pedido). A F1
    não o importa (o `migrate` usa uma conexão só, porque a trava é da sessão); quando a F2 o
    importar, o `go.mod` ganha mais dois módulos (`github.com/jackc/puddle/v2` e `golang.org/x/sync`),
    e a F2 os justifica. Também tem `CopyFrom` e envio em lote, úteis ao envio inicial em lotes (F7c).
  - **O custo, sem esconder:** o `lib/pq` não traz nenhum módulo a mais, e o `pgx` traz três (e puxa o
    `golang.org/x/text`, que o `go.mod` já tinha, para o SASLprep do SCRAM: o binário do servidor ficou
    com cerca de 14 MB). O **Agent não leva o `pgx`**: `go list -deps ./cmd/agent` não tem nenhum pacote
    de `jackc`, e o instalador monta as licenças pela lista de dependências do Agent. Em termos de
    capacidade, qualquer um dos dois servia; a escolha é de conforto de tipos e de pool, e foi a que a
    ADR já fixou.
  - A linha de licença está em `THIRD_PARTY_NOTICES.md`.
- **A tabela dos tokens de aparelho** ficou **ao lado** de `devices` (`device_tokens`), e não em
  colunas dela (a ADR e o `05` deixavam a F1 decidir): o segredo nunca vai junto num `SELECT` de
  aparelho, e `last_used_at` (escrito a cada chamada) não mexe na `version` do aparelho.
- **Isolamento no esquema, além do pedido:** `UNIQUE (user_id, id)` em `devices` e chaves estrangeiras
  compostas `(user_id, device_id)` em `transcriptions`, `sync_events` e `applied_mutations`: o banco
  recusa apontar para o aparelho de outro usuário. `created_at` de `transcriptions` sem `DEFAULT` (é a
  data do ditado, a do PC). Segredo só como hash ou HMAC, com `CHECK` de 32 bytes. Só ditado deixa
  lápide; o aprendido é desfeito por `undone_at`. A PK do aprendido é `(user_id, id)`, porque o id é o
  mesmo em todo usuário. Os nomes finais de tabela e coluna estão no `05` e nas migrations 0010 a 0016.
- **`pending_logins` e `pairing_requests` não têm `user_id`**: existem antes de o usuário ser conhecido
  (a segunda é escrita por um endpoint sem sessão, e por isso os textos dela têm limite no banco).
- **Revisão da F1 (01-out-2026): o que mudou depois da primeira entrega**, tudo ainda dentro de
  "corrigir no lugar", e o que precisa da confirmação do Mauro está marcado.
  - **A PK de `transcriptions` é `(user_id, id)`. [Confirmado pelo Mauro em 05-out-2026; o `05` dizia `id
    uuid PK`.]** O id do ditado é o UUID v7 que o **PC** gerou: vem do cliente. Com a PK global, o mesmo
    `historico.db` ligado a duas contas (a real e a de teste, D7), ou um cliente com defeito, faria o
    push de uma conta colidir com a linha de outra, e o erro (ou a resposta) mostraria que o id existe em
    outra conta: o mesmo motivo pelo qual a ADR já tirou a PK global de `applied_mutations`. O aprendido
    (`(user_id, id)`) e as lápides (`(user_id, entity_type, entity_id)`) já eram por usuário. Custo: o
    upsert do push da F2 é `ON CONFLICT (user_id, id)` (**substituído pela F2**: o push lê a linha
    `FOR UPDATE` e faz o `INSERT` ou o `UPDATE`, sem `ON CONFLICT`; ver "Decisões da F2"), e a chave
    estrangeira de `corrections` (0006, sem uso na etapa 5) virou composta, com `ON DELETE SET NULL
    (transcription_id)` (a lista de colunas é do PostgreSQL 15 em diante; o alvo é o 18).
    **A alternativa**, se o Mauro preferir a PK global: manter
    `id` como PK e a F2 responder a colisão com a linha de outra conta como `rejected`, sem dizer que a
    linha existe, com um teste; desfaz-se trocando a PK de volta (as duas são migrations ainda não
    aplicadas).
  - **`applied_mutations` guarda a entidade e o aparelho, e os três são `NOT NULL`**: `entity_type`,
    `entity_id` e `device_id`. Antes, o duplicado era decidido só por `(user_id, mutation_id)`: um
    `mutation_id` reusado para **outra** entidade (cliente com defeito) viraria `duplicate`, e a
    mudança se perderia em silêncio, contra a meta "Sync confirmado perdido: 0". Agora a F2 compara a
    entidade do reenvio com a gravada e responde `rejected` quando for outra. O push só vem de um
    aparelho pareado, então o aparelho nunca é nulo.
  - **O `state` do pareamento é o `id` do pedido**, sem coluna a mais: o `POST /device-auth/start`
    devolve o `pedido`, a `auth_url` o leva, e o painel local confere que o `state` de
    `/nuvem/retorno?code&state` é o do pedido em curso (`05`, "PedidoDePareamento"; `06`). Quem protege
    a troca do `code` é o PKCE; o `state` só liga o retorno ao pedido. **[Proposta; a F6 confirma.]**
  - **`pending_logins.return_to`**: para onde o callback leva a pessoa no fim do login (o caso de
    `/device/authorize`, que exige sessão). É um **caminho** da própria origem, nunca uma URL, com um
    `CHECK` contra open redirect (`/` no começo, sem `//`, sem barra invertida, até 256 caracteres), e o
    `POST /auth/start` ganha o corpo opcional `volta` (`06`). **[Proposta; a F3 confirma.]**
  - **A regra de "corrigir no lugar".** A ADR (decisão 13) autorizou corrigir a **0001-0009** no lugar
    **na F1**. A 0010-0016 são novas da própria F1, e as correções desta revisão as tocam (0012) ou
    tocam a 0001-0009 de novo (0003, 0006, 0009) depois da entrega da F1: isso **estende** o que a ADR
    disse. **Confirmado pelo Mauro em 05-out-2026:** corrigir uma migration no lugar vale **até a primeira
    aplicação fora de um banco de teste descartável, que é o deploy (F11)**; depois, só migration nova.
    A razão é a mesma da decisão 13: remendar numa migration nova o que ninguém criou só acumula
    história falsa. O `migrate` já protege o outro lado: um banco que aplicou uma versão antiga recusa o
    arquivo alterado por checksum, e a saída é apagar o banco de teste. Sem a confirmação, as fatias
    seguintes que precisarem de uma coluna a mais abrem uma migration nova (0017 em diante).
- **Testes.** Sem Postgres, rodam sempre: o leitor e o planejador (`pgmigrate`), o `schema_test.go`
  ampliado e as leituras do script (`pgtest`). Com `AURORA_TEST_POSTGRES_URL`: o `postgres_test.go`
  (isolamento por usuário, o contador `UPDATE ... RETURNING` sem buraco com rollback e serializando
  dois escritores, as constraints tabela a tabela), o `pgmigrate/run_test.go` (do zero, de novo, arquivo
  alterado, dois `migrate` simultâneos, falha no meio, trava) e o `cmd/server/migrate_pg_test.go`. Cada
  um cria um banco `aw_test_<hex>` e o apaga no fim, e recusa um servidor que não seja a própria máquina.
  `scripts/postgres-de-teste.ps1` sobe o contêiner (127.0.0.1, senha aleatória, sem volume).
- **O que a F1 NÃO provou.** Nada disso rodou contra um Postgres: o Docker Desktop da máquina onde ela
  foi escrita estava parado, e ninguém o iniciou. Os testes com Postgres **compilam e pulam**, e a
  lógica deles foi conferida por leitura. O SQL nunca passou por um Postgres: o que o confere hoje é
  leitura e um lint léxico. O script do contêiner também nunca rodou (o parser do PowerShell 5.1 o
  aceita). A primeira execução real é a do Mauro, e o que ela mostrar volta para a F1.
  **Rodou em 05-out-2026:** o Mauro subiu o contêiner com o script e rodou os testes de `migrations`,
  `pgmigrate`, `pgtest` e `cmd/server` com `AURORA_TEST_POSTGRES_URL`; os quatro passaram, sem
  nenhuma volta para a F1. Os tempos (4,45 s e 1,83 s, contra 0,24 s e 0,26 s quando pulam) mostram
  que os testes do esquema e do `migrate` rodaram de verdade.
- **Para as fatias seguintes:** quem cria o usuário (F3) cria também a linha de `user_sync_counters`,
  na mesma transação, com o `sync_epoch` (UUID v7); quem sobe a marca d'água usa `GREATEST` (desde a
  D2 da F9 só o "apagar meus dados" a sobe, e ninguém a lê); o
  `status` `completed` do servidor em memória vira `done` na F2; o upsert do ditado é
  `ON CONFLICT (user_id, id)` (substituído pela F2 por `SELECT ... FOR UPDATE` e `INSERT` ou `UPDATE`,
  confirmada pelo Mauro em 05-out-2026; ver "Decisões da F2"), e a F2 compara a entidade de um reenvio com a de `applied_mutations`.
  Se a F6 ou a F3 precisarem de mais uma coluna, vale a regra de "corrigir no lugar" da revisão acima
  (confirmada pelo Mauro em 05-out-2026). Três coisas que o
  esquema **pressupõe** e que não se veem no SQL (estão também no comentário da 0008 e da 0010):
  - **O contador sem buraco e serializado depende do isolamento `READ COMMITTED`**, o padrão do
    Postgres. Em `REPEATABLE READ` ou `SERIALIZABLE`, o segundo escritor do mesmo usuário recebe
    `40001` (falha de serialização) em vez de esperar a trava. A transação de gravação da F2 é
    `READ COMMITTED`, e quem a mudar tem de tratar o `40001`.
  - **O pull só vê um prefixo consistente de `seq` se ler as entidades e as lápides num snapshot só**:
    uma consulta, ou uma transação somente leitura em `REPEATABLE READ`. Duas consultas soltas em
    `READ COMMITTED` podem ver uma gravação confirmada entre uma e outra, e o cursor pularia a mudança.
  - **O `device_id` de origem que o pull devolve vem de `sync_events`**, juntando `last_seq` da
    entidade ao evento de mesmo `seq`. Se a arrumação por idade da F9 apagar o evento que é o
    `last_seq` de uma entidade **viva**, a origem some (o `learned_pairs` não tem coluna de aparelho), e
    nada no esquema impede isso: a arrumação da F9 só apaga evento cujo `seq` nenhuma entidade viva
    guarda como `last_seq`, ou a origem passa a ser guardada na própria entidade.

### Decisões da F2 (05-out-2026)

O que a F2 decidiu dentro do que a ADR deixou para ela, o que rodou e o que ela **não** provou.

- **Onde mora o quê.** Três pacotes, e a mesma regra nos dois servidores:
  - `internal/server/cloudstore`: os tipos do ditado na nuvem, as **regras puras** (validar o payload,
    mesclar, acusar conflito, recusar, responder `gone`), a interface `Store` e o **Store em memória**;
  - `internal/server/pgstore`: o `Store` no Postgres (o ponto único de gravação, `gravar`, e as
    leituras);
  - `internal/server/contracttest`: a **suíte de contrato pela API**, que roda contra os dois Stores
    (`internal/server/store_contract_test.go` e `internal/server/pgstore/store_pg_test.go`). O servidor
    em memória e o de Postgres têm o mesmo contrato por construção: as regras são as mesmas funções, e
    os testes são os mesmos.
- **Como o servidor liga isso.** `server.Deps.Store`: com ele, o servidor monta só health, `/me`,
  `/transcriptions` e `/sync/push|pull`, e toda escrita passa pelo `Store`. Sem ele, continua o servidor
  em memória da F1-D, com o contrato antigo de sync (genérico, sem `gone` nem mescla): ele só existe
  para os testes do cliente antigo (`internal/sync`, `internal/server/integration`), que a F7a refaz;
  quando ela refizer, o modo antigo sai. O `status` `completed` do servidor antigo virou `done`.
- **O ponto único de gravação.** Toda escrita (o CRUD do celular, o push do PC e o "excluir
  definitivamente", que o `Store` já tem e a F3 monta como rota) é uma transação `READ COMMITTED` cujo
  primeiro comando é o `UPDATE user_sync_counters ... RETURNING last_seq, sync_epoch`; depois, a
  entidade (`FOR UPDATE`), o `sync_event` com o mesmo `seq` e, no push, a `applied_mutation`.
  **Decisão que não grava entidade não gasta `seq`:** um reenvio, uma recusa, um `gone`, uma lixeira
  que já estava lá. O número volta por um `UPDATE ... last_seq - 1` na mesma transação, ainda com a
  trava (ninguém o viu), e a recusa e o `gone` ficam gravados em `applied_mutations` para o reenvio
  receber `duplicate`. Assim os `seq` de cada usuário são 1, 2, 3... sem buraco. Uma falha no meio
  desfaz tudo, e o rollback devolve o número também.
  O upsert do ditado é por `(user_id, id)`, como a revisão da F1 previa, mas **sem** `ON CONFLICT`: as
  regras precisam do estado atual antes de decidir (mesclar, acusar conflito, `gone`), então a
  transação lê a linha `FOR UPDATE` e faz o `INSERT` ou o `UPDATE` (este com `AND version = <a lida>`).
  Com a trava do contador, dois pushes do mesmo ditado não correm juntos; se um dia correrem, o
  `INSERT` bate na PK `(user_id, id)` e a transação desfaz, sem gravar nada pela metade.
  **[Desvio do que a F1 escreveu, achado na revisão da F2; confirmado pelo Mauro em 05-out-2026.]** A revisão da F1 e a
  decisão do Mauro diziam "upsert `ON CONFLICT (user_id, id)`" (as duas frases, em "Decisões da F1",
  estão marcadas como substituídas). A chave, que é o que o Mauro decidiu, é a mesma; muda só a forma
  de gravar, e o contrato não muda. Se o Mauro preferir o `ON CONFLICT` literal, o `INSERT` do ditado
  novo ganha `ON CONFLICT (user_id, id) DO NOTHING` e confere as linhas afetadas, sem mexer nas regras.
- **Só o ditado.** O `learned` no push é `rejected` com `tipo_ainda_nao_sincroniza` até a F8, e o pull
  não lê `learned_pairs` (a F8 acrescenta, no mesmo snapshot).
- **O formato do payload** (a ADR dizia "lista fechada de campos por tipo"). **[Decisão da F2; a F7a
  confirma.]**
  - **Criar** (`base_version` 0): o ditado inteiro, e só estes campos: `source_platform`, `raw_text`,
    `final_text`, `mode`, `language`, `asr_provider`, `asr_model`, `duration_ms`, `processing_ms`,
    `confidence`, `status`, `created_at` e `deleted_at`. `device_id` não vem no payload: é o aparelho
    do token. Campo fora da lista é `campo_desconhecido` (`target_app`, áudio, contexto, `id`, qualquer
    outro).
  - **Mudar** (`base_version` >= 1): **só o que o PC mudou desde a base**, entre `final_text` e
    `deleted_at` (`null` é restaurar). Por quê: o servidor não guarda o estado de cada versão antiga, e
    o PC manda o conteúdo da linha na hora do envio (a fila é um marcador por entidade). Se o PC
    mandasse o ditado inteiro, o servidor não saberia separar "o PC mudou o texto" de "o PC tem o texto
    velho", e a lixeira de um lado com o texto velho do outro viraria conflito à toa. **Para a F7a:** o
    marcador da fila guarda **quais campos** mudaram (o texto, a lixeira), sem texto nenhum.
  - **Recomeço:** o ditado inteiro com `base_version` 0 sobre um que o servidor já tem é comparado com
    tudo o que mudou (ele não conhece versão nenhuma): o mesmo texto é mescla, outro texto é conflito.
  - `delete` de ditado é `rejected` (`operacao_invalida`): a lixeira é o `deleted_at` do upsert, e
    apagar de vez é só da nuvem ("excluir definitivamente", F3; a retenção, F9). O `delete` fica para o
    desfazer do aprendido (F8).
- **As regras do conflito, exatas** (`cloudstore/rules.go`, com a tabela de testes
  `TestDecidePush_RegrasDeMescla`). O servidor guarda, em cada ditado, a versão em que o texto mudou por
  último (`text_version`) e a versão em que a lixeira mudou por último (`trash_version`); a 0003 foi
  **corrigida no lugar** para isso (vale até o deploy). Com a base do PC:
  - o mesmo texto, ou os dois na lixeira: a mesma mudança, não é conflito;
  - **texto contra texto** (o servidor mudou o texto depois da base, e o PC mandou outro): vale o que
    chegou primeiro; o texto do PC vai para o selo, e a resposta é `conflict`;
  - **campos diferentes** (a lixeira ou o restaurar de um lado, o texto do outro): mescla, `applied` com
    `mesclado: true`;
  - **apagar vence:** pôr na lixeira sempre vale; restaurar contra uma lixeira mais nova que a base não
    vale (o item fica na lixeira, e isso é mescla, porque nenhum texto se perde); uma edição de texto
    sobre uma base anterior à ida para a lixeira (a "edição antiga") **não** muda o texto do item na
    lixeira: vai para o selo (`conflict`), como o painel já faz (`web/src/edicao.ts`: gravar num item da
    lixeira é conflito). Na ordem inversa (a edição chega antes, a lixeira com base velha depois), é
    mescla: o item vai para a lixeira com o texto editado;
  - **"depois da base" é de outro aparelho** (revisão da F2). O servidor guarda também quem fez a
    última mudança de cada campo (`text_device_id`, `trash_device_id`; nulo quando foi o celular; a
    0003 corrigida no lugar de novo). O campo cuja última mudança veio do aparelho que manda a mutação
    conta como conhecido por ele, mesmo com a versão acima da base. Ver "Revisão da F2", abaixo.
- **O `conflict` grava**: o selo entra no item com versão e `seq` novos, e chega a todos pelo pull. A
  resposta traz a versão nova e o `state` (o item com o selo). **O `applied` mesclado sempre grava uma
  versão nova**, mesmo quando nenhum campo muda (a mesma mudança com base velha): o PC pulou, num pull
  anterior, a mudança que veio depois da base (o item tinha marcador), e só uma versão nova, com `seq`
  novo, a traz no pull seguinte. É a outra metade de "Eco e mescla".
- **O `duplicate`** repete a primeira resposta, lida de `applied_mutations`: `"status": "duplicate"`,
  `"original"` (o status da primeira vez), `entity_version`, `mesclado`, `code` ou `motivo`. Não traz o
  `state` (a tabela não guarda texto). O mesmo `mutation_id` para outra entidade, ou mandado por
  **outro aparelho** do mesmo usuário (revisão da F2), é `rejected` (`mutacao_reusada`). A mutação
  cujo `mutation_id` ou `entity_id` não é UUID nem é gravada
  (`mutacao_invalida`), e o reenvio recebe `rejected` de novo.
- **`gone`:** com lápide, `apagado`; ditado fora da janela de 6 meses (pela data do ditado, a mesma
  conta de `internal/retention`), ao criar ou ao mudar, `fora_da_janela`; e uma mudança (`base_version`
  >= 1) de um ditado que a nuvem não tem nem tem lápide também é `fora_da_janela` (a lápide já venceu,
  ou os dados foram trocados, e aí o epoch muda e o PC descarta a resposta).
- **A folga para o futuro é de 24 h** (`data_no_futuro`, para `created_at` e para `deleted_at`). Um dia
  deixa passar o relógio de um PC um pouco adiantado sem tirar o ditado do sync, e alonga a retenção na
  nuvem em no máximo um dia.
- **Os nomes finais:** `mesclado` (o nome final que a ADR pedia), `original`, `code`, `motivo`, `state`
  e `sync_epoch` na resposta do push. Os códigos de `rejected`: `mutacao_invalida`, `tipo_desconhecido`,
  `tipo_ainda_nao_sincroniza`, `operacao_invalida`, `campo_desconhecido`, `campo_invalido`,
  `data_no_futuro`, `base_invalida`, `mutacao_reusada`. O selo sai como `versao_em_conflito`
  (`final_text`, `device_id`, `conflict_at`); os nomes de escolher uma versão continuam da F7c.
- **Quem pode sincronizar:** push e pull **só com o token de um aparelho**; a sessão do navegador (o
  celular) recebe `403`. O aparelho e o usuário vêm da credencial; um `device_id` ou `user_id` no corpo é
  ignorado. (O token do PC no CRUD continua passando até a F3 fechar.)
- **O pull** lê o epoch, as entidades e as lápides numa transação somente leitura em `REPEATABLE READ`.
  O `device_id` da mudança é o do `sync_event` da última mudança (nulo quando veio do celular); o
  `device_id` do payload é o aparelho onde o ditado nasceu. `deleted_at` e `versao_em_conflito` vêm
  sempre, `null` quando não há, para o PC saber que o item saiu da lixeira ou que o selo foi limpo. O
  `410` e a marca d'água **não** estão na F2 (F9): a F9 lê a marca d'água na mesma linha do contador
  que o pull já lê para o epoch, no mesmo snapshot. **[F9, D2, 08-out-2026: a marca não ganha leitor;
  o pull continua a ler só o epoch.]**
- **Limites:** até 500 mutações por push (`400` acima), dentro do teto de 1 MiB do corpo, e até 500
  mudanças por pull. [ASSUMPTION] A F7c fixa o lote do envio inicial (por número e por bytes).
  Na revisão da F2, mais dois, os dois **[confirmados pelo Mauro em 05-out-2026]**:
  - **64 KiB por texto** (`raw_text` e `final_text`; o selo é um `final_text` que perdeu, e herda):
    acima, `campo_invalido` no push e `400` no `PATCH`. Um ditado tem no máximo 2 minutos de gravação,
    uns 3 KiB de texto;
  - **2 MiB de texto por página** do pull e da listagem: a página para no item em que o texto
    acumulado (bruto, final e selo) chega a 2 MiB, sempre com pelo menos um, e diz que há mais
    (`has_more`, `next_cursor`). O Postgres para de ler as linhas uma depois desse item, então a memória
    de um pedido fica no orçamento. Uma página de ditados comuns (500 de 1 a 3 KiB) cabe inteira.
- **As rotas do CRUD na F2:** `GET /transcriptions` (ainda com `q` na URL; a F3 move a busca para
  `POST /transcriptions/search`), `GET /transcriptions/{id}` (nova: o painel a usa depois de um `409`),
  `PATCH` (só sobre a versão atual, `409` com a velha), `DELETE` (lixeira; de novo é `204` sem gravar) e
  `POST .../restore`. A lixeira sai pela data de exclusão, como a API local. `POST /transcriptions` não é
  montada (D1); as rotas que a decisão 9 tira também não.
- **A dependência nova, justificada:** o `pgxpool`, do próprio `pgx`, que a F1 já previa. Traz **dois
  módulos novos** no `go.mod`: `github.com/jackc/puddle/v2` v2.2.2 (MIT, dos autores do `pgx`: o pool
  genérico que o `pgxpool` usa) e `golang.org/x/sync` v0.22.0 (BSD-3-Clause, do time do Go: o
  semáforo do `puddle`). Por quê: o servidor faz transações curtas e concorrentes por pedido, e uma
  `pgx.Conn` não pode ser usada por dois pedidos ao mesmo tempo; um pool à mão seria mais código nosso
  para revisar do que dois módulos pequenos e mantidos. A alternativa sem módulo novo era o
  `database/sql` com o adaptador `stdlib` do `pgx`: serviria, mas troca a API de transação (`pgx.Tx`,
  que o `migrate` já usa) por outra só para o servidor. O **Agent não leva** nenhum dos dois:
  `go list -deps ./cmd/agent` não tem `jackc` nem `x/sync` (conferido). A linha está em
  `THIRD_PARTY_NOTICES.md`.
- **O que rodou contra o Postgres 18** (o contêiner de `scripts/postgres-de-teste.ps1`, 05-out-2026):
  a suíte de contrato inteira (17 casos: AC-016 pela API, `duplicate` com o mesmo resultado para
  `applied`, `rejected`, `gone` e `conflict`, `mutation_id` reusado, a mescla com um pull no meio e o
  `duplicate` que a repete, texto contra texto, apagar vence edição antiga nas duas ordens, `gone`, as
  recusas, só com token de aparelho, as páginas do pull com lápide, o isolamento, inclusive o mesmo id e
  o mesmo `mutation_id` em duas contas, o CRUD, as rotas não montadas, nada de texto ditado em log, oito
  escritores concorrentes com um leitor fazendo pull ao mesmo tempo, e oito reenvios simultâneos do
  mesmo `mutation_id`) e as provas que só o banco dá: o rollback devolve o número, a decisão que não
  grava não gasta `seq`, o segundo escritor espera a trava (sem depender de sorte), o pull num snapshot
  só (uma edição e uma exclusão definitiva gravadas entre as duas leituras), o mesmo id em duas contas
  são duas linhas, e o erro do Postgres sem o texto ditado. Tempos: `pgstore` 2,67 s, `migrations`
  3,90 s, `pgmigrate` 1,81 s; sem a variável, 0,30 s, 0,33 s e 0,30 s (pulam).
- **Mutação.** Em cópias feitas por `git archive`, com a mutação conferida antes e a restauração
  conferida byte a byte depois: 29 mutações nas regras puras, no Store em memória e nas rotas, todas
  mortas pelos testes puros e pela suíte em memória; e 19 contra o Postgres, 18 mortas. A que sobrevive
  é tirar o `FOR UPDATE` da leitura da entidade: mutação equivalente, porque a trava do contador já
  enfileira toda escrita do usuário (o `FOR UPDATE` fica como segunda guarda). Duas mutações acharam
  buraco e viraram teste: tirar o escape do `LIKE` da busca, e um cursor que não anda prendia o teste
  até o prazo (agora o laço tem teto).
- **Revisão da F2 (05-out-2026).** O que mudou depois da primeira entrega, tudo provado contra o
  Postgres 18 de teste; o que precisa da confirmação do Mauro está marcado.
  - **A mudança mesclada do próprio PC.** **[Opção A da revisão, confirmada pelo Mauro em 05-out-2026.]** Depois
    de um `applied` mesclado, o PC não guarda a versão devolvida ("Eco e mescla") e a mudança seguinte
    do mesmo item sai com a base velha. O servidor comparava `text_version`/`trash_version` com essa
    base e tomava a mudança do **próprio** PC por uma de outro aparelho. Provado com duas sondas: **S1**
    (o celular edita, v2; o PC põe na lixeira com base 1, mesclado, v3; o PC restaura com base 1): o
    item ficava na lixeira, sem selo, e o restaurar sumia sem aviso; **S2** (o celular põe na lixeira
    e restaura, v3; o PC edita "A" com base 1, mesclado, v4; o PC edita "B" com base 1): `conflict`, e
    o texto mais novo do PC ia para o selo. **A correção:** a 0003 ganha `text_device_id` e
    `trash_device_id` (corrigida no lugar, que vale até o deploy; chaves estrangeiras compostas, como
    as outras colunas de aparelho), e o campo cuja última mudança veio do aparelho que manda a mutação
    conta como conhecido por ele. É seguro porque essa é a **última** mudança do campo, e o aparelho
    tem o valor que ele mesmo mandou; o campo mudado por outro aparelho segue as regras de sempre
    (apagar vence; texto contra texto é conflito), e o celular (sem aparelho) nunca é "o mesmo". O
    contrato do PC não muda. Vale também no recomeço (base 0): o texto que o próprio PC criou e
    ninguém mudou é dele. **A alternativa (opção B):** o `applied` mesclado devolve o `state`, como o
    `conflict`, e o PC aplica os campos sem marcador e passa a usar a versão nova como base; muda o
    contrato da F7a e o "Eco e mescla". Para desfazer a A: tirar as duas colunas da 0003 (ainda não
    aplicada fora de banco de teste) e o `sameDevice` de `cloudstore/rules.go`. S1 e S2, e os dois
    controles com outro PC, são casos da suíte de contrato.
  - **O `duplicate` confere o aparelho.** O mesmo `mutation_id` mandado por outro aparelho do mesmo
    usuário, para a mesma entidade e com outro conteúdo, recebia `duplicate` e a mudança se perdia em
    silêncio (um `historico.db` copiado para um segundo PC com marcadores pendentes, se a F7a guardar o
    `mutation_id` no marcador). Agora é `rejected` (`mutacao_reusada`), como o reuso para outra
    entidade. A `applied_mutations` já guardava o `device_id`.
  - **O teto do texto e o orçamento da página** (em "Limites", acima). Antes, só valia o teto de 1 MiB
    do corpo: três ditados de 0,88 MiB entravam, o pull devolvia 2,5 MiB, e 500 mudanças por pull (ou
    100 itens por página) fariam uma resposta de centenas de MiB, montada inteira em memória, a partir
    de um pedido mínimo.
  - **Busca e modo com o caractere nulo ou UTF-8 inválido** (`?q=%00`, `?q=%FF`, o mesmo no `mode`)
    davam `500` no Postgres (SQLSTATE 22021) e `200` vazio em memória. Agora os dois Stores respondem
    `400` (`cloudstore.CheckListFilter`), e a suíte cobre.
  - **O isolamento da lápide e do "excluir definitivamente" pelo mesmo id em duas contas** não era
    testado: tirar o `user_id` da consulta da lápide ou do `DELETE` passava na suíte inteira. Agora a
    suíte de contrato tem A e B com o mesmo id na lixeira, A exclui de vez, e confere que a linha de B
    fica, que o pull de B não traz a lápide de A, que o push de B no próprio item é `applied`, e que B
    cria um id que só A apagou.
  - **O `ON CONFLICT`:** ver o desvio marcado em "O ponto único de gravação", acima.
- **O que rodou na revisão** (05-out-2026, o mesmo contêiner): a suíte de contrato com **19 casos**
  (mais "o campo que o próprio PC mudou é conhecido por ele", com S1, S2, os dois controles e o
  recomeço, e "teto do texto e páginas por bytes", com uma lápide no meio), mais um teste do Postgres
  que prova que a leitura, e não só a resposta, para no orçamento. Tempos: `pgstore` 3,40 s,
  `migrations` 4,07 s, `pgmigrate` 1,85 s; sem a variável, 0,30 s, 0,30 s e 0,29 s (pulam).
  **Mutação da revisão**, do mesmo jeito (cópia por `git archive`, a troca conferida, timeout conta como
  falha): 28 nas regras puras, no Store em memória e nas rotas, e 17 contra o Postgres. Todas mortas, duas
  só depois de um teste a mais: o restaurar pelo push sem gravar o aparelho (o caso do teste já tinha o
  mesmo aparelho) e o `INSERT` sem os aparelhos (nenhum caso dependia do aparelho da criação; virou o
  caso do recomeço). As três mutações de isolamento (a lápide, o `DELETE` e as lápides do pull sem o
  `user_id`) foram rodadas também na versão de antes: duas sobreviviam, e a terceira morria por acaso.
  Duas são equivalentes na saída e só morrem pelo gancho de teste `rowsRead` (`pgstore`): a leitura do
  pull e a da listagem que não param no orçamento devolvem a mesma página, só gastam mais memória.
- **O que a F2 NÃO provou:**
  - não há servidor HTTP de verdade (F3): a suíte fala com o `http.Handler` sem rede;
  - o detector de corrida (`-race`) não rodou: a máquina não tem compilador C. A concorrência está
    provada pelos testes contra o Postgres, não pelo `-race`;
  - o PC ainda não fala este contrato (F7a e F7b); o cliente antigo continua no contrato antigo;
  - o login, a sessão e o token do aparelho são os de teste (`InMemorySessionStore`), e o usuário, o
    contador e o aparelho são criados à mão nos testes (F3 e F6).
- **Riscos que ficam:**
  - **um selo só:** um segundo conflito no mesmo ditado, antes de a pessoa escolher, troca o perdedor
    guardado (o primeiro perdedor se perde). Com uma pessoa só, raro (estimativa);
  - **o recomeço grava uma versão nova de cada ditado** que reenvia (base vazia sobre o que existe é
    sempre mescla ou conflito): correto, mas é escrita a mais no dia do recomeço;
  - **o aparelho de origem depois de restaurar um backup:** um ditado que só um PC tinha e que outro PC
    reenvia no recomeço fica com o aparelho de quem reenviou;
  - **sem cota nem limite de taxa de escrita por usuário** (revisão da F2): cada recusa gravável
    (`tipo_desconhecido`, `tipo_ainda_nao_sincroniza`, `operacao_invalida`...) grava uma linha em
    `applied_mutations`, com um `entity_type` de até 32 bytes, e o push aceita 500 mutações por pedido
    sem limite de pedidos. Um token de PC com defeito, ou roubado, enche a tabela do próprio usuário e
    ocupa a trava do contador dele (não a dos outros). Fica para a F3 (o limite por token) e a F11 (a
    cota por conta; era "F3 e F9" até a D3 da F9, de 08-out-2026: "Decisões da F9").
- **Para as fatias seguintes:**
  - **F3:** monta o `pgstore.New(pool)` no `cmd/server`, abre e fecha o pool, põe o `/health/ready` no
    `pool.Ping`; troca a sessão de teste pela de verdade; fecha o CRUD para o token do PC (`403`); move a
    busca para `POST /transcriptions/search` (e `q` na URL vira `400`); monta o `POST
    /transcriptions/{id}/purge` sobre o `Store.Purge`; e faz os `404` das rotas não montadas (hoje o
    `POST /transcriptions` dá `405`, porque o `GET` existe no mesmo caminho). Também põe **limite de
    taxa e de lote por token** no push (pedidos por minuto e mutações por pedido), para um PC com
    defeito não encher a `applied_mutations` nem segurar a trava do contador do usuário; o número é da
    F3;
  - **F7a:** o marcador guarda quais campos mudaram; o PC manda o UUID em minúsculas (é como a nuvem os
    devolve); reconhece o eco pela versão; não guarda a versão de um `applied` mesclado; e trata o
    `duplicate` pelo `original`. Gera o `mutation_id` **na hora do envio** (o reenvio de um lote sem
    resposta repete os mesmos); se guardar no marcador, um `mutacao_reusada` quer dizer "outro aparelho
    já usou este id" (um `historico.db` copiado), e o PC gera outro e manda de novo. O `PATCH` local
    passa a ter o mesmo teto de 64 KiB do texto, para o PC nunca aceitar uma edição que a nuvem recusa
    (`campo_invalido`);
  - **F7b:** **não faz pull entre um push sem resposta e o reenvio dele.** Se fizer, o pull pode passar
    pela versão mesclada (ou pelo selo) enquanto o item ainda tem marcador e pulá-la, e o `duplicate` que
    vem depois não traz o estado: a versão se perderia nesse PC até a próxima mudança do item;
  - **F8:** acrescenta o `learned` às regras (o `delete` é o desfazer), ao pull (no mesmo snapshot) e à
    suíte de contrato;
  - **F7c, o recomeço sem a lixeira** (achado da revisão da F2; não é defeito do servidor, onde a base 0
    nunca restaura uma lixeira de outro aparelho): depois de restaurar um backup (epoch novo), o
    servidor pode ter fora da lixeira um item que o PC já tinha posto lá. O recomeço não manda a
    lixeira, o pull de cursor 0 traz o item fora dela, e o PC, sem marcador, o aplica: o item volta ao
    histórico. A F7c escolhe: mandar no recomeço também o `deleted_at` dos itens da lixeira que estão na
    janela (o servidor já trata a lixeira com base 0 como "apagar vence"), ou o aplicador não restaurar
    durante o recomeço;
  - **F9:** o `410` lê a marca d'água no mesmo snapshot do pull; o job de retenção grava pelo mesmo
    `gravar` (lápide e evento com `seq`). E arruma a `applied_mutations` por idade (um reenvio legítimo
    vem em minutos ou horas, não em meses), o que também limita o que um token com defeito acumula;
    **[F9, 08-out-2026, confirmada pelo Mauro: sem o `410` da marca d'água (D2); a arrumação da
    `applied_mutations` vale 190 dias, e não "horas", porque o PC que volta dentro da vida do token
    reenvia os mesmos `mutation_id`; e a cota por conta fica para a F11 (D3). Ver "Decisões da
    F9".]**;
  - **F11:** o log do Postgres sem texto ditado (`log_error_verbosity = terse` e o resto, em "Segurança
    e privacidade"), conferido pelo roteiro de deploy.

### Decisões da F3 (05-out-2026)

O que a F3 decidiu dentro do que a ADR deixou para ela, o que rodou e o que ela **não** provou. As três
decisões do plano da F3 foram implementadas **com a recomendação como padrão** e estão marcadas
**[a confirmar pelo Mauro]**: cada uma diz a alternativa e como desfazer.

- **Onde mora o quê.**
  - `internal/server/cloudauth`: as peças puras, só biblioteca padrão: a URL do Google e a troca do
    código (`google.go`), a validação do ID token (`idtoken.go`), os segredos, o SHA-256, o HMAC e o
    desafio do PKCE (`segredo.go`), `CONTAS_PERMITIDAS` (`contas.go`), a `volta` (`volta.go`), o rótulo
    da sessão (`rotulo.go`), o balde de fichas (`limite.go`) e o IP com o proxy confiável (`ip.go`);
  - `internal/server/cloudstore`: a interface `Accounts` (`contas.go`) e a implementação em memória
    (`memory_contas.go`); o `MemoryStore` implementa `Store` e `Accounts` sob o mesmo mutex;
  - `internal/server/pgstore/contas.go`: o `Accounts` no Postgres;
  - `internal/server`: o `Cloud` (`nuvem.go`), a **tabela de rotas** (`rotas.go`: padrão, quem pode e o
    handler; o roteador é montado dela, e os testes a percorrem), o login (`auth_routes.go`), as
    sessões, o `/me` e o "apagar meus dados" (`sessoes.go`), os cabeçalhos, o Host, o CSRF e os limites
    (`seguranca.go`), o log de acesso (`acesso_log.go`) e a página provisória (`pagina/`);
  - `internal/server/authtest`: a suíte do login, da sessão e do escopo, com o dublê do Google
    (`googlefake.go`, sobre `httptest.NewTLSServer`), contra a memória e contra o Postgres;
  - `cmd/server`: o `serve` (`serve.go`) e a configuração (`config.go`).
  O `Server` antigo (`server.go`, sem `Store`) fica só para os testes do cliente de sync antigo, até a
  F7a; o `Deps.Store` da F2 saiu dele, e a suíte de contrato passou a montar o `Cloud`.
- **Nenhuma dependência nova.** Só a biblioteca padrão (Go 1.27: `http.CrossOriginProtection`,
  `crypto/hmac`, `log/slog`, `net/netip`) e o `pgx`/`pgxpool` que já estavam. O `go.mod` não mudou, e o
  Agent continua sem nada do servidor (`go list -deps ./cmd/agent` não tem `internal/server`).
- **O ID token sem JWKS e sem o userinfo.** O ID token chega direto do endpoint de token do Google, por
  TLS, numa chamada autenticada com o client secret: o OpenID Connect Core 1.0 (3.1.3.7, item 6) e a
  documentação do Google dispensam a checagem da assinatura nesse caso. O servidor confere `iss`
  (`https://accounts.google.com` ou `accounts.google.com`), `aud` (o client; em lista, o `azp` é
  obrigatório), `azp` quando vem, `exp` e `iat` com 5 minutos de folga, o `nonce` guardado no servidor,
  `sub` (1 a 255 caracteres ASCII) e `email_verified` igual ao booleano `true`.
  - **Por que é seguro:** o endereço do endpoint de token (`cloudauth.GoogleTokenURL`) está no código;
    nunca vem do ambiente nem do JSON do client (o `token_uri` do arquivo é ignorado). E o client HTTP
    não segue redirecionamento, porque o client secret vai no corpo.
  - **Por que não JWKS:** umas 120 linhas de RSA, um cache de chaves com rotação e mais uma chamada de
    rede, sem ganho neste fluxo.
  - **Por que não o userinfo:** é mais uma chamada, com o access token, e não amarra o `nonce` nem o
    `aud`.
  - **Quando muda:** se um dia o ID token chegar pelo navegador (o One Tap, por exemplo), entra o JWKS.
- **O login, como ficou.** Os nomes da D6 ficam: `POST /auth/start`, `GET /auth/google/callback`,
  `POST /auth/logout`, `__Host-pre`, `__Host-sessao`.
  - O `/auth/start` gera o `state`, o valor do `__Host-pre`, o verificador do PKCE e o `nonce`, 256
    bits cada; grava o SHA-256 do `state` e do `pre`, o verificador, o `nonce` e a `volta` em
    `pending_logins`, com 5 minutos; e devolve a URL do Google com `prompt=select_account`.
  - O callback apaga o `__Host-pre` sempre, consome o login pendente pelo hash do `state` **e** pelo
    hash do `pre` no mesmo `UPDATE ... used_at ... RETURNING` (uso único, sem corrida; com o `pre`
    ausente ou errado, a linha não casa e nada é consumido: revisão da F3), trata o `error=` do Google
    como "cancelado", troca o código (10 s de prazo) e valida o ID token.
  - **A lista vem antes de qualquer escrita:** e-mail não verificado e e-mail fora de
    `CONTAS_PERMITIDAS` não chegam ao banco. Depois, numa transação só: o `sub` conhecido acha o usuário
    (conta desativada é recusada; o e-mail novo, se o Google o trocou, não pode ser de outro usuário);
    o `sub` novo com um e-mail já fixado é recusado (D7); senão, o usuário (UUID v7) e o contador
    (`sync_epoch` UUID v7) nascem juntos. Dois primeiros logins ao mesmo tempo: o `INSERT ... ON
    CONFLICT DO NOTHING` deixa um só, e o outro recomeça e cai no caminho de quem já existe.
  - A sessão: 256 bits no cookie, só o HMAC com `SESSION_SECRET` no banco, o rótulo do User-Agent (de
    listas fechadas), e a sessão que o navegador trouxer é apagada: o id muda a cada login. Termina com
    `303` para a `volta`.
  - A recusa é uma página fixa, que não ecoa nada do pedido: `403` "Esta conta não tem acesso" (fora da
    lista, `sub` diferente, e-mail não verificado, e-mail de outro usuário, conta desativada); `400` "Não
    deu para entrar" (`state` inválido, vencido ou reusado, `pre` ausente ou trocado, cancelado, ID
    token inválido); `502` (o Google falhou); `429`. A lista fechada de motivos do log ganhou, além dos
    seis do plano, `email_em_uso`, `conta_desativada`, `cancelado` e `falha_interna`.
  - A `volta` (proposta da F1) foi implementada como estava: a mesma regra no servidor
    (`cloudauth.ValidarVolta`, `400`) e no `CHECK` do banco.
- **Sessão: 30 dias sem uso e teto de 90 dias desde o login. [Decisão 1 da F3; confirmada pelo
  Mauro em 05-out-2026.]** O resolver anota o uso e anda o vencimento no mesmo `UPDATE` (`expires_at = LEAST(now +
  30 dias, created_at + 90 dias)`), e o `Max-Age` do cookie é o teto. Sair e derrubar **apagam** a
  linha (a coluna `revoked_at` fica sem uso; a arrumação apaga também o que a tiver). A alternativa é
  não ter teto: desfaz-se tirando o `LEAST` e o `created_at` do `WHERE`.
- **O cookie em `http://localhost`.** O servidor manda `Secure` e o prefixo `__Host-` também no
  desenvolvimento. O Chrome e o Edge tratam `localhost` como contexto seguro e aceitam (o teste manual
  confere); o Firefox recente deve aceitar (**não conferido**); o Safari não aceita `Secure` em `http`.
  A página tem de abrir em `localhost`, e não em `127.0.0.1`: o `Host` diferente do de
  `PUBLIC_BASE_URL` é `400` (menos em `/api/v1/health/*`), o que também barra o DNS rebinding.
- **CSRF e cabeçalhos.** O `http.NewCrossOriginProtection()` envolve o roteador inteiro, sem origem
  confiável nem exceção: `POST`, `PATCH` e `DELETE` de outro site (`Sec-Fetch-Site` `cross-site` ou
  `same-site`, ou um `Origin` de outro host) são `403`; o PC, que não manda esses cabeçalhos, passa.
  Nenhum `GET` muda estado, fora o callback (anotar o último uso da sessão é só registro). Nenhum CORS:
  o `OPTIONS` é `404`. Em toda resposta: a CSP `default-src 'self'; base-uri 'none'; object-src 'none';
  frame-ancestors 'none'; form-action 'self'`, `nosniff`, `Referrer-Policy: no-referrer`, COOP
  `same-origin`; HSTS de um ano, sem `includeSubDomains`, só com `https`; `no-store` em `/api` e
  `/auth`.
- **O escopo do token do PC.** Cada rota da tabela declara quem pode: `sessao` (o CRUD, a busca, o
  excluir de vez, as sessões e o `/me/data`), `aparelho` (`/sync/*`), `qualquer` (`GET /me`, que com
  o token traz o aparelho) e `publico` (health, `/auth/*` e a página). Sem credencial, `401`; do tipo
  errado, `403`. Com `Authorization`, só vale `Bearer <token>` de um aparelho não revogado; qualquer
  outra coisa é `401`, **sem olhar o cookie**. O token tem de ter o formato de um segredo de 256 bits em
  base64url (43 caracteres) para ir ao banco: **a F6 gera o token assim**. `/devices` fica sem montar
  até a F6.
- **A lista em toda checagem de credencial.** O e-mail do usuário ainda tem de estar em
  `CONTAS_PERMITIDAS` na sessão e no token: tirar um e-mail da lista e reiniciar corta as sessões e os
  PCs dele. É mais restritivo que o "só no login" da ADR; desfaz-se tirando as duas checagens de
  `autenticar` (`rotas.go`).
- **Limites de tentativas. [Decisão 2 da F3; confirmada pelo Mauro em 05-out-2026.]** Em memória, por processo
  (reiniciar zera; um servidor só, então basta): no login, 10 por minuto por IP (o `/auth/start` e o
  callback no mesmo balde) e 100 por minuto no `/auth/start` de todos (ele grava no banco sem sessão);
  no push, 60 pedidos e 5.000 mutações por minuto por aparelho, além do teto de 500 por lote (com esses
  números, o envio inicial de 10 mil ditados leva uns 2 minutos). Resposta `429` com `Retry-After`. O IP
  é o `RemoteAddr`, ou o **último** salto do `X-Forwarded-For` quando o `RemoteAddr` é o
  `PROXY_CONFIAVEL` (um IP ou uma rede CIDR); ele só serve de chave e nunca vai a log. A chave é o
  IPv4 inteiro e o **IPv6 por `/64`** (revisão da F3: por endereço, quem tem um `/64` girava endereços
  e escapava). Isso fecha o risco da F2 de "sem limite de taxa nem de lote por token" **só no push**:
  o pull, o CRUD, o `/me` e as sessões continuam sem limite de taxa (ver "Riscos que ficam"). O teto
  total do `/auth/start` troca o abuso do banco por um DoS do login: quem o esgota trava o login de
  todos enquanto durar (o callback não passa por ele). A alternativa é outros números (são constantes,
  `LimitesPadrao`).
- **A configuração do `serve`.** As variáveis estão no `10`. Duas escolhas da F3:
  - o client do Google sai de **um arquivo**, `GOOGLE_CLIENT_SECRET_FILE` (o JSON que o Google Cloud
    dá ao baixar um client Web), e não de `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` e
    `GOOGLE_REDIRECT_URL`: o secret não passa pelo histórico do shell, e o redirect é derivado de
    `PUBLIC_BASE_URL` e conferido nos `redirect_uris` do arquivo na subida;
  - a variável que falta ou está errada encerra com saída 2 e diz o nome, nunca o valor (nem o caminho
    do arquivo). Na subida, o `serve` recusa um banco com migration pendente (decisão 13: ele nunca
    migra), liga o `/health/ready` ao `Ping` do Postgres, sobe o `http.Server` com timeouts (cabeçalho
    10 s, leitura 30 s, escrita 60 s, ociosa 120 s, cabeçalho de até 64 KiB) e desliga limpo em 15 s
    no SIGINT e no SIGTERM. A arrumação (logins pendentes e sessões vencidos) roda na subida e de hora
    em hora.
- **Login de teste só por build tag.** `POST /auth/teste` com `{"email"}` da lista abre a mesma
  sessão; o `sub` é `teste:` + e-mail, então o D7 impede que ele tome um e-mail já fixado no Google. Só
  compila com a tag `logindeteste`; com ela, o `NewCloud` e o `serve` recusam `PUBLIC_BASE_URL` fora
  de `localhost`, o `serve` recusa `HTTP_ADDR` fora do loopback (`127.0.0.0/8`, `::1` ou `localhost`;
  saída 2), a rota responde `404` a um `RemoteAddr` fora do loopback ou a um pedido com
  `X-Forwarded-For` ou `Forwarded`, e o `serve` avisa no log (revisão da F3: antes, só a URL pública
  era conferida, e em `0.0.0.0` um vizinho de rede, mandando `Host: localhost:8080` sem `Origin`,
  ganhava a sessão de um e-mail da lista). Sem a tag, a rota é `404` (a suíte confere). Nenhuma
  variável a liga, e o build de produção (F11) não passa a tag (a F11 confere). **O D7 vale nos dois
  sentidos:** o primeiro login de teste com um e-mail fixa nele o `sub` `teste:<e-mail>`, e o login
  Google desse e-mail, no mesmo banco, passa a ser recusado (`sub_diferente`) até apagar a linha. O
  login de teste usa um e-mail da lista que não é o do Google de quem testa, ou outro banco (o roteiro
  manual avisa).
- **Rotas, busca e apagar.**
  - **404 em vez de 405:** os padrões `/api/v1/`, `/auth/` e `/` pegam o resto, com qualquer método.
  - **Busca:** `POST /api/v1/transcriptions/search`, o `q` e os filtros no corpo (até 16 KiB), a
    mesma resposta do `GET`; um `q` no `GET`, com qualquer valor, é `400`.
  - **Excluir definitivamente:** `POST /api/v1/transcriptions/{id}/purge` sobre o `Store.Purge`: `204`;
    fora da lixeira, `409`.
  - **"Apagar meus dados" mantém a sessão atual e apaga os aparelhos. [Decisão 3 da F3; confirmada
    pelo Mauro em 05-out-2026.]** `DELETE /api/v1/me/data` com `{"confirmar":"apagar"}`, só com sessão; sem a
    confirmação, `400` e nada muda. Numa transação só, cujo **primeiro** comando é o `UPDATE` do
    contador (a mesma trava do `gravar`: um push do mesmo usuário espera e, depois, não acha mais o
    aparelho): troca o `sync_epoch` (UUID v7), sobe a marca d'água ao `last_seq` (`GREATEST`; o `seq`
    continua e não volta a 0), e apaga os ditados, as lápides, os eventos, as mutações, o aprendido, as
    correções, o dicionário, as preferências, os pedidos de pareamento aprovados pela conta, **os
    aparelhos** (com os tokens, em cascata) e as outras sessões. Ficam o usuário, o contador e a sessão
    de quem pediu, que vê o histórico vazio. A alternativa é encerrar também a sessão atual e manter os
    aparelhos como revogados (`revoked_at`): desfaz-se trocando o `DELETE FROM devices` por um `UPDATE
    ... revoked_at` e o `keepSessionID` por nenhum. **A diferença para a D2**, que diz "revoga": para o
    PC, o efeito é o mesmo (`401` na chamada seguinte); mas apagado, o aparelho sai da lista de
    aparelhos da conta (a da F6), em vez de ficar nela como revogado.
- **Logs.** `slog` em JSON no stderr: uma linha por pedido com o método (fora da lista conhecida,
  `outro`), o `r.Pattern` (`sem rota` quando barrado antes), o status, os ms, os bytes, as contagens do
  push e do pull e um motivo de lista fechada; o login recusado com o motivo. Nunca a URL, a query, o
  corpo, o cookie, o token, o `code`, o `state`, o e-mail nem o IP. O `ErrorLog` do `http.Server`
  passa só a categoria (as mensagens dele trazem o endereço de quem pediu), e um pânico é registrado só
  com a rota.
- **O esquema.** Só a 0001 mudou, corrigida no lugar (vale até o deploy): `CHECK (email =
  lower(email))` e `google_sub` de 1 a 255 caracteres; o `schema_test.go`, o `postgres_test.go` e o
  `05` mudaram junto. A 0010 a 0014 serviram como estavam. Um banco de teste que já tinha a 0001
  antiga recusa o arquivo pelo checksum: recria-se o contêiner (o roteiro manual diz como).
- **A página provisória** (`internal/server/pagina`, embutida): entrar, a conta, as sessões (com
  derrubar), sair e "apagar meus dados", e as páginas de recusa; sem script nem estilo embutido (a
  CSP). A F4 a troca pelo app.
- **Os nomes do JSON novos (D6):** as sessões são `{ "items": [ { "id", "rotulo", "criada_em",
  "ultimo_uso_em", "atual" } ] }`; o `/me` com o token traz `"aparelho": { "id", "nome",
  "plataforma" }`; o "apagar meus dados" pede `{"confirmar":"apagar"}`.
- **O que rodou contra o Postgres 18** (o contêiner de `scripts/postgres-de-teste.ps1`, 05-out-2026):
  - a suíte `authtest` inteira, 23 casos: o caminho feliz (o usuário e o contador juntos, o epoch v7, o
    cookie com os atributos exatos, e o valor dele, os bytes dele e o SHA-256 puro dele em lugar nenhum
    do banco); fora da lista, `sub` diferente, e-mail não verificado e conta desativada sem criar nada
    (as linhas contadas antes e depois); o `state` reusado, vencido, sem `pre` e com o `pre` de outro
    login; o cancelado, o `500` do Google e cada claim errada; a `volta`; a sessão nova a cada login;
    sair, listar e derrubar (a de outro usuário é `404`); os 30 dias sem uso e o teto de 90; os limites
    do login (com e sem proxy confiável) e do push; tirar o e-mail da lista; o escopo, percorrendo a
    tabela de rotas; o `/me`; o CSRF e nenhum CORS; os cabeçalhos; o Host; as rotas não montadas; o
    "apagar meus dados"; o log; e a página;
  - a suíte de contrato da F2, agora com a sessão do celular por cookie e o PC por Bearer, e um caso a
    mais (o "excluir definitivamente" pela rota): 20 casos;
  - as provas que só o banco dá (`contas_pg_test.go`): oito primeiros logins simultâneos com o mesmo
    `sub` viram um usuário e um contador, e oito com o mesmo e-mail e `sub`s diferentes deixam entrar
    um só; oito callbacks simultâneos com o mesmo `state` consomem o login uma vez; o vencimento da
    sessão anda no mesmo `UPDATE` que a resolve, sem passar do teto, e um teto encurtado corta a sessão
    antiga; e o "apagar meus dados" com um push do mesmo usuário ao mesmo tempo: no fim, nenhum ditado
    nem evento da conta, o aparelho, o token e o pedido de pareamento aprovado somem, a sessão atual
    fica, o `seq` continua, a marca d'água sobe ao `last_seq`, e B fica intacto;
  - o `serve` de verdade: recusa um banco não migrado; num migrado, sobe, responde o `/health/ready`
    com o `Ping`, a página, o `400` do Host e o `401`, e desliga limpo, com a porta fechada depois.
  Tempos: `pgstore` 5,09 s, `migrations` 4,28 s, `pgmigrate` 1,84 s e `cmd/server` 0,77 s; sem a
  variável, 0,30 s, 0,31 s, 0,30 s e 0,32 s (pulam). Com a tag `logindeteste`, os mesmos pacotes passam
  (`pgstore` 4,64 s), com o servidor da suíte em `localhost`.
- **Mutação.** Em cópias feitas por `git archive`, com cada troca conferida antes (o trecho aparece uma
  vez só, e o arquivo mudou) e a restauração conferida byte a byte depois; timeout contaria como teste
  que falhou (nenhum houve).
  **98 mutações** nos pontos de segurança, em três rodadas: cada checagem de claim do ID token e a
  folga; a lista no login, na sessão e no token; o D7 (memória e Postgres); o `pre` (ausente e
  trocado); o uso único e o vencimento do login pendente (memória e Postgres); o SHA-256 puro no lugar
  do HMAC; cada atributo dos dois cookies; a sessão trocada a cada login; o sair apagando no servidor;
  o Bearer sem cair no cookie; o tipo de credencial por rota (seis linhas da tabela); o `404` do resto;
  a `CrossOriginProtection`; o Host; o HSTS só em https; o `no-store`; a CSP; o `nosniff`; os limites do
  login (por IP, total e no callback) e do push (pedidos e mutações); o balde de fichas; o
  `X-Forwarded-For` (de qualquer um e o primeiro salto); a `volta`; a lista sem minúsculas; o PKCE, o
  `prompt` e o redirecionamento do client do Google; o aparelho revogado e a conta desativada; o teto e
  o vencimento da sessão; o "apagar meus dados" (o `user_id`, a sessão que fica, o epoch, a marca
  d'água, os aparelhos e os pedidos de pareamento) e a confirmação dele; o `q` na URL; o log (a URL, o
  método cru, a query no login recusado e o endereço no `ErrorLog`); e a configuração (o redirect, o
  secret, o segredo curto, a base em http, os segredos iguais e a migration pendente). 15 delas no SQL do
  Postgres, rodadas contra o Postgres de teste.
  - **96 mortas, 2 equivalentes**, que sobrevivem por construção: tirar só a checagem do tipo de
    credencial das rotas de aparelho (o `requireDevice` do push e do pull é a segunda guarda; tirando as
    duas, morre) e aceitar um Bearer de qualquer formato (ele vai ao banco, que não o acha: o mesmo
    `401`, com uma consulta a mais).
  - **Quatro acharam buraco e viraram teste:** tirar o teto do `WHERE` da sessão (o `LEAST` ainda
    cortava; só um teto encurtado entre duas subidas os distingue); apagar os dados sem os pedidos de
    pareamento aprovados (nenhum teste criava um); e o client do Google sem secret e a base em http fora
    de `localhost` (o caso de teste quebrava também o redirect, que recusava antes).
  - Três trocas não compilavam na primeira forma e foram refeitas. Nenhum timeout do executor; a
    mutação que faz o `serve` subir num banco não migrado morre pelo prazo de 20 s que o teste ganhou.
- **Revisão da F3 (05-out-2026).** O que mudou depois da primeira entrega, provado na memória e no
  Postgres 18 de teste:
  - **O login de teste só escuta e só atende na própria máquina.** Com a tag, a única guarda era a URL
    pública em `localhost`; o `HTTP_ADDR=0.0.0.0:8080` passava, e um vizinho de rede (que não é
    navegador: manda `Host: localhost:8080`, sem `Origin`) ganhava a sessão de um e-mail da lista. Agora
    o `serve` com a tag recusa `HTTP_ADDR` fora do loopback (saída 2, só o nome da variável), e a rota
    responde `404` a um `RemoteAddr` fora do loopback ou com `X-Forwarded-For`/`Forwarded`. O `NewCloud`
    não recebe o endereço de escuta, então a segunda guarda é a da rota. Ver "Login de teste", acima.
  - **O `state` sozinho não queima o login de ninguém.** O callback consumia o login pendente e só
    depois conferia o `__Host-pre`: quem obtivesse o `state` de outra pessoa (a URL de retorno vazada)
    era recusado, mas o login dela também. Agora o hash do `pre` está no `WHERE` do `UPDATE` de uso
    único (e no `MemoryStore`); a comparação em tempo constante (`cloudauth.Iguais`) saiu, sem uso.
  - **O IPv6 conta por `/64`** no limite por IP (ver "Limites de tentativas", acima, com a troca do
    teto total por um DoS do login, que fica para a F11).
  - **Cobertura que faltava, sem mudar o código:** a conferência do `Host` só era testada no `GET /`, e
    isentar todo o `/api/v1/` ou o `/auth/` sobrevivia à suíte (com a tag, um site faria o `POST
    /auth/teste` por DNS rebinding e leria a conta). O `testHost` agora percorre a tabela de rotas
    inteira, com a sessão válida e o corpo que faria efeito, e exige o `400` da guarda. E tirar o `/` de
    resto (o `POST /` passaria a `405` com `Allow`) também sobrevivia: o `testNaoMontadas` cobre o
    `POST /`, os métodos que a página não tem, um caminho qualquer e o `OPTIONS`, e exige o `404` em
    JSON sem `Allow` nem `Access-Control-*`.
  - **Docs:** o `10` ainda falava em `GOOGLE_CLIENT_ID`/`SECRET`; o `06` dizia "revoga" os aparelhos
    no "apagar meus dados" e "apaga" logo abaixo (harmonizado, com a decisão 3 em destaque); o roteiro
    manual avisa do `401` esperado no console e que o limite é de uns 5 logins por minuto (10 passos).
  - **Mutação da revisão**, do mesmo jeito (cópia por `git archive`, a troca conferida, a restauração
    conferida byte a byte, timeout contaria como morta e nenhum houve): **26 mutações** nos pontos
    mudados, 4 delas contra o Postgres. **25 mortas e 1 equivalente:** tirar a recusa explícita do
    callback sem o cookie `__Host-pre` (sem ele, o hash do valor vazio não casa linha nenhuma, e a
    recusa é a mesma; a checagem só poupa a ida ao banco). Entre as mortas: isentar da guarda do `Host`
    o `/api/v1/`, o `/auth/`, o callback, o logout, o `/me/data` e (com a tag) o `/auth/teste`; o `pre`
    fora do `WHERE` e o "consumir e depois recusar" de antes (memória e Postgres, num `WITH ... UPDATE`
    que consome e só devolve com o `pre` certo), que a suíte agora pega; o `/` de resto fora ou com o
    404 em texto; cada parte da chave por `/64`; e cada guarda do login de teste (o `HTTP_ADDR`, o
    loopback, o `RemoteAddr`, o `X-Forwarded-For` e o `Forwarded`). Duas trocas não compilavam na
    primeira forma (uma variável sem uso) e foram refeitas.
- **O que a F3 NÃO provou:**
  - **o Google de verdade.** Nenhum teste o chama: o dublê confere o que o endpoint de token confere
    (o grant, o code de uso único, o `redirect_uri`, o client, o secret e o verificador contra o
    desafio). Que o Google aceite o PKCE num client Web confidencial, que o aviso de "app não
    verificado" apareça ou não no modo de teste com o escopo básico, e quanto tempo o client novo leva
    para valer, só o teste manual mostra (`docs/teste-manual-nuvem-f3.md`);
  - **os navegadores:** que o Chrome guarde o `__Host-sessao` vindo de `http://localhost` é da
    documentação do Chromium, e o teste manual confere; o Firefox não foi conferido;
  - **o HTTPS e o proxy de verdade** (F11): os testes falam com o handler, e o `serve` com Postgres sobe
    num `127.0.0.1` em `http`;
  - o `-race` não rodou (a máquina não tem compilador C); a concorrência do login e do "apagar" está
    provada contra o Postgres;
  - o pareamento (F6): o token do PC é criado à mão nos testes.
- **Riscos que ficam:**
  - **os limites zeram quando o servidor reinicia**, e valem por processo (um servidor só);
  - **o `code` e o `state` do callback** passam na URL e podem ir ao log do proxy da VPS (risco 11):
    são de uso único e vencem em 5 minutos;
  - **no modo de teste do Google**, uma conta que não está em "Usuários de teste" é barrada pelo Google
    antes da Aurora: o teste manual da recusa precisa da segunda conta lá;
  - **a lista em toda credencial** tira o acesso na hora em que o servidor reinicia sem o e-mail: é o
    pedido, mas um e-mail digitado errado na lista derruba a própria conta até corrigir;
  - **a sessão vale 90 dias no máximo**: o celular pede o login de novo a cada três meses (decisão 1);
  - **só o login e o push têm limite de taxa.** O pull, o CRUD, o `/me` e as sessões não têm (uma
    sessão ou um token válidos podem pedir à vontade), e a **cota de escrita por usuário** continua
    aberta, como a F2 já dizia: 5.000 mutações por minuto por aparelho ainda enchem `applied_mutations`
    e o histórico. Ficam para a F9 (a retenção) e a F11 (a cota de escrita por conta, que era da F9 até
    a D3 da F9 de 08-out-2026, e o limite por rota atrás do proxy);
  - **o teto total do login** (100 inícios por minuto, de todos) é um DoS fácil do login: quem o
    esgota, de muitos IPs, trava o login do Mauro enquanto durar. É a troca pelo banco sem escrita
    à vontade; a F11 o reavalia atrás do proxy (o Caddy pode limitar antes).
- **Para as fatias seguintes:**
  - **F4:** troca a página provisória pelo app; o padrão `/` passa a servir o `index.html` do app nas
    rotas dele (e `/api` e `/auth` continuam `404` para o resto); o build da nuvem não pode ter script
    nem estilo embutido (a CSP), e as fontes vão embutidas no build;
  - **F5:** o `manifest`, os ícones e o `/sw.js` entram na tabela como `publico`, com o `Cache-Control`
    da ADR;
  - **F6:** as rotas do pareamento entram na tabela (`/device-auth/start` e `/exchange` públicas e com
    limite por IP; `/device/authorize` com sessão, usando a `volta` para voltar depois do login; os
    `PATCH`/`DELETE /devices/{id}` com o token só do próprio aparelho); o token é gerado por
    `cloudauth.NovoSegredo` e gravado como HMAC com `DEVICE_TOKEN_PEPPER`; e o vencimento dos tokens sem
    uso (sugestão: 180 dias);
  - **F7b:** o `429` com `Retry-After` vira espera, e o `401` vira "desconectado";
  - **F11:** `PROXY_CONFIAVEL` com o endereço do Caddy do compose; `PUBLIC_BASE_URL` em `https` (o HSTS
    passa a sair); o build confere que não tem a tag `logindeteste`; e o teto total do login e o
    limite das rotas sem limite (o pull, o CRUD) são reavaliados atrás do proxy.

### Decisões da F4 (05-out-2026)

O que a F4 decidiu dentro do que a ADR deixou para ela, o que rodou e o que ela **não** provou. O Mauro
autorizou a F4 em 05-out-2026 ("Deixar para o deploy": o login de verdade fica para a F11, e as partes
seguintes seguem). As três escolhas do plano da F4 foram implementadas **com a recomendação como
padrão** e estão marcadas **[a confirmar pelo Mauro]**; cada uma diz a alternativa e como desfazer.

- **Onde mora o quê.**
  - `web/src/ambiente.ts`: o `Ambiente` do Painel, o que muda entre o PC e a nuvem (o cliente do
    histórico, a tela de Configurações, a faixa do topo, os textos, o `aoSemSessao`, o Aprender, o
    "excluir definitivamente" e a escolha do selo). O Painel não importa nada de um modo só.
  - `web/src/api.ts`: os tipos, os erros, o `pedir` e o `ClienteDoHistorico`. Do PC:
    `clienteLocal.ts` (a Local API, com o token do bilhete), `ambienteLocal.tsx`, `FaixaLocal.tsx`,
    `App.tsx` e `main.tsx`.
  - `web/src/nuvem/`: `cliente.ts`, `AppNuvem.tsx` (a abertura e a troca de telas), `Telas.tsx`
    (Entrar, Sem conexão, Não deu para abrir, a faixa), `ConfiguracoesNuvem.tsx`, `ambienteNuvem.tsx`
    e `main.tsx`.
  - Dos dois: `Painel.tsx` (o "excluir definitivamente" na linha da Lixeira, a escolha do selo e a
    nota de edição sem conexão) e `ItemDitado.tsx` (o selo).
  - O servidor: `internal/server/painel.go` (serve o build), `pagina/` (só as duas páginas fixas do
    callback), `login_de_teste.go` (o `GET /auth/teste`) e `cmd/server/teste.go` (o servidor de
    teste).
- **Dois builds (decisão 12, implementada).** `vite build` é o de sempre (`bin\painel`, entrada
  `src/main.tsx`); `vite build --mode nuvem` (`pnpm build:nuvem`) troca, por um plugin do
  `vite.config.ts`, a entrada do mesmo `index.html` por `src/nuvem/main.tsx` e sai em
  `web/dist-nuvem` (fora do git). Nada de `if` em tempo de execução: o código de um modo não entra no
  grafo de módulos do outro. Nada vira `data:` (`assetsInlineLimit: 0`; a CSP da nuvem não o aceita).
  **As guardas:** o ESLint proíbe importar `src/nuvem` fora dela, e, dentro dela, o que é só do PC
  (`no-restricted-imports`); e o `web/bundle.test.ts` constrói os dois bundles numa pasta temporária
  (nunca em `bin\`) e lê o JS, o CSS e o HTML:

  | Bundle | Tem de ter | Não pode ter |
  |---|---|---|
  | Local | `/api/sessao`, `/api/history`, `Bearer` (prova que leu o certo) | `/auth/`, `/api/v1`, `serviceWorker`, "Entrar com o Google", "Sessões abertas", "Apagar meus dados da nuvem" |
  | Nuvem | `/auth/start`, `/api/v1/transcriptions`, `/api/v1/transcriptions/search`, "Entrar com o Google" | `/api/sessao`, `/api/history`, `/api/settings`, `/api/ia/`, `/api/aprendizado`, `/api/termos`, `Bearer`, `Authorization`, "Gerenciador de Credenciais", `serviceWorker` (a F5 inverte), `localStorage`, `sessionStorage`, `indexedDB` |
  | Os dois | todo `<script>` com `src` em `/assets/` | `<style>` ou `style=` no HTML; endereço de fora ou `data:` no HTML e no CSS; arquivo fora de `index.html`, `assets/` e dos dois da raiz |
- **O cliente da nuvem.** `fetch` da mesma origem (`credentials: 'same-origin'`, `cache: 'no-store'`);
  nunca manda `Authorization`, não vê o cookie (`HttpOnly`) e não guarda nada no aparelho. O CSRF não
  pede nada do app (o navegador manda `Sec-Fetch-Site: same-origin`). A busca vai sempre no corpo do
  `POST /transcriptions/search`; sem busca, `GET /transcriptions` com `trash` e `cursor`. `401` é
  `SemSessao` (a tela "Sua sessão terminou.", ou, com um texto não salvo na tela, a faixa da sessão:
  "O rascunho que fica até entrar de novo", abaixo), a rede é `ForaDoAr` (a faixa, ou a tela, "Sem
  conexão."), `409` é `Conflito` (o item é relido, como no PC), e o `429` do login tem a frase de
  espera. O item da nuvem (`deleted_at` e `versao_em_conflito` sempre presentes, `next_cursor` `""`)
  vira o do painel (ausente quando não há).
- **As telas e os textos** estão no `08` ("Celular (PWA)"): Abrindo, Entrar, Sem conexão, Não deu
  para abrir, Sessão expirada, Configurações (Conta, Sessões abertas, Privacidade e o "apagar meus
  dados" com a confirmação digitada), o selo e o "excluir definitivamente". Aparelhos (F6) e
  Aprendido (F8) têm o lugar marcado em `ConfiguracoesNuvem.tsx`, sem "em breve" na tela. **Um texto
  mudou do plano:** a Privacidade diz "Vão para a nuvem: o texto dos ditados, com a data, o PC de
  origem e os detalhes da transcrição." (o plano dizia só "o texto, a data e o PC de origem"): pela
  D4 sobem também o modo e os metadados do ASR, e a frase não pode prometer menos do que sai.
- **Escolher a versão do selo já na F4. [Decisão 1 do plano da F4; confirmada pelo Mauro em 05-out-2026.]** O
  `PATCH /api/v1/transcriptions/{id}` aceita `{base_version, resolver_conflito: "manter"|"trocar"}` sem
  `final_text` (`06`). A regra é pura (`cloudstore.ResolveConflict`) e a mesma na memória e no
  Postgres: só sobre a versão atual e só com selo (senão, `409`); as duas escolhas limpam o selo e
  gravam versão e `seq` novos (o PC recebe pelo pull); "trocar" é uma mudança do texto feita por quem
  escolheu (o celular, sem aparelho), e "manter" não toca no texto, então o PC que escreveu o texto
  que ficou continua o conhecendo ("Eco e mescla"). O nome do campo, que a F2 deixou para a F7c, fica
  fixado aqui, e o PC usa o mesmo na F7c. **A alternativa:** a F4 só mostra as duas versões, sem os
  botões, até a F7c; desfaz-se tirando o `resolverConflito` do `ambienteNuvem.tsx` (os botões somem
  sozinhos) e a escolha da rota (`store_routes.go`) e do `Store`.
- **O servidor serve o app.** `PAINEL_DIR` é obrigatória no `serve` (`10`); a pasta é lida uma vez,
  na subida, para a memória (`painel.go`), e cada arquivo vira uma rota da tabela: `GET /` (o
  `index.html`, `no-cache`), os arquivos da raiz (`no-cache`) e `GET /assets/{arquivo}` (`public,
  max-age=31536000, immutable`), com `ETag` e `304`. Os tipos saem de uma tabela fixa (no Windows, o
  `mime` lê o registro e pode errar o `.js`); extensão, nome, pasta fora de `assets/`, arquivo grande
  demais ou a falta do `index.html` encerram a subida com 2. Sem fallback de SPA (o app não tem rotas
  na F4): `/index.html` e qualquer caminho fora do build continuam o `404` em JSON. A CSP e os
  cabeçalhos são os de sempre. A página provisória da F3 saiu; as duas páginas fixas do callback
  ganharam o visual da tela Entrar ("Esta conta não tem acesso a esta nuvem." / "Não deu para
  entrar."), com o mesmo status de antes.
- **O login de teste no fluxo e o servidor de teste. [Decisão 2 do plano da F4; confirmada pelo
  Mauro em 05-out-2026.]** Com a build tag `logindeteste`: o `GET /auth/teste` diz a conta (a primeira de
  `CONTAS_PERMITIDAS`) e se o Google existe, só da própria máquina, e a tela Entrar mostra **Entrar com
  a conta de teste** só quando ele responde `200` (num servidor de verdade é `404`). A tela só
  pergunta com a página em `localhost`, o único endereço em que o `NewCloud` com a tag sobe: fora
  dele, o botão do Google aparece junto com o título, sem esperar um pedido nem deixar um `404` no
  console (revisão da F4). O `NewCloud` aceita subir sem o Google (aí o `/auth/start` e o callback
  são `404`). Sem a tag, o Google continua obrigatório. O subcomando `teste` (só com a tag) sobe a
  nuvem no PC: o Store em memória, sem Postgres e sem Google, só no loopback, as chaves geradas no
  processo e nunca impressas, a conta fictícia `voce@teste.local` e sete ditados inventados, criados
  pelo push de dois PCs fictícios (um na lixeira, um com um selo de verdade, pelas regras da F2). O
  atalho é o `scripts/nuvem-local-teste.ps1`, e o roteiro, o `docs/teste-manual-nuvem-f4.md`. O
  `scripts/nuvem-local.ps1` (o do Google) passou a compilar o app. **A alternativa:** o Postgres de
  teste, com o Docker aberto e o histórico vazio (sem ditado nem selo para ver antes da F7);
  desfaz-se apagando o `cmd/server/teste.go` e o script.
- **A tela Entrar com a ilustração da Aurora. [Decisão 3 do plano da F4; confirmada pelo Mauro em 05-out-2026.]** O
  `08` citava "o símbolo"; a tarefa da F4 pedia a Aurora, como a tela de abrir pela bandeja. Ficou a
  Aurora em 96 px, também em Sem conexão, Sessão expirada e nas páginas fixas. **A alternativa:** o
  símbolo pequeno (`Marca`), trocando o `<Aurora tamanho={96} />` de `Telas.tsx` e o SVG das duas
  páginas fixas.
- **Acessibilidade.** Cada tela inteira tem um `h1` que recebe o foco quando ela aparece, e o Painel
  que entra no lugar de Entrar ou de Sem conexão leva o foco ao `h1` dele (`focarNoTitulo`); os erros
  ficam em `role="alert"`; os avisos, na região `aria-live` de sempre; com o ponteiro de toque, os
  botões, as abas e a busca têm 44 px; o selo tem "2 versões, abra para escolher" para o leitor de
  tela, e a diferença entre as versões é sublinhado e peso, não cor; o "excluir definitivamente" abre
  com o foco no Cancelar.
- **Conferido no navegador** (o servidor de teste, com o build da nuvem compilado, no navegador do
  app, em 375 px e no desktop, claro e escuro): Entrar, o histórico com o selo, a escolha, a lixeira
  com a pergunta, a faixa "Sem conexão." (desligando o servidor), "Sua sessão terminou." (religando),
  as Configurações com o "apagar" aberto, e as duas páginas fixas. **Achados e corrigidos:** a classe
  das telas inteiras (`.entrada`) era a mesma dos campos das Configurações, e a tela Entrar herdava o
  cartão branco e a borda do campo (agora `.tela`); no celular, abrir o ditado com o selo rolava até o
  texto e escondia o selo sob o cabeçalho (agora o cartão rola para o começo); na Lixeira do celular,
  os botões ficavam à direita; o título focado mostrava o anel; e a linha da conta tinha outra largura
  que a lista das sessões. As capturas do navegador do app no modo celular saem com a escala errada
  (DPR 2), então o layout em 375 px foi conferido também por medida: nada passa da largura.
- **Testes.** No `web/`, 540 (eram 463, que passam sem mudar, fora o `import` do `criarCliente` em
  `edicao.test.ts`): `bundle.test.ts` (30), o cliente da nuvem (11), o app (16), o Painel na nuvem
  (10: o selo, o "excluir definitivamente" e a edição sem conexão) e o `estilo.test.ts` (o botão de
  risco, o CSS da nuvem só com tokens, a Space Grotesk no título). No Go: a tabela pura do
  `ResolveConflict`, o caso novo da suíte de contrato (memória e Postgres), `TestPainel_ServeOBuild`,
  os casos do build no `NewCloud`, o `PAINEL_DIR` no `serve`, o `GET /auth/teste` e o Google nulo (com
  a tag), o servidor de teste de ponta a ponta (com a tag), e o `teste` que não existe sem ela; a
  `authtest` mudou com o app (a página, os cabeçalhos e as rotas não montadas).
- **O que rodou contra o Postgres 18** (o contêiner de `scripts/postgres-de-teste.ps1`, ligado nesta
  máquina, 05-out-2026): `pgstore` (com o caso do selo), `migrations`, `pgmigrate` e `cmd/server` (o
  `serve` com `PAINEL_DIR`), sem e com a tag `logindeteste`. Tempos: `pgstore` 5,56 s, `migrations`
  4,47 s, `pgmigrate` 1,95 s, `cmd/server` 0,84 s.
- **Mutação.** Numa cópia feita por `git archive`, cada troca conferida antes (o trecho aparece uma vez
  só, e o arquivo mudou) e a restauração conferida byte a byte depois; o motivo de cada morte foi
  lido (a asserção certa, não um erro de compilação). **45 mutações, 44 mortas e 1 equivalente.** No
  `web/` (20): o Painel importando a nuvem (o ESLint e o bundle), a nuvem importando o cliente do PC
  (os dois), a busca com o `q` na URL, o `401` virando faixa, o "apagar" sem a confirmação, o botão de
  teste sem o `200`, a escolha do selo ignorada, o `409` do selo sem reler, o "excluir" sem
  perguntar, o `localStorage` e o `Authorization` no bundle da nuvem, o estilo embutido no HTML, sem o
  `credentials`, a nota de sem conexão, o selo em vermelho, a sessão atual com Encerrar, o Google sem
  ele, o foco no Excluir, e o modo nuvem sem trocar a entrada. No Go (25, 1 contra o Postgres): o
  cache dos assets, o `/` servindo outro arquivo, o asset desconhecido virando o `index.html`, sem
  `ETag`, extensão, subpasta e pasta estranhas aceitas, o Google nulo sem a tag, as rotas do Google sem
  o Google, o `GET /auth/teste` fora da máquina, o `google` sempre verdadeiro, a primeira conta vazia,
  o `PAINEL_DIR` não obrigatório, os exemplos sem o selo, o selo não limpo, o texto não trocado, o
  "manter" mudando o dono do texto, escolher sem selo e com a base velha, o texto e a escolha juntos,
  a escolha virando edição (memória e Postgres), e a página recusada mandando para fora. **A
  equivalente:** tirar a conferência explícita do `index.html` em `carregarPainel`; a leitura dele,
  logo depois, recusa a pasta do mesmo jeito (a checagem só dá a mensagem certa).
- **O que a F4 NÃO provou:**
  - **um celular de verdade:** o caminho pelo cabo USB está no roteiro, e nada rodou num Android; o
    `__Host-sessao` vindo de `http://localhost` foi aceito pelo navegador do app (Chromium), e o
    Chrome do celular só o roteiro confere;
  - o login do Google de verdade (F11, por decisão do Mauro): na F4, só o login de teste entrou de
    verdade num navegador; o botão do Google é provado pelos testes, com a nuvem de mentira;
  - Firefox e Safari; o `-race` (a máquina não tem compilador C);
  - a tela "Sem conexão" na abertura só aparece com a página já carregada (sem a casca da F5), e foi
    conferida pelos testes, não no navegador.
- **Riscos que ficam:**
  - **a prova do bundle é por texto:** um nome que mude (de `serviceWorker` para outro caminho de
    registro, por exemplo) escaparia; a outra camada é o ESLint, que barra a importação;
  - **o código do botão de teste vai no bundle de produção:** inerte sem a tag (fora de `localhost` a
    tela nem pergunta, e o `GET /auth/teste` e a rota de entrar são `404`), mas presente;
  - as páginas fixas do callback usam as fontes do sistema (as do app têm nome com hash e vêm com o
    build dele);
  - **escolher a versão descarta a outra para sempre** (é o pedido: um selo só, ADR, "Decisões da
    F2"; a tela diz "A que você não escolher é descartada." embaixo dos botões, sem Desfazer), e um
    segundo conflito antes da escolha troca o perdedor guardado;
  - o nome do aparelho no selo e a lista de aparelhos esperam a F6.
- **A revisão da F4 (05-out-2026).** Doze achados (um médio e onze baixos): dez corrigidos, um de
  limpeza e um que fica para o Mauro decidir.
  - **O teste do foco quebrava de vez em quando** (o médio): o `findByRole` podia voltar antes do
    efeito que leva o foco ao título, e três conferências do `AppNuvem.test.tsx` falhavam com a
    máquina carregada. Agora esperam (`waitFor`). A prova: com o foco atrasado 30 ms de propósito,
    as três conferências antigas quebram e as novas passam; e a suíte inteira passou três vezes
    seguidas e três em paralelo.
  - **O foco depois de entrar:** com a conta de teste, ou quando o **Tentar de novo** abre o
    histórico, o botão sumia com a tela e o foco caía no `<body>`. Agora vai para o título do
    histórico (`focarNoTitulo` no Painel; só a nuvem passa, nesses dois casos). Aberto com a sessão do
    cookie, e no PC, nada muda.
  - **A abertura:** enquanto o `GET /api/v1/me` não voltava, o app era só o fundo. Depois de 300 ms,
    a Aurora e "Abrindo…", num `role="status"`. E a tela Entrar só pergunta pelo login de teste com a
    página em `localhost`: num servidor de verdade, o Google aparece junto com o título.
  - **A volta do Google pelo Voltar:** a página vinda do cache de ida e volta deixava o botão em
    "Abrindo o Google…", ignorando o clique. O `pageshow` com `persisted` o solta.
  - **O selo avisa antes:** "A que você não escolher é descartada." embaixo dos botões, que é também a
    descrição deles para o leitor de tela.
  - **As abas com 44 px no toque** (eram 38): o cabeçalho cresce 6 px no celular.
  - **O histórico vazio da nuvem** diz "O que você ditar num PC conectado aparece aqui.": depois do
    "apagar meus dados", os PCs ficam desconectados, e a frase antiga prometia o que não acontece.
  - O roteiro manual não fixa mais em que dia cai cada exemplo; a suíte das rotas não montadas confere
    também o `GET /auth/teste` sem a tag; o ESLint barra o que é só do PC em qualquer profundidade de
    `src/nuvem` (uma regex sobre o que sobe de pasta); e o `server.exe` que sobrou na raiz do
    worktree, de um `go build` sem `-o`, foi apagado.
  - ~~**Fica para o Mauro decidir: o rascunho que some com o `401`.**~~ **Decidido pelo Mauro em
    05-out-2026 ("Guarda até entrar de novo") e feito no mesmo dia**: "O rascunho que fica até entrar
    de novo", logo abaixo. Era assim: no PC, a sessão que acaba deixa o editor na tela, com a faixa;
    na nuvem, qualquer `401` trocava a tela inteira por "Sua sessão terminou.", e o texto que estava
    sendo editado sumia. O caso mais caro era o `401` no meio de uma edição longa.
  - **Testes e mutação:** no `web/`, 544 (eram 540); no Go, o caso novo da suíte. 18 mutações, 18
    mortas (17 no `web/`, 1 no Go), numa cópia feita por `git archive`, cada troca conferida antes e
    restaurada byte a byte depois, com o motivo de cada morte lido. **Conferido no navegador do app**
    (o servidor de teste; 320, 375 e 800 px; claro e escuro): Entrar e o foco no título do histórico,
    as abas de 44 px com o toque e de 30 px sem ele, o selo com a linha nova, o vazio depois do
    "apagar", e o "Abrindo…" (com a resposta do `/me` atrasada de propósito na página).
- **O rascunho que fica até entrar de novo (decisão do Mauro, 05-out-2026: "Guarda até entrar de
  novo"; feito no mesmo dia).** Se a sessão acaba (qualquer `401`) com um texto não salvo na tela, o
  texto não some: fica só na memória da página (nunca no `localStorage`, no `sessionStorage`, no
  `indexedDB`, em cookie ou em outro lugar do aparelho; o `bundle.test.ts` continua proibindo), e a
  pessoa entra de novo sem a página sair do lugar, volta ao editor com o texto e salva. Sem texto não
  salvo, nada muda: a tela inteira "Sua sessão terminou.".
  - **O que conta como texto não salvo** (`textosNaoSalvos`, em `edicao.ts`): o editor aberto com uma
    mudança e as linhas fechadas com "não salvo". O editor aberto sem mexer não conta (some como
    antes); o rascunho com a nota de conflito conta (a versão da outra aba não é a da pessoa).
  - **O Painel fica** (`Painel.tsx`): o `acender` só chama o `aoSemSessao` (a tela inteira) quando não
    há texto não salvo; havendo, acende a faixa `'sem-sessao'`, como no PC. A faixa recebe o texto
    (para o Copiar) e o `aoEntrarDeNovo`, que tira a faixa, lê a lista de novo (os editores e os
    rascunhos ficam, `mesclarRecarga`), diz "Você entrou de novo. Pode salvar." e leva o foco, se ele
    estava na faixa, ao editor aberto ou à primeira linha com "não salvo". O PC não muda.
  - **A faixa da sessão** (`nuvem/EntrarDeNovo.tsx`, `FaixaDaSessao`): "**Sua sessão terminou.** Seu
    texto continua aqui; entre de novo para salvar.", com **Entrar de novo** e, no servidor de teste,
    **Entrar com a conta de teste** (um `fetch`, sem sair da página; sem o Google no servidor, só
    ele). Textos no `08`, "Sessão expirada".
  - **Por que uma janela à parte:** entrar com o Google navega a página inteira (`location.assign`),
    e a navegação apaga a memória onde o texto está. **Entrar de novo** abre, no clique (senão o
    navegador bloqueia), uma janela do próprio app em `/?janela=entrar` (no Android, outra aba), que
    faz o `POST /auth/start` com a volta `/?janela=entrou` e segue para o Google com
    `location.replace` (o Voltar do Google não pede outro login). O Google devolve a janela ao app em
    `/?janela=entrou`, que diz "Você entrou de novo." / "Pode fechar esta janela e voltar ao seu
    texto.", avisa as outras abas e chama `window.close()`. A janela não carrega o histórico nem
    pergunta nada à nuvem além do `/auth/start`. **O servidor não mudou:** o `ValidarVolta` já
    aceitava a volta, e o `GET /{$}` serve o `index.html` com a busca; dois casos de teste fixam isso
    (`TestValidarVolta` e `TestPainel_ServeOBuild`).
  - **Como a aba do texto sabe que voltou:** ela pergunta `GET /api/v1/me` quando volta a ter o foco
    ou a ficar à vista (a janela que se fecha devolve o foco; no Android, voltar à aba), e quando
    chega o aviso da janela por `BroadcastChannel` (`aurora-whisper-sessao`). **Não pelo
    `window.opener` com `postMessage`:** o servidor manda `Cross-Origin-Opener-Policy: same-origin`, e
    a janela que passa pelo Google perde o opener de vez (o handle da aba de origem passa a dizer
    `closed`). O canal é da mesma origem, só na memória, e o que chega por ele é só um "confira":
    quem decide é o `/me`, então uma mensagem forjada só causa um `GET /me`.
  - **O portão** (`criarPortao`, em `nuvem/cliente.ts`): enquanto a faixa da sessão está na tela, todo
    pedido do Painel (lista, salvar, Configurações) volta como `401` sem sair da página. Só a faixa o
    abre, quando o `/me` diz que voltou a **mesma conta** (o mesmo `id`). Outra conta, entrando pela
    janela, deixa a faixa em "**Você entrou em outra conta.** Este texto é de {e-mail}: entre com ela
    para salvar.", com o portão fechado: a lista dela não se mistura com o texto desta, e o salvar não
    vai para a conta errada (o servidor já recusaria, com `404`). O `/me`, o login de teste e o
    `/auth/start` da faixa usam o cliente sem o portão.
  - **A janela bloqueada:** "**Seu navegador bloqueou a janela.** Copie o texto antes de entrar de novo
    nesta aba.", com o foco em **Copiar o texto** (a área de transferência do navegador, no clique da
    pessoa; todos os textos não salvos, separados por uma linha em branco) e **Entrar nesta aba** (a
    página vai ao Google e volta ao começo do app; o texto que não foi copiado se perde, e a faixa
    disse antes). Sem `noopener` no `window.open`: com ele, o retorno é sempre `null`, e não daria
    para saber do bloqueio (a janela é do próprio app).
  - **Acessibilidade:** a frase da faixa fica num `role="alert"` (anuncia ao aparecer e a cada troca:
    bloqueada, copiado, outra conta, erro); os botões ficam fora dele, e o foco não sai do editor
    quando a faixa aparece; com o toque, os botões da faixa têm 44 px (os da faixa "Sem conexão"
    também, que tinham 30); em telas estreitas, os botões descem para uma linha deles.
  - **A revisão da fatia (05-out-2026, na mesma branch): cinco achados, todos corrigidos.**
    - **A pergunta do navegador antes de sair** (média; era "uma rede de segurança possível, não
      feita"). Enquanto a faixa da sessão está na tela **e há texto nela**, um `beforeunload`
      (registrado num efeito da faixa, removido no cleanup) faz o navegador perguntar ao recarregar,
      fechar a aba, puxar para atualizar ou, no navegador que abre o `window.open` na mesma aba,
      quando o Entrar de novo leva a página embora. Não pergunta sem nada a perder (a pessoa apagou
      o texto, ou o devolveu ao que estava salvo) nem depois de entrar de novo. **Entrar nesta aba**
      é a exceção, de propósito: a pessoa escolheu perder o texto, e a faixa disse antes (a ref
      `deixarSair` é solta logo antes do `irPara`, e a volta do cache de ida e volta a rearma).
      Com o diálogo ligado ali, cancelá-lo deixaria o botão preso em "Abrindo o Google…".
    - **O Sair com a faixa na tela** (baixa; verificado com uma sonda). O Sair passava pelo portão
      fechado, recebia o `401` de mentira, contava como "já saiu" e levava à tela Entrar: o editor
      com o texto sumia, e o `POST /auth/logout` nunca chegava ao servidor (uma sessão que voltou em
      outra aba continuava válida). Agora o Sair usa o cliente **sem o portão** (o mesmo da faixa;
      `ambienteNuvem.tsx`), e, com texto não salvo na tela, **pergunta antes**, ali mesmo ("Sair com
      texto não salvo", **Sair e descartar o texto** ou **Cancelar**, com o foco no Cancelar e o Esc
      cancelando), como o "apagar meus dados". A pergunta vale também sem a faixa (o Sair com um
      texto não salvo jogava o texto fora do mesmo jeito). Sem rede, o `ForaDoAr` do Sair acende a
      faixa de conexão no lugar da faixa da sessão (o texto fica, o portão abre com ela, e o
      Tentar de novo chega à nuvem).
    - **A faixa em pouca altura** (baixa; medida: no celular de 375 x 440, o cabeçalho fixo era de
      305 px, a faixa, de 117, e sobravam uns 94 px do editor). Com `max-height: 560px`, a faixa com
      a frase de sempre fica numa linha só, o título e o botão: **58 px em vez de 117, o cabeçalho
      com 246 px** (medidos no navegador do app, 375 x 440, claro e escuro). A frase continua para o
      leitor de tela (a técnica do `.so-leitor`, e não `display: none`). A da janela bloqueada, a da
      outra conta e a do erro dizem o que fazer, e ficam inteiras. **Fica sem prova:** o Chrome do
      Android atual não encolhe a janela de layout com o teclado (`resizes-visual`), e aí o
      `max-height` não dispara; a regra vale para o celular deitado, para as janelas baixas e para os
      navegadores que encolhem a janela com o teclado. Tirar a faixa do bloco fixo (rola com a
      página) foi descartado: um Salvar com a faixa fora de vista não diria nada.
    - **"Você entrou de novo."** (baixa). A página ganhou o link **Abrir o histórico** (`href="/"`),
      para o `window.close()` ignorado (COOP) ou a página aberta na mesma aba; não custa nada quando
      a janela se fecha sozinha (como se viu no navegador do app).
    - **Mutações que sobreviviam** (baixa): o separador do Copiar com vários textos, o guarda do
      Copiar com o texto vazio e o cleanup do portão, o `useVoltaDoCache` e o filtro de visibilidade
      da faixa. Agora cada um morre num teste (dois textos separados por uma linha em branco; o texto
      apagado e o navegador sem área de transferência; a faixa de conexão tomando o lugar da da
      sessão; o `pageshow` do cache; a aba escondida que não confere o `/me`).
    - **Testes e mutação da revisão.** No `web/`, 578 (eram 563). 32 mutações, 32 mortas, na mesma
      forma de antes (cópia por `git archive`, cada troca conferida e restaurada byte a byte, a cópia
      no fim igual ao `HEAD`, o motivo de cada morte lido): o `beforeunload` sem o guarda, nunca
      registrado, nunca removido, sem a exceção do Entrar nesta aba, sem soltá-la, sem rearmá-la,
      sem o cancelamento, sem dependência e sempre ligado; o portão sem abrir e a aba escondida
      conferindo; as quatro condições da frase compactável; o Copiar com outro separador, sem o
      guarda do vazio e sem o da área de transferência; o Sair pelo cliente com portão, que nunca ou
      sempre pergunta, sem o foco no Cancelar, sem devolvê-lo ao Sair e sem o Esc; o Painel dizendo
      sempre ou nunca que há texto; o link apontando para outro lugar e sem texto; e as três regras
      do CSS. Não mexi no servidor (Go).
  - **Testes.** No `web/`, 563 (eram 544): a sessão que expira no meio da edição (o Painel fica, o
    texto no editor, nada no `localStorage`, no `sessionStorage` ou em cookie), o portão (nada vai à
    nuvem), a janela aberta (o endereço, e a página não navega), a volta pelo foco (com a janela
    fechada sem entrar, a faixa fica; com a sessão, sai, a lista lê de novo, o foco vai para o texto
    e o Salvar grava), a volta pelo canal (a página `?janela=entrou` de verdade, na mesma suíte),
    outra conta (e a volta ao começo quando essa sessão também acaba), o foco que fica onde a pessoa
    está, a janela bloqueada (o Copiar, a cópia que falha e o entrar nesta aba), a conta de teste, a
    linha fechada com "não salvo" (e o foco nela), o editor aberto sem mexer (a tela inteira, como
    antes), a janela `?janela=entrar` (a volta no corpo, o erro com Tentar de novo) e
    `?janela=entrou`, o abrir de verdade (`false` no bloqueio, sem `noopener`), o
    `textosNaoSalvos`, o portão no cliente, a volta do `inicioDoGoogle` e os 44 px dos botões da
    faixa no toque (`estilo.test.ts`); o `bundle.test.ts` continua passando. No Go, os dois casos
    acima. Um teste antigo do `AppNuvem.test.tsx` (o foco no título de
    "Sessões abertas" depois de encerrar) falhou 2 vezes em ~30 rodadas da suíte inteira com os testes
    novos (0 em 15 no `abf139c`): a mesma corrida que a revisão da F4 tirou dos outros (o efeito do
    foco roda depois de o DOM mudar); agora espera (`waitFor`).
  - **Mutação.** Numa cópia feita por `git archive`, cada troca conferida antes (o trecho aparece uma
    vez só, e o arquivo mudou) e a restauração conferida byte a byte depois (e, no fim, a cópia
    inteira igual ao `HEAD`); o motivo de cada morte foi lido (a asserção certa, nenhum erro de
    sintaxe). **42 mutações, 42 mortas.** No `web/` (40): a tela inteira mesmo com texto e nunca a
    tela inteira; o voltar sem tirar a faixa, sem o foco, sem ler a lista e sem o aviso; o foco só
    nas linhas, sem a linha fechada e tirado de quem está noutro campo; a faixa sem o texto do
    Copiar; o editor não tocado contando como texto; o `/auth/start` sem a volta; o portão que deixa
    passar e o que nunca abre; qualquer `?janela=` virando a janela; o abrir que sempre "abriu" e com
    `noopener`; a volta sem avisar pelo canal; o login de teste nunca perguntado; a faixa sem fechar o
    portão; outra conta como a mesma, e a que não volta com o `401`; a volta à aba e o canal sem
    conferir; o bloqueio sem o foco no Copiar e passando despercebido; a conta de teste sem
    conferir; o Copiar com outro texto; o Entrar de novo sem o Google no servidor; a faixa sem
    perguntar pelo login de teste; o entrar nesta aba sem ir; a janela sem a volta, sem avisar, sem
    se fechar e sem pedir de novo no Tentar de novo; a faixa que nunca aparece; a página que nunca é
    a janela; o Painel sem o portão e a faixa atrás dele; e os 30 px no toque. **Uma só morria no
    teste de unidade** (o editor não tocado contando como texto): o teste de ponta a ponta trocava de
    vista para provocar o `401`, e a troca fecha o editor antes; corrigido (o `401` vem da lista lida
    na volta à aba, com o editor aberto), e a mutação passou a morrer nos dois. No Go (2), os dois
    casos novos mordem sozinhos: o `ValidarVolta` recusando a busca na raiz e o `/` com busca sem o
    app.
  - **Conferido no navegador do app** (o servidor de teste numa porta livre, com o build da nuvem
    numa pasta de rascunho; 375 px e 800 px, claro e escuro): a edição, o `POST /auth/logout` por
    fora (como o Sair de outra aba), o Salvar com `401` e a faixa com o texto no editor; a conta de
    teste pela faixa, a faixa saindo, "Você entrou de novo. Pode salvar.", o foco no texto e o
    Salvar gravando; o aviso de verdade entre abas (uma aba com a faixa, a sessão de volta, e outra
    aba aberta em `/?janela=entrou`, que se fechou sozinha: a primeira tirou a faixa e salvou); a
    página `/?janela=entrar` com o servidor sem o Google (o `404` do `/auth/start` vira "Não deu para
    entrar. Tente de novo.", com "Seu texto continua na outra janela."); e, com trocas só na página
    (o `/auth/teste` respondendo `404`, como num servidor de verdade; o `window.open` devolvendo
    `null`; uma área de transferência de mentira, para nada ir à do Windows; um `/me` de outra conta),
    o Entrar de novo, a janela bloqueada com o foco no Copiar, o "Texto copiado." e a outra conta.
    Nada no `localStorage`, no `sessionStorage` nem em `document.cookie`. Em 375 px, nada passa da
    largura, a faixa tem 343 px com 16 de margem, e os botões, 44 px. **Achado:** o navegador do app
    abre o `window.open` na **mesma aba**, sem aba nova: lá, Entrar de novo levou a página embora com
    o texto. O Chrome abre uma janela (no Android, uma aba), que é o caso do app; mas um navegador
    embutido que faça como o do app perde o texto (Riscos).
  - **O que não foi provado:** a janela à parte num Chrome de verdade (o navegador do app a abre na
    mesma aba, e abrir janela no Chrome do Mauro está fora das regras desta máquina) e o Google de
    verdade (F11); o `window.close()` depois da passagem pelo Google com o COOP (se o Chrome não
    deixar, a janela diz "Pode fechar esta janela e voltar ao seu texto."); o Android, o app
    instalado (F5) e o iPhone (depois do MVP). Da revisão da fatia: o diálogo do `beforeunload` num
    Chrome de verdade (o teste dispara o evento; o roteiro, seção 6, passo 4, é quem confere) e a
    faixa baixa no Chrome do Android com o teclado aberto.
  - **Riscos que ficam:**
    - **um navegador que abre o `window.open` na mesma aba** (o do app; talvez navegadores embutidos
      de outros apps) leva a página embora no Entrar de novo. A rede de segurança agora existe (o
      `beforeunload`, na revisão da fatia, abaixo): o navegador pergunta, e a pessoa cancela e fica.
      Um navegador embutido que não mostre o diálogo perde o texto como antes. O diálogo é do
      próprio navegador, sem texto nosso; manter ou tirar a pergunta fica para o Mauro decidir;
    - **o Android pode descartar a aba de fundo** com pouca memória enquanto a pessoa entra na outra
      aba; aí o texto se perde, e o `beforeunload` não roda (a aba é morta, não fechada). O login é
      curto, mas o risco existe;
    - qualquer página pode abrir `/?janela=entrar`, como pode mandar alguém para o `/`: leva ao
      seletor de contas do Google (`prompt=select_account`), que pede o clique da pessoa, e nada volta
      para quem abriu (o COOP corta o opener); o `/?janela=entrou` só faz as abas do app perguntarem o
      `/me`;
    - a faixa de outra conta diz "outra conta" pelo `id`: no servidor de teste, que recria a conta a
      cada subida, religar o servidor com um texto não salvo dá essa frase com o mesmo e-mail (o
      roteiro evita: seção 5, passo 3);
    - outros testes de foco antigos ainda conferem o foco sem esperar o efeito (um do `aprendizado`
      falhou uma vez com a máquina muito carregada); a correção é a mesma (`waitFor`), fora desta
      fatia.
- **Para as fatias seguintes:**
  - **F5:** o `manifest`, os ícones e o `sw.js` entram no build da nuvem (a raiz do build já aceita
    `.png`, `.ico` e `.webmanifest`, e cada arquivo da raiz vira uma rota pública); o `bundle.test.ts`
    inverte o `serviceWorker` no bundle da nuvem e o mantém fora do local; o Agent responde `404` em
    `/sw.js`; e o `Cache-Control` do `sw.js` é `no-cache`, como o dos outros arquivos da raiz; e,
    no app instalado, a janela à parte de entrar de novo (onde o Android a abre, se o cookie e o
    `BroadcastChannel` chegam à janela do app, e se ela se fecha);
  - **F6:** a seção Aparelhos em `ConfiguracoesNuvem.tsx` (o lugar está marcado) e o nome do aparelho
    no selo;
  - **F7c:** o PC mostra o selo (o componente já é o do Painel) e escolhe pelo mesmo
    `resolver_conflito`, com o `resolverConflito` no ambiente do PC;
  - **F8:** a seção Aprendido, com o Desfazer, em `ConfiguracoesNuvem.tsx`;
  - **F11:** o build de produção do servidor não passa a tag `logindeteste` (o `teste` e o `GET
    /auth/teste` somem), e o roteiro do deploy compila o `pnpm build:nuvem` e aponta o `PAINEL_DIR`.

### Decisões da F5 (05-out-2026)

O que a F5 decidiu dentro do que a ADR deixou para ela, o que rodou e o que ela **não** provou. O Mauro
autorizou a F5 em 05-out-2026 e decidiu as três escolhas do plano: o `short_name` **"Whisper"** (o
`name` continua "Aurora Whisper"), o ícone **do .exe, da bandeja e da aba** (o ladrilho escuro com o
símbolo), com o maskable de fundo cheio, e **`#070a09`** no `background_color` e no `theme_color`.

- **Onde mora o quê.**
  - `web/pwa/`: o `manifest.webmanifest`, o `sw.js` (à mão, ~90 linhas com os comentários, sem
    Workbox) e os três PNGs.
  - `web/vite.config.ts`: o plugin `aurora-app-instalavel`, só no `--mode nuvem`. Põe na raiz do build
    o manifest (depois de um `JSON.parse`: um manifest quebrado quebra o build), os ícones e o `sw.js`
    com os dois marcadores trocados (a falta de um quebra o build), e põe no `index.html` o `<link
    rel="manifest">` e as duas `<meta name="theme-color" media=…>` (`#f5f7f5` no claro, `#070a09` no
    escuro: o manifest aceita uma cor só, e a meta vale quando a página abre).
  - `web/src/nuvem/registrarSW.ts`, chamado pelo `src/nuvem/main.tsx`: só com `import.meta.env.PROD` e
    `'serviceWorker' in navigator`, no `load`, `register('/sw.js', { scope: '/', updateViaCache: 'none'
    })`; um erro é engolido (o app funciona sem o service worker).
  - `cmd/gerarico/pwa.go`: os ícones do app; `internal/agent/painel/servidor.go`: o `404` do `/sw.js`.
- **O manifest:** `id`, `start_url` e `scope` em `/`, `display: "standalone"`, `name`, `short_name`,
  `description` ("Os seus ditados, no celular."), `lang: "pt-BR"`, `dir: "ltr"`, as duas cores e três
  ícones: o 192 e o 512 com `purpose: "any"` e o 512 maskable numa entrada própria. **O `id` fica fixo
  daqui em diante:** mudá-lo é, para o navegador, outro app (um teste prende o manifest inteiro).
- **Os ícones** (Go puro, o mesmo `go run ./cmd/gerarico`, que agora escreve também em `web/pwa/`).
  O 192 e o 512 "any" são o `pixelsDoIcone` do .exe (o ladrilho arredondado com o canto transparente).
  O 512 maskable é o quadrado cheio `#070a09`, todo opaco, com o `tray.PixelsDoIcone(384, TemaEscuro,
  false)` no centro: o ponto do símbolo mais longe do centro (o lado de fora do arco de um colchete)
  fica a 183,6 px, dentro do círculo de 204,8 px (40% de 512) que todo launcher preserva, e 384 = 12 ×
  32 põe a grade do desenho em pixel inteiro. Os PNGs ficam no repositório, e um teste os compara pixel
  a pixel com o gerador. **A folha de prévia** (`PNGDIR=… go test -tags renderpng -run PWAEmPNG
  ./cmd/gerarico/`): os "any" em tamanho real no claro e no escuro, o maskable recortado em círculo,
  squircle e quadrado arredondado em 144 px (48 dp num celular 3x) e ampliado, com a zona segura em
  magenta. **Ela vai ao Mauro antes de a fatia fechar** (é o que ele pediu). O de 180 px do iPhone fica
  para depois do MVP.
- **O service worker** (o desenho do plano, com as duas mudanças abaixo):
  - a **casca** é o `/`, o `/favicon.svg` e todo `/assets/*` do build (nunca o `/index.html`, que a
    nuvem responde com `404`), uns 600 KB, quase tudo fonte; é gravada só no `install` (`addAll`, cada
    pedido com `cache: 'reload'`), que termina com o `skipWaiting()`; o `activate` apaga todo cache
    `aurora-casca-*` que não seja o atual (e não toca os de outro nome); sem `clients.claim()`;
  - **não há `put` em lugar nenhum:** nada que chega da rede depois entra no cache, então nenhum texto
    ditado tem como parar lá;
  - **responde por lista branca:** a navegação `GET /` (com ou sem busca: `/?janela=entrou` também)
    pela rede primeiro, com o `/` guardado só se a rede falhar; o `GET /favicon.svg` do mesmo jeito; e
    o `GET /assets/*` da mesma origem pelo guardado, senão pela rede (sem guardar). O resto ele nem
    responde: outro método, `/api/*` (o `device-auth` inclusive), `/auth/*` (a volta do Google),
    `/device/*`, `/sw.js`, o manifest, os ícones e outra origem. Uma rota nova fica de fora sozinha;
  - a **`VERSAO`** é `aurora-casca-` e 12 hex do sha256 **dos nomes e do conteúdo** de cada arquivo da
    casca. O plano dizia "do `index.html` e dos nomes"; com o conteúdo, o favicon (que muda sem mudar
    de nome) também troca a versão;
  - **mudança do plano: sem navigation preload.** Ele vale para **toda** navegação do escopo, inclusive
    as que o service worker não responde, e aí o navegador pede duas vezes: o do preload e o de sempre.
    A volta do Google (`/auth/google/callback`) é de uso único, e a segunda vez falharia (o login
    quebraria no app instalado). **Medido** no Chromium (abaixo): uma navegação a `/api/v1/me` chegou
    ao servidor uma vez sem o preload e **duas** com ele. O custo de ficar sem é o tempo de acordar o
    service worker na navegação (não medido; dezenas de milissegundos, pela documentação);
  - **mudança do plano: o favicon respondido.** O plano o punha na casca, mas não no que o service
    worker responde: guardado e nunca usado. Agora sai pela rede primeiro e, sem rede, do guardado;
  - **como a versão nova chega:** com rede, a página vem sempre da rede, então abrir o app já traz a
    versão nova; o navegador confere o `/sw.js` a cada navegação (`no-cache` no servidor e
    `updateViaCache: 'none'`), e o novo só muda o que abre sem rede. Sem aviso de "nova versão".
- **CSP e cabeçalhos: nada mudou no servidor.** O `default-src 'self'` já permite o registro do
  `/sw.js` (sem `worker-src`, vale o `script-src` e depois o `default-src`) e o manifest (o
  `manifest-src` cai no `default-src`); o `/sw.js` na raiz dispensa o `Service-Worker-Allowed`; o
  `painel.go` já dava `no-cache` com `ETag` a todo arquivo da raiz, e a tabela de tipos já tinha o
  `.webmanifest` e o `.png`. Só os testes mudaram (abaixo).
- **O painel local nunca é instalável**, em três barreiras e uma rede: o registro só existe no build da
  nuvem (a entrada da nuvem é a única que o importa), o ESLint barra o build local de importar
  `src/nuvem`, e o `bundle.test.ts` confere que o local não tem `serviceWorker`, `sw.js`, manifest,
  `theme-color` nem os ícones. A rede: o Agent responde **`404` em `/sw.js`** antes do fallback de
  página única (que até aqui respondia o `index.html` com `200`), mesmo com um `sw.js` na pasta (um
  build da nuvem copiado para `bin\painel` por engano), sem distinguir maiúscula (o disco do Windows
  não distingue), com `no-store`. Um navegador com um service worker registrado nessa origem o remove
  ao receber o `404`.
- **Testes.** No `web/`, 619 (eram 578): no `bundle.test.ts`, o local sem nada do app instalável, a
  nuvem com o registro, o link e as metas, os arquivos exatos da raiz de cada build, o manifest
  inteiro, a largura de cada PNG lida no IHDR, os ícones iguais aos de `web/pwa/`, a `CASCA` igual aos
  arquivos do build, a `VERSAO` recalculada, nenhum `put`, `add` ou preload no texto, e o `sw.js`
  emitido rodando num `node:vm` (com o `self`, o `caches` e o `fetch` de mentira): o install, o
  activate, a navegação com e sem rede (`/`, `/?janela=entrou`, `/?x=1`), o favicon, o asset guardado
  e o que não está, a navegação sem rede e sem casca, 19 pedidos que ele nunca responde, e o cache no
  fim de um uso inteiro só com a casca; e o `registrarSW.test.ts` (4). No Go: os ícones (os três do
  manifest, os "any" iguais ao ícone do programa, o maskable todo opaco, a marca dentro da zona
  segura, o símbolo da bandeja em 384 centrado, os PNGs do repositório atualizados e a pasta
  inexistente), o `TestRotas_SwJsE404` do Agent, o `TestPainel_ServeOBuild` com o `sw.js`, o manifest
  e um PNG (200 sem sessão, o tipo, `no-cache`, `ETag` e `304`, a CSP, nenhum cookie, as rotas
  públicas na tabela; `/index.html` e `/SW.JS` continuam `404`), e a suíte `authtest` com os
  cabeçalhos dos três (memória; o Postgres, quando pedido).
- **Mutação.** Numa cópia feita por `git archive`, cada troca conferida antes (o trecho aparece uma vez
  só, e o arquivo mudou), a restauração conferida byte a byte depois, a cópia inteira igual ao `HEAD`
  no fim (945 arquivos), e o motivo de cada morte lido (a asserção certa, nenhum erro de sintaxe);
  timeout contaria como falha (nenhum houve). **33 mutações, 33 mortas.** No `sw.js` (10): responder
  a qualquer `GET`, sem conferir a origem, outro método, um `put` na navegação (escrito sem o texto
  `.put(`, para só o teste de execução o pegar), o cache primeiro na navegação, o activate sem limpar e
  apagando tudo, sem `skipWaiting`, sem `reload` e com o navigation preload. No plugin (7): o
  `/index.html` na casca, a casca sem os assets, a versão fixa e sem o conteúdo, o app instalável no
  modo local, sem a meta do escuro e sem o link do manifest. No manifest (3): sem a entrada maskable,
  o maskable junto do "any" e outro `id`. No registro (5): importado no `main.tsx` do PC (o ESLint e o
  bundle), sem o `catch`, fora de produção, sem esperar o `load` e sem o `updateViaCache`. Nos ícones
  (5): o maskable em 480 px, com canto transparente, fora do centro, um PNG velho no repositório e o
  gerador sem os ícones do app. No Go (3): o Agent sem o `404`, o `404` só em minúsculas e o
  `no-cache` da raiz trocado por `immutable`. **Uma sobrevivia na primeira rodada** (o registro sem o
  `catch`): o `vi.fn` acompanha a promessa que devolve (o `settledResults`) e tratava a rejeição no
  lugar do código; com uma função comum no teste, o vitest acusa a rejeição não tratada, e ela morre.
- **Conferido num navegador.** **O navegador do app não registra service worker**: o registro falha
  com "An unknown error occurred when fetching the script", e o pedido do `/sw.js` nem chega ao
  servidor (o mesmo `/sw.js` por `fetch` volta `200`, com o tipo e a CSP certos). Lá, só se conferiu
  que nada ficou (nenhum registro, nenhum cache, nenhum cookie em `localhost:8093`), e a aba foi
  fechada. **A conferência foi num Chromium de verdade, sem janela:** o Edge em modo headless, com um
  perfil novo numa pasta de rascunho (nada do perfil da pessoa), falando por CDP no loopback, contra o
  servidor de teste numa porta livre com o build da nuvem numa pasta de rascunho:
  - abrindo o app, o service worker registra (escopo `/`, `updateViaCache: 'none'`), ativa e grava a
    casca (18 arquivos, o `/` e nenhum `/index.html`); o Chromium lê o manifest sem erro, vê os três
    ícones e **não acusa erro de instalação** (`Page.getInstallabilityErrors` vazio);
  - entrando com a conta de teste e recarregando, ele assume a página;
  - o navigation preload, medido: a navegação a `/api/v1/me` chega ao servidor 1 vez sem ele e 2 com
    ele (desligado em seguida);
  - **com o servidor desligado, recarregar mostra "Sem conexão."**, "O que você ditou continua no PC."
    e **Tentar de novo**, com a Aurora e as fontes (a casca guardada; captura em 390 px); a volta do
    Google e a `/api/v1/me` sem rede dão o erro do navegador, e não a casca;
  - no fim, o service worker desregistrado, a casca e os dados do site apagados (`Storage.clearDataForOrigin`,
    os cookies e o cache), o navegador fechado, a pasta do perfil apagada e o servidor parado.
- **O que a F5 NÃO provou:**
  - **o Android de verdade e o Chrome do PC instalado:** o roteiro (`docs/teste-manual-nuvem-f5.md`)
    é quem confere o ícone no launcher, o nome "Whisper", a abertura escura, a cor da barra e o app
    abrindo sem o servidor; abrir o Chrome do Mauro está fora das regras desta máquina;
  - o **WebAPK** pelo cabo (o celular não alcança o `localhost` do PC para montar o app; o Chrome pode
    criar só um atalho), a volta do Google pela Custom Tab até o app instalado, o `__Host-sessao` em
    https, a atualização depois de um deploy com o app instalado, o Caddy mantendo o `no-cache` do
    `sw.js` e o modo avião de verdade: só na F11, com HTTPS;
  - **a janela à parte de entrar de novo no app instalado** (onde o Android a abre, se o cookie e o
    `BroadcastChannel` chegam a ela, e se ela se fecha), que a F4 deixou para cá: precisa do Google, e
    fica para a F11;
  - o tempo que o service worker custa na navegação sem o preload; Firefox e Safari; o iPhone (depois
    do MVP).
- **Riscos que ficam:**
  - **o service worker fica no navegador** de quem abriu o app em `localhost:8080` e responderia ao `/`
    e aos `/assets` de outro projeto nessa porta (sempre pela rede primeiro no `/`, e nos `/assets` só
    um nome igual ao da casca sairia dela). O roteiro manda desinstalar e limpar no fim;
  - **sem rede, a casca é a da última versão instalada**; uma aba aberta há dias continua na versão de
    quando abriu (sem aviso de "nova versão", como pede a UI pequena);
  - a prova do service worker é num `node:vm`, não num navegador, na suíte; a do Chromium foi feita
    uma vez, à mão, e não está no `go test` nem no `pnpm test`;
  - no tema claro, a abertura do app instalado pisca escura antes do app claro (o custo da decisão 3).

### Decisões da F6 (05-out-2026)

O que a F6 decidiu dentro do que a ADR deixou para ela, o que rodou e o que ela **não** provou. O Mauro
autorizou a F6 em 05-out-2026 ("um PC conectado que manda os ditados para o celular"; a F6 é só o
pareamento, e o envio é a F7). As três decisões do plano da F6 foram implementadas **com a
recomendação como padrão** e estão marcadas **[a confirmar pelo Mauro]**; cada uma diz a alternativa
e como desfazer.

- **Desconectar no PC também revoga na nuvem. [Decisão 1 da F6; confirmada pelo Mauro em 06-out-2026.]** Num
  esforço só: `DELETE /api/v1/devices/{o próprio}` com o token, 5 segundos; o token daqui é apagado
  **sempre**, e, sem a confirmação da nuvem, o painel diz "Desconectado neste PC. A nuvem não
  respondeu: revogue-o em Aparelhos, no app." Um `401` ou `404` da nuvem contam como revogado. **A
  alternativa:** só apagar o token local (o aparelho fica na lista até alguém revogar ou passarem 180
  dias); desfaz-se tirando a chamada a `revogar` de `Servico.Desconectar` (`internal/agent/nuvem`) e o
  aviso `sem_resposta`.
- **O token sem uso vence em 180 dias. [Decisão 2 da F6; confirmada pelo Mauro em 06-out-2026 (já era a D7).]** A sugestão da ADR
  (D7). Conta `COALESCE(last_used_at, created_at)`: um PC conectado que sincroniza nunca chega lá; um
  PC parado há seis meses aparece desconectado e é só conectar de novo (o mesmo aparelho, pelo
  `install_id`). Vale no resolver do Bearer (`401`) e na lista de aparelhos (some). **A alternativa:**
  outro número (`cloudstore.TokenSemUso`) ou nenhum vencimento (tirar as duas checagens).
- **O PC revogado some da lista de Aparelhos. [Decisão 3 da F6; confirmada pelo Mauro em 06-out-2026.]** A linha
  fica no banco, com `revoked_at` e sem token, porque os ditados que vieram dele apontam para ela (F7),
  e parear de novo a reaproveita. **A alternativa:** mostrá-lo como "Revogado" (a lista passaria a ler
  os revogados, com uma etiqueta e sem o botão).
- **Onde mora o quê.**
  - No servidor: `internal/server/pareamento.go` (as rotas), `cloudstore/pareamento.go` (os tipos,
    `PrazoDoPedido` de 10 minutos e `TokenSemUso`), `cloudstore/memory_contas.go` e
    `pgstore/pareamento.go` (o armazenamento), `cloudauth/segredo.go` (`NovaConferencia`) e
    `painel.go` (`GET /device/authorize` serve o app). A suíte `authtest` ganhou 13 casos do pareamento
    e dos aparelhos (memória e Postgres), e `pgstore/pareamento_pg_test.go`, as provas que só o banco
    dá.
  - No PC: `internal/agent/nuvem` (sem build tag, testado com dublês: `url.go`, `servico.go`,
    `retorno.go`), `internal/storage/instalacao.go` (só `install_id` e `nuvem_*` na tabela
    `instalacao`; o dono fixo não se mexe por ali), `internal/agent/localapi/nuvem.go` (a Local API),
    `internal/agent/painel/servidor.go` (`/nuvem/retorno`) e `cmd/agent/nuvem.go` (`montarNuvem`,
    `politicaDaNuvem` e `versaoDeDesenvolvimento`).
  - No web: `web/src/NuvemNoPC.tsx` (a seção Nuvem do PC), `web/src/nuvem/AprovarPC.tsx` (a página de
    aprovação), a seção Aparelhos em `ConfiguracoesNuvem.tsx`, o caminho em `AppNuvem.tsx` e o cliente.
  - E o instalador (`#define AlvoDaChaveNuvem`).
- **Nenhuma dependência nova e nenhuma migration**: a 0002 (`instalacao`), a 0013 (`devices`,
  `pairing_requests`) e a 0014 (`device_tokens`) serviram como estavam.
- **O `state` é o id do pedido** (a proposta da F1, confirmada): o PC confere o `state` do retorno
  contra o pedido em curso; quem protege a troca é o PKCE.
- **Aprovar é `fetch`, não formulário (muda do `06`).** `POST
  /api/v1/device-auth/requests/{pedido}/approve` devolve `{ "retorno": "http://127.0.0.1:<porta>/
  nuvem/retorno?code=…&state=…" }`, e a página vai para lá com `location.replace` (o Voltar não volta
  à aprovação). Por quê: o Chromium aplica o `form-action 'self'` da CSP ao redirecionamento de um
  formulário, e o `303` para o `127.0.0.1` seria bloqueado; assim, a CSP fica como está e um erro
  aparece na própria tela. A página só navega para `http://127.0.0.1:<porta>/nuvem/retorno?…`.
- **A porta do retorno é a do painel que já existe** (aleatória a cada subida, só `127.0.0.1`):
  nenhum ouvinte a mais. O pedido em curso (o id, o verificador e o prazo) fica só na memória: um
  Agent reiniciado no meio o perde, e o retorno é `400`.
- **O PC monta o endereço que abre** com a URL que a pessoa digitou (`{url}/device/authorize?pedido=…`):
  o `auth_url` da resposta é ignorado (um servidor ruim não escolhe o que o PC abre). A resposta
  continua a trazê-lo.
- **O código de conferência**: 4 caracteres de `23456789ABCDEFGHJKMNPQRSTUVWXYZ` (sem 0, 1, I, L e O),
  por `crypto/rand`, sem viés. Só conferência; quem protege é o retorno com o PKCE.
- **A troca, de uso único, com o desafio na busca:** o `UPDATE ... SET used_at` acha o pedido pelo
  SHA-256 do `code` e pelo desafio (`S256(code_verifier)`), aprovado, sem uso e sem vencer. Um
  verificador errado não acha a linha e não queima o `code` (como o `pre` da revisão da F3): o link
  copiado não conecta e não derruba quem começou. Tudo o que não vale é o mesmo `400 CODIGO_INVALIDO`.
- **O aparelho:** o mesmo de antes por `(user_id, install_id)`: parear de novo (depois de revogar, ou
  de desconectar) o reaproveita, com `revoked_at` nulo e a `version` mais um. Um token por aparelho: o
  de antes sai e recebe `401`. **Revogar** é `revoked_at` mais apagar o token; o aparelho fica.
- **Limites** (`Limites`, em memória, por processo): o start e a troca juntos, 10 por minuto por IP; o
  start, 100 por minuto de todos (ele grava sem sessão). `429` com `Retry-After`.
- **A D7, como ficou:** o build de desenvolvimento é o que tem `version == versaoDeDesenvolvimento`
  (o literal de `main.go`, sem `-X`; um teste prende os dois juntos, sem mudar o
  `montar-instalador.ps1`), e ele só conecta no modo portátil (`portatil` ou `portatil-legado`), com
  `https` ou `http://localhost:<porta>`. O release só aceita `https`, em qualquer modo. Tudo antes de
  qualquer rede. **E um alvo do token por modo [derivada da D7, nova]:** `AuroraWhisper/nuvem` no
  instalado e `AuroraWhisper/nuvem-portatil` no `bin\`. Sem isso, o `bin\` (conta de teste) e o
  instalado, que são do mesmo usuário do Windows, dividiriam a credencial: conectar um trocaria o token
  do outro, e o PC real passaria a usar a conta de teste. O desinstalador apaga só o do instalado.
- **A URL da nuvem** é normalizada para a origem (`esquema://host[:porta]`, em minúsculas, sem a porta
  padrão), até 256 caracteres; usuário e senha, caminho além de `/`, busca, `#` e host fora do ASCII
  são recusados ("Use um endereço https://." ou, no desenvolvimento, "Use https:// ou
  http://localhost.").
- **A D3, como ficou:** outra nuvem (a `nuvem_url` gravada é outra) é recusada **antes da rede**;
  outra conta (o `nuvem_conta` gravado é outro) só se sabe depois da troca, e aí o PC revoga o
  aparelho recém-criado com o token novo, não o guarda e diz "Este PC já está ligado a outra conta."
  O Desconectar mantém a URL, a conta e o e-mail (é o que a D3 compara, e o que preenche o campo).
- **Conectado** é o aparelho gravado em `instalacao` **e** o token no Gerenciador. Sem o token (o
  desinstalador, ou à mão), o PC aparece desconectado, e a instalação é limpa.
- **O Conferir** (`GET /api/v1/me` com o token) é ação de quem abriu a tela: o painel o pede ao
  aparecer e ao voltar à aba, e o Agent fala com a nuvem no máximo a cada 30 s. Nenhum laço (o laço é
  da F7b). O `401` apaga o token e desconecta ("Este PC foi desconectado na nuvem."); a rede fora não
  ("A nuvem não respondeu agora. O PC continua conectado.").
- **O cliente HTTP da nuvem** não segue redirecionamento (uma `3xx` é falha: o token nunca vai a outro
  endereço), tem prazo de 10 s e lê até 64 KiB. **Só HTTP/1.1** (13-out-2026, auditoria de rede do
  PC): o mesmo transporte da chave de IA (`ia.NovoTransporteHTTP1`), porque o HTTP/2 do `net/http`
  escreve o `Authorization` e o começo de cada corpo no log padrão (o `agent.log`) quando `GODEBUG`
  traz `http2debug` (`TestHTTP2Debug_TokenDoAparelhoNaoVaiParaOLog`).
- **O log:** o do servidor é só o padrão da rota; o do Agent diz o que aconteceu com motivos de lista
  fechada, nunca o `code`, o `state`, o pedido, o verificador, a conferência, o token, a URL ou o
  e-mail.
- **Mudanças do plano, achadas no caminho:**
  - **o retorno recarregado** (achado no navegador): depois de conectar, recarregar a página do
    `127.0.0.1` dizia "Não deu para conectar", com o PC conectado. Agora o Agent lembra, só na memória,
    o pedido que deu certo, e o mesmo retorno mostra "PC conectado" de novo, sem trocar nada; sai
    quando o PC deixa de estar conectado;
  - **"Nada vai para a nuvem"**, e não "Nada sai deste PC", na nuvem desligada: com o Polish ligado,
    o texto vai ao provedor de IA, e a frase não pode prometer menos do que sai (o cuidado da F4).
- **Conferido num Chromium** (o navegador do app, 05-out-2026), de ponta a ponta, com o servidor de
  teste da nuvem (o binário de `cmd/server teste`, com o build da nuvem numa pasta de rascunho, numa
  porta livre) e um "PC" de mentira, temporário e fora do git: o servidor do painel de verdade, a Local
  API de verdade e o `nuvem.Servico` de verdade, com o Cofre na memória (nada no Gerenciador de
  Credenciais) e o abrir que só escrevia no log (nenhuma janela). O Conectar, o código igual nos dois
  lados, a tela Entrar com "Entre para aprovar a conexão do PC.", a conta de teste caindo na
  aprovação, o Aprovar levando o Chromium de `http://localhost` ao `http://127.0.0.1:<porta>/nuvem/
  retorno` ("PC conectado"), o painel em "Conectado", os Aparelhos com o PC, o Revogar na linha, "PC
  revogado.", o painel em "Este PC foi desconectado na nuvem." com o campo preenchido, conectar de novo
  (a mesma conta), o Desconectar do PC tirando o PC da lista, e o pedido vencido. Em 375 px e no
  desktop, claro e escuro: nada passa da largura, os botões da aprovação e da pergunta do Revogar têm
  44 px no toque. O log da nuvem teve só os padrões das rotas. No fim, a sessão foi encerrada, as abas
  fechadas, os servidores parados e a pasta de rascunho apagada.
- **Testes.** No Go: o pacote `nuvem` (29 testes, 51 com os subtestes, sem build tag), a Local API
  (4), o painel (1), a montagem e os portões em `cmd/agent` (4, um só no Windows), o instalador (o
  golden e as credenciais), e o pareamento de ponta a ponta contra o servidor de verdade
  (`internal/server/integration`); a `authtest` e a suíte de contrato da F2 com o pareamento. `go test
  ./...`: 65 pacotes, todos verdes. No `web/`, 657 (eram 619): a seção Nuvem do PC (14), a aprovação e
  os Aparelhos (18), e o `bundle.test.ts` (o local sem `/device` nem a aprovação; o da nuvem sem
  `/api/nuvem` nem o campo do endereço).
- **Mutação.** Em cópias feitas por `git archive`, cada troca conferida antes (o trecho aparece uma vez
  só, e o arquivo mudou) e restaurada byte a byte depois (e, no fim, a cópia inteira igual ao commit);
  o teste que matou cada uma foi lido (a asserção certa, nenhum erro de compilação: quatro trocas da
  primeira rodada não compilavam ou não casavam e foram refeitas). **59 mutações, 58 mortas e 1
  equivalente.** No servidor (23, só na memória: o Postgres estava desligado): o desafio fora da busca
  da troca, o `code` reusado, vencido, aprovar duas vezes ou vencido, o SHA puro no lugar do HMAC, o
  revogado e os 180 dias no resolver, os 180 dias e o `user_id` na lista, o token revogando outro
  aparelho, revogar o de outro usuário, cada limite (por IP, total e na troca), o token aprovando,
  listando e lendo o pedido, parear de novo ficando revogado ou com o token velho, a arrumação sem os
  pedidos e o retorno para `localhost`. No pacote `nuvem` (18): o `state` não conferido, consumido só
  depois da rede, consumido quando errado, sem prazo e sem teto, o release com `http`, o
  desenvolvimento com `http` fora do `localhost` e fora do portátil, o alvo trocado, a D3 sem a conta,
  sem a URL antes da rede e sem revogar o aparelho novo, o `401` que não desconecta, a rede fora que
  desconecta, o Desconectar que só apaga com a nuvem, o retorno repetido (que troca de novo, e que diz
  "conectado" depois de desconectar) e o token ausente tratado como conectado. Na montagem (6): o
  navegador e o Gerenciador sem o portão, o modo e a versão fora da política, o retorno sem o `Host` e
  o alvo da montagem fixo. No instalador (2): sem o token e com o do `bin\`. No `web/` (10): o retorno
  de qualquer endereço, o pedido que não é id indo à nuvem, o login sem a volta, a aprovação em
  qualquer caminho, o Revogar sem perguntar, o Agora não aprovando, o Abrir de novo com qualquer
  endereço, a aba escondida conferindo, sem conferir ao voltar à aba e o endereço do Agent por cima do
  digitado. **Uma sobrevivia e virou teste:** o alvo fixo na montagem (o teste conferia a função do
  alvo, não o que `montarNuvem` passa; agora monta os três modos). **A equivalente:** tirar o
  `revoked` da lista de aparelhos na memória: revogar apaga o token, e a lista parte dos tokens (no
  Postgres, a junção com `device_tokens` faz o mesmo); a checagem é a segunda guarda. As mutações no
  SQL do Postgres (o desafio, o `used_at`, o `expires_at` e o `approved_by IS NULL` no `WHERE`, o
  `revoked_at` e os 180 dias) ficam para quando o contêiner estiver de pé: sem ele, os testes de
  `pgstore` pulam, e toda mutação ali sobreviveria sem dizer nada.
- **O que a F6 NÃO provou:**
  - **o Postgres:** o contêiner de teste estava desligado (o arquivo da URL existe, e o banco não
    respondeu); os testes de `pgstore` (oito trocas simultâneas do mesmo `code` dão um token só, oito
    aprovações, a conta desativada) e a `authtest` contra o Postgres **não rodaram**. Rodam com
    `scripts/postgres-de-teste.ps1` e `AURORA_TEST_POSTGRES_URL`;
  - **o Chrome do Mauro e o Gerenciador de Credenciais de verdade:** só o roteiro
    (`docs/teste-manual-nuvem-f6.md`) os confere; nenhum teste abre janela ou grava credencial;
  - **a Local Network Access e o HTTPS-Upgrades do Chrome numa nuvem pública** (risco 3): o retorno foi
    de `localhost` para `127.0.0.1`, os dois locais; numa nuvem em `https` na internet, só na F11;
  - os testes do pacote `nuvem` no Linux (só o `GOOS=linux go vet` rodou); o `-race` (sem compilador C).
- **Riscos que ficam:**
  - ~~**a nuvem de teste reiniciada prende o `bin\`**~~ (resolvido na revisão, 06-out-2026: a conta de
    teste nasce com o id fixo). Fica o caso mais raro: o `bin\` ligado a **outro endereço** (outra
    porta) ou a outra conta não tem saída pelo painel; o "esquecer esta nuvem" no PC é da F7
    (**resolvido na F7c**, 06-out-2026: Configurações > Nuvem > Esquecer esta nuvem);
  - **a porta do retorno é escolhida por quem inicia o pedido** (`redirect_port`, `validarInicio`):
    no phishing, o atacante inicia o pedido e tem o verificador; o único segredo que lhe falta é o
    `code`. Se a pessoa aprovar apesar do aviso, o navegador dela leva o `code` a
    `http://127.0.0.1:<porta do atacante>/nuvem/retorno`, e ele só vaza se um serviço local naquela
    porta repassar a URL para fora (um proxy, um servidor de desenvolvimento que redirecione
    preservando a busca). É inerente à RFC 8252 (7.3: qualquer porta no loopback); o aviso, o código de
    conferência, a conta mostrada e o `Referrer-Policy: no-referrer` mitigam. **Endurecimento feito
    (decisão do Mauro, 06-out-2026):** o servidor aceita só a faixa dinâmica do Windows (49152 a 65535), onde o
    `guard.Listen(0)` do Agent cai por padrão; afasta as portas de serviços conhecidos (3000, 5173,
    8080, 8888), mas quebra o pareamento de quem mudou a faixa com `netsh`;
  - quem tiver o painel do PC aberto (o token da Local API) pode ligá-lo a uma nuvem que controla, como
    já pode ler o histórico; o Conectar é ação da Local API, atrás do token e do `Origin`;
  - o `/nuvem/retorno` é um `GET` que muda estado (o retorno a loopback da RFC 8252); um site qualquer
    pode mandar o navegador lá, mas sem o `state` do pedido em curso (um UUID v7) é `400` e nada é
    consumido;
  - o `code` passa na URL do `127.0.0.1` e fica no histórico do navegador: é de uso único e já foi
    trocado;
  - os limites zeram quando o servidor reinicia (como os do login).
- **A revisão da F6 (06-out-2026).** Dez achados (dois médios e oito baixos; dois pares repetem o mesmo ponto),
  corrigidos no mesmo branch, salvo o que diz abaixo:
  - **a conta de teste com o id fixo** (o médio do roteiro): cada subida de `cmd/server teste` criava a
    conta com um UUID v7 novo, e o `bin\` conectado numa subida recusava a seguinte ("Este PC já está
    ligado a outra conta."), com a aba do `127.0.0.1` mandando clicar em Conectar de novo, o que nunca
    resolvia. Agora ela nasce com `IDDaContaDeTeste` (`00000000-7e57-7000-8000-000000000001`) pelo
    `MemoryStore.AddConta` (só o servidor de teste o usa), e o login de teste a acha pelo `sub`. Dois
    testes prendem isso (o `/me` do servidor de teste e duas subidas com o mesmo id). O roteiro diz o
    que fazer se a nuvem reiniciar (conectar de novo) e se aparecer "outra conta" (o endereço da
    primeira vez; senão, parar e avisar). **Muda o comportamento** só do servidor de teste;
  - **a conta que aprova** na página de aprovação: "Este PC vai ficar ligado à conta {e-mail}.", com o
    e-mail do `/me` que a abertura já carregou, antes do Aprovar (quem está na conta errada vê antes de
    prender o PC nela; o e-mail longo quebra no celular, medido a 375 px);
  - **a segunda guarda da D3 com teste:** outra `nuvem_url` gravada entre o Conectar e o retorno (sem
    conta e com a mesma) dá `409`, o token fora do Cofre e o aparelho novo revogado com o token novo. A
    mutação `temURL && false` sobrevivia; agora morre;
  - **o comentário do token no Gerenciador:** "Aurora Whisper: token deste PC na nuvem" nos alvos
    `nuvem` e `nuvem-…` (`chaves.ComentarioDe`), e não "chave de API do provedor de IA". A credencial é
    montada em `credencialDe`, que o teste confere sem chamar o sistema. **Muda o comportamento:** o
    comentário de uma credencial nova da nuvem (a que já existe muda na próxima gravação);
  - **o `09`:** o item do CSRF voltou à lista da etapa 5, antes do bloco da F6, e o bloco ganhou a
    porta do retorno nos riscos;
  - **a porta do retorno** ficou registrada nos riscos (acima e no `09`); o endurecimento (só 49152 a
    65535) **foi feito** depois da revisão, por decisão do Mauro (06-out-2026), com teste das portas 1024,
    8080 e 49151 recusadas e 49152 e 65535 aceitas;
  - **o "Agora não" não invalida o pedido [aceito]:** a tela diz "Nada foi conectado.", e recarregar a
    página mostra o Aprovar de novo até o pedido vencer (10 minutos). Não abre brecha (aprovar exige a
    sessão, e o `code` só serve ao PC que começou, com o verificador); o `08` registra. Uma rota que
    invalide o pedido fica para a F7, se fizer falta;
  - **o Postgres continua sem rodar** (os dois achados que o pediam): o Docker Desktop estava fechado
    na revisão, e abri-lo é com o Mauro (`scripts/postgres-de-teste.ps1` não o abre). Fica para antes
    do merge no `main` ou, no máximo, antes da F7: subir o contêiner, rodar `go test
    ./internal/server/... -count=1` com `AURORA_TEST_POSTGRES_URL` lido do arquivo e refazer as
    mutações do SQL (o desafio, o `used_at`, o `expires_at` e o `approved_by IS NULL` no `WHERE`; o
    `revoked_at` e os 180 dias no resolver);
  - **testes:** `go test ./...` com 65 pacotes verdes, `go vet` (Windows e `GOOS=linux`), `gofmt` e,
    com a tag `logindeteste`, `cmd/server` e `internal/server/...`; no `web/`, 659 (eram 657), o lint e
    os dois builds (numa pasta de rascunho). **15 mutações**, numa cópia por `git archive` (cada troca
    conferida antes e restaurada depois; a cópia inteira igual ao commit no fim), **todas mortas**:
    tirar o `AddConta` do servidor de teste, cada uma das seis recusas do `AddConta`, a guarda da URL na
    troca, o comentário (o alvo `nuvem`, o `nuvem-portatil`, qualquer `nuvem*` e o fixo em
    `credencialDe`) e, no web, a conta fixa, a conta ausente e o e-mail que não vem do `/me`. Quatro
    trocas da primeira rodada não compilavam (uma variável sem uso) e foram refeitas;
  - **conferida num Chromium** (o navegador do app), com o servidor de teste numa porta livre e o
    build da nuvem numa pasta de rascunho: a página de aprovação com a conta, em 375 px e no desktop,
    claro e escuro, nada passando da largura (também com um e-mail de 84 caracteres); o `/me` com o id
    fixo. No fim, a sessão encerrada, a aba fechada e o servidor parado; o log só com os padrões das
    rotas.
- **Para as fatias seguintes:**
  - **F7b:** o laço de sync usa o token do `Cofre` no alvo do modo, e o `401` do sync segue o caminho
    do `Conferir` (apaga o token e desconecta); a bandeja ganha o estado; o "esquecer esta nuvem" no PC
    (o `bin\` ligado a outro endereço ou conta; acima); antes dela, os testes de Postgres da F6 (abaixo);
  - **F7c:** o "último envio" na seção Nuvem, a prévia do envio inicial e a frase da nuvem na
    Privacidade do PC;
  - **F11:** a Local Network Access e o HTTPS-Upgrades com a nuvem em `https`; os limites do
    pareamento atrás do proxy.

### Decisões da F7a/b (06-out-2026)

O que a F7a e a F7b decidiram dentro do que a ADR deixou para elas, o que rodou e o que elas **não**
provaram. O Mauro autorizou a F7 em 06-out-2026 ("deu certinho" na F6 e o roteiro aprovado da etapa
5); a F7c (o envio inicial e o selo no painel do PC) é a próxima. As três decisões do plano foram
implementadas **com a recomendação como padrão**, e o **Mauro as confirmou em 06-out-2026** (a
decisão 2 já na forma refeita, abaixo, e com ela a mudança na regra da fila); cada uma diz a
alternativa e como desfazer. A decisão 2 foi **refeita na revisão do mesmo dia** ("A revisão da
F7a/b", abaixo): a primeira versão, "reconectar é recomeço", foi apresentada com uma consequência que
não era a real, e perdia edição sem aviso.

- **Desconectado, nada roda; na fila, só entra a mudança num ditado que a nuvem já tem.** (A primeira
  versão, decidida com a ADR, "Fila no PC", sem pergunta, era "desconectado, nada entra na fila"; a
  revisão a refinou, dentro da decisão 2, abaixo, e **o Mauro aprovou essa mudança de regra em
  06-out-2026**.) Lido na própria transação da gravação: com
  `instalacao.nuvem_aparelho` presente (conectado), todo ditado gravado entra; sem ele, só a mudança
  numa linha com `server_version` (a nuvem já tem o ditado), com os campos que mudaram. O ditado que a
  nuvem nunca teve (o novo, e o de antes da primeira conexão) continua de fora, com as mudanças dele:
  é do envio inicial (F7c), que deixa a lixeira de fora. Nenhum pedido sai desconectado.
- **O intervalo da busca: 60 s. [Decisão 1 da F7b; confirmada pelo Mauro em 06-out-2026.]** Além disso, o laço roda
  na subida do Agent, 2 s depois de cada gravação local (para juntar as seguintes) e ao conectar.
  **A alternativa:** 30 s (a edição do celular aparece antes, com o dobro de pedidos vazios) ou 5 min;
  é a constante `IntervaloPadrao` (`internal/sync/laco.go`).
  **Nota de 11-out-2026 (ADR-021, decisão P, subfatia S, escolha menor 17; aprovada pelo Mauro em
  10-out-2026):** com o painel do PC aberto e lendo o contador de mudanças (`GET /api/nuvem/mudancas`),
  o laço busca a nuvem a cada **10 s** (`IntervaloComPainelPadrao`), por uma janela de 15 s depois de
  cada leitura (`JanelaDoPainelPadrao`); com o painel fechado, continuam os **60 s** desta decisão
  (`IntervaloPadrao`, que não mudou). O painel visível lê o contador a cada 3 s e relê a lista quando ele
  muda. Uma edição feita no celular aparece no painel aberto em até cerca de 13 s.
- **A volta do PC que ficou fora continua de onde parou, no mesmo epoch. [Decisão 2 da F7b, refeita
  na revisão de 06-out-2026; confirmada pelo Mauro em 06-out-2026.]** As `server_version` que o PC guardou continuam
  valendo enquanto o epoch é o mesmo, e a volta não reenvia nada com a base vazia:
  - **reconectar** (outro pareamento da mesma instalação: outro `nuvem_conectado_em`, ou outro
    aparelho depois de revogar; a D3 garante a mesma conta) passa o cursor e o epoch ao pareamento
    novo; só os envios em curso são esquecidos (o aparelho novo os manda com outros ids);
  - **o `410`** no mesmo epoch só volta o cursor a 0: o pull do 0 traz o estado atual e as lápides
    vivas, reconhece pela versão o que o PC já tem, e nada sobe;
  - **o que mudou aqui com o PC fora** já está na fila (o item acima), com os campos e a base certa, e
    a nuvem compara campo a campo, como numa edição feita conectado;
  - **o recomeço** (cursor 0, `server_version` vazia, o reenvio com a base 0) fica para o **epoch
    novo** (um backup restaurado; o "apagar meus dados" revoga os aparelhos) e para a primeira rodada
    da instalação, e volta ao plano: **sem a lixeira**.

  O efeito, provado contra a nuvem de verdade (`sync_reconexao_e2e_test.go`): um ditado que outro
  aparelho editou com o PC fora chega ao PC sem selo (sondas A e D); a edição feita fora num ditado
  de outro aparelho, que ninguém mais mexeu, vale, sem selo (B); a edição feita fora que bate com uma
  edição de outro aparelho vira selo, com o texto deste PC guardado nele (o conflito de verdade); a
  lixeira feita fora sobe (apagar vence), e a lixeira antiga não volta a subir, então o restaurar mais
  novo do celular fica (C); o selo de verdade que outro PC deixou continua com a edição dele quando o
  PC desatualizado volta (E e F).

  **A primeira versão, e por que saiu.** "Reconectar é recomeço" (e o `410` também): `server_version`
  vazia em tudo e o histórico da janela reenviado com `base_version` 0 e o texto local. Ela foi
  descrita aqui como "a edição feita desconectado num ditado que a nuvem também mudou vira selo", mas
  o efeito real era mais amplo. Com a base 0, a nuvem (`mergePatch`) dá o texto como desconhecido
  sempre que o último texto veio de outro aparelho, então: (a) todo ditado que outro aparelho editou
  com o PC fora ganhava um selo com o texto **velho** deste PC, sem o PC ter mexido em nada, e
  "trocar" voltava ao texto velho; (b) a edição feita desconectado num ditado de outro aparelho virava
  selo mesmo sem nenhuma mudança na nuvem, e o texto do PC voltava ao da nuvem; (c) a lixeira antiga
  reenviada desfazia um restaurar mais novo do celular (a derivada "o recomeço leva a lixeira", que
  saiu junto); e (d) o PC desatualizado **trocava o selo de verdade** (a edição perdedora de outro PC)
  pelo texto velho dele, e essa edição sumia da nuvem e, no pull seguinte, de todos: perda de edição
  sem aviso. O `TestSyncE2E_ReconectarNaoTiraDaLixeira` da primeira versão só passava porque o ditado
  editado era do próprio PC.

  **A alternativa:** a primeira versão (descartada pelo que está acima), ou marcar a mudança feita
  fora numa coluna da linha em vez da fila (o mesmo efeito, com mais código). Para desfazer esta: em
  `Fila.Preparar`, tratar outro pareamento como recomeço; em `Motor.Rodada`, o `410` chamar
  `Recomecar` no lugar de `ZerarCursor`; e, em `storage.emitir`, tirar o `server_version IS NOT NULL
  OR`. **O que esta versão não resolve** (fica no epoch novo, F11): com a base vazia, a nuvem compara
  contra tudo, e um ditado cujo texto difere do backup e cujo último texto veio de outro aparelho vira
  selo; um selo pendente pode ser trocado; e a primeira rodada depois do backup ainda manda a fila com
  as bases do epoch velho (a resposta é descartada, mas a nuvem já gravou), como já fazia.
- **A bandeja: uma linha desabilitada. [Decisão 3 da F7b; confirmada pelo Mauro em 06-out-2026.]** "Nuvem: em dia",
  "Nuvem: N para enviar", "Nuvem: sem conexão", "Nuvem: aguardando" (o `429`: a nuvem pediu para
  esperar), "Nuvem: resposta inesperada" (a nuvem no ar, fora do contrato: um `403` persistente, por
  exemplo; os dois da revisão) ou "Nuvem: desconectada", logo abaixo do ASR, sem ícone nem aviso; sem
  nuvem configurada, a linha não aparece (o menu de quem nunca conectou fica igual). O menu só lê
  memória (o laço guarda a linha). **A alternativa:** nada na bandeja (só o painel, F7c) ou o ícone
  mudar; desfaz-se tirando o `Nuvem` de `tray.Estado` e o `resumoDaNuvem` do `cmd/agent`.
- **~~O recomeço leva também a lixeira do que já tinha subido.~~ Desfeita na revisão (06-out-2026).**
  Ela existia por causa da primeira versão da decisão 2 (sem ela, pôr na lixeira desconectado e
  reconectar traria o ditado de volta da lixeira). Com a volta que continua do cursor, a lixeira feita
  fora sobe pela fila, e o recomeço do epoch novo volta ao plano: **sem a lixeira**. O risco que a
  revisão da F2 deixou para a F7c (o backup restaurado trazendo de volta da lixeira um item que o PC
  pôs lá) continua com a F7c.
- **Onde mora o quê.**
  - `internal/storage`: a migration `0005_sync_fila.sql`; a emissão do marcador
    (`transcription_sqlite.go`); e `fila_sqlite.go`, a fila (o SQL da `sync_outbox`, da `sync_state`
    e do aplicador), que executa o que o sync decide (`AcaoLocal`), sem decidir nada.
  - `internal/sync` (reescrito): `fio.go` (o contrato de fio e o `SyncClient`), `payload.go` (o
    payload e o corte do lote), `decisao.go` (as decisões puras), `motor.go` (uma rodada), `cliente.go`
    (o HTTPS) e `laco.go` (o laço em segundo plano). Saem o `engine`, o `outbox`, o `resolve`, o
    `state`, o `apply` e os testes do cliente antigo em `internal/server/integration`.
  - `internal/agent/nuvem`: `Credencial`, `TokenRecusado` e `AoConectar`; `cmd/agent/sync.go`
    (`montarSync` e `iniciarSync`); `internal/agent/tray` (a linha); `internal/agent/localapi` (o teto).
  - **O modo antigo do servidor** (`internal/server.New`, sem `Store`, com o sync genérico da F1-D)
    **ficou**: só os testes dele o usam, e tirá-lo é tarefa à parte.
- **Nenhuma dependência nova.**
- **A migration 0005 (aditiva, na transação do Migrator).** A `sync_outbox` da 0001 (uma linha por
  mudança, com o texto em `payload_json`; nunca teve escritor) foi **renomeada** para
  `sync_outbox_0001`, intocada, com o índice dela; a nova nasceu no lugar: PK `(entity_type,
  entity_id)`, `campos` (bits: 1 o texto, 2 a lixeira; criar não é bit, é `server_version` nulo),
  `mutation_id` e `versao_enviada` (o envio em curso) e `criado_em`, **sem texto**. `sync_state` ganhou
  `sync_epoch`, `pareado_em` e `ultimo_erro`; `local_transcriptions`, o selo (`conflito_texto`,
  `conflito_device_id`, `conflito_em`). Passaram a valer as colunas que já existiam: `server_version`,
  `sync_state` (`pending` | `recusado` | `fora_da_janela`) e `last_sync_error`. **Sem cópia do banco:**
  o projeto só copia antes de mexer em linhas (`dono.go`), e esta não muda nenhuma. **A prova**
  (`migracao_0005_internal_test.go`): um banco num arquivo, migrado até a 0004, com seis ditados (dois
  na lixeira, com acento), as preferências, a instalação (o dono e a nuvem da F6), o cursor e uma linha
  na fila velha, lido tabela por tabela antes e depois: cada linha igual, as colunas novas nulas, a
  fila velha intacta como `sync_outbox_0001`, o repositório lendo os seis; e uma 0005 que falha no meio
  não deixa nada (nem a renomeação).
- **A emissão.** Na mesma transação da linha, no repositório SQLite: `Put` (o ditado novo e o texto
  que chega atrasado: sem bit), `UpdateFinalText` (o bit do texto), `SoftDelete` e `Restore` (o bit da
  lixeira, só quando a linha muda). Um `INSERT ... SELECT ... WHERE` (conectado, ou, desconectado, um
  ditado com `server_version`, desde a revisão; e o ditado fora de `fora_da_janela`) `ON CONFLICT DO
  UPDATE SET campos = campos | excluded.campos`: duas edições, um
  marcador, e a gravação durante o voo não troca o envio em curso. O marcador que falha desfaz a linha
  (o erro volta a quem gravou, como antes). `PurgeExpired` apaga os marcadores do que expurga.
  Excluir de vez não existe no PC; o aprendido é da F8. Depois do commit, o aviso ao laço
  (`AoEnfileirar`, um canal de 1 com `select default`): o ditado nunca espera. O `PATCH
  /api/history/{id}` ganhou o teto de 64 KiB e a recusa do caractere nulo (`400`), o que a nuvem
  recusaria, e um teto de 512 KiB no corpo.
- **O push (o motor).** Os marcadores saem numa leitura curta, os em voo primeiro (o marcador sem
  linha, ou de um ditado fora da janela, sai ali); o payload é montado da linha (criar: o ditado
  inteiro, com o `deleted_at` se estiver na lixeira; mudar: só os campos do marcador, `null` é
  restaurar; nunca `target_app`; UUID em minúsculas); o lote é cortado em 500 mutações e 512 KiB de
  payload, sempre com pelo menos uma; os ids (v7) e a versão lida vão para o marcador numa transação
  curta, só nos que não estavam em voo; e a rede, sempre fora de transação. A resposta é aplicada numa
  transação por lote, com as decisões da tabela (`06`, "O lado do PC"): `applied` grava a versão,
  menos com `mesclado`; `duplicate` segue o `original`; `conflict` sem "mudou" aplica o `state`; os
  `rejected` e os `gone` como a ADR diz. **Uma diferença do plano:** o `rejected` comum (`recusado`)
  tira o marcador só se a linha não mudou durante o voo; se mudou, o marcador fica, porque a edição
  nova (a de 64 KiB a menos, por exemplo) pode valer e já está naquele marcador. E um status ou motivo
  que o PC não conhece vira `recusado` com `resposta_desconhecida`, sem travar a fila. **Outra, achada
  na revisão do próprio código:** um ditado com texto acima de 64 KiB (uma linha de antes do teto no
  painel) nem sai: fica `recusado` com `texto_grande`, sem marcador; a nuvem o recusaria, e um texto
  enorme passaria do corpo do push e travaria a fila inteira, lote após lote. **Nenhum pull depois de
  um push sem resposta**, e a rodada seguinte começa pelo reenvio com os mesmos ids.
- **O aplicador (o pull).** Uma transação por página, com o cursor: lápide com marcador pula, sem
  marcador apaga; upsert com marcador pula; `entity_version` igual à `server_version` é eco (nunca pelo
  `device_id`); o resto grava o texto, a lixeira (com `purge_at` + 30 dias), o status e o selo (nulo
  limpa), sobe a `version` local e grava a versão; o novo entra inteiro, com o dono local, `version`
  1 e o aparelho de origem. Uma linha de outro dono com o mesmo id não é tocada. A recusa por regra
  (tipo desconhecido, id que não é UUID, payload de outro id, sem data, texto acima de 64 KiB ou UTF-8
  inválido, versão zero) conta "não aplicado", e o cursor anda; um erro de E/S desfaz a página e o
  cursor fica. `has_more` sem o cursor andar é falha; até 100 páginas por rodada.
- **O cursor, o epoch e o recomeço** (como a revisão os deixou). `sync_state` tem uma linha, a do
  pareamento atual. `Fila.Preparar`: o estado deste pareamento é devolvido; o de outro pareamento
  desta instalação (a única linha) passa ao novo, com o cursor e o epoch, e só os envios em curso são
  esquecidos (`InicioReconexao`); sem linha (a primeira rodada) ou com mais de uma (um banco mexido à
  mão), recomeço (`InicioRecomeco`). O epoch guardado nulo adota o da resposta (push ou pull); outro
  descarta a resposta inteira e recomeça. O `410` só zera o cursor (`Fila.ZerarCursor`). O recomeço,
  numa transação: cursor 0, epoch nulo, os envios em curso esquecidos, um marcador (criar) para cada
  ditado com `server_version`, na janela de 6 meses (a conta de `internal/retention`), **fora da
  lixeira** e fora de `fora_da_janela`, e `server_version` nulo em todos; os marcadores que já existiam
  ficam, e também viram criar. Depois do `410` e do epoch novo, a rodada roda de novo, no máximo uma
  vez para cada um; o segundo seguido volta como erro (defeito da nuvem: espera longa).
- **O cliente.** `Authorization: Bearer`, prazo de 30 s, nenhum redirecionamento seguido, só HTTP/1.1
  (13-out-2026: o mesmo motivo do cliente do pareamento, `TestHTTP2Debug_TokenNaoVaiParaOLog`, que
  procura também o texto do push), o pedido até
  1 MiB e a resposta até 16 MiB (8 MiB até a revisão; passou é falha de rede); os erros tipados do
  `18` (`401`, `410`, `429` com o `Retry-After` até 10 min, transitório e contrato), com um código de
  lista fechada, sem a URL,
  o token ou o erro cru do `net/http`. Confere o lote (um resultado por mutação, na ordem, com o
  epoch) e o cursor (não volta).
- **O laço.** Uma goroutine, só com o histórico aberto, parada antes de o banco fechar; um pânico nela
  é registrado e a para, sem derrubar o ditado. A rodada lê a credencial na hora (o token do Cofre,
  pelo portão; nada fica em memória entre as rodadas) e, desconectado, não monta cliente nenhum. A
  espera: a rede, o prazo, um `5xx` ou um erro local, de 2 s a 5 min (a `BackoffPolicy`, com o "equal
  jitter"), e a gravação não fura a espera; o `429`, o pedido mais até 25% (no máximo 30 s), nunca
  menos; a resposta fora do contrato e o recomeço que não resolveu, o teto. O `401` chama o
  `TokenRecusado`: o caminho do 401 do Conferir, numa função só, e **só se o token recusado ainda é o
  guardado** (um 401 de um pareamento anterior, com o PC já reconectado, não o desconecta). O log da
  rodada só com contagens e códigos (`11`), e a primeira falha de uma sequência em `WARN`.
- **O que a F7b NÃO mudou na F6:** o Conferir, o pareamento e o Desconectar são os mesmos; o
  `Desconectar` deixa na fila o que estava lá, e o que mudar depois num ditado que a nuvem já tem
  entra nela (sobe na reconexão, que continua do cursor: a mesma nuvem e a mesma conta, que a D3
  garante).
- **Testes.** Puros, sem banco (rodam no Linux também): a tabela do push (26 casos: os cinco status,
  com e sem "mudou", com e sem mescla, o `duplicate` pelo `original`, o que não é do envio em curso),
  a do aplicador (21: o novo, o com marcador, o eco pela versão e nunca pelo `device_id`, a lápide, a
  recusa por regra), o payload (criar inteiro, mudar só os campos, restaurar, sem `target_app`), o
  corte do lote, o cliente (os status, o `Retry-After` em segundos e em data, o redirecionamento, os
  tetos, o prazo, o erro sem URL) e a espera. Com o SQLite numa pasta temporária: a migração; a emissão
  nos cinco caminhos e o que não emite (desconectado de três jeitos, fora da janela, a lixeira
  repetida, a edição recusada); duas edições, um marcador; a falha no marcador desfazendo a linha; o
  recomeço; os pendentes; o epoch; cada pedaço da `AcaoLocal`; o erro de E/S desfazendo a página; o
  motor contra uma nuvem de roteiro (nenhum pull depois do push sem resposta, o que muda no voo vai de
  novo, o `410` uma vez só, o epoch novo, o cursor); o laço (a subida e a gravação, desconectado sem
  pedido, a espera depois de falhar, o `401`, a bandeja). A montagem de verdade no `cmd/agent`, sob
  `go test`: nenhum pedido passa do portão do Gerenciador, e o ditado grava com o marcador na hora.
  **A ponta a ponta** (`internal/server/integration`, a nuvem de verdade sobre o Store em memória
  num `httptest`, com um porteiro que derruba a rede, perde a resposta, manda o `410` com o envelope do
  contrato (o servidor só o manda na F9), redireciona e trava o push; dois PCs com o SQLite, o
  `nuvem.Servico` com o Cofre na memória e o token de mentira criado no Store, e o laço; o celular pela
  sessão): ditar no PC1 chega ao celular e ao PC2, sem eco; editar no celular volta aos PCs (a aba
  velha recebe `409`); editar nos dois lados dá o selo nos três, sem perder texto, e a escolha no
  celular o limpa nos PCs; campos diferentes com um pull no meio chegam inteiros; a edição antiga
  contra a lixeira vai para o selo; lixeira, restaurar e excluir de vez nos dois sentidos (o `gone
  apagado` com uma edição pendente também); o PC sem rede acumula e envia sem duplicar, sem pull na
  queda; a resposta perdida e o reinício reenviam os mesmos ids antes de qualquer pull; revogar dá
  `401` e desconecta, o token sai do Cofre e o ditado segue local fora da fila; o `410` relê do 0 e o
  epoch novo recomeça, nos dois sem duplicar (`TestSyncE2E_410ReleDoZeroEEpochNovoRecomeca`);
  reconectar não tira da lixeira; o `429` do limite de verdade; o
  redirecionamento não leva o token; desconectado, zero pedidos; o ditado não espera o push travado; o
  laço em segundo plano sobe sozinho; e os logs dos três lados sem texto, token, id, URL, e-mail ou
  sessão. `go test ./... -count=1`: 65 pacotes verdes; `go vet ./...` no Windows e com `GOOS=linux`;
  `gofmt -l .` vazio. O `web/` não mudou.
- **Mutação.** Numa cópia feita por `git archive` (o commit `e890b8f`), cada troca conferida antes (o
  trecho aparece o número esperado de vezes, e o arquivo mudou) e restaurada byte a byte depois; no
  fim, a cópia inteira igual ao commit. O motivo de cada morte foi lido (a asserção certa); timeout
  contaria como morta (nenhum houve). **59 mutações, 58 mortas e 1 equivalente** (a 59ª, o texto
  grande que sai, rodada à mão do mesmo jeito depois, morre em `TestMotor_TextoGrandeNaoSaiNemTrava`).
  No storage (18): a
  emissão desconectado, fora da janela, sem juntar os campos, no Put, com o campo errado, na lixeira
  sem efeito e o expurgo sem o marcador; o aplicador que ecoa, que não sobe a versão local e que mexe
  na linha de outro dono; os em voo por último; o `MarcarEnvio` trocando o id em voo; o epoch
  diferente aceito; o cursor não gravado; o recomeço sem zerar a versão, sem a janela e **sem a
  lixeira (o plano)**; e a reconexão sem recomeço. Nas decisões (14): a versão mesclada guardada, o
  "mudou" ignorado, o `duplicate` sem o `original`, o `conflict` sem o `state` e com o `state` mesmo
  tendo mudado, o `mutacao_reusada` e o `base_invalida` como recusa comum, o `gone apagado` sem
  apagar, o upsert e a lápide com marcador sem pular, **o eco pelo `device_id`** e sem eco, o teto do
  texto no pull e a lápide que não apaga. No motor (5): o pull depois do push sem resposta, o reenvio
  com id novo, o `410` sem recomeço, o cursor que não anda aceito e o recomeço sem limite. No cliente
  (8): o redirecionamento seguido, os tetos (a resposta, o pedido, o `Retry-After` na data HTTP), o
  `401` e o `410` não reconhecidos e o lote que não bate. No laço (4): a gravação furando a espera, o
  `401` sem desconectar, a rodada desconectado e o `429` esperando menos. No payload (3), no `nuvem`
  (3: o `401` de um token velho, a credencial sem o pareamento, conectar sem acordar o laço), no
  `PATCH` local (2) e na bandeja (1). **A equivalente:** tirar o teto do `Retry-After` em segundos (o
  teto final, logo depois, dá o mesmo valor; a primeira guarda só evita a conta com um número
  enorme). Seis trocas da primeira rodada não compilavam (uma variável ou um import sem uso) e foram
  refeitas numa forma que compila. Uma rodada revelou uma corrida no próprio
  `TestLaco_EsperaDepoisDeFalhar` (a linha da volta no log é escrita logo depois de a bandeja mudar):
  o teste passou a esperar a linha, e a suíte do laço rodou 20 vezes seguidas sem falhar.
- **O que a F7a/b NÃO provou:**
  - **o Postgres:** a ponta a ponta roda contra o `MemoryStore`, que tem o mesmo contrato por
    construção (as mesmas regras e a mesma suíte de contrato); o contêiner continua desligado, e os
    testes de Postgres da F6 continuam pendentes;
  - **o Agent de verdade, o Gerenciador de Credenciais e a bandeja no Windows:** nada rodou fora do
    `go test`; o Mauro testa depois da F7c (abaixo);
  - **o `410` do servidor:** o envelope é o do contrato, mandado por um porteiro de teste; a marca
    d'água é da F9;
  - o `-race` (a máquina não tem compilador C); o laço é testado com tempos curtos e espera por
    condição, não por relógio de mentira.
- **Riscos que ficam:**
  - **o histórico de antes de conectar não sobe** até a F7c (é o pedido: "nada entra na fila sem
    conexão", com a exceção que o Mauro aprovou em 06-out-2026, a mudança num ditado que a nuvem já
    tem; **resolvido na F7c**, com o envio inicial e a completação a cada rodada); um PC conectado hoje mostra os ditados dos outros e manda os novos, não os antigos. O
    mesmo vale para o ditado **novo** feito desconectado depois de já ter sincronizado: ele espera o
    envio inicial da F7c (a mudança num ditado que a nuvem já tem, essa, sobe na volta);
  - **a lixeira vencida no PC sem rede:** o expurgo de 30 dias apaga o marcador junto; um ditado que
    foi para a lixeira com o PC sem rede por mais de 30 dias não leva a lixeira à nuvem, e um pull
    posterior (se alguém mexer nele) o traz de volta, ativo; **[resolvido na F10a (a D3, 08-out-2026):
    o expurgo segura a lixeira com marcador até a nuvem saber, no máximo até os 6 meses do ditado;
    ver "Decisões da F10"]**
  - **o recomeço grava uma versão nova de cada ditado** que reenvia (a F2 já dizia): depois da revisão,
    só no epoch novo (um backup restaurado), em que ele é o histórico da janela inteira, com uma
    pessoa uns milhares de escritas. É também quando ele pode criar selos sem conflito e trocar um selo
    pendente (decisão 2, "O que esta versão não resolve"): fica para a F11, que restaura backups;
  - **o selo é um só** (a F2): um segundo conflito antes da escolha troca o perdedor guardado. A
    revisão tirou o caminho que tornava isso comum (a volta do PC desatualizado);
  - **a espera do ditado pela transação do sync** (uma conexão só no `historico.db`): o próximo Put
    espera o Aplicar de uma página cheia (uns 40 ms medidos) e, no epoch novo, o recomeço (de 360 a
    480 ms com 7 mil ditados de tamanho comum nesta máquina, com SSD; a revisão, com outros dados,
    mediu de 224 a 286 ms). O texto já foi inserido no campo; só o histórico grava depois. Num disco
    lento é mais (`espera_do_ditado_test.go` mede e falha se crescer muito);
  - **um PC da F7 pula o aprendido** que a F8 mandar no pull ("não aplicado", e o cursor anda): a F8
    precisa de um recomeço ao atualizar, ou de um pull só do aprendido (**substituído por "Decisões
    da F8", "Sem o cursor 0 na 0007"**: nenhuma nuvem tem aprendido antes da F8, e o recomeço não é
    preciso; risco aceito lá);
  - **a linha da bandeja demora** até a próxima rodada para dizer "sem conexão" depois de a rede cair
    (no máximo o intervalo, ou 2 s depois de um ditado).
- **O que o Mauro vai testar (depois da F7c; sem roteiro agora):** conectar o `bin\` à nuvem de teste,
  ditar e ver no app; editar no app e ver no painel do PC; editar o mesmo ditado nos dois e ver o selo;
  lixeira e restaurar dos dois lados; desligar a nuvem de teste, ditar, religar e ver subir (e a
  bandeja dizendo "sem conexão" e depois "em dia"); revogar no app e seguir ditando. Da revisão:
  desconectar, editar no PC um ditado que já tinha subido (e editar outro no app), reconectar, e ver
  a edição do PC subir sem selo e a do app chegar ao PC sem selo; e, se o mesmo ditado for editado
  nos dois enquanto desconectado, ver o selo com o texto do PC.
- **Para as fatias seguintes:**
  - **F7c:** o envio inicial (a prévia, a cópia do banco, os 6 meses sem a lixeira, em lotes: o mesmo
    caminho, pondo marcadores sem bit e deixando a fila subir); o selo no painel do PC e a escolha
    pelo `resolver_conflito`; o "último envio" na seção Nuvem (`sync_state.last_success_at` e
    `ultimo_erro`; desde a revisão, o `last_success_at` de uma rodada ociosa é gravado no máximo a
    cada 10 minutos); a frase da nuvem na Privacidade do PC; o "esquecer esta nuvem" (a F6 já o
    pedia), que agora precisa também apagar a `sync_state`, a fila e as `server_version`: sem isso, a
    instalação "esquecida" continuaria marcando na fila as mudanças dos ditados que a nuvem tinha. O
    envio inicial põe marcador só no ditado sem `server_version` e sem marcador (os outros a nuvem já
    tem, ou já estão na fila);
  - **F8:** o aprendido na fila (outro `entity_type`), no pull e nas decisões, e o recomeço ao
    atualizar (o PC da F7 pulou o que veio antes) (**o recomeço foi substituído por "Decisões da
    F8", "Sem o cursor 0 na 0007"**);
  - **F9:** o `410` de verdade (a marca d'água), que o PC já trata; **[substituído pela D2 da F9,
    de 08-out-2026: a nuvem não responde `410` no mesmo epoch; ver "Decisões da F9"]**;
  - **F11:** os testes de Postgres pendentes (F6), antes do deploy.

### A revisão da F7a/b (06-out-2026)

Uma revisão do mesmo dia, com sondas contra a nuvem de verdade, achou dois problemas altos e um médio
no desenho da volta do PC, e seis baixos. O que foi feito, problema por problema (commits `e48962f`
a `6850937`, sobre `73c8ab8`):

- **Alto: o recomeço com a base 0 apagava em silêncio a edição perdedora guardada no selo.** O PC
  desatualizado que reconectava (ou recebia `410`) reenviava o texto velho com a base vazia, a nuvem
  o tomava por um conflito novo e trocava o selo de verdade por ele; a edição sumia da nuvem e, no pull
  seguinte, de todos. **Feito:** a decisão 2 refeita (acima): no mesmo epoch, a volta não reenvia nada
  com a base vazia. **Não feito:** a parte da proposta que mudava o servidor ("não substituir um selo
  numa mutação de base 0: responder `conflict` sem gravar"): com o `conflict`, o PC aplica o estado da
  nuvem, e o texto dele, que não foi para o selo, se perderia no lugar do outro. O caso que sobra é o
  epoch novo (um backup restaurado), que fica para a F11.
- **Alto: a decisão 2 tinha sido apresentada com a consequência errada.** Corrigido o texto aqui, no
  `07`, no `05`, no `06` e no `13`, com o que a primeira versão fazia de fato ((a) a (d), na decisão 2)
  e o que a nova faz. As sondas da revisão viraram testes de ponta a ponta, com o ditado criado por
  **outro** aparelho (o teste antigo só passava porque o ditado era do próprio PC):
  `TestSyncE2E_VoltaSemMudancaNaoCriaSelo` (A e D, pela reconexão e pelo `410`),
  `TestSyncE2E_EdicaoDesconectadoNumDitadoDeOutroPC` (B), `TestSyncE2E_ConflitoDesconectadoDaOSelo`,
  `TestSyncE2E_VoltaNaoDesfazORestaurarEALixeiraDeForaSobe` (C, e a lixeira feita fora),
  `TestSyncE2E_VoltaDoPCDesatualizadoMantemOSelo` (E e F) e
  `TestSyncE2E_DitadoNovoDesconectadoEsperaOEnvioInicial`. Contra o código de antes, as cinco
  primeiras falhavam (a do conflito de verdade e a do ditado novo passavam).
- **Médio: o recomeço reenviava a lixeira e desfazia um restaurar mais novo.** Resolvido pela mesma
  correção: a lixeira feita fora sobe pela fila, a antiga não volta a subir, e o recomeço do epoch novo
  voltou ao plano, sem a lixeira (a derivada "a confirmar" saiu).
- **Baixo: o ditado espera a transação do sync (uma conexão SQLite só).** Medido e documentado (os
  números em "Riscos que ficam"), com dois testes que falham se a espera crescer muito
  (`espera_do_ditado_test.go`). O recomeço, a transação mais longa, agora só roda no epoch novo e na
  primeira rodada (em que nenhuma linha tem `server_version`, e ele é curto). **Não feito:** quebrar
  o recomeço em transações curtas retomáveis (o caso ficou raro; quebrar pede um marcador "recomeço em
  curso" que sobreviva a uma queda no meio).
- **Baixo: uma página do pull podia passar do teto de 8 MiB e travar o cursor.** O teto foi a 16 MiB:
  `TestSyncE2E_TetoDaRespostaCobreOPiorCaso` confere a conta contra as constantes da nuvem (6 vezes o
  orçamento de 2 MiB mais o último item, e o envelope de 500 itens), e
  `TestSyncE2E_PaginaComTextoEscapado` puxa do servidor uma página real de uns 12 MiB (17 ditados de 64
  KiB de `<`). **Não feito:** o `SetEscapeHTML(false)` no servidor (mudaria a resposta de todas as
  rotas do sync, e o teto do cliente basta).
- **Baixo: a primeira subida do Agent novo num PC já conectado.** Não é defeito de código; vai no
  relatório para o Mauro (abaixo, "O que o Mauro providencia").
- **Baixo: `RegistrarRodada` a cada minuto, mesmo ocioso.** O sucesso só é anotado se a rodada fez
  algo, se a anterior falhou, ou a cada 10 minutos (`AnotacaoOciosa`); a falha, só quando o código
  muda. **Não feito:** o `busy_timeout` no banco do Agent (muda como o Agent abre o `historico.db`, fora
  da F7; o laço já tolera o `SQLITE_BUSY` com um `WARN`).
- **Baixo: o recomeço sobre um histórico grande.** O mesmo do segundo baixo: agora é raro, e está medido.
- **Baixo: a linha do histórico e o marcador na mesma transação.** **Mantido de propósito:** com a volta
  que continua do cursor, um marcador perdido nunca mais seria reposto (não há mais recomeço na
  reconexão), e a mudança não subiria nunca. A falha de E/S que derruba o marcador derruba a linha de
  qualquer jeito.
- **Baixo: a bandeja dizia "sem conexão" para tudo.** "Nuvem: aguardando" no `429` e "Nuvem: resposta
  inesperada" fora do contrato; só o texto muda (a espera é a mesma), e uma sequência de falhas de
  tipos diferentes continua dando um `WARN` só.

**O que mudou de comportamento:** desconectado, a mudança num ditado que a nuvem já tem entra na fila
(o ditado novo, não); a reconexão continua do cursor, sem reenviar; o `410` só relê do 0; o recomeço
do epoch novo deixa a lixeira de fora; a resposta pode ter até 16 MiB; a bandeja tem as duas linhas
novas; o PC parado deixa de escrever no banco a cada minuto; o log tem `releituras` e `reconexoes`.

**Testes e mutação.** `go test ./... -count=1`: 65 pacotes verdes; `go vet ./...` no Windows e com
`GOOS=linux`; `gofmt -l .` vazio; o `web/` não mudou. **23 mutações, 23 mortas**, numa cópia por `git
archive` de cada commit, cada troca conferida antes (o trecho aparece uma vez, e o arquivo mudou) e a
cópia inteira igual ao commit no fim; o motivo de cada morte foi lido. No storage (13): a emissão
desconectado que não marca o que a nuvem tem e a que marca tudo; a reconexão que recomeça, que passa
adiante com dois cursores, que não esquece os envios, que não passa o cursor ao aparelho novo, e o
mesmo aparelho reconectado tomado pelo mesmo pareamento; o `410` que não zera o cursor; o recomeço com
a lixeira; e as quatro guardas da anotação ociosa. No motor (4): o `410` que recomeça, o `410` sem
limite, a reconexão contada como recomeço e o epoch novo que não recomeça. No laço (5): o `429` e a
resposta fora do contrato como "sem conexão", a volta só de "sem conexão", o laço que não diz que a
rodada fez algo e a segunda falha de outro tipo em `WARN`. No cliente (1): o teto de 8 MiB. Duas
trocas da primeira rodada (o `410` sem limite e o epoch que não recomeça) não compilavam (uma variável
sem uso) e foram refeitas numa forma que compila. Duas, vistas sem teste antes de a rodada começar,
ganharam teste antes dela (a volta da falha que limpa o erro, e a rodada que fez algo anotada pelo
laço: `TestLaco_AnotaARodadaQueFezAlgo`); uma sobreviveu (a segunda falha de outro tipo em `WARN`) e
ganhou `TestLaco_UmaSequenciaDeFalhasUmWarn`; e o `410` sem limite morria só pelo prazo (o teste
girava, e o binário dele ficou rodando até ser parado à mão): os dois testes que o pegam passaram a
falhar na hora, e a troca, rodada de novo, morre em 10 s.

### Decisões da F7c (06-out-2026)

O que a F7c decidiu dentro do que a ADR deixou para ela, o que rodou e o que ela **não** provou. O Mauro
autorizou a F7c em 06-out-2026 (respondeu as perguntas da F7a/b e aprovou o roteiro da etapa 5). As
três decisões do plano foram implementadas **com a recomendação como padrão** e foram **confirmadas
pelo Mauro em 06-out-2026** (a D3 com as correções da reconferência, abaixo); cada uma diz a
alternativa e como desfazer.

- **D1. O histórico sobe com um clique, depois de conectar. Confirmada pelo Mauro em 06-out-2026**
  (o histórico só sobe depois do clique em "Enviar o histórico", perguntado uma vez; os ditados novos
  sobem sempre). Conectado, e
  depois da primeira rodada que deu certo com a conexão, a seção Nuvem pergunta: "Vão subir N ditados
  dos últimos 6 meses. A lixeira não vai. Antes, uma cópia do histórico fica neste PC.", com **Enviar o
  histórico** (`POST /api/nuvem/enviar`). Por quê: é dado real, e o número e a cópia aparecem antes de
  qualquer envio; a mesma pergunta serve à conta nova depois do esquecer. Sem nada elegível, não há
  pergunta nem cópia: a rodada grava o aceite sozinha (como o dono fixo num banco vazio). **A
  alternativa:** subir sozinho ao conectar, com a prévia antes do Conectar; desfaz-se gravando o
  aceite no `Conectar` (e a cópia junto) e tirando a pergunta da tela.
- **D2. Depois do esquecer, para a conta nova sobe só o que nasceu neste PC. Confirmada pelo Mauro
  em 06-out-2026.** O filtro do envio exige `device_id` nulo, e o `emitir` ganhou a guarda `device_id IS NULL
  OR server_version IS NOT NULL` (na revisão, ela ficou dentro da regra nova do `emitir`: sem
  `server_version`, só o `Put` do ditado novo nascido aqui marca): o ditado que veio de outro PC pela conta antiga fica aqui, e uma
  edição dele não entra na fila da conta nova. Por quê: era a preocupação da D3 (o histórico da conta A
  subindo para a B). **A alternativa:** subir todo o histórico deste PC (os de outro PC passariam a
  aparecer como deste, na conta nova); desfaz-se tirando `t.device_id IS NULL` do `filtroElegivel` e a
  guarda do `emitir`.
- **D3. Um ditado que a nuvem tem e este PC tem diferente, sem ter confirmado, não é sobrescrito.
  Confirmada pelo Mauro em 06-out-2026** (a reconciliação no pull, com a referência do esquecer, como
  está, com as correções da reconferência: a referência deixou de recuar, e o que mudou aqui antes do
  esquecer é anotado item por item; ver "A reconferência da F7c"). No pull, a linha sem `server_version`, sem marcador, que **mudou aqui** e
  em que o lado do PC pode vencer (outro texto, ou na lixeira só aqui; na revisão, saiu "do outro lado
  da lixeira": a lixeira só na nuvem não convergia) ganha um marcador de criar e sobe
  inteira, com a base vazia, numa volta a mais da mesma rodada; a nuvem decide pelas regras de sempre
  (apagar vence; texto contra texto vira selo). O que não mudou aqui é o ditado velho: vale o da nuvem.
  Fecha o risco que a F2 deixou para a F7c (o backup restaurado tirando da lixeira o que o PC pôs lá:
  `TestSyncE2E_EpochNovoNaoTiraDaLixeira`) e o "esquecer e voltar à mesma conta". **A alternativa:**
  o PC aplicar o da nuvem (o ditado sai da lixeira, como antes); desfaz-se tirando o ramo do
  `MarcadorCriar` de `DecidirMudanca`.

**"Mudou aqui", e uma mudança do plano achada na ponta a ponta.** O plano subia toda diferença. Na
volta à mesma conta, um ditado que o celular editou com o PC esquecido virava selo com o texto velho
deste PC (um selo à toa). Agora o esquecer grava a referência `nuvem_esquecida_em`, e só sobe a linha
cujo `updated_at` é depois dela (na revisão, a referência passou a recuar para antes da mudança mais
antiga que não subiu: ver "A revisão da F7c"; na reconferência, ela voltou a ser o instante do
esquecer, e o que mudou aqui antes dele sem a nuvem receber passou a ser anotado item por item, na
`sync_mudou_aqui`: ver "A reconferência da F7c"); sem esquecer (o epoch novo, que apaga a referência no `Recomecar`),
toda diferença sobe, como no plano. Um segundo achado da mesma ponta a ponta: a primeira rodada do
pareamento novo, que recomeça pelo `Preparar`, apagava a referência; só o recomeço do epoch novo a
apaga. E quando a linha é igual à da nuvem, o PC aplica o estado dela (o plano dizia "só grava a
`server_version`"; aplicar traz também o selo, sem perder nada).

- **O envio inicial, como ficou** (`internal/storage/envio_sqlite.go`, `Fila.EnviarHistorico`):
  - **o filtro único** (`elegiveis`, que a prévia, o envio e a completação usam): do dono,
    `server_version` nulo, fora da lixeira, `sync_state` `pending`, `device_id` nulo, dentro dos 6
    meses (`retention.ActiveExpiryAt`, a conta da nuvem; um corte grosso de uma semana no SQL, a
    janela exata em Go) e sem marcador. Um teste prende a prévia igual ao que o clique marca, com um
    ditado para cada filtro (inclusive o limite exato dos 6 meses);
  - **a cópia** (resposta 3): `VACUUM INTO` em `historico.db.antes-da-nuvem-AAAAMMDD-HHMMSS.db`, ao lado
    do banco, conferida tabela por tabela (o `copiarBanco` do `dono.go`, com a marca como parâmetro e
    `MaxCopias` por marca: as `antes-do-dono` não contam). Ela segura a conexão única do Agent entre a
    contagem e o VACUUM: nenhuma escrita passa no meio, e o `Put` do ditado espera, em vez de falhar
    com `SQLITE_BUSY` numa segunda conexão. **Medido** (`envio_espera_internal_test.go`, 7 mil ditados
    de 1,25 KiB, SSD desta máquina): o `Put` esperou de 139 a 165 ms; o envio inteiro (a cópia e os 14
    blocos), de 420 a 500 ms; a cópia ficou com os 7 mil, sem o ditado novo. Uma cópia por conta
    (`nuvem_copia`, gravada na transação do primeiro bloco); a cópia que falha não marca nada, e a tela
    diz por quê ("Não deu para copiar o histórico. Nada foi enviado." e, se der, "Já há 3 cópias…" ou
    "Falta espaço no disco."); o log diz o motivo com o nome do arquivo, nunca o caminho;
  - **os marcadores**: blocos de 500 por transação, com o filtro de novo no `INSERT` e `ON CONFLICT DO
    NOTHING`; o total soma em cada bloco; o aceite vem por último. Uma queda no meio (provada com um
    gatilho temporário que derruba o segundo bloco) deixa o primeiro bloco e a cópia registrada, sem o
    aceite: o clique seguinte continua de onde parou, sem outra cópia. Clicar de novo depois não faz
    nada;
  - **a completação**: toda rodada, logo depois do `Preparar` (`Fila.CompletarEnvio`), marca o que
    ficou de fora depois do aceite (o ditado novo feito desconectado; depois da revisão, também o
    restaurado e o que saiu da fila pela lixeira e voltou), fora do progresso (`inicial` 0);
  - **a migration 0006** (aditiva, só colunas na `sync_outbox`: `inicial`, `selo_escolha` e
    `selo_em`, com `CHECK`; desde a reconferência, também a tabela `sync_mudou_aqui`, que nasce
    vazia), provada contra um banco na 0005 com dados (um ditado com selo, um de outro
    PC, marcadores com um em voo, o cursor, a instalação, as preferências), lido tabela por tabela; uma
    0006 que falha no meio não deixa nada;
  - **a ordem da fila**: `mutation_id IS NULL, inicial, criado_em, entity_id` (o em voo, o ditado novo
    e as edições, o histórico). O ditado feito durante o envio grava na hora (o laço não segura o
    banco durante a rede) e sobe no lote seguinte, na frente;
  - **o progresso**: feitos = total − marcadores `inicial`; recusa e `gone` contam como feitos; o
    painel mostra "Enviando o histórico: 120 de 800." (relido a cada 2 s só enquanto envia, ou enquanto
    a primeira rodada não termina, e com a seção à vista) e, no fim, "O histórico subiu para a nuvem."
    com quantos não subiram; a bandeja, "Nuvem: enviando 120 de 800" (o motor chama `AoLote` a cada
    lote; vale no `429`; sem rede, vence "sem conexão");
  - **o envio espera a primeira rodada com a conexão** (mudança do plano, achada ao desenhar a volta à
    mesma conta): o `POST /api/nuvem/enviar` antes dela é `409` ("O PC ainda está conferindo a nuvem.
    Tente de novo em alguns segundos."), e a tela diz "Conferindo a nuvem…". Assim o pull do cursor 0
    já reconciliou o que a nuvem tem, e a prévia é o que ela não tem.
- **A resposta de um pareamento que mudou na rodada não recomeça** (achado ao revisar o esquecer; o
  caminho já existia na F7a/b, com a reconexão no meio de uma rodada): a resposta que chegava depois
  de o PC desconectar e esquecer (ou reconectar) caía no estado sem cursor, e a rodada recomeçava com o
  aparelho velho, recriando o cursor dele, apagando a referência do esquecer e, num pareamento novo, o
  cursor e as versões da nuvem. Agora o `Aplicar` sem o estado do pareamento devolve
  `ErrPareamentoMudou`, e a rodada só termina (`TestSyncE2E_EsquecerNoMeioDaRodada`, que falha contra
  o código de antes).
- **O selo no PC.** O mesmo componente do app (`ItemDitado`), agora com os botões no PC
  (`resolverConflito` no `ambienteLocal`; `PATCH /api/history/{id}` com `resolver_conflito`, o
  contrato da nuvem). A escolha vale na hora no PC (`ResolverConflito`, numa transação: só na versão
  atual e com selo, senão `409`) e, num ditado que a nuvem tem, entra no marcador (`selo_escolha` e
  `selo_em`), também desconectado; o payload de mudar a leva sem o texto. **Uma mudança pequena de
  contrato no servidor**, a mesma na memória e no Postgres (`cloudstore.DecidePush`): a escolha vale
  sobre o mesmo selo (o mesmo `conflict_at`) e sem o texto mudado depois da base por outro aparelho;
  senão cai, vale o que chegou primeiro, e a resposta é `applied` mesclado com versão nova (o pull traz
  o estado de verdade, com o selo de volta se ele ainda existe). Base 0 com a escolha é
  `campo_desconhecido`; escolha ou data inválida, `campo_invalido`. A suíte de contrato ganhou o caso
  (`Selo_EscolherAVersaoPeloPC`).
- **O "Esquecer esta nuvem"** (Configurações > Nuvem, desconectado, com uma nuvem gravada; `POST
  /api/nuvem/esquecer`, `409` conectado ou conectando): numa transação (`Fila.EsquecerNuvem`), saem as
  chaves da conexão e do envio (a URL, a conta, o e-mail, o nome, o aparelho, o pareamento, o aceite,
  o total e a cópia registrada), o cursor, a fila inteira e, nas linhas, só `server_version`, a
  situação e o erro; o token sai do Gerenciador, se ainda estiver lá, pelo portão. Ficam todos os
  ditados (o texto, a lixeira, o aparelho de origem, o selo), o dono, o `install_id`, as preferências
  e os arquivos das cópias. Grava `nuvem_esquecida_em` (a referência da D3; desde a reconferência,
  junto com o anotado da `sync_mudou_aqui`) e, se a nuvem esquecida
  tinha confirmado ditados nascidos aqui, `nuvem_ditados_em_outra` (a pergunta seguinte diz, desde a
  revisão, "Alguns destes ditados podem já estar na nuvem que este PC esqueceu."). A pergunta na tela
  diz o endereço, a conta e quantas mudanças não subiram (desde a revisão, "… Elas ficam neste PC.";
  desde a reconferência, só as mudanças num ditado que a nuvem tem, sem o histórico e o ditado novo
  que não subiram), com o foco no
  Cancelar e o Esc cancelando. Isso fecha o risco da F6 (o `bin\` ligado a outro endereço ou conta
  não tinha saída pela tela). O estado desconectado passa a trazer o e-mail da conta gravada.
- **Onde mora o quê.** `internal/storage`: `envio_sqlite.go` (o envio, a completação, o progresso, o
  resumo, `HistoricoDaNuvem`), `esquecer_sqlite.go`, a 0006, `ResolverConflito` no repositório e o
  `MarcadorCriar` na fila. `internal/sync`: a completação e o `AoLote` no motor, a reconciliação em
  `decisao.go`, a escolha no payload, o progresso na bandeja. `internal/server/cloudstore`: a escolha
  pelo push. `internal/agent/nuvem/historico.go`: o resumo no `Estado`, o `EnviarHistorico` e o
  `Esquecer`. `internal/agent/localapi`: as duas rotas, o `PATCH` da escolha e o selo no item.
  `cmd/agent`: `historicoDaNuvem` (a fila e o `historico.db` da tabela; o teste estático da montagem
  confere o caminho). `web/src`: `NuvemNoPC.tsx`, `textosDaNuvem.ts`, o cliente e o ambiente locais e
  a frase da Privacidade em `Configuracoes.tsx`.
- **Nenhuma dependência nova.**
- **A prova de que o histórico real não perde nem muda nada**
  (`internal/server/integration/sync_historico_de_verdade_e2e_test.go`): o fixture
  `testdata/historico-bin-0005/dados.sql` (dados inventados, com o esquema das migrations de verdade
  0001 a 0005: 221 ditados de todo tipo, três de outro dono, as preferências, a instalação revogada da
  F6, o dicionário antigo e uma correção) é montado num arquivo e **copiado byte a byte** para a pasta
  do PC. Contra a nuvem de verdade: a 0006 não muda nenhuma linha; a conexão e a primeira rodada não
  sobem nada; a cópia de antes do envio é o banco inteiro, tabela por tabela; a nuvem fica com os 181
  que aceita (dos 183 elegíveis: o texto acima do teto e a data no futuro são recusados), na versão 1;
  cada ditado do PC fica igual ao do fixture, coluna a coluna, fora as três do sync; e, depois do
  esquecer, o histórico volta a ser **exatamente** o do fixture.
- **A ponta a ponta** (`internal/server/integration/sync_envio_e2e_test.go`, a nuvem de verdade sobre o
  Store em memória): mil ditados (lixeira, mais de 6 meses, de outro PC, recusados) → a nuvem com
  exatamente os 700 elegíveis, na versão 1, uma mutação por ditado, e as linhas do PC iguais fora do
  sync; o reinício no meio (a resposta perdida e o Agent reaberto) reenvia o lote com os mesmos ids,
  sem duplicar; ditar durante o envio (o push preso na rede) grava na hora e passa na frente; o selo
  escolhido no PC chega ao celular e ao PC 2, a escolha que cai (o celular escolheu antes, ou o texto
  mudou) não perde texto; esquecer e ir a outra conta sobe só o que nasceu aqui, com cópia nova, e a
  conta antiga fica intacta; voltar à mesma conta não duplica, não cria selo à toa e a lixeira feita
  aqui não volta; o epoch novo não tira da lixeira o que o PC pôs lá; o ditado novo desconectado sobe
  depois do aceite (`TestSyncE2E_DitadoNovoDesconectadoEsperaOEnvioInicial`, refeito); e os logs sem
  texto, id, URL ou caminho.
- **Testes.** `go test ./... -count=1`: os pacotes todos verdes; `go vet ./...` no Windows e com
  `GOOS=linux`; `gofmt -l .` vazio. No `web/`: 678 testes (eram 659), o lint, e os dois builds
  numa pasta de rascunho; o `bundle.test.ts` confere que o envio e o esquecer só existem no build local.
- **Conferido no navegador do app** (o "PC" de mentira, temporário e fora do git: o servidor do painel
  de verdade, a Local API, o `nuvem.Servico` e o laço de verdade, o Cofre na memória, sobre um
  `historico.db` de rascunho com 7 mil ditados; e a nuvem de teste numa porta livre): conectar e
  aprovar, a pergunta com "6.533 ditados", o envio com "Enviando o histórico: 5.000 de 6.533." (o
  `429` do limite de 5.000 por minuto no meio, e a bandeja dizendo "Nuvem: enviando 5.000 de 6.533"),
  o fim ("O histórico subiu para a nuvem." e "Em dia com a nuvem. Conferido às 21:05."), a cópia ao
  lado do banco, o selo com os botões no PC, a frase da Privacidade, o desconectar, uma edição feita
  desconectado ("1 mudança feita aqui ainda não subiu e não vai mais para lá."), a pergunta do
  esquecer com o foco no Cancelar e o Esc devolvendo o foco, o esquecer (o campo vazio e focado, o
  aviso), a nuvem religada vazia e a pergunta de novo com "Estes ditados já foram para outra nuvem…" e
  uma segunda cópia. Em 375 px e no desktop, claro e escuro: nada passa da largura, os botões da
  pergunta com 44 px. **Achados e corrigidos:** a pergunta do esquecer encostava no campo; e a seção
  ficava em "Conferindo a nuvem…" até a pessoa trocar de aba (agora relê também nesse estado). No
  fim, os processos parados, a pasta de rascunho e o "PC" de mentira apagados.
- **Mutação.** 48 mutações numa cópia do repositório (`git archive`), cada uma conferida como aplicada (o
  trecho casava uma vez só e o arquivo mudou) e o arquivo restaurado byte a byte depois: o filtro do
  envio (a lixeira, o `device_id`, a `server_version`, a situação, a janela e o limite dela), a cópia
  (pulada, ou falhando e seguindo), a marca e a ordem do `inicial`, a completação sem o aceite, a guarda
  do `emitir` para o ditado de outro PC, o esquecer (a fila, o cursor, as versões, a URL, a referência,
  o token, conectado), o envio desconectado ou antes da primeira rodada, a reconciliação (sem ela,
  sempre, sem a volta a mais, o `MudouAqui`), a escolha do selo (sem conferir o selo ou o texto, a que
  cai sem mescla, o payload sem ela, o trocar sem trocar no PC, a fila sem a versão), a bandeja e o
  progresso, o pareamento que mudou na rodada, e o painel (o foco, o Esc, o reler com a seção fechada
  ou a aba escondida ou conferindo, o aviso do fim, a pergunta antes da primeira rodada, o selo sem os
  botões, a Privacidade, o espaço da pergunta). **As 48 morreram.** Cinco precisaram ser refeitas
  (não compilavam: variável sem uso) e morreram na segunda vez. A que pulava a cópia só morria pelo
  prazo de 10 min do `go test` (a medida da espera ficava parada esperando o gancho da cópia); o teste
  agora falha na hora, e a mutação morre em 4 testes.
- **O que a F7c NÃO provou:**
  - **o Postgres:** a escolha pelo push só foi provada na memória e pela suíte de contrato na
    memória (o contêiner continua desligado; os testes de Postgres da F6 continuam pendentes);
  - **o Agent de verdade, o Gerenciador de Credenciais e a bandeja no Windows:** só o roteiro
    (`docs/teste-manual-nuvem-f7.md`); sob `go test`, o envio e o esquecer param no portão do
    Gerenciador antes de tocar no banco (`TestEnvio_MontagemDoAgentNaoPassaDoPortao`);
  - o `-race` (sem compilador C); um disco lento (a espera do `Put` foi medida num SSD).
- **Riscos que ficam:**
  - **a cópia segura o `Put`** enquanto roda (de 139 a 165 ms com 7 mil ditados, uma vez por conta);
    num disco lento, mais;
  - **a escolha que cai** traz o selo de volta: nada se perde, mas a pessoa escolhe de novo;
  - **a referência do esquecer é o relógio do PC** (`updated_at` contra `nuvem_esquecida_em`). **Corrigido
    na revisão:** este item dizia que a mudança feita antes do esquecer e que não subiu "não volta à
    mesma conta"; o que acontecia era pior, o pull a desfazia no próprio PC. A revisão fez a referência
    recuar para antes dela, com um custo (o ditado recebido da nuvem depois da mudança mais antiga
    também contava como "mudou aqui", e podia virar selo à toa). **Refeito na reconferência:** a
    referência é o instante do esquecer, e a mudança de antes dele que não subiu fica anotada item por
    item (`sync_mudou_aqui`); o custo saiu. O que fica do relógio: a mudança feita depois do esquecer
    com o relógio do PC atrasado para antes dele não conta como "mudou aqui";
  - **o ditado de outro PC editado aqui depois do esquecer**, de volta à mesma conta, sobe com a base
    vazia e pode virar selo, se outro aparelho também o mudou;
  - **o ditado de outro aparelho editado aqui desconectado, antes do esquecer** (a sonda A6 da
    reconferência), de volta à mesma conta, vira selo **mesmo sem ninguém mais ter mexido**: a edição
    deste PC vai para o selo, e o texto do outro aparelho fica como o final. Nada se perde, e a pessoa
    escolhe no selo. É risco aceito: ver "A reconferência da F7c";
  - **a lixeira de um ditado do envio inicial com o lote em voo** (a sonda A2 da reconferência) vai
    para a lixeira da nuvem: risco aceito, ver "A reconferência da F7c";
  - **a cópia tem o segundo no nome:** dois envios no mesmo segundo (só depois de um esquecer, e em
    segundos) dariam "Não deu para copiar o histórico."; o clique seguinte resolve;
  - **o foco depois do Enviar o histórico** cai no corpo da página (o botão some com o envio); o
    progresso é anunciado pelo `role="status"`;
  - **a nuvem de teste religada** é um epoch novo: o PC reconectado sobe de novo, sozinho, o que já
    tinha subido (os ditados de exemplo da nuvem antiga também, como deste PC). É do servidor de teste
    (o roteiro avisa).
- **Para as fatias seguintes:**
  - **F8:** o aprendido na fila (outro `entity_type`) e no esquecer (a `sync_outbox` inteira já é
    apagada); a frase da Privacidade ganha o aprendido;
  - **F10:** decide quando somem as cópias `antes-da-nuvem` (e as `antes-do-dono`);
  - **F11:** o recomeço do epoch novo continua mandando o histórico com a base vazia (agora com a
    reconciliação do pull para a lixeira); os testes de Postgres pendentes, com o caso novo da suíte.

### A revisão da F7c (06-out-2026)

Uma revisão do mesmo dia, com sondas contra a nuvem de verdade e o "PC" de mentira, achou um problema
alto, quatro médios (dois deles o mesmo do alto, visto de outro lado, e um do roteiro) e seis baixos.
O que foi feito, problema por problema (commits `9321cb6` a `c20272c`, e os documentos, sobre
`0805ebd`):

- **Alto: esquecer e voltar à mesma conta desfazia a mudança local que não subiu** (e o médio do mesmo
  caminho, reproduzido no "PC" de mentira). A edição ou a lixeira feita com o PC desconectado tinha o
  marcador na fila; o esquecer apagava a fila e gravava `nuvem_esquecida_em` com o instante dele; na
  volta, a linha, mudada antes, contava como "o ditado velho", e o pull gravava o estado da nuvem por
  cima: o texto voltava ao antigo, o ditado saía da lixeira, sem selo e sem cópia (a cópia só vem com
  o clique, depois). A pergunta dizia que essas mudanças "não vão mais para lá", e o risco da F7c, que
  elas "não voltam à mesma conta"; o que acontecia era a perda no próprio PC. **Feito**
  (`referenciaDoEsquecer`, em `esquecer_sqlite.go`): a referência recua para 1 µs antes do
  `updated_at` mais antigo do que mudou aqui e a nuvem não recebeu: as linhas com marcador na fila
  (menos o do envio inicial num ditado que não mudou desde o clique, que é velho e recuaria a
  referência por meses) e, havendo um esquecer anterior, as sem `server_version` mudadas depois dele
  (o PC que esquece de novo antes da rodada que as subiria). O `updated_at` e não o `criado_em` do
  marcador, como a revisão sugeria: a lixeira grava o `updated_at` com o "agora" de quem a pediu, que
  pode ser de antes do marcador, e a linha ficaria de fora. Na volta, essas linhas sobem com a base
  vazia e a nuvem decide: a edição vale (o texto da nuvem era deste PC), a lixeira vale (apagar vence),
  e a edição que bate com outra do celular vira selo, com as duas. A frase da pergunta passou a "{n}
  mudanças feitas aqui ainda não subiram. Elas ficam neste PC.", e o risco acima foi reescrito. A
  sonda virou `TestSyncE2E_MudancaAntesDoEsquecerNaoSePerde` (falha contra o código de antes: os três
  ditados mudados perdem a mudança); os casos da referência, `TestEsquecer_ReferenciaRecuaParaAMudancaQueNaoSubiu`
  e `TestEsquecer_ReferenciaPorCaso`. **Não feito:** guardar os ids numa chave ou coluna (a
  alternativa da revisão): pediria mudar a 0006, e o recuo cobre o caso. **O custo:** o ditado que o
  PC recebeu da nuvem entre a mudança mais antiga que não subiu e o esquecer também conta como "mudou
  aqui" (em "Riscos que ficam"). **Também não coberto:** o PC que troca de conta e volta (A, B, A) com
  um ditado editado e confirmado só pela B: de volta à A, vale o texto da A para ele (o da B continua
  na B); e o recomeço de um epoch novo seguido de um esquecer antes de o reenvio sair (os marcadores
  do recomeço contam, e a referência recua até eles: selos à toa possíveis, nada perdido).
  **Refeito na reconferência:** o recuo deu lugar à anotação item por item (`sync_mudou_aqui`), e o
  custo saiu; os marcadores do recomeço continuam contando, mas só para os próprios ditados. Ver "A
  reconferência da F7c".
- **Médio: o histórico subia sem o clique, e a lixeira ia para a nuvem.** O `emitir` punha marcador
  em toda gravação com a nuvem conectada, inclusive num ditado sem `server_version`; a edição, a
  lixeira ou o restaurar de um ditado antigo o faziam subir antes do "Enviar o histórico" e sem a
  cópia (contra a D1), e o posto na lixeira subia para a lixeira da nuvem (contra a resposta 3); o
  mesmo para um ditado do envio inicial posto na lixeira antes de o lote dele sair. **Feito:** num
  ditado sem `server_version`, só o `Put` do ditado novo (sem campo, nascido aqui) marca; a edição, a
  lixeira e o restaurar ficam com o envio inicial e a completação, que respeitam a janela e a lixeira,
  e a edição não mexe nos campos de um marcador de criar que já está lá (o ditado sobe inteiro, lido
  na hora). A lixeira tira da fila o marcador de criar parado (sem campo, fora de voo) de um ditado
  sem `server_version`: o do ditado novo e o do envio inicial (este conta como feito no progresso). O
  em voo fica (a nuvem pode já ter o ditado; a resposta decide, e a lixeira sobe na volta, se ela o
  tem), e o da reconciliação também: ele passou a nascer com os dois campos, que não mudam o envio de
  criar, mas dizem que a nuvem tem o ditado. Testes: `TestEmissao_SemAVersaoDaNuvemSoODitadoNovo` e
  `TestSyncE2E_LixeiraDoHistoricoConectadoNaoSobe` (antes e durante o envio; falha contra o código de
  antes: três ditados do histórico subiam antes do clique); os testes da emissão da F7a que usavam um
  ditado sem versão para exercitar a edição, a lixeira e o restaurar passaram a usar um que a nuvem
  tem. **O que muda:** um ditado que a nuvem recusou ao criar (sem a versão da nuvem: o texto grande
  demais, a data no futuro), editado aqui, não é mais reenviado pela edição (antes, conectado, era);
  ele fica de fora até um esquecer. Se o Mauro quiser a edição como uma nova chance, é a edição voltar
  a situação do ditado para `pending` (a completação o leva).
- **Médio: a reconciliação não convergia quando só a lixeira diferia e a nuvem estava na lixeira.** A
  nuvem mantinha a lixeira (apagar vence) e respondia `applied` mesclado, com versão nova; o PC não
  guardava a versão, e o pull seguinte reconciliava de novo: a versão da nuvem subia a cada rodada
  (a sonda viu 3, 5, 7, 9, 11), o PC ficava fora da lixeira com "1 para enviar" para sempre, e os
  outros aparelhos baixavam o ditado a cada minuto. Acontecia no epoch novo (todo ditado "mudou aqui")
  e na volta à mesma conta depois de pôr e tirar da lixeira aqui, ou do "manter" do selo. **Feito:**
  reconcilia só quando o lado do PC pode vencer (`pcPodeVencer`: outro texto, ou na lixeira só aqui);
  na lixeira só na nuvem, com o mesmo texto, vale o estado da nuvem. A sonda virou
  `TestSyncE2E_ReconciliacaoSoDaLixeiraConverge` (os dois caminhos; a versão da nuvem fica parada em
  quatro rodadas).
- **Médio: o roteiro mandava conferir uma contagem que nenhuma tela mostra** ("N + os 7" no app; e,
  dos 7, um está na lixeira). **Feito:** o passo 7.4 confere por amostra (as primeiras linhas do app
  iguais às do PC, a Lixeira do app só com o de exemplo, uma frase da lixeira do PC que não aparece na
  busca do app e, se o histórico for pequeno, o mais antigo do app com menos de 6 meses), o 7.1 diz o
  que aparece quando a nuvem recusa algum ditado ("k ditados não subiram para a nuvem."), o 13.3 diz
  "6 no Histórico e 1 na Lixeira", e o "O que me contar" pede o que dá para ver.
- **Baixo: nenhum teste cobria o filtro repetido no `INSERT` do `marcar`** (a mutação que o trocava
  por uma tautologia sobrevivia). **Feito:** `TestMarcar_FiltroDeNovoNoInsert`, que muda quatro
  ditados entre a leitura dos elegíveis e o bloco (a lixeira, a versão da nuvem, a recusa, outro PC).
- **Baixo, o roteiro:** os avisos 3 e 5 apontavam para os passos 11 e 12 (são do 13); o 13.1 mandava
  clicar em Desconectar mesmo depois do passo 12, quando a seção já diz "Desligada" ("vá direto ao
  2"); o selo de exemplo pode ficar depois dos primeiros 50 itens ("busque por Confirmo a entrega,
  com Ctrl+K"). Todos feitos.
- **Baixo: o número dos que não subiram sumia com o aviso.** **Feito:** a linha de "em dia" diz também
  "{k} ditados não subiram para a nuvem." enquanto houver.
- **Baixo: "Estes ditados já foram para outra nuvem…" ficava inexata de volta à mesma conta** (o que
  sobra lá são os recusados, que nunca foram a lugar nenhum). **Feito:** "Alguns destes ditados podem
  já estar na nuvem que este PC esqueceu."

**O que mudou de comportamento:** conectado, a edição, a lixeira e o restaurar de um ditado que a
nuvem não tem não o põem mais na fila (o histórico só sobe pelo clique e, depois do aceite, pela
completação, na rodada seguinte, até 60 s depois); a lixeira de um ditado novo ou do envio inicial
antes de ele subir o tira da fila (ele nunca vai para a nuvem); o recusado editado não é reenviado; o
esquecer grava uma referência mais antiga, e a mudança que não subiu volta a subir de volta à mesma
conta, em vez de ser desfeita; o ditado que só a nuvem pôs na lixeira vai para a lixeira deste PC,
sem reconciliação; a linha da nuvem diz quantos não subiram; duas frases mudaram.

**Testes, mutação e telas.** `go test ./... -count=1`: 65 pacotes verdes; `go vet ./...` no Windows e
com `GOOS=linux`; `gofmt -l .` vazio. No `web/`: 678 testes, o lint e os dois builds numa pasta de
rascunho. **21 mutações, 21 mortas** (numa cópia por `git archive`, cada troca conferida antes: o
trecho casava uma vez só e o arquivo mudou; a cópia igual ao commit no fim): a reconciliação (a regra
antiga, sem a lixeira só aqui, sem o texto), o `emitir` (qualquer campo, sem a guarda do `device_id`,
sem o ramo do ditado que a nuvem tem), a lixeira que tira o marcador (não tira, tira o em voo, tira o
com campos, tira o de um ditado que a nuvem tem), o `MarcadorCriar` sem os campos, a referência do
esquecer (o agora, sem o esquecer anterior, o envio inicial editado depois do clique que não conta, o
envio inicial que conta sempre, sem o 1 µs, além do agora), o filtro do `marcar` e, no painel, a linha
sem os que não subiram e as duas frases antigas. Uma (a referência que é o agora) não compilava na
primeira forma (variável sem uso) e morreu na segunda; três ganharam teste antes da rodada (o `Put` de
outro PC, a lixeira depois da escolha do selo e a hora no futuro). As telas mudadas (a linha de "em
dia" com os que não subiram, a pergunta depois de um esquecer, a pergunta do esquecer) foram vistas no
Edge sem janela, em 375 px e 1.280 px, claro e escuro: nada passa da largura.

### A reconferência da F7c (06-out-2026)

Uma reconferência do mesmo dia, depois da revisão, achou um problema médio e três baixos (dois deles
as sondas A2 e A6). No mesmo dia, o Mauro confirmou as três decisões da F7c: D1 (o histórico só sobe
depois do clique em "Enviar o histórico", perguntado uma vez; os ditados novos sobem sempre), D2
(depois do esquecer, a conta nova recebe só os ditados nascidos neste PC) e D3 (a reconciliação no
pull, com a referência do esquecer, como está, com as correções abaixo). O que foi feito, problema
por problema (commits `ec2f3d1` a `28ebdd1`, e os documentos):

- **Médio: a referência do esquecer ficava presa numa linha que nunca sobe.** A revisão fez a
  referência recuar até o `updated_at` mais antigo do que mudou aqui e não subiu, contando, havendo
  um esquecer anterior, toda linha sem `server_version` mudada depois dele. Depois de um primeiro
  esquecer, a mudança num ditado que nunca sobe (de mais de 6 meses, de outro PC, na lixeira,
  recusado) ficava sem `server_version` para sempre, e um segundo esquecer, semanas depois, recuava
  até ela, mesmo com o PC conectado e reconciliado no meio: todo ditado recebido da nuvem nessas
  semanas passava a contar como "mudou aqui" e, mudado por outro aparelho com o PC esquecido, virava
  selo à toa na volta. **Feito, pelo segundo caminho da reconferência:** a referência voltou a ser o
  instante do esquecer, e o que mudou aqui antes dele sem a nuvem receber fica anotado **item por
  item**, numa tabela nova, `sync_mudou_aqui` (só `entity_type` e `entity_id`, nenhum texto; a 0006
  corrigida no lugar, antes de qualquer deploy: ela só cria a tabela, vazia, e nenhuma linha muda).
  Entram, no esquecer (`anotarMudouAqui`, na transação dele, antes de a fila sair): o ditado com
  marcador na fila (menos o do envio inicial que não mudou desde o clique), o que um esquecer
  anterior anotou e continua sem a versão de uma nuvem, e, havendo um esquecer anterior, o que mudou
  depois dele sem a versão da nuvem. Sai o que voltou a ter a versão de uma nuvem (foi reconciliado;
  se mudou de novo, o marcador o anota outra vez) e o que não existe mais. O anotado conta como
  "mudou aqui" no pull (`EstadoDoItem.MudouAqui`), e o recomeço do epoch novo o tira, junto com a
  referência. **Por que não o primeiro caminho** (restringir a segunda condição aos elegíveis do
  envio): o ditado de fora dos elegíveis também sobe, pela reconciliação, quando a nuvem o tem. O de
  outro PC editado aqui depois do esquecer, ou o posto na lixeira aqui, num PC que vai a outra conta
  e volta (A, B, A), ficaria sem proteção, e o pull desfaria a mudança; no caso do de outro PC, ela
  não existe em mais lugar nenhum. Item por item, anotar a mais custa pouco: o anotado que a nuvem
  não tem nunca chega pelo pull, e o que ela tem pode, no máximo, virar selo sozinho, sem perder
  nada. **E fecha o custo que a revisão tinha deixado** (o ditado recebido entre a mudança mais
  antiga e o esquecer contando como "mudou aqui"). Testes:
  `TestEsquecer_SegundoEsquecerNaoRecuaPeloQueNuncaSobe` (dois esquecer separados por 30 dias, com o
  PC reconciliado no meio e a mudança num ditado de 8 meses: a referência fica no instante do segundo,
  o ditado recebido e o reconciliado não contam, o velho e o de outro PC mudado aqui contam; um
  terceiro esquecer tira o que foi reconciliado depois); `TestEsquecer_AMudancaQueNaoSubiuContaComoMudouAqui`
  (o da revisão, com um ditado recebido depois da lixeira que não subiu, que agora não conta) e
  `TestEsquecer_AnotadoPorCaso` (o da revisão, caso a caso, pelo anotado); as pontas a pontas da
  volta à mesma conta continuam verdes, sem mudar.
- **Baixo: a lixeira de um ditado do envio inicial com o lote em voo vai para a lixeira da nuvem**
  (sonda A2). Com a nuvem fora do ar no push do lote, os marcadores ficam em voo; a lixeira não os
  tira (a nuvem pode já ter os ditados), e o reenvio, com o mesmo `mutation_id`, lê a linha na hora e
  leva a lixeira. **Decidido: aceito, e registrado.** Os dois caminhos terminam iguais: se o primeiro
  envio não chegou, a nuvem cria o ditado já na lixeira (na versão 1: ele nunca aparece no histórico
  do app, e some quando a lixeira da nuvem vence); se chegou (a resposta se perdeu), ela responde
  `duplicate`, e a lixeira sobe como mudança, na versão 2. O criar sem a lixeira (a alternativa) não
  evitaria isso: o ditado passaria pelo histórico do app, fora da lixeira, até a lixeira subir no lote
  seguinte; e não reenviar deixaria a nuvem com o ditado fora da lixeira quando o primeiro envio
  chegou e o pull já o tinha pulado (com marcador, o pull pula). O "nada da lixeira sobe" do envio
  inicial vale para o que estava na lixeira no clique e para o que vai para ela antes de o lote sair;
  o que vai para a lixeira com o lote já em voo termina na lixeira da nuvem. Teste:
  `TestSyncE2E_LixeiraDoEnvioInicialEmVoo` (os dois caminhos: o ditado na lixeira da nuvem, na versão
  1 e na 2, o resto do lote fora dela, a fila vazia); o porquê, no comentário do `mudarLixeira`.
- **Baixo: o ditado de outro aparelho editado aqui desconectado vira selo na volta à mesma conta**
  (sonda A6). O PC 1 tem um ditado do PC 2, desconecta, edita, esquece e volta à mesma conta: a
  edição sobe com a base vazia (o esquecer apagou a versão), e a nuvem, que não sabe que o PC 1
  conhecia o texto do PC 2, guarda os dois: o do PC 2 fica como o final, e a edição do PC 1 vai para
  o selo, sem ninguém mais ter mexido. **Aceito, e registrado** em "Riscos que ficam". Por quê: nada
  se perde (as duas versões ficam, e a pessoa escolhe no selo); o caso pede três coisas juntas (um
  ditado de outro aparelho, editado com o PC desconectado, e o esquecer antes de reconectar, de volta
  à mesma conta); e não há correção simples: a base que faltaria foi apagada de propósito no esquecer
  (a nuvem seguinte pode ser outra conta, ou outro epoch, onde aquela versão não significa nada), e
  guardá-la com a conta e o epoch para mandar na volta seria um contrato novo. Com o ditado nascido
  aqui não acontece: a nuvem sabe que o texto dela era deste PC, e a edição vale.
- **Baixo: a pergunta do esquecer contava o histórico do envio inicial como mudanças feitas aqui.**
  Ela usava os `pendentes`, todos os marcadores da fila: com dois do envio inicial, um ditado novo e
  uma edição, dizia "4 mudanças feitas aqui ainda não subiram", e só uma é mudança. **Feito:** o
  resumo ganhou `mudancas` (o marcador de um ditado que a nuvem tem: a edição, a lixeira, o restaurar
  e a escolha do selo; e o da reconciliação), e a pergunta usa ele; o "{n} para enviar." da linha
  conectada continua com os `pendentes`. O histórico e o ditado novo que não subiram ficam neste PC
  do mesmo jeito e sobem pelo envio da nuvem seguinte; a frase só não os chama de mudança. Testes:
  `TestEnvio_ResumoContaSoAsMudancas` (um marcador de cada tipo: 7 pendentes, 4 mudanças), o
  mapeamento no `nuvem` e na Local API, a ponta a ponta da mudança antes do esquecer (as 3 mudanças)
  e, no painel, "conta só as mudanças, e não o histórico nem o ditado novo que não subiram".

**O que mudou de comportamento:** de volta à mesma conta depois do esquecer, contam como "mudou
aqui" só os ditados que mudaram aqui sem a nuvem receber (os anotados) e o que mudou depois do
esquecer; o ditado recebido da nuvem antes do esquecer não conta mais, mesmo que tenha chegado depois
de uma mudança que não subiu, e um segundo esquecer não alarga nada (selos à toa a menos, nada a mais
perdido); o que um esquecer anotou continua anotado nos seguintes enquanto nenhuma nuvem o
reconciliar; a 0006 cria também a tabela `sync_mudou_aqui`, vazia; a pergunta do esquecer conta só
as mudanças, e o `GET /api/nuvem` traz `historico.mudancas`. A2 e A6: sem mudança (registrados).

**O que não foi feito:** o primeiro caminho sugerido para o médio (acima, o porquê); a A2 e a A6
ficaram como risco aceito; e continua aberta a pergunta da revisão (o ditado recusado pela nuvem,
editado aqui, ganha uma nova chance?).

**Testes e mutação.** `go test ./... -count=1`: 65 pacotes verdes; `go vet ./...` no Windows e com
`GOOS=linux`; `gofmt -l .` vazio. No `web/`: 679 testes, o lint e os dois builds numa pasta de
rascunho. **20 mutações, 20 mortas** (numa cópia por `git archive`, cada troca conferida antes: o
trecho casava uma vez só e o arquivo mudou; a cópia igual ao commit no fim; nenhuma morreu por
prazo): o anotado (sem o marcador, sem a mudança depois do esquecer anterior, o envio inicial sem
mudança anotado, o editado depois do clique de fora, o anotado que nunca sai, que sempre sai, o
reconciliado que fica, sem o dono), o aplicador que ignora o anotado, a referência recuada, o
recomeço que não tira o anotado, o criar do envio inicial sem a lixeira e a lixeira que tira o em voo
(a A2), as mudanças (sem a versão da nuvem, sem os campos, iguais aos pendentes, fora do resumo do
Agent) e, no painel, a frase com os pendentes, com zero mudanças e aparecendo pelos pendentes. Uma
(o anotado sem o dono) sobreviveu na primeira rodada e ganhou a linha de outro dono no
`TestEsquecer_SegundoEsquecerNaoRecuaPeloQueNuncaSobe` (commit `28ebdd1`); rodada de novo, morreu.
A tela não mudou de forma (a mesma frase, outro número): não foi vista no navegador, só pelos testes
do painel.

### Decisões da F8 (07-out-2026)

O que a F8 decidiu antes de escrever código: as quatro respostas do Mauro, o desenho, as subfatias,
as escolhas menores e os riscos aceitos. A F8 estava parada em dois pontos de parada (o escopo do
aprendido e a criação do termo; ver "O aprendido" e "Pontos de parada") e num conflito entre
documentos (a ADR-020 deixava para a F8 a sincronização do "Adicionar termo"). O Mauro respondeu as
quatro decisões em 07-out-2026, **todas com a recomendação**, e mandou seguir ("pode seguir"). O
desenho parte do plano e da crítica do mesmo dia; a crítica achou quatro problemas altos e outros
médios e baixos, e as correções já estão no desenho abaixo. Esta seção nasceu na F8.0 (só
documentos); o estado da F8a a F8d e os desvios dela estão no fim ("O estado da F8a a F8d"), o
da F8e logo depois ("O estado da F8e"), o da F8f3 depois dele ("O estado da F8f3"), o da F8f4
depois dele ("O estado da F8f4"), a revisão da F8e a F8f4 depois dele ("A revisão da F8e a
F8f4"), o fechamento dela depois dele ("O fechamento da F8e a F8f4", com os desvios aceitos para
seguir à F8g), o estado da F8g ("O estado da F8g"), com a revisão dele ("A revisão da F8g") e o
fechamento ("O fechamento da F8g"), e, no fim, a F8h ("A F8h", "A revisão da F8h") e a F8i ("A
F8i").

- **D1. Só o Aprender. Confirmada pelo Mauro em 07-out-2026.** Sobem só as trocas do botão
  Aprender: a variante com `alias_origem` `"painel"` no `dicionario.json`. Ficam em cada PC: o que
  entrou pelo "Adicionar termo" (ADR-020: o termo, as grafias `"adicionado"` e a dica), o que a
  pessoa pôs à mão e o que o `dictlearn` pôs. Resolve o "Fica para a F8" de "O aprendido" e o que a
  ADR-020 deixou para a F8 (a seção "Sincronização" dela ganhou uma nota). Por quê: é o que a
  decisão 7 e a linha 7 do `CLAUDE.md` já dizem (o `CLAUDE.md` não muda); a tabela da nuvem (0016)
  não tem onde guardar a origem nem a dica; e as decisões 3 e 4 da ADR-020 (a dica) seguem abertas. Um
  termo do painel adotado pelo "Adicionar termo" continua sincronizando as variantes do painel dele;
  as grafias dele, não. **As alternativas:** subir também as grafias do "Adicionar termo" (a 0016
  ganharia a origem), ou tudo dele (uma segunda entidade, `{termo, dica}`). Podem entrar depois sem
  refazer esta.
- **D2. Cria a palavra. Confirmada pelo Mauro em 07-out-2026.** A troca chega sozinha aos outros
  PCs, sem clique lá, e passa pelas mesmas checagens do Aprender. Se o PC não tem a palavra certa,
  ele a cria como o Aprender faz: prioridade 50, origem `"painel"`, `AliasOnly` (só age pela troca e
  não vai ao prompt), sem domínio. A troca presa a um domínio fica "não aplicado neste PC" quando o
  PC não conhece o domínio, e também quando não tem a palavra: criá-la sem o domínio alargaria a
  troca, e criá-la com ele é a alternativa que não foi escolhida (esta segunda parte é uma leitura do
  texto da D2, e fica a confirmar: escolha menor 6, abaixo). Por quê: o caso do aceite ("n oito
  n" vira "n8n") é uma palavra nova, e sem criar ele nunca chegaria ao outro PC; o Aprender só cria
  palavra sem domínio, então a troca com domínio vem de uma palavra que já existia na origem; e assim
  a troca nunca passa a valer em mais lugares do que na origem. **Isto confirma a aplicação
  automática no PC que recebe:** o REQ-020 e o AC-022 ("aprendizado exige confirmação") passam a
  ler que a confirmação é o clique no PC de origem (emendados no `02` e no `03`; o THR-018, no
  `09`). **As alternativas:** criar a palavra com o domínio e a restrição da troca quando o domínio
  existe no PC, ou nunca criar.
- **D3. Com o "Enviar o histórico". Confirmada pelo Mauro em 07-out-2026.** Nada aprendido sobe
  antes do clique em "Enviar o histórico", nem o antigo nem o novo. A pergunta passa a dizer "Vão
  subir N ditados e M trocas aprendidas" (a frase final é da F8h) e aparece também quando só há
  trocas (N = 0). No PC que já clicou (o `bin\` do Mauro), as trocas sobem quando ele for atualizado,
  sem pergunta. Depois do "Esquecer esta nuvem", a conta nova recebe só as trocas aprendidas neste
  PC, como a D2 da F7c faz com os ditados. O que vem da nuvem entra no PC antes do clique: o clique
  é só para subir. Por quê: segue a D1 e a D2 da F7c, sem botão novo; a troca é um trecho do que a
  pessoa ditou, Confidencial no `09`. **A alternativa:** subir sozinho ao conectar, com a frase da
  Privacidade avisando.
- **D4. Vale o primeiro na nuvem. Confirmada pelo Mauro em 07-out-2026.** No desacordo, vale o que
  chegou primeiro à nuvem, e o desfazer vence. Os dois casos: um aparelho desfaz enquanto outro,
  desconectado, aprende a troca de novo; e a mesma variante aprendida para termos diferentes em dois
  PCs. O PC que perdeu segue a nuvem e mostra uma etiqueta cinza discreta na lista "Aprendido no
  painel". Para mudar, a pessoa aprende de novo. Por quê: é a regra do ditado (apagar vence) e a
  frase de "O aprendido" ("desfazer em outro aparelho remove o par"); a outra saída guardaria uma
  exceção por troca, e a troca iria e voltaria entre os aparelhos. **A alternativa:** o PC que perdeu
  mantém a dele, com a etiqueta, sem subir.

**A entidade e o id.**
- Cada variante de origem `"painel"` é uma entidade `learned`. A identidade é a **variante**, e não o
  par: o mesmo id em todo PC, e trocar o termo é mudar a mesma entidade.
- O id é um UUID v5 (SHA-1, da biblioteca padrão), com um nome versionado, da chave
  `CollapseSpaces(Normalize(variante))`: a mesma comparação do dicionário. Não depende do `usuario`
  do arquivo (o `idDeterministico`, em `internal/dictionary/seed.go:266`, depende e não é UUID).
  Vetores congelados nos testes (caixa, acento, "DCTF Web" igual a "DCTFWeb"): mudar o `Normalize`
  depois duplicaria pares na nuvem.
- O servidor usa a mesma função (`internal/dictionary`): recalcula o id e o `normalized_variant` (a
  mesma chave), e nunca os recebe prontos.

**O payload**, com os nomes das colunas da 0016, em inglês. Nenhum nome coincide, com outro tipo,
com um campo do `Item` do ditado (`internal/sync/fio.go`):
- push `upsert` (aprender, trocar o termo ou a restrição, reaprender), lista fechada:
  `{ "variant": "sf", "term": "ECF", "restricted": false, "domain": null }`;
- push `delete` (o desfazer): payload vazio;
- pull, e o `state` do `conflict`: `{ "id": "uuid", "variant": "sf", "term": "ECF", "restricted":
  false, "domain": null, "undone_at": null }`. O par desfeito chega como `upsert` com `undone_at`: o
  aprendido não tem lápide;
- `domain` é o **nome** do domínio, nunca o mapa `dominios` (que programas a pessoa usa). Não sobem a
  prioridade, a dica, o idioma, o `usuario` nem a origem;
- um PC da F7 decodifica a mudança `learned` no `Item` sem erro (os nomes não colidem), a conta como
  "não aplicado" e anda o cursor. A suíte de contrato confere isso.

**As regras da nuvem** (`cloudstore.DecideLearned`, pura, a mesma na memória e no Postgres). Antes
do banco vêm as pré-checagens; a recusa não grava nada nem gasta `seq`:
- `operation` `delete` só vale para o `learned` (no ditado continua `operacao_invalida`);
- no `upsert`, campo fora da lista é `campo_desconhecido`. É `campo_invalido`: `variant`, `term` ou
  `domain` com o caractere nulo ou que não é UTF-8 válido (o que o `cleanString` de
  `internal/server/cloudstore/payload.go` já recusa, porque o Postgres não os guarda: aqui com a
  medida em runas, e não em bytes); `variant` ou `term` fora de 1 a 80 runas (runas, como a coluna);
  `Normalize(variant)` vazio; `domain` que não é nulo nem tem de 1 a 80 runas; `restricted` sem
  `domain`; e `entity_id` diferente de `ID(variant)`. Assim nenhum push alcança o `23505` (a `UNIQUE
  (user_id, normalized_variant)`), o `23514` (os `CHECK`s da 0016) nem o `22021` do byte nulo, cujo
  `DETAIL` poria a variante no log do Postgres (ou daria `500` e reenvio sem fim). As mesmas
  checagens são uma função só de `internal/dictionary`, que o PC usa na projeção (abaixo): o que a
  nuvem recusaria nem sobe;
- no `delete`, qualquer campo é `campo_desconhecido`.

| A linha na nuvem | A mutação | Resposta | Grava (versão, `seq`, evento) |
|---|---|---|---|
| não existe | `upsert`, base 0 | `applied`, versão 1 | insere |
| não existe | `upsert`, base > 0 | `rejected` `base_invalida` (o PC zera a base e manda com 0) | nada |
| existe | `upsert`, base > versão | `rejected` `base_invalida` | nada |
| existe, ativa, com o mesmo termo, restrição e domínio | `upsert`, base ≤ versão | `applied` com a versão atual | nada, nem `seq` |
| existe, com outro conteúdo ou desfeita | `upsert`, base = versão | `applied`, versão + 1 (troca o termo ou a restrição; ou reaprende e limpa `undone_at`) | sim |
| existe, com outro conteúdo ou desfeita | `upsert`, base < versão (0 inclusive) | `conflict`, com a versão e o `state` (vale o primeiro; o desfazer vence) | nada |
| existe, ativa | `delete`, qualquer base | `applied`, versão + 1, com `undone_at` | sim |
| existe, desfeita | `delete`, qualquer base | `applied` com a versão atual | nada |
| não existe | `delete`, qualquer base | `rejected` `nada_a_desfazer` (o PC tira o marcador) | nada |

- As linhas valem na ordem: a primeira que casa decide.
- O `applied` do aprendido **nunca** é `mesclado`: o PC sempre guarda a versão que volta.
- O desfazer é idempotente e não olha a base: nunca é `base_invalida`. Sem linha, a resposta é o
  código próprio `nada_a_desfazer` (o status `rejected` e o nome são da F8b), que o PC trata como "o
  marcador sai". Sem isso, o desfazer de um par que nunca subiu voltaria `base_invalida` em toda
  rodada, para sempre.
- O evento de toda mudança do aprendido, o desfazer inclusive, é `upsert` (o `CHECK` da 0008 não
  aceita `delete`; a linha desfeita continua viva, com `undone_at`).
- O `duplicate` confere também a operação: o mesmo `mutation_id` com outra operação é
  `mutacao_reusada`. No PC, o envio em voo repete a mesma operação (ela fica com o `mutation_id`,
  gravada antes da rede); a mudança feita durante o voo sobe depois, com outro id e a base que a
  resposta trouxe. O teste: um desfazer sem resposta seguido de um reaprender termina reaprendido na
  nuvem, sem etiqueta nenhuma.
- **De onde o `duplicate` tira a operação: a 0009 ganha a coluna `operation`, corrigida no lugar.**
  Hoje a `applied_mutations` guarda o tipo, a entidade e o aparelho, mas não a operação, e o
  `cloudstore.Applied` também não a tem: sem coluna, o Postgres não tem como comparar. A regra de
  corrigir no lugar o que nunca rodou (decisão 13; confirmada pelo Mauro em 05-out-2026 até a
  primeira aplicação fora de um banco de teste descartável, o deploy da F11) vale para a 0009, e a
  convenção "um arquivo por tabela" (`migrations/README.md`) não deixa um `ALTER` numa 0017. A
  coluna: `operation TEXT CHECK (operation IN ('upsert', 'delete'))`, nula só quando a operação
  mandada não é da lista (a recusa `operacao_invalida`, gravada como hoje). O `Applied` ganha o campo
  `Operation`, o `Duplicate` compara a operação (a de fora da lista conta como vazia) junto com o
  tipo, a entidade e o aparelho, e o `schema_test` ganha a linha da coluna. Um banco de teste que
  aplicou a 0009 de antes recusa o `migrate` por checksum: a saída é apagá-lo (`migrations/README.md`).
- **A 0016 não muda.** Sem mescla, não precisa de `undone_version` nem de colunas por campo.

**O pull no mesmo snapshot.** Uma terceira consulta, a da `learned_pairs`, na mesma transação
`REPEATABLE READ` dos ditados e das lápides (`internal/server/pgstore/read.go`), com o aparelho de
origem pelo `sync_events` do `last_seq`; as três listas se fundem por `seq`. O `Change.Payload` e o
`state` do `conflict` passam a ser por `entity_type`, nos dois lados: `storeChangeDTO` e
`storeResultDTO` no servidor (`internal/server/store_routes.go`); `Change` e `MutationResult.State`
no PC (`internal/sync/fio.go`).

**As rotas do celular**, só com sessão (o token do PC recebe `403`), no `06`:
- `GET /api/v1/learned`: só os não desfeitos, com o item do `06` (`id`, `termo`, `variante`,
  `restrito`, `dominio`), do mais recente para o mais antigo (`last_seq`), até um teto fixo, com
  `"has_more": true` quando passa dele (o envelope `items` das listas do servidor);
- `POST /api/v1/learned/undo` `{ "id": "uuid" }`: pelo ponto único de gravação, com `sync_event`;
  `200` com o item; o já desfeito dá `200` sem gravar nem gastar `seq` (como a lixeira); o de outra
  conta, o que não existe e o id que não é UUID dão `404`, com uma frase própria (não a do ditado). O
  texto nunca vai na URL.

**No PC, a projeção.** O que sobe sai do arquivo, lido sem mudar o JSON (`Semente.ParesDoPainel`,
F8a): por variante de origem `"painel"`, o id, a variante como está escrita, o termo da entrada, a
restrição efetiva (a entrada `restrito`, ou a variante em `alias_restrito`) e o nome do domínio. O
que nunca age não sincroniza:

| No `dicionario.json` (variante de origem `"painel"`) | `restricted` | `domain` | Sobe? |
|---|---|---|---|
| em `alias`, numa entrada sem `dominio` | `false` | nulo | sim |
| em `alias`, numa entrada com `dominio`, sem `restrito` | `false` | o nome | sim |
| em `alias`, numa entrada com `dominio` e `restrito` | `true` | o nome | sim |
| em `alias_restrito`, numa entrada com `dominio` | `true` | o nome | sim |
| em `alias`, numa entrada `restrito` sem `dominio` | — | — | não: nunca age (o Search não mostra a entrada) |
| em `alias_restrito`, numa entrada sem `dominio` | — | — | não: nunca age |
| a mesma variante, com a origem, em duas entradas | — | — | nenhuma das duas |
| o que a nuvem recusaria (as pré-checagens acima: `Normalize` da variante vazio, posta à mão com a origem; variante, termo ou domínio com mais de 80 runas ou com o caractere nulo) | — | — | não: a mesma função da nuvem |
| sem a origem `"painel"` (à mão, `dictlearn`, `"adicionado"`) | — | — | não (D1) |

**No PC, o espelho** (a migration 0007 do SQLite, aditiva). Uma tabela nova (proposta:
`sync_aprendido`), e não as `local_dictionary_*` da 0001, que não têm a restrição e prendem as
linhas por chave estrangeira em cascata. Por id e pelo dono, e nada no `dicionario.json`:
- a **base**: o par que a nuvem confirmou, a `server_version` dele e a projeção que o arquivo tinha
  quando a base foi gravada (a "projeção local guardada");
- o **par da nuvem que ainda não vale aqui** (não aplicado, pulado, arquivo indisponível), com a
  versão e o motivo;
- a **intenção** do aplicador: o par e a projeção esperada, com a situação "aplicando";
- a **situação** (aplicado, aplicando, não aplicado, desfeito aqui, desfeito em outro aparelho,
  trocado em outro aparelho, já vale aqui, recusado) e o **motivo**, de listas fechadas; no
  "recusado", o motivo é o código da recusa (de `rejected`, fora os tratados abaixo) e a linha
  guarda a **projeção recusada**, para a varredura não mandá-la de novo;
- a **versão local**, que sobe a cada mudança daqui (o "mudou durante o voo" do ditado). Sobe na
  varredura, quando ela vê a mudança, e **no ato** quando a mudança é um Aprender ou um Desfazer do
  painel: o mesmo caminho que chama o gancho `AoMudar` grava, para o id que já tem linha no espelho,
  que ele mudou aqui, com o laço parado ou o PC desconectado. Assim um Desfazer seguido de um
  Aprender do mesmo par, que deixa o arquivo igual ao que era, continua sendo mudança daqui;
- e, à parte, o conjunto **"veio da nuvem"**: só ids, sem texto, que o "Esquecer esta nuvem" não
  apaga (no molde da `sync_mudou_aqui`; a F8e decide se é uma tabela ou uma coluna que o esquecer
  não toca). Entra o id que o aplicador escreveu no arquivo; sai quando a pessoa aprende o mesmo par
  aqui (a varredura vê a mudança como daqui).

A fila é a mesma `sync_outbox`: um marcador `learned` por id, sem texto, sem bits de campo, nunca
`inicial`. A operação sai do espelho na hora do envio: presente aqui, `upsert`; saiu daqui, `delete`.

**O texto no espelho** (a retenção dele; a regra completa no `09`):
- a linha que vale no arquivo (aplicado, já vale aqui) guarda a projeção, que é o que o
  `dicionario.json` já tem;
- a linha desfeita, depois de a nuvem confirmar o desfazer, fica só com o id (na linha com a
  etiqueta "desfeito em outro aparelho", quando a etiqueta some), para um epoch novo mandar o
  desfazer de novo. O mesmo vale para o par que chega desfeito do pull e não tem etiqueta;
- o "desfeito aqui" que o esquecer guarda fica só com o id: o `delete` não leva texto;
- o par da nuvem que não vale aqui (não aplicado, pendente, arquivo indisponível) e o texto das
  etiquetas guardam o texto enquanto a conta é a mesma; o esquecer o apaga (fica o id), porque a
  nuvem que o mandou foi esquecida e nunca mais o limparia;
- a projeção recusada fica até o par mudar, um epoch novo ou o esquecer.

**No PC, a varredura** (`arquivo × espelho`), no começo de cada rodada: depois do `Preparar` e
**antes do `CompletarEnvio`** (que precisa do M dela, abaixo) e do envio. É o detector único das
mudanças daqui: não há vigia do arquivo, e o `dictlearn` roda em outro processo. O Aprender e o
Desfazer do painel acordam o laço (o gancho `AoMudar`) e sobem a versão local no ato (acima), e a
troca sobe em segundos.

| O arquivo diante do espelho | O que a varredura faz |
|---|---|
| ausente, inválido, de outro usuário ou preso na troca de nome | nada: é "sem informação", nunca "tudo desfeito" |
| igual à projeção guardada, versão local igual à da base, nada pendente | nada, e não escreve no SQLite |
| linha "aplicando" e o arquivo igual à projeção esperada | confirma, sem envio (o id continua no "veio da nuvem") |
| linha "aplicando", o arquivo igual à projeção guardada | o arquivo não chegou a ser gravado: a intenção sai, o par volta a pendente, sem envio |
| linha "aplicando", o arquivo diferente das duas | a intenção sai, e o par é tratado como mudado aqui (a linha de baixo) |
| linha "recusado" e a projeção igual à recusada | nada: só uma mudança do par, um epoch novo ou o esquecer liberam a linha |
| par novo, ou mudado aqui (a projeção não é a guardada, ou a versão local passou da base) | marcador (`upsert`) |
| par presente que a nuvem não tem (sem `server_version`), fora do "veio da nuvem" | marcador (`upsert`): o primeiro envio, e o de depois de um esquecer |
| par que saiu do arquivo (a variante não está mais no termo) | marcador (`delete`), com o freio; sem versão da nuvem (nem a `server_version` da base, nem a pendente do pull) e sem envio em voo, o marcador nem nasce |
| a variante perdeu só a origem `"painel"` e continua no termo | **adotada**: não é desfazer, nada sobe, e a linha passa a "já vale aqui" |
| par pendente aqui (não aplicado, ou pulado que voltou ao que era) e o arquivo mudou ou voltou | tenta aplicar de novo |

- As linhas valem na ordem: a primeira que casa decide.
- **Antes do aceite** (`nuvem_envio_aceito_em`, a D3), ela segura o que o clique solta: o `upsert`
  de qualquer par (o do par que a nuvem não tem, e também o reaprender ou a troca de termo de um par
  que ela tem). Não põe esses marcadores; conta-os, e esse é o M da prévia. Sai antes do clique só
  o `delete` de um id que a nuvem já tem (com uma versão da nuvem, a da base ou a pendente, que antes
  do aceite só o pull dá): não leva texto, e sem ele o Desfazer num PC que ainda não clicou não
  chegaria ao celular nem aos outros PCs, e o PC e a nuvem divergiriam em silêncio (escolha menor 5,
  abaixo). O aprendido não é "inicial" nem conta no progresso do envio.
- **O aceite automático do `CompletarEnvio` passa a exigir M = 0.** Hoje, sem o aceite, ele o grava
  sozinho quando não há ditado elegível ("histórico sem nada a enviar está feito, sem cópia":
  `internal/storage/envio_sqlite.go`). Com a F8, ele o grava só com N = 0 **e** M = 0. Com M > 0, a
  rodada espera o clique, e a pergunta aparece com N = 0 ("Vão subir 0 ditados e M trocas
  aprendidas"; a frase final é da F8h). O clique com N = 0 grava o aceite sem cópia (a cópia é do
  histórico, e não há ditado a enviar). Sem isso, num PC com o histórico vazio ou inelegível, a
  primeira rodada gravaria o aceite e as trocas subiriam sem pergunta, contra a D3.
- Os marcadores e a projeção nova são gravados numa transação.
- **As etiquetas** ("desfeito em outro aparelho", "trocado em outro aparelho") somem quando a pessoa
  aprende o par de novo (a varredura vê a mudança daqui) ou quando passam de 30 dias (a varredura
  confere pela hora em que a situação começou); o texto sai junto, e fica o id.
- **O freio:** quando uma varredura acha mais pares saindo do arquivo do que um limite, nada sobe, e
  o painel avisa (o número e a frase são escolha menor, abaixo).

**No PC, o aplicador** (o que chega pelo pull). Vai para um pacote importável (proposta:
`internal/agent/aprendiz`): o `Validar`, o `agiria`, o `dominiosDaConferencia`, a palavra comum e o
`smartDaSemente` saem do `package main` (`cmd/agent/aprendizado.go`), para a ponta a ponta com dois
PCs usar o aplicador de verdade.
- **As regras do PC que recebe:** as integridades do Aprender (`AprenderVariante`); o "agiria",
  conferido em cada domínio onde o par vai agir, com a restrição efetiva dele (o
  `dominiosDaConferencia` passa a recebê-la: o alias restrito confere só no domínio da entrada; hoje
  esse caminho sempre daria "não agiria"); o `learning.GrafiaArriscada`, como defesa; o termo
  desligado; e **nunca alargar**: a troca nunca passa a valer em mais lugares do que na origem. Onde
  ela age na origem: sem domínio, em todo lugar; com o domínio X sem restrição, no domínio
  desconhecido e em X; restrita a X, só em X.

| O par (onde age na origem) | O termo neste PC | O que o PC grava | Situação |
|---|---|---|---|
| X | qualquer, e X não está no mapa `dominios` deste PC | nada | não aplicado (o domínio não existe aqui) |
| qualquer | existe, `restrito` sem `dominio` | nada | não aplicado: o termo daqui nunca age (o Search só mostra a entrada restrita no domínio dela, e ela não tem), e a variante dele também não agiria |
| sem domínio | não existe | o termo novo, como o Aprender (D2), e a variante em `alias` | aplicado |
| sem domínio | existe | a variante em `alias` (age onde o termo daqui age: no máximo o mesmo) | aplicado |
| X, sem restrição | com o domínio X (restrito ou não) | a variante em `alias` | aplicado |
| X, restrita | com o domínio X, restrito | a variante em `alias` | aplicado |
| X, restrita | com o domínio X, sem restrição | a variante em `alias_restrito` | aplicado |
| X | não existe | nada | não aplicado (D2: a palavra só é criada sem domínio; escolha menor 6) |
| X | sem domínio, ou com outro domínio | nada | não aplicado: agiria em outro lugar, ou em mais lugares |

- As linhas valem na ordem: a primeira que casa decide. A conferência do mapa `dominios` vem
  primeiro, porque o `agiria` de hoje (`cmd/agent/aprendizado.go`, `ageEm` e
  `dominiosDaConferencia`) não olha o mapa: um termo local com o domínio X, sem X no mapa, passaria
  na linha "com o domínio X" e gravaria uma troca presa a um domínio que este PC não conhece. É a
  leitura da D2 do Mauro ("um domínio que aquele PC não conhece"). O termo `restrito` sem `dominio` vem em
  seguida: o `dominiosDaConferencia` o confere só no desconhecido, onde ele não aparece, e o motivo
  é "não agiria". Depois da tabela vêm as outras checagens (as integridades, o `agiria`, a palavra
  comum, o termo desligado), só nas linhas que gravam.

- **Os casos de borda.** O mesmo par já vale aqui, de origem `"painel"` e com o mesmo termo
  (comparado como o Search compara): convergente, nada é gravado. O mesmo par posto à mão, sem a
  origem: "já vale aqui" (convergente, sem adotar), e um desfazer que chega da nuvem não o tira. A
  variante é de outro termo aqui, sem a origem: não aplicado. O par que vale aqui e a nuvem muda para
  algo que este PC não consegue honrar (outro termo, ou mais restrito): o par sai do arquivo (um
  desfazer local, sem envio) e a linha fica "não aplicado neste PC", com o motivo; a restrição nunca
  se perde.
- **Os motivos** são de uma lista fechada (os nomes são da F8a): o domínio não existe aqui; a
  restrição não cabe aqui; a palavra não existe aqui e a troca é presa a um domínio; a variante já é
  de outro termo; o termo está desligado; não agiria; palavra comum; o arquivo está indisponível.
- **O lote:** uma leitura do arquivo, o `Validar`, a conferência **par a par sobre a semente
  acumulada** (o veto de um par pode fazer outro parar de agir, como em "pedro das"), uma gravação e
  uma troca do dicionário e do prompt (o `gravarETrocar` do painel). O par recebido entra com a
  origem `"painel"`, para o Desfazer valer nele também.
- **A ordem**, porque o arquivo e o SQLite não têm transação comum:
  1. uma leitura curta confere o epoch e o pareamento, com as mesmas regras do `Fila.Aplicar`
     (`ErrEpochDiferente`, `ErrPareamentoMudou`). Mudou (um backup restaurado, ou o esquecer no meio
     da rodada): o arquivo não é tocado;
  2. sob a trava do dicionário (`a.mu`, a mesma do painel), o aplicador relê o arquivo e compara a
     projeção de cada id com a guardada no espelho. Diferente (o painel, a mão ou o `dictlearn`
     mudaram o par, e a mudança não subiu), com a versão local acima da da base (o Desfazer e o
     Aprender do painel que deixaram o arquivo como era), ou com marcador na fila, o par é **pulado**: nem o
     arquivo nem a base mudam, o par fica guardado como pendente, a mudança daqui sobe pela varredura
     e a nuvem decide (D4). O aplicador nunca sobrescreve uma mudança daqui que não subiu;
  3. **a intenção antes do arquivo** (write-ahead): o espelho grava o par, a projeção esperada (já
     com a adaptação deste PC: a grafia do termo daqui, o `alias_restrito`) e a situação "aplicando",
     e põe o id no "veio da nuvem" quando vai escrever o par. Só depois o arquivo é gravado;
  4. fora da trava, numa transação: o espelho (aplicado, não aplicado, pulado), os ditados da página
     e o cursor, com a conferência do epoch e do pareamento de sempre. Se eles mudaram entre o
     arquivo e esta transação, a linha fica "aplicando" (aplicado sem confirmação), e a varredura
     seguinte a reconcilia pelo arquivo: **nunca vira envio**. Quando o que mudou foi o esquecer, a
     linha "aplicando" e o id no "veio da nuvem" sobrevivem a ele ("O esquecer e o recomeço").
- Uma queda entre o arquivo e o espelho converge do mesmo jeito: a varredura acha o arquivo igual à
  projeção esperada e só confirma. **A adaptação deste PC nunca volta como `upsert`.**
- **O arquivo indisponível** (ausente, inválido, de outro usuário, preso na troca de nome) nunca
  segura o cursor dos ditados: o par fica pendente aqui, com o motivo, e a varredura o aplica quando
  o arquivo volta ou muda (a F8f3 decide como reconhece a mudança). O mesmo vale para o "não
  aplicado": o par fica no espelho e entra quando a causa some (por exemplo, o domínio criado aqui).

**O que o PC faz com cada par do pull** (as decisões puras da F8f2):

| O que chega | Neste PC | Decisão |
|---|---|---|
| `entity_version` igual à `server_version` da base | — | eco: nada |
| qualquer | o par mudou aqui e não subiu (projeção diferente da guardada, ou a versão local passou da base), ou tem marcador | pulado (acima) |
| par ativo | já vale aqui, igual | convergente: só a base anda |
| par ativo | ausente aqui | aplica pelas regras acima, ou "não aplicado" com o motivo |
| par ativo com outro termo ou outra restrição | o par de antes vale aqui, de origem `"painel"`, sem mudança daqui | troca (desfaz e aplica, sem envio); se o novo não vale aqui, o de antes sai e a linha fica "não aplicado" |
| par desfeito (`undone_at`) | vale aqui, de origem `"painel"`, sem mudança daqui | desfaz aqui, sem envio e sem etiqueta |
| par desfeito | posto à mão, ou ausente | nada: o desfazer remoto nunca tira o que a pessoa pôs |
| qualquer | arquivo indisponível | pendente aqui, e o cursor anda |

**O que o PC faz com a resposta do push** (F8f2):
- `applied`: guarda a versão (nunca há mescla); sem mudança durante o voo, o marcador sai;
- `conflict`: o par da nuvem (o `state`) passa a valer aqui, pelas regras acima (D4); com mudança
  daqui durante o voo, o marcador fica com a base velha, e o envio seguinte conflita de novo, como no
  ditado. Quando o `state` é o par desfeito, a linha mostra "desfeito em outro aparelho"; quando é
  outro termo, "trocado em outro aparelho" (a segunda etiqueta é a escolha menor 7; as frases
  finais são da F8h). **Só aqui:** a etiqueta aparece quando uma mudança deste PC perde na nuvem, e
  não a cada desfazer que chega pelo pull. Ela some quando a pessoa aprende de novo, ou depois de 30
  dias (escolha menor 2). O Desfazer seguido do Aprender do mesmo par, que deixa o arquivo como era,
  também é uma mudança deste PC (a versão local sobe no ato) e também ganha a etiqueta;
- `rejected` `base_invalida` (só no `upsert`): zera a base e manda de novo com 0;
- `rejected` `nada_a_desfazer`: o marcador sai, e o desfazer fica confirmado;
- `rejected` `mutacao_reusada`: manda de novo com outro id;
- os outros `rejected` (`campo_invalido`, `campo_desconhecido`, o `tipo_ainda_nao_sincroniza` de
  uma nuvem anterior à F8, e qualquer código novo): a linha fica "recusado", com o código, e guarda
  a projeção recusada; o marcador sai. Como o marcador do aprendido nasce da diferença entre o
  arquivo e o espelho, e não do evento de gravação como no ditado, tirar o marcador não basta: sem a
  projeção recusada, a varredura seguinte o recriaria, e o par iria de novo a cada rodada (60 s) e a
  cada `AoMudar`, para sempre. Com ela, uma mudança gera uma tentativa só;
- o desfazer de um par que a nuvem nunca teve (sem versão da nuvem, nem a da base nem a pendente, e
  sem envio em voo) nem sai: o marcador some sem envio.

**O esquecer e o recomeço.**
- **"Esquecer esta nuvem":** a fila sai inteira, como hoje. No espelho, saem a `server_version` e a
  situação, **menos na linha "aplicando"**: ela fica com a projeção esperada e a situação até a
  varredura seguinte a reconciliar pelo arquivo (a tabela da varredura: igual à esperada, confirma
  sem envio). O id dela já está no "veio da nuvem" (o passo 3 o põe antes do arquivo) e continua lá.
  Sem isso, o esquecer que cai entre o arquivo e a transação final (ou depois de uma queda, antes da
  varredura) apagaria a marca da reconciliação, a varredura leria o par da conta esquecida como
  mudado aqui, o tiraria do "veio da nuvem", e ele subiria para a conta nova. Fica o "desfeito aqui"
  (no espelho, ou anotado na `sync_mudou_aqui` com `entity_type` `learned`: a F8e escolhe), só com o
  id, e fica o "veio da nuvem". **O texto que veio da conta esquecida e não está no arquivo sai**
  (fica o id): o do não aplicado, do pendente, do arquivo indisponível e o das etiquetas. A pergunta
  do esquecer passa a contar também o aprendido pendente: o `mudancas` dela inclui os marcadores
  `learned`.
- **De volta à mesma conta:** o pull do cursor 0 traz os pares, e o que vale igual converge. O
  "desfeito aqui" não é reaplicado (é mudança daqui que não subiu): o pull guarda a versão da nuvem
  como pendente, e a varredura seguinte põe o `delete` (a nuvem tem o id; antes do clique também,
  pela escolha 5). O que foi aprendido aqui e a nuvem não tem sobe com o clique.
- **Conta nova:** depois do clique, sobe só o aprendido neste PC (fora do "veio da nuvem"). O
  "desfeito aqui" é da conta antiga e não sobe para a nova: lá, ele tiraria uma troca que a conta
  nova aprendeu por conta própria (a F8e guarda a que conta ele pertence, só pelo `sub`, ou usa outro
  meio).
- **`410` no mesmo epoch:** só o cursor volta a 0; a base do espelho fica, e o pull reconhece o eco
  pela versão.
- **Epoch novo** (um backup restaurado), no molde do `Recomecar`: a base perde a `server_version`, e
  todo par do espelho é marcado de novo: o presente vira `upsert` com base 0, e o "desfeito aqui"
  vira `delete` (sem linha, `nada_a_desfazer`, e o marcador sai sem laço). A linha "recusado" é
  liberada e tenta uma vez de novo.
- **Sem o cursor 0 na 0007.** Nenhuma nuvem tem `learned_pairs` antes da F8 (o push do `learned` é
  recusado até ela), nada foi para produção, e a nuvem de teste sobe vazia (`-SemExemplos`): um PC
  da F7 nunca convive com uma nuvem da F8 que tenha aprendido. Por isso a 0007 não volta o cursor, e
  o "recomeço ao atualizar" que a F7a/b previa não é preciso. É uma escolha, e um risco aceito
  (abaixo).

**O que o celular e o PC mostram.**
- **Celular:** a seção "Aprendido", no lugar já marcado (`ConfiguracoesNuvem.tsx`, entre Aparelhos e
  Sessões abertas): a lista (o termo e a variante) com Desfazer, que confirma na própria linha (o
  padrão do Revogar, porque ali não dá para aprender de novo), e a pílula "Desfeito em todos os
  aparelhos."; com o teto, uma linha diz que só os mais recentes aparecem. Sem Aprender.
- **PC:** a lista "Aprendido no painel" ganha as linhas de estado do espelho, com a etiqueta cinza:
  "não aplicado neste PC" (sem Desfazer, com o motivo numa frase curta), "desfeito em outro aparelho"
  e "trocado em outro aparelho". A seção aparece também quando só tem linhas de estado. O `GET
  /api/aprendizado` ganha `situacao` e `motivo`, opcionais.
- **A pergunta do envio** ganha o M (D3). **As duas Privacidades** passam a dizer que o aprendido
  sobe.
- **O log**, no Agent e no servidor, só com contagens (aplicados, não aplicados por motivo,
  desfeitos, pulados) e códigos de listas fechadas: nunca a variante, o termo ou o domínio.

**As subfatias.** Cada uma com o seu commit, `go test ./...` e `go vet ./...` verdes (e o lint, o
teste e o build do `web/` quando mexe nele), sem `git push`. Nenhum teste toca o dicionário real, o
Cofre, o teclado ou a área de transferência: o arquivo fica em `t.TempDir()`, e o runtime é um dublê.
A ordem segue a das fatias anteriores: a nuvem, o PC sem rede, a ligação e a tela.

| Subfatia | O que entrega | O que prova |
|---|---|---|
| F8.0 | esta seção e as emendas (`02`, `03`, `05`, `06`, `07`, `09`, `13`, `16` e a ADR-020; na revisão, também `00`, `08`, `18`, o `PRODUCT.md` e a ADR-017) | nada de código; `go test` e `go vet` como estavam |
| F8a | o par no dicionário, puro e sobre o arquivo (`internal/dictionary`): o id e a chave; as pré-checagens que a nuvem também usa; a projeção; o aplicar em lote (aprender, desfazer, trocar e criar pela D2), com as regras do PC que recebe, na ordem da tabela, e os motivos | os vetores do id congelados; a tabela da projeção e a do PC que recebe, linha a linha (nunca alarga; o restrito sem domínio não sobe; a variante repetida não sobe nenhuma; o que a nuvem recusaria não sobe: `Normalize` vazio, 81 runas, o caractere nulo); o domínio fora do mapa fica "não aplicado" mesmo com o termo local no domínio X (a ordem da tabela); o termo local `restrito` sem `dominio` fica "não aplicado"; o convergente e o "já vale aqui" não gravam; um lote faz uma gravação só, e um par que veta outro é pego; desfazer o que foi aplicado volta o arquivo byte a byte; o `Gravar` não escreve campo novo |
| F8b | a nuvem, sem banco: os tipos, as pré-checagens, o `DecideLearned`, o `delete` só para o aprendido, o `Applied` com a operação e o `duplicate` que a compara, o `Change` e o `state` por tipo, e o Store em memória (o push, o pull fundido por `seq`, o "apagar meus dados") | a suíte de contrato, uma linha da tabela por caso; o `nada_a_desfazer`; o mesmo conteúdo sem `seq`; o evento do desfazer é `upsert`; o id que não confere, a variante vazia ou com 81 runas, o restrito sem domínio, a variante, o termo ou o domínio com o caractere nulo; o mesmo `mutation_id` com outra operação dá `mutacao_reusada`, e com a mesma, `duplicate`; o desfazer sem resposta seguido de reaprender; o mesmo id em duas contas; `seq` sem buraco; o caso "learned antes da F8" invertido; a mudança `learned` decodifica no `Item` da F7; o log sem variante nem termo |
| F8c | o Postgres: a 0009 corrigida no lugar com a coluna `operation` (e o `schema_test`); o ramo do aprendido no `gravar` (a linha `FOR UPDATE`), o evento com o tipo certo, a terceira consulta no mesmo snapshot; a 0016 não muda | a suíte contra o Postgres, com o `duplicate` de outra operação; um aprendido gravado entre as consultas do pull não faz o cursor pular; nenhum `23505`, `23514` nem `22021`; os erros sem texto. Só com o contêiner que o Mauro sobe (e um banco de teste novo: o de antes recusa a 0009 por checksum) |
| F8d | as rotas do celular, na memória e no Postgres | o token do PC dá `403`; outra conta dá `404`; o desfazer repetido não gasta `seq`; o desfazer do celular chega ao pull como `upsert` com `undone_at`; o teto e o `has_more`; o CSRF e o `Host` nas rotas novas; o log sem texto |
| F8e | o espelho e a 0007 (SQLite), o "veio da nuvem", o percurso do aprendido no `storage`, o `CompletarEnvio` que só grava o aceite sozinho com M = 0, o esquecer e o epoch novo | um banco na 0006 com dados migra para a 0007 sem perder nada, tabela por tabela; o cursor e as versões dos ditados não mudam; o marcador sem texto e nunca `inicial`; o `CompletarEnvio` sem ditado elegível e com M > 0 não grava o aceite; o esquecer zera a base (menos a linha "aplicando") e guarda o "desfeito aqui" (só o id) e o "veio da nuvem"; o esquecer entre o arquivo e o espelho guarda a linha "aplicando", e a varredura seguinte a confirma sem que o par suba para a conta nova; o esquecer apaga o texto do não aplicado, do pendente e das etiquetas (fica o id); o epoch novo marca de novo todo par (o presente vira `upsert` com base 0, o "desfeito aqui" vira `delete`, e o `nada_a_desfazer` tira o marcador sem laço) e libera o "recusado"; a pergunta do esquecer conta o aprendido; o `schema_test` sem coluna proibida |
| F8f1 | o fio por tipo (`internal/sync/fio.go`): o `Change.Payload` e o `MutationResult.State` por `entity_type`, e o `delete` | nenhum comportamento muda: os testes da F7 verdes, sem mexer neles |
| F8f2 | as decisões puras do aprendido: o pull, a resposta do push e a mutação | as tabelas acima, caso a caso |
| F8f3 | a varredura, antes do `CompletarEnvio`, com o M | o arquivo indisponível nunca vira `delete`; a varredura ociosa não escreve; antes do aceite, nenhum `upsert` (nem o do par que a nuvem tem) e só o `delete` de um id que a nuvem tem; o M conta o que o clique solta; adotar não é desfazer; o freio; o "aplicando" confirmado sem envio, e o "aplicando" com o arquivo igual à guardada volta a pendente; a adaptação não volta; o par sem versão da nuvem sai sem envio; o "veio da nuvem" não sobe para a conta nova; o pendente reaplicado; uma nuvem que recusa tudo gera uma tentativa por mudança, e não uma por rodada; o Desfazer e o Aprender do mesmo par pelo gancho, com o arquivo igual ao de antes, viram `upsert`; a etiqueta some quando a pessoa aprende de novo e aos 30 dias (relógio injetado), e o texto dela sai junto |
| F8f4 | o `Receber` com o aplicador (uma interface; nil mantém a F7) | o epoch ou o pareamento mudados entre o pull e o arquivo não tocam o arquivo; um Aprender no meio é pulado; a queda entre o arquivo e o espelho converge sem envio; o arquivo inválido não segura os ditados; o "não aplicado" anda o cursor |
| F8g | o Agent liga tudo: o aplicador em `internal/agent/aprendiz`, o `montarSync` com ele, o `AoMudar` (que também sobe a versão local no ato), o `GET /api/aprendizado` com a situação e o motivo (a `situacao` do fio traduzida do espelho, pela tabela do `06`, com um teste do Agent que a fixa), o `historico` com o M (`previa_trocas`) e com o freio (`freio_trocas`, o `Relatorio.Freio` da última varredura, que some quando ela o solta), o log com contagens | a ponta a ponta com dois PCs e o celular, sobre a nuvem em memória e o aplicador de verdade (TEST-008, AC-021): o par do PC 1 chega ao PC 2 com a restrição e o domínio; o desfazer no celular some dos dois; a D3 e a D4; o PC com o histórico vazio e um par de origem `"painel"` não envia nada até o `POST /api/nuvem/enviar`, e a pergunta aparece com N = 0; o PC que ainda não clicou e desfaz um par que veio da nuvem leva o desfazer ao celular; o Desfazer e o Aprender do mesmo par com o PC desconectado, enquanto outro aparelho desfaz, terminam com a etiqueta; o esquecer, a conta nova e a volta; o `dicionario.json` só com os campos de antes; o log sem texto. Nada injeta entrada no sistema |
| F8h | a web: o "Aprendido" no celular, as linhas de estado no PC, a pergunta com o M (também com N = 0), as Privacidades | `pnpm lint`, `test` e `build`; o `bundle.test.ts` (a nuvem sem `/api/aprendizado`, o local sem `/api/v1`); a tela vista em 320 e 375 px, clara e escura |
| F8i | o roteiro `docs/teste-manual-nuvem-f8.md` e os documentos (`07`, `08`, `11`, `18`, `20`, `docs/fase-1-cobertura.md`, o README) e os textos velhos (abaixo da tabela) | o Mauro roda: aprende no `bin\`, vê no celular, desfaz no celular e a troca some do PC; depois o contrário |

**Os textos velhos que a F8i corrige** (o que está velho em cada um):
- `07-flows.md`, "Learning", passo 9 (linhas 331 a 334): o sync do aprendido diz só "o par com a
  restrição e o domínio" e "não aplicado neste PC"; falta o que a F8 decidiu (só a troca do Aprender,
  D1; chega sozinha aos outros PCs, D2; só depois do "Enviar o histórico", D3; vale o primeiro na
  nuvem, D4);
- `07-flows.md`, passo 7 (linha 329), "O que o ASR recebe como dica não muda", e
  `18-tool-contract.md:186-187`, "O vocabulário do ASR **não** muda" e "um termo novo de prioridade
  50 fica abaixo do corte": velhos desde a ADR-020 (decisões 5 e 6: o prompt é remontado do arquivo
  relido em toda gravação do painel, e quem tira do prompt o termo que o painel criou é o
  `DisputaOPrompt`, pelo `AliasOnly`, e não a prioridade). A F8 acrescenta: o aplicador também
  remonta o prompt (o `gravarETrocar`), e o termo criado pela D2 é `AliasOnly` e fica fora dele;
- `01-intake.md:88`, "sync entre devices", e `:108`, "histórico/dicionário/correções/preferências
  compatíveis/devices sincronizam": na etapa 5 sincronizam o histórico, o aprendido do painel (D1) e
  os aparelhos; o dicionário completo, as correções e as preferências ficam para depois (REQ-017 e
  REQ-018).

**Escolhas menores, a confirmar pelo Mauro na entrega** (como nas F4 a F7: a implementação usa a
proposta até ele dizer o contrário):
1. **A remoção à mão** de um par de origem `"painel"` no `dicionario.json` propaga como desfazer (é a
   mesma diferença que o Desfazer do painel deixa no arquivo), com o freio contra a remoção em massa:
   um limite numérico e, quando ele dispara, nada sobe e o painel avisa. A alternativa: só o Desfazer
   do painel propaga, o que pediria marcar o clique, porque a varredura não distingue os dois.
2. **Os 30 dias** das etiquetas "desfeito em outro aparelho" e "trocado em outro aparelho", e, por
   esse tempo, **o texto delas no espelho do PC** (a variante e o termo; é retenção de dado: some
   com a etiqueta, ou no esquecer). A alternativa: a etiqueta só com o id, sem dizer qual troca.
3. **O teto do `GET /api/v1/learned`:** proposta, 500 (o teto do lote do push e da página do pull),
   os mais recentes, com o `has_more`.
4. **Sem o cursor 0** na 0007 (acima).
5. **O desfazer antes do clique.** Antes do "Enviar o histórico", sai só o `delete` de um par que a
   nuvem já tem (chegou pelo pull): não leva texto, e sem ele o Desfazer num PC que ainda não clicou
   não chegaria ao celular nem aos outros PCs, e o PC e a nuvem divergiriam em silêncio. O `upsert`
   (aprender, reaprender, trocar o termo), mesmo de um par que a nuvem tem, espera o clique (D3). As
   alternativas: tudo do par que a nuvem tem sai antes, como a edição de um ditado que veio da nuvem
   na F7c; ou nada sai, e o PC fica divergente até o clique.
6. **A troca presa a um domínio cuja palavra este PC não tem fica "não aplicado"**, mesmo quando o
   PC conhece o domínio. É uma leitura que restringe a D2 além do texto dela ("um domínio que aquele
   PC não conhece"): o Aprender só cria a palavra sem domínio, e criá-la sem o domínio alargaria a
   troca. A alternativa: criar a palavra com o domínio e a restrição da troca (a alternativa da D2).
7. **A segunda etiqueta, "trocado em outro aparelho"**, para o segundo caso da D4 (a mesma variante
   aprendida para termos diferentes), ao lado de "desfeito em outro aparelho". A alternativa: uma
   etiqueta só, "mudado em outro aparelho".
8. **O PC da F7 cujo aceite a rodada gravou sozinha** (histórico vazio) conta como quem clicou: ao
   atualizar, as trocas dele sobem sem pergunta. Só o `bin\` do Mauro tem a F7 conectada, e ele
   clicou (07-out-2026). A alternativa: a 0007 apaga o aceite gravado sem cópia (`nuvem_copia`
   ausente), e a pergunta volta, com N = 0.

**Confirmadas pelo Mauro em 08-out-2026** ("Confirmo todas"), na entrega da F8, como lhe foram apresentadas: a remoção à mão que propaga como desfazer, com o freio de mais de 10 e o aviso no painel (escolha 1 e desvio 16); os 30 dias das etiquetas (2); o teto de 500 da lista do celular, com o domínio embaixo da troca (3 e a F8h); o desfazer como o único aprendido que sobe antes do clique (5); a troca presa a um domínio cuja palavra o PC não tem fica "não aplicado" (6); a palavra desligada que continua com as trocas na nuvem (o desvio da F8a); e a pergunta só com trocas, "Vão subir M trocas aprendidas.", com o botão "Enviar o histórico" (a F8h). As escolhas 4 (sem o cursor 0), 7 (as duas etiquetas, já vistas na tela) e 8 (o aceite gravado sozinho de um PC da F7) seguem a proposta. Os desvios técnicos (1 a 44) ficam aceitos como estão, revistos e com mutação; continua aberto, sem pressa, o desvio 3 (o "v2" do nome do id, só se o `Normalize` mudar um dia).

**O que este registro completou**, por ser preciso para o desenho fechar (técnico; a revisão da F8
confere):
- o envio em voo repete a mesma operação: com o `duplicate` que confere a operação, reenviar o
  desfazer em voo como reaprender daria `mutacao_reusada`, e o reenvio com a base velha perderia para
  o próprio desfazer;
- a coluna `operation` na 0009, corrigida no lugar, de onde o `duplicate` tira a operação;
- o "desfeito aqui" guardado no esquecer sobe só de volta à mesma conta;
- o aceite automático do `CompletarEnvio` só com M = 0 (a D3: a pergunta aparece com N = 0);
- a situação "recusado", com a projeção recusada, contra o reenvio sem fim;
- o esquecer guarda a linha "aplicando" até a reconciliação;
- o gancho do painel sobe a versão local no ato, para o Desfazer e o Aprender do mesmo par não
  sumirem da varredura;
- a ordem da tabela do PC que recebe (o mapa `dominios` primeiro) e o termo `restrito` sem `dominio`;
- as pré-checagens com o caractere nulo e o UTF-8, numa função que o PC e a nuvem dividem.

**Riscos aceitos.**
- **Sem o cursor 0:** se um PC da F7 se conectar a uma nuvem da F8 que tenha aprendido, o que ele
  pulou não chega depois de atualizar. A saída é "Esquecer esta nuvem" e conectar de novo (o pull do
  cursor 0). O avesso também: um PC da F8 contra uma nuvem anterior a ela recebe
  `tipo_ainda_nao_sincroniza`, e o par fica "recusado" até mudar, até um epoch novo ou até o
  esquecer, mesmo depois de a nuvem ser atualizada. Nenhuma nuvem anterior à F8 está em uso.
- **A mudança à mão que volta ao que era** entre duas varreduras (tirar e repor o par no arquivo,
  sem o painel) não é vista: só o Aprender e o Desfazer do painel sobem a versão local no ato. Se a
  nuvem desfez o par nesse meio-tempo, ele sai daqui sem etiqueta.
- **O `dicionario.json` não tem trava entre processos** (`internal/dictionary/arquivo.go`): o
  `dictlearn` e a edição à mão correm por fora, e o aplicador é um escritor a mais. Ele divide a
  trava `a.mu` com o painel: um lote grande faz o clique esperar.
- **A remoção à mão propaga** (escolha 1): restaurar um `dicionario.json` antigo do mesmo usuário
  vira desfazer em todos os aparelhos, até o limite do freio.
- **A troca chega sem clique no destino** (o THR-018 ampliado): um par ruim chega a todos os PCs. A
  defesa é o PC que recebe (as integridades, o "agiria", a palavra comum, o nunca alargar), e o
  Desfazer vale em todos.
- **No desacordo, a pessoa não escolhe** (D4): diferente do selo do ditado, o PC que perdeu segue a
  nuvem, com a etiqueta. No epoch novo, o par mudado aqui depois do backup perde para o do backup.
- **O texto no servidor:** a variante e o termo ficam legíveis, não vencem com os 6 meses do ditado,
  e a linha desfeita guarda o texto (a 0016 exige o `term`) até a F9 decidir. A F9 e a revisão de
  LGPD incluem o aprendido (`09`). **[F9 decidiu em 08-out-2026 (D1, confirmada pelo Mauro): a linha
  desfeita fica enquanto a conta existir, e o job não a toca; a revisão de LGPD da F11 pode mudar.
  Ver "Decisões da F9".]**
- **A estabilidade do id:** mudar o `Normalize` depois duplica pares na nuvem; os vetores congelados
  avisam antes.
- **O Postgres:** a F8c só tem prova verde quando o Mauro sobe o contêiner (os testes de Postgres não
  rodam desde a F3).

**O que a F8.0 não fez:** nenhum código, nenhuma migration, nenhum teste novo. As referências velhas
de "O aprendido" foram corrigidas (`seed.go:258` é `:266`; `seed.go:157` é `:163`).

**O estado da F8a a F8d (07-out-2026).** Implementadas e revisadas no mesmo dia, cada uma com os
seus commits, com `go test ./...`, `go vet ./...` e `go test -tags logindeteste ./cmd/server/...
./internal/server/...` verdes. Nenhuma dependência nova; a única migration tocada é a 0009, corrigida
no lugar (a coluna `operation`); a 0016 não mudou. Na hora, a F8e a F8i não tinham começado (a F8e a F8f4 e a F8h vieram depois, abaixo): o PC ainda
não sobe nem recebe o aprendido (o aplicador do `internal/dictionary` existe, mas nada o chama fora
dos testes).
- **F8a** (`internal/dictionary/par.go` e `lote.go`): o id (UUID v5, vetores congelados e conferidos
  fora do Go) e a chave; as pré-checagens (`ConferirPar`, a mesma função da nuvem); a projeção
  (`ParesDoPainel`); e o lote do PC que recebe (`Semente.AplicarPares` e `Arquivo.AplicarPares`: uma
  leitura, uma gravação), com a tabela na ordem, as integridades do Aprender e, por gancho, a palavra
  comum e o "agiria" (o aplicador do Agent é da F8g). A ordem das linhas 1 e 2 da tabela (o mapa
  `dominios` antes do termo `restrito` sem `dominio`) ganhou um teste próprio no fechamento: a
  inversão delas sobrevivia em todo o pacote.
- **F8b** (`internal/server/cloudstore`, as rotas do sync): o `DecideLearned`, linha a linha; o
  `delete` só no aprendido; o `Applied` com a operação e o `duplicate` que a compara
  (`mutacao_reusada`); o `Change` e o `state` por tipo; o Store em memória (o push, o pull fundido por
  `seq`, o "apagar meus dados"); a suíte de contrato; o PC da F7 decodifica a mudança `learned` sem
  erro.
- **F8c** (`internal/server/pgstore`, a 0009 e o `schema_test`): o ramo do aprendido no ponto único
  de gravação (a linha `FOR UPDATE`, o evento `upsert`, as datas do relógio de quem chama), a
  terceira consulta do pull no mesmo snapshot (`mergePage`) e a coluna `operation`. A suíte de
  contrato roda contra o Postgres sem marca de exceção.
- **F8d** (`internal/server/learned_routes.go`, `cloudstore` e `pgstore`): `GET /api/v1/learned` e
  `POST /api/v1/learned/undo`, só com a sessão, pelo ponto único de gravação (`UndoPair`, a mesma
  regra do `delete` do PC).
- **O Postgres rodou em 07-out-2026**, num contêiner novo que o Mauro subiu com `scripts/postgres-de-teste.ps1`: `go test ./migrations/ ./internal/server/... ./cmd/server/...` e o mesmo com `-tags logindeteste`, todos verdes (`pgstore` 19 s e `migrations` 7,9 s, contra menos de 1 s quando pulam; nos dois pacotes, 78 testes passaram e nenhum pulou), com `TestContrato_Postgres` (os casos do aprendido e do celular), `learned_pg_test.go`, `learned_celular_pg_test.go`, `TestPostgres_PullNumSnapshotSo` e `TestPostgres_MutacaoGuardaAOperacao`. Uma passada de mutação contra o Postgres, numa cópia por `git archive`, cada mutação conferida como aplicada: o evento do aprendido gravado como ditado, a conferência da chave desligada, o aprendido fora da junção do pull, a ordem da consulta do aprendido e o cursor com `>=` morreram (5 de 6). Sobreviveu tirar o `FOR UPDATE` da linha do aprendido, **equivalente**: a trava da linha do contador vem antes e enfileira toda gravação do mesmo usuário, então nenhuma outra transação lê a linha entre a leitura e a escrita (o `FOR UPDATE` fica como defesa).
- **A mutação:** a viva não equivalente de cada passada ganhou teste (o `>=` no cursor do pull do
  aprendido, na F8b; a ordem das linhas 1 e 2 da tabela do PC que recebe, no fechamento da F8a a
  F8d, conferida aplicando a mutação: o teste novo falha com ela e passa sem ela). As que mexem no
  SQL que só o Postgres executa (por exemplo, a chamada do `mergePage` dentro do `Pull`) só o
  contêiner mata.

**Os desvios do desenho**, mantidos pela implementação e pela revisão (a confirmar pelo Mauro na
entrega, como as escolhas menores):
1. **O convergente vem antes da tabela do PC que recebe** (F8a, na revisão). O desenho põe o mapa
   `dominios` primeiro; a implementação decide antes dele quando a variante já está aqui com a origem
   `"painel"` e sobe exatamente como chega (o mesmo termo, como o Search compara; a mesma restrição
   efetiva; o mesmo domínio): convergente, nada é gravado. Sem isso, o par aprendido aqui num termo
   com um domínio fora do mapa (o Aprender do painel não confere o mapa) daria `dominio_ausente` no
   eco e na volta à mesma conta, e a troca o tiraria do arquivo; e a variante em `alias_restrito` de
   uma entrada restrita seria regravada em `alias`. O par que difere segue a tabela, na ordem.
2. **A variante com a origem em mais de um lugar**, no PC que recebe, fica "não aplicado" com
   `variante_de_outro_termo` (a lista fechada não ganhou motivo novo), e o arquivo não é tocado.
3. **A guarda da chave antes do `INSERT`** (F8c), que o desenho não tinha: outra linha do usuário
   com o mesmo `normalized_variant` faz o push ser `rejected` `campo_invalido`, sem gravar nem gastar
   `seq`, em vez do `23505` com a variante no `DETAIL` e um `500` sem fim. Ela só dispara quando o id
   da linha não vem da chave pelo nome de hoje (o nome do UUID v5 trocado, o "v2", ou uma linha posta
   à mão). **Pendente, decisão do Mauro:** um "v2" do nome do id precisa de uma migração que regrave o
   id e a chave das linhas antes de um PC mandar ids v2, ou de outra resposta da guarda; sem isso, todo
   par já sincronizado de chave igual seria recusado para sempre. Uma regra de chave mudada sem trocar
   o nome não é pega e grava uma segunda linha (o risco "a estabilidade do id").
4. **O payload, fechado além do desenho** (F8b): os quatro campos do `upsert` são obrigatórios
   (`domain` vem `null`, nunca ausente) e `restricted` é booleano, senão `campo_invalido`; o `delete`
   aceita o payload ausente, `null` ou `{}`; a nuvem compara o termo como veio (outra caixa é outro
   conteúdo), enquanto o PC que recebe compara como o Search.
5. **O UTF-8 conferido nos bytes crus** do payload do aprendido (F8b, na revisão): o `encoding/json`
   troca o byte inválido e o surrogate solto por U+FFFD sem erro, e a `ConferirPar` nunca os veria. O
   `cleanString` do ditado tem a mesma limitação e **não mudou** (registrado no `06`).
6. **As rotas do celular** (F8d): o corpo do `undo` sem o campo `id`, ou ilegível, é `400`, que o
   desenho não listava (os `404` ficam como estavam, com a frase "troca aprendida não encontrada");
   o item sem domínio leva `dominio` `""`; o evento do desfazer do celular não tem aparelho (o pull o
   traz sem `device_id`); o teto é 500, a proposta da escolha menor 3, ainda a confirmar.

**O estado da F8e (07-out-2026).** Implementada no mesmo dia, num commit (`feat(storage)`), com `go
test ./...`, `go vet ./...` e o `-tags logindeteste` do servidor verdes. Nenhuma dependência nova;
nada no `web/`. O PC ainda não sobe nem recebe o aprendido: o espelho existe, mas só os testes o
escrevem (a varredura é da F8f3, o aplicador da F8f4 e o gancho da F8g).
- **A 0007** (`internal/storage/migrations/0007_sync_aprendido.sql`): só `CREATE TABLE`, o espelho
  (`sync_aprendido`) e o "veio da nuvem" (`sync_veio_da_nuvem`, uma tabela só de ids); o cursor não
  volta a 0. Os nomes finais das colunas estão no `05` ("sync_aprendido"). A prova
  (`migracao_0007_internal_test.go`): um banco na 0006 com dados (ditados de todo tipo, a fila com
  um em voo, um do envio inicial e uma escolha do selo, o anotado do esquecer, o cursor, a
  instalação com o aceite), todas as tabelas do `sqlite_master` lidas antes e depois; o cursor e as
  versões dos ditados conferidos à parte; a 0007 que falha no meio não deixa nada. O
  `sync_historico_de_verdade_e2e_test.go` (o `bin\` de mentira) passa a esperar a 0006 e a 0007.
- **O percurso** (`internal/storage/aprendido_sqlite.go`): o estado de um id
  (`EstadoDoAprendido`: a base, a versão local, a projeção guardada, a esperada, o par da nuvem
  guardado, a situação, o marcador e o "veio da nuvem"); o executor (`AcaoDoAprendido`), conferido
  inteiro antes de gravar, por `Fila.MudarAprendido` (sem epoch) e `Fila.AplicarComAprendido` (na
  transação dos ditados e do cursor, com o epoch); `Fila.Espelho`; `Fila.PendentesDoAprendido`;
  `Fila.SubirVersaoLocal`. O `CompletarEnvio` recebe o M (as trocas a subir) e só grava o aceite
  sozinho com N = 0 e M = 0; o motor passa 0 até a F8f3. A pergunta do esquecer
  (`ResumoDoEnvio.Mudancas`) soma os marcadores `learned` (o `Pendentes` continua só dos ditados).
- **A mutação**, conferida aplicando cada uma (o teste falha com ela e passa sem ela): o aceite com
  M > 0; a pergunta sem o aprendido; o esquecer que limpa a linha "aplicando"; o que não apaga o
  texto do par da nuvem; o que guarda a conta também no par presente; a conta nunca solta, e
  sempre solta; o epoch novo com o upsert antes do aceite, com a linha "aplicando", com o delete do
  que a nuvem nunca teve e sem liberar o recusado; o envio sem o freio do aceite; a projeção trocada
  em voo; a esperada que fica fora de "aplicando"; a 0007 que volta o cursor.

**Os desvios e as leituras da F8e**, a confirmar pelo Mauro na entrega (como os de cima):
7. **A base sem o texto da nuvem.** O `05` listava o par confirmado (variante, termo, restrição,
   domínio) na base; a base guarda só a versão, se está desfeito e a versão local que ela cobre. O
   texto de uma linha que vale no arquivo é a projeção guardada, e nada nas decisões usa o texto da
   nuvem da base (o eco é pela versão; o PC que recebe compara com o arquivo). Menos texto retido.
8. **A projeção recusada é a guardada** (sem colunas à parte): na situação "recusado", a projeção
   guardada é a recusada, e a varredura só a troca quando o par muda, o que libera a linha.
9. **A operação do envio em voo**: em vez de guardá-la, a projeção guardada não muda durante o voo
   (o executor recusa com `ErrProjecaoEmVoo`); o reenvio sai dela, com a mesma operação e o mesmo
   payload, e a mudança do voo só sobe a versão local. A F8f3 não grava projeção de id em voo. **Na
   reconexão** (revisão da F8f): o `Preparar` esquecia o envio em voo também do `learned`, a projeção
   descongelava, e o envio seguinte saía com a operação nova e a base velha (o reaprender perdia para
   o próprio desfazer sem resposta, com a etiqueta falsa). Agora o marcador fica a repetir
   (`Marcador.Repetir`: sem o `mutation_id`, com a versão enviada; `Marcador.Congelado`), o aparelho
   novo repete a mesma operação com outro id, e só depois a mudança do voo sobe. O recomeço do epoch
   novo continua esquecendo o envio inteiro; o ditado não muda.
10. **O esquecer zera a `server_version` também na linha "aplicando"**, e apaga o par da intenção.
    O desenho dizia que a linha "aplicando" fica com a `server_version` e a situação; aqui ficam a
    situação e a projeção esperada. Por quê: a versão de uma conta esquecida, numa conta nova com o
    mesmo número, seria tomada por eco, e o par dela nunca chegaria. A reconciliação não precisa da
    versão: a confirmação grava a esperada sem versão, e o "veio da nuvem" segura o envio.
11. **O "desfeito aqui" e a conta.** Fica no espelho (e não na `sync_mudou_aqui`): é a linha
    ausente da projeção com a versão local acima da base, sem depender da situação `desfeito_aqui`.
    A conta é o id da conta na nuvem (`nuvem_conta`), e não o `sub`, guardado pelo esquecer em
    `conta_esquecida`; a primeira rodada do pareamento seguinte o mantém de volta à mesma conta e o
    solta numa conta nova (ou sem saber a conta). **O Desfazer que a varredura não viu** (revisão da
    F8f): feito com o PC desconectado (o laço parado), ou com o par num envio em voo (a projeção
    congelada), ele deixa a projeção guardada presente. O registro anterior dizia que, numa conta
    nova, a varredura podia mandar o `delete`; não podia (lá ela assenta, sem versão), e o dano real
    era na volta à mesma conta: a varredura assentava o par que saía, e o pull do cursor 0 o punha de
    volta no arquivo, sem nada subir. Agora o esquecer guarda a conta também na linha presente mudada
    aqui que a nuvem tinha; de volta à mesma conta, a marca fica (`MesmaContaEsquecida`) até a linha
    ganhar uma projeção, uma base ou uma versão coberta, e a varredura não assenta o par que vê sair
    (`TipoVarreduraEspera`): o pull guarda a versão, e o `delete` sobe (a linha do desvio 23). **O
    que fica:** até a varredura seguinte, a projeção dessa linha (a variante e o termo do par
    desfeito) continua no espelho, porque o esquecer não lê o arquivo.
12. **O epoch novo e o aceite.** O par presente só ganha o `upsert` depois do aceite (D3; antes,
    a varredura o conta, e o clique o solta); o `delete` sai antes. Ficam de fora a linha
    "aplicando" (a varredura a reconcilia antes: senão a adaptação deste PC subiria por ela) e o
    "desfeito aqui" sem versão da nuvem conhecida (a varredura também não o poria). **O par do "veio
    da nuvem" também fica de fora** (revisão da F8f; a F8e seguia "todo par"): o "veio da nuvem"
    sobrevive ao esquecer, e no primeiro epoch novo de uma conta nova o par de uma conta esquecida
    subia para ela; e o par pode estar com a adaptação deste PC. Quem o sobe de novo é o PC de
    origem, no epoch novo dele (o celular não aprende). Isso resolve a tensão que este item
    registrava. O par que veio da nuvem e foi aprendido de novo aqui já saiu do "veio da nuvem" (a
    varredura o tira) e sobe.
13. **As listas fechadas em Go**, e não por `CHECK`: a situação, o motivo do "não aplicado" e o
    código da recusa (`a-z` e `_`, até 40, como o `last_sync_error`), para uma subfatia seguinte não
    precisar refazer a tabela. Os booleanos têm `CHECK`.

**O que a F8f precisa saber do storage:** a varredura não grava projeção de id em voo (só sobe a
versão local, ou nada); quando o `delete` não nasce (sem versão da nuvem), ela assenta a mudança
(`Cobre` igual à versão local), senão o pull de uma troca que outro aparelho aprender depois a
tiraria daqui; ela conta o M e o passa ao `CompletarEnvio`; ela tira a etiqueta aos 30 dias pela
`SituacaoEm` (e o par da nuvem guardado junto). O par que chega da nuvem passa no `ConferirPar` e
tem o id certo antes de ser guardado (o executor recusa, e a recusa desfaz a página inteira); o
código de recusa fora do formato vira um código próprio antes.

**O estado da F8f3 (07-out-2026).** Implementada no mesmo dia (`feat(sync)`), com `go test ./...` e
`go vet ./...` verdes. Nenhuma dependência nova; nada no `web/`; nenhuma migration. A F8f1 (o fio
por tipo) e a F8f2 (as decisões puras) vieram antes, sem nota própria. O PC ainda não recebe o
aprendido pelo pull (o `Receber` com o aplicador é da F8f4), e o Agent não liga nada (F8g).
- **A varredura** (`internal/sync/varredura.go`): `Motor.Varrer`, no começo da `Rodada`, depois do
  `Preparar` e antes do `CompletarEnvio` (que recebe o M dela) e do envio. O arquivo vem pela
  interface `DicionarioDoAprendido` (o `AplicarPares` do lote do aplicador; `dictionary.Arquivo` a
  satisfaz, e o Agent a embrulha na F8g); `Motor.Dicionario` e `ConfigDoLaco.Dicionario` nil deixam
  o aprendido de fora, como na F7. A tabela é `DecidirVarredura`, pura, na ordem da tabela de cima.
  Em três passos, sob a trava do dicionário: a tabela numa transação (`Fila.MudarAprendido`); os
  pendentes que a tabela não tocou vão ao lote, com a leitura curta do epoch e do pareamento antes
  (`Fila.ConferirPareamento`, nova, só lê) e a intenção antes da gravação (`IntencaoDoPar`); e o
  espelho deles (`DecidirPar`) na transação com o epoch (`Fila.AplicarComAprendido`, sem ditados nem
  cursor). Um filtro (`soOQueMuda`) tira de cada ação o que não muda o espelho: a varredura ociosa
  não escreve no SQLite, e a tentativa de novo de um pendente que continua não valendo aqui também
  não.
- **O envio do aprendido** (`Motor.Enviar`, com o `Dicionario`): os marcadores `learned`
  (`Fila.PendentesDoAprendido`) vão no começo de cada lote, antes dos ditados, com a mutação do
  espelho (`MutacaoDoAprendido`) e a resposta decidida (`DecidirResultadoDoAprendido`) na mesma
  transação dos ditados do lote.
- **O resto:** `Fila.EnvioAceito` (só lê o aceite); `Semente.VarianteNoArquivo` (onde está, no
  arquivo, a variante que saiu da projeção: ainda com a origem, ou adotada); o `Relatorio` com o que
  a varredura gravou, o M, o freio e o arquivo indisponível, e o log do laço com essas contagens
  (`aprendido_marcados`, `aprendido_a_subir`, `aprendido_reaplicados`, `aprendido_freio`,
  `aprendido_sem_dicionario`), sem texto.
- **A prova** (`varredura_test.go`, a tabela linha a linha; `varredura_percurso_test.go`, com o
  `dicionario.json` de verdade num `t.TempDir()`, o `historico.db` de verdade e uma nuvem de
  roteiro; o Aprender e o Desfazer do painel são o `Arquivo` mais o `SubirVersaoLocal`): o arquivo
  ausente, inválido e de outro usuário não grava nada, e a rodada sem ele não grava o aceite; a
  varredura ociosa não escreve (`total_changes()` da conexão única, e os bytes do arquivo), antes e
  depois do aceite; antes do aceite, nenhum `upsert` (nem a troca de termo de um par que a nuvem
  tem), só o `delete` de um id que a nuvem tem, e o par aprendido e desfeito antes do clique não gera
  nada; o M é o número de `upsert`s que o clique solta, a rodada com M > 0 e N = 0 não grava o
  aceite, e a pergunta tem N = 0; adotar não é desfazer (nem no epoch novo), e a mesma variante
  movida para outro termo é; o par restrito sem domínio e o repetido ficam sem `delete`; o freio
  (11 à mão seguram tudo, 10 saem, 11 pelo painel saem); a linha "aplicando" confirmada sem envio,
  a que volta a pendente e entra na varredura seguinte, e a mudada aqui; o pendente reaplicado
  (o domínio criado aqui, o arquivo que volta) com a adaptação deste PC, que não volta; o par sem
  versão da nuvem sai sem envio (o marcador que esperava sai; em voo, o `delete` sai depois da
  resposta); o "veio da nuvem" não sobe para a conta nova; a nuvem que recusa tudo, rodada a rodada
  pelo `Motor`, tem uma tentativa por mudança; o Desfazer e o Aprender pelo gancho viram `upsert`
  (antes do aceite, o M); a etiqueta some aos 30 dias (o relógio do motor) e quando a pessoa aprende
  de novo, com o texto; o epoch ou o pareamento mudados não tocam o arquivo.
- **A mutação**, conferida aplicando cada uma (27 mutações, 27 mortas): sem a linha do par que não
  sobe, sem a adoção, sem o freio, o Desfazer do painel contando no freio, sem o aceite, o "veio da
  nuvem" que sobe, sem o par presente que a nuvem não tem, sem o filtro, a confirmação sem a
  projeção, o "aplicando" que fica, o `delete` sem versão e o que nunca nasce, a etiqueta que vence
  só depois do prazo e a que nunca vence, o reaprender que guarda o texto ou a etiqueta, o pendente
  na mesma varredura da tabela, sem a leitura curta, sem a linha do recusado, a versão local que sobe
  a cada varredura em voo, o M sem o arquivo e o M sempre 0, o "veio da nuvem" que não sai, a adoção
  e o assentado sem cobrir a versão local, o "desfeito aqui" que nunca sobe e o envio sem o
  aprendido. A passada do fechamento achou mais uma viva, já morta ("O fechamento da F8e a F8f4").

**Os desvios e as leituras da F8f3**, a confirmar pelo Mauro na entrega (como os de cima):
14. **O envio do aprendido entrou na F8f3.** O desenho não diz qual subfatia liga o push do
    `learned` ao `Motor`, e a prova "uma tentativa por mudança, e não uma por rodada" é por rodada.
    Só com o `Dicionario` (sem ele, nada muda da F7).
15. **A varredura grava sob a trava do dicionário.** O arquivo é lido uma vez pelo `AplicarPares` do
    aplicador (com zero pares quando não há pendente, e aí nada é gravado no arquivo), e a transação
    da tabela roda dentro dele: um clique do painel espera uma transação curta do SQLite (o risco
    "o aplicador divide a trava" que a ADR já aceita). Por quê: lido fora da trava, um Desfazer do
    painel entre a leitura e a gravação faria a varredura mandar o `upsert` do par desfeito.
16. **O freio: mais de 10 pares saindo do arquivo numa varredura**, contando só os que saem sem o
    Desfazer do painel (a versão local não subiu no ato; o Desfazer pode acumular com o PC
    desconectado). Com o freio, a varredura não grava nada (nem as outras linhas da tabela) e não
    tenta os pendentes; o `Relatorio.Freio` e o log dizem quantos (a frase do painel é da F8h).
    **Pendente, decisão do Mauro:** não há como confirmar a remoção; quem tirou de propósito mais de
    10 pares à mão fica com o freio até repor o arquivo (ou a F8h ganhar um "confirmar").
17. **"O arquivo mudou ou voltou" sem marca do arquivo.** O pendente é tentado de novo a cada
    varredura, sobre o arquivo relido, e só grava o que muda (o filtro). O custo são as conferências
    do lote (o "agiria" do Agent) por pendente, a cada rodada. O id que a tabela tocou espera a
    varredura seguinte (a primeira linha que casa decide): o "aplicando" que volta a pendente entra
    na seguinte.
18. **A confirmação da linha "aplicando" não grava a base** (só a projeção esperada e a situação).
    **Corrigido na revisão da F8f:** ela tirava também o par da intenção, contando com o pull da
    mesma versão para andar a base; isso só vale com o cursor parado, e não quando a intenção veio
    da tentativa de novo de um pendente, ou do lote que gravou o arquivo e devolveu erro (a página
    andou o cursor): a versão aplicada sumia, e o reaprender daqui perdia para o próprio desfazer.
    Agora o par da nuvem guardado, com a versão, fica pendente, e a tentativa de novo dele
    (`PlanejarPendente`, na varredura seguinte) converge e anda a base; sem a versão (o recomeço), o
    texto sai. A versão guardada é desta conta e deste epoch: o esquecer apaga o par da intenção (o
    desvio 10), e o recomeço a versão dele. A etiqueta que a intenção tomou o lugar não volta (a
    linha fica "aplicado").
19. **O "aplicando" que não chegou ao arquivo fica no "veio da nuvem"** (a intenção o pôs): o mais
    conservador. O avesso, tirá-lo, poderia mandar para uma conta nova um par da conta esquecida; o
    custo é que um par aprendido aqui com o mesmo id só sobe para uma conta nova depois de mudar.
20. **O gancho depois do lote** (o "aplicando" com o arquivo igual à esperada e a versão local acima
    da base): confirma e é mudança daqui (`upsert`, e o id sai do "veio da nuvem").
21. **O "recusado" não volta pelo gancho:** o Desfazer e o Aprender que deixam a projeção igual à
    recusada não mandam de novo (a recusa não guarda a versão local que subiu); só outra projeção, um
    epoch novo ou o esquecer liberam a linha.
22. **Sem o arquivo, o aceite não é gravado sozinho:** o M não é conhecido, e o motor passa M = 1 ao
    `CompletarEnvio` (as trocas que estiverem lá subiriam sem pergunta).
23. **O "desfeito aqui" assenta só na passagem.** O par que sai do arquivo nesta varredura sem versão
    da nuvem é assentado (a escolha da F8e); o que já tinha saído e continua mudado aqui (o "desfeito
    aqui" de volta à mesma conta, sem a versão ainda) espera, e vira `delete` quando o pull guarda a
    versão.
24. **"Sem `server_version`" é sem versão conhecida.** O par presente fora do "veio da nuvem" sobe
    quando o PC não conhece versão nenhuma dele (nem a da base, nem a do par da nuvem guardado): com
    o `state` de um `conflict` guardado e a base vazia, o par mandaria de novo a cada rodada.
25. **Adotar assenta a versão local**, e a linha fica sem projeção: senão o "já vale aqui" com a
    versão local acima da base seria lido como "desfeito aqui" pelo esquecer e pelo epoch novo.
26. **A etiqueta vencida com o `state` ainda pendente** (não valeu aqui): sai só a situação; o texto
    fica como o par pendente, retido enquanto a conta é a mesma, até valer aqui.
27. **A situação "desfeito aqui" não é gravada.** As regras não dependem dela (a F8e), e a tela da
    F8h não a mostra; o `delete` é o marcador com a projeção ausente.

**O que a F8f4 precisa saber da F8f3:** o aplicador é o `Motor.Dicionario` (a mesma interface); o
passo 1 do `Receber` é o `Fila.ConferirPareamento`; o `receber` de `varredura_percurso_test.go` é a
F8f4 reduzida a uma mudança (o `AplicarPares` com o `PlanejarPar`, a intenção e o `DecidirPar` na
página). A varredura reconcilia a linha "aplicando" que a página deixar, e o `soOQueMuda` evita a
escrita de um lote que não muda nada.

**O estado da F8f4 (07-out-2026).** Implementada no mesmo dia, num commit (`feat(sync)`), com `go
test ./...` e `go vet ./...` verdes. Nenhuma dependência nova; nada no `web/`; nenhuma migration; o
`storage` e o `dictionary` não mudaram. O Agent ainda não liga nada (F8g): fora dos testes, ninguém
passa o aplicador ao `Motor`.
- **O Receber** (`internal/sync/receber.go`; `Motor.Receber`): com o `Dicionario`, cada página do
  pull separa as mudanças `learned` das do ditado (uma por id, a última da página). As do aprendido
  passam pelo aplicador (o `DicionarioDoAprendido`, a mesma interface da varredura): sob a trava do
  dicionário, com o arquivo relido e o espelho lido ali, o `PlanejarPar` de cada id (o par mudado
  aqui que não subiu é pulado); só quando algum par vai ao lote, e ainda sob a trava, a leitura curta
  do epoch e do pareamento; a intenção antes da gravação (`IntencaoDoPar`, pelo
  `Fila.MudarAprendido`); e o espelho de cada id (`DecidirPar`) na transação da página
  (`Fila.AplicarComAprendido`), com os ditados e o cursor. Sem o `Dicionario`, a página é a da F7 (a
  mudança `learned` é "não aplicada" pelo `DecidirMudanca`, e o cursor anda).
- **O resto:** o `Relatorio.PullDoAprendido` (as decisões do pull do aprendido, pelos tipos do
  `DecidirPar`; o `Pull` continua só do ditado) e quatro contagens no log do laço
  (`aprendido_aplicados`, `aprendido_desfeitos`, `aprendido_nao_aplicados`, `aprendido_pulados`),
  sem texto.
- **A prova** (`receber_percurso_test.go`, com o `dicionario.json` de verdade num `t.TempDir()`, o
  `historico.db` de verdade e uma nuvem de roteiro que manda o pull; o "no meio" é um gancho na
  nuvem, entre o pull e o arquivo, ou no aplicador, entre o arquivo e a página; a queda é o contexto
  cancelado entre o arquivo e a página; a falha do SQLite é um gatilho no banco de teste): nil mantém
  a F7 (o arquivo e o espelho intocados, o cursor anda); o par entra com a adaptação deste PC, na
  página do ditado, e não volta como `upsert`, nem depois do aceite; o pull da mesma versão é eco e
  não toca o arquivo; o primeiro pull de um pareamento adota o epoch; a mesma variante duas vezes na
  página vale pela última; **o epoch ou o pareamento mudados entre o pull e o arquivo não tocam o
  arquivo** nem aplicam nada da página (no epoch novo, a rodada recomeça e aplica); **um Aprender do
  painel no meio é pulado** (o par da nuvem fica pendente, a mudança daqui sobe, e a nuvem decide: a
  D4 de ponta a ponta, com "trocado em outro aparelho"), o Desfazer e o Aprender que deixam o
  arquivo igual também, e o Desfazer entre o arquivo e a página não é sobrescrito (vira `delete`);
  **a queda entre o arquivo e o espelho converge sem envio** (a varredura confirma a linha
  "aplicando", e o pull da mesma versão converge), e o epoch mudado entre o arquivo e a página
  também; **o arquivo inválido não segura os ditados**, nem o ausente, o de outro usuário, o lote que
  não carrega e a gravação que falha depois da intenção (a página entra, o cursor anda, o par fica
  pendente e entra quando o arquivo volta, sem envio); a troca de um par que vale aqui, com o arquivo
  indisponível, fica pendente, e não pulada; a intenção que o SQLite não grava segura a página (o
  arquivo não é gravado, e o cursor não anda); **o "não aplicado" anda o cursor**, e a mudança que
  nem é par também, página a página.
- **A mutação**, conferida aplicando cada uma (o teste falha com ela e passa sem ela; 15 mortas): sem
  a leitura curta; sem adotar o epoch nulo; o arquivo indisponível que segura a página; o lote que
  falha e segura; a intenção que falha e não segura; a primeira mudança do id em vez da última; sem a
  intenção; sem o resultado do lote; o lote de outro tamanho; nada ao lote; sem a projeção do
  arquivo; sem o indisponível no plano; o espelho da página que não grava; o `learned` pelo caminho
  do ditado; sem contar o aprendido. Uma equivalente: a leitura curta também sem par no lote (a
  transação da página confere o mesmo). O filtro `soOQueMuda` no espelho da página também era
  equivalente (as ações do `DecidirPar` já não regravam à toa), e saiu.

**Os desvios e as leituras da F8f4**, a confirmar pelo Mauro na entrega (como os de cima):
28. **A leitura curta adota o epoch nulo.** A nota da F8f3 apontava o `Fila.ConferirPareamento`, que
    só lê e recusa o epoch nulo. No pull, o epoch vem da nuvem, e o desenho pede "as mesmas regras do
    `Fila.Aplicar`": sem o estado do pareamento, `ErrPareamentoMudou`; outro epoch,
    `ErrEpochDiferente`; o nulo (a primeira página de um pareamento novo, ou a de depois de um
    recomeço, sem push antes) é adotado, como a transação da página o adotaria. Sem isso, o primeiro
    pull de um PC recém-conectado nunca aplicaria o aprendido (o recomeço volta o epoch a nulo, e a
    rodada desistiria). O caso comum (o mesmo epoch) continua só leitura (`ConferirPareamento`).
29. **A leitura curta roda sob a trava, depois do plano**, e só quando algum par vai ao lote: o mais
    perto possível da gravação, como na varredura (desvio 15). O desenho a põe antes da trava; o
    efeito é o mesmo (o arquivo não é tocado com o epoch ou o pareamento mudados), com a janela menor.
30. **Qualquer erro do arquivo que não é do SQLite deixa o par pendente**, e não só os do
    `ArquivoIndisponivel`: o erro de leitura inesperado (uma permissão, um antivírus segurando o
    arquivo), o lote que não carrega (o `Validar`, ou a conferência do "agiria") e a gravação que
    falha depois da intenção. O par fica "não aplicado" com `arquivo_indisponivel` (ou na linha
    "aplicando", que a varredura reconcilia), e o cursor anda: o mais conservador para os ditados, sem
    perder o par (a varredura o tenta a cada rodada). Na varredura, o erro inesperado de leitura
    continua sendo erro da rodada (a F8f3).
31. **A intenção que o SQLite não grava segura a página**: o arquivo não é gravado (a intenção vem
    antes), nada da página entra, e o cursor não anda. É erro de E/S, o único que segura o cursor.
32. **A mesma variante duas vezes numa página** vale pela última mudança (a de maior `seq`). A nuvem
    não manda isso (uma linha por entidade no pull); é defesa, para o lote não receber o par duas
    vezes.
33. **O `Pull` do `Relatorio` deixa de contar a mudança `learned`** quando há o `Dicionario` (ela vai
    ao `PullDoAprendido`); sem ele, continua contando-a como "não aplicada", como na F7. O log ganha
    só quatro contagens; o detalhe por motivo fica para a F8g.

**O que a F8g precisa saber da F8f4:** o `Motor.Receber` já aplica o aprendido com o `Dicionario`, e
o laço já o passa (`ConfigDoLaco.Dicionario`). Falta o Agent: o `montarSync` com o aplicador, que
embrulha o `dictionary.Arquivo` (a trava é a dele, a mesma do painel) para trocar o dicionário em
memória e o prompt depois de gravar, com o `ValidarPar` e o `PalavraComum` do `aprendiz`; e o gancho
`AoMudar` do painel com o `Fila.SubirVersaoLocal` no ato, sem o qual o Desfazer e o Aprender que
deixam o arquivo igual não são pulados pelo Receber.

**A revisão da F8e a F8f4 (07-out-2026).** Dez achados, sete distintos (três vieram em dobro, de
dimensões diferentes da revisão), todos conferidos no código e reais. Cada correção é um commit
`fix`, com o teste que falhava antes e passa depois; `go test ./...` e `go vet ./...` verdes. Nenhuma
dependência nova, nenhuma migration nova (só o comentário da coluna `conta_esquecida` na 0007, que
nunca rodou fora de um banco de teste), nada no `web/`.
- **O epoch novo e o "veio da nuvem"** (alta): o recomeço não marca mais o par do "veio da nuvem"
  (o desvio 12, que ganhou a resolução).
- **O Desfazer que a varredura não viu** (alta e média: o PC desconectado, ou o par num envio em
  voo, antes do esquecer): o desvio 11, com o risco antigo corrigido.
- **O par aprendido aqui na conta nova** (média): o esquecer guarda a conta também no par aprendido
  aqui que a nuvem confirmou (presente, sem mudança daqui, fora do "veio da nuvem" e da linha
  "aplicando"), e a primeira rodada numa conta nova sobe a versão local dele: é a mudança daqui que
  a D3 manda subir. A varredura já o segurava para o clique (o M), mas o pull da conta nova, com a
  mesma variante desfeita ou com outro termo, o tirava ou trocava antes do clique, sem etiqueta e sem
  envio. Agora o pull o pula até o clique, ele sobe com a base 0, e a nuvem decide (D4, com a
  etiqueta no PC que perder). De volta à mesma conta, nada muda: o pull do cursor 0 o reconcilia, e
  o que a conta mudou enquanto o PC estava esquecido vale aqui, como no ditado.
- **A reconexão com o envio em voo** (média e baixa): o desvio 9.
- **A confirmação da linha "aplicando"** (média, duas vezes): o desvio 18.
- **O pull mais novo na linha "aplicando"** (média): o pulado e o pendente numa linha "aplicando"
  guardam o par que chega quando ele é mais novo que o da intenção (a situação e a projeção esperada
  ficam: `naIntencao`), e é ele que a varredura deixa pendente quando confirma ou devolve a intenção,
  e aplica. Antes, o cursor andava sem guardar nada, e a varredura aplicava o par velho da intenção:
  o PC voltava a um estado velho da nuvem (o `PlanejarPar` diz que o pull nunca faz isso), e nunca
  se corrigia.
- **A pergunta do esquecer** (baixa): `ResumoDoEnvio.Mudancas` conta só o marcador `learned` de uma
  mudança feita aqui (a linha do espelho com `versao_local > versao_base`), como o ditado; o que o
  epoch novo põe num par que a nuvem já tinha confirmado não conta.
- **A prova:** `internal/sync/aprendido_revisao_percurso_test.go` roda cada caso pelo `Motor`, rodada
  a rodada, com o `dicionario.json` e o `historico.db` de verdade em `t.TempDir()` e uma nuvem de
  roteiro que guarda o estado e responde pela tabela da nuvem (o desfazer sem resposta, a reconexão
  e o reaprender; o Desfazer não visto e a volta à mesma conta, desconectado e em voo; a conta nova
  com a variante desfeita ou trocada lá; a confirmação com o cursor andado, pela queda e pela troca em
  memória que falha, na varredura e no Receber; o par mais novo na linha "aplicando"). O `storage`
  ganhou os testes do recomeço, da pergunta, da marca do esquecer e do envio a repetir. Mutações
  conferidas aplicando cada uma (o teste novo falha com ela e passa sem ela): a reconexão que esquece
  o envio inteiro; a marca que sai também de volta à mesma conta; o par mais novo que não é guardado;
  a confirmação que tira o par da nuvem com a versão.
- **O que a F8g precisa saber da revisão:** o erro do `AplicarPares` pode voltar com o arquivo já
  gravado (a troca em memória que falha depois da gravação): o sync o trata como o lote que não
  terminou, e converge (o contrato está no comentário do `DicionarioDoAprendido`). O envio a
  repetir (`storage.Marcador.Repetir`) é só do aprendido.

**O fechamento da F8e a F8f4 (08-out-2026).** A F8e, a F8f1, a F8f2, a F8f3 e a F8f4 estão
implementadas, revisadas e fechadas: os commits `feat`/`refactor` de cada subfatia, os sete `fix` da
revisão de cima e um `test` deste fechamento, com `go test ./... -count=1` e `go vet ./...` verdes.
Nenhuma dependência nova; nenhuma migration além da 0007 do SQLite (que só rodou em banco de teste);
nada no `web/`. O PC ainda não sobe nem recebe o aprendido fora dos testes: o Agent não passa o
aplicador ao `Motor` (F8g), e o painel não mostra nada (F8h).
- **A mutação do fechamento:** uma passada nova sobre a F8e a F8f4 deixou uma viva não equivalente,
  que a lista da F8f3 não tinha ("a etiqueta que vence só depois do prazo e a que nunca vence"
  mexiam no prazo, e não em quem vence): o vencimento dos 30 dias sem conferir que a situação é uma
  etiqueta. Com ela, o "aplicado" e o "já vale aqui" sumiam da lista aos 30 dias, e o "não aplicado"
  que andou a base até a versão do par (o domínio ausente) deixava de ser pendente, e a varredura
  nunca mais o aplicava, nem com a causa resolvida. Morta por quatro linhas novas na tabela
  (`varredura_test.go`: o "aplicado", o "já vale aqui" e o "não aplicado" pelo arquivo indisponível e
  pelo domínio ausente, começados uma hora além do `PrazoDaEtiqueta`) e por um percurso
  (`TestVarredura_AsOutrasSituacoesNaoVencem`: a varredura não escreve, a situação fica, e o
  pendente entra quando o domínio é criado ou o arquivo volta, mais de 30 dias depois), conferidos
  aplicando a mutação à mão (os oito casos falham com ela e passam sem ela). O código não mudou.
- **Os desvios aceitos para seguir** são os de cima, 7 a 33 (da F8e, 7 a 13; da F8f3, 14 a 27; da
  F8f4, 28 a 33), mantidos pela implementação, pela revisão e por este fechamento, com as correções
  que a revisão fez no 9 (a reconexão com o envio em voo), no 11 (o Desfazer que a varredura não
  viu), no 12 (o "veio da nuvem" fora do epoch novo) e no 18 (a confirmação da linha "aplicando").
  Aceitos aqui quer dizer que a F8g parte deles; quem os confirma ou muda é o Mauro, na entrega,
  junto com os 1 a 6 da F8a a F8d e as escolhas menores (a remoção à mão que propaga como desfazer,
  com o freio; os 30 dias da etiqueta; o teto da lista do celular; sem o cursor 0).
- **Continuam pendentes de decisão do Mauro:** o 16 (não há como confirmar uma remoção de mais de 10
  pares à mão: o freio segura até o arquivo ser reposto, ou até a F8h ganhar um "confirmar") e o 3
  (o "v2" do nome do id, da F8c).
- **O que a F8g recebe:** as notas "O que a F8g precisa saber" da F8f4 e da revisão, sem mudança.

**O estado da F8g (08-out-2026).** Implementada no mesmo dia, em commits por parte (`refactor(agent)`,
`feat(agent)`, `feat(localapi)`, `feat(nuvem)`, `feat(sync)`, `test(integration)` e este
`docs`), com `go test ./... -count=1` e `go vet ./...` verdes. Nenhuma dependência nova; nenhuma
migration; nada no `web/` (a tela é da F8h). O Agent liga tudo: o PC sobe e recebe o aprendido.
- **O aplicador** (`internal/agent/aprendiz`): o `Aplicador` é o `dicionario.json` do Agent, com a
  trava do painel, e saiu do `package main` inteiro: o Aprender, o Desfazer, as Aprendidas, o
  `Dicionario` da sugestão e o "Adicionar termo", com o `gravarETrocar` de antes (uma trava, uma
  gravação, uma troca do dicionário e do prompt), e o `AplicarPares` que o sync espera
  (`DicionarioDoAprendido`, na varredura e no `Receber`) sob a MESMA trava: grava só quando o lote
  muda o arquivo, e então troca uma vez; a troca que falha depois de gravar é
  `dictionary.ErrGravadoSemTroca` (o lote que não terminou). As conferências (`conferir.go`):
  `SmartDaSemente`, `Validar`, `Agiria` (o `ValidarAprendida` do painel, sem mudança),
  `ValidarPar` (o par que chega, com a restrição efetiva: o alias restrito confere só no domínio
  da entrada), `DominiosDaConferencia` com a restrição, e a palavra comum. No `cmd/agent` ficam a
  troca no runtime do Windows (`winruntime.Dicionario` com o Polish do Smart novo, atrás de
  `Config.Trocar`) e a conferência do "Adicionar termo" (`conferirTermos`). *(Nota de 11-out-2026,
  ADR-021, C2: as conferências do `conferir.go` foram para `internal/smartconta`, onde o servidor as usa
  também; o Agent as chama de lá e o comportamento dele não mudou. A palavra comum, `Arriscada`, ficou em
  `internal/learning`.)*
- **A ligação** (`cmd/agent/sync.go`): o `montarSync` passa o aplicador ao laço, e o
  `Aplicador.LigarAoSync` liga o gancho do painel: o Aprender e o Desfazer que gravaram sobem a
  versão local do id no espelho (`Fila.SubirVersaoLocal`) no ato, sob a trava, e acordam o laço.
- **O painel** (`internal/agent/localapi`): o `GET /api/aprendizado` ganha `situacao` e `motivo`
  (os nomes finais estão no `06`), lidos do espelho (`Preferencias.Espelho`, a `storage.Fila` do
  dono); o `historico` do `GET /api/nuvem` ganha o M (`previa_trocas`, o nome que a F8h lê; até a
  revisão abaixo, `trocas`), do resumo do envio
  (`storage.ResumoDoEnvio.Trocas`, pelo `EstadoDoAprendido.SeguradoPeloClique`), e, na revisão, o
  `freio_trocas`; o `POST
  /api/nuvem/enviar` com N = 0 e M > 0 grava o aceite sem cópia (o storage já o fazia), e o log do
  envio diz as trocas.
- **O log**, só com contagens: a linha do lote do aplicador (por situação e, no não aplicado, por
  motivo) e `aprendido_nao_aplicados_por_motivo` no log do laço; o `06` lista as linhas.
- **A prova:** os testes do `aprendiz` (a tabela dos domínios com a restrição; o par restrito que
  entra em `alias_restrito` e age; o que perde no domínio fica `nao_agiria`; o lote que grava e
  troca uma vez, e o convergente e a varredura ociosa que não gravam nem trocam; a mesma trava do
  painel, com o Aprender que espera o lote e relê o que ele gravou; o `ErrGravadoSemTroca` no sync;
  o gancho sob a trava, sem o contexto do pedido, e só no Aprender e no Desfazer que gravaram; o
  log sem texto); o `cmd/agent` (o gancho pela montagem de verdade, e o Agent sem dicionário que
  deixa o aprendido de fora); o `localapi` (as linhas de estado, linha a linha); o `sync` (o M do
  resumo e o da varredura contam igual, percurso a percurso; o não aplicado por motivo no log); e a
  ponta a ponta (`internal/server/integration/sync_aprendido_e2e_test.go`, com dois PCs, o celular,
  a nuvem em memória e o aplicador de verdade): o par do PC 1 chega ao PC 2 com a restrição e o
  domínio, e ao celular; a D3 (o PC com o histórico vazio e pares do painel não envia nada até o
  `POST /api/nuvem/enviar`, e a pergunta aparece com N = 0; o clique com N = 0 não faz cópia; o PC
  que não clicou não sobe o ditado nem o par dele, e recebe o que vem da nuvem); o PC que ainda não
  clicou e desfaz um par que veio da nuvem leva o desfazer ao celular e ao outro PC; o desfazer no
  celular some dos dois, sem etiqueta; a D4 (vale o primeiro, com "trocado em outro aparelho"; o
  Desfazer e o Aprender do mesmo par com o PC desconectado, enquanto outro aparelho desfaz,
  terminam com "desfeito em outro aparelho"; aprender de novo tira a etiqueta e sobe); o esquecer e
  a volta, e a conta nova; o `dicionario.json` só com os campos de antes; o log sem texto.
- **A mutação**, conferida aplicando cada uma (o teste falha com ela e passa sem ela): a restrição
  efetiva fora da conferência (no `aprendiz` e na ponta a ponta); o `montarSync` sem o gancho (no
  `cmd/agent`) e o aplicador sem o gancho (na ponta a ponta: a D4 do PC desconectado e a volta à
  mesma conta caem); o lote do sync sem a troca do runtime; o M do resumo sem o "mudado aqui", e
  contado depois do aceite; o resumo sem o M (a pergunta com N = 0 some).

**Os desvios e as leituras da F8g**, a confirmar pelo Mauro na entrega (como os de cima):
34. **O aplicador levou o painel inteiro.** O desenho movia só as conferências; a trava tem de ser a
    mesma do painel, e por isso o `Aprender`, o `Desfazer` e o "Adicionar termo" também foram para
    o `Aplicador`. O comportamento do painel não muda: os testes de antes passam, só com os nomes
    novos das funções que saíram do `cmd/agent` (a tabela dos domínios foi para o `aprendiz`, com
    os casos da restrição).
35. **O M do `historico` vem do espelho**, e não da varredura em memória: o resumo conta os pares
    que a última varredura deixou segurados (`SeguradoPeloClique`, a mesma regra da linha do
    upsert dela com o arquivo igual à projeção guardada). Sobrevive ao reinício, e um teste confere
    que os dois contam igual. O par aprendido depois da última varredura só entra no M na seguinte
    (o gancho acorda o laço; desconectado, a pergunta não vale).
36. **O gancho roda sob a trava**, depois de o arquivo ser gravado e antes da troca, com o contexto
    do pedido sem o corte (o arquivo já mudou). O espelho que falha é registrado, sem texto, e o
    clique dá certo: a varredura vê a mudança pelo arquivo, menos a que o deixa como era.
37. **Sem dicionário na subida, o aplicador vai ao laço como o arquivo indisponível** (revisto na
    revisão da F8g, abaixo; antes, o aprendido ficava de fora, com o `Dicionario` nil, como na F7, e
    o aceite era gravado sozinho com o histórico vazio, contra o desvio 22). A varredura dá
    `SemArquivo`, o M vale 1 na rodada e o aceite não é gravado sozinho; o par do pull fica "não
    aplicado neste PC" (`arquivo_indisponivel`), guardado com a versão, e vale quando o dicionário
    voltar. O custo: com o histórico vazio, o aceite espera um ditado (a pergunta) ou o dicionário de
    volta; nada a enviar se perde. Só acontece quando a criação do dicionário vazio falhou (ADR-018).
38. **A lista do painel: o arquivo vence, e nunca leva `situacao`** (revisto na revisão da F8g,
    abaixo; antes, a etiqueta "trocado em outro aparelho" ia na linha do arquivo, e a web da F8h a
    tomaria por linha de estado, sem o Desfazer). A linha do arquivo é o par que vale aqui, com o
    Desfazer. As linhas de estado vêm depois, com os nomes do fio: o "trocado em outro aparelho"
    mostra a troca que este PC perdeu (o par perdido, a 0008, desvio 41), esteja o id no arquivo ou
    não; o "desfeito em outro aparelho" de um par que ainda está no arquivo (a varredura seguinte o
    resolve) não aparece. Os 30 dias da etiqueta também são conferidos no `GET` (com o PC
    desconectado, a varredura não roda).
39. **O "Adicionar termo" confere como antes.** A `conferirTermos` passa a restrição falsa ao
    `DominiosDaConferencia`: a variante em `alias_restrito` já só agia no domínio da entrada (no
    desconhecido, a conferência a pulava), e nada muda.
40. **A linha do lote no log fica em Debug quando não grava**: a varredura tenta de novo, a cada
    rodada, o par que continua não valendo aqui, e o Info encheria o `agent.log`. O "não aplicado"
    por motivo vai em Info pelo log do laço, que só conta o que mudou o espelho.

**A revisão da F8g (08-out-2026).** Dez achados, nove distintos (o do teste do gancho veio em dobro),
cada um conferido no código; todos eram reais. Cada correção tem o teste que falha sem ela (a
mutação aplicada e desfeita), em commits `fix(...)` próprios, com `go test ./...` e `go vet ./...`
verdes; o do `503` foi resolvido no documento, mantendo uma decisão anterior. Nada no `web/`: os nomes são os que a F8h, na
`main`, já lê (o `06` desta branch passa a dizê-los).
- **O `historico` do `GET /api/nuvem` não usava os nomes da web** (alta): o M saía como `trocas`, e
  o `freio_trocas` não saía. Com N = 0 e M > 0, a web da F8h não via nada a enviar, não perguntava, e
  o aceite nunca era gravado: as trocas não subiam. Agora `previa_trocas` sempre, e `freio_trocas`
  (conectado, com o freio) do `syncengine.Laco.Freio`: o da última varredura que comparou o arquivo,
  em memória (desvio 44); o `ligarSync` liga o laço ao `nuvem.Servico`. Os testes escrevem os nomes
  por extenso (a ponta a ponta lia a mesma tag da struct, e não provava nada).
- **O `GET /api/aprendizado` emitia as situações cruas do espelho e punha a etiqueta na linha do
  arquivo** (alta): a web descarta `desfeito_em_outro`/`trocado_em_outro`, e trataria o par que vale
  aqui como linha de estado, sem o Desfazer. Agora a tradução do `06` (`situacaoNoFio`, só as três), a
  linha do arquivo sem `situacao`, e o "trocado em outro aparelho" numa linha à parte com a troca que
  este PC perdeu, que o espelho passa a guardar (a 0008, desvio 41; o conflict a grava com a etiqueta).
  O teste que fixa a tabela do `06` é o `TestAprendizado_ATraducaoDoEspelhoParaOFio`.
- **O `dicionario.json` recriado vazio no reinício virava desfazer** (média): o `garantirDicionario`
  cria o vazio antes do laço, e a primeira varredura via cada par do espelho como "saiu do arquivo";
  abaixo do freio, os deletes subiam. O mesmo com o arquivo de outro usuário. Agora a subida confere o
  arquivo antes do laço (`prepararOAprendido`, com o `criou` e o usuario anotado em
  `nuvem_dicionario_usuario`) e, se é outro, solta o espelho do anterior (desvio 42).
- **A ligação do aprendido ao laço e ao painel no Agent de verdade não tinha teste** (média): o
  `configDoSync` e o `ligarAprendidoAoPainel` saíram do `montarSync` e do `run()`, e os testes do
  `cmd/agent` rodam uma rodada pela configuração do Agent e leem o painel montado assim; a ordem no
  `run()` (a conferência antes do `montarSync`) é conferida no código de `main.go`.
- **O desvio 37 pulava a D3** (baixa): sem dicionário, o aceite era gravado sozinho. Revisto (acima):
  o aplicador vai ao laço como o arquivo indisponível.
- **O teste do desvio 37 não observava nada** (baixa): trocado pelos que olham o efeito no laço (o
  aceite não gravado, o par do pull pendente).
- **O teste do gancho "sem o contexto do pedido" era vazio, nas duas leituras do achado** (baixa): o
  `context.Background()` já tem o `Done()` nulo. Agora o espelho falso corta o pedido antes de olhar o
  contexto que recebeu.
- **Os 30 dias da etiqueta na linha do arquivo não tinham teste no `GET`** (baixa): a linha do arquivo
  não leva mais etiqueta; o teste tem a etiqueta vencida no arquivo e fora dele.
- **Com o arquivo que não abre, o `GET` é `503` e as linhas de estado não saem** (baixa): mantido (a
  lista vazia esconderia o que a pessoa precisa desfazer, a revisão do ADR-017), e documentado no `06`
  (desvio 43).

**Os desvios da revisão da F8g**, a confirmar pelo Mauro na entrega (como os de cima):
41. **A 0008 guarda a troca que este PC perdeu** (`perdido_variante`, `perdido_termo`, duas colunas
    nulas na `sync_aprendido`: aditiva, nenhuma linha muda). A F8g dizia "nenhuma migration", mas a
    linha de estado da F8h ("a de estado, a que este PC tinha aprendido") pede o par que o espelho não
    guardava: depois que o par da nuvem passa a valer aqui, a projeção guardada vira a dele. O par
    perdido só vive com a etiqueta (e na linha "aplicando" que a aplica), sai com ela e no esquecer, e
    é retenção de dado como o resto do texto do espelho (o `09`). O delete não conflita (o desfazer
    sempre vence na nuvem), então não há par perdido de um desfazer; o "trocado" de uma linha de antes
    da 0008 fica sem linha de estado. **A alternativa:** o `06` e a web mudam juntos, e a linha de
    estado mostra o par da nuvem (repetido na linha do arquivo).
42. **O arquivo novo devolve os pares da nuvem.** O espelho do arquivo anterior é solto sem virar
    mudança daqui (`syncengine.SoltarOArquivoAntigo`), e o par que a nuvem tem vivo fica "não aplicado
    neste PC" (`arquivo_indisponivel`), e volta ao arquivo novo pela varredura, como volta o par que o
    pull trouxe com o arquivo indisponível: a nuvem é a cópia. Quem apagou o `dicionario.json` para
    "zerar" vê as trocas voltarem; para tirá-las de todos os aparelhos, é o Desfazer (no painel ou no
    celular). Ficam de fora a mudança daqui que não subiu e o id com marcador: o Desfazer do painel que
    a varredura não viu sobe como desfazer (é de verdade), mas também o Aprender ou a troca de termo
    ainda segurados viram o delete do par que a nuvem tem, porque o espelho não distingue os dois (o
    risco que fica). O arquivo de outro usuário recebe as trocas da conta, e as dele sobem como novas
    (como o arquivo antigo posto no lugar: a regra de sempre). **A alternativa:** o aprendido fica de
    fora do laço até a pessoa confirmar, sem devolver nada (preso, como o desvio 16).
43. **O arquivo que existe e não abre continua `503` no `GET /api/aprendizado`**, e as linhas de estado
    não saem com ele: o motivo `arquivo_indisponivel` só aparece com o arquivo ausente. **A
    alternativa:** a lista do arquivo vazia e as linhas de estado (`200`), o que esconderia o que a
    pessoa precisa desfazer.
44. **O `freio_trocas` vive só na memória do laço**: some num reinício até a primeira varredura com a
    conexão, que o calcula de novo (o arquivo é o mesmo). **A alternativa:** guardá-lo no SQLite.

**O que fica para a F8h e a F8i:** a tela (as linhas de estado com as frases de cada `situacao` e
`motivo`, a pergunta com o M também com N = 0, o "Aprendido" do celular, as Privacidades) e o
roteiro manual. Continuam pendentes de decisão do Mauro o 3 (o "v2" do nome do id) e o 16 (a
remoção de mais de 10 pares à mão).

**O fechamento da F8g (08-out-2026).** A F8g está implementada ("O estado da F8g"), revisada ("A
revisão da F8g") e fechada nesta branch, com `go test ./... -count=1` e `go vet ./...` verdes
depois do último commit de código. Nenhuma dependência nova; a única migration é a 0008 do SQLite
(aditiva, desvio 41); nada no `web/`. O Agent liga tudo: o aplicador vai ao laço (na varredura e no
`Receber`), o gancho do painel sobe a versão local no ato, o painel lê as linhas de estado do espelho
e o `historico` traz `previa_trocas` e `freio_trocas`.
- **O que a ponta a ponta prova** (`internal/server/integration/sync_aprendido_e2e_test.go`): três
  percursos sobre a nuvem de verdade (o Store em memória, num `httptest`), com o aplicador de verdade
  de cada PC, o `dicionario.json` e o `historico.db` em `t.TempDir()`, o laço de sync e a API local
  do painel. O runtime do Windows é o dublê do `Config.Trocar`, e o token vai para um
  `chaves.Memoria`: nada abre janela, digita, usa a área de transferência ou toca o Gerenciador de
  Credenciais.
  - `TestAprendidoE2E_DoPC1AoPC2EAoCelular`: o par do PC 1 chega ao PC 2 com a restrição e o
    domínio, e ao celular. A D3 no PC 1 (o histórico vazio e três pares de origem `"painel"`: nada
    sobe até o `POST /api/nuvem/enviar`, e a pergunta aparece com N = 0 e `previa_trocas` 3) e no
    PC 2, que não clicou (nem o ditado nem o par dele sobem, e o que vem da nuvem entra). O "não
    aplicado neste PC" (`dominio_ausente`). O PC 2 que ainda não clicou e desfaz um par que veio da
    nuvem leva o desfazer ao celular e ao PC 1; o desfazer no celular some dos dois. O
    `dicionario.json` só com os campos de antes; o log sem texto.
  - `TestAprendidoE2E_D4ValeOPrimeiroEODesfazerVence`: o PC 3 aprende pelo painel inteiro (o `PATCH`
    sugere, o `POST` aprende), para outro termo, a variante que o PC 1 já subiu: perde, segue a
    nuvem, e a lista mostra a linha do arquivo (a troca da nuvem, com o Desfazer) e a de estado
    (`trocado_em_outro_aparelho`, com a troca que ele perdeu). O PC 1, desconectado, desfaz e
    aprende de novo o mesmo par enquanto o PC 3 o desfaz: na volta, o desfazer vence e o PC 1 mostra
    "desfeito em outro aparelho"; aprender de novo tira a etiqueta e sobe.
  - `TestAprendidoE2E_EsquecerAVoltaEAContaNova`: o desfazer feito desconectado, de um par que veio
    da nuvem, sobe na volta à mesma conta (antes do clique) e chega ao outro PC; na conta nova, a
    pergunta conta só o par aprendido neste PC, e, com o clique, só ele sobe.
  Fica de fora o que é do Windows (a troca no `winruntime.Dicionario`, o hook, a inserção): é do
  roteiro manual da F8i, que o Mauro roda.
- **A mutação do fechamento.** Uma passada nova sobre a F8g, depois da revisão, deixou duas vivas
  não equivalentes, as duas no `aplicador.go`. Morrem agora por dois testes novos no
  `aplicador_test.go` (commit `test(aprendiz)`), conferidos aplicando cada mutação à mão e
  desfazendo (o teste falha com ela e passa sem ela). O código não mudou.
  - **O `AplicarPares` sem a trava do `Aplicador`** (viva no `aprendiz`, no `cmd/agent`, no `sync` e
    na ponta a ponta). Sem ela, o `dictionary.Arquivo` solta o próprio `mu` ao gravar, e um Aprender
    que entre antes da troca do lote grava e troca primeiro: a troca mais velha, sem o par novo,
    chega ao runtime por último, e o ditado segue sem o que a pessoa acabou de aprender até a
    gravação seguinte. O `TestAplicador_MesmaTravaDoPainel` prende só o `escolher`, que roda sob o
    `mu` do próprio `Arquivo`, e não via a ordem das trocas. O
    `TestAplicador_TrocaDoLoteEsperaATravaEAMaisNovaFicaPorUltimo` prende a troca do lote no runtime
    falso, já gravado o arquivo, e dispara um Aprender: o Aprender não termina em 150 ms, e as trocas
    chegam na ordem das gravações, a última com os dois pares e igual ao arquivo no disco. Sem gcc
    nesta máquina não há `go test -race`: a prova é pela ordem.
  - **O campo `desfeitos` do log do lote lendo o convergente** (viva no `aprendiz`, no `cmd/agent` e
    na ponta a ponta). O `TestAplicador_LogDoLoteSoComContagens` só tem aplicados e não aplicados, e
    os outros quatro campos ficavam em 0. O `TestAplicador_LogDoLoteContaCadaSituacao` confere a
    linha inteira do lote que desfaz (`desfeitos=1`, em Info, porque grava) e a de um lote que só
    confere, com `convergentes=1`, `ja_vale_aqui=2` (um ativo e um desfeito, cada um sobre uma
    variante posta à mão) e `ausentes=3` (em Debug, porque não grava). Entre os dois testes do log,
    quaisquer dois campos têm contagens diferentes em algum lote: um campo que lê a situação de
    outro muda a linha (conferido também em quatro trocas vizinhas).
- **Os desvios** são os 34 a 40 da implementação e os 41 a 44 da revisão, sem mudança no
  fechamento; o 37 e o 38 já estão revistos. Aceitos para seguir, a confirmar pelo Mauro na entrega,
  junto com os 1 a 33 e as escolhas menores; continuam pendentes de decisão dele o 3 (o "v2" do nome
  do id) e o 16 (a remoção de mais de 10 pares à mão).
- **O que fica:** a F8h (a web) já está na `main`, feita antes do Agent e lendo os nomes que esta
  branch emite; o merge desta branch na `main` foi feito em 08-out-2026, com o conflito esperado na linha
  da F8 em "Fatias" resolvido à mão. Depois, a F8i (o roteiro manual e os documentos).

**A F8h (07-out-2026).** A web, feita **antes** da F8g (o Agent ainda não emite os campos que ela lê;
a F8e a F8f4, acima, não tocam o `web/`), contra os dublês dos testes
(`web/src/testes/servidorFalso.ts` e `servidorFalsoNuvem.ts`): nenhum Agent nem servidor rodou.
`pnpm lint`, `pnpm test` (770 testes, 91 a mais, com a revisão abaixo) e os dois builds verdes; nenhuma dependência nova;
nenhum arquivo do Go ou do SQL mudou. As telas foram vistas no navegador, em 320 e 375 px, claro e escuro
(`/telas-dev.html` e `/telas-dev-nuvem.html`, só do `pnpm dev`; `08`, "Como a F8h fez").
- **O celular** (`web/src/nuvem/AprendidoNaNuvem.tsx`): a seção "Aprendido", entre os Aparelhos e as
  Sessões abertas; a lista (`sf → ECF`, e o domínio quando há); o Desfazer que confirma na própria linha,
  como o Revogar; a pílula "Desfeito em todos os aparelhos."; o vazio; os erros no molde das outras
  seções; o teto e o `has_more` numa linha. `ClienteNuvem` ganhou `aprendidos()` e
  `desfazerAprendido(id)`.
- **O PC** (`AprendidoNoPainel.tsx`): as linhas de estado, com a etiqueta cinza e sem Desfazer; a seção
  aparece também só com elas; o foco calculado pelos botões. A pergunta do envio
  (`NuvemNoPC.tsx`) com o M, também com N = 0. As duas Privacidades.
- **Os nomes que o `06` deixava para a F8h, e a F8g emite** (agora no `06`): `previa_trocas` e
  `freio_trocas` no `historico` do `GET /api/nuvem`; `situacao` (`nao_aplicado`,
  `desfeito_em_outro_aparelho`, `trocado_em_outro_aparelho`) e `motivo` (os de
  `internal/dictionary/lote.go`) no `GET /api/aprendizado`. **A F8g tem de emitir exatamente estes**
  (ou o `06` e a web mudam juntos): a web ignora uma `situacao` que não conhece.
- **O que a F8h decidiu e o Mauro confirma na entrega:**
  1. **O texto da pergunta:** "Vão subir N ditados dos últimos 6 meses e M trocas aprendidas. A lixeira
     não vai. Antes, uma cópia do histórico fica neste PC."; com N = 0, só "Vão subir M trocas
     aprendidas." (sem a lixeira e sem a cópia, que são do histórico). A alternativa: manter a lixeira e a
     cópia na frase também com N = 0.
  2. **A Privacidade do PC diz "as trocas que você aprendeu"**, e não "o que você aprendeu" (a frase do
     plano no `08`): a D1 sobe só a troca do Aprender, e "o que você aprendeu" sugeriria também os
     termos que a pessoa adicionou.
  3. **O celular mostra a restrição:** com domínio, uma linha pequena, "domínio X" ou, se a troca é
     presa a ele, "só no domínio X". O item do `06` traz `restrito` e `dominio`; sem a linha, o celular
     esconderia onde a troca age. A alternativa: só o termo e a variante.
  4. **O `404` do desfazer no celular, com a frase "troca aprendida não encontrada", tira a linha e
     diz "Desfeito em todos os aparelhos."**, como o sucesso: o que a pessoa queria (a troca fora de
     todos) já é verdade. A alternativa: dizer que já não estava lá. (Qualquer `404`, sem conferir a
     frase, valia na primeira versão; ver "A revisão da F8h".)
  5. **O teto não relê a lista depois de um Desfazer:** a linha diz "Só as mais recentes aparecem."; a
     troca mais antiga entra quando a tela aparece de novo. Com o teto de 500, é raro.
  6. **A linha de estado nunca tem Desfazer**, nem a de "trocado em outro aparelho" (uma troca deste PC
     que perdeu; a que vale vem em outra linha, da nuvem, com o Desfazer).
  7. **O botão da pergunta com N = 0** continua "Enviar o histórico" (a D3 e o `06` o chamam assim) e
     diz "Enviando…" durante o clique, no lugar de "Copiando o histórico…" (não há cópia).

**A revisão da F8h (08-out-2026).** Nove achados, cada um conferido; os que eram reais foram
corrigidos, cada um com o teste (ou o documento) que o prende. A branch recebeu a `main` (a F8e a F8f4)
por um merge, com o conflito só na ADR (a linha F8 da tabela e o bloco depois de "O estado da F8a a
F8d"); `pnpm lint`, `pnpm test` (770 testes) e os dois builds verdes depois dela. O aviso do freio foi
visto no navegador em 375 px (`/telas-dev.html?cenario=freio`).
- **O aviso do freio faltava** (a escolha menor 1 prometia "nada sobe, e o painel avisa", e o desvio 16
  deixava "a frase do painel" para a F8h, que não a entregou nem a listou). Agora: o campo
  `freio_trocas` no `historico` do `06` (um inteiro, opcional, só contagem), a frase no `08` ("{n}
  trocas aprendidas saíram do dicionário de uma vez. Por segurança, nada do aprendido sobe para a
  nuvem até elas voltarem."), uma linha de ajuda em `NuvemNoPC.tsx` e o cenário `freio` nas telas de
  dev. A F8g emite o campo enquanto o freio vale e o tira na varredura que o solta. **Não é o
  "confirmar" do desvio 16:** quem tirou de propósito mais de 10 pares à mão continua sem como
  liberar o freio que não seja repor o arquivo, e isso segue pendente de decisão do Mauro.
- **Os nomes da `situacao` no fio não eram os do espelho do Agent** (`desfeito_em_outro_aparelho` e
  `trocado_em_outro_aparelho`, contra `desfeito_em_outro` e `trocado_em_outro` em
  `internal/storage/aprendido_sqlite.go`), e o `06` dizia que a F8g "emite exatamente estes" sem avisar
  que era preciso traduzir. Uma `situacao` desconhecida faz a linha sumir, e um `aplicado` posto num
  par que vale tiraria da lista o par e o seu Desfazer. Agora o `06` traz a tabela do espelho para o
  fio, diz que as outras cinco situações nunca saem, e a F8g tem um teste do Agent que a fixa; o painel
  ganhou um teste dos três nomes que reconhece. O painel continua descartando o que não conhece (uma
  linha de estado que ele não sabe ler não pode virar um Desfazer que daria `404`); os motivos
  (`motivo`) batem com `internal/dictionary/lote.go`.
- **O `08` e o comentário de `clienteLocal.ts` descreviam o `historico.mudancas` sem a F8**, e o `06`
  e o Agent já somam a troca aprendida desfeita ou trocada aqui (`ResumoDoEnvio.Mudancas`, F8e). Dois
  documentos diziam números diferentes num ponto que o `CLAUDE.md` manda perguntar: o código manda, e o
  `08`, o `06` e o comentário passam a dizer o mesmo. A frase da pergunta do esquecer já servia ("{n}
  mudanças feitas aqui ainda não subiram").
- **Cinco dos oito motivos repetiam o "neste PC" da etiqueta** ("não aplicado neste PC" e, embaixo,
  "A palavra está desligada neste PC."). Cortado o escopo das cinco frases (o `08` e o teste mudam
  juntos, e o teste agora recusa "este/neste/deste PC" em qualquer motivo); as três que não o
  repetiam ficaram como estavam.
- **O `404` do desfazer valia por qualquer `404`** (a decisão 4): o painel estático publicado antes do
  binário do servidor, ou um proxy sem a rota, faria o app anunciar "Desfeito em todos os aparelhos."
  sem nada ter sido desfeito. Agora só vale o `404` com a frase do servidor
  (`internal/server/store_routes.go`); os outros são o erro comum "Não deu para desfazer: …".
- **O nome da pergunta do Desfazer não dizia qual troca era** (o foco vai para o Cancelar, e o botão
  da linha, que tinha o nome, sai de cena): o grupo agora se chama "sf vira ECF. Desfazer? A troca deixa
  de valer em todos os PCs." para o leitor de tela; o texto que se vê não mudou.
- **O Cancelar e o Esc fechavam a pergunta com o POST no ar**, e a troca saía da lista mesmo assim, com
  a pílula de "Desfeito": agora esperam o pedido terminar, no Desfazer e no Revogar (o mesmo padrão).
- **A entrada "Vão subir" do `bundle.test.ts` não provava nada:** a frase é montada em pedaços
  (`${verbo} subir …`) e não existe em bundle nenhum, então passaria com o `textosDaNuvem.ts` dentro do
  bundle da nuvem. Trocada por "dos últimos 6 meses" e "A lixeira não vai", que existem no bundle
  local (o "precisa ter" do lado local agora as exige); conferido pondo o `textosDaNuvem` e o
  `NuvemNoPC` no bundle da nuvem: o teste falha nos dois casos.

**A F8i (08-out-2026).** O roteiro manual e os documentos. Nenhum código, nenhuma migration, nenhum
teste novo: `go test ./...` e `go vet ./...` como estavam.
- **O roteiro**, `docs/teste-manual-nuvem-f8.md`, no molde do da F7 (o "o que é e por que" no começo,
  um passo por vez com o nome que aparece na tela, os comandos com o caminho completo, um por bloco, o
  `taskkill` com a conferência do `StartTime` e o build do painel num passo à parte). Parte do estado
  real do `bin\` (lido só para leitura em 08-out-2026): o Agent aberto é o da F7c; a nuvem de teste de
  07-out foi desligada e é de memória, então o PC se desconecta sozinho e conecta de novo; o histórico
  antigo nunca subiu (sem o aceite e sem a cópia `antes-da-nuvem`); o `dicionario.json` não tem troca
  de origem `"painel"` (M = 0) e tem 6 grafias do "Adicionar termo". É um PC só, e o "celular" é o app
  da nuvem no navegador dele. O percurso: subir a nuvem vazia; fechar o Agent; montar o painel e o
  Agent; conectar de novo; **aprender `contrato` → `acordo` antes de enviar**, para a pergunta dizer "N
  ditados … e 1 troca aprendida" (D3); enviar o histórico e conferir pela cópia e pelas primeiras linhas
  do app; ver a troca no "Aprendido" do app, e só ela (as grafias do "Adicionar termo" não sobem: D1);
  desfazer no app e ver a troca sumir do "Aprendido no painel" do PC em até 1 minuto, e o ditado
  seguinte sem a troca; o contrário (aprender de novo no PC, ver no app, desfazer no PC, ver sumir do
  app); e a limpeza (a troca de teste não fica nos ditados do `bin\`). O que ele não testa está dito
  no fim dele: dois PCs, o Google, o Postgres, as etiquetas da D4, o "não aplicado neste PC", a troca
  presa a um domínio e o freio (os testes automáticos provam).
- **Os textos velhos** da lista de cima, corrigidos: o `07` (os passos 7 e 9 de "Learning": o prompt
  e o que a F8 decidiu em D1 a D4), o `18` (o vocabulário do ASR) e o `01-intake` (as duas linhas do
  sync). Cada um com a mudança mínima.
- **Os documentos** da tabela de subfatias: o `11` (o log do aprendido, que só estava no `06`), o `20`
  (a linha do AC-021), o `docs/fase-1-cobertura.md` (uma atualização de 08-out-2026 para o AC-021 e o
  AC-022), o README (o "Estado" ainda dizia que o app do celular não existe e que o Agent não
  sincroniza; a Privacidade da nuvem; o layout) e o `08` (uma nota do roteiro). Também a linha da F8
  no `13`, a menção ao roteiro em `scripts/README.md` e no fim do roteiro da F7.
- **Para o Mauro:** rodar o roteiro e confirmar ou mudar as escolhas menores 1 a 8 e os desvios 1 a
  44; continuam pendentes de decisão dele o 3 (o "v2" do nome do id) e o 16 (a remoção de mais de 10
  pares à mão).

### Decisões da F9 (08-out-2026)

O que a F9 decidiu antes de escrever código: as quatro respostas do Mauro, o desenho, as subfatias,
as escolhas menores e os riscos aceitos. O plano da fatia parou em quatro perguntas que só o Mauro
responde (o aprendido desfeito, o `410` no mesmo epoch, a cota de escrita por conta e a lixeira
expurgada sem internet). Ele as respondeu em 08-out-2026, **todas com a recomendação**, e mandou
seguir. O desenho parte do plano e da crítica do mesmo dia: a crítica achou um problema alto (a
lápide vencida decidida por um corte de data) e vários médios e baixos, e as correções já estão no
desenho abaixo. Esta seção nasceu na F9.0 (só documentos). As referências `arquivo:linha` foram
conferidas no código de `2350908` (08-out-2026); os nomes de função e de constante são propostas.

- **D1. Enquanto a conta existir. Confirmada pelo Mauro em 08-out-2026.** A linha do aprendido
  desfeito (a variante e o termo, com `undone_at`) fica na nuvem até o "apagar meus dados"; o job não
  a toca. Por quê: sem a linha o desfazer some da nuvem, e um PC que ainda tem o par o reenvia (com a
  base 0, depois de um `base_invalida`) e a nuvem o cria como novo: a troca desfeita volta a todos
  os aparelhos. O aprendido não tem lápide, `gone` nem `410`, então a ausência não é sinal para o PC.
  O dado é pequeno (duas palavras curtas por linha, só das trocas desfeitas). O custo: o texto da
  troca desfeita continua legível no Postgres e no backup, fora dos 6 meses do ditado, e o texto de
  privacidade diz isso. **A revisão de LGPD da F11 pode mudar:** as outras saídas eram vencer N dias
  depois do desfazer (perde o "desfeito em outro aparelho" de um PC parado) ou guardar só o id (pede
  corrigir a 0016 no lugar, porque ela exige o `term`). **No desenho:** o `Reter` não toca
  `learned_pairs`, e a arrumação dos eventos poupa o do `last_seq` de todo aprendido, o desfeito
  inclusive (a linha continua viva).
- **D2. Segue de onde parou. Confirmada pelo Mauro em 08-out-2026** (era a D5 da crítica). A nuvem
  **não responde `410` no mesmo epoch**, nem pela marca d'água das lápides nem por outro motivo, e o
  PC não muda. O que um PC parado perde é a lápide de um ditado com mais de 6 meses, e esse ditado o
  próprio PC apaga pela retenção dele (F10); enquanto isso a nuvem continua recusando trazê-lo de
  volta (`gone`, `fora_da_janela`). O que fica de pé do contrato é o epoch novo (backup restaurado,
  "apagar meus dados"), que já existe. A linha da F9 em "Fatias", o `05` e o `06` deixam de prometer
  o `410` da marca d'água. Por quê:
  - o `410` no mesmo epoch só volta o cursor do PC a 0 (`internal/sync/motor.go:236-241`;
    `internal/storage/fila_sqlite.go:272-283`), e o PC nunca apaga um ditado por ele faltar na nuvem
    (`internal/sync/decisao.go:179-203`: sem lápide, a ausência não é sinal). A releitura não
    recupera a lápide perdida; quem impede a ressurreição é o `gone`, e ele vale com ou sem `410`;
  - o `410` literal tem dois defeitos de contrato, os dois conferidos no código, que pediriam uma
    mudança no PC e no `06` (um `releitura=1`, e uma chave que lembrasse a releitura entre as rodadas,
    porque uma releitura com mais de 100 páginas não termina numa rodada): o `Receber` pagina com o
    `last_seq` da página anterior como `after_seq`, sempre maior que 0 (`motor.go:430-476`), então
    depois do `410` a página 2 da releitura levaria outro `410` se a marca estivesse acima do fim da
    página 1; e o `last_seq` da última página é o `seq` da última mudança viva
    (`internal/server/pgstore/read.go:70, 167-169`; `cloudstore/memory.go:585-589`), que pode ficar
    abaixo da marca, e cada pull seguinte daria `410`, a cada 60 s. O porteiro dos testes de ponta a
    ponta não pega nenhum dos dois, porque manda o `410` por contador
    (`internal/server/integration/sync_harness_test.go:110-126`);
  - a releitura do 0 traria de volta, ativa, a lixeira expurgada sem internet (a D4 abaixo).

  **A marca d'água não ganha leitor, e o job não a sobe.** A coluna `tombstone_watermark` (0010) fica
  como está: o "apagar meus dados" continua a subi-la ao `last_seq` (`pgstore/contas.go:81-85`;
  `cloudstore/memory_contas.go:380-381`), o `CHECK (tombstone_watermark <= last_seq)` vale, e ninguém
  a lê. Subi-la no job seria uma escrita a mais sem efeito. O PC continua a tratar qualquer `410`
  como "releia do 0" (`internal/sync/cliente.go:259-260`), por robustez, e o porteiro dos testes o
  continua a mandar. **Se o `410` um dia voltar** (a F11, com o backup restaurado, pode pedir), os
  dois defeitos acima são a lista do que o desenho precisa resolver junto.
- **D3. Fica para a F11. Confirmada pelo Mauro em 08-out-2026.** A cota de escrita por conta (um teto
  do que a conta pode mandar à nuvem, somando os PCs) sai da F9. Os três lugares da ADR que a davam
  à F9 (a "revisão da F2", a "revisão da F3" e o "Para as fatias seguintes" da F2) e o `09` passam a
  dizer F11. A F9 entrega a retenção e a arrumação; continua o limite por aparelho do push (60
  pedidos e 5.000 mutações por minuto). Por quê: não há número escolhido nem dado real antes da F11;
  é uma pessoa só; a arrumação já limita o acúmulo; e um teto chutado agora pode travar o envio
  inicial de dois PCs ao mesmo tempo. Na F11, com o proxy, decide-se com o uso real. O risco que
  fica: um token de PC com defeito ou roubado ainda enche `applied_mutations` a 5.000 mutações por
  minuto; a arrumação da F9 limita o que sobra, e não o ritmo.
- **D4. Corrigir na F10. Confirmada pelo Mauro em 08-out-2026.** O ditado que a pessoa pôs na lixeira
  com o PC sem internet, e que o PC apagou de vez depois dos 30 dias antes de a nuvem saber (o
  expurgo apaga a linha e o marcador na mesma transação, `internal/storage/transcription_sqlite.go:
  502-524`, e roda antes do primeiro sync da subida, `cmd/agent/main.go:1186`), continua ativo na
  nuvem. A correção fica para a F10, que é a fatia que mexe na retenção do PC: **o PC só apaga de
  vez da lixeira o que a nuvem já soube.** Até conectar e subir, o ditado fica na lixeira do PC
  além dos 30 dias; ao subir, a nuvem o põe na lixeira, onde ele **fica 24 h na lixeira da nuvem
  e então sai**, com lápide (o piso da lixeira que chega pelo push, a D5 abaixo; era "vencido, o
  job o apaga com lápide", em até 1 h, até a decisão do Mauro de 08-out-2026). A F10
  desenha o resto (o que é "já soube": `server_version` e sem marcador pendente; o que fazer com um
  PC que nunca conecta). **Com a D2, a F9 não muda a premissa do risco aceito na F7c** ("a lixeira
  vencida no PC sem rede"): sem o `410`, nenhuma releitura do 0 acontece no mesmo epoch; só o epoch
  novo (um backup restaurado, F11) ou o "Esquecer esta nuvem" leem do 0. O risco fica registrado
  aqui, nos riscos abaixo e na linha da F10.
- **D5. Fica 24 h na lixeira. Decidida pelo Mauro em 08-out-2026** (a proposta da revisão da F9:
  "A revisão da F9", o relógio do PC atrasado). **O piso do vencimento da lixeira que chega pelo
  push:** o `purge_at` nunca antes da **chegada** (o relógio do servidor no push) **mais 24 h** (a
  mesma `FolgaDaLapide`), para o ditado aparecer na lixeira da nuvem e do celular, restaurável,
  antes de sumir. Por quê: a lixeira com o `deleted_at` antigo (o PC que ficou sem internet, a D4;
  o relógio do PC atrasado, com o `deleted_at` ainda depois do `created_at`) nascia vencida, e o job
  a apagava em até 1 h, com a lápide que faz todos os PCs apagarem a cópia, sem ela passar pela
  lixeira restaurável; do lado da nuvem os dois casos não se distinguem. **Como funciona** (feito
  em `851bed6`):
  - `VencimentoDaLixeira(created_at, deleted_at, chegada)` (`cloudstore/retencao.go`) é o
    `purge_at` que o push e o CRUD gravam: `FimDaLixeira` (os 30 dias do `deleted_at`, nunca antes
    do `created_at`, ou os 6 meses do ditado, o que vier primeiro), nunca antes de `chegada + 24 h`;
  - **a janela de 6 meses ganha do piso:** o piso nunca passa do fim da janela do ditado. O ditado
    que chega à lixeira perto dela vence com ela, como antes (com lápide até a folga de 24 h depois
    da janela, sem ela depois). Assim o `purge_at` também nunca passa da janela, e o pré-filtro do
    SQL (`purge_at <= now`) traz todo item que ela venceu;
  - **o `DecideRetencao` lê o `purge_at` gravado** (antes recalculava do `created_at` e do
    `deleted_at`), na memória e no Postgres, na leitura sem trava e de novo sobre a linha travada;
    um `purge_at` nulo na lixeira (o CHECK da 0003 o permite; o código nunca o grava) conta
    `FimDaLixeira`, sem o piso;
  - o **`deleted_at` gravado continua o que o PC mandou**; o **restaurar limpa** o `purge_at`, e a
    **lixeira seguinte calcula de novo**, com a chegada dela; o **reenvio** (`duplicate`), a lixeira
    mandada de novo sobre um item que já está nela e o recomeço com a base 0 **não mexem** no piso
    (não gravam a lixeira de novo);
  - a lixeira de sempre não muda: os 30 dias contam do `deleted_at`, e o piso só pesa quando faltam
    menos de 24 h para eles. A lixeira do celular (o CRUD) é feita agora, então o piso nunca pesa
    nela;
  - **onde ela aparece** (a revisão da D5): a lixeira do `GET /transcriptions?trash=true` é ordenada
    pelo `deleted_at`, decrescente (`memory.go`, `List`; `pgstore/read.go`, `ORDER BY deleted_at
    DESC`), e o `deleted_at` é o que o PC mandou. A lixeira antiga chega, então, sempre como a mais
    antiga: **no fim da lista**, depois das lixeiras recentes (numa lixeira de mais de 50 itens, na
    página seguinte), e, no painel, agrupada no dia antigo (`agruparPorDia` com `deleted_at`), e não
    no topo. Cumpre o "aparecer, restaurável", mas é fácil não vê-la nas 24 h. Pô-la no topo pediria
    outra ordem, por um campo do servidor: é uma decisão nova do Mauro, não desta fatia. Prova:
    `Retencao_ALixeiraAntigaQueChegaApareceNoFimDaLixeira` (`contracttest/retencao.go`), que falha
    com a lixeira ordenada pela chegada (`updated_at`) ou pelo `created_at`;
  - **o PC não muda.** O PC que recebe essa lixeira pelo pull conta o prazo dele do `deleted_at`
    (`fila_sqlite.go`, `gravarRemoto`: o `purge_at` local já nasce no passado) e expurga a cópia em
    até 1 h (o expurgo do PC roda na subida e de hora em hora, `internal/agent/expurgo`), como antes
    (F10). Nas 24 h, então, o ditado só está na nuvem e no celular: a lixeira do painel local desse
    PC já não o mostra, e só dá para restaurá-lo pelo app da nuvem ou pelo celular. Restaurar o traz
    de volta a esse PC pelo pull (o `gravarRemoto` o insere de novo, porque a linha não existe
    mais).
  **Provas** (falham contra o código de antes): as regras puras (`TestVencimentoDaLixeira_
  OPisoDaChegada`, `TestDecideRetencao_LeOPisoGravado`, e o fuso trocado com a janela que ganha) e
  cinco casos de contrato, na memória e no Postgres (`contracttest/retencao.go`): a lixeira antiga
  fica 24 h na lixeira da nuvem e do celular e então sai, com lápide, e a legítima conta os 30 dias
  do `deleted_at`; o restaurar nas 24 h a traz de volta, e a lixeira nova calcula de novo; a lixeira
  nova posta no meio do `Reter` (o gancho), decidida de novo sob a trava com o piso; a janela que
  ganha do piso (com lápide na janela; sem ela 24 h depois); o reenvio que não mexe no piso. O
  `TestPostgres_Retencao_OsQueFicamNaoVoltamNaMesmaRodada` passou a mandar a lixeira de k 24 h
  antes do `now` (para ela continuar vencida na rodada 1 e o teste continuar a matar o cursor que
  relê). **Os de Postgres não rodaram** (o arquivo do Postgres de teste não existe nesta máquina):
  entram na rodada antes do merge.

**O que se reaproveita de `internal/retention`.** Só a política: `ActiveExpiryAt` e
`EffectiveTrashPurgeAt` (`internal/retention/policy.go:32-61`), que o `cloudstore` já usa
(`OutOfWindow` e `purgeAt`, `internal/server/cloudstore/rules.go:174-192`). O `Job`
(`internal/retention/job.go:82-193`) não serve à nuvem: trabalha sobre listas globais, apaga por id
sem `user_id` (a PK na nuvem é `(user_id, id)`), decide fora de qualquer trava e não conhece lápide,
`seq`, evento nem contador. "O job reaproveita `internal/retention`" (em "Retenção pelo `created_at`
do PC") quer dizer isso. O `Job` fica onde está, para o PC usar na F10. **[Desvio da F10.0
(08-out-2026): o PC não usa o `Job`; a F10b escreve a regra dele, atrás do portão, e reaproveita só a
política e o corte. Ver "Decisões da F10".]**

**O `Reter`, por usuário e com o relógio de parâmetro.** Dois métodos novos em `cloudstore.Store`,
nas duas implementações: `Reter(ctx, userID, now) (Retido, error)` e `UsuariosDaRetencao(ctx)
([]string, error)` (lê `user_sync_counters`). Por usuário porque a suíte de contrato divide um Store
(e um banco) entre uns 45 casos, e uma retenção global com o relógio adiantado apagaria o dado dos
outros; e porque o usuário é a unidade da trava do contador. O `now` é parâmetro, como em todo
método do Store (nunca o `now()` do SQL). **Idempotente** (NFR-013): a segunda rodada não acha nada,
não grava e não gasta `seq`. `Retido` devolve só contagens.

**Os candidatos: lidos sem trava, decididos em Go, decididos de novo sob a trava.**
- Leitura em lotes de 500, **sem trava**, por cursor `(created_at, id)` maior que o último lido (os
  500 que ficam não voltam em toda leitura). O pré-filtro do SQL só pode trazer candidatos a mais:
  o ativo por `created_at <= now.AddDate(0, -6, 0)` e a lixeira por `purge_at <= now` (o `purge_at` já
  sai de `EffectiveTrashPurgeAt`, em Go: `rules.go:186-192, 302, 332`). Nunca `interval '6 months'`
  no SQL: o Postgres prende o fim do mês (31/ago + 6 meses = 28/fev) e o `AddDate` o empurra para a
  frente (3/mar), então decidir por uma data no SQL apagaria até três dias antes
  (`internal/retention/job.go:148-163` explica, e o `regression_test.go` dele fixa o defeito). O corte
  `AddDate(0, -6, 0)` nunca cai antes do `created_at` de um item vencido (o `AddDate` só empurra para
  a frente); a propriedade é provada por teste, hora a hora, por quatro anos.
- Cada linha lida é decidida em Go (`DecideRetencao`), com as datas em **UTC**: o pgx devolve
  `timestamptz` no fuso local (`pgstore/store.go:241-243`) e o `AddDate` depende do fuso, enquanto o
  push e o PC contam em UTC. Teste com `time.Local` trocado para um fuso positivo.
- **Só o que parece vencido** passa pelo `gravar` (`store.go:281-309`: o contador sobe primeiro, a
  linha vem `FOR UPDATE`) e é **decidido de novo sobre a linha travada**: o ditado restaurado ou
  mudado entre a leitura e a trava fica (AC-018). É o que o `Job` antigo não faz.
- **Um item por transação.** Um lote grande numa transação só seguraria a trava do contador, e o push
  e o CRUD do mesmo usuário esperariam (os prazos do servidor são 30 s e 60 s,
  `cmd/server/serve.go:35-40`).

**As três decisões, linha a linha** (`DecideRetencao` e `LapideVencida`, puras):
- **O ativo** (fora da lixeira) com `!now.Before(ActiveExpiryAt(created_at))` sai **sem lápide e sem
  evento**, e o `seq` volta (`gravar` com `wrote=false`). A recusa por janela já cobre (`rules.go:
  201-210, 295-297`). É de propósito: uma lápide faria o PC apagar a cópia (`gone` `apagado`), e o PC
  mantém o que saiu da janela até a F10 (`fora_da_janela`). O evento dele fica órfão e a arrumação o
  leva.
- **A lixeira** com `!now.Before(EffectiveTrashPurgeAt(created_at, deleted_at))` **[desde a D5, de
  08-out-2026: `!now.Before(purge_at)`, o vencimento gravado, com o piso da chegada]**: se ainda
  **dentro da janela** (`DeixaLapide`: `!OutOfWindow(created_at, now - 24 h)`), apaga o ditado e grava
  a **lápide** (`dictated_at` = o `created_at`) e o **evento `tombstone`**, com `device_id` nulo e a
  versão seguinte (`version + 1`), no molde do `Purge` (`pgstore/store.go:512-540`;
  `cloudstore/memory.go:386-406`); se não (venceu pelos 6 meses, há mais de 24 h), só o `DELETE`. O
  "excluir definitivamente" do CRUD passa pelo mesmo ajudante, então fora da janela também não deixa
  lápide (ela nasceria vencida).
- **A lápide** vence quando `LapideVencida(dictated_at, now)` = `OutOfWindow(dictated_at, now -
  24 h)`: vive a janela inteira mais um dia.

**A folga de 24 horas é a mesma em toda decisão do limite dos 6 meses** (o mesmo número do
`FutureTolerance`, `cloudstore/types.go:203-209`; proposta: `FolgaDaLapide`). A lápide vive um dia a
mais que a janela para um relógio do servidor que volte atrás por menos de um dia não abrir brecha
de recriação, e a lápide só nasce quando não nasceria vencida (`DeixaLapide` = `!LapideVencida`). O
ativo vencido é a exceção, porque não tem lápide (acima): se o relógio recuar, um `create` com a base
0 do ditado que o job acabou de apagar volta a ser aceito, e a rodada seguinte o apaga de novo, sem
lápide e sem nada perdido (risco aceito, abaixo).

**A lápide vencida é decidida linha a linha em Go, nunca por um corte de data no SQL** (o problema
alto da crítica). Numa transação só:
1. trava o contador (`SELECT ... FROM user_sync_counters WHERE user_id = $1 FOR UPDATE`): a ordem de
   travas é fixa (contador, entidade, evento), o que evita o deadlock com o `gravar`, e um push do
   mesmo usuário espera em vez de ver a lápide sumir no meio;
2. lê `entity_id`, `dictated_at` e `seq` das lápides do usuário com um pré-filtro grosseiro
   (`dictated_at <= (now - 24 h).AddDate(0, -6, 0)`, em lotes, por cursor; índice `idx_tombstones_
   dictated_at`) que só traz candidatos a mais;
3. filtra em Go com `LapideVencida`, com `dictated_at` em UTC;
4. `DELETE ... WHERE user_id = $1 AND entity_type = 'transcription' AND entity_id = ANY($2)`, por
   chave. **A marca d'água não sobe** (D2).

**[A revisão da F9, 08-out-2026: "numa transação só" virou uma transação por lote. Cada lote de
lápides (e, na arrumação, cada lote de eventos e de respostas) é uma transação curta que trava o
contador, faz os passos 1 a 4 e confirma; o cursor passa de um lote para o outro. Ver "A revisão da
F9".]**

Um corte de uma data só apagaria a lápide cedo no fim do mês, e o ditado apagado voltaria. O ditado
de 31/ago/2026 12:00Z sai da janela em 03/mar/2027 12:00Z (o `AddDate` empurra o 31/fev para 3/mar), e
a lápide dele deve viver até 04/mar 12:00Z. Com o corte calculado em Go ela sumiria em 02/mar, e com
o `interval` do Postgres em 01/mar. Nesse intervalo um envio com a base 0 (por exemplo, o envio
inicial depois de "Esquecer esta nuvem" e reconectar à mesma conta) seria aceito como ditado novo
(`rules.go:201-210, 287-310`) e desceria pelo pull aos PCs que já o tinham apagado
(`decisao.go:196-203`); o job o apagaria de novo, sem lápide, porque já estaria fora da janela, e os
PCs ficariam com ele até a F10. Fere o AC-061.

**A arrumação** (dentro do `Reter`, depois do resto):
- **`sync_events` órfãos:** apaga o evento com `changed_at` mais velho que **90 dias** que **não é** o
  `last_seq` de um ditado nem de um aprendido vivo do mesmo usuário (`NOT EXISTS` nos dois `UNIQUE
  (user_id, last_seq)`, que servem de índice). O pull junta a entidade ao evento dela por `(user_id,
  last_seq)` com `LEFT JOIN` para dar o `device_id` de origem (`pgstore/read.go:33-54`; o aprendido
  não tem coluna de aparelho): apagar o evento de uma entidade viva some com a origem em silêncio
  (o aviso da 0008, linhas 29-46). Os eventos de lápide e os de entidade que o job apagou são
  órfãos e saem. O pull não lê evento nenhum de outro jeito, então a idade deles não importa para o
  sync.
- **`applied_mutations`:** apaga as respostas com `applied_at` mais velho que `TokenSemUso` + 10 dias
  (**190 dias**), **derivado da constante** (`cloudstore/pareamento.go:23-27`), nunca menos. Um PC que
  volta dentro da vida do token (180 dias contados do último uso) reenvia os mesmos `mutation_id` do
  lote em voo e tem de receber `duplicate`; depois dela, a reconexão esquece esses envios
  (`fila_sqlite.go:256-265`), e o mesmo `mutation_id` não volta. **[Corrigido na revisão da F9,
  08-out-2026: este parágrafo dizia que o reenvio depois do prazo grava uma versão nova e pode dar um
  selo à toa. O teste da F9c (`contracttest/arrumacao.go`) prova outra coisa.]** Um reenvio que
  chegasse depois do prazo seria uma mutação nova: o do **ditado** dá `gone` `fora_da_janela`, porque
  190 dias já passam da janela dele (6 meses são no máximo 184 dias, e o `created_at` pode estar até
  24 h à frente do envio, o `FutureTolerance`: uns 185 dias no total), e não grava nem dá selo; o do
  **aprendido**, que não tem janela nem selo, segue a tabela do `DecideLearned` (o mesmo par dá
  `applied` com a versão atual, sem gravar; o par que outro aparelho mudou no meio dá `conflict` com o
  `state`, e o PC segue o da nuvem; o desfazer reenviado desfaria de novo o par que outro aparelho
  reaprendeu). O prazo precisa cobrir a vida do token (é ela que vale para o aprendido) e, para o
  ditado, a janela mais a folga: abaixo disso, o reenvio de um ditado ainda na janela, com a base
  velha, gravaria uma versão nova (`rules.go:259-276`) e, com o texto mudado no meio por outro
  aparelho, daria o selo falso da F7. A tabela não guarda texto, então não há pressão de privacidade
  para encurtar. (O "minutos ou horas, não em meses" das "Decisões da F2" fica corrigido: valia para
  a resposta que chega, e não para o PC que fica fora.)
- O índice `(user_id, changed_at)` entra **dentro da 0008**, corrigida no lugar (a regra vale até o
  deploy, F11), com o `schema_test.go` e o `postgres_test.go`. `applied_mutations` já tem o
  `idx_applied_mutations_applied_at` (0009, linhas 77-80). Na memória, `Event` ganha `ChangedAt`, e
  `put`, `putLearned` e `Purge` passam a receber o relógio.

**Onde roda.** No processo do `serve`, na subida e **de hora em hora**: o `ArrumarSempre`
(`internal/server/nuvem.go:295-309`, que já roda a arrumação das sessões) passa a chamar também o
`Reter`. Sem subcomando nem cron: o deploy da F11 é um compose sem agendador. A hora cumpre o
"diário" da spec e limita a 1 h o tempo em que um item vencido continua visível e restaurável (o
`List` e o `Restore` não olham o vencimento: `rules.go:473-482`, `pgstore/read.go:217-222`); a subida
cobre o backup restaurado. O `servir` (`cmd/server/serve.go:127-160`, o mesmo do servidor de teste,
`teste.go:114`) passa a **esperar o goroutine** antes de logar "desligado" (hoje não espera:
`serve.go:139, 157-158`), porque o job agora escreve e o pool fecha por baixo.

**O que o job loga.** Uma linha INFO `retenção` por rodada, só contagens, **mesmo com zero** (o
batimento do alerta "retention job missed" da F11): `usuarios`, `ativos`, `lixeira`,
`lapides_criadas`, `lapides_vencidas`, `eventos`, `mutacoes`, `falhas` e `ms`. No erro, só o
`DBError` (a operação e o SQLSTATE, `pgstore/store.go:83-116`) ou a categoria (`contexto`, `outro`):
nunca `user_id`, texto, e-mail nem o erro cru do driver. `retention_purged_total` passa a ser essa
linha (não há infraestrutura de métricas no servidor). **[A revisão da F9, 08-out-2026: a linha
`retenção` sai em toda rodada, até com a listagem dos usuários falhando (`usuarios=0`, `falhas=1`) ou
com todo usuário falhando, então a ausência dela não pega o job quebrado. O alerta da F11 é duplo: a
ausência da linha `retenção` e a linha `retenção falhou` com `categoria` `banco` ou `outro` (a
`contexto` é o desligamento). O batimento exige `LOG_LEVEL` em `info` ou `debug`. O `11` tem o
texto inteiro.]**

**Sem configuração.** `RETENTION_MONTHS` e `TRASH_DAYS` saem do `10` e do `.env.example` (escolha
menor 3): o código nunca as leu, e os prazos são constantes da política, iguais no push, no job e no
PC.

**Os testes.** Nenhum age no sistema real; o Postgres é só o de teste.
- **Suíte de contrato** (`internal/server/contracttest`; memória sempre, Postgres com o contêiner):
  `w.reter()` chama o `Reter` para os usuários do próprio caso, com `w.clk.Now()`, e os dados levam
  datas explícitas, nunca `now()`. Quando o relógio anda meses: o token do PC vale 180 dias sem uso,
  então há um pull no meio (`suite.go:597-602`); e a sessão do celular vence com 30 dias sem uso e
  nunca passa de 90 (`internal/server/nuvem.go:31-34`; `world.go:142-151` cria a `celA` com
  `now0 + SessaoSemUso`), então os casos criam uma sessão nova com o relógio já avançado, ou
  conferem pelo `Store`, e nunca pela `celA` velha (que daria `401`).
- **Postgres** (`pgstore/retencao_pg_test.go`): a corrida restaurar × reter, por um gancho entre a
  listagem e a trava; o push do mesmo usuário durante o `Reter`, sem deadlock; o "apagar meus dados"
  correndo com o job (o `CHECK (tombstone_watermark <= last_seq)` e o `GREATEST` com conjunto vazio).
- **Servidor de teste** (`MemoryStore`, o mesmo `servir`, sem Docker): a linha de log na subida e o
  desligamento esperando o goroutine; e o `Reter` com o `ctx` cancelado no meio.
- **Ponta a ponta do PC** (`internal/server/integration`): o `nuvemE2E` ganha o relógio injetado
  (`CloudConfig.Now`), o mesmo do Motor do PC. Só teste: o PC não muda.

**O que a F9 prova do AC-061, e o que não.** Prova o "PC parado não ressuscita" e a recusa por
janela. O "backup restaurado" (epoch novo) só se prova na F11, e o AC-061 não é dado como fechado
na F9.

**As subfatias.** Cada uma com o seu commit, `go test ./...` e `go vet ./...` verdes, sem `git push`.
Os testes de Postgres só rodam com o contêiner de teste que o Mauro sobe; sem ele pulam, e cada
entrega diz quais não rodaram.

| Subfatia | O que entrega | O que prova |
|---|---|---|
| F9.0 | esta seção e as emendas (`03`, `04`, `05`, `06`, `09`, `11`, `13`) | nada de código; `go test ./...` e `go vet ./...` como estavam |
| F9a | as regras puras, em `internal/server/cloudstore/retencao.go`: `DecideRetencao` (fica, apaga sem lápide, apaga com lápide), `DeixaLapide`, `LapideVencida`, os cortes grosseiros do SQL, e as constantes `FolgaDaLapide` (24 h), `PrazoDosEventos` (90 dias) e `PrazoDasMutacoes` (`TokenSemUso` + 10 dias). Nada passa a apagar | a fronteira exata e 1 µs antes; 31/ago vence em 3/mar e a lápide dele em 4/mar, e o 29/fev, nos três (ativo, lixeira e lápide); a lixeira em D+29 fica e em D+30 sai; a sobreposição (30 dias contra os 6 meses); o relógio que anda para trás; a propriedade do pré-filtro (para cada hora de quatro anos, todo item vencido, ativo ou lápide, está dentro do corte) e `LapideVencida(d, now)` implica `OutOfWindow(d, now)`; as datas em UTC com `time.Local` trocado para um fuso positivo; `PrazoDasMutacoes` nunca abaixo de `TokenSemUso` + 10 dias (o teste falha se alguém baixar a constante); mutações nas fronteiras |
| F9b | o job no Store, nas duas implementações: `Reter` (o ativo, a lixeira e as lápides) e `UsuariosDaRetencao` em `cloudstore.Store`; o "excluir definitivamente" do CRUD pelo mesmo ajudante. Sem a arrumação | na suíte de contrato, em `contracttest/retencao.go`: o ativo com 6 meses sai e 1 s antes fica; a lixeira em D+29 fica e em D+30 sai com lápide, evento e `seq`; a sobreposição, sem lápide quando venceu pelos 6 meses há mais de 24 h; a lápide só some depois da janela mais um dia (o ditado de 31/ago reenviado com a base 0 dá `gone` `apagado` em 3/mar e `fora_da_janela` em 4/mar); lápides que saem fora de ordem de `seq`; a segunda rodada não faz nada e não gasta `seq`; o mesmo id em duas contas (reter A não toca B); **PC parado:** com a lápide viva, `gone` `apagado`, com a vencida, `fora_da_janela`, e o ativo que a nuvem apagou também `fora_da_janela`, e nada volta à nuvem; o `Reter` com o `ctx` cancelado no meio para entre itens e não deixa meia-purga. No Postgres: a corrida restaurar × reter (o item fica); o push durante o `Reter`, sem deadlock; o "apagar meus dados" correndo com o job; o fuso trocado |
| F9c | a arrumação dentro do `Reter`: os `sync_events` órfãos com mais de 90 dias e as `applied_mutations` com mais de `TokenSemUso` + 10 dias; o índice `(user_id, changed_at)` na 0008, corrigida no lugar, e os comentários velhos da 0008, da 0010 e da 0015 (o `410`, "o job sobe a marca d'água"); na memória, `Event.ChangedAt` | o evento que é o `last_seq` de um ditado ou de um aprendido vivo (o desfeito inclusive) sobrevive, e o `device_id` de origem continua no pull; o evento órfão velho, o de lápide e o de entidade que o job apagou somem; 89 dias fica e 90 sai; a resposta com 189 dias fica (o reenvio dá `duplicate`) e com 191 some; depois do prazo, o reenvio do ditado dá `gone` `fora_da_janela` (190 dias passam da janela) e o do aprendido, sem outra mudança, `applied` com a versão atual, sem gravar (corrigido na revisão da F9: dizia "`applied` mesclado, com versão nova e sem selo", o que o código não faz); `migrations/schema_test.go` e `postgres_test.go` com o índice. Os bancos dos testes são novos (um por teste: `pgtest.go:12, 63-100`); só um banco persistente, o do `nuvem-local.ps1`, precisa ser recriado |
| F9d | o job no `serve`: `Cloud.Reter(ctx)` percorre `UsuariosDaRetencao` e grava a linha `retenção`; o `ArrumarSempre` o chama na subida e de hora em hora; o `servir` espera o goroutine; o `nuvemE2E` com o relógio injetado | pelo servidor de teste (MemoryStore, o mesmo `servir`, sem Docker): a linha na subida só com as chaves permitidas, sem id nem texto, mesmo com zero; o erro de um Store que falha só com a categoria; o goroutine termina antes de "desligado"; o `Reter` com o `ctx` cancelado no meio. Ponta a ponta, com o Motor do PC de verdade, SQLite temporário e `httptest`: o PC parado por meses com uma edição pendente num ditado cuja lápide ainda vive apaga a cópia (`gone` `apagado`); com a lápide vencida, mantém a cópia, marca `fora_da_janela` e nada volta à nuvem; o ativo que a nuvem apagou fica no PC. `serve_pg_test` confere a linha com o contêiner. Nenhuma linha do PC muda |
| F9e | os documentos: o `10` e o `.env.example` (saem `RETENTION_MONTHS` e `TRASH_DAYS`), o `13`, o README, o estado desta seção, e os comentários de teste que dizem que "o servidor só manda o `410` na F9" (`sync_harness_test.go:122-123`, `sync_e2e_test.go:414`) | `go test ./...` e `go vet ./...`; os documentos relidos contra o código |

**O que ficou de fora.**
- O "correction FK set null" da TASK-020 não tem objeto na nuvem: a 0006 já faz `ON DELETE SET NULL`,
  e a tabela não é usada (a `Correction` com a frase inteira não é guardada nesta etapa). Fica no
  relatório da F9.
- O giro do backup e a revisão de LGPD são da F11; o job não alcança o backup, e o texto de
  privacidade diz isso.
- O que a crítica pediu para o `410` (a prova de que ele não cria selo, o PC que reconecta depois do
  "apagar meus dados", a ordem de atualização do Agent antes da nuvem, o envelope do erro) saiu com
  ele (D2). Ficaram os casos da corrida do job com o "apagar meus dados" e do AC-018.

**Escolhas menores, a confirmar pelo Mauro na entrega** (como nas F4 a F8: a implementação usa a
proposta até ele dizer o contrário):
1. **Os dois prazos da arrumação, separados:** **90 dias** para o `sync_events` órfão (o pull não o
   lê; é o número que a spec já sugeria como suposição) e **190 dias** (`TokenSemUso` + 10) para as
   `applied_mutations`, porque ali o prazo tem de cobrir a vida do token. As alternativas: 30 dias ou
   1 ano para os eventos; para as respostas, um prazo abaixo da vida do token (180 dias) faria o
   reenvio de um PC que volta entre o prazo e os 180 dias ser uma mutação nova: no ditado ainda na
   janela (até uns 185 dias, contando a folga de 24 h do futuro), uma versão nova e, com o texto
   mudado no meio, o selo falso da F7; no aprendido, um `conflict` à toa ou o desfazer de novo do par
   que outro aparelho reaprendeu. Passada a janela, o reenvio do ditado já dá `gone`
   `fora_da_janela`, com ou sem a resposta guardada (corrigido na revisão da F9).
2. **O job de hora em hora**, no lugar do "diário" da spec: o item vencido fica visível e
   restaurável por no máximo 1 h, e o job roda na subida. A alternativa: diário (até 24 h).
3. **Tirar `RETENTION_MONTHS` e `TRASH_DAYS` dos documentos** (o `10`, inclusive o exemplo dele, e o
   `.env.example`): o código nunca as leu, e configurá-las faria a nuvem e os PCs divergirem na
   janela. A alternativa: o job, a regra da janela do push e o envio inicial do PC passarem a
   honrá-las, os três juntos.
4. **A marca d'água sem leitor e sem o job** (decorre da D2): a coluna fica, e o "apagar meus dados"
   continua a subi-la. A alternativa: o job a sobe a cada lápide vencida, para o dia em que houver
   um leitor.
5. **O lote de 500 e um item por transação**, sem `pg_try_advisory_lock` entre dois `serve` ao mesmo
   tempo: o `Reter` é idempotente e decide de novo sob a trava, então dois processos não duplicam
   nada. A F11 pode acrescentar o advisory lock se o deploy tiver sobreposição.

**Confirmadas pelo Mauro em 08-out-2026** ("Confirmo todas"), na entrega da F9, como lhe foram
apresentadas: os 90 dias do `sync_events` órfão e os 190 dias (`TokenSemUso` + 10) das
`applied_mutations` (1); o job de hora em hora, na subida e a cada hora (2); tirar `RETENTION_MONTHS`
e `TRASH_DAYS` dos documentos (3, já feito na F9e); a marca d'água sem leitor e sem o job (4); e o
lote de 500, um item por transação, sem o advisory lock (5). Na mesma conversa ele decidiu o piso da
lixeira que chega pelo push ("Fica 24 h na lixeira", a D5 acima). Nenhuma das cinco mudou código.

**Riscos aceitos.**
- **O restaurar feito num PC parado perde para a lápide.** Um ditado que outro aparelho pôs na lixeira
  e que o PC parado restaurou, dentro dos 30 dias, mas que só sobe depois de a lixeira vencer na
  nuvem (o job já deixou a lápide), recebe `gone` `apagado` no push, e o PC apaga a cópia e o
  restaurar: "apagar vence" (`rules.go:198-199`). Só acontece com um PC que fica mais de 30 dias sem
  conectar.
- **O AC-018 passa a valer "até 30 dias + até 1 h"** na nuvem: o job é de hora em hora, e o `List` e
  o `Restore` não olham o vencimento.
- **O celular mostra menos histórico que os PCs.** A nuvem apaga o ativo aos 6 meses, e os PCs o
  mantêm até a F10 (`fora_da_janela`). Já estava decidido acima; a pessoa vai notar com dado real
  (F11).
- **Um PC parado perde a lápide** de um ditado com mais de 6 meses (D2): ele continua a mostrar o
  que foi apagado no celular, até a retenção dele (F10). A nuvem não o traz de volta.
- **Relógio do servidor.** A folga de 24 h cobre as lápides. O ativo vencido não tem lápide: com o
  relógio recuado, um `create` com a base 0 do ditado recém-apagado volta a ser aceito, e a rodada
  seguinte o apaga de novo. Nada se perde (sem lápide, nenhum PC apaga a cópia). Um relógio adiantado
  apagaria cedo: o servidor segue o NTP do host, e o roteiro da F11 confere.
- **Texto fora do alcance do job:** o backup da nuvem até a cópia girar (F11); as cópias
  `antes-da-nuvem` e `antes-do-dono` no PC (F10); e a linha do aprendido desfeito (D1). O texto de
  privacidade e a revisão de LGPD da F11 precisam dizer isso.
- **Job que falha em silêncio retém texto além do prazo.** Não há métrica nem `health` do job, só a
  linha `retenção`, mesmo com zero, e a `retenção falhou`. O servidor continua "ready". **[A revisão
  da F9: a F11 monitora as duas, e não só a ausência da `retenção`, que sai em toda rodada, mesmo com
  `falhas` acima de zero: o alerta dispara com a ausência dela por mais de 2 h, ou com uma
  `retenção falhou` de `categoria` `banco` ou `outro`. Com `LOG_LEVEL` em `warn` ou `error`, a
  `retenção` some e o primeiro alerta dispara à toa: o servidor da F11 roda em `info`.]**
- **O backup restaurado ressuscita texto vencido** até a primeira rodada. O job roda na subida; a
  ordem "restaurar, rodar a retenção e só então abrir ao tráfego" é do roteiro da F11.
- **A lixeira expurgada no PC sem internet** (D4): continua o risco aceito da F7c até a F10. **[Resolvido
  na F10a: a guarda da D3; ver "Decisões da F10".]**
- **A lixeira que chega pelo push vive até 24 h além dos 30 dias** (D5, 08-out-2026): o texto que a
  pessoa pôs na lixeira há mais de 30 dias fica na nuvem até 24 h depois de chegar, restaurável no
  celular; nunca passa da janela de 6 meses. O PC que a recebe pelo pull ainda a expurga em até 1 h
  (conta do `deleted_at`; o expurgo do PC roda na subida e de hora em hora, `internal/agent/expurgo`),
  até a F10: nas 24 h, só dá para restaurá-la pelo app da nuvem ou pelo celular, e restaurar a traz
  de volta a esse PC pelo pull (o `gravarRemoto` a insere de novo). **[A F10 não muda isso: essa
  lixeira veio da nuvem, que já sabe dela, e a guarda da D3 não a segura.]**
- **A cota por conta** (D3): um token de PC com defeito ou roubado ainda enche `applied_mutations`
  a 5.000 mutações por minuto até a F11.
- **O SQL só tem prova real com o contêiner.** A F9 é pesada em SQL (as travas, a decisão sob `FOR
  UPDATE`, o `NOT EXISTS`, o rollback). Sem o Docker Desktop aberto e o
  `scripts/postgres-de-teste.ps1` rodado pelo Mauro, a prova é só da memória. **Recomendo não juntar
  a F9 à `main` sem essa rodada.** Nas F6 e F8 a rodada ficou pendente por dias.
- **O índice novo na 0008 invalida um banco persistente já migrado** (o checksum). Os testes criam um
  banco por teste e não são afetados; só o banco do `nuvem-local.ps1` precisa ser recriado.

**O que a F9.0 não fez:** nenhum código, nenhuma migration, nenhum teste novo. Fica para a F9e o `10`,
o `.env.example` e os comentários de teste; para a F9c, os comentários velhos da 0008, da 0010 e da
0015 (mexer neles muda o checksum, e a regra de corrigir no lugar vale até a F11).

**O estado da F9 (08-out-2026).** Implementada (F9.0 a F9e), revista e fechada no mesmo dia, em
commits locais (`cd7d53d` a `1d9557b`, as subfatias e a revisão; `0996d29`, o fechamento), sem
`git push`; o que a revisão achou e o que foi feito estão em "A revisão da F9", abaixo.
`go test ./... -count=1` (66 pacotes verdes), `go vet ./...` e o `go test -tags logindeteste
./cmd/server/... ./internal/server/...` verdes. Nenhuma dependência nova.
- **O que provou:** o que a tabela das subfatias diz, pela memória: a suíte de contrato
  (`contracttest/retencao.go` e `arrumacao.go`), as regras puras, o servidor de teste e a ponta a
  ponta com o Motor do PC de verdade.
- **O Postgres não rodou.** O arquivo do Postgres de teste não existe na máquina do fechamento (nem
  na da revisão). Sem ele, em `./migrations/`, `./internal/server/...` e `./cmd/server/...`, 213
  testes passam e 71 pulam: todos os de Postgres, entre eles os nove de `retencao_pg_test.go`, o
  `TestContrato_Postgres` (com os casos da retenção e da arrumação), o
  `TestPostgres_IndicesDaRetencao` e o `serve_pg_test`. O SQL da F9 (as travas, a decisão sob `FOR
  UPDATE`, o `NOT EXISTS`, os lotes por cursor, o fuso) só tem prova com essa rodada; continua a
  recomendação de não juntar a F9 à `main` sem ela.
- **A mutação do fechamento.** Uma passada sobre a F9a a F9d, depois da revisão, deixou três vivas não
  equivalentes, as três no `pgstore`, que só o Postgres mata:
  - **O `reterUm` sem a decisão refeita sob a trava.** O teste existe
    (`TestContrato_Postgres/Retencao_ORestauradoNoMeioFica` e
    `TestPostgres_Retencao_RestaurarNoMeioGanhaDaTrava`); falta rodá-lo. A mesma mutação na memória
    (o `Reter` apagando sem conferir o `RetencaoFica` sob o mutex) morre no
    `Retencao_ORestauradoNoMeioFica`, conferida à mão.
  - **O `sqlArrumarEventos` sem o `NOT EXISTS` de `learned_pairs`.** O teste existe
    (`Arrumacao_OEventoDoLastSeqVivoFicaEOOrfaoSai`, com o desfeito no `seq` 5); falta rodá-lo. A
    guarda análoga da memória (o laço sobre `u.learned` em `memUser.arrumar`), que não tinha mutação
    própria, morre em três casos de contrato (os dois da arrumação e um da retenção), conferida à
    mão; os mesmos casos rodam no `TestContrato_Postgres`.
  - **O cursor dos candidatos sem o desempate por `id`.** Morre no `TestPostgres_Retencao_MaisDeUmLote`
    (o `created_at` repetido na divisa do lote). Ficava uma lacuna que nem o Postgres fechava: o
    cursor que não avança e o `>=` no lugar do `>` passavam nele, porque ele apaga todo candidato que
    lê. **Feito** (`0996d29`): o `TestPostgres_Retencao_OsQueFicamNaoVoltamNaMesmaRodada` põe mais de
    um lote de candidatos que o pré-filtro traz e que ficam (o ativo do fim de agosto em 1/mar), com
    um vencido na frente; o gancho do vencido põe na lixeira, já vencida, o último lido do primeiro
    lote, que só sai na rodada seguinte; e a terceira rodada, só com os que ficam, termina. **Não
    rodou** (pula sem o Postgres). As contas das três rodadas e as três variantes do cursor foram
    conferidas num rascunho fora do commit (o `MemoryStore` com o mesmo gancho, e o laço do cursor
    simulado sobre as linhas): com o `>`, só o vencido sai; com o `>=` e com o cursor parado, o
    último lido do primeiro lote sai na mesma rodada, e o parado não termina.
  - **Equivalentes, não são buracos:** o sinal da folga no `CorteDaLapide` (só aumenta o
    pré-filtro) e o `.UTC()` do começo do `DecideRetencao` (as funções que ele chama já convertem).
    O UTC no `OutOfWindow` e no `CorteDoAtivo` morre só no `TestRetencao_EmUTCComOFusoLocalTrocado`:
    na memória o `Reter` normaliza o `now` para UTC e as datas já são gravadas em UTC, então a suíte de
    contrato não chega a ele; a prova ponta a ponta do fuso é o `TestPostgres_Retencao_FusoTrocado`,
    que só roda com o Postgres.
- **Os desvios do desenho**, todos da revisão (abaixo): as lápides e a arrumação numa transação por
  lote, e não numa só; a lixeira contada do `created_at` quando o `deleted_at` vem antes dele
  (`FimDaLixeira`); o `IntervaloDaArrumacao` na `CloudConfig` (zero é a 1 h; o `serve` não muda); e
  o texto do reenvio depois dos 190 dias, corrigido para o que o código faz. O fechamento não mudou
  código de produção, só acrescentou o teste.
- **Para o Mauro:** a rodada no Postgres de teste (`scripts/postgres-de-teste.ps1`) antes do merge;
  ~~confirmar ou mudar as escolhas menores 1 a 5~~ (confirmadas em 08-out-2026, "Confirmo todas");
  ~~decidir o piso do vencimento da lixeira que chega pelo push~~ (decidido em 08-out-2026, "Fica 24 h
  na lixeira": a D5, feita em `851bed6`).
- **Depois do fechamento: a D5 (08-out-2026).** O piso da lixeira que chega pelo push, com as provas
  na D5. `go test ./... -count=1`, `go vet ./...` e o `go test -tags logindeteste ./cmd/server/...
  ./internal/server/...` verdes. Os de Postgres continuam sem rodar (o arquivo do Postgres de teste
  não existe na máquina): entre eles, os cinco casos de contrato novos no `TestContrato_Postgres` e o
  `OsQueFicamNaoVoltamNaMesmaRodada` mexido. Conferido por mutação, na memória: o piso sem a folga,
  sem o teto da janela ou desligado, e a decisão sob o mutex sem o `purge_at`, morrem nos testes
  novos; o `DecideRetencao` que ignora o `purge_at` morre só nas regras puras (na suíte, o pré-filtro
  que lê o `purge_at` o esconde, a não ser pela decisão sob a trava).

### A revisão da F9 (08-out-2026)

Uma revisão do mesmo dia, sobre a F9a a F9d, achou três problemas médios (um de dados, dois de
prova) e seis baixos. O que foi feito, achado por achado (commits `e765969` a `2fd57ea`, sobre
`703587c`). Os testes de Postgres novos ou mexidos **não rodaram**: o arquivo do Postgres de teste não
existe na máquina da revisão; eles pulam no `go test ./...` e entram na rodada que o Mauro faz antes
do merge.

- **Médio: a lixeira com a data antes do próprio ditado vencia na hora.** O push só recusava o
  `deleted_at` no futuro. Um PC com o relógio semanas atrás (sem NTP, uma VM restaurada) que põe na
  lixeira um ditado criado em outro PC manda um `deleted_at` anterior ao `created_at`; o `purge_at`
  nascia no passado, e o job apagava o ditado de vez, com a lápide que faz todos os PCs apagarem a
  cópia, na rodada seguinte: sem os 30 dias para restaurar (AC-018). **Feito** (`e765969`):
  `FimDaLixeira` (`cloudstore/retencao.go`) conta a lixeira do `created_at` quando o `deleted_at` vem
  antes dele. É a mesma função no `purge_at` que o push e o CRUD gravam (o pré-filtro do SQL) e no
  vencimento que `DecideRetencao` confere; o `deleted_at` gravado continua o que o PC mandou. Não
  recusa nada (a recusa `campo_invalido`, a outra saída, recusaria também a lixeira legítima feita
  logo depois de um ditado criado por um PC com o relógio um pouco adiantado, dentro da folga de
  24 h). Provas: `TestFimDaLixeira_NuncaAntesDoDitado` e o caso de contrato
  `Retencao_ALixeiraNaoComecaAntesDoDitado`, que falham contra o código de antes.
  **O que ficou, para o Mauro decidir (mexe na D4):** o relógio atrasado com o `deleted_at` ainda
  **depois** do `created_at` (o PC com o relógio 40 dias atrás põe na lixeira um ditado de 60 dias)
  continua a dar um `purge_at` já vencido, e o ditado sai em até 1 h, com lápide, sem passar pela
  lixeira restaurável da nuvem; o mesmo vale para a edição feita em outro aparelho depois dessa
  lixeira antiga. Do lado da nuvem, isso não se distingue do PC que ficou mesmo sem internet (a D4
  aceita esse caso). A proposta da revisão é um **piso para o vencimento da lixeira que chega pelo
  push**: o `purge_at` nunca antes da chegada mais uma folga (24 h, a mesma `FolgaDaLapide`), para o
  item aparecer na lixeira da nuvem (e do celular) antes de sumir. Pede gravar o piso no `purge_at` e
  fazer o `DecideRetencao` lê-lo (hoje ele recalcula do `created_at` e do `deleted_at`), e muda o
  texto da D4 ("vencido, o job o apaga com lápide" passa a "fica 24 h na lixeira da nuvem e então
  sai"). ~~**Não feito:** é decisão do Mauro.~~ **[Decidido pelo Mauro em 08-out-2026: "Fica 24 h na
  lixeira". Feito em `851bed6`, com a janela de 6 meses ganhando do piso; ver a D5, em "Decisões da
  F9".]** **Também não muda:** o PC que recebe essa lixeira pelo
  pull conta o prazo dele do `deleted_at` (`fila_sqlite.go`, `gravarRemoto`) e expurga a cópia
  ~~na subida seguinte~~ **[em até 1 h: o expurgo do PC roda na subida e de hora em hora,
  `internal/agent/expurgo`; corrigido na revisão da D5]**, antes da F9 também; o PC não muda na F9
  (é a F10). O celular e a nuvem a mantêm restaurável, e restaurar a traz de volta a esse PC pelo
  pull.
- **Médio: nenhuma prova da D2.** O código está certo (nenhum caminho do servidor responde `410`, e a
  marca d'água só sobe no "apagar meus dados"), mas todo pull dos casos da retenção era do cursor 0,
  isento do `410` no desenho antigo, e o ponta a ponta tratava a releitura como sucesso. **Feito**
  (`fed8cc3`): os casos do PC parado e da lápide no fim do mês guardam o cursor do PC 2 de antes das
  lápides e puxam dele depois que elas vencem (`200`, com o que resta); o ponta a ponta exige que o PC 3
  puxe do cursor dele, sem `pull:0` nem releitura. Conferido por mutação (a marca d'água subindo no
  job e o `410` no pull): os testes antigos passavam, os novos falham.
- **Médio: o "de hora em hora" sem prova.** O intervalo era uma constante de 1 h, e todo teste
  cancelava o `ctx` antes do primeiro tique. **Feito** (`c1db32f`): a `CloudConfig` ganha
  `IntervaloDaArrumacao` (zero é a constante; o `serve` não muda), e
  `TestArrumarSempre_RodaARetencaoACadaTique` roda com 20 ms e o relógio injetado: a rodada do tique
  apaga a lixeira que venceu depois da subida. Conferido por mutação (sem o `Reter` do tique, falha).
- **Baixo: a arrumação prendia a trava do contador por um tempo sem limite.** As lápides vencidas e os
  dois `DELETE` sem limite (os eventos órfãos e as respostas) iam numa transação só, contra o "um item
  por transação" do próprio desenho; com o token com defeito da D3, ou o job parado dias, o push e o
  CRUD do mesmo usuário esperariam além dos prazos do servidor. **Feito** (`726bfd3`): cada lote é uma
  transação curta com a trava (as lápides em lotes de 500, por cursor; os eventos e as respostas em
  lotes de 5.000, pela chave primária, até um lote apagar menos que o tamanho). A trava fica em cada
  lote: é ela que evita o deadlock com o "apagar meus dados". Provas no Postgres (não rodaram):
  `TestPostgres_Retencao_ArrumacaoEmLotesSoltaATrava` (o contador livre entre os lotes, por um `FOR
  UPDATE NOWAIT` de fora) e `TestPostgres_Retencao_ArrumacaoCanceladaEntreLotes`.
- **Baixo (dois achados): a linha `retenção` como batimento não pega o job quebrado.** Ela sai em toda
  rodada, até com a listagem falhando ou com todo usuário falhando. **Feito** (`2fd57ea`, só
  documentos; o código já contava a listagem em `falhas`): o alerta da F11 é duplo (a ausência da
  `retenção` por mais de 2 h, ou uma `retenção falhou` de `categoria` `banco` ou `outro`), o `11`
  documenta a `retenção falhou`, a `etapa` e a `categoria` `banco`, e o batimento exige `LOG_LEVEL`
  em `info` ou `debug`.
- **Baixo: a ADR prometia, para o reenvio depois dos 190 dias, o que o código não faz.** **Feito**
  (`4667268`): a linha da F9c, o parágrafo das `applied_mutations`, a escolha menor 1 e o `05` dizem o
  que a F9c prova (o ditado dá `gone` `fora_da_janela`; o aprendido, `applied` com a versão atual, sem
  gravar) e o limite real do prazo.
- **Baixo: a F9e não estava feita.** **Feito** (`deb9634`): saem `RETENTION_MONTHS` e `TRASH_DAYS` do
  `10` e do `.env.example`, os dois comentários de teste do `410` dizem a D2, e o README, esta ADR e o
  `13` têm o estado da F9.
- **Baixo: o `serve_pg_test` cancelava antes da rodada da subida terminar.** **Feito** (`e4dc678`):
  espera a linha `retenção` por polling antes do `cancel()`, como o teste da memória. Não rodou aqui.

**Riscos que a revisão acrescenta:**
- **O relógio do PC atrasado** (acima): a lixeira que um PC com o relógio errado põe num ditado mais
  velho que o atraso ~~sai da nuvem em até 1 h, com lápide, até o Mauro decidir o piso~~. **[Desde a
  D5 (08-out-2026): fica 24 h na lixeira da nuvem e do celular, restaurável, e então sai, com lápide;
  perto da janela de 6 meses, sai com ela. Se ninguém a restaurar nas 24 h, o efeito é o de antes: a
  lápide faz todos os PCs apagarem a cópia.]**
- **A rodada no Postgres continua pendente**, e agora com mais SQL novo (os `DELETE` em lotes).

**O Postgres da F9 rodou em 08-out-2026**, num contêiner novo que o Mauro subiu com `scripts/postgres-de-teste.ps1`, sobre a ponta da F10 (que contém a F9): `go test ./migrations/ ./internal/server/... ./cmd/server/...` com 287 testes de topo passando, nenhum pulado e nenhum falhando (`pgstore` 33 s; entre eles `TestContrato_Postgres` com a retenção, a arrumação e o piso, e o `retencao_pg_test.go` inteiro), e o mesmo com `-tags logindeteste`, verde. As três mutações da F9 que só o Postgres mata, numa cópia por `git archive`, cada uma conferida como aplicada, morreram: a decisão refeita sob a trava tirada do `reterUm`; o cursor dos candidatos sem o desempate pelo id (o lote de 507 com `created_at` repetido na divisa termina com 506, a falha certa, e não um erro de SQL); e a arrumação dos eventos sem o `NOT EXISTS` do aprendido. Com isso, os avisos de "o Postgres não rodou" desta seção ficam superados.

### Decisões da F10 (08-out-2026)

O que a F10 decidiu antes de escrever código: as quatro respostas do Mauro, o desenho, as subfatias,
as escolhas menores e os riscos aceitos. O plano da fatia parou em quatro perguntas que só o Mauro
responde (onde fica a pergunta dos 6 meses, o que vale depois do OK, até quando a lixeira espera a
nuvem e o que acontece com as cópias do banco). Ele as respondeu em 08-out-2026, **todas com a
recomendação**, e mandou seguir. O desenho parte do plano e da crítica do mesmo dia: a crítica achou
um problema alto (o portão que recusa por padrão e o Ligar, que precisa apagar, se contradiziam) e
vários médios e baixos, e as correções já estão no desenho abaixo. Esta seção nasceu na F10.0 (só
documentos). As referências `arquivo:linha` foram conferidas no código de `3b40fd7` (a ponta da F9,
08-out-2026; o código do PC é o mesmo da `main`, `2350908`); os nomes de função, de chave e de
constante são propostas.

- **D1. Configurações, confirma na linha. Confirmada pelo Mauro em 08-out-2026.** Um bloco
  **Histórico** nas Configurações, logo antes da Privacidade (depois da Nuvem), que só aparece quando
  há o que apagar: "N ditados têm mais de 6 meses. Ao confirmar, eles saem deste PC de vez, e daqui
  em diante o que fizer 6 meses sai sozinho. Antes, uma cópia do histórico fica neste PC." O botão
  pergunta de novo na própria linha, sem janela e sem faixa, no molde do "Esquecer esta nuvem"
  (`web/src/NuvemNoPC.tsx:205-217, 303-341`: o foco no Cancelar, o Esc cancela). Os ditados que têm
  duas versões (o selo) também saem, sem a escolha de quem usa; a tela diz quantos são, quando houver.
  Por quê: é o jeito que o painel já usa no PC para o que não tem volta; um clique só é leve demais
  para apagar de vez, e digitar uma palavra é pesado para uma decisão que ele toma uma vez.
- **D2. Sai sozinho, com Desligar e freio. Confirmada pelo Mauro em 08-out-2026.** Depois do OK, o
  que completar 6 meses sai sozinho do PC, em até 1 h (o ciclo do expurgo, que já roda na subida e de
  hora em hora). Na mesma linha, **Desligar** volta ao de hoje: o PC guarda tudo, e o que já saiu não
  volta (só a cópia guarda). **O freio:** se o Agent ficou mais de 7 dias sem rodar, ou o relógio do
  PC pulou, nada é apagado e a pergunta volta, com a contagem. Por quê: a ADR e o AC-062 pedem o OK
  para ligar a regra, não a cada dia, e perguntar todo dia vira ruído; o Desligar deixa o controle com
  ele; o freio cobre o relógio errado, que apagaria cedo, e a volta depois de muito tempo parado,
  quando sairiam muitos de uma vez.
- **D3. No máximo até 6 meses. Confirmada pelo Mauro em 08-out-2026** (a correção da D4 da F9). A
  lixeira do PC só apaga de vez o que a nuvem já soube: o ditado que a pessoa pôs na lixeira com a
  nuvem ainda sem saber (o PC desconectado, por exemplo) **fica na lixeira do PC além dos 30 dias até
  a nuvem saber**; se o PC não volta, fica **no máximo até o ditado completar 6 meses**, porque a
  essa altura a nuvem já o apagou e recusa qualquer mudança nele (`gone` `fora_da_janela`), e esperar
  não avisa nada. Num PC que nunca conectou, nada muda: os 30 dias de hoje. O ditado que a nuvem
  **recusou** (`recusado`) sai aos 30 dias: segurar não ajuda, porque não há marcador nem reenvio.
  Por quê: sem limite, o texto que a pessoa mandou embora ficaria guardado indefinidamente, o oposto
  do que a correção queria. A tabela de situações, com o prazo real de cada uma, está em "A guarda da
  D3".
- **D4. Botão "Apagar as cópias". Confirmada pelo Mauro em 08-out-2026.** As cópias do histórico ao
  lado do banco (`antes-do-dono`, `antes-da-nuvem` e a nova, `antes-dos-6-meses`) guardam também o
  texto que os 6 meses apagam. O bloco Histórico diz quantas cópias há e tem **Apagar as cópias**,
  com a pergunta na própria linha antes. **O Agent nunca as apaga sozinho.** A cópia de um envio que
  ainda não terminou (a `nuvem_copia` sem o `nuvem_envio_aceito_em`) não pode ser apagada. Por quê:
  enquanto a cópia existe, os 6 meses não valem de verdade no disco; apagar sozinho tira a rede de
  segurança sem ele ver; o botão deixa a decisão com ele, no mesmo lugar da contagem.

**A D4 da F9, lida de novo.** A D4 da F9 dizia que a F10 desenharia "o que é 'já soube':
`server_version` e sem marcador pendente". Lida ao pé da letra, essa regra prenderia para sempre três
tipos de lixeira: a do PC que nunca conectou (nenhuma linha tem `server_version`), a do ditado que a
nuvem nunca terá (a lixeira não sobe: `internal/storage/envio_sqlite.go:171-172`) e tudo o que vem
depois de um "Esquecer esta nuvem" ou de um epoch novo (os dois zeram o `server_version`:
`esquecer_sqlite.go:76-81`, `fila_sqlite.go:382-385`). Isso quebraria o AC-019, que vale desde
29-set-2026. O desenho usa uma **guarda negativa**: o PC segura só o que "a nuvem pode ter e ainda não
soube", com o teto na janela de 6 meses (a D3). É uma leitura da D4, não uma decisão nova; fica
registrada como desvio abaixo.

**Onde o código está hoje (conferido em `3b40fd7`).**
- **A lixeira.** O único expurgo por tempo é o `PurgeExpired`
  (`internal/storage/transcription_sqlite.go:502-524`): numa transação, apaga os marcadores e as
  linhas de tudo que tem `purge_at <= agora`. Não olha marcador pendente nem em voo, `server_version`,
  selo nem dono. É o buraco da D4 da F9. O `fila_sqlite_test.go:142-153` prende exatamente isso: o
  ditado `idA`, com `server_version` e o marcador `CampoTexto|CampoLixeira`, é expurgado aos 31 dias.
  Esse teste vai ter de virar, de propósito, e o relatório da F10a diz isso.
- **Quando roda.** `expurgo.Iniciar` roda uma vez na subida e de hora em hora
  (`internal/agent/expurgo/expurgo.go:42, 68-99`), ligado em `abrirHistoricoEm`
  (`cmd/agent/main.go:1184-1187`) depois do `escolherDono` e antes do laço do sync.
- **Os 6 meses do ativo não existem no PC.** Só a lixeira grava `purge_at`
  (`transcription_sqlite.go:424`; `fila_sqlite.go:808-812` para a que vem da nuvem). O `retention.Job`
  (`internal/retention/job.go:82-193`) não tem chamador de produção.
- **Onde nasce marcador.** `emitir` (`transcription_sqlite.go:170-186`) só o põe num ditado com
  `server_version`, ou num ditado novo, nascido aqui, com a nuvem conectada (`nuvem_aparelho`). O
  `SoftDelete` de um ditado que a nuvem não tem tira o marcador de criar que ainda não saiu
  (`transcription_sqlite.go:469-474`); o em voo fica.
- **Desconectar** mantém o `server_version` das linhas (`internal/agent/nuvem/servico.go:372-382,
  651-700`): o marcador de uma lixeira feita desconectado nasce do mesmo jeito e espera na fila.
- **O Esquecer e o epoch novo.** O "Esquecer esta nuvem" zera o `server_version`, apaga a fila e
  anota as pendências na `sync_mudou_aqui` (`esquecer_sqlite.go:43-95, 124-150`), e grava o instante
  dele em `nuvem_esquecida_em` (`envio_sqlite.go:69`). O recomeço do epoch novo (`fila_sqlite.go:
  304-390`) zera o `server_version`, apaga a `nuvem_esquecida_em` e a `sync_mudou_aqui` (315-320) e
  deixa a lixeira fora da fila (296-299).
- **A conexão é uma só** (`SetMaxOpenConns(1)`, `cmd/agent/main.go:1170`), e o `foreign_keys` vale
  por conexão (`internal/storage/migrations.go:97`). Não há `PRAGMA secure_delete`, e o
  `modernc.org/sqlite` v1.59.0 para Windows não é compilado com `SQLITE_SECURE_DELETE` (a linha de
  geração de `lib/sqlite_windows.go` não tem esse `-D`).
- **O piso de 24 h da lixeira que chega à nuvem** (a D5 da F9) é da nuvem: o `purge_at` não viaja no
  fio, e o PC não muda por ele.

**O desenho em uma frase.** São duas regras separadas, e só uma delas apaga mais do que hoje:
1. **A lixeira (30 dias) ganha a guarda da D3.** O resultado é sempre um subconjunto do que é apagado
   hoje, então **não precisa de OK**. Pode ir primeiro (F10a).
2. **Os 6 meses.** Apagam o que hoje fica, então ficam atrás de um **portão que recusa por padrão**:
   a prévia, a cópia e o OK expresso (AC-062). Sem o OK gravado, nada dos 6 meses é apagado (F10b e
   F10c).

**A guarda da D3 na lixeira (F10a).** O `PurgeExpired` passa a ler os candidatos (`purge_at <=
agora`), decidir item a item numa função pura (`esperaANuvem`, em `internal/storage`) e apagar os
liberados. Um item com `purge_at` vencido **fica** quando, ao mesmo tempo:
- ele não é `recusado` nem `fora_da_janela` (a nuvem já disse que não o quer, ou que ele não vale mais
  lá: segurar não ajuda);
- ele está **dentro da janela** de 6 meses (`naJanela`, `envio_sqlite.go:178-180`, decidida em Go e
  em UTC): é o **teto da D3**. Depois dos 6 meses do `created_at`, a nuvem recusa o push (`gone`
  `fora_da_janela`, regra da F2) e o job da F9 apaga o ativo, então esperar não avisa nada. Na rodada
  em que o freio dispara, o teto não vale (abaixo, "O freio vale também para o teto");
- e cumpre **pelo menos uma** destas, as três de "a nuvem pode ter e ainda não soube":
  - **P1.** Há qualquer marcador dele na `sync_outbox` (`entity_type = 'transcription'`): parado, em
    voo, a repetir ou de reconciliação (`MarcadorCriar`, `fila_sqlite.go:159-167`).
  - **P2.** Ele está na `sync_mudou_aqui` **e** o `server_version` é nulo (a mesma condição do
    `estado()`, `fila_sqlite.go:692-704`): estava pendente na hora do Esquecer. Com o
    `server_version`, a nuvem já o reconciliou, e a anotação, que só o próximo Esquecer ou o
    Recomeçar limpam (`esquecer_sqlite.go:133-138`), não prende a lixeira (correção da crítica).
  - **P3.** Há `nuvem_esquecida_em = E`, e ele tem `server_version` nulo, `created_at <= E` e
    `deleted_at > E`: a lixeira feita depois do Esquecer num ditado que a nuvem esquecida pode ter.
    Na volta à mesma conta, o pull o reconcilia (`internal/sync/decisao.go:193-195`;
    `fila_sqlite.go:786-796`, `MarcadorCriar`) e ele cai em P1; ao subir, o `server_version` volta e
    P3 deixa de valer.

Tudo é decidido **numa transação só**, pela conexão única: (1) lê `E` e os candidatos, com `created_at`,
`deleted_at`, `server_version`, `sync_state` e dois `EXISTS` (o marcador e a `sync_mudou_aqui`); (2)
decide item a item; (3) apaga por id o marcador de ditado, a `sync_mudou_aqui` e a linha dos
liberados. Ler e apagar na mesma transação fecha a corrida com o push e com o `Aplicar`, que rodam em
transações separadas. O marcador do aprendido (`learned`) nunca é tocado.

**O freio vale também para o teto.** O relógio do PC que pula para a frente faria `naJanela` dar falso
e liberaria a lixeira com marcador, sem OK e sem a nuvem saber: o mesmo defeito da D4 da F9, por outro
caminho (correção da crítica). Por isso o `PurgeExpired` lê o freio (abaixo, "O freio") e, **na
rodada em que ele dispara, não libera pelo teto**: o item com marcador fica mais uma rodada. Como o
expurgo da subida roda antes do primeiro sync, esse adiamento dá ao sync a chance de a nuvem dizer
`applied` ou `fora_da_janela` antes de o PC soltar a lixeira. É um adiamento de uma rodada (1 h), e
não uma retenção sem fim: um freio que durasse até alguém agir prenderia para sempre a lixeira do PC
desconectado para sempre, contra a D3. O risco que sobra está nos riscos aceitos.

**A guarda, em cada situação** (o prazo real de cada uma; todo "fica" termina na janela do ditado):

| Situação | O que acontece |
|---|---|
| Nunca conectado | Nenhum marcador, nenhuma anotação, nenhum `E`: os 30 dias de hoje. |
| Conectado, ditado nascido aqui que a nuvem nunca teve | O `SoftDelete` tira o marcador de criar parado: 30 dias. O marcador **em voo** fica até a resposta (P1). |
| Conectado, ditado que a nuvem tem | O `SoftDelete` põe o marcador `CampoLixeira`; fica até o push confirmar (o marcador sai) e sai na rodada seguinte. Se a nuvem não responde, fica, até a janela. |
| Desconectado (o Desconectar mantém o `server_version`) | O marcador `CampoLixeira` segura até reconectar e subir. É o caso da D4 da F9. |
| Desconectado para sempre | Sai quando o ditado completa 6 meses (o teto da D3): a nuvem já o apagou, e nada se perde. |
| Depois do Esquecer | P2 e P3 seguram até a nuvem saber (a volta à mesma conta) ou até os 6 meses do ditado, **inclusive o ditado que nunca subiu**: o Esquecer zerou o `server_version`, que dizia se a nuvem o tinha. Retém a mais; é limitado e aceito. |
| Outra conta (esqueceu a A, conectou a B) | Igual: a B nunca terá o ditado, então P2 e P3 seguram até os 6 meses do ditado. Aceito. |
| Recusado | Sai aos 30 dias (decisão do Mauro). A nuvem continua com o ditado ativo; risco registrado. |
| Epoch novo (backup restaurado) | Sem mudança: o recomeço apaga `E` e a `sync_mudou_aqui`, e a lixeira fica de fora da fila; 30 dias. Risco já aceito na F7c e na F9. |

**Os 6 meses no storage, atrás do portão (F10b).**
- **A regra.** Todo ditado **do dono** (`user_id` igual ao dono fixo, como no painel) fora da janela
  sai, esteja ativo ou na lixeira. Isso já contém a sobreposição (`EffectiveTrashPurgeAt`), que o PC
  hoje não aplica. A decisão é `!agora.Before(retention.ActiveExpiryAt(created_at))`, linha a linha,
  em Go e em UTC: a mesma conta da nuvem (`cloudstore.OutOfWindow`, `rules.go:178-180`). Nunca
  `interval` nem corte de data como regra: 31/ago vence em 3/mar.
- **As linhas de outro dono ficam.** Uma linha com outro `user_id` (de um Agent anterior ao dono
  fixo) não é tocada (`dono.go:235-264`). A prévia diz quantas são, quando houver, e o `09` também.
- **O passo só roda com o dono fixo.** Sem ele (`LerDonoFixo` com `ErrSemDonoFixo`: `escolherDono`
  caiu na regra antiga, `cmd/agent/main.go:823-838`), o passo pula e loga a contagem do que pulou. A
  regra antiga é provisória, e apagar com um dono provisório poderia apagar o que o painel nunca
  mostrou.
- **O corte do pré-filtro, num lugar só.** `retention.CorteDoAtivo(agora)` (em `internal/retention/
  policy.go`) é `agora.UTC().AddDate(0, -ActiveRetentionMonths, 0)`. O PC e a nuvem o usam; o
  `cloudstore.CorteDoAtivo` (`retencao.go:201-203`) passa a chamá-lo, e mantém o nome para não mexer
  em chamada nem em teste de Postgres que ainda não rodou. **O `.UTC()` importa:** o relógio do Agent
  é o do sistema, em hora local, e o `AddDate` soma meses no relógio de parede. Em
  `America/Sao_Paulo`, com `agora` = 2027-03-01T01:00Z (28/fev 22:00 local), o corte local daria
  2026-08-29T01:00Z, e o ditado de 2026-09-01T00:30Z, vencido desde 2027-03-01T00:30Z, ficaria de
  fora da prévia e do apagar por uns 3 dias. Não apaga cedo, mas diverge da nuvem. (O `job.go:163`
  tem a conta sem `.UTC()`; o `Job` não roda no PC, e fica como está.)
- **Lotes de 500, por cursor.** Cada lote é uma transação: lê até 500 candidatos com
  `(created_at, id) > último lido` (o pré-filtro `created_at <= CorteDoAtivo(agora)`, `user_id` do
  dono), **decide cada um em Go** e apaga por id os que venceram. O cursor anda pelo último **lido**,
  e não pelo último apagado, e o laço só termina quando uma leitura volta com menos de 500 linhas
  (nunca por "nenhum apagado no lote"). O pré-filtro traz de propósito candidatos que ainda não
  venceram (até uns 3 dias perto do fim do mês; em 1/mar, os ditados de 29 a 31/ago); sem cursor, o
  laço pararia cedo (apagados diferente da prévia) ou releria os mesmos 500 para sempre, segurando o
  mutex do Ciclo, e o `Parar` do fechamento do Agent esperaria sem fim. É a classe de defeito que a
  F9 já pagou (o cursor que não avança e o `>=` no lugar do `>`). Todos os lotes de uma rodada usam o
  **mesmo `agora`**.
- **O que sai com o ditado.** A linha, o marcador de ditado da `sync_outbox` e a `sync_mudou_aqui`;
  o `local_corrections.transcription_id` vira nulo pela FK (0001). **Nunca** o aprendido: nem a
  `sync_outbox` do tipo `learned`, nem a `sync_aprendido`, nem a `sync_veio_da_nuvem`. Os pendentes e
  os que têm o selo saem junto: a nuvem já recusa e já apagou esse ditado, e a prévia diz quantos
  têm o selo, porque saem sem a escolha de quem usa.
- **Uma função para a prévia e para o apagar.** `foraDaJanela(q, dono, agora)` é a leitura que os dois
  usam, com o mesmo cursor; a prévia só conta (o total, quantos estão na lixeira, quantos têm o selo
  e quantos são de outro dono, que não entram no total). A tabela tem uns milhares de linhas: sem
  índice novo e sem migration.
- **O portão.** Um só ponto apaga, `apagarLoteComPortao(ctx, tx, ...)`, e ele **lê a chave
  `retencao_6m_aceita_em` dentro da transação que recebe**; sem a chave, apaga zero, mesmo com o
  relógio anos à frente. O `ApagarForaDaJanela` do ciclo abre a transação e o chama; o Ligar abre a
  dele, grava as chaves e o chama (abaixo). **Não existe parâmetro de "forçar"** nem caminho que apague
  sem ler a chave: um parâmetro desses reabriria o que o portão existe para fechar.

**A chave, a cópia, o Ligar e o Desligar (F10c).** As chaves moram na tabela `instalacao` (sem
migration), com nomes que não começam por `nuvem_`: o Esquecer não as apaga (`esquecer_sqlite.go:
14-17`) e o acessor da nuvem não as alcança (`instalacao.go:44-46`). `retencao_6m_aceita_em` é o OK
(o portão); `retencao_6m_rodada_em`, a última rodada (o freio); `retencao_6m_copia`, a cópia feita
(o nome do arquivo, sem a pasta); `retencao_6m_pausada_em`, a pausa do freio, para a tela dizer que a
regra foi pausada.
- **A cópia.** `Fila.copiar` (`envio_sqlite.go:285-321`) vira uma função do banco com a **marca como
  parâmetro** (hoje fixa em `marcaDaCopiaDoEnvio`, linha 303). Marca nova:
  `historico.db.antes-dos-6-meses-AAAAMMDD-HHMMSS.db`; `MaxCopias = 3` por marca (`dono.go:59,
  94-112, 335-354`); as frases de erro do envio, com "Nada foi apagado." no lugar de "Nada foi
  enviado." (`internal/agent/nuvem/historico.go:30-41, 139-155`). Segura a conexão, como a do envio
  (de 139 a 165 ms com 7 mil ditados, F7c), e é conferida tabela por tabela.
- **Uma cópia só.** Ela é registrada em `retencao_6m_copia`, à moda da `nuvem_copia`
  (`envio_sqlite.go:62-64, 249-261`), e os OKs seguintes (depois do freio ou do Desligar) **não copiam
  enquanto a registrada existir** (a chave e o arquivo). Uma cópia por OK acumularia, a cada pausa,
  mais um arquivo com o texto inteiro, que a retenção quer apagar, e depois da terceira pausa o OK
  falharia com "já há 3 cópias". Apagada a cópia (pelo botão da D4, ou à mão), o próximo OK copia de
  novo. O que nasce depois da cópia e sai mais tarde não está nela: é por desenho, e fica nos riscos.
  **[A tela diz isso: com a cópia de um OK anterior ao lado do banco, a pergunta não promete outra
  ("Não se faz outra cópia: a que já está neste PC é de antes, e o que nasceu depois dela não está
  nela."); revisão da F10.]**
- **O Ligar** (`POST /api/retencao/ligar`, dentro do mutex do Ciclo; já ligado, não faz nada; sem o
  dono fixo, `409` antes de tudo), nesta ordem:
  1. **Reconta** com um `agora` fixo. Com N = 0: `409` "Não há nada a apagar agora." e nada mais (nem
     cópia, nem OK). Com N diferente do `previa` que a tela mandou: `409` com o número novo, e a tela
     troca a frase antes de apagar. A escolha do N = 0 (correção da crítica): recusar, e não "copiar
     sempre que houver ditado do dono". O caso é uma corrida (a tela contou lixeira fora da janela, e
     o expurgo da lixeira a levou antes do clique); copiar sem nada a apagar deixaria um arquivo com
     todo o texto à toa, e o OK sem cópia faria a primeira purga real (a do dia seguinte) acontecer
     sem a cópia que o AC-062 exige. Recusando, a regra continua desligada, e a pergunta volta no dia
     em que houver o que apagar.
  2. **Faz a cópia e a confere**, se a registrada não existir, e a registra logo depois de conferida
     (o clique de novo, depois de uma queda, não copia outra vez, como a `nuvem_copia`). A cópia que
     falha deixa tudo como estava: nada apagado, nenhuma chave gravada (o OK, a rodada e a pausa).
  3. **Numa transação só**, grava `retencao_6m_aceita_em` e `retencao_6m_rodada_em` (e tira a
     `retencao_6m_pausada_em`) e apaga o **primeiro lote** pelo mesmo `apagarLoteComPortao`, que lê a
     chave recém-gravada. Se o primeiro lote falha, o rollback leva o OK junto. (O precedente do
     `EnviarHistorico`, `envio_sqlite.go:237-276`, grava o aceite por último, mas lá o aceite não
     protege nenhum `DELETE`; aqui, gravar o OK por último faria o apagar do Ligar recusar-se a si
     mesmo, e a saída "natural" seria um parâmetro de forçar.)
  4. **Os lotes seguintes** passam pelo mesmo método, cada um na sua transação, com o mesmo `agora`.
  5. Devolve `apagados`, que é igual à recontagem.
- **O Desligar** (`POST /api/retencao/desligar`, **no mesmo mutex do Ciclo**: um Desligar entre dois
  lotes do Ligar de outra aba seria sobrescrito pela gravação do OK) tira `retencao_6m_aceita_em`,
  `retencao_6m_rodada_em` e `retencao_6m_pausada_em`. Fica a `retencao_6m_copia`. O que já saiu não
  volta.

**O freio (F10c).** A regra é pura, em `internal/retention`: o freio dispara quando a última rodada
foi há **mais de 7 dias** (`agora - rodada_em > 7 dias`). Só o salto para a frente freia: o relógio
que volta apaga menos, nunca mais cedo.
- **Quem grava `retencao_6m_rodada_em`:** o Ligar (na transação do OK) e **toda rodada do Ciclo**, ao
  fim, com ou sem OK, menos a rodada que pausou **[e a rodada em que o passo falhou: a pausa cuja
  transação voltou atrás deixa o OK e a rodada velha no banco, e gravar a rodada apagaria o freio;
  revisão da F10]**. Quem apaga: o Desligar e a pausa. **Rodada sem
  `rodada_em` não dispara o freio** (a primeira rodada da instalação, a depois do Desligar ou da
  pausa); então a rodada também a grava. É uma escrita pequena por hora. Sem ela fora do OK, o freio
  não existiria para a lixeira (o teto da D3) de quem nunca ligou os 6 meses.
- **No passo dos 6 meses** (OK gravado e freio disparado): não apaga nada, **tira o OK** e a
  `rodada_em`, grava `retencao_6m_pausada_em`, loga um WARN só com contagens (`dias_parado` e a
  prévia) e a tela volta a perguntar. Sem OK, o freio só adia o teto da lixeira (acima).
- **Na tela** (D2, "a pergunta volta"): o bloco Histórico volta com a pergunta de sempre (com N maior
  que 0) e uma frase discreta: "A regra foi pausada: o Agent ficou parado por mais de 7 dias, ou o
  relógio do PC mudou. Nada foi apagado." Com N = 0 o bloco não aparece (não há o que perguntar), e a
  regra fica desligada, com o WARN no log; o Ligar de novo com N = 0 daria `409`. **[Só sem cópias:
  com elas (depois de um OK, a dos 6 meses fica), o bloco aparece por elas, e a frase da pausa vem no
  topo, com "A pergunta volta quando houver o que apagar."; revisão da F10.]**
- **O teste do freio não é por `Advance` com tiques.** O `clock.Fake` dispara cada tique de hora
  dentro do `Advance` (`internal/clock/fake.go:78-95`), então "8 dias sem rodar" por `Advance` gera 192
  rodadas e nunca freia (e `Set` seguido de `Advance` volta o `now` para o timer pendente, `fake.go:
  47-51`). O teste fecha o Agent (`fechar()`, o Ciclo parado), avança 8 dias e **reabre** com
  `abrirHistoricoEm`; o salto do relógio com o Agent aberto chama o passo direto, com um `agora`
  explícito. A mutação "sem o freio" tem de falhar.

**O ciclo no Agent (F10c).**
- O `expurgo.Ciclo` ganha, depois da lixeira, o passo dos 6 meses, na mesma cadência (a subida e de
  hora em hora): a ordem de cada rodada é (1) o `PurgeExpired`, (2) o passo dos 6 meses, (3) a gravação
  da `rodada_em`. O `Repositorio` (`expurgo.go:46-48`) e a `expurgo.Iniciar` mudam de assinatura, e o
  `Ciclo` passa a ser o dono do mutex que o Ligar, o Desligar e o apagar das cópias compartilham.
- **O `PurgeExpired` muda de assinatura:** de `(int, error)` para um resultado com `Apagados` e
  `EsperandoANuvem` (a interface em `storage/transcription.go:203-208`, o
  `InMemoryTranscriptionRepository`, que não tem fila e devolve zero esperando, o `expurgo.Repositorio`
  e os chamadores de teste). Ele lê, na própria transação, `nuvem_esquecida_em` e a `rodada_em`.
- A ordem da subida não muda: depois do `escolherDono` (`cmd/agent/main.go:1178-1186`). O Ciclo e a
  Local API se encontram por uma interface injetável em `localapi.Preferencias` (como a `Nuvem`,
  `server.go:77-80`).
- **O `secure_delete`.** Sem ele, o `DELETE` deixa o texto nas páginas livres do `historico.db` (o
  `auto_vacuum` está desligado e o arquivo não encolhe): os 6 meses, e o expurgo de hoje, não valeriam
  no disco, pelo mesmo motivo das cópias. `abrirHistoricoEm` liga `PRAGMA secure_delete = ON` logo
  depois do `SetMaxOpenConns(1)` (vale por conexão, como o `foreign_keys` do Migrator, e a conexão é
  uma só; se o teste mostrar a conexão trocada, o `_pragma` do DSN vale em toda conexão nova).
  **[O teste mostrou: o driver troca a conexão que teve uma consulta cancelada. O `secure_delete` e
  o `foreign_keys` foram para o DSN; revisão da F10.]** **É
  escolha menor, a confirmar pelo Mauro na entrega.** O teste, em `t.TempDir()`: depois da purga, os
  bytes do texto apagado não estão no arquivo (com a mutação "sem o pragma", o teste falha). O
  `-journal` apagado do disco e o próprio sistema de arquivos ficam fora do alcance (riscos aceitos).
- **O log, só com contagens:** `lixeira: expurgo` com `apagados` e `esperando_a_nuvem` (na subida, ou
  quando `apagados` for maior que zero ou `esperando_a_nuvem` mudar desde a rodada anterior: o PC
  que segura uma lixeira por meses não escreve uma linha por hora); `historico: 6 meses apagados`;
  `ligados` (com `previa`, `apagados` e `copia`: feita ou já existia), `desligados`, `pausados` (WARN,
  com `dias_parado` e `previa`) e `pulados` (sem dono fixo). Nunca id, texto nem caminho.

**A API local e o bloco (F10d), as cópias (F10e).** Rotas novas na Local API, atrás do mesmo
`Host` + `Origin` + token, com `Cache-Control: no-store`; só números e booleanos, nunca texto, id ou
caminho. Sem a interface montada, `503` (`UNAVAILABLE`); um Agent velho responde `404`, e o painel
mostra "Este Agent ainda não tem os 6 meses. Reinicie o Agent." O contrato está no `06`.
- `GET /api/retencao`: `{ "previa", "na_lixeira", "com_selo", "de_outro_dono", "ligada", "pausada",
  "copias" }`. Não depende do `nuvem.Servico` (que devolve nulo sem nuvem, `nuvem/historico.go:
  88-91`): os 6 meses valem também no PC que nunca conectou. A contagem é feita ao vivo, como o
  `Fila.Previa` (`envio_sqlite.go:217-221`).
- `POST /api/retencao/ligar` `{ "previa": N }`: o Ligar acima. `200` com o estado e `apagados`;
  `409` "Não há nada a apagar agora."; `409` com o número novo (`previa`, ao lado do envelope de erro)
  quando a recontagem difere; `409` (cópias demais), `507` (disco cheio) ou `500` na cópia, com a frase;
  `409` sem dono fixo.
- `POST /api/retencao/desligar`: o Desligar; devolve o estado.
- `POST /api/retencao/copias/apagar` (F10e): apaga só os arquivos que casam **exatamente** com
  `historico.db.antes-(do-dono|da-nuvem|dos-6-meses)-AAAAMMDD-HHMMSS.db`, na pasta do banco; devolve
  quantas saíram e quantas ficaram. **Recusa apagar a `antes-da-nuvem` enquanto a `nuvem_copia` existe
  e o `nuvem_envio_aceito_em` não** (o `EnviarHistorico` não copia de novo quando a `nuvem_copia`
  existe, então o clique repetido, depois de uma queda no meio do envio, seguiria sem a cópia que a
  F7c exige). Apagada a cópia dos 6 meses, tira a `retencao_6m_copia`. Também no mutex do Ciclo.
- **O bloco Histórico** (`web/src/Configuracoes.tsx`, entre a Nuvem e a Privacidade; um componente
  novo, `HistoricoNoPC.tsx`): aparece com N maior que 0, com os 6 meses ligados, ou com cópias ao
  lado do banco; nada fora das Configurações. Propostas de texto, a confirmar na entrega: a frase da
  D1 (com "1 ditado tem… Ao confirmar, ele sai…" no singular) e o botão; "{M} deles estão na lixeira.",
  "{K} têm duas versões, e as duas saem.", "{J} são de outro dono e ficam." (cada uma só quando maior
  que 0); ligado, "Os 6 meses estão ligados: o que fizer 6 meses sai sozinho deste PC, em até 1 hora."
  e **Desligar** (sem pergunta: não apaga nada); "Há {C} cópias do histórico ao lado do banco, com o
  texto de antes." e **Apagar as cópias**, com a pergunta na linha ("**Apagar as {C} cópias?** Elas
  guardam o texto que os 6 meses apagam. Não dá para desfazer."). Depois de apagar, o aviso e a
  recarga do histórico. A Privacidade ganha a linha dos 6 meses quando estiver ligado, e o comentário
  de `web/src/ambiente.ts:82-83` deixa de prometer "30 dias".

**As subfatias.** Cada uma com o seu commit, `go test ./...` e `go vet ./...` verdes (e o `pnpm lint`,
`test` e `build` na F10d e na F10e), sem `git push`. **Todo teste usa `t.TempDir()` e o relógio
injetado; nenhum abre o `bin\historico.db`, o `bin\dicionario.json` nem nada do `bin\`.** A primeira
purga real é o clique do Mauro no `bin\` dele, pelo roteiro: nenhum agente clica no OK, roda o Ligar,
roda o Agent nem apaga cópia.

| Subfatia | O que entrega | O que prova |
|---|---|---|
| F10.0 | esta seção e as emendas (`03`, `05`, `06`, `08`, `09`, a linha da F10 em "Fatias" e em "O que o Mauro providencia", e o estado da F10 no `13`) | nada de código; `go test ./...` e `go vet ./...` como estavam |
| F10a | a guarda da D3 no `PurgeExpired`: `esperaANuvem` (pura, `internal/storage/lixeira_nuvem.go`), P1 a P3, o teto da janela, a regra pura do freio (`retention.FreioDaRodada`) com a constante da chave `retencao_6m_rodada_em`, que o `PurgeExpired` lê (quem a grava é a F10c; sem ela, nada freia), a assinatura nova (`Apagados`, `EsperandoANuvem`) e o log. Nunca apaga mais do que hoje, então sem OK | uma linha de teste por linha da tabela de situações: nunca conectado, nascido aqui sem subir e conectado sai aos 31 dias; desconectado com `server_version` e `CampoLixeira` fica aos 31 e aos 90 dias e sai depois que o marcador sai, e sai no teto da janela; em voo fica; o Esquecer com mudança anotada fica, e reconectar à mesma conta, reconciliar, pôr na lixeira e o push `applied` o faz sair aos 30 dias (P2 só com `server_version` nulo); a lixeira feita depois do Esquecer num ditado de antes fica, mesmo o que nunca subiu, e também com o PC ligado a outra conta (os dois até a janela), e a num ditado criado depois sai; recusado sai; fora da janela com marcador sai; epoch novo sai; o marcador `learned` fica intacto; a segunda rodada apaga 0; o freio (`rodada_em` de 8 dias atrás, marcador e janela vencida): fica na rodada em que dispara e sai na seguinte, com a `rodada_em` regravada entre as duas; o `fila_sqlite_test.go:142-153` passa a exigir que o `idA` fique; **ponta a ponta** em `internal/server/integration` (o `nuvemE2E` com o relógio, `novaNuvemE2EComRelogio`, e o `pcE2E` de verdade, SQLite em `TempDir`, `MemoryStore`): desconectar, pôr na lixeira, 31 dias de expurgo (fica), reconectar, o push `applied`, a nuvem tem a lixeira com o piso de 24 h, e o PC só expurga depois de o marcador sair; mutação: sem a guarda, o caso desconectado e o ponta a ponta falham; sem o `server_version` nulo do P2, o teste do P2 falha; sem o teto, o do desconectado para sempre falha. `TestAbrirHistorico_ExpurgaALixeira` continua verde |
| F10b | `foraDaJanela` (a prévia e o apagar), `apagarLoteComPortao`, `PreviaDosSeisMeses` e `ApagarForaDaJanela` (lotes de 500 por cursor, dono, UTC) em `internal/storage/seis_meses_sqlite.go`; `retention.CorteDoAtivo` em `internal/retention`. Ainda nada liga isso no Agent | sem a chave, apaga zero com o relógio 2 anos à frente (e a mutação "o método ignora a chave" morre); com a chave, apaga exatamente o que a prévia contou, com o mesmo `agora`; 31/ago para 3/mar, 29/fev, a hora exata e 1 ns antes; `time.Local` em `America/Sao_Paulo` e num fuso positivo, com o caso de 2027-03-01T01:00Z; **501 candidatos do pré-filtro que não venceram, mais 1 vencido (28/ago, com `agora` em 1/mar)**: só o vencido sai, a rodada termina e `apagados` é igual à prévia (as mutações "cursor parado" e `>=` morrem); a lixeira fora da janela sai, o ativo dentro dela fica, a linha de outro dono fica e a prévia a conta; o pendente e o selo saem; `local_corrections` vira nulo; `sync_aprendido`, `sync_veio_da_nuvem` e o marcador `learned` ficam; a segunda rodada apaga 0; a propriedade (o corte nunca exclui um vencido, hora a hora ao longo de 4 anos) em UTC, em `America/Sao_Paulo` e num fuso positivo |
| F10c | a cópia com a marca (`.antes-dos-6-meses-`) e a `retencao_6m_copia`; o Ligar, o Desligar e o freio; o passo dos 6 meses no `expurgo.Ciclo`, atrás do dono fixo; o `secure_delete`; o log | o Ligar: a cópia que falha deixa a chave ausente e nada apagado; um gancho que derruba o primeiro lote deixa a chave ausente e nada apagado; 3 cópias falsas da marca no `TempDir`: "já há 3 cópias", nada apagado nem gravado; a cópia confere tabela por tabela e o nome tem a marca; `previa` diferente: `409` com o número novo; N = 0: `409`, sem cópia e sem OK; `apagados` igual à recontagem; 3 pausas e 3 re-OKs: sem 4ª cópia e com o OK gravado; a cópia apagada à mão: o próximo OK copia de novo. O ciclo, via `abrirHistoricoEm`, `t.TempDir` e `clock.Fake`: sem OK, nada sai em 60 dias de `Advance` (cada hora é uma rodada com escrita, então o ano inteiro vai pelo passo chamado direto, com o relógio um ano à frente); com OK, o ditado que faz 6 meses amanhã sai no tique seguinte; o Desligar e depois 60 dias de `Advance`: nada sai; o Desligar durante um Ligar espera o mutex. O freio: fechar, `Advance` de 8 dias, reabrir: pausa, nada apagado, o OK e a `rodada_em` tirados, a `pausada_em` gravada; rodada sem `rodada_em` não pausa; Desligar, fechar, `Advance` de 30 dias, reabrir, Ligar e `Advance` de 1 h: continua ligado; a mutação "sem o freio" falha. Sem dono fixo, o passo pula e loga. O texto apagado não está nos bytes do arquivo (e falha sem o pragma). O log não tem id, texto nem caminho |
| F10d | as rotas `GET /api/retencao`, `POST /api/retencao/ligar` e `/desligar` (`internal/agent/localapi/retencao.go`), ligadas por uma interface em `Preferencias`; o bloco Histórico, o cliente, os textos e o cenário em `telasDev.tsx` | Go: o handler devolve só números; `503` sem a interface; o Ligar com erro de cópia devolve a frase certa; o `409` com o `previa` novo; sem token, `401`. Vitest: o bloco não aparece com N = 0 (sem cópias nem regra ligada); o plural e o milhar em pt-BR; o foco no Cancelar; o Esc cancela; o Ligar chama a rota com o `previa` e recarrega o histórico; o `409` troca a frase antes de apagar; o Desligar; a frase da pausa; um Agent velho (`404`) mostra o "Reinicie o Agent". `pnpm lint`, `test` e `build` passam |
| F10e | `internal/storage/copias.go`, a rota `POST /api/retencao/copias/apagar` e o botão com a pergunta na linha | em `t.TempDir()`: só os nomes que casam saem; o `historico.db`, o `-wal`, outro `.db` e um nome parecido ficam; sem confirmação (a tela), nada sai; a `antes-da-nuvem` com a `nuvem_copia` e sem o aceite não sai (e com o aceite sai); apagada a dos 6 meses, a chave sai; a contagem devolve só números. Vitest da pergunta |
| F10f | os documentos (`03`, `05`, `06`, `07`, `08`, `09`, `13`, `PRODUCT.md`, o README, a linha da F10 em "Fatias" e o "Para o Mauro") e o roteiro `docs/teste-manual-f10.md` **[feito como `docs/teste-manual-nuvem-f10.md`, no nome dos outros roteiros da etapa 5]** | a releitura cruzada entre a ADR, o `05`, o `09` e o `PRODUCT.md`: nenhum texto diz que "os 6 meses não existem" nem que "a lixeira some em 30 dias" sem a ressalva da D3. O roteiro (fechar o Agent e conferir o `StartTime` antes de recompilar o `bin\`, abrir as Configurações, ver a contagem, decidir, conferir a cópia, conferir a lixeira desconectado) é rodado pelo Mauro, nunca por um agente |

**O que a F10 muda no que já está escrito (desvios do desenho).**
1. **O `retention.Job` não é usado no PC** (a ADR dizia, em "O que se reaproveita de
   `internal/retention`", que "o `Job` fica onde está, para o PC usar na F10"). Ele trabalha sobre
   listas globais, não conhece dono, marcador nem OK, e não tem portão. A F10b escreve o seu e
   reaproveita só a política (`ActiveExpiryAt`, `EffectiveTrashPurgeAt`) e o corte.
2. **A D4 da F9 é lida como guarda negativa com teto na janela**, e não como "`server_version` e sem
   marcador pendente" (acima).
3. **O OK é gravado na mesma transação do primeiro lote**, e não por último, como no
   `EnviarHistorico`; e **a cópia é uma só**, registrada, e não uma por OK. As duas saíram da crítica.
4. **O `PurgeExpired` muda de assinatura** e passa a ler o freio.
5. **A `rodada_em` é gravada por toda rodada**, e não só com o OK: sem isso o freio não alcança o teto
   da D3 de quem nunca ligou os 6 meses.
6. **O corte do pré-filtro vai para `internal/retention`**, em UTC, e o `cloudstore.CorteDoAtivo` o
   chama.
7. **O `PRAGMA secure_delete`** é novo: nem o expurgo de hoje apagava o texto do disco.
8. **As referências velhas do plano**: o piso de 24 h já está decidido e escrito (a D5 da F9), e o
   plano o dava como pendente; o `Desconectar` está em `servico.go:651`, e não em `347-382`; e o risco
   do relógio estava errado para o ativo, porque a nuvem apaga o ativo exatamente na janela
   (`DecideRetencao`, `retencao.go:103-122`), e a folga de 24 h é só da lápide e do piso: um relógio
   do PC adiantado apaga antes da nuvem pela diferença entre os dois relógios, sem folga de nenhum
   lado.

**Escolhas menores, a confirmar pelo Mauro na entrega** (como nas F4 a F9: a implementação usa a
proposta até ele dizer o contrário):
1. **O freio:** 7 dias (D2); só o salto para a frente freia; e na lixeira (o teto) ele adia a
   liberação por uma rodada, e não até alguém agir.
2. **Lotes de 500**, por cursor, e o mesmo `agora` em todos.
3. **As chaves e a marca da cópia:** `retencao_6m_aceita_em`, `retencao_6m_rodada_em`,
   `retencao_6m_copia`, `retencao_6m_pausada_em` e `.antes-dos-6-meses-`.
4. **N = 0 no Ligar:** `409`, sem cópia e sem OK (acima).
5. **Uma cópia dos 6 meses por instalação**, refeita se for apagada.
6. **O `PRAGMA secure_delete = ON`** no `historico.db`.
7. **Os textos da tela** (acima), e "M deles na lixeira", "K com duas versões" e "J de outro dono"
   só quando maiores que 0.
8. **Depois do Desligar**, o bloco volta a oferecer a regra enquanto houver o que apagar (sem
   insistência: só nas Configurações).
9. **A guarda retém a mais depois do Esquecer e com outra conta** (P2 e P3), até 6 meses, inclusive o
   que nunca subiu. Estreitar pediria uma coluna ou tabela nova (o Esquecer anotaria quais linhas
   tinham `server_version`) e uma migration.
10. **Nada novo na tela da Lixeira** por causa da D3.
11. **O vai-e-volta do pull:** até 1 h depois do vencimento, a nuvem ainda pode devolver um ativo (nem
    o pull nem o `DecidirMudanca` filtram a janela, `decisao.go:169-206`), e o PC o apaga de novo no
    tique seguinte. Fica aceito, sem mexer no sync.
12. **O `InMemoryTranscriptionRepository.PurgeExpired` fica sem a guarda:** a versão em memória não
    tem fila.

**Confirmadas pelo Mauro em 08-out-2026** ("Confirmo todas"), como lhe foram apresentadas na entrega:
o freio de 7 dias só com o salto para a frente (1); nada acontece sem o que apagar no clique (4); uma
cópia por instalação, refeita se for apagada (5); o `secure_delete` (6); as linhas da lixeira, das
duas versões e de outro dono só quando há (7); o bloco que volta a oferecer só nas Configurações
depois do Desligar (8); a lixeira retida até 6 meses depois do Esquecer ou com outra conta (9); e o
vai-e-volta de até 1 h do pull (11). As técnicas (2, 3, 10 e 12) seguem a proposta.

**Riscos aceitos.**
- **A primeira purga dos 6 meses apaga ditado real, sem volta; só a cópia guarda.** É ponto de parada
  (Riscos 7; `09`, "Operações destrutivas"; AC-062). A prova real é o clique do Mauro no `bin\` dele,
  pelo roteiro.
- **O portão é o que separa a F10 do apagar sem OK.** Se o passo dos 6 meses entrar no ciclo que já
  roda sozinho (na subida e de hora em hora) sem o portão lido dentro da transação, os 6 meses passam
  a valer sem OK. A mitigação é o teste negativo (sem a chave, zero apagados com o relógio anos à
  frente) e a mutação que ignora a chave.
- **A lixeira retida a mais.** Depois do Esquecer e com outra conta, a lixeira de ditados que a
  nuvem nunca teve fica até 6 meses (P2 e P3); o PC desconectado para sempre guarda a lixeira com
  marcador até a janela. Nunca passa dos 6 meses do ditado.
- **O recusado sai aos 30 dias com a nuvem ainda o tendo ativo** (decisão do Mauro), e o **epoch novo**
  põe a lixeira fora da fila (`fila_sqlite.go:296-299`): segurar não ajudaria em nenhum dos dois.
- **O relógio do PC.** Adiantado, apaga antes da nuvem. O freio de 7 dias cobre o salto grande e a
  volta depois de parado, mas não um relógio que vive alguns dias adiantado. Na lixeira, o freio só
  adia uma rodada, para o sync da subida falar com a nuvem; um PC **desconectado** com o relógio
  errado não tem esse sync e solta a lixeira com marcador pelo teto.
- **A cópia segura a conexão única** (de 139 a 165 ms com 7 mil ditados, F7c): o ditado espera, não
  falha. E ela soma um arquivo com todo o texto.
- **As cópias guardam o texto que os 6 meses apagam**, e o Agent nunca as apaga sozinho (D4): enquanto
  elas existirem, os 6 meses não valem no disco. A cópia dos 6 meses também não tem o que nascer
  depois dela. O assunto entra na revisão de LGPD da F11.
- **O `secure_delete` só alcança o arquivo do banco.** O `-journal` que o SQLite apaga do disco (não o
  zera), o sistema de arquivos e o backup do Windows ficam fora.
- **As linhas de outro dono ficam** (e o passo pula sem dono fixo): texto além dos 6 meses que o
  painel nunca mostra.
- **A pergunta não expira.** Uma pergunta que fica, com um número que só cresce, pode virar ruído se
  ele não responder. Mitigação: nada com N = 0 (a não ser com a regra ligada ou com cópias ao lado
  do banco), nada fora das Configurações, sem faixa nem janela.
- **A F9 ainda não está juntada** e espera a rodada no Postgres de teste: a F10 parte da ponta dela
  (`3b40fd7`), porque os documentos (a ADR, o `05`, o `09`, o `13`) são os mesmos. O código do PC não
  depende da F9.

**O que a F10.0 não fez:** nenhum código, nenhuma migration, nenhum teste novo. O resto do `13` (a
TASK-020 e a linha 377), o `07`, o `PRODUCT.md` e o README ficam para a F10f; o `06` e o `08`
descrevem o que a F10d e a F10e vão fazer.

**A F10a (08-out-2026): a guarda da D3 na lixeira.** O `PurgeExpired` lê os candidatos, a
`nuvem_esquecida_em` e a `retencao_6m_rodada_em` numa transação só, decide item a item em
`esperaANuvem` (`internal/storage/lixeira_nuvem.go`, pura: P1 a P3, o teto da janela e o freio) e
apaga por id, dos liberados, o marcador de ditado, a `sync_mudou_aqui` e a linha. Devolve
`ResultadoDoExpurgo` (`Apagados`, `EsperandoANuvem`); a versão em memória não tem a guarda (escolha
menor 12). O freio é `retention.FreioDaRodada` (mais de 7 dias desde a última rodada; sem rodada,
nunca), e um valor ilegível da `rodada_em` conta como freio (segurar uma rodada a mais nunca apaga
cedo). O log `lixeira: expurgo` ganha `esperando_a_nuvem` e sai na subida, quando apaga ou quando a
espera muda. O `fila_sqlite_test.go` (o `TestEmissao_OsCincoCaminhos`) **virou de propósito**: o
ditado na lixeira com o marcador de texto e lixeira não é mais expurgado aos 31 dias. Testes: uma
linha por situação da tabela (`lixeira_nuvem_test.go`), a regra pura
(`lixeira_nuvem_internal_test.go`) e o ponta a ponta com a nuvem de verdade
(`internal/server/integration/sync_lixeira_d3_e2e_test.go`). As mutações "sem a guarda", "P2 sem o
`server_version` nulo" e "sem o teto" foram rodadas e derrubam os testes.

**A F10b (08-out-2026): os 6 meses no storage, atrás do portão.** `internal/storage/seis_meses_sqlite.go`:
`foraDaJanela` (a leitura única da prévia e do apagar: lotes de 500 por cursor `(created_at, id)`,
o pré-filtro `retention.CorteDoAtivo` e a decisão linha a linha em Go e em UTC), o
`apagarLoteComPortao` (o único ponto que apaga, que lê `retencao_6m_aceita_em` e o dono fixo dentro
da transação que recebe), `PreviaDosSeisMeses` (`ContagemDosSeisMeses`: total, na lixeira, com o
selo e de outro dono) e `ApagarForaDaJanela`. `retention.CorteDoAtivo` é o corte em UTC, e o
`cloudstore.CorteDoAtivo` passa a chamá-lo. Nada liga isso no Agent ainda. Testes em `t.TempDir()`
(`seis_meses_sqlite_test.go`): sem o OK, zero com o relógio 2 anos à frente; com ele, o apagado igual
à prévia; as bordas (31/ago para 3/mar, o 29/fev, a hora exata e 1 ns antes); o `time.Local` em
`America/Sao_Paulo` e em UTC+14; os 501 do pré-filtro que não venceram com o vencido de 28/ago, e o
vencido na última linha de um lote cheio; o que sai e o que fica (a linha de outro dono, o
`local_corrections`, o aprendido); e a propriedade do corte, hora a hora por 4 anos, nos três fusos
(`internal/retention/corte_test.go`). As mutações "o método ignora a chave", "cursor parado", "`>=`
no cursor" e "o corte sem `.UTC()`" foram rodadas e derrubam os testes.

**A F10c (08-out-2026): a cópia, o Ligar, o Desligar, o freio e o passo no ciclo.**
`internal/storage/seis_meses_ligar.go`: a cópia `historico.db.antes-dos-6-meses-AAAAMMDD-HHMMSS.db`
pelo `copiarSegurando` (o `Fila.copiar` virou uma função do banco com a marca como parâmetro; o
`ErroDaCopia` diz de qual cópia é, `ErrCopiaDoEnvio` ou `ErrCopiaDosSeisMeses`, e o
`ErrCopiasDemais` deixou de dizer "do envio"), registrada em `retencao_6m_copia` (uma só: os OKs
seguintes não copiam enquanto a chave e o arquivo existirem); o `Ligar` (o dono fixo e o "já
ligada" antes de tudo, a recontagem com `ErrNadaAApagar` e `*ErrPreviaMudou`, a cópia, o OK e a
rodada na transação do primeiro lote, e os lotes seguintes); o `Desligar` (tira o OK, a rodada e a
pausa; a cópia fica); o `Passo` (sem o dono fixo, pula e conta; sem o OK, nada; com o OK e o freio,
pausa na mesma transação; com o OK, apaga) e o `GravarRodada`. O `expurgo.Ciclo` roda, com um
`agora` só, a lixeira, o passo e a gravação da rodada (menos na que pausou), e é o dono do mutex do
`Ligar` e do `Desligar`; o log diz `historico: 6 meses apagados`, `ligados`, `desligados`,
`pausados` (WARN) e `pulados`, só com contagens. O `abrirHistoricoEm` liga `PRAGMA secure_delete =
ON` e passa os 6 meses ao ciclo; o `abrirHistoricoComRetencao` devolve também o ciclo, para a F10d
ligar as rotas (o `run` continua chamando o `abrirHistoricoEm`). Testes: o storage
(`seis_meses_ligar_test.go` e o gancho do primeiro lote), o ciclo com dublês
(`internal/agent/expurgo/seis_meses_test.go`, com o Desligar que espera o Ligar) e o ciclo de verdade
(`cmd/agent/retencao_test.go`: 60 dias sem OK, o tique seguinte com OK, o Desligar, o freio na
subida, o freio no teto da lixeira, o Ligar depois de 30 dias parado, sem o dono fixo, o texto
apagado fora dos bytes do arquivo, e o log sem id, texto nem caminho). As mutações "sem o freio" e
"sem o pragma" foram rodadas e derrubam os testes.

**A F10d e a F10e (08-out-2026): as rotas locais, o bloco Histórico e o botão das cópias.**
- **As rotas** (`internal/agent/localapi/retencao.go`; o contrato está no `06`): `GET /api/retencao`
  (só contagens e booleanos), `POST /api/retencao/ligar` `{ "previa": N }`, `POST /api/retencao/desligar` e
  `POST /api/retencao/copias/apagar`, atrás do mesmo `Host` + `Origin` + token (o `ac012_external_test.go`
  confere o `401` e o `403` das quatro) e com `Cache-Control: no-store`. A interface `Retencao` é injetável
  em `Preferencias` e o `*expurgo.Ciclo` a cumpre; sem ela, `503`. O `Estado` do ciclo é só leitura e não
  entra no mutex (o GET de uma aba não espera o Ligar de outra); o `ApagarCopias` entra nele, porque um
  Ligar pode estar fazendo a cópia dos 6 meses. Sem o dono fixo o GET devolve tudo zerado (o passo pula, e
  a tela não tem o que perguntar); o Ligar dá `409` "O histórico deste PC ainda não está pronto para isso.
  Reinicie o Agent." O corpo do Ligar sem `previa` (ou negativa, ou que não é número) é `400`: um OK sem
  número seria um OK às cegas. Nenhuma resposta traz texto, id nem caminho, nem o nome da cópia; o erro de
  dentro vai para o log (que já só tem contagens) e a tela recebe uma frase fixa.
- **O storage** (`internal/storage/copias.go`): `SeisMeses.Estado` (a prévia, o OK, a pausa e quantas
  cópias há, numa transação de leitura) e `SeisMeses.ApagarCopias`. Só apaga **arquivo regular** cujo
  nome casa inteiro com `<banco>.antes-(do-dono|da-nuvem|dos-6-meses)-AAAAMMDD-HHMMSS.db` na pasta do
  banco: o `historico.db`, o `-wal`, o `-shm`, outro `.db`, um nome parecido, uma pasta e um link com o
  nome da cópia ficam. A cópia do envio à nuvem que ainda não terminou (a `nuvem_copia` sem o
  `nuvem_envio_aceito_em`) **não sai**: é só a registrada (as outras `antes-da-nuvem` saem), e com a
  chave ilegível, todas as do envio ficam. Apagada a cópia dos 6 meses (ou sumida à mão), a
  `retencao_6m_copia` sai e o próximo OK copia de novo. O arquivo que o sistema não deixa apagar não para
  os outros: conta como "ficou", e o erro vem sem o caminho (o do sistema leva o nome de usuário do
  Windows). O ciclo nunca chama o `ApagarCopias`.
- **No Agent:** `run` passa a chamar `abrirHistoricoComRetencao` e põe o ciclo em `Preferencias.Retencao`.
  O `abrirHistoricoEm` de cinco retornos virou ajuda dos testes (`cmd/agent/historico_helper_test.go`),
  porque o conferente de `caminhos_test.go` exige uma chamada só, com `cam.Historico`, no `main.go`.
- **A tela** (`web/src/HistoricoNoPC.tsx`, `textosDoHistorico.ts`): como em "A API local e o bloco" e no
  `08`. Nenhum CSS novo: os botões herdam os 44 px do toque e as variáveis dos dois temas. O `ErroDaApi` leva o
  número novo do `409` (`previa`). Uma leitura que chega atrasada (a da volta à aba) não desfaz o que o OK
  acabou de mostrar. O Agent de antes da F10 (`404`) mostra "Este Agent ainda não tem os 6 meses. Reinicie
  o Agent." A Privacidade ganha "O que completa 6 meses é apagado deste computador." com a regra ligada.
  Os cenários de `/telas-dev.html?cenario=…` são `historico`, `historico-simples`, `historico-1`,
  `historico-ligado`, `historico-pausado`, `historico-copias`, `historico-envio` (3 cópias, 1 delas a do
  envio que não terminou) e `historico-velho`; `telasDev.test.tsx` monta cada um.
- **Desvios do desenho:** (1) o botão do OK diz **"Copiando e apagando…"** durante o pedido, e não
  "Copiando o histórico…" e depois "Apagando…": o Agent responde uma vez só, e a tela não vê a fronteira
  entre as duas fases. (2) O Desligar avisa "Os 6 meses foram desligados. O PC guarda tudo de novo." (o `08`
  não dizia). (3) Depois de uma falha do OK, a tela relê o estado e põe a frase numa nota; se a releitura
  mostra que o bloco sumiu ("Não há nada a apagar agora."), a frase vai no aviso.
- **Risco aceito:** o `EnviarHistorico` (nuvem) não entra no mutex do ciclo. Se o clique em Apagar as
  cópias cair, na mesma fração de segundo, entre a cópia do envio ficar pronta e o registro da
  `nuvem_copia`, a cópia ainda não registrada sai; se for antes da conferência dela, o envio falha fechado
  ("Nada foi enviado"), e se for depois, o envio segue com a chave apontando para um arquivo que não
  existe. A janela é de milissegundos e exige dois cliques seus ao mesmo tempo; não se fecha sem mexer no
  `EnviarHistorico`.
- **Testes:** Go (`localapi/retencao_test.go`: só números, `503`, `400` sem a contagem, cada recusa, o `409`
  com o número novo, nada de texto, id ou caminho na resposta; `storage/copias_test.go`: o nome exato, o link,
  a pasta, a cópia do envio em curso, a chave da cópia, o estado; `expurgo/seis_meses_test.go`: o mutex e o
  log; `cmd/agent/retencao_rotas_test.go`: o caminho inteiro pelo HTTP sobre o ciclo de verdade, em
  `t.TempDir()`) e Vitest (`historicoNoPC.test.tsx`, `telasDev.test.tsx`). As mutações "sem a proteção do
  envio", "regex sem o fim", "sem checar o arquivo regular", "sem exigir a contagem", "a leitura atrasada
  vale", "manda 0 como contagem", "o bloco aparece sempre", "sem o foco no Cancelar" e "o Esc não cancela"
  foram rodadas e derrubam os testes.

**O estado da F10, depois da F10e (08-out-2026).** Decidida; a F10.0 (os documentos), a F10a, a F10b, a
F10c, a F10d e a F10e feitas; a F10f (os documentos que faltam e o roteiro manual) não iniciada. A regra
já liga pelo bloco Histórico, mas só pelo clique do Mauro: nenhum agente o dá. **Para o Mauro:**
confirmar ou mudar as escolhas menores 1 a 12 (entre elas o `secure_delete`, a 6) e os textos do bloco
na entrega, e o desvio 1 acima ("Copiando e apagando…"); e, na F10f, rodar o roteiro manual (abaixo).
**[Superado pelo estado depois da F10f, abaixo.]**

**O fechamento da F10 (08-out-2026).** Depois da revisão (a seção seguinte, "A revisão da F10"), a
passada de mutação sobre a F10a a F10e achou **uma viva não equivalente**: o cursor dos 6 meses sem o
id de desempate (em `foraDaJanela`, `AND (created_at > ? OR (created_at = ? AND id > ?))` virando
`AND created_at > ?`) sobrevivia em todos os pacotes testados (`storage`, `expurgo`, `localapi`,
`cmd/agent` e `retention`), porque todo teste do cursor usava `created_at` distintos. Não é
equivalente: com a fronteira de um lote dentro de um empate de `created_at`, as linhas do empate com
id maior eram puladas, a prévia contava menos e o apagar deixava ditados vencidos no PC, em silêncio.
**Feito** (`cadf6a4`): `TestSeisMeses_OCursorDesempataPeloIdNoEmpateDeCreatedAt` grava 2 × 500 + 37
vencidos no mesmo instante, um vencido depois do empate e um que não venceu, e confere a prévia, o
apagado e o que sobra. Conferido à mão: falha na mutação (a prévia dá 501, e não 1.038) e na variante
`id >= ?` (1.040), e passa na base. **Uma observação, que não é viva:** tirar só o `.UTC()` de
`retention.CorteDoAtivo` sobrevive no `internal/storage`, porque o `foraDaJanela` já converte o
`agora` para UTC antes de chamá-lo; as duas conversões se cobrem. O teste de `internal/retention`
mata a de dentro, e o `TestSeisMeses_ComOFusoLocal` mata as duas juntas. Nada a mudar.

**A F10f (08-out-2026): os documentos e o roteiro manual.**
- **O roteiro** é `docs/teste-manual-nuvem-f10.md`, e não `docs/teste-manual-f10.md` como dizia a
  tabela das subfatias: o nome segue os outros roteiros da etapa 5 (desvio de nome, sem efeito). No
  molde do da F8: o que é e por que, e o que o OK faz, antes de qualquer passo (apaga de verdade, a
  cópia fica, daí em diante sai sozinho, ver a contagem não apaga nada); fechar o Agent e conferir o
  `StartTime`; o build do painel num passo à parte; o log do expurgo novo (`esperando_a_nuvem`) e do
  "6 meses pulados" (sem o dono fixo); o bloco **Histórico** nos três casos (a pergunta, só as cópias,
  nada); a lista das cópias ao lado do banco; a decisão, com a confirmação na linha, o Esc e a saída
  de parar na contagem; depois do OK, o bloco ligado, a linha da Privacidade, a cópia
  `antes-dos-6-meses` e a linha `historico: 6 meses ligados`; o **Desligar**; o **Apagar as
  cópias**; e a volta ao Agent do dia a dia. Um passo por vez, com o nome do que aparece na tela, e
  os comandos com o caminho inteiro, um por bloco, para o PowerShell 5.1. É rodado pelo Mauro, nunca
  por um agente: nenhum agente abriu o `bin\`, rodou o Agent ou deu o OK.
- **O caso provável é não haver o que apagar.** O repositório começou em 16-set-2026, então o ditado
  mais antigo do `bin\` é de setembro de 2026 e só completa 6 meses em março de 2027 (é inferência
  pela data: ninguém abriu o banco). O roteiro diz isso logo no começo, mostra o que aparece no lugar
  da pergunta (só as cópias, ou nada) e manda aos cenários de `/telas-dev.html` para ver a pergunta e o
  OK contra o Agent de mentira.
- **A D3 não é conferida à mão.** Pede uma lixeira de mais de 30 dias feita com o PC desconectado:
  só esperando 30 dias, ou adiantando o relógio do Windows, o que o roteiro não faz (mexe no resto do
  PC e dispara o freio). A prova é o ponta a ponta `TestSyncE2E_D3_ALixeiraDesconectadaEsperaANuvem`
  (`internal/server/integration`), que o roteiro oferece rodar (pasta temporária e nuvem na memória:
  não toca no `bin\`); no dia a dia, o `esperando_a_nuvem` do log mostra a lixeira segurada.
- **Os documentos**, na releitura cruzada (nenhum texto diz mais que "os 6 meses não existem" no PC,
  nem que a lixeira do PC sai em 30 dias sem a ressalva da D3): o `03` (o AC-020 sem o "até a F10");
  o `05` (a retenção no PC como implementada, as ressalvas da D3 e do OK na lista de cima, a lápide e
  o ativo vencido, as exceções das cópias); o `06` (o `DELETE` do histórico com a guarda da D3 e os 6
  meses atrás do OK, e o ditado que o PC parado apaga só com o OK); o `07` (o fluxo do expurgo no PC,
  com o diagrama da rodada); o `08` (o teste manual); o `09` (a retenção no PC como implementada, as
  ressalvas na lista de cima e o THR-024); o `13` (a linha da F10 e o estado da TASK-020); o
  `PRODUCT.md` (a lixeira com a D3, os 6 meses atrás do clique, e a linha do sync, que ainda dizia
  "o PC ainda não conecta"); o README (o estado e os dados no disco); nesta ADR, as notas em "Onde
  estamos" e em "Segurança e privacidade", a linha da F10 em "Fatias" e o "O que o Mauro
  providencia". O `01`, o `02` e o `14` (o intake, o PRD e o checklist) ficam: são os requisitos de
  origem, e o `03` e o `05` dizem como eles valem no PC.
- Nenhum código, nenhuma migration e nenhum teste novo na F10f (o teste do fechamento é o commit
  anterior).

**O estado da F10, depois da F10f (08-out-2026).** Decidida, implementada (F10.0 a F10f), revista e
fechada. `go test ./... -count=1`, `go vet ./...`, `pnpm lint` e `pnpm test` verdes, e o `vite build`
compila (numa pasta temporária, sem tocar no `bin\painel`). A regra liga pelo bloco Histórico, só
pelo clique do Mauro. A F10 vem sobre a F9, que espera a rodada no Postgres de teste antes do merge:
o código do PC não depende dela, mas a branch a traz. **Para o Mauro:** juntar à `main` (depois da
rodada da F9 no Postgres, como recomendado na F9); rodar `docs/teste-manual-nuvem-f10.md` (o OK é
decisão dele, e o roteiro deixa parar na contagem); e confirmar ou mudar as escolhas menores 1 a 12
(entre elas o `secure_delete`, a 6, e a cópia única, a 5), os textos do bloco, o desvio 1 da F10d
("Copiando e apagando…") e o nome do roteiro.

### A revisão da F10 (08-out-2026)

Uma revisão do mesmo dia, sobre a F10a a F10e, achou três problemas médios (dois de dados, um de
tela) e oito baixos. O que foi feito, achado por achado (commits `9309987` a `58ef4b5`, sobre
`8a8a98d`). Cada correção tem um teste que falha contra o código de antes (o do bundle, que protege
um comportamento já certo, foi conferido por mutação); nenhum teste abre o `bin\`.

- **Médio (dados): o freio da D2 se perdia quando a pausa falhava.** Na rodada em que o freio
  disparava e a transação da pausa voltava atrás (o `COMMIT` com o banco ocupado, por exemplo), o OK
  e a rodada velha ficavam no banco, mas o ciclo gravava a rodada com o agora: na rodada seguinte o
  freio não disparava mais, e o passo apagava sem pausar e sem a pergunta voltar. **Feito**
  (`9309987`): o passo que falha não grava a rodada; a seguinte freia de novo e tenta pausar outra vez
  (na lixeira, o freio só segura mais). Prova: `TestRodada_OPassoQueFalhaNaoGravaARodada`, com o
  dublê que devolve a pausa com erro.
- **Médio (dados): o `secure_delete` valia só na primeira conexão.** O modernc v1.59.0 descarta a
  conexão que teve uma consulta interrompida por contexto cancelado (a busca que o painel aborta, a
  aba fechada no meio de um pedido), e o `database/sql` abria outra pelo DSN, sem o `secure_delete`
  e sem o `foreign_keys`, até o Agent reiniciar. **Feito** (`29101ab`): os dois pragmas vão no DSN
  (`abrirBancoDoHistorico`, `cmd/agent/main.go`), e a abertura confere o `secure_delete` na
  primeira conexão. Prova: `TestRetencao_OsPragmasValemDepoisDeUmaConsultaCancelada` (cancela uma
  consulta, confere os dois pragmas e só então apaga e procura o texto nos bytes do arquivo); a
  mutação "só o `secure_delete` no DSN" derruba o do `foreign_keys`.
- **Médio (tela): a pergunta prometia cópia em todo OK.** A cópia dos 6 meses é uma só (escolha menor
  5): no OK seguinte (depois do freio ou do Desligar), o que nasceu depois dela não está nela, e a
  tela dizia "Antes, uma cópia do histórico fica neste PC" e "só a cópia guarda". **Feito**
  (`a451f46`, a opção 2 da revisão): o `GET /api/retencao` ganha `copia_dos_6_meses`; com ela, a
  pergunta diz "Não se faz outra cópia: a que já está neste PC é de antes, e o que nasceu depois dela
  não está nela.", e o botão diz só "Apagando…". A confirmação fica só com "Não dá para desfazer.". A
  opção 1 (refazer a cópia) foi descartada: trocar a registrada pediria apagar a velha, e o Agent nunca
  apaga cópia sozinho (D4); acumular bateria nas 3 cópias. **Para o Mauro, na confirmação da escolha
  menor 5:** a cópia continua uma só; quem quiser uma cópia nova antes de um OK apaga as cópias pelo
  botão (o próximo OK copia de novo).
- **Baixo (dois achados, um de dados e um de tela): o Ligar que apagou parte e caiu depois era
  tratado como recusa.** Com o OK gravado e o primeiro lote confirmado, a falha de um lote seguinte dava "não ligaram" no log e `500` "Não deu
  para apagar agora. Tente de novo." na tela, que não relia o histórico e mandava o foco para um botão
  que já não existia. **Feito** (`d87ae63`): o storage devolve `ErrLigadoIncompleto` com o resultado
  marcado (`Incompleto`), o ciclo loga "ligados" com `incompleto=true` e a causa (WARN), a rota
  responde `200` com `"incompleto": true`, e a tela diz "{n} ditados saíram deste PC. Os outros {M}
  saem na próxima rodada, em até 1 hora.", relê o histórico e leva o foco ao Desligar. Se a resposta do
  OK falha e a releitura mostra a regra ligada, a tela diz "Os 6 meses estão ligados. O que ainda não
  saiu sai na próxima rodada, em até 1 hora.", sem "Tente de novo". Provas: o gancho
  `ganchoDepoisDoOK` (cancela o pedido depois do OK) no storage, o dublê do ciclo, a rota e o Vitest.
- **Baixo (tela): pausada, sem nada a apagar e com cópias, nada dizia que a regra desligou.** A frase
  da pausa só existia dentro da pergunta. **Feito** (`7b0856a`): ela vem no topo do bloco sempre que
  a regra está pausada (sem nada a apagar, com "A pergunta volta quando houver o que apagar."). Sem
  cópias e sem nada a apagar, o bloco continua sem aparecer (o texto acima, em "O freio", ganhou a
  ressalva).
- **Baixo (tela): a contagem das cópias incluía a do envio que não termina.** "Apagar as 3 cópias?"
  apagava 2, e o botão seguia oferecendo apagar a que nunca sai. **Feito** (`a451f46`): o `GET` ganha
  `copias_apagaveis`; a pergunta conta só as que saem, o bloco diz "1 delas é a do envio do histórico
  à nuvem, que fica até o envio terminar.", e sem nenhuma apagável o botão não aparece (o foco vai ao
  Salvar).
- **Baixo (tela): o bundle da nuvem sem a prova da F10.** O comportamento estava certo. **Feito**
  (`58ef4b5`): `/api/retencao` e os textos do bloco entram no "não tem" da nuvem e no "é do PC"; um
  import do bloco no app da nuvem derruba o teste (mutação rodada).
- **Baixo (tela): a confirmação colava no texto (gap 0).** **Feito** (`7b0856a`): `.ajuste > * +
  .confirmar` dá à caixa o respiro da linha de botões que ela substitui (14 px); a confirmação que é a
  primeira coisa do ajuste (as do app da nuvem) não muda. Prova: `estilo.test.ts` (a regra) e o Vitest
  do bloco (a caixa fica onde a regra a alcança). A medida no navegador fica para a conferência visual.
- **Baixo (tela): textos repetidos e fora de ordem.** **Feito** (`7b0856a`): o aviso depois do OK diz
  só "{N} ditados saíram deste PC." (a cópia já está na linha das cópias); a frase da pausa vem antes
  da pergunta, e não entre ela e "2 deles estão na lixeira."; com um ditado só, "Ele está na lixeira." e
  "Ele tem duas versões, e as duas saem.". A confirmação deixou de repetir a cópia (acima).
- **Baixo (tela): o limite de 3 cópias contava com outra regra.** O limite contava por prefixo e
  `.db`, inclusive pastas e nomes fora do padrão, que a tela não mostra e o botão não apaga: o "já há 3
  cópias, apague as que não quiser" ficava sem saída. **Feito** (`96673bc`): uma regra só (`ehCopia`:
  arquivo regular de nome exato) no limite, na tela e no botão. Toda cópia que o Agent faz tem o nome
  exato, então nenhuma escapa do limite.

**O que a revisão não mudou:** o Ligar continua usando o contexto do pedido (a aba fechada no meio do
"Copiando e apagando…" cancela a cópia antes do OK, e nada muda, ou os lotes depois dele, e o resto sai
na rodada seguinte); e não há `busy_timeout` no `historico.db` (o passo que falha por banco ocupado
tenta de novo na rodada seguinte, agora sem perder o freio).

### Decisões da F11 (09-out-2026)

O que a F11 decidiu antes de escrever código ou script: as decisões do Mauro, o desenho da primeira
parte, o que espera a D1, as subfatias, o que é dele, as escolhas menores e os riscos aceitos. O plano
da fatia (leitura só, 09-out-2026) parou em quatro perguntas que só o Mauro responde (onde a nuvem
mora, o backup, a revisão de LGPD e como o programa chega ao servidor), e a crítica do plano achou
quatro problemas altos, onze médios e sete baixos. O Mauro respondeu **três** das quatro em
09-out-2026 (D2, D3 e D4), decidiu o que fazer com a restauração e mandou seguir ("segue o baile;
vamos terminar o quanto antes"). A **D1 espera o diagnóstico da VPS**, que ele vai colar. Por isso a
F11 anda em duas partes: a **Parte 1** não depende da D1 e começa já; a **Parte 2** (o compose, a
borda, o roteiro do deploy) só começa com a resposta dela. Esta seção nasceu na F11.0 (só documentos).
As referências `arquivo:linha` foram conferidas no código de `4b870e4` (a ponta da `main`,
09-out-2026); os nomes de função, de arquivo e de subcomando são propostas.

**As decisões do Mauro.**

- **D1. Onde a nuvem mora e quem atende o https. Confirmada pelo Mauro em 09-out-2026: a A1, numa VPS
  que ele já tem** ("segue o baile; vamos terminar o quanto antes"). Os fatos que ele deu, em resumo:
  uma VPS Linux pequena, com Docker e o compose comum, sem Swarm, **datacenter no Brasil**, atrás de um
  **Traefik v3** que já roda (o contêiner `traefik`, com as portas 80 e 443 e o provider docker; a rede
  `web` dos sites). O endereço é um subdomínio dele (o DNS só DNS, sem proxy na frente,
  para o IP da VPS; nos roteiros, o exemplo `https://aurora.exemplo.com.br`); o client Web do Google já tem o redirect de produção e o do PC. **Nada do Traefik
  nem dos outros serviços do servidor é editado ou reiniciado:** a nuvem entra só com os rótulos do
  próprio contêiner, no molde dos outros serviços (a rede `web`, o entrypoint `websecure`, o resolvedor
  `letsencrypt`, `exposedbydefault=false`), com o log de acesso desligado só no nosso router. A CPU da
  VPS já tem a carga dos outros serviços: risco registrado em "A Parte 2 da F11". O texto abaixo é o de
  antes da resposta.
  Nada que dependa
  dela entra na Parte 1: nem o compose, nem rótulos do Traefik, nem Caddyfile. A pergunta começa por
  **qual VPS**: um Traefik num compose comum e um Swarm são cenários diferentes, e o plano tinha
  juntado os dois, e daí saíam conclusões sem base ("o deploy vira stack Swarm", "a malha de entrada
  esconde o IP"). As opções, para ele escolher com a saída do diagnóstico na mão:
  - **A1.** Numa VPS com Traefik, atrás do Traefik que já roda, num compose comum com rótulos só
    nossos (a rede e o entrypoint de TLS que o Traefik já usa, o resolvedor de certificados dele, e o
    serviço só exposto pelo rótulo). Sem Swarm e sem editar arquivo nenhum do Traefik. Encostar nele
    continua ponto de parada: o Traefik passa a atender um nome novo.
  - **A2.** Numa VPS com Swarm, como stack Swarm: segredos do Swarm, `order: stop-first` (o
    `start-first`, comum nos stacks, põe dois `serve` de pé ao mesmo tempo), uma restrição de nó, e a imagem
    carregada por `docker load` só existe no nó onde foi carregada.
  - **B.** Uma VPS só da Aurora, com Caddy próprio: o desenho original desta ADR, isolado do resto.
  - **C.** O servidor atual com Caddy próprio, o que só cabe se o diagnóstico mostrar as portas 80 e
    443 livres (o que ele tem registrado indica que não).

  **O que pesa e o plano não pesava: o alcance de uma invasão.** Numa VPS compartilhada há outros
  serviços expostos à internet. Quem tomar um deles chega à raiz do Docker e lê o volume do Postgres, onde o
  texto é legível. O `09` pede que a revisão diga quem administra e quem lê o texto, e o dossiê da F11b
  diz isso com todas as letras. O argumento pesa a favor da B, e o "custa mais um servidor" vira uma
  pergunta (um servidor menor já era alternativa no intake). **Recomendação (a decisão é dele):** entre as duas de dentro do servidor atual, a A1,
  que não tem as arestas do Swarm; a B, se pesar o alcance de uma invasão.
- **D2. Backup: `pg_dump -Fc` cifrado com o `age`, enviado ao Backblaze B2, giro de 30 dias.
  Confirmada pelo Mauro em 09-out-2026** ("Backblaze B2": a recomendação do plano era o Google Drive, e
  ele escolheu o B2). O desenho:
  - **O dump.** `pg_dump -Fc --no-owner --no-privileges`, com `set -euo pipefail`, o `PIPESTATUS`
    conferido e um `pg_restore --list` sobre o fluxo (por `tee`) **antes** da cifra. É a correção de um
    defeito do plano: num `pg_dump | age` sem `pipefail`, um dump que falha no meio vira um arquivo
    cifrado truncado e o script diz OK, e o servidor, que só cifra, não consegue decifrar para
    conferir. Nenhum dump em claro vai para o disco da VPS.
  - **A cifra.** `age` para a chave **pública**; o servidor só cifra. A chave privada é só do Mauro:
    gerada no PC dele, dentro de um contêiner, na hora do deploy (o `age` não está no Windows, e nada é
    instalado), e guardada em `%USERPROFILE%\.aurora-whisper-dev\backup` e no gerenciador de senhas.
    O restic ficou de fora: o backup automático pediria a senha do repositório no servidor, e aí quem
    administra a VPS leria o backup, contra "a chave só dele".
  - **O destino.** Um bucket **privado** do B2, com uma *application key* **restrita a esse bucket e
    sem `deleteFiles`**, só com as capacidades de listar e escrever. O servidor sobe arquivos novos e
    não consegue apagar o que subiu: quem invadir a VPS não leva os backups junto (o defeito da opção do
    Drive, onde a chave alcançava o Drive inteiro e o apagado ainda ia para uma lixeira de mais 30
    dias). Os arquivos têm nome único, com a data, então nada é sobrescrito e a chave não precisa de
    mais. **[Corrigido na revisão da Parte 1, 09-out-2026: a chave sem `deleteFiles` sozinha não
    basta.** A `writeFiles`, que ela precisa para subir, também deixa esconder um arquivo
    (`b2_hide_file`, o que o `rclone delete` faz no B2) e subir outra versão com o mesmo nome, e a
    regra de ciclo de vida apaga de vez o que foi escondido: sem mais nada, quem invadir a VPS apaga os
    backups, só que com um dia de atraso. O que segura é o **Object Lock** do bucket, com retenção
    padrão de 30 dias (modo governance, a confirmar), e uma chave só com `listBuckets`, `listFiles` e
    `writeFiles` (sem `bypassGovernance`, `writeFileRetentions` nem `writeBucketRetentions`): nenhuma
    versão sai antes do prazo, nem pela regra, e o Mauro desfaz um esconder com a chave mestra. O
    `deploy/scripts/conferir-b2.sh` confere a chave na VPS antes do primeiro backup; o Object Lock, a
    regra e as capacidades exatas ele confere no painel (`deploy/README.md`, "O bucket e a chave do
    B2").**]**
  - **O giro de 30 dias é a regra de ciclo de vida do próprio bucket**, que o Mauro configura no painel
    do B2; o servidor não apaga nada lá. A regra esconde o arquivo aos 30 dias e o B2 o apaga depois. O
    dossiê e a política dizem "30 dias, mais o tempo que o B2 leva para apagar o que a regra escondeu"
    até ele ver o número no painel.
  - **A ferramenta de envio: o `rclone`, com o backend `b2`, numa imagem pequena à parte** (a imagem do
    backup, abaixo), e não a CLI do B2. Por quê: é um binário único e estático, sem Python nem `pip` numa
    VPS compartilhada; o `copyto` de um nome novo só precisa de listar e escrever, que é o que a chave
    restrita tem; ele confere o que subiu; e a mesma imagem leva o `age` e o cliente do Postgres 18 para
    o PC. A CLI do B2 é Python, mais pesada, e não faz aqui nada que o `rclone` não faça. É ferramenta
    de operação numa imagem separada: **nenhuma dependência nova no servidor Go**.
  - **A restauração** usa a mesma chave: o PC baixa o arquivo (à mão, do painel do B2), decifra no
    contêiner, e o dump decifrado segue por `ssh` até o `pg_restore`, sem arquivo em claro no disco da
    VPS (abaixo, em "Os scripts da VPS").
- **D3. LGPD: o Mauro revisa com um dossiê de duas páginas e um rascunho da política de privacidade,
  escritos pelo agente, e aprova no chat. Confirmada pelo Mauro em 09-out-2026** (a opção A, a
  recomendação do plano). **Os três pontos em aberto ficam como estão, e a política os descreve:**
  (1) a linha do aprendido desfeito, que a nuvem e o backup guardam enquanto a conta existir (D1 da
  F9); (2) as cópias do banco no PC (`antes-do-dono`, `antes-da-nuvem` e `antes-dos-6-meses`), que só
  saem pelo botão (D4 da F10); (3) o Polish, que manda o texto à OpenAI ou à Anthropic com a chave de
  quem usa e segue fora da nuvem e sem liberação (ADR-016). **O país e a região do datacenter ficam em
  branco, marcados** ("[a preencher: país e região do datacenter]"), até ele dizer: o dossiê não
  adivinha. Como nada muda no banco, as migrations 0001 a 0016 podem congelar no primeiro `migrate` em
  produção; se a leitura dele pedir mudança no banco, ela entra **antes** desse `migrate`
  (`migrations/README.md`: depois do deploy, só migration nova).
- **D4. Entrega: a imagem testada no PC vai por `ssh` (`docker save`, e `docker load` no servidor), sem
  `git push`. Confirmada pelo Mauro em 09-out-2026.** O que sobe é exatamente a imagem que passou no
  teste do PC, e não há registro nem credencial nova. O push dos commits (a `main` está 663 à frente de
  `origin/main`) **fica para depois, como decisão à parte** (o repositório privado ou aberto, e o CI).
  Enquanto isso, vale "O que substitui o CI", abaixo.
- **A restauração: aceitar e deixar escrito. Confirmada pelo Mauro em 09-out-2026.** A crítica achou um
  conflito entre documentos, que o `CLAUDE.md` manda levar a ele. O `12` ("Rollback", linhas 72-73) e o
  AC-061 dizem que restaurar um backup troca o `sync_epoch` "sem ressuscitar o que foi apagado depois do
  backup", e o código não garante isso. O epoch novo faz cada PC recomeçar lendo a nuvem do 0, e o
  `gravarRemoto` insere todo ditado vivo da nuvem que o PC não tem (`internal/storage/fila_sqlite.go:
  807`); a lixeira feita depois do backup já voltava assim, e o `Recomecar` o documenta
  (`fila_sqlite.go:296-299`). Logo, **o que foi apagado de vez depois do backup volta ao restaurar**,
  ativo, se ainda estiver dentro dos 6 meses (o que a janela já venceu o `Reter` apaga antes de a porta
  abrir: F11c, abaixo). As saídas eram (a) reaplicar as lápides e a lixeira do dump "antes-de-restaurar"
  e apagar as linhas com lápide no banco mais novo; (b) aceitar o risco e corrigir o texto; (c) o PC
  guardar lápides locais, o que é código novo no Agent. **Ele escolheu a (b).** Consequências, todas
  escritas: o `12` e o AC-061 são corrigidos na F11d (o AC-061 passa a valer para o PC parado, que a F9
  provou, e a restauração ganha a sua própria frase); o roteiro de restauração (Parte 2) diz isso antes
  de ele apertar; e a política de privacidade diz que, depois de uma restauração, o que a pessoa apagou
  desde o último backup pode reaparecer e pode ser apagado de novo. O alcance é limitado: o backup é de
  toda noite, então volta o que foi apagado desde o último, e restaurar só se faz num desastre.

**O que a crítica mudou no plano (desvios).** O que entra na Parte 1 está no desenho abaixo; o que
depende da D1, em "O que fica para depois da D1".
1. **A D1 começa por "qual VPS"**, e não pelo "Traefik do Swarm" (acima).
2. **A ordem "restaurar, reter, abrir" vira código** (F11c), e não "subir o proxy por último". Na A1 e
   na A2 o proxy é o Traefik que já está no ar e encaminha para o contêiner assim que ele existe, e o
   mesmo buraco aparece em qualquer volta de um servidor parado por dias.
3. **O alerta do job não usa o socket do Docker nem lê log** (F11c). O socket é raiz na VPS, e um
   Traefik bem montado não o expõe por isso; e no Swarm um contêiner `unhealthy` é reiniciado e o alerta
   sumiria sozinho.
4. **O backup vai ao B2, sem poder apagar** (D2), e não ao Drive.
5. **O `PROXY_CONFIAVEL` ganha um aviso** (F11c): errar o valor põe todo mundo no mesmo balde de 10
   logins por minuto, em silêncio, e logar só "o proxy está ligado" não pega o erro.
6. **Os segredos têm formato e permissão** (F11f): hex ou base64url, e arquivos que o usuário `nonroot`
   da imagem consegue ler (a imagem distroless roda como uid 65532, e o Docker Desktop do PC mostra
   todo arquivo do NTFS aberto, então esse erro não aparece no teste do PC).
7. **O Postgres da aplicação não é superusuário** (F11e), e não "um usuário só": o usuário que a imagem
   oficial cria é superusuário, e o `COPY ... TO PROGRAM` daria um shell no contêiner.
8. **O `.dockerignore` é uma lista de permissão** (F11e): um build com o contexto na raiz levaria o
   `bin\historico.db`, com texto ditado de verdade, para o contexto e o cache.
9. **O Google de verdade se testa no PC antes do deploy** (`scripts/nuvem-local.ps1`), com uma segunda
   conta para a recusa da Aurora (a seção 7 de `docs/teste-manual-nuvem-f3.md`); "uma conta fora da
   lista entra" é critério de parar o rollout (`12`, "Stop rollout if").
10. **O banco antigo da restauração tem prazo** (F11f): 7 dias, com o `DROP DATABASE` pronto no roteiro,
    que é do Mauro por ser destrutivo, e o prazo entra na política.
11. **O que o CI faria, sem push, é feito por testes locais** (F11h, abaixo), e o "job de CI só com
    `docker build`" do plano sai: ele nunca rodaria.
12. **O teste final troca de ordem** (Parte 2): primeiro sem dado, depois o backup e a restauração com a
    conta que o login criou (e a troca de epoch de verdade), e só então o PC conectado e o Enviar o
    histórico. Um backup de banco vazio prova a cifra, o destino e a chave, mas não a troca de epoch.
13. **A versão do binário por `ldflags`, que o plano punha na F11c, fica de fora**: o `healthcheck` e o
    roteiro não precisam dela, e a `const version` de `cmd/server/main.go:30` não aceita `-X`; trocar
    por `var` é um ajuste à parte, sem pressa.

**O servidor pronto para contêiner (F11c).** Hoje o binário tem `serve`, `migrate` e, só com a tag,
`teste` (`cmd/server/main.go:49-65`).
- **Segredos por arquivo.** `DATABASE_URL_FILE`, `SESSION_SECRET_FILE` e `DEVICE_TOKEN_PEPPER_FILE`
  (hoje as três só vêm do ambiente: `config.go:98-117`; só o JSON do Google vem de arquivo). A variável
  comum continua valendo, e **as duas juntas dão erro de configuração**: o erro diz o nome das duas
  variáveis, nunca o valor nem o caminho (o molde do `lerClientGoogle`, `config.go:182-194`). O arquivo
  é regular, com um teto pequeno de tamanho; o leitor tira só o fim de linha (`\n` ou `\r\n`) e recusa
  o arquivo vazio. As regras de hoje (pelo menos 32 caracteres, o `SESSION_SECRET` diferente do pepper)
  valem depois da leitura. O `migrate` usa a mesma leitura para a `DATABASE_URL` (`migrate.go`). Os
  valores nascem em hex ou base64url, que não pedem escape dentro da URL do banco.
- **`healthcheck`.** Um subcomando (`aurora-whisper-server healthcheck`) que faz `GET` em
  `http://127.0.0.1:<porta do HTTP_ADDR>/api/v1/health/live`, com prazo curto e sem seguir
  redirecionamento, e sai com 0 (resposta `200`) ou 1. A imagem final não tem shell nem `curl`, então o
  `HEALTHCHECK` é o próprio binário.
- **A saúde do job, sem socket e sem ler log.** A API guarda em memória a hora e o resultado da última
  rodada do `Reter` (a que sai na linha `retenção`) e os expõe numa rota **que só responde a quem vem
  de loopback**: o `RemoteAddr` do proxy (outro contêiner ou outro servidor, em todos os desenhos desta
  ADR) nunca é loopback, então por ele a rota dá `404`. A resposta tem só números e booleanos. O
  `healthcheck -retencao` a lê e **sai com 1 se a última rodada tem mais de 2 h, ou se houve falha de
  categoria `banco` ou `outro`** (a `contexto` é o desligamento no meio de uma rodada e fica de fora: é
  a regra do alerta duplo, `docs/ai-spec/11-observability.md:111-118`).
  Como o subcomando roda dentro do contêiner, quem o usa é o `healthcheck` do compose da API (Parte 2),
  no lugar do da imagem; o `HEALTHCHECK` da imagem continua sendo o de vida, para nada ser reiniciado
  onde o `unhealthy` reinicia (Swarm). Como a primeira rodada vem antes da porta (abaixo), a rota nunca
  responde "ainda sem rodada". **[A revisão da Parte 2 (09-out-2026) tirou o `-retencao` do healthcheck
  do compose: o provider docker do Traefik não roteia um contêiner que não está `healthy`, e a saúde do
  job derrubaria o endereço. O compose usa o de vida, e o `-retencao` roda por `docker exec` no
  `subir.sh` e na conferência do mês. Ver "A revisão da Parte 2".]**
- **A primeira rodada de Arrumar e Reter, síncrona, antes do `net.Listen`.** Hoje o `serve` abre a
  porta (`serve.go:114`) e dispara a arrumação e a retenção num goroutine (`serve.go:140-147`;
  `internal/server/nuvem.go:309-311`), então, depois de uma restauração ou de dias parado, o texto
  vencido aparece até a primeira rodada. Depois da correção, o `runServe` roda `Arrumar` e `Reter` uma
  vez, em ordem, com o `ctx` do processo (e não com o `startupTimeout` de 30 s, que cobre só o pool e a
  conferência das migrations), **antes** de abrir a porta; só então o `ArrumarSempre` segue com o tique
  de hora em hora, sem repetir a primeira. A ordem "restaurar, reter, abrir" passa a valer **sem
  depender do proxy**, e o roteiro fica como segunda barreira. O `start_period` do `HEALTHCHECK` cobre
  a espera. O servidor de teste (`teste`) continua como está. O que fazer se a rodada falhar com
  categoria `banco` ou `outro` é a escolha menor 4.
- **O aviso do `X-Forwarded-For`.** No ponto por onde todo pedido passa, um WARN **único** (uma vez por
  processo, sem IP e sem valor) quando chega `X-Forwarded-For` de um `RemoteAddr` fora do
  `PROXY_CONFIAVEL` (`internal/server/cloudauth/ip.go:45`; `internal/server/seguranca.go:93-97`). É ele
  que pega o valor errado, que hoje falha em silêncio. A linha `servindo` passa a dizer também se o
  proxy confiável está ligado (um booleano).
- **O build de produção continua sem a tag `logindeteste`**: o Dockerfile não passa `-tags`, e o teste
  estático da imagem confere.
- **Documentos:** o `10-env-and-secrets.md` e o `.env.example` ganham as três variáveis `_FILE`.
- **Feita (09-out-2026), no desenho acima.** Onde mora: `cmd/server/config.go` (`lerSegredo`, a
  leitura do `_FILE`, e `lerDatabaseURL` em `migrate.go`, a mesma no `serve` e no `migrate`),
  `cmd/server/healthcheck.go` (o subcomando), `cmd/server/serve.go` (`abrir`: a primeira rodada, a
  porta, a linha `servindo` com `proxy_confiavel`) e `internal/server/saude.go` (a rota
  `GET /api/v1/health/retencao`, a última rodada guardada e o aviso do `X-Forwarded-For`); o `Reter`
  devolve o resumo da rodada (`RodadaDaRetencao`), e o servidor de teste segue com a `ArrumarSempre`.
  O que a implementação acrescentou ao desenho, sem mudar nada dele: o arquivo do segredo é de **uma
  linha só** (o de mais de uma é recusado, além do vazio), com teto de 4 KiB; o `healthcheck` sai só
  com 0 ou 1 também no uso errado (o 2 é reservado pelo Docker); a rota da saúde do job recusa também
  quem traz `X-Forwarded-For`, `Forwarded` ou `X-Real-Ip` mesmo pelo loopback (um proxy na mesma
  máquina os acrescenta); o SIGTERM no meio da primeira rodada sai com 0 sem abrir; e a falha da
  arrumação na primeira rodada não impede a subida (só a da retenção, que é a do texto vencido). Os
  testes: os do `_FILE` (o fim de linha `\n` e `\r\n`, o vazio, o de duas linhas, o grande, a pasta,
  as duas juntas, a regra dos 32 caracteres depois da leitura, nada de valor nem caminho na mensagem;
  no `serve` e no `migrate`); o `healthcheck` contra o `httptest` (200, 503, o prazo, o
  redirecionamento não seguido, ninguém escutando) e o `-retencao` contra a nuvem de verdade com o
  relógio andando (2 h vale, 2 h e 1 s não; a falha de outra categoria dá 1, a de contexto não); a rota
  só pelo loopback; **enquanto o teste segura a primeira rodada, a porta recusa conexão, o `onListen`
  não chega e a linha `servindo` não sai; solta, a ordem é `arrumação`, `retenção`, `servindo`**; a
  falha grave na primeira rodada sai com 1 sem abrir; o WARN uma vez só, sem IP. **Mutações, todas
  mortas (17):** a rodada de volta para depois do `Listen`; o WARN a cada pedido; o redirecionamento
  seguido; o limite de 2 h com `>=`; o contexto contado como grave (no resumo e no `healthcheck`); o
  `TrimSpace` no lugar do fim de linha; as duas linhas aceitas; a rota sem a conferência do loopback e
  sem a dos cabeçalhos de proxy; a falha grave ignorada no `abrir`; as duas variáveis juntas aceitas;
  o `migrate` sem o `_FILE`; a linha `servindo` sem o `proxy_confiavel` (morta no teste do Postgres); a
  rodada não guardada; e o `healthcheck` aceitando zero rodada e falha grave. Verde no Postgres 18 de
  teste (o `serve` sobe com a linha `retenção` antes da `servindo`).

**Depois de restaurar (F11d).** Um subcomando, `aurora-whisper-server pos-restauracao`, que **exige
`-confirmo`** (sem ele, sai com 2 e não toca em nada). Numa transação só: troca o `sync_epoch` de
**todas as contas**, cada uma por um `NewUUIDv7`, pelo mesmo comando que o "apagar meus dados" usa
(`internal/server/pgstore/contas.go:83-85, 328`), e **revoga as sessões e os aparelhos** (o token do PC
vale enquanto o aparelho não está revogado). Imprime só contagens (contas, sessões, aparelhos), nunca
id, e-mail nem nome. A revogação vem junto porque o backup guarda o `revoked_at` antigo: sem ela, um
aparelho revogado depois do backup voltaria a valer (escolha menor 3).
- **O teste de ponta a ponta, em memória** (`internal/server/integration`): o backup é **mais velho**
  que o PC. Os testes de epoch de hoje trocam o epoch com os mesmos dados
  (`sync_e2e_test.go:449-457`). O cenário: a nuvem com o estado do backup; depois dele, no PC, um
  ditado editado, um posto na lixeira, um excluído de vez, um novo, e um aparelho pareado; a nuvem
  volta ao estado do backup, o `pos-restauracao` roda, e o PC faz uma rodada. O teste afirma o que tem
  de valer (o PC recomeça pelo epoch novo **sem selo falso**, sem duplicar e sem perder a edição que só
  ele tinha; o aparelho pareado depois do backup e as sessões recebem `401`) **e o que o Mauro aceitou**
  (o excluído de vez volta ativo; a lixeira feita depois do backup volta pelo pull do 0): nada é
  escondido, e o teste é a prova escrita da restauração.
- **O limite que a F7 deixou para aqui** (decisão 2 de "Decisões da F7a/b"): com a base vazia, a nuvem
  compara contra tudo, e um ditado cujo texto difere do backup e cujo último texto veio de outro
  aparelho vira selo; um selo pendente pode ser trocado; e a primeira rodada depois do backup ainda
  manda a fila com as bases do epoch velho. Se o teste mostrar isso, ou outro defeito (selo sem conflito
  de verdade, duplicata, edição do PC perdida), a F11d **para** e leva as opções ao Mauro, sem consertar
  calada. A ressurreição do apagado depois do backup, essa, já está decidida e escrita.
- **O mesmo cenário no Postgres de teste** (a suíte de contrato e o `pgstore`), **se o arquivo
  `%USERPROFILE%\.aurora-whisper-dev\postgres-de-teste.url` existir**: o agente só o lê para pôr o
  endereço na variável do processo de teste e nunca imprime o conteúdo. Sem o arquivo, os testes de
  Postgres pulam, e a entrega diz quais não rodaram.
- **Documentos:** o AC-061 em `03-acceptance-criteria.md` e as linhas 72-73 do `12-rollout.md`, como na
  decisão da restauração.
- **Feita (09-out-2026), menos um ponto que parou e foi ao Mauro (abaixo).** Onde mora:
  `internal/server/cloudstore/restauracao.go` (a interface `Restaurador`, com `DepoisDeRestaurar`, e
  as contagens `Restauracao`), a do Postgres em `internal/server/pgstore/restauracao.go` (trava os
  contadores, o `sqlNovoEpoch` por conta, `DELETE` das sessões, `revoked_at` e a versão mais um nos
  aparelhos ainda não revogados, `DELETE` dos tokens, numa transação), a da memória em
  `memory_restauracao.go` (com a foto do `MemoryStore`, `Fotografar` e `Restaurar`, só para os
  testes: o `pg_dump` e o `pg_restore`), e o subcomando em `cmd/server/posrestauracao.go`, que também
  **recusa um banco com migration pendente** (rode o `migrate` antes) e lê a URL como o `migrate` (a
  variável ou o `_FILE`). A saída: `pos-restauracao: N conta(s) com o epoch novo, N sessão(ões)
  derrubada(s), N aparelho(s) revogado(s)` e o lembrete de reconectar, nada mais.
  **O que o ponta a ponta provou** (`internal/server/integration/sync_restauracao_e2e_test.go`, o
  Agent de verdade; o backup mais velho que o PC): o PC, o PC pareado depois do backup e a sessão do
  celular recebem `401`; o PC conecta de novo e recomeça pelo epoch novo, sem selo, sem duplicar, com
  a fila vazia e as versões batendo; a edição que só ele tinha sobe e vale, a de depois do backup e
  **a feita desconectado depois da restauração** (com a base do epoch velho, inclusive maior que a
  versão do banco restaurado); o ditado novo sobe. **Melhor que o previsto:** a lixeira que o PC tinha
  (a dele e a do celular que ele recebeu) **fica na lixeira**, porque a reconciliação do pull (F7c,
  D3) a sobe de novo; só volta fora da lixeira a que **só a nuvem** tinha (o celular pôs e o PC não
  chegou a receber). **O aceito, afirmado no teste:** o excluído de vez depois do backup volta vivo,
  ao PC e à nuvem. **O selo de verdade** que o backup guarda continua com os dois PCs recomeçando (o
  "selo pendente pode ser trocado" da F7 não aconteceu). O mesmo cenário, no nível do Store, roda na
  suíte de contrato contra a memória e o **Postgres 18 de teste** (`Restauracao_*`, com a foto feita
  por cópia das tabelas e a restauração no lugar), mais os testes de números exatos dos dois
  (`TestMemoria_DepoisDeRestaurarEAFoto`, `TestDepoisDeRestaurar_Postgres`) e do subcomando no
  Postgres (sem `-confirmo` nada muda; com ele, só as contagens). **Mutações: 16 mortas** (o epoch que
  não troca, na memória e no Postgres; os aparelhos sem revogar e revogados de novo; as sessões e os
  tokens que ficam; a marca d'água que não sobe no Postgres; a foto rasa; o `-confirmo` ignorado; a
  migration pendente ignorada; a URL impressa) e **1 equivalente** (a marca d'água da memória, que
  ninguém lê).
  **O que parou e foi ao Mauro: o limite da F7, confirmado.** Um ditado cujo último texto, no backup,
  veio de **outro aparelho** (o celular editou antes do backup) e que o PC editou depois do backup: a
  nuvem volta ao texto do celular, o PC recomeça e sobe o dele com a base vazia, e a nuvem, sem saber
  que a edição do PC partiu do texto do celular, faz um **selo sem conflito de verdade**: o texto
  principal fica o do celular, e o do PC vai para o selo, nos dois lados. A edição não se perde (está
  no selo, e "trocar" a recupera), mas deixa de ser o texto principal. O
  `TestSyncE2E_Restauracao_LimiteDaF7_SeloSobreOTextoDoCelular` fixa o comportamento de hoje, sem
  consertar. As opções: **(a)** aceitar e deixar escrito, como a ressurreição (restaurar só se faz num
  desastre; o roteiro de restauração e a política dizem que pode aparecer um selo, e a pessoa escolhe
  o texto); **(b)** consertar no PC numa fatia própria: antes de zerar as versões no `Recomecar`, o PC
  guarda, por ditado, o texto da última versão da nuvem que ele conhecia; o push do recomeço o manda, e
  a nuvem, se o texto dela é igual a ele, aplica sem selo (uma migration do SQLite, um campo novo no
  push, a regra da nuvem e o `06`). **Sugestão do agente (a decisão é dele): a (a) agora**, porque
  nada se perde e a restauração é rara, com a (b) anotada para depois. Até ele responder, o AC-061
  diz "pendente do Mauro" e nada muda no código.

**A imagem (F11e).**
- **`deploy/Dockerfile`, em estágios.** (1) O painel: Node 24 e pnpm 11.5.1 (o do `web/pnpm-lock.yaml`),
  `pnpm install --frozen-lockfile` e `pnpm build:nuvem`, que gera `web/dist-nuvem` (e não `pnpm build`,
  que escreve em `../bin/painel`). (2) O servidor: Go 1.27, `CGO_ENABLED=0`, `-trimpath` e **sem
  `-tags`**, o binário estático de `./cmd/server`. (3) A final: `distroless/static` em `nonroot` (uid
  65532), que traz as CAs do sistema (o servidor fala TLS com o Google), com o binário, o painel em
  `/painel` e `HTTP_ADDR=0.0.0.0:8080`, `PAINEL_DIR=/painel` e `LOG_LEVEL=info` como padrão da imagem
  (com `warn` ou `error` a linha `retenção` some). O `HEALTHCHECK` chama o `healthcheck` em formato
  exec, com um `start-period` que cobre a primeira rodada. As versões do Node, do pnpm e do Go são as
  do repositório (`go.mod`, `.github/workflows/ci.yml`, `web/pnpm-lock.yaml`), em tags exatas, nunca
  `latest`.
- **`.dockerignore` de lista de permissão:** `*`, e depois `!cmd/`, `!internal/`, `!migrations/`,
  `!web/`, `!go.mod` e `!go.sum`, com `web/node_modules` e `web/dist*` de fora. O resto do repositório
  não entra no contexto: o `bin\` (com o `historico.db` de verdade), o `dist\`, o `models\`, o
  `thirdparty\`, os `.exe` e qualquer `*.db`.
- **Prova (estática, em Go):** um teste lê o Dockerfile e o `.dockerignore` e confere as promessas:
  nada de `-tags`, `CGO_ENABLED=0`, `-trimpath`, a final `static` em `nonroot`, o `HEALTHCHECK` pelo
  subcomando, `pnpm build:nuvem`, sem `COPY . .`, sem `ADD` de URL, sem `curl | sh`, sem nome de
  segredo em `ENV` ou `ARG`, as versões iguais às do repositório; e aplica as regras do
  `.dockerignore` (a última que casa vence) a `bin/historico.db`, `models/x.gguf`, `dist/Setup.exe`,
  `thirdparty/x`, `web/node_modules/x` e `web/dist-nuvem/index.html`, que têm de ficar de fora. **O
  build de verdade é o do Mauro**, no teste do PC (Parte 2): o agente não roda Docker.
- **Feita (09-out-2026), no desenho acima.** `deploy/Dockerfile` e o `.dockerignore` da raiz; a prova
  em `deploy/imagem_test.go` (o pacote `deploy` só tem testes). As versões: `node:24.14.0-slim` com
  `pnpm@11.5.1` (instalado pelo `npm`, que confere a integridade pelo registro), `golang:1.27.0` (a do
  `go.mod`) e `gcr.io/distroless/static-debian12:nonroot`, que não tem tag de versão: **a fixação por
  digest fica para o primeiro build do Mauro**, com o digest que ele vir. O binário vai para
  `/usr/local/bin/aurora-whisper-server`, de dono root (o `nonroot` lê e não altera), com `-ldflags="-s
  -w"`. O que a implementação acrescentou ao desenho: o `.dockerignore`, depois da lista, tira de
  dentro das pastas permitidas o que não é do build (`node_modules`, bancos, `.exe`, `.env`, chaves,
  `.age`, dumps, modelos, `.wav`); e a prova vai além das amostras: (1) roda o `go list -deps` do
  `./cmd/server` para o Linux, sem compilar, e confere que **todo arquivo Go e todo `go:embed`** que o
  build usa entra no contexto; (2) anda pela árvore de verdade (no PC do Mauro, com o `bin\`) e
  confere que tudo o que entra é de uma pasta permitida e não é banco, executável, chave nem dump, e
  que tudo o que o build do painel lê em `web/` entra; (3) o próprio compilador de padrões do teste é
  conferido contra as regras do Docker. Conferido à parte, sem Docker: o `tsc -b` do `build:nuvem`
  passa sem o `docs/` (o `marca.test.tsx` importa `../../docs/marca/*.svg?raw`, que o TypeScript
  resolve pela declaração `*?raw` do Vite, exista o arquivo ou não), e o `go build -trimpath` com
  `CGO_ENABLED=0` sai para `linux/amd64` e `linux/arm64`. **Mutações: 20 mortas** (o `-tags
  logindeteste`, o `pnpm build`, o `CGO_ENABLED=1`, o `USER root`, a final em `latest`, o Go e o pnpm
  de outra versão, o `COPY . .`, o HEALTHCHECK sem `start-period`, o `LOG_LEVEL=warn`; no
  `.dockerignore`, oito regras de dentro tiradas uma a uma, o `!deploy/` a mais e o `!migrations/` a
  menos); as três que sobreviveram na primeira passada (a regra de `node_modules` fora do `web/`, a do
  `.env.*` e a do `.exe`) morreram com amostras novas.

**O Postgres (F11e).**
- **Um script de init** (`deploy/postgres/init/`, que a imagem oficial roda uma vez, na criação do
  volume) cria o papel da aplicação, `aurora`, `NOSUPERUSER NOCREATEDB NOCREATEROLE`, e o banco
  `aurora`, de dono dele. A senha vem do arquivo de segredo e entra pela entrada padrão do `psql`,
  nunca pela linha de comando. **A API e o `migrate` usam esse papel** (as migrations não precisam de
  extensão nenhuma); o superusuário que a imagem cria fica **só para administrar** (criar e renomear
  bancos na restauração, conferir os parâmetros). Continua sendo um usuário só para a aplicação.
- **A conferência dos 5 parâmetros de log por `SHOW`** (`deploy/postgres/conferir-postgres.sh`, para o
  Mauro rodar na VPS antes do primeiro dado real): `log_error_verbosity = terse`, `log_statement =
  none`, `log_min_duration_statement = -1`, `log_parameter_max_length_on_error = 0` e `auto_explain` fora
  do `shared_preload_libraries` (a ADR, em "Segurança e privacidade", manda os cinco). Confere também
  que o papel `aurora` não é superusuário e é o dono do banco. Imprime `OK` ou `FALHOU` com o nome do
  parâmetro e o valor que achou (que não é segredo). Os cinco são aplicados por `-c` no comando do
  Postgres, no compose (Parte 2, com teste estático).
- **Feita (09-out-2026).** `deploy/postgres/init/10-papel-da-aplicacao.sh` e
  `deploy/postgres/conferir-postgres.sh`; a prova em `deploy/postgres_test.go`, com o `psql` e o
  `docker` de mentira (`deploy/bash_test.go`: o bash de verdade, no Windows o do Git, com um `PATH`
  só dos falsos e de uma lista fechada de utilitários, numa pasta temporária; um teste confere que
  esse `PATH` não alcança `docker`, `psql`, `ssh`, `curl` nem `sudo`). **Um desvio do desenho, a
  confirmar:** o papel `aurora` e o superusuário têm **senhas diferentes** (`postgres-aurora-senha` e
  `postgres-superusuario-senha`, as duas do `gerar-segredos.sh`). O desenho dizia "a senha do Postgres
  é a mesma no `POSTGRES_PASSWORD_FILE` e na `DATABASE_URL`", o que vinha de antes do papel
  `NOSUPERUSER`: com a mesma senha, quem tirasse a `DATABASE_URL` da API entraria como superusuário, e
  o papel sem poder não protegeria nada. O que a implementação acrescentou: a senha não entra nem pela
  entrada do `psql` (o desenho dizia "pela entrada padrão"): o `psql` a lê do arquivo com `\set senha`
  e crase, e o `CREATE ROLE` a recebe como literal (`:'senha'`); o init recusa uma senha que não seja
  uma linha só de 64 hexadecimais e um `POSTGRES_DB=aurora` (a imagem criaria o banco com o dono
  errado), antes de chamar o `psql`; o papel também é `NOREPLICATION` e `NOBYPASSRLS`, e o banco
  `REVOKE ALL ... FROM PUBLIC`; e o corpo roda num subshell, porque a imagem faz `.` (source) do init
  que não é executável (o checkout no Windows não guarda o `+x`), e o `set -u` e o `exit` vazariam para
  o script dela (provado com um "script da imagem" de mentira, com `set -Eeo pipefail`). O `conferir`
  olha também o `session_preload_libraries` e os cinco poderes do papel, para antes de consultar se o
  contêiner não responde e só faz `SHOW` e `SELECT` (conferido no registro dos falsos). **Mutações: 17
  mortas** (o papel `SUPERUSER`, o init fora do subshell, a senha de duas linhas aceita, a senha na
  linha de comando, o `POSTGRES_DB` sem conferir, o `REVOKE` e o `\unset` tirados; no `conferir`, três
  valores esperados trocados ou tirados, o `auto_explain` sem conferir, o `session_preload_libraries`
  fora, o resumo sem o `exit 1`, o papel inexistente, o dono errado e o `BYPASSRLS` aceitos, e a
  consulta que segue com o contêiner fora); as duas últimas sobreviveram na primeira passada e
  morreram com testes novos.

**Os scripts da VPS (F11f).** Bash, só ASCII, fim de linha LF (`.gitattributes`), `set -euo pipefail`,
imprimem `OK` ou `FALHOU` e nunca um segredo: sem `set -x`, sem eco de valor, sem segredo na linha de
comando, em `deploy/scripts/`. Os nomes de contêiner, de rede e de volume que eles usam são variáveis
no topo, com o padrão `aurora-whisper-*`; o compose da Parte 2 os adota, e o teste estático da Parte 2
confere que os dois combinam.
- **`gerar-segredos.sh`.** Gera a senha do Postgres, o `SESSION_SECRET` e o `DEVICE_TOKEN_PEPPER` de
  `/dev/urandom`, em hex ou base64url, numa pasta `0700` do usuário, **com os arquivos em `0444`**: a
  imagem roda como `nonroot`, e um arquivo `0600` do usuário do host não seria lido ("não consegui
  abrir"). A senha do Postgres é a mesma no arquivo do `POSTGRES_PASSWORD_FILE` e dentro do arquivo da
  `DATABASE_URL` (o nome do serviço do Postgres é fixo no script, `aurora-whisper-postgres`, e o compose
  da Parte 2 usa o mesmo, conferido por teste). **Recusa sobrescrever** (se qualquer arquivo existe,
  falha sem mudar nada). Pergunta o domínio e o e-mail permitido e grava um `config.env` `0600`, **fora
  do repositório**, com o `PUBLIC_BASE_URL` e o `CONTAS_PERMITIDAS`: não são segredo, mas também não vão
  para o repositório.
- **`backup.sh`** (o da D2): `pg_dump` pelo papel da aplicação, a conferência do fluxo, o `age`, o
  arquivo `.partial` renomeado só no fim, o `rclone copyto` ao B2, a conferência do que subiu e, só
  depois dela, a hora do último sucesso gravada (para o `healthcheck` do contêiner do backup, que falha
  se o backup tem mais de 26 h: Parte 2). Uma linha de log por execução, sem nome de arquivo do
  usuário, sem segredo. `flock` contra execução dupla. O cifrado fica no volume do backup por 3 dias,
  para reenviar se o B2 falhar (escolha menor 5).
- **`restaurar.sh`** (para o Mauro rodar, com a aprovação que o `09` exige): recusa se a API está de
  pé; faz antes um dump cifrado "antes-de-restaurar" do banco atual; recebe o dump já decifrado pela
  entrada padrão (a decifra é no PC, e a chave privada não sai dele; o dump passa por um arquivo em
  memória, `/dev/shm`, para ser conferido com `pg_restore --list` antes de tocar no banco, e é apagado ao
  fim); restaura num banco **novo**, sem `--clean` e sem `DROP`; só então **renomeia**: o atual vira
  `aurora_antes_AAAAMMDD` e o novo toma o nome `aurora`; roda o `migrate -status` e, depois de uma
  pausa em "digite SIM" (migration em produção é ponto de parada), o `migrate`; roda o
  `pos-restauracao -confirmo` num contêiner descartável da imagem da aplicação; e termina mandando subir
  a API pelo roteiro (a subida é do compose, Parte 2; a API roda a retenção antes de abrir a porta).
  **Nunca apaga o banco antigo**: o script imprime o `DROP DATABASE` para daqui a 7 dias, que o Mauro
  roda, e o prazo entra na política. Cada passo confere o anterior e para no primeiro `FALHOU`.
  **[Revisão da Parte 1, 09-out-2026: a memória é a pasta só do usuário (`/run/user/<uid>`), e não o
  `/dev/shm`, com prazo de 2 h; o `migrate` e o `pos-restauracao` rodam no banco novo, e a troca de
  nomes é o último passo. Ver "A revisão da Parte 1", abaixo.]**
- **A imagem do backup** (`deploy/backup/Dockerfile`): o cliente do Postgres 18 (o mesmo major do
  servidor), o `age` e o `rclone`, em versões exatas. É a do sidecar da VPS e a do PC (a chave e o
  teste mensal).
- **Prova:** `bash -n`; testes estáticos em Go no molde de `internal/server/pgtest/script_test.go`
  (que lê o texto do script e confere as promessas): `set -euo pipefail`, o `PIPESTATUS` e o
  `pg_restore --list` no `backup.sh`, nenhum dump em claro em disco, nenhuma senha na linha de comando,
  os modos `0700`, `0444` e `0600`, a codificação dos segredos, `--exit-on-error` e a falta de `--clean`
  e de `DROP` no `restaurar.sh`, só ASCII e LF; e, **quando der, o bash executado com programas de
  mentira num `PATH` só deles, sem rede** (`docker`, `pg_dump`, `pg_restore`, `psql`, `age`, `rclone`,
  como o `scripts/README.md` conta do diagnóstico, numa pasta temporária): um dump que **falha no meio**
  (não pode subir nada nem deixar arquivo), um dump vazio, uma chave pública inválida, um `rclone` que
  falha, os segredos que já existem (nada sobrescrito), a API de pé na restauração, e a saída sem
  nenhum valor de segredo.
- **Feita (09-out-2026).** `deploy/scripts/gerar-segredos.sh`, `backup.sh` e `restaurar.sh`, a imagem
  do backup em `deploy/backup/Dockerfile` (com o `Dockerfile.dockerignore` que só deixa entrar o
  `backup.sh`); a prova em `deploy/scripts_test.go`: o texto, o `bash -n` de todo `.sh` do `deploy/` e
  do diagnóstico, e o bash de verdade com o `docker`, o `psql`, o `pg_dump`, o `pg_restore`, o `age`, o
  `rclone` e o `flock` de mentira (`deploy/bash_test.go`). **Desvios do desenho, a confirmar:**
  1. **O `restaurar.sh` tem três comandos, e não um:** `receber` (o dump decifrado chega pela entrada do
     `ssh`, vai para `/dev/shm` e é conferido com o `pg_restore --list`; nada muda no banco), `aplicar`
     (com `ssh -t`: as confirmações "RESTAURAR" e "SIM") e `descartar`. Num comando só, a entrada do
     `ssh` seria ao mesmo tempo o dump e o teclado: com `-t`, o terminal estragaria os bytes do dump;
     sem ele, não há como digitar. Uma recusa do `aplicar` antes da confirmação (a API de pé, por
     exemplo) deixa o dump na memória para tentar de novo; depois dela, o dump sai da memória ao fim,
     de certo ou de errado. O `receber` recusa uma `/dev/shm` que não é `tmpfs`.
  2. **O `gerar-segredos.sh` pergunta também o bucket, a keyID e a application key do B2 (esta sem eco)
     e a chave pública do `age`**, e grava o `rclone.conf` (o remoto `b2`) e a `backup-chave-publica` em
     `segredos/`, e o `B2_BUCKET` no `config.env`: o desenho só citava o domínio e o e-mail, e a chave do
     B2 tinha de chegar ao servidor "pelo roteiro, nunca ao chat". Tudo é conferido antes de gravar
     qualquer coisa (a chave privada do `age` colada no lugar da pública é recusada).
  3. **Duas senhas do Postgres** (o papel `aurora` e o superusuário), como em "O Postgres (F11e)".
  4. **O `aplicar --sem-copia-antes`** pula a cópia cifrada do banco atual, para o desastre em que ele
     nem consegue ser lido; o banco atual continua guardado pelo nome (`aurora_antes_AAAAMMDD`).
  O que a implementação acrescentou: o `backup.sh` exige no índice do dump as tabelas `users` e
  `schema_migrations` (um dump do banco errado não sobe); confere os `PIPESTATUS` da ponta para o começo
  (quando o `age` morre, o `pg_dump` morre depois dele de `SIGPIPE`, e a causa é o `age`); o `pg_restore
  --list` lê só o começo do fluxo, e um `cat` drena o resto, para o `tee` não morrer no meio; reenvia na
  vez seguinte o que não subiu, e nunca apaga do volume o que não subiu; apaga as sobras de uma chamada
  morta no meio (o `.partial` e a pasta de trabalho com mais de um dia); e a senha vai ao `pg_dump` pelo
  `PGPASSWORD` do ambiente. O `restaurar.sh` faz a cópia "antes de restaurar" com a mesma conferência do
  `backup.sh`, guarda-a cifrada em `~/aurora-whisper/antes-de-restaurar/` e imprime o `rm` dela junto
  do `DROP` dos 7 dias; o `psql` dele nunca usa `docker exec -i` (que repassaria ao contêiner as
  respostas do teclado; o `docker` de mentira repassa, como o de verdade); a API descartável do
  `migrate` e do `pos-restauracao` roda na rede interna, `--read-only`, sem capacidades, só com a
  `database-url` montada; se a segunda troca de nome falha, a primeira é desfeita; se o `pg_restore`
  falha, o banco novo vazio fica, com o `DROP` dele impresso para o Mauro; e o `migrate` só roda se o
  `migrate -status` mostrar migration pendente. A imagem do backup: o `age` v1.2.1 por `go install`
  (conferido pela base de checksums do Go), com o Go do repositório; o `rclone` da imagem oficial
  `rclone/rclone:1.70.0`; a final `postgres:18.0-alpine`, como o usuário `postgres` (uid 70), com o
  `backup.sh`; e o próprio build confere que o `bash`, o `flock`, o `pg_dump`, o `age` e o `rclone`
  estão lá. **As tags exatas (`1.70.0`, `18.0-alpine`, `v1.2.1`) não foram conferidas num registro:** o
  primeiro build do Mauro é a prova, e o mais novo de cada uma pode entrar no lugar. O `deploy/Dockerfile`
  perdeu o `# syntax=docker/dockerfile:1`, que baixaria o frontend do build por uma tag que anda.
  **Mutações: 45 mortas e 1 equivalente** (no `backup.sh`, 16: sem o `wait`, sem o dreno, cada
  `PIPESTATUS`, a lista sem `users`, o tamanho sem conferir, o que não subiu apagado, o prazo de 3 dias,
  a senha fora do ambiente, sem o `flock`, a falha do envio ignorada, sem o `.partial`, sem o trap, a
  senha vazia aceita e as sobras sem apagar; no `restaurar.sh`, 17: cada recusa do `aplicar` e do
  `receber`, a confirmação, o `SIM`, o desfazer da troca de nomes, os dois traps, o `psql` com `-i`, o
  banco atual não visto, o `--sem-copia-antes` ignorado, o `DROP` do banco vazio não impresso e o
  `pos-restauracao` ignorado; no `gerar-segredos.sh`, 12: a recusa de sobrescrever, as duas recusas de
  pasta, cada conferência de resposta, a URL com a senha do superusuário, o `0444` e a pasta temporária
  esquecida); quatro sobreviveram na primeira passada (o `wait`, o `.partial`, a pasta temporária e uma
  que não tinha aplicado) e morreram com testes novos (o dump pequeno com o `pg_restore --list` devagar,
  o backup morto por `kill -9` no meio da cifra, o `chmod` que falha no meio da gravação). A
  equivalente: a guarda que poupa a pasta de trabalho da própria chamada na limpeza (ela é nova e nunca
  tem mais de um dia). **Nada disso rodou num Linux:** o bash é o do Git no Windows, com programas de
  mentira; a primeira execução de verdade é a do Mauro.

**Os scripts do PC (F11g).** PowerShell 5.1, só ASCII, CRLF (`.gitattributes`), caminhos completos. **O
agente só escreve; quem roda é o Mauro.**
- **`scripts\backup-chave.ps1`**: gera o par do `age` **dentro de um contêiner** da imagem do backup,
  com a pasta `%USERPROFILE%\.aurora-whisper-dev\backup` montada, e mostra **só a chave pública**.
  Recusa uma pasta dentro do repositório e recusa gerar de novo se já houver chave (uma chave nova
  invalidaria os backups em curso).
- **`scripts\teste-restauracao-mensal.ps1`**: recebe o arquivo cifrado que ele baixou à mão do B2, decifra
  num contêiner, restaura num Postgres 18 **descartável** (sem porta publicada, senha aleatória na hora,
  removido ao fim) e confere que o banco abre e tem as tabelas e as contas esperadas; **falha se o backup
  tem mais de 48 h** (o nome do arquivo traz a data), que é também o que mostra que o backup parou.
- **`.gitignore`:** passa a cobrir as chaves do `age`, os dumps e o `config.env` (`*.age`, `*.dump`,
  `config.env`, e as pastas `/backup/` e `/segredos/`); o `*.key` que já existe cobre o nome padrão da
  chave.
- **Prova:** teste estático no molde do `script_test.go` (só ASCII, CRLF, sem `&&` nem `||`, o contêiner
  só com a pasta do backup montada, sem `--privileged` nem `--network host`, a recusa de uma pasta
  dentro do repositório, a chave privada nunca impressa, o contêiner descartável removido, as linhas
  novas do `.gitignore`); e, se o `powershell` estiver à mão, o **parser** do PowerShell sobre os
  arquivos (ele lê, não executa). Nenhum script é executado pelo agente.
- **Feita (09-out-2026), no desenho acima.** `scripts/backup-chave.ps1` e
  `scripts/teste-restauracao-mensal.ps1`; a prova em `deploy/pc_test.go`, com o parser do PowerShell 5.1
  (no Windows) sobre os dois e o `postgres-de-teste.ps1`. A imagem é a do backup
  (`aurora-whisper-backup:producao`), que os dois constroem de `deploy/backup/Dockerfile` se ela ainda
  não existe (o Docker baixa as imagens de base: é a primeira execução do Mauro). O que a implementação
  acrescentou ao desenho: os contêineres do PC rodam **sem rede nenhuma** (`--network none`), além de
  sem porta e sem privilégio; a chave é gerada com `age-keygen -o` numa montagem gravável e a pública é
  lida de volta com `age-keygen -y` numa montagem só de leitura, conferida contra a forma `age1...` antes
  de aparecer (nada do que o Docker diz é impresso); o teste mensal usa a **própria imagem do backup**
  como o Postgres descartável (ela é a `postgres:18.x-alpine` com o `age` e o `rclone`), monta só a
  chave e o arquivo, só para leitura, e decifra e restaura **dentro do contêiner** (o cano do PowerShell
  5.1 trataria os bytes do dump como texto, e o dump decifrado não vai para o disco do PC); espera a
  linha do fim da inicialização da imagem antes do `pg_isready` (sem rede, o servidor temporário
  responde pelo mesmo socket); restaura com `--exit-on-error --single-transaction`; confere as
  migrations, as contas (pelo menos uma) e os ditados, e mostra só as contagens; e, com o backup velho,
  restaura assim mesmo e falha no fim, dizendo as duas coisas (o `-MaxHoras`, padrão 48, muda o limite).
  Um contêiner do teste que sobrou de uma vez anterior é removido antes (é nosso e descartável). O
  `.gitignore` ganhou também `*.dump.age`, `*.partial`, `rclone.conf`, `**/segredos/` e
  `AGE-SECRET-KEY*`; nenhum arquivo do repositório fica ignorado por elas (`git ls-files -ci`). A soma do
  teste mensal é `concat_ws` (e não `||`, que o teste do PowerShell 5.1 recusa em todo o texto).
  **Mutações: 15 mortas** (o contêiner com rede, a recusa da pasta no repositório e da chave que já
  existe, a leitura da pública com a pasta gravável, a saída do Docker impressa, o `-y` trocado, o
  contêiner mensal fora do `finally`, as 48 h, a senha na linha de comando, uma porta publicada, o
  `--single-transaction`, a idade sem conferir, o arquivo montado gravável, e o `*.age` e o
  `/segredos/` fora do `.gitignore`); duas sobreviveram na primeira passada (o `-y` trocado por `-o` e
  o `rm` do `finally`, que o teste achava no `finally` do `Docker-Silencioso`) e morreram com o teste
  corrigido. Nenhum dos dois scripts rodou.

**A varredura de segredos, e o que substitui o CI (F11h).** O gitleaks do CI nunca roda sem `git push`
(D4). Um teste Go varre `deploy/`, `docs/` e `scripts/` atrás de padrões de segredo: a chave privada do
`age` (`AGE-SECRET-KEY-`), blocos de chave privada PEM, o `GOCSPX-` do Google, chaves de API conhecidas,
URLs de Postgres com senha, e uma atribuição de `SESSION_SECRET`, `DEVICE_TOKEN_PEPPER` ou das chaves do
B2 com um valor longo, com uma lista curta de exceções (os exemplos com valor falso). As amostras do
próprio teste são montadas por concatenação, para o arquivo de teste não parecer segredo. **Até haver
push, o que substitui o CI é:** `go test ./...`, `go vet ./...`, o `pnpm lint`, o `pnpm test` e o
`pnpm build` do `web/`, os testes estáticos do deploy (F11e a F11i), a varredura, e o `docker build` do
`deploy-teste.ps1` (Parte 2), que o Mauro roda. O pré-requisito "CI verde" do `12` ("Pré-requisitos")
**não se cumpre** até o push: é um desvio registrado, e o `12` ganha a ressalva na F11k.
- **Feita (09-out-2026).** `deploy/varredura_test.go`, que roda em todo `go test ./...`: varre `deploy/`,
  `docs/`, `scripts/`, `migrations/`, os `.md` da raiz e o `.env.example` (pula os binários) atrás de
  treze padrões: a chave privada do `age`, o bloco PEM de chave privada, o secret de client do Google
  (`GOCSPX-`), a chave de API do Google, as chaves da Anthropic e da OpenAI, os tokens do GitHub e do
  Slack, a chave da AWS, a application key do B2, a URL de banco com senha (fora os exemplos `<senha>`,
  `$variavel` e os valores de mentira conhecidos), a atribuição de um segredo com valor longo e a `key =`
  de um `rclone.conf`. O erro diz só o arquivo e o nome do padrão, nunca o valor achado. As amostras de
  cada padrão são montadas por partes no próprio teste, e um teste confere que cada padrão pega a sua e
  que os exemplos da documentação (a URL do `postgres-de-teste`, a do `gerar-segredos.sh`, a chave
  pública `age1...`) não são achados; e a varredura inteira roda numa árvore de mentira (acha a amostra
  num `.md` e num arquivo de configuração, pula o binário pela extensão e pelo byte zero e o
  `node_modules`, e não põe o valor no log). Conferida antes contra o repositório: os únicos achados
  eram falsos (a `SESSION_SECRET=` vazia seguida da linha de baixo), e o padrão não atravessa mais a
  quebra de linha. **Mutações: 6 mortas** (a lista de exemplos, o byte zero, o `node_modules`, o `=`
  que atravessa a linha, um padrão a menos e uma pasta a menos). O "o que substitui o CI" está escrito
  também em `deploy/README.md`.

**O dossiê de LGPD e a política (F11b).** Dois documentos em `docs/`, em português simples:
`docs/lgpd-dossie.md` (duas páginas, para o Mauro ler, mais um anexo de conferência) e
`docs/politica-de-privacidade.md` (o rascunho). Os caminhos são os do pedido da F11b de 09-out-2026; a F11.0
(esta seção, na primeira escrita) dizia `docs/lgpd/dossie-nuvem-f11.md` e
`docs/lgpd/politica-de-privacidade.md`.
- **O dossiê diz:** o que sai do PC, onde fica e quem consegue ler (quem administra a VPS lê o texto);
  os prazos (6 meses no ativo, a lixeira de 30 dias, o piso de 24 h da lixeira que chega pelo push, o
  aprendido desfeito enquanto a conta existir, o backup de 30 dias no B2 cifrado, as cópias do banco no
  PC que só saem pelo botão, o banco antigo de uma restauração por 7 dias); os operadores e os países
  (a VPS, **a preencher**; o Google no login, que recebe o e-mail e o identificador; o Backblaze, que
  recebe só o arquivo cifrado que não consegue ler, com a região do bucket); os direitos do titular e
  como exercê-los (apagar meus dados da nuvem, excluir definitivamente, revogar um aparelho ou uma
  sessão, o acesso pelo próprio app); a ressurreição aceita na restauração; o texto de terceiros que a
  pessoa dite (de clientes, por exemplo); e **o alcance de uma invasão numa VPS compartilhada**.
- **Uma lista responde item a item** a pré-condição do `09` ("LGPD", `09-security-privacy.md:574-586`),
  cada resposta com `arquivo:linha` do código ou da ADR, sem nada inventado.
- **A política** é o rascunho do texto que a pessoa lê, com os três pontos em aberto da D3 descritos
  como são e o país e a região em branco marcado. O texto novo da seção Privacidade do app
  (`web/src/nuvem/ConfiguracoesNuvem.tsx:448-455`) vem junto, mas **só é aplicado depois do OK**.
- **O OK do Mauro, no chat, fica registrado nesta ADR.** Sem ele, nada de dado real sobe (`09`).
  **OK: pendente** (os dois documentos estão escritos; falta ele ler e responder).
- **Feita (09-out-2026), no desenho acima, menos o texto do app (que espera o OK).** O dossiê tem o
  corpo (as seções 1 a 7: o que sai, onde fica e quem lê, os prazos, os operadores e os países, os
  direitos, a restauração e a invasão, e as decisões) e um anexo que confere cada número no código
  (`arquivo:linha`) e responde a pré-condição do `09` item a item. A política tem 13 seções e, no fim, o
  texto proposto para o app. Conferido no código, e não só nas ADRs: os prazos (6 meses e 30 dias em
  `internal/retention/policy.go`; o piso de 24 h, a lápide, os 90 e 190 dias em
  `internal/server/cloudstore/retencao.go`; a sessão e o token em `nuvem.go` e `pareamento.go`), o que o
  "apagar meus dados" apaga e o que deixa (`pgstore/contas.go`), o que o login guarda (`0001`, `idtoken.go`)
  e o que o PC manda (`payload.go`). **O que a conferência achou e os documentos não diziam**, e que
  vai ao Mauro na seção 7 do dossiê: (1) o **nome** da conta Google é guardado (`display_name`); (2) o
  "apagar meus dados" **não apaga o cadastro** (e-mail, `google_sub` e nome): nenhum código apaga a linha
  de `users`, e não existe "excluir a conta"; (3) a nuvem **não tem exportação** (a cópia que o art. 19,
  §3º da Lei 13.709/2018 prevê hoje só existe no PC); (4) o "excluir definitivamente" só vale para um
  ditado que já está na lixeira; (5) o nome do computador vai à nuvem e a linha do aparelho fica no
  banco depois de revogado. O cadastro (2) e a exportação (3) podem pedir código ou banco; se o Mauro
  pedir uma rotina ou um botão, o que mexer no banco entra **antes** do primeiro `migrate` em produção
  (D3). Em branco, marcados
  `[A PREENCHER]`: o país e a região da VPS, a região do bucket do B2, o contato de privacidade e, depois
  da D1, o que o servidor de entrada registra. A base legal proposta é o consentimento (art. 7º, I), que
  o PC prova só do lado dele (o clique do envio fica no `historico.db`, não na nuvem). Nenhum código nem
  teste mudou; o `09` aponta para os dois documentos e ganhou o nome da conta no quadro de dados.

**A revisão da Parte 1 (09-out-2026).** A revisão dos commits da F11c à F11h e da F11b achou onze
problemas (um alto, dois médios, oito baixos), todos conferidos e todos reais; cada um tem o seu commit
`fix(...)`, com teste. O estado:
1. **(alto) A chave sem `deleteFiles` não segurava os backups.** A `writeFiles` esconde e sobrescreve,
   e a regra de ciclo de vida apaga o escondido: corrigido na D2 (acima) com o **Object Lock** do
   bucket e a chave só com `listBuckets`, `listFiles` e `writeFiles`, no `deploy/README.md` ("O bucket e
   a chave do B2"), no `backup.sh`, no item 5 do "O que é do Mauro" e no dossiê (seção 6).
2. **(médio) O `gerar-segredos.sh` mostrava e deixava sobrar o colado:** a keyID, a application key e a
   chave pública agora sem eco (a pública aparece depois de conferida), todo `FALHOU` descarta a
   entrada pendente (o `.key` inteiro colado não chega ao bash da VPS), e a keyID de 25 hexadecimais e
   a application key com `K` e três dígitos pegam uma trocada pela outra. **Desvio:** a keyID de 12
   hexadecimais, que a revisão aceitava, é recusada: é a da master key, que alcança a conta inteira.
3. **(baixo) O `aurora` era a restauração antes do pós:** o `migrate` e o `pos-restauracao` rodam no
   banco novo (por uma cópia da `database-url` com o nome dele, na pasta em memória), e a troca de nomes
   é o último passo; qualquer parada antes dela deixa o `aurora` como estava e imprime o `DROP` do novo.
   O `aplicar` recusa também qualquer conexão de cliente ao `aurora` (`pg_stat_activity`).
4. **(baixo) O dump decifrado no `/dev/shm`, sem prazo:** agora na pasta em memória **só do usuário**
   (`/run/user/<uid>`, conferida: tmpfs, dele, 0700), que some quando a última sessão fecha; o
   `aplicar` recusa e apaga um dump com mais de 2 h; o `receber` avisa do swap. Um `mount -t tmpfs`
   próprio, a outra saída da revisão, pede `sudo`. **Consequência para o roteiro (Parte 2):** uma
   sessão `ssh` interativa aberta antes do `receber`, e o `aplicar` nela.
5. **(baixo) O backup entrava com o dono do banco:** um papel só de leitura, `aurora_backup`
   (`pg_read_all_data`, `CONNECT` no `aurora`, senha própria, `postgres-backup-senha`), criado pelo
   init, conferido pelo `conferir-postgres.sh` e de novo com `CONNECT` no banco novo de uma restauração;
   a senha vai só ao `pg_dump`. **Para o compose (Parte 2):** o Postgres monta a `postgres-backup-senha`
   (`AURORA_BACKUP_SENHA_FILE`), e o backup a monta no lugar da `postgres-aurora-senha`. As imagens de
   base por digest, a outra metade do achado, ficam com a do `distroless` para o primeiro build do
   Mauro.
6. **(baixo) A senha do papel no log do Postgres** se o `CREATE ROLE` falhasse: a sessão do init
   desliga o log de comando (`log_min_error_statement = panic` e mais dois), e o init recusa
   `POSTGRES_USER=aurora` (e `aurora_backup`). O `conferir-postgres.sh` **não** ganhou um sexto ajuste
   para o `log_min_error_statement` do servidor: os cinco são decisão desta ADR, e a API manda os
   valores como parâmetros (o `STATEMENT` leva `$1`, não o texto ditado); fica para o Mauro, se quiser.
7. **(médio) O teste do mês dava 141 com um backup bom** maior que o cano: o `cat` drena depois do
   `pg_restore --list`, e o código que vale é o dele.
8. **(baixo) O grupo do `pg_restore --list` herdava o `set -e`** e culpava o `tee`: corrigido no
   `backup.sh`. No `restaurar.sh` o defeito não existia (a função é chamada com `||`, e o bash ignora o
   `set -e` ali), mas o grupo ficou igual.
9. **(baixo) A API em laço de reinício passava** pelo filtro `status=running`: a API e o backup são
   recusados em qualquer estado vivo, e o filtro de nome é `^/?nome$` (o Docker guarda a barra).
10. **(baixo) O `receber` só lia o índice:** agora lê o dump inteiro (`pg_restore -f /dev/null`) antes
    do OK.
11. **(baixo) O conjunto de capacidades da chave não estava escrito:** está no `deploy/README.md`, com o
    comando da CLI do B2, e o `deploy/scripts/conferir-b2.sh` (novo) confere na VPS, antes do primeiro
    backup, que a chave sobe, lista e **não** apaga de vez.

**Como foi provado:** os testes de texto e o bash com programas de mentira (`deploy/`), e, no Postgres
18 de teste, o SQL do próprio init (`deploy/postgres_pg_test.go`): o `aurora_backup` lê e trava cada
tabela e sequência das migrations, é recusado em toda escrita, e sem o `CONNECT` não entra; as consultas
do `conferir-postgres.sh` dão o esperado. **Mutações: 22 mortas e 1 equivalente** (o grupo do
`restaurar.sh`, pelo `||`); uma sobreviveu na primeira passada (o tamanho do arquivo de conferência no
`conferir-b2.sh`, sem conferir) e morreu com um teste novo. **O que não foi provado de verdade:** o `pg_dump` pelo `aurora_backup`, o
`pg_restore -f /dev/null` num dump cortado, o `rclone` com a chave restrita e o Object Lock, o
`docker ps` com um contêiner reiniciando e a pasta `/run/user` sumindo com a sessão; a primeira
execução do Mauro é a prova. **A confirmar pelo Mauro:** o modo do Object Lock (governance, proposto;
compliance não deixa nem ele apagar antes do prazo), o prazo de 2 h do dump decifrado e a recusa da
keyID da master key.

**O que fica para depois da D1 (Parte 2).** Nada disso é escrito antes de ele responder. **[Feito em
09-out-2026, depois da D1: ver "A Parte 2 da F11", abaixo. A variante A1; a A2 e a B não foram
escritas.]**
- **O compose** (ou o stack, na A2) da API, do Postgres e do sidecar do backup. Os nomes dos serviços e
  dos apelidos de DNS levam o prefixo `aurora-whisper-*`: o nome do serviço vira apelido na rede em que
  ele entra, e um `api` ou `db` nosso numa rede compartilhada entraria no rodízio de DNS de quem já usa
  o nome (e um Postgres numa rede compartilhada ficaria ao alcance dos outros serviços). O Postgres
  vive só numa rede própria com `internal: true`; a API entra na rede própria e na do proxy; o sidecar
  do backup, numa rede de saída à parte. Postgres e API sem `ports`; a API `read_only`; limites de
  memória tirados do diagnóstico; `stop_grace_period` de 20 s ou mais (o desligamento limpo leva até
  15 s: `serve.go:42`); log do Docker com rotação; `restart`; o `healthcheck` da API com
  `-retencao`; os cinco `-c` do Postgres; **nenhum serviço monta `/var/run/docker.sock`**; versões de
  imagem fixas. Teste estático que confere tudo isso.
- **A borda,** conforme a D1: na A1, rótulos do Traefik só no nosso serviço, com o **log de acesso
  desligado só no nosso router** (`traefik.http.routers.<nome>.observability.accesslogs=false`, a
  observabilidade por router do Traefik v3, **a conferir na documentação da versão em uso**; o log do
  Traefik hoje liga para todos os sites e gravaria o `code` e o `state` do callback do Google e o IP de
  cada pedido, e desligá-lo no Traefik recriaria o Traefik e derrubaria os sites), e **sem o middleware
  `sec-headers`**, que traz outra CSP e duplicaria a do servidor. **Se o rótulo não existir na versão
  dele, é ponto de parada:** o Mauro decide entre mexer no Traefik e aceitar o risco 11. Na B, um
  Caddyfile sem log de acesso. Na A2, a variante do Swarm.
- **O `PROXY_CONFIAVEL` da rede do proxy.** Na A1 o Traefik está numa rede que não é nossa, com IP
  dinâmico: a leitura que imprime só a sub-rede dela (F11a) dá o valor, e o WARN único da F11c é o que
  detecta o erro.
- **O agente lê, só para leitura, o compose do Traefik e o de outro serviço que já roda atrás dele**
  (repositórios do Mauro no PC, fora deste), e escreve a variante A1 sem perguntar nada ao Mauro.
- **O roteiro do deploy** (`docs/deploy-f11.md`, um comando por passo, com o porquê), **o guia do client
  Google** (`docs/google-client-f11.md`, tela a tela, escrito depois de saber o endereço) **e o teste
  final** (`docs/teste-final-f11.md`).
- **O que a variante de teste não prova.** A variante `exemplo.test` com um Caddy de teste só em
  `127.0.0.1` não exercita a borda de verdade (os rótulos do Traefik, o log de acesso desligado, o
  `X-Forwarded-For`). Reproduzir o Traefik pede o socket do Docker Desktop num contêiner de teste, e
  reproduzir o Swarm pede um `docker swarm init` no PC, o que muda o sistema e **nunca se faz**. A
  escolha é do Mauro: um Traefik de teste descartável com o socket só leitura, ou aceitar a lacuna e
  conferir no servidor com o `conferir-nuvem.ps1`.
- **O client do Google de produção** (o redirect `https://<subdomínio>/auth/google/callback`).

**As subfatias.** Cada uma com o seu commit, `go test ./...` e `go vet ./...` verdes (e o `pnpm lint`,
`test` e `build` quando mexe no `web/`), sem `git push`. **Nenhum agente roda Docker, `ssh`, `scp`,
`rclone`, `b2`, o `age` contra algo real, o servidor, os scripts `.ps1` ou `.sh` contra o sistema, nem o
Agent; nenhum cria ou vê segredo real nem toca no `bin\` ou no `%LOCALAPPDATA%`.** Os scripts e os
arquivos de deploy são provados por testes estáticos em Go e, quando der, pelo bash com programas de
mentira. Nenhuma dependência de produção nova.

| Subfatia | Parte | O que entrega | O que prova |
|---|---|---|---|
| F11.0 | 1 | esta seção, a linha da F11 em "Fatias" e o "O que o Mauro providencia"; as emendas dos outros documentos entram junto da subfatia que as muda | nada de código; `go test ./...` e `go vet ./...` como estavam |
| F11a | antes de tudo | o diagnóstico ampliado, só leitura: `docker network ls` (nome, driver e escopo), a sub-rede de cada rede (`docker network inspect --format`), o estado do Swarm (`docker info`: `LocalNodeState`, nós e `ControlAvailable`) e, havendo Swarm, `docker service ls` (nome e portas); o NTP (`timedatectl`, `NTPSynchronized`); e um resumo no fim do que a saída decide (proxy, folga, arquitetura). O plugin do compose e o `uname` já são lidos (`diagnostico-servidor.sh:73-74, 160-161`). Sem `sudo`, sem rede, sem `inspect` de contêiner, sem variável de ambiente | `bash -n`; o bash com `docker` e `timedatectl` de mentira num `PATH` só deles, para Swarm ativo e inativo e NTP sim, não e ausente; só ASCII e LF. Se o Mauro rodar o diagnóstico de hoje antes dela, a Parte 2 pede um segundo comando de leitura |
| F11b | 1 | o dossiê de LGPD (`docs/lgpd-dossie.md`) e o rascunho da política (`docs/politica-de-privacidade.md`) | cada item de `09:574-586` respondido com `arquivo:linha`; o país e a região em branco marcado; o OK do Mauro registrado aqui; o texto do app só depois do OK |
| F11c | 1 | `*_FILE` dos três segredos; o subcomando `healthcheck` e o `-retencao`, com a rota de saúde do job só em loopback; a primeira rodada de Arrumar e Reter antes do `net.Listen`; o WARN único do `X-Forwarded-For`; o booleano do proxy na linha `servindo`; `10` e `.env.example` | config: arquivo ausente, vazio, com `\n` e com `\r\n` no fim, grande demais, variável e `_FILE` juntas (erro), nunca o valor nem o caminho na mensagem; `healthcheck` contra `httptest` (0 com `200`, 1 com `503`, com prazo estourado e com redirecionamento); `-retencao` (1 com mais de 2 h e com falha `banco` ou `outro`, 0 com `contexto`), e a rota que dá `404` fora do loopback; **a primeira rodada: enquanto o teste segura a rodada, a porta não aceita conexão, e a linha `retenção` sai antes da `servindo`**; o WARN uma vez só e sem IP; mutações rodadas: a rodada de volta depois do `Listen` e o WARN a cada pedido |
| F11d | 1 | o subcomando `pos-restauracao -confirmo` (epoch novo por conta, sessões e aparelhos revogados, só contagens) na memória e no Postgres; o AC-061 e o `12` corrigidos | o ponta a ponta em memória do backup mais velho que o PC (o que tem de valer e o que foi aceito); o mesmo no Postgres de teste, se o arquivo existir; sem `-confirmo`, nada muda; o log só com contagens; **se aparecer outro defeito, para e vai ao Mauro** |
| F11e | 1 | `deploy/Dockerfile`, `.dockerignore`, `deploy/postgres/` (o init do papel e o `conferir-postgres.sh`) e `deploy/deploy_test.go` | os testes estáticos acima; o init e o `conferir-postgres.sh` com `psql` e `docker` de mentira (valor errado, papel superusuário, banco de outro dono); o build de verdade é o do Mauro |
| F11f | 1 | `gerar-segredos.sh`, `backup.sh` e `restaurar.sh` em `deploy/scripts/`, e a imagem do backup | `bash -n`, os testes estáticos e o bash com programas de mentira (acima) |
| F11g | 1 | `scripts\backup-chave.ps1`, `scripts\teste-restauracao-mensal.ps1` e o `.gitignore` | o teste estático e o parser do PowerShell (acima); nenhum executado |
| F11h | 1 | o teste Go de padrões de segredo sobre `deploy/`, `docs/` e `scripts/` | passa no repositório; falha nas amostras montadas no próprio teste; o que substitui o CI, escrito |
| F11i | 2 | o compose (ou o stack) e a borda que a D1 mandar, a variante de teste e o teste estático | os testes de "O que fica para depois da D1"; `exemplo.test` só em `127.0.0.1` |
| F11j | 2 | `subir.sh` (a ordem: o NTP, o Postgres, o `migrate -status` com a pausa em "digite SIM", o `migrate`, a API, o `SHOW`, a borda, o health por https), `deploy-teste.ps1`, `enviar-para-servidor.ps1` (`docker save` e `docker load` por `ssh`, mais a pasta `deploy/` e o JSON do Google), `montar-bin.ps1` e `conferir-nuvem.ps1` | estático e com programas de mentira, como acima; o `deploy-teste.ps1` do Mauro sobe, faz backup e restaura com epoch novo num contêiner descartável, exige a linha `retenção` antes da porta, e termina com `down -v` |
| F11k | 2 | os roteiros e os documentos em dia (`04`, `09`, `10`, `11`, `12`, `13`, README, `deploy/README.md`, que ainda promete o "Sprint 6+") | `go test ./...` e `go vet ./...`; as referências conferidas; nenhum domínio, e-mail ou segredo real no repositório |

**O que é do Mauro, na ordem (o mínimo).** O agente entrega cada passo pronto; os que mexem na máquina
ou em segredo real são dele (`CLAUDE.md`). **[09-out-2026: os itens 1, 2 e 4 estão feitos (a D1
respondida, o Google no PC); a ordem do que falta é a de "A Parte 2 da F11", abaixo, e do
`docs/deploy-f11.md`.]**
1. **Rodar o diagnóstico** na VPS em que a nuvem pode morar e colar a saída, dizendo também o país e a
   região do datacenter (5 minutos). O comando, no PowerShell (o `<` não existe nele, então vai pelo
   `cmd`), com o apelido do `ssh` dele no lugar de `<apelido>`; se mais de um servidor é candidato,
   em cada um: `cmd /c "ssh <apelido> bash -s < C:\Desenvolvimento\aurora-whisper\scripts\diagnostico-servidor.sh"`.
   O ideal é rodar já a versão ampliada da F11a, para ser um comando só.
2. **Responder a D1**, com a saída na mão, e confirmar as escolhas menores, numa mensagem só.
3. **LGPD:** ler o dossiê (duas páginas) e o rascunho da política, e aprovar no chat ou pedir mudança
   (30 a 60 minutos). Tem de estar feito antes do primeiro `migrate` em produção.
4. **O Google no PC, antes do deploy** (uns 25 minutos). No Google Cloud, o client Web leva o redirect
   `http://localhost:8080/auth/google/callback` (o de produção entra depois, no mesmo client ou em
   outro). Com o Postgres de teste ligado, rodar `scripts\nuvem-local.ps1 -Email <o e-mail dele>` e
   entrar (uns 10 minutos). Pôr uma **segunda conta dele** em "Usuários de teste" do Google, fora do
   `CONTAS_PERMITIDAS`, para ver a recusa da Aurora. Isso tira do dia do deploy o PKCE, o aviso de "app
   não verificado" e a espera até o client valer, e cumpre o que a F3 deixou para a F11.
5. **O Backblaze B2** (uns 20 minutos): a conta, um bucket privado **com Object Lock e retenção padrão
   de 30 dias** (modo governance, a confirmar), a regra de ciclo de vida de 30 dias e a application key
   do bucket **só com `listBuckets`, `listFiles` e `writeFiles`** (sem `deleteFiles`, `readFiles`,
   `bypassGovernance`, `writeFileRetentions` nem `writeBucketRetentions`; pela CLI ou pela API do B2,
   porque o painel, pelo que se sabe, só oferece conjuntos prontos). O passo a passo, com o comando,
   está em `deploy/README.md` ("O bucket e a chave do B2"). A chave vai direto ao servidor, pelo
   roteiro, nunca ao chat; lá, o `deploy/scripts/conferir-b2.sh` a confere antes do primeiro backup.
6. **A chave do backup:** `scripts\backup-chave.ps1`, com o Docker Desktop aberto. Guardar a privada no
   gerenciador de senhas e numa segunda cópia fora do PC. Se ela se perder, todos os backups se perdem.
7. **Depois da D1 (Parte 2):** o subdomínio e 1 registro DNS (se o DNS estiver na Cloudflare, "somente
   DNS": ela leria o texto); o redirect de produção no client do Google; o `deploy-teste.ps1` no PC;
   mandar a imagem e a pasta `deploy/` ao servidor; no servidor, `gerar-segredos.sh` e `subir.sh`; o JSON
   do Google no lugar que o roteiro indica; **uma cópia do `DEVICE_TOKEN_PEPPER` no gerenciador de
   senhas** (perdê-lo desconecta todos os PCs).
8. **O teste final, em duas partes.** Parte 1, sem dado: o Google no PC e no Android, instalar o app,
   as sessões, o modo avião. Depois, o primeiro backup e a restauração dele no PC (1 comando cada), com
   a conta que o login criou. Parte 2, com dado: `scripts\montar-bin.ps1`, conectar o PC, o Enviar o
   histórico, editar no celular e ver no PC, aprender e desfazer pelo celular, revogar, tirar o PC da
   internet e ver a volta. Isso **absorve os testes à mão da F8 e da F10** que ele adiou. Mais duas
   conferências: uma palavra-sentinela ditada na Parte 2 e contada nos logs da API, do Postgres e da
   borda (o esperado é zero), e um `docker stats --no-stream` depois do Enviar o histórico, para ver o
   efeito nos outros serviços.
9. **Uma vez por mês:** `scripts\teste-restauracao-mensal.ps1`.

**Escolhas menores, a confirmar pelo Mauro na entrega** (como nas F4 a F10: a implementação usa a
proposta até ele dizer o contrário):
1. **A cota de escrita por conta não entra na F11** (a D3 da F9 a mandou para cá). Sem número e sem uso
   real, um teto chutado pode travar o envio inicial; decide-se com dado, na "Revisão". O risco da F9
   fica (um token de PC com defeito enche `applied_mutations` a 5.000 mutações por minuto).
2. **Sem `advisory lock` na retenção.** A premissa é "o deploy para a instância velha antes de subir a
   nova"; no Swarm o `start-first` a derruba, então ela só vale com o compose comum ou com `stop-first`,
   que o teste estático da Parte 2 confere.
3. **Restaurar revoga todas as sessões e todos os aparelhos.** Todos os PCs reconectam à mão e o celular
   entra de novo, o que é um "revoke massivo" (`09`, "Operações destrutivas"): o roteiro de restauração
   diz isso antes de ele aprovar.
4. **Se a primeira rodada falha com `banco` ou `outro`, o `serve` sai com 1**, e não abre assim mesmo. É
   a escolha que não deixa texto vencido à vista, como já se recusa a subir com migration pendente; o
   `restart` tenta de novo. A outra, abrir com o WARN e o `healthcheck -retencao` falhando, mantém a
   nuvem no ar com o texto vencido à mostra.
5. **O cifrado fica 3 dias no volume do backup da VPS**, para reenviar se o B2 falhar; ninguém na VPS o
   lê, e o B2 guarda os 30 dias.
6. **O Google em modo de teste**, com o e-mail dele (e a segunda conta, só no Google) em "Usuários de
   teste".
7. **O aviso do `unhealthy`:** sem canal novo na v1. O estado aparece no `docker ps` (ou num painel do
   Docker), e o teste mensal é a rede de segurança; um aviso por mensagem (por uma automação ou um
   monitor) só se ele pedir.
8. **O agendador do backup é um contêiner do próprio compose** (Parte 2); nada de `cron` no host.
9. **O teto do login e as rotas sem limite ficam como estão** (uma conta só). O `PROXY_CONFIAVEL` é
   provado por um teste com um `X-Forwarded-For` forjado e pelo WARN único.
10. **O banco antigo de uma restauração fica 7 dias**, com o `DROP DATABASE` pronto no roteiro.
11. **O teste mensal usa o arquivo baixado à mão do B2**: nenhuma credencial do B2 no PC.
12. **Técnicas, sem confirmação:** os nomes `pos-restauracao`, `-confirmo` e `healthcheck -retencao`; as
    pastas `deploy/scripts/`, `deploy/postgres/` e `deploy/backup/`; e a `const version`
    como está.

**Riscos aceitos.**
- **Quem administra a VPS lê o texto, e uma VPS compartilhada tem um alcance de invasão maior.** Nas
  A1 e A2, quem tomar um serviço vizinho chega à raiz do Docker e ao volume do Postgres. O backup
  cifrado e o Postgres sem porta não impedem isso. Está no dossiê, com todas as letras, e pesa a favor
  da B.
- **A ressurreição na restauração é aceita** (acima). O texto que o `Reter` já venceu não volta.
- **Nada do que o agente escrever será rodado por ele de verdade.** O Dockerfile, o compose e os
  scripts são provados por teste estático e por programas de mentira; o bash ainda não rodou num Linux
  de verdade (nem o diagnóstico). O primeiro build é o `deploy-teste.ps1` do Mauro, e o primeiro uso
  real dos scripts é dele. O `pg_restore --list` sobre o fluxo não será provado com o `pg_restore` de
  verdade; a primeira restauração do Mauro é a prova, e o recurso, se ele não ler do fluxo, é um
  arquivo em memória.
- **Perder a chave privada do backup perde todos os backups; perder o `DEVICE_TOKEN_PEPPER` desconecta
  todos os PCs.** Os dois pedem cópia fora do PC e do servidor.
- **O backup guarda texto apagado até o giro**: 30 dias, mais o tempo que o B2 leva para apagar o que a
  regra escondeu (a política diz o número que ele vir no painel). O arquivo cifrado fica 3 dias na VPS.
- **Quem invadir a VPS ainda pode esconder os backups e parar o backup** (revisão da Parte 1): a chave
  que sobe precisa da `writeFiles`, que esconde. O Object Lock segura cada cópia até o fim da retenção
  dela, e o teste do mês é o que percebe (o escondido não aparece no painel sem "mostrar versões");
  um alarme fica para quando ele pedir (escolha menor 7).
- **O dump decifrado de uma restauração passa pela memória da VPS** (a pasta só do usuário, no
  máximo 2 h): se a VPS tem swap, essa memória pode ir para o disco do swap. O `receber` avisa; desligar
  o swap pede `sudo`, que os scripts não usam.
- **O Postgres fala sem TLS (`sslmode=disable`) numa rede `internal: true`** onde só estão a API e o
  backup.
- **Os segredos em `0444` numa pasta `0700`** podem ser lidos por qualquer processo do usuário do host
  (e pelo root, que de todo jeito lê o volume).
- **A primeira rodada antes da porta atrasa a abertura** pelo tempo dela (a conta é pequena, uma pessoa
  só); o `start_period` cobre. Um defeito dela, com a escolha 4, derruba o `serve` em vez de abri-lo.
- **O Google de verdade nunca foi exercitado** até o passo 4 do Mauro. A volta do https para o
  `127.0.0.1` no pareamento (Local Network Access e HTTPS-Upgrades, Riscos 3) só existe com a nuvem em
  https; se barrar, o "código colado" perde a resistência a phishing e volta para ele decidir.
- **A D1 pendente trava só a Parte 2.** A Parte 1 não depende dela; se a resposta demorar, nada do que
  se faz agora é jogado fora.
- **O código só existe no PC** (a `main` está 663 commits à frente de `origin/main`). Perder o PC é
  perder o código. Fica fora da F11, mas é real, e o push espera a decisão à parte da D4.

**O que a F11.0 não fez:** nenhum código, nenhum script, nenhuma migration, nenhum teste novo, e
nenhuma mudança nos outros documentos; nesta ADR, além desta seção, só a linha da F11 em "Fatias", o "O
que o Mauro providencia" e duas notas entre colchetes em "Hospedagem e backup". O AC-061 (`03`) e as linhas 72-73 do `12` continuam com o texto
velho até a F11d, que os corrige junto do teste que prova o que vale; o `04`, o `09`, o `10`, o `11`,
o `13`, o README e o `deploy/README.md` esperam a subfatia que os muda (a F11c, a F11d, a F11h, a F11k).
Ninguém rodou Docker, `ssh`, o servidor, o Agent nem script nenhum, e ninguém criou ou viu segredo
real.

### A Parte 2 da F11 (09-out-2026)

Escrita no mesmo dia da D1 (acima), com a autorização do Mauro ("segue o baile; vamos terminar o quanto
antes"): a variante **A1**, na VPS escolhida, atrás do Traefik que já roda. O agente leu, **só para
leitura**, o molde dos rótulos num repositório do Mauro no PC (o compose de outro serviço que já roda
atrás do Traefik, e o compose e as notas do próprio Traefik): os entrypoints `web` (o 80, que manda
para o `websecure`) e `websecure` (o 443), o resolvedor `letsencrypt` (desafio HTTP),
`exposedbydefault=false`, a rede padrão `web`, o `--accesslog=true` para todos, e o provider docker,
cujo "limite honesto" é que o Traefik lê o `Config.Env` de todo contêiner. Nada fora deste
repositório foi editado. **Ninguém rodou Docker, `ssh`, `scp`, `rclone`, o servidor,
o Agent nem script nenhum contra o sistema; ninguém criou ou viu segredo real.**

**O rótulo do log de acesso, conferido.** A documentação oficial do Traefik v3 (a referência do
provider docker e a página de observabilidade dos routers) traz
`traefik.http.routers.<router>.observability.accesslogs`, com padrão `true` e os rótulos sem diferença
de maiúsculas; um router com a própria observabilidade deixa de herdar a do entrypoint. Logo, **não
houve o ponto de parada** previsto ("se o rótulo não existir na versão dele"). A prova com um Traefik
de verdade (a versão fixada no `compose.teste.yml`) é do `deploy-teste.ps1` (abaixo), e a da VPS, o
passo 8 do `docs/deploy-f11.md` (o nome do router contado no log de acesso: zero).

**O que foi feito, por subfatia** (cada uma com o seu commit, e `go test ./...` e `go vet ./...` verdes):
- **F11i, o compose (`deploy/compose.yml`).** O `aurora-whisper-postgres` (`postgres:18.6-trixie`, só na
  rede `aurora-whisper-interna`, `internal: true`; o init dos papéis montado só leitura; os cinco `-c`
  de log; a saúde pelo `pg_isready` **no TCP**, porque o servidor temporário do init só escuta no
  socket), a `aurora-whisper-api` (a imagem do PC com `pull_policy: never`; o `serve`; a interna e a
  `web`; `read_only`, sem capacidades; o `healthcheck -retencao` com `start_period` de 2 min **[a
  revisão da Parte 2 o trocou pelo de vida, abaixo]**; o
  `stop_grace_period` de 30 s; 256 MB, o `GOMEMLIMIT` de 200 MiB e 1 CPU) e o `aurora-whisper-backup`
  (o agendador; a interna e a `aurora-whisper-saida`; 128 MB e meia CPU). Os segredos por arquivo
  (`secrets:` do compose, em `/run/secrets`, e as variáveis `*_FILE`), **nenhum por variável de
  ambiente**; o que não é segredo vem do `config.env` por `${VAR:?}` (o compose recusa sem ele).
  Nenhuma porta publicada, nenhum socket do Docker, `no-new-privileges` em todos, o log do Docker com
  rotação. Os rótulos do Traefik só na API: `traefik.enable`, `traefik.docker.network`, a regra
  `Host` com o `DOMINIO` do `config.env`, o `websecure`, o `letsencrypt`, o serviço na 8080 e o
  `observability.accesslogs=false`; **sem um middleware de cabeçalhos** dos outros sites (a CSP e o
  HSTS são os do servidor).
  O nome fixo dos contêineres dá o stop-first (a escolha menor 2). O **agendador**
  (`deploy/scripts/agendar-backup.sh`, dentro da imagem do backup): o `backup.sh` todo dia às 06 UTC
  (03:00 em São Paulo), sem `date -d` (o busybox da alpine), e o `saude` do healthcheck (o último que
  subiu ao B2 tem no máximo 26 h; nenhum ainda é `unhealthy`). A imagem do backup sobe para o
  `postgres:18.6-alpine`, a mesma versão do servidor.
- **F11j, os scripts.** O `gerar-segredos.sh` grava também o `DOMINIO` no `config.env` e confere o JSON
  do Google que o PC mandou para `~/aurora-whisper/recebido/` (o client Web, sem o `installed`, com o
  redirect `https://<domínio>/auth/google/callback`, até 64 KiB), sem mostrar nada dele, e o **move**
  para `segredos/` (0444) só no fim; sem ele, recusa antes de perguntar. O **`subir.sh`** (na ordem: o
  Docker, o compose e as duas imagens; a pasta `segredos/` 0700 com os nove arquivos; o `config.env`
  conferido sem ser executado; o NTP pelo `timedatectl`; a sub-rede IPv4 da rede `web` por `docker
  network inspect`, gravada como `PROXY_CONFIAVEL`, com um aviso se a rede tiver IPv6; o `compose
  config`; o Postgres saudável e o `conferir-postgres.sh`; o `migrate -status` e, com pendente, "digite
  SIM" e o `migrate` (parando a API antes, numa imagem nova); "digite SIM" antes da API; a API saudável e
  o log na ordem arrumação, retenção, servindo, com `proxy_confiavel` verdadeiro; o backup de pé; e o
  https pelo Traefik com o `curl` do host, que confere o certificado). Os scripts do PC: o
  **`deploy-teste.ps1`** (as duas imagens de produção; o **mesmo** `compose.yml` com o
  `compose.teste.yml` por cima, no projeto `aurora-whisper-teste`, só em `127.0.0.1`; segredos de mentira
  e uma chave de teste numa pasta temporária; os cinco ajustes e os papéis; o `migrate`; a API e a ordem
  do log; o https pelo `curl.exe` com `--connect-to`: o HSTS, **uma** CSP, o Host errado com 404 ou 421,
  o http para o https, a saúde do job fechada por fora, **o log de acesso do Traefik de teste com o
  pedido de controle e sem os do nosso router**, nenhum aviso de `X-Forwarded-For`; uma conta de teste,
  o `backup.sh` do contêiner do backup para um "B2" de mentira, o que subiu igual ao do volume, o
  `saude` antes e depois; a restauração num banco novo por um contêiner descartável, o
  `pos-restauracao` nele e o epoch trocado só nele; tudo derrubado no `finally`, só pelo projeto e pelo
  rótulo do teste; e, se tudo passou, os IDs das imagens anotados), o **`enviar-para-servidor.ps1`**
  (recusa imagens diferentes das aprovadas; o JSON do Google conferido e recusado dentro do
  repositório; a pasta `~/aurora-whisper` 0700 na VPS; o `docker save | ssh -C docker load` pelo `cmd`,
  que passa os bytes; o `scp` da pasta `deploy` e do JSON) e o **`conferir-nuvem.ps1`** (de fora, só
  GET: o DNS e o IP esperado, o certificado pelo `SslStream` do .NET com a cadeia do Windows e os dias
  que faltam, o http para o https, o health com os cabeçalhos, o painel e a saúde do job com 404).
- **F11k, os documentos.** Os roteiros `docs/deploy-f11.md`, `docs/backup-b2-f11.md` e
  `docs/teste-final-f11.md`; o dossiê e a política com a VPS no Brasil, o servidor de
  entrada e o endereço; o `09`, o `10` (o compose, o `config.env` e os `*_FILE` na VPS), o `12` (o
  rollout na ordem do roteiro, a ressalva do "CI verde" e o critério de parar com os outros serviços do
  servidor), o `13` e o `deploy/README.md`.

**Desvios do desenho, a confirmar pelo Mauro:**
1. **Dois "digite SIM" no `subir.sh`:** antes do `migrate` (só com migration pendente; o desenho da F11j)
   e antes de subir a API (o pedido de 09-out-2026). Cada um é um ponto de parada: o primeiro congela as
   migrations, o segundo abre a nuvem para a internet.
2. **O `migrate` e o `pos-restauracao` rodam por `docker run`, e não por `docker compose run`:** o
   `compose run` leva os rótulos do serviço, e o Traefik da VPS poria o contêiner do `migrate` no rodízio
   da API enquanto ele existe. É o mesmo molde do `api_descartavel` do `restaurar.sh`.
3. **A borda do teste do PC é um Traefik com o provider de arquivo**, e não um Caddy nem o provider
   docker: prova o `accessLogs: false` com a mesma versão do servidor sem o socket do Docker, que a ADR
   proibia no teste. O que ela não prova (a leitura dos rótulos pelo provider docker e o Let's Encrypt)
   fica para o passo 8 do roteiro, na VPS, e para o `conferir-nuvem.ps1`. Um teste estático confere que
   o roteador do arquivo e os rótulos dizem o mesmo.
4. **O "B2" do teste do PC** é um remoto `alias` do `rclone` para uma pasta dentro do volume do backup
   (o `backup.sh` só varre o topo do volume e as pastas `.trabalho.*`): exercita o `backup.sh` inteiro
   na imagem de verdade, menos o B2.
5. **A restauração do teste do PC** vai para um banco novo do mesmo Postgres de teste (como o
   `restaurar.sh`), por um contêiner descartável da imagem do backup, e o `pos-restauracao` por um
   descartável da API. O `restaurar.sh` em si (o `/run/user`, o `ssh -t`) não roda no PC.
6. **O JSON do Google vai antes dos segredos:** o `enviar-para-servidor.ps1` o põe em
   `~/aurora-whisper/recebido/`, e o `gerar-segredos.sh` o confere e o move para `segredos/`. O
   `config.env` ganhou o `DOMINIO` (o `Host` da regra do Traefik; o compose não tira o `https://` do
   `PUBLIC_BASE_URL`), e o `subir.sh` o acrescenta a um `config.env` que não o tenha.
7. **As imagens aprovadas:** o `deploy-teste.ps1` anota os IDs em
   `%USERPROFILE%\.aurora-whisper-dev\deploy-teste-aprovado.txt`, e o `enviar-para-servidor.ps1` recusa
   outra. É a D4 ("o que sobe é exatamente a imagem que passou no teste") virando conferência.
8. **O Postgres 18.6, nos dois:** o servidor (`postgres:18.6-trixie`) e a imagem do backup
   (`postgres:18.6-alpine`, que era 18.0). As tags foram lidas no Docker Hub (só leitura) e ainda não
   passaram por um build; a fixação por digest (a da `distroless` e estas) fica para depois do primeiro
   build do Mauro, com o digest que ele mandar.
9. **O domínio aparece nos documentos** (os roteiros, a política, o dossiê, o `12` e esta ADR), porque o
   roteiro o traz pronto para colar e ele é público pelo DNS. O `compose.yml` e os scripts não o têm (o
   `config.env` e o `-Dominio`). A linha da F11k dizia "nenhum domínio, e-mail ou segredo real no
   repositório": o e-mail e os segredos continuam fora.
10. **O primeiro backup é à mão** (depois do `conferir-b2.sh` e de uma entrada pelo Google, para a
    conta existir): o agendador não roda nada ao subir, e o backup fica `unhealthy` até ele.
11. **O `scripts\montar-bin.ps1` e o `docs/google-client-f11.md` da tabela da F11j não foram escritos:** o
    teste final usa os comandos que os roteiros da F8 e da F10 já usam (o `pnpm build` e o `go build`),
    e o client do Google já existe.
12. **A API tem um `tmpfs` de 16 MB em `/tmp`**, além do `read_only`, e os contêineres têm limite de CPU
    (não só de memória). **[Corrigido na revisão da Parte 2:** o teto sozinho não fazia o que esta
    linha dizia (a API e o Postgres com 1 CPU cada, mais meia do backup, somam 2,5 CPUs, mais do que
    uma VPS pequena tem, com o mesmo peso dos outros serviços do servidor). Quem dá a vez a eles na
    disputa é o **peso**: `cpu_shares: 256` nos três, um quarto do padrão; com a CPU livre, a nuvem usa
    até o teto.**]**

**Riscos, além dos de "Decisões da F11":**
- **Quem divide a rede do proxy** (os outros contêineres na rede `web`) alcança a porta 8080 da API
  direto, sem o Traefik, e o `X-Forwarded-For` que vier dessa rede é aceito, porque a sub-rede inteira é
  o proxy confiável. O login, as sessões e os tokens valem igual; um contêiner comprometido nessa rede
  dribla o limite de tentativas por IP. Fechar isso pede uma rede só nossa com o Traefik e o
  `PROXY_CONFIAVEL` só com o IP dele, que é editar o Traefik (fora da D1). O mesmo vale ao contrário: a
  API não alcança as redes internas dos outros serviços.
- **O Traefik lê o `Config.Env` da API** pelo provider docker: lá estão o `PUBLIC_BASE_URL`, o
  `PROXY_CONFIAVEL` e o **e-mail** do `CONTAS_PERMITIDAS` (dado pessoal, não segredo); nenhum segredo.
- **O que o log de acesso do Traefik ainda pode registrar:** um pedido ao nosso endereço que não casa
  com router nenhum (a API fora do ar), e o redirecionamento do http para o https, se o router interno
  dele entrar no log (o `deploy-teste.ps1` avisa). O callback do Google vem sempre por https.
- **A CPU da VPS já tem a carga dos outros serviços do servidor:** a nuvem pode responder devagar nos
  picos deles, e o envio inicial do histórico pode pesar neles. O teste final mede
  com o `docker stats`; "o servidor impacta serviços que já rodam nele" é critério de parar.
- **Nada disto rodou de verdade.** O compose só foi lido por teste; o bash rodou no Git Bash do Windows
  com programas de mentira; os `.ps1` só passaram pelo parser do PowerShell 5.1. O primeiro `docker
  build`, o primeiro `compose up` e o primeiro Traefik com estes rótulos são os do `deploy-teste.ps1` e
  do roteiro do Mauro. O `start_interval` do healthcheck pede o Docker 25 ou mais novo (a VPS tem um
  mais novo).

**Como foi provado:** os testes de `deploy/` (o leitor de YAML pequeno, sem dependência nova, e os
testes do compose; o agendador, o `subir.sh` e o `gerar-segredos.sh` no bash com o `docker`, o
`timedatectl`, o `curl`, o `stat` e o `sleep` de mentira, os do `subir.sh` em paralelo e com as esperas
curtas pelas variáveis `AW_ESPERA_*`, que só os testes usam; os `.ps1` por texto e pelo parser).
**Mutações: 81, todas mortas** (16 no compose: o `accesslogs=true`, a interna sem o `internal`, o
healthcheck sem o `-retencao`, um ajuste de log trocado, a API sem `read_only`, o backup na `web`, a API
com a senha do Postgres, o `stop_grace_period` curto, o `pg_isready` pelo socket, o `pull_policy`, o
resolvedor e o entrypoint trocados, a hora do backup, o nome do contêiner, o `service_started` e o
volume do backup noutro lugar; 8 no agendador; 20 no `subir.sh`: cada conferência e cada SIM tirados,
a API sem parar antes do migrate, a ordem do log, o `proxy_confiavel`, o https, o `unset`, a pasta
0700, as duas sub-redes, a pasta base, o `migrate` que não aplica, o domínio que não bate, o
`migrate` fora da rede interna, a espera que aceita qualquer estado, o `DOMINIO` derivado errado, o
`compose config` e o backup que não fica de pé; 7 no `gerar-segredos.sh`: cada conferência do JSON, o
recebido que fica, o `DOMINIO` e o JSON que não vai para `segredos/`; e 30 nos scripts do PC e no
compose do teste: o rótulo dos descartáveis, a limpeza por outro rótulo, o projeto da VPS, o compose
do teste esquecido, o Host errado, a CSP, o log de acesso, a ordem do log, a saúde do job, o aviso do
`X-Forwarded-For`, o `--single-transaction`, o JSON no repositório, o cano do PowerShell, a pasta
0755, as imagens não conferidas, o destino do JSON e o redirect, o `--insecure`, o certificado não
conferido, os dias, a porta em `0.0.0.0`, a versão do Traefik, o provider docker, o log de acesso
desligado no teste e o roteador diferente dos rótulos). Uma mutação não aplicou na primeira passada
(o padrão do `sed`) e morreu na segunda.

**A revisão da Parte 2 (09-out-2026).** A revisão dos commits da F11i à F11k achou treze problemas, que
são nove depois de juntar os repetidos (quatro médios, cinco baixos); todos foram conferidos e todos
são reais. Cada um tem o seu commit `fix(...)`, com teste:
1. **(médio) O `healthcheck -retencao` derrubaria a nuvem.** O provider docker do Traefik v3 tira do
   roteamento todo contêiner com healthcheck que não esteja `healthy` (o `keepContainer`, com o
   `allowEmptyServices` desligado no Traefik da VPS, e ligá-lo seria editar o Traefik). Uma rodada da
   retenção com falha de banco tiraria o router por até 1 h, com o log de acesso do Traefik
   registrando os pedidos sem router (um callback do Google com o `code` e o `state`). Corrigido para o
   que as escolhas menores 4 e 7 e o `11` já diziam: o compose usa o healthcheck **de vida** (o mesmo da
   imagem), e o `-retencao` roda por `docker exec`, por fora do roteamento, no `subir.sh` (que para antes
   do backup se ele falhar), no `deploy-teste.ps1` e na nova "Uma vez por mês" do roteiro. A outra
   saída da revisão, aceitar que a falha do job tire a nuvem do ar (fail-closed), seria uma decisão
   nova, com o efeito no log de acesso escrito no dossiê e na política: fica para o Mauro, se a quiser.
2. **(médio) O `subir.sh` não limpava o ambiente que o compose lê antes do `--env-file`.** Um `export
   DOMINIO=...` na sessão da VPS chegaria ao rótulo do Traefik sem conferência (o nosso contêiner
   disputando a rota de outro site), e um `COMPOSE_PROJECT_NAME` exportado faria o `stop` do roteiro não
   parar nada. Agora o `unset` cobre toda variável que o `compose.yml` interpola e as `COMPOSE_*`, o
   `subir.sh` confere na saída do `docker compose config` que a única regra é `Host(<DOMINIO do
   config.env>)`, e todo comando à mão leva o `--project-name aurora-whisper`.
3. **(médio) Não havia o roteiro da restauração na VPS**, com que o `restaurar.sh` e esta ADR contavam
   (os avisos da ressurreição, do selo e da revogação em massa antes do "RESTAURAR"; o `DROP` dos 7
   dias). Novo `docs/restauracao-f11.md`, com o comando inteiro da decifra no PC (num contêiner da
   imagem do backup, sem rede, pelo `cmd`, ligado ao `restaurar.sh receber` pelo `ssh`).
4. **(baixo) A troca do client do Google não trocava** (o compose não recria a API quando só o conteúdo
   de um segredo muda, e a API lê o JSON só na subida), e o comando do passo 5 repetido a cada imagem
   deixava uma cópia do client secret em `recebido/`. Agora o `subir.sh` consome o
   `recebido/google-client.json`: confere como o `gerar-segredos.sh`, apaga o que for igual ao de
   `segredos/` e troca o diferente só depois do SIM, com `--force-recreate`; um recusado para o script
   até alguém tirá-lo. O roteiro separa o comando da imagem nova (sem o JSON) e traz a troca pronta.
   **Desvio:** o `enviar-para-servidor.ps1` não recusa o `-ArquivoGoogle` com `segredos/` na VPS (a
   saída da revisão): como o `subir.sh` consome o que chega, nenhuma cópia fica, e a troca não depende
   de mexer à mão em `segredos/`.
5. **(baixo) O teto de CPU não dava a vez aos outros serviços do servidor** (2,5 CPUs numa VPS pequena,
   com o peso padrão):
   `cpu_shares: 256` nos três serviços (o desvio 12, acima, corrigido).
6. **(baixo) A conferência da ordem do log lia o `docker logs` inteiro:** depois de um `stop`, ela
   valia para a subida antiga; dois meses depois, a rotação já tinha levado a linha `servindo`. Agora
   ela lê só o log da subida de agora (`--since` o `StartedAt`), e não se repete quando o compose não
   recriou nem reiniciou a API.
7. **(baixo) As contagens do log de acesso (passo 8 do roteiro) pegavam o log do próprio Traefik** (o
   `ERR` do ACME traz `routerName=...`). Agora contam só o formato comum do log de acesso, depois de um
   `docker inspect traefik --format '{{json .Args}}'` (sem o ambiente) que confere onde e como ele é
   gravado; o teste roda os dois padrões contra linhas de exemplo.
8. **(baixo) O digest da `distroless` por `docker image inspect`** falharia (a imagem de base de um
   build do BuildKit não entra na lista): agora pelo `docker buildx imagetools inspect`, logo depois do
   teste.
9. **(baixo) O roteiro do B2 apontava o passo 5** do deploy para a chave e para o `conferir-b2.sh`: são o
   6 e o 6.1.

**Como foi provado:** os testes de `deploy/` (o bash com o `docker` de mentira, que agora interpola o
`DOMINIO` como o compose de verdade, com o ambiente antes do `--env-file`, e responde ao `--since`, ao
`StartedAt` e ao `docker exec` da API; e o novo `deploy/roteiros_test.go`, que lê os roteiros: o
`--project-name` em todo comando do compose, os passos citados pelo B2, os padrões do log de acesso
contra linhas de exemplo, o digest, a imagem nova sem o JSON, a troca do client e a restauração montada
dos nomes de verdade). `go test ./...` e `go vet ./...` verdes. **Mutações: 10, todas mortas** (o
`-retencao` de volta no compose, o peso de CPU padrão, o `docker exec` do job tirado, o `unset` das
variáveis do `config.env`, a conferência da regra `Host`, o `--since`, o pulo da API não recriada, o
`--force-recreate`, o redirect do JSON novo e o `cmp`); uma não aplicou na primeira passada (o padrão do
`sed`) e morreu na segunda. **O que não foi provado de verdade:** o filtro do Traefik com um contêiner
`unhealthy` (lido no código do Traefik v3, não rodado), o `docker logs --since` com os nanossegundos do
`StartedAt`, a saída do `docker compose config` (a conferência só procura `Host(...)`, em qualquer
formato), o `mv -f` sobre o 0444 no Linux, o `cpu_shares` no cgroup v2 da VPS, o `docker buildx
imagetools inspect` e a decifra do roteiro da restauração. Como o resto da Parte 2, nada disto rodou: a
primeira execução do Mauro é a prova.

**O estado da Parte 2:** o código, os scripts e os roteiros estão prontos e revisados; nada foi para a
VPS, e nenhum Docker rodou. **A confirmar pelo Mauro, antes do passo 7 abaixo:** os desvios 1 a 12
(acima) e os dois desta revisão (a saúde do job fora do healthcheck, e não o fail-closed; o `subir.sh`
consumindo o `recebido/`, e não a recusa no `enviar-para-servidor.ps1`).

**O que é do Mauro, na ordem** (o roteiro `docs/deploy-f11.md`, com o comando de cada passo):
1. ~~o Google no PC~~ (feito);
2. o B2 (`docs/backup-b2-f11.md`): a conta (e dizer a região), o bucket com Object Lock em governance e
   30 dias, a regra de ciclo de vida e a chave só com `listBuckets`, `listFiles` e `writeFiles`;
3. a chave do backup (`scripts\backup-chave.ps1`), se ainda não fez;
4. o `scripts\deploy-teste.ps1`, com o Docker Desktop aberto, e logo depois mandar o digest da
   `distroless` (o `docker buildx imagetools inspect` do passo 4);
5. o `scripts\enviar-para-servidor.ps1`, com o `-ArquivoGoogle` (só desta vez);
6. na VPS, o `gerar-segredos.sh` (e a cópia do `DEVICE_TOKEN_PEPPER`) e o `conferir-b2.sh`;
7. **a revisão de LGPD aprovada** e as confirmações acima, e então o `subir.sh` (os dois SIM);
8. o `scripts\conferir-nuvem.ps1`; o `docker inspect` dos argumentos do Traefik e as duas contagens do
   log de acesso; entrar uma vez pelo Google e ver a segunda conta recusada;
9. o primeiro backup na VPS e a restauração dele no PC (`scripts\teste-restauracao-mensal.ps1`);
10. o teste final (`docs/teste-final-f11.md`);
11. uma vez por mês, a "Uma vez por mês" do roteiro: na VPS, o `healthcheck -retencao` por `docker exec` e
    o `docker ps`; no PC, o `scripts\teste-restauracao-mensal.ps1`;
12. num desastre, e só então, o `docs/restauracao-f11.md` (ponto de parada), com o `DROP` do banco
    antigo 7 dias depois.

**A revisão de LGPD, aprovada pelo Mauro em 10-out-2026** ("estou lendo o dossiê e concordo"), pelo `docs/lgpd-dossie.md` e o rascunho da política (`docs/politica-de-privacidade.md`), como decidido na D3 da F11: os três pontos em aberto (o aprendido desfeito guardado enquanto a conta existir, as cópias do banco no PC que só saem pelo botão, e o Polish) ficam como estão, descritos na política. Com isso, o PC pode conectar à nuvem de produção (o primeiro dado real), e o Mauro mandou seguir com a conexão, o celular e a publicação do código.

**O deploy, feito pelo Mauro em 10-out-2026.** A nuvem está no ar (a
instância do mantenedor), na VPS, atrás do Traefik (certificado do Let's Encrypt até
jan-2027; HSTS, CSP e `nosniff` conferidos de fora; `http` vai para `https`; a saúde do job fechada por
fora), com a conta dele criada pelo Google e o primeiro backup automático `OK` (cifrado, no B2, 29 s).
Nada do Traefik nem dos outros serviços do servidor foi editado ou reiniciado. A primeira rodada real
achou seis defeitos que os testes com programas de mentira não pegavam, todos corrigidos com teste:
- **PowerShell 5.1:** o `return @(...)` desembrulha a lista de um item (o `.Count` quebrava no
  StrictMode); `$Aprovado` e `$aprovado` são a mesma variável (o caminho das imagens aprovadas era
  sobrescrito); e a saída de um programa dentro de uma função volta junto com o valor (o envio acusava
  falha com as imagens carregadas). Três testes novos recusam esses padrões em todo `scripts/*.ps1`.
- **A CLI do B2:** a imagem abre direto no `b2` (o roteiro passou a usar `--entrypoint sh`), e o
  `b2 account authorize` imprime a applicationKey da mestra (o roteiro passou a mandá-la para
  `/dev/null`, e a chave do backup ganhou o `scripts/b2-chave-do-backup.ps1`, que a grava sem mostrar).
- **O rclone:** o 1.70 autenticava pela API v1, que o B2 recusa para as chaves restritas de hoje (o
  suporte veio no 1.73; a imagem do backup foi para o 1.75.2); e o `--immutable` e o `lsf` de um arquivo
  são um HEAD de download, que pede `readFiles`, e a chave do backup não a tem (401): o envio vai com
  `--no-check-dest` e o tamanho é conferido pela listagem da pasta, no `backup.sh` e no
  `conferir-b2.sh`.
**O que fica, e é do Mauro:** ler o dossiê de LGPD (`docs/lgpd-dossie.md`, duas páginas) antes de
**conectar o PC** (o primeiro dado real); o teste final (`docs/teste-final-f11.md`, que cobre os
adiados da F8 e da F10); e, quando quiser, publicar o código aberto (o `git push` dos commits e a
escolha de público ou privado). Pendências técnicas pequenas: fixar por digest a imagem `distroless` e
a do Postgres (pede um ensaio a mais), e o `conferir-b2.sh` ainda não rodou na VPS (o teste à mão do
mesmo dia provou subir, listar e não apagar).

**O PC na nuvem de produção, em 10-out-2026.** O Mauro instalou o app no Android (Chrome, "Instalar app") e entrou com o Google. No PC: o Agent do `bin\` remontado com tudo até a F11 (o banco migrou para a 0008 sem mudar ditado); a nuvem de teste desconectada e esquecida (o contêiner do Postgres de teste removido com o volume); conectado à instância do mantenedor como a conta dele (o navegador já tinha a sessão, e a aprovação passou sem novo login); o histórico enviado: **56 ditados** dos últimos 6 meses, fora da lixeira, todos com a versão da nuvem, a fila vazia, e a cópia `historico.db.antes-da-nuvem-20261010-185227.db` ao lado do banco (conferido só lendo). Uma edição no celular chegou ao PC na rodada seguinte (18:55:30, `recebidos=1 aplicados_do_pull=1`). **Achados para depois:** a lista do painel do PC não se atualiza sozinha quando chega uma mudança da nuvem (só ao voltar à tela), e a busca de 60 s parece lenta para ele. **Ponto que ficou claro só agora:** o app do celular não dita (decisão 1: ver e editar; "Ditar no celular continua fora", no `02`); o Mauro quer o ditado no celular, como a próxima etapa. **[ADR-021, 10-out-2026: o ditado no celular e as duas queixas (a busca de 60 s e a lista que não se atualiza) estão decididos lá.]**

**A publicação do código, decidida pelo Mauro em 10-out-2026.** Um repositório público novo, com um
commit só e sem o histórico; o de antes, privado, continua guardado com o histórico inteiro. O commit
sai com o e-mail anônimo do GitHub. A árvore foi limpa antes: o servidor descrito de forma genérica
(sem endereço, usuário, nome de máquina, provedor nem os outros serviços dele), o bucket e a chave do
B2 como exemplo, a política de privacidade como modelo para quem sobe a própria nuvem, e o
`SECURITY.md` (relato privado do GitHub) e o `CONTRIBUTING.md` novos. A aprovação da revisão de LGPD
(acima) **vale também para distribuir o Setup** ("vale para o Setup também", confirmada pelo Mauro em
10-out-2026): o release continua parado só pelo portão do workflow e pelo release do modelo.

**O Backblaze B2, feito pelo Mauro em 09-out-2026** (guiado pela tela dele):
- **Conta e região:** uma região dos EUA (o B2 não tem região no Brasil). O backup sai do servidor já
  cifrado com o `age`, para a chave pública; a Backblaze guarda um arquivo que não consegue ler. É
  transferência internacional de dado cifrado, e entra no dossiê de LGPD (o país da VPS é o Brasil).
- **O bucket** (um nome só dele, no molde de `aurora-whisper-backup-exemplo`): privado, com o SSE-B2 e
  com o Object Lock ligado na criação.
  O painel gravou a retenção padrão em **compliance** sem mostrar o modo; o `b2 bucket get` mostrou isso.
  Com o bucket ainda vazio, o Mauro escolheu **governance** ("Só você, com a chave mestra", **confirmada
  pelo Mauro em 09-out-2026**; a escolha menor da Parte 1 sobre o modo fica fechada), aplicado pela CLI
  (`b2 bucket update --default-retention-mode governance --default-retention-period "30 days"`), junto
  com a regra de ciclo de vida, que o painel não tinha salvado: esconder aos 30 dias e apagar 1 dia depois,
  para todo o bucket. A saída do `b2 bucket get` conferiu as duas (revisão 4).
- **A chave do backup** `aurora-whisper-backup`: criada pela CLI do B2 num contêiner descartável no PC
  (`backblazeit/b2:4.8.0`, com `--entrypoint sh`; o roteiro foi corrigido), só no bucket, com
  **exatamente** `listBuckets,listFiles,writeFiles`, conferida pelo `b2 key list --long`. Ela está só no
  gerenciador de senhas do Mauro e vai para a VPS pelo `gerar-segredos.sh`.
- **Incidente:** o `b2 account authorize` do roteiro termina imprimindo a applicationKey da **chave
  mestra** e um token da conta, e o Mauro colou a saída no chat. A mestra foi gerada de novo no painel
  logo depois (a que vazou deixou de valer; a chave do backup não depende dela). O roteiro passou a mandar
  essa saída para `/dev/null` (`fix(docs): o authorize do B2 não imprime mais a chave mestra`), e ficou a
  regra: comando que imprime segredo vem com a saída suprimida, porque o aviso "não cole" não basta (o
  mesmo da senha do Postgres de teste, em 05-out-2026).

### Emenda da N7 (ADR-022, 26-out-2026): notas e marcadores no sync

A ADR-022 trouxe a nota e o marcador de nota para o contrato de sync desta ADR. Nada do que está acima
muda para o ditado e o aprendido; o que entra é aditivo. O desenho completo, com os desvios, está na
ADR-022, "Decisões da N7"; aqui fica o que toca o contrato.

- **Dois tipos novos no fio:** `note` e `label` (`entity_type`), cada um com as operações `upsert` e
  `delete` (apagar de vez, com lápide). A regra de conflito desta ADR vale igual para o **conteúdo** da
  nota (título e corpo juntos: o que chegou primeiro fica, o do PC que perdeu vai para o selo, com a
  escolha "manter" ou "trocar" como no ditado). Cor, fixar, arquivar, marcadores e posição: o último que
  escreve vence, sem selo. O marcador não tem conflito.
- **Tabelas à parte para o log e a lápide: `note_sync_events` (0024) e `note_tombstones` (0023).** Os
  `CHECK` de `entity_type` do `sync_events` (0008) e do `tombstones` (0015) só aceitam os tipos de antes,
  e alargá-los pede `ALTER`, que as migrations depois do deploy não usam (só `CREATE TABLE` e
  `CREATE INDEX`, uma tabela por arquivo). As duas tabelas novas usam o **mesmo contador por usuário**
  (`user_sync_counters`, 0010): um `seq` é de uma mudança só, numa das tabelas, e o pull junta as listas por
  `seq` antes do corte da página. As tabelas das entidades são `labels` (0020), `notes` (0021) e
  `note_labels` (0022); a ligação do ditado com a nota é `transcription_note_links` (0025).
- **A lápide da nota e a do marcador nunca vencem.** A lápide do ditado vence com a janela de 6 meses
  (a decisão da F9); a nota não tem janela, então nada cobriria a recusa depois que a lápide saísse, e um
  PC parado por um ano ressuscitaria a nota apagada. São só ids (`user_id`, tipo, id, `seq`); o job de
  retenção não as toca. Um `upsert` atrasado de uma entidade com lápide recebe `gone` (`apagado`).
- **A capacidade: `entidades` no pull.** A página do pull ganha `entidades`, a lista dos tipos que a nuvem
  sincroniza (`["transcription", "learned", "note", "label"]`), sempre presente; o PC antigo a ignora. O
  PC só manda nota, marcador e o `virou_nota` do ditado depois de ver `note` ali. Por quê: o Agent novo
  instalado antes do servidor novo teria toda nota recusada (`tipo_desconhecido`) e o ditado com
  `virou_nota` recusado (`campo_desconhecido`), e a recusa fica. Na **primeira vez** que o PC vê a
  capacidade, ele **zera o cursor** e lê tudo de novo uma vez (um binário de antes pode ter passado pelas
  notas sem aplicá-las); os ditados voltam como eco, sem mudar nada. Uma nuvem que deixa de dizer `note`
  (volta de versão) faz o PC parar de mandar notas. A recusa de tipo de uma nuvem antiga é `adiado`: a
  nota fica na fila e o resto da rodada segue.
- **`virou_nota` no ditado.** O ditado ganha o campo `virou_nota` (o id da nota, ou `null`): no payload de
  criar e no de mudar, e sempre presente no `itemDTO` do pull e do `GET`. A nuvem guarda a ligação na
  `transcription_note_links` (cascata do ditado: a retenção de 6 meses continua valendo e a ligação sai
  junto) e **esconde o ditado ligado da lista e da busca do histórico do celular** (`GET /transcriptions`).
  Sem conflito: o último que escreve vence. O `nova:<token>` da caixa "Criar uma nota" é só do PC e nunca
  sobe. A nuvem de antes da N7 não manda a chave, e o PC não mexe no `virou_nota` local nesse caso.
- **A ordem do lote do push** é: aprendido, marcadores de nota, notas, ditados. O marcador vai antes da
  nota que o cita, e a nota antes do ditado que virou nota nela. Mesmo assim a nuvem aceita a nota
  que cita um marcador que ainda não existe (`note_labels` não tem chave estrangeira para `labels`); o PC
  que recebe a nota antes do marcador guarda uma linha "que espera" (`sync_state = 'aguardando'`).
- **O envio inicial das notas** não pede clique novo: o "Enviar o histórico" (F7c) já é o aceite da pessoa
  para mandar o que é dela. Com o aceite e a capacidade, toda rodada põe na fila os marcadores vivos e as
  notas fora da lixeira que a nuvem ainda não tem (`Fila.CompletarNotas`), sem cópia nova do banco.
- **Retenção.** A nota e o marcador não têm a retenção de 6 meses. O ditado que virou nota **continua**
  sujeito aos 6 meses na nuvem (o texto vive na nota; a nuvem guarda o mínimo). No PC, a N3 deixou o
  ditado que virou nota fora dos 6 meses, e a N7 não mudou isso (o ponto de privacidade sobre o `raw_text`
  que fica no PC segue aberto, ADR-022, "Riscos").
- **Rotas de leitura do celular:** `GET /notes?vista=ativas|arquivo|lixeira` e `GET /labels`, só com a
  sessão do navegador (o token do PC recebe `403`: o PC lê pelo pull). A tela das notas no celular é a N8.
  Detalhe em `06-api-contract.md`.
- **O "Apagar meus dados"** e a arrumação dos eventos cobrem as seis tabelas novas.
- **Deploy:** ponto de parada, do Mauro. O roteiro é `docs/deploy-notas-n7.md` (as migrations `0020` a
  `0025`, só aditivas, e a API nova), e o teste depois dele é `docs/teste-manual-notas-n7.md`.

## O que o Mauro providencia, e quando

- **Agora (F0):**
  - ~~aprovar esta ADR, e confirmar ou mudar as derivadas D1 a D7~~ (feito em 01-out-2026);
  - ~~ajustar a linha 7 do `CLAUDE.md`~~ (autorizado e feito em 01-out-2026);
  - rodar o diagnóstico na VPS e colar a saída (`scripts/README.md` diz como, sem copiar o arquivo
    para lá);
  - informar o país e a região do datacenter.
- **F1:** abrir o Docker Desktop e rodar o script do contêiner de teste.
- **Antes da F3**, para entrar de verdade no PC: o projeto no Google Cloud, com a tela de
  consentimento e um client Web de desenvolvimento (redirect
  `http://localhost:<porta>/auth/google/callback`). O ID e o secret vão para um arquivo local fora
  do repositório, nunca para o chat. Se o Google ficar para depois, a F3 é provada só pelos testes, e
  o "entra com a conta dele" espera o client existir. **Decidido em 05-out-2026: fica para o deploy
  (F11).** O Mauro preferiu não fazer agora o teste do login com o Google de verdade; a F3 está
  provada pelos testes com o dublê do Google, e o roteiro `docs/teste-manual-nuvem-f3.md` (ou o
  atalho `scripts/nuvem-local.ps1`) roda quando o client existir. O risco aceito é descobrir um
  problema de login (PKCE, redirect, aviso de "app não verificado") só na F11.
- **F3:** rodar `docs/teste-manual-nuvem-f3.md` com o client de desenvolvimento (e uma segunda conta em
  "Usuários de teste", para ver a recusa da Aurora), contar o que viu (se o Google aceitou o PKCE, se o
  aviso de "app não verificado" apareceu) e confirmar ou mudar as três decisões da F3 (o teto de 90
  dias da sessão, os limites de tentativas e o "apagar meus dados" mantendo a sessão atual e apagando
  os aparelhos).
- **F4:** rodar `docs/teste-manual-nuvem-f4.md` (um comando, sem Docker e sem Google; se quiser, também
  no Android pelo cabo) e confirmar ou mudar as três decisões da F4 (escolher a versão do selo já
  agora, o servidor de teste com ditados de exemplo, e a Aurora na tela Entrar); ~~e decidir o
  rascunho que some com o `401` na nuvem~~ (decidido em 05-out-2026, "Guarda até entrar de novo", e
  feito: "Decisões da F4", "O rascunho que fica até entrar de novo"; a seção 6 do roteiro confere).
- **F5:** o celular Android, com o cabo e a depuração USB ligada. ~~Decidir o nome, o ícone e a cor~~
  (decidido em 05-out-2026: "Whisper", o ícone do .exe com o maskable cheio, `#070a09`). Ver a folha
  de prévia dos ícones e rodar `docs/teste-manual-nuvem-f5.md` (no PC e no Android, desinstalando e
  limpando no fim).
- **F6:** uma segunda conta Google, só de teste, na lista permitida (recomendado; com o Google, na
  F11). Rodar `docs/teste-manual-nuvem-f6.md` (a nuvem de teste, sem Google: conectar o `bin\`,
  aprovar, ver "Conectado", revogar e ver "desconectado") e confirmar ou mudar as três decisões da F6
  (o Desconectar que revoga na nuvem, os 180 dias e o revogado que some da lista). Decidir se o
  retorno aceita só as portas 49152 a 65535 (a revisão da F6). Antes do merge, ou no máximo antes da
  F7: abrir o Docker Desktop e rodar `scripts/postgres-de-teste.ps1`, para os testes de Postgres da F6
  rodarem.
- **F7a/b:** ~~confirmar ou mudar as três decisões da F7b (a busca a cada 60 s; **a volta do PC que
  ficou fora continua de onde parou, no mesmo epoch**, refeita na revisão de 06-out-2026, e com ela a
  mudança feita desconectado num ditado que a nuvem já tem entrando na fila; e a linha da nuvem na
  bandeja, agora com "aguardando" e "resposta inesperada")~~ (confirmadas pelo Mauro em
  06-out-2026, as três e a mudança na regra da fila). A derivada da F7a (o recomeço leva a
  lixeira) saiu na revisão. Nada a testar agora: o teste manual vem depois da F7c ("O que o Mauro vai
  testar", em "Decisões da F7a/b"). Os testes de Postgres da F6 continuam pendentes (o Docker Desktop
  fechado). **Antes de rodar o `bin\` com a F7a/b e sem a F7c:** a primeira rodada de um PC conectado
  puxa do cursor 0 **tudo** o que a nuvem de teste tiver (o que veio do celular e dos testes) para o
  `historico.db` desse PC, e todo ditado novo passa a subir na hora; o que é de antes da conexão só
  sobe na F7c. O `bin\` do Mauro foi revogado no fim do teste da F6 ("revoguei", 06-out-2026), então
  está desconectado, e nada disso acontece até ele conectar de novo (e, como nenhum ditado dele tem
  `server_version`, nada entra na fila enquanto isso); se preferir não ter o sync ligado antes da F7c,
  é só não conectar, ou testar depois da F7c.
- **F7c:** ~~rodar `docs/teste-manual-nuvem-f7.md` (a F7 inteira: a nuvem de teste, o `bin\`
  recompilado, conectar, enviar o histórico, ditar, editar nos dois lados, o selo, a lixeira e o
  esquecer)~~ (rodado em 07-out-2026; ver a linha da F7c em "Fatias"); ~~confirmar ou mudar as três decisões da F7c (D1, o histórico com um clique; D2, a conta
  nova recebendo só o que nasceu neste PC; D3, o ditado que mudou aqui sem confirmação não é
  sobrescrito)~~ (confirmadas pelo Mauro em 06-out-2026, a D3 com as correções da reconferência); e
  dizer se o ditado recusado pela nuvem, editado aqui, deve ganhar uma nova chance
  (a revisão da F7c o deixou de fora; ver "A revisão da F7c"). A cópia
  `bin\historico.db.antes-da-nuvem-…db` fica até a F10, ou pode ser apagada depois de conferir o app.
  **[F10e: ela sai pelo botão Apagar as cópias, no bloco Histórico das Configurações; o Agent nunca a
  apaga sozinho.]**
- **F8:** ~~responder as quatro decisões da F8~~ (respondidas em 07-out-2026, todas com a
  recomendação: "Decisões da F8"); ~~para a F8c, abrir o Docker Desktop e rodar
  `scripts/postgres-de-teste.ps1`~~ (feito em 07-out-2026: os testes de Postgres da F8a a F8d
  rodaram verdes). **Agora:** rodar `docs/teste-manual-nuvem-f8.md` (F8i, pronto em 08-out-2026), com
  a nuvem de teste vazia e o `bin\` recompilado: aprende no PC, envia o histórico (que ainda não
  subiu), vê no app, desfaz dos dois lados. E confirmar ou mudar as escolhas menores (a remoção à mão
  que propaga como desfazer, com o freio; os 30 dias da etiqueta; o teto da lista do celular; sem o
  cursor 0; e as de 5 a 8) e os desvios 1 a 44, decidindo os dois pendentes: o "v2" do nome do id
  (desvio 3) e a remoção de mais de 10 pares à mão (desvio 16).
- **F9:** ~~responder as quatro decisões da F9~~ (respondidas em 08-out-2026, todas com a
  recomendação: "Decisões da F9"). **Para a prova no Postgres** (a F9 é pesada em SQL: as travas, a
  decisão sob `FOR UPDATE`, o `NOT EXISTS`, o rollback): abrir o Docker Desktop e rodar
  `scripts/postgres-de-teste.ps1`; recomendo não juntar a F9 à `main` sem essa rodada. Os testes
  criam um banco novo cada um, então a 0008 corrigida não esbarra no checksum; só um banco persistente
  (o do `nuvem-local.ps1`) precisa ser recriado (`-Parar` e subir de novo). ~~**Na entrega:** confirmar
  ou mudar as escolhas menores da F9 (os 90 dias dos eventos e os 190 das respostas, o job de hora em
  hora, tirar `RETENTION_MONTHS` e `TRASH_DAYS` dos documentos), **e decidir o piso do vencimento da
  lixeira que chega pelo push**~~ (as escolhas confirmadas e o piso decidido em 08-out-2026: "Fica 24 h
  na lixeira", a D5 em "Decisões da F9"; a rodada no Postgres agora prova também os casos do piso).
  Nada a testar à mão na F9.
- **F10:** ~~responder as quatro decisões da F10~~ (respondidas em 08-out-2026, todas com a
  recomendação: "Decisões da F10"). **Agora (a F10 está pronta, F10f em 08-out-2026):** juntá-la à
  `main` (ela traz a F9, que espera antes a rodada no Postgres de teste) e rodar
  `docs/teste-manual-nuvem-f10.md`: fechar o Agent e conferir o `StartTime`, montar o painel num passo à
  parte e o Agent do `bin\`, abrir as Configurações, ver a contagem do bloco Histórico e decidir. O OK
  expresso para ligar os 6 meses no PC é o clique dele, depois de ver a contagem (nenhum agente o dá),
  e ele pode parar na contagem. O mais provável é ainda não haver ditado com mais de 6 meses no `bin\`
  (o primeiro é de setembro de 2026): o roteiro diz o que aparece no lugar da pergunta e mostra a
  pergunta pelas telas de desenvolvimento. A correção da D4 da F9, que ele confirmou em 08-out-2026
  (o PC só apaga de vez da lixeira o que a nuvem já soube, no máximo até os 6 meses do ditado), não se
  vê à mão sem 30 dias de espera ou o relógio mexido: o teste ponta a ponta a prova, e o roteiro
  oferece rodá-lo. **Na entrega:** confirmar ou mudar as escolhas menores 1 a 12 da F10, entre elas o
  `PRAGMA secure_delete`, os textos do bloco e o desvio 1 da F10d.
- **Os testes à mão da F8 e da F10, adiados pelo Mauro em 09-out-2026** ("esse lance de testar seis meses vai demorar… segue o baile"). A conferência só de leitura do `bin\` no mesmo dia mostrou que o Agent aberto ainda é o da F7 (compilado em 07-out, 13:04): o roteiro da F8 não rodou, e o histórico antigo continua sem subir (sem o aceite, sem a cópia, sem a 0007). O que eles provariam entra no **teste final da F11**, no servidor de verdade: conectar o PC, enviar o histórico, aprender e desfazer pelo celular. Os 6 meses não têm o que mostrar no `bin\` antes de março de 2027 (o primeiro ditado é de 29-set-2026); ficam provados pelos testes automáticos (o portão, a prévia igual ao apagado, a ponta a ponta da D3). As escolhas menores da F8 e da F10 já foram confirmadas.
- **F11 (09-out-2026):** ~~responder as decisões da F11~~ (D2, D3, D4 e a restauração respondidas em
  09-out-2026; a D1 espera o diagnóstico: "Decisões da F11"). **Agora, sem depender da D1** (o detalhe,
  na ordem, está em "O que é do Mauro, na ordem" daquela seção):
  - rodar o diagnóstico na VPS onde a nuvem pode morar (em qual das duas? a D1 começa por isso) e colar
    a saída, com o país e a região do datacenter; e responder a D1 e confirmar as escolhas menores da
    F11, numa mensagem só;
  - a revisão de LGPD: ler o dossiê de duas páginas e o rascunho da política (F11b) e aprovar no chat;
    sem isso nenhum dado real sobe;
  - o Google no PC, antes do deploy: o client Web com o redirect
    `http://localhost:8080/auth/google/callback`, o `scripts/nuvem-local.ps1` e uma segunda conta dele em
    "Usuários de teste", fora da lista permitida, para ver a recusa da Aurora;
  - ~~um destino de backup fora do servidor~~ (decidido: Backblaze B2): a conta, um bucket privado com
    Object Lock (retenção padrão de 30 dias), a regra de ciclo de vida de 30 dias e a application key
    do bucket só com `listBuckets`, `listFiles` e `writeFiles` (`deploy/README.md`, "O bucket e a chave
    do B2");
  - a chave do backup, só dele: gerada no PC pelo script (`age`, num contêiner), a privada no
    gerenciador de senhas e numa segunda cópia fora do PC.
- **Antes da F11, depois da D1 (Parte 2):**
  - ~~o subdomínio e o registro DNS~~ (feito: o subdomínio da instância do mantenedor, só DNS, para a VPS);
  - ~~o client Web de produção~~ (feito: o mesmo client, com o redirect de produção e o do PC); o JSON
    vai ao servidor pelo `enviar-para-servidor.ps1`, e nunca ao chat;
  - a senha do Postgres, o `SESSION_SECRET` e o `DEVICE_TOKEN_PEPPER`, gerados por ele no servidor
    (o comando vem pronto: `gerar-segredos.sh`), e uma cópia do pepper no gerenciador de senhas;
  - ~~a decisão sobre o proxy (a D1)~~ (decidida em 09-out-2026: o Traefik que já roda, só pelos
    rótulos).
- **F11:** rodar o deploy, pelo roteiro `docs/deploy-f11.md` (a ordem está em "A Parte 2 da F11"): o B2
  (`docs/backup-b2-f11.md`), o teste no Docker do PC, a imagem por `ssh`, os segredos, a LGPD aprovada
  antes do `subir.sh`, a conferência de fora, o primeiro backup e a restauração dele no PC; e então o
  teste final (`docs/teste-final-f11.md`).

## Revisão

Depois de uso real: quantos conflitos apareceram, quantos "não aplicado neste PC", quanta memória o
servidor usa de fato e se o teste de restauração mensal está sendo feito. Com dado, reabrir o
iPhone, as preferências e a cifra ponta a ponta.
