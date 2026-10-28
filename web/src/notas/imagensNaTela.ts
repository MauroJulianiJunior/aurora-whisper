// O que a aba Notas sabe das imagens enquanto ela vive (ADR-022, N10): as URLs
// blob: das imagens já lidas, as que se anexaram nesta visita, as que estão
// "guardando…" e as que foram tiradas da nota (esperando o Desfazer vencer, ou já
// tiradas de vez). Uma loja só, para o cartão da grade, a nota aberta e a imagem
// grande mostrarem a mesma coisa; os componentes a leem pelo
// useSyncExternalStore (ImagensDaNota.tsx).
//
// A imagem chega à tela por fetch com o token (ClienteDeImagens.ler) e vira uma
// URL blob:, guardada aqui até a aba sair (fechar, que solta todas). Nada vai
// para o disco do navegador: nem cache, nem localStorage, nem IndexedDB.

import { ErroDaApi, problemaDe } from '../api'
import { FRASES, MAX_POR_NOTA, fraseDoErro, imagensDaNota } from './imagens'
import type { ImagemReduzida } from './reduzirImagem'
import type { ClienteDeImagens, ImagemDaNota, Nota } from './tipos'

// Guardando é a imagem que já foi reduzida e está subindo: o quadrado com o
// "guardando…", desenhado da própria cópia reduzida.
export interface Guardando {
  chave: string
  url: string
  largura: number
  altura: number
}

// AvisarComDesfazer é o avisar do Painel, com o Desfazer e o aoVencer.
export type AvisarComDesfazer = (texto: string, desfazer?: () => void, abrir?: () => void, aoVencer?: () => void) => void

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))
const porCriacao = (a: ImagemDaNota, b: ImagemDaNota) => (a.created_at < b.created_at ? -1 : a.created_at > b.created_at ? 1 : 0)

export class ImagensNaTela {
  private readonly urls = new Map<string, string>()
  private readonly pedidos = new Set<string>()
  private readonly falhas = new Set<string>()
  // novas: as imagens anexadas nesta visita, por nota, até a lista das notas
  // as trazer (a lista e as novas se juntam pelo id).
  private readonly novas = new Map<string, ImagemDaNota[]>()
  private readonly subindo = new Map<string, Guardando[]>()
  // ocultas: as tiradas da nota. Ficam escondidas mesmo depois de saírem de vez:
  // a lista que está na tela ainda pode trazê-las até ser lida de novo.
  private readonly ocultas = new Set<string>()
  private readonly ouvintes = new Set<() => void>()
  private n = 0
  private sequencia = 0
  private fechada = false
  // fila: uma anexação depois da outra, na ordem em que a pessoa escolheu (a
  // ordem das imagens na nota é a da criação).
  private fila: Promise<unknown> = Promise.resolve()

  private readonly cliente: ClienteDeImagens
  private readonly reduzir: (arquivo: Blob) => Promise<ImagemReduzida>

  constructor(cliente: ClienteDeImagens, reduzir: (arquivo: Blob) => Promise<ImagemReduzida>) {
    this.cliente = cliente
    this.reduzir = reduzir
  }

  assinar = (cb: () => void) => {
    this.ouvintes.add(cb)
    return () => {
      this.ouvintes.delete(cb)
    }
  }

  versao = () => this.n

  private mudou() {
    this.n++
    for (const cb of this.ouvintes) cb()
  }

  url(id: string): string | undefined {
    return this.urls.get(id)
  }

  falhou(id: string): boolean {
    return this.falhas.has(id)
  }

  // pedir lê a imagem, se ela ainda não foi lida nem está sendo. A falha fica
  // anotada (o quadrado diz "Imagem indisponível") e não se repete sozinha.
  pedir(notaId: string, id: string) {
    if (this.fechada || this.urls.has(id) || this.pedidos.has(id) || this.falhas.has(id)) return
    this.pedidos.add(id)
    this.cliente.ler(notaId, id).then(
      (blob) => {
        this.pedidos.delete(id)
        if (this.fechada) return
        this.urls.set(id, this.cliente.paraATela(blob))
        this.mudou()
      },
      () => {
        this.pedidos.delete(id)
        if (this.fechada) return
        this.falhas.add(id)
        this.mudou()
      },
    )
  }

  // imagensDe são as imagens da nota como a tela deve mostrá-las agora: as da
  // lista e as anexadas nesta visita, sem as tiradas, na ordem da criação.
  imagensDe(nota: Pick<Nota, 'id' | 'imagens'>): ImagemDaNota[] {
    const vistas = new Set<string>()
    const todas: ImagemDaNota[] = []
    for (const im of [...imagensDaNota(nota), ...(this.novas.get(nota.id) ?? [])]) {
      if (vistas.has(im.id) || this.ocultas.has(im.id)) continue
      vistas.add(im.id)
      todas.push(im)
    }
    return todas.sort(porCriacao)
  }

  guardandoDe(notaId: string): readonly Guardando[] {
    return this.subindo.get(notaId) ?? []
  }

  // anexar reduz e manda os arquivos, um de cada vez, à nota. O que não dá certo
  // vira o aviso da ADR, e o resto continua (menos a nota cheia e o teto, que
  // param). Devolve quantas imagens entraram.
  anexar(nota: Pick<Nota, 'id' | 'imagens'>, arquivos: readonly Blob[], avisar: (texto: string) => void, aoPerderLigacao?: (e: unknown) => void): Promise<number> {
    const passo = async (): Promise<number> => {
      let entraram = 0
      for (const arquivo of arquivos) {
        if (this.fechada) break
        if (this.imagensDe(nota).length + this.guardandoDe(nota.id).length >= MAX_POR_NOTA) {
          avisar(FRASES.cheia)
          break
        }
        let reduzida: ImagemReduzida
        try {
          reduzida = await this.reduzir(arquivo)
        } catch (e) {
          avisar(fraseDoErro(e))
          continue
        }
        const g: Guardando = { chave: `g${++this.sequencia}`, url: this.cliente.paraATela(reduzida.jpeg), largura: reduzida.largura, altura: reduzida.altura }
        this.subindo.set(nota.id, [...this.guardandoDe(nota.id), g])
        this.mudou()
        try {
          const im = await this.cliente.anexar(nota.id, reduzida.jpeg)
          this.tirarDoGuardando(nota.id, g)
          if (this.fechada) {
            this.cliente.soltar(g.url)
            break
          }
          // A cópia que subiu é a que o Agent guardou (o canvas não grava
          // metadado): a tela fica com ela, sem pedir de novo.
          this.urls.set(im.id, g.url)
          this.novas.set(nota.id, [...(this.novas.get(nota.id) ?? []), im])
          entraram++
          this.mudou()
        } catch (e) {
          this.tirarDoGuardando(nota.id, g)
          this.cliente.soltar(g.url)
          this.mudou()
          if (problemaDe(e)) aoPerderLigacao?.(e)
          avisar(fraseDoErro(e))
          if (e instanceof ErroDaApi && (e.codigo === 'TOO_MANY_IMAGES' || e.codigo === 'IMAGES_FULL')) break
        }
      }
      return entraram
    }
    const p = this.fila.then(passo, passo)
    this.fila = p
    return p
  }

  private tirarDoGuardando(notaId: string, g: Guardando) {
    const resto = this.guardandoDe(notaId).filter((x) => x !== g)
    if (resto.length > 0) this.subindo.set(notaId, resto)
    else this.subindo.delete(notaId)
  }

  // criarNotaComImagem é o ícone de imagem do "Criar uma nota…": reduz o arquivo e
  // cria a nota só com ele. Os erros sobem como vieram (quem chama avisa com
  // fraseDoErro).
  async criarNotaComImagem(arquivo: Blob): Promise<Nota> {
    const reduzida = await this.reduzir(arquivo)
    const nota = await this.cliente.criarNotaComImagem(reduzida.jpeg)
    const im = imagensDaNota(nota)[0]
    if (im && !this.fechada) {
      this.urls.set(im.id, this.cliente.paraATela(reduzida.jpeg))
      this.mudou()
    }
    return nota
  }

  // tirar é o × da imagem e o "Tirar da nota" da imagem grande: a imagem some da
  // tela na hora, e o aviso oferece o Desfazer. Só quando ele sai sem o Desfazer
  // (aoVencer) o pedido vai ao Agent, e a imagem sai de vez. A que já não estava lá
  // (404) conta como tirada; a falha devolve a imagem à tela, com o aviso.
  tirar(notaId: string, imagem: ImagemDaNota, avisar: AvisarComDesfazer, aoPerderLigacao?: (e: unknown) => void) {
    this.ocultas.add(imagem.id)
    this.mudou()
    let desfeita = false
    const desfazer = () => {
      desfeita = true
      this.ocultas.delete(imagem.id)
      this.mudou()
    }
    const vencer = () => {
      if (desfeita) return
      this.cliente.tirar(notaId, imagem.id).then(
        () => this.esquecer(imagem.id),
        (e: unknown) => {
          if (e instanceof ErroDaApi && e.status === 404) return this.esquecer(imagem.id)
          this.ocultas.delete(imagem.id)
          this.mudou()
          if (problemaDe(e)) aoPerderLigacao?.(e)
          avisar(`Não consegui tirar a imagem da nota: ${mensagemDe(e).replace(/\.$/, '')}. Ela continua lá.`)
        },
      )
    }
    avisar(FRASES.tirada, desfazer, undefined, vencer)
  }

  // esquecer solta a URL da imagem que saiu de vez. Ela continua escondida.
  private esquecer(id: string) {
    const u = this.urls.get(id)
    if (u) {
      this.cliente.soltar(u)
      this.urls.delete(id)
    }
    this.mudou()
  }

  // abrir é a aba que (re)aparece: a loja volta a guardar o que chega. O React,
  // no modo estrito, monta, desmonta e monta de novo; a loja é a mesma.
  abrir() {
    this.fechada = false
  }

  // fechar é a aba que sai: todas as URLs são soltas, e o que ainda chegar é
  // jogado fora.
  fechar() {
    this.fechada = true
    for (const u of this.urls.values()) this.cliente.soltar(u)
    for (const gs of this.subindo.values()) for (const g of gs) this.cliente.soltar(g.url)
    this.urls.clear()
    this.subindo.clear()
  }
}
