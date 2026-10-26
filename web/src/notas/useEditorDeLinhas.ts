import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react'
import {
  type Foco,
  type Linha,
  type TipoDaLinha,
  apagarMarcados,
  converter,
  desmarcarTudo,
  ehLista,
  escreverCorpo,
  garantirUma,
  lerCorpo,
  normalizarMarcados,
  separarEmItens,
  trocarPorItens,
} from './corpo'
import type { ComoSeparou, ItensSeparados } from './tipos'

// PedidoDeFoco é onde o foco vai no próximo render: a linha, a posição do
// cursor no texto dela, ou a caixa de marcar (depois de marcar, o foco
// acompanha a linha que desceu).
export interface PedidoDeFoco extends Foco {
  alvo?: 'texto' | 'caixa'
}

const nenhuma: ReadonlySet<number> = new Set()

// Separacao é a última linha de texto corrido que virou itens (N5): o que a
// tela mostra embaixo dela ("separado com IA, as mesmas palavras · Desfazer") e
// o que o Desfazer devolve. Vale só enquanto as linhas são as que a separação
// deixou (depois): qualquer outra mudança a apaga, e o Desfazer nunca leva junto
// uma edição feita depois.
export interface Separacao {
  como: Exclude<ComoSeparou, 'nenhum'>
  // ultima: a chave do último item, embaixo do qual a nota aparece.
  ultima: number
  // primeira: a chave da linha que foi separada (o primeiro item a mantém).
  primeira: number
  antes: Linha[]
  depois: Linha[]
}

// DesfechoDaSeparacao é o que formatarComItens conta a quem chamou, para o
// aviso: separou; não achou itens (a linha só trocou de tipo); a linha mudou
// enquanto o Agent respondia (nada foi trocado); ou o pedido falhou.
export type DesfechoDaSeparacao =
  | { tipo: 'separou'; como: Exclude<ComoSeparou, 'nenhum'> }
  | { tipo: 'sem-itens' }
  | { tipo: 'mudou' }
  | { tipo: 'erro'; erro: unknown }
  | { tipo: 'prefixo' }

// vaiAoAgent diz se a linha é texto corrido que o Agent pode separar: uma
// linha só, que não é título, com duas palavras ou mais. O resto (linhas já
// uma por item, título, linha vazia) só troca o prefixo, sem IA.
const vaiAoAgent = (l: Linha) => l.tipo !== 'titulo' && l.texto.trim().split(/\s+/).length >= 2

// useEditorDeLinhas guarda o que a nota aberta e o "Criar uma nota" dividem: as
// linhas do corpo, a linha onde o cursor está, as linhas escolhidas com Shift
// (para o Formatar valer para várias), se a seção dos marcados está recolhida
// e o pedido de foco que o EditorDeLinhas cumpre depois de cada operação. A
// barra de ferramentas de quem usa o editor chama as operações daqui.
export function useEditorDeLinhas(corpoInicial: string) {
  const [linhas, setLinhas] = useState<Linha[]>(() => garantirUma(normalizarMarcados(lerCorpo(corpoInicial))))
  // atual: a chave da linha onde o cursor esteve por último. Fica quando o
  // foco sai (o clique no Formatar tira o foco da linha, e o Formatar precisa
  // saber qual era).
  const [atual, setAtual] = useState<number | null>(null)
  const [selecao, setSelecao] = useState<ReadonlySet<number>>(nenhuma)
  const [recolhido, setRecolhido] = useState(false)
  // separando: a chave da linha que está no Agent, sendo separada em itens.
  const [separando, setSeparando] = useState<number | null>(null)
  const [separacao, setSeparacao] = useState<Separacao | null>(null)
  // agora: as linhas do último render, para a resposta do Agent (que chega
  // depois) conferir se a linha ainda é a mesma.
  const agora = useRef(linhas)
  useLayoutEffect(() => {
    agora.current = linhas
  })
  const pedido = useRef<PedidoDeFoco | null>(null)
  // ancora: a linha onde a escolha com Shift começou.
  const ancora = useRef<number | null>(null)

  // aplicar troca as linhas e diz onde o foco fica. Toda operação passa por
  // aqui; a escolha de linhas acaba junto.
  const aplicar = useCallback((novas: Linha[], foco?: PedidoDeFoco) => {
    pedido.current = foco ?? null
    setLinhas(garantirUma(novas))
    setSelecao(nenhuma)
  }, [])

  // consumirPedido entrega o pedido de foco pendente, uma vez só.
  const consumirPedido = useCallback((): PedidoDeFoco | null => {
    const p = pedido.current
    pedido.current = null
    return p
  }, [])
  const lerAncora = useCallback(() => ancora.current, [])
  const fixarAncora = useCallback((chave: number | null) => {
    ancora.current = chave
  }, [])

  const escolher = useCallback((chaves: ReadonlySet<number>) => setSelecao(chaves.size > 0 ? chaves : nenhuma), [])

  const alvoDoFormatar = (): ReadonlySet<number> => {
    if (selecao.size > 0) return selecao
    const chave = atual !== null && linhas.some((l) => l.chave === atual) ? atual : linhas[linhas.length - 1]!.chave
    return new Set([chave])
  }

  // formatar troca o prefixo da linha do cursor, ou das linhas escolhidas com
  // Shift. Só o prefixo: o texto fica.
  const formatar = (tipo: TipoDaLinha) => {
    const alvo = alvoDoFormatar()
    const primeira = linhas.find((l) => alvo.has(l.chave))
    aplicar(converter(linhas, alvo, tipo), primeira && { chave: primeira.chave, pos: primeira.texto.length })
  }

  // formatarComItens é o Formatar para Lista, Lista de marcar e Numerada (N5;
  // ADR-022, "Formatar: converter trecho"). Numa linha só de texto corrido, o
  // Agent separa os itens (com a IA da pessoa, ou pela regra) e a linha vira um
  // item por linha, com a nota e o Desfazer embaixo. Nas linhas que já são uma
  // por item (várias escolhidas, título, linha curta), só o prefixo muda, como
  // no formatar. A espera nunca trava a digitação: se a linha mudou enquanto o
  // Agent respondia, nada é trocado. Sem itens, a linha só ganha o tipo, e o
  // texto fica.
  const formatarComItens = async (tipo: TipoDaLinha, separar: (trecho: string) => Promise<ItensSeparados>): Promise<DesfechoDaSeparacao> => {
    const alvo = alvoDoFormatar()
    const linha = alvo.size === 1 ? linhas.find((l) => alvo.has(l.chave)) : undefined
    if (!ehLista(tipo) || !linha || !vaiAoAgent(linha) || separando !== null) {
      formatar(tipo)
      return { tipo: 'prefixo' }
    }
    const texto = linha.texto
    setSeparando(linha.chave)
    let r: ItensSeparados
    try {
      r = await separar(texto.trim())
    } catch (erro) {
      setSeparando(null)
      return { tipo: 'erro', erro }
    }
    setSeparando(null)
    const atuais = agora.current
    const ainda = atuais.find((l) => l.chave === linha.chave)
    if (!ainda || ainda.texto !== texto) return { tipo: 'mudou' }
    const feito = r.como === 'nenhum' ? null : trocarPorItens(atuais, linha.chave, r.itens, tipo)
    if (!feito || r.como === 'nenhum') {
      // Sem itens: a linha só ganha o tipo pedido, e o texto fica.
      aplicar(converter(atuais, new Set([linha.chave]), tipo), { chave: linha.chave, pos: texto.length })
      return { tipo: 'sem-itens' }
    }
    const depois = garantirUma(feito.linhas)
    aplicar(depois, feito.foco)
    setSeparacao({ como: r.como, ultima: feito.foco.chave, primeira: linha.chave, antes: atuais, depois })
    return { tipo: 'separou', como: r.como }
  }

  // desfazerSeparacao devolve as linhas de antes da separação, exatamente como
  // estavam, com o cursor no fim da linha que foi separada.
  const desfazerSeparacao = () => {
    if (!separacao || separacao.depois !== linhas) return
    const volta = separacao.antes.find((l) => l.chave === separacao.primeira)
    aplicar(separacao.antes, volta && { chave: volta.chave, pos: volta.texto.length })
    setSeparacao(null)
  }

  // separar troca a linha da chave pelos itens da enumeração dela (o ditado
  // "arroz, feijão e batata" numa linha de lista, ou o botão "Virar lista"
  // num parágrafo). Lê as linhas de agora, não as do render que pediu: o ditado
  // que acabou de chegar já está nelas. Sem enumeração, não muda nada.
  const separar = useCallback((chave: number, minimo: number, tipo?: TipoDaLinha) => {
    setLinhas((agora) => {
      const r = separarEmItens(agora, chave, minimo, tipo)
      if (!r) return agora
      pedido.current = r.foco
      return garantirUma(r.linhas)
    })
    setSelecao(nenhuma)
  }, [])

  const corpo = useMemo(() => escreverCorpo(linhas, { limpar: true }), [linhas])
  const marcados = linhas.filter((l) => l.tipo === 'tarefa' && l.feito).length

  return {
    linhas,
    corpo,
    marcados,
    atual,
    setAtual,
    selecao,
    escolher,
    lerAncora,
    fixarAncora,
    recolhido,
    alternarRecolhido: () => setRecolhido((r) => !r),
    consumirPedido,
    aplicar,
    formatar,
    formatarComItens,
    separando,
    // A nota da separação só vale enquanto as linhas são as que ela deixou.
    separacao: separacao && separacao.depois === linhas ? separacao : null,
    desfazerSeparacao,
    separar,
    desmarcarTudo: () => aplicar(desmarcarTudo(linhas)),
    apagarMarcados: () => aplicar(apagarMarcados(linhas)),
  }
}

export type EditorDeLinhasEstado = ReturnType<typeof useEditorDeLinhas>
