# Observability — Aurora Whisper

## Eventos locais
- agent start/stop;
- dictation state;
- microphone error;
- ASR start/completed/failed;
- resolve;
- insertion;
- sync;
- model checksum/download;
- update check.

Campos permitidos:
- IDs;
- provider/model;
- timings;
- status/error_code;
- Polish (ADR-016): `prompt@versão` com os 8 primeiros caracteres do hash, desfecho (aceito ou
  motivo do fallback), tamanho em caracteres e em tokens.

Sem transcript/context/audio. No Polish, também sem o texto enviado ou devolvido, sem os
`warnings` do modelo, sem o corpo de erro do provedor e sem a chave. Uma linha por ditado com
Polish; Literal e Smart não deixam linha de Polish (`09-security-privacy.md`, "Polish: o que sai
da máquina").

### A linha do Polish (ADR-016, fatia S4)
`winruntime: polish`, INFO, uma por ditado com o Polish escolhido — inclusive o curto (menos de 4
palavras, que nem vai ao provedor) e qualquer fallback. Exemplo:

```
level=INFO msg="winruntime: polish" attempt=12 prompt=polish-ptbr@1.0.2 prompt_hash=b0da9b12 provedor=openai modelo=gpt-6-luna esforco=none desfecho=aceito latencia_ms=1180 prazo_ms=3497 entrada_chars=214 saida_chars=219 tokens_entrada=852 tokens_saida=34 frases=0 frases_revertidas=0
```

| Campo | O que é |
|---|---|
| `attempt` | a tentativa, com a mesma chave das outras linhas do runtime |
| `prompt`, `prompt_hash` | `id@versão` do prompt e os 8 primeiros hex do sha256 dele |
| `provedor`, `modelo` | a escolha que valia no keydown (o modelo é o nome escolhido na tela) |
| `esforco` | o `reasoning.effort` mandado: `none`, `minimal`, `omitir` (sem o campo) ou `nao_se_aplica` (Anthropic) |
| `desfecho` | `aceito`, ou o motivo do fallback (enum de `18-tool-contract.md`, "PolishClient e Polir") |
| `latencia_ms` | do começo da chamada ao resultado (ou ao prazo) |
| `prazo_ms` | o que sobrava do teto quando a chamada começou (o Smart já saiu dele). O teto cresce com o texto: 3,5 s até 250 caracteres, mais 6 ms por caractere, até 8 s aos 1000 (ADR-016, emenda de 01-out-2026); o `prazo_ms` fica perto de 3500 + 6 × (caracteres − 250), até 8000 |
| `entrada_chars` | caracteres do texto do Smart |
| `saida_chars` | caracteres do texto que segue para a inserção: o do Polish se aceito, senão o do Smart |
| `tokens_entrada`, `tokens_saida` | o que o provedor informou; 0 quando a chamada não foi feita ou o prazo a cortou |
| `frases`, `frases_revertidas` | quando o Polish valeu **por frase** (a resposta inteira não passou, parte das frases sim; ADR-016, emenda de 19-out-2026): quantas frases a resposta tinha e quantas voltaram ao texto do Smart. 0 quando valeu inteiro ou não valeu |

Quando a política de esforço desce um degrau (`modelo_incompativel`), sai uma linha a mais, INFO,
`polish: o modelo recusou o esforço de raciocínio…`, com `provedor`, `modelo`, `recusado` e
`proximo`. O trabalho que ainda falta com o log real: conferir a taxa de fallback por motivo
(ADR-016, "Revisão").

### O ditado longo (01-out-2026, `docs/defeito-ditado-longo.md`)
- `winruntime: transcrição concluída` ganhou `audio`, a duração do áudio transcrito, ao lado de
  `duracao` (o tempo do ASR): a razão ASR/áudio sai da linha, e a duração diz se a janela do
  encoder foi a curta (até 12 s) ou a inteira.
- `winruntime: transcrição chegou depois do prazo; o texto ficou no histórico, sem inserir`, INFO:
  `attempt`, `runas` (tamanho do bruto), `espera` (do envio ao ASR até a chegada), `duracao_audio`,
  `modo` (`smart` ou `literal`). É a transcrição que estourou o prazo e foi guardada na linha da
  tentativa. Variantes WARN: `... sem texto` (o ASR voltou tarde com erro ou vazio: `reason`,
  `espera`), `... e não há histórico para guardá-la` e `... e não consegui gravá-la no histórico`.
- A linha `winruntime: dictation attempt finished` continua dizendo `outcome=timeout` para essa
  tentativa: o desfecho não muda, só a linha do histórico ganha o texto.
- `winruntime: laço de repetição do ASR removido`, WARN: `attempt`, `lacos` (quantos laços),
  `copias_removidas`, `palavras_da_unidade` (a maior), `palavras_removidas`, `runas_antes`,
  `runas_depois`. Só contagens: a frase repetida não vai ao log. A pílula do sucesso dessa
  tentativa sai com `motivo=repeticao_removida` na linha `winruntime: overlay`.
- O erro de um ditado com o microfone bloqueado pelo Windows (AC-080) sai na linha `winruntime: overlay`
  com `fase=error`, `motivo=microfone_bloqueado` e `bloqueio=<palavra>` (`empresa`, `aparelho`, `conta`,
  `programas` ou `este_app`), e a pílula mostra a mensagem do microfone no lugar de "Não consegui
  transcrever". Só a palavra: nenhum caminho, título nem texto.
- `permissao: o Windows bloqueia o microfone; o painel explica como liberar`, WARN, uma vez na subida do
  Agent, só quando o registro diz que há bloqueio: `motivo=<palavra>`. Liberado, ou "não deu para ler",
  não escreve nada.

## Etapa 5: a nuvem (ADR-019, 01-out-2026)
- **Log do servidor:** método, padrão da rota (`r.Pattern`, nunca a URL crua), status, duração e
  contagens. Nunca texto, busca, token, cookie, código de pareamento nem e-mail; conta recusada na
  lista: só "login recusado".
- **Log do laço de sync no Agent:** só contagens (enviados, aplicados, conflitos, `gone`, "não
  aplicado neste PC"), nunca o conteúdo dos ditados.
  **F7b (06-out-2026):** `sync: rodada` (só quando a rodada fez algo) com `enviados`, `aplicados`,
  `mesclados`, `conflitos`, `recusados` (`codigo:n`, de lista fechada), `reenviar`, `gone_apagados`,
  `gone_fora_da_janela`, `recebidos`, `aplicados_do_pull`, `pulados`, `ecos`, `nao_aplicados`,
  `paginas`, `recomecos`, `releituras` (o `410` do mesmo epoch), `reconexoes` (o cursor passado a um
  pareamento novo; revisão de 06-out-2026) e `pendentes`; a primeira falha de uma sequência em `WARN` (`sync: a rodada
  não terminou`, com o `codigo`: `rede`, `prazo`, `servidor`, `redirecionamento`, `corpo_grande`,
  `json`, `status`, `lote`, `cursor`, `espere`, `recomeco`, `local`), as seguintes em `DEBUG`, e a
  volta em `INFO` (`sync: a nuvem voltou a responder`); o `401` em `INFO`. Nunca texto, id de
  ditado, token, URL ou e-mail. O código da última falha fica também em `sync_state.ultimo_erro`
  (gravado só quando muda), e `last_success_at` só quando a rodada fez algo, quando a anterior falhou
  ou a cada 10 minutos (revisão de 06-out-2026: o PC parado não escreve no banco a cada busca).
  **F8 (07 e 08-out-2026; ADR-019, "Decisões da F8"):** o `sync: rodada` ganha as contagens do aprendido:
  `aprendido_marcados`, `aprendido_a_subir` (o M da pergunta do envio), `aprendido_reaplicados`,
  `aprendido_freio`, `aprendido_sem_dicionario`, `aprendido_aplicados`, `aprendido_desfeitos`,
  `aprendido_nao_aplicados`, `aprendido_nao_aplicados_por_motivo` (`motivo:n,...`, de uma lista
  fechada) e `aprendido_pulados`. O aplicador do Agent registra `aprendizado: pares da nuvem
  conferidos` (por situação e por motivo; em `INFO` só quando gravou, senão em `DEBUG`, porque a
  varredura tenta de novo a cada rodada o par que continua não valendo aqui) e, no caminho do painel,
  `aprendizado: dicionário atualizado` (`acao`, `termos`, `variantes_do_painel`, `termos_no_prompt`,
  `vocabulario_runas`). A subida registra, quando o `dicionario.json` é outro arquivo, `sync: o
  dicionário é outro arquivo…` (`criado_agora`, `pares`). **Nunca a variante, o termo, o domínio nem o
  usuário do dicionário.** O servidor não loga o aprendido além da linha de acesso de sempre (a rota
  e o status). As linhas e os campos estão no `06-api-contract.md`.
- `sync_mutations_total{status}` passa a ter também `gone`.
- **O job de retenção da nuvem (F9, 08-out-2026; ADR-019, "Decisões da F9"):** uma linha `INFO`
  `retenção` por rodada (na subida e de hora em hora), **só com contagens, mesmo com zero**:
  `usuarios` (os percorridos), `ativos` (apagados sem lápide), `lixeira` (apagados da lixeira),
  `lapides_criadas`, `lapides_vencidas`, `eventos` e `mutacoes` (a arrumação), `falhas` e `ms`.
  `falhas` conta cada usuário que deu erro **e** a falha da listagem dos usuários (aí sai
  `usuarios=0`, `falhas=1`); a linha `retenção` sai mesmo quando tudo falhou. Cada falha sai antes
  numa linha `ERROR` `retenção falhou`, com `etapa` (`usuarios`, a listagem; `usuario`, a retenção de
  um usuário) e `categoria`: `contexto` (o desligamento ou o prazo; a rodada para entre um usuário e
  outro), `banco` (com `operacao` e `sqlstate`, do `DBError`) ou `outro`. **Nunca** `user_id`, texto,
  e-mail, id de ditado nem o erro cru do driver. `retention_purged_total` passa a ser essa linha (o
  servidor não tem infraestrutura de métricas; o contador da lista "Métricas", abaixo, é alvo, e nada
  no código o emite hoje).
  **O alerta "retention job missed" (F11) é duplo** (a revisão da F9, 08-out-2026): dispara (1) na
  **ausência** da linha `retenção` por mais de 2 h (o job parou, ou o processo não está de pé); e
  (2) numa linha `retenção falhou` com `categoria` `banco` ou `outro` (o job roda e não apaga: o
  texto vencido fica retido além do prazo). A ausência sozinha não pega o job que falha toda hora,
  porque a linha `retenção` sai assim mesmo, com `falhas` acima de zero. A `categoria=contexto` fica
  de fora: é o desligamento no meio de uma rodada (um restart), e a rodada da subida seguinte refaz o
  trabalho. O batimento exige `LOG_LEVEL` em `info` ou `debug`: com `warn` ou `error`, a linha
  `retenção` some mesmo com o job saudável (a `retenção falhou`, que é `ERROR`, continua).
  **F11c (09-out-2026): o alerta sem ler log.** Ler o log de outro contêiner pediria o socket do
  Docker, que é raiz no servidor. A API guarda em memória a última rodada (quando terminou e quantas
  falhas graves teve) e a mostra na rota `GET /api/v1/health/retencao`, que só responde pelo loopback
  (`06-api-contract.md`); o `aurora-whisper-server healthcheck -retencao`, dentro do contêiner, sai com
  1 nas duas condições acima (mais de 2 h sem rodada; falha `banco` ou `outro` na última). Quem o usa
  é o `docker exec` do `subir.sh` e da conferência do mês (F11, Parte 2, abaixo); o `HEALTHCHECK` da
  imagem e o do compose são o de vida, para nada ser reiniciado onde o `unhealthy` reinicia nem sair
  do roteamento do Traefik. **A primeira rodada roda antes de a porta abrir**: o
  `serve` faz a arrumação e a retenção uma vez, em ordem, e só então o `net.Listen`; a linha `retenção`
  sai antes da `servindo`, e uma falha `banco` ou `outro` nessa rodada faz o `serve` sair com 1 sem
  abrir a porta. **[F11, Parte 2 (09-out-2026, refeito na revisão da Parte 2): o healthcheck da API
  no `deploy/compose.yml` é o de vida (`healthcheck`), e **não** o `-retencao`: o provider docker do
  Traefik tira do roteamento todo contêiner que não está `healthy`, e uma rodada da retenção com
  falha de banco tiraria o endereço do ar por até 1 h, com o log de acesso do Traefik registrando os
  pedidos sem router. O `healthcheck -retencao` roda por `docker exec` (`docker exec
  aurora-whisper-api /usr/local/bin/aurora-whisper-server healthcheck -retencao`): no `subir.sh`, a cada
  subida, e na conferência do mês (`docs/deploy-f11.md`, "Uma vez por mês"). O do backup é o
  `agendar-backup.sh saude` (`unhealthy` se o último backup que subiu ao B2 tem mais de 26 h). Sem
  canal de aviso na v1 (escolha menor 7 da F11): o estado do backup e o de vida da API aparecem no
  `docker ps` da VPS, e a conferência do mês é a rede de segurança do job. O `subir.sh` confere no log
  da API, na subida, a ordem `arrumação`, `retenção`, `servindo`.]**
- **O aviso do `X-Forwarded-For` (F11c).** Um `WARN` **único** por processo, `X-Forwarded-For de quem
  não é o proxy confiável...`, com só `proxy_confiavel` (um booleano), quando chega o cabeçalho de um
  `RemoteAddr` fora do `PROXY_CONFIAVEL`: é o sinal do valor errado (ou vazio com um proxy na frente),
  que põe todo mundo no mesmo balde do limite de login, em silêncio. Nunca o IP nem o valor. A linha
  `servindo` também diz `proxy_confiavel`.

## Métricas
- dictation_sessions_total
- dictation_errors_total{code}
- asr_latency_ms{provider}
- resolve_latency_ms{mode}
- release_to_insert_ms
- insertion_failures_total{method}
- sync_mutations_total{status}
- sync_outbox_depth
- cloud_requests_total
- cloud_request_latency_ms
- retention_purged_total
- provider_errors_total
- process_rss_bytes
- model_rss_bytes

## Alertas cloud
- DB unavailable;
- readiness fail;
- error rate;
- provider failures;
- disk low;
- backup fail;
- retention job missed;
- inference memory pressure.

## Health
- `/health/live`
- `/health/ready`
- `/health/retencao` (F11c: só pelo loopback; o `healthcheck -retencao`)

## Retenção logs
[ASSUMPTION]
- local 7–14 dias;
- cloud 14–30 dias.

## Runbooks
- microfone ausente;
- model load fail;
- OAuth expired;
- sync conflict;
- API down;
- migration fail;
- inference pressure;
- update checksum fail.
