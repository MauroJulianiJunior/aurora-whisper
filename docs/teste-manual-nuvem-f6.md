# Teste manual da nuvem, F6: conectar o PC à nuvem de teste e revogar

**O que é:** o primeiro passo do "PC conectado que manda os ditados para o celular". A F6 só **liga**
o PC à nuvem: você cola o endereço da nuvem nas Configurações do painel do PC, aprova no navegador,
e o PC passa a ter um token guardado no Gerenciador de Credenciais do Windows. **Nenhum ditado sobe
ainda** (isso é a F7). **Por que agora:** é a parte de segurança do caminho todo (o pareamento que um
link mandado por outra pessoa não consegue usar, o token, o revogar), e dá para ver de ponta a ponta
no seu PC, sem Google e sem servidor de verdade: a nuvem de teste roda na memória, com a conta de
teste `voce@teste.local`.

ADR-019, fatia F6. Tempo estimado: 15 minutos. Precisa da F6 no `main` (depois do merge).

**O que conferir, em uma frase:** conectar o Agent do `bin\` à nuvem de teste, ver o mesmo código nos
dois lados, ver "Conectado", revogar pelo app da nuvem e ver o PC dizer que foi desconectado.

## Antes de começar (leia, são quatro avisos)

1. **O Agent instalado fecha durante o teste.** Os dois (o instalado e o do `bin\`) não rodam juntos:
   a instância é única. O roteiro fecha o instalado no passo 2 e manda abri-lo de novo no fim.
2. **Não desligue a nuvem de teste até o fim.** Ela guarda tudo na memória. Se for reiniciada, ela
   esquece o PC, e o painel passa a mostrar "Este PC foi desconectado na nuvem." É só conectar de novo
   (passos 5 a 7): a conta de teste nasce sempre com o mesmo número, e o Agent aceita a mesma conta.
   O PC fica ligado ao **endereço** da primeira conexão (a D3 da ADR-019): use sempre o mesmo, o
   `http://localhost:8080` (se a 8080 estiver ocupada e você subir com `-Porta 8081`, use
   `http://localhost:8081` daí em diante).
3. **O token de teste vai para o Gerenciador de Credenciais**, no nome `AuroraWhisper/nuvem-portatil`,
   separado do nome do Agent instalado (`AuroraWhisper/nuvem`). O passo 10 confere que ele saiu.
4. **O endereço é `http://localhost:8080`**, e não `127.0.0.1`: a nuvem recusa o outro nome.

Use duas janelas do PowerShell: a da nuvem (passo 1) fica presa no servidor; o resto vai na outra.

## 1. Subir a nuvem de teste (janela 1)

```powershell
powershell -File C:/Desenvolvimento/aurora-whisper/scripts/nuvem-local-teste.ps1
```

**Deve aparecer:** "Compilando o painel no modo nuvem...", depois **"Nuvem de teste em
http://localhost:8080"**. Deixe a janela aberta.

## 2. Fechar o Agent instalado (janela 2)

O `taskkill` sem `/F` é o mesmo que **Sair** na bandeja.

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

**Deve aparecer:** o `taskkill` diz que mandou fechar (ou que não achou o processo), e o
`Get-Process` não mostra nada. Se ainda mostrar, espere uns segundos e rode o `Get-Process` de novo.

## 3. Montar o painel e o Agent do `bin\`

O painel é passo próprio (ele escreve em `bin\painel`); sem ele, a seção Nuvem não aparece.

```powershell
pnpm -C C:/Desenvolvimento/aurora-whisper/web build
```

```powershell
go -C C:/Desenvolvimento/aurora-whisper build -ldflags "-H=windowsgui" -o bin/aurora-whisper-agent.exe ./cmd/agent
```

**Deve aparecer:** o `pnpm` termina com "built in …"; o `go build` não escreve nada (é o certo).

## 4. Abrir o Agent do `bin\` e conferir que é o novo

```powershell
Start-Process C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent | Select-Object StartTime, Path
```

**Deve aparecer:** o `StartTime` com a hora de agora e o `Path` em `...\aurora-whisper\bin\`. Se a hora
for antiga, o Agent velho continua aberto: volte ao passo 2.

## 5. Conectar à nuvem (no painel do PC)

1. Abra o painel pela bandeja (**Abrir Aurora Whisper**). Se uma aba antiga do painel estava aberta,
   feche-a antes: a sessão do painel vive só na aba.
2. Clique em **Configurações** e desça até a seção **Nuvem**.
   **Deve aparecer:** "Desligada. Nada vai para a nuvem até você conectar.", o campo **Endereço da
   nuvem** e o botão **Conectar à nuvem**.
3. No campo, escreva `http://localhost:8080` e clique em **Conectar à nuvem**.
   **Deve aparecer, no painel:** "Aprove no navegador e confira o código **XXXX**." (quatro letras e
   números), com **Abrir de novo** e **Cancelar**. **E, sozinha, uma aba nova no seu navegador** em
   `localhost:8080`.

**Se não aparecer, anote:**
- "Este build de teste só conecta da pasta bin\ (modo portátil).": o `bin\` não está no modo
  portátil (falta o arquivo `portatil` ao lado do `.exe`). Rode
  `Test-Path C:/Desenvolvimento/aurora-whisper/bin/portatil` e me diga o resultado.
- "Não deu para falar com a nuvem. Confira o endereço.": a nuvem do passo 1 não está de pé.
- A aba não abriu: o painel diz "O navegador não abriu. Use Abrir de novo."; clique em **Abrir de
  novo** e siga.
- "Este PC já está ligado a outra conta.": o `bin\` foi ligado antes a outro endereço (outra porta,
  como a `8081`) ou a outra conta. Confira se o endereço é o da primeira vez (o aviso 2). Se for o
  mesmo e o aviso continuar, pare aqui e me avise: o painel ainda não tem como esquecer a nuvem (o
  "esquecer esta nuvem" é da F7), e eu mando o caminho à parte.

## 6. Aprovar (na aba da nuvem)

1. Se a aba mostrar **Entrar**, com a linha "Entre para aprovar a conexão do PC.", clique em **Entrar
   com a conta de teste**.
2. **Deve aparecer:** "**Conectar este PC?**", o nome do seu PC com "· Windows", e num quadro "Código
   **XXXX**" (o **mesmo** do painel), "Confira se é o mesmo do painel do PC.", "Este PC vai ficar
   ligado à conta **voce@teste.local**." e "Só aprove se você clicou em Conectar neste PC agora.", com
   **Aprovar** e **Agora não**.
3. Confira que o código é o mesmo do painel e clique em **Aprovar**.
   **Deve aparecer:** a aba vai, sozinha, para um endereço `http://127.0.0.1:…/nuvem/retorno?…` e
   mostra "**PC conectado**" / "Pode fechar esta aba e voltar ao painel."

**Isto confere um risco da ADR (o risco 3):** que o Chrome deixa a página da nuvem levar o navegador
de volta ao `127.0.0.1` do PC. **Anote** se o Chrome mostrar qualquer pergunta sobre "rede local" ou
"dispositivos na rede local", ou se a aba ficar parada em "Aprovando…".

## 7. Ver "Conectado" (no painel do PC)

Feche a aba "PC conectado" e volte à aba do painel.

**Deve aparecer, na seção Nuvem:** "Conectado como **voce@teste.local**.", "Na nuvem, este PC aparece
como {o nome do seu PC}.", o botão **Desconectar** e, embaixo, "Desconectar tira o acesso deste PC,
aqui e na nuvem." Se ainda mostrar o código, clique em **Histórico** e em **Configurações** de novo.

Confira o token no Gerenciador de Credenciais (o comando só lê):

```powershell
cmdkey /list:AuroraWhisper/nuvem-portatil
```

**Deve aparecer:** uma credencial com esse nome (tipo genérico). E o nome do Agent instalado continua
sem nada:

```powershell
cmdkey /list:AuroraWhisper/nuvem
```

**Deve aparecer:** nenhuma credencial (`NONE` ou `NENHUM`).

## 8. Ver o PC em Aparelhos e revogar (no app da nuvem)

1. Numa aba, abra `http://localhost:8080` (você já está com a conta de teste) e clique em
   **Configurações**.
2. **Deve aparecer**, entre **Conta** e **Sessões abertas**, a seção **Aparelhos**, com o seu PC numa
   linha: o nome e, embaixo, "Windows · versão 0.1.0-sprint2 · último uso …", e o botão **Revogar**.
3. Clique em **Revogar**. Na própria linha: "Revogar {nome}? O PC para de sincronizar e continua
   funcionando nele.", com **Revogar** (em vermelho) e **Cancelar** (o foco está nele; o Esc também
   cancela).
4. Clique em **Revogar**. **Deve aparecer:** o aviso "PC revogado." e "Nenhum PC conectado."

## 9. Ver o PC desconectado (no painel do PC)

Volte à aba do painel. O Agent pergunta à nuvem quando você volta à aba, **no máximo a cada 30
segundos**: se nada mudar, espere meio minuto e clique em **Histórico** e em **Configurações** de
novo.

**Deve aparecer:** "Este PC foi desconectado na nuvem.", "Desligada. Nada vai para a nuvem até você
conectar." e o campo já com `http://localhost:8080`. O Agent continua ditando normalmente: dite uma
frase em qualquer lugar e veja que entra.

```powershell
cmdkey /list:AuroraWhisper/nuvem-portatil
```

**Deve aparecer:** nenhuma credencial: o Agent apagou o token recusado.

## 10. (Opcional) Desconectar pelo PC

1. Conecte de novo (passos 5 a 7, com a mesma nuvem: a mesma conta conecta de novo, e no mesmo
   aparelho).
2. No painel, clique em **Desconectar**. **Deve aparecer:** o aviso "Este PC foi desconectado da
   nuvem." e o campo de volta.
3. No app da nuvem, **Configurações**: "Nenhum PC conectado." (o Desconectar do PC revoga na nuvem).

## 11. Limpar e voltar ao Agent instalado

1. Se o painel ainda mostrar "Conectado", clique em **Desconectar**.
2. Confira que o token de teste saiu:

```powershell
cmdkey /list:AuroraWhisper/nuvem-portatil
```

   Se ainda listar, apague (só esse nome):

```powershell
cmdkey /delete:AuroraWhisper/nuvem-portatil
```

3. No app da nuvem, **Configurações** → **Sair**. Depois, na janela 1, aperte **Ctrl+C** para
   desligar a nuvem de teste.
4. Feche o Agent do `bin\` e confira:

```powershell
taskkill /IM aurora-whisper-agent.exe
```

```powershell
Get-Process aurora-whisper-agent -ErrorAction SilentlyContinue
```

5. Abra o Agent instalado pelo **Menu Iniciar** (Aurora Whisper).

O que **fica** de propósito: o `bin\historico.db` guarda o endereço e a conta da nuvem de teste
(não o token). Na próxima vez, com o mesmo endereço, o PC conecta de novo à conta de teste (ela tem
sempre o mesmo número); com outro endereço, o PC recusa (o aviso 2).

## O que me contar

- Se o código do painel e o da página foram os mesmos.
- Se a aba voltou sozinha ao `127.0.0.1` com "PC conectado", e se o Chrome perguntou algo sobre rede
  local (passo 6).
- Se o painel mostrou "Conectado" e, depois de revogar, "Este PC foi desconectado na nuvem.", e
  quanto tempo levou.
- O que os `cmdkey` mostraram nos passos 7, 9 e 11.
- Qualquer frase diferente das deste roteiro, com um print.

## O que este roteiro não testa

O login do Google (F11), o celular, o envio de ditados (F7) e uma nuvem em `https` de verdade (a Local
Network Access do Chrome numa nuvem pública: F11).
