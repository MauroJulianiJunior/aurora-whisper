# Teste manual do sync das notas (ADR-022, N7)

Este roteiro é seu (do Mauro), e vem **depois** do `docs/deploy-notas-n7.md`: a nuvem já tem de estar com as
migrations `0020` a `0025` e a API nova no ar. Os testes automáticos cobrem o banco do PC, o motor, as regras
da nuvem (inclusive a mescla da lista de marcar) e a ponta a ponta com dois PCs e o celular, tudo sobre uma
nuvem em memória; eles nunca usam o Agent de verdade, a rede de verdade nem o Postgres da VPS. Só aqui as
notas sobem e descem pela nuvem real, entre dois aparelhos de verdade.

Para quê: confirmar que a nota e o marcador criados num PC aparecem no outro, que o conflito de texto vira o
selo "Versão em conflito" sem perder nada, que a lista de marcar mescla sozinha quando cada lado marca itens
diferentes, que apagar de vez num PC apaga no outro, e que o ditado que virou nota some do histórico do
celular.

**O que você precisa:** a nuvem do deploy no ar, o PC 1 (o seu, já conectado à nuvem) e **um segundo
aparelho conectado à mesma conta**: outro PC, de preferência. Sem ele, só dá para fazer as partes 1, 2 (pela
URL da nuvem), 5 (pela URL da nuvem) e 6; as partes 3, 4 e 7 pedem dois Agents.

**Um segundo Agent no mesmo PC.** Dois Agents não rodam juntos no mesmo usuário do Windows (o mutex
`Local\AuroraWhisperAgent` é um só, ADR-018). A alternativa é um segundo usuário do Windows com o Agent
instalado e conectado à mesma conta (troca rápida de usuário). Não testei esse arranjo; se o Agent do
segundo usuário não abrir, ou brigar com o primeiro por alguma porta, **pare e me avise**, em vez de forçar.
Não vale usar o celular com as ferramentas do navegador para ler as notas: para olhar o que a nuvem tem, use
a URL da nuvem na aba em que você já está logado (parte 2).

**Quanto espera:** o PC sincroniza a cada 60 s, e a cada 10 s enquanto o painel está aberto. Depois de mexer
numa nota, dê até um minuto antes de dizer que não chegou.

## 0. Antes
1. Feche o painel no navegador, se estiver aberto, nos dois aparelhos.
2. No PC 1, num PowerShell, rode (ele fecha o Agent como o "Sair" da bandeja, recompila o Agent e o painel,
   abre o Agent novo e confere que é ele que está rodando):

   ```powershell
   powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\remontar-agent.ps1
   ```

   Tem de terminar com as linhas `OK:`. A migration `0013` roda sozinha na subida e só acrescenta cinco
   colunas nulas em `local_notes`; o resto do banco não muda, e nenhuma nota some.
3. No segundo aparelho, ponha o Agent **desta mesma versão** (a da branch `notas/n7`: num outro PC, um clone
   dela com o mesmo `remontar-agent.ps1`, ou o instalador montado a partir dela) e conecte-o à mesma
   conta, como no `docs/teste-final-f11.md`. Um Agent de antes da N7 no segundo aparelho **não** serve:
   ele ignora as notas.
4. Confirme que a nuvem é a nova. Num PowerShell, troque `aurora.exemplo.com.br` pelo seu domínio:

   ```powershell
   powershell -ExecutionPolicy Bypass -File C:\Desenvolvimento\aurora-whisper\scripts\conferir-nuvem.ps1 -Dominio aurora.exemplo.com.br -IPEsperado <IP-DA-VPS>
   ```

   **Deve acontecer:** "OK: a nuvem em https://aurora.exemplo.com.br responde certo de fora".
5. No PC 1, abra o painel pela bandeja e anote, na aba **Notas**: quantas notas há (as do Arquivo contam; as
   da Lixeira não) e quantos marcadores (**Editar marcadores**). Crie, se faltar, o necessário para o resto:
   - uma nota com título **Teste N7** e o corpo "primeira versão";
   - uma lista de marcar com título **Mercado N7** e quatro itens: Arroz, Feijão, Leite, Pão (abertos);
   - um marcador chamado **Compras N7**, posto em **Mercado N7**.

## 1. A primeira rodada: o que já existe sobe
1. No PC 1, abra **Configurações** e olhe a seção da nuvem. Espere até dois minutos (um PC com muitas notas
   pode levar algumas rodadas; o painel aberto acelera). Não há botão novo: o "Enviar o histórico" que você
   já clicou na F7 vale como o aceite das notas.
2. Na VPS (`ssh vps`), só contagens, nunca o texto:

   ```bash
   docker exec aurora-whisper-postgres psql -X -A -t -U postgres -d aurora -c "SELECT (SELECT count(*) FROM notes) AS notas, (SELECT count(*) FROM labels) AS marcadores, (SELECT count(*) FROM transcription_note_links) AS ditados_que_viraram_nota"
   ```

   **Deve acontecer:** `notas` igual ao número de notas do PC 1 fora da lixeira (o que você anotou na parte
   0, mais as duas do passo 5, com as arquivadas contando), `marcadores` igual ao número de marcadores, e
   `ditados_que_viraram_nota` igual ao número de ditados que já viraram nota no PC **e que a nuvem já tinha**.
   A nota na lixeira do PC não sobe (a completação pula a lixeira), então ela não entra na conta.
3. No `agent.log` do PC 1 (na pasta de dados do programa; o Agent do `bin\` guarda os dados ao lado do
   executável, o instalado em `%LOCALAPPDATA%\AuroraWhisper`). Para achar o arquivo e ler só as linhas do
   sync, num PowerShell:

   ```powershell
   $log = Get-ChildItem -Path "C:\Desenvolvimento\aurora-whisper\bin", "$env:LOCALAPPDATA\AuroraWhisper" -Recurse -Filter agent.log -ErrorAction SilentlyContinue | Sort-Object LastWriteTime | Select-Object -Last 1 -ExpandProperty FullName; $log; Select-String -Path $log -Pattern 'sync: rodada' | Select-Object -Last 8 | ForEach-Object { $_.Line }
   ```

   Na primeira rodada depois da atualização deve haver `recebidos` alto e muitos `ecos` (o cursor voltou a 0
   e leu tudo de novo, uma vez), e depois `enviados` com as notas e os marcadores. Essas linhas só têm
   contagens: nenhuma leva título, corpo ou nome de marcador.

## 2. Uma nota criada num PC aparece no outro
1. No PC 1, crie uma nota nova: título **Nota do PC 1**, corpo "escrita no primeiro PC". Espere até um minuto.
2. **Deve acontecer, no segundo aparelho:** na aba **Notas**, a nota **Nota do PC 1** aparece, com o corpo
   certo, e também **Teste N7**, **Mercado N7** (com o marcador **Compras N7**) e as outras notas que o PC 1 já
   tinha. O marcador **Compras N7** está na **Editar marcadores** do segundo aparelho.
3. Edite a nota no segundo aparelho (acrescente "e editada no segundo") e espere. **Deve acontecer:** no PC 1
   a nota ganha a edição, sem selo.
4. **Sem segundo aparelho:** abra, na mesma aba do navegador em que você está logado na nuvem (o app do
   celular), este endereço (troque o domínio):

   ```
   https://aurora.exemplo.com.br/api/v1/notes?vista=ativas
   ```

   **Deve acontecer:** um JSON com `items`, onde está a **Nota do PC 1** (campos `titulo`, `corpo`, `marcadores`,
   `posicao`, `version`, e `versao_em_conflito: null`) e `has_more: false`. Se aparecer um erro de
   autenticação, entre no app da nuvem na mesma aba e tente de novo. A lista de marcadores está em
   `https://aurora.exemplo.com.br/api/v1/labels`. O JSON mostra o texto das suas notas: não cole a página no
   chat, só o que o roteiro pede.

## 3. O conflito: a mesma nota editada nos dois lados
Precisa de dois Agents. A ideia: a edição do PC 2 é feita **sem rede** e sobe depois da do PC 1.
1. Nos dois aparelhos, a nota **Teste N7** está igual ("primeira versão").
2. No PC 2, **corte a rede** (modo avião, ou tire o cabo/Wi-Fi). O Agent continua funcionando local.
3. No PC 2, edite **Teste N7**: troque o corpo para "versão do PC 2".
4. No PC 1 (com rede), edite a mesma nota: troque o corpo para "versão do PC 1". Espere um minuto: ela sobe.
5. No PC 2, **volte a rede**. Espere até um minuto e meio.
6. **Deve acontecer, nos dois aparelhos:** o corpo de **Teste N7** é o da versão que chegou à nuvem primeiro
   ("versão do PC 1") e o cartão mostra o selo **Versão em conflito**. Abra a nota: a janela mostra a outra
   versão ("versão do PC 2") com os botões **Manter esta** e **Usar a outra**. Nada foi perdido.
   (Na nuvem, `https://aurora.exemplo.com.br/api/v1/notes?vista=ativas` mostra `versao_em_conflito` preenchido
   nessa nota.)
7. No PC 1, clique **Usar a outra**. **Deve acontecer:** a nota vira "versão do PC 2" e o selo some do PC 1.
   Espere um minuto: no PC 2 a nota também fica "versão do PC 2", sem selo. O JSON da nuvem volta a ter
   `versao_em_conflito: null`.
8. Repita o conflito (passos 2 a 5, agora com "terceira do PC 1" e "terceira do PC 2") e dessa vez clique
   **Manter esta** no PC 2. **Deve acontecer:** o texto que a nuvem tem fica, o selo some nos dois.
9. A cor, o fixar, o arquivar e a ordem **não** dão selo: no PC 2 sem rede, troque a cor de **Teste N7** para
   verde; no PC 1, troque para azul. Reconecte. **Deve acontecer:** os dois ficam com uma cor só (a do último
   que escreveu) e **nenhum selo**.

## 4. A lista de marcar mescla sozinha
Precisa de dois Agents. **Mercado N7** tem Arroz, Feijão, Leite e Pão, todos abertos, igual nos dois.
1. No PC 2, **sem rede**, marque **Arroz**.
2. No PC 1, com rede, marque **Pão**. Espere um minuto: sobe.
3. No PC 2, **volte a rede**. Espere até um minuto e meio.
4. **Deve acontecer, nos dois aparelhos:** **Arroz e Pão** marcados (embaixo, na divisória "2 marcados", em
   qualquer ordem), **Feijão e Leite** abertos, e **nenhum** selo "Versão em conflito".
5. Agora o caso que não mescla. Com **Arroz** e **Pão** marcados nos dois, corte a rede do PC 2 e, no PC 2,
   troque o texto do item **Feijão** para "Feijão preto". No PC 1, troque o mesmo item para "Feijão carioca".
   Reconecte o PC 2. **Deve acontecer:** o selo **Versão em conflito** aparece, e a outra versão pode ser
   escolhida como na parte 3.
6. (Opcional) Troque o texto de **um** item no PC 1 e **marque outro** no PC 2 sem rede. **Deve acontecer:**
   também o selo, porque a mescla só vale quando o texto de todos os itens é igual nos três lados (o antes, o
   PC 1 e o PC 2); qualquer texto mudado vira conflito normal, nunca perda.

## 5. Apagar de vez num PC apaga no outro
1. Crie no PC 1 uma nota **Apagar N7** e espere até ela aparecer no PC 2.
2. No PC 1, mande **Apagar N7** para a **Lixeira** da aba Notas. Espere. **Deve acontecer:** no PC 2, ela sai
   da lista principal e aparece na **Lixeira** das notas. Na nuvem,
   `https://aurora.exemplo.com.br/api/v1/notes?vista=lixeira` a tem.
3. No PC 1, na Lixeira, escolha **Excluir para sempre** em **Apagar N7** e confirme na própria linha.
   Espere um minuto e meio.
4. **Deve acontecer:** a nota some da Lixeira do PC 2 (apagada, não só escondida), e o endereço
   `https://aurora.exemplo.com.br/api/v1/notes?vista=lixeira` não a tem mais. Nenhum PC a traz de volta nas
   rodadas seguintes, nem depois de fechar e abrir o Agent do PC 2.
5. (Opcional, com dois Agents) Corte a rede do PC 2 **antes** de apagar de vez no PC 1, edite a mesma nota
   no PC 2 sem rede, apague de vez no PC 1, e reconecte o PC 2. **Deve acontecer:** a nota **não** volta; o
   PC 2 a apaga (a lápide da nuvem não vence, mesmo para um PC que ficou parado).
6. Apague também um marcador (**Editar marcadores**, uma etiqueta de teste que você criou): sai das notas nos
   dois PCs, e as notas ficam.

## 6. "Virar nota" esconde o ditado no celular
1. Dite uma frase no PC 1 (ou use uma do histórico que já subiu), por exemplo "teste do virar nota sete".
   Espere um minuto e confira, no app do celular (ou na aba da nuvem no navegador), que ela está no
   histórico.
2. No **Histórico** do PC 1, abra a frase e clique **Virar nota**. A frase sai do histórico do PC e aparece
   o aviso **"Virou nota"**.
3. Espere até um minuto e meio e olhe o histórico no app do celular (recarregue a tela). **Deve
   acontecer:** a frase **não aparece mais** no histórico do celular, nem na busca. A nota nova aparece na
   aba Notas do segundo aparelho.
4. Num ditado novo, clique **Virar nota** e **Desfazer** (no aviso). Espere. **Deve acontecer:** a frase
   **volta** ao histórico do celular, e a nota some do segundo aparelho (o Desfazer apaga a nota que ninguém
   editou).
5. (Opcional) Ditado feito dentro da nota: abra uma nota no PC 1, com o cursor no corpo, segure **Ctrl+Win** e
   dite. A frase entra na nota e **não** aparece no histórico do PC; espere, e ela também **não** aparece no
   histórico do celular.
6. A retenção de 6 meses da nuvem continua valendo para o ditado que virou nota; isso não dá para ver agora.

## 7. A ordem das notas viaja com a nota
Precisa de dois Agents.
1. No PC 1, na aba **Notas** (sem busca e sem filtro de marcador), arraste **Mercado N7** para o topo do
   grupo dela, ou use **Alt** e as setas.
2. Espere um minuto. **Deve acontecer:** no PC 2, **Mercado N7** está na mesma posição relativa às outras
   notas, e **nenhum** selo aparece (a ordem não é conteúdo).
3. Nos dois PCs, **Mercado N7** não ganhou "editada agora": arrastar não muda a hora de edição.
4. Mover no PC 2 e no PC 1 a mesma nota sem rede: ao reconectar, os dois ficam na posição do último que
   escreveu, sem selo.

## 8. O que me contar
- Se a primeira rodada mandou tudo: as três contagens da VPS da parte 1 e quantas notas, marcadores e
  ditados-nota o PC 1 tinha.
- Em cada parte (2 a 7): se o "Deve acontecer" se cumpriu, e, se não, em que passo parou e o que apareceu.
- Se algum selo apareceu onde **não** devia (parte 3, passo 9; parte 4, passos 4 e 6; parte 7), ou deixou de
  aparecer onde devia.
- Se alguma nota **voltou** depois de apagada de vez, ou **sumiu** sem você apagar. É o erro mais grave deste
  trabalho: pare e me avise antes de seguir.
- Quanto levou, aproximadamente, para uma nota criada num PC aparecer no outro.
- As linhas `sync: rodada` do `agent.log` do PC 1 (só números) da primeira rodada, se algo não bateu. Os campos
  `recusados` e `nao_aplicados` devem ficar em zero ou quase.
- Se o Agent do segundo usuário do Windows (ou do segundo PC) abriu sem problema.
- Se a janela da nota mostrou o selo e os dois botões como descrito, e se o texto dos botões ficou claro.
