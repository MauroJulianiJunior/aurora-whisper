// Aprender com as edições do painel (ADR-017) contra o Agent de mentira
// (testes/servidorFalso.ts): o aviso que pergunta, o clique que grava, o
// Desfazer, o erro, e a lista "Aprendido no painel" das Configurações.
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos
// sai, com que corpo, e o que a pessoa vê. O que o Agent decide (filtros,
// versão da sugestão) é dos testes Go de internal/agent/localapi.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { type Sugestao } from './api'
import { type OpcoesDoServidor, type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

const em = (dia: number, hora: number) => new Date(2026, 8, dia, hora).toISOString()

const bruto = 'vou mandar o sf amanhã'
const SF: Sugestao = { de: 'sf', para: 'ECF', termo_existente: 'ECF', ocorrencias: 0 }
const RJ: Sugestao = { de: 'rj', para: 'RF', termo_existente: '', ocorrencias: 0 }

function montar(sugestoes: Sugestao[] = [SF], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso(
    [
      ditado({ id: 'd1', created_at: em(28, 14), raw_text: bruto, final_text: 'Vou mandar o sf amanhã.' }),
      ditado({ id: 'd2', created_at: em(28, 9), raw_text: 'outro ditado', final_text: 'Outro ditado.' }),
    ],
    opcoes,
  )
  srv.sugerirNoSalvar(sugestoes)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  return srv
}

const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const campoFinal = () => screen.getByLabelText<HTMLTextAreaElement>('Texto final')
const doAviso = (nome: string) => within(aviso()).getByRole<HTMLButtonElement>('button', { name: nome })
const semNoAviso = (nome: string) => within(aviso()).queryByRole('button', { name: nome })
const aoAprender = (srv: { pedidos: Pedido[] }) => srv.pedidos.filter((p) => p.metodo === 'POST' && p.caminho === '/api/aprendizado')
const aoDesfazer = (srv: { pedidos: Pedido[] }) => srv.pedidos.filter((p) => p.metodo === 'POST' && p.caminho === '/api/aprendizado/desfazer')
const pares = (srv: { aprendidas: { termo: string; variante: string }[] }) => srv.aprendidas.map((a) => `${a.variante}>${a.termo}`)
const texto = () => aviso().textContent

// editarESalvar é o que a pessoa faz: abre a linha, corrige e salva.
async function editarESalvar(novo = 'Vou mandar o ECF amanhã.') {
  fireEvent.click(await screen.findByRole('button', { name: /Vou mandar o sf amanhã/ }))
  fireEvent.change(campoFinal(), { target: { value: novo } })
  fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
  await waitFor(() => expect(texto()).toContain('Texto salvo.'))
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 8, 28, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('o aviso da sugestão', () => {
  it('pergunta depois de salvar e não chama o aprendizado sem o clique (AC-022)', async () => {
    const srv = montar()
    await editarESalvar()

    expect(texto()).toContain('Texto salvo. Aprender “sf” → vira “ECF”?')
    expect(doAviso('Aprender')).toBeTruthy()
    // Salvar foi só o PATCH: nada foi ao aprendizado, e o dicionário está como estava.
    expect(aoAprender(srv)).toHaveLength(0)
    expect(aoDesfazer(srv)).toHaveLength(0)
    expect(srv.aprendidas).toEqual([])
  })

  it('os termos vão na mono, e a pílula diz o efeito e o atalho a quem pergunta', async () => {
    montar()
    await editarESalvar()

    expect(Array.from(aviso().querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['sf', 'ECF'])
    const aprender = doAviso('Aprender')
    expect(aprender.getAttribute('title')).toMatch(/todo ditado daqui para frente, em qualquer aplicativo/)
    expect(aprender.getAttribute('aria-keyshortcuts')).toBe('Alt+A')
    expect(within(aviso()).getByText('Alt+A')).toBeTruthy()
    // A região viva em que o leitor de tela ouve a pergunta existe desde o começo.
    expect(aviso().getAttribute('role')).toBe('status')
    expect(aviso().getAttribute('aria-live')).toBe('polite')
  })

  it('sem sugestão, é só "Texto salvo.": nenhum botão, nenhuma chamada', async () => {
    const srv = montar([])
    await editarESalvar()

    expect(texto()).toBe('Texto salvo.')
    expect(semNoAviso('Aprender')).toBeNull()
    expect(aoAprender(srv)).toHaveLength(0)
  })

  it('um Agent anterior à etapa 3, que nem manda o campo, continua salvando como antes', async () => {
    const srv = montar([])
    srv.interceptar((p) => {
      if (p.metodo !== 'PATCH') return undefined
      const d = srv.itens[0]!
      d.final_text = String(p.corpo?.final_text)
      d.version += 1
      return new Response(JSON.stringify(d), { status: 200, headers: { 'Content-Type': 'application/json' } })
    })
    await editarESalvar()
    expect(texto()).toBe('Texto salvo.')
  })

  it('o conflito de versão não oferece aprender', async () => {
    const srv = montar()
    fireEvent.click(await screen.findByRole('button', { name: /Vou mandar o sf amanhã/ }))
    fireEvent.change(campoFinal(), { target: { value: 'Vou mandar o ECF amanhã.' } })
    srv.alterarPorFora('d1', 'Da outra aba.')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))

    expect((await screen.findByRole('alert')).textContent).toContain('alterado em outra aba')
    expect(semNoAviso('Aprender')).toBeNull()
    expect(texto()).not.toContain('Aprender')
  })
})

describe('"aparece em N ditados"', () => {
  it.each([
    [[{ ...SF, ocorrencias: 3 }], 'Aprender “sf” → vira “ECF”? “sf” aparece em 3 ditados.'],
    [[{ ...SF, ocorrencias: 1 }], 'Aprender “sf” → vira “ECF”? “sf” aparece em 1 ditado.'],
    [[{ ...SF, ocorrencias: 0 }], 'Aprender “sf” → vira “ECF”?'],
    [[{ ...SF, ocorrencias: 3 }, RJ], 'Aprender “sf” → vira “ECF” e “rj” → vira “RF”? “sf” aparece em 3 ditados.'],
    [[SF, { ...RJ, ocorrencias: 2 }], 'Aprender “sf” → vira “ECF” e “rj” → vira “RF”? “rj” aparece em 2 ditados.'],
    [[{ ...SF, ocorrencias: 3 }, { ...RJ, ocorrencias: 1 }], 'Aprender “sf” → vira “ECF” e “rj” → vira “RF”? “sf” aparece em 3 ditados, “rj” em 1.'],
  ] as [Sugestao[], string][])('%j', async (sugestoes, frase) => {
    montar(sugestoes)
    await editarESalvar()
    expect(texto()).toContain(`Texto salvo. ${frase}`)
  })

  it('a nota é discreta: fica na cor secundária da pílula, à parte do texto', async () => {
    montar([{ ...SF, ocorrencias: 3 }])
    await editarESalvar()
    expect(aviso().querySelector('.aviso-nota')?.textContent).toBe('“sf” aparece em 3 ditados.')
  })
})

describe('Aprender', () => {
  it('o clique chama o endereço certo, com o ditado e o par no corpo, e o aviso vira "Aprendido"', async () => {
    const srv = montar()
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Aprendido: “sf” vira “ECF” no próximo ditado.'))
    const [pedido] = aoAprender(srv)
    expect(aoAprender(srv)).toHaveLength(1)
    expect(pedido?.corpo).toEqual({ id: 'd1', de: 'sf', para: 'ECF' })
    expect(pedido?.auth).toBe('Bearer tok')
    // O texto ditado vai no corpo, nunca na URL.
    expect(pedido?.busca.toString()).toBe('')
    expect(pares(srv)).toEqual(['sf>ECF'])
    // O Desfazer, e o verde do que foi confirmado.
    expect(doAviso('Desfazer')).toBeTruthy()
    expect(aviso().querySelector('.confirmado')?.textContent).toContain('Aprendido:')
    expect(semNoAviso('Aprender')).toBeNull()
  })

  it('duas trocas: um clique aprende as duas, na ordem do texto', async () => {
    const srv = montar([SF, RJ])
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Aprendido: “sf” vira “ECF” e “rj” vira “RF” no próximo ditado.'))
    expect(aoAprender(srv).map((p) => p.corpo)).toEqual([
      { id: 'd1', de: 'sf', para: 'ECF' },
      { id: 'd1', de: 'rj', para: 'RF' },
    ])
    expect(pares(srv)).toEqual(['sf>ECF', 'rj>RF'])
  })

  it('o Agent recusa (variante de outro termo): a frase dele no aviso, nada aprendido, sem Desfazer', async () => {
    const srv = montar()
    srv.recusarAprender('sf', 409, '"sf" já vira "ECD" no dicionário.')
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Não deu para aprender.'))
    expect(texto()).toBe('Não deu para aprender. "sf" já vira "ECD" no dicionário.')
    expect(pares(srv)).toEqual([])
    expect(semNoAviso('Desfazer')).toBeNull()
    expect(semNoAviso('Aprender')).toBeNull()
    // O 409 do aprendizado não é o conflito de versão: nenhuma faixa, nenhuma releitura.
    expect(document.querySelector('.faixa')).toBeNull()
  })

  it('uma troca vale e a outra é recusada: o aviso diz as duas coisas, e o Desfazer é só da que valeu', async () => {
    const srv = montar([SF, RJ])
    srv.recusarAprender('rj', 409, '"rj" já vira "RZ" no dicionário.')
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Aprendido: “sf” vira “ECF” no próximo ditado. "rj" já vira "RZ" no dicionário.'))
    expect(pares(srv)).toEqual(['sf>ECF'])

    fireEvent.click(doAviso('Desfazer'))
    await waitFor(() => expect(texto()).toContain('Desfeito: “sf” não vira mais “ECF”.'))
    expect(aoDesfazer(srv).map((p) => p.corpo)).toEqual([{ variante: 'sf' }])
    expect(pares(srv)).toEqual([])
  })

  it('uma sugestão que venceu (409 do Agent) diz a verdade: o ditado mudou, e é corrigir de novo', async () => {
    const srv = montar()
    await editarESalvar()
    // O texto mudou por fora depois da sugestão: o Agent não a reconhece.
    srv.alterarPorFora('d1', 'Outro texto.')
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Essa sugestão não vale mais; corrija o ditado de novo para aprender.'))
    // Não manda "salvar de novo": salvar o mesmo texto não devolve a sugestão.
    expect(texto()).not.toContain('Salve a edição de novo')
    expect(pares(srv)).toEqual([])
  })

  it('o 500 do Agent vai inteiro para a pílula, com a frase dele (sem "erro interno" cru)', async () => {
    const srv = montar()
    srv.recusarAprender('sf', 500, 'Deu um erro dentro do Agent. Tente de novo.')
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toBe('Não deu para aprender. Deu um erro dentro do Agent. Tente de novo.'))
    expect(pares(srv)).toEqual([])
  })

  it('o Agent grava mas não consegue trocar o dicionário em memória: aprendeu, vale depois de reiniciar, e dá para desfazer', async () => {
    const srv = montar()
    await editarESalvar()
    srv.interceptar((p) =>
      p.caminho === '/api/aprendizado' && p.metodo === 'POST'
        ? new Response(JSON.stringify({ termo: 'ECF', variante: 'sf', vale_ao_reiniciar: true }), { status: 200 })
        : undefined,
    )
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Aprendido: “sf” vira “ECF”, mas só depois de reiniciar o Agent.'))
    // Nada de "não deu para aprender" nem do "próximo ditado", que seria falso.
    expect(texto()).not.toContain('Não deu')
    expect(texto()).not.toContain('próximo ditado')
    expect(doAviso('Desfazer')).toBeTruthy()

    // O Desfazer também pode ficar para o reinício, e a pílula diz.
    srv.aprendidas.push({ termo: 'ECF', variante: 'sf' })
    srv.interceptar((p) =>
      p.caminho === '/api/aprendizado/desfazer'
        ? new Response(JSON.stringify({ termo: 'ECF', variante: 'sf', vale_ao_reiniciar: true }), { status: 200 })
        : undefined,
    )
    fireEvent.click(doAviso('Desfazer'))
    await waitFor(() => expect(texto()).toContain('Desfeito: “sf” não vira mais “ECF”, mas só depois de reiniciar o Agent.'))
  })

  // Duas trocas do mesmo clique podem divergir. A ressalva é de quem a tem, e
  // não das duas: "só depois de reiniciar" seria falso para a que já vale. Nas
  // duas ordens, para a regra não depender de qual é a primeira.
  it.each([
    {
      reinicia: 'rj',
      aprendido: 'Aprendido: “sf” vira “ECF” no próximo ditado; “rj” vira “RF”, mas só depois de reiniciar o Agent.',
      desfeito: 'Desfeito: “sf” não vira mais “ECF”; “rj” não vira mais “RF”, mas só depois de reiniciar o Agent.',
    },
    {
      reinicia: 'sf',
      aprendido: 'Aprendido: “rj” vira “RF” no próximo ditado; “sf” vira “ECF”, mas só depois de reiniciar o Agent.',
      desfeito: 'Desfeito: “rj” não vira mais “RF”; “sf” não vira mais “ECF”, mas só depois de reiniciar o Agent.',
    },
  ])('duas trocas e só a de "$reinicia" ficou para o reinício: a ressalva é só dela, ao aprender e ao desfazer', async ({ reinicia, aprendido, desfeito }) => {
    const srv = montar([SF, RJ])
    await editarESalvar()
    const par = reinicia === 'sf' ? SF : RJ
    srv.interceptar((p) =>
      p.metodo === 'POST' && (p.corpo?.de === reinicia || p.corpo?.variante === reinicia)
        ? new Response(JSON.stringify({ termo: par.para, variante: par.de, vale_ao_reiniciar: true }), { status: 200 })
        : undefined,
    )
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain(aprendido))
    // Cada ressalva aparece uma vez só.
    expect(texto()?.match(/reiniciar/g)).toHaveLength(1)
    expect(texto()?.match(/próximo ditado/g)).toHaveLength(1)

    fireEvent.click(doAviso('Desfazer'))
    await waitFor(() => expect(texto()).toContain(desfeito))
    expect(texto()?.match(/reiniciar/g)).toHaveLength(1)
  })

  it('a pílula da sugestão esconde a seta do leitor de tela e diz "vira": a mesma troca das Configurações', async () => {
    montar([SF, RJ])
    await editarESalvar()

    const setas = Array.from(aviso().querySelectorAll('[aria-hidden="true"]'))
    expect(setas.map((s) => s.textContent)).toEqual(['→', '→'])
    expect(Array.from(aviso().querySelectorAll('.so-leitor'), (s) => s.textContent)).toEqual([' vira', ' vira'])
    // A seta nunca é lida; o texto que sobra para o leitor é "sf vira ECF e rj vira RF".
    const lido = aviso().cloneNode(true) as HTMLElement
    lido.querySelectorAll('[aria-hidden="true"]').forEach((e) => e.remove())
    expect(lido.textContent?.replace(/\s+/g, ' ')).toContain('Aprender “sf” vira “ECF” e “rj” vira “RF”?')
  })

  it('sem sessão no meio do clique: a faixa do topo, e nenhuma frase enganosa no aviso', async () => {
    const srv = montar()
    await editarESalvar()
    srv.interceptar((p) => (p.caminho === '/api/aprendizado' ? new Response('', { status: 401 }) : undefined))
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(document.querySelector('.faixa')).toBeTruthy())
    expect(texto()).not.toContain('Aprendido')
    expect(pares(srv)).toEqual([])
  })

  it('dois cliques seguidos pedem uma vez só', async () => {
    const srv = montar()
    await editarESalvar()
    const botao = doAviso('Aprender')
    fireEvent.click(botao)
    fireEvent.click(botao)

    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    expect(aoAprender(srv)).toHaveLength(1)
  })

  it('o foco de quem estava no botão vai para o TEXTO do resultado: nunca para o Desfazer, que um Enter a mais desfaria', async () => {
    const srv = montar()
    await editarESalvar()
    act(() => doAviso('Aprender').focus())
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    // O foco vai com o aviso novo: espera por ele, em vez de olhar no instante
    // em que o texto aparece.
    await waitFor(() => expect(document.activeElement).toBe(aviso().querySelector('.aviso-texto')))
    const foco = document.activeElement as HTMLElement
    // Não cai no início da página, e não está sobre a ação.
    expect(foco).not.toBe(document.body)
    expect(foco).not.toBe(doAviso('Desfazer'))
    expect(foco.tagName).not.toBe('BUTTON')
    expect(foco).toBe(aviso().querySelector('.aviso-texto'))
    // Focável por código, mas não é uma parada do Tab.
    expect(foco.getAttribute('tabindex')).toBe('-1')
    // E nada foi desfeito.
    await act(async () => {})
    expect(aoDesfazer(srv)).toHaveLength(0)
    expect(pares(srv)).toEqual(['sf>ECF'])
  })

  it('o foco que não estava na pílula não é puxado para ela', async () => {
    montar()
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    expect(aviso().contains(document.activeElement)).toBe(false)
  })

  it('a ligação cai no meio das trocas: as outras nem são pedidas, e a pílula não diz que não deu', async () => {
    const srv = montar([SF, RJ])
    await editarESalvar()
    srv.interceptar((p) => (p.caminho === '/api/aprendizado' && p.metodo === 'POST' ? new Response('', { status: 401 }) : undefined))
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(document.querySelector('.faixa')).toBeTruthy())
    // Só a primeira troca saiu: as outras também não dariam certo.
    expect(aoAprender(srv)).toHaveLength(1)
    // A faixa do topo diz o que fazer; a pílula não a contradiz com um "Não deu".
    expect(texto()).not.toContain('Não deu para aprender')
    expect(texto()).not.toContain('Aprendido')
    expect(texto()).toContain('Texto salvo. Aprender')
  })
})

describe('Alt+A', () => {
  const alt = (init: KeyboardEventInit = {}, alvo: Element | Document | Window = document.body) =>
    fireEvent.keyDown(alvo, { key: 'a', altKey: true, ...init })

  it('aprende pelo teclado, sem precisar chegar ao botão', async () => {
    const srv = montar()
    await editarESalvar()
    alt()

    await waitFor(() => expect(texto()).toContain('Aprendido: “sf” vira “ECF”'))
    expect(aoAprender(srv)).toHaveLength(1)
  })

  it.each([
    ['Ctrl+Alt+A (o AltGr dos teclados que o têm)', { ctrlKey: true }],
    ['Alt+Shift+A', { shiftKey: true }],
    ['a tecla segurada (repeat)', { repeat: true }],
    ['Alt+B', { key: 'b' }],
    ['A sem o Alt', { altKey: false }],
  ] as [string, KeyboardEventInit][])('%s não aprende', async (_, init) => {
    const srv = montar()
    await editarESalvar()
    alt(init)
    await act(async () => {})

    expect(aoAprender(srv)).toHaveLength(0)
    expect(doAviso('Aprender')).toBeTruthy()
  })

  it('dentro de um campo de texto não aprende', async () => {
    const srv = montar()
    await editarESalvar()
    fireEvent.click(await screen.findByRole('button', { name: /Vou mandar o ECF amanhã/ }))
    alt({}, campoFinal())
    await act(async () => {})

    expect(aoAprender(srv)).toHaveLength(0)
  })

  it('sem o aviso da sugestão, não faz nada', async () => {
    const srv = montar([])
    await editarESalvar()
    alt()
    await act(async () => {})

    expect(aoAprender(srv)).toHaveLength(0)
  })

  it('Alt+A e o botão juntos pedem uma vez só', async () => {
    const srv = montar()
    await editarESalvar()
    alt()
    fireEvent.click(doAviso('Aprender'))

    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    expect(aoAprender(srv)).toHaveLength(1)
  })
})

describe('Desfazer logo depois de aprender', () => {
  it('chama o endereço de desfazer, com a variante no corpo e fora da URL', async () => {
    const srv = montar()
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    fireEvent.click(doAviso('Desfazer'))

    await waitFor(() => expect(texto()).toContain('Desfeito: “sf” não vira mais “ECF”.'))
    const [pedido] = aoDesfazer(srv)
    expect(aoDesfazer(srv)).toHaveLength(1)
    expect(pedido?.corpo).toEqual({ variante: 'sf' })
    expect(pedido?.busca.toString()).toBe('')
    expect(pedido?.caminho).toBe('/api/aprendizado/desfazer')
    expect(pares(srv)).toEqual([])
  })

  it('Ctrl+Z, fora de campo, faz o mesmo', async () => {
    const srv = montar()
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })

    await waitFor(() => expect(texto()).toContain('Desfeito:'))
    expect(pares(srv)).toEqual([])
  })

  it('desfazer duas trocas tira as duas', async () => {
    const srv = montar([SF, RJ])
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    fireEvent.click(doAviso('Desfazer'))

    await waitFor(() => expect(texto()).toContain('Desfeito: “sf” não vira mais “ECF” e “rj” não vira mais “RF”.'))
    expect(aoDesfazer(srv).map((p) => p.corpo)).toEqual([{ variante: 'sf' }, { variante: 'rj' }])
  })

  it('o Agent diz que não há o que desfazer (outra aba já desfez): a frase dele, sem fingir que desfez', async () => {
    const srv = montar()
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    srv.aprendidas.length = 0
    fireEvent.click(doAviso('Desfazer'))

    await waitFor(() => expect(texto()).toContain('Não deu para desfazer.'))
    expect(texto()).toContain('"sf" não foi aprendido pelo painel.')
    expect(texto()).not.toContain('Desfeito')
  })
})

describe('Aprendido no painel, nas Configurações', () => {
  const aprendidas = [
    { termo: 'ECF', variante: 'sf' },
    { termo: 'Wispr Flow', variante: 'whisperflow' },
  ]

  async function abrirConfiguracoes(opcoes: OpcoesDoServidor = {}, sugestoes: Sugestao[] = []) {
    const srv = montar(sugestoes, opcoes)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Ditado' })
    return srv
  }
  const secao = () => screen.queryByRole('region', { name: 'Aprendido no painel' })
  const linhas = () => within(secao()!).getAllByRole('listitem')

  it('só aparece se houver algo aprendido', async () => {
    await abrirConfiguracoes()
    await act(async () => {})
    expect(secao()).toBeNull()
    expect(screen.queryByText('Aprendido no painel')).toBeNull()
  })

  it('uma linha por par, com os termos na mono e um Desfazer em cada', async () => {
    await abrirConfiguracoes({ aprendidas })
    await screen.findByRole('heading', { name: 'Aprendido no painel' })

    expect(linhas()).toHaveLength(2)
    expect(Array.from(linhas()[0]!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['sf', 'ECF'])
    expect(Array.from(linhas()[1]!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['whisperflow', 'Wispr Flow'])
    expect(within(linhas()[0]!).getByRole('button', { name: 'Desfazer: sf vira ECF' })).toBeTruthy()
    // O leitor de tela ouve "sf vira ECF"; a seta é só para os olhos.
    expect(linhas()[0]!.textContent).toContain('sf → vira ECF')
  })

  it('Desfazer tira a linha, chama o endereço certo e avisa', async () => {
    const srv = await abrirConfiguracoes({ aprendidas })
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    fireEvent.click(within(linhas()[0]!).getByRole('button', { name: 'Desfazer: sf vira ECF' }))

    await waitFor(() => expect(linhas()).toHaveLength(1))
    expect(aoDesfazer(srv).map((p) => p.corpo)).toEqual([{ variante: 'sf' }])
    expect(pares(srv)).toEqual(['whisperflow>Wispr Flow'])
    expect(texto()).toContain('Desfeito: “sf” não vira mais “ECF”.')
    // O foco fica na lista, na linha que ocupou o lugar. Vem de um efeito, que
    // roda depois de a linha sair da página: espera.
    await waitFor(() => expect(document.activeElement).toBe(within(linhas()[0]!).getByRole('button')))
  })

  it('desfazer a última some com a seção, e o foco vai para o Salvar configurações', async () => {
    await abrirConfiguracoes({ aprendidas: [aprendidas[0]!] })
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    fireEvent.click(screen.getByRole('button', { name: 'Desfazer: sf vira ECF' }))

    await waitFor(() => expect(secao()).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Salvar configurações' })))
  })

  it('o Desfazer que o Agent não reconhece (outra aba já desfez) mostra a frase dele, e a linha fica', async () => {
    const srv = await abrirConfiguracoes({ aprendidas })
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    srv.aprendidas.length = 0
    fireEvent.click(screen.getByRole('button', { name: 'Desfazer: sf vira ECF' }))

    const alerta = await within(secao()!).findByRole('alert')
    expect(alerta.textContent).toBe('Não deu para desfazer: "sf" não foi aprendido pelo painel.')
    expect(linhas()).toHaveLength(2)
  })

  it('o que o aviso do histórico aprendeu aparece na lista, mesmo com a tela já aberta antes', async () => {
    const srv = await abrirConfiguracoes({}, [SF])
    await act(async () => {})
    expect(secao()).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))

    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(pares(srv)).toEqual(['sf>ECF'])
    expect(Array.from(linhas()[0]!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['sf', 'ECF'])
  })

  it('e o que o aviso desfez sai da lista', async () => {
    const srv = await abrirConfiguracoes({}, [SF])
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))

    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    // A pílula continua à vista nas Configurações: o Desfazer dela age no mesmo dicionário.
    fireEvent.click(doAviso('Desfazer'))

    await waitFor(() => expect(secao()).toBeNull())
    expect(pares(srv)).toEqual([])
  })

  it('e o que o aviso aprende com as Configurações à vista entra na lista na hora', async () => {
    const srv = await abrirConfiguracoes({}, [SF])
    // A sugestão nasce no histórico; a pessoa vai às Configurações com a pílula ainda de pé.
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await editarESalvar()
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await act(async () => {})
    expect(secao()).toBeNull()

    fireEvent.click(doAviso('Aprender'))
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(pares(srv)).toEqual(['sf>ECF'])
  })

  it('a lista só é lida com a tela à vista: escondida, o que o aviso faz não gera leitura', async () => {
    const srv = await abrirConfiguracoes({}, [SF])
    await act(async () => {})
    const leituras = () => srv.pedidos.filter((p) => p.metodo === 'GET' && p.caminho === '/api/aprendizado').length
    expect(leituras()).toBe(1)

    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    expect(leituras()).toBe(1)

    // Ao voltar, lê uma vez, e já mostra o que foi aprendido.
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(leituras()).toBe(2)
  })

  it('uma lista null do Agent é uma lista vazia: a seção não aparece e nada quebra', async () => {
    const srv = montar([])
    srv.interceptar((p) => (p.caminho === '/api/aprendizado' && p.metodo === 'GET' ? new Response('{"items":null}', { status: 200 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Ditado' })
    await act(async () => {})

    expect(secao()).toBeNull()
    expect(screen.getByRole('heading', { name: 'Privacidade' })).toBeTruthy()
  })

  it('a lista que não carrega por ligação perdida acende a faixa do topo, e a seção não aparece', async () => {
    const srv = montar([])
    srv.interceptar((p) => (p.caminho === '/api/aprendizado' && p.metodo === 'GET' ? new Response('', { status: 401 }) : undefined))
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Ditado' })

    await waitFor(() => expect(document.querySelector('.faixa')).toBeTruthy())
    expect(secao()).toBeNull()
  })

  it('um erro da lista que não é da ligação (o Agent diz 503) não derruba a tela nem acende a faixa', async () => {
    const srv = montar([])
    srv.interceptar((p) =>
      p.caminho === '/api/aprendizado' && p.metodo === 'GET'
        ? new Response(JSON.stringify({ error: { code: 'UNAVAILABLE', message: 'O dicionário do Agent não abre. Nada foi gravado.' } }), { status: 503 })
        : undefined,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Ditado' })
    await act(async () => {})

    expect(document.querySelector('.faixa')).toBeNull()
    expect(secao()).toBeNull()
    expect(screen.getByRole('heading', { name: 'Privacidade' })).toBeTruthy()
  })

  it('dois cliques no Desfazer da mesma linha pedem uma vez só', async () => {
    const srv = await abrirConfiguracoes({ aprendidas })
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    const botao = screen.getByRole('button', { name: 'Desfazer: sf vira ECF' })
    fireEvent.click(botao)
    fireEvent.click(botao)

    await waitFor(() => expect(linhas()).toHaveLength(1))
    expect(aoDesfazer(srv)).toHaveLength(1)
    expect(screen.queryByRole('alert')).toBeNull()
  })
})

describe('o Agent responde 409 ao desfazer', () => {
  it('é a frase dele, e não a nota de "alterado em outra aba"', async () => {
    const srv = montar()
    await editarESalvar()
    fireEvent.click(doAviso('Aprender'))
    await waitFor(() => expect(texto()).toContain('Aprendido:'))
    srv.interceptar((p) =>
      p.caminho === '/api/aprendizado/desfazer'
        ? new Response(JSON.stringify({ error: { code: 'CONFLICT', message: 'O dicionário está em uso. Tente de novo.' } }), { status: 409 })
        : undefined,
    )
    fireEvent.click(doAviso('Desfazer'))

    await waitFor(() => expect(texto()).toContain('Não deu para desfazer. O dicionário está em uso. Tente de novo.'))
  })
})
