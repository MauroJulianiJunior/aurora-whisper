// O painel da nuvem não tem o "Virar nota" (ADR-022, N3): as notas moram no Agent
// do PC. O botão, o aviso e as rotas vêm do ambiente local; o ambiente da nuvem
// não os tem, e o editor do histórico fica como era.

import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 25, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('o painel da nuvem', () => {
  it('o editor não tem o Virar nota, e nenhuma rota de nota é chamada', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: new Date(2026, 9, 25, 14, 32).toISOString(), final_text: 'Manda o relatório amanhã cedo.' })])
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: /Manda o relatório/ }))
    expect(screen.getByRole('button', { name: 'Copiar' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Virar nota' })).toBeNull()
    expect(srv.pedidos.some((p) => p.caminho.includes('virar-nota') || p.caminho.includes('/notas'))).toBe(false)
  })
})
