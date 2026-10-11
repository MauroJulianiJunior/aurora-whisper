import { describe, expect, it } from 'vitest'
import { agruparPorDia, rotuloDoDia } from './datas'
import { ditado } from './testes/servidorFalso'

const hoje = new Date(2026, 8, 28, 18, 0)
const em = (dia: number, hora: number, ano = 2026) => new Date(ano, 8, dia, hora, 0).toISOString()

describe('rotuloDoDia', () => {
  it('hoje e ontem pelo nome, pelo calendário e não por 24 horas', () => {
    expect(rotuloDoDia(new Date(2026, 8, 28, 0, 5), hoje)).toBe('Hoje')
    expect(rotuloDoDia(new Date(2026, 8, 27, 23, 55), hoje)).toBe('Ontem')
  })

  it('dia mais antigo por extenso, com maiúscula; o ano só quando é outro', () => {
    expect(rotuloDoDia(new Date(2026, 8, 22, 9), hoje)).toBe('Terça-feira, 22 de setembro')
    expect(rotuloDoDia(new Date(2025, 8, 22, 9), hoje)).toBe('Segunda-feira, 22 de setembro de 2025')
  })

  it('virada de mês', () => {
    const primeiro = new Date(2026, 9, 1, 10)
    expect(rotuloDoDia(new Date(2026, 8, 30, 23), primeiro)).toBe('Ontem')
  })
})

describe('agruparPorDia', () => {
  it('mantém a ordem da API e abre grupo só quando o dia muda', () => {
    const itens = [
      ditado({ id: 'a', created_at: em(28, 15) }),
      ditado({ id: 'b', created_at: em(28, 9) }),
      ditado({ id: 'c', created_at: em(27, 20) }),
      ditado({ id: 'd', created_at: em(22, 8) }),
    ]
    const grupos = agruparPorDia(itens, hoje)
    expect(grupos.map((g) => [g.rotulo, g.itens.map((i) => i.id)])).toEqual([
      ['Hoje', ['a', 'b']],
      ['Ontem', ['c']],
      ['Terça-feira, 22 de setembro', ['d']],
    ])
  })
})
