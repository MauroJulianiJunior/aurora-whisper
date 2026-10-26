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

## 4. Ditar dentro da nota (não pode aparecer no Histórico)
Aqui o Agent de verdade registra o ditado, então este roteiro precisa do Agent novo aberto e do painel novo
(`pnpm build`). Os ditados que já estão no Histórico **não mudam**: só os novos nascem escondidos.
1. Abra uma nota existente (aba **Notas**) e deixe o cursor no corpo dela. Segure `Ctrl+Win`, fale uma
   frase, solte. O texto entra na nota como sempre. Volte ao **Histórico**: a frase **não** está lá.
2. Com a nota ainda aberta, clique num botão da janela dela (por exemplo **Fixar**), de modo que o cursor
   saia dos campos, e dite outra frase em outro lugar (outro aplicativo). Essa frase **aparece** no
   Histórico (o aviso saiu junto com o foco).
3. **O ponto a conferir (Ctrl+Win):** com o cursor na nota, segure `Ctrl+Win` e solte. O painel **não
   pode** perder o foco com isso (a pílula não rouba o foco, e o Windows não deve mexer na janela do
   navegador). Se a frase de um ditado feito logo na primeira vez aparecer no Histórico, o foco foi perdido
   no meio: anote o navegador e o que apareceu na tela.
4. Mude de janela com `Alt+Tab` para outro aplicativo e dite lá: aparece no Histórico (a janela do
   navegador perdeu o foco). Volte ao navegador, com o cursor ainda na nota, e dite: some do Histórico de
   novo.
5. **Criar uma nota…:** abra a caixa, dite duas frases dentro dela e clique fora (ou `Ctrl+Enter`). A nota
   nasce com as frases, e o Histórico **não** tem nenhuma delas.
6. **Caixa vazia:** abra **Criar uma nota…**, dite uma frase, apague o texto inteiro e feche com `Esc`.
   Nenhuma nota é criada, e a frase ditada **volta ao Histórico** (nada se perde calado).
7. Feche o navegador com a nota aberta e o cursor no campo, e dite logo: pode aparecer fora do Histórico
   por até 60 segundos (o aviso vence sozinho). Depois disso, volta ao normal.
8. Nuvem: o painel da nuvem não tem nada disso, e o ditado feito dentro da nota continua subindo para o
   histórico da nuvem (ponto aberto da N7).

Se algo falhar, anote a ordem exata dos passos e se o Agent mostrou algum aviso vermelho.
