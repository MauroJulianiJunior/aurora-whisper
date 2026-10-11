// A calibração do microfone no Android (ADR-021, C6c): a medição que a página
// de calibração (PaginaDeCalibracao.tsx, só do `pnpm dev`) faz no próprio aparelho para
// calibrar o limiar de fala, a onda e o bitrate com os números do celular, e
// não com os do PC.
//
// MOSTRA SÓ NÚMEROS. Nada daqui grava arquivo, guarda áudio, baixa, copia ou
// envia: cada quadro vira um nível (RMS), cada pedaço do MediaRecorder vira um
// tamanho em bytes, e o áudio é jogado fora na hora (o Blob do pedaço nem é
// guardado). calibracao.test.ts lê o código-fonte e recusa qualquer coisa de
// rede, armazenamento ou arquivo; bundle.test.ts prova que a página não está em
// nenhum dos dois builds.
//
// É a MESMA medição do gravador: o microfone abre com as mesmas restrições do
// getUserMedia (restricoesDoMicrofone), o nível sai do mesmo AnalyserNode
// (rmsDoAnalisador, a mesma janela), a fala é somada pelo mesmo MedidorDeFala
// (o limiar e os 200 ms do fim), e o MediaRecorder grava no mesmo formato e no
// mesmo bitrate.

import { MicrofoneIndisponivel } from './erros'
import {
  MedidorDeFala,
  bitsPorSegundo,
  fimDescartadoMs,
  janelaDaOndaMs,
  limiarDeFala,
  maiorQuadroMs,
  pisoDBFS,
  tetoDBFS,
  tetoMs,
  tipoDoAudio,
  falaMinimaMs,
} from './medidas'
import { construtorDoContexto, janelaDoAnalisador, restricoesDoMicrofone, rmsDoAnalisador, suporta } from './navegador'

// Os números que o app usa hoje, para a pessoa ler os da medição ao lado deles.
export const doApp = {
  limiarDeFala,
  falaMinimaMs,
  fimDescartadoMs,
  pisoDBFS,
  tetoDBFS,
  janelaDaOndaMs,
  bitsPorSegundo,
  tipoDoAudio,
  tetoMs,
  janelaDoAnalisador,
  // O teto do corpo no servidor (ADR-021, "Limites"): 1 MiB.
  tetoDoCorpoBytes: 1024 * 1024,
} as const

// Os limiares de RMS em que se soma o tempo acima. O do app (limiarDeFala) está
// entre eles, e a página o marca.
export const limiaresMedidos: readonly number[] = [0.003, 0.005, 0.01, 0.02, limiarDeFala, 0.05, 0.1, 0.2]

// O piso do dBFS: um RMS zero (ou menor que isto) vale -120 dBFS, em vez de -∞.
export const dbfsMinimo = -120

// As bandas da distribuição do nível: de 6 em 6 dB, de -96 a 0 dBFS, mais uma
// "abaixo" para o que fica de fora.
export const larguraDaBandaDB = 6
export const inicioDasBandasDB = -96

export function dbfsDe(rms: number): number {
  if (!(rms > 0)) return dbfsMinimo
  return Math.max(dbfsMinimo, 20 * Math.log10(Math.min(rms, 1)))
}

export interface Quantil {
  nome: string
  // p é o percentil, de 0 a 100 (0 é o mínimo; 100, o máximo).
  p: number
  rms: number
  dbfs: number
}

export interface Banda {
  // de e ate: os dBFS da banda ([de, ate)); a primeira, "abaixo", tem de = -Infinity.
  de: number
  ate: number
  quadros: number
  fracao: number
}

export interface AcimaDe {
  limiar: number
  ms: number
  fracao: number
  // doApp: é o limiar que o app usa hoje.
  doApp: boolean
}

export interface Bytes {
  pedacos: number
  total: number
  menor: number
  maior: number
  porSegundo: number
  kbitPorSegundo: number
  // projetado120: o que 120 s ocupariam no mesmo ritmo, em bytes; o servidor
  // aceita até doApp.tetoDoCorpoBytes.
  projetado120: number
}

export interface Resumo {
  duracaoMs: number
  quadros: number
  quantis: Quantil[]
  bandas: Banda[]
  acimaDe: AcimaDe[]
  // falaMedidaMs: a fala que o MedidorDeFala do app mediria nesta janela, sem os
  // últimos 200 ms; enviaria: se passaria dos 300 ms que o app exige.
  falaMedidaMs: number
  enviaria: boolean
  bytes: Bytes
}

// quantil por posição (nearest-rank): o menor valor que cobre p% dos quadros.
function quantilDe(ordenado: readonly number[], p: number): number {
  if (ordenado.length === 0) return 0
  const i = p <= 0 ? 0 : Math.min(ordenado.length - 1, Math.ceil((p / 100) * ordenado.length) - 1)
  return ordenado[i]!
}

const percentis: [string, number][] = [
  ['mínimo', 0],
  ['p5 (o piso)', 5],
  ['p10', 10],
  ['p25', 25],
  ['mediana', 50],
  ['p75', 75],
  ['p90', 90],
  ['p95', 95],
  ['p99', 99],
  ['máximo (o pico)', 100],
]

// Medicao soma os quadros e os pedaços de UMA janela de medição (o silêncio, ou
// a fala). Pura: sem navegador, sem relógio próprio.
export class Medicao {
  private niveis: number[] = []
  private anterior: number | null = null
  private inicio: number | null = null
  private fim = 0
  private medidor = new MedidorDeFala()
  private acima = limiaresMedidos.map(() => 0)
  private tamanhos: number[] = []

  // amostrar: o nível (RMS, de 0 a 1) lido no instante t (ms, monotônico).
  amostrar(t: number, rms: number): void {
    if (this.inicio === null) this.inicio = t
    if (this.anterior !== null && t > this.anterior) {
      const dt = Math.min(t - this.anterior, maiorQuadroMs)
      limiaresMedidos.forEach((limiar, i) => {
        if (rms >= limiar) this.acima[i]! += dt
      })
    }
    this.medidor.amostrar(t, rms)
    this.anterior = Math.max(t, this.anterior ?? t)
    this.fim = Math.max(this.fim, t)
    this.niveis.push(rms)
  }

  // pedaco: um pedaço do MediaRecorder chegou. Só o tamanho entra aqui: o
  // áudio já foi jogado fora por quem o recebeu.
  pedaco(bytes: number): void {
    if (bytes > 0) this.tamanhos.push(bytes)
  }

  resumo(fimDaJanela?: number): Resumo {
    const fim = Math.max(this.fim, fimDaJanela ?? this.fim)
    const duracaoMs = this.inicio === null ? 0 : Math.max(0, fim - this.inicio)
    const ordenado = [...this.niveis].sort((a, b) => a - b)
    const quadros = ordenado.length

    const quantis = percentis.map(([nome, p]) => {
      const rms = quantilDe(ordenado, p)
      return { nome, p, rms, dbfs: dbfsDe(rms) }
    })

    // A distribuição do nível, em bandas de 6 dB.
    const nBandas = Math.round(-inicioDasBandasDB / larguraDaBandaDB)
    const contagem = new Array<number>(nBandas + 1).fill(0)
    for (const rms of ordenado) {
      const db = dbfsDe(rms)
      const i = db < inicioDasBandasDB ? 0 : Math.min(nBandas - 1, Math.floor((db - inicioDasBandasDB) / larguraDaBandaDB)) + 1
      contagem[i]!++
    }
    const bandas: Banda[] = contagem.map((n, i) => ({
      de: i === 0 ? -Infinity : inicioDasBandasDB + (i - 1) * larguraDaBandaDB,
      ate: i === 0 ? inicioDasBandasDB : inicioDasBandasDB + i * larguraDaBandaDB,
      quadros: n,
      fracao: quadros > 0 ? n / quadros : 0,
    }))

    const total = this.tamanhos.reduce((s, n) => s + n, 0)
    const segundos = duracaoMs / 1000
    const porSegundo = segundos > 0 ? total / segundos : 0
    const falaMedidaMs = this.medidor.falaMs(fim)
    return {
      duracaoMs,
      quadros,
      quantis,
      bandas,
      acimaDe: limiaresMedidos.map((limiar, i) => ({
        limiar,
        ms: Math.round(this.acima[i]!),
        fracao: duracaoMs > 0 ? this.acima[i]! / duracaoMs : 0,
        doApp: limiar === limiarDeFala,
      })),
      falaMedidaMs,
      enviaria: falaMedidaMs >= falaMinimaMs,
      bytes: {
        pedacos: this.tamanhos.length,
        total,
        menor: this.tamanhos.length > 0 ? Math.min(...this.tamanhos) : 0,
        maior: this.tamanhos.length > 0 ? Math.max(...this.tamanhos) : 0,
        porSegundo,
        kbitPorSegundo: (porSegundo * 8) / 1000,
        projetado120: porSegundo * (tetoMs / 1000),
      },
    }
  }
}

// InfoDaCaptura são os números e os nomes que o navegador diz sobre a captura.
// Nada de áudio.
export interface InfoDaCaptura {
  // taxaDoContexto: AudioContext.sampleRate (Hz).
  taxaDoContexto: number
  // O que o navegador aplicou à faixa (MediaStreamTrack.getSettings): os filtros
  // do getUserMedia no padrão do Chrome, que a calibração conta. null: o
  // navegador não disse. O eco pode vir como texto ("all", "remote-only"), e
  // então a página o mostra como veio.
  eco: boolean | string | null
  ruido: boolean | null
  ganho: boolean | null
  canais: number | null
  taxaDaFaixa: number | null
  // O que o MediaRecorder diz: o tipo e o bitrate que ele acredita usar.
  tipoDoRecorder: string
  bitsPedidos: number
  bitsDoRecorder: number | null
}

// Captura é o microfone aberto para medir. O áudio nunca sai daqui: nivel() é um
// número, e aoPedaco só entrega o tamanho de cada pedaço do MediaRecorder.
export interface Captura {
  info: InfoDaCaptura
  nivel(): number
  aoPedaco(cb: (bytes: number) => void): () => void
  // parar desliga o microfone na hora e devolve quando o último pedaço foi
  // contado (ou em 1 s, se o stop nunca vier).
  parar(): Promise<void>
}

export type AbrirCaptura = () => Promise<Captura>

// As duas medições da página, e o quanto cada uma dura: o silêncio é curto (o
// piso se acha logo); a fala, o bastante para uma frase de verdade, com pausas.
export type Fase = 'silencio' | 'fala'
export const duracaoDaFase: Record<Fase, number> = { silencio: 6000, fala: 12000 }

// O tamanho de cada pedaço do MediaRecorder na medição: 1 s. O gravador do app
// não pica a gravação (um pedaço só, ao parar); aqui se pica para contar os
// bytes por segundo enquanto a medição corre.
export const pedacoDaCalibracaoMs = 1000

// abrirCapturaDoNavegador abre o microfone como o gravador (navegador.ts): o
// mesmo getUserMedia, o mesmo AnalyserNode sem saída, o MediaRecorder no mesmo
// formato e bitrate.
export const abrirCapturaDoNavegador: AbrirCaptura = async () => {
  const Contexto = construtorDoContexto()
  if (!suporta() || !Contexto) throw new MicrofoneIndisponivel('sem-suporte')
  const contexto = new Contexto()
  void contexto.resume().catch(() => undefined)
  let stream: MediaStream
  try {
    stream = await navigator.mediaDevices.getUserMedia(restricoesDoMicrofone)
  } catch (e) {
    void contexto.close().catch(() => undefined)
    throw e
  }
  let ligado = true
  const desligar = () => {
    if (!ligado) return
    ligado = false
    for (const faixa of stream.getTracks()) faixa.stop()
    void contexto.close().catch(() => undefined)
  }

  try {
    if (typeof MediaRecorder.isTypeSupported !== 'function' || !MediaRecorder.isTypeSupported(tipoDoAudio)) {
      throw new MicrofoneIndisponivel('sem-formato')
    }
    const analisador = contexto.createAnalyser()
    analisador.fftSize = janelaDoAnalisador
    contexto.createMediaStreamSource(stream).connect(analisador)
    const leitura = new Float32Array(analisador.fftSize)

    const ouvintes = new Set<(bytes: number) => void>()
    const gravador = new MediaRecorder(stream, { mimeType: tipoDoAudio, audioBitsPerSecond: bitsPorSegundo })
    // O pedaço chega, o tamanho é lido, e o Blob é largado: nada do áudio fica.
    gravador.addEventListener('dataavailable', (e) => {
      const bytes = e.data.size
      for (const cb of [...ouvintes]) cb(bytes)
    })
    gravador.start(pedacoDaCalibracaoMs)

    const faixa = stream.getAudioTracks()[0]
    const ajustes: MediaTrackSettings = faixa?.getSettings?.() ?? {}
    const info: InfoDaCaptura = {
      taxaDoContexto: contexto.sampleRate,
      eco: ajustes.echoCancellation ?? null,
      ruido: ajustes.noiseSuppression ?? null,
      ganho: ajustes.autoGainControl ?? null,
      canais: ajustes.channelCount ?? null,
      taxaDaFaixa: ajustes.sampleRate ?? null,
      tipoDoRecorder: gravador.mimeType,
      bitsPedidos: bitsPorSegundo,
      bitsDoRecorder: typeof gravador.audioBitsPerSecond === 'number' ? gravador.audioBitsPerSecond : null,
    }

    return {
      info,
      nivel: () => rmsDoAnalisador(analisador, leitura),
      aoPedaco(cb) {
        ouvintes.add(cb)
        return () => void ouvintes.delete(cb)
      },
      parar() {
        return new Promise<void>((resolve) => {
          let feito = false
          const acabar = () => {
            if (feito) return
            feito = true
            resolve()
          }
          if (gravador.state === 'inactive') {
            desligar()
            acabar()
            return
          }
          gravador.addEventListener('stop', acabar, { once: true })
          setTimeout(acabar, 1000)
          gravador.stop()
          // O microfone desliga na hora, sem esperar o último pedaço.
          desligar()
        })
      },
    }
  } catch (e) {
    desligar()
    throw e
  }
}

// --- como os números aparecem -------------------------------------------

const decimais = (n: number, casas: number) => n.toLocaleString('pt-BR', { minimumFractionDigits: casas, maximumFractionDigits: casas })

export const formatarRMS = (rms: number) => decimais(rms, 4)

export function formatarDB(db: number): string {
  if (db <= dbfsMinimo) return `≤ ${decimais(dbfsMinimo, 0)}`
  return decimais(db, 1)
}

export const formatarPct = (fracao: number) => `${decimais(fracao * 100, 1)}%`
export const formatarSeg = (ms: number) => `${decimais(ms / 1000, 1)} s`
export const formatarInteiro = (n: number) => Math.round(n).toLocaleString('pt-BR')
