# Sprint 2 — Roteiro de teste manual

Continuação do `sprint-1-teste-manual.md`, que segue válido como regressão. Este cobre o que o
Sprint 2 acrescentou: **inserção em camadas**, **preservação do clipboard** e **bloqueio em campo
de senha**.

Tempo estimado: 20 minutos.

---

## O que mudou desde o roteiro anterior

### ✅ O campo de senha agora é protegido

O aviso que abria o roteiro do Sprint 1 — *"não teste dentro de um campo de senha"* — **não vale
mais**. O bloqueio existe, e testá-lo é um dos objetivos aqui.

A política é falhar para o lado seguro em dois pontos: o cache de foco **começa protegido**, antes
de qualquer janela ser observada, e qualquer inspeção que falhe, dê timeout ou seja ambígua
resulta em protegido. O custo desse erro é um ditado que não acontece; o custo do erro oposto
seria voz capturada num campo de senha.

### O clipboard agora é usado — e restaurado

A inserção passou a ter dois caminhos: `SendInput` para texto curto (até **25 unidades UTF-16**)
e **clipboard + Ctrl+V** para texto longo.

Como o texto do stub tem ~78 caracteres, **todo ditado hoje passa pelo clipboard**. Isso é
conveniente: o caminho novo é exatamente o que precisa ser exercitado. O `SendInput` já foi
validado no Sprint 1, quando era o único caminho.

**Consequência prática:** o Agent vai mexer no seu clipboard a cada ditado. Ele preserva e
restaura o conteúdo anterior — e o teste **T14** existe justamente para verificar isso.

---

## Como subir

```bash
C:/Desenvolvimento/aurora-whisper/bin/aurora-whisper-agent.exe
```

`Ctrl+C` encerra e devolve a tecla F ao normal. Enquanto roda: toque rápido em F digita `f`,
segurar começa a gravar.

---

## Roteiro

### T12 — Campo de senha bloqueia o ditado ⚠️ o teste principal

**Use uma página de login qualquer no Chrome. Não digite senha de verdade** — nenhuma credencial
é necessária para este teste, e o ditado deve estar bloqueado justamente para que nada seja
capturado.

**Faça:** clique no campo de **senha**. Depois:

1. toque F rapidamente
2. segure F por uns 2 segundos

**Esperado:**
- o toque curto digita `f` normalmente no campo — **a tecla não pode sumir**
- segurar mostra o overlay com *"Ditado desativado em campo protegido"*
- **nenhuma gravação começa**: sem onda, sem contador de duração, sem beep

**Falhou se:** o `f` não aparece no toque curto (você ficaria sem conseguir digitar essa letra em
campos de senha), ou se o overlay entra em "Gravando".

> Este é o teste que justifica o sprint inteiro. Até ontem o ditado funcionava dentro de campo de
> senha.

### T13 — Campo normal continua funcionando

**Faça:** clique no campo de **usuário** (mesma página) e segure F por 2 segundos, falando.
**Esperado:** grava e insere normalmente. O bloqueio é do campo de senha, não da página.
**Falhou se:** o campo comum também for tratado como protegido — a proteção estaria ampla demais
e o produto ficaria inutilizável em páginas de login.

### T14 — O clipboard é preservado ⚠️ o segundo mais importante

**Faça, nesta ordem exata:**

1. abra o Bloco de Notas e digite `MARCADOR-ORIGINAL`
2. selecione esse texto e copie com `Ctrl+C`
3. clique numa linha vazia, segure F, fale, solte — espere o texto do stub ser inserido
4. dê `Enter` e pressione `Ctrl+V`

**Esperado:** o `Ctrl+V` cola **`MARCADOR-ORIGINAL`** — o que você tinha copiado antes.

**Falhou se:** colar o texto do stub. Isso significa que a restauração não aconteceu, ou aconteceu
cedo demais, e **você perdeu o que tinha na área de transferência**. É o tipo de defeito que
ninguém associa ao app de ditado.

### T15 — Matriz de aplicativos

Repita um ditado completo (segurar F, falar, soltar) em cada um:

| Aplicativo | Onde |
|---|---|
| Bloco de Notas | qualquer linha |
| Chrome | caixa de busca do Google, ou um `textarea` |
| WhatsApp Web | campo de mensagem — **não envie**, só digite |
| VS Code | um arquivo de texto aberto |

**Esperado:** o texto do stub aparece nos quatro.
**Falhou se:** falhar em algum. Anote **em qual** e **o que aconteceu** — nada foi inserido? veio
truncado? veio duplicado? A resposta muda o diagnóstico.

> O WhatsApp Web é o mais interessante da lista: é um editor rico dentro do navegador, e é onde
> inserção sintética costuma se comportar diferente.

### T16 — A digitação continua intacta (regressão do T9)

**Faça:** digite rápido `refrigerador fofoca afinal difícil fácil`.
**Esperado:** tudo sai certo, sem atraso nem letra faltando.
**Falhou se:** qualquer perda ou lentidão.

> O Sprint 2 acrescentou um segundo hook de sistema (`SetWinEventHook` para rastrear o foco). Este
> teste confirma que ele não onerou o caminho do teclado. Passou no Sprint 1; tem que continuar
> passando.

### T17 — Encerramento limpo

**Faça:** `Ctrl+C` no terminal. Depois segure F por 2 segundos no Bloco de Notas.
**Esperado:** o Agent encerra sem erro, e segurar F volta a digitar `fffff...`.

---

## Medições — os dois números que eu não pude obter sozinho

Estes valores foram escolhidos por julgamento e marcados `[ASSUMPTION]` no código, porque medi-los
exigiria injetar entrada e colar em aplicativos reais — exatamente o que a regra de segurança
proíbe aos agentes. Só dá para obtê-los com você na máquina.

### M1 — A espera de 120 ms antes de restaurar o clipboard

O **T14** já é a medição: se ele passar nos quatro aplicativos do **T15**, 120 ms é suficiente
para todos eles.

O que o teste manual **não** consegue determinar é a margem — se o valor mínimo seguro é 40 ms ou
110 ms. Para isso seria preciso variar o valor e repetir, o que não vale o esforço agora. O que
interessa hoje é saber se é suficiente.

**Reporte:** o T14 passou em todos os apps do T15, ou falhou em algum?

### M2 — O limiar de 25 unidades entre `SendInput` e clipboard

**Não é medível nesta fase.** O texto do stub é fixo e tem ~78 caracteres, então todo ditado toma
o caminho do clipboard. Para exercitar o `SendInput` seria preciso um texto curto, e isso só
acontece quando o ASR real chegar (Sprint 3) e você ditar uma palavra só.

Fica registrado como pendente, a ser medido quando houver transcrição de verdade. O que dá para
dizer hoje: o caminho `SendInput` funcionou para textos dessa ordem no Sprint 1, quando era o
único.

---

## O que me reportar

Para cada teste de **T12** a **T17**: passou ou falhou, e o que você viu nos que falharem.

Para o **T15**, mesmo passando, me diga se notou diferença de comportamento entre os aplicativos —
lentidão, piscada, texto aparecendo em pedaços. Isso informa o ajuste do limiar depois.

E as últimas linhas do terminal, principalmente se houver `WARN` ou `ERROR`.

---

## O que este roteiro ainda NÃO prova

- **Qualidade de transcrição** — o ASR continua stub; o texto inserido é fixo (Sprint 3).
- **O limiar do M2** — ver acima.
- **Perda de texto no pior caso** — se os dois caminhos de inserção falharem *e* a escrita no
  clipboard também falhar, o texto se perde. Está registrado no `ADR-011` como limitação
  estrutural: sem persistência de histórico (`TASK-005`), não há onde guardar.
- **Uso prolongado** — vazamento de handle COM ou de memória só aparece em horas de uso. O
  Sprint 2 acrescentou objetos COM da UI Automation a cada troca de foco, então isso passa a
  importar mais do que antes.

---

## Resultado da execução

**17 de setembro de 2026** — executado por Mauro na máquina de referência.

| Teste | Resultado |
|---|---|
| **T12** campo de senha bloqueia | ✅ passou |
| **T13** campo normal continua funcionando | ✅ passou |
| **T14** clipboard preservado | ✅ passou, após correção |
| T15 matriz de aplicativos | pendente |
| T16 digitação intacta (regressão do T9) | pendente |
| T17 encerramento limpo | pendente |

### O que esta rodada estabeleceu

**O `AC-036` existe de verdade.** O overlay mostrou "Ditado desativado em campo protegido",
nenhum áudio foi capturado — sem beep, sem onda — e o toque curto continuou digitando a letra.
Os dois lados do critério: bloqueia o ditado, preserva a tecla.

**A proteção não é ampla demais.** Dois ditados num campo de texto comum, logo em seguida,
deram `outcome=success`. Se a política "na dúvida, proteger" estivesse errando para o lado
largo, ela bloquearia campos normais e o produto seria inutilizável em qualquer página de
login.

### Três defeitos encontrados aqui, não pela suíte

Os três só apareceram porque uma pessoa rodou o programa numa máquina real:

1. **O Agent não subia.** O microfone reporta PCM de 32 bits inteiro, e o código só aceitava
   float32 e PCM de 16. O `ADR-010` dizia "tipicamente float32", e a implementação leu
   "tipicamente" como "sempre".
2. **O clipboard nunca era restaurado.** O Histórico da Área de Transferência do Windows
   reescreve o clipboard após cada escrita, então a comparação por número de sequência sempre
   acusava mudança externa e pulava o restore — em silêncio, com o log dizendo "pulado" como se
   fosse prudência. A decisão passou a ser por conteúdo.
3. **A mensagem do overlay aparecia cortada:** "Ditado desativado em campo prot…". E o teste que
   deveria cobrir isso não olhava um único label — só conferia uma fórmula contra si mesma, com
   um comentário que pré-justificava a lacuna.

Nenhum dos três seria pego por mais testes automatizados do mesmo tipo. O primeiro dependia do
hardware, o segundo de um serviço do Windows rodando, e o terceiro de alguém **olhando a tela**.
