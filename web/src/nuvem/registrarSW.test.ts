import { afterEach, describe, expect, it, vi } from 'vitest'
import { registrarSW } from './registrarSW'

// O registro do service worker (ADR-019, F5): só em produção, só no load, com o
// escopo / e sem o cache HTTP no sw.js; um erro no registro não escapa.

type JanelaDeMentira = Window & { carregar(): void; ouvintesDoLoad(): number }

// O carregar chama os ouvintes do load direto, e não por um dispatchEvent: no
// jsdom, a exceção lançada dentro de um ouvinte só é relatada (pelo console
// virtual) e não escapa do dispatchEvent, e o teste não a veria.
function janelaDeMentira(register: (...a: unknown[]) => Promise<unknown>, comSW = true): JanelaDeMentira {
  const doLoad: Array<() => void> = []
  const navigator = comSW ? { serviceWorker: { register } } : {}
  return {
    navigator,
    addEventListener: (tipo: string, ouvinte: () => void) => {
      if (tipo === 'load') doLoad.push(ouvinte)
    },
    carregar: () => doLoad.forEach((ouvinte) => ouvinte()),
    ouvintesDoLoad: () => doLoad.length,
  } as unknown as JanelaDeMentira
}

afterEach(() => {
  vi.unstubAllEnvs()
})

describe('registrarSW', () => {
  it('em produção, registra o /sw.js no load, com o escopo / e updateViaCache none', () => {
    vi.stubEnv('PROD', true)
    const register = vi.fn(() => Promise.resolve({}))
    const janela = janelaDeMentira(register)
    registrarSW(janela)
    expect(register).not.toHaveBeenCalled()
    janela.carregar()
    expect(register).toHaveBeenCalledTimes(1)
    expect(register).toHaveBeenCalledWith('/sw.js', { scope: '/', updateViaCache: 'none' })
  })

  it('fora de produção (o servidor de desenvolvimento), não registra', () => {
    vi.stubEnv('PROD', false)
    const register = vi.fn(() => Promise.resolve({}))
    const janela = janelaDeMentira(register)
    registrarSW(janela)
    janela.carregar()
    expect(register).not.toHaveBeenCalled()
  })

  it('sem service worker no navegador, não faz nada', () => {
    vi.stubEnv('PROD', true)
    const register = vi.fn(() => Promise.resolve({}))
    const janela = janelaDeMentira(register, false)
    registrarSW(janela)
    // Nem espera o load: sem a conferência, o ouvinte iria ler o register de
    // um serviceWorker que não existe e soltaria um TypeError no load.
    expect(janela.ouvintesDoLoad()).toBe(0)
    expect(() => janela.carregar()).not.toThrow()
    expect(register).not.toHaveBeenCalled()
  })

  // Uma rejeição não tratada faz o vitest falhar a rodada inteira ("Unhandled
  // Rejection"): é isso que este teste prova. O register aqui é uma função
  // comum, e não um vi.fn: o vi.fn acompanha a promessa que devolve (o
  // settledResults) e, com isso, a trata no lugar do código.
  it('o erro do registro não escapa: o app segue sem o service worker', async () => {
    vi.stubEnv('PROD', true)
    let chamadas = 0
    const register = () => {
      chamadas++
      return Promise.reject(new Error('registro recusado de propósito'))
    }
    const janela = janelaDeMentira(register)
    registrarSW(janela)
    janela.carregar()
    expect(chamadas).toBe(1)
    await new Promise((r) => setTimeout(r, 0))
  })
})
