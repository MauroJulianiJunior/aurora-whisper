# Bandeja e painel — Roteiro de teste manual

Cobre o que os commits `73de148` a `574ed6f` puseram no Agent e que nenhum teste automatizado pode
exercitar: o ícone na bandeja, o menu, a pausa, a instância única, o início automático com o
Windows e o painel no navegador. Inclui as correções da revisão adversarial de 29-set-2026 e a
fase 2 da correção do falso "campo protegido" (B13, B14 e L2: `CUIAutomation8` com teto de 3 s,
vigia, leitura atrasada, janela de administrador e o nome do executável nos avisos), o dono fixo
do histórico, com a cópia do banco antes de migrar (B1a), e o microfone só enquanto dita da
ADR-015 (parte 6, M0 a M7).

Por que é manual: essas peças criam janela, gravam no registro e abrem o navegador, e o
`CLAUDE.md` proíbe um agente de fazer isso nesta máquina. Os testes automatizados cobrem as
decisões (o que o menu mostra, quando tentar o ícone de novo, se o registro aponta para este
executável, o bilhete); o que é chamada de sistema de verdade só se confere aqui. O roteiro
estava prometido desde o `73de148` e ficou fora do repositório até a revisão de 29-set-2026.

O ditado em si tem roteiro próprio: `docs/sprint-4-teste-manual.md`.

Tempo estimado: 30 minutos, mais um logon, mais uns 20 minutos para B13, B14 e L2, mais uns 20
para a parte 6.

> **Este roteiro mexe no registro** (início automático, B6 a B10) e abre o navegador — é
> exatamente o que ele testa. O B10 desfaz o início automático no fim, se você não quiser
> mantê-lo.

---

## Antes de começar

### Nenhum Agent rodando

No Gerenciador de Tarefas, **nenhum** `aurora-whisper-agent.exe`. Se houver, encerre pelo menu do
ícone → **Sair**. Com ele rodando, a montagem abaixo falha: o Windows não deixa sobrescrever um
executável em uso (o do Agent e o `whisper-server.exe`).

### Montar e compilar

Um comando por bloco, na ordem:

```bash
go -C C:/Desenvolvimento/aurora-whisper run ./cmd/asrstage
```

```bash
go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

```bash
pnpm -C C:/Desenvolvimento/aurora-whisper/web install
```

```bash
pnpm -C C:/Desenvolvimento/aurora-whisper/web build
```

Este roteiro é da **build de bandeja** (`-H=windowsgui`). A build de console
(`aurora-whisper-agent-console.exe`, ver o sprint 4) serve para ler o log no terminal, mas não
para os passos de início automático.

O `asrstage` também cria o arquivo `bin\portatil`: o **modo portátil** (ADR-018), em que o Agent
guarda os dados ao lado do executável. É por ele que o `bin\agent.log` e o `bin\historico.db`
usados abaixo continuam sendo os de sempre, e não os de `%LOCALAPPDATA%\AuroraWhisper`. Depois da
primeira subida, confira:

```powershell
Select-String -Path C:\Desenvolvimento\aurora-whisper\bin\agent.log -Encoding UTF8 -SimpleMatch "dados=" | Select-Object -Last 1
```

A última linha tem de dizer `dados=portatil`. Se disser `dados=portatil-legado`, o arquivo
`bin\portatil` não existe (a montagem não rodou): os dados continuam em `bin\`, e o Agent só
avisou. Se o `bin\agent.log` nem existir, o Agent gravou em `%LOCALAPPDATA%\AuroraWhisper` (um bin
sem banco e sem marcador): feche o Agent, rode o `asrstage` e abra de novo.

### Anote a versão

Algumas verificações dependem de correções da revisão de 29-set-2026 que podem ainda não estar
na sua build — o `Ctrl+Z` do P5, a ordem da Lixeira no P6, o `Alt+F4` do B12, tudo do B13, B14
e L2 (fase 2 do campo protegido), o B1a (dono fixo), e a parte 6 inteira (microfone). Anote o
commit, para separar "falhou" de "build anterior à correção":

```bash
git -C C:/Desenvolvimento/aurora-whisper log -1 --oneline
```

**A parte 6 (microfone) exige a build da ADR-015.** Uma build anterior deixa o microfone ligado o
tempo todo, e o M0 falharia por isso, não por defeito da ADR. A prova de que a build é a certa
está no log, depois da primeira subida: a linha `audio: microfone preparado`. Com o Agent
encerrado (só do PowerShell):

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "audio: microfone preparado"
```

Sem essa linha, a build é anterior à ADR-015: refaça a montagem acima antes de começar a parte 6.
O M6 pede, além disso, a ligação da opção "Microfone sempre pronto" das Configurações à captura
(commit `64ffcc9` ou posterior); sem ela, a caixa grava a preferência e nada muda no microfone. O
passo 3 do M6 (suspender e retomar) e o `perdeu_inicio_ms` do M4 pedem a revisão adversarial de
29-set-2026 (commit `f642079` ou posterior); antes dela, uma leitura que falhava com o sempre
pronto ligado matava o microfone até reiniciar o Agent, sem uma linha no log. O M7 pede a segunda
rodada dessa revisão (commit `ba7e1eb` ou posterior; o vigia de parada, `305df51`): antes dela, no
sob demanda, um keydown com o microfone ainda fora matava o microfone até reiniciar o Agent. O
ditado logo ao retomar do M6 (passo 3) pede a terceira rodada, a que tirou da ADR-015 as
"limitações conhecidas do sempre pronto": antes dela, com o sempre pronto, um ditado no meio da
espera entre duas religações falhava sem tentar ligar, e um microfone que voltava ligado e mudo
não se recuperava sozinho. E a quarta rodada (commit `f0ef2ae` ou posterior): antes dela, com um
microfone que religava e caía logo, uns seis ditados seguidos nessa espera matavam o sempre pronto
com `ERROR`.

### Deixe o log à vista

Numa janela do PowerShell, depois da primeira subida (antes dela o arquivo não existe):

```bash
Get-Content C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Tail 40 -Wait
```

É só do PowerShell. Sem o `-Encoding UTF8`, o PowerShell 5.1 lê o arquivo como ANSI e os acentos
saem trocados. `Ctrl+C` nessa janela fecha só a leitura, não o Agent.

---

## Parte 1 — Bandeja

### B1 — O ícone aparece, e nenhuma janela preta

**Faça:** no Explorador de Arquivos, dê duplo clique em
`C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe`. Espere, sem clicar em nada.

**Deve aparecer:**
- nenhuma janela: nem a preta de console, nem uma do `whisper-server`;
- depois de alguns segundos (o modelo carrega antes), o ícone na bandeja: o **Pico estourado** —
  quatro colchetes de canto verdes e, no meio, uma barra que passa da moldura por cima e por
  baixo. Sem quadrado de fundo: fica direto sobre a barra de tarefas, com a barra do meio clara
  na barra de tarefas escura e preta na clara. Se não estiver à vista, está na setinha `^` ao
  lado dos ícones;
- com o mouse parado em cima dele, a dica **Aurora Whisper**;
- no Gerenciador de Tarefas, exatamente um `aurora-whisper-agent.exe` e um `whisper-server.exe`;
- no `agent.log`: `historico: banco pronto`, `painel: no ar` (com a porta e a pasta
  `...\bin\painel`) e `agent: rodando`.

**Se não aparecer, anote:** se abriu alguma janela preta, e o título dela; quanto tempo o ícone
levou; o texto inteiro de qualquer janela "O Aurora Whisper foi encerrado por um erro"; as linhas
`WARN` e `ERROR` do `agent.log`. Uma linha `painel: não subiu` explica o item Abrir desabilitado
no B2.

### B1a — Dono fixo: a migração da primeira subida

Desde 29-set-2026 o dono dos ditados não vem mais do `"usuario"` do `dicionario.json`: é gerado uma
vez por instalação e gravado no próprio `historico.db`. Na **primeira** subida com esta versão, o
Agent copia o banco ao lado dele e só então passa todos os ditados antigos — de `mauro`, de
`local`, de versões antigas — para o dono novo (`docs/ai-spec/05-data-model.md`, "Dono local").
Os testes automatizados fazem isso com bancos temporários; com o seu banco de verdade, só aqui.

Se esta versão já subiu antes nesta pasta, a migração já aconteceu: procure a linha também no
`agent.log.1` e siga para "Em qualquer subida seguinte", abaixo.

**Faça:** com o Agent do B1 no ar, procure as linhas do dono no log:

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "historico: ditados migrados|historico: dono dos ditados|historico: não migrei|historico: há linhas de outro dono|historico: seguindo SEM registro"
```

Liste as cópias em `bin\`:

```bash
Get-ChildItem C:/Desenvolvimento/aurora-whisper/bin/historico.db*
```

E abra o painel: menu → **Abrir Aurora Whisper** (o P1 detalha o resto da página).

**Deve aparecer:**
- no log, **uma** linha `INFO` `historico: ditados migrados para o dono fixo`, com `dono=` (um
  identificador longo, como `01a0ee4a-f2b2-7a5f-...`), `copia=` apontando para
  `...\bin\historico.db.antes-do-dono-AAAAMMDD-HHMMSS.db` com a data e a hora desta subida, e
  `linhas.local_transcriptions=` com o número de ditados que você tinha (as outras tabelas
  costumam sair com `0`). Nenhum texto ditado na linha;
- logo depois, `historico: dono dos ditados e do painel` com `usuario=` igual ao `dono=` da linha
  anterior e `origem="o dono fixo desta instalação, gravado no historico.db"`;
- em `bin\`, **um** `historico.db.antes-do-dono-...db`, com o nome do `copia=` e tamanho parecido
  com o do `historico.db`;
- no painel, os ditados de **antes** desta versão — em **Ontem** e nos dias anteriores —, não só os
  de hoje;
- nenhuma linha `historico: há linhas de outro dono`.

**Depois do B3** (o primeiro ditado desta sessão), volte ao painel e atualize a página: o ditado
novo aparece em **Hoje**, e os antigos continuam em **Ontem** e antes. É o que prova que o Agent
grava com o mesmo dono com que o painel lê — nenhum teste automatizado cobre essa ligação. Ditado
novo que não aparece, com os antigos aparecendo, é falha: anote.

**Em qualquer subida seguinte** (o P8 e o B9 fazem uma): nenhuma linha `ditados migrados` nova,
nenhuma cópia nova em `bin\`, e o mesmo `usuario=` na linha `dono dos ditados e do painel`. Trocar o
`"usuario"` do `dicionario.json` não muda mais esse valor.

**Se não aparecer, anote:**
- uma linha `WARN` `historico: não migrei para o dono fixo`, com o `err=` inteiro. Nesse caso o
  Agent segue com o dono antigo (a linha do dono diz `regra antiga`), o painel continua mostrando o
  histórico, e a próxima subida tenta de novo. Se o `err=` diz `já há 3 cópia(s) de tentativas
  anteriores`, ela não tenta mais: as falhas anteriores estão nos `WARN` das subidas passadas;
- uma linha `WARN` `historico: seguindo SEM registro` com `lendo o dono fixo gravado no banco`: o
  banco tem um dono que não se lê. O Agent dita, mas não registra nem abre o painel — de propósito,
  para não gravar ditados que o painel nunca mostraria;
- uma linha `WARN` `historico: há linhas de outro dono`, com `linhas.local_transcriptions=` e
  outras contagens: há ditados fora do painel (ver o aviso abaixo);
- painel vazio, ou só com os ditados de hoje;
- mais de uma cópia depois de subidas seguidas, ou `usuario=` diferente entre duas subidas.

**Depois do B1a, não rode neste `bin\` um Agent de build anterior a esta** — por exemplo o de uma
branch que parte de uma `main` mais antiga. Ele não conhece o dono fixo: mostra o painel vazio (o
histórico agora é do dono novo) e grava os ditados daquela sessão como `mauro` (ou `local`). Quando
esta versão voltar, esses ditados ficam **fora do painel**, e nada os migra sozinho: cada subida
registra o `WARN` `historico: há linhas de outro dono` com quantos são, por tabela. Se acontecer,
anote a contagem e leve ao Mauro — migrar essas linhas (com cópia antes) é decisão dele. Para testar
uma build antiga, use outra pasta, com outro `historico.db`.

A cópia é de segurança, e o Agent nunca a apaga. Ela tem **todo** o texto ditado até a migração —
inclusive o que está na Lixeira e o que o expurgo de 30 dias / 6 meses apagaria depois
(`docs/ai-spec/09-security-privacy.md`, "Retenção"). Depois de conferir que o painel mostra o
histórico antigo, apague-a.

### B2 — O menu

**Faça:** clique no ícone (botão esquerdo ou direito). Depois, clique fora do menu.

**Deve aparecer**, nesta ordem:
1. **Abrir Aurora Whisper** — habilitado
2. separador
3. **Ditado ativo** — em cinza, é rótulo
4. **ASR: whisper-local · whisper-small-q4_0** — em cinza
5. separador
6. **Pausar ditado** — desmarcado
7. **Iniciar com o Windows** — marcado ou não, conforme o registro
8. separador
9. **Sair**

Clicar fora fecha o menu sem fazer nada. Com a escala de tela acima de 100%, o ícone e o texto do
menu saem nítidos, não borrados.

**Se não aparecer, anote:** item faltando ou fora de ordem, menu que não fecha ao clicar fora,
ícone ou texto borrado (e a escala da tela, em Configurações > Tela).

### B3 — Ditar com o Agent na bandeja

**Faça:** no Bloco de Notas, segure `Ctrl+Win`, espere o bipe, fale **"o abacaxi ficou na
mesa"**, solte. "Abacaxi" é de propósito: é uma palavra que nenhuma mensagem do Agent usa, e o L1
procura por ela no log. Desde a ADR-015 o microfone só liga quando você segura, e o bipe é o sinal
de que ele já capta: falar em cima da tecla perde a primeira sílaba — custo aceito, não defeito
deste passo (a parte 6 cuida disso).

**Deve aparecer:** a pílula na parte de baixo da tela enquanto você fala, e o texto no Bloco de
Notas ao soltar — como no T18 do sprint 4.

Desde 29-set-2026 a pílula segue o design system: enquanto você fala, uma cápsula pequena e
preta, de contorno fino, com barras cinza nas pausas e verdes nas sílabas fortes; ao soltar, uma
cápsula da mesma largura, um pouco mais alta, com um ponto verde e **Inserido** em IBM Plex Mono.
Ponto vermelho só aparece quando algo deu errado, sempre com a mensagem escrita do lado.

As mensagens têm o tamanho do texto delas (as mais longas quebram em duas linhas de larguras
parecidas), e a janela da pílula muda de tamanho junto, no mesmo lugar: o centro da pílula não
se move da gravação para a mensagem. Isso nenhum teste automatizado vê na tela.

**Se não aparecer, anote:** o que a pílula mostrou, e use a tabela de diagnóstico do fim do
`docs/sprint-4-teste-manual.md` ("Se você não vir o overlay aparecer"). Anote também barras que
não ficam verdes quando você fala, ponto vermelho num ditado que deu certo, se a pílula ficou
pequena demais para ver a onda se mexer, e, na troca da onda para **Inserido**: se a pílula
**pisca**, pula de lugar, aparece por um instante com o tamanho errado, ou deixa um pedaço da
anterior na tela; e letra borrada, cortada ou encostada na borda.

Ao julgar se a letra está borrada, anote a **escala da tela** (Configurações > Tela > Escala). A
janela da pílula não declara DPI, ao contrário da bandeja: acima de 100%, o Windows estica a
imagem dela, e o texto suavizado — e a onda — sai mais macio do que nas folhas de PNG, que são
em 100%. Borrado a 100% é defeito do desenho; borrado só a 125% ou 150% é essa ampliação.

### B4 — Pausar e retomar

**Faça:**
1. menu → **Pausar ditado**;
2. passe o mouse sobre o ícone e abra o menu de novo, só para olhar; feche com um clique fora;
3. no Bloco de Notas, segure `Ctrl+Win` por 2 segundos, falando; solte e digite `abc`;
4. menu → **Pausar ditado** de novo (desmarca) e dite uma frase.

**Deve aparecer:**
- pausado, o ícone muda de forma: os colchetes ficam cinza e, no lugar da barra que passa da
  moldura, aparece o sinal de pausa — duas barras dentro dela. A dica vira **Aurora Whisper —
  ditado pausado**, e o menu mostra **Ditado pausado** e **Pausar ditado** marcado;
- segurar `Ctrl+Win` pausado não mostra pílula, não toca bipe, não insere nada; o `abc` sai
  normal — nenhum modificador preso;
- no `agent.log`, `tray: ditado pausado` com `pausado=true`, e depois `pausado=false`;
- retomado, o ícone volta aos colchetes verdes com a barra que fura a moldura, e o ditado
  funciona.

A pausa vale a partir do próximo toque: um ditado que já estava gravando quando você clicou
termina normalmente. É de propósito — valendo na hora, a soltura da tecla passaria direto, e a
gravação seguiria até o limite de 120 s com o microfone aberto.

Pausado, `Ctrl+Win` volta a ser tecla comum do Windows. Se o menu Iniciar abrir ao soltar, é o
T26 do sprint 4, não defeito da pausa — anote mesmo assim, é dado que falta ao `ADR-013`.

**Se não aparecer, anote:** ditado que acontece com a pausa ligada, tecla que fica estranha
depois de soltar, ícone que não muda.

### B5 — Segunda instância, com aviso

**Faça:** com o Agent rodando, dê duplo clique no `aurora-whisper-agent.exe` de novo. Leia a
janela e clique **OK**. Depois dite uma frase no Bloco de Notas.

**Deve aparecer:**
- uma janela **"O Aurora Whisper já está rodando."**, que diz onde fica o ícone;
- depois do OK, continua um só `aurora-whisper-agent.exe` e um só `whisper-server.exe`, e um só
  ícone;
- o ditado sai **uma vez** — dois Agents duplicariam o texto;
- nenhuma linha nova no `agent.log` vinda do segundo: ele sai antes de abrir o log.

**Se não aparecer, anote:** segundo ícone, texto duplicado, processo a mais, ou saída sem janela
nenhuma.

---

## Parte 2 — Painel

Tenha pelo menos três ditados no histórico antes (o B3 e o B4 já criam alguns).

### P1 — Abrir o painel

**Faça:** menu → **Abrir Aurora Whisper**.

**Deve aparecer:**
- o navegador padrão abre uma aba em `http://127.0.0.1:<porta>/`. Depois de carregar, o endereço
  **não tem `?t=`**: a página apaga o bilhete da URL antes de qualquer outra coisa;
- no topo, **Aurora Whisper**, os botões **Histórico** e **Lixeira**, e o campo **Buscar no
  histórico**;
- a lista agrupada por dia (**Hoje**, **Ontem**, dia por extenso), cada linha com a hora e o
  texto final;
- no `agent.log`, `painel: aberto no navegador` — sem a URL e sem o bilhete.

**Se não aparecer, anote:**
- página **"O painel não foi construído"** → faltou o `pnpm ... build`. Rode os dois comandos do
  pnpm e clique **Abrir** de novo; não precisa reiniciar o Agent;
- **"Abra o painel pela bandeja"** logo ao abrir → a troca do bilhete falhou. Anote quanto tempo
  o navegador levou para abrir (o bilhete vale um minuto);
- página em branco → abra o console do navegador (`F12`) e anote os erros;
- endereço ainda com `?t=` depois de carregar.

### P2 — Busca

**Faça:**
1. no campo de busca, digite uma palavra que você ditou, com maiúsculas diferentes das do ditado
   — por exemplo `ABACAXI`, do B3;
2. troque por uma palavra que não existe, como `xyzw`;
3. aperte `Esc`;
4. clique numa área vazia da página e aperte `/`.

**Deve aparecer:**
- a lista filtra logo depois que você para de digitar (um quarto de segundo), sem diferença de
  maiúsculas — inclusive em letra acentuada: `ROSÂNGELA` acha "Rosângela" —, e procurando também
  no texto bruto do ASR. O acento em si conta: `rosangela`, sem ele, não acha "Rosângela";
- com `xyzw`: **Nada encontrado para “xyzw”.**;
- `Esc` limpa a busca e a lista inteira volta;
- `/` põe o cursor no campo de busca.

**Se não aparecer, anote:** palavra que existe e não foi achada (com a grafia exata), texto que
aparece por um instante e some durante a busca, demora.

### P3 — Editar, com o bruto do ASR marcado

**Faça:** clique numa linha (ou chegue nela com `Tab` e aperte `Enter`). No **Texto final**,
troque uma palavra e apague outra.

**Deve aparecer:**
- o editor abre com o cursor no fim do **Texto final**;
- embaixo, **Bruto do ASR**: o texto que o ASR produziu, marcado palavra a palavra **enquanto você
  digita** — o que saiu, riscado; o que entrou, destacado. Sem mudança nenhuma, o rótulo diz
  **— igual ao texto final**;
- uma linha de dados: data e hora, o ASR, e o que houver de duração e tempo de transcrição;
- **Salvar** desabilitado até você mudar alguma coisa;
- em ditado com a etiqueta **falhou**, uma nota dizendo que o texto pode não ter entrado no campo.

**Se não aparecer, anote:** marcação errada (qual palavra, como apareceu), marcação que não
acompanha a digitação, **Salvar** habilitado sem mudança.

### P4 — Salvar, e cancelar

**Faça:**
1. com a edição do P3, aperte `Ctrl+Enter` (ou **Salvar**);
2. abra outra linha, mude o texto e aperte `Esc`;
3. menu → **Abrir Aurora Whisper** de novo, e procure na aba nova o ditado do passo 1.

**Deve aparecer:**
- no passo 1, a pílula **Texto salvo.** embaixo da tela; o editor fecha, e a linha mostra o texto
  novo com a etiqueta **editado**;
- no passo 2, o editor fecha sem salvar, e a linha continua com o texto antigo;
- no passo 3, o texto salvo está lá: foi gravado no `historico.db`, não só na tela.

**Se não aparecer, anote:** texto que não ficou salvo, etiqueta faltando, pílula **Não deu
certo:** e o texto dela.

### P5 — Lixeira, com Desfazer (inclusive `Ctrl+Z`)

**Faça:**
1. abra um ditado → **Mover para a lixeira**. Na pílula, clique **Desfazer**;
2. mova outro ditado para a lixeira e, **sem clicar em nada**, aperte `Ctrl+Z`;
3. mova um terceiro, clique no campo de busca, digite `abc` e aperte `Ctrl+Z`;
4. mova um quarto, espere a pílula sumir, e aperte `Ctrl+Z` fora de qualquer campo.

**Deve aparecer:**
- ao mover: a linha some, o foco vai para a linha que ocupou o lugar dela, e a pílula **Movido
  para a lixeira.** aparece com **Desfazer** por uns 8 segundos. Com o mouse ou o foco em cima
  dela, ela espera;
- **Desfazer**: pílula **Ditado de volta ao histórico.**, e a linha volta para o lugar dela;
- `Ctrl+Z` com a pílula na tela, fora de campo de texto, faz o mesmo que o **Desfazer**;
- dentro do campo de busca, `Ctrl+Z` desfaz só a digitação — o terceiro ditado continua na
  lixeira;
- com a pílula já sumida, `Ctrl+Z` não faz nada. O quarto ditado fica na lixeira (ele volta no
  P6).

**Se não aparecer, anote:** `Ctrl+Z` que não faz nada com a pílula na tela (e o commit anotado no
início — pode ser build anterior à correção); ditado que não volta; ditado que volta com a
etiqueta **editado** sem ter sido editado; foco que cai no topo da página.

### P6 — Aba Lixeira, e Restaurar

**Faça:** clique **Lixeira**. Olhe a ordem e o texto das linhas. Clique **Restaurar** no primeiro
item, depois volte para **Histórico**.

**Deve aparecer:**
- cada linha com a hora, o texto, **na lixeira desde** e a data, e o botão **Restaurar**;
- o que foi para a lixeira por último vem primeiro — é a ordem do contrato
  (`06-api-contract.md`, "API local"). Uma build anterior à correção da revisão de 29-set-2026
  ordena pela data do ditado;
- **nenhum prazo** em lugar nenhum: nem "30 dias", nem "some em N dias". O expurgo automático
  ainda está sendo ligado, e o painel não promete o que o Agent não faz (`PRODUCT.md`);
- **Restaurar**: pílula **Restaurado para o histórico.**, a linha sai da Lixeira e volta ao
  Histórico, no dia em que foi ditada;
- sem nada na lixeira: **A lixeira está vazia.**

**Se não aparecer, anote:** a ordem que você viu (com as datas), item que não volta, qualquer
menção a prazo.

### P7 — Recarregar a página (`F5`)

**Faça:** na aba do painel, aperte `F5`. Depois, menu → **Abrir Aurora Whisper**.

**Deve aparecer:**
- depois do `F5`, a tela **"Abra o painel pela bandeja"**, explicando que o acesso vale só para a
  aba que o Aurora Whisper abriu e termina ao recarregar. Nenhum ditado à mostra;
- pela bandeja, uma aba nova que funciona.

Não é defeito: o token da API local existe só na memória da aba, e recarregar a apaga. Um painel
que continuasse mostrando o histórico depois do `F5` teria o token guardado em algum lugar
(`09-security-privacy.md`, "Local API").

**Se não aparecer, anote:** histórico visível depois do `F5` — é o mais importante deste passo —
ou página em branco.

### P8 — Aba aberta com o Agent fechado, e depois reiniciado

**Faça:**
1. com uma aba do painel aberta, menu → **Sair**;
2. na aba, digite algo na busca (abrir uma linha não fala com o Agent, então não serve de teste);
3. abra o Agent de novo (duplo clique no executável) e espere o ícone;
4. na aba **antiga**, clique em **Tentar de novo** na faixa do topo;
5. menu → **Abrir Aurora Whisper**.

**Deve aparecer:**
- no passo 2, uma faixa no topo dizendo **"O Aurora Whisper não está respondendo"**, com a lista e qualquer texto em edição ainda na tela;
- no passo 4, a aba antiga **continua sem funcionar**, e é o esperado: a cada subida o Agent
  escuta numa porta nova, com um token novo, e a aba velha aponta para a porta antiga. Um `F5`
  nela dá a página de erro do próprio navegador (não conseguiu conectar);
- no passo 5, uma aba nova que funciona.

**Se não aparecer, anote:** aba antiga que volta a funcionar sozinha (e a porta dela e a da nova,
no endereço), ou tela diferente das duas acima.

### P9 (opcional) — Copiar

**Atenção:** este passo **substitui o que você tem na área de transferência**.

**Faça:** abra um ditado, clique **Copiar** e cole no Bloco de Notas.

**Deve aparecer:** a pílula **Copiado.**, e colado o texto que estava no **Texto final** — o
rascunho, se você tinha mudado algo sem salvar.

**Se não aparecer, anote:** o texto colado, e a pílula que apareceu.

### P10 — A chave de API não vai para o cofre do navegador

O campo da chave é de senha, e Edge, Brave e Chrome ignoram `autocomplete="off"` nesse tipo de
campo: depois que a página manda a chave e o campo some, o navegador pode oferecer **Salvar senha**
para `127.0.0.1`. Aceitar poria a chave no cofre do navegador, talvez sincronizado — e a tela diz
que ela fica no Gerenciador de Credenciais do Windows. Isto não dá para conferir em teste
automático; precisa de um navegador com o gerenciador de senhas ligado, e de um Agent que já tenha
as rotas das Configurações (`/api/settings` nova e `/api/ia/chave`) — antes delas, a tela só diz
que não conseguiu carregar.

**Atenção:** use uma chave de teste, que você possa revogar no site do provedor depois.

**Faça:**
1. no painel, **Configurações** → **Provedor**: OpenAI (ou Anthropic);
2. cole a chave de teste e clique **Salvar chave**;
3. troque o **Provedor** para o outro, e depois vá ao **Histórico**;
4. feche a aba.

**Deve aparecer:** em nenhum dos passos o navegador oferece salvar senha (ícone de chave na barra de
endereço, ou um balão "Salvar senha?").

**Se aparecer:** **recuse**, anote o navegador, a versão e o passo em que apareceu — é defeito a
corrigir no campo da chave.

---

## Parte 3 — Início automático

### B6 — Ligar pelo menu

**Faça:** menu → **Iniciar com o Windows** (se já estiver marcado, pule para o B7). Abra o menu
de novo, só para olhar. Depois abra o Gerenciador de Tarefas → **Aplicativos de inicialização**.

**Deve aparecer:**
- o item **Iniciar com o Windows** marcado;
- no `agent.log`, a linha `autostart: ligado`, **sem caminho** (o caminho leva o nome de usuário do
  Windows, e o log é o que se cola num issue): o destino do `Run` se confere no `reg query` abaixo;
- no Gerenciador de Tarefas, uma linha do Aurora Whisper (pode aparecer como
  `aurora-whisper-agent.exe` ou `AuroraWhisper`) com status **Habilitado**.

Para conferir o registro sem mexer nele (só leitura), no PowerShell:

```bash
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v AuroraWhisper
```

O valor deve ser o caminho do executável **entre aspas**: sem elas, um caminho com espaço seria
partido pelo Windows.

**Se não aparecer, anote:** item que não fica marcado, linha `WARN` com `tray:` ou `autostart:`
no `agent.log`, valor sem aspas ou apontando para outro executável.

### B7 — Desligar no Gerenciador de Tarefas, e ver o menu desmarcado

**Faça:** no Gerenciador de Tarefas → **Aplicativos de inicialização**, selecione a linha do
Aurora Whisper → **Desabilitar**. Abra o menu da bandeja.

**Deve aparecer:** **Iniciar com o Windows** **desmarcado**. O Windows não apaga a entrada do
registro ao desabilitar: grava uma marca própria (`StartupApproved`), e o Agent lê essa marca.

**Se não aparecer, anote:** item ainda marcado com o Gerenciador mostrando **Desabilitado** — é o
defeito que o `574ed6f` corrigiu: o menu afirmaria um início que não vai acontecer.

### B7b — Salvar as Configurações não religa o que o Gerenciador desligou

Precisa de um Agent com as rotas das Configurações (como o P10). O início automático mora no
registro, fora da versão das configurações: o painel só o manda ao Agent quando a pessoa mexe na
caixa.

**Faça:** antes de desabilitar no B7, abra o painel em **Configurações** (com **Iniciar com o
Windows** marcado) e deixe a aba aberta. Faça o B7. Volte à aba do painel, mude só o **Bip ao
começar a gravar** e clique **Salvar configurações**. Feche e abra de novo o Gerenciador de
Tarefas → **Aplicativos de inicialização**.

**Deve aparecer:**
- ao voltar à aba, a caixa **Iniciar com o Windows** já desmarcada — sem nada por salvar, a tela lê
  de novo ao voltar;
- depois do Salvar, a linha do Aurora Whisper continua **Desabilitado**.

**Se não aparecer, anote:** caixa ainda marcada ao voltar à aba; linha de volta a **Habilitado**
depois do Salvar — o painel teria desfeito a escolha feita no Gerenciador.

### B8 — Religar pelo menu

**Faça:** menu → **Iniciar com o Windows**. Feche e abra de novo o Gerenciador de Tarefas →
**Aplicativos de inicialização**.

**Deve aparecer:** o item marcado no menu, e a linha do Aurora Whisper de volta a
**Habilitado** — ao ligar, o Agent apaga a marca de desabilitado que o Windows tinha gravado.

**Se não aparecer, anote:** menu marcado com o Gerenciador ainda em **Desabilitado**. Nesse
estado o Windows não inicia o Agent no logon.

### B9 — Logon

**Faça:** com **Iniciar com o Windows** marcado (B8), saia da conta do Windows (Iniciar → seu
nome → **Sair**) e entre de novo — ou reinicie. Não abra nada; espere.

**Deve aparecer:**
- **nenhuma janela preta**, em momento nenhum;
- o ícone na bandeja depois de alguns segundos. No logon o Explorer costuma estar ocupado, e o
  Agent tenta pôr o ícone de novo a cada segundo, por até 10 segundos;
- no Gerenciador de Tarefas, exatamente um `aurora-whisper-agent.exe` e um `whisper-server.exe`;
- o ditado funcionando no Bloco de Notas;
- no `agent.log`, um `agent: rodando` novo, com a hora do logon.

**Se não aparecer, anote:** janela preta (e o título dela); quanto tempo o ícone levou; ícone que
nunca apareceu com o processo rodando no Gerenciador — Agent invisível, com a tecla capturada:
encerre pelo Gerenciador e anote; janela "O Aurora Whisper foi encerrado por um erro" e o texto
dela; as linhas do `agent.log` em torno da hora do logon.

### B10 — Desligar pelo menu (se não quiser manter)

**Faça:** menu → **Iniciar com o Windows** (desmarca). Se quiser manter o início automático, pule
este passo.

**Deve aparecer:** o item desmarcado; `autostart: desligado` no `agent.log`; a linha do Aurora
Whisper some dos **Aplicativos de inicialização** (depois de reabrir o Gerenciador); e o
`reg query` do B6 responde que não encontrou o valor.

**Se não aparecer, anote:** o que o `reg query` mostrou.

---

## Parte 4 — Encerrar

### B11 — Sair

**Faça:** menu → **Sair**. Depois, no Bloco de Notas, segure `Ctrl+Win` por 2 segundos e digite
`abc`.

**Deve aparecer:**
- o ícone some da bandeja;
- os dois processos saem do Gerenciador de Tarefas;
- no `agent.log`, `tray: encerrando a pedido de quem usa`, sem `WARN` nem `ERROR` depois;
- `Ctrl+Win` não grava nada, e o `abc` sai normal — nenhum modificador preso.

**Se não aparecer, anote:** processo que ficou, e as linhas `WARN`/`ERROR` depois de
`encerrando`.

### B12 (opcional) — Casos de borda do encerramento e do ícone

Cinco verificações das correções e do ícone novo de 28 e 29-set-2026. Para cada uma, suba o
Agent de novo antes.

1. **Fechar de fora, sem `/F`.** No PowerShell:

   ```bash
   taskkill /IM aurora-whisper-agent.exe
   ```

   **Deve:** o mesmo que o **Sair** — ícone some, os dois processos saem —, mas o `agent.log`
   registra `overlay: pedido de fechar vindo de fora (taskkill sem /F); encerrando`, e não a
   linha do **Sair** (`tray: encerrando a pedido de quem usa`): o `WM_CLOSE` do `taskkill` cai
   na janela da pílula, não na bandeja. **Anote** se o ícone sumir com o processo ainda rodando
   (Agent ditando invisível), ou se aparecer `overlay: Close falhou`.

2. **`Alt+F4` depois do menu.** Deixe aberto só o Bloco de Notas, vazio. Abra o menu da bandeja,
   feche com `Esc` e, sem clicar em mais nada, aperte `Alt+F4`. **Cuidado:** `Alt+F4` fecha a
   janela que estiver em primeiro plano — por isso só o Bloco de Notas vazio.

   **Deve:** o Agent **continua rodando** (ícone lá, ditado funcionando). **Anote** se ele
   encerrar: é o defeito da revisão de 29-set-2026, e a build pode ser anterior à correção.

3. **O Explorer reinicia.** Gerenciador de Tarefas → **Processos** → **Windows Explorer** →
   **Reiniciar**.

   **Deve:** a barra de tarefas pisca e volta, e o ícone do Aurora Whisper **volta junto** em
   alguns segundos. **Anote** se ele não voltar, com o Agent ainda no Gerenciador.

4. **A reinspeção depois de uma dúvida.** Os testes automatizados cobrem quando reinspecionar; o
   timer do Windows que dispara a reinspeção, não. Tenha o Bloco de Notas aberto e o log à vista.
   O passo usa o Gerenciador de Tarefas porque, pelo log de 29-set-2026, o Agent não consegue
   inspecioná-lo (ele roda elevado) e cada foco nele dá um `WARN` com `hresult=0x80004005` — isso
   foi inferido do log, não está documentado. Se o Gerenciador não der `WARN` nesta máquina,
   anote e pule este passo.

   a. **Uma escada, e só uma.** Abra o Gerenciador (`Ctrl+Shift+Esc`), clique numa linha e
      **não mexa em nada** por uns 3 segundos.

      **Deve:** um `WARN uia: foco inspecionado` com `outcome=failed` e `reinspeciona_em=100ms`,
      seguido de exatamente três `WARN`: `reinspecao=1` com `reinspeciona_em=300ms`,
      `reinspecao=2` com `reinspeciona_em=1s` e `reinspecao=3` sem `reinspeciona_em`. Depois,
      **silêncio** enquanto você não mexer. Com a fase 2, cada um desses `WARN` traz também
      `janela_elevada=sim` e `executavel=Taskmgr.exe` (ver B14).

      **Anote:** nenhuma linha `reinspecao=` depois do primeiro `WARN` (o timer não disparou);
      mais de três, ou linhas continuando sem você mexer (seria um laço).

   b. **Timers substituídos.** Ainda no Gerenciador, clique em linhas diferentes, rápido, umas
      cinco vezes seguidas (mais de uma por segundo), e depois pare.

      **Deve:** um `WARN` com `reinspeciona_em=100ms` a cada clique, e **uma** escada só — de
      no máximo três `reinspecao=` — depois do último. Cada troca de foco cancela a reinspeção
      da anterior.

      **Anote:** `reinspecao=` repetido ou fora de ordem (`1`, `1`, ou `2` antes de `1`), ou mais
      de três depois que você parou.

   c. **A volta a livre.** Sem fechar o Gerenciador, clique no Bloco de Notas e dite uma palavra.

      **Deve:** um `INFO uia: foco inspecionado` com `outcome=ok`, `protegido=false` e
      `protegido_por=` (o tempo desde o primeiro `WARN` do Gerenciador), e o ditado funcionando.

      **Anote:** ditado recusado com "campo protegido" no Bloco de Notas.

   d. **Encerrar com a escada no meio** (pode levar umas tentativas). Deixe o Gerenciador aberto.
      No PowerShell, rode:

      ```bash
      Start-Sleep -Seconds 2; taskkill /IM aurora-whisper-agent.exe
      ```

      e, cerca de um segundo depois do `Enter`, clique numa linha do Gerenciador e não mexa mais.

      **Deve:** o Agent sai como no item 1 (ícone some, os dois processos saem), sem `ERROR`. O
      caso que interessa é quando o último `WARN` de foco antes de `overlay: pedido de fechar
      vindo de fora` ainda tem `reinspeciona_em` — o encerramento pegou uma reinspeção agendada.
      Se não tiver, repita. Um ou dois `WARN uia: foco inspecionado` com `reinspecao=` **depois**
      do pedido de fechar são normais: o rastreador só para depois da bandeja, do painel e do
      ASR, e a reinspeção agendada pode sair antes disso.

      **Anote:** processo que não sai (encerre pelo Gerenciador → **Finalizar tarefa**),
      `uia: Tracker.Stop falhou`, ou qualquer outro `WARN`/`ERROR` depois do pedido de fechar.


5. **A barra de tarefas troca de cor.** Com o Agent rodando: Configurações → Personalização →
   Cores → **Escolha seu modo** (no Windows 10, **Escolha sua cor**) → **Personalizado**. Em
   **Escolha o modo padrão do Windows**, troque de **Escuro** para **Claro** e depois de volta.
   É esse, e não o modo dos aplicativos, que muda a barra de tarefas. Faça uma vez com o ditado
   ativo e uma com ele pausado.

   **Deve:** a cada troca, em um ou dois segundos e sem reabrir nada, a barra do meio do ícone
   (ou as duas da pausa) acompanha — clara na barra de tarefas escura, preta na clara — e os
   colchetes seguem como estavam. **Anote** se o ícone ficar com a barra sumida na barra de
   tarefas nova, e se o `agent.log` tiver `a barra de tarefas mudou de tema e o ícone não foi
   redesenhado`.

---

## Parte 4b — Campo protegido com o `CUIAutomation8`, e janela de administrador

A fase 2 da correção do falso "campo protegido" (29-set-2026) trocou o objeto de UI Automation
que o Agent usa para inspecionar o foco: o `CUIAutomation8`, com teto de 3 s por chamada. A
Microsoft avisa que alguns aplicativos respondem diferente a ele — e nenhum teste automatizado pode
conferir isso, porque perguntar à UI Automation de verdade é exatamente o que o `CLAUDE.md` proíbe
um agente de fazer nesta máquina. Por isso a matriz abaixo é refeita à mão.

Antes de começar: suba o Agent de novo e confira, no `agent.log`, a linha de subida
`uia: automação` (ver L2). Se ela disser `objeto=CUIAutomation` (sem o 8), anote — o resto desta
parte testa o objeto antigo.

Em todos os passos, ditar é segurar `Ctrl+Win` por uns 2 segundos falando uma palavra curta, e
soltar. **Nunca dite uma senha de verdade**: num campo de senha, o esperado é o ditado **não**
acontecer — e se acontecer, o que você falou vai parar no campo. Use uma palavra qualquer, como
"teste".

### B13 — Matriz de detecção de senha

**Faça**, em cada linha da tabela: clique no **campo comum** e dite; depois clique no **campo de
senha** e dite. Entre um aplicativo e outro, olhe o `agent.log`.

| Aplicativo | Campo comum | Campo de senha |
|---|---|---|
| Bloco de Notas | a área de texto | não há — é o controle da linha "Senha clássica do Windows" |
| Brave (o navegador do Mauro) | a barra de endereço, e o campo de e-mail/usuário da página de login de um site que você usa | o campo de senha da mesma página — **sem enviar nada** |
| WhatsApp Web, no Brave | a caixa de mensagem de uma conversa — **não aperte `Enter`**, apague o texto depois | se a sua conta tiver: Configurações → Privacidade → **Bloqueio do app**, o campo de senha dele. Se não tiver, marque "n/a" |
| VS Code | o editor de um arquivo qualquer, e o terminal integrado | não há um fixo. Se aparecer um pedido de senha (Git por HTTPS, Remote-SSH), teste nele; senão, "n/a" |
| Senha clássica do Windows | o campo **Nome de usuário** | o campo **Senha** |

A **senha clássica do Windows** é o diálogo antigo de credenciais: `Win+R`, digite
`rundll32.exe keymgr.dll,KRShowKeyMgr` e `Enter`; na janela **Nomes de Usuário e Senhas
Armazenados**, clique **Adicionar...**. É um diálogo Win32 com um campo de senha de estilo
`ES_PASSWORD` — o que exercita também o segundo sinal de senha, não só o da UI Automation. Teste e
clique **Cancelar**; não salve nada. (A escolha é deste roteiro, não da Microsoft: o `keymgr.dll`
vem no Windows 11, mas não foi aberto nesta máquina ao escrever isto. Se não abrir, anote e use,
num notebook com Wi-Fi, `ncpa.cpl` → a conexão Wi-Fi → **Status** → **Propriedades da Rede Sem
Fio** → aba **Segurança** → **Chave de segurança da rede**.)

**Deve aparecer**, em cada aplicativo:
- **campo comum:** o ditado entra, como no B3; no log, no máximo linhas `INFO`/`DEBUG` do
  rastreador;
- **campo de senha:** a pílula **"Ditado desativado em campo protegido."**, nada entra no campo,
  e no log um `INFO uia: foco inspecionado` com `protegido=true` e `is_password=true` ou
  `style_senha=true`, seguido de `winruntime: overlay ... fase=blocked`.

**Anote**, por aplicativo: ditado que **entrou** num campo de senha (é o defeito grave — anote o
aplicativo e as linhas `uia:` antes dele); ditado recusado num campo comum (com as linhas `WARN`
do rastreador, que agora dizem `executavel=`); um `WARN` com `hresult=0x80131505` e `duracao`
perto de 3 s (o teto agiu: o aplicativo não respondeu a tempo); e qualquer `duracao` bem acima de
3 s num `WARN` (o teto **não** agiu).

### B14 — Janela de administrador

O Gerenciador de Tarefas roda como administrador, e o Agent, sem ser administrador, não consegue
nem inspecioná-lo nem escrever nele (o Windows bloqueia). Até a fase 2, a pílula dizia "campo
protegido" ali.

> **Resultado de 29-set-2026 (Mauro).** Com o Gerenciador em primeiro plano, segurar `Ctrl+Win`
> não fez **nada** — nem pílula, nem bipe. O gancho de teclado de baixo nível de um programa
> comum não recebe as teclas quando a janela da frente é de administrador: é a mesma barreira do
> Windows (UIPI) que impede o `SendInput` de escrever nela. É seguro — o ditado nem começa, então
> nada é digitado —, mas quer dizer que a mensagem "O Windows não deixa ditar em janela de
> administrador." praticamente nunca aparece. O código dela ficou, e o esperado deste passo
> passou a ser "nada acontece" (ADR-011, nota do B14).

**Faça:**
1. abra o Gerenciador de Tarefas (`Ctrl+Shift+Esc`) e clique numa linha da lista;
2. com o Gerenciador em primeiro plano, segure `Ctrl+Win` por uns 2 segundos e solte;
3. clique no Bloco de Notas e dite uma palavra.

**Deve aparecer:**
- no passo 1, um `WARN uia: foco inspecionado` com `outcome=failed`, `janela_elevada=sim` e
  `executavel=Taskmgr.exe` (mais a escada de reinspeção do B12.4);
- no passo 2, **nada**: nem pílula, nem bipe, nem linha `winruntime: overlay` no log. Nada é
  digitado no Gerenciador;
- no passo 3, o ditado entra normalmente no Bloco de Notas — o Agent não ficou preso nem com
  modificador pressionado por causa do passo 2.

**Anote:**
- se no passo 2 aparecer a pílula **"O Windows não deixa ditar em janela de administrador."**:
  não é defeito — é o caso raro em que o atalho chegou ao Agent —, mas é o dado que falta sobre
  quando isso acontece. Anote a linha `winruntime: overlay ... fase=blocked motivo=janela_elevada`
  e o que estava na frente;
- se a pílula disser **"campo protegido"** no passo 2: procure no log o `janela_elevada=` do
  `WARN` do passo 1. `desconhecida` quer dizer que o Agent não conseguiu ler o nível do
  Gerenciador (por exemplo, se o Gerenciador foi aberto com a senha de outra conta);
- qualquer coisa digitada no Gerenciador, ou modificador preso depois (o passo 3 falha ou o
  teclado fica estranho).

---

## Parte 5 — O `agent.log`

### L1 — O que o log tem, e o que não pode ter

**Faça:** com o Agent encerrado, rode no PowerShell (só do PowerShell):

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "WARN|ERROR"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "?t="
```

E a palavra ditada no B3 (se o ASR a escreveu diferente, use a grafia que apareceu no Bloco de
Notas):

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "abacaxi"
```

E o expurgo da lixeira:

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "lixeira: expurgo"
```

**Deve aparecer:**
- o primeiro lista os `WARN`/`ERROR` da sessão — anote todos, mesmo os que parecerem inofensivos.
  Um `WARN` de `uia: foco inspecionado` pode vir seguido de até três outros com `reinspecao=1`
  a `3`: é o Agent perguntando de novo quem tem o foco (100 ms, 300 ms e 1 s depois) — sem troca
  de janela no meio, em geral o mesmo campo —, não um defeito novo. Quando uma delas responde, a
  volta sai em `INFO`, com `protegido_por`. Um `WARN` com `duracao` acima de 2 s não tem
  reinspeção: seria voltar a um aplicativo que acabou de travar — salvo a leitura completa, que
  sai com `reinspeciona_em=10ms` (ver L2). Um `INFO` com
  `no_proprio_agent=true` é uma reinspeção que caiu numa janela do próprio Agent (o menu da
  bandeja) e parou ali, sem liberar;
- o segundo **não mostra nada**: a URL com o bilhete nunca vai para o log;
- o terceiro **não mostra nada**: texto ditado nunca vai para o log (`09-security-privacy.md`).
  Por isso a palavra tem de ser uma que o Agent não usa nas mensagens dele: "bandeja", por
  exemplo, aparece na linha `agent: rodando` e daria um falso alarme.
- o quarto mostra uma linha por subida, com `apagados=0` e `proximo_em=1h0m0s`: é a prova de
  que o expurgo da lixeira está ligado. Ele apaga o que está na lixeira há 30 dias, então por
  enquanto `apagados` é zero — acima de zero, antes do fim de outubro, é defeito.

**Se não aparecer, anote:** as linhas encontradas no segundo e no terceiro comando — qualquer
uma é defeito de privacidade.

O log fica em `bin\agent.log` e é girado na subida quando passa de 5 MB (o anterior vira
`agent.log.1`). O servidor de ASR escreve à parte, em `bin\whisper-server.log`.

### L2 — Os avisos do rastreador de foco (fase 2)

**Faça**, com o Agent encerrado, depois do B13 e do B14 (só do PowerShell):

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "uia: automação"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "executavel="
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "vigia=true|leitura_atrasada=true|janela_elevada=|motivo=janela_elevada"
```

**Deve aparecer:**
- o primeiro mostra uma linha por subida: `INFO uia: automação objeto=CUIAutomation8
  teto_conexao=3s teto_transacao=3s`. Um `WARN` com `objeto=CUIAutomation` (e
  `hresult_cuiautomation8=`) quer dizer que o objeto novo não foi criado e **não há teto**; um
  `WARN` com `hresult_teto_conexao=` ou `hresult_teto_transacao=` quer dizer que um dos tetos não
  se aplicou;
- no segundo, **todas** as linhas são `WARN uia: foco inspecionado`, e o valor de `executavel=`
  é só um nome de arquivo, como `brave.exe` ou `Taskmgr.exe` — sem `\`, sem `C:`, sem pasta. É a
  exceção autorizada pelo Mauro em 29-set-2026 (`09-security-privacy.md`); nenhuma outra coisa da
  janela pode estar ali: título, texto, classe;
- no terceiro, o que a fase 2 acrescentou, quando aconteceu:
  - `vigia=true`: uma inspeção passou de 500 ms, e o ditado ficou recusado enquanto ela rodava.
    Se ela terminou livre, a mesma linha sai em `INFO` com `protegido_por=`;
  - `leitura_atrasada=true` (`INFO`): uma leitura completa passou de 500 ms mas ficou em até 2 s
    e valeu — antes da fase 2, seria um `WARN` de `outcome=timeout`;
  - `reinspeciona_em=10ms`: uma leitura completa passou de 2 s; ficou protegida e o Agent
    perguntou de novo na hora;
  - `janela_elevada=sim` nos `WARN` do Gerenciador de Tarefas. A linha `winruntime: overlay` com
    `motivo=janela_elevada` só aparece se o atalho chegar ao Agent com o Gerenciador na frente —
    no B14 de 29-set-2026 não chegou (ver B14).

**Se não aparecer, anote:** qualquer `executavel=` numa linha que não seja `WARN`, ou com barra,
unidade ou pasta no valor — é defeito de privacidade; um `WARN` com `hresult=0x80131505` e
`duracao` perto de 3 s, com o `executavel=` dele (o aplicativo que não respondeu); qualquer
`duracao` de dezenas de segundos (o teto não agiu); e o `objeto=` da linha de subida.

---

## Parte 6 — Microfone só enquanto dita (ADR-015)

Desde a ADR-015 o Agent prepara o microfone na subida, mas só o **liga** quando você segura
`Ctrl+Win`, e o **estaciona** quando o ditado termina. O bipe e a pílula "Gravando" só saem quando
o microfone já está captando. Nada disso tem como ser conferido por teste automatizado: o ícone
de microfone do Windows e a latência real do microfone só existem aqui.

**Antes:** feche o Wispr Flow e qualquer aplicativo de chamada ou aba de reunião — qualquer um
deles acende o mesmo ícone. Deixe aberta, numa segunda janela, **Configurações > Privacidade e
segurança > Microfone**: é a única tela que diz *quem* está usando o microfone. O controle já foi
feito: com o Agent de antes da ADR-015, o ícone ficava aceso o tempo todo (foi o que motivou a
mudança).

### M0 — Em repouso, o ícone apaga

**Faça:** suba o Agent (B1) e fique 10 s sem tocar em nada. Repita 10 s depois de um ditado.

**Deve aparecer:**
- nenhum ícone de microfone na área de notificação, e o Aurora Whisper **fora** de "Em uso no
  momento" na tela de Privacidade;
- no `agent.log`: `audio: microfone preparado modo=sob_demanda politica=parar`.

Opcional, só leitura (a leitura de `LastUsedTimeStop` vem de fonte secundária: `0` seria "em
uso"; o ícone e a tela de Privacidade são o critério):

```bash
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone\NonPackaged\C:#Desenvolvimento#aurora-whisper#bin#aurora-whisper-agent.exe"
```

**Se não aparecer, anote:** o ícone aceso com o Agent parado, e o que a tela de Privacidade
lista. **É a pergunta que esta parte existe para responder**: a documentação da Microsoft só diz
que a sessão de áudio fica inativa com o cliente parado, não fala do ícone. Se ele não apagar, o
plano B é estacionar liberando o cliente — `audio.PoliticaLiberar` em `configDeAudio`
(`cmd/agent/main.go`), refazer a build e repetir M0 a M2. Custa de +50 a ~+250 ms até o primeiro
áudio, segundo relatos de terceiros; o M4 mede aqui.

### M1 — Segurando, o ícone acende; o bipe só com o microfone captando

**Faça:** no Bloco de Notas, segure `Ctrl+Win` por 2 s, falando "teste do microfone", e solte.
Repita três vezes.

**Deve aparecer:** o ícone de microfone acende ao apertar e apaga até ~1 s depois de soltar; o
bipe e a pílula "Gravando" saem ~0,7 s depois de apertar, como antes. No log, a linha
`audio: janela do microfone` de cada ditado com `bip_saiu=true`. Se o microfone demorou mais que
o limiar, sai também `WARN winruntime: o microfone demorou; o bipe e a pílula esperaram por ele`
com `espera_ms=` — não é defeito, é o bipe esperando o microfone, que é o que ele deve fazer.

**Se não aparecer, anote:** bipe sem o ícone aceso; ícone que não apaga depois de soltar; e o
`espera_ms` de qualquer espera.

### M2 — A primeira sílaba entra, falando junto com o bipe

**Faça:** 10 ditados com o **microfone do notebook** e depois 10 com o **microfone USB** (troque
em Configurações > Sistema > Som > Entrada, **com o Agent fechado** — com o cliente preparado, o
Agent continua no microfone de antes até reiniciar). Em cada um, segure `Ctrl+Win` e comece a
falar **junto com o bipe**, sem esperar mais nada; 3 s entre um e outro. Frases, cada uma duas
vezes: "Pagamento do DAS vence amanhã", "Depois confere o balanço", "Capital social
integralizado", "Transmitir a DCTFWeb hoje", "Bom dia, tudo certo".

**Deve aparecer:** no painel (P3), o **Bruto do ASR** de cada ditado com a primeira palavra
inteira.

**Se não aparecer, anote:** o ditado, o microfone, e a primeira palavra como saiu. Se ela sair
cortada ou abafada com o `inicio_rel_marca_ms` baixo (M4), a suspeita é o controle automático de
ganho ou a supressão de ruído do driver acordando a frio — as métricas não enxergam isso, só o
Bruto do ASR.

### M3 — `Ctrl+Win+D` pisca o ícone e não grava nada

**Faça:** aperte `Ctrl+Win+D` (cria uma área de trabalho virtual) e depois `Ctrl+Win+F4` (fecha
essa área). Repita com `Ctrl+Win+→` e `Ctrl+Win+←`, 5 vezes.

**Deve aparecer:** o ícone pisca por uma fração de segundo a cada atalho — aceito pelo Mauro em
29-set-2026 — e volta a apagar. Nenhum bipe, nenhuma pílula "Gravando", nenhum ditado novo no
painel, e nenhuma linha `audio: janela do microfone` para esses atalhos (só tentativa que chega a
gravar tem essa linha).

**Se não aparecer, anote:** ícone que fica aceso depois do atalho, bipe, ou ditado no painel.

### M4 — As linhas novas do log

**Faça**, com o Agent encerrado (só do PowerShell):

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "audio: microfone preparado"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "audio: janela do microfone"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "WARN.*(audio:|microfone)"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "ERROR (audio:|winruntime:)"
```

**Deve aparecer:**
- no segundo, uma linha por ditado, só com números e `true`/`false` — nenhum texto ditado.
  Anote, dos ditados do M2, `marca_ate_primeiro_pacote_ms` (do keydown ao primeiro pacote) e
  `inicio_rel_marca_ms` (quanto depois do keydown a gravação começou; o critério é até 100 ms,
  provisório), por microfone. E confira, nas linhas com `modo=sob_demanda` (as do M6 são de outro
  modo e não entram): `pre_roll_entregue_ms=0`,
  `pacote_anterior_ao_estacionamento=false`, `desligou_antes_do_corte=false`,
  `faltou_no_fim_ms=0` e `reabriu_por_falha=false` em todas; `aberturas` e `fechamentos`
  crescendo juntos. Em **todas** as linhas, dos dois modos, `perdeu_inicio_ms=0` — acima de 0, o
  começo do ditado não coube no buffer, e isso não deveria acontecer nem numa fala de 2 minutos;
- no terceiro, nenhum `áudio de antes desta tentativa entrou no buffer`, `o microfone desligou
  antes do corte`, `o áudio acabou antes de a tecla subir` nem `o ditado passou do que o buffer
  guarda`. Um `o microfone abriu depois de a fala começar` num ditado em que você falou junto com
  o bipe é o dado mais importante desta parte: anote o `inicio_rel_marca_ms` dele. Um `o Start do
  microfone falhou; ligou reabrindo o microfone padrão`, um `a leitura do microfone falhou; o
  microfone foi solto`, um `o microfone não ligou; o próximo ditado tenta de novo` ou um `o
  microfone parou de entregar áudio sem erro` não é defeito — o Agent se recuperou, ou vai se
  recuperar no próximo ditado —, mas anote quando foi (depois de suspender o notebook? de tirar o
  mic USB?). Com o Agent sem ninguém mexer em microfone, nenhum deles deveria aparecer;
- no quarto, **nada**. Uma linha `ERROR audio: o microfone parou de responder` ou `ERROR
  winruntime: o áudio parou` quer dizer que o microfone parou de vez e nenhum ditado funciona até
  reiniciar o Agent.

**Se não aparecer, anote:** as linhas inteiras dos `WARN` e dos `ERROR` (com o que você fez antes
de cada `ERROR`), e os números do segundo comando separados por microfone.

### M5 — Em campo de senha, o microfone nem liga

A ADR-015 promete mais que "não dita": num campo protegido o núcleo não pede o áudio, então o
microfone **nem liga**. Só o ícone mostra isso.

**Faça:** abra a senha clássica do Windows do B13 (`Win+R`, `rundll32.exe keymgr.dll,KRShowKeyMgr`,
**Adicionar...**), clique no campo **Senha** e espere 2 s (o rastreador de foco precisa ler o
campo). Segure `Ctrl+Win` por 2 s e solte. Repita três vezes, e depois no campo de senha da página
de login do Brave (B13) — **sem enviar nada**. No diálogo, clique **Cancelar**; não salve nada.

**Deve aparecer:** a pílula **"Ditado desativado em campo protegido."**, nenhum bipe, e o ícone de
microfone **não acende**, nem de relance; o Aurora Whisper não aparece em "Em uso no momento" na
tela de Privacidade. No log, `winruntime: overlay ... fase=blocked` e nenhuma linha `audio: janela
do microfone` para essas tentativas.

**Se não aparecer, anote:** o ícone aceso, mesmo piscando, numa tentativa em que a pílula disse
"campo protegido" — é o microfone ligando num campo de senha. A garantia é tão boa quanto o cache
de foco (ADR-015, "Consequências"): anote também se você trocou de campo pouco antes de apertar, e
as linhas `uia:` antes da tentativa.

### M6 — Microfone sempre pronto

A opção **Microfone sempre pronto** (Configurações, seção **Ditado**) devolve o comportamento de
antes da ADR-015: microfone aberto o tempo todo, com 300 ms de áudio de antes das teclas. Ela vale
**na hora**, ao **Salvar configurações**, sem reiniciar o Agent. O padrão é desligado, e este passo
termina com ela desligada.

**Faça**, com o Agent no ar (B1) e a caixa desmarcada:
1. menu → **Abrir Aurora Whisper** → **Configurações**, seção **Ditado**: marque **Microfone sempre
   pronto** e clique **Salvar configurações**. Não dite: olhe o ícone de microfone da barra de
   tarefas por uns 5 s;
2. no Bloco de Notas, dite cinco vezes começando a falar **junto com as teclas**, sem esperar o
   bipe: "Pagamento do DAS vence amanhã", "Depois confere o balanço", "Capital social
   integralizado", "Transmitir a DCTFWeb hoje", "Bom dia, tudo certo". Depois, no painel (P3),
   olhe o **Bruto do ASR** de cada um;
3. com a caixa **ainda marcada**, suspenda o notebook (Iniciar → Energia → **Suspender**), espere
   uns 10 s, retome e entre na sessão. **Assim que entrar**, sem esperar nada, dite no Bloco de
   Notas "Bom dia, tudo certo"; espere uns 5 s e dite de novo a mesma frase. Opcional, se o
   microfone USB for o padrão: tire o USB, espere uns 5 s e dite de novo (o Agent deve passar para
   o microfone que sobrou, o do notebook); ponha o USB de volta;
4. volte a **Configurações**, desmarque a caixa e clique **Salvar configurações**. Olhe o ícone por
   uns 5 s;
5. marque a caixa e salve de novo (o ícone acende). Feche o Agent pelo menu → **Sair** e suba de
   novo (B1), sem tocar em nada; olhe o ícone por uns 5 s. Por fim, abra **Configurações**,
   desmarque a caixa e salve: o padrão combinado é desligado.

**Deve aparecer:**
- passo 1: o ícone de microfone **acende sem você ditar**, em poucos segundos; no log,
  `audio: modo do microfone mudou` com `modo=sempre_pronto`;
- passo 2: a primeira palavra **inteira** no Bruto do ASR de todos os ditados, mesmo sem esperar o
  bipe. No log, a linha `audio: janela do microfone` desses ditados traz `modo=sempre_pronto`,
  `ja_ligado=true`, `pre_roll_entregue_ms` perto de 300 e `inicio_rel_marca_ms` negativo (é o
  pre-roll), e nenhum `WARN` novo com `audio:`;
- passo 3: o segundo ditado depois de retomar **sai inteiro**, com o bipe, e o ícone do microfone
  volta a acender sozinho depois de retomar. O primeiro, logo ao entrar, também deve sair; pode
  falhar, com a pílula de erro e **sem bipe**, se o microfone ainda estava voltando — não é
  defeito, desde que o segundo funcione. No log, uma de quatro coisas: nada de novo (o Windows só
  pausou o stream, e ele seguiu logo); `WARN audio: o microfone parou de entregar áudio sem erro`
  (o Windows pausou o stream e ele não voltou em 500 ms — o vigia de parada o soltou), seguida das
  duas linhas abaixo; `WARN audio: o microfone ligou e não entregou áudio nenhum` (o microfone
  voltou ligado e mudo — o vigia do primeiro pacote o soltou 2 s depois de ligar), seguida das
  duas linhas abaixo; ou `WARN audio: a leitura do microfone falhou; o microfone foi solto` com
  `modo=sempre_pronto` e `reabre_sozinho=true`, seguida de `INFO audio: o microfone reabriu
  depois de uma falha`. Antes da reabertura pode vir um ou dois `WARN audio: o microfone não
  religou; tento de novo depois do recuo`, com `recuo_ms=1000`, `2000`... — é o Windows ainda
  subindo o áudio. Um ditado no meio dessa espera tenta ligar na hora: se o microfone ainda não
  voltou, ele falha sem bipe e deixa `WARN audio: o microfone não ligou; o próximo ditado tenta de
  novo` com `modo=sempre_pronto`, `tentativas_perdidas=1` e `recuo_restante_ms`; se ligou e caiu
  antes de entregar áudio, falha do mesmo jeito e deixa `WARN audio: a leitura do microfone falhou`
  com `tentativas_perdidas=1` e `recuo_restante_ms` no lugar de `falhas_seguidas` e `recuo_ms`.
  Esses ditados não contam para a desistência: ditar várias vezes seguidas nessa espera não pode
  terminar em `ERROR` — salvo um microfone que liga, entrega um pedaço e cai de novo a cada vez
  (risco aceito na ADR-015; se acontecer, anote). No opcional do USB, o mesmo par de linhas. Em nenhum caso um `ERROR`.
  **Anote qual das quatro aconteceu**: é o que a ADR-015 não sabe;
- passo 4: o ícone **apaga**, em poucos segundos; no log, `audio: modo do microfone mudou` com
  `modo=sob_demanda`;
- passo 5: depois da subida, o ícone **acende sozinho**, sem ditado nem clique; no log,
  `preferencias: carregadas` com `microfone_sempre_pronto=true` e `audio: modo do microfone mudou`
  com `modo=sempre_pronto`. Depois do último Salvar, o ícone apaga de novo.

Para conferir o log de uma vez, feche o Agent (menu → **Sair**) e rode, só do PowerShell, um
comando por vez:

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "audio: modo do microfone mudou"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "janela do microfone.*modo=sempre_pronto"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "preferencias: carregadas"
```

E, para o passo 3, as linhas da recuperação do microfone e os `ERROR` (o `.` no lugar do "ã" de
"não" poupa o PowerShell 5.1 de acento no padrão):

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "a leitura do microfone falhou|o microfone reabriu|o microfone n.o religou|o microfone n.o ligou|o microfone parou de entregar|o microfone ligou e n.o entregou"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "ERROR (audio:|winruntime:)"
```

**Se não aparecer, anote:** o ícone que não acende ao salvar marcado, ou que só acende depois de
um ditado; o ícone que não apaga ao desmarcar (sem ditado em curso); a primeira palavra cortada
com a caixa marcada, junto com o `inicio_rel_marca_ms` e o `pre_roll_entregue_ms` da linha da
janela; depois de retomar a suspensão, o segundo ditado que falha ou que não bipa, um ditado que
bipa e falha, o ícone que não volta, e as linhas inteiras dos dois últimos comandos — um `ERROR audio: o microfone parou de responder`
diz que o Agent tentou religar oito vezes seguidas, por ~78 s, e desistiu; depois de reiniciar, o
ícone apagado com `microfone_sempre_pronto=true` no log — a preferência foi lida e não aplicada; ou
`microfone_sempre_pronto=false` com a caixa marcada, o que diz que o Salvar não gravou (anote o
`version=` da linha).

Se, depois de retomar, o ditado só voltou desmarcando e marcando de novo **Microfone sempre
pronto**, anote, com as linhas dos dois últimos comandos: isso não deveria mais ser preciso — o
ditado no meio da espera entre religações tenta ligar na hora, e o microfone que volta ligado e
mudo é solto sozinho (ADR-015, "Um keydown no sempre pronto corta o recuo" e "Vigia do primeiro
pacote").

Se `WARN audio: o microfone ligou e não entregou áudio nenhum` se repetir sozinho a cada ~32 s,
com o ícone piscando e os ditados funcionando, é um microfone cujo primeiro pacote demora mais de
2 s depois de ligar — o risco do fone Bluetooth da ADR-015 ("Riscos"), que nenhum microfone do
Mauro deveria ter. Anote qual microfone era o padrão e os `ligado_ha_ms` dessas linhas.

### M7 — Suspender e retomar no sob demanda

No padrão, o microfone está estacionado quando o notebook suspende, e o que o Windows fizer com
ele só aparece no keydown seguinte. Se o microfone ainda estiver voltando nesse keydown, aquele
ditado falha — e o seguinte tem de funcionar, sem reiniciar o Agent.

**Faça**, com a caixa **Microfone sempre pronto desmarcada** (o fim do M6):
1. suspenda o notebook (Iniciar → Energia → **Suspender**), espere uns 10 s e retome. **Assim que
   entrar na sessão**, sem esperar nada, dite no Bloco de Notas "Bom dia, tudo certo";
2. espere uns 5 s e dite de novo a mesma frase;
3. opcional, se o microfone USB for o padrão: tire o USB, dite logo em seguida (deve falhar: não
   há o microfone de antes), ponha o USB de volta, espere uns 5 s e dite de novo.

**Deve aparecer:**
- passo 1: o ditado sai inteiro, com o bipe. Pode também falhar, com a pílula de erro e **sem
  bipe**, se o microfone ainda estava voltando: não é defeito, desde que o passo 2 funcione;
- passo 2: o ditado **sai inteiro**, com o bipe;
- passo 3: o ditado sem o USB falha sem bipe (ou sai pelo microfone do notebook, se o Windows já
  o tiver feito padrão); o de depois de pôr o USB de volta **sai inteiro**, com o bipe;
- no log, só `WARN` e `INFO`, e **nenhum `ERROR`**. Um ditado que falhou deixa `WARN audio: o
  microfone não ligou; o próximo ditado tenta de novo` com `modo=sob_demanda` e
  `tentativas_perdidas=1`, e o seguinte que funcionou, `INFO audio: o microfone reabriu depois de
  uma falha`. Um que falhou com o `Start` dando certo, mas sem áudio a tempo, deixa `WARN
  winruntime: a tecla subiu antes de o microfone entregar áudio` — também não é defeito, se o
  seguinte funcionar: um ditado sem áudio solta o microfone ao terminar, e o seguinte o reabre. Um que funcionou reabrindo deixa `WARN audio: o Start do microfone falhou;
  ligou reabrindo o microfone padrão`.

Para conferir, com o Agent encerrado (só do PowerShell), um comando por vez:

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "o microfone n.o ligou|o microfone reabriu|o Start do microfone falhou|a leitura do microfone falhou"
```

```bash
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -Pattern "ERROR (audio:|winruntime:)"
```

**Se não aparecer, anote:** o ditado depois do primeiro que falhou também falhando — e quantos
seguidos, com o intervalo entre eles —; um `ERROR`; ou bipe num ditado que falhou. Com as linhas
inteiras dos dois comandos.

---

## O que este roteiro NÃO prova

- **Uso de dias seguidos**, com o Agent na bandeja a sessão inteira e vários logons.
- **Agent rodando como administrador.** O Agent não precisa disso, e este roteiro não o faz.
- **A matriz do B13 em outros navegadores** (Chrome, Edge, Firefox) e em aplicativos fora dela.
- **Uma janela de administrador aberta com a senha de outra conta.** O esperado é
  `janela_elevada=desconhecida` e a mensagem de "campo protegido", mas não foi visto.
- **O vigia e a leitura atrasada acontecendo de propósito.** Dependem de um aplicativo lento de
  verdade; o L2 só mostra se aconteceram durante o roteiro.
- **Várias abas editando o mesmo ditado.** O conflito de versão (409) tem teste automatizado; a
  experiência na tela, não.
- **Painel em modo nuvem**, que ainda não existe.
- **Microfone Bluetooth, e Remote Desktop.** O Mauro usa o microfone do notebook ou um USB; a
  ADR-015 registra os riscos dos dois casos — inclusive o sempre pronto que cicla a cada ~32 s
  com um fone cujo primeiro pacote demora mais de 2 s —, e o Windows 11 Home não recebe RDP.
- **"Microfone sempre pronto" pela bandeja.** A opção está só nas Configurações (M6); o menu da
  bandeja não tem item para ela. O M6 também não mede a latência do `Start` nem o AC-003 do
  sempre pronto em vários ditados: só confere que o modo muda na hora, e que o pre-roll aparece.
- **A desistência do sempre pronto.** Oito falhas seguidas, por ~78 s, sem microfone nenhum que
  abra, e a sessão morre com `ERROR` (ADR-015, "Recuperação do microfone"). Provocar isso aqui
  exigiria desativar todos os microfones da máquina; os testes automatizados cobrem o recuo, os
  keydowns no meio dele (que não apressam a desistência, com o microfone fora ou caindo antes do
  primeiro pacote) e a desistência.
- **Os vigias de propósito.** Um microfone que para de entregar sem erro, ou que liga e nunca
  entrega, só se vê se o Windows fizer isso ao suspender (M6, passo 3); não há como provocá-lo
  daqui. Os testes automatizados cobrem os dois vigias (a parada solta aos 500 ms, a janela muda
  aos 2 s, e nenhum antes) e o ditado seguinte funcionando.
- **A leitura que falha nos 200 ms da espera da última sílaba.** O ditado se perde inteiro, embora
  o áudio já cobrisse o keyup (ADR-015, "Recuperação do microfone"); a janela é estreita demais
  para provocar à mão.
- **Uma fala longa perto dos 120 s.** O buffer agora cabe o pior caso, com o limiar máximo de 2 s
  (M4 confere `perdeu_inicio_ms=0` nos ditados do dia a dia); segurar a tecla por dois minutos
  não faz parte deste roteiro.

---

## Resultado da execução

Preencha com passou/falhou e o que viu. Para o que falhou, junte as linhas do `agent.log`.

**Execução do Mauro, 30-set e 01-out-2026.** Ele testou e deu **OK** a: a bandeja, **B1 a B13**; o
painel, **P1 a P10**; e o microfone, **M0 a M7** (inclusive o suspender e retomar, no sob demanda e
no sempre pronto). O relato veio em bloco, sem detalhe por passo: a coluna **Observação** diz isso
onde vale, e **nada foi acrescentado além dele**. Célula em branco ou "não informado" quer dizer
**não relatado**, e não "falhou": o **B14**, o **L1** e o **L2** (nada foi dito sobre eles), os
passos marcados como opcionais (B12, P9 e o USB do M2 e do M7, que o relato em bloco pode ou não
ter incluído), e as contagens x/10 do M2.

| Passo | Resultado | Observação |
|---|---|---|
| B1 — ícone, sem janela preta | OK | relato em bloco (B1 a B13) |
| B1a — dono fixo: migração, cópia, painel | OK | relato em bloco (B1 a B13) |
| B2 — menu | OK | relato em bloco (B1 a B13) |
| B3 — ditar | OK | relato em bloco (B1 a B13) |
| B4 — pausar | OK | relato em bloco (B1 a B13) |
| B5 — segunda instância | OK | relato em bloco (B1 a B13) |
| P1 — abrir o painel | OK | relato em bloco (P1 a P10) |
| P2 — busca | OK | relato em bloco (P1 a P10) |
| P3 — editar, bruto marcado | OK | relato em bloco (P1 a P10) |
| P4 — salvar e cancelar | OK | relato em bloco (P1 a P10) |
| P5 — lixeira, Desfazer, `Ctrl+Z` | OK | relato em bloco (P1 a P10) |
| P6 — Lixeira, Restaurar | OK | relato em bloco (P1 a P10) |
| P7 — `F5` | OK | relato em bloco (P1 a P10) |
| P8 — Agent fechado e reiniciado | OK | relato em bloco (P1 a P10) |
| P9 — copiar (opcional) | não informado | opcional; o relato em bloco (P1 a P10) pode ou não tê-lo incluído |
| P10 — chave fora do cofre do navegador | OK | relato em bloco (P1 a P10) |
| B6 — ligar pelo menu | OK | relato em bloco (B1 a B13) |
| B7 — desligar no Gerenciador | OK | relato em bloco (B1 a B13) |
| B7b — Salvar não religa o que o Gerenciador desligou | OK | relato em bloco (B1 a B13) |
| B8 — religar pelo menu | OK | relato em bloco (B1 a B13) |
| B9 — logon | OK | relato em bloco (B1 a B13) |
| B10 — desligar pelo menu | OK | relato em bloco (B1 a B13) |
| B11 — sair | OK | relato em bloco (B1 a B13) |
| B12 — casos de borda (opcional) | não informado | opcional; o relato em bloco (B1 a B13) pode ou não tê-lo incluído |
| B13 — matriz: Bloco de Notas | OK | relatado como "B13 OK", sem a matriz por aplicativo |
| B13 — matriz: Brave | OK | relatado como "B13 OK", sem a matriz por aplicativo |
| B13 — matriz: WhatsApp Web | OK | relatado como "B13 OK", sem a matriz por aplicativo |
| B13 — matriz: VS Code | OK | relatado como "B13 OK", sem a matriz por aplicativo |
| B13 — matriz: senha clássica do Windows | OK | relatado como "B13 OK", sem a matriz por aplicativo |
| B14 — janela de administrador | | não relatado |
| L1 — agent.log | | não relatado |
| L2 — avisos do rastreador (fase 2) | | não relatado |
| M0 — repouso, ícone apagado | OK | relato em bloco (M0 a M7) |
| M1 — segurando: ícone aceso, bipe com o microfone captando | OK | relato em bloco (M0 a M7) |
| M2 — primeira sílaba: notebook (x/10) | OK | relato em bloco (M0 a M7); a contagem x/10 não foi informada |
| M2 — primeira sílaba: USB (x/10) | não informado | o relato não separa o microfone USB |
| M3 — `Ctrl+Win+D` e setas | OK | relato em bloco (M0 a M7) |
| M4 — linhas novas do log | OK | relato em bloco (M0 a M7) |
| M5 — campo de senha: o ícone não acende | OK | relato em bloco (M0 a M7) |
| M6 — microfone sempre pronto: acende, primeira palavra, suspender e retomar, apaga, reinício | OK | relato em bloco (M0 a M7), "inclusive suspender/retomar no sempre pronto" |
| M7 — sob demanda: suspender e retomar, ditado logo ao entrar e o seguinte; USB (opcional) | OK | relato em bloco (M0 a M7), "inclusive suspender/retomar no sob demanda"; o USB (opcional) não foi informado |

Commit testado: **não informado**. O Agent testado estava rodando com `StartTime` de 01-out-2026
00:27:41 (o Mauro o mostrou ao relatar), e o relato diz que o aprendizado funcionou e que a pílula
apareceu.
