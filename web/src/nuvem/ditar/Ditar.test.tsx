// O microfone do celular (Ditar.tsx) com o gravador de verdade e a Plataforma
// de mentira: o botão, a faixa da onda, o tempo, o Cancelar, o "Transcrevendo",
// a nota da falha que guarda o áudio, e o que cada gancho do Painel recebe.

import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import { type OpcoesDaPlataforma, plataformaFalsa } from './plataformaFalsa'
import type { ClienteNuvem, DitadoGravado, PedidoDeDitado } from '../cliente'
import { Ditar } from './Ditar'

afterEach(cleanup)

const ditado = {
  id: 'd1',
  raw_text: 'texto ditado',
  final_text: 'Texto ditado.',
  mode: 'smart',
  asr_provider: 'openai',
  status: 'done',
  created_at: '2026-10-10T12:00:00.000Z',
  updated_at: '2026-10-10T12:00:00.000Z',
  version: 1,
}

const busca = <input type="search" className="busca" aria-label="Buscar no histórico" />

function montar(plataforma: OpcoesDaPlataforma = {}, escondido = false) {
  const p = plataformaFalsa(plataforma)
  const ditar = vi.fn<(pedido: PedidoDeDitado, sinal?: AbortSignal) => Promise<DitadoGravado>>(async () => ({ ditado }))
  const cliente = { ditar } as unknown as ClienteNuvem
  const chamadas: string[] = []
  const ganchos = {
    aoChegar: vi.fn(),
    aoAudioPendente: vi.fn((tem: boolean) => void chamadas.push(`audio:${tem}`)),
    aoSemSessao: vi.fn(() => void chamadas.push('sessao')),
    abrirConfiguracoes: vi.fn(),
    avisar: vi.fn<(t: ReactNode) => void>(),
  }
  const ui = (esconder: boolean) => <Ditar cliente={cliente} plataforma={p.plataforma} busca={esconder ? null : busca} escondido={esconder} {...ganchos} />
  const r = render(ui(escondido))
  return { p, ditar, ganchos, chamadas, ...r, mudarEscondido: (e: boolean) => r.rerender(ui(e)) }
}

const microfone = () => screen.getByRole('button', { name: 'Ditar' })
const parar = () => screen.getByRole('button', { name: 'Parar e enviar' })
const campoBusca = () => screen.queryByRole('searchbox', { name: 'Buscar no histórico' })
const status = () => screen.getByRole('status')

// comecar toca no microfone e deixa o gravador gravando.
async function comecar() {
  await act(async () => {
    fireEvent.click(microfone())
  })
}

// falar avança o tempo com o nível dado.
function falar(m: ReturnType<typeof montar>, ms: number, rms = 0.2) {
  act(() => m.p.rodar(ms, rms))
}

async function tocarParar() {
  await act(async () => {
    fireEvent.click(parar())
  })
}

describe('em repouso', () => {
  it('um botão pequeno, "Ditar", ao lado da busca; nada de faixa', () => {
    montar()
    expect(microfone().getAttribute('title')).toBe('Ditar')
    expect(microfone().hasAttribute('aria-disabled')).toBe(false)
    expect(campoBusca()).not.toBeNull()
    expect(document.querySelector('.ditar-faixa')).toBeNull()
    expect(status().textContent).toBe('')
  })

  it('a região viva existe desde o começo (um leitor de tela só anuncia o que entra numa região que já estava)', () => {
    montar()
    expect(screen.getAllByRole('status')).toHaveLength(1)
  })

  it('sem suporte do navegador, o microfone nem aparece: só a busca', () => {
    montar({ suporta: false })
    expect(screen.queryByRole('button', { name: 'Ditar' })).toBeNull()
    expect(campoBusca()).not.toBeNull()
  })

  it('nas Configurações, parado, nada aparece', () => {
    montar({}, true)
    expect(screen.queryByRole('button')).toBeNull()
    expect(campoBusca()).toBeNull()
  })
})

describe('gravando', () => {
  it('a linha da busca vira a faixa da onda, com o tempo e o Cancelar; o microfone vira o parar', async () => {
    const m = montar()
    await comecar()
    expect(campoBusca()).toBeNull()
    expect(document.querySelector('.ditar-faixa')).not.toBeNull()
    expect(document.querySelectorAll('.onda-barra')).toHaveLength(33)
    expect(screen.getByRole('button', { name: 'Cancelar' })).toBeTruthy()
    expect(parar().className).toContain('gravando')
    expect(m.p.mic.comecou).toBe(1)
  })

  it('"Gravando" é só para o leitor de tela; a onda e o tempo ficam escondidos dele', async () => {
    montar()
    await comecar()
    expect(status().textContent).toBe('Gravando')
    expect(status().className).toBe('so-leitor')
    expect(document.querySelector('.onda')!.getAttribute('aria-hidden')).toBe('true')
    expect(document.querySelector('.ditar-tempo')!.getAttribute('aria-hidden')).toBe('true')
    // Nenhum rótulo visível "Gravando".
    expect(document.querySelector('.ditar-faixa')!.textContent).not.toContain('Gravando')
  })

  it('o tempo anda em m:ss', async () => {
    const m = montar()
    await comecar()
    expect(document.querySelector('.ditar-tempo')!.textContent).toBe('0:00')
    falar(m, 3000)
    expect(document.querySelector('.ditar-tempo')!.textContent).toBe('0:03')
    falar(m, 65_000)
    expect(document.querySelector('.ditar-tempo')!.textContent).toBe('1:08')
  })

  it('a onda acompanha a voz: barra alta com fala, em repouso no silêncio', async () => {
    const m = montar()
    await comecar()
    const barras = Array.from(document.querySelectorAll<HTMLElement>('.onda-barra'))
    falar(m, 100, 0)
    expect(barras.at(-1)!.style.transform).toBe('scaleY(0.120)')
    expect(Number(barras.at(-1)!.style.opacity)).toBeCloseTo(0.45, 3)
    falar(m, 100, 0.2)
    const alta = Number(/scaleY\(([\d.]+)\)/.exec(barras.at(-1)!.style.transform)![1])
    expect(alta).toBeGreaterThan(0.7)
    expect(Number(barras.at(-1)!.style.opacity)).toBeGreaterThan(0.85)
    // A fala vai passando para a esquerda: a barra antiga ainda guarda o pico.
    falar(m, 400, 0)
    expect(Number(/scaleY\(([\d.]+)\)/.exec(barras.at(-1)!.style.transform)![1])).toBeLessThan(0.2)
    expect(barras.some((b) => Number(/scaleY\(([\d.]+)\)/.exec(b.style.transform)![1]) > 0.7)).toBe(true)
  })

  it('o Cancelar descarta sem enviar e a busca volta', async () => {
    const m = montar()
    await comecar()
    falar(m, 1000)
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(m.ditar).not.toHaveBeenCalled()
    expect(campoBusca()).not.toBeNull()
    expect(m.p.mic.descartado).toBe(1)
    expect(status().textContent).toBe('')
  })

  it('o Esc na faixa também cancela', async () => {
    const m = montar()
    await comecar()
    falar(m, 1000)
    fireEvent.keyDown(document.querySelector('.ditar-faixa')!, { key: 'Escape' })
    expect(m.ditar).not.toHaveBeenCalled()
    expect(campoBusca()).not.toBeNull()
  })

  it('a tela que pode apagar: se o Wake Lock não veio, a faixa avisa', async () => {
    montar({ telaNegada: true })
    await comecar()
    await act(async () => {})
    expect(screen.getByText('A tela pode apagar e parar o ditado.')).toBeTruthy()
  })

  it('com o Wake Lock, não avisa', async () => {
    montar()
    await comecar()
    await act(async () => {})
    expect(screen.queryByText(/A tela pode apagar/)).toBeNull()
  })

  it('abrindo o microfone (a permissão na tela): ocupado, com o rótulo certo, e a região viva diz', async () => {
    const m = montar({ permissao: 'manual' })
    await comecar()
    const botao = screen.getByRole('button', { name: 'Abrindo o microfone' })
    expect(botao.getAttribute('aria-disabled')).toBe('true')
    expect(status().textContent).toBe('Abrindo o microfone')
    await act(async () => {
      m.p.conceder()
    })
    expect(parar()).toBeTruthy()
  })
})

describe('parar e enviar', () => {
  it('"Transcrevendo", o botão ocupado; chegando, o ditado vai ao Painel e a busca volta', async () => {
    const m = montar()
    let chegar: (g: DitadoGravado) => void = () => {}
    m.ditar.mockImplementationOnce(() => new Promise((r) => (chegar = r)))
    await comecar()
    falar(m, 2000)
    await tocarParar()

    expect(screen.getByText('Transcrevendo…')).toBeTruthy()
    const ocupado = screen.getByRole('button', { name: 'Transcrevendo' })
    expect(ocupado.getAttribute('aria-disabled')).toBe('true')
    expect(status().textContent).toBe('Transcrevendo')
    // O toque no ocupado não faz nada.
    fireEvent.click(ocupado)
    expect(m.ditar).toHaveBeenCalledTimes(1)
    expect(m.p.mic.parou).toBe(1)

    await act(async () => chegar({ ditado }))
    expect(m.ganchos.aoChegar).toHaveBeenCalledTimes(1)
    expect(m.ganchos.aoChegar).toHaveBeenCalledWith(ditado, { nota: undefined, aviso: undefined })
    expect(campoBusca()).not.toBeNull()
    expect(microfone().hasAttribute('aria-disabled')).toBe(false)
    expect(status().textContent).toBe('')
  })

  it('o foco fica no botão do microfone do começo ao fim (o mesmo elemento)', async () => {
    const m = montar()
    const botao = microfone()
    botao.focus()
    await comecar()
    expect(document.activeElement).toBe(parar())
    falar(m, 2000)
    await tocarParar()
    expect(document.activeElement?.tagName).toBe('BUTTON')
    expect(document.activeElement).toBe(botao)
    expect(m.ganchos.aoChegar).toHaveBeenCalledTimes(1)
  })

  it('o que o app fez sozinho e o aviso do Polish chegam junto do ditado', async () => {
    const m = montar()
    m.ditar.mockResolvedValueOnce({ ditado, aviso: 'sem_credito' })
    await comecar()
    falar(m, 2000)
    act(() => m.p.sairDaFrente())
    await act(async () => {})
    expect(m.ganchos.aoChegar).toHaveBeenCalledWith(ditado, { nota: 'Parei quando o app saiu da frente.', aviso: 'O Polish não agiu: falta crédito no provedor.' })
  })
})

describe('as falhas que não guardam o áudio viram o aviso da pílula', () => {
  it('"Não ouvi nada." (menos de 300 ms de fala): nada é enviado', async () => {
    const m = montar()
    await comecar()
    falar(m, 2000, 0.001)
    await tocarParar()
    expect(m.ditar).not.toHaveBeenCalled()
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Não ouvi nada.')
    expect(campoBusca()).not.toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('"Ficou longo demais." do servidor', async () => {
    const m = montar()
    const e = new ErroDaApi(413, 'x')
    e.codigo = 'PAYLOAD_TOO_LARGE'
    m.ditar.mockRejectedValueOnce(e)
    await comecar()
    falar(m, 2000)
    await tocarParar()
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Ficou longo demais.')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(m.ganchos.aoAudioPendente.mock.calls.map(([t]) => t)).toEqual([true, false])
  })

  it('a permissão negada manda liberar nas permissões do site', async () => {
    const m = montar({ permissao: 'manual' })
    await comecar()
    await act(async () => {
      m.p.negar(Object.assign(new Error('x'), { name: 'NotAllowedError' }))
    })
    expect(m.ganchos.avisar).toHaveBeenCalledWith('Sem acesso ao microfone. Libere nas permissões do site.')
    expect(campoBusca()).not.toBeNull()
  })
})

describe('a falha que guarda o áudio: a nota, com "Tentar de novo" e "Descartar"', () => {
  async function comFalhaDeRede() {
    const m = montar()
    m.ditar.mockRejectedValueOnce(new ForaDoAr())
    await comecar()
    falar(m, 2000)
    await tocarParar()
    return m
  }

  it('uma nota de erro com a frase; o microfone fica ocupado e o Painel sabe que há áudio', async () => {
    const m = await comFalhaDeRede()
    const nota = screen.getByRole('alert')
    expect(nota.className).toContain('nota')
    expect(within(nota).getByText('Sem conexão.')).toBeTruthy()
    expect(within(nota).getByRole('button', { name: 'Tentar de novo' })).toBeTruthy()
    expect(within(nota).getByRole('button', { name: 'Descartar' })).toBeTruthy()
    expect(within(nota).queryByRole('button', { name: 'Abrir as Configurações' })).toBeNull()
    expect(microfone().getAttribute('aria-disabled')).toBe('true')
    // Não grava por cima do áudio que espera.
    fireEvent.click(microfone())
    expect(m.p.estado.abriu).toBe(1)
    expect(m.chamadas).toEqual(['audio:true'])
    expect(m.ganchos.avisar).not.toHaveBeenCalled()
  })

  it('Tentar de novo reenvia com o mesmo id; dado certo, a nota sai, o ditado chega e o Painel sabe que o áudio saiu', async () => {
    const m = await comFalhaDeRede()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    })
    expect(m.ditar).toHaveBeenCalledTimes(2)
    expect(m.ditar.mock.calls[1]![0].id).toBe(m.ditar.mock.calls[0]![0].id)
    expect(screen.queryByRole('alert')).toBeNull()
    expect(m.ganchos.aoChegar).toHaveBeenCalledTimes(1)
    expect(m.chamadas).toEqual(['audio:true', 'audio:false'])
  })

  it('Descartar solta o áudio', async () => {
    const m = await comFalhaDeRede()
    fireEvent.click(screen.getByRole('button', { name: 'Descartar' }))
    expect(screen.queryByRole('alert')).toBeNull()
    expect(m.ditar).toHaveBeenCalledTimes(1)
    expect(m.chamadas).toEqual(['audio:true', 'audio:false'])
    expect(microfone().hasAttribute('aria-disabled')).toBe(false)
  })

  it.each([
    ['SEM_CHAVE', 'Falta a chave do provedor.'],
    ['SEM_ACEITE', 'Falta aceitar o aviso do ditado pelo celular.'],
    ['PROVEDOR_RECUSOU_CHAVE', 'O provedor recusou a chave.'],
  ])('%s leva às Configurações', async (codigo, frase) => {
    const m = montar()
    const e = new ErroDaApi(403, 'x')
    e.codigo = codigo
    m.ditar.mockRejectedValueOnce(e)
    await comecar()
    falar(m, 2000)
    await tocarParar()
    const nota = screen.getByRole('alert')
    expect(within(nota).getByText(frase)).toBeTruthy()
    fireEvent.click(within(nota).getByRole('button', { name: 'Abrir as Configurações' }))
    expect(m.ganchos.abrirConfiguracoes).toHaveBeenCalledTimes(1)
    // E o áudio continua à espera.
    expect(within(nota).getByRole('button', { name: 'Tentar de novo' })).toBeTruthy()
  })

  it('"Espere um pouco." diz o tempo do Retry-After', async () => {
    const m = montar()
    const e = new ErroDaApi(429, 'x')
    e.codigo = 'LIMITE'
    e.esperaSeg = 12
    m.ditar.mockRejectedValueOnce(e)
    await comecar()
    falar(m, 2000)
    await tocarParar()
    expect(screen.getByRole('alert').textContent).toContain('Espere um pouco. Tente de novo em 12 s.')
  })

  it('a sessão que acaba: o Painel é avisado do áudio ANTES, e depois da sessão', async () => {
    const m = montar()
    m.ditar.mockRejectedValueOnce(new SemSessao())
    await comecar()
    falar(m, 2000)
    await tocarParar()
    expect(m.chamadas).toEqual(['audio:true', 'sessao'])
    expect(screen.getByRole('alert').textContent).toContain('O ditado espera você entrar de novo.')
  })
})

describe('nas Configurações (escondido)', () => {
  it('a gravação em curso não some: a faixa e o botão de parar ficam', async () => {
    const m = montar()
    await comecar()
    falar(m, 1000)
    m.mudarEscondido(true)
    expect(document.querySelector('.ditar-faixa')).not.toBeNull()
    expect(parar()).toBeTruthy()
    // E o tempo continua.
    falar(m, 2000)
    expect(document.querySelector('.ditar-tempo')!.textContent).toBe('0:03')
  })

  it('o áudio que espera "Tentar de novo" também fica à vista', async () => {
    const m = montar()
    m.ditar.mockRejectedValueOnce(new ForaDoAr())
    await comecar()
    falar(m, 2000)
    await tocarParar()
    m.mudarEscondido(true)
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Tentar de novo' })).toBeTruthy()
  })

  it('terminada a gravação, sai de cena de novo', async () => {
    const m = montar()
    await comecar()
    falar(m, 2000)
    m.mudarEscondido(true)
    await tocarParar()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('o gravador é o mesmo: trocar de tela no meio não reinicia nada', async () => {
    const m = montar()
    await comecar()
    m.mudarEscondido(true)
    m.mudarEscondido(false)
    expect(m.p.estado.abriu).toBe(1)
    expect(m.p.mic.descartado).toBe(0)
  })
})

describe('a tela sai de cena', () => {
  it('desmontar no meio da gravação desliga o microfone, solta a tela e não envia', async () => {
    const m = montar()
    await comecar()
    falar(m, 1000)
    m.unmount()
    expect(m.p.mic.descartado).toBe(1)
    expect(m.p.estado.telaSolta).toBe(1)
    expect(m.ditar).not.toHaveBeenCalled()
    expect(m.p.quadrosPendentes()).toBe(0)
    expect(m.p.timersPendentes()).toBe(0)
  })

  it('desmontar com o áudio à espera solta o áudio', async () => {
    const m = montar()
    m.ditar.mockRejectedValueOnce(new ForaDoAr())
    await comecar()
    falar(m, 2000)
    await tocarParar()
    m.unmount()
    expect(m.chamadas).toEqual(['audio:true'])
  })
})
