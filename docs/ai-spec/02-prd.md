# PRD — Aurora Whisper

## 1. Problema
Digitação frequente gera atrito. O produto deve permitir falar em qualquer aplicação Windows e receber texto correto no campo focado, inclusive com termos técnicos, histórico e sincronização.

## 2. Objetivo
Entregar ditado rápido, contextual, local-first e cloud-connected, com ASR substituível e base multi-device.

## 3. Fora de escopo inicial
- [OUT-OF-SCOPE] Android completo (app nativo, teclado). **Emenda (ADR-019, 01-out-2026):** o
  celular entra na etapa 5 como **PWA instalável, Android primeiro**: o painel em modo nuvem, com
  histórico, busca, editar, apagar, lixeira e o aprendido (ver e desfazer). **Emenda (ADR-021,
  10-out-2026):** o **ditado pelo PWA** (passo 1) entra: um botão de microfone no app, a voz levada a
  texto no servidor pelo provedor que a pessoa escolhe, com a chave dela (REQ-051 a REQ-054). Seguem
  fora, para o passo 2 (outra ADR): o app nativo, a bolha flutuante que escreve em qualquer campo, o
  teclado, e ditar sem internet, com a tela apagada ou em segundo plano.
- iOS. **Depois do MVP (ADR-019):** o mesmo PWA no iPhone, com o login pelo código de 6 dígitos.
- Screenshot/OCR para contexto.
- Persistência de áudio.
- Billing.
- Colaboração multiusuário.
- Automação de mouse/ações por linguagem natural.
- Fine-tuning de modelo próprio.
- Deploy automático sem aprovação.

## 4. Requisitos

| ID | Requisito | Prioridade |
|---|---|---|
| REQ-001 | Hook global keydown/keyup | Must |
| REQ-002 | Tap curto preserva função normal | Must |
| REQ-003 | Hold ativa ditado; threshold configurável | Must |
| REQ-004 | Modificadores puros não podem ser tecla principal | Must |
| REQ-005 | Bufferizar áudio desde keydown | Must |
| REQ-006 | Overlay com onda/duração/estado | Must |
| REQ-007 | Beep configurável | Should |
| REQ-008 | VAD/partial validation durante gravação | Must |
| REQ-009 | Transcrever ao soltar | Must |
| REQ-010 | Inserir no campo focado | Must |
| REQ-011 | Preservar/restaurar clipboard | Must |
| REQ-012 | Dashboard local loopback | Must |
| REQ-013 | Histórico com busca/copiar/editar/excluir | Must |
| REQ-014 | Dashboard cloud no domínio definido | Must |
| REQ-015 | Google OAuth cloud | Must |
| REQ-016 | Sync histórico | Must |
| REQ-017 | Sync dicionário/aliases/correções. **Etapa 5 (ADR-019):** sincroniza só o aprendido no painel, o par curto com a restrição e o domínio; o `dicionario.json` continua a fonte em cada PC, e a Correction com a frase inteira não sobe. O dicionário completo no banco fica para depois | Must |
| REQ-018 | Sync preferências compatíveis/devices. **Etapa 5 (ADR-019):** só os devices (lista, renomear, revogar). **Preferências: depois**, porque "herdar ao conectar" já é meio sync e a chave do Polish **do PC** nunca sai do PC. **ADR-021:** as preferências do ditado no celular são novas, moram só na nuvem e nunca descem aos PCs | Should |
| REQ-019 | Dicionário pessoal | Must |
| REQ-020 | Aprendizado apenas confirmado. **Etapa 5 (ADR-019, F8, D2, 07-out-2026):** a confirmação é o clique em **Aprender** no PC de origem. O par chega sozinho aos outros PCs, que refazem as checagens do Aprender e o aplicam sem novo clique, sem nunca alargar onde ele age; o que não vale ali fica "não aplicado neste PC". O celular só vê e desfaz | Must |
| REQ-021 | Modo Literal | Must |
| REQ-022 | Modo Smart default | Must |
| REQ-023 | Modo Polish opt-in | Should |
| REQ-024 | Contexto via UI Automation | Must |
| REQ-025 | Contexto nunca persistido/sincronizado | Must |
| REQ-026 | Áudio nunca persistido por default | Must |
| REQ-027 | ASR local provider | Must |
| REQ-028 | ASR cloud via backend. **Depois (ADR-019):** na etapa 5 o áudio não sai do PC, e a rota não é montada. **Emenda (ADR-021):** o áudio **do PC** continua no PC, e `/asr/transcribe` continua não montada. O ditado **do celular** ganha uma rota nova, `POST /api/v1/dictations`, em que o servidor só repassa a voz ao provedor que a pessoa escolheu (OpenAI ou Groq, por trás do `ASRProvider`), com a chave dela; ele não transcreve | Must |
| REQ-029 | Policies local/local+cloud/cloud. **Depois (ADR-019):** só `local_only` existe enquanto não há ASR na nuvem | Must |
| REQ-030 | Model manager local | Should |
| REQ-031 | Benchmark ASR | Must |
| REQ-032 | SQLite local | Must |
| REQ-033 | PostgreSQL cloud | Must |
| REQ-034 | Operação local offline | Must |
| REQ-035 | Retenção 6 meses | Must |
| REQ-036 | Lixeira 30 dias | Must |
| REQ-037 | Multiusuário no schema | Must |
| REQ-038 | System tray | Must |
| REQ-039 | Autostart configurável | Must |
| REQ-040 | Installer Windows | Must |
| REQ-041 | Update com confirmação. **A parte da nuvem (`/updates/windows/latest`) fica para depois (ADR-019)**; nas 0.x a atualização é manual (ADR-018) | Should |
| REQ-042 | Contratos preparados para Android. **ADR-021:** a rota `/dictations`, o contrato (corpo cru, id do cliente, idempotência) e os códigos de recusa estáveis em `error.code` já servem ao app nativo do passo 2 | Must |
| REQ-043 | Cloud aceita áudio efêmero sem gravar. **Depois (ADR-019):** na etapa 5 o áudio não sai do PC; a rota `/asr/transcribe` não é montada, e um teste prova isso. **Emenda (ADR-021):** o que a nuvem aceita é o áudio do **celular**, na rota `/dictations`: só em memória, durante o pedido, descartado ao fim, nunca em disco, swap, log, erro ou resposta (AC-068). `/asr/transcribe` e `POST /transcriptions` continuam `404` (AC-058) | Must |
| REQ-044 | Edição/exclusão sincroniza | Must |
| REQ-045 | Erro pede nova tentativa, sem retranscrever depois | Must |
| REQ-046 | Protected fields bloqueados por default | Must |
| REQ-047 | Exibir provider/modelo/latência no app | Should |
| REQ-048 | Celular por PWA instalável, Android primeiro: histórico, busca, editar, apagar, lixeira e o aprendido (ver e desfazer), no visual do painel (ADR-019) | Must |
| REQ-049 | A nuvem só aceita as contas da lista permitida (`CONTAS_PERMITIDAS`); outra conta Google é recusada sem criar nada (ADR-019) | Must |
| REQ-050 | Conectar o PC à nuvem é ação de quem usa; sem conectar, o Agent não abre conexão nenhuma; conectar e revogar sem perder o uso local (ADR-019) | Must |
| REQ-051 | Ditar pelo celular (PWA, Android primeiro; ADR-021, passo 1): um botão de microfone no app, toque para começar e toque para parar; a voz vai ao servidor, que a leva a texto pelo provedor que a pessoa escolheu (OpenAI ou Groq), com a chave dela, passa pelo Smart da conta e, se a pessoa quiser, pelo Polish; o texto entra no histórico, desce aos PCs pelo sync e tem "Copiar". O servidor só repassa a voz: não transcreve | Must |
| REQ-052 | O ditado do celular só começa depois de um aceite específico, em destaque e separado das outras finalidades, de quem dita: o que vai (a voz; com a dica, os termos do aprendido; com o Polish, o texto), para quem (o provedor escolhido e o do Polish), para onde e por quanto tempo fica lá. O aceite é gravado no servidor, por conta, e pode ser revogado a qualquer momento (ADR-021, decisão H) | Must |
| REQ-053 | A chave de API da pessoa para ditar no celular fica **cifrada** no servidor (a chave do PC continua só no PC); o app só vê "configurada" e o fim dela; nunca em resposta, log, erro ou URL; a recomendação na tela é uma chave só para o celular, com limite de gasto no provedor (ADR-021, decisão G) | Must |
| REQ-054 | A chegada ao PC mais rápida (ADR-021, decisão P e subfatia S): com o painel do PC aberto e na frente, o Agent busca a nuvem a cada 10 s (60 s fechado), e o painel relê a lista quando um pull traz mudança | Should |

## 5. Regras
- Smart é default.
- Áudio e contexto são efêmeros.
- Login não é necessário localmente.
- Cloud exige login. Só entram as contas da lista permitida (ADR-019).
- Conectar o PC à nuvem é escolha de quem usa; sem isso, nenhum texto ditado sai do PC além do
  Polish (ADR-019).
- Cloud consolida dados sincronizados.
- Delete manual → trash 30d.
- Histórico ativo → purge ao atingir 6 meses.
- Provider secrets do projeto ficam no backend. **Exceção (ADR-016):** a chave de API do provedor de IA de quem usa (BYOK) fica no Gerenciador de Credenciais do Windows do Agent e só serve ao Polish. **Exceção (ADR-021):** para ditar no celular, a pessoa cola uma chave no app, e o servidor da nuvem a guarda **cifrada**, só depois do aceite dela; a chave do PC continua só no PC.
- O áudio do PC nunca sai do PC. O áudio do celular só existe em memória (no app e na API) até o provedor responder, e é descartado ao fim; vai ao provedor que a pessoa escolheu, depois do aceite específico dela (ADR-021).
- Sem o aceite vigente do ditado no celular, o microfone do app não abre, a chave não é pedida e o servidor recusa o ditado (ADR-021).

## 6. User stories
- US-001: segurar tecla e ditar em qualquer app.
- US-002: tap curto continua digitando normalmente.
- US-003: visualizar onda/duração.
- US-004: termos técnicos reconhecidos pelo dicionário.
- US-005: usar contexto para ambiguidades.
- US-006: revisar histórico.
- US-007: ensinar correções.
- US-008: acessar histórico no Windows/web/futuro Android (na etapa 5, o celular Android pelo PWA; ADR-019).
- US-009: continuar funcionando offline.
- US-010: escolher local/cloud/fallback.
- US-011: ditar pelo celular, tocando num botão de microfone no app, e ver o texto no histórico e nos PCs (ADR-021).
- US-012: escolher o provedor de transcrição (OpenAI ou Groq), colar a chave uma vez e poder desligar o ditado no celular a qualquer hora, apagando a chave do servidor (ADR-021).

## 7. Métricas iniciais
| Métrica | Meta |
|---|---|
| release→insert local p50 | ≤500 ms em falas 1–10 s no Dell de referência |
| release→insert local p95 | ≤1.2 s |
| ASR local working set | ~≤1 GB |
| Agent base | alvo ≤250 MB |
| Entity Accuracy técnica | ≥95% após dicionário no dataset aprovado |
| False Correction Smart | ≤1% |
| Sync confirmado perdido | 0 |
| Celular: do toque final até o texto (mediana) | até ~4 s, **a confirmar na C7** (ADR-021: a estimativa é de 2 a 4 s; o pior caso, de uns 65 s, vem dos prazos). Depende de internet e do provedor, e fica fora dos alvos de latência locais |

Os alvos de latência locais valem para Literal e Smart no PC. O Polish (ADR-016) tem orçamento próprio: corte em 3,5 s (até 8 s no ditado longo, pelo tamanho do texto), caindo para o Smart (AC-046).

## 8. Dependências
- Google OAuth credentials.
- DNS/TLS.
- VPS.
- ASR runtime após benchmark.
- Dataset de voz real.
- Revisão de LGPD antes do primeiro dado real na nuvem (ADR-019).
- Diagnóstico só de leitura da VPS (`scripts/diagnostico-servidor.sh`; ADR-019).
- Para o ditado no celular (ADR-021): o OK do Mauro na política e no dossiê reescritos (D0) antes do primeiro áudio real sair do aparelho; a leitura do proxy e da swap da VPS antes desse áudio; a chave de cifra gerada por ele na VPS.

## 9. Perguntas abertas
- provider local vencedor;
- [DEPOIS, ADR-019] provider cloud (a transcrição na nuvem fica para depois da etapa 5). **[DECIDIDO 10-out-2026, ADR-021]** para o **celular**: a pessoa escolhe OpenAI ou Groq e usa a chave dela (BYOK); o servidor só repassa a voz. Para o PC, a transcrição continua local;
- [DEPOIS, ADR-019] provider cloud do Smart (resolução contextual restrita);
- [DECIDIDO 29-set-2026, ADR-016] Polish: quem usa escolhe OpenAI ou Anthropic e cola a própria chave; o modelo sugerido sai do benchmark (a medir na S3).
- visibilidade do repo;
- [DECIDIDO 01-out-2026, ADR-019, com condição] VPS final: a atual, se o diagnóstico mostrar folga.
