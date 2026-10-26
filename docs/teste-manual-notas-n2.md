# Teste manual das Notas no painel (ADR-022, N2)

Este roteiro é seu (do Mauro). Nada aqui foi feito pelos testes automáticos: eles simulam colar,
teclar e digitar com eventos do navegador, nunca com o Agent de verdade nem com a janela em foco.

Para quê: confirmar que o editor da nota aguenta o jeito como o Aurora Whisper escreve (o ditado chega
colado, às vezes com várias linhas) e que a tela está como a prévia que você aprovou.

Antes: compile o painel (`cd web && pnpm build`), recompile o Agent e abra-o de novo (feche o antigo
primeiro e confira que o processo é o novo), e abra o painel pela bandeja.

## 1. O olhar
1. Abra a aba **Notas** (fica entre Histórico e Lixeira). Ela deve ocupar a largura da janela.
2. Crie 3 ou 4 notas com cores diferentes. Olhe no tema claro e no escuro (muda no Windows).
3. Troque a forma de ver (quadradinhos, lista, detalhes) e as colunas. Feche e abra o painel: a escolha
   continua.
4. Encolha a janela até ficar estreita: deve cair para 2 colunas, e as ferramentas do cartão ficam à vista.
5. Se algo estiver diferente de `docs/notas-previa-n2.html`, anote o que e onde.

## 2. Digitar na nota
1. **Criar uma nota…**, escreva um título e, no corpo, digite `- ovos`, Enter, `leite`, Enter, Enter. A
   lista deve continuar a cada Enter, e o segundo Enter seguido deve sair da lista.
2. Digite `[] pão` no começo de uma linha: vira caixa de marcar. Marque: o item desce para baixo de
   "N marcados".
3. No começo de uma linha da lista, Backspace: tira o marcador; outro Backspace junta com a de cima.
4. Feche a caixa com Fechar: a nota aparece na grade. Reabra, mude o texto, espere um segundo: ela salva
   sozinha (a hora de "editada às" muda).

## 3. Ditar dentro da nota (o ponto de atenção)
Aqui o Agent insere o texto no campo que tem o foco, como em qualquer programa.
1. Abra uma nota e clique numa linha vazia do corpo.
2. Segure a tecla de ditado e diga uma frase curta; solte. A frase deve entrar **na linha**, sem apagar o
   que já estava e sem duplicar.
3. Ditar uma frase com a palavra "item" ou frases em sequência: cada ditado vai para a linha em foco.
4. Se o Agent colar mais de uma linha (por exemplo um ditado longo com quebra), cada linha deve virar uma
   linha da nota; um `- ` ou `1. ` no começo vira lista.
5. Logo depois de ditar, aperte Enter com o teclado: deve dividir a linha normalmente.
6. Esc fecha a nota salvando; o foco volta ao cartão.
7. **Ditado de enumeração numa lista** (correção de 25-out-2026): "Criar uma nota…" → o ícone da lista
   de marcar → dite "arroz, feijão, batata, presunto, queijo, tomate e alface". Em menos de um segundo,
   cada item deve virar uma caixinha, sem a vírgula e com maiúscula. Vale também numa linha com `•`.
8. **Ditado de enumeração em texto comum:** numa nota sem lista, dite a mesma frase. O texto fica como
   você falou, e no fim da linha aparece o botão **Virar lista**; clicar nele separa em caixinhas.
9. **Nota nova começa no corpo:** "Criar uma nota…" e já dite; o texto entra no corpo, não no título.

Se algo falhar (texto no lugar errado, linha duplicada, cursor que pula, Enter que não responde depois do
ditado), anote a ordem exata dos passos: é o que a gente não consegue ver nos testes.

## 4. Conflito
1. Abra a mesma nota em duas abas do painel. Mude o texto numa e feche. Na outra, mude e espere salvar.
2. Deve aparecer a mensagem vermelha, sem perder o que você digitou, com **Salvar por cima**.

## 5. Arquivo e Lixeira
1. Arquive uma nota (Desfazer no aviso devolve). Ela aparece em **Arquivo**, com Desarquivar.
2. Mande uma para a Lixeira, restaure, mande de novo e use **Excluir para sempre**: a pergunta vem na linha,
   com o foco em Cancelar.

## 6. Copiar texto
**Mais**, **Copiar texto**, e cole num bloco de notas: o texto da nota. (Mexe na sua área de transferência;
por isso é manual.)
