// O editor por linhas da nota (ADR-022, "A nota") na tela: Enter, Backspace,
// colar texto de várias linhas, o valor com quebra que chega de uma vez, os
// atalhos de prefixo, a lista de marcar e o Formatar. A regra de cada operação é
// de corpo.test.ts; aqui se prova que a tela a usa, e que o foco acompanha.
//
// O Agent insere o ditado no campo em foco colando o texto ou digitando as
// teclas (o "\n" chega como Enter): os dois caminhos têm teste aqui.

import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { act } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CamposDaNota } from './CamposDaNota'
import { useEditorDeLinhas } from './useEditorDeLinhas'
import { useState } from 'react'

afterEach(cleanup)

// Harness monta o miolo da nota e mostra o corpo que sairia para o Agent.
function Harness({ inicial = '', titulo: tituloInicial = '' }: { inicial?: string; titulo?: string }) {
  const editor = useEditorDeLinhas(inicial)
  const [titulo, setTitulo] = useState(tituloInicial)
  return (
    <>
      <CamposDaNota titulo={titulo} aoMudarTitulo={setTitulo} editor={editor} rotulo="Texto da nota" />
      <output data-testid="corpo">{editor.corpo}</output>
      <output data-testid="titulo">{titulo}</output>
    </>
  )
}

const corpo = () => screen.getByTestId('corpo').textContent
const campos = () => screen.getAllByRole<HTMLTextAreaElement>('textbox').filter((e) => e.tagName === 'TEXTAREA')
const escrever = (campo: HTMLElement, valor: string) => fireEvent.change(campo, { target: { value: valor } })
const enter = (campo: HTMLTextAreaElement, pos = campo.value.length) => {
  campo.setSelectionRange(pos, pos)
  fireEvent.keyDown(campo, { key: 'Enter' })
}
const backspace = (campo: HTMLTextAreaElement) => {
  campo.setSelectionRange(0, 0)
  fireEvent.keyDown(campo, { key: 'Backspace' })
}
const colar = (campo: HTMLTextAreaElement, texto: string) => fireEvent.paste(campo, { clipboardData: { getData: () => texto } })

describe('Enter', () => {
  it('parte a linha no cursor e leva o foco e o resto do texto para a linha nova', () => {
    render(<Harness inicial="ovos e leite" />)
    enter(campos()[0]!, 5)
    expect(campos().map((c) => c.value)).toEqual(['ovos ', 'e leite'])
    expect(document.activeElement).toBe(campos()[1])
    expect((document.activeElement as HTMLTextAreaElement).selectionStart).toBe(0)
    expect(corpo()).toBe('ovos \ne leite')
  })

  it('numa lista, a linha nova é do mesmo tipo (e a tarefa nova nasce desmarcada)', () => {
    render(<Harness inicial="- [ ] a" />)
    enter(campos()[0]!)
    escrever(campos()[1]!, 'b')
    expect(corpo()).toBe('- [ ] a\n- [ ] b')
  })

  it('numa linha de lista vazia, sai da lista: vira parágrafo', () => {
    render(<Harness inicial="- a" />)
    enter(campos()[0]!)
    expect(campos()).toHaveLength(2)
    enter(campos()[1]!)
    // A linha vazia virou parágrafo e não vai para o corpo.
    expect(corpo()).toBe('- a')
    expect(campos()).toHaveLength(2)
    escrever(campos()[1]!, 'depois')
    expect(corpo()).toBe('- a\ndepois')
  })

  it('com Ctrl ou Alt não parte a linha (Ctrl+Enter é o fechar da janela)', () => {
    render(<Harness inicial="abc" />)
    const c = campos()[0]!
    c.setSelectionRange(1, 1)
    fireEvent.keyDown(c, { key: 'Enter', ctrlKey: true })
    expect(campos()).toHaveLength(1)
  })

  it('o ditado digitado pelo Agent: texto, Enter, texto, Enter, texto vira três linhas', () => {
    render(<Harness />)
    escrever(campos()[0]!, 'primeira')
    enter(campos()[0]!)
    escrever(campos()[1]!, 'segunda')
    enter(campos()[1]!)
    escrever(campos()[2]!, 'terceira')
    expect(corpo()).toBe('primeira\nsegunda\nterceira')
  })

  it('o ditado digitado com os prefixos de lista vira lista (a tecla "- " e depois o Enter)', () => {
    render(<Harness />)
    escrever(campos()[0]!, '-')
    // O "- " vira o marcador e sai do campo: o que vem depois é o texto do item.
    escrever(campos()[0]!, '- ')
    expect(campos()[0]!.value).toBe('')
    escrever(campos()[0]!, 'leite')
    enter(campos()[0]!)
    escrever(campos()[1]!, 'pão')
    expect(corpo()).toBe('- leite\n- pão')
  })
})

describe('Backspace', () => {
  it('no começo de uma linha de lista, tira o tipo e deixa o texto', () => {
    render(<Harness inicial="- a" />)
    backspace(campos()[0]!)
    expect(corpo()).toBe('a')
  })

  it('no começo de um parágrafo, junta ao anterior e leva o cursor ao ponto da junção', () => {
    render(<Harness inicial={'ab\ncd'} />)
    backspace(campos()[1]!)
    expect(campos().map((c) => c.value)).toEqual(['abcd'])
    expect(document.activeElement).toBe(campos()[0])
    expect((document.activeElement as HTMLTextAreaElement).selectionStart).toBe(2)
    expect(corpo()).toBe('abcd')
  })

  it('na primeira linha, já parágrafo, não faz nada além do que o navegador faria', () => {
    render(<Harness inicial="ab" />)
    const c = campos()[0]!
    c.setSelectionRange(0, 0)
    // Sem preventDefault: o evento segue para o navegador.
    expect(fireEvent.keyDown(c, { key: 'Backspace' })).toBe(true)
    expect(corpo()).toBe('ab')
  })

  it('Delete no fim da linha puxa a de baixo', () => {
    render(<Harness inicial={'ab\ncd'} />)
    const c = campos()[0]!
    c.setSelectionRange(2, 2)
    fireEvent.keyDown(c, { key: 'Delete' })
    expect(corpo()).toBe('abcd')
  })
})

describe('colar texto de várias linhas', () => {
  it('corta em linhas e lê o prefixo de cada uma', () => {
    render(<Harness />)
    colar(campos()[0]!, 'Compras\n- leite\n- pão\n1. um\n2. dois\n- [ ] ligar\n# Fim')
    expect(corpo()).toBe('Compras\n- leite\n- pão\n1. um\n2. dois\n- [ ] ligar\n# Fim')
    expect(campos()).toHaveLength(7)
    expect(screen.getAllByRole('checkbox')).toHaveLength(1)
  })

  it('a quebra de linha do Windows (\\r\\n) também corta', () => {
    render(<Harness />)
    colar(campos()[0]!, 'a\r\nb\r\nc')
    expect(corpo()).toBe('a\nb\nc')
  })

  it('no meio de uma linha, o que vinha depois do cursor vai para o fim da última linha colada', () => {
    render(<Harness inicial="ABCD" />)
    const c = campos()[0]!
    c.setSelectionRange(2, 2)
    colar(c, 'x\ny')
    expect(corpo()).toBe('ABx\nyCD')
  })

  it('o texto de uma linha só segue para o navegador (sem preventDefault)', () => {
    render(<Harness />)
    expect(colar(campos()[0]!, 'só uma linha')).toBe(true)
  })

  it('o valor com quebra que chega de uma vez no campo (a automação) corta do mesmo jeito', () => {
    render(<Harness />)
    escrever(campos()[0]!, '- a\n- b')
    expect(corpo()).toBe('- a\n- b')
    expect(campos().map((c) => c.value)).toEqual(['a', 'b'])
  })

  it('no título, a primeira linha fica no título e o resto vai para o corpo', () => {
    render(<Harness inicial="" />)
    fireEvent.paste(screen.getByRole('textbox', { name: 'Título' }), { clipboardData: { getData: () => 'Reunião\n- pauta\n- atas' } })
    expect(screen.getByTestId('titulo').textContent).toBe('Reunião')
    expect(corpo()).toBe('- pauta\n- atas')
  })

  it('Enter no título leva o foco ao corpo', () => {
    render(<Harness inicial="texto" titulo="T" />)
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Título' }), { key: 'Enter' })
    expect(document.activeElement).toBe(campos()[0])
  })
})

describe('os atalhos de prefixo', () => {
  it.each([
    ['- ', '- '],
    ['1. ', '1. '],
    ['[] ', '- [ ] '],
    ['[ ] ', '- [ ] '],
    ['# ', '# '],
  ])('digitar %j no começo de um parágrafo vira o tipo', (digitado, esperado) => {
    render(<Harness />)
    escrever(campos()[0]!, digitado + 'x')
    expect(corpo()).toBe(esperado + 'x')
  })

  it('só vale quando o texto cresce: apagar até sobrar "- " não converte', () => {
    // "-x" é um parágrafo; trocar o x por um espaço não faz o texto crescer.
    render(<Harness inicial="-x" />)
    escrever(campos()[0]!, '- ')
    expect(corpo()).toBe('- ')
    expect(campos()).toHaveLength(1)
    expect(screen.queryByText('•')).toBeNull()
  })

  it('o corpo gravado já lido: "- x" abre como item de lista, sem o prefixo no campo', () => {
    render(<Harness inicial="- x" />)
    expect(campos()[0]!.value).toBe('x')
  })

  it('"-" sem espaço, no meio de um parágrafo, não converte', () => {
    render(<Harness />)
    escrever(campos()[0]!, 'a - b')
    expect(corpo()).toBe('a - b')
  })
})

describe('a lista de marcar', () => {
  const abertos = () => campos().map((c) => c.value)

  it('marcar leva o item para baixo da divisória "N marcados"; a ordem da tela é a do corpo', () => {
    render(<Harness inicial={'- [ ] a\n- [ ] b\n- [ ] c'} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'a' }))
    expect(corpo()).toBe('- [ ] b\n- [ ] c\n- [x] a')
    expect(screen.getByRole('button', { name: /1 marcado/ })).toBeTruthy()
    expect(abertos()).toEqual(['b', 'c', 'a'])
  })

  it('o segundo marcado vai para o fim (os marcados ficam na ordem em que foram marcados)', () => {
    render(<Harness inicial={'- [ ] a\n- [ ] b\n- [ ] c'} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'b' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'a' }))
    expect(corpo()).toBe('- [ ] c\n- [x] b\n- [x] a')
    expect(screen.getByRole('button', { name: /2 marcados/ })).toBeTruthy()
  })

  it('desmarcar devolve o item ao fim dos abertos', () => {
    render(<Harness inicial={'- [ ] a\n- [x] b'} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'b' }))
    expect(corpo()).toBe('- [ ] a\n- [ ] b')
    expect(screen.queryByRole('button', { name: /marcado/ })).toBeNull()
  })

  it('a divisória recolhe e abre os marcados; recolhidos, saem da tela mas continuam no corpo', () => {
    render(<Harness inicial={'- [ ] a\n- [x] b'} />)
    const divisoria = screen.getByRole('button', { name: /1 marcado/ })
    expect(divisoria.getAttribute('aria-expanded')).toBe('true')
    expect(abertos()).toEqual(['a', 'b'])
    fireEvent.click(divisoria)
    expect(divisoria.getAttribute('aria-expanded')).toBe('false')
    expect(abertos()).toEqual(['a'])
    expect(corpo()).toBe('- [ ] a\n- [x] b')
  })

  it('o Enter num item aberto cria o novo antes dos marcados', () => {
    render(<Harness inicial={'- [ ] a\n- [x] b'} />)
    enter(campos()[0]!)
    escrever(campos()[1]!, 'c')
    expect(corpo()).toBe('- [ ] a\n- [ ] c\n- [x] b')
  })

  it('a caixa responde ao teclado como caixa de marcar (role checkbox, aria-checked)', () => {
    render(<Harness inicial="- [x] feito" />)
    const caixa = screen.getByRole('checkbox', { name: 'feito' })
    expect(caixa.getAttribute('aria-checked')).toBe('true')
  })
})

// O ditado do Agent chega tecla a tecla (ou colado) numa rajada: escreve o texto
// letra a letra, com o relógio parado, como o SendInput entrega.
function ditar(campo: HTMLTextAreaElement, texto: string) {
  let valor = campo.value
  for (const letra of texto) {
    valor += letra
    fireEvent.change(campo, { target: { value: valor } })
  }
}

describe('ditado numa linha de lista (25-out-2026)', () => {
  afterEach(() => vi.useRealTimers())

  it('a enumeração ditada vira um item por linha', () => {
    vi.useFakeTimers()
    render(<Harness inicial="- [ ] " />)
    ditar(campos()[0]!, 'Arroz, feijão, batata, presunto, queijo, tomate e alface.')
    act(() => vi.advanceTimersByTime(300))
    expect(corpo()).toBe('- [ ] Arroz\n- [ ] Feijão\n- [ ] Batata\n- [ ] Presunto\n- [ ] Queijo\n- [ ] Tomate\n- [ ] Alface')
    expect(document.activeElement).toBe(campos()[6])
  })

  it('a colagem de uma linha só também', () => {
    vi.useFakeTimers()
    render(<Harness inicial="- macarrão" />)
    escrever(campos()[0]!, 'Leite, pão e queijo.')
    act(() => vi.advanceTimersByTime(300))
    expect(corpo()).toBe('- Leite\n- Pão\n- Queijo')
  })

  it('o que a pessoa digita devagar não é separado', () => {
    vi.useFakeTimers()
    render(<Harness inicial="- [ ] " />)
    let valor = ''
    for (const letra of 'pão, de preferência integral, e leite') {
      valor += letra
      escrever(campos()[0]!, valor)
      act(() => vi.advanceTimersByTime(120))
    }
    act(() => vi.advanceTimersByTime(300))
    expect(corpo()).toBe('- [ ] pão, de preferência integral, e leite')
  })

  it('no parágrafo o texto fica, e o botão "Virar lista" separa em itens de marcar', () => {
    vi.useFakeTimers()
    render(<Harness inicial="" />)
    ditar(campos()[0]!, 'Arroz, feijão, batata e queijo.')
    act(() => vi.advanceTimersByTime(300))
    expect(corpo()).toBe('Arroz, feijão, batata e queijo.')
    fireEvent.click(screen.getByRole('button', { name: 'Virar lista' }))
    expect(corpo()).toBe('- [ ] Arroz\n- [ ] Feijão\n- [ ] Batata\n- [ ] Queijo')
    expect(screen.queryByRole('button', { name: 'Virar lista' })).toBeNull()
  })

  it('o botão não aparece em frase comum', () => {
    render(<Harness inicial="Ontem eu fui ao mercado, mas a padaria estava fechada." />)
    expect(screen.queryByRole('button', { name: 'Virar lista' })).toBeNull()
  })
})
