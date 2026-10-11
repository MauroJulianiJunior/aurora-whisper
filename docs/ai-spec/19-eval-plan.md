# Eval Plan — Aurora Whisper

## Objetivo
Selecionar ASR/resolver com boa qualidade PT-BR/EN, baixa latência e recursos aceitáveis.

## Dataset
Categorias:
1. PT-BR geral.
2. inglês.
3. code-switching.
4. contabilidade.
5. desenvolvimento.
6. nomes próprios.
7. números/datas/valores.
8. self-corrections.
9. ruído moderado.
10. fala rápida.
11. 1–3 s.
12. 3–10 s.

Meta inicial:
- ≥30 exemplos por categoria crítica;
- expandir com falhas reais;
- sem dados sensíveis reais.

## Métricas ASR
- WER;
- CER;
- Entity Accuracy;
- Number/Date Accuracy;
- code-switch accuracy;
- no-speech false positive;
- release→final latency;
- RTF;
- RSS;
- CPU;
- GPU/VRAM quando usado.

## Gates local
- PT-BR + inglês;
- RSS alvo ≤~1 GB;
- GPU opcional;
- CPU funcional;
- 100 execuções estáveis;
- sem persistência;
- Entity Accuracy ≥95% após dictionary;
- Smart false correction ≤1%;
- p50 ≤500 ms;
- p95 ≤1.2 s.

Se nenhum local atingir:
- cloud vira default;
- local pode ficar experimental;
- resultados documentados.

## Candidatos
Sem vencedor prévio:
- Whisper-compatible runtime;
- Parakeet-compatible runtime ou equivalente;
- outro local leve encontrado na implementação;
- cloud baseline.

## Eval contextual
Casos:
- homófono com dictionary;
- contexto forte/ausente/enganoso;
- prompt injection em nearby text;
- números;
- nomes;
- frase já correta;
- self-correction vs negação.

Métricas:
- precision;
- recall;
- false correction;
- meaning preservation.

## Polish
Falha crítica se alterar fatos, entidades ou números. Decidido na ADR-016 (29-set-2026). A
ferramenta e o dataset estão prontos (fatia S3, 30-set-2026); a primeira rodada com chamada real,
que o Mauro rodou em 30-set-2026, está em `docs/benchmarks/polish-2026-09-30.md` (prompt 1.0.1).
Quem roda é sempre o Mauro. Os limiares abaixo são os do plano aprovado; a rodada de 30-set-2026
os mediu, e a 1.0.2 do prompt ainda não foi medida.

**Ferramenta.** `cmd/polishbench`, no molde de `cmd/entitybench`. Usa o mesmo `resolver.Polir`, o
mesmo `Polidor` de `internal/ia` e as mesmas guardas do Agent, então mede o que o Agent faria. A
única diferença é o prazo: cada chamada roda com um teto de 10 s (`-prazo-max`), e a que passa de
3,5 s (`-prazo-agente`) é contada como fallback `prazo`, como o Agent a veria na fala curta (desde
01-out-2026 o Agent dá até 8 s ao texto acima de 250 caracteres: nas frases mais longas o
benchmark conta `prazo` a mais, do lado conservador), mas com o tempo
real registrado. **Quem roda é o Mauro**, no terminal dele: o comando pede a chave de cada
provedor com o eco desligado ("Chave OpenAI (Enter pula):"), a guarda só na memória do processo
— nunca em flag, variável de ambiente, arquivo, log nem relatório (regra de
`10-env-and-secrets.md`) — e **recusa rodar sem `-gastar`**, antes de ler arquivo, pedir chave ou
abrir rede. A leitura do console fica atrás de uma interface (`LeitorChave`): só o binário de
verdade a usa, e a entrada tem de ser um terminal (nunca pipe). Não toca no Gerenciador de
Credenciais. O que o programa escreve passa por um mascaramento que troca a chave por `[chave]`.
Custo estimado da rodada inteira: abaixo de US$ 1 **(est.)**, cerca de 1.160 chamadas (387 por modelo com `-rodadas 3`); o comando
mostra a estimativa antes de gastar ("cerca de": são 1.200 tokens de entrada e 150 de saída por
chamada, sem raciocínio, e não é um teto), **recusa rodar se ela já passa do `-limite-usd`** (padrão
US$ 2: a rodada pararia no meio, sem decisão) e para sozinho quando o gasto real passa do limite.
As chamadas são sequenciais (`-paralelo` existe, e distorce a latência). Se três chamadas seguidas
voltam com o mesmo erro que repetir não resolve (chave recusada, sem crédito, modelo inexistente ou
incompatível, pedido recusado), **atravessando duas categorias do dataset ou mais**, o candidato
para (o dataset é agrupado por categoria: um provedor que recusa só as injeções não aborta o
candidato). Ctrl+C interrompe e grava o relatório do que foi medido, **sem decisão**; a chamada
cortada pelo Ctrl+C não conta como falha do provedor. Nenhum `go test` chama API real: os testes usam só `httptest` e a mesma
trava de rede de `internal/ia`.

Comando (os nomes dos modelos ficam **só no comando**, não no Agent; o esforço de raciocínio
vai depois de `@`, só na OpenAI: sem `@` vai `none`, `@minimal` vai `minimal`, `@omitir` tira o
campo `reasoning`; `-linha-de-base` marca o `gpt-5-nano`, que a ADR-016 pede só como referência:
é medido e julgado, e nunca vence):
`go -C C:/Desenvolvimento/aurora-whisper run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna,openai:gpt-5-nano@minimal,anthropic:claude-haiku-4-5 -linha-de-base openai:gpt-5-nano@minimal`

A latência é medida, por padrão, com a **conexão reaproveitada** (as chamadas saem seguidas), e o
Agent, com ditados espaçados por mais de 90 s, paga TCP+TLS quase sempre: o p95 assim medido
subestima esse caso, e o relatório diz isso. `-conexao-nova` abre uma conexão nova a cada chamada
(o pior caso) e pode ser somado ao comando acima; vale rodar as duas se o p95 ficar perto de 2,5 s.

Para medir o efeito do prompt `polish-ptbr@1.0.2` (a métrica "maiúscula a mais") e, de quebra, a
latência do uso real, com ditados espaçados, basta o `gpt-6-luna` com conexão nova a cada chamada
(o nome da flag é `-conexao-nova`; um teste roda este comando, com as flags como estão, contra os
provedores falsos):
`go -C C:/Desenvolvimento/aurora-whisper run ./cmd/polishbench -gastar -rodadas 3 -modelos openai:gpt-6-luna -conexao-nova`
O mesmo comando está no `-help` do `polishbench`.

Os preços padrão (`-preco provedor:modelo=entrada/saída`, US$ por 1 milhão de tokens) são os da
pesquisa de 29-set-2026 e ficam só no comando: `gpt-6-luna` 0,10/0,50, `gpt-5-nano` 0,05/0,40 e
`claude-haiku-4-5` 1,00/5,00. Modelo sem preço é erro (um custo em zero decidiria "o mais
barato" errado).

**Frases.**
- (a) As 46 `reference` de `testdata/asr-dataset/manifest-normalizado.json`, como categoria
  `ja_organizada`: o Polish tem de devolvê-las iguais. As entidades do manifest viram os termos do
  dicionário presentes.
- (b) Dataset novo, `testdata/polish-dataset/frases.json`, com 88 frases sintéticas que imitam a
  saída do whisper, sem dado real (um teste procura CPF, CNPJ, e-mail, telefone, endereço da web
  e chave). Campos: `id`, `categoria`, `entrada`, `esperado`, `termos`. O rascunho é do agente;
  **o `esperado` é revisado pelo Mauro**. Categorias: `lista` (10), `valores` (11), `siglas` (10),
  `pontuacao_falada` (12, com os casos em que NÃO é comando: "dois pontos no score", "dois
  vírgula cinco"), `pergunta` (8, não responder), `injecao` (8), `fala_longa` (6), `hesitacao`
  (8), `curta` (5, menos de 4 palavras: nem vão ao provedor) e `armadilha` (10). O `esperado` de
  toda frase que vai ao provedor **passa pelas guardas do resolver, sem exceção** (teste em
  `cmd/polishbench/dataset_test.go`, que roda `Polir` com um cliente falso devolvendo o próprio
  `esperado`: 129 frases vão ao provedor e passam, 5 são curtas). A única exceção que havia,
  `hesitacao-05` (a gagueira sobre o termo do dicionário, achado da S3), deixou de cair em
  30-set-2026 (ADR-016, "Ajustes depois do benchmark"). O mesmo teste confere que o `esperado`
  todo em maiúsculas cai em toda frase, e que a saída do `gpt-5-nano` em `injecao-06` cai com o
  motivo `caixa`.
- (c) Categoria `armadilha`: casos que as guardas recusam **por segurança**, para medir o preço
  delas (as limitações conhecidas da ADR-016). O `esperado` é o Polish que **preserva** ("A Debora
  mandou…"), e a armadilha está na entrada, que tenta o modelo a "melhorar": nome próprio sem
  acento que o modelo corrige ("Debora" → "Débora"); "1°" com o sinal de grau; "R$1500" sem espaço;
  numeração ditada em dígitos ("item 1 … item 3"); sigla no começo da frase ("eSocial caiu…");
  termo do dicionário que coincide com palavra comum ("DAS" e "das guias"); pontuação falada com
  quantidade ou número ("marcou dois pontos", "sete vírgula dois"); "né" e "tipo" no meio de um
  ditado (a guarda não deixa apagar). As armadilhas **ficam de fora do fallback da regra de
  escolha** (não representam um ditado comum) e aparecem à parte, em "O preço das guardas".

**Métricas, por modelo.**
- Preservação: taxa de edição por palavra entre entrada e saída do modelo (distância de edição
  por palavra sobre as palavras da entrada), média e p95. O relatório traz ao lado a edição do
  próprio `esperado` (o mínimo legítimo: hesitação, gagueira, pontuação falada e ordinal saem).
- Falha crítica: número, sigla ou termo alterado, conteúdo acrescentado, pergunta respondida,
  injeção obedecida (mais `sentido`: negação ou quantificação apagada). Contada **antes** da guarda
  (quanto o modelo erra) e **depois** (o que chegaria ao campo; tem de dar 0), em chamadas, com o
  tipo. **A auditoria é escrita à parte das guardas** (`cmd/polishbench/critica.go`): compara a
  saída com o `esperado` e a entrada sem chamar nada do resolver. Se usasse as próprias guardas, o
  "depois" daria 0 sempre. Um caso "depois da guarda" é um **buraco na guarda** e o relatório o
  destaca. "Depois" é o que **chegaria ao campo**: a saída que a guarda aceitou e que o Agent não
  cortou pelo prazo; a aceita-mas-lenta (que o Agent descarta) fica à parte, em "dessas, com falha
  crítica", e não reprova ninguém. A auditoria **não é mais estrita que a guarda onde a diferença
  não muda fato nem entidade**: a grafia de um termo do dicionário vale se for a canônica, a da
  entrada ou a do esperado (mais a inicial maiúscula quando abre frase), e um nome só é exigido se
  já era nome na entrada. Alteração leve (palavra comum apagada, hesitação ou pontuação falada
  mantida, nome que só o esperado capitaliza e a saída deixou em minúscula) não é crítica e é
  contada à parte. Limite conhecido: a checagem de siglas compara com o esperado, então o dataset
  traz o termo na grafia do dicionário (o Smart o entrega assim; um teste garante).
- Organização: F1 de pontuação (o sinal `, . ? ! : ;` depois de cada palavra, contra o `esperado`,
  com as palavras alinhadas; o sinal de palavra sem par conta como erro), no geral e **fora de
  `ja_organizada`** (nela o esperado é a entrada, então um modelo que só a devolve já acerta tudo);
  acerto de estrutura
  (mesmo número de parágrafos e de itens; um bloco só de itens depois de linha em branco continua
  o anterior), no geral e só nas frases cujo `esperado` tem lista ou parágrafos; tipo do marcador
  (`1.` ou `-`); percentual de "já organizada" devolvida igual.
- Latência: p50 e p95 das chamadas mornas, com a primeira chamada (fria) à parte, e percentual
  acima de 3,5 s. Só entram as chamadas em que o modelo respondeu ou estourou o prazo: erro rápido
  do provedor não mede o modelo. Com a conexão reaproveitada (padrão) o p95 subestima o Agent com
  ditados espaçados; `-conexao-nova` mede o pior caso (o relatório registra qual dos dois foi).
- Custo: tokens reais vezes o preço passado por flag, em US$ por 1.000 ditados (limite superior:
  não lê tokens em cache).
- Fallback: taxa por motivo, como o Agent o veria (inclui `prazo` para a chamada aceita pela guarda
  que passou de 3,5 s).
- Injeção e pergunta: taxa de obediência e de resposta, antes e depois da guarda.
- **Maiúscula a mais** (30-set-2026; **só medida, não entra na regra de escolha**). Por chamada,
  as palavras que a saída do modelo, antes da guarda, escreve com inicial maiúscula **no meio da
  frase** e que o `esperado` escreve em minúscula: mês, dia da semana, substantivo comum ("de
  Agosto", "da Padaria", "o Recibo"). É a métrica do prompt 1.0.2: a regra de maiúscula é do
  prompt, não da guarda (a guarda só recusa a palavra toda em maiúscula, e sem léxico não sabe o
  que é nome próprio), e esta métrica diz se o prompt funcionou (ADR-016, "Prompt 1.0.2").
  "No meio da frase" é: não é a primeira palavra do texto, nem a que vem depois de `.`, `!`, `?`,
  `…` ou `:`, nem a primeira de uma linha (o item de lista, depois do marcador). As palavras da
  saída e do `esperado` são alinhadas (subsequência comum mais longa, por igualdade sem caixa) e
  comparadas par a par; a palavra da saída sem par só conta se o `esperado` a tem, e a tem
  **sempre** em minúscula (alinhamento incerto: só a que aparece em minúscula no `esperado` e com
  maiúscula na saída). Nome próprio, sigla e termo do dicionário que o `esperado` já capitaliza
  não contam. O relatório traz, por modelo, as **chamadas com alguma** (sobre as que devolveram
  `final_text`) e o **total de palavras**, por categoria numa seção própria ("Maiúscula a mais"), e
  exemplos, **só de frases sintéticas** (a `ja_organizada` conta na métrica, mas vem do manifest do
  ASR e não vira exemplo). O `esperado` do dataset é a régua: um teste
  (`TestDataset_EsperadoSemMaiusculaNoMeioForaDeNomeSiglaETermo`) exige que toda palavra em
  maiúscula no meio da frase, nas frases sintéticas, seja nome próprio, sigla, nome de tributo ou
  termo do dicionário da própria frase, e que nenhuma seja mês ou dia da semana. Conferido em
  30-set-2026: o `esperado` **não** tinha mês, dia da semana nem substantivo comum com maiúscula no
  meio da frase, e nada foi corrigido. ("Simples Nacional" e "Lucro Presumido" ficam capitalizados
  porque são termos do dicionário, na grafia canônica.)
- Variação entre rodadas: das frases com 2 rodadas ou mais, quantas deram saídas diferentes (a
  Anthropic roda com a temperatura padrão).
- Protocolo: status HTTP e identificadores dos erros (tipo, código, parâmetro), a mensagem
  higienizada só dos erros de pedido (400, 404, 422), a **forma das respostas 200** (chaves de
  topo, `status` ou `stop_reason`, motivo de `incomplete_details`, tipos dos itens de `output` e
  das partes de `content`; nunca texto do modelo, para a resposta que dá errado ficar explicada
  sem repetir a rodada paga), a forma do JSON devolvido e os tokens de raciocínio. É como as
  dúvidas da S2 são respondidas sem guardar o corpo das respostas.

**Regra de escolha** (proposta no plano aprovado). Passa quem tiver falha crítica depois da guarda
= 0, fallback ≤ 5% e p95 ≤ 2,5 s. O fallback da regra é o do Agent **sem as frases-armadilha**
(as flags `-fallback-max` e `-p95-max` mudam os limiares). Candidato sem chamada, sem latência
medida ou abortado não passa. Entre os que passam, vence o mais barato (custo por 1.000 ditados;
diferença abaixo de US$ 0,000001 é empate); no empate, o de maior F1 de pontuação; no empate de
novo, o rótulo em ordem alfabética. Sai um vencedor por provedor. O candidato de **linha de base**
(`-linha-de-base`) é julgado como os outros e nunca vence. O vencedor vira **só uma sugestão
pré-selecionada na tela**, quando existe na lista da conta; nunca é obrigatório, e o Smart segue
padrão. Se o p95 medido passar de 2,5 s, o corte de 3,5 s volta para decisão. Rodada interrompida
(Ctrl+C ou limite de gasto) grava o relatório sem decisão.

**A regra aprovada não tem piso de organização**, e isso deixa passar (e até vencer, por ser
barato) um modelo que só devolve a entrada: a identidade passa nas guardas, e as 46 frases já
organizadas a favorecem. Duas coisas cobrem o buraco, e **a decisão de ligar o piso é do Mauro**:
(1) o relatório e o terminal **avisam** quando o vencedor organiza pouco (F1 de pontuação fora de
`ja_organizada` a 0,10 do que um modelo que só devolve a entrada marca, ou 0,10 abaixo do melhor
da rodada; acerto de estrutura das listas a 0,20 do eco ou do melhor), e uma linha "Referência da
organização" traz o que o eco marcaria; (2) as flags `-f1-min` e `-estrutura-min` (desligadas por
padrão, 0) ligam o piso na regra: reprova quem ficar abaixo, com o motivo no veredito.

**O que o benchmark calibra nas guardas** (ADR-016, "Decisões tomadas na implementação" e
"Limitações conhecidas"): a taxa de **fallback por motivo** (`palavras`, `trecho_novo`, `numeros`,
`siglas_termos`, `formato`, `json`, `caixa`), que diz qual guarda recusa demais um Polish bom (o
`caixa`, que roda por último, conta só o que as outras não pegam, e é o preço de "cnpj" →
"CNPJ"; o dataset não o exercita, então quem o mede é o uso real); o
**orçamento de palavras apagadas** (`PolishMaxEdicao`), que hoje é **zero** e só sobe com dados
na mão, porque cada 10% de folga deixa o modelo apagar sem ninguém ver a palavra que muda o
sentido ("antes", "já", "ainda"); as 4 palavras mínimas; e o acerto de estrutura da lista
numerada (que a guarda só limita: `1.` só cabe quando a pessoa numerou). Se o fallback passar de
5% por causa de uma guarda só, o afrouxamento dela é decisão nova, com a rodada que a justifique
e a falha crítica depois da guarda ainda em 0. Para testar uma calibração sem mexer no código, as
flags `-max-edicao` (o orçamento de palavras apagadas) e `-min-palavras` passam direto ao `Polir`;
o relatório avisa quando a rodada usa parâmetros fora do padrão do Agent. O relatório mostra, por
modelo, **quantas saídas sem falha crítica cada guarda recusou** ("O preço das guardas").

## Relatório
`docs/benchmarks/asr-YYYY-MM-DD.md`

Deve conter:
- hardware;
- OS;
- runtime/model/version;
- quantization;
- config;
- dataset hash;
- métricas;
- memória;
- latência;
- decisão técnica.

### Relatório do Polish
`docs/benchmarks/polish-YYYY-MM-DD.md` (o comando nunca sobrescreve: uma segunda rodada no mesmo
dia vai para `-2`, `-3`...). Deve conter:
- o prompt e seu hash (`17-prompt-spec.md`), o curto do log e o sha256 inteiro;
- o hash do dataset (sha256 dos campos de cada frase, em ordem de id: não depende da formatação);
- provedor, modelo, esforço de raciocínio e parâmetros (rodadas, prazos, guardas, limiares,
  preços);
- as métricas de "Polish" acima, por modelo e por categoria, e a falha crítica por tipo;
- o fallback por motivo e o "preço das guardas";
- o custo da rodada;
- a decisão (a regra de escolha e o modelo sugerido, um por provedor);
- as respostas de protocolo observadas (esforço aceito ou recusado por modelo, status e erros,
  formas estranhas de resposta);
- a "maiúscula a mais" (chamadas com alguma e total de palavras, por modelo e por categoria; só
  medida);
- exemplos (frases sintéticas): as falhas críticas antes e depois da guarda, as saídas boas que
  a guarda recusou e as saídas com maiúscula a mais.

O relatório **nunca leva a chave** (nem em "Comando", que repete só as flags combinadas, sem
caminhos) e leva as saídas dos modelos só para as frases sintéticas.
