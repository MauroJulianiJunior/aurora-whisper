// A busca única do painel do PC (ADR-022, N6): o campo do topo procura nas notas
// e nos ditados, em dois grupos, nas abas Histórico e Notas. Contra o Agent de
// mentira (testes/servidorFalso.ts): o que o painel pede, o que a pessoa vê, e
// onde o foco fica.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { type OpcoesDoServidor, type Pedido, ditado, nota, servidorFalso } from './testes/servidorFalso'
import { renderizarSoHistorico } from './testes/soHistorico'
import type { Ditado } from './api'
import type { Nota } from './notas/tipos'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

// Hoje é 26 de outubro de 2026, 18h: "ontem" é dia 25.
beforeEach(() => {
  window.localStorage.clear()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 26, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function notas(): Nota[] {
  return [
    nota({ id: 'n1', titulo: 'Fechamento de outubro', corpo: '# Pauta\n- DCTFWeb\n- [ ] Folha e FGTS', cor: 'azul', updated_at: em(25, 9) }),
    nota({ id: 'n2', titulo: 'Reunião com o contador', corpo: 'conferir o extrato do Itaú', updated_at: em(25, 10) }),
    nota({ id: 'n3', corpo: 'ligar para a Fernanda antes das cinco', updated_at: em(25, 11) }),
    // Arquivada e na lixeira não entram na busca.
    nota({ id: 'n4', titulo: 'FGTS antigo', corpo: 'resolvido', arquivada: true, updated_at: em(20, 9) }),
    nota({ id: 'n5', titulo: 'FGTS apagado', corpo: 'x', deleted_at: em(21, 9), updated_at: em(21, 9) }),
  ]
}

function ditados(): Ditado[] {
  return [
    ditado({ id: 'd1', created_at: em(25, 10), final_text: 'Faltam três coisas para fechar o mês: a folha, o FGTS e a guia do INSS.' }),
    ditado({ id: 'd2', created_at: em(22, 15), final_text: 'O FGTS da padaria venceu dia 20, confere se pagaram.' }),
    ditado({ id: 'd3', created_at: em(18, 9), final_text: 'Mandar o relatório do FGTS digital para o Grupo Ramos.' }),
    ditado({ id: 'd4', created_at: em(17, 9), final_text: 'Marcar a REUNIÃO de quinta com a Fernanda.' }),
    ditado({ id: 'd5', created_at: em(16, 9), final_text: 'Nada a ver com a busca.' }),
  ]
}

async function montar(opcoes: OpcoesDoServidor & { ditados?: Ditado[] } = {}) {
  const { ditados: seus, ...doServidor } = opcoes
  const srv = servidorFalso(seus ?? ditados(), { notas: notas(), ...doServidor })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  await waitFor(() => expect(document.querySelector('[data-linha]')).toBeTruthy())
  return srv
}

// aba é um botão da barra de cima (o "Notas" e o "Lixeira" também são chips da aba Notas).
const aba = (nome: string) => within(screen.getByRole('group', { name: 'Mostrar' })).getByRole('button', { name: nome })

const nomeDoCampo = 'Buscar nas notas e nos ditados'
const campo = () => screen.getByRole<HTMLInputElement>('searchbox', { name: nomeDoCampo })
const resultados = () => Array.from(document.querySelectorAll<HTMLElement>('.busca-unica [data-resultado]'))
const grupo = (nome: string) => screen.getByRole('region', { name: nome })
const anuncio = () => document.querySelector<HTMLElement>('.busca-unica .anuncio')!.textContent
const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, q: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && p.busca.get('q') === q && p.busca.get('trash') !== 'true')
const marcados = (el: HTMLElement) => Array.from(el.querySelectorAll('mark')).map((m) => m.textContent)

// buscarPor digita no campo e espera os dois grupos chegarem.
async function buscarPor(texto: string) {
  fireEvent.change(campo(), { target: { value: texto } })
  await waitFor(() => expect(document.querySelector('.busca-unica .dia, .busca-unica .estado:not(.atrasado)')).toBeTruthy())
}

describe('os dois grupos', () => {
  it('Notas e Ditados, com a contagem, a bolinha da cor, o título · a linha que casa e o quando do ditado', async () => {
    const srv = await montar()
    await buscarPor('fgts')

    // Um pedido de cada, com o mesmo texto.
    expect(pedidosA(srv, '/api/notas', 'fgts')).toHaveLength(1)
    expect(pedidosA(srv, '/api/history', 'fgts')).toHaveLength(1)

    // A lista do histórico não é refeita com a busca: os resultados tomam o lugar dela.
    expect(document.querySelector('.item')).toBeNull()

    const nas = grupo('Notas · 1')
    const nota1 = within(nas).getByRole('button', { name: /Fechamento de outubro/ })
    expect(nota1.textContent).toBe('Fechamento de outubro · Folha e FGTS')
    expect(nota1.querySelector('strong')!.textContent).toBe('Fechamento de outubro')
    expect(nota1.querySelector('.bolinha')!.getAttribute('data-cor')).toBe('azul')
    expect(marcados(nota1)).toEqual(['FGTS'])

    const dit = grupo('Ditados · 3')
    const linhas = within(dit).getAllByRole('button')
    expect(linhas.map((l) => l.querySelector('time')!.textContent)).toEqual(['ontem', '22 out', '18 out'])
    expect(linhas[0]!.querySelector('.resultado-texto')!.textContent).toBe('Faltam três coisas para fechar o mês: a folha, o FGTS e a guia do INSS.')
    expect(linhas.map((l) => marcados(l))).toEqual([['FGTS'], ['FGTS'], ['FGTS']])
  })

  it('a nota arquivada e a da lixeira não entram; um grupo vazio não aparece', async () => {
    await montar()
    await buscarPor('fernanda')
    // Uma nota (a sem título, pela linha) e um ditado.
    expect(screen.getByRole('region', { name: 'Notas · 1' })).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Ditados · 1' })).toBeTruthy()
    fireEvent.change(campo(), { target: { value: 'padaria' } })
    await waitFor(() => expect(anuncio()).toBe('1 ditado para “padaria”.'))
    expect(screen.queryByRole('region', { name: /^Notas/ })).toBeNull()
    // "FGTS antigo" é arquivada e "FGTS apagado" está na lixeira: só a nota do fechamento casa.
    fireEvent.change(campo(), { target: { value: 'fgts' } })
    await waitFor(() => expect(anuncio()).toBe('1 nota e 3 ditados para “fgts”.'))
  })

  it('a nota sem título mostra só a linha que casa', async () => {
    await montar()
    await buscarPor('cinco')
    const linha = within(grupo('Notas · 1')).getByRole('button')
    expect(linha.textContent).toBe('ligar para a Fernanda antes das cinco')
    expect(linha.querySelector('strong')).toBeNull()
    expect(marcados(linha)).toEqual(['cinco'])
  })

  it('o termo só no título: a linha é o começo do corpo', async () => {
    await montar()
    await buscarPor('fechamento')
    const linha = within(grupo('Notas · 1')).getByRole('button')
    expect(linha.textContent).toBe('Fechamento de outubro · Pauta · DCTFWeb · Folha e FGTS')
    expect(marcados(linha)).toEqual(['Fechamento'])
  })

  it('casa sem diferenciar maiúscula nem acento, nos dois grupos, e marca a palavra como está escrita', async () => {
    await montar()
    for (const digitado of ['reuniao', 'REUNIÃO', 'Reunião']) {
      fireEvent.change(campo(), { target: { value: digitado } })
      await waitFor(() => expect(marcados(grupo('Notas · 1').parentElement!)).toEqual(['Reunião', 'REUNIÃO']))
      expect(within(grupo('Ditados · 1')).getByRole('button').querySelector('.resultado-texto')!.textContent).toBe('Marcar a REUNIÃO de quinta com a Fernanda.')
    }
    // E ao contrário: sem acento no texto, com acento na busca.
    fireEvent.change(campo(), { target: { value: 'três' } })
    await screen.findByRole('region', { name: 'Ditados · 1' })
    fireEvent.change(campo(), { target: { value: 'tres' } })
    await waitFor(() => expect(marcados(grupo('Ditados · 1'))).toEqual(['três']))
  })

  it('cada grupo mostra 5 e "Ver todas" / "Ver todos" diz quantas são', async () => {
    const muitas = Array.from({ length: 7 }, (_, i) => nota({ id: `m${i}`, titulo: `Imposto ${i + 1}`, updated_at: em(24, 8, i) }))
    const muitos = Array.from({ length: 6 }, (_, i) => ditado({ id: `x${i}`, created_at: em(23, 8, i), final_text: `Pagar o imposto número ${i + 1}.` }))
    await montar({ notas: muitas, ditados: [...muitos, ...ditados()] })
    await buscarPor('imposto')

    const nas = grupo('Notas · 7')
    expect(within(nas).getAllByRole('button')).toHaveLength(6) // 5 notas e o "Ver todas"
    expect(within(nas).getByRole('button', { name: 'Ver todas as 7 notas' })).toBeTruthy()
    const dit = grupo('Ditados · 6')
    expect(within(dit).getAllByRole('button')).toHaveLength(6)
    expect(within(dit).getByRole('button', { name: 'Ver todos os 6 ditados' })).toBeTruthy()
  })

  it('"Ver todas" leva à aba Notas com a mesma busca, e a linha de baixo traz os dois grupos de volta', async () => {
    const muitas = Array.from({ length: 7 }, (_, i) => nota({ id: `m${i}`, titulo: `Imposto ${i + 1}`, updated_at: em(24, 8, i) }))
    const srv = await montar({ notas: muitas })
    await buscarPor('imposto')
    fireEvent.click(within(grupo('Notas · 7')).getByRole('button', { name: 'Ver todas as 7 notas' }))

    await waitFor(() => expect(document.querySelectorAll('.grade [data-nota]')).toHaveLength(7))
    expect(document.querySelector('.busca-unica')).toBeNull()
    expect(aba('Notas').getAttribute('aria-pressed')).toBe('true')
    expect(campo().value).toBe('imposto')
    expect(document.querySelector('.escopo-busca')!.textContent).toContain('Só nas notas para “imposto”')
    // O histórico não é refeito com a busca: ela é das notas.
    expect(srv.pedidos.filter((p) => p.caminho === '/api/history' && p.busca.get('q') === 'imposto')).toHaveLength(1) // só o da busca única

    fireEvent.click(screen.getByRole('button', { name: 'Ver notas e ditados' }))
    await screen.findByRole('region', { name: 'Notas · 7' })
    expect(document.querySelector('.escopo-busca')).toBeNull()
    expect(document.activeElement).toBe(campo())
  })

  it('"Ver todos" leva ao Histórico filtrado pela mesma busca', async () => {
    const muitos = Array.from({ length: 6 }, (_, i) => ditado({ id: `x${i}`, created_at: em(23, 8, i), final_text: `Pagar o imposto número ${i + 1}.` }))
    const srv = await montar({ ditados: [...muitos, ...ditados()] })
    await buscarPor('imposto')
    fireEvent.click(within(grupo('Ditados · 6')).getByRole('button', { name: 'Ver todos os 6 ditados' }))

    await waitFor(() => expect(document.querySelectorAll('.item')).toHaveLength(6))
    expect(document.querySelector('.busca-unica')).toBeNull()
    expect(aba('Histórico').getAttribute('aria-pressed')).toBe('true')
    expect(document.querySelector('.escopo-busca')!.textContent).toContain('Só nos ditados para “imposto”')
    // Agora a lista do histórico é que lê a busca (uma vez além da busca única).
    expect(srv.pedidos.filter((p) => p.caminho === '/api/history' && p.busca.get('q') === 'imposto').length).toBeGreaterThanOrEqual(2)
  })

  it('mais de uma página de ditados: "50+" no grupo, e o Ver todos aparece mesmo com poucos na tela', async () => {
    const muitos = Array.from({ length: 4 }, (_, i) => ditado({ id: `x${i}`, created_at: em(23, 8, i), final_text: `Pagar o imposto número ${i + 1}.` }))
    await montar({ ditados: muitos, porPagina: 3 })
    await screen.findByRole('button', { name: /Pagar o imposto número 4/ })
    await buscarPor('imposto')
    const dit = grupo('Ditados · 3+')
    expect(within(dit).getByRole('button', { name: 'Ver todos os 3+ ditados' })).toBeTruthy()
    expect(anuncio()).toContain('3 ou mais ditados')
  })
})

describe('o campo', () => {
  it('é o mesmo nas abas Histórico e Notas, e na Lixeira continua sendo o da lixeira', async () => {
    const srv = await montar()
    expect(campo().getAttribute('placeholder')).toBe(nomeDoCampo)
    fireEvent.click(aba('Notas'))
    expect(campo().getAttribute('placeholder')).toBe(nomeDoCampo)
    fireEvent.click(aba('Lixeira'))
    expect(screen.queryByRole('searchbox', { name: nomeDoCampo })).toBeNull()
    const lixeira = screen.getByRole('searchbox', { name: 'Buscar na lixeira' })
    fireEvent.change(lixeira, { target: { value: 'xyz' } })
    await waitFor(() => expect(srv.pedidos.some((p) => p.caminho === '/api/history' && p.busca.get('trash') === 'true' && p.busca.get('q') === 'xyz')).toBe(true))
    expect(document.querySelector('.busca-unica')).toBeNull()
    expect(srv.pedidos.some((p) => p.caminho === '/api/notas' && p.busca.get('q') === 'xyz')).toBe(false)
  })

  it('na aba Notas, o mesmo campo mostra os dois grupos', async () => {
    await montar()
    fireEvent.click(aba('Notas'))
    await screen.findByText('Fechamento de outubro', { selector: 'h3' })
    await buscarPor('fgts')
    expect(grupo('Notas · 1')).toBeTruthy()
    expect(grupo('Ditados · 3')).toBeTruthy()
    // A grade não é filtrada por trás dos resultados: ela está escondida, inteira.
    expect(document.querySelector('.notas')!.hasAttribute('data-oculta')).toBe(true)
    expect(document.querySelectorAll('.grade [data-nota]').length).toBeGreaterThan(1)
  })

  it('Esc no campo acaba a busca, e a lista da aba volta', async () => {
    await montar()
    await buscarPor('fgts')
    fireEvent.keyDown(campo(), { key: 'Escape' })
    expect(campo().value).toBe('')
    expect(document.querySelector('.busca-unica')).toBeNull()
    expect(await screen.findByRole('button', { name: /Faltam três coisas/ })).toBeTruthy()
    // Sem texto na aba Notas, ela não fica montada por trás do Histórico.
    expect(document.querySelector('.notas')).toBeNull()
  })

  it('apagar o texto também devolve a aba', async () => {
    await montar()
    await buscarPor('fgts')
    fireEvent.change(campo(), { target: { value: '' } })
    expect(await screen.findByRole('button', { name: /Faltam três coisas/ })).toBeTruthy()
  })

  it('trocar de aba limpa a busca', async () => {
    await montar()
    await buscarPor('fgts')
    fireEvent.click(aba('Notas'))
    expect(campo().value).toBe('')
    expect(document.querySelector('.busca-unica')).toBeNull()
  })
})

describe('o teclado', () => {
  it('seta para baixo no campo desce ao primeiro resultado; as setas andam; para cima do primeiro volta ao campo', async () => {
    await montar()
    await buscarPor('fgts')
    campo().focus()
    fireEvent.keyDown(campo(), { key: 'ArrowDown' })
    const todos = resultados()
    expect(todos).toHaveLength(4) // uma nota e três ditados
    expect(document.activeElement).toBe(todos[0])
    fireEvent.keyDown(todos[0]!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(todos[1])
    fireEvent.keyDown(todos[1]!, { key: 'ArrowDown' })
    fireEvent.keyDown(todos[2]!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(todos[3])
    // No último, a seta para baixo não sai da lista.
    fireEvent.keyDown(todos[3]!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(todos[3])
    fireEvent.keyDown(todos[3]!, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(todos[2])
    fireEvent.keyDown(todos[0]!, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(campo())
  })

  it('as setas passam pelo "Ver todas"', async () => {
    const muitas = Array.from({ length: 6 }, (_, i) => nota({ id: `m${i}`, titulo: `Imposto ${i + 1}`, updated_at: em(24, 8, i) }))
    await montar({ notas: muitas })
    await buscarPor('imposto')
    const todos = resultados()
    expect(todos).toHaveLength(6) // 5 notas e o "Ver todas"
    todos[4]!.focus()
    fireEvent.keyDown(todos[4]!, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(todos[5])
    expect(todos[5]!.textContent).toBe('Ver todas as 6 notas')
  })

  it('Enter no campo abre o primeiro resultado', async () => {
    await montar()
    await buscarPor('fgts')
    fireEvent.keyDown(campo(), { key: 'Enter' })
    expect(await screen.findByRole('dialog', { name: 'Nota: Fechamento de outubro' })).toBeTruthy()
  })

  it('Esc num resultado acaba a busca e o foco volta ao campo', async () => {
    await montar()
    await buscarPor('fgts')
    resultados()[1]!.focus()
    fireEvent.keyDown(resultados()[1]!, { key: 'Escape' })
    expect(campo().value).toBe('')
    expect(document.querySelector('.busca-unica')).toBeNull()
    expect(document.activeElement).toBe(campo())
  })

  it('os resultados são botões: Enter e Espaço abrem (o botão faz isso sozinho)', async () => {
    await montar()
    await buscarPor('fgts')
    for (const r of resultados()) expect(r.tagName).toBe('BUTTON')
  })
})

describe('abrir um resultado', () => {
  it('a nota abre na janela, sobre os resultados; ao fechar, os resultados continuam e o foco volta a ela', async () => {
    const srv = await montar()
    await buscarPor('fgts')
    const antes = srv.pedidos.filter((p) => p.caminho === '/api/notas' && p.busca.get('q') === 'fgts').length
    const linha = within(grupo('Notas · 1')).getByRole('button')
    linha.focus()
    fireEvent.click(linha)

    const janela = await screen.findByRole('dialog', { name: 'Nota: Fechamento de outubro' })
    expect(within(janela).getByRole('textbox', { name: 'Título' })).toBeTruthy()
    // A busca fica no campo e os resultados, por trás.
    expect(campo().value).toBe('fgts')
    expect(document.querySelector('.busca-unica')).toBeTruthy()

    fireEvent.click(within(janela).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(document.querySelector('.busca-unica')).toBeTruthy()
    // Os resultados são lidos de novo (a nota pode ter mudado)...
    await waitFor(() => expect(srv.pedidos.filter((p) => p.caminho === '/api/notas' && p.busca.get('q') === 'fgts').length).toBeGreaterThan(antes))
    // ...e o foco volta ao resultado de onde a nota saiu.
    expect(document.activeElement).toBe(within(grupo('Notas · 1')).getByRole('button'))
  })

  it('uma nota editada na janela aparece nova nos resultados quando ela fecha', async () => {
    await montar()
    await buscarPor('fgts')
    fireEvent.click(within(grupo('Notas · 1')).getByRole('button'))
    const janela = await screen.findByRole('dialog')
    fireEvent.change(within(janela).getByRole('textbox', { name: 'Título' }), { target: { value: 'FGTS e DAS de outubro' } })
    fireEvent.click(within(janela).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await waitFor(() => expect(within(grupo('Notas · 1')).getByRole('button').textContent).toContain('FGTS e DAS de outubro'))
  })

  it('abrir a mesma nota duas vezes funciona', async () => {
    await montar()
    await buscarPor('fgts')
    for (let i = 0; i < 2; i++) {
      fireEvent.click(within(grupo('Notas · 1')).getByRole('button'))
      const janela = await screen.findByRole('dialog')
      fireEvent.click(within(janela).getByRole('button', { name: 'Fechar' }))
      await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    }
  })

  it('a nota achada na aba Histórico não deixa a aba Notas montada depois que a busca acaba', async () => {
    await montar()
    await buscarPor('fgts')
    fireEvent.click(within(grupo('Notas · 1')).getByRole('button'))
    const janela = await screen.findByRole('dialog')
    fireEvent.click(within(janela).getByRole('button', { name: 'Fechar' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    fireEvent.keyDown(campo(), { key: 'Escape' })
    expect(document.querySelector('.notas')).toBeNull()
    // Buscar de novo não reabre a nota.
    await buscarPor('fgts')
    await act(async () => {})
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('o ditado abre no editor do Histórico, na lista filtrada pela busca', async () => {
    const srv = await montar()
    await buscarPor('padaria')
    fireEvent.click(within(grupo('Ditados · 1')).getByRole('button'))

    const texto = await screen.findByLabelText<HTMLTextAreaElement>('Texto final')
    expect(texto.value).toBe('O FGTS da padaria venceu dia 20, confere se pagaram.')
    expect(document.activeElement).toBe(texto)
    expect(aba('Histórico').getAttribute('aria-pressed')).toBe('true')
    expect(document.querySelector('.busca-unica')).toBeNull()
    expect(document.querySelector('.escopo-busca')!.textContent).toContain('Só nos ditados')
    expect(campo().value).toBe('padaria')
    // A lista filtrada é a do mesmo texto.
    expect(srv.pedidos.filter((p) => p.caminho === '/api/history' && p.busca.get('q') === 'padaria').length).toBeGreaterThanOrEqual(2)
  })

  it('o ditado achado na aba Notas leva ao Histórico', async () => {
    await montar()
    fireEvent.click(aba('Notas'))
    await buscarPor('padaria')
    fireEvent.click(within(grupo('Ditados · 1')).getByRole('button'))
    await screen.findByLabelText('Texto final')
    expect(aba('Histórico').getAttribute('aria-pressed')).toBe('true')
    expect(aba('Notas').getAttribute('aria-pressed')).toBe('false')
  })

  it('Esc depois de abrir o ditado limpa a busca e volta à lista inteira', async () => {
    await montar()
    await buscarPor('padaria')
    fireEvent.click(within(grupo('Ditados · 1')).getByRole('button'))
    await screen.findByLabelText('Texto final')
    fireEvent.keyDown(campo(), { key: 'Escape' })
    expect(document.querySelector('.escopo-busca')).toBeNull()
    expect(await screen.findByRole('button', { name: /Faltam três coisas/ })).toBeTruthy()
  })
})

describe('os estados', () => {
  it('nada encontrado, e a região viva diz', async () => {
    await montar()
    await buscarPor('zzz')
    expect(document.querySelector('.busca-unica .estado p')!.textContent).toBe('Nada encontrado para “zzz”.')
    expect(anuncio()).toBe('Nada encontrado para “zzz”.')
  })

  it('a região viva diz quantas notas e quantos ditados', async () => {
    await montar()
    await buscarPor('fgts')
    expect(anuncio()).toBe('1 nota e 3 ditados para “fgts”.')
    fireEvent.change(campo(), { target: { value: 'padaria' } })
    await waitFor(() => expect(anuncio()).toBe('1 ditado para “padaria”.'))
  })

  it('"Carregando…" enquanto a primeira busca não responde', async () => {
    const srv = await montar()
    const soltar = srv.pausar((p) => p.caminho === '/api/history' && p.busca.get('q') === 'fgts')
    fireEvent.change(campo(), { target: { value: 'fgts' } })
    await waitFor(() => expect(document.querySelector('.busca-unica .estado')!.textContent).toBe('Carregando…'))
    soltar()
    await screen.findByRole('region', { name: 'Ditados · 3' })
  })

  it('as notas falham e os ditados chegam: o grupo que falhou diz por quê e tem Tentar de novo', async () => {
    const srv = await montar()
    srv.interceptar((p) => (p.caminho === '/api/notas' && p.busca.has('q') ? new Response('{"error":{"code":"INTERNAL","message":"deu ruim","request_id":"r"}}', { status: 500 }) : undefined))
    await buscarPor('fgts')
    const nas = await screen.findByRole('region', { name: 'Notas' })
    expect(nas.textContent).toContain('Não consegui buscar nas notas')
    expect(grupo('Ditados · 3')).toBeTruthy()
    expect(anuncio()).toContain('Não consegui buscar nas notas.')

    srv.interceptar(null)
    fireEvent.click(within(nas).getByRole('button', { name: 'Tentar de novo' }))
    await screen.findByRole('region', { name: 'Notas · 1' })
  })

  it('o Agent fora do ar acende a faixa do topo, como em toda tela', async () => {
    const srv = await montar()
    srv.derrubar(true)
    fireEvent.change(campo(), { target: { value: 'fgts' } })
    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toContain('não está respondendo')
  })

  it('a resposta lenta de uma busca antiga não aparece embaixo da nova', async () => {
    const srv = await montar()
    const soltar = srv.pausar((p) => p.caminho === '/api/history' && p.busca.get('q') === 'fg')
    fireEvent.change(campo(), { target: { value: 'fg' } })
    await waitFor(() => expect(pedidosA(srv, '/api/history', 'fg')).toHaveLength(1))

    fireEvent.change(campo(), { target: { value: 'padaria' } })
    await screen.findByRole('region', { name: 'Ditados · 1' })
    soltar()
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50))
    })
    // Os ditados de "fg" (os três do FGTS) não tomaram o lugar dos de "padaria".
    expect(screen.getByRole('region', { name: 'Ditados · 1' })).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Ditados · 3' })).toBeNull()
    expect(anuncio()).toBe('1 ditado para “padaria”.')
  })

  it('digitar depressa só pede o que parou de ser digitado (a espera da digitação)', async () => {
    const srv = await montar()
    for (const t of ['f', 'fg', 'fgt', 'fgts']) fireEvent.change(campo(), { target: { value: t } })
    await screen.findByRole('region', { name: 'Ditados · 3' })
    expect(srv.pedidos.filter((p) => p.caminho === '/api/notas' && p.busca.has('q')).map((p) => p.busca.get('q'))).toEqual(['fgts'])
  })
})

describe('o painel da nuvem', () => {
  it('sem ambiente.buscaUnica, a busca é a do histórico, como sempre: um pedido só, sem notas', async () => {
    const srv = servidorFalso(ditados(), { notas: notas() })
    renderizarSoHistorico(srv.buscar)
    await screen.findByRole('button', { name: /Faltam três coisas/ })
    expect(screen.queryByRole('searchbox', { name: nomeDoCampo })).toBeNull()
    fireEvent.change(screen.getByRole('searchbox', { name: 'Buscar no histórico' }), { target: { value: 'padaria' } })
    await waitFor(() => expect(screen.queryByRole('button', { name: /Faltam três coisas/ })).toBeNull())
    expect(screen.getByRole('button', { name: /FGTS da padaria/ })).toBeTruthy()
    expect(document.querySelector('.busca-unica')).toBeNull()
    expect(document.querySelector('.escopo-busca')).toBeNull()
    expect(srv.pedidos.some((p) => p.caminho.startsWith('/api/notas') && p.busca.has('q'))).toBe(false)
    expect(pedidosA(srv, '/api/history', 'padaria')).toHaveLength(1)
  })
})
