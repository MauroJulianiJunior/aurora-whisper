// As medidas do ditado no celular (ADR-021): a fala mínima, a onda em dBFS, o
// tempo, e o id. Aritmética pura: cada número que a C6c vai calibrar no
// Android tem o teste do que ele promete hoje.

import { describe, expect, it } from 'vitest'
import {
  type Amostra,
  MedidorDeFala,
  barrasDaOnda,
  barrasDoNivel,
  falaMinimaMs,
  formatarTempo,
  fracaoDoNivel,
  janelaDaOndaMs,
  limiarDeFala,
  pisoDBFS,
  tetoDBFS,
  uuidV7,
} from './medidas'

const deDB = (db: number) => 10 ** (db / 20)

describe('as constantes que o servidor e o app combinam', () => {
  it('são as da ADR-021', () => {
    expect(falaMinimaMs).toBe(300)
    expect(pisoDBFS).toBe(-48)
    expect(tetoDBFS).toBe(-14)
    expect(barrasDaOnda).toBe(33)
    expect(janelaDaOndaMs).toBe(1300)
    // Parte do limiar do PC (internal/agent/audio/vad.go).
    expect(limiarDeFala).toBe(0.03)
  })
})

describe('fracaoDoNivel: o dBFS espalhado sobre o curso da barra, como o overlay do PC', () => {
  it('o piso é repouso, o teto satura, e o meio é metade', () => {
    expect(fracaoDoNivel(deDB(-48))).toBeCloseTo(0, 6)
    expect(fracaoDoNivel(deDB(-14))).toBeCloseTo(1, 6)
    expect(fracaoDoNivel(deDB(-31))).toBeCloseTo(0.5, 6)
  })

  it('abaixo do piso é 0, acima do teto é 1, e o que não é nível é 0', () => {
    expect(fracaoDoNivel(deDB(-80))).toBe(0)
    expect(fracaoDoNivel(0.9)).toBe(1)
    expect(fracaoDoNivel(1)).toBe(1)
    expect(fracaoDoNivel(5)).toBe(1)
    expect(fracaoDoNivel(0)).toBe(0)
    expect(fracaoDoNivel(-0.5)).toBe(0)
    expect(fracaoDoNivel(Number.NaN)).toBe(0)
  })

  it('a fala do PC (p50 de 0,068, -23,4 dBFS) cai a 72% do curso, como na medição da ADR-014', () => {
    // (-23,35 - (-48)) / 34
    expect(fracaoDoNivel(0.068)).toBeCloseTo(0.725, 2)
  })
})

describe('barrasDoNivel: 33 barras sobre 1,3 s, pelo pico', () => {
  const passo = janelaDaOndaMs / barrasDaOnda

  it('sem amostras é repouso', () => {
    expect(barrasDoNivel([], 5000)).toEqual(new Array(33).fill(0))
  })

  it('a amostra mais nova cai na última barra, e a mais antiga da janela na primeira', () => {
    const agora = 10_000
    const barras = barrasDoNivel(
      [
        { t: agora - janelaDaOndaMs + 1, rms: deDB(-14) },
        { t: agora, rms: deDB(-14) },
      ],
      agora,
    )
    expect(barras[0]).toBeCloseTo(1, 6)
    expect(barras[32]).toBeCloseTo(1, 6)
    expect(barras.slice(1, 32).every((b) => b === 0)).toBe(true)
  })

  it('cada barra guarda o PICO do pedaço dela, e não a média', () => {
    const agora = 10_000
    const inicio = agora - janelaDaOndaMs
    const amostras: Amostra[] = [
      { t: inicio + passo * 4 + 2, rms: deDB(-40) },
      { t: inicio + passo * 4 + 20, rms: deDB(-20) },
      { t: inicio + passo * 4 + 30, rms: deDB(-45) },
    ]
    const barras = barrasDoNivel(amostras, agora)
    expect(barras[4]).toBeCloseTo(fracaoDoNivel(deDB(-20)), 6)
    expect(barras.filter((b) => b > 0)).toHaveLength(1)
  })

  it('o que é mais velho que a janela, ou do futuro, não entra', () => {
    const agora = 10_000
    const barras = barrasDoNivel(
      [
        { t: agora - janelaDaOndaMs - 5, rms: 0.5 },
        { t: agora + 1, rms: 0.5 },
      ],
      agora,
    )
    expect(barras.every((b) => b === 0)).toBe(true)
  })
})

describe('MedidorDeFala: quanto tempo acima do limiar, sem os últimos 200 ms', () => {
  // Quadros de 16 ms, o rms de cada um dado por uma função do instante.
  function medir(duracaoMs: number, rms: (t: number) => number, fim = duracaoMs) {
    const m = new MedidorDeFala()
    for (let t = 0; t <= duracaoMs; t += 16) m.amostrar(t, rms(t))
    return m.falaMs(fim)
  }

  it('fala contínua de 1 s: uns 800 ms (o 1 s menos os últimos 200)', () => {
    const fala = medir(1000, () => 0.1)
    expect(fala).toBeGreaterThanOrEqual(780)
    expect(fala).toBeLessThanOrEqual(820)
  })

  it('silêncio não conta', () => {
    expect(medir(2000, () => 0.001)).toBe(0)
  })

  it('a fala só nos últimos 200 ms (o toque para parar) não conta', () => {
    expect(medir(1200, (t) => (t > 1000 ? 0.3 : 0))).toBe(0)
  })

  it('o limiar é inclusivo: no limiar é fala, logo abaixo não', () => {
    expect(medir(1000, () => limiarDeFala)).toBeGreaterThan(0)
    expect(medir(1000, () => limiarDeFala - 0.0001)).toBe(0)
  })

  it('350 ms de fala no começo e depois silêncio passam do mínimo; 250 ms, não', () => {
    expect(medir(1500, (t) => (t < 350 ? 0.2 : 0))).toBeGreaterThanOrEqual(falaMinimaMs)
    expect(medir(1500, (t) => (t < 250 ? 0.2 : 0))).toBeLessThan(falaMinimaMs)
  })

  it('um quadro que demora (a aba segurada) conta no máximo 100 ms de fala', () => {
    const m = new MedidorDeFala()
    m.amostrar(0, 0)
    m.amostrar(5000, 0.5)
    m.amostrar(5300, 0)
    expect(m.falaMs(5300)).toBe(100)
  })

  it('o relógio que não anda (o mesmo instante duas vezes) não soma nada', () => {
    const m = new MedidorDeFala()
    m.amostrar(100, 0.5)
    m.amostrar(100, 0.5)
    m.amostrar(100, 0.5)
    expect(m.falaMs(1000)).toBe(0)
  })
})

describe('formatarTempo: o "m:ss" ao lado da onda', () => {
  it.each([
    [0, '0:00'],
    [400, '0:00'],
    [600, '0:01'],
    [6600, '0:07'],
    [59_400, '0:59'],
    [59_600, '1:00'],
    [75_000, '1:15'],
    [120_000, '2:00'],
    [-300, '0:00'],
  ])('%d ms é %s', (ms, texto) => {
    expect(formatarTempo(ms)).toBe(texto)
  })
})

describe('uuidV7: o id do ditado', () => {
  const fixo = (n: number) => Uint8Array.from({ length: n }, (_, i) => 0xa0 + i)

  it('tem a forma de um UUID v7: versão 7, variante 10', () => {
    const id = uuidV7(1_760_000_000_000, fixo)
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
  })

  it('os 48 primeiros bits são o relógio em ms', () => {
    const agora = 1_760_000_000_123
    const id = uuidV7(agora, fixo)
    expect(Number.parseInt(id.replace(/-/g, '').slice(0, 12), 16)).toBe(agora)
  })

  it('o resto vem do sorteio, e o mesmo sorteio no mesmo instante dá o mesmo id', () => {
    expect(uuidV7(1_760_000_000_000, fixo)).toBe(uuidV7(1_760_000_000_000, fixo))
    expect(uuidV7(1_760_000_000_000, fixo)).not.toBe(uuidV7(1_760_000_000_000, (n) => new Uint8Array(n).fill(1)))
  })

  it('ids de instantes seguintes ordenam como texto', () => {
    const ids = [1_760_000_000_000, 1_760_000_000_001, 1_760_000_060_000, 1_770_000_000_000].map((t) => uuidV7(t, fixo))
    expect([...ids].sort()).toEqual(ids)
  })

  it('pede 10 bytes ao sorteio e ignora os que sobrarem', () => {
    const pedidos: number[] = []
    uuidV7(1, (n) => {
      pedidos.push(n)
      return new Uint8Array(32).fill(0xff)
    })
    expect(pedidos).toEqual([10])
  })
})
