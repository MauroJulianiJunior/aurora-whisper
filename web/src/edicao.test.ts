import { describe, expect, it } from 'vitest'
import { criarCliente } from './clienteLocal'
import { naoTocado, novoRascunho, salvarSobre, textoInicial, textosNaoSalvos } from './edicao'
import { ditado, servidorFalso } from './testes/servidorFalso'

const criado = new Date(2026, 8, 28, 10).toISOString()

function montar() {
  const srv = servidorFalso([ditado({ id: 'd1', created_at: criado, raw_text: 'bruto', final_text: 'Texto de base.' })])
  const cliente = criarCliente('tok', srv.buscar)
  const bases = () => srv.pedidos.filter((p) => p.metodo === 'PATCH').map((p) => p.corpo?.base_version)
  return { srv, cliente, bases }
}

describe('salvarSobre', () => {
  it('salva sobre a versão base', async () => {
    const { cliente, bases } = montar()
    const s = await salvarSobre(cliente, 'd1', 1, 'Texto de base.', 'Novo.')
    expect(s.tipo).toBe('salvo')
    expect(bases()).toEqual([1])
  })

  it('traz as sugestões de aprendizado do PATCH; sem o campo, lista vazia', async () => {
    const { srv, cliente } = montar()
    const sf = { de: 'sf', para: 'ECF', termo_existente: 'ECF', ocorrencias: 2 }
    srv.sugerirNoSalvar([sf])
    const com = await salvarSobre(cliente, 'd1', 1, 'Texto de base.', 'Novo.')
    expect(com).toMatchObject({ tipo: 'salvo', sugestoes: [sf], item: { id: 'd1', final_text: 'Novo.', version: 2 } })
    // O ditado que a lista guarda não leva o campo das sugestões.
    expect(com.tipo === 'salvo' && 'sugestoes' in com.item).toBe(false)

    // Um Agent anterior à etapa 3 não manda o campo.
    srv.interceptar((p) => (p.metodo === 'PATCH' ? new Response(JSON.stringify({ ...srv.itens[0], version: 9 }), { status: 200 }) : undefined))
    expect(await salvarSobre(cliente, 'd1', 2, 'Novo.', 'Outro.')).toMatchObject({ tipo: 'salvo', sugestoes: [] })
    // Um slice nil do Go chega como null.
    srv.interceptar((p) => (p.metodo === 'PATCH' ? new Response(JSON.stringify({ ...srv.itens[0], sugestoes: null }), { status: 200 }) : undefined))
    expect(await salvarSobre(cliente, 'd1', 2, 'Novo.', 'Outro.')).toMatchObject({ tipo: 'salvo', sugestoes: [] })
  })

  it('o conflito não traz sugestão', async () => {
    const { srv, cliente } = montar()
    srv.sugerirNoSalvar([{ de: 'sf', para: 'ECF', termo_existente: '', ocorrencias: 0 }])
    srv.alterarPorFora('d1', 'Da outra aba.')
    const s = await salvarSobre(cliente, 'd1', 1, 'Texto de base.', 'Novo.')
    expect(s.tipo).toBe('conflito')
    expect(s).not.toHaveProperty('sugestoes')
  })

  it('só a versão mudou (lixeira e Desfazer em outra aba): tenta de novo uma vez, sobre a versão atual', async () => {
    const { srv, cliente, bases } = montar()
    srv.lixeiraPorFora('d1', true)
    const s = await salvarSobre(cliente, 'd1', 1, 'Texto de base.', 'Novo.')
    expect(s.tipo).toBe('salvo')
    expect(bases()).toEqual([1, 3])
  })

  it('o texto mudou em outra aba: conflito, sem tentar de novo', async () => {
    const { srv, cliente, bases } = montar()
    srv.alterarPorFora('d1', 'Da outra aba.')
    const s = await salvarSobre(cliente, 'd1', 1, 'Texto de base.', 'Novo.')
    expect(s).toMatchObject({ tipo: 'conflito', atual: { final_text: 'Da outra aba.', version: 2 } })
    expect(bases()).toEqual([1])
  })

  it('o ditado foi para a lixeira em outra aba: conflito, e não grava nele', async () => {
    const { srv, cliente, bases } = montar()
    srv.lixeiraPorFora('d1')
    const s = await salvarSobre(cliente, 'd1', 1, 'Texto de base.', 'Novo.')
    expect(s.tipo).toBe('conflito')
    expect(s.tipo === 'conflito' && s.atual.deleted_at).toBeTruthy()
    expect(bases()).toEqual([1])
    expect(srv.itens[0]?.final_text).toBe('Texto de base.')
  })

  it('a nova tentativa também dá 409: para aí, com o atual', async () => {
    const { srv, cliente, bases } = montar()
    srv.interceptar((p) => (p.metodo === 'PATCH' ? new Response('{}', { status: 409 }) : undefined))
    const s = await salvarSobre(cliente, 'd1', 1, 'Texto de base.', 'Novo.')
    expect(s.tipo).toBe('conflito')
    expect(bases()).toEqual([1, 1])
  })
})

describe('rascunho', () => {
  const falhou = ditado({ id: 'e1', created_at: criado, status: 'error', raw_text: 'o que o ASR ouviu', final_text: '' })

  it('ditado que falhou antes do texto final começa pelo bruto, com a base vazia', () => {
    expect(textoInicial(falhou)).toBe('o que o ASR ouviu')
    expect(novoRascunho(falhou)).toEqual({ texto: 'o que o ASR ouviu', base: 1, textoBase: '', conflito: false })
  })

  it('não tocado é o que o editor mostrou ao abrir; rascunho com conflito nunca é descartado', () => {
    const r = novoRascunho(falhou)
    expect(naoTocado(r, falhou)).toBe(true)
    expect(naoTocado({ ...r, texto: 'mexi' }, falhou)).toBe(false)
    expect(naoTocado({ ...r, conflito: true }, falhou)).toBe(false)
  })

  it('textos não salvos: só os rascunhos mexidos, na ordem em que as linhas abriram', () => {
    const ok = ditado({ id: 'd1', created_at: criado, final_text: 'Texto final.' })
    const itens = [falhou, ok]
    // Abertos e não tocados (o do ditado que falhou começa pelo bruto): nada.
    expect(textosNaoSalvos({ e1: novoRascunho(falhou), d1: novoRascunho(ok) }, itens)).toEqual([])
    expect(textosNaoSalvos({}, itens)).toEqual([])
    const mexidos = { d1: { ...novoRascunho(ok), texto: 'Mudei.' }, e1: { ...novoRascunho(falhou), texto: '' } }
    expect(textosNaoSalvos(mexidos, itens)).toEqual(['Mudei.', ''])
    // O conflito sem texto novo ainda é do Painel: a versão da outra aba não
    // é a da pessoa.
    expect(textosNaoSalvos({ d1: { ...novoRascunho(ok), conflito: true } }, itens)).toEqual(['Texto final.'])
    // A linha que saiu da lista (sem o item) compara com a base.
    expect(textosNaoSalvos({ x: { texto: 'a', base: 1, textoBase: 'a', conflito: false } })).toEqual([])
    expect(textosNaoSalvos({ x: { texto: 'b', base: 1, textoBase: 'a', conflito: false } })).toEqual(['b'])
  })
})
