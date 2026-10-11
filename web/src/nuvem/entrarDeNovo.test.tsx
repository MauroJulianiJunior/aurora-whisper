// A sessão que acaba com um texto não salvo na tela (ADR-019, "Decisões da
// F4"; decisão do Mauro, 05-out-2026: "Guarda até entrar de novo"), contra a
// nuvem de mentira (testes/servidorFalsoNuvem.ts): o Painel fica, a faixa
// leva a pessoa a entrar de novo numa janela à parte, e o texto só existe na
// memória da página até ela salvar.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { type OpcoesDaNuvem, itemNuvem, servidorFalsoNuvem } from '../testes/servidorFalsoNuvem'
import AppNuvem from './AppNuvem'
import { abrirJanelaPadrao, modoDaJanela, nomeDoCanal } from './entrar'

const em = (dia: number, hora: number, minuto = 0) => new Date(2026, 9, dia, hora, minuto).toISOString()

function base() {
  return [
    itemNuvem({ id: 'd1', created_at: em(5, 14, 32), raw_text: 'Manda o relatório amanhã.', final_text: 'Manda o relatório amanhã.' }),
    itemNuvem({ id: 'd2', created_at: em(5, 9, 5), raw_text: 'A reunião é às dez.', final_text: 'A reunião é às dez.' }),
  ]
}

function montar(opcoes: OpcoesDaNuvem = {}, abrirJanela = vi.fn((url: string) => url !== '')) {
  const srv = servidorFalsoNuvem(base(), opcoes)
  const irPara = vi.fn()
  render(<AppNuvem buscar={srv.buscar} irPara={irPara} abrirJanela={abrirJanela} />)
  return { srv, irPara, abrirJanela }
}

const linha = (texto: RegExp) => screen.findByRole('button', { name: texto })
const campo = () => screen.getByLabelText<HTMLTextAreaElement>('Texto final')
const faixa = () => document.querySelector<HTMLElement>('.faixa')
const alerta = () => faixa()?.querySelector('[role="alert"]')?.textContent
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!
const doHistorico = (srv: ReturnType<typeof servidorFalsoNuvem>) => srv.pedidos.filter((p) => p.caminho.startsWith('/api/v1/transcriptions'))
const voltarParaAAba = () => act(() => void window.dispatchEvent(new Event('focus')))

const meuTexto = 'Manda o relatório hoje à noite.'

// editarEPerder: a pessoa escreve, a sessão acaba, e o Salvar dá 401.
async function editarEPerder(srv: ReturnType<typeof servidorFalsoNuvem>) {
  fireEvent.click(await linha(/Manda o relatório amanhã/))
  fireEvent.change(campo(), { target: { value: meuTexto } })
  srv.expirar()
  fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
  await waitFor(() => expect(alerta()).toBe('Sua sessão terminou. Seu texto continua aqui; entre de novo para salvar.'))
  // Os ouvintes da faixa (o foco da aba, o beforeunload) e o portão são de
  // efeitos, que rodam depois de a faixa aparecer: sem esvaziar a fila, um
  // voltarParaAAba() ou um sairDaPagina() logo em seguida chega antes deles.
  await act(async () => {})
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 5, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('a sessão que acaba no meio de uma edição', () => {
  it('o Painel fica, com o texto no editor, e a faixa diz para entrar de novo; nada vai para o aparelho', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    // Não é a tela inteira: o histórico e o editor continuam.
    expect(screen.queryByRole('heading', { level: 1, name: 'Sua sessão terminou.' })).toBeNull()
    expect(screen.getByRole('heading', { level: 1, name: 'Histórico' })).toBeTruthy()
    expect(campo().value).toBe(meuTexto)
    expect(await screen.findByRole('button', { name: 'Entrar de novo' })).toBeTruthy()
    // O texto só existe na memória da página.
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
    expect(document.cookie).toBe('')
  })

  it('enquanto a faixa está na tela, o Painel não fala com a nuvem (o portão)', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    const antes = doHistorico(srv).length
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    await act(async () => {})
    expect(doHistorico(srv)).toHaveLength(antes)
    // E o texto continua: a linha, fechada pela troca de vista, diz "não
    // salvo", e abre com ele.
    const fechada = await linha(/Manda o relatório amanhã/)
    expect(fechada.textContent).toContain('não salvo')
    fireEvent.click(fechada)
    expect(campo().value).toBe(meuTexto)
    expect(alerta()).toContain('Sua sessão terminou.')
  })

  it('Entrar de novo abre a janela à parte, no endereço do app; a página não sai do lugar', async () => {
    const { srv, irPara, abrirJanela } = montar()
    await editarEPerder(srv)
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar de novo' }))
    expect(abrirJanela).toHaveBeenCalledWith('/?janela=entrar')
    expect(irPara).not.toHaveBeenCalled()
    // Quem pede o login é a janela, não esta página.
    expect(srv.pedidos.some((p) => p.caminho === '/auth/start')).toBe(false)
    expect(alerta()).toBe('Sua sessão terminou. Seu texto continua aqui; entre de novo para salvar.')
    expect(campo().value).toBe(meuTexto)
  })

  it('a mesma conta volta (a aba volta a ter o foco): a faixa sai, o foco vai para o texto, e o Salvar grava', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    const entrar = await screen.findByRole('button', { name: 'Entrar de novo' })
    entrar.focus()
    fireEvent.click(entrar)
    // A janela foi fechada sem entrar: a faixa fica.
    voltarParaAAba()
    await waitFor(() => expect(srv.pedidos.filter((p) => p.caminho === '/api/v1/me')).toHaveLength(2))
    expect(faixa()).not.toBeNull()

    srv.entrar()
    const antes = srv.pedidos.length
    voltarParaAAba()
    await waitFor(() => expect(faixa()).toBeNull())
    expect(aviso().textContent).toBe('Você entrou de novo. Pode salvar.')
    await waitFor(() => expect(document.activeElement).toBe(campo()))
    expect(campo().value).toBe(meuTexto)
    // A lista lê de novo: pode ter mudado enquanto a sessão estava fora.
    await waitFor(() => expect(srv.pedidos.slice(antes).some((p) => p.metodo === 'GET' && p.caminho === '/api/v1/transcriptions')).toBe(true))

    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await linha(/Manda o relatório hoje à noite/)
    await waitFor(() => expect(aviso().textContent).toBe('Texto salvo.'))
    expect(srv.pedidos.filter((p) => p.metodo === 'PATCH').map((p) => p.corpo)).toEqual([
      { base_version: 1, final_text: meuTexto },
      { base_version: 1, final_text: meuTexto },
    ])
    expect(srv.itens.find((i) => i.id === 'd1')?.final_text).toBe(meuTexto)
  })

  it('a janela que entrou avisa esta aba pelo canal, e ela confere sem precisar do foco', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    srv.entrar()
    // A volta do Google cai no próprio app, em ?janela=entrou: a mesma página
    // de verdade avisa pelo BroadcastChannel e se fecha.
    const fechar = vi.fn()
    render(<AppNuvem busca="?janela=entrou" buscar={srv.buscar} fecharJanela={fechar} />)
    await waitFor(() => expect(faixa()).toBeNull())
    expect(fechar).toHaveBeenCalledTimes(1)
    expect(campo().value).toBe(meuTexto)
  })

  it('outra conta na janela: a faixa diz de quem é o texto, e o portão continua fechado até a conta certa voltar', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    srv.entrar({ id: 'u2', email: 'outra@teste.local' })
    voltarParaAAba()
    await waitFor(() => expect(alerta()).toBe('Você entrou em outra conta. Este texto é de voce@teste.local: entre com ela para salvar.'))
    const antes = doHistorico(srv).length
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await act(async () => {})
    expect(doHistorico(srv)).toHaveLength(antes)

    // A outra sessão acabou também: a faixa volta ao começo.
    srv.expirar()
    voltarParaAAba()
    await waitFor(() => expect(alerta()).toBe('Sua sessão terminou. Seu texto continua aqui; entre de novo para salvar.'))

    srv.entrar()
    voltarParaAAba()
    await waitFor(() => expect(faixa()).toBeNull())
  })

  it('a aba escondida não confere a sessão: só o foco ou a aba à vista de novo perguntam o /me', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    const meus = () => srv.pedidos.filter((p) => p.caminho === '/api/v1/me').length
    const antes = meus()
    const estado = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    try {
      act(() => void document.dispatchEvent(new Event('visibilitychange')))
      voltarParaAAba()
      await act(async () => {})
      expect(meus()).toBe(antes)
      estado.mockReturnValue('visible')
      act(() => void document.dispatchEvent(new Event('visibilitychange')))
      await waitFor(() => expect(meus()).toBe(antes + 1))
    } finally {
      estado.mockRestore()
    }
  })

  it('quem está noutro campo quando a sessão volta fica onde está', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    const busca = screen.getByRole('searchbox')
    busca.focus()
    srv.entrar()
    voltarParaAAba()
    await waitFor(() => expect(faixa()).toBeNull())
    // O efeito do foco roda depois da troca: dá a vez a ele antes de olhar.
    await act(() => new Promise((r) => setTimeout(r, 30)))
    expect(document.activeElement).toBe(busca)
  })

  it('a janela bloqueada: a faixa diz, o foco vai para Copiar, que leva o texto; e entrar nesta aba vai ao Google', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const { srv, irPara } = montar({}, vi.fn(() => false))
    await editarEPerder(srv)
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar de novo' }))
    expect(alerta()).toBe('Seu navegador bloqueou a janela. Copie o texto antes de entrar de novo nesta aba.')
    const copiar = screen.getByRole('button', { name: 'Copiar o texto' })
    await waitFor(() => expect(document.activeElement).toBe(copiar))
    expect(screen.queryByRole('button', { name: 'Entrar de novo' })).toBeNull()

    fireEvent.click(copiar)
    expect(writeText).toHaveBeenCalledWith(meuTexto)
    await waitFor(() => expect(alerta()).toBe('Seu navegador bloqueou a janela. Texto copiado. Agora pode entrar de novo nesta aba.'))

    fireEvent.click(screen.getByRole('button', { name: 'Entrar nesta aba' }))
    await waitFor(() => expect(irPara).toHaveBeenCalledWith(expect.stringMatching(/^https:\/\/accounts\.google\.com\//)))
    expect(srv.pedidos.find((p) => p.caminho === '/auth/start')?.corpo).toEqual({})
    expect(screen.getByRole('button', { name: 'Abrindo o Google…' })).toBeTruthy()
  })

  it('a janela bloqueada e a cópia que falha: a faixa diz como copiar à mão', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn(() => Promise.reject(new Error('negado'))) } })
    const { srv } = montar({}, vi.fn(() => false))
    await editarEPerder(srv)
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar de novo' }))
    fireEvent.click(screen.getByRole('button', { name: 'Copiar o texto' }))
    await waitFor(() =>
      expect(alerta()).toBe('Seu navegador bloqueou a janela. Não consegui copiar. Selecione o texto e copie antes de entrar de novo nesta aba.'),
    )
  })

  it('a janela bloqueada com dois textos não salvos: o Copiar leva os dois, separados por uma linha em branco, na ordem em que foram abertos', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const { srv } = montar({}, vi.fn(() => false))
    const outroTexto = 'A reunião é às onze.'
    fireEvent.click(await linha(/Manda o relatório amanhã/))
    fireEvent.change(campo(), { target: { value: meuTexto } })
    // Abrir a outra linha fecha esta, que fica com "não salvo".
    fireEvent.click(await linha(/A reunião é às dez/))
    fireEvent.change(campo(), { target: { value: outroTexto } })
    srv.expirar()
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(faixa()).not.toBeNull())
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar de novo' }))
    fireEvent.click(screen.getByRole('button', { name: 'Copiar o texto' }))
    expect(writeText).toHaveBeenCalledWith(`${meuTexto}\n\n${outroTexto}`)
  })

  it('a janela bloqueada com o texto apagado: não há o que copiar, a faixa diz "Não consegui copiar", e nada vai para a área de transferência', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const { srv } = montar({}, vi.fn(() => false))
    await editarEPerder(srv)
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar de novo' }))
    fireEvent.change(campo(), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Copiar o texto' }))
    await waitFor(() =>
      expect(alerta()).toBe('Seu navegador bloqueou a janela. Não consegui copiar. Selecione o texto e copie antes de entrar de novo nesta aba.'),
    )
    expect(writeText).not.toHaveBeenCalled()
  })

  it('a janela bloqueada num navegador sem área de transferência: "Não consegui copiar"', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: undefined })
    const { srv } = montar({}, vi.fn(() => false))
    await editarEPerder(srv)
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar de novo' }))
    fireEvent.click(screen.getByRole('button', { name: 'Copiar o texto' }))
    await waitFor(() =>
      expect(alerta()).toBe('Seu navegador bloqueou a janela. Não consegui copiar. Selecione o texto e copie antes de entrar de novo nesta aba.'),
    )
  })

  it('só a frase de sempre se esconde em pouca altura: a outra conta, o erro e a janela bloqueada ficam inteiros', async () => {
    const { srv } = montar({ teste: { conta: 'voce@teste.local', google: true } }, vi.fn(() => false))
    const compactavel = () => faixa()?.classList.contains('faixa-compactavel')
    await editarEPerder(srv)
    expect(compactavel()).toBe(true)
    expect(faixa()?.querySelector('.faixa-frase')?.textContent).toBe('Seu texto continua aqui; entre de novo para salvar.')

    // Outra conta na janela.
    srv.entrar({ id: 'u2', email: 'outra@teste.local' })
    voltarParaAAba()
    await waitFor(() => expect(alerta()).toContain('Você entrou em outra conta.'))
    expect(compactavel()).toBe(false)
    srv.expirar()
    voltarParaAAba()
    await waitFor(() => expect(compactavel()).toBe(true))

    // O login que não começou.
    srv.interceptar((p) => (p.metodo === 'POST' && p.caminho === '/auth/teste' ? new Response('{}', { status: 500 }) : undefined))
    fireEvent.click(screen.getByRole('button', { name: 'Entrar com a conta de teste' }))
    await waitFor(() => expect(alerta()).toBe('Sua sessão terminou. Não deu para entrar. Tente de novo.'))
    expect(compactavel()).toBe(false)
    srv.interceptar(null)

    // A janela bloqueada.
    fireEvent.click(screen.getByRole('button', { name: 'Entrar de novo' }))
    expect(alerta()).toContain('Seu navegador bloqueou a janela.')
    expect(compactavel()).toBe(false)
  })

  it('no servidor de teste: a conta de teste entra de novo sem sair da página', async () => {
    const { srv, abrirJanela } = montar({ teste: { conta: 'voce@teste.local', google: false } })
    await editarEPerder(srv)
    const teste = await screen.findByRole('button', { name: 'Entrar com a conta de teste' })
    // Sem o Google no servidor, sem a janela.
    expect(screen.queryByRole('button', { name: 'Entrar de novo' })).toBeNull()
    fireEvent.click(teste)
    await waitFor(() => expect(faixa()).toBeNull())
    expect(srv.pedidos.find((p) => p.metodo === 'POST' && p.caminho === '/auth/teste')?.corpo).toEqual({ email: 'voce@teste.local' })
    expect(abrirJanela).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(aviso().textContent).toBe('Texto salvo.'))
  })

  it('o texto numa linha fechada ("não salvo") também segura a tela, e o foco volta para ela', async () => {
    const { srv } = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã/))
    fireEvent.change(campo(), { target: { value: meuTexto } })
    // Abrir outra linha fecha esta, que fica com "não salvo"; a outra, sem
    // mexer, não conta.
    fireEvent.click(await linha(/A reunião é às dez/))
    fireEvent.keyDown(campo(), { key: 'Escape' })
    // A pessoa volta à aba mais tarde: a lista lê de novo e dá 401.
    srv.expirar()
    vi.setSystemTime(new Date(2026, 9, 5, 18, 0, 3))
    voltarParaAAba()
    await waitFor(() => expect(alerta()).toContain('Sua sessão terminou.'))
    const entrar = await screen.findByRole('button', { name: 'Entrar de novo' })
    entrar.focus()
    srv.entrar()
    voltarParaAAba()
    await waitFor(() => expect(faixa()).toBeNull())
    // O foco vem de um efeito, que roda depois de a faixa sair da página.
    await waitFor(() => expect(document.activeElement?.hasAttribute('data-linha')).toBe(true))
    const ativo = document.activeElement as HTMLElement
    expect(ativo.textContent).toContain('Manda o relatório amanhã.')
    expect(ativo.textContent).toContain('não salvo')
  })
})

// sairDaPagina: o navegador vai sair da página (recarregar, fechar a aba, o
// puxar para atualizar). true: o beforeunload pediu para perguntar antes.
function sairDaPagina(): boolean {
  const e = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(e)
  return e.defaultPrevented
}

describe('a pergunta do navegador antes de sair da página (a rede de segurança)', () => {
  it('só enquanto a faixa está na tela: antes do 401 e depois de entrar de novo, a página sai sem perguntar', async () => {
    const { srv } = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã/))
    fireEvent.change(campo(), { target: { value: meuTexto } })
    // Texto não salvo, mas a sessão está viva: nada a proteger ainda.
    expect(sairDaPagina()).toBe(false)
    srv.expirar()
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(faixa()).not.toBeNull())
    // O ouvinte do beforeunload entra e sai num efeito, que pode rodar depois
    // de a faixa aparecer ou sair da página: a pergunta também se espera.
    await waitFor(() => expect(sairDaPagina()).toBe(true))

    srv.entrar()
    voltarParaAAba()
    await waitFor(() => expect(faixa()).toBeNull())
    // E o efeito que o tira roda depois de a faixa sair da página.
    await waitFor(() => expect(sairDaPagina()).toBe(false))
  })

  it('outra conta na janela: o texto continua não salvo, e a pergunta continua', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    srv.entrar({ id: 'u2', email: 'outra@teste.local' })
    voltarParaAAba()
    await waitFor(() => expect(alerta()).toContain('Você entrou em outra conta.'))
    expect(sairDaPagina()).toBe(true)
  })

  it('sem nada a perder (a pessoa apagou o texto), não pergunta; escrevendo de novo, volta a perguntar', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    fireEvent.change(campo(), { target: { value: '' } })
    expect(sairDaPagina()).toBe(false)
    fireEvent.change(campo(), { target: { value: meuTexto } })
    expect(sairDaPagina()).toBe(true)
  })

  it('Entrar nesta aba é a escolha de perder o texto: o navegador não pergunta, e o botão não fica preso se ela voltar do cache', async () => {
    const { srv, irPara } = montar({}, vi.fn(() => false))
    await editarEPerder(srv)
    fireEvent.click(await screen.findByRole('button', { name: 'Entrar de novo' }))
    // Bloqueada, ainda com a faixa e o texto: a pergunta vale.
    expect(sairDaPagina()).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Entrar nesta aba' }))
    await waitFor(() => expect(irPara).toHaveBeenCalledTimes(1))
    expect(sairDaPagina()).toBe(false)

    // O Voltar do Google traz a página do cache: tudo volta a ser como antes.
    act(() => void window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Entrar nesta aba' })).toBeTruthy())
    expect(sairDaPagina()).toBe(true)
  })
})

describe('Sair com texto não salvo', () => {
  const grupo = () => screen.queryByRole('group', { name: 'Sair com texto não salvo' })
  const logouts = (srv: ReturnType<typeof servidorFalsoNuvem>) => srv.pedidos.filter((p) => p.metodo === 'POST' && p.caminho === '/auth/logout')
  const irParaConfiguracoes = () => fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))

  it('com a faixa na tela, o Sair pergunta antes de jogar o texto fora; Cancelar o mantém e devolve o foco ao Sair', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    irParaConfiguracoes()
    const sair = screen.getByRole('button', { name: 'Sair' })
    fireEvent.click(sair)
    expect(grupo()).not.toBeNull()
    expect(screen.getByText('Há texto que você escreveu e não salvou. Se você sair, ele se perde.')).toBeTruthy()
    // O foco vai para o lado seguro.
    const cancelar = screen.getByRole('button', { name: 'Cancelar' })
    await waitFor(() => expect(document.activeElement).toBe(cancelar))
    expect(logouts(srv)).toHaveLength(0)

    fireEvent.click(cancelar)
    expect(grupo()).toBeNull()
    expect(document.activeElement).toBe(sair)
    expect(logouts(srv)).toHaveLength(0)
    // O texto continua: a linha, de volta no histórico, diz "não salvo".
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    expect((await linha(/Manda o relatório amanhã/)).textContent).toContain('não salvo')
  })

  it('o Esc também cancela', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    irParaConfiguracoes()
    fireEvent.click(screen.getByRole('button', { name: 'Sair' }))
    fireEvent.keyDown(screen.getByRole('button', { name: 'Cancelar' }), { key: 'Escape' })
    expect(grupo()).toBeNull()
    expect(logouts(srv)).toHaveLength(0)
  })

  it('Sair e descartar chega ao servidor mesmo com o portão fechado: a sessão que já tinha voltado em outra aba cai também', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    // A sessão voltou por fora, e esta aba ainda não conferiu: o portão está
    // fechado, e o Sair não pode contar o 401 de mentira como "já saiu".
    srv.entrar()
    irParaConfiguracoes()
    fireEvent.click(screen.getByRole('button', { name: 'Sair' }))
    fireEvent.click(screen.getByRole('button', { name: 'Sair e descartar o texto' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Aurora Whisper' })).toBeTruthy()
    expect(logouts(srv)).toHaveLength(1)
    expect(srv.logado).toBe(false)
  })

  it('sem a faixa, com o texto não salvo e a sessão viva, o Sair também pergunta', async () => {
    const { srv } = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã/))
    fireEvent.change(campo(), { target: { value: meuTexto } })
    irParaConfiguracoes()
    fireEvent.click(screen.getByRole('button', { name: 'Sair' }))
    expect(grupo()).not.toBeNull()
    expect(logouts(srv)).toHaveLength(0)
  })

  it('o logout que falha por falta de rede não derruba o texto: a faixa de conexão acende, e o descartar pode tentar de novo', async () => {
    const { srv } = montar()
    await editarEPerder(srv)
    irParaConfiguracoes()
    fireEvent.click(screen.getByRole('button', { name: 'Sair' }))
    srv.derrubar(true)
    fireEvent.click(screen.getByRole('button', { name: 'Sair e descartar o texto' }))
    // Sem rede, a faixa de conexão ocupa o lugar da faixa da sessão; o
    // texto continua na tela, e a pergunta também.
    await waitFor(() => expect(faixa()?.textContent).toContain('Sem conexão.'))
    expect(screen.queryByRole('heading', { level: 1, name: 'Aurora Whisper' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Sair e descartar o texto' }).getAttribute('aria-disabled')).toBeNull()

    // A faixa de conexão tirou a da sessão da tela, e o portão abre com ela
    // (o cleanup): com a sessão de volta e a rede também, o Tentar de novo
    // chega à nuvem, e a lista lê de verdade.
    srv.derrubar(false)
    srv.entrar()
    const antes = doHistorico(srv).length
    fireEvent.click(within(faixa()!).getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(doHistorico(srv).length).toBeGreaterThan(antes))
    await waitFor(() => expect(faixa()).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'Sair e descartar o texto' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Aurora Whisper' })).toBeTruthy()
    expect(srv.logado).toBe(false)
  })
})

describe('sem texto não salvo, como antes', () => {
  it('o editor aberto sem mexer não segura a tela: a sessão que acaba troca a tela inteira', async () => {
    const { srv } = montar()
    fireEvent.click(await linha(/Manda o relatório amanhã/))
    // O 401 chega com o editor ainda aberto: a lista lê de novo quando a
    // pessoa volta à aba (trocar de vista fecharia o editor antes).
    srv.expirar()
    vi.setSystemTime(new Date(2026, 9, 5, 18, 0, 3))
    voltarParaAAba()
    expect(await screen.findByRole('heading', { level: 1, name: 'Sua sessão terminou.' })).toBeTruthy()
    expect(faixa()).toBeNull()
  })
})

describe('a janela à parte de entrar', () => {
  it('?janela=entrar pede o login com a volta para a janela e vai ao Google, sem abrir o histórico', async () => {
    const srv = servidorFalsoNuvem(base(), { logado: false })
    const trocarPara = vi.fn()
    render(<AppNuvem busca="?janela=entrar" buscar={srv.buscar} trocarPara={trocarPara} />)
    expect(await screen.findByRole('heading', { level: 1, name: 'Entrar de novo' })).toBeTruthy()
    expect(screen.getByText('Abrindo o Google…')).toBeTruthy()
    await waitFor(() => expect(trocarPara).toHaveBeenCalledWith(expect.stringMatching(/^https:\/\/accounts\.google\.com\//)))
    expect(srv.pedidos.map((p) => [p.metodo, p.caminho, p.corpo])).toEqual([['POST', '/auth/start', { volta: '/?janela=entrou' }]])
  })

  it('?janela=entrar que não começa: a frase do erro e Tentar de novo', async () => {
    const srv = servidorFalsoNuvem(base(), { logado: false })
    srv.interceptar((p) => (p.caminho === '/auth/start' ? new Response('{}', { status: 429 }) : undefined))
    const trocarPara = vi.fn()
    render(<AppNuvem busca="?janela=entrar" buscar={srv.buscar} trocarPara={trocarPara} />)
    expect((await screen.findByRole('alert')).textContent).toBe('Muitas tentativas. Espere um minuto e tente de novo.')
    expect(screen.getByText('Seu texto continua na outra janela.')).toBeTruthy()
    srv.interceptar(null)
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(trocarPara).toHaveBeenCalledTimes(1))
    expect(srv.pedidos.filter((p) => p.caminho === '/auth/start')).toHaveLength(2)
  })

  it('?janela=entrou avisa as outras abas pelo canal e se fecha, sem perguntar nada à nuvem', async () => {
    const srv = servidorFalsoNuvem(base())
    const ouvidos: unknown[] = []
    const canal = new BroadcastChannel(nomeDoCanal)
    canal.onmessage = (e) => ouvidos.push(e.data)
    const fechar = vi.fn()
    try {
      render(<AppNuvem busca="?janela=entrou" buscar={srv.buscar} fecharJanela={fechar} />)
      expect(screen.getByRole('heading', { level: 1, name: 'Você entrou de novo.' })).toBeTruthy()
      expect(screen.getByText('Pode fechar esta janela e voltar ao seu texto.')).toBeTruthy()
      // Se a janela não se fechar (o COOP, ou a página aberta na mesma aba), há
      // um caminho de volta ao app.
      expect(screen.getByRole('link', { name: 'Abrir o histórico' }).getAttribute('href')).toBe('/')
      await waitFor(() => expect(ouvidos).toEqual(['entrou']))
      expect(fechar).toHaveBeenCalledTimes(1)
      expect(srv.pedidos).toEqual([])
    } finally {
      canal.close()
    }
  })

  it('o endereço diz se a página é a janela', () => {
    expect(modoDaJanela('?janela=entrar')).toBe('entrar')
    expect(modoDaJanela('?x=1&janela=entrou')).toBe('entrou')
    expect(modoDaJanela('?janela=outra')).toBeNull()
    expect(modoDaJanela('')).toBeNull()
  })

  it('abrir a janela: false quando o navegador bloqueia; sem noopener, para saber disso', () => {
    const open = vi.spyOn(window, 'open')
    try {
      open.mockReturnValueOnce(null)
      expect(abrirJanelaPadrao('/?janela=entrar')).toBe(false)
      // Há bloqueador que devolve uma janela já fechada.
      open.mockReturnValueOnce({ closed: true } as Window)
      expect(abrirJanelaPadrao('/?janela=entrar')).toBe(false)
      open.mockReturnValueOnce({ closed: false } as Window)
      expect(abrirJanelaPadrao('/?janela=entrar')).toBe(true)
      for (const [url, , recursos] of open.mock.calls) {
        expect(url).toBe('/?janela=entrar')
        expect(String(recursos)).not.toMatch(/noopener|noreferrer/)
      }
    } finally {
      open.mockRestore()
    }
  })
})
