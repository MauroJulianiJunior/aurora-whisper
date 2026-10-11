// A página de telas do painel do PC (testes/telasDev.tsx, só do pnpm dev) monta
// cada cenário do bloco Histórico (ADR-019, F10d e F10e): o que a pessoa vê em
// /telas-dev.html?cenario=historico… sem Agent. Importa a página de verdade, com
// o endereço do cenário, e olha o que ela desenhou.

import { act, cleanup, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

beforeEach(() => {
  document.body.innerHTML = '<div id="root"></div>'
})

afterEach(() => {
  cleanup()
  vi.resetModules()
  window.history.replaceState(null, '', '/')
})

async function montar(cenario: string) {
  window.history.replaceState(null, '', `/telas-dev.html?cenario=${cenario}`)
  vi.resetModules()
  await import('./testes/telasDev')
  // A página abre as Configurações sozinha (o botão da barra); espera a seção
  // Nuvem, que sempre existe, e a leitura dos 6 meses.
  await screen.findByRole('heading', { name: 'Nuvem' }, { timeout: 4000 })
  await act(async () => {})
}

const bloco = () => screen.queryByRole('region', { name: 'Histórico' })

describe('os cenários do Histórico em /telas-dev.html', () => {
  it.each([
    ['historico', '1.240 ditados têm mais de 6 meses.', ['3 deles estão na lixeira.', '2 têm duas versões, e as duas saem.', '5 são de outro dono e ficam.', 'Há 2 cópias do histórico ao lado do banco, com o texto de antes.']],
    ['historico-simples', '12 ditados têm mais de 6 meses.', []],
    ['historico-1', '1 ditado tem mais de 6 meses.', ['Ele está na lixeira.', 'Há 1 cópia do histórico ao lado do banco, com o texto de antes.']],
    ['historico-pausado', '12 ditados têm mais de 6 meses.', ['A regra foi pausada: o Agent ficou parado por mais de 7 dias, ou o relógio do PC mudou. Nada foi apagado.']],
    ['historico-ligado', 'Os 6 meses estão ligados', ['Há 1 cópia do histórico ao lado do banco, com o texto de antes.']],
    ['historico-copias', 'Há 3 cópias do histórico', []],
    ['historico-envio', 'Há 3 cópias do histórico', ['1 delas é a do envio do histórico à nuvem, que fica até o envio terminar.']],
    ['historico-so-envio', 'Há 1 cópia do histórico', ['Ela é a do envio do histórico à nuvem, que fica até o envio terminar.']],
    ['historico-de-novo', 'Não se faz outra cópia: a que já está neste PC é de antes, e o que nasceu depois dela não está nela.', ['Há 1 cópia do histórico ao lado do banco, com o texto de antes.']],
  ])('%s', async (cenario, abertura, linhas) => {
    await montar(cenario)
    const el = await waitFor(() => {
      const b = bloco()
      expect(b).not.toBeNull()
      return b!
    })
    expect(el.textContent).toContain(abertura)
    for (const l of linhas) expect(within(el).getByText(l)).toBeTruthy()
  })

  it('historico-velho: o Agent de antes da F10 diz para reiniciar', async () => {
    await montar('historico-velho')
    const el = await waitFor(() => {
      const b = bloco()
      expect(b).not.toBeNull()
      return b!
    })
    expect(within(el).getByRole('alert').textContent).toBe('Este Agent ainda não tem os 6 meses. Reinicie o Agent.')
  })

  it('um cenário de antes não mostra o bloco', async () => {
    await montar('pergunta')
    expect(bloco()).toBeNull()
  })
})
