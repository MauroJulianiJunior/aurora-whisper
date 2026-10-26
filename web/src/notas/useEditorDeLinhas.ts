import { useCallback, useMemo, useRef, useState } from 'react'
import {
  type Foco,
  type Linha,
  type TipoDaLinha,
  apagarMarcados,
  converter,
  desmarcarTudo,
  escreverCorpo,
  garantirUma,
  lerCorpo,
  normalizarMarcados,
} from './corpo'

// PedidoDeFoco é onde o foco vai no próximo render: a linha, a posição do
// cursor no texto dela, ou a caixa de marcar (depois de marcar, o foco
// acompanha a linha que desceu).
export interface PedidoDeFoco extends Foco {
  alvo?: 'texto' | 'caixa'
}

const nenhuma: ReadonlySet<number> = new Set()

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
  // Shift. Só o prefixo: o texto fica. (Separar texto corrido em itens, com
  // IA, é a N5.)
  const formatar = (tipo: TipoDaLinha) => {
    const alvo = alvoDoFormatar()
    const primeira = linhas.find((l) => alvo.has(l.chave))
    aplicar(converter(linhas, alvo, tipo), primeira && { chave: primeira.chave, pos: primeira.texto.length })
  }

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
    desmarcarTudo: () => aplicar(desmarcarTudo(linhas)),
    apagarMarcados: () => aplicar(apagarMarcados(linhas)),
  }
}

export type EditorDeLinhasEstado = ReturnType<typeof useEditorDeLinhas>
