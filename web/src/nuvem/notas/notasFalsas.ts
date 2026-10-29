// As notas de mentira da nuvem, para os testes do celular (ADR-022, N8): as
// rotas /api/v1/notes e /api/v1/labels que o cliente das notas usa, em memória,
// a rede que cai e volta (o pedido que não chega é um TypeError, como o fetch de
// verdade), e o PATCH que o teste roteiriza (a mescla, o selo, a recusa: as
// regras de verdade são do servidor, e a suíte de contrato do Go as cobre).
//
// Junta-se à nuvem de mentira do histórico (servidorFalsoNuvem) por juntar():
// o que é das notas vem para cá, o resto vai para lá.

import type { Buscador } from '../../api'
import type { ImagemDaNuvem, NotaDaNuvem } from './formato'

export interface PedidoDeNota {
  metodo: string
  caminho: string
  corpo?: Record<string, unknown>
  // tipo e bytes: o corpo que não é JSON, a imagem da nota (N10b): o Content-Type e
  // o tamanho do que subiu.
  tipo?: string
  bytes?: number
}

// imagemDaNuvemFalsa monta a imagem de uma nota como a nuvem a manda (N10b), só os
// metadados.
export function imagemFalsa(parcial: Partial<ImagemDaNuvem> & { id: string }): ImagemDaNuvem {
  return { largura: 1600, altura: 1200, bytes: 400_000, sha256: 'a'.repeat(64), created_at: '2026-10-27T10:00:00Z', ...parcial }
}

// O JPEG mínimo (SOI, EOI): serve de bytes quando o teste não pede outros. A nuvem
// de verdade confere o JPEG (imagemdanota.Conferir); aqui o que se prova é o que o
// app FAZ com a resposta.
const JPEG_MINIMO = () => new Blob([new Uint8Array([0xff, 0xd8, 0xff, 0xd9])], { type: 'image/jpeg' })

export interface OpcoesDasNotasFalsas {
  // bytesDaImagem: os bytes que o GET da imagem devolve (a tela de desenvolvimento
  // gera JPEG de verdade num canvas). Sem ele, o JPEG mínimo.
  bytesDaImagem?(imagem: ImagemDaNuvem): Blob | Promise<Blob>
}

export function notaDaNuvem(parcial: Partial<NotaDaNuvem> & { id: string }): NotaDaNuvem {
  return {
    titulo: '',
    corpo: '',
    cor: null,
    fixada: false,
    arquivada: false,
    posicao: 'V',
    marcadores: [],
    ditado_de_origem: null,
    created_at: '2026-10-27T10:00:00Z',
    updated_at: '2026-10-27T10:00:00Z',
    deleted_at: null,
    version: 1,
    versao_em_conflito: null,
    imagens: [],
    ...parcial,
  }
}

type RespostaDoPatch = (nota: NotaDaNuvem, corpo: Record<string, unknown>) => Response | 'aplicar' | 'aplicar-e-cair'

const json = (status: number, corpo: unknown) => new Response(JSON.stringify(corpo), { status, headers: { 'Content-Type': 'application/json' } })
const erro = (status: number, codigo: string, mensagem: string) => json(status, { error: { code: codigo, message: mensagem } })

export function notasFalsasNaNuvem(iniciais: NotaDaNuvem[] = [], opcoes: OpcoesDasNotasFalsas = {}) {
  const notas = new Map(iniciais.map((n) => [n.id, { ...n }]))
  const pedidos: PedidoDeNota[] = []
  let semRede = false
  let proximosPatches: RespostaDoPatch[] = []
  let seq = 100
  // As imagens (N10b): os bytes que subiram, por id, e as recusas que o teste
  // roteiriza para os próximos anexos (o teto, a nota cheia, a rede).
  const bytesDasImagens = new Map<string, Blob>()
  let proximosAnexos: Response[] = []
  let seqDasImagens = 0

  // guardarImagem recebe o JPEG de um anexo e devolve a imagem, ou a recusa.
  function guardarImagem(noteId: string, tipo: string, blob: Blob | undefined): ImagemDaNuvem | Response {
    const roteirizada = proximosAnexos.shift()
    if (roteirizada) return roteirizada
    if (tipo !== 'image/jpeg' || !blob) return erro(415, 'IMAGE_INVALID', 'mande a imagem como image/jpeg')
    if (blob.size > 4 * 1024 * 1024) return erro(413, 'IMAGE_TOO_LARGE', 'a imagem passou do tamanho máximo, mesmo reduzida')
    const im = imagemFalsa({ id: `img-${++seqDasImagens}`, note_id: noteId, largura: 800, altura: 600, bytes: blob.size, created_at: '2026-10-27T13:00:00Z' })
    bytesDasImagens.set(im.id, blob)
    return im
  }

  function rotaDeImagens(n: NotaDaNuvem, partes: string[], metodo: string, tipo: string, blob: Blob | undefined): Response | Promise<Response> {
    const imagemId = partes[5] ? decodeURIComponent(partes[5]) : undefined
    if (!imagemId) {
      if (metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
      if (n.deleted_at) return erro(409, 'CONFLICT', 'a nota está na lixeira: restaure antes de mexer nela')
      if ((n.imagens ?? []).length >= 10) return erro(409, 'TOO_MANY_IMAGES', 'a nota já tem 10 imagens')
      const im = guardarImagem(n.id, tipo, blob)
      if (im instanceof Response) return im
      notas.set(n.id, { ...n, imagens: [...(n.imagens ?? []), im] })
      return json(201, im)
    }
    const im = (n.imagens ?? []).find((x) => x.id === imagemId)
    if (metodo === 'GET') {
      if (!im) return erro(404, 'NOT_FOUND', 'registro não encontrado')
      const bytes = bytesDasImagens.get(im.id)
      const corpo: Blob | Promise<Blob> = bytes ?? opcoes.bytesDaImagem?.(im) ?? JPEG_MINIMO()
      // no-store: a nuvem de verdade manda o mesmo, e o celular nunca guarda a imagem.
      return Promise.resolve(corpo).then((b) => new Response(b, { status: 200, headers: { 'Content-Type': 'image/jpeg', 'Cache-Control': 'no-store' } }))
    }
    if (metodo === 'DELETE') {
      notas.set(n.id, { ...n, imagens: (n.imagens ?? []).filter((x) => x.id !== imagemId) })
      bytesDasImagens.delete(imagemId)
      return new Response(null, { status: 204 })
    }
    return erro(405, 'BAD_REQUEST', 'método')
  }

  // aplicar grava o conteúdo do PATCH (a base é a do pedido; o conteúdo vale).
  function aplicar(n: NotaDaNuvem, corpo: Record<string, unknown>): NotaDaNuvem {
    const nova: NotaDaNuvem = { ...n, version: n.version + 1, updated_at: '2026-10-27T12:00:00Z' }
    for (const k of ['titulo', 'corpo', 'cor', 'fixada', 'arquivada'] as const) if (k in corpo) (nova as unknown as Record<string, unknown>)[k] = corpo[k]
    notas.set(n.id, nova)
    return nova
  }

  const buscar: Buscador = async (entrada, init = {}) => {
    if (semRede) throw new TypeError('Failed to fetch')
    const url = new URL(entrada, 'http://localhost')
    const metodo = (init.method ?? 'GET').toUpperCase()
    const corpo = typeof init.body === 'string' && init.body !== '' ? (JSON.parse(init.body) as Record<string, unknown>) : undefined
    const blob = init.body instanceof Blob ? init.body : undefined
    const tipo = new Headers(init.headers).get('Content-Type') ?? ''
    pedidos.push({ metodo, caminho: url.pathname + url.search, corpo, ...(blob ? { tipo, bytes: blob.size } : {}) })
    const partes = url.pathname.split('/').filter(Boolean) // api v1 notes {id} {acao}
    if (url.pathname === '/api/v1/labels' && metodo === 'GET') return json(200, { items: [], has_more: false })
    if (url.pathname === '/api/v1/notes' && metodo === 'GET') {
      const vista = url.searchParams.get('vista') ?? 'ativas'
      const items = [...notas.values()].filter((n) =>
        vista === 'lixeira' ? n.deleted_at !== null : n.deleted_at === null && n.arquivada === (vista === 'arquivo'),
      )
      return json(200, { items, has_more: false })
    }
    if (url.pathname === '/api/v1/notes' && metodo === 'POST') {
      const id = `nova-${++seq}`
      const n = notaDaNuvem({ id, titulo: String(corpo?.titulo ?? ''), corpo: String(corpo?.corpo ?? ''), posicao: '0V' })
      notas.set(id, n)
      return json(201, n)
    }
    if (url.pathname === '/api/v1/notes/with-image' && metodo === 'POST') {
      const id = `nova-${++seq}`
      const im = guardarImagem(id, tipo, blob)
      if (im instanceof Response) return im
      const n = notaDaNuvem({ id, posicao: '0V', imagens: [im] })
      notas.set(id, n)
      return json(201, n)
    }
    if (partes[2] === 'notes' && partes[3]) {
      const n = notas.get(decodeURIComponent(partes[3]))
      if (!n) return erro(404, 'NOT_FOUND', 'nota não encontrada')
      if (partes.length === 4 && metodo === 'GET') return json(200, n)
      if (partes[4] === 'images' && partes.length <= 6) return rotaDeImagens(n, partes, metodo, tipo, blob)
      if (partes.length === 4 && metodo === 'PATCH') {
        const roteiro = proximosPatches.shift()
        const r = roteiro ? roteiro(n, corpo ?? {}) : 'aplicar'
        if (r === 'aplicar') return json(200, aplicar(n, corpo ?? {}))
        if (r === 'aplicar-e-cair') {
          aplicar(n, corpo ?? {})
          throw new TypeError('Failed to fetch')
        }
        return r
      }
    }
    return erro(404, 'NOT_FOUND', 'rota não encontrada')
  }

  return {
    notas,
    pedidos,
    buscar,
    cair: () => {
      semRede = true
    },
    voltar: () => {
      semRede = false
    },
    // noProximoPatch roteiriza a resposta dos próximos PATCH, na ordem.
    noProximoPatch: (...r: RespostaDoPatch[]) => {
      proximosPatches = [...proximosPatches, ...r]
    },
    // As imagens (N10b): os bytes que subiram, e a recusa dos próximos anexos.
    bytesDasImagens,
    noProximoAnexo: (...r: Response[]) => {
      proximosAnexos = [...proximosAnexos, ...r]
    },
    anexos: () => pedidos.filter((p) => p.metodo === 'POST' && /\/images$|\/with-image$/.test(p.caminho)),
    patches: () => pedidos.filter((p) => p.metodo === 'PATCH').map((p) => p.corpo),
    responder: json,
    erro,
    // atende diz se o caminho é das notas (para juntar com a nuvem do histórico).
    atende: (entrada: string) => /^\/api\/v1\/(notes|labels)(\/|\?|$)/.test(new URL(entrada, 'http://localhost').pathname + '/'),
  }
}

// juntar manda o que é das notas para as notas de mentira e o resto para a
// nuvem do histórico.
export function juntar(notas: ReturnType<typeof notasFalsasNaNuvem>, resto: Buscador): Buscador {
  return (entrada, init) => (notas.atende(entrada) ? notas.buscar(entrada, init) : resto(entrada, init))
}
