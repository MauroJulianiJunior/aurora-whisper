// O cliente da nuvem (ADR-019, F4): a /api/v1 e o /auth da própria origem,
// pela sessão do cookie __Host-sessao. Ele nunca manda Authorization, nunca
// vê o cookie (é HttpOnly) e não guarda nada no aparelho: nem token, nem
// texto ditado, nem o áudio do ditado (ditar só o repassa; quem o segura, na
// memória, até o envio dar certo é o gravador, nuvem/ditar). O CSRF não pede
// nada daqui: o navegador manda
// Sec-Fetch-Site: same-origin, e o servidor confere
// (http.CrossOriginProtection).
//
// A busca vai sempre no corpo de um POST: o texto ditado nunca vai na URL,
// para não parar no log de acesso de um proxy (ADR-019, "Busca fora da URL").

import {
  type Buscador,
  type ClienteDoHistorico,
  type Ditado,
  type EscolhaDoSelo,
  ErroDaApi,
  type Pagina,
  type VersaoEmConflito,
  buscadorPadrao,
  pedir,
} from '../api'
import { type Provedor, ehProvedor, todosOsProvedores } from './ditar/aviso'
import {
  type Aceite,
  type ChaveGuardada,
  type EstadoDoDitado,
  type Preferencias,
  chavesVazias,
  preferenciasPadrao,
} from './ditar/estado'

// Eu é a conta da sessão (GET /api/v1/me).
export interface Eu {
  id: string
  email: string
  display_name?: string
}

// Sessao é uma linha de GET /api/v1/sessions.
export interface Sessao {
  id: string
  // rotulo: "Android · Chrome", de listas fechadas do servidor.
  rotulo: string
  criada_em: string
  ultimo_uso_em: string
  // atual: é a sessão deste navegador.
  atual: boolean
}

// LoginDeTeste: o servidor de teste (build tag logindeteste, só no PC)
// oferece entrar com a conta de teste, sem o Google. google diz se o login
// do Google também existe nele.
export interface LoginDeTeste {
  conta: string
  google: boolean
}

// A confirmação que o "apagar meus dados" leva no corpo (06, DELETE /me/data).
export const confirmacaoDeApagar = 'apagar'

// PedidoDePareamento é o que a página de aprovação mostra (GET
// /api/v1/device-auth/requests/{id}; ADR-019, F6).
export interface PedidoDePareamento {
  nome: string
  plataforma: string
  versao: string
  // conferencia: o código curto, o mesmo do painel do PC. Só conferência:
  // quem protege é o retorno ao 127.0.0.1 com o PKCE.
  conferencia: string
  expira_em: string
}

// Aparelho é uma linha de GET /api/v1/devices: um PC conectado à conta.
export interface Aparelho {
  id: string
  nome: string
  plataforma: string
  versao: string
  conectado_em: string
  ultimo_uso_em: string
}

// Aprendido é uma troca que um PC aprendeu e a nuvem guarda (GET
// /api/v1/learned; ADR-019, F8): a variante vira o termo. É só o que a tela
// precisa: nada da linha (versões, datas, dono) vai ao celular.
export interface Aprendido {
  // id: o UUID da variante (o mesmo em toda conta); é o que o desfazer leva.
  id: string
  termo: string
  variante: string
  // restrito: a troca só age no domínio. Sem domínio, é sempre false.
  restrito: boolean
  // dominio: o nome do domínio a que a troca está presa; "" é nenhum.
  dominio: string
}

// Aprendidos é a lista do celular: só os não desfeitos, do mais recente para o
// mais antigo, até um teto do servidor. has_more diz que passou do teto.
export interface Aprendidos {
  items: Aprendido[]
  has_more: boolean
}

// pareceIdDePedido: o id do pedido é um UUID (o state do retorno).
export const pareceIdDePedido = (id: string) => /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)

// O retorno que a aprovação devolve só vale se for o painel do PC, em
// 127.0.0.1: a página nunca navega para outro lugar com o code.
const pareceRetorno = (u: string) => /^http:\/\/127\.0\.0\.1:\d{4,5}\/nuvem\/retorno\?/.test(u)

// PedidoDeDitado é um ditado do celular a caminho do servidor (ADR-021, "A
// rota"): o áudio cru e o que o app mediu. O id é o UUID v7 que o app gerou; é
// a chave da idempotência, e o reenvio leva sempre o mesmo.
export interface PedidoDeDitado {
  id: string
  audio: Blob
  // duracaoMs: a duração medida no app (o webm do MediaRecorder não a traz).
  duracaoMs: number
  // falaMs: quanto do áudio o app achou que era fala (decisão M).
  falaMs: number
}

// DitadoGravado é a resposta de POST /api/v1/dictations: o ditado como ficou
// e, quando o Polish caiu para o Smart, o motivo (um ia.Motivo, nunca texto).
export interface DitadoGravado {
  ditado: Ditado
  aviso?: string
}

export interface ClienteNuvem extends ClienteDoHistorico {
  // ditar leva o áudio ao servidor, que o repassa ao provedor da pessoa, passa
  // pelo Smart (e pelo Polish, se ligado) e grava o ditado (ADR-021). Os erros
  // chegam em ErroDaApi.codigo; o sinal é o prazo de 90 s do celular.
  ditar(pedido: PedidoDeDitado, sinal?: AbortSignal): Promise<DitadoGravado>
  // O ditado pelo celular na conta (ADR-021, "O aceite e as preferências" e "As
  // chaves"). Os erros chegam em ErroDaApi.codigo (SEM_ACEITE...). A chave vai
  // só no corpo do PUT (nunca na URL), e nenhuma resposta a devolve: só
  // `configurada` e o `final`.
  //
  // estadoDoDitado: GET /dictation (as preferências, o aceite vigente, as
  // chaves). salvarPreferencias: PUT /dictation/settings (SEM_ACEITE para um
  // provedor fora do aceite). aceitarAviso: POST /dictation/consent, só com a
  // versão atual do aviso. desligarDitado: DELETE /dictation/consent (revoga o
  // aceite e apaga as chaves). guardarChave/removerChave: PUT e DELETE em
  // /ai-keys/{provedor}. modelosDaChave: GET /ai-keys/{provedor}/models, todos
  // os modelos que a chave alcança (quem filtra é o app, estado.ts).
  estadoDoDitado(): Promise<EstadoDoDitado>
  salvarPreferencias(preferencias: Preferencias): Promise<Preferencias>
  aceitarAviso(versao: string, provedores: readonly Provedor[]): Promise<Aceite>
  desligarDitado(): Promise<void>
  guardarChave(provedor: Provedor, chave: string): Promise<ChaveGuardada>
  removerChave(provedor: Provedor): Promise<void>
  modelosDaChave(provedor: Provedor): Promise<string[]>
  eu(): Promise<Eu>
  sessoes(): Promise<Sessao[]>
  encerrarSessao(id: string): Promise<void>
  // sair apaga a sessão no servidor e o cookie.
  sair(): Promise<void>
  apagarMeusDados(): Promise<void>
  // excluirDeVez é o "Excluir definitivamente" da lixeira: deixa a lápide,
  // e o PC apaga a cópia dele no próximo pull.
  excluirDeVez(id: string): Promise<void>
  // resolverConflito é a escolha do selo; 409 (outra aba já escolheu, ou o
  // item mudou) vem como Conflito.
  resolverConflito(id: string, versao: number, escolha: EscolhaDoSelo): Promise<Ditado>
  // inicioDoGoogle pede ao servidor o login (POST /auth/start, que grava o
  // __Host-pre) e devolve o endereço do Google, para a tela navegar. volta:
  // o caminho desta origem aonde o Google devolve a pessoa (sem ela, "/").
  inicioDoGoogle(volta?: string): Promise<string>
  // loginDeTeste: null quando o servidor não tem o login de teste (404),
  // que é o caso de todo servidor de verdade.
  loginDeTeste(): Promise<LoginDeTeste | null>
  entrarComContaDeTeste(conta: string): Promise<void>
  // O pareamento de um PC (F6). pedidoDePareamento: vencido, usado ou
  // desconhecido é ErroDaApi 404. aprovarPedido devolve o endereço do
  // retorno ao PC (http://127.0.0.1:<porta>/nuvem/retorno?code=…&state=…),
  // para a página navegar.
  pedidoDePareamento(id: string): Promise<PedidoDePareamento>
  aprovarPedido(id: string): Promise<string>
  // Os aparelhos da conta: a lista e o Revogar.
  aparelhos(): Promise<Aparelho[]>
  revogarAparelho(id: string): Promise<void>
  // O aprendido dos PCs (F8): a lista e o Desfazer, que vale em todos os
  // aparelhos (chega aos PCs pelo pull). O celular não aprende: não há
  // rota de criar nem de mudar. O id vai no corpo do POST, nunca na URL; o
  // 404 com a frase "troca aprendida não encontrada" é "essa troca já não
  // está aí" (desfeita em outro lugar, ou de outra conta). Um 404 sem essa
  // frase é outra coisa (a rota nova que o servidor ainda não tem, um proxy):
  // erro como os outros.
  aprendidos(): Promise<Aprendidos>
  desfazerAprendido(id: string): Promise<void>
}

// O item como a nuvem manda: deleted_at e versao_em_conflito vêm sempre,
// null quando não há; next_cursor vem "" na última página.
type ItemDaNuvem = Omit<Ditado, 'deleted_at' | 'versao_em_conflito'> & {
  deleted_at?: string | null
  versao_em_conflito?: VersaoEmConflito | null
}

// doServidor deixa o item no formato do painel: o que não há fica ausente,
// como no Agent.
function doServidor(d: ItemDaNuvem): Ditado {
  const { deleted_at, versao_em_conflito, ...resto } = d
  const item: Ditado = { ...resto }
  if (deleted_at) item.deleted_at = deleted_at
  if (versao_em_conflito) item.versao_em_conflito = versao_em_conflito
  return item
}

function daPagina(p: { items?: ItemDaNuvem[] | null; next_cursor?: string | null }): Pagina {
  const pagina: Pagina = { items: (p.items ?? []).map(doServidor) }
  if (p.next_cursor) pagina.next_cursor = p.next_cursor
  return pagina
}

// O estado do ditado como a nuvem o manda (GET /api/v1/dictation, ADR-021). O
// app lê com tolerância: o que falta ou não tem a forma esperada vira o padrão
// mais fechado (sem aceite, sem chave, tudo desligado), e nunca mostra da chave
// mais que o final de 4 caracteres, mesmo que o servidor mandasse mais.
function daChave(v: unknown): ChaveGuardada {
  const c = (v ?? {}) as { configurada?: unknown; final?: unknown }
  const configurada = c.configurada === true
  return { configurada, final: configurada && typeof c.final === 'string' ? c.final.slice(-4) : '' }
}

function dasPreferencias(v: unknown): Preferencias {
  const p = (v ?? {}) as Record<string, unknown>
  return {
    provedor: p.provedor === 'groq' ? 'groq' : preferenciasPadrao.provedor,
    modelo: typeof p.modelo === 'string' ? p.modelo : '',
    dica: p.dica === true,
    polish: p.polish === true,
    polish_provedor: p.polish_provedor === 'anthropic' ? 'anthropic' : preferenciasPadrao.polish_provedor,
    polish_modelo: typeof p.polish_modelo === 'string' ? p.polish_modelo : '',
  }
}

function doAceite(v: unknown): Aceite | null {
  if (!v || typeof v !== 'object') return null
  const a = v as { versao?: unknown; provedores?: unknown; aceito_em?: unknown }
  if (typeof a.versao !== 'string' || a.versao === '') return null
  const provedores = Array.isArray(a.provedores) ? a.provedores.filter(ehProvedor) : []
  return { versao: a.versao, provedores: Array.from(new Set(provedores)), aceito_em: typeof a.aceito_em === 'string' ? a.aceito_em : '' }
}

export function criarClienteNuvem(buscar: Buscador = buscadorPadrao): ClienteNuvem {
  // Toda chamada é da mesma origem e leva o cookie dela; nada mais.
  const daOrigem = (init: RequestInit = {}): RequestInit => ({ ...init, credentials: 'same-origin' })
  const comJson = (method: string, corpo: unknown, sinal?: AbortSignal): RequestInit =>
    daOrigem({ method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(corpo), signal: sinal })
  const doItem = (id: string) => `/api/v1/transcriptions/${encodeURIComponent(id)}`

  return {
    async listar({ lixeira, busca, cursor }, sinal) {
      let r: Response
      if (busca) {
        const corpo: Record<string, unknown> = { q: busca, trash: lixeira }
        if (cursor) corpo.cursor = cursor
        r = await pedir(buscar, '/api/v1/transcriptions/search', comJson('POST', corpo, sinal))
      } else {
        const q = new URLSearchParams()
        if (lixeira) q.set('trash', 'true')
        if (cursor) q.set('cursor', cursor)
        const resto = q.toString()
        r = await pedir(buscar, `/api/v1/transcriptions${resto ? `?${resto}` : ''}`, daOrigem({ signal: sinal }))
      }
      return daPagina((await r.json()) as Parameters<typeof daPagina>[0])
    },
    async obter(id) {
      const r = await pedir(buscar, doItem(id), daOrigem())
      return doServidor((await r.json()) as ItemDaNuvem)
    },
    async editar(id, versao, texto) {
      const r = await pedir(buscar, doItem(id), comJson('PATCH', { base_version: versao, final_text: texto }))
      // A nuvem não sugere aprender: o motor e o texto bruto estão no PC.
      return { ditado: doServidor((await r.json()) as ItemDaNuvem), sugestoes: [] }
    },
    async moverParaLixeira(id) {
      await pedir(buscar, doItem(id), daOrigem({ method: 'DELETE' }))
    },
    async restaurar(id) {
      await pedir(buscar, `${doItem(id)}/restore`, daOrigem({ method: 'POST' }))
    },
    async excluirDeVez(id) {
      // O 409 aqui é "não está na lixeira" (outro aparelho restaurou), não
      // versão velha: chega como ErroDaApi, com a frase.
      await pedir(buscar, `${doItem(id)}/purge`, daOrigem({ method: 'POST' }), false)
    },
    async resolverConflito(id, versao, escolha) {
      const r = await pedir(buscar, doItem(id), comJson('PATCH', { base_version: versao, resolver_conflito: escolha }))
      return doServidor((await r.json()) as ItemDaNuvem)
    },
    async ditar({ id, audio, duracaoMs, falaMs }, sinal) {
      // O corpo é o áudio cru (nunca multipart nem JSON), e o que o app mediu
      // vai em cabeçalhos. O 409 não é versão: chega como ErroDaApi, com o
      // código. Nada daqui vai à URL.
      const r = await pedir(
        buscar,
        '/api/v1/dictations',
        daOrigem({
          method: 'POST',
          headers: {
            'Content-Type': 'audio/webm',
            'Ditado-Id': id,
            'Ditado-Duracao-Ms': String(Math.round(duracaoMs)),
            'Ditado-Fala-Ms': String(Math.round(falaMs)),
          },
          body: audio,
          signal: sinal,
        }),
        false,
      )
      const { aviso, ...item } = (await r.json()) as ItemDaNuvem & { aviso?: unknown }
      const gravado: DitadoGravado = { ditado: doServidor(item) }
      if (typeof aviso === 'string' && aviso !== '') gravado.aviso = aviso
      return gravado
    },
    async estadoDoDitado() {
      const r = await pedir(buscar, '/api/v1/dictation', daOrigem(), false)
      const v = (await r.json()) as { preferencias?: unknown; aceite?: unknown; chaves?: Record<string, unknown> | null }
      const chaves = chavesVazias()
      for (const p of todosOsProvedores) chaves[p] = daChave(v.chaves?.[p])
      return { preferencias: dasPreferencias(v.preferencias), aceite: doAceite(v.aceite), chaves }
    },
    async salvarPreferencias(preferencias) {
      const r = await pedir(buscar, '/api/v1/dictation/settings', comJson('PUT', preferencias), false)
      return dasPreferencias(await r.json())
    },
    async aceitarAviso(versao, provedores) {
      const r = await pedir(buscar, '/api/v1/dictation/consent', comJson('POST', { versao, provedores }), false)
      return doAceite(await r.json()) ?? { versao, provedores: [...provedores], aceito_em: '' }
    },
    async desligarDitado() {
      await pedir(buscar, '/api/v1/dictation/consent', daOrigem({ method: 'DELETE' }), false)
    },
    async guardarChave(provedor, chave) {
      // A chave vai só no corpo (nunca na URL) e não volta: a resposta traz
      // `configurada` e o final.
      const r = await pedir(buscar, `/api/v1/ai-keys/${encodeURIComponent(provedor)}`, comJson('PUT', { chave }), false)
      return daChave(await r.json())
    },
    async removerChave(provedor) {
      await pedir(buscar, `/api/v1/ai-keys/${encodeURIComponent(provedor)}`, daOrigem({ method: 'DELETE' }), false)
    },
    async modelosDaChave(provedor) {
      const r = await pedir(buscar, `/api/v1/ai-keys/${encodeURIComponent(provedor)}/models`, daOrigem(), false)
      const { modelos } = (await r.json()) as { modelos?: unknown }
      return Array.isArray(modelos) ? modelos.filter((m): m is string => typeof m === 'string') : []
    },
    async eu() {
      const r = await pedir(buscar, '/api/v1/me', daOrigem())
      return (await r.json()) as Eu
    },
    async sessoes() {
      const r = await pedir(buscar, '/api/v1/sessions', daOrigem())
      const { items } = (await r.json()) as { items?: Sessao[] | null }
      return items ?? []
    },
    async encerrarSessao(id) {
      await pedir(buscar, `/api/v1/sessions/${encodeURIComponent(id)}`, daOrigem({ method: 'DELETE' }))
    },
    async sair() {
      await pedir(buscar, '/auth/logout', daOrigem({ method: 'POST' }))
    },
    async apagarMeusDados() {
      await pedir(buscar, '/api/v1/me/data', comJson('DELETE', { confirmar: confirmacaoDeApagar }))
    },
    async inicioDoGoogle(volta) {
      const r = await pedir(buscar, '/auth/start', comJson('POST', volta ? { volta } : {}))
      const { url } = (await r.json()) as { url?: unknown }
      // Só um endereço https: o servidor é desta origem, mas a tela nunca
      // navega para algo que não seja a página de login.
      if (typeof url !== 'string' || !url.startsWith('https://')) throw new ErroDaApi(502, 'o servidor não deu o endereço do login')
      return url
    },
    async loginDeTeste() {
      try {
        const r = await pedir(buscar, '/auth/teste', daOrigem())
        const v = (await r.json()) as { conta?: unknown; google?: unknown }
        return typeof v.conta === 'string' && v.conta !== '' ? { conta: v.conta, google: v.google === true } : null
      } catch (e) {
        if (e instanceof ErroDaApi && e.status === 404) return null
        throw e
      }
    },
    async entrarComContaDeTeste(conta) {
      await pedir(buscar, '/auth/teste', comJson('POST', { email: conta }))
    },
    async pedidoDePareamento(id) {
      const r = await pedir(buscar, `/api/v1/device-auth/requests/${encodeURIComponent(id)}`, daOrigem(), false)
      return (await r.json()) as PedidoDePareamento
    },
    async aprovarPedido(id) {
      const r = await pedir(buscar, `/api/v1/device-auth/requests/${encodeURIComponent(id)}/approve`, daOrigem({ method: 'POST' }), false)
      const { retorno } = (await r.json()) as { retorno?: unknown }
      if (typeof retorno !== 'string' || !pareceRetorno(retorno)) throw new ErroDaApi(502, 'o servidor não deu o endereço do PC')
      return retorno
    },
    async aparelhos() {
      const r = await pedir(buscar, '/api/v1/devices', daOrigem())
      const { items } = (await r.json()) as { items?: Aparelho[] | null }
      return items ?? []
    },
    async revogarAparelho(id) {
      await pedir(buscar, `/api/v1/devices/${encodeURIComponent(id)}`, daOrigem({ method: 'DELETE' }), false)
    },
    async aprendidos() {
      const r = await pedir(buscar, '/api/v1/learned', daOrigem(), false)
      const { items, has_more } = (await r.json()) as { items?: Aprendido[] | null; has_more?: unknown }
      return { items: items ?? [], has_more: has_more === true }
    },
    async desfazerAprendido(id) {
      await pedir(buscar, '/api/v1/learned/undo', comJson('POST', { id }), false)
    },
  }
}

// Portao é a passagem dos pedidos do Painel para a nuvem. A sessão acabou
// com um texto não salvo na tela (ADR-019, "Decisões da F4": o rascunho que
// fica até a pessoa entrar de novo), e a faixa espera ela entrar, numa
// janela à parte: enquanto isso, o portão fica fechado, e todo pedido do
// Painel volta como 401 sem sair da página. Só a faixa o abre, depois de o
// GET /api/v1/me dizer que voltou a MESMA conta: outra conta, entrando pela
// janela, não mistura a lista dela com o texto desta.
export interface Portao {
  fechar(): void
  abrir(): void
  // passagem: o buscar guardado pelo portão.
  passagem(buscar: Buscador): Buscador
}

export function criarPortao(): Portao {
  let fechado = false
  return {
    fechar: () => {
      fechado = true
    },
    abrir: () => {
      fechado = false
    },
    passagem: (buscar) => (entrada, init) => (fechado ? Promise.resolve(new Response(null, { status: 401 })) : buscar(entrada, init)),
  }
}
