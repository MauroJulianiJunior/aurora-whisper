# CLAUDE.md

## Project context

Aurora Whisper é um sistema de ditado por voz para Windows 10/11 x64, com backend cloud e dashboard web. O Windows Agent captura áudio enquanto uma tecla configurável é mantida pressionada, transcreve ao soltar, aplica resolução contextual controlada e insere o texto no campo que possui foco de teclado.

O sistema deve ser local-first para captura/experiência do Windows, mas cloud-connected, só depois de a pessoa conectar, para sincronizar o histórico, o que o painel aprende e os dispositivos (dicionário completo, correções e preferências: depois, ADR-019).

A especificação canônica está em `docs/ai-spec/00-index.md`.

## Workflow obrigatório

1. Leia `docs/ai-spec/00-index.md`.
2. Leia `docs/ai-spec/03-acceptance-criteria.md`.
3. Leia `docs/ai-spec/13-implementation-plan.md`.
4. Para cada tarefa, leia os documentos técnicos apontados nela.
5. Explore o repositório antes de editar.
6. Para mudanças grandes, gere um plano curto antes da implementação.
7. Trabalhe em vertical slices pequenas e verificáveis.
8. Execute os testes/validações da slice.
9. Faça commit local por vertical slice quando a slice estiver verde.
10. Nunca faça `git push`, deploy, release ou alteração em infraestrutura real sem autorização humana explícita.

## Architecture guardrails

- Linguagem principal do Agent e backend: Go.
- Frontend: React + TypeScript + Vite.
- Banco cloud: PostgreSQL.
- Persistência/cache Windows: SQLite.
- ASR sempre por interface de provider; nunca acoplar o domínio a Whisper/Parakeet/outro modelo.
- Áudio não deve ser persistido por padrão em disco nem na nuvem.
- Contexto de outras aplicações é efêmero e não deve ser sincronizado.
- Captura de tela está fora do escopo.
- Agent deve funcionar localmente mesmo com cloud indisponível.
- Backend cloud é a fonte consolidada dos dados sincronizados.
- Dashboard React deve ser reutilizável em modo local e cloud.
- Overlay é responsabilidade do Windows Agent.
- API local deve bindar somente em loopback.
- Cloud usa HTTPS.

## Testes que agem no sistema real

Esta é uma máquina de desenvolvimento com desktop de verdade, em uso por uma pessoa. **Não é um
sandbox.**

**Nunca execute código que injete entrada ou altere recurso global do sistema.** Inclui, e não se
limita a:

- `SendInput`, `keybd_event`, `mouse_event` — injetam tecla e clique na janela que estiver com
  foco, que é a janela real de quem está usando a máquina
- escrita no clipboard — destrói o que a pessoa tinha copiado
- `SetWindowsHookEx` fora do binário do Agent executado deliberadamente
- qualquer coisa que mova foco, crie janela ou mude estado global

> Isto já causou dano real: um teste de inserção, rodado por um agente, digitou texto numa janela
> aberta do usuário e deixou um modificador preso, travando o teclado dele. A regra existe por
> causa desse episódio, não por precaução teórica.

### Como testar essas peças, então

1. **A lógica de decisão é pura e vai sem build tag**: qual caminho escolher, qual o limiar, o
   que fazer quando um caminho falha, política de retry. Isso se testa contra um dublê, roda no
   Linux e no Windows, e é onde moram os bugs interessantes.
2. **A chamada de sistema fica atrás de um portão que RECUSA por padrão.** Só o binário do Agent,
   rodando de verdade, libera. Qualquer outro chamador — inclusive teste — recebe erro.
3. **Teste que precise da chamada real não existe em `go test ./...`.** Se for indispensável, use
   uma build tag própria (ex.: `//go:build manualinput`) para que a suíte normal nunca o execute,
   e mova a verificação para o roteiro de teste manual, que uma pessoa roda sabendo o que vai
   acontecer.

Na dúvida sobre se algo age no sistema real: **não execute, e pergunte.**

## Safety rules

- Nunca commitar secrets, tokens, chaves privadas, cookies ou credenciais.
- Nunca gravar áudio bruto em logs.
- Nunca gravar contexto de janela/seleção/texto próximo em logs.
  - Exceção única (29-set-2026, autorizada pelo Mauro): o nome-base do executável em foco (ex.:
    `brave.exe`), só nas linhas WARN de `uia: foco inspecionado` — nunca título, classe, caminho
    ou texto. Ver `docs/ai-spec/09-security-privacy.md`.
- Nunca transmitir contexto de campo protegido/senha.
- Não executar comandos destrutivos de banco/servidor sem autorização explícita.
- Não alterar serviços ou aplicações que já rodam no servidor.
- Não publicar releases sem autorização.
- Não adicionar dependência de produção sem justificar no relatório da slice.

## Repository layout alvo

```text
/
├── cmd/
│   ├── agent/
│   └── server/
├── internal/
│   ├── agent/
│   ├── server/
│   ├── asr/
│   ├── context/
│   ├── dictionary/
│   ├── resolver/
│   ├── sync/
│   └── storage/
├── web/
├── migrations/
├── deploy/
├── scripts/
├── testdata/
├── docs/
│   ├── ai-spec/
│   └── adr/
├── CLAUDE.md
└── AGENTS.md
```

## Commands alvo

```bash
go test ./...
go vet ./...
go build ./cmd/agent
go build ./cmd/server

cd web
pnpm install
pnpm lint
pnpm test
pnpm build
```

## Definition of Done por slice

- critérios relacionados atendidos;
- testes passam;
- `go vet ./...` passa;
- frontend lint/test/build passam quando aplicável;
- nenhum secret real no diff;
- docs atualizadas se contrato/schema/fluxo/comando mudou;
- commit local criado;
- relatório final lista arquivos, comandos, testes e riscos.

## Stop and ask for human decision when

- exigir deploy, DNS, proxy/CDN, servidor real ou secrets reais;
- migration destrutiva/irreversível;
- precisar alterar serviços que já rodam no servidor;
- houver conflito entre documentos;
- escolha ASR depender de benchmark ainda não executado;
- solução exigir persistência de áudio;
- solução exigir screenshot;
- memória do ASR exceder de forma material os limites definidos.
