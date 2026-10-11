// O ambiente do Painel no app da nuvem (ADR-019, F4): o cliente da /api/v1,
// as Configurações da conta, a faixa de "Sem conexão" e a sessão que acaba:
// sem texto não salvo na tela, ela troca a tela inteira; com ele, o Painel
// fica, e a faixa da sessão leva a pessoa a entrar de novo (EntrarDeNovo.tsx).
// Sem o Aprender: aprender é só no PC (ADR-017).

import type { Ambiente } from '../ambiente'
import type { ClienteNuvem, Eu } from './cliente'
import { ConfiguracoesNuvem } from './ConfiguracoesNuvem'
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
}

// cliente: o do Painel, atrás do portão da reentrada (cliente.ts, Portao).
export function ambienteNuvem(cliente: ClienteNuvem, eu: Eu, { aoSemSessao, aoSair, reentrada }: Ganchos): Ambiente {
  // O Sair é da sessão, não do histórico: passa por fora do portão. Com o
  // portão fechado (a faixa da sessão na tela), ele voltaria 401 sem sair da
  // página, e o Sair contaria como "já saiu" sem o servidor saber: o texto
  // sumiria, e uma sessão que voltou em outra aba continuaria válida.
  const clienteDasConfiguracoes: ClienteNuvem = { ...cliente, sair: () => reentrada.cliente.sair() }
  return {
    cliente,
    // Um componente por ambiente (um por sessão): o estado da tela fica
    // entre um render e outro.
    Configuracoes: (props) => <ConfiguracoesNuvem cliente={clienteDasConfiguracoes} eu={eu} aoSair={aoSair} {...props} />,
    Faixa: (props) =>
      props.problema === 'sem-sessao' ? (
        <FaixaDaSessao reentrada={reentrada} textoNaoSalvo={props.textoNaoSalvo ?? ''} aoEntrarDeNovo={props.aoEntrarDeNovo ?? props.aoTentarDeNovo} />
      ) : (
        <FaixaNuvem {...props} />
      ),
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
