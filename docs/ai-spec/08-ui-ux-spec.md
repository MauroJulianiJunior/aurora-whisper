# UI/UX Spec — Aurora Whisper

## Superfícies
- tray;
- overlay;
- dashboard local;
- dashboard cloud;
- installer/update;
- futuro Android/IME. **Etapa 5 (ADR-019):** o celular Android entra como PWA instalável, que é o
  dashboard cloud (seção "Celular (PWA)"); app nativo e teclado continuam futuros.

## Overlay
Default: centro inferior.

### Recording
- waveform;
- duração;
- microfone;
- "Gravando".

### Processing
- spinner/onda congelada;
- "Processando".

### Success
- check curto;
- "Inserido".

### Error
- "Não consegui transcrever. Tente novamente."

### Blocked
- "Ditado desativado em campo protegido."

#### Blocked — janela de administrador
Acrescentado em 29-set-2026 (decisão do Mauro; ADR-011, nota de 29-set-2026).

- "O Windows não deixa ditar em janela de administrador."

Quando a janela em primeiro plano roda com nível de integridade maior que o do Agent (o
Gerenciador de Tarefas, por exemplo), o ditado é bloqueado como em qualquer campo protegido — o
Windows não deixa um programa comum escrever ali (UIPI) —, mas a pílula diz o motivo verdadeiro em
vez de "campo protegido". Só aparece quando o nível foi lido e comparado; se não der para ler, fica
a mensagem de "campo protegido". Mesmo ponto vermelho do bloqueio, mesmo tempo na tela.

A sugestão original ("Janela de administrador: o Windows não deixa ditar aqui.") foi escrita para a
fonte de bitmap, em que precisaria de quatro linhas; o texto acima cabe em duas.

**Na prática, quase nunca aparece** (teste manual do Mauro, 29-set-2026, roteiro B14): com o
Gerenciador de Tarefas em primeiro plano, segurar o atalho não fez nada — nem pílula, nem bipe. O
gancho de teclado de baixo nível de um processo comum não recebe as teclas quando a janela da
frente é elevada (a mesma UIPI). É seguro: o ditado nem começa, e nada é digitado. A mensagem
continua definida para o caso de o atalho chegar ao Agent com a janela de administrador marcada
(ADR-011, nota do B14).

### Texto que chegou depois do prazo (01-out-2026)
- "O texto demorou e ficou no histórico."

Quando a transcrição estoura o prazo (proporcional ao áudio, `07-flows.md`, "Timeouts"), a pílula
de erro sai como sempre. Se o texto chega depois, ele vai para a linha do histórico daquela
tentativa (status de erro, texto pelo Smart), **não é inserido**, e esta pílula aparece, com ponto
cinza, como a da área de transferência: o ditado não se perdeu, mas não entrou no campo. Só aparece
se nenhum outro ditado estiver em curso; com um em curso, a linha no histórico basta
(`docs/defeito-ditado-longo.md`). O painel mostra a linha com a etiqueta "falhou" e a nota de que
o texto pode não ter entrado no campo.

### Repetição removida (01-out-2026)
- "Removi uma repetição. Confira o texto."

Quando o ASR entra em laço (a mesma frase várias vezes seguidas no lugar da fala), o Agent deixa
uma ocorrência antes de resolver e inserir (`internal/asr/repeticao`). O ditado é inserido, e em
vez de "Inserido" a pílula pede para conferir, com o ponto verde do sucesso: o laço costuma tomar
o lugar de parte da fala, e só quem ditou sabe o que faltou.

O que fica de fora: palavra repetida ("não, não, não", "muito muito", menos de 8 vezes), itens de
lista diferentes entre si, números sozinhos, e frase de 3 a 9 palavras repetida menos de 6 vezes
seguidas (uma aula, "Repitam comigo: ...", ou o mesmo item de lista ditado algumas vezes). Frase de
10 palavras ou mais repetida 4 vezes seguidas é tratada como laço, e uma frase curta dita de
propósito 6 vezes ou mais também, mesmo sendo fala: é o falso positivo que a regra aceita. Por isso
o texto do ASR como chegou, com as cópias, fica no histórico como `raw_text`, e o editor do painel
mostra a diferença entre ele e o texto final; o `final_text` e o que foi inserido saem sem as
cópias.

### Aparência (29-set-2026)
Identidade da paleta do projeto (cores, contorno e ponto de estado: ver ADR-014, "Atualização
de 29-set-2026").

- Gravação e processamento: cápsula de 128x20 px, só a onda.
- Mensagens (Success, Error, Blocked, janela de administrador, falha de inserção, área de
  transferência, texto no histórico, repetição removida): IBM Plex Mono Medium, 13 px, off-white,
  com o ponto de estado antes do texto — verde em sucesso e na repetição removida, vermelho em erro
  e bloqueio, cinza na área de transferência e no texto que ficou no histórico. A pílula tem o
  tamanho do texto: de 128 px (a largura da cápsula de gravação; "Inserido") a 280 px de largura,
  32 px de altura com uma linha e 48 com duas. Texto que não cabe numa linha quebra em duas de
  larguras parecidas, nunca no meio de palavra e nunca truncado.
- Centralizada embaixo: o centro da pílula fica sempre no mesmo ponto, a 76 px da borda inferior
  da tela, e a janela muda de tamanho junto com a pílula, sem piscar.

Não rouba foco, não entra no Alt+Tab, não bloqueia cliques.

## Tray
- Abrir Aurora Whisper;
- status;
- provider ativo;
- pausar;
- reconectar;
- configurações;
- sair.

## Dashboard

### Início
- status Agent;
- sync;
- provider/model;
- modo;
- última latência;
- últimos ditados;
- testar microfone.

### Histórico
Colunas/cards:
- texto;
- data;
- device;
- app (nunca preenchido: o Agent não guarda o app em foco, e a ADR-019 o tira da nuvem);
- modo;
- provider/model;
- duração;
- processing time;
- status.

Ações:
- copiar;
- editar;
- excluir;
- detalhes.

Filtros:
- busca;
- período;
- device;
- modo;
- provider;
- trash.

**A lista se atualiza sozinha no PC (ADR-021, S; `web/src/mudancas.ts` e `Painel.tsx`).** O ditado feito
no celular e as edições feitas lá chegam ao painel do PC aberto em segundos, sem a pessoa tocar em nada.
- **Como.** Com a aba à vista, o painel lê a cada 3 s o contador de mudanças do Agent
  (`GET /api/nuvem/mudancas`, `06`). Quando o número muda, relê a lista pelo mesmo caminho do "voltar à
  aba". A leitura é também o aviso ao Agent de que o painel está aberto: ele busca a nuvem a cada 10 s
  (e a cada 60 s com o painel fechado ou escondido há mais de 15 s). Uma edição feita no celular
  aparece no painel aberto em até ~13 s (os 10 s do Agent mais os 3 s da leitura).
- **O que não muda na tela.** Nada se anuncia: sem aviso, sem "Atualizado", sem recarregar a página.
  A busca, a vista (Histórico ou Lixeira) e as páginas de "Mostrar mais antigos" já carregadas ficam
  como estão. **A edição em andamento não se perde:** o editor aberto continua aberto, o texto
  digitado e não salvo continua na caixa, o foco não sai do lugar, e as linhas com rascunho ficam na
  lista mesmo que a nuvem as tenha mudado ou mandado para a lixeira. Quem salva depois de o ditado ter
  mudado lá cai no conflito de sempre ("alterado em outra aba"), com o texto dele intacto. A região
  viva (`role="status"`) só fala se a contagem de ditados mudou, como em qualquer carga.
- **Quando para.** Escondida, a aba não lê (e o Agent volta aos 60 s); ao voltar, lê já. Se a leitura
  falha (o Agent reiniciando), o painel não mostra nada por isso: a faixa do topo é da lista, como
  sempre. Um Agent de antes da S (`404`) não é perguntado de novo. O celular e a nuvem não têm o
  contador: o app continua relendo a lista ao voltar à aba.

### Detalhe
- raw_text;
- final_text;
- diff;
- metadata;
- editar;
- aprender correção;
- copiar;
- excluir.

Nunca mostrar áudio/contexto.

**Aprender correção (ADR-017, fatia F4).** Não é tela nem janela nova: usa o **aviso que já existe** no
rodapé, o mesmo de "Texto salvo.". Fonte da verdade: `web/src/aprendizado.tsx`, `Aviso.tsx` e
`Painel.tsx`.
- Salvou uma edição que é uma troca curta de termo: `Texto salvo. Aprender “SF” → “ECF”?`, com o
  botão **Aprender**. Com 2 trocas, as duas aparecem no mesmo texto (`“SF” → “ECF” e “RJ” → “RF”`) e
  um clique aprende as duas (um pedido por troca, na ordem do texto). Se o aviso some sozinho, a
  sugestão foi ignorada, e **nada é aprendido sem o clique** (AC-022). O clique é no PC de origem:
  os outros PCs recebem o par pelo sync, sem clique lá, com as mesmas checagens do Aprender
  (ADR-019, "Decisões da F8", D2).
- Se o "de" aparece em outros ditados da pessoa, o aviso diz, discretamente (cor secundária da
  pílula), quantos: `“SF” aparece em 3 ditados.` (com duas trocas: `“SF” aparece em 3 ditados, “RJ” em
  1.`). Com 0, não diz nada. O botão **Aprender** tem no `title` o efeito: "A troca vale para todo
  ditado daqui para frente, em qualquer aplicativo. Dá para desfazer."
  A seta é só para os olhos (`aria-hidden`); o leitor de tela ouve `“SF” vira “ECF”`, como na lista
  das Configurações.
- Depois do clique: `Aprendido: “SF” vira “ECF” no próximo ditado.`, com **Desfazer** (e Ctrl+Z, como
  nos outros avisos). O foco que estava no botão passa para o **texto** da pílula nova
  (`tabIndex=-1`), e **não** para o Desfazer: um Espaço ou Enter a mais, por exemplo para rolar a
  página, desfaria o que acabou de ser aprendido. O Desfazer fica a um Tab. A pílula espera enquanto
  o foco estiver nela, como toda pílula. Se o Agent gravou mas não conseguiu trocar o dicionário em
  memória, a pílula diz `Aprendido: “SF” vira “ECF”, mas só depois de reiniciar o Agent.` (e o mesmo
  no Desfazer), e nunca `Não deu para aprender`: a lista das Configurações já mostra o par. A
  ressalva é de cada troca: com duas, e só uma ficou para o reinício, a frase separa as duas
  (`Aprendido: “SF” vira “ECF” no próximo ditado; “RJ” vira “RF”, mas só depois de reiniciar o Agent.`).
- Se o Agent recusa, o aviso traz a frase dele, sem inventar: `Não deu para aprender. "SF" já vira
  "ECD" no dicionário.` Com duas trocas em que uma vale e a outra não, o aviso diz as duas coisas
  (`Aprendido: “SF” vira “ECF” no próximo ditado. "RJ" já vira "RZ" no dicionário.`), e o **Desfazer**
  é só do que valeu.
- Visual do painel (`PRODUCT.md`): IBM Plex Mono nos termos (`.termo`), entre aspas tipográficas; o
  verde (`--acento`) só nos termos do "Aprendido:", que é o que foi confirmado; a sugestão fica no
  off-white da pílula. O atalho vai na própria pílula, em mono pequena, como o do Desfazer; some nas
  telas estreitas.
- **Tempo e foco (F4).** O aviso já esperava enquanto o ponteiro **ou** o foco estivessem nele (dois
  estados, não um), e passava o foco para a pílula nova. Confere-se, e muda: o aviso que só informa
  some em **4 s**; o com **Desfazer**, em **8 s**; o com **Aprender**, em **30 s** (`tempoDoAviso.ts`).
  Sumir em 8 s tiraria a sugestão de quem usa só o teclado, que chegaria ao botão (no fim da página)
  por dezenas de Tabs, ou de quem usa leitor de tela, que ouve e depois decide (a pílula está numa
  região `role="status"` `aria-live="polite"`). Por isso **Alt+A**, fora de campo de texto, faz o
  mesmo que o botão enquanto o aviso existe (o mesmo desenho do Ctrl+Z do Desfazer), com
  `aria-keyshortcuts`. Só o Alt: Ctrl+Alt é o AltGr dos teclados com ele, e Ctrl+Enter, que salva no
  editor, repetido viraria "aprender". A tecla segurada (`repeat`) e um segundo disparo antes da
  resposta não aprendem de novo. Com foco ou ponteiro em cima, nenhum tempo vale (WCAG 2.2.1).
  Conferido no navegador de verdade (Alt+A).
- Conflito de versão (`409`) no salvar não mostra o aviso de aprender: não há sugestão. Um `409` do
  próprio aprendizado (variante de outro termo, sugestão vencida) **não** é esse conflito: o cliente
  não o trata como versão velha, e mostra a frase.

### Dicionário
- canonical;
- idioma;
- domínio;
- prioridade;
- aliases;
- origem;
- create/edit/delete/enable.

**Pendente.** A tela completa de Dicionário (idioma, domínio, prioridade, ligar e desligar qualquer
termo) segue pendente; a curadoria do resto é pelo `cmd/dictlearn`. Já existem, em Configurações: a
lista "Aprendido no painel" (ADR-017) e a seção **Dicionário** (ADR-020), em que a pessoa cria um
termo com as grafias erradas, liga e desliga a dica dele para o reconhecimento de voz, e remove o que
pôs (abaixo).

### Correções
- from → to;
- uso;
- remover aprendizado.

**Pendente.** A tela completa de Correções segue pendente: a Correction não é guardada nesta etapa
(ADR-017; volta com o sync, na etapa 5). O "remover aprendizado" vale, por ora, como o **Desfazer** da
lista "Aprendido no painel".

### Configurações

#### Ditado
- tecla;
- threshold 300–2000 ms;
- beep;
- microfone;
- microfone sempre pronto (ADR-015; padrão desligado);
- max duration;
- overlay.

#### Texto
- Literal/Smart/Polish (o Smart é o padrão; o Polish preserva e organiza o texto — pontuação,
  parágrafos, listas — e é opcional: ADR-016);
- Polish: seção IA, com provedor, modelo e chave. A tela mostra, numa linha, por que o último
  Polish não agiu (por exemplo, "sem crédito na OpenAI"): só o motivo, nunca o texto. O modelo
  vencedor do benchmark pode aparecer pré-selecionado, quando existe na lista da conta; nunca é
  obrigatório;
  - **Como ficou (S5, ADR-016).** O modo Polish diz o que faz: "Organiza o texto com IA:
    pontuação, parágrafos e listas, sem trocar as suas palavras. Envia o texto ao provedor de IA
    escolhido abaixo. Se algo falhar, entra o texto do Smart." Ele só pode ser escolhido com
    provedor e a chave dele guardada (e `polish_disponivel: true`); antes disso o motivo do
    bloqueio aparece embaixo do modo.
  - **A linha do motivo** é a primeira coisa da seção IA depois da introdução: uma frase do Agent
    (`polish_ultimo_motivo`, ver `06`), no estilo discreto da ajuda — `role="status"`, sem cor de
    risco, sem `role="alert"`: é informação, não erro (o ditado saiu, só sem o Polish). Vazia
    quando não há motivo a mostrar. A região existe sempre, para o leitor de tela anunciar o motivo
    que entrar quando a tela lê de novo.
  - **O modelo sugerido** vem em `POST /api/ia/testar` (`modelo_sugerido`): na lista da conta ele
    leva "(sugerido)" (o `label` da opção), a ajuda do campo diz "Sugerido: … , o que se saiu
    melhor nos testes do Aurora Whisper.", e, **se nenhum modelo foi escolhido** (o rascunho está
    vazio quando o teste volta) e ele consta na lista, entra no **rascunho** — a tela mostra "não
    salvo" e o Salvar segue da pessoa. O que a pessoa escreveu, ou o modelo já salvo, nunca é
    trocado; o rascunho de outro provedor, que ela passou a ver enquanto o teste ia e voltava,
    também não. Sem sugestão do Agent, ou com a sugestão fora da lista da conta, nada é marcado
    nem preenchido.
- dictionary;
- context;
- cloud context resolve. **Depois (ADR-019):** o contexto não vai para a nuvem na etapa 5.

#### Dicionário (Adicionar termo)
ADR-020 (`web/src/Termos.tsx`), depois do botão Salvar configurações e antes de "Aprendido no painel":
age na hora, como o Desfazer, e por isso fica fora do formulário das configurações. O menor que
funciona, nos tokens do `index.css`, claro e escuro.
- introdução: "Palavras que o reconhecimento de voz escreve errado. Valem já no próximo ditado.";
- o formulário, que no celular fica um campo embaixo do outro:
  - **Palavra certa** (exemplo "RAG");
  - **Como sai errado (opcional)** (exemplo "rague, hage"), com a ajuda "Separe por vírgula.";
  - a caixa **Dica para o reconhecimento de voz**, **marcada por padrão** (decisão 1 da ADR-020), e
    marcada de novo depois de cada Adicionar, com a ajuda "Sobram 98 de 140 caracteres para dicas.";
    sem espaço, "Não sobra espaço: desmarque aqui ou desligue a dica de outro termo." (os dois
    caminhos; revisão de 01-out-2026). A ajuda só aparece com a conta: até a lista chegar, nem o
    parágrafo nem o `aria-describedby` existem;
  - **Adicionar** ("Adicionando…" enquanto vai); Enter num campo também adiciona;
- a lista **Adicionados por você** (`h3`; some quando está vazia): uma linha por termo, `RAG ← rague,
  hage` na IBM Plex Mono (o leitor de tela ouve "RAG, sai errado como rague, hage"), a caixa "dica" e
  **Remover**. O termo que já era do dicionário leva a etiqueta "do dicionário", não tem a caixa (a
  dica dele não muda pelo painel), e o Remover tira só as grafias dela;
- os avisos, na pílula: "“RAG” adicionado. Vale no próximo ditado." ("atualizado" quando o termo já
  existia), mais "Saiu das dicas: DCTFWeb." quando a dica empurrou termos para fora do prompt; "A dica
  não muda por aqui: “DCTFWeb” já era do dicionário." quando a caixa estava marcada num termo do
  dicionário (`dica_ignorada`); "Dica ligada para “RAG”." e "Dica desligada para “RAG”."; "“RAG”
  removido." (e "Grafias de “DCTFWeb” removidas." no termo do dicionário, de que só as grafias dela
  saem); e "Vale depois de reiniciar o Agent." no caso `vale_ao_reiniciar`;
- os erros ficam na seção, num `role="alert"`: "Não deu para adicionar: {frase do Agent}" (e "Não deu
  para mudar a dica:", "Não deu para remover:"); o que foi digitado fica nos campos. A ligação perdida
  vai para a faixa do Painel, como nas outras seções. A lista que o Agent não consegue dar (sem
  dicionário) mostra a frase dele no lugar do formulário; o Agent de antes do "Adicionar termo" (a
  rota responde `404` sem frase: o painel foi recompilado com o Agent velho aberto) mostra "Este Agent
  ainda não tem o Adicionar termo. Reinicie o Agent.";
- o foco: depois de adicionar, volta para a palavra certa; depois de remover, vai para o Remover da
  linha que ocupou o lugar, ou para a palavra certa quando a lista some.

#### Aprendido no painel
ADR-017, fatia F4 (`web/src/AprendidoNoPainel.tsx`), depois das seções Ditado, Texto, IA e Sistema,
do botão Salvar configurações e da seção Dicionário (ADR-020), e antes de Privacidade: o Desfazer age
na hora, e o Salvar de cima não tem nada a ver com ele.
- uma linha por par, `sf → ECF`, com **Desfazer** (para o leitor de tela: "sf vira ECF");
- só o que tem a origem "painel" (o que o `cmd/dictlearn` ou a mão puseram no arquivo não aparece);
- sem nada aprendido, a seção **não aparece**; lista que não carregou também não;
- IBM Plex Mono nos termos; sem janela nem modal;
- a lista é lida quando a tela aparece e quando o aviso do histórico aprende ou desfaz algo (o Painel
  sobe um contador), para não mostrar o que já não é verdade;
- o Desfazer de uma linha avisa ("Desfeito: “SF” não vira mais “ECF”.") e leva o foco para a linha
  que ocupou o lugar; ao desfazer a última, a seção some e o foco vai para o Salvar configurações. Se
  o Agent recusa (outra aba já desfez), a frase dele aparece na seção, num alerta, e a linha fica.

**As linhas de estado do sync (F8h, 07-out-2026; ADR-019, "Decisões da F8").** Com a nuvem, o Agent
manda também, no mesmo `GET /api/aprendizado`, as linhas de estado (`situacao` e `motivo`, opcionais;
`06`). Cada uma é a troca (`sf → ECF`, como as outras) seguida de uma **etiqueta cinza**, a `.etiqueta`
de sempre (mono, 11 px, `nowrap`, sem cor de erro: nada se perdeu), e **nunca tem Desfazer**: não está
no dicionário deste PC, e, para mudar, a pessoa aprende de novo.
- **"não aplicado neste PC":** a troca chegou da nuvem e não vale aqui. Embaixo, o motivo numa frase
  pequena (12 px, `--texto-3`): "O domínio da troca não existe aqui." (`dominio_ausente`); "A troca
  valeria em mais lugares do que no PC onde foi aprendida." (`restricao_nao_cabe`); "A palavra não existe
  aqui, e a troca é presa a um domínio." (`palavra_ausente_com_dominio`); "Aqui essa grafia já pertence a
  outra palavra." (`variante_de_outro_termo`); "A palavra está desligada." (`termo_desligado`); "O
  dicionário não aplicaria a troca." (`nao_agiria`); "A grafia é comum demais para virar troca."
  (`palavra_comum`); "O dicionário está indisponível." (`arquivo_indisponivel`). A etiqueta já diz "neste
  PC", e por isso as frases não o repetem (revisão da F8h, 08-out-2026). Um motivo que o painel não
  conhece fica sem a frase.
- **"desfeito em outro aparelho"** e **"trocado em outro aparelho":** uma troca deste PC que perdeu na
  nuvem (D4); só a etiqueta. Somem quando a pessoa aprende de novo, ou em 30 dias (o Agent para de
  mandar).
- A seção **aparece também quando só há linhas de estado**. Uma `situacao` que o painel não conhece não
  vira linha, e não faz a seção aparecer. Um Agent de antes da F8 não manda nada disso, e a lista é a de
  sempre.
- A etiqueta fica logo depois da troca, na mesma linha; se não cabe, desce inteira (a mais comprida, "desfeito em
  outro aparelho", tem 188 px, e o `estilo.test.ts` confere que cada uma cabe sozinha em 320 px). O
  foco depois de um Desfazer é calculado pelos **botões**, e não pelas linhas (as de estado não têm): vai
  para o botão que ocupou o lugar, ou para o anterior se era o último; sem botão nenhum e com linhas de
  estado, para o título da seção; sem linha nenhuma, a seção some e o foco vai para o Salvar
  configurações.

#### ASR
- policy;
- provider/model local;
- cloud provider informativo (**depois**, ADR-019: não há ASR na nuvem na etapa 5);
- language;
- model status;
- benchmark.

#### Sistema
- autostart;
- porta local advanced;
- logs;
- update.

#### Privacidade
- áudio não armazenado;
- contexto não armazenado;
- Polish: "No modo Polish, o texto ditado (nunca o áudio nem o que está nas outras janelas) vai ao
  provedor de IA escolhido, com a sua chave; OpenAI e Anthropic podem guardá-lo por até 30 dias e
  dizem não usá-lo para treinar modelos." (texto de ADR-016; a tela o traz desde a S5, como o
  quarto item da lista da Privacidade, sempre — também antes de o Polish estar liberado: é aviso,
  não recurso);
- cloud toggles;
- apagar histórico;
- devices.

Na etapa 5 (ADR-019), "cloud toggles" é a seção **Nuvem** (abaixo), e entra na Privacidade a frase
da nuvem, só depois de conectar (texto final na F6, quando o PC conectar; a F4 fez a Privacidade do
app da nuvem, em "Configurações do celular"): "Com a nuvem conectada, o texto dos seus
ditados e o que você aprendeu vão para a sua nuvem, em `<endereço>`. O áudio e o que está nas outras
janelas nunca vão."

#### Nuvem (no PC; ADR-019, F6 e F7c)
O menor que funciona, no estilo das outras seções.
- Desconectado: o campo **Endereço da nuvem** (vazio por padrão) e o botão **Conectar à nuvem**. Uma
  linha de ajuda: "Opcional. Sem conectar, nenhum ditado sai deste PC, fora o Polish, se ligado."
- Conectando: o código de conferência, em IBM Plex Mono, e "Confira se a página da nuvem mostra o
  mesmo código. Só aprove lá se foi você que clicou aqui."
- Antes do primeiro envio: "Vão subir N ditados dos últimos 6 meses. A lixeira não vai. Antes, é
  feita uma cópia do banco." com **Enviar** e **Agora não**.
- Conectado: "Conectado como `<e-mail>`. Último envio: há 2 min." e **Desconectar**.
- Revogado ou sem token: "Desconectado. O Aurora Whisper continua funcionando neste PC." com
  **Conectar de novo**. A bandeja mostra o mesmo estado no item **Reconectar** (`Tray`).
- Um aprendido que não vale neste PC aparece em **Aprendido no painel** como "não aplicado neste PC",
  com o motivo numa frase curta.

**Como a F6 fez (05-out-2026; `web/src/NuvemNoPC.tsx`).** A seção **Nuvem** fica depois do
Dicionário e do Aprendido no painel, antes da Privacidade, e age na hora (fora do **Salvar
configurações**). Os textos do plano da F6 trocaram os de cima, que eram proposta; o envio inicial e o
"último envio" são da F7.
- **Desconectado:** "Desligada. Nada vai para a nuvem até você conectar." (o plano dizia "Nada sai deste
  PC": com o Polish ligado, o texto vai ao provedor de IA, e a frase não pode prometer menos do que
  sai), o campo **Endereço da nuvem** (modelo `https://…`, vazio na primeira vez e, depois, com a URL
  da instalação) e **Conectar à nuvem** (Enter no campo também; desligado com o campo vazio).
- **Conectando:** "Aprove no navegador e confira o código **K7QM**." (o código em IBM Plex Mono,
  espaçado), com **Abrir de novo** (um link para a página de aprovação, numa aba nova; só um endereço
  `https://` ou `http://localhost` vira link) e **Cancelar**. O foco vai para o **Abrir de novo**.
- **Conectado:** "Conectado como **{e-mail}**." e "Na nuvem, este PC aparece como {nome}.", com
  **Desconectar** e, embaixo, "Desconectar tira o acesso deste PC, aqui e na nuvem." Desconectar
  avisa "Este PC foi desconectado da nuvem." e devolve o foco ao campo.
- **Os avisos**, logo abaixo do título, antes do resto: "Este PC foi desconectado na nuvem." (revogado,
  ou o token vencido); "Desconectado neste PC. A nuvem não respondeu: revogue-o em Aparelhos, no
  app."; "A nuvem não respondeu agora. O PC continua conectado." (os três como informação) e, numa
  nota de erro: "O pedido venceu. Clique em Conectar à nuvem de novo.", "Este PC já está ligado a outra
  conta.", "Não deu para conectar. Clique em Conectar à nuvem de novo." e "O navegador não abriu. Use
  Abrir de novo.".
- **As recusas do Conectar**, numa nota de erro, com a frase do Agent: "Use um endereço https://.", "Use
  https:// ou http://localhost.", "Este build de teste só conecta da pasta bin\ (modo portátil).",
  "Não deu para falar com a nuvem. Confira o endereço.", "Este PC já está ligado a outra conta.".
- **Um Agent de antes da nuvem** (o painel novo com o Agent velho): "Este Agent ainda não tem a nuvem.
  Reinicie o Agent."
- **Sem laço:** o painel pergunta ao Agent quando as Configurações aparecem e quando a aba volta a
  ficar à vista; o Agent fala com a nuvem no máximo a cada 30 s.
- **A página que o navegador vê no 127.0.0.1** (servida pelo Agent, fontes do sistema, claro ou escuro
  pelo sistema): "PC conectado" / "Pode fechar esta aba e voltar ao painel.", ou "Não deu para
  conectar" / "Volte ao painel do PC e clique em Conectar à nuvem de novo."
- A Privacidade do PC ainda não ganhou a frase da nuvem: nada sobe antes da F7, e a frase entra com o
  envio.
- A bandeja não mostra o estado da nuvem na F6 (o item **Reconectar** é da F7b, com o laço).

**Como a F7c fez (06-out-2026; ADR-019, "Decisões da F7c"; `web/src/NuvemNoPC.tsx` e
`web/src/textosDaNuvem.ts`).** Os textos do plano da F7c trocaram os de cima ("Enviar" e "Agora não",
"Último envio: há 2 min."), que eram proposta. Os números levam o ponto do milhar ("7.000").
- **Conectado, antes do envio** (só depois da primeira rodada com a conexão; até lá, "Conferindo a
  nuvem…", ou a frase da falha, e a seção relê a cada 2 s até ela terminar): "Vão subir {N} ditados dos últimos 6 meses. A lixeira não vai. Antes,
  uma cópia do histórico fica neste PC." (com 1: "Vai subir 1 ditado dos últimos 6 meses.") e
  **Enviar o histórico**. Depois de um esquecer, antes dela: "Alguns destes ditados podem já estar na
  nuvem que este PC esqueceu." (revisão de 06-out-2026: era "Estes ditados já foram para outra nuvem
  e vão subir também para esta.", inexata de volta à mesma conta, quando o que sobra são os que a
  nuvem recusou). Sem nada a enviar, não pergunta.
- **Durante:** o botão diz "Copiando o histórico…" enquanto a cópia é feita; depois, "Enviando o
  histórico: {feitos} de {total}." (num `role="status"`), relido a cada 2 s só enquanto envia e com
  a seção à vista.
- **No fim**, no aviso: "O histórico subiu para a nuvem." e, se algum não subiu, "{k} ditados não
  subiram para a nuvem." (com 1: "1 ditado não subiu para a nuvem.").
- **Depois** (num `role="status"`): "Em dia com a nuvem. Conferido às {HH:MM}.", "{n} para enviar." e
  "A última tentativa não deu certo. O PC tenta de novo sozinho." (as duas últimas juntas, se for o
  caso) e, enquanto houver, "{k} ditados não subiram para a nuvem." (revisão de 06-out-2026: antes, o
  número só aparecia no aviso do fim, e sumia com ele).
- **A cópia que falha**, numa nota: "Não deu para copiar o histórico. Nada foi enviado." e, quando se
  sabe o motivo, "Já há 3 cópias de antes do envio ao lado do histórico: apague as que não quiser." ou
  "Falta espaço no disco." O botão volta, e o próximo clique tenta de novo.
- **Esquecer esta nuvem** (desconectado, com uma nuvem gravada, embaixo do Conectar): o botão e a ajuda
  "Para ligar este PC a outra nuvem ou conta." O clique abre a pergunta na própria linha, sem janela:
  "**Esquecer {url}?** Este PC deixa de saber dessa nuvem e da conta {e-mail}. Os ditados continuam
  aqui; na nuvem, nada muda." e, se há mudanças que não subiram, "{n} mudanças feitas aqui ainda não
  subiram. Elas ficam neste PC." (com 1: "1 mudança feita aqui ainda não subiu. Ela fica neste PC.";
  revisão de 06-out-2026: era "… e não vão mais para lá.", mas, de volta à mesma conta, elas sobem
  pela reconciliação). O {n} é o `historico.mudancas`: a mudança num ditado que a nuvem tem (a
  edição, a lixeira, o restaurar, a escolha do selo, a reconciliação) e, desde a F8, a troca aprendida
  que foi desfeita ou trocada aqui e que a nuvem ainda não confirmou; o histórico do envio inicial e o
  ditado novo que ainda não subiram não contam (reconferência de 06-out-2026: a frase contava a fila
  inteira). Um PC sem nenhum ditado editado e com 2 trocas desfeitas aqui, ainda não enviadas, diz "2
  mudanças feitas aqui ainda não subiram". Com **Esquecer** (botão de risco) e **Cancelar**; o foco vai para o Cancelar, o Esc cancela, e
  cancelar devolve o foco ao **Esquecer esta nuvem**. Esquecido: o aviso "Este PC esqueceu a nuvem.",
  o campo vazio e o foco nele. As recusas do Agent, numa nota: "Desconecte antes de esquecer a
  nuvem.", "Cancele o pedido em curso antes de esquecer a nuvem.", "Conecte o PC à nuvem antes de
  enviar o histórico." e "O PC ainda está conferindo a nuvem. Tente de novo em alguns segundos."
- **A Privacidade do PC**, com o PC conectado, ganha: "Com a nuvem conectada, o texto dos seus ditados
  vai para a sua nuvem, em {endereço}. O áudio e o que está nas outras janelas nunca vão." (o aprendido
  entra na frase na F8: ver "Como a F8h fez", abaixo).
- **A bandeja:** "Nuvem: enviando {feitos} de {total}" enquanto o envio inicial corre (também no
  `429`); "sem conexão", "desconectada" e "resposta inesperada" vencem.
- **O selo no PC:** o mesmo do app (abaixo, "O selo de conflito"), agora com os botões.

**Como a F8h fez (07-out-2026; ADR-019, "Decisões da F8", D3; `web/src/NuvemNoPC.tsx` e
`web/src/textosDaNuvem.ts`).** Nada aprendido sobe antes do clique em **Enviar o histórico**, e a
pergunta conta as trocas aprendidas (o `previa_trocas` do `06`, o M).
- **A pergunta** (só depois da primeira rodada com a conexão, como a de antes): "Vão subir {N} ditados
  dos últimos 6 meses e {M} trocas aprendidas. A lixeira não vai. Antes, uma cópia do histórico fica
  neste PC." O singular de cada um ("1 ditado", "1 troca aprendida"); o verbo no singular só quando é
  uma coisa só ("Vai subir 1 ditado…", "Vai subir 1 troca aprendida."). Com M = 0, é a frase de antes,
  palavra por palavra (e um Agent de antes da F8, que não manda o M, também).
- **Com N = 0 e M > 0** a pergunta aparece do mesmo jeito, e é só "Vão subir {M} trocas aprendidas.":
  sem a lixeira e sem a cópia (a cópia é do histórico, e não há ditado a enviar), e sem "Alguns destes
  ditados podem já estar na nuvem…" (não há ditado na frase). O botão é o mesmo, **Enviar o histórico**
  (durante o clique, "Enviando…" no lugar de "Copiando o histórico…", porque não há cópia); depois, as
  trocas contam no "{n} para enviar." e não no progresso "Enviando o histórico: {feitos} de {total}.",
  que é só dos ditados.
- **O aviso do freio** (conectado, abaixo da linha do envio e acima do Desconectar, num `role="status"`,
  como uma linha de ajuda e não como nota de erro: não houve falha): quando o `historico.freio_trocas`
  (`06`) é maior que 0, "{n} trocas aprendidas saíram do dicionário de uma vez. Por segurança, nada do
  aprendido sobe para a nuvem até elas voltarem." (com 1, só por robustez: "1 troca aprendida saiu do
  dicionário de uma vez. Por segurança, nada do aprendido sobe para a nuvem até ela voltar."). Sem o
  campo, ou com 0, não aparece; desconectado, também não (a varredura só roda conectada). É o aviso
  que a ADR prometia ("nada sobe, e o painel avisa"); o botão de confirmar a remoção à mão continua
  sendo decisão do Mauro (ADR-019, desvio 16).
- **A Privacidade do PC**, com o PC conectado: "Com a nuvem conectada, o texto dos seus ditados e as
  trocas que você aprendeu vão para a sua nuvem, em {endereço}. O áudio e o que está nas outras janelas
  nunca vão." (o `08` do plano dizia "e o que você aprendeu"; "as trocas que você aprendeu" é o que a D1
  limita: só o botão Aprender). A do app do celular: "Configurações do celular", abaixo (ADR-021, D0).
- **Para ver as telas** sem Agent nem nuvem: `pnpm -C web dev` e `/telas-dev.html?cenario=estados` (o
  painel do PC; também `so-estados`, `pergunta`, `so-trocas`, `conectado` e `freio`) ou
  `/telas-dev-nuvem.html?cenario=lista` (o app da nuvem; também `vazio`, `teto`, `erro` e `longa` e,
  para o ditado do celular, `gravando`, `enviando`, `erro-rede`, `sessao` e `chegou`: um microfone de
  mentira fala sozinho, num relógio parado; e, para o aceite e as Configurações do ditado, `aceite` (o
  toque no microfone de uma conta sem aceite, com a folha), `ditar-desligado`, `ditar-ligado` e
  `ditar-polish`). A calibração do microfone, `/calibracao-dev.html` ("A calibração do microfone no
  Android", abaixo). São só do `pnpm dev`: nenhum build as inclui. A nuvem de mentira dessas telas e
  dos testes do app (`web/src/testes/servidorFalsoNuvem.ts`) fala o contrato do `06` para o ditado no
  celular (`/dictation`, `/dictation/settings`, `/dictation/consent` e `/ai-keys`), e o
  `contratoDoDitado.test.ts` prende os campos e os status dela ao `06`.
- **O teste manual** (F8i, 08-out-2026): `docs/teste-manual-nuvem-f8.md` confere, no `bin\` e no app, com
  a nuvem de teste, a pergunta do envio com a troca e o **Desfazer** do **Aprendido** do app e do
  **Aprendido no painel**, nos dois sentidos. É um PC só: as etiquetas de estado e o "não aplicado neste
  PC" ficam para os testes automáticos (ADR-019, "O fechamento da F8g").

#### Histórico (os 6 meses no PC; ADR-019, F10)
**Decidido em 08-out-2026 (ADR-019, "Decisões da F10") e implementado na F10d e na F10e**
(`web/src/HistoricoNoPC.tsx`). O menor que funciona, no estilo das outras seções. Fica em **Configurações**, logo antes da
Privacidade (depois da Nuvem), age na hora (fora do **Salvar configurações**) e **só aparece quando
há o que decidir**: com ditados de mais de 6 meses (a contagem do `GET /api/retencao`), com a regra
ligada, ou com cópias do banco ao lado dele. Nada fora das Configurações: sem faixa, sem janela, sem
aviso na bandeja. Os números levam o ponto do milhar. Os textos abaixo são proposta, a confirmar na
entrega.
- **A pergunta** (N maior que 0, regra desligada): "{N} ditados têm mais de 6 meses. Ao confirmar,
  eles saem deste PC de vez, e daqui em diante o que fizer 6 meses sai sozinho. Antes, uma cópia do
  histórico fica neste PC." (com 1: "1 ditado tem mais de 6 meses. Ao confirmar, ele sai deste PC de
  vez, e daqui em diante o que fizer 6 meses sai sozinho. Antes, uma cópia do histórico fica neste
  PC."), com o botão **Apagar os mais antigos**. Com a cópia de um OK anterior ao lado do banco
  (`copia_dos_6_meses`; a cópia dos 6 meses é uma só), a última frase vira "Não se faz outra cópia: a
  que já está neste PC é de antes, e o que nasceu depois dela não está nela." (revisão da F10: a frase
  de antes prometia uma cópia com tudo o que sai, e o que nasceu depois da primeira cópia não está
  nela). Só quando maiores que 0, em linhas discretas embaixo: "{M} deles estão na lixeira.", "{K} têm
  duas versões, e as duas saem." (o selo sai sem a escolha de quem usa) e "{J} são de outro dono e
  ficam." (as linhas de outro `user_id` que o painel não mostra). Com N = 1, as duas primeiras falam
  dele: "Ele está na lixeira." e "Ele tem duas versões, e as duas saem."
- **A confirmação na própria linha** (o padrão do **Esquecer esta nuvem**, sem janela): o clique no
  botão abre, logo abaixo das linhas e com o respiro da linha de botões que ela substitui,
  "**Apagar {N} ditados?** Não dá para desfazer." com **Apagar** (botão de risco) e **Cancelar** (a
  cópia fica na pergunta, acima; "só a cópia guarda" seria falso para o que nasceu depois da cópia de
  um OK anterior); o foco vai para o Cancelar, o Esc cancela, e cancelar devolve o foco ao botão.
  Durante o OK o botão diz "Copiando e apagando…" (o Agent responde uma vez só, e a tela não vê a
  fronteira entre a cópia e o apagar), ou só "Apagando…" quando a cópia de antes já está lá. Pronto: o
  aviso "{N} ditados saíram deste PC." (a cópia já está na linha das cópias do bloco) e o histórico é
  relido.
- **O OK que ligou mas não terminou** (um lote seguinte caiu: `"incompleto": true`): não é falha. O
  aviso diz "{n} ditados saíram deste PC. Os outros {M} saem na próxima rodada, em até 1 hora.", o
  histórico é relido e o foco vai ao **Desligar**. Se a resposta do OK falhou mas a releitura mostra a
  regra ligada (o OK foi gravado antes da falha), o aviso diz "Os 6 meses estão ligados. O que ainda não
  saiu sai na próxima rodada, em até 1 hora.", sem "Tente de novo" (não há o que tentar), com o mesmo
  histórico relido e o foco no **Desligar**.
- **Se a contagem mudou** entre o que a pessoa viu e o clique (o Agent recusa com o número novo): a
  frase troca para o número novo, o aviso diz "A contagem mudou." e a pessoa confirma de novo; nada
  foi apagado. **Se não há mais nada a apagar**: "Não há nada a apagar agora." e o bloco some.
- **Ligado:** "Os 6 meses estão ligados: o que fizer 6 meses sai sozinho deste PC, em até 1 hora." e
  **Desligar** (um clique, sem pergunta: não apaga nada), com a ajuda "Desligar volta ao de antes: o PC
  guarda tudo. O que já saiu não volta." Desligado, o bloco volta a oferecer a regra enquanto houver o
  que apagar.
- **Pausada pelo freio** (o Agent ficou mais de 7 dias parado, ou o relógio do PC pulou): o bloco volta
  com a pergunta de sempre e, **antes dela**, uma linha discreta: "A regra foi pausada: o Agent ficou
  parado por mais de 7 dias, ou o relógio do PC mudou. Nada foi apagado." Sem nada a apagar e sem
  cópias, o bloco não aparece, e a pausa fica só no registro do programa (contagens, nunca texto). Sem
  nada a apagar mas com cópias (depois de um OK, a dos 6 meses fica), o bloco aparece pelas cópias e a
  linha da pausa vem no topo, com "A pergunta volta quando houver o que apagar." no fim (revisão da
  F10: antes, a linha só existia junto da pergunta, e nada dizia que a regra tinha desligado).
- **As cópias** (F10e): "Há {C} cópias do histórico ao lado do banco, com o texto de antes." (com 1: "Há
  1 cópia…") e **Apagar as cópias**, com a pergunta na linha: "**Apagar as {C} cópias?** Elas guardam
  o texto que os 6 meses apagam. Não dá para desfazer." com **Apagar** e **Cancelar**. O Agent nunca as
  apaga sozinho. A cópia de um envio do histórico que ainda não terminou fica: o bloco diz "1 delas é
  a do envio do histórico à nuvem, que fica até o envio terminar." (só ela: "Ela é a do envio…"), a
  pergunta conta só as que saem (`copias_apagaveis`), e sem nenhuma que saia o botão não aparece. O
  aviso diz: "{n} cópias apagadas. 1 fica: o envio do histórico à nuvem ainda não terminou." Depois de
  apagadas, o bloco some, se não há mais o que decidir; se fica só a do envio, o foco vai ao **Salvar
  configurações**.
- **As recusas do Agent**, numa nota de erro: "Não deu para copiar o histórico. Nada foi apagado." e,
  quando se sabe o motivo, "Já há 3 cópias de antes dos 6 meses ao lado do histórico: apague as que não
  quiser." (que o botão acima resolve) ou "Falta espaço no disco."; "O histórico deste PC ainda não
  está pronto para isso. Reinicie o Agent."; e, de um Agent de antes da F10, "Este Agent ainda não tem
  os 6 meses. Reinicie o Agent."
- **A Privacidade do PC**, com os 6 meses ligados, ganha: "O que completa 6 meses é apagado deste
  computador." O comentário de `web/src/ambiente.ts` deixa de dizer que a lixeira "some em 30 dias":
  no PC, ela sai em 30 dias, ou depois de a nuvem saber, no máximo até os 6 meses do ditado.
- **Para ver a tela** sem Agent: `pnpm -C web dev` e `/telas-dev.html?cenario=…`: `historico` (a
  pergunta com 1.240 ditados, as três linhas de baixo e 2 cópias), `historico-simples` (12 ditados, sem as
  linhas), `historico-1` (o singular), `historico-ligado`, `historico-pausado`, `historico-copias` (só as
  3 cópias), `historico-envio` (3 cópias, 1 delas a do envio à nuvem que não terminou, que fica),
  `historico-so-envio` (só a do envio, sem o botão), `historico-de-novo` (a pergunta com a cópia de um
  OK anterior) e `historico-velho` (o Agent de antes da F10). O Apagar liga a regra no Agent de mentira, e a Privacidade
  ganha a linha. São só do `pnpm dev`: nenhum build as inclui.
- **O teste manual** (F10f, 08-out-2026): `docs/teste-manual-nuvem-f10.md` monta o Agent novo no
  `bin\`, mostra o bloco e a contagem e deixa a decisão com quem usa (pode parar na contagem). Sem
  ditado com mais de 6 meses (o caso provável até março de 2027), o bloco só aparece pelas cópias, ou
  nem aparece, e o roteiro manda aos cenários acima para ver a pergunta e o OK.

### Modelos
- installed;
- version;
- size;
- checksum;
- benchmark;
- activate/remove/download.

### Dispositivos
- name;
- platform;
- version;
- last seen;
- revoke.

### Trash
- texto;
- deleted_at;
- days to purge;
- restore;
- delete permanently.

## Celular (PWA) e o painel em modo nuvem (ADR-019)
Etapa 5, Android primeiro. É o mesmo painel, no build da nuvem, com as telas reduzidas para o
celular. "O menor que funciona" (`PRODUCT.md`): a referência é o Wispr Flow, sem janela nova nem
modal onde um aviso basta. Mesma paleta, mesmas fontes, o símbolo como ícone. **Textos finais da F4
(05-out-2026), abaixo**; o código é a fonte da verdade (`web/src/nuvem/`, `web/src/Painel.tsx` e
`web/src/ItemDitado.tsx`), e o roteiro de quem testa é o `docs/teste-manual-nuvem-f4.md`. O app
instalado (F5): "O app instalado (F5)", abaixo, e o `docs/teste-manual-nuvem-f5.md`.

**Como a F4 fez (ADR-019, "Decisões da F4"):** o mesmo código em dois builds (`vite build` e `vite
build --mode nuvem`); o Painel recebe um "ambiente" do modo (`web/src/ambiente.ts`) com o cliente, a
tela de Configurações, a faixa e os textos que mudam. Visual: os tokens do `index.css`, claro e escuro;
Inter no corpo, Space Grotesk nos títulos, IBM Plex Mono só em hora, data e etiqueta; verde só no botão
principal e na vista escolhida; vermelho só no risco, sempre com a palavra. De 320 a 430 px e no PC;
com ponteiro de toque, os botões (as abas inclusive) e a busca têm 44 px.

### O que tem
- **Histórico**, com busca, editar, apagar e **Lixeira** (restaurar e **Excluir definitivamente**).
- **Aprendido:** a lista do que os PCs aprenderam, com **Desfazer**. Não tem botão Aprender: aprender
  continua só no PC (ADR-017). Entrou na F8h (07-out-2026), nas Configurações do celular
  (`web/src/nuvem/AprendidoNaNuvem.tsx`; "Configurações do celular", abaixo).
- **Configurações do celular** (abaixo).
- **Ditar pelo celular** (ADR-021, C6a e C6b): o microfone ao lado da busca, a faixa da onda e o ditado
  que chega no topo ("Ditar pelo celular", abaixo); a **folha do aceite**, que vem antes do microfone
  ("A folha do aceite", abaixo); e, nas Configurações, o estado, o provedor, a chave, o modelo, a dica e
  o Polish ("Ditar pelo celular", em "Configurações do celular").
- Não tem: a chave de API **do PC**, atalho, modo de texto e as outras preferências dos PCs.
- Salvar uma edição só diz "Texto salvo.": a nuvem nunca sugere aprender.
- O histórico vazio diz "Nenhum ditado ainda." e "O que você ditar num PC conectado aparece aqui."

### Abrindo
- Enquanto a nuvem não diz quem está entrando (`GET /api/v1/me`), nada; se demorar mais de 300 ms
  (o celular numa rede lenta), a Aurora e "Abrindo…", num `role="status"`. Sem título nem foco: a
  tela que vier depois leva o foco ao título dela.

### Entrar
- Tela única, numa coluna no meio: **a Aurora** (96 px, como a tela de abrir pela bandeja), "Aurora
  Whisper", "Entre para ver o que você ditou." e o botão **Entrar com o Google** ("Abrindo o Google…"
  enquanto vai; quem volta do Google pelo Voltar, com a página vinda do cache, acha o botão valendo
  de novo). **[Decisão 3 do plano da F4: a ilustração da Aurora no lugar do símbolo que este
  documento citava; a confirmar pelo Mauro.]** Sem o logo do Google: o app não carrega nada de fora.
- Erro, num `role="alert"`: "Não deu para entrar. Tente de novo." (rede, servidor ou recusa); no
  limite de tentativas (`429`), "Muitas tentativas. Espere um minuto e tente de novo."
- Recusada (conta fora da lista): a página fixa do servidor, no mesmo visual, "Esta conta não tem
  acesso a esta nuvem.", "Nada foi criado com ela." e **Entrar com outra conta**. O login que venceu,
  foi cancelado ou falhou: "Não deu para entrar.", "O login venceu, foi cancelado ou não terminou." e
  **Tentar de novo**.
- **Só no servidor de teste** (build tag `logindeteste`, só no PC): o botão **Entrar com a conta de
  teste**, com a ajuda "Só neste PC, sem Google.". A tela pergunta uma vez (`GET /auth/teste`), e só
  com a página em `localhost` (o servidor de teste só sobe nele); a resposta `404` quer dizer que o
  botão não existe. Fora de `localhost` (todo servidor de verdade), a tela nem pergunta, e o botão do
  Google aparece junto com o título. No servidor de teste sem o Google, o botão do Google some.

### Sem conexão
- Na abertura (a página carregou, mas a nuvem não respondeu): a tela inteira, "Sem conexão.", "O que
  você ditou continua no PC." e **Tentar de novo** ("Tentando…" enquanto vai). Outro erro na abertura:
  "Não deu para abrir.", "Tente de novo daqui a pouco." e **Tentar de novo**.
- No meio do uso: a faixa do topo, "**Sem conexão.** O que você ditou continua no PC.", com **Tentar
  de novo**; a lista e os editores ficam na tela. Nenhum texto ditado fica guardado no aparelho.
- Uma edição que não chegou ao servidor não é guardada: o editor diz "Sem conexão: a edição não foi
  salva." e mantém o texto na tela até a pessoa tentar de novo ou sair.
- **F5 (05-out-2026):** o app abre também sem rede nenhuma: o service worker sobe a casca guardada, o
  `GET /api/v1/me` falha, e a tela é a de cima, "Sem conexão.", no lugar da página de erro do
  navegador. Nenhum ditado aparece: o celular não guarda ditado.

### O app instalado (F5)
- **Nome:** "Aurora Whisper" no Iniciar do Windows, na janela e nas listas de apps; **"Whisper"**
  embaixo do ícone, na tela inicial do celular (o `short_name`: muitos launchers cortam nomes longos).
  **[Decisão do Mauro, 05-out-2026.]**
- **Ícone:** o mesmo do .exe, da aba e da bandeja: o ladrilho escuro (`#070a09`) com o Pico estourado
  (os colchetes verdes e a barra off-white). No Android, a versão "maskable": o quadrado cheio, que o
  launcher recorta no formato dele (círculo, squircle, quadrado arredondado), com o símbolo dentro da
  zona segura (raio de 40%), para nada sair cortado. **[Decisão do Mauro, 05-out-2026.]**
- **Cores:** a tela de abertura e a cor do app instalado são `#070a09` nos dois temas (no tema claro,
  a abertura pisca escura antes de o app claro aparecer); a barra do sistema segue o tema do aparelho
  (`#f5f7f5` no claro, `#070a09` no escuro), pelas metas `theme-color` da página. **[Decisão do
  Mauro, 05-out-2026.]**
- **Janela própria** (`standalone`), sem barra de endereço. Abre na lista (o `start_url` é `/`), ou em
  Entrar, sem sessão.
- **Versão nova:** com rede, a página vem sempre do servidor, então abrir o app já traz a versão nova.
  Não há aviso de "nova versão" (UI pequena); uma janela aberta há dias continua na versão de quando
  abriu, até ser reaberta.
- O painel do PC nunca é instalável: sem manifest e sem service worker (o Agent responde `404` em
  `/sw.js`).

### Sessão expirada
- Um `401` no meio do uso, **sem texto não salvo na tela**, troca a tela inteira por: "Sua sessão
  terminou.", "Entre de novo para ver seu histórico." e **Entrar com o Google** (e, no servidor de
  teste, o da conta de teste).
- **Com um texto não salvo na tela** (o editor aberto com uma mudança, ou uma linha com "não salvo";
  decisão do Mauro, 05-out-2026: "Guarda até entrar de novo"), a tela fica, com o editor e o texto, e
  a faixa do topo diz "**Sua sessão terminou.** Seu texto continua aqui; entre de novo para salvar.",
  com **Entrar de novo**. O texto fica só na memória da página, nunca no aparelho. O editor aberto
  sem mexer não conta.
  - **Entrar de novo** abre o login numa janela à parte (no Android, outra aba), no endereço do
    próprio app, que vai ao Google. Na volta, a janela diz "Você entrou de novo." / "Pode fechar esta
    janela e voltar ao seu texto." e se fecha sozinha quando o navegador deixa. A página do texto não
    sai do lugar.
  - Quando a mesma conta volta (a aba confere ao ganhar o foco, ao voltar a ficar à vista e com o
    aviso da janela), a faixa sai, o aviso diz "Você entrou de novo. Pode salvar.", e o foco vai
    para o editor (ou para a linha com "não salvo"). **Salvar** funciona.
  - Outra conta na janela: "**Você entrou em outra conta.** Este texto é de {e-mail}: entre com ela
    para salvar." Enquanto a faixa está na tela, o painel não fala com a nuvem.
  - Janela bloqueada: "**Seu navegador bloqueou a janela.** Copie o texto antes de entrar de novo nesta
    aba.", com o foco em **Copiar o texto** (depois, "Texto copiado. Agora pode entrar de novo nesta
    aba." ou "Não consegui copiar. Selecione o texto e copie antes de entrar de novo nesta aba.") e
    **Entrar nesta aba** (a página vai ao Google; o texto que não foi copiado se perde).
  - No servidor de teste, **Entrar com a conta de teste** fica na faixa e entra sem janela (sem o
    Google no servidor, só ele).
  - A frase da faixa é um `role="alert"` (anuncia ao aparecer e a cada troca); o foco não sai do
    editor quando ela aparece; com o toque, os botões da faixa têm 44 px.
  - **A pergunta do navegador antes de sair.** Com a faixa na tela e texto nela, recarregar, fechar
    a aba, o puxar para atualizar do Android ou um navegador que abre a janela de entrar na mesma
    aba fazem o navegador perguntar antes de a página sair (o diálogo é dele, sem texto nosso). Sem
    nada a perder (a pessoa apagou o texto) e depois de entrar de novo, a página sai sem perguntar.
    **Entrar nesta aba** é a exceção: é a escolha de perder o texto, e a faixa disse antes.
  - **Pouca altura** (até 560 px: o teclado aberto numa janela que encolhe com ele, o celular
    deitado): a faixa com a frase de sempre fica numa linha só, o título e o botão (58 px em vez de
    117 num celular de 375 px); a frase continua para o leitor de tela. A da janela bloqueada, a da
    outra conta e a do erro dizem o que fazer, e ficam inteiras.
  - A página da janela, "Você entrou de novo.", tem o link **Abrir o histórico**, para quando o
    navegador não a fechar (ou a abriu na mesma aba).
  - **Um ditado gravado e ainda não enviado** (ADR-021, C6a) conta como texto não salvo: o `401`
    não troca a tela inteira, e a faixa diz "**Sua sessão terminou.** Seu ditado continua aqui; entre
    de novo para enviar." (com texto também: "Seu texto e seu ditado continuam aqui; entre de novo
    para salvar."). Não há o que copiar de um áudio: com a janela bloqueada, a faixa diz "Entrar
    nesta aba descarta o ditado que não foi enviado." e só oferece **Entrar nesta aba**. A página
    também não sai sem perguntar. Depois de entrar de novo, **Tentar de novo** reenvia o áudio com o
    mesmo id.
- Cada tela inteira leva o foco ao título dela, para o leitor de tela anunciar a troca. Ao entrar,
  ou quando o **Tentar de novo** abre o histórico, o botão some com a tela, e o foco vai para o
  título do histórico ("Histórico", que só o leitor de tela vê). Aberto com a sessão do cookie, o
  foco fica onde o navegador o pôs.

### Ditar pelo celular (ADR-021, C6a e C6b)
O ditado do celular entra pelo **microfone** do app: toque para começar, toque para parar. A voz vai
ao servidor, que a repassa ao provedor que a pessoa escolheu; o texto volta como um ditado do
histórico, e desce aos PCs pelo sync. "O menor que funciona", com forma e movimento no lugar de
texto (referência do Mauro: o Wispr Flow). Tudo em tons de cinza: o verde é da ação, e o vermelho, do
erro (sempre com a frase); gravar não é nenhum dos dois. **O que está aqui é o gravador e o envio
(C6a). A porta do aceite antes do microfone, a folha, a chave e as preferências (C6b) estão nas duas
seções seguintes; a calibração do limiar e da onda no Android (C6c), no fim deste bloco.**
- **A porta antes do microfone (C6b).** O toque no microfone confere a conta antes de abrir o
  `getUserMedia` (um `GET /api/v1/dictation`): **sem o aceite do aviso para os provedores das
  preferências, o microfone nem abre, a chave não é pedida e nada é enviado**. Sem o aceite, o toque
  mostra a folha; sem o "Aceito", fica assim. Com o aceite e a chave na conta (o caso de todo ditado
  depois do primeiro), a porta passa direto, e a conferência não se repete nesta página: o ditado sem
  rede continua gravando, e o áudio espera o "Tentar de novo". Sem rede **no primeiro toque**, ou sem a
  sessão, o microfone não abre ("Sem conexão." na pílula; a tela da sessão): sem saber do aceite, não
  abre. O servidor sem o ditado (`404`, sem a chave de cifra): "Este servidor não tem o ditado pelo
  celular.".
  - **Depois da folha, o microfone não começa sozinho** (a pessoa acabou de ler um aviso longo): a
    pílula diz "Aceito. Toque no microfone para ditar.", e o toque seguinte grava.
  - **Sem a chave** (depois do aceite): a pílula diz "Falta a chave da OpenAI. Cole a chave nas
    Configurações." (o nome é o do provedor), e o app abre as Configurações **na seção "Ditar pelo
    celular"**, com o foco no título dela. A chave é colada ali, no lugar de sempre. Se o servidor
    guardava uma chave e já não a decifra (a chave de cifra trocou, ou o backup voltou sem ela:
    `colar_de_novo` no `GET /dictation`), a pílula diz "Cole a chave da OpenAI de novo nas
    Configurações.", e a seção diz o mesmo.
  - O que a porta sabia deixa de valer quando a pessoa muda algo nas Configurações (o aceite, o
    provedor, a chave, "Desligar", "Apagar meus dados") e quando o servidor recusa um ditado por
    `SEM_ACEITE`, `SEM_CHAVE` ou `PROVEDOR_RECUSOU_CHAVE`.
- **O botão:** um quadrado pequeno ao lado da busca, no cabeçalho (40 px; 44 px no toque), com o
  ícone de microfone, "Ditar" no rótulo e no `title`. Só no app da nuvem, e só se o navegador grava
  (`getUserMedia`, `MediaRecorder` e `AudioContext`): sem isso, o microfone nem aparece. Nas
  Configurações ele some, a não ser que haja uma gravação ou um áudio à espera.
- **Gravando:** a linha da busca vira uma faixa do mesmo tamanho e contorno, com a **onda**, o
  **tempo** (`m:ss`, na IBM Plex Mono) e o **Cancelar**; o microfone vira o botão de **parar** (um
  quadrado cheio, "Parar e enviar"). "Gravando" só o leitor de tela ouve (região viva); a onda e o
  tempo ficam escondidos dele. O foco fica no mesmo botão do começo ao fim. O Esc na faixa também
  cancela.
- **A onda** é a do overlay do PC (ADR-014): 33 barras de 2 px com 1 px entre elas, espelhadas, em
  cinza, o brilho acompanhando a altura, cada uma resumindo uns 39 ms da janela de 1,3 s pelo
  **pico**. O nível vem do `AnalyserNode` (RMS, em dBFS), e a altura espalha de -48 a -14 dBFS sobre o
  curso da barra. **Esses limites são os do microfone do PC: a C6c os recalibra no Android, e a onda
  é renderizada antes de concluir.** Em celulares de até 340 px as 5 barras mais antigas saem da
  vista, para a onda inteira caber ao lado do tempo e do Cancelar.
- **Cancelar** descarta a gravação sem enviar: nada sai do aparelho.
- **Parando:** o microfone desliga na hora. Com menos de **300 ms de fala**, sem contar os últimos
  200 ms (o toque), o app não envia: o aviso da pílula diz "Não ouvi nada.". Com fala, a faixa diz
  "Transcrevendo…" e o botão fica ocupado (`aria-disabled`, rótulo "Transcrevendo"): gravar o próximo
  enquanto o anterior transcreve fica fora. A medição de fala usa o mesmo `AnalyserNode`, com o RMS
  de 0,03 do PC como limiar de partida (a C6c calibra).
- **O teto** é de 120 s: o app para sozinho e envia o que gravou; o aviso do ditado que chega diz
  "Parei no limite de 2 minutos.".
- **Tela apagada e segundo plano.** O app pede a tela acesa (Wake Lock) ao começar e a solta ao parar;
  se o pedido não vem, a faixa avisa, embaixo, em texto pequeno: "A tela pode apagar e parar o
  ditado.". Se a página sai da frente (`visibilitychange`), ou a faixa do microfone fica muda ou acaba
  (uma ligação recebida), o app **para e envia o que já gravou**, se houver fala, e diz "Parei quando o
  app saiu da frente." ou "O microfone foi interrompido." (no aviso do ditado que chegou, ou, sem
  fala, antes do "Não ouvi nada."). **No passo 1 não existe ditado em segundo plano nem com a tela
  apagada.**
- **Quando chega:** o ditado entra **no topo do histórico, em destaque** (o fundo do que entrou, mais a
  etiqueta "novo", que dura 10 s ou até a pessoa abrir uma linha ou trocar de vista), com o **Copiar**
  que toda linha tem (sempre à vista no toque), e o aviso diz "Ditado no topo do histórico.". Se o
  Polish caiu para o texto do Smart, o aviso acrescenta "O Polish não agiu: {motivo}." (o motivo é um
  código do servidor, nunca texto dele; o que o app não conhece fica só "O Polish não agiu."). Numa
  busca, na Lixeira ou nas Configurações o ditado não entra na lista que está na tela, e o aviso diz
  "Ditado salvo no histórico.". **Copiar sozinho ao chegar não existe**: escrever na área de
  transferência pede um gesto recente, e a resposta chega segundos depois do toque (escolha menor 14,
  a conferir no aparelho).
- **As falhas** (o app decide pelo `error.code`, nunca pela frase do servidor). As que **não guardam o
  áudio** viram o aviso da pílula, e a linha volta ao normal:

  | Situação | Frase |
  |---|---|
  | permissão negada | "Sem acesso ao microfone. Libere nas permissões do site." |
  | sem microfone, em uso por outro app, sem suporte, sem webm/opus | "Não achei um microfone." / "Não consegui abrir o microfone. Outro app pode estar usando." / "Este navegador não grava áudio." / "Este navegador não grava no formato certo." |
  | `SEM_FALA` ou menos de 300 ms de fala | "Não ouvi nada." |
  | `PAYLOAD_TOO_LARGE` | "Ficou longo demais." |
  | `FORMATO` | "Não deu para ler o áudio." |
  | `APAGADO` | "Esse ditado foi apagado." |
  | `404` (o servidor sem a chave de cifra) | "Este servidor não tem o ditado pelo celular." |

  As que **guardam o áudio** na memória (só aí, até **Tentar de novo** ou **Descartar**, e some ao
  fechar o app) abrem uma **nota de erro** embaixo da linha, com a frase e os botões **Tentar de
  novo** (reenvia com o **mesmo id**: se o primeiro envio chegou, o servidor devolve o ditado já
  gravado, sem cobrar de novo) e **Descartar**. Enquanto o áudio espera, o microfone fica ocupado: não
  se grava por cima dele.

  | Situação | Frase |
  |---|---|
  | sem rede | "Sem conexão." |
  | os 90 s do envio estouraram | "O envio demorou demais." |
  | `401` | "O ditado espera você entrar de novo." (e a faixa da sessão, acima) |
  | `UM_POR_VEZ`, `LIMITE` ou `OCUPADO` (`429`/`503`) | "Espere um pouco. Tente de novo em {n} s." (o `Retry-After`) |
  | `PROVEDOR_LIMITE` ou `PROVEDOR_FORA` | "O provedor não respondeu agora." |
  | `PROVEDOR_RECUSOU_CHAVE` | "O provedor recusou a chave." e **Abrir as Configurações** |
  | `SEM_CHAVE` | "Falta a chave do provedor." e **Abrir as Configurações** |
  | `SEM_ACEITE` | "Falta aceitar o aviso do ditado pelo celular." e **Ver o aviso** |
  | qualquer outra | "Não deu para enviar o ditado." |

  **Abrir as Configurações** (a chave recusada ou que falta) leva à seção "Ditar pelo celular", com o
  foco no título dela, e o áudio espera na nota, à vista nas Configurações. **Ver o aviso** (o aceite
  acabou em outro aparelho, ou o aviso mudou, com o áudio já gravado) abre a folha: aceitando, o áudio
  que esperava é reenviado **com o mesmo id** e chega; "Agora não" o deixa na nota, com **Tentar de
  novo** e **Descartar**.
- **Só na memória.** O áudio vive numa variável do gravador, do fim da gravação até o envio dar certo
  ou a pessoa descartar. Nada em `IndexedDB`, `localStorage`, `Cache` ou arquivo; o `bundle.test.ts`
  trava isso, e que o gravador só existe no build da nuvem. O `MediaRecorder` entrega um `Blob` que o
  Chrome pode paginar em disco: não verificado (ADR-021, Riscos).
- **O cabeçalho** `Permissions-Policy: microphone=(self)` sai em toda resposta do servidor da nuvem
  (escolha menor 18).
- **Os testes** usam dublês do `getUserMedia`, do `MediaRecorder`, do Wake Lock e da área de
  transferência: nenhum abre o microfone de verdade.

### A folha do aceite (ADR-021, decisão H; C6b)
**Uma folha só, em destaque, específica e separada das outras finalidades**, que aparece **no primeiro
toque no microfone, antes de o microfone abrir e antes de pedir a chave**, e de novo quando muda o que a
pessoa aceitou (trocar de provedor, ligar o Polish com outro provedor, ou o texto do aviso mudar: **a
versão do aviso**). O texto é o da D0 (`docs/politica-de-privacidade.md`); **qualquer mudança nele muda a
versão** e pede o aceite de novo.
- **A versão do aviso** é **um inteiro, e uma só no app**: `VERSAO_DO_AVISO`
  (`web/src/nuvem/avisoDoDitado.ts`), presa ao `cloudstore.VersaoDoAviso` do servidor pelo teste do Go
  `TestVersaoDoAviso_AppEServidorConferem` (subir uma sem a outra quebra a suíte). O app a manda no
  `POST /api/v1/dictation/consent`, e o servidor só aceita a atual. **O texto e a versão andam juntos:**
  o `avisoDoDitado.test.ts` guarda uma impressão do que a folha mostra (os blocos de cada provedor e o
  texto dela) para cada versão, e quebra se o texto mudar sem a versão. Subir o aviso é mudar o texto,
  os dois números e acrescentar a impressão da versão nova.
- **O aviso do servidor é mais novo que o do app** (o app abriu antes de um deploy que mudou o texto):
  o `POST` do aceite volta `409` `AVISO_DESATUALIZADO`, nada é gravado, **a folha fecha sem aceite e o
  app recarrega** (a página é lida de novo, e a folha seguinte mostra o texto novo). Um ditado gravado e
  ainda não enviado, que esperava na memória, **se perde** com o recarregar (limite conhecido: o
  recarregar é da página, e o áudio só mora na memória dela).
- **O aceite do servidor tem dois lugares**, o provedor da transcrição e o do Polish (o do Polish pode
  faltar), e cada aceite novo substitui o anterior: **o que a folha grava é só o que ela mostrou**
  (a voz e, com o Polish, o provedor dele), e nada do que valia antes é carregado para o aceite novo.
  Trocar a voz da OpenAI para a Groq tira a OpenAI do aceite, e voltar a ela pede a folha de novo. O
  aceite cobre o conjunto dos dois provedores, seja qual for o uso. **`vigente`** (do `GET
  /dictation`) é o que diz se o aceite vale com o aviso de agora; com `vigente: false` a folha abre de
  novo e a seção diz "Desligado. O aviso mudou…".
- **A tela.** Uma folha colada embaixo no celular (no PC, centrada, com no máximo 40 rem), por cima da
  tela inteira, com um fundo escuro que só escurece o que está atrás. O texto rola dentro dela; **os
  botões ficam sempre à vista**, no rodapé. É um diálogo modal (`role="dialog"`, `aria-modal`): o foco
  nasce no título, o Tab não sai da folha, o **Esc é "Agora não"**, e o foco volta ao que o tinha (o
  microfone, o seletor de provedor).
- **O título:** "Ditar pelo celular manda a sua voz a um provedor que não diz processar no Brasil."
  Depois: "Leia antes de aceitar. Por isso tratamos o envio como uma transferência internacional. Se você
  tocar em "Agora não", o microfone não abre, nada é enviado e o resto do app continua igual."
- **O provedor da voz** (só no primeiro uso, a pedido do app): "OpenAI" e "Groq", duas opções de rádio, a
  OpenAI marcada. O bloco do provedor, mais abaixo, muda com a escolha.
- **O que vai:**
  - **A voz de cada ditado.** Passa pelo servidor desta nuvem, **só na memória**, e vai ao provedor
    escolhido. **Não é gravada no aparelho nem no servidor.** O provedor pode guardá-la: veja abaixo.
  - **A sua chave de API.** Colada uma vez. O servidor a guarda cifrada, mas **quem administra o servidor
    consegue decifrá-la**. Use uma chave só para o celular, com limite de gasto no provedor.
  - **Se ligar a dica:** os termos que a pessoa ensinou, junto com a voz, em todo ditado; **podem ser
    nomes de clientes ou de outras pessoas**. A dica começa desligada.
  - **Se ligar o Polish:** o texto do ditado, para a OpenAI ou a Anthropic, com a chave dela. O Polish
    começa desligado. (Ao ligá-lo, a folha diz "O Polish que você está ligando" e o nome do provedor.)
- **Para onde, e por quanto tempo fica lá:** um bloco por provedor em uso, com **só o que a documentação
  dele diz** (lida em 10-out-2026; a Anthropic, em 29-set-2026, não relida): a retenção (até 30 dias), o
  treino, e **o país só quando a página afirma**. A OpenAI processa nos EUA ou na Europa **se a conta
  fixou a residência de dados**, e senão a documentação não diz o país. A Groq retém nos EUA o que guarda,
  e a documentação não diz onde a transcrição roda. O aviso diz que não confere a conta de quem dita (a
  residência de dados e a retenção zero).
- **Dados de outras pessoas:** "A voz e os termos podem ter dados de outras pessoas (clientes, quem está
  perto de você). O seu aceite vale por você, e não por elas. Se você dita dado de cliente, veja se isso
  cabe no que você deve a ele."
- **O que isto não faz:** não funciona com a tela apagada nem com o app em segundo plano (se a tela
  apagar, o ditado para), e a voz não é usada para identificar a pessoa.
- **Você pode desfazer:** em Configurações, "Desligar o ditado no celular" tira o aceite e apaga as
  chaves do servidor; revogue também a chave no provedor, porque uma cópia cifrada dela fica nos backups
  por cerca de 30 dias.
- **Os botões:** **Aceito** (o primário) e **Agora não**. O Aceito grava o aceite **no servidor, por
  conta** (`POST /api/v1/dictation/consent`, com `{ versao, transcricao, polish }`: a versão do aviso, o
  provedor da voz e, com o Polish, o dele) e só então fecha; durante, diz "Guardando…" e o resto espera. Sem rede, a
  folha fica, com "Sem conexão. Tente de novo."; as outras falhas dizem "Não deu para guardar o aceite.
  Tente de novo." (a frase é nossa, nunca a do servidor); a sessão que acabou fecha a folha e acende a
  faixa da sessão. **Agora não** não grava nada.
- **Só ler:** "Ler o aviso", nas Configurações, reabre a folha de quem já aceitou, só para reler: "Você
  já aceitou este aviso; ele está aqui para você reler.", e um botão só, **Fechar**.

### Ditar pelo celular nas Configurações (ADR-021, C6b)
A seção **"Ditar pelo celular"** fica **logo depois da Conta** (é o que o app faz). Tudo age na hora, sem
"Salvar": cada mudança é um `PUT /api/v1/dictation/settings` com as preferências inteiras
(`{ transcricao, dica, polish }`, o formato do `06`), e nada disso
desce aos PCs. A seção diz de saída: "Fale no celular e o texto entra no histórico. A voz vai ao
provedor que você escolher, com a sua chave. O ditado não funciona com a tela apagada nem com o app em
segundo plano."
- **O estado:** "Desligado." (com **Ligar**, o botão primário, que abre a folha com a escolha do
  provedor), "Ligado desde 10/10/2026." (a data do aceite; com **Ler o aviso** e **Desligar**) ou, se o
  aceite é de um aviso velho, "Desligado. O aviso mudou: leia e aceite de novo para ligar.". **Sem o
  aceite, só o estado e o Ligar aparecem:** nada de provedor, chave nem modelo.
- **Desligar** pergunta ali mesmo (o padrão do Revogar): "Desligar o ditado no celular. Isto tira o seu
  aceite e apaga as suas chaves do servidor. Não apaga os ditados já feitos. Revogue as chaves no
  provedor.", com **Desligar** (botão de risco) e **Cancelar** (o foco vai para o Cancelar; o Esc cancela
  e devolve o foco ao Desligar). Feito: o aviso "Ditar pelo celular desligado.", e o foco vai para o
  título da seção. Como o aceite acabou, o microfone volta a pedir a folha.
- **Provedor da voz:** uma lista, "OpenAI" ou "Groq". **Trocar para um provedor fora do aceite reabre a
  folha** (sem a escolha do provedor, que já foi feita ali), e sem o "Aceito" nada muda (a lista volta ao
  que era). O aceite novo é **só o que a folha mostrou** (a voz nova e, com o Polish ligado, o provedor
  dele): o provedor de antes sai do aceite. Trocar para um provedor que já está no aceite (nos dois
  lugares dele) não pede nada. O modelo volta ao padrão do provedor novo.
- **O servidor que recusa por falta de aceite** (`SEM_ACEITE` no `PUT` das preferências: o aceite acabou
  em outro aparelho) **também abre a folha**: o app relê a conta, a folha abre com o aceite que vale de
  verdade, e, aceitando, o `PUT` é refeito. Sem o "Aceito", a seção passa a dizer o que o servidor diz
  ("Desligado. O aviso mudou…"). O mesmo vale ao guardar uma chave: a chave colada fica no campo enquanto
  a folha está aberta, e vale se a pessoa aceitar.
- **A chave** (uma por provedor, a do provedor da voz aqui e, com o Polish, a do dele):
  - Sem chave: "Chave da OpenAI", "Nenhuma chave guardada.", um campo de senha ("Cole a chave aqui", sem
    preenchimento automático) e **Guardar chave** (só vale com algo colado; o Enter também guarda), e
    a ajuda "Use uma chave só para o celular, com limite de gasto no provedor.".
  - **A chave vai só no corpo do `PUT /api/v1/ai-keys/{provedor}`, nunca na URL, e não volta:** o servidor
    a testa no provedor, a cifra e devolve só `configurada` e o final. O campo esvazia ao guardar.
  - **A chave que o servidor guardava e já não decifra** (`colar_de_novo`: a chave de cifra trocou, ou
    o backup voltou sem ela): "A chave guardada não vale mais neste servidor. Cole a chave de novo.", com
    o campo à mão (como sem chave), sem Trocar nem Remover e sem ler a lista de modelos. Guardar a
    chave nova a substitui.
  - Guardada: **"Chave terminada em WXYZ."** (só o final, de **4 caracteres no máximo**, mesmo que o
    servidor mande mais), com **Trocar** e **Remover**. **Trocar** abre o campo com "Revogue também esta
    chave no provedor. Uma cópia cifrada dela fica nos backups por cerca de 30 dias." e **Cancelar**;
    **Remover** pergunta ali mesmo, com o mesmo lembrete, **Remover** (risco) e **Cancelar** (o foco vai
    para o Cancelar; o Esc cancela). Feito: "Chave guardada." / "Chave removida.", e o foco vai para o
    controle que ocupou o lugar.
  - **Os erros** têm frase nossa, escolhida pelo `error.code` ou pelo status, **nunca pela mensagem do
    servidor** (que pode trazer um pedaço da chave): "O provedor recusou a chave." (`PROVEDOR_RECUSOU_CHAVE`),
    "O provedor não respondeu agora. Tente de novo." (`PROVEDOR_LIMITE`, `PROVEDOR_FORA`), "Espere um
    pouco. Tente de novo em N s." (`LIMITE`, `429`), "Essa chave não parece válida. Confira e cole de
    novo." (`400`, `413` ou `422`), "Este servidor não tem o ditado pelo celular." (`404`) e "Não deu para
    guardar a chave.". A chave que falhou continua no campo, para conferir. Sem rede ou sem sessão, a
    faixa de sempre.
- **Modelo da voz:** "Padrão do provedor" (o `PUT` leva o modelo vazio e o servidor resolve: escolhas
  menores 1 e 2) e os modelos da conta, lidos com a chave (`GET /api/v1/ai-keys/{provedor}/models`) e
  **filtrados no app**: os de voz são os que têm "transcribe" ou "whisper" no nome; os do Polish, os
  outros. **A conta guarda sempre o modelo de verdade**, pelo nome (o `gpt-4o-mini-transcribe` na OpenAI,
  o `whisper-large-v3-turbo` na Groq): é ele que a lista mostra marcado, e escolher "Padrão do provedor"
  de volta mostra o nome do padrão. Sem chave: "Cole a chave para ver os modelos da sua conta." e o
  campo fechado, com o modelo da conta; a lista que não veio: "Não deu para ler os modelos. Vale o
  padrão do provedor.". O modelo que a conta tem e a lista não mostra continua escolhível. Mudar só o
  modelo não pede o aceite de novo.
- **A dica:** a caixa "Mandar os termos aprendidos como dica", **desligada**, com "Eles vão ao provedor
  junto com a voz, em todo ditado, e podem ser nomes de clientes ou de outras pessoas.".
- **O Polish:** a caixa "Organizar o texto com o Polish", **desligada**, com "O texto do ditado vai ao
  provedor do Polish.". Ligado: **Provedor do Polish** ("OpenAI" ou "Anthropic"; um provedor fora do
  aceite reabre a folha, que mostra o bloco dele), a chave dele (com a OpenAI, "A chave da OpenAI, acima,
  também serve ao Polish.") e **Modelo do Polish**. Ligar com um provedor fora do aceite abre a folha, e
  sem o "Aceito" continua desligado.
  - **O Polish não tem modelo padrão, e o servidor só o liga na conta com um modelo.** Marcar a caixa
    mostra o provedor, a chave e o modelo, mas **não escreve nada na conta**: o modelo diz "Escolha um
    modelo" (que não se escolhe de volta) e a ajuda, "Escolha o modelo para o Polish agir nos seus
    ditados.". A conta passa a ter o Polish ligado **quando o modelo é escolhido** (um só `PUT`, com o
    Polish ligado e o modelo). Até lá a caixa fica marcada só na tela, e a conta continua com o Polish
    desligado; desmarcar o que ainda não tem modelo também não escreve nada, e a caixa marcada se perde
    se a conta mudar por fora ("Apagar meus dados") ou for desligada. Trocar o provedor do Polish
    zera o modelo (os modelos são de cada provedor): com o Polish ligado, a conta guarda o provedor novo
    com o Polish desligado, e a caixa continua marcada, esperando o modelo. Marcar de novo um Polish que
    já teve modelo (desligado depois, que o servidor guarda) liga na hora, com o modelo de antes. Sem
    lista de modelos (a chave por colar, ou a lista que não veio), a ajuda diz isso, sem prometer um
    padrão: "Não deu para ler os modelos. Tente de novo mais tarde.".
- **Os erros do salvar:** "Não deu para salvar. Tente de novo." (nunca a mensagem do servidor); a leitura
  que não veio: "Não deu para ver o ditado pelo celular. Tente de novo mais tarde.", e o `404`, "Este
  servidor não tem o ditado pelo celular.".
- **"Apagar meus dados da nuvem"** apaga também o aceite, as preferências e as chaves (o servidor, na
  C3b): a seção lê de novo e fica "Desligada", e o microfone volta a pedir a folha.

### A calibração do microfone no Android (ADR-021, C6c)
Uma página **só do `pnpm dev`** (`web/calibracao-dev.html`, como a `telas-dev-nuvem.html`), que **não está
em nenhum dos dois builds** (o `bundle.test.ts` prova: nem a página, nem o texto, nem o CSS dela, e nada
do app a importa). Serve a **calibrar, com os números do Android**, o limiar de fala (hoje RMS 0,03), a
onda (hoje -48 a -14 dBFS) e o bitrate (hoje 32 kbit/s), que partem dos valores do PC.
- **Como abrir** (com o OK do Mauro; ADR-021, "Pontos de parada"): `pnpm -C web dev`, o celular no cabo
  USB, e o encaminhamento de porta do `docs/teste-manual-nuvem-f4.md`, §9, **para a porta do Vite
  (5173), no lugar da 8080**; no Chrome do celular, `http://localhost:5173/calibracao-dev.html`.
  `localhost` é um contexto seguro: o microfone abre.
- **O que mede**, com **as mesmas restrições do `getUserMedia`** do gravador (o mesmo objeto,
  `restricoesDoMicrofone`) e **a mesma medição**: o nível sai do mesmo `AnalyserNode` (a mesma janela de
  1024 amostras, o RMS), a fala é somada pelo mesmo `MedidorDeFala` (o limiar e os 200 ms do fim), e o
  `MediaRecorder` grava no mesmo formato e bitrate. Duas medições: **o silêncio** (6 s) e **a fala**
  (12 s); cada uma refaz o resultado dela, e há **Parar agora** e **Zerar**.
- **O que mostra: só números.** Para cada medição: o nível por percentil (mínimo, p5 como o piso, p10 a
  p99 e o máximo como o pico), em RMS e em dBFS; o **tempo acima de cada limiar de RMS** (0,003 a 0,2, com
  o do app marcado); **a fala medida e se o app enviaria** (300 ms); a **distribuição do nível** em
  bandas de 6 dB (de -96 a 0 dBFS); os **bytes por segundo do `MediaRecorder`** (pedaços de 1 s, **só
  contados e jogados fora**: total, menor e maior pedaço, kbit/s, o que 120 s ocupariam contra o teto de
  1 MiB); e o que o navegador diz da captura (taxa, canais, eco, ruído, ganho, tipo, bitrate).
- **Nenhum arquivo, nenhum envio.** Sem botão de salvar, baixar, copiar ou enviar; sem rede, sem
  armazenamento, sem `Blob`, sem som. O `calibracao.test.ts` lê o código-fonte e recusa tudo isso. Quem
  calibra tira uma **captura da tela** e passa os números.
- **O que falta:** a **calibração com o aparelho é do Mauro**, depois; só então o limiar de fala, a
  onda e o `getUserMedia` (hoje no padrão do Chrome) são fixados com os números do celular, e a **onda é
  renderizada com eles antes de concluir**.

### Configurações do celular
- **Conta:** "Entrou como {e-mail}." e **Sair** (volta para Entrar). Com um texto não salvo na tela,
  o **Sair** pergunta antes, ali mesmo: "**Sair com texto não salvo** / Há texto que você escreveu e
  não salvou. Se você sair, ele se perde.", com **Sair e descartar o texto** e **Cancelar** (o foco
  vai para o Cancelar, e o Esc cancela). Com a faixa da sessão na tela, o Sair passa por fora do
  portão: o `POST /auth/logout` chega ao servidor.
- **Ditar pelo celular** (ADR-021, C6b; logo depois da Conta): o estado, o provedor da voz, a chave, o
  modelo, a dica e o Polish. Está em "Ditar pelo celular nas Configurações", acima.
- **Aparelhos** (F6, entre a Conta e as Sessões abertas): uma linha por PC, "{nome}" e
  "Windows · versão {v} · último uso {data}", com **Revogar**, que pergunta na própria linha:
  "Revogar {nome}? O PC para de sincronizar e continua funcionando nele.", com **Revogar** (botão de
  risco) e **Cancelar** (o foco vai para o Cancelar; o Esc cancela e devolve o foco ao Revogar da
  linha). Feito: o aviso "PC revogado.", e o foco vai para o Revogar que ficou no lugar, ou para o
  título da seção. Sem nenhum: "Nenhum PC conectado." O revogado some da lista (decisão 3 da F6). O
  "apagar meus dados" relê a lista.
- **A página de aprovação de um PC** (`/device/authorize?pedido=…`, F6), no estilo das telas inteiras
  (a Aurora, o título, uma linha): "Conectar este PC?" e "{nome} · Windows"; num quadro, "Código
  **K7QM**" (grande, em IBM Plex Mono), "Confira se é o mesmo do painel do PC.", "Este PC vai ficar
  ligado à conta **{e-mail da sessão}**." (o e-mail quebra em qualquer ponto no celular; quem está na
  conta errada vê antes de aprovar, porque o PC recusa trocar de conta depois) e, em texto forte, "Só
  aprove se você clicou em Conectar neste PC agora."; **Aprovar** (durante, "Aprovando…"; a página vai
  ao PC) e **Agora não** (depois: "Nada foi conectado." / "Pode fechar esta aba."; o pedido não é
  invalidado: recarregar mostra o Aprovar de novo até ele vencer, em 10 minutos). O pedido vencido,
  usado ou desconhecido: "Este pedido venceu ou já foi usado." / "Volte ao PC e clique em Conectar à
  nuvem de novo." Sem sessão: Entrar, com a linha "Entre para aprovar a conexão do PC.", e o login
  volta à aprovação. Os erros do Aprovar: "Sem conexão. Tente de novo." e "Não deu para aprovar. Tente
  de novo."
- **Aprendido** (F8h, entre os Aparelhos e as Sessões abertas): "Trocas que os PCs aprenderam ao
  corrigir ditados." e uma linha por troca, a variante e o termo na IBM Plex Mono, `sf → ECF` (para o
  leitor de tela: "sf vira ECF"), do mais recente para o mais antigo. Com domínio, uma segunda linha,
  pequena e cinza, "domínio contabilidade" (a troca presa a ele diz "só no domínio contabilidade"); sem
  domínio, nada. Cada linha tem **Desfazer**, que pergunta na própria linha (o padrão do Revogar, porque
  do celular não dá para aprender de novo): "Desfazer? A troca deixa de valer em todos os PCs.", com
  **Desfazer** (botão de risco) e **Cancelar** (o foco vai para o Cancelar; o Esc cancela e devolve o
  foco ao Desfazer da linha). Para o leitor de tela, o grupo da pergunta se chama "sf vira ECF.
  Desfazer? A troca deixa de valer em todos os PCs.": o foco cai no Cancelar, e o botão da linha, que
  dizia qual troca era, sai de cena (o texto que se vê não muda). Com o pedido no ar ("Desfazendo…"), o
  Cancelar e o Esc esperam (o Cancelar fica `aria-disabled`): desistir não aborta o POST, e a pergunta
  sumiria antes de a troca sair da lista; o Revogar dos Aparelhos é igual. Feito: o aviso "Desfeito em todos os aparelhos.", a linha sai, e o foco vai
  para o Desfazer que ficou no lugar, ou para o título da seção. Sem Aprender, e sem nenhuma troca:
  "Nada aprendido ainda." O texto da troca nunca vai na URL (o `id` vai no corpo do POST).
  - **O teto:** passou do que o servidor manda (`has_more`), uma linha, "Só as mais recentes aparecem.",
    em texto comum, sem alarme; a lista é lida de novo quando a tela aparece.
  - **Os erros**, no molde das outras seções: "Não deu para ver o aprendido: …" (a lista, num alerta, sem
    o "Carregando…" nem o "Nada aprendido ainda.", que seriam mentira) e "Não deu para desfazer: …" (a
    linha e a pergunta ficam, e dá para tentar de novo); a rede fora e a sessão que acabou vão para a faixa
    e a tela de sessão expirada, como em toda parte. O `404` com a frase do servidor ("troca aprendida não
    encontrada": a troca já não está aí, por exemplo desfeita em outro aparelho) tira a linha do mesmo
    jeito, com o mesmo aviso; um `404` sem essa frase (o painel publicado antes do binário do servidor,
    um proxy) é erro comum: "Não deu para desfazer: …", a linha e a pergunta ficam.
- **Sessões abertas:** "Perdeu o celular? Encerre a sessão dele aqui." Uma linha por sessão (por
  exemplo, "Android · Chrome", com "último uso 05/10, 14:32"); a atual leva a etiqueta "este
  aparelho" e não tem botão; as outras têm **Encerrar** (o aviso: "Sessão encerrada."). Sem outra:
  "Nenhuma outra sessão aberta."
- **Privacidade** (ADR-021, D0; o texto é o da `docs/politica-de-privacidade.md`):
  - "Vão para a nuvem: o texto dos ditados, com a data, o PC de origem e os detalhes da transcrição, e as
    trocas aprendidas. O texto fica legível para quem administra o servidor." (as trocas entraram na F8h)
  - "Ficam 6 meses (a lixeira, 30 dias). Uma cópia cifrada guarda tudo por cerca de 30 dias além disso."
  - "Do PC nunca vão: o áudio, o que está nas outras janelas, a chave do Polish e as configurações dos
    PCs; **a chave do PC continua no PC**." (A frase não cita onde a chave mora: o build da nuvem não
    pode trazer "Gerenciador de Credenciais", e o `bundle.test.ts` a recusa.)
  - "Ditar pelo celular é opcional. Se você ligar, a voz de cada ditado vai ao provedor que você escolher
    (OpenAI ou Groq), com a sua chave, que o servidor guarda cifrada. A voz não é gravada: fica na
    memória e é descartada ao fim."
  - Depois, **Apagar meus dados da nuvem** (botão de risco), que abre ali mesmo: "Apaga tudo desta conta
    na nuvem e desconecta os PCs. Os PCs ficam com a cópia deles. A cópia de segurança guarda por até
    cerca de 30 dias. Apaga também o aceite do ditado no celular, as preferências e as chaves.", o campo
    "Para confirmar, digite apagar", **Apagar tudo** (só funciona com "apagar" digitado) e **Cancelar**
    (Esc também). Feito: o aviso "Seus dados foram apagados da nuvem.", e o histórico lê de novo.

### O selo de conflito
- Quando o mesmo ditado foi mudado em dois lugares antes de sincronizar, a linha mostra a etiqueta
  discreta **"2 versões"**, a de sempre (texto secundário e borda de linha; nunca o vermelho de erro:
  nada se perdeu). O leitor de tela ouve "2 versões, abra para escolher". Só no Histórico: na
  Lixeira, a pessoa restaura antes.
- Com a linha aberta, em cima do editor: "Mudou em dois lugares. Ficou esta; a outra está guardada.",
  as duas versões, **"Ficou"** e **"A outra · {data e hora}"**, com as palavras que só uma tem
  sublinhadas (o `diff.ts`, sem cor), e **Ficar com esta** e **Ficar com a outra** (lado a lado no
  PC, um embaixo do outro no celular). Embaixo dos botões, "A que você não escolher é descartada.",
  que é também a descrição dos dois para o leitor de tela: a escolha não tem Desfazer, e "guardada"
  só vale até ela. O nome do aparelho entra na F6, com os aparelhos.
- Escolher é um `PATCH` com `resolver_conflito` (`06`); o aviso diz "Ficou a versão escolhida.", o
  selo some em todos os aparelhos (o PC recebe pelo pull), e o foco vai para o texto. Se outra aba ou
  aparelho mudou o ditado antes (`409`), ele é lido de novo, e o aviso diz "Este ditado mudou em outro
  lugar. Confira as versões e escolha de novo." (ou "A versão já foi escolhida em outro lugar.").
  **[Decisão 1 do plano da F4: escolher já na F4; a confirmar pelo Mauro.]**
- No PC, o mesmo selo, no histórico do painel: o componente é o mesmo, e os botões aparecem quando o
  Agent tiver a escolha (F7c). **F7c (06-out-2026):** os botões aparecem no PC, com os mesmos textos;
  a escolha vale na hora no PC e sobe pela fila; se outro aparelho escolheu antes (ou o texto mudou),
  ela cai na nuvem, e o pull traz de volta o que vale (com o selo, se ele ainda existe).

### Lixeira na nuvem
- Cada linha tem **Restaurar** e **Excluir definitivamente**. Excluir pergunta na própria linha, sem
  janela: "Excluir para sempre? Não dá para desfazer.", com **Excluir** (botão de risco) e
  **Cancelar**, e o foco já no Cancelar (Esc também cancela). Excluído: a linha some, o foco vai para
  a vizinha, e o aviso diz "Ditado excluído para sempre." Se não deu (restaurado em outro lugar), a
  pergunta fica e o aviso diz o porquê.

### Instalar
- No Android: o menu do Chrome, **Instalar app**. Sem prompt próprio na v1.
- No iPhone: **depois do MVP** (Compartilhar, depois "Adicionar à Tela de Início").

## Local x cloud
Local: Agent runtime, mic, hotkey, modelos, logs locais.
Cloud: histórico consolidado, dictionary, corrections, devices, syncable settings.

**Etapa 5 (ADR-019):** a nuvem tem o histórico consolidado, o aprendido (só o par curto), os
aparelhos e as sessões. Dicionário completo, correções e preferências ficam em cada PC; a nuvem
delas é "depois".

## Estados
Toda tela: loading, empty, error, success, offline, unauthorized.

No celular (ADR-019), `offline` é "Sem conexão" e `unauthorized` é "Sessão expirada" (acima).

## Acessibilidade
- teclado;
- foco visível;
- labels;
- contraste;
- aria-live para status.
