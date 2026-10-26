import { describe, expect, it } from 'vitest'
import { contem, destacar, quandoNaBusca, recortar, resumirNota, semAcento, textoDoDitado } from './destaque'

// marcados é o que o destaque marca, em ordem.
const marcados = (texto: string, termo: string) => destacar(texto, termo).filter((t) => t.marcado).map((t) => t.texto)

describe('semAcento e contem', () => {
  it('põe em minúscula e tira o acento', () => {
    expect(semAcento('Reunião da Conceição, ÓTIMA')).toBe('reuniao da conceicao, otima')
  })

  it('acha sem diferenciar maiúscula nem acento, nos dois lados', () => {
    expect(contem('Reunião com a Rosângela', 'reuniao')).toBe(true)
    expect(contem('reuniao com a rosangela', 'REUNIÃO')).toBe(true)
    expect(contem('FGTS digital', 'fgts')).toBe(true)
    expect(contem('FGTS digital', 'fgtss')).toBe(false)
  })

  it('termo vazio ou só de espaços não casa com nada', () => {
    expect(contem('texto', '')).toBe(false)
    expect(contem('texto', '   ')).toBe(false)
  })

  it('texto escrito em NFD (acento solto) casa com o termo normal e com o sem acento', () => {
    const nfd = 'Reunião'
    expect(contem(nfd, 'reunião')).toBe(true)
    expect(contem(nfd, 'reuniao')).toBe(true)
  })
})

describe('destacar', () => {
  it('marca cada ocorrência e devolve o texto original, letra por letra', () => {
    const partes = destacar('O FGTS da padaria e o fgts do bar.', 'fgts')
    expect(partes.map((p) => p.texto).join('')).toBe('O FGTS da padaria e o fgts do bar.')
    expect(marcados('O FGTS da padaria e o fgts do bar.', 'fgts')).toEqual(['FGTS', 'fgts'])
  })

  it('marca a palavra acentuada digitando sem acento, e o contrário', () => {
    expect(marcados('Faltam três coisas: a folha e a Conceição.', 'tres')).toEqual(['três'])
    expect(marcados('Faltam tres coisas.', 'três')).toEqual(['tres'])
    expect(marcados('Chamar a Conceição', 'CONCEICAO')).toEqual(['Conceição'])
  })

  it('o destaque vale para o texto em NFD sem cortar o acento solto', () => {
    const nfd = 'Reunião de hoje'
    const partes = destacar(nfd, 'reuniao')
    expect(partes.map((p) => p.texto).join('')).toBe(nfd)
    expect(marcados(nfd, 'reuniao')).toEqual(['Reunião'])
  })

  it('sem termo, ou sem ocorrência, é um pedaço só, sem marca', () => {
    expect(destacar('texto', '')).toEqual([{ texto: 'texto', marcado: false }])
    expect(destacar('texto', 'xyz')).toEqual([{ texto: 'texto', marcado: false }])
    expect(destacar('', 'a')).toEqual([{ texto: '', marcado: false }])
  })

  it('ocorrências coladas e termo com espaço', () => {
    expect(marcados('aaaa', 'aa')).toEqual(['aa', 'aa'])
    expect(marcados('Manda o Relatório hoje', 'o relat')).toEqual(['o Relat'])
  })

  it('caracteres de expressão regular no termo valem como texto', () => {
    expect(marcados('custa (R$ 10,50) hoje', '(r$ 10')).toEqual(['(R$ 10'])
    expect(marcados('a.b e axb', 'a.b')).toEqual(['a.b'])
  })
})

describe('recortar', () => {
  it('texto curto fica inteiro, numa linha só', () => {
    expect(recortar('um\ndois   três', 'dois')).toBe('um dois três')
  })

  it('texto longo é cortado em volta do termo, com reticências nas pontas cortadas', () => {
    const longo = `${'começo '.repeat(40)}o FGTS venceu${' fim'.repeat(60)}`
    const r = recortar(longo, 'fgts', 80)
    expect(r.startsWith('…')).toBe(true)
    expect(r.endsWith('…')).toBe(true)
    expect(r).toContain('FGTS')
    expect(r.length).toBeLessThanOrEqual(84)
  })

  it('termo no começo do texto longo: só a reticência do fim', () => {
    const r = recortar(`FGTS venceu${' fim'.repeat(60)}`, 'fgts', 60)
    expect(r.startsWith('FGTS')).toBe(true)
    expect(r.endsWith('…')).toBe(true)
  })

  it('sem o termo, fica o começo', () => {
    const r = recortar('palavra '.repeat(40), 'zzz', 40)
    expect(r.startsWith('palavra')).toBe(true)
    expect(r.endsWith('…')).toBe(true)
  })
})

describe('resumirNota', () => {
  it('o título e a primeira linha do corpo que casa, sem o prefixo da linha', () => {
    const r = resumirNota({ titulo: 'Fechamento de outubro', corpo: '# Pauta\n- DCTFWeb\n- [ ] Folha e FGTS\n- [x] outro FGTS' }, 'fgts')
    expect(r).toEqual({ titulo: 'Fechamento de outubro', linha: 'Folha e FGTS' })
  })

  it('o termo só no título: a linha é o começo do corpo', () => {
    const r = resumirNota({ titulo: 'FGTS de outubro', corpo: '- guia\n- recibo' }, 'fgts')
    expect(r).toEqual({ titulo: 'FGTS de outubro', linha: 'guia · recibo' })
  })

  it('sem título, só a linha que casa', () => {
    const r = resumirNota({ titulo: '  ', corpo: 'ligar para a Fernanda\nconferir o FGTS' }, 'fgts')
    expect(r).toEqual({ titulo: '', linha: 'conferir o FGTS' })
  })

  it('sem título e o termo só no prefixo do corpo (ou em nenhuma linha): linha vazia, sem quebrar', () => {
    expect(resumirNota({ titulo: '', corpo: '- [ ] leite' }, '[ ]')).toEqual({ titulo: '', linha: '' })
  })

  it('casa sem diferenciar acento', () => {
    const r = resumirNota({ titulo: 'Reunião', corpo: 'conferir a conceição' }, 'conceicao')
    expect(r.linha).toBe('conferir a conceição')
  })
})

describe('textoDoDitado', () => {
  it('o texto final, quando ele casa', () => {
    expect(textoDoDitado({ final_text: 'O FGTS venceu.', raw_text: 'o fgts venceu' }, 'fgts')).toBe('O FGTS venceu.')
  })

  it('o bruto, quando só ele casa', () => {
    expect(textoDoDitado({ final_text: 'Guia do imposto.', raw_text: 'guia do fgts' }, 'fgts')).toBe('guia do fgts')
  })

  it('o bruto, quando não há texto final', () => {
    expect(textoDoDitado({ final_text: '', raw_text: 'bruto' }, 'bru')).toBe('bruto')
  })

  it('nenhum casa (o termo estava num campo que a linha não mostra): o final', () => {
    expect(textoDoDitado({ final_text: 'final', raw_text: 'bruto' }, 'zzz')).toBe('final')
  })
})

describe('quandoNaBusca', () => {
  const hoje = new Date(2026, 9, 26, 15, 0)

  it('hoje é a hora; ontem é "ontem"; antes, o dia e o mês', () => {
    expect(quandoNaBusca(new Date(2026, 9, 26, 9, 5).toISOString(), hoje)).toBe('09:05')
    expect(quandoNaBusca(new Date(2026, 9, 25, 23, 0).toISOString(), hoje)).toBe('ontem')
    expect(quandoNaBusca(new Date(2026, 9, 22, 10, 0).toISOString(), hoje)).toBe('22 out')
  })

  it('de outro ano leva o ano', () => {
    expect(quandoNaBusca(new Date(2025, 11, 3, 10, 0).toISOString(), hoje)).toBe('3 dez 25')
  })
})
