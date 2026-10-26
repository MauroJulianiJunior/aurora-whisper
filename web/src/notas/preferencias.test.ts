import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  COLUNAS_NO_CELULAR,
  COLUNAS_NO_PC,
  dispensarSugestao,
  gravarColunas,
  gravarForma,
  lerColunas,
  lerForma,
  limitar,
  sugestaoDispensada,
} from './preferencias'

afterEach(() => {
  vi.restoreAllMocks()
  window.localStorage.clear()
})

describe('a forma de ver', () => {
  it('o padrão são os quadradinhos', () => {
    expect(lerForma()).toBe('grade')
  })

  it('lembra a escolha', () => {
    gravarForma('detalhes')
    expect(lerForma()).toBe('detalhes')
    gravarForma('lista')
    expect(lerForma()).toBe('lista')
  })

  it('um valor que não é uma forma volta ao padrão', () => {
    window.localStorage.setItem('aurora.notas.forma', 'tabela')
    expect(lerForma()).toBe('grade')
  })
})

describe('as colunas', () => {
  it('os padrões: 4 no PC, 2 no celular', () => {
    expect(lerColunas(false)).toBe(4)
    expect(lerColunas(true)).toBe(2)
  })

  it('cada tamanho de tela lembra o seu número', () => {
    gravarColunas(false, 6)
    gravarColunas(true, 3)
    expect(lerColunas(false)).toBe(6)
    expect(lerColunas(true)).toBe(3)
  })

  it('o número fica dentro dos limites: 1 a 6 no PC, 1 a 3 no celular', () => {
    expect(limitar(0, COLUNAS_NO_PC)).toBe(1)
    expect(limitar(9, COLUNAS_NO_PC)).toBe(6)
    expect(limitar(9, COLUNAS_NO_CELULAR)).toBe(3)
    gravarColunas(true, 5)
    expect(lerColunas(true)).toBe(3)
    // Um valor gravado por uma versão com outro limite é trazido para dentro.
    window.localStorage.setItem('aurora.notas.colunas.celular', '6')
    expect(lerColunas(true)).toBe(3)
  })

  it('um valor estranho volta ao padrão', () => {
    window.localStorage.setItem('aurora.notas.colunas.pc', 'muitas')
    expect(lerColunas(false)).toBe(4)
    window.localStorage.setItem('aurora.notas.colunas.pc', '-2')
    expect(lerColunas(false)).toBe(4)
  })
})

describe('a sugestão de marcador dispensada', () => {
  it('lembra cada nota em que a pessoa disse "Agora não"', () => {
    expect(sugestaoDispensada('n1')).toBe(false)
    dispensarSugestao('n1')
    dispensarSugestao('n1')
    dispensarSugestao('n2')
    expect(sugestaoDispensada('n1')).toBe(true)
    expect(sugestaoDispensada('n2')).toBe(true)
    expect(sugestaoDispensada('n3')).toBe(false)
    expect(JSON.parse(window.localStorage.getItem('aurora.notas.sugestao.dispensadas')!)).toEqual(['n1', 'n2'])
  })

  it('guarda só as 300 mais recentes', () => {
    for (let i = 0; i < 305; i++) dispensarSugestao(`n${i}`)
    expect(sugestaoDispensada('n0')).toBe(false)
    expect(sugestaoDispensada('n4')).toBe(false)
    expect(sugestaoDispensada('n5')).toBe(true)
    expect(sugestaoDispensada('n304')).toBe(true)
  })

  it('um valor estranho no armazenamento vale como lista vazia', () => {
    window.localStorage.setItem('aurora.notas.sugestao.dispensadas', '{"a":1}')
    expect(sugestaoDispensada('a')).toBe(false)
    window.localStorage.setItem('aurora.notas.sugestao.dispensadas', 'não é json')
    expect(sugestaoDispensada('a')).toBe(false)
    dispensarSugestao('a')
    expect(sugestaoDispensada('a')).toBe(true)
  })
})

describe('com o armazenamento quebrado', () => {
  it('ler e gravar não derrubam nada, e valem os padrões', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('bloqueado', 'SecurityError')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('cheio', 'QuotaExceededError')
    })
    expect(() => gravarForma('lista')).not.toThrow()
    expect(() => gravarColunas(false, 3)).not.toThrow()
    expect(lerForma()).toBe('grade')
    expect(lerColunas(false)).toBe(4)
    expect(lerColunas(true)).toBe(2)
    expect(() => dispensarSugestao('n1')).not.toThrow()
    expect(sugestaoDispensada('n1')).toBe(false)
  })

  it('o acesso ao localStorage que já falha ao ser tocado também é engolido', () => {
    vi.spyOn(window, 'localStorage', 'get').mockImplementation(() => {
      throw new DOMException('bloqueado', 'SecurityError')
    })
    expect(() => gravarForma('lista')).not.toThrow()
    expect(lerForma()).toBe('grade')
    expect(lerColunas(true)).toBe(2)
  })
})
