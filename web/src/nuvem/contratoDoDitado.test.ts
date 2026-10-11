// O contrato do ditado pelo celular na conta, preso ao 06 (docs/ai-spec/06-api-contract.md):
// as rotas /dictation, /dictation/settings, /dictation/consent e /ai-keys que a C3a e a
// C3b fixaram. O app da C6b chegou a desenhar OUTRO formato, porque o servidor ainda não
// existia; este teste é o que impede a repetição. O 06 é a descrição do servidor de
// verdade (internal/server/ditado_celular.go e chaves_ia.go, executados pela suíte
// internal/server/chavestest); o servidor de mentira dos testes do app
// (testes/servidorFalsoNuvem.ts) e o cliente (cliente.ts) têm de falar o que o 06 mostra:
// os mesmos campos, nos mesmos tipos, e os mesmos status para cada código.
//
// O 06 é lido como texto (?raw): nenhum arquivo de fora de web/ entra em build algum, e o
// teste não usa o módulo fs do Node.

import { describe, expect, it } from 'vitest'
import doc from '../../../docs/ai-spec/06-api-contract.md?raw'
import { itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import { criarClienteNuvem } from './cliente'

// A seção `### <titulo>` do 06, até a próxima seção.
function secao(titulo: string): string {
  const inicio = doc.indexOf(`### ${titulo}\n`)
  if (inicio < 0) throw new Error(`o 06 perdeu a seção "${titulo}"`)
  const resto = doc.slice(inicio + 1)
  const fim = resto.search(/\n#{2,3} /)
  return fim < 0 ? resto : resto.slice(0, fim)
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
  // A tabela "Os códigos": | `CODIGO` | status | quando |.
  const tabela = new Map<string, number>()
  for (const m of secao('Os códigos (a lista fechada do ditado no celular, ADR-021)').matchAll(/^\| `([A-Z_]+)` \| (\d{3})/gm)) tabela.set(m[1]!, Number(m[2]))

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
