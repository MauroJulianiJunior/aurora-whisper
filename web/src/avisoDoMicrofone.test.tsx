import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { Painel } from './Painel'
import { ambienteLocal } from './ambienteLocal'
import { criarCliente } from './clienteLocal'
import { servidorFalso, ditado } from './testes/servidorFalso'

// O aviso de microfone bloqueado pelo Windows (internal/agent/localapi/
// microfone.go): aparece só bloqueado, diz o que fazer pelo motivo, tem o botão
// que abre a tela do Windows (menos quando é a empresa) e some sozinho quando a
// pessoa liga a chave e volta à aba. Contra o Agent de mentira.

const base = () => [ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda para o Thiago.' })]

const titulo = 'O Windows está bloqueando o microfone.'
const botao = 'Abrir as configurações do microfone'
const caminhoManual = 'Abra o menu Iniciar > Configurações > Privacidade e segurança > Microfone.'

function montar(depoisDeCriar?: (srv: ReturnType<typeof servidorFalso>) => void) {
  const srv = servidorFalso(base())
  depoisDeCriar?.(srv)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  return srv
}

const consultas = (srv: ReturnType<typeof servidorFalso>) => srv.pedidos.filter((p) => p.caminho === '/api/microfone')
const aberturas = (srv: ReturnType<typeof servidorFalso>) => srv.pedidos.filter((p) => p.caminho === '/api/microfone/configuracoes')
const aviso = () => screen.queryByText(titulo)?.closest('.faixa') ?? null
const pilula = () => document.querySelector<HTMLElement>('.aviso-regiao')

// voltarParaAAba é o que a pessoa faz depois de ligar a chave no Windows.
const voltarParaAAba = () => act(async () => void window.dispatchEvent(new Event('focus')))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('o aviso de microfone bloqueado', () => {
  it('liberado: não aparece nada, e o painel pergunta uma vez ao montar', async () => {
    const srv = montar()
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    await waitFor(() => expect(consultas(srv)).toHaveLength(1))
    expect(aviso()).toBeNull()
    expect(consultas(srv)[0]?.auth).toBe('Bearer tok')
  })

  it('"não deu para ler" não é bloqueio: nada aparece', async () => {
    const srv = montar((s) => s.microfoneAgora(false, 'desconhecido'))
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    await waitFor(() => expect(consultas(srv)).toHaveLength(1))
    expect(aviso()).toBeNull()
  })

  it.each([
    ['conta', /ligue a chave Acesso ao microfone e volte para esta aba\./],
    ['aparelho', /ligue a chave Acesso ao microfone e volte para esta aba\./],
    ['programas', /ligue a chave Permitir que aplicativos da área de trabalho acessem o microfone e volte para esta aba\./],
    ['este_app', /procure o Aurora Whisper na lista, ligue a chave dele e volte para esta aba\./],
    // Uma palavra que o painel não conhece (um Agent mais novo) cai no texto do aparelho.
    ['palavra_nova', /ligue a chave Acesso ao microfone/],
  ])('bloqueado por %s: título, o que fazer e o botão', async (motivo, texto) => {
    montar((s) => s.microfoneAgora(true, motivo))
    const faixa = (await screen.findByText(titulo)).closest('.faixa') as HTMLElement
    expect(faixa.textContent).toContain('Sem ele, o ditado não ouve nada. Clique no botão')
    expect(faixa.textContent).toMatch(texto)
    expect(faixa.getAttribute('role')).toBe('status')
    expect(faixa.querySelector('button')?.textContent).toBe(botao)
  })

  it('empresa: diz que só quem administra libera, e não tem botão', async () => {
    montar((s) => s.microfoneAgora(true, 'empresa'))
    const faixa = (await screen.findByText(titulo)).closest('.faixa') as HTMLElement
    expect(faixa.textContent).toContain('Quem administra este computador bloqueou o microfone. Só essa pessoa consegue liberar.')
    expect(faixa.querySelector('button')).toBeNull()
  })

  it('o botão pede ao Agent que abra a tela do Windows, por POST, com o token', async () => {
    const srv = montar((s) => s.microfoneAgora(true, 'conta'))
    fireEvent.click(await screen.findByRole('button', { name: botao }))
    await waitFor(() => expect(aberturas(srv)).toHaveLength(1))
    const [p] = aberturas(srv)
    expect(p?.metodo).toBe('POST')
    expect(p?.auth).toBe('Bearer tok')
    // Abriu: o aviso fica (quem some com ele é a chave ligada) e nenhuma pílula de erro.
    expect(aviso()).not.toBeNull()
    expect(pilula()?.textContent ?? '').not.toContain('Não consegui')
  })

  it('se o Agent não consegue abrir, a pílula dá o caminho à mão', async () => {
    const srv = montar((s) => {
      s.microfoneAgora(true, 'conta')
      s.abrirConfiguracoesFalha()
    })
    fireEvent.click(await screen.findByRole('button', { name: botao }))
    await waitFor(() => expect(pilula()?.textContent).toContain(caminhoManual))
    expect(aberturas(srv)).toHaveLength(1)
  })

  it('some sozinho quando a pessoa liga a chave e volta à aba, e a pílula diz que liberou', async () => {
    const srv = montar((s) => s.microfoneAgora(true, 'conta'))
    await screen.findByText(titulo)
    srv.microfoneAgora(false)

    await voltarParaAAba()

    await waitFor(() => expect(aviso()).toBeNull())
    expect(pilula()?.textContent).toContain('Microfone liberado. Pode ditar.')
  })

  it('também volta a consultar quando a aba fica visível', async () => {
    const srv = montar((s) => s.microfoneAgora(true, 'conta'))
    await screen.findByText(titulo)
    srv.microfoneAgora(false)

    await act(async () => void document.dispatchEvent(new Event('visibilitychange')))

    await waitFor(() => expect(aviso()).toBeNull())
  })

  it('liberado desde o começo: voltar à aba não anuncia "liberado"', async () => {
    const srv = montar()
    await waitFor(() => expect(consultas(srv)).toHaveLength(1))
    await voltarParaAAba()
    await waitFor(() => expect(consultas(srv).length).toBeGreaterThan(1))
    expect(pilula()?.textContent ?? '').not.toContain('Microfone liberado')
  })

  it('a consulta que falha não derruba o aviso que já está na tela', async () => {
    const srv = montar((s) => s.microfoneAgora(true, 'conta'))
    await screen.findByText(titulo)
    srv.interceptar((p) => (p.caminho === '/api/microfone' ? new Response('boom', { status: 500 }) : undefined))

    await voltarParaAAba()
    await waitFor(() => expect(consultas(srv).length).toBeGreaterThan(1))

    expect(aviso()).not.toBeNull()
  })

  it('um Agent de antes do aviso (404) não acende nada', async () => {
    const srv = montar((s) => s.interceptar((p) => (p.caminho === '/api/microfone' ? new Response('404 page not found', { status: 404 }) : undefined)))
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    await waitFor(() => expect(consultas(srv)).toHaveLength(1))
    expect(aviso()).toBeNull()
  })
})

describe('na nuvem não há aviso do microfone', () => {
  it('sem AvisoDoMicrofone no ambiente, o painel não pergunta nada ao Agent', async () => {
    const srv = servidorFalso(base())
    srv.microfoneAgora(true, 'conta')
    const ambiente = { ...ambienteLocal(criarCliente('tok', srv.buscar)), AvisoDoMicrofone: undefined }
    render(<Painel ambiente={ambiente} />)
    await screen.findByRole('button', { name: /Manda para o Thiago/ })
    await voltarParaAAba()
    expect(consultas(srv)).toHaveLength(0)
    expect(aviso()).toBeNull()
  })
})

describe('o cliente local do microfone', () => {
  it('lê o GET e devolve só o bloqueio e o motivo', async () => {
    const srv = servidorFalso(base())
    srv.microfoneAgora(true, 'programas')
    expect(await criarCliente('tok', srv.buscar).microfone()).toEqual({ bloqueado: true, motivo: 'programas' })
  })

  it('um corpo estranho não é bloqueio', async () => {
    const srv = servidorFalso(base())
    srv.interceptar(() => new Response(JSON.stringify({ bloqueado: 'sim' }), { status: 200 }))
    expect(await criarCliente('tok', srv.buscar).microfone()).toEqual({ bloqueado: false, motivo: '' })
  })
})
