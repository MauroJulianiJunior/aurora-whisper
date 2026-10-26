import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { SemSessao } from './api'
import { type Pedido, ditado, servidorFalso } from './testes/servidorFalso'
import { renderizarSoHistorico } from './testes/soHistorico'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 8, dia, hora, minuto).toISOString()

function base() {
  return [
    ditado({ id: 'd1', created_at: em(28, 14, 32), raw_text: 'Manda para o Tiago.', final_text: 'Manda para o Thiago.' }),
    ditado({ id: 'd2', created_at: em(28, 9, 5), raw_text: 'O vencimento é dia doze.', final_text: 'O vencimento é dia doze.' }),
    ditado({ id: 'd3', created_at: em(27, 20, 10), raw_text: 'Sobe no postgres local mesmo.', final_text: 'Sobe no PostgreSQL local mesmo.' }),
  ]
}

// cinco(): um por hora, do mais novo (p1) ao mais antigo (p5) — com
// porPagina 2, três páginas.
function cinco() {
  return [15, 14, 13, 12, 11].map((h, i) => ditado({ id: `p${i + 1}`, created_at: em(28, h), final_text: `Ditado número ${i + 1}.` }))
}

// soHistorico: o Painel sem a busca única (ADR-022, N6), com a busca do topo só
// no histórico, que é a da nuvem e a da Lixeira. Os testes da lista com busca
// (a mensagem de vazio de cada consulta, o cursor, a região viva) a usam.
function montar(itens = base(), opcoes: { porPagina?: number; soHistorico?: boolean } = {}) {
  const { soHistorico, ...doServidor } = opcoes
  const srv = servidorFalso(itens, doServidor)
  if (soHistorico) renderizarSoHistorico(srv.buscar)
  else render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  return srv
}

const linha = (texto: RegExp) => screen.findByRole('button', { name: texto })
const campoFinal = () => screen.getByLabelText<HTMLTextAreaElement>('Texto final')
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const anuncio = () => document.querySelector<HTMLElement>('.anuncio')!
const estado = () => document.querySelector<HTMLElement>('.estado')
const cabecalhos = () => screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
const listagens = (srv: { pedidos: Pedido[] }) => srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho === '/api/history')
const patches = (srv: { pedidos: Pedido[] }) => srv.pedidos.filter((p) => p.metodo === 'PATCH').map((p) => p.corpo?.base_version)
const ativo = () => document.activeElement

// voltarParaAba: a pessoa volta à aba do painel, passados alguns segundos.
function voltarParaAba(segundos = 3) {
  vi.setSystemTime(Date.now() + segundos * 1000)
  fireEvent.focus(window)
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 28, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('painel', () => {
  it('lista o histórico por dia, com o token em toda chamada', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    const [hoje, ontem] = screen.getAllByRole('heading', { level: 2 })
    expect(hoje?.textContent).toBe('Hoje')
    expect(ontem?.textContent).toBe('Ontem')
    expect(within(hoje!.parentElement!).getAllByRole('listitem')).toHaveLength(2)
    expect(srv.pedidos.every((p) => p.auth === 'Bearer tok')).toBe(true)
  })

  it('sem sessão: explica que se abre pela bandeja', async () => {
    render(<App sessao={Promise.reject(new SemSessao())} />)
    expect(await screen.findByRole('heading', { name: 'Abra o painel pela bandeja' })).toBeTruthy()
  })

  it('editar: mostra o bruto com o que mudou, e salva sobre a versão lida', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))

    expect(campoFinal().value).toBe('Manda para o Thiago.')
    const bruto = document.getElementById('bruto-d1')!
    expect(bruto.querySelector('del')?.textContent).toContain('Tiago.')
    expect(bruto.querySelector('ins')?.textContent).toContain('Thiago.')

    fireEvent.change(campoFinal(), { target: { value: 'Manda para o Thiago amanhã.' } })
    // O diff acompanha a edição: agora "amanhã." também entrou.
    expect(bruto.querySelector('ins')?.textContent).toContain('amanhã.')

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await linha(/Manda para o Thiago amanhã/)
    expect(srv.pedidos.find((p) => p.metodo === 'PATCH')?.corpo).toEqual({ base_version: 1, final_text: 'Manda para o Thiago amanhã.' })
    expect(aviso().textContent).toContain('Texto salvo.')
  })

  it('conflito (409): traz a versão atual e mantém o rascunho de quem edita', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Minha versão.' } })
    srv.alterarPorFora('d1', 'Versão da outra aba.')

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    expect((await screen.findByRole('alert')).textContent).toContain('alterado em outra aba')
    expect(campoFinal().value).toBe('Minha versão.')

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await linha(/Minha versão/)
    expect(patches(srv)).toEqual([1, 2])
  })

  it('Esc descarta a edição; Ctrl+Enter salva', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'descartado' } })
    fireEvent.keyDown(campoFinal(), { key: 'Escape' })
    expect(screen.queryByLabelText('Texto final')).toBeNull()

    fireEvent.click(await linha(/Manda para o Thiago/))
    expect(campoFinal().value).toBe('Manda para o Thiago.')
    fireEvent.change(campoFinal(), { target: { value: 'pelo atalho' } })
    fireEvent.keyDown(campoFinal(), { key: 'Enter', ctrlKey: true })
    await linha(/pelo atalho/)
    expect(srv.pedidos.filter((p) => p.metodo === 'PATCH')).toHaveLength(1)
  })

  it('rascunho de uma linha sobrevive a abrir outra, e aparece como não salvo', async () => {
    montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Manda para o Thiago já.' } })
    fireEvent.click(await linha(/O vencimento/))
    const pendente = await linha(/Manda para o Thiago\. não salvo/)
    fireEvent.click(pendente)
    expect(campoFinal().value).toBe('Manda para o Thiago já.')
  })

  it('mover para a lixeira some da lista e o Desfazer traz de volta, com o foco nela', async () => {
    const srv = montar()
    fireEvent.click(await linha(/O vencimento/))
    fireEvent.click(screen.getByRole('button', { name: 'Mover para a lixeira' }))

    await waitFor(() => expect(screen.queryByRole('button', { name: /O vencimento/ })).toBeNull())
    expect(srv.pedidos.some((p) => p.metodo === 'DELETE' && p.caminho === '/api/history/d2')).toBe(true)

    fireEvent.click(within(aviso()).getByRole('button', { name: 'Desfazer' }))
    const devolta = await linha(/O vencimento/)
    expect(srv.pedidos.some((p) => p.metodo === 'POST' && p.caminho === '/api/history/d2/restore')).toBe(true)
    await waitFor(() => expect(ativo()).toBe(devolta))
  })

  it('Desfazer com a Lixeira na tela tira de lá o ditado que voltou ao histórico', async () => {
    const srv = montar()
    fireEvent.click(await linha(/O vencimento/))
    fireEvent.click(screen.getByRole('button', { name: 'Mover para a lixeira' }))
    await waitFor(() => expect(aviso().textContent).toContain('Movido'))
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByRole('button', { name: 'Restaurar' })

    fireEvent.click(within(aviso()).getByRole('button', { name: 'Desfazer' }))
    expect(await screen.findByText('A lixeira está vazia.', { selector: '.estado p' })).toBeTruthy()
    expect(srv.pedidos.some((p) => p.metodo === 'POST' && p.caminho === '/api/history/d2/restore')).toBe(true)
    expect(aviso().textContent).toContain('de volta ao histórico')
    // Não sobrou linha para onde ir: o foco vai para a busca.
    await waitFor(() => expect(ativo()).toBe(screen.getByRole('searchbox')))
  })

  it('lixeira: lista o que foi apagado e restaura', async () => {
    const itens = base()
    itens[2]!.deleted_at = em(28, 17)
    const srv = montar(itens)
    await linha(/Manda para o Thiago/)
    expect(screen.queryByText(/PostgreSQL/)).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    const restaurar = await screen.findByRole('button', { name: 'Restaurar' })
    expect(srv.pedidos.some((p) => p.busca.get('trash') === 'true')).toBe(true)
    expect(screen.getByText(/ditado em/)).toBeTruthy()

    fireEvent.click(restaurar)
    expect(await screen.findByText('A lixeira está vazia.', { selector: '.estado p' })).toBeTruthy()
    expect(aviso().textContent).toContain('Restaurado')
  })

  it('busca vai para a API e mostra quando não acha nada', async () => {
    const srv = montar(base(), { soHistorico: true })
    await linha(/Manda para o Thiago/)
    const busca = screen.getByRole('searchbox', { name: 'Buscar no histórico' })

    fireEvent.change(busca, { target: { value: 'postgres' } })
    await waitFor(() => expect(screen.queryByRole('button', { name: /Manda para o Thiago/ })).toBeNull())
    expect(screen.getByRole('button', { name: /PostgreSQL/ })).toBeTruthy()
    expect(srv.pedidos.some((p) => p.busca.get('q') === 'postgres')).toBe(true)

    fireEvent.change(busca, { target: { value: 'inexistente' } })
    expect(await screen.findByText('Nada encontrado para “inexistente”.', { selector: '.estado p' })).toBeTruthy()
  })

  it('copiar leva o texto em edição', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'para colar' } })
    fireEvent.click(screen.getByRole('button', { name: 'Copiar' }))
    await waitFor(() => expect(aviso().textContent).toContain('Copiado.'))
    expect(writeText).toHaveBeenCalledWith('para colar')
  })

  // Pedido de Mauro no teste de 29-set-2026: copiar com um clique, sem
  // entrar no texto.
  it('copiar direto da linha leva o texto dela e não abre o editor', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    montar()
    await linha(/Manda para o Thiago/)
    fireEvent.click(screen.getByRole('button', { name: 'Copiar o ditado das 14:32' }))
    await waitFor(() => expect(aviso().textContent).toContain('Copiado.'))
    expect(writeText).toHaveBeenCalledWith('Manda para o Thiago.')
    expect(screen.queryByLabelText('Texto final')).toBeNull()
  })

  it('ditado que falhou: o copiar da linha leva o bruto; linha sem texto nenhum não tem copiar', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    montar([
      ditado({ id: 'f1', created_at: em(28, 10, 1), status: 'error', raw_text: 'Texto que o ASR ouviu.', final_text: '' }),
      ditado({ id: 'f2', created_at: em(28, 10, 2), status: 'error', raw_text: '', final_text: '' }),
    ])
    await linha(/Texto que o ASR ouviu/)
    expect(screen.queryByRole('button', { name: 'Copiar o ditado das 10:02' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Copiar o ditado das 10:01' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('Texto que o ASR ouviu.'))
    expect(writeText).toHaveBeenCalledTimes(1)
  })
})

describe('rascunho com versão-base', () => {
  it('outra aba salvou e a lista recarregou: o salvar ainda vai sobre a versão em que a edição começou', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Minha versão.' } })
    fireEvent.click(await linha(/O vencimento/))
    srv.alterarPorFora('d1', 'Versão da outra aba.')

    voltarParaAba()
    fireEvent.click(await linha(/Versão da outra aba\. não salvo/))
    expect(campoFinal().value).toBe('Minha versão.')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    // A lista trouxe a versão 2, mas a edição começou na 1: o 409 aparece,
    // em vez de a edição da outra aba ser apagada em silêncio.
    expect((await screen.findByRole('alert')).textContent).toContain('alterado em outra aba')
    expect(patches(srv)).toEqual([1])
    expect(srv.itens.find((i) => i.id === 'd1')?.final_text).toBe('Versão da outra aba.')
  })

  it('rascunho não tocado some ao trocar de linha: reabrir mostra o texto atual', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.click(await linha(/O vencimento/))
    srv.alterarPorFora('d1', 'Versão da outra aba.')

    voltarParaAba()
    const d1 = await linha(/Versão da outra aba/)
    expect(d1.textContent).not.toContain('não salvo')
    fireEvent.click(d1)
    expect(campoFinal().value).toBe('Versão da outra aba.')
  })

  it('só a versão mudou (lixeira e Desfazer em outra aba): salva de novo, sem nota de conflito', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Manda para o Thiago hoje.' } })
    srv.lixeiraPorFora('d1', true)

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await linha(/Manda para o Thiago hoje/)
    expect(patches(srv)).toEqual([1, 3])
    expect(screen.queryByRole('alert')).toBeNull()
    expect(aviso().textContent).toContain('Texto salvo.')
  })

  it('resposta tardia do salvar não fecha o editor de outra linha', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Manda para o Thiago já.' } })
    const soltar = srv.pausar((p) => p.metodo === 'PATCH')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    fireEvent.click(await linha(/O vencimento/))
    fireEvent.change(campoFinal(), { target: { value: 'O vencimento é dia treze.' } })

    soltar()
    await linha(/Manda para o Thiago já/)
    expect(campoFinal().value).toBe('O vencimento é dia treze.')
  })

  it('conflito que chega com a linha fechada: a nota aparece ao reabrir', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Minha versão.' } })
    srv.alterarPorFora('d1', 'Versão da outra aba.')
    const soltar = srv.pausar((p) => p.metodo === 'PATCH')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    fireEvent.click(await linha(/O vencimento/))

    soltar()
    fireEvent.click(await linha(/Versão da outra aba\. não salvo/))
    expect((await screen.findByRole('alert')).textContent).toContain('alterado em outra aba')
    expect(campoFinal().value).toBe('Minha versão.')
  })

  it('outra aba pôs o ditado na lixeira: diz isso, não grava nele e deixa o texto para copiar', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Minha versão.' } })
    srv.lixeiraPorFora('d1')

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    expect((await screen.findByRole('alert')).textContent).toContain('movido para a lixeira em outra aba')
    expect(patches(srv)).toEqual([1])
    expect(campoFinal().value).toBe('Minha versão.')
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Salvar' }).disabled).toBe(true)
  })

  it('o PATCH devolveu o ditado na lixeira: sai do histórico, e o aviso diz onde a edição foi parar', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Minha versão.' } })
    const naLixeira = { ...srv.itens[0], final_text: 'Minha versão.', version: 3, deleted_at: em(28, 17, 59) }
    srv.interceptar((p) => (p.metodo === 'PATCH' ? new Response(JSON.stringify(naLixeira), { status: 200 }) : undefined))

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(aviso().textContent).toContain('está na lixeira'))
    expect(screen.queryByRole('button', { name: /Minha versão/ })).toBeNull()
    expect(screen.queryByLabelText('Texto final')).toBeNull()
  })
})

describe('ligação com o Agent caída no meio do uso', () => {
  it('token recusado: faixa no topo, e o editor continua com o texto para copiar', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'texto longo que não pode sumir' } })
    srv.interceptar(() => new Response('unauthorized', { status: 401 }))
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))

    const faixa = await screen.findByRole('alert')
    expect(faixa.textContent).toContain('perdeu o acesso')
    expect(faixa.textContent).toContain('Abrir Aurora Whisper')
    expect(campoFinal().value).toBe('texto longo que não pode sumir')
    expect(screen.queryByRole('heading', { name: 'Abra o painel pela bandeja' })).toBeNull()
  })

  it('Agent fora do ar: explica o reinício, mantém o editor, e "Tentar de novo" serve à falha passageira', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'meu texto' } })
    srv.derrubar(true)
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))

    const faixa = await screen.findByRole('alert')
    expect(faixa.textContent).toContain('não está respondendo')
    expect(faixa.textContent).toContain('fechado ou reiniciado')
    expect(faixa.textContent).toContain('muda de endereço e de chave')
    expect(campoFinal().value).toBe('meu texto')

    srv.derrubar(false)
    const antes = listagens(srv).length
    fireEvent.click(within(faixa).getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(listagens(srv).length).toBe(antes + 1))
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
    expect(campoFinal().value).toBe('meu texto')
  })

  it('Agent fora do ar ao trocar de vista: a lista com os rascunhos fica na tela', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'meu rascunho' } })
    srv.derrubar(true)
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))

    expect((await screen.findByRole('alert')).textContent).toContain('não está respondendo')
    expect(await linha(/Manda para o Thiago\. não salvo/)).toBeTruthy()
    expect(screen.queryByText('Carregando…')).toBeNull()
  })
})

describe('Desfazer pelo teclado', () => {
  it('Ctrl+Z fora de campo devolve o ditado ao lugar dele, mesmo de uma página mais antiga, sem recarregar a lista', async () => {
    const srv = montar(cinco(), { porPagina: 2 })
    await linha(/Ditado número 1/)
    fireEvent.click(screen.getByRole('button', { name: 'Mostrar mais antigos' }))
    await linha(/Ditado número 4/)
    fireEvent.click(screen.getByRole('button', { name: 'Mostrar mais antigos' }))
    fireEvent.click(await linha(/Ditado número 5/))
    fireEvent.click(screen.getByRole('button', { name: 'Mover para a lixeira' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: /Ditado número 5/ })).toBeNull())
    expect(aviso().textContent).toContain('Ctrl+Z')

    const cargas = listagens(srv).length
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })
    const devolta = await linha(/Ditado número 5/)
    expect(srv.pedidos.some((p) => p.metodo === 'POST' && p.caminho === '/api/history/p5/restore')).toBe(true)
    expect(listagens(srv)).toHaveLength(cargas)
    const ordem = Array.from(document.querySelectorAll('[data-item]')).map((el) => el.getAttribute('data-item'))
    expect(ordem).toEqual(['p1', 'p2', 'p3', 'p4', 'p5'])
    await waitFor(() => expect(ativo()).toBe(devolta))
  })

  it('Ctrl+Z dentro de um campo de texto é do campo', async () => {
    const srv = montar()
    fireEvent.click(await linha(/O vencimento/))
    fireEvent.click(screen.getByRole('button', { name: 'Mover para a lixeira' }))
    await waitFor(() => expect(aviso().textContent).toContain('Movido'))

    fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'z', ctrlKey: true })
    expect(srv.pedidos.some((p) => p.caminho.endsWith('/restore'))).toBe(false)
    expect(aviso().textContent).toContain('Movido')
  })

  it('a etiqueta "editado" não aparece num ditado que só foi para a lixeira e voltou', async () => {
    const itens = base()
    itens[1]!.deleted_at = em(28, 17)
    montar(itens)
    await linha(/Manda para o Thiago/)
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Restaurar' }))
    await waitFor(() => expect(aviso().textContent).toContain('Restaurado'))
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    // Ao restaurar, o servidor subiu a versão, como o storage de verdade.
    const d2 = await linha(/O vencimento/)
    expect(d2.textContent).not.toContain('editado')
  })
})

describe('foco', () => {
  it('"Mostrar mais antigos" leva o foco à primeira linha nova, inclusive na última página', async () => {
    montar(cinco(), { porPagina: 2 })
    await linha(/Ditado número 1/)
    const mais = screen.getByRole('button', { name: 'Mostrar mais antigos' })
    mais.focus()
    fireEvent.click(mais)
    const p3 = await linha(/Ditado número 3/)
    await waitFor(() => expect(ativo()).toBe(p3))

    fireEvent.click(screen.getByRole('button', { name: 'Mostrar mais antigos' }))
    const p5 = await linha(/Ditado número 5/)
    expect(screen.queryByRole('button', { name: 'Mostrar mais antigos' })).toBeNull()
    await waitFor(() => expect(ativo()).toBe(p5))
  })

  it('resposta que chega depois não tira o foco da busca', async () => {
    const srv = montar()
    fireEvent.click(await linha(/O vencimento/))
    const soltar = srv.pausar((p) => p.metodo === 'DELETE')
    fireEvent.click(screen.getByRole('button', { name: 'Mover para a lixeira' }))
    const busca = screen.getByRole('searchbox')
    busca.focus()

    soltar()
    await waitFor(() => expect(screen.queryByRole('button', { name: /O vencimento/ })).toBeNull())
    // O efeito que move o foco roda depois do DOM: sem esvaziar a fila de
    // efeitos, a asserção passaria antes de ele ter a chance de errar.
    await act(async () => {})
    expect(ativo()).toBe(busca)
  })

  it('resposta que chega depois não tira o foco do editor de outra linha', async () => {
    const srv = montar()
    fireEvent.click(await linha(/O vencimento/))
    const soltar = srv.pausar((p) => p.metodo === 'DELETE')
    fireEvent.click(screen.getByRole('button', { name: 'Mover para a lixeira' }))
    // Enquanto o DELETE não responde, a pessoa abre outra linha e digita.
    fireEvent.click(await linha(/Manda para o Thiago/))
    const campo = campoFinal()
    await waitFor(() => expect(ativo()).toBe(campo))

    soltar()
    await waitFor(() => expect(screen.queryByRole('button', { name: /O vencimento/ })).toBeNull())
    await act(async () => {})
    expect(ativo()).toBe(campo)
  })

  it('Ctrl+K leva à busca; "/" não; dentro do campo de edição, Ctrl+K fica com o navegador', async () => {
    montar()
    const d1 = await linha(/Manda para o Thiago/)
    const busca = screen.getByRole('searchbox')
    expect(busca.getAttribute('aria-keyshortcuts')).toBe('Control+K')

    d1.focus()
    fireEvent.keyDown(d1, { key: '/' })
    expect(ativo()).toBe(d1)
    fireEvent.keyDown(d1, { key: 'k', ctrlKey: true })
    expect(ativo()).toBe(busca)

    fireEvent.click(d1)
    campoFinal().focus()
    fireEvent.keyDown(campoFinal(), { key: 'k', ctrlKey: true })
    expect(ativo()).toBe(campoFinal())
  })

  it('a altura do cabeçalho fixo vira o scroll-padding da página', async () => {
    let medir = () => {}
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(f: () => void) {
          medir = f
        }
        observe() {}
        disconnect() {}
      },
    )
    montar()
    await linha(/Manda para o Thiago/)
    const topo = document.querySelector<HTMLElement>('.topo')!
    Object.defineProperty(topo, 'offsetHeight', { configurable: true, value: 180 })
    act(() => medir())
    expect(document.documentElement.style.getPropertyValue('--altura-topo')).toBe('180px')
  })
})

describe('carga da lista', () => {
  it('"Hoje" é o dia da carga, não o da abertura da aba', async () => {
    vi.setSystemTime(new Date(2026, 8, 28, 23, 50))
    montar()
    await linha(/Manda para o Thiago/)
    expect(cabecalhos()).toEqual(['Hoje', 'Ontem'])

    vi.setSystemTime(new Date(2026, 8, 29, 0, 5))
    fireEvent.focus(window)
    await waitFor(() => expect(cabecalhos()).toEqual(['Ontem', 'Domingo, 27 de setembro']))
  })

  it('voltar à aba recarrega (no máximo a cada 2 s) sem fechar o editor nem perder o rascunho', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'meu rascunho' } })
    srv.alterarPorFora('d2', 'Texto novo da outra aba.')
    const antes = listagens(srv).length

    fireEvent.focus(window)
    expect(listagens(srv)).toHaveLength(antes)

    voltarParaAba()
    fireEvent(document, new Event('visibilitychange'))
    await linha(/Texto novo da outra aba/)
    expect(listagens(srv)).toHaveLength(antes + 1)
    expect(campoFinal().value).toBe('meu rascunho')
  })

  it('outra aba pôs na lixeira o ditado em edição e a pessoa voltou: o editor fica, com o texto e a nota', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'texto longo digitado' } })
    // Para mexer na outra aba, a pessoa saiu desta: a volta recarrega.
    srv.lixeiraPorFora('d1')

    voltarParaAba()
    expect((await screen.findByRole('alert')).textContent).toContain('movido para a lixeira em outra aba')
    expect(campoFinal().value).toBe('texto longo digitado')
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Salvar' }).disabled).toBe(true)
  })

  it('na volta à aba, a linha fechada com rascunho que foi para a lixeira fica, marcada; a sem rascunho sai', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Manda para o Thiago já.' } })
    fireEvent.click(await linha(/O vencimento/))
    srv.lixeiraPorFora('d1')
    srv.lixeiraPorFora('d3')

    voltarParaAba()
    await waitFor(() => expect(screen.queryByRole('button', { name: /PostgreSQL/ })).toBeNull())
    fireEvent.click(await linha(/Manda para o Thiago\..*não salvo.*na lixeira/))
    expect(campoFinal().value).toBe('Manda para o Thiago já.')
  })

  it('"Mostrar mais antigos" pedido junto com a recarga da volta à aba não se perde', async () => {
    const srv = montar(cinco(), { porPagina: 2 })
    await linha(/Ditado número 1/)
    const soltar = srv.pausar((p) => p.busca.has('cursor'))
    fireEvent.click(screen.getByRole('button', { name: 'Mostrar mais antigos' }))
    voltarParaAba()
    // A recarga responde antes da página, e troca a chave da lista.
    await waitFor(() => expect(listagens(srv)).toHaveLength(3))
    await waitFor(() => expect(screen.getByRole('main').getAttribute('aria-busy')).toBe('false'))

    soltar()
    await linha(/Ditado número 3/)
    const ordem = Array.from(document.querySelectorAll('[data-item]')).map((el) => el.getAttribute('data-item'))
    expect(ordem).toEqual(['p1', 'p2', 'p3', 'p4'])
  })

  it('enquanto a consulta nova não responde, a mensagem de vazio é a da consulta que está na tela', async () => {
    const srv = montar(base(), { soHistorico: true })
    await linha(/Manda para o Thiago/)
    const busca = screen.getByRole('searchbox')
    fireEvent.change(busca, { target: { value: 'inexistente' } })
    await screen.findByText('Nada encontrado para “inexistente”.', { selector: '.estado p' })

    const semBusca = () => listagens(srv).filter((p) => !p.busca.has('q')).length
    const antes = semBusca()
    const soltar = srv.pausar((p) => p.caminho === '/api/history' && !p.busca.has('q'))
    fireEvent.keyDown(busca, { key: 'Escape' })
    await waitFor(() => expect(semBusca()).toBe(antes + 1))
    expect(estado()?.textContent).toBe('Nada encontrado para “inexistente”.')
    expect(screen.queryByText('Nenhum ditado ainda.')).toBeNull()

    soltar()
    await linha(/Manda para o Thiago/)
  })

  it('"Mostrar mais antigos" pede a página com a busca e o cursor do resultado na tela', async () => {
    const srv = montar(cinco(), { porPagina: 2, soHistorico: true })
    await linha(/Ditado número 1/)
    const soltar = srv.pausar((p) => p.busca.get('q') === 'zzz')
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'zzz' } })
    await waitFor(() => expect(listagens(srv).some((p) => p.busca.get('q') === 'zzz')).toBe(true))

    fireEvent.click(screen.getByRole('button', { name: 'Mostrar mais antigos' }))
    await waitFor(() => expect(listagens(srv).some((p) => p.busca.has('cursor'))).toBe(true))
    const pagina = listagens(srv).find((p) => p.busca.has('cursor'))!
    expect(pagina.busca.get('cursor')).toBe('2')
    expect(pagina.busca.has('q')).toBe(false)
    soltar()
  })

  it('a região viva anuncia cada carga: quantos, nada encontrado, erro', async () => {
    const srv = montar(base(), { soHistorico: true })
    await linha(/Manda para o Thiago/)
    expect(anuncio().getAttribute('role')).toBe('status')
    expect(anuncio().textContent).toBe('3 ditados no histórico.')

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'inexistente' } })
    await waitFor(() => expect(anuncio().textContent).toBe('Nada encontrado para “inexistente”.'))

    srv.interceptar((p) => (p.busca.get('trash') === 'true' ? new Response('{}', { status: 500 }) : undefined))
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await waitFor(() => expect(anuncio().textContent).toBe('Não consegui carregar a lixeira.'))
  })
})

describe('o que a tela mostra', () => {
  it('ditado que falhou antes do texto final: mostra o bruto, o editor começa com ele, e Copiar nunca leva texto vazio', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    montar([ditado({ id: 'e1', created_at: em(28, 10), status: 'error', raw_text: 'texto que o ASR ouviu', final_text: '' })])

    const e1 = await linha(/texto que o ASR ouviu/)
    expect(e1.textContent).not.toContain('sem texto')
    fireEvent.click(e1)
    expect(campoFinal().value).toBe('texto que o ASR ouviu')
    expect(screen.getByText(/falhou antes de o texto final existir/).textContent).toContain('texto do ASR')

    const copiar = screen.getByRole<HTMLButtonElement>('button', { name: 'Copiar' })
    expect(copiar.disabled).toBe(false)
    fireEvent.change(campoFinal(), { target: { value: '   ' } })
    expect(copiar.disabled).toBe(true)
    fireEvent.click(copiar)
    expect(writeText).not.toHaveBeenCalled()
  })

  // O vermelho do sistema é só erro e risco, sempre com o texto do lado: a
  // etiqueta "falhou" e a nota do editor levam as classes que o index.css
  // pinta de vermelho (estilo.test.ts confere o CSS). As outras etiquetas
  // não.
  it('ditado com erro: a etiqueta "falhou" e a nota do editor são as de risco', async () => {
    montar([
      ditado({ id: 'e1', created_at: em(28, 10), status: 'error', raw_text: 'Texto que entrou mal.', final_text: 'Texto que entrou mal.' }),
      ditado({ id: 'ok', created_at: em(28, 9), final_text: 'Texto que entrou bem.' }),
    ])
    const e1 = await linha(/Texto que entrou mal/)
    expect(within(e1).getByText('falhou').className).toBe('etiqueta risco')
    expect((await linha(/Texto que entrou bem/)).querySelector('.etiqueta')).toBeNull()

    fireEvent.click(e1)
    expect(screen.getByText(/terminou com erro/).className).toBe('nota')
    // Abrir outro ditado guarda o rascunho, e a linha ganha "não salvo".
    fireEvent.change(campoFinal(), { target: { value: 'Mexi.' } })
    fireEvent.click(await linha(/Texto que entrou bem/))
    expect(within(await linha(/Texto que entrou mal/)).getByText('não salvo').className).toBe('etiqueta forte')
  })

  it('o diff não põe "saiu:" e "entrou:" no texto que se copia', async () => {
    montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    const diff = document.querySelector('#bruto-d1 .diff')!
    expect(diff.textContent).toBe('Manda para o Tiago. Thiago.')
  })

  it('"igual ao texto final" só quando o texto é igual de verdade, inclusive quebras de linha', async () => {
    montar()
    fireEvent.click(await linha(/O vencimento/))
    expect(document.getElementById('bruto-d2')!.textContent).toContain('igual ao texto final')
    fireEvent.change(campoFinal(), { target: { value: 'O vencimento é\ndia doze.' } })
    expect(document.getElementById('bruto-d2')!.textContent).not.toContain('igual ao texto final')
  })

  it('lixeira: agrupa e mostra a hora pela exclusão, o apagado por último no topo', async () => {
    const itens = [
      ditado({ id: 'a', created_at: em(25, 9), final_text: 'Criado dia 25, apagado hoje às 16h.', deleted_at: em(28, 16) }),
      ditado({ id: 'b', created_at: em(10, 9), final_text: 'Criado dia 10, apagado hoje às 17h.', deleted_at: em(28, 17) }),
      ditado({ id: 'c', created_at: em(27, 9), final_text: 'Criado ontem, apagado dia 8.', deleted_at: em(8, 12) }),
    ]
    montar(itens)
    fireEvent.click(await screen.findByRole('button', { name: 'Lixeira' }))
    await screen.findAllByRole('button', { name: 'Restaurar' })

    expect(cabecalhos()).toEqual(['Hoje', 'Terça-feira, 8 de setembro'])
    const horas = Array.from(document.querySelectorAll('[data-item] .hora')).map((t) => t.textContent)
    expect(horas).toEqual(['17:00', '16:00', '12:00'])
    expect(screen.getByText('Criado dia 10, apagado hoje às 17h.').closest('.linha')?.textContent).toContain('ditado em 10/09')
  })
})
