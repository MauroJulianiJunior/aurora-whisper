// O aceite no app inteiro (ADR-021, decisão H; C6b): a folha antes do microfone,
// contra a nuvem de mentira (testes/servidorFalsoNuvem.ts) e a Plataforma de
// mentira (plataformaFalsa.ts). O que se prova: sem o aceite, nem o
// getUserMedia nem o pedido da chave acontecem; com ele, o app leva à chave, e
// só depois dela o microfone abre; o servidor que recusa o ditado por falta de
// aceite reabre a folha. Nenhum teste abre o microfone nem a rede de verdade.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type OpcoesDaNuvem, itemNuvem, servidorFalsoNuvem } from '../../testes/servidorFalsoNuvem'
import AppNuvem from '../AppNuvem'
import { VERSAO_DO_AVISO } from '../avisoDoDitado'
import { plataformaFalsa } from './plataformaFalsa'

const em = (dia: number, hora: number) => new Date(2026, 9, dia, hora).toISOString()
const espera = { timeout: 5000 }
vi.setConfig({ testTimeout: 20_000 })

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 10, 12, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

function montar(opcoes: OpcoesDaNuvem = {}) {
  const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: em(9, 14), final_text: 'Manda o relatório amanhã.' })], opcoes)
  const p = plataformaFalsa()
  const recarregar = vi.fn()
  render(<AppNuvem buscar={srv.buscar} irPara={vi.fn()} abrirJanela={vi.fn(() => true)} plataformaDeDitar={p.plataforma} recarregar={recarregar} />)
  return { srv, p, recarregar }
}

type Servidor = ReturnType<typeof servidorFalsoNuvem>
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
// Os pedidos do ditado na conta (a leitura do estado, o aceite, as chaves), na ordem.
const daConta = (srv: Servidor) => srv.pedidos.filter((x) => /^\/api\/v1\/(dictation|ai-keys)/.test(x.caminho)).map((x) => `${x.metodo} ${x.caminho}`)
const envios = (srv: Servidor) => srv.pedidos.filter((x) => x.metodo === 'POST' && x.caminho === '/api/v1/dictations')

async function abrirOHistorico() {
  await screen.findByRole('button', { name: /Manda o relatório amanhã/ }, espera)
}

// tocarNoMicrofone: o toque no celular dá o foco ao botão (o fireEvent.click não),
// e é para ele que a folha devolve o foco ao fechar.
async function tocarNoMicrofone() {
  const botao = await screen.findByRole('button', { name: 'Ditar' }, espera)
  botao.focus()
  await act(async () => {
    fireEvent.click(botao)
  })
}

const folha = () => screen.findByRole('dialog', undefined, espera)
const aceito = () => screen.getByRole('button', { name: 'Aceito' })

// O aceite de um servidor que já o tem: o provedor da transcrição, o do Polish
// (ou nenhum) e a versão do aviso.
const aceiteDe = (transcricao: 'openai' | 'groq', polish: 'openai' | 'anthropic' | null = null, versao = VERSAO_DO_AVISO) => ({
  versao,
  transcricao,
  polish,
  aceito_em: '2026-10-09T12:00:00.000Z',
})

describe('sem o aceite, o microfone nem abre', () => {
  it('o toque mostra a folha; nem o getUserMedia nem o pedido da chave acontecem', async () => {
    const { srv, p } = montar({ ditado: {} })
    await abrirOHistorico()
    await tocarNoMicrofone()
    const f = await folha()
    expect(f.textContent).toContain('Ao ditar pelo celular, a sua voz vai a um provedor')
    expect(p.estado.abriu).toBe(0)
    // Só a leitura do estado da conta: nenhuma chave, nenhum aceite, nenhum ditado.
    expect(daConta(srv)).toEqual(['GET /api/v1/dictation'])
    expect(envios(srv)).toHaveLength(0)
  })

  it('"Agora não": a folha sai, nada é gravado, o microfone não abre, e o resto do app segue igual', async () => {
    const { srv, p } = montar({ ditado: {} })
    await abrirOHistorico()
    await tocarNoMicrofone()
    await folha()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Agora não' }))
    })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(p.estado.abriu).toBe(0)
    expect(daConta(srv)).toEqual(['GET /api/v1/dictation'])
    expect(srv.daConta.aceite).toBeNull()
    // O microfone continua à mão, e o foco voltou a ele.
    const microfone = screen.getByRole('button', { name: 'Ditar' })
    expect(microfone.hasAttribute('aria-disabled')).toBe(false)
    expect(document.activeElement).toBe(microfone)
    // O histórico continua lá.
    expect(screen.getByRole('button', { name: /Manda o relatório amanhã/ })).toBeTruthy()
  })

  it('o Esc é "Agora não"', async () => {
    const { srv, p } = montar({ ditado: {} })
    await abrirOHistorico()
    await tocarNoMicrofone()
    const f = await folha()
    fireEvent.keyDown(f, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(p.estado.abriu).toBe(0)
    expect(daConta(srv)).toEqual(['GET /api/v1/dictation'])
  })

  it('o aviso mudou (o servidor diz que o aceite não é vigente): a folha abre de novo, sem o microfone', async () => {
    const { p } = montar({ ditado: { aceite: aceiteDe('openai', null, VERSAO_DO_AVISO - 1), chaves: { openai: 'sk-teste-1234567890' } } })
    await abrirOHistorico()
    await tocarNoMicrofone()
    await folha()
    expect(p.estado.abriu).toBe(0)
  })

  it('ligar o Polish com um provedor fora do aceite também pede a folha, com o bloco dele', async () => {
    const { p } = montar({
      ditado: {
        preferencias: { polish: true, polish_provedor: 'anthropic', polish_modelo: 'claude-haiku-4-5' },
        aceite: aceiteDe('openai'),
        chaves: { openai: 'sk-teste-1234567890', anthropic: 'sk-ant-teste-1234567890' },
      },
    })
    await abrirOHistorico()
    await tocarNoMicrofone()
    const f = await folha()
    expect(f.textContent).toContain('O Polish que você está ligando: o texto do ditado, para Anthropic')
    expect(p.estado.abriu).toBe(0)
  })
})

describe('o primeiro uso, do aceite à chave', () => {
  it('Aceito grava o aceite e leva à chave nas Configurações; o microfone só abre com a chave, no toque seguinte', async () => {
    const { srv, p } = montar({ ditado: {} })
    await abrirOHistorico()
    await tocarNoMicrofone()
    await folha()
    await act(async () => {
      fireEvent.click(aceito())
    })

    // O aceite ficou no servidor, com a versão do aviso e o provedor da transcrição.
    expect(srv.daConta.aceite).toMatchObject({ versao: VERSAO_DO_AVISO, transcricao: 'openai', polish: null })
    const consent = srv.pedidos.find((x) => x.caminho === '/api/v1/dictation/consent')
    expect(consent?.metodo).toBe('POST')
    expect(consent?.corpo).toEqual({ versao: VERSAO_DO_AVISO, transcricao: 'openai' })
    // Sem a chave, o app leva a pessoa a colá-la, na seção do ditado: o
    // microfone não abre, e a chave só é pedida depois do aceite.
    await screen.findByRole('heading', { level: 1, name: 'Configurações' }, espera)
    expect(aviso().textContent).toBe('Falta a chave da OpenAI. Cole a chave nas Configurações.')
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Ditar pelo celular'))
    expect(p.estado.abriu).toBe(0)
    expect(daConta(srv).some((x) => x.startsWith('PUT /api/v1/ai-keys'))).toBe(false)

    // Cola a chave (o campo é uma senha: a chave não aparece).
    const campo = await screen.findByLabelText<HTMLInputElement>('Chave da OpenAI', undefined, espera)
    expect(campo.type).toBe('password')
    fireEvent.input(campo, { target: { value: 'sk-teste-do-celular-WXYZ' } })
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Guardar chave' }))
    })
    await screen.findByText(/Chave terminada em/, undefined, espera)
    expect(document.querySelector('.estado-chave')!.textContent).toBe('Chave terminada em WXYZ.')
    expect(srv.daConta.chaves.openai).toBe('sk-teste-do-celular-WXYZ')
    expect(document.body.textContent).not.toContain('sk-teste-do-celular')

    // De volta ao histórico, o toque seguinte abre o microfone.
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await tocarNoMicrofone()
    await screen.findByRole('button', { name: 'Parar e enviar' }, espera)
    expect(p.estado.abriu).toBe(1)
  })

  it('o provedor escolhido na folha é o das preferências, e a chave pedida é a dele', async () => {
    const { srv, p } = montar({ ditado: {} })
    await abrirOHistorico()
    await tocarNoMicrofone()
    await folha()
    await act(async () => {
      fireEvent.click(screen.getByRole('radio', { name: 'Groq' }))
    })
    expect(screen.getByRole('dialog').textContent).toContain('Groq. Ela diz que, por padrão')
    await act(async () => {
      fireEvent.click(aceito())
    })
    expect(srv.daConta.aceite).toMatchObject({ transcricao: 'groq', polish: null })
    expect(srv.daConta.preferencias.provedor).toBe('groq')
    await waitFor(() => expect(aviso().textContent).toBe('Falta a chave da Groq. Cole a chave nas Configurações.'))
    expect(p.estado.abriu).toBe(0)
  })

  it('com o aceite e a chave já na conta, o toque abre o microfone direto, e a conta é lida uma vez só', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    await tocarNoMicrofone()
    await screen.findByRole('button', { name: 'Parar e enviar' }, espera)
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(p.estado.abriu).toBe(1)
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    })
    await tocarNoMicrofone()
    await screen.findByRole('button', { name: 'Parar e enviar' }, espera)
    expect(p.estado.abriu).toBe(2)
    expect(daConta(srv)).toEqual(['GET /api/v1/dictation'])
  })
})

describe('quando o aviso do servidor é mais novo que o do app (AVISO_DESATUALIZADO)', () => {
  it('o Aceito recebe 409: a folha fecha, nada é gravado, o microfone não abre, e o app recarrega para ler o texto novo', async () => {
    const { srv, p, recarregar } = montar({ ditado: {} })
    await abrirOHistorico()
    await tocarNoMicrofone()
    await folha()
    // O deploy aconteceu com a folha na tela: o texto que ela mostra é o velho.
    srv.subirOAvisoPorFora()
    expect(recarregar).not.toHaveBeenCalled()
    await act(async () => {
      fireEvent.click(aceito())
    })
    await waitFor(() => expect(recarregar).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(p.estado.abriu).toBe(0)
    expect(srv.daConta.aceite).toBeNull()
    expect(daConta(srv)).toEqual(['GET /api/v1/dictation', 'POST /api/v1/dictation/consent'])
    // Sem frase de erro: o app vai ser lido de novo, e o texto novo vem na próxima folha.
    expect(screen.queryByRole('alert')).toBeNull()
  })
})

describe('quando o servidor recusa o ditado por falta de aceite', () => {
  async function ditarComOAceiteVencido() {
    const m = montar()
    await abrirOHistorico()
    await tocarNoMicrofone()
    await screen.findByRole('button', { name: 'Parar e enviar' }, espera)
    // O aviso mudou no servidor enquanto o microfone estava aberto.
    m.srv.envelhecerAceitePorFora()
    act(() => m.p.rodar(2000, 0.2))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
    })
    return m
  }

  it('a nota guarda o áudio e oferece "Ver o aviso", e não as Configurações', async () => {
    await ditarComOAceiteVencido()
    const nota = await screen.findByRole('alert', undefined, espera)
    expect(nota.textContent).toContain('Falta aceitar o aviso do ditado pelo celular.')
    expect(within(nota).getByRole('button', { name: 'Ver o aviso' })).toBeTruthy()
    expect(within(nota).queryByRole('button', { name: 'Abrir as Configurações' })).toBeNull()
    expect(within(nota).getByRole('button', { name: 'Tentar de novo' })).toBeTruthy()
  })

  it('Ver o aviso abre a folha; aceitando, o áudio que esperava é reenviado com o MESMO id e chega', async () => {
    const { srv } = await ditarComOAceiteVencido()
    const nota = await screen.findByRole('alert', undefined, espera)
    await act(async () => {
      fireEvent.click(within(nota).getByRole('button', { name: 'Ver o aviso' }))
    })
    await folha()
    await act(async () => {
      fireEvent.click(aceito())
    })
    await screen.findByText('Texto ditado pelo celular.', undefined, espera)
    expect(envios(srv)).toHaveLength(2)
    expect(envios(srv)[1]!.cabecalhos['Ditado-Id']).toBe(envios(srv)[0]!.cabecalhos['Ditado-Id'])
    expect(srv.itens.filter((i) => i.source_platform === 'web')).toHaveLength(1)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('"Agora não" deixa o áudio onde estava, com a nota', async () => {
    const { srv } = await ditarComOAceiteVencido()
    const nota = await screen.findByRole('alert', undefined, espera)
    await act(async () => {
      fireEvent.click(within(nota).getByRole('button', { name: 'Ver o aviso' }))
    })
    await folha()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Agora não' }))
    })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.getByRole('alert').textContent).toContain('Falta aceitar o aviso')
    expect(envios(srv)).toHaveLength(1)
  })
})

describe('quando a chave falta, o app leva à seção do ditado', () => {
  it('a chave que o servidor guardava e já não decifra (colar_de_novo) pede para colar de novo, e a seção diz o mesmo', async () => {
    const { p } = montar({ ditado: { aceite: aceiteDe('openai'), colarDeNovo: ['openai'] } })
    await abrirOHistorico()
    await tocarNoMicrofone()
    await screen.findByRole('heading', { level: 1, name: 'Configurações' }, espera)
    expect(aviso().textContent).toBe('Cole a chave da OpenAI de novo nas Configurações.')
    expect(p.estado.abriu).toBe(0)
    expect(document.querySelector('.estado-chave')!.textContent).toBe('A chave guardada não vale mais neste servidor. Cole a chave de novo.')
  })

  it('"Abrir as Configurações" abre as Configurações com o foco no título da seção; e o toque seguinte confere a conta de novo', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    await tocarNoMicrofone()
    await screen.findByRole('button', { name: 'Parar e enviar' }, espera)
    srv.interceptar((x) =>
      x.caminho === '/api/v1/dictations' ? new Response(JSON.stringify({ error: { code: 'SEM_CHAVE', message: 'sem chave' } }), { status: 409 }) : undefined,
    )
    act(() => p.rodar(2000, 0.2))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Parar e enviar' }))
    })
    const nota = await screen.findByRole('alert', undefined, espera)
    expect(nota.textContent).toContain('Falta a chave do provedor.')
    await act(async () => {
      fireEvent.click(within(nota).getByRole('button', { name: 'Abrir as Configurações' }))
    })
    await screen.findByRole('heading', { level: 1, name: 'Configurações' }, espera)
    await waitFor(() => expect(document.activeElement?.textContent).toBe('Ditar pelo celular'))
    // A porta sabia errado e esqueceu: o próximo toque vai à conta (o GET de novo).
    srv.interceptar(null)
    await act(async () => {
      fireEvent.click(within(screen.getByRole('alert')).getByRole('button', { name: 'Descartar' }))
    })
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    const antes = daConta(srv).filter((x) => x === 'GET /api/v1/dictation').length
    await tocarNoMicrofone()
    await screen.findByRole('button', { name: 'Parar e enviar' }, espera)
    expect(daConta(srv).filter((x) => x === 'GET /api/v1/dictation').length).toBe(antes + 1)
  })
})

describe('quando a conta não responde ao toque', () => {
  it('sem rede: a pílula diz, e o microfone não abre', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.derrubar(true)
    await tocarNoMicrofone()
    await waitFor(() => expect(aviso().textContent).toBe('Sem conexão.'))
    expect(p.estado.abriu).toBe(0)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('o servidor sem o ditado (404): a pílula diz, e o microfone não abre', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.interceptar((x) => (x.caminho === '/api/v1/dictation' ? new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'rota não encontrada' } }), { status: 404 }) : undefined))
    await tocarNoMicrofone()
    await waitFor(() => expect(aviso().textContent).toBe('Este servidor não tem o ditado pelo celular.'))
    expect(p.estado.abriu).toBe(0)
  })

  it('a sessão acabou: a tela de sessão expirada, sem microfone', async () => {
    const { srv, p } = montar()
    await abrirOHistorico()
    srv.expirar()
    await tocarNoMicrofone()
    await screen.findByRole('heading', { name: 'Sua sessão terminou.' }, espera)
    expect(p.estado.abriu).toBe(0)
  })
})
