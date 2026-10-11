# Relatório de Cobertura — Fase 1 (validação de contratos com mocks)

Data: 2026-09-17. Autor: agente de documentação, escopo exclusivo deste arquivo.

## O que "verde por mock" significa — e o que não significa

Nesta fase o Windows Agent não tem hook de teclado real, captura de áudio WASAPI real,
UI Automation real, inserção em aplicações reais, instalador real nem OAuth real. Tudo
isso vive atrás de interfaces (`AudioCapture`, `Transcriber`, `Inserter`, `ContextProvider`,
`ASRProvider`, `CorrectionRepository` etc.) e os testes exercitam **implementações fake**
dessas interfaces — não o hardware, não o Windows de verdade, não uma aplicação real
recebendo texto.

"Verde por mock" **significa**: existe um teste automatizado, determinístico, que constrói
o cenário do critério via fake/fixture e que **falharia** se a regra de negócio fosse
quebrada — a máquina de estados emitisse um efeito a mais, o resolver chamasse o dicionário
quando não devia, o clipboard não fosse restaurado, a purge acontecesse um dia cedo demais.
É prova de que a lógica de domínio, isolada de I/O real, está correta.

"Verde por mock" **não significa**: que o hook global de teclado funciona no Windows real,
que o WASAPI captura áudio de um microfone físico, que o UI Automation lê o campo focado
de um app real, que o texto é de fato inserido no Notepad/Outlook/Chrome, que o clipboard
do SO é restaurado, que o instalador roda numa máquina limpa, que o login Google funciona,
que o schema roda num PostgreSQL de verdade, ou que os limites de memória/latência são
respeitados em hardware real. Nenhuma dessas coisas foi provada nesta fase — ver a seção
"O que a Fase 1 não prova".

Uma nota metodológica: vários testes desta suíte já carregam comentários dos próprios
autores admitindo esse limite explicitamente (ex.: `TestAC009_ClipboardERestaurado`,
`TestAC005_OverlayRecebeOndaDuracaoEEstado`). Isso é saudável — significa que a suíte não
está se vendendo como mais do que é — e este relatório manteve essa mesma honestidade ao
classificar cada critério, sem dar o benefício da dúvida a teste cujo nome cita o critério
mas cujo corpo prova outra coisa.

---

## Nota sobre um bloqueador transitório observado durante a escrita deste relatório

Enquanto este relatório estava sendo escrito, um agente em paralelo editava
`internal/server/integration/dictionary_sync_test.go` (a cobertura nova do AC-021 — ver
tabela abaixo) e, por alguns minutos, o arquivo não compilava (`go vet` acusou import não
usado: primeiro `syncengine`, depois `reflect`, em edições sucessivas). Como esse arquivo
está no mesmo pacote Go que `sync_e2e_test.go`, `harness_test.go` e `local_mirror_test.go`,
isso teria derrubado a compilação de todo `internal/server/integration` — inclusive os
testes de AC-015, AC-016 e AC-017, que só existem nesse pacote.

Ao final da apuração, o arquivo já estava corrigido. **Estado verificado nesta sessão, na
árvore de trabalho completa, na ordem abaixo:**

```
go build ./...   → OK
gofmt -l .        → limpo (nenhum arquivo)
go vet ./...      → limpo, nenhuma saída
go test ./...     → ok em todos os 21 pacotes (os sem *_test.go reportam "no test files")
```

Isto inclui `internal/server/integration`, com `TestAC015_DitadoCriadoNoWindowsApareceNaCloud`,
`TestAC016_EdicaoNaCloudVoltaParaOWindowsNoProximoPull`,
`TestAC017_DeleteSincronizaEEntidadeVaiParaALixeira`,
`TestAC021_DicionarioSincronizaEntreDevices` e
`TestAC021_IsolamentoDicionarioEntreUsuarios` — todos `PASS`, confirmados individualmente
com `-run` e `-v` além do `go test ./...` completo.

Registro isto explicitamente porque é o tipo de coisa que um relatório de cobertura não
pode varrer para debaixo do tapete: por um intervalo real desta sessão, `go vet ./...`
esteve vermelho no repositório, e a suíte não podia ser honestamente chamada de "verde"
sem qualificação. A foto final é verde; a dívida de processo — dois agentes editando o
mesmo pacote de teste ao mesmo tempo, sem coordenação visível — não é um problema deste
documento, mas vale o lead saber que aconteceu.

---

## Critérios funcionais (AC-001 a AC-045)

| ID | Critério | Classificação | Evidência | Observação |
|---|---|---|---|---|
| AC-001 | Tap curto produz função normal | VERDE POR MOCK | `machine_test.go` (tap), `ac_test.go:TestAC001...`, `given_when_then_test.go:TEST-001` | Prova exatamente um `SynthesizeTapEffect` e nenhuma gravação. |
| AC-002 | Hold não produz caractere e inicia gravação | VERDE POR MOCK | `machine_test.go` (hold), `ac_test.go:TestAC002...`, TEST-002 | Prova ausência de `SynthesizeTapEffect` e overlay em `Recording`. |
| AC-003 | Início da fala não é perdido (por modo, ADR-015) | VERDE POR MOCK | Sempre pronto: `ac_test.go:TestAC003_InicioDaFalaNaoEPerdido`, `audio/microfone_test.go:TestSemprePronto_*`. Sob demanda (padrão): `audio/microfone_test.go:TestSobDemanda_AC003_CapturaComecaAteLDepoisDaMarca` | O `TestAC003` da Fase 1 alimenta frames *antes* do keydown contra um dublê sempre ligado: descreve o modo "sempre pronto". No sob demanda, o verde por mock cobre só o que o código controla, em `internal/agent/audio`, com um dispositivo falso que demora para gravar o primeiro frame: a **medição** do início acompanha o microfone (abaixo e acima de L) e nada se perde entre o primeiro frame e o corte; o valor de L está preso em `TestLimiteDeInicio`, e o alerta acima de L em `winruntime/microfone_test.go:TestJanela_Alertas`. **Não prova o "até L"** — é propriedade do microfone real, e vem do log (`inicio_rel_marca_ms`), do roteiro manual (parte 6 de `teste-manual-bandeja-e-painel.md`) e do uso real (ADR-015, "Revisão"). |
| AC-004 | Modificadores puros não são configuráveis | VERDE POR MOCK | `ac_test.go:TestAC004...`, `config.go` (validação) | Prova por construção: `Config` rejeita modificador puro como tecla, em qualquer variante L/R. |
| AC-005 | Overlay mostra onda/duração | PARCIAL | `ac_test.go:TestAC005_OverlayRecebeOndaDuracaoEEstado` | Prova que frames de áudio e duração real chegam até a borda que o overlay consumiria. **Não prova** que um overlay Win32/WPF real desenha isso — esse componente não existe no repositório ainda (nenhum código com `syscall`/`golang.org/x/sys/windows` foi encontrado em `internal/` ou `cmd/`). |
| AC-006 | Beep pode ser desligado | VERDE POR MOCK | `ac_test.go:TestAC006_BeepPodeSerDesligado`, `machine_test.go` | Config `BeepEnabled=false` → nenhum `PlayBeepEffect`. |
| AC-007 | Soltar finaliza captura | VERDE POR MOCK | `ac_test.go:TestAC007...`, `machine_test.go` | Fluxo completo verificado. |
| AC-008 | Texto entra no campo focado | PARCIAL | `ac_test.go:TestAC008_TextoEntraNoCampoFocado`, `fakes/inserter.go` | Prova que o texto pedido chega ao `Inserter` exatamente como gerado. **Não prova** inserção real via UI Automation num app real — não há `Inserter` nativo no repositório. |
| AC-009 | Clipboard é restaurado | PARCIAL | `ac_test.go:TestAC009_ClipboardERestaurado` (dois subtestes, incl. falha de inserção) | O próprio teste documenta o limite: sequência snapshot→set→paste→restore só é exercitada dentro do `FakeInserter`; só vira regressão real contra o clipboard do Windows quando o `Inserter` nativo existir. |
| AC-010 | Sem fala válida, nada é inserido | VERDE POR MOCK | `ac_test.go:TestAC010...`, `machine_test.go` | VAD sem fala → nenhum efeito de inserção, outcome `no_speech`. |
| AC-011 | Dashboard local mostra histórico | PARCIAL | `internal/agent/localapi/history_test.go` (`handleListHistory`) | A **API** local (`GET /api/history`) está provada. O **dashboard** (React, `web/src/App.tsx`) é hoje um scaffold — `<p>Dashboard em construção.</p>`, sem nenhuma tela de histórico. O critério fala em "Dashboard mostra", não só "API expõe". |
| AC-012 | Dashboard local não aceita conexão externa | VERDE POR MOCK | `internal/agent/localapi/ac012_external_test.go` | Sobe um `http.Server` real sobre `guard.Protect`, ataca via HTTP real (sem token, host forjado/DNS rebinding, origin cross-origin, token errado) e confirma 401/403; e confirma 200 na chamada legítima. É o teste mais forte da suíte — servidor real, requisições HTTP reais, só não sai da máquina. |
| AC-013 | Histórico permite busca/copiar/editar/excluir | PARCIAL | `history_test.go` (busca via `q`, `PATCH` edita `final_text`, `DELETE`→lixeira, `POST /restore`) | Busca, editar e excluir estão provados na API. **"Copiar" não tem nenhum teste** (é ação de UI/clipboard, e não há UI). Mesma ressalva do AC-011: é a API, não o dashboard. |
| AC-014 | Google login abre cloud dashboard | AUSENTE | — | Nenhuma referência a OAuth/Google em `internal/`, `cmd/` ou `web/`. Não há rota de login, callback, nem teste. Depende de credencial real de qualquer forma (ver seção seguinte). |
| AC-015 | Ditado Windows aparece na cloud | VERDE POR MOCK | `internal/server/integration/sync_e2e_test.go:TestAC015_DitadoCriadoNoWindowsApareceNaCloud` | Confirmado `PASS` nesta sessão. |
| AC-016 | Edição cloud retorna ao Windows | **VERDE NO SERVIDOR** (era PARCIAL; ADR-019, F2, 05-out-2026) | `internal/server/contracttest/suite.go:testAC016EditViaAPIReachesPull`, rodando em `TestContrato_StoreEmMemoria` e em `internal/server/pgstore:TestContrato_Postgres` (Postgres 18 de verdade) | A edição pela rota `PATCH /transcriptions/{id}` (a que o celular usa) passa pelo ponto único de gravação, grava o `SyncEvent` na mesma transação e aparece no pull do PC (seq, versão, texto, aparelho). Lixeira e restaurar pelo CRUD também. O que falta é o **lado do PC**: aplicar o pull no SQLite é a F7a. O teste antigo (`sync_e2e_test.go`), que edita por um segundo cliente de sync, continua no contrato antigo até a F7a. Ver [[ADR-009]]. |
| AC-017 | Delete sincroniza para trash | VERDE POR MOCK | `sync_e2e_test.go:TestAC017_DeleteSincronizaEEntidadeVaiParaALixeira`, `local_mirror_test.go` | Confirmado `PASS`; asserção correta de tombstone (`Deleted=true`), não remoção física. |
| AC-018 | Restore funciona até 30 dias | VERDE POR MOCK | `internal/retention/job_test.go:TestJob_Trash_RestoreWorks_D1_D15_D29` | Clock fake avança D+1/D15/D29 e confirma restore ainda funciona. |
| AC-019 | Trash purga após 30 dias | VERDE POR MOCK | `job_test.go:TestJob_Trash_PurgesAtD30_NotD29` | Prova a borda exata: D+29 não purga, D+30 purga. |
| AC-020 | Histórico ativo purga após 6 meses | VERDE POR MOCK | `job_test.go:TestJob_ActiveHistory_PurgesAt6Months_NotADayBefore` | Prova a borda exata. |
| AC-021 | Dicionário sincroniza | PARCIAL | `internal/server/integration/dictionary_sync_test.go:TestAC021_DicionarioSincronizaEntreDevices`, `TestAC021_IsolamentoDicionarioEntreUsuarios` | Confirmado `PASS` nesta sessão. O teste é bem desenhado — cobre criação, pull por outro device, alias `source=learned`, edição propagada e isolamento por usuário — mas o próprio comentário do teste assume explicitamente que **não existe hoje** um adaptador de produção em `internal/dictionary` que converta `DictionaryEntry`/`DictionaryAlias` para `syncengine.Mutation`; o teste exercita o envelope genérico de sync com payload no formato do dicionário, não um caminho de produção dedicado — por isso PARCIAL, e não VERDE pleno. Antes deste teste, o único ponto de contato era `internal/sync/engine_test.go`, que usa `"dictionary_entry"` apenas como rótulo de entidade genérica, sem checar nenhum campo real de dicionário — isso não provava o AC-021 e não deveria ter sido contado como prova dele. |
| AC-022 | Aprendizado exige confirmação | VERDE POR MOCK | `internal/learning/confirm_test.go:TestConfirm_AC022_SemConfirmacaoNadaECriado` | Repositório espião falha o teste imediatamente se `Upsert` for chamado sem `Confirmation` — prova por construção. |
| AC-023 | Literal não chama resolver semântico | VERDE POR MOCK | `internal/resolver/literal_test.go` | Verifica via reflection que `LiteralResolver` não tem nenhum campo que alcance dicionário/contexto/cloud — prova por construção, não por execução de caminho feliz. |
| AC-024 | Smart altera apenas conforme política | VERDE POR MOCK | `smart_test.go`, `cloud_test.go`, `spec_test004_test.go` | Cobre resolução fonética, não persistência de contexto e não-chamada ao cloud fora de política. |
| AC-025 | Polish é opt-in e preserva raw/final | VERDE POR MOCK | `internal/resolver/polish_test.go` | Teste falha explicitamente se `RawText`/`FinalText` não estiverem sempre presentes. "Opt-in" é estrutural (política é uma escolha de config que seleciona o resolver), mesmo padrão do AC-023. |
| AC-026 | Contexto sem screenshot | VERDE POR MOCK | `internal/context/noscreenshot_test.go:TestNoPixelCaptureOrTextRecognition` | Varredura estática do pacote por import/símbolo de GDI/OCR — prova ausência de capacidade, não só ausência de uso. |
| AC-027 | Contexto não aparece em DB/logs | VERDE POR MOCK | `internal/context/snapshot_test.go` | `ContextSnapshot.MarshalJSON` falha deliberadamente e `String()` não vaza conteúdo — prova por construção que o tipo não pode ser serializado/logado por acidente. |
| AC-028 | Áudio não aparece em arquivo/DB/log | VERDE POR MOCK | `internal/server/asr_gateway_test.go` | Confirma que nenhum arquivo novo aparece no diretório temporário depois de transcrever áudio real (em memória). |
| AC-029 | Providers usam mesmo contrato | VERDE POR MOCK | `internal/asr/provider_conformance_test.go` | Suíte de conformidade compartilhada, rodada contra múltiplos fakes com capabilities diferentes. |
| AC-030 | Fallback cloud só quando policy permite | VERDE POR MOCK | `internal/resolver/cloud_test.go:TestSmartResolver_CloudNotCalledWhenPolicyDisallows`, `internal/asr/engine_test.go` | — |
| AC-031 | local_only não envia áudio/contexto | VERDE POR MOCK | `internal/asr/engine_test.go:TestEngine_LocalOnly_NeverCallsCloud_AC030_AC031` | Espião `fatalOnCallCloud` falha o teste se o cloud for chamado. |
| AC-032 | cloud_only usa gateway autenticado | VERDE POR MOCK | `engine_test.go:TestEngine_CloudOnly_RequiresAuthentication_AC032`, `TestEngine_CloudOnly_Authenticated_CallsCloud` | Cobre tanto a falha limpa sem credencial quanto o caminho autenticado chamando o cloud. |
| AC-033 | Local funciona sem cloud | VERDE POR MOCK | `internal/sync/engine_test.go` (`TestEngine_RunOnce_PullFailure...`), `server/integration/sync_e2e_test.go` | Prova em `internal/sync` (unitária) e em `server/integration` (ponta a ponta). |
| AC-034 | Outbox sincroniza ao reconectar | VERDE POR MOCK | `internal/sync/engine_test.go:TestEngine_RunOnce_ResendIsIdempotent`, `server/integration/sync_e2e_test.go` | Idem: unitária e ponta a ponta. |
| AC-035 | Dados cloud isolados por user | VERDE POR MOCK | `internal/server/isolation_test.go:TestAC035_TranscriptionsIsolatedByUser`, `server_test.go` | Prova em `internal/server`; isolamento por usuário também reforçado para dicionário em AC-021. |
| AC-036 | Protected field bloqueado | VERDE POR MOCK | `edge_cases_test.go:TestEdgeCampoProtegido_BlockedSemAudioSemContexto`, `context/fake_test.go` | Campo protegido → snapshot vazio, nenhum `RecordHistoryEffect`. |
| AC-037 | Autostart liga/desliga | AUSENTE | `internal/agent/localapi/settings.go` (campo `Autostart bool`) | Existe apenas um campo booleano persistido via `PUT /api/settings`. O teste de settings (`settings_test.go:TestSettings_PutThenGet_Roundtrips`) nem sequer verifica esse campo no roundtrip (só verifica `hotkey`). Não há nenhum código que registre/remova entrada no Registro do Windows (`HKCU\...\Run`), e não há teste de que ligar/desligar produza efeito algum. |
| AC-038 | Installer instala/remove corretamente | DIFERIDO | — | Não existe instalador no repositório (`deploy/`, `scripts/` só têm README). Depende de build de instalador real e execução em máquina limpa — fora do escopo da Fase 1 (contratos com mocks), correto adiar para o sprint de rollout (ver `docs/ai-spec/12-rollout.md`). **Atualização (ADR-018, 30-set-2026):** já há o script do instalador (`installer/aurora-whisper.iss`), a montagem do pacote (`scripts/montar-instalador.ps1`) e o workflow de release rascunho, **escritos mas não compilados nem executados**; continua DIFERIDO até compilar o Setup e instalar e desinstalar numa conta limpa (roteiro manual, F6). |
| AC-039 | Update exige confirmação | VERDE POR MOCK | `internal/update/install_test.go` | Sem `confirmed=true`, `installer.Install` nunca é chamado — prova por construção com espião. |
| AC-040 | Secrets de provider não ficam no desktop | VERDE POR MOCK | `internal/update/security_test.go` | Varredura estática de identificadores contra lista de termos de segredo, mais teste de requisição sem header de credencial. |
| AC-041 | Backend transcreve sem gravar áudio | VERDE POR MOCK | `internal/server/asr_gateway_test.go` | Mesma prova do AC-028, do lado do backend. |
| AC-042 | API aceita futuro Android | VERDE POR MOCK | `internal/server/platform_test.go:TestAC042_AndroidPlatformAccepted` | `source_platform` aceita qualquer texto, sem enum fechado a "windows". |
| AC-043 | Histórico mostra provider/model/duração/latência | VERDE POR MOCK | `history_test.go` (assert de campos na resposta) | — |
| AC-044 | Tecla e threshold configuráveis | VERDE POR MOCK | `ac_test.go:TestAC044_TeclaEThresholdConfiguraveis` | Faixa `[300ms, 2000ms]` verificada. |
| AC-045 | Tecla com modifier preserva comportamento normal | VERDE POR MOCK | `ac_test.go:TestAC045...`, `decider_test.go` | Ex.: Ctrl+F continua funcionando como atalho normal. |

## Critérios não funcionais (NFR-001 a NFR-014)

| ID | Critério | Classificação | Evidência | Observação |
|---|---|---|---|---|
| NFR-001 | Windows 10/11 x64 | DIFERIDO | CI (`.github/workflows/ci.yml`) roda `go build`/`go test` em `windows-latest` | Prova que o código compila e os testes passam em Windows (Windows Server 2022, não 10/11) — não prova comportamento em Windows 10/11 de verdade, porque nenhum código usa API do Windows ainda. |
| NFR-002 | ASR local ≤~1 GB working set | DIFERIDO | — | Depende de modelo ASR real e hardware real — Sprint 3 (benchmark), conforme instrução da tarefa. |
| NFR-003 | Agent base ≤250 MB | DIFERIDO | — | Depende de binário final com hook/áudio/UIA reais, que não existem ainda. |
| NFR-004 | p50 local ≤500 ms (1–10s) | DIFERIDO | — | Depende de benchmark com hardware real e modelo real — Sprint 3. |
| NFR-005 | p95 local ≤1.2 s | DIFERIDO | — | Idem NFR-004. |
| NFR-006 | Sync idempotente | VERDE POR MOCK | `internal/sync/engine_test.go:TestEngine_RunOnce_ResendIsIdempotent` | Reenviar a mesma mutação não duplica no servidor nem gera segunda versão. |
| NFR-007 | Local API bind somente loopback | VERDE POR MOCK | `internal/agent/guard/listener.go`, `ac012_external_test.go` | — |
| NFR-008 | Cloud somente HTTPS em produção | DIFERIDO | — | `cmd/server/main.go` não configura TLS; a expectativa (comentário em `httpx.go`) é que TLS/porta/shutdown seja decisão de deploy (provavelmente proxy reverso na VPS). Não há teste porque é uma decisão de infraestrutura de produção, fora do escopo desta fase, e o CLAUDE.md do projeto proíbe mexer em infraestrutura real sem autorização. |
| NFR-009 | Sem secrets em logs/git | VERDE | CI: job `secrets` em `.github/workflows/ci.yml` roda `gitleaks detect` contra o histórico git inteiro | Não é um `go test`, é um scan de CI real e determinístico — mais forte que mock, porque escaneia o artefato de verdade (o histórico git), não uma simulação. |
| NFR-010 | Sem áudio/contexto em logs | VERDE POR MOCK | `context/snapshot_test.go` (AC-027), `asr/provider.go` (contrato) | Prova majoritariamente por construção (tipo não serializável) — não há uma varredura de log real de ponta a ponta, porque quase não há logging estruturado implementado ainda (só `guard/chain.go` usa `log`). |
| NFR-011 | Falha cloud não bloqueia uso local | VERDE POR MOCK | `asr/engine_test.go` (AC-031), `sync/engine_test.go:TestEngine_RunOnce_PullFailureDoesNotBlockPush` (AC-033) | — |
| NFR-012 | user_id deriva da sessão | VERDE POR MOCK | `server/isolation_test.go`, `server/server_test.go:TestProtectedRoutesRequireSession` | — |
| NFR-013 | Retention jobs idempotentes | VERDE POR MOCK | `retention/job_test.go` (comentário explícito NFR-013: rodar duas vezes não purga de novo nem falha) | — |
| NFR-014 | UI acessível por teclado/foco | AUSENTE | — | Não há UI para testar acessibilidade: `web/src/App.tsx` é um scaffold de uma linha ("Dashboard em construção"), sem nenhum componente interativo, sem teste de foco/teclado/ARIA. |

## Cenários Given/When/Then (TEST-001 a TEST-010)

| ID | Cenário | Classificação | Evidência |
|---|---|---|---|
| TEST-001 | Tap | VERDE POR MOCK | `given_when_then_test.go:TestTEST001_FCurtoProduzUmFSemGravacao` |
| TEST-002 | Hold | VERDE POR MOCK | `given_when_then_test.go` (TEST-002) |
| TEST-003 | Finalização | VERDE POR MOCK | `given_when_then_test.go:TestTEST003_FalaValidaSoltarInsereTextoFinal` |
| TEST-004 | Contexto (ambiguidade físico/Fisco) | VERDE POR MOCK | `resolver/smart_test.go:TestSmartResolver_TEST004`, `resolver/spec_test004_test.go` | 
| TEST-005 | Offline | VERDE POR MOCK | `sync/engine_test.go` (TEST-005), `sync/fake_server_test.go`, e também `server/integration/sync_e2e_test.go` (TEST-005) |
| TEST-006 | Reconexão | VERDE POR MOCK | `sync/engine_test.go:TestEngine_RunOnce_ResendIsIdempotent` e `server/integration/sync_e2e_test.go` (TEST-006) |
| TEST-007 | Áudio efêmero | VERDE POR MOCK | `machine_test.go` (assert de exatamente 1 `ReleaseAudioEffect`, sempre por último), `fakes/audio.go` |
| TEST-008 | Learning ("n oito n" → "n8n") | VERDE POR MOCK | `learning/confirm_test.go:TestConfirm_TEST008...`, `learning/diff_test.go`, `learning/regression_test.go` |
| TEST-009 | Password field | VERDE POR MOCK | `dictation/acceptance/edge_cases_test.go:TestEdgeCampoProtegido_BlockedSemAudioSemContexto`, `context/fake_test.go` |
| TEST-010 | Fallback (mesmo buffer em RAM) | VERDE POR MOCK | `asr/engine_test.go` (comentários TEST-010: mesmo array, sem cópia, liberado exatamente uma vez) |

---

## O que a Fase 1 não prova

Isto não é uma lista de falhas — é o inventário do que ainda depende de coisas que este
ambiente de desenvolvimento não tem, ou que esta fase deliberadamente não deveria provar
ainda:

- **Hardware/Windows real**: hook global de teclado (`RegisterHotKey`/`SetWindowsHookEx`),
  captura de áudio WASAPI, UI Automation para ler o campo focado, e inserção de texto em
  aplicações reais (Notepad, Outlook, Chrome, campos protegidos de verdade). Nenhum código
  com `syscall` ou `golang.org/x/sys/windows` existe no repositório — tudo isso está atrás
  de interfaces com apenas implementações fake. Todos os ACs de 001 a 010, 013, 036, 044,
  045 provam a *lógica de decisão*, não a *integração com o Windows*.
- **Credencial real (Google OAuth)**: o AC-014 não tem nenhuma cobertura — nem de código,
  nem de teste. Login Google abrir o dashboard cloud depende de um fluxo OAuth que ainda
  não foi implementado.
- **Schema PostgreSQL**: as nove migrations em `migrations/` foram escritas e verificadas
  **textualmente** (`migrations/schema_test.go` confere, via regex, que cada tabela/coluna/
  constraint da spec existe no arquivo certo). Elas **nunca foram executadas** contra um
  PostgreSQL de verdade — não há Postgres nem Docker neste ambiente. A validação real (rodar
  as migrations, testar constraints, testar `ON DELETE`, testar índices) é trabalho do
  Sprint 6, conforme `migrations/README.md` já documenta.
  **Atualização (ADR-019, F1, 01-out-2026):** as migrations viraram dezesseis (a 0002, a 0003, a
  0008 e a 0009 foram corrigidas no lugar, e entraram a 0010 a 0016), e agora existem os testes que
  as aplicam num Postgres de verdade (`migrations/postgres_test.go`, `internal/server/pgmigrate`,
  `cmd/server`), mais o comando `migrate`. Eles só rodam com `AURORA_TEST_POSTGRES_URL`
  (`scripts/postgres-de-teste.ps1`) e **rodaram verdes num Postgres 18 em 05-out-2026** (rodada do
  Mauro). O que este parágrafo diz do estado anterior vale para o que existia antes da F1.
- **`go test -race`**: não roda, porque o toolchain C necessário para o detector de race
  não está disponível neste ambiente (`CGO_ENABLED` funcional para isso). O CI
  (`.github/workflows/ci.yml`) também não passa `-race` em nenhum job. Isso significa que
  nenhuma garantia de ausência de data race foi verificada automaticamente, em nenhum dos
  pacotes concorrentes (`sync`, `server`, `retention`).
- **Benchmark de ASR (NFR-002/003/004/005)**: só acontece no Sprint 3, com hardware real e
  modelo real (Whisper/Parakeet local). Nenhum número de memória ou latência tem qualquer
  medição nesta fase — os alvos da spec permanecem não verificados.
- **Instalador (AC-038) e autostart real (AC-037)**: não há instalador no repositório, e o
  "autostart" hoje é só um campo booleano persistido, sem nenhum efeito sobre o Registro do
  Windows.
- **Dashboard React**: `web/src/App.tsx` é um scaffold de uma linha. Toda a cobertura de
  AC-011/013/014/016 e NFR-014 hoje vem exclusivamente da API HTTP local (`localapi`) e da
  API cloud (`server`) — nenhuma tela existe para mostrar histórico, buscar, copiar, editar,
  logar com Google, ou ser navegável por teclado.
- **HTTPS em produção (NFR-008)**: decisão de infraestrutura de deploy, fora do escopo desta
  fase e do que o `CLAUDE.md` do projeto autoriza mexer sem aprovação humana explícita.

---

## Dívida — o que ficou AUSENTE e precisa de teste antes do sprint correspondente

| Item | Por que é dívida | Antes de qual sprint |
|---|---|---|
| **AC-014** (Google login) | Zero cobertura, zero código. Precisa de implementação OAuth + teste de fluxo (mesmo que com um IdP fake) antes de qualquer sprint que dependa de login funcionando. | Sprint que implementa o dashboard cloud (ligado a AC-011/013/016). |
| **AC-037** (autostart liga/desliga) | Existe só um campo booleano sem efeito nem teste de roundtrip. Precisa de: (a) o teste de settings assertar o campo `autostart` no roundtrip, e (b) um teste (com fake do Registro, ou abstração equivalente) de que ligar/desligar de fato altera a entrada de autostart. | Sprint que implementa o Agent real no Windows (junto com hook/UIA). |
| **NFR-014** (UI acessível) | Não há UI para testar. Quando o dashboard React nascer, precisa de testes de navegação por teclado e foco (mínimo: `@testing-library` com `userEvent.tab()`/checagem de `aria-*`) desde o primeiro componente, não como retrofit depois. | Sprint do dashboard React. |
| **AC-021** (Dicionário sincroniza) — parcial | O teste novo prova o caminho via envelope genérico de sync, mas confirma que **não existe** um adaptador de produção `internal/dictionary` → `syncengine.Mutation`. Sem esse adaptador, nada no Agent real hoje dispara essa sincronização — é o teste provando um caminho que o código de produção ainda não percorre. Precisa: criar o adaptador real e reapontar (ou complementar) o teste para exercitá-lo. | Sprint que liga o pacote `internal/dictionary` ao `internal/sync` de verdade. |

Itens **DIFERIDOS** (AC-038, NFR-001/002/003/004/005/008) não entram nesta tabela de dívida
porque, pela definição da própria tarefa desta fase, não deveriam ter teste ainda — eles
dependem de hardware, infraestrutura de produção ou artefatos (instalador) que a Fase 1
("validação de contratos com mocks") não se propôs a construir.

---

## Números finais

**Total de critérios avaliados: 69** (45 funcionais + 14 não funcionais + 10 cenários).

| Categoria | Funcionais | Não funcionais | Cenários | Total |
|---|---|---|---|---|
| VERDE POR MOCK | 35 | 7 | 10 | 52 |
| PARCIAL | 7 | 0 | 0 | 7 |
| DIFERIDO | 1 | 6 | 0 | 7 |
| AUSENTE | 2 | 1 | 0 | 3 |

Os 7 PARCIAL funcionais (AC-005, AC-008, AC-009, AC-011, AC-013, AC-016, AC-021) refletem lacunas
estruturais reais, não testes quebrados: overlay/inserção/clipboard só são provados contra
implementações fake (não há hook/áudio/UIA/inserter reais no repositório), o dashboard
React não existe além de um scaffold, e a sincronização de dicionário ainda não tem um
adaptador de produção ligando `internal/dictionary` ao `internal/sync` — só o caminho
genérico de sync foi exercitado com payload no formato certo. Todos os testes por trás
destas seis linhas passam (`go test ./...` está verde); a ressalva é sobre o que eles
provam, não sobre se rodam.

Dos 53 VERDE POR MOCK, a esmagadora maioria prova lógica de domínio isolada corretamente —
incluindo vários exemplos de "prova por construção" (AC-022, AC-023, AC-026, AC-027, AC-040)
que são mais fortes que um teste de caminho feliz, porque tornam a violação da regra
estruturalmente impossível de escrever sem quebrar o teste. Isso é o melhor que "verde por
mock" consegue entregar nesta fase — e é exatamente por isso que a seção "o que a Fase 1 não
prova", acima, precisa ser lida junto com estes números, não substituída por eles.


> **Correção do lead, aplicada depois da apuração automática.** O `AC-016` estava
> classificado como VERDE POR MOCK e foi rebaixado para PARCIAL. O teste passa, mas a
> "edição na cloud" é feita por um segundo cliente de sync, e não pela rota REST que um
> dashboard de navegador realmente usaria. Como o CRUD tipado não grava `SyncEvent`, a
> edição feita no dashboard nunca chegaria ao Agent. É um caso exemplar do que este
> relatório existe para pegar: teste verde provando um caminho diferente do que o nome do
> critério promete. Invariante registrada em `docs/adr/ADR-009-toda-mutacao-emite-syncevent.md`.

> **Atualização de 30-set-2026 (ADR-017, aprender com as edições do painel).** Nenhuma classificação
> acima muda por enquanto; o que muda é o que cada linha significa.
>
> - **AC-022** (VERDE POR MOCK) prova que `internal/learning` não grava sem `Confirmation`, e esse
>   caminho só o `cmd/dictlearn` usa. O painel ganha um caminho novo, por um clique em **Aprender**
>   (`/api/aprendizado`). Para ele, a prova de "salvar não grava no dicionário" é
>   `internal/agent/localapi/aprendizado_test.go:TestAC022_SalvarSugereENaoGravaNoDicionario` (um
>   dublê conta cada `Aprender` e `Desfazer`, e compara o dicionário antes e depois), junto com
>   `TestAprenderExigeUmaSugestaoDoServidor` e `TestAprenderRecusaDeQueNaoEstaNoBruto`, que provam
>   que o clique não grava um par que o servidor não sugeriu, nem um "de" que não está no bruto.
> - **TEST-008** ("n oito n" → "n8n": confirmar cria uma Correction **sincronizável**) continua
>   valendo só para a biblioteca. **A Correction não é guardada nem sincronizada na etapa 3**: nada a
>   usa hoje, e ela guardaria a frase inteira além da retenção. Fica para a etapa 5, junto com a parte
>   de sync da TASK-019.
> - **AC-021** (PARCIAL) não muda: o que o painel aprende vai para o `dicionario.json`, que não
>   sincroniza, e o adaptador `internal/dictionary` → `internal/sync` continua sem existir. As tabelas
>   SQLite do dicionário e das correções seguem vazias até a etapa 5.
> - **Ordem:** a S9 (Learning) passou a rodar antes de S6 a S8 (`13-implementation-plan.md`).

> **Atualização de 05-out-2026 (ADR-019, fatia F2).** O **AC-016** sai de PARCIAL: a edição pelo CRUD
> da nuvem (`PATCH /transcriptions/{id}`) passa pelo ponto único de gravação, grava o `SyncEvent` na
> mesma transação e chega ao pull do PC. A prova roda pela API, contra o Store em memória e contra um
> Postgres 18 de verdade (`TestContrato_Postgres`), com o mesmo teste. Fica de fora o lado do PC
> (aplicar o pull no SQLite, F7a), por isso "verde no servidor". As contagens da tabela de resumo são
> as da apuração da Fase 1 e não foram refeitas: com esta linha, os PARCIAL funcionais seriam 6.
> Na revisão da F2 (mesmo dia), o AC-016 continua verde no servidor com a suíte de 19 casos; a
> revisão endureceu o push e as páginas (ADR-019, "Revisão da F2") sem mudar a prova dele.

> **Atualização de 05-out-2026 (ADR-019, fatia F3).** Os critérios da etapa 5 (`03`, AC-052 a AC-063)
> não estão na tabela acima, que é da Fase 1; o que a F3 prova deles, pelo servidor de verdade
> (`internal/server`, `Cloud`) e por um dublê do Google (nenhum teste chama o Google), contra o Store em
> memória e contra um Postgres 18 de verdade (`TestAuth_Memoria`, `internal/server/pgstore:TestAuth_Postgres`):
> - **AC-053 (só entra conta da lista): VERDE NO SERVIDOR.** `internal/server/authtest/suite.go`:
>   `testForaDaLista`, `testSubDiferente` (D7) e `testEmailNaoVerificado` contam as linhas de usuários,
>   contadores e sessões antes e depois e exigem zero a mais; `testTirarDaLista` corta a sessão e o PC de
>   quem sai da lista. Falta o login com o Google de verdade (`docs/teste-manual-nuvem-f3.md`).
> - **AC-058 (nada de áudio nem de contexto na nuvem): VERDE NO SERVIDOR.** `testNaoMontadas` pede
>   `/asr/transcribe`, `/resolve`, `/updates`, `/settings`, `/dictionary`, `/corrections`, `/devices` e o
>   `POST /transcriptions` com sessão, com o token do PC e sem credencial, e recebe `404` em todas; o
>   `target_app` no payload já era recusado sem gravar (F2, `contracttest`).
> - **AC-059 (sair, sessões, o token fora do CRUD): VERDE NO SERVIDOR.** `testSairListarDerrubar`
>   (sair apaga no servidor; a lista; derrubar a do outro aparelho; a de outro usuário é `404`) e
>   `testEscopo`, que percorre a tabela de rotas: o token do PC recebe `403` em toda rota de sessão (o
>   CRUD, as sessões e o "apagar meus dados"), a sessão recebe `403` no sync, e o Bearer inválido é `401`
>   mesmo com um cookie válido. O painel que mostra isso ao celular é da F4.
> - **AC-035 (isolamento por usuário)** continua provado pela suíte de contrato, agora com a sessão por
>   cookie e o PC por Bearer, como na nuvem de verdade.
> - **Revisão da F3 (mesmo dia; ADR-019, "Revisão da F3").** Os mesmos critérios, com a prova mais
>   apertada: `testHost` percorre a tabela de rotas com três `Host` estranhos (antes, só o `GET /`;
>   isentar o `/api/v1/` ou o `/auth/` da guarda passava na suíte); `testNaoMontadas` cobre o `/` de
>   resto (sem ele, `POST /` dava `405`); `testStateEPre` prova que o `state` sem o `__Host-pre` certo
>   não queima o login de quem o começou; `testLimiteDoLogin` conta o IPv6 por `/64`; e, com a tag
>   `logindeteste`, o `serve` recusa `HTTP_ADDR` fora do loopback e a rota recusa quem não pede da
>   própria máquina.

> **Atualização de 06-out-2026 (ADR-019, fatias F7a e F7b).** O motor de sync antigo
> (`internal/sync/engine.go` e o resto do pacote) e os testes do cliente antigo em
> `internal/server/integration` (`harness_test.go`, `http_sync_client_test.go`, `local_mirror_test.go`,
> `sync_hard_cases_test.go`, `dictionary_sync_test.go` e o `sync_e2e_test.go` de antes) **saíram**: falavam
> o contrato da F1-D (sem `gone`, sem mescla, sem epoch), e o motor tinha os dois defeitos que a ADR-019
> apontou. As linhas da tabela acima que os citam valem como registro da Fase 1; a prova de hoje:
> - **AC-015, AC-016 (o lado do PC), AC-017, AC-033, AC-034 e NFR-006: VERDE DE PONTA A PONTA, contra a
>   nuvem de verdade na memória.** `internal/server/integration/sync_e2e_test.go` (o novo, com o palco em
>   `sync_harness_test.go`): dois PCs com o SQLite, a fila e o laço de sync de verdade, e o celular pela
>   sessão. O ditado do PC chega ao celular e ao outro PC; a edição do celular volta aos PCs; a lixeira,
>   o restaurar e o "excluir definitivamente" nos dois sentidos; o PC sem rede acumula e envia quando
>   ela volta; a resposta perdida e o reinício reenviam os mesmos `mutation_id` (`duplicate`, sem
>   duplicar). As decisões do PC têm as tabelas puras em `internal/sync/decisao_test.go`.
> - **AC-021 (o dicionário que sincroniza): volta a AUSENTE no sync.** O teste antigo exercitava o
>   envelope genérico do contrato que saiu; pela ADR-019 (decisão 7), só o aprendido sincroniza, e isso é
>   a F8.
> - **Os critérios da etapa 5, pelo lado do PC, na mesma suíte:** AC-054 (a edição do celular chega ao
>   PC, e a do PC ao celular), AC-056 (revogar dá `401`, o PC mostra "desconectado" e segue ditando),
>   AC-057 (o PC sem rede guarda e envia quando ela volta), AC-060 (o selo nos dois lados; campos
>   diferentes com um pull no meio chegam inteiros ao PC que pulou o item) e AC-061 (o `gone apagado`
>   apaga a cópia do PC, mesmo com uma edição pendente; o `410` relê do 0 e o epoch novo recomeça, nos dois sem duplicar;
>   na revisão de 06-out-2026, a volta do PC que ficou fora, pela reconexão ou pelo `410`, não cria selo
>   sem conflito, não troca o selo de verdade e não desfaz um restaurar mais novo:
>   `sync_reconexao_e2e_test.go`).
>   A AC-063 (a prévia e a cópia antes do envio inicial) é da F7c; "sem conectar, nenhuma conexão" é
>   `TestSyncE2E_DesconectadoZeroPedidos` e `TestSync_MontagemDoAgentNaoPassaDoPortao` (`cmd/agent`).
>   Tudo contra a nuvem em memória: os testes de Postgres continuam pendentes.

> **Atualização de 08-out-2026 (ADR-019, fatia F8).** O aprendido do painel sincroniza, e o AC-021 e o AC-022
> mudam de significado (os textos novos estão em `03-acceptance-criteria.md`). A prova de hoje:
> - **AC-021 (o dicionário que sincroniza), a parte do aprendido: VERDE DE PONTA A PONTA, contra a nuvem em
>   memória** (o Postgres rodou a suíte de contrato da nuvem em 07-out-2026, F8a a F8d). Só a troca do botão
>   Aprender sobe (D1); o resto do dicionário continua em cada PC, e a Correction com a frase inteira não
>   sobe. `internal/server/integration/sync_aprendido_e2e_test.go` (`TestAprendidoE2E_DoPC1AoPC2EAoCelular`,
>   `TestAprendidoE2E_D4ValeOPrimeiroEODesfazerVence`, `TestAprendidoE2E_EsquecerAVoltaEAContaNova`) e
>   `sync_aprendido_decisoes_e2e_test.go`: dois PCs com o SQLite, o `dicionario.json`, o aplicador e o
>   laço de sync de verdade, e o celular pela sessão. O par do PC 1 chega ao PC 2 e ao celular com a
>   restrição e o domínio; o "não aplicado neste PC" (`dominio_ausente`); o desfazer do celular some dos
>   dois; a D3 (nada aprendido sobe antes do `POST /api/nuvem/enviar`, e a pergunta aparece com N = 0); a D4
>   (vale o primeiro, o desfazer vence, as etiquetas); o esquecer, a volta e a conta nova; o
>   `dicionario.json` só com os campos de antes; o log sem texto. As regras da nuvem têm a suíte de
>   contrato (`internal/server/contracttest/learned.go`, na memória e no Postgres) e as do PC que
>   recebe, as tabelas de `internal/dictionary/lote_test.go` (`TestAplicarPares_TabelaDoPCQueRecebe`,
>   `TestAplicarPares_NuncaAlarga`).
> - **AC-022 (aprendizado exige confirmação): VERDE POR MOCK, lido de outro jeito.** A confirmação é o
>   clique no PC de origem (D2 da F8). Nos outros PCs o par entra sem clique, depois das mesmas
>   checagens do Aprender, e nunca passa a valer em mais lugares do que na origem. As provas de antes
>   (`TestConfirm_AC022_SemConfirmacaoNadaECriado`, `TestAC022_SalvarSugereENaoGravaNoDicionario`) valem
>   para o clique; o que chega da nuvem tem as tabelas do aplicador acima.
> - **TEST-008** ("n oito n" → "n8n") continua valendo para a biblioteca; a troca que sobe é o par do
>   Aprender (a variante e o termo), e não a Correction.
> - **O que falta:** o teste manual com o `bin\` (`docs/teste-manual-nuvem-f8.md`), que o Mauro roda; o
>   que é do Windows (a troca do dicionário no runtime, a inserção) fica nele, e não nos testes
>   automáticos.

> **Atualização de 11-out-2026 (ADR-021, subfatia C5c, `a8cf426`).** A C5c tirou do repositório o
> servidor em memória da F1-D inteiro (16 arquivos apagados de `internal/server`, 11 de código e 5 de
> teste, entre eles `asr_gateway.go`, `asr_gateway_test.go`, `isolation_test.go`, `server_test.go`,
> `sync_test.go` e `platform_test.go`; o `auth.go`, o `doc.go` e o `httpx.go` foram enxugados). As
> linhas da tabela acima que citam esses testes (AC-028, AC-035, AC-041, AC-042 e NFR-012) valem como
> registro da Fase 1. A prova de hoje é a que segue. A classificação não muda (continuam VERDE POR
> MOCK), e a prova passou a ser contra a nuvem de verdade (o `Cloud` de `internal/server`, na memória
> e num Postgres 18):
> - **AC-028 e AC-041 (o áudio não aparece em arquivo, banco nem log; o backend não grava áudio).**
>   Hoje o backend não transcreve: repassa a voz ao provedor da pessoa (`POST /api/v1/dictations`,
>   ADR-021). A prova é `chavestest.testDitarCanario` (`Ditar_Canario`, em
>   `internal/server/chavestest/ditar_prazos.go`; roda em `TestChaves_Memoria` e `TestChaves_Postgres`).
>   Um áudio sentinela e um texto sentinela passam por todos os desfechos (o ditado criado com e sem o
>   Polish, a falha do provedor em três status, o sem fala, o formato errado, o corpo grande demais e a
>   gravação que falha), e o teste varre o log de acesso e o log padrão, todas as respostas, o despejo
>   do banco e o diretório temporário: o áudio não está em nenhum deles, o texto não está no log nem
>   numa resposta de erro, e o diretório temporário não ganha arquivo. A chave e o corpo de erro do
>   provedor também não aparecem.
> - **AC-035 e NFR-012 (os dados da nuvem são isolados por usuário; o `user_id` vem da credencial, nunca
>   do corpo; sem credencial, `401`).** `contracttest.testIsolation` (o mesmo id em duas contas são duas
>   linhas; o CRUD e o push de B no item de A não passam; um `user_id` forjado no corpo é ignorado; o
>   mesmo `mutation_id` nas duas contas), `authtest.testEscopo` (a tabela de rotas inteira contra o
>   token do PC, a sessão e o Bearer inválido) e `authtest.testAparelhosEscopo`, todos na memória e no
>   Postgres (`TestContrato_StoreEmMemoria` e `TestContrato_Postgres`, `TestAuth_Memoria` e
>   `TestAuth_Postgres`).
> - **AC-042 (a API aceita Android).** `internal/server/cloudstore`:
>   `TestNewTranscription_OAppNativoTambemNaoTemAparelho` e `TestIsCloudPlatform` já cobriam o caminho do
>   app, e a C5c acrescentou `TestDecidePush_AceitaQualquerOrigemDeTextoLivre` (`windows`, `android`,
>   `web`, `ios`, `a` e um nome de 32 caracteres, aplicados pelas regras do push de verdade). O
>   `source_platform` continua sem enum fechado a `windows`.
> - **As rotas de saúde** (`/health/live` e `/health/ready` sem sessão, que estavam no `server_test.go`
>   apagado): `internal/server/health_test.go` (`TestHealthLive_Responde200SemDependencia` e
>   `TestHealthReady_SoDaOkSeTodosPassam`, que também prova que o erro de um checker não vai à
>   resposta). O `200` do `ready` contra o Postgres continua em `cmd/server/serve_pg_test.go`.
> - **O que não mudou:** `POST /asr/transcribe` e `POST /transcriptions` seguem `404` com qualquer
>   credencial e método, agora sem código atrás (`authtest.testNaoMontadas` e
>   `contracttest.testUnmountedRoutes`, na memória e no Postgres). A prova do AC-058 da nota de
>   05-out-2026 continua valendo.
