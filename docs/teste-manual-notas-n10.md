# Teste manual das imagens nas notas, no PC (ADR-022, N10, a primeira metade)

Este roteiro é seu (do Mauro). Os testes automáticos cobrem a conferência da imagem (com uma foto de mentira
com EXIF e localização), o banco, a pasta, a API local e a aba Notas com dublês do canvas: eles nunca abrem
uma foto de verdade, nunca colam nada na sua área de transferência e nunca arrastam arquivo. Só aqui o
navegador reduz uma foto de verdade, e só aqui se vê a grade, a imagem grande e o arrastar.

Para quê: confirmar, no PC, que dá para **anexar imagem à nota pelos três caminhos** (o ícone, o Ctrl+V e o
arrastar), que ela aparece como na prévia aprovada (`docs/notas-previa-n10.html`), que **tirar tem Desfazer**,
que a imagem **segue a nota** na lixeira, e que os **limites** avisam com as frases combinadas.

Nesta metade as imagens são **só do PC**: não sobem à nuvem nem aparecem no celular (é a segunda metade). Se o
seu PC está conectado à nuvem, uma nota que só tem imagem aparece no celular como "Nota vazia" até lá.

## 0. Antes
1. Feche o painel no navegador, se estiver aberto.
2. Num PowerShell, rode (ele fecha o Agent como o "Sair" da bandeja, recompila o Agent e o painel, abre o
   Agent novo e confere que é ele que está rodando):

   ```powershell
   powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\remontar-agent.ps1
   ```

   Tem de terminar com as linhas `OK:`. A migration `0014` roda sozinha na subida e só cria a tabela das
   imagens, vazia; o resto do banco não muda.
3. Separe, numa pasta qualquer (a Área de Trabalho serve):
   - **uma foto tirada pelo celular** (de preferência com a localização ligada), de até 10 MB;
   - **uma foto em pé** (retrato), para conferir a orientação;
   - **um PNG com fundo transparente** (um logotipo, por exemplo);
   - **um arquivo grande**, acima de 10 MB (um vídeo curto, renomeado para `.jpg`, serve);
   - **um PDF** qualquer.
4. Abra o painel pela bandeja e vá à aba **Notas**. Crie uma nota **"Recibos de outubro"** com uma linha de
   texto ("Táxi, almoço e estacionamento").

## 1. Anexar pelo ícone da barra
1. Abra **Recibos de outubro**. Na barra de baixo, o ícone de **imagem** (entre Marcadores e Arquivar) está
   aceso. Clique nele: abre a escolha de arquivos do Windows.
2. Escolha a **foto do celular**. Aparece no topo da nota, de ponta a ponta, um instante com
   **"guardando…"**, e depois normal. O rodapé diz "editada às … · 1 imagem".
3. Feche a nota (Esc). No cartão da grade, a foto fica no topo, de ponta a ponta, com o título embaixo.
4. Abra de novo e anexe a **foto em pé**: as duas ficam lado a lado. A foto em pé está **em pé** (a orientação
   do celular foi respeitada), não deitada.

## 2. Colar com Ctrl+V
1. Com a nota aberta, tire um print de um pedaço da tela com **Win+Shift+S** (a ferramenta de recorte do
   Windows).
2. Clique no título da nota e aperte **Ctrl+V**. O print entra como a terceira imagem ("guardando…" e depois
   normal). O título **não** ganhou texto.
3. Copie um texto qualquer (Ctrl+C numa palavra do Bloco de Notas) e cole numa linha da nota: entra o
   **texto**, como sempre, e nenhuma imagem nova.
4. (Opcional) Copie uma célula do Excel e cole numa linha: entra o **texto** da célula, não a imagem dela.

## 3. Arrastar um arquivo
1. Com a nota aberta, arraste o **PNG transparente** da pasta para cima da nota: aparece o contorno tracejado
   **"Solte para anexar à nota"**. Leve o arquivo para fora da nota sem soltar: o contorno some.
2. Arraste de novo e solte dentro da nota: o PNG entra, com **fundo branco** (e não preto) onde era
   transparente.
3. Arraste a foto do celular e solte no **escuro em volta da nota** (fora dela): nada acontece, e o painel
   **não** é trocado pela imagem. Feche a nota e solte a foto **em cima da grade**, sem nota aberta: também
   nada acontece, e o painel continua lá (sem perder a sessão).
4. Agora há 4 imagens: na nota, a primeira larga e duas embaixo, e o último quadrado diz **"+1"** (a quarta não
   cabe na grade). No cartão, o mesmo.

## 4. A imagem grande
1. Na nota aberta, clique na primeira imagem. Ela abre grande, **sobre a tela escura** (escura também se o
   Windows estiver no tema claro), com **"1 de 4 · Recibos de outubro"** no alto.
2. As setas **‹ ›** e as teclas **← →** passam para as outras; depois da última, volta à primeira.
3. Clique em **Baixar**: o navegador salva `nota-imagem-1.jpg` (a cópia reduzida). Abra o arquivo baixado:
   - o lado maior tem no máximo **2.000 px** (botão direito → Propriedades → Detalhes);
   - nos Detalhes, **não há localização (GPS)**, nem câmera, nem data de captura: os metadados da foto saíram.
4. Aperte **Esc**: fecha só a imagem grande, e a nota continua aberta.
5. Clique no **"+1"**: abre a terceira imagem ("3 de 4"), e a seta leva à quarta.

## 5. Tirar e desfazer
1. Na nota aberta, passe o mouse em cima de uma imagem: aparece o **×** no canto. Clique nele: a imagem some, e
   a pílula diz **"Imagem tirada da nota."** com **Desfazer**. Clique em **Desfazer**: ela volta, no mesmo lugar.
2. Tire de novo e espere a pílula sumir (uns 8 segundos, com o mouse longe dela). Feche e abra a nota: a imagem
   **não** volta.
3. Abra uma imagem grande e clique em **Tirar da nota**: mostra a seguinte; a pílula oferece o Desfazer. Tire
   até não sobrar nenhuma: a imagem grande fecha sozinha.
4. Anexe de novo duas imagens para os passos seguintes.

## 6. A nota só com imagem
1. No **"Criar uma nota…"** do alto, clique no ícone de **imagem** (o da direita) e escolha uma foto.
2. Nasce uma nota nova, só com a imagem, no topo, e ela abre. Feche sem escrever nada.
3. O cartão mostra **só a imagem**, sem "Nota vazia". Na forma **Lista**, ela aparece como "Nota com imagem",
   com o ícone e a conta.

## 7. Lixeira, restaurar e apagar de vez
1. Mova **Recibos de outubro** para a lixeira. Na **Lixeira** das notas, a linha diz "· 2 imagens".
2. **Restaure**: a nota volta com as duas imagens.
3. Mova para a lixeira de novo e **Excluir definitivamente**. Na pasta de dados
   (`%LOCALAPPDATA%\AuroraWhisper\notas-imagens`, ou a `notas-imagens` ao lado do executável no `bin\`), os
   arquivos daquela nota sumiram. (Abrir a pasta pelo Explorador pode deixar miniaturas no cache do Windows:
   se não quiser isso, confira pela linha de comando:)

   ```powershell
   Get-ChildItem "$env:LOCALAPPDATA\AuroraWhisper\notas-imagens" | Select-Object Name, Length
   ```

4. **O expurgo de 30 dias** não dá para esperar: os testes automáticos o cobrem (a nota vencida sai com a linha
   e o arquivo). Se quiser conferir de verdade, mova uma nota com imagem para a lixeira e olhe a pasta daqui a
   30 dias.

## 8. Os limites
1. Anexe o **arquivo acima de 10 MB**: a pílula diz **"A imagem passou de 10 MB."**, e nada é anexado.
2. Anexe o **PDF** (pelo ícone, mude o filtro para "Todos os arquivos", ou arraste): **"Esse arquivo não é uma
   imagem que o navegador abra."**
3. Numa nota, anexe até ter **10 imagens** (pode escolher várias de uma vez). A décima primeira diz **"A nota já
   tem 10 imagens."**
4. O teto do total (**400 MB**) não dá para encher num teste; os testes automáticos cobrem a frase
   ("As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras.").
5. (Opcional) Com uma imagem "guardando…", feche o Agent pela bandeja: a pílula diz **"A rede caiu antes de a
   imagem subir. Tente de novo."** e a faixa de cima avisa que o Agent saiu. Abra o Agent de novo.

## 9. O que não olha a imagem
1. Na busca do topo, procure uma palavra que só existe **dentro** de uma imagem (um texto do print): a nota
   **não** aparece (não há leitura do texto da imagem).
2. No **Mais** da nota com imagem, **Copiar para WhatsApp** e cole no WhatsApp Web: só o texto vai.
3. Com o Polish ligado, o **Formatar → Lista** numa linha da nota com imagem funciona como antes (só a linha vai
   à IA, nunca a imagem).

## 10. O que anotar
- Se a grade, o "+N" e a imagem grande ficaram como na prévia (cenas 1 a 4), nos dois temas.
- Quanto tempo o "guardando…" ficou na tela com uma foto grande do celular.
- No `agent.log` (pasta de dados), nada das imagens aparece além das linhas `guard: POST
  /api/notas/<id>/imagens -> 201` (só ids). Se a subida achar algo fora do lugar na pasta, a linha é um WARN
  `notas: a varredura das imagens tirou o que estava sem par`, só com números.
