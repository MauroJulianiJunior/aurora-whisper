# ADR-013 — O gatilho do ditado passa a ser uma combinação de modificadores

Status: Accepted
Data: 2026-09-20
Contexto: Sprint 4, depois do primeiro uso real do ditado

## O problema, encontrado usando o produto

Com o Agent rodando e a tecla F configurada, digitar `freio` produzia `refio`.

Não é lentidão, é **reordenação**, e sai direto do desenho. No `keydown` do F o
hook não tem como saber se aquilo vai virar a letra `f` ou começar uma gravação.
A resposta só existe quando chega o `keyup` (toque) ou passa o limiar de 700 ms
(hold). Até lá o `f` é engolido, para que nenhum caractere apareça se virar hold
(`AC-002`). Quando resolve como toque, a letra é devolvida por `SendInput`
(`AC-001`).

As teclas seguintes não são engolidas e seguem na hora. Quando o `f` sintético
chega, já tem duas letras na frente dele.

```
tecla física:   f↓        r↓ e↓      f↑
o que chega:              r  e       f   →  "refio"
```

Isso passou por dois roteiros de teste manual sem ser detectado, e foi Mauro
quem encontrou usando o produto. O `T1` do Sprint 1 pedia tocar F cinco vezes e
conferir `fffff` — uma sequência de uma letra só **não consegue** revelar troca
de ordem. O `T9` digitava uma frase que revelaria, e foi marcado como passou.

## A decisão

**O gatilho padrão passa a ser `Ctrl+Win`: uma combinação de modificadores, sem
tecla principal.**

Uma combinação de modificadores **não produz caractere nenhum**. Não há o que
engolir nem o que devolver. A classe inteira do problema deixa de existir — não
é um remendo, é a remoção da causa.

Decidido por Mauro em 20-set-2026. `Ctrl+Win` é também a convenção do Wispr
Flow, então é um gesto que ele já conhece.

## O conflito com o REQ-004, e por que ele não é violado

O `REQ-004` do `02-prd.md` diz, como **Must**: *"Modificadores puros não podem
ser tecla principal"*, e o `AC-004` trava isso na validação.

A intenção é clara e continua valendo: **Ctrl sozinho fica pressionado o tempo
todo** em uso normal — copiar, colar, navegar — e o ditado dispararia sem parar.

Uma combinação de **dois ou mais** não tem esse problema. Ninguém segura
`Ctrl+Win` por 700 ms sem querer.

Por isso a validação passou a exigir **pelo menos dois** modificadores em vez de
proibir modificadores: preserva exatamente o que o `REQ-004` protege e libera o
caso que ele não previu. Um modificador solto continua recusado, com erro
próprio.

## Este modo NUNCA suprime nada

Com tecla de letra, o Decider suprime o keydown e o keyup. Com combinação de
modificadores, **nada é suprimido**.

Isso é deliberado e o motivo é concreto: suprimir o keyup de um modificador
deixaria o Windows achando que a tecla continua pressionada. **Já aconteceu
neste projeto** — em 17-set-2026 um teste injetou entrada e deixou um
modificador preso, travando o teclado de quem estava usando a máquina. A regra
do `CLAUDE.md` sobre testes que agem no sistema real nasceu daquele episódio.

O preço de não suprimir é, no pior caso, o menu Iniciar abrir sem querer ao
soltar as teclas. Não vale trocar um incômodo por um teclado travado.

**Isso precisa ser verificado em máquina real**, e está no roteiro de teste
manual: se o menu Iniciar abrir ao terminar um ditado, a mitigação vira uma
decisão tomada com dado, e não uma suposição embutida agora.

## Verificação em máquina real

**20-set-2026** — com `Ctrl+Win` configurado, digitar `freio` produz `freio`. O
sintoma que motivou este ADR não se reproduz.

**26-set-2026 — o ditado dispara com a combinação.** Mauro segurou `Ctrl+Win`,
falou, soltou, e o texto entrou. O log do Agent confirma o caminho inteiro:

```
operator (): processing 'audio.wav' (149920 samples, 9.4 sec), 12 threads, lang = pt
winruntime: transcrição concluída attempt=2 modelo=whisper-small-q4_0 duracao=460.9789ms
clipboard: preserve concluído had_previous_text=true new_text_runes=79
clipboard: restore concluído restored_text_runes=62
winruntime: dictation attempt finished attempt=2 outcome=success reason=none text_runes=79
```

Vale registrar o que mais aparece aí: o clipboard de Mauro tinha conteúdo
(`had_previous_text=true`), foi usado para a inserção e **devolvido** (`restore
concluído`). É a garantia do `AC-023` funcionando em uso real, não em teste.

**27-set-2026 — o menu Iniciar NÃO abre.** Mauro verificou na máquina real.

Era a única pendência deste ADR, e o resultado é o melhor possível: o preço que
eu tinha aceitado como consequência de nunca suprimir tecla — "no pior caso, o
menu Iniciar abre sem querer" — **não se materializou**.

Vale registrar por quê, porque não é sorte: o Windows só abre o menu Iniciar
quando a tecla Win é solta **sem** que nenhuma outra tenha sido pressionada no
intervalo. Com `Ctrl+Win`, o Ctrl já quebra essa condição. A combinação de dois
modificadores é, por construção, imune ao efeito colateral que um modificador
sozinho teria.

Ou seja: a escolha de exigir DOIS modificadores — feita pelo `REQ-004`, para
evitar disparo acidental — também eliminou este problema, sem que ninguém
tivesse planejado isso.

**Este ADR está integralmente verificado em máquina real.**

## O que fica de fora, de propósito

- **Outra tecla pressionada durante o gatilho não cancela a gravação.** Com o
  ditado armado, `Ctrl+Win+D` continua chegando ao Windows e o ditado continua.
  O limiar de hold é o que evita disparo acidental nesses atalhos, que são
  rápidos. Se em uso real isso incomodar, cancelar vira uma mudança pequena.
- **A tecla de letra continua suportada.** Quem preferir F continua podendo
  configurá-la, com o defeito de ordem que a acompanha — agora documentado.

## Emenda (26-out-2026): o segundo gatilho, "ditar para nota" (ADR-022, N9)

A ADR-022 (E1, aprovada no padrão) acrescenta um segundo gatilho: **Ctrl+Win+Alt** manda o ditado para
uma nota, sem inserir no campo em foco. Ele é um superconjunto do Ctrl+Win, e a regra "tem o conjunto"
do Decider já arma o ditado com ele. Por isso o desenho é o de **não mexer na borda do hook**:

- `dictation.Gatilhos` envolve o Decider de sempre. Suprimir e notificar continuam **as decisões dele,
  evento por evento** — o teste por sorteio (`TestGatilhos_CtrlWinIgualAoDeSempre`) compara as duas
  coisas em 50 sequências aleatórias de teclas. A sessão é uma só; o que muda é uma marca, `ParaNota`,
  que vai no evento até o runtime.
- A marca é o **pico** da sessão: Ctrl, Win e Alt seguros juntos em algum momento entre armar e soltar.
  A ordem de apertar e de soltar não importa, e o Alt apertado depois de a gravação começar conta (decidir
  só no começo mandaria para o campo, sem aviso, a lista de quem apertou o Alt 50 ms depois).
- **Nada novo é suprimido.** A regra deste ADR — nenhum modificador suprimido — vale para os dois
  (`TestGatilhos_NuncaSuprimeModificador`).
- Um modificador só continua recusado, e o gatilho da nota não pode ser o do ditado nem estar contido
  nele (`ValidarGatilhoDaNota`): contido, o ditado de sempre deixaria de existir. A regra do decisor é
  geral (um conjunto da nota que não contém o do ditado arma a própria sessão), mas a tela só mostra as
  teclas; o que se muda é ligar e desligar. Desligado, Ctrl+Win+Alt é um ditado comum.
- A pausa do hook respeita a sessão de qualquer um dos dois (`Gatilhos.Armado`).

O que não foi medido: se Ctrl+Win+Alt colide com atalho de outro programa nesta máquina. O detector de
conflito (`internal/agent/conflito`) olha processos, não atalhos; o Wispr Flow, que usa Ctrl+Win, já é
avisado, e com o superconjunto ele dispararia junto do mesmo jeito. O roteiro
`docs/teste-manual-notas-n9.md` confere o Ctrl+Win antes de tudo.

## Consequência

`AC-001` ("tap curto produz função normal") e `AC-045` ("tecla com modifier
preserva comportamento normal") **não se aplicam** ao gatilho por combinação:
não há função normal a preservar, porque não há caractere. Os dois continuam
valendo para a configuração por tecla de letra.

O `03-acceptance-criteria.md` precisa registrar isso, em vez de deixar dois
critérios parecendo falhos numa configuração em que eles não fazem sentido.
