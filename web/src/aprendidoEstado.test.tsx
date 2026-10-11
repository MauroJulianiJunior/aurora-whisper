// As linhas de estado do "Aprendido no painel" (ADR-019, F8h) contra o Agent de
// mentira (testes/servidorFalso.ts): a troca que chegou da nuvem e não vale
// neste PC, e a deste PC que perdeu na nuvem. Vêm do GET /api/aprendizado com
// `situacao` e `motivo`, opcionais (06); um Agent de antes da F8 não os manda,
// e a lista é a de sempre.
//
// O que se prova aqui é o que a tela faz com o que o Agent manda: qual linha
// aparece, com qual etiqueta e frase, sem Desfazer, e para onde o foco vai.
// Quando o Agent manda cada situação é dos testes Go (cmd/agent, F8g).

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import type { Aprendida } from './api'
import { linhaDoAprendido } from './aprendizado'
import { type OpcoesDoServidor, ditado, servidorFalso } from './testes/servidorFalso'

const sf: Aprendida = { termo: 'ECF', variante: 'sf' }
const wispr: Aprendida = { termo: 'Wispr Flow', variante: 'whisperflow' }
const naoAplicado = (motivo?: string): Aprendida => ({ termo: 'DCTFWeb', variante: 'dctf web', situacao: 'nao_aplicado', ...(motivo ? { motivo } : {}) })
const desfeito: Aprendida = { termo: 'n8n', variante: 'n oito n', situacao: 'desfeito_em_outro_aparelho' }
const trocado: Aprendida = { termo: 'ECF', variante: 'sf', situacao: 'trocado_em_outro_aparelho' }

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 7, 18, 0))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

async function abrirConfiguracoes(aprendidas: Aprendida[], opcoes: OpcoesDoServidor = {}) {
  const srv = servidorFalso([ditado({ id: 'd1', created_at: new Date(2026, 9, 7, 14).toISOString(), final_text: 'Manda.' })], { aprendidas, ...opcoes })
  render(<App sessao={Promise.resolve('tok')} buscar={srv.buscar} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Configurações' }))
  await screen.findByRole('heading', { name: 'Ditado' })
  await act(async () => {})
  return srv
}

const secao = () => screen.queryByRole('region', { name: 'Aprendido no painel' })
const linhas = () => within(secao()!).getAllByRole('listitem')
const aviso = () => document.querySelector<HTMLElement>('.aviso-regiao')!.textContent
const textoDa = (el: Element) => el.textContent?.replace(/\s+/g, ' ').trim()

describe('um Agent de antes da F8', () => {
  it('não manda situacao: a lista é a de sempre, com o Desfazer e sem etiqueta', async () => {
    await abrirConfiguracoes([sf, wispr])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(linhas()).toHaveLength(2)
    for (const linha of linhas()) {
      expect(within(linha).getByRole('button', { name: /^Desfazer: / })).toBeTruthy()
      expect(linha.querySelector('.etiqueta')).toBeNull()
      expect(linha.querySelector('.motivo')).toBeNull()
    }
  })

  it('situacao vazia vale como ausente', async () => {
    await abrirConfiguracoes([{ ...sf, situacao: '', motivo: '' }])
    await screen.findByRole('button', { name: 'Desfazer: sf vira ECF' })
    expect(secao()!.querySelector('.etiqueta')).toBeNull()
  })
})

describe('"não aplicado neste PC"', () => {
  it('a troca com a etiqueta cinza e o motivo em texto pequeno, sem Desfazer', async () => {
    await abrirConfiguracoes([sf, naoAplicado('dominio_ausente')])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    const [vale, estado] = linhas()
    expect(within(vale!).getByRole('button', { name: 'Desfazer: sf vira ECF' })).toBeTruthy()

    expect(Array.from(estado!.querySelectorAll('.termo'), (t) => t.textContent)).toEqual(['dctf web', 'DCTFWeb'])
    const etiqueta = estado!.querySelector('.etiqueta')!
    expect(etiqueta.textContent).toBe('não aplicado neste PC')
    // A etiqueta de sempre: cinza, na mono, sem a cor de erro nem a "forte".
    expect(etiqueta.className).toBe('etiqueta')
    expect(estado!.querySelector('.motivo')!.textContent).toBe('O domínio da troca não existe aqui.')
    expect(within(estado!).queryByRole('button')).toBeNull()
    // O leitor de tela ouve a troca, a etiqueta e o motivo.
    expect(textoDa(estado!)).toBe('dctf web → vira DCTFWeb não aplicado neste PC O domínio da troca não existe aqui.')
  })

  it.each([
    ['dominio_ausente', 'O domínio da troca não existe aqui.'],
    ['restricao_nao_cabe', 'A troca valeria em mais lugares do que no PC onde foi aprendida.'],
    ['palavra_ausente_com_dominio', 'A palavra não existe aqui, e a troca é presa a um domínio.'],
    ['variante_de_outro_termo', 'Aqui essa grafia já pertence a outra palavra.'],
    ['termo_desligado', 'A palavra está desligada.'],
    ['nao_agiria', 'O dicionário não aplicaria a troca.'],
    ['palavra_comum', 'A grafia é comum demais para virar troca.'],
    ['arquivo_indisponivel', 'O dicionário está indisponível.'],
  ])('o motivo %s tem a sua frase curta', async (motivo, frase) => {
    await abrirConfiguracoes([naoAplicado(motivo)])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(linhas()[0]!.querySelector('.motivo')!.textContent).toBe(frase)
    // Uma frase curta e inteira: cabe em duas linhas de celular, e termina em ponto.
    expect(frase.length).toBeLessThanOrEqual(70)
    expect(frase.endsWith('.')).toBe(true)
    // A etiqueta já diz "neste PC": a frase de baixo não repete o escopo.
    expect(frase).not.toMatch(/(este|neste|deste) PC/i)
  })

  it('o motivo que o painel não conhece fica sem frase, e o código cru nunca aparece', async () => {
    await abrirConfiguracoes([naoAplicado('um_motivo_do_futuro'), { ...naoAplicado('constructor'), variante: 'outra' }])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(linhas()).toHaveLength(2)
    for (const linha of linhas()) {
      expect(linha.querySelector('.etiqueta')!.textContent).toBe('não aplicado neste PC')
      expect(linha.querySelector('.motivo')).toBeNull()
    }
    expect(secao()!.textContent).not.toContain('um_motivo_do_futuro')
    expect(secao()!.textContent).not.toContain('constructor')
  })

  it('sem motivo, só a etiqueta', async () => {
    await abrirConfiguracoes([naoAplicado()])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(linhas()[0]!.querySelector('.motivo')).toBeNull()
    expect(linhas()[0]!.querySelector('.etiqueta')!.textContent).toBe('não aplicado neste PC')
  })
})

describe('"desfeito em outro aparelho" e "trocado em outro aparelho"', () => {
  it('a etiqueta cinza, sem Desfazer e sem motivo (mesmo que o Agent mande um)', async () => {
    await abrirConfiguracoes([
      { ...desfeito, motivo: 'dominio_ausente' },
      { ...trocado, motivo: 'nao_agiria' },
    ])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    const [a, b] = linhas()
    expect(a!.querySelector('.etiqueta')!.textContent).toBe('desfeito em outro aparelho')
    expect(b!.querySelector('.etiqueta')!.textContent).toBe('trocado em outro aparelho')
    for (const linha of [a!, b!]) {
      expect(linha.querySelector('.etiqueta')!.className).toBe('etiqueta')
      expect(linha.querySelector('.motivo')).toBeNull()
      expect(within(linha).queryByRole('button')).toBeNull()
    }
    expect(textoDa(a!)).toBe('n oito n → vira n8n desfeito em outro aparelho')
  })
})

describe('a seção com linhas de estado', () => {
  it('aparece também quando só há linhas de estado, sem um botão e sem alarme', async () => {
    await abrirConfiguracoes([naoAplicado('nao_agiria'), desfeito])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(linhas()).toHaveLength(2)
    expect(within(secao()!).queryByRole('button')).toBeNull()
    expect(within(secao()!).queryByRole('alert')).toBeNull()
    expect(document.querySelector('.faixa')).toBeNull()
  })

  it('sem nada, ou só com situação que o painel não conhece, a seção não aparece', async () => {
    await abrirConfiguracoes([])
    await act(async () => {})
    expect(secao()).toBeNull()
    cleanup()
    await abrirConfiguracoes([{ ...sf, situacao: 'situacao_do_futuro' }, { ...wispr, situacao: 'toString' }])
    await act(async () => {})
    expect(secao()).toBeNull()
    expect(screen.queryByText('Aprendido no painel')).toBeNull()
  })

  it('a situação desconhecida entre as outras não vira linha e não tira nada das que valem', async () => {
    await abrirConfiguracoes([sf, { ...wispr, situacao: 'situacao_do_futuro' }, desfeito])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(linhas()).toHaveLength(2)
    expect(secao()!.textContent).not.toContain('whisperflow')
  })

  it('a mesma variante pode ter a linha que vale e a de estado ("trocado em outro aparelho")', async () => {
    const valendo: Aprendida = { termo: 'ECD', variante: 'sf' }
    await abrirConfiguracoes([valendo, trocado])
    await screen.findByRole('heading', { name: 'Aprendido no painel' })
    expect(linhas()).toHaveLength(2)
    expect(textoDa(linhas()[0]!)).toContain('sf → vira ECD')
    expect(textoDa(linhas()[1]!)).toBe('sf → vira ECF trocado em outro aparelho')
  })
})

describe('Desfazer com linhas de estado na lista', () => {
  it('desfaz só a linha que vale: a de estado da mesma variante fica, e o pedido leva só a variante', async () => {
    const valendo: Aprendida = { termo: 'ECD', variante: 'sf' }
    const srv = await abrirConfiguracoes([valendo, trocado])
    await screen.findByRole('button', { name: 'Desfazer: sf vira ECD' })
    fireEvent.click(screen.getByRole('button', { name: 'Desfazer: sf vira ECD' }))
    await waitFor(() => expect(aviso()).toContain('Desfeito: “sf” não vira mais “ECD”.'))
    expect(srv.pedidos.filter((p) => p.caminho === '/api/aprendizado/desfazer').map((p) => p.corpo)).toEqual([{ variante: 'sf' }])
    expect(linhas()).toHaveLength(1)
    expect(textoDa(linhas()[0]!)).toBe('sf → vira ECF trocado em outro aparelho')
  })

  // O foco é calculado pelos botões, e não pelas linhas: as de estado não têm.
  it('[estado, A, B]: desfazer A leva o foco ao botão de B, e não a um índice de linha', async () => {
    await abrirConfiguracoes([desfeito, sf, wispr])
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    await waitFor(() => expect(linhas()).toHaveLength(2))
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Desfazer: whisperflow vira Wispr Flow' })))
  })

  it('[estado, estado, A, B, C]: desfazer A leva o foco ao botão de B, o que ocupou o lugar (não ao índice da linha)', async () => {
    const rf: Aprendida = { termo: 'RF', variante: 'rj' }
    await abrirConfiguracoes([desfeito, naoAplicado('nao_agiria'), sf, wispr, rf])
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    await waitFor(() => expect(linhas()).toHaveLength(4))
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Desfazer: whisperflow vira Wispr Flow' })))
  })

  it('[A, estado, B]: desfazer B leva o foco ao botão de A, passando por cima da linha de estado', async () => {
    await abrirConfiguracoes([sf, naoAplicado('nao_agiria'), wispr])
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer: whisperflow vira Wispr Flow' }))
    await waitFor(() => expect(linhas()).toHaveLength(2))
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Desfazer: sf vira ECF' })))
  })

  it('[A, estado, B]: desfazer A leva o foco ao botão de B', async () => {
    await abrirConfiguracoes([sf, naoAplicado('nao_agiria'), wispr])
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    await waitFor(() => expect(linhas()).toHaveLength(2))
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Desfazer: whisperflow vira Wispr Flow' })))
  })

  it('desfazer a última troca que vale, com linhas de estado: a seção fica, e o foco vai para o título dela', async () => {
    await abrirConfiguracoes([sf, desfeito])
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    await waitFor(() => expect(linhas()).toHaveLength(1))
    expect(secao()).not.toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('heading', { name: 'Aprendido no painel' })))
    // A seção não pediu o foco do Salvar configurações: ela não sumiu.
    expect(document.activeElement).not.toBe(screen.getByRole('button', { name: 'Salvar configurações' }))
  })

  it('desfazer a última linha de todas (sem estado) some com a seção, e o foco vai para o Salvar configurações', async () => {
    await abrirConfiguracoes([sf])
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer: sf vira ECF' }))
    await waitFor(() => expect(secao()).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Salvar configurações' })))
  })

  it('o Desfazer que o Agent recusa deixa a lista como está, com as linhas de estado', async () => {
    const srv = await abrirConfiguracoes([sf, naoAplicado('nao_agiria')])
    await screen.findByRole('button', { name: 'Desfazer: sf vira ECF' })
    srv.aprendidas.splice(0, 1)
    fireEvent.click(screen.getByRole('button', { name: 'Desfazer: sf vira ECF' }))
    const alerta = await within(secao()!).findByRole('alert')
    expect(alerta.textContent).toBe('Não deu para desfazer: "sf" não foi aprendido pelo painel.')
    expect(linhas()).toHaveLength(2)
  })
})

describe('linhaDoAprendido', () => {
  it('a que vale, a de estado e a que o painel não conhece', () => {
    expect(linhaDoAprendido(sf)).toEqual({ tipo: 'vale' })
    expect(linhaDoAprendido(naoAplicado('palavra_comum'))).toEqual({ tipo: 'estado', etiqueta: 'não aplicado neste PC', motivo: 'A grafia é comum demais para virar troca.' })
    expect(linhaDoAprendido(desfeito)).toEqual({ tipo: 'estado', etiqueta: 'desfeito em outro aparelho' })
    expect(linhaDoAprendido(trocado)).toEqual({ tipo: 'estado', etiqueta: 'trocado em outro aparelho' })
    expect(linhaDoAprendido({ ...sf, situacao: 'qualquer' })).toEqual({ tipo: 'desconhecida' })
    expect(linhaDoAprendido({ ...sf, situacao: '__proto__' })).toEqual({ tipo: 'desconhecida' })
  })

  // Os nomes do fio não são os do espelho do Agent (internal/storage/aprendido_sqlite.go):
  // o espelho diz `desfeito_em_outro` e `trocado_em_outro`, e a F8g traduz para
  // `..._aparelho` ao emitir (06, "A tradução do espelho para o fio"). As outras
  // situações do espelho nunca saem em `situacao`. Aqui o painel só reconhece os três do
  // fio: um nome do espelho que escapasse sem tradução não vira linha (nem etiqueta), e
  // este teste é o que fixa quais são os três.
  it('só os três nomes do fio viram linha: os do espelho do Agent, sem tradução, não', () => {
    for (const doEspelho of ['desfeito_em_outro', 'trocado_em_outro', 'aplicado', 'aplicando', 'desfeito_aqui', 'ja_vale_aqui', 'recusado']) {
      expect(linhaDoAprendido({ ...sf, situacao: doEspelho }), doEspelho).toEqual({ tipo: 'desconhecida' })
    }
    for (const doFio of ['nao_aplicado', 'desfeito_em_outro_aparelho', 'trocado_em_outro_aparelho']) {
      expect(linhaDoAprendido({ ...sf, situacao: doFio }).tipo, doFio).toBe('estado')
    }
  })
})
