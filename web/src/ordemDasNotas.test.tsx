// A ordem manual das notas no painel do PC (ADR-022, "Ordem manual (N2b)"): a
// grade em ordem de leitura, o arrastar com o mouse e com o toque, o teclado
// (Alt + setas), o que fica desligado nos filtros e na busca, e a volta atrás
// quando o Agent recusa. Tudo contra o Agent de mentira (testes/servidorFalso.ts),
// que guarda a ordem como o de verdade e responde ao POST /mover.
//
// O jsdom não tem layout: a altura dos cartões e as caixas (getBoundingClientRect)
// são de mentira aqui (instalarGeometria), calculadas do mesmo `transform` que o
// painel põe em cada cartão. O que se prova é a decisão (para onde o cartão cai, o
// que se pede ao Agent, o que a tela mostra), não os pixels.
//
// Nada aqui mexe no sistema de verdade: os eventos de ponteiro e de teclado são
// disparados no DOM do jsdom (fireEvent), e vibrate e scrollBy são de mentira.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { type Pedido, marcador, nota, servidorFalso } from './testes/servidorFalso'
import type { Nota } from './notas/tipos'

const hora = (h: number) => new Date(2026, 9, 25, h, 0, 0).toISOString()

// Duas fixadas (p1, p2) e seis outras (a..f), da mais nova para a mais velha: é
// também a ordem inicial que o Agent de mentira dá (a regra de antes da ordem manual).
const notasDeTeste = (): Nota[] => [
  nota({ id: 'p1', titulo: 'Primeira fixada', corpo: 'p1', fixada: true, updated_at: hora(20) }),
  nota({ id: 'p2', titulo: 'Segunda fixada', corpo: 'p2', fixada: true, updated_at: hora(19) }),
  nota({ id: 'a', titulo: 'Aa', corpo: 'a', marcadores: ['m1'], updated_at: hora(18) }),
  nota({ id: 'b', titulo: 'Bb', corpo: 'b', marcadores: ['m1'], updated_at: hora(17) }),
  nota({ id: 'c', titulo: 'Cc', corpo: 'c', updated_at: hora(16) }),
  nota({ id: 'd', titulo: 'Dd', corpo: 'd', updated_at: hora(15) }),
  nota({ id: 'e', titulo: 'Ee', corpo: 'e', updated_at: hora(14) }),
  nota({ id: 'f', titulo: 'Ff', corpo: 'f', updated_at: hora(13) }),
  nota({ id: 'z', titulo: 'No arquivo', corpo: 'z', arquivada: true, updated_at: hora(12) }),
]

// A geometria de mentira: o container tem 1000 px de largura e começa 200 px abaixo
// do topo da janela (longe das bordas, onde a rolagem automática liga).
const LARGURA = 1000
const TOPO_DO_CONTAINER = 200
const ALTURA_PADRAO = 100

function instalarGeometria(alturas: Record<string, number> = {}) {
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
    const id = this.dataset.nota
    return id ? (alturas[id] ?? ALTURA_PADRAO) : 0
  })
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const el = this as HTMLElement
    const caixa = (left: number, top: number, width: number, height: number) =>
      ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top, toJSON: () => ({}) }) as DOMRect
    if (el.classList.contains('ordenada') || el.tagName === 'TBODY') {
      return caixa(0, TOPO_DO_CONTAINER, LARGURA, Number.parseFloat(el.style.height) || 0)
    }
    const id = el.dataset.nota
    const pai = el.parentElement
    if (!id || !pai) return caixa(0, 0, 0, 0)
    const t = el.style.transform
    const altura = alturas[id] ?? ALTURA_PADRAO
    if (el.tagName === 'TR') {
      // Na tabela as linhas ficam no fluxo: a posição é a da ordem no tbody, mais o deslocamento.
      const dy = Number(/translate3d\(0, (-?[\d.]+)px/.exec(t)?.[1] ?? 0)
      const i = Array.from(pai.children).indexOf(el)
      return caixa(0, TOPO_DO_CONTAINER + i * ALTURA_PADRAO + dy, LARGURA, altura)
    }
    const colunas = Number(pai.dataset.colunas ?? 1)
    const larguraDaColuna = (LARGURA - (colunas - 1) * 12) / colunas
    const naColuna = /translate3d\(calc\((\d+) \* \(100% \+ (\d+)px\)\), (-?[\d.]+)px/.exec(t)
    if (naColuna) {
      return caixa(Number(naColuna[1]) * (larguraDaColuna + Number(naColuna[2])), TOPO_DO_CONTAINER + Number(naColuna[3]), larguraDaColuna, altura)
    }
    const solta = /translate3d\((-?[\d.]+)px, (-?[\d.]+)px/.exec(t)
    if (solta) return caixa(Number(solta[1]), TOPO_DO_CONTAINER + Number(solta[2]), larguraDaColuna, altura)
    return caixa(0, TOPO_DO_CONTAINER, larguraDaColuna, altura)
  })
}

beforeEach(() => {
  window.localStorage.clear()
  window.scrollBy = vi.fn()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  Reflect.deleteProperty(navigator, 'vibrate')
})

type Servidor = ReturnType<typeof servidorFalso>

async function abrirNotas(opcoes: { srv?: Servidor; forma?: 'grade' | 'lista' | 'detalhes'; alturas?: Record<string, number>; notas?: Nota[] } = {}) {
  if (opcoes.forma) window.localStorage.setItem('aurora.notas.forma', opcoes.forma)
  instalarGeometria(opcoes.alturas)
  const srv = opcoes.srv ?? servidorFalso([], { notas: opcoes.notas ?? notasDeTeste(), marcadores: [marcador({ id: 'm1', nome: 'Compras' })] })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/marcadores')).toBe(true))
  await waitFor(() => expect(document.querySelector('[data-nota]')).toBeTruthy())
  await act(async () => {})
  return srv
}

const moverPedidos = (srv: { pedidos: Pedido[] }) => srv.pedidos.filter((p) => p.metodo === 'POST' && p.caminho.endsWith('/mover'))
const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!
const agarrar = (id: string) => cartao(id).querySelector<HTMLElement>('[data-abrir]')!
const ordemDaTela = (seletor = '.grade [data-nota]') => Array.from(document.querySelectorAll<HTMLElement>(seletor)).map((c) => c.dataset.nota)
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent
const anuncioDaOrdem = () => document.querySelector<HTMLElement>('[data-ordem]')!.textContent
const vaga = () => document.querySelector('.vaga')
const alt = (id: string, key: string) => fireEvent.keyDown(agarrar(id), { key, altKey: true })

const ponteiro = (x: number, y: number, tipo = 'mouse') => ({ pointerId: 1, pointerType: tipo, isPrimary: true, button: 0, clientX: x, clientY: y })
const apertar = (id: string, x: number, y: number, tipo = 'mouse') => fireEvent.pointerDown(agarrar(id), ponteiro(x, y, tipo))
const mexer = (x: number, y: number, tipo = 'mouse') => fireEvent.pointerMove(window, ponteiro(x, y, tipo))
const soltar = (x: number, y: number, tipo = 'mouse') => fireEvent.pointerUp(window, ponteiro(x, y, tipo))

// O centro do cartão `id` (grade de 4 colunas, 1000 px): a coluna c começa em c*253.
// A primeira linha tem os cartões de 100 px de altura, de y = 200 a 300.
const colunaX = (c: number) => c * 253 + 120
const LINHA_1_Y = 250

describe('a grade em ordem de leitura', () => {
  it('cada cartão fica na coluna mais curta, da esquerda para a direita', async () => {
    await abrirNotas({ alturas: { a: 300 } })
    // Outras: a (300) na coluna 0; b, c, d nas colunas 1, 2, 3; e na mais curta (a 1: 100, a mais à esquerda), f na 2.
    const transform = (id: string) => cartao(id).style.transform
    expect(transform('a')).toBe('translate3d(calc(0 * (100% + 12px)), 0px, 0)')
    expect(transform('b')).toBe('translate3d(calc(1 * (100% + 12px)), 0px, 0)')
    expect(transform('d')).toBe('translate3d(calc(3 * (100% + 12px)), 0px, 0)')
    expect(transform('e')).toBe('translate3d(calc(1 * (100% + 12px)), 112px, 0)')
    expect(transform('f')).toBe('translate3d(calc(2 * (100% + 12px)), 112px, 0)')
  })

  it('a largura do cartão sai das colunas e o container ganha a altura da disposição', async () => {
    await abrirNotas({ alturas: { a: 300 } })
    // O jsdom normaliza o calc(), mas o vão de 3 × 12 px e as 4 colunas estão lá.
    expect(cartao('b').style.width).toMatch(/36px/)
    expect(cartao('b').style.width).toMatch(/0\.25|\/ 4/)
    // As outras: a coluna 0 tem só o "a" (300), a 1 tem b e e (100 + 12 + 100).
    const grades = document.querySelectorAll<HTMLElement>('.grade')
    expect(grades[1]!.style.height).toBe('300px')
  })

  it('as fixadas ficam na seção delas, as outras na seguinte', async () => {
    await abrirNotas()
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'b', 'c', 'd', 'e', 'f'])
    const fixadas = screen.getByRole('region', { name: 'Fixadas' })
    expect(within(fixadas).getAllByRole('button', { name: /fixada/i }).length).toBeGreaterThan(0)
    expect(fixadas.querySelectorAll('[data-nota]')).toHaveLength(2)
  })
})

describe('arrastar com o mouse', () => {
  it('soltar depois de arrastar muda a ordem e pede ao Agent o "entre estas duas"', async () => {
    const srv = await abrirNotas()
    apertar('a', 100, LINHA_1_Y)
    mexer(colunaX(2), LINHA_1_Y)
    // Em arrasto: o cartão está levantado e há uma vaga aberta.
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(true)
    expect(vaga()).not.toBeNull()
    soltar(colunaX(2), LINHA_1_Y)
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'b', 'c', 'a', 'd', 'e', 'f'])
    expect(vaga()).toBeNull()
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(false)
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]).toMatchObject({ caminho: '/api/notas/a/mover', corpo: { antes: 'c', depois: 'd' } })
  })

  it('soltar na ponta pede só a vizinha que existe', async () => {
    const srv = await abrirNotas()
    apertar('f', colunaX(1), 312 + 50 - 12)
    mexer(colunaX(0) - 100, LINHA_1_Y - 40)
    soltar(colunaX(0) - 100, LINHA_1_Y - 40)
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'f', 'a', 'b', 'c', 'd', 'e'])
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: null, depois: 'a' })
  })

  it('o que foi movido continua assim depois de abrir o painel de novo', async () => {
    const srv = await abrirNotas()
    apertar('p1', 100, LINHA_1_Y)
    mexer(colunaX(1), LINHA_1_Y)
    soltar(colunaX(1), LINHA_1_Y)
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'p2', depois: null })
    cleanup()
    await abrirNotas({ srv })
    expect(ordemDaTela()).toEqual(['p2', 'p1', 'a', 'b', 'c', 'd', 'e', 'f'])
  })

  it('mexer menos que o limiar é clique: nada se move e a nota abre', async () => {
    const srv = await abrirNotas()
    apertar('a', 100, LINHA_1_Y)
    mexer(103, LINHA_1_Y + 2)
    expect(vaga()).toBeNull()
    soltar(103, LINHA_1_Y + 2)
    fireEvent.click(agarrar('a'))
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('o clique que vem logo depois de soltar um arrasto não abre a nota', async () => {
    await abrirNotas()
    apertar('a', 100, LINHA_1_Y)
    mexer(colunaX(1), LINHA_1_Y)
    soltar(colunaX(1), LINHA_1_Y)
    fireEvent.click(agarrar('a'))
    expect(screen.queryByRole('dialog')).toBeNull()
    // E o clique seguinte, de verdade, abre.
    await new Promise((r) => setTimeout(r, 5))
    fireEvent.click(agarrar('a'))
    expect(screen.getByRole('dialog')).toBeTruthy()
  })

  it('Esc no meio do arrasto cancela: a ordem não muda e nada vai ao Agent', async () => {
    const srv = await abrirNotas()
    apertar('a', 100, LINHA_1_Y)
    mexer(colunaX(2), LINHA_1_Y)
    expect(vaga()).not.toBeNull()
    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(vaga()).toBeNull()
    soltar(colunaX(2), LINHA_1_Y)
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'b', 'c', 'd', 'e', 'f'])
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('uma fixada não cai entre as outras, nem uma outra entre as fixadas', async () => {
    const srv = await abrirNotas()
    // A fixada p1, levada para o meio das outras (y de um cartão das outras): só tem
    // vaga entre as fixadas, então cai na última posição delas.
    apertar('p1', 100, LINHA_1_Y)
    mexer(colunaX(3), LINHA_1_Y + 400)
    soltar(colunaX(3), LINHA_1_Y + 400)
    expect(ordemDaTela()).toEqual(['p2', 'p1', 'a', 'b', 'c', 'd', 'e', 'f'])
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'p2', depois: null })
    // Uma outra, levada para o alto: continua a primeira das outras, e nenhum pedido sai.
    apertar('a', 100, LINHA_1_Y)
    mexer(colunaX(0), LINHA_1_Y - 150)
    soltar(colunaX(0), LINHA_1_Y - 150)
    expect(ordemDaTela()).toEqual(['p2', 'p1', 'a', 'b', 'c', 'd', 'e', 'f'])
    await act(async () => {})
    expect(moverPedidos(srv)).toHaveLength(1)
  })

  it('se o Agent recusa, a ordem de antes volta, a lista é lida de novo e a pílula diz o que houve', async () => {
    const srv = await abrirNotas()
    srv.interceptar((p) =>
      p.caminho.endsWith('/mover') ? new Response(JSON.stringify({ error: { code: 'BAD_REQUEST', message: 'o Agent recusou' } }), { status: 400, headers: { 'Content-Type': 'application/json' } }) : undefined,
    )
    const leituras = () => srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho === '/api/notas').length
    const antes = leituras()
    // A leitura que vem depois da recusa fica presa: a volta da ordem não pode depender dela.
    const soltarLeitura = srv.pausar((p) => p.metodo === 'GET' && p.caminho === '/api/notas')
    apertar('a', 100, LINHA_1_Y)
    mexer(colunaX(2), LINHA_1_Y)
    soltar(colunaX(2), LINHA_1_Y)
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'b', 'c', 'a', 'd', 'e', 'f'])
    await waitFor(() => expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'b', 'c', 'd', 'e', 'f']))
    await waitFor(() => expect(aviso()).toContain('Não deu certo'))
    await waitFor(() => expect(leituras()).toBeGreaterThan(antes))
    soltarLeitura()
  })

  it('o 409 do Agent (vizinhas que mudaram) volta a ordem com o aviso próprio', async () => {
    const srv = await abrirNotas()
    srv.interceptar((p) =>
      p.caminho.endsWith('/mover') ? new Response(JSON.stringify({ error: { code: 'CONFLICT', message: 'as notas vizinhas mudaram' } }), { status: 409, headers: { 'Content-Type': 'application/json' } }) : undefined,
    )
    apertar('a', 100, LINHA_1_Y)
    mexer(colunaX(2), LINHA_1_Y)
    soltar(colunaX(2), LINHA_1_Y)
    await waitFor(() => expect(aviso()).toContain('A lista das notas mudou em outra aba'))
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'b', 'c', 'd', 'e', 'f'])
  })

  it('apertar numa ferramenta do cartão (fixar, menu) não começa um arrasto', async () => {
    await abrirNotas()
    const fixar = within(cartao('a')).getByRole('button', { name: /Fixar a nota/ })
    fireEvent.pointerDown(fixar, ponteiro(100, LINHA_1_Y))
    mexer(colunaX(2), LINHA_1_Y)
    expect(vaga()).toBeNull()
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(false)
    soltar(colunaX(2), LINHA_1_Y)
  })

  it('o botão direito não arrasta', async () => {
    await abrirNotas()
    fireEvent.pointerDown(agarrar('a'), { ...ponteiro(100, LINHA_1_Y), button: 2 })
    mexer(colunaX(2), LINHA_1_Y)
    expect(vaga()).toBeNull()
    soltar(colunaX(2), LINHA_1_Y)
  })

  it('perto da borda de baixo da janela a página rola', async () => {
    await abrirNotas()
    apertar('a', 100, LINHA_1_Y)
    mexer(100, 760)
    await waitFor(() => expect(window.scrollBy).toHaveBeenCalled())
    expect(vi.mocked(window.scrollBy).mock.calls[0]![1]).toBeGreaterThan(0)
    soltar(100, 760)
  })
})

describe('arrastar com o toque', () => {
  it('segurar uns 400 ms levanta o cartão (com uma vibração curta) e depois ele anda', async () => {
    const vibrar = vi.fn()
    Object.defineProperty(navigator, 'vibrate', { value: vibrar, configurable: true })
    const srv = await abrirNotas()
    apertar('a', 100, LINHA_1_Y, 'touch')
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(false)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 450))
    })
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(true)
    expect(vibrar).toHaveBeenCalledWith(10)
    mexer(colunaX(2), LINHA_1_Y, 'touch')
    soltar(colunaX(2), LINHA_1_Y, 'touch')
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'b', 'c', 'a', 'd', 'e', 'f'])
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'c', depois: 'd' })
  })

  it('mexer antes de segurar é a rolagem da página: o cartão não levanta', async () => {
    const vibrar = vi.fn()
    Object.defineProperty(navigator, 'vibrate', { value: vibrar, configurable: true })
    const srv = await abrirNotas()
    apertar('a', 100, LINHA_1_Y, 'touch')
    mexer(100, LINHA_1_Y + 40, 'touch')
    await act(async () => {
      await new Promise((r) => setTimeout(r, 450))
    })
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(false)
    expect(vibrar).not.toHaveBeenCalled()
    soltar(100, LINHA_1_Y + 40, 'touch')
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('largar antes dos 400 ms é um toque: nada levanta', async () => {
    await abrirNotas()
    apertar('a', 100, LINHA_1_Y, 'touch')
    soltar(100, LINHA_1_Y, 'touch')
    await act(async () => {
      await new Promise((r) => setTimeout(r, 450))
    })
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(false)
  })
})

describe('o teclado: Alt + setas', () => {
  it('na grade, → anda uma vaga, o foco fica no cartão e o leitor de tela ouve a posição nova', async () => {
    const srv = await abrirNotas()
    agarrar('a').focus()
    expect(alt('a', 'ArrowRight')).toBe(false) // preventDefault: Alt + → é "avançar" do navegador
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'b', 'a', 'c', 'd', 'e', 'f'])
    expect(anuncioDaOrdem()).toBe('Movida para a posição 2 de 6.')
    expect(document.activeElement).toBe(agarrar('a'))
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'b', depois: 'c' })
  })

  it('na grade, ↓ anda uma linha (as colunas) e ← volta uma vaga', async () => {
    const srv = await abrirNotas()
    alt('a', 'ArrowDown')
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'b', 'c', 'd', 'e', 'a', 'f'])
    expect(anuncioDaOrdem()).toBe('Movida para a posição 5 de 6.')
    alt('a', 'ArrowLeft')
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'b', 'c', 'd', 'a', 'e', 'f'])
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(2))
  })

  it('na ponta o cartão fica onde está e a frase diz isso, sem pedir nada ao Agent', async () => {
    const srv = await abrirNotas()
    alt('a', 'ArrowLeft')
    expect(anuncioDaOrdem()).toBe('Já está na posição 1 de 6.')
    // As fixadas são outro grupo: a primeira das outras não passa para as fixadas.
    alt('a', 'ArrowUp')
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'b', 'c', 'd', 'e', 'f'])
    await act(async () => {})
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('o Alt + seta de uma fixada anda entre as fixadas', async () => {
    const srv = await abrirNotas()
    alt('p1', 'ArrowRight')
    expect(ordemDaTela()).toEqual(['p2', 'p1', 'a', 'b', 'c', 'd', 'e', 'f'])
    expect(anuncioDaOrdem()).toBe('Movida para a posição 2 de 2.')
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'p2', depois: null })
  })

  it('sem o Alt, ou com Shift ou Ctrl, a seta não move', async () => {
    const srv = await abrirNotas()
    fireEvent.keyDown(agarrar('a'), { key: 'ArrowRight' })
    fireEvent.keyDown(agarrar('a'), { key: 'ArrowRight', altKey: true, shiftKey: true })
    fireEvent.keyDown(agarrar('a'), { key: 'ArrowRight', altKey: true, ctrlKey: true })
    await act(async () => {})
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'b', 'c', 'd', 'e', 'f'])
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('na lista, ↑ e ↓ andam uma vaga e ← e → não fazem nada', async () => {
    const srv = await abrirNotas({ forma: 'lista' })
    const seletor = '.notas-lista [data-nota]'
    expect(alt('c', 'ArrowRight')).toBe(true) // não tratada: o navegador segue
    expect(ordemDaTela(seletor)).toEqual(['p1', 'p2', 'a', 'b', 'c', 'd', 'e', 'f'])
    expect(alt('c', 'ArrowUp')).toBe(false)
    expect(ordemDaTela(seletor)).toEqual(['p1', 'p2', 'a', 'c', 'b', 'd', 'e', 'f'])
    expect(anuncioDaOrdem()).toBe('Movida para a posição 2 de 6.')
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'a', depois: 'b' })
  })

  it('depois de mover, fixar a nota usa a versão que o mover devolveu (sem 409 à toa) e a leva ao topo das fixadas', async () => {
    const srv = await abrirNotas()
    alt('c', 'ArrowLeft')
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    fireEvent.click(within(cartao('c')).getByRole('button', { name: /Fixar a nota/ }))
    await waitFor(() => expect(srv.pedidos.filter((p) => p.metodo === 'PATCH')).toHaveLength(1))
    // A nota estava na versão 1; mover subiu para 2; o PATCH leva a 2.
    expect(srv.pedidos.find((p) => p.metodo === 'PATCH')!.corpo).toEqual({ fixada: true, base_version: 2 })
    await waitFor(() => expect(ordemDaTela()).toEqual(['c', 'p1', 'p2', 'a', 'b', 'd', 'e', 'f']))
    expect(aviso() ?? '').not.toContain('mudou em outra aba')
  })
})

describe('desligado onde a ordem não é a das notas', () => {
  it('no filtro de marcador, arrastar com o mouse avisa e não move', async () => {
    const srv = await abrirNotas()
    fireEvent.click(within(await screen.findByRole('group', { name: 'Marcadores' })).getByRole('button', { name: /Compras/ }))
    await waitFor(() => expect(ordemDaTela()).toEqual(['a', 'b']))
    expect(cartao('a').hasAttribute('data-ordenavel')).toBe(false)
    apertar('a', 100, LINHA_1_Y)
    mexer(colunaX(2), LINHA_1_Y)
    expect(aviso()).toContain('saia do filtro de marcador')
    expect(vaga()).toBeNull()
    soltar(colunaX(2), LINHA_1_Y)
    expect(ordemDaTela()).toEqual(['a', 'b'])
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('no filtro de marcador, Alt + seta também avisa', async () => {
    const srv = await abrirNotas()
    fireEvent.click(within(await screen.findByRole('group', { name: 'Marcadores' })).getByRole('button', { name: /Compras/ }))
    await waitFor(() => expect(ordemDaTela()).toEqual(['a', 'b']))
    fireEvent.keyDown(agarrar('a'), { key: 'ArrowRight', altKey: true })
    expect(aviso()).toContain('saia do filtro de marcador')
    expect(ordemDaTela()).toEqual(['a', 'b'])
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('na busca ("Ver todas as notas"), arrastar avisa para limpar a busca', async () => {
    const muitas = Array.from({ length: 7 }, (_, i) => nota({ id: `m${i}`, titulo: `Imposto ${i + 1}`, corpo: 'texto', updated_at: hora(20 - i) }))
    const srv = await abrirNotas({ notas: muitas })
    fireEvent.change(screen.getByRole('searchbox', { name: 'Buscar nas notas e nos ditados' }), { target: { value: 'imposto' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Ver todas as 7 notas' }))
    await waitFor(() => expect(document.querySelectorAll('.grade [data-nota]')).toHaveLength(7))
    expect(cartao('m0').hasAttribute('data-ordenavel')).toBe(false)
    apertar('m0', 100, LINHA_1_Y)
    mexer(colunaX(2), LINHA_1_Y)
    expect(aviso()).toContain('Para reorganizar, limpe a busca.')
    soltar(colunaX(2), LINHA_1_Y)
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('no Arquivo, arrastar avisa da ordem do arquivo', async () => {
    const srv = await abrirNotas()
    fireEvent.click(screen.getByRole('button', { name: 'Arquivo' }))
    await waitFor(() => expect(ordemDaTela()).toEqual(['z']))
    apertar('z', 100, LINHA_1_Y)
    mexer(colunaX(2), LINHA_1_Y)
    expect(aviso()).toContain('O Arquivo fica na ordem em que as notas foram arquivadas.')
    soltar(colunaX(2), LINHA_1_Y)
    expect(moverPedidos(srv)).toHaveLength(0)
  })

  it('no toque, sem poder reorganizar, segurar não levanta nada', async () => {
    await abrirNotas()
    fireEvent.click(within(await screen.findByRole('group', { name: 'Marcadores' })).getByRole('button', { name: /Compras/ }))
    await waitFor(() => expect(ordemDaTela()).toEqual(['a', 'b']))
    apertar('a', 100, LINHA_1_Y, 'touch')
    await act(async () => {
      await new Promise((r) => setTimeout(r, 450))
    })
    expect(cartao('a').hasAttribute('data-arrastando')).toBe(false)
    soltar(100, LINHA_1_Y, 'touch')
  })
})

describe('as outras formas de ver', () => {
  it('na lista, arrastar uma linha para baixo troca a ordem', async () => {
    const srv = await abrirNotas({ forma: 'lista' })
    // A lista é uma coluna de 1000 px de largura, cada linha com 100 px (mais 6 de vão).
    apertar('a', 100, 250)
    mexer(100, 250 + 106 * 2)
    soltar(100, 250 + 106 * 2)
    expect(ordemDaTela('.notas-lista [data-nota]')).toEqual(['p1', 'p2', 'b', 'c', 'a', 'd', 'e', 'f'])
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'c', depois: 'd' })
  })

  it('nos detalhes cada seção é um tbody; arrastar uma linha a leva para a posição nova', async () => {
    const srv = await abrirNotas({ forma: 'detalhes' })
    const corpos = document.querySelectorAll('tbody')
    expect(corpos).toHaveLength(2)
    expect(Array.from(corpos[0]!.querySelectorAll('tr')).map((t) => t.dataset.nota)).toEqual(['p1', 'p2'])
    // A linha p1 (y 200 a 300), levada para a altura da segunda (y 300 a 400) e um pouco mais.
    apertar('p1', 50, 250)
    mexer(50, 360)
    expect(cartao('p1').hasAttribute('data-arrastando')).toBe(true)
    soltar(50, 360)
    expect(Array.from(document.querySelectorAll('tbody')[0]!.querySelectorAll('tr')).map((t) => t.dataset.nota)).toEqual(['p2', 'p1'])
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    expect(moverPedidos(srv)[0]!.corpo).toEqual({ antes: 'p2', depois: null })
  })

  it('nos detalhes, Alt + ↓ anda uma linha dentro do grupo', async () => {
    const srv = await abrirNotas({ forma: 'detalhes' })
    fireEvent.keyDown(agarrar('a'), { key: 'ArrowDown', altKey: true })
    expect(Array.from(document.querySelectorAll('tbody')[1]!.querySelectorAll('tr')).map((t) => t.dataset.nota)).toEqual(['b', 'a', 'c', 'd', 'e', 'f'])
    expect(anuncioDaOrdem()).toBe('Movida para a posição 2 de 6.')
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
  })
})

describe('o que muda (e o que não muda) a ordem', () => {
  it('uma nota nova entra no topo das outras (não no fim, nem entre as fixadas)', async () => {
    await abrirNotas()
    fireEvent.click(await screen.findByRole('button', { name: 'Criar uma nota…' }))
    const corpo = screen.getAllByRole('textbox').find((e) => e.tagName === 'TEXTAREA')!
    fireEvent.change(corpo, { target: { value: 'nota nova' } })
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(ordemDaTela()).toHaveLength(9))
    const ordem = ordemDaTela()
    expect(ordem.slice(0, 2)).toEqual(['p1', 'p2'])
    expect(ordem.slice(3)).toEqual(['a', 'b', 'c', 'd', 'e', 'f'])
  })

  it('editar uma nota não a tira do lugar onde a pessoa a deixou', async () => {
    const srv = await abrirNotas()
    alt('c', 'ArrowLeft')
    await waitFor(() => expect(moverPedidos(srv)).toHaveLength(1))
    fireEvent.click(agarrar('b'))
    const titulo = within(screen.getByRole('dialog')).getByRole<HTMLInputElement>('textbox', { name: 'Título' })
    fireEvent.change(titulo, { target: { value: 'Bb editada' } })
    fireEvent.keyDown(titulo, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(srv.pedidos.filter((p) => p.metodo === 'PATCH')).toHaveLength(1))
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'c', 'b', 'd', 'e', 'f'])
    // E ao ler a lista de novo (outro painel), a ordem é a mesma.
    cleanup()
    await abrirNotas({ srv })
    expect(ordemDaTela()).toEqual(['p1', 'p2', 'a', 'c', 'b', 'd', 'e', 'f'])
  })
})
