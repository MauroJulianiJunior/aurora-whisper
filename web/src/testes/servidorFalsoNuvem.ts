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
//
// A rota de ditar (POST /api/v1/dictations) segue a do servidor de verdade (06,
// "Ditar pelo celular", C5b), na mesma ordem: o aceite e a chave, o Ditado-Id v7, o
// que a nuvem e a memória têm com o id (o reenvio, a lápide, o que esperou, o pago
// e não gravado), as medidas, o tipo e o tamanho, a vaga, e o trabalho. O que dá
// errado em cada ponto se pede com falharNoProximoDitado; o provedor que demora,
// com segurarOProximoDitado. contratoDoDitado.test.ts prende tudo isso ao 06.

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

// DitadoDoCelular é um pedido de ditado que o servidor de mentira recebeu
// (ADR-021): o que o app mandou, o que o servidor respondeu, e se o provedor foi
// chamado por ele. Só entram os pedidos que passaram do aceite, da chave e do
// Ditado-Id (os que a rota de verdade também responde antes de olhar o ditado
// ficam só em `pedidos`).
export interface DitadoDoCelular {
  id: string
  bytes: number
  tipo: string
  duracaoMs: number
  falaMs: number
  // chamouOProvedor: false no reenvio do mesmo id (a idempotência), em quem
  // esperou o primeiro, e em qualquer recusa que vem antes do provedor.
  chamouOProvedor: boolean
  // status: o que o servidor respondeu a este pedido (201 o ditado novo, 200 o
  // reenvio, ou a recusa).
  status: number
}

// Os motivos do `aviso` do ditado (resolver.MotivoFallback): a lista fechada do
// Polish que o 06 dá em "POST /dictations". contratoDoDitado.test.ts prende esta
// lista à do 06, e a fraseDoAviso do app tem de ter uma frase para cada um.
export const motivosDoPolishFalsos = [
  'prazo',
  'sem_chave',
  'chave_recusada',
  'sem_credito',
  'limite',
  'modelo_inexistente',
  'modelo_incompativel',
  'sobrecarga',
  'transporte',
  'pedido_recusado',
  'recusa',
  'truncada',
  'resposta_invalida',
  'json',
  'formato',
  'numeros',
  'siglas_termos',
  'palavras',
  'caixa',
  'trecho_novo',
  'dicionario',
] as const
export type MotivoDoPolish = (typeof motivosDoPolishFalsos)[number]

// ComoDitar é o que o provedor de mentira devolve nos próximos ditados.
export interface ComoDitar {
  // texto: o que o ASR (e o Smart) escreveram.
  texto: string
  // aviso: o motivo de o Polish ter caído para o Smart (um resolver.MotivoFallback,
  // nunca texto). Só existe com o Polish ligado na conta: o servidor de verdade
  // só o manda quando o Polish estava ligado e não valeu.
  aviso?: MotivoDoPolish
}

// FalhaDoDitado é o que dá errado no PRÓXIMO ditado (uma vez só), no ponto em que
// daria no servidor de verdade (06, "POST /dictations", a ordem):
//  - 'UM_POR_VEZ', 'LIMITE', 'OCUPADO': a vaga e os limites, antes de ler o corpo
//    (429, 429 e 503, com Retry-After). O provedor não é chamado;
//  - 'corpo_incompleto': o corpo não chegou inteiro (400 BAD_REQUEST). O provedor
//    não é chamado, e o reenvio com o mesmo id processa normalmente;
//  - 'PROVEDOR_RECUSOU_CHAVE', 'PROVEDOR_LIMITE', 'PROVEDOR_FORA': o provedor
//    falhou (422, 502 e 502). Foi chamado, e nenhum ditado é criado;
//  - 'provedor_sem_texto': o provedor não devolveu texto (422 SEM_FALA). Foi
//    chamado, e o mesmo id dá 422 sem chamá-lo de novo;
//  - 'gravacao': o provedor devolveu o texto e a gravação falhou (500 INTERNAL). O
//    desfecho pago fica na memória, e o reenvio com o mesmo id só grava (201).
export type FalhaDoDitado =
  | 'UM_POR_VEZ'
  | 'LIMITE'
  | 'OCUPADO'
  | 'corpo_incompleto'
  | 'PROVEDOR_RECUSOU_CHAVE'
  | 'PROVEDOR_LIMITE'
  | 'PROVEDOR_FORA'
  | 'provedor_sem_texto'
  | 'gravacao'

// Os status dos códigos da rota de ditar (a tabela "Os códigos" do 06). O servidor
// de mentira responde com ESTES status, e contratoDoDitado.test.ts compara cada
// um ao da tabela do 06.
export const statusDosCodigosDaRota = {
  SEM_ACEITE: 403,
  SEM_CHAVE: 409,
  LIMITE: 429,
  UM_POR_VEZ: 429,
  OCUPADO: 503,
  PAYLOAD_TOO_LARGE: 413,
  FORMATO: 415,
  SEM_FALA: 422,
  APAGADO: 410,
  PROVEDOR_RECUSOU_CHAVE: 422,
  PROVEDOR_LIMITE: 502,
  PROVEDOR_FORA: 502,
} as const
type CodigoDaRota = keyof typeof statusDosCodigosDaRota

// Os tetos e o mínimo da rota (internal/server/ditar.go): 1 MiB de corpo, 125 s de
// duração (os 120 s do PC e 5 s de folga) e 300 ms de fala medida.
export const tetoDoCorpoNoServidorFalso = 1 << 20
export const tetoDaDuracaoNoServidorFalso = 125_000
export const falaMinimaNoServidorFalso = 300

// DesfechoFalso é o que a rota de ditar responde: o status e o corpo. É o que
// "o registro do servidor" guarda de um ditado em andamento, e quem espera o mesmo
// id recebe (200 com o ditado, se deu certo, ou a mesma recusa).
interface DesfechoFalso {
  status: number
  corpo: unknown
  // espera: o Retry-After, em segundos.
  espera?: number
  // aviso: o do Polish do ditado que este desfecho gravou.
  aviso?: string
  // reprocessar: o primeiro parou antes do provedor (o corpo dele não chegou), e
  // quem esperava tenta com o próprio corpo.
  reprocessar?: boolean
}

function desfechoDeErro(status: number, code: string, message: string, extra: Partial<DesfechoFalso> = {}): DesfechoFalso {
  return { status, corpo: { error: { code, message, request_id: 'r1' } }, ...extra }
}

// recusaDaRota é a recusa de um código da lista fechada, com o status do 06.
function recusaDaRota(codigo: CodigoDaRota, frase: string, espera?: number): DesfechoFalso {
  return desfechoDeErro(statusDosCodigosDaRota[codigo], codigo, frase, espera === undefined ? {} : { espera })
}

function comoResposta(d: DesfechoFalso): Response {
  const cabecalhos: Record<string, string> = { 'Content-Type': 'application/json' }
  if (d.espera !== undefined) cabecalhos['Retry-After'] = String(d.espera)
  return new Response(JSON.stringify(d.corpo), { status: d.status, headers: cabecalhos })
}

// A vaga e os limites: a frase e o Retry-After de cada um (5 s no UM_POR_VEZ e no
// OCUPADO; no LIMITE, o tempo até a próxima ficha).
const recusasDaVaga = {
  UM_POR_VEZ: { frase: 'um ditado de cada vez: espere o anterior terminar', espera: 5 },
  LIMITE: { frase: 'ditados demais em pouco tempo: espere um pouco', espera: 12 },
  OCUPADO: { frase: 'o servidor está ocupado com outros ditados: tente de novo em alguns segundos', espera: 5 },
} as const

const recusasDoProvedor = {
  PROVEDOR_RECUSOU_CHAVE: 'o provedor recusou a chave',
  PROVEDOR_LIMITE: 'o provedor recusou por limite de uso ou falta de crédito',
  PROVEDOR_FORA: 'o provedor não respondeu',
} as const

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

// ditadoComOPolish é a opção `ditado` de uma conta com o Polish ligado (a
// Anthropic, com um modelo), o aceite dos dois provedores e as duas chaves: a
// única de onde o `aviso` do ditado pode sair.
export const ditadoComOPolish: NonNullable<OpcoesDaNuvem['ditado']> = {
  preferencias: { polish: true, polish_provedor: 'anthropic', polish_modelo: 'claude-haiku-4-5' },
  aceite: { versao: versaoDoAvisoNoServidorFalso, transcricao: 'openai', polish: 'anthropic', aceito_em: '2026-10-10T12:00:00.000Z' },
  chaves: { openai: 'sk-teste-de-mentira-OPEN', anthropic: 'sk-ant-teste-de-mentira-ANTH' },
}

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })
const erro = (status: number, code: string, message: string) => json({ error: { code, message, request_id: 'r1' } }, status)
const vazio = (status = 204) => new Response(null, { status })
// O id do ditado é um UUID v7 (o 05 pede v7): versão 7 e variante 10. Maiúsculas
// valem, e o servidor as guarda em minúsculas.
const pareceUUID = (s: string) => /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(s)
// As medidas do ditado: milissegundos inteiros, sem sinal, de 1 a 9 dígitos.
const milissegundos = (v: string | undefined) => (v !== undefined && /^\d{1,9}$/.test(v) ? Number(v) : null)
// O tipo do Content-Type sem os parâmetros (";codecs=opus"), como o servidor o compara.
const tipoDeMidia = (v: string) => v.split(';')[0]!.trim().toLowerCase()

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
  // O que o servidor guarda na memória dos ditados (06, "A idempotência"), por id:
  // o que está em andamento (quem chega com o mesmo id espera o desfecho), o que o
  // provedor não entendeu como fala (o mesmo id dá 422 sem chamá-lo) e o que foi
  // pago e não gravado (o reenvio só grava). Some no "Apagar meus dados" e no
  // "Desligar o ditado". Uma conta só: o servidor de mentira tem uma conta.
  const emAndamento = new Map<string, Promise<DesfechoFalso>>()
  const semFala = new Set<string>()
  const pagosNaoGravados = new Map<string, { item: ItemNuvem; aviso?: string }>()
  const esquecerOsDitados = () => {
    semFala.clear()
    pagosNaoGravados.clear()
  }
  // O que dá errado no próximo ditado (uma vez só), e o ditado segurado no
  // provedor até a pessoa do teste liberar.
  let falhaDoProximo: FalhaDoDitado | null = null
  let segurado: Promise<void> | null = null

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
      esquecerOsDitados()
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

  // doDitado é o ditado como a rota o responde: o item do histórico e, quando o
  // Polish caiu para o Smart, o `aviso`.
  const doDitado = (status: number, item: ItemNuvem, aviso?: string): DesfechoFalso => ({
    status,
    corpo: { ...item, ...(aviso ? { aviso } : {}) },
    ...(aviso ? { aviso } : {}),
  })

  // ditar responde POST /api/v1/dictations como o servidor, na ORDEM do 06
  // ("POST /dictations", internal/server/ditar.go), com tudo o que vem antes do
  // corpo sem lê-lo:
  //  1. o aceite vigente dos provedores que as preferências usam (403 SEM_ACEITE) e as
  //     chaves deles (409 SEM_CHAVE);
  //  2. o Ditado-Id (UUID v7; 400 BAD_REQUEST);
  //  3. o que a nuvem tem com o id: o ditado, também na lixeira, é 200 com ele, sem
  //     chamar nada; a lápide é 410 APAGADO;
  //  4. o que a memória tem: o mesmo id em andamento é esperado (200 com o ditado e o
  //     `aviso` do primeiro, ou a mesma recusa); o que o provedor não ouviu como fala
  //     é 422; o pago e não gravado só é gravado (201, sem chamar o provedor);
  //  5. as medidas, o tipo e o tamanho: 400 fora da forma (milissegundos inteiros, a
  //     fala até a duração), 413 com a duração acima de 125.000 ms, 422 SEM_FALA com a
  //     fala abaixo de 300 ms, 415 FORMATO sem Content-Type audio/webm (os parâmetros
  //     valem) ou com o corpo vazio, 413 com o corpo acima de 1 MiB;
  //  6. a vaga e os limites (a FalhaDoDitado 'UM_POR_VEZ', 'LIMITE' e 'OCUPADO');
  //  7. o corpo e o trabalho solto: o provedor, o Smart e o Polish e a gravação, com o
  //     ditado criado pela sessão, sem aparelho, como "web".
  // Os primeiros bytes do corpo (EBML e DocType webm) o servidor de mentira não
  // olha: o áudio dos testes não é webm de verdade. Isso é do Go (internal/server).
  async function ditar(p: PedidoNuvem): Promise<Response> {
    // 1. O aceite e as chaves (ADR-021, "O caminho de um ditado", 6).
    const prefs = daConta.preferencias
    if (!conferido(exigidos(prefs))) return comoResposta(recusaDaRota('SEM_ACEITE', 'falta o aceite do aviso'))
    if (!exigidos(prefs).every((prov) => daConta.chaves[prov])) return comoResposta(recusaDaRota('SEM_CHAVE', 'falta a chave do provedor'))
    // 2. O Ditado-Id.
    const idRecebido = cabecalho(p, 'Ditado-Id')
    if (idRecebido === undefined || !pareceUUID(idRecebido)) {
      return comoResposta(desfechoDeErro(400, 'BAD_REQUEST', 'Ditado-Id ausente ou inválido: mande o UUID v7 do ditado'))
    }
    const id = idRecebido.toLowerCase()

    const duracaoMs = milissegundos(cabecalho(p, 'Ditado-Duracao-Ms'))
    const falaMs = milissegundos(cabecalho(p, 'Ditado-Fala-Ms'))
    const bytes = p.audio?.size ?? 0
    const tipo = cabecalho(p, 'Content-Type') ?? ''
    // responder registra o pedido (o que chegou e o que o servidor respondeu) e
    // monta a resposta.
    const responder = (d: DesfechoFalso, chamouOProvedor = false) => {
      ditados.push({ id, bytes, tipo, duracaoMs: duracaoMs ?? 0, falaMs: falaMs ?? 0, chamouOProvedor, status: d.status })
      return comoResposta(d)
    }

    // Quem esperou o primeiro e o viu dar certo volta ao passo 3 e lê o ditado
    // gravado; quem o viu parar antes do provedor tenta com o próprio corpo. Até
    // três voltas (maxVoltas), e depois é OCUPADO.
    let avisoDoPrimeiro: string | undefined
    for (let volta = 0; volta < 3; volta++) {
      // 3. O que a nuvem tem com o id.
      const gravado = itens.find((i) => i.id === id)
      if (gravado) return responder(doDitado(200, gravado, avisoDoPrimeiro))
      if (lapides.has(id)) return responder(recusaDaRota('APAGADO', 'esse ditado foi apagado'))

      // 4. O que a memória tem com o id.
      const primeiro = emAndamento.get(id)
      if (primeiro) {
        const d = await primeiro
        if (d.status < 300 || d.reprocessar) {
          avisoDoPrimeiro = d.aviso
          continue
        }
        return responder(d)
      }
      if (semFala.has(id)) return responder(recusaDaRota('SEM_FALA', 'o provedor não ouviu fala no áudio'))
      const pago = pagosNaoGravados.get(id)
      if (pago) {
        pagosNaoGravados.delete(id)
        itens.push(pago.item)
        return responder(doDitado(201, pago.item, pago.aviso))
      }

      // 5. As medidas, o tipo e o tamanho, ainda sem ler o corpo.
      if (duracaoMs === null || falaMs === null || falaMs > duracaoMs) {
        return responder(desfechoDeErro(400, 'BAD_REQUEST', 'Ditado-Duracao-Ms e Ditado-Fala-Ms têm de ser milissegundos inteiros, com a fala até a duração'))
      }
      if (duracaoMs > tetoDaDuracaoNoServidorFalso) return responder(recusaDaRota('PAYLOAD_TOO_LARGE', 'o ditado passa do teto de duração'))
      if (falaMs < falaMinimaNoServidorFalso) return responder(recusaDaRota('SEM_FALA', 'o app não mediu fala bastante: nada foi enviado ao provedor'))
      if (tipoDeMidia(tipo) !== 'audio/webm' || bytes === 0) return responder(recusaDaRota('FORMATO', 'o áudio tem de ser webm (audio/webm)'))
      if (bytes > tetoDoCorpoNoServidorFalso) return responder(recusaDaRota('PAYLOAD_TOO_LARGE', 'o áudio passa de 1 MiB'))

      // 6. A vaga e os limites.
      const falha = falhaDoProximo
      if (falha === 'UM_POR_VEZ' || falha === 'LIMITE' || falha === 'OCUPADO') {
        falhaDoProximo = null
        return responder(recusaDaRota(falha, recusasDaVaga[falha].frase, recusasDaVaga[falha].espera))
      }

      // 7. Este pedido é o dono do id: o corpo e o trabalho. Quem chegar com o
      // mesmo id enquanto isso espera o desfecho.
      falhaDoProximo = null
      let concluir: (d: DesfechoFalso) => void = () => {}
      emAndamento.set(
        id,
        new Promise<DesfechoFalso>((resolver) => {
          concluir = resolver
        }),
      )
      let feito: { d: DesfechoFalso; chamou: boolean } | undefined
      try {
        feito = await trabalhar(id, duracaoMs, falha)
      } finally {
        emAndamento.delete(id)
        concluir(feito?.d ?? desfechoDeErro(500, 'INTERNAL', 'falha interna', { reprocessar: true }))
      }
      return responder(feito.d, feito.chamou)
    }
    return responder(recusaDaRota('OCUPADO', recusasDaVaga.OCUPADO.frase, recusasDaVaga.OCUPADO.espera))
  }

  // trabalhar é o que o dono do id faz depois das conferências: lê o corpo, chama
  // o provedor (o texto vem de `comoDitar`), passa pelo Smart e pelo Polish, e
  // grava. `falha` é o que o teste pediu para dar errado, no ponto certo.
  async function trabalhar(id: string, duracaoMs: number, falha: FalhaDoDitado | null): Promise<{ d: DesfechoFalso; chamou: boolean }> {
    // O corpo que não chegou inteiro: o provedor não foi chamado, e nada fica na memória.
    if (falha === 'corpo_incompleto') {
      return { d: desfechoDeErro(400, 'BAD_REQUEST', 'o áudio não chegou inteiro', { reprocessar: true }), chamou: false }
    }
    // O provedor demora: o ditado fica em andamento até a pessoa do teste liberar.
    if (segurado) {
      const espera = segurado
      segurado = null
      await espera
    }
    if (falha === 'PROVEDOR_RECUSOU_CHAVE' || falha === 'PROVEDOR_LIMITE' || falha === 'PROVEDOR_FORA') {
      return { d: recusaDaRota(falha, recusasDoProvedor[falha]), chamou: true }
    }
    if (falha === 'provedor_sem_texto') {
      semFala.add(id)
      return { d: recusaDaRota('SEM_FALA', 'o provedor não ouviu fala no áudio'), chamou: true }
    }
    const prefs = daConta.preferencias
    const aviso = comoDitar.aviso
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
    // A gravação que falha: o texto foi pago, e o reenvio com o mesmo id só grava.
    if (falha === 'gravacao') {
      pagosNaoGravados.set(id, { item: novo, ...(aviso ? { aviso } : {}) })
      return { d: desfechoDeErro(500, 'INTERNAL', 'o texto saiu, mas não foi guardado: tente de novo com o mesmo ditado (sem pagar de novo)'), chamou: true }
    }
    itens.push(novo)
    return { d: doDitado(201, novo, aviso), chamou: true }
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
      // O aceite, as preferências e as chaves também (ADR-021, C3b), e o que a
      // rota de ditar guardava na memória (o pago e não gravado, o sem fala).
      daConta.aceite = null
      daConta.chaves = {}
      daConta.colarDeNovo = []
      daConta.preferencias = preferenciasIniciais()
      esquecerOsDitados()
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
    // aoDitar muda o que o provedor de mentira devolve nos próximos ditados. O
    // aviso só existe com o Polish ligado na conta (o servidor de verdade só o
    // manda então): pedir um sem ele é montar um servidor que não existe.
    aoDitar(como: ComoDitar) {
      if (como.aviso && !daConta.preferencias.polish) {
        throw new Error('servidorFalsoNuvem: o aviso do Polish só existe com o Polish ligado na conta (ditado.preferencias.polish)')
      }
      comoDitar = como
    },
    // falharNoProximoDitado faz o PRÓXIMO ditado que passar das conferências do
    // cabeçalho dar errado, uma vez só, no ponto em que daria no servidor de
    // verdade (FalhaDoDitado).
    falharNoProximoDitado(falha: FalhaDoDitado) {
      falhaDoProximo = falha
    },
    // segurarOProximoDitado faz o próximo ditado ficar no provedor (em andamento)
    // até `liberar`: quem chega com o mesmo id nesse tempo espera o primeiro e
    // recebe o mesmo ditado (200, com o aviso do primeiro).
    segurarOProximoDitado() {
      let liberar: () => void = () => {}
      segurado = new Promise<void>((resolver) => {
        liberar = resolver
      })
      return { liberar: () => liberar() }
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
