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
  pulam; o Desfazer apaga a nota só na versão 1, fora da lixeira. Nada vai para a fila do sync.

### Ditar dentro da nota

O editor da nota é um campo de texto do painel. O Agent já insere no campo com foco (ADR-011), então
**ditar dentro da nota funciona sem código novo no Agent**: abrir a nota, segurar `Ctrl+Win`, falar.
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
- Abaixo, os cards em grade: **fixadas** primeiro, depois as outras pela última edição. No celular,
  uma coluna.
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

### Sync

- **[derivada]** Duas entidades novas no motor (`internal/sync`): `nota` e `marcador`, ao lado de
  `ditado` e `aprendido` (`storage.EntidadeDitado`, `storage.EntidadeAprendido`). Toda gravação com o
  evento na mesma transação (ADR-009).
- **Conflito:** a regra da ADR-019 vale igual. A versão que perdeu fica no item, com o selo, e a
  pessoa escolhe. **[derivada]** Para a lista de marcar há uma mescla a mais, porque o caso típico
  é o mercado (marcando no celular) com uma edição no PC: se os dois lados só mudaram o estado de
  marcado de itens diferentes (o texto de cada linha igual), o servidor mescla sem selo.
- Servidor: tabelas `notes`, `labels`, `note_labels` (migrations novas, só criação; nenhuma
  destrutiva). As rotas da nuvem seguem o padrão das de ditado.
- **Celular sem rede (E4, decisão do Mauro: "Marcar sem rede"):** no mercado sem sinal, a lista abre
  do cache do service worker e **marcar e desmarcar funcionam offline**. As mudanças vão para uma fila
  no navegador (IndexedDB), com o mesmo marcador por entidade do sync do PC, e sobem quando a rede
  volta; a mescla da lista de marcar acima resolve o caso de o PC ter mudado a nota nesse meio-tempo.
  [derivada] Offline, só marcar, desmarcar e acrescentar item; criar, apagar nota e mexer em marcador
  pedem rede (a tela diz isso). O app mostra "Sem rede: N mudanças esperando".

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
- **"Virar nota" e o sync (N7), ponto aberto.** `virou_nota` é só deste PC e não gera evento: a nuvem
  continua com o ditado no histórico dela, e nada diz a ela que ele "virou nota". Quando a N7 trouxer a
  nota para a nuvem, decidir: (a) o ditado some também do histórico da nuvem e do celular (a coluna
  sobe, como uma mudança de campo), ou (b) a nuvem mantém os dois. Hoje vale (b), por omissão.
- **"Virar nota" guarda o texto bruto para sempre.** A retenção de 6 meses não alcança o ditado que
  virou nota (a ADR, lida à letra), então o `raw_text` dele (e o `final_text`) fica no PC sem prazo,
  mesmo que a pessoa apague a nota da lixeira depois (o ditado fica escondido, com o id solto). Se isso
  incomodar na LGPD (o ditado é dado pessoal), a saída é limpar o `raw_text` do ditado quando a nota
  for apagada de vez, ou deixá-lo voltar à regra dos 6 meses nesse caso. Não foi decidido.
- **O segundo gatilho** pode colidir com atalho de outro programa (`Ctrl+Win+Alt` é pouco usado, mas
  não foi medido). O detector de conflito de atalho do Agent (`internal/agent/conflito`) passa a
  olhar os dois.
- **Mescla da lista de marcar no servidor** é código novo no lugar mais delicado (o sync). Vai com
  testes de propriedade e revisão adversarial.
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
| N3 | "Virar nota" no histórico, com Desfazer | N2 |
| N4 | Marcadores: criar, renomear, apagar, filtro na lateral, sugestão de marcador | N2 |
| N5 | Converter trecho: por prefixo, por regra e com IA (prompt `notas-lista@1` com as guardas; benchmark próprio no `polishbench`) | N2, L |
| N6 | Lista de marcar (marcados descem, desmarcar tudo), copiar para WhatsApp, busca única | N2 |
| N7 | Sync das notas e marcadores: migrations do Postgres, entidades no motor, mescla da lista de marcar, rotas da nuvem. **Deploy: ponto de parada** | N1-N6 |
| N8 | Aba Notas no celular (PWA), com "Ditar para nota" e a fila offline (marcar, desmarcar e acrescentar item sem rede) | N7 |
| N9 | Gatilho "ditar para nota" no PC, com o destino pelo nome | N2 |
| N10 | Imagens. **Ponto de parada antes** (servidor, disco, LGPD) | N7 |

Ordem sugerida: L e N1 em paralelo; depois N2, N3, N6, N4, N5, N9; e N7, N8, N10.

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
- Destino do "ditar para nota": casamento do nome, empate, nome inexistente, nota com lista de marcar.
- Sync: as duas entidades no motor, conflito com selo, mescla da lista de marcar (só estados
  diferentes mescla; texto diferente vira selo), lápide.
- Fila offline do celular: marcar sem rede, recarregar a página, a rede voltar, o PC ter mudado a
  nota no meio-tempo (mescla), e a mudança que o servidor recusa (selo, nunca perda calada).
- Painel: os componentes com Testing Library, nos dois temas.
- Verificação manual (roteiro, uma pessoa sabendo o que vai acontecer): ditar dentro da nota, o
  segundo gatilho, copiar para WhatsApp.
