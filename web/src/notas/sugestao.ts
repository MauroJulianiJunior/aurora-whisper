// A sugestão de marcador (ADR-022, "Sugestão de marcador" e E2): local, sem IA,
// nada sai do PC. Para uma nota SEM marcador, compara as palavras dela com as
// das notas de cada marcador e diz qual deles se destaca, ou nenhum. Nunca
// marca: quem decide é a pessoa, na faixa da nota aberta.
//
// Puro (sem React, sem rede), para a regra ficar testável por tabela.
//
// A regra, em quatro passos:
//
//  1. Palavras. De cada nota entram o título e o texto de cada linha (sem o
//     prefixo de lista nem o "[ ]" da caixinha), em minúscula e sem acento, cortadas
//     onde não há letra nem dígito. Ficam as que têm 3 letras ou mais, que não são só
//     número e que não estão na lista de palavras vazias. Cada palavra conta UMA vez
//     por nota: uma nota longa, ou que repete a palavra, não pesa mais.
//  2. Candidatos. Só marcadores com 2 notas ou mais (a decisão do Mauro: com 1 nota o
//     painel erraria muito). A nota sendo olhada e as da lixeira ficam de fora.
//  3. Pontos. Para cada palavra que a nota tem em comum com as notas do marcador, vale a
//     fração das notas do marcador que a usam (1 se todas, 0,5 se metade). A soma é a
//     nota do marcador: é a "frequência dentro do marcador". Palavra que o marcador usa
//     pouco pesa pouco; palavra que ele repete em toda nota pesa o máximo.
//  4. Destaque. Sugere o melhor se tiver pelo menos 2 palavras em comum E 1 ponto, e se
//     passar do segundo colocado por 50% (o empate, ou uma diferença pequena, não
//     sugere nada).
//
// Fica de fora: marcador que só tem notas arquivadas (a tela passa as notas da vista
// principal) e a ordem das palavras (é um saco de palavras, sem sentido).

import { lerCorpo } from './corpo'
import { dobrar } from './marcadores'

// Os limiares da regra (exportados para os testes e para a documentação).
export const MIN_NOTAS_DO_MARCADOR = 2
export const MIN_PALAVRAS_EM_COMUM = 2
export const MIN_PONTOS = 1
// O melhor precisa de pelo menos MARGEM vezes os pontos do segundo.
export const MARGEM = 1.5

// Palavras vazias do português do dia a dia (já sem acento: a comparação é depois
// de dobrar). Só as que aparecem em qualquer assunto; as que dizem o assunto
// (leite, reunião, contador) não entram.
const palavrasVazias = new Set(
  (
    'a ao aos as com como da das de dela delas dele deles depois do dos e ela elas ele eles em entre era eram essa essas esse esses esta estas este estes ' +
    'eu foi for foram ha isso isto ja la mais mas me mesmo meu meus minha minhas muito na nas nao nem no nos nossa nosso num numa o os ou para pela pelas ' +
    'pelo pelos por pra qual quando que quem se sem ser seu seus sua suas so sobre tambem te tem tinha tudo um uma umas uns voce voces vai vamos vou ' +
    'ter tenho temos esta estao estou fazer fazendo falta ainda alem amanha ate cada coisa coisas hoje onde ontem outra outras outro outros ' +
    'pode podem quero precisa precisar preciso sao sera ver agora aqui bem entao hora tipo'
  ).split(' '),
)

// palavrasDe devolve as palavras da nota, sem repetir (passo 1).
export function palavrasDe(titulo: string, corpo: string): Set<string> {
  const textos = [titulo, ...lerCorpo(corpo).map((l) => l.texto)]
  const palavras = new Set<string>()
  for (const texto of textos) {
    for (const p of dobrar(texto).split(/[^\p{L}\p{N}]+/u)) {
      if (p.length >= 3 && !/^\p{N}+$/u.test(p) && !palavrasVazias.has(p)) palavras.add(p)
    }
  }
  return palavras
}

// O que a regra lê de uma nota. Nota é a do painel, mas só estes campos importam.
export interface NotaParaSugerir {
  id?: string
  titulo: string
  corpo: string
  marcadores: readonly string[]
  deleted_at?: string
}

export interface SugestaoDeMarcador {
  id: string
  nome: string
  pontos: number
  emComum: number
}

// sugerirMarcador devolve o marcador que se destaca para `nota`, ou null.
// outras são as notas que o painel conhece (a própria nota, se estiver entre elas,
// é ignorada pelo id).
export function sugerirMarcador(
  nota: NotaParaSugerir,
  outras: readonly NotaParaSugerir[],
  marcadores: readonly { id: string; nome: string }[],
): SugestaoDeMarcador | null {
  // Nota que já tem marcador não recebe sugestão (a pessoa já decidiu).
  if (nota.marcadores.length > 0) return null
  const minhas = palavrasDe(nota.titulo, nota.corpo)
  if (minhas.size < MIN_PALAVRAS_EM_COMUM) return null

  const candidatas = outras.filter((o) => !o.deleted_at && (nota.id === undefined || o.id !== nota.id))
  const placar: SugestaoDeMarcador[] = []
  for (const m of marcadores) {
    const doMarcador = candidatas.filter((o) => o.marcadores.includes(m.id))
    if (doMarcador.length < MIN_NOTAS_DO_MARCADOR) continue
    // Em quantas notas do marcador cada palavra aparece.
    const usos = new Map<string, number>()
    for (const o of doMarcador) for (const p of palavrasDe(o.titulo, o.corpo)) usos.set(p, (usos.get(p) ?? 0) + 1)
    let pontos = 0
    let emComum = 0
    for (const p of minhas) {
      const n = usos.get(p)
      if (n) {
        pontos += n / doMarcador.length
        emComum++
      }
    }
    placar.push({ id: m.id, nome: m.nome, pontos, emComum })
  }

  placar.sort((a, b) => b.pontos - a.pontos)
  const [melhor, segundo] = placar
  if (!melhor || melhor.emComum < MIN_PALAVRAS_EM_COMUM || melhor.pontos < MIN_PONTOS) return null
  // O segundo conta mesmo abaixo do mínimo: um empate com um marcador fraco
  // também é dúvida.
  if (segundo && melhor.pontos < MARGEM * segundo.pontos) return null
  return melhor
}
