# Defeito: a tecla de ditado sai fora de ordem ao digitar

Status: **RESOLVIDO** em 20-set-2026 — o gatilho padrão passou a ser `Ctrl+Win`,
uma combinação de modificadores que não produz caractere. Sem caractere para
engolir e devolver, a classe inteira do problema deixa de existir. Ver
`docs/adr/ADR-013-gatilho-por-combinacao-de-modificadores.md`.

**Verificado em máquina real em 20-set-2026:** com o gatilho em `Ctrl+Win`,
digitar `freio` produz `freio`. O defeito que apareceu em uso normal, e que dois
roteiros de teste manual deixaram passar, não se reproduz mais.

O texto abaixo fica como registro do diagnóstico e das saídas avaliadas — a
tecla de letra continua suportada para quem preferir, com este defeito.

Status original: **aberto**, sem correção definida
Encontrado: 17-set-2026, por Mauro, usando o Agent em uso normal
Severidade: **alta** — afeta digitação comum, não o ditado

## O sintoma

Com o Agent rodando, digitar `freio` produz `refio`. O `f` aparece depois do `r` e do `e`.

## O mecanismo

Não é lentidão. É **reordenação**, e é consequência direta do desenho.

O hook não tem como saber, no `keydown` do F, se aquilo vai ser um toque (digitar `f`) ou um hold
(começar a gravar). A resposta só existe quando uma das duas coisas acontece:

- chega o `keyup` → era toque
- passa o limiar de 700 ms → era hold

Enquanto isso, o núcleo emite `SuppressKeyEffect`: o `f` físico é engolido para que, se virar hold,
nenhum `f` apareça na tela (`AC-002`). Quando resolve como toque, o runtime devolve a tecla com
`SynthesizeTapEffect`, sintetizando o `f` via `SendInput`.

O problema é o que acontece **no meio**: as teclas seguintes (`r`, `e`, ...) não são suprimidas e
seguem para a aplicação na hora. Quando o `f` sintético finalmente chega, já tem duas letras na
frente dele.

```
tecla física:   f↓        r↓ e↓      f↑
o que chega:              r  e       f
resultado:                r  e  f  i  o   →  "refio"
```

A janela de bagunça é exatamente o tempo em que a tecla fica fisicamente pressionada. Pelo relato
(duas letras à frente), algo entre 150 e 250 ms.

## Por que os testes não pegaram

Dois testes manuais deveriam ter coberto isto e não cobriram:

- **T1** (`AC-001`, "toque curto preserva a tecla") pede para tocar F cinco vezes e conferir se
  saem `fffff`. Só F. Uma sequência de uma letra só **não consegue** revelar reordenação — a saída
  é idêntica em qualquer ordem.
- **T9** ("digitação normal continua intacta") pede para digitar
  `refrigerador fofoca afinal difícil fácil`, que tem F no meio de palavras e revelaria o defeito.
  Foi marcado como passou. Ou a digitação naquele momento foi mais lenta que a janela, ou a troca
  passou despercebida na leitura.

A lição para o roteiro: um teste de ordem precisa de **letras diferentes**, e precisa de alguém
comparando caractere a caractere, não batendo o olho.

## Por que não tem correção barata

Toda saída custa alguma coisa. Nenhuma é de graça:

| Caminho | O que resolve | O que custa |
|---|---|---|
| **1. Segurar também as teclas seguintes** e devolver tudo na ordem | ordem correta, sem `f` fantasma | a digitação inteira trava enquanto o F estiver pressionado. Em português, F é frequente — seria um engasgo constante. E aumenta muito a injeção sintética |
| **2. Deixar o `f` passar e apagar com backspace** se virar hold | nenhuma trava, ordem correta | um `f` pisca e some. Pior: backspace é destrutivo — se o foco mudou no meio, apaga caractere de outro campo |
| **3. Trocar a tecla padrão** por uma que não se digita (Ctrl direito, CapsLock, F13, botão lateral do mouse) | elimina a classe inteira do problema | muda o padrão que a spec definiu; exige decisão de produto |
| **4. Exigir modificador** (ex.: segurar Ctrl+Espaço) | elimina o problema | ergonomia pior para o gesto principal do produto |
| **5. Heurística de rajada**: se outra tecla veio há menos de X ms, tratar como digitação | reduz o caso comum | cria um modo em que o ditado silenciosamente não começa — e falha silenciosa é pior que o defeito |

## O que eu recomendo, e por quê

**A raiz é usar uma tecla de letra para hold-to-talk.** Qualquer letra tem este problema, porque
qualquer letra é digitada no meio de palavras. Não é um defeito do F: é do desenho de "segure uma
tecla que também serve para escrever".

O caminho 3 é o único que elimina a classe. Os caminhos 1 e 2 trocam este defeito por outro
defeito, e o 5 troca por um pior.

Isso **não** impede manter tecla de letra como opção configurável — impede tê-la como padrão sem
avisar do custo.

**Decisão é de Mauro**, não minha. O que está registrado aqui é o custo de cada caminho.

## O que este defeito NÃO afeta

- **o ditado em si**: segurar F continua gravando e inserindo normalmente
- **o bloqueio em campo de senha** (`AC-036`)
- **atalhos com modificador** (`Ctrl+F` etc.), que passam intactos por caminho separado

É um defeito da digitação comum enquanto o Agent está ligado — que, sendo um programa que roda o
dia inteiro em segundo plano, é praticamente o tempo todo.
