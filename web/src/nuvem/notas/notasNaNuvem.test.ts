// As notas no celular (ADR-022, N8): o cliente da /api/v1, a lista guardada no
// aparelho e a fila do que se faz sem rede (E4, "Marcar sem rede"). Os casos da
// ADR, "Testes": marcar sem rede, recarregar a página, a rede voltar, o PC ter
// mudado a nota no meio-tempo (mescla), o texto mudado dos dois lados (selo) e a
// mudança que o servidor recusa (nunca perda calada).

import { describe, expect, it, vi } from 'vitest'
import { SoComRede } from '../../notas/tipos'
import { notaDaNuvem, notasFalsasNaNuvem } from './notasFalsas'
import { armazemNaMemoria } from './armazem'
import { NotasNaNuvem } from './notasNaNuvem'

const corpo = '- [ ] leite\n- [ ] pão\n- [ ] ovos'
const marcado = '- [ ] pão\n- [ ] ovos\n- [x] leite'

function montar(armazem = armazemNaMemoria(), nuvem = notasFalsasNaNuvem([notaDaNuvem({ id: 'n1', titulo: 'Lista do sábado', corpo })])) {
  const agendados: (() => void)[] = []
  const notas = new NotasNaNuvem({
    buscar: nuvem.buscar,
    armazem,
    online: () => true,
    ouvirRede: () => () => {},
    depois: (_, cb) => {
      agendados.push(cb)
      return () => {}
    },
    agora: () => new Date('2026-10-27T12:00:00Z'),
  })
  const avisos: string[] = []
  notas.rede.aoAvisar((t) => void avisos.push(t))
  return { notas, nuvem, armazem, avisos, agendados }
}

describe('a lista', () => {
  it('com rede, vem da nuvem e fica guardada; sem rede, sai do aparelho, na mesma ordem', async () => {
    const m = montar()
    const lista = await m.notas.cliente.listarNotas({ vista: 'ativas' })
    expect(lista.map((n) => n.titulo)).toEqual(['Lista do sábado'])
    expect(m.notas.rede.estado().semRede).toBe(false)
    expect(m.armazem.foto().notas.map((n) => n.id)).toEqual(['n1'])

    // O app fechou; abre de novo no mercado, sem sinal.
    m.nuvem.cair()
    const outra = montar(m.armazem, m.nuvem)
    const semRede = await outra.notas.cliente.listarNotas({ vista: 'ativas' })
    expect(semRede.map((n) => n.titulo)).toEqual(['Lista do sábado'])
    expect(outra.notas.rede.estado().semRede).toBe(true)
  })

  it('busca e marcador filtram no aparelho, sem diferenciar acento', async () => {
    const nuvem = notasFalsasNaNuvem([
      notaDaNuvem({ id: 'a', titulo: 'Reunião', marcadores: ['m1'], posicao: 'A' }),
      notaDaNuvem({ id: 'b', titulo: 'Mercado', posicao: 'B' }),
    ])
    const m = montar(armazemNaMemoria(), nuvem)
    expect((await m.notas.cliente.listarNotas({ vista: 'ativas', busca: 'reuniao' })).map((n) => n.id)).toEqual(['a'])
    expect((await m.notas.cliente.listarNotas({ vista: 'ativas', marcador: 'm1' })).map((n) => n.id)).toEqual(['a'])
  })
})

describe('sem rede: marcar, desmarcar e acrescentar item', () => {
  it('marcar vai para a fila, sobrevive a recarregar a página e sobe quando a rede volta, com a base e o corpo dela', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    // A janela tentou salvar e a rede não respondeu: o marcar fica na fila.
    const nota = await m.notas.cliente.editarNota('n1', 1, { corpo: marcado })
    expect(nota.esperando).toBe(true)
    expect(nota.corpo).toBe(marcado)
    expect(m.notas.rede.estado()).toEqual({ semRede: true, esperando: new Set(['n1']) })
    expect(m.armazem.foto().fila).toEqual([
      expect.objectContaining({ notaId: 'n1', baseVersion: 1, tituloBase: 'Lista do sábado', corpoBase: corpo, corpo: marcado }),
    ])

    // Recarregar a página: a fila e a lista vêm do aparelho, com "esperando a rede".
    const depois = montar(m.armazem, m.nuvem)
    const lista = await depois.notas.cliente.listarNotas({ vista: 'ativas' })
    expect(lista[0]).toEqual(expect.objectContaining({ corpo: marcado, esperando: true }))
    expect(depois.notas.rede.estado().esperando.has('n1')).toBe(true)

    // A rede volta.
    m.nuvem.voltar()
    await depois.notas.subir()
    expect(m.nuvem.patches()).toEqual([{ base_version: 1, titulo: 'Lista do sábado', corpo: marcado, corpo_base: corpo }])
    expect(depois.notas.rede.estado()).toEqual({ semRede: false, esperando: new Set() })
    expect(m.armazem.foto().fila).toEqual([])
    expect((await depois.notas.cliente.listarNotas({ vista: 'ativas' }))[0]).toEqual(expect.objectContaining({ corpo: marcado, version: 2 }))
    expect(depois.notas.cliente).toBeTruthy()
  })

  it('a janela que ficou aberta enquanto a fila subia continua da versão nova, sem conflito com ela mesma', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    const naFila = await m.notas.cliente.editarNota('n1', 1, { corpo: marcado })
    expect(naFila.version).toBe(1)
    m.nuvem.voltar()
    await m.notas.subir()
    // A janela ainda acha que está na versão 1; a próxima gravação parte da 2.
    await m.notas.cliente.editarNota('n1', naFila.version, { titulo: 'Lista do domingo' })
    expect(m.nuvem.patches().at(-1)).toEqual({ titulo: 'Lista do domingo', base_version: 2, corpo: marcado, corpo_base: marcado })
  })

  it('acrescentar item vale; mudar o texto de um item, o título, a cor ou criar nota esperam a rede', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    expect(m.notas.rede.estado().semRede).toBe(true)
    const acrescentado = `${corpo}\n- [ ] manteiga`
    await expect(m.notas.cliente.editarNota('n1', 1, { corpo: acrescentado })).resolves.toEqual(expect.objectContaining({ corpo: acrescentado }))
    await expect(m.notas.cliente.editarNota('n1', 1, { corpo: corpo.replace('pão', 'pão de forma') })).rejects.toBeInstanceOf(SoComRede)
    await expect(m.notas.cliente.editarNota('n1', 1, { titulo: 'Outra' })).rejects.toBeInstanceOf(SoComRede)
    await expect(m.notas.cliente.editarNota('n1', 1, { cor: 'azul' })).rejects.toBeInstanceOf(SoComRede)
    await expect(m.notas.cliente.criarNota({ titulo: 'Nova' })).rejects.toBeInstanceOf(SoComRede)
    await expect(m.notas.cliente.moverNotaParaLixeira('n1')).rejects.toBeInstanceOf(SoComRede)
    // O que entrou na fila continua o que valeu.
    expect(m.armazem.foto().fila[0]?.corpo).toBe(acrescentado)
  })

  it('o PC marcou outro item no meio-tempo: a nuvem junta (mescla), e a nota fica com os dois', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    await m.notas.cliente.editarNota('n1', 1, { corpo: marcado })
    m.nuvem.voltar()
    const mesclado = '- [ ] ovos\n- [x] leite\n- [x] pão'
    m.nuvem.noProximoPatch((n, c) => {
      expect(c).toEqual({ base_version: 1, titulo: 'Lista do sábado', corpo: marcado, corpo_base: corpo })
      return m.nuvem.responder(200, { ...n, corpo: mesclado, version: 3, gravacao: 'mesclada' })
    })
    await m.notas.subir()
    expect(m.notas.rede.estado().esperando.size).toBe(0)
    expect((await m.notas.cliente.obterNota('n1')).version).toBeGreaterThan(0)
    expect(m.avisos).toEqual([])
  })

  it('os dois mudaram o texto: a nota ganha o selo, e o aviso diz que as duas versões estão nela', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    await m.notas.cliente.editarNota('n1', 1, { corpo: `${corpo}\n- [ ] manteiga` })
    m.nuvem.voltar()
    m.nuvem.noProximoPatch((n, c) =>
      m.nuvem.responder(200, {
        ...n,
        corpo: '- [ ] pão de forma\n- [ ] leite',
        version: 3,
        gravacao: 'no_selo',
        versao_em_conflito: { titulo: 'Lista do sábado', corpo: c.corpo, device_id: null, conflict_at: '2026-10-27T12:00:00Z' },
      }),
    )
    await m.notas.subir()
    expect(m.notas.rede.estado().esperando.size).toBe(0)
    expect(m.avisos).toEqual(['A nota “Lista do sábado” mudou em outro aparelho também: as duas versões estão nela. Abra para escolher.'])
  })

  it('a nuvem recusa (a nota foi apagada de vez): a mudança vira uma nota nova, e o aviso diz', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    await m.notas.cliente.editarNota('n1', 1, { corpo: marcado })
    m.nuvem.voltar()
    m.nuvem.noProximoPatch(() => m.nuvem.erro(404, 'NOT_FOUND', 'nota não encontrada'))
    await m.notas.subir()
    const criada = m.nuvem.pedidos.find((p) => p.metodo === 'POST' && p.caminho === '/api/v1/notes')
    expect(criada?.corpo).toEqual({ titulo: 'Lista do sábado (feita sem rede)', corpo: marcado })
    expect(m.notas.rede.estado().esperando.size).toBe(0)
    expect(m.avisos).toEqual(['Não deu para juntar a mudança feita sem rede em “Lista do sábado”: ela virou a nota “Lista do sábado (feita sem rede)”.'])
  })

  it('a resposta que se perdeu: antes de mandar de novo, a nota é lida; se ela já tem o que foi, não manda outra vez', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    await m.notas.cliente.editarNota('n1', 1, { corpo: marcado })
    m.nuvem.voltar()
    m.nuvem.noProximoPatch(() => 'aplicar-e-cair')
    await m.notas.subir()
    expect(m.notas.rede.estado()).toEqual({ semRede: true, esperando: new Set(['n1']) })
    expect(m.armazem.foto().fila[0]?.enviado).toEqual({ titulo: 'Lista do sábado', corpo: marcado })
    m.nuvem.voltar()
    await m.notas.subir()
    expect(m.nuvem.patches()).toHaveLength(1)
    expect(m.notas.rede.estado()).toEqual({ semRede: false, esperando: new Set() })
  })

  it('o que não sobe é tentado de novo depois', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    m.nuvem.cair()
    await m.notas.cliente.editarNota('n1', 1, { corpo: marcado })
    await m.notas.subir()
    expect(m.agendados.length).toBeGreaterThan(0)
    m.nuvem.voltar()
    m.agendados.at(-1)!()
    await vi.waitFor(() => expect(m.notas.rede.estado().esperando.size).toBe(0))
  })
})

describe('com rede', () => {
  it('a edição manda o conteúdo inteiro e o corpo da versão que a janela conhece (a mescla da N7)', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    const nota = await m.notas.cliente.editarNota('n1', 1, { corpo: marcado })
    expect(m.nuvem.patches()).toEqual([{ corpo: marcado, base_version: 1, titulo: 'Lista do sábado', corpo_base: corpo }])
    expect(nota.version).toBe(2)
    expect(nota.esperando).toBeUndefined()
  })

  it('a conversão de trecho é pela regra (a IA da N5 mora no Agent do PC)', async () => {
    const m = montar()
    expect(await m.notas.cliente.separarItens('leite, pão e café')).toEqual({ itens: ['Leite', 'Pão', 'Café'], como: 'regra' })
    expect(await m.notas.cliente.separarItens('leite pão café')).toEqual({ itens: [], como: 'nenhum' })
  })

  it('esquecer apaga o que a conta guardou no aparelho', async () => {
    const m = montar()
    await m.notas.cliente.listarNotas({ vista: 'ativas' })
    await m.notas.esquecer()
    expect(m.armazem.foto()).toEqual({ notas: [], marcadores: [], fila: [] })
  })
})
