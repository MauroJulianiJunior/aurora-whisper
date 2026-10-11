# PRD — Aurora Whisper

## 1. Problema
Digitação frequente gera atrito. O produto deve permitir falar em qualquer aplicação Windows e receber texto correto no campo focado, inclusive com termos técnicos, histórico e sincronização.

## 2. Objetivo
Entregar ditado rápido, contextual, local-first e cloud-connected, com ASR substituível e base multi-device.

## 3. Fora de escopo inicial
- [OUT-OF-SCOPE] Android completo (app nativo, teclado). **Emenda (ADR-019, 01-out-2026):** o
  celular entra na etapa 5 como **PWA instalável, Android primeiro**: o painel em modo nuvem, com
  histórico, busca, editar, apagar, lixeira e o aprendido (ver e desfazer). Ditar no celular continua
  fora.
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
| REQ-018 | Sync preferências compatíveis/devices. **Etapa 5 (ADR-019):** só os devices (lista, renomear, revogar). **Preferências: depois**, porque "herdar ao conectar" já é meio sync e a chave do Polish nunca sai do PC | Should |
| REQ-019 | Dicionário pessoal | Must |
| REQ-020 | Aprendizado apenas confirmado. **Etapa 5 (ADR-019, F8, D2, 07-out-2026):** a confirmação é o clique em **Aprender** no PC de origem. O par chega sozinho aos outros PCs, que refazem as checagens do Aprender e o aplicam sem novo clique, sem nunca alargar onde ele age; o que não vale ali fica "não aplicado neste PC". O celular só vê e desfaz | Must |
| REQ-021 | Modo Literal | Must |
| REQ-022 | Modo Smart default | Must |
| REQ-023 | Modo Polish opt-in | Should |
| REQ-024 | Contexto via UI Automation | Must |
| REQ-025 | Contexto nunca persistido/sincronizado | Must |
| REQ-026 | Áudio nunca persistido por default | Must |
| REQ-027 | ASR local provider | Must |
| REQ-028 | ASR cloud via backend. **Depois (ADR-019):** na etapa 5 o áudio não sai do PC, e a rota não é montada | Must |
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
| REQ-042 | Contratos preparados para Android | Must |
| REQ-043 | Cloud aceita áudio efêmero sem gravar. **Depois (ADR-019):** na etapa 5 o áudio não sai do PC; a rota `/asr/transcribe` não é montada, e um teste prova isso | Must |
| REQ-044 | Edição/exclusão sincroniza | Must |
| REQ-045 | Erro pede nova tentativa, sem retranscrever depois | Must |
| REQ-046 | Protected fields bloqueados por default | Must |
| REQ-047 | Exibir provider/modelo/latência no app | Should |
| REQ-048 | Celular por PWA instalável, Android primeiro: histórico, busca, editar, apagar, lixeira e o aprendido (ver e desfazer), no visual do painel (ADR-019) | Must |
| REQ-049 | A nuvem só aceita as contas da lista permitida (`CONTAS_PERMITIDAS`); outra conta Google é recusada sem criar nada (ADR-019) | Must |
| REQ-050 | Conectar o PC à nuvem é ação de quem usa; sem conectar, o Agent não abre conexão nenhuma; conectar e revogar sem perder o uso local (ADR-019) | Must |

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
- Provider secrets do projeto ficam no backend. **Exceção (ADR-016):** a chave de API do provedor de IA de quem usa (BYOK) fica no Gerenciador de Credenciais do Windows do Agent e só serve ao Polish.

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

Os alvos de latência valem para Literal e Smart. O Polish (ADR-016) tem orçamento próprio: corte em 3,5 s (até 8 s no ditado longo, pelo tamanho do texto), caindo para o Smart (AC-046).

## 8. Dependências
- Google OAuth credentials.
- DNS/TLS.
- VPS.
- ASR runtime após benchmark.
- Dataset de voz real.
- Revisão de LGPD antes do primeiro dado real na nuvem (ADR-019).
- Diagnóstico só de leitura da VPS (`scripts/diagnostico-servidor.sh`; ADR-019).

## 9. Perguntas abertas
- provider local vencedor;
- [DEPOIS, ADR-019] provider cloud (a transcrição na nuvem fica para depois da etapa 5);
- [DEPOIS, ADR-019] provider cloud do Smart (resolução contextual restrita);
- [DECIDIDO 29-set-2026, ADR-016] Polish: quem usa escolhe OpenAI ou Anthropic e cola a própria chave; o modelo sugerido sai do benchmark (a medir na S3).
- visibilidade do repo;
- [DECIDIDO 01-out-2026, ADR-019, com condição] VPS final: a atual, se o diagnóstico mostrar folga.
