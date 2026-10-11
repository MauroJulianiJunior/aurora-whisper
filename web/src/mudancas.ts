// A chegada mais rápida no PC (ADR-021, decisão P, subfatia S): o painel do PC
// lê o contador de mudanças do sync a cada poucos segundos enquanto está à vista
// e, quando o número muda, relê a lista pelo mesmo caminho do "voltar à aba".
// O ditado do celular e as edições feitas lá aparecem sozinhos, sem a pessoa
// tocar em nada e sem tirar da tela o que ela está editando (a recarga mantém
// os editores e os rascunhos: mesclarRecarga).
//
// A própria leitura é o aviso ao Agent de que o painel está aberto: enquanto
// ela se repete, o laço do sync busca a nuvem a cada 10 s, e não a cada 60 s.
// Por isso o painel só lê com a aba à vista: escondido, ele para, e o Agent
// volta aos 60 s sozinho, 15 s depois.
//
// Só o PC tem o contador (Ambiente.mudancasDoSync). A nuvem relê a lista só ao
// voltar à aba, como antes.

import { useEffect, useLayoutEffect, useRef } from 'react'
import { ErroDaApi } from './api'

// A cada quantos milissegundos o painel à vista lê o contador. O Agent
// considera o painel aberto por 15 s depois de cada leitura (internal/sync,
// JanelaDoPainelPadrao): 3 s deixam passar quatro leituras seguidas sem que a
// janela feche, e a leitura é só de memória, no loopback.
export const intervaloDoContador = 3000

// LerContador pergunta ao Agent o contador de mudanças do sync.
export type LerContador = (sinal: AbortSignal) => Promise<number>

// useMudancasDoSync lê o contador com a aba à vista e chama aoMudar quando o
// número é outro que o da leitura anterior (inclusive menor: o Agent
// reiniciou). A primeira leitura só marca o ponto de partida. Sem ler (a
// nuvem), não faz nada.
//
// Duas leituras nunca ficam no ar ao mesmo tempo: se uma demora mais que o
// intervalo, a rodada seguinte espera. Uma leitura que falha (o Agent
// reiniciando, a sessão perdida) não diz nada: a lista, ao voltar à aba, é
// quem acende a faixa do topo, e a leitura seguinte tenta de novo. A exceção é
// o 404 de um Agent de antes da S, que não tem a rota: aí o painel para de
// perguntar.
//
// Uma mudança que chega entre a primeira carga da lista e a primeira leitura
// (milissegundos, na montagem) só aparece na mudança seguinte, ou ao voltar à
// aba. As leituras seguintes não têm essa janela: o ponto de partida é lido
// antes de a lista ser pedida de novo.
export function useMudancasDoSync(ler: LerContador | undefined, aoMudar: () => void): void {
  const aoMudarAtual = useRef(aoMudar)
  useLayoutEffect(() => {
    aoMudarAtual.current = aoMudar
  })

  useEffect(() => {
    if (!ler) return
    const leitor = ler
    let ultimo: number | null = null
    let noAr: AbortController | null = null
    let parou = false

    async function consultar() {
      if (parou || noAr || document.visibilityState === 'hidden') return
      const ctrl = new AbortController()
      noAr = ctrl
      try {
        const atual = await leitor(ctrl.signal)
        if (ctrl.signal.aborted) return
        const mudou = ultimo !== null && atual !== ultimo
        ultimo = atual
        if (mudou) aoMudarAtual.current()
      } catch (e) {
        if (ctrl.signal.aborted) return
        if (e instanceof ErroDaApi && e.status === 404) {
          parou = true
          clearInterval(relogio)
        }
      } finally {
        if (noAr === ctrl) noAr = null
      }
    }

    // O relógio nasce antes da primeira leitura: ela pode precisar pará-lo (o
    // 404).
    const relogio = setInterval(() => void consultar(), intervaloDoContador)
    void consultar()
    // A aba que volta lê já: é o aviso ao Agent de que o painel está aberto, e
    // o contador pode ter andado enquanto ela estava escondida.
    const aoVoltar = () => void consultar()
    document.addEventListener('visibilitychange', aoVoltar)
    return () => {
      parou = true
      clearInterval(relogio)
      document.removeEventListener('visibilitychange', aoVoltar)
      noAr?.abort()
    }
  }, [ler])
}
