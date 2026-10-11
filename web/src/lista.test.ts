import { describe, expect, it } from 'vitest'
import { type Resultado, anexarPagina, anunciar, aposFalha, inserirEmOrdem, mesclarRecarga, sumidosEmEdicao, vizinho } from './lista'
import { ditado } from './testes/servidorFalso'

const em = (hora: number, minuto = 0) => new Date(2026, 8, 28, hora, minuto).toISOString()
const d = (id: string, hora: number, apagado?: number) => ditado({ id, created_at: em(hora), deleted_at: apagado === undefined ? undefined : em(apagado) })
const ids = (itens: { id: string }[]) => itens.map((i) => i.id)
const hoje = new Date(2026, 8, 28, 18)

function resultado(parcial: Partial<Resultado>): Resultado {
  return { chave: 'k1', vista: 'historico', busca: '', hoje, itens: [], anuncio: '', ...parcial }
}

describe('inserirEmOrdem', () => {
  it('volta ao lugar pela data; no empate, id maior primeiro', () => {
    const itens = [d('c', 15), d('b', 13), d('a', 11)]
    expect(ids(inserirEmOrdem(itens, d('x', 14), 'created_at', false))).toEqual(['c', 'x', 'b', 'a'])
    expect(ids(inserirEmOrdem(itens, d('bb', 13), 'created_at', false))).toEqual(['c', 'bb', 'b', 'a'])
  })

  it('depois do último carregado, entra só se não há mais páginas — senão viria de novo com elas', () => {
    const itens = [d('c', 15), d('b', 13)]
    expect(ids(inserirEmOrdem(itens, d('a', 11), 'created_at', false))).toEqual(['c', 'b', 'a'])
    expect(ids(inserirEmOrdem(itens, d('a', 11), 'created_at', true))).toEqual(['c', 'b'])
  })

  it('na lixeira, a ordem é a da exclusão', () => {
    const itens = [d('velho', 9, 17), d('novo', 15, 12)]
    expect(ids(inserirEmOrdem(itens, d('meio', 16, 14), 'deleted_at', false))).toEqual(['velho', 'meio', 'novo'])
  })

  it('não duplica quem já está na lista', () => {
    const itens = [d('c', 15), d('b', 13)]
    expect(ids(inserirEmOrdem(itens, d('b', 13), 'created_at', false))).toEqual(['c', 'b'])
  })
})

describe('vizinho', () => {
  it('a linha de baixo; a de cima quando era a última; nenhuma quando era a única', () => {
    const itens = [d('a', 3), d('b', 2), d('c', 1)]
    expect(vizinho(itens, 'a')).toBe('b')
    expect(vizinho(itens, 'c')).toBe('b')
    expect(vizinho([d('a', 1)], 'a')).toBeNull()
  })
})

describe('anexarPagina', () => {
  it('não repete o que já está na lista', () => {
    expect(ids(anexarPagina([d('a', 3), d('b', 2)], [d('b', 2), d('c', 1)]))).toEqual(['a', 'b', 'c'])
  })
})

describe('mesclarRecarga', () => {
  it('recarga da mesma consulta mantém as páginas já carregadas, com o cursor delas', () => {
    const anterior = resultado({ itens: [d('d', 15), d('c', 14), d('b', 13), d('a', 12)], proximo: 'cur-2' })
    const novo = resultado({ chave: 'k2', itens: [d('e', 16), d('d', 15)], proximo: 'cur-1' })
    const r = mesclarRecarga(anterior, novo)
    expect(ids(r.itens)).toEqual(['e', 'd', 'c', 'b', 'a'])
    expect(r.proximo).toBe('cur-2')
    expect(r.chave).toBe('k2')
  })

  it('consulta diferente: fica só a nova', () => {
    const anterior = resultado({ itens: [d('d', 15), d('c', 14)], proximo: 'x' })
    const novo = resultado({ chave: 'k2', busca: 'zzz', itens: [d('z', 16)], proximo: 'y' })
    expect(mesclarRecarga(anterior, novo)).toBe(novo)
    expect(mesclarRecarga(anterior, novo, new Set(['d']))).toBe(novo)
  })

  it('o que está em edição e não veio na recarga fica, no lugar dele; o resto sai', () => {
    const anterior = resultado({ itens: [d('c', 15), d('b', 14), d('a', 13)] })
    const novo = resultado({ chave: 'k2', itens: [d('c', 15)] })
    const r = mesclarRecarga(anterior, novo, new Set(['b']))
    expect(ids(r.itens)).toEqual(['c', 'b'])
    expect(r.anuncio).toBe('2 ditados no histórico.')
    expect(ids(mesclarRecarga(anterior, novo).itens)).toEqual(['c'])
  })

  it('em edição, era o último e não veio: fica, no fim', () => {
    const anterior = resultado({ itens: [d('c', 15), d('b', 14), d('a', 13)] })
    const novo = resultado({ chave: 'k2', itens: [d('d', 16), d('c', 15), d('b', 14)] })
    expect(ids(mesclarRecarga(anterior, novo, new Set(['a'])).itens)).toEqual(['d', 'c', 'b', 'a'])
  })
})

describe('sumidosEmEdicao', () => {
  it('só quem está em edição, estava na lista e não veio na recarga da mesma consulta', () => {
    const anterior = resultado({ itens: [d('c', 15), d('b', 14), d('a', 13)] })
    const novo = resultado({ chave: 'k2', itens: [d('c', 15)] })
    expect(ids(sumidosEmEdicao(anterior, novo, new Set(['b', 'c', 'x'])))).toEqual(['b'])
    expect(sumidosEmEdicao(anterior, { ...novo, busca: 'zzz' }, new Set(['b']))).toEqual([])
    expect(sumidosEmEdicao(null, novo, new Set(['b']))).toEqual([])
  })
})

describe('aposFalha', () => {
  const lista = resultado({ itens: [d('a', 12)], anuncio: '1 ditado no histórico.' })

  it('falha de ligação nunca troca a lista, nem numa consulta de outra vista', () => {
    const r = aposFalha(lista, { chave: 'k2', vista: 'lixeira', busca: '', hoje }, 'o Agent não respondeu', true)
    expect(r.erro).toBeUndefined()
    expect(ids(r.itens)).toEqual(['a'])
    expect(r.vista).toBe('historico')
    expect(r.chave).toBe('k2')
  })

  it('recarga da mesma consulta que falha por outro motivo também mantém a lista', () => {
    const r = aposFalha(lista, { chave: 'k2', vista: 'historico', busca: '', hoje }, 'erro 500', false)
    expect(ids(r.itens)).toEqual(['a'])
    expect(r.erro).toBeUndefined()
  })

  it('consulta nova que falha, sem ser de ligação, vira a tela de erro', () => {
    const r = aposFalha(lista, { chave: 'k2', vista: 'lixeira', busca: '', hoje }, 'erro 500', false)
    expect(r.erro).toBe('erro 500')
    expect(r.itens).toEqual([])
    expect(r.anuncio).toBe('Não consegui carregar a lixeira.')
  })
})

describe('anunciar', () => {
  it('quantos, onde, e se há mais', () => {
    expect(anunciar(resultado({ itens: [d('a', 1)] }))).toBe('1 ditado no histórico.')
    expect(anunciar(resultado({ itens: [d('a', 1), d('b', 2)], proximo: 'c' }))).toBe('2 ditados no histórico, e há mais antigos.')
    expect(anunciar(resultado({ vista: 'lixeira', itens: [d('a', 1)] }))).toBe('1 ditado na lixeira.')
    expect(anunciar(resultado({ busca: 'fator R', itens: [d('a', 1)] }))).toBe('1 ditado para “fator R”.')
  })

  it('vazio e erro', () => {
    expect(anunciar(resultado({}))).toBe('Nenhum ditado ainda.')
    expect(anunciar(resultado({ vista: 'lixeira' }))).toBe('A lixeira está vazia.')
    expect(anunciar(resultado({ busca: 'x' }))).toBe('Nada encontrado para “x”.')
    expect(anunciar(resultado({ erro: 'erro 500' }))).toBe('Não consegui carregar o histórico.')
  })
})
