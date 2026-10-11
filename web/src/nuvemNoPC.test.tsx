// A seção "Nuvem" das Configurações do PC (ADR-019, F6) contra o Agent de
// mentira (testes/servidorFalso.ts): conectar, o código, o Abrir de novo, o
// Cancelar, a volta à aba, o Desconectar e os avisos.
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos
// saem e o que a pessoa vê. O pareamento de verdade (o PKCE, o state, o
// token no Gerenciador) é dos testes Go de internal/agent/nuvem.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { EstadoNuvem } from './clienteLocal'
import { type OpcoesDoServidor, type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

async function abrir(opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda.' })], opcoes)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'Nuvem' })
  await act(async () => {})
  return srv
}

const secao = () => screen.getByRole('region', { name: 'Nuvem' })
const campo = () => within(secao()).getByLabelText<HTMLInputElement>('Endereço da nuvem')
const botao = (nome: string | RegExp) => within(secao()).getByRole<HTMLButtonElement>('button', { name: nome })
const desligado = (b: HTMLElement) => b.getAttribute('aria-disabled') === 'true'
const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string) => srv.pedidos.filter((p) => p.caminho === caminho)
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent
const alertas = () => within(secao()).queryAllByRole('alert').map((a) => a.textContent)

const conectado: EstadoNuvem = { estado: 'conectado', url: 'http://localhost:8080', email: 'voce@teste.local', nome: 'DELL-G15' }

function voltarAAba() {
  act(() => void document.dispatchEvent(new Event('visibilitychange')))
}

describe('a seção Nuvem do PC', () => {
  it('começa desligada, depois do Dicionário e antes da Privacidade, e pergunta ao Agent ao aparecer', async () => {
    const srv = await abrir()
    const titulos = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(titulos.indexOf('Nuvem')).toBeGreaterThan(titulos.indexOf('Dicionário'))
    expect(titulos.indexOf('Nuvem')).toBeLessThan(titulos.indexOf('Privacidade'))

    expect(secao().textContent).toContain('Desligada. Nada vai para a nuvem até você conectar.')
    expect(campo().value).toBe('')
    expect(campo().placeholder).toBe('https://…')
    expect(desligado(botao('Conectar à nuvem'))).toBe(true)
    // Uma pergunta, com o token, e nenhuma ida à nuvem por conta própria.
    const conferir = pedidosA(srv, '/api/nuvem/conferir')
    expect(conferir).toHaveLength(1)
    expect(conferir[0]!.metodo).toBe('POST')
    expect(conferir[0]!.auth).toBe('Bearer tok')
    expect(pedidosA(srv, '/api/nuvem/conectar')).toHaveLength(0)
  })

  it('Conectar manda o endereço, mostra o código e leva o foco ao Abrir de novo', async () => {
    const srv = await abrir()
    fireEvent.change(campo(), { target: { value: ' http://localhost:8080 ' } })
    expect(desligado(botao('Conectar à nuvem'))).toBe(false)
    fireEvent.click(botao('Conectar à nuvem'))

    await within(secao()).findByText((_, el) => el?.textContent === 'Aprove no navegador e confira o código K7QM.')
    expect(pedidosA(srv, '/api/nuvem/conectar')[0]!.corpo).toEqual({ url: 'http://localhost:8080' })
    const link = within(secao()).getByRole<HTMLAnchorElement>('link', { name: 'Abrir de novo' })
    expect(link.getAttribute('href')).toBe('http://localhost:8080/device/authorize?pedido=p1')
    expect(link.target).toBe('_blank')
    expect(link.rel).toBe('noopener noreferrer')
    expect(within(secao()).getByText('K7QM').className).toBe('codigo')
    await waitFor(() => expect(document.activeElement).toBe(link))
    expect(within(secao()).queryByLabelText('Endereço da nuvem')).toBeNull()
  })

  it('Enter no campo conecta; o campo vazio não pede nada', async () => {
    const srv = await abrir()
    fireEvent.keyDown(campo(), { key: 'Enter' })
    fireEvent.click(botao('Conectar à nuvem'))
    expect(pedidosA(srv, '/api/nuvem/conectar')).toHaveLength(0)

    fireEvent.change(campo(), { target: { value: 'https://nuvem.exemplo.test' } })
    fireEvent.keyDown(campo(), { key: 'Enter' })
    await within(secao()).findByRole('link', { name: 'Abrir de novo' })
    expect(pedidosA(srv, '/api/nuvem/conectar')).toHaveLength(1)
  })

  it('o endereço que o Agent recusa: a frase dele, e o campo fica', async () => {
    const srv = await abrir()
    fireEvent.change(campo(), { target: { value: 'http://nuvem.exemplo.test' } })
    fireEvent.click(botao('Conectar à nuvem'))
    await waitFor(() => expect(alertas()).toEqual(['Use https:// ou http://localhost.']))
    expect(campo().value).toBe('http://nuvem.exemplo.test')
    expect(pedidosA(srv, '/api/nuvem/conectar')).toHaveLength(1)
  })

  it('Cancelar desfaz o pedido, devolve o campo com o endereço e o foco a ele', async () => {
    const srv = await abrir()
    fireEvent.change(campo(), { target: { value: 'http://localhost:8080' } })
    fireEvent.click(botao('Conectar à nuvem'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Cancelar' }))

    await waitFor(() => expect(within(secao()).queryByLabelText('Endereço da nuvem')).not.toBeNull())
    expect(pedidosA(srv, '/api/nuvem/desconectar')).toHaveLength(1)
    expect(campo().value).toBe('http://localhost:8080')
    await waitFor(() => expect(document.activeElement).toBe(campo()))
  })

  it('voltar à aba pergunta de novo: a aprovação que voltou ao PC vira "Conectado"', async () => {
    const srv = await abrir()
    fireEvent.change(campo(), { target: { value: 'http://localhost:8080' } })
    fireEvent.click(botao('Conectar à nuvem'))
    await within(secao()).findByRole('link', { name: 'Abrir de novo' })

    srv.nuvemPorFora(conectado)
    voltarAAba()
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(secao().textContent).toContain('Na nuvem, este PC aparece como DELL-G15.')
    expect(secao().textContent).toContain('Desconectar tira o acesso deste PC, aqui e na nuvem.')
    expect(pedidosA(srv, '/api/nuvem/conferir')).toHaveLength(2)
  })

  it('a aba escondida não pergunta; a tela das Configurações fechada também não', async () => {
    const srv = await abrir()
    const estado = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    voltarAAba()
    expect(pedidosA(srv, '/api/nuvem/conferir')).toHaveLength(1)
    estado.mockReturnValue('visible')

    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    voltarAAba()
    expect(pedidosA(srv, '/api/nuvem/conferir')).toHaveLength(1)
  })

  it('Desconectar: o Agent desconecta e avisa; o campo volta com o endereço', async () => {
    const srv = await abrir({ nuvem: conectado })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    fireEvent.click(botao('Desconectar'))

    await waitFor(() => expect(within(secao()).queryByLabelText('Endereço da nuvem')).not.toBeNull())
    expect(pedidosA(srv, '/api/nuvem/desconectar')).toHaveLength(1)
    expect(campo().value).toBe('http://localhost:8080')
    expect(secao().textContent).toContain('Desligada. Nada vai para a nuvem até você conectar.')
    await waitFor(() => expect(aviso()).toContain('Este PC foi desconectado da nuvem.'))
  })

  it('cada aviso do Agent tem a frase dele', async () => {
    const casos: [EstadoNuvem, string, 'alert' | 'status'][] = [
      [{ estado: 'desconectado', url: 'http://localhost:8080', aviso: 'revogado' }, 'Este PC foi desconectado na nuvem.', 'status'],
      [
        { estado: 'desconectado', url: 'http://localhost:8080', aviso: 'sem_resposta' },
        'Desconectado neste PC. A nuvem não respondeu: revogue-o em Aparelhos, no app.',
        'status',
      ],
      [{ estado: 'desconectado', url: 'http://localhost:8080', aviso: 'vencido' }, 'O pedido venceu. Clique em Conectar à nuvem de novo.', 'alert'],
      [{ estado: 'desconectado', url: 'http://localhost:8080', aviso: 'outra_conta' }, 'Este PC já está ligado a outra conta.', 'alert'],
      [{ estado: 'desconectado', url: 'http://localhost:8080', aviso: 'falhou' }, 'Não deu para conectar. Clique em Conectar à nuvem de novo.', 'alert'],
      [
        { estado: 'conectando', url: 'http://localhost:8080', conferencia: 'K7QM', abrir_url: 'http://localhost:8080/device/authorize?pedido=p1', aviso: 'nao_abriu' },
        'O navegador não abriu. Use Abrir de novo.',
        'alert',
      ],
      [{ ...conectado, aviso: 'sem_rede' }, 'A nuvem não respondeu agora. O PC continua conectado.', 'status'],
    ]
    for (const [estado, frase, papel] of casos) {
      await abrir({ nuvem: estado })
      const regiao = await within(secao()).findByText(frase)
      expect(regiao.getAttribute('role'), frase).toBe(papel)
      // O aviso vem antes do resto da seção.
      expect(secao().querySelector('h2')!.nextElementSibling).toBe(regiao)
      cleanup()
    }
  })

  it('revogado: o campo volta preenchido, pronto para conectar de novo', async () => {
    await abrir({ nuvem: { estado: 'desconectado', url: 'https://nuvem.exemplo.test', aviso: 'revogado' } })
    expect(campo().value).toBe('https://nuvem.exemplo.test')
    expect(desligado(botao('Conectar à nuvem'))).toBe(false)
  })

  it('o endereço do Agent não passa por cima do que a pessoa digitou', async () => {
    const srv = await abrir()
    fireEvent.change(campo(), { target: { value: 'https://outra.exemplo.test' } })
    srv.nuvemPorFora({ estado: 'desconectado', url: 'http://localhost:8080', aviso: 'revogado' })
    voltarAAba()
    await within(secao()).findByText('Este PC foi desconectado na nuvem.')
    expect(campo().value).toBe('https://outra.exemplo.test')
  })

  it('um Abrir de novo que não é da nuvem não vira link', async () => {
    await abrir({ nuvem: { estado: 'conectando', url: 'x', conferencia: 'K7QM', abrir_url: 'javascript:alert(1)' } })
    expect(within(secao()).queryByRole('link')).toBeNull()
    expect(within(secao()).getByRole('button', { name: 'Cancelar' })).toBeTruthy()
  })

  it('um Agent de antes da nuvem: diz o que fazer', async () => {
    const srv = servidorFalso([], {})
    srv.interceptar((p) => (p.caminho.startsWith('/api/nuvem') ? new Response('404 page not found', { status: 404 }) : undefined))
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await waitFor(() => expect(alertas()).toEqual(['Este Agent ainda não tem a nuvem. Reinicie o Agent.']))
    expect(within(secao()).queryByLabelText('Endereço da nuvem')).toBeNull()
  })

  it('o Agent fora do ar acende a faixa do painel, e a seção fica como está', async () => {
    const srv = await abrir()
    fireEvent.change(campo(), { target: { value: 'http://localhost:8080' } })
    srv.derrubar(true)
    fireEvent.click(botao('Conectar à nuvem'))
    await waitFor(() => expect(document.querySelector('.faixa')).not.toBeNull())
    expect(campo().value).toBe('http://localhost:8080')
    expect(alertas()).toEqual([])
  })
})
