// Os marcadores da aba Notas (ADR-022, N4) contra o Agent de mentira
// (testes/servidorFalso.ts): a janela "Editar marcadores", o botão de marcador da nota
// e do cartão, a etiqueta com × e a sugestão de marcador.
//
// Como o notasNoPC.test.tsx, prova o que o painel FAZ com o Agent: quais pedidos saem,
// o que a pessoa vê e onde o foco fica. A regra da sugestão, por tabela, está em
// notas/sugestao.test.ts. Nada aqui toca o sistema real (sem área de transferência).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { Nota } from './notas/tipos'
import { type OpcoesDoServidor, type Pedido, marcador, nota, servidorFalso } from './testes/servidorFalso'

let relogio = 0
const hora = (h: number) => new Date(2026, 9, 25, h, 0, relogio++).toISOString()

// Duas notas do Compras, uma do Escritório, e uma lista solta parecida com as do Compras.
const compras = () => nota({ id: 'n1', titulo: 'Compras', corpo: '- [ ] leite\n- [ ] café\n- [x] pão', marcadores: ['m1'], updated_at: hora(9) })
const mercado = () => nota({ id: 'n2', titulo: 'Mercado', corpo: '- leite\n- ovos\n- café', marcadores: ['m1'], updated_at: hora(10) })
const reuniao = () => nota({ id: 'n3', titulo: 'Reunião', corpo: 'conferir o extrato', marcadores: ['m2'], updated_at: hora(8) })
const sabado = () => nota({ id: 'n4', titulo: 'Lista do sábado', corpo: '- leite\n- pão\n- ovos', updated_at: hora(11) })
const marcadores = () => [marcador({ id: 'm1', nome: 'Compras' }), marcador({ id: 'm2', nome: 'Escritório' }), marcador({ id: 'm3', nome: 'Viagem' })]

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  delete (window as { matchMedia?: unknown }).matchMedia
})

async function abrirNotas(notas: Nota[] = [compras(), mercado(), reuniao(), sabado()], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([], { notas, marcadores: marcadores(), ...opcoes })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
  await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/marcadores')).toBe(true))
  await act(async () => {})
  return srv
}

const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && (metodo === undefined || p.metodo === metodo))
const cartao = (id: string) => document.querySelector<HTMLElement>(`[data-nota="${id}"]`)!
const dialogo = () => screen.getByRole('dialog')
const linhasDoDialogo = () => within(dialogo()).getAllByRole<HTMLTextAreaElement>('textbox').filter((e) => e.tagName === 'TEXTAREA')
const campoTitulo = () => within(dialogo()).getByRole<HTMLInputElement>('textbox', { name: 'Título' })
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent

const chip = async (nome: string | RegExp) => within(await screen.findByRole('group', { name: 'Marcadores' })).getByRole('button', { name: nome })
const chips = async () => Array.from((await screen.findByRole('group', { name: 'Marcadores' })).querySelectorAll('button')).map((b) => b.textContent)

function abrir(id: string) {
  fireEvent.click(cartao(id).querySelector('[data-abrir]')!)
}

// --- Editar marcadores ---

const janela = () => screen.getByRole('dialog', { name: 'Editar marcadores' })
const campoCriar = () => within(janela()).getByRole<HTMLInputElement>('textbox', { name: 'Criar marcador' })
const botaoCriar = () => within(janela()).getByRole<HTMLButtonElement>('button', { name: 'Criar' })
const linhaDo = (nome: string) => within(janela()).getByText(nome, { selector: '.nome-m' }).closest('li')!

async function abrirEditor() {
  fireEvent.click(await chip('Editar marcadores'))
  await screen.findByRole('dialog', { name: 'Editar marcadores' })
}

function digitar(campo: HTMLElement, valor: string) {
  fireEvent.change(campo, { target: { value: valor } })
}

describe('Editar marcadores: abrir e fechar', () => {
  it('é o último chip da fileira, tracejado, e abre a janela com o foco no campo de criar', async () => {
    await abrirNotas()
    expect(await chips()).toEqual(['Notas 4', 'Compras 2', 'Escritório 1', 'Viagem 0', 'Arquivo', 'Lixeira', 'Editar marcadores'])
    const editar = await chip('Editar marcadores')
    expect(editar.classList.contains('editar')).toBe(true)
    expect(editar.getAttribute('aria-haspopup')).toBe('dialog')
    fireEvent.click(editar)
    const j = await screen.findByRole('dialog', { name: 'Editar marcadores' })
    expect(j.getAttribute('aria-modal')).toBe('true')
    expect(document.activeElement).toBe(campoCriar())
  })

  it('lista os marcadores com a conta de notas: "2 notas", "1 nota", "nenhuma nota"', async () => {
    await abrirNotas()
    await abrirEditor()
    expect(within(linhaDo('Compras')).getByText('2 notas')).toBeTruthy()
    expect(within(linhaDo('Escritório')).getByText('1 nota')).toBeTruthy()
    expect(within(linhaDo('Viagem')).getByText('nenhuma nota')).toBeTruthy()
  })

  it('a conta inclui as notas do Arquivo (apagar o marcador tira dele também)', async () => {
    const arquivada = nota({ id: 'n5', titulo: 'Velha', corpo: 'x', arquivada: true, marcadores: ['m2'], updated_at: hora(7) })
    await abrirNotas([compras(), mercado(), reuniao(), arquivada])
    expect(await chips()).toContain('Escritório 1')
    await abrirEditor()
    await waitFor(() => expect(within(linhaDo('Escritório')).getByText('2 notas')).toBeTruthy())
  })

  it('Esc, Fechar e o véu fecham, e o foco volta ao chip', async () => {
    await abrirNotas()
    for (const fechar of [
      () => fireEvent.keyDown(campoCriar(), { key: 'Escape' }),
      () => fireEvent.click(within(janela()).getByRole('button', { name: 'Fechar' })),
      () => fireEvent.mouseDown(document.querySelector('.veu')!),
    ]) {
      await abrirEditor()
      fechar()
      await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
      expect(document.activeElement).toBe(await chip('Editar marcadores'))
    }
  })

  it('clicar dentro da janela não a fecha', async () => {
    await abrirNotas()
    await abrirEditor()
    fireEvent.mouseDown(janela())
    expect(screen.queryByRole('dialog', { name: 'Editar marcadores' })).toBeTruthy()
  })

  it('o Tab não sai da janela: do último campo volta ao primeiro, e o Shift+Tab faz o caminho inverso', async () => {
    await abrirNotas()
    await abrirEditor()
    const fechar = within(janela()).getByRole('button', { name: 'Fechar' })
    fechar.focus()
    fireEvent.keyDown(fechar, { key: 'Tab' })
    expect(document.activeElement).toBe(campoCriar())
    fireEvent.keyDown(campoCriar(), { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(fechar)
  })

  it('sem nenhum marcador, a janela diz como criar o primeiro', async () => {
    const srv = servidorFalso([], { notas: [sabado()] })
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
    await abrirEditor()
    expect(within(janela()).getByText(/Nenhum marcador ainda/)).toBeTruthy()
  })
})

describe('Editar marcadores: criar', () => {
  it('Enter cria (POST), limpa o campo e o chip aparece na fileira, na ordem do nome', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    digitar(campoCriar(), '  Casa   nova ')
    fireEvent.keyDown(campoCriar(), { key: 'Enter' })
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores', 'POST')).toHaveLength(1))
    // O nome vai limpo: espaços das pontas fora, sequências reduzidas a um.
    expect(pedidosA(srv, '/api/marcadores', 'POST')[0]!.corpo).toEqual({ nome: 'Casa nova' })
    await waitFor(() => expect(campoCriar().value).toBe(''))
    expect(within(linhaDo('Casa nova')).getByText('nenhuma nota')).toBeTruthy()
    expect(srv.marcadores.map((m) => m.nome)).toContain('Casa nova')
    await waitFor(async () => expect(await chips()).toEqual(['Notas 4', 'Casa nova 0', 'Compras 2', 'Escritório 1', 'Viagem 0', 'Arquivo', 'Lixeira', 'Editar marcadores']))
  })

  it('o botão Criar também cria', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    digitar(campoCriar(), 'Saúde')
    fireEvent.click(botaoCriar())
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores', 'POST')).toHaveLength(1))
    await waitFor(() => expect(linhaDo('Saúde')).toBeTruthy())
  })

  it('campo vazio ou só espaços: o Criar fica desligado e o Enter não manda nada', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    expect(botaoCriar().disabled).toBe(true)
    digitar(campoCriar(), '   ')
    expect(botaoCriar().disabled).toBe(true)
    fireEvent.keyDown(campoCriar(), { key: 'Enter' })
    await act(async () => {})
    expect(pedidosA(srv, '/api/marcadores', 'POST')).toHaveLength(0)
  })

  it('o campo respeita o tamanho máximo do Agent (50 caracteres)', async () => {
    await abrirNotas()
    await abrirEditor()
    expect(campoCriar().maxLength).toBe(50)
  })

  it.each([
    ['compras', 'Compras'],
    ['COMPRAS', 'Compras'],
    ['  compras  ', 'Compras'],
    ['escritorio', 'Escritório'],
    ['ESCRITÓRIO', 'Escritório'],
  ])('nome repetido ("%s"), sem diferenciar maiúscula nem acento: avisa e desliga o Criar', async (digitado, existente) => {
    const srv = await abrirNotas()
    await abrirEditor()
    digitar(campoCriar(), digitado)
    expect(within(janela()).getByRole('alert').textContent).toBe(`Já existe o marcador "${existente}".`)
    expect(botaoCriar().disabled).toBe(true)
    fireEvent.keyDown(campoCriar(), { key: 'Enter' })
    await act(async () => {})
    expect(pedidosA(srv, '/api/marcadores', 'POST')).toHaveLength(0)
    // Mudar o texto tira o aviso.
    digitar(campoCriar(), 'Casa')
    expect(within(janela()).queryByRole('alert')).toBeNull()
    expect(botaoCriar().disabled).toBe(false)
  })

  it('o nome que outra aba acabou de criar volta do Agent como NAME_IN_USE e vira o mesmo aviso', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    srv.marcadores.push(marcador({ id: 'mx', nome: 'Casa' }))
    digitar(campoCriar(), 'casa')
    fireEvent.keyDown(campoCriar(), { key: 'Enter' })
    await waitFor(() => expect(within(janela()).getByRole('alert').textContent).toBe('Já existe o marcador "casa".'))
    expect(srv.marcadores.filter((m) => m.nome.toLowerCase() === 'casa')).toHaveLength(1)
    expect(botaoCriar().disabled).toBe(false)
  })
})

describe('Editar marcadores: renomear', () => {
  const lapis = (nome: string) => within(janela()).getByRole('button', { name: `Renomear ${nome}` })
  const campoDe = (nome: string) => within(janela()).getByRole<HTMLInputElement>('textbox', { name: `Novo nome de ${nome}` })

  it('o lápis abre o campo na própria linha, com o foco e o nome inteiro selecionado', async () => {
    await abrirNotas()
    await abrirEditor()
    fireEvent.click(lapis('Escritório'))
    const campo = campoDe('Escritório')
    expect(campo.value).toBe('Escritório')
    expect(document.activeElement).toBe(campo)
    expect(within(janela()).getByRole('button', { name: 'Salvar' })).toBeTruthy()
  })

  it('Enter salva (PATCH com a versão do marcador), a fileira e os cartões mostram o nome novo', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lapis('Escritório'))
    digitar(campoDe('Escritório'), 'Escritório F5')
    fireEvent.keyDown(campoDe('Escritório'), { key: 'Enter' })
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores/m2', 'PATCH')).toHaveLength(1))
    expect(pedidosA(srv, '/api/marcadores/m2', 'PATCH')[0]!.corpo).toEqual({ nome: 'Escritório F5', base_version: 1 })
    expect(await within(janela()).findByText('Escritório F5', { selector: '.nome-m' })).toBeTruthy()
    // O foco volta ao lápis da linha.
    expect(document.activeElement).toBe(lapis('Escritório F5'))
    fireEvent.click(within(janela()).getByRole('button', { name: 'Fechar' }))
    await waitFor(async () => expect(await chips()).toContain('Escritório F5 1'))
    expect(within(cartao('n3')).getByText('Escritório F5')).toBeTruthy()
  })

  it('o botão Salvar também salva; o segundo renomear usa a versão nova', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lapis('Viagem'))
    digitar(campoDe('Viagem'), 'Viagens')
    fireEvent.click(within(janela()).getByRole('button', { name: 'Salvar' }))
    await within(janela()).findByText('Viagens', { selector: '.nome-m' })
    fireEvent.click(lapis('Viagens'))
    digitar(campoDe('Viagens'), 'Viagens 2027')
    fireEvent.keyDown(campoDe('Viagens'), { key: 'Enter' })
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores/m3', 'PATCH')).toHaveLength(2))
    expect(pedidosA(srv, '/api/marcadores/m3', 'PATCH')[1]!.corpo).toEqual({ nome: 'Viagens 2027', base_version: 2 })
  })

  it('Esc cancela só o renomear: nada é enviado, a janela fica e o foco volta ao lápis', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lapis('Compras'))
    digitar(campoDe('Compras'), 'Outro')
    fireEvent.keyDown(campoDe('Compras'), { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'Editar marcadores' })).toBeTruthy()
    expect(screen.queryByRole('textbox', { name: /Novo nome/ })).toBeNull()
    expect(within(janela()).getByText('Compras', { selector: '.nome-m' })).toBeTruthy()
    expect(document.activeElement).toBe(lapis('Compras'))
    expect(pedidosA(srv, '/api/marcadores/m1', 'PATCH')).toHaveLength(0)
  })

  it('o nome de outro marcador (sem caixa nem acento) é recusado na linha, e o Salvar desliga', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lapis('Viagem'))
    digitar(campoDe('Viagem'), 'escritorio')
    expect(within(janela()).getByRole('alert').textContent).toBe('Já existe o marcador "Escritório".')
    expect(within(janela()).getByRole<HTMLButtonElement>('button', { name: 'Salvar' }).disabled).toBe(true)
    fireEvent.keyDown(campoDe('Viagem'), { key: 'Enter' })
    await act(async () => {})
    expect(pedidosA(srv, '/api/marcadores/m3', 'PATCH')).toHaveLength(0)
  })

  it('mudar só a caixa do próprio nome vale (não é repetido)', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lapis('Compras'))
    digitar(campoDe('Compras'), 'compras')
    expect(within(janela()).queryByRole('alert')).toBeNull()
    fireEvent.keyDown(campoDe('Compras'), { key: 'Enter' })
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores/m1', 'PATCH')).toHaveLength(1))
    expect(await within(janela()).findByText('compras', { selector: '.nome-m' })).toBeTruthy()
  })

  it('salvar sem mudar o nome só fecha o campo', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lapis('Compras'))
    fireEvent.keyDown(campoDe('Compras'), { key: 'Enter' })
    await waitFor(() => expect(screen.queryByRole('textbox', { name: /Novo nome/ })).toBeNull())
    expect(pedidosA(srv, '/api/marcadores/m1', 'PATCH')).toHaveLength(0)
  })

  it('o marcador mudou em outra aba (versão velha): avisa, lê a lista de novo e fecha o campo', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    const m = srv.marcadores.find((x) => x.id === 'm3')!
    m.version = 5
    m.nome = 'Viagens'
    fireEvent.click(lapis('Viagem'))
    digitar(campoDe('Viagem'), 'Passeios')
    fireEvent.keyDown(campoDe('Viagem'), { key: 'Enter' })
    await waitFor(() => expect(aviso()).toContain('Este marcador mudou em outra aba'))
    expect(await within(janela()).findByText('Viagens', { selector: '.nome-m' })).toBeTruthy()
    expect(screen.queryByRole('textbox', { name: /Novo nome/ })).toBeNull()
  })
})

describe('Editar marcadores: apagar', () => {
  const lixeira = (nome: string) => within(janela()).getByRole('button', { name: `Apagar ${nome}` })

  it('a lixeirinha pergunta na própria linha, com a conta de notas e o foco no Cancelar', async () => {
    await abrirNotas()
    await abrirEditor()
    fireEvent.click(lixeira('Compras'))
    const pergunta = within(janela()).getByRole('group', { name: 'Apagar o marcador Compras' })
    expect(pergunta.textContent).toContain('Apagar "Compras"? Ele sai das 2 notas; as notas ficam.')
    expect(document.activeElement).toBe(within(pergunta).getByRole('button', { name: 'Cancelar' }))
    // É a mesma janela: sem outra por cima.
    expect(screen.getAllByRole('dialog')).toHaveLength(1)
  })

  it.each([
    ['Escritório', 'Apagar "Escritório"? Ele sai da nota; a nota fica.'],
    ['Viagem', 'Apagar "Viagem"? Nenhuma nota usa este marcador.'],
  ])('a frase de %s no singular e sem nota', async (nome, frase) => {
    await abrirNotas()
    await abrirEditor()
    fireEvent.click(lixeira(nome))
    expect(within(janela()).getByRole('group', { name: `Apagar o marcador ${nome}` }).textContent).toContain(frase)
  })

  it('Cancelar não apaga nada e devolve o foco à lixeirinha; Esc faz o mesmo sem fechar a janela', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lixeira('Compras'))
    fireEvent.click(within(janela()).getByRole('button', { name: 'Cancelar' }))
    expect(document.activeElement).toBe(lixeira('Compras'))
    fireEvent.click(lixeira('Compras'))
    fireEvent.keyDown(within(janela()).getByRole('button', { name: 'Cancelar' }), { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'Editar marcadores' })).toBeTruthy()
    expect(document.activeElement).toBe(lixeira('Compras'))
    expect(pedidosA(srv, '/api/marcadores/m1', 'DELETE')).toHaveLength(0)
    expect(srv.marcadores).toHaveLength(3)
  })

  it('Apagar (DELETE) tira o marcador das notas e a fileira; as notas ficam', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    fireEvent.click(lixeira('Compras'))
    fireEvent.click(within(janela()).getByRole('button', { name: 'Apagar' }))
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores/m1', 'DELETE')).toHaveLength(1))
    await waitFor(() => expect(within(janela()).queryByText('Compras', { selector: '.nome-m' })).toBeNull())
    expect(srv.notas.find((n) => n.id === 'n1')!.marcadores).toEqual([])
    expect(srv.notas.find((n) => n.id === 'n2')!.marcadores).toEqual([])
    expect(srv.notas).toHaveLength(4)
    expect(aviso()).toBe('Marcador "Compras" apagado.')
    // O foco vai ao campo de criar, e os cartões perdem a etiqueta depois da recarga.
    expect(document.activeElement).toBe(campoCriar())
    fireEvent.click(within(janela()).getByRole('button', { name: 'Fechar' }))
    await waitFor(async () => expect(await chips()).toEqual(['Notas 4', 'Escritório 1', 'Viagem 0', 'Arquivo', 'Lixeira', 'Editar marcadores']))
    await waitFor(() => expect(within(cartao('n1')).queryByText('Compras', { selector: '.etiqueta' })).toBeNull())
    expect(cartao('n1')).toBeTruthy()
  })

  it('apagar o marcador do filtro volta a mostrar todas as notas', async () => {
    const srv = await abrirNotas()
    fireEvent.click(await chip('Escritório 1'))
    await waitFor(() => expect(document.querySelectorAll('.grade [data-nota]')).toHaveLength(1))
    await abrirEditor()
    fireEvent.click(lixeira('Escritório'))
    fireEvent.click(within(janela()).getByRole('button', { name: 'Apagar' }))
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores/m2', 'DELETE')).toHaveLength(1))
    await waitFor(() => expect(document.querySelectorAll('.grade [data-nota]')).toHaveLength(4))
  })

  it('um marcador que já tinha sido apagado em outra aba (404) também sai da lista', async () => {
    const srv = await abrirNotas()
    await abrirEditor()
    srv.marcadores.splice(srv.marcadores.findIndex((m) => m.id === 'm3'), 1)
    fireEvent.click(lixeira('Viagem'))
    fireEvent.click(within(janela()).getByRole('button', { name: 'Apagar' }))
    await waitFor(() => expect(within(janela()).queryByText('Viagem', { selector: '.nome-m' })).toBeNull())
  })
})

// --- O botão de marcador ---

const botaoMarcadoresDaNota = () => within(dialogo()).getByRole('button', { name: 'Marcadores' })
const opcao = (nome: string | RegExp) => screen.getByRole('option', { name: nome })
const campoDoMenu = () => screen.getByRole<HTMLInputElement>('combobox', { name: 'Buscar ou criar marcador' })

describe('o botão de marcador na nota aberta', () => {
  it('abre o menu com o foco no campo e uma caixinha por marcador (as da nota marcadas)', async () => {
    await abrirNotas()
    abrir('n1')
    fireEvent.click(botaoMarcadoresDaNota())
    expect(document.activeElement).toBe(campoDoMenu())
    expect(botaoMarcadoresDaNota().getAttribute('aria-expanded')).toBe('true')
    expect(screen.getAllByRole('option').map((o) => [o.textContent, o.getAttribute('aria-selected')])).toEqual([
      ['Compras', 'true'],
      ['Escritório', 'false'],
      ['Viagem', 'false'],
    ])
  })

  it('marcar e desmarcar valem na hora (PUT e DELETE), e a etiqueta aparece e some na nota', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n4/marcadores/m3', 'PUT')).toHaveLength(1))
    await waitFor(() => expect(opcao('Viagem').getAttribute('aria-selected')).toBe('true'))
    expect(within(dialogo()).getByText('Viagem', { selector: '.etiqueta' })).toBeTruthy()
    expect(srv.notas.find((n) => n.id === 'n4')!.marcadores).toEqual(['m3'])
    // O menu fica aberto, e o foco no campo.
    expect(screen.queryByRole('listbox')).toBeTruthy()
    expect(document.activeElement).toBe(campoDoMenu())

    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n4/marcadores/m3', 'DELETE')).toHaveLength(1))
    await waitFor(() => expect(within(dialogo()).queryByText('Viagem', { selector: '.etiqueta' })).toBeNull())
    expect(opcao('Viagem').getAttribute('aria-selected')).toBe('false')
  })

  it('uma nota pode ter vários marcadores', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(opcao('Viagem').getAttribute('aria-selected')).toBe('true'))
    fireEvent.click(opcao('Compras'))
    await waitFor(() => expect(opcao('Compras').getAttribute('aria-selected')).toBe('true'))
    expect(srv.notas.find((n) => n.id === 'n4')!.marcadores).toEqual(['m1', 'm3'])
    expect(Array.from(dialogo().querySelectorAll('.pe .etiqueta')).map((e) => e.textContent?.replace('×', ''))).toEqual(['Compras', 'Viagem'])
  })

  it('o texto digitado filtra as caixinhas, sem caixa nem acento', async () => {
    await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    digitar(campoDoMenu(), 'ESCRITORIO')
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Escritório'])
    // O nome já existe: não oferece criar.
    expect(screen.queryByRole('option', { name: /Criar marcador/ })).toBeNull()
    digitar(campoDoMenu(), 'zzz')
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['+Criar marcador “zzz”'])
    digitar(campoDoMenu(), '')
    expect(screen.getAllByRole('option')).toHaveLength(3)
  })

  it('"Criar marcador" cria e já põe na nota (POST e PUT); a lista da fileira aprende o nome', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    digitar(campoDoMenu(), 'Feira')
    fireEvent.click(opcao('Criar marcador “Feira”'))
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores', 'POST')).toHaveLength(1))
    expect(pedidosA(srv, '/api/marcadores', 'POST')[0]!.corpo).toEqual({ nome: 'Feira' })
    const novo = srv.marcadores.find((m) => m.nome === 'Feira')!
    await waitFor(() => expect(pedidosA(srv, `/api/notas/n4/marcadores/${novo.id}`, 'PUT')).toHaveLength(1))
    await waitFor(() => expect(within(dialogo()).getByText('Feira', { selector: '.etiqueta' })).toBeTruthy())
    // O campo limpa e o foco fica nele; a caixinha nova está marcada.
    expect(campoDoMenu().value).toBe('')
    expect(document.activeElement).toBe(campoDoMenu())
    expect(opcao('Feira').getAttribute('aria-selected')).toBe('true')
  })

  it('teclado: setas movem a linha ativa, Enter alterna, Esc fecha o menu e não a janela', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    const campo = campoDoMenu()
    // A primeira linha começa ativa.
    expect(campo.getAttribute('aria-activedescendant')).toBe(opcao('Compras').id)
    fireEvent.keyDown(campo, { key: 'ArrowDown' })
    fireEvent.keyDown(campo, { key: 'ArrowDown' })
    expect(campo.getAttribute('aria-activedescendant')).toBe(opcao('Viagem').id)
    // Dá a volta nas duas pontas.
    fireEvent.keyDown(campo, { key: 'ArrowDown' })
    expect(campo.getAttribute('aria-activedescendant')).toBe(opcao('Compras').id)
    fireEvent.keyDown(campo, { key: 'ArrowUp' })
    expect(campo.getAttribute('aria-activedescendant')).toBe(opcao('Viagem').id)
    fireEvent.keyDown(campo, { key: 'Enter' })
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n4/marcadores/m3', 'PUT')).toHaveLength(1))
    await waitFor(() => expect(opcao('Viagem').getAttribute('aria-selected')).toBe('true'))

    fireEvent.keyDown(campo, { key: 'Escape' })
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(screen.queryByRole('dialog')).toBeTruthy()
    expect(document.activeElement).toBe(botaoMarcadoresDaNota())
  })

  it('teclado: com um nome novo, Enter cria; com a busca vazia, o texto que casa vira a primeira linha', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    digitar(campoDoMenu(), 'Feira')
    expect(campoDoMenu().getAttribute('aria-activedescendant')).toBe(opcao(/Criar marcador/).id)
    fireEvent.keyDown(campoDoMenu(), { key: 'Enter' })
    await waitFor(() => expect(pedidosA(srv, '/api/marcadores', 'POST')).toHaveLength(1))
    await waitFor(() => expect(within(dialogo()).getByText('Feira', { selector: '.etiqueta' })).toBeTruthy())
  })

  it('sem nenhuma caixinha que case, o menu diz que não há; sem marcadores, diz como criar', async () => {
    const srv = servidorFalso([], { notas: [sabado()] })
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Notas' }))
    await waitFor(() => expect(cartao('n4')).toBeTruthy())
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    expect(screen.getByText(/Nenhum marcador ainda/)).toBeTruthy()
  })

  it('a etiqueta tem um × com nome ("Tirar o marcador X") que tira o marcador (DELETE) e leva o foco ao vizinho', async () => {
    const srv = await abrirNotas([nota({ id: 'n9', titulo: 'Duas', corpo: 'x', marcadores: ['m1', 'm3'], updated_at: hora(9) }), compras(), mercado()])
    abrir('n9')
    const tirar = within(dialogo()).getByRole('button', { name: 'Tirar o marcador Compras' })
    fireEvent.click(tirar)
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n9/marcadores/m1', 'DELETE')).toHaveLength(1))
    await waitFor(() => expect(within(dialogo()).queryByRole('button', { name: 'Tirar o marcador Compras' })).toBeNull())
    // Sobrou o Viagem, que ganha o foco.
    expect(document.activeElement).toBe(within(dialogo()).getByRole('button', { name: 'Tirar o marcador Viagem' }))
    fireEvent.click(document.activeElement!)
    await waitFor(() => expect(within(dialogo()).queryByRole('button', { name: /Tirar o marcador/ })).toBeNull())
    // Sem etiqueta, o foco vai ao botão de marcador.
    expect(document.activeElement).toBe(botaoMarcadoresDaNota())
  })

  it('a nota guarda a versão nova: o texto salvo depois do marcador leva base_version certa (sem 409)', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(opcao('Viagem').getAttribute('aria-selected')).toBe('true'))
    expect(srv.notas.find((n) => n.id === 'n4')!.version).toBe(2)
    fireEvent.keyDown(campoDoMenu(), { key: 'Escape' })
    fireEvent.change(campoTitulo(), { target: { value: 'Sábado' } })
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    const patch = pedidosA(srv, '/api/notas/n4', 'PATCH')
    expect(patch).toHaveLength(1)
    expect(patch[0]!.corpo).toEqual({ titulo: 'Sábado', base_version: 2 })
    expect(screen.queryByRole('alert')).toBeNull()
    expect(srv.notas.find((n) => n.id === 'n4')!.marcadores).toEqual(['m3'])
  })

  it('marcar com um texto ainda por gravar: o marcador vai primeiro e a gravação segue na fila, sem conflito', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.change(campoTitulo(), { target: { value: 'Sábado' } })
    fireEvent.click(botaoMarcadoresDaNota())
    fireEvent.click(opcao('Viagem'))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    const ordem = srv.pedidos.filter((p) => p.caminho.startsWith('/api/notas/n4') && p.metodo !== 'GET').map((p) => `${p.metodo} ${p.caminho}`)
    expect(ordem).toEqual(['PUT /api/notas/n4/marcadores/m3', 'PATCH /api/notas/n4'])
    expect(pedidosA(srv, '/api/notas/n4', 'PATCH')[0]!.corpo).toEqual({ titulo: 'Sábado', base_version: 2 })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('a nota que foi para a lixeira em outra aba: o marcador não vai, e a janela diz', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    srv.notaParaALixeiraPorFora('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    fireEvent.click(opcao('Viagem'))
    expect((await screen.findByRole('alert')).textContent).toContain('movida para a lixeira')
    expect(srv.notas.find((n) => n.id === 'n4')!.marcadores).toEqual([])
  })

  it('o marcador apagado em outra aba (404): avisa, sem derrubar a nota', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    srv.marcadores.splice(srv.marcadores.findIndex((m) => m.id === 'm3'), 1)
    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(aviso()).toContain('Esse marcador não existe mais'))
    expect(screen.queryByRole('dialog')).toBeTruthy()
  })

  it('fechar a nota leva a nota com os marcadores novos ao cartão e a conta à fileira', async () => {
    await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    fireEvent.click(opcao('Compras'))
    await waitFor(() => expect(opcao('Compras').getAttribute('aria-selected')).toBe('true'))
    fireEvent.keyDown(campoDoMenu(), { key: 'Escape' })
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(within(cartao('n4')).getByText('Compras', { selector: '.etiqueta' })).toBeTruthy()
    await waitFor(async () => expect(await chips()).toContain('Compras 3'))
  })
})

describe('o botão de marcador no cartão', () => {
  const botaoDoCartao = (id: string) => within(cartao(id)).getByRole('button', { name: /^Marcadores da nota/ })

  it('existe em cada cartão e abre o mesmo menu', async () => {
    await abrirNotas()
    for (const id of ['n1', 'n2', 'n3', 'n4']) expect(botaoDoCartao(id)).toBeTruthy()
    expect(botaoDoCartao('n4').getAttribute('aria-label')).toBe('Marcadores da nota “Lista do sábado”')
    fireEvent.click(botaoDoCartao('n1'))
    expect(document.activeElement).toBe(campoDoMenu())
    expect(screen.getAllByRole('option').map((o) => [o.textContent, o.getAttribute('aria-selected')])).toEqual([
      ['Compras', 'true'],
      ['Escritório', 'false'],
      ['Viagem', 'false'],
    ])
  })

  it('marcar e desmarcar no cartão (PUT/DELETE) mudam as etiquetas e a conta da fileira na hora', async () => {
    const srv = await abrirNotas()
    fireEvent.click(botaoDoCartao('n4'))
    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n4/marcadores/m3', 'PUT')).toHaveLength(1))
    await waitFor(() => expect(within(cartao('n4')).getByText('Viagem', { selector: '.etiqueta' })).toBeTruthy())
    await waitFor(async () => expect(await chips()).toContain('Viagem 1'))
    expect(opcao('Viagem').getAttribute('aria-selected')).toBe('true')

    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n4/marcadores/m3', 'DELETE')).toHaveLength(1))
    await waitFor(() => expect(within(cartao('n4')).queryByText('Viagem', { selector: '.etiqueta' })).toBeNull())
    await waitFor(async () => expect(await chips()).toContain('Viagem 0'))
  })

  it('"Criar marcador" no cartão cria e põe na nota; o Esc fecha o menu e devolve o foco ao botão', async () => {
    const srv = await abrirNotas()
    fireEvent.click(botaoDoCartao('n4'))
    digitar(campoDoMenu(), 'Feira')
    fireEvent.click(opcao(/Criar marcador/))
    await waitFor(() => expect(within(cartao('n4')).getByText('Feira', { selector: '.etiqueta' })).toBeTruthy())
    expect(srv.marcadores.map((m) => m.nome)).toContain('Feira')
    await waitFor(async () => expect(await chips()).toContain('Feira 1'))
    fireEvent.keyDown(campoDoMenu(), { key: 'Escape' })
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(document.activeElement).toBe(botaoDoCartao('n4'))
  })

  it('dois cliques seguidos em marcadores diferentes ficam os dois (um pedido de cada vez)', async () => {
    const srv = await abrirNotas()
    fireEvent.click(botaoDoCartao('n4'))
    fireEvent.click(opcao('Viagem'))
    fireEvent.click(opcao('Escritório'))
    await waitFor(() => expect(srv.notas.find((n) => n.id === 'n4')!.marcadores).toEqual(['m2', 'm3']))
    await waitFor(() => expect(within(cartao('n4')).getAllByText(/Escritório|Viagem/, { selector: '.etiqueta' })).toHaveLength(2))
  })

  it('a nota que foi para a lixeira em outra aba: avisa e lê a lista de novo', async () => {
    const srv = await abrirNotas()
    fireEvent.click(botaoDoCartao('n4'))
    srv.notaParaALixeiraPorFora('n4')
    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(aviso()).toContain('Esta nota mudou em outra aba'))
    await waitFor(() => expect(document.querySelector('[data-nota="n4"]')).toBeNull())
  })

  it('na lixeira das notas, o cartão não tem o botão', async () => {
    const lixo = nota({ id: 'n8', titulo: 'No lixo', corpo: 'x', deleted_at: hora(6), updated_at: hora(6) })
    await abrirNotas([compras(), lixo])
    fireEvent.click(await chip('Lixeira'))
    await screen.findByText('No lixo')
    expect(screen.queryByRole('button', { name: /^Marcadores da nota/ })).toBeNull()
  })
})

// --- A sugestão ---

const faixa = () => screen.queryByRole('group', { name: 'Sugestão de marcador' })

describe('a sugestão de marcador', () => {
  it('na nota sem marcador e parecida com as de um marcador: "Marcar como Compras?", sem marcar sozinha', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    const f = faixa()!
    expect(f).toBeTruthy()
    expect(f.textContent).toContain('Marcar como Compras?')
    expect(within(f).getByRole('button', { name: 'Marcar' })).toBeTruthy()
    expect(within(f).getByRole('button', { name: 'Agora não' })).toBeTruthy()
    // Nunca marca por conta própria.
    await act(async () => {})
    expect(srv.pedidos.filter((p) => p.metodo === 'PUT' && p.caminho.includes('/marcadores/'))).toHaveLength(0)
  })

  it('Marcar põe o marcador (PUT), a faixa some, a etiqueta aparece e o foco vai ao botão de marcador', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(within(faixa()!).getByRole('button', { name: 'Marcar' }))
    await waitFor(() => expect(pedidosA(srv, '/api/notas/n4/marcadores/m1', 'PUT')).toHaveLength(1))
    await waitFor(() => expect(faixa()).toBeNull())
    expect(within(dialogo()).getByText('Compras', { selector: '.etiqueta' })).toBeTruthy()
    expect(document.activeElement).toBe(botaoMarcadoresDaNota())
  })

  it('Agora não esconde a faixa nessa nota, lembra no localStorage e não volta ao reabrir', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(within(faixa()!).getByRole('button', { name: 'Agora não' }))
    expect(faixa()).toBeNull()
    expect(document.activeElement).toBe(botaoMarcadoresDaNota())
    expect(JSON.parse(window.localStorage.getItem('aurora.notas.sugestao.dispensadas')!)).toEqual(['n4'])
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    abrir('n4')
    expect(faixa()).toBeNull()
    expect(srv.pedidos.filter((p) => p.metodo === 'PUT' && p.caminho.includes('/marcadores/'))).toHaveLength(0)
  })

  it('Agora não vale para aquela nota: outra nota parecida ainda recebe a sugestão', async () => {
    const outra = nota({ id: 'n6', titulo: 'Outra', corpo: '- leite\n- ovos\n- café', updated_at: hora(12) })
    await abrirNotas([compras(), mercado(), sabado(), outra])
    abrir('n4')
    fireEvent.click(within(faixa()!).getByRole('button', { name: 'Agora não' }))
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    abrir('n6')
    expect(faixa()).toBeTruthy()
  })

  it('o armazenamento quebrado: a faixa aparece, o Agora não a esconde sem erro', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('bloqueado', 'SecurityError')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('cheio', 'QuotaExceededError')
    })
    await abrirNotas()
    abrir('n4')
    expect(faixa()).toBeTruthy()
    fireEvent.click(within(faixa()!).getByRole('button', { name: 'Agora não' }))
    expect(faixa()).toBeNull()
    expect(screen.queryByRole('dialog')).toBeTruthy()
  })

  it('nota que já tem marcador não recebe a faixa', async () => {
    await abrirNotas()
    abrir('n1')
    expect(faixa()).toBeNull()
  })

  it('marcador com uma nota só não é sugerido', async () => {
    // Só o Compras tem 2 notas; sem a Mercado, ele tem 1 e a lista solta fica sem sugestão.
    await abrirNotas([compras(), reuniao(), sabado()])
    abrir('n4')
    expect(faixa()).toBeNull()
  })

  it('nota sem nada em comum com os marcadores não recebe a faixa', async () => {
    const ideia = nota({ id: 'n7', titulo: 'Ideia', corpo: 'um vídeo sobre a reforma', updated_at: hora(12) })
    await abrirNotas([compras(), mercado(), ideia])
    abrir('n7')
    expect(faixa()).toBeNull()
  })

  it('tirar o último marcador da nota faz a sugestão voltar; pôr outro, sumir', async () => {
    const srv = await abrirNotas()
    abrir('n4')
    fireEvent.click(botaoMarcadoresDaNota())
    fireEvent.click(opcao('Viagem'))
    await waitFor(() => expect(faixa()).toBeNull())
    fireEvent.keyDown(campoDoMenu(), { key: 'Escape' })
    fireEvent.click(within(dialogo()).getByRole('button', { name: 'Tirar o marcador Viagem' }))
    await waitFor(() => expect(faixa()).toBeTruthy())
    expect(srv.notas.find((n) => n.id === 'n4')!.marcadores).toEqual([])
  })

  it('aparece enquanto se edita: uma nota nova e vazia ganha a faixa um instante depois da lista ser escrita', async () => {
    await abrirNotas([compras(), mercado(), nota({ id: 'n5', titulo: '', corpo: 'oi', updated_at: hora(12) })])
    abrir('n5')
    expect(faixa()).toBeNull()
    const linha = linhasDoDialogo()[0]!
    fireEvent.change(linha, { target: { value: 'leite' } })
    // O texto ainda não assentou: nada de sugestão.
    expect(faixa()).toBeNull()
    fireEvent.change(linhasDoDialogo()[0]!, { target: { value: 'leite e café' } })
    expect(await screen.findByRole('group', { name: 'Sugestão de marcador' })).toBeTruthy()
    expect(faixa()!.textContent).toContain('Marcar como Compras?')
  })

  it('apagar o texto que a motivou tira a faixa', async () => {
    await abrirNotas()
    abrir('n4')
    expect(faixa()).toBeTruthy()
    fireEvent.change(campoTitulo(), { target: { value: 'Uma ideia' } })
    for (const l of linhasDoDialogo()) fireEvent.change(l, { target: { value: 'sobre reforma' } })
    await waitFor(() => expect(faixa()).toBeNull())
  })
})
