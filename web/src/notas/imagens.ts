// As regras das imagens das notas do lado do painel (ADR-022, "As imagens
// (N10)"): os limites, as frases da cena 7 da prévia e o que serve de imagem num
// colar ou num arrastar. Só funções puras: nada aqui fala com a rede nem desenha.
//
// O teto do total (400 MB) não mora aqui: é do Agent (imagemdanota.TetoDoTotal),
// e a frase dele vem pronta na resposta IMAGES_FULL, com o número.

import { ErroDaApi, ForaDoAr, problemaDe } from '../api'
import type { ImagemDaNota, Nota } from './tipos'

// Os limites do painel. MAX_POR_NOTA e MAX_LADO são os mesmos do Agent
// (internal/imagemdanota); MAX_ORIGINAL é o do arquivo que a pessoa escolhe,
// conferido só aqui (o original nunca sai do navegador).
export const MAX_POR_NOTA = 10
export const MAX_LADO = 2000
export const MAX_ORIGINAL = 10 * 1024 * 1024
// MAX_REDUZIDA é o teto do Agent para a cópia reduzida (imagemdanota.MaxBytes).
export const MAX_REDUZIDA = 4 * 1024 * 1024

// As frases (a cena 7 da prévia, como a ADR as fixou).
export const FRASES = {
  tirada: 'Imagem tirada da nota.',
  grande: 'A imagem passou de 10 MB.',
  cheia: 'A nota já tem 10 imagens.',
  naoAbre: 'Esse arquivo não é uma imagem que o navegador abra.',
  semRede: 'A rede caiu antes de a imagem subir. Tente de novo.',
  // [derivada] A cópia reduzida que ainda passa de 4 MB (a redução tenta três
  // qualidades antes): na prática não acontece, e a frase diz o que houve.
  grandeReduzida: 'A imagem ficou grande demais mesmo reduzida.',
} as const

// ErroDaImagem é a recusa do lado do navegador, antes de qualquer pedido: o
// original grande demais, o arquivo que o navegador não abre, a cópia que não
// cabe nem reduzida.
export class ErroDaImagem extends Error {
  readonly motivo: 'grande' | 'nao-abre' | 'grande-reduzida'
  constructor(motivo: 'grande' | 'nao-abre' | 'grande-reduzida') {
    super(motivo === 'grande' ? FRASES.grande : motivo === 'nao-abre' ? FRASES.naoAbre : FRASES.grandeReduzida)
    this.name = 'ErroDaImagem'
    this.motivo = motivo
  }
}

const mensagemDe = (e: unknown) => (e instanceof Error ? e.message : String(e))

// fraseDoErro é o que o aviso diz quando anexar não deu certo. Os códigos são os
// do Agent (06-api-contract.md, "Imagens das notas"). O IMAGES_FULL traz a frase
// pronta, com o teto.
export function fraseDoErro(e: unknown): string {
  if (e instanceof ErroDaImagem) return e.message
  if (e instanceof ForaDoAr || problemaDe(e) === 'fora-do-ar') return FRASES.semRede
  if (e instanceof ErroDaApi) {
    switch (e.codigo) {
      case 'IMAGES_FULL':
        return e.message
      case 'TOO_MANY_IMAGES':
        return FRASES.cheia
      case 'IMAGE_INVALID':
        return FRASES.naoAbre
      case 'IMAGE_TOO_LARGE':
        return FRASES.grandeReduzida
    }
    if (e.status === 404) return 'Esta nota não existe mais; a imagem não foi anexada.'
    if (e.status === 409) return 'Esta nota foi para a lixeira em outra aba; a imagem não foi anexada.'
  }
  return `Não consegui anexar a imagem: ${mensagemDe(e).replace(/\.$/, '')}.`
}

// imagensDaNota é a lista de imagens da nota, que a nuvem (ainda) não manda.
export const imagensDaNota = (n: Pick<Nota, 'imagens'>): ImagemDaNota[] => n.imagens ?? []

// ehImagem diz se o arquivo pode ser uma imagem: o tipo que o Windows deu, ou
// nenhum (a extensão que o navegador não conhece). Quem decide de verdade é o
// navegador, ao abrir (reduzirImagem.ts).
const ehImagem = (f: File) => f.type === '' || f.type.startsWith('image/')

// arquivosDoArrastar são as imagens de um arrastar de arquivos do Windows (os
// outros arquivos ficam de fora; um arrastar só deles vira o aviso "não abre").
// tudo é a lista inteira, para quem quer dizer que algo ficou de fora.
export function arquivosDoArrastar(dados: DataTransfer | null): { imagens: File[]; tudo: File[] } {
  const tudo = Array.from(dados?.files ?? [])
  return { imagens: tudo.filter(ehImagem), tudo }
}

// arrastaArquivos diz se o arrastar traz arquivos de fora (e não um texto ou um
// item da própria página): só assim a nota mostra "Solte para anexar à nota".
export const arrastaArquivos = (dados: DataTransfer | null) => Array.from(dados?.types ?? []).includes('Files')

// imagensDoColar são as imagens de um Ctrl+V. Com texto junto (copiar uma célula do
// Excel traz o texto e a imagem dela; um trecho do Word, o texto e o desenho), o
// texto vence: o colar é o de sempre, e nada se anexa. O print (Win+Shift+S) vem
// só com a imagem.
export function imagensDoColar(dados: DataTransfer | null): File[] {
  if (!dados) return []
  if ((dados.getData('text/plain') ?? '').trim() !== '') return []
  const daLista = Array.from(dados.files ?? []).filter((f) => f.type.startsWith('image/'))
  if (daLista.length > 0) return daLista
  // Alguns navegadores só põem a imagem nos itens.
  const dosItens: File[] = []
  for (const item of Array.from(dados.items ?? [])) {
    if (item.kind !== 'file' || !item.type.startsWith('image/')) continue
    const f = item.getAsFile()
    if (f) dosItens.push(f)
  }
  return dosItens
}

// nomeComImagens é o nome da nota que só tem imagem: o "Nota vazia" de
// nomeDaNota não serve a ela.
export function nomeComImagens(nome: string, n: number): string {
  if (nome !== 'Nota vazia' || n === 0) return nome
  return n === 1 ? 'Nota com imagem' : `Nota com ${n} imagens`
}

// contaDeImagens é o "· 5 imagens" da janela da nota e da lixeira.
export const contaDeImagens = (n: number) => (n === 1 ? '1 imagem' : `${n} imagens`)
