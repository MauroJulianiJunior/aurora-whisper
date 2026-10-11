# Implementation Plan — Aurora Whisper

## Estratégia

Implementar por vertical slices verificáveis. Não começar por IA contextual avançada nem por deploy. Primeiro provar input→audio→output; depois persistência/dashboard; depois benchmark ASR; depois contexto; depois cloud/sync; por fim empacotamento/hardening.

## Ordem

| Ordem | Slice | Objetivo |
|---:|---|---|
| 1 | S0 Bootstrap | repo, CI, skeleton |
| 2 | S1 Dictation shell | hotkey + audio + overlay + mock insert |
| 3 | S2 Local data/UI | SQLite + dashboard |
| 4 | S3 ASR benchmark | selecionar provider local |
| 5 | S4 Real ASR | integrar provider |
| 6 | S5 Smart context | dictionary + UIA + resolver |
| 7 | S6 Cloud core | API + Postgres + auth |
| 8 | S7 Sync | outbox/pull + cloud dashboard |
| 9 | S8 Cloud ASR | gateway + fallback |
| 10 | S9 Learning | correction workflow |
| 11 | S10 Packaging | tray/autostart/installer/update |
| 12 | S11 Hardening | security/observability/perf |
| 13 | S12 Release candidate | E2E e revisão |

**A ordem mudou (roteiro de 29-set-2026; ADR-017).** O roteiro de trabalho do Mauro pôs "aprender
com as edições do painel" no item 3, antes da nuvem (item 5): a S9 (Learning) roda **antes** de S6 a
S8, no que não depende delas. A parte de sincronização (a Correction que sincroniza, TEST-008, e o
sync do dicionário, AC-021) segue para depois: ela é da S7 e da etapa 5 do roteiro. Ver TASK-019.

**Etapa 5 (ADR-019, 01-out-2026).** A etapa 5 do roteiro é S6 e S7 mais o celular por PWA (Android
primeiro). A S8 (Cloud ASR) fica para depois. As tasks da nuvem viram as fatias F0 a F11 da ADR-019,
na seção "Etapa 5" abaixo.

## TASK-001 — Bootstrap

Criar:
- `go.mod`;
- `cmd/agent`;
- `cmd/server`;
- `web`;
- internal packages;
- migrations;
- CI;
- `.env.example`.

Validação:
```bash
go test ./...
go vet ./...
go build ./cmd/agent
go build ./cmd/server
cd web && pnpm install && pnpm lint && pnpm build
```

Commit:
`chore: bootstrap aurora whisper monorepo`

## TASK-002 — Keyboard hook + state machine
- Win32 `WH_KEYBOARD_LL`.
- idle/primed/recording/processing/inserting/success/error/blocked.
- tap vs hold.
- bypass com modifiers.
- watchdog.

Testes unitários + manual Notepad/Chrome/VS Code.

## TASK-003 — Audio + VAD + overlay
- captura RAM-only;
- VAD;
- amplitude;
- overlay;
- beep;
- no-speech.

Testar ausência de temp files.

## TASK-004 — Text inserter
- UIA;
- clipboard fallback;
- preserve/restore;
- manual-copy fallback.

Matriz mínima:
- Notepad;
- Chrome textarea;
- WhatsApp Web;
- VS Code;
- inputs web comuns.

## TASK-005 — SQLite + Local API + React
- migrations SQLite;
- history/settings/dictionary;
- loopback API;
- local token;
- React routes.

## TASK-006 — ASR benchmark harness
Criar contrato/provider mock e CLI de benchmark.

Métricas:
- WER/CER;
- Entity Accuracy;
- p50/p95;
- RTF;
- RSS;
- CPU/GPU quando aplicável.

Candidatos:
- runtime local da família Whisper;
- runtime local Parakeet ou equivalente;
- outro local leve identificado no momento;
- cloud baseline.

Gate:
- RSS alvo ≤~1 GB;
- PT-BR/EN;
- Entity Accuracy técnica ≥95% após dictionary;
- p50 estimado ≤500 ms;
- estabilidade.

Saída:
`docs/benchmarks/asr-YYYY-MM-DD.md`.

## TASK-007 — Provider local vencedor
- model manager;
- checksum;
- warmup;
- partial/stream se possível;
- metadata.

Não promover default antes do benchmark.

## TASK-008 — Dictionary engine
- canonical/aliases;
- normalization;
- phonetic key pluggable;
- priority/domain/language.

## TASK-009 — UIA Context Engine
- app/control;
- nearby text;
- selection;
- protected detection;
- bounded context;
- zero persistence.

## TASK-010 — Smart resolver
- uncertainty;
- candidate score;
- dictionary boost;
- self-correction PT-BR/EN;
- constrained cloud resolver;
- Literal/Smart.

## TASK-011 — Polish
Decidido na ADR-016 (29-set-2026): preservar e organizar; chamada direta ao provedor com a chave
de quem usa (BYOK). Não é reescrita global e não passa por gateway cloud.
- Smart base;
- organização separada (pontuação, parágrafos, listas), sem trocar palavra;
- raw/final;
- diff;
- opt-in;
- fallback Smart em até 3,5 s (até 8 s no ditado longo, pelo tamanho do texto; ADR-016, emenda de
  01-out-2026), sem perder o ditado;
- guardas locais (números, siglas, termos do dicionário, palavras, formato): a saída é a entrada
  menos algumas palavras, na mesma ordem, sem orçamento para trocar ou acrescentar (ADR-016,
  "Decisões tomadas na implementação");
- só o texto sai da máquina; nenhum texto ditado no log.

Fatias, em ordem, cada uma com testes e um commit local (sem `go test` que chame API real):
- S0: ADR-016 e as emendas da spec (só documentação);
- S1: guardas e prompt em `internal/resolver`;
- S2: cliente HTTP em `internal/ia` (`net/http`, sem SDK);
- S3: benchmark (TASK-011b);
- S4: Agent (`Preferencias`, resolver sempre montado, adaptador da chave, goroutine com prazo,
  modo por tentativa no histórico, linha de log);
- S5: inserção (`\n` por colar) e tela (`polish_disponivel`, `polish_ultimo_motivo`, textos) — feita
  (ver ADR-016, "Decisões tomadas na implementação (S5)"); a ligação do último motivo (S4) fica para
  o merge.

## TASK-011b — Benchmark do Polish
Decidido na ADR-016. `cmd/polishbench` (pronto, S3), no molde de `cmd/entitybench`, usando o mesmo
`Polir` e as mesmas guardas do Agent. Dataset novo em `testdata/polish-dataset/frases.json`
(pronto, 88 frases sintéticas), mais as 46 `reference` do dataset de ASR. Métricas, regra de
escolha e relatório `docs/benchmarks/polish-YYYY-MM-DD.md`: ver `19-eval-plan.md`, "Polish".
O `gpt-5-nano` entra como linha de base (`-linha-de-base`: medido, nunca vence), e a regra
aprovada não tem piso de organização: o piso (`-f1-min`, `-estrutura-min`) vem desligado e ligá-lo
é decisão do Mauro.

**Quem roda é o Mauro**, no terminal dele, colando a chave num campo oculto; o comando exige
`-gastar`. Nenhum agente roda o `polishbench` com `-gastar`, e nenhum `go test` chama API real.
O resultado calibra o orçamento de palavras apagadas da guarda de palavras (hoje **zero**, e o
benchmark pode subir com dados), a taxa de fallback por motivo, o 3,5 s e as 4 palavras, e escolhe
o modelo **sugerido** na tela (nunca obrigatório).

## Etapa 5 — Nuvem e celular (ADR-019)
Decidido na ADR-019 (01-out-2026): o celular é um PWA, **Android primeiro**; um servidor Go com
Postgres; gravação única com o evento de sync na mesma transação (ADR-009); sync por usuário (fila
com um marcador por entidade, conflito com selo, lápides, `gone`, epoch e recomeço; sem `410` por
marca d'água: F9, D2); login
Google só para `CONTAS_PERMITIDAS`; pareamento por retorno a `127.0.0.1` com PKCE; sobem os últimos 6
meses sem a lixeira; texto legível no servidor, com a revisão de LGPD antes do deploy. Ficam para
depois: iPhone, preferências, ASR e contexto na nuvem, atualização pela nuvem e o dicionário completo
no banco.

Fatias, em ordem, cada uma com testes e um commit local. Nenhuma toca o servidor real. O
`go test ./...` normal não sobe contêiner: os testes com Postgres rodam só quando pedidos, contra um
contêiner que a pessoa sobe por script. Nenhum teste abre navegador, grava no Gerenciador de
Credenciais nem cria janela fora do portão `sysinput`.

| Fatia | O que entrega | Tasks | Estado |
|---|---|---|---|
| F0 | ADR-019, as emendas da spec e `scripts/diagnostico-servidor.sh` (só documentação e um script de leitura) | — | **aprovada** pelo Mauro em 01-out-2026, com as derivadas D1 a D7 confirmadas; falta a saída do diagnóstico |
| F1 | Postgres de verdade: 0001-0009 corrigidas no lugar, tabelas novas, comando `migrate`, `pgx`, script do contêiner de teste, `migrations/schema_test.go` atualizado junto | TASK-012, TASK-026 (testes de migration) | **implementada (01-out-2026) e verde num Postgres 18 de verdade (05-out-2026, rodada do Mauro com `AURORA_TEST_POSTGRES_URL`).** Revisada no mesmo dia; a PK do ditado por usuário e a regra de corrigir no lugar até o deploy foram confirmadas pelo Mauro em 05-out-2026. Ver ADR-019, "Decisões da F1" |
| F2 | ponto único de gravação; push e pull por usuário; `gone`; contador por usuário | TASK-014, TASK-015 | **implementada (05-out-2026) e verde num Postgres 18 de verdade** (`internal/server/cloudstore`, `internal/server/pgstore` e a suíte de contrato `internal/server/contracttest`, que roda contra o Store em memória e contra o Postgres). Só o ditado; o aprendido é da F8. Fecha o AC-016 no servidor. **Revisada no mesmo dia:** a mudança mesclada do próprio PC conta como conhecida por ele, o `duplicate` confere o aparelho, teto de 64 KiB por texto e 2 MiB de texto por página, busca com nulo dá `400`, isolamento da lápide testado; as propostas e as escolhas da F2 foram confirmadas pelo Mauro em 05-out-2026. Ver ADR-019, "Decisões da F2" |
| F3 | servidor de verdade no PC: login Google, lista permitida, sessões, CSRF, busca por `POST`, excluir definitivamente, apagar meus dados, rotas não montadas | TASK-012, TASK-013 (web), TASK-025 | **implementada (05-out-2026) e verde num Postgres 18 de verdade** (`internal/server/cloudauth`, o `Accounts` em `cloudstore` e `pgstore`, o `Cloud` em `internal/server` com a tabela de rotas, e o `serve` em `cmd/server`; a suíte `internal/server/authtest`, com o dublê do Google, roda contra a memória e contra o Postgres). Nenhuma dependência nova. As três decisões do plano usam a recomendação, confirmada pelo Mauro em 05-out-2026. Falta o teste manual com o client Google de verdade (`docs/teste-manual-nuvem-f3.md`). Revisada no mesmo dia (o `pre` na busca do login, o IPv6 por `/64`, o login de teste só no loopback, e o Host e o `/` de resto cobertos em toda rota). Ver ADR-019, "Decisões da F3" e "Revisão da F3" |
| F4 | painel em modo nuvem: build separado, cliente da nuvem, Entrar, sem conexão, sessão expirada, telas do celular, selo | TASK-016 | **implementada (05-out-2026).** Dois builds do mesmo código (`pnpm build` e `pnpm build:nuvem`), com o `web/bundle.test.ts` provando que o bundle local não tem login nem service worker; o Painel recebe o ambiente do modo (`web/src/ambiente.ts`); o app da nuvem em `web/src/nuvem/`; o selo com a escolha (`PATCH` com `resolver_conflito`) e o "excluir definitivamente"; o servidor serve o app (`PAINEL_DIR`); o servidor de teste no PC (`teste`, build tag `logindeteste`) e o `scripts/nuvem-local-teste.ps1`. As três escolhas do plano usam a recomendação, a confirmar pelo Mauro. Roteiro: `docs/teste-manual-nuvem-f4.md`. Revista no mesmo dia; o rascunho que sumia com o `401` foi decidido pelo Mauro ("Guarda até entrar de novo") e feito: com um texto não salvo na tela, o Painel fica, e a pessoa entra de novo numa janela à parte. Ver ADR-019, "Decisões da F4" |
| F5 | app instalável: manifest, ícones, service worker; `404` em `/sw.js` no Agent | TASK-016 | **implementada (05-out-2026).** O manifest e o `sw.js` em `web/pwa/`, postos na raiz do build só no modo nuvem por um plugin do `vite.config.ts` (a lista da casca e a versão no lugar dos marcadores); os ícones (192 e 512 "any", 512 maskable) saem do `go run ./cmd/gerarico`; o registro em `web/src/nuvem/registrarSW.ts`; o Agent responde `404` em `/sw.js`. Nenhuma dependência nova e nenhuma mudança no servidor da nuvem (só testes). O `bundle.test.ts` roda o `sw.js` emitido num `node:vm`. As três escolhas (o `short_name` "Whisper", o ícone do .exe e o `#070a09`) são decisões do Mauro (05-out-2026). Conferida num Chromium de verdade (o Edge em modo headless, com um perfil de rascunho): registra, assume a página e, sem o servidor, abre em "Sem conexão.". Falta o teste manual no PC e no Android (`docs/teste-manual-nuvem-f5.md`). Ver ADR-019, "Decisões da F5" |
| F6 | conectar o PC: pareamento, Gerenciador de Credenciais, revogar, desconectar; o desinstalador apaga o token na opção "apagar dados" | TASK-013 (device) | **implementada (05-out-2026), autorizada pelo Mauro no mesmo dia.** No servidor: o start, a leitura e a aprovação do pedido (por `fetch`, com o retorno em JSON), a troca com o desafio na busca, os aparelhos (lista e revogar) e os 180 dias, na memória e no Postgres (os testes de Postgres não rodaram: o contêiner estava desligado). No PC: `internal/agent/nuvem` (sem build tag; a política da URL, o PKCE, o retorno com o `state` de uso único, a D3, o Conferir e o Desconectar), a Local API `/api/nuvem*`, o `/nuvem/retorno` no servidor do painel, e a montagem em `cmd/agent` atrás dos dois portões, com o teste que prova que nada abre e nada é gravado sob `go test`. No web: a seção Nuvem do PC, a página de aprovação e os Aparelhos no app da nuvem. O desinstalador apaga `AuroraWhisper/nuvem` na opção "apagar dados". Nenhuma dependência nova; nenhuma migration. As três decisões do plano usam a recomendação, a confirmar pelo Mauro. Conferida num Chromium (o navegador do app) de ponta a ponta. Roteiro: `docs/teste-manual-nuvem-f6.md`. Revista em 06-out-2026: a conta de teste com o id fixo (a nuvem de teste reiniciada não prende mais o `bin\`), a conta que aprova na página de aprovação, o teste da segunda guarda da D3 e o comentário do token no Gerenciador; os testes de Postgres continuam pendentes (o Docker Desktop estava fechado), para antes do merge ou da F7. Ver ADR-019, "Decisões da F6" e "A revisão da F6" |
| F7a | fila por entidade e aplicador no PC, sem rede | TASK-015 | **implementada (06-out-2026), autorizada pelo Mauro no mesmo dia.** A migration 0005 do SQLite (aditiva: a fila velha renomeada, intocada; a fila nova com um marcador por ditado e os campos que mudaram, sem texto; o cursor, o epoch e o pareamento em `sync_state`; o selo em `local_transcriptions`), provada contra um banco na 0004 com dados. A emissão do marcador em toda gravação do histórico, na mesma transação (conectado; desconectado, só num ditado que a nuvem já tem, revisão de 06-out-2026). As decisões puras do push e do pull (`internal/sync/decisao.go`) e a fila que as executa (`internal/storage/fila_sqlite.go`). O `PATCH` local com o teto de 64 KiB. Sai o motor antigo de `internal/sync`. Ver ADR-019, "Decisões da F7a/b" |
| F7b | cliente HTTPS e laço de sync (`401`, `410`, backoff); atualiza o `SyncClient` no `18-tool-contract.md` | TASK-015 | **implementada (06-out-2026).** O motor da rodada, o cliente HTTPS (Bearer, 30 s, sem redirecionamento, 1 MiB e 16 MiB, erros tipados) e o laço em segundo plano (na subida, depois de cada gravação, a cada 60 s e ao conectar; espera de 2 s a 5 min com jitter; `401` desconecta, `429` espera), montados no `cmd/agent`; a linha da nuvem na bandeja. **Revista em 06-out-2026:** a reconexão continua do cursor e o `410` só relê do 0, no mesmo epoch, sem reenviar nada (reenviar com a base vazia criava selos sem conflito e trocava o selo de verdade); só o epoch novo recomeça, sem a lixeira; a resposta até 16 MiB; a bandeja diz "aguardando" e "resposta inesperada"; a rodada ociosa não escreve no banco. A ponta a ponta com dois PCs e o celular contra a nuvem de verdade (`internal/server/integration`). As três decisões do plano usam a recomendação, confirmadas pelo Mauro em 06-out-2026 (com a mudança na regra da fila: desconectado, entra a mudança num ditado que a nuvem já tem); o Mauro testa depois da F7c. Ver ADR-019, "Decisões da F7a/b" |
| F7c | envio inicial (prévia, cópia, 6 meses sem a lixeira), o selo de conflito no PC e o "Esquecer esta nuvem" | TASK-015 | **implementada (06-out-2026), autorizada pelo Mauro no mesmo dia.** A migration 0006 do SQLite (aditiva: `inicial`, `selo_escolha` e `selo_em` na fila e, desde a reconferência, a tabela `sync_mudou_aqui`, vazia), provada contra um banco na 0005 com dados. O envio inicial com um clique (a cópia `VACUUM INTO` pela conexão única, conferida; os marcadores em blocos de 500; o aceite; a completação a cada rodada; o progresso no painel e na bandeja); a reconciliação do pull (o que mudou aqui sem a versão da nuvem sobe, e a nuvem decide); a escolha do selo no PC, que sobe pelo push (a regra nova em `cloudstore.DecidePush`, na memória e no Postgres); o "Esquecer esta nuvem". A prova com o `historico.db` do `bin\` copiado de um fixture e a ponta a ponta contra a nuvem de verdade. As três decisões do plano (D1, D2, D3) confirmadas pelo Mauro em 06-out-2026. **Revista no mesmo dia:** o esquecer não deixa o pull desfazer a mudança que não subiu (a referência recuava para antes dela; **reconferida** no mesmo dia: a referência é o instante do esquecer, e o que mudou aqui sem a nuvem receber fica anotado item por item, na `sync_mudou_aqui` da 0006, sem selo à toa no ditado recebido da nuvem; a pergunta do esquecer conta só as mudanças); conectado, o histórico só sobe pelo clique e pela completação (a edição, a lixeira e o restaurar não o põem na fila, e a lixeira tira o marcador de criar parado); a reconciliação não reconcilia o ditado que só a nuvem pôs na lixeira; a linha da nuvem diz quantos não subiram. Teste manual: `docs/teste-manual-nuvem-f7.md`. Ver ADR-019, "Decisões da F7c", "A revisão da F7c" e "A reconferência da F7c" |
| F8 | aprendido sincronizado (par curto, restrição, domínio; "não aplicado neste PC") | TASK-019 (a parte de sync) | **decidida (07-out-2026); implementada (F8a a F8i, 07 e 08-out-2026); falta o Mauro rodar o roteiro.** As quatro decisões do Mauro e o desenho em ADR-019, "Decisões da F8"; a F8.0 (os documentos) e a F8a a F8f4 feitas (a F8c e a F8d verdes no Postgres de teste em 07-out-2026; a F8e é o espelho do SQLite, a 0007; a F8f3 é a varredura do arquivo no começo da rodada, com o M e o envio do aprendido; a F8f4 é o `Receber` com o aplicador, o par do pull passando pelo arquivo; a F8g liga tudo no Agent: o aplicador em `internal/agent/aprendiz`, o `montarSync` com ele, o gancho do painel, a situação e o motivo no `GET /api/aprendizado` e o M no `historico`); a F8h (a web: o Aprendido no celular, as linhas de estado no PC, a pergunta com o M e o aviso do freio) também está feita; e a F8i, o roteiro manual (`docs/teste-manual-nuvem-f8.md`) e os documentos, também (08-out-2026) |
| F9 | retenção na nuvem e lápides | TASK-020 (nuvem) | **decidida (08-out-2026); implementada (F9.0 a F9e) e revista no mesmo dia; falta a rodada no Postgres de teste, antes do merge.** As quatro decisões do Mauro (D1 a D4, todas com a recomendação) e o desenho, com as correções da crítica, estão em ADR-019, "Decisões da F9": o `Reter` por usuário no `serve`, na subida e de hora em hora; o ativo vencido sai sem lápide; a lápide vive a janela mais 24 h, decidida linha a linha; a arrumação do evento órfão (90 dias) e da resposta do push (190 dias); o aprendido desfeito fica enquanto a conta existir. **Sem o `410` da marca d'água** (D2) e sem a cota por conta (D3, F11). A F9.0 a F9e estão feitas; a revisão está em ADR-019, "A revisão da F9". Depois do fechamento (08-out-2026), o Mauro confirmou as escolhas menores 1 a 5 e decidiu o piso do vencimento da lixeira que chega pelo push ("Fica 24 h na lixeira": ADR-019, D5 em "Decisões da F9"), já feito |
| F10 | os 6 meses no PC, com prévia e OK expresso; e a correção da D4 da F9: o PC só apaga de vez da lixeira o que a nuvem já soube | TASK-020 (PC) | **decidida (08-out-2026); implementada (F10.0 a F10f) e revista no mesmo dia; falta o Mauro rodar o roteiro.** As quatro decisões do Mauro (todas com a recomendação) e o desenho estão em "Decisões da F10", na ADR-019, e a revisão em "A revisão da F10": a F10.0 (os documentos), a F10a (a guarda da D3 na lixeira), a F10b (os 6 meses no storage, atrás do portão), a F10c (a cópia, o Ligar, o Desligar, o freio e o passo no ciclo do Agent), a F10d (as rotas locais e o bloco Histórico nas Configurações), a F10e (o botão Apagar as cópias) e a F10f (os documentos e o roteiro `docs/teste-manual-nuvem-f10.md`). A regra só liga pelo clique do Mauro no bloco Histórico do `bin` dele, pelo roteiro: nenhum agente a liga. Vem sobre a F9, que espera a rodada no Postgres de teste antes do merge |
| F11 | deploy (**ponto de parada**): compose, Dockerfile, backup e roteiro, testados no Docker do PC com `exemplo.test`; o Mauro roda no servidor depois da revisão de LGPD | TASK-024 | **as duas partes escritas (09-out-2026); falta o Mauro rodar.** A Parte 1 (o servidor pronto para contêiner, o pós-restauração, as imagens, o Postgres sem superusuário, os scripts da VPS e do PC, a varredura de segredos e o dossiê de LGPD) está na `main`. A Parte 2 segue a D1, confirmada pelo Mauro em 09-out-2026 (uma VPS, atrás do Traefik que já roda, no molde dos outros serviços do servidor; `https://aurora-whisper.mjj.net.br`; o datacenter no Brasil): o `deploy/compose.yml` (só os rótulos do próprio contêiner, com o log de acesso desligado no nosso router), o agendador do backup, o `subir.sh`, o `gerar-segredos.sh` com o JSON do Google, os scripts do PC (`deploy-teste.ps1`, `enviar-para-servidor.ps1`, `conferir-nuvem.ps1`) e os roteiros (`docs/deploy-f11.md`, `docs/backup-b2-f11.md`, `docs/teste-final-f11.md`). Provado por testes estáticos e pelo bash com programas de mentira; o primeiro build, o primeiro deploy e o teste final são do Mauro. Ver ADR-019, "Decisões da F11" e "A Parte 2 da F11" |

O que cada fatia prova e o que o Mauro testa em cada uma está na ADR-019, "Fatias". O que ele
providencia, e quando, também.

## TASK-012 — PostgreSQL + API skeleton
- entities;
- migrations;
- health;
- middleware;
- request IDs;
- tenant isolation tests.

Etapa 5 (ADR-019): F1 e F3. As migrations rodam pelo comando `migrate`
(`aurora-whisper-server migrate`), à parte da subida. A F1 (migrations, `migrate`, `pgx` e os testes
com Postgres) está implementada, e falta rodá-la contra um Postgres (`scripts/postgres-de-teste.ps1`).

## TASK-013 — Google OAuth + device auth
- web OAuth;
- challenge/exchange;
- Credential Manager;
- revoke.

Etapa 5 (ADR-019): o login web é a F3, com `openid email profile` e a lista permitida; o device auth
é a F6, por retorno a `127.0.0.1` com PKCE, sem `poll_token`. O login pelo código de 6 dígitos (iPhone)
fica para depois do MVP. **A F3 está implementada (05-out-2026):** o login Google com state no servidor,
`__Host-pre`, PKCE e nonce, a sessão `__Host-sessao` revogável e o escopo do token do PC, provados por
um dublê do Google contra a memória e contra o Postgres; o login com o Google de verdade é o teste
manual (`docs/teste-manual-nuvem-f3.md`). Na revisão da F3 (mesmo dia): o `__Host-pre` entrou na
busca do login pendente (o `state` sozinho não queima o login de ninguém), o limite por IP conta o
IPv6 por `/64`, e o login de teste (build tag) só escuta e só atende no loopback (ADR-019, "Revisão
da F3").

## TASK-014 — Cloud CRUD
- transcriptions;
- dictionary;
- corrections;
- settings;
- devices;
- trash/restore.

Etapa 5 (ADR-019): F2 e F3, com transcriptions (sem `target_app`, busca por `POST`, excluir
definitivamente), devices, sessões e o aprendido. **Dictionary, corrections e settings: depois**
(rotas não montadas). **F3 (05-out-2026):** a busca por `POST /transcriptions/search` (o `q` na URL é
`400`), o `POST /transcriptions/{id}/purge`, as sessões e o `DELETE /me/data` estão montados; as rotas
não montadas, inclusive o `POST /transcriptions`, dão `404`. `/devices` é da F6; o aprendido, da F8.

## TASK-015 — Sync
- outbox;
- idempotency;
- cursor;
- optimistic concurrency;
- backoff;
- conflicts.

Testar offline/reconnect/duplicate/concurrent edit-delete.

Etapa 5 (ADR-019): F2 (servidor) e F7a a F7c (PC), com o contrato por usuário da ADR-019. O lado do
servidor está na F2 (05-out-2026): push, pull, `gone`, mescla, selo e o contador por usuário, provados
contra um Postgres 18. O lado do PC está na F7a e na F7b (06-out-2026): a fila por ditado no SQLite, o
aplicador, o cliente HTTPS e o laço, provados de ponta a ponta contra a nuvem de verdade, com o PC
offline, a resposta perdida, o reinício, o `401`, o `410`, o epoch novo, o `429` e a volta do PC que
ficou fora (revisão de 06-out-2026: a volta no mesmo epoch não reenvia nada). A F7c (06-out-2026)
fecha o PC: o envio inicial com a cópia antes, a reconciliação do pull, o selo escolhido no painel do
PC e o "Esquecer esta nuvem", provados com o `historico.db` do `bin\` copiado de um fixture e de ponta
a ponta contra a nuvem de verdade. A revisão da F7c (06-out-2026) corrigiu três caminhos: a mudança
feita desconectado que o pull desfazia na volta à mesma conta depois do esquecer; o histórico que
subia sem o clique (e a lixeira junto) quando era mexido com o PC conectado; e a reconciliação que não
convergia quando só a lixeira da nuvem diferia. A reconferência do mesmo dia trocou o recuo da
referência do esquecer pela anotação item por item (a `sync_mudou_aqui`: um segundo esquecer não
prende mais a referência numa mudança que nunca sobe, e o ditado recebido da nuvem não vira selo à
toa), fez a pergunta do esquecer contar só as mudanças e registrou dois riscos aceitos (a lixeira de
um ditado com o lote em voo, e o ditado de outro aparelho editado desconectado antes do esquecer).
O Mauro confirmou as três decisões da F7c (D1, D2 e D3) em 06-out-2026.

## TASK-016 — Cloud dashboard
Reusar React com runtime mode.

Etapa 5 (ADR-019): F4 e F5. O modo é escolhido na compilação (dois builds do mesmo código), não em
tempo de execução: o build local nunca registra service worker. **F4 (05-out-2026):** `vite build
--mode nuvem` troca a entrada do `index.html` por `src/nuvem/main.tsx`; o Painel recebe o ambiente do
modo; o ESLint proíbe importar `src/nuvem` fora dela (e o que é só do PC dentro dela, em qualquer
profundidade); o `web/bundle.test.ts` constrói os dois bundles e confere. **F5 (05-out-2026):** o
manifest, os ícones e o service worker só no build da nuvem (o plugin `aurora-app-instalavel` do
`vite.config.ts`, com as fontes em `web/pwa/`); o registro em `src/nuvem/registrarSW.ts`; o
`bundle.test.ts` confere que o local não tem nada disso e roda o `sw.js` emitido num `node:vm` (o que
ele guarda, o que ele responde e o que nunca toca).

## TASK-017 — Cloud ASR gateway
**Depois (ADR-019):** fora da etapa 5. O áudio não sai do PC, e a rota não é montada (um teste
prova). O handler em memória (`internal/server`) fica como está.
- stream/multipart;
- limits;
- no disk;
- server-side provider;
- timeout;
- body not logged.

## TASK-018 — Policies/fallback
**Depois (ADR-019):** sem ASR na nuvem, só `local_only` existe.
- local_only;
- local_then_cloud;
- cloud_only.

## TASK-019 — Learning
Decidido na ADR-017 (30-set-2026): aprender só com um clique de quem usa, guardado no
`dicionario.json` (e não no SQLite, que `04`, `05` e a ADR-008 previam; conflito resolvido pela
ADR-017), com a origem "painel", válido em qualquer aplicativo.
- history edit;
- diff (o texto de antes da edição contra o salvo, alinhado com o bruto);
- correction suggestion (troca curta de termo; até 2 por edição);
- explicit confirmation (o botão **Aprender**; nunca sozinho no PC de origem; desde a F8, os outros
  PCs recebem o par pelo sync, com as mesmas checagens: ADR-019, "Decisões da F8", D2);
- **sync: fora desta etapa.** A Correction que sincroniza (TEST-008, AC-021), a decisão de qual usuário
  é o dono e a ida do dicionário para o banco ficam para a etapa 5 (S7). **Emenda (ADR-019):** na
  etapa 5 sincroniza só o par aprendido (F8); o dono local fica e a instalação se liga a uma conta; o
  dicionário completo no banco fica para depois.

Fatias, em ordem, cada uma com testes e um commit local (nenhum teste digita, clica ou mexe na
área de transferência; os de gravação usam pasta temporária):
- F0: ADR-017 e as emendas da spec (só documentação);
- F1: motor de sugestão, sem efeito colateral (`internal/learning`);
- F2: gravar no `dicionario.json`, desfazer e trocar o dicionário em memória na hora;
  `cmd/asrstage` passa a preservar o arquivo existente;
- F3: endereços do painel (`/api/aprendizado`, e o campo `sugestoes` na resposta de salvar);
  **pronta** (`internal/agent/localapi/aprendizado.go`; decisões em `ADR-017`, "Decisões da F3");
- F4: aviso e lista "Aprendido no painel" no painel. **Pronta** (`web/src`; decisões em `ADR-017`,
  "Decisões da F4"). O que falta é o roteiro manual do Mauro (ADR-017, "Testes previstos").

**Adicionar termo (ADR-020, 01-out-2026).** Somado ao Aprender: em Configurações, a pessoa põe no
dicionário a palavra certa, as grafias erradas que o reconhecimento de voz escreve, e a dica para ele
(o prompt do ASR, trocado em memória, sem reiniciar). Fatias, cada uma com testes e um commit local
(os de gravação em pasta temporária; nenhum teste digita, clica ou mexe na área de transferência):
- T0: ADR-020 e as emendas (`03`, `05`, `06`, `08`, este, e a ADR-017);
- T1: `internal/dictionary/termos.go` e `arquivo_termos.go` (adicionar, dica, remover, listar),
  `DisputaOPrompt` e a ordem do prompt (as dicas dela primeiro);
- T2: `winruntime` (o prompt troca com o dicionário; a corrida de `transcrever`) e `cmd/agent`
  (`gravarETrocar` remonta o prompt; `conferirTermos`: "age?" por grafia e "quebraria?");
- T3: `internal/agent/localapi/termos.go` (`/api/termos`) e `learning.Arriscada`;
- T4: `web/src/Termos.tsx` (a seção Dicionário).
**Prontas** as cinco. O que falta: o detector de corrida (`-race`) onde houver cgo, as três decisões
do Mauro (implementadas com a recomendação; ADR-020) e o roteiro manual dele (ADR-020, "Riscos").

## TASK-020 — Retention jobs
- active 6m purge;
- trash 30d purge;
- correction FK set null;
- idempotency.

Estado (01-out-2026): o expurgo da lixeira de 30 dias já roda no Agent (`internal/agent/expurgo`); os
6 meses do ativo não existem no Agent; `internal/retention` só roda com dublês. Etapa 5 (ADR-019): a
nuvem é a F9 (pela data do ditado, com lápides); o PC é a F10 (com prévia da contagem e OK expresso
do Mauro, porque a primeira vez apaga ditados reais; e, pela D4 da F9, o expurgo da lixeira do PC só
apaga de vez o que a nuvem já soube). O "correction FK set null" não tem objeto na nuvem (a 0006 já
faz `ON DELETE SET NULL`, e a tabela não é usada).

Estado (08-out-2026, F10f): **no PC, feita** (ADR-019, "Decisões da F10"). O ciclo do expurgo, na
subida e de hora em hora, roda a lixeira com a guarda da D3 (o que a nuvem pode ter e ainda não
soube fica, no máximo até os 6 meses do ditado) e o passo dos 6 meses, que só apaga com o OK gravado
(`internal/storage/seis_meses_sqlite.go`, `seis_meses_ligar.go`; o portão lê o OK dentro da
transação). "active 6m purge": linha a linha, em Go e em UTC, em lotes de 500 por cursor
`(created_at, id)`; "trash 30d purge": com a guarda; "correction FK set null": a 0001, provado no
storage; "idempotency": a segunda rodada apaga zero. O `retention.Job` não é usado no PC (desvio 1
da F10); a política e o corte (`retention.CorteDoAtivo`) são os mesmos da nuvem. A primeira purga
real é o clique do Mauro, pelo `docs/teste-manual-nuvem-f10.md`.

## TASK-021 — Tray + autostart
- tray;
- status;
- open dashboard;
- pause;
- HKCU autostart toggle.

## TASK-022 — Installer
Preferir ferramenta simples/reproduzível (ex.: Inno Setup ou equivalente).
- install/uninstall;
- shortcuts;
- preserve/remove user data por escolha.

Decidido na ADR-018 (30-set-2026): **Inno Setup 6.7, instalação por usuário**
(`PrivilegesRequired=lowest`); programa em `%LOCALAPPDATA%\Programs\Aurora Whisper`, dados em
`%LOCALAPPDATA%\AuroraWhisper` (o pacote `internal/agent/pastas` é quem decide); o **modelo de voz
vai dentro do Setup**; o instalador **não liga** o "Iniciar com o Windows"; o desinstalador pergunta
se apaga os dados (padrão: não); o runtime do Visual C++ é baixado da Microsoft só se faltar; 0.x
**sem assinatura**; release **rascunho** no GitHub Actions por tag (publicar é sempre com o Mauro).
O `bin\` de desenvolvimento continua igual pelo **modo portátil** (arquivo marcador `portatil`, que
o `cmd/asrstage` cria). Nada foi distribuído: a revisão de LGPD foi aprovada em 10-out-2026 e vale
para o Setup (`PRODUCT.md`); falta o portão do workflow de release e o release do modelo.

Fatias, em ordem, cada uma com testes e um commit local (nenhum teste roda instalador, grava no
registro ou mexe em pasta real):
- F0: ADR-018 e as emendas (só documentação);
- F1: pacote `pastas`, o Agent passa a separar programa e dados, marcador no `asrstage`, log sem
  caminho completo (`ADR-018`, "Decisões da F1"). **Pronta** (`internal/agent/pastas`,
  `cmd/agent/caminhos.go`); o roteiro manual do Mauro foi feito em parte: o passo 2 (marcador criado)
  e, antes dele, a regra do legado vista na subida de 00:27:41 de 01-out-2026; o passo 5 como escrito
  e os passos 3, 4 e 6 não foram feitos nem relatados (`ADR-018`, "Testes previstos");
- F2: `whisper-server` com `cmd.Dir` na pasta do programa e modelo pelo nome relativo, **e a saída
  do servidor com os caminhos trocados por rótulos** (**pré-requisito da F5**: sem isso o
  `whisper-server.log` instalado traz o nome do usuário em duas linhas, a do modelo e a da biblioteca
  do processador; a segunda é do servidor e `cmd.Dir` não a muda). **Pronta** (`local.Config.Dir`,
  `configDoServidor` e `abrirLogDoServidor` em `cmd/agent`, `pastas.AnonimizarLinhas`; `ADR-018`,
  "Decisões da F2"); só falta conferir com o servidor de verdade, no log **inteiro** (roteiro
  manual, I5);
- F3: build reproduzível (`version` como `var`, `-trimpath`, `--outDir` do painel,
  `asrstage -destino -sem-dicionario`, ícone, `licencas\`). O Mauro decidiu o aprender no Agent
  instalado em 30-set-2026: o Agent **cria um `dicionario.json` vazio** na subida, só se faltar
  (ADR-018, "O dicionário que não existe"; emenda na ADR-017, decisão 6). **Feita**, com o
  ícone (`cmd/gerarico`: o `.ico` do Setup e o recurso do executável) e o script
  `scripts/montar-instalador.ps1`, que ainda não rodou inteiro;
- F4: `installer/aurora-whisper.iss` (o caminho muda do `deploy/windows/` do plano). **Escrita e
  compilada**: o Mauro compilou o Setup `0.1.0-rc.1` com o Inno Setup 6.7.3 nesta máquina, sem erro
  (135 MB), e o ISCC 6.7.3 também aceitou o `.ico` com a camada de 256 px em PNG (prova com um script
  mínimo, em 01-out-2026). **O Setup nunca foi executado**;
- F5: CI de release (`.github/workflows/release.yml`: tag, Setup, release rascunho) e teste de
  instalação no runner descartável. O workflow está **escrito e não rodou**, e tem um portão
  fechado (`F2_PRONTA: 'false'`) que o faz falhar de propósito até o Mauro o abrir (a F2 já está
  pronta; abrir é decisão dele, depois de conferir o servidor de verdade); o teste de instalação
  **não foi feito**;
- F6: `docs/teste-manual-instalador.md` e a seção Instalação do README. A seção "Instalar" do README
  está feita; o roteiro manual está **escrito** (para uma conta de teste do Windows) e **ninguém o
  executou**.

Fora desta leva: atualização por clique e winget. O AC-038 sai de DIFERIDO quando F3 a F6 estiverem
prontas e o roteiro manual for feito (`fase-1-cobertura.md`).

## TASK-023 — Update service
- `/updates/windows/latest`;
- notify;
- user confirms;
- verify hash/signature;
- no auto-install.

Emenda (ADR-018, 30-set-2026): nas versões 0.x a atualização é **manual** (a pessoa baixa o Setup
novo e instala por cima). Um item "Procurar atualização" na bandeja, que só consulta quando a pessoa
clica, vem numa fatia seguinte, **lendo o GitHub Releases** (um `UpdateProvider` novo) enquanto o
backend do `/updates/windows/latest` não existir; a pessoa confirma, o Agent baixa, confere o hash
e roda o Setup em silêncio. Sem assinatura nas 0.x, o THR-010 é coberto só por hash. Conflito com o
texto acima registrado na ADR-018; a fatia confere com o Mauro antes de codar.

**Depois (ADR-019):** a nuvem da etapa 5 não monta `/updates/windows/latest`.

## TASK-024 — Docker/Compose
- API;
- Postgres;
- optional inference profile;
- healthchecks;
- resource limits;
- isolated networks.

Sem deploy real.

Etapa 5 (ADR-019): F11. Sem perfil de inferência. Testado no Docker do PC com `exemplo.test`; o deploy
real é ponto de parada e quem roda é o Mauro. Feito (09-out-2026): os healthchecks (a API pelo
`healthcheck -retencao`, o Postgres pelo `pg_isready` no TCP, o backup pelo `agendar-backup.sh saude`),
os limites (a API 256 MB e 1 CPU, o Postgres 512 MB e 1 CPU, o backup 128 MB e meia CPU) e as redes
isoladas (o Postgres só na interna, `internal: true`; a API na interna e na do Traefik; o backup na
interna e numa de saída só dele), em `deploy/compose.yml`; o roteiro é o `docs/deploy-f11.md`.

## TASK-025 — Hardening/observability
- redacted logs;
- metrics;
- rate limit;
- CSRF;
- local token/Host/Origin;
- CSP;
- secret scan.

Etapa 5 (ADR-019): o CSRF (`http.CrossOriginProtection`), o limite de tentativas, a CSP e os logs
sem texto da nuvem entram na F3.

## TASK-026 — GitHub Actions
- Go tests/vet/build;
- frontend lint/test/build;
- secret scan;
- migration tests;
- Windows build runner.

## TASK-027 — Release candidate
- todos ACs;
- benchmark final;
- clean install;
- offline/sync;
- retention simulated time;
- security review;
- docs.

## Paralelização
- TASK-002/003/004 podem evoluir em paralelo após contratos.
- TASK-006 pode rodar com TASK-005.
- TASK-012/013/014 independem do ASR local.
- TASK-016 pode usar mocks.
- TASK-024 após server skeleton.
- TASK-026 desde o início.

## Stop conditions
Claude Code deve parar se:
- benchmark não cumprir gates;
- dependência nativa exigir privilégio/admin inesperado;
- precisar screenshot;
- precisar persistir áudio;
- migration destrutiva;
- deploy/infra real;
- credenciais reais necessárias;
- conflito de especificação.

## DoD final
Ver `03-acceptance-criteria.md` e `14-review-checklist.md`.
