// A lista do painel como dado: o resultado de uma carga, a ordem da API e
// o que fazer com ele quando algo muda. Fica em funções puras porque é
// aqui que moravam os defeitos da revisão de 29-set-2026 — mensagem de uma
// busca aplicada ao resultado de outra, recarga que jogava fora as páginas
// já carregadas, Desfazer que recarregava tudo.

import type { Ditado } from './api'

export type Vista = 'historico' | 'lixeira'

// Resultado guarda a consulta que o produziu — vista, busca e o momento da
// carga. Mensagem de vazio, "Mostrar mais antigos" e os rótulos "Hoje" e
// "Ontem" leem daqui, nunca do estado atual da tela: o estado atual pode
// ser de uma consulta que ainda não respondeu.
export interface Resultado {
  chave: string
  vista: Vista
  busca: string
  hoje: Date
  itens: Ditado[]
  proximo?: string
  erro?: string
  // anuncio é o que a região viva diz desta carga. Calculado na carga, e
  // não a cada render: tirar uma linha da lista não é uma carga nova, e o
  // leitor de tela não precisa ouvir a contagem de novo.
  anuncio: string
}

// A ordem da API (internal/storage): no histórico, mais novo primeiro; na
// lixeira, o que foi apagado por último primeiro. Empate de data: id maior
// primeiro.
export type CampoDaOrdem = 'created_at' | 'deleted_at'

export const campoDaOrdem = (vista: Vista): CampoDaOrdem => (vista === 'lixeira' ? 'deleted_at' : 'created_at')

const instante = (d: Ditado, campo: CampoDaOrdem) => Date.parse(d[campo] ?? d.created_at)

// vemAntes diz se a vem antes de b na ordem da API.
export function vemAntes(a: Ditado, b: Ditado, campo: CampoDaOrdem): boolean {
  const ta = instante(a, campo)
  const tb = instante(b, campo)
  if (ta !== tb) return ta > tb
  return a.id > b.id
}

// inserirEmOrdem devolve o item ao lugar dele na lista. Se o lugar fica
// depois do último carregado e ainda há páginas, ele não entra: virá com
// "Mostrar mais antigos", e entrar agora o faria aparecer duas vezes.
export function inserirEmOrdem(itens: Ditado[], item: Ditado, campo: CampoDaOrdem, haMais: boolean): Ditado[] {
  const resto = itens.filter((i) => i.id !== item.id)
  const lugar = resto.findIndex((i) => vemAntes(item, i, campo))
  if (lugar === -1) return haMais ? resto : [...resto, item]
  return [...resto.slice(0, lugar), item, ...resto.slice(lugar)]
}

// vizinho é a linha que ocupa o lugar de uma que sai da lista: a de baixo,
// ou a de cima quando era a última.
export function vizinho(itens: Ditado[], id: string): string | null {
  const i = itens.findIndex((x) => x.id === id)
  if (i === -1) return null
  return itens[i + 1]?.id ?? itens[i - 1]?.id ?? null
}

// anexarPagina junta a página seguinte, sem repetir o que já está na
// lista (um ditado pode ter voltado ao lugar por Desfazer).
export function anexarPagina(itens: Ditado[], novos: Ditado[]): Ditado[] {
  const ja = new Set(itens.map((i) => i.id))
  return [...itens, ...novos.filter((i) => !ja.has(i.id))]
}

// mesmaConsulta: dois resultados da mesma vista e da mesma busca. Uma
// recarga (voltar à aba, "Tentar de novo") muda a chave, mas não a
// consulta — e o que chega depois dela ainda vale para a lista na tela.
export const mesmaConsulta = (a: Pick<Resultado, 'vista' | 'busca'>, b: Pick<Resultado, 'vista' | 'busca'>) =>
  a.vista === b.vista && a.busca === b.busca

// sumidosEmEdicao: o que está em edição (a linha aberta, os rascunhos),
// estava na lista e não veio na recarga da mesma consulta. Pode ter ido
// para a lixeira em outra aba, ou mudado de texto lá e saído da busca: o
// painel pergunta por cada um (revisão de 29-set-2026).
export function sumidosEmEdicao(anterior: Resultado | null, novo: Resultado, emEdicao: ReadonlySet<string>): Ditado[] {
  if (!anterior || anterior.erro || novo.erro || !mesmaConsulta(anterior, novo)) return []
  const veio = new Set(novo.itens.map((i) => i.id))
  return anterior.itens.filter((i) => emEdicao.has(i.id) && !veio.has(i.id))
}

// mesclarRecarga aplica uma recarga da MESMA consulta (a aba voltou a ter
// foco, "Tentar de novo"). A recarga traz só a primeira página; se a
// pessoa já tinha carregado mais, as linhas mais antigas que a primeira
// página continuam, com o cursor de antes — senão, voltar à aba sumiria
// com o que ela carregou, e com o editor que estivesse aberto lá embaixo.
//
// O que está em edição também fica, mesmo que a recarga não o traga.
// Revisão de 29-set-2026: a pessoa digitava, punha o ditado na lixeira em
// outra aba e voltava; a recarga trocava a lista, e o editor sumia com o
// texto dentro, sem nota nenhuma — justo o texto que ela precisava copiar.
export function mesclarRecarga(anterior: Resultado | null, novo: Resultado, emEdicao: ReadonlySet<string> = new Set()): Resultado {
  if (!anterior || anterior.erro || novo.erro || !mesmaConsulta(anterior, novo)) return novo
  const campo = campoDaOrdem(novo.vista)
  let itens = novo.itens
  let proximo = novo.proximo
  const ultimo = novo.itens[novo.itens.length - 1]
  if (novo.proximo && ultimo) {
    const ja = new Set(novo.itens.map((i) => i.id))
    const cauda = anterior.itens.filter((i) => !ja.has(i.id) && vemAntes(ultimo, i, campo))
    if (cauda.length > 0) {
      itens = [...itens, ...cauda]
      proximo = anterior.proximo
    }
  }
  // haMais falso: a linha em edição fica na tela mesmo que o lugar dela
  // seja depois do último carregado.
  for (const item of sumidosEmEdicao(anterior, novo, emEdicao)) {
    if (!itens.some((i) => i.id === item.id)) itens = inserirEmOrdem(itens, item, campo, false)
  }
  if (itens === novo.itens) return novo
  const mesclado = { ...novo, itens, proximo }
  return { ...mesclado, anuncio: anunciar(mesclado) }
}

export type Consulta = Pick<Resultado, 'chave' | 'vista' | 'busca' | 'hoje'>

// aposFalha decide o que fica na tela quando uma carga falha.
//
// Falha de ligação (sem sessão, Agent fora do ar) nunca troca a lista: é
// nela que estão os editores e os rascunhos que a pessoa precisa copiar
// antes de abrir o painel de novo — mesmo que a carga que falhou fosse de
// outra vista (revisão de 29-set-2026: a tela inteira era trocada e o
// texto digitado sumia). Uma recarga da MESMA consulta que falha por
// outro motivo também deixa a lista como está. Só uma consulta nova, que
// não tem o que mostrar, vira a tela de erro.
export function aposFalha(anterior: Resultado | null, consulta: Consulta, mensagem: string, deLigacao: boolean): Resultado {
  const mesma = anterior !== null && !anterior.erro && mesmaConsulta(anterior, consulta)
  if (anterior && (deLigacao || mesma)) return { ...anterior, chave: consulta.chave }
  const erro = { ...consulta, itens: [], erro: mensagem }
  return { ...erro, anuncio: anunciar(erro) }
}

const plural = (n: number, um: string, varios: string) => `${n} ${n === 1 ? um : varios}`

// anunciar é a frase da região viva para uma carga (WCAG 4.1.3): quem usa
// leitor de tela, com o foco na busca, precisa ouvir se achou algo.
export function anunciar(r: Pick<Resultado, 'vista' | 'busca' | 'itens' | 'proximo' | 'erro'>): string {
  const naLixeira = r.vista === 'lixeira'
  if (r.erro) return `Não consegui carregar ${naLixeira ? 'a lixeira' : 'o histórico'}.`
  if (r.itens.length === 0) {
    if (r.busca) return `Nada encontrado para “${r.busca}”.`
    return naLixeira ? 'A lixeira está vazia.' : 'Nenhum ditado ainda.'
  }
  const onde = r.busca ? `para “${r.busca}”` : naLixeira ? 'na lixeira' : 'no histórico'
  return `${plural(r.itens.length, 'ditado', 'ditados')} ${onde}${r.proximo ? ', e há mais antigos' : ''}.`
}
