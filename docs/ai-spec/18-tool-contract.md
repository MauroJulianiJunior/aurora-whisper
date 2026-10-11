# Tool / Component Contracts

## AudioSource
```go
type AudioSource interface {
    Start(ctx context.Context, cfg AudioConfig) (AudioSession, error)
}
type AudioSession interface {
    Frames() <-chan AudioFrame
    Stop(ctx context.Context) (AudioBuffer, error)
    Abort()
}
```

Invariante: sem persistência de áudio.

## VAD
```go
type VoiceActivityDetector interface {
    Push(frame AudioFrame) VADState
    Summary() VADSummary
}
```

## ASRProvider
```go
type ASRProvider interface {
    ID() string
    Capabilities() ASRCapabilities
    Warmup(context.Context) error
    Transcribe(context.Context, AudioBuffer, ASROptions) (ASRResult, error)
}
```

Capabilities:
- streaming;
- confidence;
- timestamps;
- nbest;
- languages;
- acceleration;
- vocabulary (o provider usa `ASROptions.Vocabulary`);
- formatos (o áudio comprimido que o provider aceita; vazio é "só PCM").

### AudioBuffer: PCM ou áudio comprimido
ADR-021 (10-out-2026), decisão C. O `AudioBuffer` ganhou o campo opcional `Comprimido`, e a
`ASRCapabilities` ganhou `Formatos`. Nada mais do contrato mudou, e o Agent compila e testa sem
mudança.

```go
// internal/asr
type AudioBuffer struct {
    PCM          []int16    // mono, 16 bits
    SampleRateHz int
    Comprimido   Comprimido // opcional: o valor zero é "este buffer é PCM"
    Release      func()
}
type Comprimido struct {
    Bytes []byte // o áudio codificado, como chegou
    Tipo  string // o tipo de mídia, sem parâmetros: "audio/webm"
}
type ASRCapabilities struct {
    // ... os campos de antes ...
    Formatos []string // tipos de áudio comprimido aceitos; vazio: só PCM
}
func (b AudioBuffer) Validar() error                  // ErrAudioInvalido
func (c ASRCapabilities) AceitaFormato(tipo string) bool
func NormalizarTipo(tipo string) string               // "Audio/WebM; codecs=opus" -> "audio/webm"
```

- **Cada buffer leva PCM OU o comprimido, nunca os dois.** `Validar` recusa PCM junto de
  comprimido e um comprimido sem bytes ou sem tipo (`ErrAudioInvalido`, mensagem fixa: nunca leva os
  bytes). Um buffer sem nada continua válido aqui; quem decide o que fazer com ele é o provider.
- **Ninguém decodifica o comprimido.** Os bytes vão como estão a quem os aceita. O `Comprimido.Bytes`
  é compartilhado entre as cópias do `AudioBuffer`, como o `PCM`, e nenhum provider o altera, o grava
  em disco ou o loga. O `Release` continua sendo do dono do buffer, nunca do provider.
- **O provider local não declara nenhum formato e recusa o comprimido** com
  `asr.ErrComprimidoNaoSuportado`, antes de subir o servidor. O Agent não muda.
- **O provider da nuvem só aceita comprimido** e recusa o PCM com `asr.ErrPCMNaoSuportado`: não
  codifica nada. Um tipo fora do `Formatos` dele também é `ErrComprimidoNaoSuportado`.
- O `Engine` não mudou: um buffer comprimido atravessa as policies como o PCM (o local o recusa e o
  cloud, que declara o formato, o aceita).

### Os provedores da nuvem (`internal/asr/nuvem`)
ADR-021, decisão D. OpenAI e Groq, por trás do mesmo `ASRProvider`, com a chave de quem dita
(BYOK). As duas APIs têm a mesma forma (`POST .../audio/transcriptions`, multipart) e moram num
pacote só, que depende apenas de `internal/asr`. Sem troca automática de provedor: se o escolhido
falha, a chamada falha.

```go
// internal/asr/nuvem
type Provider struct { // implementa asr.ASRProvider; ID() é "openai" ou "groq"
    Cliente  *Cliente // nil: as APIs oficiais
    Provedor string   // OpenAI | Groq
    Modelo   string   // vazio: ModeloPadrao(Provedor)
    Idioma   string   // vazio: "pt"
    Chave    func(ctx context.Context) (string, error) // lida a CADA chamada
}
func (c *Cliente) Transcrever(ctx, Chamada{Provedor, Modelo, Idioma, Dica}, chave string, asr.Comprimido) (asr.ASRResult, error)
func (c *Cliente) ListarModelos(ctx, provedor, chave string) ([]string, error) // só os de transcrição
```

- **O que vai no pedido.** `Authorization: Bearer <chave>` e um multipart com `model`, o idioma
  (`language`; no `gpt-transcribe` o campo é `languages`), o `prompt` (a dica, que é
  `ASROptions.Vocabulary`; sem dica o campo nem vai), `temperature` `0`, `response_format`
  (`verbose_json` nos modelos whisper, que devolvem segmentos; `json` nos `gpt-*-transcribe`) e, por
  último, o arquivo (`ditado.<ext>`, com o tipo sem parâmetros). `opts.Language` `pt-BR` vira o
  código `pt`; sem idioma, `pt`. A tabela do que cada modelo aceita é mínima (`perfilDoModelo`) e se
  confere numa chamada de teste.
- **Modelos padrão** (constantes trocáveis, escolhas menores 1 e 2): OpenAI `gpt-transcribe`
  (alternativa `gpt-4o-mini-transcribe`); Groq `whisper-large-v3-turbo`. A pessoa escolhe outro da
  lista do provedor: `ListarModelos` lê `GET /models` e fica só com os ids que trazem `transcribe`
  ou `whisper`, sem os de diarização e sem os só de inglês.
- **Formatos aceitos:** webm, mp3 (`audio/mpeg`), m4a (`audio/mp4`) e wav nos dois; ogg e flac só na
  Groq. Até 25 MB (`TetoArquivo`), conferido antes de enviar.
- **O corpo tem tamanho conhecido.** O cabeçalho das partes, os bytes recebidos e o fecho vão juntos
  por um `io.MultiReader`, com o `Content-Length` calculado: **nunca `chunked`**, e sem segunda cópia
  do áudio. O pedido não se reenvia sozinho (nem por redirecionamento nem por conexão velha), então o
  transporte nunca cobra um ditado duas vezes.
- **Falhas.** Todo erro do provedor é um `*Erro{Motivo, Provedor, Mensagem}` com mensagem fixa em
  português, de uma lista fechada: `sem_chave`, `chave_recusada` (401, ou chave com caractere que não
  cabe num cabeçalho), `sem_permissao` (403), `arquivo_grande` (413, ou acima do teto), `limite`
  (429), `indisponivel` (5xx), `tempo_esgotado`, `sem_rede` e `resposta_inesperada` (qualquer outro
  status, inclusive 3xx, e um corpo que não se lê). **O corpo de erro do provedor nunca é lido nem
  repassado** (a OpenAI devolve um pedaço da chave no 401), o erro de origem não é embrulhado (carrega
  a URL; a única causa embrulhada é `context.DeadlineExceeded`), **redirecionamento não é seguido**
  e só HTTP/1.1 (o HTTP/2 do `net/http` escreve cabeçalho e corpo no log sob `GODEBUG=http2debug`).
  Cancelar o `ctx` volta `context.Canceled` puro. O prazo é `TimeoutPadrao` (20 s) ou o do `ctx`, o
  mais curto.
- **Sem fala.** Segmentos com `no_speech_prob` acima de 0,6 **e** `avg_logprob` abaixo de -1,0
  (`LimiteSemFala`, `LimiteLogprob`; estritos, e só valem com os dois campos presentes) saem do
  texto, que é refeito dos segmentos que ficaram. Sem segmento descartado, vale o texto do provedor.
  Texto vazio no fim é `ErrSemFala` (e não um `*Erro`): o provedor atendeu, e pode ter cobrado.
  **Esse filtro roda depois da cobrança:** evita o ditado vazio, não o custo. Os modelos
  `gpt-*-transcribe` não devolvem segmentos; neles vale só a medição do app.
- **`Capabilities`.** `Confidence` é `false` (o provedor não dá confiança do texto; o
  `no_speech_prob` serve só ao filtro), `Vocabulary` é `true`, `Timestamps` vale nos modelos
  whisper, e `Formatos` traz a lista acima.
- **Invariantes (testados):** a chave só vai no cabeçalho; áudio, chave e corpo de erro não aparecem
  em erro nem em log (o pacote nem importa `log`, `slog` nem `os`); nenhum arquivo é criado; a suíte
  inteira roda contra `httptest`, com uma trava que reprova qualquer pedido para fora da máquina.

## ContextProvider
```go
type ContextProvider interface {
    Snapshot(ctx context.Context, target FocusTarget) (ContextSnapshot, error)
}
```
ContextSnapshot não deve implementar persistence.

## DictionaryRepository
```go
type DictionaryRepository interface {
    Search(context.Context, DictionaryQuery) ([]DictionaryCandidate, error)
    Upsert(context.Context, DictionaryEntry) error
}
```

## Sugestão de troca de termo (`internal/learning.Sugerir`)
Etapa 3, ADR-017 (30-set-2026). **Implementado (F1).** Uma função **pura**, sem efeito colateral:

```go
// internal/learning
func Sugerir(bruto, antes, depois string, dic dictionary.Semente, contar ContadorOcorrencias) Resultado

type Resultado struct {
    Sugestoes []Sugestao // de 0 a MaxSugestoes (2), na ordem do texto
    Descartes []Motivo   // código fechado por troca descartada, nunca o texto
}
type Sugestao struct {
    De, Para, TermoExistente string
    OcorrenciasNoHistorico   int
}
```

- Recebe o `raw_text` do ditado (`bruto`), o texto final **de antes** da edição (`antes`) e o
  salvo (`depois`). Acha vários trechos por edição e compara sem maiúscula, acento e pontuação.
  `De` é a forma lida do **bruto** (a que o ASR escreveu e que um alias precisa ter para valer no
  próximo ditado); `Para` é a forma final, e a canônica do dicionário se o termo já existe.
- `contar` é opcional: injetado por quem chama, e diz em quantos ditados do histórico o `De`
  aparece (`learning.OcorrenciasEm` sobre os brutos). Não lê histórico por conta própria.
- **Nunca grava.** Uma sugestão ignorada não deixa rastro (AC-022). O `Classify` e o
  `cmd/dictlearn` ficam como estão.
- **Invariante:** nada do aprendizado roda sem o clique. Quem chama é a API local, em duas rotas
  separadas: salvar a edição só **sugere** (`PATCH /api/history/{id}`), e `POST /api/aprendizado`
  grava, depois de refazer as checagens contra o bruto do ditado (`06-api-contract.md`). O clique é
  no PC de origem: desde a F8, os outros PCs aplicam o par que chega pelo sync, sem clique lá, com as
  mesmas checagens do Aprender (ADR-019, "Decisões da F8", D2).

## Aprendizado (variante aprendida pelo painel)
Etapa 3, decidida pelo Mauro em 30-set-2026 (aprende só com um clique; guarda no
`dicionario.json` com a origem `painel`; acento ou maiúscula sozinhos não se aprendem; vale em
qualquer aplicativo). Código em `internal/dictionary` (`painel.go`, `arquivo.go`), em
`internal/agent/aprendiz` (o `Aplicador`: a trava, as conferências do Agent e o aplicador do
aprendido que chega da nuvem; desde a F8g, ADR-019, "O estado da F8g") e no binário do Agent
(`cmd/agent/aprendizado.go`, só a troca no runtime do Windows). A API local do painel (F3,
`internal/agent/localapi`) chama só a interface, acrescida de uma leitura do dicionário de agora:

```go
// internal/agent/localapi
type Aprendizado interface {
    dictionary.Aprendizado
    // O dicionário como está em disco agora, para learning.Sugerir saber o que já existe.
    Dicionario(ctx context.Context) (dictionary.Semente, error)
}
```

`Dicionario` é `dictionary.Arquivo.Atual` (relê o arquivo, sem tocá-lo); sem dicionário na subida
devolve `ErrSemArquivo`, e a edição sai sem sugestão. (O Agent cria um dicionário vazio na subida,
só se não houver nenhum, com `dictionary.CriarSeFaltar`: o caso "sem dicionário" é o de a criação
ter falhado. ADR-018.) A API só chama `Aprender` depois do clique e de
refazer as checagens (`06-api-contract.md`, "Aprendizado"); salvar uma edição só chama `Dicionario`.

```go
// internal/dictionary
type Aprendida struct{ Termo, Variante string }

type Aprendizado interface {
    Aprender(ctx context.Context, termo, variante string) (Aprendida, error)
    Desfazer(ctx context.Context, variante string) (Aprendida, error)
    Aprendidas(ctx context.Context) ([]Aprendida, error) // nunca nil; sem dicionário, vazia
}
```

**Única exceção à regra "erro = nada gravado":** `ErrGravadoSemTroca`. O arquivo foi gravado
(`Aprender` ou `Desfazer` valeram) e só a troca do dicionário em memória falhou, e a mudança vale
depois de reiniciar o Agent. O erro vem **com o `Aprendida` preenchido**, e quem chama o trata como
sucesso com uma ressalva (a API local responde `200` com `vale_ao_reiniciar`). A causa não vai no erro.

- `Aprender` acrescenta a variante ao termo, que se acha sem distinguir maiúscula, acento nem
  espaço (`ecf` cai em `ECF`). Se o termo não existe, nasce com prioridade 50 (`Origem:
  "painel"`), sem domínio. A variante vai para `alias` e para `alias_origem` com o valor `painel`.
  Uma variante que já pertence a outro termo, ou que é outro termo, é recusada
  (`ErrVarianteDeOutroTermo`, com o dono em `*ErroVarianteDeOutroTermo`): aceitá-la apagaria a
  regra anterior sem avisar. O mesmo vale para o **termo novo** que já é variante de outro termo
  (`ErrTermoEVarianteDeOutro`, `*ErroTermoEVarianteDeOutro`): o termo novo casa exato e sombrearia
  o alias do outro. Uma variante de mais de 4 palavras (`MaxPalavrasVariante`, o que o Smart sabe
  procurar) é recusada (`ErrInvalido`): seria gravada e nunca agiria.
- **O termo que o painel cria age só pela variante** (`DictionaryEntry.AliasOnly`, vinda de
  `Origem: "painel"`). A pessoa confirmou a variante, não o termo: sem isso, criar `esta` para
  aprender `ista` faria o match exato reescrever "está" para "esta" em todo texto (acento sozinho
  não se aprende, decisão 3). O Search ainda **acha** o termo escrito, mas o candidato sai com
  `AliasOnly` (`DictionaryCandidate.SoPresenca`) e o **Smart o ignora por inteiro**, num único
  ponto (`searchTop`): não reescreve, não disputa trecho com outro termo (o casamento exato dele
  não desliga o alias de frase de outro termo que o contém, como "Cloud Code" com o termo `Code`
  do painel) e não passa pelo N-best. A entrada não gera casamento fonético e **não entra no
  vocabulário do ASR** (`VocabularioTermos`), nem num dicionário pequeno. O Polish só protege o
  termo no ditado em que o Smart o escreveu no lugar da variante (`comTermosTrocados`): aprender
  `e` ou `ponto` não trava as listas e a pontuação falada dos outros ditados. A proteção é por
  termo (todas as ocorrências dele naquele texto), não só pela ocorrência trocada. Apagar `origem`
  do termo no arquivo é como a pessoa o adota: ele passa a valer como qualquer termo.
- **A variante tem de agir** (`ErrNaoAgiria`): antes de gravar, o Agent roda o Smart da semente
  nova sobre a variante sozinha (`Arquivo.ValidarAprendida`) e recusa, sem gravar, se não sair o
  termo. A conferência roda onde o termo vive: no domínio **desconhecido** para o termo sem domínio
  e para o termo novo; no desconhecido **e** no domínio do termo quando ele tem domínio e não é
  restrito (aparece nos dois, e a variante tem de agir nos dois); só no domínio do termo quando ele
  é `restrito: true` (só age lá; restrito sem domínio nunca age e é recusado). Cobre o que
  "aprendido" mentiria: outro termo de prioridade maior com casamento fonético fraco leva a busca
  (a prioridade vem antes do casamento), ou uma palavra da variante já é termo e veta a frase
  ("pedro das", com o termo DAS). Esses dois são comportamentos do Smart anteriores ao painel e não
  foram mudados. A tela deve traduzir o erro por `errors.Is`.
- Ao trocar uma variante, o Smart mantém a **inicial maiúscula** do que foi dito quando o termo
  é do painel e está **toda** em minúscula e sem dígito: "Transcrição do áudio" vira "Descrição do
  áudio", não "descrição do áudio" (`formaDoAlias`). A marca com grafia própria (`iFood`, `n8n`,
  `iPhone`, `macOS`, `eSocial`) não entra na regra: fica como foi gravada, inclusive abrindo a
  frase. A mesma regra vale no ramo da alternativa do ASR corroborada pelo contexto (a palavra
  que sai é a da hipótese principal): "Xpto do áudio" com a alternativa "Transcrição do áudio" e o
  contexto "a descrição" vira "Descrição do áudio". Por isso a F3 deve passar o termo **na caixa
  de meio de frase** ("descrição"), não a que a edição trouxe no começo da frase ("Descrição"): com
  a inicial maiúscula no termo, "a transcrição" viraria "a Descrição".
- O `ErrNaoCarrega` (a validação do dicionário em memória recusou) e os erros de regra têm
  **mensagem fixa**: a causa fica em `Unwrap` e o dono em campo, nunca no texto. Quem trata o
  erro não loga `Dono`, que é um termo do dicionário.
- `Desfazer` tira uma variante de origem `painel`, e só ela (`ErrNaoAprendida` para o que a pessoa
  ou o `dictlearn` cadastraram). O termo que o painel criou sai junto, se ficou vazio e intocado.
  Sem mais nada ter mudado, o arquivo volta **idêntico, byte a byte**, ao de antes (vale para um
  arquivo no formato que `Semente.Gravar` escreve).
- Cada operação **relê** o `dicionario.json`, muda e grava de forma atômica (temporário + rename):
  o `dictlearn` pode ter gravado no meio. O `dictlearn` também relê o arquivo **depois** da
  pergunta "Confirma?" e grava sobre o que achou, para um clique do painel durante a espera não
  sumir do disco. Arquivo ausente (`ErrSemArquivo`), inválido
  (`ErrArquivoInvalido`, inclusive com qualquer conteúdo depois do objeto JSON) ou de outro
  usuário (`ErrUsuarioDiferente`) **nunca é tocado**. Se outro processo (antivírus, indexador)
  prende o arquivo por um instante, o rename é repetido por até ~300 ms antes de falhar; a falha
  é segura (o arquivo fica como estava, sem temporário).
- Gravado o arquivo, o Agent troca o dicionário em memória sem reiniciar
  (`winruntime.Runtime.TrocarDicionario`): o Smart, o domínio do app e as guardas do Polish são
  trocados **juntos**, a partir do **próximo ditado**; um ditado em curso termina com o dicionário
  com que começou (mesmo desenho de `Ajustar`: ponteiro atômico, aplicado no keydown com a
  máquina parada). O vocabulário do ASR (o prompt) é remontado do arquivo relido em **toda**
  gravação do painel, junto com a troca (`gravarETrocar`; ADR-020, decisões 5 e 6), mas o Aprender e
  o Desfazer **não o mudam**: uma variante não entra nele, e o termo que o painel cria é `AliasOnly`,
  que `dictionary.DisputaOPrompt` tira do prompt (o que o tira é essa regra, e não a prioridade 50).
  O aplicador do aprendido que chega da nuvem (F8g) grava pelo mesmo caminho: remonta o prompt, e o
  termo que ele cria (D2 da F8) é `AliasOnly` e fica fora dele.
- Só contagens vão a log (`termos`, `variantes_do_painel`), nunca o termo nem a variante.
- O `cmd/asrstage` não sobrescreve mais o `bin/dicionario.json` que já existe. Se ele não carrega
  (vazio, quebrado, ou que o Agent recusa ao montar o dicionário em memória, como uma entrada sem
  forma canônica), a montagem falha sem sobrescrevê-lo. Se o dicionário do repositório (onde o
  `dictlearn` grava por padrão) tem termo ou variante que o de `bin/` não tem, avisa quantos, só
  em contagens. Para ensinar ao que o Agent usa: `dictlearn -dict bin/dicionario.json`.
- Um Agent anterior a esta etapa recusa um arquivo com o campo `origem` (o carregamento rejeita
  campo desconhecido): voltar de versão, depois de o painel ter criado um termo, pede `Desfazer`
  antes, ou apagar o campo.
- Uma chave antiga em `alias_origem` com o texto da variante (uma origem órfã, sem alias em termo
  nenhum) é sobrescrita por `painel`, e o `Desfazer` a apaga: a única exceção à volta idêntica.

## Resolver
```go
type Resolver interface {
    Resolve(context.Context, ResolveRequest) (ResolveResult, error)
}
```

Implementações:
- LiteralResolver;
- SmartResolver;
- PolishResolver.

## PolishClient e Polir
Decidido na ADR-016. As assinaturas de `internal/resolver` são as da fatia S1 (código em
`polish.go`, `polish_guardas.go`, `polish_prompt.go`); as de `internal/ia` são da S2. Este texto
fixa o comportamento.

```go
// internal/resolver
type PolishClient interface {
    Polish(ctx context.Context, req PolishRequest) ([]byte, error) // o JSON BRUTO do modelo
}

func Polir(ctx context.Context, cliente PolishClient, smart ResolveResult, opts PolirOpcoes) ResolveResult
func (p *PolishResolver) Reescrever(ctx context.Context, req ResolveRequest, smart ResolveResult) ResolveResult

var ErrPolishSemChave error // o cliente devolve este erro quando não há chave salva

// ErroPolish: o cliente devolve este erro quando sabe DIZER por que o provedor falhou.
type ErroPolish struct{ Motivo MotivoFallback } // Error() é sempre "polish: <motivo>"
```

- `Polir`: a reescrita separada do Smart. O Smart continua no laço de eventos; só `Polir` faz
  rede, e roda em goroutine própria do runtime (o resultado volta pela fila `pending`).
  `PolishResolver.Reescrever` é a metade Polish de `Resolve`: levanta os termos do dicionário por
  consulta **local** e chama `Polir`; é o que o Agent roda na goroutine com prazo, depois de
  rodar o Smart no laço. `PolirOpcoes` leva `Idioma`, `MinPalavras` (padrão 4), `MaxEdicao`
  (padrão **0**) e `Termos`.
- `Polir` nunca devolve erro. Devolve o `ResolveResult` do Smart com `FinalText` trocado **só se
  todas as guardas passarem**, `Reescrito bool` (verdadeiro só quando a reescrita foi aceita) e,
  no fallback, `MotivoFallback`, um **enum**, nunca texto livre: `curto`, `sem_cliente`,
  `sem_chave`, `transporte`, `prazo`, os que só o cliente do provedor sabe dar (`chave_recusada`,
  `sem_credito`, `limite`, `modelo_inexistente`, `modelo_incompativel`, `sobrecarga`,
  `pedido_recusado`, `recusa`, `truncada` e `resposta_invalida`; a tradução de cada resposta está em
  `06-api-contract.md`), `json`, `formato`, `numeros`, `siglas_termos`, `palavras`, `caixa` (uma
  palavra de 2 letras ou mais saiu toda em maiúscula sem estar assim na entrada; a guarda roda
  por último, então o motivo só aparece quando nenhuma outra recusou), `trecho_novo` e
  `dicionario` (a consulta local ao dicionário falhou: sem como conferir os termos, o texto não sai
  da máquina, e fica o Smart). `Mode` continua `polish` em qualquer
  desfecho; o histórico usa `Reescrito` para gravar `polish` ou `smart` (S4). Só o motivo chega
  ao `Warnings` (`polish: mantido resultado smart (motivo=X)`, fixo); `curto` e `sem_cliente` não
  geram aviso, porque não são falha.
- **Como `Polir` lê o erro do cliente, em ordem:** `ErrPolishSemChave` (achado por `errors.Is`) é
  `sem_chave`; um `*ErroPolish` (achado por `errors.As`, então vale embrulhado, mesmo com o `ctx` já
  vencido) é o `Motivo` que ele traz; `ctx` vencido ou `net.Error` de timeout é `prazo`; o resto,
  inclusive cancelamento, é `transporte`. O texto do erro é sempre `polish: <motivo>`: nunca o corpo
  do provedor, a chave ou o ditado.
- `PolishRequest` leva o texto final do Smart e as peças prontas que o cliente manda como estão:
  `Instrucoes` (o prompt), `Mensagem` (`PolishMensagemUsuario`), `Schema` (`PolishSchemaSaida`,
  nome `PolishSchemaNome`) e `PromptRef` (`polish-ptbr@1.0.2`). O cliente não monta prompt nem
  mensagem por conta própria. Sem contexto, dicionário, áudio, histórico nem IDs. O dicionário é
  consultado pelas guardas, localmente.
- **`PolishRequest` não se serializa nem se imprime com o ditado.** Nenhum campo tem tag JSON;
  `String`, `GoString`, `Format` (qualquer verbo) e `LogValue` mostram só a versão do prompt e o
  tamanho do texto em caracteres. Um `slog` com a struct, um `%+v` num erro ou um `json.Marshal`
  de depuração não levam o ditado. Um teste com canário confere `json`, `fmt` em 7 verbos e `slog`.
  Quem loga uma chamada ao provedor (S2, S4) escolhe os campos um a um (`PromptRef`, o tamanho).
- `internal/ia` (S2), só `net/http`: `(*Cliente).Polir(ctx, Chamada{Provedor, Modelo, Esforco},
  chave, PolishRequest) (Resposta{Texto, Tokens}, error)`, com a chave por **parâmetro** a cada
  chamada (OpenAI `POST /v1/responses`, Anthropic `POST /v1/messages`), e
  `Polidor{Cliente, Chamada, Chave func(ctx) (string, error), Uso func(Tokens)}`, que implementa
  `PolishClient` e chama `Chave` a cada ditado. `Texto` é o JSON bruto do modelo. `Esforco` vazio
  manda `reasoning.effort: "none"` (só OpenAI); `EsforcoOmitido` (`"omitir"`) tira o campo, para
  modelo sem raciocínio; outro valor (por exemplo `minimal`) vai como veio. `Tokens` (entrada, saída
  e raciocínio, este já contado na saída) vem preenchido mesmo com erro, porque uma resposta
  recusada ou truncada também é cobrada, e vai só para `Uso`, que guarda números. O contrato da
  chamada está em `06-api-contract.md`, "Chamada de Polish ao provedor". `NovoTransporteHTTP1()`
  devolve um transporte novo, igual ao do pacote (só HTTP/1.1), para quem precisa embrulhá-lo (o
  `cmd/polishbench`, S3, conta o status de cada resposta e a forma das respostas 200 por um
  `RoundTripper` próprio, e com `-conexao-nova` pede `Connection: close` a cada chamada).
- **A chave.** `Polidor.Chave` é chamada a cada ditado (no Agent, a leitura do Gerenciador de
  Credenciais, feita pelo binário do Agent, onde o portão `sysinput` abre); provedor e modelo são os
  do keydown. Nenhum campo do `Polidor` nem do `Cliente` guarda a chave, e ela só vai no cabeçalho,
  em HTTP/1.1: remover a chave vale no ditado seguinte. Um erro da função `Chave` nunca é repassado
  (poderia trazer segredo): vira `sem_chave`, exceto quando o `ctx` do ditado já venceu, e então é o
  erro do `ctx` (`prazo` ou `transporte`).
- **No Agent (fatia S4, `internal/agent/winruntime/polish.go`).** O runtime não conhece
  `internal/ia`; conhece só a metade de rede de um ditado:

  ```go
  // internal/agent/winruntime
  type Polidor interface { // *resolver.PolishResolver o cumpre
      Reescrever(ctx context.Context, req resolver.ResolveRequest, smart resolver.ResolveResult) resolver.ResolveResult
  }
  type AbrirPolidor func(provedor, modelo string, contar func(entrada, saida int)) (Polidor, string)
  func (rt *Runtime) UsarPolish(abrir AbrirPolidor)
  func (rt *Runtime) UltimoPolish() EstadoPolish // {Motivo resolver.MotivoFallback; Quando time.Time}
  ```

  `Preferencias` ganha `Polish`, `Provedor` e `Modelo` (comparável); valem os do keydown. O Smart
  roda no laço; com o Polish, `resolver` devolve nil e a goroutine da chamada chama
  `AbrirPolidor` (provedor e modelo do keydown) e `Reescrever`, com o `ctx` derivado do de `Run` e
  prazo de **3,5 s contados do começo da resolução** (o Smart incluído; ADR-016, "Decisões tomadas
  na implementação (S4)"), que cresce com o texto que entra na resolução: mais 6 ms por caractere
  acima de 250, até 8 s aos 1000 (`prazoPolish`; ADR-016, "Emenda de 01-out-2026"). O watchdog da
  resolução no núcleo é sempre 1,5 s maior (`dictation.PrazoDeResolucao`). O runtime espera o
  resultado **ou** o prazo: vencido, entra o texto do Smart com o motivo `prazo` (ou `transporte`, se foi o Agent encerrando), mesmo que o `Polidor`
  ignore o `ctx`, e a resposta tardia é descartada. O resultado volta ao laço por um canal próprio
  (`rt.polidos`). O segundo valor de `AbrirPolidor` é o esforço mandado, só para o log; `contar`
  recebe os tokens da goroutine da chamada. `AbrirPolidor` nulo, ou `Polidor` nulo, é
  `sem_cliente`. `UltimoPolish`: motivo vazio quando o último Polish agiu ou não houve nenhum; o
  curto não mexe; é o que a S5 lê para `polish_ultimo_motivo`.
- **O adaptador (`cmd/agent/polish.go`).** `polishDoAgent.abrir` implementa `AbrirPolidor`:
  `resolver.PolishResolver{Smart: <o Smart do Agent>, Client: ia.Polidor{…}}`, com `Chave =
  cofre.Ler(provedor)` a cada chamada e `Uso` levando os tokens a `contar`. Provedor que não é
  `openai` nem `anthropic` não tem cliente. **Política de esforço** (só OpenAI, sem nome de modelo
  no código): `""` (manda `none`) → `minimal` → `omitir`, descendo um degrau por
  `modelo_incompativel`, por provedor e modelo, só em memória nesta execução.
- As guardas (`16-ai-behavior-spec.md`) são funções puras, sem rede e sem relógio, e rodam na
  ordem curto → chamada → JSON → formato → números → siglas, termos e nomes → palavras → caixa.
  Falhou → texto do Smart. Um `final_text` com `\r\n` reprova (`json`): o modelo devolve `\n`, e a conversão
  para `\r\n` é da inserção. Uma tabela de alinhamento acima de 4 milhões de células (um ditado de
  cerca de 2 mil palavras) também cai no Smart, com o motivo `palavras`.
- `warnings`, `changed` e qualquer campo extra que o modelo mande são descartados.

## TextInserter
```go
type TextInserter interface {
    Insert(context.Context, FocusTarget, string) (InsertResult, error)
}
```

## SyncClient
Como a F7b o deixou (ADR-019, "Decisões da F7a/b"; `internal/sync/fio.go` e `cliente.go`):
```go
type SyncClient interface {
    // Com erro, NENHUM resultado vale: o lote fica em voo, e o próximo envio
    // o repete com os mesmos mutation_id.
    Push(ctx context.Context, muts []Mutation) (PushResult, error)
    // As mudanças depois de afterSeq (exclusivo), em ordem de seq.
    Pull(ctx context.Context, afterSeq int64, limit int) (ChangePage, error)
}

type Mutation struct {
    MutationID, EntityType, EntityID, Operation string // "upsert"; "delete" só no aprendido (F8)
    BaseVersion int64          // 0: criar (o ditado inteiro); senão a server_version
    Payload     json.RawMessage // criar: a lista fechada; mudar: só final_text e/ou deleted_at;
                                // o delete do aprendido: nil (sai como null)
}
type MutationResult struct {
    MutationID string
    Status     string // applied | duplicate | conflict | rejected | gone
    Original   string // o status da primeira vez (só no duplicate)
    EntityVersion int64
    Mesclado   bool   // applied: a versão NÃO é guardada; o estado chega no pull
    Code       string // rejected (lista fechada)
    Motivo     string // gone: apagado | fora_da_janela
    State      *Item  // só no conflict (o item, com versao_em_conflito)
    StateAprendido *Aprendido // só no conflict de uma mutação learned (F8f1)
}
type PushResult struct { Results []MutationResult; Epoch string }

type Change struct {
    Seq int64
    EntityType, EntityID, Operation string // upsert | tombstone
    EntityVersion int64
    DeviceID *string // de onde veio; NUNCA para descartar o eco
    Payload  *Item   // o estado atual do ditado; nulo na lápide
    PayloadAprendido *Aprendido // o estado atual do aprendido (F8f1); o desfeito traz UndoneAt
}
type ChangePage struct { Changes []Change; LastSeq int64; HasMore bool; Epoch string }

// O aprendido como a nuvem o devolve (F8; o payload do pull e o state do conflict).
type Aprendido struct {
    ID, Variant, Term string
    Restricted bool
    Domain     *string    // o nome do domínio; nulo sem domínio
    UndoneAt   *time.Time // nulo no par ativo
}
```
- **O fio por tipo (F8f1; ADR-019, "Decisões da F8"):** o `Cliente` lê o payload do pull pelo
  `entity_type` da mudança e o `state` do push pelo `entity_type` da mutação do mesmo lugar no lote
  (a resposta não traz o tipo): o `learned` no `Aprendido`; o ditado, e qualquer tipo que o PC não
  conhece, no `Item`, exatamente como na F7. O ilegível no tipo é `ErrContrato` com o código `json`,
  como antes. O `json.Unmarshal` direto num `ChangePage` ou num `PushResult` continua a leitura da
  F7 (tudo no `Item`): a suíte de contrato a usa para provar que um PC da F7 lê a nuvem da F8.
  O código `nada_a_desfazer` (o `delete` de um par que a nuvem não tem) é `CodigoNadaADesfazer`.
- **O `Pull` devolve o estado atual das entidades e as lápides**, não eventos com conteúdo; os dois
  devolvem o `sync_epoch`, que o PC compara com o guardado (outro: descarta a resposta e recomeça,
  com a base vazia; no mesmo epoch, o PC nunca reenvia com a base vazia).
- **Erros tipados:** `ErrNaoAutorizado` (`401`: o PC fica desconectado), `ErrRecomeco` (`410`: o
  cursor volta a 0, sem reenviar nada), `*ErrEspere` (`429`, com o `Retry-After` até 10 min; a
  bandeja diz "aguardando"), `ErrTransitorio` (a rede, o prazo, um `5xx`, um redirecionamento, uma
  resposta acima de 16 MiB: espera com jitter) e `ErrContrato` (`400`, `403`, `404`, um JSON que não
  se lê, resultados que não batem com o lote: espera longa; a bandeja diz "resposta inesperada"),
  estes dois como `*Falha` com um código de lista fechada. Nenhum erro leva a URL, o token ou o corpo.
- **O `Cliente` de produção:** o Bearer do token do aparelho (lido do Gerenciador de Credenciais pelo
  `nuvem.Servico`, atrás do portão), prazo de 30 s por pedido, nenhum redirecionamento seguido, o
  corpo do pedido até 1 MiB e o da resposta até 16 MiB (a pior página do pull, com o texto escapado
  no JSON, passa de 13 MiB; revisão de 06-out-2026).
- **Quem usa:** o `Motor` (uma rodada: o push em lotes de até 500 mutações e 512 KiB de payload, e o
  pull até `has_more` falso) e o `Laco` (`internal/sync/laco.go`), em segundo plano. As decisões sobre
  cada resultado e cada mudança são puras (`DecidirResultado`, `DecidirMudanca`) e devolvem uma
  `storage.AcaoLocal`, que a `storage.Fila` executa na transação da resposta.
- **O dicionário do aprendido (F8f3; ADR-019, "O estado da F8f3"):** o `Motor` e o `Laco` recebem o
  `dicionario.json` por uma interface; nil deixa o aprendido de fora, como na F7 (nada é varrido nem
  enviado, e o M é 0):
  ```go
  type DicionarioDoAprendido interface {
      // O lote do aplicador, com o arquivo relido sob a trava do dicionário:
      // escolher devolve os pares; antesDeGravar grava a intenção, só quando
      // há o que gravar. Os erros do arquivo indisponível são os de
      // dictionary.ArquivoIndisponivel.
      AplicarPares(escolher func(atual dictionary.Semente) []dictionary.ParDaNuvem,
          antesDeGravar func(dictionary.Lote) error) (dictionary.Semente, dictionary.Lote, error)
  }
  ```
  O `dictionary.Arquivo` a satisfaz; no Agent, quem a cumpre é o `aprendiz.Aplicador` (F8g), sob a
  mesma trava do painel, que troca o dicionário em memória e o prompt depois de gravar (a troca que
  falha depois da gravação é `dictionary.ErrGravadoSemTroca`, o lote que não terminou); o
  `montarSync` a passa ao laço, e o Aprender e o Desfazer do painel sobem a versão local no ato
  (`Aplicador.LigarAoSync`, sobre `storage.Fila.SubirVersaoLocal`) e acordam o laço. A varredura (`Motor.Varrer`) roda no começo de cada rodada, depois do
  `Preparar` e antes do `CompletarEnvio` (que recebe o M dela), e os marcadores `learned` sobem no
  começo de cada lote do push (`MutacaoDoAprendido`, `DecidirResultadoDoAprendido`). O mesmo
  aplicador recebe o pull (F8f4; ADR-019, "O estado da F8f4"): o `Motor.Receber` passa as mudanças
  `learned` de cada página pelo arquivo (sob a trava, com a leitura curta do epoch e do pareamento
  antes de tocá-lo e a intenção antes da gravação) e grava o espelho delas na transação da página,
  com os ditados e o cursor; o arquivo que não deu deixa o par pendente, e o cursor anda.

> **ADR-019 (01-out-2026): o contrato de sync passa a ser por usuário** (`06-api-contract.md`,
> "Sync"; ADR-019, "Contrato de sync, por usuário"). Até a F7b a assinatura era
> `Push(ctx, []Mutation) ([]MutationResult, error)` e `Pull(ctx, int64) (ChangePage, error)`, sobre o
> contrato antigo da F1-D (sem `gone`, sem mescla, sem epoch); ela saiu com o motor antigo na F7a.

## UpdateProvider
```go
type UpdateProvider interface {
    Latest(context.Context, string, string) (UpdateInfo, error)
}
```

## ModelManager
- catalog;
- download;
- checksum;
- atomic install;
- activate/remove;
- report disk/memory metadata.

## Invariantes
- audio/context non-serializable por default;
- logs recebem IDs/metrics;
- network clients têm timeout (o do Polish é o `ctx` de 3,5 s por ditado, até 8 s no ditado
  longo, contados do começo da resolução, sem retry e sem seguir redirecionamento; sem prazo no `ctx`, o `Cliente` corta em
  10 s; o runtime desiste no prazo mesmo que o cliente não);
- nenhum log recebe texto ditado, `warnings` do modelo, corpo de erro de provedor ou chave;
- o aprendizado (ADR-017) só registra contagens: nunca o texto, nem o "de" e o "para" da variante;
- o sync (ADR-019, F7b) só registra contagens e códigos de listas fechadas: nunca texto, id de ditado,
  token, URL ou e-mail; o cliente do sync tem prazo (30 s) e não segue redirecionamento;
- mutation_id obrigatório;
- cloud user vem da sessão;
- o provider de ASR da nuvem (ADR-021) não persiste nem loga o áudio, não decodifica nem codifica,
  não segue redirecionamento, não repassa corpo de erro e não guarda a chave: lê-a a cada chamada.
