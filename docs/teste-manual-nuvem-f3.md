# Teste manual da nuvem, F3: o servidor no PC e o login Google

ADR-019, fatia F3. O servidor da nuvem sobe no seu PC, em `http://localhost:8080`, sobre o Postgres
de teste, e você entra com a sua conta Google no Chrome. Tempo estimado: 15 minutos.

**Atalho (05-out-2026):** com o client Google criado e o JSON baixado em Downloads, um comando só faz
as seções 1 a 4 (acha e move o JSON, roda o `migrate`, gera as chaves e sobe o servidor):

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\nuvem-local.ps1 -Email SEU_EMAIL@gmail.com
```

Depois siga da seção 5 em diante. As seções 1 a 4 abaixo ficam como o caminho passo a passo.

**Desde a F4 (05-out-2026):** a página provisória deu lugar ao app (o painel no modo nuvem), e o
servidor precisa do build dele (`PAINEL_DIR`, seção 3). O atalho acima já compila o build. Os
passos abaixo usam as telas novas; o roteiro delas, sem Google e sem Docker, é o
`docs/teste-manual-nuvem-f4.md`.

Tudo é PowerShell 5.1, um comando por bloco, sempre com o caminho completo do repositório
(`go -C C:\Desenvolvimento\aurora-whisper ...`). Rode todos **na mesma janela** do PowerShell: as
variáveis `$env:...` valem só nela. Nenhum segredo vai para o repositório nem para o chat.

## O que você precisa antes

- **O Postgres de teste ligado** (o mesmo da F1 e da F2). Se não estiver, com o Docker Desktop aberto:

  ```powershell
  powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1
  ```

- **O client Google de desenvolvimento**, do tipo **Aplicativo da Web**, com o URI de redirecionamento
  autorizado exatamente `http://localhost:8080/auth/google/callback`. Baixe o JSON dele (o botão de
  download no Google Cloud, em "Credenciais") e guarde **fora do repositório**. A sugestão é a pasta
  que o script do Postgres já usa, com um nome fixo:

  ```powershell
  Move-Item "$env:USERPROFILE\Downloads\client_secret_*.json" "$env:USERPROFILE\.aurora-whisper-dev\google-client-dev.json"
  ```

  (Se houver mais de um `client_secret_*.json` em Downloads, mova o certo pelo nome completo.)

- **Na tela de consentimento** do Google Cloud, em modo de teste, a sua conta **e uma segunda conta**
  em "Usuários de teste". A segunda serve para ver a recusa da Aurora; sem ela na lista do Google, o
  próprio Google barra antes (ver "Outra conta", abaixo).

- **O Chrome** (ou o Edge). Ver "Navegadores", no fim.

## 1. O banco

O endereço do Postgres de teste (com a senha) já está num arquivo seu, fora do repositório. Ele aponta
para o banco `postgres` do contêiner descartável.

```powershell
$env:DATABASE_URL = (Get-Content "$env:USERPROFILE\.aurora-whisper-dev\postgres-de-teste.url" -Raw).Trim()
```

```powershell
go -C C:\Desenvolvimento\aurora-whisper run ./cmd/server migrate
```

O que você deve ver: `migrate: 19 aplicadas agora, 0 já estavam aplicadas` (ou `nada a aplicar`, se já
rodou; as 19 incluem a 0017, a 0018 e a 0019, do ditado pelo celular, ADR-021). Se ele recusar dizendo que a `0001_users_table.sql` **mudou** depois de aplicada, é porque esse
banco recebeu a versão antiga dela (a F3 a corrigiu no lugar): recrie o contêiner, que é descartável,
leia o endereço de novo (o primeiro comando desta seção) e rode o `migrate` outra vez.

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1 -Parar
```

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1
```

## 2. Os segredos do servidor

Gerados na hora, só nesta janela. Cada um é 32 bytes aleatórios; os dois têm de ser diferentes.

```powershell
$b = New-Object byte[] 32; [System.Security.Cryptography.RNGCryptoServiceProvider]::new().GetBytes($b); $env:SESSION_SECRET = [Convert]::ToBase64String($b)
```

```powershell
$b = New-Object byte[] 32; [System.Security.Cryptography.RNGCryptoServiceProvider]::new().GetBytes($b); $env:DEVICE_TOKEN_PEPPER = [Convert]::ToBase64String($b)
```

Fechar a janela invalida as sessões abertas (o servidor não reconhece mais o cookie): é esperado.

## 3. O Google, a lista e o endereço

```powershell
$env:GOOGLE_CLIENT_SECRET_FILE = "$env:USERPROFILE\.aurora-whisper-dev\google-client-dev.json"
```

Troque `SEU_EMAIL@gmail.com` pela conta que vai entrar (só ela; a segunda conta fica de fora de
propósito):

```powershell
$env:CONTAS_PERMITIDAS = "SEU_EMAIL@gmail.com"
```

```powershell
$env:PUBLIC_BASE_URL = "http://localhost:8080"
```

O app (F4): compile o painel no modo nuvem numa pasta fora do repositório e aponte o servidor para
ela.

```powershell
pnpm -C C:\Desenvolvimento\aurora-whisper\web exec vite build --mode nuvem --outDir "$env:USERPROFILE\.aurora-whisper-dev\painel-nuvem" --emptyOutDir
```

```powershell
$env:PAINEL_DIR = "$env:USERPROFILE\.aurora-whisper-dev\painel-nuvem"
```

O `HTTP_ADDR` fica no padrão, `127.0.0.1:8080` (só o próprio PC).

## 4. Subir o servidor

```powershell
go -C C:\Desenvolvimento\aurora-whisper run ./cmd/server serve
```

O que você deve ver, em JSON, uma linha por evento: `"msg":"servindo","endereco":"127.0.0.1:8080"`,
`"contas_permitidas":1` e uma `"msg":"arrumação"`. A janela fica presa no servidor; deixe-a aberta.

Se faltar ou estiver errada alguma variável, ele sai na hora com uma linha `serve: <VARIÁVEL>: ...`
que diz o que falta, nunca o valor. Por exemplo, se o JSON não tiver o redirect
`http://localhost:8080/auth/google/callback`, a mensagem diz isso, e a correção é no Google Cloud
(acrescente o URI e baixe o JSON de novo).

## 5. Entrar

1. No Chrome, abra **`http://localhost:8080`**. Tem de ser `localhost`: em `127.0.0.1` o servidor
   responde `host inválido`, porque o cookie e o redirect do Google são de `localhost`.
2. A tela Entrar diz "Entre para ver o que você ditou." Clique em **Entrar com o Google** (o botão
   passa a "Abrindo o Google…").
3. No Google, escolha a sua conta. Em modo de teste, ele pode avisar que o app não foi verificado:
   clique em "Continuar".
4. Você volta para o app, no **Histórico** (vazio: "Nenhum ditado ainda."). Em **Configurações**,
   "Entrou como" o seu e-mail e, em "Sessões abertas", **"Windows · Chrome"** com a etiqueta
   **"este aparelho"**.

No log do servidor, as linhas do login: `"rota":"POST /auth/start","status":200` e
`"rota":"GET /auth/google/callback","status":303`. Nenhuma linha traz o seu e-mail, o código, o
state nem o cookie.

Se quiser conferir o cookie: F12, Aplicativo, Cookies, `http://localhost:8080`. O `__Host-sessao`
tem `HttpOnly`, `Secure` e `SameSite` `Lax`, e o `__Host-pre` sumiu.

Com o F12 aberto, um `401` vermelho de `GET /api/v1/me` no console, **antes** de entrar, é normal: é
a página perguntando se já há sessão. Depois de entrar, o mesmo pedido dá `200`.

## 6. Sessões e sair

1. Abra uma **janela anônima** do Chrome (Ctrl+Shift+N), em `http://localhost:8080`, e entre de novo
   com a mesma conta.
2. Na primeira janela, em **Configurações**, são **duas** sessões. Clique em **Encerrar** na que não
   é "este aparelho".
3. Na janela anônima, clique em **Lixeira**: a tela vira "Sua sessão terminou." Feche a janela
   anônima.
4. Na primeira janela, em **Configurações**, clique em **Sair**: ela volta para "Entrar". No log:
   `"rota":"POST /auth/logout","status":204`.

## 7. Outra conta

1. Clique em **Entrar com o Google** e escolha a **segunda conta**.
2. O que você deve ver: a página **"Esta conta não tem acesso a esta nuvem."** (status 403), com
   **Entrar com outra conta**. Nada foi criado com ela: nem usuário, nem sessão.
3. No log: `"msg":"login_recusado","motivo":"fora_da_lista"`, sem o e-mail.

Se, em vez disso, o próprio Google mostrar "Acesso bloqueado" (`403: access_denied`), a segunda conta
não está em "Usuários de teste" da tela de consentimento: é a recusa do Google, antes da Aurora.
Acrescente a conta lá, se quiser ver a da Aurora.

## 8. Apagar meus dados

1. Entre de novo com a sua conta.
2. Em **Configurações**, clique em **Apagar meus dados da nuvem**, digite `apagar` e clique em
   **Apagar tudo**.
3. O que você deve ver: o aviso "Seus dados foram apagados da nuvem.", e a sessão atual
   continua (a página não volta para "Entrar"). Ainda não há ditado na nuvem (o PC sincroniza na F7);
   aqui o que se prova é o caminho: o epoch novo, os aparelhos e as outras sessões apagados. No log:
   `"rota":"DELETE /api/v1/me/data","status":204`.

## 9. Desligar

Na janela do servidor, **Ctrl+C**. O que você deve ver: `"msg":"desligando"` e `"msg":"desligado"`.

Para desligar também o Postgres de teste (opcional; ele é descartável):

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\postgres-de-teste.ps1 -Parar
```

## Navegadores

O servidor manda os cookies com `Secure` e o prefixo `__Host-`, mesmo em `http://localhost`:

- **Chrome e Edge** tratam `localhost` como contexto seguro e aceitam o cookie `Secure` vindo de
  `http://localhost`. É o caminho deste roteiro.
- **Firefox** recente também trata `localhost` como contexto seguro e deve aceitar. **Não foi
  conferido.**
- **Safari** não aceita cookie `Secure` em `http`: o login volta e a sessão não fica. Use o Chrome.
- Em produção (F11), a nuvem é `https`, e a questão some.

## O login de teste (não faz parte deste roteiro)

Este roteiro usa o Google de verdade, e o `go run` acima compila **sem** o login de teste. Se um dia
você usar o binário com a build tag `logindeteste` (entrar sem o Google): ele só sobe com
`PUBLIC_BASE_URL` em `localhost` e `HTTP_ADDR` no loopback, e só atende pedidos da própria máquina.
**Não entre nele com o seu e-mail do Google** no mesmo banco: o primeiro login de teste fixa o e-mail
num `sub` de teste, e daí o login Google com esse e-mail passa a ser recusado (`sub_diferente`), até
recriar o banco. Use outro e-mail da lista, ou outro banco.

## Se algo der errado

| O que aparece | O que é |
|---|---|
| `host inválido` | a página foi aberta em `127.0.0.1`; use `localhost` |
| no Google, `redirect_uri_mismatch` | o client não tem `http://localhost:8080/auth/google/callback` |
| "Não deu para entrar" (502) e `"motivo":"google_falhou"` no log | a troca do código falhou: o client secret do JSON não é o do client, ou o Google recusou o PKCE |
| "Não deu para entrar" (400) e `"motivo":"state_invalido"` | o login demorou mais de 5 minutos, ou o cookie `__Host-pre` não voltou (outro navegador, ou o Safari) |
| `serve: ...: o banco tem N migrations pendentes` | rode o `migrate` (seção 1) |
| `serve: PAINEL_DIR: ...` | falta o build do app, ou a variável aponta para outra pasta: compile o painel no modo nuvem (seção 3) |
| "Tentativas demais" | mais de uns 5 logins por minuto do mesmo PC (o limite é de 10 passos por minuto, e cada login gasta dois: o início e a volta do Google); espere um minuto |

Quando terminar, conte o que viu, principalmente: se o Google aceitou o PKCE (o passo 5 deu certo), se
o aviso de "app não verificado" apareceu, e se a segunda conta caiu na página da Aurora ou na do
Google.
