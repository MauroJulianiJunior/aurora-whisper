// A página de calibração do microfone (PaginaDeCalibracao.tsx; ADR-021, C6c) com uma
// captura e um relógio de mentira: o roteiro, o que o app usa hoje, as duas
// medições (o silêncio e a fala), os números que aparecem, os erros, e a prova
// de que a página só mostra números (nenhum botão de salvar, baixar, copiar ou
// enviar; nenhuma rede; nada guardado). Nenhum teste abre o microfone.

import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Captura, InfoDaCaptura } from './calibracao'
import { PaginaDeCalibracao } from './PaginaDeCalibracao'
import { plataformaFalsa } from './plataformaFalsa'

afterEach(cleanup)

const info: InfoDaCaptura = {
  taxaDoContexto: 48_000,
  eco: true,
  ruido: true,
  ganho: false,
  canais: 1,
  taxaDaFaixa: 48_000,
  tipoDoRecorder: 'audio/webm;codecs=opus',
  bitsPedidos: 32_000,
  bitsDoRecorder: 32_000,
}

function montar(erro?: unknown) {
  const p = plataformaFalsa()
  const ouvintes = new Set<(bytes: number) => void>()
  const captura = {
    info,
    nivel: () => p.mic.nivelAtual,
    aoPedaco(cb: (bytes: number) => void) {
      ouvintes.add(cb)
      return () => void ouvintes.delete(cb)
    },
    parar: vi.fn(async () => undefined),
  } satisfies Captura
  const abrir = vi.fn(async () => {
    if (erro) throw erro
    return captura
  })
  const r = render(<PaginaDeCalibracao abrir={abrir} relogio={p.plataforma} />)
  // pedaco: o MediaRecorder entregou um pedaço (só o tamanho chega à página).
  const pedaco = (bytes: number) => act(() => void [...ouvintes].forEach((cb) => cb(bytes)))
  return { p, captura, abrir, pedaco, ...r }
}

const botao = (nome: RegExp | string) => screen.getByRole('button', { name: nome })
const status = () => screen.getByRole('status')

// medir toca no botão e deixa o microfone abrir.
async function medir(nome: RegExp | string) {
  await act(async () => {
    fireEvent.click(botao(nome))
  })
}

// correr avança o relógio de mentira com o nível dado, e entrega um pedaço por
// segundo (o MediaRecorder da página pica de 1 em 1 s).
function correr(m: ReturnType<typeof montar>, ms: number, nivel: number | ((desde: number) => number), bytesPorPedaco = 4000) {
  for (let feito = 0; feito < ms; feito += 1000) {
    act(() => m.p.rodar(Math.min(1000, ms - feito), typeof nivel === 'function' ? (d) => nivel(feito + d) : nivel))
    m.pedaco(bytesPorPedaco)
  }
}

describe('em repouso', () => {
  it('o título, o roteiro e o que o app usa hoje, com os números do código', () => {
    montar()
    expect(screen.getByRole('heading', { level: 1, name: 'Calibração do microfone' })).toBeTruthy()
    const roteiro = screen.getByRole('region', { name: 'Roteiro' })
    expect(within(roteiro).getAllByRole('listitem')).toHaveLength(4)
    expect(roteiro.textContent).toContain('Medir o silêncio e fique quieto por 6,0 s')
    expect(roteiro.textContent).toContain('Medir a fala e leia um texto em voz normal por 12,0 s')
    const app = screen.getByRole('region', { name: 'O que o app usa hoje' })
    expect(app.textContent).toContain('Limiar de fala (RMS)0,0300')
    expect(app.textContent).toContain('300 ms, sem os últimos 200 ms')
    expect(app.textContent).toContain('-48 a -14 dBFS, janela de 1,3 s')
    expect(app.textContent).toContain('audio/webm;codecs=opus a 32 kbit/s')
    expect(app.textContent).toContain('120 s, 1.024 KiB')
  })

  it('nada medido ainda: nenhum resultado, nenhum erro, a região viva vazia', () => {
    montar()
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(status().textContent).toBe('')
    expect(botao('Parar agora').getAttribute('aria-disabled')).toBe('true')
  })

  it('o microfone só abre no toque: abrir a página não pede nada', () => {
    const m = montar()
    expect(m.abrir).not.toHaveBeenCalled()
  })
})

describe('medir o silêncio', () => {
  it('abre o microfone, mostra o nível enquanto mede, e no fim mostra os números e desliga o microfone', async () => {
    const m = montar()
    await medir(/^Medir o silêncio/)
    expect(m.abrir).toHaveBeenCalledTimes(1)
    expect(status().textContent).toMatch(/^Medindo o silêncio: restam \d/)
    // Enquanto mede, os outros botões esperam.
    expect(botao(/^Medir a fala/).getAttribute('aria-disabled')).toBe('true')
    expect(botao('Parar agora').getAttribute('aria-disabled')).toBeNull()
    correr(m, 3000, 0.001)
    expect(status().textContent).toContain('Nível agora: RMS 0,0010')

    correr(m, 3000, 0.001)
    await act(async () => {})
    expect(m.captura.parar).toHaveBeenCalledTimes(1)
    expect(status().textContent).toBe('')

    const r = screen.getByRole('region', { name: /^Silêncio: 6,0 s, \d+ quadros$/ })
    const percentis = within(r).getAllByRole('table')[0]!
    const linhas = within(percentis).getAllByRole('row').slice(1)
    expect(linhas.map((l) => within(l).getAllByRole('cell').map((c) => c.textContent))).toEqual(
      Array.from({ length: 10 }, () => ['0,0010', '-60,0']),
    )
    expect(within(linhas[1]!).getByRole('rowheader').textContent).toBe('p5 (o piso)')
    expect(within(linhas[9]!).getByRole('rowheader').textContent).toBe('máximo (o pico)')
    // Nenhum quadro passou dos limiares; a fala medida é zero e o app não enviaria.
    expect(r.textContent).toContain('0 ms de fala')
    expect(r.textContent).toContain('o app diria "Não ouvi nada." e não enviaria')
    // Os pedaços foram contados (6 de 4000 bytes) e jogados fora.
    expect(r.textContent).toContain('24.000 bytes')
    expect(r.textContent).toContain('4.000 e 4.000 bytes')
    expect(r.textContent).toContain('32 kbit/s; 32 kbit/s')
    expect(r.textContent).toContain('48.000 Hz')
    expect(botao(/^Medir a fala/).getAttribute('aria-disabled')).toBeNull()
  })

  it('o limiar do app vem marcado na tabela do tempo acima de cada limiar', async () => {
    const m = montar()
    await medir(/^Medir o silêncio/)
    correr(m, 6000, 0.05)
    await act(async () => {})
    const r = screen.getByRole('region', { name: /^Silêncio/ })
    const marcado = within(r).getByRole('rowheader', { name: '0,0300 (o do app)' })
    expect(within(marcado.closest('tr')!).getAllByRole('cell')[0]!.textContent).toMatch(/^\d[\d.]* ms$/)
    expect(within(r).getAllByRole('rowheader', { name: /\(o do app\)/ })).toHaveLength(1)
  })
})

describe('medir a fala', () => {
  it('a fala acima do limiar conta: o app enviaria, e a distribuição aparece em bandas de 6 dB', async () => {
    const m = montar()
    await medir(/^Medir a fala/)
    // Sílabas fortes com pausas.
    correr(m, 12_000, (d) => (Math.floor(d / 400) % 2 === 0 ? 0.12 : 0.004))
    await act(async () => {})
    expect(m.captura.parar).toHaveBeenCalledTimes(1)
    const r = screen.getByRole('region', { name: /^Fala: 12,0 s/ })
    expect(r.textContent).toMatch(/\d[\d.]* ms de fala, com o limiar de 0,0300 e sem os últimos 200 ms\./)
    expect(r.textContent).toContain('Passa dos 300 ms: o app enviaria.')
    const bandas = within(r).getAllByRole('table')[2]!
    const cabecalhos = within(bandas).getAllByRole('rowheader').map((h) => h.textContent)
    expect(cabecalhos[0]).toBe('abaixo de -96')
    expect(cabecalhos).toHaveLength(17)
    expect(cabecalhos[16]).toBe('-6 a 0')
    // Os dois níveis (0,12 = -18,4 dBFS; 0,004 = -48 dBFS) caem em bandas.
    const comQuadros = within(bandas)
      .getAllByRole('row')
      .slice(1)
      .filter((l) => within(l).getAllByRole('cell')[0]!.textContent !== '0')
    expect(comQuadros).toHaveLength(2)
  })

  it('o resultado da mesma fase é refeito a cada medição; o da outra fica', async () => {
    const m = montar()
    await medir(/^Medir o silêncio/)
    correr(m, 6000, 0.001)
    await act(async () => {})
    await medir(/^Medir a fala/)
    correr(m, 12_000, 0.1)
    await act(async () => {})
    expect(screen.getAllByRole('region', { name: /quadros$/ })).toHaveLength(2)
    expect(screen.getAllByRole('region', { name: /quadros$/ }).map((r) => r.querySelector('h2')!.textContent!.split(':')[0])).toEqual(['Silêncio', 'Fala'])

    await medir(/^Medir o silêncio/)
    correr(m, 6000, 0.002)
    await act(async () => {})
    const silencio = screen.getByRole('region', { name: /^Silêncio/ })
    expect(within(silencio).getAllByRole('cell')[0]!.textContent).toBe('0,0020')
    expect(m.abrir).toHaveBeenCalledTimes(3)
  })
})

describe('parar agora, zerar e sair', () => {
  it('Parar agora encerra a medição com o que já há, e desliga o microfone', async () => {
    const m = montar()
    await medir(/^Medir a fala/)
    correr(m, 3000, 0.1)
    await act(async () => {
      fireEvent.click(botao('Parar agora'))
    })
    expect(m.captura.parar).toHaveBeenCalledTimes(1)
    const r = screen.getByRole('region', { name: /^Fala: 3,0 s/ })
    expect(r).toBeTruthy()
    // O prazo de 12 s que sobrou não mede de novo.
    act(() => m.p.rodar(12_000, 0))
    await act(async () => {})
    expect(m.captura.parar).toHaveBeenCalledTimes(1)
  })

  it('um segundo toque com a medição em curso não abre outro microfone', async () => {
    const m = montar()
    await medir(/^Medir a fala/)
    await medir(/^Medir o silêncio/)
    await medir(/^Medir a fala/)
    expect(m.abrir).toHaveBeenCalledTimes(1)
  })

  it('Zerar tira os resultados; durante a medição não faz nada', async () => {
    const m = montar()
    await medir(/^Medir o silêncio/)
    fireEvent.click(botao('Zerar'))
    correr(m, 6000, 0.001)
    await act(async () => {})
    expect(screen.getAllByRole('table').length).toBeGreaterThan(0)
    fireEvent.click(botao('Zerar'))
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('sair da página com o microfone aberto o desliga', async () => {
    const m = montar()
    await medir(/^Medir a fala/)
    correr(m, 2000, 0.1)
    m.unmount()
    await act(async () => {})
    expect(m.captura.parar).toHaveBeenCalledTimes(1)
  })
})

describe('quando o microfone não abre', () => {
  it('a permissão negada: a frase do gravador, sem resultado, e dá para tentar de novo', async () => {
    const m = montar(Object.assign(new Error('x'), { name: 'NotAllowedError' }))
    await medir(/^Medir o silêncio/)
    expect(screen.getByRole('alert').textContent).toBe('Sem acesso ao microfone. Libere nas permissões do site.')
    expect(screen.queryByRole('table')).toBeNull()
    expect(status().textContent).toBe('')
    expect(botao(/^Medir o silêncio/).getAttribute('aria-disabled')).toBeNull()
    await medir(/^Medir o silêncio/)
    expect(m.abrir).toHaveBeenCalledTimes(2)
  })

  it('sem microfone e sem suporte também têm a frase do gravador', async () => {
    montar(Object.assign(new Error('x'), { name: 'NotFoundError' }))
    await medir(/^Medir a fala/)
    expect(screen.getByRole('alert').textContent).toBe('Não achei um microfone.')
  })
})

describe('só números: nenhum arquivo, nenhum envio', () => {
  it('nenhum botão de salvar, baixar, copiar, exportar ou enviar; nenhum link, imagem, som ou tela de desenho', async () => {
    const m = montar()
    await medir(/^Medir a fala/)
    correr(m, 12_000, 0.1)
    await act(async () => {})
    const nomes = screen.getAllByRole('button').map((b) => b.textContent)
    expect(nomes).toEqual(['Medir o silêncio (6,0 s)', 'Medir a fala (12,0 s)', 'Parar agora', 'Zerar'])
    for (const proibido of ['a', 'img', 'audio', 'video', 'canvas', 'form', 'input', 'textarea', 'iframe', 'embed', 'object', 'source', 'track']) {
      expect(document.querySelector(proibido), proibido).toBeNull()
    }
    expect(document.querySelector('[download], [href]')).toBeNull()
  })

  it('toda célula de resultado tem número', async () => {
    const m = montar()
    await medir(/^Medir o silêncio/)
    correr(m, 6000, 0.01)
    await act(async () => {})
    const celulas = screen.getAllByRole('cell')
    expect(celulas.length).toBeGreaterThan(40)
    for (const c of celulas) expect(c.textContent, c.outerHTML).toMatch(/\d/)
  })

  it('medir não faz nenhum pedido de rede e não guarda nada no aparelho', async () => {
    const rede = vi.fn()
    vi.stubGlobal('fetch', rede)
    const guardou = vi.spyOn(Storage.prototype, 'setItem')
    const m = montar()
    await medir(/^Medir a fala/)
    correr(m, 12_000, 0.1)
    await act(async () => {})
    expect(rede).not.toHaveBeenCalled()
    expect(guardou).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
