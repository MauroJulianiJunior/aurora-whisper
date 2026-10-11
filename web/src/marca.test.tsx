// As marcas do painel conferidas contra os arquivos de docs/marca, que o
// Mauro escolheu: o desenho traço a traço, as cores de cada tema e os
// lugares onde entram. No painel elas são cópias inline (o painel não
// carrega nada de fora), e cópia sem conferência desanda.

import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import picoClaro from '../../docs/marca/pico-estourado/mark-light.svg?raw'
import picoPequeno from '../../docs/marca/pico-estourado/mark-small.svg?raw'
import picoEscuro from '../../docs/marca/pico-estourado/mark.svg?raw'
import retratoClaro from '../../docs/marca/retrato/mark-light.svg?raw'
import retratoEscuro from '../../docs/marca/retrato/mark.svg?raw'
import favicon from '../public/favicon.svg?raw'
import App from './App'
import { SemSessao } from './api'
import { Aurora } from './Aurora'
import { Marca } from './Marca'
import { type Tema, regras, temaClaro, temaEscuro, valor } from './testes/css'
import { ditado, servidorFalso } from './testes/servidorFalso'

afterEach(cleanup)

const arquivo = (svg: string) => new DOMParser().parseFromString(svg, 'image/svg+xml').documentElement

function componente(el: ReactElement): Element {
  const { container } = render(el)
  const svg = container.querySelector('svg')!
  cleanup()
  return svg
}

// cor resolve fill, stroke ou color do jeito do navegador, no que estes
// SVG usam: a regra da classe vence o atributo, o valor herda dos
// ancestrais, currentColor é o color de onde foi declarado, e var() é o
// token do tema.
function cor(el: Element, prop: string, tema: Tema): string | undefined {
  for (let e: Element | null = el; e; e = e.parentElement) {
    const doCss = [...e.classList].map((c) => regras.find((r) => r.seletor === `.${c}`)).map((r) => r && valor(r.corpo, prop))
    const v = doCss.find(Boolean) ?? e.getAttribute(prop)
    if (!v) continue
    if (v === 'currentColor') return cor(e, 'color', tema)
    const token = v.match(/^var\(--([\w-]+)\)$/)?.[1]
    return (token ? tema[token] : v)?.toLowerCase()
  }
  return undefined
}

function herdado(el: Element, atributo: string): string | undefined {
  for (let e: Element | null = el; e; e = e.parentElement) {
    const v = e.getAttribute(atributo)
    if (v !== null) return v
  }
  return undefined
}

// desenho descreve cada forma, em ordem: a geometria, os atributos de
// traço que ela herda, e a cor que ela tem no tema.
function desenho(svg: Element, tema: Tema = {}) {
  return [...svg.querySelectorAll('path, circle, rect, ellipse, line, polyline, polygon')].map((el) => {
    const d: Record<string, string | undefined> = { forma: el.tagName.toLowerCase() }
    for (const a of ['d', 'cx', 'cy', 'r', 'x', 'y', 'width', 'height', 'rx']) if (el.hasAttribute(a)) d[a] = el.getAttribute(a)!
    for (const a of ['fill-rule', 'stroke-width', 'stroke-linecap', 'stroke-linejoin']) d[a] = herdado(el, a)
    d.fill = cor(el, 'fill', tema)
    d.stroke = cor(el, 'stroke', tema)
    return d
  })
}

describe('o desenho é o dos arquivos de docs/marca', () => {
  it('favicon: o pico estourado pequeno, com o ladrilho preto', () => {
    expect(arquivo(favicon).getAttribute('viewBox')).toBe(arquivo(picoPequeno).getAttribute('viewBox'))
    expect(desenho(arquivo(favicon))).toEqual(desenho(arquivo(picoPequeno)))
  })

  // No escuro o ladrilho some no fundo, que é o mesmo preto; no claro ele
  // seria a coisa mais pesada do cabeçalho. Sem ele, as cores seguem as
  // variantes da própria marca.
  it('cabeçalho: o pico estourado pequeno sem o ladrilho, nas cores de mark.svg (escuro) e mark-light.svg (claro)', () => {
    const marca = componente(<Marca />)
    const [ladrilho, ...tracos] = desenho(arquivo(picoPequeno))
    expect(ladrilho).toMatchObject({ forma: 'rect', width: '32', height: '32', fill: temaEscuro.fundo })
    expect(marca.getAttribute('viewBox')).toBe(arquivo(picoPequeno).getAttribute('viewBox'))
    expect(desenho(marca, temaEscuro)).toEqual(tracos)

    // A moldura é o primeiro traço do arquivo; a barra que fura, o último.
    for (const [tema, variante] of [
      [temaEscuro, picoEscuro],
      [temaClaro, picoClaro],
    ] as const) {
      const cores = desenho(arquivo(variante)).map((f) => f.stroke)
      expect(desenho(marca, tema).map((f) => f.stroke)).toEqual([cores[0], cores.at(-1)])
    }
  })

  it('a Aurora grande: mark.svg no escuro, mark-light.svg no claro', () => {
    const aurora = componente(<Aurora tamanho={96} />)
    expect(aurora.getAttribute('viewBox')).toBe(arquivo(retratoEscuro).getAttribute('viewBox'))
    expect(desenho(aurora, temaEscuro)).toEqual(desenho(arquivo(retratoEscuro)))
    expect(desenho(aurora, temaClaro)).toEqual(desenho(arquivo(retratoClaro)))
  })

  // O texto ao lado diz tudo: a marca não é lida, não recebe foco e não
  // tem título que um leitor de tela anuncie.
  it.each([
    ['Marca', <Marca />],
    ['Aurora', <Aurora tamanho={96} />],
  ])('%s é decorativa', (_, el) => {
    const svg = componente(el)
    expect(svg.getAttribute('aria-hidden')).toBe('true')
    expect(svg.getAttribute('focusable')).toBe('false')
    expect(svg.querySelector('title')).toBeNull()
  })
})

describe('onde as marcas entram', () => {
  const em = (dia: number, hora: number) => new Date(2026, 8, dia, hora).toISOString()
  const auroraGrande = (onde: string) => document.querySelector(`${onde} svg.aurora`)
  const semImagem = () => expect(document.querySelectorAll('img')).toHaveLength(0)
  // A Aurora grande vai de 80 a 120 px, a faixa combinada com o Mauro: é
  // o bastante para o retrato ler, e o painel continua discreto ("o menor
  // que funciona").
  function tamanhoDaAurora(onde: string) {
    const aurora = auroraGrande(onde)
    expect(aurora, `falta a Aurora em ${onde}`).not.toBeNull()
    const tamanho = Number(aurora!.getAttribute('width'))
    expect(aurora!.getAttribute('height')).toBe(String(tamanho))
    expect(tamanho).toBeGreaterThanOrEqual(80)
    expect(tamanho).toBeLessThanOrEqual(120)
  }

  function montar(itens = [ditado({ id: 'd1', created_at: em(28, 14), final_text: 'Manda para o Thiago.' })]) {
    const srv = servidorFalso(itens)
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    return srv
  }

  it('cabeçalho: o pico estourado ao lado do nome', async () => {
    montar()
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    const nome = document.querySelector('.nome')!
    expect(nome.textContent).toBe('Aurora Whisper')
    const marca = nome.querySelector('svg.marca')!
    expect(marca).not.toBeNull()
    // 16 px, o tamanho da bandeja e da aba: na grade de 32 do mark-small,
    // o traço de 4 unidades vira 2 px inteiros (a 24 px, 3 px em meio
    // pixel, borrado), e a marca não passa da altura do nome.
    expect(marca.getAttribute('width')).toBe('16')
    expect(marca.getAttribute('height')).toBe('16')
    semImagem()
  })

  it('a tela de abrir pela bandeja: a Aurora grande', async () => {
    render(<App sessao={Promise.reject(new SemSessao())} />)
    await screen.findByRole('heading', { name: 'Abra o painel pela bandeja' })
    tamanhoDaAurora('.bloqueio')
    semImagem()
  })

  // Embaixo da Aurora vem sempre um título: sem ele, na lixeira, o retrato
  // dominava uma frase cinza de cinco palavras.
  it('histórico vazio e lixeira vazia: a Aurora grande, sobre um título', async () => {
    montar([])
    await screen.findByText('Nenhum ditado ainda.', { selector: '.estado p.estado-titulo' })
    tamanhoDaAurora('.estado')

    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByText('A lixeira está vazia.', { selector: '.estado p.estado-titulo' })
    tamanhoDaAurora('.estado')
    semImagem()
  })

  // "Nada encontrado" é resposta à busca, não o painel falando de si.
  it('busca sem resultado: sem a Aurora', async () => {
    montar()
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'inexistente' } })
    await screen.findByText('Nada encontrado para “inexistente”.', { selector: '.estado p' })
    expect(auroraGrande('.estado')).toBeNull()
  })

  // A Aurora pequena saiu da faixa (decisão do Mauro, 29-set-2026): chamava
  // mais atenção que a mensagem, num alerta que o design system pinta de
  // preto e vermelho. A faixa é só texto — e o botão, na de fora do ar.
  it('faixa de Agent fora do ar e de sessão perdida: só texto, sem a Aurora', async () => {
    const srv = montar()
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    srv.derrubar(true)
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    const foraDoAr = await screen.findByRole('alert')
    expect(foraDoAr.textContent).toContain('não está respondendo')
    // .faixa é a regra do vermelho do sistema (estilo.test.ts).
    expect(foraDoAr.className).toBe('faixa')
    expect(foraDoAr.querySelector('svg')).toBeNull()

    srv.derrubar(false)
    srv.interceptar(() => new Response('unauthorized', { status: 401 }))
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    const semSessao = await screen.findByText(/perdeu o acesso/)
    expect(semSessao.closest('.faixa')!.querySelector('svg')).toBeNull()
    semImagem()
  })
})
