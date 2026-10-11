// O ambiente do Painel: o que muda entre o modo local e o da nuvem (ADR-019,
// decisão 12: o mesmo código, dois builds). O Painel não importa nada de um
// modo só: recebe daqui o cliente do histórico, a tela de Configurações, a
// faixa do topo, os textos que dependem de onde ele roda e, no modo local, o
// aprendizado. Quem monta o ambiente é a entrada de cada build: App.tsx
// (ambienteLocal.tsx) no local, nuvem/AppNuvem.tsx no da nuvem.

import type { ComponentType, ReactNode } from 'react'
import type { Aprendida, ClienteDoHistorico, Ditado, EscolhaDoSelo, Problema, Sugestao } from './api'

// PropsDasConfiguracoes é o que o Painel passa à tela de Configurações do
// modo. A tela fica montada depois de aberta, e só se esconde (visivel).
export interface PropsDasConfiguracoes {
  visivel: boolean
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
  // abrirConfiguracoes: a falha se resolve nas Configurações (a chave, o
  // aceite).
  abrirConfiguracoes(): void
  avisar(texto: ReactNode): void
}

export interface Ambiente {
  cliente: ClienteDoHistorico
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
  // ditar é o microfone do celular (ADR-021, C6a): o botão ao lado da busca, a
  // faixa da onda e o envio. Só a nuvem o tem, e o código do gravador só entra
  // no build dela (bundle.test.ts). Um componente por ambiente: o gravador, e o
  // áudio que ele guarda até o envio, ficam entre um render e outro.
  ditar?: ComponentType<PropsDoDitar>
}
