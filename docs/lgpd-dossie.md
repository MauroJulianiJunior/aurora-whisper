# Dossiê de LGPD: a nuvem do Aurora Whisper

Escrito em 09-out-2026 (ADR-019, F11b) para a revisão de LGPD da instância de referência, aprovada em
10-out-2026 (ADR-019), e mantido como **exemplo para quem hospeda a própria nuvem**: o que é de uma
instância (o país da VPS, a região do bucket, o contato) aparece como exemplo ou como campo
**[A PREENCHER]**. Numa instância nova, **nenhum dado real sobe antes de o responsável fazer esta
revisão** (`docs/ai-spec/09-security-privacy.md`, "LGPD"). O texto que a pessoa lê é o modelo
`docs/politica-de-privacidade.md`. Os **Anexos** só servem para conferir cada afirmação: o A, no código;
o B, nas páginas dos provedores; o C, no texto da lei.

**Reescrito em 10-out-2026 (ADR-021, D0) com o ditado no celular.** A aprovação de 10-out-2026 cobria a
nuvem como ela era (texto e aprendido, sem voz). O ditado no celular traz uma finalidade, um
destinatário e uma transferência internacional novos, e **tem uma revisão à parte: nenhum áudio real
sai do aparelho antes do OK do responsável neste dossiê e na política reescritos**. Ele ainda não
existe no código; as seções marcadas "ditado no celular" descrevem o desenho da ADR-021.

**Por que isto existe.** Até aqui o que se dita ficava só no PC. Com a nuvem, o texto passa a morar,
**legível**, num servidor, e uma cópia cifrada vai para um backup. **Com o ditado no celular, a voz
também sai: ela passa pela memória do servidor e vai, legível, a um provedor de transcrição que a pessoa
escolhe, com a chave dela, e a chave dela passa a morar, cifrada, no servidor.** A Lei 13.709/2018 manda
dizer à pessoa a finalidade, a forma e a duração do tratamento, quem controla e com quem os dados são
compartilhados (art. 9º).

**Quem é quem (proposta).** Quem sobe a nuvem e escolhe quem entra é o **controlador** (art. 5º,
VI). A VPS e o Backblaze tratam os dados em nome dele: são **operadores** (art. 5º, VII). **No ditado
do celular, a OpenAI, a Groq e (no Polish) a Anthropic também são operadores**, e, com a chave da
própria pessoa (BYOK), o contrato com cada uma é o da **conta dela**; o responsável pela nuvem não
paga nada a elas. O projeto não tem servidor central e não recebe nada. **Base legal proposta:
consentimento** (art. 7º, I): a pessoa escolhe conectar, vê quantos ditados vão subir, clica em "Enviar o
histórico" e revoga quando quiser (art. 8º, §5º) pelo Desconectar e pelo "Apagar meus dados". O clique
fica gravado só no PC, não na nuvem (provar o consentimento é ônus do controlador: art. 8º, §2º). **Para
o ditado no celular, o consentimento é à parte e fica gravado no servidor**: uma folha específica, em
destaque e separada das outras finalidades (art. 8º, §4º), no primeiro toque no microfone, com data,
versão do aviso e provedores. Por cautela, a mesma folha serve ao consentimento específico para
compartilhar com outro controlador, caso o provedor seja assim qualificado (art. 7º, §5º), e à hipótese
do art. 33, VIII (seção 4). O art. 4º, I só tira da lei o tratamento "para fins exclusivamente
particulares e não econômicos"; se quem usa dita também para o trabalho, o dossiê parte de que a lei
vale.

## 1. O que sai

**Do PC.** Só depois de **conectar** ("Desligada. Nada vai para a nuvem até você conectar"). Conectado,
cada ditado novo sobe sozinho; o histórico antigo só sobe depois do clique em "Enviar o histórico".

- **Sai:** de cada ditado, o texto bruto e o final, a data, o modo, o estado, a lixeira e os detalhes da
  transcrição (modelo, idioma, duração, confiança); o aprendido (variante, termo, restrição, nome do
  domínio); o **nome do computador**, a plataforma, a versão e um número aleatório da instalação. Do
  login Google: **e-mail, identificador da conta e nome**.
- **Nunca sai:** áudio; o que está em outras janelas (programa, título, texto de perto); a chave do
  Polish; logs; preferências; o mapa de domínios.

**Do celular (ditado no celular, ADR-021).** Só depois do aceite específico da pessoa, a cada ditado:

| Para | O quê |
|---|---|
| A nuvem (a VPS) | a **voz** de cada ditado, só em memória, durante o pedido, descartada ao fim; o **texto**, que vira ditado no histórico; a **chave** da pessoa, cifrada; o aceite e as preferências |
| O provedor de transcrição (OpenAI ou Groq, escolhido pela pessoa, com a chave dela) | a **voz**; com a **dica** ligada, os **termos do aprendido** (que podem ser nomes de clientes ou de terceiros) |
| O provedor do Polish (OpenAI ou Anthropic), se ligado | o texto do Smart, com 4 palavras ou mais |
| Os provedores, ao guardar ou trocar a chave | a própria chave, para o teste (a lista de modelos) |

- **Nunca sai do celular:** nada além do que está acima. O app não grava a voz em lugar nenhum. Não
  enxerga outras janelas. A chave **do PC** não vai (a pessoa cola outra no app, de propósito). O
  dicionário completo e o mapa de domínios ficam no PC.
- **A voz não é usada para identificar a pessoa.** A lei trata como sensível o "dado genético ou
  biométrico, quando vinculado a uma pessoa natural" (art. 5º, II); se a voz fosse usada para
  identificar, o tratamento pediria o art. 11. Nenhum código a compara com uma amostra nem a guarda.

## 2. Onde fica, e quem lê

| Lugar | O que tem | Quem lê |
|---|---|---|
| PC | tudo, em `historico.db` e `dicionario.json`, sem cifra própria | quem entrar na conta do Windows (no instalado, a pasta é só do dono, do SYSTEM e dos Administradores) |
| Servidor (Postgres na VPS) | ditados **legíveis**, aprendido, cadastro; sessões e aparelhos só como HMAC. **Ditado no celular:** a **chave da pessoa cifrada**, o aceite e as preferências | **quem administra a VPS lê o texto**; cifra ponta a ponta fica para depois |
| Servidor (memória da API) | **ditado no celular:** a **voz**, durante o pedido; o texto que seria gravado, 10 min, se a gravação falhar | quem administra a VPS (a voz passa pela memória do processo, e o arquivo da chave de cifra está no mesmo servidor) |
| Servidor de entrada (o proxy) | o endereço pedido e o IP, de passagem; **ditado no celular: a voz passa por ele** (termina o https) e ele pode segurar pedaços na memória dele | quem administra o proxy (o mesmo servidor, na instância de referência) |
| Provedor de transcrição | **ditado no celular:** a voz e, com a dica, os termos; o que ele retém, pela documentação dele (Anexo B) | o provedor, pelo contrato da conta da pessoa |
| Backup no B2 | o banco cifrado com `age` (inclui as chaves da pessoa, já cifradas pela chave de cifra do servidor) | só quem tem a chave privada (a do responsável); o B2 vê o arquivo, o nome com data e hora, o tamanho |
| Celular | nada do texto gravado (só na memória da página); a casca do app e o cookie. **Ditado no celular:** a voz, numa variável de memória, até o envio dar certo ou a pessoa descartar | a pessoa |

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
| **A voz do ditado no celular, na nuvem** | **não é guardada**: memória durante o pedido, descartada ao fim (o Go não zera a memória: "descartada", e não "apagada") |
| **A voz, no provedor** | o que a documentação dele diz (seção 4 e Anexo B): OpenAI, registros de abuso por até 30 dias (a tabela dela diz "nenhuma" para a transcrição, e o texto geral, até 30 dias: a política assume 30); Groq, nenhuma por padrão, até 30 dias só para falha ou abuso, e retenção zero se a conta ligar |
| **A chave da pessoa** | até a pessoa trocar, remover, desligar o ditado ou apagar os dados; **a cópia antiga, cifrada, fica nos backups (cerca de 30 dias) e no banco `aurora_antes_AAAAMMDD` de uma restauração (7 dias)** |
| **Aceite e preferências** | enquanto a conta existir, ou até o "Apagar meus dados" |
| **Desfecho pago e não gravado** | 10 min em memória, o reinício ou a mudança do epoch da conta |

## 4. Operadores e países

| Quem | Recebe | País / região |
|---|---|---|
| VPS | tudo o que está na nuvem, legível; no ditado do celular, a voz (memória) e as chaves (cifradas) | **[A PREENCHER: país e região do datacenter]** (no exemplo, Brasil). A empresa de hospedagem: **[A PREENCHER]** |
| O Traefik (a entrada do https, na mesma VPS) | o IP de quem acessa e o endereço pedido, de passagem; o registro de acesso dele fica **desligado** só para a nuvem. **No ditado do celular, a voz passa por ele** | o mesmo lugar da VPS |
| Google (login) | que a pessoa entrou; devolve e-mail, identificador e nome | a confirmar nas condições do Google |
| Backblaze B2 | só o arquivo cifrado | **[A PREENCHER: região do bucket]** (a da conta, escolhida na criação: `docs/backup-b2-f11.md`; pelo que se sabe, o B2 não tem região no Brasil) |
| **OpenAI** (ditado no celular, transcrição), se escolhida | a voz; com a dica, os termos | pela página lida em 10-out-2026: **EUA ou Europa, se a residência de dados do projeto da chave estiver fixada** (opção para contas elegíveis, com a Europa exigindo controles de retenção aprovados); **sem a residência, a página não diz o país**; o **Brasil não aparece**. Retenção: ver seção 3 |
| **Groq** (ditado no celular, transcrição), se escolhida | a voz; com a dica, os termos | pela documentação lida em 10-out-2026: o que ela **retém** fica em servidores nos **EUA**; a página **não diz onde a transcrição roda**; o contrato (DPA) prevê tratamento nos EUA e em outros países |
| OpenAI ou Anthropic (**Polish**, no PC e, com a ADR-021, no celular), opt-in, com a chave da pessoa | o texto final (4 palavras ou mais), até 30 dias lá. No PC, **fora da nuvem**; no celular, chamado pelo servidor | a confirmar |

**O texto legível fica no país da VPS** (no exemplo, o Brasil, com a VPS e a entrada do https no
mesmo lugar). O que sai do Brasil é o backup, **cifrado**, para a região do B2, o login, que passa
pelo Google, e, **no ditado do celular, a voz e, se a dica estiver ligada, os termos**. Para o backup há
transferência internacional (art. 5º, XV) de um arquivo que o B2 não consegue ler, e o art. 33 exige
uma das suas hipóteses (por exemplo, o inciso VIII: consentimento específico e em destaque, com aviso
prévio de que é internacional): a região e a hipótese **têm de estar preenchidas antes do primeiro dado
real**.

**A voz é uma segunda transferência internacional, de dado legível.** Nenhuma das duas documentações diz
processar no Brasil (a da OpenAI nem lista o Brasil; a da Groq põe o que retém nos EUA), então o dossiê
**trata o envio como transferência internacional** sem afirmar o país exato. A voz vai pelo https até o
provedor, mas o Aurora Whisper não a cifra de ponta a ponta, e o provedor a trata para transcrever. Isso
é diferente do backup, que o B2 não lê. **A hipótese proposta é o inciso VIII do art. 33** (consentimento
específico e em destaque, com informação prévia do caráter internacional, distinguindo-o de outras
finalidades): é o que a folha do aceite faz. Outras hipóteses do art. 33 (por exemplo, as cláusulas-padrão
do inciso II, b) existem, mas **a escolha é do responsável e fica em branco aqui**:
**[A PREENCHER: a hipótese do art. 33 para a voz e para a dica]**. **Somar uma segunda transferência
internacional, agora de dado legível, antes de dar base escrita à primeira (o backup), piora a situação:
por isso se recomenda fechar a L1 a L5 antes do OK do primeiro áudio.** O DNS fica só em modo DNS, sem
proxy de terceiro (ele leria o texto e a voz): o domínio da nuvem aponta direto para a VPS.

## 5. Direitos da pessoa (art. 18)

| Direito | Como, hoje |
|---|---|
| Confirmar e acessar (I, II) | o app da nuvem ou o painel do PC: lista, busca, aparelhos, sessões, aprendido. Pedido fora do app: declaração em até 15 dias (art. 19, II) |
| Corrigir (III) | editar o texto final |
| Eliminar (IV, VI) | lixeira; **Excluir definitivamente** (só de ditado já na lixeira); **Apagar meus dados da nuvem** (digitar "apagar": ditados, lápides, aprendido, aparelhos e outras sessões **e, no ditado do celular, o aceite, as preferências e as chaves**; os PCs ficam com a cópia; o backup guarda até girar) |
| Revogar (IX) | Desconectar (revoga o PC na nuvem), revogar um aparelho, encerrar uma sessão, Sair. **Ditado no celular:** "Desligar o ditado no celular" revoga o aceite e apaga as chaves (art. 8º, §5º: o tratamento já feito com o aceite fica ratificado enquanto não houver pedido de eliminação) |
| Cópia e portabilidade (V; art. 19, §3º) | o PC já tem tudo em `historico.db`. **Não há botão de exportar na nuvem** |
| Saber com quem compartilhamos (VII) | a seção 4 e a tabela da política, seção 6 (**no ditado do celular, inclui o provedor escolhido e o do Polish**). Eliminar o que o provedor guardou da voz não é com a nuvem: o pedido vai ao provedor, pela conta da pessoa |

## 6. Restauração e invasão

**Restaurar o backup** (decisão do projeto em 09-out-2026, ADR-019: "aceitar e deixar escrito"): o que foi
apagado de vez depois do último backup **pode voltar**, se ainda estiver nos 6 meses, e a pessoa o
apaga de novo; o que os 6 meses já venceram, a nuvem apaga antes de abrir a porta. O que só existia na
nuvem depois do backup pode se perder. Todas as sessões e todos os PCs são desligados, e cada PC
conecta de novo. Pode aparecer um "selo" (duas versões do mesmo ditado; a pessoa escolhe). **Ditado no
celular:** o aceite, as preferências e as chaves voltam como estavam no backup; restaurado **sem** a
mesma chave de cifra, cada chave vira `SEM_CHAVE`, a tela pede para colar de novo e o servidor não dá
erro `500`.

**Alcance de uma invasão.** Se a nuvem divide a VPS com outros serviços expostos à internet (como na
instância de referência, com outros sites atrás do mesmo Traefik), quem tomar um deles e chegar à raiz
do Docker alcança o volume do Postgres e os arquivos de segredo, e leva os ditados dos últimos 6 meses
e da lixeira, o aprendido, o e-mail, o identificador e o nome. Sem chegar à raiz, quem divide a rede do
proxy já alcança a porta da API direto (sem o Traefik): o login, as sessões e os tokens valem igual,
mas ele pode driblar o limite de tentativas por IP. Não leva áudio do PC, contexto de janela nem os backups: eles são
cifrados, e a chave do B2 que fica na VPS não tem `deleteFiles`; mas ela pode **esconder** os arquivos,
e a regra do bucket apaga o escondido, então quem os segura é o **Object Lock** do bucket (cada cópia
fica 30 dias, nem a regra a apaga antes). Sem o Object Lock, a chave apagaria os backups, só que com
um dia de atraso. Postgres sem porta e backup cifrado **não impedem isso**; uma VPS só da Aurora
(opção B da D1) reduz o alcance. Num incidente com risco relevante, o controlador avisa a ANPD e as
pessoas (art. 48); a cifra só atenua onde o dado está ilegível (art. 48, §3º), e o Postgres no ar não
está.

**Com o ditado no celular, o alcance cresce, e o texto precisa dizer isso com estas palavras:**
- **Quem toma a VPS ouve a voz em trânsito** (ela passa pela memória do processo da API e pelo proxy, e
  o responsável não tem como cifrá-la de ponta a ponta, porque o servidor precisa lê-la para repassar) e
  **decifra as chaves da pessoa**, porque o arquivo da chave de cifra e o banco moram no mesmo host. A
  cifra das chaves protege de um dump ou backup que vaze, de uma leitura indevida do banco e do log do
  Postgres. **Não protege de quem tem a raiz.** A saída num incidente é **revogar a chave no provedor**
  (a cobrança é da conta da pessoa).
- **As chaves antigas continuam cifradas** nos backups (cerca de 30 dias) e no banco da restauração
  (7 dias) depois de "Trocar" ou "Remover". Se a chave de cifra vazar depois, essas cópias se decifram.
  A tela manda a pessoa **revogar a antiga no provedor**.
- **Não é garantido que a voz nunca vá a disco** no que não é nosso: o proxy pode segurar pedaços do
  corpo na memória dele (e um middleware de buffering levaria um corpo acima de 1 MiB ao disco), e o
  Chrome pode paginar o `Blob` do `MediaRecorder` no aparelho. No contêiner da API, a promessa é
  "descartado ao fim", com `read_only`, `memswap_limit` igual ao `mem_limit` e `ulimits: core: 0`; a
  C7 confere a swap do contêiner (`memory.swap.max` em `0`) antes do primeiro áudio, e para se não der.

## 7. O que o responsável decide

1. **País e região.** A VPS: **[A PREENCHER]** (no exemplo, Brasil). O bucket do B2: a região da conta
   e a hipótese do art. 33 (seção 4). Bloqueia o primeiro dado real.
2. **O cadastro não sai.** O "Apagar meus dados" deixa o e-mail, o identificador e o nome (a linha em
   `users`); não existe "excluir a conta". Aceitar e escrever na política, ou pedir a rotina (um
   comando no roteiro, ou um botão). O cadastro é dado pessoal (art. 5º, I).
3. **Exportar.** Aceitar a falta do botão (o PC tem a cópia), ou pedir o botão.
4. **Texto de terceiros, e agora a voz e a dica.** O ditado pode ter dado de clientes ou, sem querer,
   dado sensível (art. 5º, II); a nuvem não separa nem filtra. Com dado de cliente, quem usa pode estar
   como operador dele (art. 5º, VII; art. 39), e a nuvem vira mais um lugar onde ele mora. Ditar isso na
   nuvem é decisão de quem usa. **Com o ditado no celular, o mesmo vale para a voz** (que leva também o
   que o microfone captar, como a voz de quem está perto) **e para a dica** (os termos do aprendido, que
   podem ser nomes de clientes, a cada ditado, mesmo nos que não falam deles): as duas vão a um
   provedor que não diz processar no Brasil. Se o dado é de um cliente, mandar a voz ou os termos a um
   terceiro no exterior pode ir além do que o cliente autorizou; o aceite da folha vale por quem dita, e não pelo
   cliente. **Na instância de referência, o Mauro confirmou em 10-out-2026 que vai ditar dado de
   cliente pelo celular às vezes** ("Sim, às vezes"), e por isso a política (seção 12) e a folha dizem
   que a voz e a dica podem levar dado de terceiro. Fica a cargo dele conferir o que deve a cada cliente
   antes de ditar, e decidir se liga a dica (que começa desligada).
5. **Contato de privacidade e encarregado** (art. 41; a dispensa depende de norma da ANPD, art. 41,
   §3º: o texto da lei, conferido no Planalto em 10-out-2026, diz que a autoridade nacional pode
   estabelecer normas complementares, inclusive hipóteses de dispensa; essas normas não foram
   conferidas aqui): [A PREENCHER: nome e e-mail].
6. **O selo na restauração** (pendente da F11d): aceitar e escrever (sugestão) ou consertar no PC numa
   fatia própria.
7. **Ditado no celular: a hipótese do art. 33 para a voz e para a dica** (seção 4). A proposta é o
   inciso VIII, que a folha do aceite cumpre. **[A PREENCHER]**
8. **Ditado no celular: o que a conta da pessoa tem no provedor.** O servidor **não confere** se a
   retenção zero está ligada nem se a residência de dados está fixada. A recomendação (na tela e na
   política) é uma **chave só para o celular, com limite de gasto**, e, se a conta tiver, a residência de
   dados e a retenção zero (na OpenAI, a retenção zero depende de aprovação dela, e a residência, da
   elegibilidade da conta, que a página manda perguntar ao comercial; na Groq, a retenção zero está ao
   alcance de todos os clientes, pela página lida). Na instância de referência, isso é com o Mauro, na C7.
9. **Ditado no celular: o OK antes do primeiro áudio real.** Ler a política e este dossiê reescritos
   e dar o OK no chat. Recomenda-se fechar antes a L1 a L5 (a região do bucket, a hipótese do art. 33 do
   backup, o contato e o encarregado, exportar e o cadastro).

Já aceitos no projeto (D3 da ADR-019) e descritos na política: o aprendido desfeito na nuvem e no
backup; as cópias do banco no PC; o Polish fora da nuvem (no PC). Na instância de referência, a revisão
foi aprovada em 10-out-2026 (ADR-019). **O ditado no celular não estava nela** (ADR-021).

---

# Anexo A: conferência no código (fora das duas páginas)

Cada número do dossiê, com o lugar onde se confere. `arquivo:linha` é do commit em que este anexo foi
escrito. **As linhas do ditado no celular (ADR-021) ainda não têm código**: apontam para a seção da ADR e
para o critério de aceitação (AC) que a prova; quando a subfatia (C1 a C7) existir, o `arquivo:linha`
entra aqui.

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
| **Ditado no celular:** a rota só aceita a sessão; o token de um PC dá `403`; sem o arquivo da chave de cifra, `404` | ADR-021, decisões A e O; AC-065 (a conferir quando existir a C5b) |
| **Ditado no celular:** o aceite específico, gravado por conta, antes do microfone, da chave e do provedor | ADR-021, decisão H e "O aceite e as preferências"; AC-066 (C3b, C5b, C6b) |
| **Ditado no celular:** a chave cifrada com AES-256-GCM e HKDF; o app só vê o fim; quem tem a raiz decifra | ADR-021, decisão G e "As chaves"; AC-067 (C3a); `09`, "A chave da pessoa na nuvem" |
| **Ditado no celular:** a voz só em memória, nunca em disco, swap, log ou resposta; o `Blob` do Chrome não verificado | ADR-021, decisão L e "Memória, disco e log"; AC-068 (C5b, C5c, C6a e C7); `09`, "A voz é efêmera" |
| **Ditado no celular:** a dica desligada por padrão, de 140 runas, sem par preso a domínio | ADR-021, decisão E; AC-075 (C2 e C8) |
| **Ditado no celular:** os provedores, o que dizem sobre retenção, treino e país | Anexo B, lido em 10-out-2026 |
| **Ditado no celular:** dado de cliente (voz e dica), confirmado pelo Mauro | ADR-021, cabeçalho ("Sim, às vezes"); seção 7, item 4 |

## A pré-condição do `09`, item a item

| Item pedido pelo `09` ("LGPD", pré-condição do deploy) | Resposta | Onde |
|---|---|---|
| Base legal e transparência (texto da tela e política) | consentimento (proposta); a política como modelo, com os campos de cada instância; o texto novo do app está na política e no app desde 11-out-2026 (C6b). **Ditado no celular:** um consentimento à parte, na folha do aceite | "Quem é quem"; `docs/politica-de-privacidade.md`; `web/src/nuvem/ConfiguracoesNuvem.tsx:448-456` |
| Retenção na nuvem, backup (o giro) e cópias no PC | seção 3 | acima |
| Direitos: excluir definitivamente, "apagar meus dados", acesso | seção 5; duas lacunas (cadastro, exportar) | seção 7, pontos 2 e 3 |
| O aprendido, no servidor e no espelho do PC, inclusive a linha desfeita | seção 3; aceito na D3 (ADR-019) | `09`, "Retenção" |
| País e região | a VPS (no exemplo, Brasil) e o bucket do B2 **em branco, marcados**. **Ditado no celular:** a voz e a dica vão a provedores que não dizem processar no Brasil; a hipótese do art. 33 fica **em branco, marcada** | seção 4 |
| Dados de terceiros | a nuvem não separa; decisão de quem usa. **Ditado no celular:** vale para a voz e a dica; o Mauro confirmou que dita dado de cliente às vezes | seção 7, ponto 4 |
| Quem administra o servidor e lê o texto | quem administra a VPS lê; alcance de invasão. **Ditado no celular:** ouve a voz em trânsito e decifra as chaves | seções 2 e 6 |

## O que a conferência achou e o `09` não dizia

- O **nome** da conta Google é guardado (`users.display_name`); o `09` só falava em e-mail e `google_sub`
  (corrigido no `09`).
- O "Apagar meus dados" **não apaga o cadastro** (seção 7, ponto 2).
- Não há como **exportar** os dados da nuvem (seção 7, ponto 3).
- O "Excluir definitivamente" só vale para um ditado **que já está na lixeira**.
- O **nome do computador** vai para a nuvem e a linha do aparelho fica no banco mesmo depois de
  revogado (a lista de aparelhos deixa de mostrá-la); só o "Apagar meus dados" a tira.
- **Releitura das páginas dos provedores (10-out-2026, D0 da ADR-021), que corrige o levantamento da
  ADR-021 na mesma data:**
  - **OpenAI:** a ADR dizia que "o Brasil aparece só como armazenamento". A página de dados da API,
    relida, **não lista o Brasil em lugar nenhum** (nem armazenamento): as regiões são EUA, Europa,
    Austrália, Canadá, Japão, Índia, Singapura, Coreia do Sul, Reino Unido e Emirados Árabes Unidos.
  - **Groq:** a ADR dizia que "a política de privacidade, vista só pela busca, diz que não treina". A
    política (em vigor desde 12-nov-2025), lida na página, **não fala em treino com dados de clientes**:
    remete esses dados ao contrato de serviço e ao DPA, e o DPA (15-out-2025) limita o uso a prestar o
    serviço, mas **não menciona treino**. A página "Your Data" também não fala. Por isso a política e a
    folha **não afirmam** "a Groq não treina": dizem que as páginas lidas não falam em treino.
  - **Groq, retenção zero:** a ADR dizia que "o servidor não tem como conferir" se está ligada; continua
    valendo. A página diz que **todos os clientes** podem ligá-la em Data Controls (a OpenAI exige
    aprovação).
  - **Nenhuma das duas páginas diz o país onde a transcrição roda sem configuração**; nenhuma nomeia o
    português: a da OpenAI diz que o Whisper suporta 98 idiomas e que a precisão varia por idioma; a da
    Groq chama os modelos de multilíngues.

---

# Anexo B: o que as páginas dos provedores dizem (lido em 10-out-2026)

Lido em **10-out-2026**, no endereço público de cada página. O que segue é **resumo com palavras
nossas**, e "não diz" quer dizer que a página lida não afirma aquilo, o que **não** significa que seja
falso. Preço, limite e modelo mudam; nada disto vira número no código. O Aurora Whisper não consegue
conferir a conta da pessoa em nenhum dos dois.

**B1. OpenAI, "Data controls in the OpenAI platform"**
`https://developers.openai.com/api/docs/guides/your-data` (o endereço antigo,
`platform.openai.com/docs/guides/your-data`, redireciona para ele). A página não mostra data de
atualização.
- **Diz:**
  - Desde 1º-mar-2023, os dados enviados à API não são usados para treinar nem melhorar os modelos, a
    menos que o cliente opte por compartilhá-los.
  - Há dois tipos de dado guardado: os registros de monitoramento de abuso e o estado de aplicação.
  - Por padrão, os registros de abuso são gerados para todo uso da API e **mantidos por até 30 dias**,
    a menos que mais tempo seja exigido por lei ou seja razoavelmente necessário para proteger o serviço
    ou terceiros; eles **podem conter conteúdo do cliente**, como prompts e respostas.
  - Clientes elegíveis podem excluir o conteúdo desses registros com a retenção zero (ZDR) ou o
    monitoramento modificado; hoje essas opções dependem de **aprovação prévia da OpenAI**.
  - A tabela de endpoints, para `/v1/audio/transcriptions`: não usado para treino; retenção de abuso
    **"nenhuma"**; estado de aplicação **"nenhum"**; elegível à retenção zero. O texto geral, porém, fala
    em até 30 dias para todo uso da API. **A política assume até 30 dias.**
  - A residência de dados é uma opção de **projeto**; a página manda falar com o comercial para saber da
    elegibilidade, e diz que, quando a residência está habilitada na conta, se fixa uma região para os
    projetos novos. Numa região que oferece processamento regional (EUA e Europa), a inferência roda lá;
    a transcrição de áudio está nas duas. A Europa exige retenção zero ou monitoramento modificado;
    qualquer região fora dos EUA exige aprovação dos controles de retenção e um aditivo. Austrália, Canadá, Japão, Índia, Singapura, Coreia do Sul e Reino Unido são só de
    armazenamento. Se a região escolhida não tem processamento regional, a OpenAI pode processar e guardar
    temporariamente fora dela. Dados do sistema (conta, metadados, uso) não são cobertos.
- **Não diz:** em que país a voz é processada **quando o projeto não tem a residência fixada**; que toda
  conta possa habilitar a residência ou a retenção zero; nada sobre o **Brasil**; a lista de
  subprocessadores nem os termos do contrato (DPA).

**B2. OpenAI, "File transcription"**
`https://developers.openai.com/api/docs/guides/speech-to-text`. Sem data de atualização.
- **Diz:** os formatos aceitos incluem `webm`; arquivos de até 25 MB; recomenda o `gpt-transcribe`, e os
  modelos `gpt-4o-transcribe` e `gpt-4o-mini-transcribe` aceitam `prompt`; as `keywords` são dicas, não
  saída obrigatória, e convém avaliar se melhoram a precisão sem fazer aparecer termos que ninguém disse;
  o Whisper suporta 98 idiomas, com precisão que varia por idioma.
- **Não diz:** nada sobre retenção ou privacidade do áudio (isso está no B1); **não nomeia o português**.

**B3. Groq, "Your Data in GroqCloud"**
`https://console.groq.com/docs/your-data`. Sem data de atualização.
- **Diz:** coleta metadados de uso de todos os usuários (sem entradas nem saídas); **por padrão não
  retém os dados de pedidos de inferência**; só retém os dados do cliente em dois casos (recursos que
  exigem estado, como lote e ajuste fino, e proteção da confiabilidade); pode **registrar
  temporariamente entradas e saídas** só para depurar erros que degradam a plataforma ou investigar abuso
  suspeito, e esses registros ficam **por até 30 dias**, salvo exigência legal; a tabela de retenção lista
  `/openai/v1/audio/transcriptions` na inferência, com o tipo de retenção de confiabilidade e
  monitoramento de abuso, por até 30 dias, e elegível à retenção zero; **todos os clientes** podem ligar a retenção zero (ZDR) em Data Controls, e
  então a Groq não retém dados nem para confiabilidade e abuso; **todos os dados retidos ficam em
  buckets do Google Cloud nos EUA**; quando aplicável, os clientes podem se apoiar em cláusulas-padrão
  contratuais para transferências entre terceiros países e os EUA.
- **Não diz:** onde a inferência (a transcrição) roda; se treina com dados de clientes.

**B4. Groq, DPA (Data Processing Addendum for GroqCloud Services)**
`https://console.groq.com/docs/legal/customer-data-processing-addendum`. Em vigor desde
**15-out-2025**.
- **Diz:** a Groq só trata os dados pessoais para prestar, manter e dar suporte ao serviço e cumprir o
  contrato, em nome do cliente e conforme as instruções documentadas dele, ou quando a lei exigir; não
  "vende" nem "compartilha" os dados; mantém uma lista de subprocessadores (endereço
  `trust.groq.com/subprocessors`) e avisa mudanças com pelo menos 15 dias; **pode transferir e tratar os
  dados nos EUA e em outros países onde ela ou seus subprocessadores operam**; avisa um incidente em até
  72 horas; o cliente pode pedir a exclusão dos dados pessoais, em até 180 dias, ressalvados a lei e o
  que está em backup.
- **Não diz:** nada sobre treino; nada sobre o **Brasil** nem sobre a LGPD (cita as leis da Califórnia,
  da UE, da Suíça, do Reino Unido e da Arábia Saudita); a lista de subprocessadores **não carregou como texto**
  ao ser lida (a página só mostrou o título), então **não foi lida**.

**B5. Groq, política de privacidade**
`https://groq.com/privacy-policy`. Em vigor desde **12-nov-2025**.
- **Diz:** a política **não se aplica** ao tratamento de dados de clientes dos serviços de nuvem, que é
  regido pelo contrato de serviço e pelo DPA; a Groq fica nos EUA, "mantém operações de tratamento em
  várias jurisdições" e pode transferir informações para outros países.
- **Não diz:** nada sobre treino com dados de clientes; períodos de retenção desses dados; retenção zero;
  locais específicos de tratamento.

**B6. Groq, "Speech to Text"**
`https://console.groq.com/docs/speech-to-text`. Sem data de atualização.
- **Diz:** os tipos de arquivo incluem `webm` e `ogg`; tamanho máximo de 25 MB no plano gratuito e 100 MB
  no de desenvolvedor; **mínimo cobrado de 10 s** por pedido; `prompt` de até 224 tokens; os modelos
  `whisper-large-v3-turbo` e `whisper-large-v3` são multilíngues; o `verbose_json` devolve segmentos com
  `no_speech_prob`.
- **Não diz:** nada sobre retenção (remete à página B3); **não nomeia o português**.

**B7. Não lidas (a página recusou a leitura ou não carregou):** a página de privacidade para empresas da
OpenAI (`openai.com/enterprise-privacy`), a de dados de negócio (`openai.com/business-data`), a de
privacidade da API (`openai.com/api-data-privacy`), o DPA e a lista de subprocessadores da OpenAI
(`openai.com/policies`), e a lista de subprocessadores da Groq. Os **contratos** (os termos de serviço e
os DPAs) das duas não foram lidos por inteiro, e a **Anthropic** (Polish) não foi relida nesta data. São
pendências do responsável (seção 7, itens 8 e 9).

---

# Anexo C: os artigos da LGPD citados, conferidos (10-out-2026)

Conferidos **um a um** no texto compilado da Lei nº 13.709, de 14 de agosto de 2018, em
`https://www.planalto.gov.br/ccivil_03/_ato2015-2018/2018/lei/l13709compilado.htm`, lido em
**10-out-2026** (o compilado traz as redações das Leis 13.853/2019 e 15.352/2026). A coluna do meio
resume o dispositivo **com palavras nossas**, só no que o texto usa.

| Artigo | O que diz (resumo) | Onde o dossiê, a política ou o `09` o usam |
|---|---|---|
| 4º, I | a lei não se aplica ao tratamento por pessoa natural para fins exclusivamente particulares e não econômicos | quem é quem |
| 5º, I | dado pessoal: informação relacionada a pessoa natural identificada ou identificável | o cadastro (seção 7, ponto 2) |
| 5º, II | dado pessoal sensível: origem racial ou étnica, convicção religiosa, opinião política, filiação a sindicato ou a organização religiosa, filosófica ou política, saúde ou vida sexual, dado genético ou biométrico, quando vinculado a uma pessoa natural | dado sensível sem querer; a voz como biometria (seção 1) |
| 5º, V | titular: a pessoa natural a quem os dados se referem | quem dita |
| 5º, VI | controlador: quem decide sobre o tratamento | quem hospeda |
| 5º, VII | operador: quem trata em nome do controlador | a VPS, o B2 e os provedores; quem dita dado de cliente |
| 5º, XV | transferência internacional: de dados pessoais para país estrangeiro ou organismo internacional do qual o país seja membro | o backup e a voz |
| 7º, I | o consentimento do titular é uma hipótese de tratamento | a base proposta |
| 7º, §5º | o controlador que obteve o consentimento e precisa comunicar ou compartilhar dados com outros controladores deve obter consentimento específico para isso, salvo as dispensas da lei | a folha, por cautela (se o provedor for qualificado como outro controlador) |
| 8º, caput e §2º | o consentimento é por escrito ou por outro meio que mostre a vontade; cabe ao controlador provar que o obteve conforme a lei | o aceite gravado no servidor |
| 8º, §4º | o consentimento deve referir-se a finalidades determinadas, e as autorizações genéricas são nulas | a folha específica |
| 8º, §5º | revogável a qualquer momento, por procedimento gratuito e facilitado; ratificados os tratamentos feitos antes, enquanto não houver pedido de eliminação (art. 18, VI) | o Desconectar, o Apagar e o Desligar o ditado |
| 8º, §6º | se mudar a informação dos incisos I, II, III ou V do art. 9º (finalidade, forma e duração, identificação do controlador, uso compartilhado), o controlador informa o titular, com destaque, do teor das mudanças, e o titular pode revogar | a seção 13 da política |
| 9º, caput; §2º | o titular tem acesso facilitado às informações do tratamento (finalidade específica, forma e duração, controlador, contato, uso compartilhado, responsabilidades e direitos); se o consentimento é requerido e a finalidade muda de modo incompatível com o original, o controlador informa antes | o corpo da política; a seção 13 |
| 11, I | dado sensível: consentimento específico e destacado, para finalidades específicas | a folha, se a voz levar dado sensível |
| 18, I a IX; §1º | os direitos do titular (confirmação, acesso, correção, anonimização/bloqueio/eliminação, portabilidade, eliminação do que foi tratado com consentimento, informação sobre o uso compartilhado, informação sobre a possibilidade de não consentir, revogação); o direito de peticionar à autoridade nacional | seção 5; política, seção 8 |
| 19, II; §3º | declaração clara e completa em até 15 dias; cópia eletrônica integral quando o tratamento vier do consentimento ou de contrato | seção 5 |
| 33, II (b); VIII | a transferência internacional só é permitida nos casos da lei, entre eles garantias do controlador, como as cláusulas-padrão contratuais (II, b), e o consentimento específico e em destaque do titular, com informação prévia do caráter internacional, distinguindo-o de outras finalidades (VIII) | seção 4 |
| 39 | o operador trata conforme as instruções do controlador | dado de cliente |
| 41; §3º | o controlador indica o encarregado; a autoridade nacional pode estabelecer normas complementares, inclusive hipóteses de dispensa | seção 7, ponto 5 |
| 48; §3º | o controlador comunica a autoridade e o titular o incidente de segurança com risco ou dano relevante; na avaliação de gravidade conta a prova de medidas que tornem os dados ininteligíveis a terceiros | seção 6 |

**Conferido, e nada a corrigir:** os artigos que o dossiê e a política já citavam antes (4º, I; 5º, I,
II, VI, VII e XV; 7º, I; 8º, §§ 2º, 5º e 6º; 9º; 18; 19, II e §3º; 33, VIII; 39; 41 e §3º; 48 e §3º)
dizem o que o texto afirma. **Os três que a ADR-021 citou de memória** (art. 8º, §4º; art. 33, VIII;
art. 5º, II e art. 11) também foram conferidos acima.
