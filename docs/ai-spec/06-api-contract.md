# API Contract — Aurora Whisper Cloud

> A API **local** do Windows Agent — a que o painel usa em loopback — está no fim deste
> documento, em **API local (Windows Agent)**. Ela não é a cloud: não tem `/api/v1`, nem OAuth,
> nem cookie.

## Convenções
Base: `https://exemplo.com/api/v1` (o domínio real vem de `PUBLIC_BASE_URL`)

- JSON UTF-8, exceto áudio.
- Datas RFC3339 UTC.
- IDs UUID.
- `user_id` deriva da autenticação.
- Mutations idempotentes.
- `version` para optimistic concurrency.

**Etapa 5 (ADR-019, 01-out-2026).** Valem também:
- `Cache-Control: no-store` em toda resposta de `/api/v1`. Nenhum cabeçalho de CORS.
- Proteção contra CSRF por `http.CrossOriginProtection` em todo pedido que não é `GET` nem `HEAD`
  (só `Sec-Fetch-Site` `same-origin` ou `none`). Nenhum `GET` muda estado, fora o callback do Google.
- Com `Authorization: Bearer`, o cookie é ignorado; Bearer inválido é `401`, sem cair no cookie.
- Texto ditado nunca vai na URL: a busca é por `POST` (abaixo).
- Log do servidor: método, padrão da rota, status, duração e contagens. Nunca a URL crua, a query, o
  corpo, token, cookie ou e-mail.
- Limite de tentativas no login e no pareamento: `429`.
- **Rotas que não são montadas na etapa 5** (um teste pede cada uma e recebe `404`):
  `POST /asr/transcribe`, `POST /resolve`, `GET /updates/windows/latest`, `GET`/`PUT /settings`,
  `/dictionary` (todas), `POST /corrections` e `POST /transcriptions`. Elas ficam descritas abaixo,
  marcadas "depois".
- **F3 (05-out-2026), o que o servidor faz** (`internal/server`, `Cloud`; ADR-019, "Decisões da F3"):
  - **Quem pode cada rota** sai de uma tabela só (`internal/server/rotas.go`): `sessão` (o cookie
    `__Host-sessao`), `aparelho` (o Bearer do PC), `qualquer` ou `público`. Sem credencial, `401`;
    credencial do tipo errado, `403`. O e-mail do usuário tem de estar em `CONTAS_PERMITIDAS` em toda
    credencial: fora da lista, `401`.
  - **O que não está na tabela é `404`, com qualquer método** (os padrões `/api/v1/`, `/auth/` e `/`
    pegam o resto): `POST /transcriptions` deixa de dar `405`, e um `OPTIONS` também é `404`, sem
    `Access-Control-*`. `/devices` fica sem montar até a F6 (**F6:** `GET` e `DELETE /devices` montados;
    o `PATCH /devices/{id}`, o renomear, continua sem montar).
  - **Cabeçalhos em toda resposta:** `Content-Security-Policy: default-src 'self'; base-uri 'none';
    object-src 'none'; frame-ancestors 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`,
    `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy: same-origin`; `Strict-Transport-Security:
    max-age=31536000` só com `PUBLIC_BASE_URL` em `https`; `Cache-Control: no-store` em `/api` e `/auth`.
  - **Host:** um `Host` diferente do de `PUBLIC_BASE_URL` é `400` (`host inválido`), menos em
    `/api/v1/health/*`.
  - **Origem cruzada:** `POST`, `PATCH`, `DELETE` com `Sec-Fetch-Site` `cross-site` ou `same-site`, ou com
    um `Origin` de outro host, é `403` (`http.CrossOriginProtection`). O PC (sem esses cabeçalhos) passa.
  - **Limite de tentativas:** `429` com `Retry-After` (segundos), código `TOO_MANY_REQUESTS`. No login,
    10 por minuto por IP (o `/auth/start` e o callback juntos; o IPv6 conta por `/64`) e 100 por minuto
    no `/auth/start` de todos; no push, 60 pedidos e 5.000 mutações por minuto por aparelho. Em memória:
    reiniciar zera. O pull, o CRUD, o `/me` e as sessões não têm limite de taxa (ADR-019, "Decisões da
    F3", riscos).

Erro:
```json
{
  "error": {
    "code": "CONFLICT",
    "message": "Entity version is stale",
    "request_id": "req_..."
  }
}
```

## Auth

### Web
Google OAuth + cookie `HttpOnly`, `Secure`, `SameSite`.

### Device
Bearer token opaco salvo no Windows Credential Manager.

### POST `/device-auth/start`
Cria challenge de pareamento.

Request:
```json
{
  "device_id": "uuid",
  "install_id": "opaque-install-id",
  "device_name": "Dell G15",
  "platform": "windows",
  "challenge": "base64url"
}
```

Response:
```json
{
  "auth_url": "https://exemplo.com/device/authorize?...",
  "poll_token": "opaque",
  "expires_in": 600
}
```

### POST `/device-auth/exchange`
Troca autorização concluída por device token.

### Como fica na etapa 5 (ADR-019, 01-out-2026)

**Web: login Google.** Fluxo de código no servidor, cliente Web confidencial, escopos `openid email
profile`, com `state`, `nonce` e PKCE (S256). Fora de `/api/v1`, na mesma origem:
- `POST /auth/start` (por `fetch`, do app): grava o cookie `__Host-pre` (5 min, `HttpOnly`,
  `Secure`, `SameSite=Lax`, uso único) e devolve `{ "url": "<endereço do Google>" }`. O `state`, o
  verificador e o `nonce` ficam no servidor. Corpo opcional: `{ "volta": "/caminho" }`, para onde o
  callback leva a pessoa no fim (por exemplo, `/device/authorize?pedido=…`, que exige sessão); sem
  ele, `/`. É um **caminho** da própria origem, nunca uma URL: o servidor recusa o que não começa com
  `/`, o que começa com `//` e o que tem barra invertida, espaço ou quebra de linha (`400`), e o
  banco confere de novo (`pending_logins.return_to`). **[proposta da F1; a F3 a implementou como
  está, com a mesma regra no servidor e no banco, e também recusa `#`.]**
- `GET /auth/google/callback?code&state` (o Google redireciona para cá; é `PUBLIC_BASE_URL` +
  `/auth/google/callback`, que tem de estar nos `redirect_uris` do client; F3):
  confere o `state` e o cookie `pre`, troca o código, valida o ID token (`iss`, `aud`, `exp`,
  `nonce`) e exige `email_verified`. Só cria sessão para e-mail de `CONTAS_PERMITIDAS`, com o `sub`
  fixado no primeiro login; fora da lista, recusa sem criar nada. Termina redirecionando para o app.
- `POST /auth/logout`: apaga a sessão no servidor e o cookie.
- Sessão: cookie `__Host-sessao` (`HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, sem `Domain`),
  id de 256 bits trocado a cada login; 30 dias sem uso (sugestão).
- **Depois do MVP (iPhone):** se o cookie `pre` não voltar no callback, a página mostra um código de 6
  dígitos que o app troca pela sessão. As rotas desse caminho são da fatia que trouxer o iPhone.
- **Como a F3 fechou o login (05-out-2026; ADR-019, "Decisões da F3").** Os nomes da D6 ficam.
  - `POST /auth/start`: corpo de até 1 KiB, opcional (`{}` ou nenhum). A `volta` inválida é `400`, sem
    cookie nem linha no banco. Resposta `200` `{ "url": "https://accounts.google.com/o/oauth2/v2/auth?..." }`
    com `response_type=code`, `scope=openid email profile`, `state`, `nonce`, `code_challenge` (S256),
    `prompt=select_account` e `redirect_uri` = `PUBLIC_BASE_URL` + `/auth/google/callback`. O cookie:
    `__Host-pre=<256 bits>; Path=/; Max-Age=300; HttpOnly; Secure; SameSite=Lax`.
  - `GET /auth/google/callback`: apaga o `__Host-pre` sempre. Dá certo: `303` para a `volta` (ou `/`),
    com `__Host-sessao=<256 bits>; Path=/; Max-Age=7776000; HttpOnly; Secure; SameSite=Lax` (sem
    `Domain`). Não dá: uma **página fixa** (`text/html`), que não ecoa nada do pedido: `403` "Esta conta
    não tem acesso" (fora da lista, o mesmo e-mail com outro `sub`, e-mail não verificado, o e-mail novo
    de uma conta já ser de outro usuário, conta desativada; nada é criado); `400` "Não deu para entrar"
    (`state` desconhecido, já usado ou vencido, `__Host-pre` ausente ou de outro login, cancelado no
    Google com `error=`, ID token inválido); `502` (o endpoint de token falhou); `429`. O hash do
    `__Host-pre` faz parte da busca do login pendente: o `state` certo com o `pre` ausente ou errado é
    recusado **sem consumir** o login, e quem o começou ainda entra (revisão da F3).
  - `POST /auth/logout`: `204`, com ou sem sessão; apaga a sessão no servidor e o cookie.
  - A sessão vence em 30 dias sem uso e nunca passa de 90 dias desde o login (o `Max-Age` do cookie é
    o teto). Uma sessão vencida ou apagada recebe `401` e o cookie apagado.
  - **Login de teste** (entrar sem o Google, para desenvolvimento): `POST /auth/teste` com
    `{ "email": "..." }` de `CONTAS_PERMITIDAS`, `204` com a mesma sessão. **Só existe num binário
    compilado com a build tag `logindeteste`**, que só sobe com `PUBLIC_BASE_URL` em `localhost` e
    `HTTP_ADDR` no loopback (`127.0.0.0/8`, `::1` ou `localhost`), e a rota responde `404` a quem não
    pede da própria máquina (um `RemoteAddr` fora do loopback, ou um pedido com `X-Forwarded-For` ou
    `Forwarded`). Sem a tag, é `404`. Nenhuma variável o liga. O primeiro login de teste com um e-mail
    fixa nele o `sub` `teste:<e-mail>`, e o login Google desse e-mail, no mesmo banco, passa a ser
    recusado (D7): use um e-mail da lista que não é o do Google, ou outro banco.

**Device: retorno a `127.0.0.1` com PKCE (RFC 8252, §7.3).** O desenho acima, com `poll_token`, é
consulta repetida e phishável (RFC 8628, §5.4); sai.
- `POST /device-auth/start` (sem sessão; limite de tentativas). Request: `install_id`,
  `device_name`, `platform`, `app_version`, `code_challenge` (S256) e `redirect_port` (a porta do
  painel local, só de 49152 a 65535, a faixa dinâmica do Windows; fora dela, `400`). Response: `{ "pedido": "<uuid>", "auth_url":
  "https://exemplo.com/device/authorize?pedido=…", "conferencia": "4 caracteres", "expires_in": 600 }`.
  Sem `poll_token`. O `pedido` é o `id` do pedido e é o **`state`** do retorno abaixo: o Agent o guarda,
  e o painel local confere que o `state` de `/nuvem/retorno?code&state` é o do pedido em curso. Quem
  protege a troca do `code` é o PKCE; o `state` só liga o retorno ao pedido. **[proposta da F1; a F6
  confirma.]**
- `GET /device/authorize?pedido=…` (página, exige sessão): mostra o nome do PC e o código de
  conferência, e avisa "Só aprove se você clicou em Conectar neste PC agora".
- `POST /device/authorize` (sessão + CSRF): aprova e responde `303` para
  `http://127.0.0.1:<redirect_port>/nuvem/retorno?code=…&state=…`. Recusar não redireciona.
  **[Substituído na F6, abaixo: aprovar é `POST /api/v1/device-auth/requests/{pedido}/approve` por
  `fetch`, que devolve o endereço em JSON, e o app navega com `location.replace`. O Chromium aplica o
  `form-action 'self'` da CSP ao redirecionamento de um formulário; assim a CSP fica como está.]**
- `POST /device-auth/exchange` (sem sessão). Request: `code` e `code_verifier`. Response:
  `{ "device_id": "uuid", "token": "opaco, 256 bits" }`. Código de uso único, que vence em minutos.
  A nuvem guarda só o HMAC do token (`DEVICE_TOKEN_PEPPER`).
- **Escopo do token:** `/sync/*`, `GET /me` e o próprio aparelho (`PATCH` e `DELETE
  /devices/{o próprio id}`). Qualquer outra rota com o token do PC é `403`.

**Como a F6 fechou o pareamento (05-out-2026; ADR-019, "Decisões da F6").** O `state` é o `pedido`
(a proposta da F1, confirmada). Todas sob `/api/v1`, na tabela de rotas, com o `Host`, o CSRF, os
cabeçalhos e o log de sempre (só o padrão da rota: nunca o `code`, o `state`, o pedido, o verificador,
a conferência nem o token).

| Rota | Acesso | Resposta |
|---|---|---|
| `POST /device-auth/start` | público; 10 por minuto por IP (junto com o `/exchange`) e 100 por minuto de todos | `200` `{ "pedido", "auth_url", "conferencia", "expires_in": 600 }`; `400` (corpo até 1 KiB, sem campo desconhecido: `install_id` UUID, `device_name` de 1 a 80 caracteres sem controle, `platform` `windows`, `app_version` até 64 ASCII visíveis, `code_challenge` de 43 base64url, `redirect_port` de 1024 a 65535; nada é gravado); `429` |
| `GET /device-auth/requests/{pedido}` | sessão | `200` `{ "nome", "plataforma", "versao", "conferencia", "expira_em" }`; vencido, usado, já aprovado ou desconhecido: o mesmo `404` |
| `POST /device-auth/requests/{pedido}/approve` | sessão (e o CSRF) | `200` `{ "retorno": "http://127.0.0.1:<redirect_port>/nuvem/retorno?code=<43 base64url>&state=<pedido>" }`; aprovar de novo ou depois do prazo: `404`. Grava quem aprovou e o SHA-256 do `code` |
| `POST /device-auth/exchange` | público; o limite por IP | `200` `{ "device_id", "token", "conta": { "id", "email" } }`; tudo o que não vale (o `code` desconhecido, usado, vencido ou de pedido não aprovado, o verificador que não dá o desafio, a conta fora da lista ou desativada) é o mesmo `400` `CODIGO_INVALIDO` |
| `GET /devices` | sessão | `{ "items": [ { "id", "nome", "plataforma", "versao", "conectado_em", "ultimo_uso_em" } ] }`: só os da conta, não revogados e com o token valendo (180 dias), o usado por último primeiro |
| `DELETE /devices/{id}` | sessão ou token | `204`. Com a sessão, qualquer aparelho da conta; com o token, só o próprio (outro é `403`). De outro usuário, inexistente, já revogado ou id que não é UUID: `404` |
| `GET /device/authorize` | público | o `index.html` do app (a página pede a sessão; sem ela, Entrar, que volta a ela) |

- **A troca é de uso único e o desafio fica na busca:** o `UPDATE ... SET used_at` acha a linha pelo
  hash do `code` **e** pelo desafio (`code_challenge = S256(code_verifier)`), aprovada, sem `used_at` e
  sem vencer. Um verificador errado não acha a linha e **não queima o `code`**: o certo ainda troca
  (o link copiado não conecta e não derruba quem começou).
- **O aparelho:** o mesmo de antes por `(user_id, install_id)` (parear de novo depois de revogar o
  reaproveita: `revoked_at` volta a nulo e a `version` sobe), um token por aparelho (o de antes sai e
  passa a receber `401`). A nuvem guarda só o HMAC do token com `DEVICE_TOKEN_PEPPER`.
- **Revogar** grava `revoked_at` e apaga o token; o aparelho fica (os ditados da F7 apontam para ele).
- **180 dias sem uso** (`COALESCE(last_used_at, created_at)`): o token deixa de valer (`401`) e o
  aparelho sai da lista. A arrumação de hora em hora apaga os pedidos vencidos.
- O `auth_url` vai na resposta por compatibilidade; o Agent não o usa: monta o endereço da página com
  a URL que a pessoa digitou.

## Me
### GET `/me`

**Etapa 5 (ADR-019):** com sessão ou com o token do PC. Devolve o e-mail, o nome e, com o token, o
aparelho. **F3:** `{ "id": "uuid", "email": "...", "display_name": "...", "aparelho": { "id": "uuid",
"nome": "...", "plataforma": "windows" } }`; `aparelho` só com o token, e `display_name` só quando o
Google o deu.

### DELETE `/me/data`
"Apagar meus dados da nuvem" (ADR-019). **Só com sessão** (nunca com o token do PC) e com
`{ "confirmar": "apagar" }` no corpo. Apaga tudo da conta na nuvem, troca o `sync_epoch`, tira o
acesso de todos os aparelhos e encerra as outras sessões. Os PCs ficam com a cópia deles e passam a
mostrar "desconectado" (o token deles recebe `401`). `204`.
**F3:** sem `"confirmar": "apagar"` (ou com corpo quebrado, ou acima de 1 KiB), `400` e nada muda; com
o token do PC, `403`. Numa transação só: troca o `sync_epoch`, sobe a marca d'água das lápides ao
`last_seq` (o `seq` continua; não volta a 0; a marca não tem leitor desde a F9, D2, mas o `CHECK
(tombstone_watermark <= last_seq)` vale), apaga os ditados, as lápides, os eventos, as mutações, o
aprendido, as correções, o dicionário, as preferências, os pedidos de pareamento aprovados pela conta e
as outras sessões. **Os aparelhos são apagados, com os tokens**, e não ficam como revogados
(`revoked_at`): o efeito para o PC é o mesmo `401`, mas eles saem da lista de aparelhos da conta (a
da F6), em vez de ficarem nela como revogados. Ficam o usuário, o contador e a sessão de quem pediu. **[Decisão 3
da F3; confirmada pelo Mauro em 05-out-2026.]** A alternativa é manter os aparelhos com `revoked_at` (a palavra da
D2) e encerrar também a sessão atual (ADR-019, "Decisões da F3").

## Sessions (ADR-019)
### GET `/sessions`
As sessões abertas da conta: id, rótulo curto do aparelho, criada, último uso e qual é a atual. Só
com sessão.
**F3:** `{ "items": [ { "id": "uuid", "rotulo": "Android · Chrome", "criada_em": "...",
"ultimo_uso_em": "...", "atual": true } ] }`, a usada por último primeiro. O rótulo sai do
User-Agent, de listas fechadas de sistema e navegador; nunca o User-Agent inteiro.
### DELETE `/sessions/{id}`
Derruba uma sessão (o celular perdido). `204`.
**F3:** a sessão de outro usuário, uma que não existe ou um id que não é UUID: `404` (o mesmo erro).
Derrubar a própria sessão também apaga o cookie.

## Devices
### GET `/devices`
### PATCH `/devices/{id}`
### DELETE `/devices/{id}`

**Etapa 5 (ADR-019):** `GET` e revogar outro aparelho só com sessão. `DELETE` revoga: o token do
aparelho passa a receber `401`. Com o token do PC, só o próprio aparelho (renomear e desconectar).
**F6:** o `GET` e o `DELETE` estão na tabela da seção "Como a F6 fechou o pareamento", acima; o
`PATCH` (renomear) fica para depois e é `404`.

## Transcriptions

### GET `/transcriptions`
Query:
- cursor;
- limit max 100;
- q;
- from/to;
- device_id;
- mode;
- trash.

### POST `/transcriptions`
```json
{
  "id": "uuid",
  "device_id": "uuid",
  "source_platform": "windows",
  "raw_text": "texto",
  "final_text": "texto",
  "mode": "smart",
  "language": "pt-BR",
  "asr_provider": "local",
  "asr_model": "model-id",
  "duration_ms": 3200,
  "processing_ms": 410,
  "confidence": 0.91,
  "target_app": "chrome",
  "created_at": "2026-09-16T18:00:00Z"
}
```

### PATCH `/transcriptions/{id}`
```json
{
  "base_version": 4,
  "final_text": "texto editado"
}
```

### DELETE `/transcriptions/{id}`
Move para trash.

### POST `/transcriptions/{id}/restore`
Restaura antes de purge.

### Como fica na etapa 5 (ADR-019, 01-out-2026)
Todas só com sessão (o token do PC não alcança o CRUD).
- **`target_app` sai:** não está no item nem é aceito em lugar nenhum (é contexto, REQ-025).
- **`GET /transcriptions`** fica **sem `q`** (um `q` na URL é `400`): cursor, limit (máx. 100),
  from/to, device_id, mode, trash.
- **`POST /transcriptions/search`**: a busca, com o texto no corpo, para ele nunca parar num log de
  acesso. Corpo: `q` e os mesmos filtros do `GET`. Mesma resposta do `GET`.
- **`POST /transcriptions`: não é montada.** O ditado só nasce no PC e sobe pelo sync.
- **`PATCH /transcriptions/{id}`**: como acima. `409` com a versão velha. Não devolve `sugestoes`:
  aprender continua só no PC (ADR-017).
- **O selo de conflito:** um item pode trazer `versao_em_conflito` (o texto que perdeu, o aparelho e
  quando). Escolher é um `PATCH` com `base_version` e `resolver_conflito`: `"manter"` (fica o texto
  atual) ou `"trocar"` (vale o outro); os dois limpam o selo. ~~Nomes finais na F7c.~~
  **F4 (05-out-2026), implementado:** `PATCH /transcriptions/{id}` com
  `{ "base_version": 3, "resolver_conflito": "trocar" }`, sem `final_text`; os dois juntos, ou nenhum,
  são `400`, e uma escolha fora da lista também. Base velha, ou item sem selo (outra aba ou aparelho já
  escolheu), `409`. Dá certo: `200` com o item, versão e `seq` novos, `versao_em_conflito: null`, e a
  mudança chega ao PC pelo pull. `"trocar"` conta como mudança do texto feita por quem escolheu (o
  celular, sem aparelho); `"manter"` não toca no texto. A regra é `cloudstore.ResolveConflict`, igual
  na memória e no Postgres. O PC passa a escolher na F7c, com o mesmo campo. **[Decisão 1 do plano da
  F4; a confirmar pelo Mauro.]** **F7c (06-out-2026):** o PC escolhe pelo push (abaixo, "Payload de
  mudar"), e o painel do PC pelo `PATCH /api/history/{id}` da Local API, com o mesmo corpo.
- **`POST /transcriptions/{id}/purge`**: excluir definitivamente, só para item na lixeira. Deixa uma
  lápide, **se o ditado ainda está dentro da janela** (`!OutOfWindow(created_at, now - 24 h)`; fora
  dela a lápide nasceria vencida, e só apaga: F9). `204`.
- Toda mudança por essas rotas passa pelo ponto único de gravação e chega ao PC pelo pull (ADR-009).
- **F2 (05-out-2026):** montadas sobre o `Store` (`internal/server/store_routes.go`). **O token do PC
  ainda alcança o CRUD até a F3**, que o fecha com `403` (o "só com sessão" acima é o alvo). `GET
  /transcriptions` (ainda com `q`; a F3 move a busca para o `POST`), **`GET /transcriptions/{id}`**
  (nova: o painel a usa depois de um `409`), `PATCH` (`base_version` e `final_text` obrigatórios; `409`
  com a versão velha), `DELETE` (lixeira; de novo é `204` sem gravar) e `POST .../restore` (`200` com o
  item). A lixeira sai pela data de exclusão, como a API local. O item:
  ```json
  { "id": "uuid", "device_id": "uuid ou null (onde o ditado nasceu)", "source_platform": "windows",
    "raw_text": "...", "final_text": "...", "mode": "smart", "language": "pt-BR",
    "asr_provider": "whisper-local", "asr_model": "small-q4_0", "duration_ms": 3200,
    "processing_ms": 410, "confidence": 0.91, "status": "done",
    "created_at": "...", "updated_at": "...", "deleted_at": null, "version": 2,
    "versao_em_conflito": null }
  ```
  `deleted_at` e `versao_em_conflito` vêm sempre (`null` quando não há). O selo:
  `{ "final_text": "...", "device_id": "uuid ou null", "conflict_at": "..." }`. O "excluir
  definitivamente" já existe no `Store` (`Purge`, com a lápide); a rota foi montada na F3 (abaixo).
- **F3 (05-out-2026):** o token do PC recebe `403` em todas estas rotas. `GET /transcriptions` com `q`
  na URL (qualquer valor, até vazio) é `400`. `POST /transcriptions/search`, corpo de até 16 KiB:
  `{ "q": "...", "cursor": "...", "limit": 50, "from": "RFC3339", "to": "RFC3339", "device_id": "uuid",
  "mode": "smart", "trash": false }`, todos opcionais, com a mesma resposta e os mesmos `400` do `GET`.
  `POST /transcriptions/{id}/purge`: `204`; fora da lixeira, `409`; de outro usuário ou que não existe,
  `404`. Deixa a lápide (dentro da janela, como acima), que chega ao PC no pull (`gone`, `apagado`,
  num push depois).
- **Revisão da F2 (05-out-2026):** `final_text` acima de 64 KiB no `PATCH` é `400` ("texto grande
  demais"); `q` ou `mode` com o caractere nulo ou UTF-8 inválido é `400` (antes, `500` no Postgres); e a
  página da listagem para no item em que o texto acumulado (bruto, final e selo) chega a 2 MiB, com
  `next_cursor` para o resto, mesmo abaixo do `limit`. Os dois números são [ASSUMPTION] (ADR-019,
  "Decisões da F2").

## Learned (ADR-019)
O aprendido no painel dos PCs, para ver e desfazer no celular. Só com sessão. O texto nunca vai na
URL.
### GET `/learned`
`{ "items": [ { "id": "uuid", "termo": "ECF", "variante": "sf", "restrito": false, "dominio": "" } ] }`
### POST `/learned/undo`
`{ "id": "uuid" }`. Desfaz em toda parte: chega aos PCs pelo pull. `200` com o item desfeito.

**F8 (07-out-2026; ADR-019, "Decisões da F8"); a F8d implementa.** As duas só com sessão: o token do
PC recebe `403`.
- `GET`: só os não desfeitos, do mais recente para o mais antigo (`last_seq`), até um teto fixo
  (proposta: 500, ~~a confirmar pelo Mauro~~ confirmada pelo Mauro em 08-out-2026), com o sinal de
  que passou dele:
  `{ "items": [ ... ], "has_more": false }`. O item é o de cima.
- `undo`: pelo ponto único de gravação, com `sync_event` (`upsert`, versão nova, `undone_at`). O já
  desfeito responde `200` sem gravar nem gastar `seq`. O de outra conta, o que não existe e o id que
  não é UUID dão `404`, com uma frase própria (não a do ditado). O texto nunca vai na URL.
- O celular não aprende: não há rota de criar nem de mudar o aprendido.
- **Como a F8d fechou isso (07-out-2026; `internal/server/learned_routes.go`, a suíte
  `internal/server/contracttest/learned_celular.go`):**
  - o teto é `cloudstore.MaxLearnedList` = 500 (a proposta, ~~ainda a confirmar pelo Mauro~~
    confirmada pelo Mauro em 08-out-2026, "Confirmo todas": escolha menor 3). Com exatamente 500 pares vivos, `has_more` é `false`; com 501, vêm os 500 mais
    recentes e `has_more` é `true`; os desfeitos não contam. O `GET` não lê a query: um parâmetro na
    URL é ignorado, e o log de acesso nunca a tem;
  - o item é `{ id, termo, variante, restrito, dominio }`, e só isso: `dominio` é `""` sem domínio
    (o domínio tem de 1 a 80 runas, então `""` não se confunde com um nome), e nada da linha (a chave,
    o dono, as versões, as datas, o `undone_at`) vai ao celular. `items` nunca é `null`. O mesmo item
    é a resposta do `undo`;
  - o corpo do `undo` é `{ "id": "uuid" }`, até 1 KiB; o id em maiúsculas vale. Sem o campo `id`
    (ou com um que não é texto, ou um JSON que não se lê) é `400`. O que não é um UUID, o que não
    existe e o de outra conta são o mesmo `404`: `NOT_FOUND`, "troca aprendida não encontrada". Como o
    id é o hash da variante (o mesmo em toda conta), o `undo` desfaz só o par da conta de quem pede;
  - o `undo` passa pelo ponto único de gravação (`Store.UndoLearned`): o contador primeiro, a linha
    `FOR UPDATE`, `cloudstore.UndoPair` (a mesma regra do `delete` do PC: `undone_at` e `updated_at`
    no relógio do servidor, versão + 1, o texto mantido), o evento `learned` `upsert` com o mesmo
    `seq` e **sem aparelho** (o celular). Chega ao pull como `upsert` com `undone_at`, `entity_version`
    nova e `device_id` ausente. O par já desfeito (pelo celular ou pelo PC) responde `200` com a linha
    como está, e o número volta: o `delete` do PC sobre um par que o celular desfez também é `applied`
    na versão atual, sem gravar. Um PC que manda outro termo com a base de antes do desfazer recebe o
    `conflict` com o par desfeito: o desfazer vence (D4);
  - o log de acesso do `GET` tem a contagem `itens`; o `undo` tem só o padrão da rota e o status.
    Nenhum id, variante, termo ou domínio;
  - **o app do celular (F8h, 07-out-2026; `web/src/nuvem/AprendidoNaNuvem.tsx`):** lê o `GET` quando as
    Configurações aparecem (de novo a cada vez). Com `has_more`, uma linha diz que só as mais recentes
    aparecem: sem alarme, e sem paginar (o teto é o do servidor). O `undo` leva o `id` no corpo, depois
    de a pessoa confirmar na própria linha. O `404` **com a frase** "troca aprendida não encontrada"
    vale como "já não está aí": a linha sai e a pílula diz "Desfeito em todos os aparelhos.", como no
    sucesso; um `404` sem ela (a rota nova que o servidor ainda não tem, um proxy) e os outros erros
    ficam na seção ("Não deu para desfazer: …"), e o `401` e a rede fora vão para a faixa do app.
    `dominio` `""` é "sem domínio"; com domínio, a linha o mostra, e com `restrito` diz "só no domínio";
  - **no Postgres, sem prova executada:** a listagem e o desfazer sobre a `learned_pairs`
    (`internal/server/pgstore/learned.go`) e os casos do celular em `TestContrato_Postgres` e em
    `learned_celular_pg_test.go` estão escritos e nunca rodaram (o Postgres de teste estava desligado
    na F8d e na revisão). Ver "Como a F8c fechou isso no Postgres", abaixo, para o que falta rodar.

## Dictionary
**Depois (ADR-019):** não montada na etapa 5. O dicionário continua no `dicionario.json` de cada PC;
só o aprendido sincroniza (`/learned` e o sync).
### GET `/dictionary`
### POST `/dictionary`
```json
{
  "id": "uuid",
  "canonical_form": "DCTFWeb",
  "language": "pt-BR",
  "domain": "contabilidade",
  "priority": 0,
  "aliases": ["dctf web"]
}
```
### PATCH `/dictionary/{id}`
### DELETE `/dictionary/{id}`

## Corrections
**Depois (ADR-019):** não montada na etapa 5. A Correction com a frase inteira não é guardada
(ADR-017); o par curto sobe como aprendido.
### POST `/corrections`
```json
{
  "id": "uuid",
  "transcription_id": "uuid",
  "from_text": "n oito n",
  "to_text": "n8n",
  "language": "pt-BR",
  "confirmed_by_user": true
}
```

## Settings
**Depois (ADR-019):** não montada na etapa 5. As preferências ficam em cada PC.
### GET `/settings`
### PUT `/settings`

Apenas settings sincronizáveis. Hotkey/microfone/modelo local são device-local por default.

## Sync

### POST `/sync/push`
```json
{
  "device_id": "uuid",
  "mutations": [
    {
      "mutation_id": "uuid",
      "entity_type": "transcription",
      "entity_id": "uuid",
      "operation": "upsert",
      "base_version": 0,
      "payload": {}
    }
  ]
}
```

Status por mutation:
- applied;
- duplicate;
- conflict;
- rejected.

### GET `/sync/pull?after_seq=123&limit=500`
```json
{
  "changes": [
    {
      "seq": 124,
      "entity_type": "dictionary_entry",
      "entity_id": "uuid",
      "operation": "upsert",
      "entity_version": 4,
      "payload": {}
    }
  ],
  "last_seq": 124,
  "has_more": false
}
```

### Como fica na etapa 5 (ADR-019, 01-out-2026)
Push e pull **só com o token do PC**, e por usuário.

**Push.** Cada mutação: `mutation_id`, `entity_type` (`transcription` ou `learned`), `entity_id`,
`operation` (`upsert` ou `delete`), `base_version` e `payload`. O payload tem uma lista fechada de
campos por tipo; campo fora dela é `rejected` (nunca `target_app`, áudio ou contexto). O
`created_at` do ditado é o do PC, e a nuvem recusa data fora da janela de 6 meses ou no futuro além
de uma folga. Lotes limitados por número e por bytes (limites na F7c). Status por mutação:
- `applied` (com `entity_version`). Quando o servidor **mesclou** (a `base_version` era velha e os
  campos mudados eram diferentes), vem também `mesclado: true` (nome final na F2). O `applied` não
  traz o estado: o PC não guarda a `entity_version` de uma mescla como `server_version`, e o estado
  mesclado chega no pull seguinte (ADR-019, "Eco e mescla");
- `duplicate` (a mesma resposta da primeira vez, inclusive o `mesclado`);
- `conflict` (com `entity_version` e o **estado atual** da entidade, `state`). No ditado, a versão que
  perdeu fica no item, como `versao_em_conflito`;
- `rejected` (com `code`, de uma lista fechada);
- `gone` (com `motivo`: `apagado`, e o PC apaga a cópia dele; ou `fora_da_janela`, e o PC a mantém e
  para de sincronizá-la).

A resposta traz `sync_epoch`.

**Pull.** `GET /sync/pull?after_seq=N&limit=M` devolve o **estado atual** de cada entidade que mudou
depois de `N` (cada uma com `seq` igual ao `last_seq` dela) e as **lápides**
(`"operation": "tombstone"`, sem payload), em ordem de `seq`:
```json
{
  "changes": [
    { "seq": 124, "entity_type": "transcription", "entity_id": "uuid", "operation": "upsert",
      "entity_version": 4, "device_id": "uuid de origem", "payload": {} },
    { "seq": 125, "entity_type": "transcription", "entity_id": "uuid", "operation": "tombstone" }
  ],
  "last_seq": 125,
  "has_more": false,
  "sync_epoch": "uuid"
}
```
- ~~`410 Gone` (`code: "RESYNC_REQUIRED"`) quando `after_seq` é menor que o maior `seq` de lápide já
  apagada (a marca d'água guardada em `user_sync_counters`)~~. **F9 (08-out-2026; ADR-019, "Decisões
  da F9", D2, confirmada pelo Mauro): a nuvem não responde `410` por marca d'água, nem por outro
  motivo, no mesmo epoch.** O pull segue de onde o PC parou: `200` com o que mudou depois de
  `after_seq`, e as lápides que ainda vivem. O que o PC parado perde é a lápide de um ditado com mais
  de 6 meses, que o job já apagou (a lápide vive a janela mais 24 h); o `gone` impede que ele volte
  (`apagado` enquanto a lápide vive, `fora_da_janela` depois), e o PC apaga esse ditado pela
  retenção dele (F10: só depois do OK dos 6 meses nele; sem o OK, o PC o guarda). A marca d'água
  (`tombstone_watermark`) fica na tabela, sem leitor.
- O PC compara o `sync_epoch` de cada resposta (push ou pull) com o que guardou: diferente, ele
  descarta a resposta e recomeça: cursor 0, `server_version` vazio e o envio de novo do que está na
  janela, sem a lixeira. É o **único** recomeço que a nuvem provoca (um backup restaurado, F11, ou o
  "apagar meus dados"). **Revisão de 06-out-2026 (ADR-019, "Decisões da F7a/b", decisão 2):** o `410`
  no mesmo epoch, se um dia vier (de um proxy, por exemplo; o cliente trata qualquer `410` assim), só
  volta o cursor a 0: as `server_version` continuam valendo, o pull do 0 reconhece pela versão o que
  o PC já tem, e nada é reenviado (reenviar com a base vazia criava selos sem conflito e trocava o
  selo de verdade pelo texto velho do PC).
- O pull não depende da idade do `SyncEvent`: ele lê as entidades e as lápides.
- `device_id` é o aparelho de origem da última mudança, para mostrar de onde veio o ditado. **Não**
  serve para descartar o eco: o PC o reconhece pela versão (a `entity_version` é igual à
  `server_version` que ele guardou), porque uma mescla leva o `device_id` de quem enviou e traz
  campos de outro aparelho.

**Como a F2 fechou isso (ADR-019, "Decisões da F2", 05-out-2026).** Implementado em
`internal/server/cloudstore` (regras), `internal/server/pgstore` (Postgres) e `internal/server/store_routes.go`
(HTTP); a suíte `internal/server/contracttest` é o contrato executável.
- **Credencial:** push e pull só com o token de um aparelho; a sessão do navegador recebe `403`. O
  aparelho e o usuário vêm do token: um `device_id` ou `user_id` no corpo é ignorado.
- **Só `transcription` na F2.** `learned` é `rejected` (`tipo_ainda_nao_sincroniza`) até a F8.
  **F8 (07-out-2026):** a F8b passa a aceitá-lo, pelo contrato de "O aprendido no sync", abaixo.
- **Payload de criar** (`base_version` 0), lista fechada: `source_platform`, `raw_text`, `final_text`,
  `mode` (`literal`/`smart`/`polish`), `language`, `asr_provider`, `asr_model`, `duration_ms`,
  `processing_ms`, `confidence` (0 a 1), `status` (`done`/`error`), `created_at` (RFC3339) e
  `deleted_at` (opcional). Obrigatórios: `source_platform`, `raw_text`, `final_text`, `mode`,
  `asr_provider`, `status`, `created_at`.
- **Payload de mudar** (`base_version` >= 1): só o que o PC mudou desde a base, `final_text` e/ou
  `deleted_at` (`null` é restaurar). Payload vazio é `campo_invalido`.
- **A escolha do selo pelo push (F7c, 06-out-2026; ADR-019, "Decisões da F7c").** O payload de mudar
  aceita também `"resolver_conflito": "manter" | "trocar"` com `"conflict_at"` (o do selo que o PC
  viu e escolheu), os dois juntos, com ou sem `final_text`/`deleted_at`:
  `{ "resolver_conflito": "trocar", "conflict_at": "2026-10-06T10:00:00.123456Z" }`. A regra é pura
  (`cloudstore.DecidePush`), a mesma na memória e no Postgres:
  - **vale** quando o selo de agora é o mesmo (o mesmo `conflict_at`) e ninguém mudou o texto depois
    da base (a última mudança do texto é da base ou de antes, ou do próprio PC): o selo sai como no
    `PATCH` do celular ("trocar" conta como mudança do texto feita por este PC), o resto do payload
    se aplica sobre o resultado, e a resposta é `applied` (com `mesclado` se a base era velha);
  - **cai** quando outro aparelho escolheu antes, chegou outro selo, ou o texto mudou depois da base:
    vale o que chegou primeiro, o resto do payload segue a mescla de sempre, e a resposta é `applied`
    com `mesclado: true` e versão nova (o pull leva ao PC o estado de verdade, com o selo de volta se
    ele ainda existe). Nenhum texto se perde.
  - Recusas: a escolha sem a data (ou a data sem a escolha), uma escolha fora da lista, que não é
    texto, ou uma data inválida são `campo_invalido`; com `base_version` 0 (o ditado inteiro), os dois
    campos são `campo_desconhecido`.
- `operation: "delete"` de ditado é `rejected` (`operacao_invalida`): a lixeira é o `deleted_at`.
- **Resposta:**
  ```json
  { "results": [
      { "mutation_id": "uuid", "status": "applied", "entity_version": 3, "mesclado": true },
      { "mutation_id": "uuid", "status": "duplicate", "original": "applied", "entity_version": 3, "mesclado": true },
      { "mutation_id": "uuid", "status": "conflict", "entity_version": 4, "state": { "...": "o item, com versao_em_conflito" } },
      { "mutation_id": "uuid", "status": "rejected", "code": "campo_desconhecido" },
      { "mutation_id": "uuid", "status": "gone", "motivo": "apagado" }
    ],
    "sync_epoch": "uuid" }
  ```
  `mesclado` só aparece quando é `true`. O `duplicate` traz `original` (o status da primeira vez) e o
  resto da primeira resposta, sem o `state`.
- **Códigos de `rejected`:** `mutacao_invalida` (`mutation_id` ou `entity_id` que não é UUID; não é
  gravada, e o reenvio recebe `rejected` de novo), `tipo_desconhecido`, `tipo_ainda_nao_sincroniza`,
  `operacao_invalida`, `campo_desconhecido`, `campo_invalido`, `data_no_futuro` (mais de 24 h à
  frente do relógio do servidor), `base_invalida` (negativa, ou maior que a versão atual) e
  `mutacao_reusada` (o mesmo `mutation_id` para outra entidade, ou mandado por outro aparelho do mesmo
  usuário: revisão da F2). **F8 (07-out-2026):** `mutacao_reusada` também para o mesmo `mutation_id`
  com outra operação, e o código novo `nada_a_desfazer` (o `delete` de um aprendido que a nuvem não
  tem). Ver "O aprendido no sync", abaixo.
- **`gone`:** `apagado` (há lápide); `fora_da_janela` (o ditado tem 6 meses ou mais pela data dele, ou
  a mudança é de um ditado que a nuvem não tem nem tem lápide). **F9:** a lápide vive a janela mais
  24 h, então um ditado apagado de vez dá `apagado` até lá e `fora_da_janela` depois; o ativo que o
  job apagou aos 6 meses não tem lápide e dá `fora_da_janela` (o PC mantém a cópia e para de
  sincronizá-la).
- **Mescla e conflito:** a mesma mudança não é conflito; campos diferentes são mescla (`applied`,
  `mesclado: true`); texto contra texto é `conflict`, e o texto do PC vai para o selo; pôr na lixeira
  sempre vale; restaurar contra uma lixeira mais nova não vale (mescla); uma edição de texto com base
  anterior à ida para a lixeira vai para o selo (`conflict`). O `conflict` e o `applied` mesclado
  **sempre gravam uma versão nova**, que chega a todos pelo pull.
  "Depois da base" é de outro aparelho: o campo (texto ou lixeira) cuja última mudança veio do
  próprio aparelho que manda a mutação conta como conhecido por ele, mesmo com a versão acima da base
  (o reenvio com a base velha depois de um `applied` mesclado). Revisão da F2, proposta a confirmar.
- **Limites:** até 500 mutações por push (`400` acima), corpo de até 1 MiB; até 500 mudanças por pull.
  [ASSUMPTION] A F7c fixa o lote do envio inicial.
  Revisão da F2, [ASSUMPTION]: `raw_text` e `final_text` até 64 KiB cada (`campo_invalido` acima); e a
  página do pull para no item em que o texto acumulado chega a 2 MiB (`has_more: true`), mesmo abaixo
  do `limit`.
- **Pull:** o `payload` de um `upsert` é o item (o mesmo JSON do CRUD, abaixo); o `device_id` da
  mudança falta quando ela veio do celular. O `410` da marca d'água, que este contrato previa para a
  F9, não existe (F9, D2).
- **A retenção na nuvem (F9; ADR-019, "Decisões da F9").** Não há rota nem configuração: é um job no
  processo do servidor, na subida e de hora em hora, por usuário. O que o cliente vê:
  - o **ativo** com 6 meses, pela data do ditado que o PC mandou, some do `GET /transcriptions` e do
    pull, **sem lápide**: o PC mantém a cópia (`fora_da_janela`, acima);
  - a **lixeira** com 30 dias (ou os 6 meses do ditado, o que vier primeiro) some; se o ditado ainda
    está dentro da janela, deixa a **lápide** e um evento `tombstone`, que chegam ao PC no pull, como
    o "excluir definitivamente";
  - a lixeira que chega pelo push **nunca some antes de 24 h depois de chegar** (D5, decidida pelo
    Mauro em 08-out-2026): com o `deleted_at` antigo (o PC que ficou sem internet, ou com o relógio
    atrasado), ela fica 24 h na lixeira do `GET /transcriptions?trash=true`, restaurável, e então
    some, com a lápide. Como a lixeira é ordenada pelo `deleted_at` (decrescente), ela aparece **no
    fim da lista**, na data antiga do `deleted_at`, e não no topo: depois das lixeiras recentes, e
    numa página seguinte se a lixeira passar do `limit`. A de sempre conta os 30 dias do
    `deleted_at`. A janela de 6 meses ganha do
    piso. O `deleted_at` do item continua o que o PC mandou; o reenvio (`duplicate`) e a lixeira
    mandada de novo não estendem o prazo; depois de restaurar, a lixeira seguinte conta de novo;
  - o `List` e o `Restore` não olham o vencimento, então um item vencido continua visível e
    restaurável por **até 1 h** depois (AC-018);
  - a **lápide** some depois da janela mais 24 h (`OutOfWindow(dictated_at, now - 24 h)`), decidida
    linha a linha, nunca por uma data no SQL;
  - o evento de `sync_events` que nenhuma entidade viva guarda como `last_seq` some depois de 90
    dias, e a `applied_mutations`, depois de 190 (`TokenSemUso` + 10 dias): um PC que volta dentro da
    vida do token (180 dias) ainda recebe `duplicate`;
  - o aprendido desfeito não é tocado (fica enquanto a conta existir);
  - o log do job é uma linha só com contagens (`11-observability.md`).
- **F3 (05-out-2026):** o token do PC vale como `Authorization: Bearer <token>` (o esquema sem distinção
  de maiúsculas); qualquer outra coisa no `Authorization`, ou um token desconhecido, de aparelho
  revogado ou de conta fora da lista, é `401`, **sem cair no cookie**. O token tem o formato de um
  segredo de 256 bits em base64url (43 caracteres; a F6 os gera assim); outro formato nem vai ao banco.
  O push tem o limite de 60 pedidos e 5.000 mutações por minuto por aparelho (`429` com `Retry-After`);
  o teto de 500 por lote (`400`) vale antes.

**O lado do PC (F7a e F7b, 06-out-2026; ADR-019, "Decisões da F7a/b"; `internal/sync`).**
- **O que o PC manda:** só `transcription`, sempre `upsert`, UUIDs em minúsculas. Sem `server_version`
  (a nuvem não conhece o ditado, ou o PC recomeçou): `base_version` 0 e o ditado inteiro, na lista
  fechada (nunca `target_app`, áudio, contexto, `id` ou `device_id`), com `deleted_at` se estiver na
  lixeira. Com `server_version`: `base_version` = ela, e só os campos que o marcador da fila diz que
  mudaram (`final_text` e/ou `deleted_at`, `null` para restaurar), lidos da linha na hora do envio.
  **F7c:** com a escolha do selo no marcador, o payload leva `resolver_conflito` e `conflict_at`; o
  marcador só com a escolha não manda texto nem lixeira. O envio inicial é o mesmo caminho: marcadores
  sem campo, que sobem o ditado inteiro, por último na fila (`inicial`), nos mesmos lotes; o limite da
  nuvem (60 pedidos e 5.000 mutações por minuto por aparelho) vira `429`, e o envio continua sozinho.
- **O lote:** até 500 mutações e 512 KiB de payload somado, sempre pelo menos uma (um ditado tem no
  máximo 128 KiB de texto); o corpo do pedido nunca passa de 1 MiB. Os em voo vão primeiro, com o
  mesmo `mutation_id` (gravado antes da rede, com a `version` local lida).
- **O que o PC faz com cada resultado** ("mudou" = a `version` local mudou durante o voo):
  - `applied`: grava `entity_version` como `server_version`, **menos com `mesclado`** (o estado vem no
    pull seguinte); sem "mudou", o marcador sai; com "mudou", fica (outro id na próxima vez);
  - `duplicate`: segue o `original`; o de um `conflict` não tem `state`, então a versão não é gravada
    (o estado chega no pull, que não aconteceu entre o envio sem resposta e o reenvio);
  - `conflict`: sem "mudou", aplica o `state` (o texto que venceu, o selo, a lixeira) e grava a versão;
    com "mudou", fica com a base velha, e o envio seguinte conflita de novo, com o texto mais novo;
  - `rejected`: `mutacao_reusada` manda de novo com outro id; `base_invalida` esquece a
    `server_version` e sobe o ditado inteiro; os outros marcam o ditado como `recusado` (com o código),
    e o marcador sai (fica, se mudou durante o voo);
  - `gone`: `apagado` apaga a linha e o marcador; `fora_da_janela` marca o ditado, que nunca mais
    entra na fila;
  - um status ou motivo que o PC não conhece: `recusado` com `resposta_desconhecida`;
  - um ditado com texto acima de 64 KiB nem sai: `recusado` com `texto_grande` (a nuvem o recusaria,
    e ele passaria do corpo do push).
- **O pull:** `GET /sync/pull?after_seq=<cursor>&limit=500`, página por página até `has_more` falso
  (até 100 páginas por rodada), cada página aplicada numa transação com o cursor novo. Um ditado com
  marcador é **pulado**; o eco é a `entity_version` igual à `server_version` guardada (nunca o
  `device_id`); o resto é gravado sem marcador, subindo a `version` local; uma lápide apaga a cópia
  (ou é pulada, com marcador: o push recebe o `gone`). O que não vale no PC (tipo desconhecido, id que
  não é UUID, payload de outro id, texto acima de 64 KiB, sem data) é "não aplicado" e o cursor anda;
  só um erro de E/S o segura. `has_more` com o cursor parado é falha.
  **F7c, a reconciliação (D3):** um ditado que existe aqui sem `server_version`, sem marcador, que
  mudou aqui (depois do "Esquecer esta nuvem", ou sem esquecer) e em que o lado do PC pode vencer
  (outro texto, ou na lixeira só aqui) não é sobrescrito: ganha um marcador de criar e sobe inteiro,
  com a base 0, numa volta a mais da mesma rodada; a nuvem decide (apagar vence; texto contra texto
  vira selo). O que não mudou aqui, o igual e o que só difere porque a **nuvem** o pôs na lixeira
  recebem o estado da nuvem (revisão da F7c: nesse último caso a nuvem mantinha a lixeira e
  respondia mesclado, com versão nova, e o PC reconciliava a cada rodada, sem fim). "Mudou aqui" é o
  `updated_at` depois da referência do esquecer (o instante dele), ou o ditado que o esquecer anotou
  como mudado aqui sem a nuvem receber (`05-data-model.md`, `nuvem_esquecida_em` e
  `sync_mudou_aqui`; reconferência da F7c: antes, a referência recuava até a mudança mais antiga).
- **O epoch, o `410` e a reconexão** (refeitos na revisão de 06-out-2026; ADR-019, "Decisões da
  F7a/b", decisão 2; **a nuvem não responde `410` desde a F9**, D2: o PC trata um `410` que venha de
  fora por robustez): o epoch guardado nulo adota o da resposta; outro descarta a resposta inteira
  (nada aplicado, nem o cursor) e **recomeça** (cursor 0, `server_version` vazia, o reenvio da janela
  sem a lixeira, com a base 0). O `410` no mesmo epoch só volta o cursor a 0 e lê tudo de novo, sem
  reenviar nada. A reconexão (outro pareamento da mesma instalação) continua do cursor, com as
  `server_version`; sobe só a fila, que guardou com a base certa o que mudou num ditado que a nuvem
  já tinha enquanto o PC estava desconectado. Cada um roda a rodada de novo no máximo uma vez; o
  segundo seguido é defeito da nuvem (espera longa).
- **O cliente:** `Authorization: Bearer <token>`, prazo de 30 s, nenhum redirecionamento seguido (uma
  `3xx` é falha: o token nunca vai a outro endereço), a resposta lida até 16 MiB (a pior página do
  pull, com o texto escapado no JSON, passa de 13 MiB; passou do teto é falha de rede). `401`
  desconecta o PC; `429` espera o `Retry-After` (até 10 min, com jitter); a rede, o prazo e um `5xx`
  esperam de 2 s a 5 min, com jitter; `400`, `403`, `404` e um JSON que não se lê esperam o teto.
  Nenhum pedido sai com o PC desconectado.

**O aprendido no sync (F8; ADR-019, "Decisões da F8", 07-out-2026).** Desenhado na F8.0; a F8b a
F8f o implementam, e os nomes finais saem delas. Sobe só a variante que o botão Aprender gravou
(`alias_origem` `"painel"`, D1), e só depois do clique em "Enviar o histórico" (D3; antes dele, só
o desfazer de um par que a nuvem já tem, que não leva texto: escolha menor 5 da ADR).
- **A entidade:** `entity_type` `learned`. O `entity_id` é o UUID v5 da chave
  `CollapseSpaces(Normalize(variante))` (`internal/dictionary`): o mesmo em todo PC, sem depender do
  `usuario` do arquivo. A identidade é a variante: trocar o termo muda a mesma entidade.
- **Push, `upsert`** (aprender, trocar o termo ou a restrição, reaprender), payload de lista fechada:
  `{ "variant": "sf", "term": "ECF", "restricted": false, "domain": null }`. `domain` é o nome do
  domínio, nunca o mapa de domínios para executáveis. `base_version` 0 cria; >= 1 é a versão que o
  PC conhece.
- **Push, `delete`** (o desfazer): payload vazio. Só o `learned` aceita `delete`; o do ditado
  continua `operacao_invalida`.
- **Pré-checagens, antes do banco** (a recusa não grava nada nem gasta `seq`): campo fora da lista,
  ou qualquer campo no `delete`, é `campo_desconhecido`. É `campo_invalido`: `variant`, `term` ou
  `domain` com o caractere nulo ou fora do UTF-8 válido (o que o `cleanString` já recusa no ditado);
  `variant` ou `term` fora de 1 a 80 runas (não bytes); `Normalize(variant)` vazio; `domain` que não
  é nulo nem tem de 1 a 80 runas; `restricted` sem `domain`; e `entity_id` diferente do id da
  `variant`. O servidor calcula o `normalized_variant` (a mesma chave do id). Nenhum push chega ao
  `23505`, ao `23514` nem ao `22021`. As checagens são uma função de `internal/dictionary`, que o PC
  usa na projeção: o que a nuvem recusaria nem sobe.
- **A decisão** (`cloudstore.DecideLearned`, pura, a mesma na memória e no Postgres):

  | A linha na nuvem | A mutação | Resposta | Grava |
  |---|---|---|---|
  | não existe | `upsert`, base 0 | `applied`, versão 1 | sim |
  | não existe | `upsert`, base > 0 | `rejected` `base_invalida` | nada |
  | existe | `upsert`, base > versão | `rejected` `base_invalida` | nada |
  | ativa, com o mesmo termo, restrição e domínio | `upsert`, base ≤ versão | `applied` com a versão atual | nada, nem `seq` |
  | com outro conteúdo, ou desfeita | `upsert`, base = versão | `applied`, versão + 1 (reaprender limpa `undone_at`) | sim |
  | com outro conteúdo, ou desfeita | `upsert`, base < versão (0 inclusive) | `conflict`, com `entity_version` e `state` | nada |
  | ativa | `delete`, qualquer base | `applied`, versão + 1, com `undone_at` | sim |
  | desfeita | `delete`, qualquer base | `applied` com a versão atual | nada |
  | não existe | `delete`, qualquer base | `rejected` `nada_a_desfazer` | nada |

  As linhas valem na ordem (a primeira que casa decide). Vale o primeiro na nuvem, e o desfazer
  vence (D4). O `applied` do aprendido nunca traz `mesclado`.
  O `delete` não olha a base e nunca é `base_invalida`; o `nada_a_desfazer` é um código novo da
  lista fechada do `rejected`, que o PC trata como sucesso (o marcador sai). O evento de toda mudança
  do aprendido, o desfazer inclusive, é `upsert` (o `CHECK` da 0008; a linha desfeita continua, com
  `undone_at`). O `duplicate` confere também a operação: o mesmo `mutation_id` com outra operação é
  `mutacao_reusada`. A operação fica gravada na `applied_mutations`, numa coluna `operation` que a
  0009 ganha, corrigida no lugar (F8c; ver `05-data-model.md`, AppliedMutation): sem ela, o
  Postgres não teria de onde tirá-la.
- **Pull:** a mudança `learned` vem sempre como `upsert`, com `entity_version`, o `device_id` de
  origem e o payload `{ "id": "uuid", "variant": "sf", "term": "ECF", "restricted": false, "domain":
  null, "undone_at": null }`; o par desfeito traz `undone_at`. O aprendido não tem lápide. O `state`
  do `conflict` é o mesmo objeto. As três consultas (ditados, lápides e aprendido) rodam no mesmo
  snapshot e se fundem por `seq`. Nenhum nome do payload coincide, com outro tipo, com um campo do
  item do ditado: um PC da F7 o decodifica sem erro, o conta como "não aplicado" e anda o cursor.
- **Como a F8b fechou isso (07-out-2026; `internal/server/cloudstore/learned.go`, a suíte
  `internal/server/contracttest/learned.go`):**
  - os quatro campos do `upsert` são obrigatórios (`domain` vem como `null`, nunca ausente), e
    `restricted` tem de ser booleano: o que falta, ou vem com outro tipo, é `campo_invalido`;
  - o payload do `delete` pode vir ausente, `null` ou `{}`; o que não é objeto é `campo_invalido`;
  - o texto que não é UTF-8 válido (`variant`, `term` ou `domain`) é conferido nos **bytes crus** do
    payload, antes de decodificar: o byte que não é UTF-8 e o escape `\u` de surrogate solto (a
    metade alta sem a baixa logo depois, ou a baixa sozinha) são `campo_invalido`. O `encoding/json`
    troca os dois por U+FFFD sem erro, e a `dictionary.ConferirPar`, que vê o texto já decodificado,
    nunca os veria: o par seria gravado com o texto trocado (e `"s\xfff"` teria a chave de `"sf"`).
    O U+FFFD de verdade (cru ou escapado) é UTF-8 válido e passa; o PC nunca manda o
    inválido, porque o `json.Marshal` dele também o troca. (Corrigido na revisão da F8: a primeira
    versão aceitava o texto trocado.) O `cleanString` do ditado tem a mesma limitação e não mudou;
  - "o mesmo termo, restrição e domínio" compara o texto como veio (o termo com outra caixa é outro
    conteúdo), e a grafia da variante não conta (o id já é o mesmo); o `upsert` que grava leva o par
    inteiro, a grafia da variante inclusive;
  - a recusa das pré-checagens não grava entidade, evento nem `seq`, mas fica em
    `applied_mutations` como toda recusa do push (a regra da F2): o reenvio é `duplicate` com o mesmo
    código;
  - o payload do pull e o `state` do `conflict` têm exatamente as seis chaves acima (sem o
    `normalized_variant`, o `user_id`, as versões nem as datas da linha), com `domain` e
    `undone_at` sempre presentes;
  - o Postgres, na F8b, ainda recusava o `learned` com `tipo_ainda_nao_sincroniza`; a F8c o grava
    (abaixo).
- **Como a F8c fechou isso no Postgres (07-out-2026; `internal/server/pgstore`, a 0009):**
  - o push do `learned` passa pelo mesmo ponto único de gravação do ditado: o contador primeiro, a
    linha da `learned_pairs` `FOR UPDATE` por `(user_id, id)`, a mesma `cloudstore.DecideLearned`, o
    `INSERT` ou o `UPDATE` (este com `AND version = <a lida>`, sem tocar o `normalized_variant` nem
    o `created_at`), o evento `learned` `upsert` com o mesmo `seq` (o desfazer inclusive) e a
    `applied_mutation`. `created_at`, `updated_at` e `undone_at` vêm do relógio do servidor (o
    injetado nos testes), nunca do `DEFAULT now()`;
  - a `applied_mutations` guarda a operação (a coluna `operation` da 0009, corrigida no lugar):
    `upsert`, `delete`, ou nula quando a mandada não era da lista; o reenvio a compara, como na
    memória;
  - **a chave é conferida antes do `INSERT`:** se outra linha do usuário já tem o mesmo
    `normalized_variant` (com outro id), a mutação é `rejected` `campo_invalido`, sem gravar nem gastar
    `seq` (fica em `applied_mutations`, e o reenvio é `duplicate`). Toda linha gravada tem o id = o
    UUID v5 do nome mais a chave que ela guarda, e as pré-checagens conferem o id: a mesma chave dá
    sempre o mesmo id, que a leitura pelo id já teria achado. Então a guarda só dispara quando o id da
    linha não vem da chave pelo nome de hoje: o nome ou o namespace do v5 trocado (o "v2" que
    `internal/dictionary/par.go` prevê para uma regra nova de chave), ou uma linha posta à mão. Sem a
    conferência, o `INSERT` daria o `23505`, com a variante no `DETAIL` do log do Postgres, e um `500`
    que o PC reenviaria sem fim. A memória não tem esse caso (as linhas dela são todas do mesmo nome).
    **O que ela não pega:** uma regra de chave mudada sem trocar o nome dá outra chave e outro id, e o
    `INSERT` grava uma segunda linha para a mesma variante, em silêncio (o risco "a estabilidade do
    id" da ADR-019; os vetores congelados avisam antes). **A consequência para o "v2" (decisão do
    Mauro, pendente):** com `campo_invalido`, todo par já sincronizado cuja chave não muda é recusado
    para sempre, e o de chave mudada ganha uma segunda linha; o "v2" precisa vir com uma migração que
    regrave o id e a chave das linhas antes de um PC mandar ids v2, ou a resposta da guarda precisa ser
    outra. (Corrigido na revisão da F8: a primeira versão dizia o contrário, que a guarda pegava a regra
    de chave mudada sem trocar o nome.)
  - o pull lê o aprendido numa terceira consulta, no mesmo snapshot `REPEATABLE READ` dos ditados e
    das lápides, com o aparelho de origem pelo `sync_event` do `last_seq`, e funde as três listas por
    `seq` antes do corte da página;
  - a suíte de contrato roda inteira contra o Postgres (a marca `SemAprendido` saiu), com os testes
    do que só o banco prova (`internal/server/pgstore/learned_pg_test.go`): um aprendido gravado entre
    as consultas do pull não faz o cursor pular; as datas do relógio; a operação gravada e comparada;
    nenhum `23505`, `23514` nem `22021`; o erro do Postgres sem a variante. **Só rodam com o Postgres
    de teste** (`AURORA_TEST_POSTGRES_URL`); sem ele, pulam;
  - **a F8c e a F8d no Postgres estão sem prova executada** (a revisão da F8, 07-out-2026, conferiu:
    sem `AURORA_TEST_POSTGRES_URL` e sem o contêiner). Nunca rodaram: `TestContrato_Postgres` (com os
    casos do aprendido e do celular), `learned_pg_test.go`, `learned_celular_pg_test.go`,
    `TestPostgres_PullNumSnapshotSo` e `TestPostgres_MutacaoGuardaAOperacao`
    (`migrations/postgres_test.go`). O que rodou é só o puro: `mergePage` e o texto do SQL conferido
    contra as colunas da 0009 e da 0016. O SQL de verdade (a `EXISTS` da chave, o `UPDATE` com `AND
    version`, o `LEFT JOIN` do pull, a listagem e o desfazer do celular, a ida e volta da coluna
    `operation`) só é exercitado quando o Mauro sobe um contêiner **novo** com
    `scripts/postgres-de-teste.ps1` (o de antes recusa a 0009 corrigida por checksum: `-Parar` e
    subir de novo) e roda `go test ./internal/server/pgstore/ ./migrations/` com
    `AURORA_TEST_POSTGRES_URL`. Até lá, a F8c e a F8d não estão fechadas no Postgres.
- **O lado do PC:** a operação sai do espelho no SQLite na hora do envio (presente aqui, `upsert`;
  saiu daqui, `delete`); o envio em voo repete a mesma operação; o desfazer de um par que a nuvem
  nunca teve nem sai. Respostas: `applied` guarda a versão; `conflict` aplica o `state` com as
  regras do PC que recebe e mostra a etiqueta "desfeito em outro aparelho" ou "trocado em outro
  aparelho"; `base_invalida` zera a base e manda com 0; `nada_a_desfazer` tira o marcador;
  `mutacao_reusada` manda com outro id; os outros `rejected` deixam a linha "recusado", com o código
  e a projeção recusada, e o par só tenta de novo quando muda (ou no epoch novo, ou no esquecer):
  uma tentativa por mudança, e não uma por rodada. Antes do aceite (D3), o `upsert` espera o clique,
  e só o `delete` de um par que a nuvem já tem sai (sem texto). No pull: o par que mudou aqui e não
  subiu é pulado (o aplicador nunca o sobrescreve); o que não vale aqui fica "não aplicado neste PC", com o motivo, e
  o cursor anda; o arquivo indisponível nunca segura o cursor. O epoch e o pareamento são conferidos
  antes de o `dicionario.json` ser tocado. O detalhe está na ADR-019, "Decisões da F8".

## ASR cloud
**Depois (ADR-019):** não montada na etapa 5 (o áudio não sai do PC). Um teste prova que a rota dá
`404`.
### POST `/asr/transcribe`

- multipart/stream;
- áudio em memória/stream;
- não persistir;
- limite de tamanho/duração;
- timeout;
- provider server-side.

Response:
```json
{
  "text": "transcrição",
  "language": "pt-BR",
  "confidence": 0.93,
  "provider": "cloud-provider-id",
  "model": "model-id",
  "processing_ms": 380,
  "segments": []
}
```

## Resolve cloud
**Depois (ADR-019):** não montada na etapa 5. O contexto (`context.app`, `nearby_text`) nunca vai
para a nuvem nesta etapa. Um teste prova que a rota dá `404`.
### POST `/resolve`

Smart request:
```json
{
  "mode": "smart",
  "transcript": "abra o modulo fisico",
  "uncertain_spans": [
    {
      "start": 14,
      "end": 20,
      "original": "fisico",
      "candidates": ["físico", "Fisco"]
    }
  ],
  "context": {
    "app": "chrome",
    "nearby_text": "Módulo Fisco"
  },
  "dictionary_terms": ["Fisco"]
}
```

Response:
```json
{
  "replacements": [
    {
      "start": 14,
      "end": 20,
      "replacement": "Fisco",
      "confidence": 0.96
    }
  ]
}
```

Backend não persiste `context`.

## Updates
**Depois (ADR-019):** não montada na etapa 5. A atualização é manual nas 0.x (ADR-018).
### GET `/updates/windows/latest?current=0.1.0&arch=x64`

```json
{
  "update_available": true,
  "version": "0.2.0",
  "release_notes_url": "https://...",
  "download_url": "https://...",
  "sha256": "hex",
  "signature": "optional"
}
```

Isso abstrai GitHub público/privado.

## Health
- GET `/health/live`
- GET `/health/ready`

**F3:** sem credencial e com qualquer `Host`. O `ready` faz um `Ping` no Postgres (prazo de 2 s) e
responde `503` se ele não responder.

**F11c (ADR-019, "Decisões da F11"):** `GET /api/v1/health/retencao`, a saúde do job de retenção, só
para quem pede pela própria máquina (o `RemoteAddr` de loopback) e **sem** cabeçalho de proxy
(`X-Forwarded-For`, `Forwarded`, `X-Real-Ip`); para qualquer outro, é o `404` de rota não montada.
Responde `200` com só números: `{"rodadas": 3, "ultima_ha_s": 1200, "falhas_graves": 0}` (as rodadas
desde a subida, há quantos segundos a última terminou, no relógio da nuvem, e as falhas de categoria
`banco` ou `outro` da última). Quem a lê é o `aurora-whisper-server healthcheck -retencao`, dentro do
contêiner (`10-env-and-secrets.md`). Sem credencial, com qualquer `Host` (como o `live`) e com
`Cache-Control: no-store`.

## Idempotência
- sync usa `mutation_id`;
- transcription aceita ID client-side;
- ASR `request_id` só para tracing;
- retries não podem duplicar records.

## Android futuro
Nenhuma rota depende de Win32.
`platform=android` é válido.
Device auth/sync/ASR são cross-platform.

**Etapa 5 (ADR-019):** o celular Android entra como PWA, o app servido pela própria nuvem, que usa as
rotas acima com a sessão do navegador. Ele não pareia como aparelho nem usa o sync: lê e edita pelo
CRUD.

## App (ADR-019)
Fora de `/api/v1`, na mesma origem: o build da nuvem do painel (`index.html`, `/assets/*`), o
`/manifest.webmanifest`, os ícones e o `/sw.js`. Manifest e ícones são públicos. `index.html` e
`sw.js` saem com `Cache-Control: no-cache`; `/assets/*`, com `immutable`. O service worker nunca trata
`/api`, `/auth` nem `/device`.

**F3:** até a F4 trazer o app, `/` é uma página provisória (`internal/server/pagina`), com
`/pagina.js` e `/pagina.css`, sem script nem estilo embutido (a CSP é `default-src 'self'`): entrar,
a conta, as sessões (derrubar), sair e "apagar meus dados". Outro caminho fora de `/api` e `/auth` é
`404`.

**F4 (05-out-2026): o app.** O servidor serve o build da nuvem do painel (`pnpm build:nuvem`,
`web/dist-nuvem`), da pasta de `PAINEL_DIR` (`10`), lida uma vez, na subida, para a memória; nenhum
caminho de pedido chega ao disco. A página provisória saiu (`/pagina.js` é `404`).
- `GET /`: o `index.html`, `Cache-Control: no-cache` e `ETag` (o navegador confere a cada vez: `304`
  quando não mudou).
- `GET /assets/{arquivo}`: um arquivo do build, `Cache-Control: public, max-age=31536000, immutable`
  (o nome tem o hash do conteúdo). O que não está no build é o `404` em JSON.
- Os arquivos da raiz do build (`/favicon.svg`, `/licencas-fontes.txt`): `no-cache` e `ETag`.
- Os tipos saem de uma tabela fixa (`.html`, `.js`, `.css`, `.svg`, `.woff2`, `.txt`, e `.png`,
  `.ico` e `.webmanifest` para a F5); um arquivo com outra extensão, outro nome, outra pasta além de
  `assets/` ou sem o `index.html` é erro na subida (saída 2).
- Sem fallback de SPA: `/index.html`, `/historico` ou (até a F5) `/sw.js` são `404`, como qualquer caminho fora do
  build. `POST /` e os outros métodos também. A CSP e os cabeçalhos são os de sempre.
- As duas páginas fixas do login que não deu (`recusado.html` e `falhou.html`, os mesmos status da
  F3) ganharam o visual da tela Entrar e o estilo `/pagina.css`.
- **Login de teste (só com a build tag `logindeteste`):** além do `POST /auth/teste`, o `GET
  /auth/teste` responde `{ "conta": "<a primeira de CONTAS_PERMITIDAS>", "google": true|false }`, com
  as mesmas guardas (só da própria máquina). A tela Entrar o pergunta para decidir se mostra o botão
  da conta de teste, e só com a página em `localhost` (o único endereço em que o servidor com a tag
  sobe); sem a tag, é `404`. Com a tag, o servidor também sobe sem o client do Google (o
  subcomando `teste`), e aí `POST /auth/start` e o callback são `404`.

**F5 (05-out-2026): o app instalável.** O build da nuvem traz, na raiz, o `/sw.js`, o
`/manifest.webmanifest` e os ícones (`/icone-192.png`, `/icone-512.png` e `/icone-maskable-512.png`).
O servidor não mudou: cada um vira uma rota pública da raiz, como o favicon (`no-cache` com `ETag`; os
tipos `text/javascript`, `application/manifest+json` e `image/png` já estavam na tabela), com a CSP de
sempre (o `default-src 'self'` permite o registro do `/sw.js` e o manifest). Sem
`Service-Worker-Allowed`: o `/sw.js` está na raiz e o escopo é `/`. `/index.html` continua `404`. O
service worker responde só à navegação `GET /` (rede primeiro), ao `GET /favicon.svg` (rede primeiro)
e ao `GET /assets/*` da casca (guardado primeiro); nunca a `/api`, `/auth`, `/device`, outro método ou
outra origem (ADR-019, "Decisões da F5").

**F6 (05-out-2026): a página de aprovação.** `GET /device/authorize` serve o mesmo `index.html`
(público, `no-cache`, `ETag`), e o app decide pelo caminho: com a sessão, "Conectar este PC?" (o
nome do PC, o código de conferência, a conta a que o PC vai ficar ligado, o e-mail do `/me`, Aprovar
e Agora não); sem ela, Entrar, cujo login volta a
`/device/authorize?pedido=…` (a `volta` do `/auth/start`). O service worker continua sem tocar
`/device`.

---

## API local (Windows Agent)

Acrescentada em 29-set-2026, na revisão adversarial dos commits `6f546e0` e `3c7dba4`: a rota
`POST /api/sessao` e o fluxo do bilhete existiam só no código e no texto dos commits, e este
contrato só descrevia a cloud. Em divergência, vale o código:
`internal/agent/painel/bilhete.go` e `servidor.go` (sessão, rotas, cabeçalhos),
`internal/agent/guard` (Host, Origin, token) e `internal/agent/localapi` (histórico, settings).
A segurança está em `09-security-privacy.md`, seção "Local API".

### Convenções
- Base: `http://127.0.0.1:<porta>`, só loopback IPv4. A porta é escolhida pelo sistema a cada
  subida do Agent: não há porta fixa.
- `Host` aceito: exatamente `127.0.0.1:<porta>` ou `localhost:<porta>`. Qualquer outro → `403`
  (DNS rebinding, THR-002).
- `Origin`, **se presente**, precisa ser `http://127.0.0.1:<porta>` ou `http://localhost:<porta>`;
  qualquer outro → `403`. Ausente é aceito — ver 09, "Local API".
- Um usuário por instalação: o pedido não carrega `user_id`; o Agent usa o dono fixo da instalação,
  gravado no banco local (`05-data-model.md`, "Dono local").
- JSON UTF-8, datas RFC3339.
- Erros de `/api/history*`, `/api/settings`, `/api/ia/*`, `/api/aprendizado*` (ADR-017) e
  `/api/termos*` (ADR-020): o mesmo envelope da cloud (`{"error":{"code","message","request_id"}}`),
  com `BAD_REQUEST`, `NOT_FOUND`, `CONFLICT`, `INTERNAL` e, só em `/api/aprendizado*` e
  `/api/termos*`, `UNAVAILABLE`. O `401`/`403` da cadeia de segurança e os erros de `/api/sessao` são
  texto puro. Em `/api/settings`, `/api/ia/*`, `/api/aprendizado*` e `/api/termos*` a `message` é
  escrita para a tela, em português.

### Três camadas de rota

| Rota | Exige | Para quê |
|---|---|---|
| `POST /api/sessao` | Host + Origin + bilhete | troca o bilhete pelo token |
| `/api/...` (as demais) | Host + Origin + `Authorization: Bearer <token>` | histórico, configurações, chave de API, aprendizado, termos, nuvem e (F10) retenção |
| `/nuvem/retorno` | Host | a volta da aprovação da nuvem (F6; abaixo, "Nuvem") |
| `/...` (o resto) | Host | arquivos do painel |

Os arquivos do painel não pedem token porque o navegador os carrega **antes** de ter um: é a
página que vai buscá-lo. Continuam atrás do `Host`.

### Sessão do painel: o bilhete

O token da API local (32 bytes aleatórios em hex, gerado a cada subida do Agent) existe só na
memória do Agent e na da aba — nunca em arquivo, cookie, `localStorage`, URL ou log. Chega à aba
assim:

1. A pessoa escolhe **Abrir Aurora Whisper** no menu da bandeja.
2. O Agent emite um **bilhete**: 32 bytes aleatórios em hex (64 caracteres), de **uso único**,
   válido por **um minuto**. No máximo **8** ficam vivos ao mesmo tempo; o nono descarta o
   emitido primeiro (por ordem de emissão, não por horário).
3. O Agent abre o navegador padrão em `http://127.0.0.1:<porta>/?t=<bilhete>`. A URL não vai
   para log.
4. A página tira o bilhete da URL (`history.replaceState`) **antes** de qualquer requisição e
   o troca pelo token em `POST /api/sessao` — uma vez por carga de página.
5. O bilhete morre na troca. O token fica só na memória da aba: recarregar a página perde a
   sessão (a página volta sem bilhete e não tem o que trocar). Reiniciar o Agent troca o token
   **e a porta**: a aba antiga continua apontando para a porta velha, onde ninguém escuta, e as
   requisições dela falham sem resposta — não chegam a receber `401`. Nos dois casos, abrir de
   novo pela bandeja resolve.

#### POST `/api/sessao`
Request (corpo de no máximo 1 KiB):
```json
{ "bilhete": "64 caracteres hex" }
```

Response `200`, com `Cache-Control: no-store`:
```json
{ "token": "64 caracteres hex" }
```

- `400`: corpo que não é JSON, ou com o campo de tipo errado. Um JSON válido sem `bilhete` (`{}`) cai no `401`.
- `401`: bilhete inexistente, expirado **ou** já usado — a mesma resposta para os três, para
  não dizer a quem tenta adivinhar quando chegou perto. A comparação é em tempo constante.
- `403`: `Host` ou `Origin` fora da lista.

### Histórico
Mesma semântica de `/transcriptions` da cloud, com o prefixo `/api/history`.

#### GET `/api/history`
Query:
- `q` — trecho procurado, sem diferença de maiúsculas, no texto bruto e no final;
- `from`/`to` — RFC3339, sobre `created_at`;
- `device_id`, `mode`, `provider` — igualdade exata;
- `trash` — `true` lista a lixeira; qualquer outro valor, o histórico ativo;
- `cursor` — o `next_cursor` da página anterior;
- `limit` — padrão 50, máximo 100.

Response:
```json
{ "items": [], "next_cursor": "opaco; ausente na última página" }
```

Ordem:
- histórico ativo: `created_at` decrescente — o mais novo primeiro;
- lixeira (`trash=true`): **data de exclusão** decrescente — o que foi para a lixeira por
  último vem primeiro (`deleted_at DESC, id DESC`, com o cursor sobre o mesmo par). É o que a
  migration local já prometia ("Lixeira: itens excluídos, mais recentes primeiro", índice
  `(user_id, deleted_at DESC)`). Até a revisão de 29-set-2026 a lixeira saía por `created_at`,
  e um ditado antigo apagado agora ia para o fim da lista, às vezes fora da primeira página.

Cada item (campos opcionais podem faltar; nunca há áudio nem contexto de janela — AC-027,
AC-028):
```json
{
  "id": "uuid",
  "device_id": "uuid",
  "source_platform": "windows",
  "raw_text": "texto bruto do ASR",
  "final_text": "texto que entrou no campo",
  "mode": "smart",
  "language": "pt-BR",
  "asr_provider": "whisper-local",
  "asr_model": "whisper-small-q4_0",
  "duration_ms": 3200,
  "processing_ms": 410,
  "confidence": 0.91,
  "target_app": "chrome",
  "status": "done",
  "created_at": "2026-09-29T12:00:00Z",
  "updated_at": "2026-09-29T12:00:00Z",
  "deleted_at": "2026-09-29T12:05:00Z",
  "version": 1
}
```

`status` é `done` ou `error` (o ditado terminou com erro e o texto pode não ter entrado no
campo). `deleted_at` só aparece em item da lixeira.

#### GET `/api/history/{id}`
Detalhe, com `raw_text` e `final_text`. `404` se não existe.

#### PATCH `/api/history/{id}`
```json
{ "base_version": 4, "final_text": "texto editado" }
```
`200` com o item atualizado. `409 CONFLICT` quando `base_version` não é a versão atual — outra
aba salvou antes (ou o sync acabou de aplicar uma mudança vinda da nuvem: aplicar sobe a versão).

**F7a (ADR-019, 06-out-2026):** `400` para `final_text` acima de 64 KiB ou com o caractere nulo, o que
a nuvem recusaria no push (`campo_invalido`): o PC não aceita uma edição que ficaria "recusada" no
sync. O corpo tem teto de 512 KiB. Com a nuvem conectada, a edição põe o marcador do ditado na fila do
sync, na mesma transação; a lixeira e o restaurar também (só quando a linha muda).

**Com a etapa 3 (ADR-017, fatia F3):** o `200` é o item atualizado **mais** o campo `sugestoes`,
sempre presente, uma lista de **0 a 2** trocas curtas de termo que a pessoa pode aprender com um
clique:
```json
{
  "id": "uuid", "final_text": "Vou mandar o ECF amanhã.", "version": 2,
  "sugestoes": [
    { "de": "sf", "para": "ECF", "termo_existente": "ECF", "ocorrencias": 3 }
  ]
}
```
- `de`: a forma que o ASR escreveu, lida do `raw_text` (a que vira variante).
- `para`: a forma final. Se o dicionário já tem o termo, é a forma canônica dele; se não, é como a
  pessoa escreveu, na caixa de meio de frase ("descrição", e não "Descrição" do começo da frase).
- `termo_existente`: o termo do dicionário que já é o `para`, ou `""` quando aprender **cria** o
  termo (com prioridade 50).
- `ocorrencias`: em quantos dos **últimos 500 ditados ativos** (sem este, sem a lixeira, só os do
  dono) o `de` aparece no texto bruto, como palavra inteira e sem olhar caixa, acento nem pontuação.
  `0` é "em nenhum" ou "não deu para contar"; a tela só fala disso quando é maior que zero.

**Salvar nunca grava no dicionário** (AC-022): quem grava é `POST /api/aprendizado`, depois do
clique. Para achar a troca, o Agent lê o texto **anterior** à edição antes de gravá-la (sem coluna
nova, sem migration), compara com o salvo e alinha o resultado com o `raw_text`. A lista vem vazia
(e a edição vale do mesmo jeito) quando: nada na edição é troca de termo; o Agent não montou o
aprendizado; não há `dicionario.json` (ou ele não abre), porque não se oferece um botão que não
funciona; ou alguém gravou o mesmo ditado entre a leitura do texto de antes e a gravação (a versão
não subiu de um em um). Também não se sugere a variante que o dicionário recusaria por ela já
pertencer a um termo **desativado** (o desativado é dono das suas formas, como o ligado). O `409` não traz `sugestoes`. O Agent guarda, **só em memória**, os pares
curtos que devolveu, por ditado e versão: é o que o clique confere (abaixo). Salvar o ditado de
novo invalida os do salvamento anterior.

#### DELETE `/api/history/{id}`
Move para a lixeira (soft delete): grava `deleted_at` e o prazo de expurgo de 30 dias. `204`.
A API local não tem exclusão definitiva. O expurgo automático do vencido roda no Agent, na subida
e de hora em hora (`internal/agent/expurgo`, desde 29-set-2026; corrigido pela ADR-019, que achou
este texto desatualizado). **Desde a F10 (ADR-019, a D3), o vencido que a nuvem ainda não soube
fica** (o ditado posto na lixeira com o PC desconectado, por exemplo), no máximo até o ditado
completar 6 meses: o `purge_at` gravado continua sendo o dos 30 dias, e a guarda é do expurgo. Os 6
meses do histórico ativo existem no Agent desde a F10, mas só valem depois do OK de quem usa (abaixo,
"Retenção no PC").

#### POST `/api/history/{id}/restore`
Tira da lixeira. `204`.

### Configurações
Acrescentado em 29-set-2026 (tela de Configurações do painel). Fonte da verdade:
`internal/agent/localapi/settings.go` e `ia.go`.

As preferências ficam em `local_settings`, no `historico.db` (na pasta de dados: `%LOCALAPPDATA%\AuroraWhisper`, ou ao lado do executável no modo portátil, ADR-018), e sobrevivem a
reiniciar o Agent. A chave de API **não**: ela mora no Gerenciador de Credenciais do Windows (ver
"Chave de API do provedor de IA", abaixo, e `09-security-privacy.md`).

**"Vale a partir do próximo ditado"**: o Agent aplica o que foi gravado no próximo keydown da tecla
de ditado em que nenhum ditado estiver em curso. Um ditado já começado termina com a configuração
com que começou.

#### GET `/api/settings`
Sempre `200`. Antes da primeira gravação devolve o padrão, com `version: 0` — não há `404`.
```json
{
  "hotkey": "ctrl+win",
  "hold_threshold_ms": 700,
  "beep_enabled": true,
  "microphone_always_ready": false,
  "text_mode": "smart",
  "ai_provider": "",
  "ai_model": "",
  "autostart": false,
  "autostart_indisponivel": "",
  "reinicio_pendente": [],
  "polish_disponivel": true,
  "polish_ultimo_motivo": "",
  "version": 3
}
```

| Campo | Edita no PUT | O que é |
|---|---|---|
| `hotkey` | não | O gatilho do ditado. Só leitura nesta etapa: um PUT com valor diferente recebe `400`. |
| `hold_threshold_ms` | sim | Quanto tempo segurar até gravar: **300 a 2000**, inclusive. Vale a partir do próximo ditado. |
| `beep_enabled` | sim | Beep ao começar a gravar. Vale a partir do próximo ditado. |
| `microphone_always_ready` | sim | "Microfone sempre pronto" (ADR-015); padrão `false`. Ligado, o microfone fica aberto o tempo todo e a captura leva 300 ms de antes das teclas; desligado, ele só abre durante o ditado. Booleano; ausente no PUT mantém. A API só grava e devolve: se entra em `reinicio_pendente`, quem diz é o Agent. |
| `text_mode` | sim | `"literal"`, `"smart"` ou `"polish"`; padrão `"smart"` (REQ-022). Ver abaixo. |
| `ai_provider` | sim | `""` (nenhum), `"openai"` ou `"anthropic"`. |
| `ai_model` | sim | Texto livre, até 200 caracteres, sem caractere de controle; espaços das pontas são removidos. A lista de onde escolher vem de `POST /api/ia/testar`. O código pode **sugerir** um modelo — o vencedor do benchmark do Polish, pré-selecionado na tela quando consta na lista da conta (ADR-016) —, **nunca exigi-lo**: nenhuma rota valida o nome contra uma lista fixa, e `ai_model` segue texto livre, gravado só pelo `PUT`. A sugestão viaja em `modelo_sugerido`, na resposta de `POST /api/ia/testar` (abaixo), e não aqui: ela é por provedor, e a tela precisa dela junto da lista daquele provedor. Começar com `sk-` (qualquer caixa) é `400`: é o começo das chaves dos dois provedores, e uma chave colada no campo errado seria gravada em texto puro numa preferência sincronizável. A mensagem não repete o valor. |
| `autostart` | sim | "Iniciar com o Windows", **lido do registro a cada GET** (`HKCU\...\Run` + `StartupApproved`, pacote `autostart`) — não é gravado no banco. A mesma regra da bandeja. |
| `autostart_indisponivel` | — | Por que não dá para mudar o `autostart` agora; `""` = dá. Hoje: `"só na versão de bandeja"` (build de console), `"não consegui ler"` (registro ilegível) ou `"não achei o programa"` (o Agent não soube o caminho do próprio executável). |
| `reinicio_pendente` | — | Campos gravados que só valem no **próximo início** do Agent; `[]` quando tudo já vale. Quem decide é o Agent em execução. Hoje é sempre `[]`: tudo o que esta tela muda vale a partir do próximo ditado. Nunca `null`. |
| `polish_disponivel` | — | Se o Agent faz o Polish. É `true` com a etapa 2 no Agent (ADR-016; fatias S4 e S5) — a tela então libera o modo (ver `text_mode`) quando há provedor e a chave dele guardada. Um Agent de antes da etapa 2 não manda o campo, ou manda `false`, e o painel o trata como `false`: o modo fica bloqueado, com "Chega na próxima versão". |
| `polish_ultimo_motivo` | — | Por que o último Polish não agiu, numa **frase curta pronta para a tela**, como o `erro` de `POST /api/ia/testar` (por exemplo, "Sem crédito na OpenAI: o último ditado saiu sem o Polish."). `""` quando o último Polish agiu, quando ainda não houve nenhum, quando o motivo era só o ditado curto demais (menos de 4 palavras: é o esperado, não é falha) ou quando já faz mais de 24 h. **Só o motivo: nunca o texto ditado, o corpo de erro do provedor nem a chave.** Vive em memória no Agent: não é gravado e some ao reiniciar. O Agent entrega ao `localapi` o motivo (o enum do `resolver`), o provedor que valia na tentativa e o instante (`Preferencias.UltimoPolish`), e a **frase sai de uma tabela do `localapi`** (`polish.go`), uma por `MotivoFallback`; um motivo que a tabela não conhece vira "Algo impediu o Polish: o último ditado saiu sem o Polish." — o texto do valor desconhecido nunca chega à tela. Frases que nomeiam o provedor (o da tentativa, não o gravado agora; "IA" se desconhecido): sem chave, sem conexão, prazo ("mais de 3,5 s"), chave recusada, sem crédito, limite de uso, modelo não achado, modelo que não serve, fora do ar, pedido rejeitado, modelo que recusou, resposta cortada, resposta inesperada. As das guardas dizem "A resposta da IA …" (formato, números, siglas, termos ou nomes, texto mudado, palavras acrescentadas). |
| `version` | — | Concorrência otimista. `0` antes da primeira gravação. |

`text_mode`:
- `"literal"` — o ditado sai como o ASR entregou, mesmo com o dicionário ligado (AC-023), e o
  histórico registra `"literal"`. Vale a partir do próximo ditado.
- `"smart"` — o comportamento de sempre: dicionário e autocorreção (quando há `dicionario.json`).
- `"polish"` — **aceito e gravado**, mas só com `ai_provider`, `ai_model` e a chave desse provedor
  guardada. **Num Agent de antes da etapa 2 o ditado sai como no Smart**, e o histórico registra
  `"smart"` (ele diz o que aconteceu, não o que foi escolhido); `polish_disponivel` ausente ou
  `false` é como a tela sabe disso. Apagar depois a chave do provedor não muda o modo gravado; sem
  chave, o Polish cai para o Smart (`16-ai-behavior-spec.md`, "Fallback").
  **Com a etapa 2 (ADR-016, fatia S4):** o Polish **preserva e organiza** o texto do Smart, e o
  histórico registra `"polish"` **só quando a reescrita foi aceita** pelas guardas — por
  tentativa, não pelo que foi escolhido —, e `"smart"` em qualquer fallback (sem internet, sem
  chave, sem crédito, chave recusada, modelo inexistente, sobrecarga, prazo de 3,5 s (até 8 s no
  ditado longo), guarda).
  Ditado com menos de 4 palavras nem chega ao provedor e sai como Smart. `raw_text` continua
  sendo o bruto do ASR.
  A chave é exigida só quando o PUT **leva** ao Polish: liga o modo, ou troca `ai_provider` com ele
  ligado. Com o Polish gravado e a chave apagada, um PUT que muda o beep, o limiar, o `autostart` ou o
  `ai_model` — ou que reenvia o objeto inteiro do GET — é aceito normalmente.

#### PUT `/api/settings`
Corpo: `base_version` (obrigatório: a `version` que veio no GET) e qualquer subconjunto dos campos
editáveis. **Campo ausente mantém o valor atual** — um cliente que esquecesse `autostart` não pode
desligar o início automático de ninguém.
```json
{ "base_version": 3, "hold_threshold_ms": 900, "text_mode": "literal" }
```
- `200`: o mesmo corpo do GET, já com a `version` nova.
- `409 CONFLICT`: `base_version` não é a versão atual — outra aba salvou antes. Conferido antes de
  qualquer outra coisa.
- `400 BAD_REQUEST`, com a mensagem pronta para a tela: sem `base_version`; limiar fora de 300–2000;
  `text_mode` ou `ai_provider` desconhecido; `ai_model` inválido (inclusive com cara de chave);
  `polish` sem provedor ou sem modelo; entrar no `polish` (ou trocar o provedor dele) sem a chave do
  provedor guardada; `hotkey` diferente; `autostart` diferente do atual quando
  `autostart_indisponivel` não é vazio (a mensagem traz o motivo).
- `500 INTERNAL`: o registro recusou o `autostart`, ou o banco falhou. O `autostart` é mudado
  **antes** de gravar: se o registro recusar, nada é gravado.
- Os PUTs entram em fila: um de cada vez, da conferência da versão até a entrega ao Agent. Duas
  abas salvando no mesmo instante com a mesma `base_version`: uma recebe `200`, a outra `409` — e a
  do `409` não chegou a tocar o registro.

### Chave de API do provedor de IA
A chave que quem usa colou na tela. Entra por `PUT` e **não sai por rota nenhuma**: nem no GET, nem
na confirmação, nem em mensagem de erro. Toda resposta de `/api/ia/*` sai com
`Cache-Control: no-store`. `provedor` fora de `openai`/`anthropic` é `400` em todas as rotas.

#### GET `/api/ia/chave?provedor=openai`
```json
{ "provedor": "openai", "configurada": true, "final": "a1B2" }
```
`final` são os últimos caracteres da chave: no máximo 4, e nunca mais que um quarto dela (uma chave
de API real tem dezenas). `""` quando `configurada` é `false`. `500` se o Gerenciador de Credenciais
não puder ser lido.

#### PUT `/api/ia/chave`
```json
{ "provedor": "openai", "chave": "sk-..." }
```
`204`, sem corpo. Substitui a chave anterior do provedor. Espaços e quebras de linha das pontas são
removidos (vêm junto ao copiar). `400` para chave vazia, maior que 2560 bytes (o limite do
Gerenciador de Credenciais) ou com espaço, caractere de controle ou não-ASCII no meio. O formato de
cada provedor (prefixo, tamanho) **não** é conferido: quem confere é o "Testar". `500` se o
Gerenciador recusar a gravação.

#### DELETE `/api/ia/chave?provedor=openai`
`204`, também quando não havia chave.

#### POST `/api/ia/testar`
```json
{ "provedor": "openai" }
```
Faz a chamada mínima ao provedor com a chave guardada — `GET /v1/models` (OpenAI:
`Authorization: Bearer`; Anthropic: `x-api-key` + `anthropic-version: 2023-06-01`, `limit=1000`) — e
devolve `200` nos dois desfechos:
```json
{ "ok": true, "modelos": ["...", "..."], "modelo_sugerido": "gpt-6-luna" }
{ "ok": false, "erro": "A OpenAI recusou a chave. Confira se ela foi copiada inteira e se ainda está ativa." }
```
- `modelos`: tudo o que o provedor listou, sem filtro, do mais novo para o mais antigo (OpenAI:
  pelo `created`; Anthropic: na ordem dela). Lista vazia é `"modelos": []`. A sugestão **não
  filtra** a lista.
- `modelo_sugerido` (só no `ok: true`; sempre presente): o modelo que o projeto **sugere** para o
  Polish nesse provedor, `""` quando não sugere nenhum. Vem de um mapa pequeno no `localapi`
  (`polish.go`, `modelosSugeridos`), do vencedor do benchmark de 30-set-2026
  (`docs/benchmarks/polish-2026-09-30.md`, regra da ADR-016, decisão 9): **`gpt-6-luna` na OpenAI;
  nenhum na Anthropic** (o `claude-haiku-4-5` ficou com fallback de 6,4%, acima dos 5%). É
  **sugestão, nunca exigência**: a resposta o manda **mesmo que a conta não o liste** (quem confere
  se ele consta em `modelos` é a tela), nenhuma rota valida `ai_model` contra ele, e trocar o
  vencedor é rodar o `polishbench` de novo e mudar uma linha. A tela o marca "(sugerido)" na lista
  e, se nenhum modelo foi escolhido e ele consta em `modelos`, o põe no **rascunho** (salvar segue
  explícito, pelo `PUT /api/settings`). Um Agent de antes da sugestão não manda o campo, e a tela
  o trata como `""`.
- `erro`, em português: sem chave guardada; chave recusada (401); sem permissão (403); limite de uso
  ou falta de crédito (429); provedor com problema (5xx); tempo esgotado (**10 s**); sem rede;
  resposta inesperada. O corpo de erro do provedor **nunca** passa adiante — o 401 da OpenAI traz um
  pedaço da chave.
- Redirecionamento do provedor não é seguido: o `x-api-key` não é removido pelo `net/http` ao trocar
  de domínio.
- `500` só se o Gerenciador de Credenciais não puder ser lido.

#### Chamada de Polish ao provedor (saída do Agent; não é rota da API local)
Decidida em 29-set-2026 (ADR-016) e implementada na fatia S2 (`internal/ia/polish.go`). O prompt,
o schema, a mensagem do usuário e as guardas da resposta estão em `internal/resolver` (fatia S1).
É a segunda chamada que `internal/ia` faz, e a única que leva texto ditado. Só `net/http`, sem SDK. As assinaturas Go estão em `18-tool-contract.md`; aqui está
o comportamento.

- **Quando:** só com `text_mode = "polish"`, provedor, modelo e a chave desse provedor guardada,
  e o texto final do Smart com **4 palavras ou mais**. A chave é lida do Gerenciador **a cada
  ditado**. Provedor e modelo são os que valiam no keydown.
- **Corpo, em comum:** o prompt fixo (`17-prompt-spec.md`, `polish-ptbr@1.0.2`) e, na mensagem do
  usuário, `{"ditado":"…"}` gerado por `json.Encoder` com `SetEscapeHTML(false)`
  (`PolishMensagemUsuario`). Nada de contexto, dicionário, áudio, histórico nem IDs. Schema de
  saída `{"final_text": string}`, estrito. O cliente manda `Instrucoes`, `Mensagem` e `Schema`
  exatamente como vêm no `PolishRequest`; não monta prompt por conta própria.
- **OpenAI:** `POST https://api.openai.com/v1/responses`, `Authorization: Bearer`. `instructions`
  = o prompt; o texto em mensagem `user`; `text.format` = `json_schema` com `strict: true`;
  `reasoning.effort: "none"`; `store: false`; `max_output_tokens: 2048`.
- **Anthropic:** `POST https://api.anthropic.com/v1/messages`, `x-api-key` e
  `anthropic-version: 2023-06-01`. `system` = o prompt; o texto em mensagem `user`;
  `output_config.format` = `json_schema`; `max_tokens: 2048`; sem `thinking`.
- **Prazo e protocolo:** o `ctx` deriva do `Run` do Agent e vence **3,5 s** depois do começo da
  resolução, com o Smart dentro (fatia S4); no ditado longo, mais 6 ms por caractere acima de 250,
  até **8 s** (ADR-016, emenda de 01-out-2026) — depois disso o texto do Smart é inserido, mesmo
  que o cliente não tenha respondido. Sem prazo no `ctx`, o `Cliente` corta em 10 s (`Cliente.Timeout`); com
  prazo, o mais curto vale. **Sem retry** e **sem seguir redirecionamento**. **Só HTTP/1.1**: o
  HTTP/2 do `net/http` escreve cada cabeçalho (a chave) e o começo de cada corpo (o ditado) no log
  padrão quando `GODEBUG` traz `http2debug`, e guarda o cabeçalho de autenticação na memória da
  conexão. **Só o status 200 vale** (um 3xx ou outro 2xx é `resposta_invalida`, mesmo com corpo de
  aparência boa) e a resposta é lida até **1 MiB**. Recusa do modelo, `status: "incomplete"`
  (OpenAI) e `stop_reason` diferente de `end_turn` (Anthropic) contam como erro.
- **Erros traduzidos, sem o corpo do provedor e sem a chave.** O corpo de um erro é lido só para
  achar `error.type`, `error.code`, `error.details.error_code` e `error.param` (sem distinguir
  caixa); nada dele passa adiante, e o erro do cliente é sempre `polish: <motivo>`. Viram o motivo
  de fallback (enum no `resolver`; `polish_ultimo_motivo` na tela, com a frase de `localapi/polish.go`, S5) e uma linha
  de log sem texto (`09-security-privacy.md`, "Polish: o que sai da máquina"). O motivo, por
  resposta:
  - `sem_credito`: 402; um destes no tipo, no código ou no detalhe do erro: `insufficient_quota`,
    `credit_balance_exhausted`, `billing_hard_limit_reached`, `organization_spend_limit_exceeded`,
    `project_spend_limit_exceeded`, `organization_usage_limit_exceeded`,
    `enforced_spend_limit_reached`; e, só no 400, a frase de crédito, de uso ou de gasto da
    Anthropic (heurística: "credit balance", "usage limit", "spend limit");
  - `modelo_inexistente`: 404, ou `model_not_found` em qualquer status (a OpenAI o manda também em
    400 e 403);
  - `modelo_incompativel`: 400 cujo `error.param` começa por `reasoning` (o modelo existe, mas
    recusa o esforço de raciocínio que o pedido manda);
  - `limite`: 429 que não é crédito (passa sozinho);
  - `chave_recusada`: 401 e 403 (o 403 de região da OpenAI cai aqui também); chave com espaço,
    quebra de linha ou caractere não ASCII nem sai da máquina;
  - `sobrecarga`: qualquer 5xx (a Anthropic usa 529);
  - `pedido_recusado`: os outros 4xx (bug de protocolo nosso, não da conta);
  - `transporte`: rede, TLS, DNS, conexão cortada; `prazo`: o `ctx` (ou o teto de 10 s) venceu.
  No 200: `recusa` (`refusal`, `incomplete` por filtro de conteúdo, `stop_reason: refusal`),
  `truncada` (`incomplete` por tamanho, `stop_reason: max_tokens`) e `resposta_invalida` (texto
  vazio, envelope com `error`, `status` ou `stop_reason` estranho, corpo acima de 1 MiB ou que não
  decodifica). Modelo vazio (`modelo_inexistente`) e chave vazia (`sem_chave`) nem abrem a rede.
- **Resposta:** só `final_text` é lido. `warnings`, `changed` ou qualquer outro campo são
  descartados. O `final_text` só vale se passar pelas guardas (`16-ai-behavior-spec.md`,
  "Guardas do Polish").

### Aprendizado
ADR-017 (30-set-2026), fatia F3. Fonte da verdade: `internal/agent/localapi/aprendizado.go`. Atrás da
mesma cadeia das demais rotas `/api/...` (Host + Origin + token) e com o mesmo envelope de erro. A
`message` de todo erro é uma frase **pronta para a tela**, em português, que a página mostra como
está. Nenhuma rota carrega texto ditado na URL: o "de" e o "para" vão no corpo.

#### GET `/api/aprendizado`
O que o painel aprendeu, só o de origem `"painel"`, na ordem do arquivo. É a lista "Aprendido no
painel" das Configurações (`08-ui-ux-spec.md`). Sem dicionário, ou sem o aprendizado montado, a
lista vem vazia (`200`). O Agent cria um dicionário vazio na subida quando não há nenhum (ADR-018),
então "sem dicionário" só acontece se essa criação falhou.
```json
{ "items": [ { "termo": "ECF", "variante": "sf" } ] }
```
`200`, com `Cache-Control: no-store`. O `items` nunca é `null`.

**F8 (07-out-2026; ADR-019, "Decisões da F8"); implementado na F8g (08-out-2026), com os nomes que
a F8h lê (`web/src/aprendizado.tsx`; revisão da F8g, 08-out-2026).** Cada item pode trazer
`situacao` e `motivo`, opcionais, lidos do espelho no SQLite:
```json
{ "items": [
  { "termo": "ECF", "variante": "sf" },
  { "termo": "ECD", "variante": "ecedê" },
  { "termo": "ECF", "variante": "ecedê", "situacao": "trocado_em_outro_aparelho" },
  { "termo": "DIRF", "variante": "dirfi", "situacao": "desfeito_em_outro_aparelho" },
  { "termo": "EFD", "variante": "efedê", "situacao": "nao_aplicado", "motivo": "dominio_ausente" }
] }
```
- Primeiro, o arquivo, como antes e na ordem dele, **sempre sem `situacao`**: é o par que vale aqui,
  e tem o Desfazer.
- Depois, as linhas de estado, da mais recente para a mais antiga. Nenhuma está no dicionário daqui,
  e nenhuma tem Desfazer (`POST /api/aprendizado/desfazer` daria `404`). `situacao`: `nao_aplicado`
  ("não aplicado neste PC": o par que chegou da nuvem e não vale aqui, com a variante e o termo de
  lá, e o `motivo`, só nele: `dominio_ausente`, `restricao_nao_cabe`, `palavra_ausente_com_dominio`,
  `variante_de_outro_termo`, `termo_desligado`, `nao_agiria`, `palavra_comum` ou
  `arquivo_indisponivel`, os de `internal/dictionary/lote.go`); `desfeito_em_outro_aparelho` (a troca
  deste PC que perdeu na nuvem para um desfazer, e saiu daqui); ou `trocado_em_outro_aparelho` (a troca
  deste PC que perdeu para outro termo). A mesma variante pode ter a linha que vale e a de estado: no
  "trocado em outro aparelho", a que vale é a que veio da nuvem, e a de estado, **a que este PC tinha
  aprendido** (o par perdido, que o espelho guarda com a etiqueta desde a 0008: `perdido_variante` e
  `perdido_termo`; o `05`).
- **A tradução do espelho para o fio.** Os nomes de `situacao` no fio não são os do espelho
  (`storage.SituacaoDoEspelho`); o Agent traduz ao emitir (`situacaoNoFio`). Só estas três saem; as
  outras situações do espelho (`aplicado`, `aplicando`, `desfeito_aqui`, `ja_vale_aqui`, `recusado`)
  **nunca** saem, e o par que vale aqui vem sem `situacao`, mesmo que o espelho o chame de `aplicado`
  ou `ja_vale_aqui`:

  | espelho (`SituacaoDoEspelho`) | fio (`situacao`) |
  |---|---|
  | `nao_aplicado` | `nao_aplicado` (com o `motivo`, o `dictionary.MotivoDoPar`) |
  | `desfeito_em_outro` | `desfeito_em_outro_aparelho` |
  | `trocado_em_outro` | `trocado_em_outro_aparelho` |

  O painel descarta a linha de uma `situacao` que não conhece. O teste do Agent que fixa a tabela é
  `TestAprendizado_ATraducaoDoEspelhoParaOFio` (`internal/agent/localapi/aprendizado_estado_test.go`):
  a linha que vale, sem `situacao`; as três de estado, com os nomes do fio, escritos por extenso; as
  outras cinco, sem linha.
- Não aparecem: as etiquetas com mais de 30 dias (a varredura as tira quando roda; desconectado, ela
  não roda, e o `GET` confere a data também), o par sem texto (o esquecer o apaga, e fica o id), o
  "não aplicado" e o "desfeito em outro aparelho" de um id que está no arquivo (o par do arquivo é o
  que vale; o desfazer da nuvem é o que a varredura seguinte aplica), e o "trocado em outro aparelho"
  sem o par perdido (uma linha de antes da 0008). O "desfeito em outro aparelho" sem o par perdido
  mostra o par da nuvem.
- A lista aparece também quando só tem itens de estado. O espelho que não se lê não derruba a lista:
  ela sai só com o arquivo, e o log registra `aprendizado: não consegui ler o espelho do sync` (sem
  texto).
- **O arquivo indisponível.** Ausente (a criação do vazio falhou, ou o arquivo sumiu com o Agent
  aberto), a lista do arquivo vem vazia (`200`) e as linhas de estado saem, inclusive o "não aplicado"
  com `arquivo_indisponivel`. O arquivo que existe e não se lê (inválido, de outro usuário) é `503`, com
  a frase de sempre, e **as linhas de estado não saem**: a lista vazia diria que não há nada aprendido,
  e a tela esconderia a seção com o que a pessoa precisa desfazer (a revisão do ADR-017). Por isso o
  motivo `arquivo_indisponivel` só aparece com o arquivo ausente; com ele inválido ou de outro
  usuário, a seção fica como estava até ele voltar a abrir (e a varredura seguinte tenta o par de
  novo). O teste é `TestAprendizado_GetComOArquivoIndisponivel`.

**Os nomes que a F8h lê (07-out-2026; `web/src/aprendizado.tsx`); a F8g os emite.** `situacao`:
`nao_aplicado`, `desfeito_em_outro_aparelho` ou `trocado_em_outro_aparelho`. `motivo`, só em
`nao_aplicado`: `dominio_ausente`, `restricao_nao_cabe`, `palavra_ausente_com_dominio`,
`variante_de_outro_termo`, `termo_desligado`, `nao_agiria`, `palavra_comum` ou `arquivo_indisponivel`
(os de `internal/dictionary/lote.go`). O item de estado traz `termo` e `variante`, a troca a que a
linha se refere, e **não está no dicionário daqui**: o painel não oferece o Desfazer nele
(`POST /api/aprendizado/desfazer` daria `404`). A mesma variante pode ter a linha que vale e a de
estado: no "trocado em outro aparelho", a que vale é a que veio da nuvem, e a de estado, a que este PC
tinha aprendido. Uma `situacao` que o painel não conhece não vira linha (o Agent é mais novo que a
página), e um `motivo` que ele não conhece fica sem a frase, nunca com o código cru. Sem os dois, a
linha é a de sempre.

**A tradução do espelho para o fio (F8h, 08-out-2026).** Os nomes de `situacao` no fio **não são** os
do espelho `sync_aprendido` do Agent (`internal/storage/aprendido_sqlite.go`, `SituacaoDoEspelho`); a
F8g traduz ao emitir. Só estas três saem em `situacao`; as demais situações do espelho (`aplicado`,
`aplicando`, `desfeito_aqui`, `ja_vale_aqui`, `recusado`) **nunca** saem, e o par que vale aqui vem sem
`situacao`, mesmo que o espelho o chame de `aplicado` ou `ja_vale_aqui`:

| espelho (`SituacaoDoEspelho`) | fio (`situacao`) |
|---|---|
| `nao_aplicado` | `nao_aplicado` (com o `motivo`, o `dictionary.MotivoDoPar`) |
| `desfeito_em_outro` | `desfeito_em_outro_aparelho` |
| `trocado_em_outro` | `trocado_em_outro_aparelho` |

Isso importa porque o painel descarta a linha de uma `situacao` desconhecida: um nome do espelho sem
tradução some sem erro nem log, e um `aplicado` posto num par que vale tiraria da lista "Aprendido no
painel" o par e o seu Desfazer. A F8g tem um teste do Agent que fixa a tabela (o item que vale, sem
`situacao`; os três de estado, com os nomes do fio; os outros cinco do espelho, sem linha), e o
`web/src/aprendidoEstado.test.tsx` fixa do lado do painel quais três nomes ele reconhece.

#### POST `/api/aprendizado`
O clique em **Aprender**. Uma troca por pedido (com duas sugestões, a tela faz dois pedidos
seguidos). Corpo de no máximo 16 KiB:
```json
{ "id": "uuid do ditado", "de": "sf", "para": "ECF" }
```
`200` com o que ficou gravado (`termo` é a forma canônica do dicionário, `variante` é o "de"):
```json
{ "termo": "ECF", "variante": "sf" }
```
O Agent **não confia na página**. Antes de gravar, confere, nesta ordem:
1. o ditado existe e é do dono da instalação (`404`);
2. o `de` está no `raw_text` **daquele** ditado, como palavras seguidas, sem olhar caixa, acento nem
   pontuação (`400`);
3. o par `de`/`para` foi uma sugestão que o Agent devolveu, **para a versão atual** do ditado
   (`409`). O texto de antes da edição não é guardado, então o motor não roda de novo no clique; o
   que vale é o que ele produziu, em memória;
4. o que o dicionário decide ao gravar (abaixo).

Passando, grava no `dicionario.json` (escrita atômica) como variante do termo, com `"painel"` em
`alias_origem`, e troca o dicionário em memória: vale **a partir do próximo ditado**, sem
reiniciar. Não passando, **nada é gravado**.

| Status | `code` | Quando | Frase da tela (exemplos) |
|---|---|---|---|
| `400` | `BAD_REQUEST` | corpo que não é JSON ou acima de 16 KiB; falta `id`, `de` ou `para` | `Faltou o ditado, o "de" ou o "para".` |
| `400` | `BAD_REQUEST` | o `de` não está no bruto do ditado | `"xyz" não está no que o reconhecimento de voz escreveu nesse ditado.` |
| `400` | `BAD_REQUEST` | só muda maiúscula, acento ou espaço; vazio; grande demais (mais de 4 palavras ou 80 caracteres) | `Só muda maiúscula, acento ou espaço, e isso não se aprende.` |
| `404` | `NOT_FOUND` | o ditado não existe | `Esse ditado não existe mais.` |
| `409` | `CONFLICT` | o par não foi sugerido para esta versão do ditado | `Essa sugestão não vale mais; corrija o ditado de novo para aprender.` |
| `409` | `CONFLICT` | a variante já pertence a outro termo | `"sf" já vira "ECD" no dicionário.` |
| `409` | `CONFLICT` | a variante já é um termo do dicionário | `"sf" já é um termo do dicionário.` |
| `409` | `CONFLICT` | o termo novo já é variante de outro | `"ECF" já é variante de "ECD" e não pode virar termo.` |
| `409` | `CONFLICT` | a variante já é deste termo | `"sf" já vira "ECF" no dicionário.` |
| `409` | `CONFLICT` | o termo está desligado | `"ECF" está desligado no dicionário.` |
| `409` | `CONFLICT` | o dicionário não aplicaria a variante a um ditado | `O dicionário não aplicaria "sf" num ditado. Nada foi gravado.` |
| `503` | `UNAVAILABLE` | não há `dicionario.json`; ele não abre; é de outro usuário; ou o Agent não montou o aprendizado | `Não achei o dicionário do Agent, então não há onde aprender.` |
| `500` | `INTERNAL` | o dicionário resultante não carregaria (nada foi gravado), ou qualquer defeito (inclusive não conseguir ler o ditado) | `Deu um erro dentro do Agent. Tente de novo.` |

**Decisão da F3: a frase do `409` cita o dono** (`"sf" já vira "ECD"`). É o que a tela precisa
mostrar para a pessoa decidir (desfazer a regra velha antes, se foi do painel). O termo é do
dicionário dela, a API é local e atrás do token, e a frase só vai na **resposta**: o dono, o "de" e
o "para" **nunca vão a log**. O `500` não repassa o texto do erro de dentro (pode citar termo ou
caminho): é sempre a frase fixa do defeito, ou a de "não carregaria". Toda `message` é uma frase
inteira, porque a tela a junta a `Não deu para aprender.`: nada de `registro não encontrado` nem
`erro interno` crus.

**Gravado, mas o dicionário em memória não foi trocado.** Se o arquivo foi gravado e a troca em
memória falhou (na prática não acontece: a validação roda a mesma montagem antes de gravar), o que a
pessoa pediu **valeu**: a lista já mostra o par. A resposta é `200`, com o que ficou gravado e
`"vale_ao_reiniciar": true` (o campo só existe nesse caso, também em `/desfazer`), e a tela diz que a
mudança só vale depois de reiniciar o Agent, e não que "não deu para aprender".
```json
{ "termo": "ECF", "variante": "sf", "vale_ao_reiniciar": true }
```

`UNAVAILABLE` (`503`) é um código novo do envelope, só desta rota: "não agora, e não por erro do
pedido".

#### POST `/api/aprendizado/desfazer`
Tira do dicionário uma variante que o painel aprendeu (no aviso, logo depois de aprender, e na lista
das Configurações). É `POST` com o texto no corpo, e não `DELETE` com ele no caminho: a variante é
texto ditado, e não vai para a URL.
```json
{ "variante": "sf" }
```
`200` com `{ "termo": "ECF", "variante": "sf" }` (com `"vale_ao_reiniciar": true` no caso acima).
Sem mais nada ter mudado, o arquivo volta idêntico, byte a byte. `404 NOT_FOUND` se a variante não foi aprendida pelo painel (não existe, ou a pessoa ou
o `dictlearn` a cadastraram: essas não saem por aqui). `400` sem `variante`; `503` como acima.

#### Log
Só contagens e códigos de um conjunto fechado, nunca o texto, o ditado, o "de" ou o "para":
`aprendizado: edição avaliada` (`sugestoes`, `descartes` e `motivos_do_descarte`, os códigos de
`learning.Motivo`), `aprendizado: edição sem sugestão` (`motivo`: `sem_arquivo`, `arquivo_invalido`,
`usuario_diferente`, `erro` ou `versao_mudou_no_meio`), `aprendizado: recusado` (`motivo` e, nos
erros do dicionário, `status`), `aprendizado: aprendido` e `aprendizado: desfeito` (os dois com
`vale_ao_reiniciar`, verdadeiro ou falso). O Agent ainda
registra `aprendizado: dicionário atualizado` (`termos`, `variantes_do_painel`) e, no caso em que o
arquivo foi gravado mas o dicionário em memória não pôde ser trocado, o WARN
`aprendizado: gravado, mas o dicionário em memória não foi trocado` (só `acao`, `aprender` ou
`desfazer`: nem a variante, nem o termo, nem a causa). Ver `09-security-privacy.md`.

**F8g (08-out-2026; ADR-019, "O estado da F8g").** O aplicador do aprendido que chega da nuvem grava
pelo mesmo caminho, com `acao` `sync` nas duas linhas de cima, e registra
`aprendizado: pares da nuvem conferidos` (`pares`, `aplicados`, `desfeitos`, `convergentes`,
`ja_vale_aqui`, `ausentes`, `nao_aplicados`, `nao_aplicados_por_motivo` como `motivo:n,...` da lista
fechada, e `gravou`; em Info só quando gravou, senão em Debug, porque a varredura tenta de novo a
cada rodada o par que continua não valendo aqui). O gancho do painel, quando o espelho do sync não
grava a versão local, registra o WARN
`aprendizado: não consegui anotar a mudança no espelho do sync; a varredura a vê pelo arquivo` (o
erro do storage, sem texto). O log do laço de sync ganha `aprendido_nao_aplicados_por_motivo`, ao
lado das contagens do aprendido da F8f3 e da F8f4. Nunca a variante, o termo ou o domínio.

**A revisão da F8g (08-out-2026).** Na subida, a conferência do dicionário contra o espelho do sync
(`cmd/agent`, `prepararOAprendido`) registra, quando o arquivo é outro (o recriado vazio, ou o de
outro usuário), o INFO `sync: o dicionário é outro arquivo; o aprendido do anterior não vira
desfazer, e o da nuvem volta pela varredura` (`criado_agora`, `pares`: só contagens), e, quando não
consegue conferir, o WARN `sync: não consegui conferir o dicionário do aprendido; o laço o deixa de
lado nesta execução` (ou `... soltar o espelho do dicionário antigo ...`; `etapa` de uma lista
fechada: `ler`, `marcar`, `soltar`), e o WARN `sync: não consegui anotar o dicionário do aprendido`
(`etapa` `anotar`: a subida seguinte confere de novo). Nem o usuário do dicionário vai ao log.

O que **não** existe: rota local de CRUD do dicionário inteiro nem de "Correções"
(`08-ui-ux-spec.md`: essas telas seguem pendentes). O "Adicionar termo" (abaixo, "Termos") só mexe no
que a pessoa põe. O dicionário completo e a Correction que sincroniza eram da etapa 5; a
ADR-019 os deixa para depois, e a etapa 5 sincroniza só o par aprendido (F8).

### Termos
ADR-020 (01-out-2026). Fonte da verdade: `internal/agent/localapi/termos.go`. O "Adicionar termo" das
Configurações: a pessoa digita a palavra certa, opcionalmente as grafias erradas que o reconhecimento
de voz escreve, e diz se a palavra vira **dica** (vai no prompt do ASR). Atrás da mesma cadeia das
demais rotas `/api/...`, com o mesmo envelope de erro e `Cache-Control: no-store` em toda resposta.
Tudo por `POST`, com o texto no corpo (no máximo 16 KiB): **nenhum texto vai na URL**. Não há ditado a
conferir: quem confere é o dicionário. Toda mudança grava no `dicionario.json` (escrita atômica) e
troca o dicionário **e o prompt do ASR** em memória: vale **a partir do próximo ditado**, sem
reiniciar. Não passando, **nada é gravado**.

#### GET `/api/termos`
O que a pessoa adicionou, na ordem do arquivo, e o orçamento das dicas dela:
```json
{
  "items": [
    { "termo": "RAG", "variantes": ["hage", "rague"], "dica": true, "do_dicionario": false },
    { "termo": "DCTFWeb", "variantes": ["dctf web"], "dica": false, "do_dicionario": true }
  ],
  "dicas": { "usadas": 3, "orcamento": 140 }
}
```
- `variantes`: só as grafias que a pessoa pôs (nunca `null`).
- `dica`: o termo está nas dicas dela. Falso num termo `do_dicionario`.
- `do_dicionario`: o termo já era do dicionário; ela só pôs grafias nele, e a dica dele não muda pelo
  painel.
- `dicas.usadas`: os caracteres que as dicas dela ocupam no prompt, com o `", "` entre elas; elas vão
  primeiro no prompt, então é o que de fato gastam. `orcamento`: 140.

Sem o Agent ter montado o "Adicionar termo", ou sem `dicionario.json`, `503` (a lista **não** vem
vazia: a tela tem de dizer que não há onde adicionar).

#### POST `/api/termos`
```json
{ "termo": "RAG", "variantes": ["rague", "hage"], "dica": true }
```
Se o termo existe (comparado sem caixa, acento nem espaço): de origem `"adicionado"`, as grafias novas
entram e a dica segue o pedido; de origem `"painel"`, é adotado; do dicionário de antes, só as grafias
entram e a dica dele não muda: com grafias e `"dica": true`, a resposta traz `"dica_ignorada": true`;
sem grafias e com a dica, é `409` (abaixo), e não "já está assim".

#### POST `/api/termos/dica`
```json
{ "termo": "RAG", "dica": false }
```
Só num termo que a pessoa adicionou. Pedir o estado em que ele já está não é erro.

#### POST `/api/termos/remover`
```json
{ "termo": "RAG" }
```
Tira as grafias que a pessoa pôs e, se o termo é dela, o termo. Se o termo tinha variantes aprendidas
pelo painel, ele fica, de volta ao painel. Num termo de antes, só as grafias dela saem.

#### Resposta de toda mutação
`200`, com a lista inteira (a tela só troca o estado, sem pedir de novo) e o que a mudança fez:
```json
{
  "items": [ ... ], "dicas": { "usadas": 3, "orcamento": 140 },
  "termo": "RAG", "ja_existia": false, "sairam_das_dicas": ["eSocial"]
}
```
- `termo`: a forma como ficou no dicionário.
- `sairam_das_dicas`: os termos do dicionário que saíram do prompt com a mudança (nunca `null`).
- `vale_ao_reiniciar: true` só quando o arquivo foi gravado e a troca em memória não (o mesmo caso de
  `/api/aprendizado`).
- `dica_ignorada: true` só no Adicionar de um termo do dicionário de antes com a dica pedida: as grafias
  entraram, a dica dele não mudou, e a tela diz. Ausente no caso comum.

#### Erros
| Status | `code` | Quando | Frase da tela (exemplos) |
|---|---|---|---|
| `400` | `BAD_REQUEST` | corpo que não é JSON ou acima de 16 KiB; sem `termo` | `Faltou a palavra certa.` |
| `400` | `BAD_REQUEST` | palavra certa com quebra de linha, mais de 4 palavras ou 40 caracteres | `A palavra certa vai até 40 caracteres e 4 palavras, numa linha só.` |
| `400` | `BAD_REQUEST` | grafia de menos de 2 ou mais de 40 caracteres, mais de 4 palavras, ou com quebra | `“r” não serve: cada grafia tem de 2 a 40 caracteres, até 4 palavras, numa linha só.` |
| `400` | `BAD_REQUEST` | a palavra certa só de ligação, de polaridade ou número (`learning.Arriscada`); a grafia, além disso, só de palavras funcionais frequentes ("eu", "né", "isso") (`learning.GrafiaArriscada`) | `“das” é palavra comum demais: trocaria texto certo.` / `“2025” é só número: trocaria texto certo.` |
| `400` | `BAD_REQUEST` | a grafia tem palavra de polaridade em qualquer lugar ("não", "nunca", "mais") | `“não rague” tem palavra que muda o sentido da frase, e ela sumiria do texto.` |
| `400` | `BAD_REQUEST` | a grafia só muda maiúscula, acento ou espaço | `“rág” só muda maiúscula, acento ou espaço, e o dicionário já entende isso.` |
| `400` | `BAD_REQUEST` | os tetos: 5 grafias por pedido, 10 dela por termo, 50 termos | `Dá para mandar até 5 grafias erradas por vez.` |
| `400` | `BAD_REQUEST` | nada a mudar | `“ECF” já está no dicionário assim.` |
| `409` | `CONFLICT` | só a dica, num termo do dicionário de antes | `“ECF” já é do dicionário: a dica dele não muda por aqui.` |
| `404` | `NOT_FOUND` | dica ou remover de um termo que a pessoa não adicionou | `“ECF” não foi adicionado por você.` |
| `409` | `CONFLICT` | a grafia já é de outro termo, ou é outro termo | `“SF” já vira “ECF” no dicionário.` |
| `409` | `CONFLICT` | o termo novo já é grafia de outro | `“sf” já vira “ECF” no dicionário e não pode virar termo.` |
| `409` | `CONFLICT` | o termo está desligado | `“X” está desligado no dicionário.` |
| `409` | `CONFLICT` | a dica não cabe | `Não cabe nas dicas: sobram 3 caracteres.` |
| `409` | `CONFLICT` | uma grafia nova não agiria num ditado | `O dicionário não aplicaria “Viti” num ditado. Nada foi gravado.` |
| `409` | `CONFLICT` | com a mudança, um alias que agia deixaria de agir | `Com “RAG”, “sf” deixaria de virar “ECF”. Nada foi gravado.` |
| `409` | `CONFLICT` | com a mudança, a forma canônica de outro termo, escrita certa, passaria a ser trocada quando o contexto cita o termo mudado | `Com “Vite”, “Vitti”, escrito certo, passaria a virar “Vite”. Nada foi gravado.` |
| `409` | `CONFLICT` | a grafia do próprio termo só age com a dica ligada (desligar a dica, ou readicionar sem ela) | `“Viti” só vira “Vity” com a dica ligada. Para desligá-la, remova “Vity” e adicione de novo sem essa grafia. Nada foi gravado.` |
| `503` | `UNAVAILABLE` | não há `dicionario.json`; ele não abre; é de outro usuário; ou o Agent não montou os termos | `Não achei o dicionário do Agent, então não há onde adicionar.` |
| `500` | `INTERNAL` | o dicionário resultante não carregaria, ou qualquer defeito | `Deu um erro dentro do Agent. Tente de novo.` |

A frase do "quebraria" muda com o pedido: `Mudando a dica de “RAG”, …` e `Sem “RAG”, …`.

#### Log
Só contagens e códigos fechados, nunca o termo nem a grafia: `termos: adicionado`, `termos: dica` e
`termos: removido` (`novas`, `ja_existia`, `dica_ignorada`, `sairam`, `termos`, `vale_ao_reiniciar`), e
`termos: recusado` (`acao`, `motivo` e `status`; os motivos novos são `palavra_comum`, `sem_espaco`,
`quebraria`, `limite`, `nada_a_mudar`, `dica_do_dicionario` e `nao_adicionado`, além dos de
`/api/aprendizado`). O Agent
registra `aprendizado: dicionário atualizado` com `acao` (`adicionar_termo`, `dica_do_termo`,
`remover_termo`), `termos_no_prompt` e `vocabulario_runas`.

### Nuvem (etapa 5; ADR-019)
O que a API local ganha para conectar o PC à nuvem. Contrato final nas fatias F6 e F7.
- Em Configurações: a URL da nuvem (vazia por padrão), **Conectar à nuvem**, o estado ("conectado
  como …", último envio, "desconectado") e **Desconectar**. A prévia do envio inicial (quantos
  ditados vão subir) vem antes de qualquer envio.
- `GET /nuvem/retorno?code&state`: onde a página de aprovação da nuvem devolve a autorização (F6).
  Atrás do `Host`, como os arquivos do painel (o navegador chega aqui por redirecionamento, sem o
  token); vale só com o `state` do pedido em curso, uma vez. É um `GET` que muda estado por desenho
  (o retorno a loopback da RFC 8252), e o `state` de uso único é o que o protege. O `code` não vai
  para log.
- `/sw.js` responde `404` (F5): o painel local nunca registra service worker.

**Como a F6 fechou (05-out-2026; ADR-019, "Decisões da F6"; `internal/agent/nuvem` e
`internal/agent/localapi/nuvem.go`).** As quatro rotas pedem o token da Local API, como o resto de
`/api`, e respondem `Cache-Control: no-store`. **Nenhuma resposta traz o token do aparelho.** Um
Agent sem a nuvem montada responde `503` (`UNAVAILABLE`).

- O estado (`GET /api/nuvem`, e a resposta das três abaixo): `{ "estado": "desconectado" |
  "conectando" | "conectado", "url": "<a nuvem desta instalação, para o campo>", "email"?, "nome"?
  (conectado), "conferencia"?, "abrir_url"? (conectando), "aviso"? }`. O `aviso` é um de `revogado`
  (a nuvem respondeu `401`), `sem_resposta` (desconectou aqui, a nuvem não confirmou), `nao_abriu` (o
  navegador não abriu), `vencido` (o pedido venceu), `outra_conta` (a D3), `falhou` (a troca não deu)
  e `sem_rede` (a conferência não alcançou a nuvem; continua conectado). Conectado é o aparelho
  gravado na tabela `instalacao` **e** o token no Gerenciador de Credenciais; sem o token, a
  instalação é limpa e o estado é desconectado.
- `POST /api/nuvem/conectar` `{ "url": "..." }` (corpo até 1 KiB): a URL é conferida antes de qualquer
  rede (só `https`; o build de desenvolvimento só no modo portátil, com `https` ou
  `http://localhost:<porta>`; sem usuário, caminho, busca ou `#`; até 256 caracteres) e normalizada
  para a origem. Pede o pareamento à nuvem, abre a página de aprovação no navegador (atrás do portão
  `sysinput`) e devolve o estado `conectando`. O navegador que não abriu é `200` com o aviso
  `nao_abriu`. Recusas, com a frase para a tela: `400` (a URL), `409` (já conectado, ou outra nuvem: a
  D3), `502` (a nuvem não respondeu direito), `503` (o painel ainda não subiu).
- `POST /api/nuvem/conferir`: com o PC conectado, pergunta à nuvem (`GET /api/v1/me` com o token),
  no máximo uma vez a cada 30 s; o `401` apaga o token e desconecta (aviso `revogado`); a rede fora,
  não (aviso `sem_rede`). Sem o PC conectado, só devolve o estado. O painel o chama ao abrir as
  Configurações e ao voltar à aba (sem laço).
- `POST /api/nuvem/desconectar`: com o pedido em curso, o Cancelar. Conectado: pede à nuvem que
  revogue o próprio aparelho (`DELETE /api/v1/devices/{id}` com o token, 5 s) e **sempre** apaga o
  token daqui e o aparelho da instalação (ficam a URL, a conta e o e-mail). Sem a confirmação da
  nuvem, o aviso `sem_resposta`.
- `GET /nuvem/retorno?code&state`: o `state` errado é `400` e **não consome** o pedido; o certo o
  consome antes da rede (uso único), confere o prazo (o da nuvem, nunca mais de 10 minutos), troca o
  `code` com o verificador e responde uma página fixa (`text/html`, `no-store`, CSP própria `default-src
  'none'; style-src 'unsafe-inline'`), que não ecoa nada: "PC conectado" / "Pode fechar esta aba e
  voltar ao painel." (`200`), ou "Não deu para conectar" / "Volte ao painel do PC e clique em Conectar
  à nuvem de novo." O mesmo retorno de novo, depois de dar certo (a página recarregada), mostra "PC
  conectado" sem trocar nada. A D3: a troca que traz outra conta revoga o aparelho recém-criado com o
  token novo, não o guarda e responde `409`.
- `/api/ia/chave` não alcança o token: `nuvem` e `nuvem-portatil` não são provedores (`400`).

**F7c (06-out-2026; ADR-019, "Decisões da F7c"): o envio inicial, o esquecer e o selo no PC.**
- O estado ganha, com uma nuvem gravada (conectado, ou desconectado com a URL) e sem pedido em curso,
  `historico`: `{ "previa": 800, "aceito": false, "envio"?: { "feitos": 120, "total": 800 },
  "nao_subiram": 2, "pendentes": 680, "mudancas": 1, "conferido_em"?: "<RFC 3339>", "falhou": false,
  "outra_nuvem": false }` (só contagens e a hora da última rodada que deu certo: nunca texto, id ou
  caminho). `pendentes` são todos os marcadores da fila (o "{n} para enviar."); `mudancas`, só os
  que levam uma mudança feita aqui num ditado que a nuvem tem (a edição, a lixeira, o restaurar, a
  escolha do selo e a reconciliação), sem o histórico e o ditado novo que não subiram: é o número da
  pergunta do esquecer (reconferência da F7c). Desconectado com a URL, também o `email` (o esquecer diz de qual conta). O painel relê o
  `GET /api/nuvem` a cada 2 s só enquanto há `envio` e a seção está à vista: é leitura local, sem rede.
  **F8 (07-out-2026; ADR-019, "Decisões da F8", D3):** o `historico` ganha a contagem das trocas
  aprendidas que subiriam (o M de "Vão subir N ditados e M trocas aprendidas"), e a pergunta aparece
  também com `previa` 0 e M maior que 0. **O nome do campo (F8h, 07-out-2026): `previa_trocas`**, um
  inteiro: ausente (um Agent de antes da F8) vale 0. O painel conta `previa + previa_trocas` para
  decidir se pergunta, e também para esperar a primeira rodada com a conexão
  ("Conferindo a nuvem…"). A F8g o emite sempre, no estado que a última varredura deixou no espelho (0
  depois do aceite); até a revisão da F8g (08-out-2026) ele saía como `trocas`, um nome que a web não
  lê, e a pergunta com N = 0 não aparecia. Para isso, a rodada só grava o aceite
  sozinha (`aceito` verdadeiro sem o clique) quando não há ditado elegível **e** M é 0: hoje ela o
  grava só pelo histórico vazio, e com isso as trocas subiriam sem pergunta. O aprendido não entra no
  `envio` (não é inicial). O `mudancas` passa a contar também os marcadores do aprendido de uma
  mudança feita aqui (a troca aprendida desfeita ou trocada, com a versão local acima da base: o
  `ResumoDoEnvio.Mudancas` de `internal/storage/envio_sqlite.go`, F8e); o marcador que o epoch novo põe
  num par que a nuvem já tinha confirmado não conta, como no ditado.
  **O aviso do freio (F8h, 08-out-2026): `freio_trocas`**, um inteiro, opcional, também no `historico`:
  quantos pares saíram do `dicionario.json` de uma vez, sem o Desfazer do painel, na **última**
  varredura que comparou o arquivo (`syncengine.Laco.Freio`, do `Relatorio.Freio` da rodada; sempre
  maior que 10 quando há), e que por isso nada do aprendido grava nem sobe. Só conectado, só
  contagem, e só em memória: ausente sem o freio, e também antes da primeira varredura desta execução
  do Agent; some na varredura que o solta (o arquivo reposto). Uma rodada com o arquivo indisponível
  não compara nada, e não o muda. Ausente vale `0`, e o painel não avisa.
- `POST /api/nuvem/enviar`: o "Enviar o histórico". Só conectado (`409` "Conecte o PC à nuvem antes
  de enviar o histórico.") e só depois da primeira rodada que deu certo com esta conexão (`409` "O PC
  ainda está conferindo a nuvem. Tente de novo em alguns segundos."). Faz a cópia do banco (uma por
  conta) e põe os ditados elegíveis na fila; acorda o laço; devolve o estado. A cópia que falha não
  marca nada: `409` (cópias demais), `507` (disco cheio) ou `500`, com a frase ("Não deu para copiar o
  histórico. Nada foi enviado." e o motivo). Já aceito, não faz nada e devolve o estado. **F8 (D3):**
  com N = 0 e M > 0, grava o aceite sem cópia (a cópia é do histórico, e não há ditado a enviar), e
  as trocas sobem pela rodada que ele acorda. Antes dele, nenhuma troca nova sobe (só o desfazer de
  um par que a nuvem tem). O log do envio diz `marcados` e `trocas`, só as contagens.
- `POST /api/nuvem/esquecer`: o "Esquecer esta nuvem". Só desconectado e sem pedido em curso (`409`
  "Desconecte antes de esquecer a nuvem." / "Cancele o pedido em curso antes de esquecer a nuvem.");
  sem nuvem gravada, só devolve o estado. Tira o token do Gerenciador de Credenciais se ainda estiver
  lá (pelo portão), e esquece numa transação as chaves da conexão, o cursor, a fila e o que é do sync
  nas linhas; devolve o estado desconectado, com a URL vazia. As mudanças que estavam na fila
  (`historico.mudancas`) ficam nos ditados deste PC, anotadas; de volta à mesma conta, sobem pela
  reconciliação (revisão e reconferência da F7c).
- `PATCH /api/history/{id}` aceita `{ "base_version": 3, "resolver_conflito": "manter" | "trocar" }`,
  sem `final_text` (os dois juntos, ou uma escolha fora da lista, são `400`); sem selo, ou com a base
  velha, `409`; dá certo: `200` com o item (sem `sugestoes`). O item da Local API passa a trazer
  `versao_em_conflito` (`final_text`, `device_id`, `conflict_at`; ausente sem selo), como o da
  nuvem.

**A chegada mais rápida no PC (ADR-021, decisão P, subfatia S; 10-out-2026).** Fonte da verdade:
`internal/agent/localapi/nuvem.go` (`handleMudancasNuvem`) e `internal/sync/laco.go`
(`Laco.LerMudancas`); em divergência vale o código. A rota pede o token da Local API, como o resto de
`/api`, e responde `Cache-Control: no-store`.
- `GET /api/nuvem/mudancas`: `{ "contador": 7 }`. **Um inteiro e mais nada: nenhuma resposta traz texto,
  id, versão, URL, e-mail ou token**, nem nenhum outro campo. Só `GET` (os outros métodos são `405`), sem
  corpo nem query. É leitura de memória: não vai à nuvem nem ao banco.
- **O que o contador conta.** Quantas rodadas do sync, desde que o Agent subiu, gravaram no histórico
  deste PC algo que veio da nuvem: sobe **1 por rodada** (e não 1 por ditado), depois de a gravação ter
  ido para o SQLite, mesmo na rodada que falhou mais adiante. Conta o pull que trouxe um ditado novo ou
  mudado (o ditado do celular, a edição feita lá, a lixeira, o restaurar) ou uma lápide que apagou a
  linha daqui, e o push que a nuvem respondeu com a lápide (apagar vence) ou com o estado que venceu um
  conflito. A rodada que não gravou nada da nuvem (o eco do que este PC mesmo enviou, a página vazia, o
  aprendido) não o mexe. Só em memória: recomeça de 0 a cada execução do Agent, e quem lê compara com a
  leitura anterior, sem supor que ele só cresce.
- **A leitura é o aviso de "painel aberto".** Depois de cada leitura, durante 15 s (a janela), o laço do
  sync busca a nuvem a cada **10 s**; passados 15 s sem leitura, volta aos **60 s** de antes (ADR-019,
  "Decisões da F7a/b", decisão 1; a escolha do Mauro, escolha menor 17 da ADR-021). A primeira leitura
  depois de uma janela sem nenhuma também acorda o laço: a próxima busca sai 10 s depois da última (ou
  já, se isso passou), e não espera os 60 s que sobravam. Depois de uma falha de rede o laço segue o
  backoff, e desconectado ele não gira a cada 10 s: o painel aberto não fura nenhum dos dois.
- **O painel** (`web/src/mudancas.ts`) lê a rota a cada 3 s só com a aba à vista, uma leitura por vez,
  e relê a lista quando o número é outro que o da leitura anterior (inclusive um número menor: o Agent
  reiniciou). Escondida, a aba para de ler, e ao voltar lê já. A leitura que falha é ignorada (a faixa do
  topo é da lista).
- `503` (`UNAVAILABLE`, "A nuvem não está disponível neste Agent."): o Agent sem o laço do sync (sem
  histórico). Um Agent de antes da S responde `404` sem frase, e o painel para de perguntar.
- A nuvem (`/api/v1`) não tem esta rota: o celular continua relendo a lista ao voltar à aba.

### Retenção no PC (etapa 5; ADR-019, F10)
Os 6 meses do histórico no PC e as cópias do banco. **Decidido em 08-out-2026 (ADR-019, "Decisões da
F10") e implementado na F10d e na F10e.** A fonte da verdade é
`internal/agent/localapi/retencao.go`, e em divergência vale o código. As rotas pedem o token da Local
API, como o resto de `/api`, e respondem `Cache-Control: no-store`. **Só números e booleanos: nenhuma
resposta traz texto, id ou caminho.** Erros no envelope da Local API (`CONFLICT`, `INTERNAL`,
`UNAVAILABLE`), com a `message` em português para a tela. Um Agent sem a retenção montada responde
`503` (`UNAVAILABLE`); um Agent de antes da F10 responde `404`, e o painel mostra "Este Agent ainda
não tem os 6 meses. Reinicie o Agent." A retenção não depende da nuvem: vale no PC que nunca conectou.
- `GET /api/retencao`: `{ "previa": 1240, "na_lixeira": 3, "com_selo": 0, "de_outro_dono": 0,
  "ligada": false, "pausada": false, "copias": 2, "copias_apagaveis": 2, "copia_dos_6_meses": false }`.
  `previa` é quantos ditados **do dono** o OK
  apagaria agora (os fora da janela de 6 meses, ativos ou na lixeira); `na_lixeira` e `com_selo` são
  subconjuntos dela (o selo, "2 versões", sai sem a escolha de quem usa); `de_outro_dono` são os fora
  da janela de outro `user_id`, que **ficam** e não entram em `previa`; `ligada` é o OK gravado;
  `pausada`, o freio ter pausado a regra (o Agent ficou mais de 7 dias parado, ou o relógio pulou);
  `copias`, quantos arquivos `historico.db.antes-(do-dono|da-nuvem|dos-6-meses)-…db` há ao lado do
  banco; `copias_apagaveis`, quantas delas o botão apaga agora (todas menos a do envio do histórico à
  nuvem que ainda não terminou: a tela pergunta por estas e, sem nenhuma, não oferece o botão);
  `copia_dos_6_meses`, se a cópia de um OK anterior (a registrada) está ao lado do banco: o próximo OK
  não copia de novo, e o que nasceu depois dela não está nela (a tela diz isso em vez de prometer uma
  cópia nova; revisão da F10). A contagem é feita ao vivo, sem rede.
- `POST /api/retencao/ligar` `{ "previa": 1240 }` (corpo até 1 KiB): o OK. Dentro do mutex do ciclo do
  expurgo, nesta ordem: reconta com um `agora` fixo; faz a cópia do banco e a confere (uma só, a
  `antes-dos-6-meses`, registrada: um OK seguinte não copia enquanto ela existir); numa transação só,
  grava o OK e a hora da rodada e apaga o primeiro lote (o método que apaga lê o OK dentro da
  transação: não há como apagar sem ele); apaga os lotes seguintes (de 500, por cursor, com o mesmo
  `agora`). `200` com o estado do `GET`, `apagados` (**igual à recontagem**) e `"incompleto": false`.
  Se um lote **seguinte** falha (o OK e o primeiro lote já confirmados), não é recusa: a regra está
  ligada, e a resposta é `200` com `"incompleto": true`, os `apagados` até ali e, no `previa` do
  estado, o que falta, que sai na rodada seguinte do ciclo (em até 1 hora). Já ligado: `200` sem fazer
  nada. Recusas, com a frase para a tela:
  - `409` "Não há nada a apagar agora." (a recontagem deu 0: nem cópia, nem OK);
  - `409` quando a recontagem difere do `previa`, com o número novo em `previa`, ao lado do envelope
    de erro (`{ "error": {…}, "previa": 1301 }`); a tela troca a frase e pergunta de novo;
  - a cópia que falha não apaga nem grava nada: `409` (já há 3 cópias da marca; "Não deu para copiar o
    histórico. Nada foi apagado." e o motivo), `507` (falta espaço no disco) ou `500`;
  - `409` sem o dono fixo do histórico (o passo dos 6 meses só roda com ele): "O histórico deste PC
    ainda não está pronto para isso. Reinicie o Agent.";
  - `400` quando o corpo não traz `previa` (ou ela é negativa ou não é número): nada é pedido ao ciclo;
  - `503` com o ciclo parado (o Agent está fechando o banco).
  O `GET` sem o dono fixo devolve tudo zerado. As frases da cópia que falha são "Não deu para copiar o
  histórico. Nada foi apagado." e, quando se sabe o motivo, a ela se junta "Já há 3 cópias de antes dos 6
  meses ao lado do histórico: apague as que não quiser." (`409`) ou "Falta espaço no disco." (`507`,
  código `INTERNAL`).
- `POST /api/retencao/desligar`: tira o OK (volta ao de hoje: o PC guarda tudo, e o que saiu não
  volta). Dentro do mesmo mutex. Devolve o estado. Idempotente.
- `POST /api/retencao/copias/apagar` (F10e): apaga só os arquivos cujo nome casa **exatamente** com
  `historico.db.antes-(do-dono|da-nuvem|dos-6-meses)-AAAAMMDD-HHMMSS.db`, na pasta do banco (nunca o
  `historico.db`, o `-wal`, o `-shm`, outro `.db`, uma pasta ou um link com o nome da cópia). Guarda a
  cópia do envio do histórico que ainda não terminou (a `nuvem_copia` sem o
  `nuvem_envio_aceito_em`): só a registrada fica; as outras `antes-da-nuvem` saem. Apagada a cópia dos
  6 meses, a `retencao_6m_copia` sai e o próximo OK copia de novo. `200` com o estado e
  `{ "apagadas": 2, "ficaram": 1 }` (o resto do estado vem achatado, como no Ligar). Um arquivo que o
  sistema não deixou apagar não para os outros: `500` "Nem todas as cópias saíram. O que saiu não volta."
  (ou "Não deu para apagar as cópias agora." quando nenhuma saiu), sem o caminho. O Agent nunca as apaga
  sozinho; a pergunta fica na tela.
- O **ciclo** não tem rota: roda na subida e de hora em hora, sem OK não apaga nada dos 6 meses, e o
  log só tem contagens.

### Arquivos do painel
- Servidos da pasta `painel\` ao lado do executável — é para onde vai o
  `pnpm -C C:/Desenvolvimento/aurora-whisper/web build` (`bin/painel`).
- Caminho que não é arquivo devolve o `index.html` (aplicação de página única). O `index.html`
  sai com `Cache-Control: no-store`. **Exceção da etapa 5 (ADR-019, F5):** `/sw.js` dá `404`.
- Sem `index.html` na pasta: `503`, com uma página que ensina a construir o painel. Não é
  preciso reiniciar o Agent: depois do build, abrir de novo pela bandeja basta.

### Cabeçalhos em toda resposta
- `Referrer-Policy: no-referrer` — a página abre com o bilhete na URL; se carregasse algo de
  fora, o `Referer` levaria o bilhete junto.
- `X-Content-Type-Options: nosniff` e `X-Frame-Options: DENY`.
- `Content-Security-Policy: default-src 'self'; connect-src 'self'; img-src 'self' data:;
  frame-ancestors 'none'; base-uri 'none'; form-action 'none'` — nada de script embutido nem
  recurso de fora (THR-015).
- Exceção, só na página `503` do painel não construído: ela se formata com `style` embutido, que
  a CSP acima bloqueia. Outra frente da revisão de 29-set-2026 dá a essa resposta uma CSP
  própria, ainda mais fechada no resto (`default-src 'none'; style-src 'unsafe-inline'; ...`).
  A CSP do painel construído não muda.
