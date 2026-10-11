# ADR-018 — O instalador: por usuário, programa e dados em pastas separadas, modelo dentro

Status: Accepted (as quatro decisões e o resto do plano, Mauro, 30-set-2026, sobre o plano da
etapa 4).
A redação desta ADR ainda não passou pela revisão dele.
Decisão posterior do Mauro, 30-set-2026: **o Agent cria um `dicionario.json` vazio na pasta de dados**
quando não há nenhum (opção (a) da seção "O dicionário que não existe"); a pendência virou decisão,
e a seção "Decisões tomadas na implementação (F3 a F5)" diz como ficou.
Data: 2026-09-30
Contexto: etapa 4 do roteiro de 29-set-2026 ("instalador"), depois do aprender com as edições
(ADR-017) e antes da nuvem (etapa 5)
Emenda: `13-implementation-plan.md` (TASK-022 e TASK-023); `09-security-privacy.md` (onde ficam os
dados, o download do runtime pelo instalador, THR-010); o `README.md` (seção Instalação, Privacidade
e Licença); `THIRD_PARTY_NOTICES.md` e `docs/marca/README.md` (a ilustração da Aurora, decisão 2);
e o registro em `00-index.md`. **Não muda o núcleo** (ADR-007). **Cumpre o AC-038** (instala e
remove), que hoje está DIFERIDO em `fase-1-cobertura.md`, quando as fatias F3 a F6 estiverem
prontas; a F1, sozinha, não o cumpre.

## O problema

Hoje o Agent só roda numa pasta montada à mão. O `cmd/asrstage` copia o servidor de ASR, as DLLs e
o modelo para `bin\`, e o Agent assume **tudo ao lado do próprio `.exe`**, para ler e para gravar:
o modelo, o `whisper-server.exe`, o painel, o `historico.db` (com as preferências e o dono fixo), o
`dicionario.json` e os dois logs. Cinco lugares do código repetem `filepath.Dir(os.Executable())`
(`cmd/agent/main.go` e `logarquivo.go`). Isso é cômodo para desenvolver e quebra num instalador.

### O que foi medido (leitura do repositório, de `bin\` e dos binários; nada foi instalado nem executado)

- **Em `C:\Program Files` o Agent sobe e dita, e perde tudo o que grava, sem dizer.** O log não
  abre (o Agent roda sem nenhum); o `whisper-server.log` não abre; o `historico.db` não migra, e
  como o painel sobe **dentro do `else` do histórico**, não há painel, nem API local, nem tela de
  Configurações, nem preferências. O item "Abrir" da bandeja fica desabilitado sem explicação.
- **O layout atual é legível por qualquer conta local.** O `historico.db` em
  `C:\Desenvolvimento\aurora-whisper\bin\` herda a ACL de `C:\`: leitura para `BUILTIN\Usuários`
  (conferido com `icacls`). `%LOCALAPPDATA%` só dá acesso a quem é dono, ao SYSTEM e aos
  Administradores.
- **O runtime do Visual C++ é dependência real.** `whisper-server.exe`, `whisper.dll`, `ggml.dll`,
  `ggml-base.dll` e as `ggml-cpu-*.dll` importam `msvcp140`, `vcruntime140` e `vcruntime140_1`; a
  `ggml-base` e as `ggml-cpu-*` importam também `vcomp140` (tabela de importação dos PE). A máquina
  do Mauro tem o redistribuível v14.51, por isso ele nunca viu o problema. Sem ele o servidor não
  sobe e o Agent encerra com a mensagem de modelo/servidor, apontando para um `whisper-server.log`
  que provavelmente estará vazio (falha do carregador antes do `main`; isso é inferência).
- **O `q4_0` do modelo não existe pronto em lugar nenhum oficial.** No Hugging Face
  (`ggerganov/whisper.cpp`) o `small` só tem f16 (488 MB, `ggml-small.bin`), `q5_1` e `q8_0`.
- **O dicionário que o `asrstage` copia é o do Mauro** (`usuario: "mauro"`, 45 termos), não um
  padrão para terceiros.
- **Não há nada de distribuição:** o `.exe` não tem ícone, manifesto nem versão embutidos; `version`
  é uma `const` (não aceita `-ldflags -X`); o painel só constrói para `../bin/painel`; o CI não tem
  job de release; `internal/update` é biblioteca que nada chama (o `Installer` nem tem
  implementação, e a assinatura é lida e nunca verificada).
- **Tamanhos (medidos em `bin\` e `thirdparty\`):** modelo 145.458.015 bytes; servidor mais 12 DLLs
  10.761.216 bytes; Agent (windowsgui) 19.685.888 bytes; painel 586.204 bytes; total descomprimido
  176.491.323 bytes (168,3 MiB). **Estimativa (est.), não medida:** o Setup com o modelo dentro fica
  perto de 140 MB (xz -6 no modelo dá 128.053.064 bytes; nenhum build real do Inno foi feito).

## Decisão

As quatro primeiras são as respostas do Mauro; as demais são o resto do plano, aceito junto.

1. **O modelo de voz vai dentro do Setup.** Funciona offline, respeita a decisão de 18-set (o
   arquivo ao lado do executável, sem `go:embed`) e o que o README promete (nenhuma conexão sem
   ação de quem usa).
2. **As versões 0.x saem sem assinatura digital**, rotuladas como prévia. **A ilustração da Aurora
   fica liberada:** deixa de ser "todos os direitos reservados" e passa a seguir a licença do
   projeto (MIT). Isso também tira o único componente proprietário do repositório, que era o
   obstáculo da assinatura gratuita (SignPath Foundation), a pedir depois do primeiro release.
3. **O instalador não liga o "Iniciar com o Windows".** Quem liga é a pessoa, pela bandeja, como o
   código já faz (`internal/agent/autostart`, que diz: nunca o instalador por conta própria).
4. **Atualização manual nas 0.x.** A pessoa baixa o Setup novo e instala por cima. Um "Procurar
   atualização", por clique, fica para uma fatia seguinte; o winget, só depois do primeiro release
   estável.
5. **Ferramenta: Inno Setup 6.7, instalação por usuário** (`PrivilegesRequired=lowest`): sem
   administrador, coerente com o que o Agent já usa (`HKCU\...\Run`, o mutex `Local\`).
6. **Duas pastas, com papéis diferentes.** O **programa** (somente leitura em uso) em
   `%LOCALAPPDATA%\Programs\Aurora Whisper\`; os **dados** (o que o Agent grava) em
   `%LOCALAPPDATA%\AuroraWhisper\`. Um pacote novo, `internal/agent/pastas`, é o único lugar que
   decide isso (`Programa()` e `Dados()`).
7. **Modo portátil por um arquivo marcador**, para o `bin\` de desenvolvimento, com uma regra de
   segurança para quem já tem dados ao lado do executável (seção própria abaixo).
8. **Desinstalar pergunta se apaga os dados**; o padrão é **não** apagar.
9. **O runtime do Visual C++ é baixado da Microsoft se faltar**, só então, e com a permissão que o
   Windows pedir. Não embutimos as DLLs.
10. **Um release rascunho no GitHub Actions, disparado por tag.** Publicar é sempre com o Mauro.
11. **Migrar os dados do `bin\` é manual e copia antes** (seção própria abaixo); nenhum código de
    migração.
12. **O Agent cria um `dicionario.json` vazio na pasta de dados, na subida, só se não houver nada
    ali** (Mauro, 30-set-2026; seção "O dicionário que não existe"). Assim o aprender do painel
    funciona no primeiro dia de um Agent instalado, que não vem com dicionário.

## As quatro respostas do Mauro (30-set-2026)

| # | Pergunta | Resposta |
|---|---|---|
| 1 | Modelo embutido ou baixado? | Embutido. |
| 2 | Assinatura? | 0.x sem assinatura; a ilustração da Aurora fica liberada (MIT). |
| 3 | Autostart no instalador? | Não. Quem liga é a pessoa, pela bandeja. |
| 4 | Atualização e winget? | Manual nas 0.x; "Procurar atualização" por clique numa fatia seguinte; winget depois do primeiro release estável. |

As recomendações, resumidas: o embutido funciona offline e não cria a primeira conexão sem ação de
quem usa, e o baixado obrigaria a emendar a política de privacidade (1); sem assinatura o custo é
zero, e liberar a ilustração abre a assinatura gratuita depois (2); a regra do autostart já está
escrita no código (3); o winget pode rejeitar um app com hook de teclado (não verificado) e o
serviço de atualização depende de um backend que não existe (4).

### Aprovado no plano, sem pergunta

Fazem parte do plano aprovado, e não foram perguntas: o Inno Setup por usuário; as duas pastas; o
marcador para o `bin\`; a pergunta dos dados na desinstalação (padrão não); o VC++ baixado se
faltar; o release rascunho por tag; o `-trimpath` e o `-ldflags -X` no build; o pacote **sem
dicionário**; o log sem caminho completo; a migração copiando.

## Onde fica cada coisa

| O quê | Onde | Observação |
|---|---|---|
| Agent, `whisper-server.exe` e as 12 DLLs, modelo, `painel\`, `licencas\` | `%LOCALAPPDATA%\Programs\Aurora Whisper\` | só leitura em uso; cerca de 168 MiB instalado (medido). O Setup troca a pasta na atualização |
| `historico.db` (com preferências e dono fixo), `.db-journal`, as cópias `antes-do-dono` | `%LOCALAPPDATA%\AuroraWhisper\` | a ACL de `%LOCALAPPDATA%` dá acesso só a quem é dono, ao SYSTEM e a Administradores |
| `dicionario.json` (inclusive o aprendido no painel) | `%LOCALAPPDATA%\AuroraWhisper\` | local-first, uma máquina e uma conta; não vai para `%APPDATA%` (Roaming): divergiria da nuvem da etapa 5. **O pacote não o leva; o Agent cria um vazio na subida, se não houver nada ali, para o aprender do painel funcionar desde o primeiro dia (ver "O dicionário que não existe")** |
| `agent.log` (e `.1`), `whisper-server.log` | `%LOCALAPPDATA%\AuroraWhisper\` | o giro de 5 MB continua |
| Chaves de API | Gerenciador de Credenciais, alvo `AuroraWhisper/<provedor>` | **sem mudança** |
| `HKCU\...\Run\AuroraWhisper` | só existe se a pessoa ligar o início automático | **sem mudança**; o desinstalador o remove se apontar para o exe desinstalado |
| Entrada "Aplicativos" do Windows | `HKCU` (o Inno por usuário) | sem administrador |

**O pacote não leva dicionário** (o `testdata/asr-dataset/dicionario.json` é o pessoal do Mauro e
não vai para terceiros). Quem instala sobe **sem** dicionário, e o Agent cria um **vazio** na pasta de
dados (seção abaixo).

### O dicionário que não existe, e o aprender (achado da revisão da F1; **decidido: opção (a)**)

O plano aprovado resolveu "sem dicionário" só para o **resolver**. Não olhou o **aprender** da
etapa 3. A ADR-017, decisão 6, diz: sem `dicionario.json` na subida (ou com ele de outro usuário),
o aprendizado do painel não existe. Salvar uma edição devolve a lista de sugestões vazia (o aviso
"Aprender" nunca aparece) e as rotas de escrita de `/api/aprendizado` respondem `503`
(`novoAprendizado` recebe o usuário vazio e fica sem arquivo). A lista "Aprendido no painel" das
Configurações fica vazia. O Setup não cria o arquivo (decisão do plano) e, até esta decisão,
**nada no Agent o criava**: num Agent instalado, o aprender não funcionava e a pessoa não era
avisada. Isso conflitava com a ADR-017 sem decisão registrada, e por `CLAUDE.md` era ponto de parada.

Opções que foram postas ao Mauro:

- **(a) O Agent cria, na pasta de dados, um `dicionario.json` vazio**, na primeira subida (ou no
  primeiro "Aprender"). Não fere "o Setup nunca cria dados": quem cria é o Agent, numa pasta
  gravável. Custa código e prova por teste (abaixo). Deixa a etapa 3 funcionando para quem instala.
- **(b) Aceitar nas 0.x.** O instalado só aprende se a pessoa criar o arquivo; o README diz isso.
  Custo zero, e o recurso de maior valor da etapa 3 fica morto para quase todo mundo.

**Decisão (Mauro, 30-set-2026): (a).** Como ficou, e o que ficou diferente do que a opção dizia:

1. **Quando: na subida, antes de abrir o dicionário** (`garantirDicionario`, em
   `cmd/agent/dicionario_vazio.go`), e não no primeiro "Aprender". Lazy não funciona sem mexer no
   runtime: o runtime nasce com o `usuario` vazio e o Smart nulo, e `TrocarDicionario` não troca o
   `usuario`, de modo que uma variante aprendida de um arquivo que apareceu depois não agiria até
   reiniciar; e a rota que sugere ao salvar uma edição (`Dicionario`) teria de criar o arquivo numa
   **leitura**. Criado na subida, o resto do Agent vê um dicionário comum: a mesma carga, o mesmo
   `usuario` no runtime e no aprendizado, nenhum caminho novo. É também o momento que **não muda nada
   para quem já tem dicionário**: o arquivo existe e não é tocado.
2. **O `usuario` é `"local"`, e não o dono do painel.** Era a ideia da pendência, mas o dono fixo
   nasce **dentro** do `historico.db`, que só abre depois do dicionário, e a decisão de 29-set
   desligou os dois ("o dicionário volta a ser só dicionário"). O que a busca exige é que o `usuario`
   do arquivo seja o que o runtime recebe (`UsarResolver`) e o que o aprendizado confere
   (`Arquivo.Usuario`, `ErrUsuarioDiferente`): os dois saem do próprio arquivo, na subida. `"local"` é
   o valor que a regra antiga do dono (`donoPelaRegraAntiga`) e o `dictlearn` já usam para "sem
   dicionário", então, se a migração do dono fixo falhar, o dono é o mesmo com ou sem o arquivo
   criado (um teste confere).
3. **Nunca sobrescreve.** Só cria se **nada** existe no caminho (`dictionary.CriarSeFaltar`): um
   dicionário válido, um inválido (o Agent o recusa e o diz, como sempre), um arquivo vazio e uma
   pasta com o nome do arquivo ficam como estão. A garantia não depende de olhar antes de criar: o
   arquivo nasce completo num temporário ao lado e é **ligado** ao nome final (`os.Link`, que falha
   se o destino existe), e não renomeado por cima (o rename do Windows substitui o que está lá, que
   é o que `Gravar` faz e aqui não pode acontecer). Sem link físico no sistema de arquivos (FAT, exFAT,
   alguns compartilhamentos), cai para criar de forma exclusiva (`O_EXCL`) e escrever de uma vez, e
   apaga o arquivo se a escrita falhar, para não deixar um dicionário truncado que travaria a subida.
   Dois chamadores ao mesmo tempo resultam em um arquivo íntegro (teste de concorrência).
4. **No modo portátil também, e só se faltar.** O `bin\` do Mauro tem o dele e não é tocado.
5. **Falhar não impede a subida.** Pasta sem permissão ou disco cheio: WARN (só o nome do arquivo) e o
   Agent segue como seguia sem dicionário, com o aprender indisponível naquela execução.
6. **O que muda para quem não tinha dicionário:** o Smart passa a rodar com um dicionário **sem
   termos** em vez de nenhum. O texto do ditado sai igual (um teste compara os dois, com confiança
   baixa e alternativa do N-best), e a linha "sem DictionaryRepository" deixa de aparecer a cada
   ditado. O vocabulário do ASR continua vazio.
7. **O formato é o de `Semente.Gravar`** (JSON indentado, `"entradas": []`), então o primeiro
   Aprender e o Desfazer que o desfaz tratam o arquivo como qualquer outro, e o Desfazer devolve os
   mesmos bytes do arquivo criado (teste).

**Efeito na migração do `bin\`** (seção própria, passo 2): se o Agent instalado abrir **antes** de a
pessoa copiar o `dicionario.json` do `bin\`, já existe um vazio na pasta de dados, e a cópia o
**substitui** (é o que a pessoa quer; o que ela tinha aprendido nesse meio tempo some). Por isso o
passo 2 manda copiar antes da primeira subida. O aviso do log para um `dicionario.json` ao lado do
executável que não é usado continua valendo.

Provado por teste (`cmd/agent/dicionario_vazio_test.go`, `internal/dictionary/vazio_test.go`): o
vazio é criado válido; nunca sobrescreve (válido, inválido, vazio, de outro usuário, marcas de
conflito do git, pasta); a segunda subida preserva o que foi aprendido; abre como qualquer
dicionário; o texto não muda; **o aprender funciona no primeiro dia, de ponta a ponta, pela API do
painel** (a edição oferece "Aprender", o clique grava, a lista mostra, o Desfazer devolve o arquivo
criado); a ordem em `run()` (garantir antes de abrir) é conferida no código de `main.go`.

## Modo portátil

O ambiente de desenvolvimento continua igual: o `bin\` monta o programa e guarda os dados no mesmo
lugar. **A garantia de não tocar nos dados reais de quem usa o instalado vale só para o `bin\`
montado pelo `asrstage`** (que tem o marcador) e para uma pasta que já tem `historico.db`. Um
`go run ./cmd/agent`, ou um executável compilado (`-o`) para uma pasta sem marcador e sem banco, cai
no modo `localappdata` e usa a pasta de dados **real**: cria `%LOCALAPPDATA%\AuroraWhisper`, anexa
ao `agent.log` do instalado (e o gira, se passar de 5 MB) e, com o modelo ao lado, abre o
`historico.db` real e migra o banco para a frente (as migrations não voltam). Ver o risco 11.

- **O marcador.** Um arquivo chamado `portatil`, sem extensão, **ao lado do executável**. Com ele,
  `Dados()` é a própria pasta do executável. O `cmd/asrstage` o cria em `bin\`, como primeiro
  passo da montagem (assim ele sobra mesmo que a cópia das DLLs falhe por elas estarem em uso).
- **Por que um marcador e não "se existir `historico.db`".** Um `bin\` limpo, sem banco ainda, cairia
  nos dados reais em `%LOCALAPPDATA%`: um build de desenvolvimento gravaria no histórico de quem
  usa o programa instalado.
- **Por que só o marcador não basta (decisão tomada na F1, pede confirmação do Mauro).** Quem já tem
  dados em `bin\` e roda um Agent novo **sem** ter rodado o `asrstage` teria os dados "perdidos" sem
  aviso: o Agent abriria um banco novo em `%LOCALAPPDATA%`, com **outro dono fixo** (o dono mora
  dentro do banco) e o painel vazio, e o histórico real continuaria em `bin\`, parado. O
  `escolherDono` existe justamente porque um banco novo gera outro dono.

**A regra, em ordem:**

1. Há o arquivo `portatil` ao lado do executável: **modo portátil** (`dados=portatil`).
2. Senão, há um `historico.db` ao lado do executável: **modo portátil legado**
   (`dados=portatil-legado`). O Agent usa a pasta do programa para os dados e registra um **WARN**
   a cada subida, sem caminho: há um `historico.db` ao lado do executável e não há o arquivo
   `portatil`; para tornar isso explícito, crie o arquivo (ou rode o `asrstage`).
3. Senão: **dados em `%LOCALAPPDATA%\AuroraWhisper`** (`dados=localappdata`), criada com `MkdirAll`.

**Por que só o `historico.db` como gatilho, e não também o `dicionario.json`.** O `historico.db` só
existe ao lado do executável se um Agent rodou ali: ninguém o cria à mão. Já o `dicionario.json`
pode ser posto ali por uma pessoa (o README antigo mandava fazê-lo), e uma instalação por usuário
que o recebesse cairia no portátil, com os dados dentro da pasta do programa, que o Setup troca na
atualização. O `historico.db` não corre esse risco: o Setup nunca o cria. Custo aceito: um `bin\`
montado pelo `asrstage` antigo, que nunca rodou, tem só o dicionário, e cai em `%LOCALAPPDATA%`;
rodar o `asrstage` novo uma vez resolve, e ali não há dado de quem usa para perder.

**O Agent nunca cria o marcador sozinho.** O programa é somente leitura em uso (em `Program Files`
nem poderia), e um marcador criado pelo Agent numa pasta instalada desviaria os dados para dentro
da pasta que o Setup troca. Quem cria é quem monta (o `asrstage`) ou a pessoa.

**Restrição que isso põe nas fatias seguintes (F3 e F4).** O pacote do instalador **não leva** o
marcador `portatil` nem um `historico.db`, e o `asrstage` só cria o marcador na montagem de
desenvolvimento (não na do pacote). Um teste de pacote confere os dois.

## Migração dos dados do `bin\` (manual e segura, copiando antes)

Sem código novo; nada é movido nem apagado. O dono fixo e as preferências estão **dentro** do
`historico.db`, então basta copiar o arquivo: não se reimporta nem se recria nada. As chaves de API
ficam no Gerenciador de Credenciais e não dependem de pasta.

1. Na bandeja do `bin\`, desligar "Iniciar com o Windows", se estiver ligado. Fechar o Agent pelo
   menu, em **Sair**.
2. **Antes** de abrir o Agent instalado pela primeira vez, copiar `bin\historico.db` e
   `bin\dicionario.json` para `%LOCALAPPDATA%\AuroraWhisper\`. O roteiro da F6 terá o comando
   pronto, e ele **para** se o destino já tiver um banco. (O banco usa o journal padrão, sem
   `-wal`, então com o Agent parado é uma cópia simples.)
3. Instalar, abrir e conferir no painel que o histórico apareceu. Depois religar "Iniciar com o
   Windows" pela bandeja do Agent instalado.
4. O `bin\` fica **intacto** e vira o ambiente de desenvolvimento, com banco próprio (as migrations
   só vão para a frente: não rodar o Agent antigo sobre o banco já aberto pelo novo). Os dois
   Agents não rodam juntos: disputam o mesmo mutex (`Local\AuroraWhisperAgent`).

O autostart: o valor `Run\AuroraWhisper` guarda o caminho absoluto do executável no momento do
clique (`autostart_windows.go`). Se o executável mudar de lugar, a bandeja mostra "desligado" e o
Windows tenta o caminho antigo no logon. Por isso o passo 1 (desligar no antigo) e o passo 3
(religar no instalado). O instalador **não** cria nem reaponta o valor (decisão 3).

## Ferramenta, build e pacote

**Inno Setup 6.7, por usuário.** Já vem no runner `windows-latest` (6.7.1, imagem de 22-set-2026),
a licença é livre, o TASK-022 já o sugere, e o `[Code]` dele resolve as três partes específicas:
perguntar sobre os dados, checar o VC++ e fechar o Agent. Aceita assinatura quando houver.

**Descartados**

- **MSIX.** O pacote é somente leitura e o registro é virtualizado: o `Run` do autostart gravado pelo
  Agent não chegaria ao Windows, e o Agent grava ao lado do executável. A desinstalação apaga os
  dados virtualizados sem perguntar.
- **WiX/MSI.** Mais complexo; a pergunta dos dados na desinstalação é difícil. Só se alguém exigir
  MSI (política de grupo corporativa).
- **NSIS.** Equivalente ao Inno, em nível mais baixo, e não vem no runner.
- **Velopack.** Não pergunta sobre os dados na desinstalação e não tem SDK para Go (aparece como
  "planejado").
- **O Agent baixando as peças** (modelo ou servidor, na primeira execução). Quebraria o "nenhuma
  rede sem ação sua" e exigiria código de retomada, arquivo temporário e interface de progresso num
  app de bandeja que deve ser discreto.
- **Embutir as DLLs do VC++.** A licença de redistribuição é amarrada a quem tem o Visual Studio, e
  elas não recebem as correções do Windows Update.

**Fechar o Agent para instalar ou atualizar.** `taskkill` sem `/F`, que o Agent já trata como
"Sair" (o overlay recebe o `WM_CLOSE`), e esperar o mutex `Local\AuroraWhisperAgent` sumir. O Job
Object derruba o `whisper-server` junto e libera as DLLs. Não usamos o Restart Manager do Inno: o
Agent não trata `WM_QUERYENDSESSION`, e **não foi verificado** como ele reage a isso.

**Runtime do VC++.** O instalador lê `HKLM\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64`;
se faltar ou for antigo, baixa `aka.ms/vc14/vc_redist.x64.exe`, o que exige permissão (UAC) uma vez.
É a **única** conexão de rede do instalador, e só acontece se faltar. **A versão mínima é 14.51.**
Medida: o cabeçalho PE do `whisper-server.exe` e das DLLs do zip da `b5130` (hash `f9ec6c52…`) diz
que o linker é o 14.51, e a regra da Microsoft é que o redistribuível seja igual ou mais novo que o
toolset que compilou o programa; a máquina do Mauro tem a v14.51.36247. **Não foi testado** rodar o
servidor com um redistribuível mais antigo, e, na prática, quase toda máquina que não tem o 14.51
pedirá o download. O arquivo baixado só é executado se a assinatura for válida e de "Microsoft
Corporation" (`Get-AuthenticodeSignature`), porque ele roda com permissão de administrador e o
endereço `aka.ms` aponta sempre para a versão mais nova (não há hash fixo possível).

**Build (F3).** Um job de release no GitHub Actions, por tag `v*`; o mesmo script roda local
(`scripts/montar-instalador.ps1`, depois `.github/workflows/release.yml`; ver "Decisões tomadas na
implementação (F3 a F5)").

- Agent: `go build -trimpath -ldflags "-H=windowsgui -X main.version=<tag>"`. `version` passa de
  `const` a `var`. O `-trimpath` tira de dentro do binário os caminhos da máquina de build.
- Painel: `pnpm build` com `--outDir`, porque hoje o destino está preso a `../bin/painel`.
- whisper.cpp: o zip da `b5130` é baixado com SHA-256 fixo
  (`f9ec6c52a2e949b62ab51fa21d0d497958f9e41c3010c157c4e42932d5316f3c`) e **espelhado** no release do
  modelo, porque é um prerelease e pode sumir do upstream.
- Montagem: o `asrstage` ganha `-destino` e `-sem-dicionario`, e reaproveita a lista das 12 DLLs e a
  checagem do modelo.

**O que entra no pacote:** o Agent, o painel, o `whisper-server.exe` com as 12 DLLs, o modelo, o
ícone (feito a partir do símbolo, que é MIT; fica dentro do executável do Agent e no Setup, e não
como arquivo solto: ver "Decisões tomadas na implementação (o ícone)") e a pasta `licencas\`: o `LICENSE` do projeto e
o `THIRD_PARTY_NOTICES.md`; a OFL da fonte do overlay; os avisos BSD do Go, do `x/*` e do
`modernc`; a MIT do whisper.cpp; e a licença dos pesos da OpenAI, com os dois textos (MIT, no
README da OpenAI, e Apache-2.0, no card do Hugging Face: ver o risco 6).

**O que fica fora:** o dicionário pessoal; o `parakeet`, o `llama`, o SDL e o `whisper-cli` do zip
do whisper.cpp; o banco e os logs; o marcador `portatil`.

## Modelo embutido e de onde ele vem

- **O arquivo:** `ggml-small-q4_0.bin`, 145.458.015 bytes, SHA-256
  `f6099c17a4305c6d173ef90ab6f15150dcbb85332b7831313bf36a378d7c5a85` (o que `SmallQ4_0` em
  `internal/asr/model` confere a cada subida).
- **Procedência.** É o `models/quant/small-q4_0.bin` do Mauro, **quantizado localmente** a partir do
  f16 oficial do Hugging Face (`ggml-small.bin`, 487.601.967 bytes, SHA-256 `1be3a9b2…987b`), com o
  `whisper-quantize.exe` da build `b5130` do whisper.cpp (commit `927cfce3…`), comando
  `whisper-quantize ggml-small.bin ggml-small-q4_0.bin q4_0`. A re-quantização do f16 nesta máquina
  reproduziu os mesmos bytes e o mesmo hash (**medido**, 1,8 s; o `q8_0` local também é idêntico ao
  publicado no HF). **Não foi provado** que o `q4_0` bate noutra máquina ou com outro compilador.
  Se não bater, o `asrstage` e o `Resolve` acusam na hora.
- **Hospedagem.** Um release fixo do projeto, por exemplo `modelo-small-q4_0-v1`, com o arquivo, o
  `.sha256` e uma nota de procedência (commit do HF, tag `b5130` e o comando). O CI baixa de lá e
  confere o hash; o `Resolve` confere de novo a cada subida. **Subir esse arquivo é publicar**: o
  Mauro faz, ou autoriza.
- **Opcional:** um job manual que refaz a quantização a partir do HF e compara o hash, para servir
  de prova a terceiros (baixa 488 MB).
- **O que o hash protege:** arquivo corrompido, CDN, MITM. **Não** protege contra quem controla o
  repositório, que altera o binário e o hash juntos.
- **Licença.** Os pesos são da OpenAI; o README do `openai/whisper` diz MIT, o card do HF
  `openai/whisper-small` diz Apache-2.0. As duas são permissivas e permitem redistribuir derivado
  quantizado; a MIT pede o aviso, a Apache pede o texto e a indicação da alteração. O pacote leva os
  dois textos, e o `THIRD_PARTY_NOTICES.md` passa a dizer que o `q4_0` é distribuído pelo projeto
  (hoje diz que é gerado por quem monta).

## Assinatura e SmartScreen

- **Sem assinar (0.x, custo zero).** A pessoa vê "O Windows protegeu o computador", depois "Mais
  informações" e "Executar assim mesmo". Cada versão nova recomeça do zero, porque a reputação não
  se acumula sem assinatura estável.
- **Windows 11 com Smart App Control** (instalação limpa): pode bloquear sem opção de seguir. Vale
  também para o `whisper-server.exe` e as DLLs do upstream, que não são assinados. **Não verificado
  na prática.**
- **Antivírus:** pode dar falso positivo por causa do hook de teclado (`WH_KEYBOARD_LL`) e do
  `SendInput` (**não verificado**). Mitigação gratuita: enviar ao portal de falso positivo da
  Microsoft depois de cada release.
- **Depois do primeiro release:** pedir o SignPath Foundation (grátis para código aberto; exige
  licença OSI sem componente proprietário, um release anterior, MFA e aprovação manual de cada
  assinatura). A decisão 2 tira o obstáculo da ilustração.
- **Alternativas pagas, descartadas por ora:** certificado OV (US$ 150 a 300 por ano, faixa citada
  pela Microsoft, mais um HSM em nuvem para o CI, custo **não verificado**); o Azure Artifact
  Signing (cerca de US$ 9,99 por mês) hoje não atende quem está no Brasil; o EV já não dá vantagem
  no SmartScreen.
- **THR-010** (atualização maliciosa): enquanto não houver assinatura, é coberta **só por hash**
  (SHA-256 do Setup), que protege contra arquivo corrompido e não contra quem controla o release.

## Atualização

- **Agora, manual.** A pessoa baixa o Setup novo e instala por cima: ele fecha o Agent, troca a
  pasta do programa e reabre. Os dados em `%LOCALAPPDATA%\AuroraWhisper` não são tocados (o Setup
  nunca cria o marcador `portatil` nem um `historico.db`, e por isso nenhum dado de quem usa fica na
  pasta que ele troca).
- **O que `internal/update` já tem:** consulta, comparação de versão sem downgrade, e download com
  SHA-256 e Content-Length. **O que falta:** nada no Agent o usa; o `Installer` não tem
  implementação; a assinatura é lida e nunca verificada; o endpoint `/updates/windows/latest`
  depende de um backend que não existe.
- **Numa fatia seguinte:** um item "Procurar atualização" na bandeja, que só consulta quando a
  pessoa clica; um `UpdateProvider` novo que lê o GitHub Releases; a pessoa confirma, o Agent baixa,
  confere o hash e roda o Setup em silêncio (`/VERYSILENT /CURRENTUSER`).
- **Conflito com a spec, registrado aqui.** O TASK-023 manda usar o `/updates/windows/latest` do
  backend. Enquanto o backend não existir, a origem é o GitHub Releases. Por `CLAUDE.md`, conflito
  entre documentos é ponto de parada; por `00-index.md` ("Fonte da verdade"), a decisão humana mais
  recente vale, desde que registrada: esta ADR é o registro, e a fatia de atualização confere com
  o Mauro antes de codar.
- **Custo do modelo embutido:** cada atualização baixa cerca de 140 MB (est.). Se isso incomodar,
  dá para gerar também um Setup "só app" (cerca de 12 MB, est.), que exige o modelo já instalado.

## Alternativas descartadas (as que não cabem nas seções acima)

- **Instalar em `Program Files` (por máquina).** Pede administrador para instalar e para cada
  atualização. Os dados iriam para `%LOCALAPPDATA%\AuroraWhisper` do mesmo jeito. Se o Mauro
  preferir, é a mudança de um parâmetro no `.iss`; a F1 já serve aos dois layouts.
- **Modelo baixado pelo instalador** (Setup de cerca de 12 MB, com `DownloadTemporaryFile` e
  SHA-256). Funciona, mas é a primeira conexão sem ação de quem usa e obriga a emendar a política de
  privacidade. Fica como alternativa se o tamanho do Setup incomodar.
- **Dados no `%APPDATA%` (Roaming).** O Agent é local-first, de uma máquina e uma conta; um
  dicionário em Roaming divergiria da nuvem da etapa 5.
- **Migrar os dados por código** (um passo do instalador, ou "importar dados do bin antigo" no
  Agent). Seria mais superfície para um problema de uma pessoa e uma vez; a cópia manual, com o
  comando pronto que para se o destino já tem banco, resolve.
- **Só o marcador, sem a regra do `historico.db`.** É o que o plano dizia; descartado na F1 pelo
  motivo da seção "Modo portátil".

## Consequências

**Positivas:**
- o Agent passa a funcionar instalado em pasta só de leitura; o painel, o histórico e as
  preferências deixam de depender da pasta do programa ser gravável;
- os dados de quem usa ficam numa pasta que só a conta dele e os administradores leem;
- o log deixa de gravar o caminho completo (leva o nome de usuário do Windows; quem cola o log num
  issue o expunha);
- o `bin\` montado pelo `asrstage` continua igual e nunca mexe nos dados reais de quem usa o
  instalado (só ele: um Agent de desenvolvimento fora dali usa os dados reais; ver o risco 11);
- nenhuma dependência nova de produção (o Inno e o gerador de `.ico` são ferramentas de build).

**Negativas:**
- **o Setup é grande** (cerca de 140 MB, est.), e cada atualização manual o baixa inteiro;
- **sem assinatura**, o SmartScreen avisa a cada versão nova, e o Smart App Control pode bloquear;
- **o VC++ pode exigir permissão (UAC)** uma vez, o que quebra o "sem administrador" numa máquina
  sem o runtime;
- **dois Agents não rodam juntos** (o mutex é um só): o instalado e o do `bin\` precisam de um
  fechado para o outro abrir;
- **o portátil legado depende de uma heurística** (o `historico.db` ao lado do executável). Um
  `historico.db` jogado à mão numa pasta instalada desviaria o Agent para dentro da pasta que o
  Setup troca; o WARN a cada subida é o que avisa;
- **o `whisper-server.log` trazia caminhos em duas linhas**, porque são impressas pelo
  whisper.cpp, não por nós: a do modelo e a da biblioteca do processador (`load_backend: loaded CPU
  backend from <pasta do executável>\ggml-cpu-*.dll`). Instalado, esse caminho é
  `C:\Users\<nome>\AppData\Local\Programs\...`, e o aviso de erro do ASR manda a pessoa abrir esse
  arquivo (confirmado em dado real: as linhas 1 e 2 do `whisper-server.log` do `bin\` trazem o
  caminho absoluto da biblioteca e do modelo). A F2 **foi feita** ("Decisões tomadas na
  implementação (F2)") em **duas medidas**: o modelo pelo nome relativo, com `cmd.Dir` na pasta do
  programa (`-m ggml-small-q4_0.bin`; tira o caminho da linha do modelo, e **só dela**), e a saída do
  servidor pelo filtro de caminhos do `agent.log` (tira a da biblioteca, que `cmd.Dir` não muda). A
  primeira versão da F2 só fez a primeira, e dizia que o log deixava de levar o perfil: a revisão de
  01-out-2026 achou a segunda linha. **Falta ler o log do servidor de verdade, inteiro**; era
  **pré-requisito de qualquer Setup distribuído** (F5);
- **o Agent instalado cria um `dicionario.json` vazio** na pasta de dados, na primeira subida (seção
  "O dicionário que não existe"): é um arquivo que a pessoa não pediu, mas que ela pode abrir e
  auditar, e é o que faz o aprender funcionar. Quem copia o dicionário do `bin\` depois da primeira
  subida o substitui;
- **um Agent de desenvolvimento fora do `bin\` montado usa os dados reais** de quem usa o instalado
  (risco 11).

## Riscos, e o que não foi verificado

1. **Nada foi instalado, assinado nem medido em máquina limpa.** O tamanho do Setup (cerca de
   140 MB) é estimativa por `xz`, não um build do Inno.
2. **O VC++.** Se faltar, o instalador o baixa e pede permissão. A versão mínima não foi
   descoberta. Um redistribuível antigo de outro programa pode dar "ponto de entrada não
   encontrado" (relato da comunidade, não documento da Microsoft).
3. **O caminho com acento ou fora do cp1252.** O modelo e as DLLs passam por caminho absoluto, e
   `%LOCALAPPDATA%\Programs` leva o nome do perfil. **Não verificado** se o `whisper-server` carrega
   o modelo nesse caso; a busca na web foi inconclusiva. A F2 (feita) reduz o risco: o modelo vai
   pelo nome do arquivo e o servidor roda na pasta do programa, então o caminho com acento deixa
   de passar pelo argumento do servidor (as DLLs ainda são achadas pela pasta do executável, que
   pode ter acento). A F6 testa com um perfil com acento.
4. **Smart App Control, antivírus e SmartScreen** (seção própria): nada disso foi visto na prática.
5. **winget.** A validação pode rejeitar um app com hook global de teclado e injeção de entrada
   (`Binary-Validation-Error`); **não verificado**. Por isso só depois do primeiro release estável.
6. **A licença dos pesos tem dois textos** (MIT e Apache-2.0). O pacote leva os dois; se um
   jurista quiser só um, é decisão do Mauro.
7. **O desempenho em máquina mais fraca.** O p50 de 565 ms foi medido numa máquina de 20 threads
   lógicas; em um PC mais fraco será pior, e o README deve dizer isso. O ajuste de threads é
   extrapolação, e o requisito mínimo de CPU **não foi medido**. O ggml não exige AVX (leitura do
   código do upstream; não executado em CPU sem AVX).
8. **O hash do modelo a cada subida** lê 139 MB. Com o disco frio ou um antivírus varrendo o
   arquivo novo, a primeira subida pode levar vários segundos (est.); medir numa máquina limpa.
9. **O `q4_0` noutra máquina** (seção do modelo): não foi provado que bate.
10. **A regra do portátil legado** foi uma decisão minha na F1, sobre o que o plano deixou em
    aberto; pede a confirmação do Mauro.
11. **Um Agent de desenvolvimento fora do `bin\` montado usa os dados reais.** O modo portátil só
    liga com o marcador ou com um `historico.db` ao lado do executável. Um `go run ./cmd/agent`, ou
    um `-o <pasta sem marcador>` (por exemplo, o `bin\` de outro worktree), cai em
    `%LOCALAPPDATA%\AuroraWhisper`: cria a pasta, anexa e gira o `agent.log` do instalado e, com o
    modelo ao lado, abre o `historico.db` real e migra o banco para a frente. Quem desenvolve na
    máquina onde instala o programa corre isso. **A avaliar na F3, não decidido:** um build sem
    `-X main.version` (o de desenvolvimento) recusar o modo `localappdata` sem o marcador, ou exigir
    um opt-in explícito.
12. **O aprender no Agent instalado** (seção "O dicionário que não existe"): resolvido pela decisão
    de 30-set-2026, o Agent cria o dicionário vazio. Resta o que nenhum teste prova: que, num Windows
    de verdade, o arquivo nasce na pasta de dados e o painel o usa (roteiro manual, F6).

### Pontos de parada que continuam abertos (`CLAUDE.md`: parar e perguntar)

- **`PRODUCT.md` exige revisão formal de LGPD antes de qualquer distribuição para terceiros.** Sem
  ela, o Setup é gerado, mas **não publicado**.
- **Publicar qualquer release ou asset** (o modelo, o Setup) é só com o Mauro.
- ~~**O aprender no Agent instalado**~~ (ADR-017, decisão 6 × o pacote sem dicionário): **decidido**
  em 30-set-2026, opção (a), e implementado (seção "O dicionário que não existe").
- **TASK-023 × GitHub Releases** (seção "Atualização"): registrado acima; confirmar na fatia da
  atualização.
- **O roteiro de teste do instalador** roda em conta local nova e numa máquina sem VC++ (uma VM):
  nada disso roda na máquina de desenvolvimento do Mauro, nem o CI abre o Agent (por causa do hook).

## Testes previstos

Nenhum teste roda instalador, grava no registro, mexe em `%LOCALAPPDATA%` de verdade, injeta entrada,
cria janela, abre o microfone ou toca na área de transferência. Os de caminho usam **pastas
temporárias** e um ambiente injetado (nunca o `%LOCALAPPDATA%` real).

- **F1, `pastas` e o Agent** (`internal/agent/pastas`, `cmd/agent`, `cmd/asrstage`): por tabela, com
  e sem marcador, com o `historico.db` legado, `%LOCALAPPDATA%` vazio ou relativo, caminho com
  acento e com espaço, `MkdirAll` (inclusive falhando); cada consumidor usa `Dados()` para dados e
  `Programa()` para programa; o `asrstage` cria o marcador (em pasta temporária) e o `pastas` o
  reconhece; o log não traz o caminho completo. **Mutação** numa cópia isolada (`git archive`), com o
  diff conferido: marcador ignorado; dado gravado em `Programa()`; `asrstage` sem marcador; a regra
  do `historico.db` legado removida.
- **F2:** o servidor roda com `cmd.Dir` na pasta do programa e recebe o modelo pelo nome relativo:
  teste puro da montagem dos argumentos.
- **F3:** todas as licenças listadas existem no repositório (ou o script as obtém: GOROOT, cache de
  módulos, download); o pacote não leva dicionário, nem o marcador `portatil`, nem `historico.db`
  (o `asrstage` recusa uma pasta de pacote que tenha isso, e o script confere o staging).
- **F4:** as DLLs do `.iss` são as mesmas do `asrstage`; o `.iss` não cita dicionário, marcador nem
  `historico.db`. Compilar com o `ISCC` não executa nada. Um teste Go (`cmd/asrstage`,
  `instalador_test.go`) confere isso e cada nome que o `.iss` repete do Go (o mutex, as pastas, as
  chaves, o modelo, o `Run` do autostart).
- **F5:** um teste de instalação só no runner descartável do GitHub: instala em silêncio, confere
  arquivos e hashes, desinstala e confere que os dados ficaram. Nunca abre o Agent; nunca roda na
  máquina do Mauro.

**Manual, do Mauro, na F1** (o `bin\` dele continua igual):

1. Fechar o Agent (**Sair** na bandeja). Com o Agent aberto, o `.exe` e as DLLs ficam travados.
2. Rodar o `asrstage` uma vez, para criar o marcador `bin\portatil` (ou criar o arquivo vazio à mão):

   ```powershell
   go -C C:\Desenvolvimento\aurora-whisper run ./cmd/asrstage
   ```

3. Recompilar o Agent e abrir:

   ```powershell
   go -C C:\Desenvolvimento\aurora-whisper build -ldflags "-H=windowsgui" -o C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe ./cmd/agent
   ```

4. Conferir: o painel mostra o histórico de sempre; depois de um ditado, `bin\historico.db` mudou de
   data; a pasta `%LOCALAPPDATA%\AuroraWhisper` **não** foi criada por este Agent. Quanto ao
   `bin\agent.log`: ele é **anexado** (só gira aos 5 MB) e o do Mauro tem linhas antigas, de antes
   da F1, com `caminho=`, `pasta=` ou `procurado_em=`. Por isso se confere só o que veio **depois da
   última** linha `pasta de dados resolvida`, que é a desta subida. No PowerShell:

   ```powershell
   $log = 'C:\Desenvolvimento\aurora-whisper\bin\agent.log'
   $i = (Select-String -Path $log -Encoding UTF8 -SimpleMatch 'pasta de dados resolvida' | Select-Object -Last 1).LineNumber
   $desta = Get-Content $log -Encoding UTF8 | Select-Object -Skip ($i - 1)
   $desta | Select-Object -First 1
   $desta | Select-String -Pattern '[A-Za-z]:\\'
   ```

   A primeira saída tem de dizer `dados=portatil`; a segunda **não imprime nada** (nenhuma linha
   desta subida traz um caminho com unidade).
5. A regra do legado: **saia pela bandeja (Sair)**, renomeie `bin\portatil` para `portatil.off` e
   abra o Agent (o modo é decidido na subida; com o Agent do passo 4 ainda aberto, aparece "já está
   rodando" e nada muda). O histórico continua lá, e o `agent.log` traz o WARN com
   `portatil-legado`. Depois **saia pela bandeja de novo**, volte o nome do arquivo e reabra.
6. **Opcional** (simula a instalação sem tocar nos dados reais): numa janela nova do PowerShell,
   copiar `bin\` sem o banco, o dicionário e o marcador, apontar `LOCALAPPDATA` para uma pasta de
   teste só nessa janela, e abrir a cópia (com o Agent do `bin\` fechado; não mexer no "Iniciar
   com o Windows" da cópia):

   ```powershell
   robocopy C:\Desenvolvimento\aurora-whisper\bin C:\Temp\aw-copia /E /XF historico.db* dicionario.json portatil agent.log* whisper-server.log
   $env:LOCALAPPDATA = 'C:\Temp\aw-local'
   & C:\Temp\aw-copia\aurora-whisper-agent.exe
   ```

   Esperado: `C:\Temp\aw-local\AuroraWhisper` com `historico.db` e `agent.log` (a linha diz
   `dados=localappdata`), e nada novo em `bin\`. Para desfazer: sair pela bandeja, fechar a janela do
   PowerShell e apagar `C:\Temp\aw-copia` e `C:\Temp\aw-local` se quiser.

**Resultado do roteiro manual da F1 (Mauro, 30-set e 01-out-2026).** Feito: o **marcador
`bin\portatil` criado** (passo 2, à mão, com `New-Item ... -ItemType File`, às 00:33 de 01-out-2026;
arquivo vazio, 0 bytes). **Visto, mas não como o roteiro o escreve:** a regra do **modo portátil
legado**. O Agent que ele estava rodando tinha `StartTime` de 00:27:41, **antes** de o marcador
existir, e o `agent.log` registra nessa hora o `INFO ... dados=portatil-legado` e o WARN
`portatil-legado` (linhas 495 e 496). É a mesma regra do passo 5, mas **o passo 5 como escrito**
(sair pela bandeja, renomear o marcador para `portatil.off`, reabrir, depois dos passos 3 e 4) **não
foi feito**: o marcador só foi criado depois dessa subida. **Não relatado**, e portanto sem
resultado: os passos 3 e 4 (recompilar, `dados=portatil`, nenhuma linha desta subida com caminho de
unidade) e o 6 (a simulação da instalação com `LOCALAPPDATA` apontado para outra pasta). (A primeira
versão deste parágrafo chamava a subida das 00:27:41 de "passo 5" e a dava como OK; a revisão de
01-out-2026 apontou que o relato do Mauro não dizia isso.)

## Fatias

Toda fatia roda `go test ./...`, `go vet ./...` e termina com **commit local**, sem `git push`.
Nenhum teste roda instalador, grava no registro ou mexe em pastas reais.

| Fatia | O que entrega | Estado |
|---|---|---|
| F0 | esta ADR e as emendas (só documentação) | pronta |
| F1 | o pacote `pastas`; a troca dos lugares que repetiam `filepath.Dir(os.Executable())`; o marcador no `asrstage`; o log sem caminho completo; os textos sem `C:\Desenvolvimento\...` | **pronta** (decisões em "Decisões da F1"); o roteiro manual do Mauro foi feito **em parte**: o passo 2 (marcador criado) e, **antes** dele, a regra do legado vista na subida de 00:27:41 de 01-out-2026; o passo 5 como escrito, e os passos 3, 4 e 6, não foram feitos nem relatados |
| F2 | o `whisper-server` roda com `cmd.Dir` na pasta do programa e recebe o modelo pelo nome relativo (reduz o risco de caminho com acento; teste puro da montagem dos argumentos), **e a saída dele passa pelo filtro de caminhos** (`pastas.AnonimizarLinhas`). **Pré-requisito da F5:** sem isso o `whisper-server.log` instalado traz o nome do usuário em duas linhas, a do modelo e a da biblioteca do processador | **pronta** (`local.Config.Dir`, `configDoServidor`, `abrirLogDoServidor`; "Decisões tomadas na implementação (F2)" e a correção da revisão); falta ler o log do servidor de verdade, **inteiro** (roteiro manual, I5) |
| Dicionário vazio | o Agent cria o `dicionario.json` vazio na subida, só se faltar (decisão de 30-set-2026; seção "O dicionário que não existe") | **pronta** (`dictionary.CriarSeFaltar`, `garantirDicionario`) |
| F3 | build reproduzível: `version` como `var`, `-ldflags` e `-trimpath`, `--outDir` do painel, `asrstage -destino -sem-dicionario`, pasta `licencas\`, o ícone (`cmd/gerarico`) e o script que monta tudo (`scripts/montar-instalador.ps1`). **Falta:** avaliar o item de desenvolvimento do risco 11 | **quase pronta** (ver "Decisões tomadas na implementação (F3 a F5)" e "(o ícone)"); o script **não rodou inteiro** |
| F4 | `installer/aurora-whisper.iss` (o caminho do plano era `deploy/windows/`): por usuário, atalho no Menu Iniciar, "Abrir agora", fecha o Agent com `taskkill` e espera o mutex, checa o VC++, o desinstalador pergunta sobre os dados (apaga a pasta e as chaves `AuroraWhisper/*` se a pessoa confirmar) e sempre remove o `Run` e a marca em `StartupApproved` se apontarem para o exe desinstalado | **escrita e compilada**: o Mauro compilou o Setup `0.1.0-rc.1` com o Inno Setup 6.7.3 nesta máquina, sem erro (135 MB); o Setup **nunca foi executado** |
| F5 | CI de release (`.github/workflows/release.yml`: tag, Setup, release rascunho) e o teste rápido de instalação no runner descartável; **depende da F2** | o workflow está **escrito e não rodou**, e tem um **portão fechado** (`F2_PRONTA: 'false'`) que o faz falhar de propósito até alguém o abrir; o teste de instalação no runner **não foi feito**; a F2 **está pronta**, e abrir o portão continua sendo decisão do Mauro, depois de conferir o servidor de verdade |
| F6 | `docs/teste-manual-instalador.md` e a seção Instalação do README, com o roteiro do Mauro: conta local nova, padrão, com acento no nome (instalar, ditar, abrir o painel, instalar por cima, desinstalar mantendo os dados e reinstalar, desinstalar apagando); máquina sem VC++ (uma VM); SmartScreen e Smart App Control; o tempo da primeira subida com disco frio | a seção **Instalar** do README está feita; o **roteiro manual** está escrito (`docs/teste-manual-instalador.md`, para uma conta de teste do Windows) e **ninguém o executou** |
| Fora desta leva | atualização por clique; winget | |

Uma sessão por fatia é estimativa **(est.)**, não medida.

## Decisões tomadas na implementação (F1)

1. **O pacote `internal/agent/pastas`.** `Resolver(Ambiente)` devolve `Pastas`, com `Programa()`,
   `Dados()` e `Modo()`; `Anonimizar` cuida do log (item 6). **Não há valor padrão:** tudo o que o
   pacote pede ao sistema (`os.Executable`, `os.UserCacheDir`) entra por `Ambiente`, e uma função
   faltando é erro. Só `pastas.Sistema()` liga o real, e só `main` do Agent a chama; nenhum teste
   toca no `%LOCALAPPDATA%` de verdade. A pasta local só é pedida no modo `localappdata`: um
   executável portátil roda sem ela.
2. **Sem `%LOCALAPPDATA%` (vazio, relativo ou com erro) não há pasta de dados, e o Agent não
   sobe.** Não cai para a pasta do programa: num `Program Files` ela nem grava, e numa pasta gravável
   dividiria os dados em dois lugares quando o `%LOCALAPPDATA%` voltasse. Antes o Agent também não
   subia sem o caminho do executável (o ASR o usava). O aviso diz o que faltou. Num Windows de uso
   normal isso não acontece (**não verificado** em ambiente sem o perfil carregado).
3. **As pastas se resolvem em `main`, depois da instância única e antes do log.** O log mora na
   pasta de dados, e resolver cria a pasta (`MkdirAll`); o segundo Agent, que sai antes, não cria
   nada (o mutex segue exatamente como estava).
4. **`caminhosDe` é o único lugar que junta pasta e nome de arquivo** (`cmd/agent/caminhos.go`).
   Vão para `Dados()`: `historico.db` (e as cópias `antes-do-dono`, que se derivam do caminho do
   banco), `dicionario.json` (inclusive o aprendido no painel), `agent.log` e `whisper-server.log`.
   Ficam em `Programa()`: o modelo, o `whisper-server.exe` (e as DLLs ao lado), o `painel\`.
   Os consumidores recebem o caminho pronto (`abrirHistoricoEm`, `abrirDicionarioEm`,
   `novoAprendizado`, `subirPainel`, `abrirASRLocal`); as funções que só somavam o diretório do
   executável (`abrirHistorico`, `abrirDicionario`, `caminhoDoDicionario`) deixaram de existir.
5. **O log não grava o caminho completo, em duas camadas.** (a) As linhas que o Agent escreve:
   "banco pronto", "painel no ar", "dicionário nenhum encontrado", "autostart ligado" e a da cópia do
   dono fixo dizem só o nome do arquivo (ou nada); a subida registra `dados=localappdata`,
   `dados=portatil` ou `dados=portatil-legado`, este com um WARN. (b) O `agent.log` passa por
   `pastas.Anonimizar`, que troca por `<dados>`, `<programa>` e `<perfil>` o caminho que sobrar
   dentro de uma mensagem de erro do sistema (`open C:\Users\...: Acesso negado`), nas formas com
   barra invertida, com a barra dobrada do `slog` entre aspas, e com barra normal. **O que isso não
   cobre:** o `whisper-server.log` (as linhas com caminho são do whisper.cpp; a F2 o trata à parte:
   o modelo pelo nome e a saída do servidor pelo mesmo filtro, `pastas.AnonimizarLinhas`), o pânico
   do runtime (`debug.SetCrashOutput` escreve direto no arquivo; com `-trimpath`, a F3, os caminhos
   da máquina de build saem do binário), a tela do terminal quando há console (crua,
   porque é de quem desenvolve) e uma diferença só de caixa de letras no caminho (a troca é
   exata). Uma trava no código-fonte (`TestLogNaoTemChaveDeCaminho`) impede que uma chave `caminho`,
   `pasta`, `exe`... volte a entrar numa linha de log.
6. **Os textos.** O aviso de erro fatal diz que o `agent.log` fica "na pasta de dados do programa
   (`%LOCALAPPDATA%\AuroraWhisper`; no modo portátil, ao lado do executável)", e não mais "na pasta
   do programa". A página "o painel não foi construído" diz `pnpm -C web install` e
   `pnpm -C web build`, a partir da pasta do repositório, e manda reinstalar quem instalou. Nenhum
   caminho `C:\Desenvolvimento\...` sobra em mensagem.
7. **O `asrstage`.** Cria `bin\portatil` como **primeiro** passo, antes de copiar qualquer coisa
   (com o Agent aberto a cópia das DLLs falha, e um `bin\` que já tem dados não pode ficar sem o
   marcador por isso), e não o reescreve se já existe. `run()` virou `montar(montagem)`, com as
   origens, o destino e o conferidor do modelo injetáveis, e um campo `portatil`: a montagem do pacote
   (F3) o desliga. O texto do marcador explica o que ele é.
8. **O autostart não mudou.** `internal/agent/autostart` não foi tocado, e `inicioComWindows` continua
   recebendo `os.Executable()`: o `Run\AuroraWhisper` guarda o caminho do `.exe`, e não o de uma pasta.
   Para o `bin\` isso é o mesmo valor de antes. Só a linha de log perdeu o `exe=`.
9. **Fora desta fatia.** O `cmd/dictlearn` continua achando o `historico.db` ao lado do
   `dictlearn.exe`, que mora no `bin\` (modo portátil), e não resolve a pasta de dados de uma
   instalação; ele não é instalado. O `-ldflags -X` e a `version` como `var` são da F3.
10. **Correções da revisão da F1** (mesmo branch, commits seguintes).
    - **Um `dicionario.json` ao lado do executável, no modo `localappdata`, agora avisa.** Instalado,
      ele não é usado (o dicionário vale na pasta de dados), e o log só dizia INFO "nenhum
      encontrado". Acontece com quem seguiu o README antigo ou montou o `bin\` com o `asrstage`
      antigo e nunca o rodou. É um WARN a cada subida, sem caminho, que manda copiar o arquivo para
      a pasta de dados. No portátil e no legado o arquivo vale e não há aviso; uma **pasta** chamada
      `dicionario.json` não conta.
    - **O erro das pastas não manda a pessoa ao `agent.log`.** Quando `LOCALAPPDATA` falta ou a
      pasta de dados não se cria, não há log em lugar nenhum, e o aviso dizia "os detalhes ficam no
      `agent.log`". `resolverPastas` marca o erro (`errDePastas`) e o aviso diz que não há
      `agent.log` desta vez; o próprio erro diz o que faltou.
    - **O giro do log, antes sem teste nenhum, tem teste**: gira na pasta de dados (e nada nasce
      na do programa), não gira abaixo do limite, e no portátil gira ao lado do executável.
    - **A trava na árvore sintática de `main.go` ficou mais firme**: exige as pastas resolvidas por
      `resolverPastas(pastas.Sistema())`, o log configurado com as mesmas pastas e o perfil
      (`perfilDoUsuario()`), o registro do modo, e **proíbe reatribuir a tabela de caminhos** (`cam`
      ou `cam.Campo`), que desviava um consumidor sem mudar o argumento que a trava lia.
    - **O teste do log olha o caminho em três formas** (crua, com a barra dobrada, com barra
      normal): o `slog` dobra a barra quando a mensagem tem espaço, e o teste só olhava a crua.
    - **O roteiro manual** (passos 4 e 5 acima, e o B6 e o trecho do `grep` em
      `teste-manual-bandeja-e-painel.md`) foi corrigido: o `agent.log` é anexado e tem linhas
      antigas com caminho; o modo se decide na subida, então é preciso sair pela bandeja antes de
      renomear o marcador; o B6 já não espera `exe=` na linha do autostart; e o que era `grep` é
      `Select-String`, porque o terminal do Mauro é o PowerShell 5.1.
    - **A garantia do modo portátil foi reescrita com precisão** (seção "Modo portátil" e risco
      11), e o texto do marcador e do `asrstage` também.
    - **Duas coisas não foram resolvidas aqui, de propósito:** o aprender no Agent instalado (seção
      "O dicionário que não existe") é decisão do Mauro, e o `whisper-server.log` com o caminho do
      modelo é da F2, agora registrada como pré-requisito da F5.

### Como foi verificado (F1)

- **Testes novos:** 39 funções de teste em 4 arquivos: `internal/agent/pastas` (18),
  `cmd/agent/caminhos_test.go` (11), `cmd/asrstage/marcador_test.go` (9) e uma em
  `internal/agent/painel`. Todos em pasta temporária e com ambiente injetado; nenhum toca no
  `%LOCALAPPDATA%` real, no registro, na área de transferência, nem abre janela ou microfone.
  `go test ./...`, `go vet ./...` e `gofmt -l .` limpos; `GOOS=linux go vet` nos pacotes novos.
- **Mutações** numa cópia isolada (`git archive`), com o diff conferido e `-timeout 90s`: 24
  mutantes, **todos mortos**. Marcador ignorado (M1); `historico.db`, `dicionario.json`,
  `agent.log` ou `whisper-server.log` gravados em `Programa()`, e servidor, painel ou pasta do modelo
  lidos de `Dados()` (M2, sete variantes); `asrstage` sem marcador, de duas formas (M3); a regra do
  `historico.db` existente removida (M4); o `dicionario.json` virando gatilho do legado (M5);
  `agent.log` sem `Anonimizar` (M6); o log do modo com o caminho (M7); `%LOCALAPPDATA%` relativo
  aceito (M8); `criarMarcador` reescrevendo o existente (M9); `Anonimizar` sem a barra dobrada
  (M10); `MkdirAll` removido (M11); uma pasta chamada `portatil` ligando o modo (M12, refeito porque
  a primeira forma não compilava); a página sem painel com o caminho fixo (M13); a chave `caminho`
  de volta ao log do histórico (M14); `run()` abrindo o histórico em `Programa()` ou o servidor de
  ASR com o caminho do log (M15 e M16, pegos só pela trava na árvore sintática de `main.go`); e o
  `exe=` de volta à linha do autostart (M17).
- **Depois da revisão (item 10):** 44 funções de teste (`pastas` 18, `cmd/agent` 16, `asrstage` 9 e
  1 em `painel`). **Mais 24 mutantes, todos mortos**, em outra cópia isolada: o giro do log na
  pasta do programa, o limite do giro (`<=` e sempre girar) e o `Rename` removido; o perfil não
  anonimizado (em `saidaDoLog` e em `main`); `registrarPastas` removida de `run`; `cam.Historico`
  e `cam.LogDoServidor` reatribuídos; `pastas.Resolver` direto em `main`; `caminhosDe` e
  `configurarLog` com pastas vazias; o aviso do dicionário desligado, sem a checagem do modo,
  disparando também no legado, aceitando uma pasta, como INFO, e com o caminho; o erro de pastas sem
  a marca, sem o `Unwrap` e com a frase do `agent.log`; `perfilDoUsuario` sempre vazio; e o log do
  modo com `Programa()` ou `Dados()`. **Um mutante sobreviveu na primeira rodada** (o aviso do
  dicionário com o caminho dentro da mensagem): o teste olhava só a forma crua do caminho, e o
  `slog` dobra a barra numa mensagem entre aspas. Corrigido com `trazOCaminho`, e ele morreu. Três
  mutantes (`info.Size() < 0`, `err == nil && info != nil` e `dir[:0]`) tiveram de ser refeitos porque
  a primeira forma não compilava. Falta o roteiro manual.
- **O que nenhum teste prova:** que o Agent de verdade, no Windows de verdade, abre os arquivos nas
  pastas certas. `run()` sobe o hook, o microfone e a bandeja e não roda em teste; a ligação dos
  consumidores é conferida só no código. Isso é o roteiro manual acima.

## Decisões tomadas na implementação (F3 a F5, e o dicionário vazio)

O dicionário vazio tem a seção própria, acima. O resto, em ordem:

1. **`version` é `var`** (`cmd/agent/main.go`), com o valor de desenvolvimento por padrão; o build de
   release a troca com `-ldflags "-X main.version=<versão>"`. Um teste lê o `main.go` e exige `var`
   inicializada por literal (o `-X` não age sobre `const` nem sobre o que vem de chamada de função, e
   não dá erro).
2. **`asrstage -destino <pasta>` e `-sem-dicionario`.** Sem argumentos monta o `bin\` como sempre.
   **Qualquer `-destino` é a pasta do pacote e nunca leva o marcador `portatil`** (não há uma
   terceira opção: o marcador na pasta que o Setup troca desviaria os dados para dentro dela; para
   uma segunda pasta de desenvolvimento, cria-se o arquivo à mão). **Nem leva dicionário:
   `-destino` implica `-sem-dicionario`** (revisão, item 10: nenhum Agent leria o dicionário dali, e
   copiá-lo só abria a chance de vazar o pessoal de quem desenvolve); `-sem-dicionario` sem
   `-destino` vale para o `bin\`. A pasta do pacote tem de estar **limpa**: a montagem recusa, antes
   de copiar o modelo, o marcador, `historico.db*`, `agent.log*`, `whisper-server.log` e um
   `dicionario.json`. Nunca apaga. Argumento sobrando e opção desconhecida são erro. O `bin\` de
   desenvolvimento não é recusado por ter dados.
3. **`scripts/montar-instalador.ps1`** (PowerShell 5.1, só ASCII: o 5.1 lê UTF-8 sem BOM como ANSI).
   Roda `asrstage -destino -sem-dicionario`, compila o Agent com `-trimpath -H=windowsgui -X
   main.version=<versão>` (e **confere o binário, sem executá-lo**: que o `-X` pegou, pelo símbolo
   `main.version.str` do `go tool nm`; que o `-trimpath` pegou, pelo `go version -m`; e que nenhum
   caminho da máquina de build ficou), roda `pnpm build --outDir <staging>\painel` (a opção
   passa pelo script `build`, que tem o `tsc -b` antes do `vite build`: testado com a pasta de
   saída fora de `web\`), monta `licencas\`, confere o staging (o que tem de ter, e que não tem
   dicionário, marcador, banco nem log) e chama o ISCC se existir. Só escreve dentro de `dist\`
   (a guarda vem antes do único `Remove-Item`), nunca em `bin\`, nunca executa o Agent nem o Setup.
   `-ExigirIscc` faz a falta do ISCC ser erro (o workflow usa). O Setup sai com um `.sha256` ao
   lado.
4. **`licencas\`.** Do repositório: `LICENSE`, `THIRD_PARTY_NOTICES.md` e a OFL da fonte do overlay.
   Do Go: a licença do GOROOT e, de cada módulo que entra no Agent (12, por `go list -deps
   ./cmd/agent`), os `LICENSE`, `NOTICE` e `PATENTS` do cache de módulos: testado, 20 arquivos, nenhum
   módulo sem licença. Baixadas na montagem (o zip do whisper.cpp não traz licença): o MIT do
   whisper.cpp (no commit da tag `b5130`), o MIT do `openai/whisper` (num commit fixo, e não em
   `main`) e o texto da Apache-2.0. **Os três downloads não têm hash fixo ainda e os endereços não
   foram baixados**: o script imprime o SHA-256 de cada um, e quem fecha o primeiro release o fixa
   (os dois endereços do GitHub já são imutáveis; um teste exige um commit de 40 hexadecimais). O
   painel leva sozinho o
   `licencas-fontes.txt` (vem de `web/public/`). O `THIRD_PARTY_NOTICES.md` ganhou a seção "No
   Setup".
5. **`installer/aurora-whisper.iss`** (o plano dizia `deploy/windows/`; o `deploy/` é do backend).
   UTF-8 **com BOM** (sem ele o Inno lê ANSI e os acentos saem trocados; um teste confere). Lista de
   arquivos **explícita**, sem coringa na raiz, e um teste confere que as 12 DLLs são as do
   `asrstage` e que cada nome repetido do Go (mutex, pasta de dados, chaves, modelo, `Run` do
   autostart, executável) é o do Go. Sem seção `[Registry]` e sem `RegWrite` (decisão 3). O
   desinstalador: fecha o Agent (se estiver aberto, pede a própria confirmação antes; no modo
   silencioso não pergunta); pergunta (padrão não; modo silencioso nunca apaga); com o Agent
   fechado e o caminho conferido (exatamente `<localappdata>\AuroraWhisper`, montado no código, que
   não é atalho/junção, e sem relação com a pasta do programa), apaga **só essa pasta** e as chaves
   `AuroraWhisper/openai` e `AuroraWhisper/anthropic` (por `CredDeleteW`, a mesma chamada do Agent,
   tipo genérico), e sempre remove o `Run` e a marca do
   Windows se apontarem para o executável desinstalado. A pasta do programa não pode ser a de
   dados, nem conter, nem estar dentro dela (conferido na página e no `/DIR=` silencioso).
6. **O Agent fecha pelo nome e por usuário.** `taskkill /IM aurora-whisper-agent.exe` com o filtro
   `USERNAME eq <DOMÍNIO>\<usuário>` (sintaxe do filtro conferida com `tasklist`, só leitura), para
   um Setup elevado não fechar o Agent de outra conta; sem `/F`; espera o mutex por até 20 s. Um
   Agent de desenvolvimento (`bin\`), que tem o mesmo nome de executável, também fecha: os dois não
   rodam juntos de qualquer modo.
7. **O VC++.** Mínimo 14.51 (seção "Ferramenta, build e pacote"), baixado só se faltar, com a
   assinatura conferida antes de executar. Se não for possível instalá-lo, o Setup instala mesmo
   assim, avisa, e **tira a opção "abrir agora"** (o Agent não abriria).
8. **O workflow** (`.github/workflows/release.yml`). Dispara só em tag `v*` (nem manual); `contents:
   write` só no job, para criar o release; usa o `gh` do runner, e não uma ação de terceiro; o único
   `gh release` que escreve é o `create --draft --verify-tag`, e 0.x leva `--prerelease`. Baixa o zip
   do whisper.cpp (upstream, com o **espelho** no release do modelo como reserva) e o modelo, cada
   um com SHA-256 fixo; o release do modelo é **uma variável com `TODO(Mauro)`**: até existir, o
   job falha no passo do modelo, de propósito. **Um portão fechado** (`F2_PRONTA: 'false'`, primeira
   etapa) o faz falhar antes de qualquer coisa enquanto a F2 não entrar; abri-lo é decisão, quando a
   F2 estiver feita. O `GH_TOKEN` (com `contents: write`) só chega às três etapas que chamam o `gh`,
   e não ao `pnpm install` nem ao `vite build`, que rodam código de terceiros; o checkout não deixa a
   credencial no `.git/config`. Roda os `run:` com `shell: powershell` (o 5.1, como o
   terminal do Mauro). Um teste de texto garante que ele não publica (`--draft=false`, `--latest`,
   `gh release edit` e `upload` proibidos, uma só chamada de `create`, uma só permissão de escrita) e
   que os hashes e o tamanho são os de `model.SmallQ4_0` e da ADR.
9. **Fora desta leva, ainda a fazer:** o teste de instalação no runner descartável (F5), e avaliar
   o risco 11 (um build sem `-X` recusar o modo `localappdata` sem o marcador: agora é possível,
   porque `version` é `var`). **Feito depois, em 01-out-2026** (ver as seções abaixo): o ícone `.ico`
   (F3); a F2, o `whisper-server` com `cmd.Dir` na pasta do programa, o modelo pelo nome relativo e a
   saída do servidor pelo filtro de caminhos, que era **pré-requisito da F5**; e o roteiro
   `docs/teste-manual-instalador.md` (F6), escrito e ainda **não executado**.
10. **Correções da revisão (mesmo branch).** A revisão achou que os testes de texto não protegiam o
    que importa num `.iss` que nunca foi compilado: um mutante que invertia a guarda da junção, ou
    tirava `DirExists`, `Length(Local) < 4`, o `CompareText` do caminho exato, o `FecharOAgent` do
    `PrepareToInstall`, a conferência da assinatura do VC++ ou o `Check` do "abrir agora" **passava
    com tudo verde** (27 de 41 mutantes sobreviveram). O mesmo valia para o hash do zip do
    whisper.cpp no workflow. O que mudou:
    - **Golden do que protege** (`cmd/asrstage/instalador_golden_test.go` e o texto esperado em
      `instalador_golden_data_test.go`). Cada `function`/`procedure` do `[Code]` (e o `const`/`var`
      que o abre), as seções `[Setup]`, `[Tasks]`, `[InstallDelete]`, `[Icons]` e `[Run]`, e as
      etapas do workflow que decidem integridade e segredo (portão, checkout, versão da tag, zip,
      modelo, montagem) são comparadas **inteiras**, sem comentário e sem recuo, com o texto
      revisado. Função nova no `[Code]` sem texto esperado falha. Mudar uma proteção obriga a mudar o
      teste, e é essa segunda leitura que se quer. Comentário, recuo e linha em branco **não**
      contam (três mutantes de controle sobrevivem de propósito).
    - **O hash do zip** (upstream e espelho) tem teste próprio: exatamente duas conferências, nenhum
      `$ok = $true`, o `throw` depois e o `Expand-Archive` só depois.
    - **`-destino` implica `-sem-dicionario`** (item 2). Antes `asrstage -destino X` copiava o
      dicionário pessoal para a pasta do pacote, e um teste exigia isso; nenhum Agent o leria dali.
    - **`.iss`:** `ContemOuIgual` usa `AnsiLowercase` (o `Lowercase` do Inno só converte A-Z, e um
      perfil com acento escapava da trava programa/dados); `EhLinkOuJuncao` falha para o lado seguro
      (se o `FindFirst` falha, conta como atalho e a limpeza recusa); as chaves de API saem por
      `CredDeleteW`, e não por `cmdkey /delete` (a documentação não diz se o `cmdkey` acha uma
      credencial genérica pelo nome puro; o resultado vai para o log do desinstalador, e o erro 1168,
      "não existia", é esperado); o `InitializeUninstall` roda **antes** da confirmação padrão do
      desinstalador e fechava o Agent mesmo se a pessoa dissesse não depois, então agora, com o Agent
      aberto, pede a própria confirmação, e a falha em fechar usa `SuppressibleMsgBox` (a
      desinstalação silenciosa não espera um clique).
    - **Script:** o `-X` é conferido pelo símbolo `main.version.str` (`go tool nm`), que o linker só
      cria quando aplica um `-X`: procurar o texto da versão não provava nada para a `0.1.0`,
      porque o valor de desenvolvimento (`0.1.0-sprint2`) a contém, **e o Go o deixa no binário mesmo
      com o `-X`** (medido). O `-trimpath` é conferido por `go version -m` (`-trimpath=true`). O
      `go version -m` **não** lista o `-ldflags` nesta versão do Go; por isso o `nm`. As conferências
      rodaram à parte contra dois binários de rascunho (com e sem `-X`): passa no primeiro e para no
      segundo. As licenças do whisper.cpp e do `openai/whisper` apontam para commits fixos.
    - **Workflow:** `GH_TOKEN` só nas três etapas que chamam o `gh`; `persist-credentials: false`
      no checkout; o portão da F2 (item 8).
    - **Não feito:** o SHA-256 das três licenças baixadas (exige baixar o arquivo; fica com quem
      fecha o primeiro release); fixar as ações do workflow por SHA (opcional); normalizar nome curto
      8.3 na trava programa/dados (o `DelTree` só age na pasta exata, e nunca em atalho).

### Como foi verificado (F3 a F5 e o dicionário vazio)

- **Testes novos:** 51 funções de teste: `internal/dictionary/vazio_test.go` (12),
  `cmd/agent/dicionario_vazio_test.go` (10) e `cmd/agent/versao_test.go` (1),
  `cmd/asrstage/pacote_test.go` (11) e `cmd/asrstage/instalador_test.go` (17).
  `go test ./... -count=1`, `go vet ./...` e `gofmt -l .` limpos. Nenhum teste toca no
  `%LOCALAPPDATA%` real, no registro, na área de transferência, nem abre janela ou microfone; os de
  arquivo usam pastas temporárias, e os do `.iss`, do script e do workflow **só leem texto**.
- **Mutações** numa cópia isolada (`git archive`, fora do repositório), uma por vez, com o diff
  conferido (a troca tem de casar exatamente uma vez) e `-timeout 90s`: **87 mutantes, 86 mortos e
  1 equivalente**. O dicionário vazio (12): o link vira rename, `O_EXCL` vira `O_TRUNC`, `entradas`
  nulo, temporário que sobra, usuário vazio ou só de espaços aceito, o "já existe" do link vira
  "criou", arquivo truncado que não se apaga, nunca criar, e a conferência de existência devolvendo
  `true`; o equivalente é tirar o `Lstat` inicial (o link recusa do mesmo jeito, e o `Lstat` só poupa
  o temporário a cada subida de quem já tem dicionário). O Agent (8): chamada de
  `garantirDicionario` removida ou depois de abrir, outro `usuario`, sobrescrever com `Gravar`,
  caminho no log, falha sem aviso, criação silenciosa, `version` de volta a `const`. O `asrstage`
  (17): marcador no pacote, `-sem-dicionario` ignorado de duas formas, conferência da pasta do
  pacote removida ou aplicada ao `bin\`, cada um dos nomes proibidos removido, argumento sobrando e
  `-destino` vazio aceitos, uma DLL a menos. O `.iss` (26), o script (12) e o workflow (12): `/F` no
  `taskkill`, `[Registry]`, `DelTree` em outra pasta ou a mais, padrão da pergunta, mutex, pasta de
  dados e VC++ mínimos trocados, coringa na raiz, BOM, assinatura sem conferir, `RegWrite`, chave de
  API a mais, download a mais, `--draft`, `gh release edit`, hash e tamanho trocados, permissão de
  escrita no topo, gatilho fora de `v*`, o `-trimpath`, o `-X` e o `-o` do `go build`, dicionário do
  repositório embutido, acento no script, segundo `Remove-Item`. **Dois testes ficaram fracos e foram
  reforçados:** a conferência do hash e do tamanho dentro do passo do modelo (o mutante que a
  tirava sobreviveu) e o `-trimpath` do script (o teste achava a palavra num comentário). Dois
  mutantes (`-sem-dicionario` ignorado e o `-trimpath`) não compilaram ou não casaram na primeira
  forma e foram refeitos.
- **Executado de verdade, sem rodar o Agent:** o `go build -trimpath -ldflags "-X main.version=…"`
  (para uma pasta de rascunho, com `-o`; a versão entrou e nenhum caminho da máquina ficou, e **sem**
  `-trimpath` os caminhos aparecem, o que prova que a conferência do script age); o `pnpm build
  --outDir` para fora de `web\`; a coleta de licenças do Go; o parser do PowerShell (zero erros) no
  script e em cada passo do workflow; o YAML do workflow.
- **O que NÃO foi verificado:**
  - **O `.iss` não foi compilado.** (Valia na hora em que foi escrito; **desde 01-out-2026 o Mauro o
    compilou com o Inno Setup 6.7.3, nesta máquina, sem erro**: o `0.1.0-rc.1`, 135 MB, nunca
    executado.) O Pascal Script foi escrito contra a documentação, sem compilador.
  - **O script não rodou inteiro** (faltam `thirdparty\` e `models\` no worktree) e **o workflow
    nunca rodou**. Os três endereços de licença e o do zip do whisper.cpp não foram baixados.
  - **Nada do que o Setup faz foi executado:** o `taskkill` e a espera pelo mutex (e se
    `CheckForMutexes` aceita o prefixo `Local\`), o download e a assinatura do `vc_redist`, a
    exigência de UAC, o `CredDeleteW` das chaves (que mexeria no Gerenciador de Credenciais de
    verdade; antes era `cmdkey /delete`), o `DelTree`, a limpeza do `Run`. Tudo isso é o roteiro
    manual (F6), que deve conferir também que as duas chaves somem do Gerenciador depois de
    desinstalar apagando os dados.
  - Que o servidor de voz roda com o redistribuível mínimo (14.51), e que o Setup fica em torno de
    140 MB (estimativa da ADR, nenhum build real).

### Como foi verificado (correções da revisão, item 10)

- **Testes:** 9 funções novas (uma substitui a que exigia copiar o dicionário para o pacote), fora
  as que já existiam e foram reforçadas: 3 no golden (`[Code]`, seções e etapas do workflow), 3 de
  texto (portão da F2, escopo do `GH_TOKEN`, licenças em commit fixo) e 3 do `asrstage`.
  `go test ./... -count=1` verde, com 4075 testes e subtestes passando; `go vet ./...` e
  `gofmt -l .` limpos. Só **leem** texto ou usam pasta temporária.
- **Mutações** numa cópia isolada (`git archive`, diff conferido e restauração conferida byte a
  byte contra a cópia original, `-timeout 90s`): **83 mutantes, todos mortos**. `.iss` (44): a guarda
  da junção invertida, `EhLinkOuJuncao` sempre falso, o bit lido ao contrário, a falha aberta, e
  cada linha de `PodeApagarOsDados` removida; `PastaDosDados` redirecionada para `{app}`;
  `FecharOAgent` ausente, sempre verdadeiro e ignorado no desinstalador; a trava programa/dados
  removida de `PrepareToInstall`; a assinatura do VC++ com `-ne` e sem o código de saída; o mínimo
  do VC++ com `>=` e com `>`; `Installed <> 1` removido; `VcPronto` sempre verdadeiro; código de saída
  extra aceito; o `Check` do "abrir agora"; o modo silencioso, o padrão da pergunta, o `DelTree` em
  outra pasta, uma terceira chave de API, o tipo da credencial; `/F` no `taskkill`; a espera sem
  teto; `PrivilegesRequired=admin`; o `AppId`; o `InstallDelete` sem conferência; a chave do VC++
  e `HKLM64`. Workflow (19): o hash do zip forçado nos dois ramos, `-ine`, o `throw` final virando
  aviso, a expansão antes da conferência, o `GH_TOKEN` no job e em mais uma etapa, o checkout com
  credencial, o portão (nunca fecha, sem a variável, sem aspas, fora da primeira etapa). Script
  (11): o `nm` e o `go version -m` desligados, com regex frouxa e lendo outro arquivo; licenças de
  volta a `main` e à tag; sem `-trimpath`; sem `-sem-dicionario`; sem a guarda do `Remove-Item`. Go
  (9): o pacote deixa de forçar `-sem-dicionario`, aceita um `dicionario.json`, volta a levar o
  marcador, o ramo invertido; o `bin\` sem dicionário. **Três controles** (comentário de bloco e de
  linha alterados, recuo e linhas em branco) **sobrevivem de propósito**: o golden não pode
  prender o que não é código. **Um mutante sobreviveu e virou teste:** forçar "sem dicionário"
  também no `bin\` (o ramo que copia o dicionário só tinha o teste do pacote, que foi invertido);
  agora há `TestMontarDev_SemAOpcaoCopiaODicionarioPadrao`. Um mutante (`-sem-dicionario` ignorado)
  não compilou na primeira forma e foi refeito.
- **Executado de verdade, sem rodar o Agent:** `go build -trimpath -ldflags "-X main.version=9.9.9"`
  e o mesmo sem o `-X`, para uma pasta de rascunho e com `-o`. As conferências do script (o bloco do
  `nm`, do `go version -m` e do texto) foram executadas à parte contra os dois: passam no
  primeiro, e o segundo para com "o -X main.version nao pegou". O parser do PowerShell 5.1 (zero
  erros). Os binários de rascunho nunca foram executados.
- **Consultado, não executado:** a documentação do Inno Setup (`AnsiLowerCase` converte pela
  localidade e `LowerCase` só A-Z; `UninstallSilent`, `SuppressibleMsgBox` e `DLLGetLastError`
  existem; a declaração `external 'CredDeleteW@advapi32.dll stdcall'` com `DWORD` e `BOOL`); a
  árvore do GitHub (só metadados, sem baixar arquivo) para o commit e o tamanho do `LICENSE` do
  whisper.cpp (1078 bytes) e do `openai/whisper` (1063 bytes).
- **Continua sem verificar:** tudo o que o Setup faz de verdade (item "Nada do que o Setup faz foi
  executado", acima). Que o `.iss` compila **deixou de ser pendência** em 01-out-2026: o Mauro o
  compilou com o Inno Setup 6.7.3, sem erro (a sintaxe das funções novas, `AnsiLowercase`,
  `DLLGetLastError` e a declaração externa, que vinham só da documentação, foram aceitas).

## Decisões tomadas na implementação (F2, 01-out-2026)

O que a pendência pedia: o servidor de voz com `cmd.Dir` na pasta do programa e o modelo pelo nome
relativo (reduz o risco do caminho com acento; teste puro da montagem), como **pré-requisito da F5**
(sem ela o `whisper-server.log` instalado traz o caminho do modelo, com o nome do usuário). Feito
assim (**os itens 1 a 6 trataram só a linha do modelo; a revisão de 01-out-2026 achou a segunda linha
com caminho e o item 7 a corrige**):

1. **`local.Config.Dir` é a pasta de trabalho do servidor** (`cmd.Dir`), e o Agent a preenche com a
   pasta do programa (`configDoServidor`, em `cmd/agent/main.go`: `Dir: cam.Programa`). É a mesma do
   `whisper-server.exe`, das DLLs e do modelo; no `bin\` portátil, a do executável. Vazio mantém o
   comportamento de antes (o servidor herda a pasta atual do Agent): os testes de latência, que
   passam o caminho absoluto do `bin\`, não mudam.
2. **O modelo vai pelo caminho relativo a `Dir`, quando está dentro dela** (`caminhoDoModelo`):
   `-m ggml-small-q4_0.bin`. Só encurta quando dá para ter certeza do que "relativo" quer dizer: sem
   `Dir`, com caminho relativo (já é relativo a `Dir`), com `Dir` relativa, noutro disco ou fora da
   pasta (nunca sobe um nível), devolve o que recebeu. O Windows não diferencia caixa de letra, e
   `filepath.Rel` também não (teste). Quem confere o SHA-256 continua sendo `model.Resolve`, com o
   caminho inteiro, antes de o servidor subir.
3. **A montagem é pura e o `*exec.Cmd` nasce sem ser iniciado** (`montarComando`, `execCmd`, em
   `process.go`, sem build tag): os testes olham o caminho, os argumentos, `cmd.Dir` e a saída, em pasta
   temporária e com acento e espaço no nome do usuário, sem executar nada. Um teste do Windows roda
   `cmd /c cd` (um processo de verdade e inofensivo, como o `ping` dos testes de antes) para provar que
   `Dir` chega ao `CreateProcess`; outro, de ponta a ponta, reexecuta o **próprio binário de teste**
   como "whisper-server" de mentira (só com uma variável de ambiente que o teste põe) e confere que
   `Start` o sobe na pasta pedida, com o `-m` pelo nome do arquivo e nenhum nome de usuário nos
   argumentos. Nenhum `whisper-server.exe` e nenhum modelo.
4. **O `whisper-server.log` já ia para a pasta de dados** (F1, `cam.LogDoServidor`); a F2 não o
   moveu, e confere que continua lá (testes e a trava na árvore sintática de `main.go`).
   `abrirASRLocal` monta a configuração só por `configDoServidor` (uma trava na árvore sintática
   recusa uma `local.Config` escrita à mão ali, que perderia o `Dir`).
5. **O que a F2 NÃO faz.** Não muda o `ServerPath` (absoluto) nem as DLLs: o carregador do Windows as
   acha pela pasta do **executável** do servidor, e não pela pasta atual, então um perfil com acento
   continua passando pelo caminho do `.exe` e das DLLs (o risco 3 diminui, não some). Não troca a
   mensagem de erro do Agent, que continua mandando abrir o `whisper-server.log`.
6. **O portão `F2_PRONTA` do workflow continua fechado.** A ADR dizia que abri-lo é decisão, quando a
   F2 estiver feita. A F2 está feita nos testes, mas **ninguém leu ainda o `whisper-server.log` de um
   servidor de verdade depois da mudança**: abrir o portão fica com o Mauro, depois do roteiro abaixo.
7. **A saída do servidor passa pelo filtro de caminhos (correção da revisão, 01-out-2026).** A F2 dos
   itens 1 a 6 só limpava a linha do modelo, e o código e a spec diziam que "o log não leva o caminho
   do perfil". Não é verdade: a **primeira** linha do log, impressa pelo ggml do whisper.cpp, é
   `load_backend: loaded CPU backend from <pasta do executável>\ggml-cpu-<variante>.dll`, e a pasta é
   a do **executável** do servidor (a que o Windows devolve ao processo), que `cmd.Dir` não muda. No
   `bin\whisper-server.log` real (lido, só leitura) a linha 1 é essa, com o caminho absoluto do `bin\`;
   instalada, seria `C:\Users\<nome>\AppData\Local\Programs\Aurora Whisper\...`. As conferências
   manuais da F2 (`Select-String ... 'ggml-small-q4_0'`) olhavam só a linha do modelo e não podiam achar
   isso. Agora:
   - `abrirLogDoServidor` (`cmd/agent/logservidor.go`) cria o `whisper-server.log` e devolve a saída do
     servidor **atrás do mesmo filtro do `agent.log`**, `pastas.AnonimizarLinhas`; `abrirASRLocal` recebe
     as pastas e o perfil (`pp` e `perfilDoUsuario()`, os de `configurarLog`) e liga o resultado a
     `cfg.SaidaDoServidor`. Uma trava na árvore sintática confere a ligação.
   - **Por que não basta o `Anonimizar`:** o comentário dele já dizia que não serve à saída de outro
     processo, que chega em pedaços quaisquer, e um caminho pode vir partido entre duas escritas.
     `AnonimizarLinhas` junta o que recebe até a quebra de linha e só então entrega, linhas inteiras,
     ao `Anonimizar`; o `Close` entrega o que sobrou sem quebra e **não** fecha o arquivo (quem o abriu o
     fecha). É seguro para escritas concorrentes (o `os/exec` usa um `Writer` só para o stdout e o
     stderr do filho). Passando de 64 KiB sem uma quebra de linha, entrega tudo o que tem: um caminho
     partido nesse ponto não é achado, e o servidor não escreve uma saída assim.
   - **A codificação.** Não se sabe se o servidor escreve o nome com acento em UTF-8 ou na página de
     código do Windows (a linha da biblioteca vem de `GetModuleFileNameW` convertido por ele, e não
     foi lida de um servidor com acento no perfil). O filtro troca o caminho **nas duas**: para um
     caminho que cabe no Latin-1 (os acentos do português), gera também a forma ANSI, com um byte por
     caractere. Para o que não cabe (U+0100 em diante) e para a faixa em que o Windows-1252 difere do
     Latin-1 (U+0080 a U+009F), **não** gera a forma ANSI: só a UTF-8 vale. É o limite honesto.
   - **A conferência manual passou a ser no log inteiro:** nenhum `X:\` e nenhum nome de conta, nas
     duas leituras (UTF-8 e ANSI). Está no I5 do roteiro do instalador e abaixo.
   - **Não coberto:** uma subida que falha e é refeita em outra porta (o `Start` tenta de novo, com o
     mesmo `Writer`) pode deixar uma linha pela metade, de um processo morto, colada à primeira do
     seguinte; um caminho partido nessa emenda não seria achado. Exigiria um servidor que morra no meio
     de uma escrita de linha, e não se espera isso.

**Como foi verificado.** Testes novos: 6 funções puras e de montagem (uma em tabela, com 11 casos de
caminho) em `internal/asr/local/comando_test.go`, 2 do Windows em `comando_windows_test.go`
(a tabela de caixa de letra e disco, e o `cmd /c cd`), 1 de ponta a ponta com o servidor de mentira
em `servidor_falso_windows_test.go`, e 4 em `cmd/agent/servidor_asr_test.go`. `go test ./... -count=1`,
`go vet ./...`, `gofmt -l .` e `GOOS=linux go vet` limpos. **Mutações**, numa cópia isolada
(`git archive`), com o diff conferido (um trecho por mutante), `-timeout 90s` e a restauração conferida
contra o original: **16 mutantes, todos mortos**. `caminhoDoModelo` devolvendo sempre o caminho
inteiro, sem a guarda de "sobe um nível" (a primeira forma não compilava e foi refeita), sem a de "é a
pasta de cima", sem a de "é a própria pasta", sem a de caminho relativo e com a guarda inicial sempre
falsa; `-m` com `ModelPath` direto; `montarComando` com `Dir` vazio e com os argumentos de uma
`Config` sem `Dir`; `execCmd` sem `cmd.Dir`; `Start` que não usa `montarComando` (o único que as
montagens puras não pegam: morreu no teste de ponta a ponta); `configDoServidor` sem `Dir` e com `Dir`
na pasta de dados; `abrirASRLocal` com `local.Config` à mão; o log do servidor aberto em `Programa()`
(na chamada e na tabela). Um guarda era redundante (`Dir` relativa: `filepath.Rel` já recusa) e foi
removido em vez de testado.

**Manual, do Mauro, depois de recompilar o Agent** (o servidor de verdade; nada disto roda em
`go test`). Feche o Agent pelo menu (Sair), monte e abra como no roteiro da bandeja, dite uma frase
e, com o Agent ainda aberto, leia o log do servidor (só leitura):

```powershell
Select-String -Path C:\Desenvolvimento\aurora-whisper\bin\whisper-server.log -Encoding UTF8 -SimpleMatch 'ggml-small-q4_0', '<dados>'
```

```powershell
Select-String -Path C:\Desenvolvimento\aurora-whisper\bin\whisper-server.log -Encoding UTF8 -Pattern '[A-Za-z]:\\'
```

Esperado: o ditado funciona; o primeiro comando mostra as duas linhas, a do modelo (só
`ggml-small-q4_0.bin`) e a da biblioteca (`<dados>\ggml-cpu-...dll`: no `bin\` portátil o programa e os
dados são a mesma pasta, e o rótulo é `<dados>`); **o segundo não imprime nada**, nem `C:\Users\...` nem
`C:\Desenvolvimento\...`. No Setup instalado o rótulo é `<programa>`, e a conferência (no log inteiro,
com o nome da conta, nas duas leituras) está no I5 de `docs/teste-manual-instalador.md`. Se o servidor
não subir, ou o segundo comando imprimir uma linha, copie a **linha** (e, se o servidor não subiu, o
`whisper-server.log` inteiro): a hipótese a conferir é o servidor ter escrito o caminho de outro jeito
(outra codificação, ou partido por um byte que não é quebra de linha).

**Como foi verificada a correção do item 7.** Testes novos: 12 funções em
`internal/agent/pastas/linhas_test.go` (a saída partida em pedaços de 1 a N bytes, só linha inteira
sai, várias linhas numa escrita, o `Close` e o `Write` depois dele, o limite de linha, o erro do
destino sem repetir a linha, escritas concorrentes, as duas codificações, `emLatin1` nas bordas e as
formas do caminho) e 4 em `cmd/agent/logservidor_test.go` (o arquivo de verdade, em pasta temporária e
com o perfil `José Teste`, escrito em pedaços e nas duas codificações; recriado a cada subida; o
arquivo que não abre; e a trava na árvore sintática de `abrirASRLocal`). A tabela de ligações de
`TestRun_LigaCadaConsumidorAoCaminhoCerto` ganhou os argumentos de `abrirLogDoServidor` e
`abrirASRLocal`. `go test ./... -count=1` (56 pacotes, 4151 testes e subtestes, 5 pulados, 0 falhas),
`go vet ./...` e `gofmt -l .` limpos; `GOOS=linux go vet` limpo nos pacotes mexidos. **Mutações**,
numa cópia isolada (`git archive`), diff conferido, `-timeout 90s` e restauração conferida:
**41 mutantes, todos mortos** (23 no filtro de linhas e na codificação: `IndexByte` no lugar de
`LastIndexByte`, a entrega sem a quebra, o limite com `>` e com `-1`, o `Write` sem checar o
fechamento, o `Close` sem entregar o resto e escrevendo sem nada, a ordem de escrever e deslocar, o
erro que não esvazia a fila, o retorno errado, o `Write` sem o mutex, o filtro ausente, o pendente
descartado, as quatro bordas de `emLatin1`, as formas ANSI ausentes ou duplicadas; 7 em
`abrirLogDoServidor`: sem filtro, sem fechar o filtro ou o arquivo, `O_APPEND`, o `io.Discard` no lugar
do nulo, o perfil vazio, as pastas vazias; 7 em `main.go`: o perfil ou as pastas vazios nas duas
chamadas, a configuração sem a saída ou com o `stderr`, um `os.Create` à mão; e 4 no overlay, ver
ADR-010). O do `Write` sem o mutex morre por pânico (`slice bounds out of range` no `entregar`), e não
por asserção: é a corrida de verdade, e não há `-race` nesta máquina (sem cgo). Um mutante
equivalente (a guarda de reentrada do `Close`) foi removido do código, e o `if` em volta de
`cfg.SaidaDoServidor` também (redundante: `abrirLogDoServidor` devolve uma interface nula de verdade).

## Decisões tomadas na implementação (o ícone, 01-out-2026)

O que faltava na F3: o ícone. O executável do Agent não tinha ícone nenhum (o Explorador, o Menu
Iniciar e "Aplicativos" mostrariam o genérico), e o Setup saía com o do Inno. Feito assim:

1. **O desenho é o da bandeja.** `cmd/gerarico` chama `tray.PixelsDoIcone` (o Pico estourado, tema
   escuro, ativo) e o compõe sobre o **ladrilho preto arredondado** do `mark-small.svg` (`rx` 7 de 32,
   `#070A09`): o ícone da bandeja não tem fundo, porque fica sobre a barra de tarefas, mas o do
   executável aparece sobre fundo claro e escuro, e o arquivo SVG já é o símbolo com o ladrilho.
   Nenhuma imagem de origem e nenhum redesenho: mudar a bandeja muda o ícone no próximo
   `go run ./cmd/gerarico`. Olhado renderizado (16, 24, 48 e 256 px do `.ico`, e o de 32 px que o
   Windows extraiu do executável), e não só medido.
2. **Dois arquivos, ambos no repositório.** `installer/aurora-whisper.ico` (o Setup: camadas de 16,
   24, 32, 48, 64 e 256 px; bitmap de 32 bits com a máscara até 64, PNG em 256) e
   `cmd/agent/icone_windows_amd64.syso` (os mesmos bytes como recursos `RT_ICON` e `RT_GROUP_ICON`
   num objeto COFF, que o `go build` do Agent liga sozinho e só para Windows x64, pelo sufixo do nome:
   `GOOS=linux` o ignora). **Sem cgo e sem ferramenta externa**: o `.syso` é escrito em Go puro
   (~100 linhas, `cmd/gerarico/syso.go`), então `rsrc` e `goversioninfo` **não** entraram, e não há
   pendência de dependência. O ícone fica **dentro do executável** e do Setup, e não como arquivo
   solto na pasta do programa: o `UninstallDisplayIcon` (que já apontava para o `.exe`) e os atalhos
   passam a mostrá-lo.
3. **Regenerar:** da raiz do repositório, `go run ./cmd/gerarico`, e conferir o diff dos dois
   arquivos. Um teste (`gerado_test.go`) falha se os arquivos do repositório não forem o desenho de
   hoje, camada por camada; compara pixels, e não bytes, porque o PNG de 256 px pode mudar de bytes
   com a versão do Go sem mudar de desenho.
4. **No `.iss`:** `SetupIconFile=aurora-whisper.ico` (relativo à pasta do `.iss`), e a linha entrou no
   golden da seção `[Setup]`. Um teste confere que o arquivo existe, tem as camadas de 16, 32, 48 e
   256 px, que o caminho é relativo e que o `.syso` existe.
5. **Verificado, sem executar nada.** O Agent foi compilado para uma pasta de rascunho, com `-o`, e
   **nunca executado**: o ligador do Go aceitou o `.syso`; o `debug/pe` leu o `.rsrc` do executável
   com os sete recursos (seis `RT_ICON` e o grupo) nos RVAs certos; o `System.Drawing` do Windows
   extraiu dele o ícone de 32 px e carregou do `.ico` as camadas de 16 a 64 px. Os testes leem o
   `.ico` e o `.syso` de volta por código independente do que os escreve (o `debug/pe` da
   biblioteca padrão e um percurso da árvore de recursos).
6. **Não verificado.** (a) **O ISCC aceita o `.ico`** com a camada de 256 px em PNG (era pendência
   enquanto se achava que o ISCC não estava na máquina; está em `%LOCALAPPDATA%\Programs\Inno Setup
   6`, e o Mauro compilou o Setup com ele). Prova de 01-out-2026: um `.iss` mínimo, numa pasta de
   rascunho, com `SetupIconFile` apontando para o `installer/aurora-whisper.ico`, compilou sem erro,
   e o ISCC acusou "Updating icons (Setup.exe)" (a revisão fez a prova, e foi repetida; o executável
   gerado **não foi executado** e foi apagado). O `System.Drawing` não carrega a camada de 256 px,
   mas é limite dele. (b) O script de montagem **não confere** que o ícone entrou no executável
   (poderia ler o diretório de recursos do PE); fica como possível melhoria. (c) O `.syso` leva só o
   ícone: não leva o `VERSIONINFO` (a aba "Detalhes" do executável continua sem versão) nem manifesto.
7. **Mutações** numa cópia isolada (`git archive`, diff conferido, `-timeout 90s`, restauração
   conferida): **25 mutantes, todos mortos**. No desenho: cantos do ladrilho retos, ladrilho de outra
   cor ou ausente, a bandeja no tema claro ou pausada, e a cor verde da bandeja trocada (o arquivo
   fica velho). No `.ico`: altura sem a máscara, deslocamento errado, máscara invertida, PNG em
   64 px, largura 255 no lugar de 0. No `.syso`: sem as relocações, tipo de relocação, `nID` do
   grupo, flags da seção, máquina i386, entrada do grupo apontando errado, arquivo removido ou com a
   máquina trocada; e o gerador escrevendo o `.syso` onde devia ser o `.ico`. No `.iss`:
   `SetupIconFile` removido, inexistente ou absoluto, e
   `UninstallDisplayIcon` apontando para outro arquivo (cada um pego também pelo teste novo, sem o
   golden). Um `.ico` sem a camada de 256 px.

### Verificação da leva de 01-out-2026 (F2, ícone, pílula e roteiros)

`go test ./... -count=1`: **4121 testes e subtestes passando, nenhum falhando, 5 pulados** (os de
sempre, que dependem de dado, de modelo ou de rede), em 56 pacotes; `go vet ./...` e `gofmt -l .`
limpos; `GOOS=linux go vet` limpo nos pacotes puros (`internal/asr`, `overlay`, `win32`, `tray`,
`cmd/gerarico`, `cmd/asrstage`, `cmd/agent`). Funções de teste novas: 32 (F2 e o servidor de
mentira, 13; a pílula, 7; o gerador do ícone, 11; o `.iss`, 1). **Mutações:** 59 mutantes (16 na F2,
18 na pílula, 25 no ícone), todos mortos, em cópias isoladas com o diff e a restauração conferidos.
Nenhum Agent, Setup ou `whisper-server.exe` foi executado; nenhum teste toca em `bin\`, no
`%LOCALAPPDATA%` real, no registro ou na área de transferência (o único processo filho dos testes
novos é o `cmd /c cd` e o próprio binário de teste como servidor de mentira).

## Revisão

Depois do primeiro Setup instalado numa conta limpa: o tamanho real do Setup; quanto levou a
primeira subida com disco frio; se o SmartScreen, o Smart App Control ou algum antivírus
barraram; se o `whisper-server` carregou o modelo num perfil com acento. Com dado, reabrir: modelo
embutido ou baixado, `Program Files` ou por usuário, e a assinatura.
