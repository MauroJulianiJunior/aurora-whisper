# Política de privacidade: a nuvem do Aurora Whisper (modelo)

> **Modelo para quem hospeda a própria nuvem** (ADR-019, F11b; revisado com o dossiê de LGPD,
> `docs/lgpd-dossie.md`). O projeto Aurora Whisper não tem servidor central: cada nuvem é de quem a
> sobe, e quem a sobe responde pelos dados dela. O texto descreve o que o código faz. Antes de pôr na
> sua nuvem outra pessoa além de você (`CONTAS_PERMITIDAS`), copie este modelo, preencha os campos
> **[A PREENCHER]** (o responsável, o contato, onde ficam a VPS e a cópia de segurança, a condição do
> art. 33) e confira se o resto vale para a sua instalação (os prazos, o servidor compartilhado, o
> provedor da cópia). Não é parecer jurídico.

Este texto é para quem usa o Aurora Whisper. Está em português simples de propósito. Ele só trata da
**nuvem**, que é opcional: o Aurora Whisper funciona no seu PC sem ela.

## 1. O resumo

- **Sem conectar a nuvem, nada sai do seu PC.** O áudio não sai do PC nesta versão, conectando ou não.
- **Se você conectar**, o texto dos seus ditados passa a ficar num servidor, **legível para quem
  administra esse servidor**.
- O texto fica **6 meses**, a lixeira **30 dias**. Uma cópia de segurança, cifrada, guarda tudo por
  mais **cerca de 30 dias**.
- Você pode **apagar tudo da nuvem** quando quiser, e **desconectar** o PC a qualquer hora.
- A nuvem **não transcreve nem analisa** o que você dita. Só guarda e sincroniza.

## 2. Quem cuida dos seus dados

O responsável pela nuvem (o "controlador", art. 5º, VI da Lei 13.709/2018) é
**[A PREENCHER: nome do responsável]**. Para qualquer pedido ou dúvida sobre privacidade:
**[A PREENCHER: e-mail de contato e, se houver, o encarregado]**. O endereço da nuvem é
**[A PREENCHER: o endereço, como `https://aurora-whisper.exemplo.com.br`]**.

O projeto Aurora Whisper não tem servidor central e não recebe nada seu: cada nuvem é de quem a sobe.

## 3. O que a gente guarda

**No seu PC (com ou sem nuvem):** o histórico dos ditados, as preferências e o dicionário, em arquivos
na pasta de dados do programa. Eles não são cifrados pelo Aurora Whisper: quem entra na sua conta do
Windows os alcança.

**Na nuvem (só se você conectar):**

- de cada ditado: o texto que o programa entendeu e o texto final, a data, o modo (Literal, Smart ou
  Polish), se deu certo, se está na lixeira, e detalhes da transcrição (modelo, idioma, duração,
  confiança);
- as trocas que o programa aprendeu com você (a palavra que ele escrevia errado, a certa, a restrição e
  o nome do domínio);
- o nome do seu computador, o sistema, a versão do programa e um número aleatório da instalação;
- do seu login Google: o **e-mail, o identificador da conta e o nome**. Não guardamos a foto nem a
  senha;
- a sua sessão no navegador (um cookie, `__Host-sessao`, necessário para você ficar logado) e os PCs
  conectados. No servidor eles ficam só como um resumo irreversível, que não serve para entrar no seu
  lugar.

**O que nunca sai do PC:** o áudio; o que está em outras janelas (o programa em uso, o título, o texto
em volta); a chave do Polish; os logs; as preferências; o mapa de domínios.

## 4. Para que usamos

Só para mostrar o seu histórico nos seus aparelhos (o PC e o celular), manter o aprendido igual em todos
e deixar você editar, apagar e restaurar. Não usamos seus ditados para treinar modelos nem para
anúncios, e a nuvem não os processa com inteligência artificial.

## 5. Por que podemos tratar seus dados

Pelo seu **consentimento** (art. 7º, I e art. 8º): a nuvem só liga quando você a conecta; os ditados
novos sobem sozinhos enquanto o PC está conectado, e o histórico antigo só sobe depois que o programa
mostra quantos ditados vão subir e você clica em "Enviar o histórico". Você pode
**revogar** quando quiser, sem custo (art. 8º, §5º): é o "Desconectar" e o "Apagar meus dados da
nuvem". O programa guarda no seu PC o instante do seu clique. Se esta política mudar de um jeito que
mexa no que você aceitou, avisamos com destaque (art. 8º, §6º).

## 6. Com quem os dados passam

| Quem | O que recebe | Onde fica |
|---|---|---|
| A VPS (hospedagem) | tudo o que está na nuvem, legível | **[A PREENCHER: país e região da VPS, por exemplo Brasil]** |
| Google (login) | que você entrou. Devolve e-mail, identificador e nome | conforme as condições do Google |
| Backblaze B2 (cópia de segurança) | só o arquivo cifrado, que ele não consegue ler | **[A PREENCHER: região do bucket]** |
| O servidor de entrada (o que atende o https, no mesmo servidor) | o endereço IP de quem acessa e o endereço pedido, só de passagem: o registro de acesso dele fica **desligado** para a nuvem. Ele ainda pode registrar um pedido que chegue com a nuvem fora do ar, ou pelo http sem o "s" (que ele manda para o https) | o mesmo lugar da VPS |

Com a VPS no Brasil, o texto legível dos seus ditados fica no Brasil. O que sai do Brasil é a cópia de
segurança, **cifrada** (o Backblaze não consegue lê-la), e o seu login, que passa pelo Google. A cópia
de segurança fora do Brasil é uma transferência internacional de dados (art. 5º, XV), e ela só
acontece com uma das condições do art. 33. **[A PREENCHER: a condição escolhida, com a região do
bucket.]**

A OpenAI e a Anthropic só entram no **Polish** (seção 10), que não passa pela nuvem.

## 7. Por quanto tempo

| O quê | Quanto tempo |
|---|---|
| Ditados na nuvem | 6 meses, contados da data do ditado. O servidor confere de hora em hora |
| Lixeira | 30 dias depois de você mandar para lá (nunca além dos 6 meses do ditado). Se o PC ficou sem internet, a lixeira que chega atrasada fica pelo menos 24 horas |
| Ditado apagado de vez | some na hora. Fica só uma marca (o número do ditado e a data, sem texto) até 6 meses e 24 horas depois da data do ditado, para o ditado não voltar de um PC desligado |
| Trocas aprendidas | enquanto você não as desfaz. **Depois de desfeita, a nuvem guarda a troca (a palavra e a certa), marcada como desfeita, enquanto a sua conta existir** (seção 10) |
| Sessão do navegador | 30 dias sem uso, e no máximo 90 desde o login |
| PC conectado | 180 dias sem uso |
| Cópia de segurança | cerca de **30 dias**, mais o tempo que o provedor da cópia leva para apagar o que a regra dele escondeu **[A PREENCHER: conferir no painel do provedor]**. A cópia tem também o que a regra de 6 meses já apagou, até ela girar |
| Cópia cifrada no servidor | 3 dias |
| Banco anterior, depois de uma restauração | 7 dias |
| No seu PC | a lixeira, 30 dias (depois de a nuvem saber dela); os 6 meses só depois do seu OK na tela Histórico; as cópias de segurança do PC não têm prazo (seção 10) |

## 8. Seus direitos, e como exercer

Você tem direito a (art. 18): confirmar que tratamos seus dados, acessá-los, corrigi-los, pedir a
eliminação, pedir cópia, saber com quem compartilhamos e revogar o consentimento. Sem pedir a ninguém,
**no app**:

- **ver e buscar** seu histórico, seus aparelhos e suas sessões, e o que foi aprendido;
- **corrigir** o texto de um ditado;
- **mandar para a lixeira**, **restaurar**, e **excluir definitivamente** (só de um ditado que está na
  lixeira);
- **Apagar meus dados da nuvem**: apaga seus ditados, as marcas, o aprendido, os PCs conectados e as
  outras sessões. Os seus PCs ficam com a cópia deles, e a cópia de segurança guarda o que apagamos até
  girar (seção 7);
- **Desconectar** um PC, revogar um aparelho, encerrar uma sessão, ou **Sair**.

Pelo contato da seção 2, você pede o que o app não faz sozinho, como a declaração completa do que
guardamos de você (respondemos em até 15 dias, art. 19, II) e uma cópia dos seus dados. **Hoje o app
da nuvem não tem botão de exportar**; o seu PC já tem a cópia completa, e pela nossa parte entregamos
uma cópia eletrônica de quem pedir. **[A PREENCHER: o responsável confirma esta promessa.]**

**O cadastro.** O "Apagar meus dados" **não apaga o seu cadastro** (e-mail, identificador do Google e
nome). Para tirá-lo, peça pelo contato. **[A PREENCHER: o responsável confirma como atende esse
pedido.]**

Você também pode reclamar à autoridade de proteção de dados (art. 18, §1º).

## 9. Segurança

O acesso é por https. A nuvem só aceita as contas de uma lista, e quem não está nela não cria nada. Sessões
e PCs conectados ficam no banco só como um resumo irreversível. O áudio nunca sai do PC. Os registros de
funcionamento do servidor não têm texto ditado, e-mail, cookie, endereço IP nem a busca que você fez. O
banco não tem porta aberta para a internet, e a cópia de segurança é cifrada com uma chave que só o
responsável tem.

**O que isso não impede:** quem administra o servidor lê o texto dos seus ditados, e quem invadisse o
servidor leria o mesmo. **[A PREENCHER, se for o caso: O servidor é compartilhado com outros serviços
do responsável, atrás da mesma entrada do https, e quem invadisse um deles poderia chegar até aqui.]**
Se acontecer um incidente com risco relevante para você, o responsável comunica você e a autoridade
(art. 48).

## 10. Três coisas que valem ser lidas com calma

1. **A troca desfeita fica na nuvem.** Quando você desfaz uma troca aprendida, a nuvem continua
   guardando a palavra e a certa, marcada como desfeita, enquanto a sua conta existir. Sem isso, um PC
   que ainda tem a troca a mandaria de volta, e ela voltaria a todos os aparelhos. Só o "Apagar meus
   dados" a remove. A cópia de segurança também a guarda até girar.
2. **As cópias do banco no seu PC.** Antes de enviar o histórico, antes de fixar o dono do histórico e
   antes de ligar os 6 meses, o programa faz uma cópia do banco, ao lado dele. Ela tem **todo o texto
   ditado até ali, inclusive o da lixeira**. O programa **nunca** as apaga sozinho: elas saem só pelo
   botão **Apagar as cópias** (Configurações, Histórico). Apague depois de conferir o histórico.
3. **O Polish fica fora da nuvem.** Se você ligar o Polish, o texto final (com 4 palavras ou mais) vai
   para a OpenAI ou a Anthropic, com a sua chave, a cada ditado. Elas podem guardá-lo por até 30 dias e
   dizem não usá-lo para treinar modelos. Isso não depende da nuvem e acontece só se você ligar. A chave
   fica no Gerenciador de Credenciais do Windows.

## 11. Se for preciso restaurar a cópia de segurança

Restauramos a cópia só num desastre. Nesse caso, **o que você apagou de vez depois da última cópia pode
reaparecer** (se ainda estiver nos 6 meses), e você pode apagar de novo; o que já passou dos 6 meses
a nuvem apaga antes de abrir. O que só existia na nuvem depois da cópia pode se perder. Todos os seus PCs
e sessões são desligados, e cada PC conecta de novo. O banco de antes da restauração fica 7 dias com
outro nome, e depois é apagado.

## 12. Dados de outras pessoas

O que você dita pode falar de outras pessoas (clientes, por exemplo) e, sem querer, trazer dado
sensível, como saúde. A nuvem **não separa nem filtra** nada: esse texto tem os mesmos prazos e o mesmo
acesso que o resto. Se a pessoa de quem você falou pedir os dados dela (art. 18), o pedido vai ao contato
da seção 2. Se você dita dado de terceiros no seu trabalho, veja se isso cabe nas suas obrigações com
eles antes de conectar a nuvem.

## 13. Mudanças

Se mudar algo que altere o que você aceitou (a finalidade, a duração, quem trata, com quem
compartilhamos), avisamos com destaque no app antes de valer (art. 8º, §6º).

---

# Texto proposto para o app (ainda não aplicado)

Nada abaixo foi aplicado. Quando for, o texto entra no app junto com os testes que conferem as frases.

**App da nuvem, seção Privacidade** (`web/src/nuvem/ConfiguracoesNuvem.tsx`, no lugar das duas frases de
hoje):

- Vão para a nuvem: o texto dos ditados, com a data, o PC de origem e os detalhes da transcrição, e as
  trocas aprendidas. O texto fica legível para quem administra o servidor.
- Ficam 6 meses (a lixeira, 30 dias). Uma cópia cifrada guarda tudo por cerca de 30 dias além disso.
- Nunca vão: o áudio, o que está nas outras janelas, a chave do Polish e as configurações dos PCs.
- Um link para esta política, quando ela estiver publicada.

**Abaixo de "Apagar meus dados da nuvem"**: "Apaga tudo desta conta na nuvem e desconecta os PCs. Os PCs
ficam com a cópia deles. A cópia de segurança guarda por até cerca de 30 dias. [Seu cadastro (e-mail e
nome) fica; para tirá-lo, peça pelo contato.]" (a frase entre colchetes depende da decisão 2 do dossiê).

**PC, perto de "Enviar o histórico"** (`web/src/NuvemNoPC.tsx`): "Na nuvem, o texto fica legível para quem
administra o servidor, no Brasil." (com a VPS no Brasil; a cópia de segurança cifrada fora do Brasil é
dita na política, junto da condição do art. 33).
