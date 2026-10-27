// As notas de mentira da nuvem, para os testes do celular (ADR-022, N8): as
// rotas /api/v1/notes e /api/v1/labels que o cliente das notas usa, em memória,
// a rede que cai e volta (o pedido que não chega é um TypeError, como o fetch de
// verdade), e o PATCH que o teste roteiriza (a mescla, o selo, a recusa: as
// regras de verdade são do servidor, e a suíte de contrato do Go as cobre).
//
// Junta-se à nuvem de mentira do histórico (servidorFalsoNuvem) por juntar():
// o que é das notas vem para cá, o resto vai para lá.

import type { Buscador } from '../../api'
import type { NotaDaNuvem } from './formato'

export interface PedidoDeNota {
  metodo: string
  caminho: string
  corpo?: Record<string, unknown>
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
    ...parcial,
  }
}

type RespostaDoPatch = (nota: NotaDaNuvem, corpo: Record<string, unknown>) => Response | 'aplicar' | 'aplicar-e-cair'

const json = (status: number, corpo: unknown) => new Response(JSON.stringify(corpo), { status, headers: { 'Content-Type': 'application/json' } })
const erro = (status: number, codigo: string, mensagem: string) => json(status, { error: { code: codigo, message: mensagem } })

export function notasFalsasNaNuvem(iniciais: NotaDaNuvem[] = []) {
  const notas = new Map(iniciais.map((n) => [n.id, { ...n }]))
  const pedidos: PedidoDeNota[] = []
  let semRede = false
  let proximosPatches: RespostaDoPatch[] = []
  let seq = 100

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
    pedidos.push({ metodo, caminho: url.pathname + url.search, corpo })
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
    if (partes[2] === 'notes' && partes[3]) {
      const n = notas.get(decodeURIComponent(partes[3]))
      if (!n) return erro(404, 'NOT_FOUND', 'nota não encontrada')
      if (partes.length === 4 && metodo === 'GET') return json(200, n)
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
