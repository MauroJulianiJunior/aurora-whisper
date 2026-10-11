// O "Adicionar termo" das Configurações (ADR-020) contra o Agent de mentira
// (testes/servidorFalso.ts): o formulário, a lista "Adicionados por você", a
// dica de cada termo, o Remover, os avisos e os erros.
//
// O que cada teste prova é o que o painel FAZ com o Agent: quais pedidos
// saem, com que corpo, e o que a pessoa vê. O que o Agent decide (o que não
// agiria, o que quebraria, o orçamento de verdade) é dos testes Go de
// internal/dictionary, cmd/agent e internal/agent/localapi.

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import App from './App'
import { type TermoAdicionado } from './api'
import { type OpcoesDoServidor, type Pedido, ditado, servidorFalso } from './testes/servidorFalso'

afterEach(() => {
  cleanup()
})

async function abrir(opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda.' })], opcoes)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'Dicionário' })
  await act(async () => {})
  return srv
}

const secao = () => screen.getByRole('region', { name: 'Dicionário' })
const palavra = () => screen.getByLabelText<HTMLInputElement>('Palavra certa')
const grafias = () => screen.getByLabelText<HTMLInputElement>(/Como sai errado/)
const caixaDaDica = () => screen.getByLabelText<HTMLInputElement>('Dica para o reconhecimento de voz')
const adicionar = () => within(secao()).getByRole<HTMLButtonElement>('button', { name: /^Adicion/ })
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent
const linhas = () => within(secao()).queryAllByRole('listitem')
const alertasDaSecao = () => within(secao()).queryAllByRole('alert').map((a) => a.textContent)
const pedidosA = (srv: { pedidos: Pedido[] }, caminho: string) => srv.pedidos.filter((p) => p.metodo === 'POST' && p.caminho === caminho)
const ajudaDaDica = () => document.getElementById('cfg-dica-ajuda')!.textContent

function preencher(certa: string, erradas = '') {
  fireEvent.change(palavra(), { target: { value: certa } })
  fireEvent.change(grafias(), { target: { value: erradas } })
}

const RAG: TermoAdicionado = { termo: 'RAG', variantes: ['rague', 'hage'], dica: true, do_dicionario: false }

describe('a seção Dicionário', () => {
  it('fica depois do Salvar configurações e antes da Privacidade, com o formulário e a dica marcada', async () => {
    await abrir()
    const titulos = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(titulos.indexOf('Dicionário')).toBeGreaterThan(titulos.indexOf('Sistema'))
    expect(titulos.indexOf('Dicionário')).toBeLessThan(titulos.indexOf('Privacidade'))
    const salvar = screen.getByRole('button', { name: 'Salvar configurações' })
    expect(salvar.compareDocumentPosition(secao()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    expect(secao().textContent).toContain('Palavras que o reconhecimento de voz escreve errado. Valem já no próximo ditado.')
    expect(caixaDaDica().checked).toBe(true)
    expect(ajudaDaDica()).toBe('Sobram 140 de 140 caracteres para dicas.')
    expect(document.getElementById('cfg-grafias-ajuda')!.textContent).toBe('Separe por vírgula.')
    // Sem nada adicionado, a lista não aparece.
    expect(screen.queryByRole('heading', { name: 'Adicionados por você' })).toBeNull()
  })

  it('Adicionar manda a palavra, as grafias e a dica no corpo, limpa os campos e avisa', async () => {
    const srv = await abrir()
    preencher('  RAG ', 'rague, , hage')
    fireEvent.click(adicionar())

    await waitFor(() => expect(aviso()).toContain('“RAG” adicionado. Vale no próximo ditado.'))
    const [p] = pedidosA(srv, '/api/termos')
    expect(p!.corpo).toEqual({ termo: 'RAG', variantes: ['rague', 'hage'], dica: true })
    // Nada do texto na URL.
    expect(srv.pedidos.every((x) => !`${x.caminho}?${x.busca}`.includes('RAG'))).toBe(true)

    expect(palavra().value).toBe('')
    expect(grafias().value).toBe('')
    await waitFor(() => expect(document.activeElement).toBe(palavra()))
    expect(screen.getByRole('heading', { name: 'Adicionados por você' })).toBeTruthy()
    expect(linhas()).toHaveLength(1)
    expect(Array.from(linhas()[0]!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['RAG', 'rague, hage'])
    expect(ajudaDaDica()).toBe('Sobram 137 de 140 caracteres para dicas.')
    expect(within(linhas()[0]!).getByLabelText<HTMLInputElement>('Dica para o reconhecimento de voz: RAG').checked).toBe(true)
  })

  it('sem a dica, a caixa vai desmarcada no pedido', async () => {
    const srv = await abrir()
    preencher('RAG')
    fireEvent.click(caixaDaDica())
    fireEvent.click(adicionar())
    await waitFor(() => expect(linhas()).toHaveLength(1))
    expect(pedidosA(srv, '/api/termos')[0]!.corpo).toEqual({ termo: 'RAG', variantes: [], dica: false })
    // O formulário volta ao começo: a próxima palavra vem com a dica marcada.
    expect(caixaDaDica().checked).toBe(true)
  })

  it('Enter na palavra certa também adiciona', async () => {
    const srv = await abrir()
    preencher('RAG')
    fireEvent.submit(palavra().form!)
    await waitFor(() => expect(pedidosA(srv, '/api/termos')).toHaveLength(1))
  })

  it('quando a dica empurra termos do dicionário para fora do prompt, o aviso diz quais', async () => {
    await abrir({ dicionario: ['DCTFWeb', 'eSocial'], termos: [{ termo: 'a'.repeat(70), variantes: [], dica: true, do_dicionario: false }] })
    preencher('b'.repeat(57))
    fireEvent.click(adicionar())
    await waitFor(() => expect(aviso()).toContain('adicionado. Vale no próximo ditado. Saiu das dicas: eSocial.'))
  })

  it('quando saem dois ou três termos, o aviso os junta com vírgula e "e"', async () => {
    const grande = { termo: 'a'.repeat(70), variantes: [], dica: true, do_dicionario: false }
    await abrir({ dicionario: ['DCTFWeb', 'eSocial', 'DAS'], termos: [grande] })
    preencher('b'.repeat(60))
    fireEvent.click(adicionar())
    await waitFor(() => expect(aviso()).toContain('Saíram das dicas: DCTFWeb e eSocial.'))

    cleanup()
    await abrir({ dicionario: ['DCTFWeb', 'eSocial', 'DAS'], termos: [grande] })
    preencher('b'.repeat(64))
    fireEvent.click(adicionar())
    await waitFor(() => expect(aviso()).toContain('Saíram das dicas: DCTFWeb, eSocial e DAS.'))
  })

  it('a palavra vazia não vai ao Agent', async () => {
    const srv = await abrir()
    preencher('   ', 'rague')
    fireEvent.click(adicionar())
    expect(alertasDaSecao()).toEqual(['Não deu para adicionar: faltou a palavra certa.'])
    expect(pedidosA(srv, '/api/termos')).toHaveLength(0)
  })

  it('a recusa do Agent fica na seção, com a frase dele, e o que foi digitado continua lá', async () => {
    const srv = await abrir()
    srv.recusarTermo('/api/termos', 409, 'Não cabe nas dicas: sobram 3 caracteres.')
    preencher('RAG', 'rague')
    fireEvent.click(adicionar())

    await waitFor(() => expect(alertasDaSecao()).toEqual(['Não deu para adicionar: Não cabe nas dicas: sobram 3 caracteres.']))
    expect(palavra().value).toBe('RAG')
    expect(grafias().value).toBe('rague')
    expect(document.querySelector('.faixa')).toBeNull()
    // O próximo que dá certo apaga o erro.
    srv.recusarTermo()
    fireEvent.click(adicionar())
    await waitFor(() => expect(alertasDaSecao()).toEqual([]))
  })

  it('a ligação perdida vai para a faixa do Painel, e não para a seção', async () => {
    const srv = await abrir()
    preencher('RAG')
    srv.derrubar(true)
    fireEvent.click(adicionar())
    await waitFor(() => expect(document.querySelector('.faixa')).toBeTruthy())
    expect(alertasDaSecao()).toEqual([])
  })

  it('dois cliques no Adicionar pedem uma vez só, e o botão diz que está adicionando', async () => {
    const srv = await abrir()
    const soltar = srv.pausar((p) => p.caminho === '/api/termos' && p.metodo === 'POST')
    preencher('RAG')
    fireEvent.click(adicionar())
    fireEvent.click(adicionar())
    await waitFor(() => expect(adicionar().textContent).toBe('Adicionando…'))
    expect(adicionar().getAttribute('aria-disabled')).toBe('true')
    soltar()
    await waitFor(() => expect(linhas()).toHaveLength(1))
    expect(pedidosA(srv, '/api/termos')).toHaveLength(1)
  })

  it('sem espaço nas dicas, a ajuda diz os dois caminhos', async () => {
    await abrir({ termos: [{ termo: 'a'.repeat(138), variantes: [], dica: true, do_dicionario: false }] })
    expect(ajudaDaDica()).toBe('Não sobra espaço: desmarque aqui ou desligue a dica de outro termo.')
  })

  it('com o mínimo que cabe, a ajuda diz quanto sobra', async () => {
    await abrir({ termos: [{ termo: 'a'.repeat(137), variantes: [], dica: true, do_dicionario: false }] })
    expect(ajudaDaDica()).toBe('Sobram 3 de 140 caracteres para dicas.')
  })

  it('a ajuda da dica só aparece com a conta, e a caixa só a cita quando ela existe', async () => {
    const srv = servidorFalso([])
    const soltar = srv.pausar((p) => p.caminho === '/api/termos' && p.metodo === 'GET')
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Dicionário' })
    expect(document.getElementById('cfg-dica-ajuda')).toBeNull()
    expect(caixaDaDica().hasAttribute('aria-describedby')).toBe(false)
    soltar()
    await waitFor(() => expect(ajudaDaDica()).toBe('Sobram 140 de 140 caracteres para dicas.'))
    expect(caixaDaDica().getAttribute('aria-describedby')).toBe('cfg-dica-ajuda')
  })

  it('um Agent de antes do Adicionar termo (404 sem frase): a seção diz para reiniciar', async () => {
    const srv = servidorFalso([])
    srv.interceptar((p) => (p.caminho === '/api/termos' && p.metodo === 'GET' ? new Response('404 page not found', { status: 404 }) : undefined))
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Dicionário' })
    await waitFor(() => expect(secao().textContent).toContain('Este Agent ainda não tem o Adicionar termo. Reinicie o Agent.'))
    expect(secao().textContent).not.toContain('erro 404')
    expect(screen.queryByLabelText('Palavra certa')).toBeNull()
  })

  it('reabrir as Configurações lê a lista de novo', async () => {
    const srv = await abrir()
    expect(linhas()).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Histórico' }))
    srv.termos.push({ ...RAG, variantes: [...RAG.variantes] })
    fireEvent.click(screen.getByRole('button', { name: 'Configurações' }))
    await waitFor(() => expect(linhas()).toHaveLength(1))
  })

  it('a lista que o Agent não consegue dar (503) mostra a frase dele no lugar do formulário, sem a faixa', async () => {
    const srv = servidorFalso([])
    srv.interceptar((p) =>
      p.caminho === '/api/termos' && p.metodo === 'GET'
        ? new Response(JSON.stringify({ error: { code: 'UNAVAILABLE', message: 'Não achei o dicionário do Agent, então não há onde adicionar.' } }), { status: 503 })
        : undefined,
    )
    render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
    await screen.findByRole('heading', { name: 'Dicionário' })
    await waitFor(() => expect(secao().textContent).toContain('Não achei o dicionário do Agent, então não há onde adicionar.'))
    expect(screen.queryByLabelText('Palavra certa')).toBeNull()
    expect(document.querySelector('.faixa')).toBeNull()
  })
})

describe('Adicionados por você', () => {
  it('desligar e ligar a dica de um termo: o pedido, o aviso e a conta', async () => {
    const srv = await abrir({ termos: [RAG] })
    const caixa = within(linhas()[0]!).getByLabelText<HTMLInputElement>('Dica para o reconhecimento de voz: RAG')
    fireEvent.click(caixa)
    await waitFor(() => expect(aviso()).toContain('Dica desligada para “RAG”.'))
    expect(pedidosA(srv, '/api/termos/dica')[0]!.corpo).toEqual({ termo: 'RAG', dica: false })
    expect(caixa.checked).toBe(false)
    expect(ajudaDaDica()).toBe('Sobram 140 de 140 caracteres para dicas.')

    fireEvent.click(caixa)
    await waitFor(() => expect(aviso()).toContain('Dica ligada para “RAG”.'))
    expect(caixa.checked).toBe(true)
  })

  it('a dica que o Agent recusa fica como estava, com a frase dele', async () => {
    const srv = await abrir({ termos: [{ ...RAG, dica: false }] })
    srv.recusarTermo('/api/termos/dica', 409, 'Com “RAG”, “sf” deixaria de virar “ECF”. Nada foi gravado.')
    const caixa = within(linhas()[0]!).getByLabelText<HTMLInputElement>('Dica para o reconhecimento de voz: RAG')
    fireEvent.click(caixa)
    await waitFor(() => expect(alertasDaSecao()).toEqual(['Não deu para mudar a dica: Com “RAG”, “sf” deixaria de virar “ECF”. Nada foi gravado.']))
    expect(caixa.checked).toBe(false)
  })

  it('Remover tira a linha, avisa, e o foco vai para o Remover que ocupou o lugar', async () => {
    const srv = await abrir({ termos: [RAG, { termo: 'n8n', variantes: ['n oito n'], dica: false, do_dicionario: false }] })
    fireEvent.click(within(linhas()[0]!).getByRole('button', { name: 'Remover RAG' }))
    await waitFor(() => expect(aviso()).toContain('“RAG” removido.'))
    expect(pedidosA(srv, '/api/termos/remover')[0]!.corpo).toEqual({ termo: 'RAG' })
    expect(linhas()).toHaveLength(1)
    await waitFor(() => expect(document.activeElement).toBe(within(linhas()[0]!).getByRole('button', { name: 'Remover n8n' })))
  })

  it('remover o último some com a lista, e o foco vai para a palavra certa', async () => {
    await abrir({ termos: [RAG] })
    fireEvent.click(screen.getByRole('button', { name: 'Remover RAG' }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Adicionados por você' })).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(palavra()))
  })

  it('o termo que já era do dicionário: grafias dela, a etiqueta, sem a caixa da dica', async () => {
    await abrir({ dicionario: ['DCTFWeb'] })
    preencher('dctfweb', 'dctf web')
    fireEvent.click(adicionar())
    await waitFor(() => expect(aviso()).toContain('“DCTFWeb” atualizado. Vale no próximo ditado.'))
    // A caixa estava marcada, e a dica dele não muda por aqui: o aviso diz.
    expect(aviso()).toContain('A dica não muda por aqui: “DCTFWeb” já era do dicionário.')
    const linha = linhas()[0]!
    expect(within(linha).getByText('do dicionário')).toBeTruthy()
    expect(within(linha).queryByRole('checkbox')).toBeNull()
    expect(within(linha).getByRole('button', { name: 'Remover as grafias de DCTFWeb' })).toBeTruthy()
  })

  it('o termo do dicionário sem a dica marcada: o aviso não fala da dica', async () => {
    await abrir({ dicionario: ['DCTFWeb'] })
    preencher('dctfweb', 'dctf web')
    fireEvent.click(caixaDaDica())
    fireEvent.click(adicionar())
    await waitFor(() => expect(aviso()).toContain('“DCTFWeb” atualizado.'))
    expect(aviso()).not.toContain('dica')
  })

  it('só a dica, num termo do dicionário: a recusa diz que a dica dele não muda por aqui', async () => {
    await abrir({ dicionario: ['DCTFWeb'] })
    preencher('DCTFWeb')
    fireEvent.click(adicionar())
    await waitFor(() => expect(alertasDaSecao()).toEqual(['Não deu para adicionar: “DCTFWeb” já é do dicionário: a dica dele não muda por aqui.']))
  })

  it('Remover num termo do dicionário diz que saíram as grafias, e não o termo', async () => {
    const srv = await abrir({ termos: [{ termo: 'DCTFWeb', variantes: ['dctf web'], dica: false, do_dicionario: true }] })
    fireEvent.click(screen.getByRole('button', { name: 'Remover as grafias de DCTFWeb' }))
    await waitFor(() => expect(aviso()).toContain('Grafias de “DCTFWeb” removidas.'))
    expect(aviso()).not.toContain('removido')
    expect(pedidosA(srv, '/api/termos/remover')[0]!.corpo).toEqual({ termo: 'DCTFWeb' })
  })

  it('o termo sem grafias não mostra a seta', async () => {
    await abrir({ termos: [{ termo: 'n8n', variantes: [], dica: true, do_dicionario: false }] })
    expect(linhas()[0]!.textContent).not.toContain('←')
    expect(linhas()[0]!.textContent).not.toContain('sai errado como')
    expect(Array.from(linhas()[0]!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['n8n'])
  })

  it('o Remover que o Agent recusa fica na seção, com a frase dele, e a linha continua', async () => {
    const srv = await abrir({ termos: [RAG] })
    srv.recusarTermo('/api/termos/remover', 500, 'Deu um erro dentro do Agent. Tente de novo.')
    fireEvent.click(screen.getByRole('button', { name: 'Remover RAG' }))
    await waitFor(() => expect(alertasDaSecao()).toEqual(['Não deu para remover: Deu um erro dentro do Agent. Tente de novo.']))
    expect(linhas()).toHaveLength(1)
    expect(document.querySelector('.faixa')).toBeNull()
  })

  it('o erro da lista some quando outro pedido começa', async () => {
    const srv = await abrir({ termos: [RAG] })
    srv.recusarTermo('/api/termos/dica', 409, 'Não cabe nas dicas: sobram 3 caracteres.')
    fireEvent.click(within(linhas()[0]!).getByLabelText('Dica para o reconhecimento de voz: RAG'))
    await waitFor(() => expect(alertasDaSecao()).toHaveLength(1))
    srv.recusarTermo()
    const soltar = srv.pausar((p) => p.caminho === '/api/termos/remover')
    fireEvent.click(screen.getByRole('button', { name: 'Remover RAG' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remover RAG' }).textContent).toBe('Removendo…'))
    expect(alertasDaSecao()).toEqual([])
    soltar()
    await waitFor(() => expect(linhas()).toHaveLength(0))
  })

  it('a ligação perdida na dica e no Remover vai para a faixa do Painel, e não para a seção', async () => {
    const srv = await abrir({ termos: [RAG] })
    srv.derrubar(true)
    fireEvent.click(within(linhas()[0]!).getByLabelText('Dica para o reconhecimento de voz: RAG'))
    await waitFor(() => expect(document.querySelector('.faixa')).toBeTruthy())
    expect(alertasDaSecao()).toEqual([])
    fireEvent.click(screen.getByRole('button', { name: 'Remover RAG' }))
    await act(async () => {})
    expect(alertasDaSecao()).toEqual([])
    expect(linhas()).toHaveLength(1)
  })

  it('a dica de outro termo, com um pedido no ar, espera: um pedido por vez', async () => {
    const srv = await abrir({ termos: [RAG, { termo: 'n8n', variantes: ['n oito n'], dica: false, do_dicionario: false }] })
    const soltar = srv.pausar((p) => p.caminho === '/api/termos/dica')
    fireEvent.click(within(linhas()[0]!).getByLabelText('Dica para o reconhecimento de voz: RAG'))
    fireEvent.click(within(linhas()[1]!).getByLabelText('Dica para o reconhecimento de voz: n8n'))
    fireEvent.click(within(linhas()[1]!).getByRole('button', { name: 'Remover n8n' }))
    soltar()
    await waitFor(() => expect(aviso()).toContain('Dica desligada para “RAG”.'))
    expect(pedidosA(srv, '/api/termos/dica')).toHaveLength(1)
    expect(pedidosA(srv, '/api/termos/remover')).toHaveLength(0)
    expect(within(linhas()[1]!).getByLabelText<HTMLInputElement>('Dica para o reconhecimento de voz: n8n').checked).toBe(false)
  })

  it('gravado, mas só vale depois de reiniciar: o aviso diz', async () => {
    const srv = await abrir({ termos: [RAG] })
    srv.interceptar((p) =>
      p.caminho === '/api/termos/remover'
        ? new Response(
            JSON.stringify({ items: null, dicas: { usadas: 0, orcamento: 140 }, termo: 'RAG', ja_existia: true, sairam_das_dicas: null, vale_ao_reiniciar: true }),
            { status: 200 },
          )
        : undefined,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Remover RAG' }))
    await waitFor(() => expect(aviso()).toContain('“RAG” removido. Vale depois de reiniciar o Agent.'))
    // A lista null do Go é uma lista vazia.
    expect(linhas()).toHaveLength(0)
  })

  it('dois cliques no Remover pedem uma vez só', async () => {
    const srv = await abrir({ termos: [RAG] })
    const soltar = srv.pausar((p) => p.caminho === '/api/termos/remover')
    const botao = screen.getByRole('button', { name: 'Remover RAG' })
    fireEvent.click(botao)
    fireEvent.click(botao)
    await waitFor(() => expect(botao.textContent).toBe('Removendo…'))
    soltar()
    await waitFor(() => expect(linhas()).toHaveLength(0))
    expect(pedidosA(srv, '/api/termos/remover')).toHaveLength(1)
  })

  it('o leitor de tela ouve "sai errado como", e a seta é só para os olhos', async () => {
    await abrir({ termos: [RAG] })
    expect(linhas()[0]!.textContent).toContain('RAG ←, sai errado como rague, hage')
    expect(linhas()[0]!.querySelector('[aria-hidden="true"]')!.textContent).toBe(' ←')
  })
})
