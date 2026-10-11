# Aurora Whisper

Ditado por voz local para Windows 10/11 x64, inspirado no Wispr Flow (sem ligação com a empresa).
Você segura uma tecla (por padrão `Ctrl+Win`), fala e solta: o Aurora Whisper transcreve na sua
máquina, com o [whisper.cpp](https://github.com/ggml-org/whisper.cpp), e insere o texto no campo
que tem o foco do teclado. O ícone fica na bandeja, e um painel local mostra o histórico dos
ditados e as configurações.

**Estado.** O Agent funciona de ponta a ponta sem nuvem: captura, transcrição na sua máquina,
dicionário, inserção, bandeja, overlay e painel. A nuvem é opcional e serve para ver e editar o
histórico no celular e nos outros PCs; o projeto não tem servidor central, e cada pessoa sobe a sua,
num servidor seu (o código está em `cmd/server`, o deploy em `deploy/` e o roteiro em
`docs/deploy-f11.md`; o desenho, na ADR-019). **Ditar pelo celular** existe desde 11-out-2026
(ADR-021): no app da nuvem, com o provedor e a chave de quem dita. Ainda não há Setup publicado: falta abrir o portão do
workflow de release e publicar o release do modelo (veja "Montar o Setup"). Quem quer usar hoje
monta do código.

## Requisitos

- Windows 10 ou 11, x64. A transcrição usa só a CPU; o Agent mantém cerca de 365 MB de memória.
  O tempo de transcrição (mediana de 565 ms) foi medido num PC de 20 threads: num mais fraco, será
  maior, e o requisito mínimo de CPU **não foi medido**.
- Para só **instalar**: nada além do Windows (veja "Instalar").
- Para **montar do código**: [Go](https://go.dev/dl/) 1.27 ou mais novo (`go.mod`),
  [Node.js](https://nodejs.org/) 24 e [pnpm](https://pnpm.io/) 11 (só para construir o painel), e o
  whisper.cpp e o modelo do Whisper, que **não estão no repositório** (passo 1 abaixo).

## Instalar

> **Ainda não há versão publicada.** O Setup abaixo é gerado pelo repositório (veja "Montar o
> Setup"), mas nenhum foi liberado: falta abrir o portão do workflow de release e publicar o release
> do modelo. Quem quer usar hoje monta do código, na seção seguinte.

1. **Baixe** o `AuroraWhisper-Setup-<versão>.exe` na página de
   [Releases](https://github.com/MauroJulianiJunior/aurora-whisper/releases). O arquivo `.sha256` ao
   lado traz o hash para conferir. O Setup leva o modelo de voz (cerca de 140 MB): funciona sem
   internet.
2. **Execute.** As versões 0.x são **prévias, sem assinatura digital**, então o Windows mostra
   "O Windows protegeu o computador" (SmartScreen): clique em **Mais informações** e depois em
   **Executar assim mesmo**. Num Windows 11 com o Smart App Control ligado, o bloqueio pode não
   deixar essa opção (não foi testado). O Setup **não pede administrador**, com uma exceção: se
   faltar o runtime do Visual C++ (x64, versão 14.51 ou mais nova), ele o baixa do site da
   Microsoft, confere a assinatura e o executa, e o Windows pede permissão uma vez. É a única
   conexão de rede do instalador.
3. **Use.** O Aurora Whisper aparece no Menu Iniciar e abre na bandeja (ao terminar, o Setup oferece
   abri-lo). Ele **não liga com o Windows**: para isso, use o item "Iniciar com o Windows" do menu da
   bandeja. Segure `Ctrl+Win`, fale e solte. O dicionário começa vazio: o painel o preenche quando
   você corrige um ditado e clica em **Aprender**.
4. **Onde ficam as coisas.** O programa, em `%LOCALAPPDATA%\Programs\Aurora Whisper`. Os seus dados
   (histórico, dicionário, preferências e logs), em `%LOCALAPPDATA%\AuroraWhisper`, que só a sua
   conta e os administradores leem. As chaves de API do Polish ficam no Gerenciador de Credenciais
   do Windows.
5. **Atualizar.** Baixe o Setup novo e execute por cima: ele fecha o Agent, troca o programa e não
   mexe nos dados.
6. **Desinstalar.** Em Configurações > Aplicativos. O desinstalador fecha o Agent e **pergunta se
   apaga também** o histórico, o dicionário, os logs e as chaves de API; o padrão é **não**. Se você
   só vai reinstalar, deixe os dados.

## Como montar

Para desenvolver, ou para usar sem Setup: monta o `bin/`, o ambiente de desenvolvimento, que guarda
os dados ao lado do executável (modo portátil).

Todos os comandos rodam a partir da raiz do repositório, um por bloco: o PowerShell 5.1 não tem
`&&`. Funcionam no PowerShell e no bash.

### 1. O reconhecimento de voz (whisper.cpp e o modelo)

O Agent procura o servidor de transcrição e o modelo **ao lado do próprio executável**, em `bin/`.
O `cmd/asrstage` monta essa pasta, mas **não baixa nada**: ele só copia o que você já colocou em
`thirdparty/` e `models/` (as duas pastas estão no `.gitignore`).

1. **whisper.cpp.** Baixe o `whisper-bin-x64.zip` da tag de build `b5130` nas
   [releases do whisper.cpp](https://github.com/ggml-org/whisper.cpp/releases) (a tag das medições;
   as tags semver não trazem esse arquivo) e extraia de modo que exista
   `thirdparty/whisper/Release/whisper-server.exe`, com as DLLs (`whisper.dll`, `ggml*.dll`) ao
   lado.
2. **Modelo.** Baixe o `ggml-small.bin` (Whisper small, formato ggml, cerca de 465 MB) do
   [repositório de modelos do whisper.cpp](https://huggingface.co/ggerganov/whisper.cpp) para
   `models/ggml-small.bin`.
3. **Quantização.** O modelo usado é o `small` em `q4_0` (cerca de 139 MB), que não existe pronto
   para baixar. Gere-o com o `whisper-quantize.exe` do whisper.cpp (aqui, o do pacote do passo 1;
   se o seu pacote não o traz, compile-o do código-fonte). Crie antes a pasta `models/quant/`:

   ```bash
   thirdparty/whisper/Release/whisper-quantize.exe models/ggml-small.bin models/quant/small-q4_0.bin q4_0
   ```

4. **Montar `bin/`.** Copia o servidor, as DLLs, o modelo (com o nome que o Agent procura) e o
   dicionário inicial (`testdata/asr-dataset/dicionario.json`, opcional), e **confere o checksum do
   modelo**:

   ```bash
   go run ./cmd/asrstage
   ```

> **O dicionário do `bin/` fica.** O dicionário do repositório só é copiado se `bin/dicionario.json`
> ainda não existe: o que o painel aprendeu fica. Para voltar ao padrão, apague o arquivo e rode o
> passo 4 de novo. Um `bin/dicionario.json` vazio ou quebrado não é sobrescrito nem mantido em
> silêncio: a montagem falha e diz como seguir. O `dictlearn` grava, por padrão, no dicionário do
> **repositório** (`testdata/asr-dataset/dicionario.json`), e não em `bin/`; para ensinar ao
> dicionário que o Agent usa, rode-o com `-dict bin/dicionario.json`. Se o do repositório tem termo
> ou variante que o de `bin/` não tem, a montagem avisa quantos. Voltar a um Agent anterior ao
> aprendizado do painel, depois de ele ter criado um termo, pede cuidado: o Agent antigo recusa o
> campo `origem` do arquivo. Desfaça o que o painel aprendeu antes, ou apague esse campo à mão.

> **Se o passo 4 disser que o modelo não confere:** o Agent só aceita o arquivo exato que foi
> medido (tamanho e SHA-256 em `SmallQ4_0`, `internal/asr/model/model.go`), e quantizar noutra
> versão do whisper.cpp pode gerar bytes diferentes. Com o mesmo `b5130` e o `ggml-small.bin`
> oficial o resultado deve bater; se ainda assim não bater, ajuste o `Bytes` e o `SHA256` dessa
> variável para o seu arquivo e refaça o passo 4.

### 2. O Agent

O `-H=windowsgui` tira a janela de console (o Agent é aplicativo de bandeja), e o `-o` põe o
executável em `bin/`, ao lado do que o passo 1 montou:

```bash
go build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

> **Não use `go build ./cmd/agent` sem o `-o`.** Sem ele, o Go grava um `agent.exe` na pasta em
> que o comando rodou, longe do modelo, e esse executável abre direto a janela "O Aurora Whisper
> foi encerrado por um erro", com "modelo de ASR" no erro.

### 3. O painel

O build vai para `bin/painel`, a pasta que o Agent serve ao lado do executável (`web/vite.config.ts`):

```bash
pnpm -C web install
```

```bash
pnpm -C web build
```

### 4. Subir

O ícone aparece na bandeja depois que o modelo carrega. O log fica em `bin\agent.log`. Para sair,
use o menu do ícone, **Sair**.

```bash
./bin/aurora-whisper-agent.exe
```

Segure `Ctrl+Win`, fale, solte. O roteiro de teste manual da bandeja e do painel está em
`docs/teste-manual-bandeja-e-painel.md`; o do ditado, em `docs/sprint-4-teste-manual.md`.

## Privacidade

- **Áudio do PC.** Só existe na memória, durante a transcrição. Nunca é gravado em disco nem enviado
  a ninguém; o servidor de transcrição roda na sua máquina, em loopback. Isso vale para o PC, conectado
  ou não a uma nuvem. **O celular é outro caso, ainda em desenho** (veja "Ditar no celular", abaixo).
- **Contexto de outras aplicações** (janela, campo em foco, texto próximo) é efêmero: vive na
  memória enquanto o texto é resolvido, não vai para o log nem é sincronizado. Campos de senha
  são bloqueados.
- **Histórico e dicionário** ficam no seu disco: o histórico num SQLite local (`historico.db`) e o
  dicionário em `dicionario.json`. Instalado, em `%LOCALAPPDATA%\AuroraWhisper`; no `bin/` de
  desenvolvimento, ao lado do executável. A lixeira sai de vez em 30 dias; num PC que já foi ligado
  a uma nuvem, a que a nuvem ainda não soube espera por ela, no máximo até o ditado completar 6
  meses. O resto do histórico
  fica até você ligar os **6 meses** no bloco **Histórico** das Configurações: o painel mostra antes
  quantos ditados sairiam, o Agent copia o banco ao lado dele e só então apaga; daí em diante, o que
  completa 6 meses sai sozinho, até você desligar. As cópias do banco ao lado dele (de antes do dono
  fixo, do envio à nuvem e dos 6 meses) guardam o texto inteiro e só saem pelo botão **Apagar as
  cópias**, no mesmo bloco.
- **Polish** (opcional, desligado por padrão, nas Configurações) organiza a pontuação e os
  parágrafos do que você ditou chamando a OpenAI ou a Anthropic **com a sua chave de API**, guardada
  no Gerenciador de Credenciais do Windows. **Sem a nuvem conectada**, é o único caso em que o texto
  ditado sai da máquina.
- **Nuvem** (opcional e desligada por padrão; cada pessoa sobe a sua, veja "Estado"). Só depois que
  você conecta o PC a uma nuvem, o texto dos ditados e as trocas que você aprendeu no painel (o
  botão **Aprender**) passam a ir para lá, **legíveis para quem administra o servidor**, para
  aparecer no celular e nos outros PCs. O que já estava no PC antes (o histórico e as trocas
  aprendidas) só sobe quando você clica em **Enviar o histórico**. O áudio do PC e o que está nas outras
  janelas nunca vão. Sem conectar, o Agent não abre conexão nenhuma. Quem hospeda uma nuvem para
  outras pessoas tem um modelo de política de privacidade em `docs/politica-de-privacidade.md` e o
  dossiê de LGPD em `docs/lgpd-dossie.md`. O desenho está na ADR-019.
- **Ditar no celular** (opcional, ADR-021, no código desde 11-out-2026). No app do celular, a voz vai ao servidor da
  nuvem **de quem hospeda** e dele ao provedor de transcrição que você escolher, **OpenAI ou Groq**,
  **com a sua chave de API**. Isso muda a regra do PC, e por isso começa com um **aceite específico**
  seu, antes do microfone abrir:
  - **a voz** só fica na memória, durante o pedido, e é descartada ao fim; não é gravada em disco,
    nem no aparelho, nem no servidor. O que o provedor guarda depende da documentação e da conta dele
    (por exemplo, registros por até 30 dias para investigar falha ou abuso); o Aurora Whisper não
    controla isso. Nenhum dos dois provedores diz processar no Brasil, então o envio é tratado como
    transferência internacional;
  - **a sua chave de API** fica no servidor, **cifrada**; o app só mostra o fim dela. Quem tem acesso de
    administrador (a raiz) ao servidor lê o arquivo da chave de cifra e o banco, e decifra. Use uma
    chave só para o celular, com limite de gasto no provedor. A chave do PC continua só no PC;
  - **a dica** (termos que você ensinou, enviados junto com a voz) começa **desligada**;
  - **o Polish no celular** começa desligado e, ligado, chama o provedor do Polish pelo servidor;
  - **a voz e a dica podem levar dado de outras pessoas**, como dado de cliente. Isso é decisão sua:
    veja se cabe nas suas obrigações com elas antes de ligar;
  - no passo 1 não há ditado com a tela apagada nem em segundo plano.
  O detalhe e os prazos de cada provedor estão em `docs/politica-de-privacidade.md` e
  `docs/lgpd-dossie.md`; o desenho, na ADR-021.
- A API local só escuta em `127.0.0.1`.

Os detalhes estão em `docs/ai-spec/09-security-privacy.md`.

## Desenvolvimento

Para o ambiente, copie `.env.example` para `.env` (as variáveis são do servidor da nuvem, para
rodá-lo num PC de desenvolvimento). Os testes da suíte normal nunca injetam entrada no sistema
(`SendInput`, clipboard, hooks globais): as regras estão em `CLAUDE.md`.

```bash
go test ./...
```

```bash
go vet ./...
```

```bash
pnpm -C web lint
```

```bash
pnpm -C web test
```

Para só conferir que tudo compila (Agent e backend), sem gravar executável nenhum:

```bash
go build ./...
```

### Montar o Setup

O instalador é do [Inno Setup](https://jrsoftware.org/isinfo.php) 6 (`installer/aurora-whisper.iss`).
O script monta a pasta de staging em `dist/staging` (o servidor de voz, o modelo, o Agent compilado
com `-trimpath` e a versão, o painel e as licenças; **sem** dicionário) e compila o Setup em `dist/`.
Ele precisa do que o passo 1 acima põe em `thirdparty/` e `models/`, e do `ISCC.exe` do Inno Setup;
sem o `ISCC.exe`, só monta o staging. Nunca executa o Agent nem o Setup.

```powershell
powershell -File scripts\montar-instalador.ps1 -Versao 0.1.0
```

O roteiro para testar o Setup de verdade, numa conta de teste do Windows, está em
`docs/teste-manual-instalador.md`.

A cada tag `v*` o workflow `.github/workflows/release.yml` faz isso e cria um release **rascunho**;
publicar é sempre à mão. Por ora o workflow **falha de propósito** no primeiro passo (o portão
`F2_PRONTA` está fechado: a F2 da ADR já está feita, e o portão só abre quando o mantenedor
conferir o servidor de voz de verdade) e, depois dele, no passo do modelo (o release que o guarda
ainda não existe). As decisões estão em `docs/adr/ADR-018-instalador.md`.

### Layout

```
.
├── cmd/
│   ├── agent/            Windows Agent (Go)
│   ├── asrstage/         Monta bin/ (ou a pasta do pacote) com o servidor de ASR e o modelo
│   ├── gerarico/         Gera o ícone do programa (.ico do Setup e recurso do executável)
│   └── server/           Servidor da nuvem (serve, migrate e a nuvem de teste)
├── internal/             Domínio e módulos compartilhados (Go)
├── installer/            Script do instalador (Inno Setup)
├── web/                  Painel (React + TypeScript + Vite)
├── migrations/           Migrations PostgreSQL
├── deploy/               O deploy da nuvem: as imagens, o compose e os scripts da VPS
├── scripts/              Automações, montagem do instalador e harness de benchmark
├── testdata/             Fixtures e datasets de teste
└── docs/
    ├── ai-spec/          Especificação completa (comece por 00-index.md)
    ├── adr/              Architecture Decision Records
    └── marca/            Símbolo e ilustração
```

Como contribuir (issues e pull requests em português, os testes e o padrão de commit) está em
`CONTRIBUTING.md`; como relatar uma falha de segurança, em `SECURITY.md`. As regras para agentes de
código estão em `AGENTS.md` e `CLAUDE.md`. A especificação começa em `docs/ai-spec/00-index.md`; os
critérios de aceitação estão em `docs/ai-spec/03-acceptance-criteria.md`.

## Licença

Código aberto sob a licença **MIT** (`LICENSE`): use, modifique e distribua à vontade, mantendo o
aviso de copyright. Isso inclui as marcas: o símbolo (`docs/marca/pico-estourado/`) e a ilustração
da Aurora (`docs/marca/retrato/`) seguem a MIT. Os componentes de terceiros e as licenças deles
estão em `THIRD_PARTY_NOTICES.md`.

O Aurora Whisper não é afiliado nem endossado pela OpenAI (o Whisper é o modelo de voz que ele usa,
pelo whisper.cpp) nem pela Wispr.
