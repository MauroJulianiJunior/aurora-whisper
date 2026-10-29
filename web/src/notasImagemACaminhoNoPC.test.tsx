// A imagem "a caminho" no painel do PC (ADR-022, N10b): a nota que veio da nuvem com
// uma imagem que o Agent ainda não baixou (o `estado: "a_caminho"` da API local, e
// os bytes dela são 409 IMAGE_PENDING). O painel mostra o lugar dela, quieto, com a
// proporção da imagem, sem erro, e a carrega quando ela chega (a aba Notas relê
// quando o sync traz algo, N8). O × continua tirando a imagem.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { imagem, nota, servidorFalso } from './testes/servidorFalso'

let urls = 0

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
  window.localStorage.clear()
  urls = 0
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, writable: true, value: vi.fn(() => `blob:aw-${++urls}`) })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, writable: true, value: vi.fn() })
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

const passar = (ms: number) => act(async () => void (await vi.advanceTimersByTimeAsync(ms)))
const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent ?? ''
const leiturasDaImagem = (srv: { pedidos: { caminho: string; metodo: string }[] }, id: string) =>
  srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho.endsWith(`/imagens/${id}`))

async function montar() {
  const srv = servidorFalso([], {
    notas: [
      nota({
        id: 'n1',
        titulo: 'Veio do celular',
        corpo: 'foto do boleto',
        imagens: [imagem({ id: 'a1', largura: 1600, altura: 900, estado: 'a_caminho' }), imagem({ id: 'a2', largura: 800, altura: 600 })],
      }),
    ],
  })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await screen.findByText('Veio do celular')
  await act(async () => {})
  return srv
}

describe('a imagem a caminho', () => {
  it('no cartão: o lugar dela, quieto, com a proporção da imagem, sem pedir os bytes e sem erro', async () => {
    const srv = await montar()
    const grade = cartao('n1').querySelector('.fotos.dois')!
    const vazia = grade.querySelector<HTMLElement>('.foto.a-caminho')!
    expect(vazia).toBeTruthy()
    expect(within(vazia).getByText('Imagem a caminho')).toBeTruthy()
    // A que já está aqui carrega; a que está a caminho nem é pedida.
    await waitFor(() => expect(grade.querySelector('.foto:not(.a-caminho) img')?.getAttribute('src')).toMatch(/^blob:aw-/))
    expect(leiturasDaImagem(srv, 'a1')).toHaveLength(0)
    expect(leiturasDaImagem(srv, 'a2')).toHaveLength(1)
    expect(document.body.textContent).not.toContain('Imagem indisponível')
    expect(aviso()).toBe('')
  })

  it('numa nota só com a imagem a caminho, o cartão tem a proporção dela', async () => {
    const srv = servidorFalso([], { notas: [nota({ id: 'n2', imagens: [imagem({ id: 'b1', largura: 1600, altura: 900, estado: 'a_caminho' })] })] })
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
    await waitFor(() => expect(cartao('n2')).toBeTruthy())
    const foto = cartao('n2').querySelector<HTMLElement>('.foto.a-caminho')!
    expect(foto.style.aspectRatio).toMatch(/^1\.77/)
    expect(within(foto).getByText('Imagem a caminho')).toBeTruthy()
  })

  it('na nota aberta e na imagem grande, o mesmo lugar; o × tira a imagem a caminho', async () => {
    const srv = await montar()
    fireEvent.click(cartao('n1').querySelector('[data-abrir]')!)
    const d = await screen.findByRole('dialog', { name: /^Nota:/ })
    expect(d.querySelector('.foto.a-caminho')).toBeTruthy()
    fireEvent.click(within(d).getByRole('button', { name: 'Abrir a imagem 1 de 2' }))
    const luz = screen.getByRole('dialog', { name: 'Imagem 1 de 2' })
    expect(within(luz).getByText('Imagem a caminho')).toBeTruthy()
    // Baixar não tem o que baixar ainda.
    expect(within(luz).getByRole<HTMLButtonElement>('button', { name: 'Baixar' }).disabled).toBe(true)
    fireEvent.keyDown(luz, { key: 'Escape' })

    fireEvent.click(within(d).getByRole('button', { name: 'Tirar a imagem 1 da nota' }))
    expect(aviso()).toContain('Imagem tirada da nota.')
    expect(d.querySelector('.foto.a-caminho')).toBeNull()
    window.dispatchEvent(new Event('pagehide'))
    await waitFor(() => expect(srv.pedidos.filter((p) => p.metodo === 'DELETE' && p.caminho === '/api/notas/n1/imagens/a1')).toHaveLength(1))
  })

  it('quando a imagem chega (o sync traz a lista nova, sem o estado), o painel a carrega sozinho', async () => {
    const srv = await montar()
    expect(leiturasDaImagem(srv, 'a1')).toHaveLength(0)
    srv.notaPorFora('n1', { imagens: [imagem({ id: 'a1', largura: 1600, altura: 900 }), imagem({ id: 'a2', largura: 800, altura: 600 })] })
    srv.mudancasPorFora()
    await passar(3000)
    await waitFor(() => expect(cartao('n1').querySelectorAll('.foto.a-caminho')).toHaveLength(0))
    await waitFor(() => expect(cartao('n1').querySelectorAll('.foto img')).toHaveLength(2))
    expect(leiturasDaImagem(srv, 'a1')).toHaveLength(1)
    expect(document.body.textContent).not.toContain('Imagem a caminho')
  })

  it('o 409 IMAGE_PENDING do Agent não vira "Imagem indisponível" (a lista já diz que está a caminho)', async () => {
    // Uma imagem listada como presente, mas que o Agent já não tem os bytes (a corrida do recomeço).
    const srv = servidorFalso([], { notas: [nota({ id: 'n3', titulo: 'Corrida', imagens: [imagem({ id: 'c1' })] })] })
    srv.interceptar((p) => (p.caminho === '/api/notas/n3/imagens/c1' && p.metodo === 'GET' ? new Response(JSON.stringify({ error: { code: 'IMAGE_PENDING', message: 'a caminho' } }), { status: 409, headers: { 'Content-Type': 'application/json' } }) : undefined))
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
    await screen.findByText('Corrida')
    await act(async () => {})
    expect(leiturasDaImagem(srv, 'c1').length).toBeGreaterThan(0)
    expect(document.body.textContent).not.toContain('Imagem indisponível')
  })
})
