// A loja das imagens da aba Notas (ADR-022, N10), com um cliente e uma redução de
// mentira: o que a tela mostra (as da lista, as anexadas, as tiradas), a ordem e o
// limite da anexação, as frases dos erros, o Desfazer e as URLs soltas.

import { describe, expect, it, vi } from 'vitest'
import { ErroDaApi, ForaDoAr } from '../api'
import { ErroDaImagem } from './imagens'
import { ImagensNaTela } from './imagensNaTela'
import type { ClienteDeImagens, ImagemDaNota, Nota } from './tipos'

const im = (id: string, minuto = 0): ImagemDaNota => ({
  id,
  largura: 800,
  altura: 600,
  bytes: 1000,
  created_at: new Date(Date.UTC(2026, 9, 28, 12, minuto)).toISOString(),
})

function montar(opcoes: { anexar?: (n: number) => ImagemDaNota | Error; reduzir?: (f: Blob) => Promise<never> | undefined } = {}) {
  let n = 0
  let url = 0
  const soltas: string[] = []
  const enviados: Blob[] = []
  const cliente: ClienteDeImagens = {
    anexar: vi.fn(async (_nota: string, jpeg: Blob) => {
      enviados.push(jpeg)
      n++
      const r = opcoes.anexar?.(n) ?? im(`nova-${n}`, 30 + n)
      if (r instanceof Error) throw r
      return r
    }),
    criarNotaComImagem: vi.fn(),
    ler: vi.fn(async () => new Blob(['x'], { type: 'image/jpeg' })),
    tirar: vi.fn(async () => undefined),
    paraATela: vi.fn(() => `blob:${++url}`),
    soltar: vi.fn((u: string) => void soltas.push(u)),
  }
  const reduzir = vi.fn(async (f: Blob) => {
    const recusa = opcoes.reduzir?.(f)
    if (recusa) return recusa
    return { jpeg: new Blob(['jpeg'], { type: 'image/jpeg' }), largura: 800, altura: 600 }
  })
  return { loja: new ImagensNaTela(cliente, reduzir), cliente, reduzir, soltas, enviados }
}

const nota = (imagens: ImagemDaNota[] = []): Pick<Nota, 'id' | 'imagens'> => ({ id: 'n1', imagens })
const arquivos = (k: number) => Array.from({ length: k }, (_, i) => new File([`f${i}`], `foto-${i}.jpg`, { type: 'image/jpeg' }))

describe('ImagensNaTela', () => {
  it('lê cada imagem uma vez e guarda a URL blob:; a que falha fica anotada', async () => {
    const { loja, cliente } = montar()
    const ouvinte = vi.fn()
    loja.assinar(ouvinte)
    loja.pedir('n1', 'a')
    loja.pedir('n1', 'a')
    await vi.waitFor(() => expect(loja.url('a')).toBe('blob:1'))
    loja.pedir('n1', 'a')
    expect(cliente.ler).toHaveBeenCalledTimes(1)
    expect(ouvinte).toHaveBeenCalled()

    vi.mocked(cliente.ler).mockRejectedValueOnce(new ErroDaApi(404, 'registro não encontrado'))
    loja.pedir('n1', 'b')
    await vi.waitFor(() => expect(loja.falhou('b')).toBe(true))
    loja.pedir('n1', 'b')
    expect(cliente.ler).toHaveBeenCalledTimes(2)
  })

  it('anexa um de cada vez, na ordem, com o "guardando…" no meio; a tela junta a lista e as novas', async () => {
    const { loja, cliente, reduzir } = montar()
    const n = nota([im('velha', 0)])
    let guardandoNoMeio = 0
    vi.mocked(cliente.anexar).mockImplementation(async () => {
      guardandoNoMeio = Math.max(guardandoNoMeio, loja.guardandoDe('n1').length)
      return im(`nova-${vi.mocked(cliente.anexar).mock.calls.length}`, 30 + vi.mocked(cliente.anexar).mock.calls.length)
    })
    const avisar = vi.fn()
    await expect(loja.anexar(n, arquivos(2), avisar)).resolves.toBe(2)
    expect(guardandoNoMeio).toBe(1)
    expect(reduzir).toHaveBeenCalledTimes(2)
    expect(loja.imagensDe(n).map((i) => i.id)).toEqual(['velha', 'nova-1', 'nova-2'])
    expect(loja.guardandoDe('n1')).toEqual([])
    // A cópia que subiu já está na tela: nada de pedir de novo.
    expect(loja.url('nova-1')).toBeDefined()
    loja.pedir('n1', 'nova-1')
    expect(cliente.ler).not.toHaveBeenCalled()
    expect(avisar).not.toHaveBeenCalled()
    // A lista nova (lida do Agent) com as mesmas imagens não as repete.
    expect(loja.imagensDe(nota([im('velha', 0), im('nova-1', 31), im('nova-2', 32)])).length).toBe(3)
  })

  it('dez por nota: o que passa não é reduzido nem mandado, e o aviso é o da ADR', async () => {
    const { loja, cliente } = montar()
    const n = nota(Array.from({ length: 8 }, (_, i) => im(`i${i}`, i)))
    const avisar = vi.fn()
    await expect(loja.anexar(n, arquivos(4), avisar)).resolves.toBe(2)
    expect(cliente.anexar).toHaveBeenCalledTimes(2)
    expect(avisar).toHaveBeenCalledWith('A nota já tem 10 imagens.')
  })

  it('as recusas viram as frases da ADR, e o que vem depois de uma recusa do navegador continua', async () => {
    const avisar = vi.fn()
    const naoAbre = montar({ reduzir: (f) => ((f as File).name === 'foto-0.jpg' ? Promise.reject(new ErroDaImagem('nao-abre')) : undefined) })
    await expect(naoAbre.loja.anexar(nota(), arquivos(2), avisar)).resolves.toBe(1)
    expect(avisar).toHaveBeenCalledWith('Esse arquivo não é uma imagem que o navegador abra.')

    const teto = new ErroDaApi(409, 'As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras.')
    teto.codigo = 'IMAGES_FULL'
    const cheio = montar({ anexar: () => teto })
    avisar.mockClear()
    await expect(cheio.loja.anexar(nota(), arquivos(3), avisar)).resolves.toBe(0)
    // O teto para tudo (um aviso só), e a cópia que não subiu é solta.
    expect(avisar.mock.calls).toEqual([['As imagens das notas chegaram ao limite de 400 MB. Apague algumas para anexar outras.']])
    expect(cheio.cliente.anexar).toHaveBeenCalledTimes(1)
    expect(cheio.soltas).toEqual(['blob:1'])
    expect(cheio.loja.guardandoDe('n1')).toEqual([])

    const perdeu = vi.fn()
    const semRede = montar({ anexar: () => new ForaDoAr() })
    avisar.mockClear()
    await semRede.loja.anexar(nota(), arquivos(1), avisar, perdeu)
    expect(avisar).toHaveBeenCalledWith('A rede caiu antes de a imagem subir. Tente de novo.')
    expect(perdeu).toHaveBeenCalled()
  })

  it('tirar esconde na hora; o Desfazer devolve e nada vai ao Agent', () => {
    const { loja, cliente } = montar()
    const n = nota([im('a', 0), im('b', 1)])
    const avisar = vi.fn()
    loja.tirar('n1', im('a', 0), avisar)
    expect(loja.imagensDe(n).map((i) => i.id)).toEqual(['b'])
    const [texto, desfazer, abrir, aoVencer] = avisar.mock.calls[0]!
    expect(texto).toBe('Imagem tirada da nota.')
    expect(abrir).toBeUndefined()
    desfazer()
    aoVencer()
    expect(loja.imagensDe(n).map((i) => i.id)).toEqual(['a', 'b'])
    expect(cliente.tirar).not.toHaveBeenCalled()
  })

  it('sem o Desfazer, o aviso vence e a imagem sai de vez: a URL é solta, e o 404 conta como tirada', async () => {
    const { loja, cliente, soltas } = montar()
    loja.pedir('n1', 'a')
    await vi.waitFor(() => expect(loja.url('a')).toBe('blob:1'))
    const avisar = vi.fn()
    loja.tirar('n1', im('a', 0), avisar)
    avisar.mock.calls[0]![3]()
    await vi.waitFor(() => expect(soltas).toEqual(['blob:1']))
    expect(cliente.tirar).toHaveBeenCalledWith('n1', 'a')
    // Continua escondida, mesmo que a lista da tela ainda a traga.
    expect(loja.imagensDe(nota([im('a', 0)]))).toEqual([])

    vi.mocked(cliente.tirar).mockRejectedValueOnce(new ErroDaApi(404, 'registro não encontrado'))
    loja.tirar('n1', im('b', 1), avisar)
    avisar.mock.calls[1]![3]()
    await vi.waitFor(() => expect(cliente.tirar).toHaveBeenCalledTimes(2))
    expect(avisar).toHaveBeenCalledTimes(2)
  })

  it('a falha de tirar devolve a imagem e diz por quê', async () => {
    const { loja, cliente } = montar()
    vi.mocked(cliente.tirar).mockRejectedValueOnce(new ErroDaApi(500, 'erro interno'))
    const avisar = vi.fn()
    loja.tirar('n1', im('a', 0), avisar)
    avisar.mock.calls[0]![3]()
    await vi.waitFor(() => expect(avisar).toHaveBeenCalledTimes(2))
    expect(avisar.mock.calls[1]![0]).toBe('Não consegui tirar a imagem da nota: erro interno. Ela continua lá.')
    expect(loja.imagensDe(nota([im('a', 0)])).map((i) => i.id)).toEqual(['a'])
  })

  it('fechar solta todas as URLs, e o que chega depois é jogado fora', async () => {
    const { loja, soltas, cliente } = montar()
    loja.pedir('n1', 'a')
    await vi.waitFor(() => expect(loja.url('a')).toBe('blob:1'))
    let soltar!: () => void
    vi.mocked(cliente.ler).mockImplementationOnce(() => new Promise((r) => (soltar = () => r(new Blob(['y'])))))
    loja.pedir('n1', 'b')
    loja.fechar()
    expect(soltas).toEqual(['blob:1'])
    soltar()
    await Promise.resolve()
    await Promise.resolve()
    expect(loja.url('b')).toBeUndefined()
    expect(cliente.paraATela).toHaveBeenCalledTimes(1)
  })
})
