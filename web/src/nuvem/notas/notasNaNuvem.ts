// As notas no app do celular (ADR-022, N8): o ClienteDeNotas da /api/v1 pela
// sessão, a lista guardada no aparelho e a fila das mudanças feitas sem rede (a
// E4, "Marcar sem rede"). A aba Notas é a mesma do PC (notas/Notas.tsx): só o
// cliente e a rede mudam.
//
// Com rede, cada pedido vai à nuvem, e o que volta (a lista, a nota) fica
// guardado no aparelho (armazem.ts). Sem rede, a lista sai do que está guardado,
// e só três coisas se fazem: marcar, desmarcar e acrescentar item. Elas vão para
// a fila, uma entrada por nota (a base que a nuvem confirmou e o corpo de
// agora), que sobrevive a fechar o app e sobe sozinha quando a rede volta: o
// PATCH com a base e o corpo_base, e a nuvem junta com o que o PC mudou no meio
// (a mescla da lista de marcar da N7) ou guarda no selo. Nada se perde calado:
// a recusa vira uma nota nova com o que foi feito sem rede, e o aviso diz.
//
// "Sem rede" é o que o navegador diz (navigator.onLine) e, mais que isso, o
// pedido que não chegou: o primeiro que chega de volta acende a rede de novo.

import { type Buscador, Conflito, ErroDaApi, ForaDoAr, SemSessao, buscadorPadrao, pedir } from '../../api'
import { itensDaEnumeracao, lerCorpo, nomeDaNota, soMarcouOuAcrescentou } from '../../notas/corpo'
import {
  type ClienteDeImagens,
  type ClienteDeNotas,
  type EstadoDaRede,
  type ImagemDaNota,
  type Marcador,
  type MudancaDaNota,
  type Nota,
  type NovaNota,
  type RedeDasNotas,
  SoComRede,
} from '../../notas/tipos'
import type { Armazem, MudancaEsperando } from './armazem'
import { type ImagemDaNuvem, type NotaDaNuvem, antesNaLista, daNuvem, doMarcador, imagemDaNuvem, vistaDa } from './formato'
import { soltarUrlDaImagem, urlDaImagem } from './urlDaImagem'

// Quanto espera para tentar subir de novo a fila que não subiu.
export const TENTAR_DE_NOVO_EM = 30_000

// Quantas versões de cada nota o cliente lembra o conteúdo (o corpo_base do
// PATCH sai daqui).
const VERSOES_LEMBRADAS = 6

export interface ConfigDasNotas {
  buscar?: Buscador
  armazem: Armazem
  // online: o que o navegador diz da rede (navigator.onLine). Os testes trocam.
  online?(): boolean
  // ouvirRede: avisa quando o navegador diz que a rede voltou ou caiu, e quando
  // o app volta à frente. Devolve quem para de ouvir.
  ouvirRede?(cb: () => void): () => void
  // depois agenda (setTimeout); os testes trocam.
  depois?(ms: number, cb: () => void): () => void
  // agora é o relógio (a data da mudança na fila).
  agora?(): Date
}

// RedeDasNotasNaNuvem é a RedeDasNotas do celular, com os avisos da fila.
export interface RedeComAvisos extends RedeDasNotas {
  // aoAvisar: a fila subiu algo que pede a atenção da pessoa (o selo, a nota
  // que foi para a lixeira, a recusa que virou nota nova).
  aoAvisar(cb: (texto: string) => void): () => void
}

// dobrar é a busca sem caixa e sem acento, como a do PC (internal/notas.Dobrar).
const dobrar = (s: string) =>
  s
    .toLocaleLowerCase('pt-BR')
    .normalize('NFD')
    .replace(/\p{Mn}/gu, '')

const ouvirRedePadrao = (cb: () => void) => {
  if (typeof window === 'undefined') return () => {}
  const visivel = () => {
    if (document.visibilityState === 'visible') cb()
  }
  window.addEventListener('online', cb)
  window.addEventListener('offline', cb)
  document.addEventListener('visibilitychange', visivel)
  return () => {
    window.removeEventListener('online', cb)
    window.removeEventListener('offline', cb)
    document.removeEventListener('visibilitychange', visivel)
  }
}

const depoisPadrao = (ms: number, cb: () => void) => {
  const t = setTimeout(cb, ms)
  return () => clearTimeout(t)
}

const onlinePadrao = () => (typeof navigator === 'undefined' ? true : navigator.onLine !== false)

export class NotasNaNuvem {
  readonly cliente: ClienteDeNotas
  readonly rede: RedeComAvisos

  private buscar: Buscador
  private armazem: Armazem
  private online: () => boolean
  private depois: (ms: number, cb: () => void) => () => void
  private agora: () => Date
  private soltarRede: () => void

  // O que a nuvem deu por último, de cada nota (sem a fila por cima).
  private notas = new Map<string, Nota>()
  // O título e o corpo de cada versão vista de cada nota (o corpo_base).
  private versoes = new Map<string, Map<number, { titulo: string; corpo: string }>>()
  private marcadores: Marcador[] = []
  private fila = new Map<string, MudancaEsperando>()
  // equivalente: a versão que a fila deu à janela (a base da entrada) e a versão
  // em que a nuvem gravou exatamente aquele conteúdo, quando a entrada subiu. A
  // janela ainda aberta continua com a de antes; a próxima gravação dela parte
  // da nova, e não vira um conflito com ela mesma.
  private equivalente = new Map<string, { de: number; para: number }>()
  private atual: EstadoDaRede
  private ouvintes = new Set<() => void>()
  private avisos = new Set<(texto: string) => void>()
  private pronto: Promise<void>
  private subindo: Promise<void> | null = null
  private proxima: Promise<void> | null = null
  private cancelarTentativa: (() => void) | null = null
  private encerrado = false

  constructor(cfg: ConfigDasNotas) {
    this.buscar = cfg.buscar ?? buscadorPadrao
    this.armazem = cfg.armazem
    this.online = cfg.online ?? onlinePadrao
    this.depois = cfg.depois ?? depoisPadrao
    this.agora = cfg.agora ?? (() => new Date())
    this.atual = { semRede: !this.online(), esperando: new Set() }
    this.pronto = this.carregar()
    this.soltarRede = (cfg.ouvirRede ?? ouvirRedePadrao)(() => this.aoMudarARede())
    this.rede = {
      assinar: (cb) => {
        this.ouvintes.add(cb)
        return () => void this.ouvintes.delete(cb)
      },
      estado: () => this.atual,
      aoAvisar: (cb) => {
        this.avisos.add(cb)
        return () => void this.avisos.delete(cb)
      },
    }
    this.cliente = this.criarCliente()
  }

  // esperando é quantas notas têm mudança na fila (o Sair avisa antes).
  esperando(): number {
    return this.fila.size
  }

  // esquecer apaga o que esta conta guardou no aparelho (o Sair e o "Apagar meus
  // dados"). No Sair, também para de ouvir a rede (encerrar); no "Apagar meus
  // dados", a sessão continua, e as notas também.
  async esquecer({ encerrar = true }: { encerrar?: boolean } = {}): Promise<void> {
    if (encerrar) this.encerrar()
    this.notas.clear()
    this.versoes.clear()
    this.marcadores = []
    this.fila.clear()
    this.publicar()
    await this.armazem.esquecer()
  }

  // encerrar para de ouvir a rede e de tentar subir.
  encerrar(): void {
    this.encerrado = true
    this.soltarRede()
    this.cancelarTentativa?.()
    this.cancelarTentativa = null
  }

  // receber guarda uma nota que chegou por outro caminho (o "Ditar para nota").
  receber(n: Nota): void {
    this.guardar(n)
  }

  // subir manda a fila, uma nota de cada vez, na ordem em que entraram. Uma só
  // rodada por vez. Para na primeira falha de rede (ou de sessão) e tenta de novo
  // depois.
  subir(): Promise<void> {
    // Pedida com uma rodada no meio (a rede voltou enquanto a rodada de antes
    // falhava): uma rodada nova, depois dela.
    if (this.subindo) {
      this.proxima ??= this.subindo.then(() => {
        this.proxima = null
        return this.subir()
      })
      return this.proxima
    }
    this.subindo = (async () => {
      try {
        await this.pronto
        const ordem = [...this.fila.values()].sort((a, b) => (a.criadaEm < b.criadaEm ? -1 : a.criadaEm > b.criadaEm ? 1 : 0))
        for (const e of ordem) {
          if (this.encerrado) return
          if (!this.fila.has(e.notaId)) continue
          if (!(await this.subirUma(e.notaId))) break
        }
      } finally {
        this.subindo = null
        this.agendar()
      }
    })()
    return this.subindo
  }

  // --- por dentro ---------------------------------------------------------

  private async carregar(): Promise<void> {
    let g
    try {
      g = await this.armazem.carregar()
    } catch {
      return
    }
    for (const n of g.notas) if (!this.notas.has(n.id)) this.lembrar(n)
    if (this.marcadores.length === 0) this.marcadores = g.marcadores
    for (const m of g.fila) if (!this.fila.has(m.notaId)) this.fila.set(m.notaId, m)
    this.publicar()
    if (this.fila.size > 0) void this.subir()
  }

  private aoMudarARede(): void {
    if (this.encerrado) return
    if (!this.online()) return this.caiu()
    // O navegador diz que voltou: a fila tenta subir (e é ela que confirma).
    if (this.fila.size > 0) void this.subir()
    else if (this.atual.semRede) this.publicar({ semRede: false })
  }

  private agendar(): void {
    this.cancelarTentativa?.()
    this.cancelarTentativa = null
    if (this.encerrado || this.fila.size === 0) return
    this.cancelarTentativa = this.depois(TENTAR_DE_NOVO_EM, () => {
      this.cancelarTentativa = null
      void this.subir()
    })
  }

  private publicar(mudanca: Partial<EstadoDaRede> = {}): void {
    this.atual = { semRede: mudanca.semRede ?? this.atual.semRede, esperando: new Set(this.fila.keys()) }
    for (const cb of [...this.ouvintes]) cb()
  }

  private avisar(texto: string): void {
    for (const cb of [...this.avisos]) cb(texto)
  }

  // vivo: um pedido chegou de volta. caiu: um pedido não chegou.
  private vivo(): void {
    if (!this.atual.semRede) return
    this.publicar({ semRede: false })
    if (this.fila.size > 0) void this.subir()
  }

  private caiu(): void {
    if (!this.atual.semRede) this.publicar({ semRede: true })
  }

  // lembrar guarda a nota e o conteúdo da versão dela, só na memória.
  private lembrar(n: Nota): void {
    const limpa = { ...n }
    delete limpa.esperando
    delete limpa.gravacao
    this.notas.set(n.id, limpa)
    let vs = this.versoes.get(n.id)
    if (!vs) {
      vs = new Map()
      this.versoes.set(n.id, vs)
    }
    vs.set(n.version, { titulo: n.titulo, corpo: n.corpo })
    while (vs.size > VERSOES_LEMBRADAS) vs.delete(vs.keys().next().value as number)
  }

  // guardar lembra e grava no aparelho.
  private guardar(n: Nota): void {
    this.lembrar(n)
    void this.armazem.gravarNota(n).catch(() => undefined)
  }

  private esquecerNota(id: string): void {
    this.notas.delete(id)
    this.versoes.delete(id)
    void this.armazem.apagarNota(id).catch(() => undefined)
  }

  // mudarImagens põe no que o aparelho guarda da nota (só os metadados) a imagem que
  // entrou ou saiu: a lista sem rede e as caixas "Imagem sem rede" ficam certas. Os
  // bytes nunca passam por aqui.
  private mudarImagens(notaId: string, mudar: (imagens: ImagemDaNota[]) => ImagemDaNota[]): void {
    const n = this.notas.get(notaId)
    if (n) this.guardar({ ...n, imagens: mudar(n.imagens ?? []) })
  }

  // comFila é a nota como a tela a vê: a da nuvem, com o que espera na fila por
  // cima.
  private comFila(n: Nota): Nota {
    const e = this.fila.get(n.id)
    return e ? { ...n, titulo: e.titulo, corpo: e.corpo, esperando: true } : n
  }

  // versaoDe troca a versão que a janela tem pela que a nuvem deu ao mesmo
  // conteúdo quando a fila subiu (equivalente).
  private versaoDe(id: string, versaoDaJanela: number): number {
    const eq = this.equivalente.get(id)
    return eq && eq.de === versaoDaJanela && !this.fila.has(id) ? eq.para : versaoDaJanela
  }

  private conteudoNaVersao(id: string, versao: number): { titulo: string; corpo: string } | undefined {
    return this.versoes.get(id)?.get(versao)
  }

  private daOrigem(init: RequestInit = {}): RequestInit {
    return { ...init, credentials: 'same-origin' }
  }

  private comJson(method: string, corpo: unknown, sinal?: AbortSignal): RequestInit {
    return this.daOrigem({ method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(corpo), signal: sinal })
  }

  // pedido faz o pedido e cuida da rede: o que volta acende a rede; o que não
  // chega a apaga e vira SoComRede (o que não se faz sem rede).
  private async pedido(caminho: string, init: RequestInit, conflitoEhVersao = true): Promise<Response> {
    try {
      const r = await pedir(this.buscar, caminho, init, conflitoEhVersao)
      this.vivo()
      return r
    } catch (e) {
      if (e instanceof ForaDoAr) {
        this.caiu()
        throw new SoComRede()
      }
      if (!(e instanceof DOMException)) this.vivo()
      throw e
    }
  }

  private async lerNota(r: Response): Promise<Nota> {
    const n = daNuvem((await r.json()) as NotaDaNuvem)
    this.guardar(n)
    return n
  }

  private exigirRede(): void {
    if (this.atual.semRede) throw new SoComRede()
  }

  // enfileirar põe a mudança de conteúdo na fila da nota. Sem rede, só vale
  // marcar, desmarcar e acrescentar item, sobre a base da fila (ou a da versão
  // que a janela conhece). Com rede (a fila ainda não subiu), vale qualquer
  // texto: sobe com a base de antes, e a nuvem junta ou guarda no selo.
  private enfileirar(id: string, versao: number, titulo: string | undefined, corpo: string | undefined): Nota {
    let e = this.fila.get(id)
    if (!e) {
      const base = this.conteudoNaVersao(id, versao)
      if (!base) throw new SoComRede()
      e = { notaId: id, baseVersion: versao, tituloBase: base.titulo, corpoBase: base.corpo, titulo: base.titulo, corpo: base.corpo, criadaEm: this.agora().toISOString() }
    }
    const novo = { ...e, titulo: titulo ?? e.titulo, corpo: corpo ?? e.corpo }
    if (this.atual.semRede && (novo.titulo !== e.tituloBase || !soMarcouOuAcrescentou(e.corpoBase, novo.corpo))) throw new SoComRede()
    if (novo.titulo === novo.tituloBase && novo.corpo === novo.corpoBase && !novo.enviado) {
      this.fila.delete(id)
      void this.armazem.tirarDaFila(id).catch(() => undefined)
    } else {
      this.fila.set(id, novo)
      void this.armazem.gravarNaFila(novo).catch(() => undefined)
    }
    this.publicar()
    if (!this.atual.semRede) void this.subir()
    const conhecida = this.notas.get(id)
    const n: Nota = conhecida
      ? { ...conhecida }
      : { id, titulo: '', corpo: '', cor: null, fixada: false, arquivada: false, marcadores: [], posicao: '', created_at: '', updated_at: '', version: versao }
    return { ...n, titulo: novo.titulo, corpo: novo.corpo, version: novo.baseVersion, esperando: this.fila.has(id) || undefined }
  }

  private gravarFila(e: MudancaEsperando): void {
    this.fila.set(e.notaId, e)
    void this.armazem.gravarNaFila(e).catch(() => undefined)
  }

  private tirarDaFila(id: string): void {
    this.fila.delete(id)
    void this.armazem.tirarDaFila(id).catch(() => undefined)
    this.publicar()
  }

  // subirUma manda a mudança de uma nota. Devolve false quando a rodada tem de
  // parar (a rede caiu, a sessão acabou, o servidor falhou).
  private async subirUma(id: string): Promise<boolean> {
    let e = this.fila.get(id)
    if (!e) return true
    if (e.enviado) {
      // O envio de antes não teve resposta: se a nota já tem o que foi
      // enviado, ele valeu, e a base passa a ser ela.
      const antes = e.enviado
      let lida: Nota
      try {
        lida = daNuvem((await (await pedir(this.buscar, `/api/v1/notes/${encodeURIComponent(id)}`, this.daOrigem())).json()) as NotaDaNuvem)
      } catch (err) {
        return this.falhaNaSubida(e, err)
      }
      this.vivo()
      this.guardar(lida)
      let r: MudancaEsperando = { ...e, enviado: undefined }
      if (lida.titulo === antes.titulo && lida.corpo === antes.corpo) r = { ...r, baseVersion: lida.version, tituloBase: lida.titulo, corpoBase: lida.corpo }
      if (r.titulo === r.tituloBase && r.corpo === r.corpoBase) {
        this.tirarDaFila(id)
        return true
      }
      this.gravarFila(r)
      e = r
    }
    const enviado = { titulo: e.titulo, corpo: e.corpo }
    this.gravarFila({ ...e, enviado })
    let s: Nota
    try {
      const r = await pedir(
        this.buscar,
        `/api/v1/notes/${encodeURIComponent(id)}`,
        this.comJson('PATCH', { base_version: e.baseVersion, titulo: e.titulo, corpo: e.corpo, corpo_base: e.corpoBase }),
        false,
      )
      s = daNuvem((await r.json()) as NotaDaNuvem)
    } catch (err) {
      return this.falhaNaSubida({ ...e, enviado }, err)
    }
    this.vivo()
    this.guardar(s)
    const agora = this.fila.get(id)
    if (!agora || (agora.titulo === enviado.titulo && agora.corpo === enviado.corpo)) {
      if (s.titulo === enviado.titulo && s.corpo === enviado.corpo) this.equivalente.set(id, { de: e.baseVersion, para: s.version })
      this.tirarDaFila(id)
    } else {
      // Mudou enquanto subia: o resto fica na fila. Se a nuvem gravou o que foi
      // mandado, a base passa a ser ela; se juntou, a base continua a de antes
      // (a próxima subida junta de novo).
      const resto = { ...agora, enviado: undefined }
      if (s.titulo === enviado.titulo && s.corpo === enviado.corpo) Object.assign(resto, { baseVersion: s.version, tituloBase: s.titulo, corpoBase: s.corpo })
      this.gravarFila(resto)
      this.publicar()
    }
    const nome = nomeDaNota(s.titulo, lerCorpo(s.corpo))
    if (s.deleted_at) this.avisar(`A nota “${nome}” foi para a lixeira em outro aparelho. A mudança feita sem rede ficou guardada nela.`)
    else if (s.gravacao === 'no_selo') this.avisar(`A nota “${nome}” mudou em outro aparelho também: as duas versões estão nela. Abra para escolher.`)
    return true
  }

  // falhaNaSubida decide o que fazer com a mudança que não subiu.
  private async falhaNaSubida(e: MudancaEsperando, err: unknown): Promise<boolean> {
    if (err instanceof ForaDoAr || (err instanceof DOMException && err.name === 'AbortError')) {
      this.caiu()
      return false
    }
    if (err instanceof SemSessao) {
      // A sessão acabou: nada foi aplicado. A fila espera a pessoa entrar de novo.
      this.gravarFila({ ...e, enviado: undefined })
      return false
    }
    this.vivo()
    const status = err instanceof ErroDaApi ? err.status : err instanceof Conflito ? 409 : 500
    if (status >= 500 || status === 429) return false
    // A nuvem recusou (a nota foi apagada de vez, a base não vale mais, o texto
    // não passa): a mudança vira uma nota nova, para nada se perder calado.
    return this.recuperarComoNova(e)
  }

  private async recuperarComoNova(e: MudancaEsperando): Promise<boolean> {
    const nome = nomeDaNota(e.titulo, lerCorpo(e.corpo))
    const titulo = `${e.titulo.trim() || nome} (feita sem rede)`
    try {
      const r = await pedir(this.buscar, '/api/v1/notes', this.comJson('POST', { titulo, corpo: e.corpo }), false)
      this.guardar(daNuvem((await r.json()) as NotaDaNuvem))
    } catch (err) {
      if (err instanceof ForaDoAr) {
        this.caiu()
        return false
      }
      this.avisar(`A mudança feita sem rede em “${nome}” não subiu. Ela continua guardada neste aparelho.`)
      return false
    }
    this.tirarDaFila(e.notaId)
    this.avisar(`Não deu para juntar a mudança feita sem rede em “${nome}”: ela virou a nota “${titulo}”.`)
    return true
  }

  private filtrar(itens: Nota[], marcador?: string, busca?: string): Nota[] {
    let out = itens
    if (marcador) out = out.filter((n) => n.marcadores.includes(marcador))
    const termo = dobrar((busca ?? '').trim())
    if (termo) out = out.filter((n) => dobrar(n.titulo).includes(termo) || dobrar(n.corpo).includes(termo))
    return out
  }

  // criarClienteDeImagens é o ClienteDeImagens do celular (ADR-022, N10b) sobre as
  // rotas da sessão: anexar e tirar só com rede; ler a cada vez que a imagem vai à
  // tela. Os bytes só passam pela memória da página (a URL blob: de urlDaImagem.ts);
  // a resposta do GET é no-store, e o service worker nem toca nessa rota.
  private criarClienteDeImagens(): ClienteDeImagens {
    const daNota = (id: string) => `/api/v1/notes/${encodeURIComponent(id)}`
    const daImagem = (notaId: string, id: string) => `${daNota(notaId)}/images/${encodeURIComponent(id)}`
    // O corpo é o JPEG cru, já reduzido no aparelho (reduzirImagem), sem multipart e
    // sem o nome do arquivo.
    const comJpeg = (jpeg: Blob) => this.daOrigem({ method: 'POST', headers: { 'Content-Type': 'image/jpeg' }, body: jpeg })
    return {
      anexar: async (notaId, jpeg) => {
        this.exigirRede()
        // conflitoEhVersao false: os 409 daqui são a nota cheia, o teto e a lixeira.
        const r = await this.pedido(`${daNota(notaId)}/images`, comJpeg(jpeg), false)
        const im = imagemDaNuvem((await r.json()) as ImagemDaNuvem)
        this.mudarImagens(notaId, (l) => [...l.filter((x) => x.id !== im.id), im])
        return im
      },
      criarNotaComImagem: async (jpeg) => {
        this.exigirRede()
        return this.lerNota(await this.pedido('/api/v1/notes/with-image', comJpeg(jpeg), false))
      },
      ler: async (notaId, id, sinal) => {
        const r = await this.pedido(daImagem(notaId, id), this.daOrigem({ signal: sinal }), false)
        const tipo = r.headers.get('Content-Type') ?? ''
        if (!tipo.toLowerCase().startsWith('image/jpeg')) throw new Error('a nuvem não devolveu um JPEG')
        // O tipo vem da resposta conferida acima: a URL blob: só nasce de um JPEG.
        return new Blob([await r.blob()], { type: 'image/jpeg' })
      },
      tirar: async (notaId, id) => {
        this.exigirRede()
        // keepalive: o aviso que vence com a página fechando ainda tira a imagem.
        await this.pedido(daImagem(notaId, id), { ...this.daOrigem({ method: 'DELETE' }), keepalive: true }, false)
        this.mudarImagens(notaId, (l) => l.filter((x) => x.id !== id))
      },
      paraATela: urlDaImagem,
      soltar: soltarUrlDaImagem,
    }
  }

  private criarCliente(): ClienteDeNotas {
    const daNota = (id: string) => `/api/v1/notes/${encodeURIComponent(id)}`
    const doMarcadorUrl = (id: string) => `/api/v1/labels/${encodeURIComponent(id)}`
    return {
      listarNotas: async ({ vista, marcador, busca }, sinal) => {
        await this.pronto
        try {
          const r = await pedir(this.buscar, `/api/v1/notes?vista=${encodeURIComponent(vista)}`, this.daOrigem({ signal: sinal }))
          this.vivo()
          const { items } = (await r.json()) as { items?: NotaDaNuvem[] | null }
          const vieram = (items ?? []).map(daNuvem)
          const ids = new Set(vieram.map((n) => n.id))
          // O que era desta vista e não veio saiu dela (outro aparelho mexeu): sai
          // do aparelho, menos a nota com mudança na fila.
          for (const n of [...this.notas.values()]) if (vistaDa(n) === vista && !ids.has(n.id) && !this.fila.has(n.id)) this.esquecerNota(n.id)
          for (const n of vieram) this.guardar(n)
        } catch (e) {
          if (!(e instanceof ForaDoAr)) throw e
          this.caiu()
        }
        const daVista = [...this.notas.values()].filter((n) => vistaDa(n) === vista).sort(antesNaLista)
        return this.filtrar(daVista.map((n) => this.comFila(n)), marcador, busca)
      },
      obterNota: async (id) => {
        await this.pronto
        try {
          return this.comFila(await this.lerNota(await this.pedido(daNota(id), this.daOrigem())))
        } catch (e) {
          const guardada = this.notas.get(id)
          if (e instanceof SoComRede && guardada) return this.comFila(guardada)
          throw e
        }
      },
      criarNota: async (p: NovaNota) => {
        this.exigirRede()
        const corpo: Record<string, unknown> = { titulo: p.titulo ?? '', corpo: p.corpo ?? '' }
        if (p.cor !== undefined) corpo.cor = p.cor
        if (p.fixada !== undefined) corpo.fixada = p.fixada
        if (p.arquivada !== undefined) corpo.arquivada = p.arquivada
        return this.lerNota(await this.pedido('/api/v1/notes', this.comJson('POST', corpo)))
      },
      editarNota: async (id, versaoDaJanela, mudanca: MudancaDaNota) => {
        await this.pronto
        const versao = this.versaoDe(id, versaoDaJanela)
        const soConteudo = Object.keys(mudanca).every((k) => k === 'titulo' || k === 'corpo')
        if (this.fila.has(id) || this.atual.semRede) {
          if (!soConteudo) throw new SoComRede()
          return this.enfileirar(id, versao, mudanca.titulo, mudanca.corpo)
        }
        const corpo: Record<string, unknown> = { ...mudanca, base_version: versao }
        if (mudanca.titulo !== undefined || mudanca.corpo !== undefined) {
          // O conteúdo vai inteiro, com o corpo da base: é o que a mescla da
          // lista de marcar precisa (N7).
          const base = this.conteudoNaVersao(id, versao)
          if (base) {
            corpo.titulo ??= base.titulo
            corpo.corpo ??= base.corpo
            corpo.corpo_base = base.corpo
          }
        }
        try {
          return await this.lerNota(await pedir(this.buscar, daNota(id), this.comJson('PATCH', corpo)))
        } catch (e) {
          if (!(e instanceof ForaDoAr)) {
            this.vivo()
            throw e
          }
          this.caiu()
          // A rede caiu no meio: marcar, desmarcar e acrescentar item esperam na
          // fila; o resto, a rede.
          if (!soConteudo) throw new SoComRede()
          return this.enfileirar(id, versao, mudanca.titulo, mudanca.corpo)
        }
      },
      moverNotaParaLixeira: async (id) => {
        this.exigirRede()
        await this.pedido(daNota(id), this.daOrigem({ method: 'DELETE' }))
        const n = this.notas.get(id)
        if (n) this.guardar({ ...n, deleted_at: n.deleted_at ?? this.agora().toISOString() })
      },
      restaurarNota: async (id) => {
        this.exigirRede()
        return this.lerNota(await this.pedido(`${daNota(id)}/restore`, this.daOrigem({ method: 'POST' })))
      },
      moverNota: async (id, antes, depois) => {
        this.exigirRede()
        return this.lerNota(await this.pedido(`${daNota(id)}/move`, this.comJson('POST', { antes, depois })))
      },
      apagarNotaDeVez: async (id) => {
        this.exigirRede()
        await this.pedido(`${daNota(id)}/purge`, this.daOrigem({ method: 'POST' }), false)
        this.esquecerNota(id)
        if (this.fila.has(id)) this.tirarDaFila(id)
      },
      resolverConflitoDaNota: async (id, versao, escolha) => {
        this.exigirRede()
        return this.lerNota(await this.pedido(daNota(id), this.comJson('PATCH', { base_version: this.versaoDe(id, versao), resolver_conflito: escolha })))
      },
      listarMarcadores: async (sinal) => {
        await this.pronto
        try {
          const r = await pedir(this.buscar, '/api/v1/labels', this.daOrigem({ signal: sinal }))
          this.vivo()
          const { items } = (await r.json()) as { items?: Marcador[] | null }
          this.marcadores = (items ?? []).map(doMarcador)
          void this.armazem.gravarMarcadores(this.marcadores).catch(() => undefined)
        } catch (e) {
          if (!(e instanceof ForaDoAr)) throw e
          this.caiu()
        }
        return [...this.marcadores]
      },
      criarMarcador: async (nome) => {
        this.exigirRede()
        // conflitoEhVersao false: o 409 daqui é o nome já usado (NAME_IN_USE).
        const r = await this.pedido('/api/v1/labels', this.comJson('POST', { nome }), false)
        return doMarcador((await r.json()) as Marcador)
      },
      renomearMarcador: async (id, versao, nome) => {
        this.exigirRede()
        const r = await this.pedido(doMarcadorUrl(id), this.comJson('PATCH', { nome, base_version: versao }), false)
        return doMarcador((await r.json()) as Marcador)
      },
      apagarMarcador: async (id) => {
        this.exigirRede()
        await this.pedido(doMarcadorUrl(id), this.daOrigem({ method: 'DELETE' }), false)
      },
      marcarNota: async (notaId, marcadorId) => {
        this.exigirRede()
        return this.lerNota(await this.pedido(`${daNota(notaId)}/labels/${encodeURIComponent(marcadorId)}`, this.daOrigem({ method: 'PUT' })))
      },
      desmarcarNota: async (notaId, marcadorId) => {
        this.exigirRede()
        return this.lerNota(await this.pedido(`${daNota(notaId)}/labels/${encodeURIComponent(marcadorId)}`, this.daOrigem({ method: 'DELETE' })))
      },
      imagens: this.criarClienteDeImagens(),
      // O "nota em foco" é do PC (o ditado do Agent que cai na nota aberta); no
      // celular, o microfone da nota está no próprio app (DitarNaNota).
      definirFoco: async () => {},
      limparFoco: async () => {},
      // A conversão com IA da N5 mora no Agent do PC; no celular, a regra (a
      // mesma das listas: vírgula, ponto e vírgula e o último " e ").
      separarItens: async (trecho) => {
        const itens = itensDaEnumeracao(trecho, 2)
        return itens ? { itens, como: 'regra' } : { itens: [], como: 'nenhum' }
      },
    }
  }
}
