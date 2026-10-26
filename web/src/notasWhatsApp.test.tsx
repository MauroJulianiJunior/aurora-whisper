// "Copiar para WhatsApp" (ADR-022, N6): no "Mais" da nota aberta e no do cartão.
// A área de transferência e o compartilhar são de mentira (vi.fn): este teste
// nunca toca a do sistema.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { nota, servidorFalso } from './testes/servidorFalso'

const compras = () =>
  nota({
    id: 'n1',
    titulo: 'Compras',
    corpo: '# Mercado\n- [ ] leite\n\nlevar *sacola*\n- [x] pão',
    updated_at: new Date(2026, 9, 25, 9).toISOString(),
  })
// Os marcados ficam no fim do corpo (ADR-022), na nota aberta e no cartão.
const esperado = '*Compras*\n\n*Mercado*\n☐ leite\n\nlevar *sacola*\n✅ pão'

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  delete (window as { matchMedia?: unknown }).matchMedia
  Reflect.deleteProperty(navigator, 'clipboard')
  Reflect.deleteProperty(navigator, 'share')
})

const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent

function telaEstreita() {
  window.matchMedia = vi.fn().mockImplementation((consulta: string) => ({
    matches: consulta.includes('max-width: 640px'),
    media: consulta,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
}

function areaDeTransferencia(writeText = vi.fn().mockResolvedValue(undefined)) {
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  return writeText
}

function compartilhar(share = vi.fn().mockResolvedValue(undefined)) {
  Object.defineProperty(navigator, 'share', { value: share, configurable: true })
  return share
}

async function abrirADoCartao() {
  const srv = servidorFalso([], { notas: [compras()] })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await screen.findByText('Compras', { selector: 'h3' })
  await act(async () => {})
}

const abrirNota = () => fireEvent.click(document.querySelector('[data-nota="n1"] [data-abrir]')!)
const dialogo = () => screen.getByRole('dialog')

describe('Copiar para WhatsApp', () => {
  it('na nota aberta, escreve o texto de WhatsApp só dentro do clique, e avisa', async () => {
    const escrever = areaDeTransferencia()
    await abrirADoCartao()
    abrirNota()
    expect(escrever).not.toHaveBeenCalled()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(escrever).toHaveBeenCalledWith(esperado)
    await waitFor(() => expect(aviso()).toContain('Copiado para o WhatsApp.'))
    // O menu fecha, como em toda ação dele.
    expect(within(dialogo()).queryByRole('button', { name: 'Copiar para WhatsApp' })).toBeNull()
  })

  it('na nota aberta, copia o que está na janela, mesmo antes de salvar', async () => {
    const escrever = areaDeTransferencia()
    await abrirADoCartao()
    abrirNota()
    fireEvent.change(within(dialogo()).getByRole('textbox', { name: 'Título' }), { target: { value: 'Feira' } })
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(escrever.mock.calls[0]![0]).toMatch(/^\*Feira\*\n\n/)
  })

  it('no cartão, sem abrir a nota', async () => {
    const escrever = areaDeTransferencia()
    await abrirADoCartao()
    const cartao = document.querySelector<HTMLElement>('[data-nota="n1"]')!
    fireEvent.click(within(cartao).getByRole('button', { name: /Mais opções da nota/ }))
    fireEvent.click(within(cartao).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(escrever).toHaveBeenCalledWith(esperado)
    expect(screen.queryByRole('dialog')).toBeNull()
    await waitFor(() => expect(aviso()).toContain('Copiado para o WhatsApp.'))
  })

  it('se a cópia falhar, o aviso diz o que fazer', async () => {
    areaDeTransferencia(vi.fn().mockRejectedValue(new Error('negado')))
    await abrirADoCartao()
    abrirNota()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Copiar para WhatsApp' }))
    await waitFor(() => expect(aviso()).toContain('Não consegui copiar. Selecione o texto e use Ctrl+C.'))
  })

  it('sem área de transferência no navegador, o aviso é o mesmo', async () => {
    await abrirADoCartao()
    const cartao = document.querySelector<HTMLElement>('[data-nota="n1"]')!
    fireEvent.click(within(cartao).getByRole('button', { name: /Mais opções da nota/ }))
    fireEvent.click(within(cartao).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(aviso()).toContain('Não consegui copiar. Abra a nota e use Ctrl+C.')
  })

  it('numa tela larga o compartilhar do sistema não é usado, mesmo existindo', async () => {
    const escrever = areaDeTransferencia()
    const share = compartilhar()
    await abrirADoCartao()
    abrirNota()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(share).not.toHaveBeenCalled()
    expect(escrever).toHaveBeenCalledWith(esperado)
  })

  it('no celular (tela estreita com compartilhar), abre a folha de compartilhar e não escreve na área', async () => {
    telaEstreita()
    const escrever = areaDeTransferencia()
    const share = compartilhar()
    await abrirADoCartao()
    abrirNota()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(share).toHaveBeenCalledWith({ text: esperado })
    await act(async () => {})
    expect(escrever).not.toHaveBeenCalled()
  })

  it('no celular, fechar a folha sem escolher não é erro nem cópia', async () => {
    telaEstreita()
    const escrever = areaDeTransferencia()
    compartilhar(vi.fn().mockRejectedValue(new DOMException('fechou', 'AbortError')))
    await abrirADoCartao()
    const cartao = document.querySelector<HTMLElement>('[data-nota="n1"]')!
    fireEvent.click(within(cartao).getByRole('button', { name: /Mais opções da nota/ }))
    fireEvent.click(within(cartao).getByRole('button', { name: 'Copiar para WhatsApp' }))
    await act(async () => {})
    expect(escrever).not.toHaveBeenCalled()
    expect(aviso() ?? '').toBe('')
  })

  it('no celular, se o compartilhar falhar, cai para a área de transferência', async () => {
    telaEstreita()
    const escrever = areaDeTransferencia()
    compartilhar(vi.fn().mockRejectedValue(new DOMException('sem folha', 'NotAllowedError')))
    await abrirADoCartao()
    const cartao = document.querySelector<HTMLElement>('[data-nota="n1"]')!
    fireEvent.click(within(cartao).getByRole('button', { name: /Mais opções da nota/ }))
    fireEvent.click(within(cartao).getByRole('button', { name: 'Copiar para WhatsApp' }))
    await waitFor(() => expect(escrever).toHaveBeenCalledWith(esperado))
    await waitFor(() => expect(aviso()).toContain('Copiado para o WhatsApp.'))
  })

  it('no celular sem compartilhar no navegador, vai para a área de transferência', async () => {
    telaEstreita()
    const escrever = areaDeTransferencia()
    await abrirADoCartao()
    abrirNota()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(escrever).toHaveBeenCalledWith(esperado)
  })

  it('nota vazia: nada é copiado, e o aviso diz por quê', async () => {
    const escrever = areaDeTransferencia()
    const srv = servidorFalso([], { notas: [nota({ id: 'n9', titulo: '', corpo: '- \n', updated_at: new Date(2026, 9, 25, 9).toISOString() })] })
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
    const cartao = await waitFor(() => {
      const c = document.querySelector<HTMLElement>('[data-nota="n9"]')
      expect(c).toBeTruthy()
      return c!
    })
    fireEvent.click(within(cartao).getByRole('button', { name: /Mais opções da nota/ }))
    fireEvent.click(within(cartao).getByRole('button', { name: 'Copiar para WhatsApp' }))
    expect(escrever).not.toHaveBeenCalled()
    expect(aviso()).toContain('A nota está vazia')
  })
})
