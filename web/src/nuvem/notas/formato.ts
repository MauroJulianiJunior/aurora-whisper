// A nota e o marcador como a nuvem os manda (ADR-022, N7 e N8; 06, "Notas e
// marcadores (nuvem)"), e a tradução para o formato do painel (notas/tipos.ts).
// A nuvem manda sempre as chaves (null quando não há): cor, ditado_de_origem,
// deleted_at e versao_em_conflito. O painel quer o que não há ausente, e o selo
// em "conflito", como a API local do PC.

import { CORES, type CorDaNota, type ImagemDaNota, type Marcador, type Nota } from '../../notas/tipos'

export interface SeloDaNuvem {
  titulo: string
  corpo: string
  device_id: string | null
  conflict_at: string
}

// ImagemDaNuvem é a imagem como a nuvem a manda (06, "Imagens das notas (nuvem)"):
// só os metadados, nunca os bytes (que vêm do GET da imagem, com a sessão).
export interface ImagemDaNuvem {
  id: string
  note_id?: string
  largura: number
  altura: number
  bytes: number
  sha256?: string
  created_at: string
}

export interface NotaDaNuvem {
  id: string
  titulo: string
  corpo: string
  cor: string | null
  fixada: boolean
  arquivada: boolean
  posicao: string
  marcadores: string[] | null
  ditado_de_origem: string | null
  created_at: string
  updated_at: string
  deleted_at: string | null
  version: number
  versao_em_conflito: SeloDaNuvem | null
  // imagens: sempre um array na nuvem (N10b); ausente num servidor de antes.
  imagens?: ImagemDaNuvem[] | null
  // gravacao: só na resposta do PATCH (N8): "mesclada" ou "no_selo".
  gravacao?: string
}

const ehCor = (c: unknown): c is CorDaNota => typeof c === 'string' && (CORES as readonly string[]).includes(c)

// imagemDaNuvem traduz a imagem da nuvem para a do painel (sem o note_id, que o
// painel já sabe pela nota).
export function imagemDaNuvem(i: ImagemDaNuvem): ImagemDaNota {
  const im: ImagemDaNota = { id: i.id, largura: i.largura, altura: i.altura, bytes: i.bytes, created_at: i.created_at }
  if (i.sha256) im.sha256 = i.sha256
  return im
}

// daNuvem traduz a nota da nuvem para a do painel.
export function daNuvem(n: NotaDaNuvem): Nota {
  const nota: Nota = {
    id: n.id,
    titulo: n.titulo ?? '',
    corpo: n.corpo ?? '',
    cor: ehCor(n.cor) ? n.cor : null,
    fixada: n.fixada === true,
    arquivada: n.arquivada === true,
    marcadores: n.marcadores ?? [],
    posicao: n.posicao ?? '',
    created_at: n.created_at,
    updated_at: n.updated_at,
    version: n.version,
    imagens: (n.imagens ?? []).map(imagemDaNuvem),
  }
  if (n.ditado_de_origem) nota.ditado_de_origem = n.ditado_de_origem
  if (n.deleted_at) nota.deleted_at = n.deleted_at
  if (n.versao_em_conflito) {
    const c = n.versao_em_conflito
    nota.conflito = { titulo: c.titulo, corpo: c.corpo, device_id: c.device_id, conflict_at: c.conflict_at }
  }
  if (n.gravacao === 'mesclada' || n.gravacao === 'no_selo') nota.gravacao = n.gravacao
  return nota
}

// doMarcador lê o marcador da nuvem (as mesmas chaves do painel).
export function doMarcador(m: Marcador): Marcador {
  return { id: m.id, nome: m.nome, created_at: m.created_at, updated_at: m.updated_at, version: m.version }
}

// A vista de uma nota: a lixeira, o Arquivo ou a tela principal.
export function vistaDa(n: Nota): 'lixeira' | 'arquivo' | 'ativas' {
  if (n.deleted_at) return 'lixeira'
  return n.arquivada ? 'arquivo' : 'ativas'
}

// antesNaLista é a ordem da nuvem (cloudstore.NoteBefore): na lixeira, a que foi
// para lá por último primeiro; nas outras, as fixadas, depois a posição (pelos
// bytes, como o COLLATE "C") e o id. É com ela que a lista sem rede sai na mesma
// ordem que a nuvem daria.
export function antesNaLista(a: Nota, b: Nota): number {
  if (a.deleted_at || b.deleted_at) {
    const da = a.deleted_at ?? ''
    const db = b.deleted_at ?? ''
    if (da !== db) return da > db ? -1 : 1
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  }
  if (a.fixada !== b.fixada) return a.fixada ? -1 : 1
  if (a.posicao !== b.posicao) return a.posicao < b.posicao ? -1 : 1
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
}
