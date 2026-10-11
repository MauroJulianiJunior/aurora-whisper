# Instalador — Roteiro de teste manual

Cobre o que o Setup do Aurora Whisper (`installer/aurora-whisper.iss`, ADR-018) faz de verdade e
que nenhum teste automatizado pode exercitar: aparecer o aviso do SmartScreen, instalar sem
administrador, criar o atalho, abrir o Agent instalado, guardar os dados em outra pasta, fechar o
Agent para instalar por cima, perguntar se apaga os dados ao desinstalar, e apagar mesmo as chaves
de API do Gerenciador de Credenciais. Os testes de `cmd/asrstage` só **leem** o `.iss`; o ISCC já o
compilou (o Setup `0.1.0-rc.1`, 135 MB), mas **ninguém executou o resultado** até este roteiro.

Por que é manual: o Setup grava no registro, cria atalhos, fecha processos e apaga uma pasta, e o
`CLAUDE.md` proíbe um agente de fazer isso nesta máquina. O que é decisão (qual pasta, quando
perguntar, o que apagar) tem teste; o que é chamada de sistema só se confere aqui.

Tempo estimado: 1 hora e meia, mais o tempo de criar a conta de teste e de entrar nela pela
primeira vez (o Windows monta o perfil, alguns minutos).

> **Este roteiro instala, desinstala e apaga dados. Faça-o numa CONTA DE TESTE do Windows, e não
> na sua.** O motivo está logo abaixo. Os comandos de limpeza da última parte apagam pastas: só
> rodam na conta de teste, e têm uma trava que confere o nome da conta antes.

---

## Antes de começar

### Use uma conta de teste, e não a sua

Na sua conta, este roteiro faria estragos reais:

- **O Agent instalado e o do `bin\` disputam a instância única.** O Agent segura o mutex
  `Local\AuroraWhisperAgent`; um segundo, na mesma conta, avisa "já está rodando" e sai. Pior: o
  Setup fecha qualquer `aurora-whisper-agent.exe` da conta (o nome do executável é o mesmo), então
  fecharia o seu Agent de desenvolvimento. Numa conta de teste não há disputa: o prefixo `Local\`
  vale por sessão do Windows.
- **Desinstalar apagando os dados apaga as suas chaves de API.** O desinstalador remove
  `AuroraWhisper/openai`, `AuroraWhisper/anthropic` e (desde a F6 da ADR-019) `AuroraWhisper/nuvem`,
  o token do PC na nuvem, do Gerenciador de Credenciais **da conta que o roda**. Na sua, seriam as
  chaves reais do Polish, que o Agent do `bin\` também usa, e o token do PC instalado. (O `bin\`
  guarda o dele em `AuroraWhisper/nuvem-portatil`, que o desinstalador não toca.)
- **A pasta de dados da instalação é `%LOCALAPPDATA%\AuroraWhisper`.** Um Agent de desenvolvimento
  fora do `bin\` montado já escreve ali (ADR-018, risco 11); apagar a pasta levaria junto o que ele
  tiver gravado.
- **Uma conta nova é um Windows limpo para o Agent:** sem dicionário, sem histórico, sem chaves, sem
  o `Run` do início automático. É o que quem instala vê pela primeira vez.
- **Uma conta padrão não tem administrador**, que é a condição do "instalar sem administrador".

### Criar a conta local de teste

Você precisa ser administrador para criar a conta; ela mesma **não** pode ser.

1. **Configurações** (`Win+I`) → **Contas** → **Outros usuários** → **Adicionar conta**.
2. Na janela da Microsoft que abrir, clique em **Não tenho as informações de entrada dessa
   pessoa** e depois em **Adicionar um usuário sem uma conta da Microsoft**. (Os textos podem variar
   um pouco com a versão do Windows; o que se quer é uma conta **local**, sem e-mail.)
3. Nome de usuário **com acento e com espaço**, por exemplo `José Teste`. É de propósito: a pasta do
   perfil herda o nome (`C:\Users\José Teste`), e se o servidor de voz carrega o modelo num caminho
   assim é uma das coisas que a ADR-018 não verificou (risco 3).
4. Escolha uma senha e as três perguntas de segurança que o Windows exigir. Anote a senha onde
   você a ache.
5. De volta em **Outros usuários**, a conta nova tem de aparecer como **Conta local** e **não**
   como **Administrador**. Não mude isso: o roteiro quer uma conta padrão.
6. Troque de usuário (`Win+L`, depois a conta nova) e espere o perfil ficar pronto. A sua sessão
   continua aberta em segundo plano; o seu Agent do `bin\`, se estiver rodando, não atrapalha o da
   conta de teste (cada sessão tem o seu mutex e o seu hook de teclado), mas segura uns 365 MB. Se a
   máquina estiver justa, feche-o antes pelo menu da bandeja → **Sair**.

Daqui em diante, **tudo é na conta de teste**, menos onde o texto diz "na sua conta".

### Qual Setup, e o que ele tem

O Setup é o da pasta `dist\`, montado por `scripts\montar-instalador.ps1`. O que Mauro compilou em
30-set/01-out-2026:

```text
C:\Desenvolvimento\aurora-whisper\dist\AuroraWhisper-Setup-0.1.0-rc.1.exe
```

Três mudanças entraram no repositório **depois** dele (01-out-2026). Cada uma só aparece num Setup
montado de um commit que a tenha; num Setup mais antigo, o passo dá o resultado de antes, e isso
não é defeito do instalador:

| O que | Commit (assunto) | Onde este roteiro olha |
|---|---|---|
| o servidor de voz roda na pasta do programa e recebe o modelo pelo **nome** do arquivo | `feat(asr): o whisper-server roda na pasta do programa...` | I5: o `whisper-server.log` |
| o ícone do programa e do Setup (o Pico estourado) | `feat(marca): gerador do icone do programa...` | I4: o ícone no Menu Iniciar |
| a pílula reafirma o topo ao ser mostrada | `fix(overlay): a pilula reafirma o topo ao ser mostrada...` | I6: a pílula sobre uma janela |

Para ter os três, monte de novo (`powershell -File scripts\montar-instalador.ps1 -Versao 0.1.0-rc.2`,
na sua conta) e use o `dist\AuroraWhisper-Setup-0.1.0-rc.2.exe`. Os comandos abaixo usam o
`rc.1`: troque o nome se for outro.

### Preparar o Setup para a conta de teste

Os três comandos abaixo rodam **na sua conta**, no PowerShell, um por vez. A pasta
`C:\Users\Public` é a que todas as contas leem e escrevem.

Copie o Setup e o `.sha256`:

```powershell
Copy-Item C:\Desenvolvimento\aurora-whisper\dist\AuroraWhisper-Setup-0.1.0-rc.1.exe C:\Users\Public\Documents\
```

```powershell
Copy-Item C:\Desenvolvimento\aurora-whisper\dist\AuroraWhisper-Setup-0.1.0-rc.1.exe.sha256 C:\Users\Public\Documents\
```

Um arquivo copiado de dentro da própria máquina **não** tem a marca de "veio da internet", e sem
ela o SmartScreen nem aparece (o I1 testaria nada). Para imitar o download, ponha a marca
(`ZoneId=3`, a da zona Internet) no arquivo copiado:

```powershell
Set-Content -Path C:\Users\Public\Documents\AuroraWhisper-Setup-0.1.0-rc.1.exe -Stream Zone.Identifier -Value '[ZoneTransfer]','ZoneId=3'
```

É uma **simulação**. A prova de verdade é baixar o Setup da página de Releases, e ela só existe
depois da publicação (que espera o portão do workflow de release e o release do modelo).

### Conferir o Setup (na conta de teste)

```powershell
Get-FileHash -Algorithm SHA256 C:\Users\Public\Documents\AuroraWhisper-Setup-0.1.0-rc.1.exe
```

```powershell
Get-Content C:\Users\Public\Documents\AuroraWhisper-Setup-0.1.0-rc.1.exe.sha256
```

Os dois hashes têm de ser iguais (o `Get-FileHash` escreve em maiúsculas, o `.sha256` em minúsculas).
Anote o tamanho: `(Get-Item C:\Users\Public\Documents\AuroraWhisper-Setup-0.1.0-rc.1.exe).Length / 1MB`.

### O estado de antes: a conta de teste está limpa

Na conta de teste, antes de instalar. Cada comando só lê.

```powershell
Test-Path "$env:LOCALAPPDATA\Programs\Aurora Whisper"
```

```powershell
Test-Path "$env:LOCALAPPDATA\AuroraWhisper"
```

```powershell
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v AuroraWhisper
```

```powershell
cmdkey /list:AuroraWhisper/openai
```

```powershell
cmdkey /list:AuroraWhisper/nuvem
```

Os dois `Test-Path` têm de dar `False`; o `reg query` tem de dizer que não achou o valor; os dois
`cmdkey` não podem listar credencial nenhuma. Se algum não bater, a conta não está limpa: use outra.

Anote também, só olhando e **sem mudar nada**: se o **Controle inteligente de aplicativos**
(Segurança do Windows → Controle de aplicativo e navegador) está ligado, desligado ou em avaliação;
e qual antivírus está ativo. O I1 pode depender disso.

---

## Parte 1 — Instalar

### I1 — O SmartScreen, sem assinatura

**Faça:** no Explorador de Arquivos, abra `C:\Users\Public\Documents` e dê duplo clique em
`AuroraWhisper-Setup-0.1.0-rc.1.exe`.

**Deve aparecer:**
- o arquivo do Setup, no Explorador, com o ícone do Pico estourado (só num Setup montado depois do
  commit do ícone; antes, é o do instalador padrão do Inno);
- a tela azul **O Windows protegeu o computador** (ou "O Microsoft Defender SmartScreen impediu a
  inicialização de um aplicativo não reconhecido"). Clique em **Mais informações**: o nome do
  arquivo aparece, o editor é **Publicador desconhecido**, e surge o botão **Executar assim mesmo**.
  Clique nele.

**Se não aparecer, anote:**
- se a tela **não** apareceu: o arquivo pode ter perdido a marca (confira com `Get-Content -Path
  C:\Users\Public\Documents\AuroraWhisper-Setup-0.1.0-rc.1.exe -Stream Zone.Identifier`) ou o
  SmartScreen estar desligado na conta. Anote qual;
- se a tela apareceu **sem** o botão **Executar assim mesmo**: é o Controle inteligente de
  aplicativos bloqueando sem saída (a ADR-018 prevê, e não foi visto). Anote o texto inteiro e o
  estado do Controle. **Não o desligue** para continuar: peça ao Mauro;
- um aviso do antivírus: o nome do antivírus, o texto, e o nome da ameaça que ele disse achar. O
  hook de teclado e a injeção de entrada são os suspeitos de sempre.

### I2 — Instalar sem administrador

**Faça:** com o Setup aberto, avance as páginas. Não mude nada: a pasta é a sugerida, e a tarefa
**Criar um atalho na área de trabalho** vem desmarcada. Na página **Pronto para instalar**, leia o
que ela mostra e clique em **Instalar**. Espere o fim. Na última página, **deixe marcado**
**Abrir o Aurora Whisper agora** e clique em **Concluir**.

**Deve aparecer:**
- **nenhum pedido de administrador** (a janela azul-escura do UAC) em momento nenhum. A exceção
  seria faltar o Visual C++, e esta máquina o tem (v14.51): se o Setup mostrar o aviso do Visual
  C++ na página **Pronto para instalar**, anote a versão que a máquina tem;
- a página **Pronto para instalar** mostrando a pasta
  `C:\Users\José Teste\AppData\Local\Programs\Aurora Whisper` (com o nome da sua conta de teste);
- o Setup terminando sozinho, em alguns segundos (o modelo tem 139 MB, o que mais demora);
- na última página, a caixa **Abrir o Aurora Whisper agora**, marcada.

O Setup grava um log em `%TEMP%`. Anote o nome dele, que é útil se algo falhar:

```powershell
Get-ChildItem $env:TEMP -Filter 'Setup Log*' | Sort-Object LastWriteTime | Select-Object -Last 1 Name, Length
```

**Se não aparecer, anote:** o pedido de administrador (e o que o causou), uma página de erro com o
texto inteiro, a mensagem sobre a pasta de dados (**"A pasta do programa não pode ser a pasta de
dados..."**, que só aparece se você mudou a pasta), e o `Setup Log` inteiro.

### I3 — Onde ficou cada coisa

**Faça:** no PowerShell da conta de teste, **com o Agent ainda aberto ou fechado**, tanto faz.

```powershell
Get-ChildItem "$env:LOCALAPPDATA\Programs\Aurora Whisper" | Select-Object Name, Length
```

```powershell
(Get-ChildItem "$env:LOCALAPPDATA\Programs\Aurora Whisper" -Recurse -File | Measure-Object Length -Sum).Sum / 1MB
```

```powershell
Test-Path "$env:LOCALAPPDATA\AuroraWhisper"
```

**Deve aparecer:**
- na pasta do programa: `aurora-whisper-agent.exe`, `whisper-server.exe`, as **12 DLLs** (`whisper.dll`,
  `ggml.dll`, `ggml-base.dll` e as `ggml-cpu-*.dll`), `ggml-small-q4_0.bin`, as pastas `painel` e
  `licencas`, e o desinstalador (`unins000.exe` e `unins000.dat`);
- o total perto de **168 MiB** (a ADR mediu 168,3 MiB descomprimido);
- **não** há `dicionario.json`, `historico.db` nem o arquivo `portatil` ali: o Setup nunca cria
  dado de quem usa;
- se você fez o I2 e **abriu** o Agent: a pasta `%LOCALAPPDATA%\AuroraWhisper` já existe (o Agent a
  criou ao subir). Se **não** abriu, o `Test-Path` dá `False`: o Setup não cria a pasta de dados.

**Se não aparecer, anote:** arquivo faltando ou sobrando, tamanho muito diferente de 168 MiB, e
qualquer arquivo de dados dentro da pasta do programa.

### I4 — Menu Iniciar e Aplicativos

**Faça:** abra o Menu Iniciar e digite `Aurora`. Depois abra **Configurações → Aplicativos →
Aplicativos instalados** e procure o Aurora Whisper. Na área de trabalho, procure um atalho.

**Deve aparecer:**
- no Menu Iniciar, **Aurora Whisper**, com o **ícone do Pico estourado**: um quadrado preto de
  cantos arredondados, quatro colchetes verdes nos cantos e uma barra clara no meio, que passa da
  moldura por cima e por baixo. Com um Setup anterior ao commit do ícone, o ícone é o genérico do
  Windows, e isso é o esperado;
- em **Aplicativos instalados**, **Aurora Whisper 0.1.0-rc.1**, editor **Mauro Juliani Junior**, com
  o mesmo ícone;
- **nenhum** atalho na área de trabalho (a tarefa veio desmarcada).

Para conferir os dados da entrada do Windows, sem mexer (o `Select-Object` pega só os campos):

```powershell
Get-ItemProperty -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\{D8740DB2-3756-465C-96BA-EF589951C223}_is1' | Select-Object DisplayName, DisplayVersion, Publisher, InstallLocation, DisplayIcon
```

A chave está em `HKCU`, e não em `HKLM`: é a instalação por usuário. `DisplayIcon` aponta para o
`aurora-whisper-agent.exe` instalado.

**Se não aparecer, anote:** ícone genérico (e de que Setup, para separar "Setup antigo" de
"ícone não entrou"), ícone borrado ou cortado num tamanho (diga qual: lista pequena, mosaico,
Configurações), atalho que apareceu sem ser pedido, e entrada em `HKLM` (o Setup não devia gravar lá).

---

## Parte 2 — Usar

### I5 — Abrir, e o que o log diz

**Faça:** se o Agent não abriu no fim do Setup, abra-o pelo Menu Iniciar (**Aurora Whisper**).
Espere o ícone aparecer na bandeja, sem clicar em nada, e **anote quanto tempo levou** desde o
clique até o ícone. É a primeira subida da conta: o modelo vem de um disco que ninguém leu ainda, e
o Agent calcula o SHA-256 dos 139 MB (a ADR-018, risco 8, estima vários segundos; ninguém mediu).

Depois, no PowerShell da conta de teste:

```powershell
Get-Process aurora-whisper-agent, whisper-server | Select-Object Name, Id, StartTime
```

```powershell
Select-String -Path "$env:LOCALAPPDATA\AuroraWhisper\agent.log" -Encoding UTF8 -SimpleMatch "agent: rodando"
```

```powershell
Select-String -Path "$env:LOCALAPPDATA\AuroraWhisper\agent.log" -Encoding UTF8 -SimpleMatch "dados="
```

Agora o `whisper-server.log` **inteiro**, e não só a linha do modelo. As duas linhas que o servidor
imprime com caminho (a do modelo e a da biblioteca do processador, `load_backend: loaded CPU backend
from ...`) precisam sair sem a pasta de quem usa. Os dois primeiros comandos **mostram** o que
deve estar lá; os três seguintes **caçam** o que não pode:

```powershell
Select-String -Path "$env:LOCALAPPDATA\AuroraWhisper\whisper-server.log" -Encoding UTF8 -SimpleMatch "ggml-small-q4_0", "<programa>"
```

```powershell
Select-String -Path "$env:LOCALAPPDATA\AuroraWhisper\whisper-server.log" -Encoding UTF8 -Pattern '[A-Za-z]:\\', 'Users'
```

```powershell
foreach ($enc in 'UTF8', 'Default') { Select-String -Path "$env:LOCALAPPDATA\AuroraWhisper\whisper-server.log" -Encoding $enc -SimpleMatch $env:USERNAME }
```

O último roda duas vezes, de propósito: o servidor pode escrever o nome com acento em UTF-8 ou na
página de código do Windows, e cada leitura só acha uma das duas.

**Deve aparecer:**
- **nenhuma janela preta** (nem a de console, nem uma do `whisper-server`), e o ícone na bandeja
  (o Pico estourado; se não estiver à vista, está na setinha `^`);
- um `aurora-whisper-agent.exe` e um `whisper-server.exe`;
- no `agent.log`, `agent: rodando` com `versao=0.1.0-rc.1`; a hora dessa linha menos o `StartTime`
  do Agent é o tempo da subida;
- uma linha `pastas: pasta de dados resolvida` com **`dados=localappdata`** (e **não** `portatil`);
- no `whisper-server.log`, duas linhas com o que o servidor carregou: a do modelo diz só
  **`ggml-small-q4_0.bin`** (F2), e a da biblioteca do processador diz
  **`<programa>\ggml-cpu-...dll`** (o filtro de caminhos da saída do servidor). **Os dois comandos
  de caça (`[A-Za-z]:\\`/`Users`, e o nome da conta) não podem imprimir nada**: nem `C:\Users\...`, nem
  `José Teste`. Com um Setup anterior a esse filtro, a linha da biblioteca traz o caminho inteiro, e o
  servidor funciona do mesmo jeito: anote qual dos dois viu. **Este é o resultado que decide abrir o
  portão `F2_PRONTA`** do workflow de release.

Para ver de que commit é o Agent instalado (só lê o executável, não o roda):

```powershell
go version -m "$env:LOCALAPPDATA\Programs\Aurora Whisper\aurora-whisper-agent.exe"
```

Procure `vcs.revision` e `vcs.modified`. Se o `go` não estiver no PATH da conta de teste, pule: a
linha `versao=` do log já diz a versão.

**Se não aparecer, anote:**
- janela preta e o título dela; ícone que nunca aparece com o processo no Gerenciador de Tarefas;
- "O Aurora Whisper foi encerrado por um erro" e o texto inteiro;
- um aviso de erro na bandeja ou uma página dizendo que o modelo ou o servidor não subiu: copie o
  `agent.log` e o `whisper-server.log` **inteiros**. Servidor que **não sobe com a conta de nome com
  acento** é o achado que este roteiro quer: diga se as linhas do modelo e da biblioteca no log
  tinham o caminho inteiro (Setup anterior à F2 ou ao filtro) ou só o nome e o rótulo `<programa>`.
  Se um dos comandos de caça imprimir algo, copie **a linha** (não o log, que pode ter mais) e diga
  qual dos dois comandos a achou;
- o tempo de subida, mesmo que tenha dado certo: é o dado que falta à ADR.

### I6 — Ditar

**Faça:** no Bloco de Notas, segure `Ctrl+Win`, espere o bipe, fale **"o abacaxi ficou na mesa"** e
solte.

**Deve aparecer:** a pílula na parte de baixo da tela enquanto você fala, e o texto no Bloco de
Notas ao soltar. Se o texto não sai e o ícone da bandeja não acende ao segurar a tecla, olhe
**Configurações → Privacidade e segurança → Microfone** (a permissão é por conta, e a conta de
teste é nova) e anote o que a página mostra.

Para ver a pílula **sobre** uma janela que subiu depois dela (a reafirmação do topo; só num Setup
com o commit `fix(overlay)`), use uma janela topmost **comum**, aberta **depois** do Agent.

**Não use o Gerenciador de Tarefas para isto.** Com "Sempre visível" ele fica numa faixa própria do
Windows, acima das topmost comuns (é o que descrevem terceiros, e não a Microsoft; não foi verificado
aqui), e uma janela de programa comum não passa dele. A pílula pode ficar atrás dele com o código
certo, e o teste não distinguiria o certo do errado.

**Faça:** arraste o Bloco de Notas para a parte de **cima** da tela. Num PowerShell da conta de
teste, abra uma janela de teste que fica sempre por cima, encostada na borda de baixo e no centro da
tela (é onde a pílula aparece):

```powershell
Add-Type -AssemblyName System.Windows.Forms; $tela = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds; $f = New-Object System.Windows.Forms.Form; $f.Text = 'Janela de teste, sempre por cima'; $f.TopMost = $true; $f.StartPosition = 'Manual'; $f.Width = 900; $f.Height = 300; $f.Left = [int](($tela.Width - $f.Width) / 2); $f.Top = $tela.Height - $f.Height; [void]$f.ShowDialog()
```

O PowerShell fica preso até você fechar a janela de teste; é o esperado. Clique no Bloco de Notas
(a janela de teste não o cobre, fica embaixo) e dite de novo. Depois, feche a janela de teste. Se
preferir, o **Always On Top** do PowerToys (`Win+Ctrl+T`) faz o mesmo com qualquer janela comum.

**Deve aparecer, nesse segundo teste:** a pílula **por cima** da janela de teste, e o texto entrando
no Bloco de Notas (o foco não pode ter ido para a pílula).

```powershell
Select-String -Path "$env:LOCALAPPDATA\AuroraWhisper\agent.log" -Encoding UTF8 -SimpleMatch "abacaxi"
```

Este último comando **não pode imprimir nada**: o texto ditado nunca vai para o log.

**Se não aparecer, anote:** nenhum texto no Bloco de Notas, a pílula que não aparece (e se estava
atrás da janela de teste), foco que saiu do Bloco de Notas, e qualquer linha do `agent.log`
com `WARN` ou `ERROR`.

### I7 — O dicionário vazio

**Faça:** na conta de teste:

```powershell
Get-Content "$env:LOCALAPPDATA\AuroraWhisper\dicionario.json" -Encoding UTF8
```

```powershell
Select-String -Path "$env:LOCALAPPDATA\AuroraWhisper\agent.log" -Encoding UTF8 -SimpleMatch "dicionario:"
```

**Deve aparecer:**
- o arquivo existe, criado pelo Agent na primeira subida, com `"usuario": "local"` e
  `"entradas": []` (nenhum termo: o Setup não leva o dicionário do Mauro);
- no log, a linha `dicionario: criei um dicionário vazio na pasta de dados, para o aprender do
  painel funcionar desde o primeiro dia`. Se, em vez dela, aparecer `não consegui criar o
  dicionário vazio`, anote a linha: o aprender não vai funcionar (I8).

**Se não aparecer, anote:** arquivo ausente, com termos que você não pôs, ou em outra pasta; a
linha de aviso inteira.

### I8 — Aprender funcionando, desde o primeiro dia

Vale **só** com o dicionário vazio criado (I7): é o que o Setup promete (ADR-017 e ADR-018).

**Faça:**
1. no Bloco de Notas, dite **"o contrato está assinado"**;
2. menu da bandeja → **Abrir Aurora Whisper**; clique no ditado; no **Texto final**, troque
   **contrato** por **acordo**; **Salvar** (`Ctrl+Enter`);
3. na pílula de baixo, clique em **Aprender**;
4. dite **"o contrato está assinado"** de novo, **sem reiniciar o Agent**;
5. no painel, **Configurações** → **Aprendido no painel** → **Desfazer** na linha do par; dite de
   novo a mesma frase;
6. **aprenda o par outra vez** (repita os passos 1 a 3) e **deixe-o aprendido**: o I10, o I11 e o
   I12 conferem que ele sobrevive.

**Deve aparecer:**
- no passo 2, a pílula `Texto salvo. Aprender "contrato" → "acordo"?` com o botão **Aprender** (as
  aspas podem ser curvas);
- no passo 3, `Aprendido: "contrato" vira "acordo" no próximo ditado`;
- no passo 4, o Bloco de Notas recebe **"o acordo está assinado"**;
- em **Aprendido no painel**, uma linha com o par e o botão **Desfazer**;
- depois do Desfazer, a mesma frase volta a sair com **contrato**;
- `dicionario.json` com a entrada depois do passo 3, e igual ao de antes depois do passo 5.

Se o ASR escrever a frase de outro jeito (por exemplo, "contrato" com maiúscula), use a palavra
que ele escreveu; o par é o que **ele** escreveu.

**Se não aparecer, anote:** pílula sem o botão **Aprender**, a mensagem da pílula depois do clique
(**Não deu certo...** e o texto), o par que não valeu no ditado seguinte, e se a lista de
**Aprendido no painel** ficou vazia.

### I9 — O início com o Windows começa desligado

**Faça:** no PowerShell da conta de teste, antes de tocar na bandeja:

```powershell
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v AuroraWhisper
```

Abra o menu da bandeja. Depois ligue: menu → **Iniciar com o Windows**. Confira de novo:

```powershell
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v AuroraWhisper
```

**Deve aparecer:**
- na primeira consulta, **o valor não existe**: o Setup não liga o início automático (decisão 3
  da ADR-018); quem liga é a pessoa;
- o item **Iniciar com o Windows** desmarcado no menu, e depois marcado;
- na segunda consulta, o valor `AuroraWhisper` com o caminho do executável **entre aspas**:
  `"C:\Users\José Teste\AppData\Local\Programs\Aurora Whisper\aurora-whisper-agent.exe"`.

Deixe-o **ligado**: o I11 confere que o desinstalador o remove.

**Se não aparecer, anote:** valor que já existia antes de ligar, valor sem aspas, ou apontando para
outro lugar.

---

## Parte 3 — Atualizar, desinstalar, reinstalar

### I10 — Instalar por cima (a mesma versão)

**Faça:** primeiro, crie três credenciais de **mentira** no Gerenciador de Credenciais da conta de
teste, com os nomes que o Agent usa (o I11 e o I13 conferem o que acontece com elas). Um comando por
vez:

```powershell
cmdkey /generic:AuroraWhisper/openai /user:teste /pass:chave-falsa-de-teste
```

```powershell
cmdkey /generic:AuroraWhisper/anthropic /user:teste /pass:chave-falsa-de-teste
```

```powershell
cmdkey /generic:AuroraWhisper/nuvem /user:teste /pass:token-falso-de-teste
```

O painel pode passar a mostrar o Polish como "com chave": não ligue o Polish, a chave é falsa. O
token falso da nuvem não liga nada: sem ter conectado pelo painel, o Agent continua mostrando a
nuvem **Desligada** (ele só se diz conectado com o aparelho gravado no histórico e o token juntos).
Confira que o `cmdkey` as lista (`cmdkey /list:AuroraWhisper/openai`). Se **não** listar a que você
acabou de criar, olhe no **Gerenciador de Credenciais** (Painel de Controle → Contas de Usuário →
Credenciais do Windows → Credenciais genéricas) e anote: então é o `cmdkey` que não acha uma
credencial genérica pelo nome puro (a dúvida que levou o desinstalador a usar a API em vez dele), e
as conferências de chave dos passos seguintes se fazem naquela tela.

Com o Agent **aberto** e o início automático ligado (I9), execute o Setup de novo (o mesmo arquivo,
duplo clique; o SmartScreen pode aparecer outra vez). Avance e instale. Na última página, deixe
**Abrir o Aurora Whisper agora** marcado.

**Deve aparecer:**
- a página da pasta pode nem aparecer (o Inno lembra a anterior); se aparecer, vem com a mesma
  pasta;
- o Setup **fecha o Agent sozinho** (o ícone some da bandeja), sem você clicar em **Sair**, e
  segue. Se o Agent não fechar em 20 segundos, o Setup para com **O Aurora Whisper está aberto e
  não fechou a tempo**;
- no fim, o Agent abre, e o painel traz **o mesmo histórico** de antes, e o par aprendido no I8
  continua em **Aprendido no painel** (o Setup não toca nos dados);
- `reg query` do I9: o `Run` continua lá, igual (o Setup não mexe nele);
- a pasta do programa **sem** restos da versão anterior: a pasta `painel` foi trocada inteira.

```powershell
cmdkey /list:AuroraWhisper/openai
```

**Deve aparecer, nesse comando:** a credencial ainda listada.

**Se não aparecer, anote:** o Agent que não fechou (e o que o `Setup Log` diz do `taskkill`), dado
que sumiu (histórico, dicionário), o `Run` que mudou ou sumiu, a credencial que sumiu, ou o Agent
que não abriu no fim.

### I11 — Desinstalar, mantendo os dados

Com o Agent **aberto**, para ver o pedido de fechar.

**Faça:** rode o desinstalador pelo comando abaixo (equivale a **Configurações → Aplicativos →
Desinstalar**, e deixa um log do que ele fez):

```powershell
& "$env:LOCALAPPDATA\Programs\Aurora Whisper\unins000.exe" /LOG="$env:TEMP\desinstalar-mantendo.log"
```

Responda: **Sim** ao pedido de fechar o Agent, **Sim** à confirmação padrão de remover, e, na
pergunta **Apagar também os dados do Aurora Whisper deste usuário?**, **Não**. (O botão padrão da
pergunta é **Não**: `Enter` não apaga.)

**Deve aparecer:**
- o desinstalador pedindo para fechar o Agent (**O Aurora Whisper está aberto. Para desinstalar,
  ele precisa ser fechado.**); com **Sim**, o ícone some da bandeja; com **Não**, nada se fecha e
  nada é desinstalado (teste isso uma vez, antes, se quiser);
- ao fim, o programa some: a pasta `%LOCALAPPDATA%\Programs\Aurora Whisper` não existe mais (ou
  existe vazia, se algo a prendia: anote), e **Aurora Whisper** sai do Menu Iniciar e de
  **Aplicativos instalados**;
- o início automático **foi removido**: o `reg query` do I9 diz que não achou o valor (o
  desinstalador remove o `Run` quando ele aponta para o executável que está sendo desinstalado);
- os **dados continuam**:

```powershell
Get-ChildItem "$env:LOCALAPPDATA\AuroraWhisper" | Select-Object Name, Length
```

  com `historico.db`, `dicionario.json`, `agent.log` e `whisper-server.log`;
- as **chaves de API e o token da nuvem continuam**: `cmdkey /list:AuroraWhisper/openai` e
  `cmdkey /list:AuroraWhisper/nuvem` ainda os listam (apagar o token a cada desinstalação
  desconectaria o PC a cada atualização).

**Se não aparecer, anote:** dado ou chave que sumiu (isso seria grave: a resposta foi **Não**),
o `Run` que ficou, o programa que ficou, o Agent que não fechou. Junte o
`%TEMP%\desinstalar-mantendo.log`.

### I12 — Reinstalar, e ver o histórico de volta

**Faça:** execute o Setup de novo, instale, abra o Agent e abra o painel.

**Deve aparecer:**
- o painel traz o histórico que você tinha, nas mesmas linhas, e **Aprendido no painel** com o
  que você aprendeu e não desfez (os dados não foram tocados, e o Agent **não** troca um dicionário
  que já existe pelo vazio);
- o item **Iniciar com o Windows** **desmarcado**: o desinstalador o tinha removido, e o Setup
  não o religa;
- o mesmo dono do histórico (as linhas aparecem: o dono fixo mora dentro do `historico.db`).

**Se não aparecer, anote:** histórico vazio (o Agent teria criado um banco novo: a pasta de dados
mudou, ou o banco foi apagado), dicionário vazio, o início automático ligado sem você ter pedido.

### I13 — Desinstalar, apagando os dados

Antes, confira que as três credenciais de mentira ainda estão lá (`cmdkey
/list:AuroraWhisper/openai`, `/anthropic` e `/nuvem`). Se o I11 as apagou, crie-as de novo (comandos
do I10).

**Faça:** com o Agent aberto, rode:

```powershell
& "$env:LOCALAPPDATA\Programs\Aurora Whisper\unins000.exe" /LOG="$env:TEMP\desinstalar-apagando.log"
```

Responda **Sim** ao pedido de fechar, **Sim** à confirmação padrão e, na pergunta sobre os dados,
**Sim**. A pergunta cita as três credenciais (**AuroraWhisper/openai, AuroraWhisper/anthropic e
AuroraWhisper/nuvem**) e diz que **o PC continua em Aparelhos, na nuvem, até você revogá-lo lá**.

**Deve aparecer:**
- o Agent fechando, e o programa saindo como no I11;
- **o que some:** a pasta `%LOCALAPPDATA%\AuroraWhisper` inteira (histórico, dicionário,
  preferências, logs); o `Run`; e as três credenciais do Gerenciador de Credenciais;
- no `%TEMP%\desinstalar-apagando.log`, as linhas `Credencial AuroraWhisper/openai apagada.`,
  `Credencial AuroraWhisper/anthropic apagada.` e `Credencial AuroraWhisper/nuvem apagada.` (se uma
  delas disser `nao apagada (erro 1168...)`, a credencial não existia: confira o passo anterior).

```powershell
Test-Path "$env:LOCALAPPDATA\AuroraWhisper"
```

```powershell
cmdkey /list:AuroraWhisper/openai
```

```powershell
cmdkey /list:AuroraWhisper/anthropic
```

```powershell
cmdkey /list:AuroraWhisper/nuvem
```

O `Test-Path` tem de dar `False`, e nenhum `cmdkey` lista nada (ele escreve `NONE`, ou `NENHUM`).

**Se não aparecer, anote:** pasta que ficou (e o que tinha dentro: um arquivo preso, se o Agent
ainda estava aberto), chave que ficou no Gerenciador de Credenciais, e qualquer mensagem do
desinstalador (**Por segurança, o desinstalador só apaga a pasta de dados quando...**, **Não consegui
apagar tudo...**). Junte o `%TEMP%\desinstalar-apagando.log`.

### I14 — Conferir que nada ficou

**Faça:** na conta de teste, cada comando só lê:

```powershell
Test-Path "$env:LOCALAPPDATA\Programs\Aurora Whisper"
```

```powershell
Test-Path "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Aurora Whisper.lnk"
```

```powershell
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\{D8740DB2-3756-465C-96BA-EF589951C223}_is1"
```

```powershell
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v AuroraWhisper
```

```powershell
Get-Process aurora-whisper-agent, whisper-server -ErrorAction SilentlyContinue
```

**Deve aparecer:** `False`, `False`, o `reg query` dizendo que não achou a chave, o `reg query`
dizendo que não achou o valor, e nenhum processo. Com o I13, a pasta de dados e as chaves também já
sumiram.

O que **fica** de propósito, e não é defeito: os `Setup Log` e os logs do desinstalador em
`%TEMP%`, e a pasta `%LOCALAPPDATA%\Programs` (vazia; é do Windows, e não do Aurora Whisper).

**Se não aparecer, anote:** o que ficou, e onde.

---

## Devolver a máquina ao estado de antes

Tudo aconteceu na conta de teste, então a sua não mudou. Para desfazer o resto:

1. **Saia da conta de teste** (Iniciar → o nome da conta → **Sair**) e volte à sua.
2. **Apague as cópias do Setup**, na sua conta:

```powershell
Remove-Item C:\Users\Public\Documents\AuroraWhisper-Setup-0.1.0-rc.1.exe
```

```powershell
Remove-Item C:\Users\Public\Documents\AuroraWhisper-Setup-0.1.0-rc.1.exe.sha256
```

3. **Apague a conta de teste**, e com ela o perfil, as chaves de mentira e o que ficou: **Configurações
   → Contas → Outros usuários →** a conta de teste → **Remover** → **Excluir conta e dados**. Essa
   é a limpeza completa; o resto abaixo só serve se você quiser **manter** a conta.
4. **Se o desinstalador falhou e você quer manter a conta**, na conta de teste, com o Agent fechado
   (Gerenciador de Tarefas: nenhum `aurora-whisper-agent.exe`). Estes comandos apagam pastas e
   chaves **da conta em que rodam**: se rodassem na sua, apagariam o seu histórico, o seu
   dicionário e as suas chaves de API. Por isso cada um só age se o nome da conta for o da conta de
   teste. **Troque `José Teste` pelo nome que você deu a ela**, nos sete comandos; em qualquer outra
   conta eles só escrevem `Conta errada: pare.`

```powershell
if ($env:USERNAME -eq 'José Teste') { Remove-Item -Recurse -Force "$env:LOCALAPPDATA\Programs\Aurora Whisper" } else { 'Conta errada: pare.' }
```

```powershell
if ($env:USERNAME -eq 'José Teste') { Remove-Item -Recurse -Force "$env:LOCALAPPDATA\AuroraWhisper" } else { 'Conta errada: pare.' }
```

```powershell
if ($env:USERNAME -eq 'José Teste') { Remove-Item "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Aurora Whisper.lnk" } else { 'Conta errada: pare.' }
```

```powershell
if ($env:USERNAME -eq 'José Teste') { reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v AuroraWhisper } else { 'Conta errada: pare.' }
```

```powershell
if ($env:USERNAME -eq 'José Teste') { reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\{D8740DB2-3756-465C-96BA-EF589951C223}_is1" } else { 'Conta errada: pare.' }
```

```powershell
if ($env:USERNAME -eq 'José Teste') { cmdkey /delete:AuroraWhisper/openai } else { 'Conta errada: pare.' }
```

```powershell
if ($env:USERNAME -eq 'José Teste') { cmdkey /delete:AuroraWhisper/anthropic } else { 'Conta errada: pare.' }
```

Os `reg delete` perguntam antes de apagar. Um item que já não existe dá erro: é só dizer que a
limpeza já estava feita.

---

## O que este roteiro NÃO prova

- **Uma máquina sem o Visual C++** (x64, v14.51 ou mais nova). Esta máquina o tem, no `HKLM`, e vale
  para todas as contas; então o download do `vc_redist` (o único pedido de administrador do Setup,
  a conferência da assinatura da Microsoft e o aviso na página **Pronto para instalar**) **não
  roda** aqui. Precisa de uma VM (o VirtualBox roda no Windows Home) ou do PC de outra pessoa, com
  um Windows limpo.
- **O Windows 10**, e o **Windows 11 com o Controle inteligente de aplicativos ligado** (só dá para
  anotar o estado do que a máquina tem).
- **O download de verdade.** O I1 usa a marca de "veio da internet" posta à mão; a reputação do
  SmartScreen de um arquivo baixado da página de Releases só se vê depois da publicação.
- **Um antivírus de terceiros** além do que esta máquina tem.
- **Um nome de conta com letras fora do Latin-1** (cirílico, grego, polonês como "Łukasz"). O filtro
  de caminhos do `whisper-server.log` troca o caminho em UTF-8 e, para o que cabe no Latin-1 (os
  acentos do português, como o "é" de `José Teste`), também na página de código do Windows. Para um
  nome fora do Latin-1, só a forma em UTF-8 é trocada; se o servidor escrever o nome noutra
  codificação, a linha vaza, e a conta de teste deste roteiro não exercita isso.
- **A atualização de uma versão para outra.** O I10 instala a **mesma** versão por cima, que é o que
  o `.iss` faz de especial (fechar o Agent, trocar o `painel`). Atualizar de uma versão menor para
  uma maior, com o `AppId` igual, precisa de dois Setups e não está aqui.
- **A instalação silenciosa** (`/VERYSILENT`), que o futuro "Procurar atualização" vai usar.
- **O logon com o Agent instalado.** O B9 do roteiro da bandeja o confere com o `bin\`; instalado,
  o caminho do `Run` é outro, e este roteiro só confere o valor, não o logon.
- **Uma pasta de instalação escolhida** (`/DIR=`), nem a recusa dela quando é a pasta de dados
  (tem teste de texto, não de execução).
- **Várias contas no mesmo PC com o programa instalado em cada uma.** Cada conta teria o seu
  programa e os seus dados; a ADR-018 prevê, e não foi visto.
- **O desempenho em um PC mais fraco**, e a subida com antivírus varrendo o arquivo novo: só o
  tempo do I5, desta máquina.

---

## Resultado da execução

Preencha com passou/falhou e o que viu. Para o que falhou, junte o `Setup Log`, o `agent.log` e o
`whisper-server.log`.

Setup testado: `AuroraWhisper-Setup-__________.exe` — SHA-256: `________` — tamanho: `____` MB —
commit do Agent instalado (`vcs.revision`): `________` — conta de teste: `________`.

| Passo | Resultado | Observação |
|---|---|---|
| Conta de teste limpa (estado de antes) | | |
| Controle inteligente de aplicativos: ligado / desligado / avaliação | | |
| Antivírus ativo | | |
| I1 — SmartScreen sem assinatura | | |
| I2 — instalar sem administrador | | |
| I3 — onde ficou o programa e os dados | | |
| I4 — Menu Iniciar, Aplicativos, ícone | | |
| I5 — abrir; `dados=localappdata`; `whisper-server.log` | | |
| I5 — tempo da primeira subida (s) | | |
| I6 — ditar | | |
| I6 — pílula por cima de uma janela topmost comum | | |
| I7 — dicionário vazio criado | | |
| I8 — Aprender, ditar de novo, Desfazer | | |
| I9 — início com o Windows desligado até ligar pela bandeja | | |
| I10 — instalar por cima (mesma versão) | | |
| I11 — desinstalar mantendo os dados | | |
| I12 — reinstalar e ver o histórico | | |
| I13 — desinstalar apagando os dados e as chaves | | |
| I14 — nada ficou | | |
| Conta de teste apagada, cópias do Setup removidas | | |
