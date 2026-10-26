# Teste manual de Copiar para WhatsApp e da busca única (ADR-022, N6)

Este roteiro é seu (do Mauro). Os testes automáticos cobrem a conversão para WhatsApp por tabela, a cópia com
a área de transferência e a folha de compartilhar de mentira, e a busca contra um Agent de mentira; nenhum
cola no WhatsApp de verdade nem olha pixel. A referência do desenho da busca é `docs/notas-previa-n2.html`,
cena 5.

Para quê: ver, com o Agent de verdade, que o texto colado no WhatsApp chega com o negrito, os `•`, os `☐` e
os `✅` certos, e que a busca acha nota e ditado juntos sem atrapalhar o resto.

Antes: recompile o painel (`cd web && pnpm build`) e abra o Agent de novo (feche o antigo primeiro e confira
que o processo é o novo). A única mudança no Agent é a busca do histórico sem acento. Use o tema claro e o
escuro.

## 1. Copiar para WhatsApp
1. Crie uma nota com título `Mercado`, uma seção `# Padaria`, dois itens com marcador, uma lista de marcar com
   um item marcado e um aberto, e uma lista numerada de dois itens.
2. Abra a nota, clique em **Mais** e em **Copiar para WhatsApp**. A pílula diz "Copiado para o WhatsApp.".
3. Abra o **WhatsApp Web**, cole numa conversa (pode ser a de "Você") e **não envie ainda**. Confira: o título
   e a seção em negrito (sem asteriscos soltos), `•` nos marcadores, `☐` no aberto, `✅` no marcado, a
   numerada como `1.` e `2.`, uma linha em branco depois do título e antes da seção.
4. Envie e veja como ficou na bolha. Anote o que estiver estranho.
5. Passe o mouse num cartão da grade, abra o **Mais** dele e copie por ali: o mesmo texto?
6. Edite o corpo na nota aberta **sem esperar salvar** e copie: sai o que está na janela, não o antigo?
7. Uma nota sem texto algum: a pílula diz "A nota está vazia: não há o que copiar.".
8. Um título com `*` no meio (`Preço * 2`): o negrito não quebra.

## 2. A busca única
1. Tenha ao menos uma nota e um ditado com a mesma palavra (por exemplo, `reunião`). Na aba **Histórico**,
   Ctrl+K leva ao campo "Buscar nas notas e nos ditados".
2. Digite a palavra. Pouco depois a lista dá lugar a dois grupos: **NOTAS · n** e **DITADOS · n**, com o trecho
   que casa destacado. Confira nos dois temas: o destaque se lê, o título da nota está em Space Grotesk, a
   bolinha tem a cor da nota.
3. Digite sem acento e em maiúscula (`REUNIAO`): acha os mesmos, nos dois grupos.
4. Mais de 5 resultados num grupo: aparece **Ver todas as N notas** (ou **Ver todos os N ditados**). Clique:
   vai para a aba do grupo com a lista filtrada, e uma linha embaixo do campo traz **Ver notas e ditados**.
5. Clique numa **nota**: abre a janela dela sobre os resultados. Feche (Esc): o foco volta ao resultado.
6. Clique num **ditado**: abre no Histórico, com o editor dele.
7. Teclado: seta para baixo no campo vai ao primeiro resultado; setas andam; para cima do primeiro volta ao
   campo; Enter no campo abre o primeiro; **Esc** limpa a busca e a aba volta como estava.
8. Termo que não existe: "Nada encontrado para “…”.". Pare o Agent no meio de uma busca (se quiser testar o
   erro): o aviso diz o que houve e há **Tentar de novo**.
9. Na aba **Lixeira** do topo a busca é a de sempre (só da lixeira).

## 3. O que olhar com atenção
- Nada de janela por cima de janela, e a aba Notas não pisca por trás dos resultados.
- O foco nunca se perde: depois de fechar a nota, de apagar a busca ou de voltar de "Ver todas".
- Se algo estiver estranho, anote o passo e o tema (claro ou escuro).

## 4. Fica para depois (N8, celular)
No celular, "Copiar para WhatsApp" abre a folha de compartilhar do Android (`navigator.share`): escolher o
WhatsApp e ver o mesmo texto formatado. Só dá para testar quando o painel abrir no celular.
