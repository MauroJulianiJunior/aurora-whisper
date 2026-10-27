import type { ReactNode } from 'react'
import type { Nota } from './tipos'
import { type ConfigDaOrdem, type PropsDoItem, useOrdemManual } from './useOrdemManual'

// As duas formas de ver que são uma pilha de cartões com ordem manual (ADR-022,
// "Ordem manual (N2b)"): a grade, em colunas na ordem de leitura, e a lista, uma
// coluna. Cada seção (as fixadas, as outras) é uma coleção, com o gesto dela: o
// cartão de uma seção nunca cai na outra, porque a vaga só existe dentro da sua.

interface Props {
  notas: Nota[]
  config: ConfigDaOrdem
  // renderizar desenha a nota e põe `ordem` no elemento dela.
  renderizar(nota: Nota, ordem: PropsDoItem): ReactNode
}

// Os vãos entre os cartões, os mesmos do CSS de antes (column-gap e a margem de
// baixo do cartão: 12 px; a lista: 6 px).
const VAO_DA_GRADE = 12
const VAO_DA_LISTA = 6

export function GradeDeNotas({ notas, colunas, config, renderizar }: Props & { colunas: number }) {
  const { montarContainer, estiloDoContainer, propsDoItem, vaga, arrastando, pronto } = useOrdemManual({ ids: notas.map((n) => n.id), colunas, vao: VAO_DA_GRADE, modo: 'absoluto', ...config })
  return (
    <div
      className="grade ordenada"
      data-colunas={colunas}
      data-pronto={pronto ? '' : undefined}
      data-em-arrasto={arrastando ? '' : undefined}
      ref={montarContainer}
      style={estiloDoContainer}
    >
      {notas.map((n) => renderizar(n, propsDoItem(n.id)))}
      {vaga && <Vaga vaga={vaga} />}
    </div>
  )
}

export function ListaDeNotas({ notas, config, renderizar }: Props) {
  const { montarContainer, estiloDoContainer, propsDoItem, vaga, arrastando, pronto } = useOrdemManual({ ids: notas.map((n) => n.id), colunas: 1, vao: VAO_DA_LISTA, modo: 'absoluto', ...config })
  return (
    <ul
      className="notas-lista ordenada"
      data-pronto={pronto ? '' : undefined}
      data-em-arrasto={arrastando ? '' : undefined}
      ref={montarContainer}
      style={estiloDoContainer}
    >
      {notas.map((n) => renderizar(n, propsDoItem(n.id)))}
      {vaga && <Vaga vaga={vaga} lista />}
    </ul>
  )
}

// Vaga é o espaço aberto onde o cartão cairia: um contorno tracejado do tamanho
// dele.
function Vaga({ vaga, lista }: { vaga: NonNullable<ReturnType<typeof useOrdemManual>['vaga']>; lista?: boolean }) {
  const estilo = {
    width: vaga.largura,
    height: vaga.altura,
    transform: `translate3d(${vaga.x}px, ${vaga.y}px, 0)`,
  }
  // Na lista o filho de <ul> é <li>; a vaga não é item de lista para o leitor de tela.
  return lista ? <li className="vaga" aria-hidden="true" role="presentation" style={estilo} /> : <div className="vaga" aria-hidden="true" style={estilo} />
}
