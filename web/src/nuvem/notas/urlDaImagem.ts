// A URL blob: da imagem da nota no celular (ADR-022, N10b). É o ÚNICO lugar do app
// da nuvem que cria uma URL de blob, e só aceita um JPEG: a imagem reduzida que a
// pessoa escolheu, ou a que a nuvem devolveu. O áudio do ditado nunca passa por
// aqui (nem chega a este arquivo: o gravador, em ../ditar/, não o importa), e o
// bundle.test.ts confere as duas coisas.
//
// A URL vive só na memória da página: a loja da aba (notas/imagensNaTela.ts) a solta
// quando a aba sai. Nada vai para o IndexedDB, nem para o Cache API, nem para o
// service worker.

export function urlDaImagem(blob: Blob): string {
  if (blob.type !== 'image/jpeg') throw new Error('só a imagem da nota vira URL blob:')
  return URL.createObjectURL(blob)
}

export function soltarUrlDaImagem(url: string): void {
  URL.revokeObjectURL(url)
}
