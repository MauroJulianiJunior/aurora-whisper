// "Onde consigo a chave" no painel do PC (a garantia 1 do lançamento: quem usa
// sabe que a chave é dela, onde criá-la e que o ditado nem precisa dela):
//
//  - a linha embaixo do campo de chave da IA, com o link oficial do provedor;
//  - a frase da seção IA e a do primeiro uso (o histórico ainda vazio) dizendo
//    que o ditado funciona sem chave e que ela só serve ao Polish, opcional.
//
// A linha do app da nuvem (OpenAI, Groq e Anthropic) é testada em
// nuvem/ditar/ditarPeloCelular.test.tsx e nuvem/ditar/paginasDeChave.test.ts. Os
// endereços ficam presos: se um provedor mudar o dele, o teste quebra e a
// mudança é uma decisão, não um acaso.

import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import App from './App'
import { OndeCriarAChave } from './OndeCriarAChave'
import { paginasDeChave } from './paginasDeChave'
import { type OpcoesDoServidor, ditado, servidorFalso } from './testes/servidorFalso'

afterEach(cleanup)

const frase = (endereco: string, nome: string) =>
  `Crie a chave em ${endereco} (abre em outra aba). Ela é sua e o uso é cobrado na sua conta da ${nome}: use uma só para o Aurora Whisper, com limite de gasto.`

// O nome do link pelo leitor de tela: o endereço e o aviso de outra aba (com ou
// sem o espaço, que cada cálculo de nome trata de um jeito).
const nomeDoLink = (endereco: string) => new RegExp(`^${endereco.replaceAll('.', '\\.')}\\s?\\(abre em outra aba\\)$`)

describe('os endereços onde se cria a chave', () => {
  it('OpenAI e Anthropic: as páginas oficiais, conferidas em 13-out-2026', () => {
    expect(paginasDeChave.openai.url).toBe('https://platform.openai.com/api-keys')
    // O Console da Anthropic saiu de console.anthropic.com (que redireciona) para platform.claude.com.
    expect(paginasDeChave.anthropic.url).toBe('https://platform.claude.com/settings/keys')
    // O painel do PC conhece só os dois provedores do Polish.
    expect(Object.keys(paginasDeChave).sort()).toEqual(['anthropic', 'openai'])
  })

  it('todos em https, sem parâmetro, sem credencial no endereço', () => {
    for (const p of Object.values(paginasDeChave)) {
      const u = new URL(p.url)
      expect(u.protocol).toBe('https:')
      expect(u.search).toBe('')
      expect(u.hash).toBe('')
      expect(u.username + u.password).toBe('')
    }
  })
})

describe('a linha de onde criar a chave', () => {
  it.each([
    [paginasDeChave.openai, 'platform.openai.com/api-keys', 'OpenAI'],
    [paginasDeChave.anthropic, 'platform.claude.com/settings/keys', 'Anthropic'],
  ])('%j: uma frase, com o endereço por extenso no link', (pagina, endereco, nome) => {
    render(<OndeCriarAChave pagina={pagina} paraQue="o Aurora Whisper" id="onde" />)
    const p = document.getElementById('onde')!
    expect(p.tagName).toBe('P')
    expect(p.className).toBe('ajuda')
    expect(p.textContent).toBe(frase(endereco, nome))
    const link = within(p).getByRole<HTMLAnchorElement>('link', { name: nomeDoLink(endereco) })
    expect(link.getAttribute('href')).toBe(pagina.url)
    expect(link.textContent).toContain(endereco)
    // Em outra aba, sem passar a página de origem (o painel do PC mora em 127.0.0.1 com porta que muda) nem a referência.
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.rel.split(' ').sort()).toEqual(['noopener', 'noreferrer'])
  })

  it('diz as três coisas e nada de tutorial: onde, que é dela e cobrada nela, e uma chave só para isto com limite de gasto', () => {
    render(<OndeCriarAChave pagina={paginasDeChave.openai} paraQue="o Aurora Whisper" />)
    const texto = document.querySelector('p')!.textContent!
    expect(texto).toContain('Crie a chave em')
    expect(texto).toContain('Ela é sua e o uso é cobrado na sua conta da OpenAI')
    expect(texto).toContain('use uma só para o Aurora Whisper, com limite de gasto')
    // O menor que resolve: duas frases num parágrafo só, com o que se enxerga (sem o aviso do leitor de tela) em menos de 160 letras.
    const visivel = texto.replace(' (abre em outra aba)', '')
    expect(visivel.length).toBeLessThan(160)
    expect(document.querySelectorAll('p')).toHaveLength(1)
  })
})

// montar abre o painel do PC; abrirIA vai às Configurações.
function montar(ditados = [ditado({ id: 'd1', created_at: new Date(2026, 8, 28, 14, 32).toISOString(), final_text: 'Manda para o Thiago.' })], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso(ditados, opcoes)
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  return srv
}

async function abrirIA(opcoes: OpcoesDoServidor = {}) {
  montar(undefined, opcoes)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'IA' })
}

const provedor = () => screen.getByLabelText<HTMLSelectElement>('Provedor')
const secaoIA = () => screen.getByRole('region', { name: 'IA' })

describe('o campo da chave da IA, no painel do PC', () => {
  it('sem provedor escolhido não há campo de chave, e portanto nenhum link', async () => {
    await abrirIA()
    expect(provedor().value).toBe('')
    expect(screen.queryByLabelText(/Chave de API/)).toBeNull()
    expect(secaoIA().querySelector('a')).toBeNull()
  })

  it('OpenAI: o link dela embaixo do campo, ligado ao campo, antes de dizer onde a chave fica guardada', async () => {
    await abrirIA({ config: { ai_provider: 'openai' } })
    const campo = await screen.findByLabelText('Chave de API da OpenAI')
    await screen.findByText('Nenhuma chave guardada.')
    const linha = document.getElementById('cfg-chave-onde')!
    expect(linha.textContent).toBe(frase('platform.openai.com/api-keys', 'OpenAI'))
    expect(within(linha).getByRole('link').getAttribute('href')).toBe('https://platform.openai.com/api-keys')
    // O campo é descrito pelo estado, por onde criar e por onde a chave fica guardada, nessa ordem.
    expect(campo.getAttribute('aria-describedby')).toBe('cfg-chave-estado cfg-chave-onde cfg-chave-ajuda')
    expect(linha.nextElementSibling!.id).toBe('cfg-chave-ajuda')
    expect(linha.nextElementSibling!.textContent).toBe('Ao salvar, a chave vai direto para o Gerenciador de Credenciais do Windows e não volta para esta página.')
  })

  it('Anthropic: o link dela; trocar de provedor troca o link, e voltar a Nenhum o tira', async () => {
    await abrirIA({ config: { ai_provider: 'openai' } })
    await screen.findByLabelText('Chave de API da OpenAI')
    fireEvent.change(provedor(), { target: { value: 'anthropic' } })
    await screen.findByLabelText('Chave de API da Anthropic')
    const linha = document.getElementById('cfg-chave-onde')!
    expect(linha.textContent).toBe(frase('platform.claude.com/settings/keys', 'Anthropic'))
    expect(Array.from(secaoIA().querySelectorAll('a'), (a) => a.getAttribute('href'))).toEqual(['https://platform.claude.com/settings/keys'])

    fireEvent.change(provedor(), { target: { value: '' } })
    expect(secaoIA().querySelector('a')).toBeNull()
  })

  it('a linha fica com a chave já guardada também: serve para trocar a chave por outra', async () => {
    await abrirIA({ config: { ai_provider: 'openai' }, chaves: { openai: 'sk-teste-a1B2' } })
    await screen.findByText('••••a1B2')
    expect(document.getElementById('cfg-chave-onde')!.textContent).toBe(frase('platform.openai.com/api-keys', 'OpenAI'))
  })

  it('a chave colada não sai da página para o link: o endereço é fixo', async () => {
    await abrirIA({ config: { ai_provider: 'openai' } })
    fireEvent.input(await screen.findByLabelText('Chave de API da OpenAI'), { target: { value: 'sk-proj-colada-no-campo-7Kq9' } })
    expect(Array.from(secaoIA().querySelectorAll('a'), (a) => a.href)).toEqual(['https://platform.openai.com/api-keys'])
    expect(secaoIA().textContent).not.toContain('sk-proj-colada')
  })

  it('o painel do PC não conhece a Groq: só OpenAI e Anthropic', async () => {
    await abrirIA({ config: { ai_provider: 'openai' } })
    await screen.findByLabelText('Chave de API da OpenAI')
    expect(Array.from(provedor().options, (o) => o.textContent)).toEqual(['Nenhum', 'OpenAI', 'Anthropic'])
    expect(document.body.textContent).not.toContain('Groq')
  })
})

describe('a seção IA e o primeiro uso dizem que o ditado não precisa de chave', () => {
  it('a introdução da seção IA: só o Polish usa a IA, ele é opcional, e o ditado funciona sem chave', async () => {
    await abrirIA()
    expect(screen.getByText('Só o modo Polish usa a IA, e ele é opcional: o ditado funciona sem chave.')).toBeTruthy()
  })

  it('o histórico ainda vazio: segure Ctrl+Win, fale e solte; funciona sem chave; ela só serve ao Polish, opcional', async () => {
    montar([])
    await screen.findByText('Nenhum ditado ainda.', { selector: '.estado p.estado-titulo' })
    const texto = 'Segure Ctrl+Win, fale e solte, em qualquer aplicativo. Funciona sem chave: ela só serve ao Polish, que é opcional.'
    const p = screen.getByText(texto)
    expect(p.closest('.estado')).not.toBeNull()
    // O mesmo atalho que as Configurações mostram: o de fábrica, que por enquanto não se troca.
    expect(p.textContent).toContain('Ctrl+Win')
  })

  it('o aviso é do primeiro uso: com ditados no histórico, ele não aparece', async () => {
    montar()
    await screen.findByText(/Manda para o Thiago/)
    expect(screen.queryByText(/Funciona sem chave/)).toBeNull()
  })

  it('a lixeira vazia não repete o aviso', async () => {
    montar([])
    await screen.findByText('Nenhum ditado ainda.', { selector: '.estado p.estado-titulo' })
    fireEvent.click(screen.getByRole('button', { name: 'Lixeira' }))
    await screen.findByText('A lixeira está vazia.', { selector: '.estado p.estado-titulo' })
    expect(screen.queryByText(/Funciona sem chave/)).toBeNull()
  })
})
