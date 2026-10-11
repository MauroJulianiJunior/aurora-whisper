// A seção Aprendido das Configurações do celular (ADR-019, F8h) contra a nuvem
// de mentira (testes/servidorFalsoNuvem.ts): a lista das trocas dos PCs, o
// Desfazer que pergunta na própria linha, o teto do servidor e os erros.
//
// O que se prova aqui é o que a tela faz: quais pedidos saem (e que o texto da
// troca nunca vai na URL) e o que a pessoa vê. O que a nuvem decide (a regra
// do desfazer, o que chega aos PCs pelo pull) é dos testes Go
// (internal/server, contracttest/learned_celular.go).

import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type OpcoesDaNuvem, aprendido, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

const sf = aprendido({ variante: 'sf', termo: 'ECF' })
const n8n = aprendido({ variante: 'n oito n', termo: 'n8n' })
const dctf = aprendido({ variante: 'dctf web', termo: 'DCTFWeb', dominio: 'contabilidade' })
const rj = aprendido({ variante: 'rj', termo: 'RF', dominio: 'contabilidade', restrito: true })

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 7, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

async function abrirConfiguracoes(opcoes: OpcoesDaNuvem = {}) {
  const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(7, 14), final_text: 'Um ditado.' })], opcoes)
  render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
  // A primeira renderização do arquivo, com a máquina ocupada (os outros testes
  // do repositório rodam junto), passa do 1 s padrão do findBy.
  await screen.findByRole('button', { name: /Um ditado/ }, { timeout: 5000 })
  fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { level: 2, name: 'Aprendido' }, { timeout: 5000 })
  return srv
}

const secao = () => screen.getByRole('region', { name: 'Aprendido' })
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const doAprendido = (srv: ReturnType<typeof servidorFalsoNuvem>) => srv.pedidos.filter((p) => p.caminho.startsWith('/api/v1/learned'))

describe('a seção Aprendido', () => {
  it('fica entre os Aparelhos e as Sessões abertas', async () => {
    await abrirConfiguracoes()
    const titulos = within(document.querySelector<HTMLElement>('.config')!).getAllByRole('heading', { level: 2 })
    expect(titulos.map((h) => h.textContent)).toEqual(['Conta', 'Ditar pelo celular', 'Aparelhos', 'Aprendido', 'Sessões abertas', 'Privacidade'])
  })

  it('sem nada aprendido, diz com discrição e não tem botão', async () => {
    const srv = await abrirConfiguracoes()
    await within(secao()).findByText('Nada aprendido ainda.')
    expect(within(secao()).queryByRole('button')).toBeNull()
    expect(within(secao()).queryByRole('alert')).toBeNull()
    expect(secao().textContent).not.toContain('Só as mais recentes')
    expect(doAprendido(srv).map((p) => `${p.metodo} ${p.caminho}`)).toEqual(['GET /api/v1/learned'])
  })

  it('uma linha por troca: a variante vira o termo, na mono, com o Desfazer; o leitor de tela ouve "vira"', async () => {
    await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    const linhas = await within(secao()).findAllByRole('listitem')
    expect(linhas).toHaveLength(2)
    expect(Array.from(linhas[0]!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['sf', 'ECF'])
    expect(Array.from(linhas[1]!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['n oito n', 'n8n'])
    // A seta é só para os olhos.
    expect(linhas[0]!.querySelector('.sessao-nome')!.textContent).toBe('sf → vira ECF')
    expect(linhas[0]!.querySelector('[aria-hidden="true"]')!.textContent).toBe(' →')
    expect(within(linhas[0]!).getByRole('button', { name: 'Desfazer: sf vira ECF' })).toBeTruthy()
    // Sem Aprender: o celular não aprende.
    expect(within(secao()).queryByRole('button', { name: /Aprender/ })).toBeNull()
    expect(secao().textContent).not.toContain('Aprender')
  })

  it('o domínio aparece quando há; a troca presa a ele diz "só no domínio"', async () => {
    await abrirConfiguracoes({ aprendidos: [sf, dctf, rj] })
    const [semDominio, comDominio, restrita] = await within(secao()).findAllByRole('listitem')
    expect(semDominio!.querySelector('.sessao-uso')).toBeNull()
    expect(comDominio!.querySelector('.sessao-uso')!.textContent).toBe(', domínio contabilidade')
    expect(restrita!.querySelector('.sessao-uso')!.textContent).toBe(', só no domínio contabilidade')
    // A vírgula é só para o leitor de tela.
    expect(comDominio!.querySelector('.sessao-uso .so-leitor')!.textContent).toBe(', ')
  })

  it('a ordem é a do servidor (a mais recente primeiro)', async () => {
    await abrirConfiguracoes({ aprendidos: [n8n, sf, dctf] })
    const linhas = await within(secao()).findAllByRole('listitem')
    expect(linhas.map((l) => l.querySelector('.termo')!.textContent)).toEqual(['n oito n', 'sf', 'dctf web'])
  })

  it('o texto da troca nunca vai na URL: a lista é um GET sem query', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf] })
    await within(secao()).findAllByRole('listitem')
    for (const p of doAprendido(srv)) {
      expect(p.busca.toString()).toBe('')
      expect(p.caminho).not.toContain('sf')
      expect(p.credentials).toBe('same-origin')
      expect('Authorization' in p.cabecalhos).toBe(false)
    }
  })

  it('a lista é lida de novo quando a tela volta a aparecer', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf] })
    await within(secao()).findAllByRole('listitem')
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    // Um PC aprendeu mais uma enquanto a tela estava escondida.
    srv.aprendidos.unshift(n8n)
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await waitFor(() => expect(within(secao()).getAllByRole('listitem')).toHaveLength(2))
    expect(doAprendido(srv).filter((p) => p.metodo === 'GET')).toHaveLength(2)
  })
})

describe('o teto do servidor', () => {
  it('passou do teto: uma linha diz que só as mais recentes aparecem, sem alarme', async () => {
    await abrirConfiguracoes({ aprendidos: [sf, n8n, dctf], tetoDosAprendidos: 2 })
    const linhas = await within(secao()).findAllByRole('listitem')
    expect(linhas).toHaveLength(2)
    const nota = within(secao()).getByText('Só as mais recentes aparecem.')
    // Informação, e não erro: nada de alerta nem de nota de risco.
    expect(nota.className).toBe('ajuda')
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })

  it('no teto exato, a linha não aparece', async () => {
    await abrirConfiguracoes({ aprendidos: [sf, n8n], tetoDosAprendidos: 2 })
    await within(secao()).findAllByRole('listitem')
    expect(secao().textContent).not.toContain('Só as mais recentes')
  })

  it('has_more ausente, ou que não é true, vale como false; items null é a lista vazia', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(7, 14), final_text: 'Um ditado.' })])
    srv.interceptar((p) => (p.caminho === '/api/v1/learned' ? new Response(JSON.stringify({ items: null, has_more: 'sim' }), { status: 200 }) : undefined))
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
    await screen.findByRole('button', { name: /Um ditado/ })
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await within(await screen.findByRole('region', { name: 'Aprendido' })).findByText('Nada aprendido ainda.')
    expect(secao().textContent).not.toContain('Só as mais recentes')
  })
})

describe('Desfazer', () => {
  const linha = (nome: string) => within(secao()).getByRole('button', { name: nome }).closest('li')!

  it('pergunta na própria linha, com o foco no Cancelar; nada sai antes da confirmação', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    const pergunta = within(secao()).getByRole('group', { name: 'sf vira ECF. Desfazer? A troca deixa de valer em todos os PCs.' })
    expect(linha('Desfazer: n oito n vira n8n')).not.toBe(pergunta.closest('li'))
    expect(pergunta.closest('li')!.querySelector('.termo')!.textContent).toBe('sf')
    await waitFor(() => expect(document.activeElement).toBe(within(pergunta).getByRole('button', { name: 'Cancelar' })))
    expect(within(pergunta).getByRole('button', { name: 'Desfazer' }).className).toBe('botao risco')
    expect(srv.pedidos.some((p) => p.metodo === 'POST')).toBe(false)
  })

  it('com 10 trocas, quem ouve o Cancelar sabe qual troca vai sair: o nome do grupo traz a troca, e o texto que se vê não muda', async () => {
    const dez = Array.from({ length: 10 }, (_, i) => aprendido({ variante: `v${i}`, termo: `T${i}` }))
    await abrirConfiguracoes({ aprendidos: dez })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: v4 vira T4' }))
    const cancelar = within(secao()).getByRole('button', { name: 'Cancelar' })
    await waitFor(() => expect(document.activeElement).toBe(cancelar))
    // O grupo que contém o foco é o da quinta troca, e o nome diz qual é.
    const grupo = cancelar.closest('[role="group"]')!
    expect(within(secao()).getByRole('group', { name: 'v4 vira T4. Desfazer? A troca deixa de valer em todos os PCs.' })).toBe(grupo)
    expect(grupo.querySelector('p')!.textContent).toBe('Desfazer? A troca deixa de valer em todos os PCs.')
    expect(within(secao()).getAllByRole('group')).toHaveLength(1)
  })

  it('o Esc desiste e devolve o foco ao Desfazer da linha; o Cancelar também', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    const pergunta = within(secao()).getByRole('group')
    fireEvent.keyDown(pergunta, { key: 'Escape' })
    await waitFor(() => expect(within(secao()).queryByRole('group')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Desfazer: sf vira ECF' })))

    fireEvent.click(within(secao()).getByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(secao()).getByRole('button', { name: 'Cancelar' }))
    await waitFor(() => expect(within(secao()).queryByRole('group')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Desfazer: sf vira ECF' })))
    expect(srv.pedidos.some((p) => p.metodo === 'POST')).toBe(false)
    expect(srv.aprendidos).toHaveLength(1)
  })

  it('perguntar por outra linha troca a pergunta, uma por vez', async () => {
    await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(secao()).getByRole('button', { name: 'Desfazer: n oito n vira n8n' }))
    const grupos = within(secao()).getAllByRole('group')
    expect(grupos).toHaveLength(1)
    expect(grupos[0]!.closest('li')!.querySelector('.termo')!.textContent).toBe('n oito n')
  })

  it('confirmar desfaz: um POST com o id no corpo, a linha sai, a pílula avisa e o foco vai para a vizinha', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(within(secao()).getAllByRole('listitem')).toHaveLength(1))

    const posts = doAprendido(srv).filter((p) => p.metodo === 'POST')
    expect(posts.map((p) => [p.caminho, p.corpo])).toEqual([['/api/v1/learned/undo', { id: sf.id }]])
    // O texto da troca não vai na URL.
    expect(posts[0]!.caminho).not.toContain('sf')
    expect(posts[0]!.busca.toString()).toBe('')
    expect(srv.aprendidos.map((a) => a.id)).toEqual([n8n.id])
    expect(aviso().textContent).toBe('Desfeito em todos os aparelhos.')
    expect(within(secao()).queryByText('sf')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Desfazer: n oito n vira n8n' })))
  })

  it('desfazer a última troca: "Nada aprendido ainda." e o foco no título da seção', async () => {
    await abrirConfiguracoes({ aprendidos: [sf] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await within(secao()).findByText('Nada aprendido ainda.')
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Aprendido'))
  })

  it('durante o POST o botão diz "Desfazendo…", fica desligado, e um segundo clique não pede de novo', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(7, 14), final_text: 'Um ditado.' })], { aprendidos: [sf, n8n] })
    let soltar: () => void = () => {}
    const espera = new Promise<void>((r) => {
      soltar = r
    })
    // Segura o POST do desfazer até o teste soltar, e deixa passar o resto.
    const passagem = (entrada: string, init?: RequestInit) =>
      entrada === '/api/v1/learned/undo' ? espera.then(() => srv.buscar(entrada, init)) : srv.buscar(entrada, init)
    render(<AppNuvem buscar={passagem} irPara={vi.fn()} />)
    await screen.findByRole('button', { name: /Um ditado/ })
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    fireEvent.click(await within(await screen.findByRole('region', { name: 'Aprendido' })).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    const confirmar = within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' })
    fireEvent.click(confirmar)
    await within(secao()).findByRole('button', { name: 'Desfazendo…' })
    expect(within(secao()).getByRole('button', { name: 'Desfazendo…' }).getAttribute('aria-disabled')).toBe('true')
    fireEvent.click(within(secao()).getByRole('button', { name: 'Desfazendo…' }))
    fireEvent.click(within(secao()).getByRole('button', { name: 'Desfazer: n oito n vira n8n' }))
    soltar()
    await waitFor(() => expect(within(secao()).getAllByRole('listitem')).toHaveLength(1))
    expect(doAprendido(srv).filter((p) => p.metodo === 'POST')).toHaveLength(1)
    // O clique no Desfazer da outra linha, com o POST no ar, não abriu pergunta.
    expect(within(secao()).queryByRole('group')).toBeNull()
  })

  it('com o POST no ar, o Cancelar e o Esc esperam: a pergunta fica, e o pedido que a pessoa fez termina como ela viu', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(7, 14), final_text: 'Um ditado.' })], { aprendidos: [sf, n8n] })
    let soltar: () => void = () => {}
    const espera = new Promise<void>((r) => {
      soltar = r
    })
    const passagem = (entrada: string, init?: RequestInit) =>
      entrada === '/api/v1/learned/undo' ? espera.then(() => srv.buscar(entrada, init)) : srv.buscar(entrada, init)
    render(<AppNuvem buscar={passagem} irPara={vi.fn()} />)
    await screen.findByRole('button', { name: /Um ditado/ })
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    fireEvent.click(await within(await screen.findByRole('region', { name: 'Aprendido' })).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await within(secao()).findByRole('button', { name: 'Desfazendo…' })

    const cancelar = within(secao()).getByRole('button', { name: 'Cancelar' })
    expect(cancelar.getAttribute('aria-disabled')).toBe('true')
    fireEvent.click(cancelar)
    fireEvent.keyDown(within(secao()).getByRole('group'), { key: 'Escape' })
    // Nada fechou: a pergunta e o "Desfazendo…" seguem, e a linha também.
    expect(within(secao()).getByRole('group')).toBeTruthy()
    expect(within(secao()).getByRole('button', { name: 'Desfazendo…' })).toBeTruthy()
    expect(within(secao()).getAllByRole('listitem')).toHaveLength(2)

    soltar()
    await waitFor(() => expect(within(secao()).getAllByRole('listitem')).toHaveLength(1))
    expect(aviso().textContent).toBe('Desfeito em todos os aparelhos.')
    expect(doAprendido(srv).filter((p) => p.metodo === 'POST')).toHaveLength(1)
    expect(srv.aprendidos.map((a) => a.id)).toEqual([n8n.id])
  })

  it('o Cancelar volta a valer quando o pedido falha: a pergunta fecha, e o foco volta ao Desfazer da linha', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf] })
    srv.interceptar((p) => (p.caminho === '/api/v1/learned/undo' ? new Response(JSON.stringify({ error: { message: 'falha interna' } }), { status: 500 }) : undefined))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(within(secao()).getByRole('alert').textContent).toBe('Não deu para desfazer: falha interna'))
    expect(within(secao()).getByRole('button', { name: 'Cancelar' }).getAttribute('aria-disabled')).toBeNull()
    fireEvent.click(within(secao()).getByRole('button', { name: 'Cancelar' }))
    await waitFor(() => expect(within(secao()).queryByRole('group')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(within(secao()).getByRole('button', { name: 'Desfazer: sf vira ECF' })))
  })

  it('um 404 que não é o do servidor (sem a frase) não desfez nada: erro como os outros, a linha e a pergunta ficam', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    // O painel estático publicado antes do binário: a rota nova não existe.
    srv.interceptar((p) => (p.caminho === '/api/v1/learned/undo' ? new Response(JSON.stringify({ error: { message: 'rota não encontrada' } }), { status: 404 }) : undefined))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(within(secao()).getByRole('alert').textContent).toBe('Não deu para desfazer: rota não encontrada'))
    expect(within(secao()).getAllByRole('listitem')).toHaveLength(2)
    expect(within(secao()).getByRole('group')).toBeTruthy()
    expect(aviso().textContent).not.toContain('Desfeito')
    expect(srv.aprendidos).toHaveLength(2)

    // Um proxy que responde 404 com uma página, e não com o JSON do contrato.
    srv.interceptar((p) => (p.caminho === '/api/v1/learned/undo' ? new Response('<html>Not Found</html>', { status: 404 }) : undefined))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(within(secao()).getByRole('alert').textContent).toBe('Não deu para desfazer: erro 404'))
    expect(within(secao()).getAllByRole('listitem')).toHaveLength(2)
    expect(aviso().textContent).not.toContain('Desfeito')
  })

  it('desfeita em outro lugar (404): some da lista do mesmo jeito, e a pílula avisa', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    // Outra conta, ou id que não existe mais: o servidor responde 404.
    srv.interceptar((p) => (p.caminho === '/api/v1/learned/undo' ? new Response(JSON.stringify({ error: { message: 'troca aprendida não encontrada' } }), { status: 404 }) : undefined))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(within(secao()).getAllByRole('listitem')).toHaveLength(1))
    expect(within(secao()).queryByRole('alert')).toBeNull()
    expect(aviso().textContent).toBe('Desfeito em todos os aparelhos.')
  })

  it('o desfazer repetido de uma troca que outro aparelho já desfez dá certo (200 do servidor)', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    await within(secao()).findAllByRole('listitem')
    srv.desfazerPorFora(sf.id)
    fireEvent.click(within(secao()).getByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(within(secao()).getAllByRole('listitem')).toHaveLength(1))
    expect(aviso().textContent).toBe('Desfeito em todos os aparelhos.')
  })

  it('outro erro: diz num alerta, a linha e a pergunta ficam, e dá para tentar de novo', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf] })
    srv.interceptar((p) => (p.caminho === '/api/v1/learned/undo' ? new Response(JSON.stringify({ error: { message: 'falha interna' } }), { status: 500 }) : undefined))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await waitFor(() => expect(within(secao()).getByRole('alert').textContent).toBe('Não deu para desfazer: falha interna'))
    expect(within(secao()).getByText('sf')).toBeTruthy()
    expect(within(secao()).getByRole('group')).toBeTruthy()
    expect(within(secao()).getByRole('button', { name: 'Desfazer' }).getAttribute('aria-disabled')).toBeNull()

    srv.interceptar(null)
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    await within(secao()).findByText('Nada aprendido ainda.')
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })

  it('sem rede no Desfazer: a frase do app, e a linha fica', async () => {
    const srv = await abrirConfiguracoes({ aprendidos: [sf] })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    srv.derrubar(true)
    fireEvent.click(within(within(secao()).getByRole('group')).getByRole('button', { name: 'Desfazer' }))
    // A ligação caiu: a faixa do topo fala, e a troca continua na lista.
    await waitFor(() => expect(document.querySelector('.faixa')).toBeTruthy())
    expect(within(secao()).getByText('sf')).toBeTruthy()
    expect(srv.aprendidos).toHaveLength(1)
  })
})

describe('os erros da lista', () => {
  it('um erro que não é da ligação diz na seção, no molde das outras, e o resto da tela segue', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(7, 14), final_text: 'Um ditado.' })])
    srv.interceptar((p) => (p.caminho === '/api/v1/learned' ? new Response(JSON.stringify({ error: { message: 'falha interna' } }), { status: 500 }) : undefined))
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
    await screen.findByRole('button', { name: /Um ditado/ })
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    const alerta = await within(await screen.findByRole('region', { name: 'Aprendido' })).findByRole('alert')
    expect(alerta.textContent).toBe('Não deu para ver o aprendido: falha interna')
    // Nem "Carregando…" nem "Nada aprendido ainda.": não se sabe.
    expect(secao().textContent).not.toContain('Carregando')
    expect(secao().textContent).not.toContain('Nada aprendido ainda.')
    // As outras seções continuam.
    await screen.findByText('Nenhum PC conectado.')
    expect(document.querySelector('.faixa')).toBeNull()
  })

  it('a sessão que acabou na lista leva à tela de sessão expirada', async () => {
    const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(7, 14), final_text: 'Um ditado.' })])
    render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} />)
    await screen.findByRole('button', { name: /Um ditado/ })
    srv.interceptar((p) => (p.caminho === '/api/v1/learned' ? new Response('', { status: 401 }) : undefined))
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { level: 1, name: 'Sua sessão terminou.' })
  })
})

describe('apagar meus dados da nuvem', () => {
  it('apaga também o aprendido, e a seção lê de novo', async () => {
    await abrirConfiguracoes({ aprendidos: [sf, n8n] })
    await within(secao()).findAllByRole('listitem')
    fireEvent.click(screen.getByRole('button', { name: 'Apagar meus dados da nuvem' }))
    fireEvent.change(screen.getByLabelText(/Para confirmar, digite/), { target: { value: 'apagar' } })
    fireEvent.click(screen.getByRole('button', { name: 'Apagar tudo' }))
    await within(secao()).findByText('Nada aprendido ainda.')
  })
})

describe('a Privacidade do celular', () => {
  it('diz que as trocas aprendidas também vão para a nuvem', async () => {
    await abrirConfiguracoes()
    const priv = screen.getByRole('region', { name: 'Privacidade' })
    expect(priv.textContent).toContain('Vão para a nuvem: o texto dos ditados, com a data, o PC de origem e os detalhes da transcrição, e as trocas aprendidas.')
    expect(priv.textContent).toContain('Do PC nunca vão: o áudio, o que está nas outras janelas, a chave do Polish e as configurações dos PCs; a chave do PC continua no PC.')
  })
})
