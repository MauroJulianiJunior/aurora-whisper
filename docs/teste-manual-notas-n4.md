# Teste manual dos marcadores (ADR-022, N4)

Este roteiro é seu (do Mauro). Os testes automáticos cobrem o painel contra um Agent de mentira e a regra da
sugestão por tabela; nenhum olha pixel. A referência do desenho é `docs/notas-previa-n4.html`.

Para quê: ver no navegador, com o Agent de verdade, que criar, renomear, apagar e marcar funciona e que a
sugestão aparece quando faz sentido e nunca marca sozinha.

Antes: recompile o painel (`cd web && pnpm build`) e abra o Agent de novo (feche o antigo primeiro e confira
que o processo é o novo). Nada muda no Agent nesta etapa; só o painel é novo. Use o tema claro e o escuro.

## 1. Editar marcadores
1. Na aba **Notas**, o último chip da fileira é **Editar marcadores**, com borda tracejada. Clique: abre uma
   janela pequena, com o cursor em "Criar marcador".
2. Digite `Compras` e Enter. O marcador aparece na lista ("nenhuma nota") e como chip na fileira.
3. Digite `compras` (minúscula) ou `COMPRAS`: aparece `Já existe o marcador "Compras".` em vermelho e o
   **Criar** fica desligado. O mesmo vale sem acento (`escritorio` contra `Escritório`).
4. Crie mais dois (`Escritório`, `Ideias`). Clique no lápis de um: o nome vira um campo; mude e Enter salva; Esc
   cancela sem fechar a janela. Tente renomear para o nome de outro: recusa na linha.
5. Clique na lixeirinha de um marcador que tem notas: a pergunta aparece **na própria linha**: `Apagar "X"? Ele
   sai das N notas; as notas ficam.` O foco está no **Cancelar**. Cancele; depois apague de verdade: o chip
   some e as notas continuam lá, sem a etiqueta.
6. Esc e **Fechar** fecham a janela, e o foco volta ao chip **Editar marcadores**.

## 2. Marcar uma nota
1. Abra uma nota e clique no ícone de etiqueta da barra de baixo. O cursor vai para o campo do menu.
2. Clique numa caixinha: marca na hora e a etiqueta aparece na nota, com um **×**. Clique de novo: desmarca.
3. Digite um nome que não existe: aparece `Criar marcador "..."`. Enter (ou clique) cria e já põe na nota.
4. Teclado: setas movem a linha, Enter marca/desmarca, Esc fecha o menu sem fechar a nota.
5. Marque dois marcadores na mesma nota. Clique no **×** de um: ele sai.
6. Mude o título da nota **logo depois** de marcar e feche: não pode aparecer aviso vermelho de "alterada
   em outra aba".
7. Passe o mouse num cartão da grade: a fileira de ferramentas tem o mesmo ícone de etiqueta, com o mesmo
   menu. Marque ali e confira a etiqueta no cartão e o número no chip da fileira, sem recarregar.
8. Num cartão da coluna da direita, o menu abre inteiro (não corta na borda da janela)? Anote se cortar.

## 3. A sugestão
1. Tenha um marcador com pelo menos **2 notas** (por exemplo, duas listas de compras com leite, café, pão).
2. Crie uma nota nova, sem marcador, e escreva uma lista parecida (leite, ovos, pão). Cerca de meio segundo
   depois de parar de digitar aparece, dentro da nota, uma faixa tracejada: **Marcar como Compras?**, com
   **Marcar** e **Agora não**. Ela não marca sozinha.
3. **Marcar** põe o marcador (a etiqueta aparece e a faixa some). **Agora não** some com a faixa; feche e
   abra a mesma nota: ela não volta. Em outra nota parecida, a faixa volta a aparecer.
4. Com um marcador de **uma nota só**, nenhuma faixa aparece. Numa nota que já tem marcador, também não.
5. Apague o texto que a motivou: a faixa some.

## 4. O que olhar com atenção
- O verde só nos colchetes da janela e nas caixinhas marcadas; o vermelho só no aviso de nome repetido e no
  **Apagar**.
- Nada de janela por cima de janela: tudo acontece na janela de marcadores ou na própria nota.
- Se algo estiver estranho, anote o passo e o tema (claro ou escuro).
