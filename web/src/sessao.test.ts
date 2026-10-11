import { describe, expect, it, vi } from 'vitest'
import { ForaDoAr, SemSessao } from './api'
import { lerBilhete, trocarBilhete } from './sessao'
import { servidorFalso } from './testes/servidorFalso'

function historicoFalso() {
  return { state: null, replaceState: vi.fn() }
}

describe('lerBilhete', () => {
  it('devolve o bilhete e o apaga da URL, sem mexer no resto', () => {
    const h = historicoFalso()
    const b = lerBilhete({ search: '?x=1&t=abc123&y=2', pathname: '/', hash: '#topo' }, h)
    expect(b).toBe('abc123')
    expect(h.replaceState).toHaveBeenCalledWith(null, '', '/?x=1&y=2#topo')
  })

  it('só com o bilhete, a URL volta a ser a raiz limpa', () => {
    const h = historicoFalso()
    lerBilhete({ search: '?t=abc123', pathname: '/', hash: '' }, h)
    expect(h.replaceState).toHaveBeenCalledWith(null, '', '/')
  })

  it('sem bilhete não toca no histórico do navegador', () => {
    const h = historicoFalso()
    expect(lerBilhete({ search: '', pathname: '/', hash: '' }, h)).toBeNull()
    expect(h.replaceState).not.toHaveBeenCalled()
  })
})

describe('trocarBilhete', () => {
  it('bilhete válido vira o token', async () => {
    const srv = servidorFalso([], { token: 'tok-1' })
    await expect(trocarBilhete('bilhete-bom', srv.buscar)).resolves.toBe('tok-1')
    expect(srv.pedidos[0]).toMatchObject({ metodo: 'POST', caminho: '/api/sessao', corpo: { bilhete: 'bilhete-bom' } })
  })

  it('bilhete gasto, vencido ou inventado: sem sessão', async () => {
    const srv = servidorFalso([])
    await expect(trocarBilhete('gasto', srv.buscar)).rejects.toBeInstanceOf(SemSessao)
  })

  it('corpo recusado (400) também é sem sessão, não um erro técnico', async () => {
    const srv = servidorFalso([])
    srv.interceptar(() => new Response('corpo inválido', { status: 400 }))
    await expect(trocarBilhete('x', srv.buscar)).rejects.toBeInstanceOf(SemSessao)
  })

  it('Agent fechado: fora do ar', async () => {
    const srv = servidorFalso([])
    srv.derrubar(true)
    await expect(trocarBilhete('bilhete-bom', srv.buscar)).rejects.toBeInstanceOf(ForaDoAr)
  })
})
