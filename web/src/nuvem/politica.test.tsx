// O link para a política de privacidade da instância (docs/deploy-privacidade.md):
// o app só o mostra se a nuvem confirma a página (HEAD /privacidade = 200). Sem
// ela (404), com outro código ou com a rede caída, não há link: nunca um link
// que leva a um 404.

import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Entrar } from './Telas'
import { LinkDaPolitica } from './LinkDaPolitica'
import { politicaPublicada } from './politica'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function nuvemCom(resposta: () => Promise<Response>) {
  const fetchFalso = vi.fn<typeof fetch>(() => resposta())
  vi.stubGlobal('fetch', fetchFalso)
  return fetchFalso
}

const link = () => screen.queryByRole('link', { name: 'Política de privacidade' })

describe('politicaPublicada', () => {
  it('pergunta com HEAD, na própria origem, sem cookie e sem cache', async () => {
    const f = nuvemCom(async () => new Response(null, { status: 200 }))
    expect(await politicaPublicada()).toBe(true)
    expect(f).toHaveBeenCalledTimes(1)
    const [url, init] = f.mock.calls[0]!
    expect(url).toBe('/privacidade')
    expect(init).toMatchObject({ method: 'HEAD', cache: 'no-store', credentials: 'omit' })
  })

  it.each([404, 401, 500, 204, 304])('o código %i é "não tem"', async (status) => {
    nuvemCom(async () => new Response(null, { status }))
    expect(await politicaPublicada()).toBe(false)
  })

  it('a rede caída é "não tem"', async () => {
    nuvemCom(() => Promise.reject(new TypeError('Failed to fetch')))
    expect(await politicaPublicada()).toBe(false)
  })
})

describe('LinkDaPolitica', () => {
  it('com a página: um link simples para /privacidade, numa aba nova e sem opener', async () => {
    nuvemCom(async () => new Response(null, { status: 200 }))
    render(<LinkDaPolitica />)
    const a = await screen.findByRole('link', { name: 'Política de privacidade' })
    expect(a.getAttribute('href')).toBe('/privacidade')
    expect(a.getAttribute('target')).toBe('_blank')
    expect(a.getAttribute('rel')).toContain('noopener')
  })

  it('sem a página (404): nenhum link', async () => {
    const f = nuvemCom(async () => new Response(null, { status: 404 }))
    render(<LinkDaPolitica />)
    await waitFor(() => expect(f).toHaveBeenCalled())
    await Promise.resolve()
    expect(link()).toBeNull()
  })
})

describe('a tela de entrar', () => {
  const tela = () => (
    <Entrar expirada={false} teste={null} entrarComGoogle={async () => {}} entrarComContaDeTeste={async () => {}} />
  )

  it('mostra o link quando a instância tem a política', async () => {
    nuvemCom(async () => new Response(null, { status: 200 }))
    render(tela())
    expect((await screen.findByRole('link', { name: 'Política de privacidade' })).getAttribute('href')).toBe('/privacidade')
  })

  it('não mostra o link quando não tem', async () => {
    const f = nuvemCom(async () => new Response(null, { status: 404 }))
    render(tela())
    await waitFor(() => expect(f).toHaveBeenCalled())
    await Promise.resolve()
    expect(link()).toBeNull()
  })
})
