// Os microfones das notas no celular (ADR-022, N8): o "Ditar para nota" de baixo
// da aba e o de dentro da nota aberta, com o gravador de verdade e a Plataforma
// de mentira (nada toca o microfone de verdade). O que importa: o ditado vai com
// o destino (Ditado-Nota), a nota que chegou vai para a lista com o Desfazer, e
// o texto do microfone da nota vai para o cursor.

import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ChegadaNaNota } from '../../notas/tipos'
import { notaDaNuvem } from './notasFalsas'
import type { ClienteNuvem, DitadoGravado, PedidoDeDitado } from '../cliente'
import { plataformaFalsa } from '../ditar/plataformaFalsa'
import type { Porta } from '../ditar/porta'
import { DitarNaNota, DitarParaNota } from './DitarNasNotas'
import type { NotasNaNuvem } from './notasNaNuvem'

afterEach(cleanup)
vi.setConfig({ testTimeout: 20_000 })

const ditado = {
  id: 'd1',
  raw_text: 'lista do sábado queijo presunto',
  final_text: 'Lista do sábado: queijo, presunto.',
  mode: 'smart',
  asr_provider: 'openai',
  status: 'done',
  created_at: '2026-10-27T12:00:00.000Z',
  updated_at: '2026-10-27T12:00:00.000Z',
  version: 1,
}

function montar(resposta: DitadoGravado) {
  const p = plataformaFalsa()
  const ditar = vi.fn<(pedido: PedidoDeDitado, sinal?: AbortSignal) => Promise<DitadoGravado>>(async () => resposta)
  const desfazerDitadoNaNota = vi.fn(async () => ({ nota_voltou: true }))
  const cliente = { ditar, desfazerDitadoNaNota } as unknown as ClienteNuvem
  const receber = vi.fn()
  const notas = { receber } as unknown as NotasNaNuvem
  const porta: Porta = { liberar: async () => true, reabrirAceite: async () => false, invalidar: vi.fn() }
  return { p, ditar, desfazerDitadoNaNota, cliente, notas, receber, porta }
}

async function gravarEParar(m: ReturnType<typeof montar>, iniciar: string, parar: string) {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: iniciar }))
  })
  act(() => m.p.rodar(2000, 0.2))
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: parar }))
  })
}

describe('"Ditar para nota"', () => {
  it('grava, manda com Ditado-Nota "para-nota", e a nota que recebeu vai para a lista com o Desfazer', async () => {
    const nota = notaDaNuvem({ id: 'n1', titulo: 'Lista do sábado', corpo: '- [ ] leite\n- [ ] Queijo\n- [ ] Presunto', version: 4 })
    const m = montar({ ditado, nota, destino: { nota_id: 'n1', nova: false, linhas: 2, corpo_antes: '- [ ] leite' } })
    const chegadas: ChegadaNaNota[] = []
    render(
      <DitarParaNota
        cliente={m.cliente}
        notas={m.notas}
        porta={m.porta}
        plataforma={m.p.plataforma}
        desligado={false}
        aoChegar={(c) => void chegadas.push(c)}
        avisar={vi.fn()}
        aoPerderLigacao={vi.fn()}
      />,
    )
    await gravarEParar(m, 'Ditar para nota', 'Parar e enviar')
    await waitFor(() => expect(chegadas).toHaveLength(1))
    expect(m.ditar.mock.calls[0]![0].nota).toBe('para-nota')
    expect(chegadas[0]).toEqual(expect.objectContaining({ nova: false, linhas: 2, nota: expect.objectContaining({ id: 'n1', version: 4 }) }))
    expect(m.receber).toHaveBeenCalledWith(expect.objectContaining({ id: 'n1' }))
    await chegadas[0]!.desfazer()
    expect(m.desfazerDitadoNaNota).toHaveBeenCalledWith('d1', 4, '- [ ] leite')
    // De volta ao botão.
    expect(screen.getByRole('button', { name: 'Ditar para nota' })).toBeTruthy()
  })

  it('gravando, a faixa diz "Para nota", com o Cancelar e o parar', async () => {
    const m = montar({ ditado })
    render(<DitarParaNota cliente={m.cliente} notas={m.notas} porta={m.porta} plataforma={m.p.plataforma} desligado={false} aoChegar={vi.fn()} avisar={vi.fn()} aoPerderLigacao={vi.fn()} />)
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Ditar para nota' }))
    })
    expect(screen.getByText('Para nota')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(screen.getByRole('button', { name: 'Ditar para nota' })).toBeTruthy()
    expect(m.ditar).not.toHaveBeenCalled()
  })

  it('sem rede, o botão fica apagado', () => {
    const m = montar({ ditado })
    render(<DitarParaNota cliente={m.cliente} notas={m.notas} porta={m.porta} plataforma={m.p.plataforma} desligado aoChegar={vi.fn()} avisar={vi.fn()} aoPerderLigacao={vi.fn()} />)
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Ditar para nota' }).disabled).toBe(true)
  })
})

describe('o microfone dentro da nota', () => {
  it('anota o cursor ao começar, manda com o id da nota, e o texto vai para o cursor', async () => {
    const m = montar({ ditado })
    const aoComecar = vi.fn()
    const aoTexto = vi.fn()
    render(
      <DitarNaNota
        cliente={m.cliente}
        notas={m.notas}
        porta={m.porta}
        plataforma={m.p.plataforma}
        notaId="n7"
        desligado={false}
        aoComecar={aoComecar}
        aoTexto={aoTexto}
        avisar={vi.fn()}
        aoPerderLigacao={vi.fn()}
      />,
    )
    await gravarEParar(m, 'Ditar aqui', 'Parar e escrever na nota')
    await waitFor(() => expect(aoTexto).toHaveBeenCalledWith('Lista do sábado: queijo, presunto.'))
    expect(aoComecar).toHaveBeenCalledTimes(1)
    expect(m.ditar.mock.calls[0]![0].nota).toBe('n7')
  })

  it('sem rede, apagado', () => {
    const m = montar({ ditado })
    render(
      <DitarNaNota
        cliente={m.cliente}
        notas={m.notas}
        porta={m.porta}
        plataforma={m.p.plataforma}
        notaId="n7"
        desligado
        aoComecar={vi.fn()}
        aoTexto={vi.fn()}
        avisar={vi.fn()}
        aoPerderLigacao={vi.fn()}
      />,
    )
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Ditar aqui' }).disabled).toBe(true)
  })
})
