# Teste manual do "Ditar para nota" (ADR-022, N9)

Este roteiro é seu (do Mauro). Os testes automáticos cobrem o decisor das teclas, a escolha da nota, o
acréscimo, a pílula e a tela de Configurações com dublês: eles nunca instalam o gancho de teclado, nunca
apertam tecla e nunca mexem na área de transferência. Só aqui as teclas de verdade são usadas.

Para quê: confirmar, no PC, que o **Ctrl+Win de sempre não mudou**, e que o **Ctrl+Win+Alt** manda o
ditado para uma nota em vez do campo em foco, dizendo na pílula para onde foi.

## 0. Antes
1. Feche o painel no navegador, se estiver aberto.
2. Num PowerShell, rode (ele fecha o Agent como o "Sair" da bandeja, recompila o Agent e o painel, abre o
   Agent novo e confere que é ele que está rodando):

   ```powershell
   powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\remontar-agent.ps1
   ```

   Tem de terminar com as linhas `OK:`. A migration `0011` roda sozinha na subida e só acrescenta uma
   coluna nas configurações (o "Ditar para nota", que nasce ligado); o resto do banco não muda.
3. Abra o painel pela bandeja. Em **Configurações**, logo abaixo da **Tecla de ditado**, deve aparecer
   **Ditar para nota**, marcado, com as teclas `Ctrl + Win + Alt`.
4. Na aba **Notas**, crie (ou confira que existem) estas notas, com estes títulos:
   - **Lista de compras**, corpo de lista de marcar: um item aberto ("Arroz") e um marcado ("Feijão");
   - **Lista**, com qualquer texto em parágrafo;
   - **Ideias**, com uma linha de parágrafo ("Primeira ideia.").

## 1. O Ctrl+Win de sempre (primeiro, antes de tudo)
1. Abra o Bloco de Notas, clique dentro dele, segure **Ctrl+Win**, diga "teste do ditado comum", solte. O
   texto entra no Bloco de Notas, com o bipe e a pílula de sempre ("Inserido").
2. Digite à mão `freio refrigerador` no Bloco de Notas: sai igual (a ordem das letras não mudou).
3. Segure **Ctrl+Win**, solte rápido (menos de meio segundo): nada acontece, nem pílula.
4. O menu Iniciar **não** abriu em nenhum desses passos.

Se algo daqui mudou, pare e me avise: é o que este trabalho não podia mexer.

## 2. Para uma nota que existe, com lista de marcar
1. Ainda com o cursor no Bloco de Notas, segure **Ctrl+Win+Alt** (aperte as três; a ordem não importa),
   diga "lista de compras: leite, pão e café", solte.
2. A pílula diz **"Lista de compras +3"**, com o ponto verde. **Nada** entrou no Bloco de Notas.
3. Na aba **Notas** do painel (volte ao navegador: a lista relê sozinha), abra **Lista de compras**: os
   itens "Leite", "Pão" e "Café" estão abertos, depois de "Arroz", e "Feijão" continua no grupo dos marcados,
   embaixo.
4. No **Histórico**, a frase **não** aparece (ela foi para a nota).
5. Repita apertando **Ctrl+Win** primeiro e o **Alt** só depois de o bipe tocar, já falando: também vai para
   a nota. E soltando o **Alt** antes das outras: também.

## 3. O título mais longo e o empate
1. **Ctrl+Win+Alt**: "lista de compras, sal". Vai para **Lista de compras** ("+1"), não para **Lista**.
2. **Ctrl+Win+Alt**: "lista: lembrar do aniversário". Vai para **Lista** ("Lista +1"), no fim, como
   parágrafo.
3. **Ctrl+Win+Alt**: "Ideias, comprar um barco". Vai para **Ideias** ("Ideias +1"), numa linha nova no fim.
4. (Opcional) Arquive a nota **Ideias** e dite de novo "ideias: outra coisa": nasce uma **nota nova**
   (só as notas ativas recebem). Desarquive depois.

## 4. Sem título que case
1. **Ctrl+Win+Alt**: "ligar para o contador amanhã cedo". A pílula diz **"Nota nova"**.
2. Na aba **Notas** há uma nota nova, sem título, com a frase inteira, e o cartão diz "veio de um ditado".
3. **Ctrl+Win+Alt** dizendo só "lista de compras": nasce uma nota nova com essas palavras (não há o que
   acrescentar, e nada se perde).

## 5. Desligado
1. Em **Configurações**, desmarque **Ditar para nota** e salve.
2. No Bloco de Notas, **Ctrl+Win+Alt**: "teste desligado". O texto entra **no Bloco de Notas**, como um
   ditado comum ("Inserido"), e vai para o Histórico.
3. Marque de novo e salve.

## 6. A falha (se der para provocar)
Não há um jeito limpo de fazer o banco falhar de propósito. O caso que dá para tentar é a nota grande
demais: só se você tiver uma nota perto de 64 KB. Se a pílula disser **"Não salvou na nota: está no
histórico."**, confira que a frase está no **Histórico**, inteira, com a etiqueta de falha. Se não der para
provocar, pule: os testes automáticos cobrem esse caminho.

## 7. O que anotar
- Se o **menu Iniciar**, a barra de menus de algum programa (o Alt costuma ativá-la) ou algum atalho de outro
  programa reagiu ao **Ctrl+Win+Alt**: qual programa e o que apareceu. A colisão não foi medida.
- Se a pílula de gravação deveria dizer que é para nota já durante a fala (hoje só o desfecho diz).
- No `agent.log` (pasta de dados do programa), a linha `winruntime: ditado gravado na nota` leva só números
  (`nota_nova`, `linhas`, `text_runes`): nem o texto nem o título.
