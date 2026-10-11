// A Plataforma de verdade (navegador.ts) contra dublês dos globais do navegador:
// o getUserMedia, o MediaRecorder, o AudioContext, o Wake Lock. Nenhum teste
// abre o microfone, nem a área de transferência, de verdade (ADR-021, "Pontos
// de parada"). O que se confere: o que é pedido ao navegador (webm/opus a 32
// kbit/s, sem ligar o analisador a nenhuma saída), que o microfone desliga na
// hora, e que nada fica aberto quando algo falha.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MicrofoneIndisponivel } from './erros'
import type { Microfone } from './gravador'
import { plataformaDoNavegador } from './navegador'

class FaixaFalsa extends EventTarget {
  paradas = 0
  stop() {
    this.paradas++
  }
}

class StreamFalso {
  faixas = [new FaixaFalsa()]
  getTracks() {
    return this.faixas
  }
  getAudioTracks() {
    return this.faixas
  }
}

class GravadorFalso extends EventTarget {
  static instancias: GravadorFalso[] = []
  static suportados = new Set(['audio/webm;codecs=opus'])
  static isTypeSupported(tipo: string) {
    return GravadorFalso.suportados.has(tipo)
  }
  // Quem testa decide se o stop chega (o navegador com defeito não o manda).
  static mandaStop = true
  state: 'inactive' | 'recording' = 'inactive'
  iniciou = 0
  parou = 0
  args: { stream: StreamFalso; opcoes: MediaRecorderOptions }
  constructor(stream: StreamFalso, opcoes: MediaRecorderOptions) {
    super()
    this.args = { stream, opcoes }
    GravadorFalso.instancias.push(this)
  }
  start(...args: unknown[]) {
    this.iniciou++
    this.state = 'recording'
    this.argsDoStart = args
  }
  argsDoStart: unknown[] = []
  stop() {
    this.parou++
    this.state = 'inactive'
    if (!GravadorFalso.mandaStop) return
    // O último pedaço chega antes do stop.
    queueMicrotask(() => {
      this.entregar(new Blob([new Uint8Array([1, 2, 3])]))
      this.dispatchEvent(new Event('stop'))
    })
  }
  entregar(blob: Blob) {
    const e = new Event('dataavailable') as Event & { data: Blob }
    e.data = blob
    this.dispatchEvent(e)
  }
}

class ContextoFalso {
  static instancias: ContextoFalso[] = []
  static amostras = new Float32Array(1024)
  fechado = false
  retomado = 0
  ligacoes: unknown[] = []
  analisador = {
    fftSize: 0,
    getFloatTimeDomainData: (buf: Float32Array) => buf.set(ContextoFalso.amostras.subarray(0, buf.length)),
    connect: vi.fn(),
  }
  destination = { destino: true }
  constructor() {
    ContextoFalso.instancias.push(this)
  }
  createAnalyser() {
    return this.analisador
  }
  createMediaStreamSource(stream: StreamFalso) {
    return {
      stream,
      connect: (no: unknown) => {
        this.ligacoes.push(no)
      },
    }
  }
  resume() {
    this.retomado++
    return Promise.resolve()
  }
  close() {
    this.fechado = true
    return Promise.resolve()
  }
}

let getUserMedia: ReturnType<typeof vi.fn>
let stream: StreamFalso

function instalar() {
  stream = new StreamFalso()
  getUserMedia = vi.fn(async () => stream)
  Object.defineProperty(navigator, 'mediaDevices', { value: { getUserMedia }, configurable: true })
  vi.stubGlobal('MediaRecorder', GravadorFalso)
  vi.stubGlobal('AudioContext', ContextoFalso)
}

beforeEach(() => {
  GravadorFalso.instancias = []
  GravadorFalso.suportados = new Set(['audio/webm;codecs=opus'])
  GravadorFalso.mandaStop = true
  ContextoFalso.instancias = []
  ContextoFalso.amostras = new Float32Array(1024)
  instalar()
})

afterEach(() => {
  Reflect.deleteProperty(navigator, 'mediaDevices')
  Reflect.deleteProperty(navigator, 'wakeLock')
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const abrir = () => plataformaDoNavegador().abrirMicrofone()

describe('suporta', () => {
  it('com getUserMedia, MediaRecorder e AudioContext', () => {
    expect(plataformaDoNavegador().suporta()).toBe(true)
  })

  it('sem getUserMedia (uma página sem HTTPS)', () => {
    Reflect.deleteProperty(navigator, 'mediaDevices')
    expect(plataformaDoNavegador().suporta()).toBe(false)
  })

  it('sem MediaRecorder', () => {
    vi.stubGlobal('MediaRecorder', undefined)
    expect(plataformaDoNavegador().suporta()).toBe(false)
  })

  it('sem AudioContext', () => {
    vi.stubGlobal('AudioContext', undefined)
    ;(window as unknown as { webkitAudioContext?: unknown }).webkitAudioContext = undefined
    expect(plataformaDoNavegador().suporta()).toBe(false)
  })

  it('abrir o microfone onde não há suporte falha sem pedir nada', async () => {
    Reflect.deleteProperty(navigator, 'mediaDevices')
    await expect(abrir()).rejects.toMatchObject({ motivo: 'sem-suporte' })
  })
})

describe('abrirMicrofone: o que é pedido ao navegador', () => {
  it('só o áudio, nos filtros do Chrome (nenhuma restrição), e o MediaRecorder em webm/opus a 32 kbit/s', async () => {
    await abrir()
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    expect(getUserMedia).toHaveBeenCalledWith({ audio: true })
    expect(GravadorFalso.instancias).toHaveLength(1)
    expect(GravadorFalso.instancias[0]!.args.opcoes).toEqual({ mimeType: 'audio/webm;codecs=opus', audioBitsPerSecond: 32_000 })
    expect(GravadorFalso.instancias[0]!.args.stream).toBe(stream)
  })

  it('o contexto de áudio nasce antes da permissão (no gesto do toque) e é retomado', async () => {
    let contextosAoPedir = -1
    getUserMedia.mockImplementationOnce(async () => {
      contextosAoPedir = ContextoFalso.instancias.length
      return stream
    })
    await abrir()
    expect(contextosAoPedir).toBe(1)
    expect(ContextoFalso.instancias[0]!.retomado).toBe(1)
  })

  it('o analisador lê o sinal e NÃO é ligado a nenhuma saída: nada toca', async () => {
    await abrir()
    const ctx = ContextoFalso.instancias[0]!
    expect(ctx.analisador.fftSize).toBe(1024)
    expect(ctx.ligacoes).toEqual([ctx.analisador])
    expect(ctx.ligacoes).not.toContain(ctx.destination)
  })

  it('o MediaRecorder só começa no comecar(), sem fatias: um pedaço só, ao parar', async () => {
    const mic = await abrir()
    expect(GravadorFalso.instancias[0]!.iniciou).toBe(0)
    mic.comecar()
    expect(GravadorFalso.instancias[0]!.iniciou).toBe(1)
    expect(GravadorFalso.instancias[0]!.argsDoStart).toEqual([])
  })
})

describe('nivel: o RMS do analisador', () => {
  it('uma senoide de amplitude 0,5 dá 0,354; o silêncio, 0', async () => {
    const mic = await abrir()
    expect(mic.nivel()).toBe(0)
    for (let i = 0; i < 1024; i++) ContextoFalso.amostras[i] = 0.5 * Math.sin((2 * Math.PI * i * 8) / 1024)
    expect(mic.nivel()).toBeCloseTo(0.5 / Math.SQRT2, 3)
  })
})

describe('parar: o microfone desliga na hora e o áudio chega inteiro', () => {
  it('para o gravador, desliga as faixas e fecha o contexto antes do último pedaço, e entrega um webm', async () => {
    const mic = await abrir()
    mic.comecar()
    GravadorFalso.instancias[0]!.entregar(new Blob([new Uint8Array([9, 9])]))
    const pronto = mic.parar()
    // Já desligado, antes de o stop chegar.
    expect(GravadorFalso.instancias[0]!.parou).toBe(1)
    expect(stream.faixas[0]!.paradas).toBe(1)
    expect(ContextoFalso.instancias[0]!.fechado).toBe(true)
    const blob = await pronto
    expect(blob.type).toBe('audio/webm')
    expect(blob.size).toBe(2 + 3)
  })

  it('o gravador que já parou sozinho (a faixa acabou) entrega o que juntou', async () => {
    const mic = await abrir()
    mic.comecar()
    const g = GravadorFalso.instancias[0]!
    g.entregar(new Blob([new Uint8Array([1, 2, 3, 4])]))
    g.state = 'inactive'
    const blob = await mic.parar()
    expect(blob.size).toBe(4)
    expect(g.parou).toBe(0)
    expect(stream.faixas[0]!.paradas).toBe(1)
  })

  it('o navegador que nunca manda o stop não prende o envio: em 3 s segue com o que chegou', async () => {
    GravadorFalso.mandaStop = false
    vi.useFakeTimers()
    const mic = await abrir()
    mic.comecar()
    GravadorFalso.instancias[0]!.entregar(new Blob([new Uint8Array([5, 5, 5])]))
    let blob: Blob | null = null
    void mic.parar().then((b) => (blob = b))
    await vi.advanceTimersByTimeAsync(2999)
    expect(blob).toBeNull()
    await vi.advanceTimersByTimeAsync(2)
    expect((blob as Blob | null)?.size).toBe(3)
  })

  it('os pedaços saem do microfone com o Blob: um segundo parar() não entrega o mesmo áudio', async () => {
    const mic = await abrir()
    mic.comecar()
    GravadorFalso.instancias[0]!.entregar(new Blob([new Uint8Array([1, 2, 3])]))
    const a = await mic.parar()
    const b = await mic.parar()
    expect(a.size).toBe(3 + 3)
    expect(b.size).toBe(0)
  })
})

describe('descartar', () => {
  it('desliga as faixas, para o gravador e joga o áudio fora', async () => {
    const mic = await abrir()
    mic.comecar()
    GravadorFalso.instancias[0]!.entregar(new Blob([new Uint8Array([1, 2, 3])]))
    mic.descartar()
    expect(stream.faixas[0]!.paradas).toBe(1)
    expect(GravadorFalso.instancias[0]!.parou).toBe(1)
    expect(ContextoFalso.instancias[0]!.fechado).toBe(true)
    const depois = await mic.parar()
    expect(depois.size).toBe(0)
  })

  it('duas vezes é como uma: a faixa para uma vez só', async () => {
    const mic = await abrir()
    mic.descartar()
    mic.descartar()
    expect(stream.faixas[0]!.paradas).toBe(1)
  })

  it('o gravador que levanta ao parar (já parou) não impede o desligamento', async () => {
    const mic = await abrir()
    mic.comecar()
    GravadorFalso.instancias[0]!.stop = () => {
      throw new DOMException('já parou', 'InvalidStateError')
    }
    expect(() => mic.descartar()).not.toThrow()
    expect(stream.faixas[0]!.paradas).toBe(1)
  })
})

describe('aoInterromper: o mute e o ended da faixa', () => {
  it('uma ligação recebida (mute) e a faixa que acaba (ended) avisam, e o ouvinte sai', async () => {
    const mic = await abrir()
    const cb = vi.fn()
    const soltar = mic.aoInterromper(cb)
    stream.faixas[0]!.dispatchEvent(new Event('mute'))
    stream.faixas[0]!.dispatchEvent(new Event('ended'))
    expect(cb.mock.calls).toEqual([['mudo'], ['acabou']])
    soltar()
    stream.faixas[0]!.dispatchEvent(new Event('mute'))
    expect(cb).toHaveBeenCalledTimes(2)
  })

  it('parar as faixas não conta como interrupção', async () => {
    const mic = await abrir()
    const cb = vi.fn()
    mic.aoInterromper(cb)
    mic.comecar()
    await mic.parar()
    expect(cb).not.toHaveBeenCalled()
  })
})

describe('quando algo falha, nada fica aberto', () => {
  it('a permissão negada fecha o contexto e repassa o erro do navegador', async () => {
    const negado = Object.assign(new Error('x'), { name: 'NotAllowedError' })
    getUserMedia.mockRejectedValueOnce(negado)
    await expect(abrir()).rejects.toBe(negado)
    expect(ContextoFalso.instancias[0]!.fechado).toBe(true)
    expect(GravadorFalso.instancias).toHaveLength(0)
  })

  it('o navegador sem webm/opus desliga o microfone que acabou de abrir', async () => {
    GravadorFalso.suportados = new Set()
    const erro = await abrir().catch((e: unknown) => e)
    expect(erro).toBeInstanceOf(MicrofoneIndisponivel)
    expect((erro as MicrofoneIndisponivel).motivo).toBe('sem-formato')
    expect(stream.faixas[0]!.paradas).toBe(1)
    expect(ContextoFalso.instancias[0]!.fechado).toBe(true)
  })

  it('o analisador que não monta desliga o microfone', async () => {
    const original = ContextoFalso.prototype.createMediaStreamSource
    ContextoFalso.prototype.createMediaStreamSource = () => {
      throw new Error('x')
    }
    try {
      await expect(abrir()).rejects.toThrow('x')
      expect(stream.faixas[0]!.paradas).toBe(1)
    } finally {
      ContextoFalso.prototype.createMediaStreamSource = original
    }
  })

  it('o MediaRecorder que levanta ao ser criado desliga o microfone', async () => {
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        function () {
          throw new Error('x')
        },
        { isTypeSupported: () => true },
      ),
    )
    await expect(abrir()).rejects.toThrow('x')
    expect(stream.faixas[0]!.paradas).toBe(1)
  })
})

describe('pedirTela: o Wake Lock', () => {
  it('sem navigator.wakeLock, não vem', async () => {
    expect(await plataformaDoNavegador().pedirTela()).toBeNull()
  })

  it('o pedido de screen vem, e soltar libera', async () => {
    const release = vi.fn(async () => {})
    const request = vi.fn(async () => ({ release }))
    Object.defineProperty(navigator, 'wakeLock', { value: { request }, configurable: true })
    const tela = await plataformaDoNavegador().pedirTela()
    expect(request).toHaveBeenCalledWith('screen')
    tela!.soltar()
    expect(release).toHaveBeenCalledTimes(1)
  })

  it('o pedido recusado (sem HTTPS, página fora da frente) é null, sem levantar', async () => {
    Object.defineProperty(navigator, 'wakeLock', { value: { request: async () => Promise.reject(new DOMException('x', 'NotAllowedError')) }, configurable: true })
    expect(await plataformaDoNavegador().pedirTela()).toBeNull()
  })

  it('soltar com o release que falha não levanta', async () => {
    Object.defineProperty(navigator, 'wakeLock', { value: { request: async () => ({ release: () => Promise.reject(new Error('x')) }) }, configurable: true })
    const tela = await plataformaDoNavegador().pedirTela()
    expect(() => tela!.soltar()).not.toThrow()
  })
})

describe('o resto da Plataforma', () => {
  it('aoSairDaFrente só avisa quando a página fica escondida, e o ouvinte sai', () => {
    const cb = vi.fn()
    const soltar = plataformaDoNavegador().aoSairDaFrente(cb)
    const estado = vi.spyOn(document, 'visibilityState', 'get')
    estado.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(cb).not.toHaveBeenCalled()
    estado.mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(cb).toHaveBeenCalledTimes(1)
    soltar()
    document.dispatchEvent(new Event('visibilitychange'))
    expect(cb).toHaveBeenCalledTimes(1)
    estado.mockRestore()
  })

  it('o id é um UUID v7, e cada chamada dá um', () => {
    const p = plataformaDoNavegador()
    const a = p.novoId()
    expect(a).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(p.novoId()).not.toBe(a)
  })

  it('depois() e quadro() agendam e cancelam', () => {
    vi.useFakeTimers()
    const p = plataformaDoNavegador()
    const cb = vi.fn()
    const cancela = p.depois(1000, cb)
    vi.advanceTimersByTime(999)
    expect(cb).not.toHaveBeenCalled()
    vi.advanceTimersByTime(2)
    expect(cb).toHaveBeenCalledTimes(1)
    const outro = vi.fn()
    p.depois(1000, outro)()
    cancela()
    vi.advanceTimersByTime(5000)
    expect(outro).not.toHaveBeenCalled()

    const q = vi.fn()
    p.quadro(q)
    vi.advanceTimersByTime(40)
    expect(q).toHaveBeenCalledTimes(1)
    const q2 = vi.fn()
    p.quadro(q2)()
    vi.advanceTimersByTime(40)
    expect(q2).not.toHaveBeenCalled()
  })

  it('agora() é monotônico', () => {
    const p = plataformaDoNavegador()
    const a = p.agora()
    expect(p.agora()).toBeGreaterThanOrEqual(a)
  })
})

describe('o microfone nunca é tocado fora dos testes', () => {
  it('este arquivo só usa dublês: o getUserMedia chamado é o do vi.fn', async () => {
    const mic: Microfone = await abrir()
    expect(vi.isMockFunction(getUserMedia)).toBe(true)
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    mic.descartar()
  })
})
