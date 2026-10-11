# web

Painel do Aurora Whisper. React + TypeScript (strict) + Vite, sem biblioteca de componentes.

## Scripts

```
pnpm dev          # servidor de desenvolvimento (sem API: ver abaixo)
pnpm build        # type-check (tsc -b) + build do PC em ..\bin\painel
pnpm build:nuvem  # type-check + build da nuvem em dist-nuvem (o app; ADR-019, F4)
pnpm lint         # eslint .
pnpm test         # vitest run (inclui o bundle.test.ts, que constrói os dois numa pasta temporária)
```

## Dois builds do mesmo código (ADR-019, decisão 12)

O mesmo `index.html` e os mesmos componentes servem o painel do PC e o app da nuvem; o que muda é a
entrada. `vite build` usa `src/main.tsx` (o bilhete, a Local API do Agent, as Configurações do PC);
`vite build --mode nuvem` troca a entrada por `src/nuvem/main.tsx` (o login, a `/api/v1` pelo cookie
da sessão, as Configurações da conta). O Painel recebe do modo um ambiente (`src/ambiente.ts`): o
cliente do histórico, a tela de Configurações, a faixa do topo e os textos. O ESLint proíbe importar
`src/nuvem` fora dela (e o que é só do PC dentro dela), e o `bundle.test.ts` confere os dois bundles:
o local nunca tem login nem service worker, e o da nuvem nunca tem nada do PC. Para ver o app da
nuvem com dados de exemplo, sem Docker e sem Google: `scripts/nuvem-local-teste.ps1` e
`docs/teste-manual-nuvem-f4.md`.

O app da nuvem é instalável (ADR-019, F5): só no modo nuvem, um plugin do `vite.config.ts` põe na raiz
do build o que está em `pwa/` (o `manifest.webmanifest`, os ícones e o `sw.js`, com a lista da casca
e a versão trocadas no lugar dos marcadores) e o link do manifest no `index.html`. Os ícones saem do
`go run ./cmd/gerarico` (da raiz do repositório). O `src/nuvem/registrarSW.ts` registra o service
worker; o `bundle.test.ts` roda o `sw.js` emitido num `node:vm` e confere o que ele guarda e o que
nunca toca. Roteiro: `docs/teste-manual-nuvem-f5.md`.

## Como o painel chega ao navegador

O build vai para `bin\painel`, ao lado do `aurora-whisper-agent.exe`. O Agent serve essa pasta em
`http://127.0.0.1:<porta aleatória>` (`internal/agent/painel`) e o item **Abrir Aurora Whisper** do
menu da bandeja abre o navegador com um bilhete de uso único na URL. A página troca o bilhete pelo
token da Local API (`src/sessao.ts`) e guarda o token só na memória da aba — recarregar a página
perde a sessão, e abrir de novo pela bandeja resolve.

Por isso `pnpm dev` sozinho mostra só a tela "Abra o painel pela bandeja": não há Agent, bilhete
nem API. Para ver o painel com dados, rode o Agent e abra pela bandeja.

Para **ver as telas sem Agent nem nuvem**, o `pnpm dev` serve duas páginas que montam o app inteiro
contra os dublês dos testes (em memória; nada sai da máquina), e que nenhum build inclui:
`/telas-dev.html?cenario=estados` (o painel do PC; também `so-estados`, `pergunta`, `so-trocas`,
`conectado`, `freio` e, para o bloco Histórico dos 6 meses, `historico`, `historico-simples`, `historico-1`,
`historico-ligado`, `historico-pausado`, `historico-copias`, `historico-envio`, `historico-so-envio`, `historico-de-novo` e `historico-velho`) e `/telas-dev-nuvem.html?cenario=lista` (o app da nuvem; também `vazio`, `teto`, `erro` e
`longa` e, para o ditado do celular, `gravando`, `enviando`, `erro-rede`, `sessao` e `chegou`, com um microfone de mentira que fala sozinho; e, para o aceite e as Configurações do ditado (ADR-021, C6b), `aceite` (o toque no microfone de uma conta sem aceite: a folha), `ditar-desligado`, `ditar-ligado` e `ditar-polish`). Abrem as Configurações sozinhas (`&config=0` deixa no Histórico; os do ditado ficam no Histórico, menos o `aceite`, que abre a folha); claro e escuro seguem o
sistema do navegador, e 320 e 375 px, o modo de dispositivo. Os cenários estão no cabeçalho de
`src/testes/telasDev.tsx` e `src/nuvem/telasDev.tsx`.

O contrato de `/api/sessao` e `/api/history` está em `docs/ai-spec/06-api-contract.md`, seção "API
local (Windows Agent)". As rotas das Configurações — `/api/settings` na forma nova, `/api/ia/chave` e
`/api/ia/testar` — seguem o contrato comum de 29-set-2026, que ainda não está no 06 (lá,
`/api/settings` ainda é a versão em memória): ele entra junto com a frente do Agent. Até lá, a forma
que o painel espera está no dublê `src/testes/servidorFalso.ts`, inclusive o que ele lê fora do
contrato comum: `polish_disponivel` (ausente = `false`), `polish_ultimo_motivo` (ausente = `""`),
`modelo_sugerido` na resposta do teste (ausente = `""`), e `null` onde o Go manda slice nil
(`reinicio_pendente`, `modelos`). O roteiro de teste manual do painel, com a montagem de `bin/`,
está em `docs/teste-manual-bandeja-e-painel.md`.

A vista **Configurações** (`src/Configuracoes.tsx`, lógica pura em `src/config.ts`) lê e grava
`/api/settings` com `base_version` (409 recarrega, mantém o que a pessoa mudou e avisa). O PUT leva
o `autostart` **só quando a pessoa mexeu na caixa** (`paraEnviar`): ele mora no registro, fora da
`version`, e a bandeja e o Gerenciador de Tarefas o mudam sem subir a versão — reenviar o valor
lido ao abrir religaria o que foi desligado lá. Campo ausente, o Agent mantém. Pelo mesmo motivo,
sem nada por salvar, a tela lê de novo ao voltar a esta vista ou à aba do navegador. A chave de
API nunca entra no estado do React: fica só no campo de senha até **Salvar chave**, vai no corpo do
`PUT /api/ia/chave` e o campo esvazia. Nenhuma resposta a devolve — a tela mostra só os 4 últimos
caracteres, vindos do `GET /api/ia/chave`. O Polish só pode ser escolhido quando o Agent manda
`polish_disponivel: true` (ausente vale `false`) e há provedor e chave guardada. A seção IA mostra,
numa linha discreta (`role="status"`), a frase de `polish_ultimo_motivo` — por que o último Polish
não agiu, sem texto ditado —, marca com "(sugerido)" o `modelo_sugerido` do Agent na lista da conta
e, se nenhum modelo foi escolhido e a conta o tem, o põe no **rascunho** (`comSugestao`; salvar
segue explícito). A Privacidade traz a frase do Polish (o texto ditado vai ao provedor de IA).

O servidor manda `Content-Security-Policy: default-src 'self'`: nada de script embutido no HTML,
nada carregado de fora (fonte, imagem, CDN).
