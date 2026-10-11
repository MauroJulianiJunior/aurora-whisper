# Teste manual da nuvem, F4: o app no navegador, sem Docker e sem Google

**O que é:** o mesmo painel do PC, aberto pelo navegador e falando com a nuvem: é o app que vai para
o celular na F5. **Por que agora:** dá para ver as telas novas (Entrar, o selo de conflito, a lixeira,
as configurações da conta, a falta de rede) antes de existir o servidor de verdade e o client do
Google; tudo roda só no seu PC, na memória, com ditados de exemplo inventados.

ADR-019, fatia F4. Tempo estimado: 12 minutos (mais 5, se quiser ver no Android pelo cabo).

Nada aqui toca os seus ditados reais: o servidor de teste não lê o banco do Agent nem o Postgres. Os
ditados de exemplo são inventados, vêm de dois PCs que não existem, e somem quando você desliga.

## 1. Subir (um comando)

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\nuvem-local-teste.ps1
```

O que ele faz: compila o painel no modo nuvem numa pasta sua, fora do repositório
(`%USERPROFILE%\.aurora-whisper-dev\painel-nuvem`), e sobe o servidor de teste. O que você deve ver:
"Compilando o painel no modo nuvem...", depois **"Nuvem de teste em http://localhost:8080"** e
"Entre com a conta de teste (voce@teste.local)". A janela fica presa no servidor; deixe-a aberta.

Se a porta 8080 estiver em uso, o script avisa antes de compilar. Rode com outra porta e use ela nos
endereços abaixo:

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\nuvem-local-teste.ps1 -Porta 8081
```

## 2. Entrar

1. No Chrome, abra **`http://localhost:8080`** (`localhost`, e não `127.0.0.1`: o servidor recusa o
   outro nome, com `host inválido`).
2. A tela **Entrar**: a Aurora, "Aurora Whisper", "Entre para ver o que você ditou." e um botão só,
   **Entrar com a conta de teste**, com "Só neste PC, sem Google." embaixo. O botão do Google não
   aparece porque o servidor de teste não tem o Google; num servidor de verdade, aparece só o do
   Google.
3. Clique. Você cai no **Histórico**.

Com o F12 aberto, um `401` vermelho de `GET /api/v1/me` no console, antes de entrar, é normal: é o app
perguntando se já há sessão.

## 3. O histórico e o selo

1. Seis ditados de exemplo, de hoje, de ontem e de alguns dias atrás (em que dia cada um cai depende
   da hora em que você subiu o servidor).
2. Um deles, "Confirmo a entrega para quinta à tarde.", tem a etiqueta discreta **2 versões**: o
   mesmo ditado mudou em dois PCs antes de sincronizar.
3. Clique nele. Em cima do editor: "Mudou em dois lugares. Ficou esta; a outra está guardada.", as
   duas versões ("Ficou" e "A outra", com a hora), as palavras que mudam sublinhadas, **Ficar com
   esta** e **Ficar com a outra**, e embaixo "A que você não escolher é descartada."
4. Clique em **Ficar com a outra**. O que você deve ver: o aviso "Ficou a versão escolhida.", o selo
   some, e o texto passa a "Confirmo a entrega para sexta de manhã."
5. Mude o texto de outro ditado e clique em **Salvar**: o aviso é só "Texto salvo.", sem "Aprender"
   (aprender continua só no PC).
6. Na busca, escreva `relatório`: aparece um ditado. Se quiser conferir, no F12, aba Rede, o pedido
   é um `POST` para `/api/v1/transcriptions/search`: o texto buscado vai no corpo, nunca no endereço.

## 4. A lixeira

1. Clique em **Lixeira**: "Rascunho antigo que não serve mais.", com **Restaurar** e **Excluir
   definitivamente**.
2. Clique em **Excluir definitivamente**. Na própria linha, sem janela: "Excluir para sempre? Não dá
   para desfazer.", com **Excluir** (em vermelho) e **Cancelar** (o foco já está nele). **Cancelar**,
   ou Esc, fecha sem excluir.
3. Abra de novo e clique em **Excluir**: a linha some, e o aviso diz "Ditado excluído para sempre."

## 5. Sem conexão e sessão expirada

1. Com o app aberto, vá à janela do servidor e aperte **Ctrl+C**.
2. No app, clique em **Histórico**. No topo, a faixa **"Sem conexão. O que você ditou continua no
   PC."**, com **Tentar de novo**; a lista que estava na tela fica.
3. Abra um ditado, mude o texto e clique em **Salvar**: o editor diz "Sem conexão: a edição não foi
   salva.", e o seu texto continua lá. Depois, clique em **Cancelar** no editor: um texto não salvo
   seguraria a tela no passo 5 (é a seção 6).
4. Suba o servidor de novo, com o mesmo comando da seção 1. Ele recomeça do zero: a memória sumiu, e a
   sua sessão também.
5. No app, clique em **Tentar de novo**, na faixa. A tela inteira vira **"Sua sessão terminou."** /
   "Entre de novo para ver seu histórico." Entre de novo com a conta de teste: os exemplos voltaram.

Desde a F5, recarregar a página (F5) com o servidor desligado mostra a tela inteira "Sem conexão." (o
app guarda a casca no navegador); antes dela, era a página de erro do próprio Chrome. O roteiro do
app instalado é o `docs/teste-manual-nuvem-f5.md`, que diz também como limpar o que ele guarda.

## 6. A sessão que acaba com um texto não salvo

Se a sessão acaba enquanto você edita, o texto não some: a tela fica, e você entra de novo sem sair
dela.

1. Abra um ditado, mude o texto e **não** salve.
2. Abra uma segunda aba (**Ctrl+T**) em `http://localhost:8080`: ela já entra, com a mesma sessão.
   Clique em **Configurações** e em **Sair**. Feche essa aba.
3. Na primeira aba, clique em **Salvar**. O que você deve ver: o editor continua na tela, com o seu
   texto, e no topo a faixa **"Sua sessão terminou. Seu texto continua aqui; entre de novo para
   salvar."**, com **Entrar com a conta de teste**. Não aparece a tela inteira "Sua sessão
   terminou.".
4. Com a faixa na tela, aperte **F5**. O Chrome pergunta se quer recarregar (o aviso de que as
   alterações podem não ser salvas): clique em **Cancelar**, e o texto continua lá. (Sem a faixa, o F5 recarrega
   sem perguntar.)
5. Clique em **Configurações** e em **Sair**. Em vez de sair, abre ali mesmo "**Sair com texto não
   salvo**", com **Sair e descartar o texto** e **Cancelar**, e o cursor no Cancelar. Clique em
   **Cancelar** (ou aperte Esc): a pergunta some. Volte ao **Histórico**: a linha diz "não salvo";
   clique nela, e o editor abre de novo com o seu texto.
6. Para ver a faixa quando a janela é baixa (o teclado aberto no celular): F12, o ícone de
   dispositivo, e uma janela de 375 x 440. A faixa fica numa linha só, o título e o botão.
7. Clique em **Entrar com a conta de teste**. A faixa some, o aviso embaixo diz "Você entrou de novo.
   Pode salvar.", e o cursor está no texto. Clique em **Salvar**: "Texto salvo.", e a linha mostra o
   texto novo.

Num servidor de verdade, o botão da faixa é **Entrar de novo**: ele abre o login do Google numa janela
à parte (no Android, outra aba), que se fecha sozinha depois de entrar, e a faixa some quando você
volta à aba do texto. O servidor de teste não tem o Google, então isso só se vê no deploy (F11); os
testes automáticos cobrem a janela, a janela bloqueada e a volta.

## 7. Configurações

1. Clique em **Configurações**. Só três seções:
   - **Conta**: "Entrou como voce@teste.local." e **Sair**;
   - **Sessões abertas**: "Windows · Chrome", o último uso e a etiqueta "este aparelho";
   - **Privacidade**: o que vai para a nuvem e o que nunca vai, e **Apagar meus dados da nuvem**.

   Nada de chave de API, microfone, atalho, início automático ou modo de texto: isso fica em cada PC.
2. Abra uma **janela anônima** (Ctrl+Shift+N), em `http://localhost:8080`, e entre com a conta de teste
   lá também. Volte à primeira janela, clique em **Histórico** e de novo em **Configurações**: são duas
   sessões, e a outra tem **Encerrar**. Clique: a linha some, e o aviso diz "Sessão encerrada." Na
   janela anônima, clique em **Lixeira**: ela vira "Sua sessão terminou." Feche a janela anônima.
3. Clique em **Apagar meus dados da nuvem**. Abre ali mesmo: "Apaga tudo desta conta na nuvem e
   desconecta os PCs. Os PCs ficam com a cópia deles." e o campo "Para confirmar, digite apagar".
   **Apagar tudo** só funciona com `apagar` digitado. Digite e clique: o aviso "Seus dados foram
   apagados da nuvem.", e o Histórico fica vazio ("O que você ditar num PC conectado aparece aqui.").
4. Clique em **Sair**: volta para a tela Entrar.

## 8. (Opcional) Tela de celular no PC

Com o app aberto, F12, depois **Ctrl+Shift+M** (a barra de dispositivos), e escolha uma largura de
celular (360 ou 390 px). O que você deve ver: tudo numa coluna, sem rolar para o lado; os botões
ficam mais altos quando a barra simula um celular (toque).

## 9. (Opcional) No Android, pelo cabo USB

Precisa: o celular com a **Depuração USB** ligada (em "Opções do desenvolvedor"), o cabo e o Chrome no
celular.

1. Ligue o celular no PC pelo cabo e, no celular, aceite "Permitir depuração USB".
2. No Chrome do PC, abra **`chrome://inspect/#devices`**. O celular aparece na lista.
3. Clique em **Port forwarding...**. Em "Port", `8080`; em "IP address and port", `localhost:8080`.
   Marque **Enable port forwarding** e clique em **Done**.
4. No Chrome do celular, abra **`http://localhost:8080`**. O PC encaminha a porta pelo cabo, e o
   celular vê o app como `localhost`, que conta como endereço seguro: o login de teste e o cookie da
   sessão funcionam.
5. O que você deve ver: as mesmas telas, na largura do celular. Entre com a conta de teste, abra o
   selo, a lixeira e as Configurações: a sessão do celular aparece como "Android · Chrome", e a do PC
   continua na lista.

O "Instalar app" do Chrome é a F5: o roteiro dela (`docs/teste-manual-nuvem-f5.md`) instala, confere o
ícone e desinstala no fim.
A depuração USB pode ficar ligada ou ser desligada depois, como preferir.

## 10. Desligar

Na janela do servidor, **Ctrl+C**. Não sobra nada dele: nem ditado, nem sessão, nem chave. Fica só a
pasta do build (`%USERPROFILE%\.aurora-whisper-dev\painel-nuvem`), que o próximo comando refaz.

## Se algo der errado

| O que aparece | O que é |
|---|---|
| `ERRO: A porta 8080 ja esta em uso` | outro programa usa a porta; rode com `-Porta 8081` |
| `ERRO: Nao achei o pnpm no PATH` (ou o `go`) | falta a ferramenta; instale e abra uma janela nova do PowerShell |
| `host inválido` | a página foi aberta em `127.0.0.1`; use `localhost` |
| a página de erro do Chrome ("Não é possível acessar esse site") | o servidor está desligado (ou noutra porta) |
| no celular, a página não abre | o encaminhamento de porta não está ligado (seção 9, passo 3) ou o cabo caiu; o `chrome://inspect` mostra |

Quando terminar, conte o que viu e o que estranhou, principalmente nas telas do celular.
