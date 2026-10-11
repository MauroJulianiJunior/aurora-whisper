// Edição do texto final: o rascunho e o salvar contra a versão em que ele
// começou.
//
// Revisão de 29-set-2026: o rascunho guardava só o texto, e o salvar
// mandava a versão do item que estivesse na lista. Depois de qualquer
// recarga, essa era a versão que OUTRA aba tinha acabado de gravar — o
// PATCH passava sem 409 e apagava a edição da outra aba. E um rascunho
// nunca tocado reaparecia como "não salvo", com o texto velho.

import { type ClienteDoHistorico, Conflito, type Ditado, type Sugestao } from './api'

export interface Rascunho {
  texto: string
  // base e textoBase: a versão do item e o texto final dele quando a
  // edição começou. base vai no PATCH como base_version; textoBase diz se
  // um 409 é mesmo outra edição ou só a versão que subiu.
  base: number
  textoBase: string
  // conflito: o último salvar achou o texto mudado por outra aba. Vive no
  // rascunho, não na tela, para a nota reaparecer quando a linha reabre.
  conflito: boolean
  // semConexao: o último salvar não chegou a lugar nenhum (a rede, na
  // nuvem). A edição não foi guardada em lugar nenhum, nem no aparelho: a
  // nota diz isso, e o texto fica na tela até a pessoa tentar de novo.
  semConexao?: boolean
}

// semTextoFinal: o ditado falhou depois do ASR e antes de o texto final
// existir (o Agent grava final_text vazio nesse caso). O texto que existe
// é o bruto — é ele que a linha mostra e que o editor oferece.
export function semTextoFinal(item: Ditado): boolean {
  return item.status === 'error' && item.final_text === '' && item.raw_text !== ''
}

// textoInicial é o que o editor mostra ao abrir, antes de a pessoa digitar.
export function textoInicial(item: Ditado): string {
  return semTextoFinal(item) ? item.raw_text : item.final_text
}

export function novoRascunho(item: Ditado): Rascunho {
  return { texto: textoInicial(item), base: item.version, textoBase: item.final_text, conflito: false }
}

// naoTocado: o rascunho ainda é exatamente o que o editor mostrou ao
// abrir. Rascunho assim não vale a pena guardar: some ao trocar de linha,
// e ao reabrir o editor parte do texto atual, não de um velho.
export function naoTocado(r: Rascunho, item: Ditado | undefined): boolean {
  if (r.conflito) return false
  const inicial = item ? textoInicial({ ...item, final_text: r.textoBase }) : r.textoBase
  return r.texto === inicial
}

// textosNaoSalvos: o texto de cada rascunho que a pessoa mexeu e não salvou
// (o do editor aberto e os das linhas com "não salvo"), na ordem em que as
// linhas foram abertas. É o que a nuvem não deixa sumir quando a sessão
// acaba (ADR-019, "Decisões da F4"). O editor aberto e não tocado não conta.
export function textosNaoSalvos(rascunhos: Record<string, Rascunho>, itens: readonly Ditado[] = []): string[] {
  return Object.entries(rascunhos)
    .filter(([id, r]) => !naoTocado(r, itens.find((i) => i.id === id)))
    .map(([, r]) => r.texto)
}

// sugestoes: as trocas de termo que a edição sugere aprender (quase sempre
// vazia). Só o salvamento que gravou as traz, nunca o conflito.
export type Salvamento = { tipo: 'salvo'; item: Ditado; sugestoes: Sugestao[] } | { tipo: 'conflito'; atual: Ditado }

// salvarSobre grava o texto sobre a versão base. No 409, busca o atual:
// se o texto final dele ainda é o textoBase, só a versão mudou (lixeira e
// Desfazer, ou Restaurar, em outra aba sobem a versão sem tocar no texto)
// e tenta de novo UMA vez sobre a versão nova, sem incomodar ninguém. Se
// o texto mudou, é conflito de verdade: devolve o atual para a pessoa
// decidir. Item que está na lixeira também volta como conflito, mesmo
// com o texto igual: gravar nele seria salvar num ditado que a pessoa não
// vê mais no histórico (revisão de 29-set-2026).
export async function salvarSobre(cliente: Pick<ClienteDoHistorico, 'editar' | 'obter'>, id: string, base: number, textoBase: string, texto: string): Promise<Salvamento> {
  try {
    const g = await cliente.editar(id, base, texto)
    return { tipo: 'salvo', item: g.ditado, sugestoes: g.sugestoes }
  } catch (e) {
    if (!(e instanceof Conflito)) throw e
  }
  let atual = await cliente.obter(id)
  if (atual.final_text === textoBase && !atual.deleted_at) {
    try {
      const g = await cliente.editar(id, atual.version, texto)
      return { tipo: 'salvo', item: g.ditado, sugestoes: g.sugestoes }
    } catch (e) {
      if (!(e instanceof Conflito)) throw e
    }
    atual = await cliente.obter(id)
  }
  return { tipo: 'conflito', atual }
}
