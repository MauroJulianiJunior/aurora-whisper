// O pareamento no app da nuvem (ADR-019, F6) contra a nuvem de mentira
// (testes/servidorFalsoNuvem.ts): a página de aprovação de um PC
// (/device/authorize) e a seção Aparelhos das Configurações, com o Revogar.
//
// O que se prova aqui é o que a tela faz: quais pedidos saem, para onde a
// página vai e o que a pessoa vê. O pareamento de verdade (o code de uso
// único, o PKCE, o token) é dos testes Go (internal/server/authtest e
// internal/server/integration).

import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type AparelhoNuvem, type OpcoesDaNuvem, type PedidoFalso, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

const idDoPedido = '0192f0a1-7c3e-7d4a-9b2f-1a2b3c4d5e6f'
const pedido: PedidoFalso = { nome: 'DELL-G15', plataforma: 'windows', versao: '0.1.0-sprint2', conferencia: 'K7QM', expira_em: em(5, 18, 10), porta: 51234 }

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 5, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function aprovar(opcoes: OpcoesDaNuvem = {}, busca = `?pedido=${idDoPedido}`) {
  const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(5, 14), final_text: 'Um ditado.' })], { pedidos: { [idDoPedido]: pedido }, ...opcoes })
  const irPara = vi.fn()
  const trocarPara = vi.fn()
  render(<AppNuvem buscar={srv.buscar} irPara={irPara} trocarPara={trocarPara} caminho="/device/authorize" busca={busca} host="nuvem.exemplo.test" />)
  return { srv, irPara, trocarPara }
}

const titulo = (nome: string) => screen.findByRole('heading', { level: 1, name: nome })
const botao = (nome: string) => screen.getByRole<HTMLButtonElement>('button', { name: nome })

describe('a página de aprovação de um PC', () => {
  it('com a sessão: o nome do PC, o código e o aviso, sem abrir o histórico', async () => {
    const { srv } = aprovar()
    await titulo('Conectar este PC?')
    const tela = document.querySelector<HTMLElement>('.tela')!
    expect(tela.textContent).toContain('DELL-G15 · Windows')
    expect(within(tela).getByText('K7QM').className).toBe('codigo')
    expect(tela.querySelector('.aprovar-codigo')!.textContent).toBe('Código K7QM')
    expect(tela.textContent).toContain('Confira se é o mesmo do painel do PC.')
    // A conta que aprova, antes do Aprovar: o PC fica ligado a ela.
    expect(tela.querySelector('.aprovar-conta')!.textContent).toBe('Este PC vai ficar ligado à conta voce@teste.local.')
    expect(within(tela).getByText('voce@teste.local').tagName).toBe('STRONG')
    expect(tela.textContent).toContain('Só aprove se você clicou em Conectar neste PC agora.')
    expect(botao('Aprovar')).toBeTruthy()
    expect(botao('Agora não')).toBeTruthy()
    // O título recebe o foco, como em toda tela inteira.
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Conectar este PC?'))
    expect(srv.pedidos.map((p) => `${p.metodo} ${p.caminho}`)).toEqual(['GET /api/v1/me', `GET /api/v1/device-auth/requests/${idDoPedido}`])
    expect(srv.pedidos.every((p) => p.credentials === 'same-origin' && !('Authorization' in p.cabecalhos))).toBe(true)
  })

  it('a conta mostrada é a da sessão, qualquer que seja (a errada aparece antes do Aprovar)', async () => {
    aprovar({ email: 'outra.conta.com.um.nome.bem.comprido@exemplo-de-dominio-longo.test' })
    await titulo('Conectar este PC?')
    expect(document.querySelector('.aprovar-conta')!.textContent).toBe(
      'Este PC vai ficar ligado à conta outra.conta.com.um.nome.bem.comprido@exemplo-de-dominio-longo.test.',
    )
  })

  it('Aprovar: um POST, e a página vai ao retorno do PC sem ficar no histórico', async () => {
    const { srv, trocarPara } = aprovar()
    await titulo('Conectar este PC?')
    fireEvent.click(botao('Aprovar'))
    expect(botao('Aprovando…').getAttribute('aria-disabled')).toBe('true')
    fireEvent.click(botao('Aprovando…'))
    await waitFor(() => expect(trocarPara).toHaveBeenCalledTimes(1))
    expect(trocarPara).toHaveBeenCalledWith(`http://127.0.0.1:51234/nuvem/retorno?code=c0de-de-teste&state=${idDoPedido}`)
    expect(srv.pedidos.filter((p) => p.metodo === 'POST')).toEqual([expect.objectContaining({ caminho: `/api/v1/device-auth/requests/${idDoPedido}/approve` })])
    // A página está indo embora: o botão fica em "Aprovando…".
    expect(botao('Aprovando…')).toBeTruthy()
  })

  it('Agora não: nada vai à nuvem, e a página diz que nada foi conectado', async () => {
    const { srv, trocarPara } = aprovar()
    await titulo('Conectar este PC?')
    fireEvent.click(botao('Agora não'))
    await titulo('Nada foi conectado.')
    expect(screen.getByText('Pode fechar esta aba.')).toBeTruthy()
    expect(srv.pedidos.some((p) => p.metodo === 'POST')).toBe(false)
    expect(trocarPara).not.toHaveBeenCalled()
  })

  it('o pedido vencido, usado ou desconhecido: volte ao PC', async () => {
    aprovar({ pedidos: {} })
    await titulo('Este pedido venceu ou já foi usado.')
    expect(screen.getByText('Volte ao PC e clique em Conectar à nuvem de novo.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Aprovar' })).toBeNull()
  })

  it('um pedido que não é um id nem chega à nuvem', async () => {
    for (const busca of ['', '?pedido=', '?pedido=../me', `?pedido=${idDoPedido}x`]) {
      const { srv } = aprovar({}, busca)
      await titulo('Este pedido venceu ou já foi usado.')
      expect(srv.pedidos.some((p) => p.caminho.startsWith('/api/v1/device-auth'))).toBe(false)
      cleanup()
    }
  })

  it('aprovar depois de o pedido vencer: a mesma página do vencido', async () => {
    const { srv, trocarPara } = aprovar()
    await titulo('Conectar este PC?')
    srv.pedidosDePareamento.clear()
    fireEvent.click(botao('Aprovar'))
    await titulo('Este pedido venceu ou já foi usado.')
    expect(trocarPara).not.toHaveBeenCalled()
  })

  it('um retorno que não é o 127.0.0.1 do PC: a página não vai, e diz', async () => {
    for (const retorno of ['https://outro.exemplo/nuvem/retorno?code=x', 'http://127.0.0.1.outro.exemplo:51234/nuvem/retorno?code=x', 'javascript:alert(1)', 'http://127.0.0.1:51234/outra?code=x']) {
      const { srv, trocarPara } = aprovar()
      await titulo('Conectar este PC?')
      srv.interceptar((p) => (p.caminho.endsWith('/approve') ? new Response(JSON.stringify({ retorno }), { status: 200 }) : undefined))
      fireEvent.click(botao('Aprovar'))
      await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Não deu para aprovar. Tente de novo.'))
      expect(trocarPara).not.toHaveBeenCalled()
      expect(botao('Aprovar').getAttribute('aria-disabled')).toBeNull()
      cleanup()
    }
  })

  it('sem rede no Aprovar: diz, e o botão volta', async () => {
    const { srv, trocarPara } = aprovar()
    await titulo('Conectar este PC?')
    srv.derrubar(true)
    fireEvent.click(botao('Aprovar'))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Sem conexão. Tente de novo.'))
    expect(trocarPara).not.toHaveBeenCalled()
  })

  it('a sessão que acaba no Aprovar: Entrar, e o login volta à aprovação', async () => {
    const { srv, irPara } = aprovar()
    await titulo('Conectar este PC?')
    srv.expirar()
    fireEvent.click(botao('Aprovar'))
    await titulo('Sua sessão terminou.')
    expect(screen.getByText('Entre para aprovar a conexão do PC.')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar com o Google' }))
    await waitFor(() => expect(irPara).toHaveBeenCalled())
    const start = srv.pedidos.find((p) => p.caminho === '/auth/start')!
    expect(start.corpo).toEqual({ volta: `/device/authorize?pedido=${idDoPedido}` })
  })

  it('sem sessão: Entrar, com a volta para a aprovação', async () => {
    const { srv, irPara } = aprovar({ logado: false })
    await titulo('Aurora Whisper')
    expect(screen.getByText('Entre para aprovar a conexão do PC.')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar com o Google' }))
    await waitFor(() => expect(irPara).toHaveBeenCalledWith('https://accounts.google.com/o/oauth2/v2/auth?client_id=c&state=s'))
    expect(srv.pedidos.find((p) => p.caminho === '/auth/start')!.corpo).toEqual({ volta: `/device/authorize?pedido=${idDoPedido}` })
    // Nada do pedido sem a sessão.
    expect(srv.pedidos.some((p) => p.caminho.startsWith('/api/v1/device-auth'))).toBe(false)
  })

  it('a conta de teste entra e cai na aprovação, no mesmo endereço', async () => {
    const srv = servidorFalsoNuvem([], { logado: false, teste: { conta: 'voce@teste.local', google: false }, pedidos: { [idDoPedido]: pedido } })
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} trocarPara={vi.fn()} caminho="/device/authorize" busca={`?pedido=${idDoPedido}`} host="localhost" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar com a conta de teste' }))
    await titulo('Conectar este PC?')
    expect(screen.queryByRole('heading', { level: 1, name: 'Histórico' })).toBeNull()
  })

  it('em qualquer outro caminho, o app de sempre', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(5, 14), final_text: 'Um ditado.' })], { pedidos: { [idDoPedido]: pedido } })
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} caminho="/" busca={`?pedido=${idDoPedido}`} />)
    await screen.findByRole('heading', { level: 1, name: 'Histórico' })
    expect(srv.pedidos.some((p) => p.caminho.startsWith('/api/v1/device-auth'))).toBe(false)
  })
})

describe('a seção Aparelhos', () => {
  const pc = (id: string, nome: string, uso: string): AparelhoNuvem => ({
    id,
    nome,
    plataforma: 'windows',
    versao: '0.1.0',
    conectado_em: em(1, 9),
    ultimo_uso_em: uso,
  })

  async function abrirConfiguracoes(opcoes: OpcoesDaNuvem = {}) {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(5, 14), final_text: 'Um ditado.' })], opcoes)
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
    await screen.findByRole('button', { name: /Um ditado/ })
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { level: 2, name: 'Aparelhos' })
    return srv
  }

  const secao = () => screen.getByRole('region', { name: 'Aparelhos' })

  it('vem entre a Conta e as Sessões abertas; sem PC, diz', async () => {
    await abrirConfiguracoes()
    await within(secao()).findByText('Nenhum PC conectado.')
    expect(within(secao()).queryByRole('button')).toBeNull()
  })

  it('cada PC numa linha: o nome, a plataforma, a versão e o último uso', async () => {
    await abrirConfiguracoes({ aparelhos: [pc('a1', 'DELL-G15', em(5, 17, 30)), pc('a2', 'NOTE-SALA', em(3, 8))] })
    const linhas = await within(secao()).findAllByRole('listitem')
    expect(linhas).toHaveLength(2)
    expect(within(linhas[0]!).getByText('DELL-G15')).toBeTruthy()
    expect(linhas[0]!.querySelector('.sessao-uso')!.textContent).toMatch(/^, Windows · versão 0\.1\.0 · último uso .+/)
    expect(within(linhas[0]!).getByRole('button', { name: /^Revogar DELL-G15, último uso/ })).toBeTruthy()
  })

  it('Revogar pergunta na própria linha, com o foco no Cancelar; o Esc desiste e devolve o foco', async () => {
    const srv = await abrirConfiguracoes({ aparelhos: [pc('a1', 'DELL-G15', em(5, 17, 30))] })
    const pedir = await within(secao()).findByRole('button', { name: /^Revogar DELL-G15/ })
    fireEvent.click(pedir)
    const pergunta = within(secao()).getByRole('group', { name: 'Revogar DELL-G15? O PC para de sincronizar e continua funcionando nele.' })
    await waitFor(() => expect(document.activeElement).toBe(within(pergunta).getByRole('button', { name: 'Cancelar' })))
    expect(srv.pedidos.some((p) => p.metodo === 'DELETE')).toBe(false)

    fireEvent.keyDown(pergunta, { key: 'Escape' })
    await waitFor(() => expect(within(secao()).queryByRole('group')).toBeNull())
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Revogar'))
    expect(srv.pedidos.some((p) => p.metodo === 'DELETE')).toBe(false)
  })

  it('confirmar revoga: o DELETE, a linha sai, o aviso, e o foco vai para o Revogar do lado (ou o título)', async () => {
    const srv = await abrirConfiguracoes({ aparelhos: [pc('a1', 'DELL-G15', em(5, 17, 30)), pc('a2', 'NOTE-SALA', em(3, 8))] })
    fireEvent.click(await within(secao()).findByRole('button', { name: /^Revogar DELL-G15/ }))
    fireEvent.click(within(secao()).getByRole('button', { name: 'Revogar' }))
    await waitFor(() => expect(within(secao()).queryByText('DELL-G15')).toBeNull())
    expect(srv.pedidos.filter((p) => p.metodo === 'DELETE').map((p) => p.caminho)).toEqual(['/api/v1/devices/a1'])
    expect(document.querySelector('.aviso-regiao')!.textContent).toBe('PC revogado.')
    await waitFor(() => expect(document.activeElement?.getAttribute('aria-label')).toMatch(/^Revogar NOTE-SALA/))

    fireEvent.click(within(secao()).getByRole('button', { name: /^Revogar NOTE-SALA/ }))
    fireEvent.click(within(secao()).getByRole('button', { name: 'Revogar' }))
    await within(secao()).findByText('Nenhum PC conectado.')
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Aparelhos'))
  })

  it('com o DELETE no ar, o Cancelar e o Esc esperam: a pergunta fica, e o PC sai como a pessoa viu', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(5, 14), final_text: 'Um ditado.' })], { aparelhos: [pc('a1', 'DELL-G15', em(5, 17, 30))] })
    let soltar: () => void = () => {}
    const espera = new Promise<void>((r) => {
      soltar = r
    })
    const passagem = (entrada: string, init?: RequestInit) =>
      init?.method === 'DELETE' && entrada === '/api/v1/devices/a1' ? espera.then(() => srv.buscar(entrada, init)) : srv.buscar(entrada, init)
    render(<AppNuvem buscar={passagem} irPara={vi.fn()} />)
    await screen.findByRole('button', { name: /Um ditado/ })
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { level: 2, name: 'Aparelhos' })
    fireEvent.click(await within(secao()).findByRole('button', { name: /^Revogar DELL-G15/ }))
    fireEvent.click(within(secao()).getByRole('button', { name: 'Revogar' }))
    await within(secao()).findByRole('button', { name: 'Revogando…' })

    const cancelar = within(secao()).getByRole('button', { name: 'Cancelar' })
    expect(cancelar.getAttribute('aria-disabled')).toBe('true')
    fireEvent.click(cancelar)
    fireEvent.keyDown(within(secao()).getByRole('group'), { key: 'Escape' })
    expect(within(secao()).getByRole('group')).toBeTruthy()
    expect(within(secao()).getByRole('button', { name: 'Revogando…' })).toBeTruthy()

    soltar()
    await waitFor(() => expect(within(secao()).queryByText('DELL-G15')).toBeNull())
    expect(document.querySelector('.aviso-regiao')!.textContent).toBe('PC revogado.')
    expect(srv.pedidos.filter((p) => p.metodo === 'DELETE')).toHaveLength(1)
  })

  it('o PC que já saiu (404) some da lista do mesmo jeito; outro erro diz e fica', async () => {
    const srv = await abrirConfiguracoes({ aparelhos: [pc('a1', 'DELL-G15', em(5, 17, 30))] })
    srv.interceptar((p) => (p.metodo === 'DELETE' ? new Response(JSON.stringify({ error: { message: 'falha interna' } }), { status: 500 }) : undefined))
    fireEvent.click(await within(secao()).findByRole('button', { name: /^Revogar DELL-G15/ }))
    fireEvent.click(within(secao()).getByRole('button', { name: 'Revogar' }))
    await waitFor(() => expect(within(secao()).getByRole('alert').textContent).toBe('Não deu para revogar: falha interna'))
    expect(within(secao()).getByText('DELL-G15')).toBeTruthy()

    srv.interceptar(null)
    srv.aparelhos.splice(0, 1)
    fireEvent.click(within(secao()).getByRole('button', { name: 'Revogar' }))
    await within(secao()).findByText('Nenhum PC conectado.')
  })

  it('apagar meus dados da nuvem tira os PCs da lista', async () => {
    await abrirConfiguracoes({ aparelhos: [pc('a1', 'DELL-G15', em(5, 17, 30))] })
    await within(secao()).findByText('DELL-G15')
    fireEvent.click(screen.getByRole('button', { name: 'Apagar meus dados da nuvem' }))
    fireEvent.change(screen.getByLabelText(/Para confirmar, digite/), { target: { value: 'apagar' } })
    fireEvent.click(screen.getByRole('button', { name: 'Apagar tudo' }))
    await within(secao()).findByText('Nenhum PC conectado.')
  })
})
