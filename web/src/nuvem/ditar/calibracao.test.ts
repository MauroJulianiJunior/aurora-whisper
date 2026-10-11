// A calibração do microfone (calibracao.ts; ADR-021, C6c): a aritmética da
// medição (quantis em dBFS, as bandas, o tempo acima de cada limiar, a fala
// medida, os bytes por segundo), a captura de verdade contra dublês dos globais
// do navegador (as MESMAS restrições do getUserMedia e a MESMA medição do
// gravador), e a prova de que o código só mostra números: nada de rede,
// arquivo, armazenamento ou cópia. Nenhum teste abre o microfone de verdade.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import entradaTsx from './calibracaoDev.tsx?raw'
import paginaTsx from './PaginaDeCalibracao.tsx?raw'
import calibracaoTs from './calibracao.ts?raw'
import {
  Medicao,
  abrirCapturaDoNavegador,
  dbfsDe,
  dbfsMinimo,
  doApp,
  formatarDB,
  formatarInteiro,
  formatarPct,
  formatarRMS,
  formatarSeg,
  limiaresMedidos,
  pedacoDaCalibracaoMs,
} from './calibracao'
import { MedidorDeFala, bitsPorSegundo, falaMinimaMs, limiarDeFala, tipoDoAudio } from './medidas'
import { plataformaDoNavegador, restricoesDoMicrofone } from './navegador'

// --- a aritmética ---------------------------------------------------------

// quadros alimenta a medição com `n` leituras de 16 ms, o nível dado por quadro.
function alimentar(m: Medicao, n: number, nivel: (i: number) => number, t0 = 1000): number {
  let t = t0
  for (let i = 0; i < n; i++) {
    m.amostrar(t, nivel(i))
    t += 16
  }
  return t
}

describe('dBFS', () => {
  it('é 20·log10 do RMS: 1 é 0 dBFS, 0,1 é -20, 0,01 é -40', () => {
    expect(dbfsDe(1)).toBeCloseTo(0, 6)
    expect(dbfsDe(0.1)).toBeCloseTo(-20, 6)
    expect(dbfsDe(0.01)).toBeCloseTo(-40, 6)
  })

  it('o silêncio perfeito não é -∞: vale o piso de -120, e o que passa de 1 vale 0', () => {
    expect(dbfsDe(0)).toBe(dbfsMinimo)
    expect(dbfsDe(1e-9)).toBe(dbfsMinimo)
    expect(dbfsDe(-0.5)).toBe(dbfsMinimo)
    expect(dbfsDe(NaN)).toBe(dbfsMinimo)
    expect(dbfsDe(3)).toBe(0)
  })
})

describe('os quantis', () => {
  it('um nível só: todos os percentis são ele, em RMS e em dBFS', () => {
    const m = new Medicao()
    alimentar(m, 100, () => 0.01)
    const r = m.resumo()
    expect(r.quadros).toBe(100)
    for (const q of r.quantis) {
      expect(q.rms).toBe(0.01)
      expect(q.dbfs).toBeCloseTo(-40, 6)
    }
    expect(r.quantis.map((q) => q.nome)).toEqual(['mínimo', 'p5 (o piso)', 'p10', 'p25', 'mediana', 'p75', 'p90', 'p95', 'p99', 'máximo (o pico)'])
  })

  it('por posição (nearest-rank): 1 a 100 dá p5 = 5, mediana = 50, p95 = 95, máximo = 100', () => {
    const m = new Medicao()
    // Embaralhado de propósito: a ordem de chegada não conta.
    const valores = Array.from({ length: 100 }, (_, i) => (i + 1) / 1000)
    const embaralhado = valores.map((_, i) => valores[(i * 37) % 100]!)
    alimentar(m, 100, (i) => embaralhado[i]!)
    const por = Object.fromEntries(m.resumo().quantis.map((q) => [q.p, q.rms]))
    expect(por[0]).toBe(0.001)
    expect(por[5]).toBe(0.005)
    expect(por[50]).toBe(0.05)
    expect(por[95]).toBe(0.095)
    expect(por[100]).toBe(0.1)
  })

  it('sem nenhum quadro: tudo zero, sem divisão por zero', () => {
    const r = new Medicao().resumo()
    expect(r).toMatchObject({ quadros: 0, duracaoMs: 0, falaMedidaMs: 0, enviaria: false })
    expect(r.quantis.every((q) => q.rms === 0 && q.dbfs === dbfsMinimo)).toBe(true)
    expect(r.bandas.every((b) => b.quadros === 0 && b.fracao === 0)).toBe(true)
    expect(r.bytes).toMatchObject({ pedacos: 0, total: 0, porSegundo: 0, menor: 0, maior: 0 })
  })
})

describe('a distribuição do nível, em bandas de 6 dB', () => {
  it('uma banda "abaixo de -96", e 16 de -96 a 0; cada quadro cai numa só; as frações somam 1', () => {
    const m = new Medicao()
    // -100 dBFS (abaixo), -40 dBFS, -40, -3 dBFS, 0 dBFS (a banda de cima).
    for (const db of [-100, -40, -40, -3, 0]) m.amostrar(1000 + db * -1, 10 ** (db / 20))
    const { bandas } = m.resumo()
    expect(bandas).toHaveLength(17)
    expect(bandas[0]).toMatchObject({ de: -Infinity, ate: -96, quadros: 1 })
    expect(bandas[1]).toMatchObject({ de: -96, ate: -90 })
    expect(bandas[16]).toMatchObject({ de: -6, ate: 0, quadros: 2 })
    // -40 dBFS está em [-42, -36).
    const dos40 = bandas.find((b) => b.de === -42)!
    expect(dos40).toMatchObject({ ate: -36, quadros: 2 })
    expect(bandas.reduce((s, b) => s + b.quadros, 0)).toBe(5)
    expect(bandas.reduce((s, b) => s + b.fracao, 0)).toBeCloseTo(1, 9)
  })

  it('a borda é fechada embaixo e aberta em cima: -90 dBFS cai em [-90, -84)', () => {
    const m = new Medicao()
    m.amostrar(1000, 10 ** (-90 / 20) * 1.0000001)
    const banda = m.resumo().bandas.find((b) => b.quadros === 1)!
    expect([banda.de, banda.ate]).toEqual([-90, -84])
  })
})

describe('o tempo acima de cada limiar', () => {
  it('soma o intervalo entre quadros em que o nível passa do limiar, e o limiar do app vem marcado', () => {
    const m = new Medicao()
    // 1 s a 0,015 (passa de 0,003, 0,005 e 0,01), depois 1 s a 0,06 (passa até 0,05).
    const t = alimentar(m, 63, () => 0.015)
    alimentar(m, 63, () => 0.06, t)
    const por = Object.fromEntries(m.resumo().acimaDe.map((a) => [a.limiar, a]))
    expect(Object.keys(por).map(Number)).toEqual([...limiaresMedidos])
    expect(por[0.01]!.ms).toBeGreaterThan(1900)
    expect(por[0.03]!.ms).toBeGreaterThan(900)
    expect(por[0.03]!.ms).toBeLessThan(1010)
    expect(por[0.1]!.ms).toBe(0)
    expect(por[0.03]!.doApp).toBe(true)
    expect(Object.values(por).filter((a) => a.doApp)).toHaveLength(1)
    expect(por[0.03]!.fracao).toBeCloseTo(por[0.03]!.ms / m.resumo().duracaoMs, 6)
  })

  it('um quadro depois de uma pausa longa (a aba congelada) conta no máximo 100 ms', () => {
    const m = new Medicao()
    m.amostrar(1000, 0.5)
    m.amostrar(11_000, 0.5)
    expect(m.resumo().acimaDe.find((a) => a.limiar === 0.2)!.ms).toBe(100)
  })

  it('o limiar do app é o do gravador', () => {
    expect(limiaresMedidos).toContain(limiarDeFala)
    expect(doApp.limiarDeFala).toBe(limiarDeFala)
  })
})

describe('a fala medida: a mesma conta do gravador', () => {
  it('dá o que o MedidorDeFala do app daria para os mesmos quadros, sem os últimos 200 ms', () => {
    const m = new Medicao()
    const medidor = new MedidorDeFala()
    let t = 1000
    // 300 ms de fala, 400 de silêncio, 500 de fala, e o "toque" no fim.
    const niveis = [...Array(19).fill(0.1), ...Array(25).fill(0.002), ...Array(31).fill(0.1)]
    for (const rms of niveis) {
      m.amostrar(t, rms)
      medidor.amostrar(t, rms)
      t += 16
    }
    const r = m.resumo()
    expect(r.falaMedidaMs).toBe(medidor.falaMs(t - 16))
    // 18 quadros de fala no começo e 18 antes dos últimos 200 ms do fim.
    expect(r.falaMedidaMs).toBeGreaterThan(500)
    expect(r.falaMedidaMs).toBeLessThan(600)
    expect(r.enviaria).toBe(true)
  })

  it('menos de 300 ms de fala: o app não enviaria', () => {
    const m = new Medicao()
    alimentar(m, 10, () => 0.1)
    alimentar(m, 60, () => 0.002, 1000 + 160)
    expect(m.resumo().falaMedidaMs).toBeLessThan(falaMinimaMs)
    expect(m.resumo().enviaria).toBe(false)
  })
})

describe('os bytes por segundo do MediaRecorder', () => {
  it('soma só os tamanhos: o total, o menor e o maior pedaço, e o ritmo', () => {
    const m = new Medicao()
    alimentar(m, 313, () => 0.01) // ~5 s
    for (const b of [3900, 4100, 4000, 4050, 3950]) m.pedaco(b)
    m.pedaco(0) // um pedaço vazio não conta
    const { bytes, duracaoMs } = m.resumo()
    expect(bytes.pedacos).toBe(5)
    expect(bytes.total).toBe(20_000)
    expect([bytes.menor, bytes.maior]).toEqual([3900, 4100])
    expect(bytes.porSegundo).toBeCloseTo(20_000 / (duracaoMs / 1000), 6)
    expect(bytes.kbitPorSegundo).toBeCloseTo((bytes.porSegundo * 8) / 1000, 6)
    expect(bytes.projetado120).toBeCloseTo(bytes.porSegundo * 120, 6)
  })

  it('o ritmo certo (32 kbit/s = 4000 bytes/s) cabe em 120 s abaixo de 1 MiB', () => {
    const m = new Medicao()
    alimentar(m, 626, () => 0.01) // ~10 s
    const { duracaoMs } = m.resumo()
    m.pedaco(Math.round(4000 * (duracaoMs / 1000)))
    const { bytes } = m.resumo()
    expect(bytes.kbitPorSegundo).toBeCloseTo(32, 0)
    expect(bytes.projetado120).toBeLessThan(doApp.tetoDoCorpoBytes)
    expect(bitsPorSegundo).toBe(32_000)
  })

  it('a janela vai até o fim pedido, e não só até o último quadro', () => {
    const m = new Medicao()
    alimentar(m, 10, () => 0.01)
    m.pedaco(1000)
    const curta = m.resumo()
    const longa = m.resumo(1000 + 5000)
    expect(longa.duracaoMs).toBeGreaterThan(curta.duracaoMs + 4000)
    expect(longa.bytes.porSegundo).toBeLessThan(curta.bytes.porSegundo)
  })
})

describe('como os números aparecem', () => {
  it('em português, com vírgula', () => {
    expect(formatarRMS(0.03)).toBe('0,0300')
    expect(formatarDB(-48.04)).toBe('-48,0')
    expect(formatarPct(0.1234)).toBe('12,3%')
    expect(formatarSeg(6000)).toBe('6,0 s')
    expect(formatarInteiro(1234567)).toBe('1.234.567')
  })

  it('o que está no piso aparece como "≤ -120", e não como -∞', () => {
    expect(formatarDB(dbfsMinimo)).toBe('≤ -120')
    expect(formatarDB(dbfsDe(0))).toBe('≤ -120')
  })
})

// --- a captura de verdade, contra dublês do navegador ----------------------

class FaixaFalsa extends EventTarget {
  paradas = 0
  stop() {
    this.paradas++
  }
  getSettings(): MediaTrackSettings {
    return { echoCancellation: true, noiseSuppression: true, autoGainControl: false, channelCount: 1, sampleRate: 48_000 }
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
  static isTypeSupported(tipo: string): boolean {
    return tipo === 'audio/webm;codecs=opus'
  }
  static mandaStop = true
  state: 'inactive' | 'recording' = 'inactive'
  mimeType: string
  audioBitsPerSecond = 32_000
  parou = 0
  argsDoStart: unknown[] = []
  args: { stream: StreamFalso; opcoes: MediaRecorderOptions }
  constructor(stream: StreamFalso, opcoes: MediaRecorderOptions) {
    super()
    this.args = { stream, opcoes }
    this.mimeType = opcoes.mimeType ?? ''
    GravadorFalso.instancias.push(this)
  }
  start(...args: unknown[]) {
    this.argsDoStart = args
    this.state = 'recording'
  }
  stop() {
    this.parou++
    this.state = 'inactive'
    if (!GravadorFalso.mandaStop) return
    queueMicrotask(() => {
      this.entregar(512)
      this.dispatchEvent(new Event('stop'))
    })
  }
  entregar(bytes: number) {
    const e = new Event('dataavailable') as Event & { data: Blob }
    e.data = new Blob([new Uint8Array(bytes)])
    this.dispatchEvent(e)
  }
}

class ContextoFalso {
  static instancias: ContextoFalso[] = []
  static amostras = new Float32Array(1024)
  sampleRate = 48_000
  fechado = false
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
  createMediaStreamSource() {
    return { connect: (no: unknown) => void this.ligacoes.push(no) }
  }
  resume() {
    return Promise.resolve()
  }
  close() {
    this.fechado = true
    return Promise.resolve()
  }
}

let getUserMedia: ReturnType<typeof vi.fn>
let stream: StreamFalso

beforeEach(() => {
  stream = new StreamFalso()
  getUserMedia = vi.fn(async () => stream)
  Object.defineProperty(navigator, 'mediaDevices', { value: { getUserMedia }, configurable: true })
  GravadorFalso.instancias = []
  GravadorFalso.mandaStop = true
  ContextoFalso.instancias = []
  ContextoFalso.amostras = new Float32Array(1024)
  vi.stubGlobal('MediaRecorder', GravadorFalso)
  vi.stubGlobal('AudioContext', ContextoFalso)
})

afterEach(() => {
  Reflect.deleteProperty(navigator, 'mediaDevices')
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('abrirCapturaDoNavegador: a mesma captura do gravador', () => {
  it('pede o microfone com as MESMAS restrições do gravador (o mesmo objeto), e só o áudio', async () => {
    await abrirCapturaDoNavegador()
    expect(getUserMedia).toHaveBeenCalledTimes(1)
    const daCalibracao = getUserMedia.mock.calls[0]![0]
    expect(daCalibracao).toBe(restricoesDoMicrofone)
    expect(daCalibracao).toEqual({ audio: true })

    getUserMedia.mockClear()
    await plataformaDoNavegador().abrirMicrofone()
    expect(getUserMedia.mock.calls[0]![0]).toBe(daCalibracao)
  })

  it('o MediaRecorder grava no mesmo formato e bitrate do gravador, em pedaços de 1 s', async () => {
    await abrirCapturaDoNavegador()
    const [g] = GravadorFalso.instancias
    expect(g!.args.opcoes).toEqual({ mimeType: tipoDoAudio, audioBitsPerSecond: bitsPorSegundo })
    expect(g!.args.stream).toBe(stream)
    expect(g!.argsDoStart).toEqual([pedacoDaCalibracaoMs])
    expect(pedacoDaCalibracaoMs).toBe(1000)
  })

  it('o analisador tem a janela do gravador e NÃO é ligado a nenhuma saída: nada toca', async () => {
    await abrirCapturaDoNavegador()
    const ctx = ContextoFalso.instancias[0]!
    expect(ctx.analisador.fftSize).toBe(doApp.janelaDoAnalisador)
    expect(ctx.ligacoes).toEqual([ctx.analisador])
    expect(ctx.ligacoes).not.toContain(ctx.destination)
  })

  it('o nível é o mesmo número que o do gravador para o mesmo sinal', async () => {
    for (let i = 0; i < 1024; i++) ContextoFalso.amostras[i] = 0.3 * Math.sin((2 * Math.PI * i * 11) / 1024) + 0.05
    const daCalibracao = (await abrirCapturaDoNavegador()).nivel()
    const doGravador = (await plataformaDoNavegador().abrirMicrofone()).nivel()
    expect(daCalibracao).toBeGreaterThan(0.2)
    expect(daCalibracao).toBe(doGravador)
  })

  it('diz o que o navegador aplicou: filtros, taxa, canais, tipo e bitrate', async () => {
    const { info } = await abrirCapturaDoNavegador()
    expect(info).toEqual({
      taxaDoContexto: 48_000,
      eco: true,
      ruido: true,
      ganho: false,
      canais: 1,
      taxaDaFaixa: 48_000,
      tipoDoRecorder: tipoDoAudio,
      bitsPedidos: 32_000,
      bitsDoRecorder: 32_000,
    })
  })

  it('o navegador que não diz os ajustes da faixa: "não informado" (null), sem quebrar', async () => {
    stream.faixas[0]!.getSettings = () => ({})
    const { info } = await abrirCapturaDoNavegador()
    expect([info.eco, info.ruido, info.ganho, info.canais, info.taxaDaFaixa]).toEqual([null, null, null, null, null])
  })

  it('os pedaços entregam só o tamanho (um número), e quem deixou de ouvir não recebe mais', async () => {
    const captura = await abrirCapturaDoNavegador()
    const recebidos: unknown[] = []
    const soltar = captura.aoPedaco((b) => recebidos.push(b))
    const g = GravadorFalso.instancias[0]!
    g.entregar(3900)
    g.entregar(4100)
    soltar()
    g.entregar(5000)
    expect(recebidos).toEqual([3900, 4100])
    expect(recebidos.every((b) => typeof b === 'number')).toBe(true)
  })

  it('parar desliga o microfone na hora, conta o último pedaço e fecha o contexto', async () => {
    const captura = await abrirCapturaDoNavegador()
    const tamanhos: number[] = []
    captura.aoPedaco((b) => tamanhos.push(b))
    const pronto = captura.parar()
    expect(GravadorFalso.instancias[0]!.parou).toBe(1)
    expect(stream.faixas[0]!.paradas).toBe(1)
    expect(ContextoFalso.instancias[0]!.fechado).toBe(true)
    await pronto
    expect(tamanhos).toEqual([512])
  })

  it('se o stop nunca vem, a página não trava: segue em 1 s', async () => {
    vi.useFakeTimers()
    GravadorFalso.mandaStop = false
    const captura = await abrirCapturaDoNavegador()
    let acabou = false
    void captura.parar().then(() => {
      acabou = true
    })
    await vi.advanceTimersByTimeAsync(999)
    expect(acabou).toBe(false)
    await vi.advanceTimersByTimeAsync(2)
    expect(acabou).toBe(true)
  })

  it('o formato que o navegador não grava fecha o microfone antes de sair', async () => {
    vi.stubGlobal('MediaRecorder', class extends GravadorFalso {
      static override isTypeSupported() {
        return false
      }
    })
    await expect(abrirCapturaDoNavegador()).rejects.toMatchObject({ motivo: 'sem-formato' })
    expect(stream.faixas[0]!.paradas).toBe(1)
    expect(ContextoFalso.instancias[0]!.fechado).toBe(true)
  })

  it('a permissão negada fecha o contexto e deixa o erro do navegador subir', async () => {
    getUserMedia.mockRejectedValueOnce(Object.assign(new Error('x'), { name: 'NotAllowedError' }))
    await expect(abrirCapturaDoNavegador()).rejects.toMatchObject({ name: 'NotAllowedError' })
    expect(ContextoFalso.instancias[0]!.fechado).toBe(true)
  })

  it('onde não há suporte, falha sem pedir nada', async () => {
    Reflect.deleteProperty(navigator, 'mediaDevices')
    await expect(abrirCapturaDoNavegador()).rejects.toMatchObject({ motivo: 'sem-suporte' })
  })
})

// --- só números -------------------------------------------------------------

describe('só números: nenhum arquivo, nenhum envio', () => {
  // O texto de cada arquivo da calibração, como o Vite o lê (?raw).
  const fontes = [
    ['calibracao.ts', calibracaoTs],
    ['PaginaDeCalibracao.tsx', paginaTsx],
    ['calibracaoDev.tsx', entradaTsx],
  ] as const

  it('são três arquivos diferentes, e não o mesmo lido três vezes', () => {
    expect(new Set(fontes.map(([, texto]) => texto)).size).toBe(3)
    expect(fontes[1]![1]).toContain('export function PaginaDeCalibracao')
    expect(fontes[2]![1]).toContain('createRoot(')
  })

  // O código, sem os comentários: o cabeçalho cita o que a página NÃO faz.
  const codigo = (texto: string) => texto.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '')

  // Rede, armazenamento, arquivo, cópia, download, som.
  const proibidos = [
    'fetch(',
    'XMLHttpRequest',
    'sendBeacon',
    'WebSocket',
    'EventSource',
    'localStorage',
    'sessionStorage',
    'indexedDB',
    'caches.',
    'navigator.storage',
    'serviceWorker',
    'new Blob',
    'createObjectURL',
    'FileReader',
    'showSaveFilePicker',
    'showOpenFilePicker',
    'FileSystem',
    'download',
    'navigator.clipboard',
    'document.execCommand',
    'new Audio(',
    'AudioWorklet',
    'audioWorklet',
    '.destination',
    'window.open',
    'postMessage',
  ]

  it.each(fontes)('%s não tem nada de rede, armazenamento, arquivo, cópia nem som', (_, texto) => {
    const c = codigo(texto)
    for (const p of proibidos) expect(c, p).not.toContain(p)
  })

  it('a página só importa os módulos de medição do próprio app, React e os estilos', () => {
    for (const [nome, texto] of fontes) {
      const importados = [...texto.matchAll(/^import\s[^'"]*['"]([^'"]+)['"]/gm), ...texto.matchAll(/^import\s+['"]([^'"]+)['"]/gm)].map((m) => m[1]!)
      for (const origem of importados) {
        expect(origem, `${nome} importa ${origem}`).toMatch(/^(react|react-dom\/client|\.\.?\/[\w./-]+)$/)
      }
    }
  })

  it('abrir e medir não faz nenhum pedido de rede nem toca o armazenamento', async () => {
    const rede = vi.fn()
    vi.stubGlobal('fetch', rede)
    const guardou = vi.spyOn(Storage.prototype, 'setItem')
    const captura = await abrirCapturaDoNavegador()
    captura.aoPedaco(() => undefined)
    GravadorFalso.instancias[0]!.entregar(4000)
    captura.nivel()
    await captura.parar()
    expect(rede).not.toHaveBeenCalled()
    expect(guardou).not.toHaveBeenCalled()
  })
})
