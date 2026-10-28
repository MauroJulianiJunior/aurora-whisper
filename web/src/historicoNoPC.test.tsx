// O bloco "Histórico" das Configurações do PC (ADR-019, "Decisões da F10",
// F10d e F10e) contra o Agent de mentira (testes/servidorFalso.ts): a pergunta
// dos 6 meses na própria linha, o OK com a contagem que a tela mostrou, o
// Desligar, a regra pausada pelo freio, o Agent de antes da F10, as cópias do
// banco e a linha da Privacidade.
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos saem,
// o que a pessoa vê e onde o foco fica. O que o Agent apaga de verdade, o portão
// que recusa por padrão e as cópias em disco são dos testes Go (internal/storage,
// internal/agent/localapi e cmd/agent).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { Buscador } from './api'
import type { EstadoRetencao } from './clienteLocal'
import { copiasApagadas } from './textosDoHistorico'
import { type OpcoesDoServidor, type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

async function abrir(opcoes: OpcoesDoServidor = {}, aoBuscar?: (b: Buscador) => Buscador) {
  const srv = servidorFalso([ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda.' })], opcoes)
  render(<App sessao={Promise.resolve('tok')} buscar={aoBuscar ? aoBuscar(srv.buscar) : srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'Nuvem' })
  await act(async () => {})
  return srv
}

const secao = () => screen.getByRole('region', { name: 'Histórico' })
const semSecao = () => screen.queryByRole('region', { name: 'Histórico' })
const botao = (nome: string | RegExp) => within(secao()).getByRole<HTMLButtonElement>('button', { name: nome })
const desligado = (b: HTMLElement) => b.getAttribute('aria-disabled') === 'true'
const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && (metodo === undefined || p.metodo === metodo))
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent
const alertas = () => within(secao()).queryAllByRole('alert').map((a) => a.textContent)
const botaoSalvar = () => screen.getByRole('button', { name: 'Salvar configurações' })

const estado = (p: Partial<EstadoRetencao>): Partial<EstadoRetencao> => p

const perguntaDe1240 =
  '1.240 ditados têm mais de 6 meses. Ao confirmar, eles saem deste PC de vez, e daqui em diante o que fizer 6 meses sai sozinho. Antes, uma cópia do histórico fica neste PC.'

function voltarAAba() {
  act(() => void document.dispatchEvent(new Event('visibilitychange')))
}

describe('o bloco Histórico do PC: quando aparece', () => {
  it('não aparece sem ditados de mais de 6 meses, sem a regra ligada e sem cópias; o Agent é perguntado uma vez', async () => {
    const srv = await abrir()
    expect(semSecao()).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Histórico' })).toBeNull()
    const gets = pedidosA(srv, '/api/retencao')
    expect(gets).toHaveLength(1)
    expect(gets[0]!.metodo).toBe('GET')
    expect(gets[0]!.auth).toBe('Bearer tok')
    // Perguntar não apaga nada: nenhum POST.
    expect(srv.pedidos.filter((p) => p.caminho.startsWith('/api/retencao') && p.metodo === 'POST')).toHaveLength(0)
  })

  it('pausada pelo freio, mas sem nada a apagar: o bloco não aparece', async () => {
    await abrir({ retencao: estado({ previa: 0, pausada: true }) })
    expect(semSecao()).toBeNull()
  })

  it('só com cópias ao lado do banco, ou só com a regra ligada, aparece', async () => {
    await abrir({ retencao: estado({ copias: 2 }) })
    expect(secao().textContent).toContain('Há 2 cópias do histórico ao lado do banco, com o texto de antes.')
    cleanup()
    await abrir({ retencao: estado({ ligada: true }) })
    expect(secao().textContent).toContain('Os 6 meses estão ligados')
  })

  it('fica depois da Nuvem e antes da Privacidade', async () => {
    await abrir({ retencao: estado({ previa: 3 }) })
    const titulos = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(titulos.indexOf('Histórico')).toBeGreaterThan(titulos.indexOf('Nuvem'))
    expect(titulos.indexOf('Histórico')).toBeLessThan(titulos.indexOf('Privacidade'))
  })

  it('a tela das Configurações fechada, ou a aba escondida, não pergunta de novo', async () => {
    const srv = await abrir({ retencao: estado({ previa: 3 }) })
    const visibilidade = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    voltarAAba()
    expect(pedidosA(srv, '/api/retencao', 'GET')).toHaveLength(1)
    visibilidade.mockReturnValue('visible')
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    voltarAAba()
    expect(pedidosA(srv, '/api/retencao', 'GET')).toHaveLength(1)
  })

  it('voltar à aba pergunta de novo: a contagem nova troca a frase', async () => {
    const srv = await abrir({ retencao: estado({ previa: 1240 }) })
    expect(secao().textContent).toContain(perguntaDe1240)
    srv.retencaoPorFora({ previa: 1301 })
    voltarAAba()
    await within(secao()).findByText(/^1\.301 ditados têm mais de 6 meses\./)
    expect(pedidosA(srv, '/api/retencao', 'GET')).toHaveLength(2)
  })
})

describe('o bloco Histórico do PC: a pergunta', () => {
  it('diz o que acontece ao confirmar, com o milhar em pt-BR, e as linhas só quando maiores que 0', async () => {
    const srv = await abrir({ retencao: estado({ previa: 1240, na_lixeira: 3, com_selo: 2, de_outro_dono: 5 }) })
    const el = secao()
    expect(within(el).getByText(perguntaDe1240)).toBeTruthy()
    expect(within(el).getByText('3 deles estão na lixeira.')).toBeTruthy()
    expect(within(el).getByText('2 têm duas versões, e as duas saem.')).toBeTruthy()
    expect(within(el).getByText('5 são de outro dono e ficam.')).toBeTruthy()
    const apagar = botao('Apagar os mais antigos')
    expect(desligado(apagar)).toBe(false)
    // Sem o clique, nada sai: nenhum POST, e nenhuma pergunta de confirmação.
    expect(within(el).queryByRole('group')).toBeNull()
    expect(srv.pedidos.filter((p) => p.metodo === 'POST' && p.caminho.startsWith('/api/retencao'))).toHaveLength(0)
  })

  it('o singular: 1 ditado tem, ele sai; ele está na lixeira (e não "1 deles", com um só)', async () => {
    await abrir({ retencao: estado({ previa: 1, na_lixeira: 1, com_selo: 1, de_outro_dono: 1 }) })
    const el = secao()
    expect(
      within(el).getByText(
        '1 ditado tem mais de 6 meses. Ao confirmar, ele sai deste PC de vez, e daqui em diante o que fizer 6 meses sai sozinho. Antes, uma cópia do histórico fica neste PC.',
      ),
    ).toBeTruthy()
    expect(within(el).getByText('Ele está na lixeira.')).toBeTruthy()
    expect(within(el).getByText('Ele tem duas versões, e as duas saem.')).toBeTruthy()
    expect(within(el).getByText('1 é de outro dono e fica.')).toBeTruthy()
    expect(el.textContent).not.toContain('1 deles')
  })

  it('com mais de um, 1 deles está na lixeira e 1 tem duas versões', async () => {
    await abrir({ retencao: estado({ previa: 7, na_lixeira: 1, com_selo: 1 }) })
    expect(within(secao()).getByText('1 deles está na lixeira.')).toBeTruthy()
    expect(within(secao()).getByText('1 tem duas versões, e as duas saem.')).toBeTruthy()
  })

  it('as linhas de lixeira, selo e outro dono não aparecem quando são 0', async () => {
    await abrir({ retencao: estado({ previa: 5 }) })
    const t = secao().textContent!
    expect(t).not.toContain('na lixeira')
    expect(t).not.toContain('duas versões')
    expect(t).not.toContain('outro dono')
    expect(t).not.toContain('virou nota')
    expect(t).not.toContain('viraram nota')
  })

  // O ditado que virou nota segue os 6 meses (o dossiê de LGPD, decisão 11): o
  // histórico não o mostra, e a pergunta, que o conta, diz que a nota fica.
  it('os que viraram nota: a linha diz que a nota fica, no plural, com 1 deles e com um só; sem o campo (Agent de antes), nada', async () => {
    await abrir({ retencao: estado({ previa: 1240, viraram_nota: 12 }) })
    expect(within(secao()).getByText('12 deles viraram nota: saem os ditados, e as notas ficam.')).toBeTruthy()
    cleanup()
    await abrir({ retencao: estado({ previa: 7, viraram_nota: 1 }) })
    expect(within(secao()).getByText('1 deles virou nota: sai o ditado, e a nota fica.')).toBeTruthy()
    cleanup()
    await abrir({ retencao: estado({ previa: 1, viraram_nota: 1 }) })
    expect(within(secao()).getByText('Ele virou nota: sai o ditado, e a nota fica.')).toBeTruthy()
    cleanup()
    await abrir({ retencao: estado({ previa: 5 }) })
    expect(secao().textContent).not.toContain('nota fica')
  })

  it('o clique pergunta de novo na própria linha: o foco vai ao Cancelar, e o Esc cancela e devolve o foco', async () => {
    const srv = await abrir({ retencao: estado({ previa: 1240 }) })
    fireEvent.click(botao('Apagar os mais antigos'))
    const grupo = await within(secao()).findByRole('group')
    expect(grupo.textContent).toBe('Apagar 1.240 ditados? Não dá para desfazer.ApagarCancelar')
    expect(within(grupo).getByText('Apagar 1.240 ditados?').tagName).toBe('STRONG')
    // A caixa fica dentro do ajuste, depois do texto: é onde a regra
    // ".ajuste > * + .confirmar" (estilo.test.ts) lhe dá o respiro.
    expect(grupo.classList.contains('confirmar')).toBe(true)
    expect(grupo.parentElement!.classList.contains('ajuste')).toBe(true)
    expect(grupo.previousElementSibling).not.toBeNull()
    expect(botao('Apagar').className).toContain('risco')
    await waitFor(() => expect(document.activeElement).toBe(botao('Cancelar')))
    expect(within(secao()).queryByRole('button', { name: 'Apagar os mais antigos' })).toBeNull()

    fireEvent.keyDown(botao('Cancelar'), { key: 'Escape' })
    await waitFor(() => expect(within(secao()).queryByRole('group')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(botao('Apagar os mais antigos')))
    // Nada foi pedido ao Agent.
    expect(pedidosA(srv, '/api/retencao/ligar')).toHaveLength(0)
  })

  it('Cancelar também volta, sem pedir nada', async () => {
    const srv = await abrir({ retencao: estado({ previa: 7 }) })
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Cancelar' }))
    await waitFor(() => expect(botao('Apagar os mais antigos')).toBeTruthy())
    await waitFor(() => expect(document.activeElement).toBe(botao('Apagar os mais antigos')))
    expect(pedidosA(srv, '/api/retencao/ligar')).toHaveLength(0)
    expect(srv.retencaoAgora().ligada).toBe(false)
  })

  it('com a cópia de um OK anterior ao lado do banco, a pergunta não promete cópia nova: diz que a de antes não tem o que nasceu depois dela', async () => {
    const srv = await abrir({ retencao: estado({ previa: 12, copias: 1, copia_dos_6_meses: true }) })
    const el = secao()
    expect(
      within(el).getByText(
        '12 ditados têm mais de 6 meses. Ao confirmar, eles saem deste PC de vez, e daqui em diante o que fizer 6 meses sai sozinho. Não se faz outra cópia: a que já está neste PC é de antes, e o que nasceu depois dela não está nela.',
      ),
    ).toBeTruthy()
    expect(el.textContent).not.toContain('Antes, uma cópia do histórico fica neste PC.')
    fireEvent.click(botao('Apagar os mais antigos'))
    const grupo = await within(secao()).findByRole('group')
    expect(grupo.textContent).toBe('Apagar 12 ditados? Não dá para desfazer.ApagarCancelar')
    expect(grupo.textContent).not.toContain('cópia')
    // O OK não copia: o botão diz só "Apagando…", e nenhuma cópia nova aparece.
    const solta = srv.pausar((p) => p.caminho === '/api/retencao/ligar')
    fireEvent.click(botao('Apagar'))
    expect(await within(secao()).findByRole('button', { name: 'Apagando…' })).toBeTruthy()
    solta()
    await waitFor(() => expect(secao().textContent).toContain('Os 6 meses estão ligados'))
    expect(srv.retencaoAgora().copias).toBe(1)
  })

  it('a pergunta no singular: Apagar 1 ditado?', async () => {
    await abrir({ retencao: estado({ previa: 1 }) })
    fireEvent.click(botao('Apagar os mais antigos'))
    expect(await within(secao()).findByText('Apagar 1 ditado?')).toBeTruthy()
  })
})

describe('o bloco Histórico do PC: o OK', () => {
  it('Apagar manda a contagem que a tela mostrou, avisa, recarrega o histórico e liga a regra', async () => {
    const srv = await abrir({ retencao: estado({ previa: 1240, na_lixeira: 3, copias: 1 }) })
    const listas = pedidosA(srv, '/api/history', 'GET').length
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))

    // Só quantos saíram: a cópia já está na linha das cópias do bloco.
    await waitFor(() => expect(aviso()).toContain('1.240 ditados saíram deste PC.'))
    expect(aviso()).not.toContain('cópia')
    const ligar = pedidosA(srv, '/api/retencao/ligar')
    expect(ligar).toHaveLength(1)
    expect(ligar[0]!.metodo).toBe('POST')
    expect(ligar[0]!.corpo).toEqual({ previa: 1240 })
    expect(ligar[0]!.auth).toBe('Bearer tok')

    // O bloco agora diz que está ligado, com o Desligar e a ajuda; a pergunta sumiu.
    const el = secao()
    expect(el.textContent).toContain('Os 6 meses estão ligados: o que fizer 6 meses sai sozinho deste PC, em até 1 hora.')
    expect(el.textContent).toContain('Desligar volta ao de antes: o PC guarda tudo. O que já saiu não volta.')
    expect(el.textContent).not.toContain('têm mais de 6 meses')
    expect(within(el).queryByRole('button', { name: 'Apagar os mais antigos' })).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(botao('Desligar')))
    // A lista do Histórico lê de novo.
    await waitFor(() => expect(pedidosA(srv, '/api/history', 'GET').length).toBeGreaterThan(listas))
  })

  it('o OK que apagou só parte (um lote seguinte caiu): o aviso diz quantos saíram e que o resto sai sozinho, recarrega o histórico e o foco vai ao Desligar', async () => {
    const srv = await abrir({ retencao: estado({ previa: 1240 }) })
    srv.ligarEmParte({ apagados: 500, resposta: 'incompleto' })
    const listas = pedidosA(srv, '/api/history', 'GET').length
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))

    await waitFor(() => expect(aviso()).toContain('500 ditados saíram deste PC. Os outros 740 saem na próxima rodada, em até 1 hora.'))
    expect(secao().textContent).toContain('Os 6 meses estão ligados')
    expect(alertas()).toEqual([])
    await waitFor(() => expect(document.activeElement).toBe(botao('Desligar')))
    await waitFor(() => expect(pedidosA(srv, '/api/history', 'GET').length).toBeGreaterThan(listas))
  })

  it('a resposta do OK que falha com a regra já ligada: sem "tente de novo"; o aviso diz que o que ficou sai sozinho, recarrega o histórico e o foco vai ao Desligar', async () => {
    const srv = await abrir({ retencao: estado({ previa: 1240 }) })
    srv.ligarEmParte({ apagados: 500, resposta: 'falha' })
    const listas = pedidosA(srv, '/api/history', 'GET').length
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))

    await waitFor(() => expect(aviso()).toContain('Os 6 meses estão ligados. O que ainda não saiu sai na próxima rodada, em até 1 hora.'))
    expect(secao().textContent).toContain('Os 6 meses estão ligados')
    expect(alertas()).toEqual([])
    expect(document.body.textContent).not.toContain('Tente de novo')
    await waitFor(() => expect(document.activeElement).toBe(botao('Desligar')))
    await waitFor(() => expect(pedidosA(srv, '/api/history', 'GET').length).toBeGreaterThan(listas))
  })

  it('Apagar e Cancelar ficam desligados enquanto o Agent copia e apaga', async () => {
    const srv = await abrir({ retencao: estado({ previa: 9 }) })
    const solta = srv.pausar((p) => p.caminho === '/api/retencao/ligar')
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))
    await within(secao()).findByRole('button', { name: 'Copiando e apagando…' })
    expect(desligado(botao('Copiando e apagando…'))).toBe(true)
    expect(desligado(botao('Cancelar'))).toBe(true)
    // Um segundo clique, ou o Esc, não pede de novo nem cancela.
    fireEvent.click(botao('Copiando e apagando…'))
    fireEvent.keyDown(botao('Cancelar'), { key: 'Escape' })
    expect(within(secao()).queryByRole('group')).not.toBeNull()
    solta()
    await waitFor(() => expect(secao().textContent).toContain('Os 6 meses estão ligados'))
    expect(pedidosA(srv, '/api/retencao/ligar')).toHaveLength(1)
  })

  it('a contagem mudou: a frase troca para o número novo, o aviso diz, nada foi apagado, e a pessoa confirma de novo', async () => {
    const srv = await abrir({ retencao: estado({ previa: 1240 }) })
    fireEvent.click(botao('Apagar os mais antigos'))
    await within(secao()).findByRole('group')
    srv.retencaoPorFora({ previa: 1301 })
    fireEvent.click(botao('Apagar'))

    await within(secao()).findByText(/^1\.301 ditados têm mais de 6 meses\./)
    await waitFor(() => expect(aviso()).toContain('A contagem mudou.'))
    expect(srv.retencaoAgora().ligada).toBe(false)
    // De volta à pergunta, não à confirmação; o foco no botão.
    expect(within(secao()).queryByRole('group')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(botao('Apagar os mais antigos')))
    expect(alertas()).toEqual([])

    // Confirmar de novo vale, com o número novo.
    fireEvent.click(botao('Apagar os mais antigos'))
    expect((await within(secao()).findByRole('group')).textContent).toContain('Apagar 1.301 ditados?')
    fireEvent.click(botao('Apagar'))
    await waitFor(() => expect(secao().textContent).toContain('Os 6 meses estão ligados'))
    const ligar = pedidosA(srv, '/api/retencao/ligar')
    expect(ligar.map((p) => p.corpo)).toEqual([{ previa: 1240 }, { previa: 1301 }])
    expect(aviso()).toContain('1.301 ditados saíram deste PC.')
  })

  it('não há mais nada a apagar: o aviso diz, o bloco some e o foco vai ao Salvar', async () => {
    const srv = await abrir({ retencao: estado({ previa: 4, na_lixeira: 4 }) })
    fireEvent.click(botao('Apagar os mais antigos'))
    await within(secao()).findByRole('group')
    // A lixeira foi expurgada entre a pergunta e o clique.
    srv.retencaoPorFora({ previa: 0, na_lixeira: 0 })
    fireEvent.click(botao('Apagar'))

    await waitFor(() => expect(semSecao()).toBeNull())
    await waitFor(() => expect(aviso()).toContain('Não há nada a apagar agora.'))
    expect(srv.retencaoAgora().ligada).toBe(false)
    await waitFor(() => expect(document.activeElement).toBe(botaoSalvar()))
  })

  it('a cópia que falha: a frase do Agent numa nota, nada foi apagado, e dá para tentar de novo', async () => {
    const frase =
      'Não deu para copiar o histórico. Nada foi apagado. Já há 3 cópias de antes dos 6 meses ao lado do histórico: apague as que não quiser.'
    const srv = await abrir({ retencao: estado({ previa: 8, copias: 3 }) })
    srv.recusarRetencao({ status: 409, mensagem: frase })
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))

    await waitFor(() => expect(alertas()).toEqual([frase]))
    // O bloco continua pronto: a pergunta, e o botão que resolve (as cópias).
    expect(secao().textContent).toContain('8 ditados têm mais de 6 meses.')
    expect(botao('Apagar as cópias')).toBeTruthy()
    await waitFor(() => expect(document.activeElement).toBe(botao('Apagar os mais antigos')))
    expect(srv.retencaoAgora().ligada).toBe(false)

    // Resolvida a causa, o OK de novo passa e a nota sai.
    srv.recusarRetencao(null)
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))
    await waitFor(() => expect(secao().textContent).toContain('Os 6 meses estão ligados'))
    expect(alertas()).toEqual([])
  })

  it('o disco cheio e o erro sem motivo: a frase do Agent, e o estado é lido de novo', async () => {
    const srv = await abrir({ retencao: estado({ previa: 8 }) })
    srv.recusarRetencao({ status: 507, mensagem: 'Não deu para copiar o histórico. Nada foi apagado. Falta espaço no disco.' })
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))
    await waitFor(() => expect(alertas()).toEqual(['Não deu para copiar o histórico. Nada foi apagado. Falta espaço no disco.']))
    // A releitura depois da falha: o GET foi feito de novo.
    expect(pedidosA(srv, '/api/retencao', 'GET').length).toBeGreaterThanOrEqual(2)
  })

  it('o Agent fora do ar no meio do OK acende a faixa, e o bloco fica como está', async () => {
    const srv = await abrir({ retencao: estado({ previa: 8 }) })
    fireEvent.click(botao('Apagar os mais antigos'))
    await within(secao()).findByRole('group')
    srv.derrubar(true)
    fireEvent.click(botao('Apagar'))
    await waitFor(() => expect(document.querySelector('.faixa')).not.toBeNull())
    expect(alertas()).toEqual([])
    expect(srv.retencaoAgora().ligada).toBe(false)
  })

  it('uma leitura que chega atrasada não desfaz o que o OK acabou de mostrar', async () => {
    let atrasar = false
    let soltar!: () => void
    const espera = new Promise<void>((r) => {
      soltar = r
    })
    const srv = await abrir({ retencao: estado({ previa: 20 }) }, (b) => async (entrada, init) => {
      // A resposta é calculada agora (com o estado de antes do OK) e entregue
      // tarde.
      const resposta = await b(entrada, init)
      if (atrasar && entrada === '/api/retencao' && (init?.method ?? 'GET') === 'GET') {
        atrasar = false
        await espera
      }
      return resposta
    })
    atrasar = true
    voltarAAba() // a leitura que vai chegar tarde, com a regra ainda desligada
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))
    await waitFor(() => expect(secao().textContent).toContain('Os 6 meses estão ligados'))
    expect(srv.retencaoAgora().ligada).toBe(true)

    await act(async () => {
      soltar()
      await Promise.resolve()
    })
    expect(secao().textContent).toContain('Os 6 meses estão ligados')
    expect(secao().textContent).not.toContain('têm mais de 6 meses')
  })
})

describe('o bloco Histórico do PC: ligado, desligado e pausado', () => {
  it('Desligar vale na hora, sem pergunta; a pergunta volta enquanto houver o que apagar', async () => {
    const srv = await abrir({ retencao: estado({ ligada: true, previa: 2 }) })
    // Ligada, a prévia não pergunta: o Agent apaga sozinho.
    expect(secao().textContent).not.toContain('têm mais de 6 meses')
    fireEvent.click(botao('Desligar'))

    await waitFor(() => expect(pedidosA(srv, '/api/retencao/desligar')).toHaveLength(1))
    expect(pedidosA(srv, '/api/retencao/desligar')[0]!.metodo).toBe('POST')
    await within(secao()).findByText(/^2 ditados têm mais de 6 meses\./)
    expect(srv.retencaoAgora().ligada).toBe(false)
    expect(within(secao()).queryByRole('group')).toBeNull()
    await waitFor(() => expect(aviso()).toContain('Os 6 meses foram desligados.'))
    await waitFor(() => expect(document.activeElement).toBe(botao('Apagar os mais antigos')))
  })

  it('Desligar sem nada a apagar e sem cópias: o bloco some, e o foco vai ao Salvar', async () => {
    await abrir({ retencao: estado({ ligada: true }) })
    fireEvent.click(botao('Desligar'))
    await waitFor(() => expect(semSecao()).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(botaoSalvar()))
  })

  it('pausada pelo freio: a pergunta volta, com a frase que diz o que houve ANTES dela (e não entre ela e as linhas de baixo)', async () => {
    await abrir({ retencao: estado({ previa: 12, na_lixeira: 2, pausada: true }) })
    const el = secao()
    expect(within(el).getByText('12 ditados têm mais de 6 meses.', { exact: false })).toBeTruthy()
    expect(within(el).getByText('A regra foi pausada: o Agent ficou parado por mais de 7 dias, ou o relógio do PC mudou. Nada foi apagado.')).toBeTruthy()
    const t = el.textContent!
    expect(t.indexOf('A regra foi pausada')).toBeLessThan(t.indexOf('12 ditados têm'))
    // "2 deles" logo depois da pergunta, sem a frase da pausa no meio.
    expect(t).toContain('Antes, uma cópia do histórico fica neste PC.2 deles estão na lixeira.')
    expect(botao('Apagar os mais antigos')).toBeTruthy()
    expect(el.textContent).not.toContain('estão ligados')
  })

  it('pausada sem nada a apagar, mas com a cópia ao lado do banco: o bloco aparece pelas cópias e diz que a regra foi pausada', async () => {
    await abrir({ retencao: estado({ previa: 0, pausada: true, copias: 1, copia_dos_6_meses: true }) })
    const el = secao()
    expect(
      within(el).getByText(
        'A regra foi pausada: o Agent ficou parado por mais de 7 dias, ou o relógio do PC mudou. Nada foi apagado. A pergunta volta quando houver o que apagar.',
      ),
    ).toBeTruthy()
    expect(el.textContent).toContain('Há 1 cópia do histórico ao lado do banco')
    expect(el.textContent).not.toContain('estão ligados')
  })

  it('sem a pausa, a frase do freio não aparece', async () => {
    await abrir({ retencao: estado({ previa: 12 }) })
    expect(secao().textContent).not.toContain('A regra foi pausada')
  })
})

describe('o bloco Histórico do PC: o Agent', () => {
  it('um Agent de antes da F10 (404): diz o que fazer, e o resto das Configurações fica', async () => {
    const srv = servidorFalso([], {})
    srv.agentSemRetencao()
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await waitFor(() => expect(alertas()).toEqual(['Este Agent ainda não tem os 6 meses. Reinicie o Agent.']))
    expect(within(secao()).queryByRole('button')).toBeNull()
    expect(screen.getByRole('heading', { name: 'Privacidade' })).toBeTruthy()
  })

  it('sem a retenção montada (503): a frase do Agent', async () => {
    const srv = servidorFalso([], {})
    srv.interceptar((p) =>
      p.caminho === '/api/retencao'
        ? new Response(JSON.stringify({ error: { code: 'UNAVAILABLE', message: 'O histórico deste PC ainda não está pronto para isso. Reinicie o Agent.' } }), {
            status: 503,
            headers: { 'Content-Type': 'application/json' },
          })
        : undefined,
    )
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await waitFor(() => expect(alertas()).toEqual(['O histórico deste PC ainda não está pronto para isso. Reinicie o Agent.']))
  })

  it('o Agent fora do ar ao abrir acende a faixa, sem nota no bloco', async () => {
    const srv = servidorFalso([], { retencao: estado({ previa: 3 }) })
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Nuvem' })
    srv.derrubar(true)
    voltarAAba()
    await waitFor(() => expect(document.querySelector('.faixa')).not.toBeNull())
  })
})

describe('o bloco Histórico do PC: as cópias do banco', () => {
  it('a pergunta na linha: quantas cópias há, o foco no Cancelar, o Esc cancela', async () => {
    const srv = await abrir({ retencao: estado({ copias: 3 }) })
    expect(within(secao()).getByText('Há 3 cópias do histórico ao lado do banco, com o texto de antes.')).toBeTruthy()
    fireEvent.click(botao('Apagar as cópias'))
    const grupo = await within(secao()).findByRole('group')
    expect(grupo.textContent).toBe('Apagar as 3 cópias? Elas guardam o texto que os 6 meses apagam. Não dá para desfazer.ApagarCancelar')
    await waitFor(() => expect(document.activeElement).toBe(botao('Cancelar')))

    fireEvent.keyDown(botao('Cancelar'), { key: 'Escape' })
    await waitFor(() => expect(within(secao()).queryByRole('group')).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(botao('Apagar as cópias')))
    expect(pedidosA(srv, '/api/retencao/copias/apagar')).toHaveLength(0)
  })

  it('o singular: Há 1 cópia; Apagar a cópia?', async () => {
    await abrir({ retencao: estado({ copias: 1 }) })
    expect(within(secao()).getByText('Há 1 cópia do histórico ao lado do banco, com o texto de antes.')).toBeTruthy()
    fireEvent.click(botao('Apagar as cópias'))
    expect((await within(secao()).findByRole('group')).textContent).toContain('Apagar a cópia? Ela guarda o texto que os 6 meses apagam. Não dá para desfazer.')
  })

  it('Apagar: pede a rota, avisa quantas saíram, e o bloco some (nada mais a decidir); o foco vai ao Salvar', async () => {
    const srv = await abrir({ retencao: estado({ copias: 3 }) })
    fireEvent.click(botao('Apagar as cópias'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))

    await waitFor(() => expect(aviso()).toContain('3 cópias apagadas.'))
    const pedidos = pedidosA(srv, '/api/retencao/copias/apagar')
    expect(pedidos).toHaveLength(1)
    expect(pedidos[0]!.metodo).toBe('POST')
    await waitFor(() => expect(semSecao()).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(botaoSalvar()))
    expect(srv.retencaoAgora().copias).toBe(0)
  })

  it('a cópia do envio que ainda não terminou: o bloco diz que ela fica, a pergunta conta só as que saem, e com só ela o botão some', async () => {
    const srv = await abrir({ retencao: estado({ copias: 3 }), copiasProtegidas: 1 })
    expect(within(secao()).getByText('1 delas é a do envio do histórico à nuvem, que fica até o envio terminar.')).toBeTruthy()
    fireEvent.click(botao('Apagar as cópias'))
    const grupo = await within(secao()).findByRole('group')
    expect(grupo.textContent).toBe('Apagar as 2 cópias? Elas guardam o texto que os 6 meses apagam. Não dá para desfazer.ApagarCancelar')
    fireEvent.click(botao('Apagar'))

    await waitFor(() => expect(aviso()).toContain('2 cópias apagadas. 1 fica: o envio do histórico à nuvem ainda não terminou.'))
    // O bloco continua, com a que ficou e o porquê; sem o botão, que não teria o
    // que apagar. O foco, que estava no Apagar, vai ao Salvar.
    expect(within(secao()).getByText('Há 1 cópia do histórico ao lado do banco, com o texto de antes.')).toBeTruthy()
    expect(within(secao()).getByText('Ela é a do envio do histórico à nuvem, que fica até o envio terminar.')).toBeTruthy()
    expect(within(secao()).queryByRole('button', { name: 'Apagar as cópias' })).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(botaoSalvar()))
    expect(srv.retencaoAgora().copias).toBe(1)
  })

  it('só a cópia do envio que não terminou: sem o botão desde o começo', async () => {
    await abrir({ retencao: estado({ copias: 1 }), copiasProtegidas: 1 })
    expect(secao().textContent).toContain('Ela é a do envio do histórico à nuvem, que fica até o envio terminar.')
    expect(within(secao()).queryByRole('button')).toBeNull()
  })

  it('a pergunta dos 6 meses e a das cópias não se misturam: uma de cada vez, e as duas lado a lado', async () => {
    await abrir({ retencao: estado({ previa: 5, copias: 2 }) })
    expect(botao('Apagar os mais antigos')).toBeTruthy()
    expect(botao('Apagar as cópias')).toBeTruthy()
    fireEvent.click(botao('Apagar as cópias'))
    const grupo = await within(secao()).findByRole('group')
    expect(grupo.textContent).toContain('Apagar as 2 cópias?')
    // A pergunta dos 6 meses segue como estava, com o botão dela.
    expect(botao('Apagar os mais antigos')).toBeTruthy()
  })

  it('o Agent recusa apagar as cópias: a frase numa nota, e o bloco fica', async () => {
    const srv = await abrir({ retencao: estado({ copias: 2 }) })
    srv.interceptar((p) =>
      p.caminho === '/api/retencao/copias/apagar'
        ? new Response(JSON.stringify({ error: { code: 'INTERNAL', message: 'Não deu para apagar as cópias agora.' } }), {
            status: 500,
            headers: { 'Content-Type': 'application/json' },
          })
        : undefined,
    )
    fireEvent.click(botao('Apagar as cópias'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))
    await waitFor(() => expect(alertas()).toEqual(['Não deu para apagar as cópias agora.']))
    expect(botao('Apagar as cópias')).toBeTruthy()
  })
})

describe('o bloco Histórico do PC: a Privacidade', () => {
  it('com os 6 meses ligados, a Privacidade diz que o que completa 6 meses é apagado deste computador', async () => {
    await abrir({ retencao: estado({ ligada: true }) })
    const privacidade = screen.getByRole('region', { name: 'Privacidade' })
    expect(within(privacidade).getByText('O que completa 6 meses é apagado deste computador.')).toBeTruthy()
  })

  it('desligados, a linha não aparece; ligar e desligar a põem e a tiram', async () => {
    await abrir({ retencao: estado({ previa: 3 }) })
    const privacidade = () => screen.getByRole('region', { name: 'Privacidade' })
    expect(within(privacidade()).queryByText(/completa 6 meses/)).toBeNull()
    fireEvent.click(botao('Apagar os mais antigos'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Apagar' }))
    await within(privacidade()).findByText('O que completa 6 meses é apagado deste computador.')
    fireEvent.click(botao('Desligar'))
    await waitFor(() => expect(within(privacidade()).queryByText(/completa 6 meses/)).toBeNull())
  })
})

describe('as frases das cópias que saíram', () => {
  it('cada combinação de apagadas e ficaram', () => {
    expect(copiasApagadas(3, 0)).toBe('3 cópias apagadas.')
    expect(copiasApagadas(1, 0)).toBe('1 cópia apagada.')
    expect(copiasApagadas(2, 1)).toBe('2 cópias apagadas. 1 fica: o envio do histórico à nuvem ainda não terminou.')
    expect(copiasApagadas(0, 1)).toBe('Nenhuma cópia saiu. 1 fica: o envio do histórico à nuvem ainda não terminou.')
    expect(copiasApagadas(1, 2)).toBe('1 cópia apagada. 2 ficam: o envio do histórico à nuvem ainda não terminou.')
    expect(copiasApagadas(0, 0)).toBe('Não havia cópias para apagar.')
    expect(copiasApagadas(1200, 0)).toBe('1.200 cópias apagadas.')
  })
})
