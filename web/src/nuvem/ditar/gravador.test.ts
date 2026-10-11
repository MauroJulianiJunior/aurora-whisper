// A máquina de estados do ditado no celular (ADR-021): parado, pedindo
// permissão, gravando, enviando, pronto e erro. Com a Plataforma de mentira
// (testes/plataformaFalsa.ts): nenhum teste abre o microfone, o Wake Lock ou a
// rede de verdade.

import { describe, expect, it, vi } from 'vitest'
import { ErroDaApi, ForaDoAr, SemSessao } from '../../api'
import { plataformaFalsa } from './plataformaFalsa'
import type { DitadoGravado, PedidoDeDitado } from '../cliente'
import { type Chegada, Gravador } from './gravador'
import { tetoMs } from './medidas'

// O teste do teto corre 120 s de relógio de mentira, quadro a quadro; com a
// máquina ocupada, os 5 s padrão apertam.
vi.setConfig({ testTimeout: 20_000 })

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

type Enviar = (pedido: PedidoDeDitado, sinal: AbortSignal) => Promise<DitadoGravado>

function montar(opcoes: Parameters<typeof plataformaFalsa>[0] = {}, liberar?: () => Promise<boolean>) {
  const p = plataformaFalsa(opcoes)
  const enviar = vi.fn<Enviar>(async () => ({ ditado }))
  const g = new Gravador({ plataforma: p.plataforma, enviar, liberar })
  const fases: string[] = []
  g.assinar(() => {
    const f = g.estado().fase
    if (fases.at(-1) !== f) fases.push(f)
  })
  const chegadas: Chegada[] = []
  g.aoChegar((c) => chegadas.push(c))
  const audio: boolean[] = []
  g.aoAudio((tem) => audio.push(tem))
  return { p, g, enviar, fases, chegadas, audio }
}

// gravar abre o microfone e fala por `ms` ao nível `rms`; devolve com a
// gravação em curso.
async function gravar(m: ReturnType<typeof montar>, ms: number, rms = 0.2) {
  await m.g.iniciar()
  m.p.rodar(ms, rms)
}

const nada = async () => {
  // deixa as promessas pendentes andarem
  await Promise.resolve()
  await Promise.resolve()
}

describe('um ditado, do toque ao texto', () => {
  it('parado, pedindo, gravando, enviando, pronto; e o envio leva o id, a duração e a fala medida', async () => {
    const m = montar()
    expect(m.g.estado()).toEqual({ fase: 'parado' })
    await gravar(m, 1500)
    expect(m.g.estado().fase).toBe('gravando')
    await m.g.parar()

    expect(m.fases).toEqual(['pedindo', 'gravando', 'enviando', 'pronto'])
    expect(m.enviar).toHaveBeenCalledTimes(1)
    const [pedido, sinal] = m.enviar.mock.calls[0]!
    expect(pedido.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(pedido.audio).toBe(m.p.mic.audio)
    // 1500 ms de ouvido, dos quais os últimos 200 não contam como fala.
    expect(pedido.duracaoMs).toBeGreaterThanOrEqual(1480)
    expect(pedido.duracaoMs).toBeLessThanOrEqual(1520)
    expect(pedido.falaMs).toBeGreaterThanOrEqual(1260)
    expect(pedido.falaMs).toBeLessThanOrEqual(1320)
    expect(sinal).toBeInstanceOf(AbortSignal)
    expect(m.g.estado()).toEqual({ fase: 'pronto', ditado })
    expect(m.chegadas).toEqual([{ ditado }])
  })

  it('o microfone abre uma vez, começa, e desliga na hora ao parar (ADR-015)', async () => {
    const m = montar()
    await gravar(m, 800)
    expect(m.p.estado.abriu).toBe(1)
    expect(m.p.mic.comecou).toBe(1)
    expect(m.p.mic.parou).toBe(0)
    await m.g.parar()
    expect(m.p.mic.parou).toBe(1)
    // parar() já entregou o áudio; descartar o microfone não vai nada além.
    expect(m.p.mic.descartado).toBe(0)
  })

  it('o áudio só existe na memória do gravador: some depois do envio', async () => {
    const m = montar()
    await gravar(m, 1000)
    await m.g.parar()
    expect(m.g.temAudio()).toBe(false)
    // Teve áudio na memória (o aoAudio avisou), e depois não teve mais.
    expect(m.audio).toEqual([true, false])
  })

  it('terminado, não sobra nada ligado: nem o quadro, nem o teto, nem o prazo, nem o ouvinte da página', async () => {
    const m = montar()
    await gravar(m, 1000)
    await m.g.parar()
    expect(m.p.quadrosPendentes()).toBe(0)
    expect(m.p.timersPendentes()).toBe(0)
    expect(m.p.ouvintesDeVisibilidade()).toBe(0)
    expect(m.p.mic.ouvintes()).toBe(0)
  })

  it('o ditado novo usa outro id; o reenvio é que repete', async () => {
    const m = montar()
    await gravar(m, 1000)
    await m.g.parar()
    await gravar(m, 1000)
    await m.g.parar()
    const [a, b] = m.enviar.mock.calls.map(([p]) => p.id)
    expect(a).not.toBe(b)
  })

  it('não começa por cima do que está em curso: pedindo, gravando ou enviando', async () => {
    const m = montar({ permissao: 'manual' })
    void m.g.iniciar()
    await nada()
    expect(m.g.estado().fase).toBe('pedindo')
    await m.g.iniciar()
    expect(m.p.estado.abriu).toBe(1)
    m.p.conceder()
    await nada()
    expect(m.g.estado().fase).toBe('gravando')
    await m.g.iniciar()
    expect(m.p.estado.abriu).toBe(1)
  })
})

describe('a permissão do microfone', () => {
  it('negada: a frase manda liberar nas permissões do site, e nada ficou na memória', async () => {
    const m = montar({ permissao: 'manual' })
    void m.g.iniciar()
    await nada()
    m.p.negar(Object.assign(new Error('x'), { name: 'NotAllowedError' }))
    await nada()
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'permissao', frase: 'Sem acesso ao microfone. Libere nas permissões do site.', retem: false } })
    expect(m.g.temAudio()).toBe(false)
    expect(m.enviar).not.toHaveBeenCalled()
  })

  it('a porta antes do microfone: sem o OK dela, o getUserMedia nem é chamado', async () => {
    const m = montar({}, async () => false)
    await m.g.iniciar()
    expect(m.p.estado.abriu).toBe(0)
    expect(m.g.estado()).toEqual({ fase: 'parado' })
  })

  it('a porta que falha também não abre o microfone', async () => {
    const m = montar({}, async () => {
      throw new Error('x')
    })
    await m.g.iniciar()
    expect(m.p.estado.abriu).toBe(0)
    expect(m.g.estado()).toEqual({ fase: 'parado' })
  })

  it('com o OK da porta, o microfone abre depois dela', async () => {
    const ordem: string[] = []
    const m = montar({}, async () => {
      ordem.push('porta')
      return true
    })
    const abrir = m.p.plataforma.abrirMicrofone
    m.p.plataforma.abrirMicrofone = () => {
      ordem.push('microfone')
      return abrir()
    }
    await m.g.iniciar()
    expect(ordem).toEqual(['porta', 'microfone'])
    expect(m.g.estado().fase).toBe('gravando')
  })

  it('cancelar com a permissão na tela: o microfone que chega depois é desligado sem gravar', async () => {
    const m = montar({ permissao: 'manual' })
    void m.g.iniciar()
    await nada()
    m.g.cancelar()
    expect(m.g.estado()).toEqual({ fase: 'parado' })
    m.p.conceder()
    await nada()
    expect(m.p.mic.descartado).toBe(1)
    expect(m.p.mic.comecou).toBe(0)
    expect(m.g.estado()).toEqual({ fase: 'parado' })
  })
})

describe('sem fala, sem envio', () => {
  it('menos de 300 ms de fala: nada é enviado, o microfone sai, e a frase é "Não ouvi nada."', async () => {
    const m = montar()
    await m.g.iniciar()
    m.p.rodar(200, 0.2)
    m.p.rodar(1500, 0)
    await m.g.parar()
    expect(m.enviar).not.toHaveBeenCalled()
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'sem-fala', frase: 'Não ouvi nada.', retem: false } })
    expect(m.p.mic.descartado).toBe(1)
    expect(m.p.mic.parou).toBe(0)
    expect(m.g.temAudio()).toBe(false)
  })

  it('a fala só nos últimos 200 ms (o toque para parar) não conta', async () => {
    const m = montar()
    await m.g.iniciar()
    m.p.rodar(1000, 0)
    m.p.rodar(200, 0.3)
    await m.g.parar()
    expect(m.enviar).not.toHaveBeenCalled()
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'sem-fala' } })
  })

  it('fala que basta, com silêncio depois: envia', async () => {
    const m = montar()
    await m.g.iniciar()
    m.p.rodar(600, 0.2)
    m.p.rodar(900, 0.001)
    await m.g.parar()
    expect(m.enviar).toHaveBeenCalledTimes(1)
    expect(m.enviar.mock.calls[0]![0].falaMs).toBeGreaterThanOrEqual(300)
  })

  it('o gravador que não entregou nada (áudio vazio) não envia um corpo vazio', async () => {
    const m = montar({ bytes: 0 })
    await gravar(m, 1500)
    await m.g.parar()
    expect(m.enviar).not.toHaveBeenCalled()
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'gravacao', retem: false } })
    expect(m.g.temAudio()).toBe(false)
  })

  it('o silêncio inteiro não paga o provedor: a pessoa vê a frase e a linha volta ao normal', async () => {
    const m = montar()
    await gravar(m, 3000, 0.0005)
    await m.g.parar()
    expect(m.enviar).not.toHaveBeenCalled()
    m.g.dispensar()
    expect(m.g.estado()).toEqual({ fase: 'parado' })
  })
})

describe('o Cancelar', () => {
  it('descarta sem enviar, desliga o microfone e tudo o que estava ligado', async () => {
    const m = montar()
    await gravar(m, 1200)
    m.g.cancelar()
    expect(m.g.estado()).toEqual({ fase: 'parado' })
    expect(m.enviar).not.toHaveBeenCalled()
    expect(m.p.mic.descartado).toBe(1)
    expect(m.p.mic.parou).toBe(0)
    expect(m.p.estado.telaSolta).toBe(1)
    expect(m.p.quadrosPendentes()).toBe(0)
    expect(m.p.timersPendentes()).toBe(0)
    expect(m.p.ouvintesDeVisibilidade()).toBe(0)
    expect(m.p.mic.ouvintes()).toBe(0)
    expect(m.g.temAudio()).toBe(false)
  })

  it('não faz nada fora da gravação (enviando, parado)', async () => {
    const m = montar()
    m.g.cancelar()
    expect(m.g.estado()).toEqual({ fase: 'parado' })
    let soltar: (g: DitadoGravado) => void = () => {}
    m.enviar.mockImplementationOnce(() => new Promise((r) => (soltar = r)))
    await gravar(m, 1000)
    const parando = m.g.parar()
    await nada()
    expect(m.g.estado().fase).toBe('enviando')
    m.g.cancelar()
    expect(m.g.estado().fase).toBe('enviando')
    soltar({ ditado })
    await parando
    expect(m.g.estado().fase).toBe('pronto')
  })

  it('depois do Cancelar, dá para gravar de novo, com outro id', async () => {
    const m = montar()
    await gravar(m, 1000)
    m.g.cancelar()
    await gravar(m, 1000)
    await m.g.parar()
    expect(m.enviar).toHaveBeenCalledTimes(1)
    expect(m.p.estado.ids).toHaveLength(2)
    expect(m.enviar.mock.calls[0]![0].id).toBe(m.p.estado.ids[1])
  })
})

describe('o teto de 120 s', () => {
  it('para sozinho, envia o que gravou, e a nota diz que parou no limite', async () => {
    const m = montar()
    await m.g.iniciar()
    m.p.rodar(tetoMs - 2000, 0.2)
    expect(m.g.estado().fase).toBe('gravando')
    m.p.rodar(2500, 0.2)
    await nada()
    await nada()
    expect(m.enviar).toHaveBeenCalledTimes(1)
    const pedido = m.enviar.mock.calls[0]![0]
    expect(pedido.duracaoMs).toBeLessThanOrEqual(tetoMs)
    expect(pedido.duracaoMs).toBeGreaterThanOrEqual(tetoMs - 20)
    expect(m.g.estado().fase).toBe('pronto')
    expect(m.chegadas[0]).toEqual({ ditado, nota: 'Parei no limite de 2 minutos.' })
  })
})

describe('a página sai da frente, a faixa fica muda ou acaba', () => {
  it('a tela apagada ou outro app: para, envia o que já gravou e diz isso', async () => {
    const m = montar()
    await gravar(m, 2000)
    m.p.sairDaFrente()
    await nada()
    await nada()
    expect(m.enviar).toHaveBeenCalledTimes(1)
    expect(m.chegadas[0]!.nota).toBe('Parei quando o app saiu da frente.')
    expect(m.p.mic.parou).toBe(1)
  })

  it('sem fala até ali: não envia, e a frase junta a parada com o "Não ouvi nada."', async () => {
    const m = montar()
    await gravar(m, 2000, 0)
    m.p.sairDaFrente()
    await nada()
    expect(m.enviar).not.toHaveBeenCalled()
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'sem-fala', frase: 'Parei quando o app saiu da frente. Não ouvi nada.' } })
  })

  it.each(['mudo', 'acabou'] as const)('a faixa do microfone: %s (uma ligação recebida) para e envia', async (motivo) => {
    const m = montar()
    await gravar(m, 2000)
    m.p.mic.interromper(motivo)
    await nada()
    await nada()
    expect(m.enviar).toHaveBeenCalledTimes(1)
    expect(m.chegadas[0]!.nota).toBe('O microfone foi interrompido.')
  })

  it('um aviso a mais depois de parar não faz nada', async () => {
    const m = montar()
    await gravar(m, 2000)
    await m.g.parar()
    m.p.sairDaFrente()
    m.p.mic.interromper('mudo')
    await nada()
    expect(m.enviar).toHaveBeenCalledTimes(1)
  })
})

describe('o Wake Lock', () => {
  it('a tela fica acesa enquanto grava e é solta ao parar', async () => {
    const m = montar()
    await gravar(m, 1000)
    await nada()
    expect(m.p.estado.pediuTela).toBe(1)
    expect(m.p.estado.telaSolta).toBe(0)
    await m.g.parar()
    expect(m.p.estado.telaSolta).toBe(1)
  })

  it('o pedido que falha avisa que a tela pode apagar e parar o ditado', async () => {
    const m = montar({ telaNegada: true })
    await gravar(m, 500)
    await nada()
    expect(m.g.estado()).toMatchObject({ fase: 'gravando', telaPodeApagar: true })
  })

  it('o pedido que só chega depois de parar é solto na hora', async () => {
    const m = montar()
    let chegar: (t: { soltar(): void } | null) => void = () => {}
    const soltou = vi.fn()
    m.p.plataforma.pedirTela = () => new Promise((r) => (chegar = r))
    await gravar(m, 1000)
    m.g.cancelar()
    chegar({ soltar: soltou })
    await nada()
    expect(soltou).toHaveBeenCalledTimes(1)
  })

  it('o pedido que levanta erro é como o negado', async () => {
    const m = montar()
    m.p.plataforma.pedirTela = () => Promise.reject(new Error('x'))
    await gravar(m, 500)
    await nada()
    expect(m.g.estado()).toMatchObject({ fase: 'gravando', telaPodeApagar: true })
  })
})

describe('a falha do envio, código a código', () => {
  const api = (status: number, codigo?: string, esperaSeg?: number) => {
    const e = new ErroDaApi(status, 'frase do servidor')
    if (codigo) e.codigo = codigo
    if (esperaSeg !== undefined) e.esperaSeg = esperaSeg
    return e
  }

  // [erro do envio, tipo, o áudio fica na memória?]
  const casos: [string, unknown, string, boolean][] = [
    ['sem rede', new ForaDoAr(), 'rede', true],
    ['401', new SemSessao(), 'sessao', true],
    ['SEM_ACEITE', api(403, 'SEM_ACEITE'), 'sem-aceite', true],
    ['SEM_CHAVE', api(403, 'SEM_CHAVE'), 'sem-chave', true],
    ['UM_POR_VEZ', api(429, 'UM_POR_VEZ', 5), 'espere', true],
    ['LIMITE', api(429, 'LIMITE', 5), 'espere', true],
    ['OCUPADO', api(503, 'OCUPADO', 5), 'espere', true],
    ['PROVEDOR_RECUSOU_CHAVE', api(502, 'PROVEDOR_RECUSOU_CHAVE'), 'provedor-chave', true],
    ['PROVEDOR_LIMITE', api(502, 'PROVEDOR_LIMITE'), 'provedor-fora', true],
    ['PROVEDOR_FORA', api(502, 'PROVEDOR_FORA'), 'provedor-fora', true],
    ['500 sem código', api(500), 'outro', true],
    ['PAYLOAD_TOO_LARGE', api(413, 'PAYLOAD_TOO_LARGE'), 'longo', false],
    ['FORMATO', api(415, 'FORMATO'), 'formato', false],
    ['SEM_FALA do servidor', api(422, 'SEM_FALA'), 'sem-fala', false],
    ['APAGADO', api(410, 'APAGADO'), 'apagado', false],
    ['404 (o servidor sem a chave de cifra)', api(404), 'indisponivel', false],
  ]

  it.each(casos)('%s', async (_, erro, tipo, guarda) => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(erro)
    await gravar(m, 1500)
    await m.g.parar()
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo, retem: guarda } })
    expect(m.g.temAudio()).toBe(guarda)
    // O que não guarda áudio não tem o que tentar de novo.
    await m.g.tentarDeNovo()
    expect(m.enviar).toHaveBeenCalledTimes(guarda ? 2 : 1)
    // Nenhuma frase do servidor vai à tela.
    expect(JSON.stringify(m.g.estado())).not.toContain('frase do servidor')
  })

  it('o prazo de 90 s: o envio é cortado, e o áudio fica para tentar de novo com o mesmo id', async () => {
    const m = montar()
    m.enviar.mockImplementationOnce(
      (_, sinal) =>
        new Promise((_, rejeitar) => {
          sinal.addEventListener('abort', () => rejeitar(new DOMException('abortado', 'AbortError')))
        }),
    )
    await gravar(m, 1500)
    const parando = m.g.parar()
    await nada()
    expect(m.g.estado().fase).toBe('enviando')
    m.p.passo(89_000)
    expect(m.enviar.mock.calls[0]![1].aborted).toBe(false)
    m.p.passo(1_500)
    await parando
    expect(m.enviar.mock.calls[0]![1].aborted).toBe(true)
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'prazo', frase: 'O envio demorou demais.', retem: true } })
    expect(m.g.temAudio()).toBe(true)
  })

  it('o prazo é cancelado quando o envio dá certo: nada fica agendado', async () => {
    const m = montar()
    await gravar(m, 1000)
    await m.g.parar()
    expect(m.p.timersPendentes()).toBe(0)
  })
})

describe('Tentar de novo: o mesmo id, o mesmo áudio', () => {
  it('o reenvio leva o id e o áudio do primeiro; dado certo, o áudio sai da memória', async () => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(new ForaDoAr())
    await gravar(m, 1500)
    await m.g.parar()
    expect(m.g.estado().fase).toBe('erro')
    const primeiro = m.enviar.mock.calls[0]![0]

    await m.g.tentarDeNovo()
    expect(m.enviar).toHaveBeenCalledTimes(2)
    const segundo = m.enviar.mock.calls[1]![0]
    expect(segundo.id).toBe(primeiro.id)
    expect(segundo.audio).toBe(primeiro.audio)
    expect(segundo.duracaoMs).toBe(primeiro.duracaoMs)
    expect(segundo.falaMs).toBe(primeiro.falaMs)
    expect(m.g.estado().fase).toBe('pronto')
    expect(m.g.temAudio()).toBe(false)
    expect(m.chegadas).toHaveLength(1)
  })

  it('falha de novo: o áudio continua, e dá para tentar mais uma vez, sempre com o mesmo id', async () => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(new ForaDoAr()).mockRejectedValueOnce(new ForaDoAr())
    await gravar(m, 1500)
    await m.g.parar()
    await m.g.tentarDeNovo()
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'rede' } })
    await m.g.tentarDeNovo()
    expect(m.g.estado().fase).toBe('pronto')
    expect(new Set(m.enviar.mock.calls.map(([p]) => p.id)).size).toBe(1)
  })

  it('a nota do app (parou no limite) acompanha o ditado até chegar, mesmo depois de uma falha', async () => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(new ForaDoAr())
    await gravar(m, 1500)
    m.p.sairDaFrente()
    await nada()
    await nada()
    expect(m.g.estado().fase).toBe('erro')
    await m.g.tentarDeNovo()
    expect(m.chegadas[0]!.nota).toBe('Parei quando o app saiu da frente.')
  })

  it('Descartar solta o áudio, e dá para ditar de novo', async () => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(new ForaDoAr())
    await gravar(m, 1500)
    await m.g.parar()
    expect(m.g.temAudio()).toBe(true)
    m.g.descartar()
    expect(m.g.estado()).toEqual({ fase: 'parado' })
    expect(m.g.temAudio()).toBe(false)
    expect(m.audio).toEqual([true, false])
    await m.g.tentarDeNovo()
    expect(m.enviar).toHaveBeenCalledTimes(1)
    await gravar(m, 1000)
    expect(m.g.estado().fase).toBe('gravando')
  })

  it('com um áudio à espera, o microfone não abre por cima dele', async () => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(new ForaDoAr())
    await gravar(m, 1500)
    await m.g.parar()
    await m.g.iniciar()
    expect(m.p.estado.abriu).toBe(1)
    expect(m.g.temAudio()).toBe(true)
  })

  it('não dispara duas vezes: o segundo toque enquanto envia não envia de novo', async () => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(new ForaDoAr())
    let soltar: (g: DitadoGravado) => void = () => {}
    m.enviar.mockImplementationOnce(() => new Promise((r) => (soltar = r)))
    await gravar(m, 1500)
    await m.g.parar()
    const a = m.g.tentarDeNovo()
    await nada()
    expect(m.g.estado().fase).toBe('enviando')
    await m.g.tentarDeNovo()
    expect(m.enviar).toHaveBeenCalledTimes(2)
    soltar({ ditado })
    await a
    expect(m.g.estado().fase).toBe('pronto')
  })
})

describe('a sessão que acaba no meio do envio (401)', () => {
  it('o áudio fica; o aoAudio chega ANTES do aoSessaoPerdida; e o reenvio, depois de entrar, tem o mesmo id', async () => {
    const m = montar()
    const ordem: string[] = []
    m.g.aoAudio((tem) => ordem.push(`audio:${tem}`))
    m.g.aoSessaoPerdida(() => ordem.push('sessao'))
    m.enviar.mockRejectedValueOnce(new SemSessao())
    await gravar(m, 1500)
    await m.g.parar()
    expect(ordem).toEqual(['audio:true', 'sessao'])
    expect(m.g.estado()).toMatchObject({ fase: 'erro', erro: { tipo: 'sessao', retem: true } })
    expect(m.g.temAudio()).toBe(true)

    await m.g.tentarDeNovo()
    expect(m.g.estado().fase).toBe('pronto')
    expect(m.enviar.mock.calls[1]![0].id).toBe(m.enviar.mock.calls[0]![0].id)
    expect(ordem).toEqual(['audio:true', 'sessao', 'audio:false'])
  })

  it('só o 401 avisa a sessão perdida', async () => {
    const m = montar()
    const sessao = vi.fn()
    m.g.aoSessaoPerdida(sessao)
    m.enviar.mockRejectedValueOnce(new ForaDoAr())
    await gravar(m, 1500)
    await m.g.parar()
    expect(sessao).not.toHaveBeenCalled()
  })
})

describe('o aviso do Polish', () => {
  it('o ditado que chegou leva a frase de o Polish ter caído para o Smart', async () => {
    const m = montar()
    m.enviar.mockResolvedValueOnce({ ditado, aviso: 'chave_recusada' })
    await gravar(m, 1500)
    await m.g.parar()
    expect(m.chegadas[0]).toEqual({ ditado, aviso: 'O Polish não agiu: o provedor recusou a chave.' })
  })
})

describe('encerrar: a tela saiu de cena', () => {
  it('gravando: desliga o microfone, solta tudo e não envia', async () => {
    const m = montar()
    await gravar(m, 1000)
    m.g.encerrar()
    expect(m.g.estado()).toEqual({ fase: 'parado' })
    expect(m.p.mic.descartado).toBe(1)
    expect(m.p.estado.telaSolta).toBe(1)
    expect(m.p.quadrosPendentes()).toBe(0)
    expect(m.p.timersPendentes()).toBe(0)
    expect(m.enviar).not.toHaveBeenCalled()
  })

  it('enviando: corta o envio, e a resposta que vier depois não muda nada', async () => {
    const m = montar()
    let soltar: (g: DitadoGravado) => void = () => {}
    m.enviar.mockImplementationOnce(() => new Promise((r) => (soltar = r)))
    await gravar(m, 1000)
    const parando = m.g.parar()
    await nada()
    m.g.encerrar()
    expect(m.enviar.mock.calls[0]![1].aborted).toBe(true)
    soltar({ ditado })
    await parando
    expect(m.g.estado()).toEqual({ fase: 'parado' })
    expect(m.chegadas).toEqual([])
    expect(m.g.temAudio()).toBe(false)
  })

  it('com o áudio à espera: solta o áudio', async () => {
    const m = montar()
    m.enviar.mockRejectedValueOnce(new ForaDoAr())
    await gravar(m, 1000)
    await m.g.parar()
    m.g.encerrar()
    expect(m.g.temAudio()).toBe(false)
    expect(m.g.estado()).toEqual({ fase: 'parado' })
  })

  it('o objeto continua usável (o StrictMode monta duas vezes)', async () => {
    const m = montar()
    m.g.encerrar()
    await gravar(m, 1000)
    await m.g.parar()
    expect(m.g.estado().fase).toBe('pronto')
  })
})

describe('a onda e o tempo', () => {
  it('um quadro por leitura, com o tempo decorrido e as 33 barras; só enquanto grava', async () => {
    const m = montar()
    const quadros: { decorrido: number; barras: number[] }[] = []
    m.g.aoQuadro((q) => quadros.push(q))
    await m.g.iniciar()
    m.p.rodar(1000, 0.1)
    expect(quadros.length).toBeGreaterThan(50)
    const ultimo = quadros.at(-1)!
    expect(ultimo.barras).toHaveLength(33)
    expect(ultimo.decorrido).toBeGreaterThan(900)
    // 0,1 de RMS é -20 dBFS: a barra enche a maior parte do curso.
    expect(ultimo.barras.at(-1)).toBeGreaterThan(0.7)
    await m.g.parar()
    const antes = quadros.length
    m.p.rodar(200, 0.1)
    expect(quadros.length).toBe(antes)
  })
})

// A prova de que ninguém grava áudio fora da memória: o gravador não tem
// como (só conhece a Plataforma), e a Plataforma de mentira só devolve o Blob
// a quem pediu.
describe('o áudio só vai para quem o envia', () => {
  it('o pedido é a única referência: depois do envio, o gravador não guarda o Blob', async () => {
    const m = montar()
    await gravar(m, 1000)
    await m.g.parar()
    const donos = Object.values(m.g as unknown as Record<string, unknown>).filter((v) => v === m.p.mic.audio)
    expect(donos).toEqual([])
    expect((m.g as unknown as { pedido: unknown }).pedido).toBeNull()
  })
})
