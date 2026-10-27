# ADR-022 — Notas: o ditado vira anotação útil, com lista, marcador e cor, no PC e no celular

Status: **Accepted (25-out-2026).** As decisões do Mauro estão em "As decisões do Mauro", como ele
as deu. O resto é desenho desta ADR, feito a partir da leitura do código em `ca5c6b7`
(`main` depois do relatório do Polish de 25-out). **Tudo o que não sai das decisões dele está marcado
[derivada]**, com o padrão proposto e a alternativa, e uma linha "Para quem usa" quando muda o que ele
vê.
- **A aprovação, do Mauro em 25-out-2026:** E1 no padrão ("Gatilho próprio"); **E4 trocada:
  marcar sem rede** ("Marcar sem rede"), a fila offline entra na N8; as outras escolhas menores
  com os padrões ("Aprovo com os padrões").

Data: 2026-10-25
Contexto: depois do aceite por linha (ADR-016, sétima emenda) e da auditoria de grafia do
`polishbench`. O pedido do Mauro é do mesmo dia: "transformar ditados em anotações úteis que eu
posso recuperar em outra aba", "integrar funcionalidades do Keep na Whisper e manter isso como uma
memória", e "deixar de usar o Keep pra coisas que eu posso fazer na minha aplicação".

Emenda (quando aceita):
- **ADR-016 (Polish):** um uso novo, a conversão de trecho em lista ou lista de marcar, com prompt
  próprio e as mesmas guardas de palavra; e a frente L, listas melhores no ditado.
- **ADR-019 (nuvem e celular):** duas entidades novas no sync (`nota` e `marcador`) e o "Fora do
  escopo" (as notas deixam de ser só do PC).
- **ADR-013 (gatilho):** um segundo gatilho, "ditar para nota".
- **Spec:** `02-prd.md` (requisitos novos), `03-acceptance-criteria.md` (ACs novos),
  `05-data-model.md` (tabelas novas no SQLite e no Postgres), `06-api-contract.md` (rotas locais e da
  nuvem), `07-flows.md`, `08-ui-ux-spec.md` (a aba Notas), `09-security-privacy.md` (o texto da nota
  no servidor; a imagem, na fase N10), `13-implementation-plan.md` e `20-traceability.md`.

## O problema

Hoje cada ditado é inserido no campo em foco e fica no histórico, uma lista cronológica com busca,
edição e lixeira (`web/src/Painel.tsx`, `local_transcriptions` em
`internal/storage/migrations/0001_init_schema.sql:21`). Não há como guardar um ditado como coisa à
parte (uma lista de compras, uma ideia, um roteiro), organizá-lo e voltar a ele. Para isso o Mauro
usa o Google Keep.

Duas coisas faltam:

1. **A lista no ditado não sai bem.** O Polish já organiza enumeração em `- ` (ADR-016, regra de
   formato), mas as guardas recusam muitas respostas com lista: no diagnóstico de 25-out-2026 (26
   ditados x 3 rodadas), as recusas por palavra mais comuns foram "palavra acrescentada" (o "e" entre
   itens) e "palavra trocada".
2. **Não existe a nota.** Nem no banco, nem no sync, nem no painel.

## As decisões do Mauro (25-out-2026)

1. **As notas vivem no PC e no celular**, pelo sync da ADR-019. O texto da nota fica legível no
   servidor, como o dos ditados.
2. **Converter um trecho em lista é com IA, com guarda.** O modelo do Polish, com a chave da pessoa,
   separa os itens; a guarda garante que nenhuma palavra foi inventada ou trocada; se ela recusar, a
   conversão simples (por regra) assume.
3. **O ditado que vira nota sai do histórico.** A nota guarda de qual ditado veio.
4. **Marcadores e cor, como no Keep.** Uma nota pode ter vários marcadores e uma cor; a lateral
   filtra por marcador.
5. **Entram também as ideias "melhor que o Keep"** ("Podemos implementar todas elas também"):
   acrescentar por voz sem abrir a nota; item marcado desce e "desmarcar tudo"; copiar como texto de
   WhatsApp; busca única nas notas e no histórico; sugestão de marcador.
6. **Fora:** colaboradores ("Única coisa que eu não quero é adicionar colaboradores às notas hoje").
   Também fora desta ADR, por proposta aceita no mesmo dia: lembrete com horário e desenho à mão.

## Decisão

### A nota

**[derivada] O corpo é texto, uma linha por bloco, com um prefixo que diz o tipo.**

| Linha | Tipo |
|---|---|
| `# Título de seção` | título |
| `- item` | marcador (bullet) |
| `1. item` | lista numerada |
| `- [ ] item` / `- [x] item` | lista de marcar |
| qualquer outra | parágrafo |

- Por quê: é o mesmo formato que o Polish já produz (`- `), cabe numa coluna `TEXT`, a busca é
  `LIKE`/FTS sem conversão, o sync manda uma string, o diff do conflito é o mesmo do histórico
  (`web/src/diff.ts`), e copiar para outro app sai legível sem exportador.
- Alternativa: blocos em JSON (como o Keep e o Notion). Mais flexível para imagem no meio do texto e
  formatação dentro da linha, mas cada leitor precisa do parser, e o conflito vira merge de árvore.
  Fica descartada; a imagem tem lugar próprio (fase N10), fora do corpo.
- Sem negrito, itálico e link dentro da linha nesta ADR (o ícone "A" do Keep). Ver "Escolhas
  menores", E3.

**Campos da nota [derivada]:** `id` (UUID v7, como o ditado), `user_id`, `titulo` (opcional),
`corpo`, `cor` (uma de 8 nomes fixos, ou nenhuma), `fixada`, `arquivada`, `marcadores` (ids),
`ditado_de_origem` (opcional), `created_at`, `updated_at`, `deleted_at`, `purge_at`, `version` e as
colunas de sync que o ditado já tem (`sync_state`, `server_version`, `last_sync_error`).

- A cor é um **nome** (`amarelo`, `verde`...), não um hex: cada tema (claro e escuro) tem o seu tom,
  como no Keep, e o painel continua discreto (`PRODUCT.md`).
- Lixeira de 30 dias, com o mesmo expurgo dos ditados (`internal/agent/expurgo`). Arquivada não é
  lixeira: sai da tela principal, aparece em "Arquivo", nunca expira.
- **A nota não tem a retenção de 6 meses** do histórico (ADR-019, decisão da F10). Nota é coisa que a
  pessoa guardou de propósito. Para quem usa: a nota fica até ele apagar.

**O marcador [derivada]** é entidade própria (`id`, `user_id`, `nome`, `created_at`, `updated_at`,
`deleted_at`, `version`), para existir sem nota, ser renomeado num lugar só e sincronizar. Nome
único por pessoa, sem diferenciar maiúscula. Apagar o marcador tira ele das notas, não apaga nota.

### Ditado vira nota

- No histórico, cada ditado ganha a ação **"Virar nota"**. A nota nasce com o `final_text` no corpo,
  sem título, e com `ditado_de_origem`.
- **[derivada] O ditado sai do histórico sem ir para a lixeira.** Ganha a coluna `virou_nota`
  (o id da nota), e a lista do histórico o esconde. Por quê: na lixeira, ele seria expurgado em 30
  dias e apareceria como "apagado", o que ele não foi; e o "Aprender" e a auditoria continuam
  achando o `raw_text`. O aviso da ação tem **Desfazer** (devolve o ditado e apaga a nota nova, se
  ela não foi editada). Alternativa: mandar para a lixeira (mais simples, sem coluna nova).
- Apagar a nota depois não devolve o ditado ao histórico.
- A retenção de 6 meses do histórico não alcança o ditado que virou nota (o texto vive na nota).
- **Feito na N3 (25-out-2026):** rota `POST /api/history/{id}/virar-nota` e `POST
  /api/notas/{id}/desfazer-virar`; a coluna `virou_nota` (migration 0010), sem chave estrangeira; a lista e
  a busca do histórico (e a lixeira) escondem o ditado marcado, o expurgo de 30 dias e os 6 meses o
  pulam; o Desfazer apaga a nota só na versão 1, fora da lixeira. Nada vai para a fila do sync (até a N7; ver Decisões da N7: a marca passou a subir).

**Ditado feito dentro da nota.** O ditado feito com o cursor no editor de uma nota **não aparece no
histórico**, como se fosse um "Virar nota" automático (pedido do Mauro, 25-out-2026; o texto já está
na nota, e o histórico fica só com o que não tem outro lugar). O Agent não vê o painel, então o painel
o avisa: enquanto o título ou uma linha da nota tem o foco e a janela do navegador também, `PUT
/api/notas/foco` (renovado a cada ~20 s) com `{nota}` ou, no "Criar uma nota", `{token}`; ao sair,
`DELETE`. O Agent guarda o aviso **só na memória**, com validade de 60 s (é contexto: nada em disco, log
nem sync), e, ao gravar um ditado que deu certo, escreve `virou_nota` com o id da nota ou `nova:<token>`.
A inserção no campo não muda. Reusa a coluna e as regras do "Virar nota" (fora do histórico sem
lixeira, fora dos 6 meses, sem evento de sync até a N7; ver Decisões da N7); não define `ditado_de_origem`. No "Criar uma nota", o
`POST /api/notas` leva o `token` e liga os ditados à nota na mesma transação; a caixa que fecha vazia
manda `descartar`, e os ditados voltam ao histórico (nada some calado); o resto volta em 24 h. Só
**ditados novos** são escondidos: os que já estão no histórico não mudam. Alternativa descartada: o
Agent descobrir o campo em foco pela UIA (contexto de outro app, mais frágil, e o painel já sabe).
Risco aceito: um ditado feito no instante em que o foco sai da nota (ou com o aviso vencido por um
painel travado) aparece no histórico, que é o comportamento de sempre.

### Ditar dentro da nota

O editor da nota é um campo de texto do painel. O Agent já insere no campo com foco (ADR-011), então
**ditar dentro da nota funciona sem código novo na inserção**: abrir a nota, segurar `Ctrl+Win`, falar
(o que mudou é só o histórico: ver "Ditado feito dentro da nota", acima).
O Polish (se ligado) entrega a lista já com `- `. Só a verificação manual confirma (o painel no
navegador é um campo comum para o Agent; o caso a conferir é o editor por linhas, ver "Riscos").

### Acrescentar por voz sem abrir a nota

**[derivada] Um segundo gatilho, "ditar para nota"**, e não uma frase-comando no ditado comum.

- Por quê: frase-comando ("na lista de compras: ...") confunde com fala comum ("na lista de compras
  faltou o leite", ditado num e-mail) e mandaria o texto para o lugar errado sem a pessoa perceber.
  Um gatilho próprio deixa a intenção explícita.
- Padrão: `Ctrl+Win+Alt` segurados (configurável, como o gatilho de hoje, ADR-013). O texto **não é
  inserido** no campo em foco; vai para uma nota:
  - se o ditado começa com o nome de uma nota existente ("lista de compras: leite, pão e café"), o
    resto entra no fim dela. O casamento do nome é exato, sem caixa e sem acento, pelo título da
    nota; se dois títulos casam, vence a nota editada por último;
  - se a nota de destino tem lista de marcar, os itens entram como `- [ ]`;
  - se nenhum nome casa, nasce uma nota nova com o ditado inteiro (sem tirar nada).
- A pílula mostra para onde foi: "Lista de compras +3", ou "Nota nova".
- Alternativa: frase-comando, sem gatilho novo; mais natural, mais erro. Ver E1.
- No celular, o equivalente é um botão "Ditar para nota" (fase N8, com a mesma regra do nome).

**Decisões da N9 (26-out-2026), na implementação.** As marcadas **[desvio]** mudam algo escrito acima.
- **Qual gatilho, pelo pico.** Ctrl+Win+Alt contém o Ctrl+Win, e o hook já arma o ditado com ele. A sessão
  é uma só, com as decisões de suprimir e notificar do Ctrl+Win, e leva a marca "para nota" se Ctrl, Win e
  Alt estiveram seguros juntos em algum momento entre armar e soltar (`dictation.Gatilhos`; emenda da
  ADR-013). A ordem de apertar e de soltar não importa, e o Alt apertado no meio da gravação conta.
  Alternativa: decidir só no aperto (o Alt atrasado em 50 ms mandaria o texto para o campo, sem aviso).
- **O mesmo ditado até o fim da resolução**: limiar, bipe, pílula de gravação, ASR, Smart e Polish. O
  campo protegido continua bloqueando (o núcleo decide no aperto, antes de saber o destino; nada é
  capturado). Muda só o destino: no lugar da inserção, a gravação na nota (`internal/agent/paranota`), sem
  SendInput nem clipboard.
- **Só notas ativas são destino.** A arquivada e a da lixeira com o mesmo título não recebem nada; o ditado
  vira nota nova, à vista. Por quê: o Arquivo saiu da tela de propósito, e a busca única e a sugestão de
  marcador também só olham as ativas (N4, N6); acrescentar numa arquivada esconderia o texto. Alternativa:
  incluir as arquivadas e desarquivar ao acrescentar.
- **O casamento** é palavra a palavra, sem caixa e sem acento (a mesma dobra da busca), com a pontuação do
  fim do título ignorada; depois do título vem fim de palavra e, opcionalmente, ":", ",", ";", ".", "!",
  "?", travessão ou espaço (o ponto entra porque o ASR às vezes põe "Lista de compras. Leite"). Nota sem
  título nunca casa.
- **O título mais longo vence** ("lista de compras: sal" vai para "Lista de compras", não para "Lista"); com
  o mesmo título, a editada por último. **O ditado que é só o título** não casa (não há o que acrescentar), e
  um título mais curto não fica com o resto: vira nota nova, nada se perde.
- **Como entra** (o resto, depois do título): na lista de marcar, itens `- [ ] ` logo depois do último item
  aberto (todos marcados: antes do primeiro marcado), para os marcados continuarem no fim (N2); itens pela
  regra das listas (`resolver.ItensPorRegra`, a mesma do painel), ou um item só, ou uma linha por item quando
  o Polish já entregou a lista em linhas; a inicial maiúscula como a regra (menos sigla). Lista com `- `:
  `- `. **[desvio]** Lista numerada: continua a contagem (a ADR não dizia). Sem lista: o resto no fim, como
  foi ditado. Nada que já estava no corpo muda.
- **A nota nova** leva o ditado inteiro, sem título, e `ditado_de_origem` = a linha do histórico (o Agent
  escolhe o id antes): o cartão diz "veio de um ditado", como no "Virar nota".
- **O histórico**: gravado na nota, o ditado sai do histórico com `virou_nota` (como na N3); **se a nota não
  grava** (banco, corpo grande demais, a nota mudando três vezes no meio), ele fica à vista com o texto e
  `status = 'error'`, e a pílula diz "Não salvou na nota: está no histórico." A nota que o painel salvou no
  meio é relida e o acréscimo refeito em cima (concorrência otimista); uma nota aberta no painel durante o
  ditado recebe o 409 de sempre ao salvar, com a escolha que a janela já oferece.
- **A pílula**: "Lista de compras +3" (o título cortado com "…" antes de 40 letras), "Nota nova", ou a
  falha, com ponto cinza. A gravação mostra a mesma onda de sempre: **[desvio]** nada na gravação diz que é
  para nota (só o desfecho); se fizer falta, é uma marca pequena na cápsula, a decidir com o Mauro.
- **Configurações**: "Ditar para nota" liga e desliga (padrão ligado; `note_trigger_enabled`, migration
  `0011`); as teclas são só leitura, como as do ditado. Desligado, Ctrl+Win+Alt é um ditado comum.
- Sem evento de sync, como N1 e N3 (até a N7; o ditar para nota chegou à nuvem junto com o resto das
  notas, ver Decisões da N7). O painel na aba Notas relê a lista (e as contas dos marcadores) ao
  voltar à frente, como já fazia.
- O que só a máquina real mostra: se Ctrl+Win+Alt colide com atalho de outro programa (não medido), e se o
  menu Iniciar ou a barra de menus de algum app reage ao Alt solto por último (o Ctrl já quebra o "Win
  sozinho" que abre o Iniciar; o Alt sozinho ativa menus só quando é a única tecla). Roteiro:
  `docs/teste-manual-notas-n9.md`.

### Formatar: converter trecho

No editor, selecionar linhas e escolher: **lista**, **lista de marcar**, **numerada**, **título**
ou **parágrafo**.

- Converter linhas que já são uma por item é só trocar o prefixo, sem IA.
- **Converter texto corrido em itens** ("comprar leite pão café e duas dúzias de ovos") é a decisão 2:
  - vai ao provedor do Polish, com a chave da pessoa, com um **prompt próprio** (`notas-lista@1`):
    separar em itens, sem trocar, acrescentar nem apagar palavra, a não ser o "e" e a vírgula entre
    itens;
  - a resposta passa pelas guardas da ADR-016 que valem para lista: palavras (subsequência, o "e" da
    lista licenciado pelo marcador), números, siglas e termos, sentido, injeção;
  - recusada (ou sem chave, ou sem rede), a **conversão por regra** assume: quebra em vírgula, ponto,
    ponto e vírgula e no último " e "; nunca dentro de número ("1,5 kg").
  - Para quem usa: com ou sem IA, a lista sai com as palavras dele; a IA só acerta mais onde não há
    vírgula ("leite pão café").
- O ditado que caiu no histórico com lista também pode ser convertido ao virar nota.

**Decisões da N5 (26-out-2026), na implementação.** As marcadas **[desvio]** mudam algo escrito acima.
- **O modelo só decide onde cortar.** O texto de cada item que volta à nota é recortado da própria linha,
  entre a primeira e a última palavra que a guarda casou com aquele item (sem a vírgula, o ponto e vírgula e o
  ponto das bordas, com a inicial maiúscula, menos sigla). Nada que o modelo escreveu chega à nota. Por isso
  as guardas são as de **lista**, em `resolver.GuardarItens`, e não o `Polir` inteiro: JSON; dois itens ou
  mais (um item só é o modelo dizendo "não é lista", motivo `um_item`); item com palavra e sem quebra; sem
  marcador, markdown nem símbolo novo (a parte da guarda de formato que vale aqui); números iguais **item a
  item**; as palavras da linha na mesma ordem, uma a uma, com a única folga de UM "e" logo antes de um item
  que não é o primeiro e da inicial de cada item; e a grafia de cada palavra (sigla e nome com outra caixa,
  `siglas_termos`; palavra comum, `caixa`). **[desvio]** A guarda de "sentido" e a consulta ao dicionário
  não entram: sem palavra trocada nem acrescentada, e com o texto recortado da linha, não há como mudar
  sentido nem termo. As guardas do Polish (`polish_guardas.go`) não mudaram; a ADR-016 não ganha emenda.
- **A IA só com o Polish ligado** (modo polish, provedor e modelo escolhidos), o mesmo opt-in do ditado; fora
  disso, a regra, e nada sai do PC.
- **Que linhas vão ao Agent:** uma linha só (a do cursor, ou a única escolhida), que não é título, com duas
  palavras ou mais. **[desvio]** Inclui a linha que já é item de lista: é como o ditado sem vírgula numa
  lista ("leite pão café", que a regra não separa) vira itens. Várias linhas escolhidas, título e linha de
  uma palavra seguem só trocando o prefixo.
- **Sem itens** (nem a IA nem a regra), a linha só ganha o tipo pedido, com o aviso "Não achei itens para
  separar: a linha virou um item só." O texto não muda.
- **O ditado numa linha de lista continua só com a regra; a IA não é chamada ali.** Por quê: quase todo
  ditado numa lista é um item só, e cada um viraria uma chamada paga sem a pessoa pedir; a resposta chega
  segundos depois e trocaria a linha enquanto a pessoa já dita ou digita a próxima; e, com o Polish ligado, a
  enumeração ditada já chega em linhas "- ", que a colagem separa. O Formatar cobre o caso sem vírgula, a
  pedido. O botão "Virar lista" também segue só com a regra (e só aparece quando ela separa).
- A regra do painel e a do Agent (`ItensPorRegra`) são a mesma e rodam os mesmos casos
  (`testdata/notas-lista/regra.json`). Ela parte na vírgula (nunca entre dois dígitos), no ponto e vírgula e
  no último " e "; o ponto só sai do fim, como já era desde a N2. Ganhou um ajuste nos dois lados: o item que
  começa por sigla não ganha maiúscula ("eSocial" não vira "ESocial").
- **[desvio]** A categoria `notas-lista` do `polishbench` mora num arquivo próprio
  (`testdata/polish-dataset/notas-lista.json`) e roda com `-notas-lista`: outro prompt, outra saída, outras
  métricas, e o hash do dataset do Polish, que os relatórios antigos citam, não muda (`19-eval-plan.md`).

### Lista de marcar

- Marcar um item o leva para o fim da lista, abaixo de uma linha "N marcados", recolhível (como no
  Keep). **[derivada]** a ordem gravada no corpo é a da tela (os marcados no fim), para o celular e o
  PC mostrarem igual sem regra de ordenação no leitor.
- **"Desmarcar tudo"** e **"Apagar marcados"** no menu da nota.

### Copiar como texto de WhatsApp

Menu da nota → **Copiar para WhatsApp**: título em `*negrito*`, marcador como `•`, lista de marcar
como `☐`/`✅`, numerada como está. Usa a área de transferência **pela ação de quem clica** no
navegador (`navigator.clipboard.writeText`), nunca pelo Agent (`CLAUDE.md`, "Testes que agem no
sistema real"). No celular, abre o compartilhamento do Android (`navigator.share`).

### Busca única

A busca do painel (Ctrl+K) procura nas notas e no histórico, com os resultados em dois grupos
("Notas", "Ditados"). Nas notas, procura em título e corpo. Para quem usa: a "memória" do pedido.

**[derivada] N6: um campo só, sem diferenciar acento.** O campo "Buscar nas notas" da N2 e a busca do
histórico viraram o mesmo campo, "Buscar nas notas e nos ditados", nas abas Histórico e Notas; com texto,
os dois grupos (até 5 de cada) tomam o lugar da lista, e "Ver todas" leva à aba do grupo com a mesma busca.
Só as notas ativas entram (Arquivo e Lixeira não). A busca do histórico do Agent passou a ignorar acento,
como a das notas, para o mesmo termo achar o mesmo nos dois grupos. Por quê: duas buscas no mesmo painel
confundiriam, e "reuniao" achar a nota mas não o ditado seria um erro.

### Sugestão de marcador

**[derivada] Local, sem IA.** Ao salvar uma nota sem marcador, o painel compara as palavras dela com
as das notas de cada marcador (frequência, sem palavras vazias) e, se um marcador se destaca, mostra
"Marcar como Compras?" com um clique. Nunca marca sozinho. Por quê: custo zero, nada sai do PC, e
com poucas notas a regra simples já acerta o caso típico (a segunda lista de compras). Alternativa:
perguntar ao modelo do Polish. Ver E2.

### A tela

- **Aba "Notas"** ao lado do histórico, no PC e no celular (o mesmo código React, ADR-019 decisão 12).
- No topo, o campo "Criar uma nota..." (o print do Keep do Mauro); expande em título, corpo e a barra:
  formatar, cor, marcador, imagem (N10), arquivar, mais (copiar, desmarcar tudo, lixeira).
- Abaixo, os cards em grade: **fixadas** primeiro, depois as outras, cada grupo na **ordem manual**
  (seção "Ordem manual (N2b)"). No celular, uma coluna.
- Lateral (ou menu, no celular): **Notas**, cada marcador, **Arquivo**, **Lixeira**, **Editar
  marcadores**.
- Discreto como o resto do painel (`PRODUCT.md`): sem animação de vitrine, a cor do card só como
  fundo suave.

**Decisões do Mauro sobre a prévia da tela (25-out-2026, "Está SENSACIONAL"):** valem por cima dos
itens acima.
- **Marcadores numa fileira embaixo da busca**, não na lateral: Notas, cada marcador, Arquivo,
  Lixeira e "Editar marcadores". No celular, a fileira rola para o lado.
- **A nota abre numa janela sobre a grade**, com os colchetes do editor do histórico.
- **A lixeira das notas fica dentro da aba Notas.** A aba Lixeira de cima continua só dos ditados.
- **Colunas:** 4 no PC por padrão, de 1 a 6; 2 no celular por padrão, de 1 a 3. **[derivada]** Com
  4 colunas o painel de 46rem não cabe: a aba Notas usa a largura da janela (até ~96rem), e as
  outras abas ficam como estão. A escolha fica no navegador (`localStorage`), por aparelho.
- **Três formas de ver:** quadradinhos (a grade de cartões, com as colunas acima), lista e
  detalhes. **[derivada]** Lista: uma nota por linha, título e o começo do corpo, a cor numa
  bolinha. Detalhes: uma tabela com título, marcadores, itens marcados ("2/6"), cor e quando foi
  editada. A forma escolhida também fica no navegador.

**Depois do primeiro teste do Mauro (25-out-2026):**
- **A nota nova começa no corpo**, como no Keep ("No corpo"): o primeiro ditado ia para o título.
- **Enumeração ditada numa linha de lista vira itens** ("não gera lista automaticamente"): o painel
  reconhece o ditado pela rajada de letras (o Agent insere tudo de uma vez, por teclas ou colando) e
  aplica a conversão por regra desta ADR (vírgula, ponto e vírgula, último " e "; nunca "1,5").
- **Enumeração ditada em texto comum fica texto, com o botão "Virar lista"** (escolha dele: "Fica
  texto, com botão"), que separa em lista de marcar.
- **A linha em edição tem um fundo leve, a linha inteira**, no lugar do traço verde à esquerda ("ficou
  muito genérico").

**Marcadores, prévia da N4 aprovada pelo Mauro (26-out-2026, "Concordo"):** `docs/notas-previa-n4.html`.
- "Editar marcadores" no fim da fileira abre uma janela pequena: criar no campo de cima (nome
  repetido, sem diferenciar maiúscula, é recusado com aviso), renomear na linha, apagar com a
  pergunta na própria linha ("Ele sai das N notas; as notas ficam").
- O botão de marcador da nota e do cartão abre uma lista com caixinhas (marca e desmarca na hora), com
  "Criar marcador" para o nome que não existe; na nota, cada marcador tem um × para tirar.
- **A sugestão aparece dentro da nota aberta**, numa faixa discreta ("Marcar como Compras?" com Marcar
  e Agora não), só em nota sem marcador; "Agora não" vale para aquela nota.
- **Só sugere marcador que já tem 2 notas ou mais.**
- **Uma nota pode ter vários marcadores.**
- **Feito na N4 (26-out-2026), só no painel (o Go da N1 bastou):** a regra da sugestão é uma função pura
  (`web/src/notas/sugestao.ts`): palavras do título e do corpo (sem acento, sem palavra vazia, 3 letras ou
  mais, uma vez por nota); cada marcador de 2 notas ou mais ganha a soma, sobre as palavras em comum, da fração
  das suas notas que usam a palavra; sugere o melhor com pelo menos 2 palavras em comum, 1 ponto e 1,5 vez o
  segundo. Por quê: é a "frequência dentro do marcador" do texto da ADR, sem biblioteca, e o empate não
  sugere nada. Alternativa: Jaccard (penaliza a nota curta contra o marcador de vocabulário largo). Só as
  notas da tela principal entram na conta; as do Arquivo não.

### Ordem manual (N2b)

Pedido do Mauro, 26-out-2026: "Assim como no keep eu quero poder arrastar as notas na tela e
reorganizá-las conforme eu achar melhor." As quatro respostas dele (a nota nova no topo; editar não
move; fixadas entre fixadas e outras entre outras; no celular, segurar e arrastar) estão na seção
Fases, e valem como regra abaixo.

**O campo.** `local_notes.posicao`, um **índice fracionário**: uma chave de texto no alfabeto
`0-9A-Za-z`, que **não termina em `0`**, e a lista sai por `fixada DESC, posicao ASC` (a menor chave é
o topo). Mover uma nota grava **uma** chave nova, a que cai entre as duas vizinhas, e mais nenhuma
nota é tocada: o sync (N7) leva só a nota que mudou. A chave só cresce com o uso: a nota nova e a que
se fixa pegam a chave "antes da primeira" (o dígito de antes, não o meio, para gastar um caractere
a cada ~30 notas no topo, e não a cada cinco), e arrastar para o meio gasta um caractere a cada
dezenas de arrastos na mesma fresta. O teto é de 128 caracteres; o uso normal fica abaixo de 10.
Migration `0012`, só de coluna e índice novos (nada destrutivo), com o preenchimento que **mantém a
ordem de hoje** (fixadas, depois a última edição, depois a criação mais recente) em três dígitos de
base 61 por pessoa.

**Os grupos.** Uma nota está num de quatro: (ativa ou arquivada) x (fixada ou não), fora da lixeira.
A ordem manual é a das **ativas**; as fixadas se ordenam entre si e as outras entre as outras. Quem
muda de grupo vai para o **topo do grupo de destino**: fixar e desafixar, **restaurar da lixeira** e
**tirar do Arquivo**. Nota nova entra no topo das outras. **Editar** (título, corpo, cor, marcador,
marcar item) e o **acrescentar por voz** da N9 **não mexem** na posição. O Arquivo continua na ordem
em que as notas foram arquivadas (a mais recente primeiro): arrastar lá avisa.

**A API.** `POST /api/notas/{id}/mover` com `{antes, depois}`, os ids das vizinhas **como ficaram
depois de soltar** (um dos dois pode ser nulo: a ponta). Quem calcula a chave é o Agent; o painel
não conhece chaves. Devolve a nota. 400 sem nenhuma vizinha; 404 nota desconhecida; 409 para vizinha
que não existe, está na lixeira, é de outro grupo (fixada com não fixada, ativa com arquivada), é a
própria nota ou está fora de ordem, e para a própria nota na lixeira; o painel lê a lista de novo.
Se a nota já está entre as duas, nada é gravado.

**[derivada] Versão e concorrência.** `mover` **sobe `version`** (o sync da N7 precisa ver a
mudança) e **não** sobe `updated_at` (arrastar não é editar). O pedido não leva `base_version`:
a posição é uma intenção de lugar, não de conteúdo. Para a janela de uma nota aberta não dar 409 à
toa, o painel põe os `mover` e as edições (PATCH) **numa fila de um pedido de cada vez**, guarda a
versão que cada `mover` devolveu e a usa no PATCH seguinte (e ao abrir a nota); a janela é modal, então
a nota que se edita não é a que se arrasta. O caso que sobra (outra aba mexeu) cai no caminho do 409
que já existia.

**[derivada] Sem `sync_outbox` (até a N7).** Na N2b a N7 ainda não existia e `mover` não gerava evento.
Com a N7, a `posicao` viaja **com a entidade nota**, como qualquer outro campo (o bit `CampoNotaPosicao`
da fila). **Decidido na N7:** o conflito de posição entre dois aparelhos é o último que escreve vence, sem
selo, porque ordem não é conteúdo (ver Decisões da N7). Dois aparelhos que arrastem para a mesma fresta podem gerar chaves iguais; no Agent o
desempate de leitura é o `rowid` (estável) e o próximo arrasto desfaz o empate.

**A grade.** Em vez de colunas de CSS, o painel monta a grade em JS, **na ordem de leitura**: cada
cartão vai para a coluna mais curta (a da esquerda no empate), em posição absoluta com `transform`.
Medida a altura (`offsetHeight`, `ResizeObserver` do container e `document.fonts.ready`), a
disposição é uma função pura (`ordem.ts`, `dispor`); antes da medida vale o rodízio `i % n`. As
colunas continuam as do Mauro (1 a 6 no PC, 1 a 3 no celular), as seções FIXADAS e OUTRAS e o visual
são os de antes. A lista e os detalhes reorganizam também (a lista é a mesma engrenagem com uma
coluna; os detalhes são uma tabela com um `<tbody>` por grupo, em que as linhas ficam no fluxo e só
se deslocam).

**Arrastar.** Eventos de ponteiro. **Mouse e caneta:** passou de 5 px, começa (antes disso é
clique e abre a nota). **Toque:** segurar ~400 ms (vibração curta onde existe), e mexer antes
disso é a rolagem (a página rola normalmente até o cartão levantar; depois dele levantado, a rolagem da
página fica presa e só a rolagem automática das bordas anda). O cartão levantado
segue o ponteiro, os outros abrem a vaga (contorno tracejado) com `transform` suave (desligado em
`prefers-reduced-motion`), a janela rola sozinha perto das bordas, **Esc** cancela. A vaga que o
cartão ocupa é a **mais próxima do centro dele**, calculada da posição do ponteiro e não da
anterior (sem vaivém). Só dentro do grupo. Soltar muda a lista na hora (otimista) e pede o `mover`;
se o Agent recusa, a ordem de antes volta, a pílula diz por quê e a lista é lida de novo.
**[derivada]** Só vale na tela principal (Notas, sem filtro de marcador e sem busca); no filtro, na
busca e no Arquivo, arrastar com o mouse avisa por quê ("Para reorganizar, limpe a busca.", "... saia do
filtro de marcador: volte para Notas.", "O Arquivo fica na ordem em que as notas foram arquivadas."), porque
ali a lista é só um pedaço da ordem e o "entre estas duas" não faria sentido.

**Teclado.** `Alt` + seta no cartão move uma vaga dentro do grupo: na lista e nos detalhes, `↑` e `↓`;
**[derivada]** na grade, `←` e `→` andam uma vaga e `↑` e `↓` andam uma linha (as colunas). Na ponta
fica onde está. O leitor de tela ouve "Movida para a posição 3 de 8." (ou "Já está na posição 1 de 8."),
numa região `aria-live` à parte, e o foco fica no cartão. `Alt` + `←` e `→` são "voltar" e "avançar"
do navegador; o painel as toma só quando o foco está num cartão.

### Sync

- **[derivada]** Duas entidades novas no motor (`internal/sync`): `nota` e `marcador`, ao lado de
  `ditado` e `aprendido` (`storage.EntidadeDitado`, `storage.EntidadeAprendido`). Toda gravação com o
  evento na mesma transação (ADR-009).
- **[derivada] Ordem manual:** a `posicao` (N2b) é um campo da nota e **viaja com a entidade nota**, junto
  com o resto; mover não tem evento próprio (a gravação da nota que mudou de lugar é que sobe, com a
  versão nova). A migration do Postgres da N7 traz a coluna `posicao`, e a nota que sobe pela primeira
  vez leva a posição que tem. O conflito de posição entre dois aparelhos ficava para esta fase decidir;
  decidido: o último que escreve vence, sem selo (ver Decisões da N7).
- **Conflito:** a regra da ADR-019 vale igual. A versão que perdeu fica no item, com o selo, e a
  pessoa escolhe. **[derivada]** Para a lista de marcar há uma mescla a mais, porque o caso típico
  é o mercado (marcando no celular) com uma edição no PC: se os dois lados só mudaram o estado de
  marcado de itens diferentes (o texto de cada linha igual), o servidor mescla sem selo.
- Servidor: tabelas `notes`, `labels`, `note_labels` (migrations novas, só criação; nenhuma
  destrutiva). As rotas da nuvem seguem o padrão das de ditado. (Na implementação foram seis tabelas, as
  migrations `0020` a `0025`; ver Decisões da N7.)
- **Celular sem rede (E4, decisão do Mauro: "Marcar sem rede"):** no mercado sem sinal, a lista abre
  do cache do service worker e **marcar e desmarcar funcionam offline**. As mudanças vão para uma fila
  no navegador (IndexedDB), com o mesmo marcador por entidade do sync do PC, e sobem quando a rede
  volta; a mescla da lista de marcar acima resolve o caso de o PC ter mudado a nota nesse meio-tempo.
  [derivada] Offline, só marcar, desmarcar e acrescentar item; criar, apagar nota e mexer em marcador
  pedem rede (a tela diz isso). O app mostra "Sem rede: N mudanças esperando".

### Decisões da N7 (26-out-2026), na implementação

As marcadas **[desvio]** mudam algo escrito acima. O deploy é ponto de parada: o roteiro do Mauro é o
`docs/deploy-notas-n7.md`, e o teste depois dele, o `docs/teste-manual-notas-n7.md`.

- **Os nomes no fio:** `note` e `label` (o servidor fala inglês, como `transcription` e `learned`). O
  PC chama de `storage.EntidadeNota` e `storage.EntidadeMarcadorDeNota`. Operações: `upsert` e `delete`
  (apagar de vez), nas duas.
- **[desvio] Seis tabelas, e não três.** `notes`, `labels` e `note_labels` (`0020` a `0022`), mais
  `note_tombstones` (`0023`), `note_sync_events` (`0024`) e `transcription_note_links` (`0025`). Por quê:
  a convenção das migrations (só `CREATE TABLE` e `CREATE INDEX`, uma tabela por arquivo; o
  `migrations/schema_test.go` recusa `ALTER`) não deixa alargar o `CHECK` de `entity_type` do
  `sync_events` e do `tombstones`, nem pôr coluna no `transcriptions`. Uma tabela a mais por necessidade é
  o caminho aditivo. Alternativa: um `ALTER` (mudar a convenção, que congela as migrations desde o deploy).
- **`note_labels` sem chave estrangeira para `labels`.** A nota pode chegar à nuvem antes do marcador
  (outro PC, a ordem do pull é por estado, não por criação): o id fica guardado e vale quando o marcador
  chegar. O marcador com lápide sai do conjunto da nota na hora de gravar, e o "apagar" do marcador tira
  as ligações dele (sem subir a versão das notas: o PC que apagou manda as notas mudadas).
- **Conflito, campo a campo.** Título e corpo são um campo só (o **conteúdo**), com a regra da ADR-019:
  vale o que chegou primeiro, e o do PC vai para o selo (`versao_em_conflito`: título, corpo, aparelho e
  hora), com a escolha "manter" ou "trocar" como no ditado. A lixeira é a do ditado (apagar vence;
  restaurar contra uma lixeira mais nova não vale; a edição antiga contra a lixeira vai para o selo).
  **Cor, fixar, arquivar, marcadores e posição: o último que escreve vence, sem selo.** Por quê: não são
  conteúdo, e um selo por cor trocada seria barulho; o pior caso é a cor (ou a ordem) de um aparelho
  vencer a do outro, sem texto perdido.
- **Posição (o ponto aberto da N2b): o último que escreve vence, sem selo**, como a sugestão da N2b. Duas
  chaves iguais (dois aparelhos na mesma fresta) desempatam pelo `rowid` no PC; o próximo arrasto desfaz.
- **A mescla da lista de marcar** (`cloudstore.MesclarListaDeMarcar`, pura) é de três vias: precisa da
  base, e o PC a manda (`corpo_base`, o corpo da última versão que a nuvem confirmou, guardado em
  `local_notes.corpo_nuvem`). Sem a base, sem mescla: conflito com selo (nada se perde). Com ela, e com o
  mesmo título dos dois lados: as linhas que não são de marcar têm de ser iguais e na mesma ordem nos
  três, e os itens de marcar têm os mesmos textos nos três, **contados no corpo inteiro** (o painel leva
  o item marcado para o fim do corpo, passando por parágrafos; a posição do item não conta), e o estado de
  cada item é o do lado que mudou. Qualquer diferença de texto (item editado, acrescentado ou tirado,
  parágrafo mudado) é conflito normal, com selo. **"O mesmo item marcado de jeitos diferentes" não
  existe numa mescla de três vias de um sim/não**: se os dois lados mudaram o item, os dois o puseram no
  mesmo estado (o contrário da base), e isso mescla. Com textos repetidos ("leite" duas vezes), conta o
  número de marcados de cada texto: os dois lados mudando esse número para valores diferentes é conflito
  normal. Só os trechos de itens que mudaram são reescritos (os abertos na ordem da nuvem, depois os
  marcados); sem mudança, o corpo da nuvem volta igual. Um reordenar feito só de um lado perde para a
  ordem da nuvem (nenhum texto se perde). A resposta é `applied` com `mesclado: true`, e o PC recebe o
  corpo mesclado pelo pull.
- **Lápides.** Apagar de vez (a lixeira, o expurgo de 30 dias, o Desfazer do "Virar nota" que apaga a nota
  e o "apagar" do marcador) sobe como `delete`. No PC, o marcador da fila com o bit `CampoApagado` é a
  lápide até a nuvem responder (a linha já saiu); a nota que a nuvem nunca teve sai sem deixar nada. Na
  nuvem, a lápide (`note_tombstones`, só os ids) **não vence**: a nota não tem janela de 6 meses, e um
  PC parado por um ano não pode ressuscitar a nota. O `delete` de uma nota que a nuvem nem tem grava a
  lápide do mesmo jeito (um `upsert` atrasado do mesmo id recebe `gone`).
- **[desvio] O expurgo da lixeira das notas fica no PC.** A nuvem não expurga nota (não há job novo):
  toda lixeira de nota hoje vem de um PC, e o expurgo dele sobe como `delete`. Quando o celular puder pôr
  nota na lixeira (N8), a nuvem ganha o expurgo dela.
- **O "Virar nota" chega à nuvem (o ponto aberto da N3): opção (a).** O ditado ganha `virou_nota` no fio
  (o bit `CampoVirouNota` do marcador do ditado; a chave no payload de criar), a nuvem guarda a ligação em
  `transcription_note_links` e esconde o ditado do histórico do celular (a lista e a busca), como o PC já
  fazia. Sem conflito: o último que escreve vence. O `nova:<token>` (a caixa "Criar uma nota" ainda
  aberta) é só do PC e nunca sobe; quando a nota nasce, os ditados ligados a ela ganham o bit. O ditado
  feito dentro de uma nota sobe já com o `virou_nota`.
- **A retenção de 6 meses na nuvem continua valendo para o ditado que virou nota** (a ligação sai junto,
  em cascata). No PC continua como a N3 deixou (fora dos 6 meses). Por quê: a nuvem guarda o mínimo, e o
  texto vive na nota. **A nota de privacidade da N3 ("Virar nota" guarda o texto bruto para sempre no PC)
  continua em aberto**: a N7 não muda o PC.
- **`ditado_de_origem` sobe** no criar da nota (só o id), e nunca muda depois.
- **O envio inicial das notas.** Nada de clique novo: o "Enviar o histórico" (F7c) já é o aceite da pessoa
  para mandar o que é dela à nuvem. Com o aceite e com a nuvem que sabe de notas, toda rodada põe na fila
  os marcadores vivos e as notas fora da lixeira que a nuvem ainda não tem (`Fila.CompletarNotas`), sem
  cópia nova. A nota criada ou mudada com a nuvem conectada entra na fila na hora, como o ditado novo.
- **[desvio] A capacidade da nuvem.** O pull ganha `entidades` (`["transcription", "learned", "note",
  "label"]`). O PC só manda nota, marcador e o `virou_nota` do ditado depois de ver `note` ali (a chave
  `nuvem_notas` da tabela `instalacao`). Por quê: o Agent novo instalado antes do servidor novo teria toda
  nota recusada (`tipo_desconhecido`) e o ditado com `virou_nota` recusado (`campo_desconhecido`), e a
  recusa fica. Na primeira vez que vê a capacidade, o PC zera o cursor (lê tudo de novo uma vez: um
  binário antigo pode ter passado pelas notas sem as aplicar) e marca com `CampoVirouNota` todo ditado que
  virou nota e que a nuvem já tem. A nuvem que deixa de dizer `note` (uma volta de versão) faz o PC parar
  de mandar notas.
- **O marcador que a nota cita antes de ele chegar** vira, no PC, uma linha "que espera"
  (`sync_state = 'aguardando'`, na lixeira dos marcadores, invisível): a ligação fica guardada, e o
  marcador aparece quando chega. Dois marcadores com o mesmo nome (criados em dois PCs sem rede) não são
  recusados pela nuvem; o PC que recebe o segundo o grava com " (2)" no nome, só nele.
- **O que a N7 não faz:** a tela das notas no celular (N8), o expurgo das notas na nuvem (N8), e o painel
  da nuvem não mostra notas. No painel do PC, a nota com selo ganha um aviso no cartão e, na janela da
  nota, a outra versão com "Manter esta" e "Usar a outra".

### As notas no celular (N8): a prévia aprovada (27-out-2026)

A prévia é o `docs/notas-previa-n8.html`. O Mauro aprovou as seis cenas ("1 - Sim. Se tem o botão não faz
sentido o microfone. 2 - Isso mesmo. 3 - concordo 4 - Concordo 5 - concordo 6 - Perfeito.") e as quatro
decisões ("1 - Isso mesmo 2 - Sim 3 - sim 4 - Sim"):

- **A aba Notas no app do celular** é a mesma do PC (o mesmo componente, pelo `Ambiente`), com duas
  colunas por padrão (1 a 3), como as Decisões do Mauro sobre a prévia da tela já diziam.
- **Na aba Notas, o microfone do lado da busca sai** e no lugar dele fica o botão **"Ditar para nota"**,
  embaixo, ao alcance do polegar. No Histórico, o microfone continua como hoje (o ditado vai para o
  histórico).
- **"Ditar para nota" tem a regra do Ctrl+Win+Alt do PC** (N9): se o ditado começa com o título de uma nota,
  o resto entra no fim dela (como itens, se ela for lista de marcar); se não, nasce uma nota nova com o
  ditado inteiro. O aviso diz o destino ("Lista do sábado +2", ou "Nota nova") com **Abrir** e
  **Desfazer**. O ditado não fica no histórico.
- **Microfone dentro da nota aberta**, na barra de baixo: escreve onde está o cursor, como segurar Ctrl+Win
  dentro da nota no PC (N3). Numa linha de lista, a enumeração vira itens pela mesma regra do PC. O ditado
  não aparece no histórico.
- **Sem rede: marcar, desmarcar e acrescentar item** (a E4). Criar nota, mudar o texto, a cor e os
  marcadores, e ditar, esperam a rede, com os botões apagados e o aviso "Sem rede: dá para marcar,
  desmarcar e acrescentar item. O resto volta com a rede." A faixa do topo conta o que espera ("Sem rede ·
  2 mudanças esperando"), e o cartão com mudança guardada mostra "esperando a rede". O que espera fica no
  aparelho (sobrevive a fechar o app e a desligar o celular) e sobe sozinho quando a rede volta.
- **A volta da rede:** os dois lados só marcaram itens, a mescla da N7 junta sem perguntar; os dois
  mudaram o texto, o selo da N7 ("Versão em conflito") e a escolha ao abrir. Nada se perde calado.
- **Arrastar com o dedo:** segurar meio segundo levanta a nota; passar o dedo sem segurar rola a tela (o
  gesto da N2b, agora conferido num celular de verdade).
- **A lixeira das notas esvazia sozinha também na nuvem, em 30 dias**, como no PC (o expurgo que a N7
  deixou para cá).

### Imagens (fase N10, por último)

Anexar imagem à nota (o ícone do Keep). Desenho prévio, a confirmar quando a fase chegar:
- no PC, o arquivo em `<dados>\notas-imagens\`, até 10 MB por imagem, reduzida para no máximo
  2.000 px no lado maior;
- na nuvem, no disco da VPS (volume próprio do compose), entrando no backup cifrado;
- **ponto de parada:** volume novo no servidor, espaço em disco e a revisão de LGPD (foto de
  documento de cliente é dado pessoal) precisam do OK do Mauro antes da N10.

## Alternativas descartadas

- **Usar o Keep pela API do Google.** O Keep não tem API para conta pessoal; e o pedido é justamente
  sair do Keep.
- **Biblioteca de editor (TipTap, Lexical, Slate).** Resolve formatação rica, mas é dependência de
  produção grande para um editor por linhas. Fica o editor próprio; se a formatação dentro da linha
  (E3) entrar, a decisão é revista.
- **Nota como um tipo de ditado** (a mesma tabela, com uma coluna "é nota"). Economiza tabela, mas
  mistura a retenção, a lixeira e o sync de duas coisas com regras diferentes.

## Riscos, e o que não foi verificado

- **O editor por linhas e a inserção do Agent.** O Agent insere por colagem ou por digitação
  (ADR-011); se o editor tiver um campo por linha, o texto ditado com várias linhas cai num campo só.
  A N2 testa isso na verificação manual; a saída provável é o editor tratar a colagem de várias
  linhas (cada linha vira um bloco).
- **"Virar nota" e o sync (N7): decidido, opção (a).** Até a N7, `virou_nota` era só deste PC e não gerava
  evento: a nuvem continuava com o ditado no histórico dela (a opção (b), por omissão). A N7 escolheu
  (a): o ditado some também do histórico da nuvem e do celular, e a marca sobe como uma mudança de campo
  do ditado, com a ligação numa tabela à parte. Ver "Decisões da N7", item "O 'Virar nota' chega à
  nuvem". O que a N7 não resolve é o item seguinte (o texto bruto que fica no PC).
- **"Virar nota" guarda o texto bruto para sempre.** A retenção de 6 meses não alcança o ditado que
  virou nota (a ADR, lida à letra), então o `raw_text` dele (e o `final_text`) fica no PC sem prazo,
  mesmo que a pessoa apague a nota da lixeira depois (o ditado fica escondido, com o id solto). Se isso
  incomodar na LGPD (o ditado é dado pessoal), a saída é limpar o `raw_text` do ditado quando a nota
  for apagada de vez, ou deixá-lo voltar à regra dos 6 meses nesse caso. Não foi decidido.
- **O segundo gatilho** pode colidir com atalho de outro programa (`Ctrl+Win+Alt` é pouco usado, mas
  não foi medido). O detector de conflito de atalho do Agent (`internal/agent/conflito`) passa a
  olhar os dois.
- **Mescla da lista de marcar no servidor** é código novo no lugar mais delicado (o sync). Vai com
  testes de propriedade e revisão adversarial. (N7: a mescla é pura e tem tabela de casos e teste de
  propriedade, `cloudstore/lista_de_marcar_test.go`; ver Decisões da N7.)
- **Texto da nota no servidor:** mesmo tratamento dos ditados (ADR-019); a política de privacidade
  ganha uma linha.
- **Custo da IA na conversão:** uma chamada por conversão de texto corrido, na chave da pessoa
  (centavos por mês no uso esperado).

## Fases

Cada fase é uma ou mais vertical slices, com teste, commit local e o relatório de sempre. Push,
deploy e release só com a autorização do Mauro.

| Fase | O quê | Depende de |
|---|---|---|
| L | Listas melhores no Polish: afinar as guardas para lista, medindo no `polishbench` (categoria `lista` e o diagnóstico do histórico) | — |
| N1 | Banco local (SQLite) e API local das notas e marcadores: criar, editar, fixar, arquivar, cor, lixeira e expurgo | — |
| N2 | Aba Notas no painel: campo "Criar uma nota", cards, editor por linhas, ditar dentro (verificação manual) | N1 |
| N2b | Ordem manual: `posicao` (índice fracionário, migration 0012), `POST /api/notas/{id}/mover`, grade em ordem de leitura, arrastar com mouse e toque, Alt + setas | N2 |
| N3 | "Virar nota" no histórico, com Desfazer | N2 |
| N4 | Marcadores: criar, renomear, apagar, filtro na lateral, sugestão de marcador | N2 |
| N5 | Converter trecho: por prefixo, por regra e com IA (prompt `notas-lista@1` com as guardas; benchmark próprio no `polishbench`) | N2, L |
| N6 | Lista de marcar (marcados descem, desmarcar tudo), copiar para WhatsApp, busca única | N2 |
| N7 | Sync das notas e marcadores: migrations do Postgres, entidades no motor, mescla da lista de marcar, rotas da nuvem (a `posicao` da N2b viaja com a nota). **Deploy: ponto de parada.** **Feita localmente (26-out-2026), nos dublês, em quatro commits na branch `notas/n7`; o deploy espera o Mauro** (`docs/deploy-notas-n7.md`, depois `docs/teste-manual-notas-n7.md`). Ver "Decisões da N7" | N1-N6, N2b |
| N8 | Aba Notas no celular (PWA), com "Ditar para nota" e a fila offline (marcar, desmarcar e acrescentar item sem rede) | N7 |
| N9 | Gatilho "ditar para nota" no PC, com o destino pelo nome | N2 |
| N10 | Imagens. **Ponto de parada antes** (servidor, disco, LGPD) | N7 |

Ordem sugerida: L e N1 em paralelo; depois N2, N3, N6, N4, N5, N9; e N7, N8, N10.

**Pedido do Mauro, 26-out-2026 (durante o teste da N9), virou a fase N2b:** "Assim como no keep eu quero
poder arrastar as notas na tela e reorganizá-las conforme eu achar melhor." Pede uma ordem manual
gravada na nota (um campo de posição, que também entra no sync da N7) e arrastar e soltar na grade,
com alternativa pelo teclado. Vem antes da N7, para o sync já nascer com a posição. O desenho e as
escolhas derivadas estão na seção "Ordem manual (N2b)".

**Respostas do Mauro (26-out-2026):**
- a nota nova entra no **topo**;
- editar **não** move a nota: ela "fica onde eu coloquei";
- **fixadas se arrastam entre si** (e as outras, entre as outras);
- no celular, **segurar e arrastar** (o gesto está feito e testado com eventos de toque simulados; a
  conferência num celular de verdade fica para a N8, quando o painel chegar ao PWA).

## Escolhas menores (aprovadas em 25-out-2026: todas no padrão, menos a E4)

- **E1. Acrescentar por voz:** segundo gatilho `Ctrl+Win+Alt` (padrão) ou frase-comando no ditado
  comum.
- **E2. Sugestão de marcador:** local, sem IA (padrão) ou pelo modelo do Polish.
- **E3. Formatação dentro da linha** (negrito, itálico, link): fora desta ADR (padrão) ou na N2.
- **E4. Celular sem rede:** ~~só leitura do cache~~; **o Mauro escolheu marcar sem rede** (fila offline na N8).
- **E5. Cores:** as 8 do Keep em tons discretos (padrão) ou menos.
- **E6. Ditado que vira nota:** escondido do histórico com `virou_nota` (padrão) ou para a lixeira.

## Testes

- Banco e API local: CRUD, lixeira e expurgo, arquivo, fixar, marcadores (nome único sem caixa),
  "Virar nota" e Desfazer, evento de sync na mesma transação.
- Conversão por regra: tabela de casos (vírgula, "e", número com vírgula, linha vazia).
- Conversão com IA: as guardas da ADR-016 sobre o prompt novo, com a categoria nova no
  `polishbench` (a auditoria independente, como nas outras).
- Ordem manual: a chave entre duas vizinhas, antes da primeira e depois da última, muitas inserções
  na mesma fresta, nunca duas chaves iguais; a migration preserva a ordem de hoje; nota nova no topo;
  editar e acrescentar por voz não movem; fixar, desafixar, restaurar e tirar do Arquivo vão para o
  topo; `/mover` com vizinha de outro grupo, da lixeira ou do Arquivo (409), sem vizinha (400);
  no painel, a grade em ordem de leitura (tabela de alturas), arrastar com mouse e toque, Esc,
  recusa do Agent, teclado, desligado no filtro, na busca e no Arquivo.
- Destino do "ditar para nota": casamento do nome, empate, nome inexistente, nota com lista de marcar.
- Sync: as duas entidades no motor, conflito com selo, mescla da lista de marcar (só estados
  diferentes mescla; texto diferente vira selo), lápide.
- Fila offline do celular: marcar sem rede, recarregar a página, a rede voltar, o PC ter mudado a
  nota no meio-tempo (mescla), e a mudança que o servidor recusa (selo, nunca perda calada).
- Painel: os componentes com Testing Library, nos dois temas.
- Verificação manual (roteiro, uma pessoa sabendo o que vai acontecer): ditar dentro da nota, o
  segundo gatilho, copiar para WhatsApp, arrastar as notas (`docs/teste-manual-notas-ordem.md`).
