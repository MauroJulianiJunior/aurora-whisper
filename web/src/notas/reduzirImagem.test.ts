// A redução da imagem no navegador (ADR-022, N10) com dublês das ferramentas: o
// jsdom não tem canvas nem createImageBitmap. O que se prova é a regra: o lado
// maior em até 2.000 px com a proporção, o JPEG, as qualidades e as recusas.

import { describe, expect, it, vi } from 'vitest'
import { ErroDaImagem, MAX_ORIGINAL, MAX_REDUZIDA } from './imagens'
import { type Bitmap, type FerramentasDaImagem, QUALIDADES, reduzirImagem, tamanhoReduzido } from './reduzirImagem'

interface Canvas {
  largura: number
  altura: number
}

function ferramentas(bitmap: Partial<Bitmap> | Error, tamanhos: (number | null)[] = [300_000], tipo = 'image/jpeg') {
  const close = vi.fn()
  const desenhados: Canvas[] = []
  const qualidades: number[] = []
  const f: FerramentasDaImagem<Bitmap, Canvas> = {
    abrir: vi.fn(async () => {
      if (bitmap instanceof Error) throw bitmap
      return { width: 0, height: 0, close, ...bitmap }
    }),
    desenhar: (_b, largura, altura) => {
      const c = { largura, altura }
      desenhados.push(c)
      return c
    },
    gravar: async (_c, q) => {
      qualidades.push(q)
      const tamanho = tamanhos[Math.min(qualidades.length - 1, tamanhos.length - 1)]
      return tamanho === null ? null : new Blob([new Uint8Array(tamanho ?? 0)], { type: tipo })
    },
  }
  return { f, close, desenhados, qualidades }
}

const arquivo = (tamanho = 2_000_000, tipo = 'image/jpeg') => new Blob([new Uint8Array(Math.min(tamanho, 16))], { type: tipo })

// Um Blob de mentira com o tamanho dado, sem alocar os bytes.
function grande(tamanho: number, tipo = 'image/jpeg'): Blob {
  const b = new Blob([], { type: tipo })
  Object.defineProperty(b, 'size', { value: tamanho })
  return b
}

describe('o tamanho reduzido', () => {
  it.each([
    [4000, 3000, 2000, 1500],
    [3000, 4000, 1500, 2000],
    [2000, 2000, 2000, 2000],
    [1200, 800, 1200, 800],
    [12000, 100, 2000, 17],
    [100, 30000, 7, 2000],
    [9000, 1, 2000, 1],
  ])('%ix%i vira %ix%i', (w, h, lw, lh) => {
    expect(tamanhoReduzido(w, h)).toEqual({ largura: lw, altura: lh })
  })
})

describe('reduzirImagem', () => {
  it('reduz a foto grande ao lado de 2.000 px, em JPEG, na qualidade do padrão, e solta o bitmap', async () => {
    const { f, close, desenhados, qualidades } = ferramentas({ width: 4032, height: 3024 })
    const r = await reduzirImagem(arquivo(), f)
    expect(desenhados).toEqual([{ largura: 2000, altura: 1500 }])
    expect(qualidades).toEqual([0.85])
    expect(r.jpeg.type).toBe('image/jpeg')
    expect(r).toMatchObject({ largura: 2000, altura: 1500 })
    expect(close).toHaveBeenCalledTimes(1)
  })

  it('a imagem pequena não cresce; o PNG e o print do Windows também viram JPEG', async () => {
    const { f, desenhados } = ferramentas({ width: 640, height: 480 })
    const r = await reduzirImagem(arquivo(50_000, 'image/png'), f)
    expect(desenhados).toEqual([{ largura: 640, altura: 480 }])
    expect(r.jpeg.type).toBe('image/jpeg')
  })

  it('a cópia que passa de 4 MB tenta as qualidades de baixo; nenhuma cabendo, recusa', async () => {
    const acima = MAX_REDUZIDA + 1
    const cabe = ferramentas({ width: 2000, height: 2000 }, [acima, 1_000_000])
    await expect(reduzirImagem(arquivo(), cabe.f)).resolves.toMatchObject({ largura: 2000 })
    expect(cabe.qualidades).toEqual([0.85, 0.72])

    const nunca = ferramentas({ width: 2000, height: 2000 }, [acima])
    const erro = await reduzirImagem(arquivo(), nunca.f).catch((e: unknown) => e)
    expect(erro).toBeInstanceOf(ErroDaImagem)
    expect((erro as ErroDaImagem).motivo).toBe('grande-reduzida')
    expect(nunca.qualidades).toEqual([...QUALIDADES])
  })

  it('o original acima de 10 MB é recusado antes de abrir', async () => {
    const { f } = ferramentas({ width: 10, height: 10 })
    const erro = await reduzirImagem(grande(MAX_ORIGINAL + 1), f).catch((e: unknown) => e)
    expect(erro).toBeInstanceOf(ErroDaImagem)
    expect((erro as ErroDaImagem).message).toBe('A imagem passou de 10 MB.')
    expect(f.abrir).not.toHaveBeenCalled()
    // Exatamente 10 MB passa.
    await expect(reduzirImagem(grande(MAX_ORIGINAL), f)).resolves.toBeDefined()
  })

  it.each([
    ['o arquivo que não é imagem', arquivo(100, 'application/pdf'), { width: 10, height: 10 }],
    ['o que o navegador não abre (o HEIC, o arquivo estragado)', arquivo(100, 'image/heic'), new Error('InvalidStateError')],
    ['a imagem sem tamanho', arquivo(), { width: 0, height: 0 }],
  ])('%s: "não é uma imagem que o navegador abra"', async (_, a, bitmap) => {
    const { f } = ferramentas(bitmap as Partial<Bitmap> | Error)
    const erro = await reduzirImagem(a, f).catch((e: unknown) => e)
    expect(erro).toBeInstanceOf(ErroDaImagem)
    expect((erro as ErroDaImagem).message).toBe('Esse arquivo não é uma imagem que o navegador abra.')
  })

  it('o navegador que não grava JPEG (devolve PNG, ou nada) não manda nada', async () => {
    for (const { tamanhos, tipo } of [
      { tamanhos: [1000], tipo: 'image/png' },
      { tamanhos: [null], tipo: 'image/jpeg' },
    ]) {
      const { f, close } = ferramentas({ width: 100, height: 100 }, tamanhos, tipo)
      const erro = await reduzirImagem(arquivo(), f).catch((e: unknown) => e)
      expect((erro as ErroDaImagem).motivo).toBe('nao-abre')
      expect(close).toHaveBeenCalled()
    }
  })

  it('o arquivo sem tipo (a extensão que o Windows não conhece) é aberto: quem decide é o navegador', async () => {
    const { f } = ferramentas({ width: 30, height: 20 })
    await expect(reduzirImagem(arquivo(100, ''), f)).resolves.toMatchObject({ largura: 30, altura: 20 })
  })
})
