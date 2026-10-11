# Dossiê de LGPD: a nuvem do Aurora Whisper

Escrito em 09-out-2026 (ADR-019, F11b) para a revisão de LGPD da instância de referência, aprovada em
10-out-2026 (ADR-019), e mantido como **exemplo para quem hospeda a própria nuvem**: o que é de uma
instância (o país da VPS, a região do bucket, o contato) aparece como exemplo ou como campo
**[A PREENCHER]**. Numa instância nova, **nenhum dado real sobe antes de o responsável fazer esta
revisão** (`docs/ai-spec/09-security-privacy.md`, "LGPD"). O texto que a pessoa lê é o modelo
`docs/politica-de-privacidade.md`. O **Anexo**, depois destas duas páginas, só serve para conferir cada
afirmação no código.

**Por que isto existe.** Até aqui o que se dita ficava só no PC. Com a nuvem, o texto passa a morar,
**legível**, num servidor, e uma cópia cifrada vai para um backup. A Lei 13.709/2018 manda dizer à
pessoa a finalidade, a forma e a duração do tratamento, quem controla e com quem os dados são
compartilhados (art. 9º).

**Quem é quem (proposta).** Quem sobe a nuvem e escolhe quem entra é o **controlador** (art. 5º,
VI). A VPS e o Backblaze tratam os dados em nome dele: são **operadores** (art. 5º, VII). O projeto não
tem servidor central e não recebe nada. **Base legal proposta: consentimento** (art. 7º, I): a pessoa
escolhe conectar, vê quantos ditados vão subir, clica em "Enviar o histórico" e revoga quando quiser
(art. 8º, §5º) pelo Desconectar e pelo "Apagar meus dados". O clique fica gravado só no PC, não na
nuvem (provar o consentimento é ônus do controlador: art. 8º, §2º). O art. 4º, I só tira da lei o
tratamento "para fins exclusivamente particulares e não econômicos"; se quem usa dita também para o
trabalho, o dossiê parte de que a lei vale.

## 1. O que sai do PC

Só depois de **conectar** ("Desligada. Nada vai para a nuvem até você conectar"). Conectado, cada
ditado novo sobe sozinho; o histórico antigo só sobe depois do clique em "Enviar o histórico".

- **Sai:** de cada ditado, o texto bruto e o final, a data, o modo, o estado, a lixeira e os detalhes da
  transcrição (modelo, idioma, duração, confiança); o aprendido (variante, termo, restrição, nome do
  domínio); o **nome do computador**, a plataforma, a versão e um número aleatório da instalação. Do
  login Google: **e-mail, identificador da conta e nome**.
- **Nunca sai:** áudio; o que está em outras janelas (programa, título, texto de perto); a chave do
  Polish; logs; preferências; o mapa de domínios.

## 2. Onde fica, e quem lê

| Lugar | O que tem | Quem lê |
|---|---|---|
| PC | tudo, em `historico.db` e `dicionario.json`, sem cifra própria | quem entrar na conta do Windows (no instalado, a pasta é só do dono, do SYSTEM e dos Administradores) |
| Servidor (Postgres na VPS) | ditados **legíveis**, aprendido, cadastro; sessões e aparelhos só como HMAC | **quem administra a VPS lê o texto**; cifra ponta a ponta fica para depois |
| Backup no B2 | o banco cifrado com `age` | só quem tem a chave privada (a do responsável); o B2 vê o arquivo, o nome com data e hora, o tamanho |
| Celular | nada do texto gravado (só na memória da página); a casca do app e o cookie | a pessoa |

## 3. Prazos

| O quê | Quanto tempo |
|---|---|
| Ditado ativo na nuvem | **6 meses** da data do ditado; o job apaga de hora em hora |
| Lixeira | **30 dias** da ida à lixeira, nunca além dos 6 meses do ditado; a que chega atrasada (PC sem internet) fica ao menos **24 h** |
| Apagado de vez | sai na hora; fica uma **lápide** (id e data do ditado, sem texto) até 6 meses e 24 h depois da data do ditado |
| Aprendido **desfeito** | **enquanto a conta existir** (variante e termo, marcados como desfeitos); só o "Apagar meus dados" o tira |
| Sessão / token do PC | 30 dias sem uso, teto de 90 / 180 dias sem uso |
| **Backup no B2** | **30 dias** (regra do bucket) mais o tempo do B2 para apagar o que a regra escondeu (conferir no painel); guarda também texto que a retenção já apagou |
| Cifrado na VPS / banco antigo da restauração | 3 dias / 7 dias (o `DROP` é do responsável, à mão) |
| **No PC** | lixeira 30 dias (depois de a nuvem saber dela); os 6 meses só com o OK da pessoa; as cópias `antes-do-dono`, `antes-da-nuvem` e `antes-dos-6-meses` **não têm prazo**: só saem pelo botão "Apagar as cópias" |

## 4. Operadores e países

| Quem | Recebe | País / região |
|---|---|---|
| VPS | tudo o que está na nuvem, legível | **[A PREENCHER: país e região do datacenter]** (no exemplo, Brasil). A empresa de hospedagem: **[A PREENCHER]** |
| O Traefik (a entrada do https, na mesma VPS) | o IP de quem acessa e o endereço pedido, de passagem; o registro de acesso dele fica **desligado** só para a nuvem | o mesmo lugar da VPS |
| Google (login) | que a pessoa entrou; devolve e-mail, identificador e nome | a confirmar nas condições do Google |
| Backblaze B2 | só o arquivo cifrado | **[A PREENCHER: região do bucket]** (a da conta, escolhida na criação: `docs/backup-b2-f11.md`; pelo que se sabe, o B2 não tem região no Brasil) |
| OpenAI ou Anthropic | **só no Polish**, opt-in, com a chave da pessoa: o texto final (4 palavras ou mais), até 30 dias lá. **Fora da nuvem** | a confirmar |

**O texto legível fica no país da VPS** (no exemplo, o Brasil, com a VPS e a entrada do https no
mesmo lugar). O que sai do Brasil é o backup, **cifrado**, para a região do B2, e o login, que passa
pelo Google. Para o backup há transferência internacional (art. 5º, XV) de um arquivo que o B2 não
consegue ler, e o art. 33 exige uma das suas hipóteses (por exemplo, o inciso VIII: consentimento
específico e em destaque, com aviso prévio de que é internacional): a região e a hipótese **têm de
estar preenchidas antes do primeiro dado real**. O DNS fica só em modo DNS, sem proxy de terceiro (ele
leria o texto): o domínio da nuvem aponta direto para a VPS.

## 5. Direitos da pessoa (art. 18)

| Direito | Como, hoje |
|---|---|
| Confirmar e acessar (I, II) | o app da nuvem ou o painel do PC: lista, busca, aparelhos, sessões, aprendido. Pedido fora do app: declaração em até 15 dias (art. 19, II) |
| Corrigir (III) | editar o texto final |
| Eliminar (IV, VI) | lixeira; **Excluir definitivamente** (só de ditado já na lixeira); **Apagar meus dados da nuvem** (digitar "apagar": ditados, lápides, aprendido, aparelhos e outras sessões; os PCs ficam com a cópia; o backup guarda até girar) |
| Revogar (IX) | Desconectar (revoga o PC na nuvem), revogar um aparelho, encerrar uma sessão, Sair |
| Cópia e portabilidade (V; art. 19, §3º) | o PC já tem tudo em `historico.db`. **Não há botão de exportar na nuvem** |

## 6. Restauração e invasão

**Restaurar o backup** (decisão do projeto em 09-out-2026, ADR-019: "aceitar e deixar escrito"): o que foi
apagado de vez depois do último backup **pode voltar**, se ainda estiver nos 6 meses, e a pessoa o
apaga de novo; o que os 6 meses já venceram, a nuvem apaga antes de abrir a porta. O que só existia na
nuvem depois do backup pode se perder. Todas as sessões e todos os PCs são desligados, e cada PC
conecta de novo. Pode aparecer um "selo" (duas versões do mesmo ditado; a pessoa escolhe).

**Alcance de uma invasão.** Se a nuvem divide a VPS com outros serviços expostos à internet (como na
instância de referência, com outros sites atrás do mesmo Traefik), quem tomar um deles e chegar à raiz
do Docker alcança o volume do Postgres e os arquivos de segredo, e leva os ditados dos últimos 6 meses
e da lixeira, o aprendido, o e-mail, o identificador e o nome. Sem chegar à raiz, quem divide a rede do
proxy já alcança a porta da API direto (sem o Traefik): o login, as sessões e os tokens valem igual,
mas ele pode driblar o limite de tentativas por IP. Não leva áudio, contexto de janela nem os backups: eles são
cifrados, e a chave do B2 que fica na VPS não tem `deleteFiles`; mas ela pode **esconder** os arquivos,
e a regra do bucket apaga o escondido, então quem os segura é o **Object Lock** do bucket (cada cópia
fica 30 dias, nem a regra a apaga antes). Sem o Object Lock, a chave apagaria os backups, só que com
um dia de atraso. Postgres sem porta e backup cifrado **não impedem isso**; uma VPS só da Aurora
(opção B da D1) reduz o alcance. Num incidente com risco relevante, o controlador avisa a ANPD e as
pessoas (art. 48); a cifra só atenua onde o dado está ilegível (art. 48, §3º), e o Postgres no ar não
está.

## 7. O que o responsável decide

1. **País e região.** A VPS: **[A PREENCHER]** (no exemplo, Brasil). O bucket do B2: a região da conta
   e a hipótese do art. 33 (seção 4). Bloqueia o primeiro dado real.
2. **O cadastro não sai.** O "Apagar meus dados" deixa o e-mail, o identificador e o nome (a linha em
   `users`); não existe "excluir a conta". Aceitar e escrever na política, ou pedir a rotina (um
   comando no roteiro, ou um botão). O cadastro é dado pessoal (art. 5º, I).
3. **Exportar.** Aceitar a falta do botão (o PC tem a cópia), ou pedir o botão.
4. **Texto de terceiros.** O ditado pode ter dado de clientes ou, sem querer, dado sensível (art. 5º,
   II); a nuvem não separa nem filtra. Com dado de cliente, quem usa pode estar como operador dele (art.
   5º, VII; art. 39), e a nuvem vira mais um lugar onde ele mora. Ditar isso na nuvem é decisão de quem
   usa.
5. **Contato de privacidade e encarregado** (art. 41; a dispensa depende de norma da ANPD, art. 41,
   §3º, que não está entre os textos conferidos): [A PREENCHER: nome e e-mail].
6. **O selo na restauração** (pendente da F11d): aceitar e escrever (sugestão) ou consertar no PC numa
   fatia própria.

Já aceitos no projeto (D3 da ADR-019) e descritos na política: o aprendido desfeito na nuvem e no
backup; as cópias do banco no PC; o Polish fora da nuvem. Na instância de referência, a revisão foi
aprovada em 10-out-2026 (ADR-019).

---

# Anexo: conferência no código (fora das duas páginas)

Cada número do dossiê, com o lugar onde se confere. `arquivo:linha` é do commit em que este anexo foi
escrito.

| Afirmação | Onde |
|---|---|
| 6 meses do ativo, 30 dias da lixeira | `internal/retention/policy.go:9`, `:18` |
| Piso de 24 h; vida da lápide | `internal/server/cloudstore/retencao.go:39`, `:169-179` (`VencimentoDaLixeira`), `:185-194` (`LapideVencida`, `DeixaLapide`); `migrations/0015_tombstones_table.sql` |
| Lápide sem texto (id, data, motivo) | `migrations/0015_tombstones_table.sql` (colunas) |
| Eventos 90 dias; respostas 190 dias (180 + 10) | `retencao.go:46`, `:55`; `cloudstore/pareamento.go:27` |
| Job de hora em hora, na subida e antes da porta | `internal/server/nuvem.go:42`, `:321-345`; `cmd/server/serve.go:135-138` (`abrir`) |
| Sessão 30 dias sem uso, teto 90; token do PC 180 dias | `nuvem.go:33-35`; `cloudstore/pareamento.go:27` |
| Aprendido desfeito só sai no "Apagar meus dados" (o único `DELETE FROM learned_pairs`) | `internal/server/pgstore/contas.go:100`; `migrations/0016_learned_pairs_table.sql`; ADR-019, D1 da F9 |
| Campos que o servidor aceita do ditado (lista fechada, sem contexto) | `internal/server/cloudstore/payload.go:30-44`; `06-api-contract.md`, "Payload de criar" |
| O que o login guarda (e-mail, `sub`, nome; `avatar_url` existe e não é preenchido) | `migrations/0001_users_table.sql`; `cloudauth/google.go:28`, `cloudauth/idtoken.go:85`; `pgstore/contas.go:46` |
| Nome do computador e número da instalação | `cmd/agent/nuvem.go:59`; `internal/agent/nuvem/servico.go:387` |
| Nada vai antes de conectar | `web/src/NuvemNoPC.tsx:274`; ADR-018 |
| O clique do envio fica só no PC | `internal/storage/aprendido_sqlite.go:894` (`ChaveNuvemEnvioAceitoEm`) |
| Apagar meus dados: o que sai e o que fica | `internal/server/pgstore/contas.go:91-105`, `:321-352`; nenhum `DELETE FROM users` no código |
| Excluir definitivamente só da lixeira | `internal/server/store_routes.go:293-296` |
| Rotas de acesso, sessões, aparelhos | `internal/server/rotas.go:87-98`, `:117-118` |
| Desconectar revoga na nuvem | ADR-019, "Decisões da F6" (decisão 1) |
| O app não grava texto no navegador | ADR-019, "O rascunho que fica até entrar de novo"; THR-021 do `09` |
| Backup: cifra, B2, chave sem `deleteFiles` num bucket com Object Lock, 30 dias, 3 dias no volume; o papel só de leitura | ADR-019, D2; `deploy/scripts/backup.sh:60`; `deploy/README.md` ("O bucket e a chave do B2"); `deploy/scripts/conferir-b2.sh`; `deploy/postgres/init/10-papel-da-aplicacao.sh` |
| Postgres sem porta publicada, HTTPS na borda, log de acesso do proxy | `deploy/compose.yml` (nenhum `ports`; o Postgres só na rede `internal: true`; o rótulo `observability.accesslogs=false` no router da nuvem); `deploy/compose_test.go`; o `scripts/deploy-teste.ps1` prova, com um Traefik de verdade (a versão fixada no `deploy/compose.teste.yml`), que o log de acesso registra os outros pedidos e não os da nuvem |
| País e região da VPS | o da instância (no exemplo, Brasil); na de referência, a D1 (ADR-019, "Decisões da F11") |
| Banco antigo 7 dias; cópia "antes de restaurar" | `deploy/scripts/restaurar.sh:24-44` |
| Restauração: o apagado volta, tudo é revogado | `docs/ai-spec/03-acceptance-criteria.md` (AC-061); `12-rollout.md` ("Rollback"); `internal/server/integration/sync_restauracao_e2e_test.go` |
| Cópias no PC só saem pelo botão | `09`, "Retenção" (F10e); `internal/agent/expurgo/expurgo.go:47-56` (só o `ApagarCopias` as apaga); `web/src/HistoricoNoPC.tsx` |
| Polish: o que sai e por quanto tempo fica no provedor | `09`, "Polish: o que sai da máquina"; ADR-016 |

## A pré-condição do `09`, item a item

| Item pedido pelo `09` ("LGPD", pré-condição do deploy) | Resposta | Onde |
|---|---|---|
| Base legal e transparência (texto da tela e política) | consentimento (proposta); a política como modelo, com os campos de cada instância; o texto novo do app está na política, **ainda não aplicado** | "Quem é quem"; `docs/politica-de-privacidade.md`; `web/src/nuvem/ConfiguracoesNuvem.tsx:448-456` |
| Retenção na nuvem, backup (o giro) e cópias no PC | seção 3 | acima |
| Direitos: excluir definitivamente, "apagar meus dados", acesso | seção 5; duas lacunas (cadastro, exportar) | seção 7, pontos 2 e 3 |
| O aprendido, no servidor e no espelho do PC, inclusive a linha desfeita | seção 3; aceito na D3 (ADR-019) | `09`, "Retenção" |
| País e região | a VPS (no exemplo, Brasil) e o bucket do B2 **em branco, marcados** | seção 4 |
| Dados de terceiros | a nuvem não separa; decisão de quem usa | seção 7, ponto 4 |
| Quem administra o servidor e lê o texto | quem administra a VPS lê; alcance de invasão | seções 2 e 6 |

## O que a conferência achou e o `09` não dizia

- O **nome** da conta Google é guardado (`users.display_name`); o `09` só falava em e-mail e `google_sub`
  (corrigido no `09`).
- O "Apagar meus dados" **não apaga o cadastro** (seção 7, ponto 2).
- Não há como **exportar** os dados da nuvem (seção 7, ponto 3).
- O "Excluir definitivamente" só vale para um ditado **que já está na lixeira**.
- O **nome do computador** vai para a nuvem e a linha do aparelho fica no banco mesmo depois de
  revogado (a lista de aparelhos deixa de mostrá-la); só o "Apagar meus dados" a tira.
