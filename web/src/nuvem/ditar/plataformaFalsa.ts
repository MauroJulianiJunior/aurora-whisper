// Uma Plataforma de mentira para o gravador do ditado (ADR-021): o relógio, o
// quadro, os temporizadores, a permissão do microfone, o próprio microfone, o
// Wake Lock e a página que sai da frente, todos nas mãos de quem testa. Nada
// toca o microfone, a área de transferência nem a rede de verdade (CLAUDE.md,
// "Testes que agem no sistema real"). Serve aos testes do gravador, da faixa e
// do Painel, e às telas de desenvolvimento (nuvem/telasDev.tsx).

import type { Microfone, Plataforma } from './gravador'

export interface OpcoesDaPlataforma {
  // suporta: o navegador grava (padrão: sim).
  suporta?: boolean
  // permissao: 'auto' concede ao pedir (padrão); 'manual' deixa a permissão na
  // tela até quem testa chamar conceder() ou negar().
  permissao?: 'auto' | 'manual'
  // telaNegada: o Wake Lock não vem.
  telaNegada?: boolean
  // bytes: o áudio que o microfone entrega ao parar.
  bytes?: number
}

export interface MicrofoneFalso extends Microfone {
  // nivelAtual é o RMS que o analisador devolve agora.
  nivelAtual: number
  comecou: number
  parou: number
  descartado: number
  // interromper dispara o mute ou o ended da faixa.
  interromper(motivo: 'mudo' | 'acabou'): void
  // ouvintes: quantos aoInterromper estão ligados.
  ouvintes(): number
  // audio é o Blob que o parar() entrega.
  audio: Blob
}

export function plataformaFalsa(opcoes: OpcoesDaPlataforma = {}) {
  let t = 1000
  let sequencia = 0
  const quadros = new Set<() => void>()
  const timers: { em: number; cb: () => void }[] = []
  const visibilidade = new Set<() => void>()
  const interrupcoes = new Set<(m: 'mudo' | 'acabou') => void>()
  let pendente: { ok(m: Microfone): void; erro(e: unknown): void } | null = null

  const audio = new Blob([new Uint8Array(opcoes.bytes ?? 4096).fill(7)], { type: 'audio/webm' })
  const mic: MicrofoneFalso = {
    nivelAtual: 0,
    comecou: 0,
    parou: 0,
    descartado: 0,
    audio,
    nivel: () => mic.nivelAtual,
    comecar() {
      mic.comecou++
    },
    async parar() {
      mic.parou++
      return audio
    },
    descartar() {
      mic.descartado++
    },
    aoInterromper(cb) {
      interrupcoes.add(cb)
      return () => void interrupcoes.delete(cb)
    },
    interromper: (m) => {
      for (const cb of [...interrupcoes]) cb(m)
    },
    ouvintes: () => interrupcoes.size,
  }

  const estado = {
    abriu: 0,
    pediuTela: 0,
    telaSolta: 0,
    ids: [] as string[],
  }

  const plataforma: Plataforma = {
    suporta: () => opcoes.suporta ?? true,
    agora: () => t,
    quadro(cb) {
      quadros.add(cb)
      return () => void quadros.delete(cb)
    },
    depois(ms, cb) {
      const item = { em: t + ms, cb }
      timers.push(item)
      return () => {
        const i = timers.indexOf(item)
        if (i >= 0) timers.splice(i, 1)
      }
    },
    novoId() {
      // Um UUID v7 de verdade na forma: a nuvem de mentira confere.
      const id = `0192f0a1-7c3e-7d4a-9b2f-${(++sequencia).toString(16).padStart(12, '0')}`
      estado.ids.push(id)
      return id
    },
    abrirMicrofone() {
      estado.abriu++
      if (opcoes.permissao === 'manual') {
        return new Promise<Microfone>((ok, erro) => {
          pendente = { ok, erro }
        })
      }
      return Promise.resolve(mic)
    },
    async pedirTela() {
      estado.pediuTela++
      if (opcoes.telaNegada) return null
      return {
        soltar: () => {
          estado.telaSolta++
        },
      }
    },
    aoSairDaFrente(cb) {
      visibilidade.add(cb)
      return () => void visibilidade.delete(cb)
    },
  }

  return {
    plataforma,
    mic,
    estado,
    // passo avança o relógio `ms` (16 ms é um quadro a 60 Hz): dispara os
    // temporizadores que venceram, em ordem, e depois cada quadro agendado.
    passo(ms = 16) {
      t += ms
      for (;;) {
        const i = timers.findIndex((x) => x.em <= t)
        if (i < 0) break
        timers.splice(i, 1)[0]!.cb()
      }
      for (const cb of [...quadros]) {
        quadros.delete(cb)
        cb()
      }
    },
    // rodar avança `ms` com o nível dado (um número, ou uma função do instante
    // desde o começo da chamada), quadro a quadro.
    rodar(ms: number, nivel: number | ((desde: number) => number)) {
      for (let feito = 0; feito < ms; feito += 16) {
        mic.nivelAtual = typeof nivel === 'function' ? nivel(feito) : nivel
        this.passo(Math.min(16, ms - feito))
      }
    },
    // conceder e negar fecham a permissão que ficou na tela (permissao
    // 'manual').
    conceder() {
      pendente?.ok(mic)
      pendente = null
    },
    negar(e: unknown) {
      pendente?.erro(e)
      pendente = null
    },
    // sairDaFrente: a página foi para segundo plano (a tela apagou, outro app).
    sairDaFrente() {
      for (const cb of [...visibilidade]) cb()
    },
    quadrosPendentes: () => quadros.size,
    timersPendentes: () => timers.length,
    ouvintesDeVisibilidade: () => visibilidade.size,
    agora: () => t,
  }
}

export type PlataformaFalsa = ReturnType<typeof plataformaFalsa>
