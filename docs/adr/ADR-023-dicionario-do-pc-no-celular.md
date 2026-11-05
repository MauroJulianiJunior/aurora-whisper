# ADR-023 — O dicionário do PC no celular: uma cópia que só vai do PC à nuvem

Status: **Accepted (05-nov-2026).** As decisões do Mauro estão em "As decisões do Mauro", como ele as
deu. O resto é desenho desta ADR, feito a partir da leitura do código em `6341269` (`main` depois da
proteção da nuvem). **Tudo o que não sai das decisões dele está marcado [derivada]**, com a linha "Para
quem usa" quando muda o que ele vê.

Data: 2026-11-05
Contexto: o ditado no celular (ADR-021) passa o texto pelo Smart montado só com o aprendido da conta
(os pares do botão Aprender). A conta do Mauro tinha **0 pares** na nuvem (12-out), então o celular
escreve, na prática, sem dicionário. O `dicionario.json` do PC dele tem 49 termos e 104 variantes
(6 KB), e nunca sai do PC (ADR-019, decisão 7). O pedido dele, em 03-nov, ao escolher a frente
seguinte: "Vamos de dicionário completo antes do app nativo do Android."

Emenda (quando aceita):
- **ADR-019, decisão 7** ("do dicionário, sincroniza só o aprendido"): continua valendo para o **sync**
  (nada desce aos PCs, e o arquivo continua a fonte da verdade). O que muda é o "o celular não precisa
  disso": a cópia sobe, de um PC só, para o celular ler. E o "Fora do escopo" ("O dicionário completo
  no banco").
- **ADR-021, decisão 4 do Mauro** ("o dicionário completo continua no PC") e o "Fora do escopo"
  ("O dicionário completo no servidor; subir as dicas 'adicionadas' do PC").
- **Spec:** `05-data-model.md` (a tabela nova no Postgres e a coluna nova no SQLite),
  `06-api-contract.md` (três rotas da nuvem, duas locais e o campo novo do `GET /dictation`),
  `08-ui-ux-spec.md` (a opção no PC e a linha no celular), `09-security-privacy.md` (o que vai à
  nuvem e ao provedor), `13-implementation-plan.md` e o dossiê da LGPD.
- **A política de privacidade publicada** (`docs/politica-de-privacidade.md`): hoje ela diz que o
  dicionário fica no PC. O texto novo vai ao Mauro para aprovar, e ele republica.

## As decisões do Mauro (05-nov-2026)

| # | Pergunta | Resposta |
|---|---|---|
| 1 | Como o dicionário chega à nuvem? | **Uma cópia, só do PC à nuvem.** O PC manda uma cópia sempre que o arquivo muda; o celular só lê; editar continua no PC. O arquivo continua sendo o dono, como na decisão 7 da ADR-019. |
| 2 | Como isso é ligado? | **Uma opção no painel do PC, desligada por padrão:** "Levar o dicionário para o celular". Quem não ligar não manda nada, e desligar apaga a cópia da nuvem. |

E três itens do desenho que foram mostrados a ele junto das perguntas ("Os itens 2 a 4 seguem como
propus", sem objeção):

- **No celular, o dicionário vale como no PC num programa fora dos domínios** (o navegador, por
  exemplo): os termos valem, e as variantes restritas a um domínio ficam de fora, porque o celular não
  sabe em que programa a pessoa está.
- **A dica do ASR fica igual à do PC:** os termos com dica primeiro, depois os outros, por
  prioridade, até 140 caracteres, atrás da opção "dica" que já existe no celular.
- **Um PC só é a fonte:** ligar a opção num PC passa a fonte para ele.

## Decisão

### A. A cópia: o arquivo, menos o que diz de programas e de amostras

A cópia é a `Semente` do `dicionario.json`, recortada por uma função pura, `Semente.ParaACopia()`
(`internal/dictionary/copia.go`), e serializada pelo `encoding/json` (os campos na ordem da struct, as
chaves dos mapas em ordem): **os mesmos termos dão os mesmos bytes**, e o SHA-256 dos bytes é a versão
da cópia.

| Campo do arquivo | Na cópia | Por quê |
|---|---|---|
| `usuario` | `"copia"`, sempre | o servidor troca pelo dono da conta ao montar; o nome local não sai do PC |
| `dominios` (domínio → executáveis) | **não vai** | diz quais programas a pessoa usa. O celular não tem programa em foco, então o mapa não serviria |
| `alias_origem` | só os valores `"painel"` e `"adicionado"` | os ids de amostra do `dictlearn` são da medição e não mudam o Smart |
| `entradas` | todas, inclusive as desligadas, com todos os campos | a desligada muda o que o aprendido faz (`termo_desligado`), como no PC |

**[derivada]** Manter as desligadas é o que dá a mesma conta do PC: um par aprendido de um termo
desligado não é aplicado no PC, e também não pode ser no celular.

**Os limites** (`dictionary.ValidarCopia`, a mesma função no PC, antes de mandar, e no servidor, ao
receber) **[derivada]**: no máximo **512 KiB**, **2.000 termos**, **50 variantes por termo** e
**10.000 variantes no total**. Cada termo e cada variante segue a regra de hoje (até 80 caracteres;
`painel.go`). O arquivo do Mauro usa 1,2% do tamanho e 2,5% dos termos. **Fora do limite, a cópia não
sobe**, e o painel diz o motivo, sem cortar o dicionário pela metade.

**O servidor confere a cópia como o PC confere o arquivo:** `CarregarSemente` (o JSON estrito, com
`DisallowUnknownFields`), `ValidarCopia` e a montagem do Smart (`SmartDaSemente`, que recusa termo sem
forma canônica). Uma cópia que não monta é `422`, e nada é gravado.

### B. A nuvem guarda uma cópia por conta, e sabe de que PC ela veio

A tabela nova, `dictionary_copies` (a migration `0027`; só `CREATE`, como manda o
`migrations/README.md`) **[derivada]**:

| Coluna | O quê |
|---|---|
| `user_id` | a conta, **PK**: uma cópia só por conta |
| `device_id` | o PC que é a fonte. Chave estrangeira `(user_id, device_id)` → `devices (user_id, id)` |
| `content` | os bytes da cópia, como `TEXT` (não `JSONB`: o `JSONB` reordena as chaves, e o SHA-256 deixaria de bater) |
| `sha256` | o SHA-256 dos bytes, em hexadecimal minúsculo, conferido contra o cabeçalho do PUT |
| `terms` | quantos termos, para a linha do celular |
| `updated_at` | quando a cópia mudou pela última vez |

As tabelas `dictionary_entries` e `dictionary_aliases` (a `0004` e a `0005`) **ficam como estão,
vazias**: elas eram o dicionário no banco, editável e por linha, que a decisão 1 descartou.

**A cópia sai:**
- quando a pessoa desliga a opção no PC que é a fonte (o `DELETE`);
- quando o PC que é a fonte é revogado ou desconectado (o `RevogarAparelho` apaga a cópia na mesma
  transação; a leitura do ditado também ignora a cópia de um aparelho revogado, por garantia);
- no "Apagar meus dados" (uma linha nova na lista `apagarDaConta`, antes dos aparelhos) e no
  `tirar-cadastro.sh`.

### C. Três rotas da nuvem, só com o token de um PC

Todas na família `/api/v1/sync/`, com `AcessoAparelho`, como as imagens das notas **[derivada]**. O
`/api/v1/dictionary` continua não montado (é o dicionário da spec antiga, que a decisão 1 descartou).

```
GET    /api/v1/sync/dictionary    o estado da fonte
PUT    /api/v1/sync/dictionary    sobe a cópia
DELETE /api/v1/sync/dictionary    apaga a cópia, se este PC for a fonte
```

**O estado** (o corpo do GET, e do 200 do PUT e do 409):

```json
{"fonte": "este", "aparelho": "PC-DO-MAURO", "termos": 49,
 "sha256": "…", "atualizado_em": "2026-11-05T14:02:00Z"}
```

`fonte` é `"este"` (a cópia é deste PC), `"outro"` (é de outro PC da conta; `aparelho` é o nome dele)
ou `"nenhuma"` (sem cópia; os outros campos ausentes).

**O PUT** leva o corpo da cópia (`Content-Type: application/json`) e dois cabeçalhos:

- `Dicionario-SHA256`: o SHA-256 dos bytes; se não bater, `400`.
- `Dicionario-Fonte`: `tomar` ou `manter`.
  - `tomar` é o clique em "Levar o dicionário para o celular": grava sempre, e este PC passa a ser a
    fonte, mesmo que outro fosse. `200` com o estado.
  - `manter` é o envio automático, quando o arquivo muda: só grava se este PC **já é** a fonte. Se a
    fonte é outro PC, ou se não há cópia (ela foi apagada no "Apagar meus dados", por exemplo), é
    `409` com `code: "nao_e_a_fonte"` e o estado, e nada é gravado. É o que impede dois PCs ligados de
    tomarem a fonte um do outro a cada mudança.
- O mesmo SHA-256 que já está gravado, do mesmo PC, é `200` sem gravar (o reenvio é de graça).
- A cópia inválida é `422` com `code: "dicionario_invalido"` e um motivo fechado
  (`json`, `grande_demais`, `termos_demais`, `variantes_demais`, `termo_invalido`). Nunca leva o termo.

**O DELETE** apaga só se a fonte é este PC: `204`. Se é outro PC, ou se não há cópia, também `204`, sem
apagar nada (ele é idempotente, e o PC só quer saber que a sua cópia não está lá).

**O log** do servidor leva o motivo, o tamanho em faixas e o número de termos, **nunca** um termo,
uma variante ou o nome do aparelho.

### D. O ditado no celular monta o Smart com a cópia

No `transcrever` (`internal/server/ditar_trabalho.go`), junto com o aprendido e dentro do mesmo prazo
do Smart (5 s), a cópia da conta é lida (só a de um aparelho não revogado).

- **A montagem** **[derivada]**: `smartconta.Montar(usuario, copia, pares)` começa da semente da
  cópia, com o `usuario` trocado pelo dono da conta, e aplica o aprendido por cima com o mesmo
  `AplicarPares` e as mesmas conferências do PC. Os pares presos a um domínio continuam de fora, como
  hoje. Sem cópia, é exatamente a montagem de hoje (a semente vazia). É a conta do PC: o arquivo, com
  o aprendido que veio da nuvem aplicado nele.
- **A resolução** continua sem domínio (`Domain` nil), que é o PC num programa fora do mapa: as
  entradas valem, de qualquer domínio, e as **variantes restritas** e as **entradas restritas** ficam
  de fora (`repository_memory.go:239-248`). No arquivo do Mauro, 0 entradas e 3 variantes são
  restritas. O casamento fonético continua precisando do contexto da janela, que o celular não tem;
  então, no celular, valem o exato e a variante, como no PC sem contexto.
- **O cache** (`smartconta.Cache`) passa a ter como versão o SHA-256 da cópia junto com o resumo dos
  pares (`versaoDosPares`): mudar a cópia, ou o aprendido, monta de novo.
- **Se a cópia não monta** (um erro que o PUT não pegou, ou o prazo), o ditado segue com a montagem
  de hoje, só com o aprendido, e o relatório do ditado anota `dicionario=falhou`. Nunca falha o
  ditado por causa da cópia.
- **A dica** (só com a opção "dica" ligada, decisão E da ADR-021): **com a cópia**, é a dica do PC,
  `dictionary.Vocabulario(semente.ParaEntradas(), 140)` sobre a semente final, a mesma função do
  Agent (`cmd/agent/main.go:1103`). Os termos com dica da pessoa vêm primeiro, e os termos do Aprender
  (só variante) ficam fora, como no PC. **Sem a cópia**, é a dica de hoje (`smartconta.Dica`, os termos
  aprendidos), para quem não ligou a opção não perder o que já tinha.
- **O Polish** confere os termos pelo mesmo Smart (`polish.Montagem{Smart: conta.Smart()}`): a cópia
  entra nas guardas sem mudança.

**O teste de paridade** (`smartconta/paridade_test.go`, hoje "a nuvem é um PC de dicionário vazio")
passa a provar o caso novo: **a mesma semente, montada pelo caminho do Agent e pelo da nuvem, dá o
mesmo texto** num conjunto de frases, com o domínio ausente.

### E. O estado no celular

O `GET /api/v1/dictation` ganha o campo `dicionario`:

```json
"dicionario": {"aparelho": "PC-DO-MAURO", "termos": 49, "atualizado_em": "2026-11-05T14:02:00Z"}
```

ou `null` sem cópia. **Para quem usa:** nas Configurações › Ditar pelo celular, uma linha discreta
abaixo da opção da dica:

```
Dicionário: o do PC-DO-MAURO, 49 termos (atualizado em 05/11 às 14:02).
```

ou, sem cópia:

```
Sem o dicionário do PC. Para usá-lo aqui, ligue "Levar o dicionário para o celular" no painel do PC.
```

A opção da dica passa de "Mandar os termos aprendidos como dica" a **"Mandar os termos do dicionário
como dica"** **[derivada]**: com a cópia, são os do dicionário; sem ela, os aprendidos, que também são
do dicionário.

### F. O PC: a opção, o envio e o aviso

**A opção** mora em `local_settings`, na coluna nova `dictionary_copy_enabled` (a migration `0016` do
SQLite; `0` por padrão). Ela é a **intenção** deste PC; quem é a fonte quem diz é a nuvem.

**Ligar** (o clique, `POST /api/nuvem/dicionario {"ligar": true}` na API local): o Agent monta a cópia
do arquivo de agora, confere com `ValidarCopia` e manda o `PUT … tomar` **na hora**, para o painel
mostrar o resultado sem esperar a rodada. Só com `200` a opção é gravada ligada. Sem rede, ou com a
cópia recusada, a opção continua desligada, e o painel diz por quê.

**Desligar** (`{"ligar": false}`): o `DELETE` na hora. Com `204`, a opção é gravada desligada. Sem
rede, a opção **continua ligada** e o painel diz "Não deu para apagar a cópia da nuvem agora. Tente
de novo." **[derivada]**: desligar sem apagar daria a impressão de que a cópia saiu.

**O envio automático** é uma fase nova da rodada do sync (`Motor.EnviarDicionario`,
`internal/sync/motor_dicionario.go`), **depois do pull e das imagens**, só com a opção ligada:

1. monta a cópia do arquivo de agora e o SHA-256;
2. se o SHA-256 é o último que a nuvem aceitou deste pareamento (guardado **em memória**; depois de
   reiniciar, o primeiro envio é um `200` sem gravar), não faz nada;
3. senão, `PUT … manter`:
   - `200`: guarda o SHA-256;
   - `409 nao_e_a_fonte`: **grava a opção desligada** e guarda o aviso para o painel: "O celular
     passou a usar o dicionário de NOTEBOOK." (fonte `outro`) ou "A cópia do dicionário foi apagada
     da nuvem." (fonte `nenhuma`);
   - `422`: guarda o aviso com o motivo, mantém a opção e só tenta de novo quando o arquivo mudar;
   - `404` (a nuvem é de antes desta versão): guarda o aviso "A nuvem ainda não recebe o dicionário."
     e não tenta de novo até o Agent reiniciar;
   - o `401`, o `429`, o redirecionamento e a rede seguem a regra das imagens: param a fase, e a
     rodada seguinte tenta de novo.

O arquivo que muda (o Aprender, o "Adicionar termo", uma edição à mão) é visto na rodada seguinte, sem
gatilho novo **[derivada]**: o Aprender já acorda o laço, e o resto espera a rodada periódica.

**Desconectar** o PC da nuvem grava a opção desligada (a revogação já apaga a cópia na nuvem):
reconectar **não** manda o dicionário sem um clique novo.

**Para quem usa:** nas Configurações › Nuvem do painel do PC, com a nuvem conectada, uma opção a
mais, no estilo das outras:

```
[ ] Levar o dicionário para o celular
    O celular passa a usar os termos deste PC. Editar continua aqui.
```

Ligada:

```
[x] Levar o dicionário para o celular
    49 termos, enviados às 14:02.
```

Com outro PC como fonte (o `GET /api/nuvem/dicionario`, que pergunta à nuvem quando a seção abre):

```
[ ] Levar o dicionário para o celular
    Hoje o celular usa o dicionário de NOTEBOOK. Ligar aqui passa a usar o deste PC.
```

As rotas locais **[derivada]**: `GET /api/nuvem/dicionario` (o estado da nuvem, mais a opção e o
aviso deste PC) e `POST /api/nuvem/dicionario` (`{"ligar": true|false}`), com a guarda de sempre da API
local.

### G. Privacidade

O que muda, e entra na política, no `09` e no dossiê:

- **Vai à nuvem, só com a opção ligada:** os termos e as variantes do dicionário, as prioridades, as
  restrições e o **nome** dos domínios. Os termos podem ser nomes de clientes ou de terceiros.
- **Continua sem sair do PC:** a lista de programas de cada domínio, os ids de amostra e o nome do
  usuário local.
- **Vai ao provedor da transcrição, só com a dica ligada:** até 140 caracteres de termos do dicionário,
  como já ia com os aprendidos.
- **Guarda:** enquanto a opção estiver ligada no PC que é a fonte. Sai ao desligar, ao desconectar ou
  revogar o PC e no "Apagar meus dados". Os backups (cifrados) a guardam pelo prazo deles, como o
  resto.

## Fases

| Fase | O quê |
|---|---|
| **D1** | A cópia (`ParaACopia`, `ValidarCopia`), a tabela `0027`, o store, as três rotas, a revogação e o "Apagar meus dados", com os testes de contrato, de acesso e do Postgres. |
| **D2** | O ditado usa a cópia: a montagem, o cache, a dica, o campo `dicionario` do `GET /dictation`, a paridade. |
| **D3** | O PC: a coluna `0016`, o cliente, a fase do motor, ligar e desligar, as rotas locais. |
| **D4** | O painel: a opção no PC e a linha no celular. |
| **D5** | Os documentos: a política (para o Mauro aprovar), o dossiê, a spec e o roteiro de teste manual. |

## Testes

- **A cópia:** os mesmos termos dão os mesmos bytes; `dominios`, ids de amostra e o `usuario` local
  não saem; cada limite recusa no número exato e aceita um abaixo.
- **A paridade:** a mesma semente, pelo Agent e pela nuvem, dá o mesmo texto, com e sem aprendido por
  cima.
- **As rotas:** `tomar` e `manter` em cada estado da fonte (nenhuma, este, outro); o SHA-256 errado; a
  cópia inválida em cada motivo; o mesmo SHA-256 sem gravar; o `DELETE` de quem não é a fonte; a
  sessão do navegador é `403` nas três.
- **O Postgres:** uma cópia só por conta; a revogação apaga; o "Apagar meus dados" apaga (o teste que
  cobre toda tabela com `user_id` já pega a falta); a leitura ignora o aparelho revogado.
- **O motor:** cada resposta do PUT leva à ação da tabela F; o mesmo SHA-256 não sobe de novo; o `409`
  desliga a opção.
- **O ditado:** com a cópia, um termo do dicionário corrige o texto e entra na dica; sem a cópia, tudo
  como hoje; a cópia que não monta não derruba o ditado.
- **O roteiro manual** (`docs/teste-manual-dicionario.md`): ligar no PC, ditar no celular um termo do
  dicionário, mudar o arquivo e ver a linha do celular mudar, desligar e ver a linha "Sem o dicionário".

## Alternativas descartadas

- **O dicionário no banco, editável no celular e sincronizado entre os PCs.** Várias vezes maior, e o
  `dicionario.json` deixaria de ser a fonte que a pessoa confere (decisão 1 do Mauro).
- **Mandar automaticamente ao conectar.** Dado novo saindo do PC sem pergunta; o projeto é aberto, e
  outros usuários escolhem (decisão 2 do Mauro).
- **Juntar os dicionários de todos os PCs.** Exige uma regra de conflito entre arquivos que ninguém
  edita juntos, e o resultado não seria o de nenhum PC.
- **A cópia como entidade do sync (push e pull).** Nada desce aos PCs, e a cópia é um documento, não
  linhas com versão: três rotas próprias são menores e não mexem no `CHECK` fechado do
  `sync_events`.
- **Um seletor de domínio no celular.** Mais tela por 3 variantes restritas; fica para quando fizer
  falta.
- **Guardar o SHA-256 enviado no SQLite.** Economiza um PUT sem gravar por reinício do Agent, de uns
  6 KB; não vale a coluna.

## Riscos, e o que não foi verificado

1. **A cópia atrasa até a próxima rodada** depois de uma edição à mão ou de um "Adicionar termo". O
   Aprender acorda o laço; o resto espera a rodada periódica.
2. **Dois PCs com a opção ligada:** o que perdeu a fonte só fica sabendo no próximo envio dele (o
   `409`), ou quando a seção Nuvem do painel abre.
3. **O tempo de montagem** com o dicionário no limite (2.000 termos) não foi medido no servidor. O
   teste mede a montagem do limite e falha acima de 2 s; o do Mauro tem 49.
4. **A política muda depois do código:** o deploy espera a aprovação do texto novo, porque a política
   publicada diria que o dicionário fica no PC.
