// As medidas do ditado no celular (ADR-021): as constantes que o gravador e o
// servidor combinam, a medição de fala, a onda em dBFS, o tempo, e o id do
// ditado. Tudo aritmética pura, sem navegador: é aqui que moram os números
// que a C6c vai calibrar no Android, e por isso cada um tem o seu nome.

// O áudio vai comprimido, em webm/opus, a 32 kbit/s: 120 s dão uns 0,5 MB
// (decisão C). O Chrome pode não respeitar o audioBitsPerSecond: a C6c e a C7
// medem os bytes reais.
export const tipoDoAudio = 'audio/webm;codecs=opus'
export const bitsPorSegundo = 32_000

// O teto de duração é o do PC (internal/agent/dictation/config.go): 120 s.
export const tetoMs = 120_000

// O celular espera a resposta 90 s (ADR-021, "Prazos"). Depois disso o envio
// é cortado, e a pessoa tenta de novo com o mesmo id.
export const prazoDoEnvioMs = 90_000

// A fala mínima (decisão M): menos de 300 ms de fala, sem contar os últimos
// 200 ms (o toque para parar), e o app nem envia. É o único ponto que evita a
// cobrança do provedor.
export const falaMinimaMs = 300
export const fimDescartadoMs = 200

// limiarDeFala: o RMS (0 a 1, fração do fundo de escala) acima do qual um
// quadro conta como fala. Parte do valor do PC (internal/agent/audio/vad.go,
// DefaultVADThreshold, calibrado em amostras sintéticas) e é calibrado no
// próprio Android na C6c, por uma página que só mostra números.
export const limiarDeFala = 0.03

// Um quadro não conta mais que isto como fala: se o navegador segurou a página
// por alguns segundos (a aba congelada), o intervalo grande não pode virar
// "fala" de uma vez só.
const maiorQuadroMs = 100

// A onda é a do overlay do PC (ADR-014): barras espelhadas, o nível em dBFS
// espalhado sobre o curso da barra. Os limites foram medidos no microfone do
// PC (-48 a -14); no celular são recalibrados na C6c e renderizados antes de
// concluir. A janela é por TEMPO (1,3 s), e não por quadro: o quadro não tem
// duração fixa.
export const pisoDBFS = -48
export const tetoDBFS = -14
export const barrasDaOnda = 33
export const janelaDaOndaMs = 1300

// fracaoDoNivel converte um RMS em [0,1] na altura da barra, em [0,1]: o piso
// é repouso, o teto satura. Mesma conta do overlay (layout.go, barHeight).
export function fracaoDoNivel(rms: number): number {
  if (!(rms > 0)) return 0
  const db = 20 * Math.log10(Math.min(rms, 1))
  return Math.min(1, Math.max(0, (db - pisoDBFS) / (tetoDBFS - pisoDBFS)))
}

export interface Amostra {
  // t: o instante da leitura, em ms do relógio do navegador.
  t: number
  // rms: o nível do analisador naquele instante, de 0 a 1.
  rms: number
}

// barrasDoNivel: as 33 alturas (de 0 a 1), da mais antiga para a mais nova. Cada
// barra resume um pedaço da janela de 1,3 s pelo PICO, e não pela média: a
// média borraria a sílaba curta e alta (ADR-014).
export function barrasDoNivel(amostras: readonly Amostra[], agora: number): number[] {
  const passo = janelaDaOndaMs / barrasDaOnda
  const inicio = agora - janelaDaOndaMs
  const picos = new Array<number>(barrasDaOnda).fill(0)
  for (const a of amostras) {
    if (a.t < inicio || a.t > agora) continue
    const i = Math.min(barrasDaOnda - 1, Math.floor((a.t - inicio) / passo))
    if (a.rms > picos[i]!) picos[i] = a.rms
  }
  return picos.map(fracaoDoNivel)
}

// MedidorDeFala soma o tempo acima do limiar enquanto grava. Cada leitura
// responde pelo intervalo desde a anterior. Os últimos 200 ms não contam: é o
// toque para parar (e o dedo), que o microfone ouve.
export class MedidorDeFala {
  private anterior: number | null = null
  // fechado: a fala dos quadros que já saíram dos últimos 200 ms.
  private fechado = 0
  // recentes: os quadros dos últimos 200 ms, ainda sem dono.
  private recentes: { t: number; fala: number }[] = []

  amostrar(t: number, rms: number): void {
    if (this.anterior !== null && t > this.anterior) {
      const dt = Math.min(t - this.anterior, maiorQuadroMs)
      this.recentes.push({ t, fala: rms >= limiarDeFala ? dt : 0 })
    }
    this.anterior = Math.max(t, this.anterior ?? t)
    while (this.recentes.length > 0 && this.recentes[0]!.t <= t - fimDescartadoMs) {
      this.fechado += this.recentes.shift()!.fala
    }
  }

  // falaMs é a fala medida até o instante `fim` (o toque para parar), sem os
  // últimos 200 ms.
  falaMs(fim: number): number {
    let total = this.fechado
    for (const q of this.recentes) if (q.t <= fim - fimDescartadoMs) total += q.fala
    return Math.round(total)
  }
}

// formatarTempo é o "m:ss" ao lado da onda (AC-005 do PC, FormatDuration).
export function formatarTempo(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

// uuidV7 é o id do ditado (o 05 pede v7): 48 bits do relógio, a versão, a
// variante e o resto aleatório. O servidor o usa, com a conta, como a chave
// da idempotência; o reenvio leva o mesmo. `aleatorio` é quem dá os bytes
// (crypto.getRandomValues no navegador).
export function uuidV7(agoraMs: number, aleatorio: (n: number) => Uint8Array): string {
  const b = new Uint8Array(16)
  b.set(aleatorio(10).subarray(0, 10), 6)
  let t = Math.floor(agoraMs)
  for (let i = 5; i >= 0; i--) {
    b[i] = t % 256
    t = Math.floor(t / 256)
  }
  b[6] = (b[6]! & 0x0f) | 0x70
  b[8] = (b[8]! & 0x3f) | 0x80
  const hex = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
