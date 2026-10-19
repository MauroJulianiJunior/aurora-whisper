# Componentes de terceiros

O Aurora Whisper é distribuído sob a licença MIT (`LICENSE`), sem exceção: o símbolo e a ilustração
da Aurora também (ver "Marcas", no fim). Ele usa os componentes abaixo, cada um sob a própria
licença. Os do primeiro grupo vão dentro do que este projeto compila e distribui (o executável do Agent e o
painel); os do segundo **não estão no repositório**: quem monta a própria cópia os obtém das fontes
originais.

## Embutidos no build

| Componente | Onde | Licença |
|---|---|---|
| Space Grotesk, Inter, IBM Plex Mono (via `@fontsource`) | fontes do painel (`web/`) | SIL Open Font License 1.1 — texto completo em `web/public/licencas-fontes.txt`, que vai junto do build |
| IBM Plex Mono Medium, subconjunto latin (`internal/agent/overlay/fontes/IBMPlexMono-Medium-latin.ttf`, convertido do WOFF do `@fontsource/ibm-plex-mono` sem alterar tabela) | embutida no executável do Agent (`go:embed`), texto das mensagens do overlay | SIL Open Font License 1.1 — texto completo em `internal/agent/overlay/fontes/OFL.txt`, que tem de acompanhar o executável distribuído (cláusula 2 da OFL): dentro do `.exe` a licença não fica visível |
| React, React DOM e Scheduler (dependência do React DOM) | painel (`web/`), dentro do JavaScript gerado pelo build | MIT — o aviso de copyright e a licença acompanham o pacote de cada um (`web/node_modules/<pacote>/LICENSE`) |
| Biblioteca padrão e runtime do Go | Agent e demais executáveis | BSD-3-Clause — o aviso de copyright e a licença têm de acompanhar o executável distribuído |
| `golang.org/x/sys` | Agent | BSD-3-Clause |
| VERO, o dicionário pt-BR do LibreOffice (`pt_BR.aff` e `pt_BR.dic`, de Raimundo Santos Moura e colaboradores, sem alteração, comprimidos com gzip) | `internal/ortografia/vero/`, embutido no Agent e no servidor (`go:embed`); diz se uma palavra existe, para a correção de grafia do Polish (ADR-016, 18-out-2026) | Mozilla Public License 2.0 (o VERO é LGPLv3 ou MPL 2.0, à escolha; usamos a MPL 2.0) — texto em `internal/ortografia/vero/MPL-2.0.txt`, com a origem e o hash em `internal/ortografia/vero/LEIAME.md`. Os arquivos ficam no repositório na forma original, o que cumpre a MPL |
| `golang.org/x/image` (`font`, `font/opentype`, `font/sfnt`, `math/fixed`, `vector`) e `golang.org/x/text` (`encoding/charmap`, `transform`, puxado pelo `font/sfnt`) | rasterização do texto do overlay do Agent | BSD-3-Clause — o aviso de copyright e a licença têm de acompanhar o executável distribuído |
| `modernc.org/sqlite` (o SQLite compilado para Go, sem CGO) e dependências (`modernc.org/libc`, `modernc.org/mathutil`, `modernc.org/memory`, `github.com/google/uuid`, `github.com/dustin/go-humanize`, `github.com/mattn/go-isatty`, `github.com/ncruces/go-strftime`, `github.com/remyoudompheng/bigfft`) | banco local do Agent (histórico e dicionário) | BSD-3-Clause, MIT e similares — ver o `LICENSE` de cada módulo. O SQLite em si é de domínio público |
| `github.com/jackc/pgx/v5` e dependências (`github.com/jackc/pgpassfile`, `github.com/jackc/pgservicefile`; `golang.org/x/text`, `secure/precis` e dependências, para o SCRAM; e, pelo pool de conexões `pgxpool` da F2, `github.com/jackc/puddle/v2` e `golang.org/x/sync/semaphore`) | driver do PostgreSQL do servidor da nuvem (`cmd/server`, `internal/server/pgstore`, ADR-019). **Não entra no Agent** | MIT (`pgx`, `pgpassfile`, `pgservicefile`, `puddle`) e BSD-3-Clause (`golang.org/x/text`, `golang.org/x/sync`) — ver o `LICENSE` de cada módulo |

As ferramentas só de desenvolvimento (Vite, TypeScript, ESLint, Vitest e afins) não entram no que
é distribuído.

## Obtidos na montagem, fora do repositório

O `cmd/asrstage` **não baixa nada**. Ele só copia, para a pasta `bin/`, arquivos que quem monta já
colocou em `thirdparty/whisper/Release/` (o servidor e as DLLs) e em `models/quant/small-q4_0.bin`
(o modelo), e confere o tamanho e o checksum do modelo. As duas pastas estão no `.gitignore`. O
modelo `q4_0` não existe pronto para baixar: ele é gerado localmente, com o `whisper-quantize.exe`
do próprio whisper.cpp, a partir do `ggml-small.bin`. O passo a passo está no `README.md`.

| Componente | Origem | Licença |
|---|---|---|
| whisper.cpp (`whisper-server.exe` e as DLLs `whisper.dll`, `ggml*.dll`) | pacote de release do whisper.cpp (`whisper-bin-x64.zip`) | MIT |
| Modelo Whisper `small`, no formato ggml (`ggml-small.bin`), de onde sai o `small-q4_0.bin` | pesos do Whisper, da OpenAI, convertidos para ggml pelo projeto whisper.cpp | MIT |
| `small-q4_0.bin` (o `ggml-small.bin` quantizado em `q4_0`) | gerado localmente por quem monta; no Setup, distribuído pelo projeto (ver "No Setup") | MIT, por derivar dos pesos acima |

## No Setup (instalador)

O Setup (`installer/aurora-whisper.iss`, montado por `scripts/montar-instalador.ps1`) **distribui** o
que a seção acima diz que quem monta obtém: o servidor e as DLLs do whisper.cpp (do pacote
`whisper-bin-x64.zip` da tag de build `b5130`, conferido por SHA-256) e o modelo `small-q4_0.bin`,
que o projeto gera quantizando o `ggml-small.bin` oficial do Hugging Face e hospeda num release
próprio (ADR-018, "Modelo embutido e de onde ele vem"). O Setup instala, em `licencas\` ao lado do
programa, os textos de licença de tudo o que leva:

| Arquivo em `licencas\` | O quê | De onde vem na montagem |
|---|---|---|
| `LICENSE`, `THIRD_PARTY_NOTICES.md` | a licença MIT do projeto e este arquivo | o repositório |
| `OFL-IBMPlexMono-overlay.txt` | OFL 1.1 da fonte embutida no executável do Agent (a cláusula 2 da OFL pede o texto junto) | `internal/agent/overlay/fontes/OFL.txt` |
| `Go-LICENSE.txt` | BSD-3-Clause da biblioteca padrão e do runtime do Go | o `LICENSE` do GOROOT usado no build |
| `go-modulos\<módulo>\` | `LICENSE`, `NOTICE`, `PATENTS` etc. de cada módulo que entra no Agent (`golang.org/x/*`, `modernc.org/*` e os demais) | o cache de módulos, pela lista de `go list -deps ./cmd/agent` |
| `whisper.cpp-LICENSE-MIT.txt` | MIT do whisper.cpp | o repositório do whisper.cpp, no commit da tag `b5130` (endereço fixo no script) |
| `openai-whisper-LICENSE-MIT.txt` e `Apache-2.0.txt` | os dois textos que cobrem os pesos do Whisper: o README do `openai/whisper` diz MIT, e o card do `openai/whisper-small` no Hugging Face diz Apache-2.0. As duas são permissivas e permitem redistribuir um derivado quantizado; o `q4_0` é uma alteração dos pesos originais (quantização), indicada aqui | o `LICENSE` do `openai/whisper` (num commit fixo, e não em `main`) e o texto da Apache-2.0 |

O painel leva também `licencas-fontes.txt` (a OFL das fontes web, vinda de `web/public/`). As
três últimas linhas da tabela são baixadas na montagem, de endereços fixos, e conferidas pelo
**SHA-256 fixado** em `scripts/montar-instalador.ps1` (`$LicencasRemotas`): um arquivo diferente faz
a montagem falhar. Os endereços e os hashes ainda não passaram por um download de verdade; a
primeira montagem do Setup os confere.

## Marcas

- O símbolo "Pico estourado" (`docs/marca/pico-estourado/`) foi desenhado para este projeto e segue
  a licença MIT do repositório.
- A ilustração da Aurora (`docs/marca/retrato/`, e a cópia do desenho embutida em
  `web/src/Aurora.tsx`) segue a licença MIT do repositório, como o símbolo. O autor a liberou em
  30-set-2026: ela deixou de ser de direitos reservados. Quem a copiar, modificar ou distribuir
  mantém o aviso de copyright da MIT (`LICENSE`).
- A paleta de cores do projeto é descrita em `docs/marca/README.md`. Nenhum logo nem nome pessoal
  faz parte do projeto.
