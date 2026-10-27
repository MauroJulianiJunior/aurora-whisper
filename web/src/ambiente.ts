// O ambiente do Painel: o que muda entre o modo local e o da nuvem (ADR-019,
// decisão 12: o mesmo código, dois builds). O Painel não importa nada de um
// modo só: recebe daqui o cliente do histórico, a tela de Configurações, a
// faixa do topo, os textos que dependem de onde ele roda e, no modo local, o
// aprendizado. Quem monta o ambiente é a entrada de cada build: App.tsx
// (ambienteLocal.tsx) no local, nuvem/AppNuvem.tsx no da nuvem.

import type { ComponentType, ReactNode } from 'react'
import type { Aprendida, ClienteDoHistorico, Ditado, EscolhaDoSelo, Problema, Sugestao } from './api'

// SecaoDasConfiguracoes é uma seção das Configurações que o Painel pode pedir
// que a tela leve à vista: hoje, só a do ditado pelo celular (a nuvem; ADR-021),
// aonde o app leva quem precisa colar a chave do provedor.
export type SecaoDasConfiguracoes = 'ditar'

// PropsDasConfiguracoes é o que o Painel passa à tela de Configurações do
// modo. A tela fica montada depois de aberta, e só se esconde (visivel).
export interface PropsDasConfiguracoes {
  visivel: boolean
  // secao: a seção que o Painel pediu (vez sobe a cada pedido, para o mesmo
  // pedido de novo valer de novo). Só a nuvem a usa; o PC não tem seções
  // pedidas.
  secao?: { nome: SecaoDasConfiguracoes; vez: number } | null
  // aoPerderLigacao: a falha foi da ligação (sem sessão, fora do ar); o
  // Painel decide o que mostrar.
  aoPerderLigacao(e: unknown): void
  avisar(texto: ReactNode): void
  // aprendizado: sobe a cada aprendizado ou desfazer feito pelo aviso do
  // histórico (só no modo local).
  aprendizado: number
  // comTextoNaoSalvo: há um texto que a pessoa escreveu e não salvou na
  // tela; sair da conta o jogaria fora (a nuvem avisa antes). O PC não usa.
  comTextoNaoSalvo?: boolean
  // recarregarHistorico: o histórico mudou por fora da lista (o "apagar
  // meus dados", na nuvem), e a lista lê de novo.
  recarregarHistorico(): void
}

// Aprendiz é o "Aprender" do aviso do histórico (ADR-017): só existe no PC,
// onde estão o motor e o texto bruto. Sem ele, salvar nunca pergunta.
export interface Aprendiz {
  pergunta(sugestoes: Sugestao[]): ReactNode
  // efeito: o que o Aprender faz, dito no title do botão.
  efeito: string
  aprender(id: string, sugestao: Pick<Sugestao, 'de' | 'para'>): Promise<Aprendida>
  desfazer(variante: string): Promise<Aprendida>
  resultadoDeAprender(feitas: Aprendida[], recusas: string[]): ReactNode
  resultadoDeDesfazer(desfeitas: Aprendida[], recusas: string[]): ReactNode
}

export interface TextosDoAmbiente {
  // vazio: a linha embaixo de "Nenhum ditado ainda."
  vazio: string
  // outroLugar: de onde veio a mudança que o painel não fez ("em outra aba"
  // no PC; na nuvem, pode ser o PC, outro aparelho ou outra aba).
  outroLugar: string
  // semResposta: o que a tela de erro da lista diz quando nada respondeu.
  semResposta: string
  // edicaoSemConexao: a nota do editor quando o salvar não chegou a lugar
  // nenhum. Sem ela, só a faixa do topo fala.
  edicaoSemConexao?: string
}

// PropsDaFaixa: o aviso do topo quando a ligação caiu no meio do uso.
export interface PropsDaFaixa {
  problema: Problema
  aoTentarDeNovo(): void
  // A sessão que acabou com um texto não salvo na tela (a nuvem; ADR-019,
  // "Decisões da F4"): textoNaoSalvo é o que a faixa oferece para copiar, e
  // aoEntrarDeNovo, o que ela chama quando a mesma conta volta, sem a página
  // sair do lugar. O PC não usa.
  textoNaoSalvo?: string
  aoEntrarDeNovo?(): void
  // ditadoNaoEnviado: a sessão acabou com um ditado gravado e ainda não
  // enviado (ADR-021: o áudio conta como texto não salvo). Só a nuvem.
  ditadoNaoEnviado?: boolean
}

// PropsDoDitar é o que o Painel passa ao gravador do ditado (ADR-021). O
// gravador mora na nuvem (ambiente.ditar): o Painel só lhe dá o campo de busca
// para pôr ao lado do microfone e recebe de volta o que chegou.
export interface PropsDoDitar {
  // busca: o campo de busca do Painel. O gravador o mostra ao lado do
  // microfone e o troca pela faixa da onda enquanto grava; null nas
  // Configurações, onde não há busca.
  busca: ReactNode
  // escondido: o Painel está nas Configurações. O microfone sai de cena, mas
  // uma gravação em curso, ou um áudio que espera "Tentar de novo", fica.
  escondido: boolean
  // semMicrofone: a aba Notas (ADR-022, N8): a busca fica, e o microfone do
  // lado dela sai (o "Ditar para nota" embaixo toma o lugar dele). Uma gravação
  // em curso, ou um áudio que espera, fica à vista.
  semMicrofone?: boolean
  // aoChegar: o servidor gravou o ditado. nota é o que o app fez sozinho
  // ("Parei quando o app saiu da frente."); aviso, a frase de o Polish não
  // ter agido.
  aoChegar(ditado: Ditado, extra: { nota?: string; aviso?: string }): void
  // aoAudioPendente: passou a haver, ou deixou de haver, um ditado gravado e
  // não enviado, só na memória. Conta como texto não salvo: a sessão que acaba
  // não troca a tela inteira (o áudio se perderia).
  aoAudioPendente(pendente: boolean): void
  // aoSemSessao: o envio levou 401. O áudio fica, e o Painel acende a faixa
  // da sessão.
  aoSemSessao(): void
  // abrirConfiguracoes: a falha se resolve nas Configurações (a chave do
  // provedor). Com secao, o Painel leva a seção à vista.
  abrirConfiguracoes(secao?: SecaoDasConfiguracoes): void
  avisar(texto: ReactNode): void
}

// PropsDoAvisoDoMicrofone é o que o Painel passa ao aviso de microfone
// bloqueado pelo Windows: só o avisar, a pílula de baixo da tela.
export interface PropsDoAvisoDoMicrofone {
  avisar(texto: ReactNode): void
}

// PropsDasNotas é o que o Painel passa à aba Notas (ADR-022, N2; no celular
// desde a N8): o componente vem do ambiente, com o cliente de cada lado, e o Painel
// não importa nada dela.
export interface PropsDasNotas {
  // busca: o campo de busca do Painel (já com a espera da digitação).
  busca: string
  aoPerderLigacao(e: unknown): void
  // avisar: a pílula de baixo da tela; com desfazer, ela oferece o Desfazer;
  // com abrir, o "Abrir" (o aviso do "Ditar para nota" no celular, N8).
  avisar(texto: ReactNode, desfazer?: () => void, abrir?: () => void): void
  // abrirNota: o pedido do "Abrir" do aviso "Virou nota": a nota que a janela
  // abre. vez sobe a cada pedido, para o mesmo pedido de novo valer de novo.
  abrirNota?: { id: string; vez: number } | null
  // oculta: a busca única está à vista (ADR-022, N6). A aba fica montada, com o
  // que a pessoa estava fazendo, mas só a janela da nota aparece, para abrir uma
  // nota achada na busca sem sair dos resultados.
  oculta?: boolean
  // aoTerminarNota: a janela de uma nota fechou (salvou, arquivou, foi para a
  // lixeira). A busca única relê os resultados.
  aoTerminarNota?(): void
  // abrirConfiguracoes leva às Configurações do ditado (o "Ditar para nota" do
  // celular sem a chave do provedor; N8).
  abrirConfiguracoes?(): void
}

// GrupoDaBusca é um dos dois grupos da busca única: o que o Painel sabe dizer
// deles sem saber o que são notas.
export type GrupoDaBusca = 'notas' | 'ditados'

// PropsDosResultadosDaBusca é o que o Painel passa aos resultados da busca única
// (ADR-022, N6): "Ctrl+K procura nas notas e no histórico, em dois grupos".
export interface PropsDosResultadosDaBusca {
  // busca: o texto do campo do topo (já com a espera da digitação).
  busca: string
  // recarga: sobe quando os resultados devem ser lidos de novo (voltar à aba, o
  // sync, uma nota que fechou). O que está na tela fica até a leitura chegar.
  recarga: number
  // aoAbrirNota abre a nota na janela, sobre os resultados.
  aoAbrirNota(id: string): void
  // aoAbrirDitado leva ao histórico, com o ditado aberto no editor.
  aoAbrirDitado(ditado: Ditado): void
  // aoVerTodas leva à aba do grupo, com a mesma busca.
  aoVerTodas(grupo: GrupoDaBusca): void
  // aoLimpar é o Esc nos resultados: a busca acaba e o foco volta ao campo.
  aoLimpar(): void
  // aoVoltarAoCampo é a seta para cima no primeiro resultado.
  aoVoltarAoCampo(): void
  aoPerderLigacao(e: unknown): void
}

// BuscaUnica é a busca do topo que procura nas notas e nos ditados (ADR-022, N6).
// Só o PC a tem (as notas moram no Agent): o texto e os resultados vêm daqui, e o
// Painel, que é o mesmo código dos dois builds, não carrega nada disso no da nuvem
// (bundle.test.ts).
export interface BuscaUnica {
  // placeholder: o texto do campo, e o nome dele para o leitor de tela.
  placeholder: string
  // rotuloDoEscopo: a linha de baixo do campo quando a pessoa foi a um grupo só
  // ("Ver todas"), e rotuloDeVoltar, o botão que traz os dois grupos de volta.
  rotuloDoEscopo: Record<GrupoDaBusca, string>
  rotuloDeVoltar: string
  Resultados: ComponentType<PropsDosResultadosDaBusca>
}

// AcaoVirarNota é a ação "Virar nota" do histórico (ADR-022, N3). Só o PC a tem
// (as notas moram no Agent): o texto e a rota vêm daqui, e o Painel, que é o
// mesmo código dos dois builds, não carrega nada disso no da nuvem.
export interface AcaoVirarNota {
  // rotulo: o texto do botão ("Virar nota").
  rotulo: string
  // virou: o texto do aviso depois de virar ("Virou nota").
  virou: string
  // voltou: o aviso do Desfazer, com a nota apagada; voltouComNota: com a nota
  // mantida porque a pessoa já a tinha editado.
  voltou: string
  voltouComNota: string
  // jaVirou diz se a falha de virar() é "este ditado já virou nota" (outra aba,
  // ou o clique anterior): a lista na tela estava velha.
  jaVirou(e: unknown): boolean
  virar(ditadoId: string): Promise<{ id: string }>
  desfazer(notaId: string): Promise<{ nota_apagada: boolean }>
}

export interface Ambiente {
  cliente: ClienteDoHistorico
  // Notas é a aba Notas (o PC, e o celular desde a N8); sem ela, o Painel não
  // mostra o botão.
  Notas?: ComponentType<PropsDasNotas>
  // virarNota é o "Virar nota" do editor do histórico (ADR-022, N3). Só o PC.
  virarNota?: AcaoVirarNota
  // buscaUnica é a busca nas notas e nos ditados (ADR-022, N6). Só o PC. Sem ela, a
  // busca do topo procura no histórico, como sempre.
  buscaUnica?: BuscaUnica
  // AvisoDoMicrofone é o aviso do topo quando o Windows bloqueia o microfone, com
  // o botão que abre a tela certa. Só o PC o tem (o Agent lê o registro do
  // Windows); é o componente, e não só a consulta, para o texto e as rotas não
  // entrarem no build da nuvem (bundle.test.ts). Sem ele, o Painel não mostra
  // nada.
  AvisoDoMicrofone?: ComponentType<PropsDoAvisoDoMicrofone>
  // Configuracoes é a tela de Configurações do modo. O mesmo componente a
  // vida toda do ambiente: o estado dele (o que a pessoa mudou e não salvou)
  // fica entre um render e outro.
  Configuracoes: ComponentType<PropsDasConfiguracoes>
  Faixa: ComponentType<PropsDaFaixa>
  // aoSemSessao: a sessão acabou no meio do uso (401). Sem ele (no PC), a
  // faixa avisa e a tela fica, com o que a pessoa escreveu. Com ele (na
  // nuvem), o app troca a tela inteira pela "Sessão expirada" — menos com
  // um texto não salvo na tela: aí o Painel nem o chama, fica, e a faixa
  // ('sem-sessao') leva a pessoa a entrar de novo.
  aoSemSessao?(): void
  textos: TextosDoAmbiente
  aprendiz?: Aprendiz
  // excluirDeVez é o "Excluir definitivamente" da Lixeira. Só a nuvem apaga
  // de vez (deixa a lápide); no PC, a lixeira sai em 30 dias, ou depois de a
  // nuvem saber, no máximo até os 6 meses do ditado (ADR-019, F10, D3).
  excluirDeVez?(id: string): Promise<void>
  // resolverConflito é a escolha do selo ("2 versões"). Os dois ambientes o
  // têm (o PC desde a F7c); sem ele, o selo apareceria sem os botões.
  resolverConflito?(id: string, versao: number, escolha: EscolhaDoSelo): Promise<Ditado>
  // mudancasDoSync é o contador de mudanças do sync do PC (ADR-021, S): o
  // Painel o lê a cada poucos segundos enquanto está à vista e, quando o número
  // muda, relê a lista (mudancas.ts). Ler o contador é também o aviso ao Agent
  // de que o painel está aberto, e ele busca a nuvem mais vezes. Só o PC o tem;
  // na nuvem, a lista é relida só ao voltar à aba. A leitura que falha é
  // ignorada; o 404 de um Agent de antes da S faz o Painel parar de perguntar.
  mudancasDoSync?(sinal: AbortSignal): Promise<number>
  // ditar é o microfone do celular (ADR-021, C6a): o botão ao lado da busca, a
  // faixa da onda e o envio. Só a nuvem o tem, e o código do gravador só entra
  // no build dela (bundle.test.ts). Um componente por ambiente: o gravador, e o
  // áudio que ele guarda até o envio, ficam entre um render e outro.
  ditar?: ComponentType<PropsDoDitar>
}
