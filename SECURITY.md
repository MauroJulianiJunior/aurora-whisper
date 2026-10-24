# Segurança

## Como relatar uma falha

Use o **relato privado de vulnerabilidade do GitHub**: na aba **Security** deste repositório, clique em
**Report a vulnerability**. O relato fica visível só para você e para o mantenedor até a correção.
**Não abra issue pública** para falha de segurança.

Escreva em português, se puder. Diga o que acontece, como reproduzir e o que alguém consegue fazer
com isso.

## Escopo

- **O Windows Agent:** a API local do painel (que só escuta em loopback, `127.0.0.1`), o hook de
  teclado e a inserção do texto no campo com foco.
- **O servidor da nuvem** (`cmd/server`): o login, as sessões, o pareamento dos PCs, a sincronização
  e o isolamento entre contas.
- **O deploy** (`deploy/`) e os scripts que tratam segredo (`scripts/`).

Fora do escopo: uma nuvem específica que alguém hospede (cada uma é de quem a sobe), e os provedores
externos (Google, OpenAI, Anthropic, Backblaze).

## Cuidados ao relatar

- **Nunca cole ditado real, histórico, chave de API, token, cookie ou senha** em issue, pull request ou
  relato. Use texto inventado e chaves falsas.
- Não teste contra a nuvem de outra pessoa nem contra serviços de terceiros.

## Versões

A 0.1.x é uma **prévia**. As correções saem em uma versão nova (a 0.1.3, por exemplo), na página de
[Releases](https://github.com/MauroJulianiJunior/aurora-whisper/releases), e entram na `main`. Use sempre a
versão mais nova.

O modelo de ameaças e as regras de privacidade estão em
[`docs/ai-spec/09-security-privacy.md`](docs/ai-spec/09-security-privacy.md).
