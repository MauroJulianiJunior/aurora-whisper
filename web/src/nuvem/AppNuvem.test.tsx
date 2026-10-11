// O app da nuvem contra a nuvem de mentira (testes/servidorFalsoNuvem.ts): a
// abertura (Painel, Entrar, Sem conexão, Não abriu), o login (Google e conta
// de teste), a sessão que acaba no meio do uso, a rede que cai, e as
// Configurações da conta.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Buscador } from '../api'
import { type OpcoesDaNuvem, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

function base() {
  return [
    itemNuvem({ id: 'd1', created_at: em(5, 14, 32), raw_text: 'Manda o relatório amanhã.', final_text: 'Manda o relatório amanhã.' }),
    itemNuvem({ id: 'd2', created_at: em(5, 9, 5), raw_text: 'A reunião é às dez.', final_text: 'A reunião é às dez.' }),
  ]
}

function montar(opcoes: OpcoesDaNuvem = {}, itens = base(), host?: string) {
  const srv = servidorFalsoNuvem(itens, opcoes)
  const irPara = vi.fn()
  render(<AppNuvem buscar={srv.buscar} irPara={irPara} host={host} />)
  return { srv, irPara }
}

const titulos = () => Array.from(document.querySelectorAll('h1'), (h) => h.textContent)
const tituloDoHistorico = () => screen.getByRole('heading', { level: 1, name: 'Histórico' })

const linha = (texto: RegExp) => screen.findByRole('button', { name: texto })
const titulo = (nome: string) => screen.findByRole('heading', { level: 1, name: nome })
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 5, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('abertura', () => {
  it('com a sessão do cookie, abre o histórico, sem mexer no foco', async () => {
    const { srv } = montar()
    await linha(/Manda o relatório amanhã/)
    expect(srv.pedidos[0]).toMatchObject({ metodo: 'GET', caminho: '/api/v1/me' })
    expect(screen.queryByRole('button', { name: 'Entrar com o Google' })).toBeNull()
    // A página acabou de abrir: o foco fica onde o navegador o pôs.
    expect(document.activeElement).toBe(document.body)
    expect(tituloDoHistorico().hasAttribute('tabindex')).toBe(false)
  })

  it('a nuvem que demora: depois de 300 ms, a Aurora e "Abrindo…", que o leitor de tela ouve', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    vi.setSystemTime(new Date(2026, 9, 5, 18, 0))
    const srv = servidorFalsoNuvem(base())
    let soltar = () => {}
    const segurado = new Promise<void>((r) => {
      soltar = r
    })
    const buscar: Buscador = async (entrada, init) => {
      if (entrada === '/api/v1/me') await segurado
      return srv.buscar(entrada, init)
    }
    render(<AppNuvem buscar={buscar} irPara={vi.fn()} />)
    const status = screen.getByRole('status')
    act(() => vi.advanceTimersByTime(299))
    // Numa rede boa, a resposta chega antes, e nada pisca.
    expect(status.textContent).toBe('')
    expect(document.querySelector('.aurora')).toBeNull()
    act(() => vi.advanceTimersByTime(1))
    expect(status.textContent).toBe('Abrindo…')
    expect(document.querySelector('.aurora')).not.toBeNull()
    expect(titulos()).toEqual([])
    // E sai quando a nuvem responde.
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date(2026, 9, 5, 18, 0))
    soltar()
    await linha(/Manda o relatório amanhã/)
    expect(screen.queryByText('Abrindo…')).toBeNull()
  })

  it('sem sessão: Entrar, com o foco no título; em localhost sem o login de teste, só o Google', async () => {
    const { srv } = montar({ logado: false })
    const h1 = await titulo('Aurora Whisper')
    await waitFor(() => expect(document.activeElement).toBe(h1))
    expect(screen.getByText('Entre para ver o que você ditou.')).toBeTruthy()
    expect(await screen.findByRole('button', { name: 'Entrar com o Google' })).toBeTruthy()
    // Em localhost, a tela pergunta; o 404 quer dizer: nenhum botão a mais.
    expect(srv.pedidos.some((p) => p.metodo === 'GET' && p.caminho === '/auth/teste')).toBe(true)
    expect(screen.queryByRole('button', { name: /conta de teste/ })).toBeNull()
  })

  it('num servidor de verdade, fora de localhost: o Google junto com o título, sem perguntar pelo login de teste', async () => {
    const { srv } = montar({ logado: false }, base(), 'aurora.exemplo.com.br')
    await titulo('Aurora Whisper')
    expect(screen.getByRole('button', { name: 'Entrar com o Google' })).toBeTruthy()
    expect(srv.pedidos.map((p) => `${p.metodo} ${p.caminho}`)).toEqual(['GET /api/v1/me'])
  })

  it('sem rede: Sem conexão, e Tentar de novo abre quando ela volta', async () => {
    const srv = servidorFalsoNuvem(base())
    srv.derrubar(true)
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
    const h1 = await titulo('Sem conexão.')
    await waitFor(() => expect(document.activeElement).toBe(h1))
    expect(screen.getByText('O que você ditou continua no PC.')).toBeTruthy()
    srv.derrubar(false)
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await linha(/Manda o relatório amanhã/)
    // O botão saiu com a tela: o foco vai para o título do histórico.
    await waitFor(() => expect(document.activeElement).toBe(tituloDoHistorico()))
  })

  it('outro erro: Não deu para abrir', async () => {
    const srv = servidorFalsoNuvem(base())
    srv.interceptar((p) => (p.caminho === '/api/v1/me' ? new Response('{}', { status: 500 }) : undefined))
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
    await titulo('Não deu para abrir.')
    srv.interceptar(null)
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await linha(/Manda o relatório amanhã/)
  })
})

describe('entrar', () => {
  it('com o Google: POST /auth/start e vai para o endereço que ele deu', async () => {
    const { srv, irPara } = montar({ logado: false })
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar com o Google' }))
    await waitFor(() => expect(irPara).toHaveBeenCalledWith(expect.stringMatching(/^https:\/\/accounts\.google\.com\//)))
    expect(srv.pedidos.some((p) => p.metodo === 'POST' && p.caminho === '/auth/start')).toBe(true)
    expect(screen.getByRole('button', { name: 'Abrindo o Google…' })).toBeTruthy()
  })

  it('voltar do Google pelo Voltar (a página do cache de ida e volta) solta o botão', async () => {
    const { srv, irPara } = montar({ logado: false })
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar com o Google' }))
    const preso = await screen.findByRole('button', { name: 'Abrindo o Google…' })
    await waitFor(() => expect(irPara).toHaveBeenCalledTimes(1))
    // Um pageshow que não é a volta do cache não muda nada.
    act(() => {
      window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: false }))
    })
    expect(preso.textContent).toBe('Abrindo o Google…')
    act(() => {
      window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }))
    })
    const botao = screen.getByRole('button', { name: 'Entrar com o Google' })
    expect(botao.getAttribute('aria-disabled')).toBeNull()
    fireEvent.click(botao)
    await waitFor(() => expect(irPara).toHaveBeenCalledTimes(2))
    expect(srv.pedidos.filter((p) => p.metodo === 'POST' && p.caminho === '/auth/start')).toHaveLength(2)
  })

  it('o limite de tentativas tem a frase de espera; a rede, a frase de tentar de novo', async () => {
    const { srv, irPara } = montar({ logado: false })
    srv.interceptar((p) => (p.caminho === '/auth/start' ? new Response('{}', { status: 429 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar com o Google' }))
    expect((await screen.findByRole('alert')).textContent).toBe('Muitas tentativas. Espere um minuto e tente de novo.')
    srv.interceptar(null)
    srv.derrubar(true)
    fireEvent.click(screen.getByRole('button', { name: 'Entrar com o Google' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Não deu para entrar. Tente de novo.'))
    expect(irPara).not.toHaveBeenCalled()
  })

  it('no servidor de teste sem Google: só a conta de teste, que abre o histórico', async () => {
    const { srv } = montar({ logado: false, teste: { conta: 'voce@teste.local', google: false } })
    const teste = await screen.findByRole('button', { name: 'Entrar com a conta de teste' })
    expect(screen.queryByRole('button', { name: 'Entrar com o Google' })).toBeNull()
    expect(teste.getAttribute('aria-describedby')).toBe('entrar-teste-ajuda')
    expect(screen.getByText('Só neste PC, sem Google.')).toBeTruthy()
    fireEvent.click(teste)
    await linha(/Manda o relatório amanhã/)
    expect(srv.pedidos.find((p) => p.metodo === 'POST' && p.caminho === '/auth/teste')?.corpo).toEqual({ email: 'voce@teste.local' })
    // O botão saiu com a tela Entrar: o foco vai para o título do histórico,
    // e não para o <body>.
    await waitFor(() => expect(document.activeElement).toBe(tituloDoHistorico()))
  })

  it('no servidor de teste com Google: os dois botões', async () => {
    montar({ logado: false, teste: { conta: 'voce@teste.local', google: true } })
    expect(await screen.findByRole('button', { name: 'Entrar com o Google' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Entrar com a conta de teste' })).toBeTruthy()
  })
})

describe('no meio do uso', () => {
  it('a sessão que acaba (401) troca a tela inteira por Sua sessão terminou', async () => {
    const { srv } = montar({ teste: { conta: 'voce@teste.local', google: true } })
    await linha(/Manda o relatório amanhã/)
    srv.expirar()
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    const h1 = await titulo('Sua sessão terminou.')
    await waitFor(() => expect(document.activeElement).toBe(h1))
    expect(screen.getByText('Entre de novo para ver seu histórico.')).toBeTruthy()
    // Nada do que estava na tela fica.
    expect(screen.queryByText(/Manda o relatório/)).toBeNull()
    expect(await screen.findByRole('button', { name: 'Entrar com o Google' })).toBeTruthy()
    // E entra de novo pelo mesmo caminho.
    fireEvent.click(screen.getByRole('button', { name: 'Entrar com a conta de teste' }))
    await linha(/Manda o relatório amanhã/)
  })

  it('a rede que cai acende a faixa Sem conexão, com a lista no lugar', async () => {
    const { srv } = montar()
    await linha(/Manda o relatório amanhã/)
    srv.derrubar(true)
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    const faixa = await screen.findByRole('alert')
    expect(faixa.textContent).toContain('Sem conexão.')
    expect(faixa.textContent).toContain('O que você ditou continua no PC.')
    expect(screen.getByRole('button', { name: /Manda o relatório amanhã/ })).toBeTruthy()
    srv.derrubar(false)
    fireEvent.click(within(faixa).getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })

  it('o histórico vazio fala do PC, e não da tecla de ditado', async () => {
    montar({}, [])
    expect(await screen.findByText('Nenhum ditado ainda.', { selector: '.estado-titulo' })).toBeTruthy()
    expect(screen.getByText('O que você ditar num PC conectado aparece aqui.')).toBeTruthy()
  })

  it('salvar não pergunta se quer aprender: aprender é só no PC', async () => {
    const { srv } = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã/))
    fireEvent.change(screen.getByLabelText('Texto final'), { target: { value: 'Manda o relatório hoje.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await linha(/Manda o relatório hoje/)
    expect(aviso().textContent).toBe('Texto salvo.')
    expect(srv.pedidos.find((p) => p.metodo === 'PATCH')?.corpo).toEqual({ base_version: 1, final_text: 'Manda o relatório hoje.' })
  })
})

describe('Configurações da nuvem', () => {
  const sessoes = [
    { id: 's1', rotulo: 'Android · Chrome', criada_em: em(1, 9), ultimo_uso_em: em(5, 17, 59), atual: true },
    { id: 's2', rotulo: 'Windows · Edge', criada_em: em(2, 9), ultimo_uso_em: em(4, 8, 15), atual: false },
  ]

  async function abrirConfiguracoes(opcoes: OpcoesDaNuvem = {}) {
    const m = montar({ sessoes, ...opcoes })
    await linha(/Manda o relatório amanhã/)
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByText('Windows · Edge')
    return m
  }

  it('a conta, as sessões e a privacidade; nada do PC', async () => {
    await abrirConfiguracoes()
    const tela = document.querySelector<HTMLElement>('.config')!
    expect(within(tela).getByText(/Entrou como/).textContent).toBe('Entrou como voce@teste.local.')
    expect(within(tela).getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual(['Conta', 'Aparelhos', 'Aprendido', 'Sessões abertas', 'Privacidade'])
    for (const doPC of ['Tecla de ditado', 'Microfone sempre pronto', 'Chave de API', 'Iniciar com o Windows', 'Salvar configurações', 'Literal']) {
      expect(tela.textContent).not.toContain(doPC)
    }
    expect(tela.textContent).toContain('Nunca vão: o áudio, o que está nas outras janelas, a chave do Polish e as configurações dos PCs.')
  })

  it('a sessão atual é "este aparelho", sem Encerrar; encerrar outra a tira da lista', async () => {
    const { srv } = await abrirConfiguracoes()
    const lista = document.querySelector<HTMLElement>('.sessoes')!
    const [atual, outra] = within(lista).getAllByRole('listitem')
    expect(within(atual!).getByText('este aparelho')).toBeTruthy()
    expect(within(atual!).queryByRole('button')).toBeNull()
    fireEvent.click(within(outra!).getByRole('button', { name: /^Encerrar a sessão Windows · Edge/ }))
    await waitFor(() => expect(screen.queryByText('Windows · Edge')).toBeNull())
    expect(srv.pedidos.some((p) => p.metodo === 'DELETE' && p.caminho === '/api/v1/sessions/s2')).toBe(true)
    expect(aviso().textContent).toBe('Sessão encerrada.')
    // Sem outro Encerrar, o foco vai para o título da seção. O foco vem de um
    // efeito, que pode rodar depois de a linha sair da página: espera.
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Sessões abertas'))
    expect(screen.getByText('Nenhuma outra sessão aberta.')).toBeTruthy()
  })

  it('Sair apaga a sessão no servidor e volta para Entrar', async () => {
    const { srv } = await abrirConfiguracoes()
    fireEvent.click(screen.getByRole('button', { name: 'Sair' }))
    await titulo('Aurora Whisper')
    expect(srv.pedidos.some((p) => p.metodo === 'POST' && p.caminho === '/auth/logout')).toBe(true)
    expect(srv.logado).toBe(false)
  })

  it('apagar meus dados só vai com "apagar" digitado; Cancelar não apaga nada', async () => {
    const { srv } = await abrirConfiguracoes()
    const apagados = () => srv.pedidos.filter((p) => p.caminho === '/api/v1/me/data')
    fireEvent.click(screen.getByRole('button', { name: 'Apagar meus dados da nuvem' }))
    const campo = screen.getByLabelText<HTMLInputElement>(/Para confirmar, digite/)
    expect(document.activeElement).toBe(campo)
    expect(screen.getByText('Apaga tudo desta conta na nuvem e desconecta os PCs. Os PCs ficam com a cópia deles.')).toBeTruthy()
    const apagarTudo = screen.getByRole('button', { name: 'Apagar tudo' })
    expect(apagarTudo.getAttribute('aria-disabled')).toBe('true')
    fireEvent.click(apagarTudo)
    fireEvent.change(campo, { target: { value: 'apaga' } })
    fireEvent.click(apagarTudo)
    fireEvent.keyDown(campo, { key: 'Enter' })
    expect(apagados()).toHaveLength(0)

    // Cancelar fecha, e o foco volta para o botão.
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Apagar meus dados da nuvem'))
    expect(apagados()).toHaveLength(0)

    fireEvent.click(screen.getByRole('button', { name: 'Apagar meus dados da nuvem' }))
    fireEvent.change(screen.getByLabelText(/Para confirmar, digite/), { target: { value: 'apagar' } })
    expect(screen.getByRole('button', { name: 'Apagar tudo' }).getAttribute('aria-disabled')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Apagar tudo' }))
    await waitFor(() => expect(aviso().textContent).toBe('Seus dados foram apagados da nuvem.'))
    expect(apagados().map((p) => [p.metodo, p.corpo])).toEqual([['DELETE', { confirmar: 'apagar' }]])
    // O histórico lê de novo, vazio; as outras sessões foram encerradas.
    await waitFor(() => expect(screen.queryByText('Windows · Edge')).toBeNull())
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    expect(await screen.findByText('Nenhum ditado ainda.', { selector: '.estado-titulo' })).toBeTruthy()
  })
})
