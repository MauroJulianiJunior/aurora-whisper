# Migrations

Schema e migrações PostgreSQL para Aurora Whisper.

## Propósito

Este diretório contém todas as migrações de banco de dados PostgreSQL, versionadas sequencialmente.

**Elas rodam por um comando próprio do servidor (`aurora-whisper-server migrate`), nunca na subida
dele** (`05-data-model.md`, "Integridade"; ADR-019, decisão 13). Este README dizia "na inicialização
do servidor", o que contrariava o `05`; corrigido em 01-out-2026.

## Convenção

- Nomenclatura: `NNNN_<tabela>_table.sql` (e.g., `0001_users_table.sql`): quatro dígitos a partir de
  `0001`, **sem buraco nem repetição**, e **um arquivo por tabela** (o leitor e os testes conferem).
- Cada arquivo é uma transação completa, aberta e fechada pelo comando `migrate`: o arquivo **não**
  pode ter `BEGIN`, `COMMIT` nem `ROLLBACK` (o leitor recusa).
- Migrations nunca são modificadas após aplicadas; erros exigem nova migration de rollback. **O
  comando `migrate` recusa um arquivo alterado depois de aplicado** (checksum, abaixo).
  **Exceção (ADR-019, decisão 13): corrigir no lugar o que nunca rodou.** A ADR autorizou isso para
  a 0001-0009, na F1 da etapa 5, que as **corrigiu no lugar** (abaixo) em vez de remendá-las numa
  0010. **Confirmado pelo Mauro em 05-out-2026** (ADR-019, "Decisões da F1", revisão): a mesma regra vale
  **até a primeira aplicação fora de um banco de teste descartável, que é o deploy (F11)**, e vale
  também para a 0010-0016 (novas da F1); depois do deploy, só migration nova. Um banco que já aplicou uma versão
  antiga destes arquivos (de um teste manual, por exemplo) recusa o `migrate` por checksum, e a saída é
  apagar o banco de teste, não editar de novo.
- IDs globais **UUID v7**, **gerados pela aplicação** (`05-data-model.md`, "IDs"; o Agent os gera em
  `internal/storage/id.go`). Nenhuma migration usa `gen_random_uuid()`, `uuid_generate_*`, `SERIAL`
  nem `IDENTITY` (`schema_test.go` confere). Este README dizia v4; corrigido em 01-out-2026.
- Isolamento por usuário **no esquema**: toda tabela tem `user_id NOT NULL REFERENCES users (id)`, fora
  as que não podem ter (`users`, `dictionary_aliases`, `pending_logins`, `pairing_requests`,
  `device_tokens`, cada uma com o motivo em `schema_test.go`). Onde há `device_id`, a chave estrangeira
  é **composta** `(user_id, device_id) -> devices (user_id, id)`: o banco recusa apontar para o aparelho
  de outro usuário.
- Segredo só como hash: `token_hmac` (HMAC-SHA256 com `SESSION_SECRET` ou `DEVICE_TOKEN_PEPPER`),
  `state_hash`, `pre_hash`, `code_hash` (SHA-256). Nenhuma coluna guarda sessão, token ou `code` em claro.

## O que a F1 da etapa 5 fez (ADR-019)

Os nomes de coluna e o formato são desta fatia; o que cada tabela guarda está em
`05-data-model.md`, "Etapa 5: o que entra no Postgres".

**Corrigidas no lugar:**
- **0002** (`devices`): `UNIQUE (user_id, id)`, o alvo das chaves estrangeiras compostas.
- **0003** (`transcriptions`): sai `target_app` (contexto não vai para a nuvem; `schema_test.go` o
  proíbe); `created_at` **sem `DEFAULT`** (é a data do ditado, a do PC, e a retenção conta por ela);
  entram `last_seq` (único por usuário) e a versão que perdeu um conflito (`conflict_final_text`,
  `conflict_device_id`, `conflict_at`: o selo); `CHECK`s de `status` (`done`/`error`), `mode`
  (`literal`/`smart`/`polish`), lixeira e selo.
- **0008** (`sync_events`): o `seq` é **por usuário**, PK `(user_id, seq)`, número tirado do
  contador do usuário; sem `BIGSERIAL` global; entra o `device_id` de origem; sem payload. O comentário
  que dizia que o `BIGSERIAL` bastava como cursor foi corrigido (sob transações concorrentes ele não
  basta: o número sai antes do commit e não volta no rollback).
- **0009** (`applied_mutations`): PK `(user_id, mutation_id)`; entram `status`, `result_version`,
  `merged` e `result_code`, em colunas (nenhuma carrega texto do ditado); e `entity_type`,
  `entity_id` e `device_id`, os três `NOT NULL` (um `mutation_id` reusado para outra entidade não pode
  virar `duplicate` em silêncio).
- **Revisão da F1 (01-out-2026):** a PK de `transcriptions` (0003) é `(user_id, id)`, porque o id do
  ditado vem do cliente, e a chave estrangeira de `corrections` (0006) virou composta, com
  `ON DELETE SET NULL (transcription_id)` (PostgreSQL 15 ou mais novo; o alvo é o 18); a 0012 ganhou
  `return_to` (um caminho da própria origem, com `CHECK` contra open redirect); e os comentários da
  0008 e da 0010 dizem o que o esquema pressupõe (`READ COMMITTED`, o pull num snapshot só, e o
  evento que é o `last_seq` de uma entidade viva não se apaga). A PK por usuário do ditado é
  **confirmada pelo Mauro em 05-out-2026** (ADR-019, "Decisões da F1").
- **F2 (05-out-2026), pela mesma regra de corrigir no lugar:** a 0003 ganhou `text_version` e
  `trash_version` (a versão em que o texto final e a lixeira mudaram por último, de 1 até `version`).
  O push precisa delas para separar a mescla (a lixeira de um lado, o texto do outro) do conflito (o
  texto dos dois lados) (ADR-019, "Decisões da F2"). Um banco de teste que aplicou a 0003 anterior
  recusa o `migrate` por checksum: apague-o.
- **Revisão da F2 (05-out-2026), de novo na 0003:** `text_device_id` e `trash_device_id`, o aparelho que
  fez a última mudança do texto e a da lixeira (nulos quando veio do celular), com chave estrangeira
  composta para `devices (user_id, id)`. O push conta como conhecido pelo aparelho o campo que ele
  mesmo mudou por último (ADR-019, "Decisões da F2", "Revisão da F2"). O comentário do upsert passou a
  dizer o que a F2 faz (`FOR UPDATE` e `INSERT` ou `UPDATE`, sem `ON CONFLICT`).
- **F8c (07-out-2026), pela mesma regra de corrigir no lugar:** a 0009 ganhou `operation TEXT CHECK
  (operation IN ('upsert', 'delete'))`, a operação mandada da primeira vez, nula só quando ela não é
  da lista (a recusa `operacao_invalida`), sem `DEFAULT`. O aprendido tem duas operações (aprender e
  desfazer), e o reenvio com o mesmo `mutation_id` e outra operação é `mutacao_reusada`: sem a coluna,
  o Postgres não teria de onde tirar a operação da primeira vez (ADR-019, "Decisões da F8"). A
  convenção de um arquivo por tabela não deixa um `ALTER` numa 0017. Os testes não sentem (cada um
  cria o próprio banco); um banco que já aplicou a 0009 de antes (o que o `scripts/nuvem-local.ps1`
  migra no Postgres de teste, por exemplo) recusa o `migrate` por checksum: apague-o. O contêiner do
  `scripts/postgres-de-teste.ps1` não tem volume, então `-Parar` e subir de novo basta.
- **F9c (08-out-2026), pela mesma regra de corrigir no lugar:** a 0008 ganhou o índice
  `idx_sync_events_changed_at (user_id, changed_at)`, o da arrumação por idade do job de retenção
  (o evento órfão com mais de 90 dias sai; ADR-019, "Decisões da F9"), e os comentários velhos da
  0008, da 0010 e da 0015 foram corrigidos (o `410` da marca d'água saiu, D2, e o job não a sobe; a
  lápide vive a janela mais 24 horas, decidida em Go). As três mudam de checksum: os testes não sentem
  (cada um cria o próprio banco); um banco persistente que já as aplicou (o do
  `scripts/nuvem-local.ps1`) recusa o `migrate` e precisa ser recriado (`-Parar` e subir de novo).

**Novas** (uma tabela por arquivo):

| Migration | Tabela | O que é |
|---|---|---|
| 0010 | `user_sync_counters` | `last_seq`, `sync_epoch` e a marca d'água das lápides, por usuário |
| 0011 | `web_sessions` | sessões do navegador; só o HMAC do id da sessão |
| 0012 | `pending_logins` | login Google em andamento; `state` e cookie `__Host-pre` só como hash |
| 0013 | `pairing_requests` | pedido de pareamento de um PC (PKCE com retorno a `127.0.0.1`) |
| 0014 | `device_tokens` | token do aparelho; só o HMAC; um por aparelho |
| 0015 | `tombstones` | lápides de ditado apagado de vez |
| 0016 | `learned_pairs` | o aprendido: par curto, restrição e nome do domínio |

**Ficam, sem uso na etapa 5:** 0004, 0005, 0006 e 0007 (dicionário, correções, preferências).

**`schema_test.go` foi atualizado na mesma fatia**, e ganhou as leituras que dão para fazer sem
Postgres (abaixo).

## O ditado no celular (ADR-021)

Depois do deploy, as 0001 a 0016 estão em produção, e a regra é a do fim da "Convenção": **só migration
nova, só aditiva**, uma tabela por arquivo.

| Arquivo | Tabela | O quê |
|---|---|---|
| 0017 | `ai_provider_keys` | **C3a (10-out-2026):** a chave de API do provedor de IA colada no app do celular, **só cifrada** (o id da chave de cifra, o nonce, o cifrado com a etiqueta do GCM e o fim de até 4 caracteres que a tela mostra); uma por pessoa e por provedor (`openai`, `groq`, `anthropic`) |
| 0018 | `dictation_settings` | **C3b (11-out-2026):** as preferências do ditado no celular, uma linha por conta: quem transcreve (`openai` ou `groq`) e o modelo, a dica dos termos aprendidos, o Polish ligado, quem faz o Polish (`openai` ou `anthropic`) e o modelo. Nunca descem aos PCs |
| 0019 | `dictation_consents` | **C3b (11-out-2026):** o aceite do ditado no celular, **uma linha por aceite**: a versão do aviso, os provedores aceitos, quando aceitou e, depois, quando e por que terminou (`revogado` ou `substituido`). Um índice único parcial deixa no máximo um aceite em aberto por conta |

A chave de cifra nunca entra no banco (`AI_KEYS_SECRET_FILE`, `docs/ai-spec/10-env-and-secrets.md`).
`schema_test.go` lê a 0017, a 0018 e a 0019 (as colunas, os tamanhos fixos, as listas dos provedores, os
`CHECK`s, o índice único do aceite em aberto, nenhuma coluna de áudio); o `internal/server/pgstore` confere
as colunas, as listas de provedores e a forma do nome do modelo contra o Go
(`TestSQL_AsChavesDaIAConferemComA0017`, `TestSQL_AsPreferenciasEOAceiteConferemComAs0018E0019` e
`TestSQL_AFormaDoModeloEIgualNoSQLENoGo`) e cada `CHECK` no Postgres
(`TestPostgres_AsChavesDaIANoBanco`, `TestPostgres_AsPreferenciasEOAceiteNoBanco`,
`TestPostgres_PreferenciasEAceiteDoDitado`). Como são **só aditivas**,
`TestPostgres_AsMigrationsDoDitadoNoCelularAplicamSobreUmBancoComDados` aplica as 0001 a 0017 num banco,
põe dados nas tabelas de antes (inclusive uma chave da IA cifrada), roda a 0018 e a 0019 e prova que nenhuma
linha de antes mudou. O "apagar meus dados" apaga as três tabelas do ditado no celular
(`TestSQL_ApagarMeusDadosCobreToda_TabelaComUserID` falha se uma tabela com `user_id` ficar de fora).

## As notas (ADR-022, N7)

O sync das notas e dos marcadores de nota. Pela mesma regra do ditado no celular: **só migration nova, só
aditiva**, uma tabela por arquivo, nenhuma tabela anterior muda.

| Arquivo | Tabela | O quê |
|---|---|---|
| 0020 | `labels` | o marcador de nota: o nome (1 a 50 caracteres, já limpo; **sem** nome único: dois PCs podem criar "Mercado" sem rede, e quem resolve é o PC), as datas, a versão e o `last_seq` |
| 0021 | `notes` | a nota: título (até 1 KiB) e corpo (até 64 KiB), a cor (um dos 8 nomes), fixada, arquivada, a posição do índice fracionário (`COLLATE "C"`: a ordem dos bytes, a mesma do Go e do PC), o ditado de origem, a lixeira, as versões do conteúdo e da lixeira com o aparelho de cada uma, e o selo (título, corpo, aparelho e data, juntos ou nenhum). Sem a retenção de 6 meses |
| 0022 | `note_labels` | os marcadores de cada nota; saem com a nota (`ON DELETE CASCADE`), e **sem** chave estrangeira para `labels` (o marcador pode chegar depois da nota) |
| 0023 | `note_tombstones` | as lápides da nota e do marcador; **nunca vencem** (a nota não tem a janela de 6 meses) |
| 0024 | `note_sync_events` | o log de eventos das notas e dos marcadores, com o seq do mesmo contador do usuário |
| 0025 | `transcription_note_links` | o `virou_nota` do ditado: no máximo uma nota por ditado; sai com o ditado (`ON DELETE CASCADE`, a retenção continua valendo) |

**Por que `note_tombstones` e `note_sync_events` são tabelas à parte**, e não a `tombstones` (0015) e a
`sync_events` (0008): o `CHECK` do `entity_type` delas só aceita os tipos de antes, e as duas já estão em
produção. Alargar o `CHECK` exige um `ALTER`, que esta convenção não deixa (só `CREATE TABLE` e `CREATE
INDEX`). Pelo mesmo motivo o `virou_nota` é uma tabela, e não uma coluna da `transcriptions`. O pull junta as
listas por seq (`internal/server/pgstore`, `mergePage`).

`schema_test.go` lê as seis (as colunas, os `CHECK`s, as chaves, os índices, as quatro colunas de aparelho
com a chave composta, nenhuma coluna de áudio); o `internal/server/pgstore` confere as colunas, as cores e a
expressão da posição contra o Go (`TestSQL_ANotaEOMarcadorUsamAsColunasDas0020E0021`,
`TestSQL_AsListasDa0021SaoAsDoGo`, `TestSQL_AsConsultasDasNotas`). Com um Postgres: cada `CHECK` e chave
(`TestPostgres_Notas`, `TestPostgres_MarcadoresDeNota`, `TestPostgres_LapidesEEventosDasNotas`,
`TestPostgres_LigacaoDoDitadoComANota`, a ordem da posição em `COLLATE "C"` inclusive), e
`TestPostgres_AsMigrationsDasNotasAplicamSobreUmBancoComDados` aplica as 0001 a 0019, põe dados nas tabelas de
antes, roda as 0020 a 0025 e prova que nenhuma linha de antes mudou. O "apagar meus dados" apaga as seis
(`TestSQL_ApagarMeusDadosCobreToda_TabelaComUserID`), e o job de retenção arruma o evento órfão da
`note_sync_events` pela mesma regra da `sync_events` (o evento que é o `last_seq` de uma nota ou de um marcador
vivo fica).

**Estado em 26-out-2026: nada da N7 rodou contra um Postgres** (o Docker estava parado na máquina em que foi
escrita). Os testes com Postgres compilam e pulam; a primeira execução real é a do Mauro.

## As imagens das notas (ADR-022, N10b)

| Arquivo | Tabela | O quê |
|---|---|---|
| 0026 | `note_images` | a imagem da nota, **no banco** (a decisão do Mauro: entra sozinha no backup cifrado, na restauração e no "apagar meus dados"): os bytes já limpos em `bytea` `STORAGE EXTERNAL` (fora da linha, sem a tentativa de compressão; PostgreSQL 16 ou mais novo), a largura, a altura, o tamanho, o SHA-256, a hora do anexo, o `last_seq` e a **lápide na própria linha** (`removed_at`, sem os bytes). Sai com a nota (`ON DELETE CASCADE`) |

Uma tabela só, sem eventos nem lápides à parte: a imagem é imutável (nasce e sai), a lápide sai com a nota
(e a da nota cobre o PUT atrasado de qualquer imagem dela), e a mudança de imagem não tem aparelho de origem
no pull. Os limites (2.000 px, 4 MB, 10 por nota, 400 MB por conta) ficam em Go, como na `0014` do PC.

`schema_test.go` a lê (as colunas, os `CHECK`s, a chave estrangeira com cascata, o `STORAGE EXTERNAL`, o
índice das vivas); `internal/server/pgstore` confere o SQL das imagens contra ela
(`TestSQL_AsConsultasDasImagens`). Com um Postgres: `TestPostgres_ImagensDasNotas` (cada `CHECK`, a chave, o
`attstorage` `e` e a cascata) e `TestPostgres_AMigrationDasImagensAplicaSobreUmBancoComDados` (aplica as
0001 a 0025, põe dados, roda a 0026 e prova que nada de antes mudou). **Estado em 29-out-2026: rodaram
contra o Postgres 18 de teste** (o `scripts/postgres-de-teste.ps1`), com a suíte de contrato inteira.

## Como rodar o `migrate`

É um **subcomando do binário do servidor** (escolha da F1; a ADR e o `05` dizem só "comando próprio do
servidor"): `aurora-whisper-server migrate`. Por quê, e não um binário à parte: é um artefato só para
construir, versionar e levar ao servidor, e as migrations vão **embutidas** no mesmo binário que as
usa (`migrations.FS`), então o binário e o esquema que ele espera nunca se separam. Por quê, e não uma
opção do servidor: a subida do servidor nunca altera o esquema (`05`, "Integridade"); o servidor
(`serve`, F3) só lê o estado (`pgmigrate.Inspect`) e recusa subir com migration pendente, mandando
rodar o `migrate`.

```powershell
# a URL do Postgres só pelo ambiente (nunca por opção de linha de comando: iria para o histórico do shell)
$env:DATABASE_URL = 'postgres://usuario:senha@servidor:5432/banco'
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/server migrate -status   # o que está aplicado e o que falta, sem escrever nada
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/server migrate           # aplica as pendentes
```

- Com um binário compilado: `aurora-whisper-server migrate` e `aurora-whisper-server migrate -status`.
- Em produção, o `migrate` pode rodar com um usuário do banco que cria tabelas, e o servidor com outro,
  que só lê e escreve: a variável é a mesma (`DATABASE_URL`), definida por invocação. **Rodar em
  produção é ponto de parada** (`CLAUDE.md`, ADR-019): quem roda é o Mauro, com `-status` antes.
- Saída: só nomes de arquivo e contagens. Nunca a URL, a senha nem o corpo do SQL. Códigos de saída:
  `0` deu certo, `1` falhou, `2` uso errado ou `DATABASE_URL` ausente.

**Como o `migrate` controla a história** (`internal/server/pgmigrate`; `schema_migrations` no próprio
banco: `version`, `name`, `checksum`, `applied_at`):
- **Uma transação por arquivo**, com o registro em `schema_migrations` na mesma transação: ou o arquivo
  e a versão entram juntos, ou nenhum dos dois. Falhou no meio: o arquivo é desfeito, os anteriores
  ficam, os seguintes não rodam.
- **Checksum**: o SHA-256 do arquivo com fim de linha LF (um checkout com CRLF não o muda). Migration
  **já aplicada que mudou** é recusada, e nada novo é aplicado em cima. Também recusa: migration
  aplicada que este binário não tem (binário mais velho que o banco), a que mudou de nome, e uma
  pendente com número menor que outra já aplicada.
- **Trava**: `pg_advisory_lock` na conexão do comando. Dois `migrate` ao mesmo tempo no mesmo banco: o
  segundo espera o primeiro, vê tudo aplicado e não faz nada. O comando inteiro vence em 10 minutos.
- Recusa um banco que não é UTF8 (o texto ditado é português).
- `-status` só lê: nem a tabela `schema_migrations` ele cria.

## Como rodar os testes contra um Postgres

O `go test ./...` normal **não** sobe contêiner nem toca no Docker (`CLAUDE.md`: subir contêiner muda
o estado da máquina; ADR-019, decisão 15). Os testes que precisam de um Postgres só rodam com a
variável `AURORA_TEST_POSTGRES_URL` definida; sem ela, cada um pula com uma mensagem que diz como
ligar. Quem sobe o Postgres é o Mauro, com o Docker Desktop aberto:

```powershell
powershell -File C:/Desenvolvimento/aurora-whisper/scripts/postgres-de-teste.ps1
# cole a linha $env:AURORA_TEST_POSTGRES_URL = '...' que ele imprime, e então:
go -C C:/Desenvolvimento/aurora-whisper test ./migrations/ ./internal/server/pgmigrate/ ./internal/server/pgtest/ ./cmd/server/ -count=1 -v
powershell -File C:/Desenvolvimento/aurora-whisper/scripts/postgres-de-teste.ps1 -Parar
```

Detalhes (porta só no loopback, senha aleatória, sem volume, o que acontece se o Docker estiver
parado) em `scripts/README.md`. Cada teste cria um banco com nome único (`aw_test_<hex>`) e o apaga no
fim, e os testes recusam um servidor que não seja a própria máquina (`internal/server/pgtest`).

## O que está verificado, e o que não

**Roda sempre, sem Postgres** (e é o que dá para dizer do SQL lendo-o):
- `schema_test.go`: cada tabela e coluna do `05` existe no arquivo certo; as constraints de unicidade
  por usuário estão no texto; nenhuma coluna de áudio, contexto ou `target_app`; nenhuma sequence nem
  ID gerado no banco; toda tabela escopada por usuário; aparelho por chave composta; segredo só como
  hash; sessão, login e pareamento com `expires_at`; as restrições que dá para ler no texto (`CHECK`,
  `NOT NULL`, `UNIQUE`, `FOREIGN KEY` e `ON DELETE` de cada tabela, em `sqlContracts`; a chave
  estrangeira composta em **toda** coluna de aparelho, inclusive `conflict_device_id`; a guarda do
  `return_to`); e um lint léxico (vírgulas, parênteses, comandos).
- `internal/server/pgmigrate`: o leitor (nome, ordem, vazio, controle de transação, checksum) e o
  planejador (alterada, ausente, renomeada, fora de ordem).

**Roda só contra um Postgres de verdade** (com `AURORA_TEST_POSTGRES_URL`):
- `postgres_test.go`: o Postgres aceita todas as migrations e as tabelas são exatamente as do modelo;
  o mesmo `mutation_id` (e o mesmo id de ditado) em dois usuários é aceito e repetido no mesmo é
  recusado; a mutação sem entidade ou sem aparelho é recusada; o `seq` é único por usuário; o
  aparelho de outro usuário é recusado; o contador (`UPDATE ... RETURNING`) não pula número
  com rollback e **serializa dois escritores** (duas transações concorrentes, com o Postgres mostrando
  uma sessão esperando a trava), e oito escritores concorrentes não deixam buraco nem repetem; e as
  constraints de cada tabela, uma a uma.
- `pgmigrate/run_test.go`: migrate do zero aplica tudo; rodar de novo é no-op; arquivo alterado depois
  de aplicado é recusado; dois `migrate` simultâneos não aplicam duas vezes (e a trava é a mesma que
  uma terceira conexão segura); falha no meio desfaz o arquivo; a trava solta no fim.
- `cmd/server/migrate_pg_test.go`: o comando de ponta a ponta, e que a senha não vaza na saída.

**Estado em 01-out-2026: nada disso rodou contra um Postgres.** O ambiente onde a F1 foi escrita não
tinha Postgres nem o Docker Desktop aberto. Os testes com Postgres **compilam** (`go vet`) e **pulam**;
a lógica deles foi conferida por leitura. A primeira execução real é a do Mauro, e o que ela mostrar
(um `SQLSTATE` inesperado, um teste que falha por razão de teste e não de esquema) volta para a F1.

## Referência

- `docs/ai-spec/05-data-model.md` — modelo de dados completo (seção "PostgreSQL")
- `docs/ai-spec/09-security-privacy.md` — dados sensíveis e proibições de logging
- `docs/adr/ADR-019-nuvem-e-celular.md` — as decisões desta etapa (decisões 13 a 15, "Fatias")
- `docs/ai-spec/10-env-and-secrets.md` — `DATABASE_URL` e `AURORA_TEST_POSTGRES_URL`
