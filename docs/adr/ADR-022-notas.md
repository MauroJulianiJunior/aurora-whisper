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
  **[mudou em 28-out-2026]** Alcança, no PC e na nuvem: ver "Riscos", item "'Virar nota' guarda o texto
  bruto para sempre".
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
- No celular, o equivalente é um botão "Ditar para nota" (fase N8, com a mesma regra do nome; feito, ver
  "Decisões da N8").

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
  pedem rede (a tela diz isso). O app mostra "Sem rede: N mudanças esperando". **[desvio, N8]** A lista não
  vem do cache do service worker: vem do mesmo IndexedDB da fila (ver "Decisões da N8").

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
  nota na lixeira (N8), a nuvem ganha o expurgo dela. (Feito na N8: ver "Decisões da N8".)
- **O "Virar nota" chega à nuvem (o ponto aberto da N3): opção (a).** O ditado ganha `virou_nota` no fio
  (o bit `CampoVirouNota` do marcador do ditado; a chave no payload de criar), a nuvem guarda a ligação em
  `transcription_note_links` e esconde o ditado do histórico do celular (a lista e a busca), como o PC já
  fazia. Sem conflito: o último que escreve vence. O `nova:<token>` (a caixa "Criar uma nota" ainda
  aberta) é só do PC e nunca sobe; quando a nota nasce, os ditados ligados a ela ganham o bit. O ditado
  feito dentro de uma nota sobe já com o `virou_nota`.
- **A retenção de 6 meses na nuvem continua valendo para o ditado que virou nota** (a ligação sai junto,
  em cascata). No PC continua como a N3 deixou (fora dos 6 meses). Por quê: a nuvem guarda o mínimo, e o
  texto vive na nota. **A nota de privacidade da N3 ("Virar nota" guarda o texto bruto para sempre no PC)
  continua em aberto**: a N7 não muda o PC. (Fechada em 28-out-2026: o PC segue os 6 meses também; ver
  "Riscos".)
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

### Decisões da N8 (27-out-2026), na implementação

As marcadas **[desvio]** mudam algo escrito acima (ou na prévia). O deploy é ponto de parada: o roteiro do
Mauro é o `docs/deploy-notas-n8.md`, e o teste depois dele, o `docs/teste-manual-notas-n8.md`. **Não há
migration nova**: as seis tabelas da N7 bastam.

- **As escritas do celular, pela sessão** (`internal/server/notas_celular_routes.go`; `06`, "As escritas do
  celular"): criar, editar, a escolha do selo, lixeira, restaurar, apagar de vez, mover, marcar e desmarcar,
  e o marcador (criar, renomear, apagar). **[derivada]** Cada pedido vira a mutação que um PC mandaria e
  passa pelas regras da N7 (`DecideNote`, `DecideLabel`), com o `Actor` sem aparelho e o evento na
  `note_sync_events` na mesma transação: o celular não tem regra própria, e o PC recebe a mudança pelo pull.
  O plano de cada pedido é puro (`cloudstore/notas_celular.go`: a posição no topo do grupo, as vizinhas do
  mover, o conjunto de marcadores), o mesmo na memória e no Postgres.
- **[desvio da API local] O `PATCH` com a base velha não é `409`.** No PC, a API local recusa a versão velha
  (o Agent é a única ponta). No celular, a escrita **é** o sync: as regras da N7 decidem, e a resposta diz o
  que ficou (`gravacao`: `mesclada` ou `no_selo`). Por quê: o caso típico é o mercado (o celular marcando com
  a nota aberta, alguém em casa marcando no PC), e o `409` do PC ("salvar por cima") jogaria fora o que o
  outro marcou. A janela da nota passa a mostrar o que ficou (`DialogoDaNota`, `adotar`), e no selo a outra
  versão é a do celular. O cliente manda o conteúdo inteiro e o `corpo_base` (o corpo da versão que a janela
  conhece). Renomear o marcador também não é `409`: o nome do celular vence, como o do PC (`DecideLabel`); o
  nome repetido continua `409 NAME_IN_USE`.
- **O "Ditar para nota" na nuvem.** O cabeçalho `Ditado-Nota: para-nota` no `POST /dictations`, e a regra do
  PC aplicada **no servidor**, na mesma transação que grava o ditado (`Store.CreateParaNota`): a nota (o
  acrescentar, ou a nota nova) e o ditado já ligado a ela (`virou_nota`), cada um com o seu seq. Por isso o
  ditado **nunca** aparece no histórico, em aparelho nenhum, nem por um instante. A parte pura da N9 (o
  casamento do título, o acrescentar, a dobra sem acento) e a ordem manual (o índice fracionário) saíram do
  `internal/storage` e do `internal/agent/paranota` para **`internal/notas`**, que os dois lados importam:
  uma regra só. O acrescentar que passaria do teto do corpo vira nota nova (nada se perde).
  **[desvio da prévia]** A prévia mostrou "lista do sábado: queijo e presunto" virando dois itens; pela
  regra da N9 (a mesma do PC), sem vírgula a enumeração não se separa ("Queijo e presunto" é um item). Com
  o Polish ligado, a lista já chega em linhas e vira itens; sem ele, "queijo, presunto e pão" vira três.
- **O aviso** é o da prévia: "Lista do sábado +2" (o título cortado antes de 40 letras, como a pílula do PC)
  ou "Nota nova", com **Abrir** e **Desfazer**. **[derivada] O Desfazer** é o do "Virar nota" (N3): o ditado
  sai da nota e **volta ao histórico** (nada se perde); a nota nova nascida dele e ainda na versão 1 sai de
  vez; o acrescentar volta ao corpo de antes se a nota ainda está na versão do ditado. Mexida depois, a nota
  fica, e o aviso diz "A nota ficou, porque mudou depois do ditado." Rota: `POST
  /transcriptions/{id}/undo-note`. Alternativa: apagar o ditado (perderia a fala de quem desfez por engano).
  **[desvio pequeno]** Com o botão na tela, a pílula do aviso fica acima dele (na prévia, no lugar dele):
  o botão continua à mão para o próximo ditado.
- **O microfone dentro da nota** (o `Ditado-Nota` com o id da nota): o ditado nasce ligado à nota (fora do
  histórico), e o app escreve o texto **onde o cursor estava quando a gravação começou** (a pessoa pode
  tocar em outro lugar enquanto fala); numa linha de lista, a enumeração vira itens pela regra do PC
  (`corpo.ts`, `inserirDitado`). **[derivada]** Só na nota que já existe: a caixa "Criar uma nota…" ainda
  não tem id (no PC ela tem o token do Agent); ali vale o microfone do teclado do celular.
- **A barra da nota no celular** é a da prévia: Formatar, Cor, Marcadores, Mais e o microfone, sem o botão
  da imagem (N10) e com **[desvio]** o Arquivar dentro do Mais (não cabia na largura de um celular). A lista
  de marcar ganha a linha **"+ Item"** (a prévia), com e sem rede. O PC não muda.
- **[desvio] Sem rede, a lista vem do IndexedDB, não do cache do service worker.** O app guarda, num banco
  IndexedDB por conta (`web/src/nuvem/notas/armazem.ts`), a última lista que a nuvem deu, os marcadores e a
  **fila**; o service worker continua guardando só a casca do app. Por quê: a fila precisa da lista para
  mostrar o "esperando a rede" por cima dela, e um lugar só é um lugar só para apagar (o Sair e o "Apagar meus
  dados" apagam o banco). O app aberto **sem rede** abre na aba Notas, com a última conta que abriu o app
  (anotada no banco `aurora-notas`, só o id, o e-mail e o nome); sem conta anotada, a tela "Sem conexão" de
  sempre.
- **A fila** (`notasNaNuvem.ts`): uma entrada por nota, com a versão e o conteúdo da base (o que a nuvem
  confirmou) e o conteúdo de agora; ela sobe sozinha (a rede que volta, o app que volta à frente, e a cada
  30 s enquanto houver o que subir) com o `PATCH` de `base_version` e `corpo_base`. A nuvem junta (a mescla
  da N7, sem perguntar) ou guarda no selo (com o aviso "mudou em outro aparelho também: as duas versões estão
  nela"). **Nunca perda calada:** a nuvem que recusa (a nota apagada de vez no meio, o texto que não passa)
  faz a mudança virar uma **nota nova** "… (feita sem rede)", com aviso; a resposta que se perdeu (a rede
  caiu no meio do envio) faz o app ler a nota antes de mandar de novo, e não mandar o que já valeu.
  "Sem rede" é o que o navegador diz e, mais que isso, o pedido que não chega.
- **O que vale sem rede** (E4): marcar, desmarcar, "Desmarcar tudo" e acrescentar item. O editor deixa as
  linhas que já estavam só de leitura e o título também; o item novo se escreve. O cliente confere de novo
  (`soMarcouOuAcrescentou`: as linhas que não são de marcar iguais e na mesma ordem, e todo item de antes
  ainda lá). Criar nota, mudar o texto, a cor, os marcadores, fixar, arquivar, lixeira, mover e ditar ficam
  apagados ou recusam com o aviso da prévia. A faixa "Sem rede · N mudanças esperando" fica no alto da aba,
  e o cartão diz "↑ esperando a rede".
- **[desvio] A busca única (N6) não vai para o celular.** A busca do histórico da nuvem não ignora acento
  (`ILIKE`), e juntar as duas faria "reuniao" achar a nota e não o ditado (o erro que a N6 evitou). No
  celular, a aba Notas busca nas notas ("Buscar nas notas", sem acento, no aparelho), e o Histórico nos
  ditados, como hoje. Para juntar, a busca da nuvem tem de ignorar acento antes (um passo à parte).
- **[derivada] A conversão com IA (N5) também não**: ela usa o Polish e a chave do PC, pelo Agent. No
  celular, o Formatar separa os itens pela regra (a mesma vírgula, ponto e vírgula e último " e "). O
  "nota em foco" do PC (o ditado do Agent na nota aberta) não existe no celular: o microfone é do próprio app.
- **O expurgo da lixeira das notas na nuvem, 30 dias** (o que a N7 deixou para cá): o job de retenção apaga
  de vez a nota com o `purge_at` vencido, decidida de novo sobre a linha travada (a restaurada no meio
  fica), com a lápide e o evento `tombstone` sem aparelho; os PCs apagam a cópia pelo pull. As lápides das
  notas continuam sem vencer. A linha `retenção` do log ganha a contagem `notas`.
- **Arrastar com o dedo:** o gesto da N2b vale igual no celular (o mesmo componente; o `move` da nuvem
  calcula a chave com a mesma `notas.PosicaoEntre`). O que falta é só a conferência num celular de verdade
  (o roteiro).
- **Privacidade:** o texto das notas passa a ficar **no celular** (o IndexedDB da conta), além do PC e da
  nuvem. Nunca o ditado nem o áudio (o `bundle.test.ts` prende: só o banco das notas usa o IndexedDB). O
  Sair e o "Apagar meus dados" o apagam; o Sair com mudança esperando a rede pergunta antes. `09`, "Notas
  no celular".
- **Riscos abertos:** o áudio de um "Ditar para nota" que espera "Tentar de novo" não segura a tela quando
  a sessão acaba (só o do microfone do histórico conta como texto não salvo): se a sessão vencer bem nessa
  hora, a fala se perde. Duas abas do app no mesmo celular, as duas sem rede mexendo na mesma nota, dividem
  o banco: a última a gravar a entrada da fila vale. O IndexedDB é "melhor esforço" do navegador (ele pode
  apagar sob falta de espaço); o app não pede o armazenamento persistente. O Postgres não rodou nos testes
  desta fase (o Docker estava fechado): os testes de contrato com Postgres estão escritos e rodam no
  deploy.

### Imagens (fase N10, por último)

Anexar imagem à nota (o ícone do Keep). Desenho prévio, a confirmar quando a fase chegar:
- no PC, o arquivo em `<dados>\notas-imagens\`, até 10 MB por imagem, reduzida para no máximo
  2.000 px no lado maior;
- na nuvem, no disco da VPS (volume próprio do compose), entrando no backup cifrado;
- **ponto de parada:** volume novo no servidor, espaço em disco e a revisão de LGPD (foto de
  documento de cliente é dado pessoal) precisam do OK do Mauro antes da N10.

**[mudou em 28-out-2026]** O ponto de parada foi feito: o Mauro respondeu às quatro perguntas e aprovou a
prévia (abaixo). Na nuvem, as imagens ficam **no banco**, e não no volume do desenho prévio.

### As imagens (N10): as decisões e a prévia aprovada (28-out-2026)

**As quatro perguntas da parada (Mauro, 28-out-2026):**
- **Na nuvem, no banco** (Postgres), e não num volume próprio. Por quê: as imagens entram sozinhas no backup
  cifrado (o `pg_dump` de todo dia), na restauração e no "Apagar meus dados", na mesma transação da nota; a
  API continua `read_only`, sem volume novo nem backup novo. O preço: um **teto para o total de imagens da
  conta**, porque a restauração abre o dump decifrado na memória só do usuário da VPS (`/run/user/<uid>`,
  `deploy/scripts/restaurar.sh`), e as imagens quase não comprimem. O número sai do tamanho dessa pasta na
  VPS (o Mauro roda o comando que só lê); até lá, o código usa um valor provisório, numa constante só.
  Alternativa: o volume no disco (o desenho prévio), sem o teto, mas com volume novo, a API gravando em
  disco e um backup e uma restauração só das imagens.
- **O celular anexa**, pela câmera ou pela galeria, **só com rede** (a foto sobe direto para a nuvem e de lá
  chega ao PC).
- **No celular, a imagem não fica guardada para abrir sem rede.** Sem rede, no lugar dela aparece "Imagem sem
  rede"; a que já abriu na visita continua aparecendo enquanto a página não fecha (só na memória). Nem o
  IndexedDB nem o cache do navegador guardam a imagem.
- **Até 10 imagens por nota.**

**A prévia (`docs/notas-previa-n10.html`), aprovada pelo Mauro em 28-out-2026 ("ficou maravilhoso!
Aprovado!!!"), com as cinco escolhas do fim dela:**
- **A imagem no topo do cartão e da nota, em grade, como no Keep:** uma, de ponta a ponta; duas, lado a lado;
  três ou mais, a primeira larga e duas embaixo, com "+N" no último quadrado. Uma nota pode ser só a imagem,
  sem texto (o cartão mostra só ela). O campo "Criar uma nota…" ganha o ícone de imagem (nota nova com
  imagem).
- **No PC, colar com Ctrl+V e arrastar o arquivo** para dentro da nota aberta ("Solte para anexar à nota"),
  além do ícone de imagem da barra (que deixa de estar apagado). A imagem nova mostra "guardando…" até ficar
  guardada.
- **A imagem grande:** o clique abre sobre a tela escura (escura nos dois temas), com "3 de 5", as setas para
  as outras da nota, **Baixar** (a cópia reduzida, a única que existe), **Tirar da nota** e fechar (Esc).
- **Tirar da nota** (o × sobre a imagem, ou o botão da imagem grande) tem **Desfazer** por alguns segundos;
  depois, a imagem sai de vez, no PC e na nuvem. Não há lixeira só de imagens: a imagem vai para a lixeira,
  e volta dela, junto com a nota inteira, e sai de vez com ela (os 30 dias, ou o "Apagar para sempre").
- **O teto** para o total de imagens (acima).

**Os padrões que valem sem pergunta** (ditos ao Mauro na parada, sem objeção):
- **Reduzir no navegador**, antes de guardar: no máximo 2.000 px no lado maior, em JPEG, respeitando a
  orientação da foto. Isso tira a localização (GPS) e o resto dos metadados que a foto do celular carrega; o
  Agent e o servidor conferem (só aceitam JPEG que decodifica, dentro do tamanho, e guardam sem os
  segmentos de metadados). Só a versão reduzida é guardada; o original nunca sai do aparelho. Arquivo
  original acima de **10 MB** é recusado.
- **A imagem segue a nota:** sobe à nuvem sob a mesma permissão das notas ("Enviar o histórico"), vai e
  volta da lixeira com ela, e sai no "Apagar meus dados".
- **Nunca vai à IA** (nem ao Polish, nem ao ASR) e **não entra na busca** (sem OCR). O "Copiar para
  WhatsApp" continua só com o texto.
- **Os avisos** (a cena 7): "Imagem tirada da nota." com Desfazer; "A imagem passou de 10 MB."; "A nota já
  tem 10 imagens."; "Esse arquivo não é uma imagem que o navegador abra."; "As imagens das notas chegaram ao
  limite de [teto]. Apague algumas para anexar outras."; "A rede caiu antes de a imagem subir. Tente de
  novo."

### Decisões da N10 (implementação)

A N10 foi feita em duas metades. **A primeira (28-out-2026, branch `notas/n10a`) é a do PC**: o pacote que
confere a imagem, o SQLite, a pasta das imagens, a API local e o painel do PC. **A segunda é a do sync, da
nuvem e do celular**, de outro trabalho, depois desta; o que ela precisa saber está no fim. As marcadas
**[desvio]** mudam algo escrito acima (ou na prévia); as **[derivada]** são escolhas desta implementação.
Roteiro do Mauro: `docs/teste-manual-notas-n10.md`.

- **O teto do total: 400 MB** (o Mauro escolheu em 28-out-2026, no lugar do valor provisório). Por quê: a
  restauração abre o dump decifrado em `/run/user/<uid>` da VPS, um tmpfs de 795 MB, e as imagens quase não
  comprimem; com 400 MB de imagens, o dump fica em uns 450 MB, pouco mais da metade, com folga para o resto do
  banco crescer. Mora numa constante só, `imagemdanota.TetoDoTotal`, que o PC já usa e o servidor vai usar.
  Contam as imagens de todas as notas da pessoa, as da lixeira também (elas voltam com a nota). A frase do
  aviso é montada com o número (`imagemdanota.FraseDoTeto`) e chega ao painel pronta, na resposta
  `IMAGES_FULL`.
- **A conferência, num pacote puro** (`internal/imagemdanota`, só a biblioteca padrão), para o servidor reusar:
  só JPEG; até **4 MB** a cópia reduzida e **2.000 px** no lado maior; o cabeçalho lido com `DecodeConfig`
  **antes** de decodificar (a bomba de descompressão é recusada sem alocar a imagem); a decodificação
  inteira da original e da limpa; e os bytes devolvidos **sem APP1 a APP15, sem COM e sem nada depois do EOI**
  (o vídeo da "foto em movimento"), sem recomprimir (os dados comprimidos saem byte a byte). **[derivada]** Só
  as cores YCbCr e cinza (o que o canvas grava): o CMYK e o RGB da Adobe dependem do APP14 para as cores, e
  tirá-lo as trocaria. A estrutura é lida de forma estrita (um byte solto entre segmentos é recusado) e com
  até 100 varreduras. Testes de tabela, o da foto com EXIF e localização, e o alvo de fuzz `FuzzConferir`.
- **No PC, a linha e o arquivo** (migration `0014`, `local_note_images`: id, nota, largura, altura, bytes,
  sha256, created_at). Os bytes ficam em `<dados>\notas-imagens\<id>.jpg` (a tabela de caminhos do Agent,
  `cam.ImagensDasNotas`), gravados com temporário e rename **dentro** da transação que insere a linha: com a
  linha, o arquivo existe. O SHA-256 é conferido a cada leitura. Apagar é a linha na transação e o arquivo
  depois do commit. **A varredura** da subida (antes do expurgo e do painel) tira o arquivo sem linha, a linha
  sem arquivo e o temporário que sobrou, com um WARN só de contagens; a rodada do expurgo, a cada hora, tira o
  arquivo sem linha (a nota que a lápide da nuvem apaga pela `Fila`, que não conhece a pasta).
- **[derivada] Anexar e tirar não mudam a versão nem a hora de edição da nota.** A imagem é uma linha à parte,
  imutável (o id nasce no PC e não muda; não há "editar imagem"). Por quê: a janela aberta salva o texto com a
  `base_version`, e o cartão faz o PATCH da cor e do fixar com a versão que tem; anexar subindo a versão daria
  `409` à toa em quem não mexeu no texto, e o "Tirar da nota" que vence depois de a janela fechar deixaria o
  cartão com a versão velha. Alternativa: subir a versão e adotar a nova na janela, como o marcador (N4).
- **[desvio da N3] O Desfazer do "Virar nota" deixa a nota que ganhou imagem.** Ele apagava a nota "na versão
  1"; como anexar não sobe a versão, a regra passa a ser "na versão 1 **e sem imagem**" (a pessoa já mexeu na
  nota).
- **Tirar da nota, com o Desfazer** (a prévia): a imagem some da tela na hora, e o `DELETE` só vai ao Agent
  quando o aviso "Imagem tirada da nota." sai **sem** o Desfazer: o tempo da pílula (os 8 s de sempre, parados
  com o ponteiro ou o foco nela), outro aviso no lugar dela, ou a página fechando (`pagehide`, com
  `keepalive`). O `avisar` do Painel ganhou o `aoVencer` para isso. **[derivada]** O tirar vale também na nota
  da lixeira (o aviso pode vencer depois de a nota ir para lá); anexar, não (`409`).
- **[derivada] A nota só com imagem nasce de uma rota própria**, `POST /api/notas/com-imagem` (a nota e a
  imagem numa transação): o ícone de imagem do "Criar uma nota…" cria a nota com a primeira imagem escolhida,
  a põe no topo e a abre; as outras escolhidas entram nela. A nota sem texto só vale com imagem (o `POST
  /api/notas` continua recusando a nota vazia).
- **A redução no navegador** (`web/src/notas/reduzirImagem.ts`): `createImageBitmap` com `imageOrientation:
  'from-image'`, canvas com fundo branco, 2.000 px no lado maior, `toBlob('image/jpeg', 0.85)`; **[derivada]**
  se passar de 4 MB, 0,72 e 0,6 antes de recusar, com a frase a mais "A imagem ficou grande demais mesmo
  reduzida." O original acima de 10 MB e o que o navegador não abre (o HEIC, o PDF) são recusados antes de
  qualquer pedido. Só a cópia reduzida sai; o nome do arquivo nunca.
- **[derivada] O colar:** o Ctrl+V anexa quando o que se colou é **só** imagem (o print do Win+Shift+S); com
  texto junto (a célula do Excel, o trecho do Word), o colar é o de sempre. **O arrastar:** só um arrastar de
  arquivos mostra "Solte para anexar à nota"; o arquivo solto fora da nota (no véu) não faz nada, para o
  navegador não trocar o painel pela imagem.
- **[derivada] Na tela:** o cartão inteiro continua sendo o botão que abre a nota (a imagem grande abre de
  dentro da nota); o × fica só na nota aberta (como a prévia). A primeira imagem do cartão tem a proporção dela,
  contida entre 3:4 e 2:1 (o print de celular não estica o cartão). Na imagem grande, as setas dão a volta, e o
  Baixar grava `nota-imagem-N.jpg`. A nota que só tem imagem se chama "Nota com imagem" (ou "com N imagens").
  A forma "lista" e a Lixeira contam as imagens. A imagem do cartão só é lida quando ele chega perto da tela
  (600 px de folga), e o cartão que muda de altura sozinho (a imagem que entra, sai ou volta no Desfazer) pede a
  medida da grade de novo (o `ResizeObserver` da ordem manual, N2b, passou a olhar cada cartão).
- **A imagem chega à tela** por `fetch` com o token (`GET /api/notas/{id}/imagens/{imagem}`), vira uma URL
  `blob:` guardada na loja da aba (`web/src/notas/imagensNaTela.ts`) e é solta quando a aba sai. A CSP do painel
  passou a `img-src 'self' data: blob:`. **[derivada]** A URL `blob:` nasce **só no cliente do PC**
  (`ClienteDeImagens.paraATela`, em `clienteLocal.ts`): o build da nuvem ainda não mostra imagem, e o
  `bundle.test.ts` prende que ele não cria URL de blob (a garantia do áudio do ditado). O app do celular segue
  sem imagens nesta metade: o `ClienteDeNotas.imagens` é opcional, e sem ele a aba não mostra nem anexa.
  **[desvio, N10b]** Superada: a nuvem também cria a URL `blob:` (`nuvem/notas/urlDaImagem.ts`), com a garantia
  do áudio refeita no `bundle.test.ts` (ver "Decisões da N10b (o celular)").
- **Nada da imagem vai à IA nem à busca**: a busca (`q`), o "Copiar texto", o "Copiar para WhatsApp", o
  Formatar e a sugestão de marcador só olham o título e o corpo. O log nunca leva os bytes, o tamanho nem o nome.
- **Riscos desta metade:** (1) com a nuvem conectada, a nota só com imagem sobe (N7) como uma nota de título e
  corpo vazios, e no celular aparece "Nota vazia" até a segunda metade levar as imagens; (2) a aba derrubada
  sem `pagehide` (o Agent ou o navegador que caem) com o aviso "Imagem tirada da nota." na tela deixa a imagem
  na nota (nada some calado; a pessoa tira de novo); (3) a miniatura que o Explorador do Windows guarda, se a
  pessoa abrir a pasta `notas-imagens` nele, fica no cache do Windows, fora do alcance do Agent; (4) a
  conferência visual (a grade, a imagem grande, o arrastar de verdade) é do roteiro manual: os testes do painel
  rodam no jsdom, com dublês do canvas; (5) a aba guarda na memória as imagens que já mostrou (a cópia de até
  2.000 px, que serve também de miniatura do cartão) até sair dela, sem descartar as mais antigas: com centenas
  de notas com imagem rolando na grade, isso pesa (uma miniatura própria, ou um descarte das mais antigas, fica
  para quando fizer falta).

**O que a segunda metade (o sync, a nuvem e o celular) precisa saber:**
- **Os nomes.** No PC: a tabela `local_note_images`; o tipo `storage.ImagemDaNota` (e `Nota.Imagens`); o
  contrato `storage.ImagemDaNotaRepository` (`AnexarImagem`, `CriarNotaComImagem`, `LerImagem`, `TirarImagem`),
  cumprido pelo `*storage.SQLiteNotaRepository`, com `UsarPastaDasImagens` e `VarrerImagens`; as rotas
  `POST /api/notas/com-imagem`, `POST /api/notas/{id}/imagens`, `GET` e `DELETE
  /api/notas/{id}/imagens/{imagem}`; os códigos `IMAGE_INVALID`, `IMAGE_TOO_LARGE`, `TOO_MANY_IMAGES`,
  `IMAGES_FULL`. No painel: `ImagemDaNota` e `ClienteDeImagens` (`web/src/notas/tipos.ts`), a loja
  `ImagensNaTela`, a redução `reduzirImagem`.
- **O teto e os limites** moram em `internal/imagemdanota` (`MaxBytes`, `MaxLado`, `MaxPorNota`,
  `TetoDoTotal`, `FraseDoTeto`); o servidor confere com o mesmo `Conferir` antes do Postgres. O PC deve aceitar
  a imagem que vem da nuvem mesmo acima do teto local (quem decide o teto é a nuvem).
- **O que já pensa no sync:** o id é um UUID v7 gerado no PC (a nuvem pode guardá-lo como está); a imagem é
  imutável (só criar e apagar, como uma entidade à parte da nota); o SHA-256 vai junto para conferir o que
  chegou; anexar e tirar não sobem a versão da nota (a segunda metade decide se a imagem viaja como entidade
  própria, que é o que o desenho pede, ou se mexe na versão); a ordem é a de `created_at`. **A tabela não tem
  `server_version` nem `sync_state`**: entram por `ALTER TABLE ... ADD COLUMN`, como na `0013`. Nada desta
  metade põe marcador na `sync_outbox`.
- **A ordem das gravações que a varredura supõe:** o arquivo antes da linha (com a linha, o arquivo existe). A
  imagem que desce da nuvem deve fazer o mesmo (baixar, conferir, gravar o arquivo, inserir a linha); e a linha
  sem arquivo é apagada na subida, então uma imagem "à espera de baixar" precisa de um estado próprio que a
  varredura respeite. A lápide da nuvem que apaga a nota pela `Fila` já leva as linhas (`apagarNotaDaTabela`),
  e o arquivo sai na rodada do expurgo.
- **A imagem chega ao painel** por `ClienteDeImagens.ler` (os bytes) e `paraATela`/`soltar` (a URL `blob:`). O
  celular precisa do próprio `ClienteDeImagens` (a `/api/v1`), da redução no aparelho (a mesma
  `reduzirImagem`) e de: mudar o `bundle.test.ts` ("a URL blob: só no cliente do PC") e a CSP da nuvem (que hoje
  não tem `img-src blob:`). O "Imagem sem rede" da prévia (cena 6) é dela.
- **O Desfazer do tirar** já é do painel (o `aoVencer`), e serve ao celular igual.

### Decisões da N10b (o sync e a nuvem)

A segunda metade da N10 foi feita em duas partes. **Esta (28 e 29-out-2026, branch `notas/n10b`) é a do Go**:
o Postgres, o servidor, o motor do sync do PC e a API local. **A parte do celular** (a tela, a CSP da nuvem, o
`bundle.test.ts`, a política e o dossiê) vem depois, por cima desta; o que ela precisa saber está no fim. As
marcadas **[desvio]** mudam algo escrito acima; as **[derivada]**, escolhas desta implementação. O deploy é
ponto de parada: o roteiro do Mauro é o `docs/deploy-notas-n10.md`.

- **[derivada] A imagem é uma entidade própria do sync, `note_image`, imutável** (só nasce e sai), como o
  desenho da N10a pedia. Anexar e tirar continuam sem subir a versão da nota, na nuvem também. O id é o
  UUID v7 de onde a imagem foi anexada, guardado como veio. Por quê: a imagem não tem campo que mude, e
  levá-la na versão da nota daria conflito de conteúdo onde não há. Alternativa: um campo `imagens` na
  mutação da nota, com a versão (o desenho do marcador, N4).
- **[derivada] Os bytes nunca vão no push nem no pull.** O PC sobe pelo `PUT /api/v1/sync/notes/{nota}/
  images/{imagem}` (o JPEG cru, com o `Imagem-SHA256` da cópia que ele guardou e o `Imagem-Criada-Em`),
  baixa pelo `GET` e tira pelo `DELETE` do mesmo caminho, com o token do aparelho. O push recusa
  `note_image` (`tipo_desconhecido`). O pull leva só os metadados: a mudança `note_image` (upsert com `id`,
  `note_id`, `largura`, `altura`, `bytes`, `sha256`, `created_at`; a lápide como `tombstone`, sem payload) e a
  lista `imagens` de cada nota. Por quê: o push é um lote JSON de até 1 MiB, e uma página do pull já tem o
  orçamento de texto; 4 MB por imagem num deles prenderia o resto. Alternativa: base64 no push (33% a mais, e
  o teto do corpo do push).
- **[derivada] O PUT é idempotente e não ressuscita.** O mesmo id com os mesmos bytes (o SHA-256 da cópia
  limpa) é `200` com o que está gravado, sem gastar seq, e é respondido **antes de ler o corpo** quando o
  `Imagem-SHA256` já bate (o reenvio não gasta a banda de novo); o mesmo id com outros bytes, ou de outra
  nota, é `409 IMAGE_CONFLICT`; o SHA-256 que não é o da cópia limpa, `400 IMAGE_SHA_MISMATCH`. **A lápide fica
  na própria linha** (`removed_at`, sem os bytes): o PUT atrasado de um id tirado é `410 IMAGE_GONE`, e o
  `DELETE` de um id que a nuvem nunca teve, numa nota que existe, grava a lápide do mesmo jeito (o PUT que
  ainda estava na rede chega depois e recebe `410`). A lápide sai com a nota (a cascata), e a da nota cobre o
  PUT atrasado de qualquer imagem dela (`410`). Alternativa: uma tabela de lápides à parte, sem a nota (uma
  tabela a mais, e o "Apagar meus dados" a mais).
- **[derivada] Uma tabela só, a `0026` (`note_images`).** Os bytes em
  `bytea` com `STORAGE EXTERNAL` (fora da linha, sem a tentativa de compressão, que num JPEG só gasta CPU;
  Postgres 16 ou mais novo), a chave estrangeira `(user_id, note_id) → notes` com `ON DELETE CASCADE`, o
  `last_seq` único por usuário (o pull lê as imagens que mudaram depois do cursor, como as notas) e a lápide
  na linha. **Sem tabela de eventos:** a mudança de imagem não tem aparelho de origem no pull (o `device_id`
  da mudança vem nulo; ele só serve para mostrar de onde veio). Os limites (2.000 px, 4 MB, 10, o teto) ficam
  em Go, como na `0014` do PC.
- **As regras, puras** (`cloudstore/imagens.go`): `DecidirAnexo` e `DecidirTirar`, as mesmas no Store em
  memória e no do Postgres, testadas em tabela. **Anexar:** a nota que não existe é `404` (com a lápide dela,
  `410`); **na lixeira, `409 CONFLICT`** (como no PC: restaure antes); 10 na nota, `409 TOO_MANY_IMAGES`; o
  total passaria de 400 MB, `409 IMAGES_FULL` com a `FraseDoTeto`. **O teto conta as imagens vivas de todas as
  notas da conta, as da lixeira também**, e não conta as lápides. **Tirar** vale na nota da lixeira e é
  idempotente (`204`). A contagem e a soma são lidas depois da trava do contador do usuário: dois anexos ao
  mesmo tempo da mesma conta não passam os dois.
- **[derivada] A hora do anexo é a do PC** (`Imagem-Criada-Em`), dentro da folga para o futuro (24 h; além
  dela, a de agora): a ordem das imagens na nota é a mesma em todo aparelho. No celular, a do servidor.
- **[derivada] O servidor confere antes do Postgres e com teto próprio.** Os bytes passam pelo mesmo
  `imagemdanota.Conferir` do Agent **fora da transação**, e o servidor guarda a cópia limpa. Só as rotas que
  sobem imagem têm o teto de `MaxBytes` (4 MB; o Conferir recusa o que passa disso, então não há folga a dar),
  e o prazo de 2 min para o corpo chegar (o `ReadTimeout` de 30 s continua o de todo o resto); as de JSON
  continuam em 1 MiB. **Duas vagas** de leitura ao mesmo tempo (`VagasDeImagem`): o corpo na memória e as
  duas decodificações do Conferir são até uns 30 MB por imagem, e o contêiner da API tem 256 MB. O `GET` dos
  bytes leva `Cache-Control: no-store` (a decisão do Mauro: o celular nunca guarda a imagem) e
  `X-Content-Type-Options: nosniff`, e **tira os bytes da resposta do log de acesso** (seriam o tamanho da
  imagem).
- **[derivada] As rotas do celular, pela sessão:** `POST /api/v1/notes/{id}/images` (o anexo; **o id é do
  servidor**, como o `POST /api/notas/{id}/imagens` do PC, que o mesmo painel já fala), `POST /api/v1/notes/
  with-image` (a nota nova só com a imagem, o ícone do "Criar uma nota…"), `GET` e `DELETE /api/v1/notes/
  {id}/images/{imagem}`. Por quê o id do servidor: o celular só anexa com rede e com a pessoa olhando; a
  resposta que se perde no meio pode deixar a imagem duas vezes, visível e removível, e nunca some nada.
  Alternativa: o id do app (o `uuidV7` do ditado), com o PUT idempotente do PC (fica para se fizer falta).
  O token de um PC nessas rotas recebe `403`, e a sessão nas rotas do sync também.
- **A nota no fio ganha `imagens`** (sempre um array, na ordem da nota, só os metadados das vivas), no pull,
  no `GET /notes`, no `GET /notes/{id}` e nas respostas das escritas. O PC de antes da N10b ignora a chave.
- **[derivada] A capacidade `note_image`** entra no `entidades` do pull. O PC só sobe e baixa imagem depois de
  vê-la (a chave `nuvem_imagens`), e na primeira vez **lê tudo de novo uma vez** (o cursor volta a 0, como na
  N7): um Agent de antes da N10b passou pelas mudanças `note_image` sem gravá-las (para ele são "não
  aplicadas", e o cursor andou), e as notas voltam com a lista `imagens`.
- **No PC, a migration `0015` (só colunas, por `ALTER TABLE ... ADD COLUMN`, como a `0013`):** em
  `local_note_images`, `na_nuvem`, `sync_state` (`pending`, a situação normal de todas as tabelas do sync;
  `aguardando`; `recusado`), `last_sync_error`, `tentativas` e `tentou_em`; e **em `sync_outbox`, `nota_id`**
  (o caminho do `DELETE` da imagem que já saiu da tabela). **[desvio da N10a]** O "o que a segunda metade
  precisa saber" falava em `server_version`; entrou **`na_nuvem`**: a nuvem tem, **ou pode ter**, a imagem
  (veio dela, ou este PC já mandou o PUT pelo menos uma vez, **gravado antes da rede**). Por quê: a imagem
  não tem versão (é imutável), e é isso, e não o envio em voo, que decide se tirar a imagem manda o
  `DELETE`: o envio em voo é esquecido na reconexão e no recomeço, e o PUT que valeu sem resposta deixaria a
  imagem na nuvem (e ela voltaria ao PC pelo pull). E a coluna na `sync_outbox` (e não só na tabela das
  imagens) porque a lápide local não tem mais a linha da imagem para dizer a nota. Alternativa: manter a
  linha da imagem tirada até a nuvem confirmar (um estado a mais em toda leitura das imagens).
- **A emissão:** anexar com a nuvem conectada põe o marcador de criar na mesma transação (como a nota da N7);
  tirar põe a lápide (`CampoApagado`, com a nota) se `na_nuvem`, senão tira o marcador; a nota que sai de vez
  (a lixeira de 30 dias, o "Apagar para sempre", a lápide da nuvem) leva os marcadores das imagens dela.
  **O PUT só sai se a imagem ainda está lá** (correção da revisão, 29-out-2026): o motor lê as imagens da
  rodada de uma vez e as sobe uma depois da outra; a que a pessoa tira da nota nesse meio-tempo, sem
  `na_nuvem`, sai sem lápide, e o PUT dela (com os bytes já lidos) deixaria a imagem na nuvem, de onde o pull
  a traria de volta. O `MarcarEnvioDaImagem` grava `na_nuvem` só se a linha e o marcador de criar ainda
  existem, e o motor pula a imagem quando não gravou.
- **[derivada] A ordem:** a fase das imagens vem **depois do push** (a nota criada no lote já tem a versão da
  nuvem) e antes do pull; a imagem cuja nota ainda não tem `server_version` (o criar dela ainda na fila, ou
  recusado) **espera a rodada seguinte**, com o marcador, sem pedido nem erro; a da nota da lixeira daqui
  também (sobe se a nota voltar). Até 20 envios e 20 downloads por rodada.
- **[derivada] O que cada resposta do envio faz** (`DecidirEnvioDaImagem`, puro): `200` e `201`, subiu; `410`,
  o marcador sai (a lápide, ou a da nota, vem pelo pull e tira a daqui); `IMAGE_INVALID`, `IMAGE_TOO_LARGE`,
  `IMAGE_SHA_MISMATCH`, `IMAGE_CONFLICT`, **recusada de vez**: a linha fica `recusado` e a imagem fica **só
  neste PC** (nada some); o resto (`TOO_MANY_IMAGES`, `IMAGES_FULL`, a nota da lixeira na nuvem, a nota que a
  nuvem não tem, um 5xx), **espera e tenta de novo**. O `DELETE` `204`, `404` ou `410` termina a lápide.
- **[derivada] A espera é por imagem e não segura o sync.** `EsperaDasImagens`, a `BackoffPolicy` do sync
  com outra escala (de 1 min a 6 h, sem o jitter, contada de `tentou_em`), porque cada tentativa pode custar
  4 MB. Uma imagem que não sobe ou não baixa **nunca falha a rodada**: só o `401` (o token caiu) a derruba; a
  rede que cai e o `429` só param a fase, e a rodada seguinte continua. Por quê: um arquivo estragado, ou uma
  recusa da nuvem, não pode prender o push e o pull de todo o resto. A bandeja continua "em dia" com uma
  imagem esperando (o log da rodada a conta).
- **[derivada] A imagem que vem da nuvem** (a mudança `note_image`, ou a lista `imagens` de uma nota que
  chegou depois dela) **nasce `aguardando`**: a linha com os metadados e sem o arquivo, que **a varredura da
  subida respeita**. O download (`GET`) confere o tamanho, o SHA-256 da linha e o `Conferir` (a cópia que o
  servidor guardou já é a limpa, e conferir de novo a deixa igual: `TestConferir_ALimpaPassaIgual`), grava o
  arquivo com temporário e rename **dentro** da transação e só então põe a linha normal. `404` e `410` tiram a
  linha que espera; o resto espera a vez. **O PC aceita a imagem da nuvem acima do teto local** (quem decide
  o teto é a nuvem): ela entra e baixa, e o teto daqui passa a contá-la. A lápide da nuvem tira a linha na
  página do pull, e o arquivo sai logo depois (a varredura sem linhas), e não só na rodada do expurgo.
- **[derivada] O eco:** a imagem daqui que o pull mostra com o mesmo SHA-256 (o PUT cuja resposta se perdeu)
  ganha `na_nuvem`, e o marcador de criar sai; a imagem tirada aqui (a lápide na fila) não volta.
- **O envio inicial:** as imagens anexadas antes desta versão (a N10a não punha marcador) entram na fila pela
  completação de cada rodada (`CompletarImagens`), **sob o mesmo aceite das notas** ("Enviar o histórico") e
  com a capacidade: toda imagem com o arquivo aqui que a nuvem nunca viu, de nota fora da lixeira. É o que
  leva a imagem que o Mauro já anexou.
- **O recomeço** (o epoch novo) põe de volta na fila as imagens que a nuvem tinha ou podia ter (o PUT
  idempotente responde `200` sem ler o corpo se a nuvem restaurada já as tem); as que esperam o download
  continuam esperando. **O "Esquecer esta nuvem"** tira as que esperavam (sem arquivo, e sem a nuvem de onde
  viriam) e zera o resto.
- **[derivada] Na API local:** o item da nota leva `"estado": "a_caminho"` na imagem que ainda não baixou (a
  presente não leva a chave), e o `GET` dos bytes dela é **`409 IMAGE_PENDING`**, não um `404` qualquer.
  Tirar da nota vale do mesmo jeito (sobe o `DELETE`). O painel do PC que mostra o lugar dela é da parte do
  celular.
- **O "Apagar meus dados"** apaga as imagens da conta (o `DELETE FROM note_images` antes das notas, e a
  cascata como segunda guarda); **a restauração** traz as do backup, como o resto; **o job de retenção** não
  muda (o expurgo das notas leva as imagens pela cascata).
- **O log** nunca leva os bytes, o tamanho de uma imagem, o id, o SHA-256, um nome de arquivo nem o texto da
  nota: o PC conta por tipo (`imagens_subiram`, `imagens_baixadas`, `imagens_esperando`…), e o log de acesso
  da nuvem tem o padrão da rota.
- **Riscos desta parte:** (1) **o proxy da VPS**: o Traefik que já roda não é nosso; sem um middleware de
  buffering, ele não limita o corpo, mas o `readTimeout` padrão do entrypoint no Traefik v3 é de 60 s, e uma
  imagem de 4 MB por uma rede de menos de ~0,55 Mbit/s não sobe (fica esperando a vez; nada se perde). O
  roteiro do deploy confere de fora; (2) **o tamanho do backup e da restauração** cresce com as imagens (elas
  quase não comprimem): o teto de 400 MB por conta foi escolhido pelo tmpfs da restauração, mas o volume do
  backup guarda 3 dias de dumps, e o B2, 30; (3) a resposta do anexo do celular que se perde pode deixar a
  imagem duas vezes (visível, removível); (4) a imagem recusada de vez fica só neste PC, sem aviso na tela
  (só a linha `recusado`); (5) voltar o Agent para a versão de antes com imagens a caminho deixa a varredura
  dela apagar essas linhas (sem arquivo); de volta à N10b, elas só voltam com o recomeço ou um "Esquecer";
  (6) duas imagens anexadas no mesmo microssegundo, em aparelhos diferentes, ficam na ordem do id.

**O que a parte do celular precisa saber:**
- **As rotas do celular** (sessão; o token de um PC é `403`):
  - `POST /api/v1/notes/{id}/images`: o corpo é o JPEG cru (`Content-Type: image/jpeg`), já reduzido pela
    `reduzirImagem`; `201` com a imagem: `{"id", "note_id", "largura", "altura", "bytes", "sha256",
    "created_at"}`. O id é do servidor.
  - `POST /api/v1/notes/with-image`: o mesmo corpo; `201` com a nota (o `noteDTO` de sempre, sem título nem
    corpo, no topo das ativas, com `imagens` de um item).
  - `GET /api/v1/notes/{id}/images/{imagem}`: os bytes (`image/jpeg`, `Cache-Control: no-store`), também da
    nota da lixeira; `404 NOT_FOUND` sem a imagem (tirada, de outra nota, ou de outra conta).
  - `DELETE /api/v1/notes/{id}/images/{imagem}`: `204`, idempotente (tirar de novo é `204`), vale na nota da
    lixeira; `404` só sem a nota.
- **Os erros** (o JSON `{"error": {"code", "message"}}` de sempre): `415 IMAGE_INVALID` (o Content-Type),
  `400 IMAGE_INVALID` (não é um JPEG que se abra, ou passa de 2.000 px), `413 IMAGE_TOO_LARGE` (mais de 4 MB),
  `409 TOO_MANY_IMAGES` (a nota com 10), `409 IMAGES_FULL` (o `message` é a `FraseDoTeto`, pronta para a tela),
  `409 CONFLICT` (a nota na lixeira), `404 NOT_FOUND` (a nota). Os códigos são os mesmos da API local, e as
  frases da cena 7 já estão no `imagens.ts`.
- **A nota** (`GET /notes`, `GET /notes/{id}`, as respostas das escritas) traz `imagens`: sempre um array, na
  ordem da nota, com os mesmos campos da imagem acima. No PC (a API local), o item tem `estado: "a_caminho"`
  enquanto não baixou, e os bytes dela são `409 IMAGE_PENDING`: é o "a caminho" do painel do PC.
- **O que faltava (era dela, e está feito, abaixo):** o `ClienteDeImagens` do celular sobre estas rotas; a
  CSP da nuvem com `img-src blob:` e o `bundle.test.ts` (a URL `blob:` no cliente da nuvem); o "Imagem sem
  rede" (cena 6) e o ícone apagado sem rede; o lugar da imagem "a caminho" no painel do PC; a política de
  privacidade, o dossiê e os textos do app.

### Decisões da N10b (o celular)

A tela das imagens no app do celular e o que ficou da N10b no painel do PC (29-out-2026). Valem as decisões
do Mauro da prévia aprovada (cenas 5, 6 e 7); o que segue é o que a implementação decidiu por cima delas.
O Mauro quer "o menor que funciona".

- **O `ClienteDeImagens` do celular** (`web/src/nuvem/notas/notasNaNuvem.ts`) fala as quatro rotas da
  `/api/v1`: anexa pelo `POST /notes/{id}/images` (o JPEG cru, já reduzido no navegador pela mesma
  `reduzirImagem` do PC), cria a nota com a imagem pelo `POST /notes/with-image`, lê pelo `GET` e tira pelo
  `DELETE` (com `keepalive`, para o pedido do Desfazer vencido sair mesmo se a aba fechar). **Anexar e criar
  exigem rede** (o `exigirRede`): sem ela é um `SoComRede`, e nada vai para a fila offline (a fila é só do
  texto da nota). O `fraseDoErro` ganhou o `SoComRede` e usa os mesmos códigos e frases do PC; o `IMAGES_FULL`
  mostra a frase do servidor. A tela é **a mesma `Notas` do PC**: o que muda é o `celular` que ela já recebia.
- **A barra da nota e o "Criar uma nota…"** (cena 5): o ícone de imagem abre um menu com **Tirar foto** (um
  `<input type="file" accept="image/*" capture="environment">`) e **Escolher da galeria** (sem `capture`, com
  `multiple`; entram uma por vez, até as 10). No PC o ícone continua um botão só, que abre a escolha de
  arquivos (o `BotaoDeImagem`). A ordem da barra no celular é a da prévia: Formatar, Cor, Marcadores, Imagem,
  Ditar aqui, Mais, Fechar (o Arquivar segue dentro do Mais). O "Criar uma nota…" cria pelo `with-image` com
  a primeira foto e anexa as outras escolhidas nela.
- **[derivada] A barra cabe numa linha a 360 px:** o toque deixa o `.icone-botao` em 44 px, e com isso o
  **Fechar** caía para uma segunda linha. No celular (`pointer: coarse`) a barra da nota usa ícones de 40 px
  e sem folga entre eles (o alvo ainda passa dos 36 px); conferido a 360 e a 384 px, nos dois temas.
- **A imagem não fica no aparelho** (a decisão do Mauro): a imagem lida vira uma URL `blob:` na loja da aba
  (`ImagensNaTela`, a mesma do PC), solta ao sair; **nada de IndexedDB, Cache API nem service worker**. O
  service worker nunca toca `/api/v1/notes/*/images/*` (o `bundle.test.ts` ganhou as linhas, para GET, POST
  e DELETE), e o `GET` responde `no-store`. O IndexedDB das notas leva **só os metadados** da imagem (o id, o
  tamanho, o SHA-256), que vêm na nota e que o app mantém em dia ao anexar e tirar (`mudarImagens`).
- **O "Imagem sem rede"** (cena 6): sem rede, a imagem que **nunca foi lida nesta visita** aparece como um
  quadrado com a **proporção dela** (a grade não pula quando a rede volta) e o texto "Imagem sem rede"; só o
  primeiro quadrado de cada nota leva o texto, os outros ficam só com o ícone **[derivada]** (o texto
  repetido em cada quadrado pesava). A imagem que **já apareceu nesta visita continua aparecendo**. O
  ícone de imagem fica apagado sem rede. **[derivada]** A imagem que faltou por falta de rede **se pede
  sozinha quando a rede volta** (a loja separa "sem rede", que se repete, da falha de verdade, que não):
  sem isso, a pessoa teria de sair e voltar da aba.
- **[derivada] Tirar a imagem sem rede no celular** não esconde a imagem com o Desfazer e falha depois: o
  pedido sairia para lugar nenhum. A imagem **fica**, e o aviso diz **"Sem rede: a imagem só sai da nota
  quando a rede voltar."** É uma frase a mais fora da cena 7; a "A rede caiu antes de a imagem subir. Tente de
  novo." continua sendo a do anexo.
- **[derivada] A resposta que não é JPEG:** a leitura confere o `Content-Type` da resposta (`image/jpeg`) antes de
  fazer a URL `blob:`; qualquer outra coisa vira "Imagem indisponível". É a segunda trava da garantia do áudio
  (abaixo).
- **[desvio da N10a] A URL `blob:` agora também nasce no cliente da nuvem.** A N10a escreveu "a URL `blob:`
  nasce só no cliente do PC" e prendeu isso no `bundle.test.ts` (a garantia do áudio do ditado: o gravador
  do celular nunca cria URL de áudio). Isso não podia ficar: a imagem do celular precisa dela, e a ADR já
  previa mudar o teste. **Como a garantia continua:** o `createObjectURL` só existe em dois arquivos
  (`clienteLocal.ts` e `nuvem/notas/urlDaImagem.ts`); o segundo **recusa tudo o que não é `image/jpeg`**
  (o áudio do ditado é `audio/webm`, `audio/mp4`…); nada de `nuvem/ditar/` menciona `blob:`, o
  `createObjectURL` nem o `urlDaImagem`, e só o `notasNaNuvem.ts` o importa; e o `urlDaImagem.ts` não fala de
  áudio, `MediaRecorder` nem `getUserMedia`. O teste foi conferido por mutação: pôr um `createObjectURL` em
  `nuvem/ditar/` o quebra.
- **A CSP da nuvem** ganhou `img-src 'self' blob:` (`internal/server/seguranca.go`; o `06` e o `09` dizem). O
  `blob:` vale só para imagem: script, estilo e conexão continuam `'self'`. Os scripts que conferem a nuvem
  (`deploy-teste.ps1` e `conferir-nuvem.ps1`) e o teste do servidor comparam o texto inteiro, e foram
  atualizados; **uma nuvem com a API antiga falha nessa conferência até o deploy** (`docs/deploy-notas-n10.md`).
- **O "a caminho" no painel do PC:** a imagem com `estado: "a_caminho"` não é pedida (o `GET` dela é `409
  IMAGE_PENDING`) e aparece como um quadrado quieto, **na proporção dela**, com "Imagem a caminho", **nunca
  como erro**; o × dela funciona (sobe o `DELETE`). Quando a imagem chega (o painel lê a lista de novo), o
  quadrado vira a imagem. **[derivada]** Se o `GET` responder `IMAGE_PENDING` mesmo assim (a lista estava velha),
  a loja não marca falha: espera a lista nova.
- **[derivada] O painel do PC nunca mostra "Imagem sem rede":** a API local é o próprio PC. A loja e o
  quadrado são os mesmos, mas só o celular acende o `semRede`; a imagem do PC que não abre continua sendo
  "Imagem indisponível".
- **[derivada] A imagem grande no celular:** a barra do alto tem o "N de M" e os botões **Baixar** e **Tirar da
  nota**; abaixo de 480 px os dois viram só o ícone (com o nome no `title` e no leitor de tela), porque o
  título da nota e o "N de M" não cabiam. Na imagem grande de uma imagem sem rede ou a caminho, o texto
  aparece no lugar da imagem.
- **As telas de mentira** (só do `pnpm dev`, fora de toda build): no app da nuvem, `?cenario=notas` (notas com
  uma, duas, cinco imagens e uma só com imagem) e `?cenario=notas-sem-rede` (o mesmo, sem rede); no painel do
  PC, `?cenario=notas-imagens` (imagens presentes e a caminho). Os bytes são JPEGs desenhados no canvas na
  hora. O `notasFalsas.ts` da nuvem ganhou as rotas das imagens (as dez por nota, a lixeira, `409`).
- **A LGPD:** a política de privacidade, o dossiê (que abre a **decisão 12**, do responsável: o aviso e o
  dado de terceiros na foto) e o `09` dizem o que sobe, onde fica, o prazo e que o celular não guarda; as
  frases do app (a Privacidade do celular e a do PC, e a pergunta do "Enviar o histórico") também.
- **O que não foi feito:** a imagem **não entra na fila offline** do celular (anexar exige rede, a decisão da
  cena 6); a **busca** não olha a imagem; o "Copiar para WhatsApp" a deixa de fora; uma **miniatura própria**
  (a grade baixa a imagem inteira de cada cartão, como no PC) fica para se o peso incomodar.
- **Riscos desta parte:** (1) a conferência com **câmera, galeria, sinal e dedo de verdade** é do roteiro
  manual (`docs/teste-manual-notas-n10.md`, partes 11 a 19): os testes rodam no jsdom, e as telas foram vistas
  num Edge sem cabeça, que não é um Chrome de Android nem um Safari; (2) o `capture="environment"` se comporta
  de um jeito em cada navegador (alguns celulares mostram a câmera direto, outros um seletor); (3) um iPhone
  manda HEIC, e o navegador que não o abre recebe "Esse arquivo não é uma imagem que o navegador abra." (o
  Safari o abre); (4) a loja guarda na memória as imagens já vistas até a aba sair, e uma lista com muitas
  imagens grandes pesa no celular (a mesma ressalva do PC); (5) "sem rede" e "a nuvem fora do ar" dão o mesmo
  quadrado (o app não distingue); (6) o celular desbloqueado mostra as imagens que estão na tela, como o
  resto.

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
  **Decidido em 28-out-2026 (Mauro, na revisão de LGPD das notas: "Segue os 6 meses"; o dossiê, decisão
  11):** o ditado que virou nota segue a regra dos 6 meses do PC, como já seguia na nuvem. Sai a linha
  escondida do ditado, e a nota fica (o `ditado_de_origem` não tem chave estrangeira: o id fica solto,
  como na nuvem). A pergunta dos 6 meses o conta à parte (`viraram_nota`, "N deles viraram nota: saem os
  ditados, e as notas ficam."), porque o histórico não o mostra. Código:
  `internal/storage/seis_meses_sqlite.go`.
- **O segundo gatilho** pode colidir com atalho de outro programa (`Ctrl+Win+Alt` é pouco usado, mas
  não foi medido). O detector de conflito de atalho do Agent (`internal/agent/conflito`) passa a
  olhar os dois.
- **Mescla da lista de marcar no servidor** é código novo no lugar mais delicado (o sync). Vai com
  testes de propriedade e revisão adversarial. (N7: a mescla é pura e tem tabela de casos e teste de
  propriedade, `cloudstore/lista_de_marcar_test.go`; ver Decisões da N7.)
- **Texto da nota no servidor:** mesmo tratamento dos ditados (ADR-019); a política de privacidade
  ganha uma linha. (Feito em 28-out-2026: a política, o dossiê e as frases do app falam das notas; ver
  `docs/lgpd-dossie.md`, seção 7, decisões 10 e 11, as duas decididas pelo Mauro no mesmo dia.)
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
| N8 | Aba Notas no celular (PWA), com "Ditar para nota" e a fila offline (marcar, desmarcar e acrescentar item sem rede). **Feita localmente (27-out-2026), na branch `notas/n8`; o deploy espera o Mauro** (`docs/deploy-notas-n8.md`, depois `docs/teste-manual-notas-n8.md`). Ver "Decisões da N8" | N7 |
| N9 | Gatilho "ditar para nota" no PC, com o destino pelo nome | N2 |
| N10 | Imagens. **Ponto de parada antes** (servidor, disco, LGPD; feito em 28-out-2026). **A primeira metade, o PC, feita localmente (28-out-2026), na branch `notas/n10a`** (`docs/teste-manual-notas-n10.md`); a segunda, o sync, a nuvem e o celular, vem depois. Ver "Decisões da N10 (implementação)". **A parte do Go da segunda metade (o Postgres, o servidor, o sync do PC e a API local) feita localmente (29-out-2026), na branch `notas/n10b`** (`docs/deploy-notas-n10.md`); a tela do celular vem por cima dela. Ver "Decisões da N10b". **A parte do celular (a tela das imagens no app, o "Imagem sem rede" e o "a caminho" no painel do PC) e os textos de LGPD, feitas localmente (29-out-2026), na mesma branch `notas/n10b`**; a conferência no celular de verdade é do roteiro (`docs/teste-manual-notas-n10.md`, partes 11 a 19), e o deploy espera o Mauro. Ver "Decisões da N10b (o celular)" | N7 |

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
