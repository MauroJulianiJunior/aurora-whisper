// O gravador do ditado no celular (ADR-021, "O caminho de um ditado" e "Tela
// apagada e segundo plano"): a máquina de estados que pede o microfone,
// grava, mede a fala, para (num toque, no teto, ou quando o app sai da
// frente) e envia. Não conhece o navegador: tudo o que ele toca (o
// getUserMedia, o MediaRecorder, o AnalyserNode, o Wake Lock, o relógio) vem
// de uma Plataforma, e os testes põem dublês no lugar (navegador.ts é a de
// verdade).
//
// O áudio vive só numa variável deste objeto: do fim da gravação até o envio
// dar certo ou a pessoa descartar. Nunca vai a disco, a log nem a mensagem de
// erro; o id do ditado não muda entre uma tentativa e outra (a idempotência
// do servidor depende disso).
//
//   parado -> pedindo -> gravando -> enviando -> pronto
//                  \          \          \-> erro (com o áudio, ou sem)
//                   \          \-> erro (sem fala)       erro -> enviando (de novo)
//                    \-> erro (sem microfone)            erro -> parado (descartar)

import type { Ditado } from '../../api'
import type { DitadoGravado, PedidoDeDitado } from '../cliente'
import { type ErroDoDitado, erroDoEnvio, erroDoMicrofone, erroSemFala, fraseDoAviso } from './erros'
import { type Amostra, MedidorDeFala, barrasDoNivel, falaMinimaMs, intervaloDeLeituraMs, janelaDaOndaMs, prazoDoEnvioMs, tetoMs } from './medidas'

// Microfone é o microfone aberto, com a gravação pronta para começar.
export interface Microfone {
  // nivel é o RMS de agora, de 0 a 1 (o AnalyserNode que desenha a onda é o
  // mesmo que mede a fala).
  nivel(): number
  comecar(): void
  // parar desliga o microfone na hora (ADR-015) e devolve o áudio gravado.
  parar(): Promise<Blob>
  // descartar desliga o microfone e joga o áudio fora.
  descartar(): void
  // aoInterromper: a faixa do microfone ficou muda (uma ligação recebida) ou
  // acabou. Devolve quem desliga o ouvinte.
  aoInterromper(cb: (motivo: 'mudo' | 'acabou') => void): () => void
}

// Plataforma é tudo o que o gravador pede ao navegador.
export interface Plataforma {
  // suporta: o navegador grava áudio (getUserMedia, MediaRecorder, AudioContext).
  suporta(): boolean
  // agora é o relógio em ms, monotônico.
  agora(): number
  // quadro agenda um desenho (requestAnimationFrame). O gravador não o usa para
  // ler o nível (medidas.ts, intervaloDeLeituraMs); a página de calibração usa.
  quadro(cb: () => void): () => void
  // depois agenda cb daqui a ms (setTimeout). O gravador lê o nível por ele.
  depois(ms: number, cb: () => void): () => void
  novoId(): string
  // abrirMicrofone pede a permissão e abre o microfone. Falha com o erro do
  // navegador (NotAllowedError...) ou com MicrofoneIndisponivel.
  abrirMicrofone(): Promise<Microfone>
  // pedirTela pede o Wake Lock; null se o navegador não deu.
  pedirTela(): Promise<{ soltar(): void } | null>
  // aoSairDaFrente avisa quando a página sai da frente (visibilitychange).
  aoSairDaFrente(cb: () => void): () => void
}

export type Parada = 'teto' | 'tela' | 'mudo' | 'acabou'

// O que a faixa diz quando o app parou sozinho (ADR-021, "Tela apagada e
// segundo plano"). Vai junto do resultado: no aviso do ditado que chegou, ou
// na frase de "Não ouvi nada.".
const notaDaParada: Record<Parada, string> = {
  teto: 'Parei no limite de 2 minutos.',
  tela: 'Parei quando o app saiu da frente.',
  mudo: 'O microfone foi interrompido.',
  acabou: 'O microfone foi interrompido.',
}

export type Estado =
  | { fase: 'parado' }
  // pedindo: a permissão do microfone (ou a folha do aceite, na C6b).
  | { fase: 'pedindo' }
  // gravando: inicio é o relógio da Plataforma; telaPodeApagar, o Wake Lock
  // que não veio ("A tela pode apagar e parar o ditado.").
  | { fase: 'gravando'; inicio: number; telaPodeApagar: boolean }
  | { fase: 'enviando' }
  | { fase: 'pronto'; ditado: Ditado }
  | { fase: 'erro'; erro: ErroDoDitado }

// Chegada é o ditado que o servidor gravou. nota é o que o app fez sozinho
// (parou no teto, a tela saiu da frente); aviso, a frase de o Polish não ter
// agido.
export interface Chegada {
  ditado: Ditado
  nota?: string
  aviso?: string
}

// Quadro é uma leitura do nível, para quem desenha a onda e o tempo.
export interface Quadro {
  // decorrido: ms desde o começo da gravação.
  decorrido: number
  // barras: as alturas (de 0 a 1) da onda, da mais antiga para a mais nova.
  barras: number[]
}

export interface ConfigDoGravador {
  plataforma: Plataforma
  // enviar leva o pedido ao servidor (ClienteNuvem.ditar). O sinal é o prazo
  // de 90 s.
  enviar(pedido: PedidoDeDitado, sinal: AbortSignal): Promise<DitadoGravado>
  // liberar é a porta antes do microfone (a folha do aceite, na C6b: sem o
  // aceite, o getUserMedia nem é chamado). Sem ela, passa.
  liberar?(): Promise<boolean>
}

export class Gravador {
  private cfg: ConfigDoGravador
  private atual: Estado = { fase: 'parado' }
  private ouvintes = new Set<() => void>()
  private quadros = new Set<(q: Quadro) => void>()
  private chegadas = new Set<(c: Chegada) => void>()
  private sessoesPerdidas = new Set<() => void>()
  private audios = new Set<(tem: boolean) => void>()

  // vez muda a cada início, cancelamento e encerramento: o que volta de um
  // await com outra vez foi superado e não faz mais nada.
  private vez = 0
  private mic: Microfone | null = null
  private medidor = new MedidorDeFala()
  private amostras: Amostra[] = []
  private inicio = 0
  private id = ''
  private cancelaQuadro: (() => void) | null = null
  private cancelaTeto: (() => void) | null = null
  private cancelaVisibilidade: (() => void) | null = null
  private cancelaInterrupcao: (() => void) | null = null
  private tela: { soltar(): void } | null = null

  // pedido é o áudio na memória, com o id: do fim da gravação até o envio dar
  // certo ou a pessoa descartar. É a única referência ao áudio.
  private pedido: PedidoDeDitado | null = null
  private nota: string | undefined
  private envio: AbortController | null = null

  constructor(cfg: ConfigDoGravador) {
    this.cfg = cfg
  }

  // --- o que a tela lê ---------------------------------------------------

  // estado e assinar são o contrato do useSyncExternalStore: estado devolve o
  // mesmo objeto até a máquina mudar de fase.
  estado = (): Estado => this.atual

  assinar = (cb: () => void): (() => void) => {
    this.ouvintes.add(cb)
    return () => void this.ouvintes.delete(cb)
  }

  // aoQuadro: uma leitura do nível por quadro, só enquanto grava.
  aoQuadro(cb: (q: Quadro) => void): () => void {
    this.quadros.add(cb)
    return () => void this.quadros.delete(cb)
  }

  // aoChegar: o ditado que o servidor gravou.
  aoChegar(cb: (c: Chegada) => void): () => void {
    this.chegadas.add(cb)
    return () => void this.chegadas.delete(cb)
  }

  // aoSessaoPerdida: o envio levou 401. O áudio fica na memória (a mesma regra
  // do texto não salvo, F4), e quem ouve decide como a pessoa entra de novo.
  aoSessaoPerdida(cb: () => void): () => void {
    this.sessoesPerdidas.add(cb)
    return () => void this.sessoesPerdidas.delete(cb)
  }

  // aoAudio: passou a haver, ou deixou de haver, áudio na memória. Chega
  // antes de aoSessaoPerdida, e de forma síncrona: o Painel precisa saber que
  // há áudio ANTES de decidir se a sessão que acabou troca a tela inteira.
  aoAudio(cb: (tem: boolean) => void): () => void {
    this.audios.add(cb)
    return () => void this.audios.delete(cb)
  }

  temAudio(): boolean {
    return this.pedido !== null
  }

  // --- as ações ----------------------------------------------------------

  // iniciar é o toque no microfone. Só começa de onde não há nada em curso, e
  // nunca por cima de um áudio que espera "Tentar de novo".
  async iniciar(): Promise<void> {
    const f = this.atual.fase
    if (f === 'pedindo' || f === 'gravando' || f === 'enviando' || this.pedido !== null) return
    const vez = ++this.vez
    this.definir({ fase: 'pedindo' })
    if (this.cfg.liberar) {
      const livre = await this.cfg.liberar().catch(() => false)
      if (vez !== this.vez) return
      if (!livre) return this.definir({ fase: 'parado' })
    }
    let mic: Microfone
    try {
      mic = await this.cfg.plataforma.abrirMicrofone()
    } catch (e) {
      if (vez !== this.vez) return
      return this.definir({ fase: 'erro', erro: erroDoMicrofone(e) })
    }
    // Cancelado enquanto a permissão estava na tela: o microfone que chegou
    // agora é desligado sem gravar nada.
    if (vez !== this.vez) {
      mic.descartar()
      return
    }

    const p = this.cfg.plataforma
    this.mic = mic
    this.medidor = new MedidorDeFala()
    this.amostras = []
    this.nota = undefined
    try {
      mic.comecar()
    } catch (e) {
      this.largarMicrofone()
      return this.definir({ fase: 'erro', erro: erroDoMicrofone(e) })
    }
    // O id nasce com a gravação e é o mesmo em toda tentativa de enviá-la.
    this.id = p.novoId()
    this.inicio = p.agora()
    this.definir({ fase: 'gravando', inicio: this.inicio, telaPodeApagar: false })

    // A leitura do nível, a cada intervaloDeLeituraMs, por temporizador e não
    // por quadro (medidas.ts diz por quê): a onda e a fala saem do mesmo lugar.
    const ler = () => {
      const t = p.agora()
      const rms = mic.nivel()
      this.medidor.amostrar(t, rms)
      this.amostras.push({ t, rms })
      while (this.amostras.length > 0 && this.amostras[0]!.t < t - janelaDaOndaMs) this.amostras.shift()
      if (this.quadros.size > 0) {
        const q = { decorrido: t - this.inicio, barras: barrasDoNivel(this.amostras, t) }
        for (const cb of [...this.quadros]) cb(q)
      }
      this.cancelaQuadro = p.depois(intervaloDeLeituraMs, ler)
    }
    this.cancelaQuadro = p.depois(intervaloDeLeituraMs, ler)

    // O teto, a página que sai da frente e a faixa que fica muda ou acaba: o
    // app para, e envia o que já gravou, se houver fala.
    this.cancelaTeto = p.depois(tetoMs, () => void this.parar('teto'))
    this.cancelaVisibilidade = p.aoSairDaFrente(() => void this.parar('tela'))
    this.cancelaInterrupcao = mic.aoInterromper((m) => void this.parar(m))

    // O Wake Lock: a tela acesa enquanto grava. Se o pedido falha, a faixa
    // avisa que a tela pode apagar e parar o ditado.
    p.pedirTela()
      .catch(() => null)
      .then((tela) => {
        if (vez !== this.vez || this.atual.fase !== 'gravando') {
          tela?.soltar()
          return
        }
        if (tela) this.tela = tela
        else this.definir({ ...this.atual, telaPodeApagar: true })
      })
  }

  // parar é o toque para parar (ou o app parando sozinho: motivo). Sem fala
  // bastante, nada é enviado: é o único ponto que evita a cobrança.
  async parar(motivo?: Parada): Promise<void> {
    if (this.atual.fase !== 'gravando' || this.mic === null) return
    const mic = this.mic
    const t = this.cfg.plataforma.agora()
    const duracaoMs = Math.min(Math.max(0, t - this.inicio), tetoMs)
    // A fala vai "até a duração" (06, Ditado-Fala-Ms): o servidor recusa a que passa dela
    // (400). A duração trava no teto e a fala medida segue o relógio; se o temporizador do
    // teto atrasar, a fala passaria da duração, e o ditado não entraria nunca.
    const falaMs = Math.min(this.medidor.falaMs(t), duracaoMs)
    const nota = motivo ? notaDaParada[motivo] : undefined
    this.pararDeLer()
    if (falaMs < falaMinimaMs) {
      this.largarMicrofone()
      return this.definir({ fase: 'erro', erro: erroSemFala(nota) })
    }
    const vez = this.vez
    this.definir({ fase: 'enviando' })
    let audio: Blob
    try {
      audio = await mic.parar()
    } catch (e) {
      this.mic = null
      if (vez !== this.vez) return
      return this.definir({ fase: 'erro', erro: erroDoMicrofone(e) })
    }
    this.mic = null
    if (vez !== this.vez) return
    // Um gravador que não entregou nada (parou antes do primeiro pedaço) não tem
    // o que enviar: pagar o provedor por um corpo vazio não adianta.
    if (audio.size === 0) return this.definir({ fase: 'erro', erro: erroDoMicrofone(new Error('áudio vazio')) })
    this.pedido = { id: this.id, audio, duracaoMs, falaMs }
    this.nota = nota
    this.avisarAudio()
    await this.enviar()
  }

  // cancelar descarta a gravação sem enviar (o "Cancelar" da faixa).
  cancelar(): void {
    if (this.atual.fase === 'pedindo') {
      this.vez++
      return this.definir({ fase: 'parado' })
    }
    if (this.atual.fase !== 'gravando') return
    this.vez++
    this.pararDeLer()
    this.largarMicrofone()
    this.definir({ fase: 'parado' })
  }

  // tentarDeNovo reenvia o áudio que ficou na memória, com o MESMO id: se o
  // primeiro envio chegou ao servidor, a resposta é a do ditado já gravado.
  async tentarDeNovo(): Promise<void> {
    if (this.atual.fase !== 'erro' || this.pedido === null) return
    await this.enviar()
  }

  // descartar solta o áudio que esperava "Tentar de novo".
  descartar(): void {
    if (this.atual.fase !== 'erro') return
    this.soltarPedido()
    this.definir({ fase: 'parado' })
  }

  // dispensar tira da vista uma falha que não guarda áudio ("Não ouvi nada.").
  dispensar(): void {
    if (this.atual.fase === 'erro' && this.pedido === null) this.definir({ fase: 'parado' })
  }

  // encerrar desliga tudo o que está ligado e solta o áudio: a tela saiu de
  // cena. O objeto continua usável (o StrictMode monta duas vezes).
  encerrar(): void {
    this.vez++
    this.pararDeLer()
    this.largarMicrofone()
    this.envio?.abort()
    this.envio = null
    this.soltarPedido()
    this.definir({ fase: 'parado' })
  }

  // --- por dentro --------------------------------------------------------

  private async enviar(): Promise<void> {
    const pedido = this.pedido
    if (pedido === null) return
    const ctrl = new AbortController()
    this.envio = ctrl
    this.definir({ fase: 'enviando' })
    const cancelaPrazo = this.cfg.plataforma.depois(prazoDoEnvioMs, () => ctrl.abort())
    try {
      const gravado = await this.cfg.enviar(pedido, ctrl.signal)
      if (this.envio !== ctrl) return
      const nota = this.nota
      this.soltarPedido()
      this.definir({ fase: 'pronto', ditado: gravado.ditado })
      const chegada: Chegada = { ditado: gravado.ditado }
      if (nota) chegada.nota = nota
      if (gravado.aviso) chegada.aviso = fraseDoAviso(gravado.aviso)
      for (const cb of [...this.chegadas]) cb(chegada)
    } catch (e) {
      if (this.envio !== ctrl) return
      const erro = erroDoEnvio(e)
      // Sem retem, o reenvio não adianta (apagado, longo demais, sem fala): o
      // áudio sai da memória agora.
      if (!erro.retem) this.soltarPedido()
      this.definir({ fase: 'erro', erro })
      if (erro.tipo === 'sessao') for (const cb of [...this.sessoesPerdidas]) cb()
    } finally {
      cancelaPrazo()
      if (this.envio === ctrl) this.envio = null
    }
  }

  private soltarPedido() {
    if (this.pedido === null) return
    this.pedido = null
    this.nota = undefined
    this.avisarAudio()
  }

  private avisarAudio() {
    const tem = this.pedido !== null
    for (const cb of [...this.audios]) cb(tem)
  }

  // pararDeLer tira da frente o que só vale enquanto grava: a leitura do
  // nível, o teto, os ouvintes da página e da faixa, e a tela acesa.
  private pararDeLer() {
    this.cancelaQuadro?.()
    this.cancelaTeto?.()
    this.cancelaVisibilidade?.()
    this.cancelaInterrupcao?.()
    this.cancelaQuadro = this.cancelaTeto = this.cancelaVisibilidade = this.cancelaInterrupcao = null
    this.tela?.soltar()
    this.tela = null
  }

  private largarMicrofone() {
    this.mic?.descartar()
    this.mic = null
  }

  private definir(e: Estado) {
    this.atual = e
    for (const cb of [...this.ouvintes]) cb()
  }
}
