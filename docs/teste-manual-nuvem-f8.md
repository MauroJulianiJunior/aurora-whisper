# Teste manual da nuvem, F8: o que você aprende no PC aparece no app, e o contrário

**O que é e por que:** quando você corrige uma palavra no painel e clica em **Aprender**, o dicionário
passa a trocar essa palavra em todo ditado daí em diante. Na F8, essa troca também sobe para a sua
nuvem: aparece no app (o do celular; aqui, o navegador do PC faz o papel dele) e chega aos outros PCs
seus, e dá para **desfazer** de qualquer lado. Este roteiro confere isso de ponta a ponta no seu PC,
com o Agent do `bin\` e a nuvem de teste (na memória, sem Google e sem servidor de verdade).

**Dois motivos para ir com cuidado:**

1. O Agent novo acrescenta duas migrations ao `historico.db` do `bin\` (a 0007 e a 0008): só tabelas
   novas e duas colunas vazias. Nenhum ditado muda (a prova está na ADR-019, "Decisões da F8").
2. A troca que você vai aprender de teste (`contrato` vira `acordo`) **vale de verdade** nos seus ditados
   do `bin\` enquanto existir. O roteiro a desfaz no fim, duas vezes.

**O histórico antigo ainda não subiu.** A conferência de 08-out-2026 no `historico.db` do `bin\` mostrou
que o "Enviar o histórico" da F7 nunca gravou o aceite nem a cópia: só os ditados feitos depois de
conectar têm versão da nuvem. Este roteiro envia o histórico de verdade e confere no disco.

ADR-019, fatia F8 (F8a a F8h). Tempo estimado: 30 minutos. Precisa da F8 no `main` (a F8g e a F8h já
estão, desde 08-out-2026).

**O que conferir, em uma frase:** conectar de novo; aprender uma troca antes de enviar e ver a pergunta
contá-la; enviar o histórico e ver que subiu; ver a troca no app (e só ela); desfazer no app e ver a
troca sumir do PC; e o contrário.

## Antes de começar (são seis avisos)

1. **O Agent aberto fecha durante o teste** (a instância é única). O passo 2 o fecha; o 5 abre o novo.
2. **Não desligue a nuvem de teste até o passo 12.** Ela guarda tudo na memória: religada, esquece o
   PC e as trocas, e o painel passa a dizer "Este PC foi desconectado na nuvem."
3. **O endereço é sempre `http://localhost:8080`**, e não `127.0.0.1`. Use só essa porta: um PC que já
   foi ligado a outro endereço recusa este ("Este PC já está ligado a outra conta."); se isso
   acontecer, use o **Esquecer esta nuvem** (passo 12) antes de conectar. Se a 8080 estiver ocupada, o
   passo 1 diz o que fazer.
4. **A nuvem sobe vazia** (com `-SemExemplos`, no passo 1): tudo o que a nuvem tem desce para o
   histórico do `bin\`, e os ditados inventados dos outros roteiros ficariam lá para sempre.
5. **O token vai para o Gerenciador de Credenciais** no nome `AuroraWhisper/nuvem-portatil`. O passo 12
   confere que ele saiu.
6. **O Agent que está aberto é o da F7** (compilado em 07-out). Por isso os passos 3 e 4 montam o painel
   e o Agent de novo: sem eles, nada da F8 aparece.

Use duas janelas do PowerShell: a da nuvem (passo 1) fica presa no servidor; o resto vai na outra.

## 1. Subir a nuvem de teste, vazia (janela 1)

```powershell
powershell -File C:/Desenvolvimento/aurora-whisper/scripts/nuvem-local-teste.ps1 -SemExemplos
```

**Deve aparecer:** "Compilando o painel no modo nuvem...", "Sem os ditados de exemplo: a nuvem sobe
vazia, so com a conta de teste.", depois **"Nuvem de teste em http://localhost:8080"**. Na primeira vez
pode levar um minuto. Deixe a janela aberta.

O script compila o painel da nuvem em `%USERPROFILE%\.aurora-whisper-dev\painel-nuvem`, fora do
`bin\`. Se `web\node_modules` não existir, ele instala as dependências com o pnpm antes (a janela mostra
"Instalando as dependencias do painel (pnpm install)...").

**Se aparecer "A porta 8080 ja esta em uso…"**, o script parou antes de subir. Veja quem usa a porta
(só leitura):

```powershell
Get-NetTCPConnection -State Listen -LocalPort 8080 | Select-Object OwningProcess
```

O número é o Id do processo (`Get-Process -Id <número>` mostra o nome). Feche esse programa e rode o
comando do passo 1 de novo. Não troque a porta (aviso 3).

O Agent velho, ainda aberto, vai notar que a nuvem nova não o conhece e se desconectar sozinho. Tudo
bem: ele vai fechar no passo seguinte.

## 2. Fechar o Agent e conferir (janela 2)

Primeiro, veja qual Agent está aberto (só leitura):

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue | Select-Object Id, Path
```

**Deve aparecer:** o `Path` em `C:\Desenvolvimento\aurora-whisper\bin\aurora-whisper-agent.exe`. Se o
`Path` for outro (o Agent instalado, o do dia a dia, tem o mesmo nome de arquivo), **pare e me avise**
antes de fechar: o `taskkill` abaixo o fecharia também. Se não aparecer nada, não há Agent aberto: o
`taskkill` só vai reclamar que não achou o processo, e o passo 3 segue igual.

O `taskkill` sem `/F` é o mesmo que **Sair** na bandeja.

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

**Deve aparecer:** o `Get-Process` não mostra nada. Se mostrar, espere uns segundos e rode de novo.

## 3. Montar o painel do PC (passo próprio)

O painel vai para `bin\painel`; sem ele, o Agent novo abre o painel velho, sem nada da F8.

```powershell
pnpm -C C:/Desenvolvimento/aurora-whisper/web build
```

**Deve aparecer:** "built in …" no fim.

## 4. Montar o Agent do `bin\`

```powershell
go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

**Deve aparecer:** nada (é o certo).

## 5. Abrir o Agent do `bin\` e conferir que é o novo

```powershell
Start-Process C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent | Select-Object StartTime, Path
```

**Deve aparecer:** o `StartTime` com a hora de agora e o `Path` em `...\aurora-whisper\bin\`. Se a hora
for antiga, o Agent velho continua aberto: volte ao passo 2.

Na primeira subida o Agent aplica as migrations 0007 e 0008 no `historico.db`; leva um instante. O ícone
volta à bandeja quando o modelo termina de carregar.

## 6. Conectar de novo (como na F6 e na F7)

1. Feche qualquer aba antiga do painel e abra o painel pela bandeja (**Abrir Aurora Whisper**). Atualizar
   a aba velha não basta.
2. **Configurações**, seção **Nuvem**. **Deve aparecer:** "Desligada. Nada vai para a nuvem até você
   conectar." e o campo com `http://localhost:8080`. Se o Agent novo já tiver visto a nuvem recusar o PC
   (a nuvem nova não o conhece; é o esperado), a frase "Este PC foi desconectado na nuvem." aparece logo
   acima. Esse aviso fica só na memória do Agent, por isso nem sempre aparece. Se o campo vier vazio,
   escreva o endereço. Clique em **Conectar à nuvem**.
3. Na aba que abre sozinha: **Entrar com a conta de teste**, confira o código (o mesmo do painel) e
   clique em **Aprovar**. A aba vai para "**PC conectado**". Feche-a e volte ao painel. Se nenhuma aba
   abrir, o painel mostra "O navegador não abriu. Use Abrir de novo.": clique em **Abrir de novo** e
   siga daí.

**Deve aparecer, na seção Nuvem:** "Conectado como **voce@teste.local**." e, em seguida (é a primeira
conferência com a nuvem, alguns segundos), "Conferindo a nuvem…" e depois a pergunta:

> Vão subir N ditados dos últimos 6 meses. A lixeira não vai. Antes, uma cópia do histórico fica neste PC.

com o botão **Enviar o histórico**. **Ainda não clique.** Se a pergunta não aparecer em meio minuto,
clique em **Histórico** e em **Configurações** de novo.

**Anote o N.** Ele é: os ditados do `bin\` dos últimos 6 meses, fora da lixeira, que a nuvem ainda não
tem. Hoje é quase o seu histórico inteiro. Os ditados que você fez depois de conectar na F7 podem subir
sozinhos de novo (a nuvem nova é como uma nuvem restaurada de backup); isso não entra no N e não é erro.

**A frase não fala de troca aprendida, e é o certo:** o dicionário do `bin\` ainda não tem nenhuma troca
feita pelo Aprender. As grafias que você pôs em **Dicionário** (o botão **Adicionar**) não contam (ficam
em cada PC, de propósito).

Na **Privacidade**, logo abaixo, entra a frase "Com a nuvem conectada, o texto dos seus ditados e as
trocas que você aprendeu vão para a sua nuvem, em http://localhost:8080. …". **Na bandeja**, clique com o
botão direito no ícone: a linha da nuvem aparece no menu, desabilitada (por exemplo, "Nuvem: em dia").

## 7. Aprender uma troca, antes de enviar

Nada aprendido sobe antes do clique em **Enviar o histórico** (a decisão D3). Por isso, antes de clicar,
vamos aprender uma troca e ver a pergunta passar a contá-la.

1. Num Bloco de Notas, dite: **"o contrato está assinado"**. (Esse ditado novo sobe para a nuvem
   sozinho; ele não entra no N.)
2. No painel, **Histórico**: clique no ditado do topo e, no **Texto final**, troque **contrato** por
   **acordo**. Clique em **Salvar** (ou `Ctrl+Enter`).
3. Embaixo aparece a pílula **Texto salvo. Aprender “contrato” → “acordo”?** com o botão **Aprender**;
   ela espera uns 30 segundos. Clique em **Aprender**.
   **Deve aparecer:** "Aprendido: “contrato” vira “acordo” no próximo ditado."
4. **Configurações**, seção **Aprendido no painel**: **Deve aparecer** a linha `contrato → acordo` com
   o botão **Desfazer**, e só ela (as grafias que você pôs em **Dicionário** ficam nessa outra seção).
5. Na seção **Nuvem**, ainda sem clicar. **Deve aparecer:** a pergunta agora conta a troca.

   > Vão subir N ditados dos últimos 6 meses e 1 troca aprendida. A lixeira não vai. Antes, uma cópia
   > do histórico fica neste PC.

   O N é o mesmo do passo 6, e a troca é **1**: as 6 grafias que você pôs em Dicionário (o botão
   **Adicionar**) não sobem (senão seriam 7). Se a pergunta ainda não mostrar a troca, espere uns segundos e clique em
   **Histórico** e em **Configurações** de novo.

Se o ASR escrever a frase de outro jeito (por exemplo, "Contrato" com maiúscula), use a palavra que ele
escreveu: o par é o que **ele** escreveu. Se a pílula sumir antes do clique, nada foi aprendido: repita
os passos 1 a 3 com outro ditado.

(Se quiser ver a troca valendo aqui, dite a frase de novo: sai "o acordo está assinado".)

## 8. Enviar o histórico e conferir que subiu

1. Clique em **Enviar o histórico**.
   **Deve aparecer:** o botão vira "Copiando o histórico…" por um instante; depois, "Enviando o
   histórico: X de N." (o X sobe sozinho) e, no fim, essa linha some e aparecem o aviso "O histórico
   subiu para a nuvem." e a linha "Em dia com a nuvem. Conferido às HH:MM.". Com mais de 5.000
   ditados, o envio para uns segundos no meio (a nuvem pede para esperar) e continua sozinho. **Se a
   nuvem recusar algum ditado**, o aviso e a linha dizem também "**k ditados não subiram para a
   nuvem.**": eles ficam só no PC. Anote o k.
2. **Na bandeja**, durante o envio, clique com o botão direito no ícone: a linha da nuvem aparece no
   menu, desabilitada ("Nuvem: enviando X de N").
3. A cópia, ao lado do banco. Na F7 ela não apareceu; é a prova de que o envio rodou:

```powershell
Get-ChildItem C:/Desenvolvimento/aurora-whisper/bin/historico.db.antes-da-nuvem-*.db | Select-Object Name, Length, LastWriteTime
```

   **Deve aparecer:** um arquivo com a hora do clique, do tamanho do `historico.db` (ou quase).
4. No navegador, abra `http://localhost:8080`. Se o app abrir logado, siga. Se pedir para entrar (ou
   mostrar "Sua sessão terminou."), clique em **Entrar com a conta de teste**. Nem o app nem o painel
   mostram o total, então a conferência é por amostra:
   - **as primeiras linhas** do Histórico do app são as mesmas do Histórico do PC, na mesma ordem
     (os seus ditados mais novos; no topo, o "o acordo está assinado" do passo 7);
   - **a Lixeira do app** está vazia;
   - (opcional) no app, clique em **Mostrar mais antigos** até o botão sumir: o ditado mais antigo do
     app tem menos de 6 meses. Se o seu histórico não tem nada de mais de 6 meses, não há o que ver aqui.

## 9. A troca no app (e só ela)

1. No app, **Configurações**. Entre **Aparelhos** e **Sessões abertas** está a seção **Aprendido**, com
   "Trocas que os PCs aprenderam ao corrigir ditados." e a linha `contrato → acordo` com **Desfazer**.
   Se ainda disser "Nada aprendido ainda.", espere até 1 minuto (a troca sobe na rodada seguinte ao
   clique) e recarregue a página (`F5`).
2. **Deve aparecer só essa linha.** As grafias que você pôs em **Dicionário** não aparecem aqui: ficam
   em cada PC, de propósito (a decisão D1). Para ver o que ficou de fora, no PC: **Configurações** >
   **Dicionário** > **Adicionados por você**.
3. Na **Privacidade** do app: "Vão para a nuvem: o texto dos ditados, com a data, o PC de origem e os
   detalhes da transcrição, e as trocas aprendidas."

## 10. Desfazer no app, e a troca sumir do PC

1. No app, clique em **Desfazer** na linha `contrato → acordo`. **Deve aparecer**, na própria linha:
   "Desfazer? A troca deixa de valer em todos os PCs.", com **Desfazer** (em vermelho) e **Cancelar**.
   Clique em **Desfazer**. **Deve aparecer:** o aviso "Desfeito em todos os aparelhos.", a linha some e a
   seção diz "Nada aprendido ainda."
2. No PC, espere até 1 minuto com o painel fechado (o Agent busca a cada 60 s), ou cerca de 13 s com
   o painel aberto (o Agent busca a cada 10 s enquanto ele está aberto; ADR-021, subfatia S). Depois
   clique em **Histórico** e em **Configurações**: o aviso de mudanças do painel não conta o que é só do
   aprendido, e a seção só relê quando a tela aparece. **Deve aparecer:** a seção **Aprendido no
   painel** sumiu, porque a troca era a única. **Dicionário** > **Adicionados por você** segue igual:
   o desfazer só tira a troca do Aprender.
3. Dite de novo: **"o contrato está assinado"**. **Deve aparecer:** "o contrato está assinado", e não
   mais "o acordo…".

## 11. O contrário: aprender no PC, desfazer no PC

1. No PC, repita os passos 7.1 a 7.3 (dite, troque **contrato** por **acordo**, **Salvar**, **Aprender**).
   Desta vez a pergunta do envio não volta: o histórico já foi aceito, e a troca sobe sozinha.
2. No app, recarregue a página (`F5`) e abra **Configurações**. **Deve aparecer:** `contrato → acordo`
   de volta na seção **Aprendido**, em poucos segundos (no máximo 1 minuto).
3. No PC, **Configurações** > **Aprendido no painel** > **Desfazer** na linha.
   **Deve aparecer:** "Desfeito: “contrato” não vira mais “acordo”." e a seção some.
4. No app, recarregue (`F5`) e abra **Configurações**. **Deve aparecer**, em até 1 minuto: a seção
   **Aprendido** vazia ("Nada aprendido ainda.").
5. Dite uma última vez a frase do passo 10.3: sai "o contrato está assinado".

## 12. Esquecer a nuvem e limpar

1. No PC, **Configurações**: a seção **Aprendido no painel** não deve existir (nada aprendido). Se
   existir, clique em **Desfazer** na linha: a troca de teste não pode ficar nos seus ditados.
2. **Configurações** > **Nuvem** > **Desconectar**. **Deve aparecer:** "Desligada. Nada vai para a nuvem
   até você conectar.", o campo com o endereço e, embaixo, **Esquecer esta nuvem**.
3. Clique em **Esquecer esta nuvem**. **Deve aparecer**, na própria linha: "**Esquecer
   http://localhost:8080?** Este PC deixa de saber dessa nuvem e da conta voce@teste.local. Os ditados
   continuam aqui; na nuvem, nada muda.", com **Esquecer** (em vermelho) e **Cancelar**. Se aparecer
   também "N mudanças feitas aqui ainda não subiram.", anote o N. Clique em **Esquecer**:
   o aviso "Este PC esqueceu a nuvem." e o campo vazio. Em **Histórico**, os seus ditados continuam
   todos lá.
4. Confira que o token de teste saiu:

```powershell
cmdkey /list:AuroraWhisper/nuvem-portatil
```

   **Deve aparecer:** nenhuma credencial (`NONE` ou `NENHUM`).
5. Na janela 1, **Ctrl+C** para desligar a nuvem de teste.
6. Feche o Agent do `bin\`:

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

7. Abra o Agent que você usa no dia a dia (o instalado, pelo **Menu Iniciar**, se for o caso).

**A cópia de antes do envio** fica em `C:\Desenvolvimento\aurora-whisper\bin\` (um arquivo por envio
feito). Ela tem todo o texto ditado até o clique, inclusive a lixeira, e o Agent nunca a apaga. Pode
guardar ou apagar depois de conferir o app. Com a F10 no `bin\`, o jeito é o botão **Apagar as
cópias**, no bloco **Histórico** das Configurações (`docs/teste-manual-nuvem-f10.md`, passo 11);
antes dela, à mão:

```powershell
Remove-Item C:/Desenvolvimento/aurora-whisper/bin/historico.db.antes-da-nuvem-*.db -Confirm
```

## O que me contar

- O N do passo 6, e a frase inteira da pergunta no passo 7 (com a troca).
- Se a cópia do passo 8 apareceu, se as primeiras linhas do app eram as mesmas do PC e se a Lixeira do
  app estava vazia. Se apareceu "k ditados não subiram", o k.
- Se o app mostrou **só** `contrato → acordo` no passo 9 (e nenhuma das grafias que você pôs em Dicionário).
- Quanto tempo a troca levou para sumir do PC depois do Desfazer no app (passo 10), e se o ditado
  seguinte saiu com "contrato".
- O mesmo no sentido contrário (passo 11): quanto levou para aparecer no app e para sumir dele.
- O que o `cmdkey` mostrou no passo 12.
- Qualquer frase diferente das deste roteiro, com um print.
- Só se algo não bater: as últimas linhas do log, que têm só contagens (nunca texto):

```powershell
Select-String -Path C:/Desenvolvimento/aurora-whisper/bin/agent.log -Encoding UTF8 -SimpleMatch "sync: rodada" | Select-Object -Last 8
```

## O que este roteiro não testa

- **Dois PCs de verdade.** Aqui há um PC só, e o "celular" é o app no navegador dele. A troca que chega
  a outro PC (com a restrição e o domínio, sem clique lá) é provada por teste automático, com dois PCs e
  o celular sobre a nuvem em memória, mas não à mão.
- **As etiquetas da decisão D4** ("desfeito em outro aparelho" e "trocado em outro aparelho") e o "não
  aplicado neste PC": pedem dois aparelhos disputando a mesma troca, ou um PC sem o domínio. Também
  estão nos testes automáticos.
- **A troca presa a um domínio** e **o freio** (mais de 10 trocas saindo do dicionário de uma vez):
  provados por teste, e não à mão.
- **O Google de verdade, o celular instalado e o servidor na VPS** (F11), e **o Postgres** (a nuvem de
  teste é a de memória, que tem as mesmas regras; os testes de Postgres da F8a a F8d rodaram em
  07-out-2026).
- **A retenção de 6 meses** (F9 e F10).
