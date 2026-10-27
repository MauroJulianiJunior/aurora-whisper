# Teste manual de arrastar as notas para reorganizar (ADR-022, N2b)

Este roteiro é seu (do Mauro). Os testes automáticos cobrem a chave da ordem, a migration, as regras de grupo
e a rota no Go, e, no painel, a grade em ordem de leitura, o arrastar com o mouse e o toque, o teclado e o
desligado nos filtros, tudo com eventos de mentira e a geometria do navegador simulada. **Nenhum teste olha
pixel, sente o peso do arrasto nem roda num celular de verdade.** É isso que este roteiro confere.

Para quê: ver, no Agent de verdade e no seu navegador, que arrastar uma nota parece com o Keep (o cartão
levanta, os outros abrem espaço, soltar guarda), que a ordem fica depois de fechar e abrir o painel, e que o
teclado faz o mesmo.

Antes:
1. Feche o Agent antigo e confira no Gerenciador de Tarefas que o processo novo é que está aberto. A **migration
   `0012`** roda na subida: ela acrescenta a coluna `posicao` e dá a cada nota o lugar que ela já tinha. Ao
   abrir o painel, **as notas devem estar exatamente na ordem de antes** (fixadas primeiro, depois a última
   edição). Se a ordem mudou, pare aqui e anote.
2. Recompile o painel (`cd web && pnpm build`).
3. Tenha ao menos 10 notas, de tamanhos diferentes, duas ou três **fixadas**, uma ou duas com marcador e uma no
   Arquivo. Use o tema claro e o escuro.

## 1. Arrastar com o mouse (na grade de quadradinhos)
1. Aponte o mouse num cartão (o texto dele, não um botão), aperte e arraste uns 3 ou 4 pixels e solte: a nota
   **abre**, como sempre (isso é um clique).
2. Aperte e arraste de verdade, para a direita: o cartão **levanta** (cresce um pouco, ganha sombra, o cursor
   vira mão fechada) e segue o mouse. Os outros se afastam, **sem pular**, e um contorno tracejado mostra onde
   ele vai cair. Confira que o contorno é do tamanho do cartão e que não fica indo e voltando quando o mouse
   para entre dois cartões.
3. Solte: o cartão assenta no contorno. Nenhuma janela abre depois de soltar.
4. **Fechar e abrir o painel** (atualize a página, ou feche a aba e abra de novo): a nota está **onde você a
   deixou**.
5. Arraste um cartão para o **primeiro** lugar da seção e outro para o **último**. Os dois valem.
6. Arraste e aperte **Esc** no meio: o cartão volta para o lugar de antes, sem pedir nada (confira que a ordem
   não mudou depois de atualizar a página).
7. Arraste um cartão para perto da **borda de baixo** da janela com a página comprida: ela **rola sozinha**,
   e o cartão continua na mão. Idem na borda de cima.
8. Aperte num **botão** do cartão (fixar, cor, Mais) e arraste: o cartão **não** levanta (os botões são botões).
9. Mude as **colunas** (Mais colunas / Menos colunas, de 1 a 6): a ordem de leitura é a mesma (esquerda para a
   direita, de cima para baixo) e os cartões ocupam a coluna mais curta. Arraste nas várias larguras.
10. Estreite a janela: os cartões se refazem sem sobrepor. Alargue de novo.

## 2. Só dentro do grupo
1. Arraste uma nota **fixada** para o meio das outras: ela só se mexe entre as fixadas (o contorno não sai da
   seção FIXADAS). Solte onde quiser: ela fica na última posição das fixadas, ou na que você escolheu entre as
   fixadas.
2. Arraste uma nota **comum** para cima das fixadas: o mesmo, ao contrário.
3. **Fixar** uma nota comum (o alfinete do cartão): ela vai para o **topo** das fixadas. **Desafixar**: vai para
   o **topo** das outras.
4. Crie uma nota nova: ela entra no **topo** das outras.
5. **Edite** uma nota do meio (mude o título, a cor, marque um item): ela **fica onde estava**, não sobe.
6. Mande uma nota para a Lixeira e **restaure**: ela volta no topo do grupo dela.
7. Ditado para nota (Ctrl+Win+Alt, "Título: coisa nova") numa nota do meio: a nota recebe o texto e **fica onde
   estava**.

## 3. O teclado
1. Dê **Tab** até um cartão (o foco visível é o da nota). Aperte **Alt + →**: ele anda uma vaga para a
   direita; o foco **continua nele**. O navegador **não** volta de página.
2. **Alt + ↓** anda uma linha inteira (o número de colunas); **Alt + ←** e **Alt + ↑** voltam.
3. No primeiro ou no último lugar do grupo, a seta que não anda não faz nada.
4. Com um leitor de tela (o Narrador do Windows: Win+Ctrl+Enter), cada movimento anuncia
   **"Movida para a posição 3 de 8."**; na ponta, **"Já está na posição 1 de 8."**. A posição é a do grupo (uma
   fixada conta entre as fixadas).
5. Sem Alt, as setas se comportam como antes (nada se move).

## 4. A lista e os detalhes
1. Troque para **Lista**: arraste e use Alt + ↑/↓. O mesmo: levanta, abre a vaga, guarda.
2. Troque para **Detalhes** (a tabela): arraste uma linha (o título ou qualquer célula, menos os botões); as outras linhas se afastam e
   ela cai no lugar. Alt + ↑/↓ também.
3. Troque de forma de ver: a ordem é a mesma nas três.

## 5. Onde não deve arrastar
1. Clique num **marcador** da fileira (filtro): arraste um cartão. Ele **não** levanta, e a pílula diz "Para
   reorganizar, saia do filtro de marcador: volte para “Notas”.". Alt + seta diz o mesmo.
2. Digite algo na busca e use **Ver todas as notas**: arrastar diz "Para reorganizar, limpe a busca.".
3. No **Arquivo**: "O Arquivo fica na ordem em que as notas foram arquivadas." (a mais recente em cima).
4. Na **Lixeira**, nada arrasta.

## 6. Quando o Agent não responde
1. Com o painel aberto, **pare o Agent** (saia pela bandeja) e arraste uma nota. O cartão assenta, mas o
   pedido não vai: o painel avisa e a ordem de antes **volta**. Abra o Agent de novo e atualize: a ordem é a
   de antes do arrasto.
2. (Opcional) Abra o painel em **duas abas**, mova uma nota na primeira e, na segunda, mova a mesma nota para
   outro lugar. A segunda funciona (a ordem é por posição, não por versão); se uma das duas vizinhas já não
   estiver no mesmo grupo (você a fixou na outra aba), o aviso é "A lista das notas mudou em outra aba. Li de
   novo; repita se ainda quiser." e a lista se acerta.

## 7. Movimento reduzido
No Windows, **Configurações > Acessibilidade > Efeitos visuais > Efeitos de animação**, desligue. Arraste: o
cartão levantado ainda segue o mouse, mas os outros **pulam** para o lugar, sem deslizar.

## 8. O que olhar com atenção
- O cartão levantado nunca fica atrás dos outros, e nunca perde o texto.
- Não aparece barra de rolagem horizontal durante o arrasto.
- Nenhum cartão sobrepõe outro no repouso, em nenhum tema nem número de colunas.
- A janela da nota (clicar) continua abrindo, com o foco certo, e fechar devolve o foco ao cartão.
- Se algo estiver estranho, anote o passo, o tema e o número de colunas.

## 9. Fica para depois (N8, celular)
No celular, o gesto é **segurar e arrastar**: segurar um cartão por uns 400 ms (o aparelho vibra de leve), e
então arrastar; mexer o dedo antes disso é só rolar a página. Foi feito e testado com eventos de toque
simulados, **mas não foi visto num celular de verdade**: só dá para conferir quando o painel abrir no celular
(N8). Pontos a olhar lá: a página não rola enquanto o cartão está levantado, o menu de "segurar" do navegador
não aparece, e a vibração acontece.
