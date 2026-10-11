import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { Painel } from './Painel'
import { ambienteLocal } from './ambienteLocal'
import { criarCliente } from './clienteLocal'
import { intervaloDoContador } from './mudancas'
import { type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

// A chegada mais rápida no PC (ADR-021, subfatia S): o painel à vista lê o
// contador de mudanças do sync a cada 3 s e, quando o número muda, relê a
// lista — sem a pessoa tocar em nada e sem tirar da tela o que ela edita. O
// relógio é falso (só o setInterval; o Date fica real): nenhuma espera de
// verdade, e nenhum pedido sai da máquina.

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 8, dia, hora, minuto).toISOString()

function base() {
  return [
    ditado({ id: 'd1', created_at: em(28, 14, 32), raw_text: 'Manda para o Tiago.', final_text: 'Manda para o Thiago.' }),
    ditado({ id: 'd2', created_at: em(28, 9, 5), raw_text: 'O vencimento é dia doze.', final_text: 'O vencimento é dia doze.' }),
  ]
}

const doCelular = () => ditado({ id: 'd9', created_at: em(28, 17, 50), final_text: 'Ditado que veio do celular.', raw_text: 'Ditado que veio do celular.' })

const listagens = (srv: { pedidos: Pedido[] }) => srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho === '/api/history')
const leituras = (srv: { pedidos: Pedido[] }) => srv.pedidos.filter((p) => p.caminho === '/api/nuvem/mudancas')
const linha = (texto: RegExp) => screen.findByRole('button', { name: texto })
const campoFinal = () => screen.getByLabelText<HTMLTextAreaElement>('Texto final')

function montar(srv = servidorFalso(base())) {
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  return srv
}

// passar anda o relógio do painel, uma leitura de cada vez: a versão assíncrona
// deixa a resposta de cada leitura chegar antes da rodada seguinte, como no
// navegador (a síncrona dispararia as rodadas todas juntas, e a leitura no ar
// seguraria as outras).
const passar = (ms: number) => act(async () => void (await vi.advanceTimersByTimeAsync(ms)))

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('o painel do PC relê a lista quando o sync traz algo', () => {
  it('lê o contador ao abrir e a cada 3 s, com o token', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    expect(intervaloDoContador).toBe(3000)
    expect(leituras(srv)).toHaveLength(1)
    await passar(2999)
    expect(leituras(srv)).toHaveLength(1)
    await passar(1)
    expect(leituras(srv)).toHaveLength(2)
    await passar(6000)
    expect(leituras(srv)).toHaveLength(4)
    expect(leituras(srv).every((p) => p.auth === 'Bearer tok' && p.metodo === 'GET')).toBe(true)
  })

  it('o ditado do celular aparece sozinho quando o contador sobe, e não antes', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    expect(listagens(srv)).toHaveLength(1)

    // Nada mudou: três leituras, nenhuma recarga.
    await passar(9000)
    expect(listagens(srv)).toHaveLength(1)

    // O sync traz um ditado do celular: na leitura seguinte, a lista é relida.
    srv.chegouDaNuvem(doCelular())
    await passar(3000)
    await linha(/Ditado que veio do celular/)
    expect(listagens(srv)).toHaveLength(2)
    expect(screen.getAllByRole('listitem')).toHaveLength(3)

    // Uma vez só por mudança: sem outra, não relê de novo.
    await passar(9000)
    expect(listagens(srv)).toHaveLength(2)
  })

  it('a edição feita no celular também: o texto novo aparece sem tocar em nada', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    srv.alterarPorFora('d2', 'O vencimento é dia quinze.')
    srv.mudancasPorFora()
    await passar(3000)
    await linha(/O vencimento é dia quinze/)
    expect(screen.queryByRole('button', { name: /dia doze/ })).toBeNull()
  })

  it('o ditado que foi para a lixeira no celular sai da lista', async () => {
    const srv = montar()
    await linha(/O vencimento é dia doze/)
    // A lixeira do celular e um ditado novo chegam no mesmo pull: o novo é o
    // que mostra que a lista já foi relida.
    srv.lixeiraPorFora('d2')
    srv.chegouDaNuvem(doCelular())
    await passar(3000)
    await linha(/Ditado que veio do celular/)
    expect(screen.queryByRole('button', { name: /O vencimento é dia doze/ })).toBeNull()
    expect(screen.getByRole('button', { name: /Manda para o Thiago/ })).toBeTruthy()
  })

  it('o Agent que reiniciou (o contador voltou a um número menor) também faz reler', async () => {
    const srv = servidorFalso(base())
    srv.mudancasPorFora()
    srv.mudancasPorFora()
    montar(srv)
    await linha(/Manda para o Thiago/)
    expect(srv.contadorAgora()).toBe(2)
    // O Agent reinicia (o contador volta a 0) e, depois, o sync traz um ditado
    // (1): menor que o 2 que o painel leu, e diferente.
    srv.reiniciarContador()
    srv.chegouDaNuvem(doCelular())
    expect(srv.contadorAgora()).toBe(1)
    await passar(3000)
    await linha(/Ditado que veio do celular/)
    expect(listagens(srv)).toHaveLength(2)
  })

  it('não perde a edição em andamento: o texto digitado fica, com o editor aberto', async () => {
    const srv = montar()
    fireEvent.click(await linha(/Manda para o Thiago/))
    fireEvent.change(campoFinal(), { target: { value: 'Minha edição, ainda sem salvar.' } })

    // Chegam, juntos, um ditado novo, a edição de outro e uma mudança no
    // MESMO ditado que está sendo editado.
    srv.chegouDaNuvem(doCelular())
    srv.alterarPorFora('d2', 'O vencimento é dia quinze.')
    srv.alterarPorFora('d1', 'Texto que a nuvem trouxe para o mesmo ditado.')
    await passar(3000)

    await linha(/Ditado que veio do celular/)
    await linha(/O vencimento é dia quinze/)
    expect(listagens(srv)).toHaveLength(2)
    expect(campoFinal().value).toBe('Minha edição, ainda sem salvar.')
    // O editor continua aberto, e salvar cai no conflito de sempre (a base é a
    // velha), sem perder o texto.
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    expect((await screen.findByRole('alert')).textContent).toContain('alterado em outra aba')
    expect(campoFinal().value).toBe('Minha edição, ainda sem salvar.')
  })

  it('mantém as páginas já carregadas e a busca', async () => {
    const muitos = Array.from({ length: 5 }, (_, i) =>
      ditado({ id: `p${i + 1}`, created_at: em(28, 15 - i), final_text: `Ditado número ${i + 1}.` }),
    )
    const srv = servidorFalso(muitos, { porPagina: 2 })
    montar(srv)
    await linha(/Ditado número 1\./)
    fireEvent.click(screen.getByRole('button', { name: 'Mostrar mais antigos' }))
    await linha(/Ditado número 3\./)

    srv.chegouDaNuvem(doCelular())
    await passar(3000)
    await linha(/Ditado que veio do celular/)
    // As páginas que a pessoa já tinha aberto continuam.
    expect(screen.getByRole('button', { name: /Ditado número 3\./ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Ditado número 4\./ })).toBeTruthy()
  })
})

describe('a leitura do contador só acontece com o painel à vista', () => {
  it('escondido, o painel não lê; ao voltar, lê já', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    expect(leituras(srv)).toHaveLength(1)

    const visibilidade = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    await passar(12000)
    expect(leituras(srv)).toHaveLength(1)

    visibilidade.mockReturnValue('visible')
    await act(async () => void document.dispatchEvent(new Event('visibilitychange')))
    expect(leituras(srv)).toHaveLength(2)
  })

  it('o contador que andou com a aba escondida faz reler ao voltar', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    const visibilidade = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    srv.chegouDaNuvem(doCelular())
    await passar(9000)
    visibilidade.mockReturnValue('visible')
    await act(async () => void document.dispatchEvent(new Event('visibilitychange')))
    await linha(/Ditado que veio do celular/)
  })

  it('duas leituras nunca ficam no ar ao mesmo tempo', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    const soltar = srv.pausar((p) => p.caminho === '/api/nuvem/mudancas')
    await passar(3000) // a leitura de agora fica presa
    expect(leituras(srv)).toHaveLength(2)
    await passar(12000) // e as rodadas seguintes esperam por ela
    expect(leituras(srv)).toHaveLength(2)
    soltar()
    await passar(3000)
    expect(leituras(srv)).toHaveLength(3)
  })

  it('ao fechar o painel, a leitura para', async () => {
    const srv = montar()
    await linha(/Manda para o Thiago/)
    cleanup()
    const antes = leituras(srv).length
    await passar(12000)
    expect(leituras(srv)).toHaveLength(antes)
  })
})

describe('quando a leitura não dá', () => {
  it('um Agent de antes da S (404) não é perguntado de novo, e o painel segue igual', async () => {
    const srv = servidorFalso(base())
    srv.agentSemContador()
    montar(srv)
    await linha(/Manda para o Thiago/)
    expect(leituras(srv)).toHaveLength(1)
    await passar(15000)
    expect(leituras(srv)).toHaveLength(1)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('a leitura que falha (o Agent fora do ar) não acende faixa nem alerta, e a seguinte tenta de novo', async () => {
    const srv = servidorFalso(base())
    let falhar = true
    srv.interceptar((p) => (falhar && p.caminho === '/api/nuvem/mudancas' ? new Response('erro', { status: 500 }) : undefined))
    montar(srv)
    await linha(/Manda para o Thiago/)
    expect(leituras(srv)).toHaveLength(1)
    expect(screen.queryByRole('alert')).toBeNull()

    falhar = false
    await passar(3000) // o ponto de partida, agora lido
    srv.chegouDaNuvem(doCelular())
    await passar(3000)
    await linha(/Ditado que veio do celular/)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('um corpo que não é o contador não vale como mudança', async () => {
    const srv = servidorFalso(base())
    srv.interceptar((p) => (p.caminho === '/api/nuvem/mudancas' ? new Response(JSON.stringify({ outra: 1 }), { status: 200 }) : undefined))
    montar(srv)
    await linha(/Manda para o Thiago/)
    await passar(9000)
    expect(listagens(srv)).toHaveLength(1)
  })
})

describe('na nuvem não há contador', () => {
  it('sem mudancasDoSync no ambiente, o painel não pergunta nada ao Agent', async () => {
    const srv = servidorFalso(base())
    const ambiente = { ...ambienteLocal(criarCliente('tok', srv.buscar)), mudancasDoSync: undefined }
    render(<Painel ambiente={ambiente} />)
    await linha(/Manda para o Thiago/)
    await passar(15000)
    expect(leituras(srv)).toHaveLength(0)
  })
})

describe('o cliente local', () => {
  it('lê o contador pelo GET, com o token, e devolve o número', async () => {
    const srv = servidorFalso(base())
    srv.mudancasPorFora()
    srv.mudancasPorFora()
    const cliente = criarCliente('tok', srv.buscar)
    expect(await cliente.mudancas()).toBe(2)
    const [p] = leituras(srv)
    expect(p?.metodo).toBe('GET')
    expect(p?.auth).toBe('Bearer tok')
    expect(p?.corpo).toBeUndefined()
    expect(p?.busca.size).toBe(0)
  })

  it('recusa o corpo que não é um número', async () => {
    const srv = servidorFalso(base())
    for (const corpo of [{}, { contador: 'sete' }, { contador: null }]) {
      srv.interceptar((p) => (p.caminho === '/api/nuvem/mudancas' ? new Response(JSON.stringify(corpo), { status: 200 }) : undefined))
      await expect(criarCliente('tok', srv.buscar).mudancas()).rejects.toThrow('contador')
    }
  })
})
