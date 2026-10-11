// Uma nuvem de mentira para os testes do app da nuvem (ADR-019, F4): responde
// as rotas da /api/v1 e do /auth que o app usa, em memória, com as regras que
// importam para a tela — a sessão pelo cookie (aqui, um booleano: o cookie é
// HttpOnly e o app nunca o vê), base_version no PATCH, o selo e a escolha
// dele, a lixeira por deleted_at, o "excluir definitivamente" só da lixeira,
// a busca só por POST (um q na URL é 400, como no servidor), o "apagar meus
// dados" só com {"confirmar":"apagar"}, e o login de teste que só existe
// quando o teste pede.
//
// O item sai como a nuvem o manda: deleted_at e versao_em_conflito sempre
// presentes (null quando não há), e next_cursor "" na última página.

import type { Buscador, Ditado, VersaoEmConflito } from '../api'

export interface PedidoNuvem {
  metodo: string
  caminho: string
  busca: URLSearchParams
  corpo?: Record<string, unknown>
  cabecalhos: Record<string, string>
  credentials?: RequestCredentials
}

// O item na nuvem, com os campos que só ela manda.
export type ItemNuvem = Omit<Ditado, 'deleted_at' | 'versao_em_conflito'> & {
  deleted_at: string | null
  versao_em_conflito: VersaoEmConflito | null
  device_id: string | null
  source_platform: string
}

export function itemNuvem(parcial: Partial<ItemNuvem> & { id: string; created_at: string }): ItemNuvem {
  return {
    raw_text: parcial.final_text ?? '',
    final_text: '',
    mode: 'smart',
    asr_provider: 'whisper-local',
    status: 'done',
    updated_at: parcial.created_at,
    version: 1,
    deleted_at: null,
    versao_em_conflito: null,
    device_id: '0192a000-0000-7000-8000-0000000000d1',
    source_platform: 'windows',
    ...parcial,
  }
}

export interface SessaoNuvem {
  id: string
  rotulo: string
  criada_em: string
  ultimo_uso_em: string
  atual: boolean
}

// AparelhoNuvem é uma linha de GET /api/v1/devices (F6).
export interface AparelhoNuvem {
  id: string
  nome: string
  plataforma: string
  versao: string
  conectado_em: string
  ultimo_uso_em: string
}

// PedidoFalso é um pedido de pareamento em curso (F6): o que a página de
// aprovação lê, e a porta do painel do PC para onde a aprovação devolve.
export interface PedidoFalso {
  nome: string
  plataforma: string
  versao: string
  conferencia: string
  expira_em: string
  porta: number
}

// AprendidoNuvem é uma linha de GET /api/v1/learned (F8): a troca que um PC
// aprendeu. Como o servidor manda: dominio "" sem domínio.
export interface AprendidoNuvem {
  id: string
  termo: string
  variante: string
  restrito: boolean
  dominio: string
}

// aprendido monta uma troca com os padrões de uma sem domínio; o id é um UUID
// (o servidor recusa com 404 o que não é).
export function aprendido(parcial: Partial<AprendidoNuvem> & { variante: string; termo: string }): AprendidoNuvem {
  const n = [...parcial.variante].reduce((s, c) => (s * 31 + c.charCodeAt(0)) % 0x1_0000_0000, 7)
  return {
    id: `0192f0a1-7c3e-7d4a-9b2f-${n.toString(16).padStart(12, '0')}`,
    restrito: false,
    dominio: '',
    ...parcial,
  }
}

export interface OpcoesDaNuvem {
  // logado: a sessão já existe ao abrir.
  logado?: boolean
  email?: string
  porPagina?: number
  sessoes?: SessaoNuvem[]
  // teste: o que o GET /auth/teste responde; ausente, 404 (todo servidor
  // de verdade).
  teste?: { conta: string; google: boolean }
  // aparelhos: os PCs conectados à conta (F6).
  aparelhos?: AparelhoNuvem[]
  // pedidos: os pedidos de pareamento em curso, por id (F6). Aprovar um o
  // tira daqui (o segundo é 404, como no servidor).
  pedidos?: Record<string, PedidoFalso>
  // aprendidos: as trocas aprendidas, do mais recente para o mais antigo (F8).
  aprendidos?: AprendidoNuvem[]
  // tetoDosAprendidos: quantas o GET devolve antes de dizer has_more (o
  // servidor de verdade usa 500; o teste põe um número pequeno).
  tetoDosAprendidos?: number
}

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })
const erro = (status: number, code: string, message: string) => json({ error: { code, message, request_id: 'r1' } }, status)
const vazio = (status = 204) => new Response(null, { status })

function ordenar(itens: ItemNuvem[], campo: 'created_at' | 'deleted_at'): ItemNuvem[] {
  const t = (d: ItemNuvem) => Date.parse(d[campo] ?? d.created_at)
  return itens.sort((a, b) => t(b) - t(a) || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0))
}

export function servidorFalsoNuvem(
  inicial: ItemNuvem[],
  {
    logado = true,
    email = 'voce@teste.local',
    porPagina = 50,
    sessoes = [],
    teste,
    aparelhos = [],
    pedidos: pedidosIniciais = {},
    aprendidos = [],
    tetoDosAprendidos = 500,
  }: OpcoesDaNuvem = {},
) {
  const itens = inicial.map((i) => ({ ...i }))
  const listaDeSessoes = sessoes.map((s) => ({ ...s }))
  const listaDeAparelhos = aparelhos.map((a) => ({ ...a }))
  // As trocas vivas, e as que foram desfeitas (o desfazer repetido é 200).
  const listaDeAprendidos = aprendidos.map((a) => ({ ...a }))
  const desfeitos: AprendidoNuvem[] = []
  const pedidosDePareamento = new Map(Object.entries(pedidosIniciais).map(([id, p]) => [id, { ...p }]))
  const pedidos: PedidoNuvem[] = []
  let sessao = logado
  let conta = { id: 'u1', email }
  let foraDoAr = false
  let interceptar: ((p: PedidoNuvem) => Response | undefined) | null = null

  function rotaDoItem(p: PedidoNuvem, id: string, acao?: string): Response {
    const item = itens.find((i) => i.id === decodeURIComponent(id))
    if (!item) return erro(404, 'NOT_FOUND', 'transcrição não encontrada')
    if (acao === 'restore' && p.metodo === 'POST') {
      if (item.deleted_at) {
        item.deleted_at = null
        item.version += 1
      }
      return json(item)
    }
    if (acao === 'purge' && p.metodo === 'POST') {
      if (!item.deleted_at) return erro(409, 'CONFLICT', 'o ditado não está na lixeira')
      itens.splice(itens.indexOf(item), 1)
      return vazio()
    }
    if (acao) return erro(404, 'NOT_FOUND', 'rota não encontrada')
    switch (p.metodo) {
      case 'GET':
        return json(item)
      case 'PATCH': {
        const c = p.corpo ?? {}
        const temTexto = 'final_text' in c
        const temEscolha = 'resolver_conflito' in c
        if (typeof c.base_version !== 'number' || temTexto === temEscolha) return erro(400, 'VALIDATION', 'corpo inválido')
        if (temEscolha && c.resolver_conflito !== 'manter' && c.resolver_conflito !== 'trocar') return erro(400, 'VALIDATION', 'escolha inválida')
        if (c.base_version !== item.version) return erro(409, 'CONFLICT', 'Entity version is stale')
        if (temEscolha) {
          if (!item.versao_em_conflito) return erro(409, 'CONFLICT', 'o ditado não tem versão em conflito')
          if (c.resolver_conflito === 'trocar') item.final_text = item.versao_em_conflito.final_text
          item.versao_em_conflito = null
        } else {
          item.final_text = String(c.final_text)
        }
        item.version += 1
        return json(item)
      }
      case 'DELETE':
        if (!item.deleted_at) {
          item.deleted_at = new Date().toISOString()
          item.version += 1
        }
        return vazio()
    }
    return erro(404, 'NOT_FOUND', 'rota não encontrada')
  }

  function listar(lixeira: boolean, q: string, cursor: string): Response {
    const busca = q.toLowerCase()
    const lista = ordenar(
      itens.filter((i) => Boolean(i.deleted_at) === lixeira).filter((i) => !busca || i.final_text.toLowerCase().includes(busca) || i.raw_text.toLowerCase().includes(busca)),
      lixeira ? 'deleted_at' : 'created_at',
    )
    const inicio = Number(cursor || 0)
    const fim = inicio + porPagina
    return json({ items: lista.slice(inicio, fim).map((i) => ({ ...i })), next_cursor: fim < lista.length ? String(fim) : '' })
  }

  const buscar: Buscador = async (entrada, init = {}) => {
    if (foraDoAr) throw new TypeError('Failed to fetch')
    const url = new URL(entrada, 'http://localhost:8080')
    const p: PedidoNuvem = {
      metodo: (init.method ?? 'GET').toUpperCase(),
      caminho: url.pathname,
      busca: url.searchParams,
      corpo: typeof init.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : undefined,
      cabecalhos: { ...((init.headers ?? {}) as Record<string, string>) },
      credentials: init.credentials,
    }
    pedidos.push(p)
    const forcada = interceptar?.(p)
    if (forcada) return forcada

    // O /auth é público.
    if (p.caminho === '/auth/teste') {
      if (!teste) return erro(404, 'NOT_FOUND', 'rota não encontrada')
      if (p.metodo === 'GET') return json(teste)
      if (p.corpo?.email !== teste.conta) return erro(403, 'FORBIDDEN', 'conta fora da lista')
      sessao = true
      conta = { id: 'u1', email }
      return vazio()
    }
    if (p.caminho === '/auth/start' && p.metodo === 'POST') {
      if (teste && !teste.google) return erro(404, 'NOT_FOUND', 'rota não encontrada')
      return json({ url: 'https://accounts.google.com/o/oauth2/v2/auth?client_id=c&state=s' })
    }
    if (p.caminho === '/auth/logout' && p.metodo === 'POST') {
      sessao = false
      return vazio()
    }
    if (!p.caminho.startsWith('/api/v1/')) return erro(404, 'NOT_FOUND', 'rota não encontrada')
    if (!sessao) return erro(401, 'UNAUTHORIZED', 'autenticação obrigatória')

    if (p.caminho === '/api/v1/me' && p.metodo === 'GET') return json(conta)
    if (p.caminho === '/api/v1/me/data' && p.metodo === 'DELETE') {
      if (p.corpo?.confirmar !== 'apagar') return erro(400, 'VALIDATION', 'confirmação obrigatória')
      itens.splice(0, itens.length)
      for (let i = listaDeSessoes.length - 1; i >= 0; i--) if (!listaDeSessoes[i]!.atual) listaDeSessoes.splice(i, 1)
      // Os aparelhos também (decisão 3 da F3): os PCs ficam desconectados.
      listaDeAparelhos.splice(0, listaDeAparelhos.length)
      // E o aprendido (F8): tudo da conta.
      listaDeAprendidos.splice(0, listaDeAprendidos.length)
      desfeitos.splice(0, desfeitos.length)
      return vazio()
    }
    // O aprendido (F8), como o servidor (internal/server/learned_routes.go): a
    // lista só tem os vivos, até o teto, com has_more; o desfazer leva o id no
    // corpo; o já desfeito responde 200 de novo; o que não existe, o que não é
    // um UUID e o corpo sem id são o 404 e o 400 do servidor. Nunca texto na
    // URL: um parâmetro na busca é ignorado.
    if (p.caminho === '/api/v1/learned' && p.metodo === 'GET') {
      return json({ items: listaDeAprendidos.slice(0, tetoDosAprendidos), has_more: listaDeAprendidos.length > tetoDosAprendidos })
    }
    if (p.caminho === '/api/v1/learned/undo' && p.metodo === 'POST') {
      if (typeof p.corpo?.id !== 'string') return erro(400, 'BAD_REQUEST', 'corpo inválido')
      const id = p.corpo.id.toLowerCase()
      const i = listaDeAprendidos.findIndex((a) => a.id === id)
      if (i >= 0) {
        const [feito] = listaDeAprendidos.splice(i, 1)
        desfeitos.push(feito!)
        return json(feito)
      }
      const jaDesfeito = desfeitos.find((a) => a.id === id)
      if (jaDesfeito) return json(jaDesfeito)
      return erro(404, 'NOT_FOUND', 'troca aprendida não encontrada')
    }
    // O pareamento (F6): ler e aprovar o pedido, com a sessão. Vencido, usado
    // ou desconhecido é o mesmo 404.
    const pedidoRota = p.caminho.match(/^\/api\/v1\/device-auth\/requests\/([^/]+)(\/approve)?$/)
    if (pedidoRota) {
      const [, id, aprovar] = pedidoRota
      const ped = pedidosDePareamento.get(decodeURIComponent(id!))
      if (!ped) return erro(404, 'NOT_FOUND', 'pedido de pareamento vencido, já usado ou desconhecido')
      if (!aprovar && p.metodo === 'GET') {
        const { nome, plataforma, versao, conferencia, expira_em } = ped
        return json({ nome, plataforma, versao, conferencia, expira_em })
      }
      if (aprovar && p.metodo === 'POST') {
        pedidosDePareamento.delete(id!)
        return json({ retorno: `http://127.0.0.1:${ped.porta}/nuvem/retorno?code=c0de-de-teste&state=${id}` })
      }
      return erro(404, 'NOT_FOUND', 'rota não encontrada')
    }
    if (p.caminho === '/api/v1/devices' && p.metodo === 'GET') return json({ items: listaDeAparelhos })
    const aparelhoId = p.caminho.match(/^\/api\/v1\/devices\/([^/]+)$/)?.[1]
    if (aparelhoId && p.metodo === 'DELETE') {
      const i = listaDeAparelhos.findIndex((a) => a.id === aparelhoId)
      if (i < 0) return erro(404, 'NOT_FOUND', 'aparelho não encontrado')
      listaDeAparelhos.splice(i, 1)
      return vazio()
    }
    if (p.caminho === '/api/v1/sessions' && p.metodo === 'GET') return json({ items: listaDeSessoes })
    const sessaoId = p.caminho.match(/^\/api\/v1\/sessions\/([^/]+)$/)?.[1]
    if (sessaoId && p.metodo === 'DELETE') {
      const i = listaDeSessoes.findIndex((s) => s.id === sessaoId)
      if (i < 0) return erro(404, 'NOT_FOUND', 'sessão não encontrada')
      listaDeSessoes.splice(i, 1)
      return vazio()
    }

    if (p.caminho === '/api/v1/transcriptions' && p.metodo === 'GET') {
      if (p.busca.has('q')) return erro(400, 'VALIDATION', 'a busca é por POST')
      return listar(p.busca.get('trash') === 'true', '', p.busca.get('cursor') ?? '')
    }
    if (p.caminho === '/api/v1/transcriptions/search' && p.metodo === 'POST') {
      const c = p.corpo ?? {}
      return listar(c.trash === true, String(c.q ?? ''), String(c.cursor ?? ''))
    }
    const rota = p.caminho.match(/^\/api\/v1\/transcriptions\/([^/]+)(?:\/(restore|purge))?$/)
    if (rota) return rotaDoItem(p, rota[1]!, rota[2])
    return erro(404, 'NOT_FOUND', 'rota não encontrada')
  }

  return {
    buscar,
    pedidos,
    itens,
    sessoes: listaDeSessoes,
    aparelhos: listaDeAparelhos,
    aprendidos: listaDeAprendidos,
    desfeitos,
    pedidosDePareamento,
    get logado() {
      return sessao
    },
    // expirar: a sessão venceu (ou foi encerrada em outro aparelho).
    expirar() {
      sessao = false
    },
    // entrar: a pessoa entrou de novo por fora do app (a janela à parte do
    // Google). outra: com outra conta.
    entrar(outra?: { id: string; email: string }) {
      sessao = true
      conta = outra ?? { id: 'u1', email }
    },
    derrubar(sim: boolean) {
      foraDoAr = sim
    },
    interceptar(f: ((p: PedidoNuvem) => Response | undefined) | null) {
      interceptar = f
    },
    // conflitoPorFora: o PC mandou outro texto com a base velha; o dele vai
    // para o selo.
    conflitoPorFora(id: string, textoDoPC: string) {
      const item = itens.find((i) => i.id === id)
      if (!item) throw new Error(id)
      item.versao_em_conflito = { final_text: textoDoPC, device_id: '0192a000-0000-7000-8000-0000000000d2', conflict_at: new Date().toISOString() }
      item.version += 1
    },
    alterarPorFora(id: string, texto: string) {
      const item = itens.find((i) => i.id === id)
      if (!item) throw new Error(id)
      item.final_text = texto
      item.version += 1
    },
    // desfazerPorFora: outro aparelho (um PC, ou o celular em outra aba)
    // desfez a troca; o id continua aceito no desfazer (200), como no servidor.
    desfazerPorFora(id: string) {
      const i = listaDeAprendidos.findIndex((a) => a.id === id)
      if (i < 0) throw new Error(id)
      desfeitos.push(...listaDeAprendidos.splice(i, 1))
    },
    restaurarPorFora(id: string) {
      const item = itens.find((i) => i.id === id)
      if (!item) throw new Error(id)
      item.deleted_at = null
      item.version += 1
    },
  }
}
