# Política de privacidade: a nuvem do Aurora Whisper (modelo)

> **Modelo para quem hospeda a própria nuvem** (ADR-019, F11b; revisado com o dossiê de LGPD,
> `docs/lgpd-dossie.md`; **reescrito em 10-out-2026 com o ditado no celular, ADR-021, D0**). O projeto
> Aurora Whisper não tem servidor central: cada nuvem é de quem a sobe, e quem a sobe responde pelos
> dados dela. O texto descreve o que o código faz. Antes de pôr na sua nuvem outra pessoa além de você
> (`CONTAS_PERMITIDAS`), copie este modelo, preencha os campos **[A PREENCHER]** (o responsável, o
> contato, onde ficam a VPS e a cópia de segurança, a condição do art. 33) e confira se o resto vale
> para a sua instalação (os prazos, o servidor compartilhado, o provedor da cópia). Não é parecer
> jurídico.
>
> **O ditado no celular** (as partes marcadas "ditado no celular") só vale se a sua nuvem o tiver
> ligado: o servidor só o monta com a chave de cifra (`AI_KEYS_SECRET_FILE`). Se você não vai ligá-lo,
> tire essas partes. **Ele ainda não existe no código**: este texto descreve o que a ADR-021 promete e
> só passa a valer quando existir. Antes do primeiro áudio real na sua nuvem, o responsável lê e
> aprova estas partes e o dossiê. O que é da sua instância (o responsável, o contato, as regiões, a
> condição do art. 33) fica nos campos **[A PREENCHER]**, e a cópia preenchida fica fora do repositório.

Este texto é para quem usa o Aurora Whisper. Está em português simples de propósito. Ele só trata da
**nuvem**, que é opcional: o Aurora Whisper funciona no seu PC sem ela.

## 1. O resumo

- **Sem conectar a nuvem, nada sai do seu PC.** O áudio **do PC** nunca sai dele, conectando a nuvem ou
  não.
- **Se você conectar**, o texto dos seus ditados passa a ficar num servidor, **legível para quem
  administra esse servidor**.
- **Ditado no celular (opcional, só depois de um aceite seu):** a **voz** de cada ditado vai ao servidor
  desta nuvem e, dele, a um provedor de transcrição que **você** escolhe (**OpenAI** ou **Groq**), com a
  **sua chave de API**. A voz fica só na memória, durante o pedido, e é **descartada ao fim**: nada é
  gravado. O provedor, porém, pode guardar por conta própria (seções 6 e 7), e nenhum dos dois diz
  processar no Brasil: tratamos o envio como transferência internacional. A sua chave fica no
  servidor, **cifrada**, mas **quem administra o servidor consegue decifrá-la**.
- O texto fica **6 meses**, a lixeira **30 dias**. Uma cópia de segurança, cifrada, guarda tudo por
  mais **cerca de 30 dias**.
- Você pode **apagar tudo da nuvem** quando quiser, **desconectar** o PC a qualquer hora e **desligar o
  ditado no celular**, que revoga o seu aceite e apaga as suas chaves.
- A nuvem **não transcreve nem analisa** o que você dita no PC. Só guarda e sincroniza. No ditado do
  celular ela só repassa a voz ao provedor que você escolheu, corrige os termos com o que você ensinou
  (o Smart) e, se você ligar, chama o Polish.

## 2. Quem cuida dos seus dados

O responsável pela nuvem (o "controlador", art. 5º, VI da Lei 13.709/2018) é
**[A PREENCHER: nome do responsável]**. Para qualquer pedido ou dúvida sobre privacidade:
**[A PREENCHER: e-mail de contato e, se houver, o encarregado]**. O endereço da nuvem é
**[A PREENCHER: o endereço, como `https://aurora-whisper.exemplo.com.br`]**.

O projeto Aurora Whisper não tem servidor central e não recebe nada seu: cada nuvem é de quem a sobe.

No ditado no celular, os provedores (OpenAI, Groq e, no Polish, a Anthropic) tratam o que você manda
**com o contrato da sua própria conta neles**: a chave é sua, e o gasto também. O responsável pela
nuvem não paga nada deles por você.

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

**Se você ligar o ditado no celular, a nuvem guarda também:**

- o **texto** que sai de cada ditado do celular, que vira um ditado no histórico, como os do PC (o
  aparelho de origem fica em branco, e o histórico mostra que veio "pelo app");
- a **sua chave de API** do provedor, **cifrada** (AES-256-GCM); a tela só mostra o fim dela. A chave que
  abre essa cifra é um arquivo do servidor, fora do banco e da cópia de segurança. **Quem tem acesso de
  administrador (a raiz) à VPS lê esse arquivo e o banco, e decifra** (seção 9);
- o seu **aceite** (a data, a versão do aviso e os provedores que você aceitou) e as suas **preferências
  do ditado** (o provedor e o modelo da transcrição, se a dica está ligada, se o Polish está ligado, e o
  provedor e o modelo dele). Isso mora só na nuvem e não desce aos PCs;
- **a voz, não.** Ela fica na memória do servidor durante o pedido e é descartada ao fim. Se o pedido
  cair depois de o provedor responder, o **texto** que seria gravado fica até 10 minutos na memória,
  para o reenvio gravar sem cobrar de novo; ele some se o servidor reiniciar e nunca vai a log.

**O que nunca sai do PC:** o áudio do PC; o que está em outras janelas (o programa em uso, o título, o
texto em volta); a chave do Polish **do PC**; os logs; as preferências do PC; o mapa de domínios. O
app do celular nem enxerga outras janelas.

## 4. Para que usamos

Só para mostrar o seu histórico nos seus aparelhos (o PC e o celular), manter o aprendido igual em todos
e deixar você editar, apagar e restaurar. Não usamos seus ditados para treinar modelos nem para
anúncios, e a nuvem, por si, não os processa com inteligência artificial (no ditado do celular, quem
o faz é o provedor que você escolheu, como está logo abaixo).

**Ditado no celular:** a voz serve **só** para virar o texto do ditado que você pediu; a chave, só para
chamar o provedor que você escolheu, por você; os termos da dica, só para ajudar o provedor a escrever
certo, se você ligar. **A sua voz não é usada para identificar você**: nenhum código a compara com uma
amostra nem a guarda. Quem processa com inteligência artificial nesse caminho é o provedor que você
escolheu e, se você ligar, o provedor do Polish.

## 5. Por que podemos tratar seus dados

Pelo seu **consentimento** (art. 7º, I e art. 8º): a nuvem só liga quando você a conecta; os ditados
novos sobem sozinhos enquanto o PC está conectado, e o histórico antigo só sobe depois que o programa
mostra quantos ditados vão subir e você clica em "Enviar o histórico". Você pode
**revogar** quando quiser, sem custo (art. 8º, §5º): é o "Desconectar" e o "Apagar meus dados da
nuvem". O programa guarda no seu PC o instante do seu clique. Se esta política mudar de um jeito que
mexa no que você aceitou, avisamos com destaque (art. 8º, §6º).

**Ditado no celular: um consentimento à parte.** O envio da voz a um provedor é outra finalidade, e por
isso tem o seu próprio aceite, **específico, em destaque e separado das outras finalidades** (art. 8º,
§4º; para o envio ao exterior, art. 33, VIII; para dado sensível, art. 11, I). Ele aparece no primeiro
toque no microfone, **antes** de o microfone abrir e antes de o app pedir a chave. Sem o aceite, o
microfone nem abre, a chave não é pedida e o servidor recusa o ditado. O aceite fica **gravado no
servidor, por conta**, com a data, a versão do aviso e os provedores que você aceitou, e vale em
qualquer aparelho seu (a prova do consentimento é do responsável: art. 8º, §2º). O servidor pede de novo
quando você **troca de provedor**, liga o Polish com **outro provedor** ou quando o **texto do aviso
muda**; mudar só o modelo não pede. Você revoga a qualquer momento em **Desligar o ditado no celular**.

## 6. Com quem os dados passam

| Quem | O que recebe | Onde fica |
|---|---|---|
| A VPS (hospedagem) | tudo o que está na nuvem, legível; no ditado do celular, a voz (em memória, em trânsito) e as chaves (cifradas) | **[A PREENCHER: país e região da VPS, por exemplo Brasil]** |
| Google (login) | que você entrou. Devolve e-mail, identificador e nome | conforme as condições do Google |
| Backblaze B2 (cópia de segurança) | só o arquivo cifrado, que ele não consegue ler | **[A PREENCHER: região do bucket]** |
| O servidor de entrada (o que atende o https, no mesmo servidor) | o endereço IP de quem acessa e o endereço pedido, só de passagem: o registro de acesso dele fica **desligado** para a nuvem. Ele ainda pode registrar um pedido que chegue com a nuvem fora do ar, ou pelo http sem o "s" (que ele manda para o https). **No ditado do celular, a voz passa por ele**, e ele pode segurar pedaços dela na memória dele | o mesmo lugar da VPS |
| **OpenAI** (ditado no celular), se você a escolher para transcrever | a voz; com a dica ligada, os termos do que você ensinou. Com o Polish: o texto | nos EUA ou na Europa, **se o projeto da sua chave tiver a residência de dados fixada** (uma opção para contas elegíveis; a página manda falar com o comercial da OpenAI para saber); **sem isso, a documentação que lemos não diz em que país**. O Brasil não aparece nela |
| **Groq** (ditado no celular), se você a escolher para transcrever | a voz; com a dica ligada, os termos do que você ensinou | o que ela guarda fica em servidores nos **EUA**; **a documentação não diz onde a transcrição roda**; o contrato dela prevê tratamento nos EUA e em outros países |
| **OpenAI ou Anthropic** (Polish, no PC ou no celular), se você ligar | o texto final (4 palavras ou mais) | a confirmar. No PC a chamada sai do seu PC; no celular, do servidor da nuvem |

Com a VPS no Brasil, o texto legível dos seus ditados fica no Brasil. O que sai do Brasil é a cópia de
segurança, **cifrada** (o Backblaze não consegue lê-la), o seu login, que passa pelo Google, e, **no
ditado do celular, a voz**, que tratamos como saída do Brasil porque nenhum dos dois provedores diz
processá-la aqui. A cópia de segurança fora do Brasil é uma transferência internacional de dados
(art. 5º, XV), e ela só acontece com uma das condições do art. 33. **[A PREENCHER: a condição
escolhida, com a região do bucket.]**

**A voz também é uma transferência internacional, e é de dado legível.** Ela vai protegida pelo https
até o provedor, mas o Aurora Whisper não a cifra de ponta a ponta, e o provedor a trata para transcrever.
A condição proposta é a do art. 33, VIII: o **seu consentimento específico e em destaque**, dado na folha
do aceite, com a informação prévia de que o envio é internacional, separado das outras finalidades.
**[A PREENCHER: o responsável confirma a condição para a voz e para os termos da dica.]**

### O que cada provedor diz

Lido nas páginas públicas dos provedores em **10-out-2026** (as fontes e o que cada uma **não** diz
estão no dossiê, Anexo B). O que está abaixo é o que **eles** dizem. O Aurora Whisper **não consegue
conferir** a sua conta neles (se a retenção zero está ligada, se a residência de dados está fixada), e
os provedores mudam preço, modelo e regras com o tempo.

| | OpenAI | Groq |
|---|---|---|
| Treinar com o que você envia | diz que, desde março de 2023, os dados enviados à API não são usados para treinar nem melhorar os modelos, a menos que você permita | as páginas de dados e de privacidade que lemos **não falam** em treino com dados de clientes; o contrato de tratamento de dados (DPA) limita o uso a prestar o serviço, em nome do cliente e conforme as instruções dele |
| Guardar | por padrão gera registros de monitoramento de abuso, mantidos por **até 30 dias** (ou mais, se a lei exigir ou se for necessário para proteger o serviço ou terceiros), que podem conter o que foi enviado e a resposta. A tabela de endpoints dela mostra "nenhuma" retenção de abuso para a transcrição de áudio, e o texto geral fala em até 30 dias: **por cautela, esta política conta até 30 dias**. Contas aprovadas por ela podem excluir o conteúdo desses registros | por padrão **não retém** os dados dos pedidos; pode registrar o que foi enviado e a resposta por **até 30 dias** (ou mais, se a lei exigir), só para investigar falha de confiabilidade ou abuso, a menos que a conta ligue a retenção zero, que a página diz estar ao alcance de todos os clientes |

**Onde cada um processa** está na coluna "Onde fica" da tabela acima. Se algo aqui ficar diferente do
que o provedor diz hoje, vale o que o provedor diz. As páginas lidas estão citadas no dossiê.

## 7. Por quanto tempo

| O quê | Quanto tempo |
|---|---|
| Ditados na nuvem | 6 meses, contados da data do ditado. O servidor confere de hora em hora |
| Lixeira | 30 dias depois de você mandar para lá (nunca além dos 6 meses do ditado). Se o PC ficou sem internet, a lixeira que chega atrasada fica pelo menos 24 horas |
| Ditado apagado de vez | some na hora. Fica só uma marca (o número do ditado e a data, sem texto) até 6 meses e 24 horas depois da data do ditado, para o ditado não voltar de um PC desligado |
| Trocas aprendidas | enquanto você não as desfaz. **Depois de desfeita, a nuvem guarda a troca (a palavra e a certa), marcada como desfeita, enquanto a sua conta existir** (seção 10) |
| Sessão do navegador | 30 dias sem uso, e no máximo 90 desde o login |
| PC conectado | 180 dias sem uso |
| **A voz do ditado no celular** | **a nuvem não a guarda**: memória durante o pedido, descartada ao fim (no app, ao fim do envio ou quando você descarta). **No provedor:** o que a tabela da seção 6 diz, até 30 dias nos casos que ele cita |
| **Sua chave de API (ditado no celular)** | até você trocar, remover, desligar o ditado ou apagar seus dados. **A cópia antiga, cifrada, fica na cópia de segurança (cerca de 30 dias) e no banco anterior depois de uma restauração (7 dias).** Por isso a tela manda você **revogar a chave antiga no provedor** |
| **Aceite e preferências (ditado no celular)** | enquanto a sua conta existir, ou até o "Apagar meus dados" |
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
- **Apagar meus dados da nuvem**: apaga seus ditados, as marcas, o aprendido, os PCs conectados, as
  outras sessões e, se você ligou o ditado no celular, **o aceite, as preferências e as chaves**. Os
  seus PCs ficam com a cópia deles, e a cópia de segurança guarda o que apagamos até girar (seção 7);
- **Desconectar** um PC, revogar um aparelho, encerrar uma sessão, ou **Sair**;
- **Desligar o ditado no celular**: revoga o seu aceite (art. 8º, §5º) e apaga as suas chaves do
  servidor. Não apaga os ditados já feitos, e o que já foi tratado com o seu aceite fica ratificado
  enquanto você não pedir a eliminação (art. 8º, §5º).

**Quem recebe a sua voz (art. 18, VII):** é a tabela da seção 6. **Eliminar o que o provedor guardou
não é com a nuvem**: o pedido vai ao provedor, pela sua conta nele, e vale o prazo que ele diz (seção 7).

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
e PCs conectados ficam no banco só como um resumo irreversível. **O áudio do PC nunca sai do PC.** Os
registros de funcionamento do servidor não têm texto ditado, e-mail, cookie, endereço IP nem a busca que
você fez, e **nunca têm a voz, a sua chave nem o que o provedor devolve num erro**. O banco não tem porta
aberta para a internet, e a cópia de segurança é cifrada com uma chave que só o responsável tem.

**No ditado do celular:** a voz fica só na memória do servidor, num pedido de cada vez por conta, e é
**descartada** ao fim. O contêiner da API é configurado para não escrever em disco, não usar swap e não
gravar core dump, e o responsável confere isso no servidor antes do primeiro áudio. "Descartada" não é
"apagada da memória": o programa não zera a memória. A sua chave é cifrada (AES-256-GCM) com uma chave
que mora num arquivo do servidor, nunca no banco nem na cópia de segurança, e o app só vê o fim dela. O
servidor só chama o provedor que você escolheu e para o qual você deu o aceite, e não troca de provedor
sozinho: se ele falha, o ditado falha.

**O que isso não impede:** quem administra o servidor lê o texto dos seus ditados, e quem invadisse o
servidor leria o mesmo. **No ditado do celular, quem tem a raiz da VPS também pode ouvir a voz em
trânsito e decifrar as suas chaves**, porque o arquivo da chave de cifra e o banco moram no mesmo
servidor. Por isso: use **uma chave de API só para o celular, com limite de gasto no provedor**, e, se
houver um incidente, **revogue a chave no provedor**. **[A PREENCHER, se for o caso: O servidor é
compartilhado com outros serviços do responsável, atrás da mesma entrada do https, e quem invadisse um
deles poderia chegar até aqui.]** Se acontecer um incidente com risco relevante para você, o
responsável comunica você e a autoridade (art. 48).

## 10. Quatro coisas que valem ser lidas com calma

1. **A troca desfeita fica na nuvem.** Quando você desfaz uma troca aprendida, a nuvem continua
   guardando a palavra e a certa, marcada como desfeita, enquanto a sua conta existir. Sem isso, um PC
   que ainda tem a troca a mandaria de volta, e ela voltaria a todos os aparelhos. Só o "Apagar meus
   dados" a remove. A cópia de segurança também a guarda até girar.
2. **As cópias do banco no seu PC.** Antes de enviar o histórico, antes de fixar o dono do histórico e
   antes de ligar os 6 meses, o programa faz uma cópia do banco, ao lado dele. Ela tem **todo o texto
   ditado até ali, inclusive o da lixeira**. O programa **nunca** as apaga sozinho: elas saem só pelo
   botão **Apagar as cópias** (Configurações, Histórico). Apague depois de conferir o histórico.
3. **O Polish do PC fica fora da nuvem.** Se você ligar o Polish no PC, o texto final (com 4 palavras
   ou mais) vai para a OpenAI ou a Anthropic, com a sua chave, a cada ditado. Elas podem guardá-lo por
   até 30 dias e dizem não usá-lo para treinar modelos. Isso não depende da nuvem e acontece só se você
   ligar. A chave do PC fica no Gerenciador de Credenciais do Windows.
4. **O ditado no celular manda a sua voz a um provedor que não diz processar no Brasil, e a sua chave
   fica no servidor.** Isto é o contrário da regra do PC, e só acontece depois do seu aceite:
   - a **voz** vai ao provedor que você escolheu (OpenAI ou Groq) e pode ficar lá até 30 dias, nos
     casos que ele cita (seção 6). O servidor não confere se a sua conta tem a retenção zero;
   - a **chave** fica cifrada no servidor, mas quem tem a raiz dele a decifra; a antiga, depois de
     "Trocar" ou "Remover", continua cifrada na cópia de segurança por cerca de 30 dias: revogue-a no
     provedor;
   - a **dica** (os termos que você ensinou) começa **desligada**. Ligada, ela vai ao provedor em
     **todo ditado**, mesmo quando o ditado não fala deles, e **pode levar nomes de clientes ou de
     outras pessoas**. Ela também pode fazer o provedor escrever um termo que você não disse;
   - o **Polish no celular** começa desligado; ligado, o servidor manda o texto à OpenAI ou à
     Anthropic, com a sua chave, a cada ditado de 4 palavras ou mais;
   - **sem tela apagada**: o ditado do celular não funciona com a tela apagada nem com o app em segundo
     plano. Se a tela apagar, ele para e envia o que já gravou, se houver fala;
   - **a cobrança é sua**, na sua conta no provedor. "Tentar de novo" não cobra de novo, salvo em três
     casos raros: o prazo cortou uma transcrição que o provedor já tinha cobrado; o servidor reiniciou
     no meio; ou o reenvio veio mais de 10 minutos depois de uma gravação que falhou. São centavos por
     ditado.

## 11. Se for preciso restaurar a cópia de segurança

Restauramos a cópia só num desastre. Nesse caso, **o que você apagou de vez depois da última cópia pode
reaparecer** (se ainda estiver nos 6 meses), e você pode apagar de novo; o que já passou dos 6 meses
a nuvem apaga antes de abrir. O que só existia na nuvem depois da cópia pode se perder. Todos os seus PCs
e sessões são desligados, e cada PC conecta de novo. O banco de antes da restauração fica 7 dias com
outro nome, e depois é apagado.

**Ditado no celular:** o aceite, as preferências e as chaves voltam como estavam na cópia (se você
desligou o ditado ou trocou a chave depois dela, faça de novo). Se a cópia for restaurada **sem** a mesma
chave de cifra, cada chave guardada vira "sem chave": o app pede para você colar de novo, e o servidor
não dá erro.

## 12. Dados de outras pessoas

O que você dita pode falar de outras pessoas (clientes, por exemplo) e, sem querer, trazer dado
sensível, como saúde. A nuvem **não separa nem filtra** nada: esse texto tem os mesmos prazos e o mesmo
acesso que o resto. Se a pessoa de quem você falou pedir os dados dela (art. 18), o pedido vai ao contato
da seção 2. Se você dita dado de terceiros no seu trabalho, veja se isso cabe nas suas obrigações com
eles antes de conectar a nuvem.

**Ditado no celular: a voz e a dica também podem levar dado de outras pessoas.**
- A **voz** leva o que você diz e **o que o microfone captar** (a voz de quem está perto, uma conversa
  ao fundo), e vai a um provedor que não diz processar no Brasil.
- A **dica**, se você a ligar, leva os termos que você ensinou ao Aurora Whisper em todo ditado, mesmo
  nos que não falam deles; **esses termos podem ser nomes de clientes ou de terceiros**.
- O **seu aceite vale por você, e não por essas pessoas.** Se você dita dado de cliente, você pode estar
  como operador desse dado (art. 5º, VII), que deve seguir as instruções de quem o controla (art. 39), e
  mandar a voz ou os termos a um provedor no exterior pode ir além do que o cliente autorizou. **A
  decisão de ditar isso pelo celular é de quem dita.** Na dúvida, não ligue a dica e não dite dado de
  cliente pelo celular até conferir o que você deve a ele.
- Se uma dessas pessoas pedir os dados dela (art. 18), o pedido vai ao contato da seção 2 para o que a
  nuvem guarda; o que o provedor guardou da voz dela é com o provedor.

## 13. Mudanças

Se mudar algo que altere o que você aceitou (a finalidade, a duração, quem trata, com quem
compartilhamos), avisamos com destaque no app antes de valer (art. 8º, §6º; art. 9º, §2º).

**10-out-2026 (ADR-021):** esta política ganhou o ditado no celular, que traz uma finalidade nova (a
voz), um destinatário novo (o provedor que você escolher) e uma transferência internacional da voz.
Quem já usa a nuvem é avisado com destaque, no app, antes de isso valer para a sua conta, **e nada muda
sem o aceite**: sem ele, o microfone nem abre. Trocar de provedor ou mudar o texto do aviso pede o aceite
de novo.

---

# Texto proposto para o app (ainda não aplicado)

Nada abaixo foi aplicado. Quando for, o texto entra no app junto com os testes que conferem as frases
(subfatia C6b da ADR-021). **Duas regras de código:** o build da nuvem não pode trazer a frase
"Gerenciador de Credenciais" (o `bundle.test` a recusa), e a tela não afirma o que não foi medido.

**App da nuvem, seção Privacidade** (`web/src/nuvem/ConfiguracoesNuvem.tsx`, no lugar das duas frases de
hoje):

- Vão para a nuvem: o texto dos ditados, com a data, o PC de origem e os detalhes da transcrição, e as
  trocas aprendidas. O texto fica legível para quem administra o servidor.
- Ficam 6 meses (a lixeira, 30 dias). Uma cópia cifrada guarda tudo por cerca de 30 dias além disso.
- Do PC nunca vão: o áudio, o que está nas outras janelas, a chave do Polish e as configurações dos PCs.
  A chave do PC continua no PC.
- Ditar pelo celular é opcional. Se você ligar, a voz de cada ditado vai ao provedor que você escolher
  (OpenAI ou Groq), com a sua chave, que o servidor guarda cifrada. A voz não é gravada: fica na
  memória e é descartada ao fim.
- Um link para esta política, quando ela estiver publicada.

**Abaixo de "Apagar meus dados da nuvem"**: "Apaga tudo desta conta na nuvem e desconecta os PCs. Os PCs
ficam com a cópia deles. A cópia de segurança guarda por até cerca de 30 dias. Apaga também o aceite do
ditado no celular, as preferências e as chaves. [Seu cadastro (e-mail e nome) fica; para tirá-lo, peça
pelo contato.]" (a frase entre colchetes depende da decisão 2 do dossiê).

**PC, perto de "Enviar o histórico"** (`web/src/NuvemNoPC.tsx`): "Na nuvem, o texto fica legível para quem
administra o servidor, no Brasil." (com a VPS no Brasil; a cópia de segurança cifrada fora do Brasil é
dita na política, junto da condição do art. 33).

## A folha do aceite do ditado no celular (versão 1 do aviso; proposta)

Aparece no primeiro toque no microfone, **antes** de o microfone abrir e de o app pedir a chave. É uma
folha só, em destaque, separada das outras finalidades. Os botões são **Aceito** e **Agora não**. A
identificação da versão do aviso é da C3b: **qualquer mudança neste texto muda a versão e pede o aceite
de novo.** Onde há `{...}`, o app mostra o bloco do provedor que a pessoa escolheu.

> **Ao ditar pelo celular, a sua voz vai a um provedor que não diz processar no Brasil.**
>
> Por isso, tratamos o envio como uma transferência internacional. Leia antes de aceitar. Se você tocar
> em "Agora não", o microfone não abre, nada é enviado e o resto do app continua igual.
>
> **O que vai**
> - **A voz de cada ditado.** Ela passa pelo servidor desta nuvem, só na memória, e vai ao provedor que
>   você escolheu: **{OpenAI ou Groq}**. Não é gravada no seu aparelho nem no servidor. O provedor pode
>   guardá-la: veja abaixo.
> - **A sua chave de API.** Você a cola uma vez. O servidor a guarda cifrada, mas **quem administra o
>   servidor consegue decifrá-la**. Use uma chave só para o celular, com limite de gasto no provedor.
> - **Se você ligar a dica:** os termos que você ensinou ao Aurora Whisper, junto com a voz, em todo
>   ditado. Eles podem ser nomes de clientes ou de outras pessoas. A dica começa desligada.
> - **Se você ligar o Polish:** o texto do ditado, para **{OpenAI ou Anthropic}**, com a sua chave. O
>   Polish começa desligado.
>
> **Para onde, e por quanto tempo fica lá**
> {Bloco do provedor, abaixo.}
>
> **Dados de outras pessoas.** A voz e os termos podem ter dados de outras pessoas (clientes, quem está
> perto de você). O seu aceite vale por você, e não por elas. Se você dita dado de cliente, veja se
> isso cabe no que você deve a ele.
>
> **O que isto não faz.** Não funciona com a tela apagada nem com o app em segundo plano: se a tela
> apagar, o ditado para. A sua voz não é usada para identificar você.
>
> **Você pode desfazer.** Em Configurações, "Desligar o ditado no celular" tira o seu aceite e apaga as
> suas chaves do servidor. Revogue também a chave no provedor: uma cópia cifrada dela fica nos backups
> por cerca de 30 dias.
>
> [Aceito]  [Agora não]    Política completa: {link}

**Bloco do provedor, OpenAI:**

> **OpenAI.** Ela diz que não usa o que você envia pela API para treinar os modelos dela, a menos que
> você permita. Guarda registros de monitoramento de abuso, que podem conter a sua voz e a resposta,
> por até 30 dias (a tabela dela diz "nenhuma" para a transcrição de áudio e o texto geral diz até 30
> dias, então contamos até 30; pode ser mais, se a lei exigir), a não ser que a sua conta tenha uma
> exceção que ela aprovou. Ela
> processa nos EUA ou na Europa **se a sua conta fixou a residência de dados**; se não, a documentação
> dela não diz em que país. O que está aqui é o que a OpenAI diz (lido em 10-out-2026). O Aurora
> Whisper não confere a sua conta.

**Bloco do provedor, Groq:**

> **Groq.** Ela diz que, por padrão, não retém o que você envia; pode registrar a voz e o texto por até
> 30 dias, só para investigar falha ou abuso, a menos que a sua conta ligue a retenção zero. O que ela
> retém fica em servidores nos EUA; a documentação dela não diz onde a transcrição roda, e o contrato
> dela prevê tratamento nos EUA e em outros países. As páginas que lemos não falam em treinar com os
> seus dados. O que está aqui é o que a Groq diz (lido em 10-out-2026). O Aurora Whisper não confere a
> sua conta.

**Bloco do Polish:** o da OpenAI, acima, se for ela; se for a Anthropic: "A Anthropic pode guardar o texto
por até 30 dias e diz não usá-lo para treinar modelos." (a fonte é a pesquisa de 29-set-2026; a página
dela **não foi relida** na D0).

## Configurações do celular: "Ditar pelo celular"

- **O estado:** "Desligado" ou "Ligado desde 10/10/2026", com **Desligar** e um link que reabre a folha.
- **Ao colar a chave:** "Use uma chave só para o celular, com limite de gasto no provedor."
- **Depois de salva:** "Chave terminada em WXYZ." Com **Trocar** e **Remover**, que mostram: "Revogue
  também esta chave no provedor. Uma cópia cifrada dela fica nos backups por cerca de 30 dias."
- **A dica:** "Mandar os termos aprendidos como dica. Eles vão ao provedor junto com a voz, em todo
  ditado, e podem ser nomes de clientes ou de outras pessoas."
- **O Polish:** "Organizar o texto com o Polish. O texto do ditado vai ao provedor do Polish." (reabre a
  folha se o provedor for outro).
- **Desligar:** "Isto tira o seu aceite e apaga as suas chaves do servidor. Não apaga os ditados já
  feitos. Revogue as chaves no provedor."
- **Na primeira vez:** "No celular, o ditado não funciona com a tela apagada nem com o app em segundo
  plano."
