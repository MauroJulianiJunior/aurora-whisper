# Data Model — Aurora Whisper

## Fontes
- Windows: SQLite local.
- Cloud: PostgreSQL.
- Áudio/contexto: memória somente.

## IDs
UUID v7 ou ID global ordenável equivalente.

## PostgreSQL

### User
- id uuid PK
- google_sub text unique (1 a 255 caracteres; ADR-019, F3)
- email text unique (em minúsculas, `CHECK (email = lower(email))`; ADR-019, F3)
- display_name text nullable
- avatar_url text nullable
- created_at / updated_at
- disabled_at nullable

### Device
- id uuid PK
- user_id uuid FK
- install_id text
- name text
- platform text
- app_version text nullable
- last_seen_at nullable
- created_at
- revoked_at nullable
- version bigint

Unique `(user_id, install_id)`.

> **F1 (ADR-019, 01-out-2026).** Também `UNIQUE (user_id, id)`, redundante com a PK de propósito: é o
> alvo das chaves estrangeiras compostas `(user_id, device_id)` das tabelas que apontam para um
> aparelho (ditado, evento de sync, mutação aplicada). O token do aparelho mora à parte, em
> `device_tokens` (abaixo).

### Transcription
- id uuid PK
- user_id uuid
- device_id uuid nullable
- source_platform text
- raw_text text
- final_text text
- mode text
- language text nullable
- asr_provider text
- asr_model text nullable
- duration_ms int nullable
- processing_ms int nullable
- confidence real nullable
- target_app text nullable
- status text
- created_at / updated_at
- deleted_at nullable
- purge_at nullable
- version bigint

Índices:
- `(user_id, created_at desc)` ativo;
- `(user_id, deleted_at desc)` trash;
- `(user_id, updated_at)`.

> **Emendado pela ADR-019 (01-out-2026).** No Postgres, `target_app` **sai** (é contexto, REQ-025, e
> o Agent nunca o grava): a 0003 é corrigida no lugar na F1, e o servidor recusa o campo. O
> `created_at` é o do PC (a data do ditado), e é por ele que a retenção conta. Entram: `last_seq` (o
> `seq` da última mudança, para o pull pelo estado atual, com índice `(user_id, last_seq)`) e a
> **versão que perdeu um conflito** (o texto, o aparelho e quando), que é o selo; escolher uma versão
> a limpa. `status` segue o Agent (`done`/`error`); o servidor em memória grava `completed` hoje
> (`internal/server/transcriptions.go:217`), e isso se acerta na F2.
>
> **Como a F1 fechou isso (a 0003, nomes finais; 01-out-2026):**
> - `created_at` **sem `DEFAULT`**: é a data do ditado, a do PC. Um `DEFAULT now()` deixaria a hora em
>   que a nuvem recebeu passar por data do ditado sem ninguém ver.
> - `last_seq bigint not null`, `> 0`, com `UNIQUE (user_id, last_seq)` (é também o índice
>   `(user_id, last_seq)`): cada mudança toma um `seq` novo e grava uma entidade só. O pull junta a
>   entidade ao evento por `(user_id, last_seq)` para devolver o `device_id` de origem.
> - O selo: `conflict_final_text`, `conflict_device_id` (nulo quando a versão veio do celular) e
>   `conflict_at`; o texto e o `conflict_at` vêm juntos (ou nenhum dos dois), e `conflict_device_id`
>   só existe com eles.
> - `CHECK`s: `status in ('done','error')`; `mode in ('literal','smart','polish')`; `version >= 1`;
>   `confidence` de 0 a 1; durações não negativas; `purge_at` só com `deleted_at`.
> - `(user_id, device_id)` e `(user_id, conflict_device_id)` são chaves estrangeiras **compostas** para
>   `devices (user_id, id)` (o `Device` ganha `UNIQUE (user_id, id)` só para isso): o aparelho tem de ser
>   do mesmo usuário.
> - A **PK é `(user_id, id)`**, e não `id` sozinho (como o `05` dizia acima: `id uuid PK`). O id do
>   ditado é o UUID v7 que o **PC** gerou, ou seja, vem do cliente. Com a PK global, o mesmo
>   `historico.db` ligado a duas contas (a real e a de teste, D7), ou um cliente com defeito, faria o
>   push de uma conta colidir com a linha de outra, e o erro ou a resposta mostraria que o id existe em
>   outra conta (o que a ADR-019 já recusou para o `mutation_id`). Por usuário, o mesmo id em duas
>   contas são duas linhas, e o upsert do push é por `(user_id, id)` (a F1 previa `ON CONFLICT`; a F2 lê a linha
>   `FOR UPDATE` e faz o `INSERT` ou o `UPDATE`, ADR-019, "Decisões da F2"). A `Correction.transcription_id`
>   (0006) vira chave estrangeira composta, com `ON DELETE SET NULL (transcription_id)` (a lista de
>   colunas é do PostgreSQL 15 em diante; o alvo é o 18). **Proposta da F1, confirmada pelo Mauro em 05-out-2026**
>   (ADR-019, "Decisões da F1").
>
> **F2 (ADR-019, 05-out-2026; a 0003 corrigida no lugar):** entram `text_version` e `trash_version`, a
> versão em que o texto final e a lixeira mudaram por último (de 1 até `version`). O push as compara
> com a `base_version` do PC para saber o que o servidor mudou depois dela: a lixeira de um lado e o
> texto do outro são mescla; o texto dos dois lados é conflito, e o texto que perdeu vai para o selo
> (ADR-019, "Decisões da F2"). Toda gravação sobe `version` e grava `last_seq`; o `device_id` é o
> aparelho onde o ditado nasceu (o do token que fez o push que o criou) e não muda depois.
>
> **Revisão da F2 (05-out-2026; a 0003 corrigida no lugar de novo):** entram `text_device_id` e
> `trash_device_id`, o aparelho que fez a última mudança do texto e a da lixeira (nulos quando veio do
> celular; chaves estrangeiras compostas para `devices (user_id, id)`, como as outras colunas de
> aparelho). O campo cuja última mudança veio do aparelho que manda a mutação conta como conhecido por
> ele, mesmo com a versão acima da base (a mudança mesclada do próprio PC). `raw_text` e `final_text`
> têm teto de 64 KiB na aplicação (não no banco). **Confirmado pelo Mauro em 05-out-2026** (ADR-019, "Decisões da
> F2", "Revisão da F2").
>
> **ADR-021, C1 (10-out-2026): o ditado que nasce na nuvem (o do celular).** Sem migration: a 0003 já
> aceita `device_id` nulo e `source_platform` em texto livre. O `Store.Create` grava a linha pelo ponto
> único de gravação, com a regra de criar do push:
> - `device_id`, `text_device_id` e `trash_device_id` **nulos** (nenhum PC o criou), `source_platform`
>   `web` (o app do celular) ou `android` (o app nativo, passo 2) — a lista fechada das origens da
>   nuvem; `status` `done`; `version`, `text_version` e `trash_version` 1; sem lixeira nem selo;
> - `created_at` e `updated_at` são o **relógio do servidor** (nos ditados do PC, `created_at` é a data
>   do PC); a retenção conta por ele do mesmo jeito;
> - o evento de sync é `upsert`, versão 1, `device_id` nulo, com o `seq` do contador;
> - o `Create` confere o `sync_epoch` esperado **dentro** da transação que trava o contador (sem janela
>   para o "apagar meus dados" ou um backup restaurado) e confere só o id, o usuário e o epoch, nunca o
>   texto: o mesmo id na conta devolve a linha gravada, e o id com lápide não é recriado;
> - o `Store.Situacao(user_id, id)` lê, sem travar, se o id é um ditado, uma lápide ou nada (o `Get` não
>   mostra a lápide), junto com o epoch da conta.
>
> Não há coluna de áudio nem de contexto: o áudio só existe em memória, durante o pedido (ADR-021).
>
> **ADR-021, C5b (11-out-2026): o que a rota de ditar grava** (`POST /api/v1/dictations`, `06`, "Ditar
> pelo celular"). Sem migration nem tabela nova. A linha é a do `Create` acima, com: `source_platform`
> `web`; `raw_text` o texto do provedor de transcrição como chegou; `final_text` o do Smart da conta
> ou, quando o Polish valeu, o do Polish; `mode` `polish` só nesse caso, senão `smart`; `language`
> `pt-BR`; `asr_provider` e `asr_model` os das preferências da conta (`openai` ou `groq`);
> `duration_ms` a duração que o app mediu (cabeçalho `Ditado-Duracao-Ms`, até 125 s); `processing_ms`
> o tempo do servidor depois do corpo; `confidence` nulo. O `sync_epoch` esperado é o que o
> `Situacao` leu antes de o corpo ser lido. **A idempotência do ditado (o andamento, o "sem fala" e o
> desfecho pago e não gravado) mora na memória do processo, não no banco** (ADR-021, risco 11: duas
> instâncias da API exigiriam levá-la ao banco); o desfecho pago guarda o texto por até 10 minutos e
> nunca vai a log.

### DictionaryEntry
- id uuid
- user_id uuid
- canonical_form
- normalized_form
- language nullable
- domain nullable
- priority int default 0
- active bool
- created_at/updated_at
- version bigint

Unique `(user_id, normalized_form)`.

### DictionaryAlias
- id uuid
- entry_id uuid
- alias
- normalized_alias
- phonetic_key nullable
- source manual/learned/imported
- usage_count int
- timestamps

Unique `(entry_id, normalized_alias)`.

### Correction
- id uuid
- user_id
- transcription_id nullable
- from_text
- to_text
- normalized_from
- normalized_to
- language nullable
- confirmed_by_user bool
- use_count
- timestamps
- version

### UserSettings
- user_id PK/FK
- settings jsonb
- updated_at
- version

### SyncEvent
- seq bigserial
- user_id
- entity_type
- entity_id
- operation
- entity_version
- changed_at

Index `(user_id, seq)`.

> **Emendado pela ADR-019 (01-out-2026).** O `seq` passa a ser **por usuário**: PK
> `(user_id, seq)`, com o número tirado de `UserSyncCounter` (abaixo) no primeiro comando da
> transação. O `BIGSERIAL` global da 0008 sai: sob transações concorrentes, um número menor pode
> ficar visível depois de um maior, e o cursor pularia o evento (o comentário da 0008, que diz o
> contrário, é corrigido). Entra o `device_id` de origem, para mostrar de que aparelho veio o ditado
> (o PC reconhece o eco pela versão, nunca só por ele: ADR-019, "Eco e mescla").
> **Sem payload:** o pull lê o estado atual das entidades e as lápides, não o evento.
>
> **Como a F1 fechou isso (a 0008; 01-out-2026):** PK `(user_id, seq)` (é o índice do cursor: a 0008
> não tem `CREATE INDEX`), `seq bigint not null` sem `DEFAULT` nem sequence nenhuma, `device_id` nulo
> (celular, retenção e "apagar meus dados" não têm aparelho), com chave estrangeira composta para o
> aparelho. `entity_type in ('transcription','learned')` e `operation in ('upsert','tombstone')`:
> `upsert` é a entidade no estado atual (a lixeira é estado), `tombstone` é a apagada de vez.
>
> **Como a F2 grava (05-out-2026):** um evento por mudança de entidade, com o `seq` que a transação
> tirou do contador e a `entity_version` nova; o `device_id` é o do token (nulo quando a mudança veio do
> celular, pelo CRUD). O evento da lápide leva a versão seguinte à última do ditado. Decisão que não
> grava entidade (reenvio, recusa, `gone`, lixeira que já estava lá) não grava evento nem gasta `seq`.
>
> **A arrumação (F9, decidida em 08-out-2026; ADR-019, "Decisões da F9"):** o job de retenção apaga o
> evento com `changed_at` mais velho que **90 dias** que **não é** o `last_seq` de um ditado nem de um
> aprendido vivo do mesmo usuário (`NOT EXISTS` nos dois `UNIQUE (user_id, last_seq)`). O pull junta a
> entidade ao evento dela por `(user_id, last_seq)` para dar o `device_id` de origem, e o aprendido
> não tem coluna de aparelho: apagar o evento de uma entidade viva some com a origem em silêncio. O
> evento de lápide e o de entidade que o job apagou são órfãos e saem. O pull não lê evento nenhum de
> outro jeito, então a idade não importa para o sync. O índice `(user_id, changed_at)` entra na 0008,
> corrigida no lugar (F9c). O evento do **ativo vencido** (apagado sem lápide e sem evento novo) fica
> órfão e sai pela mesma arrumação.

### AppliedMutation
- mutation_id uuid PK
- user_id
- device_id
- applied_at
- result_version nullable

> **Emendado pela ADR-019 (01-out-2026).** A PK passa a ser `(user_id, mutation_id)` (a 0009 tem a PK
> global, e o motor em memória já escopa por usuário). Entram o **status** e o resultado da primeira
> aplicação, para um reenvio receber `duplicate` com a mesma resposta.
>
> **Como a F1 fechou isso (a 0009; 01-out-2026):** PK `(user_id, mutation_id)`; o resultado em
> **colunas**, e não em JSON livre, para a tabela nunca guardar texto do ditado além da retenção:
> `status in ('applied','conflict','rejected','gone')` (`duplicate` nunca é gravado: é a resposta a um
> reenvio), `result_version`, `merged` (o "mesclado"; só vale para `applied`) e `result_code` (o código
> de `rejected`, ou o motivo de `gone`: `apagado`, `fora_da_janela`). O aparelho, como acima, por chave
> composta. Índice só em `applied_at`, para a arrumação por idade.
>
> **A arrumação (F9, decidida em 08-out-2026):** o job apaga a resposta com `applied_at` mais velho que
> `TokenSemUso` + 10 dias (**190 dias**), derivado da constante (`internal/server/cloudstore/
> pareamento.go`), nunca menos. Um PC que volta dentro da vida do token (180 dias contados do último
> uso) reenvia os mesmos `mutation_id` do lote em voo e tem de receber `duplicate`; depois dela, a
> reconexão esquece esses envios, e o mesmo `mutation_id` não volta. Um reenvio que chegasse depois do
> prazo seria uma mutação nova: o do ditado dá `gone` `fora_da_janela` (190 dias passam da janela de
> 6 meses, uns 185 dias contando a folga de 24 h do futuro), e o do aprendido segue a tabela dele (o
> mesmo par dá `applied` com a versão atual, sem gravar). Por isso o prazo cobre a vida do token (o
> aprendido não tem janela) e, para o ditado, a janela mais a folga; abaixo disso, o reenvio de um
> ditado ainda na janela, com a base velha, gravaria uma versão nova e, com o texto mudado no meio
> por outro aparelho, daria um selo à toa (corrigido na revisão da F9, 08-out-2026). A tabela não
> guarda texto. Os 90 dias da "Retenção", abaixo, valem só para o `SyncEvent`.
>
> **A entidade e o aparelho são obrigatórios (revisão da F1).** `entity_type` e `entity_id` (a entidade
> que a mutação mudou) e `device_id` são `NOT NULL`. Sem a entidade, um `mutation_id` reusado para
> **outra** entidade (um cliente com defeito) seria respondido como `duplicate`, e a mudança se
> perderia em silêncio, contra a meta "Sync confirmado perdido: 0". Com ela, a F2 compara a entidade
> do reenvio com a gravada e responde `rejected` quando for outra. O push só vem de um aparelho
> pareado (o celular edita pelo CRUD, que não grava mutação).
>
> **Como a F2 grava (05-out-2026):** toda decisão do push fica aqui, inclusive `rejected` (com o código)
> e `gone` (com o motivo), para o reenvio receber `duplicate` com a mesma resposta; `result_version` é
> nulo nesses dois. A única que não fica é a mutação cujo `mutation_id` ou `entity_id` não é UUID
> (`mutacao_invalida`): não há chave para gravar.
> Na revisão da F2, o reenvio confere também o `device_id`: o mesmo `mutation_id` mandado por outro
> aparelho do usuário é `rejected` (`mutacao_reusada`), como o mandado para outra entidade.
>
> **F8 (07-out-2026; ADR-019, "Decisões da F8"): a operação.** O aprendido tem duas operações
> (`upsert` e `delete`), e o reenvio confere também a operação: o mesmo `mutation_id` com outra é
> `mutacao_reusada`. A 0009 ganha, **corrigida no lugar** (a regra da decisão 13, confirmada pelo
> Mauro até o deploy da F11; a convenção de um arquivo por tabela não deixa um `ALTER` noutro
> arquivo), a coluna `operation TEXT CHECK (operation IN ('upsert', 'delete'))`, nula só quando a
> operação mandada não é da lista (a recusa `operacao_invalida`). O `cloudstore.Applied` ganha o
> campo `Operation`, e o `Duplicate` a compara. A F8c a implementa, com o `schema_test`; um banco de
> teste que aplicou a 0009 de antes recusa o `migrate` por checksum e deve ser apagado.
>
> **Como a F8c fechou isso (07-out-2026):** a linha é `operation TEXT CHECK (operation IN ('upsert',
> 'delete'))`, depois de `entity_id`, **sem `DEFAULT`** (um `INSERT` que esquecesse a coluna não
> gravaria `upsert` no lugar de um `delete`). O servidor grava a operação como a lista a conhece
> (`cloudstore.RecordedOperation`): `upsert` ou `delete`, e nula para qualquer outra; o `delete` de um
> ditado (recusado, `operacao_invalida`) fica gravado como `delete`, e o reenvio dele como `upsert` é
> `mutacao_reusada`, como no Store em memória. O `schema_test` confere a linha inteira, e o
> `postgres_test` o comportamento (as duas da lista e a nula aceitas; `tombstone`, texto livre e
> outra caixa recusados; a linha sem a coluna fica nula).

### Etapa 5: o que entra no Postgres (ADR-019)
Os nomes finais de tabela e coluna são os da F1 (01-out-2026), nas migrations 0010 a 0016, uma tabela
por arquivo (ver `migrations/README.md`). Abaixo, o que cada uma guarda, com o nome da tabela e das
colunas entre crases. Em toda tabela com `user_id`, ele é `NOT NULL` e referencia `users (id)`; segredo
só como hash ou HMAC; as `CHECK`s de formato (32 bytes de hash, tamanhos de texto, expiração depois da
criação) estão nos arquivos.

- **UserSyncCounter** (`user_sync_counters`; uma linha por usuário, separada de `users` para não
  disputar trava com o login): `user_id` PK; `last_seq` (começa em 0; o primeiro `seq` é 1);
  `sync_epoch` (uuid gerado pela aplicação, sem `DEFAULT`; trocado ao restaurar backup e no "apagar
  meus dados"); `tombstone_watermark`, a **marca d'água das lápides**: só sobe (quem a sobe usa
  `GREATEST`) e nunca passa de `last_seq`. **F9 (08-out-2026; ADR-019, "Decisões da F9", D2): a
  coluna fica, mas ninguém a lê.** O pull **não** responde `410` por ela (o desenho da etapa 5 previa
  o `410` quando `after_seq` fosse menor que ela, com `after_seq=0` isento; foi retirado: o `gone`
  impede o ressuscitar, e a releitura não recuperaria a lápide perdida), e o job de retenção **não**
  a sobe. Só o "apagar meus dados" a sobe, ao `last_seq`. Se um dia houver um leitor, ela é o maior
  `seq` de lápide já apagada.
- **WebSession** (`web_sessions`): `id` (uuid v7); `user_id`; `token_hmac` (o **HMAC** do id da sessão
  com `SESSION_SECRET`, 32 bytes, único; o id em claro só existe no cookie); `created_at`,
  `last_seen_at`, `expires_at` (30 dias sem uso, sugestão), `revoked_at`; `label`, um rótulo curto do
  aparelho para a lista de sessões (por exemplo, "Android · Chrome"; até 64 caracteres, nunca o
  user-agent inteiro). **F3:** 30 dias sem uso e teto de 90 dias desde `created_at` (`expires_at`
  anda a cada uso, sem passar do teto); sair e derrubar **apagam** a linha, então `revoked_at` fica
  sem uso (a arrumação de hora em hora apaga as vencidas e qualquer uma com `revoked_at`).
- **LoginPendente** (`pending_logins`): `state_hash` PK (o hash do `state`), `pre_hash` (o hash do
  cookie `__Host-pre`: o vínculo entre o navegador que começou e o que termina), `code_verifier` e
  `nonce` (efêmeros, em claro), `created_at`, `expires_at` (5 min) e `used_at` (uso único: o `UPDATE`
  que marca o uso filtra por `used_at IS NULL`); `return_to`, para onde o callback leva a pessoa
  quando o login termina (por exemplo, a página de aprovação do pareamento, que exige sessão): um
  **caminho** da própria origem, nunca uma URL (começa com `/`, não com `//`, até 256 caracteres,
  sem barra invertida, espaço nem quebra de linha; o `CHECK` do banco é a segunda guarda, a primeira
  é a validação do `POST /auth/start`), com `/` por padrão. **Sem `user_id`**: a linha existe antes de
  o usuário ser conhecido. Guarda o `state` **no servidor**, o que deixa entrar depois o código de 6
  dígitos do iPhone sem refazer o login.
- **PedidoDePareamento** (`pairing_requests`): `id`; `code_challenge` (o desafio do PKCE, S256);
  `device_name`, `platform`, `app_version` e `install_id` do PC; `redirect_port` (a porta do painel
  local; de 1024 a 65535); `confirmation_code` (o código de conferência); `created_at` e `expires_at`
  (10 min, sugestão); `approved_by` (usuário) e `approved_at`; `code_hash` (o hash do `code`
  devolvido, único); `used_at`. **Sem `user_id`**: a linha nasce de um endpoint sem sessão (por isso os
  textos têm limite de tamanho no banco). `approved_by`, `approved_at` e `code_hash` vêm juntos ou
  nenhum, e `used_at` só existe com aprovação.
  **O `state` do retorno é o próprio `id` do pedido**, sem coluna a mais: o `POST /device-auth/start`
  devolve o `pedido` (esse `id`, UUID v7), a `auth_url` o leva, o Agent o guarda, e o painel local
  confere que o `state` de `/nuvem/retorno?code&state` é o do pedido em curso. Quem protege a troca do
  `code` é o PKCE (o `code_verifier`, que só o Agent que começou tem); o `state` só liga o retorno ao
  pedido em curso do painel, e quem não conhece o `id` do pedido de outra pessoa não o forja (o
  `Referrer-Policy: no-referrer` e o `id` fora do log mantêm isso). **Proposta da F1; a F6
  confirma**, e, se precisar de um `state` à parte, a correção da migration segue a regra de "corrigir
  no lugar" (`migrations/README.md`).
- **Token do aparelho** (`device_tokens`, tabela **ao lado** de `devices`; a F1 decidiu): `token_hmac`
  PK (o **HMAC** do token com `DEVICE_TOKEN_PEPPER`, 32 bytes), `device_id` (único: um token por
  aparelho; parear de novo o mesmo PC reaproveita a linha de `devices` e troca o token),
  `created_at` e `last_used_at`. Fica à parte para o material secreto nunca ir junto num `SELECT` de
  aparelho, e para `last_used_at` (escrito a cada chamada autenticada) não mexer na `version` do
  aparelho. Revogar é o `revoked_at` do `Device`; o token de um aparelho revogado recebe `401`. Tokens
  sem uso expiram (prazo na F6; sugestão: 180 dias), por `last_used_at`.
- **Lapide** (`tombstones`): PK `(user_id, entity_type, entity_id)`; `reason` (`apagado`);
  `dictated_at` (a data do ditado, o `created_at` que o PC mandou); `seq` (único por usuário). Vive
  até a data do ditado passar dos 6 meses (índice em `dictated_at` para o job); depois, a recusa por
  janela cobre. Só ditado deixa lápide (`entity_type = 'transcription'`): o aprendido é desfeito por
  `undone_at`. **F9 (ADR-019, "Decisões da F9"):** a lápide vive a janela **mais 24 horas**: o job a
  apaga quando `OutOfWindow(dictated_at, now - 24 h)`, decidida **linha a linha em Go** (o SQL só
  faz um pré-filtro grosseiro que traz candidatos a mais, e o `DELETE` é por chave), na mesma
  transação em que trava o contador. O mesmo corte decide se uma lápide nasce: deixa lápide quando
  `!OutOfWindow(created_at, now - 24 h)` (nunca nasce vencida). Quem apaga um ativo vencido não a
  deixa (o PC mantém o ditado até quem usa ligar os 6 meses nele, F10).
- **Aprendido** (`learned_pairs`): PK **`(user_id, id)`**, porque o `id` é um UUID derivado do hash da
  variante normalizada (o mesmo em todo PC, e portanto em todo usuário). Guarda `variant`,
  `normalized_variant` (único por usuário), `term`, `restricted` e `domain` (o **nome** do domínio,
  nunca o mapa de domínios para executáveis; restrito sem domínio é recusado, como o dicionário faz),
  `version`, `last_seq` (único por usuário) e `undone_at`, quando foi desfeito (a linha continua:
  "desfeito em outro aparelho" precisa ser lembrado; reaprender é a mesma linha com `undone_at` limpo).
  Variante e termo têm até 80 caracteres, o teto de integridade do `dicionario.json`.
  **F8 (07-out-2026; ADR-019, "Decisões da F8"): a 0016 não muda.** O `id` é o UUID v5 da chave
  `CollapseSpaces(Normalize(variant))`, e o `normalized_variant` é essa mesma chave, calculada pelo
  servidor (nunca recebida). Antes do banco, o servidor confere o id contra a variante, os 80
  caracteres (em runas), o caractere nulo e o UTF-8 válido, a variante que não normaliza para vazio
  e o restrito com domínio: nenhum push alcança o `UNIQUE`, os `CHECK`s nem o erro do byte nulo
  (`22021`). O `duplicate` compara também a operação (a coluna nova da 0009, em AppliedMutation).
  O evento de toda mudança do aprendido, o desfazer inclusive, é `upsert`. Sem mescla (o aprendido nunca é `mesclado`), não há colunas por campo nem
  `undone_version`. A linha desfeita guarda o texto **enquanto a conta existir** (F9, D1, confirmada
  pelo Mauro em 08-out-2026): sai só no "apagar meus dados", o job de retenção não a toca, e a
  revisão de LGPD da F11 pode mudar isso.
  **Como a F8c grava (07-out-2026):** a linha é lida `FOR UPDATE` por `(user_id, id)`; o `INSERT`
  leva todas as colunas, com `created_at` e `updated_at` do relógio do servidor (nunca o `DEFAULT
  now()`); o `UPDATE` não toca o `normalized_variant` nem o `created_at` (a grafia da variante pode
  mudar, o id não), então nunca alcança o `UNIQUE`; e, antes do `INSERT`, a chave é conferida: outra
  linha com o mesmo `normalized_variant` (só um id que não vem da chave pelo nome de hoje a
  produziria: o nome do UUID v5 trocado, o "v2", ou uma linha posta à mão; uma regra de chave mudada
  sem trocar o nome dá outra chave e passa, com uma segunda linha) faz o push ser `rejected`
  `campo_invalido`, sem gravar (`06`, "O aprendido no sync", que diz a consequência para o "v2").

Sem uso na etapa 5, e ficam nas migrations para o que vem depois: `DictionaryEntry` e
`DictionaryAlias` (o dicionário completo no banco), `Correction` (a frase inteira não é guardada,
ADR-017) e `UserSettings` (preferências não sincronizam na v1).

`User`: só é criado no primeiro login de um e-mail da lista permitida (`CONTAS_PERMITIDAS`), e o
`google_sub` fica fixado; o mesmo e-mail com outro `sub` é recusado.

### O ditado no celular: o que entra no Postgres (ADR-021)
As tabelas do ditado no celular são **só aditivas** (as 0001 a 0016 já estão em produção): uma por
arquivo, a partir da 0017 (a 0017 é da C3a, a 0018 e a 0019 são da C3b). Nenhuma sincroniza com os PCs, nenhuma passa pelo contador nem gera evento,
e nenhuma guarda áudio (o `schema_test.go` confere).

- **As chaves da IA** (`ai_provider_keys`, a 0017; **C3a, 10-out-2026**): a chave de API do provedor
  que a pessoa cola no app do celular (BYOK; a chave **do PC** continua só no PC), guardada **só
  cifrada**. PK **`(user_id, provider)`**: uma chave por pessoa e por provedor, e trocar é gravar por
  cima. Colunas:
  - `provider`: a lista fechada `openai`, `groq` e `anthropic` (a transcrição no celular é OpenAI ou
    Groq; o Polish no celular, OpenAI ou Anthropic). A mesma lista mora em
    `cloudstore.ProvedoresDeIA`, e um teste do `pgstore` confere as duas;
  - `cipher_key_id` (8 bytes): o id da chave de cifra que cifrou a linha, os 8 primeiros bytes de um
    HMAC-SHA256 da chave de cifra com o rótulo `aurora-whisper/ai-keys/id/v1`, e **não** um número de
    versão. Trocar o arquivo da chave de cifra (`AI_KEYS_SECRET_FILE`, `10`) leva a linha a `SEM_CHAVE`
    sem tentar decifrar;
  - `nonce` (12 bytes, aleatórios a cada gravação) e `ciphertext` (de 17 a 2576 bytes: a chave, até
    2560 bytes, mais os 16 da etiqueta do GCM). A cifra é AES-256-GCM com uma subchave derivada por
    HKDF-SHA256 (rótulo `aurora-whisper/ai-keys/v1`); o AAD leva o `user_id`, o `provider` e o
    `cipher_key_id`, então o cifrado copiado para outra conta ou outro provedor não decifra
    (`internal/server/cofre`);
  - `key_suffix`: o fim da chave que a tela mostra ("Chave terminada em WXYZ"), até 4 caracteres ASCII
    imprimíveis e nunca mais que um quarto da chave (`chavedeapi.Final`), para a tela não precisar
    decifrar;
  - `created_at` (a primeira chave do provedor) e `updated_at` (a última troca), do relógio de quem
    grava, sem `DEFAULT`, com `CHECK (updated_at >= created_at)`.

  A chave de cifra **nunca** entra no banco nem no backup: o backup leva só o cifrado, e restaurado num
  servidor sem o mesmo arquivo, cada chave vira `SEM_CHAVE` (sem erro 500). O "apagar meus dados" apaga
  as linhas da conta (`pgstore.apagarDaConta`). As cópias cifradas de uma chave trocada ou removida
  ficam nos backups (cerca de 30 dias) e no banco da restauração (7 dias): a tela manda revogar a chave
  antiga no provedor (ADR-021, "As chaves").

  **A gravação da chave é conferida sob a trava da conta (C3-correções, 11-out-2026).** O `PUT` do app
  lê o `sync_epoch` da conta antes de chamar o provedor (até 10 s), e a gravação
  (`GravarChaveDeIAConferindo`, nos dois Stores) acontece numa transação que trava, nesta ordem, **a
  linha do usuário** (`FOR NO KEY UPDATE`, a mesma trava do aceite e do revogar) e **o contador de sync**
  (`FOR NO KEY UPDATE`, a mesma do primeiro comando do "apagar meus dados"), e só então confere de novo o
  epoch (`ErrEpochChanged`) e o aceite em aberto, da versão atual do aviso, que cobre o provedor
  (`ErrSemAceite`; `Aceite.Cobre`). Sob o `READ COMMITTED`, quem espera uma trava vê, ao acordar, o que a
  transação que a segurava confirmou. Assim a chave **nunca sobrevive** a um "apagar meus dados" nem a um
  revogar, nem aos que terminam durante a chamada ao provedor: um deles que esteja no meio faz a gravação
  esperar (e então a recusa), ou termina depois dela e apaga a chave que ela gravou. Gravar a chave não
  passa pelo contador do histórico (não gasta `last_seq`, não gera evento). O `GravarChaveDeIA` sem
  condição continua existindo só para montar os testes; o servidor usa o conferido.

  **A ordem do "apagar meus dados"** apaga os aceites **antes** das chaves (`pgstore.apagarDaConta`): o
  revogar trava o aceite e depois apaga as chaves, e se o apagar fizesse o contrário os dois se
  esperariam (deadlock, `40P01`, que o Postgres desfaz abortando o apagar). Um teste do `pgstore`
  (`TestPostgres_RevogarEApagarNaoSeTravam`) segura o revogar entre as duas coisas e lança o apagar.

- **As preferências do ditado no celular** (`dictation_settings`, a 0018; **C3b, 11-out-2026**): uma
  linha por conta (PK `user_id`), gravada inteira a cada `PUT`. Moram só na nuvem e **nunca descem aos
  PCs** (as preferências do PC continuam no PC; não é a `UserSettings`). Sem a linha, a conta tem os
  padrões (`cloudstore.PreferenciasPadrao`): transcrição pela OpenAI com o `gpt-4o-mini-transcribe`, sem
  dica, sem Polish. Colunas:
  - `asr_provider`: quem transcreve, `openai` ou `groq` (a Anthropic não transcreve); `asr_model`: o
    modelo, de 1 a 100 caracteres entre letras, dígitos e `. _ : / @ -` (o nome vem do provedor, e a
    nuvem não o valida contra uma lista fixa, só contra a forma; a mesma expressão mora em
    `cloudstore.ModeloValido`, e um teste confere as duas);
  - `use_hint`: manda os termos aprendidos da conta como dica (decisão E); começa desligada até a C8
    medir. `polish_enabled`: o Polish no celular (decisão N); começa desligado. As duas sem `DEFAULT`;
  - `polish_provider`: quem faz o Polish, `openai` ou `anthropic`, guardado mesmo com o Polish desligado
    (é só a escolha, e o aceite do provedor vale quando ele liga); `polish_model`: o modelo (de 0 a 100
    caracteres da mesma forma), vazio só enquanto o Polish está desligado
    (`CHECK (NOT polish_enabled OR polish_model <> '')`);
  - `created_at` (a primeira gravação) e `updated_at` (a última), do relógio de quem grava, sem
    `DEFAULT`, com `CHECK (updated_at >= created_at)`.

- **O aceite do ditado no celular** (`dictation_consents`, a 0019; **C3b, 11-out-2026**; ADR-021, decisão
  H): **cada aceite é uma linha**, e nenhuma some quando termina, porque provar o consentimento é do
  controlador (LGPD, art. 8º, §2º). PK `id` (UUID v7 gerado pela aplicação). Colunas:
  - `notice_version`: a versão do texto do aviso que a pessoa viu (inteiro a partir de 1). O aceite só
    **vale** com a versão atual, `cloudstore.VersaoDoAviso`; mudar o texto do aviso é subir o número, e o
    servidor passa a pedir o aceite de novo (o aceite velho continua gravado, e não vale). **A mesma
    versão mora no app**, em `web/src/nuvem/avisoDoDitado.ts` (`VERSAO_DO_AVISO`), e o app manda a
    versão do texto que mostrou; um teste do Go (`TestVersaoDoAviso_AppEServidorConferem`) lê esse arquivo
    e prende os dois números;
  - `asr_provider` (`openai` ou `groq`) e `polish_provider` (`openai` ou `anthropic`, **nulo** quando o
    aviso que a pessoa viu não incluía o Polish): os provedores que ela aceitou. O aceite cobre quem
    está em qualquer um dos dois (`Aceite.Inclui`);
  - `accepted_at`: quando aceitou, do relógio do servidor;
  - `ended_at` e `end_reason`, sempre juntos (`CHECK ((ended_at IS NULL) = (end_reason IS NULL))`, e o
    fim nunca antes do começo): `revogado` (a pessoa desligou o ditado no celular, e as chaves dela
    saíram na mesma transação) ou `substituido` (ela aceitou de novo: outro provedor, o Polish com um
    provedor novo, ou outra versão do aviso);
  - **um índice único parcial** (`idx_dictation_consents_open`, `(user_id) WHERE ended_at IS NULL`)
    garante no máximo **um aceite em aberto por conta**, o candidato a vigente. Gravar e revogar travam a
    linha do usuário, então duas gravações ao mesmo tempo se enfileiram.

  **O aceite vigente** é o aceite em aberto da versão atual do aviso: é ele que o `PUT /ai-keys`, o
  `GET /ai-keys/{provedor}/models` e o `PUT /dictation/settings` conferem (a rota de ditar, da C5b,
  confere de novo na hora de chamar). **Quando pede de novo:** trocar de provedor, ligar o Polish com um
  provedor que o aceite não cobre, ou mudar o texto do aviso; **mudar só o modelo não pede**. Aceitar de
  novo exatamente o mesmo (a mesma versão e os mesmos provedores) não grava outra linha: a data fica a
  da primeira vez.

  O "apagar meus dados" apaga as **três** tabelas do ditado no celular da conta, `ai_provider_keys`,
  `dictation_settings` e `dictation_consents` (os aceites que terminaram também), e só as dela
  (`pgstore.apagarDaConta`; um teste lê as migrations e falha se uma tabela com `user_id` ficar fora do
  apagar). Nenhuma das duas guarda áudio, texto ditado nem chave (`schema_test.go` confere).

## SQLite

### local_transcriptions
Espelho útil de Transcription +:
- sync_state;
- server_version;
- last_sync_error.

> **Emendado pela ADR-019 (01-out-2026).** Entra a versão que perdeu um conflito (o selo), como na
> nuvem. Aplicar uma mudança remota **sobe a `version` local** (uma aba aberta recebe `409`) e não
> cria marcador na fila (sem eco). Ditados de outro PC entram aqui com o dono local e o `device_id`
> de origem. No recomeço, `server_version` volta a vazio em todas as linhas. O eco é reconhecido pela
> versão (`entity_version` igual à `server_version`), nunca só pelo `device_id`; e quando o push é
> `applied` com `mesclado: true`, o PC não grava a versão devolvida em `server_version` (o estado
> mesclado chega no pull seguinte).
>
> **Como a F7a fechou isso (migration `0005_sync_fila`, 06-out-2026; ADR-019, "Decisões da F7a/b"):**
> - entram `conflito_texto`, `conflito_device_id` e `conflito_em`, o selo como a nuvem o manda
>   (`versao_em_conflito`; os três juntos, ou nenhum; nulo limpa). A escolha no PC é da F7c;
> - `sync_state` passa a dizer a situação do ditado diante da nuvem: `pending` (o nome legado de
>   "nada a dizer", o padrão de toda linha), `recusado` (a nuvem recusou a última mudança; o código
>   fica em `last_sync_error`) e `fora_da_janela` (a nuvem disse que o ditado tem 6 meses ou mais: o PC
>   o mantém e nunca mais o põe na fila);
> - `server_version` é a versão da nuvem que o PC conhece: a base do próximo envio e o que reconhece
>   o eco. Nula: a nuvem não o tem, ou o PC recomeçou. **Revisão de 06-out-2026:** só o epoch novo
>   (e a primeira rodada da instalação) a apaga; a reconexão e o `410` do mesmo epoch a mantêm, e
>   ela continua sendo a base certa do que mudou com o PC fora;
> - o que vem da nuvem entra com o dono local, `version` 1, o `device_id` de origem (o do payload) e,
>   na lixeira, `purge_at` = `deleted_at` + 30 dias (o mesmo prazo do PC); o que muda sobe a `version`
>   local em um.
>
> **F7c (06-out-2026; ADR-019, "Decisões da F7c"), sem migration nesta tabela:**
> - **o envio inicial** lê as linhas e põe marcadores na `sync_outbox`; nenhuma coluna daqui muda. O
>   filtro único (a prévia, o envio e a completação): do dono, `server_version` nulo, `deleted_at`
>   nulo, `sync_state` `pending`, `device_id` nulo (nasceu neste PC) e dentro dos 6 meses
>   (`retention.ActiveExpiryAt`, a conta da nuvem);
> - **a escolha do selo** no PC (`ResolverConflito`): só na `version` atual e só com selo; "trocar" põe
>   `conflito_texto` em `final_text`; as duas limpam os três `conflito_*` e sobem a `version`;
> - **o "Esquecer esta nuvem"** limpa só o que é do sync: `server_version` nulo, `sync_state`
>   `pending` e `last_sync_error` nulo. O texto, a lixeira, o `device_id` de origem e o selo ficam;
> - **a reconciliação do pull**: a linha sem `server_version`, sem marcador, que mudou aqui
>   (`updated_at` depois de `instalacao.nuvem_esquecida_em`, ou anotada pelo esquecer na
>   `sync_mudou_aqui`, ou sem esquecer) e em que o lado do PC
>   pode vencer (outro texto, ou na lixeira só aqui) não é sobrescrita: ganha um marcador de criar. A
>   que não mudou aqui, a igual e a que só difere porque a **nuvem** a pôs na lixeira recebem o estado
>   da nuvem (revisão da F7c: subir o ditado fora da lixeira não mudava nada lá, e o PC reconciliava
>   a cada rodada).
>
> **ADR-021, C1 (10-out-2026), sem migration: a origem "pelo app".** O ditado que o celular criou na
> nuvem chega com `device_id` nulo e `source_platform` `web` (ou `android`, no app nativo): o pull o
> aplica como `recebido`, e o histórico o mostra como "pelo app". Como o `device_id` nulo era o que
> dizia "nasceu neste PC", **a origem entra na conta**: "nasceu aqui" é `device_id` nulo **e**
> `source_platform` fora de `web` e `android` (um só filtro, `nasceuAquiSQL`, em `internal/storage/
> origem.go`). Ele vale no envio inicial (a prévia, o envio e a completação), no "Esquecer esta nuvem"
> (que anota `nuvem_ditados_em_outra` só se a nuvem esquecida confirmou um ditado nascido aqui) e na
> fila do sync (o `Put` de um ditado novo sem a versão da nuvem). Sem isso, o ditado do app, sem
> aparelho e sem `server_version` depois do esquecer, seria enviado como histórico deste PC para uma
> conta nova. O que o celular criou e o PC recebeu, e editou aqui, sobe como qualquer ditado que a
> nuvem tem (a base é a `server_version`).

### Notas e marcadores (ADR-022, N1; migration `0009_notas`)
Três tabelas novas, só criação (nenhuma linha existente muda, nenhuma tabela ganha coluna). **A N1 é só
o banco local e a API local: o sync não entra.** As linhas ficam com `sync_state = 'pending'` e
**nenhum marcador de nota ou de marcador é posto na `sync_outbox`** (o motor de `internal/sync` ainda
não conhece as entidades, e um marcador de tipo desconhecido poderia travar o sync do histórico). A
N7 faz o envio inicial a partir destas linhas. O `user_id` de `local_notes` e de `local_labels` é
migrado pelo dono fixo como o das outras tabelas (a migração lê as tabelas do schema).

#### local_notes
| Coluna | Tipo | Notas |
|---|---|---|
| `id` | TEXT PK | UUID v7 (`storage.NewID`) |
| `user_id` | TEXT | o dono local |
| `titulo` | TEXT | pode ser vazio; até 1 KiB |
| `corpo` | TEXT | uma linha por bloco, com o prefixo do tipo (`# `, `- `, `- [ ] `, `- [x] `, `1. `); **o banco não interpreta**; até 64 KiB (o teto do ditado e da nuvem) |
| `cor` | TEXT nulo | um de `amarelo`, `laranja`, `vermelho`, `rosa`, `roxo`, `azul`, `verde`, `cinza`; a lista fechada é conferida em Go (`storage.CoresDaNota`), sem `CHECK` |
| `fixada`, `arquivada` | INTEGER 0/1 | `CHECK (... IN (0, 1))` |
| `ditado_de_origem` | TEXT nulo | o id do ditado de que a nota nasceu ("Virar nota", N3); sem chave estrangeira |
| `created_at`, `updated_at` | TEXT | o mesmo formato de largura fixa do histórico (`formatoTempo`) |
| `deleted_at`, `purge_at` | TEXT nulo | a lixeira de 30 dias, a mesma dos ditados |
| `version` | INTEGER | 1 na criação; sobe em toda mudança |
| `sync_state` | TEXT | `pending` (padrão; a N7 define o resto) |
| `server_version`, `last_sync_error` | nulos | da N7 |

Índices: `idx_local_notes_lista` (`user_id, arquivada, fixada DESC, updated_at DESC`, só não
apagadas), `idx_local_notes_lixeira` (`user_id, deleted_at DESC`, só apagadas) e
`idx_local_notes_purge` (`purge_at`, para o expurgo).

- **Sem a retenção de 6 meses.** A nota é coisa que a pessoa guardou de propósito (ADR-022): só a
  lixeira expira. O passo dos 6 meses do expurgo não olha esta tabela.
- **Arquivada não é lixeira:** sai da tela principal, aparece em "Arquivo" e nunca expira.
- **Ordem da lista:** fixadas primeiro, depois `updated_at` decrescente (a criação mais recente desempata);
  na lixeira, `deleted_at` decrescente.
- **Expurgo:** a lixeira vencida (`purge_at <= agora`) sai no mesmo ciclo da dos ditados
  (`internal/agent/expurgo`, `expurgo.ComNotas`), na subida e de hora em hora; o log só tem a
  contagem (`lixeira: expurgo das notas`).
- **O que sobe a `version`:** editar, fixar/desafixar, arquivar/desarquivar, trocar a cor, ir para a
  lixeira e voltar, pôr ou tirar um marcador (estes dois também sobem `updated_at`) e apagar um
  marcador que a nota tinha (este sobe só a `version`, para a nota não pular para o topo da lista).
  Repetir uma operação que já vale (reapagar, remarcar) não muda nada, nem a `version`.

#### local_labels
`id`, `user_id`, `nome`, `nome_dobrado`, `created_at`, `updated_at`, `deleted_at`, `version`,
`sync_state` (padrão `pending`) e `server_version`.

- **Nome único por pessoa, sem diferenciar maiúscula nem acento, entre os não apagados.** O Go
  guarda em `nome_dobrado` o nome em minúsculas, sem acento e com os espaços colapsados
  (`C#` e `C++` continuam diferentes), e o índice único parcial `idx_local_labels_nome`
  (`user_id, nome_dobrado WHERE deleted_at IS NULL`) garante a regra no banco. O nome tem de 1 a 50
  caracteres, numa linha só; o do marcador apagado fica livre.
- **Apagar é soft** (`deleted_at`): o marcador sai de todas as notas (as linhas de
  `local_note_labels` somem na mesma transação) e **nenhuma nota é apagada**.

#### local_note_labels
`note_id` (FK para `local_notes`, `ON DELETE CASCADE`), `label_id` (FK para `local_labels`,
`ON DELETE CASCADE`), PK composta. Índice `idx_local_note_labels_label (label_id)`. Sai junto com a
nota (apagar de vez, expurgo).

#### local_transcriptions.virou_nota (ADR-022, N3; migration `0010_ditado_virou_nota`)
Uma coluna nova, nula, em `local_transcriptions`, e o índice parcial `idx_local_transcriptions_virou_nota`
(`virou_nota WHERE virou_nota IS NOT NULL`). Só criação: nenhum ditado existente muda, e todo ele
continua no histórico.

- **O que é:** o id da nota em que o ditado virou ("Virar nota"). **Sem chave estrangeira**: apagar a
  nota de vez não devolve o ditado ao histórico (o id fica solto de propósito).
- **Esconde sem apagar.** `List` (histórico **e** lixeira) filtra `virou_nota IS NULL`. O ditado
  marcado **não vai para a lixeira** (`deleted_at` e `purge_at` ficam nulos), então o expurgo de 30
  dias não o alcança; e a **retenção de 6 meses** também não (`foraDaJanela` pula `virou_nota` não
  nulo, e a prévia não o conta): o texto dele vive na nota, e a ADR diz que a regra "não alcança o
  ditado que virou nota". `Get` por id não filtra: o "Aprender" e a auditoria continuam achando o
  `raw_text`.
- **Local, sem sync.** Marcar ou desmarcar não passa por `emitir`: nada entra na `sync_outbox`, e o
  ditado **não sobe a `version` nem o `updated_at`** (a base do próximo envio à nuvem fica a mesma). A
  nuvem continua mostrando o ditado. O que a nuvem deve fazer com "virou nota" é ponto aberto da N7.
- **Tudo ou nada.** "Virar nota" cria a nota (`corpo` = `final_text`, ou `raw_text` se o final está
  vazio; sem título nem cor; `ditado_de_origem` = o ditado) e marca o ditado na mesma transação
  (`SQLiteNotaRepository.VirarNota`). Recusa o ditado inexistente (ou de outro dono), na lixeira e o que
  já virou nota. O Desfazer (`DesfazerVirarNota`) limpa a marca e apaga a nota **só se ela está na
  `version` 1 e fora da lixeira** (qualquer edição, fixar, arquivar, marcador ou lixeira sobe a
  versão); editada, a nota fica e só o ditado volta. Também numa transação.

### local_dictionary_entries / aliases / corrections
Espelhos sincronizáveis.

> **Emendado pela ADR-017 (30-set-2026).** Estas três tabelas ficam **vazias e sem repositório** até
> a etapa 5. O dicionário vive no `dicionario.json` (na pasta de dados; ao lado do executável só no
modo portátil, ADR-018), e o que o painel
> aprende vai para lá, como variante de um termo, com a origem `"painel"` em `alias_origem`: é o
> que a nuvem vai chamar de `source=learned` (`DictionaryAlias`, acima). A Correction (com `from_text`
> e `to_text` como a frase inteira, hoje, em `Confirm`) **não** é guardada nesta etapa: nada a usa, e
> ela sobreviveria à retenção de 6 meses com texto ditado. As tabelas também não têm a coluna
> `restricted` nem o mapa de domínios que o dicionário já usa. Trazer tudo isso para o banco, com o
> sync e a decisão de qual usuário é o dono (ver "Dono local", pendente), é da etapa 5. Conflito com
> a previsão do SQLite resolvido pela ADR-017.
>
> **Emendado pela ADR-019 (01-out-2026).** A etapa 5 **não** traz o dicionário para o banco: o
> `dicionario.json` continua a fonte em cada PC, e só o aprendido sincroniza (ver "Aprendido: base da
> última sincronização", abaixo). O dicionário completo no banco fica para depois.
>
> **Emendado pela ADR-020 (01-out-2026).** O campo `origem` de uma entrada do `dicionario.json` tem
> dois valores: `"painel"` (o termo que o Aprender criou: age só pela variante e não vai ao prompt do
> ASR) e `"adicionado"` (o termo que a pessoa pôs pelo "Adicionar termo" das Configurações: termo de
> verdade, com casamento exato e fonético). `alias_origem` usa os mesmos dois valores, por variante.
> Num termo `"adicionado"`, a **prioridade é a dica**: `100` (ou mais) vai ao prompt do ASR antes de
> tudo, na ordem do arquivo; abaixo de `100` (o painel grava `50`), nunca vai. **Nenhum campo novo**:
> um Agent anterior ao valor carrega o arquivo e trata o termo como termo comum.

### local_settings
- hotkey;
- hold_threshold_ms;
- beep_enabled;
- overlay_position;
- microphone_device_id;
- autostart;
- asr_policy;
- local_model_id;
- local_context_enabled;
- protected_field_policy;
- local_port;
- text_mode — `literal`/`smart`/`polish`, padrão `smart` (migration `0002`, 29-set-2026);
- ai_provider — `""`/`openai`/`anthropic` (0002);
- ai_model — texto livre, como o provedor nomeia (0002);
- microphone_always_ready — `0`/`1`, padrão `0` (false): "microfone sempre pronto", aberto o tempo todo
  com 300 ms de captura antes das teclas; desligado, o microfone só abre durante o ditado. Migration
  `0004`, ADR-015, 29-set-2026. É só a escolha de quem usa: nenhum áudio é guardado.

Linha única (`id = 1`), com `version` para concorrência otimista. A coluna `autostart` não é a
fonte da verdade: o início automático é lido e gravado no registro do Windows (pacote
`autostart`), e a API local não usa a coluna. **A chave de API não tem coluna**: mora no
Gerenciador de Credenciais (`storage.TestSchema_NenhumaColunaDeSegredo` trava isso).

### sync_outbox
- mutation_id PK
- entity_type
- entity_id
- operation
- base_version
- payload_json
- created_at
- attempt_count
- next_attempt_at
- last_error_code

> **Emendado pela ADR-019 (01-out-2026): um marcador por entidade.** `UNIQUE(entity_type,
> entity_id)` e **sem** `payload_json`: o conteúdo é lido da linha na hora de enviar. Guarda o
> `mutation_id` da tentativa em curso e a `version` local enviada; o marcador só sai em
> `applied`/`duplicate` se a `version` não mudou desde o envio. Sem nuvem conectada, nada entra aqui.
> A tabela está vazia em todo banco (nenhum código a escreve), e a F7a a refaz por migration nova do
> SQLite, sem perda.
>
> **Como a F7a fechou (migration `0005_sync_fila`, 06-out-2026).** A `sync_outbox` da 0001 foi
> **renomeada** para `sync_outbox_0001`, intocada (com o índice dela), e a nova nasceu no lugar:
> - PK `(entity_type, entity_id)`: um marcador por entidade (só `transcription` na etapa 5; o
>   aprendido é da F8);
> - `campos` (inteiro, bits): o que mudou desde a última versão que a nuvem confirmou: `1` o texto
>   final, `2` a lixeira (entrar ou sair). Criar não é bit: o ditado com `server_version` nulo sobe
>   inteiro. Duas gravações juntam os bits (`campos | novos`);
> - `mutation_id` e `versao_enviada`: o envio em curso (o id da mutação e a `version` local lida para
>   montá-la), gravados antes da rede; nulos fora de voo. O reenvio repete o mesmo id;
> - `criado_em`. **Nenhum texto.**
>
> Quem escreve: toda gravação do histórico no repositório SQLite, na mesma transação da linha (o
> ditado novo e o texto que chega atrasado sem bit; a edição com o bit do texto; a lixeira e o
> restaurar, só quando a linha muda, com o bit da lixeira), só para o ditado fora de
> `fora_da_janela`, e quando (lido na transação): com `instalacao.nuvem_aparelho` presente,
> sempre; **desconectado, só se a linha tem `server_version`** (revisão de 06-out-2026, ADR-019,
> "Decisões da F7a/b", decisão 2): a mudança feita fora num ditado que a nuvem já tem sobe na volta,
> com a base e os campos certos. O ditado que a nuvem nunca teve continua de fora (é do envio
> inicial, F7c). O expurgo apaga o marcador junto. O aplicador do que vem da nuvem nunca escreve
> aqui. Quem lê e tira: a fila do sync (`internal/storage/fila_sqlite.go`). A prova de que a 0005 não
> perde nada: um banco na 0004 com ditados, lixeira, preferências, a instalação e uma linha na fila
> velha, lido tabela por tabela antes e depois (`migracao_0005_internal_test.go`).
>
> **F7c (migration `0006_sync_envio_inicial`, 06-out-2026; ADR-019, "Decisões da F7c").** Aditiva:
> colunas aqui e uma tabela nova, vazia (`sync_mudou_aqui`, abaixo); nenhuma linha muda:
> - `inicial` (`0` ou `1`, padrão `0`): o marcador foi posto pelo "Enviar o histórico". Conta para o
>   progresso ("enviando 120 de 800") e sobe por último: os pendentes saem na ordem `mutation_id IS
>   NULL, inicial, criado_em, entity_id` (o em voo, o ditado novo e as edições, o histórico);
> - `selo_escolha` (`manter` | `trocar`, com `CHECK`) e `selo_em` (o `conflict_at` do selo escolhido):
>   a escolha do selo feita no PC, que sobe pelo push. Os dois juntos, ou nenhum. Nenhum texto.
>
> Quem mais escreve aqui na F7c: o envio inicial (marcadores sem campo, com `inicial` 1, em blocos de
> 500 por transação, `ON CONFLICT DO NOTHING`), a completação de cada rodada depois do aceite (o
> ditado novo feito desconectado, o restaurado, `inicial` 0), a reconciliação do pull (um marcador
> com os dois bits, que não mudam o envio de criar, mas dizem que a nuvem tem o ditado) e a
> escolha do selo (só num ditado com `server_version`, também desconectado). O "Esquecer esta nuvem"
> apaga a tabela inteira. **A regra do `emitir` na F7c (revisão de 06-out-2026):** num ditado com
> `server_version`, toda mudança, conectado ou não; num ditado **sem** `server_version`, só o `Put` do
> ditado novo (sem bit), conectado, e só o nascido aqui (`device_id` nulo): a edição, a lixeira e o
> restaurar de um ditado do histórico não entram (antes entravam conectado, e o histórico subia antes
> do clique e sem a cópia, e a lixeira ia para a nuvem); e a edição não mexe nos bits de um marcador
> de criar que já está lá (o ditado sobe inteiro, lido na hora). A lixeira tira da fila o marcador
> de criar parado (sem bit, fora de voo) de um ditado sem `server_version` (o do ditado novo e o do
> envio inicial); o em voo e o da reconciliação ficam. A prova de que a
> 0006 não perde nada: um banco na 0005 com ditados (um com selo, um de outro PC), marcadores (um em
> voo), o cursor, a instalação e as preferências, lido tabela por tabela antes e depois
> (`migracao_0006_internal_test.go`); e a 0006 que falha no meio não deixa nada.
>
> **F8 (07-out-2026; ADR-019, "Decisões da F8"), sem mudar a tabela:** entra o marcador `learned`,
> um por id do aprendido: sem texto, `campos` 0, nunca `inicial` (não conta no progresso do envio).
> Quem o escreve é a varredura do aprendido (abaixo). O `upsert` só depois do aceite
> (`instalacao.nuvem_envio_aceito_em`, D3); antes dele, sai só o `delete` de um id que a nuvem já
> tem (sem texto; escolha menor 5 da ADR). E o aceite automático do `CompletarEnvio` (sem ditado
> elegível) passa a exigir também que não haja troca a subir (M = 0): com M > 0, a rodada espera o
> clique, e a pergunta aparece com N = 0. A operação não fica no marcador: sai do espelho na hora
> do envio (presente aqui, `upsert`; saiu daqui, `delete`); a do envio em voo é guardada com o
> `mutation_id` e repetida no reenvio (onde, a F8e decide). O esquecer continua apagando a tabela
> inteira.
>
> **Como a F8e fechou (07-out-2026), sem mudar a tabela:** o marcador `learned` nasce pelo executor
> do espelho (`Fila.MudarAprendido`, `AplicarComAprendido`), com `campos` 0 e `inicial` 0. **A
> operação do envio em voo fica no espelho:** enquanto o marcador está em voo, a projeção guardada
> do id não muda (o executor recusa a troca com `ErrProjecaoEmVoo`; a mudança feita no voo só sobe a
> `versao_local`), então o reenvio, com o mesmo `mutation_id`, sai da mesma projeção: a mesma
> operação e o mesmo payload. A resposta que tira o marcador, ou limpa o envio, pode trocá-la. O
> envio (`Fila.PendentesDoAprendido`) devolve os marcadores `learned` à parte dos ditados (os em voo
> primeiro, depois por `criado_em`) e, antes do aceite, segura o que seria `upsert` (fica na fila,
> não sai); o `delete` sai. Os ditados (`Pendentes`, `Contar`, `Progresso`) não veem o marcador
> `learned`. A pergunta do esquecer (`ResumoDoEnvio.Mudancas`) soma o marcador `learned` de uma
> mudança feita aqui (a linha do espelho com `versao_local > versao_base`); o que o epoch novo põe
> num par que a nuvem já tinha confirmado não conta, como no ditado (revisão da F8f, 07-out-2026).

### sync_mudou_aqui
- entity_type
- entity_id
- PRIMARY KEY (entity_type, entity_id)

> **F7c, na reconferência (migration `0006_sync_envio_inicial`, corrigida no lugar antes de qualquer
> deploy, 06-out-2026; ADR-019, "A reconferência da F7c").** Os itens que o "Esquecer esta nuvem" viu
> mudados aqui sem a nuvem esquecida ter recebido a mudança. Só o id: nenhum texto. Nasce vazia.
> Quem escreve: o esquecer, na transação dele, antes de a fila sair (`anotarMudouAqui`): entra o
> ditado com marcador na fila (menos o do envio inicial que não mudou desde o clique), fica o que um
> esquecer anterior anotou e continua sem `server_version`, e entra, havendo um esquecer anterior, o
> que mudou depois dele sem `server_version`; sai o que voltou a ter `server_version` (foi
> reconciliado) e o que não existe mais. O recomeço do epoch novo a esvazia, junto com
> `instalacao.nuvem_esquecida_em`. Quem lê: o aplicador do pull (`EstadoDoItem.MudouAqui`): o
> anotado, sem `server_version`, conta como "mudou aqui" e não é sobrescrito. Item por item, e não uma
> referência recuada até a mudança mais antiga (como a revisão da F7c fez): o recuo pegava também o
> ditado recebido da nuvem depois dela, e uma mudança num ditado que nunca sobe prendia a referência
> nela para sempre.

### sync_state
- user_id
- device_id
- last_server_seq
- last_success_at

> **Emendado pela ADR-019 (01-out-2026).** `user_id` é o **dono local**; `device_id` é o que a nuvem
> deu no pareamento. Entra o `sync_epoch` visto por último (um epoch diferente leva ao recomeço).
>
> **Como a F7a fechou (0005, 06-out-2026):** entram `sync_epoch` (nulo: adota o da próxima resposta),
> `pareado_em` (o `nuvem_conectado_em` do pareamento a que o cursor pertence) e `ultimo_erro` (o
> código de lista fechada da última rodada que falhou; nulo depois de uma que deu certo, junto com
> `last_success_at`). O cursor é o `last_server_seq`, gravado na transação da página do pull. Uma
> linha só.
>
> **Revisão de 06-out-2026 (ADR-019, "Decisões da F7a/b", decisão 2):** outro `pareado_em`, ou outro
> aparelho, é reconexão da mesma instalação (a D3 garante a mesma conta): a linha **passa** ao
> pareamento novo com o cursor e o epoch, e só os envios em curso da fila são esquecidos. O `410` só
> zera o `last_server_seq`. O recomeço (cursor 0, epoch nulo, as outras linhas apagadas) fica para o
> epoch novo e para a primeira rodada da instalação (sem linha). `last_success_at` só é gravado quando
> a rodada fez algo, quando a anterior falhou, ou a cada 10 minutos (`AnotacaoOciosa`); `ultimo_erro`,
> só quando o código muda: o PC parado não escreve no banco a cada busca.

### Aprendido: base da última sincronização (ADR-019, F8)
O que o PC e a nuvem tinham do aprendido na última sincronização, para a diferença em 3 vias entre o
`dicionario.json`, essa base e a nuvem: uma edição à mão no arquivo ou pelo `dictlearn` não volta
como eco. As tabelas `local_dictionary_*` (vazias) podem servir, ou uma nova: a F8 decide. Nada novo
entra no `dicionario.json`.

> **F8.0 (07-out-2026; ADR-019, "Decisões da F8"): o desenho. A F8e o implementa, e os nomes
> finais saem dela.** Uma tabela nova (proposta: `sync_aprendido`), na **migration 0007**, aditiva,
> e não as `local_dictionary_*`: a 0001 não tem a restrição e prende as linhas por chave estrangeira
> em cascata. Uma linha por id do aprendido (o UUID da variante), pelo dono local, com:
> - **a base:** o par que a nuvem confirmou (variante, termo, restrição, nome do domínio, desfeito ou
>   não) e a `server_version` dele, e a projeção que o `dicionario.json` tinha quando a base foi
>   gravada (a "projeção local guardada": a grafia do termo daqui, `alias` ou `alias_restrito`);
> - **o par da nuvem que ainda não vale aqui** (não aplicado, pulado, arquivo indisponível), com a
>   versão e o motivo;
> - **a intenção do aplicador**, gravada **antes** do arquivo (write-ahead): o par e a projeção
>   esperada, com a situação "aplicando";
> - **a situação** (aplicado, aplicando, não aplicado, desfeito aqui, desfeito em outro aparelho,
>   trocado em outro aparelho, já vale aqui, recusado) e **o motivo**, de listas fechadas, e quando
>   a situação começou (a etiqueta some em 30 dias); no "recusado", o motivo é o código da recusa, e
>   a linha guarda **a projeção recusada**: a varredura não a manda de novo, e só uma mudança do par,
>   um epoch novo ou o esquecer a liberam (sem isso, o par iria a cada rodada, para sempre);
> - **a versão local**, que sobe a cada mudança daqui (o "mudou durante o voo"): na varredura, e no
>   ato do Aprender e do Desfazer do painel (o caminho do gancho `AoMudar`), para o id que já tem
>   linha, mesmo com o laço parado. Um Desfazer seguido de um Aprender do mesmo par, que deixa o
>   arquivo como era, continua sendo mudança daqui.
>
> À parte, o conjunto **"veio da nuvem"**: só ids, sem texto, que o "Esquecer esta nuvem" não apaga
> (no molde da `sync_mudou_aqui`; tabela ou coluna, a F8e decide). Entra o id que o aplicador
> escreveu no arquivo; sai quando a pessoa aprende o mesmo par aqui. O envio para uma conta nova o
> filtra (D3).
>
> **Quem escreve:** a varredura, no começo de cada rodada, antes do `CompletarEnvio` (a projeção
> nova e os marcadores, numa transação; sem mudança, não escreve; antes do aceite, conta o M); o
> aplicador do pull (a intenção antes do arquivo; o resultado depois, na transação da página, com os
> ditados e o cursor); a resposta do push (a base nova, ou o "recusado"); o Aprender e o Desfazer
> do painel (a versão local). **Quem lê:** a varredura, o aplicador, o envio (a operação e o
> payload na hora) e a Local API (`GET /api/aprendizado`, as linhas de estado; `GET /api/nuvem`, o
> M).
>
> **O esquecer** zera a `server_version` e a situação, **menos na linha "aplicando"**, que fica com
> a projeção esperada até a varredura seguinte a reconciliar pelo arquivo (o id dela já está no
> "veio da nuvem" e continua lá: sem isso, o par da conta esquecida pareceria mudado aqui e subiria
> para a conta nova). Guarda o "desfeito aqui" (aqui, ou anotado na `sync_mudou_aqui` com
> `entity_type` `learned`: a F8e escolhe), só com o id, que sobe só de volta à mesma conta; não toca
> no "veio da nuvem"; e apaga o texto (fica o id) do que veio da conta esquecida e não está no
> arquivo: o não aplicado, o pendente, o arquivo indisponível e as etiquetas. **O epoch novo**, no
> molde do `Recomecar`, zera a `server_version`, marca de novo todo par (o presente vira `upsert`,
> menos o do "veio da nuvem", que o PC de origem sobe de novo no epoch novo dele; o desfeito aqui,
> `delete`) e libera o "recusado". O `410` no mesmo epoch não mexe aqui. **A 0007
> não volta o cursor** (`sync_state.last_server_seq`): nenhuma nuvem tem aprendido antes da F8, e a
> escolha é um risco aceito na ADR.
>
> **A retenção do texto:** a linha que vale no arquivo guarda a projeção (o que o `dicionario.json`
> já tem). O texto da linha desfeita sai quando a nuvem confirma o desfazer (fica o id); na linha
> com a etiqueta, quando a etiqueta some (30 dias, ou a pessoa aprende de novo). O par da nuvem que
> não vale aqui guarda o texto enquanto a conta é a mesma, e o esquecer o apaga (acima).
> **A prova da 0007** segue a da 0006: um banco na 0006 com dados, lido tabela por tabela antes e
> depois; o cursor e as `server_version` dos ditados não mudam; a 0007 que falha no meio não deixa
> nada.

#### sync_aprendido (migration `0007_sync_aprendido`, F8e, 07-out-2026)
- user_id (o dono local), entity_id (o UUID v5 da variante, `dictionary.IDDoPar`); PRIMARY KEY
  (user_id, entity_id)
- **a base:** server_version (nula: a nuvem não tem o par, ou o PC esqueceu a versão), base_desfeita
  (0/1: o par confirmado está desfeito lá), versao_base (a versão local que a base cobre)
- versao_local (sobe a cada mudança daqui; `versao_local > versao_base` é "mudou aqui e a nuvem não
  confirmou", e o esquecer não mexe nas duas)
- **a projeção guardada** (`dictionary.ParDoPainel`): variante, termo, restrito, dominio (nulo: sem
  domínio), em_alias_restrito; a variante nula é o id fora da projeção. Durante o voo, é a do envio;
  no "recusado", é a recusada
- **a projeção esperada** da intenção (só em "aplicando"): esperada_variante, esperada_termo,
  esperada_restrito, esperada_dominio, esperada_em_alias_restrito; nula nessa situação é a intenção
  que tira o par
- **o par da nuvem guardado** (`dictionary.ParDaNuvem`, com a versão): nuvem_versao, nuvem_variante,
  nuvem_termo, nuvem_restrito, nuvem_dominio, nuvem_desfeito. É o pendente (não aplicado, pulado,
  arquivo indisponível), o da intenção ou o da etiqueta
- situacao, motivo, situacao_em (a data de quando a situação começou; a mesma situação gravada de
  novo a mantém)
- conta_esquecida (a conta, `instalacao.nuvem_conta`, que o esquecer guardou na linha que depende
  dela: o "desfeito aqui" e, desde a revisão da F8f, a linha presente mudada aqui que a nuvem tinha
  (pode ser o Desfazer que a varredura ainda não viu) e o par aprendido aqui que a nuvem confirmou)
- **o par perdido** (migration `0008_sync_aprendido_perdido`, revisão da F8g, 08-out-2026):
  perdido_variante, perdido_termo, os dois juntos ou nenhum (`storage.ParPerdido`). É a troca deste PC
  que perdeu na nuvem (o conflict, D4), a projeção do envio quando a resposta chega, e é a linha de
  estado do "trocado em outro aparelho" no `GET /api/aprendizado` (o `06`): a troca que vale aqui é a da
  nuvem, e a de estado, a que este PC tinha aprendido. Só existe com a etiqueta (`desfeito_em_outro`,
  `trocado_em_outro`) e na linha "aplicando" que aplica o par da nuvem dela: qualquer outra situação
  gravada a tira (`storage.GuardaOPerdido`), e o esquecer também. O delete não conflita (o desfazer
  sempre vence na nuvem), então não há par perdido de um desfazer.

> **A 0008** só põe as duas colunas, nulas, na `sync_aprendido` (`ALTER TABLE ... ADD COLUMN`): nenhuma
> linha muda e o cursor não volta. A prova segue a da 0007 (`migracao_0008_internal_test.go`: um banco
> na 0007 com uma linha do espelho, lido tabela por tabela antes e depois; a 0008 que falha no meio
> não deixa nada).

#### sync_veio_da_nuvem (migration `0007_sync_aprendido`, F8e)
- user_id, entity_id; PRIMARY KEY (user_id, entity_id). Só o id; nenhum texto.

> **Como a F8e fechou (07-out-2026; ADR-019, "O estado da F8e").** Os nomes finais estão acima, e a
> 0007 é só `CREATE TABLE` (nenhuma tabela que existe ganha coluna; o cursor não volta a 0). As
> escolhas que o desenho deixou para a F8e:
> - **a base não guarda outra cópia do texto do par da nuvem:** só a versão, o desfeito e a versão
>   local que ela cobre. O texto de uma linha que vale no arquivo é a projeção guardada (o que o
>   `dicionario.json` já tem), e nada nas decisões precisa do texto da nuvem da base (o eco é pela
>   versão; o PC que recebe compara com o arquivo);
> - **a projeção recusada é a projeção guardada** na situação "recusado": a varredura só a troca
>   quando o par muda, e aí libera a linha. Sem colunas à parte;
> - **o "veio da nuvem" é uma tabela**, e não uma coluna: o esquecer e o epoch novo nem a tocam;
> - **o "desfeito aqui" fica no espelho**, e não na `sync_mudou_aqui` (que continua só do ditado): é
>   a linha ausente da projeção com `versao_local > versao_base` (`EstadoDoAprendido.DesfeitoAqui`),
>   e não depende da situação `desfeito_aqui`. **A conta a que ele pertence** é o id da conta na nuvem
>   (`instalacao.nuvem_conta`, e não o `sub`), guardado pelo esquecer em `conta_esquecida`; a
>   primeira rodada do pareamento seguinte (o recomeço do `Preparar`) o mantém mudado aqui de volta à
>   mesma conta, e o solta (a versão coberta passa a ser a local) numa conta nova, ou quando não sabe
>   a conta. **O Desfazer que a varredura ainda não viu** (o PC desconectado, ou o par com um envio em
>   voo) deixa a projeção presente, com a versão local acima da base: o esquecer guarda a conta
>   também nessa linha quando a nuvem tinha o par (uma versão, da base ou do par guardado), e, de
>   volta à mesma conta, a marca fica (`EstadoDoAprendido.MesmaContaEsquecida`) até a linha ganhar
>   uma projeção, uma base ou uma versão coberta: a varredura que vê o par sair não o assenta, e o
>   delete sobe quando o pull trouxer a versão. Numa conta nova, a marca sai. **O par aprendido aqui
>   que a nuvem confirmou** (presente, sem mudança daqui, fora do "veio da nuvem") também guarda a
>   conta: numa conta nova, a versão local dele sobe (é mudança daqui para ela: espera o clique, o
>   pull dela o pula até lá, e depois a nuvem decide, D4); de volta à mesma conta, nada muda (revisão
>   da F8f, 07-out-2026);
> - **as listas fechadas** são conferidas em Go (o executor recusa o resto com
>   `ErrAcaoDoAprendido`), e não por `CHECK`: a situação (`storage.SituacoesDoEspelho`), o motivo do
>   "não aplicado" (`dictionary.MotivosDoPar`) e o código da recusa (de 1 a 40 caracteres, `a-z` e
>   `_`, como o `last_sync_error`). Os booleanos têm `CHECK (0, 1)`.
>
> **O que escreve:** o executor do espelho (`Fila.MudarAprendido`, sem conferir o epoch: a varredura,
> a intenção do aplicador e a reconciliação dela; `Fila.AplicarComAprendido`, na transação dos
> ditados e do cursor, com o epoch: a resposta do push e a página do pull), com a ação de cada id
> (`AcaoDoAprendido`) conferida inteira antes de gravar (o par é deste id e passa no `ConferirPar`; a
> esperada só com "aplicando", e qualquer outra situação a apaga; a versão coberta de 0 até a local);
> a ação vazia não escreve. `Fila.SubirVersaoLocal` (o Aprender e o Desfazer do painel, no ato, só
> no id que já tem linha). **O esquecer** (`EsquecerNuvem`): guarda a conta no "desfeito aqui"; zera
> a `server_version` e o `base_desfeita` de toda linha, inclusive a "aplicando" (a versão de uma
> conta esquecida nunca é tomada pelo eco de outra); apaga o par da nuvem guardado (o texto e a
> versão: o do não aplicado, do pendente, das etiquetas e o da intenção); tira a situação, o motivo e
> a esperada, menos na linha "aplicando"; fica a projeção guardada, as versões locais e o "veio da
> nuvem". **O recomeço** (`Recomecar`, e o do `Preparar` sem estado): solta o "desfeito aqui" de
> outra conta; marca de novo o par presente (só depois do aceite, D3) e o ausente que a nuvem tinha
> desfeito, ou que saiu daqui com uma versão da nuvem conhecida; deixa de fora a linha "aplicando" e
> o que a nuvem nunca teve; zera a `server_version` e a `nuvem_versao`; e libera o "recusado". O
> `410` no mesmo epoch não mexe aqui. **A prova:** `migracao_0007_internal_test.go` (a 0006 com
> dados, todas as tabelas do `sqlite_master` lidas antes e depois; a 0007 que falha no meio) e
> `aprendido_sqlite_test.go`.

### instalacao
Chave/valor do que é desta instalação, e de nenhuma outra (migration `0002_instalacao`, só `CREATE`):
- nome text PK
- valor text

Chaves:
- `dono_local`: o dono fixo — o `user_id` de todas as linhas locais;
- `dono_local_migrado_em`: quando as linhas antigas foram migradas para ele;
- `dono_local_copia`: onde ficou a cópia do banco feita antes da migração (vazio quando o banco
  não tinha linha a migrar).

Chaves novas da etapa 5 (ADR-019; nomes finais na F6 e na F7c):
- o `install_id` da instalação (gerado uma vez);
- a URL da nuvem, o `sub` da conta ligada a esta instalação, o `device_id` dado no pareamento e
  quando conectou;
- onde ficou a cópia do banco feita antes do envio inicial.

**F7c (06-out-2026), os nomes finais:**
- `nuvem_envio_aceito_em`: quando a pessoa clicou em "Enviar o histórico" (ou quando a primeira
  rodada viu que não havia nada a enviar). Presente, a rodada completa o que faltar; **F8 (D3):**
  e só com ela a varredura do aprendido põe o `upsert` (antes, só o `delete` de um id que a nuvem
  tem); a rodada só a grava sozinha sem ditado elegível **e** sem troca a subir (M = 0);
- `nuvem_envio_total`: quantos marcadores o envio inicial pôs (o "de 800"); some quando o envio acaba;
- `nuvem_copia`: onde ficou a cópia de antes do envio (uma por conta: o clique de novo, depois de uma
  queda no meio, não copia outra vez);
- `nuvem_esquecida_em`: a referência da reconciliação (só sobe, de volta à mesma conta, o que mudou
  aqui depois dela, e o que o esquecer anotou na `sync_mudou_aqui`); o recomeço de um epoch novo a
  apaga. É o instante do esquecer. (A revisão de 06-out-2026 a fazia recuar até o `updated_at` mais
  antigo do que mudou aqui e a nuvem não recebeu; a reconferência do mesmo dia trocou o recuo pela
  anotação item por item, que não pega o ditado recebido da nuvem nesse meio.);
- `nuvem_ditados_em_outra`: a nuvem esquecida tinha confirmado ditados nascidos neste PC (a pergunta
  do envio, na seguinte, diz "Alguns destes ditados podem já estar na nuvem que este PC esqueceu.").

**A revisão da F8g (08-out-2026):**
- `nuvem_dicionario_usuario`: o `usuario` do `dicionario.json` que o espelho do aprendido conhece,
  gravado pelo Agent na subida (`cmd/agent`, `prepararOAprendido`), antes do laço. O arquivo que o
  Agent acabou de recriar vazio (`garantirDicionario`: ele sumiu entre duas execuções), ou o de outro
  `usuario`, é outro arquivo: o espelho do anterior é solto (`syncengine.SoltarOArquivoAntigo`: o par
  sai da projeção sem virar mudança daqui, e o que a nuvem tem vivo fica "não aplicado neste PC" com
  `arquivo_indisponivel`, e volta ao arquivo novo pela varredura), para nenhum par virar delete. O
  valor vazio é o arquivo novo cujo espelho ainda não foi solto (a subida que caiu no meio): a seguinte
  refaz. Sem o arquivo (a criação falhou), nada é gravado. O esquecer não a apaga.

O "Esquecer esta nuvem" apaga `nuvem_url`, `nuvem_conta`, `nuvem_email`, `nuvem_nome`,
`nuvem_aparelho`, `nuvem_conectado_em`, `nuvem_envio_aceito_em`, `nuvem_envio_total` e `nuvem_copia`
(o arquivo da cópia fica no disco) e grava as duas últimas; ficam `install_id` e o dono.

O token do PC **não** fica aqui: mora no Gerenciador de Credenciais (`AuroraWhisper/nuvem`).

**F7a (06-out-2026):** a fila do sync lê `nuvem_aparelho` (presente: a nuvem está conectada, e as
gravações põem marcador) e `nuvem_conectado_em` (o pareamento do cursor) na própria transação; os
nomes são constantes de `internal/storage` (`ChaveNuvemAparelho`, `ChaveNuvemConectadoEm`) que o
pareamento usa também.

Separada de `local_settings` de propósito: preferência é de quem usa, editável pelo painel;
identidade do banco não se edita depois de gravada.

### Dono local
Decisão do Mauro, 29-set-2026: "dono local fixo, migrando tudo pra esse dono e fazer a cópia antes
de migrar". Até ali o dono era o `usuario` do `dicionario.json`, ou `local` sem ele — e trocar o
dicionário escondia do painel todo o histórico anterior, porque toda listagem é escopada pelo dono.

- O `user_id` das tabelas locais é o **dono fixo da instalação**: um UUID v7 gerado uma vez e
  gravado em `instalacao`. O `usuario` do dicionário serve só ao dicionário.
- Primeira subida sem dono fixo (`storage.GarantirDonoFixo`):
  1. cópia completa com `VACUUM INTO` — consistente mesmo em WAL — para
     `historico.db.antes-do-dono-AAAAMMDD-HHMMSS.db`, ao lado do banco. A cópia é conferida: existe,
     abre e tem as mesmas linhas, tabela por tabela. Um arquivo já existente nunca é sobrescrito;
  2. numa transação: gera o dono, grava, faz `UPDATE` de `user_id` para ele em **toda** tabela que
     tenha a coluna (descobertas no schema), qualquer que seja o valor antigo (`mauro`, `local`,
     `''`...), e registra quando migrou e onde ficou a cópia. Tudo ou nada.
- Cópia que falha: nada migra. O Agent registra `WARN` com o motivo, usa a regra antiga naquela
  execução (para o painel não aparecer vazio) e tenta de novo na próxima subida. Transação que
  falha: o mesmo, e a cópia fica onde foi feita.
- No máximo 3 cópias (`storage.MaxCopias`) ao lado do banco: cada falha depois do `VACUUM INTO`
  deixa uma cópia completa, e uma falha que se repete a cada subida as acumularia. Com 3 lá,
  nenhuma nova é feita e nada migra — o Agent segue na regra antiga, com `WARN` — até alguém ler o
  motivo no log e apagá-las.
- A regra antiga só vale quando o banco **não tem dono** (a cópia, a contagem ou a transação
  falharam; `storage.ErrSemDonoFixo`). Banco com dono gravado que não se lê (vazio, erro de
  leitura): o Agent sobe **sem histórico** — a regra antiga ali gravaria ditados que o painel não
  mostraria nunca mais, porque a migração não roda sobre um banco com dono.
- Banco sem nenhuma linha com `user_id` (instalação nova): o dono é gerado sem cópia.
- Com o dono gravado, nenhuma subida copia nem migra de novo. Linha gravada depois com outro
  `user_id` — um Agent anterior ao dono fixo rodando sobre o mesmo banco — fica fora do painel e
  não é migrada; toda subida conta essas linhas (`storage.LinhasDeOutroDono`) e, se houver, registra
  `WARN` com a contagem por tabela. Migrá-las automaticamente é decisão pendente do Mauro (com
  cópia antes).
- Log: uma linha `INFO` com o dono, o caminho da cópia e as linhas migradas por tabela; nunca texto.
- A cópia foge da retenção: guarda todo o texto ditado até a migração, **inclusive a lixeira** e o
  que o expurgo de 30 dias / 6 meses apagaria depois, e o Agent nunca a apaga. Depois de conferir
  o painel, apague-a (ver "Retenção" e `09-security-privacy.md`). Fica fora do git por `/bin/` e
  `*.db` no `.gitignore`.
- Leitores (`cmd/dictlearn`) abrem o banco só para leitura e leem o dono de `instalacao`; banco
  sem ele cai na regra antiga, com aviso.
- Pendente: como o dono local se relaciona com o usuário da cloud, que deriva da sessão (ver
  "Integridade"), quando o pareamento de device existir. A ADR-017 (30-set-2026) **não** resolve
  isso: o dicionário continua com o `usuario` do arquivo, e o histórico, com o dono fixo. Ambos
  seguem para a etapa 5.
- **Resolvido pela ADR-019 (01-out-2026), "Dono local e conta":**
  - as linhas locais continuam com o dono fixo; o usuário da nuvem vem da sessão ou do token, e
    nenhum payload leva `user_id`;
  - a instalação fica ligada a **uma** conta: o `sub` vai para `instalacao` ao conectar;
  - o pull grava as linhas com o dono local, e um ditado de outro PC entra no histórico deste;
  - **trocar de conta numa instalação é recusado na v1**, com aviso (o histórico da conta A subiria
    para a B); fica para depois;
  - o aprendido sincroniza por um id que não depende do `usuario` do arquivo.

## Retenção
- active history: 6 meses.
- manual delete: deleted_at + purge_at 30 dias. (No PC, desde a F10: a lixeira que a nuvem ainda não
  soube espera por ela, no máximo até os 6 meses do ditado; abaixo, "A retenção no PC".)
- auto-retention aos 6 meses: purge definitivo. (No PC, só depois do OK de quem usa; abaixo.)
- se trash atingir 6 meses antes dos 30 dias, retenção de 6 meses prevalece.
- corrections/dictionary persistem até exclusão. A variante que o painel aprende (ADR-017) é um
  trecho de até 4 palavras do que o ASR escreveu e fica no `dicionario.json` até a pessoa desfazer,
  inclusive depois dos 6 meses do ditado de onde veio.
- `Correction.transcription_id` usa `ON DELETE SET NULL`.
- SyncEvents/AppliedMutation: ~~[ASSUMPTION] 90 dias ou compactação~~. **Decidido na F9 (08-out-2026;
  ADR-019, "Decisões da F9"), em dois prazos:** o `SyncEvent` órfão (o que nenhuma entidade viva
  guarda como `last_seq`) com mais de **90 dias**; a `AppliedMutation` com mais de **190 dias**
  (`TokenSemUso` + 10 dias, derivado da constante). Escolhas menores, ~~a confirmar pelo Mauro~~
  confirmadas pelo Mauro em 08-out-2026 ("Confirmo todas").
- **Emenda (ADR-019, 01-out-2026):**
  - na nuvem, os 6 meses e os 30 dias contam pela **data do ditado** que o PC mandou (`created_at`),
    nunca pela hora em que a nuvem recebeu; ela recusa criar ditado fora da janela (`gone`);
  - apagar de vez um ditado ainda dentro da janela deixa uma **lápide**, que vive até a data do
    ditado passar dos 6 meses;
  - como o pull lê o estado atual, e não os eventos, a idade do `SyncEvent` deixa de importar para o
    sync; a retenção dele vira só arrumação (F9, abaixo);
  - no PC, os 6 meses existem desde a F10 (08-out-2026), mas só ligam com a prévia da contagem e o
    OK expresso de quem usa; sem o OK, o PC guarda tudo (abaixo, "A retenção no PC");
  - o backup da nuvem guarda texto já apagado até a cópia girar (prazo na F11).
- **A retenção na nuvem (F9, decidida em 08-out-2026; ADR-019, "Decisões da F9"):**
  - o job roda no processo do servidor, **na subida e de hora em hora**, por usuário, com o relógio
    de parâmetro; cada item é lido sem trava, **decidido em Go** (nunca por uma data no SQL, que
    prende o fim do mês e apagaria até 3 dias antes) e **decidido de novo sobre a linha travada**: o
    ditado restaurado ou mudado entre a leitura e a trava fica. Um item por transação (e, nas lápides
    e na arrumação, um lote por transação curta: a revisão da F9); a segunda rodada não acha nada
    (NFR-013);
  - o **ativo** com 6 meses sai **sem lápide e sem evento** (a recusa por janela cobre, e o PC
    mantém o ditado até quem usa ligar os 6 meses nele, F10); a **lixeira** que venceu (30 dias ou os 6 meses, o que vier primeiro)
    sai com **lápide e evento `tombstone`** (`device_id` nulo) se ainda dentro da janela, e só com o
    `DELETE` se não; o "excluir definitivamente" segue a mesma regra. **A lixeira nunca começa antes
    do próprio ditado** (a revisão da F9): um `deleted_at` anterior ao `created_at` (o relógio de um
    PC atrasado) conta do `created_at`, no `purge_at` gravado e no vencimento do job; o `deleted_at`
    fica o que o PC mandou. **E nunca vence antes de 24 h depois de chegar à nuvem** (a D5, decidida
    pelo Mauro em 08-out-2026: "Fica 24 h na lixeira"): o `purge_at` que o push e o CRUD gravam é o
    vencimento acima, nunca antes da chegada (o relógio do servidor no push) mais 24 h, e esse piso
    nunca depois da janela de 6 meses do ditado (a janela ganha). A lixeira com o `deleted_at` antigo
    (o PC que ficou sem internet; o relógio atrasado) fica 24 h na lixeira da nuvem e do celular e
    então sai; a de sempre conta os 30 dias do `deleted_at`. O job lê o `purge_at` gravado; o
    restaurar o limpa, a lixeira seguinte calcula de novo, e o reenvio não mexe nele;
  - a **folga de 24 h** vale em toda decisão do limite dos 6 meses: deixa lápide quando
    `!OutOfWindow(created_at, now - 24 h)`, e a lápide vence quando `OutOfWindow(dictated_at,
    now - 24 h)`;
  - a arrumação apaga o `SyncEvent` órfão com mais de 90 dias (poupando o que é o `last_seq` de um
    ditado ou de um aprendido vivo) e a `AppliedMutation` com mais de 190 dias;
  - a linha do **aprendido desfeito** não é tocada: fica enquanto a conta existir (D1);
  - **sem `410` por marca d'água** (D2) e sem cota de escrita por conta (D3, F11);
  - o AC-018 vale, na nuvem, "até 30 dias mais até 1 h" (o job é de hora em hora);
  - o job não alcança o backup da nuvem (F11) nem as cópias `antes-da-nuvem` e `antes-do-dono` no PC
    (F10: elas só saem pelo botão de quem usa, abaixo).
- **A retenção no PC (F10, decidida e implementada em 08-out-2026; ADR-019, "Decisões da F10";
  o roteiro manual é `docs/teste-manual-nuvem-f10.md`):**
  - **A lixeira (30 dias) ganha uma guarda:** o `PurgeExpired` não apaga de vez o que a nuvem pode
    ter e ainda não soube, no máximo até os 6 meses do ditado (a janela). Fica o ditado da lixeira
    que tem um marcador na `sync_outbox` (parado, em voo ou de reconciliação), ou que está na
    `sync_mudou_aqui` sem `server_version` (a pendência anotada pelo Esquecer), ou que foi para a
    lixeira depois do `nuvem_esquecida_em` num ditado criado antes dele, sem `server_version`. Não
    segura o `recusado` nem o `fora_da_janela` (saem aos 30 dias), nem o que nunca teve nuvem, nem
    o ditado nascido aqui que nunca subiu (o `SoftDelete` tira o marcador de criar parado). O
    resultado é sempre um subconjunto do que o expurgo apaga hoje, então não pede OK;
  - **Os 6 meses do ativo ficam atrás de um portão que recusa por padrão:** todo ditado **do dono**
    (as linhas de outro `user_id` ficam) fora da janela, ativo ou na lixeira, sai, decidido linha a
    linha em Go e em UTC (`!agora.Before(ActiveExpiryAt(created_at))`, a conta da nuvem), em lotes
    de 500 por cursor `(created_at, id)`, com o marcador de ditado e a `sync_mudou_aqui` junto; o
    `local_corrections` vira nulo pela FK; o aprendido (`learned`, `sync_aprendido`,
    `sync_veio_da_nuvem`) nunca é tocado. Os que têm o selo saem sem a escolha de quem usa. O método
    que apaga lê o OK **dentro da transação**; sem ele, apaga zero. O passo só roda com o dono fixo;
  - **As chaves da tabela `instalacao`** (sem migration; não começam por `nuvem_`, então o Esquecer
    não as apaga): `retencao_6m_aceita_em` (o OK), `retencao_6m_rodada_em` (a última rodada do
    ciclo, o freio), `retencao_6m_copia` (a cópia feita, uma só) e `retencao_6m_pausada_em` (o freio
    pausou a regra);
  - **O ciclo do expurgo** (a subida e de hora em hora) roda a lixeira com a guarda, depois o passo
    dos 6 meses, e grava a `retencao_6m_rodada_em`. O freio: a última rodada há mais de 7 dias (o
    Agent parado, ou o relógio que pulou para a frente) pausa a regra (tira o OK, grava a pausa,
    nada é apagado) e, na lixeira, adia por uma rodada a liberação pelo teto da janela;
  - **A cópia do OK** é `historico.db.antes-dos-6-meses-AAAAMMDD-HHMMSS.db` (`MaxCopias = 3` por
    marca), uma só, registrada em `retencao_6m_copia`: os OKs seguintes não copiam enquanto ela
    existir. Sem nada a apagar, não há OK nem cópia;
  - **O `historico.db` apaga de verdade:** `PRAGMA secure_delete = ON`, para o texto apagado não
    ficar nas páginas livres do arquivo (o `-journal` que o SQLite apaga do disco fica fora do
    alcance). Ele e o `foreign_keys` vão no DSN (`_pragma`), e não num `Exec` depois de abrir: valem
    por conexão, e o driver troca a conexão que teve uma consulta cancelada (revisão da F10);
  - **As cópias** `antes-do-dono`, `antes-da-nuvem` e `antes-dos-6-meses` guardam o texto que a
    retenção apaga. O Agent nunca as apaga sozinho: elas saem só pelo botão **Apagar as cópias**
    (F10e), que apaga os arquivos que casam exatamente com o padrão do nome, na pasta do banco, e
    recusa a `antes-da-nuvem` enquanto o envio do histórico não foi aceito (a `nuvem_copia` sem o
    `nuvem_envio_aceito_em`).
- Exceção da etapa 5 (ADR-019): a cópia do banco feita antes do envio inicial, como a
  `antes-do-dono`, fica fora da retenção; desde a F10e, ela sai só pelo botão acima.
- Exceção: a cópia `historico.db.antes-do-dono-*.db` feita antes da migração para o dono fixo (ver
  "Dono local") não passa pela retenção — tem a lixeira e o que seria expurgado, e o Agent nunca a
  apaga sozinho. Quem instalou a apaga depois de conferir o painel, pelo botão **Apagar as cópias**
  (F10e).

## Integridade
- user sempre deriva da sessão.
- cascades explícitos.
- migration destrutiva exige aprovação.
- SQLite migrations transacionais no startup.
- PostgreSQL migrations executadas separadamente do startup em produção. **Emenda (ADR-019):** por um
  comando próprio do servidor (`migrate`), nunca na subida; as 0001-0009 nunca rodaram e foram
  corrigidas no lugar na F1 (ver `migrations/README.md`). **F1 (01-out-2026):** o comando é
  `aurora-whisper-server migrate` (um subcomando do binário do servidor, com as migrations embutidas),
  com `schema_migrations` (versão, nome e checksum), uma transação por arquivo, recusa de arquivo
  alterado depois de aplicado e `pg_advisory_lock` contra dois `migrate` ao mesmo tempo.
- Isolamento por usuário no esquema: onde há `device_id`, a chave estrangeira é composta
  `(user_id, device_id)` para `devices (user_id, id)` (em toda coluna de aparelho, inclusive
  `conflict_device_id`); a PK de `transcriptions`, a de `applied_mutations`, a de `sync_events`, a das
  lápides e a do aprendido são por usuário: um id que vem do cliente nunca colide com o de outra conta.

## Dados sensíveis
| Dado | Proteção |
|---|---|
| raw/final text | TLS + auth. Legível no servidor na v1 (ADR-019): Postgres sem porta publicada, HTTPS terminando no servidor, backup cifrado |
| email/google_sub | acesso mínimo |
| device token | Credential Manager/hash server-side (HMAC com `DEVICE_TOKEN_PEPPER`) |
| sessão web | cookie `__Host-` `HttpOnly`; no servidor, só o HMAC com `SESSION_SECRET` (ADR-019) |
| provider secrets | server-side |
| chave de API do provedor de IA de quem usa | Credential Manager do Windows, nunca SQLite |
| chave de API do provedor de IA colada no app do celular (ADR-021) | `ai_provider_keys`, só cifrada (AES-256-GCM, subchave por HKDF da chave de cifra de `AI_KEYS_SECRET_FILE`, que nunca entra no banco nem no backup); o app vê só o fim (até 4 caracteres). Quem tem a raiz da VPS decifra |
| áudio | RAM. **ADR-021:** o do celular também, na memória da API só durante o pedido (no máximo 3 corpos de até 1 MiB ao mesmo tempo), repassado ao provedor da pessoa e descartado ao fim; nunca em disco, log ou banco |
| contexto UIA | RAM |
