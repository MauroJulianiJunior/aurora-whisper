import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { ditado, servidorFalso } from './testes/servidorFalso'

// A origem do ditado no histórico (ADR-021, C1): o que o celular criou na
// nuvem chega ao PC pelo sync com source_platform "web" (ou "android", do app
// nativo) e o histórico diz "pelo app"; o do PC, que é o comum, não diz nada.

const em = (dia: number, hora: number) => new Date(2026, 8, dia, hora, 0).toISOString()

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 28, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

// meta abre o ditado de texto e devolve a linha de detalhes do editor.
async function meta(texto: RegExp): Promise<string> {
  fireEvent.click(await screen.findByRole('button', { name: texto }))
  return document.querySelector('.meta')?.textContent ?? ''
}

describe('origem do ditado', () => {
  function montar() {
    const srv = servidorFalso([
      ditado({ id: 'c1', created_at: em(28, 15), final_text: 'Dito no celular.', source_platform: 'web' }),
      ditado({ id: 'c2', created_at: em(28, 14), final_text: 'Dito no app nativo.', source_platform: 'android' }),
      ditado({ id: 'p1', created_at: em(28, 13), final_text: 'Dito no PC.', source_platform: 'windows' }),
      ditado({ id: 'p2', created_at: em(28, 12), final_text: 'Sem origem no dublê.' }),
    ])
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  }

  it('o ditado do app diz "pelo app", entre a data e o provedor', async () => {
    montar()
    const linha = await meta(/Dito no celular/)
    expect(linha).toContain(' · pelo app · ')
    expect(linha.indexOf('pelo app')).toBeLessThan(linha.indexOf('whisper-local'))
  })

  it('o do app nativo também', async () => {
    montar()
    expect(await meta(/Dito no app nativo/)).toContain('pelo app')
  })

  it('o do PC não diz nada, nem o que veio sem a origem', async () => {
    montar()
    expect(await meta(/Dito no PC/)).not.toContain('pelo app')
    expect(await meta(/Sem origem no dublê/)).not.toContain('pelo app')
  })
})
