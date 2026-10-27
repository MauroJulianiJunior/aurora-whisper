// O que a N8 acrescentou ao corpo da nota (corpo.ts): o que o celular faz sem
// rede (marcar, desmarcar, acrescentar item), o "+ Item" e o ditado do
// microfone de dentro da nota, que entra no cursor.

import { describe, expect, it } from 'vitest'
import { acrescentarItem, escreverCorpo, inserirDitado, lerCorpo, soMarcouOuAcrescentou, temListaDeMarcar } from './corpo'

describe('soMarcouOuAcrescentou (sem rede, E4)', () => {
  const base = 'Mercado de sábado\n- [ ] leite\n- [ ] pão\n- [x] ovos'
  it.each([
    ['marcar um item (ele desce)', 'Mercado de sábado\n- [ ] pão\n- [x] ovos\n- [x] leite'],
    ['desmarcar um item', 'Mercado de sábado\n- [ ] leite\n- [ ] pão\n- [ ] ovos'],
    ['acrescentar item', 'Mercado de sábado\n- [ ] leite\n- [ ] pão\n- [ ] queijo\n- [x] ovos'],
    ['acrescentar e marcar', 'Mercado de sábado\n- [ ] pão\n- [x] ovos\n- [x] leite\n- [ ] manteiga'],
    ['nada mudou', base],
  ])('vale: %s', (_, depois) => {
    expect(soMarcouOuAcrescentou(base, depois)).toBe(true)
  })

  it.each([
    ['mudar o texto de um item', 'Mercado de sábado\n- [ ] leite integral\n- [ ] pão\n- [x] ovos'],
    ['tirar um item', 'Mercado de sábado\n- [ ] leite\n- [x] ovos'],
    ['mudar o parágrafo', 'Mercado do sábado\n- [ ] leite\n- [ ] pão\n- [x] ovos'],
    ['acrescentar um parágrafo', 'Mercado de sábado\n- [ ] leite\n- [ ] pão\n- [x] ovos\nobs'],
    ['o item virar marcador', 'Mercado de sábado\n- leite\n- [ ] pão\n- [x] ovos'],
  ])('não vale: %s', (_, depois) => {
    expect(soMarcouOuAcrescentou(base, depois)).toBe(false)
  })

  it('o item repetido conta: tirar um dos dois "leite" não vale', () => {
    expect(soMarcouOuAcrescentou('- [ ] leite\n- [ ] leite', '- [x] leite')).toBe(false)
    expect(soMarcouOuAcrescentou('- [ ] leite\n- [ ] leite', '- [x] leite\n- [ ] leite')).toBe(true)
  })

  it('temListaDeMarcar', () => {
    expect(temListaDeMarcar(base)).toBe(true)
    expect(temListaDeMarcar('- leite\ntexto')).toBe(false)
  })
})

describe('acrescentarItem (o "+ Item")', () => {
  it('entra depois do último aberto, antes dos marcados, com o cursor nele', () => {
    const linhas = lerCorpo('- [ ] leite\n- [ ] pão\n- [x] ovos')
    const r = acrescentarItem(linhas)
    expect(r.linhas.map((l) => [l.tipo, l.texto, l.feito])).toEqual([
      ['tarefa', 'leite', false],
      ['tarefa', 'pão', false],
      ['tarefa', '', false],
      ['tarefa', 'ovos', true],
    ])
    expect(r.foco).toEqual({ chave: r.linhas[2]!.chave, pos: 0 })
  })

  it('com todos marcados, entra antes do primeiro marcado', () => {
    const r = acrescentarItem(lerCorpo('Título\n- [x] a\n- [x] b'))
    expect(r.linhas.map((l) => l.texto)).toEqual(['Título', '', 'a', 'b'])
  })
})

describe('inserirDitado (o microfone de dentro da nota)', () => {
  it('no meio de um parágrafo, com o espaço antes', () => {
    const linhas = lerCorpo('Ligar para a Fernanda')
    const r = inserirDitado(linhas, linhas[0]!.chave, 'Ligar para a Fernanda'.length, 'antes das cinco.')
    expect(escreverCorpo(r.linhas)).toBe('Ligar para a Fernanda antes das cinco.')
  })

  it('numa linha de lista, a enumeração vira itens (a regra do PC)', () => {
    const linhas = lerCorpo('- [ ] leite\n- [ ] ')
    const r = inserirDitado(linhas, linhas[1]!.chave, 0, 'queijo, presunto e pão')
    expect(escreverCorpo(r.linhas)).toBe('- [ ] leite\n- [ ] Queijo\n- [ ] Presunto\n- [ ] Pão')
  })

  it('a lista do Polish em linhas lê os prefixos', () => {
    const linhas = lerCorpo('Compras')
    const r = inserirDitado(linhas, linhas[0]!.chave, 'Compras'.length, '\n- arroz\n- feijão')
    expect(escreverCorpo(r.linhas)).toBe('Compras\n- arroz\n- feijão')
  })

  it('sem a linha (ela saiu no meio), o texto vai para o fim da última', () => {
    const r = inserirDitado(lerCorpo('a\nb'), 999, 0, 'c')
    expect(escreverCorpo(r.linhas)).toBe('a\nb c')
  })
})
