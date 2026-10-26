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

**Ditar no celular (ADR-021, 10-out-2026).** Depois da F11, o Mauro pediu o ditado no celular: o app
grava, o servidor repassa a voz ao provedor que a pessoa escolheu (OpenAI ou Groq), com a chave dela
guardada cifrada, e o texto desce aos PCs pelo sync. É a TASK-028, com as subfatias S, LG, D0 e C1 a C8.

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

**Nota de 11-out-2026 (ADR-021, C5a):** o adaptador da S4 (`clienteDoDitado`, a política de esforço
e o `prazoPolish`) saiu de `cmd/agent` e de `winruntime` para o pacote comum `internal/polish`
(`polish.Prazo`, `polish.PoliticaEsforco`, `polish.Montagem{...}.Abrir()`), que o servidor usa igual
no ditado do celular. O comportamento do Agent não mudou: os testes do Polish do PC passam sem
mudança. Ver a TASK-028.

## TASK-011b — Benchmark do Polish
**Diagnóstico com o histórico (16-out-2026).** No log do Agent, a guarda de palavras não recusou
nenhum ditado com menos de 600 caracteres (0 de 24) e recusou 4 de 11 de 600 a 999 e os 3 de 1.000
ou mais; o ditado de 44 s do celular (12-out) caiu nela. O log só guarda o motivo (AC-048), então
não diz o que o modelo mudou. O `polishbench -historico local` lê os ditados longos do modo `smart`
do histórico do PC (só leitura) e mostra no terminal, palavra por palavra e com a categoria, o que o
modelo mudou nas respostas recusadas; não grava relatório nem arquivo. Quem roda é o Mauro, com a
chave dele. O conserto (aceitar por frase, licença específica, outro modelo ou prompt) se decide
com o resultado.

**Correção de grafia (18-out-2026).** O diagnóstico (26 ditados, 3 rodadas) mostrou que a maior
parte das recusas era o modelo corrigindo o erro do Whisper ("vejante" -> "vigente"). O Mauro
decidiu: a guarda de palavras aceita a correção de grafia só da palavra que não existe em português
(dicionário VERO, `internal/ortografia`); ver a emenda de 18-out-2026 da ADR-016. Falta: rodar o
`polishbench -historico` de novo para medir o ganho. A auditoria do benchmark aprendeu a regra em
24-out-2026 (`cmd/polishbench/grafia.go`), com a categoria `grafia` no dataset (9 frases com erro
de transcrição e o `esperado` corrigido, à espera da revisão do Mauro).

**Aceite por frase e maiúscula do Whisper (19-out-2026).** Com a correção de grafia, o ganho foi
pequeno (16 → 19-20 de 78): a resposta recusada tinha 1 a 3 pontos ruins, e a guarda de nomes
recusava palavra comum que o Whisper pôs em maiúscula. O Mauro aprovou o aceite por frase e a
minúscula da palavra comum (com a lista de nomes de pessoa); ver a emenda de 19-out-2026 da
ADR-016. Falta: medir com o `polishbench -historico`.

**Aceite por linha (24-out-2026).** O aceite por frase chegou às listas e às respostas com quebra de
linha, a pedido do Mauro: a linha é a unidade, e a que não passa volta ao Smart sem desmanchar a
lista; ver a emenda de 24-out-2026 da ADR-016.

Decidido na ADR-016. `cmd/polishbench` (pronto, S3), no molde de `cmd/entitybench`, usando o mesmo
`Polir` e as mesmas guardas do Agent. Dataset novo em `testdata/polish-dataset/frases.json`
(pronto, 97 frases sintéticas), mais as 46 `reference` do dataset de ASR. Métricas, regra de
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
| F11 | deploy (**ponto de parada**): compose, Dockerfile, backup e roteiro, testados no Docker do PC com `exemplo.test`; o Mauro roda no servidor depois da revisão de LGPD | TASK-024 | **as duas partes escritas (09-out-2026); falta o Mauro rodar.** A Parte 1 (o servidor pronto para contêiner, o pós-restauração, as imagens, o Postgres sem superusuário, os scripts da VPS e do PC, a varredura de segredos e o dossiê de LGPD) está na `main`. A Parte 2 segue a D1, confirmada pelo Mauro em 09-out-2026 (uma VPS, atrás do Traefik que já roda, no molde dos outros serviços do servidor; um domínio próprio; o datacenter no Brasil): o `deploy/compose.yml` (só os rótulos do próprio contêiner, com o log de acesso desligado no nosso router), o agendador do backup, o `subir.sh`, o `gerar-segredos.sh` com o JSON do Google, os scripts do PC (`deploy-teste.ps1`, `enviar-para-servidor.ps1`, `conferir-nuvem.ps1`) e os roteiros (`docs/deploy-f11.md`, `docs/backup-b2-f11.md`, `docs/teste-final-f11.md`). Provado por testes estáticos e pelo bash com programas de mentira; o primeiro build, o primeiro deploy e o teste final são do Mauro. Ver ADR-019, "Decisões da F11" e "A Parte 2 da F11" |

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

**Emenda (ADR-021, 11-out-2026):** a `/asr/transcribe` continua não montada, e os testes de `404`
continuam valendo para ela. O ditado do celular tem rota própria, `POST /api/v1/dictations`, que só
repassa a voz ao provedor da pessoa (OpenAI ou Groq, `internal/asr/nuvem`) e não grava o áudio: é a
TASK-028. O `asr_gateway.go` antigo saiu na C5c (`a8cf426`), junto do servidor em memória da F1-D.
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
  mínimo, em 01-out-2026). **Executado**: o Mauro instalou a 0.1.0 numa conta nova do Windows e, em
  16-out-2026, a 0.1.1 numa máquina que nunca teve o Aurora Whisper, e ditou nas duas;
- F5: CI de release (`.github/workflows/release.yml`: tag, Setup, release rascunho) e teste de
  instalação no runner descartável. O workflow **rodou**: o portão `F2_PRONTA` foi aberto em
  12-out-2026, e ele montou os rascunhos da 0.1.0 e da 0.1.1. A **0.1.1 foi publicada** como prévia
  em 16-out-2026, com o OK do Mauro (ADR-018, "Nota de 16-out-2026"), e a **0.1.2** em 24-out-2026
  (ADR-018, "Nota de 24-out-2026"). O teste de instalação no runner
  **não foi feito**;
- F6: `docs/teste-manual-instalador.md` e a seção Instalação do README. A seção "Instalar" do README
  está feita; o roteiro manual está **escrito** (para uma conta de teste do Windows); dele, só os
  testes curtos da F4 (instalar e ditar) foram feitos, e o roteiro inteiro **não foi relatado**.

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

## TASK-028 — Ditar no celular (ADR-021)
Decidido na ADR-021 (10-out-2026; o Mauro aprovou a ADR com as 27 escolhas menores no padrão). O app do
celular (o PWA) ganha um microfone: toque para começar e toque para parar. O servidor recebe a voz em
webm/opus numa rota nova (`POST /api/v1/dictations`), repassa ao provedor que a pessoa escolheu (OpenAI
ou Groq) com a chave dela (BYOK, guardada cifrada no servidor), passa o texto pelo Smart da conta e, se
ela ligou, pelo Polish, grava pelo ponto único (ADR-009) e responde. O texto desce aos PCs pelo sync. O
áudio fica só em memória e nunca vai a disco, a swap nem a log. **Nenhum áudio real sai do aparelho
antes do OK do Mauro na política e no dossiê reescritos (D0).** Requisitos: REQ-051 a REQ-054; critérios:
AC-065 a AC-079 (`03`, "Ditar no celular").

Cada subfatia roda `go test ./...` e `go vet ./...` (e o lint, o teste e o build do `web/` quando mexe
nele) e termina com um commit local. Nenhum teste chama provedor real (só `httptest`) nem abre o
microfone ou a área de transferência (dublês do navegador).

**Estado em 11-out-2026:** as fases 1 e 2 estão na `main`. A fase 1 entrou pelos merges `919338a`,
`fc3cec7`, `ce08af4`, `c044fcf` e `a4405ec`; a fase 2 (as correções da C3, o alinhamento do app, a rota
de ditar e o compose) pelos merges `922627c` (documentos), `e4a9aac` (app) e `50fd062` (servidor). **Falta
o deploy e o teste no aparelho (C7), que o Mauro autorizou em 11-out-2026 e vem a seguir**, e, se ele
quiser, a medição de qualidade (C8). O primeiro áudio real segue esperando o OK dele na política e no
dossiê (D0). Os hashes abaixo são os dos commits na `main`.

| Subfatia | O que entrega | Estado | Commits |
|---|---|---|---|
| S (OK próprio) | a chegada mais rápida no PC (decisão P): o contador na API local e o painel que relê a lista | **Implementada (10-out-2026).** 10 s com o painel aberto e 60 s fechado (a escolha menor 17, aprovada pelo Mauro). `GET /api/nuvem/mudancas` devolve só um número (nenhum texto); a janela é de 15 s depois de cada leitura, e a abertura do painel adianta a busca; o painel visível lê o contador a cada 3 s e relê a lista quando ele muda. **Teste no PC: o Mauro faz depois de remontar o Agent** (fechar o Agent e conferir o `StartTime`) | `6e599dc` (laço e contador), `cbb6d8b` (painel) |
| LG (OK próprio) | as pendências L1 a L5 da LGPD, só documentos, na cópia da instância dele | **Pendente:** espera as respostas do Mauro (a região do backup, a hipótese do art. 33, o contato e as decisões 2 e 3 do dossiê) | — |
| D0 | a ADR e as emendas; REQ-051 a REQ-054, AC-065 a AC-079 e THR-026 a THR-031; a política e o dossiê reescritos; README e PRODUCT | **Escrita (10-out-2026).** O OK do Mauro na ADR é de 10-out-2026 e liberou o código. **O OK dele na política e no dossiê está pendente** e é o que libera o primeiro áudio real | `b0820dd` (a ADR), `c4d0223` (as emendas), `feaaa12` (política, dossiê, README e PRODUCT) |
| C1 | `Store.Create` e `Store.Situacao` nos dois Stores, com o epoch esperado; o `Actor` sem aparelho; a origem "pelo app" no histórico do PC | **Implementada (10-out-2026).** O storage do PC passou a considerar a origem em "nasceu aqui": o ditado do app chega sem aparelho e não é histórico deste PC | `1d36609` |
| C2 | `internal/smartconta`: o Smart da conta, o `Colapsar` e a dica | **Implementada (10-out-2026).** Resolver leva cerca de 4 ms; montar do zero leva cerca de 1 s com 500 pares e cerca de 50 ms com 100; **o cache por conta entrou** (cerca de 3,7 ms com o cache acertado). A meta de 25 ms vale para resolver e para o cache, e não para montar do zero. O índice da busca do dicionário deixou também o Smart do PC mais rápido (6,03 ms para 0,243 ms no benchmark com o dicionário real), sem mudar o resultado | `43e6648`; `1d76697` (o índice do dicionário) |
| C3a | o cofre das chaves da IA e as rotas `/ai-keys` | **Implementada (11-out-2026), com a revisão adversarial do mesmo dia sem correção** (cinco mutações pegas e uma lacuna de teste, sem commit). As pendências da revisão foram: a corrida do apagar/revogar durante o `PUT` da chave, para a C3b; e o `GET` dos modelos, que tem efeito colateral (gasta uma ficha do limite), para o `06` ou a C6b. **As duas foram fechadas na C3-correções (`6925ecc`), abaixo** | `d0ae5b6` |
| C3b | o aceite e as preferências: as migrations 0018 e 0019 e as rotas `/dictation` | **Implementada (11-out-2026).** A versão do aviso mora no Go e no app, presas por um teste; o modelo padrão da OpenAI é o `gpt-4o-mini-transcribe` até a C7. A corrida do `PUT` da chave contra o apagar e o revogar, que a revisão da C3a levou para cá, não foi fechada nela, e **foi fechada na C3-correções (`6925ecc`), abaixo** | `6447cf0` |
| C3-correções | as pontas que a revisão da C3a e a C3b deixaram: a gravação da chave sob a trava da conta, o `GET` dos modelos só da mesma origem, o limite por conta e o deadlock do revogar com o apagar | **Implementada (11-out-2026).** O `PUT /ai-keys/{provedor}` lê o `sync_epoch` primeiro, grava a chave numa transação que trava a linha do usuário e o contador (nessa ordem) e só então reconfere o epoch e o aceite em aberto que cobre o provedor (`Aceite.Cobre`): a corrida com o "Apagar meus dados" e com o "Desligar o ditado" não regrava a chave (`409` `CONFLICT`, motivo de log `conta_mudou`, ou `403` `SEM_ACEITE`; nada é gravado). O `GET` e o `HEAD` dos modelos recusam (`403`, `origem_cruzada`) um `Sec-Fetch-Site` que venha e não seja exatamente `same-origin`. O limite das chaves já era por conta, e um teste novo prova que duas sessões da mesma conta dividem o balde (a lacuna de teste da C3a-revisão). Um deadlock da C3b entre o revogar e o apagar (`SQLSTATE 40P01`) foi achado e corrigido (`apagarDaConta` apaga os aceites antes das chaves). O servidor de teste do PC monta o ditado com um cofre por processo e provedores de mentira. 13 mutações, todas pegas | `6925ecc` |
| C4 | o contrato do ASR com áudio comprimido e os provedores OpenAI e Groq (`internal/asr/nuvem`) | **Implementada (10-out-2026).** Os `gpt-*-transcribe` não devolvem segmentos (vale só a medição do app); a lista de modelos exclui os de diarização e os só de inglês; o provider da nuvem recusa PCM. Nenhuma chamada real: os campos de cada modelo se conferem na C7 | `bb82253` (o contrato), `42d92c5` (os provedores) |
| C5a | o adaptador do Polish num pacote comum (`internal/polish`) | **Implementada (10-out-2026).** Paridade no Agent provada (os testes antigos do Polish passam sem edição de expectativa); o pacote compila e testa em Linux. **Teste no PC: o Mauro faz depois de remontar o Agent** | `b1d428f` |
| C5b | a rota `POST /api/v1/dictations` | **Implementada (11-out-2026), com a revisão adversarial do mesmo dia (duas correções).** O `AcessoDitar` só aceita a sessão (o token de um PC é `403`); as conferências vêm antes do corpo; a idempotência é por `(user_id, id)`, com o desfecho pago e não gravado por 10 min na memória; o provedor, o Smart da conta e o Polish rodam num trabalho solto da conexão, com o teto de 44 s; o desligamento espera os ditados. Os status e os prazos escolhidos estão na ADR-021 ("O que a implementação fixou (a fase 2)") e no `06`. **A revisão achou dois defeitos reais e os corrigiu:** o reenvio que chegava no fim do primeiro pagava o provedor de novo e gastava uma ficha (`ca2d21f`), e o "Apagar meus dados" ou o "Desligar" feito durante a chamada ao provedor deixava o Smart da conta de volta no cache (`1eea4b9`). O risco 16 (a janela do aceite revogado antes do `Create`) segue aceito. Mutação: a conferência do aceite antes do Polish não era pega por nenhum caso e ganhou o `Ditar_Corridas_AceiteSemOPolishNoMeio`; as demais mutações foram pegas | `d8e03b9` (a rota), `ca2d21f` e `1eea4b9` (a revisão) |
| C5c | o compose e o deploy | **Implementada (11-out-2026).** O segredo `ai-keys-secret` é declarado no compose (obrigatório) e no `subir.sh`; a API ganha `memswap_limit` igual ao `mem_limit`, `ulimits: core: 0` e `stop_grace_period: 90s` (amarrado por teste aos 15 s e 70 s do `serve`); o `gerar-segredos.sh` cria o `ai-keys-secret` numa VPS nova; o `subir.sh` já roda o `migrate` (as 0017 a 0019, só aditivas); o roteiro de deploy ganhou o passo 10 (a chave de cifra, as migrations e a leitura do proxy e da swap, só leitura). O servidor antigo da F1-D saiu inteiro, `asr_gateway.go` incluído (16 arquivos apagados), e as provas de AC-028, AC-035, AC-041, AC-042 e NFR-012 passaram para outros testes (`fase-1-cobertura.md`). **Nada do Docker nem do `scripts\deploy-teste.ps1` rodou aqui** (regra do projeto): a parte nova do `.ps1` é provada só por texto. 6 mutações, todas pegas | `a8cf426` |
| C6a | o gravador e o envio no app, e o `Permissions-Policy` | **Implementada (10-out-2026).** O copiar sozinho ao chegar não foi feito (escolha menor 14): só o botão Copiar, a conferir no aparelho. Em 11-out-2026, o teste de ponta a ponta contra a nuvem de teste achou a fala medida por quadro de desenho (sem quadros, "Não ouvi nada" e o ditado jogado fora): corrigido por temporizador de 16 ms; depois disso, o caminho inteiro passou | `227d1b1` (o `Permissions-Policy`), `06b1296` (o gravador e o envio), `543f217` (os testes aguentam a máquina ocupada), `4c1419f` (a fala por temporizador) |
| C6b | o aceite e as Configurações do ditado no app | **Implementada (11-out-2026).** A chave não é pedida dentro da folha: depois do "Aceito", o app abre as Configurações. O JSON do cliente não era o do servidor (ver a C6-alinhar) | `243d17f` |
| C6-alinhar | o app e o servidor de mentira do app falando o contrato real do aceite, das preferências e das chaves | **Implementada (11-out-2026).** `cliente.ts` e `servidorFalsoNuvem.ts` seguem o `06` (as chaves como lista, as preferências aninhadas, o aceite `{versao, transcricao, polish?}` com a versão inteira); a versão do aviso é só a `VERSAO_DO_AVISO` inteira (o `aviso.ts` provisório em texto saiu); o `AVISO_DESATUALIZADO` recarrega o app; o aceite do servidor tem dois lugares e cada aceite novo substitui o anterior; o Polish só liga com modelo (a tela guarda o marcado sem modelo como rascunho). `contratoDoDitado.test.ts` prende o servidor de mentira e o cliente ao `06`, e o `avisoDoDitado.test.ts` guarda uma impressão do texto da folha. 7 mutações, todas pegas. Nada visto num Android real | `796d17d` |
| C6c (OK próprio) | a página de calibração do microfone, só do `pnpm dev` | **A página e as provas, implementadas (11-out-2026).** A calibração no aparelho continua com o Mauro: o limiar de fala, a escala da onda e as restrições do `getUserMedia` ainda são os do PC | `6ae9447` |
| C7 | no ar e no aparelho | **No ar (11-out-2026); falta o aparelho.** **O deploy, feito em 11-out-2026** (autorizado pelo Mauro): o ensaio no Docker do PC passou inteiro (as 19 migrations, as rotas do ditado montadas, o `Permissions-Policy` pela borda, o backup decifrado e restaurado); as imagens foram pela `ssh`; a chave de cifra foi criada na VPS pelo `criar-chave-de-cifra.sh`, sem mostrar o valor; o `subir.sh` aplicou a 0017, a 0018 e a 0019 e subiu a API. **As leituras de antes do primeiro áudio, sem nenhum PARE:** o Traefik sem prazo curto nem middleware no entrypoint; o `docker info` sem o aviso de swap; a API com `Memory` igual a `MemorySwap` (256 MB) e o `core` em 0; o `memory.swap.max` do contêiner em `0`; e a VPS **sem swap nenhuma** (`/proc/swaps` vazio). De fora: as rotas novas respondem `401` sem sessão, e a conferência do PC passou. **Falta:** o OK do Mauro na política e no dossiê (D0), a chave dele no app, o aceite e os testes no Android. | — |
| C8 (opcional, OK próprio) | a medição de qualidade, sem arquivo de áudio | **Pendente** | — |

**O que o Mauro testa no PC, depois de remontar o Agent** (a S e a C5a mexem no Agent; nenhum agente roda
o Agent). Antes: fechar o Agent e conferir o `StartTime`, porque recompilar com o Agent aberto deixa o
antigo rodando sem aviso.
- **S:** abrir o painel; editar um ditado no celular e ver a mudança chegar ao painel aberto em até
  cerca de 13 s, sem tocar em nada; digitar num editor aberto enquanto chega algo e conferir que o texto
  fica; fechar o painel, esperar uns 15 s e conferir que o Agent volta aos 60 s.
- **C5a:** ditar com o Polish ligado, uma fala curta e uma longa (acima de uns 250 caracteres). No
  `agent.log`, a linha `winruntime: polish` deve mostrar `prazo_ms` perto de 3500 na curta e maior na
  longa (até uns 8000 aos 1000 caracteres), o desfecho aceito, os tokens e o esforço como antes.
- A C2 também mexeu no Agent (as conferências do lote vieram de `internal/agent/aprendiz`); o teste da
  C5a cobre as duas.

**O que a C5b fixou** (11-out-2026; era a lista "o que a C5b herda"). O `06`, "Ditar pelo celular", e a
ADR-021, "O que a implementação fixou (a fase 2)", têm os nomes, os status e os prazos:
- **Criar:** a rota lê `Store.Situacao` (estado, ditado e epoch num passo só) antes de ler o corpo e
  passa o epoch ao `Create`; `ErrEpochChanged` não grava e vira `SEM_ACEITE`, `ErrTombstoned` vira `410`
  `APAGADO`, `ErrInvalidDictation` vira erro do servidor e `created=false` vira `200`. O `Actor` é
  `Actor{UserID}`, sem aparelho. Quem pega a vaga **lê a situação de novo** antes do corpo (a correção
  `ca2d21f`): o reenvio que chega no fim do primeiro recebe o ditado gravado, e a ficha volta.
- **Smart:** `cloudstore.Learned` vira `dictionary.ParDaNuvem` na ordem do `ListLearned`, e o
  `smartconta.Cache` guarda o Smart por conta. **A versão do cache não é o `last_seq`** (o que a C2
  pedia): é um resumo SHA-256 dos pares que montam o Smart (`versaoDosPares`), que cobre por construção
  o par desfeito, o aprendido apagado e reaprendido e a conta com mais de 500 pares, sem método novo no
  `Store`. O cache se esquece no "Apagar meus dados" e no "Desligar o ditado", **e de novo no fim do
  ditado que um deles cortou** (a correção `1eea4b9`). A dica sai por `smartconta.Dica` e vem desligada.
- **Aceite e chave:** o aceite é o `aceitePeloStore` (`Aceite.Cobre`), conferido antes do corpo, antes do
  Polish e antes de gravar; as preferências, de `LerPreferenciasDoDitado`; a chave, de `chaveDeIA` (sem
  decifrar até o provedor ser chamado); a falha do provedor, de `FalhaDoProvedor`, que passou a traduzir
  também o `*nuvem.Erro` (`sem_chave` vira `SEM_CHAVE` `409`, `arquivo_grande` vira `PROVEDOR_FORA`). A
  rota entra em `rotasDoDitado()` e cai no `404` sem a chave de cifra.
- **Transcrição:** um `nuvem.Provider` por ditado, com o buffer comprimido sem copiar os bytes;
  `ErrSemFala` vira `SEM_FALA` `422`; a duração vem do cabeçalho. **O `Content-Type` `audio/webm` é
  exigido** (`415` antes de ler), além dos primeiros bytes; o `Ditado-Id` é UUID v7; a duração tem o teto
  de 125 s (`413`).
- **Polish:** uma `PoliticaEsforco` por processo, passada a todas as `Montagem`. O modelo é validado
  contra um registro por processo dos modelos que os provedores listaram (`modelos_conhecidos.go`, teto
  de 2000 nomes por provedor, alimentado pelo `PUT` da chave, pelo `GET` dos modelos e por uma pergunta
  de 3 s à lista no próprio ditado); modelo fora da lista: o Polish nem é chamado (`modelo_inexistente`).
  O `aviso` da resposta é um `resolver.MotivoFallback`, e não um `ia.Motivo`. O Smart que falha (sem o
  aprendido, ou a montagem atrasada) leva o Polish a não rodar, com o aviso `dicionario`.
- **Os prazos que a ADR não fixava:** o Smart 5 s para ler o aprendido e 5 s para montar e resolver; a
  lista de modelos 3 s; o trabalho solto, 44 s depois do corpo; quem espera o mesmo id estende o próprio
  prazo de escrita em 80 s; o `Retry-After` do `UM_POR_VEZ` e do `OCUPADO` é 5 s; o prazo do corpo conta
  de quando o pedido pega a vaga (`SetReadDeadline` de 30 s).
- **O contrato com o app:** as diferenças que a C5b deixou para o app (o `aviso`, o `200` com `aviso`
  para quem esperou, o `413` da duração, o UUID v7, o `Content-Type` com parâmetros, o `500` do pago não
  gravado) estão na pendência da C7 abaixo; a C6-alinhar fechou só o aceite, as preferências e as chaves.

**O que a C5c fixou** (11-out-2026; era a lista "o que a C5c herda"):
- **O compose:** o segredo `ai-keys-secret` declarado (`AI_KEYS_SECRET_FILE=/run/secrets/ai-keys-secret`),
  obrigatório; `memswap_limit: 256m` igual ao `mem_limit`; `ulimits: core: 0`; `stop_grace_period: 90s`,
  amarrado por teste aos dois números do `cmd/server/serve.go` (o `Shutdown` de 15 s e a espera de 70 s
  pelos ditados: o teste exige `90 s` e `>= 15 s + 70 s`).
- **Os scripts:** o `SEGREDOS_ESPERADOS` do `subir.sh` tem o `ai-keys-secret`, com uma recusa à parte
  que aponta o `criar-chave-de-cifra.sh` (o `gerar-segredos.sh` recusa rodar numa VPS no ar e só
  confundiria); **o `gerar-segredos.sh` agora cria o `ai-keys-secret`** numa VPS nova, e o
  `criar-chave-de-cifra.sh` recusa nesse caso ("ja existe"), sem mudar nada nem mostrar o valor. **O
  `subir.sh` já roda o `migrate`:** no passo 6 dele, havendo migration pendente, para e pergunta "Digite
  SIM para rodar o migrate agora", e roda num contêiner descartável da imagem nova da API, parando antes
  a API se ela estiver de pé. As 0017 a 0019 só criam tabelas; nada anterior muda.
- **O roteiro (`docs/deploy-f11.md`, passo 10):** 10.1 a chave de cifra, 10.2 as migrations, 10.3 o
  proxy e 10.4 a swap e o core dump, os dois últimos só de leitura. Os valores de parada: `readTimeout`
  do proxy `0` ou maior que 30 s; `writeTimeout` `0` ou de pelo menos 90 s; `memory.swap.max` do
  contêiner em `0`; `HostConfig.MemorySwap` igual a `Memory`. O `roteiros_test` encontra o passo novo.
- **O servidor antigo:** saiu inteiro, `asr_gateway.go` incluído, refeito do zero a partir do `deadcode`
  (rodado com `go run`, sem tocar no `go.mod`) e do compilador, sem olhar branch nem commit antigo. A
  escolha menor 19 da ADR está cumprida. As provas que moravam nos testes apagados passaram para
  outros (`docs/fase-1-cobertura.md`, nota de 11-out-2026).
- **Fora da lista, por necessidade:** o `scripts\deploy-teste.ps1` e o `deploy/pc_parte2_test.go`. O
  compose declara o segredo com `file:`, então o teste do PC grava um `ai-keys-secret` de mentira (64
  hexadecimais) e confere que `GET /api/v1/dictation` sem credencial dá `401` (e não `404`, o que
  provaria que a chave de cifra não chegou) e que o `Permissions-Policy` chega pela borda uma vez só.

**O que a C7 herda** (nada disto foi visto num aparelho nem numa chamada real):
- **Nos provedores:** que o `gpt-transcribe` aceita `languages=pt`, `temperature=0` e `prompt`; o
  `response_format` de cada modelo (a tabela assume `json` para os `gpt-*-transcribe` e `verbose_json`
  para os `whisper`); se o `gpt-transcribe` vira o padrão da OpenAI (a constante `ModeloPadraoOpenAI`,
  hoje o `gpt-4o-mini-transcribe`); que o `Content-Length` sem `chunked` é aceito pelos dois; que a Groq
  cobra o mínimo de 10 s; que a lista de modelos da Groq responde em `/openai/v1/models` com a chave.
  Os limiares do silêncio (0,6 e -1,0) seguem a calibrar.
- **No Android:** o `AudioContext` que nasce suspenso (o app diria "Não ouvi nada." com a pessoa
  falando); o evento `mute` da faixa disparando sem querer no início; o `tracks.stop()` logo depois do
  `recorder.stop()` sem perder o último pedaço; o Chrome paginando o `Blob` em disco (risco 5 da ADR);
  os bytes reais a 32 kbit/s contra o teto de 1 MiB; o Wake Lock, a tela apagando e o `visibilitychange`;
  a folha do aceite (a rolagem, o `dvh` e a área segura com a barra do Chrome, o teclado aberto, o botão
  Voltar do Android com a folha na tela), o `<select>` nativo, colar a chave com o gerenciador de senhas
  do Chrome, e o foco do botão com o leitor de tela.
- **No servidor:** a leitura do proxy e da swap antes do primeiro áudio, como a ADR manda, pelos passos
  10.3 e 10.4 do `docs/deploy-f11.md`, parando em qualquer **PARE**. Três pontos foram escritos pela
  documentação, sem máquina real, e se conferem na primeira leitura: (a) o formato de
  `docker inspect ... {{json .HostConfig.Ulimits}}` (`[{"Name":"core","Hard":0,"Soft":0}]`) e que um
  limite de swap descartado pelo Docker aparece como `MemorySwap` diferente de `Memory` (`-1`); (b) que
  `docker info 2>&1 | grep -i "swap limit"` pega o aviso (o Docker o escreve em `stderr`); (c) o
  caminho do `memory.swap.max`, que o comando acha com `find` pelo id do contêiner (cgroup v2); em
  cgroup v1 o roteiro manda comparar `memory.memsw.limit_in_bytes` com `memory.limit_in_bytes`. O
  limiar de 90 s para o `writeTimeout` do Traefik é a soma dos prazos do servidor com folga (30 s de
  corpo mais 44 s de trabalho): escolha da C5c, a confirmar com o Mauro.
- **Medir no aparelho o caminho inteiro da rota:** o tempo típico do toque final até o texto, os bytes
  de um ditado de 120 s contra o teto de 1 MiB e o `aviso` na tela; e confirmar com o Mauro o teto de
  125 s da duração e o prazo do Smart de 5 s (a primeira montagem de 500 pares leva cerca de 1 s).
- **Rodar `scripts\deploy-teste.ps1` no PC do Mauro** (Docker Desktop): ele agora grava o
  `ai-keys-secret` e faz duas conferências novas; só o texto foi provado nesta fase. Ele não confere o
  `memswap_limit` nem o `ulimits` de propósito (o kernel do Docker Desktop pode descartar a swap e dar
  um falso `FALHOU`): a leitura de verdade é a da VPS.
- **No app (C6-alinhar):** o fluxo novo do Polish (marcar, colar a chave da Anthropic, escolher o
  modelo), a folha com "Escolha um modelo" e o recarregar do `AVISO_DESATUALIZADO` (o service worker com
  rede primeiro na navegação) não foram vistos num Android real. O `Sec-Fetch-Site: same-origin` do
  `GET` dos modelos tem de chegar à API sem que nenhum proxy do caminho o remova ou troque (hoje, sem o
  cabeçalho, a rota aceita).

**Fechado em 11-out-2026** (era o "Fica aberto" da fase 1; ADR-021, riscos 18, 19 e 20):
- **O app e o servidor não falavam o mesmo JSON** (risco 20): a C6-alinhar (`796d17d`) trocou o
  `cliente.ts`, o servidor de mentira e a versão do aviso para o `06`, e o `contratoDoDitado.test.ts`
  prende o servidor de mentira e o cliente ao `06`. Fica uma ponta sem prova no Go: um teste em
  `internal/server` que serialize o estado real e compare os campos com o exemplo do `06`.
- **A corrida do `PUT /ai-keys` contra o apagar e o revogar** (risco 18): a C3-correções (`6925ecc`)
  grava a chave sob a trava da conta, com o epoch e o aceite reconferidos.
- **O `GET` dos modelos com efeito colateral** (risco 19): a C3-correções recusa o `Sec-Fetch-Site` que
  vier e não seja `same-origin`.

**Fica aberto** (levado à frente, não é desta tarefa de documentos):
- **O cliente do ditado (`ClienteNuvem.ditar`, da C6a) contra a rota real.** A C6-alinhar e a C5b
  andaram em paralelo, e ninguém conferiu um contra o outro: o `contratoDoDitado.test.ts` só cobre
  `/dictation` e `/ai-keys`, e o servidor de mentira do app (`ditar()`) e o `erros.ts` seguem a ADR, e
  não o código. As diferenças conhecidas: o `aviso` é um `resolver.MotivoFallback` (e o app espera um
  `ia.Motivo`); o `200` com `aviso` para quem esperou o mesmo id; o `413` da duração acima de 125 s; o
  UUID v7 (versão e variante conferidas); o `Content-Type` `audio/webm` com parâmetros; o `500` do pago
  não gravado, com o reenvio que só grava (`201`); e frases próprias para os avisos das guardas do
  Polish (hoje viram "O Polish não agiu."). O app trata o `409` do ditar como erro comum, e não como
  versão velha. Estender a tabela de status do `contratoDoDitado.test.ts` à rota nova.
- **Um `AVISO_DESATUALIZADO` recarrega a página, e o ditado gravado e não enviado que estava na memória
  se perde.** É raro (exige um deploy que mude o aviso com o app aberto). A alternativa mais
  conservadora é mostrar uma frase com o botão "Recarregar" quando há áudio pendente. Decisão do Mauro.
- **"Padrão do provedor" no modelo da voz nunca fica marcado depois do `PUT`** (o servidor devolve o
  nome). Se ele preferir ver "Padrão do provedor", o app precisa conhecer o modelo padrão de cada
  provedor (hoje constantes do servidor, a mudar na C7 para o `gpt-transcribe`) ou o servidor expor
  `padrao` no estado.
- **Baixo risco, da revisão da C5b** (ADR-021, risco 21): o pago e o "sem fala" vencem só quando chega um
  pedido novo (um `time.AfterFunc` com relógio injetável resolveria); no desligamento, o
  `EsperarDitados` pode voltar com 0 trabalhos se o primeiro `Shutdown` estoura (chamá-lo de novo
  depois do segundo `Shutdown`); um `defer` de guarda para o pânico entre pegar a vaga e soltar o
  trabalho; e o registro de modelos conhecidos é global por provedor.
- **Sem o `-race` aqui:** a máquina não tem compilador C. `internal/smartconta` (`Cache`, `Conta`),
  `internal/asr/nuvem` e `internal/server` (o registro da rota, o `desistir`, o `olharDeNovo` e o
  trabalho solto) pedem a rodada com `-race` no CI Linux.
- **Testes sensíveis a carga**, que falham com a máquina ocupada e passam sozinhos: `deploy`
  (`TestSubir_*`, restaurar e gerar, com o teto fixo de 60 s do bash; o pacote passa dos 10 min do
  padrão do `go test`), `internal/storage` (`TestEspera_*`), `internal/agent/audio`
  (`TestSemprePronto_*`), `candidate.TestRunCold_Success` e `TestWarmSession_CostNeverSummedWithCold`
  (o relógio do Windows), `benchmark/rss`, `server/integration` `TestSyncE2E_PutNaoEsperaOPushTravado`,
  `pgstore.TestContrato_Postgres` (quando várias frentes dividem o contêiner de teste) e alguns do
  vitest da nuvem (o `findBy` com 1 s). A rodada de fechamento usa `go test ./... -count=1 -p 4
  -timeout 60m`, de preferência com a máquina quieta.

## TASK-029 — Aviso de microfone bloqueado pelo Windows (AC-080)
Quem instalou o app com o microfone bloqueado pela privacidade do Windows não via nada. Decisão do Mauro:
o Agent percebe o bloqueio, avisa, e o painel tem um botão que abre a tela certa do Windows. Nenhuma
dependência nova. Em quatro fatias, cada uma com testes:
- `internal/agent/permissao`: a leitura só leitura do registro (política `AppPrivacy` e `ConsentStore`
  do aparelho, da conta, dos programas e deste executável) e a decisão pura (empresa, aparelho, conta,
  programas, este app);
- a pílula `FaseMicrofoneBloqueado` (fora do núcleo, como a da janela elevada), a troca no runtime
  quando o erro acontece com o microfone bloqueado e a linha WARN da subida;
- `GET /api/microfone` e `POST /api/microfone/configuracoes` na API local, com `painel.AbrirConfiguracoesDoMicrofone`
  atrás do portão do `sysinput`;
- o aviso do painel do PC (`web/src/AvisoDoMicrofone.tsx`, entra pelo `Ambiente`; a nuvem não ganha nada).

Falta o teste no PC, que é do Mauro: `docs/teste-manual-bandeja-e-painel.md`, parte 7.

## TASK-030 — Notas (ADR-022)
O Mauro quer transformar ditados em anotações úteis, organizadas como no Google Keep (marcadores, cor,
fixar, arquivar), no PC e no celular, para deixar de usar o Keep. Aceita em 25-out-2026, com o gatilho
próprio para "ditar para nota" e a lista marcável sem rede no celular. Fases (detalhe na ADR):
- **L:** listas melhores no Polish (as guardas para lista, medidas no `polishbench`);
- **N1:** banco local (SQLite) e API local das notas e marcadores;
- **N2:** aba Notas no painel, editor por linhas, ditar dentro (verificação manual). **Feita nos dublês
  (25-out-2026):** a aba entre Histórico e Lixeira (só no PC), os marcadores, as três formas de ver com
  colunas guardadas por aparelho, criar, a nota aberta com autosave e conflito, o editor por linhas, o
  Arquivo e a Lixeira (`web/src/notas/`, AC-081 e AC-082). **Falta o teste no PC, que é do Mauro:**
  `docs/teste-manual-notas-n2.md` (o olhar no navegador e ditar dentro da nota);
- **N3:** "Virar nota" no histórico, com Desfazer;
- **N4:** marcadores, filtro e sugestão local;
- **N5:** converter trecho (prefixo, regra e IA com as guardas; prompt `notas-lista@1`);
- **N6:** lista de marcar, copiar para WhatsApp, busca única;
- **N7:** sync das notas e marcadores (migrations do Postgres; **deploy é ponto de parada**);
- **N8:** aba Notas no celular, "Ditar para nota" e a fila offline;
- **N9:** gatilho "ditar para nota" no PC;
- **N10:** imagens (**ponto de parada antes**: disco do servidor e LGPD).

Os documentos da spec (`02`, `03`, `05`, `06`, `07`, `08`, `09`, `20`) são emendados por fase, junto
da slice que muda o contrato.

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
