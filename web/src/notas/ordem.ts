// A ordem manual das notas, a parte pura (ADR-022, "Ordem manual (N2b)"): onde
// cada cartão fica na grade, qual vaga está mais perto do cartão arrastado, e o
// que fazer com a lista quando uma nota muda de lugar. Nada aqui toca o DOM nem
// a rede: o gesto (useOrdemManual.ts) mede os cartões e chama estas contas, e o
// teste as confere com uma tabela de alturas.

// ALTURA_ESTIMADA é a altura de um cartão que ainda não foi medido (a primeira
// pintura, ou o jsdom dos testes). Todos iguais, a grade cai em rodízio: o 1º na
// coluna 1, o 2º na 2, e assim por diante (i % colunas).
export const ALTURA_ESTIMADA = 1

export interface Disposicao {
  // coluna[i] e y[i] são o lugar do i-ésimo cartão da ordem de leitura: a coluna
  // (0 é a da esquerda) e a distância do topo da grade.
  coluna: number[]
  y: number[]
  // altura é a da grade inteira (a da coluna mais comprida, sem o vão de baixo).
  altura: number
}

// dispor põe os cartões na grade em ORDEM DE LEITURA: cada um, na ordem, vai para
// a coluna que está mais curta agora (a da esquerda, no empate). É o que o CSS
// columns não faz: lá a ordem desce a primeira coluna antes de ir para a
// segunda, e arrastar um cartão para "depois do 3º" caía num lugar que a pessoa
// não via. alturas são as medidas dos cartões; vao é o espaço entre eles (nas
// duas direções).
export function dispor(alturas: readonly number[], colunas: number, vao: number): Disposicao {
  const n = Math.max(1, Math.floor(colunas))
  const fundo = new Array<number>(n).fill(0) // onde cada coluna termina (com o vão de baixo)
  const coluna: number[] = []
  const y: number[] = []
  for (const medida of alturas) {
    const h = medida > 0 ? medida : ALTURA_ESTIMADA
    let escolhida = 0
    for (let c = 1; c < n; c++) {
      if (fundo[c]! < fundo[escolhida]!) escolhida = c
    }
    coluna.push(escolhida)
    y.push(fundo[escolhida]!)
    fundo[escolhida] = fundo[escolhida]! + h + vao
  }
  const maior = alturas.length === 0 ? 0 : Math.max(...fundo) - vao
  return { coluna, y, altura: Math.max(0, maior) }
}

export interface Ponto {
  x: number
  y: number
}

// Geometria é o que o gesto mediu: quantas colunas, quanto vale uma coluna de
// largura e o vão.
export interface Geometria {
  colunas: number
  larguraDaColuna: number
  vao: number
}

// centroDaVaga é o centro de um cartão da disposição, nas coordenadas da grade.
function centroDaVaga(d: Disposicao, i: number, altura: number, g: Geometria): Ponto {
  return {
    x: d.coluna[i]! * (g.larguraDaColuna + g.vao) + g.larguraDaColuna / 2,
    y: d.y[i]! + altura / 2,
  }
}

// vagaMaisPerto diz em que posição (0 a restantes.length) o cartão arrastado
// ficaria se fosse solto com o centro em `centro`. restantes são as alturas dos
// OUTROS cartões da seção, na ordem; alturaDoArrastado é a dele. Para cada vaga
// possível, simula a grade com o cartão ali e olha a que fica mais perto do
// centro: assim a vaga que a pessoa vê (o espaço aberto) é exatamente a que se
// escolhe, e a conta não depende de onde a vaga estava antes, o que evita o
// vaivém da vaga quando os cartões se mexem. No empate, a vaga mais para cima.
export function vagaMaisPerto(restantes: readonly number[], alturaDoArrastado: number, g: Geometria, centro: Ponto): number {
  const h = alturaDoArrastado > 0 ? alturaDoArrastado : ALTURA_ESTIMADA
  let melhor = 0
  let menor = Infinity
  for (let k = 0; k <= restantes.length; k++) {
    const com = [...restantes.slice(0, k), h, ...restantes.slice(k)]
    const d = dispor(com, g.colunas, g.vao)
    const c = centroDaVaga(d, k, h, g)
    const dist = (c.x - centro.x) ** 2 + (c.y - centro.y) ** 2
    if (dist < menor - 1e-9) {
      menor = dist
      melhor = k
    }
  }
  return melhor
}

// moverNaLista devolve a ordem dos ids com `id` na posição `indice` (contada
// SEM ele: 0 é o topo, ids.length - 1 é o fim). indice fora do intervalo é
// levado para a ponta.
export function moverNaLista(ids: readonly string[], id: string, indice: number): string[] {
  const sem = ids.filter((x) => x !== id)
  const i = Math.min(Math.max(0, Math.round(indice)), sem.length)
  sem.splice(i, 0, id)
  return sem
}

export interface Vizinhas {
  // antes e depois são as vizinhas da nota DEPOIS de soltar, ou null se não há
  // desse lado: é o corpo de POST /api/notas/{id}/mover.
  antes: string | null
  depois: string | null
}

export function vizinhasDe(ordem: readonly string[], id: string): Vizinhas {
  const i = ordem.indexOf(id)
  return { antes: i > 0 ? ordem[i - 1]! : null, depois: i >= 0 && i < ordem.length - 1 ? ordem[i + 1]! : null }
}

// aplicarOrdem refaz a lista de itens com a nova ordem de UM grupo: os lugares
// que o grupo ocupava na lista passam a ser ocupados pelos ids em `ordemNova`.
// Os itens de fora do grupo não saem do lugar (a lista tem as fixadas e as outras
// juntas, e mover uma fixada não mexe nas outras).
export function aplicarOrdem<T extends { id: string }>(itens: readonly T[], ordemNova: readonly string[]): T[] {
  const doGrupo = new Set(ordemNova)
  const porId = new Map(itens.map((x) => [x.id, x]))
  let proximo = 0
  return itens.map((x) => (doGrupo.has(x.id) ? (porId.get(ordemNova[proximo++]!) ?? x) : x))
}

export type TeclaDeSeta = 'ArrowUp' | 'ArrowDown' | 'ArrowLeft' | 'ArrowRight'

export const ehTeclaDeSeta = (k: string): k is TeclaDeSeta =>
  k === 'ArrowUp' || k === 'ArrowDown' || k === 'ArrowLeft' || k === 'ArrowRight'

// indiceDoTeclado é a posição nova (a mesma conta de moverNaLista: sem o item)
// de uma nota movida com Alt + seta, ou null se a tecla não vale nesta forma de
// ver. `indice` é onde a nota está, `total` o tamanho do grupo.
//
//   - lista e detalhes (uma coluna): ↑ sobe uma, ↓ desce uma; ←/→ não fazem nada.
//   - grade: ← e → andam uma vaga na ordem de leitura; ↑ e ↓ andam uma "linha",
//     isto é, `colunas` vagas (na grade em ordem de leitura, a vaga de baixo é a
//     `colunas` adiante, aproximadamente: os cartões têm alturas diferentes).
//
// Na ponta, devolve a própria posição (não há para onde ir).
export function indiceDoTeclado(tecla: TeclaDeSeta, indice: number, total: number, colunas: number): number | null {
  let passo: number
  if (colunas <= 1) {
    if (tecla === 'ArrowUp') passo = -1
    else if (tecla === 'ArrowDown') passo = 1
    else return null
  } else {
    passo = tecla === 'ArrowLeft' ? -1 : tecla === 'ArrowRight' ? 1 : tecla === 'ArrowUp' ? -colunas : colunas
  }
  return Math.min(Math.max(0, indice + passo), total - 1)
}

// frasePosicao é o que a região viva anuncia depois de mover.
export const frasePosicao = (posicao: number, total: number) => `Movida para a posição ${posicao} de ${total}.`
export const fraseNaPonta = (posicao: number, total: number) => `Já está na posição ${posicao} de ${total}.`

// velocidadeDaRolagem diz quantos pixels rolar por quadro, perto das bordas da
// área visível: 0 no meio, e cada vez mais rápido (até `maxima`) quanto mais
// perto da borda; negativo para cima. `zona` é a faixa de cada borda que rola.
export function velocidadeDaRolagem(y: number, topo: number, base: number, zona = 72, maxima = 18): number {
  if (y < topo + zona) return -Math.min(maxima, ((topo + zona - y) / zona) * maxima)
  if (y > base - zona) return Math.min(maxima, ((y - (base - zona)) / zona) * maxima)
  return 0
}
