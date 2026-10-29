// A página de telas do app da nuvem (nuvem/telasDev.tsx, só do pnpm dev) monta
// os cenários do aceite e das Configurações do ditado pelo celular (ADR-021,
// C6b): o que a pessoa vê em /telas-dev-nuvem.html?cenario=aceite… sem Docker,
// sem Google e sem servidor. Importa a página de verdade, com o endereço do
// cenário, e olha o que ela desenhou.

import { act, cleanup, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.setConfig({ testTimeout: 20_000 })

beforeEach(() => {
  document.body.innerHTML = '<div id="root"></div>'
})

afterEach(() => {
  cleanup()
  vi.resetModules()
  window.history.replaceState(null, '', '/')
})

const espera = { timeout: 8000 }

async function montar(cenario: string) {
  window.history.replaceState(null, '', `/telas-dev-nuvem.html?cenario=${cenario}`)
  vi.resetModules()
  await import('./telasDev')
}

const secao = () => screen.getByRole('region', { name: 'Ditar pelo celular' })

describe('os cenários do ditado pelo celular em /telas-dev-nuvem.html', () => {
  it('aceite: o toque no microfone abre a folha, com a escolha do provedor, e o microfone fica fechado', async () => {
    await montar('aceite')
    const folha = await screen.findByRole('dialog', undefined, espera)
    expect(within(folha).getByRole('radiogroup', { name: 'Provedor da voz' })).toBeTruthy()
    expect(within(folha).getByRole('button', { name: 'Aceito' })).toBeTruthy()
    expect(within(folha).getByRole('button', { name: 'Agora não' })).toBeTruthy()
    await act(async () => {})
    expect(screen.queryByRole('button', { name: 'Parar e enviar' })).toBeNull()
  })

  it('entrar: sem sessão, a tela Entrar com a linha da nuvem particular embaixo do botão do Google', async () => {
    await montar('entrar')
    await screen.findByRole('button', { name: 'Entrar com o Google' }, espera)
    expect(screen.getByText('Nuvem particular: só entra quem foi convidado.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Configurações' })).toBeNull()
  })

  it('ditar-desligado: as Configurações, com "Desligado." e o Ligar', async () => {
    await montar('ditar-desligado')
    await within(await screen.findByRole('region', { name: 'Ditar pelo celular' }, espera)).findByText('Desligado.', undefined, espera)
    expect(within(secao()).getByRole('button', { name: 'Ligar' })).toBeTruthy()
  })

  it('ditar-ligado: ligado, com a chave da OpenAI guardada (só o final)', async () => {
    await montar('ditar-ligado')
    await within(await screen.findByRole('region', { name: 'Ditar pelo celular' }, espera)).findByText('Chave terminada em', { exact: false }, espera)
    expect(within(secao()).getByText('Chave terminada em', { exact: false }).textContent).toBe('Chave terminada em WXYZ.')
    expect(within(secao()).getByText('Ligado desde 09/10/2026.')).toBeTruthy()
    expect(document.body.textContent).not.toContain('sk-teste-de-mentira')
  })

  it('ditar-polish: o Polish na Anthropic, e a chave dela por colar', async () => {
    await montar('ditar-polish')
    await within(await screen.findByRole('region', { name: 'Ditar pelo celular' }, espera)).findByText('Chave da Anthropic', undefined, espera)
    expect(within(secao()).getByLabelText('Chave da Anthropic')).toBeTruthy()
    expect(within(secao()).getByLabelText<HTMLSelectElement>('Provedor do Polish').value).toBe('anthropic')
  })

  it('ditar-sem-chave: os dois campos por colar, cada um com o link de onde criar a chave do provedor dele', async () => {
    await montar('ditar-sem-chave')
    await within(await screen.findByRole('region', { name: 'Ditar pelo celular' }, espera)).findByText('Chave da Anthropic', undefined, espera)
    expect(within(secao()).getByLabelText('Chave da OpenAI')).toBeTruthy()
    expect(within(secao()).getByLabelText('Chave da Anthropic')).toBeTruthy()
    expect(Array.from(secao().querySelectorAll('a'), (a) => a.getAttribute('href'))).toEqual(['https://platform.openai.com/api-keys', 'https://platform.claude.com/settings/keys'])
  })
})

describe('os cenários das notas com imagem em /telas-dev-nuvem.html (N10b)', () => {
  const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!

  beforeEach(() => {
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, writable: true, value: vi.fn(() => 'blob:dev') })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, writable: true, value: vi.fn() })
  })

  it('notas: uma, duas, cinco imagens e uma nota só com imagem', async () => {
    await montar('notas')
    await screen.findByText('Mercado N8', undefined, espera)
    expect(cartao('n1').querySelectorAll('.fotos.um .foto')).toHaveLength(1)
    expect(cartao('n2').querySelectorAll('.fotos.dois .foto')).toHaveLength(2)
    expect(cartao('n3').querySelectorAll('.fotos.tres .foto')).toHaveLength(3)
    expect(cartao('n3').querySelector('.faltam')?.textContent).toBe('+2')
    expect(cartao('n5').classList.contains('so-imagem')).toBe(true)
    expect(cartao('n4').querySelector('.fotos')).toBeNull()
    await waitFor(() => expect(cartao('n1').querySelector('.foto img')).toBeTruthy())
  })

  it('notas-sem-rede: a faixa, as imagens como "Imagem sem rede" e o ícone de imagem apagado', async () => {
    await montar('notas-sem-rede')
    await screen.findByText('Mercado N8', undefined, espera)
    await waitFor(() => expect(document.querySelector('.faixa-rede')?.textContent).toBe('Sem rede'))
    expect(within(cartao('n1')).getByText('Imagem sem rede')).toBeTruthy()
    expect(cartao('n3').querySelectorAll('.foto.sem-rede')).toHaveLength(3)
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Nova nota com imagem' }).disabled).toBe(true)
    expect(document.body.textContent).not.toContain('Imagem indisponível')
  })
})
