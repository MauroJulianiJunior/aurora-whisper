// As regras dos marcadores que a tela precisa conhecer (ADR-022, "O marcador"):
// a limpeza do nome, a chave em que dois nomes são o mesmo e a contagem de notas.
// Puro (sem React, sem rede). Quem decide é o Agent (storage.nomeDoMarcador): a
// tela só antecipa a recusa para avisar antes do pedido.

// MAX_NOME_DO_MARCADOR é storage.MaxNomeDoMarcador: 50 caracteres.
export const MAX_NOME_DO_MARCADOR = 50

// limparNome tira os espaços das pontas e reduz cada sequência de espaços a um,
// como o Agent faz antes de gravar.
export const limparNome = (bruto: string) => bruto.split(/\s+/).filter(Boolean).join(' ')

// dobrar põe em minúscula e sem acento: a chave da unicidade do nome ("Compras",
// "compras" e "cômpras" são o mesmo marcador).
export const dobrar = (s: string) => s.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase()

// acharPorNome devolve o marcador que já tem esse nome (pela chave dobrada), se
// houver. exceto: o id do que está sendo renomeado, que pode manter o próprio nome
// (ou só mudar a caixa dele).
export function acharPorNome<T extends { id: string; nome: string }>(marcadores: readonly T[], nome: string, exceto?: string): T | undefined {
  const chave = dobrar(limparNome(nome))
  if (chave === '') return undefined
  return marcadores.find((m) => m.id !== exceto && dobrar(m.nome) === chave)
}

// ordenarPorNome é a ordem da lista do Agent: pelo nome sem caixa nem acento.
export const ordenarPorNome = <T extends { nome: string }>(marcadores: readonly T[]): T[] =>
  [...marcadores].sort((a, b) => dobrar(a.nome).localeCompare(dobrar(b.nome)))

// contagemDeNotas diz "3 notas", "1 nota" ou "nenhuma nota".
export const contagemDeNotas = (n: number) => (n === 0 ? 'nenhuma nota' : n === 1 ? '1 nota' : `${n} notas`)
