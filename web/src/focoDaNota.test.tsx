// O aviso "o cursor está dentro de uma nota" (ADR-022, "Ditado feito dentro da
// nota") contra o Agent de mentira (testes/servidorFalso.ts): quando o painel manda
// o PUT /api/notas/foco e o DELETE, o token da caixa "Criar uma nota", o descartar
// da caixa que fecha vazia e a ligação do token à nota que nasce.
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos saem, em
// que ordem, com que corpo. Que o Agent guarde o aviso só na memória, o venza em
// 60 s e esconda o ditado é dos testes Go (internal/agent/notafoco, winruntime e
// localapi). Nada aqui ditou, e nada toca o teclado nem a área de transferência.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { RENOVA_O_FOCO_EM } from './notas/useFocoDaNota'
import { type OpcoesDoServidor, type Pedido, ditado, marcador, nota, servidorFalso } from './testes/servidorFalso'
import type { Nota } from './notas/tipos'

const compras = () => nota({ id: 'n1', titulo: 'Compras', corpo: '- leite\n- café', updated_at: new Date(2026, 9, 25, 9).toISOString() })
const ideia = () => nota({ id: 'n2', titulo: 'Ideia', corpo: 'um texto', updated_at: new Date(2026, 9, 25, 8).toISOString() })

let janelaComFoco = true

beforeEach(() => {
  window.localStorage.clear()
  janelaComFoco = true
  vi.spyOn(document, 'hasFocus').mockImplementation(() => janelaComFoco)
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

async function abrirNotas(notas: Nota[] = [compras(), ideia()], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([ditado({ id: 'd1', created_at: new Date(2026, 9, 25, 10).toISOString() })], {
    notas,
    marcadores: [marcador({ id: 'm1', nome: 'Compras' })],
    ...opcoes,
  })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/marcadores')).toBe(true))
  await act(async () => {})
  return srv
}

const doFoco = (srv: { pedidos: Pedido[] }, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === '/api/notas/foco' && (metodo === undefined || p.metodo === metodo))
const dialogo = () => screen.getByRole('dialog')
const linhas = () => within(dialogo()).getAllByRole<HTMLTextAreaElement>('textbox').filter((e) => e.tagName === 'TEXTAREA')
const campoTitulo = () => within(dialogo()).getByRole<HTMLInputElement>('textbox', { name: 'Título' })
const abrir = (id: string) => fireEvent.click(document.querySelector<HTMLElement>(`[data-nota="${id}"] [data-abrir]`)!)
const aberta = async (id = 'n1') => {
  abrir(id)
  await screen.findByRole('dialog')
  await act(async () => {})
}
const sequencia = (srv: { pedidos: Pedido[] }) => doFoco(srv).map((p) => `${p.metodo} ${JSON.stringify(p.corpo)}`)

describe('a nota que já existe', () => {
  it('abrir a nota põe o cursor no corpo e avisa o Agent, com o id', async () => {
    const srv = await abrirNotas()
    await aberta('n1')
    expect(document.activeElement).toBe(linhas().at(-1))
    expect(sequencia(srv)).toEqual(['PUT {"nota":"n1"}'])
    expect(srv.foco()).toBe('n1')
  })

  it('do corpo para o título e de volta, o aviso não pisca', async () => {
    const srv = await abrirNotas()
    await aberta()
    campoTitulo().focus()
    await act(async () => {})
    linhas()[0]!.focus()
    await act(async () => {})
    expect(sequencia(srv)).toEqual(['PUT {"nota":"n1"}'])
    expect(srv.foco()).toBe('n1')
  })

  it('o foco que sai para um botão da janela tira o aviso, e volta ao campo, põe de novo', async () => {
    const srv = await abrirNotas()
    await aberta()
    screen.getByRole('button', { name: 'Fixar' }).focus()
    await waitFor(() => expect(srv.foco()).toBeNull())
    expect(sequencia(srv)).toEqual(['PUT {"nota":"n1"}', 'DELETE {"nota":"n1"}'])

    campoTitulo().focus()
    await waitFor(() => expect(srv.foco()).toBe('n1'))
    expect(sequencia(srv)).toEqual(['PUT {"nota":"n1"}', 'DELETE {"nota":"n1"}', 'PUT {"nota":"n1"}'])
  })

  it('a janela do navegador que perde o foco tira o aviso; ao voltar, com o cursor no campo, põe de novo', async () => {
    const srv = await abrirNotas()
    await aberta()
    expect(srv.foco()).toBe('n1')

    janelaComFoco = false
    await act(async () => {
      window.dispatchEvent(new Event('blur'))
    })
    await waitFor(() => expect(srv.foco()).toBeNull())

    janelaComFoco = true
    await act(async () => {
      window.dispatchEvent(new Event('focus'))
    })
    await waitFor(() => expect(srv.foco()).toBe('n1'))
    expect(sequencia(srv)).toEqual(['PUT {"nota":"n1"}', 'DELETE {"nota":"n1"}', 'PUT {"nota":"n1"}'])
  })

  it('a janela que volta com o cursor fora dos campos não avisa nada', async () => {
    const srv = await abrirNotas()
    await aberta()
    janelaComFoco = false
    await act(async () => {
      window.dispatchEvent(new Event('blur'))
    })
    screen.getByRole('button', { name: 'Fixar' }).focus()
    janelaComFoco = true
    await act(async () => {
      window.dispatchEvent(new Event('focus'))
    })
    await act(async () => {})
    expect(srv.foco()).toBeNull()
    expect(doFoco(srv, 'PUT')).toHaveLength(1)
  })

  it('a janela aberta sem o foco do navegador só avisa quando ele chega', async () => {
    janelaComFoco = false
    const srv = await abrirNotas()
    await aberta()
    expect(doFoco(srv)).toHaveLength(0)

    janelaComFoco = true
    await act(async () => {
      window.dispatchEvent(new Event('focus'))
    })
    await waitFor(() => expect(srv.foco()).toBe('n1'))
  })

  it('sair da página (pagehide) ou esconder a aba tira o aviso', async () => {
    const srv = await abrirNotas()
    await aberta()
    await act(async () => {
      window.dispatchEvent(new Event('pagehide'))
    })
    await waitFor(() => expect(srv.foco()).toBeNull())

    await act(async () => {
      window.dispatchEvent(new Event('focus'))
    })
    await waitFor(() => expect(srv.foco()).toBe('n1'))
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await waitFor(() => expect(srv.foco()).toBeNull())
  })

  it('enquanto o cursor fica, renova a cada 20 s, para o Agent não deixar o aviso vencer', async () => {
    const srv = await abrirNotas()
    // O relógio de verdade segue valendo (o waitFor precisa dele): só se
    // espiam as renovações para disparar a de 20 s na mão.
    const ligou = vi.spyOn(globalThis, 'setInterval')
    const desligou = vi.spyOn(globalThis, 'clearInterval')
    await aberta()
    const i = ligou.mock.calls.findIndex((c) => c[1] === RENOVA_O_FOCO_EM)
    expect(i).toBeGreaterThanOrEqual(0)
    const renovar = ligou.mock.calls[i]![0] as () => void
    const id = ligou.mock.results[i]!.value
    expect(doFoco(srv, 'PUT')).toHaveLength(1)

    await act(async () => renovar())
    await waitFor(() => expect(doFoco(srv, 'PUT')).toHaveLength(2))
    await act(async () => renovar())
    await waitFor(() => expect(doFoco(srv, 'PUT')).toHaveLength(3))
    expect(desligou).not.toHaveBeenCalledWith(id)

    // Sem foco, o relógio da renovação para.
    screen.getByRole('button', { name: 'Fixar' }).focus()
    await waitFor(() => expect(srv.foco()).toBeNull())
    expect(desligou).toHaveBeenCalledWith(id)
  })

  it('Esc fecha a nota e tira o aviso', async () => {
    const srv = await abrirNotas()
    const ligou = vi.spyOn(globalThis, 'setInterval')
    const desligou = vi.spyOn(globalThis, 'clearInterval')
    await aberta()
    const id = ligou.mock.results[ligou.mock.calls.findIndex((c) => c[1] === RENOVA_O_FOCO_EM)]!.value
    fireEvent.keyDown(dialogo(), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(srv.foco()).toBeNull())
    expect(sequencia(srv)).toEqual(['PUT {"nota":"n1"}', 'DELETE {"nota":"n1"}'])
    // Fechada a nota, a renovação para de vez.
    expect(desligou).toHaveBeenCalledWith(id)
  })

  it('o botão Fechar e o clique no véu também tiram o aviso', async () => {
    const srv = await abrirNotas()
    await aberta()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(srv.foco()).toBeNull())

    await aberta('n2')
    expect(srv.foco()).toBe('n2')
    fireEvent.mouseDown(document.querySelector('.veu')!)
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(srv.foco()).toBeNull())
  })

  it('o DELETE atrasado de uma nota não apaga o aviso da nota que abriu depois', async () => {
    const srv = await abrirNotas()
    await aberta('n1')
    fireEvent.keyDown(dialogo(), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await aberta('n2')
    await waitFor(() => expect(srv.foco()).toBe('n2'))
    expect(sequencia(srv)).toEqual(['PUT {"nota":"n1"}', 'DELETE {"nota":"n1"}', 'PUT {"nota":"n2"}'])
  })

  it('o Agent que recusa o aviso (a nota foi para a lixeira em outra aba) não atrapalha a edição', async () => {
    const srv = await abrirNotas([compras()])
    srv.interceptar((p) =>
      p.caminho === '/api/notas/foco' ? new Response('{"error":{"code":"CONFLICT","message":"x"}}', { status: 409 }) : undefined,
    )
    await aberta()
    fireEvent.change(campoTitulo(), { target: { value: 'Compras da semana' } })
    expect(campoTitulo().value).toBe('Compras da semana')
    expect(screen.queryByText(/Não consegui/)).toBeNull()
    expect(doFoco(srv, 'PUT')).toHaveLength(1)
    expect(srv.foco()).toBeNull()
  })
})

describe('o Criar uma nota', () => {
  const abrirCaixa = async (srv: { pedidos: Pedido[] }) => {
    fireEvent.click(await screen.findByRole('button', { name: 'Criar uma nota…' }))
    await act(async () => {})
    expect(srv).toBeTruthy()
  }
  const tokenDe = (srv: { pedidos: Pedido[] }) => {
    const c = doFoco(srv, 'PUT').at(-1)!.corpo as { token?: string }
    return c.token!
  }

  it('abre a caixa com o cursor no corpo e avisa com um token, sem id de nota', async () => {
    const srv = await abrirNotas()
    await abrirCaixa(srv)
    const puts = doFoco(srv, 'PUT')
    expect(puts).toHaveLength(1)
    expect(Object.keys(puts[0]!.corpo!)).toEqual(['token'])
    expect(tokenDe(srv)).toMatch(/^[A-Za-z0-9_-]{1,64}$/)
    expect(srv.foco()).toBe(`nova:${tokenDe(srv)}`)
  })

  it('cada abertura da caixa tem o seu token', async () => {
    const srv = await abrirNotas()
    await abrirCaixa(srv)
    const primeiro = tokenDe(srv)
    fireEvent.keyDown(screen.getByLabelText('Texto da nova nota'), { key: 'Escape' })
    await screen.findByRole('button', { name: 'Criar uma nota…' })
    await abrirCaixa(srv)
    expect(tokenDe(srv)).not.toBe(primeiro)
  })

  it('criar a nota leva o token, e os ditados feitos na caixa passam a ser os da nota', async () => {
    const srv = await abrirNotas()
    await abrirCaixa(srv)
    const token = tokenDe(srv)
    // O Agent esconde o ditado sob o token enquanto a caixa está aberta.
    srv.ditarNaCaixa(token, 'd1')
    expect(srv.virouNota.get('d1')).toBe(`nova:${token}`)

    const corpo = within(document.querySelector<HTMLElement>('.criar.aberto')!).getAllByRole<HTMLTextAreaElement>('textbox')[1]!
    fireEvent.change(corpo, { target: { value: 'Manda o relatório' } })
    fireEvent.keyDown(corpo, { key: 'Enter', ctrlKey: true })

    await waitFor(() => expect(srv.notas.some((n) => n.corpo === 'Manda o relatório')).toBe(true))
    const criada = srv.notas.find((n) => n.corpo === 'Manda o relatório')!
    const post = srv.pedidos.filter((p) => p.caminho === '/api/notas' && p.metodo === 'POST').at(-1)!
    expect(post.corpo).toMatchObject({ corpo: 'Manda o relatório', token })
    expect(srv.virouNota.get('d1')).toBe(criada.id)
    expect(criada.ditado_de_origem).toBeUndefined()
    await waitFor(() => expect(srv.foco()).toBeNull())
  })

  it('Esc com a caixa vazia descarta: o aviso sai e os ditados feitos nela voltam ao histórico', async () => {
    const srv = await abrirNotas()
    await abrirCaixa(srv)
    const token = tokenDe(srv)
    srv.ditarNaCaixa(token, 'd1')

    fireEvent.keyDown(screen.getByLabelText('Texto da nova nota'), { key: 'Escape' })
    await screen.findByRole('button', { name: 'Criar uma nota…' })
    await waitFor(() => expect(srv.virouNota.has('d1')).toBe(false))
    expect(srv.foco()).toBeNull()
    const descarte = doFoco(srv, 'DELETE').find((p) => (p.corpo as { descartar?: boolean }).descartar)
    expect(descarte?.corpo).toEqual({ token, descartar: true })
    expect(srv.pedidos.filter((p) => p.caminho === '/api/notas' && p.metodo === 'POST')).toHaveLength(0)
  })

  it('clicar fora da caixa vazia também descarta', async () => {
    const srv = await abrirNotas()
    await abrirCaixa(srv)
    const token = tokenDe(srv)
    srv.ditarNaCaixa(token, 'd1')
    fireEvent.mouseDown(document.body)
    await screen.findByRole('button', { name: 'Criar uma nota…' })
    await waitFor(() => expect(srv.virouNota.has('d1')).toBe(false))
  })

  it('fechar com texto não descarta: só a nota criada leva o token', async () => {
    const srv = await abrirNotas()
    await abrirCaixa(srv)
    const corpo = within(document.querySelector<HTMLElement>('.criar.aberto')!).getAllByRole<HTMLTextAreaElement>('textbox')[1]!
    fireEvent.change(corpo, { target: { value: 'algo' } })
    fireEvent.mouseDown(document.body)
    await waitFor(() => expect(srv.notas.some((n) => n.corpo === 'algo')).toBe(true))
    expect(doFoco(srv, 'DELETE').some((p) => (p.corpo as { descartar?: boolean }).descartar)).toBe(false)
  })

  it('o foco que sai da caixa para o título e volta não pisca', async () => {
    const srv = await abrirNotas()
    await abrirCaixa(srv)
    const caixa = document.querySelector<HTMLElement>('.criar.aberto')!
    within(caixa).getByRole<HTMLInputElement>('textbox', { name: 'Título' }).focus()
    await act(async () => {})
    expect(doFoco(srv)).toHaveLength(1)
  })
})
