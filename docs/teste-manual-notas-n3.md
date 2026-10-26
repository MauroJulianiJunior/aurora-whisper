# Teste manual do "Virar nota" (ADR-022, N3)

Este roteiro é seu (do Mauro). Os testes automáticos cobrem o banco, as rotas e o painel contra um Agent
de mentira; eles nunca usam o Agent de verdade, a tecla de ditado nem a área de transferência.

Para quê: confirmar, no PC, que um ditado vira nota sem sumir de verdade, que o Abrir e o Desfazer fazem o
que dizem e que o histórico não "perde" nada.

Antes: recompile o painel (`cd web && pnpm build`), recompile o Agent e abra-o de novo (feche o antigo
primeiro e confira que o processo é o novo), e abra o painel pela bandeja. A migration `0010` roda sozinha
na subida e só acrescenta uma coluna: o histórico que você já tem continua inteiro.

## 1. O botão
1. Dite duas ou três frases (uma delas uma lista: "- leite", "- café"), e abra uma no **Histórico**.
2. No editor, ao lado do **Copiar**, deve ter **Virar nota**.
3. Mude uma palavra no texto sem salvar: o botão fica desligado (a dica diz para salvar antes). Salve: o
   editor fecha; abra de novo, e o botão volta a ficar ligado.

## 2. Virar nota, Abrir e Desfazer
1. Clique **Virar nota**. A linha sai do Histórico (a busca também não a acha) e embaixo aparece
   **"Virou nota"**, com **Abrir** e **Desfazer**.
2. Clique **Abrir**: o painel vai para a aba **Notas** e a nota abre na janela dela, com o texto do ditado
   no corpo, sem título. O cartão da nota diz "veio de um ditado".
3. Feche a janela, volte ao Histórico e à aba Notas de novo: a nota não reabre sozinha.
4. Outro ditado: **Virar nota** e logo **Desfazer** (ou Ctrl+Z, fora de campo de texto). A linha volta ao
   lugar dela no Histórico, e na aba Notas não sobra nota.
5. Para testar a nota editada: **Virar nota** num ditado; sem usar o aviso, abra a aba **Notas**, edite a
   nota e volte ao Histórico (o aviso espera 8 segundos, e espera mais enquanto o ponteiro está nele; se
   sumir, repita mais depressa). **Desfazer**: o ditado volta, e o aviso diz "A nota ficou, porque você a
   editou.".

## 3. O que não pode acontecer
1. O ditado que virou nota **não** está na **Lixeira**.
2. Apague a nota (Lixeira das notas, e depois **Excluir para sempre**): o ditado **não** volta ao
   Histórico (é a regra da ADR; ele só fica escondido).
3. Uma lista ditada ("- leite" em linhas separadas) vira lista de marcar na nota? Não precisa: ela chega
   como texto com `- `, e o editor a lê como lista. Uma enumeração em texto corrido ("arroz, feijão e
   pão") chega como texto, e o botão **Virar lista** da nota a separa.
4. Nuvem: se o PC está conectado, o ditado continua aparecendo no painel da nuvem e no celular (o "virou
   nota" é só deste PC por enquanto, ponto aberto da N7).

Se algo falhar, anote a ordem exata dos passos e se o Agent mostrou algum aviso vermelho.
