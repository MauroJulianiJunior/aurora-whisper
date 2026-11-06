# Teste manual do dicionário do PC no celular (ADR-023)

Este roteiro é seu (do Mauro), e vem **depois** do deploy: a nuvem já tem de estar com a API nova no ar e o
Agent novo rodando no PC (e a política de privacidade nova, aprovada e republicada: ADR-023, risco 4). Os
testes automáticos cobrem a cópia, as rotas, a montagem do Smart e a paridade com o PC contra uma nuvem de
mentira; eles nunca usam o celular de verdade, a voz nem a sua conta. Só aqui isso acontece.

**Para quê:** confirmar que o dicionário do PC chega ao celular só quando você liga a opção, que um termo
que só existe no dicionário do PC sai certo no ditado do celular, que uma mudança no arquivo chega ao
celular, e que desligar apaga a cópia da nuvem.

**O que você precisa:**
- o **PC** com o Agent novo ligado e **conectado** à sua conta, e o painel aberto pela bandeja;
- o **celular** com o app da nuvem entrado na mesma conta e o **ditado pelo celular ligado** (Configurações
  do app → "Ditar pelo celular": Ligado, com a chave do provedor);
- um **termo que só existe no dicionário do PC**, que o ditado do celular não escreve certo hoje. Se não
  tiver um, crie um agora: no painel do PC, em Configurações → **Dicionário**, adicione o termo (por
  exemplo, o nome de um programa ou de um cliente) com a grafia que o reconhecimento de voz erra, e salve.
  Anote o termo e como você vai falá-lo.

**Quanto espera:** o PC leva uma mudança do arquivo à nuvem na rodada seguinte do sync, em até uns 2
minutos. O celular só enxerga a cópia nova no próximo ditado ou ao reabrir as Configurações.

**Se algo não sair como está escrito:** pare naquele passo, anote o número do passo e o que apareceu (um
print ajuda), e siga para a próxima parte. No fim, me conte tudo.

## 0. Antes

1. Abra o `dicionario.json` do PC só para ver quantos termos tem (sem mostrar os termos aqui), no
   PowerShell:
   ```
   (Get-Content -Raw -Encoding UTF8 "$env:LOCALAPPDATA\AuroraWhisper\dicionario.json" | ConvertFrom-Json).entradas.Count
   ```
   Anote o número. Ele é o N que vai aparecer no painel e no celular. (Se o Agent roda em modo portátil, o
   arquivo está ao lado do executável: troque o caminho.)
2. No **celular**, abra Configurações → **Ditar pelo celular** e olhe abaixo da caixa da dica.
   **Deve aparecer:** `Sem o dicionário do PC. Para usá-lo aqui, ligue "Levar o dicionário para o celular"
   no painel do PC.` A caixa da dica diz **"Mandar os termos do dicionário como dica"**.

## 1. Ligar no PC

1. No **PC**, painel → Configurações → **Nuvem** (conectada). Procure a opção **Levar o dicionário para o
   celular**.
   **Deve aparecer:** a caixa **desligada**, com "O celular passa a usar os termos deste PC. Editar
   continua aqui."
2. Marque a caixa.
   **Deve acontecer:** na hora, a caixa fica marcada e a frase vira `N termos, enviados às HH:MM.`, com o N
   do passo 0 e a hora de agora. Se aparecer uma frase de erro, a caixa continua desmarcada: anote a frase.
3. No **celular**, reabra Configurações → **Ditar pelo celular**.
   **Deve aparecer:** `Dicionário: o do <nome do seu PC>, N termos (atualizado em DD/MM às HH:MM).`

## 2. Ditar um termo do dicionário do PC

1. No celular, dite uma frase com o **termo que só existe no dicionário do PC**, do jeito que você o fala.
   **Deve acontecer:** o ditado sai com o termo **escrito como está no dicionário**.
2. (Opcional) Ligue **Mandar os termos do dicionário como dica** e dite de novo, com o termo no meio de uma
   frase maior.
   **Deve acontecer:** o termo sai certo, igual ou melhor que sem a dica. A dica leva até 140 caracteres dos
   termos ao provedor da voz; **desligue-a depois** se não quiser mantê-la.

## 3. Mudar o arquivo no PC

1. No PC, no painel → Configurações → **Dicionário**, adicione **mais um termo** (qualquer um).
2. Espere até uns 2 minutos e reabra no celular Configurações → **Ditar pelo celular**.
   **Deve acontecer:** a linha do dicionário mostra **N + 1 termos**, com a hora nova.
3. No PC, a frase da opção também mostra `N + 1 termos, enviados às HH:MM` quando você reabre a seção
   Nuvem.

## 4. Desligar

1. No PC, desmarque a caixa **Levar o dicionário para o celular**.
   **Deve acontecer:** na hora, a caixa fica desmarcada e a frase volta a "O celular passa a usar os termos
   deste PC. Editar continua aqui." Se aparecer "Não deu para apagar a cópia da nuvem agora. Tente de novo.",
   a caixa continua marcada: anote, e tente de novo.
2. No celular, reabra Configurações → **Ditar pelo celular**.
   **Deve aparecer:** `Sem o dicionário do PC. Para usá-lo aqui, ligue "Levar o dicionário para o celular"
   no painel do PC.`
3. Dite de novo o termo do passo 2.
   **Deve acontecer:** ele **não** sai mais pelo dicionário do PC (sai como o reconhecimento de voz o
   escreve, ou pelo que o botão Aprender já ensinou à conta).

## O que me contar

- Se cada "Deve aparecer" e "Deve acontecer" saiu como está escrito, e o número do passo de cada um que não
  saiu (com a frase que apareceu e um print).
- O N do passo 0, o que o painel mostrou no passo 1.2, e o que o celular mostrou nos passos 1.3 e 3.2.
- Quanto tempo o celular levou para mostrar o termo novo no passo 3.
- Se o termo do passo 2 saiu certo **sem** a dica e **com** a dica, e se o ditado ficou mais lento.
- Se, em algum momento, a opção do PC se desmarcou sozinha (isso só deve acontecer se outro PC da conta
  tomou a fonte: o painel diz "O celular passou a usar o dicionário de ...").
