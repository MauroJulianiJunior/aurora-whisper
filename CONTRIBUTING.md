# Como contribuir

Issues e pull requests são bem-vindos, **em português**. Falha de segurança não vai em issue: veja o
[`SECURITY.md`](SECURITY.md).

## Antes de começar

- A especificação começa em `docs/ai-spec/00-index.md`; os critérios de aceitação estão em
  `docs/ai-spec/03-acceptance-criteria.md`, e as decisões, em `docs/adr/`.
- Para uma mudança grande, abra antes uma issue com o que você pretende fazer.
- O Agent precisa funcionar sem a nuvem. Áudio nunca vai para disco nem para log, e o contexto de
  outras janelas nunca é guardado nem sincronizado.

## Testes

Rode da raiz do repositório:

```bash
go test ./...
```

```bash
go vet ./...
```

```bash
go build ./...
```

```bash
pnpm -C web install
```

```bash
pnpm -C web lint
```

```bash
pnpm -C web test
```

```bash
pnpm -C web build
```

**Teste nunca age no sistema real.** Nada de `SendInput`, `keybd_event`, escrita no clipboard, hook
global ou janela nova na suíte normal: a lógica de decisão se testa contra um dublê, e a chamada de
sistema fica atrás de um portão que recusa por padrão. As regras estão em `CLAUDE.md`, em "Testes que
agem no sistema real".

## Commits

[Conventional Commits](https://www.conventionalcommits.org/pt-br/), em português. Exemplos:

```
feat(painel): mostrar a data do ditado
fix(agent): não perder o modificador ao soltar a tecla
docs(deploy): explicar o PROXY_CONFIAVEL
```

## Nada de segredo nem de dado real

- Nenhum segredo no repositório: chave de API, token, senha, cookie, `.env`, JSON do Google, chave do
  `age`. O CI roda o gitleaks no histórico inteiro.
- Nenhum dado real: nada de ditado, histórico, áudio ou banco de verdade em teste, fixture, issue ou
  pull request. Use texto e nomes inventados.
