// O "Virar nota" do histórico do PC (ADR-022, N3) contra o Agent de mentira
// (testes/servidorFalso.ts): o botão no editor, o ditado que sai da lista, o aviso
// "Virou nota" com Abrir e Desfazer e os erros (o painel da nuvem, que não tem
// nada disso, é conferido em nuvem/virarNotaNuvem.test.tsx).
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos saem, o
// que a pessoa vê e onde o foco fica. O que o Agent grava (a transação, a
// retenção, o sync) é dos testes Go (internal/storage e internal/agent/localapi).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

function base() {
  return [
    ditado({ id: 'd1', created_at: em(25, 14, 32), raw_text: 'manda o relatorio', final_text: 'Manda o relatório amanhã cedo.' }),
    ditado({ id: 'd2', created_at: em(25, 9, 5), raw_text: 'leite cafe pao', final_text: '- leite\n- café\n- pão' }),
    // Falhou antes de o texto final existir: o editor começa com o bruto do ASR.
    ditado({ id: 'd3', created_at: em(24, 20, 10), raw_text: 'so o bruto', final_text: '', status: 'error' }),
  ]
}

function montar() {
  const srv = servidorFalso(base())
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  return srv
}

const linha = (texto: RegExp) => screen.findByRole('button', { name: texto })
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const botaoVirar = () => screen.getByRole<HTMLButtonElement>('button', { name: 'Virar nota' })
const campoFinal = () => screen.getByLabelText<HTMLTextAreaElement>('Texto final')
const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && (metodo === undefined || p.metodo === metodo))
const ativo = () => document.activeElement

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 25, 18, 0))
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('o botão Virar nota no editor do histórico', () => {
  it('fica ao lado do Copiar, só com o editor aberto', async () => {
    montar()
    await linha(/Manda o relatório/)
    expect(screen.queryByRole('button', { name: 'Virar nota' })).toBeNull()
    fireEvent.click(await linha(/Manda o relatório/))
    const acoes = botaoVirar().parentElement!
    const nomes = within(acoes).getAllByRole('button').map((b) => b.textContent)
    expect(nomes.indexOf('Virar nota')).toBe(nomes.indexOf('Copiar') + 1)
    expect(botaoVirar().disabled).toBe(false)
  })

  it('com edição por salvar, espera o Salvar: a nota nasce do texto salvo', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.change(campoFinal(), { target: { value: 'Manda o relatório amanhã cedo já.' } })
    expect(botaoVirar().disabled).toBe(true)
    expect(botaoVirar().title).toBe('Salve a edição antes de virar nota.')
    fireEvent.click(botaoVirar())
    expect(pedidosA(srv, '/api/history/d1/virar-nota')).toHaveLength(0)

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    // Salvo, o editor fecha; aberto de novo, o botão está pronto.
    fireEvent.click(await linha(/Manda o relatório amanhã cedo já\./))
    expect(botaoVirar().disabled).toBe(false)
  })

  it('texto vazio no editor: desligado, como o Copiar', async () => {
    montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.change(campoFinal(), { target: { value: '   ' } })
    expect(botaoVirar().disabled).toBe(true)
  })
})

describe('virar nota', () => {
  it('pede ao Agent, tira o ditado da lista e avisa "Virou nota" com Abrir e Desfazer', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())

    await waitFor(() => expect(screen.queryByRole('button', { name: /Manda o relatório/ })).toBeNull())
    const pedidos = pedidosA(srv, '/api/history/d1/virar-nota', 'POST')
    expect(pedidos).toHaveLength(1)
    expect(pedidos[0]!.auth).toBe('Bearer tok')
    // A nota nasceu com o texto do ditado, sem título, e com a origem.
    expect(srv.notas).toHaveLength(1)
    expect(srv.notas[0]).toMatchObject({ corpo: 'Manda o relatório amanhã cedo.', titulo: '', ditado_de_origem: 'd1', cor: null })
    // O aviso, na região viva, com as duas ações; os outros ditados continuam.
    expect(aviso().textContent).toContain('Virou nota')
    expect(within(aviso()).getByRole('button', { name: 'Abrir' })).toBeTruthy()
    expect(within(aviso()).getByRole('button', { name: 'Desfazer' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /O vencimento|leite/ })).toBeTruthy()
    // O foco não se perde: vai para a linha vizinha.
    await waitFor(() => expect(ativo()).toBe(screen.getByRole('button', { name: /leite/ })))
  })

  it('o ditado não volta na próxima carga da lista (o Agent o esconde)', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(screen.queryByRole('button', { name: /Manda o relatório/ })).toBeNull())
    // Outra carga do histórico: busca vazia -> a mesma busca.
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await screen.findByRole('button', { name: /leite/ })
    expect(screen.queryByRole('button', { name: /Manda o relatório/ })).toBeNull()
    expect(srv.virouNota.get('d1')).toBe(srv.notas[0]!.id)
  })

  it('o ditado sem texto final vira nota com o bruto', async () => {
    const srv = montar()
    fireEvent.click(await linha(/so o bruto/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    expect(srv.notas[0]!.corpo).toBe('so o bruto')
  })

  it('a lista dita "- " chega à nota como veio (o painel não converte nada)', async () => {
    const srv = montar()
    fireEvent.click(await linha(/leite/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    expect(srv.notas[0]!.corpo).toBe('- leite\n- café\n- pão')
  })

  it('Abrir leva à aba Notas com a nota na janela', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    fireEvent.click(await within(aviso()).findByRole('button', { name: 'Abrir' }))

    const janela = await screen.findByRole('dialog')
    expect(within(janela).getByText('Manda o relatório amanhã cedo.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Notas' }).getAttribute('aria-pressed')).toBe('true')
    expect(pedidosA(srv, `/api/notas/${srv.notas[0]!.id}`, 'GET').length).toBeGreaterThan(0)
    // O aviso saiu com o clique.
    expect(within(aviso()).queryByRole('button', { name: 'Abrir' })).toBeNull()
  })

  it('Abrir não reabre a nota ao voltar para a aba Notas depois', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    fireEvent.click(await within(aviso()).findByRole('button', { name: 'Abrir' }))
    await screen.findByRole('dialog')
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    fireEvent.click(screen.getByRole('button', { name: 'Notas' }))
    await act(async () => {})
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('Desfazer devolve o ditado ao histórico, tira a nota intacta e põe o foco nele', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    const notaId = srv.notas[0]!.id

    fireEvent.click(within(aviso()).getByRole('button', { name: 'Desfazer' }))
    const devolta = await linha(/Manda o relatório/)
    expect(pedidosA(srv, `/api/notas/${notaId}/desfazer-virar`, 'POST')).toHaveLength(1)
    expect(srv.notas).toHaveLength(0)
    expect(srv.virouNota.size).toBe(0)
    await waitFor(() => expect(ativo()).toBe(devolta))
    expect(aviso().textContent).toBe('Ditado de volta ao histórico.')
  })

  it('Desfazer com a nota editada: o ditado volta e o aviso diz que a nota ficou', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    srv.notaPorFora(srv.notas[0]!.id, { corpo: 'mexida em outra aba' })

    fireEvent.click(within(aviso()).getByRole('button', { name: 'Desfazer' }))
    await linha(/Manda o relatório/)
    expect(srv.notas).toHaveLength(1)
    expect(aviso().textContent).toBe('Ditado de volta ao histórico. A nota ficou, porque você a editou.')
  })

  it('Ctrl+Z fora de campo de texto faz o Desfazer enquanto o aviso existe', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    ;(document.activeElement as HTMLElement | null)?.blur()
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })
    await linha(/Manda o relatório/)
    expect(srv.notas).toHaveLength(0)
  })

  it('o aviso some sozinho, como o do Desfazer da lixeira (8 s), sem Abrir nem Desfazer em cima dele', async () => {
    vi.useRealTimers()
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    expect(aviso().textContent).toContain('Virou nota')
    // Só confere que existe uma pílula enquanto espera; o tempo é o de tempoDoAviso.
    expect(aviso().querySelectorAll('.aviso-acao')).toHaveLength(2)
  })
})

describe('virar nota: quando não dá', () => {
  it('o Agent recusa (o ditado foi para a lixeira em outra aba): o aviso diz o motivo e o ditado fica', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    srv.itens.find((i) => i.id === 'd1')!.deleted_at = new Date().toISOString()
    fireEvent.click(botaoVirar())

    await waitFor(() => expect(aviso().textContent).toContain('o ditado está na lixeira'))
    expect(aviso().textContent).toContain('Não deu certo')
    expect(srv.notas).toHaveLength(0)
    // O editor continua aberto, com o texto, para copiar.
    expect(campoFinal().value).toBe('Manda o relatório amanhã cedo.')
    expect(botaoVirar().disabled).toBe(false)
  })

  it('o Agent diz que o ditado já virou nota (outra aba): sai da lista e o aviso explica', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    srv.virouNota.set('d1', 'nota-x')
    fireEvent.click(botaoVirar())

    await waitFor(() => expect(screen.queryByRole('button', { name: /Manda o relatório/ })).toBeNull())
    expect(aviso().textContent).toBe('este ditado já virou nota')
    expect(within(aviso()).queryByRole('button', { name: 'Abrir' })).toBeNull()
  })

  it('o Agent fora do ar: erro na pílula, o ditado continua na lista', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    srv.interceptar((p) => (p.caminho.endsWith('/virar-nota') ? new Response('', { status: 500 }) : undefined))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(aviso().textContent).toContain('Não deu certo'))
    expect(campoFinal().value).toBe('Manda o relatório amanhã cedo.')
  })

  it('o Desfazer que o Agent recusa mostra o erro, sem tirar o ditado de onde está', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda o relatório/))
    fireEvent.click(botaoVirar())
    await waitFor(() => expect(srv.notas).toHaveLength(1))
    srv.virouNota.clear() // o ditado já voltou por outro lado
    fireEvent.click(within(aviso()).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(aviso().textContent).toContain('Não deu certo'))
    expect(screen.queryByRole('button', { name: /Manda o relatório/ })).toBeNull()
  })
})
