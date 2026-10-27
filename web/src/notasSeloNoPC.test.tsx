// O selo de conflito da nota no painel do PC (ADR-022, N7) contra o Agent de
// mentira (testes/servidorFalso.ts): o aviso no cartão, o bloco com a outra versão
// na nota aberta e as duas escolhas (POST /api/notas/{id}/resolver-conflito).
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos saem, o
// que a pessoa vê e o que fica na janela. A regra do selo é dos testes Go
// (internal/storage, internal/agent/localapi). Os dois temas (claro e escuro) são
// só variáveis de CSS (index.css): o selo usa as mesmas do selo do histórico, e o
// aviso é texto, e não só cor.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { Nota } from './notas/tipos'
import { type OpcoesDoServidor, type Pedido, nota, servidorFalso } from './testes/servidorFalso'

const seloDe = (parcial: Partial<NonNullable<Nota['conflito']>> = {}): NonNullable<Nota['conflito']> => ({
  titulo: 'Da outra',
  corpo: 'corpo da outra\nsegunda linha',
  device_id: 'celular',
  conflict_at: new Date(2026, 9, 25, 15, 30).toISOString(),
  ...parcial,
})

const comSelo = () => nota({ id: 'n1', titulo: 'Minha', corpo: 'meu corpo', conflito: seloDe() })
const semSelo = () => nota({ id: 'n2', titulo: 'Tranquila', corpo: 'tudo certo' })

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  delete (window as { matchMedia?: unknown }).matchMedia
})

async function abrirNotas(notas: Nota[] = [comSelo(), semSelo()], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([], { notas, ...opcoes })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/marcadores')).toBe(true))
  await act(async () => {})
  return srv
}

const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && (metodo === undefined || p.metodo === metodo))
const resolucoes = (srv: { pedidos: Pedido[] }, id = 'n1') => pedidosA(srv, `/api/notas/${id}/resolver-conflito`, 'POST')
const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!
const abrir = (id: string) => fireEvent.click(cartao(id).querySelector('[data-abrir]')!)
const dialogo = () => screen.getByRole('dialog')
const campoTitulo = () => within(dialogo()).getByRole<HTMLInputElement>('textbox', { name: 'Título' })
const selo = () => document.querySelector<HTMLElement>('.dialogo .selo')
const botao = (nome: string) => within(dialogo()).getByRole('button', { name: nome })

describe('o aviso no cartão', () => {
  it('só a nota com selo leva "Versão em conflito"', async () => {
    await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    expect(within(cartao('n1')).getByText('Versão em conflito')).toBeTruthy()
    expect(within(cartao('n2')).queryByText('Versão em conflito')).toBeNull()
  })

  it('a lista e os detalhes também mostram o aviso', async () => {
    await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    fireEvent.click(screen.getByRole('button', { name: 'Lista' }))
    await waitFor(() => expect(document.querySelector('.nota-linha')).toBeTruthy())
    expect(within(cartao('n1')).getByText('Versão em conflito')).toBeTruthy()
    expect(within(cartao('n2')).queryByText('Versão em conflito')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Detalhes' }))
    await waitFor(() => expect(document.querySelector('table.detalhes')).toBeTruthy())
    expect(within(cartao('n1')).getByText('Versão em conflito')).toBeTruthy()
    expect(within(cartao('n2')).queryByText('Versão em conflito')).toBeNull()
  })
})

describe('a nota aberta com selo', () => {
  it('mostra a outra versão (título e corpo) e as duas escolhas; sem selo, nada disso', async () => {
    await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    const bloco = selo()!
    expect(within(bloco).getByText('Da outra')).toBeTruthy()
    expect(within(bloco).getByRole('region', { name: 'Texto da outra versão' }).textContent).toBe('corpo da outra\nsegunda linha')
    expect(botao('Manter esta')).toBeTruthy()
    expect(botao('Usar a outra')).toBeTruthy()
    // Abrir e fechar sem escolher não grava nada.
    fireEvent.keyDown(dialogo(), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    abrir('n2')
    expect(document.querySelector('.dialogo .selo')).toBeNull()
    expect(within(dialogo()).queryByRole('button', { name: 'Usar a outra' })).toBeNull()
  })

  it('"Manter esta" manda manter com a versão da nota, tira o selo e deixa o texto', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    fireEvent.click(botao('Manter esta'))
    await waitFor(() => expect(document.querySelector('.dialogo .selo')).toBeNull())
    expect(resolucoes(srv)).toHaveLength(1)
    expect(resolucoes(srv)[0]!.corpo).toEqual({ base_version: 1, escolha: 'manter' })
    expect(campoTitulo().value).toBe('Minha')
    expect(srv.notas.find((n) => n.id === 'n1')).toMatchObject({ titulo: 'Minha', corpo: 'meu corpo', version: 2 })
    expect(document.querySelector('.aviso-regiao')!.textContent).toContain('Ficou esta versão')

    // Fechar depois da escolha não manda PATCH: a janela não ficou "suja".
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(pedidosA(srv, '/api/notas/n1', 'PATCH')).toHaveLength(0)
    expect(within(cartao('n1')).queryByText('Versão em conflito')).toBeNull()
  })

  it('"Usar a outra" manda trocar, e a janela passa a mostrar o título e o corpo da outra', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    fireEvent.click(botao('Usar a outra'))
    await waitFor(() => expect(document.querySelector('.dialogo .selo')).toBeNull())
    expect(resolucoes(srv)[0]!.corpo).toEqual({ base_version: 1, escolha: 'trocar' })
    expect(campoTitulo().value).toBe('Da outra')
    const linhas = within(dialogo())
      .getAllByRole<HTMLTextAreaElement>('textbox')
      .filter((e) => e.tagName === 'TEXTAREA')
      .map((e) => e.value)
    expect(linhas).toEqual(['corpo da outra', 'segunda linha'])
    expect(document.activeElement).toBe(campoTitulo())

    // Sem PATCH sobrando ao fechar, e o cartão mostra o texto da outra.
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(pedidosA(srv, '/api/notas/n1', 'PATCH')).toHaveLength(0)
    expect(await screen.findByText('Da outra', { selector: 'h3' })).toBeTruthy()
    expect(within(cartao('n1')).queryByText('Versão em conflito')).toBeNull()
  })

  it('o que foi digitado e ainda não gravado vai antes da escolha, na versão em que o Agent está', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    fireEvent.change(campoTitulo(), { target: { value: 'Minha, editada' } })
    fireEvent.click(botao('Manter esta'))
    await waitFor(() => expect(resolucoes(srv)).toHaveLength(1))
    const patch = pedidosA(srv, '/api/notas/n1', 'PATCH')
    expect(patch).toHaveLength(1)
    expect(patch[0]!.corpo).toEqual({ titulo: 'Minha, editada', base_version: 1 })
    // A escolha parte da versão que o PATCH devolveu.
    expect(resolucoes(srv)[0]!.corpo).toEqual({ base_version: 2, escolha: 'manter' })
    await waitFor(() => expect(document.querySelector('.dialogo .selo')).toBeNull())
    expect(campoTitulo().value).toBe('Minha, editada')
  })

  it('409 (a nota mudou em outra aba): lê a nota de novo e diz o que houve, sem perder o texto', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    srv.notaPorFora('n1', { titulo: 'Mudou lá' })
    fireEvent.click(botao('Usar a outra'))
    const alerta = await screen.findByRole('alert', undefined, { timeout: 3000 })
    expect(alerta.textContent).toContain('alterada em outra aba')
    expect(resolucoes(srv)).toHaveLength(1)
    // A nota foi lida de novo, o selo continua (era da nuvem), e o texto da janela ficou.
    expect(pedidosA(srv, '/api/notas/n1', 'GET').length).toBeGreaterThan(0)
    expect(campoTitulo().value).toBe('Minha')
    expect(document.querySelector('.dialogo .selo')).toBeTruthy()
    // Parada no conflito, a escolha espera o "Salvar por cima" ou o fechar.
    fireEvent.click(botao('Manter esta'))
    await act(async () => {})
    expect(resolucoes(srv)).toHaveLength(1)
  })

  it('409 porque o selo já saiu (resolvido em outro lugar): a janela relê e o bloco some', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    srv.notaPorFora('n1', { conflito: undefined })
    fireEvent.click(botao('Manter esta'))
    await screen.findByRole('alert', undefined, { timeout: 3000 })
    expect(resolucoes(srv)).toHaveLength(1)
    await waitFor(() => expect(document.querySelector('.dialogo .selo')).toBeNull())
  })

  it('um erro que não é conflito mantém o selo e a janela, com a frase do Agent', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    srv.interceptar((p) =>
      p.caminho.endsWith('/resolver-conflito')
        ? new Response(JSON.stringify({ error: { code: 'INTERNAL', message: 'banco travado', request_id: 'r' } }), { status: 500 })
        : undefined,
    )
    fireEvent.click(botao('Usar a outra'))
    const alerta = await screen.findByRole('alert', undefined, { timeout: 3000 })
    expect(alerta.textContent).toContain('banco travado')
    expect(document.querySelector('.dialogo .selo')).toBeTruthy()
    expect(campoTitulo().value).toBe('Minha')
  })

  it('clicar duas vezes na escolha manda um pedido só', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Minha', { selector: 'h3' })
    abrir('n1')
    const b = botao('Usar a outra')
    fireEvent.click(b)
    fireEvent.click(b)
    await waitFor(() => expect(document.querySelector('.dialogo .selo')).toBeNull())
    expect(resolucoes(srv)).toHaveLength(1)
  })
})
