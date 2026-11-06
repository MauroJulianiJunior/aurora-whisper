// "Levar o dicionário para o celular" na seção Nuvem do PC (ADR-023, F): a opção
// em cada estado (desligada, ligada, com outro PC como fonte, com aviso, com
// erro, com o pedido no ar), e o que ela faz com as Configurações de cima: gravar
// a opção sobe a version das configurações do PC, e a tela adota a nova sem
// acender a nota de "salvas em outra aba" e sem perder o que a pessoa mexeu e
// ainda não salvou. Contra o Agent de mentira (testes/servidorFalso).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { EstadoNuvem } from './clienteLocal'
import { formatarHora } from './datas'
import { type OpcoesDoServidor, type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const conectado: EstadoNuvem = { estado: 'conectado', url: 'http://localhost:8080', email: 'voce@teste.local', nome: 'DELL-G15' }
const desconectado: EstadoNuvem = { estado: 'desconectado', url: 'http://localhost:8080' }

async function abrir(opcoes: OpcoesDoServidor = {}, preparar: (srv: ReturnType<typeof servidorFalso>) => void = () => {}) {
  const itens = [ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda.' })]
  const srv = servidorFalso(itens, { nuvem: conectado, ...opcoes })
  preparar(srv)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'Nuvem' })
  await act(async () => {})
  return srv
}

const secao = () => screen.getByRole('region', { name: 'Nuvem' })
const caixa = () => within(secao()).getByRole<HTMLInputElement>('checkbox', { name: 'Levar o dicionário para o celular' })
const esperarCaixa = () => within(secao()).findByRole<HTMLInputElement>('checkbox', { name: 'Levar o dicionário para o celular' })
const ajuda = () => document.getElementById('cfg-dicionario-ajuda')!.textContent
const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string, metodo?: string) =>
  srv.pedidos.filter((p) => p.caminho === caminho && (!metodo || p.metodo === metodo))

const AJUDA = 'O celular passa a usar os termos deste PC. Editar continua aqui.'
const ENVIO = new Date(2026, 10, 5, 14, 2).toISOString()

describe('a opção, em cada estado', () => {
  it('desligada e sem outra fonte: a ajuda de sempre, e a caixa livre', async () => {
    const srv = await abrir()
    const c = await esperarCaixa()
    expect(c.checked).toBe(false)
    expect(c.disabled).toBe(false)
    expect(ajuda()).toBe(AJUDA)
    expect(c.getAttribute('aria-describedby')).toBe('cfg-dicionario-ajuda')
    // O estado é lido uma vez, quando a seção aparece (pergunta à nuvem), com a guarda do Agent.
    expect(pedidosA(srv, '/api/nuvem/dicionario', 'GET')).toHaveLength(1)
    expect(pedidosA(srv, '/api/nuvem/dicionario', 'GET')[0]!.auth).toBe('Bearer tok')
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })

  it('ligada: quantos termos e a que horas o PC os enviou', async () => {
    await abrir({ dicionarioDaNuvem: { ligado: true, fonte: 'este', termos: 49, atualizado_em: ENVIO, enviado_em: ENVIO } })
    expect((await esperarCaixa()).checked).toBe(true)
    expect(ajuda()).toBe(`49 termos, enviados às ${formatarHora(ENVIO)}.`)
  })

  it('ligada, sem a hora do envio (o Agent reiniciou): só os termos; e um termo é singular', async () => {
    await abrir({ dicionarioDaNuvem: { ligado: true, fonte: 'este', termos: 49 } })
    await esperarCaixa()
    expect(ajuda()).toBe('49 termos.')
    cleanup()
    await abrir({ dicionarioDaNuvem: { ligado: true, fonte: 'este', termos: 1, enviado_em: ENVIO } })
    await esperarCaixa()
    expect(ajuda()).toBe(`1 termo, enviados às ${formatarHora(ENVIO)}.`)
  })

  it('ligada, mas sem o número de termos (a nuvem não pôde ser perguntada): volta à ajuda de sempre', async () => {
    await abrir({ dicionarioDaNuvem: { ligado: true } })
    expect((await esperarCaixa()).checked).toBe(true)
    expect(ajuda()).toBe(AJUDA)
  })

  it('desligada, com outro PC como fonte: diz de quem é o dicionário que o celular usa', async () => {
    await abrir({ dicionarioDaNuvem: { ligado: false, fonte: 'outro', aparelho: 'NOTEBOOK', termos: 12, atualizado_em: ENVIO } })
    expect((await esperarCaixa()).checked).toBe(false)
    expect(ajuda()).toBe('Hoje o celular usa o dicionário de NOTEBOOK. Ligar aqui passa a usar o deste PC.')
  })

  it('o aviso do Agent vem sob a opção, e convive com a ajuda', async () => {
    const aviso = 'O celular passou a usar o dicionário de NOTEBOOK.'
    await abrir({ dicionarioDaNuvem: { ligado: false, fonte: 'outro', aparelho: 'NOTEBOOK', aviso } })
    await esperarCaixa()
    const nota = within(secao()).getByText(aviso)
    expect(nota.className).toBe('ajuda')
    expect(nota.getAttribute('role')).toBe('status')
    expect(ajuda()).toBe('Hoje o celular usa o dicionário de NOTEBOOK. Ligar aqui passa a usar o deste PC.')
    cleanup()
    await abrir({ dicionarioDaNuvem: { ligado: true, fonte: 'este', termos: 3, aviso: 'A cópia passa dos limites da nuvem.' } })
    await esperarCaixa()
    expect(within(secao()).getByText('A cópia passa dos limites da nuvem.')).toBeTruthy()
    expect(ajuda()).toBe('3 termos.')
  })

  it('só com o PC conectado: desconectado, nem a opção nem a pergunta à nuvem', async () => {
    const srv = await abrir({ nuvem: desconectado })
    await within(secao()).findByRole('button', { name: 'Conectar à nuvem' })
    expect(within(secao()).queryByRole('checkbox', { name: 'Levar o dicionário para o celular' })).toBeNull()
    expect(pedidosA(srv, '/api/nuvem/dicionario')).toHaveLength(0)
  })

  it('um Agent de antes da opção (404 sem frase): a opção não aparece, e nada é dito', async () => {
    const srv = await abrir({}, (s) => s.agentSemDicionario())
    await within(secao()).findByText('Conectado como', { exact: false })
    await waitFor(() => expect(pedidosA(srv, '/api/nuvem/dicionario', 'GET')).toHaveLength(1))
    await act(async () => {})
    expect(within(secao()).queryByRole('checkbox', { name: 'Levar o dicionário para o celular' })).toBeNull()
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })
})

describe('ligar e desligar', () => {
  it('ligar: manda {ligar: true}, desabilita a caixa enquanto o pedido corre, e passa a mostrar o estado novo', async () => {
    const srv = await abrir()
    const c = await esperarCaixa()
    const soltar = srv.pausar((p) => p.caminho === '/api/nuvem/dicionario' && p.metodo === 'POST')
    fireEvent.click(c)
    await waitFor(() => expect(caixa().disabled).toBe(true))
    // Enquanto o Agent não respondeu, a opção segue como estava.
    expect(caixa().checked).toBe(false)
    expect(ajuda()).toBe(AJUDA)
    soltar()
    await waitFor(() => expect(caixa().checked).toBe(true))
    expect(caixa().disabled).toBe(false)
    expect(ajuda()).toBe(`49 termos, enviados às ${formatarHora(ENVIO)}.`)
    expect(pedidosA(srv, '/api/nuvem/dicionario', 'POST').map((p) => p.corpo)).toEqual([{ ligar: true }])
    expect(srv.dicionarioAgora().ligado).toBe(true)
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })

  it('desligar: manda {ligar: false}, e a ajuda volta à de sempre', async () => {
    const srv = await abrir({ dicionarioDaNuvem: { ligado: true, fonte: 'este', termos: 49, enviado_em: ENVIO } })
    fireEvent.click(await esperarCaixa())
    await waitFor(() => expect(caixa().checked).toBe(false))
    expect(ajuda()).toBe(AJUDA)
    expect(pedidosA(srv, '/api/nuvem/dicionario', 'POST').map((p) => p.corpo)).toEqual([{ ligar: false }])
  })

  it('o foco fica na caixa depois do pedido (a caixa desabilitada o tira do teclado)', async () => {
    await abrir()
    const c = await esperarCaixa()
    c.focus()
    fireEvent.click(c)
    await waitFor(() => expect(caixa().checked).toBe(true))
    await waitFor(() => expect(document.activeElement).toBe(caixa()))
  })

  it('a frase de erro do Agent aparece junto da opção, e a opção fica como estava', async () => {
    const srv = await abrir()
    const frase = 'Não deu para falar com a nuvem agora. Tente de novo.'
    srv.recusarDicionario({ status: 502, mensagem: frase })
    fireEvent.click(await esperarCaixa())
    const alerta = await within(secao()).findByRole('alert')
    expect(alerta.textContent).toBe(frase)
    expect(caixa().checked).toBe(false)
    expect(caixa().disabled).toBe(false)
    expect(ajuda()).toBe(AJUDA)
    // Tentar de novo, já sem o problema: o erro sai e a opção liga.
    srv.recusarDicionario(null)
    fireEvent.click(caixa())
    await waitFor(() => expect(caixa().checked).toBe(true))
    expect(within(secao()).queryByRole('alert')).toBeNull()
  })

  it('desligar sem rede: a opção continua ligada, com a frase do Agent', async () => {
    const srv = await abrir({ dicionarioDaNuvem: { ligado: true, fonte: 'este', termos: 49, enviado_em: ENVIO } })
    const frase = 'Não deu para apagar a cópia da nuvem agora. Tente de novo.'
    srv.recusarDicionario({ status: 502, mensagem: frase })
    fireEvent.click(await esperarCaixa())
    expect((await within(secao()).findByRole('alert')).textContent).toBe(frase)
    expect(caixa().checked).toBe(true)
  })

  it('uma leitura que chega depois do clique não desfaz a resposta do POST', async () => {
    const srv = await abrir()
    await esperarCaixa()
    // O Agent responde ao POST com o estado novo; uma leitura velha, solta depois, é descartada.
    const soltar = srv.pausar((p) => p.caminho === '/api/nuvem/dicionario' && p.metodo === 'GET')
    document.dispatchEvent(new Event('visibilitychange'))
    await waitFor(() => expect(pedidosA(srv, '/api/nuvem/dicionario', 'GET')).toHaveLength(2))
    fireEvent.click(caixa())
    await waitFor(() => expect(caixa().checked).toBe(true))
    soltar()
    await act(async () => {})
    expect(caixa().checked).toBe(true)
    expect(ajuda()).toBe(`49 termos, enviados às ${formatarHora(ENVIO)}.`)
  })
})

describe('as Configurações de cima, depois de gravar a opção', () => {
  const bip = () => screen.getByLabelText<HTMLInputElement>('Bip ao começar a gravar')
  const salvar = () => screen.getByRole<HTMLButtonElement>('button', { name: 'Salvar configurações' })
  const puts = (srv: { pedidos: Pedido[] }) => pedidosA(srv, '/api/settings', 'PUT')
  const leituras = (srv: { pedidos: Pedido[] }) => pedidosA(srv, '/api/settings', 'GET')
  const notaDeConflito = () => screen.queryByText('As configurações foram salvas em outra aba', { exact: false })

  it('adota a version nova em silêncio: o Salvar seguinte manda a base nova, sem 409 e sem a nota de outra aba', async () => {
    const srv = await abrir()
    const antes = srv.versaoDasConfiguracoes()
    await esperarCaixa()
    const lidas = leituras(srv).length

    fireEvent.click(caixa())
    await waitFor(() => expect(caixa().checked).toBe(true))
    // A opção mora nas configurações do PC: a version subiu, e a tela a releu.
    expect(srv.versaoDasConfiguracoes()).toBe(antes + 1)
    await waitFor(() => expect(leituras(srv).length).toBe(lidas + 1))
    expect(notaDeConflito()).toBeNull()

    // Mexer em algo e salvar: a base é a version nova, e dá certo de primeira.
    fireEvent.click(bip())
    expect(salvar().getAttribute('aria-disabled')).toBeNull()
    fireEvent.click(salvar())
    await waitFor(() => expect(puts(srv)).toHaveLength(1))
    expect(puts(srv)[0]!.corpo).toMatchObject({ base_version: antes + 1 })
    await waitFor(() => expect(document.querySelector('.aviso-regiao')!.textContent).toContain('Configurações salvas.'))
    expect(notaDeConflito()).toBeNull()
    expect(screen.queryByText('Não deu para salvar', { exact: false })).toBeNull()
  })

  it('o que a pessoa já tinha mexido e não salvou continua no rascunho', async () => {
    const srv = await abrir()
    const antes = srv.versaoDasConfiguracoes()
    await esperarCaixa()
    const bipAntes = bip().checked
    fireEvent.click(bip())
    expect(bip().checked).toBe(!bipAntes)
    expect(screen.getByText('não salvo')).toBeTruthy()

    fireEvent.click(caixa())
    await waitFor(() => expect(caixa().checked).toBe(true))
    await waitFor(() => expect(srv.pedidos.filter((p) => p.caminho === '/api/settings' && p.metodo === 'GET').length).toBeGreaterThan(1))
    await act(async () => {})

    // O rascunho ficou: a caixa mexida, o "não salvo" aceso, sem nota de conflito.
    expect(bip().checked).toBe(!bipAntes)
    expect(screen.getByText('não salvo')).toBeTruthy()
    expect(notaDeConflito()).toBeNull()
    fireEvent.click(salvar())
    await waitFor(() => expect(puts(srv)).toHaveLength(1))
    expect(puts(srv)[0]!.corpo).toMatchObject({ base_version: antes + 1, beep_enabled: !bipAntes })
    await waitFor(() => expect(screen.queryByText('não salvo')).toBeNull())
    expect(notaDeConflito()).toBeNull()
  })

  it('o que outra aba mudou nos outros campos entra como base, e a mudança da pessoa vence nos dela', async () => {
    const srv = await abrir()
    await esperarCaixa()
    const bipAntes = bip().checked
    fireEvent.click(bip())
    // Outra aba muda o limiar de segurar e salva (a version sobe); a pessoa liga a opção do dicionário.
    srv.configPorFora({ hold_threshold_ms: 900 })
    fireEvent.click(caixa())
    await waitFor(() => expect(caixa().checked).toBe(true))
    await act(async () => {})
    expect(screen.getByLabelText<HTMLInputElement>('Segurar por').value).toBe('900')
    expect(bip().checked).toBe(!bipAntes)
    expect(notaDeConflito()).toBeNull()
  })

  it('uma falha ao gravar a opção não mexe nas configurações: nem leitura nova, nem version nova', async () => {
    const srv = await abrir()
    await esperarCaixa()
    const lidas = leituras(srv).length
    const versao = srv.versaoDasConfiguracoes()
    srv.recusarDicionario({ status: 409, mensagem: 'A cópia passa dos limites da nuvem.' })
    fireEvent.click(caixa())
    await within(secao()).findByRole('alert')
    await act(async () => {})
    expect(leituras(srv)).toHaveLength(lidas)
    expect(srv.versaoDasConfiguracoes()).toBe(versao)
  })
})
