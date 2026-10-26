// A aba Notas do painel do PC (ADR-022, N2) contra o Agent de mentira
// (testes/servidorFalso.ts): a aba, os marcadores e as formas de ver, a grade,
// o "Criar uma nota", a nota aberta (salvar sozinho, conflito, foco), o Arquivo e
// a Lixeira.
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos saem, o
// que a pessoa vê e onde o foco fica. A API das notas é dos testes Go
// (internal/agent/localapi); o editor por linhas, de notas/editorDeLinhas.test.tsx.
//
// A área de transferência é uma de mentira (vi.fn): este teste nunca toca a do
// sistema.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { type OpcoesDoServidor, type Pedido, marcador, nota, servidorFalso } from './testes/servidorFalso'
import type { Nota } from './notas/tipos'

let relogio = 0
const hora = (h: number) => new Date(2026, 9, 25, h, 0, relogio++).toISOString()

const compras = () =>
  nota({
    id: 'n1',
    titulo: 'Compras',
    corpo: '- [ ] leite\n- [ ] café\n- [x] pão',
    cor: 'amarelo',
    fixada: true,
    marcadores: ['m1'],
    updated_at: hora(9),
  })
const ideia = () => nota({ id: 'n2', titulo: 'Ideia', corpo: 'um texto\n1. primeiro\n2. segundo', updated_at: hora(10), ditado_de_origem: 'd9' })
const sem = () => nota({ id: 'n3', corpo: 'só corpo, sem título', updated_at: hora(8) })
const velha = () => nota({ id: 'n4', titulo: 'Velha', corpo: 'arquivada', arquivada: true, updated_at: hora(7) })
const lixo = () => nota({ id: 'n5', titulo: 'No lixo', corpo: 'apagada', deleted_at: hora(6), updated_at: hora(6) })
const marcadores = () => [marcador({ id: 'm1', nome: 'Compras' }), marcador({ id: 'm2', nome: 'Ideias' })]

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  delete (window as { matchMedia?: unknown }).matchMedia
  Reflect.deleteProperty(navigator, 'clipboard')
})

async function abrirNotas(notas: Nota[] = [compras(), ideia(), sem(), velha(), lixo()], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([], { notas, marcadores: marcadores(), ...opcoes })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/marcadores')).toBe(true))
  await act(async () => {})
  return srv
}

const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && (metodo === undefined || p.metodo === metodo))
const patches = (srv: { pedidos: Pedido[] }, id = 'n1') => pedidosA(srv, `/api/notas/${id}`, 'PATCH')
const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!
const cartoes = () => Array.from(document.querySelectorAll<HTMLElement>('.grade [data-nota]')).map((c) => c.dataset.nota)
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent
const dialogo = () => screen.getByRole('dialog')
const campoTitulo = () => within(dialogo()).getByRole<HTMLInputElement>('textbox', { name: 'Título' })
const linhasDoDialogo = () => within(dialogo()).getAllByRole<HTMLTextAreaElement>('textbox').filter((e) => e.tagName === 'TEXTAREA')

// chip é um botão da fileira de marcadores (o "Lixeira" e o "Notas" também são abas do topo).
const chip = async (nome: string | RegExp) => within(await screen.findByRole('group', { name: 'Marcadores' })).getByRole('button', { name: nome })

function abrir(id: string) {
  fireEvent.click(cartao(id).querySelector('[data-abrir]')!)
}

describe('a aba', () => {
  it('fica entre Histórico e Lixeira, abre as notas e usa a largura da janela; as outras abas voltam aos 46 rem', async () => {
    const srv = servidorFalso([], { notas: [compras()], marcadores: marcadores() })
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    const abas = (await screen.findByRole('group', { name: 'Mostrar' })).querySelectorAll('button')
    expect(Array.from(abas).map((b) => b.textContent)).toEqual(['Histórico', 'Notas', 'Lixeira', 'Configurações'])

    fireEvent.click(screen.getByRole('button', { name: 'Notas' }))
    expect(await screen.findByRole('heading', { name: 'Notas', level: 1 })).toBeTruthy()
    expect(document.querySelector('.painel')!.classList.contains('larga')).toBe(true)
    expect(screen.getByRole('button', { name: 'Notas' }).getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByRole('button', { name: 'Histórico' }).getAttribute('aria-pressed')).toBe('false')
    expect(screen.getByRole('searchbox', { name: 'Buscar nas notas' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    expect(document.querySelector('.painel')!.classList.contains('larga')).toBe(false)
    expect(screen.getByRole('searchbox', { name: 'Buscar no histórico' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Notas' }))
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    expect(document.querySelector('.painel')!.classList.contains('larga')).toBe(false)
    expect(screen.getByRole('button', { name: 'Notas' }).getAttribute('aria-pressed')).toBe('false')
  })

  it('a busca do topo busca nas notas (q), e trocar de aba a limpa', async () => {
    const srv = await abrirNotas()
    await screen.findByText('Compras', { selector: 'h3' })
    fireEvent.change(screen.getByRole('searchbox', { name: 'Buscar nas notas' }), { target: { value: 'ideia' } })
    await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/notas' && p.busca.get('q') === 'ideia')).toBe(true))
    await waitFor(() => expect(cartoes()).toEqual(['n2']))
    // O histórico, escondido, não lê a busca das notas.
    expect(srv.pedidos.some((p) => p.caminho === '/api/history' && p.busca.get('q') === 'ideia')).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    expect((screen.getByRole('searchbox', { name: 'Buscar no histórico' }) as HTMLInputElement).value).toBe('')
  })

  it('o painel da nuvem não tem a aba (o ambiente dela não traz as notas)', async () => {
    // O ambiente do PC traz Notas; sem ele, o Painel não desenha o botão.
    const { Painel } = await import('./Painel')
    const { ambienteLocal } = await import('./ambienteLocal')
    const { criarCliente } = await import('./clienteLocal')
    const srv = servidorFalso([])
    const ambiente = { ...ambienteLocal(criarCliente('tok', srv.buscar)), Notas: undefined }
    render(<Painel ambiente={ambiente} />)
    await screen.findByRole('group', { name: 'Mostrar' })
    expect(screen.queryByRole('button', { name: 'Notas' })).toBeNull()
  })
})

describe('a grade', () => {
  it('põe as fixadas em "Fixadas" e as outras em "Outras", pela última edição', async () => {
    await abrirNotas([compras(), ideia(), sem()])
    await screen.findByText('Ideia', { selector: 'h3' })
    const fixadas = screen.getByRole('region', { name: 'Fixadas' })
    const outras = screen.getByRole('region', { name: 'Outras' })
    expect(within(fixadas).getByText('Compras', { selector: 'h3' })).toBeTruthy()
    expect(Array.from(outras.querySelectorAll('[data-nota]')).map((c) => (c as HTMLElement).dataset.nota)).toEqual(['n2', 'n3'])
  })

  it('o cartão desenha o corpo pelo prefixo e esconde os marcados atrás de "+ N marcado"', async () => {
    await abrirNotas([compras(), ideia()])
    const c = await waitFor(() => {
      const el = cartao('n1')
      expect(el).toBeTruthy()
      return el
    })
    expect(c.getAttribute('data-cor')).toBe('amarelo')
    expect(c.querySelectorAll('.marcar-lista li')).toHaveLength(2)
    expect(within(c).queryByText('pão')).toBeNull()
    expect(within(c).getByText('+ 1 marcado')).toBeTruthy()
    expect(within(c).getByText('Compras', { selector: '.etiqueta' })).toBeTruthy()
    const outro = cartao('n2')
    expect(outro.querySelectorAll('ol li')).toHaveLength(2)
    expect(within(outro).getByText('veio de um ditado')).toBeTruthy()
  })

  it('a nota sem título se chama pelo começo do corpo', async () => {
    await abrirNotas([sem()])
    expect(await screen.findByRole('button', { name: 'só corpo, sem título' })).toBeTruthy()
  })

  it('sem nenhuma nota, mostra o estado vazio do painel', async () => {
    await abrirNotas([])
    expect(await screen.findByText('Nenhuma nota ainda.')).toBeTruthy()
  })
})

describe('os marcadores', () => {
  it('uma fileira: Notas, um chip por marcador (com a conta), Arquivo, Lixeira e Editar marcadores', async () => {
    await abrirNotas()
    const fila = await screen.findByRole('group', { name: 'Marcadores' })
    await waitFor(() => expect(within(fila).getByRole('button', { name: 'Compras 1' })).toBeTruthy())
    expect(Array.from(fila.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['Notas 3', 'Compras 1', 'Ideias 0', 'Arquivo', 'Lixeira', 'Editar marcadores'])
    expect(within(fila).getByRole('button', { name: 'Notas 3' }).getAttribute('aria-pressed')).toBe('true')
  })

  it('escolher um marcador pede ?marcador= e mostra só as notas dele; Notas volta a todas', async () => {
    const srv = await abrirNotas()
    const fila = await screen.findByRole('group', { name: 'Marcadores' })
    await waitFor(() => expect(within(fila).getByRole('button', { name: 'Compras 1' })).toBeTruthy())
    fireEvent.click(within(fila).getByRole('button', { name: 'Compras 1' }))
    await waitFor(() => expect(cartoes()).toEqual(['n1']))
    expect(srv.pedidos.some((p) => p.caminho === '/api/notas' && p.busca.get('marcador') === 'm1')).toBe(true)
    expect(within(fila).getByRole('button', { name: 'Compras 1' }).getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(within(fila).getByRole('button', { name: 'Notas 3' }))
    await waitFor(() => expect(cartoes()).toHaveLength(3))
  })

  it('um marcador sem nota mostra a frase certa', async () => {
    await abrirNotas()
    const fila = await screen.findByRole('group', { name: 'Marcadores' })
    await waitFor(() => expect(within(fila).getByRole('button', { name: 'Ideias 0' })).toBeTruthy())
    fireEvent.click(within(fila).getByRole('button', { name: 'Ideias 0' }))
    expect(await screen.findByText('Nenhuma nota com este marcador.')).toBeTruthy()
  })
})

describe('as formas de ver', () => {
  const forma = (nome: string) => screen.getByRole('button', { name: nome })

  it('quadradinhos, lista e detalhes; a escolha fica no aparelho', async () => {
    await abrirNotas([compras(), ideia()])
    await screen.findByText('Compras', { selector: 'h3' })
    expect(forma('Quadradinhos').getAttribute('aria-pressed')).toBe('true')

    fireEvent.click(forma('Lista'))
    expect(document.querySelector('.notas-lista')).toBeTruthy()
    expect(document.querySelector('.grade')).toBeNull()
    expect(window.localStorage.getItem('aurora.notas.forma')).toBe('lista')
    const linha = document.querySelector<HTMLElement>('.nota-linha[data-nota="n1"]')!
    expect(within(linha).getByText('Compras')).toBeTruthy()
    expect(linha.querySelector('.bolinha')?.getAttribute('data-cor')).toBe('amarelo')

    fireEvent.click(forma('Detalhes'))
    const tabela = screen.getByRole('table')
    expect(within(tabela).getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Título', 'Marcadores', 'Itens marcados', 'Cor', 'Editada'])
    const linhaDaTabela = within(tabela).getByRole('button', { name: 'Compras' }).closest('tr')!
    expect(within(linhaDaTabela).getByText('1/3')).toBeTruthy()
    expect(within(linhaDaTabela).getByText('Amarelo')).toBeTruthy()
    expect(window.localStorage.getItem('aurora.notas.forma')).toBe('detalhes')
  })

  it('volta à forma da última vez ao abrir de novo', async () => {
    window.localStorage.setItem('aurora.notas.forma', 'detalhes')
    await abrirNotas([compras()])
    expect(await screen.findByRole('table')).toBeTruthy()
    expect(forma('Detalhes').getAttribute('aria-pressed')).toBe('true')
  })

  it('no PC a grade começa com 4 colunas (de 1 a 6); mais e menos colunas ficam gravados', async () => {
    await abrirNotas([compras()])
    const grade = await waitFor(() => {
      const g = document.querySelector<HTMLElement>('.grade')
      expect(g).toBeTruthy()
      return g!
    })
    expect(grade.dataset.colunas).toBe('4')
    fireEvent.click(screen.getByRole('button', { name: 'Mais colunas' }))
    fireEvent.click(screen.getByRole('button', { name: 'Mais colunas' }))
    expect(grade.dataset.colunas).toBe('6')
    expect((screen.getByRole('button', { name: 'Mais colunas' }) as HTMLButtonElement).disabled).toBe(true)
    expect(window.localStorage.getItem('aurora.notas.colunas.pc')).toBe('6')
    for (let i = 0; i < 8; i++) fireEvent.click(screen.getByRole('button', { name: 'Menos colunas' }))
    expect(grade.dataset.colunas).toBe('1')
    expect((screen.getByRole('button', { name: 'Menos colunas' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('o número de colunas gravado volta ao reabrir, e um valor fora dos limites é levado a eles', async () => {
    window.localStorage.setItem('aurora.notas.colunas.pc', '9')
    await abrirNotas([compras()])
    const grade = await waitFor(() => {
      const g = document.querySelector<HTMLElement>('.grade')
      expect(g).toBeTruthy()
      return g!
    })
    expect(grade.dataset.colunas).toBe('6')
  })

  it('numa tela estreita a grade começa com 2 colunas (de 1 a 3) e guarda à parte', async () => {
    window.matchMedia = vi.fn().mockImplementation((consulta: string) => ({
      matches: consulta.includes('max-width: 640px'),
      media: consulta,
      addEventListener: () => {},
      removeEventListener: () => {},
    })) as unknown as typeof window.matchMedia
    await abrirNotas([compras()])
    const grade = await waitFor(() => {
      const g = document.querySelector<HTMLElement>('.grade')
      expect(g).toBeTruthy()
      return g!
    })
    expect(grade.dataset.colunas).toBe('2')
    fireEvent.click(screen.getByRole('button', { name: 'Mais colunas' }))
    expect(grade.dataset.colunas).toBe('3')
    expect((screen.getByRole('button', { name: 'Mais colunas' }) as HTMLButtonElement).disabled).toBe(true)
    expect(window.localStorage.getItem('aurora.notas.colunas.celular')).toBe('3')
    expect(window.localStorage.getItem('aurora.notas.colunas.pc')).toBeNull()
  })

  it('com o armazenamento do navegador recusando leitura e escrita, tudo funciona com o padrão', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('bloqueado')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('bloqueado')
    })
    await abrirNotas([compras()])
    const grade = await waitFor(() => {
      const g = document.querySelector<HTMLElement>('.grade')
      expect(g).toBeTruthy()
      return g!
    })
    expect(grade.dataset.colunas).toBe('4')
    fireEvent.click(screen.getByRole('button', { name: 'Mais colunas' }))
    expect(grade.dataset.colunas).toBe('5')
    fireEvent.click(forma('Lista'))
    expect(document.querySelector('.notas-lista')).toBeTruthy()
  })
})

describe('criar uma nota', () => {
  it('abre com o cursor no corpo (o título é opcional); fechar com texto grava (POST) e a nota aparece', async () => {
    const srv = await abrirNotas([])
    fireEvent.click(await screen.findByRole('button', { name: 'Criar uma nota…' }))
    const titulo = screen.getByRole('textbox', { name: 'Título' })
    // O ditado da nota nova cai no corpo, como no Keep (decisão do Mauro, 25-out-2026).
    expect(document.activeElement?.tagName).toBe('TEXTAREA')
    titulo.focus()
    fireEvent.change(titulo, { target: { value: 'Feira' } })
    fireEvent.keyDown(titulo, { key: 'Enter' })
    const corpo = screen.getAllByRole('textbox').find((e) => e.tagName === 'TEXTAREA')!
    expect(document.activeElement).toBe(corpo)
    fireEvent.change(corpo, { target: { value: '- maçã' } })
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(pedidosA(srv, '/api/notas', 'POST')).toHaveLength(1))
    expect(pedidosA(srv, '/api/notas', 'POST')[0]!.corpo).toEqual({ titulo: 'Feira', corpo: '- maçã', token: expect.any(String) })
    expect(await screen.findByText('Feira', { selector: 'h3' })).toBeTruthy()
    // A barra recolhida volta e recebe o foco.
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Criar uma nota…' })))
  })

  it('fechar sem nada escrito não grava nada (nem pelo Esc)', async () => {
    const srv = await abrirNotas([])
    fireEvent.click(await screen.findByRole('button', { name: 'Criar uma nota…' }))
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Criar uma nota…' }))
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Título' }), { key: 'Escape' })
    await screen.findByRole('button', { name: 'Criar uma nota…' })
    expect(pedidosA(srv, '/api/notas', 'POST')).toHaveLength(0)
  })

  it('uma lista de marcar vazia (só o item em branco) também não grava', async () => {
    const srv = await abrirNotas([])
    fireEvent.click(await screen.findByRole('button', { name: 'Nova lista de marcar' }))
    expect(screen.getByRole('checkbox')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await screen.findByRole('button', { name: 'Criar uma nota…' })
    expect(pedidosA(srv, '/api/notas', 'POST')).toHaveLength(0)
  })

  it('a nota grávida na lista de marcar vai com o prefixo, e o clique fora fecha e grava', async () => {
    const srv = await abrirNotas([])
    fireEvent.click(await screen.findByRole('button', { name: 'Nova lista de marcar' }))
    const item = screen.getAllByRole('textbox').find((e) => e.tagName === 'TEXTAREA')!
    fireEvent.change(item, { target: { value: 'ligar' } })
    fireEvent.mouseDown(document.body)
    await waitFor(() => expect(pedidosA(srv, '/api/notas', 'POST')).toHaveLength(1))
    expect(pedidosA(srv, '/api/notas', 'POST')[0]!.corpo).toEqual({ titulo: '', corpo: '- [ ] ligar', token: expect.any(String) })
  })

  it('se o Agent recusar, a caixa continua aberta com o texto e a frase', async () => {
    const srv = await abrirNotas([])
    srv.interceptar((p) => (p.metodo === 'POST' && p.caminho === '/api/notas' ? new Response(JSON.stringify({ error: { code: 'INTERNAL', message: 'disco cheio', request_id: 'r' } }), { status: 500 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Criar uma nota…' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'Título' }), { target: { value: 'Importante' } })
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    const alerta = await screen.findByRole('alert')
    expect(alerta.textContent).toContain('disco cheio')
    expect((screen.getByRole('textbox', { name: 'Título' }) as HTMLInputElement).value).toBe('Importante')
  })
})

describe('a nota aberta', () => {
  it('abre com Enter ou clique, com o foco no fim do texto, e é uma janela modal', async () => {
    await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    const botao = cartao('n1').querySelector<HTMLElement>('[data-abrir]')!
    botao.focus()
    fireEvent.keyDown(botao, { key: 'Enter' })
    const d = dialogo()
    expect(d.getAttribute('aria-modal')).toBe('true')
    expect(d.getAttribute('aria-label')).toBe('Nota: Compras')
    expect(d.getAttribute('data-cor')).toBe('amarelo')
    expect(campoTitulo().value).toBe('Compras')
    expect(linhasDoDialogo().map((l) => l.value)).toEqual(['leite', 'café', 'pão'])
    // Com texto, o foco vai para o fim do último item ainda aberto: o pão,
    // marcado, fica embaixo e não recebe o ditado.
    expect(document.activeElement).toBe(linhasDoDialogo()[1])
    expect(within(d).getByText(/editada às \d\d:\d\d/)).toBeTruthy()
  })

  it('mostra "veio de um ditado" e os marcadores', async () => {
    await abrirNotas([ideia(), compras()])
    await screen.findByText('Ideia', { selector: 'h3' })
    abrir('n2')
    expect(within(dialogo()).getByText(/veio de um ditado/)).toBeTruthy()
    fireEvent.keyDown(dialogo(), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    abrir('n1')
    expect(within(dialogo()).getByText('Compras', { selector: '.etiqueta' })).toBeTruthy()
  })

  it('Esc fecha, salva o que mudou (com base_version) e devolve o foco ao cartão', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.change(campoTitulo(), { target: { value: 'Compras da feira' } })
    fireEvent.keyDown(campoTitulo(), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patches(srv)).toHaveLength(1)
    expect(patches(srv)[0]!.corpo).toEqual({ titulo: 'Compras da feira', base_version: 1 })
    expect(document.activeElement).toBe(cartao('n1').querySelector('[data-abrir]'))
    expect(await screen.findByText('Compras da feira', { selector: 'h3' })).toBeTruthy()
  })

  it('fechar sem mudar nada não manda PATCH', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patches(srv)).toHaveLength(0)
  })

  it('clicar no véu fecha; clicar dentro da janela não', async () => {
    await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.mouseDown(dialogo())
    expect(screen.queryByRole('dialog')).toBeTruthy()
    fireEvent.mouseDown(document.querySelector('.veu')!)
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('o Tab não sai da janela: no último campo volta ao primeiro, e Shift+Tab no primeiro vai ao último', async () => {
    await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    const focaveis = Array.from(dialogo().querySelectorAll<HTMLElement>('button, input, textarea')).filter((e) => !(e as HTMLButtonElement).disabled)
    const primeiro = focaveis[0]!
    const ultimo = focaveis[focaveis.length - 1]!
    expect(ultimo.textContent).toBe('Fechar')
    ultimo.focus()
    fireEvent.keyDown(ultimo, { key: 'Tab' })
    expect(document.activeElement).toBe(primeiro)
    fireEvent.keyDown(primeiro, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(ultimo)
  })

  it('salva sozinha um instante depois da última tecla, e só o que mudou', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.change(linhasDoDialogo()[0]!, { target: { value: 'leite integral' } })
    expect(patches(srv)).toHaveLength(0)
    await waitFor(() => expect(patches(srv)).toHaveLength(1), { timeout: 3000 })
    expect(patches(srv)[0]!.corpo).toEqual({ corpo: '- [ ] leite integral\n- [ ] café\n- [x] pão', base_version: 1 })
    // A janela continua aberta; o PATCH seguinte usa a versão nova.
    expect(screen.queryByRole('dialog')).toBeTruthy()
    fireEvent.change(campoTitulo(), { target: { value: 'Mercado' } })
    await waitFor(() => expect(patches(srv)).toHaveLength(2), { timeout: 3000 })
    expect(patches(srv)[1]!.corpo).toEqual({ titulo: 'Mercado', base_version: 2 })
  })

  it('cor, fixar e arquivar vão na hora, sem esperar a pausa do texto', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Cor' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Azul' }))
    await waitFor(() => expect(patches(srv)).toHaveLength(1), { timeout: 600 })
    expect(patches(srv)[0]!.corpo).toEqual({ cor: 'azul', base_version: 1 })
    expect(dialogo().getAttribute('data-cor')).toBe('azul')
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Desafixar' }))
    await waitFor(() => expect(patches(srv)).toHaveLength(2), { timeout: 600 })
    expect(patches(srv)[1]!.corpo).toEqual({ fixada: false, base_version: 2 })
  })

  it('"Sem cor" tira a cor (cor null)', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Cor' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Sem cor' }))
    await waitFor(() => expect(patches(srv)).toHaveLength(1), { timeout: 600 })
    expect(patches(srv)[0]!.corpo).toEqual({ cor: null, base_version: 1 })
  })

  it('marcar um item o leva para baixo da divisória e grava a ordem da tela', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.click(within(dialogo()).getByRole('checkbox', { name: 'leite' }))
    expect(within(dialogo()).getByRole('button', { name: /2 marcados/ })).toBeTruthy()
    fireEvent.keyDown(dialogo(), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patches(srv)[0]!.corpo).toEqual({ corpo: '- [ ] café\n- [x] pão\n- [x] leite', base_version: 1 })
  })

  it('Mais: Desmarcar tudo e Apagar marcados', async () => {
    await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Apagar marcados' }))
    expect(linhasDoDialogo().map((l) => l.value)).toEqual(['leite', 'café'])
    expect(within(dialogo()).queryByRole('button', { name: /\d+ marcados?/ })).toBeNull()
    fireEvent.click(within(dialogo()).getByRole('checkbox', { name: 'café' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Desmarcar tudo' }))
    expect(within(dialogo()).queryByRole('button', { name: /\d+ marcados?/ })).toBeNull()
  })

  it('Formatar troca o prefixo da linha do cursor', async () => {
    const srv = await abrirNotas([ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    abrir('n2')
    const primeira = linhasDoDialogo()[0]!
    fireEvent.focus(primeira)
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Formatar' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Lista de marcar' }))
    fireEvent.keyDown(dialogo(), { key: 'Escape' })
    await waitFor(() => expect(patches(srv, 'n2')).toHaveLength(1))
    expect(patches(srv, 'n2')[0]!.corpo).toEqual({ corpo: '- [ ] um texto\n1. primeiro\n2. segundo', base_version: 1 })
  })

  it('o botão de imagem existe e está desligado ("Imagens chegam depois")', async () => {
    await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    const imagem = within(dialogo()).getByRole<HTMLButtonElement>('button', { name: 'Imagem' })
    expect(imagem.disabled).toBe(true)
    expect(imagem.title).toBe('Imagens chegam depois')
  })

  it('Esc num menu fecha só o menu, não a janela', async () => {
    await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    const mais = within(dialogo()).getByRole('button', { name: 'Mais' })
    fireEvent.click(mais)
    expect(mais.getAttribute('aria-expanded')).toBe('true')
    fireEvent.keyDown(within(dialogo()).getByRole('button', { name: 'Copiar texto' }), { key: 'Escape' })
    expect(mais.getAttribute('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(mais)
    expect(screen.queryByRole('dialog')).toBeTruthy()
  })

  it('Copiar texto escreve o título e o corpo na área de transferência, só dentro do clique', async () => {
    const escrever = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: escrever }, configurable: true })
    await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    expect(escrever).not.toHaveBeenCalled()
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Copiar texto' }))
    expect(escrever).toHaveBeenCalledWith('Compras\n\n- [ ] leite\n- [ ] café\n- [x] pão')
    await waitFor(() => expect(aviso()).toContain('Copiado.'))
  })
})

describe('o conflito', () => {
  it('versão desatualizada: a nota vermelha diz o que houve, o texto fica, e "Salvar por cima" grava sobre a versão atual', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    srv.notaPorFora('n1', { titulo: 'Mudou lá' })
    fireEvent.change(campoTitulo(), { target: { value: 'Mudei aqui' } })
    const alerta = await screen.findByRole('alert', undefined, { timeout: 3000 })
    expect(alerta.textContent).toContain('alterada em outra aba')
    // O texto da pessoa continua na tela, e não há outro PATCH sozinho.
    expect(campoTitulo().value).toBe('Mudei aqui')
    expect(patches(srv)).toHaveLength(1)
    await new Promise((r) => setTimeout(r, 1000))
    expect(patches(srv)).toHaveLength(1)

    fireEvent.click(within(alerta).getByRole('button', { name: 'Salvar por cima' }))
    await waitFor(() => expect(patches(srv)).toHaveLength(2))
    expect(patches(srv)[1]!.corpo).toEqual({ titulo: 'Mudei aqui', base_version: 2 })
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
    expect(srv.notas[0]!.titulo).toBe('Mudei aqui')
  })

  it('a nota que foi para a lixeira em outra aba: a janela diz isso e guarda o texto para copiar', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    srv.notaParaALixeiraPorFora('n1')
    fireEvent.change(campoTitulo(), { target: { value: 'Ainda quero' } })
    const alerta = await screen.findByRole('alert', undefined, { timeout: 3000 })
    expect(alerta.textContent).toContain('lixeira')
    expect(campoTitulo().value).toBe('Ainda quero')
    // Fechar fecha sem outro PATCH.
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patches(srv)).toHaveLength(1)
  })

  it('um erro que não é conflito mantém a janela aberta com o texto e a frase do Agent', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    srv.interceptar((p) =>
      p.metodo === 'PATCH' ? new Response(JSON.stringify({ error: { code: 'INTERNAL', message: 'banco travado', request_id: 'r' } }), { status: 500 }) : undefined,
    )
    fireEvent.change(campoTitulo(), { target: { value: 'Novo' } })
    const alerta = await screen.findByRole('alert', undefined, { timeout: 3000 })
    expect(alerta.textContent).toContain('banco travado')
    fireEvent.click(screen.getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(patches(srv).length).toBeGreaterThanOrEqual(2))
    expect(screen.queryByRole('dialog')).toBeTruthy()
    expect(campoTitulo().value).toBe('Novo')
  })
})

describe('fixar, cor e arquivar no cartão', () => {
  it('fixar sobe o cartão para "Fixadas"', async () => {
    const srv = await abrirNotas([compras(), ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Fixar a nota/ }))
    await waitFor(() => expect(patches(srv, 'n2')).toHaveLength(1))
    expect(patches(srv, 'n2')[0]!.corpo).toEqual({ fixada: true, base_version: 1 })
    await waitFor(() => expect(within(screen.getByRole('region', { name: 'Fixadas' })).getByText('Ideia', { selector: 'h3' })).toBeTruthy())
  })

  it('a cor do menu do cartão muda o fundo', async () => {
    const srv = await abrirNotas([ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Cor da nota/ }))
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: 'Verde' }))
    await waitFor(() => expect(cartao('n2').getAttribute('data-cor')).toBe('verde'))
    expect(patches(srv, 'n2')[0]!.corpo).toEqual({ cor: 'verde', base_version: 1 })
  })

  it('PATCH com a nota desatualizada na lista: avisa e lê de novo', async () => {
    const srv = await abrirNotas([ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    srv.notaPorFora('n2', { titulo: 'Ideia nova' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Fixar a nota/ }))
    await waitFor(() => expect(aviso()).toContain('mudou em outra aba'))
    await screen.findByText('Ideia nova', { selector: 'h3' })
  })
})

describe('arquivar', () => {
  it('o cartão sai, o aviso oferece Desfazer, e a nota está no Arquivo', async () => {
    const srv = await abrirNotas([compras(), ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Arquivar a nota/ }))
    await waitFor(() => expect(document.querySelector('[data-nota="n2"]')).toBeNull())
    expect(patches(srv, 'n2')[0]!.corpo).toEqual({ arquivada: true, base_version: 1 })
    expect(aviso()).toContain('Nota arquivada.')
    fireEvent.click(screen.getByRole('button', { name: 'Arquivo' }))
    expect(await screen.findByText('Ideia', { selector: 'h3' })).toBeTruthy()
    expect(srv.pedidos.some((p) => p.caminho === '/api/notas' && p.busca.get('vista') === 'arquivo')).toBe(true)
  })

  it('Desfazer devolve a nota, com a versão nova', async () => {
    const srv = await abrirNotas([compras(), ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Arquivar a nota/ }))
    await waitFor(() => expect(document.querySelector('[data-nota="n2"]')).toBeNull())
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(patches(srv, 'n2')).toHaveLength(2))
    expect(patches(srv, 'n2')[1]!.corpo).toEqual({ arquivada: false, base_version: 2 })
    await screen.findByText('Ideia', { selector: 'h3' })
  })

  it('o Arquivo desarquiva pelo cartão, e diz quando está vazio', async () => {
    const srv = await abrirNotas([velha()])
    fireEvent.click(await screen.findByRole('button', { name: 'Arquivo' }))
    await screen.findByText('Velha', { selector: 'h3' })
    fireEvent.click(within(cartao('n4')).getByRole('button', { name: /Desarquivar a nota/ }))
    await waitFor(() => expect(patches(srv, 'n4')).toHaveLength(1))
    expect(patches(srv, 'n4')[0]!.corpo).toEqual({ arquivada: false, base_version: 1 })
    expect(await screen.findByText('O arquivo está vazio.')).toBeTruthy()
    expect(aviso()).toContain('Nota desarquivada.')
  })

  it('arquivar pela janela fecha a janela, tira o cartão e leva o foco ao vizinho', async () => {
    const srv = await abrirNotas([compras(), ideia(), sem()])
    await screen.findByText('Ideia', { selector: 'h3' })
    abrir('n2')
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Arquivar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patches(srv, 'n2')[0]!.corpo).toEqual({ arquivada: true, base_version: 1 })
    expect(document.querySelector('[data-nota="n2"]')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(cartao('n3').querySelector('[data-abrir]')))
  })

  it('a nota arquivada aberta oferece Desarquivar', async () => {
    const srv = await abrirNotas([velha()])
    fireEvent.click(await screen.findByRole('button', { name: 'Arquivo' }))
    await screen.findByText('Velha', { selector: 'h3' })
    abrir('n4')
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Desarquivar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patches(srv, 'n4')[0]!.corpo).toEqual({ arquivada: false, base_version: 1 })
  })
})

describe('a lixeira das notas', () => {
  it('Mover para a lixeira (menu do cartão): DELETE, o cartão sai, e o Desfazer restaura', async () => {
    const srv = await abrirNotas([compras(), ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Mais opções/ }))
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: 'Mover para a lixeira' }))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n2', 'DELETE')).toHaveLength(1))
    await waitFor(() => expect(document.querySelector('[data-nota="n2"]')).toBeNull())
    expect(aviso()).toContain('Nota movida para a lixeira.')
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n2/restaurar', 'POST')).toHaveLength(1))
    await screen.findByText('Ideia', { selector: 'h3' })
  })

  it('pela janela: o que foi escrito vai junto, e depois a nota vai para a lixeira', async () => {
    const srv = await abrirNotas([compras()])
    await screen.findByText('Compras', { selector: 'h3' })
    abrir('n1')
    fireEvent.change(campoTitulo(), { target: { value: 'Compras!' } })
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mais' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Mover para a lixeira' }))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n1', 'DELETE')).toHaveLength(1))
    expect(patches(srv)[0]!.corpo).toEqual({ titulo: 'Compras!', base_version: 1 })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('a Lixeira lista as apagadas com Restaurar; restaurar tira da lixeira', async () => {
    const srv = await abrirNotas([lixo()])
    fireEvent.click(await chip('Lixeira'))
    expect(await screen.findByText('No lixo')).toBeTruthy()
    expect(srv.pedidos.some((p) => p.caminho === '/api/notas' && p.busca.get('vista') === 'lixeira')).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Restaurar' }))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n5/restaurar', 'POST')).toHaveLength(1))
    expect(await screen.findByText('A lixeira das notas está vazia.')).toBeTruthy()
    expect(aviso()).toContain('Nota restaurada.')
  })

  it('Excluir definitivamente pergunta na própria linha, com o foco no Cancelar', async () => {
    const srv = await abrirNotas([lixo()])
    fireEvent.click(await chip('Lixeira'))
    await screen.findByText('No lixo')
    fireEvent.click(screen.getByRole('button', { name: 'Excluir definitivamente' }))
    expect(screen.getByText('Excluir para sempre? Não dá para desfazer.')).toBeTruthy()
    const cancelar = screen.getByRole('button', { name: 'Cancelar' })
    expect(document.activeElement).toBe(cancelar)
    // Sem confirmar, nada foi apagado.
    expect(pedidosA(srv, '/api/notas/n5/definitivo')).toHaveLength(0)
    // Cancelar (ou Esc) devolve o foco ao botão que perguntou.
    fireEvent.click(cancelar)
    expect(screen.queryByText(/Excluir para sempre/)).toBeNull()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Excluir definitivamente' }))
    fireEvent.click(screen.getByRole('button', { name: 'Excluir definitivamente' }))
    fireEvent.keyDown(screen.getByRole('group', { name: /Excluir para sempre/ }), { key: 'Escape' })
    expect(screen.queryByText(/Excluir para sempre/)).toBeNull()
    expect(pedidosA(srv, '/api/notas/n5/definitivo')).toHaveLength(0)
  })

  it('confirmar apaga de vez (DELETE /definitivo) e a nota some', async () => {
    const srv = await abrirNotas([lixo()])
    fireEvent.click(await chip('Lixeira'))
    await screen.findByText('No lixo')
    fireEvent.click(screen.getByRole('button', { name: 'Excluir definitivamente' }))
    fireEvent.click(screen.getByRole('button', { name: 'Excluir' }))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n5/definitivo', 'DELETE')).toHaveLength(1))
    expect(await screen.findByText('A lixeira das notas está vazia.')).toBeTruthy()
    expect(aviso()).toContain('Nota excluída para sempre.')
    expect(srv.notas).toHaveLength(0)
  })

  it('as formas de ver não aparecem na Lixeira (a linha da lixeira é uma só)', async () => {
    await abrirNotas([lixo()])
    fireEvent.click(await chip('Lixeira'))
    await screen.findByText('No lixo')
    expect(screen.queryByRole('group', { name: 'Forma de ver' })).toBeNull()
  })
})

describe('cartão: copiar e abrir', () => {
  it('"Copiar texto" do cartão escreve na área de transferência de mentira', async () => {
    const escrever = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: escrever }, configurable: true })
    await abrirNotas([ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Mais opções/ }))
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: 'Copiar texto' }))
    expect(escrever).toHaveBeenCalledWith('Ideia\n\num texto\n1. primeiro\n2. segundo')
  })

  it('as ferramentas do cartão não abrem a nota; só o corpo abre', async () => {
    await abrirNotas([ideia()])
    await screen.findByText('Ideia', { selector: 'h3' })
    fireEvent.click(within(cartao('n2')).getByRole('button', { name: /Fixar a nota/ }))
    expect(screen.queryByRole('dialog')).toBeNull()
    fireEvent.click(cartao('n2').querySelector('[data-abrir]')!)
    expect(screen.queryByRole('dialog')).toBeTruthy()
  })
})

describe('quando a lista falha', () => {
  it('mostra a frase e o Tentar de novo', async () => {
    const srv = servidorFalso([], { notas: [compras()], marcadores: marcadores() })
    let falhando = true
    srv.interceptar((p) =>
      falhando && p.metodo === 'GET' && p.caminho === '/api/notas'
        ? new Response(JSON.stringify({ error: { code: 'INTERNAL', message: 'banco travado', request_id: 'r' } }), { status: 500 })
        : undefined,
    )
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
    expect((await screen.findByText(/Não consegui carregar as notas/)).textContent).toContain('banco travado')
    falhando = false
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Compras', { selector: 'h3' })).toBeTruthy()
  })
})
