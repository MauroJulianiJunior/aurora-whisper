// O contrato do ditado pelo celular na conta, preso ao 06 (docs/ai-spec/06-api-contract.md):
// as rotas /dictation, /dictation/settings, /dictation/consent e /ai-keys que a C3a e a
// C3b fixaram. O app da C6b chegou a desenhar OUTRO formato, porque o servidor ainda não
// existia; este teste é o que impede a repetição. O 06 é a descrição do servidor de
// verdade (internal/server/ditado_celular.go e chaves_ia.go, executados pela suíte
// internal/server/chavestest); o servidor de mentira dos testes do app
// (testes/servidorFalsoNuvem.ts) e o cliente (cliente.ts) têm de falar o que o 06 mostra:
// os mesmos campos, nos mesmos tipos, e os mesmos status para cada código.
//
// A rota de ditar, POST /api/v1/dictations, tem a sua parte no fim deste arquivo: a C5b a
// fixou depois, e o servidor de mentira e o cliente passaram a segui-la (a ordem das
// conferências, o `aviso`, o reenvio que espera, os tetos, o id v7, o 500 que só regrava, o
// 400 e os status da tabela de códigos).
//
// O 06 é lido como texto (?raw): nenhum arquivo de fora de web/ entra em build algum, e o
// teste não usa o módulo fs do Node.

import { describe, expect, it } from 'vitest'
import doc from '../../../docs/ai-spec/06-api-contract.md?raw'
import { ErroDaApi } from '../api'
import {
  type FalhaDoDitado,
  type OpcoesDaNuvem,
  ditadoComOPolish,
  falaMinimaNoServidorFalso,
  itemNuvem,
  motivosDoPolishFalsos,
  servidorFalsoNuvem,
  statusDosCodigosDaRota,
  tetoDaDuracaoNoServidorFalso,
  tetoDoCorpoNoServidorFalso,
} from '../testes/servidorFalsoNuvem'
import { type ClienteNuvem, type PedidoDeDitado, criarClienteNuvem } from './cliente'
import { type ErroDoDitado, erroDoEnvio, fraseDoAviso } from './ditar/erros'
import { falaMinimaMs, tetoMs, uuidV7 } from './ditar/medidas'

// A seção `### <titulo>` do 06, até a próxima seção.
function secao(titulo: string): string {
  const inicio = doc.indexOf(`### ${titulo}\n`)
  if (inicio < 0) throw new Error(`o 06 perdeu a seção "${titulo}"`)
  const resto = doc.slice(inicio + 1)
  const fim = resto.search(/\n#{2,3} /)
  return fim < 0 ? resto : resto.slice(0, fim)
}

// O texto da seção numa linha só: o 06 quebra as linhas em ~100 colunas, e uma frase pode
// cair no meio.
const texto = (titulo: string): string => secao(titulo).replace(/\s+/g, ' ')

// A tabela "Os códigos" do 06: | `CODIGO` | status | quando |.
function statusDaTabelaDoSeis(): Map<string, number> {
  const tabela = new Map<string, number>()
  for (const m of secao('Os códigos (a lista fechada do ditado no celular, ADR-021)').matchAll(/^\| `([A-Z_]+)` \| (\d{3})/gm)) tabela.set(m[1]!, Number(m[2]))
  return tabela
}

// O primeiro bloco ```json da seção, já lido.
function exemploJson(titulo: string): unknown {
  const m = /```json\n([\s\S]*?)\n```/.exec(secao(titulo))
  if (!m) throw new Error(`a seção "${titulo}" do 06 perdeu o exemplo em JSON`)
  return JSON.parse(m[1]!)
}

// forma descreve um JSON pelos campos e pelos tipos, sem os valores. Uma lista tem a
// forma do primeiro elemento, e todos têm de ter a mesma.
function forma(v: unknown): unknown {
  if (v === null) return 'null'
  if (Array.isArray(v)) {
    const formas = v.map(forma)
    for (const f of formas) expect(f).toEqual(formas[0])
    return [formas[0]]
  }
  if (typeof v === 'object') return Object.fromEntries(Object.entries(v!).map(([k, x]) => [k, forma(x)]))
  return typeof v
}

// A conta do exemplo do 06: o aceite com o Polish da Anthropic, as preferências com o
// modelo de verdade, e uma chave de cada tipo (a que vale, a que falta e a que se cola
// de novo).
function montar() {
  const srv = servidorFalsoNuvem([itemNuvem({ id: 'd1', created_at: new Date(2026, 9, 9, 14).toISOString(), final_text: 'Um ditado.' })], {
    ditado: {
      aceite: { versao: 1, transcricao: 'openai', polish: 'anthropic', aceito_em: '2026-10-11T12:00:00.000Z' },
      chaves: { openai: 'sk-teste-de-mentira-WXYZ' },
      colarDeNovo: ['anthropic'],
    },
  })
  return { srv, cliente: criarClienteNuvem(srv.buscar) }
}

const json = async (r: Response) => (await r.json()) as unknown

describe('o servidor de mentira fala o que o 06 mostra', () => {
  it('GET /dictation: {aviso, aceite, preferencias, chaves} com os campos do exemplo', async () => {
    const { srv } = montar()
    const r = await srv.buscar('/api/v1/dictation', { credentials: 'same-origin' })
    expect(r.status).toBe(200)
    expect(forma(await json(r))).toEqual(forma(exemploJson('GET `/dictation`')))
  })

  it('o PUT das preferências e o POST do aceite devolvem o mesmo estado do GET', async () => {
    const { srv } = montar()
    const estado = forma(exemploJson('GET `/dictation`'))
    const put = await srv.buscar('/api/v1/dictation/settings', {
      method: 'PUT',
      body: JSON.stringify({ transcricao: { provedor: 'openai', modelo: '' }, dica: false, polish: { ligado: false, provedor: 'openai', modelo: '' } }),
    })
    expect(put.status).toBe(200)
    expect(forma(await json(put))).toEqual(estado)
    const post = await srv.buscar('/api/v1/dictation/consent', { method: 'POST', body: JSON.stringify({ versao: 1, transcricao: 'openai', polish: 'anthropic' }) })
    expect(post.status).toBe(200)
    expect(forma(await json(post))).toEqual(estado)
  })

  it('o aceite sem Polish tem polish: null (o exemplo mostra o Polish aceito; a seção diz que sem ele é null)', async () => {
    expect(secao('GET `/dictation`')).toContain('`polish` é `null` quando')
    const { srv } = montar()
    await srv.buscar('/api/v1/dictation/consent', { method: 'POST', body: JSON.stringify({ versao: 1, transcricao: 'groq' }) })
    const r = await srv.buscar('/api/v1/dictation')
    expect(((await json(r)) as { aceite: { polish: unknown } }).aceite.polish).toBeNull()
  })
})

describe('o cliente manda o que o 06 pede', () => {
  it('as preferências: o corpo do PUT tem a forma do exemplo (transcricao, dica e polish)', async () => {
    const { srv, cliente } = montar()
    await cliente.salvarPreferencias({
      provedor: 'openai',
      modelo: '',
      dica: false,
      polish: true,
      polish_provedor: 'anthropic',
      polish_modelo: 'claude-haiku-4-5',
    })
    const put = srv.pedidos.find((p) => p.metodo === 'PUT')!
    expect(put.caminho).toBe('/api/v1/dictation/settings')
    expect(forma(put.corpo)).toEqual(forma(exemploJson('PUT `/dictation/settings`')))
  })

  it('o aceite: {versao, transcricao, polish}, com a versão como inteiro', async () => {
    const { srv, cliente } = montar()
    await cliente.aceitarAviso(1, 'openai', 'anthropic')
    const post = srv.pedidos.find((p) => p.metodo === 'POST')!
    expect(post.caminho).toBe('/api/v1/dictation/consent')
    const exemplo = /Corpo `(\{[^`]*\})`: `versao` e/.exec(secao('POST `/dictation/consent`'))
    if (!exemplo) throw new Error('o 06 perdeu o exemplo do corpo do POST /dictation/consent')
    expect(forma(post.corpo)).toEqual(forma(JSON.parse(exemplo[1]!)))
  })

  it('a chave: PUT {chave} em /ai-keys/{provedor}, e a resposta traz provedor, configurada e final', async () => {
    const { srv, cliente } = montar()
    await cliente.guardarChave('openai', 'sk-teste-do-celular-ABCD')
    const put = srv.pedidos.find((p) => p.metodo === 'PUT')!
    expect(put.caminho).toBe('/api/v1/ai-keys/openai')
    expect(Object.keys(put.corpo!)).toEqual(['chave'])
    // O 06: `200` com { "provedor": "openai", "configurada": true, "final": "WXYZ" }.
    expect(secao('PUT `/ai-keys/{provedor}`')).toContain('{ "provedor": "openai", "configurada": true, "final": "WXYZ" }')
    const r = await srv.buscar('/api/v1/ai-keys/openai', { method: 'PUT', body: JSON.stringify({ chave: 'sk-teste-do-celular-WXYZ' }) })
    expect(await json(r)).toEqual({ provedor: 'openai', configurada: true, final: 'WXYZ' })
  })

  it('os modelos: GET /ai-keys/{provedor}/models devolve { provedor, modelos }', async () => {
    const { srv } = montar()
    expect(secao('GET `/ai-keys/{provedor}/models`')).toContain('{ "provedor": "openai", "modelos": ["...", "..."] }')
    const r = await srv.buscar('/api/v1/ai-keys/openai/models')
    const corpo = (await json(r)) as { provedor: string; modelos: unknown }
    expect(corpo.provedor).toBe('openai')
    expect(Array.isArray(corpo.modelos)).toBe(true)
  })
})

describe('os códigos de erro: o status de cada um é o do 06', () => {
  const tabela = statusDaTabelaDoSeis()

  it('a tabela do 06 tem os códigos que o app trata nas rotas da conta', () => {
    for (const codigo of ['SEM_ACEITE', 'SEM_CHAVE', 'LIMITE', 'PROVEDOR_RECUSOU_CHAVE', 'PROVEDOR_LIMITE', 'PROVEDOR_FORA', 'AVISO_DESATUALIZADO']) {
      expect(tabela.has(codigo), codigo).toBe(true)
    }
  })

  it('o servidor de mentira responde cada um com o status do 06', async () => {
    const { srv } = montar()
    const codigoEStatus = async (r: Response) => {
      const corpo = (await json(r)) as { error: { code: string } }
      return [corpo.error.code, r.status] as const
    }
    // SEM_ACEITE: um provedor fora do aceite.
    const semAceite = await codigoEStatus(await srv.buscar('/api/v1/ai-keys/groq', { method: 'PUT', body: JSON.stringify({ chave: 'gsk-teste-1234567' }) }))
    // SEM_CHAVE: a lista de modelos de quem não tem chave que valha (a Anthropic é colar_de_novo).
    const semChave = await codigoEStatus(await srv.buscar('/api/v1/ai-keys/anthropic/models'))
    // PROVEDOR_RECUSOU_CHAVE: o provedor recusa a chave.
    const recusada = await codigoEStatus(await srv.buscar('/api/v1/ai-keys/openai', { method: 'PUT', body: JSON.stringify({ chave: 'recusada-pelo-provedor' }) }))
    // AVISO_DESATUALIZADO: o app mostrou outra versão do aviso.
    const velho = await codigoEStatus(await srv.buscar('/api/v1/dictation/consent', { method: 'POST', body: JSON.stringify({ versao: 7, transcricao: 'openai' }) }))
    for (const [codigo, status] of [semAceite, semChave, recusada, velho]) expect(tabela.get(codigo), codigo).toBe(status)
  })
})

// ---------------------------------------------------------------------------------------------
// A rota de ditar, POST /api/v1/dictations (06, "Ditar pelo celular", C5b). O servidor de
// mentira a segue na ORDEM do 06, e o cliente trata cada resposta como a ADR-021 manda.
// ---------------------------------------------------------------------------------------------

const idDoDitado = '0192f0a1-7c3e-7d4a-9b2f-000000000001'
// Outro id v7 por número, para os testes que mandam mais de um ditado.
const idN = (n: number) => `0192f0a1-7c3e-7d4a-9b2f-${n.toString(16).padStart(12, '0')}`
const webm = (bytes = 8) => new Blob([new Uint8Array(bytes)], { type: 'audio/webm;codecs=opus' })
const umDitado = (extra: Partial<PedidoDeDitado> = {}): PedidoDeDitado => ({ id: idDoDitado, audio: webm(), duracaoMs: 4000, falaMs: 3000, ...extra })

type NuvemDeMentira = ReturnType<typeof servidorFalsoNuvem>

// A conta pronta (aceite da OpenAI e da Anthropic, as três chaves, o Polish desligado), sem
// itens no histórico. Com `ditado: ditadoComOPolish`, o Polish ligado.
function montarRota(opcoes: OpcoesDaNuvem = {}) {
  const srv = servidorFalsoNuvem([], opcoes)
  return { srv, cliente: criarClienteNuvem(srv.buscar) }
}

// falhou manda o ditado e devolve o erro da API (o teste falha se o envio deu certo).
async function falhou(cliente: ClienteNuvem, pedido: PedidoDeDitado = umDitado()): Promise<ErroDaApi> {
  const e = await cliente.ditar(pedido).catch((x: unknown) => x)
  expect(e).toBeInstanceOf(ErroDaApi)
  return e as ErroDaApi
}

// cru manda o POST direto ao servidor de mentira, com os cabeçalhos como o teste quiser:
// `null` tira o cabeçalho; sem o campo, vale o de um ditado bom.
interface Cru {
  id?: string | null
  tipo?: string | null
  duracao?: string | null
  fala?: string | null
  audio?: Blob
}
function cru(srv: NuvemDeMentira, c: Cru = {}): Promise<Response> {
  const cabecalhos: Record<string, string> = {}
  const poe = (nome: string, valor: string | null | undefined, padrao: string) => {
    const v = valor === undefined ? padrao : valor
    if (v !== null) cabecalhos[nome] = v
  }
  poe('Ditado-Id', c.id, idDoDitado)
  poe('Content-Type', c.tipo, 'audio/webm')
  poe('Ditado-Duracao-Ms', c.duracao, '4000')
  poe('Ditado-Fala-Ms', c.fala, '3000')
  return srv.buscar('/api/v1/dictations', { method: 'POST', credentials: 'same-origin', headers: cabecalhos, body: c.audio ?? webm() })
}
// contem confere que o 06 ainda diz isso (o texto vem numa linha só, ver `texto`): se o 06
// mudar, o teste diz qual frase foi, em vez de despejar a seção inteira.
const contem = (secaoDoSeis: string, trecho: string) => expect(secaoDoSeis.includes(trecho), `o 06 perdeu: ${trecho}`).toBe(true)

// recusa: o status e o error.code de uma resposta de erro.
const recusa = async (r: Response) => [r.status, ((await r.json()) as { error?: { code?: string } }).error?.code] as const

describe('a rota POST /dictations: o status de cada código', () => {
  const tabela = statusDaTabelaDoSeis()

  it('a tabela do 06 e o servidor de mentira dizem o mesmo status para cada código da rota', () => {
    for (const [codigo, st] of Object.entries(statusDosCodigosDaRota)) expect(tabela.get(codigo), codigo).toBe(st)
    // Os do contrato geral que a rota também usa, no parágrafo depois da tabela.
    const geral = texto('Os códigos (a lista fechada do ditado no celular, ADR-021)')
    contem(geral, '`UNAUTHORIZED` (`401`)')
    contem(geral, '`BAD_REQUEST` (`400`:')
    contem(geral, '`INTERNAL` (`500`:')
  })

  // Cada código, produzido pelo servidor de mentira onde o de verdade o produz, chega ao
  // cliente com o status do 06 e vira o que a ADR-021 manda ("As frases de erro"): o tipo, se
  // o áudio fica na memória para "Tentar de novo" (retem) e para onde a pessoa vai.
  const espere = (seg: number): Partial<ErroDoDitado> => ({ tipo: 'espere', retem: true, frase: `Espere um pouco. Tente de novo em ${seg} s.` })
  const comAFalha = (falha: FalhaDoDitado) => () => {
    const m = montarRota()
    m.srv.falharNoProximoDitado(falha)
    return falhou(m.cliente)
  }
  const cenarios: { codigo: string; rodar: () => Promise<ErroDaApi>; erro: Partial<ErroDoDitado> }[] = [
    { codigo: 'SEM_ACEITE', rodar: () => falhou(montarRota({ ditado: {} }).cliente), erro: { tipo: 'sem-aceite', retem: true, vai: 'aceite' } },
    {
      codigo: 'SEM_CHAVE',
      rodar: () => falhou(montarRota({ ditado: { aceite: { versao: 1, transcricao: 'openai', polish: null, aceito_em: '2026-10-11T12:00:00.000Z' } } }).cliente),
      erro: { tipo: 'sem-chave', retem: true, vai: 'configuracoes' },
    },
    { codigo: 'LIMITE', rodar: comAFalha('LIMITE'), erro: espere(12) },
    { codigo: 'UM_POR_VEZ', rodar: comAFalha('UM_POR_VEZ'), erro: espere(5) },
    { codigo: 'OCUPADO', rodar: comAFalha('OCUPADO'), erro: espere(5) },
    {
      codigo: 'PAYLOAD_TOO_LARGE',
      rodar: () => falhou(montarRota().cliente, umDitado({ duracaoMs: tetoDaDuracaoNoServidorFalso + 1 })),
      erro: { tipo: 'longo', retem: false, frase: 'Ficou longo demais.' },
    },
    {
      codigo: 'PAYLOAD_TOO_LARGE',
      rodar: () => falhou(montarRota().cliente, umDitado({ audio: webm(tetoDoCorpoNoServidorFalso + 1) })),
      erro: { tipo: 'longo', retem: false, frase: 'Ficou longo demais.' },
    },
    { codigo: 'FORMATO', rodar: () => falhou(montarRota().cliente, umDitado({ audio: new Blob([], { type: 'audio/webm' }) })), erro: { tipo: 'formato', retem: false } },
    // A fala medida pelo app abaixo do mínimo, e o provedor que não devolveu texto.
    { codigo: 'SEM_FALA', rodar: () => falhou(montarRota().cliente, umDitado({ falaMs: falaMinimaNoServidorFalso - 1 })), erro: { tipo: 'sem-fala', retem: false, frase: 'Não ouvi nada.' } },
    { codigo: 'SEM_FALA', rodar: comAFalha('provedor_sem_texto'), erro: { tipo: 'sem-fala', retem: false, frase: 'Não ouvi nada.' } },
    {
      codigo: 'APAGADO',
      rodar: async () => {
        const { cliente } = montarRota()
        await cliente.ditar(umDitado())
        await cliente.moverParaLixeira(idDoDitado)
        await cliente.excluirDeVez(idDoDitado)
        return falhou(cliente)
      },
      erro: { tipo: 'apagado', retem: false, frase: 'Esse ditado foi apagado.' },
    },
    { codigo: 'PROVEDOR_RECUSOU_CHAVE', rodar: comAFalha('PROVEDOR_RECUSOU_CHAVE'), erro: { tipo: 'provedor-chave', retem: true, vai: 'configuracoes' } },
    { codigo: 'PROVEDOR_LIMITE', rodar: comAFalha('PROVEDOR_LIMITE'), erro: { tipo: 'provedor-fora', retem: true, frase: 'O provedor não respondeu agora.' } },
    { codigo: 'PROVEDOR_FORA', rodar: comAFalha('PROVEDOR_FORA'), erro: { tipo: 'provedor-fora', retem: true, frase: 'O provedor não respondeu agora.' } },
  ]

  it.each(cenarios)('$codigo: o status é o da tabela do 06, e o cliente trata como a ADR manda', async ({ codigo, rodar, erro }) => {
    const e = await rodar()
    expect([e.codigo, e.status]).toEqual([codigo, tabela.get(codigo)])
    expect(erroDoEnvio(e)).toMatchObject(erro)
  })

  it('os cenários cobrem todos os códigos da rota', () => {
    expect(new Set(cenarios.map((c) => c.codigo))).toEqual(new Set(Object.keys(statusDosCodigosDaRota)))
  })

  it('o Retry-After da vaga e dos limites chega ao cliente (esperaSeg)', async () => {
    const { srv, cliente } = montarRota()
    srv.falharNoProximoDitado('LIMITE')
    expect((await falhou(cliente)).esperaSeg).toBe(12)
    srv.falharNoProximoDitado('OCUPADO')
    expect((await falhou(cliente)).esperaSeg).toBe(5)
  })

  it('a falha que vale só para o próximo ditado: o seguinte, com o mesmo id, passa', async () => {
    const { srv, cliente } = montarRota()
    srv.falharNoProximoDitado('UM_POR_VEZ')
    await falhou(cliente)
    expect((await cliente.ditar(umDitado())).ditado.id).toBe(idDoDitado)
    // A recusa da vaga vem antes do provedor: nada foi pago.
    expect(srv.ditados.map((d) => [d.status, d.chamouOProvedor])).toEqual([
      [429, false],
      [201, true],
    ])
  })
})

describe('a rota POST /dictations: o `aviso` é um resolver.MotivoFallback', () => {
  const rota = texto('POST `/dictations`')

  // A lista fechada do 06: "(`resolver.MotivoFallback`: `prazo`, `sem_chave`, ...)".
  function motivosDoSeis(): string[] {
    const trecho = /resolver\.MotivoFallback`: ([^)]*)\)/.exec(rota)
    if (!trecho) throw new Error('o 06 perdeu a lista de motivos do `aviso` (resolver.MotivoFallback)')
    return [...trecho[1]!.matchAll(/`([a-z_]+)`/g)].map((m) => m[1]!)
  }

  it('a lista do servidor de mentira é a do 06', () => {
    const lista = motivosDoSeis()
    expect(lista.length).toBeGreaterThan(15)
    expect([...motivosDoPolishFalsos].sort()).toEqual([...lista].sort())
  })

  it('o app tem uma frase própria para cada motivo da lista (nenhum cai no "O Polish não agiu." solto)', () => {
    for (const motivo of motivosDoSeis()) expect(fraseDoAviso(motivo), motivo).toMatch(/^O Polish não agiu: .+\.$/)
  })

  it('o motivo que o app não conhece, e o texto livre, continuam sem frase solta', () => {
    expect(fraseDoAviso('um_motivo_novo')).toBe('O Polish não agiu.')
    expect(fraseDoAviso('texto livre do provedor')).toBe('O Polish não agiu.')
  })

  it('o aviso vem no ditado novo (201) e o cliente o separa do ditado', async () => {
    const { srv, cliente } = montarRota({ ditado: ditadoComOPolish })
    srv.aoDitar({ texto: 'Ligar para o cliente.', aviso: 'modelo_inexistente' })
    const g = await cliente.ditar(umDitado())
    expect(g.aviso).toBe('modelo_inexistente')
    expect('aviso' in g.ditado).toBe(false)
    expect(srv.ditados[0]?.status).toBe(201)
  })

  it('o reenvio de um ditado já gravado é 200, sem aviso', async () => {
    contem(rota, 'O reenvio** de um id que já está gravado é **`200`** com o ditado, sem `aviso`')
    const { srv, cliente } = montarRota({ ditado: ditadoComOPolish })
    srv.aoDitar({ texto: 'Ligar para o cliente.', aviso: 'prazo' })
    expect((await cliente.ditar(umDitado())).aviso).toBe('prazo')
    const reenvio = await cliente.ditar(umDitado())
    expect(reenvio.aviso).toBeUndefined()
    expect(srv.ditados.map((d) => [d.status, d.chamouOProvedor])).toEqual([
      [201, true],
      [200, false],
    ])
  })
})

describe('a rota POST /dictations: a idempotência e a memória', () => {
  const rota = texto('POST `/dictations`')

  it('o reenvio que esperou o mesmo id em andamento recebe 200 com o ditado e o aviso do primeiro', async () => {
    contem(rota, 'o reenvio que esperou o mesmo id em andamento é `200` com o `aviso` do primeiro')
    const { srv, cliente } = montarRota({ ditado: ditadoComOPolish })
    srv.aoDitar({ texto: 'Ligar para o cliente.', aviso: 'prazo' })
    const porta = srv.segurarOProximoDitado()
    const primeiro = cliente.ditar(umDitado())
    const segundo = cliente.ditar(umDitado())
    porta.liberar()
    const [a, b] = await Promise.all([primeiro, segundo])
    expect(a.aviso).toBe('prazo')
    expect(b.aviso).toBe('prazo')
    expect(b.ditado).toEqual(a.ditado)
    expect(srv.ditados.map((d) => [d.status, d.chamouOProvedor])).toEqual([
      [201, true],
      [200, false],
    ])
    expect(srv.chamadasAoProvedor).toBe(1)
    expect(srv.itens).toHaveLength(1)
  })

  it('quem espera um primeiro que foi recusado recebe a mesma recusa, sem chamar o provedor; depois o id passa', async () => {
    const { srv, cliente } = montarRota()
    srv.falharNoProximoDitado('PROVEDOR_FORA')
    const porta = srv.segurarOProximoDitado()
    const primeiro = falhou(cliente)
    const segundo = falhou(cliente)
    porta.liberar()
    const [a, b] = await Promise.all([primeiro, segundo])
    expect([a.status, a.codigo]).toEqual([502, 'PROVEDOR_FORA'])
    expect([b.status, b.codigo]).toEqual([502, 'PROVEDOR_FORA'])
    expect(srv.ditados.map((d) => [d.status, d.chamouOProvedor])).toEqual([
      [502, true],
      [502, false],
    ])
    // Nenhum ditado foi criado, e o "Tentar de novo" com o mesmo id processa.
    expect(srv.itens).toHaveLength(0)
    await cliente.ditar(umDitado())
    expect(srv.chamadasAoProvedor).toBe(2)
  })

  it('o primeiro que parou antes do provedor (o corpo não chegou): quem esperava tenta com o próprio corpo', async () => {
    const { srv, cliente } = montarRota()
    srv.falharNoProximoDitado('corpo_incompleto')
    const primeiro = falhou(cliente)
    const segundo = cliente.ditar(umDitado())
    const [a, b] = await Promise.all([primeiro, segundo])
    expect([a.status, a.codigo]).toEqual([400, 'BAD_REQUEST'])
    expect(b.ditado.id).toBe(idDoDitado)
    expect(srv.chamadasAoProvedor).toBe(1)
  })

  it('o corpo que não chegou inteiro é 400 BAD_REQUEST, o áudio fica, e o reenvio com o mesmo id processa', async () => {
    const { srv, cliente } = montarRota()
    srv.falharNoProximoDitado('corpo_incompleto')
    const e = await falhou(cliente)
    expect([e.status, e.codigo]).toEqual([400, 'BAD_REQUEST'])
    expect(erroDoEnvio(e)).toMatchObject({ tipo: 'outro', retem: true, frase: 'Não deu para enviar o ditado.' })
    expect(srv.chamadasAoProvedor).toBe(0)
    await cliente.ditar(umDitado())
    expect(srv.chamadasAoProvedor).toBe(1)
  })

  it('a gravação que falha depois de o provedor cobrar é 500 INTERNAL, e o "Tentar de novo" com o mesmo id só grava (201)', async () => {
    contem(rota, 'o reenvio com o mesmo id só grava')
    contem(rota, 'o que só gravou o desfecho pago é `201`')
    const { srv, cliente } = montarRota({ ditado: ditadoComOPolish })
    srv.aoDitar({ texto: 'Ligar para o cliente.', aviso: 'sobrecarga' })
    srv.falharNoProximoDitado('gravacao')
    const e = await falhou(cliente)
    expect([e.status, e.codigo]).toEqual([500, 'INTERNAL'])
    // O áudio fica na memória do app, para o "Tentar de novo".
    expect(erroDoEnvio(e)).toMatchObject({ tipo: 'outro', retem: true })
    expect(srv.itens).toHaveLength(0)

    const g = await cliente.ditar(umDitado())
    expect(g.ditado.id).toBe(idDoDitado)
    // O aviso do texto que foi pago volta com ele.
    expect(g.aviso).toBe('sobrecarga')
    expect(srv.ditados.map((d) => [d.status, d.chamouOProvedor])).toEqual([
      [500, true],
      [201, false],
    ])
    expect(srv.chamadasAoProvedor).toBe(1)
    expect(srv.itens.map((i) => i.id)).toEqual([idDoDitado])
  })

  it('o que o provedor não ouviu como fala é lembrado: o mesmo id dá 422 sem chamá-lo; a fala medida pelo app, não', async () => {
    const { srv, cliente } = montarRota()
    srv.falharNoProximoDitado('provedor_sem_texto')
    await falhou(cliente)
    const denovo = await falhou(cliente)
    expect([denovo.status, denovo.codigo]).toEqual([422, 'SEM_FALA'])
    expect(srv.chamadasAoProvedor).toBe(1)
    // Outro id não herda nada.
    expect((await cliente.ditar(umDitado({ id: idN(2) }))).ditado.id).toBe(idN(2))
    // A fala medida abaixo do mínimo recusa antes do corpo e não fica na memória: o mesmo id, com a fala certa, passa.
    await falhou(cliente, umDitado({ id: idN(3), falaMs: 100 }))
    expect((await cliente.ditar(umDitado({ id: idN(3) }))).ditado.id).toBe(idN(3))
  })

  it('"Desligar o ditado" esquece o pago e não gravado: o reenvio paga de novo', async () => {
    const { srv, cliente } = montarRota()
    srv.falharNoProximoDitado('gravacao')
    await falhou(cliente)
    await cliente.desligarDitado()
    await srv.buscar('/api/v1/dictation/consent', { method: 'POST', body: JSON.stringify({ versao: 1, transcricao: 'openai' }) })
    await srv.buscar('/api/v1/ai-keys/openai', { method: 'PUT', body: JSON.stringify({ chave: 'sk-teste-do-celular-ABCD' }) })
    await cliente.ditar(umDitado())
    expect(srv.chamadasAoProvedor).toBe(2)
  })
})

describe('a rota POST /dictations: a ordem das conferências, os tetos e a forma dos pedidos', () => {
  const rota = texto('POST `/dictations`')

  it('a ordem: o aceite e a chave vêm antes do id; o ditado gravado e a lápide, antes das medidas e do tipo', async () => {
    const semAceite = montarRota({ ditado: {} })
    expect(await recusa(await cru(semAceite.srv, { id: 'não-é-uuid' }))).toEqual([403, 'SEM_ACEITE'])

    const { srv, cliente } = montarRota()
    await cliente.ditar(umDitado())
    // Já gravado: 200 mesmo com as medidas e o tipo errados (nada disso é conferido).
    expect((await cru(srv, { duracao: 'xx', fala: 'yy', tipo: 'audio/ogg' })).status).toBe(200)
    await cliente.moverParaLixeira(idDoDitado)
    await cliente.excluirDeVez(idDoDitado)
    expect(await recusa(await cru(srv, { duracao: 'xx', fala: 'yy', tipo: 'audio/ogg' }))).toEqual([410, 'APAGADO'])
  })

  it('a ordem das medidas: a forma (400), a duração (413), a fala (422), o tipo (415) e o tamanho (413)', async () => {
    const { srv } = montarRota()
    expect(await recusa(await cru(srv, { duracao: 'xx', tipo: 'audio/ogg' }))).toEqual([400, 'BAD_REQUEST'])
    expect(await recusa(await cru(srv, { duracao: '125001', fala: '100', tipo: 'audio/ogg' }))).toEqual([413, 'PAYLOAD_TOO_LARGE'])
    expect(await recusa(await cru(srv, { fala: '100', tipo: 'audio/ogg' }))).toEqual([422, 'SEM_FALA'])
    expect(await recusa(await cru(srv, { tipo: 'audio/ogg' }))).toEqual([415, 'FORMATO'])
    expect(await recusa(await cru(srv, { audio: webm(tetoDoCorpoNoServidorFalso + 1) }))).toEqual([413, 'PAYLOAD_TOO_LARGE'])
  })

  it('a duração acima de 125.000 ms é 413; no teto passa; e o app nunca manda mais que o teto dele', async () => {
    contem(rota, 'acima de 125.000')
    expect(tetoDaDuracaoNoServidorFalso).toBe(125_000)
    // O teto do app (2 minutos) cabe no do servidor, com a folga do 06 (5 s).
    expect(tetoMs).toBeLessThanOrEqual(tetoDaDuracaoNoServidorFalso)
    const { srv } = montarRota()
    expect((await cru(srv, { id: idN(1), duracao: '125000' })).status).toBe(201)
    expect(await recusa(await cru(srv, { id: idN(2), duracao: '125001' }))).toEqual([413, 'PAYLOAD_TOO_LARGE'])
  })

  it('o corpo de até 1 MiB passa; acima é 413', async () => {
    contem(rota, 'até **1 MiB**')
    expect(tetoDoCorpoNoServidorFalso).toBe(1 << 20)
    const { srv } = montarRota()
    expect((await cru(srv, { id: idN(1), audio: webm(1 << 20) })).status).toBe(201)
    expect(await recusa(await cru(srv, { id: idN(2), audio: webm((1 << 20) + 1) }))).toEqual([413, 'PAYLOAD_TOO_LARGE'])
  })

  it('a fala abaixo de 300 ms é 422 SEM_FALA, a mesma medida que o app usa para nem enviar', async () => {
    contem(rota, 'Abaixo de 300 é `422` `SEM_FALA`')
    expect(falaMinimaNoServidorFalso).toBe(300)
    expect(falaMinimaMs).toBe(falaMinimaNoServidorFalso)
    const { srv } = montarRota()
    expect((await cru(srv, { id: idN(1), fala: '300' })).status).toBe(201)
    expect(await recusa(await cru(srv, { id: idN(2), fala: '299' }))).toEqual([422, 'SEM_FALA'])
  })

  it('o Ditado-Id é um UUID v7 (a versão 7 e a variante 8 a b); maiúsculas valem e viram minúsculas', async () => {
    contem(rota, 'o UUID v7 que o app gerou (a versão 7 e a variante do RFC 9562; maiúsculas valem e viram minúsculas)')
    const { srv } = montarRota()
    for (const id of [
      '0192f0a1-7c3e-4d4a-9b2f-000000000001', // versão 4
      '0192f0a1-7c3e-7d4a-cb2f-000000000001', // variante c
      '0192f0a17c3e7d4a9b2f000000000001', // sem hífens
      'não-é-uuid',
      '',
      null, // o cabeçalho ausente
    ]) {
      expect(await recusa(await cru(srv, { id })), String(id)).toEqual([400, 'BAD_REQUEST'])
    }
    expect((await cru(srv, { id: idDoDitado.toUpperCase() })).status).toBe(201)
    expect(srv.itens.map((i) => i.id)).toEqual([idDoDitado])
    // O mesmo id em minúsculas é o mesmo ditado: 200, e nenhuma chamada nova ao provedor.
    expect((await cru(srv, { id: idDoDitado })).status).toBe(200)
    expect(srv.chamadasAoProvedor).toBe(1)
  })

  it('os ids que o app gera (uuidV7) são aceitos', async () => {
    const { srv } = montarRota()
    for (let i = 0; i < 20; i++) {
      const id = uuidV7(Date.now() + i, (n) => crypto.getRandomValues(new Uint8Array(n)))
      expect((await cru(srv, { id })).status, id).toBe(201)
    }
  })

  it('o Content-Type é audio/webm e os parâmetros valem (;codecs=opus); outro tipo, ou nenhum, é 415 FORMATO', async () => {
    contem(rota, 'os parâmetros, como `;codecs=opus`, valem')
    const { srv } = montarRota()
    let n = 0
    for (const tipo of ['audio/webm', 'audio/webm;codecs=opus', 'Audio/WebM; codecs=opus']) {
      expect((await cru(srv, { id: idN(++n), tipo })).status, tipo).toBe(201)
    }
    for (const tipo of ['audio/ogg', 'video/webm', 'application/json', 'multipart/form-data; boundary=x', '', null]) {
      expect(await recusa(await cru(srv, { id: idN(++n), tipo })), String(tipo)).toEqual([415, 'FORMATO'])
    }
  })

  it('os cabeçalhos das medidas fora da forma são 400 BAD_REQUEST (milissegundos inteiros, sem sinal, a fala até a duração)', async () => {
    contem(rota, 'em milissegundos inteiros')
    const { srv } = montarRota()
    const ruins: Cru[] = [
      { duracao: null },
      { fala: null },
      { duracao: '' },
      { fala: '' },
      { duracao: '-5' },
      { fala: '-1' },
      { duracao: '1.5' },
      { fala: '3e2' },
      { duracao: 'abc' },
      { duracao: '1234567890' },
      // A fala maior que a duração.
      { duracao: '4000', fala: '4001' },
    ]
    for (const c of ruins) expect(await recusa(await cru(srv, c)), JSON.stringify(c)).toEqual([400, 'BAD_REQUEST'])
    // O 400 não cobra nada: o mesmo id, com os cabeçalhos certos, passa.
    expect((await cru(srv)).status).toBe(201)
    expect(srv.chamadasAoProvedor).toBe(1)
  })

  it('só o POST existe: GET, PUT e DELETE em /dictations são 404', async () => {
    // Esta frase está na abertura de "Ditar pelo celular", antes da seção da rota.
    contem(doc.replace(/\s+/g, ' '), '`GET`, `PUT` e `DELETE /dictations`, e qualquer coisa depois de `/dictations/`, também são `404`')
    const { srv } = montarRota()
    for (const metodo of ['GET', 'PUT', 'DELETE']) {
      const r = await srv.buscar('/api/v1/dictations', { method: metodo, credentials: 'same-origin' })
      expect(r.status, metodo).toBe(404)
    }
  })

  it('o ditado nasce da sessão: source_platform web, sem aparelho, com o provedor e o modelo das preferências', async () => {
    const { srv, cliente } = montarRota()
    const { ditado } = await cliente.ditar(umDitado())
    expect(srv.itens[0]).toMatchObject({ source_platform: 'web', device_id: null, asr_provider: 'openai', asr_model: 'gpt-4o-mini-transcribe', duration_ms: 4000 })
    expect(ditado.id).toBe(idDoDitado)
  })
})
