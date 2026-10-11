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
// Os 6 meses no PC e as cópias do banco (ADR-019, F10d e F10e;
// internal/agent/localapi/retencao.go): GET /api/retencao é só números e
// booleanos; o OK (POST /api/retencao/ligar) leva a contagem que a tela
// mostrou e o Agent recusa com o número novo (409, ao lado do envelope) quando
// ela difere, e com "Não há nada a apagar agora." quando não há nada; o
// Desligar só tira o OK; o botão das cópias deixa as que o teste manda
// proteger (a do envio do histórico que não terminou). Um Agent de antes da
// F10 responde 404 sem frase (agentSemRetencao).

import type { Aprendida, Buscador, Configuracoes, Ditado, ListaDeTermos, Provedor, ResultadoTeste, Sugestao, TermoAdicionado } from '../api'
import type { EstadoNuvem, EstadoRetencao } from '../clienteLocal'

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

const editaveis = ['hold_threshold_ms', 'beep_enabled', 'microphone_always_ready', 'text_mode', 'ai_provider', 'ai_model', 'autostart'] as const
const provedores: readonly string[] = ['openai', 'anthropic']

export interface OpcoesDoServidor {
  token?: string
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
  function rotaNuvem(p: Pedido): Response {
    if (p.caminho === '/api/nuvem' && p.metodo === 'GET') return json(nuvem)
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
    if (p.caminho === '/api/nuvem' || p.caminho.startsWith('/api/nuvem/')) return rotaNuvem(p)
    if (p.caminho === '/api/retencao' || p.caminho.startsWith('/api/retencao/')) return rotaRetencao(p)

    const rota = p.caminho.match(/^\/api\/history(?:\/([^/]+))?(\/restore)?$/)
    if (!rota) return erro(404, 'NOT_FOUND', 'rota')
    const [, id, restaurar] = rota

    if (!id) {
      const lixeira = p.busca.get('trash') === 'true'
      const q = (p.busca.get('q') ?? '').toLowerCase()
      const lista = ordenar(
        itens
          .filter((i) => Boolean(i.deleted_at) === lixeira)
          .filter((i) => !q || i.final_text.toLowerCase().includes(q) || i.raw_text.toLowerCase().includes(q)),
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
    // nuvemAgora é o estado da nuvem como o Agent o tem.
    nuvemAgora(): EstadoNuvem {
      return nuvem
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
