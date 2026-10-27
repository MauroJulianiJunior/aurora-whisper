import { describe, expect, it } from 'vitest'
import { aplicarOrdem, dispor, fraseNaPonta, frasePosicao, indiceDoTeclado, moverNaLista, vagaMaisPerto, velocidadeDaRolagem, vizinhasDe } from './ordem'

// A parte pura da ordem manual das notas (ADR-022, N2b): a grade em ordem de
// leitura e o que se faz com a lista quando uma nota muda de lugar.

describe('dispor: a grade em ordem de leitura', () => {
  // Cada caso: as alturas, quantas colunas e a coluna que cada cartão recebe.
  const casos: { nome: string; alturas: number[]; colunas: number; colunaDe: number[] }[] = [
    { nome: 'alturas iguais caem em rodízio', alturas: [10, 10, 10, 10, 10, 10, 10], colunas: 3, colunaDe: [0, 1, 2, 0, 1, 2, 0] },
    { nome: 'o cartão seguinte vai para a coluna mais curta', alturas: [100, 40, 40, 40, 40], colunas: 3, colunaDe: [0, 1, 2, 1, 2] },
    { nome: 'no empate, a coluna da esquerda', alturas: [50, 50, 100, 10, 10], colunas: 2, colunaDe: [0, 1, 0, 1, 1] },
    { nome: 'uma coluna só empilha tudo', alturas: [30, 60, 20], colunas: 1, colunaDe: [0, 0, 0] },
    { nome: 'mais colunas do que cartões', alturas: [10, 20], colunas: 6, colunaDe: [0, 1] },
    { nome: 'o vão entra na conta (a coluna 0 termina em 10+12=22)', alturas: [10, 15, 5], colunas: 2, colunaDe: [0, 1, 0] },
    { nome: 'sem medida (0) vale a estimada: rodízio', alturas: [0, 0, 0, 0, 0], colunas: 2, colunaDe: [0, 1, 0, 1, 0] },
    { nome: 'lista vazia', alturas: [], colunas: 3, colunaDe: [] },
  ]
  for (const c of casos) {
    it(c.nome, () => {
      expect(dispor(c.alturas, c.colunas, 12).coluna).toEqual(c.colunaDe)
    })
  }

  it('o y de cada cartão é o fundo da coluna mais o vão, e a altura é a da coluna mais comprida', () => {
    const d = dispor([100, 40, 40, 40], 2, 10)
    // col 0: 100; col 1: 40, 40+... (coluna 1 recebe 40, 40, 40: 0, 50, 100)
    expect(d.coluna).toEqual([0, 1, 1, 1])
    expect(d.y).toEqual([0, 0, 50, 100])
    // a coluna 1 termina em 100 + 40 = 140; a 0, em 100.
    expect(d.altura).toBe(140)
  })

  it('a ordem de leitura vale: o 2º cartão está à direita do 1º, e não embaixo', () => {
    const d = dispor([300, 300, 300, 300], 2, 12)
    expect(d.coluna.slice(0, 2)).toEqual([0, 1])
    expect(d.y.slice(0, 2)).toEqual([0, 0])
  })
})

describe('vagaMaisPerto: onde o cartão arrastado cai', () => {
  const g = { colunas: 3, larguraDaColuna: 100, vao: 10 }
  // Três cartões iguais de 50 (altura): 4 vagas possíveis.
  const restantes = [50, 50, 50]

  it('perto do topo à esquerda é a primeira vaga', () => {
    expect(vagaMaisPerto(restantes, 50, g, { x: 50, y: 25 })).toBe(0)
  })

  it('no meio da primeira linha, entre os cartões', () => {
    expect(vagaMaisPerto(restantes, 50, g, { x: 160, y: 25 })).toBe(1)
    expect(vagaMaisPerto(restantes, 50, g, { x: 270, y: 25 })).toBe(2)
  })

  it('embaixo de tudo é a última vaga (a quarta, que abre uma segunda linha)', () => {
    expect(vagaMaisPerto(restantes, 50, g, { x: 50, y: 90 })).toBe(3)
  })

  it('numa coluna só (a lista), vale a altura do ponteiro', () => {
    const lista = { colunas: 1, larguraDaColuna: 300, vao: 6 }
    expect(vagaMaisPerto([40, 40, 40], 40, lista, { x: 150, y: 5 })).toBe(0)
    expect(vagaMaisPerto([40, 40, 40], 40, lista, { x: 150, y: 70 })).toBe(1)
    expect(vagaMaisPerto([40, 40, 40], 40, lista, { x: 150, y: 400 })).toBe(3)
    expect(vagaMaisPerto([40, 40, 40], 40, lista, { x: 150, y: -50 })).toBe(0)
  })

  it('com um cartão só, a única vaga é a 0', () => {
    expect(vagaMaisPerto([], 50, g, { x: 500, y: 500 })).toBe(0)
  })

  it('não depende de onde a vaga estava (sem vaivém): a mesma posição dá sempre a mesma vaga', () => {
    const a = vagaMaisPerto([30, 80, 50, 20, 60], 45, g, { x: 120, y: 70 })
    for (let i = 0; i < 5; i++) expect(vagaMaisPerto([30, 80, 50, 20, 60], 45, g, { x: 120, y: 70 })).toBe(a)
  })
})

describe('a lista depois de mover', () => {
  const ids = ['a', 'b', 'c', 'd']

  it('moverNaLista põe o id na posição pedida, contada sem ele', () => {
    expect(moverNaLista(ids, 'a', 0)).toEqual(['a', 'b', 'c', 'd'])
    expect(moverNaLista(ids, 'a', 2)).toEqual(['b', 'c', 'a', 'd'])
    expect(moverNaLista(ids, 'a', 3)).toEqual(['b', 'c', 'd', 'a'])
    expect(moverNaLista(ids, 'd', 0)).toEqual(['d', 'a', 'b', 'c'])
    expect(moverNaLista(ids, 'c', 1)).toEqual(['a', 'c', 'b', 'd'])
  })

  it('posição fora do intervalo vai para a ponta', () => {
    expect(moverNaLista(ids, 'b', -4)).toEqual(['b', 'a', 'c', 'd'])
    expect(moverNaLista(ids, 'b', 99)).toEqual(['a', 'c', 'd', 'b'])
  })

  it('vizinhasDe: as vizinhas depois de soltar, null na ponta', () => {
    expect(vizinhasDe(['b', 'a', 'c'], 'a')).toEqual({ antes: 'b', depois: 'c' })
    expect(vizinhasDe(['a', 'b', 'c'], 'a')).toEqual({ antes: null, depois: 'b' })
    expect(vizinhasDe(['a', 'b', 'c'], 'c')).toEqual({ antes: 'b', depois: null })
    expect(vizinhasDe(['a'], 'a')).toEqual({ antes: null, depois: null })
  })

  it('aplicarOrdem refaz só os lugares do grupo e deixa o resto onde está', () => {
    // A lista tem as fixadas (f1, f2) e as outras (o1, o2, o3) juntas.
    const itens = [{ id: 'f1' }, { id: 'o1' }, { id: 'f2' }, { id: 'o2' }, { id: 'o3' }]
    expect(aplicarOrdem(itens, ['o3', 'o1', 'o2']).map((x) => x.id)).toEqual(['f1', 'o3', 'f2', 'o1', 'o2'])
    expect(aplicarOrdem(itens, ['f2', 'f1']).map((x) => x.id)).toEqual(['f2', 'o1', 'f1', 'o2', 'o3'])
  })
})

describe('o teclado', () => {
  it('lista e detalhes (uma coluna): ↑ e ↓ andam uma vaga, ← e → não valem', () => {
    expect(indiceDoTeclado('ArrowUp', 2, 5, 1)).toBe(1)
    expect(indiceDoTeclado('ArrowDown', 2, 5, 1)).toBe(3)
    expect(indiceDoTeclado('ArrowLeft', 2, 5, 1)).toBeNull()
    expect(indiceDoTeclado('ArrowRight', 2, 5, 1)).toBeNull()
  })

  it('grade: ← e → andam uma vaga; ↑ e ↓ andam uma linha (as colunas)', () => {
    expect(indiceDoTeclado('ArrowLeft', 4, 9, 3)).toBe(3)
    expect(indiceDoTeclado('ArrowRight', 4, 9, 3)).toBe(5)
    expect(indiceDoTeclado('ArrowUp', 4, 9, 3)).toBe(1)
    expect(indiceDoTeclado('ArrowDown', 4, 9, 3)).toBe(7)
  })

  it('nas pontas fica onde está', () => {
    expect(indiceDoTeclado('ArrowUp', 0, 5, 1)).toBe(0)
    expect(indiceDoTeclado('ArrowDown', 4, 5, 1)).toBe(4)
    expect(indiceDoTeclado('ArrowDown', 7, 9, 3)).toBe(8)
    expect(indiceDoTeclado('ArrowUp', 1, 9, 3)).toBe(0)
  })

  it('as frases do leitor de tela', () => {
    expect(frasePosicao(3, 8)).toBe('Movida para a posição 3 de 8.')
    expect(fraseNaPonta(1, 8)).toBe('Já está na posição 1 de 8.')
  })
})

describe('a rolagem perto da borda', () => {
  it('no meio da área, nada', () => {
    expect(velocidadeDaRolagem(400, 0, 800)).toBe(0)
  })
  it('perto do topo sobe, perto da base desce, e mais rápido quanto mais perto da borda', () => {
    expect(velocidadeDaRolagem(10, 0, 800)).toBeLessThan(0)
    expect(velocidadeDaRolagem(790, 0, 800)).toBeGreaterThan(0)
    expect(Math.abs(velocidadeDaRolagem(0, 0, 800))).toBeGreaterThan(Math.abs(velocidadeDaRolagem(60, 0, 800)))
  })
  it('passar da borda não passa da velocidade máxima', () => {
    expect(velocidadeDaRolagem(-500, 0, 800)).toBe(-18)
    expect(velocidadeDaRolagem(2000, 0, 800)).toBe(18)
  })
})
