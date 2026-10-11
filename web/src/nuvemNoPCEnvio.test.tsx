// A seção Nuvem do PC na F7c (ADR-019, "Decisões da F7c"): a pergunta do
// envio inicial, o "Copiando o histórico…", o progresso relido a cada 2 s
// (só com a seção à vista e só enquanto o envio corre), o fim, a cópia que
// falha, a linha de "em dia", o "Esquecer esta nuvem" com a pergunta na
// linha (o foco no Cancelar, o Esc), a frase da Privacidade e o selo com a
// escolha no Painel do PC. Contra o Agent de mentira (testes/servidorFalso).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { EstadoNuvem, ResumoDoHistorico } from './clienteLocal'
import { formatarHora } from './datas'
import { type OpcoesDoServidor, type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

async function abrir(opcoes: OpcoesDoServidor = {}, itens = [ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda.' })]) {
  const srv = servidorFalso(itens, opcoes)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'Nuvem' })
  await act(async () => {})
  return srv
}

const secao = () => screen.getByRole('region', { name: 'Nuvem' })
const botao = (nome: string | RegExp) => within(secao()).getByRole<HTMLButtonElement>('button', { name: nome })
const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && (!metodo || p.metodo === metodo))
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent
const textoDa = (el: Element) => el.textContent?.replace(/\s+/g, ' ').trim()

// CONF: a primeira rodada com esta conexão já deu certo (a pergunta espera
// por ela).
const CONF = new Date(2026, 9, 6, 15, 0).toISOString()

const resumo = (r: Partial<ResumoDoHistorico> = {}): ResumoDoHistorico => ({
  previa: 0,
  aceito: true,
  nao_subiram: 0,
  pendentes: 0,
  mudancas: 0,
  falhou: false,
  outra_nuvem: false,
  ...r,
})
const conectado = (h: Partial<ResumoDoHistorico> = {}): EstadoNuvem => ({
  estado: 'conectado',
  url: 'http://localhost:8080',
  email: 'voce@teste.local',
  nome: 'DELL-G15',
  historico: resumo(h),
})
const desconectado = (h: Partial<ResumoDoHistorico> = {}): EstadoNuvem => ({
  estado: 'desconectado',
  url: 'http://localhost:8080',
  email: 'voce@teste.local',
  historico: resumo(h),
})

describe('o envio do histórico', () => {
  it('pergunta uma vez, com o número, a lixeira e a cópia', async () => {
    await abrir({ nuvem: conectado({ previa: 800, aceito: false, conferido_em: CONF }) })
    const grupo = await within(secao()).findByRole('group', {
      name: 'Vão subir 800 ditados dos últimos 6 meses. A lixeira não vai. Antes, uma cópia do histórico fica neste PC.',
    })
    expect(within(grupo).getByRole('button', { name: 'Enviar o histórico' })).toBeTruthy()
    expect(grupo.textContent).not.toContain('outra nuvem')
  })

  it('no singular, e o número com o ponto do milhar', async () => {
    await abrir({ nuvem: conectado({ previa: 1, aceito: false, conferido_em: CONF }) })
    await within(secao()).findByText('Vai subir 1 ditado dos últimos 6 meses. A lixeira não vai. Antes, uma cópia do histórico fica neste PC.')
    cleanup()
    await abrir({ nuvem: conectado({ previa: 7000, aceito: false, conferido_em: CONF }) })
    await within(secao()).findByText(/^Vão subir 7\.000 ditados/)
  })

  it('depois de esquecer uma nuvem, diz que alguns ditados podem já estar nela', async () => {
    await abrir({ nuvem: conectado({ previa: 12, aceito: false, conferido_em: CONF, outra_nuvem: true }) })
    await within(secao()).findByText('Alguns destes ditados podem já estar na nuvem que este PC esqueceu.')
  })

  it('antes da primeira rodada com esta conexão, não pergunta: confere, e relê até ela terminar', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const srv = await abrir({ nuvem: conectado({ previa: 12, aceito: false }) })
    const status = await within(secao()).findByText('Conferindo a nuvem…')
    expect(status.getAttribute('role')).toBe('status')
    expect(within(secao()).queryByRole('button', { name: 'Enviar o histórico' })).toBeNull()
    srv.nuvemPorFora(conectado({ previa: 12, aceito: false, conferido_em: CONF }))
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(await within(secao()).findByRole('button', { name: 'Enviar o histórico' })).toBeTruthy()
    vi.useRealTimers()
    cleanup()
    await abrir({ nuvem: conectado({ previa: 12, aceito: false, falhou: true }) })
    await within(secao()).findByText('A última tentativa não deu certo. O PC tenta de novo sozinho.')
    expect(within(secao()).queryByRole('button', { name: 'Enviar o histórico' })).toBeNull()
  })

  it('sem nada a enviar, ou já aceito, não pergunta', async () => {
    await abrir({ nuvem: conectado({ previa: 0, aceito: false, conferido_em: CONF }) })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(within(secao()).queryByRole('button', { name: 'Enviar o histórico' })).toBeNull()
    cleanup()
    await abrir({ nuvem: conectado({ previa: 3, aceito: true }) })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(within(secao()).queryByRole('button', { name: 'Enviar o histórico' })).toBeNull()
  })

  it('Enviar: "Copiando o histórico…" no botão, e depois o progresso', async () => {
    const srv = await abrir({ nuvem: conectado({ previa: 800, aceito: false, conferido_em: CONF }) })
    const soltar = srv.pausar((p) => p.caminho === '/api/nuvem/enviar')
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Enviar o histórico' }))
    await within(secao()).findByRole('button', { name: 'Copiando o histórico…' })
    expect(botao('Copiando o histórico…').getAttribute('aria-disabled')).toBe('true')
    soltar()
    const status = await within(secao()).findByText('Enviando o histórico: 0 de 800.')
    expect(status.getAttribute('role')).toBe('status')
    expect(pedidosA(srv, '/api/nuvem/enviar', 'POST')).toHaveLength(1)
    expect(within(secao()).queryByRole('button', { name: /Enviar o histórico|Copiando/ })).toBeNull()
  })

  it('a cópia que falha diz por quê, e o botão volta', async () => {
    const srv = await abrir({ nuvem: conectado({ previa: 5, aceito: false, conferido_em: CONF }) })
    const frase = 'Não deu para copiar o histórico. Nada foi enviado. Já há 3 cópias de antes do envio ao lado do histórico: apague as que não quiser.'
    srv.recusarEnvio({ status: 409, mensagem: frase })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Enviar o histórico' }))
    const alerta = await within(secao()).findByRole('alert')
    expect(alerta.textContent).toBe(frase)
    expect(botao('Enviar o histórico').getAttribute('aria-disabled')).toBeNull()
  })

  it('relê a cada 2 s só enquanto envia e com a seção à vista, e avisa o fim', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const srv = await abrir({ nuvem: conectado({ envio: { feitos: 0, total: 800 }, pendentes: 800 }) })
    await within(secao()).findByText('Enviando o histórico: 0 de 800.')
    const leituras = () => pedidosA(srv, '/api/nuvem', 'GET').length
    expect(leituras()).toBe(0)

    srv.nuvemPorFora(conectado({ envio: { feitos: 120, total: 800 }, pendentes: 680 }))
    await act(async () => void vi.advanceTimersByTime(2000))
    await within(secao()).findByText('Enviando o histórico: 120 de 800.')
    expect(leituras()).toBe(1)

    // A aba escondida não relê.
    const visibilidade = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    await act(async () => void vi.advanceTimersByTime(4000))
    expect(leituras()).toBe(1)
    visibilidade.mockReturnValue('visible')

    // As Configurações fechadas também não.
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await act(async () => void vi.advanceTimersByTime(4000))
    expect(leituras()).toBe(1)
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await act(async () => {})

    // O fim: o aviso, com o que não subiu, e a releitura para.
    srv.nuvemPorFora(conectado({ nao_subiram: 2, conferido_em: new Date(2026, 9, 6, 15, 42).toISOString() }))
    await act(async () => void vi.advanceTimersByTime(2000))
    await waitFor(() => expect(aviso()).toContain('O histórico subiu para a nuvem. 2 ditados não subiram para a nuvem.'))
    const depois = leituras()
    await act(async () => void vi.advanceTimersByTime(6000))
    expect(leituras()).toBe(depois)
    // Depois que o aviso some, a linha continua dizendo o que não subiu.
    expect(secao().textContent).toContain(
      `Em dia com a nuvem. Conferido às ${formatarHora(new Date(2026, 9, 6, 15, 42).toISOString())}. 2 ditados não subiram para a nuvem.`,
    )
  })

  it('a linha de depois: em dia, o que falta, a falha e o que não subiu', async () => {
    const quando = new Date(2026, 9, 6, 9, 5).toISOString()
    for (const [h, frase] of [
      [{ conferido_em: quando }, `Em dia com a nuvem. Conferido às ${formatarHora(quando)}.`],
      [{ pendentes: 3, conferido_em: quando }, '3 para enviar.'],
      [{ pendentes: 2, falhou: true }, '2 para enviar. A última tentativa não deu certo. O PC tenta de novo sozinho.'],
      [{ falhou: true }, 'A última tentativa não deu certo. O PC tenta de novo sozinho.'],
      [{ conferido_em: quando, nao_subiram: 1 }, `Em dia com a nuvem. Conferido às ${formatarHora(quando)}. 1 ditado não subiu para a nuvem.`],
      [{ pendentes: 1, conferido_em: quando, nao_subiram: 3 }, '1 para enviar. 3 ditados não subiram para a nuvem.'],
    ] as [Partial<ResumoDoHistorico>, string][]) {
      await abrir({ nuvem: conectado(h) })
      const linha = await within(secao()).findByText(frase)
      expect(linha.getAttribute('role')).toBe('status')
      cleanup()
    }
  })
})

// F8h (ADR-019, D3): nada aprendido sobe antes do clique em "Enviar o
// histórico", e a pergunta conta as trocas aprendidas ("Vão subir N ditados e M
// trocas aprendidas"), também quando não há ditado a enviar (N = 0). O Agent
// de antes da F8 não manda previa_trocas: a frase é a de sempre.
describe('o envio com trocas aprendidas', () => {
  const resto = ' A lixeira não vai. Antes, uma cópia do histórico fica neste PC.'
  const pergunta = async () => within(secao()).findByRole('group')
  // A frase da pergunta (o grupo leva também o botão).
  const frase = async () => textoDa((await pergunta()).querySelector('#cfg-envio')!)

  it('com ditados e trocas: as duas contagens na mesma frase, e o resto de sempre', async () => {
    await abrir({ nuvem: conectado({ previa: 800, previa_trocas: 3, aceito: false, conferido_em: CONF }) })
    expect(await frase()).toBe(`Vão subir 800 ditados dos últimos 6 meses e 3 trocas aprendidas.${resto}`)
    expect(within(await pergunta()).getByRole('button', { name: 'Enviar o histórico' })).toBeTruthy()
  })

  it('o singular de cada um, e o verbo no plural quando são duas coisas', async () => {
    await abrir({ nuvem: conectado({ previa: 1, previa_trocas: 1, aceito: false, conferido_em: CONF }) })
    expect(await frase()).toBe(`Vão subir 1 ditado dos últimos 6 meses e 1 troca aprendida.${resto}`)
    cleanup()
    await abrir({ nuvem: conectado({ previa: 1, previa_trocas: 0, aceito: false, conferido_em: CONF }) })
    expect(await frase()).toBe(`Vai subir 1 ditado dos últimos 6 meses.${resto}`)
  })

  it('o número leva o ponto do milhar', async () => {
    await abrir({ nuvem: conectado({ previa: 7000, previa_trocas: 1200, aceito: false, conferido_em: CONF }) })
    expect(await frase()).toBe(`Vão subir 7.000 ditados dos últimos 6 meses e 1.200 trocas aprendidas.${resto}`)
  })

  it('sem ditado a enviar (N = 0) e com trocas: a pergunta aparece, sem a lixeira e sem a cópia', async () => {
    await abrir({ nuvem: conectado({ previa: 0, previa_trocas: 3, aceito: false, conferido_em: CONF }) })
    const grupo = await pergunta()
    expect(await frase()).toBe('Vão subir 3 trocas aprendidas.')
    expect(within(grupo).getByRole('button', { name: 'Enviar o histórico' })).toBeTruthy()
    expect(grupo.textContent).not.toContain('lixeira')
    expect(grupo.textContent).not.toContain('cópia')
    cleanup()
    await abrir({ nuvem: conectado({ previa: 0, previa_trocas: 1, aceito: false, conferido_em: CONF }) })
    expect(await frase()).toBe('Vai subir 1 troca aprendida.')
  })

  it('com N = 0, a frase dos ditados que "podem já estar na nuvem" não cabe', async () => {
    await abrir({ nuvem: conectado({ previa: 0, previa_trocas: 2, aceito: false, conferido_em: CONF, outra_nuvem: true }) })
    await pergunta()
    expect(secao().textContent).not.toContain('Alguns destes ditados')
    cleanup()
    await abrir({ nuvem: conectado({ previa: 5, previa_trocas: 2, aceito: false, conferido_em: CONF, outra_nuvem: true }) })
    await within(secao()).findByText('Alguns destes ditados podem já estar na nuvem que este PC esqueceu.')
  })

  it('com M = 0, é a frase de hoje, e sem previa_trocas (Agent de antes da F8) também', async () => {
    await abrir({ nuvem: conectado({ previa: 800, previa_trocas: 0, aceito: false, conferido_em: CONF }) })
    expect(await frase()).toBe(`Vão subir 800 ditados dos últimos 6 meses.${resto}`)
    expect(secao().textContent).not.toContain('troca')
  })

  it('sem ditado e sem troca, ou já aceito, não pergunta', async () => {
    await abrir({ nuvem: conectado({ previa: 0, previa_trocas: 0, aceito: false, conferido_em: CONF }) })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(within(secao()).queryByRole('group')).toBeNull()
    cleanup()
    await abrir({ nuvem: conectado({ previa: 0, previa_trocas: 4, aceito: true, conferido_em: CONF }) })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(within(secao()).queryByRole('button', { name: 'Enviar o histórico' })).toBeNull()
  })

  it('só com trocas, também espera a primeira rodada com a conexão: confere, e relê até ela terminar', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const srv = await abrir({ nuvem: conectado({ previa: 0, previa_trocas: 3, aceito: false }) })
    const status = await within(secao()).findByText('Conferindo a nuvem…')
    expect(status.getAttribute('role')).toBe('status')
    expect(within(secao()).queryByRole('button', { name: 'Enviar o histórico' })).toBeNull()
    srv.nuvemPorFora(conectado({ previa: 0, previa_trocas: 3, aceito: false, conferido_em: CONF }))
    await act(async () => void vi.advanceTimersByTime(2000))
    expect(await within(secao()).findByRole('button', { name: 'Enviar o histórico' })).toBeTruthy()
  })

  it('o clique com N = 0 não diz que copia o histórico: "Enviando…", um POST, e a pergunta sai', async () => {
    const srv = await abrir({ nuvem: conectado({ previa: 0, previa_trocas: 3, aceito: false, conferido_em: CONF }) })
    const soltar = srv.pausar((p) => p.caminho === '/api/nuvem/enviar')
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Enviar o histórico' }))
    await within(secao()).findByRole('button', { name: 'Enviando…' })
    expect(botao('Enviando…').getAttribute('aria-disabled')).toBe('true')
    expect(secao().textContent).not.toContain('Copiando')
    soltar()
    await within(secao()).findByText('3 para enviar.')
    expect(pedidosA(srv, '/api/nuvem/enviar', 'POST')).toHaveLength(1)
    expect(within(secao()).queryByRole('group')).toBeNull()
    // Não é o envio inicial de ditados: nada de "Enviando o histórico: 0 de 0".
    expect(secao().textContent).not.toContain('Enviando o histórico')
  })

  it('o clique com ditados e trocas copia o histórico, e o progresso é só dos ditados', async () => {
    const srv = await abrir({ nuvem: conectado({ previa: 800, previa_trocas: 3, aceito: false, conferido_em: CONF }) })
    const soltar = srv.pausar((p) => p.caminho === '/api/nuvem/enviar')
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Enviar o histórico' }))
    await within(secao()).findByRole('button', { name: 'Copiando o histórico…' })
    soltar()
    await within(secao()).findByText('Enviando o histórico: 0 de 800.')
  })
})

// F8h (ADR-019, escolha menor 1): com mais de 10 trocas saindo do dicionário de
// uma vez, o PC segura o aprendido (o freio) e o painel avisa. O campo é o
// `freio_trocas` do `historico` (06); o aviso não depende do envio inicial.
describe('o freio do aprendido', () => {
  const frase = (n: string) => `${n} trocas aprendidas saíram do dicionário de uma vez. Por segurança, nada do aprendido sobe para a nuvem até elas voltarem.`

  it('avisa quantas trocas saíram de uma vez, num role="status", sem o tom de erro', async () => {
    await abrir({ nuvem: conectado({ aceito: true, conferido_em: CONF, freio_trocas: 12 }) })
    const aviso = await within(secao()).findByText(frase('12'))
    expect(aviso.getAttribute('role')).toBe('status')
    expect(aviso.className).not.toContain('nota')
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })

  it('o número leva o ponto do milhar, e o singular existe (o campo é um inteiro qualquer)', async () => {
    await abrir({ nuvem: conectado({ aceito: true, conferido_em: CONF, freio_trocas: 1200 }) })
    await within(secao()).findByText(frase('1.200'))
    cleanup()
    await abrir({ nuvem: conectado({ aceito: true, conferido_em: CONF, freio_trocas: 1 }) })
    await within(secao()).findByText('1 troca aprendida saiu do dicionário de uma vez. Por segurança, nada do aprendido sobe para a nuvem até ela voltar.')
  })

  it('não toma o lugar do resto: o "em dia" e o freio ficam, cada um na sua linha', async () => {
    await abrir({ nuvem: conectado({ aceito: true, conferido_em: CONF, freio_trocas: 11 }) })
    await within(secao()).findByText(frase('11'))
    expect(within(secao()).getByText(`Em dia com a nuvem. Conferido às ${formatarHora(CONF)}.`)).toBeTruthy()
  })

  it('aparece também antes do clique no "Enviar o histórico", junto da pergunta', async () => {
    await abrir({ nuvem: conectado({ previa: 800, previa_trocas: 0, aceito: false, conferido_em: CONF, freio_trocas: 14 }) })
    await within(secao()).findByText(frase('14'))
    expect(within(secao()).getByRole('button', { name: 'Enviar o histórico' })).toBeTruthy()
  })

  it('sem o campo (Agent de antes da F8) ou com 0, não há aviso nem a palavra "dicionário"', async () => {
    await abrir({ nuvem: conectado({ aceito: true, conferido_em: CONF }) })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(secao().textContent).not.toContain('de uma vez')
    cleanup()
    await abrir({ nuvem: conectado({ aceito: true, conferido_em: CONF, freio_trocas: 0 }) })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(secao().textContent).not.toContain('de uma vez')
  })

  it('desconectado a varredura não roda: o campo, se vier, não vira aviso', async () => {
    await abrir({ nuvem: desconectado({ aceito: true, freio_trocas: 12 }) })
    await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' })
    expect(secao().textContent).not.toContain('de uma vez')
  })
})

describe('o Esquecer esta nuvem', () => {
  it('só aparece desconectado com uma nuvem gravada', async () => {
    await abrir({ nuvem: { estado: 'desconectado', url: '' } })
    expect(within(secao()).queryByRole('button', { name: 'Esquecer esta nuvem' })).toBeNull()
    cleanup()
    await abrir({ nuvem: conectado() })
    await within(secao()).findByText((_, el) => el?.textContent === 'Conectado como voce@teste.local.')
    expect(within(secao()).queryByRole('button', { name: 'Esquecer esta nuvem' })).toBeNull()
    cleanup()
    await abrir({ nuvem: desconectado() })
    expect(await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' })).toBeTruthy()
    expect(secao().textContent).toContain('Para ligar este PC a outra nuvem ou conta.')
  })

  it('pergunta na linha, com o foco no Cancelar; o Esc cancela e devolve o foco', async () => {
    const srv = await abrir({ nuvem: desconectado({ pendentes: 2, mudancas: 2 }) })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' }))
    const grupo = await within(secao()).findByRole('group', { name: /^Esquecer http:\/\/localhost:8080\?/ })
    expect(textoDa(grupo.querySelector('#cfg-esquecer')!)).toBe(
      'Esquecer http://localhost:8080? Este PC deixa de saber dessa nuvem e da conta voce@teste.local. Os ditados continuam aqui; na nuvem, nada muda.',
    )
    expect(grupo.textContent).toContain('2 mudanças feitas aqui ainda não subiram. Elas ficam neste PC.')
    // O espaço de cima dos ajustes (achado no navegador: a caixa encostava no campo).
    expect(grupo.classList.contains('ajuste')).toBe(true)
    const cancelar = within(grupo).getByRole('button', { name: 'Cancelar' })
    await waitFor(() => expect(document.activeElement).toBe(cancelar))
    expect(within(grupo).getByRole('button', { name: 'Esquecer' }).className).toBe('botao risco')

    fireEvent.keyDown(cancelar, { key: 'Escape' })
    await waitFor(() => expect(document.activeElement).toBe(botao('Esquecer esta nuvem')))
    expect(within(secao()).queryByRole('group', { name: /^Esquecer/ })).toBeNull()
    expect(pedidosA(srv, '/api/nuvem/esquecer')).toHaveLength(0)

    // O Cancelar também.
    fireEvent.click(botao('Esquecer esta nuvem'))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Cancelar' }))
    expect(within(secao()).queryByRole('group', { name: /^Esquecer/ })).toBeNull()
  })

  it('sem nada pendente, sem a frase das mudanças; uma no singular', async () => {
    await abrir({ nuvem: desconectado() })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' }))
    expect(secao().textContent).not.toContain('mudança')
    cleanup()
    await abrir({ nuvem: desconectado({ pendentes: 1, mudancas: 1 }) })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' }))
    expect(secao().textContent).toContain('1 mudança feita aqui ainda não subiu. Ela fica neste PC.')
  })

  // Reconferência da F7c: a fila com o histórico do envio inicial e o ditado
  // novo que não subiram, e uma edição. A frase conta só a edição: os outros
  // não são "mudanças feitas aqui".
  it('conta só as mudanças, e não o histórico nem o ditado novo que não subiram', async () => {
    await abrir({ nuvem: desconectado({ pendentes: 4, mudancas: 1 }) })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' }))
    expect(secao().textContent).toContain('1 mudança feita aqui ainda não subiu. Ela fica neste PC.')
    expect(secao().textContent).not.toContain('4 mudanças')
    cleanup()
    await abrir({ nuvem: desconectado({ pendentes: 3, mudancas: 0 }) })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' }))
    expect(secao().textContent).not.toContain('mudança')
  })

  it('Esquecer: o Agent esquece, o aviso diz, e o foco vai para o campo vazio', async () => {
    const srv = await abrir({ nuvem: desconectado() })
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' }))
    fireEvent.click(await within(secao()).findByRole('button', { name: 'Esquecer' }))
    await waitFor(() => expect(aviso()).toContain('Este PC esqueceu a nuvem.'))
    expect(pedidosA(srv, '/api/nuvem/esquecer', 'POST')).toHaveLength(1)
    const campo = within(secao()).getByLabelText<HTMLInputElement>('Endereço da nuvem')
    expect(campo.value).toBe('')
    await waitFor(() => expect(document.activeElement).toBe(campo))
    expect(within(secao()).queryByRole('button', { name: 'Esquecer esta nuvem' })).toBeNull()
  })
})

describe('a Privacidade do PC', () => {
  // F8h: as trocas aprendidas também vão para a nuvem (06 e 09).
  const frase =
    'Com a nuvem conectada, o texto dos seus ditados e as trocas que você aprendeu vão para a sua nuvem, em http://localhost:8080. O áudio e o que está nas outras janelas nunca vão.'
  const privacidade = () => screen.getByRole('region', { name: 'Privacidade' })

  it('diz para onde o texto e as trocas aprendidas vão, só com o PC conectado', async () => {
    await abrir({ nuvem: conectado() })
    await waitFor(() => expect(textoDa(privacidade())).toContain(frase))
    cleanup()
    await abrir({ nuvem: desconectado() })
    await within(secao()).findByRole('button', { name: 'Esquecer esta nuvem' })
    expect(privacidade().textContent).not.toContain('Com a nuvem conectada')
  })
})

describe('o selo no Painel do PC', () => {
  const comSelo = () => [
    ditado({ id: 'd1', created_at: new Date(2026, 9, 6, 14, 32).toISOString(), final_text: 'Manda o relatório amanhã cedo.' }),
    ditado({ id: 'd2', created_at: new Date(2026, 9, 6, 9, 5).toISOString(), final_text: 'A reunião é às dez.' }),
  ]

  async function montar() {
    const srv = servidorFalso(comSelo())
    srv.seloPorFora('d1', 'Manda o relatório amanhã à tarde.')
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    return srv
  }

  it('a linha mostra "2 versões", e aberta, as duas e os botões', async () => {
    await montar()
    const linha = await screen.findByRole('button', { name: /Manda o relatório amanhã cedo/ })
    expect(linha.textContent).toContain('2 versões')
    fireEvent.click(linha)
    const selo = await screen.findByRole('region', { name: 'Mudou em dois lugares. Ficou esta; a outra está guardada.' })
    expect(within(selo).getByRole('button', { name: 'Ficar com esta' })).toBeTruthy()
    expect(within(selo).getByRole('button', { name: 'Ficar com a outra' })).toBeTruthy()
    expect(selo.textContent).toContain('A que você não escolher é descartada.')
  })

  it('Ficar com a outra: o PATCH leva resolver_conflito, e o texto da outra fica', async () => {
    const srv = await montar()
    fireEvent.click(await screen.findByRole('button', { name: /Manda o relatório amanhã cedo/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Ficar com a outra' }))
    await waitFor(() => expect(aviso()).toBe('Ficou a versão escolhida.'))
    const patches = srv.pedidos.filter((p) => p.metodo === 'PATCH')
    expect(patches.map((p) => p.corpo)).toEqual([{ base_version: 2, resolver_conflito: 'trocar' }])
    expect(patches[0]!.auth).toBe('Bearer tok')
    expect(screen.getByLabelText<HTMLTextAreaElement>('Texto final').value).toBe('Manda o relatório amanhã à tarde.')
    expect(screen.queryByRole('region', { name: /Mudou em dois lugares/ })).toBeNull()
  })

  it('um 409 lê o ditado de novo e diz o que houve', async () => {
    const srv = await montar()
    fireEvent.click(await screen.findByRole('button', { name: /Manda o relatório amanhã cedo/ }))
    await screen.findByRole('button', { name: 'Ficar com esta' })
    // A escolha que chegou pela nuvem enquanto isso (o pull tirou o selo).
    const d1 = srv.itens.find((i) => i.id === 'd1')!
    delete d1.versao_em_conflito
    d1.version += 1
    fireEvent.click(screen.getByRole('button', { name: 'Ficar com esta' }))
    await waitFor(() => expect(aviso()).toBe('A versão já foi escolhida em outra aba.'))
    expect(srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho === '/api/history/d1')).toHaveLength(1)
  })
})
