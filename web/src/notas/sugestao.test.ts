// A sugestão de marcador (ADR-022, N4), por tabela: a regra é pura, então cada
// linha é um caso inteiro (notas dos marcadores, a nota nova, o que se espera).

import { describe, expect, it } from 'vitest'
import { type NotaParaSugerir, palavrasDe, sugerirMarcador } from './sugestao'

const marcadores = [
  { id: 'compras', nome: 'Compras' },
  { id: 'escritorio', nome: 'Escritório' },
]

let seq = 0
const nota = (marcadoresDaNota: string[], titulo: string, corpo = '', extra: Partial<NotaParaSugerir> = {}): NotaParaSugerir => ({
  id: `n${++seq}`,
  titulo,
  corpo,
  marcadores: marcadoresDaNota,
  ...extra,
})
const nova = (titulo: string, corpo = ''): NotaParaSugerir => ({ titulo, corpo, marcadores: [] })

const feira1 = () => nota(['compras'], 'Compras da semana', '- [ ] leite\n- [ ] café\n- [ ] ovos\n- [x] pão')
const feira2 = () => nota(['compras'], 'Mercado', '- leite\n- ovos\n- arroz\n- café')
const reuniao1 = () => nota(['escritorio'], 'Reunião com o contador', '# Pauta\n- conferir o extrato\n- boleto do aluguel')
const reuniao2 = () => nota(['escritorio'], 'Reunião da equipe', '- conferir o boleto\n- mandar o extrato ao contador')

describe('palavrasDe', () => {
  it('sem caixa, sem acento, sem prefixo de lista, sem número, sem palavra vazia e sem repetir', () => {
    const p = palavrasDe('Lista do SÁBADO', '- [ ] Café\n- [x] pão de forma\n1. 12345\n# Pauta\nCafé com leite para a casa 2x ir')
    expect([...p].sort()).toEqual(['cafe', 'casa', 'forma', 'leite', 'lista', 'pao', 'pauta', 'sabado'])
  })

  it('palavra de 1 ou 2 letras não conta', () => {
    expect([...palavrasDe('', 'ir ao de MG')]).toEqual([])
  })
})

describe('sugerirMarcador', () => {
  const casos: { caso: string; notas: NotaParaSugerir[]; nota: NotaParaSugerir; esperado: string | null }[] = [
    {
      caso: 'um marcador se destaca (a segunda lista de compras)',
      notas: [feira1(), feira2(), reuniao1(), reuniao2()],
      nota: nova('Lista do sábado', '- leite\n- pão\n- ovos'),
      esperado: 'Compras',
    },
    {
      caso: 'o outro marcador também se destaca quando o assunto é dele',
      notas: [feira1(), feira2(), reuniao1(), reuniao2()],
      nota: nova('', 'Reunião: conferir o extrato com o contador'),
      esperado: 'Escritório',
    },
    {
      caso: 'maiúscula e acento não atrapalham',
      notas: [feira1(), feira2()],
      nota: nova('CAFÉ e LEITE', ''),
      esperado: 'Compras',
    },
    {
      caso: 'o título conta, o prefixo de lista e a caixinha não',
      notas: [feira1(), feira2()],
      nota: nova('Leite', '- [ ] café'),
      esperado: 'Compras',
    },
    {
      caso: 'empate entre dois marcadores: nada',
      notas: [
        nota(['compras'], 'Casa', '- boleto\n- extrato'),
        nota(['compras'], 'Casa 2', '- boleto\n- extrato'),
        nota(['escritorio'], 'Gaveta', '- boleto\n- extrato'),
        nota(['escritorio'], 'Gaveta 2', '- boleto\n- extrato'),
      ],
      nota: nova('Papelada', '- boleto\n- extrato'),
      esperado: null,
    },
    {
      caso: 'o segundo chega perto (menos de 50% de diferença): nada',
      notas: [
        nota(['compras'], 'Feira', '- leite\n- ovos\n- boleto'),
        nota(['compras'], 'Feira 2', '- leite\n- ovos\n- boleto'),
        nota(['escritorio'], 'Contas', '- leite\n- ovos\n- boleto'),
        nota(['escritorio'], 'Contas 2', '- leite\n- ovos'),
      ],
      nota: nova('', '- leite\n- ovos\n- boleto'),
      esperado: null,
    },
    {
      caso: 'marcador com uma nota só não é candidato',
      notas: [feira1(), reuniao1(), reuniao2()],
      nota: nova('Lista', '- leite\n- café\n- ovos'),
      esperado: null,
    },
    {
      caso: 'a nota que já tem marcador não recebe sugestão',
      notas: [feira1(), feira2()],
      nota: { titulo: 'Lista', corpo: '- leite\n- café\n- ovos', marcadores: ['escritorio'] },
      esperado: null,
    },
    {
      caso: 'só uma palavra em comum: nada, mesmo que ela esteja em todas as notas',
      notas: [feira1(), feira2()],
      nota: nova('Presente', 'comprar leite'),
      esperado: null,
    },
    {
      caso: 'palavras vazias e números em comum não contam',
      notas: [nota(['compras'], 'Lista 1', 'para a casa de 2026'), nota(['compras'], 'Lista 2', 'para a casa de 2026')],
      nota: nova('Outra', 'para a casa de 2026, com o que tem'),
      esperado: null,
    },
    {
      caso: 'nada em comum: nada',
      notas: [feira1(), feira2(), reuniao1(), reuniao2()],
      nota: nova('Ideia de vídeo', 'Um Reels curto sobre a reforma tributária.'),
      esperado: null,
    },
    {
      caso: 'nota da lixeira não vale como exemplo do marcador',
      notas: [feira1(), nota(['compras'], 'Mercado', '- leite\n- ovos\n- café', { deleted_at: '2026-10-20T10:00:00Z' })],
      nota: nova('Lista', '- leite\n- café\n- ovos'),
      esperado: null,
    },
    {
      caso: 'sem nenhum marcador: nada',
      notas: [nota([], 'Solta', 'leite café ovos')],
      nota: nova('Lista', '- leite\n- café'),
      esperado: null,
    },
    {
      caso: 'nota quase vazia: nada',
      notas: [feira1(), feira2()],
      nota: nova('', 'leite'),
      esperado: null,
    },
  ]

  it.each(casos)('$caso', ({ notas, nota: n, esperado }) => {
    expect(sugerirMarcador(n, notas, marcadores)?.nome ?? null).toBe(esperado)
  })

  it('a própria nota, entre as outras, não conta como exemplo de si mesma', () => {
    const f1 = feira1()
    const eu: NotaParaSugerir = { id: 'eu', titulo: 'Mercado', corpo: '- leite\n- ovos\n- café', marcadores: [] }
    // Com ela fora, o marcador só tem 1 nota (f1); com ela dentro ainda teria 1: nada.
    expect(sugerirMarcador(eu, [f1, { ...eu, marcadores: ['compras'] }], marcadores)).toBeNull()
  })

  it('devolve os pontos: a fração das notas do marcador que usam cada palavra, somada', () => {
    const s = sugerirMarcador(nova('', '- leite\n- arroz\n- queijo'), [feira1(), feira2()], marcadores)
    // leite (2 de 2) + arroz (1 de 2) = 1,5; queijo não aparece.
    expect(s).toMatchObject({ id: 'compras', emComum: 2 })
    expect(s?.pontos).toBeCloseTo(1.5)
  })
})
