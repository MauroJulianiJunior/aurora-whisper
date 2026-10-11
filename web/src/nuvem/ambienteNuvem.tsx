// O ambiente do Painel no app da nuvem (ADR-019, F4): o cliente da /api/v1,
// as Configurações da conta, a faixa de "Sem conexão" e a sessão que acaba:
// sem texto não salvo na tela, ela troca a tela inteira; com ele, o Painel
// fica, e a faixa da sessão leva a pessoa a entrar de novo (EntrarDeNovo.tsx).
// Sem o Aprender: aprender é só no PC (ADR-017). Com o microfone do ditado
// (ADR-021, ditar/): só a nuvem o tem, e o código dele só entra neste build.

import type { Ambiente } from '../ambiente'
import type { ClienteNuvem, Eu } from './cliente'
import { ConfiguracoesNuvem } from './ConfiguracoesNuvem'
import { DitarNaNuvem } from './ditar/DitarNaNuvem'
import { Folha } from './ditar/folha'
import type { Plataforma } from './ditar/gravador'
import { criarPorta } from './ditar/porta'
import type { Reentrada } from './entrar'
import { FaixaDaSessao } from './EntrarDeNovo'
import { FaixaNuvem } from './Telas'

interface Ganchos {
  // aoSemSessao: um 401 no meio do uso, sem texto não salvo na tela.
  aoSemSessao(): void
  // aoSair: o Sair das Configurações deu certo.
  aoSair(): void
  // reentrada: a faixa da sessão que acabou com um texto não salvo na tela.
  reentrada: Reentrada
  // plataformaDeDitar: o microfone e o resto do navegador que o gravador usa
  // (ADR-021). Sem ela, o do navegador de verdade; os testes e as telas de
  // desenvolvimento põem um dublê.
  plataformaDeDitar?: Plataforma
  // recarregar: o app é lido de novo quando o servidor recusa o aceite por ele
  // ter mostrado um aviso desatualizado (a folha, folha.ts). Sem ele, a página
  // recarrega.
  recarregar?: () => void
}

// cliente: o do Painel, atrás do portão da reentrada (cliente.ts, Portao).
export function ambienteNuvem(cliente: ClienteNuvem, eu: Eu, { aoSemSessao, aoSair, reentrada, plataformaDeDitar, recarregar }: Ganchos): Ambiente {
  // O Sair é da sessão, não do histórico: passa por fora do portão. Com o
  // portão fechado (a faixa da sessão na tela), ele voltaria 401 sem sair da
  // página, e o Sair contaria como "já saiu" sem o servidor saber: o texto
  // sumiria, e uma sessão que voltou em outra aba continuaria válida.
  const clienteDasConfiguracoes: ClienteNuvem = { ...cliente, sair: () => reentrada.cliente.sair() }
  // A folha do aceite e a porta do microfone (ADR-021, decisão H): um par por
  // sessão, que o gravador e as Configurações dividem. A porta lembra que a
  // conta está pronta; as Configurações a avisam quando algo mudou.
  const folha = new Folha(cliente, recarregar)
  const porta = criarPorta(cliente, folha)
  return {
    cliente,
    // Um componente por ambiente (um por sessão): o estado da tela fica
    // entre um render e outro.
    Configuracoes: (props) => (
      <ConfiguracoesNuvem cliente={clienteDasConfiguracoes} eu={eu} aoSair={aoSair} folha={folha} aoMudarDitado={porta.invalidar} {...props} />
    ),
    Faixa: (props) =>
      props.problema === 'sem-sessao' ? (
        <FaixaDaSessao
          reentrada={reentrada}
          textoNaoSalvo={props.textoNaoSalvo ?? ''}
          ditadoNaoEnviado={props.ditadoNaoEnviado ?? false}
          aoEntrarDeNovo={props.aoEntrarDeNovo ?? props.aoTentarDeNovo}
        />
      ) : (
        <FaixaNuvem {...props} />
      ),
    // O gravador do ditado (ADR-021): fala pelo cliente do Painel, atrás do
    // portão, como o resto, e só abre o microfone depois da porta do aceite.
    // Um componente por sessão: o áudio que espera "Tentar de novo" fica entre
    // um render e outro.
    ditar: (props) => <DitarNaNuvem cliente={cliente} plataforma={plataformaDeDitar} folha={folha} porta={porta} {...props} />,
    aoSemSessao,
    excluirDeVez: (id) => cliente.excluirDeVez(id),
    resolverConflito: (id, versao, escolha) => cliente.resolverConflito(id, versao, escolha),
    textos: {
      // "conectado": depois do "apagar meus dados", os PCs ficam
      // desconectados, e o que se dita neles não chega aqui.
      vazio: 'O que você ditar num PC conectado aparece aqui.',
      outroLugar: 'em outro lugar',
      semResposta: 'sem conexão',
      edicaoSemConexao: 'Sem conexão: a edição não foi salva.',
    },
  }
}
