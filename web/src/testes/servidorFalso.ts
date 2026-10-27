// Um Agent de mentira para os testes: responde as rotas da Local API
// (internal/agent/localapi) e do bilhete (internal/agent/painel) em
// memória, com as mesmas regras que importam para o painel — token no
// cabeçalho, base_version no PATCH, lixeira por deleted_at.
//
// Revisão de 29-set-2026: o dublê não subia a versão ao mover para a
// lixeira nem ao restaurar, e o armazenamento de verdade sobe
// (internal/storage, "version = version + 1"). Por isso nenhum teste via
// a etiqueta "editado" mentir, nem o 409 que outra aba provoca só por
// mover e desfazer. E a lixeira vem na ordem da exclusão (deleted_at
// DESC, id DESC), a que a API passou a usar.

//
// As rotas de Configurações e da chave de IA seguem o contrato comum de
// 29-set-2026 (GET/PUT /api/settings, /api/ia/chave, /api/ia/testar): o
// PUT pede base_version e devolve 409 se ela é velha, e 400 com mensagem
// para valor inválido — Polish sem a chave do provedor escolhido,
// inclusive. Campo ausente do PUT mantém o valor, como no Go. A chave
// fica só aqui, no mapa `chaves`, e nenhuma resposta a devolve: GET
// /api/ia/chave diz se há uma e os 4 últimos caracteres.

//
// O aprendizado (ADR-017, internal/agent/localapi/aprendizado.go) segue as
// regras do Agent que importam para o painel: o PATCH devolve `sugestoes`
// (as que o teste mandou devolver com sugerirNoSalvar), e POST
// /api/aprendizado só aceita um par que o PATCH devolveu para a versão
// ATUAL do ditado (409 com a frase, senão), grava a variante, e recusa a
// que já existe. O desfazer é por POST com a variante no corpo. Nada aqui
// usa a URL para o texto ditado.

//
// O "Adicionar termo" (ADR-020, internal/agent/localapi/termos.go) segue as
// regras do Agent que importam para o painel: toda mutação devolve a lista
// inteira e o orçamento das dicas; a dica que não cabe é recusada (409) e,
// quando cabe, empurra para fora os termos do dicionário de antes (opção
// `dicionario`, na ordem do prompt), que vêm em sairam_das_dicas; o termo de
// antes só ganha grafias (do_dicionario), e a dica dele não muda (com
// grafias, a resposta traz dica_ignorada; só a dica é 409); pedido sem
// mudança é 400; dica e remover de termo que não é da pessoa são 404. Tudo
// por POST, com o texto no corpo.

//
// O contador de mudanças do sync (ADR-021, S; GET /api/nuvem/mudancas): um
// número que o teste faz subir por fora, como um pull que trouxe algo
// (chegouDaNuvem, mudancasPorFora); o Agent de antes da S responde 404
// (agentSemContador).

//
// Os 6 meses no PC e as cópias do banco (ADR-019, F10d e F10e;
// internal/agent/localapi/retencao.go): GET /api/retencao é só números e
// booleanos; o OK (POST /api/retencao/ligar) leva a contagem que a tela
// mostrou e o Agent recusa com o número novo (409, ao lado do envelope) quando
// ela difere, e com "Não há nada a apagar agora." quando não há nada; o
// Desligar só tira o OK; o botão das cópias deixa as que o teste manda
// proteger (a do envio do histórico que não terminou). Um Agent de antes da
// F10 responde 404 sem frase (agentSemRetencao).

//
// As notas e os marcadores (ADR-022, N1; internal/agent/localapi/notas.go) seguem
// as regras do Agent que importam para o painel: GET /api/notas filtra por vista,
// marcador e q (sem diferenciar maiúscula nem acento), com as fixadas primeiro e,
// dentro de cada grupo, a ordem manual (ADR-022, N2b): as notas que o servidor já
// tinha começam na ordem de antes (fixadas, depois a última edição), a nota nova
// entra no topo do grupo, fixar, desafixar, arquivar e restaurar levam ao topo do
// grupo de destino, e editar não move. POST /api/notas/{id}/mover {antes, depois}
// põe a nota entre as duas vizinhas (409 se são de outro grupo, da lixeira, do
// Arquivo, inexistentes, a própria nota ou estão fora de ordem; 400 sem vizinha),
// sobe a versão e não mexe na hora de edição; o PATCH pede base_version e
// devolve 409 para a versão velha e para a nota que está na lixeira; todo
// POST/PATCH recusa campo desconhecido, nota vazia e cor fora das oito (400);
// mover para a lixeira, restaurar e todo PATCH sobem a versão; o apagar de vez
// só vale na lixeira (409 fora dela). notaPorFora faz o papel de outra aba.
//
// O selo da nota (N7): POST /api/notas/{id}/resolver-conflito {base_version, escolha}
// devolve a nota sem o selo e com a versão nova (manter fica com o texto, trocar põe
// o do selo); 400 sem base_version ou com escolha fora das duas; 409 com a versão
// velha ou sem selo.
//
// Os marcadores (N4) seguem o Agent: nome único sem diferenciar maiúscula nem acento
// (409 NAME_IN_USE, no POST e no PATCH), PATCH com base_version (409 CONFLICT para a
// versão velha), apagar tira o marcador das notas (a versão delas sobe, a hora não),
// e PUT/DELETE em /api/notas/{id}/marcadores/{marcador} são idempotentes, sobem a
// versão da nota só quando mudam algo e devolvem a nota (409 na lixeira, 404 sem o
// marcador).

import type { Aprendida, Buscador, Configuracoes, Ditado, ListaDeTermos, Provedor, ResultadoTeste, Sugestao, TermoAdicionado } from '../api'
import type { EstadoNuvem, EstadoRetencao } from '../clienteLocal'
import { itensDaEnumeracao } from '../notas/corpo'
import { CORES, type ItensSeparados, type Marcador, type Nota } from '../notas/tipos'

export interface Pedido {
  metodo: string
  caminho: string
  busca: URLSearchParams
  corpo?: Record<string, unknown>
  auth?: string
}

export function ditado(parcial: Partial<Ditado> & { id: string; created_at: string }): Ditado {
  return {
    raw_text: parcial.final_text ?? '',
    final_text: '',
    mode: 'default',
    asr_provider: 'whisper-local',
    status: 'done',
    updated_at: parcial.created_at,
    version: 1,
    ...parcial,
  }
}

const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })
const erro = (status: number, code: string, message: string) => json({ error: { code, message, request_id: 'r1' } }, status)

// Mais novo primeiro; no empate de data, id maior primeiro — como o
// ORDER BY da API.
function ordenar(itens: Ditado[], campo: 'created_at' | 'deleted_at'): Ditado[] {
  const t = (d: Ditado) => Date.parse(d[campo] ?? d.created_at)
  return itens.sort((a, b) => t(b) - t(a) || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0))
}

// O que o Agent devolve numa instalação nova, pelo contrato. Com a etapa 2
// (ADR-016) o Agent faz o Polish: polish_disponivel é true e
// polish_ultimo_motivo é "" (nenhum Polish ainda). Um teste que precisa do
// Agent de antes passa polish_disponivel: false (ou o tira).
export function configuracoesPadrao(parcial: Partial<Configuracoes> = {}): Configuracoes {
  return {
    hotkey: 'ctrl+win',
    hold_threshold_ms: 700,
    beep_enabled: true,
    microphone_always_ready: false,
    note_hotkey: 'ctrl+win+alt',
    note_trigger_enabled: true,
    text_mode: 'smart',
    ai_provider: '',
    ai_model: '',
    autostart: false,
    autostart_indisponivel: '',
    reinicio_pendente: [],
    polish_disponivel: true,
    polish_ultimo_motivo: '',
    version: 1,
    ...parcial,
  }
}

const editaveis = [
  'hold_threshold_ms',
  'beep_enabled',
  'microphone_always_ready',
  'note_trigger_enabled',
  'text_mode',
  'ai_provider',
  'ai_model',
  'autostart',
] as const
const provedores: readonly string[] = ['openai', 'anthropic']

export function nota(parcial: Partial<Nota> & { id: string }): Nota {
  const quando = parcial.updated_at ?? parcial.created_at ?? new Date(2026, 9, 25, 10).toISOString()
  return {
    titulo: '',
    corpo: '',
    cor: null,
    fixada: false,
    arquivada: false,
    marcadores: [],
    posicao: '',
    created_at: quando,
    updated_at: quando,
    version: 1,
    ...parcial,
  }
}

export function marcador(parcial: Partial<Marcador> & { id: string; nome: string }): Marcador {
  const quando = new Date(2026, 9, 20, 10).toISOString()
  return { created_at: quando, updated_at: quando, version: 1, ...parcial }
}

export interface OpcoesDoServidor {
  token?: string
  // notas e marcadores: o que o banco das notas já tinha (ADR-022).
  notas?: Nota[]
  marcadores?: Marcador[]
  porPagina?: number
  config?: Partial<Configuracoes>
  chaves?: Partial<Record<Provedor, string>>
  // exigemReinicio: campos que, mudados, entram em reinicio_pendente.
  exigemReinicio?: string[]
  // aprendidas: o que o painel já tinha aprendido.
  aprendidas?: Aprendida[]
  // termos: o que a pessoa já tinha adicionado (ADR-020).
  termos?: TermoAdicionado[]
  // dicionario: os termos do dicionário de antes que estão no prompt, na
  // ordem dele. Uma dica da pessoa que não deixa espaço os empurra para fora.
  dicionario?: string[]
  // nuvem: o estado da nuvem no começo (ADR-019, F6); sem ela, desligada.
  nuvem?: EstadoNuvem
  // retencao: o que o Agent diz dos 6 meses no começo (ADR-019, F10); sem ela,
  // nada a apagar, regra desligada e sem cópias (o bloco Histórico não aparece).
  retencao?: Partial<EstadoRetencao>
  // copiasProtegidas: quantas cópias o botão não apaga (a do envio do histórico
  // à nuvem que ainda não terminou).
  copiasProtegidas?: number
}

export function servidorFalso(
  inicial: Ditado[],
  {
    token = 'tok',
    notas: notasIniciais = [],
    marcadores: marcadoresIniciais = [],
    porPagina = 50,
    config: configInicial,
    chaves: chavesIniciais,
    exigemReinicio = [],
    aprendidas: aprendidasIniciais = [],
    termos: termosIniciais = [],
    dicionario = [],
    nuvem: nuvemInicial = { estado: 'desconectado', url: '' },
    retencao: retencaoInicial = {},
    copiasProtegidas: protegidas = 0,
  }: OpcoesDoServidor = {},
) {
  const itens = inicial.map((i) => ({ ...i }))
  const config = configuracoesPadrao(configInicial)
  const chaves: Partial<Record<Provedor, string>> = { ...chavesIniciais }
  const aprendidas: Aprendida[] = aprendidasIniciais.map((a) => ({ ...a }))
  const termos: TermoAdicionado[] = termosIniciais.map((t) => ({ ...t, variantes: [...t.variantes] }))
  // recusasDeTermos: o que a rota de termos responde em vez de gravar.
  const recusasDeTermos = new Map<string, { status: number; mensagem: string }>()
  // O que o próximo PATCH devolve como sugestões, e o que o Agent lembra
  // ter devolvido (id e versão do ditado, par).
  let sugestoesDoSalvar: Sugestao[] = []
  const lembradas = new Map<string, { versao: number; pares: Sugestao[] }>()
  // recusarAprender: o que POST /api/aprendizado responde em vez de gravar,
  // por "de" (ou para qualquer um, com '*').
  const recusasDoAprender = new Map<string, { status: number; mensagem: string }>()
  let resultadoDoTeste: ResultadoTeste = { ok: true, modelos: ['modelo-de-teste-1', 'modelo-de-teste-2'] }
  const pedidos: Pedido[] = []
  // A nuvem (ADR-019, F6), como o Agent a responde: o Conectar confere o
  // endereço como o build de desenvolvimento no bin\ (https ou
  // http://localhost) e abre o pedido com o código K7QM; o Desconectar é
  // também o Cancelar; o Conferir só devolve o estado (o teste muda o estado
  // por fora, como a aprovação no navegador ou a revogação no app).
  let nuvem: EstadoNuvem = { ...nuvemInicial }
  // O contador de mudanças do sync (ADR-021, S; GET /api/nuvem/mudancas): só um
  // número, que sobe quando o Agent grava no histórico algo que veio da nuvem.
  // O teste o faz subir por fora (chegouDaNuvem, mudancasPorFora), como um pull.
  // agentSemContador: o Agent de antes da S, que responde 404.
  let contador = 0
  let agentSemContador = false
  // O microfone bloqueado pelo Windows (internal/agent/localapi/microfone.go):
  // GET /api/microfone devolve {bloqueado, motivo}, e o POST de configuracoes é
  // o que abre a tela do Windows (204).
  let microfone = { bloqueado: false, motivo: '' }
  let abrirConfiguracoesFalha = false
  function rotaNuvem(p: Pedido): Response {
    if (p.caminho === '/api/nuvem' && p.metodo === 'GET') return json(nuvem)
    if (p.caminho === '/api/nuvem/mudancas' && p.metodo === 'GET') {
      return agentSemContador ? new Response('404 page not found', { status: 404 }) : json({ contador })
    }
    if (p.metodo !== 'POST') return erro(404, 'NOT_FOUND', 'rota')
    switch (p.caminho) {
      case '/api/nuvem/conferir':
        return json(nuvem)
      case '/api/nuvem/conectar': {
        const url = String(p.corpo?.url ?? '').replace(/\/+$/, '')
        if (!/^(https:\/\/[a-z0-9.-]+|http:\/\/localhost:\d+)$/.test(url)) return erro(400, 'BAD_REQUEST', 'Use https:// ou http://localhost.')
        if (nuvem.estado === 'conectado') return erro(409, 'CONFLICT', 'Este PC já está conectado. Desconecte antes de conectar de novo.')
        nuvem = { estado: 'conectando', url, conferencia: 'K7QM', abrir_url: `${url}/device/authorize?pedido=p1` }
        return json(nuvem)
      }
      case '/api/nuvem/desconectar':
        nuvem = { estado: 'desconectado', url: nuvem.url, email: nuvem.email, historico: nuvem.historico }
        return json(nuvem)
      // O envio inicial (F7c), como o Agent: só conectado; a cópia que
      // falha volta com a frase (recusaDoEnvio); o resto põe o histórico na
      // fila (o progresso começa em 0 de N).
      case '/api/nuvem/enviar': {
        if (nuvem.estado !== 'conectado') return erro(409, 'CONFLICT', 'Conecte o PC à nuvem antes de enviar o histórico.')
        if (!nuvem.historico?.conferido_em) return erro(409, 'CONFLICT', 'O PC ainda está conferindo a nuvem. Tente de novo em alguns segundos.')
        if (recusaDoEnvio) return erro(recusaDoEnvio.status, 'INTERNAL', recusaDoEnvio.mensagem)
        const h = nuvem.historico ?? { previa: 0, aceito: false, nao_subiram: 0, pendentes: 0, mudancas: 0, falhou: false, outra_nuvem: false }
        // F8 (D3): o clique solta também as trocas aprendidas (previa_trocas),
        // que sobem pela rodada e não entram no progresso do envio; com
        // previa 0, grava o aceite sem cópia e sem envio.
        const trocas = h.previa_trocas ?? 0
        nuvem = {
          ...nuvem,
          historico: {
            ...h,
            aceito: true,
            previa: 0,
            previa_trocas: 0,
            pendentes: h.pendentes + h.previa + trocas,
            envio: h.previa > 0 ? { feitos: 0, total: h.previa } : undefined,
          },
        }
        return json(nuvem)
      }
      // O esquecer (F7c): só desconectado e sem pedido.
      case '/api/nuvem/esquecer':
        if (nuvem.estado === 'conectado') return erro(409, 'CONFLICT', 'Desconecte antes de esquecer a nuvem.')
        if (nuvem.estado === 'conectando') return erro(409, 'CONFLICT', 'Cancele o pedido em curso antes de esquecer a nuvem.')
        nuvem = { estado: 'desconectado', url: '' }
        return json(nuvem)
    }
    return erro(404, 'NOT_FOUND', 'rota')
  }
  // Os 6 meses (F10d e F10e), como o Agent: ver o cabeçalho deste arquivo.
  let retencao: EstadoRetencao = {
    previa: 0,
    na_lixeira: 0,
    com_selo: 0,
    de_outro_dono: 0,
    ligada: false,
    pausada: false,
    copias: 0,
    copia_dos_6_meses: false,
    ...retencaoInicial,
  }
  // comoOAgent: o estado como o Agent o devolve. As apagáveis são as cópias
  // menos as protegidas (a do envio do histórico à nuvem que não terminou).
  const comoOAgent = (): EstadoRetencao => ({ ...retencao, copias_apagaveis: retencao.copias - Math.min(retencao.copias, protegidas) })
  // agentVelho: o Agent de antes da F10, sem as rotas (404 sem frase).
  let agentVelho = false
  // recusaDaRetencao: o que o próximo OK responde (a cópia que falhou, o disco
  // cheio), em vez de apagar.
  let recusaDaRetencao: { status: number; mensagem: string } | null = null
  // emParte: o próximo OK grava o OK e apaga só os primeiros apagados (um lote
  // seguinte caiu). 'incompleto' é o Agent desta versão (200, com incompleto);
  // 'falha' é a resposta que se perdeu depois do OK (500 com a frase).
  let emParte: { apagados: number; resposta: 'incompleto' | 'falha' } | null = null
  function rotaRetencao(p: Pedido): Response {
    if (agentVelho) return new Response('404 page not found', { status: 404 })
    if (p.caminho === '/api/retencao' && p.metodo === 'GET') return json(comoOAgent())
    if (p.metodo !== 'POST') return erro(404, 'NOT_FOUND', 'rota')
    switch (p.caminho) {
      case '/api/retencao/ligar': {
        const previa = p.corpo?.previa
        if (typeof previa !== 'number' || previa < 0) return erro(400, 'BAD_REQUEST', 'Faltou a contagem que a tela mostrou.')
        if (retencao.ligada) return json({ ...comoOAgent(), apagados: 0 })
        if (recusaDaRetencao) return erro(recusaDaRetencao.status, recusaDaRetencao.status === 409 ? 'CONFLICT' : 'INTERNAL', recusaDaRetencao.mensagem)
        if (retencao.previa === 0) return erro(409, 'CONFLICT', 'Não há nada a apagar agora.')
        if (previa !== retencao.previa) {
          return json({ error: { code: 'CONFLICT', message: 'A contagem mudou. Nada foi apagado.', request_id: 'r1' }, previa: retencao.previa }, 409)
        }
        // A cópia dos 6 meses é uma só: com a de um OK anterior ao lado do
        // banco, o OK não copia de novo.
        const copias = retencao.copias + (retencao.copia_dos_6_meses ? 0 : 1)
        if (emParte) {
          const { apagados, resposta } = emParte
          emParte = null
          retencao = { ...retencao, previa: retencao.previa - apagados, ligada: true, pausada: false, copias, copia_dos_6_meses: true }
          if (resposta === 'falha') return erro(500, 'INTERNAL', 'Não deu para apagar agora. Tente de novo.')
          return json({ ...comoOAgent(), apagados, incompleto: true })
        }
        const apagados = retencao.previa
        retencao = { ...retencao, previa: 0, na_lixeira: 0, com_selo: 0, ligada: true, pausada: false, copias, copia_dos_6_meses: true }
        return json({ ...comoOAgent(), apagados, incompleto: false })
      }
      case '/api/retencao/desligar':
        retencao = { ...retencao, ligada: false, pausada: false }
        return json(comoOAgent())
      case '/api/retencao/copias/apagar': {
        const ficam = Math.min(retencao.copias, protegidas)
        const apagadas = retencao.copias - ficam
        // A dos 6 meses nunca é a protegida: sai.
        retencao = { ...retencao, copias: ficam, copia_dos_6_meses: false }
        return json({ ...comoOAgent(), apagadas, ficaram: ficam })
      }
    }
    return erro(404, 'NOT_FOUND', 'rota')
  }
  // recusaDoEnvio: o que o próximo "Enviar o histórico" responde (a cópia
  // que falhou), em vez de enviar.
  let recusaDoEnvio: { status: number; mensagem: string } | null = null
  // interceptar: se devolver uma Response, ela vence a rota normal.
  let interceptar: ((p: Pedido) => Response | undefined) | null = null
  // portao: pedidos que casam com o filtro esperam até ele abrir, e só
  // então passam pela rota normal — é assim que um teste faz uma resposta
  // chegar tarde.
  let portao: { filtro: (p: Pedido) => boolean; aberto: Promise<void> } | null = null
  let foraDoAr = false
  // iaDaLista: o que a IA da pessoa "devolve" em POST /api/notas/separar-itens
  // (N5), já depois das guardas do Agent: os itens aceitos, ou null (recusada,
  // sem chave, sem rede), e aí vale a regra. Só é chamada com o Polish ligado
  // nas preferências, como no Agent.
  let iaDaLista: ((trecho: string) => string[] | null) | null = null

  // Como o Go (localapi, settingsPutDTO): campo AUSENTE do PUT mantém o
  // valor atual, e só o que veio é conferido e gravado.
  function rotaConfiguracoes(p: Pedido): Response {
    if (p.metodo === 'GET') return json(config)
    if (p.metodo !== 'PUT') return erro(405, 'BAD_REQUEST', 'método')
    const corpo = p.corpo ?? {}
    if (corpo.base_version !== config.version) return erro(409, 'CONFLICT', 'versão desatualizada')
    const c: Record<string, unknown> = { ...config }
    for (const k of editaveis) if (k in corpo) c[k] = corpo[k]
    const ms = c.hold_threshold_ms
    if (typeof ms !== 'number' || ms < 300 || ms > 2000) return erro(400, 'BAD_REQUEST', 'hold_threshold_ms precisa estar entre 300 e 2000')
    if (!['literal', 'smart', 'polish'].includes(String(c.text_mode))) return erro(400, 'BAD_REQUEST', 'text_mode inválido')
    if (c.ai_provider !== '' && !provedores.includes(String(c.ai_provider))) return erro(400, 'BAD_REQUEST', 'ai_provider inválido')
    if (c.text_mode === 'polish' && !chaves[c.ai_provider as Provedor]) {
      return erro(400, 'BAD_REQUEST', 'o modo Polish precisa da chave de API do provedor escolhido')
    }
    if (config.autostart_indisponivel && c.autostart !== config.autostart) return erro(400, 'BAD_REQUEST', config.autostart_indisponivel)
    for (const k of editaveis) {
      if (c[k] !== config[k] && exigemReinicio.includes(k) && !config.reinicio_pendente.includes(k)) {
        config.reinicio_pendente = [...config.reinicio_pendente, k]
      }
      Object.assign(config, { [k]: c[k] })
    }
    config.version += 1
    return json(config)
  }

  function rotaChave(p: Pedido): Response {
    const provedor = (p.metodo === 'PUT' ? p.corpo?.provedor : p.busca.get('provedor')) as Provedor
    if (!provedores.includes(String(provedor))) return erro(400, 'BAD_REQUEST', 'provedor inválido')
    switch (p.metodo) {
      case 'GET': {
        const k = chaves[provedor]
        return json({ provedor, configurada: Boolean(k), final: k ? k.slice(-4) : '' })
      }
      case 'PUT': {
        const k = p.corpo?.chave
        if (typeof k !== 'string' || k.trim() === '') return erro(400, 'BAD_REQUEST', 'chave vazia')
        chaves[provedor] = k
        return new Response(null, { status: 204 })
      }
      case 'DELETE':
        delete chaves[provedor]
        return new Response(null, { status: 204 })
    }
    return erro(405, 'BAD_REQUEST', 'método')
  }

  function rotaTestar(p: Pedido): Response {
    const provedor = p.corpo?.provedor as Provedor
    if (!provedores.includes(String(provedor))) return erro(400, 'BAD_REQUEST', 'provedor inválido')
    if (!chaves[provedor]) return json({ ok: false, erro: 'Nenhuma chave guardada para este provedor.' })
    return json(resultadoDoTeste)
  }

  // As rotas do aprendizado. Uma troca por POST, como no Agent.
  function rotaAprendizado(p: Pedido): Response {
    if (p.caminho === '/api/aprendizado' && p.metodo === 'GET') return json({ items: aprendidas })
    if (p.caminho === '/api/aprendizado/desfazer' && p.metodo === 'POST') {
      const variante = String(p.corpo?.variante ?? '')
      // Só a troca que vale aqui (sem situação): a linha de estado do sync
      // (F8) não está no dicionário, e o Agent responde 404, como a quem
      // nunca foi aprendida.
      const i = aprendidas.findIndex((a) => !a.situacao && a.variante.toLowerCase() === variante.toLowerCase())
      if (i < 0) return erro(404, 'NOT_FOUND', `"${variante}" não foi aprendido pelo painel.`)
      const [tirada] = aprendidas.splice(i, 1)
      return json(tirada)
    }
    if (p.caminho !== '/api/aprendizado' || p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')

    const id = String(p.corpo?.id ?? '')
    const de = String(p.corpo?.de ?? '')
    const para = String(p.corpo?.para ?? '')
    const item = itens.find((i) => i.id === id)
    if (!item) return erro(404, 'NOT_FOUND', 'Esse ditado não existe mais.')
    const recusa = recusasDoAprender.get(de) ?? recusasDoAprender.get('*')
    if (recusa) return erro(recusa.status, recusa.status === 409 ? 'CONFLICT' : 'BAD_REQUEST', recusa.mensagem)
    const lembrada = lembradas.get(id)
    if (!lembrada || lembrada.versao !== item.version || !lembrada.pares.some((s) => s.de === de && s.para === para)) {
      return erro(409, 'CONFLICT', 'Essa sugestão não vale mais; corrija o ditado de novo para aprender.')
    }
    if (aprendidas.some((a) => !a.situacao && a.variante.toLowerCase() === de.toLowerCase())) {
      return erro(409, 'CONFLICT', `"${de}" já vira "${para}" no dicionário.`)
    }
    const nova = { termo: para, variante: de }
    aprendidas.push(nova)
    return json(nova)
  }

  // O "Adicionar termo". Orçamento de 140 caracteres; as dicas da pessoa
  // primeiro, depois as do dicionário que ainda couberem.
  const orcamento = 140
  const igual = (a: string, b: string) => a.toLowerCase() === b.toLowerCase()
  const usadas = () => {
    const d = termos.filter((t) => t.dica)
    return d.reduce((n, t) => n + t.termo.length, 0) + Math.max(0, d.length - 1) * 2
  }
  const noPrompt = () => {
    let n = usadas()
    const dentro: string[] = []
    for (const t of dicionario) {
      const custo = t.length + (n > 0 ? 2 : 0)
      if (n + custo <= orcamento) {
        dentro.push(t)
        n += custo
      }
    }
    return dentro
  }
  const lista = (): ListaDeTermos => ({ items: termos.map((t) => ({ ...t, variantes: [...t.variantes] })), dicas: { usadas: usadas(), orcamento } })
  function mudou(termo: string, jaExistia: boolean, antes: string[], dicaIgnorada = false): Response {
    const agora = noPrompt()
    return json({
      ...lista(),
      termo,
      ja_existia: jaExistia,
      sairam_das_dicas: antes.filter((t) => !agora.includes(t)),
      ...(dicaIgnorada ? { dica_ignorada: true } : {}),
    })
  }
  const dicaDeFabrica = (termo: string) => erro(409, 'CONFLICT', `“${termo}” já é do dicionário: a dica dele não muda por aqui.`)
  function cabe(t: TermoAdicionado): Response | null {
    const custo = t.termo.length + (usadas() > 0 ? 2 : 0)
    if (usadas() + custo <= orcamento) return null
    return erro(409, 'CONFLICT', `Não cabe nas dicas: sobram ${orcamento - usadas()} caracteres.`)
  }
  function rotaTermos(p: Pedido): Response {
    const recusa = recusasDeTermos.get(p.caminho)
    if (recusa && p.metodo === 'POST') return erro(recusa.status, recusa.status === 409 ? 'CONFLICT' : 'BAD_REQUEST', recusa.mensagem)
    if (p.caminho === '/api/termos' && p.metodo === 'GET') return json(lista())
    if (p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
    const termo = String(p.corpo?.termo ?? '').trim()
    const antes = noPrompt()
    if (p.caminho === '/api/termos') {
      if (!termo) return erro(400, 'BAD_REQUEST', 'Faltou a palavra certa.')
      const variantes = (p.corpo?.variantes as string[] | undefined) ?? []
      const dica = p.corpo?.dica === true
      const dele = termos.find((t) => igual(t.termo, termo))
      const deAntes = dicionario.find((t) => igual(t, termo))
      if (!dele) {
        if (deAntes && variantes.length === 0) return dica ? dicaDeFabrica(termo) : erro(400, 'BAD_REQUEST', `“${termo}” já está no dicionário assim.`)
        const novo = { termo: deAntes ?? termo, variantes: [...variantes], dica: deAntes ? false : dica, do_dicionario: Boolean(deAntes) }
        if (novo.dica) {
          const r = cabe(novo)
          if (r) return r
        }
        termos.push(novo)
        return mudou(novo.termo, Boolean(deAntes), antes, Boolean(deAntes) && dica)
      }
      if (dele.do_dicionario && variantes.length === 0 && dica) return dicaDeFabrica(termo)
      const novaDica = dele.do_dicionario ? dele.dica : dica
      if (variantes.length === 0 && novaDica === dele.dica) return erro(400, 'BAD_REQUEST', `“${dele.termo}” já está no dicionário assim.`)
      if (novaDica && !dele.dica) {
        const r = cabe(dele)
        if (r) return r
      }
      dele.variantes.push(...variantes)
      dele.dica = novaDica
      return mudou(dele.termo, true, antes, dele.do_dicionario && dica)
    }
    const dele = termos.find((t) => igual(t.termo, termo))
    if (p.caminho === '/api/termos/dica') {
      if (!dele || dele.do_dicionario) return erro(404, 'NOT_FOUND', `“${termo}” não foi adicionado por você.`)
      const ligar = p.corpo?.dica === true
      if (ligar && !dele.dica) {
        const r = cabe(dele)
        if (r) return r
      }
      dele.dica = ligar
      return mudou(dele.termo, true, antes)
    }
    if (p.caminho === '/api/termos/remover') {
      if (!dele) return erro(404, 'NOT_FOUND', `“${termo}” não foi adicionado por você.`)
      termos.splice(termos.indexOf(dele), 1)
      return mudou(dele.termo, true, antes)
    }
    return erro(404, 'NOT_FOUND', 'rota')
  }

  // As notas e os marcadores. O relógio é um contador de milissegundos: duas
  // gravações no mesmo instante ainda saem em ordem.
  const bancoDeNotas = notasIniciais.map((n) => ({ ...n, marcadores: [...n.marcadores] }))
  // A ordem manual: um número por nota (o topo é o menor), que a nota mostra como
  // texto em `posicao`. As notas do começo ficam na ordem de antes da N2b (fixadas,
  // depois a última edição, e a mais nova do array desempata).
  const ordemDasNotas = new Map<string, number>()
  bancoDeNotas
    .map((n, i) => ({ n, i }))
    .sort((a, b) => Number(b.n.fixada) - Number(a.n.fixada) || Date.parse(b.n.updated_at) - Date.parse(a.n.updated_at) || b.i - a.i)
    .forEach(({ n }, i) => ordemDasNotas.set(n.id, i * 1024))
  const textoDaPosicao = (n: Nota) => `p${String(ordemDasNotas.get(n.id) ?? 0)}`
  const comPosicao = (n: Nota): Nota => ({ ...n, marcadores: [...n.marcadores], posicao: textoDaPosicao(n) })
  const mesmoGrupo = (a: Nota, b: Nota) => !a.deleted_at && !b.deleted_at && a.fixada === b.fixada && a.arquivada === b.arquivada
  // noTopo põe a nota acima da primeira do grupo dela.
  function noTopo(n: Nota) {
    const outras = bancoDeNotas.filter((x) => x.id !== n.id && mesmoGrupo(x, n)).map((x) => ordemDasNotas.get(x.id) ?? 0)
    ordemDasNotas.set(n.id, outras.length === 0 ? 0 : Math.min(...outras) - 1024)
  }
  const bancoDeMarcadores = marcadoresIniciais.map((m) => ({ ...m }))
  let relogioDasNotas = Date.UTC(2026, 9, 25, 12)
  let seqDasNotas = 0
  let seqDosMarcadores = 0
  const agoraDasNotas = () => new Date((relogioDasNotas += 1000)).toISOString()
  const dobrar = (s: string) => s.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase()
  const MAX_NOME_DO_MARCADOR = 50
  const camposDoMarcador = ['nome', 'base_version']
  const camposDoPost = ['titulo', 'corpo', 'cor', 'fixada', 'arquivada', 'ditado_de_origem', 'marcadores', 'token']
  const camposDoPatch = ['base_version', 'titulo', 'corpo', 'cor', 'fixada', 'arquivada']
  const corValida = (c: unknown) => c === null || c === '' || (typeof c === 'string' && (CORES as readonly string[]).includes(c))
  const ehTexto = (v: unknown) => v === undefined || typeof v === 'string'

  // "Virar nota" (ADR-022, N3): o ditado que virou nota sai das listas do
  // histórico (sem ir para a lixeira), e a coluna virou_nota guarda o id da nota.
  const virouNota = new Map<string, string>()

  // "Ditado feito dentro da nota" (ADR-022): o aviso de que o cursor está numa
  // nota, só na memória como no Agent. O valor é o que o Agent gravaria em
  // virou_nota: o id da nota ou "nova:<token>". dictadosDaCaixa guarda os ids
  // dos ditados que a caixa (token) escondeu, para o vínculo e o descartar.
  let foco: string | null = null
  const ditadosDaCaixa = new Map<string, string[]>()

  // rotaSepararItens é POST /api/notas/separar-itens (N5), como o Agent: 400
  // para o trecho vazio ou de várias linhas; a IA só com o Polish ligado; a
  // regra no resto (a mesma do painel e do Agent).
  function rotaSepararItens(p: Pedido): Response {
    if (p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
    if (Object.keys(p.corpo ?? {}).some((k) => k !== 'trecho')) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
    const trecho = typeof p.corpo?.trecho === 'string' ? p.corpo.trecho.trim() : ''
    if (!trecho) return erro(400, 'BAD_REQUEST', 'Escolha uma linha com texto para separar em itens.')
    if (/[\r\n]/.test(trecho)) return erro(400, 'BAD_REQUEST', 'Separo em itens uma linha por vez.')
    const ligada = config.text_mode === 'polish' && config.ai_provider !== '' && config.ai_model.trim() !== ''
    const daIA = ligada && iaDaLista ? iaDaLista(trecho) : null
    if (daIA && daIA.length >= 2) return json({ itens: daIA, como: 'ia' } satisfies ItensSeparados)
    const itens = itensDaEnumeracao(trecho, 2)
    return json((itens ? { itens, como: 'regra' } : { itens: [], como: 'nenhum' }) satisfies ItensSeparados)
  }

  // rotaFoco é PUT e DELETE /api/notas/foco, com as recusas do Agent.
  function rotaFoco(p: Pedido): Response {
    const c = p.corpo ?? {}
    const { nota, token, descartar } = c as { nota?: unknown; token?: unknown; descartar?: unknown }
    if (Object.keys(c).some((k) => !['nota', 'token', 'descartar'].includes(k))) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
    const valorDe = () => (typeof nota === 'string' && nota ? nota : typeof token === 'string' && token ? `nova:${token}` : null)
    if (p.metodo === 'PUT') {
      if (descartar || (typeof nota === 'string' && nota ? 1 : 0) + (typeof token === 'string' && token ? 1 : 0) !== 1) {
        return erro(400, 'BAD_REQUEST', 'informe a nota ou o token, um dos dois')
      }
      if (typeof nota === 'string' && nota) {
        const n = bancoDeNotas.find((x) => x.id === nota)
        if (!n) return erro(404, 'NOT_FOUND', 'registro não encontrado')
        if (n.deleted_at) return erro(409, 'CONFLICT', 'a nota está na lixeira: restaure antes de mexer nela')
      }
      foco = valorDe()
      return new Response(null, { status: 204 })
    }
    if (p.metodo === 'DELETE') {
      const v = valorDe()
      if (descartar === true && typeof token === 'string' && token) {
        for (const d of ditadosDaCaixa.get(token) ?? []) virouNota.delete(d)
        ditadosDaCaixa.delete(token)
      }
      if (v === null || v === foco) foco = null
      return new Response(null, { status: 204 })
    }
    return erro(405, 'BAD_REQUEST', 'método')
  }

  // virarNota é POST /api/history/{id}/virar-nota, como o Agent: 404, 409
  // IN_TRASH, 409 ALREADY_NOTE, e a nota nasce com o texto final (o bruto, se o
  // final está vazio), sem título nem cor, com ditado_de_origem.
  function rotaVirarNota(p: Pedido, id: string): Response {
    if (p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
    const d = itens.find((i) => i.id === id)
    if (!d) return erro(404, 'NOT_FOUND', 'registro não encontrado')
    if (d.deleted_at) return erro(409, 'IN_TRASH', 'o ditado está na lixeira: restaure antes de virar nota')
    if (virouNota.has(id)) return erro(409, 'ALREADY_NOTE', 'este ditado já virou nota')
    const corpo = d.final_text.trim() !== '' ? d.final_text : d.raw_text
    if (corpo.trim() === '') return erro(400, 'BAD_REQUEST', 'a nota está vazia')
    const quando = agoraDasNotas()
    const nova: Nota = {
      id: `nota-${++seqDasNotas}`,
      titulo: '',
      corpo,
      cor: null,
      fixada: false,
      arquivada: false,
      marcadores: [],
      posicao: '',
      ditado_de_origem: id,
      created_at: quando,
      updated_at: quando,
      version: 1,
    }
    bancoDeNotas.push(nova)
    noTopo(nova)
    virouNota.set(id, nova.id)
    return json(comPosicao(nova), 201)
  }

  // desfazerVirar é POST /api/notas/{id}/desfazer-virar: o ditado volta; a nota
  // sai se está na versão 1 e fora da lixeira, e fica se foi editada.
  function desfazerVirar(n: Nota): Response {
    const ditado = n.ditado_de_origem
    if (!ditado || virouNota.get(ditado) !== n.id) {
      return erro(409, 'CONFLICT', 'a nota não está virada de um ditado, ou o ditado já voltou ao histórico')
    }
    virouNota.delete(ditado)
    const apagada = n.version === 1 && !n.deleted_at
    if (apagada) bancoDeNotas.splice(bancoDeNotas.indexOf(n), 1)
    return json({ ditado_id: ditado, nota_apagada: apagada })
  }

  // nomeDoMarcador limpa o nome como o Agent (espaços das pontas fora, sequências
  // viram um) e o recusa vazio ou grande demais.
  function nomeDoMarcador(bruto: unknown): string | null {
    if (typeof bruto !== 'string') return null
    const nome = bruto.split(/\s+/).filter(Boolean).join(' ')
    return nome === '' || [...nome].length > MAX_NOME_DO_MARCADOR ? null : nome
  }
  const nomeEmUso = (nome: string, exceto = '') => bancoDeMarcadores.some((x) => x.id !== exceto && dobrar(x.nome) === dobrar(nome))
  const porNomeDoMarcador = (a: Marcador, b: Marcador) => dobrar(a.nome).localeCompare(dobrar(b.nome)) || a.id.localeCompare(b.id)

  // rotaMarcadores é /api/marcadores e /api/marcadores/{id}.
  function rotaMarcadores(p: Pedido): Response {
    const m = p.caminho.match(/^\/api\/marcadores(?:\/([^/]+))?$/)
    if (!m) return erro(404, 'NOT_FOUND', 'rota')
    if (!m[1]) {
      if (p.metodo === 'GET') return json({ items: [...bancoDeMarcadores].sort(porNomeDoMarcador).map((x) => ({ ...x })) })
      if (p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
      const c = p.corpo ?? {}
      if (Object.keys(c).some((k) => k !== 'nome')) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
      const nome = nomeDoMarcador(c.nome)
      if (nome === null) return erro(400, 'BAD_REQUEST', 'nome de marcador inválido: de 1 a 50 caracteres, em uma linha')
      if (nomeEmUso(nome)) return erro(409, 'NAME_IN_USE', 'já existe um marcador com esse nome')
      const quando = agoraDasNotas()
      const novo: Marcador = { id: `marcador-${++seqDosMarcadores}`, nome, created_at: quando, updated_at: quando, version: 1 }
      bancoDeMarcadores.push(novo)
      return json({ ...novo }, 201)
    }
    const id = decodeURIComponent(m[1])
    const alvo = bancoDeMarcadores.find((x) => x.id === id)
    if (!alvo) return erro(404, 'NOT_FOUND', 'registro não encontrado')
    if (p.metodo === 'DELETE') {
      bancoDeMarcadores.splice(bancoDeMarcadores.indexOf(alvo), 1)
      for (const n of bancoDeNotas) {
        if (!n.marcadores.includes(id)) continue
        n.marcadores = n.marcadores.filter((x) => x !== id)
        n.version += 1
      }
      return new Response(null, { status: 204 })
    }
    if (p.metodo !== 'PATCH') return erro(405, 'BAD_REQUEST', 'método')
    const c = p.corpo ?? {}
    if (Object.keys(c).some((k) => !camposDoMarcador.includes(k))) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
    if (typeof c.base_version !== 'number') return erro(400, 'BAD_REQUEST', 'base_version é obrigatório')
    const nome = nomeDoMarcador(c.nome)
    if (nome === null) return erro(400, 'BAD_REQUEST', 'nome de marcador inválido: de 1 a 50 caracteres, em uma linha')
    if (c.base_version !== alvo.version) return erro(409, 'CONFLICT', 'versão desatualizada')
    if (nomeEmUso(nome, id)) return erro(409, 'NAME_IN_USE', 'já existe um marcador com esse nome')
    alvo.nome = nome
    alvo.version += 1
    alvo.updated_at = agoraDasNotas()
    return json({ ...alvo })
  }

  // marcadoresDaNota é PUT e DELETE /api/notas/{id}/marcadores/{marcador}.
  function marcadoresDaNota(p: Pedido, n: Nota, marcadorId: string): Response {
    if (p.metodo !== 'PUT' && p.metodo !== 'DELETE') return erro(405, 'BAD_REQUEST', 'método')
    if (n.deleted_at) return erro(409, 'CONFLICT', 'a nota está na lixeira: restaure antes de mexer nela')
    const tem = n.marcadores.includes(marcadorId)
    if (p.metodo === 'PUT') {
      if (!bancoDeMarcadores.some((x) => x.id === marcadorId)) return erro(404, 'NOT_FOUND', 'registro não encontrado')
      if (!tem) {
        const nomes = new Map(bancoDeMarcadores.map((x) => [x.id, x]))
        n.marcadores = [...n.marcadores, marcadorId].sort((a, b) => porNomeDoMarcador(nomes.get(a)!, nomes.get(b)!))
      }
    } else if (tem) {
      n.marcadores = n.marcadores.filter((x) => x !== marcadorId)
    }
    const mudou = p.metodo === 'PUT' ? !tem : tem
    if (mudou) {
      n.version += 1
      n.updated_at = agoraDasNotas()
    }
    return json(comPosicao(n))
  }

  // moverNota é POST /api/notas/{id}/mover, como o Agent.
  function moverNota(p: Pedido, n: Nota): Response {
    if (p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
    const c = p.corpo ?? {}
    if (Object.keys(c).some((k) => k !== 'antes' && k !== 'depois')) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
    const ehTextoOuNulo = (v: unknown) => v === undefined || v === null || typeof v === 'string'
    if (!ehTextoOuNulo(c.antes) || !ehTextoOuNulo(c.depois)) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
    const antesId = typeof c.antes === 'string' && c.antes !== '' ? c.antes : null
    const depoisId = typeof c.depois === 'string' && c.depois !== '' ? c.depois : null
    if (!antesId && !depoisId) return erro(400, 'BAD_REQUEST', 'mover pede a nota de antes ou a de depois')
    if (n.deleted_at) return erro(409, 'CONFLICT', 'a nota está na lixeira: restaure antes de mexer nela')
    const invalida = () => erro(409, 'CONFLICT', 'as notas vizinhas mudaram (ou não estão no mesmo grupo): a lista será lida de novo')
    const vizinha = (id: string | null) => (id ? bancoDeNotas.find((x) => x.id === id && x.id !== n.id && mesmoGrupo(x, n)) : undefined)
    const antes = vizinha(antesId)
    const depois = vizinha(depoisId)
    if ((antesId && !antes) || (depoisId && !depois)) return invalida()
    const a = antes ? (ordemDasNotas.get(antes.id) ?? 0) : -Infinity
    const d = depois ? (ordemDasNotas.get(depois.id) ?? 0) : Infinity
    if (a >= d) return invalida()
    const atual = ordemDasNotas.get(n.id) ?? 0
    if (a < atual && atual < d) return json(comPosicao(n)) // já está entre as duas: nada a gravar
    ordemDasNotas.set(n.id, a === -Infinity ? d - 1024 : d === Infinity ? a + 1024 : (a + d) / 2)
    n.version += 1 // a hora de edição não muda: arrastar não é editar
    return json(comPosicao(n))
  }

  // resolverConflitoDaNota é POST /api/notas/{id}/resolver-conflito, como o Agent:
  // 400 sem base_version ou com escolha fora de manter/trocar; 409 com a versão velha
  // ou sem selo. Devolve a nota sem o selo e com a versão nova.
  function resolverConflitoDaNota(p: Pedido, n: Nota): Response {
    if (p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
    const c = p.corpo ?? {}
    if (Object.keys(c).some((k) => k !== 'base_version' && k !== 'escolha')) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
    if (typeof c.base_version !== 'number') return erro(400, 'BAD_REQUEST', 'base_version é obrigatório')
    if (c.escolha !== 'manter' && c.escolha !== 'trocar') return erro(400, 'BAD_REQUEST', 'escolha do selo inválida: use manter ou trocar')
    if (c.base_version !== n.version || !n.conflito) return erro(409, 'CONFLICT', 'versão desatualizada')
    if (c.escolha === 'trocar') {
      n.titulo = n.conflito.titulo
      n.corpo = n.conflito.corpo
    }
    delete n.conflito
    n.version += 1
    n.updated_at = agoraDasNotas()
    return json(comPosicao(n))
  }

  function rotaNotas(p: Pedido): Response {
    const m = p.caminho.match(/^\/api\/notas(?:\/([^/]+)(\/restaurar|\/mover|\/resolver-conflito|\/definitivo|\/desfazer-virar|\/marcadores\/[^/]+)?)?$/)
    if (!m) return erro(404, 'NOT_FOUND', 'rota')
    const [, idBruto, sufixo] = m
    if (!idBruto) {
      if (p.metodo === 'GET') {
        const vista = p.busca.get('vista') ?? 'ativas'
        if (!['ativas', 'arquivo', 'lixeira'].includes(vista)) return erro(400, 'BAD_REQUEST', 'parâmetro vista inválido: use ativas, arquivo ou lixeira')
        const q = dobrar(p.busca.get('q') ?? '')
        const marcadorId = p.busca.get('marcador') ?? ''
        const itens = bancoDeNotas
          .filter((n) => (vista === 'lixeira' ? Boolean(n.deleted_at) : !n.deleted_at && n.arquivada === (vista === 'arquivo')))
          .filter((n) => !marcadorId || n.marcadores.includes(marcadorId))
          .filter((n) => !q || dobrar(`${n.titulo}\n${n.corpo}`).includes(q))
        const apagadaEm = (n: Nota) => Date.parse(n.deleted_at ?? n.updated_at)
        itens.sort((a, b) =>
          vista === 'lixeira' ? apagadaEm(b) - apagadaEm(a) : Number(b.fixada) - Number(a.fixada) || (ordemDasNotas.get(a.id) ?? 0) - (ordemDasNotas.get(b.id) ?? 0),
        )
        return json({ items: itens.map(comPosicao) })
      }
      if (p.metodo !== 'POST') return erro(405, 'BAD_REQUEST', 'método')
      const c = p.corpo ?? {}
      if (Object.keys(c).some((k) => !camposDoPost.includes(k))) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
      if (!ehTexto(c.titulo) || !ehTexto(c.corpo)) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
      if ('cor' in c && !corValida(c.cor)) return erro(400, 'BAD_REQUEST', 'cor inválida: use ' + CORES.join(', ') + ' ou null')
      const titulo = (c.titulo as string | undefined) ?? ''
      const corpo = (c.corpo as string | undefined) ?? ''
      if (titulo === '' && corpo === '') return erro(400, 'BAD_REQUEST', 'a nota está vazia')
      const quando = agoraDasNotas()
      const nova: Nota = {
        id: `nota-${++seqDasNotas}`,
        titulo,
        corpo,
        cor: (c.cor || null) as Nota['cor'],
        fixada: c.fixada === true,
        arquivada: c.arquivada === true,
        marcadores: [],
        posicao: '',
        ...(typeof c.ditado_de_origem === 'string' && c.ditado_de_origem ? { ditado_de_origem: c.ditado_de_origem } : {}),
        created_at: quando,
        updated_at: quando,
        version: 1,
      }
      bancoDeNotas.push(nova)
      noTopo(nova)
      // O token liga à nota os ditados feitos na caixa (virou_nota = id).
      if (typeof c.token === 'string' && c.token) {
        for (const d of ditadosDaCaixa.get(c.token) ?? []) virouNota.set(d, nova.id)
        ditadosDaCaixa.delete(c.token)
      }
      return json(comPosicao(nova), 201)
    }

    const id = decodeURIComponent(idBruto)
    const n = bancoDeNotas.find((x) => x.id === id)
    if (!n) return erro(404, 'NOT_FOUND', 'registro não encontrado')
    if (sufixo === '/restaurar' && p.metodo === 'POST') {
      if (n.deleted_at) {
        delete n.deleted_at
        n.version += 1
        n.updated_at = agoraDasNotas()
        noTopo(n)
      }
      return json(comPosicao(n))
    }
    if (sufixo === '/mover') return moverNota(p, n)
    if (sufixo === '/resolver-conflito') return resolverConflitoDaNota(p, n)
    if (sufixo === '/desfazer-virar' && p.metodo === 'POST') return desfazerVirar(n)
    if (sufixo === '/definitivo' && p.metodo === 'DELETE') {
      if (!n.deleted_at) return erro(409, 'CONFLICT', 'só se apaga de vez a nota que está na lixeira')
      bancoDeNotas.splice(bancoDeNotas.indexOf(n), 1)
      return new Response(null, { status: 204 })
    }
    if (sufixo?.startsWith('/marcadores/')) return marcadoresDaNota(p, n, decodeURIComponent(sufixo.slice('/marcadores/'.length)))
    if (sufixo) return erro(405, 'BAD_REQUEST', 'método')
    switch (p.metodo) {
      case 'GET':
        return json(comPosicao(n))
      case 'DELETE':
        if (!n.deleted_at) {
          n.deleted_at = agoraDasNotas()
          n.updated_at = n.deleted_at
          n.version += 1
        }
        return new Response(null, { status: 204 })
      case 'PATCH': {
        const c = p.corpo ?? {}
        if (Object.keys(c).some((k) => !camposDoPatch.includes(k))) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
        if (typeof c.base_version !== 'number') return erro(400, 'BAD_REQUEST', 'base_version é obrigatório')
        if (Object.keys(c).length === 1) return erro(400, 'BAD_REQUEST', 'nada a mudar: mande titulo, corpo, cor, fixada ou arquivada')
        if (!ehTexto(c.titulo) || !ehTexto(c.corpo)) return erro(400, 'BAD_REQUEST', 'corpo JSON inválido')
        if ('cor' in c && !corValida(c.cor)) return erro(400, 'BAD_REQUEST', 'cor inválida: use ' + CORES.join(', ') + ' ou null')
        if (n.deleted_at) return erro(409, 'CONFLICT', 'a nota está na lixeira: restaure antes de mexer nela')
        if (c.base_version !== n.version) return erro(409, 'CONFLICT', 'versão desatualizada')
        if (typeof c.titulo === 'string') n.titulo = c.titulo
        if (typeof c.corpo === 'string') n.corpo = c.corpo
        if ('cor' in c) n.cor = (c.cor || null) as Nota['cor']
        const fixadaAntes = n.fixada
        const arquivadaAntes = n.arquivada
        if (typeof c.fixada === 'boolean') n.fixada = c.fixada
        if (typeof c.arquivada === 'boolean') n.arquivada = c.arquivada
        // Mudar de grupo (fixar, desafixar, arquivar, desarquivar) leva ao topo do destino.
        if (n.fixada !== fixadaAntes || n.arquivada !== arquivadaAntes) noTopo(n)
        n.version += 1
        n.updated_at = agoraDasNotas()
        return json(comPosicao(n))
      }
    }
    return erro(405, 'BAD_REQUEST', 'método')
  }

  const buscar: Buscador = async (entrada, init = {}) => {
    if (foraDoAr) throw new TypeError('Failed to fetch')
    const url = new URL(entrada, 'http://127.0.0.1:51234')
    const cabecalhos = (init.headers ?? {}) as Record<string, string>
    const p: Pedido = {
      metodo: (init.method ?? 'GET').toUpperCase(),
      caminho: url.pathname,
      busca: url.searchParams,
      corpo: typeof init.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : undefined,
      auth: cabecalhos.Authorization,
    }
    pedidos.push(p)
    const forcada = interceptar?.(p)
    if (forcada) return forcada
    if (portao?.filtro(p)) await portao.aberto
    if (foraDoAr) throw new TypeError('Failed to fetch')

    if (p.caminho === '/api/sessao') {
      return p.corpo?.bilhete === 'bilhete-bom' ? json({ token }) : new Response('bilhete inválido', { status: 401 })
    }
    if (p.auth !== `Bearer ${token}`) return new Response('unauthorized', { status: 401 })

    if (p.caminho === '/api/settings') return rotaConfiguracoes(p)
    if (p.caminho === '/api/ia/chave') return rotaChave(p)
    if (p.caminho === '/api/ia/testar' && p.metodo === 'POST') return rotaTestar(p)
    if (p.caminho === '/api/aprendizado' || p.caminho === '/api/aprendizado/desfazer') return rotaAprendizado(p)
    if (p.caminho.startsWith('/api/termos')) return rotaTermos(p)
    if (p.caminho === '/api/notas/foco') return rotaFoco(p)
    if (p.caminho === '/api/notas/separar-itens') return rotaSepararItens(p)
    if (p.caminho === '/api/notas' || p.caminho.startsWith('/api/notas/')) return rotaNotas(p)
    if (p.caminho === '/api/marcadores' || p.caminho.startsWith('/api/marcadores/')) return rotaMarcadores(p)
    if (p.caminho === '/api/nuvem' || p.caminho.startsWith('/api/nuvem/')) return rotaNuvem(p)
    if (p.caminho === '/api/retencao' || p.caminho.startsWith('/api/retencao/')) return rotaRetencao(p)
    if (p.caminho === '/api/microfone' && p.metodo === 'GET') return json(microfone)
    if (p.caminho === '/api/microfone/configuracoes' && p.metodo === 'POST') {
      return abrirConfiguracoesFalha ? erro(500, 'INTERNAL', 'Não consegui abrir as configurações do Windows.') : new Response(null, { status: 204 })
    }

    const viraNota = p.caminho.match(/^\/api\/history\/([^/]+)\/virar-nota$/)
    if (viraNota) return rotaVirarNota(p, decodeURIComponent(viraNota[1] ?? ''))
    const rota = p.caminho.match(/^\/api\/history(?:\/([^/]+))?(\/restore)?$/)
    if (!rota) return erro(404, 'NOT_FOUND', 'rota')
    const [, id, restaurar] = rota

    if (!id) {
      const lixeira = p.busca.get('trash') === 'true'
      // Sem diferenciar maiúscula nem acento, como o Agent (storage.dobrar; N6).
      const q = dobrar(p.busca.get('q') ?? '')
      const lista = ordenar(
        itens
          .filter((i) => Boolean(i.deleted_at) === lixeira)
          .filter((i) => !virouNota.has(i.id))
          .filter((i) => !q || dobrar(i.final_text).includes(q) || dobrar(i.raw_text).includes(q)),
        lixeira ? 'deleted_at' : 'created_at',
      )
      // O cursor aqui é só o deslocamento; o da API é opaco, e o painel
      // não olha dentro dele.
      const inicio = Number(p.busca.get('cursor') ?? 0)
      const pagina = lista.slice(inicio, inicio + porPagina).map((i) => ({ ...i }))
      const fim = inicio + porPagina
      return json(fim < lista.length ? { items: pagina, next_cursor: String(fim) } : { items: pagina })
    }

    const item = itens.find((i) => i.id === decodeURIComponent(id))
    if (!item) return erro(404, 'NOT_FOUND', 'registro não encontrado')
    if (restaurar && p.metodo === 'POST') {
      delete item.deleted_at
      item.version += 1
      return new Response(null, { status: 204 })
    }
    switch (p.metodo) {
      case 'GET':
        return json(item)
      case 'PATCH':
        if (p.corpo?.base_version !== item.version) return erro(409, 'CONFLICT', 'versão desatualizada')
        // A escolha do selo (F7c), como o Agent: só com selo; "trocar" põe
        // o texto do selo; as duas o tiram e sobem a versão.
        if (p.corpo?.resolver_conflito !== undefined) {
          if (p.corpo.final_text !== undefined) return erro(400, 'BAD_REQUEST', 'o texto e a escolha do selo não vão juntos')
          if (!item.versao_em_conflito) return erro(409, 'CONFLICT', 'versão desatualizada')
          if (p.corpo.resolver_conflito === 'trocar') item.final_text = item.versao_em_conflito.final_text
          delete item.versao_em_conflito
          item.version += 1
          return json({ ...item })
        }
        item.final_text = String(p.corpo?.final_text)
        item.version += 1
        // Como o Agent: o item e, junto, as sugestões, que ele lembra por
        // ditado e versão. Salvar de novo as substitui.
        lembradas.set(item.id, { versao: item.version, pares: sugestoesDoSalvar })
        return json({ ...item, sugestoes: sugestoesDoSalvar })
      case 'DELETE':
        item.deleted_at = new Date().toISOString()
        item.version += 1
        return new Response(null, { status: 204 })
    }
    return erro(405, 'BAD_REQUEST', 'método')
  }

  return {
    buscar,
    pedidos,
    itens,
    // notas e marcadores são o que o banco das notas tem agora.
    notas: bancoDeNotas,
    // virouNota: ditado -> nota, dos ditados escondidos do histórico.
    virouNota,
    // foco: o que o Agent gravaria em virou_nota agora (o id da nota, ou
    // "nova:<token>"); null, o ditado vai ao histórico.
    foco: () => foco,
    // ditarNaCaixa simula um ditado feito com o aviso da caixa ativo: o ditado
    // sai do histórico (fica escondido sob o token) até a nota nascer.
    ditarNaCaixa(token: string, id: string) {
      ditadosDaCaixa.set(token, [...(ditadosDaCaixa.get(token) ?? []), id])
      virouNota.set(id, `nova:${token}`)
    },
    marcadores: bancoDeMarcadores,
    // notaPorFora simula outra aba mexendo na nota: a versão sobe, a data
    // também.
    notaPorFora(id: string, parcial: Partial<Pick<Nota, 'titulo' | 'corpo' | 'cor' | 'fixada' | 'arquivada' | 'conflito'>>) {
      const n = bancoDeNotas.find((x) => x.id === id)
      if (!n) throw new Error(id)
      Object.assign(n, parcial)
      n.version += 1
      n.updated_at = agoraDasNotas()
    },
    // notaParaALixeiraPorFora simula outra aba movendo a nota para a lixeira.
    notaParaALixeiraPorFora(id: string) {
      const n = bancoDeNotas.find((x) => x.id === id)
      if (!n) throw new Error(id)
      n.deleted_at = agoraDasNotas()
      n.version += 1
    },
    config,
    chaves,
    // aprendidas é o que o dicionário do painel tem agora.
    aprendidas,
    // termos é o que a pessoa adicionou, como o Agent tem agora.
    termos,
    // recusarTermo: o POST dessa rota de termos responde o status e a frase
    // dados, sem gravar. Sem argumentos, limpa as recusas.
    recusarTermo(rota?: string, status = 409, mensagem = '') {
      if (rota === undefined) recusasDeTermos.clear()
      else recusasDeTermos.set(rota, { status, mensagem })
    },
    // sugerirNoSalvar: o que os próximos PATCH devolvem em `sugestoes`.
    sugerirNoSalvar(sugestoes: Sugestao[]) {
      sugestoesDoSalvar = sugestoes
    },
    // recusarAprender: o POST /api/aprendizado desse "de" (ou de qualquer,
    // com '*') responde o status e a frase dados, sem gravar. Sem
    // argumentos, limpa as recusas.
    recusarAprender(de?: string, status = 409, mensagem = '') {
      if (de === undefined) recusasDoAprender.clear()
      else recusasDoAprender.set(de, { status, mensagem })
    },
    // nuvemPorFora troca o estado da nuvem como o Agent o veria: a aprovação
    // que voltou ao PC, a revogação no app, o pedido que venceu.
    nuvemPorFora(e: EstadoNuvem) {
      nuvem = { ...e }
    },
    // recusarEnvio: o próximo "Enviar o histórico" volta com a frase (a
    // cópia que falhou); null deixa enviar.
    recusarEnvio(r: { status: number; mensagem: string } | null) {
      recusaDoEnvio = r
    },
    // retencaoPorFora troca o que o Agent diz dos 6 meses, como o passar do
    // tempo ou o freio o fariam: ditados que venceram, a regra pausada.
    retencaoPorFora(parcial: Partial<EstadoRetencao>) {
      retencao = { ...retencao, ...parcial }
    },
    // retencaoAgora é o estado dos 6 meses como o Agent o tem.
    retencaoAgora(): EstadoRetencao {
      return retencao
    },
    // recusarRetencao: o próximo OK volta com a frase (a cópia que falhou); null
    // deixa ligar.
    recusarRetencao(r: { status: number; mensagem: string } | null) {
      recusaDaRetencao = r
    },
    // ligarEmParte: o próximo OK liga a regra e apaga só apagados (um lote
    // seguinte caiu): responde 200 com incompleto, ou 500 ('falha').
    ligarEmParte(r: { apagados: number; resposta: 'incompleto' | 'falha' } | null) {
      emParte = r
    },
    // agentSemRetencao: o Agent de antes da F10 (as rotas dão 404).
    agentSemRetencao(sim = true) {
      agentVelho = sim
    },
    // microfoneAgora muda o que o Agent diz do microfone (o Windows bloqueou, ou
    // a pessoa ligou a chave): o painel só vê na próxima consulta.
    microfoneAgora(bloqueado: boolean, motivo = '') {
      microfone = { bloqueado, motivo }
    },
    // abrirConfiguracoesFalha: o POST que abre a tela do Windows dá erro.
    abrirConfiguracoesFalha(sim = true) {
      abrirConfiguracoesFalha = sim
    },
    // nuvemAgora é o estado da nuvem como o Agent o tem.
    nuvemAgora(): EstadoNuvem {
      return nuvem
    },
    // chegouDaNuvem simula um pull que trouxe estes ditados (o do celular, por
    // exemplo): eles entram no histórico e o contador do sync sobe, uma vez.
    chegouDaNuvem(...novos: Ditado[]) {
      itens.push(...novos.map((i) => ({ ...i })))
      contador += 1
    },
    // mudancasPorFora sobe o contador sem trazer ditado novo: o pull que
    // trouxe uma edição feita no celular (use com alterarPorFora) ou a lápide.
    mudancasPorFora() {
      contador += 1
    },
    // contadorAgora é o contador de mudanças como o Agent o tem.
    contadorAgora(): number {
      return contador
    },
    // reiniciarContador simula o Agent que reiniciou: o contador volta a 0.
    reiniciarContador() {
      contador = 0
    },
    // agentSemContador: o Agent de antes da S (a rota dá 404).
    agentSemContador(sim = true) {
      agentSemContador = sim
    },
    // seloPorFora põe um selo num ditado, como um pull que trouxe o
    // conflito (a versão sobe).
    seloPorFora(id: string, outra: string, quando = new Date(2026, 9, 6, 10, 0).toISOString()) {
      const item = itens.find((i) => i.id === id)
      if (!item) throw new Error(id)
      item.versao_em_conflito = { final_text: outra, device_id: null, conflict_at: quando }
      item.version += 1
    },
    // configPorFora simula outra aba salvando as configurações.
    configPorFora(parcial: Partial<Configuracoes>) {
      Object.assign(config, parcial)
      config.version += 1
    },
    // autostartPorFora simula a bandeja ou o Gerenciador de Tarefas
    // mudando o registro: o autostart muda, a version NÃO sobe (ela é do
    // banco, e o autostart não mora lá).
    autostartPorFora(ligado: boolean) {
      config.autostart = ligado
    },
    // testeDevolve troca o que o "provedor" responde ao Testar conexão.
    testeDevolve(r: ResultadoTeste) {
      resultadoDoTeste = r
    },
    // iaDaListaDevolve troca o que a IA "devolve" na separação em itens (N5).
    iaDaListaDevolve(f: ((trecho: string) => string[] | null) | null) {
      iaDaLista = f
    },
    interceptar(f: ((p: Pedido) => Response | undefined) | null) {
      interceptar = f
    },
    // pausar segura os pedidos que casam com o filtro; a função devolvida
    // os solta.
    pausar(filtro: (p: Pedido) => boolean): () => void {
      let abrir = () => {}
      const aberto = new Promise<void>((r) => {
        abrir = r
      })
      portao = { filtro, aberto }
      return () => {
        portao = null
        abrir()
      }
    },
    derrubar(sim: boolean) {
      foraDoAr = sim
    },
    // alterarPorFora simula outra aba salvando o mesmo ditado.
    alterarPorFora(id: string, texto: string) {
      const item = itens.find((i) => i.id === id)
      if (!item) throw new Error(id)
      item.final_text = texto
      item.version += 1
    },
    // lixeiraPorFora simula outra aba movendo para a lixeira (e, com
    // desfazer, trazendo de volta): a versão sobe, o texto não muda.
    lixeiraPorFora(id: string, desfazer = false) {
      const item = itens.find((i) => i.id === id)
      if (!item) throw new Error(id)
      item.deleted_at = new Date().toISOString()
      item.version += 1
      if (desfazer) {
        delete item.deleted_at
        item.version += 1
      }
    },
  }
}
