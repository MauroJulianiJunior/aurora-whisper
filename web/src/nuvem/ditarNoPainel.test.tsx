// O ditado do celular dentro do app inteiro (ADR-021, C6a): o microfone ao lado
// da busca, o ditado que chega no topo do histórico, em destaque e com o
// Copiar, a falha que guarda o áudio, e a sessão que acaba no meio do envio.
// Contra a nuvem de mentira (testes/servidorFalsoNuvem.ts) e a Plataforma de
// mentira (testes/plataformaFalsa.ts): nenhum teste abre o microfone, a rede
// nem a área de transferência de verdade.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type Buscador } from '../api'
import { type OpcoesDaPlataforma, plataformaFalsa } from './ditar/plataformaFalsa'
import { type OpcoesDaNuvem, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

function base() {
  return [
    itemNuvem({ id: 'd1', created_at: em(5, 14, 32), raw_text: 'Manda o relatório amanhã.', final_text: 'Manda o relatório amanhã.' }),
    itemNuvem({ id: 'd2', created_at: em(5, 9, 5), raw_text: 'A reunião é às dez.', final_text: 'A reunião é às dez.' }),
    itemNuvem({ id: 'l1', created_at: em(3, 10), final_text: 'Lembrete velho.', deleted_at: em(5, 11) }),
  ]
}

function montar(opcoes: OpcoesDaNuvem = {}, plataforma: OpcoesDaPlataforma = {}, comBuscar?: (b: Buscador) => Buscador, janela = true) {
  const srv = servidorFalsoNuvem(base(), opcoes)
  const p = plataformaFalsa(plataforma)
  // janela: o navegador deixa abrir a janela de entrar (false: a bloqueou).
  const abrirJanela = vi.fn(() => janela)
  render(<AppNuvem buscar={comBuscar ? comBuscar(srv.buscar) : srv.buscar} irPara={vi.fn()} abrirJanela={abrirJanela} plataformaDeDitar={p.plataforma} />)
  return { srv, p, abrirJanela }
}

const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const linhas = () => Array.from(document.querySelectorAll<HTMLElement>('[data-item]'))
const posts = (srv: ReturnType<typeof servidorFalsoNuvem>) => srv.pedidos.filter((x) => x.metodo === 'POST' && x.caminho === '/api/v1/dictations')

// A primeira renderização de um arquivo, com a máquina ocupada (os outros
// testes do repositório rodam junto), passa do 1 s padrão do findBy.
const espera = { timeout: 5000 }

async function abrirOHistorico() {
  await screen.findByRole('button', { name: /Manda o relatório amanhã/ }, espera)
}

async function comecar() {
  await act(async () => {
    fireEvent.click(await screen.findByRole('button', { name: 'Ditar' }, espera))
  })
}

async function pararEEnviar(p: ReturnType<typeof plataformaFalsa>, ms = 2000) {
  act(() => p.rodar(ms, 0.2))
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
  })
}

async function ditar(p: ReturnType<typeof plataformaFalsa>, ms = 2000) {
  await comecar()
  await pararEEnviar(p, ms)
}

// Com a máquina ocupada (os outros testes do repositório rodam junto), o app
// inteiro numa renderização passa dos 5 s padrão.
vi.setConfig({ testTimeout: 20_000 })

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 5, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  Reflect.deleteProperty(navigator, 'clipboard')
})

describe('o ditado que chega', () => {
  it('entra no topo do histórico, em destaque, com a etiqueta "novo" e o Copiar da linha', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.aoDitar({ texto: 'Ligar para o cliente às três.' })
    await ditar(p)

    await screen.findByText('Ligar para o cliente às três.')
    const [primeira, segunda] = linhas()
    expect(primeira!.textContent).toContain('Ligar para o cliente às três.')
    expect(primeira!.className).toContain('chegou')
    expect(within(primeira!).getByText('novo').className).toBe('etiqueta')
    expect(segunda!.dataset.item).toBe('d1')
    expect(segunda!.className).not.toContain('chegou')
    expect(aviso().textContent).toBe('Ditado no topo do histórico.')
    // O Copiar que toda linha tem.
    expect(within(primeira!).getByRole('button', { name: /^Copiar o ditado das/ })).toBeTruthy()
    // O ditado de verdade do servidor, não um texto do cliente: o id é o que o app mandou.
    expect(primeira!.dataset.item).toBe(posts(srv)[0]!.cabecalhos['Ditado-Id'])
  })

  it('o Copiar leva o texto do ditado (a área de transferência é um dublê)', async () => {
    const escrever = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: escrever }, configurable: true })
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.aoDitar({ texto: 'Ligar para o cliente às três.' })
    await ditar(p)
    await screen.findByText('Ligar para o cliente às três.')
    fireEvent.click(within(linhas()[0]!).getByRole('button', { name: /^Copiar o ditado das/ }))
    await waitFor(() => expect(aviso().textContent).toBe('Copiado.'))
    expect(escrever).toHaveBeenCalledWith('Ligar para o cliente às três.')
  })

  it('o envio leva o áudio cru, o id v7 e o que o app mediu; o corpo não é JSON nem multipart', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    await ditar(p, 3000)
    await screen.findByText('Texto ditado pelo celular.')
    const [envio] = posts(srv)
    expect(envio!.cabecalhos['Content-Type']).toBe('audio/webm')
    expect(envio!.cabecalhos['Ditado-Id']).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(Number(envio!.cabecalhos['Ditado-Duracao-Ms'])).toBeGreaterThanOrEqual(2980)
    expect(Number(envio!.cabecalhos['Ditado-Fala-Ms'])).toBeGreaterThanOrEqual(2700)
    expect(envio!.audio?.size).toBe(4096)
    expect(envio!.corpo).toBeUndefined()
    expect(envio!.busca.toString()).toBe('')
  })

  it('o destaque sai ao abrir a linha', async () => {
    const { p } = montar()
    await abrirOHistorico()
    await ditar(p)
    await screen.findByText('Texto ditado pelo celular.')
    expect(linhas()[0]!.className).toContain('chegou')
    fireEvent.click(within(linhas()[0]!).getByRole('button', { name: /Texto ditado pelo celular/ }))
    expect(linhas()[0]!.className).not.toContain('chegou')
  })

  it('o destaque dura 10 s', async () => {
    // O waitFor do Testing Library não anda com setTimeout falso: o temporizador
    // de 10 s é pego na hora em que é agendado, e disparado à mão.
    const agendar = vi.spyOn(window, 'setTimeout')
    const { p } = montar()
    await abrirOHistorico()
    await ditar(p)
    await screen.findByText('Texto ditado pelo celular.')
    expect(linhas()[0]!.className).toContain('chegou')
    const dezSegundos = agendar.mock.calls.filter(([, ms]) => ms === 10_000)
    expect(dezSegundos).toHaveLength(1)
    act(() => (dezSegundos[0]![0] as () => void)())
    expect(linhas()[0]!.className).not.toContain('chegou')
    agendar.mockRestore()
  })

  it('quando o Polish caiu para o Smart, o aviso diz o motivo', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.aoDitar({ texto: 'Ligar para o cliente.', aviso: 'chave_recusada' })
    await ditar(p)
    await screen.findByText('Ligar para o cliente.')
    expect(aviso().textContent).toBe('Ditado no topo do histórico. O Polish não agiu: o provedor recusou a chave.')
  })

  it('quando o app parou sozinho, o aviso diz isso', async () => {
    const { p } = montar()
    await abrirOHistorico()
    await comecar()
    act(() => p.rodar(2000, 0.2))
    await act(async () => p.sairDaFrente())
    await screen.findByText('Texto ditado pelo celular.')
    expect(aviso().textContent).toBe('Ditado no topo do histórico. Parei quando o app saiu da frente.')
  })

  it('durante a gravação a busca dá lugar à faixa; ao chegar, a busca volta com o que estava escrito', async () => {
    const { p } = montar()
    await abrirOHistorico()
    const campo = screen.getByRole('searchbox', { name: 'Buscar no histórico' })
    fireEvent.change(campo, { target: { value: 'relatório' } })
    await waitFor(() => expect(linhas()).toHaveLength(1))
    await comecar()
    expect(screen.queryByRole('searchbox')).toBeNull()
    expect(document.querySelector('.ditar-faixa')).not.toBeNull()
    await pararEEnviar(p)
    await waitFor(() => expect(screen.getByRole('searchbox', { name: 'Buscar no histórico' })).toBeTruthy())
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('relatório')
  })
})

describe('onde o ditado não aparece na lista da tela', () => {
  it('numa busca: o aviso diz que foi salvo, e a lista não muda', async () => {
    const { p } = montar()
    await abrirOHistorico()
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'relatório' } })
    await waitFor(() => expect(linhas()).toHaveLength(1))
    await ditar(p)
    await waitFor(() => expect(aviso().textContent).toBe('Ditado salvo no histórico.'))
    expect(linhas().map((l) => l.dataset.item)).toEqual(['d1'])
  })

  it('na Lixeira: o aviso diz que foi salvo no histórico; ao voltar, ele está lá, sem destaque', async () => {
    const { p } = montar()
    await abrirOHistorico()
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByText('Lembrete velho.')
    await ditar(p)
    await waitFor(() => expect(aviso().textContent).toBe('Ditado salvo no histórico.'))
    expect(linhas().map((l) => l.dataset.item)).toEqual(['l1'])
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await screen.findByText('Texto ditado pelo celular.')
    expect(linhas()[0]!.className).not.toContain('chegou')
  })

  it('o microfone some nas Configurações, e volta com o Histórico', async () => {
    montar()
    await abrirOHistorico()
    expect(screen.getByRole('button', { name: 'Ditar' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { level: 1, name: 'Configurações' })
    expect(screen.queryByRole('button', { name: 'Ditar' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    expect(screen.getByRole('button', { name: 'Ditar' })).toBeTruthy()
  })

  it('gravando, ir às Configurações não desliga o microfone, e o parar continua à mão', async () => {
    const { p } = montar()
    await abrirOHistorico()
    await comecar()
    act(() => p.rodar(1500, 0.2))
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { level: 1, name: 'Configurações' })
    expect(screen.getByRole('button', { name: 'Parar e enviar' })).toBeTruthy()
    expect(p.mic.descartado).toBe(0)
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
    })
    await waitFor(() => expect(aviso().textContent).toBe('Ditado salvo no histórico.'))
  })
})

describe('as falhas', () => {
  it('"Não ouvi nada." não envia nada e fala pela pílula', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    await comecar()
    act(() => p.rodar(2000, 0.001))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
    })
    expect(aviso().textContent).toBe('Não ouvi nada.')
    expect(posts(srv)).toHaveLength(0)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('sem conexão: o áudio fica; "Tentar de novo" reenvia com o MESMO id e dá certo', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.derrubar(true)
    await ditar(p)
    const nota = await screen.findByRole('alert')
    expect(nota.textContent).toContain('Sem conexão.')
    srv.derrubar(false)
    await act(async () => {
      fireEvent.click(within(nota).getByRole('button', { name: 'Tentar de novo' }))
    })
    await screen.findByText('Texto ditado pelo celular.')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(posts(srv)).toHaveLength(1)
    expect(p.estado.ids).toHaveLength(1)
  })

  it('a resposta que se perdeu: o servidor já gravou, o reenvio não chama o provedor de novo e o ditado não duplica', async () => {
    let perdeu = false
    const { srv, p } = montar({}, {}, (buscar) => async (entrada, init) => {
      const r = await buscar(entrada, init)
      if (entrada === '/api/v1/dictations' && !perdeu) {
        perdeu = true
        throw new TypeError('Failed to fetch')
      }
      return r
    })
    await abrirOHistorico()
    await ditar(p)
    const nota = await screen.findByRole('alert')
    expect(nota.textContent).toContain('Sem conexão.')
    // O servidor recebeu e gravou; o app não soube.
    expect(srv.chamadasAoProvedor).toBe(1)
    expect(srv.itens.filter((i) => i.source_platform === 'web')).toHaveLength(1)
    await act(async () => {
      fireEvent.click(within(nota).getByRole('button', { name: 'Tentar de novo' }))
    })
    await screen.findByText('Texto ditado pelo celular.')
    expect(posts(srv).map((x) => x.cabecalhos['Ditado-Id'])).toHaveLength(2)
    expect(new Set(posts(srv).map((x) => x.cabecalhos['Ditado-Id'])).size).toBe(1)
    expect(srv.chamadasAoProvedor).toBe(1)
    expect(srv.ditados.map((d) => d.chamouOProvedor)).toEqual([true, false])
    expect(linhas().filter((l) => l.textContent?.includes('Texto ditado pelo celular.'))).toHaveLength(1)
  })

  it('Descartar solta o áudio e a nota sai', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.derrubar(true)
    await ditar(p)
    const nota = await screen.findByRole('alert')
    fireEvent.click(within(nota).getByRole('button', { name: 'Descartar' }))
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByRole('button', { name: 'Ditar' }).hasAttribute('aria-disabled')).toBe(false)
  })

  it('SEM_CHAVE leva às Configurações; o áudio espera', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.interceptar((x) =>
      x.caminho === '/api/v1/dictations' ? new Response(JSON.stringify({ error: { code: 'SEM_CHAVE', message: 'sem chave' } }), { status: 403 }) : undefined,
    )
    await ditar(p)
    const nota = await screen.findByRole('alert')
    fireEvent.click(within(nota).getByRole('button', { name: 'Abrir as Configurações' }))
    await screen.findByRole('heading', { level: 1, name: 'Configurações' })
    // O áudio continua na memória, com a nota à vista nas Configurações.
    expect(screen.getByRole('button', { name: 'Tentar de novo' })).toBeTruthy()
  })
})

describe('a sessão que acaba no meio do envio (401)', () => {
  it('o Painel fica (o áudio é como o texto não salvo), a faixa fala do ditado, e depois de entrar de novo o reenvio tem o mesmo id', async () => {
    const { srv, p, abrirJanela } = montar()
    await abrirOHistorico()
    await comecar()
    act(() => p.rodar(2000, 0.2))
    srv.expirar()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
    })

    // Não trocou a tela inteira por "Sua sessão terminou.": o Painel está lá.
    const faixa = await screen.findByText('Seu ditado continua aqui; entre de novo para enviar.')
    expect(faixa).toBeTruthy()
    expect(screen.queryByRole('heading', { level: 1, name: 'Sua sessão terminou.' })).toBeNull()
    expect(screen.getByRole('heading', { level: 1, name: 'Histórico' })).toBeTruthy()
    const nota = document.querySelector<HTMLElement>('.ditar-erro')!
    expect(nota.textContent).toContain('O ditado espera você entrar de novo.')
    expect(posts(srv)).toHaveLength(1)

    // A página não sai sem perguntar: o ditado só existe nela.
    const saida = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(saida)
    expect(saida.defaultPrevented).toBe(true)

    // Com a faixa na tela, o Painel não fala com a nuvem: o reenvio nem sai.
    await act(async () => {
      fireEvent.click(within(nota).getByRole('button', { name: 'Tentar de novo' }))
    })
    expect(posts(srv)).toHaveLength(1)

    // A pessoa entra de novo; a faixa sai; o reenvio dá certo, com o id do primeiro.
    srv.entrar()
    fireEvent.click(screen.getByRole('button', { name: 'Entrar de novo' }))
    expect(abrirJanela).toHaveBeenCalledTimes(1)
    await act(async () => {
      window.dispatchEvent(new Event('focus'))
    })
    await waitFor(() => expect(screen.queryByText('Seu ditado continua aqui; entre de novo para enviar.')).toBeNull())
    await act(async () => {
      fireEvent.click(within(document.querySelector<HTMLElement>('.ditar-erro')!).getByRole('button', { name: 'Tentar de novo' }))
    })
    await screen.findByText('Texto ditado pelo celular.')
    const ids = posts(srv).map((x) => x.cabecalhos['Ditado-Id'])
    expect(ids).toHaveLength(2)
    expect(ids[0]).toBe(ids[1])
    expect(srv.chamadasAoProvedor).toBe(1)
    expect(document.querySelector('.ditar-erro')).toBeNull()
  })

  // A sessão que acaba com o ditado na memória: as variações da faixa.
  async function comOSessaoQueAcabou(m: ReturnType<typeof montar>) {
    await abrirOHistorico()
    await comecar()
    act(() => m.p.rodar(2000, 0.2))
    m.srv.expirar()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
    })
    await screen.findByText('Seu ditado continua aqui; entre de novo para enviar.')
  }

  it('com texto não salvo também: a faixa fala do texto e do ditado', async () => {
    const m = montar()
    await abrirOHistorico()
    fireEvent.click(await screen.findByRole('button', { name: /Manda o relatório amanhã/ }))
    fireEvent.change(screen.getByLabelText('Texto final'), { target: { value: 'Manda o relatório hoje.' } })
    await comecar()
    act(() => m.p.rodar(2000, 0.2))
    m.srv.expirar()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
    })
    await screen.findByText('Seu texto e seu ditado continuam aqui; entre de novo para salvar.')
    expect(screen.queryByRole('heading', { level: 1, name: 'Sua sessão terminou.' })).toBeNull()
  })

  it('a janela de entrar bloqueada: não há o que copiar de um áudio, e a faixa diz o que se perde', async () => {
    const m = montar({}, {}, undefined, false)
    await comOSessaoQueAcabou(m)
    fireEvent.click(screen.getByRole('button', { name: 'Entrar de novo' }))
    await screen.findByText('Entrar nesta aba descarta o ditado que não foi enviado.')
    expect(screen.getByText('Seu navegador bloqueou a janela.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Copiar o texto' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Entrar nesta aba' })).toBeTruthy()
  })

  it('outra conta na janela: a faixa diz de quem é o ditado', async () => {
    const m = montar()
    await comOSessaoQueAcabou(m)
    m.srv.entrar({ id: 'u2', email: 'outra@teste.local' })
    await act(async () => {
      window.dispatchEvent(new Event('focus'))
    })
    await screen.findByText('Este ditado é de voce@teste.local: entre com ela para enviar.')
    expect(screen.getByText('Você entrou em outra conta.')).toBeTruthy()
  })

  it('sem áudio à espera, o 401 continua trocando a tela inteira (a regra de antes)', async () => {
    const { srv } = montar()
    await abrirOHistorico()
    srv.expirar()
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByRole('heading', { level: 1, name: 'Sua sessão terminou.' })
  })

  it('o 401 de outra chamada, com o áudio à espera por falta de rede, também preserva a tela', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.derrubar(true)
    await ditar(p)
    await screen.findByRole('alert')
    srv.derrubar(false)
    srv.expirar()
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByText('Seu ditado continua aqui; entre de novo para enviar.')
    expect(screen.queryByRole('heading', { level: 1, name: 'Sua sessão terminou.' })).toBeNull()
  })
})
