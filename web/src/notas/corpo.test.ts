import { describe, expect, it } from 'vitest'
import {
  type Linha,
  apagarMarcados,
  colar,
  contarItens,
  converter,
  criarLinha,
  desmarcarTudo,
  dividir,
  escreverCorpo,
  inicioDoCorpo,
  itensDaEnumeracao,
  lerCorpo,
  marcarLinha,
  nomeDaNota,
  normalizarMarcados,
  numeroDaLinha,
  prefixoDigitado,
  puxar,
  separarEmItens,
  trocarPorItens,
  voltar,
} from './corpo'
// Os casos da regra que o Agent também roda (ItensPorRegra, internal/resolver):
// as duas regras não podem divergir.
// Lidos pelo import.meta.glob, não por import: o build da imagem da nuvem
// (deploy/Dockerfile) só leva o web/, e um import estático faria o tsc dele
// exigir o arquivo. O teste abaixo confere que o arquivo foi achado.
type CasoDaRegra = { texto: string; minimo: number; itens: string[] }
const arquivosDaRegra = import.meta.glob<{ casos: CasoDaRegra[] }>('../../../testdata/notas-lista/regra.json', {
  eager: true,
  import: 'default',
})
const casosDaRegra = Object.values(arquivosDaRegra)[0] ?? { casos: [] }

// O que importa de uma linha, sem a chave (que só existe para a tela).
const forma = (ls: Linha[]) => ls.map(({ tipo, texto, feito }) => ({ tipo, texto, feito }))
const textos = (ls: Linha[]) => ls.map((l) => l.texto)

describe('lerCorpo e escreverCorpo', () => {
  // Cada caso: o corpo, e como ele é lido. O inverso tem de devolver o mesmo texto.
  const casos: [string, string, { tipo: string; texto: string; feito?: boolean }[]][] = [
    ['parágrafo', 'Ligar para o contador', [{ tipo: 'paragrafo', texto: 'Ligar para o contador' }]],
    ['título', '# Compras', [{ tipo: 'titulo', texto: 'Compras' }]],
    ['marcador', '- leite\n- pão', [{ tipo: 'marcador', texto: 'leite' }, { tipo: 'marcador', texto: 'pão' }]],
    ['numerada', '1. A\n2. B\n3. C', [{ tipo: 'numerada', texto: 'A' }, { tipo: 'numerada', texto: 'B' }, { tipo: 'numerada', texto: 'C' }]],
    [
      'lista de marcar, com e sem marca',
      '- [ ] café\n- [x] ovos',
      [{ tipo: 'tarefa', texto: 'café', feito: false }, { tipo: 'tarefa', texto: 'ovos', feito: true }],
    ],
    ['linhas em branco entre parágrafos', 'a\n\nb', [{ tipo: 'paragrafo', texto: 'a' }, { tipo: 'paragrafo', texto: '' }, { tipo: 'paragrafo', texto: 'b' }]],
    ['quebra no fim', 'a\n', [{ tipo: 'paragrafo', texto: 'a' }, { tipo: 'paragrafo', texto: '' }]],
    [
      'tudo junto',
      '# Mercado\n- [ ] leite\n- [x] pão\nObs: fecha às 22h\n1. um\n2. dois',
      [
        { tipo: 'titulo', texto: 'Mercado' },
        { tipo: 'tarefa', texto: 'leite', feito: false },
        { tipo: 'tarefa', texto: 'pão', feito: true },
        { tipo: 'paragrafo', texto: 'Obs: fecha às 22h' },
        { tipo: 'numerada', texto: 'um' },
        { tipo: 'numerada', texto: 'dois' },
      ],
    ],
    ['hífen no meio do texto não é marcador', 'um - dois', [{ tipo: 'paragrafo', texto: 'um - dois' }]],
    ['hífen sem espaço não é marcador', '-leite', [{ tipo: 'paragrafo', texto: '-leite' }]],
    ['jogo da velha sem espaço não é título', '#hashtag', [{ tipo: 'paragrafo', texto: '#hashtag' }]],
    ['"3." sem texto depois não é numerada', '2026.', [{ tipo: 'paragrafo', texto: '2026.' }]],
    ['colchetes sem o hífen não são tarefa', '[ ] x', [{ tipo: 'paragrafo', texto: '[ ] x' }]],
  ]

  it.each(casos)('%s', (_, corpo, esperado) => {
    const linhas = lerCorpo(corpo)
    expect(forma(linhas)).toEqual(esperado.map((e) => ({ feito: false, ...e })))
    expect(escreverCorpo(linhas)).toBe(corpo)
  })

  it('o corpo vazio não tem linha nenhuma', () => {
    expect(lerCorpo('')).toEqual([])
    expect(escreverCorpo([])).toBe('')
  })

  it('o fim de linha do Windows (\\r\\n) e o \\r sozinho são quebras de linha', () => {
    const linhas = lerCorpo('# Lista\r\n- [x] a\r\n- b\r1. c')
    expect(forma(linhas).map((l) => l.tipo)).toEqual(['titulo', 'tarefa', 'marcador', 'numerada'])
    // Não sobra \r em texto nenhum, e o corpo gravado usa \n.
    expect(textos(linhas)).toEqual(['Lista', 'a', 'b', 'c'])
    expect(escreverCorpo(linhas)).toBe('# Lista\n- [x] a\n- b\n1. c')
  })

  it('tarefa vazia, com e sem o espaço depois do colchete; [X] maiúsculo conta como marcada', () => {
    expect(forma(lerCorpo('- [ ]'))).toEqual([{ tipo: 'tarefa', texto: '', feito: false }])
    expect(forma(lerCorpo('- [ ] '))).toEqual([{ tipo: 'tarefa', texto: '', feito: false }])
    expect(forma(lerCorpo('- [X] feito'))).toEqual([{ tipo: 'tarefa', texto: 'feito', feito: true }])
  })

  it('a numeração é refeita, e recomeça depois de uma linha que não é numerada', () => {
    const corpo = '3. c\n7. d\nparágrafo\n9. e'
    expect(escreverCorpo(lerCorpo(corpo))).toBe('1. c\n2. d\nparágrafo\n1. e')
    const linhas = lerCorpo(corpo)
    expect([0, 1, 2, 3].map((i) => numeroDaLinha(linhas, i))).toEqual([1, 2, 0, 1])
  })

  it('o prefixo escrito dentro do texto de uma linha lê de novo igual', () => {
    for (const corpo of ['- [x] - [ ] dentro', '# - título', '- 1. mistura']) expect(escreverCorpo(lerCorpo(corpo))).toBe(corpo)
  })

  it('limpar: tira o item vazio e os parágrafos vazios do fim, e deixa os do meio', () => {
    const linhas = [criarLinha('titulo', 'A'), criarLinha('marcador', ''), criarLinha('paragrafo', ''), criarLinha('marcador', 'b'), criarLinha('tarefa', ''), criarLinha('paragrafo', ''), criarLinha('paragrafo', '')]
    expect(escreverCorpo(linhas, { limpar: true })).toBe('# A\n\n- b')
    expect(escreverCorpo([criarLinha('paragrafo', ''), criarLinha('marcador', '')], { limpar: true })).toBe('')
  })
})

describe('a lista de marcar', () => {
  const lista = () => lerCorpo('- [ ] a\n- [ ] b\n- [x] c\n- [ ] d')

  it('normalizarMarcados leva os marcados para o fim, na ordem em que estavam', () => {
    expect(escreverCorpo(normalizarMarcados(lista()))).toBe('- [ ] a\n- [ ] b\n- [ ] d\n- [x] c')
    expect(escreverCorpo(normalizarMarcados(lerCorpo('- [x] 1\n- [ ] 2\n- [x] 3')))).toBe('- [ ] 2\n- [x] 1\n- [x] 3')
  })

  it('marcar desce o item para baixo dos outros itens; desmarcar o devolve ao fim dos abertos', () => {
    const l = lista()
    const marcada = marcarLinha(l, l[0]!.chave, true)
    expect(escreverCorpo(marcada)).toBe('- [ ] b\n- [ ] d\n- [x] c\n- [x] a')
    // Desmarcar o "c": ele sobe e vai para o fim dos abertos, abaixo do "d".
    const c = marcada.find((x) => x.texto === 'c')!
    expect(escreverCorpo(marcarLinha(marcada, c.chave, false))).toBe('- [ ] b\n- [ ] d\n- [ ] c\n- [x] a')
  })

  it('marcar uma linha que não é tarefa não faz nada', () => {
    const l = lerCorpo('- bullet')
    expect(escreverCorpo(marcarLinha(l, l[0]!.chave, true))).toBe('- bullet')
  })

  it('desmarcar tudo e apagar marcados', () => {
    expect(escreverCorpo(desmarcarTudo(lista()))).toBe('- [ ] a\n- [ ] b\n- [ ] c\n- [ ] d')
    expect(escreverCorpo(apagarMarcados(lista()))).toBe('- [ ] a\n- [ ] b\n- [ ] d')
  })

  it('contarItens: os marcados e o total de tarefas', () => {
    expect(contarItens(lista())).toEqual({ feitos: 1, total: 4 })
    expect(contarItens(lerCorpo('- a\ntexto'))).toEqual({ feitos: 0, total: 0 })
  })
})

describe('resumos', () => {
  it('inicioDoCorpo junta as linhas, sem os itens marcados, e corta com reticências', () => {
    expect(inicioDoCorpo(lerCorpo('# Compras\n- [ ] leite\n- [x] pão\n\n- café'))).toBe('Compras · leite · café')
    const longo = inicioDoCorpo(lerCorpo('palavra '.repeat(40)), 50)
    expect(longo).toHaveLength(50)
    expect(longo.endsWith('…')).toBe(true)
  })

  it('nomeDaNota: o título, ou a primeira linha com texto, ou "Nota vazia"', () => {
    expect(nomeDaNota('  Mercado ', lerCorpo('- a'))).toBe('Mercado')
    expect(nomeDaNota('', lerCorpo('\n- leite'))).toBe('leite')
    expect(nomeDaNota('', [])).toBe('Nota vazia')
    expect(nomeDaNota('', lerCorpo('x'.repeat(100)))).toHaveLength(60)
  })
})

describe('dividir (Enter)', () => {
  it('parte a linha no cursor, e a de baixo tem o mesmo tipo', () => {
    const l = lerCorpo('- leite integral')
    const r = dividir(l, 0, 5)
    expect(forma(r.linhas)).toEqual([
      { tipo: 'marcador', texto: 'leite', feito: false },
      { tipo: 'marcador', texto: ' integral', feito: false },
    ])
    expect(r.foco).toEqual({ chave: r.linhas[1]!.chave, pos: 0 })
  })

  it('a tarefa nova nasce desmarcada, abaixo das abertas, mesmo vindo de uma marcada', () => {
    const l = lerCorpo('- [ ] a\n- [x] b')
    const r = dividir(l, 1, 1)
    expect(escreverCorpo(r.linhas)).toBe('- [ ] a\n- [ ] \n- [x] b')
    expect(r.linhas[1]!.chave).toBe(r.foco.chave)
  })

  it('depois de um título vem um parágrafo', () => {
    expect(forma(dividir(lerCorpo('# Título'), 0, 6).linhas).map((l) => l.tipo)).toEqual(['titulo', 'paragrafo'])
  })

  it('Enter no item vazio sai da lista: vira parágrafo, no mesmo lugar', () => {
    const l = lerCorpo('- a\n- ')
    const r = dividir(l, 1, 0)
    expect(forma(r.linhas)).toEqual([
      { tipo: 'marcador', texto: 'a', feito: false },
      { tipo: 'paragrafo', texto: '', feito: false },
    ])
    expect(r.foco).toEqual({ chave: l[1]!.chave, pos: 0 })
    // E o numerado e o de marcar também.
    expect(dividir(lerCorpo('1. '), 0, 0).linhas[0]!.tipo).toBe('paragrafo')
    expect(dividir(lerCorpo('- [ ] '), 0, 0).linhas[0]!.tipo).toBe('paragrafo')
  })

  it('Enter num parágrafo vazio faz outro parágrafo', () => {
    expect(dividir([criarLinha()], 0, 0).linhas).toHaveLength(2)
  })
})

describe('voltar (Backspace no começo da linha)', () => {
  it('a linha com tipo perde o tipo e fica o texto', () => {
    const l = lerCorpo('- [x] leite')
    const r = voltar(l, 0)!
    expect(forma(r.linhas)).toEqual([{ tipo: 'paragrafo', texto: 'leite', feito: false }])
    expect(r.foco).toEqual({ chave: l[0]!.chave, pos: 0 })
  })

  it('o parágrafo se junta ao anterior, e o cursor fica na emenda', () => {
    const l = lerCorpo('- leite\npão')
    const r = voltar(l, 1)!
    expect(forma(r.linhas)).toEqual([{ tipo: 'marcador', texto: 'leitepão', feito: false }])
    expect(r.foco).toEqual({ chave: l[0]!.chave, pos: 5 })
  })

  it('o parágrafo vazio some', () => {
    const l = lerCorpo('a\n')
    expect(textos(voltar(l, 1)!.linhas)).toEqual(['a'])
  })

  it('na primeira linha, parágrafo, não há o que fazer', () => {
    expect(voltar(lerCorpo('a'), 0)).toBeNull()
  })

  it('o item vazio de uma lista precisa de dois Backspace para sumir', () => {
    const l = lerCorpo('- a\n- ')
    const um = voltar(l, 1)!
    expect(forma(um.linhas).map((x) => x.tipo)).toEqual(['marcador', 'paragrafo'])
    const dois = voltar(um.linhas, 1)!
    expect(textos(dois.linhas)).toEqual(['a'])
  })
})

describe('puxar (Delete no fim da linha)', () => {
  it('a linha de baixo sobe para esta', () => {
    const l = lerCorpo('- a\n- b')
    const r = puxar(l, 0)!
    expect(forma(r.linhas)).toEqual([{ tipo: 'marcador', texto: 'ab', feito: false }])
    expect(r.foco).toEqual({ chave: l[0]!.chave, pos: 1 })
  })

  it('na última linha, nada', () => {
    expect(puxar(lerCorpo('a'), 0)).toBeNull()
  })
})

describe('colar', () => {
  it('texto de várias linhas vira várias linhas, lendo o prefixo de cada uma', () => {
    const r = colar([criarLinha()], 0, 0, 0, 'Preciso comprar:\n- detergente\n- papel toalha\n1. um\n- [ ] café\n- [x] ovos')
    expect(forma(r.linhas)).toEqual([
      { tipo: 'paragrafo', texto: 'Preciso comprar:', feito: false },
      { tipo: 'marcador', texto: 'detergente', feito: false },
      { tipo: 'marcador', texto: 'papel toalha', feito: false },
      { tipo: 'numerada', texto: 'um', feito: false },
      { tipo: 'tarefa', texto: 'café', feito: false },
      { tipo: 'tarefa', texto: 'ovos', feito: true },
    ])
    expect(r.foco.chave).toBe(r.linhas[r.linhas.length - 1]!.chave)
    expect(r.foco.pos).toBe(4)
  })

  it('o fim de linha do Windows também quebra', () => {
    const r = colar([criarLinha()], 0, 0, 0, 'a\r\n- b\r\nc')
    expect(forma(r.linhas).map((l) => [l.tipo, l.texto])).toEqual([
      ['paragrafo', 'a'],
      ['marcador', 'b'],
      ['paragrafo', 'c'],
    ])
  })

  it('no meio de uma linha: o começo fica, o fim desce para a última linha colada', () => {
    const l = [criarLinha('paragrafo', 'AB')]
    const r = colar(l, 0, 1, 1, 'x\ny')
    expect(textos(r.linhas)).toEqual(['Ax', 'yB'])
    // O cursor fica entre o que foi colado e o que já estava lá.
    expect(r.foco).toEqual({ chave: r.linhas[1]!.chave, pos: 1 })
  })

  it('substitui a seleção', () => {
    const r = colar([criarLinha('paragrafo', 'abcdef')], 0, 1, 4, 'X\nY')
    expect(textos(r.linhas)).toEqual(['aX', 'Yef'])
  })

  it('a primeira linha só lê o prefixo se estiver no começo da linha', () => {
    const noMeio = colar([criarLinha('paragrafo', 'texto ')], 0, 6, 6, '- item\n- outro')
    expect(forma(noMeio.linhas).map((l) => [l.tipo, l.texto])).toEqual([
      ['paragrafo', 'texto - item'],
      ['marcador', 'outro'],
    ])
    const noComeco = colar([criarLinha()], 0, 0, 0, '- item\n- outro')
    expect(noComeco.linhas.map((l) => l.tipo)).toEqual(['marcador', 'marcador'])
  })

  it('a linha colada sem prefixo, dentro de uma lista, continua a lista; a vazia fica parágrafo', () => {
    const r = colar([criarLinha('tarefa', '')], 0, 0, 0, 'leite\npão\n\ncafé')
    expect(forma(r.linhas).map((l) => [l.tipo, l.texto])).toEqual([
      ['tarefa', 'leite'],
      ['tarefa', 'pão'],
      ['paragrafo', ''],
      ['tarefa', 'café'],
    ])
  })

  it('o item marcado colado vai para o fim, abaixo dos abertos', () => {
    const r = colar([criarLinha()], 0, 0, 0, '- [x] feito\n- [ ] aberto')
    expect(escreverCorpo(r.linhas)).toBe('- [ ] aberto\n- [x] feito')
  })

  it('texto de uma linha só, sem prefixo, entra no lugar da seleção', () => {
    const r = colar([criarLinha('marcador', 'abc')], 0, 3, 3, 'def')
    expect(forma(r.linhas)).toEqual([{ tipo: 'marcador', texto: 'abcdef', feito: false }])
    expect(r.foco.pos).toBe(6)
  })

  it('a colagem entre outras linhas não mexe nas vizinhas', () => {
    const l = lerCorpo('antes\n\ndepois')
    const r = colar(l, 1, 0, 0, '- x\n- y')
    expect(escreverCorpo(r.linhas)).toBe('antes\n- x\n- y\ndepois')
  })
})

describe('prefixoDigitado', () => {
  it.each([
    ['- ', 'marcador', ''],
    ['- leite', 'marcador', 'leite'],
    ['1. ', 'numerada', ''],
    ['12. dois', 'numerada', 'dois'],
    ['[] ', 'tarefa', ''],
    ['[ ] café', 'tarefa', 'café'],
    ['- [ ] ', 'tarefa', ''],
    ['# ', 'titulo', ''],
  ])('digitar "%s" num parágrafo vira %s', (digitado, tipo, texto) => {
    expect(prefixoDigitado('paragrafo', '', digitado)).toMatchObject({ tipo, texto })
  })

  it('"[x] " vem marcada', () => {
    expect(prefixoDigitado('paragrafo', '', '[x] ovos')).toMatchObject({ tipo: 'tarefa', texto: 'ovos', feito: true })
  })

  it('só vale em parágrafo, e só quando o texto cresceu', () => {
    expect(prefixoDigitado('marcador', '', '- ')).toBeNull()
    expect(prefixoDigitado('paragrafo', '- x', '- ')).toBeNull()
    expect(prefixoDigitado('paragrafo', '', 'texto comum')).toBeNull()
    expect(prefixoDigitado('paragrafo', '', '-')).toBeNull()
  })
})

describe('converter', () => {
  it('troca só o tipo das linhas dadas, e o texto fica', () => {
    const l = lerCorpo('a\nb\nc')
    const r = converter(l, new Set([l[0]!.chave, l[2]!.chave]), 'marcador')
    expect(escreverCorpo(r)).toBe('- a\nb\n- c')
  })

  it('a tarefa que deixa de ser tarefa perde a marca; quem vira tarefa nasce desmarcada', () => {
    const l = lerCorpo('- [x] a\nb')
    const todas = new Set(l.map((x) => x.chave))
    expect(escreverCorpo(converter(l, todas, 'marcador'))).toBe('- a\n- b')
    expect(escreverCorpo(converter(l, todas, 'tarefa'))).toBe('- [ ] a\n- [ ] b')
    expect(escreverCorpo(converter(l, todas, 'titulo'))).toBe('# a\n# b')
  })
})

describe('itensDaEnumeracao', () => {
  it.each([
    ['Arroz, feijão, batata, presunto, queijo, tomate e alface.', ['Arroz', 'Feijão', 'Batata', 'Presunto', 'Queijo', 'Tomate', 'Alface']],
    ['arroz, feijão e batata', ['Arroz', 'Feijão', 'Batata']],
    ['leite; pão; café', ['Leite', 'Pão', 'Café']],
    ['1,5 kg de carne, duas dúzias de ovos e pão', ['1,5 kg de carne', 'Duas dúzias de ovos', 'Pão']],
    ['DCTFWeb, PGDAS e ECF', ['DCTFWeb', 'PGDAS', 'ECF']],
  ])('%s', (texto, itens) => {
    expect(itensDaEnumeracao(texto, 3)).toEqual(itens)
  })

  it.each([
    ['sem vírgula', 'arroz e feijão'],
    ['frase com vírgula', 'Ontem eu fui ao mercado, mas a padaria estava fechada e voltei para casa.'],
    ['pergunta', 'Você comprou arroz, feijão e batata?'],
    ['com dois-pontos', 'Preciso comprar: arroz, feijão e batata'],
    ['número com vírgula só', 'O peso deu 1,5 kg'],
  ])('não é enumeração: %s', (_, texto) => {
    expect(itensDaEnumeracao(texto, 3)).toBeNull()
  })

  it('o mínimo de itens vale', () => {
    expect(itensDaEnumeracao('arroz, feijão', 3)).toBeNull()
    expect(itensDaEnumeracao('arroz, feijão', 2)).toEqual(['Arroz', 'Feijão'])
  })
})

describe('a regra é a mesma do Agent (testdata/notas-lista/regra.json)', () => {
  it('acha o arquivo dos casos', () => {
    expect(casosDaRegra.casos.length).toBeGreaterThan(0)
  })
  it.each(casosDaRegra.casos.map((c) => [c.texto, c.minimo, c.itens] as const))('%s (mínimo %i)', (texto, minimo, itens) => {
    expect(itensDaEnumeracao(texto, minimo)).toEqual(itens)
  })

  it('a sigla do começo do item não ganha maiúscula', () => {
    expect(itensDaEnumeracao('eSocial, iPhone e pão', 2)).toEqual(['eSocial', 'iPhone', 'Pão'])
  })
})

describe('trocarPorItens', () => {
  it('troca a linha pelos itens dados, no tipo pedido, e o primeiro fica com a chave', () => {
    const [a, b] = [criarLinha('paragrafo', 'leite pão café'), criarLinha('paragrafo', 'depois')]
    const r = trocarPorItens([a, b], a.chave, ['Leite', 'Pão', 'Café'], 'numerada')!
    expect(escreverCorpo(r.linhas)).toBe('1. Leite\n2. Pão\n3. Café\ndepois')
    expect(r.linhas[0]!.chave).toBe(a.chave)
    expect(r.foco).toEqual({ chave: r.linhas[2]!.chave, pos: 'Café'.length })
  })

  it('sem itens, ou sem a linha, null', () => {
    const a = criarLinha('paragrafo', 'x')
    expect(trocarPorItens([a], a.chave, [])).toBeNull()
    expect(trocarPorItens([a], a.chave + 999, ['a', 'b'])).toBeNull()
  })
})

describe('separarEmItens', () => {
  it('a linha de lista vira um item por linha, no tipo dela, com o foco no último', () => {
    const [a, b] = [criarLinha('tarefa', 'Arroz, feijão e batata.'), criarLinha('tarefa', 'feito', true)]
    const r = separarEmItens([a, b], a.chave, 2)!
    expect(escreverCorpo(r.linhas)).toBe('- [ ] Arroz\n- [ ] Feijão\n- [ ] Batata\n- [x] feito')
    expect(r.linhas[0]!.chave).toBe(a.chave)
    expect(r.foco).toEqual({ chave: r.linhas[2]!.chave, pos: 'Batata'.length })
  })

  it('o parágrafo vira o tipo pedido', () => {
    const a = criarLinha('paragrafo', 'leite, pão e café')
    expect(escreverCorpo(separarEmItens([a], a.chave, 3, 'tarefa')!.linhas)).toBe('- [ ] Leite\n- [ ] Pão\n- [ ] Café')
  })

  it('sem enumeração, null', () => {
    const a = criarLinha('tarefa', 'leite')
    expect(separarEmItens([a], a.chave, 2)).toBeNull()
  })
})
