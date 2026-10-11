# Product

<!-- impeccable:product-schema 1 -->

> Escrito em 28-set-2026 SEM a entrevista do `init`, a pedido do mantenedor, que quis seguir sem
> parar. Os fatos vêm do `CLAUDE.md`, de `docs/ai-spec/` e do que o mantenedor disse nas sessões
> de trabalho. O que é inferência está marcado **[inferido]** — corrigir quando ele revisar.

## Platform

web

(O painel é uma superfície web servida pelo Agent em loopback e aberta no navegador padrão. O
Agent em si é um programa nativo de Windows; o overlay e o ícone da bandeja são desenhados por ele,
não pelo painel.)

## Users

**O mantenedor** — contador e desenvolvedor. Dita em português do Brasil no Windows, o dia
inteiro, em qualquer aplicativo: Bloco de Notas, navegador, WhatsApp Web, VS Code, sistemas
contábeis. O vocabulário mistura termos contábeis e fiscais (DCTFWeb, PGDAS, EFD-Reinf, eSocial,
balancete, Simples Nacional, fator R) e de desenvolvimento (Supabase, PostgreSQL, commit,
pipeline).

É um **projeto pessoal**, independente de qualquer empresa. Um usuário por instalação.

A revisão de privacidade (LGPD; ver `docs/ai-spec/09-security-privacy.md` e `docs/lgpd-dossie.md`)
foi aprovada pelo mantenedor em 10-out-2026, e vale também para distribuir o Setup. Até o primeiro
release publicado, o usuário é o mantenedor. **Essa aprovação não cobre o ditado no celular
(ADR-021):** a política e o dossiê foram reescritos com ele, e nenhum áudio real sai do aparelho antes
do OK do mantenedor neles.

## Product Purpose

Segurar uma tecla, falar, soltar — e o texto entra no campo que tem o foco do teclado, em qualquer
aplicativo. É **inspirado no Wispr Flow**, feito para uso próprio.

O painel existe para as vezes em que o ditado precisa de atenção: ver o que foi ditado, corrigir um
texto que saiu errado, e ensinar o dicionário a partir do que o ASR **de fato** produziu. Sucesso é
a pessoa quase nunca precisar abri-lo.

## Positioning

- **Local-first.** O ASR roda na própria máquina, em CPU (whisper.cpp `small` q4_0). Áudio nunca é
  persistido, nem em disco nem na nuvem. O áudio **do PC** não sai do PC. Contexto de outras janelas é
  efêmero e nunca sincronizado.
- **O celular, quando ditar (ADR-021, decidido e ainda não implementado), é um caminho à parte e
  opcional.** A voz vai, só em memória, ao provedor de transcrição que a pessoa escolhe (OpenAI ou
  Groq), com a chave dela, pelo servidor da nuvem dela. Começa com um aceite específico, é descartada ao
  fim e nunca é gravada. A chave dela fica cifrada no servidor. Não muda o PC.
- **Português primeiro, com dicionário pessoal que aprende.** Termos do domínio de quem usa são
  corrigidos, e o dicionário aprende de correções **confirmadas** — nunca sozinho. A confirmação é o
  clique no PC onde a pessoa aprende; os outros PCs dela recebem a troca pelo sync, com as mesmas
  checagens (ADR-019, "Decisões da F8", D2).
- Funciona sem nuvem e sem login.

## Operating Context

- Windows 10/11 x64. O Agent fica na bandeja e pode subir com o Windows.
- O painel é aberto pelo item "Abrir Aurora Whisper" do menu da bandeja, no navegador padrão, em
  `http://127.0.0.1:<porta aleatória>`.
- O token da API local existe só na memória da aba. **Recarregar a página perde a sessão**; abrir
  de novo pela bandeja resolve. Isso é regra da spec (09-security-privacy.md), não defeito.
- Uso do painel é esporádico e rápido: abrir, achar um ditado, corrigir ou apagar, fechar.
- **[inferido]** Aberto ao lado do aplicativo em que ele estava ditando, numa janela de tamanho
  médio — não em tela cheia.

## Capabilities and Constraints

Disponível hoje pela API local:

- histórico com busca por texto (no bruto e no final), paginação por cursor;
- cada ditado guarda o **texto bruto do ASR** e o **texto final** que entrou no campo;
- editar o texto final, com concorrência otimista (conflito de versão devolve 409);
- mover para a lixeira e restaurar. Cada ditado ganha data de expurgo ao entrar na lixeira, e o
  **expurgo automático roda** no Agent, na subida e de hora em hora (`internal/agent/expurgo`, desde
  29-set-2026): o que passou de 30 dias na lixeira sai de vez, **menos o que a nuvem ainda não
  soube** (ADR-019, F10, a D3, 08-out-2026): o ditado posto na lixeira com o PC desconectado, por
  exemplo, fica até a nuvem saber, no máximo até o ditado completar 6 meses; num PC que nunca
  conectou, são os 30 dias de sempre. Os **6 meses do histórico ativo** existem no Agent desde a F10,
  mas **só ligam com o clique de quem usa**: o bloco **Histórico** das Configurações (que só aparece
  quando há o que decidir) diz quantos ditados têm mais de 6 meses e pergunta de novo na própria
  linha; com o OK, o Agent copia o banco, apaga esses ditados e, daí em diante, apaga sozinho o que
  completar 6 meses, em até 1 hora, até o **Desligar**. Sem o OK, nada dos 6 meses é apagado. O
  mesmo bloco tem **Apagar as cópias** (as cópias do banco ao lado dele, que o Agent nunca apaga
  sozinho). O painel ainda **não mostra prazo** na lixeira — nem "30 dias", nem "some em N dias";
- **aprender com a edição de um ditado** (ADR-017, 30-set-2026). Ao salvar uma edição que é uma troca
  curta de termo, o aviso de "Texto salvo." pergunta `Aprender “SF” → “ECF”?`, com o botão
  **Aprender** (ou **Alt+A**). Só o clique grava: se o aviso some, nada foi aprendido. Depois vem
  `Aprendido: “SF” vira “ECF” no próximo ditado.`, com **Desfazer**, e a seção **Aprendido no
  painel** das Configurações lista o que foi aprendido, com Desfazer em cada linha (a seção só
  aparece se houver algo). Acento ou maiúscula sozinhos não se aprendem; a troca vale em qualquer
  aplicativo, a partir do próximo ditado, sem reiniciar. O aviso diz, discretamente, em quantos
  ditados a forma aparece ("aparece em 3 ditados").

Restrições:

- nunca mostrar, guardar ou registrar áudio (no celular, ADR-021, o áudio fica só numa variável, na
  memória, e nunca vai a disco, a log nem a uma resposta);
- texto ditado não vai para log;
- a API só aceita `127.0.0.1`/`localhost` na porta certa (DNS rebinding) e origem do próprio painel;
- o painel funciona em modo local e em modo nuvem (`CLAUDE.md`: "Dashboard React deve ser
  reutilizável em modo local e cloud"). A ADR-019 decide como: o mesmo código em dois builds; o da
  nuvem é também o app do celular (PWA, Android primeiro), e o local nunca registra service worker. A
  F4 (05-out-2026) fez o build da nuvem: entrar, o histórico com o selo de "2 versões", a lixeira com
  "excluir definitivamente" e as configurações da conta, no mesmo visual; ele roda, por ora, só no
  servidor de teste do PC (`docs/teste-manual-nuvem-f4.md`). A F5 (05-out-2026) o fez instalável: o
  manifest, o ícone (o mesmo do programa; "Whisper" na tela inicial) e a casca que abre sem rede, com
  "Sem conexão." (`docs/teste-manual-nuvem-f5.md`).

Ainda não existe (não inventar na interface):

- gerenciamento do dicionário pelo painel — hoje é pela linha de comando (`cmd/dictlearn`). O
  painel só lista e desfaz o que **ele mesmo** aprendeu; não mostra nem edita o resto do dicionário,
  e não cria um termo à mão;
- no painel do PC, excluir um ditado definitivamente, ou esvaziar a lixeira à mão (o app da nuvem
  exclui de vez, um por um, desde a F4);
- mostrar quantos dias faltam para um ditado sair da lixeira;
- o celular com os ditados de verdade. A nuvem e o celular estão decididos na ADR-019 (etapa 5): o
  servidor (F1 a F3), o app da nuvem (F4), que já se instala (F5), a conexão do PC (F6), o sync do
  histórico (F7) e do que o painel aprendeu (F8) e a retenção (F9 e F10) existem, mas só contra a
  nuvem de teste do PC: nada roda num servidor de verdade (F11);
- **ditar pelo celular** (ADR-021, aprovada em 10-out-2026, sem código ainda). O passo 1 é um botão de
  microfone no app (toque para começar e toque para parar), discreto e no estilo do painel; o texto
  entra no histórico e desce aos PCs. A voz vai ao provedor que a pessoa escolhe, com a chave dela, só
  depois de um aceite específico (veja `docs/politica-de-privacidade.md`). O passo 2, o app nativo do
  Android com a bolha flutuante ou o teclado, é outra ADR. Não inventar na interface nada disso antes de
  existir.

## Brand Commitments

- Nome: **Aurora Whisper**. Produto autônomo, com arquitetura própria.
- Interface em **português do Brasil**.
- Compromisso visual declarado pelo mantenedor, e vinculante: **"o menor que funciona"**, **"bem
  pequeno e discreto, como no Wispr Flow original"**.
- **Paleta do projeto** (decidida em 29-set-2026): cores, fontes e detalhes visuais valem no
  painel, na pílula de gravação e no ícone da bandeja — verde `#00E676` como cor do que está
  ativo ou válido, preto `#070A09`, off-white `#F5F7F5`, vermelho só para erro e risco (sempre
  com texto), Space Grotesk / Inter / IBM Plex Mono embutidas, colchetes de canto como assinatura.
  **Nunca** o logo nem o nome do autor, que não fazem parte do produto. Substitui o "azul como
  base" e o cinza do overlay aprovados antes.
- Marcas (`docs/marca/`): o **Pico estourado** é o símbolo do sistema (bandeja, favicon,
  cabeçalho); a **Aurora ilustrada** aparece dentro do painel.
- A pílula de gravação ficou ~35% menor a pedido do mantenedor ("dá pra tirar uns 30 a 40% do
  tamanho").

## Evidence on Hand

- Renderizações do overlay e do ícone da bandeja (testes com a tag `renderpng`).
- Nenhum depoimento, cliente, métrica pública ou material de imprensa. Não fabricar.

## Product Principles

1. **Invisível até ser necessário.** O produto é o ditado; a interface aparece para informar ou
   para consertar, e sai do caminho.
2. **Nunca afirmar o que não foi medido.** Estado mostrado tem de ser estado real — lição
   aprendida no projeto (um indicador não afirma estado que não mediu).
3. **Quem usa decide.** Aprender alias, ligar o início automático, apagar um ditado: sempre por
   ação explícita, nunca como efeito colateral.
4. **Privado por construção.** O que não precisa existir não é guardado.
5. **O menor que funciona.**

## Accessibility & Inclusion

- Claro e escuro, seguindo o sistema.
- Uso completo pelo teclado — quem usa passa o dia ditando e digitando. O aviso que tem uma ação
  (**Desfazer**, **Aprender**) fica no fim da página e some sozinho, então a ação também tem atalho
  (Ctrl+Z, Alt+A), e o aviso espera enquanto o foco ou o ponteiro estiverem nele; o de **Aprender**
  espera 30 s.
- **[inferido]** A spec prevê NFR-014 (acessibilidade) sem critério fechado; até lá, WCAG 2.2 AA
  como piso.
