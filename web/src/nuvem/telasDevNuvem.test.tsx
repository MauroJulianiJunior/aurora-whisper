// A página de telas do app da nuvem (nuvem/telasDev.tsx, só do pnpm dev) monta
// os cenários do aceite e das Configurações do ditado pelo celular (ADR-021,
// C6b): o que a pessoa vê em /telas-dev-nuvem.html?cenario=aceite… sem Docker,
// sem Google e sem servidor. Importa a página de verdade, com o endereço do
// cenário, e olha o que ela desenhou.

import { act, cleanup, screen, within } from '@testing-library/react'
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
})
