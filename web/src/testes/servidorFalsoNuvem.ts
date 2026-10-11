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
  // audio: o corpo de um ditado (POST /api/v1/dictations), o áudio cru.
  audio?: Blob
  cabecalhos: Record<string, string>
  credentials?: RequestCredentials
}

// DitadoDoCelular é um ditado que o servidor de mentira recebeu (ADR-021): o
// que o app mandou, e se o provedor foi chamado por ele.
export interface DitadoDoCelular {
  id: string
  bytes: number
  tipo: string
  duracaoMs: number
  falaMs: number
  // chamouOProvedor: false no reenvio do mesmo id (a idempotência).
  chamouOProvedor: boolean
}

// ComoDitar é o que o provedor de mentira devolve no próximo ditado.
export interface ComoDitar {
  // texto: o que o ASR (e o Smart) escreveram.
  texto: string
  // aviso: o motivo de o Polish ter caído para o Smart (um ia.Motivo).
  aviso?: string
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

// O ditado pelo celular na conta (ADR-021, C3a e C3b), do lado do servidor de
// mentira. Ele fala o contrato REAL do servidor (06-api-contract.md, "As chaves
// da IA" e "O aceite e as preferências do ditado no celular"; o código em
// internal/server/ditado_celular.go e chaves_ia.go, e a suíte executável em
// internal/server/chavestest): o que sai e o que entra é o JSON do servidor, e
// o contratoDoDitado.test.ts prende as formas dele ao 06. Os tipos são DESTE
// arquivo, e não os de src/nuvem: o servidor não importa o app (o ESLint também
// não deixa src/ importar src/nuvem), e quem os prende um ao outro é o teste
// de nuvem/ditar/aviso.test.ts.
export type ProvedorFalso = 'openai' | 'groq' | 'anthropic'
const provedoresFalsos: ProvedorFalso[] = ['openai', 'groq', 'anthropic']
const provedoresDeVozFalsos: string[] = ['openai', 'groq']
const provedoresDePolishFalsos: string[] = ['openai', 'anthropic']

// A versão do aviso que o servidor de mentira considera a atual (no de verdade,
// cloudstore.VersaoDoAviso). É um inteiro, e é a mesma de
// web/src/nuvem/avisoDoDitado.ts (VERSAO_DO_AVISO): quem prende os dois é o
// aviso.test.ts, e quem prende aquela à do servidor de verdade é o teste do Go
// TestVersaoDoAviso_AppEServidorConferem.
export const versaoDoAvisoNoServidorFalso = 1

// Os modelos padrão da transcrição (cloudstore.ModeloPadraoOpenAI e
// ModeloPadraoGroq): o PUT com o modelo vazio os resolve, e o que se lê é sempre
// o nome de um modelo. O Polish não tem modelo padrão.
export const modeloPadraoFalso: Record<'openai' | 'groq', string> = {
  openai: 'gpt-4o-mini-transcribe',
  groq: 'whisper-large-v3-turbo',
}

// As preferências como o servidor de mentira as guarda: planas, como a tela as
// vê. No fio elas são {transcricao, dica, polish}, e é assim que o servidor as
// lê e as devolve.
export interface PreferenciasFalsas {
  provedor: 'openai' | 'groq'
  modelo: string
  dica: boolean
  polish: boolean
  polish_provedor: 'openai' | 'anthropic'
  polish_modelo: string
}

// AceiteFalso é o aceite em aberto da conta, como o servidor o guarda: a versão
// do aviso, o provedor da transcrição e o do Polish (null: o aviso não incluía o
// Polish). Um aceite novo substitui o anterior.
export interface AceiteFalso {
  versao: number
  transcricao: 'openai' | 'groq'
  polish: 'openai' | 'anthropic' | null
  aceito_em: string
}

// DitadoDaConta é o que o servidor de mentira guarda do ditado pelo celular. As
// chaves ficam INTEIRAS aqui (é o servidor): o que o app recebe é só o final, e
// os testes conferem que a chave inteira não volta. colarDeNovo são os
// provedores que têm uma linha cifrada por outra chave de cifra: o servidor não
// a decifra, e a tela pede a chave de novo.
export interface DitadoDaConta {
  preferencias: PreferenciasFalsas
  aceite: AceiteFalso | null
  chaves: Partial<Record<ProvedorFalso, string>>
  colarDeNovo: ProvedorFalso[]
}

// Os modelos que cada provedor lista para a chave (os de voz e os de texto,
// misturados como o /models de verdade: quem filtra é o app).
const modelosFalsos: Record<ProvedorFalso, string[]> = {
  openai: ['gpt-4o-transcribe', 'gpt-4o-mini-transcribe', 'whisper-1', 'gpt-4o', 'gpt-4o-mini'],
  groq: ['whisper-large-v3-turbo', 'whisper-large-v3', 'llama-3.3-70b-versatile'],
  anthropic: ['claude-haiku-4-5', 'claude-sonnet-4-5'],
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
  // ditado: o ditado pelo celular da conta ao abrir (ADR-021). Sem esta opção,
  // a conta já está pronta (aceite de todos os provedores, uma chave de cada),
  // e os testes que não são do aceite não precisam prepará-la. Com ela, o que
  // não vem é vazio: sem aceite, sem chaves, preferências no padrão.
  ditado?: {
    preferencias?: Partial<PreferenciasFalsas>
    aceite?: AceiteFalso | null
    chaves?: Partial<Record<ProvedorFalso, string>>
    colarDeNovo?: ProvedorFalso[]
  }
  // versaoDoAviso: a versão do aviso que o servidor aceita no consentimento.
  versaoDoAviso?: number
}

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })
const erro = (status: number, code: string, message: string) => json({ error: { code, message, request_id: 'r1' } }, status)
const vazio = (status = 204) => new Response(null, { status })
// O id do ditado é um UUID v7 (o 05 pede v7): versão 7 e variante 10.
const pareceUUID = (s: string) => /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(s)

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
    ditado: ditadoInicial,
    versaoDoAviso: versaoInicial = versaoDoAvisoNoServidorFalso,
  }: OpcoesDaNuvem = {},
) {
  // A versão do aviso que o servidor aceita agora (sobe com subirOAvisoPorFora).
  let versaoAtual = versaoInicial
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
  // O ditado do celular (ADR-021): cada pedido que chegou, o que o provedor de
  // mentira devolve, e as lápides (um id apagado de vez é 410, sem provedor).
  const ditados: DitadoDoCelular[] = []
  const lapides = new Set<string>()
  let comoDitar: ComoDitar = { texto: 'Texto ditado pelo celular.' }

  // O ditado pelo celular da conta: aceite, preferências e chaves. Sem a opção
  // `ditado`, a conta nasce pronta (aceite da OpenAI para a voz e da Anthropic
  // para o Polish, uma chave de cada, as preferências no padrão do servidor).
  const preferenciasIniciais = (parcial: Partial<PreferenciasFalsas> = {}): PreferenciasFalsas => {
    const p: PreferenciasFalsas = {
      provedor: 'openai',
      modelo: '',
      dica: false,
      polish: false,
      polish_provedor: 'openai',
      polish_modelo: '',
      ...parcial,
    }
    // O servidor guarda sempre o modelo de verdade, o padrão do provedor se
    // ninguém escolheu (cloudstore.PreferenciasPadrao).
    if (p.modelo === '') p.modelo = modeloPadraoFalso[p.provedor]
    // E nunca guarda o Polish ligado sem modelo (ConferirPreferencias): um
    // teste que parte disso parte de uma conta que não existe.
    if (p.polish && p.polish_modelo === '') throw new Error('servidorFalsoNuvem: o servidor só guarda o Polish ligado com um modelo')
    return p
  }
  const daConta: DitadoDaConta = ditadoInicial
    ? {
        preferencias: preferenciasIniciais(ditadoInicial.preferencias),
        aceite: ditadoInicial.aceite ? { ...ditadoInicial.aceite } : null,
        chaves: { ...ditadoInicial.chaves },
        colarDeNovo: [...(ditadoInicial.colarDeNovo ?? [])],
      }
    : {
        preferencias: preferenciasIniciais(),
        aceite: { versao: versaoAtual, transcricao: 'openai', polish: 'anthropic', aceito_em: '2026-10-10T12:00:00.000Z' },
        chaves: { openai: 'sk-teste-de-mentira-OPEN', groq: 'gsk-teste-de-mentira-GROQ', anthropic: 'sk-ant-teste-de-mentira-ANTH' },
        colarDeNovo: [],
      }
  // cobre é a conferência do servidor (cloudstore.Aceite.Vigente e Inclui): o
  // aceite em aberto, da versão atual do aviso, que tem o provedor num dos dois
  // lugares, o da transcrição ou o do Polish.
  const cobre = (provedor: string) => {
    const a = daConta.aceite
    return a !== null && a.versao === versaoAtual && (a.transcricao === provedor || a.polish === provedor)
  }
  const conferido = (provedores: ProvedorFalso[]) => provedores.every(cobre)
  const exigidos = (p: PreferenciasFalsas): ProvedorFalso[] => (p.polish && p.polish_provedor !== p.provedor ? [p.provedor, p.polish_provedor] : [p.provedor])
  const daChave = (p: ProvedorFalso) => {
    const chave = daConta.chaves[p]
    if (chave) return { provedor: p, configurada: true, final: chave.slice(-4), colar_de_novo: false }
    return { provedor: p, configurada: false, final: '', colar_de_novo: daConta.colarDeNovo.includes(p) }
  }
  // O estado como o servidor o manda: no GET, e também no PUT das preferências e
  // no POST do aceite (200).
  const estadoDoDitado = () => ({
    aviso: { versao: versaoAtual },
    aceite: daConta.aceite && { ...daConta.aceite, vigente: daConta.aceite.versao === versaoAtual },
    preferencias: {
      transcricao: { provedor: daConta.preferencias.provedor, modelo: daConta.preferencias.modelo },
      dica: daConta.preferencias.dica,
      polish: { ligado: daConta.preferencias.polish, provedor: daConta.preferencias.polish_provedor, modelo: daConta.preferencias.polish_modelo },
    },
    chaves: provedoresFalsos.map(daChave),
  })
  const ehProvedor = (v: unknown): v is ProvedorFalso => provedoresFalsos.includes(v as ProvedorFalso)
  const ehObjeto = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
  // soTem: o objeto não tem campo fora da lista (o decodificador do servidor
  // recusa o campo desconhecido).
  const soTem = (o: Record<string, unknown>, campos: string[]) => Object.keys(o).every((k) => campos.includes(k))
  const formaDeModelo = /^[A-Za-z0-9._:/@-]{1,100}$/

  // As rotas do ditado pelo celular na conta (ADR-021, "O aceite e as
  // preferências" e "As chaves"), como o servidor as responde: o estado nos três
  // verbos que o devolvem (GET, PUT das preferências, POST do aceite); o aceite
  // só com a versão atual do aviso (a outra é 409 AVISO_DESATUALIZADO) e
  // provedores conhecidos; as preferências, a chave e a lista de modelos só para
  // um provedor dentro do aceite vigente (SEM_ACEITE, 403); o Polish só liga com
  // um modelo; o modelo da transcrição vazio vira o padrão do provedor; revogar o
  // aceite apaga as chaves e deixa as preferências; a chave inteira nunca volta
  // (só `configurada`, o final e o `colar_de_novo`); o provedor que recusa a
  // chave é a que começa com "recusada". Corpo com campo fora da lista ou de tipo
  // errado é 400 BAD_REQUEST, com uma frase fixa.
  function rotaDoDitado(p: PedidoNuvem): Response | undefined {
    if (p.caminho === '/api/v1/dictation' && p.metodo === 'GET') return json(estadoDoDitado())
    if (p.caminho === '/api/v1/dictation/settings' && p.metodo === 'PUT') return gravarPreferencias(p)
    if (p.caminho === '/api/v1/dictation/consent' && p.metodo === 'POST') return gravarAceite(p)
    if (p.caminho === '/api/v1/dictation/consent' && p.metodo === 'DELETE') {
      daConta.aceite = null
      daConta.chaves = {}
      daConta.colarDeNovo = []
      return vazio()
    }
    const rotaDaChave = p.caminho.match(/^\/api\/v1\/ai-keys\/([^/]+)(\/models)?$/)
    if (rotaDaChave) return rotaDeChave(p, decodeURIComponent(rotaDaChave[1]!), rotaDaChave[2] !== undefined)
    return undefined
  }

  // PUT /dictation/settings: o corpo, a forma, o aceite, a gravação.
  function gravarPreferencias(p: PedidoNuvem): Response {
    const frase = 'corpo inválido: mande transcricao, dica e polish'
    const c = p.corpo
    if (!ehObjeto(c) || !soTem(c, ['transcricao', 'dica', 'polish'])) return erro(400, 'BAD_REQUEST', frase)
    const { transcricao, dica, polish } = c
    // Os três campos são obrigatórios (faltando um, VALIDATION). Dentro deles, o
    // campo que falta vale o vazio (ou falso), e o de tipo errado é BAD_REQUEST.
    if (transcricao == null || dica == null || polish == null) return erro(400, 'VALIDATION', 'faltam campos: mande transcricao, dica e polish')
    if (!ehObjeto(transcricao) || !soTem(transcricao, ['provedor', 'modelo'])) return erro(400, 'BAD_REQUEST', frase)
    if (!ehObjeto(polish) || !soTem(polish, ['ligado', 'provedor', 'modelo'])) return erro(400, 'BAD_REQUEST', frase)
    const texto = (o: Record<string, unknown>, campo: string) => (o[campo] === undefined ? '' : o[campo])
    const provedor = texto(transcricao, 'provedor')
    const modelo = texto(transcricao, 'modelo')
    const ligado = polish.ligado === undefined ? false : polish.ligado
    const polishProvedor = texto(polish, 'provedor')
    const polishModelo = texto(polish, 'modelo')
    if (typeof dica !== 'boolean' || typeof ligado !== 'boolean') return erro(400, 'BAD_REQUEST', frase)
    if (typeof provedor !== 'string' || typeof modelo !== 'string' || typeof polishProvedor !== 'string' || typeof polishModelo !== 'string') {
      return erro(400, 'BAD_REQUEST', frase)
    }
    const novas = { provedor, modelo, dica, polish: ligado, polish_provedor: polishProvedor, polish_modelo: polishModelo } as PreferenciasFalsas
    // O modelo da transcrição vazio é o padrão do provedor.
    if (novas.modelo === '' && (novas.provedor === 'openai' || novas.provedor === 'groq')) novas.modelo = modeloPadraoFalso[novas.provedor]
    // ConferirPreferencias, na ordem do servidor.
    if (!provedoresDeVozFalsos.includes(novas.provedor)) return erro(400, 'VALIDATION', 'a transcrição é feita pela openai ou pela groq')
    if (!formaDeModelo.test(novas.modelo)) return erro(400, 'VALIDATION', 'o nome do modelo só pode ter letras, dígitos e . _ : / @ - (de 1 a 100 caracteres)')
    if (!provedoresDePolishFalsos.includes(novas.polish_provedor)) return erro(400, 'VALIDATION', 'o Polish é feito pela openai ou pela anthropic')
    if (novas.polish_modelo !== '' && !formaDeModelo.test(novas.polish_modelo)) return erro(400, 'VALIDATION', 'o nome do modelo só pode ter letras, dígitos e . _ : / @ - (de 1 a 100 caracteres)')
    if (novas.polish && novas.polish_modelo === '') return erro(400, 'VALIDATION', 'o Polish ligado precisa de um modelo')
    if (!conferido(exigidos(novas))) return erro(403, 'SEM_ACEITE', 'aceite o aviso do ditado no celular para este provedor antes')
    daConta.preferencias = novas
    return json(estadoDoDitado())
  }

  // POST /dictation/consent: o corpo, a versão (AVISO_DESATUALIZADO), os
  // provedores, a gravação. O aceite em aberto é substituído; aceitar de novo o
  // mesmo não muda a data da primeira vez.
  function gravarAceite(p: PedidoNuvem): Response {
    const frase = 'corpo inválido: mande versao, transcricao e, se houver, polish'
    const c = p.corpo
    if (!ehObjeto(c) || !soTem(c, ['versao', 'transcricao', 'polish'])) return erro(400, 'BAD_REQUEST', frase)
    const { versao, transcricao, polish } = c
    if (versao !== undefined && !(typeof versao === 'number' && Number.isInteger(versao))) return erro(400, 'BAD_REQUEST', frase)
    if (transcricao !== undefined && typeof transcricao !== 'string') return erro(400, 'BAD_REQUEST', frase)
    if (polish !== undefined && polish !== null && typeof polish !== 'string') return erro(400, 'BAD_REQUEST', frase)
    if (versao === undefined || transcricao === undefined) return erro(400, 'VALIDATION', 'faltam campos: mande versao e transcricao')
    if (versao !== versaoAtual) {
      return erro(409, 'AVISO_DESATUALIZADO', 'o aviso que o app mostrou não é o atual: recarregue o app e leia o aviso de novo')
    }
    // "" não é um provedor: "sem Polish" é null ou a falta do campo.
    if (polish === '') return erro(400, 'VALIDATION', 'o Polish é feito pela openai ou pela anthropic')
    if (!provedoresDeVozFalsos.includes(transcricao as string)) return erro(400, 'VALIDATION', 'a transcrição é feita pela openai ou pela groq')
    if (typeof polish === 'string' && !provedoresDePolishFalsos.includes(polish)) return erro(400, 'VALIDATION', 'o Polish é feito pela openai ou pela anthropic')
    const voz = transcricao as AceiteFalso['transcricao']
    const pol = (typeof polish === 'string' ? polish : null) as AceiteFalso['polish']
    const atual = daConta.aceite
    const igual = atual !== null && atual.versao === versao && atual.transcricao === voz && atual.polish === pol
    if (!igual) daConta.aceite = { versao, transcricao: voz, polish: pol, aceito_em: new Date().toISOString() }
    return json(estadoDoDitado())
  }

  // /ai-keys/{provedor} e /ai-keys/{provedor}/models. O provedor desconhecido é
  // 404 antes de qualquer outra conferência; o aceite, antes de ler o corpo.
  function rotaDeChave(p: PedidoNuvem, provedor: string, modelos: boolean): Response | undefined {
    if (!ehProvedor(provedor)) return erro(404, 'NOT_FOUND', 'provedor desconhecido')
    if (modelos) {
      if (p.metodo !== 'GET') return erro(404, 'NOT_FOUND', 'rota não encontrada')
      if (!cobre(provedor)) return erro(403, 'SEM_ACEITE', 'aceite o aviso do ditado no celular para este provedor antes')
      // Sem chave, ou com a que o servidor já não decifra: o mesmo SEM_CHAVE.
      if (!daConta.chaves[provedor]) return erro(409, 'SEM_CHAVE', 'nenhuma chave guardada para este provedor')
      return json({ provedor, modelos: modelosFalsos[provedor] })
    }
    if (p.metodo === 'PUT') {
      if (!cobre(provedor)) return erro(403, 'SEM_ACEITE', 'aceite o aviso do ditado no celular para este provedor antes')
      const c = p.corpo
      if (!ehObjeto(c) || !soTem(c, ['chave']) || (c.chave !== undefined && typeof c.chave !== 'string')) {
        return erro(400, 'BAD_REQUEST', 'corpo inválido: mande só {"chave": "..."}')
      }
      const chave = c.chave ?? ''
      if (!/^[\x21-\x7e]{8,200}$/.test(chave)) return erro(400, 'VALIDATION', 'chave inválida')
      if (chave.startsWith('recusada')) return erro(422, 'PROVEDOR_RECUSOU_CHAVE', 'o provedor recusou a chave')
      daConta.chaves[provedor] = chave
      daConta.colarDeNovo = daConta.colarDeNovo.filter((x) => x !== provedor)
      const { provedor: nome, configurada, final } = daChave(provedor)
      return json({ provedor: nome, configurada, final })
    }
    if (p.metodo === 'DELETE') {
      delete daConta.chaves[provedor]
      daConta.colarDeNovo = daConta.colarDeNovo.filter((x) => x !== provedor)
      return vazio()
    }
    return undefined
  }

  const cabecalho = (p: PedidoNuvem, nome: string) => Object.entries(p.cabecalhos).find(([k]) => k.toLowerCase() === nome.toLowerCase())?.[1]

  // ditar responde POST /api/v1/dictations como o servidor: o id procurado
  // antes de tudo (o que já existe volta sem chamar o provedor), a fala medida
  // abaixo do mínimo recusada antes do corpo, o formato e os cabeçalhos
  // conferidos, e o ditado criado pela sessão, sem aparelho, como "web".
  function ditar(p: PedidoNuvem): Response {
    // O aceite dos provedores que as preferências usam, e a chave deles, antes
    // de qualquer outra coisa (ADR-021, "O caminho de um ditado", 6).
    const prefs = daConta.preferencias
    if (!conferido(exigidos(prefs))) return erro(403, 'SEM_ACEITE', 'falta o aceite do aviso')
    if (!exigidos(prefs).every((prov) => daConta.chaves[prov])) return erro(409, 'SEM_CHAVE', 'falta a chave do provedor')
    const id = cabecalho(p, 'Ditado-Id') ?? ''
    if (!pareceUUID(id)) return erro(400, 'BAD_REQUEST', 'Ditado-Id inválido')
    const existente = itens.find((i) => i.id === id)
    if (existente) {
      ditados.push({ id, bytes: p.audio?.size ?? 0, tipo: cabecalho(p, 'Content-Type') ?? '', duracaoMs: 0, falaMs: 0, chamouOProvedor: false })
      return json({ ...existente })
    }
    if (lapides.has(id)) return erro(410, 'APAGADO', 'o ditado foi apagado')
    const falaMs = Number(cabecalho(p, 'Ditado-Fala-Ms'))
    const duracaoMs = Number(cabecalho(p, 'Ditado-Duracao-Ms'))
    if (!Number.isInteger(falaMs) || !Number.isInteger(duracaoMs) || falaMs < 0 || duracaoMs < 0) return erro(400, 'BAD_REQUEST', 'cabeçalho inválido')
    if (falaMs < 300) return erro(422, 'SEM_FALA', 'fala demais curta')
    if (cabecalho(p, 'Content-Type') !== 'audio/webm') return erro(415, 'FORMATO', 'só webm')
    const bytes = p.audio?.size ?? 0
    ditados.push({ id, bytes, tipo: cabecalho(p, 'Content-Type') ?? '', duracaoMs, falaMs, chamouOProvedor: true })
    const agora = new Date().toISOString()
    const novo = itemNuvem({
      id,
      created_at: agora,
      updated_at: agora,
      raw_text: comoDitar.texto,
      final_text: comoDitar.texto,
      asr_provider: prefs.provedor,
      asr_model: prefs.modelo,
      duration_ms: duracaoMs,
      device_id: null,
      source_platform: 'web',
    })
    itens.push(novo)
    return json({ ...novo, ...(comoDitar.aviso ? { aviso: comoDitar.aviso } : {}) }, 201)
  }

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
      lapides.add(item.id)
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
      audio: init.body instanceof Blob ? init.body : undefined,
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
    if (p.caminho === '/api/v1/dictations' && p.metodo === 'POST') return ditar(p)
    const doDitado = rotaDoDitado(p)
    if (doDitado) return doDitado
    if (p.caminho === '/api/v1/me/data' && p.metodo === 'DELETE') {
      if (p.corpo?.confirmar !== 'apagar') return erro(400, 'VALIDATION', 'confirmação obrigatória')
      itens.splice(0, itens.length)
      // O aceite, as preferências e as chaves também (ADR-021, C3b).
      daConta.aceite = null
      daConta.chaves = {}
      daConta.colarDeNovo = []
      daConta.preferencias = preferenciasIniciais()
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
    // ditados: os pedidos de ditado do celular que chegaram, na ordem. O
    // provedor de mentira só é chamado quando chamouOProvedor é true.
    ditados,
    get chamadasAoProvedor() {
      return ditados.filter((d) => d.chamouOProvedor).length
    },
    // aoDitar muda o que o provedor de mentira devolve nos próximos ditados.
    aoDitar(como: ComoDitar) {
      comoDitar = como
    },
    // O ditado pelo celular da conta (aceite, preferências e as chaves
    // INTEIRAS, que o app nunca recebe): para o teste conferir o que ficou
    // guardado.
    daConta,
    // revogarAceitePorFora: o aceite acabou em outro aparelho (ou a conta
    // apagou os dados): o app, que ainda acha que vale, descobre pelo 403.
    revogarAceitePorFora() {
      daConta.aceite = null
      daConta.chaves = {}
      daConta.colarDeNovo = []
    },
    // aceitarPorFora: outro aparelho aceitou (os dois provedores e a versão
    // dados).
    aceitarPorFora(transcricao: AceiteFalso['transcricao'], polish: AceiteFalso['polish'] = null, versao = versaoAtual) {
      daConta.aceite = { versao, transcricao, polish, aceito_em: '2026-10-10T12:00:00.000Z' }
    },
    // envelhecerAceitePorFora: o aviso mudou (o texto ganhou outra versão) e o
    // aceite que a conta tinha já não vale; as chaves continuam guardadas. O
    // app que pergunta já tem o texto novo: o aceite novo que ele mandar vale.
    envelhecerAceitePorFora() {
      if (daConta.aceite) daConta.aceite = { ...daConta.aceite, versao: versaoAtual - 1 }
    },
    // subirOAvisoPorFora: o servidor passou a uma versão nova do aviso depois de
    // este app abrir (um deploy): o app ainda tem o texto velho, o aceite que a
    // conta tinha deixa de valer, e o POST do aceite com a versão velha é 409
    // AVISO_DESATUALIZADO.
    subirOAvisoPorFora() {
      versaoAtual += 1
    },
    // trocarChaveDeCifraPorFora: o servidor passou a cifrar com outra chave de
    // cifra (o arquivo trocou, ou o backup voltou sem ele): a chave guardada de
    // cada provedor deixa de valer, e a tela pede para colar de novo.
    trocarChaveDeCifraPorFora() {
      for (const p of provedoresFalsos) {
        if (daConta.chaves[p] !== undefined && !daConta.colarDeNovo.includes(p)) daConta.colarDeNovo.push(p)
      }
      daConta.chaves = {}
    },
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
