import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Aviso, type AvisoDados } from './Aviso'
import { tempoDoAviso } from './tempoDoAviso'

const comDesfazer = (id: number): AvisoDados => ({ id, texto: `Aviso ${id}.`, desfazer: vi.fn() })
const comSugestao = (id: number): AvisoDados => ({ id, texto: `Sugestão ${id}.`, aprender: vi.fn(), efeito: 'Vale para todo ditado.' })
const desfazer = () => screen.getByRole('button', { name: 'Desfazer' })
const aprender = () => screen.getByRole('button', { name: 'Aprender' })

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('Aviso', () => {
  it('diz o atalho do Desfazer', () => {
    render(<Aviso aviso={comDesfazer(1)} aoSumir={() => {}} />)
    expect(screen.getByText('Ctrl+Z')).toBeTruthy()
    expect(desfazer().getAttribute('aria-keyshortcuts')).toBe('Control+Z')
  })

  it('a pausa vale enquanto houver foco OU ponteiro: o ponteiro sair não solta a pausa do foco', () => {
    const aoSumir = vi.fn()
    render(<Aviso aviso={comDesfazer(1)} aoSumir={aoSumir} />)
    const pilula = desfazer().closest('.aviso')!

    act(() => desfazer().focus())
    fireEvent.mouseEnter(pilula)
    fireEvent.mouseLeave(pilula)
    act(() => vi.advanceTimersByTime(20_000))
    expect(aoSumir).not.toHaveBeenCalled()

    act(() => desfazer().blur())
    act(() => vi.advanceTimersByTime(8_000))
    expect(aoSumir).toHaveBeenCalledTimes(1)
  })

  it('aviso novo herda o foco do que saiu; um aviso que chega depois não rouba o foco', () => {
    const { rerender } = render(<Aviso aviso={comDesfazer(1)} aoSumir={() => {}} />)
    act(() => desfazer().focus())

    rerender(<Aviso aviso={comDesfazer(2)} aoSumir={() => {}} />)
    expect(screen.getByText('Aviso 2.')).toBeTruthy()
    expect(document.activeElement).toBe(desfazer())

    // A pílula sai com o foco dentro, e nenhuma entra no lugar dela.
    rerender(<Aviso aviso={null} aoSumir={() => {}} />)
    rerender(<Aviso aviso={comDesfazer(3)} aoSumir={() => {}} />)
    expect(screen.getByText('Aviso 3.')).toBeTruthy()
    expect(document.activeElement).not.toBe(desfazer())
  })

  it('focoNoTexto: o aviso novo herda o foco no TEXTO, e a ação fica a um Tab, sem foco', () => {
    const { rerender } = render(<Aviso aviso={comSugestao(1)} aoSumir={() => {}} />)
    act(() => aprender().focus())

    rerender(<Aviso aviso={{ ...comDesfazer(2), focoNoTexto: true }} aoSumir={() => {}} />)
    const texto = screen.getByText('Aviso 2.')
    expect(document.activeElement).toBe(texto)
    expect(texto.getAttribute('tabindex')).toBe('-1')
    expect(document.activeElement).not.toBe(desfazer())
    // A pílula continua esperando por quem está nela (o foco é dela).
    const aoSumir = vi.fn()
    rerender(<Aviso aviso={{ ...comDesfazer(2), focoNoTexto: true }} aoSumir={aoSumir} />)
    act(() => vi.advanceTimersByTime(20_000))
    expect(aoSumir).not.toHaveBeenCalled()
  })

  it('sem focoNoTexto o texto não é focável, e sem foco na pílula que saiu nada é puxado', () => {
    const { rerender } = render(<Aviso aviso={comSugestao(1)} aoSumir={() => {}} />)
    // Sem o foco na pílula, mesmo com focoNoTexto: o aviso novo não rouba o foco.
    rerender(<Aviso aviso={{ ...comDesfazer(2), focoNoTexto: true }} aoSumir={() => {}} />)
    expect(document.activeElement).toBe(document.body)
    rerender(<Aviso aviso={comDesfazer(3)} aoSumir={() => {}} />)
    expect(screen.getByText('Aviso 3.').hasAttribute('tabindex')).toBe(false)
  })

  describe('quanto o aviso espera', () => {
    // O aviso que só informa some logo; com ação espera mais; com a sugestão
    // de aprender espera bem mais, porque quem usa o teclado ou um leitor de
    // tela precisa chegar nele (ADR-017).
    it.each([
      ['só informa', { id: 1, texto: 'Copiado.' } as AvisoDados, tempoDoAviso.simples],
      ['com Desfazer', comDesfazer(1), tempoDoAviso.comDesfazer],
      ['com Aprender', comSugestao(1), tempoDoAviso.comSugestao],
    ])('%s: some depois de %i ms, e não antes', (_, dados, ms) => {
      const aoSumir = vi.fn()
      render(<Aviso aviso={dados} aoSumir={aoSumir} />)
      act(() => vi.advanceTimersByTime(ms - 1))
      expect(aoSumir).not.toHaveBeenCalled()
      act(() => vi.advanceTimersByTime(1))
      expect(aoSumir).toHaveBeenCalledTimes(1)
    })

    it('a sugestão espera mais que o Desfazer, e o Desfazer mais que o aviso simples', () => {
      expect(tempoDoAviso.simples).toBeLessThan(tempoDoAviso.comDesfazer)
      expect(tempoDoAviso.comDesfazer).toBeLessThan(tempoDoAviso.comSugestao)
      expect(tempoDoAviso.comSugestao).toBeGreaterThanOrEqual(20_000)
    })

    it('com o foco no Aprender, a sugestão não some sozinha, por mais que demore; ao soltar o foco, conta de novo', () => {
      const aoSumir = vi.fn()
      render(<Aviso aviso={comSugestao(1)} aoSumir={aoSumir} />)
      act(() => aprender().focus())
      act(() => vi.advanceTimersByTime(10 * 60_000))
      expect(aoSumir).not.toHaveBeenCalled()

      act(() => aprender().blur())
      act(() => vi.advanceTimersByTime(tempoDoAviso.comSugestao))
      expect(aoSumir).toHaveBeenCalledTimes(1)
    })

    it('com o ponteiro em cima, também espera', () => {
      const aoSumir = vi.fn()
      render(<Aviso aviso={comSugestao(1)} aoSumir={aoSumir} />)
      const pilula = aprender().closest('.aviso')!
      fireEvent.mouseEnter(pilula)
      act(() => vi.advanceTimersByTime(10 * 60_000))
      expect(aoSumir).not.toHaveBeenCalled()
      fireEvent.mouseLeave(pilula)
      act(() => vi.advanceTimersByTime(tempoDoAviso.comSugestao))
      expect(aoSumir).toHaveBeenCalledTimes(1)
    })
  })

  describe('o botão Aprender', () => {
    it('diz o atalho e o efeito, e o clique chama aprender sem tirar a pílula', () => {
      const dados = comSugestao(1)
      const aoSumir = vi.fn()
      render(<Aviso aviso={dados} aoSumir={aoSumir} />)

      expect(screen.getByText('Alt+A')).toBeTruthy()
      expect(aprender().getAttribute('aria-keyshortcuts')).toBe('Alt+A')
      expect(aprender().getAttribute('title')).toBe('Vale para todo ditado.')
      expect(screen.queryByRole('button', { name: 'Desfazer' })).toBeNull()

      fireEvent.click(aprender())
      expect(dados.aprender).toHaveBeenCalledTimes(1)
      // Quem troca a pílula pelo resultado é quem chamou, quando a resposta chega.
      expect(aoSumir).not.toHaveBeenCalled()
    })

    it('o aviso que só informa não tem o botão', () => {
      render(<Aviso aviso={{ id: 1, texto: 'Copiado.' }} aoSumir={() => {}} />)
      expect(screen.queryByRole('button')).toBeNull()
      expect(screen.queryByText('Alt+A')).toBeNull()
    })
  })
})
