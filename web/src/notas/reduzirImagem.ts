// A redução da imagem no navegador, antes de ela sair da página (ADR-022, "As
// imagens (N10)", os padrões que valem sem pergunta): no máximo 2.000 px no lado
// maior, em JPEG, com a orientação da foto. Desenhar no canvas e gravar de novo
// tira a localização (GPS) e o resto dos metadados que a foto do celular carrega;
// o Agent confere de novo e guarda sem eles. Só a cópia reduzida sai daqui: o
// original nunca é mandado.
//
// As ferramentas do navegador (createImageBitmap, o canvas e o toBlob) entram
// por parâmetro: o jsdom não tem canvas, e os testes passam dublês
// (reduzirImagem.test.ts).

import { ErroDaImagem, MAX_LADO, MAX_ORIGINAL, MAX_REDUZIDA } from './imagens'

// Bitmap é o que createImageBitmap devolve, no que a redução usa.
export interface Bitmap {
  width: number
  height: number
  close?(): void
}

// FerramentasDaImagem são as três pontas do navegador.
export interface FerramentasDaImagem<B extends Bitmap = Bitmap, C = unknown> {
  // abrir decodifica o arquivo, com a orientação da foto (EXIF) já aplicada.
  abrir(arquivo: Blob): Promise<B>
  // desenhar faz um canvas largura x altura com fundo branco (o PNG transparente
  // não vira preto no JPEG) e a imagem esticada nele.
  desenhar(bitmap: B, largura: number, altura: number): C
  // gravar grava o canvas em JPEG, na qualidade dada (0 a 1).
  gravar(canvas: C, qualidade: number): Promise<Blob | null>
}

// As qualidades, da primeira à última tentativa: a primeira é a do padrão
// (~0,85); as outras só entram se a cópia passar do teto do Agent.
export const QUALIDADES = [0.85, 0.72, 0.6] as const

export interface ImagemReduzida {
  jpeg: Blob
  largura: number
  altura: number
}

// tamanhoReduzido é o tamanho da cópia: o lado maior em até MAX_LADO, a
// proporção mantida, nunca menos de 1 px.
export function tamanhoReduzido(largura: number, altura: number): { largura: number; altura: number } {
  const escala = Math.min(1, MAX_LADO / Math.max(largura, altura))
  return { largura: Math.max(1, Math.round(largura * escala)), altura: Math.max(1, Math.round(altura * escala)) }
}

// reduzirImagem reduz o arquivo e devolve o JPEG. Recusa (ErroDaImagem) o
// original acima de 10 MB, o que o navegador não abre, e a cópia que, mesmo na
// última qualidade, passa do teto do Agent.
export async function reduzirImagem<B extends Bitmap, C>(arquivo: Blob, ferramentas: FerramentasDaImagem<B, C>): Promise<ImagemReduzida> {
  if (arquivo.size > MAX_ORIGINAL) throw new ErroDaImagem('grande')
  if (arquivo.type !== '' && !arquivo.type.startsWith('image/')) throw new ErroDaImagem('nao-abre')
  let bitmap: B
  try {
    bitmap = await ferramentas.abrir(arquivo)
  } catch {
    throw new ErroDaImagem('nao-abre')
  }
  try {
    if (!(bitmap.width > 0 && bitmap.height > 0)) throw new ErroDaImagem('nao-abre')
    const { largura, altura } = tamanhoReduzido(bitmap.width, bitmap.height)
    let canvas: C
    try {
      canvas = ferramentas.desenhar(bitmap, largura, altura)
    } catch {
      throw new ErroDaImagem('nao-abre')
    }
    for (const q of QUALIDADES) {
      const jpeg = await ferramentas.gravar(canvas, q)
      // O navegador que não grava JPEG devolve PNG (ou nada): isso não sai daqui.
      if (!jpeg || jpeg.type !== 'image/jpeg' || jpeg.size === 0) throw new ErroDaImagem('nao-abre')
      if (jpeg.size <= MAX_REDUZIDA) return { jpeg, largura, altura }
    }
    throw new ErroDaImagem('grande-reduzida')
  } finally {
    bitmap.close?.()
  }
}

// ferramentasDoNavegador são as de verdade. createImageBitmap com
// imageOrientation 'from-image' gira a foto como a câmera mandou; o canvas tem
// fundo branco e a suavização alta; o toBlob grava o JPEG (o navegador grava só
// os pixels: nada do EXIF original vai junto).
export const ferramentasDoNavegador: FerramentasDaImagem<ImageBitmap, HTMLCanvasElement> = {
  abrir: (arquivo) => createImageBitmap(arquivo, { imageOrientation: 'from-image' }),
  desenhar(bitmap, largura, altura) {
    const canvas = document.createElement('canvas')
    canvas.width = largura
    canvas.height = altura
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('sem o contexto 2d')
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(0, 0, largura, altura)
    ctx.imageSmoothingEnabled = true
    ctx.imageSmoothingQuality = 'high'
    ctx.drawImage(bitmap, 0, 0, largura, altura)
    return canvas
  },
  gravar: (canvas, qualidade) => new Promise((resolver) => canvas.toBlob(resolver, 'image/jpeg', qualidade)),
}
