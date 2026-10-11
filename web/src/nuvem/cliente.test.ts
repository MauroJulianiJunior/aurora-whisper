// O cliente da nuvem contra a nuvem de mentira (testes/servidorFalsoNuvem.ts):
// o que sai em cada pedido (rota, método, corpo, cabeçalhos) e como cada
// resposta vira o que o Painel entende.

import { describe, expect, it } from 'vitest'
import { Conflito, ErroDaApi, ForaDoAr, SemSessao } from '../api'
import { aprendido, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import { criarClienteNuvem, criarPortao } from './cliente'

const em = (dia: number, hora: number) => new Date(2026, 9, dia, hora).toISOString()

function montar(opcoes: Parameters<typeof servidorFalsoNuvem>[1] = {}) {
  const srv = servidorFalsoNuvem(
    [
      itemNuvem({ id: 'd1', created_at: em(5, 14), final_text: 'Manda o relatório amanhã.' }),
      itemNuvem({ id: 'd2', created_at: em(5, 9), final_text: 'A reunião é às dez.' }),
      itemNuvem({ id: 'd3', created_at: em(4, 9), final_text: 'Na lixeira.', deleted_at: em(5, 10) }),
    ],
    opcoes,
  )
  return { srv, cliente: criarClienteNuvem(srv.buscar) }
}

describe('cliente da nuvem', () => {
  it('a busca vai no corpo de um POST: o texto nunca vai na URL', async () => {
    const { srv, cliente } = montar()
    const p = await cliente.listar({ lixeira: false, busca: 'relatório', cursor: '0' })
    expect(p.items.map((i) => i.id)).toEqual(['d1'])
    const [pedido] = srv.pedidos
    expect(pedido?.metodo).toBe('POST')
    expect(pedido?.caminho).toBe('/api/v1/transcriptions/search')
    expect(pedido?.corpo).toEqual({ q: 'relatório', trash: false, cursor: '0' })
    expect(pedido?.busca.toString()).toBe('')
    for (const p of srv.pedidos) expect(JSON.stringify([p.caminho, p.busca.toString()])).not.toContain('relat')
  })

  it('sem busca, a lista é um GET, com a lixeira e o cursor na URL', async () => {
    const { srv, cliente } = montar()
    const lixeira = await cliente.listar({ lixeira: true, busca: '' })
    expect(lixeira.items.map((i) => i.id)).toEqual(['d3'])
    await cliente.listar({ lixeira: false, busca: '', cursor: 'abc' })
    expect(srv.pedidos.map((p) => [p.metodo, p.caminho, p.busca.toString()])).toEqual([
      ['GET', '/api/v1/transcriptions', 'trash=true'],
      ['GET', '/api/v1/transcriptions', 'cursor=abc'],
    ])
  })

  it('nunca manda Authorization: toda chamada é da mesma origem, pelo cookie', async () => {
    const { srv, cliente } = montar({ teste: { conta: 'voce@teste.local', google: true } })
    await cliente.eu()
    await cliente.listar({ lixeira: false, busca: '' })
    await cliente.listar({ lixeira: false, busca: 'x' })
    await cliente.obter('d1')
    await cliente.editar('d1', 1, 'novo')
    await cliente.moverParaLixeira('d2')
    await cliente.restaurar('d2')
    await cliente.sessoes()
    await cliente.aparelhos()
    await cliente.aprendidos()
    await cliente.desfazerAprendido('0192f0a1-7c3e-7d4a-9b2f-000000000000').catch(() => undefined)
    await cliente.loginDeTeste()
    await cliente.inicioDoGoogle()
    await cliente.ditar({ id: '0192f0a1-7c3e-7d4a-9b2f-000000000009', audio: new Blob([new Uint8Array([1])]), duracaoMs: 1000, falaMs: 800 })
    await cliente.apagarMeusDados()
    await cliente.sair()
    expect(srv.pedidos.length).toBeGreaterThanOrEqual(16)
    for (const p of srv.pedidos) {
      expect(Object.keys(p.cabecalhos).map((k) => k.toLowerCase())).not.toContain('authorization')
      expect(p.credentials).toBe('same-origin')
    }
  })

  it('o item da nuvem vira o do painel: o que é null fica ausente, e o selo vem junto', async () => {
    const { srv, cliente } = montar()
    srv.conflitoPorFora('d1', 'do PC')
    const d1 = await cliente.obter('d1')
    expect(d1.versao_em_conflito?.final_text).toBe('do PC')
    expect('deleted_at' in d1).toBe(false)
    const d2 = await cliente.obter('d2')
    expect('versao_em_conflito' in d2).toBe(false)
    const p = await cliente.listar({ lixeira: false, busca: '' })
    expect(p.next_cursor).toBeUndefined()
  })

  it('editar manda a base e o texto, e nunca traz sugestão de aprender', async () => {
    const { srv, cliente } = montar()
    const g = await cliente.editar('d1', 1, 'Manda amanhã.')
    expect(g.sugestoes).toEqual([])
    expect(g.ditado.version).toBe(2)
    expect(srv.pedidos.at(-1)?.corpo).toEqual({ base_version: 1, final_text: 'Manda amanhã.' })
    await expect(cliente.editar('d1', 1, 'de novo')).rejects.toBeInstanceOf(Conflito)
  })

  it('a escolha do selo é um PATCH com resolver_conflito, e o 409 é Conflito', async () => {
    const { srv, cliente } = montar()
    srv.conflitoPorFora('d1', 'do PC')
    const novo = await cliente.resolverConflito('d1', 2, 'trocar')
    expect(novo.final_text).toBe('do PC')
    expect(novo.versao_em_conflito).toBeUndefined()
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'PATCH', caminho: '/api/v1/transcriptions/d1', corpo: { base_version: 2, resolver_conflito: 'trocar' } })
    await expect(cliente.resolverConflito('d1', 3, 'manter')).rejects.toBeInstanceOf(Conflito)
  })

  it('excluir definitivamente: POST purge; fora da lixeira, a frase do servidor', async () => {
    const { srv, cliente } = montar()
    await cliente.excluirDeVez('d3')
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'POST', caminho: '/api/v1/transcriptions/d3/purge' })
    const e = await cliente.excluirDeVez('d1').catch((x: unknown) => x)
    expect(e).toBeInstanceOf(ErroDaApi)
    expect((e as ErroDaApi).message).toBe('o ditado não está na lixeira')
  })

  it('401 é SemSessao; a rede que não responde é ForaDoAr', async () => {
    const { srv, cliente } = montar({ logado: false })
    await expect(cliente.eu()).rejects.toBeInstanceOf(SemSessao)
    await expect(cliente.listar({ lixeira: false, busca: '' })).rejects.toBeInstanceOf(SemSessao)
    srv.derrubar(true)
    await expect(cliente.eu()).rejects.toBeInstanceOf(ForaDoAr)
  })

  it('o login do Google começa pelo POST /auth/start e devolve o endereço https', async () => {
    const { srv, cliente } = montar({ logado: false })
    expect(await cliente.inicioDoGoogle()).toMatch(/^https:\/\/accounts\.google\.com\//)
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'POST', caminho: '/auth/start', corpo: {} })
    // Um endereço que não é https não sai daqui.
    srv.interceptar((p) => (p.caminho === '/auth/start' ? new Response(JSON.stringify({ url: 'javascript:alert(1)' })) : undefined))
    await expect(cliente.inicioDoGoogle()).rejects.toBeInstanceOf(ErroDaApi)
    // 429: o limite de tentativas, com o status para a tela.
    srv.interceptar(() => new Response('{}', { status: 429 }))
    const e = await cliente.inicioDoGoogle().catch((x: unknown) => x)
    expect((e as ErroDaApi).status).toBe(429)
    // A janela de entrar pede a volta para ela.
    srv.interceptar(null)
    await cliente.inicioDoGoogle('/?janela=entrou')
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'POST', caminho: '/auth/start', corpo: { volta: '/?janela=entrou' } })
  })

  it('o portão fechado devolve 401 sem pedir nada à nuvem; aberto, deixa passar', async () => {
    const { srv } = montar()
    const portao = criarPortao()
    const cliente = criarClienteNuvem(portao.passagem(srv.buscar))
    expect((await cliente.listar({ lixeira: false, busca: '' })).items).toHaveLength(2)
    portao.fechar()
    const antes = srv.pedidos.length
    await expect(cliente.listar({ lixeira: false, busca: '' })).rejects.toBeInstanceOf(SemSessao)
    await expect(cliente.editar('d1', 1, 'outro')).rejects.toBeInstanceOf(SemSessao)
    expect(srv.pedidos).toHaveLength(antes)
    expect(srv.itens.find((i) => i.id === 'd1')?.final_text).toBe('Manda o relatório amanhã.')
    portao.abrir()
    expect((await cliente.eu()).id).toBe('u1')
    expect(srv.pedidos).toHaveLength(antes + 1)
  })

  it('o login de teste: null sem ele (404); com ele, a conta e se o Google existe', async () => {
    expect(await montar().cliente.loginDeTeste()).toBeNull()
    const { srv, cliente } = montar({ logado: false, teste: { conta: 'voce@teste.local', google: false } })
    expect(await cliente.loginDeTeste()).toEqual({ conta: 'voce@teste.local', google: false })
    await cliente.entrarComContaDeTeste('voce@teste.local')
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'POST', caminho: '/auth/teste', corpo: { email: 'voce@teste.local' } })
    expect((await cliente.eu()).email).toBe('voce@teste.local')
  })

  it('o aprendido: a lista é um GET sem query, e o desfazer, um POST com o id no corpo', async () => {
    const sf = aprendido({ variante: 'sf', termo: 'ECF' })
    const dctf = aprendido({ variante: 'dctf web', termo: 'DCTFWeb', dominio: 'contabilidade', restrito: true })
    const { srv, cliente } = montar({ aprendidos: [sf, dctf], tetoDosAprendidos: 1 })
    expect(await cliente.aprendidos()).toEqual({ items: [sf], has_more: true })
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'GET', caminho: '/api/v1/learned', corpo: undefined })
    expect(srv.pedidos.at(-1)?.busca.toString()).toBe('')

    await cliente.desfazerAprendido(sf.id)
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'POST', caminho: '/api/v1/learned/undo', corpo: { id: sf.id }, credentials: 'same-origin' })
    expect(srv.pedidos.at(-1)?.busca.toString()).toBe('')
    expect(await cliente.aprendidos()).toEqual({ items: [dctf], has_more: false })
    // O texto da troca nunca foi à URL.
    for (const p of srv.pedidos) expect(JSON.stringify([p.caminho, p.busca.toString()])).not.toMatch(/sf|dctf|ECF/i)
  })

  it('o aprendido: items null é a lista vazia, e has_more só vale com true', async () => {
    const { srv, cliente } = montar()
    srv.interceptar(() => new Response(JSON.stringify({ items: null }), { status: 200 }))
    expect(await cliente.aprendidos()).toEqual({ items: [], has_more: false })
    srv.interceptar(() => new Response(JSON.stringify({ items: [], has_more: 1 }), { status: 200 }))
    expect((await cliente.aprendidos()).has_more).toBe(false)
  })

  it('o aprendido: o desfazer que a nuvem não conhece é 404 com a frase dela; sem sessão, SemSessao', async () => {
    const { srv, cliente } = montar({ aprendidos: [aprendido({ variante: 'sf', termo: 'ECF' })] })
    const e = await cliente.desfazerAprendido('0192f0a1-7c3e-7d4a-9b2f-000000000000').catch((x: unknown) => x)
    expect(e).toBeInstanceOf(ErroDaApi)
    expect((e as ErroDaApi).status).toBe(404)
    expect((e as ErroDaApi).message).toBe('troca aprendida não encontrada')
    srv.expirar()
    await expect(cliente.aprendidos()).rejects.toBeInstanceOf(SemSessao)
    await expect(cliente.desfazerAprendido('x')).rejects.toBeInstanceOf(SemSessao)
  })

  it('apagar meus dados leva a confirmação; sair é o POST /auth/logout', async () => {
    const { srv, cliente } = montar()
    await cliente.apagarMeusDados()
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'DELETE', caminho: '/api/v1/me/data', corpo: { confirmar: 'apagar' } })
    expect(srv.itens).toHaveLength(0)
    await cliente.sair()
    expect(srv.pedidos.at(-1)).toMatchObject({ metodo: 'POST', caminho: '/auth/logout' })
    await expect(cliente.eu()).rejects.toBeInstanceOf(SemSessao)
  })
})

// O ditado do celular (ADR-021, C6a): o POST /api/v1/dictations com o áudio
// cru, o id e o que o app mediu nos cabeçalhos, e os códigos de erro em
// ErroDaApi.codigo.
describe('ditar', () => {
  const id = '0192f0a1-7c3e-7d4a-9b2f-000000000001'
  const audio = new Blob([new Uint8Array([1, 2, 3, 4, 5])], { type: 'audio/webm;codecs=opus' })
  const pedido = { id, audio, duracaoMs: 4200.4, falaMs: 3100.6 }

  it('o corpo é o áudio cru; o id, a duração e a fala vão em cabeçalhos; o texto nunca vai à URL', async () => {
    const { srv, cliente } = montar()
    srv.aoDitar({ texto: 'Manda o relatório amanhã.' })
    const g = await cliente.ditar(pedido)

    const [p] = srv.pedidos
    expect(p).toMatchObject({ metodo: 'POST', caminho: '/api/v1/dictations', credentials: 'same-origin' })
    expect(p?.busca.toString()).toBe('')
    expect(p?.corpo).toBeUndefined()
    expect(p?.audio).toBe(audio)
    expect(p?.cabecalhos).toEqual({
      'Content-Type': 'audio/webm',
      'Ditado-Id': id,
      'Ditado-Duracao-Ms': '4200',
      'Ditado-Fala-Ms': '3101',
    })
    expect(srv.ditados).toEqual([{ id, bytes: 5, tipo: 'audio/webm', duracaoMs: 4200, falaMs: 3101, chamouOProvedor: true }])
    expect(g.ditado.final_text).toBe('Manda o relatório amanhã.')
    expect(g.aviso).toBeUndefined()
  })

  it('o ditado volta no formato do painel: veio da web, sem aparelho, e o que é null fica ausente', async () => {
    const { srv, cliente } = montar()
    const { ditado } = await cliente.ditar(pedido)
    expect('deleted_at' in ditado).toBe(false)
    expect('versao_em_conflito' in ditado).toBe(false)
    expect(srv.itens.find((i) => i.id === id)).toMatchObject({ source_platform: 'web', device_id: null })
    expect(ditado.id).toBe(id)
  })

  it('quando o Polish caiu para o Smart, o motivo vem como aviso (e fica fora do ditado)', async () => {
    const { srv, cliente } = montar()
    srv.aoDitar({ texto: 'Texto.', aviso: 'chave_recusada' })
    const g = await cliente.ditar(pedido)
    expect(g.aviso).toBe('chave_recusada')
    expect('aviso' in g.ditado).toBe(false)
  })

  it('o mesmo id de novo: o servidor devolve o que está gravado, sem chamar o provedor', async () => {
    const { srv, cliente } = montar()
    const a = await cliente.ditar(pedido)
    const b = await cliente.ditar(pedido)
    expect(b.ditado).toEqual(a.ditado)
    expect(srv.ditados.map((d) => d.chamouOProvedor)).toEqual([true, false])
    expect(srv.chamadasAoProvedor).toBe(1)
    expect(srv.itens.filter((i) => i.id === id)).toHaveLength(1)
  })

  it('o sinal chega ao fetch: o prazo de 90 s corta o envio', async () => {
    const { srv } = montar()
    const ctrl = new AbortController()
    let sinal: AbortSignal | null | undefined
    const buscar = (e: string, init?: RequestInit) => {
      sinal = init?.signal
      return srv.buscar(e, init)
    }
    await criarClienteNuvem(buscar).ditar(pedido, ctrl.signal)
    expect(sinal).toBe(ctrl.signal)
  })

  it('o envio cortado pelo prazo é AbortError, como veio (e não vira "Sem conexão")', async () => {
    const ctrl = new AbortController()
    ctrl.abort()
    const abortado = criarClienteNuvem(() => Promise.reject(new DOMException('abortado', 'AbortError')))
    const e = await abortado.ditar(pedido, ctrl.signal).catch((x: unknown) => x)
    expect(e).toBeInstanceOf(DOMException)
    expect((e as DOMException).name).toBe('AbortError')
    expect(e).not.toBeInstanceOf(ForaDoAr)
  })

  it('401 é SemSessao; a rede que cai é ForaDoAr', async () => {
    const { srv, cliente } = montar({ logado: false })
    await expect(cliente.ditar(pedido)).rejects.toBeInstanceOf(SemSessao)
    srv.derrubar(true)
    await expect(cliente.ditar(pedido)).rejects.toBeInstanceOf(ForaDoAr)
  })

  it('o código do envelope vai em ErroDaApi.codigo, e o Retry-After em esperaSeg', async () => {
    const { srv, cliente } = montar()
    srv.interceptar(
      () =>
        new Response(JSON.stringify({ error: { code: 'LIMITE', message: 'tentativas demais', request_id: 'r1' } }), {
          status: 429,
          headers: { 'Retry-After': '12' },
        }),
    )
    const e = (await cliente.ditar(pedido).catch((x: unknown) => x)) as ErroDaApi
    expect(e).toBeInstanceOf(ErroDaApi)
    expect(e.status).toBe(429)
    expect(e.codigo).toBe('LIMITE')
    expect(e.esperaSeg).toBe(12)
  })

  it.each([
    [403, 'SEM_ACEITE'],
    [403, 'SEM_CHAVE'],
    [429, 'UM_POR_VEZ'],
    [503, 'OCUPADO'],
    [413, 'PAYLOAD_TOO_LARGE'],
    [415, 'FORMATO'],
    [422, 'SEM_FALA'],
    [502, 'PROVEDOR_RECUSOU_CHAVE'],
    [502, 'PROVEDOR_LIMITE'],
    [502, 'PROVEDOR_FORA'],
    [410, 'APAGADO'],
  ])('%d %s chega com o código', async (status, code) => {
    const { srv, cliente } = montar()
    srv.interceptar(() => new Response(JSON.stringify({ error: { code, message: 'a frase do servidor' } }), { status }))
    const e = (await cliente.ditar(pedido).catch((x: unknown) => x)) as ErroDaApi
    expect(e).toBeInstanceOf(ErroDaApi)
    expect(e.codigo).toBe(code)
  })

  it('o 409 não é versão velha: chega como ErroDaApi, não como Conflito', async () => {
    const { srv, cliente } = montar()
    srv.interceptar(() => new Response(JSON.stringify({ error: { code: 'CONFLICT', message: 'x' } }), { status: 409 }))
    const e = await cliente.ditar(pedido).catch((x: unknown) => x)
    expect(e).toBeInstanceOf(ErroDaApi)
    expect(e).not.toBeInstanceOf(Conflito)
  })

  it('sem o envelope (um proxy), fica o status; o Retry-After que é uma data não conta', async () => {
    const { srv, cliente } = montar()
    srv.interceptar(() => new Response('<html>bad gateway</html>', { status: 502, headers: { 'Retry-After': 'Wed, 21 Oct 2026 07:28:00 GMT' } }))
    const e = (await cliente.ditar(pedido).catch((x: unknown) => x)) as ErroDaApi
    expect(e.status).toBe(502)
    expect(e.codigo).toBeUndefined()
    expect(e.esperaSeg).toBeUndefined()
    expect(e.message).toBe('erro 502')
  })

  it('o fake confere o que o servidor confere: id v7, fala mínima, formato, lápide', async () => {
    const { srv, cliente } = montar()
    const status = async (p: Parameters<typeof cliente.ditar>[0]) => ((await cliente.ditar(p).catch((x: unknown) => x)) as ErroDaApi).status
    expect(await status({ ...pedido, id: 'não-é-uuid' })).toBe(400)
    expect(await status({ ...pedido, falaMs: 299 })).toBe(422)
    // A lápide: o id apagado de vez é 410, sem provedor.
    await cliente.ditar(pedido)
    await cliente.moverParaLixeira(id)
    await cliente.excluirDeVez(id)
    expect(await status(pedido)).toBe(410)
    expect(srv.chamadasAoProvedor).toBe(1)
  })
})
