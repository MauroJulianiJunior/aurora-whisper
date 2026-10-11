import { describe, expect, it } from 'vitest'
import { compararPalavras } from './diff'

describe('compararPalavras', () => {
  it('troca de uma palavra: o que saiu vem antes do que entrou', () => {
    expect(compararPalavras('Manda para o Tiago.', 'Manda para o Thiago.')).toEqual([
      { tipo: 'igual', texto: 'Manda para o' },
      { tipo: 'saiu', texto: 'Tiago.' },
      { tipo: 'entrou', texto: 'Thiago.' },
    ])
  })

  it('uma palavra virando duas (o alias que o resolver aplica)', () => {
    expect(compararPalavras('teste com o Whisperflow aberto', 'teste com o Wispr Flow aberto')).toEqual([
      { tipo: 'igual', texto: 'teste com o' },
      { tipo: 'saiu', texto: 'Whisperflow' },
      { tipo: 'entrou', texto: 'Wispr Flow' },
      { tipo: 'igual', texto: 'aberto' },
    ])
  })

  it('texto igual é um trecho só, e espaços extras não contam como mudança', () => {
    expect(compararPalavras('fator  R', ' fator R ')).toEqual([{ tipo: 'igual', texto: 'fator R' }])
  })

  it('palavra acrescentada no fim', () => {
    expect(compararPalavras('São três mil e quinhentos', 'São três mil e quinhentos reais')).toEqual([
      { tipo: 'igual', texto: 'São três mil e quinhentos' },
      { tipo: 'entrou', texto: 'reais' },
    ])
  })

  it('lados vazios', () => {
    expect(compararPalavras('', '')).toEqual([])
    expect(compararPalavras('', 'novo')).toEqual([{ tipo: 'entrou', texto: 'novo' }])
    expect(compararPalavras('velho', '')).toEqual([{ tipo: 'saiu', texto: 'velho' }])
  })

  // Revisão de 29-set-2026: a versão anterior comparava dois textos sem
  // nada em comum, e a LCS completa dá a mesma resposta que o teto — o
  // teste passava com o teto removido. Aqui os textos são quase iguais:
  // a LCS acharia trechos iguais, o teto troca o bloco todo.
  it('texto enorme não monta a tabela inteira: cai para troca do bloco todo', () => {
    const palavras = Array.from({ length: 600 }, (_, i) => `p${i}`)
    const a = palavras.join(' ')
    const b = palavras.map((p, i) => (i === 300 ? 'trocada' : p)).join(' ')
    expect(compararPalavras(a, b)).toEqual([
      { tipo: 'saiu', texto: a },
      { tipo: 'entrou', texto: b },
    ])
    expect(compararPalavras(a, a)).toEqual([{ tipo: 'igual', texto: a }])
  })

  it('abaixo do teto, o mesmo tipo de texto ganha o diff palavra a palavra', () => {
    const palavras = Array.from({ length: 400 }, (_, i) => `p${i}`)
    const b = palavras.map((p, i) => (i === 200 ? 'trocada' : p)).join(' ')
    expect(compararPalavras(palavras.join(' '), b).map((t) => t.tipo)).toEqual(['igual', 'saiu', 'entrou', 'igual'])
  })
})
