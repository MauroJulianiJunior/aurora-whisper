# Security and Privacy — Aurora Whisper

## Dados
| Dado | Classificação | Persistência | Retenção |
|---|---|---|---|
| Texto | Confidencial | SQLite; no Postgres **só depois que quem usa conecta a nuvem**, legível no servidor na v1 (ADR-019). O texto do ditado do celular nasce na nuvem, depois do aceite dela (ADR-021) | 6m, pela data do ditado; o backup da nuvem o guarda até a cópia girar |
| Áudio (do PC) | Sensível efêmero | RAM | tentativa. Nunca sai do PC |
| Contexto UIA | Sensível efêmero | RAM | resolução |
| Dictionary/corrections | Confidencial | SQLite/Postgres; **hoje o `dicionario.json` local, em texto, na pasta de dados** (ADR-017; ADR-018: `%LOCALAPPDATA%\AuroraWhisper`, ou ao lado do executável no modo portátil), onde o painel grava o que aprende | até exclusão |
| Email/google_sub/nome (`display_name`, o `name` do Google; o `avatar_url` existe na tabela e nunca é preenchido) | Pessoal | Postgres | vida da conta: o "apagar meus dados" **não** apaga o cadastro (dossiê de LGPD, `docs/lgpd-dossie.md`, decisão 2) |
| Device token | Secreto | Credential Manager (alvo `AuroraWhisper/nuvem` no Agent instalado e `AuroraWhisper/nuvem-portatil` no `bin\`, ADR-019, F6)/hash (HMAC com `DEVICE_TOKEN_PEPPER`) | até revoke, ou 180 dias sem uso (F6) |
| Sessão web da nuvem (ADR-019) | Secreto | cookie `__Host-sessao` (`HttpOnly`); no Postgres, só o HMAC com `SESSION_SECRET` | 30 dias sem uso (sugestão), "Sair" ou encerrada pela lista de sessões |
| Aprendido sincronizado (ADR-019): par curto, termo, restrição, nome do domínio | Confidencial | `dicionario.json` em cada PC; **F8:** uma cópia no espelho do SQLite (`historico.db`: o par da última sincronização, a projeção que o arquivo tinha, o par que chegou e não vale ali e, com a etiqueta da D4, a troca deste PC que perdeu na nuvem); Postgres com a nuvem conectada (só a troca do Aprender, só depois do clique em "Enviar o histórico"; antes dele, só o desfazer, sem texto, de um par que a nuvem já tem) | no arquivo, até desfazer; no espelho: a linha que vale no arquivo guarda a projeção (o que o arquivo já tem); o texto da linha desfeita sai quando a nuvem confirma o desfazer, ou quando some a etiqueta (30 dias, ou a pessoa aprende de novo), e a troca perdida sai com a etiqueta; o par da nuvem que não vale ali guarda o texto enquanto a conta é a mesma; o esquecer apaga o texto do que veio da conta esquecida e não está no arquivo, e o "desfeito aqui" que ele guarda fica só com o id (em todos, fica o id); na nuvem, o par ativo até desfazer, e a linha desfeita guarda o texto até a F9 decidir |
| Provider keys | Secreto | server-side | operação |
| Chave de API do provedor de IA de quem usa | Secreto | Gerenciador de Credenciais do Windows (Agent) | até quem usa apagar |
| Texto ditado enviado ao provedor de IA escolhido (só no Polish, com a chave de quem usa) | Confidencial; sai da máquina | nenhuma no Agent; o provedor pode reter | até 30 dias no provedor (ADR-016) |
| Voz do ditado no celular (ADR-021) | Sensível efêmero; **sai do aparelho** | só em memória: no app (uma variável, até o envio dar certo ou a pessoa descartar) e na API (uma variável do handler, durante o pedido). Nunca em disco, swap, log, erro ou resposta | descartada ao fim. No provedor escolhido, o que a documentação dele diz (ver "Ditar no celular") |
| Chave de API da pessoa para ditar no celular (ADR-021) | Secreto | Postgres, **cifrada** (AES-256-GCM, subchave por HKDF); a chave de cifra é um arquivo do servidor (`AI_KEYS_SECRET_FILE`), fora do banco e do backup | até a pessoa trocar, remover, desligar o ditado ou apagar os dados. A cópia antiga, cifrada, fica nos backups (cerca de 30 dias) e no banco anterior da restauração (7 dias) |
| Aceite e preferências do ditado no celular (ADR-021) | Pessoal | Postgres (`dictation_consents` e `dictation_settings`); nunca descem aos PCs | a vida da conta; o "Apagar meus dados" os apaga |
| Termos do aprendido enviados como dica (ADR-021) | Confidencial; sai da máquina | nenhuma (a dica é montada a cada ditado); só com o interruptor ligado | a chamada ao provedor; no provedor, como a voz |
| Desfecho pago e não gravado (ADR-021) | Confidencial | memória da API, por `(user_id, id)`: o texto que seria gravado, o modo, o provedor e o modelo | 10 min, o reinício ou a mudança do epoch da conta; nunca em log |

## Threat model
| ID | Ameaça | Mitigação |
|---|---|---|
| THR-001 | abuso local API | token + loopback + Origin/Host; o token chega ao painel só por bilhete de uso único (ver "Local API") |
| THR-002 | DNS rebinding | loopback + Host allowlist |
| THR-003 | device token theft | Credential Manager + revoke |
| THR-004 | IDOR | user da sessão |
| THR-005 | CSRF | SameSite + CSRF. **ADR-019:** `http.CrossOriginProtection` (só `Sec-Fetch-Site` `same-origin` ou `none`) em todo pedido que não é `GET`/`HEAD`, porque o `SameSite=Lax` não protege de um serviço "same-site" no mesmo domínio; nenhum `GET` muda estado, fora o callback do Google (protegido pelo `state` de uso único e pelo cookie `__Host-pre`); nenhum CORS |
| THR-006 | provider secret no client | gateway. Exceção (ADR-016): a chave de quem usa (BYOK) fica no Agent, no Gerenciador de Credenciais; o risco dela é o THR-016. **ADR-021:** a chave que a pessoa cola no app do celular não fica no cliente: vai ao servidor, que a guarda cifrada e só devolve o fim dela; o risco é o THR-027 |
| THR-007 | áudio/contexto em logs | policy + tests. **ADR-021:** vale também para a API e para o ditado do celular: o áudio, a chave da pessoa, o texto ditado, a dica, o `Ditado-Id` e o corpo de erro do provedor nunca vão a log, a mensagem de erro, a resposta nem ao `/tmp`. O log de acesso fica como é (método, padrão da rota, status, ms e bytes), e a linha `ditado:` traz só contagens e códigos (duração, fala, bytes, provedor, modelo, desfecho, desfecho do Polish, termos na dica, ms do provedor). Canário de áudio e de chave cobre log, erro, resposta e `/tmp` (AC-068, TEST-017) |
| THR-008 | prompt injection via contexto | tratar contexto como dado |
| THR-009 | Polish muda sentido, ou obedece à ordem que foi ditada | raw/final + opt-in + o ditado vai ao modelo como dado (JSON) + oito guardas locais que devolvem o texto do Smart: a saída só pode ser a entrada com sinais e com algumas palavras a menos, nunca com palavra trocada ou acrescentada, número ou sigla alterado, nem palavra que virou toda maiúscula (ADR-016) |
| THR-010 | malicious update | TLS + hash/signature |
| THR-011 | keylogging acidental | processar só tecla necessária |
| THR-012 | password field | block default |
| THR-013 | VPS lateral exposure | networks/ports isolados |
| THR-014 | SQLi | queries parametrizadas |
| THR-015 | XSS | React escaping + CSP |
| THR-016 | vazamento da chave de API de quem usa | Gerenciador de Credenciais; nunca em log/arquivo/banco/resposta/erro; redirecionamento do provedor não seguido (ver "Chave de API do provedor de IA") |
| THR-017 | texto ditado sai da máquina para o provedor de IA (Polish): retenção pelo terceiro, envio de texto de campo protegido por cache de foco defasado, texto no log | opt-in próprio; só o texto final do Smart com 4 palavras ou mais; nunca áudio, contexto, dicionário ou IDs; `store:false`; sem log de texto, de `warnings` nem de corpo de erro; prazo de 3,5 s (até 8 s no ditado longo); aviso na tela; defasagem do cache de foco aceita e documentada (ver "Polish: o que sai da máquina") |
| THR-018 | alias aprendido errado: uma variante de palavra comum passa a trocar texto certo em todo ditado futuro, em qualquer aplicativo (o Smart aplica alias sem olhar contexto) | só com o clique de quem usa (AC-022): nunca sozinho no PC onde a pessoa aprende; desde a F8 (D2), os outros PCs aplicam o par sem clique, pelo sync, com as mesmas checagens (abaixo); o aviso diz o efeito e quantos ditados da própria pessoa já têm a palavra; só troca curta de termo (até 4 palavras e 30 letras), nunca acento ou maiúscula sozinhos, número ou palavra de ligação; o Agent refaz as checagens contra o bruto do ditado; Desfazer a um clique. Risco residual aceito: o dano existe até o Desfazer (ADR-017). **Com o sync (ADR-019):** a restrição e o domínio viajam com o par, e o PC que recebe refaz as checagens; o que não vale ali fica "não aplicado neste PC". **F8 (07-out-2026; ADR-019, "Decisões da F8"):** o clique é no PC de origem (D2): o par chega sozinho aos outros PCs, sem clique lá, e um par ruim se espalha. O PC que recebe se defende: as integridades do Aprender; o "agiria" em cada domínio onde o par vai agir, com a restrição dele; a palavra comum (`learning.GrafiaArriscada`); nunca alargar (a troca nunca vale em mais lugares do que na origem: o par restrito vai para `alias_restrito` ou fica de fora); o domínio tem de existir ali; a palavra só é criada sem domínio, com prioridade 50 e só pela troca; o aplicador nunca sobrescreve uma mudança daqui que não subiu; o que a pessoa pôs à mão nunca sai por um desfazer remoto. O Desfazer, num PC ou no celular, vale em todos (D4: o desfazer vence). Risco residual: o dano existe em todos os PCs até o Desfazer |
| THR-019 | phishing no pareamento: alguém manda o link de aprovação, e quem está logado aprova um PC alheio | retorno a `127.0.0.1` com PKCE (RFC 8252, §7.3), sem consulta repetida: o código sai para o PC de quem aprovou e só vale com o verificador do Agent que começou; aprovar é `POST`; a página mostra o nome do PC e o código de conferência e avisa para só aprovar se a pessoa clicou agora (ADR-019). **F6:** o desafio fica na busca da troca (um verificador errado não queima o `code`); o `state` errado não consome o pedido do PC, e o certo o consome antes da rede; o PC monta o endereço que abre, e a página da nuvem só navega para `http://127.0.0.1:<porta>/nuvem/retorno` |
| THR-020 | qualquer conta Google entrar na nuvem (OAuth na internet, código aberto) | lista `CONTAS_PERMITIDAS`; `sub` fixado no primeiro login; mesmo e-mail com outro `sub` recusado; conta fora da lista não cria nada; login de teste só por build tag, nunca por configuração (ADR-019) |
| THR-021 | texto ditado (e, com a ADR-021, áudio) guardado no celular, ou casca velha servida pelo service worker | o service worker só guarda a casca (`/assets` e a navegação, rede primeiro), nunca `/api`, `/auth` ou `/device`; cache com versão; `no-store` na API; o painel local nunca registra service worker (ADR-019). **F5:** a casca é gravada só no install, sem `put` depois (nada que chega da rede entra no cache), e sem navigation preload (que pediria duas vezes a volta de uso único do Google); o Agent responde `404` em `/sw.js`. **ADR-021:** o áudio do ditado também não é guardado: nada em `IndexedDB`, Cache ou `localStorage` (o `bundle.test` recusa), e o gravador só existe no build da nuvem. Não verificado: o `Blob` do `MediaRecorder` pode ser paginado em disco pelo Chrome (ver "Riscos aceitos", abaixo) |
| THR-022 | texto ditado no log de acesso de um proxy | a busca vai no corpo de um `POST`, nunca na URL; o log do servidor tem só o padrão da rota (ADR-019). Residual: o `code` e o `state` do callback do Google passam na URL (uso único, minutos) |
| THR-023 | texto legível no servidor e no backup | Postgres sem porta publicada; HTTPS terminando no servidor, sem CDN de terceiro; backup cifrado com chave só do dono; retenção também no backup (o giro). Cifra ponta a ponta: depois (ADR-019, resposta 4) |
| THR-024 | dado apagado ressuscitar a partir de um PC desligado, de um backup restaurado ou depois do "apagar meus dados" | lápides (que vivem a janela mais 24 h), `gone`, `sync_epoch` e o recomeço do epoch novo; retenção pela data do ditado, decidida linha a linha; o "apagar meus dados" revoga os aparelhos (ADR-019). Sem `410` por marca d'água (F9, D2): o `gone` impede o ressuscitar, e o PC parado só perde a lápide de um ditado com mais de 6 meses, que ele apaga pela retenção dele (F10, com o OK de quem usa) ou guarda (sem o OK) |
| THR-025 | token do PC roubado | só serve ao sync, ao `/me` e ao próprio aparelho, nunca ao CRUD, às sessões ou ao "apagar meus dados"; HMAC no servidor; revogar pela lista de aparelhos (ADR-019). **F6:** vence com 180 dias sem uso; o token não aprova pedido, não lista nem revoga outro aparelho (`403`); um alvo no Gerenciador por modo, para o `bin\` (conta de teste) e o instalado nunca dividirem a credencial |
| THR-026 | a voz do ditado do celular (ADR-021) sai do aparelho: passa pela memória da API, vai a um provedor escolhido pela pessoa, que não diz processar no Brasil, e pode ser retida lá; quem toma a VPS a ouve em trânsito | o aceite específico antes do microfone (THR-030); só o provedor que a pessoa escolheu, sem troca automática; a voz só em memória, descartada ao fim, nunca em disco, swap, log ou resposta (`read_only`, `memswap_limit` igual ao `mem_limit`, `ulimits: core: 0`, canário); HTTP/1.1, sem seguir redirecionamento, sem repassar o corpo de erro. Residual aceito: a retenção no provedor (o que a documentação dele diz; o servidor não confere se a retenção zero está ligada), quem tem a raiz da VPS, o proxy da entrada (que não é nosso e segura pedaços do corpo na memória dele) e o `Blob` do `MediaRecorder` no Chrome |
| THR-027 | vazamento da chave de API que a pessoa guarda na nuvem (ADR-021) | AES-256-GCM com subchave por HKDF e AAD preso ao usuário, ao provedor e ao id da chave de cifra; a chave de cifra num arquivo `*_FILE`, nunca no banco nem no backup; o app só vê `configurada` e o `final`; nunca em resposta, log, erro, URL ou JSON malformado (frase fixa no erro de decodificação); decifrada só durante a chamada. **Não protege** de quem tem a raiz da VPS (lê o arquivo e o banco). As cópias antigas, cifradas, ficam nos backups e no banco da restauração; a tela manda **revogar a chave antiga no provedor** |
| THR-028 | a dica leva ao provedor os termos do aprendido, que podem ser nomes de clientes ou de terceiros, e pode induzir um termo que ninguém disse (ADR-021) | desligada por padrão até a C8 medir; o interruptor é da pessoa; só os termos distintos, sem pares presos a um domínio, até 140 runas; a folha do aceite diz que os termos podem levar nomes de terceiros; a OpenAI avisa que as palavras-chave são só dicas |
| THR-029 | sessão roubada, ou um defeito do app, gasta o dinheiro da pessoa no provedor (ADR-021) | limites por conta, em memória: 1 de cada vez, 10 por minuto, 200 por dia, 3 ao mesmo tempo no total; 10 por minuto nas rotas da chave; sem fala não se envia (o app mede a fala); o teto de gasto é da pessoa, com uma chave só para o celular e limite de gasto no provedor. Um ditado de 120 s custa centavos (ADR-021, "Riscos") |
| THR-030 | voz, dica ou texto saírem sem o aceite de quem dita, ou depois de revogado (ADR-021) | sem aceite vigente que inclua o provedor: o microfone não abre, a chave não é pedida e o servidor recusa (`403`, `SEM_ACEITE`); o aceite é gravado no servidor, por conta, com a data, a versão do aviso e os provedores; a rota confere de novo antes do Polish e antes de gravar; "Apagar meus dados" é conferido dentro do `Store.Create` (epoch), sem janela. Residual: uma janela de milissegundos entre a conferência do aceite e a gravação |
| THR-031 | o microfone fica mudo (tela apagada, ligação, app fora da frente) e o provedor inventa frases num fim mudo (ADR-021) | Wake Lock; o app para quando a página sai da frente ou a faixa do microfone fica muda ou acaba (`mute` e `ended`), envia o que gravou se tiver fala e avisa; sem fala bastante nada é enviado; o filtro de `no_speech_prob` do servidor, quando o modelo devolve o campo. No passo 1 não existe ditado com a tela apagada |

## Hook policy
- nunca registrar sequências de teclas;
- somente tecla configurada/modifiers necessários;
- não transmitir keyboard events.

## Local API
- bind 127.0.0.1;
- Host allowlist;
- token aleatório em memória;
- React mantém token em memória;
- Origin validation;
- sem rotas de comando arbitrário. A rota que grava no dicionário (`/api/aprendizado`, ADR-017) só
  escreve no `dicionario.json` (na pasta de dados, ADR-018), e só depois de refazer as checagens contra o
  `raw_text` guardado daquele ditado: não grava texto que o cliente inventou.

### Painel local: como o token chega à aba
Acrescentado em 29-set-2026 (revisão adversarial de `6f546e0`). O contrato das rotas está em
`06-api-contract.md`, seção "API local (Windows Agent)"; a fonte da verdade é
`internal/agent/painel/bilhete.go` e `servidor.go`.

- O token nunca é servido por rota aberta: uma rota que o devolvesse a quem pedisse o tornaria
  inútil, porque qualquer programa da máquina alcança `127.0.0.1`.
- O item "Abrir Aurora Whisper" da bandeja emite um **bilhete** aleatório de 256 bits, de uso
  único, válido por 1 minuto, com no máximo 8 vivos. O navegador abre com ele na URL (`?t=`).
- A página apaga o bilhete da URL antes de qualquer requisição e o troca pelo token em
  `POST /api/sessao`, atrás de Host + Origin. Bilhete inexistente, vencido ou já usado recebe a
  mesma resposta (`401`), e a comparação é em tempo constante.
- Os arquivos estáticos do painel exigem só Host: o navegador os carrega antes de ter o token.
  O Host continua barrando DNS rebinding (THR-002).
- `Referrer-Policy: no-referrer`, porque o bilhete viaja na URL; CSP `default-src 'self'`, sem
  script embutido nem recurso de fora (THR-015).
- Nem o bilhete, nem a URL, nem o token vão para log.
- Consequência assumida da regra "React mantém token em memória": recarregar a página perde a
  sessão; abrir de novo pela bandeja emite outro bilhete.

Risco residual: o bilhete pode ficar no histórico do navegador e, por instantes, na linha de
comando dele. Ele morre na primeira troca e em um minuto de qualquer forma. `Origin` ausente é
aceito (clientes que não são navegador não o mandam; a defesa contra rebinding é o Host), então
um processo local que lesse o bilhete a tempo conseguiria trocá-lo — mas esse processo roda
como o mesmo usuário e já lê o `historico.db` direto, então o bilhete não lhe dá nada novo.

## Chave de API do provedor de IA
Acrescentado em 29-set-2026 (tela de Configurações). O contrato está em `06-api-contract.md`, "Chave
de API do provedor de IA"; a fonte da verdade é `internal/agent/chaves`, `internal/ia` e
`internal/agent/localapi/ia.go`.

É a chave que **quem usa** cola no painel (OpenAI ou Anthropic), para o modo Polish (etapa 2;
ADR-016). Não é
chave do projeto: a de "Provider keys" acima é do operador da cloud e fica no servidor (THR-006). O
projeto vai ser aberto, e cada instalação usa a própria chave. **Esta seção é da chave do PC, que nunca
sai do PC** (ADR-021). A chave que a pessoa cola no app do celular para ditar é outra: fica cifrada no
servidor, e está em "Ditar no celular: o que sai do aparelho", mais abaixo.

- **Onde mora:** no Gerenciador de Credenciais do Windows, credencial genérica
  (`CRED_TYPE_GENERIC`), alvo `AuroraWhisper/<provedor>`. O Windows a cifra com a chave do usuário
  (DPAPI); outro usuário da máquina não a lê.
- **Persistência `CRED_PERSIST_LOCAL_MACHINE`:** apesar do nome, toda credencial do Gerenciador é do
  usuário que a gravou; o `Persist` escolhe quanto ela dura e para onde vai. `SESSION` sumiria no
  logoff; `ENTERPRISE` viajaria com o perfil móvel do domínio para outras máquinas, onde o Aurora
  Whisper nem está. `LOCAL_MACHINE` é o usuário atual, neste computador.
- **Onde nunca aparece:** log, arquivo, banco (`local_settings` só tem a *escolha* de provedor e
  modelo), resposta da API local (o GET mostra só os últimos caracteres: no máximo 4, nunca mais
  que um quarto da chave), mensagem de erro, URL e o bundle do painel.
- **Ao falar com o provedor:** a chave vai só no cabeçalho. Há duas chamadas: `GET /v1/models`
  (o "Testar"), com timeout de 10 s, e, no Polish (ADR-016), um `POST` com o texto ditado —
  OpenAI `POST /v1/responses`, Anthropic `POST /v1/messages` —, com prazo de **3,5 s** por ditado
  (até 8 s no ditado longo) e sem retry. **As mesmas travas valem para as duas:** o corpo de erro do provedor não passa adiante
  (o 401 da OpenAI devolve um pedaço da chave), redirecionamento não é seguido (o `x-api-key`
  sobreviveria a uma troca de domínio) e a chamada é só HTTP/1.1 (o HTTP/2 do `net/http` escreve
  cada cabeçalho e o começo de cada corpo no log padrão, que o Agent manda para o `agent.log`, quando
  a variável `GODEBUG` traz `http2debug`, e guarda o cabeçalho de autenticação na memória da
  conexão). O que o `POST` leva e o que nunca leva está em "Polish: o
  que sai da máquina", abaixo.
- **Validação na entrada:** só ASCII imprimível sem espaço, até 2560 bytes. A chave vira cabeçalho
  HTTP; um caractere de controle ali seria cabeçalho quebrado ou injetado.
- **Portão:** gravar, ler e apagar no Gerenciador passam por `internal/agent/sysinput`, que recusa
  sob `go test` — ler também, para que um teste nunca traga a chave real de quem usa para a saída
  dele. Os testes usam o dublê em memória.
- **Travas:** `localapi.TestChave_NuncaSaiDoCofre` passa a chave por todas as rotas, contra SQLite de
  verdade e um provedor falso que a ecoa no corpo e num cabeçalho, e procura qualquer pedaço de 8
  caracteres dela em cada resposta, cabeçalho, no log da API, no log padrão do processo e em cada
  coluna de cada tabela; `storage.TestSchema_NenhumaColunaDeSegredo` falha se o schema migrado tiver
  coluna com nome de segredo; `ia.TestListarModelos_NaoSegueRedirecionamento` e
  `ia.TestListarModelos_ErrosTraduzidosSemAChave`; `chaves.TestGerenciador_RecusaSobGoTest`.

Risco residual: um processo rodando como o mesmo usuário lê a credencial pelas APIs do Windows —
como lê o `historico.db`. É a fronteira de sempre do Agent: o usuário da máquina.

## Polish: o que sai da máquina
Acrescentado em 29-set-2026 (ADR-016). O Polish é a **primeira vez que texto ditado sai da
máquina**: o ASR é local. Só acontece com o opt-in do Polish nas Configurações, para o provedor
que quem usa escolheu, com a chave dele. O contrato da chamada está em `06-api-contract.md`,
"Chamada de Polish ao provedor". **Com a nuvem conectada (ADR-019)**, o histórico também sai, para a
nuvem de quem usa: ver "Nuvem: o que sai da máquina", abaixo. Nada disto muda o que o Polish manda
ao provedor.

**ADR-021 (10-out-2026): o Polish também roda no celular, e lá quem chama o provedor é o servidor.** O
que vai ao provedor é o mesmo (o texto do Smart, com 4 palavras ou mais; o prompt fixo; o nome do
modelo; a chave, no cabeçalho), com as mesmas travas e o mesmo prazo (`prazoPolish`, de 3,5 s a 8 s).
Muda o resto: a chave vem do cofre cifrado do servidor (e não do Gerenciador de Credenciais), o texto
que vai é o do **Smart da conta** (sem o `dicionario.json` do PC, nem os pares presos a um domínio), o
Polish do celular **começa desligado** e só liga com o aceite do provedor do Polish (`SEM_ACEITE`
antes de chamar). O campo protegido não se aplica: o celular não escreve em outro aplicativo (a
escrita em outro app e o campo de senha são do passo 2, e são parada própria). O que o provedor do
Polish diz sobre retenção abaixo vale para os dois. Ver "Ditar no celular: o que sai do aparelho".

- **O que sai:** o texto final do Smart (não o bruto do ASR), só com 4 palavras ou mais; o prompt
  fixo (`polish-ptbr@1.0.2`); o nome do modelo; e a chave, no cabeçalho.
- **O que nunca sai:** áudio; contexto de janela (nome do app, domínio, título, texto de perto);
  dicionário; histórico; IDs. O dicionário é consultado **localmente**, pelas guardas.
- **Quando não sai nada:** Literal e Smart; Polish desligado; sem chave; ditado com menos de 4
  palavras; campo protegido, porque o ditado é bloqueado **antes do áudio** (ADR-007, ADR-011) e o
  texto nem chega a existir.
- **`asr_policy`:** `local_only` vale para o áudio e para o contexto (AC-031). O Polish tem opt-in
  próprio, e a tela diz isso.
- **Risco que sobra no campo protegido:** a classificação do campo pode estar desatualizada por até
  um orçamento de 500 ms, e a ADR-011 (nota de 29-set-2026) admite até dois desses intervalos numa
  mesma troca de foco (deduzido do mecanismo, não medido). O risco já existia — o texto seria
  digitado no campo —; o agravante é que agora o texto também sairia da máquina. **Aceito e
  documentado** (proposto no plano da etapa 2, aprovado pelo Mauro em 29-set-2026). Checar de
  novo no fim do ditado custaria uma nova inspeção de UIA, e foi descartado.
- **Log:** uma linha por ditado, com tentativa, `prompt@versão` e os 8 primeiros caracteres do hash
  do prompt, provedor, modelo, desfecho (aceito ou motivo), latência e tamanho em caracteres e em
  tokens. **Nunca vai para o log:** o texto (enviado ou devolvido), os `warnings` do modelo
  (descartados), o corpo de erro do provedor e a chave. Literal e Smart não deixam nenhuma linha de
  Polish.
- **Retenção no provedor** (fontes oficiais, pesquisa de 29-set-2026; nenhuma chamada feita):
  - OpenAI: não treina com dados da API sem opt-in; os logs de abuso ficam até 30 dias e podem
    conter prompts e respostas; mandamos `store: false`. (A página de dados da API foi relida em
    10-out-2026 para o ditado do celular e diz o mesmo: `docs/lgpd-dossie.md`, Anexo B. A da
    Anthropic não foi relida nessa data.)
  - Anthropic: não treina sem permissão expressa. As duas fontes oficiais divergem sobre a
    retenção; **assume-se até 30 dias**.
  - Zero Data Retention é por organização e não vale para uma chave pessoal.
- **Texto da tela do PC** (`08-ui-ux-spec.md`, Configurações, Privacidade; na tela desde a S5, com um
  teste que confere a frase palavra por palavra; o texto do app da nuvem é outro, abaixo): "No modo Polish, o texto
  ditado (nunca o áudio nem o que está nas outras janelas) vai ao provedor de IA escolhido, com a
  sua chave; OpenAI e Anthropic podem guardá-lo por até 30 dias e dizem não usá-lo para treinar
  modelos."
- **O motivo na tela é só o enum.** `polish_ultimo_motivo` (`06`) não carrega texto ditado, texto do
  modelo, corpo de erro do provedor nem a chave: o Agent entrega ao `localapi` o motivo, o provedor
  e a hora, e a frase sai de uma tabela fixa. Um motivo que a tabela não conhece vira uma frase
  genérica, sem o valor. Fica em memória e some ao reiniciar.
- **O `PolishRequest` não sabe se imprimir com o ditado** (fatia S1, já implementado): nenhum
  campo tem tag JSON, e `String`, `GoString`, `Format` e `LogValue` mostram só a versão do prompt
  e o tamanho do texto. Um teste com canário confere `json`, `fmt` (7 verbos) e `slog`. Quem loga a
  chamada escolhe os campos um a um, nunca a struct inteira. Os `warnings` que o Polish devolve ao
  resultado são um aviso fixo com o motivo (`polish: mantido resultado smart (motivo=X)`): o erro
  do cliente e os `warnings` do modelo são descartados.
- **Travas** (fatias S2 e S4): o dublê de provedor que confere o corpo enviado (só o texto em JSON
  na mensagem `user`, `store: false`, nada de contexto) e `TestChave_NuncaSaiDoPolish`, em
  `internal/ia`; o `TestChave_NuncaSaiDoCofre` estendido ao `POST` do Agent
  (`TestChave_NuncaSaiDoPolishDoAgent`, em `cmd/agent`: a chave sai do cofre, volta ecoada pelo
  provedor, e não aparece no resultado, no log do adaptador nem no log padrão);
  `TestPolish_CanarioDePrivacidadeNoLog`, em `internal/agent/winruntime`: o ditado, os `warnings`,
  o corpo de erro e a chave passam por quatro desfechos e nenhum chega ao log; e
  `TestPolish_LiteralESmartNaoChamamNinguem`: Literal e Smart não abrem conexão nem deixam linha de
  Polish.

## Cloud auth
- Google OAuth;
- secure web cookie;
- device challenge;
- revogação;
- provider secrets server-side.

Como fica na etapa 5 (ADR-019; contrato em `06`, fluxos em `07`):
- **Login Google** só com `openid email profile`, `state`, `nonce` e PKCE; o `state` fica no
  servidor; cookie `__Host-pre` de 5 min entre o início e o callback; `email_verified` exigido.
- **Só as contas de `CONTAS_PERMITIDAS`** (THR-020).
- **Sessão** em cookie `__Host-sessao` (`HttpOnly`, `Secure`, `SameSite=Lax`), trocada a cada login,
  HMAC no banco, revogável pela lista de sessões; "Sair" apaga no servidor.
- **Pareamento** por retorno a `127.0.0.1` com PKCE (THR-019); o Agent abre o navegador atrás do
  portão `sysinput`, que recusa sob `go test`.
- **Token do PC** com escopo mínimo (THR-025), no Gerenciador de Credenciais pelo mesmo portão.
- **CSRF** pelo `http.CrossOriginProtection` (THR-005); limite de tentativas; `X-Forwarded-For` só
  vale vindo do proxy configurado.

**F6 (05-out-2026): o pareamento, implementado** (`internal/server/pareamento.go`,
`internal/agent/nuvem`; ADR-019, "Decisões da F6"). O que vale, e onde é provado:
- **No servidor:** o start e a troca são públicos, com 10 tentativas por minuto por IP (os dois
  juntos) e 100 inícios por minuto de todos; o corpo é estrito (até 1 KiB, sem campo desconhecido) e
  nada é gravado com um campo fora. Ler e aprovar o pedido, só com a sessão (e o CSRF). A troca acha o
  pedido pelo SHA-256 do `code` **e** pelo desafio, aprovado, sem uso e sem vencer, e o consome no
  mesmo `UPDATE`: verificador errado não queima o `code`, e oito trocas simultâneas do mesmo `code`
  dão um token só (`pgstore/pareamento_pg_test.go`, que pede o Postgres de teste: ele estava desligado
  quando a F6 fechou, e esse teste não rodou; ver a ADR). O token: 256
  bits, só o HMAC com `DEVICE_TOKEN_PEPPER` no banco (o teste confere que nem o valor, nem os bytes,
  nem o SHA-256 puro estão lá), um por aparelho; 180 dias sem uso o vencem. Revogar grava `revoked_at`
  e apaga o token. O log: só o padrão da rota.
- **No PC:** a URL da nuvem é conferida antes de qualquer rede (o release só `https`; o build de
  desenvolvimento só no modo portátil, com `https` ou `http://localhost`). O pedido em curso (o id, o
  verificador e o prazo) fica só na memória. O Agent abre o endereço que **ele** monta com a URL
  digitada (o `auth_url` da nuvem é ignorado), pelo `painel.AbrirNoNavegador` (portão). O retorno
  (`/nuvem/retorno`, só com o `Host`) confere o `state` em tempo constante; o errado não consome nada;
  o certo consome antes da rede; o prazo é o da nuvem, nunca mais de 10 minutos. O cliente HTTP da
  nuvem não segue redirecionamento (o token nunca vai a outro endereço), tem prazo de 10 s e lê até
  64 KiB. O token vai para o Gerenciador pelo `chaves.NovoGerenciador` (portão), no alvo do modo:
  `AuroraWhisper/nuvem` no instalado, `AuroraWhisper/nuvem-portatil` no `bin\` (D7: sem isso, conectar
  o `bin\` com a conta de teste trocaria o token do PC real), com o comentário "Aurora Whisper: token
  deste PC na nuvem" (o das chaves de API diz "chave de API"). A conta, o e-mail, a URL e o aparelho
  vão para a tabela `instalacao`; o token, nunca. A D3: outra conta revoga o aparelho recém-criado e
  não guarda o token; outra nuvem é recusada antes da rede, e de novo na troca (outra URL gravada
  durante o pedido: revoga e não guarda, com teste). A página de aprovação mostra a conta da sessão
  antes do Aprovar ("Este PC vai ficar ligado à conta …"): quem está na conta errada vê antes de
  prender o PC nela.
- **O que não sai:** o token não volta em resposta nenhuma da Local API (o `/api/ia/chave` não o
  alcança: `nuvem` não é provedor) e não vai a log; o log do Agent não tem o `code`, o `state`, o
  pedido, o verificador, a conferência, a URL nem o e-mail (`vazamento`, no teste do pacote e da API
  local).
- **Os portões, provados sob `go test`:** a montagem de verdade (`cmd/agent`, `montarNuvem`) faz o
  Conectar contra uma nuvem de teste e para em `sysinput.ErrDisabledInTest` no navegador; o retorno
  troca o `code` e para no Gerenciador, e o aparelho recém-criado é revogado; o `Gravar`, o `Ler` e o
  `Apagar` dos dois alvos dão `ErrDisabledInTest`.
- **Riscos que ficam:** a Local Network Access e o HTTPS-Upgrades do Chrome numa nuvem pública (o
  retorno foi conferido só de `localhost` para `127.0.0.1`; F11); quem tiver o painel do PC aberto
  (o token da Local API) pode conectar o PC a uma nuvem que controla, como já pode ler o histórico;
  e a **porta do retorno é escolhida por quem inicia o pedido**: no phishing, o atacante inicia (e
  tem o verificador), e, se a pessoa aprovar apesar do aviso, o navegador dela leva o `code` a
  `http://127.0.0.1:<porta do atacante>/nuvem/retorno`. Ele só vaza se um serviço local nessa porta
  repassar a URL para fora (um proxy, um servidor de desenvolvimento que redirecione preservando a
  busca). É da RFC 8252 (7.3: qualquer porta no loopback); o aviso, o código de conferência, a conta
  mostrada e o `Referrer-Policy: no-referrer` mitigam, e o servidor aceita só a faixa dinâmica do
  Windows (49152 a 65535), onde o painel do PC tira a porta, o que afasta as portas de serviços
  conhecidos (3000, 5173, 8080, 8888; decisão do Mauro, 06-out-2026; ADR-019, "Decisões da F6").

**F3 (05-out-2026): implementado no servidor** (`internal/server`, `internal/server/cloudauth`;
ADR-019, "Decisões da F3"). O que vale, e onde cada coisa é provada (a suíte
`internal/server/authtest`, contra a memória e contra o Postgres):
- **O ID token sem JWKS.** Ele chega direto do endpoint de token do Google (fixo no código, nunca do
  ambiente nem do JSON do client), por TLS, numa chamada autenticada com o client secret: é o caso em
  que o OpenID Connect Core (3.1.3.7, item 6) dispensa a assinatura. O servidor confere `iss`, `aud`,
  `azp`, `exp` e `iat` (5 min de folga), o `nonce`, o `sub` e `email_verified` igual a `true`. O
  client HTTP não segue redirecionamento (o client secret vai no corpo).
- **A lista vem antes de qualquer escrita** (THR-020): conta fora dela, o mesmo e-mail com outro `sub`
  (D7), e-mail não verificado ou conta desativada não criam usuário, contador nem sessão. E a lista vale
  **em toda checagem de credencial**: tirar um e-mail e reiniciar corta as sessões e os PCs dele.
- **Segredos só como hash ou HMAC.** O id da sessão e o token do aparelho: HMAC-SHA256 com
  `SESSION_SECRET` e `DEVICE_TOKEN_PEPPER` (pelo menos 32 bytes, diferentes). O `state` e o
  `__Host-pre`: SHA-256 (vivem 5 minutos). O verificador do PKCE e o `nonce` ficam em claro por 5
  minutos, porque o servidor precisa deles inteiros. O teste confere que o valor do cookie, os bytes
  dele e o SHA-256 puro dele não estão em lugar nenhum do banco.
- **Sessão:** 256 bits, trocada a cada login (a que o navegador trouxer é apagada), 30 dias sem uso e
  teto de 90 dias desde o login; sair e derrubar apagam a linha no servidor.
- **O token do PC** (THR-025) só alcança `/sync/*` e `GET /me`; no CRUD, nas sessões e no "apagar meus
  dados", `403`. Bearer inválido é `401`, sem cair no cookie.
- **Cabeçalhos:** CSP `default-src 'self'` com `base-uri 'none'`, `object-src 'none'`,
  `frame-ancestors 'none'` e `form-action 'self'`; `nosniff`; `Referrer-Policy: no-referrer`; COOP
  `same-origin`; HSTS (1 ano, sem `includeSubDomains`) só em `https`; `no-store` em `/api` e `/auth`.
  Nenhum CORS.
- **Host:** só o de `PUBLIC_BASE_URL` (menos o health): barra o DNS rebinding.
- **Limites** (em memória; reiniciar zera): login 10/min por IP (o IPv6 por `/64`, para quem gira
  endereços no próprio bloco não escapar) e 100/min no total do início; push 60 pedidos e 5.000
  mutações/min por aparelho. O teto total troca o abuso do banco por um DoS do login (quem esgota os
  100/min trava o login de todos enquanto durar): a F11 o reavalia atrás do proxy. O pull, o CRUD, o
  `/me` e as sessões não têm limite de taxa, e a cota de escrita por usuário é da F11 (era da F9 até
  a D3 da F9, de 08-out-2026: não há número nem dado real antes do deploy).
- **O `state` e o `__Host-pre` juntos:** o hash do `pre` está na busca do login pendente, então quem
  tem só o `state` de outra pessoa (a URL de retorno vazada) não consome nem queima o login dela.
- **Login de teste só por build tag** (`logindeteste`); sem a tag, a rota não existe (`404`), e
  nenhuma variável a liga. Com a tag, três guardas, porque o Host e o CSRF não barram um cliente que
  não é navegador: `PUBLIC_BASE_URL` em `localhost`; `HTTP_ADDR` no loopback (`127.0.0.0/8`, `::1` ou
  `localhost`; em `0.0.0.0` o serve não sobe, e o `POST /auth/teste` não fica exposto à rede local); e
  a rota responde `404` a um `RemoteAddr` fora do loopback ou a um pedido com `X-Forwarded-For` ou
  `Forwarded`. O primeiro login de teste com um e-mail fixa o `sub` `teste:<e-mail>` (D7): o login
  Google desse e-mail, no mesmo banco, passa a ser recusado; use outro e-mail da lista, ou outro banco.

## Nuvem: o que sai da máquina (ADR-019)
- **É a primeira conexão permanente do Agent, e só existe depois que quem usa conecta.** Sem
  conectar, o Agent não abre conexão nenhuma com a nuvem (vale a ADR-018: nenhuma conexão sem ação
  de quem usa). O campo do endereço vem vazio; o projeto é aberto, e cada um sobe a própria nuvem.
- **O que sai:** de cada ditado, texto bruto e final, data, status, lixeira, modo, aparelho de origem
  e os metadados do ASR que o histórico mostra; o aprendido (par curto, termo, restrição, nome do
  domínio); nome, plataforma e versão do PC.
- **O que nunca sai:** o áudio **do PC**; contexto de janela (app em foco, título, texto de perto; o
  `target_app` não existe na nuvem); a chave do Polish **do PC**; os logs; o mapa de domínios para
  executáveis; as preferências **do PC**.
- **O que o celular manda é outro caso (ADR-021):** a voz de cada ditado, a chave que a pessoa cola no
  app, o aceite e as preferências do ditado. Isso não sai do PC nem passa por esta lista: está em
  "Ditar no celular: o que sai do aparelho", abaixo, e só existe depois do aceite específico dela.
- **Na primeira conexão:** o Agent mostra quantos ditados vão subir (os dos últimos 6 meses, sem a
  lixeira) e faz uma cópia do banco antes de enviar. **F7c (06-out-2026):** só com o clique em
  **Enviar o histórico** (D1); o que veio de outro PC nunca sobe deste (D2); a cópia é
  `historico.db.antes-da-nuvem-AAAAMMDD-HHMMSS.db`, ao lado do banco (exceção conhecida, abaixo).
- **Esquecer esta nuvem (F7c):** só desconectado. O PC deixa de saber da nuvem e da conta (a URL, a
  conta, o aparelho, o cursor e a fila saem; o token sai do Gerenciador de Credenciais, se ainda
  estiver lá); os ditados ficam, e na nuvem nada muda (o aparelho já foi revogado no Desconectar). O
  que não tinha subido fica neste PC (de volta à mesma conta, a mudança sobe pela reconciliação), e a
  pergunta diz quantas mudanças são. O esquecer anota, só pelo id e sem texto, quais ditados mudaram
  aqui sem a nuvem receber (`sync_mudou_aqui`). Para outra conta, o histórico daqui só sobe com o
  clique, com uma cópia nova.
- **O aprendido (F8, 07-out-2026; ADR-019, "Decisões da F8"):** sobe só a troca do botão Aprender
  (D1): a variante, o termo, a restrição e o nome do domínio. Nunca sobem o que entrou pelo
  "Adicionar termo", à mão ou pelo `dictlearn`, o mapa de domínios, a prioridade, a dica, o idioma nem
  o `usuario` do arquivo. Sobe só depois do clique em "Enviar o histórico", e a pergunta conta as
  trocas (D3), também quando não há ditado a enviar (N = 0): a rodada não grava o aceite sozinha
  enquanto houver troca a subir. Antes do clique, só o desfazer de um par que a nuvem já tem sobe, e
  ele não leva texto. O que vem da nuvem entra no `dicionario.json` deste PC sem clique (D2). Depois
  do esquecer, a conta nova recebe só o que foi aprendido neste PC: o que veio da nuvem esquecida
  fica aqui, anotado só pelo id, e não sobe (a linha que estava sendo aplicada no momento do esquecer
  também, até a reconciliação). **A cópia local:** o espelho no `historico.db` guarda a variante e o
  termo da última sincronização e o par que chegou e não vale aqui. O esquecer zera a versão e a
  situação, e apaga o texto do que veio da conta esquecida e não está no arquivo (fica o id).
- **No servidor:** o texto fica legível para quem administra a VPS (THR-023). A revisão de LGPD é
  pré-condição do deploy (abaixo; o dossiê é `docs/lgpd-dossie.md`).
- **Revogar ou desconectar** não apaga nada no PC: o Agent continua funcionando localmente.

## Ditar no celular: o que sai do aparelho (ADR-021)
Acrescentado em 10-out-2026 (D0 da ADR-021). **É o desenho aprovado, e ainda não está implementado**: o
código vem nas subfatias C1 a C7 da ADR, e cada afirmação abaixo só vale como atendida depois do teste
que a ADR lista (os ACs são o AC-065 a AC-079). **Nenhum áudio real sai do aparelho antes do OK do
Mauro na política e no dossiê reescritos** (`docs/politica-de-privacidade.md`, que é o modelo, e
`docs/lgpd-dossie.md`). O contrato da rota está em `06-api-contract.md`.

É a **primeira vez que a voz sai da máquina de quem dita**. Para o **PC**, a regra "o áudio não sai do
PC" continua verdadeira e é a de sempre. O celular é um caminho à parte, opcional, que só começa depois
do aceite específico da pessoa (decisão H da ADR-021).

**O caminho.** O app grava a voz em webm/opus, na memória, e a envia à rota nova
`POST /api/v1/dictations`, com a sessão do navegador. O servidor confere o aceite, as preferências, a
chave e os limites **antes de ler o corpo**, depois lê a voz para a memória, a manda ao provedor que a
pessoa escolheu (OpenAI ou Groq), com a chave dela, e descarta. O texto passa pelo `Colapsar` e pelo Smart
da conta e, se a pessoa ligou, pelo Polish (OpenAI ou Anthropic, com a chave dela). O ditado é gravado
pelo ponto único de gravação (`source_platform` `web`, `device_id` nulo) e desce aos PCs pelo sync.

**O que sai, e para quem:**

| Para | O quê | Quando |
|---|---|---|
| A nuvem (a VPS) | a voz de cada ditado, **só em memória, durante o pedido, descartada ao fim**; o texto, que vira ditado no histórico como os do PC; a chave da pessoa, cifrada; o aceite e as preferências | a chave, depois do aceite; a voz, a cada ditado |
| O provedor de transcrição (OpenAI ou Groq, escolhido pela pessoa, com a chave dela) | a voz; com a dica ligada, os termos do aprendido (que podem ser nomes de clientes ou de terceiros) | a cada ditado, com o aceite |
| O provedor do Polish (OpenAI ou Anthropic) | o texto do Smart, com 4 palavras ou mais | só com o Polish ligado e aceito |
| Os provedores, ao guardar ou trocar a chave | a própria chave, para o teste (a lista de modelos) | depois do aceite |

**O que nunca vai:** a chave **do PC** (ela continua no PC); o áudio do PC; o contexto de outras janelas
(o app do celular nem o enxerga); o dicionário completo; o mapa de domínios.

**Quem é quem (proposta, como no dossiê).** Quem hospeda a nuvem é o controlador (art. 5º, VI da
Lei 13.709/2018); quem dita é o titular (art. 5º, V), e a voz e os termos podem ser de **outros
titulares** (clientes, pessoas perto do microfone). Os provedores são operadores (art. 5º, VII). Com
BYOK, o contrato com o provedor é o da conta da pessoa. **A voz não é usada para identificar a pessoa**:
nenhum código a compara com uma amostra nem a guarda. Se fosse, ela poderia ser dado biométrico
sensível (art. 5º, II, "dado genético ou biométrico, quando vinculado a uma pessoa natural"), e o
tratamento pediria o art. 11.

### A voz é efêmera, e o que isso promete
- **No app:** os bytes ficam só numa variável, até o envio dar certo ou a pessoa descartar, e as
  referências são soltas no fim. Nada em `IndexedDB`, Cache ou `localStorage`. **O que não se garante:** o
  `MediaRecorder` entrega um `Blob`, e o Chrome pode paginar `Blob`s em disco acima de um limite de
  memória. Isso não foi verificado para o tamanho de um ditado (cerca de 0,5 MB para 120 s).
- **Na API:** não há multipart nem arquivo temporário (o corpo é cru). O corpo é lido com
  `http.MaxBytesReader`, o tipo vem dos primeiros bytes (o cabeçalho EBML do webm), nunca do
  `Content-Type`, e os bytes ficam numa variável do handler que sai de escopo no fim, com sucesso ou erro.
  Eles nunca entram em estrutura de vida longa (cache, pool, erro, pânico).
- **Sem disco, swap nem core dump:** o contêiner da API é `read_only`; ganha `memswap_limit` igual ao
  `mem_limit` e `ulimits: core: 0` (casos no `deploy/compose_test.go`). **O `memswap_limit` só vale se
  o kernel da VPS contar a swap no cgroup**, e a C7 confere isso **antes do primeiro áudio** (o
  `docker info` sem "No swap limit support" e o `memory.swap.max` do contêiner em `0`, lido no host).
  Se não der `0`, para e pergunta.
- **O proxy da entrada não é nosso:** ele pode segurar pedaços do corpo na memória dele, fora do
  `memswap_limit`, e um middleware de buffering levaria ao disco um corpo acima de 1 MiB. O nosso router
  não tem middleware, e o corpo fica perto de 0,5 MB. A C7 lê a configuração real (o `readTimeout`, o
  `writeTimeout` e os middlewares do entrypoint) antes do primeiro áudio; **o proxy nunca é editado**.
- **"Descartado ao fim", e não "apagado da memória":** o Go não zera a memória. A política diz
  "descartado".
- **No log:** ver "Logging proibido", abaixo. Nunca vão ao log o áudio, o texto, a chave, o id nem o
  corpo de erro do provedor (a OpenAI devolve um pedaço da chave no `401`).

### A chave da pessoa na nuvem
Contrato: as rotas das chaves, no `06-api-contract.md`. Desenho: ADR-021, decisão G e "As chaves".

- **A cifra:** AES-256-GCM (biblioteca padrão), com uma subchave derivada do arquivo por HKDF
  (`aurora-whisper/ai-keys/v1`); o arquivo nunca é usado cru. O nonce é aleatório a cada gravação. Cada
  linha guarda um id curto da chave de cifra (8 bytes de um HMAC dela, com outro rótulo), e o AAD prende o
  texto cifrado ao usuário, ao provedor e a esse id. Trocar o arquivo leva toda linha com o id antigo a
  `SEM_CHAVE`: a pessoa cola de novo. Manter duas chaves de cifra fica fora.
- **A chave de cifra** é do servidor: um arquivo `AI_KEYS_SECRET_FILE` (64 caracteres em hexadecimal,
  como os outros segredos), nunca no banco nem no backup. Sem o arquivo, as rotas não são montadas. Um
  arquivo malformado impede a subida, sem mostrar o conteúdo.
- **O que o app recebe:** `configurada` e o `final` (as regras de `chaves.Final`: no máximo 4
  caracteres e nunca mais que um quarto da chave). A chave nunca volta inteira, nem a quem a colou. A
  rota da chave **não** usa a mensagem do `decodeJSON`: qualquer erro de decodificação responde com uma
  frase fixa.
- **A validação** vem **depois do aceite**: as regras de `chaves.Validar` e um teste no provedor (a lista
  de modelos).
- **O que a cifra protege:** um dump ou backup do banco que vaze, uma leitura indevida do banco e o log
  do Postgres. **O que não protege:** quem tem a raiz da VPS lê o arquivo da chave de cifra e o banco e
  decifra tudo. O dossiê (seção 6) diz isso, e a saída num incidente é **revogar a chave no provedor**.
- **O que fica para trás:** ao "Trocar" ou "Remover", a chave antiga, cifrada, fica nos backups (cerca
  de 30 dias) e no banco `aurora_antes_AAAAMMDD` de uma restauração (7 dias). Se a chave de cifra vazar
  depois, essas cópias se decifram. Por isso a tela manda **revogar a chave antiga no provedor**.
- **A recomendação na tela:** uma chave só para o celular, com limite de gasto no provedor.

### O aceite
- **Quando:** no primeiro toque no microfone, antes de pedir a chave. Uma folha só, em destaque,
  específica e separada das outras finalidades (art. 8º, §4º; art. 11, I, se a voz levar dado sensível;
  art. 33, VIII, para a transferência internacional). O texto proposto está em
  `docs/politica-de-privacidade.md`, "Texto proposto para o app".
- **Onde fica:** gravado no servidor, por conta, com a data, a versão do aviso e os provedores aceitos.
  Vale para qualquer aparelho da pessoa e para o app nativo do passo 2. Provar o consentimento é do
  controlador (art. 8º, §2º).
- **Quem confere:** o `PUT` da chave, o `PUT` das preferências e a rota de ditar (duas vezes, antes de
  chamar o provedor e antes do Polish e de gravar).
- **Pede de novo:** trocar de provedor, ligar o Polish com outro provedor, mudar o texto do aviso. Mudar
  só o modelo não pede.
- **Revoga:** "Desligar o ditado no celular" revoga o aceite e apaga as chaves (art. 8º, §5º).

### O que o provedor diz (páginas lidas em 10-out-2026)
Resumo; as fontes, o que cada página diz e o que **não** diz estão em `docs/lgpd-dossie.md`, Anexo B.
O servidor **não tem como conferir** a conta da pessoa no provedor (se a retenção zero está ligada, se a
residência de dados está fixada).

| | OpenAI | Groq |
|---|---|---|
| Treino | a página de dados da API diz que, desde 1º-mar-2023, os dados enviados à API não são usados para treinar nem melhorar os modelos, salvo opt-in | a página de dados e a política de privacidade lidas não falam em treino com os dados dos clientes (a política remete esses dados ao contrato de serviço e ao DPA); o DPA limita o tratamento a prestar o serviço, em nome do cliente e conforme as instruções dele, e também não fala em treino |
| Retenção | por padrão, registros de monitoramento de abuso por **até 30 dias**, que podem conter prompts e respostas. A tabela da mesma página mostra "nenhuma" retenção de abuso para `/v1/audio/transcriptions`, que é elegível à retenção zero (ZDR), mas o texto geral fala em até 30 dias. **A política assume até 30 dias**. ZDR e monitoramento modificado dependem de aprovação da OpenAI | por padrão não retém os dados de pedidos de inferência; pode registrar entrada e saída por **até 30 dias**, só para investigar falha de confiabilidade ou abuso, a menos que a conta ligue a retenção zero (ZDR) em Data Controls, que a página diz estar ao alcance de todos os clientes |
| Onde processa | a residência de dados é uma opção de **projeto**: os EUA (processamento e armazenamento regionais) ou a Europa (exige aprovação de controles de retenção); a página manda falar com o comercial para saber da elegibilidade. A transcrição de áudio está na lista dos dois. **Sem a residência fixada, a página lida não diz o país.** O Brasil não aparece na página | os dados que ela retém ficam em buckets do Google Cloud **nos EUA**. **A página não diz onde a transcrição roda.** O DPA diz que a Groq pode transferir e tratar dados nos EUA e em outros países onde ela ou seus subprocessadores operam |

**A política não afirma o que a página não afirma.** Ela escreve o que a documentação diz: para a
OpenAI, "nos EUA ou na Europa, se o projeto da sua chave tiver a residência de dados fixada; sem isso, a
documentação não diz em que país"; para a Groq, "o que ela guarda fica nos EUA; a documentação não diz
onde a transcrição roda; o contrato prevê tratamento nos EUA e em outros países". E trata o envio como
transferência internacional porque nenhum dos dois diz processar no Brasil.

### A dica do aprendido
Desligada por padrão até a C8 medir. Ligada, os termos distintos do aprendido da conta (sem os pares
presos a um domínio, do mais recente ao mais antigo, até 140 runas) vão ao provedor de transcrição como
`prompt`, a cada ditado, **qualquer que seja o conteúdo**. Eles podem ser nomes de clientes ou de
terceiros. O ganho de 61,2% para 79,6% da Entity Accuracy foi medido no initial prompt do whisper.cpp
local, **e não nos modelos da OpenAI nem no whisper da Groq**; a dica pode induzir o termo, e a própria
OpenAI avisa disso para as palavras-chave. No PC o termo aprendido continua fora do prompt (ADR-017,
ADR-020).

### Tela apagada e segundo plano
No Chrome do Android, com a tela apagada ou o app fora da frente, o microfone para ou fica mudo. **No
passo 1 não existe ditado em segundo plano nem com a tela apagada.** O app pede o Wake Lock, para quando
a página sai da frente ou a faixa do microfone fica muda ou acaba, envia o que gravou se tiver fala e
avisa. A política diz isso (THR-031).

### Limites e custo
Em memória; zeram ao reiniciar (como os do login): 1 ditado de cada vez por conta, 10 por minuto, 200 por
dia, 3 ao mesmo tempo no total, e 10 por minuto nas rotas da chave. A defesa principal contra gasto é da
pessoa (uma chave só para o celular, com limite de gasto no provedor). O custo é da pessoa, na conta
dela no provedor; o projeto não paga nada.

### Riscos aceitos
(Os da ADR-021, "Riscos aceitos", que tocam privacidade.)
1. Quem tem a raiz da VPS ouve o áudio em trânsito e decifra as chaves.
2. O país do processamento não está comprovado (Groq, sempre; OpenAI, sem a residência fixada).
3. O servidor não confere se a retenção zero está ligada na conta da pessoa.
4. O reenvio ainda paga de novo em três casos (o prazo de 20 s; o reinício; o desfecho pago que passou
   dos 10 min), a centavos por ditado.
5. No aparelho, "nunca em disco" não é garantido: o `Blob` pode ser paginado pelo Chrome.
6. "Descartado ao fim", e não "apagado".
7. A dica pode induzir termos, e o ganho na nuvem não foi medido.
8. Uma sessão roubada gasta o dinheiro da pessoa, até os limites por conta.
9. A revogação no meio do Polish deixa uma janela de milissegundos; o "Apagar meus dados" não (o epoch
   é conferido dentro do `Create`).
10. As chaves antigas ficam cifradas nos backups e no banco da restauração depois de "Trocar" ou
    "Remover".
11. O proxy da entrada segura pedaços do corpo na memória dele, fora do nosso `memswap_limit`.

### Texto da tela
O texto de Privacidade do app da nuvem e a folha do aceite estão propostos em
`docs/politica-de-privacidade.md`, "Texto proposto para o app", **ainda não aplicados**; entram no app
(C6b) com os testes que conferem as frases. O texto novo diz "a chave do PC continua no PC" e **não**
traz a frase "Gerenciador de Credenciais", que o `bundle.test` recusa no build da nuvem.

## Áudio
- RAM only;
- sem temp;
- backend stream (**depois**, ADR-019: na etapa 5 o áudio **do PC** não sai do PC; o áudio do **celular**
  vai à rota `/dictations` só em memória, ADR-021: ver "Ditar no celular: o que sai do aparelho");
- sem body logging;
- liberar em success/error.

## Contexto
- UIA only;
- sem screenshot;
- protected block;
- bounded/minimized;
- cloud somente se permitido (**na etapa 5, nunca**: a rota `/resolve` não é montada, e o
  `target_app` não existe na nuvem; ADR-019);
- não persistir.

## Logging proibido
- áudio;
- transcript completo por default;
- o texto enviado ao provedor do Polish e o que ele devolve, os `warnings` do modelo e o corpo de
  erro do provedor;
- o ditado do celular, na API (ADR-021): o áudio; o texto (o do provedor, o do Smart e o do Polish); a
  dica; a chave da pessoa, inteira ou em pedaço; o `Ditado-Id`; o corpo do pedido (inclusive um JSON
  malformado que leve a chave); o corpo de erro do provedor (a OpenAI devolve um pedaço da chave no
  `401`). O que vale: o log de acesso, com o método, o padrão da rota, o status, os ms e os bytes, e uma
  linha `ditado:` só com contagens e códigos (duração, fala, bytes, provedor, modelo, desfecho, desfecho
  do Polish, termos na dica, ms do provedor);
- o aprendizado pelo painel (ADR-017): o evento só registra **contagens** (por exemplo, sugestões
  devolvidas, aprendidas e desfeitas, e as ocorrências de desfazer uma troca do próprio sistema, que
  só são contadas); nunca o texto, nem o "de" e o "para" da variante, nem o termo dono de uma
  variante que o dicionário recusou (a frase da tela o cita, mas só a resposta HTTP a carrega). O
  que a API local registra (F3): `sugestoes`, `descartes` e `motivos_do_descarte` (os códigos
  fechados de `learning.Motivo`), o `motivo` de uma edição sem sugestão ou de um pedido recusado
  (uma classe de erro, nunca o texto dele), e `aprendido`/`desfeito` sem campo nenhum. Ver
  `06-api-contract.md`, "Aprendizado";
- o sync do aprendido (ADR-019, F8, 07-out-2026): no Agent e no servidor, só contagens (aplicados,
  não aplicados por motivo, desfeitos, pulados) e códigos de listas fechadas; nunca a variante, o
  termo ou o domínio. O servidor confere o par antes do banco (o id, os 80 caracteres, o caractere
  nulo e o UTF-8, o restrito com domínio) para nenhuma constraint do Postgres pôr a variante no
  `DETAIL` do log dele;
- contexto;
- Authorization;
- cookies;
- tokens;
- keys;
- clipboard;
- na nuvem (ADR-019): a URL crua e a query (o servidor loga só o padrão da rota), o termo buscado, o
  `code` e o `state` do login e do pareamento, o código de conferência, o e-mail (o login recusado
  registra só "login recusado") e o corpo de qualquer pedido. O Agent registra o sync só com
  contagens e códigos. **F7b (06-out-2026):** o laço de sync loga só contagens e códigos de listas
  fechadas (`11-observability.md`); o erro do cliente nunca leva a URL, o token ou o corpo (o erro cru
  do `net/http`, que traz a URL, não sai do cliente); o token é lido do Gerenciador de Credenciais a
  cada rodada, pelo portão, e não fica em memória entre elas; e a fila do sync não guarda texto.
  **F3 (05-out-2026):** o servidor escreve `slog` em JSON no stderr, uma linha por pedido: o método (um
  método fora da lista conhecida vira `outro`), o padrão da rota (`r.Pattern`; `sem rota` quando o
  pedido foi barrado antes dela), o status, os milissegundos, os bytes, as contagens do push e do pull e
  um motivo de lista fechada (`host`, `origem_cruzada`, `limite`, `panico`, ou o do login). O login
  recusado registra `login_recusado` com o motivo (`fora_da_lista`, `sub_diferente`,
  `email_nao_verificado`, `email_em_uso`, `conta_desativada`, `state_invalido`, `cancelado`,
  `token_invalido`, `google_falhou`, `falha_interna`), nunca o e-mail. O IP de quem pede só serve de
  chave ao limite de tentativas, nunca vai a log; o `ErrorLog` do `http.Server` (que escreveria o
  endereço) passa só a categoria; um pânico é registrado só com a rota. A configuração errada sai com a
  variável, nunca o valor. A suíte `authtest` confere que o log não tem e-mail, cookie, token, `code`,
  `state`, a `volta`, o IP nem a query.
- no log do **próprio Postgres** da nuvem (achado da revisão da F2, 05-out-2026): com o padrão, uma
  violação de constraint grava o `DETAIL` "Failing row contains (...)" com o `raw_text` e o
  `final_text`. O erro que sobe na aplicação já sai sem eles (`pgstore.DBError`), mas o servidor do
  banco loga por conta própria. A F11 configura `log_error_verbosity = terse`, `log_statement = none`,
  `log_min_duration_statement = -1`, `log_parameter_max_length_on_error = 0` e `auto_explain`
  desligado, e o roteiro de deploy confere com `SHOW` (ADR-019, "Segurança e privacidade").

### Exceção: nome do executável em foco nos avisos do rastreador
Registrada em 29-set-2026, **autorizada pelo Mauro** (decisão 5 da correção do falso "campo
protegido"; ADR-011, nota de 29-set-2026).

- **O que pode sair:** só o nome-base do executável em foco — por exemplo `brave.exe` —, no campo
  `executavel=` da linha `uia: foco inspecionado`.
- **Onde:** só nas linhas **WARN** desse rastreador (a inspeção em dúvida: falha, timeout, nenhum
  elemento). Nunca em linha Info ou Debug, nem em outro log.
- **O que continua proibido:** título de janela, classe, caminho completo do executável, texto do
  campo ou de perto dele, e qualquer outro contexto de janela.
- **De onde vem:** do processo do elemento com foco, quando a UI Automation o devolveu; senão, do
  processo da janela em primeiro plano (só leitura, depois da falha).
- **Por quê:** sem isso o log de um travamento da UI Automation nunca diz qual aplicativo travou
  (em 29-set-2026, duas chamadas presas de um minuto ficaram sem suspeito).
- **Trava:** `uia.TestDiagnosticoFoco_SoTemCamposQueNaoCarregamTexto` só aceita esse campo
  string, pelo nome; `uia.TestExecutavelDoAviso_SoONomeBase` confere que o valor é só o nome-base
  (sem barra, sem caminho) e só em aviso; `TestRegistroDoFoco_ExecutavelSoNoWarn` (cmd/agent)
  confere que ele só sai no nível WARN.

## Retenção
- 6m active (no PC, só depois do OK de quem usa: abaixo, "No PC");
- trash 30d (no PC, a lixeira que a nuvem ainda não soube espera por ela, no máximo até os 6 meses do
  ditado: a D3 da F10, abaixo);
- purge daily (no PC e na nuvem, na subida e de hora em hora);
- delete definitivo remove raw/final.
- **Nuvem (ADR-019):** os prazos contam pela data do ditado que o PC mandou; o que foi apagado de vez
  dentro da janela deixa lápide, para não ressuscitar; o backup guarda texto apagado até a cópia
  girar (30 dias no Backblaze B2, mais o tempo do B2 para apagar o que a regra escondeu: D2 da F11;
  `docs/politica-de-privacidade.md`, seção 7). "Apagar meus dados da nuvem" apaga tudo da conta
  e revoga os aparelhos; os PCs ficam com a cópia deles.
- **O job de retenção da nuvem (F9, 08-out-2026; ADR-019, "Decisões da F9"):** roda no servidor, na
  subida e de hora em hora, e decide cada item pela data do ditado, linha a linha. O ativo com 6
  meses sai sem lápide; a lixeira vencida dentro da janela deixa lápide, que vive a janela mais 24 h
  (a folga cobre um relógio do servidor que volte atrás); a lixeira que chega à nuvem com a data
  antiga (o PC que ficou sem internet) fica até 24 h na lixeira da nuvem antes de sair, mesmo
  passados os 30 dias, e nunca além dos 6 meses do ditado (D5, decidida pelo Mauro em
  08-out-2026); o evento do sync que nenhuma entidade viva
  guarda e a resposta antiga do push também saem (90 e 190 dias). O job **não** alcança: o backup da
  nuvem (o giro é da F11), as cópias `antes-da-nuvem` e `antes-do-dono` no PC (F10) e a linha do
  aprendido desfeito (abaixo). O log dele é uma linha só com contagens, e o erro, só a categoria.
- **No PC (ADR-019, F10, decidida e implementada em 08-out-2026):** o expurgo da lixeira roda, com a
  guarda da D3; os 6 meses do ativo existem, mas só valem depois do OK. A primeira vez que rodarem,
  apagam de vez os ditados mais antigos: só com a prévia da contagem, uma cópia do banco e o OK
  expresso de quem usa, na tela (o bloco **Histórico** das Configurações; o roteiro manual,
  `docs/teste-manual-nuvem-f10.md`, é rodado por quem usa, nunca por um agente). O desenho (ADR-019,
  "Decisões da F10"; `05`, "Retenção"):
  - **O portão recusa por padrão.** Sem o OK gravado, nada dos 6 meses é apagado, mesmo com o relógio
    anos à frente; o método que apaga lê o OK dentro da própria transação, e não existe parâmetro de
    forçar. O Ligar só vale com a cópia feita e conferida, e sem nada a apagar não há OK.
  - **O que a regra apaga e o que fica.** Apaga o ditado do dono do histórico fora da janela de 6
    meses (ativo ou na lixeira, com o selo e os pendentes), decidido linha a linha em Go e em UTC.
    **Ficam as linhas de outro dono** (as que o painel nunca mostra; a tela diz quantas) e o passo
    **não roda sem o dono fixo**. O aprendido (`dicionario.json` e o espelho do SQLite) não é tocado.
  - **Depois do OK, sai sozinho** (em até 1 h), com **Desligar** (volta ao de hoje) e um **freio**: o
    Agent parado por mais de 7 dias, ou o relógio que pulou para a frente, não apaga nada, tira o OK
    e a pergunta volta. O relógio do PC adiantado apaga antes da nuvem; o freio cobre o salto grande,
    e não um relógio que vive adiantado.
  - **A lixeira de 30 dias** só sai de vez depois de a nuvem saber dela, no máximo até os 6 meses do
    ditado (a D3 da F10): o que a nuvem pode ter e ainda não soube fica além dos 30 dias. Retém a mais
    depois do "Esquecer esta nuvem" e com outra conta (até 6 meses), e o `recusado` sai aos 30 dias.
  - **O disco.** O `historico.db` liga `PRAGMA secure_delete` no DSN, em toda conexão (o driver
    troca a conexão que teve uma consulta cancelada, e um pragma ligado só na primeira se perderia),
    para o texto apagado não ficar nas páginas livres do arquivo; o `-journal` que o SQLite apaga do
    disco, o sistema de arquivos e o backup do Windows ficam fora do alcance.
  - **O log**, só com contagens (apagados, esperando a nuvem, a prévia, os dias parado): nunca id,
    texto nem caminho.
  - As cópias `antes-do-dono`, `antes-da-nuvem` e `antes-dos-6-meses` ficam fora da retenção: o Agent
    **nunca** as apaga sozinho, e elas saem só pelo botão **Apagar as cópias** (F10e), com a pergunta
    antes. A cópia de um envio do histórico que ainda não terminou não pode ser apagada.
- **Ditar no celular (ADR-021):** a **voz** não tem retenção: é descartada ao fim do pedido (no app, ao
  fim do envio ou ao descartar). O texto do ditado do celular tem os mesmos prazos dos outros (6 meses
  pela data do ditado; lixeira de 30 dias). A **chave da pessoa** fica até ela trocar, remover, desligar o
  ditado ou apagar os dados; a cópia cifrada dela fica nos backups (cerca de 30 dias) e no banco anterior
  de uma restauração (7 dias). O **aceite e as preferências** ficam pela vida da conta, e o "Apagar meus
  dados" apaga as três tabelas (`ai_provider_keys`, `dictation_settings`, `dictation_consents`). O
  **desfecho pago e não gravado** fica 10 min em memória. No **provedor**, vale o que a documentação dele
  diz (a tabela de "Ditar no celular").
- Exceção (ADR-017): a variante que o painel aprende é um trecho de até 4 palavras do que o ASR
  escreveu, gravado no `dicionario.json`. O dicionário persiste até exclusão, então a variante **não
  expira** com os 6 meses (nem com a lixeira) do ditado de onde veio; sai quando a pessoa a desfaz. A
  Correction, com a frase inteira, não é guardada nesta etapa justamente por isso.
- O aprendido sincronizado (ADR-019, F8, 07-out-2026), além do `dicionario.json`:
  - **no espelho do PC** (`historico.db`): a variante e o termo da última sincronização, o par que
    chegou e não vale aqui ("não aplicado neste PC") e, com as etiquetas da D4, a troca deste PC que
    perdeu na nuvem (o par perdido, a 0008: a linha de estado do "trocado em outro aparelho"; sai com a
    etiqueta, em até 30 dias ou quando a pessoa aprende de novo, e no esquecer). O texto da linha desfeita sai do espelho quando
    a nuvem confirma o desfazer (na linha com a etiqueta "desfeito em outro aparelho" ou "trocado em
    outro aparelho", quando a etiqueta some: em até 30 dias, ou quando a pessoa aprende de novo; os
    30 dias são a escolha menor 2 da ADR, a confirmar); fica só o id (para um epoch novo mandar o
    desfazer de novo). O esquecer apaga o texto do que veio da conta esquecida e não está no arquivo
    (o não aplicado, o pendente, o arquivo indisponível e as etiquetas: a nuvem que o mandou foi
    esquecida e nunca mais o limparia), e o "desfeito aqui" que ele guarda fica só com o id (o
    desfazer não leva texto); fica o id de tudo. A linha que vale no arquivo guarda o que o
    `dicionario.json` já tem. Como no arquivo, nada disso vence com os 6 meses do ditado;
  - **na nuvem:** o par ativo fica até ser desfeito. A linha desfeita continua no Postgres, com a
    variante e o termo (a 0016 exige o `term`; "desfeito em outro aparelho" precisa ser lembrado),
    **enquanto a conta existir** (F9, D1, confirmada pelo Mauro em 08-out-2026): sai só no "apagar
    meus dados", e o job de retenção não a toca. Sem a linha, um PC que ainda tem o par o reenviaria
    e a nuvem o criaria como novo: a troca desfeita voltaria a todos os aparelhos. Ou seja, o texto de
    uma troca desfeita **não vence** com os 6 meses do ditado, no servidor nem no backup (que a guarda
    até a cópia girar). O texto de privacidade diz isso (`docs/politica-de-privacidade.md`, seções 7 e
    10). **A revisão de LGPD da F11 (D3, 09-out-2026) manteve a escolha**; a alternativa (vencer N dias
    depois do desfazer, ou guardar só o id) segue possível se o Mauro mudar de ideia ao ler o dossiê.
- Exceção conhecida: na primeira subida com o dono fixo, o Agent copia o banco local inteiro para
  `historico.db.antes-do-dono-AAAAMMDD-HHMMSS.db`, ao lado dele (até 3 cópias, se a migração
  falhar), antes de migrar (`05-data-model.md`, "Dono local"). A cópia tem todo o texto ditado até
  ali, **inclusive a lixeira e o que o expurgo apagaria depois**, e o Agent nunca a apaga: o
  expurgo não a alcança, e um "delete definitivo" feito depois não a limpa. Recomendação: apagá-la
  assim que o painel mostrar o histórico antigo. Fica fora do git (`/bin/`, `*.db`).
- Exceção conhecida, a mesma (ADR-019, F7c): antes do envio inicial, o Agent copia o banco inteiro
  para `historico.db.antes-da-nuvem-AAAAMMDD-HHMMSS.db`, ao lado dele (uma por conta; até 3, se a
  cópia falhar). Ela tem todo o texto ditado até ali, inclusive a lixeira, e o Agent nunca a apaga
  (nem o "Esquecer esta nuvem"): fica até quem usa a apagar, pelo botão **Apagar as cópias** da F10
  (decidido em 08-out-2026; enquanto o envio do histórico não termina, ela não pode ser apagada).
  Recomendação: apagá-la depois de conferir o histórico no app da nuvem. O log diz só se copiou,
  nunca o caminho.
- A mesma exceção vale para a cópia `historico.db.antes-dos-6-meses-AAAAMMDD-HHMMSS.db` (F10, uma só,
  feita no OK dos 6 meses): tem todo o texto que a retenção apaga, e o Agent nunca a apaga sozinho.
  Enquanto ela existe, os 6 meses não valem no disco; o assunto entra na revisão de LGPD da F11.

## Pastas e instalação (ADR-018)
- **Programa e dados em pastas separadas.** O programa (só leitura em uso) fica em
  `%LOCALAPPDATA%\Programs\Aurora Whisper`; os dados (`historico.db` com as preferências e o dono
  fixo, as cópias `antes-do-dono`, `dicionario.json`, `agent.log`, `whisper-server.log`) ficam em
  `%LOCALAPPDATA%\AuroraWhisper`. A ACL de `%LOCALAPPDATA%` dá acesso só a quem é dono, ao SYSTEM e
  aos Administradores; o layout de desenvolvimento (`bin\`, herdando a ACL de `C:\`) é legível por
  qualquer conta local, e só serve ao desenvolvimento. A decisão de onde é do pacote
  `internal/agent/pastas`.
- **Modo portátil** (arquivo `portatil` ao lado do executável, ou um `historico.db` ali, com aviso
  no log): os dados ficam ao lado do executável. É o `bin\` de desenvolvimento; o instalador nunca
  cria o marcador nem um banco. Só o `bin\` montado pelo `asrstage` fica fora dos dados reais de quem
  usa o instalado: um Agent de desenvolvimento em outra pasta, sem o marcador, usa
  `%LOCALAPPDATA%\AuroraWhisper` de verdade (ADR-018, risco 11).
- **O instalador faz uma só conexão de rede**, e só se faltar o runtime do Visual C++ (x64, 14.51 ou
  mais novo): baixa o redistribuível oficial da Microsoft (`aka.ms/vc14/vc_redist.x64.exe`, por
  HTTPS) e **só o executa se a assinatura for válida e de "Microsoft Corporation"**, porque ele roda
  com permissão de administrador e o endereço aponta sempre para a versão mais nova (não há hash
  fixo). O Agent não baixa nada. O Setup leva o modelo de voz, então não há download dele.
- **O Agent cria um `dicionario.json` vazio** na pasta de dados, na subida, só se não houver nada
  ali (para o aprender do painel funcionar desde o primeiro dia; ADR-018). Nunca sobrescreve um
  dicionário existente, e o arquivo vazio não tem nada de quem usa.
- **Sem assinatura nas 0.x, o THR-010** (atualização maliciosa) **é coberto só por hash** (SHA-256
  do Setup): protege contra arquivo corrompido, não contra quem controla o release.
- **O log não grava caminho completo de pasta**, porque ele leva o nome de usuário do Windows (e o
  log é o que se cola num issue): só o modo (`dados=portatil`, `dados=portatil-legado` ou
  `dados=localappdata`); o que sobrar de caminho numa mensagem de erro sai com rótulo
  (`<dados>`, `<programa>`, `<perfil>`). O `whisper-server.log` é a saída do próprio servidor, e duas
  linhas dele trazem caminho, ambas impressas pelo whisper.cpp: a do **modelo** e a da **biblioteca do
  processador** (`load_backend: loaded CPU backend from <pasta do executável>\ggml-cpu-*.dll`).
  Por isso há duas medidas (F2 da ADR-018, **pré-requisito de qualquer Setup distribuído**): o Agent
  roda o servidor com a pasta de trabalho no programa e lhe passa o modelo pelo **nome do arquivo**
  (tira o caminho da primeira); e a saída do servidor passa pelo **mesmo filtro de caminhos do
  `agent.log`**, linha a linha (`pastas.AnonimizarLinhas`; a pasta do executável do servidor é a do
  programa instalado, e `cmd.Dir` não a muda). O filtro troca o caminho também na codificação ANSI
  (Latin-1), caso o servidor escreva o nome com acento na página de código do Windows. Conferido em
  teste com a saída partida em pedaços; **o log de um servidor de verdade ainda não foi lido depois
  da mudança**, e a conferência é no log **inteiro** (nenhum `X:\` e nenhum nome de conta), não só na
  linha do modelo (roteiro manual do instalador, I5).
- **A desinstalação** pergunta se apaga os dados (padrão: não; no modo silencioso nunca apaga). Se a
  pessoa confirmar, e com o Agent fechado, apaga **só** a pasta exata `%LOCALAPPDATA%\AuroraWhisper`
  (montada no código, nunca um atalho ou junção, e sem relação com a pasta do programa; na dúvida,
  recusa) e as **duas** chaves de API do Gerenciador de Credenciais, `AuroraWhisper/openai` e
  `AuroraWhisper/anthropic` (e, desde a F6, o token da nuvem, abaixo: **três** credenciais), pelo
  nome exato e pela mesma chamada do Agent (`CredDeleteW`; `installer/aurora-whisper.iss`,
  `AlvoDaChaveOpenAI`, `AlvoDaChaveAnthropic` e `AlvoDaChaveNuvem`). Não é um curinga
  `AuroraWhisper/*`, como este texto dizia antes da ADR-019. E remove sempre o `Run\AuroraWhisper` e
  a marca em `StartupApproved` se apontarem para o executável desinstalado. Se o Agent estiver
  aberto, pede confirmação antes de fechá-lo.
- **O token do PC na nuvem (ADR-019, F6):** a partir da F6, a opção "apagar dados" apaga também o
  token do PC (`AuroraWhisper/nuvem`; `#define AlvoDaChaveNuvem`, conferido contra o Go). Só nessa
  opção: apagar sempre desconectaria o PC a cada atualização. Apagar o token local não revoga o
  aparelho na nuvem; ele fica na lista até ser revogado pelo celular ou pelo navegador, ou até 180
  dias sem uso (a pergunta do desinstalador diz isso). O `AuroraWhisper/nuvem-portatil`, do `bin\` de
  desenvolvimento, não é instalado, e o desinstalador não o toca. **Feito na F6.**
- **O pacote do instalador nunca leva dicionário**, nem o pessoal de quem desenvolve: o `asrstage
  -destino` não o copia (e recusa uma pasta que já tenha um).

## LGPD
Antes de distribuir para terceiros, revisar formalmente base legal, transparência, provedores, retenção e direitos do titular.

**Pendente com o Polish (ADR-016):** o texto ditado passa a ir para OpenAI ou Anthropic, com a chave de quem usa, e os provedores podem retê-lo por até 30 dias. A revisão precisa cobrir esse envio, a retenção no provedor e o aviso que a tela dá. Nada disso foi revisado.

**Pré-condição do deploy da nuvem (ADR-019, 01-out-2026).** Com a nuvem, o texto ditado deixa de
ficar só no PC e passa a morar, legível, num servidor. Nenhum dado real sobe antes de a revisão
cobrir:
- a base legal e a transparência (o texto da tela e o da política de privacidade);
- a retenção na nuvem, inclusive a do backup (o giro), e as cópias do banco feitas no PC;
- os direitos do titular: excluir definitivamente, "apagar meus dados da nuvem", acesso;
- o aprendido (a variante, que é um trecho do que a pessoa ditou, e o termo), no servidor e no
  espelho do PC, inclusive a linha desfeita que a nuvem guarda (ADR-019, F8);
- o país e a região do servidor (o diagnóstico pede que o dono informe);
- dados de terceiros que a pessoa dite (de clientes, por exemplo);
- quem administra o servidor e lê o texto.

**Estado (F11b, 09-out-2026; aprovado na instância de referência em 10-out-2026, ADR-019).** A resposta
a cada item está em [`docs/lgpd-dossie.md`](../lgpd-dossie.md) (duas páginas, mais um anexo que confere
cada número no código), e o texto que a pessoa lê é o modelo
[`docs/politica-de-privacidade.md`](../politica-de-privacidade.md), com o texto proposto para o app
(que **ainda não foi aplicado**). Os pontos em aberto da D3 (a linha do aprendido desfeito, as cópias
do banco no PC e o Polish) ficam como estão e a política os descreve. O servidor de entrada (o Traefik
da mesma VPS, com o registro de acesso desligado só no router da nuvem: `deploy/compose.yml`) está
descrito. **O que é de cada instância fica marcado `[A PREENCHER]` no modelo:** o responsável e o
contato de privacidade, o país da VPS, a região do bucket do B2 (e a hipótese do art. 33 para a cópia
cifrada fora do Brasil). O dossiê levanta duas lacunas que este documento não dizia: o "apagar meus
dados" não apaga o cadastro (e-mail, identificador do Google e nome) e a nuvem não tem exportação.
Numa instância nova, sem essa revisão o deploy com dado real não roda.

**Ditar no celular (ADR-021, D0, 10-out-2026).** A voz, a dica e o Polish do celular são uma finalidade
nova e um destinatário novo: a política e o dossiê foram reescritos (na política, o resumo e as seções
3 a 13, mais a folha do aceite e os textos propostos para o app; no dossiê, as seções 1 a 7, e os
Anexos B, com o que cada provedor diz e **não** diz, e C, com os artigos conferidos no texto da lei).
Pontos que a revisão cobre:
- a voz sai do aparelho, passa pela memória da VPS e vai a um provedor que a pessoa escolhe, em
  linguagem simples e **antes** do primeiro áudio (a folha do aceite; art. 8º, §4º e art. 9º);
- a **transferência internacional** da voz, que é legível (não cifrada pelo Aurora) e vai a um
  provedor que trata dados nos EUA e, conforme a conta, na Europa (art. 5º, XV; art. 33). A hipótese
  proposta é o inciso VIII (consentimento específico e em destaque, com informação prévia do caráter
  internacional); a escolha é do responsável;
- o item 4 do dossiê (dado de terceiros) **estendido à voz e à dica**: quem dita dado de cliente pode
  ser operador dele (art. 39), e mandar a voz ou os termos a um provedor no exterior pode ir além do
  que o cliente autorizou. **Na instância de referência, o Mauro confirmou em 10-out-2026 que dita dado
  de cliente pelo celular às vezes**;
- quem tem a raiz da VPS ouve a voz em trânsito e decifra as chaves da pessoa;
- as chaves antigas, cifradas, nos backups e no banco da restauração.

**Pré-condição do primeiro áudio real:** o OK do Mauro na política e no dossiê reescritos. Recomenda-se
fechar antes as pendências L1 a L5 do dossiê (a região do bucket, a hipótese do art. 33 do backup, o
contato de privacidade, exportar e o cadastro): somar uma segunda transferência internacional, agora de
dado legível, antes de dar base escrita à primeira, piora a situação.

## Operações destrutivas
[SECURITY][BLOCKER] Exigem aprovação:
- purge manual massivo;
- migration destrutiva;
- reset DB;
- deploy;
- DNS;
- rotação secrets;
- delete volume;
- release;
- revoke massivo;
- restaurar o backup da nuvem (troca o `sync_epoch`, e todos os PCs recomeçam; ADR-019);
- encostar no proxy que já roda no servidor (ADR-019);
- ligar os 6 meses no PC pela primeira vez (apaga ditados reais; ADR-019, F10): o OK é o clique de
  quem usa, depois da contagem; nenhum agente o dá, nem roda o Ligar, nem abre o `historico.db` do
  `bin\`;
- apagar as cópias do banco ao lado dele (F10e): só pelo botão de quem usa, com a pergunta antes;
- mandar o **primeiro áudio real** do celular a um provedor (ADR-021): só depois do OK do Mauro na
  política e no dossiê reescritos; a calibração da C6c e a medição da C8 também têm OK próprio;
- gerar ou **trocar a chave de cifra** das chaves da nuvem (ADR-021): gerar é com ele, na VPS, com a
  saída suprimida; trocar leva toda chave guardada a `SEM_CHAVE`;
- colar a chave de API da pessoa: só no app, nunca no chat; qualquer **arquivo de áudio** (por exemplo,
  para comparar com e sem a dica) é persistência de áudio e parada própria.
