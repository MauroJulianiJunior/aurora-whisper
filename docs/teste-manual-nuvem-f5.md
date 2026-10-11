# Teste manual da nuvem, F5: o app instalado no PC e no Android

**O que é:** o app da nuvem (o da F4) agora se instala como um programa: ícone no Iniciar do Windows e
na tela inicial do celular, janela própria, sem barra de endereço, e ele abre mesmo sem rede, com a
tela "Sem conexão." no lugar da página de erro do navegador. **Por que agora:** é a última peça do
celular antes do servidor de verdade (F11). O que só uma pessoa vê é aqui: o ícone no formato do
launcher do seu celular, o nome embaixo dele ("Whisper"), a cor da barra e o app abrindo sem o
servidor.

ADR-019, fatia F5. Tempo estimado: 10 minutos (mais 10 com o Android).

Nada aqui toca os seus ditados reais: é o mesmo servidor de teste da F4, na memória, com ditados
inventados. **No fim, desinstale e limpe** (seção 4): o Chrome guarda a casca do app para o endereço
`localhost:8080`.

## 1. Subir (um comando)

```powershell
powershell -File C:\Desenvolvimento\aurora-whisper\scripts\nuvem-local-teste.ps1
```

Espere **"Nuvem de teste em http://localhost:8080"**. Deixe a janela aberta. (Porta ocupada: rode com
`-Porta 8081` e troque o número nos endereços abaixo.)

## 2. No PC (Chrome)

1. Abra **`http://localhost:8080`** (`localhost`, e não `127.0.0.1`) e clique em **Entrar com a conta de
   teste**.
2. Instale: o ícone de instalar no fim da barra de endereço, ou o menu **⋮** (procure **Instalar
   Aurora Whisper**). O que você deve ver: o app abre numa janela própria, sem barra de endereço, com o
   ícone (o ladrilho escuro com os colchetes verdes) na barra de tarefas e no Iniciar.
3. Na janela do servidor, **Ctrl+C**. Feche o app e abra de novo pelo Iniciar. O que você deve ver:
   **"Sem conexão."**, "O que você ditou continua no PC." e **Tentar de novo**, com a Aurora e as
   fontes de sempre. **Não** pode aparecer o dinossauro nem "Não é possível acessar esse site".
4. Suba o servidor de novo (o mesmo comando da seção 1) e clique em **Tentar de novo**: aparece a tela
   **Entrar** (o servidor de teste esqueceu a sessão ao desligar). Entre de novo com a conta de teste:
   os exemplos voltaram.

## 3. No Android (pelo cabo USB)

Precisa: a **Depuração USB** ligada (Opções do desenvolvedor), o cabo e o Chrome no celular.

1. Ligue o cabo e aceite "Permitir depuração USB". No Chrome do PC, abra **`chrome://inspect/#devices`**,
   clique em **Port forwarding...**, ponha `8080` e `localhost:8080`, marque **Enable port forwarding** e
   clique em **Done** (como na seção 9 da F4).
2. No Chrome do celular, abra **`http://localhost:8080`**, entre com a conta de teste e toque em **⋮ →
   Instalar app** (ou **Adicionar à tela inicial → Instalar**).
3. Na tela inicial, confira:
   - o nome embaixo do ícone: **Whisper**;
   - o ícone **inteiro no formato do seu launcher** (círculo, squircle ou quadrado arredondado), sem
     colchete nem barra cortados;
   - o app abre **sem a barra de endereço**, com a tela de abertura escura;
   - a barra de status acompanha o tema do celular (clara no claro, escura no escuro).
4. Desligue o servidor no PC (**Ctrl+C**), feche o app pelos recentes e abra de novo pelo ícone: **"Sem
   conexão."**.

Pelo cabo, o Chrome pode criar só um atalho (com o selo pequeno do Chrome no canto do ícone) em vez de
instalar de verdade: o celular não alcança o `localhost` do PC para montar o app instalado. Se
acontecer, conte; a instalação de verdade só se confere com o HTTPS da F11.

## 4. No fim: desinstale e limpe os dois

**Por quê:** o service worker fica guardado no Chrome para `localhost:8080`, e responderia ao endereço
`/` de outro projeto seu nessa porta. Ele sempre vai à rede primeiro e não quebra nada com o servidor
ligado, mas não deve ficar lá à toa.

- **PC:** na janela do app, **⋮ → Desinstalar Aurora Whisper**, marcando a opção de limpar também os
  dados do Chrome. Se não aparecer a opção, abra `chrome://settings/content/all`, procure
  `localhost:8080` e clique na lixeira.
- **Android:** toque e segure o ícone **Whisper** → **Desinstalar** (ou Remover). Depois, no Chrome do
  celular, **⋮ → Configurações → Configurações do site → Todos os sites → localhost:8080 → Limpar e
  redefinir**. Desligue o encaminhamento de porta no `chrome://inspect` do PC.
- Desligue o servidor (**Ctrl+C**).

## Se algo der errado

| O que aparece | O que é |
|---|---|
| não aparece a opção de instalar | a página está em `127.0.0.1`, ou foi aberta antes de o servidor subir; abra `http://localhost:8080` e recarregue uma vez (o F12, em Application → Service workers, mostra o `/sw.js` "activated") |
| sem o servidor, aparece a página de erro do Chrome | o app foi aberto pela primeira vez já sem rede, ou os dados do site foram limpos: abra uma vez com o servidor ligado |
| no celular, a página não abre | o encaminhamento de porta não está ligado ou o cabo caiu (seção 3, passo 1) |

Quando terminar, conte o que viu: principalmente o ícone no seu launcher, o nome e a tela sem rede.
