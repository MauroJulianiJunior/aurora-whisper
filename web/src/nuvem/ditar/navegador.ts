// A Plataforma de verdade (ADR-021): o getUserMedia, o MediaRecorder em
// webm/opus a 32 kbit/s, o AnalyserNode que mede o nível, o Wake Lock, o
// relógio e a visibilidade da página. É a única peça do app que toca o
// microfone; os testes põem dublês no lugar dos globais do navegador
// (navegador.test.ts), e nenhum abre o microfone de verdade.
//
// O áudio passa por aqui em pedaços de Blob, numa variável da função, até
// parar() entregá-lo ao gravador. Nada é guardado no aparelho: nem cache, nem
// armazenamento da página.
//
// O AnalyserNode lê o nível sem AudioWorklet (a CSP default-src 'self' não
// muda) e não é ligado a nenhuma saída: nada toca.

import { MicrofoneIndisponivel } from './erros'
import type { Microfone, Plataforma } from './gravador'
import { bitsPorSegundo, tipoDoAudio, uuidV7 } from './medidas'

// O quanto do sinal o analisador lê por vez: 1024 amostras, uns 21 ms a 48 kHz.
const janelaDoAnalisador = 1024

function construtorDoContexto(): typeof AudioContext | undefined {
  return window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
}

function suporta(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    typeof navigator.mediaDevices?.getUserMedia === 'function' &&
    typeof MediaRecorder !== 'undefined' &&
    construtorDoContexto() !== undefined
  )
}

async function abrirMicrofone(): Promise<Microfone> {
  const Contexto = construtorDoContexto()
  if (!suporta() || !Contexto) throw new MicrofoneIndisponivel('sem-suporte')
  // O contexto nasce já, ainda no gesto do toque: criado depois da permissão,
  // o Chrome pode deixá-lo suspenso, e um analisador suspenso lê silêncio. Os
  // filtros do getUserMedia (eco, ruído, ganho) ficam no padrão do Chrome até
  // a calibração da C6c.
  const contexto = new Contexto()
  void contexto.resume().catch(() => undefined)
  let stream: MediaStream
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: true })
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

  // Daqui em diante, qualquer falha desliga o microfone antes de sair: um
  // stream aberto e esquecido deixa o indicador do Android aceso.
  try {
    if (typeof MediaRecorder.isTypeSupported !== 'function' || !MediaRecorder.isTypeSupported(tipoDoAudio)) {
      throw new MicrofoneIndisponivel('sem-formato')
    }
    const analisador = contexto.createAnalyser()
    analisador.fftSize = janelaDoAnalisador
    contexto.createMediaStreamSource(stream).connect(analisador)
    const leitura = new Float32Array(analisador.fftSize)

    const pedacos: Blob[] = []
    const gravador = new MediaRecorder(stream, { mimeType: tipoDoAudio, audioBitsPerSecond: bitsPorSegundo })
    gravador.addEventListener('dataavailable', (e) => {
      if (e.data.size > 0) pedacos.push(e.data)
    })

    return {
      nivel() {
        analisador.getFloatTimeDomainData(leitura)
        let soma = 0
        for (const v of leitura) soma += v * v
        return Math.sqrt(soma / leitura.length)
      },
      comecar() {
        gravador.start()
      },
      parar() {
        return new Promise<Blob>((resolve) => {
          let entregue = false
          const entregar = () => {
            if (entregue) return
            entregue = true
            // Os pedaços saem daqui: o Blob é a única referência ao áudio.
            resolve(new Blob(pedacos.splice(0), { type: 'audio/webm' }))
          }
          if (gravador.state === 'inactive') {
            desligar()
            entregar()
            return
          }
          // O último dataavailable chega antes do stop. Se o stop nunca vier, o
          // que já chegou segue em 3 s, e o envio não fica preso.
          gravador.addEventListener('stop', entregar, { once: true })
          setTimeout(entregar, 3000)
          gravador.stop()
          // O microfone desliga na hora (ADR-015), sem esperar o último pedaço.
          desligar()
        })
      },
      descartar() {
        pedacos.length = 0
        if (gravador.state !== 'inactive') {
          try {
            gravador.stop()
          } catch {
            // já parou
          }
        }
        desligar()
      },
      aoInterromper(cb) {
        const faixas = stream.getAudioTracks()
        const mudo = () => cb('mudo')
        const acabou = () => cb('acabou')
        for (const f of faixas) {
          f.addEventListener('mute', mudo)
          f.addEventListener('ended', acabou)
        }
        return () => {
          for (const f of faixas) {
            f.removeEventListener('mute', mudo)
            f.removeEventListener('ended', acabou)
          }
        }
      },
    }
  } catch (e) {
    desligar()
    throw e
  }
}

async function pedirTela(): Promise<{ soltar(): void } | null> {
  try {
    const wakeLock = navigator.wakeLock
    if (!wakeLock) return null
    const trava = await wakeLock.request('screen')
    return {
      soltar() {
        void trava.release().catch(() => undefined)
      },
    }
  } catch {
    // sem HTTPS, página fora da frente, ou o sistema negou
    return null
  }
}

export function plataformaDoNavegador(): Plataforma {
  return {
    suporta,
    agora: () => performance.now(),
    quadro(cb) {
      const id = requestAnimationFrame(cb)
      return () => cancelAnimationFrame(id)
    },
    depois(ms, cb) {
      const id = setTimeout(cb, ms)
      return () => clearTimeout(id)
    },
    novoId: () => uuidV7(Date.now(), (n) => crypto.getRandomValues(new Uint8Array(n))),
    abrirMicrofone,
    pedirTela,
    aoSairDaFrente(cb) {
      const f = () => {
        if (document.visibilityState === 'hidden') cb()
      }
      document.addEventListener('visibilitychange', f)
      return () => document.removeEventListener('visibilitychange', f)
    },
  }
}
