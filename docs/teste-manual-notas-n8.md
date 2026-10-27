# Teste manual das notas no celular (ADR-022, N8)

Este roteiro é seu (do Mauro), e vem **depois** do `docs/deploy-notas-n8.md`: a nuvem já tem de estar com a
API nova no ar. Os testes automáticos cobrem as regras (a mescla, o selo, a fila sem rede, o "Ditar para
nota", o Desfazer) contra uma nuvem de mentira; eles nunca usam um celular de verdade, o sinal que cai de
verdade nem o dedo. Só aqui isso acontece.

**Para quê:** confirmar que a aba Notas do celular é a do PC; que o "Ditar para nota" e o microfone dentro da
nota levam o ditado para a nota certa, sem ele aparecer no histórico; que no mercado sem sinal dá para marcar,
desmarcar e acrescentar item, e que isso sobe sozinho quando o sinal volta, sem perder nada; e, com o celular
fazendo o papel do segundo aparelho que a N7 pedia, o conflito com selo, a mescla da lista de marcar e a ordem
entre aparelhos.

**O que você precisa:**
- o **celular** com o app da nuvem instalado (o ícone na tela inicial) e entrado na sua conta;
- o **PC** com o Agent ligado e conectado à mesma conta, e o painel aberto pela bandeja;
- o **ditado pelo celular ligado** (Configurações do app → "Ditar pelo celular": Ligado, com a chave do
  provedor). Sem ele, pule as partes 3 e 4.

**Quanto espera:** o PC busca a nuvem a cada 60 s (a cada 10 s com o painel aberto). Depois de mexer numa nota
num aparelho, dê até um minuto antes de dizer que não chegou ao outro. No celular, voltar à aba Notas (ou
puxar a tela para baixo) lê a lista de novo.

**Se algo não sair como está escrito:** pare naquele passo, anote o número do passo e o que apareceu (um
print ajuda), e siga para a próxima parte. No fim, me conte tudo.

## 0. Antes

1. No **PC**, abra o painel pela bandeja e vá à aba **Notas**. Crie:
   - uma **lista de marcar** com título **Mercado N8** e quatro itens: Arroz, Feijão, Leite, Pão (abertos);
   - uma nota com título **Teste N8** e o texto "primeira versão";
   - um marcador chamado **Compras N8** (em **Editar marcadores**), e ponha-o em **Mercado N8**.
2. Espere um minuto.
3. No **celular**, abra o app. Se ele estava aberto desde antes do deploy, feche-o de vez (tire da lista de
   apps abertos) e abra de novo, para pegar a versão nova.

## 1. A aba Notas no celular

1. No celular, no alto, ao lado de **Histórico**, toque em **Notas**.
   **Deve acontecer:** aparecem as notas do PC, em **duas colunas**, com a cor e o marcador; **Mercado N8**
   mostra os quatro itens com as caixinhas. Embaixo da busca, a fileira dos marcadores (**Compras N8**
   entre eles); embaixo de tudo, no meio, o botão verde **"Ditar para nota"**.
2. Olhe o campo de busca no alto.
   **Deve acontecer:** na aba Notas, **não** há o microfone do lado da busca, e o campo diz "Buscar nas
   notas".
3. Toque em **Histórico**.
   **Deve acontecer:** o microfone volta ao lado da busca, e o "Ditar para nota" some.
4. Volte para **Notas** e use o **− 2 +** para mudar as colunas (1, 2, 3). Deixe em 2.

## 2. Escrever no celular, ver no PC (e o contrário)

1. No celular, toque em **"Criar uma nota…"**, escreva o título **Celular N8** e o texto "escrito no
   celular", e toque fora da caixa.
   **Deve acontecer:** a nota aparece no topo da lista.
2. Abra **Celular N8**, mude a cor para **azul** (o botão da paleta), ponha o marcador **Compras N8** (o
   botão de marcador) e toque em **Fechar**.
3. No PC, espere até um minuto (a aba Notas aberta).
   **Deve acontecer:** **Celular N8** aparece no PC, azul, com **Compras N8**.
4. No PC, abra **Teste N8**, troque o texto para "segunda versão" e feche.
5. No celular, volte à aba Notas (toque em Histórico e de novo em Notas).
   **Deve acontecer:** **Teste N8** diz "segunda versão".
6. No celular, abra **Celular N8**, toque nos três pontinhos (**Mais**) → **Mover para a lixeira**.
   **Deve acontecer:** o aviso "Nota movida para a lixeira." com **Desfazer**. No PC, em um minuto, a nota
   sai da lista e aparece na **Lixeira** das notas.
7. No celular, vá à **Lixeira** (na fileira dos marcadores) e toque em **Restaurar** em **Celular N8**.
   **Deve acontecer:** ela volta ao topo das notas, no celular e, em um minuto, no PC.

## 3. "Ditar para nota"

1. No celular, na aba Notas, toque em **"Ditar para nota"** (na primeira vez, o celular pede a permissão do
   microfone: permita).
   **Deve acontecer:** o botão vira uma faixa escura com **"Para nota"**, a onda e o tempo.
2. Fale, com uma pausa entre os itens: **"Mercado N8: queijo, presunto e manteiga"**. Toque no botão de
   parar (o quadradinho verde).
   **Deve acontecer:** "Transcrevendo…" e, em seguida, o aviso **"Mercado N8 +3"** com **Abrir** e
   **Desfazer**. Em **Mercado N8** entram **Queijo**, **Presunto** e **Manteiga** como itens de marcar,
   depois do Pão e antes dos marcados. (Se vierem como um item só, "Queijo presunto e manteiga", a
   transcrição veio sem as vírgulas: é a regra das listas, a mesma do PC. Anote, e conte se veio assim.)
3. Toque em **Abrir** no aviso.
   **Deve acontecer:** a nota **Mercado N8** abre, com os três itens novos.
4. Feche a nota. Vá ao **Histórico** do celular.
   **Deve acontecer:** o ditado "Mercado N8: queijo…" **não** está no histórico. No PC, em um minuto,
   também não; mas **Mercado N8** no PC tem os três itens.
5. Volte a **Notas**, toque em **"Ditar para nota"** e fale: **"Ligar para o contador amanhã cedo"**. Pare.
   **Deve acontecer:** o aviso **"Nota nova"**; no topo das notas, uma nota sem título com esse texto, que
   diz "veio de um ditado" quando aberta.
6. Toque em **Desfazer** no aviso (rápido, antes de ele sumir).
   **Deve acontecer:** o aviso "Ditado de volta ao histórico."; a nota nova some; e o ditado aparece no
   **Histórico** do celular (e, em um minuto, no do PC).
7. Faça o passo 2 de novo, com **"Mercado N8: sal"**, e toque em **Desfazer** no aviso.
   **Deve acontecer:** o "Sal" sai de **Mercado N8**, e o ditado vai para o Histórico.

## 4. O microfone dentro da nota

1. No celular, abra **Teste N8** e toque no fim do texto ("segunda versão"), para o cursor ficar ali.
2. Toque no **microfone** da barra de baixo da nota (ao lado dos três pontinhos) e fale: **"com um ditado
   no fim"**. Toque de novo no mesmo botão (agora um quadrado) para parar.
   **Deve acontecer:** em segundos, o texto entra onde o cursor estava: "segunda versão com um ditado no
   fim".
3. Abra **Mercado N8**, toque em **"+ Item"** (embaixo dos itens abertos), toque no microfone e fale:
   **"farinha, ovos e fermento"**. Pare.
   **Deve acontecer:** viram três itens: Farinha, Ovos, Fermento.
4. Feche a nota. Olhe o **Histórico** do celular e o do PC.
   **Deve acontecer:** nenhum desses dois ditados aparece no histórico.

## 5. No mercado, sem sinal

1. No celular, ligue o **modo avião** (sem Wi-Fi também). **Feche o app de vez** (tire da lista de apps
   abertos) e abra de novo pelo ícone.
   **Deve acontecer:** o app abre direto na aba **Notas**, com a lista de antes; no alto, a faixa **"Sem
   rede"**. O "Criar uma nota…" e o "Ditar para nota" ficam apagados.
2. Abra **Mercado N8**.
   **Deve acontecer:** a frase "Sem rede: dá para marcar, desmarcar e acrescentar item. O resto volta com a
   rede."; o título e os itens não se editam; os botões de formatar, cor, marcadores e o microfone ficam
   apagados.
3. Marque **Arroz** e **Feijão** (toque nas caixinhas). Toque em **"+ Item"**, escreva **Café** e toque em
   **Fechar**.
   **Deve acontecer:** a faixa diz **"Sem rede · 1 mudança esperando"**, e o cartão de **Mercado N8** diz
   **"↑ esperando a rede"**.
4. Tente mexer em outra coisa: abra **Teste N8** e tente mudar o texto; ou, no cartão de **Teste N8**, tente
   fixar ou mudar a cor.
   **Deve acontecer:** não deixa, e o aviso diz o que dá para fazer sem rede.
5. **Feche o app de vez** de novo e, se quiser, **desligue e ligue o celular**. Abra o app (ainda sem rede).
   **Deve acontecer:** Arroz e Feijão continuam marcados, o Café continua lá, e a faixa ainda diz "1
   mudança esperando".
6. Desligue o modo avião. Volte ao app (ou feche e abra).
   **Deve acontecer:** em alguns segundos (até meio minuto), a faixa some e o "esperando a rede" sai do
   cartão. No PC, em um minuto, **Mercado N8** tem Arroz e Feijão marcados e o Café.

## 6. A volta da rede quando o PC mexeu no meio (os dois só marcaram)

1. No PC, em **Mercado N8**, desmarque tudo (Mais → **Desmarcar tudo**) e feche. Espere um minuto.
2. No celular, volte à aba Notas (a lista lê de novo) e confira que tudo está desmarcado. Ligue o modo
   avião.
3. No celular, sem rede, marque **Arroz**. Feche a nota.
4. No PC (com rede), marque **Leite** em **Mercado N8** e feche. Espere um minuto.
5. No celular, desligue o modo avião e espere a faixa sumir.
   **Deve acontecer:** em **Mercado N8**, **Arroz e Leite marcados**, nos dois aparelhos, **sem** o selo
   "Versão em conflito" e sem pergunta nenhuma.

## 7. Os dois mudaram o texto (o selo)

1. No celular, ligue o modo avião. Abra **Mercado N8**, toque em **"+ Item"**, escreva **Manteiga** e feche.
2. No PC, abra **Mercado N8** e troque o texto do item **Pão** para **Pão de forma**. Feche e espere um
   minuto.
3. No celular, desligue o modo avião e espere a faixa sumir.
   **Deve acontecer:** o aviso diz que a nota mudou em outro aparelho também e que as duas versões estão
   nela. O cartão de **Mercado N8** mostra **"Versão em conflito"**, no celular e, em um minuto, no PC.
   Nada se perdeu: uma versão tem "Pão de forma", a outra tem "Manteiga".
4. No celular, abra a nota: em cima, o texto que ficou; embaixo, "A outra", com o que você fez sem rede.
   Toque em **Usar a outra** (ou **Manter esta**, como preferir).
   **Deve acontecer:** o selo some, nos dois aparelhos. Se quiser os dois, escreva o que falta depois.

## 8. A nuvem que recusa (a nota apagada de vez no meio)

1. No PC, crie uma lista de marcar **Recusa N8** com dois itens, A e B. Espere um minuto e confira que ela
   apareceu no celular.
2. No celular, ligue o modo avião, marque o item **A** de **Recusa N8** e feche.
3. No PC, ponha **Recusa N8** na lixeira e, na **Lixeira** das notas, **Excluir para sempre**. Espere um
   minuto.
4. No celular, desligue o modo avião e espere.
   **Deve acontecer:** o aviso "Não deu para juntar a mudança feita sem rede em “Recusa N8”: ela virou a
   nota “Recusa N8 (feita sem rede)”." Essa nota nova aparece no celular e, em um minuto, no PC, com o A
   marcado. Nada sumiu calado.

## 9. A ordem e o arrastar com o dedo

1. No celular, na aba Notas, **passe o dedo** pela lista para cima e para baixo, começando em cima de um
   cartão.
   **Deve acontecer:** a tela rola normalmente; nenhum cartão sai do lugar.
2. **Segure** um cartão das "Outras" (não fixadas) por **meio segundo**, sem mexer o dedo.
   **Deve acontecer:** o cartão "levanta" (fica maior e com sombra; o celular pode vibrar de leve). Arraste
   para outra posição e solte.
   **Deve acontecer:** o cartão fica no lugar novo. Em um minuto, a mesma ordem no PC.
3. No PC, arraste uma nota para outra posição. Volte à aba Notas no celular.
   **Deve acontecer:** a ordem do PC aparece no celular.
4. Sem rede (modo avião), tente arrastar.
   **Deve acontecer:** a nota volta ao lugar e o aviso diz que isso espera a rede.

## 10. O que fica para o tempo

A lixeira das notas agora esvazia sozinha também na nuvem, 30 dias depois de a nota ir para lá. Isso não dá
para ver hoje (é preciso esperar um mês); os testes automáticos cobrem. Se daqui a um mês uma nota que você
pôs na lixeira pelo celular tiver sumido da Lixeira, é isso.

## 11. O que me contar

1. Cada parte (1 a 9): deu certo, ou o número do passo em que parou e o que apareceu.
2. Na parte 3, passo 2: os itens vieram separados (Queijo, Presunto, Manteiga) ou num item só?
3. Na parte 5, quanto tempo levou para a faixa sumir depois de desligar o modo avião.
4. Na parte 9: o arrastar com o dedo foi fácil? Alguma vez a tela rolou quando você queria arrastar (ou o
   contrário)?
5. Qualquer coisa estranha na tela do celular (algo cortado, um botão em cima de outro, o aviso tapando o
   botão "Ditar para nota").
